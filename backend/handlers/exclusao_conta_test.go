// Testes HTTP de SolicitarExclusaoContaHandler /
// ListarSolicitacoesExclusaoHandler / ProcessarExclusaoContaHandler — Story
// 8.2 (Epic 8, Privacidade/LGPD), spec-8-2. Despacham pela MESMA composição
// registrada em newMux (main.go): a rota `me` só atrás de RequireAuth; as
// duas rotas `adm` atrás de RequireAuth + RequireRole(adm).
package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

func postSolicitacaoExclusao(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/usuarios/me/solicitacao-exclusao", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	middleware.RequireAuth(db, testJWTSecret)(SolicitarExclusaoContaHandler(db))(w, req)
	return w
}

func getSolicitacoesExclusao(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/solicitacoes-exclusao", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	middleware.RequireAuth(db, testJWTSecret)(
		middleware.RequireRole(services.PapelAdm)(
			ListarSolicitacoesExclusaoHandler(db)))(w, req)
	return w
}

// postProcessamentoExclusao usa um http.ServeMux local com o MESMO padrão de
// rota de newMux para exercitar a extração de `r.PathValue("id")` além da
// composição RequireAuth -> RequireRole(adm) -> handler.
func postProcessamentoExclusao(db *sql.DB, id, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/solicitacoes-exclusao/{id}/processamento",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAdm)(
				ProcessarExclusaoContaHandler(db))))
	r := httptest.NewRequest(http.MethodPost, "/api/solicitacoes-exclusao/"+id+"/processamento", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

type solicitacaoExclusaoBody struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	CriadoEm string `json:"criadoEm"`
	Nome     string `json:"nome"`
	Email    string `json:"email"`
	Papel    string `json:"papel"`
}

type listaExclusaoBody struct {
	Solicitacoes []solicitacaoExclusaoBody `json:"solicitacoes"`
}

// criarPendenteExclusao registra, via o handler real da rota `me`, uma
// solicitação de exclusão pendente para a conta autenticada por `token` e
// devolve o id gerado.
func criarPendenteExclusao(t *testing.T, db *sql.DB, token string) string {
	t.Helper()
	w := postSolicitacaoExclusao(db, "Bearer "+token)
	if w.Code != http.StatusCreated {
		t.Fatalf("criarPendenteExclusao: status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var b solicitacaoExclusaoBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil || b.ID == "" {
		t.Fatalf("criarPendenteExclusao: resposta sem id: %v (body=%s)", err, w.Body.String())
	}
	return b.ID
}

// --- POST /api/usuarios/me/solicitacao-exclusao -----------------------

func TestSolicitarExclusaoContaHandler_201(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Solicita HTTP", "excl-h-solicita@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "excl-h-solicita@empresa.com", "senha-123456")

	w := postSolicitacaoExclusao(db, "Bearer "+token)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var b solicitacaoExclusaoBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if b.ID == "" || b.Status != "pendente" || b.CriadoEm == "" {
		t.Errorf("corpo = %+v, want {id,status:pendente,criadoEm}", b)
	}
}

func TestSolicitarExclusaoContaHandler_409Duplicata(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Dup HTTP", "excl-h-dup@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "excl-h-dup@empresa.com", "senha-123456")

	if w := postSolicitacaoExclusao(db, "Bearer "+token); w.Code != http.StatusCreated {
		t.Fatalf("pré-condição 201: status = %d (body=%s)", w.Code, w.Body.String())
	}
	w := postSolicitacaoExclusao(db, "Bearer "+token)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

func TestSolicitarExclusaoContaHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	if w := postSolicitacaoExclusao(db, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// --- GET /api/solicitacoes-exclusao ---------------------------------

func TestListarSolicitacoesExclusaoHandler_200Adm(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Alguém", "excl-h-lista-user@empresa.com", "senha-123456", "usuario")
	userToken := tokenDeLogin(t, db, "excl-h-lista-user@empresa.com", "senha-123456")
	criarPendenteExclusao(t, db, userToken)

	criarContaComPapel(t, db, "Adm Lista", "excl-h-lista-adm@empresa.com", "senha-123456", "adm")
	admToken := tokenDeLogin(t, db, "excl-h-lista-adm@empresa.com", "senha-123456")

	w := getSolicitacoesExclusao(db, "Bearer "+admToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var b listaExclusaoBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(b.Solicitacoes) != 1 {
		t.Fatalf("len(solicitacoes) = %d, want 1 (%s)", len(b.Solicitacoes), w.Body.String())
	}
	it := b.Solicitacoes[0]
	if it.ID == "" || it.Nome != "Alguém" || it.Email != "excl-h-lista-user@empresa.com" || it.Papel != "usuario" || it.CriadoEm == "" {
		t.Errorf("item = %+v, want nome/email/papel/criadoEm do solicitante", it)
	}
}

func TestListarSolicitacoesExclusaoHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	if w := getSolicitacoesExclusao(db, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestListarSolicitacoesExclusaoHandler_403Gestor(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Gestor", "excl-h-gestor@empresa.com", "senha-123456", "gestor")
	token := tokenDeLogin(t, db, "excl-h-gestor@empresa.com", "senha-123456")

	w := getSolicitacoesExclusao(db, "Bearer "+token)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
}

// --- POST /api/solicitacoes-exclusao/{id}/processamento -------------

func TestProcessarExclusaoContaHandler_200Happy(t *testing.T) {
	db := testDB(t)
	alvoID := criarContaComPapel(t, db, "Alvo HTTP", "excl-h-alvo@empresa.com", "senha-123456", "usuario")
	alvoToken := tokenDeLogin(t, db, "excl-h-alvo@empresa.com", "senha-123456")
	solID := criarPendenteExclusao(t, db, alvoToken)

	criarContaComPapel(t, db, "Adm Proc", "excl-h-adm-proc@empresa.com", "senha-123456", "adm")
	admToken := tokenDeLogin(t, db, "excl-h-adm-proc@empresa.com", "senha-123456")

	w := postProcessamentoExclusao(db, solID, "Bearer "+admToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var b solicitacaoExclusaoBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if b.Nome != "Alvo HTTP" || b.Email != "excl-h-alvo@empresa.com" || b.Papel != "usuario" {
		t.Errorf("corpo = %+v, want dados PRÉ-anonimização do solicitante", b)
	}

	var nome, email string
	var ativo bool
	if err := db.QueryRow(`SELECT nome, email, ativo FROM usuarios WHERE id = $1`, alvoID).Scan(&nome, &email, &ativo); err != nil {
		t.Fatalf("reler alvo: %v", err)
	}
	if nome != "Usuário anonimizado" || email != "anonimizado+"+alvoID+"@anonimizado.invalido" || ativo {
		t.Errorf("alvo não anonimizado: nome=%q email=%q ativo=%v", nome, email, ativo)
	}
}

func TestProcessarExclusaoContaHandler_409UltimoAdm(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Único Adm HTTP", "excl-h-unico-adm@empresa.com", "senha-123456", "adm")
	admToken := tokenDeLogin(t, db, "excl-h-unico-adm@empresa.com", "senha-123456")
	// A única conta adm registra a própria exclusão.
	solID := criarPendenteExclusao(t, db, admToken)

	w := postProcessamentoExclusao(db, solID, "Bearer "+admToken)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
	if env.Error.Message == "" {
		t.Error("mensagem vazia, want explicação do invariante do último administrador")
	}
}

func TestProcessarExclusaoContaHandler_404IdDesconhecido(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Adm 404 HTTP", "excl-h-adm-404@empresa.com", "senha-123456", "adm")
	admToken := tokenDeLogin(t, db, "excl-h-adm-404@empresa.com", "senha-123456")

	w := postProcessamentoExclusao(db, "00000000-0000-0000-0000-000000000000", "Bearer "+admToken)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
	}
}

func TestProcessarExclusaoContaHandler_403Gestor(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Gestor Proc", "excl-h-gestor-proc@empresa.com", "senha-123456", "gestor")
	token := tokenDeLogin(t, db, "excl-h-gestor-proc@empresa.com", "senha-123456")

	w := postProcessamentoExclusao(db, "00000000-0000-0000-0000-000000000000", "Bearer "+token)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
}
