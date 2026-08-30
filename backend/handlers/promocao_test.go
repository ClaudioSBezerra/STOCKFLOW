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

// --- despacho pela MESMA composição de newMux (main.go) ------------------

func postPromocao(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/promocoes", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	middleware.RequireAuth(db, testJWTSecret)(SolicitarPromocaoHandler(db))(w, req)
	return w
}

func getMinhaPromocao(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/promocoes/minha", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	middleware.RequireAuth(db, testJWTSecret)(MinhaSolicitacaoHandler(db))(w, req)
	return w
}

func getPromocoes(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/promocoes", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	middleware.RequireAuth(db, testJWTSecret)(
		middleware.RequireRole(services.PapelGestor)(
			ListarPromocoesHandler(db)))(w, req)
	return w
}

// postDecisao usa um http.ServeMux local com o MESMO padrão de rota de newMux
// para exercitar a extração de `r.PathValue("id")` além da composição de
// middleware.
func postDecisao(db *sql.DB, id, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/promocoes/{id}/decisao",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelGestor)(
				DecidirPromocaoHandler(db))))
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, "/api/promocoes/"+id+"/decisao", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, "/api/promocoes/"+id+"/decisao", nil)
	}
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

type solicitacaoBody struct {
	Solicitacao *struct {
		ID         string  `json:"id"`
		PapelAlvo  string  `json:"papel_alvo"`
		Status     string  `json:"status"`
		CriadoEm   string  `json:"criado_em"`
		DecididoEm *string `json:"decidido_em"`
	} `json:"solicitacao"`
}

func decodeSolicitacao(t *testing.T, body []byte) solicitacaoBody {
	t.Helper()
	var b solicitacaoBody
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatalf("falha ao decodificar solicitação: %v (body=%s)", err, body)
	}
	return b
}

// criarPendente solicita promoção para `email` (papel `usuario`/`almoxarife`)
// via o handler real e devolve o id da solicitação pendente criada.
func criarPendente(t *testing.T, db *sql.DB, token string) string {
	t.Helper()
	w := postPromocao(db, "Bearer "+token)
	if w.Code != http.StatusCreated {
		t.Fatalf("criarPendente: status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	b := decodeSolicitacao(t, w.Body.Bytes())
	if b.Solicitacao == nil || b.Solicitacao.ID == "" {
		t.Fatalf("criarPendente: resposta sem solicitação: %s", w.Body.String())
	}
	return b.Solicitacao.ID
}

// --- POST /api/promocoes ----------------------------------------------

func TestSolicitarPromocaoHandler_SucessoAlvoDerivado(t *testing.T) {
	db := testDB(t)
	casos := []struct{ papel, wantAlvo string }{
		{"usuario", "almoxarife"},
		{"almoxarife", "gestor"},
	}
	for _, c := range casos {
		t.Run(c.papel, func(t *testing.T) {
			email := c.papel + "-http-solicita@empresa.com"
			criarContaComPapel(t, db, "Conta", email, "senha-123456", c.papel)
			token := tokenDeLogin(t, db, email, "senha-123456")

			w := postPromocao(db, "Bearer "+token)
			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
			}
			b := decodeSolicitacao(t, w.Body.Bytes())
			if b.Solicitacao == nil {
				t.Fatal("resposta sem solicitação")
			}
			if b.Solicitacao.PapelAlvo != c.wantAlvo {
				t.Errorf("papel_alvo = %q, want %q", b.Solicitacao.PapelAlvo, c.wantAlvo)
			}
			if b.Solicitacao.Status != "pendente" {
				t.Errorf("status = %q, want pendente", b.Solicitacao.Status)
			}
		})
	}
}

func TestSolicitarPromocaoHandler_PapelSemPromocao403(t *testing.T) {
	db := testDB(t)
	for _, papel := range []string{"gestor", "adm"} {
		t.Run(papel, func(t *testing.T) {
			email := papel + "-http-sem-promo@empresa.com"
			criarContaComPapel(t, db, "Conta", email, "senha-123456", papel)
			token := tokenDeLogin(t, db, email, "senha-123456")

			w := postPromocao(db, "Bearer "+token)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "FORBIDDEN" {
				t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
			}
		})
	}
}

func TestSolicitarPromocaoHandler_JaHaPendente409(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Conta", "http-ja-pendente@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "http-ja-pendente@empresa.com", "senha-123456")

	criarPendente(t, db, token)
	w := postPromocao(db, "Bearer "+token)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

func TestSolicitarPromocaoHandler_AposRejeicao201(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Conta", "http-apos-rejeicao@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Gestor", "http-gestor-rej@empresa.com", "senha-123456", "gestor")
	solicitanteToken := tokenDeLogin(t, db, "http-apos-rejeicao@empresa.com", "senha-123456")
	gestorToken := tokenDeLogin(t, db, "http-gestor-rej@empresa.com", "senha-123456")

	solID := criarPendente(t, db, solicitanteToken)
	if w := postDecisao(db, solID, "Bearer "+gestorToken, `{"aprovar":false}`); w.Code != http.StatusOK {
		t.Fatalf("rejeição: status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	w := postPromocao(db, "Bearer "+solicitanteToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("nova solicitação após rejeição: status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
}

func TestSolicitarPromocaoHandler_SemToken401(t *testing.T) {
	db := testDB(t)
	w := postPromocao(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want TOKEN_EXPIRED", env.Error.Code)
	}
}

// --- GET /api/promocoes/minha ---------------------------------------

func TestMinhaSolicitacaoHandler_ExisteENil(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Sem", "http-minha-sem@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Com", "http-minha-com@empresa.com", "senha-123456", "usuario")
	tokenSem := tokenDeLogin(t, db, "http-minha-sem@empresa.com", "senha-123456")
	tokenCom := tokenDeLogin(t, db, "http-minha-com@empresa.com", "senha-123456")

	wSem := getMinhaPromocao(db, "Bearer "+tokenSem)
	if wSem.Code != http.StatusOK {
		t.Fatalf("sem histórico: status = %d, want 200 (body=%s)", wSem.Code, wSem.Body.String())
	}
	if b := decodeSolicitacao(t, wSem.Body.Bytes()); b.Solicitacao != nil {
		t.Errorf("sem histórico: solicitacao = %+v, want null", *b.Solicitacao)
	}

	criarPendente(t, db, tokenCom)
	wCom := getMinhaPromocao(db, "Bearer "+tokenCom)
	if wCom.Code != http.StatusOK {
		t.Fatalf("com histórico: status = %d, want 200 (body=%s)", wCom.Code, wCom.Body.String())
	}
	b := decodeSolicitacao(t, wCom.Body.Bytes())
	if b.Solicitacao == nil {
		t.Fatal("com histórico: solicitacao = null, want objeto")
	}
	if b.Solicitacao.Status != "pendente" {
		t.Errorf("status = %q, want pendente", b.Solicitacao.Status)
	}
	if b.Solicitacao.DecididoEm != nil {
		t.Errorf("decidido_em = %v, want null enquanto pendente", *b.Solicitacao.DecididoEm)
	}
}

func TestMinhaSolicitacaoHandler_SemToken401(t *testing.T) {
	db := testDB(t)
	w := getMinhaPromocao(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
}

// --- GET /api/promocoes -------------------------------------------

type pendentesBody struct {
	Solicitacoes []struct {
		ID               string `json:"id"`
		SolicitanteNome  string `json:"solicitante_nome"`
		SolicitanteEmail string `json:"solicitante_email"`
		PapelAtual       string `json:"papel_atual"`
		PapelAlvo        string `json:"papel_alvo"`
	} `json:"solicitacoes"`
}

func decodePendentes(t *testing.T, body []byte) pendentesBody {
	t.Helper()
	var b pendentesBody
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatalf("falha ao decodificar pendentes: %v (body=%s)", err, body)
	}
	return b
}

func TestListarPromocoesHandler_RecorteGestorVsAdm(t *testing.T) {
	db := testDB(t)
	// Dois pedidos de alvo almoxarife + um de alvo gestor.
	criarContaComPapel(t, db, "U1", "http-fila-u1@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "U2", "http-fila-u2@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "A1", "http-fila-a1@empresa.com", "senha-123456", "almoxarife")
	criarContaComPapel(t, db, "Gestora", "http-fila-gestor@empresa.com", "senha-123456", "gestor")
	criarContaComPapel(t, db, "Adm", "http-fila-adm@empresa.com", "senha-123456", "adm")

	criarPendente(t, db, tokenDeLogin(t, db, "http-fila-u1@empresa.com", "senha-123456"))
	criarPendente(t, db, tokenDeLogin(t, db, "http-fila-u2@empresa.com", "senha-123456"))
	criarPendente(t, db, tokenDeLogin(t, db, "http-fila-a1@empresa.com", "senha-123456"))

	gestorToken := tokenDeLogin(t, db, "http-fila-gestor@empresa.com", "senha-123456")
	admToken := tokenDeLogin(t, db, "http-fila-adm@empresa.com", "senha-123456")

	wg := getPromocoes(db, "Bearer "+gestorToken)
	if wg.Code != http.StatusOK {
		t.Fatalf("gestor: status = %d, want 200 (body=%s)", wg.Code, wg.Body.String())
	}
	bg := decodePendentes(t, wg.Body.Bytes())
	if len(bg.Solicitacoes) != 2 {
		t.Fatalf("gestor: len = %d, want 2 (%+v)", len(bg.Solicitacoes), bg.Solicitacoes)
	}
	for _, s := range bg.Solicitacoes {
		if s.PapelAlvo != "almoxarife" {
			t.Errorf("gestor recebeu alvo %q — fora do escopo", s.PapelAlvo)
		}
		if s.SolicitanteEmail == "" || s.PapelAtual == "" {
			t.Errorf("dados do solicitante não resolvidos: %+v", s)
		}
	}

	wa := getPromocoes(db, "Bearer "+admToken)
	if wa.Code != http.StatusOK {
		t.Fatalf("adm: status = %d, want 200 (body=%s)", wa.Code, wa.Body.String())
	}
	ba := decodePendentes(t, wa.Body.Bytes())
	if len(ba.Solicitacoes) != 3 {
		t.Fatalf("adm: len = %d, want 3 (%+v)", len(ba.Solicitacoes), ba.Solicitacoes)
	}
}

func TestListarPromocoesHandler_PapelAbaixoDeGestor403(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Usuária", "http-fila-403-u@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Almox", "http-fila-403-a@empresa.com", "senha-123456", "almoxarife")

	for _, email := range []string{"http-fila-403-u@empresa.com", "http-fila-403-a@empresa.com"} {
		token := tokenDeLogin(t, db, email, "senha-123456")
		w := getPromocoes(db, "Bearer "+token)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: status = %d, want 403 (body=%s)", email, w.Code, w.Body.String())
		}
		if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "FORBIDDEN" {
			t.Errorf("%s: code = %q, want FORBIDDEN", email, env.Error.Code)
		}
	}
}

func TestListarPromocoesHandler_SemToken401(t *testing.T) {
	db := testDB(t)
	w := getPromocoes(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
}

// --- POST /api/promocoes/{id}/decisao -----------------------------

// TestDecidirPromocaoHandler_AprovarAlmoxarifePorGestor prova o encadeamento
// pós-aprovação: 200; e o token do solicitante, na PRÓXIMA requisição
// (GET /api/auth/me), já resolve com o novo papel `almoxarife` (AC2 — o
// middleware relê o papel do Postgres, sem esperar a sessão expirar).
func TestDecidirPromocaoHandler_AprovarAlmoxarifePorGestor(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Solicitante", "http-aprova-u@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Gestora", "http-aprova-gestor@empresa.com", "senha-123456", "gestor")
	solicitanteToken := tokenDeLogin(t, db, "http-aprova-u@empresa.com", "senha-123456")
	gestorToken := tokenDeLogin(t, db, "http-aprova-gestor@empresa.com", "senha-123456")

	solID := criarPendente(t, db, solicitanteToken)

	w := postDecisao(db, solID, "Bearer "+gestorToken, `{"aprovar":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	b := decodeSolicitacao(t, w.Body.Bytes())
	if b.Solicitacao == nil || b.Solicitacao.Status != "aprovada" {
		t.Fatalf("resposta = %s, want status aprovada", w.Body.String())
	}
	if b.Solicitacao.DecididoEm == nil {
		t.Error("decidido_em = null, want preenchido")
	}

	wMe := getMe(db, "Bearer "+solicitanteToken)
	if wMe.Code != http.StatusOK {
		t.Fatalf("/me: status = %d, want 200 (body=%s)", wMe.Code, wMe.Body.String())
	}
	var me struct {
		Papel string `json:"papel"`
	}
	if err := json.Unmarshal(wMe.Body.Bytes(), &me); err != nil {
		t.Fatalf("decodificar /me: %v", err)
	}
	if me.Papel != "almoxarife" {
		t.Errorf("papel em /me = %q, want almoxarife (efeito imediato da aprovação)", me.Papel)
	}
}

// TestDecidirPromocaoHandler_AprovarGestorPorAdm prova a linha "Aprovar
// promoção a gestor por adm" + o encadeamento com uma rota RequireRole(gestor):
// antes da aprovação o token do solicitante recebe 403 em GET /api/usuarios;
// depois, 200.
func TestDecidirPromocaoHandler_AprovarGestorPorAdm(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Almox", "http-aprova-g-a@empresa.com", "senha-123456", "almoxarife")
	criarContaComPapel(t, db, "Adm", "http-aprova-g-adm@empresa.com", "senha-123456", "adm")
	solicitanteToken := tokenDeLogin(t, db, "http-aprova-g-a@empresa.com", "senha-123456")
	admToken := tokenDeLogin(t, db, "http-aprova-g-adm@empresa.com", "senha-123456")

	if w := getUsuarios(db, "Bearer "+solicitanteToken); w.Code != http.StatusForbidden {
		t.Fatalf("antes: GET /api/usuarios status = %d, want 403", w.Code)
	}

	solID := criarPendente(t, db, solicitanteToken)
	if w := postDecisao(db, solID, "Bearer "+admToken, `{"aprovar":true}`); w.Code != http.StatusOK {
		t.Fatalf("aprovação: status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	if w := getUsuarios(db, "Bearer "+solicitanteToken); w.Code != http.StatusOK {
		t.Fatalf("depois: GET /api/usuarios status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
}

func TestDecidirPromocaoHandler_AlvoGestorPorNaoAdm403(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Almox", "http-dec-g-a@empresa.com", "senha-123456", "almoxarife")
	criarContaComPapel(t, db, "Gestora", "http-dec-g-gestor@empresa.com", "senha-123456", "gestor")
	solicitanteToken := tokenDeLogin(t, db, "http-dec-g-a@empresa.com", "senha-123456")
	gestorToken := tokenDeLogin(t, db, "http-dec-g-gestor@empresa.com", "senha-123456")

	solID := criarPendente(t, db, solicitanteToken)
	w := postDecisao(db, solID, "Bearer "+gestorToken, `{"aprovar":true}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "FORBIDDEN" {
		t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
	}
	// Nada mudou: o solicitante segue almoxarife.
	wMe := getMe(db, "Bearer "+solicitanteToken)
	var me struct {
		Papel string `json:"papel"`
	}
	_ = json.Unmarshal(wMe.Body.Bytes(), &me)
	if me.Papel != "almoxarife" {
		t.Errorf("papel = %q, want almoxarife (intacto)", me.Papel)
	}
}

func TestDecidirPromocaoHandler_Rejeitar200(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Solicitante", "http-rej-u@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Gestora", "http-rej-gestor@empresa.com", "senha-123456", "gestor")
	solicitanteToken := tokenDeLogin(t, db, "http-rej-u@empresa.com", "senha-123456")
	gestorToken := tokenDeLogin(t, db, "http-rej-gestor@empresa.com", "senha-123456")

	solID := criarPendente(t, db, solicitanteToken)
	w := postDecisao(db, solID, "Bearer "+gestorToken, `{"aprovar":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if b := decodeSolicitacao(t, w.Body.Bytes()); b.Solicitacao == nil || b.Solicitacao.Status != "rejeitada" {
		t.Fatalf("resposta = %s, want status rejeitada", w.Body.String())
	}
}

func TestDecidirPromocaoHandler_Inexistente404(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Gestora", "http-dec-404@empresa.com", "senha-123456", "gestor")
	token := tokenDeLogin(t, db, "http-dec-404@empresa.com", "senha-123456")

	w := postDecisao(db, "00000000-0000-0000-0000-000000000000", "Bearer "+token, `{"aprovar":true}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
	}
}

func TestDecidirPromocaoHandler_IDMalformado404(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Gestora", "http-dec-malform@empresa.com", "senha-123456", "gestor")
	token := tokenDeLogin(t, db, "http-dec-malform@empresa.com", "senha-123456")

	w := postDecisao(db, "nao-e-uuid", "Bearer "+token, `{"aprovar":true}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
	}
}

func TestDecidirPromocaoHandler_JaDecidida409(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Solicitante", "http-dec-2x-u@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Gestora", "http-dec-2x-gestor@empresa.com", "senha-123456", "gestor")
	solicitanteToken := tokenDeLogin(t, db, "http-dec-2x-u@empresa.com", "senha-123456")
	gestorToken := tokenDeLogin(t, db, "http-dec-2x-gestor@empresa.com", "senha-123456")

	solID := criarPendente(t, db, solicitanteToken)
	if w := postDecisao(db, solID, "Bearer "+gestorToken, `{"aprovar":true}`); w.Code != http.StatusOK {
		t.Fatalf("primeira decisão: status = %d, want 200", w.Code)
	}
	w := postDecisao(db, solID, "Bearer "+gestorToken, `{"aprovar":false}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

func TestDecidirPromocaoHandler_PapelDoSolicitanteMudou409(t *testing.T) {
	db := testDB(t)
	solicitanteID := criarContaComPapel(t, db, "Subiu", "http-dec-mudou-u@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Gestora", "http-dec-mudou-gestor@empresa.com", "senha-123456", "gestor")
	solicitanteToken := tokenDeLogin(t, db, "http-dec-mudou-u@empresa.com", "senha-123456")
	gestorToken := tokenDeLogin(t, db, "http-dec-mudou-gestor@empresa.com", "senha-123456")

	solID := criarPendente(t, db, solicitanteToken)
	if _, err := db.Exec(`UPDATE usuarios SET papel = 'gestor' WHERE id = $1`, solicitanteID); err != nil {
		t.Fatalf("forçar mudança de papel: %v", err)
	}

	w := postDecisao(db, solID, "Bearer "+gestorToken, `{"aprovar":true}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

// TestDecidirPromocaoHandler_CorpoInvalido400 cobre a linha "Decidir, corpo
// malformado" da I/O Matrix E o guard adicional contra rejeição silenciosa:
// um corpo sem a chave `aprovar` (`{}`, `{"aprovar":null}`, chave errada)
// decodifica sem erro num `bool` puro e viraria `aprovar=false` — precisa ser
// 400, nunca uma rejeição involuntária da solicitação pendente.
func TestDecidirPromocaoHandler_CorpoInvalido400(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Gestora", "http-dec-inv-gestor@empresa.com", "senha-123456", "gestor")
	gestorToken := tokenDeLogin(t, db, "http-dec-inv-gestor@empresa.com", "senha-123456")

	casos := []struct{ nome, slug, corpo string }{
		{"JSON malformado", "json", `{isto nao e json`},
		{"objeto vazio", "vazio", `{}`},
		{"aprovar null", "null", `{"aprovar":null}`},
		{"chave errada", "chave", `{"aprovado":true}`},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			// Uma solicitação pendente nova por caso, para provar que ela NÃO
			// foi decidida silenciosamente (o bug do `bool` puro viraria
			// aprovar=false e rejeitaria/aprovaria sem intenção).
			email := "http-dec-inv-" + c.slug + "@empresa.com"
			criarContaComPapel(t, db, "S", email, "senha-123456", "usuario")
			sToken := tokenDeLogin(t, db, email, "senha-123456")
			solID := criarPendente(t, db, sToken)

			w := postDecisao(db, solID, "Bearer "+gestorToken, c.corpo)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
				t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
			}

			wMinha := getMinhaPromocao(db, "Bearer "+sToken)
			b := decodeSolicitacao(t, wMinha.Body.Bytes())
			if b.Solicitacao == nil || b.Solicitacao.Status != "pendente" {
				t.Errorf("solicitação = %s, want ainda pendente (nenhuma decisão silenciosa)", wMinha.Body.String())
			}
		})
	}
}

func TestDecidirPromocaoHandler_SemTokenOuPapelInsuficiente(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Usuária", "http-dec-semtoken-u@empresa.com", "senha-123456", "usuario")
	usuarioToken := tokenDeLogin(t, db, "http-dec-semtoken-u@empresa.com", "senha-123456")

	id := "00000000-0000-0000-0000-000000000000"

	wSem := postDecisao(db, id, "", `{"aprovar":true}`)
	if wSem.Code != http.StatusUnauthorized {
		t.Fatalf("sem token: status = %d, want 401 (body=%s)", wSem.Code, wSem.Body.String())
	}

	wPapel := postDecisao(db, id, "Bearer "+usuarioToken, `{"aprovar":true}`)
	if wPapel.Code != http.StatusForbidden {
		t.Fatalf("papel usuario: status = %d, want 403 (body=%s)", wPapel.Code, wPapel.Body.String())
	}
}
