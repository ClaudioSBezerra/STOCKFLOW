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

// --- despacho pela MESMA composição de newMux (main.go) --------------------
//
// Ambas as rotas usam um http.ServeMux local com o MESMO padrão registrado em
// produção, para exercitar a extração de `r.PathValue("id")` além da
// composição RequireAuth -> RequireRole(gestor) -> handler.

func postDesativacao(db *sql.DB, id, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/usuarios/{id}/desativacao",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelGestor)(
				DesativarUsuarioHandler(db))))
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, "/api/usuarios/"+id+"/desativacao", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, "/api/usuarios/"+id+"/desativacao", nil)
	}
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func postRebaixamento(db *sql.DB, id, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/usuarios/{id}/rebaixamento",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelGestor)(
				RebaixarUsuarioHandler(db))))
	r := httptest.NewRequest(http.MethodPost, "/api/usuarios/"+id+"/rebaixamento", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

type usuarioGestaoBody struct {
	Usuario *services.UsuarioResumo `json:"usuario"`
}

func decodeUsuarioGestao(t *testing.T, body []byte) usuarioGestaoBody {
	t.Helper()
	var b usuarioGestaoBody
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatalf("falha ao decodificar resposta de gestão de usuário: %v (body=%s)", err, body)
	}
	return b
}

func sessoesVivasDe(t *testing.T, db *sql.DB, usuarioID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT count(*) FROM sessoes WHERE usuario_id = $1 AND revogado_em IS NULL`, usuarioID,
	).Scan(&n); err != nil {
		t.Fatalf("contar sessões vivas: %v", err)
	}
	return n
}

// --- POST /api/usuarios/{id}/desativacao ----------------------------------

// TestDesativarUsuarioHandler_DesativaEEncerraSessoes prova a AC2 na fronteira
// HTTP: um gestor desativa um almoxarife -> 200 {"usuario":{ativo:false}}; as
// sessões vivas do alvo ficam revogadas; o alvo não loga mais; e um access
// token pré-existente do alvo passa a receber 401 SESSION_REVOKED.
func TestDesativarUsuarioHandler_DesativaEEncerraSessoes(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Gestora", "h-gestor-desativa@empresa.com", "senha-123456", "gestor")
	alvoID := criarContaComPapel(t, db, "Almox", "h-almox-alvo@empresa.com", "senha-123456", "almoxarife")

	gestorToken := tokenDeLogin(t, db, "h-gestor-desativa@empresa.com", "senha-123456")

	// Login real do alvo para capturar TANTO o access token QUANTO o cookie de
	// refresh emitidos antes da desativação — POST /api/auth/refresh não passa
	// por RequireAuth, então precisa ser provado separadamente que o cookie
	// pré-desativação deixa de rotacionar (Design Note da spec-1-8).
	wLoginAlvo := postLogin(db, `{"email":"h-almox-alvo@empresa.com","senha":"senha-123456"}`)
	if wLoginAlvo.Code != http.StatusOK {
		t.Fatalf("login do alvo: status = %d, want 200 (body=%s)", wLoginAlvo.Code, wLoginAlvo.Body.String())
	}
	var loginAlvoBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(wLoginAlvo.Body.Bytes(), &loginAlvoBody); err != nil {
		t.Fatalf("decodificar login do alvo: %v", err)
	}
	alvoToken := loginAlvoBody.Token
	cookieDoAlvo := refreshCookieDoResultado(t, wLoginAlvo)
	if sessoesVivasDe(t, db, alvoID) == 0 {
		t.Fatal("pré-condição: o login do alvo deveria ter criado uma sessão viva")
	}

	w := postDesativacao(db, alvoID, "Bearer "+gestorToken, `{"ativo":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	b := decodeUsuarioGestao(t, w.Body.Bytes())
	if b.Usuario == nil || b.Usuario.Ativo {
		t.Fatalf("resposta = %s, want usuario.ativo=false", w.Body.String())
	}

	if n := sessoesVivasDe(t, db, alvoID); n != 0 {
		t.Errorf("sessões vivas do alvo = %d, want 0", n)
	}

	// O alvo não loga mais com a senha correta.
	wLogin := postLogin(db, `{"email":"h-almox-alvo@empresa.com","senha":"senha-123456"}`)
	if wLogin.Code != http.StatusUnauthorized {
		t.Errorf("login pós-desativação: status = %d, want 401 (body=%s)", wLogin.Code, wLogin.Body.String())
	}

	// Um access token já emitido do alvo é derrubado na requisição seguinte.
	wMe := getMe(db, "Bearer "+alvoToken)
	if wMe.Code != http.StatusUnauthorized {
		t.Fatalf("/me com token antigo: status = %d, want 401 (body=%s)", wMe.Code, wMe.Body.String())
	}
	if env := decodeErro(t, wMe.Body.Bytes()); env.Error.Code != "SESSION_REVOKED" {
		t.Errorf("code = %q, want SESSION_REVOKED", env.Error.Code)
	}

	// E o cookie de refresh emitido antes da desativação já não rotaciona —
	// sem a revogação de `sessoes` na desativação, este refresh seguiria
	// emitindo novos access tokens mesmo com toda rota protegida em 401.
	wRefresh := postRefresh(db, cookieDoAlvo)
	if wRefresh.Code != http.StatusUnauthorized {
		t.Fatalf("refresh com cookie pré-desativação: status = %d, want 401 (body=%s)", wRefresh.Code, wRefresh.Body.String())
	}
}

// TestDesativarUsuarioHandler_Reativa prova a linha "Reativar conta por gestor":
// {"ativo":true} numa conta inativa -> 200 e o login volta a funcionar.
func TestDesativarUsuarioHandler_Reativa(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Gestora", "h-gestor-reativa@empresa.com", "senha-123456", "gestor")
	alvoID := criarContaComPapel(t, db, "Inativo", "h-inativo-alvo@empresa.com", "senha-123456", "almoxarife")
	if _, err := db.Exec(`UPDATE usuarios SET ativo = false WHERE id = $1`, alvoID); err != nil {
		t.Fatalf("forçar inativo: %v", err)
	}
	gestorToken := tokenDeLogin(t, db, "h-gestor-reativa@empresa.com", "senha-123456")

	w := postDesativacao(db, alvoID, "Bearer "+gestorToken, `{"ativo":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if b := decodeUsuarioGestao(t, w.Body.Bytes()); b.Usuario == nil || !b.Usuario.Ativo {
		t.Fatalf("resposta = %s, want usuario.ativo=true", w.Body.String())
	}
	wLogin := postLogin(db, `{"email":"h-inativo-alvo@empresa.com","senha":"senha-123456"}`)
	if wLogin.Code != http.StatusOK {
		t.Errorf("login pós-reativação: status = %d, want 200 (body=%s)", wLogin.Code, wLogin.Body.String())
	}
}

// TestDesativarUsuarioHandler_EscopoEAutoAcao403 prova as linhas de escopo e
// auto-ação: gestor sobre gestor/adm e qualquer conta sobre si mesma -> 403
// FORBIDDEN, com `usuarios`/`sessoes` intactos.
func TestDesativarUsuarioHandler_EscopoEAutoAcao403(t *testing.T) {
	db := testDB(t)
	gestorID := criarContaComPapel(t, db, "Gestora", "h-gestor-escopo@empresa.com", "senha-123456", "gestor")
	admID := criarContaComPapel(t, db, "Adm", "h-adm-escopo@empresa.com", "senha-123456", "adm")
	outroGestorID := criarContaComPapel(t, db, "Outro Gestor", "h-gestor2-escopo@empresa.com", "senha-123456", "gestor")
	gestorToken := tokenDeLogin(t, db, "h-gestor-escopo@empresa.com", "senha-123456")
	admToken := tokenDeLogin(t, db, "h-adm-escopo@empresa.com", "senha-123456")

	casos := []struct {
		nome, alvoID, token string
	}{
		{"gestor sobre gestor", outroGestorID, gestorToken},
		{"gestor sobre adm", admID, gestorToken},
		{"gestor sobre a própria conta", gestorID, gestorToken},
		{"adm sobre a própria conta", admID, admToken},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w := postDesativacao(db, c.alvoID, "Bearer "+c.token, `{"ativo":false}`)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "FORBIDDEN" {
				t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
			}
			var ativo bool
			if err := db.QueryRow(`SELECT ativo FROM usuarios WHERE id = $1`, c.alvoID).Scan(&ativo); err != nil {
				t.Fatalf("reler ativo: %v", err)
			}
			if !ativo {
				t.Errorf("conta alvo foi desativada — não deveria")
			}
		})
	}
}

// TestDesativarUsuarioHandler_AdmSobreGestor200 prova a linha "por adm sobre
// gestor": ação aplicada.
func TestDesativarUsuarioHandler_AdmSobreGestor200(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Adm", "h-adm-sobre-g@empresa.com", "senha-123456", "adm")
	alvoID := criarContaComPapel(t, db, "Gestor alvo", "h-gestor-alvo-adm@empresa.com", "senha-123456", "gestor")
	admToken := tokenDeLogin(t, db, "h-adm-sobre-g@empresa.com", "senha-123456")

	w := postDesativacao(db, alvoID, "Bearer "+admToken, `{"ativo":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if b := decodeUsuarioGestao(t, w.Body.Bytes()); b.Usuario == nil || b.Usuario.Ativo {
		t.Fatalf("resposta = %s, want usuario.ativo=false", w.Body.String())
	}
}

// TestDesativarUsuarioHandler_Inexistente404 e _IDMalformado404 provam que
// `id` sem linha e `id` não-UUID caem os dois em 404 NOT_FOUND.
func TestDesativarUsuarioHandler_Inexistente404(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Gestora", "h-gestor-404@empresa.com", "senha-123456", "gestor")
	token := tokenDeLogin(t, db, "h-gestor-404@empresa.com", "senha-123456")

	casos := []struct{ nome, id string }{
		{"uuid inexistente", "00000000-0000-0000-0000-000000000000"},
		{"id malformado", "nao-e-uuid"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w := postDesativacao(db, c.id, "Bearer "+token, `{"ativo":false}`)
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "NOT_FOUND" {
				t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
			}
		})
	}
}

// TestDesativarUsuarioHandler_CorpoInvalido400 cobre a linha "Desativacao
// sem/má chave": um corpo sem `ativo` (nil) viraria `ativo=false` num `bool`
// puro — precisa ser 400, nunca uma desativação silenciosa.
func TestDesativarUsuarioHandler_CorpoInvalido400(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Gestora", "h-gestor-inv@empresa.com", "senha-123456", "gestor")
	alvoID := criarContaComPapel(t, db, "Alvo", "h-alvo-inv@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "h-gestor-inv@empresa.com", "senha-123456")

	casos := []struct{ nome, corpo string }{
		{"JSON malformado", `{isto nao e json`},
		{"objeto vazio", `{}`},
		{"ativo null", `{"ativo":null}`},
		{"chave errada", `{"ativado":true}`},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w := postDesativacao(db, alvoID, "Bearer "+token, c.corpo)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
				t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
			}
			var ativo bool
			if err := db.QueryRow(`SELECT ativo FROM usuarios WHERE id = $1`, alvoID).Scan(&ativo); err != nil {
				t.Fatalf("reler ativo: %v", err)
			}
			if !ativo {
				t.Errorf("conta alvo foi desativada silenciosamente — não deveria")
			}
		})
	}
}

// TestDesativarUsuarioHandler_SemTokenOuPapelInsuficiente prova a linha "sem
// token / papel < gestor": RequireAuth/RequireRole respondem antes do handler.
func TestDesativarUsuarioHandler_SemTokenOuPapelInsuficiente(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Almox", "h-almox-semtoken@empresa.com", "senha-123456", "almoxarife")
	alvoID := criarContaComPapel(t, db, "Alvo", "h-alvo-semtoken@empresa.com", "senha-123456", "usuario")
	almoxToken := tokenDeLogin(t, db, "h-almox-semtoken@empresa.com", "senha-123456")

	wSem := postDesativacao(db, alvoID, "", `{"ativo":false}`)
	if wSem.Code != http.StatusUnauthorized {
		t.Fatalf("sem token: status = %d, want 401 (body=%s)", wSem.Code, wSem.Body.String())
	}
	wPapel := postDesativacao(db, alvoID, "Bearer "+almoxToken, `{"ativo":false}`)
	if wPapel.Code != http.StatusForbidden {
		t.Fatalf("papel almoxarife: status = %d, want 403 (body=%s)", wPapel.Code, wPapel.Body.String())
	}
}

// --- POST /api/usuarios/{id}/rebaixamento --------------------------------

// TestRebaixarUsuarioHandler_GestorPorAdmEEfeitoImediato prova a AC5 na
// fronteira: adm rebaixa um gestor -> 200 {"usuario":{papel:"almoxarife"}}; e a
// PRÓXIMA requisição daquela conta a uma rota RequireRole(gestor) já recebe 403
// (o middleware relê o papel do Postgres, sem re-login).
func TestRebaixarUsuarioHandler_GestorPorAdmEEfeitoImediato(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Adm", "h-adm-rebaixa@empresa.com", "senha-123456", "adm")
	criarContaComPapel(t, db, "Gestor alvo", "h-gestor-rebaixado@empresa.com", "senha-123456", "gestor")
	admToken := tokenDeLogin(t, db, "h-adm-rebaixa@empresa.com", "senha-123456")
	alvoToken := tokenDeLogin(t, db, "h-gestor-rebaixado@empresa.com", "senha-123456")

	// Antes: o token do alvo passa no gate RequireRole(gestor).
	if w := getUsuarios(db, "Bearer "+alvoToken); w.Code != http.StatusOK {
		t.Fatalf("antes: GET /api/usuarios status = %d, want 200", w.Code)
	}

	alvoID := ""
	if err := db.QueryRow(`SELECT id FROM usuarios WHERE email = $1`, "h-gestor-rebaixado@empresa.com").Scan(&alvoID); err != nil {
		t.Fatalf("id do alvo: %v", err)
	}

	w := postRebaixamento(db, alvoID, "Bearer "+admToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if b := decodeUsuarioGestao(t, w.Body.Bytes()); b.Usuario == nil || b.Usuario.Papel != "almoxarife" {
		t.Fatalf("resposta = %s, want usuario.papel=almoxarife", w.Body.String())
	}

	// Depois: a MESMA sessão do alvo já não passa no gate.
	if w := getUsuarios(db, "Bearer "+alvoToken); w.Code != http.StatusForbidden {
		t.Fatalf("depois: GET /api/usuarios status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
}

// TestRebaixarUsuarioHandler_AlmoxarifePorGestor200 prova a linha "Rebaixar
// almoxarife por gestor": papel vira `usuario`.
func TestRebaixarUsuarioHandler_AlmoxarifePorGestor200(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Gestora", "h-gestor-reb-a@empresa.com", "senha-123456", "gestor")
	alvoID := criarContaComPapel(t, db, "Almox alvo", "h-almox-reb@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "h-gestor-reb-a@empresa.com", "senha-123456")

	w := postRebaixamento(db, alvoID, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if b := decodeUsuarioGestao(t, w.Body.Bytes()); b.Usuario == nil || b.Usuario.Papel != "usuario" {
		t.Fatalf("resposta = %s, want usuario.papel=usuario", w.Body.String())
	}
}

// TestRebaixarUsuarioHandler_JaUsuario409 prova a linha "Rebaixar conta já
// usuario": 409 CONFLICT.
func TestRebaixarUsuarioHandler_JaUsuario409(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Gestora", "h-gestor-reb-u@empresa.com", "senha-123456", "gestor")
	alvoID := criarContaComPapel(t, db, "Usuário piso", "h-usuario-piso@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "h-gestor-reb-u@empresa.com", "senha-123456")

	w := postRebaixamento(db, alvoID, "Bearer "+token)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

// TestRebaixarUsuarioHandler_EscopoEAutoAcao403 prova que os guards de escopo e
// auto-ação valem igualmente para o rebaixamento.
func TestRebaixarUsuarioHandler_EscopoEAutoAcao403(t *testing.T) {
	db := testDB(t)
	gestorID := criarContaComPapel(t, db, "Gestora", "h-gestor-reb-escopo@empresa.com", "senha-123456", "gestor")
	admID := criarContaComPapel(t, db, "Adm", "h-adm-reb-escopo@empresa.com", "senha-123456", "adm")
	outroGestorID := criarContaComPapel(t, db, "Outro", "h-gestor2-reb-escopo@empresa.com", "senha-123456", "gestor")
	gestorToken := tokenDeLogin(t, db, "h-gestor-reb-escopo@empresa.com", "senha-123456")
	admToken := tokenDeLogin(t, db, "h-adm-reb-escopo@empresa.com", "senha-123456")

	casos := []struct{ nome, alvoID, token string }{
		{"gestor sobre gestor", outroGestorID, gestorToken},
		{"gestor sobre adm", admID, gestorToken},
		{"gestor sobre si", gestorID, gestorToken},
		{"adm sobre si", admID, admToken},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w := postRebaixamento(db, c.alvoID, "Bearer "+c.token)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "FORBIDDEN" {
				t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
			}
		})
	}
}

// TestRebaixarUsuarioHandler_Inexistente404 prova `id` sem linha / não-UUID.
func TestRebaixarUsuarioHandler_Inexistente404(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Gestora", "h-gestor-reb-404@empresa.com", "senha-123456", "gestor")
	token := tokenDeLogin(t, db, "h-gestor-reb-404@empresa.com", "senha-123456")

	for _, id := range []string{"00000000-0000-0000-0000-000000000000", "nao-e-uuid"} {
		w := postRebaixamento(db, id, "Bearer "+token)
		if w.Code != http.StatusNotFound {
			t.Fatalf("id %q: status = %d, want 404 (body=%s)", id, w.Code, w.Body.String())
		}
		if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "NOT_FOUND" {
			t.Errorf("id %q: code = %q, want NOT_FOUND", id, env.Error.Code)
		}
	}
}

// TestRebaixarUsuarioHandler_SemTokenOuPapelInsuficiente prova a linha "sem
// token / papel < gestor" na rota de rebaixamento.
func TestRebaixarUsuarioHandler_SemTokenOuPapelInsuficiente(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Usuária", "h-usuario-reb-semtoken@empresa.com", "senha-123456", "usuario")
	alvoID := criarContaComPapel(t, db, "Alvo", "h-alvo-reb-semtoken@empresa.com", "senha-123456", "almoxarife")
	usuarioToken := tokenDeLogin(t, db, "h-usuario-reb-semtoken@empresa.com", "senha-123456")

	if w := postRebaixamento(db, alvoID, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("sem token: status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
	if w := postRebaixamento(db, alvoID, "Bearer "+usuarioToken); w.Code != http.StatusForbidden {
		t.Fatalf("papel usuario: status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
}
