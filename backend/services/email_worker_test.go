package services

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// inserirUsuarioDeTeste cria uma linha mínima em usuarios só para satisfazer
// a FK de tokens_acao/emails_pendentes — os testes deste arquivo não passam
// pelo fluxo de Cadastrar.
func inserirUsuarioDeTeste(t *testing.T, db *sql.DB, email string) string {
	t.Helper()
	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
		VALUES ('Usuário Worker', $1, 'hash', 'usuario', false, true)
		RETURNING id`
	if err := db.QueryRow(insert, email).Scan(&id); err != nil {
		t.Fatalf("falha ao inserir usuario de teste: %v", err)
	}
	return id
}

// inserirEmailPendente insere diretamente uma linha em emails_pendentes,
// simulando o que EnfileirarEmail teria feito dentro da transação de
// cadastro.
func inserirEmailPendente(t *testing.T, db *sql.DB, usuarioID, destinatario, tipo string, variaveis map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(variaveis)
	if err != nil {
		t.Fatalf("falha ao serializar variaveis: %v", err)
	}
	var id string
	const insert = `
		INSERT INTO emails_pendentes (usuario_id, destinatario, tipo, variaveis_json)
		VALUES ($1, $2, $3, $4)
		RETURNING id`
	if err := db.QueryRow(insert, usuarioID, destinatario, tipo, payload).Scan(&id); err != nil {
		t.Fatalf("falha ao inserir email pendente: %v", err)
	}
	return id
}

func lerEmailPendente(t *testing.T, db *sql.DB, id string) (status string, tentativas int, ultimoErro sql.NullString) {
	t.Helper()
	if err := db.QueryRow(`SELECT status, tentativas, ultimo_erro FROM emails_pendentes WHERE id = $1`, id).
		Scan(&status, &tentativas, &ultimoErro); err != nil {
		t.Fatalf("falha ao reler email pendente: %v", err)
	}
	return status, tentativas, ultimoErro
}

// TestProcessarProximoEmailPendente_SemSMTP prova o AC3 desta story: rodando
// num ambiente sem SMTP configurado (SMTP_PASSWORD vazio, o mesmo de
// testEmailCfg), uma linha pendente registra a falha (tentativas
// incrementadas, ultimo_erro preenchido) sem derrubar o processo — a chamada
// simplesmente retorna.
func TestProcessarProximoEmailPendente_SemSMTP(t *testing.T) {
	db := testDB(t)
	usuarioID := inserirUsuarioDeTeste(t, db, "worker-sem-smtp@empresa.com")
	id := inserirEmailPendente(t, db, usuarioID, "worker-sem-smtp@empresa.com", "verificacao_conta", map[string]any{
		"nome": "Fulano",
		"link": "http://test.local/verificar-email?token=abc",
	})

	processarProximoEmailPendente(db, testEmailCfg)

	status, tentativas, ultimoErro := lerEmailPendente(t, db, id)
	if status != "pendente" {
		t.Errorf("status = %q, want %q (1ª falha ainda abaixo do limite de tentativas)", status, "pendente")
	}
	if tentativas != 1 {
		t.Errorf("tentativas = %d, want 1", tentativas)
	}
	if !ultimoErro.Valid || ultimoErro.String == "" {
		t.Error("ultimo_erro não preenchido após falha de envio")
	}
}

// TestProcessarProximoEmailPendente_MarcaFalhoAposLimite prova que, depois de
// maxTentativasEmail falhas consecutivas, a linha vira status='falho'
// (terminal) e para de ser reprocessada.
func TestProcessarProximoEmailPendente_MarcaFalhoAposLimite(t *testing.T) {
	db := testDB(t)
	usuarioID := inserirUsuarioDeTeste(t, db, "worker-limite@empresa.com")
	id := inserirEmailPendente(t, db, usuarioID, "worker-limite@empresa.com", "verificacao_conta", map[string]any{
		"nome": "Fulano",
		"link": "http://test.local/verificar-email?token=abc",
	})

	for i := 0; i < maxTentativasEmail; i++ {
		processarProximoEmailPendente(db, testEmailCfg)
	}

	status, tentativas, _ := lerEmailPendente(t, db, id)
	if status != "falho" {
		t.Errorf("status = %q, want %q após %d tentativas", status, "falho", maxTentativasEmail)
	}
	if tentativas != maxTentativasEmail {
		t.Errorf("tentativas = %d, want %d", tentativas, maxTentativasEmail)
	}

	// Uma vez 'falho', a linha é terminal: outra chamada não deve
	// processá-la de novo (nenhuma linha 'pendente' resta para o SELECT
	// buscar).
	processarProximoEmailPendente(db, testEmailCfg)
	statusDepois, tentativasDepois, _ := lerEmailPendente(t, db, id)
	if statusDepois != "falho" || tentativasDepois != maxTentativasEmail {
		t.Errorf("linha 'falho' foi reprocessada: status=%q tentativas=%d", statusDepois, tentativasDepois)
	}
}

// TestProcessarProximoEmailPendente_RedefinicaoSenhaTemTemplate prova que,
// a partir da Story 1.6, o worker resolve o template de 'redefinicao_senha'
// SEM cair no ramo de "tipo de e-mail desconhecido": a linha passa da
// renderização e só falha no envio SMTP (testEmailCfg não tem SMTP),
// registrando a falha sem derrubar o worker. O CHECK de
// emails_pendentes.tipo só admite tipos que hoje têm template
// ('verificacao_conta', 'redefinicao_senha'), então o ramo de erro de
// renderização do worker é defensivo — coberto isoladamente por
// TestRenderizarTemplate_TipoDesconhecidoRetornaErro.
func TestProcessarProximoEmailPendente_RedefinicaoSenhaTemTemplate(t *testing.T) {
	db := testDB(t)
	usuarioID := inserirUsuarioDeTeste(t, db, "worker-redefinicao@empresa.com")
	id := inserirEmailPendente(t, db, usuarioID, "worker-redefinicao@empresa.com", "redefinicao_senha", map[string]any{
		"nome": "Fulano",
		"link": "http://test.local/redefinir-senha?token=abc123",
	})

	processarProximoEmailPendente(db, testEmailCfg)

	status, tentativas, ultimoErro := lerEmailPendente(t, db, id)
	if status != "pendente" {
		t.Errorf("status = %q, want %q", status, "pendente")
	}
	if tentativas != 1 {
		t.Errorf("tentativas = %d, want 1", tentativas)
	}
	if !ultimoErro.Valid || ultimoErro.String == "" {
		t.Fatal("ultimo_erro não preenchido")
	}
	if strings.Contains(ultimoErro.String, "tipo de e-mail desconhecido") {
		t.Errorf("worker caiu no ramo de tipo desconhecido para 'redefinicao_senha': %q", ultimoErro.String)
	}
}

// TestProcessarProximoEmailPendente_SemLinhasPendentes prova que rodar o
// worker sem nenhuma linha pendente não gera pânico nem erro fatal.
func TestProcessarProximoEmailPendente_SemLinhasPendentes(t *testing.T) {
	db := testDB(t)
	processarProximoEmailPendente(db, testEmailCfg)
}

// TestIniciarWorkerEmail_ProcessaAutomaticamente prova que o worker, uma vez
// iniciado, processa uma linha pendente sozinho dentro de alguns ciclos de
// polling, e que `parar()` encerra a goroutine de forma limpa (a chamada
// retorna).
func TestIniciarWorkerEmail_ProcessaAutomaticamente(t *testing.T) {
	db := testDB(t)
	usuarioID := inserirUsuarioDeTeste(t, db, "worker-automatico@empresa.com")
	id := inserirEmailPendente(t, db, usuarioID, "worker-automatico@empresa.com", "verificacao_conta", map[string]any{
		"nome": "Fulano",
		"link": "http://test.local/verificar-email?token=abc",
	})

	parar := IniciarWorkerEmail(db, testEmailCfg, 20*time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	var tentativas int
	for time.Now().Before(deadline) {
		_, tentativas, _ = lerEmailPendente(t, db, id)
		if tentativas > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	parar()

	if tentativas == 0 {
		t.Fatal("worker não processou a linha pendente dentro do prazo esperado")
	}
}
