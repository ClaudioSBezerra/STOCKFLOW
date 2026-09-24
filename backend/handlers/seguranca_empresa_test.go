package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// ===== Story 14.3: o adm altera a exigência de MFA, com aviso e auditoria =====

// prepararSegurancaEmpresaTeste parte a Empresa padrão (compartilhada) de
// `false` sem auditoria e registra a mesma restauração no fim do teste.
func prepararSegurancaEmpresaTeste(t *testing.T, db *sql.DB) {
	t.Helper()
	limpar := func() {
		_, _ = db.Exec(`DELETE FROM auditoria_seguranca WHERE empresa_id = $1`, empresaTeste)
		_, _ = db.Exec(`UPDATE empresas SET mfa_obrigatorio = false WHERE id = $1`, empresaTeste)
	}
	limpar()
	t.Cleanup(limpar)
}

// rotaSeguranca despacha pela MESMA composição de newMux:
// RequireEmpresa -> RequireAuth -> RequireRole(adm) -> handler.
func rotaSeguranca(db *sql.DB, metodo, caminho, corpo, authHeader string, h http.HandlerFunc) *httptest.ResponseRecorder {
	req := httptest.NewRequest(metodo, prefixoEmpresaTeste+caminho, strings.NewReader(corpo))
	if corpo != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	comEmpresa(db, middleware.RequireAuth(db, testJWTSecret)(
		middleware.RequireRole(services.PapelAdm)(h)))(w, req)
	return w
}

func getExigenciaMFA(db *sql.DB, auth string) *httptest.ResponseRecorder {
	return rotaSeguranca(db, http.MethodGet, "/api/seguranca/mfa-empresa", "", auth, ObterExigenciaMFAHandler(db))
}

func putExigenciaMFA(db *sql.DB, auth, corpo string) *httptest.ResponseRecorder {
	return rotaSeguranca(db, http.MethodPut, "/api/seguranca/mfa-empresa", corpo, auth, AlterarExigenciaMFAHandler(db))
}

func getAuditoriaSeguranca(db *sql.DB, auth string) *httptest.ResponseRecorder {
	return rotaSeguranca(db, http.MethodGet, "/api/seguranca/auditoria", "", auth, ListarAuditoriaSegurancaHandler(db))
}

// contaSenhaSemMFA cria uma conta por senha, sem MFA, no papel pedido, e
// devolve id e access token (origem=senha).
func contaSenhaSemMFA(t *testing.T, db *sql.DB, email, papel string) (string, string) {
	t.Helper()
	id := criarUsuarioLogin(t, db, email, "senha-123456")
	if _, err := db.Exec(`UPDATE usuarios SET papel = $1 WHERE id = $2`, papel, id); err != nil {
		t.Fatalf("definir papel %s: %v", papel, err)
	}
	return id, tokenDeLogin(t, db, email, "senha-123456")
}

func exigenciaNoBanco(t *testing.T, db *sql.DB, empresaID string) bool {
	t.Helper()
	var v bool
	if err := db.QueryRow(`SELECT mfa_obrigatorio FROM empresas WHERE id = $1`, empresaID).Scan(&v); err != nil {
		t.Fatalf("ler mfa_obrigatorio: %v", err)
	}
	return v
}

func contarAuditoriaSeguranca(t *testing.T, db *sql.DB, empresaID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM auditoria_seguranca WHERE empresa_id = $1`, empresaID).Scan(&n); err != nil {
		t.Fatalf("contar auditoria_seguranca: %v", err)
	}
	return n
}

type respostaExigenciaMFA struct {
	MfaObrigatorio *bool `json:"mfaObrigatorio"`
	Alterado       *bool `json:"alterado"`
	ContasSemMfa   *int  `json:"contasSemMfa"`
}

func decodeExigencia(t *testing.T, w *httptest.ResponseRecorder) respostaExigenciaMFA {
	t.Helper()
	var r respostaExigenciaMFA
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatalf("decodificar resposta: %v (body=%s)", err, w.Body.String())
	}
	return r
}

// TestExigenciaMFA_LigarIdempotenteDesligar cobre as linhas Ligar,
// Idempotente e Desligar da matriz na fronteira HTTP.
func TestExigenciaMFA_LigarIdempotenteDesligar(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	admID := criarContaComPapel(t, db, "Adm Seg", "seg-adm@empresa.com", "senha-123456", "adm")
	token := "Bearer " + tokenDeLogin(t, db, "seg-adm@empresa.com", "senha-123456")
	gestorID := criarContaComPapel(t, db, "Gestor Seg", "seg-gestor@empresa.com", "senha-123456", "gestor")
	var secretAntes string
	if err := db.QueryRow(`SELECT mfa_secret FROM usuarios WHERE id = $1`, gestorID).Scan(&secretAntes); err != nil {
		t.Fatalf("ler mfa_secret: %v", err)
	}

	// GET inicial.
	w := getExigenciaMFA(db, token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET: status = %d (body=%s)", w.Code, w.Body.String())
	}
	if r := decodeExigencia(t, w); r.MfaObrigatorio == nil || *r.MfaObrigatorio || r.ContasSemMfa == nil || *r.ContasSemMfa != 0 {
		t.Errorf("GET inicial = %s, want mfaObrigatorio:false contasSemMfa:0", w.Body.String())
	}

	// Ligar.
	w = putExigenciaMFA(db, token, `{"mfaObrigatorio":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("ligar: status = %d (body=%s)", w.Code, w.Body.String())
	}
	if r := decodeExigencia(t, w); r.MfaObrigatorio == nil || !*r.MfaObrigatorio || r.Alterado == nil || !*r.Alterado {
		t.Errorf("ligar: body = %s", w.Body.String())
	}
	if !exigenciaNoBanco(t, db, empresaTeste) {
		t.Error("ligar: banco = false")
	}
	if n := contarAuditoriaSeguranca(t, db, empresaTeste); n != 1 {
		t.Fatalf("ligar: auditoria = %d, want 1", n)
	}
	var ator, acao string
	var detalhe []byte
	if err := db.QueryRow(`SELECT ator_id, acao, detalhe FROM auditoria_seguranca WHERE empresa_id = $1`, empresaTeste).Scan(&ator, &acao, &detalhe); err != nil {
		t.Fatalf("ler auditoria: %v", err)
	}
	var d map[string]bool
	_ = json.Unmarshal(detalhe, &d)
	if ator != admID || acao != "exigencia_alterada" || d["anterior"] || !d["novo"] || len(d) != 2 {
		t.Errorf("ligar: auditoria = %s %s %s", ator, acao, detalhe)
	}

	// Idempotente.
	w = putExigenciaMFA(db, token, `{"mfaObrigatorio":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("reenvio: status = %d (body=%s)", w.Code, w.Body.String())
	}
	if r := decodeExigencia(t, w); r.MfaObrigatorio == nil || !*r.MfaObrigatorio || r.Alterado == nil || *r.Alterado {
		t.Errorf("reenvio: body = %s, want alterado:false", w.Body.String())
	}
	if n := contarAuditoriaSeguranca(t, db, empresaTeste); n != 1 {
		t.Errorf("reenvio: auditoria = %d, want 1", n)
	}

	// Desligar: gestor mantém o MFA.
	w = putExigenciaMFA(db, token, `{"mfaObrigatorio":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("desligar: status = %d (body=%s)", w.Code, w.Body.String())
	}
	if r := decodeExigencia(t, w); r.MfaObrigatorio == nil || *r.MfaObrigatorio || r.Alterado == nil || !*r.Alterado {
		t.Errorf("desligar: body = %s", w.Body.String())
	}
	if exigenciaNoBanco(t, db, empresaTeste) {
		t.Error("desligar: banco = true")
	}
	if n := contarAuditoriaSeguranca(t, db, empresaTeste); n != 2 {
		t.Errorf("desligar: auditoria = %d, want 2", n)
	}
	var mfa bool
	var secret string
	if err := db.QueryRow(`SELECT mfa_habilitado, mfa_secret FROM usuarios WHERE id = $1`, gestorID).Scan(&mfa, &secret); err != nil {
		t.Fatalf("ler MFA do gestor: %v", err)
	}
	if !mfa || secret != secretAntes {
		t.Errorf("desligar tocou o MFA do gestor: habilitado=%v secretIgual=%v", mfa, secret == secretAntes)
	}

	// Histórico: 2 eventos, mais recente primeiro, com atorNome e chaves do contrato.
	w = getAuditoriaSeguranca(db, token)
	if w.Code != http.StatusOK {
		t.Fatalf("auditoria: status = %d (body=%s)", w.Code, w.Body.String())
	}
	var cru struct {
		Eventos []map[string]any `json:"eventos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cru); err != nil {
		t.Fatalf("decode auditoria: %v", err)
	}
	if len(cru.Eventos) != 2 {
		t.Fatalf("len(eventos) = %d, want 2", len(cru.Eventos))
	}
	for _, chave := range []string{"id", "acao", "atorId", "atorNome", "alvoId", "alvoNome", "detalhe", "criadoEm"} {
		if _, ok := cru.Eventos[0][chave]; !ok {
			t.Errorf("evento sem a chave %q: %v", chave, cru.Eventos[0])
		}
	}
	if cru.Eventos[0]["atorNome"] != "Adm Seg" {
		t.Errorf("atorNome = %v", cru.Eventos[0]["atorNome"])
	}
	if det, _ := cru.Eventos[0]["detalhe"].(map[string]any); det["novo"] != false || det["anterior"] != true {
		t.Errorf("eventos[0].detalhe = %v, want o desligamento", cru.Eventos[0]["detalhe"])
	}
}

// TestExigenciaMFA_ContagemEEfeitoDoGate: o GET conta as contas que o gate
// bloquearia; depois de ligar, um gestor por senha sem MFA recebe 403
// MFA_SETUP_REQUIRED numa rota RequireRole(gestor), sem novo login.
func TestExigenciaMFA_ContagemEEfeitoDoGate(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	criarContaComPapel(t, db, "Adm Gate", "seg-gate-adm@empresa.com", "senha-123456", "adm")
	token := "Bearer " + tokenDeLogin(t, db, "seg-gate-adm@empresa.com", "senha-123456")
	_, tokenGestor := contaSenhaSemMFA(t, db, "seg-gate-gestor@empresa.com", "gestor")

	w := getExigenciaMFA(db, token)
	if r := decodeExigencia(t, w); w.Code != http.StatusOK || r.ContasSemMfa == nil || *r.ContasSemMfa != 1 {
		t.Fatalf("GET: status = %d body = %s, want contasSemMfa:1", w.Code, w.Body.String())
	}

	rotaGestor := comEmpresa(db, middleware.RequireAuth(db, testJWTSecret)(middleware.RequireRole(services.PapelGestor)(
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })))
	chamarGestor := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, prefixoEmpresaTeste+"/api/usuarios", nil)
		req.Header.Set("Authorization", "Bearer "+tokenGestor)
		w := httptest.NewRecorder()
		rotaGestor(w, req)
		return w
	}
	if w := chamarGestor(); w.Code != http.StatusOK {
		t.Fatalf("antes de ligar: status = %d, want 200", w.Code)
	}

	if w := putExigenciaMFA(db, token, `{"mfaObrigatorio":true}`); w.Code != http.StatusOK {
		t.Fatalf("ligar: status = %d (body=%s)", w.Code, w.Body.String())
	}
	w = chamarGestor()
	if w.Code != http.StatusForbidden {
		t.Fatalf("depois de ligar: status = %d, want 403", w.Code)
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "MFA_SETUP_REQUIRED" {
		t.Errorf("code = %q, want MFA_SETUP_REQUIRED", env.Error.Code)
	}
}

// TestExigenciaMFA_PapelInsuficiente: gestor recebe 403 FORBIDDEN nas 3 rotas
// e nada muda.
func TestExigenciaMFA_PapelInsuficiente(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	criarContaComPapel(t, db, "Gestor Papel", "seg-papel-gestor@empresa.com", "senha-123456", "gestor")
	token := "Bearer " + tokenDeLogin(t, db, "seg-papel-gestor@empresa.com", "senha-123456")

	for nome, w := range map[string]*httptest.ResponseRecorder{
		"GET mfa-empresa": getExigenciaMFA(db, token),
		"PUT mfa-empresa": putExigenciaMFA(db, token, `{"mfaObrigatorio":true}`),
		"GET auditoria":   getAuditoriaSeguranca(db, token),
	} {
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", nome, w.Code)
			continue
		}
		if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "FORBIDDEN" {
			t.Errorf("%s: code = %q, want FORBIDDEN", nome, env.Error.Code)
		}
	}
	if exigenciaNoBanco(t, db, empresaTeste) || contarAuditoriaSeguranca(t, db, empresaTeste) != 0 {
		t.Error("banco alterado por um gestor")
	}
}

// TestExigenciaMFA_AdmBloqueado: adm por senha sem MFA numa Empresa que exige
// recebe 403 MFA_SETUP_REQUIRED do gate, e o banco fica inalterado.
func TestExigenciaMFA_AdmBloqueado(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	_, token := contaSenhaSemMFA(t, db, "seg-adm-sem-mfa@empresa.com", "adm")
	if _, err := db.Exec(`UPDATE empresas SET mfa_obrigatorio = true WHERE id = $1`, empresaTeste); err != nil {
		t.Fatalf("ligar flag: %v", err)
	}

	w := putExigenciaMFA(db, "Bearer "+token, `{"mfaObrigatorio":false}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "MFA_SETUP_REQUIRED" {
		t.Errorf("code = %q, want MFA_SETUP_REQUIRED", env.Error.Code)
	}
	if !exigenciaNoBanco(t, db, empresaTeste) || contarAuditoriaSeguranca(t, db, empresaTeste) != 0 {
		t.Error("banco alterado apesar do 403")
	}
}

// TestExigenciaMFA_CorpoInvalido: {}, null e não-booleano -> 400, nada gravado.
func TestExigenciaMFA_CorpoInvalido(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	criarContaComPapel(t, db, "Adm Corpo", "seg-corpo-adm@empresa.com", "senha-123456", "adm")
	token := "Bearer " + tokenDeLogin(t, db, "seg-corpo-adm@empresa.com", "senha-123456")

	for _, corpo := range []string{`{}`, `{"mfaObrigatorio":null}`, `{"mfaObrigatorio":"sim"}`, `nao-json`, `{"mfaObrigatorio":true,"x":"` + strings.Repeat("a", 5000) + `"}`} {
		w := putExigenciaMFA(db, token, corpo)
		if w.Code != http.StatusBadRequest {
			t.Errorf("corpo %.40q: status = %d, want 400", corpo, w.Code)
			continue
		}
		if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
			t.Errorf("corpo %.40q: code = %q", corpo, env.Error.Code)
		}
	}
	if exigenciaNoBanco(t, db, empresaTeste) || contarAuditoriaSeguranca(t, db, empresaTeste) != 0 {
		t.Error("banco alterado por corpo inválido")
	}
}

// TestExigenciaMFA_AuditoriaEscopadaPorEmpresa: o adm da Empresa A só vê as
// linhas da A.
func TestExigenciaMFA_AuditoriaEscopadaPorEmpresa(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)

	const slugOutra = "seg-auditoria-outra-h"
	removerEmpresaPlataformaHandlers(t, db, slugOutra)
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	outra, err := services.ProvisionarEmpresa(tx, services.DadosEmpresa{
		NomeFantasia: "Seg Outra", RazaoSocial: "Seg Outra LTDA", CNPJ: "66554433000105", Slug: slugOutra,
		Endereco: services.EnderecoEmpresa{Logradouro: "Rua", Numero: "1", Bairro: "Centro", Cidade: "Recife", CEP: "50000000", UF: "PE"},
	})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("provisionar outra empresa: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	t.Cleanup(func() { removerEmpresaPlataformaHandlers(t, db, slugOutra) })

	var admOutra string
	if err := db.QueryRow(`
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, empresa_id)
		VALUES ('Adm Outra', 'seg-adm-outra@empresa.com', 'hash', 'adm', true, true, $1) RETURNING id`, outra.ID).Scan(&admOutra); err != nil {
		t.Fatalf("criar adm da outra: %v", err)
	}
	if _, err := services.AlterarExigenciaMFA(db, outra.ID, admOutra, true); err != nil {
		t.Fatalf("auditoria da outra: %v", err)
	}

	admID := criarContaComPapel(t, db, "Adm Escopo", "seg-escopo-adm@empresa.com", "senha-123456", "adm")
	token := "Bearer " + tokenDeLogin(t, db, "seg-escopo-adm@empresa.com", "senha-123456")
	if w := putExigenciaMFA(db, token, `{"mfaObrigatorio":true}`); w.Code != http.StatusOK {
		t.Fatalf("ligar: %d", w.Code)
	}
	if w := putExigenciaMFA(db, token, `{"mfaObrigatorio":false}`); w.Code != http.StatusOK {
		t.Fatalf("desligar: %d", w.Code)
	}

	w := getAuditoriaSeguranca(db, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Eventos []services.EventoAuditoriaSeguranca `json:"eventos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Eventos) != 2 {
		t.Fatalf("len(eventos) = %d, want 2 (body=%s)", len(resp.Eventos), w.Body.String())
	}
	for i, e := range resp.Eventos {
		if e.AtorID != admID || e.AtorNome == nil || *e.AtorNome != "Adm Escopo" {
			t.Errorf("eventos[%d] de outro ator: %+v", i, e)
		}
	}
	if resp.Eventos[0].CriadoEm.Before(resp.Eventos[1].CriadoEm) {
		t.Error("eventos fora de ordem DESC")
	}
}

// TestExigenciaMFA_SemToken: 401 nas 3 rotas.
func TestExigenciaMFA_SemToken(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	for nome, w := range map[string]*httptest.ResponseRecorder{
		"GET mfa-empresa": getExigenciaMFA(db, ""),
		"PUT mfa-empresa": putExigenciaMFA(db, "", `{"mfaObrigatorio":true}`),
		"GET auditoria":   getAuditoriaSeguranca(db, ""),
	} {
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", nome, w.Code)
		}
	}
}
