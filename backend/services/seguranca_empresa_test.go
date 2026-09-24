package services

import (
	"database/sql"
	"encoding/json"
	"testing"
)

// Story 14.3: exigência de MFA alterada pelo `adm`, com auditoria.

// inserirContaSeguranca cria uma conta com controle direto dos campos que
// ContarContasSemMFA lê. senhaHash vazio grava NULL (conta só-SSO).
func inserirContaSeguranca(t *testing.T, db *sql.DB, empresaID, email, papel string, ativo, mfa bool, senhaHash string) string {
	t.Helper()
	var hash sql.NullString
	if senhaHash != "" {
		hash = sql.NullString{String: senhaHash, Valid: true}
	}
	var id string
	const q = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, mfa_habilitado, mfa_secret, empresa_id)
		VALUES ($1, $2, $3, $4, true, $5, $6, CASE WHEN $6 THEN 'SEGREDO' ELSE NULL END, $7)
		RETURNING id`
	if err := db.QueryRow(q, "Conta "+email, email, hash, papel, ativo, mfa, empresaID).Scan(&id); err != nil {
		t.Fatalf("inserir conta %q: %v", email, err)
	}
	return id
}

// restaurarExigenciaEmpresaTeste garante que a Empresa padrão (compartilhada
// com a suíte de handlers) parte de `false` e volta a `false` no fim do
// teste, sem deixar linhas de auditoria para trás.
func restaurarExigenciaEmpresaTeste(t *testing.T, db *sql.DB) {
	t.Helper()
	limpar := func() {
		_, _ = db.Exec(`DELETE FROM auditoria_seguranca WHERE empresa_id = $1`, empresaTeste)
		_, _ = db.Exec(`UPDATE empresas SET mfa_obrigatorio = false WHERE id = $1`, empresaTeste)
	}
	limpar()
	t.Cleanup(limpar)
}

// criarOutraEmpresaSeguranca provisiona uma segunda Empresa e registra a
// remoção dela (com contas e auditoria) no fim do teste.
func criarOutraEmpresaSeguranca(t *testing.T, db *sql.DB, slug, cnpjBase12 string) Empresa {
	t.Helper()
	remover := func() {
		var id string
		if err := db.QueryRow(`SELECT id FROM empresas WHERE slug = $1`, slug).Scan(&id); err != nil {
			return
		}
		_, _ = db.Exec(`DELETE FROM auditoria_seguranca WHERE empresa_id = $1`, id)
		_, _ = db.Exec(`DELETE FROM usuarios WHERE empresa_id = $1`, id)
		removerEmpresaDeTeste(t, db, slug)
	}
	remover()
	t.Cleanup(remover)
	return criarEmpresaDeTeste(t, db, slug, cnpjBase12, "Seguranca "+slug)
}

func lerExigencia(t *testing.T, db *sql.DB, empresaID string) bool {
	t.Helper()
	var v bool
	if err := db.QueryRow(`SELECT mfa_obrigatorio FROM empresas WHERE id = $1`, empresaID).Scan(&v); err != nil {
		t.Fatalf("ler mfa_obrigatorio: %v", err)
	}
	return v
}

func contarAuditoria(t *testing.T, db *sql.DB, empresaID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM auditoria_seguranca WHERE empresa_id = $1`, empresaID).Scan(&n); err != nil {
		t.Fatalf("contar auditoria_seguranca: %v", err)
	}
	return n
}

func TestContarContasSemMFA(t *testing.T) {
	db := testDB(t)
	outra := criarOutraEmpresaSeguranca(t, db, "seg-contagem-outra", "887766550001")

	inserirContaSeguranca(t, db, empresaTeste, "cont-gestor-sem@seg.com", PapelGestor, true, false, "hash")
	inserirContaSeguranca(t, db, empresaTeste, "cont-adm-sem@seg.com", PapelAdm, true, false, "hash")
	inserirContaSeguranca(t, db, empresaTeste, "cont-gestor-com@seg.com", PapelGestor, true, true, "hash")
	inserirContaSeguranca(t, db, empresaTeste, "cont-gestor-inativo@seg.com", PapelGestor, false, false, "hash")
	inserirContaSeguranca(t, db, empresaTeste, "cont-gestor-sso@seg.com", PapelGestor, true, false, "")
	inserirContaSeguranca(t, db, empresaTeste, "cont-almox-sem@seg.com", PapelAlmoxarife, true, false, "hash")
	inserirContaSeguranca(t, db, outra.ID, "cont-gestor-outra@seg.com", PapelGestor, true, false, "hash")

	n, err := ContarContasSemMFA(db, empresaTeste)
	if err != nil {
		t.Fatalf("ContarContasSemMFA: %v", err)
	}
	if n != 2 {
		t.Errorf("contasSemMfa = %d, want 2", n)
	}
}

func TestAlterarExigenciaMFA_LigarDesligarIdempotente(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	adm := inserirContaSeguranca(t, db, empresaTeste, "alt-adm@seg.com", PapelAdm, true, true, "hash")
	gestor := inserirContaSeguranca(t, db, empresaTeste, "alt-gestor@seg.com", PapelGestor, true, true, "hash")
	if _, err := db.Exec(`UPDATE usuarios SET mfa_ultimo_passo_usado = 42 WHERE id = $1`, gestor); err != nil {
		t.Fatalf("seed mfa_ultimo_passo_usado: %v", err)
	}

	type detalhe struct {
		Anterior bool `json:"anterior"`
		Novo     bool `json:"novo"`
	}
	ultimo := func() (acao, ator string, alvo sql.NullString, d detalhe) {
		t.Helper()
		var raw []byte
		err := db.QueryRow(`
			SELECT acao, ator_id, alvo_id, detalhe FROM auditoria_seguranca
			WHERE empresa_id = $1 ORDER BY criado_em DESC, id DESC LIMIT 1`, empresaTeste).Scan(&acao, &ator, &alvo, &raw)
		if err != nil {
			t.Fatalf("ler última auditoria: %v", err)
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatalf("decodificar detalhe %s: %v", raw, err)
		}
		return
	}

	// Ligar.
	alterado, err := AlterarExigenciaMFA(db, empresaTeste, adm, true)
	if err != nil || !alterado {
		t.Fatalf("ligar: alterado=%v err=%v, want true,nil", alterado, err)
	}
	if !lerExigencia(t, db, empresaTeste) {
		t.Error("ligar: mfa_obrigatorio = false, want true")
	}
	if n := contarAuditoria(t, db, empresaTeste); n != 1 {
		t.Fatalf("ligar: auditoria = %d, want 1", n)
	}
	acao, ator, alvo, d := ultimo()
	if acao != AcaoExigenciaAlterada || ator != adm || alvo.Valid || d != (detalhe{Anterior: false, Novo: true}) {
		t.Errorf("ligar: linha = %q %q %v %+v", acao, ator, alvo, d)
	}

	// Idempotente.
	alterado, err = AlterarExigenciaMFA(db, empresaTeste, adm, true)
	if err != nil || alterado {
		t.Fatalf("reenvio: alterado=%v err=%v, want false,nil", alterado, err)
	}
	if n := contarAuditoria(t, db, empresaTeste); n != 1 {
		t.Errorf("reenvio: auditoria = %d, want 1", n)
	}

	// Desligar preserva o MFA das contas.
	var secretAntes string
	if err := db.QueryRow(`SELECT mfa_secret FROM usuarios WHERE id = $1`, gestor).Scan(&secretAntes); err != nil {
		t.Fatalf("ler mfa_secret: %v", err)
	}
	alterado, err = AlterarExigenciaMFA(db, empresaTeste, adm, false)
	if err != nil || !alterado {
		t.Fatalf("desligar: alterado=%v err=%v, want true,nil", alterado, err)
	}
	if lerExigencia(t, db, empresaTeste) {
		t.Error("desligar: mfa_obrigatorio = true, want false")
	}
	if n := contarAuditoria(t, db, empresaTeste); n != 2 {
		t.Fatalf("desligar: auditoria = %d, want 2", n)
	}
	if _, _, _, d := ultimo(); d != (detalhe{Anterior: true, Novo: false}) {
		t.Errorf("desligar: detalhe = %+v", d)
	}
	var mfa bool
	var secret string
	var passo sql.NullInt64
	if err := db.QueryRow(`SELECT mfa_habilitado, mfa_secret, mfa_ultimo_passo_usado FROM usuarios WHERE id = $1`, gestor).Scan(&mfa, &secret, &passo); err != nil {
		t.Fatalf("ler MFA do gestor: %v", err)
	}
	if !mfa || secret != secretAntes || !passo.Valid || passo.Int64 != 42 {
		t.Errorf("desligar tocou o MFA do gestor: habilitado=%v secret=%q passo=%v", mfa, secret, passo)
	}
}

func TestListarAuditoriaSeguranca_EscopoEOrdem(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	outra := criarOutraEmpresaSeguranca(t, db, "seg-auditoria-outra", "887766550002")

	adm := inserirContaSeguranca(t, db, empresaTeste, "aud-adm@seg.com", PapelAdm, true, true, "hash")
	admOutra := inserirContaSeguranca(t, db, outra.ID, "aud-adm-outra@seg.com", PapelAdm, true, true, "hash")

	if _, err := AlterarExigenciaMFA(db, empresaTeste, adm, true); err != nil {
		t.Fatalf("ligar: %v", err)
	}
	if _, err := AlterarExigenciaMFA(db, outra.ID, admOutra, true); err != nil {
		t.Fatalf("ligar outra: %v", err)
	}
	if _, err := AlterarExigenciaMFA(db, empresaTeste, adm, false); err != nil {
		t.Fatalf("desligar: %v", err)
	}

	eventos, err := ListarAuditoriaSeguranca(db, empresaTeste)
	if err != nil {
		t.Fatalf("ListarAuditoriaSeguranca: %v", err)
	}
	if len(eventos) != 2 {
		t.Fatalf("len(eventos) = %d, want 2", len(eventos))
	}
	var primeiro struct{ Novo bool }
	if err := json.Unmarshal(eventos[0].Detalhe, &primeiro); err != nil {
		t.Fatalf("detalhe: %v", err)
	}
	if primeiro.Novo {
		t.Error("eventos[0] deveria ser o desligamento (mais recente primeiro)")
	}
	for i, e := range eventos {
		if e.AtorID != adm || e.AtorNome == nil || *e.AtorNome != "Conta aud-adm@seg.com" {
			t.Errorf("eventos[%d]: ator = %q / %v", i, e.AtorID, e.AtorNome)
		}
		if e.AlvoID != nil || e.AlvoNome != nil {
			t.Errorf("eventos[%d]: alvo deveria ser nil", i)
		}
		if e.Acao != AcaoExigenciaAlterada {
			t.Errorf("eventos[%d]: acao = %q", i, e.Acao)
		}
	}
	if eventos[0].CriadoEm.Before(eventos[1].CriadoEm) {
		t.Error("eventos fora de ordem criado_em DESC")
	}

	daOutra, err := ListarAuditoriaSeguranca(db, outra.ID)
	if err != nil {
		t.Fatalf("ListarAuditoriaSeguranca outra: %v", err)
	}
	if len(daOutra) != 1 || daOutra[0].AtorID != admOutra {
		t.Errorf("outra empresa: %+v", daOutra)
	}
}
