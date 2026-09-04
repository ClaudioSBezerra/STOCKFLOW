// Testes de SolicitarExclusaoConta / ListarSolicitacoesExclusao /
// ProcessarExclusaoConta — Story 8.2 (Epic 8, Privacidade/LGPD), spec-8-2.
// Cobrem a I/O & Edge-Case Matrix no nível de service.
package services

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// inserirSolicitacaoExclusao grava uma linha em solicitacoes_exclusao_conta
// com status e ordem de criação controlados. `processadoPor` vazio -> NULL;
// para status != 'pendente', `processado_em` recebe now() (trilha de
// auditoria, exigida pelo CHECK). `ordem` vira um offset em `criado_em` para
// tornar o ORDER BY determinístico. Devolve o id gerado.
func inserirSolicitacaoExclusao(t *testing.T, db *sql.DB, solicitanteID, status, processadoPor string, ordem int) string {
	t.Helper()
	var proc sql.NullString
	if processadoPor != "" {
		proc = sql.NullString{String: processadoPor, Valid: true}
	}
	processadoEm := sql.NullTime{Time: time.Now().UTC(), Valid: status != "pendente"}
	const insert = `
		INSERT INTO solicitacoes_exclusao_conta (solicitante_id, status, processado_por, processado_em, criado_em)
		VALUES ($1, $2, $3, $4, now() + ($5 || ' seconds')::interval)
		RETURNING id`
	var id string
	if err := db.QueryRow(insert, solicitanteID, status, proc, processadoEm, ordem).Scan(&id); err != nil {
		t.Fatalf("falha ao inserir solicitação de exclusão (%s): %v", status, err)
	}
	return id
}

func contarPorUsuario(t *testing.T, db *sql.DB, tabela, usuarioID string) int {
	t.Helper()
	var n int
	q := fmt.Sprintf(`SELECT count(*) FROM %s WHERE usuario_id = $1`, tabela)
	if err := db.QueryRow(q, usuarioID).Scan(&n); err != nil {
		t.Fatalf("contar %s por usuario_id: %v", tabela, err)
	}
	return n
}

// --- SolicitarExclusaoConta --------------------------------------------

// TestSolicitarExclusaoConta_Happy cobre "Usuário solicita exclusão (1ª vez)":
// linha `pendente` criada, id/criadoEm preenchidos, ProcessadoEm nil.
func TestSolicitarExclusaoConta_Happy(t *testing.T) {
	db := testDB(t)
	id := semearConta(t, db, "Solicitante", "excl-solicita@empresa.com", PapelUsuario, 0)

	s, err := SolicitarExclusaoConta(db, id)
	if err != nil {
		t.Fatalf("SolicitarExclusaoConta erro inesperado: %v", err)
	}
	if s.ID == "" || s.CriadoEm.IsZero() {
		t.Errorf("ID/CriadoEm não preenchidos: %+v", s)
	}
	if s.Status != "pendente" {
		t.Errorf("Status = %q, want pendente", s.Status)
	}
	if s.ProcessadoEm != nil {
		t.Errorf("ProcessadoEm = %v, want nil", s.ProcessadoEm)
	}

	var n int
	if err := db.QueryRow(
		`SELECT count(*) FROM solicitacoes_exclusao_conta WHERE solicitante_id = $1 AND status = 'pendente'`, id,
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("linhas pendentes = %d, want 1", n)
	}
}

// TestSolicitarExclusaoConta_Duplicata cobre "Usuário já tem solicitação
// pendente": segunda chamada -> ErrExclusaoPendenteExiste, nenhuma linha nova.
func TestSolicitarExclusaoConta_Duplicata(t *testing.T) {
	db := testDB(t)
	id := semearConta(t, db, "Repetido", "excl-repete@empresa.com", PapelUsuario, 0)

	if _, err := SolicitarExclusaoConta(db, id); err != nil {
		t.Fatalf("primeira SolicitarExclusaoConta falhou: %v", err)
	}
	_, err := SolicitarExclusaoConta(db, id)
	if !errors.Is(err, ErrExclusaoPendenteExiste) {
		t.Fatalf("erro = %v, want ErrExclusaoPendenteExiste", err)
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM solicitacoes_exclusao_conta WHERE solicitante_id = $1`, id).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("linhas = %d, want 1", n)
	}
}

// TestSolicitarExclusaoConta_AposProcessada prova que uma solicitação anterior
// já `processada` não bloqueia uma nova (o gate olha só `status='pendente'`).
func TestSolicitarExclusaoConta_AposProcessada(t *testing.T) {
	db := testDB(t)
	solicitante := semearConta(t, db, "Reincidente", "excl-reincide@empresa.com", PapelUsuario, 0)
	adm := semearConta(t, db, "Adm", "excl-adm-reincide@empresa.com", PapelAdm, 1)
	inserirSolicitacaoExclusao(t, db, solicitante, "processada", adm, 0)

	s, err := SolicitarExclusaoConta(db, solicitante)
	if err != nil {
		t.Fatalf("SolicitarExclusaoConta após processada: %v", err)
	}
	if s.Status != "pendente" {
		t.Errorf("Status = %q, want pendente", s.Status)
	}
}

// --- ListarSolicitacoesExclusao ---------------------------------------

// TestListarSolicitacoesExclusao_PendentesOrdenadas cobre "Adm lista
// solicitações": só pendentes, com nome/email/papel do solicitante (JOIN),
// ordenadas por criado_em, id.
func TestListarSolicitacoesExclusao_PendentesOrdenadas(t *testing.T) {
	db := testDB(t)

	u1 := semearConta(t, db, "Ana", "excl-ana@empresa.com", PapelUsuario, 0)
	u2 := semearConta(t, db, "Bruno", "excl-bruno@empresa.com", PapelAlmoxarife, 0)
	u3 := semearConta(t, db, "Carla", "excl-carla@empresa.com", PapelGestor, 0)
	adm := semearConta(t, db, "Adm lista", "excl-adm-lista@empresa.com", PapelAdm, 1)

	// Inseridas fora de ordem; o ORDER BY criado_em deve normalizar.
	inserirSolicitacaoExclusao(t, db, u3, "pendente", "", 30)
	inserirSolicitacaoExclusao(t, db, u1, "pendente", "", 10)
	inserirSolicitacaoExclusao(t, db, u2, "pendente", "", 20)
	// Ruído: uma já processada nunca aparece.
	inserirSolicitacaoExclusao(t, db, adm, "processada", adm, 5)

	lista, err := ListarSolicitacoesExclusao(db)
	if err != nil {
		t.Fatalf("ListarSolicitacoesExclusao erro: %v", err)
	}
	if len(lista) != 3 {
		t.Fatalf("len = %d, want 3 (%+v)", len(lista), lista)
	}
	wantNomes := []string{"Ana", "Bruno", "Carla"}
	wantPapeis := []string{PapelUsuario, PapelAlmoxarife, PapelGestor}
	for i, p := range lista {
		if p.SolicitanteNome != wantNomes[i] {
			t.Errorf("lista[%d].SolicitanteNome = %q, want %q", i, p.SolicitanteNome, wantNomes[i])
		}
		if p.SolicitantePapel != wantPapeis[i] {
			t.Errorf("lista[%d].SolicitantePapel = %q, want %q", i, p.SolicitantePapel, wantPapeis[i])
		}
		if p.SolicitanteEmail == "" || p.ID == "" || p.CriadoEm.IsZero() {
			t.Errorf("lista[%d] campos não resolvidos: %+v", i, p)
		}
	}
}

// TestListarSolicitacoesExclusao_Vazia prova que ausência de pendentes devolve
// slice vazio não-nil, nunca nil/erro.
func TestListarSolicitacoesExclusao_Vazia(t *testing.T) {
	db := testDB(t)
	lista, err := ListarSolicitacoesExclusao(db)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if lista == nil {
		t.Fatal("lista = nil, want slice vazio")
	}
	if len(lista) != 0 {
		t.Fatalf("len = %d, want 0", len(lista))
	}
}

// --- ProcessarExclusaoConta -----------------------------------------

// TestProcessarExclusaoConta_AnonimizaESemMexerNoHistorico cobre "Adm processa
// solicitação de conta comum": usuarios.nome/email anonimizados, credenciais
// zeradas, MFA off, sessões revogadas, solicitação `processada` — e
// movimentacoes/pedidos/logs_acesso do alvo intactos (mesma contagem, mesmo
// usuario_id).
func TestProcessarExclusaoConta_AnonimizaESemMexerNoHistorico(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	alvo := semearConta(t, db, "Conta Comum", "excl-alvo@empresa.com", PapelAlmoxarife, 0)
	adm := semearConta(t, db, "Adm proc", "excl-adm-proc@empresa.com", PapelAdm, 1)
	// Liga MFA na conta alvo para provar que o processamento desliga.
	if _, err := db.Exec(
		`UPDATE usuarios SET mfa_habilitado = true, mfa_secret = 'SEGREDO', email_verificado = true WHERE id = $1`, alvo,
	); err != nil {
		t.Fatalf("preparar MFA do alvo: %v", err)
	}
	semearSessao(t, db, alvo, "refresh-alvo-vivo")

	// Histórico do alvo em cada uma das três tabelas protegidas.
	inserirLogAcessoDireto(t, db, &alvo, "excl-alvo@empresa.com", "senha", true, "10.0.0.9", time.Now())
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Estoque Excl 82", 50)
	if _, err := RegistrarBaixa(db, produtoID, estoqueID, alvo, 3); err != nil {
		t.Fatalf("seed RegistrarBaixa: %v", err)
	}
	seedPedidoComItem(t, db, alvo, "Excl 82", 2)

	movAntes := contarPorUsuario(t, db, "movimentacoes", alvo)
	pedAntes := contarPorUsuario(t, db, "pedidos", alvo)
	logAntes := contarPorUsuario(t, db, "logs_acesso", alvo)
	if movAntes == 0 || pedAntes == 0 || logAntes == 0 {
		t.Fatalf("pré-condição: histórico não semeado (mov=%d ped=%d log=%d)", movAntes, pedAntes, logAntes)
	}

	solID := inserirSolicitacaoExclusao(t, db, alvo, "pendente", "", 0)

	p, err := ProcessarExclusaoConta(db, solID, adm)
	if err != nil {
		t.Fatalf("ProcessarExclusaoConta erro inesperado: %v", err)
	}
	if p.SolicitanteNome != "Conta Comum" || p.SolicitanteEmail != "excl-alvo@empresa.com" {
		t.Errorf("projeção devolvida = %+v, want dados PRÉ-anonimização", p)
	}

	// usuarios: anonimizado + credenciais zeradas, papel intacto.
	var nome, email, papel string
	var senhaHash, mfaSecret sql.NullString
	var ativo, mfaHab, emailVerif bool
	var tentativas int
	var bloqueadoAte sql.NullTime
	if err := db.QueryRow(`
		SELECT nome, email, papel, senha_hash, ativo, mfa_habilitado, mfa_secret, email_verificado, tentativas_login_falhas, bloqueado_ate
		FROM usuarios WHERE id = $1`, alvo).
		Scan(&nome, &email, &papel, &senhaHash, &ativo, &mfaHab, &mfaSecret, &emailVerif, &tentativas, &bloqueadoAte); err != nil {
		t.Fatalf("reler conta alvo: %v", err)
	}
	if nome != "Usuário anonimizado" {
		t.Errorf("nome = %q, want 'Usuário anonimizado'", nome)
	}
	wantEmail := "anonimizado+" + alvo + "@anonimizado.invalido"
	if email != wantEmail {
		t.Errorf("email = %q, want %q", email, wantEmail)
	}
	if senhaHash.Valid {
		t.Errorf("senha_hash = %q, want NULL", senhaHash.String)
	}
	if ativo {
		t.Error("ativo = true, want false")
	}
	if mfaHab || mfaSecret.Valid {
		t.Errorf("MFA não desligado: habilitado=%v secret.Valid=%v", mfaHab, mfaSecret.Valid)
	}
	if emailVerif {
		t.Error("email_verificado = true, want false")
	}
	if tentativas != 0 || bloqueadoAte.Valid {
		t.Errorf("bloqueio não zerado: tentativas=%d bloqueado_ate.Valid=%v", tentativas, bloqueadoAte.Valid)
	}
	if papel != PapelAlmoxarife {
		t.Errorf("papel = %q, want almoxarife (papel NÃO muda)", papel)
	}

	// sessões do alvo revogadas.
	var vivas int
	if err := db.QueryRow(
		`SELECT count(*) FROM sessoes WHERE usuario_id = $1 AND revogado_em IS NULL`, alvo,
	).Scan(&vivas); err != nil {
		t.Fatalf("contar sessões vivas: %v", err)
	}
	if vivas != 0 {
		t.Errorf("sessões vivas = %d, want 0", vivas)
	}

	// solicitação processada + auditoria.
	var status string
	var procPor sql.NullString
	var procEm sql.NullTime
	if err := db.QueryRow(
		`SELECT status, processado_por, processado_em FROM solicitacoes_exclusao_conta WHERE id = $1`, solID,
	).Scan(&status, &procPor, &procEm); err != nil {
		t.Fatalf("reler solicitação: %v", err)
	}
	if status != "processada" || !procPor.Valid || procPor.String != adm || !procEm.Valid {
		t.Errorf("solicitação = status:%q processado_por:%v processado_em.Valid:%v", status, procPor, procEm.Valid)
	}

	// As três tabelas protegidas: mesma contagem, mesmo usuario_id.
	if got := contarPorUsuario(t, db, "movimentacoes", alvo); got != movAntes {
		t.Errorf("movimentacoes do alvo = %d, want %d (intacto)", got, movAntes)
	}
	if got := contarPorUsuario(t, db, "pedidos", alvo); got != pedAntes {
		t.Errorf("pedidos do alvo = %d, want %d (intacto)", got, pedAntes)
	}
	if got := contarPorUsuario(t, db, "logs_acesso", alvo); got != logAntes {
		t.Errorf("logs_acesso do alvo = %d, want %d (intacto)", got, logAntes)
	}
}

// TestProcessarExclusaoConta_LoginPorEmailAntigoFalha cobre as linhas "Login
// por e-mail antigo" e "SSO com o e-mail antigo" da I/O Matrix: depois da
// anonimização, nenhum dos dois caminhos acha a conta pelo e-mail original.
func TestProcessarExclusaoConta_LoginPorEmailAntigoFalha(t *testing.T) {
	db := testDB(t)

	const emailOriginal = "excl-login-antigo@empresa.com"
	alvo := semearConta(t, db, "Some Depois", emailOriginal, PapelUsuario, 0)
	adm := semearConta(t, db, "Adm login", "excl-adm-login@empresa.com", PapelAdm, 1)
	solID := inserirSolicitacaoExclusao(t, db, alvo, "pendente", "", 0)

	if _, err := ProcessarExclusaoConta(db, solID, adm); err != nil {
		t.Fatalf("ProcessarExclusaoConta: %v", err)
	}

	if _, err := Login(db, emailOriginal, "qualquer-senha"); !errors.Is(err, ErrCredenciaisInvalidas) {
		t.Errorf("Login(e-mail antigo) erro = %v, want ErrCredenciaisInvalidas", err)
	}
	if _, err := BuscarUsuarioPorEmailSSO(db, emailOriginal); !errors.Is(err, ErrContaSSONaoEncontrada) {
		t.Errorf("BuscarUsuarioPorEmailSSO(e-mail antigo) erro = %v, want ErrContaSSONaoEncontrada", err)
	}
}

// TestProcessarExclusaoConta_Inexistente cobre "Solicitação inexistente / id
// malformado": uuid sem match e não-UUID caem em
// ErrSolicitacaoExclusaoNaoEncontrada.
func TestProcessarExclusaoConta_Inexistente(t *testing.T) {
	db := testDB(t)
	adm := semearConta(t, db, "Adm 404", "excl-adm-404@empresa.com", PapelAdm, 0)

	if _, err := ProcessarExclusaoConta(db, "00000000-0000-0000-0000-000000000000", adm); !errors.Is(err, ErrSolicitacaoExclusaoNaoEncontrada) {
		t.Errorf("uuid sem match: erro = %v, want ErrSolicitacaoExclusaoNaoEncontrada", err)
	}
	if _, err := ProcessarExclusaoConta(db, "nao-e-uuid", adm); !errors.Is(err, ErrSolicitacaoExclusaoNaoEncontrada) {
		t.Errorf("id malformado: erro = %v, want ErrSolicitacaoExclusaoNaoEncontrada", err)
	}
}

// TestProcessarExclusaoConta_JaProcessada cobre "Solicitação já processada
// (reuso/corrida)": segundo processamento -> ErrSolicitacaoExclusaoNaoPendente,
// nenhuma escrita nova.
func TestProcessarExclusaoConta_JaProcessada(t *testing.T) {
	db := testDB(t)
	alvo := semearConta(t, db, "Duas vezes", "excl-2x@empresa.com", PapelUsuario, 0)
	adm := semearConta(t, db, "Adm 2x", "excl-adm-2x@empresa.com", PapelAdm, 1)
	solID := inserirSolicitacaoExclusao(t, db, alvo, "pendente", "", 0)

	if _, err := ProcessarExclusaoConta(db, solID, adm); err != nil {
		t.Fatalf("primeiro processamento falhou: %v", err)
	}
	if _, err := ProcessarExclusaoConta(db, solID, adm); !errors.Is(err, ErrSolicitacaoExclusaoNaoPendente) {
		t.Fatalf("segundo processamento: erro = %v, want ErrSolicitacaoExclusaoNaoPendente", err)
	}
}

// TestProcessarExclusaoConta_UltimoAdmBloqueado cobre "Processar deixaria zero
// adm ativo": alvo `adm` sem nenhum outro `adm` ativo -> ErrUltimoAdmAtivo,
// NENHUMA escrita (conta e solicitação intactas).
func TestProcessarExclusaoConta_UltimoAdmBloqueado(t *testing.T) {
	db := testDB(t)
	adm := semearConta(t, db, "Único Adm", "excl-unico-adm@empresa.com", PapelAdm, 0)
	solID := inserirSolicitacaoExclusao(t, db, adm, "pendente", "", 0)

	_, err := ProcessarExclusaoConta(db, solID, adm)
	if !errors.Is(err, ErrUltimoAdmAtivo) {
		t.Fatalf("erro = %v, want ErrUltimoAdmAtivo", err)
	}

	var nome, email, status string
	var ativo bool
	if err := db.QueryRow(
		`SELECT u.nome, u.email, u.ativo, s.status
		 FROM usuarios u JOIN solicitacoes_exclusao_conta s ON s.solicitante_id = u.id
		 WHERE u.id = $1`, adm,
	).Scan(&nome, &email, &ativo, &status); err != nil {
		t.Fatalf("reler estado: %v", err)
	}
	if nome != "Único Adm" || email != "excl-unico-adm@empresa.com" || !ativo {
		t.Errorf("conta adm foi tocada: nome=%q email=%q ativo=%v", nome, email, ativo)
	}
	if status != "pendente" {
		t.Errorf("solicitação status = %q, want pendente (intacta)", status)
	}
}

// TestProcessarExclusaoConta_AdmComOutroAdmAtivoProssegue cobre "adm com outro
// adm ativo prossegue": com a unicidade de `adm` relaxada (idx_usuarios_unico_adm
// removido só neste teste), processar um `adm` alvo quando sobra outro `adm`
// ativo é permitido e anonimiza normalmente.
func TestProcessarExclusaoConta_AdmComOutroAdmAtivoProssegue(t *testing.T) {
	db := testDB(t)

	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_usuarios_unico_adm`); err != nil {
		t.Fatalf("relaxar unicidade de adm: %v", err)
	}
	t.Cleanup(func() {
		// A conta alvo continua com papel='adm' (o papel NÃO muda na
		// anonimização), então recriar o índice único parcial exige limpar as
		// linhas antes. O próximo teste refaz o TRUNCATE de qualquer forma.
		if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
			t.Fatalf("cleanup TRUNCATE usuarios: %v", err)
		}
		if _, err := db.Exec(
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_usuarios_unico_adm ON usuarios (papel) WHERE papel = 'adm'`,
		); err != nil {
			t.Fatalf("cleanup recriar idx_usuarios_unico_adm: %v", err)
		}
	})

	alvo := semearConta(t, db, "Adm Alvo", "excl-adm-alvo@empresa.com", PapelAdm, 0)
	outro := semearConta(t, db, "Adm Sobrevivente", "excl-adm-sobrevive@empresa.com", PapelAdm, 1)
	solID := inserirSolicitacaoExclusao(t, db, alvo, "pendente", "", 0)

	if _, err := ProcessarExclusaoConta(db, solID, outro); err != nil {
		t.Fatalf("ProcessarExclusaoConta erro inesperado: %v", err)
	}

	var nome string
	var ativo bool
	if err := db.QueryRow(`SELECT nome, ativo FROM usuarios WHERE id = $1`, alvo).Scan(&nome, &ativo); err != nil {
		t.Fatalf("reler alvo: %v", err)
	}
	if nome != "Usuário anonimizado" || ativo {
		t.Errorf("alvo não anonimizado: nome=%q ativo=%v", nome, ativo)
	}
}

// TestProcessarExclusaoConta_ErroDeBancoNaoAnonimizaParcial cobre "Falha de
// banco em qualquer passo": com a conexão fechada, o processamento devolve
// erro e nada é anonimizado (transação revertida).
func TestProcessarExclusaoConta_ErroDeBancoNaoAnonimizaParcial(t *testing.T) {
	db := testDB(t)
	alvo := semearConta(t, db, "Sem Erro", "excl-erro-banco@empresa.com", PapelUsuario, 0)
	adm := semearConta(t, db, "Adm erro", "excl-adm-erro@empresa.com", PapelAdm, 1)
	solID := inserirSolicitacaoExclusao(t, db, alvo, "pendente", "", 0)

	db2, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("abrir conexão auxiliar: %v", err)
	}
	db2.Close()

	if _, err := ProcessarExclusaoConta(db2, solID, adm); err == nil {
		t.Fatal("erro esperado com a conexão fechada")
	}

	var nome, status string
	if err := db.QueryRow(
		`SELECT u.nome, s.status FROM usuarios u
		 JOIN solicitacoes_exclusao_conta s ON s.solicitante_id = u.id WHERE u.id = $1`, alvo,
	).Scan(&nome, &status); err != nil {
		t.Fatalf("reler estado: %v", err)
	}
	if nome != "Sem Erro" || status != "pendente" {
		t.Errorf("anonimização parcial: nome=%q status=%q", nome, status)
	}
}
