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
	"os"
	"strings"
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
	return postProcessamentoExclusaoComHandlerDB(db, db, id, authHeader)
}

// postProcessamentoExclusaoComHandlerDB é a variante de postProcessamentoExclusao
// que permite usar uma conexão DIFERENTE para o handler de processamento
// (`dbHandler`) da usada para resolver a sessão em RequireAuth (`dbAuth`) —
// necessária para simular uma falha de banco isolada no passo de
// processamento sem impedir a autenticação/autorização de resolver primeiro.
func postProcessamentoExclusaoComHandlerDB(dbAuth, dbHandler *sql.DB, id, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/solicitacoes-exclusao/{id}/processamento",
		middleware.RequireAuth(dbAuth, testJWTSecret)(
			middleware.RequireRole(services.PapelAdm)(
				ProcessarExclusaoContaHandler(dbHandler))))
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

// TestListarSolicitacoesExclusaoHandler_200Vazia cobre "lista vazia => []" da
// I/O Matrix na fronteira HTTP: nenhuma solicitação pendente devolve um array
// vazio (nunca `null`), via a rota real.
func TestListarSolicitacoesExclusaoHandler_200Vazia(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Adm Vazia HTTP", "excl-h-vazia-adm@empresa.com", "senha-123456", "adm")
	admToken := tokenDeLogin(t, db, "excl-h-vazia-adm@empresa.com", "senha-123456")

	w := getSolicitacoesExclusao(db, "Bearer "+admToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"solicitacoes":[]`) {
		t.Fatalf("body = %s, want solicitacoes:[] (array vazio, não null)", w.Body.String())
	}
	var b listaExclusaoBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(b.Solicitacoes) != 0 {
		t.Errorf("len(solicitacoes) = %d, want 0", len(b.Solicitacoes))
	}
}

// setCriadoEmExclusao força criado_em de uma solicitação de exclusão via SQL
// direto (offset em segundos a partir de agora), para tornar o ORDER BY
// determinístico em teste — mesma técnica de
// services.inserirSolicitacaoExclusao, usada aqui só para controlar a ORDEM
// de linhas já criadas pela rota HTTP real.
func setCriadoEmExclusao(t *testing.T, db *sql.DB, id string, offsetSegundos int) {
	t.Helper()
	if _, err := db.Exec(
		`UPDATE solicitacoes_exclusao_conta SET criado_em = now() + ($2 || ' seconds')::interval WHERE id = $1`,
		id, offsetSegundos,
	); err != nil {
		t.Fatalf("forçar criado_em da solicitação de exclusão: %v", err)
	}
}

// TestListarSolicitacoesExclusaoHandler_200MultiplosForaDeOrdem cobre "Adm
// lista solicitações" com MÚLTIPLOS itens inseridos fora de ordem de criação,
// batendo na rota real (o service já tem cobertura equivalente em
// TestListarSolicitacoesExclusao_PendentesOrdenadas) — prova que a ordenação
// por criado_em,id também se sustenta na fronteira HTTP.
func TestListarSolicitacoesExclusaoHandler_200MultiplosForaDeOrdem(t *testing.T) {
	db := testDB(t)

	criarContaComPapel(t, db, "Carla Ordem", "excl-h-ordem-c@empresa.com", "senha-123456", "usuario")
	tokenC := tokenDeLogin(t, db, "excl-h-ordem-c@empresa.com", "senha-123456")
	idC := criarPendenteExclusao(t, db, tokenC)

	criarContaComPapel(t, db, "Ana Ordem", "excl-h-ordem-a@empresa.com", "senha-123456", "usuario")
	tokenA := tokenDeLogin(t, db, "excl-h-ordem-a@empresa.com", "senha-123456")
	idA := criarPendenteExclusao(t, db, tokenA)

	criarContaComPapel(t, db, "Bruno Ordem", "excl-h-ordem-b@empresa.com", "senha-123456", "usuario")
	tokenB := tokenDeLogin(t, db, "excl-h-ordem-b@empresa.com", "senha-123456")
	idB := criarPendenteExclusao(t, db, tokenB)

	// Força a ORDEM cronológica: Ana(10) < Bruno(20) < Carla(30), mesmo tendo
	// sido criada primeiro na chamada acima.
	setCriadoEmExclusao(t, db, idC, 30)
	setCriadoEmExclusao(t, db, idA, 10)
	setCriadoEmExclusao(t, db, idB, 20)

	criarContaComPapel(t, db, "Adm Ordem HTTP", "excl-h-ordem-adm@empresa.com", "senha-123456", "adm")
	admToken := tokenDeLogin(t, db, "excl-h-ordem-adm@empresa.com", "senha-123456")

	w := getSolicitacoesExclusao(db, "Bearer "+admToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var b listaExclusaoBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(b.Solicitacoes) != 3 {
		t.Fatalf("len(solicitacoes) = %d, want 3 (%+v)", len(b.Solicitacoes), b.Solicitacoes)
	}
	wantNomes := []string{"Ana Ordem", "Bruno Ordem", "Carla Ordem"}
	for i, want := range wantNomes {
		if b.Solicitacoes[i].Nome != want {
			t.Errorf("solicitacoes[%d].Nome = %q, want %q (ordenação por criado_em)", i, b.Solicitacoes[i].Nome, want)
		}
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
	// Assert o texto EXATO (não só "não vazio"): garante que a mensagem do
	// invariante do último `adm` (services.ErrUltimoAdmAtivo) realmente chega
	// até a resposta HTTP, e não uma mensagem genérica qualquer.
	if want := services.ErrUltimoAdmAtivo.Error(); env.Error.Message != want {
		t.Errorf("message = %q, want %q", env.Error.Message, want)
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

func TestProcessarExclusaoContaHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	w := postProcessamentoExclusao(db, "00000000-0000-0000-0000-000000000000", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// TestProcessarExclusaoContaHandler_LoginAntigoFalha cobre, na fronteira HTTP
// real (POST /api/auth/login via LoginHandler — não Login(...) direto), a
// linha "Login por e-mail antigo após anonimização" da I/O Matrix: depois do
// processamento pela rota real, autenticar com o e-mail original continua
// falhando exatamente como conta inexistente.
func TestProcessarExclusaoContaHandler_LoginAntigoFalha(t *testing.T) {
	db := testDB(t)
	const emailOriginal = "excl-h-login-antigo@empresa.com"
	criarContaComPapel(t, db, "Login Antigo HTTP", emailOriginal, "senha-123456", "usuario")
	alvoToken := tokenDeLogin(t, db, emailOriginal, "senha-123456")
	solID := criarPendenteExclusao(t, db, alvoToken)

	criarContaComPapel(t, db, "Adm Login HTTP", "excl-h-adm-login@empresa.com", "senha-123456", "adm")
	admToken := tokenDeLogin(t, db, "excl-h-adm-login@empresa.com", "senha-123456")

	if w := postProcessamentoExclusao(db, solID, "Bearer "+admToken); w.Code != http.StatusOK {
		t.Fatalf("pré-condição processamento: status = %d (body=%s)", w.Code, w.Body.String())
	}

	w := postLogin(db, `{"email":"`+emailOriginal+`","senha":"senha-123456"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "INVALID_CREDENTIALS" {
		t.Errorf("code = %q, want INVALID_CREDENTIALS", env.Error.Code)
	}
}

// TestProcessarExclusaoContaHandler_SSOAntigoFalha cobre, na fronteira HTTP
// real (POST /api/auth/sso/keycloak via KeycloakSSOHandler — não
// BuscarUsuarioPorEmailSSO(...) direto), a linha "SSO com o e-mail antigo
// após anonimização": o callback responde 401 SSO_SEM_CONTA porque
// BuscarUsuarioPorEmailSSO não acha mais nenhuma linha com o e-mail original.
func TestProcessarExclusaoContaHandler_SSOAntigoFalha(t *testing.T) {
	db := testDB(t)
	priv := ssoGerarChave(t)
	const emailOriginal = "excl-h-sso-antigo@empresa.com"
	criarContaComPapel(t, db, "SSO Antigo HTTP", emailOriginal, "senha-123456", "usuario")
	alvoToken := tokenDeLogin(t, db, emailOriginal, "senha-123456")
	solID := criarPendenteExclusao(t, db, alvoToken)

	criarContaComPapel(t, db, "Adm SSO HTTP", "excl-h-adm-sso@empresa.com", "senha-123456", "adm")
	admToken := tokenDeLogin(t, db, "excl-h-adm-sso@empresa.com", "senha-123456")

	if w := postProcessamentoExclusao(db, solID, "Bearer "+admToken); w.Code != http.StatusOK {
		t.Fatalf("pré-condição processamento: status = %d (body=%s)", w.Code, w.Body.String())
	}

	h := ssoHandler(t, db, priv, "")
	c := ssoClaims()
	c["email"] = emailOriginal
	w := postSSOKeycloak(h, ssoAssinar(t, priv, ssoKid, c))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
	if got := decodeErro(t, w.Body.Bytes()).Error.Code; got != "SSO_SEM_CONTA" {
		t.Fatalf("code = %q, want SSO_SEM_CONTA", got)
	}
}

// TestProcessarExclusaoContaHandler_500FalhaDeBanco cobre "Falha de banco em
// qualquer passo" através do HANDLER real (ProcessarExclusaoContaHandler),
// não só de ProcessarExclusaoConta(db2,...) no nível de service: a conexão
// usada pelo HANDLER está fechada (RequireAuth ainda resolve a sessão pela
// conexão boa), então o processamento falha e a resposta é 500 INTERNAL_ERROR
// no envelope AD-14.
func TestProcessarExclusaoContaHandler_500FalhaDeBanco(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Erro Banco HTTP", "excl-h-erro-banco@empresa.com", "senha-123456", "usuario")
	alvoToken := tokenDeLogin(t, db, "excl-h-erro-banco@empresa.com", "senha-123456")
	solID := criarPendenteExclusao(t, db, alvoToken)

	criarContaComPapel(t, db, "Adm Erro HTTP", "excl-h-adm-erro@empresa.com", "senha-123456", "adm")
	admToken := tokenDeLogin(t, db, "excl-h-adm-erro@empresa.com", "senha-123456")

	dbFechado, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("abrir conexão auxiliar: %v", err)
	}
	dbFechado.Close()

	w := postProcessamentoExclusaoComHandlerDB(db, dbFechado, solID, "Bearer "+admToken)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%s)", w.Code, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("code = %q, want INTERNAL_ERROR", env.Error.Code)
	}
}

// TestProcessarExclusaoContaHandler_409JaProcessada cobre "Solicitação já
// processada (reuso/corrida)" no nível de HANDLER — batendo duas vezes na
// rota real de processamento — complementando
// services.TestProcessarExclusaoConta_JaProcessada (só no nível de service).
func TestProcessarExclusaoContaHandler_409JaProcessada(t *testing.T) {
	db := testDB(t)
	alvoToken := (func() string {
		criarContaComPapel(t, db, "Duas Vezes HTTP", "excl-h-2x@empresa.com", "senha-123456", "usuario")
		return tokenDeLogin(t, db, "excl-h-2x@empresa.com", "senha-123456")
	})()
	solID := criarPendenteExclusao(t, db, alvoToken)

	criarContaComPapel(t, db, "Adm 2x HTTP", "excl-h-adm-2x@empresa.com", "senha-123456", "adm")
	admToken := tokenDeLogin(t, db, "excl-h-adm-2x@empresa.com", "senha-123456")

	if w := postProcessamentoExclusao(db, solID, "Bearer "+admToken); w.Code != http.StatusOK {
		t.Fatalf("primeiro processamento: status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	w := postProcessamentoExclusao(db, solID, "Bearer "+admToken)
	if w.Code != http.StatusConflict {
		t.Fatalf("segundo processamento: status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}
