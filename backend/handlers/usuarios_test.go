package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// criarContaComPapel insere uma conta ativa/verificada com papel e senha
// controlados — criarUsuarioLogin (auth_test.go) só cria papel 'usuario'.
func criarContaComPapel(t *testing.T, db *sql.DB, nome, email, senha, papel string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("falha ao gerar hash: %v", err)
	}
	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
		VALUES ($1, $2, $3, $4, true, true)
		RETURNING id`
	if err := db.QueryRow(insert, nome, email, string(hash), papel).Scan(&id); err != nil {
		t.Fatalf("falha ao criar conta %q: %v", email, err)
	}
	return id
}

// tokenDeLogin faz um login real e devolve o access token — o mesmo caminho
// que o frontend percorre.
func tokenDeLogin(t *testing.T, db *sql.DB, email, senha string) string {
	t.Helper()
	w := postLogin(db, `{"email":"`+email+`","senha":"`+senha+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("login (%s): status = %d, want %d (body=%s)", email, w.Code, http.StatusOK, w.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("falha ao decodificar login: %v", err)
	}
	return body.Token
}

// getUsuarios despacha através da MESMA composição registrada em newMux
// (main.go): RequireAuth -> RequireRole("gestor") -> ListarUsuariosHandler —
// nunca chama o handler isoladamente, para provar o contrato real da rota.
func getUsuarios(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/usuarios", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	middleware.RequireAuth(db, testJWTSecret)(
		middleware.RequireRole(services.PapelGestor)(
			ListarUsuariosHandler(db)))(w, req)
	return w
}

type usuariosResposta struct {
	Usuarios []services.UsuarioResumo `json:"usuarios"`
}

func decodeUsuarios(t *testing.T, body []byte) usuariosResposta {
	t.Helper()
	var resp usuariosResposta
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("falha ao decodificar resposta de usuários: %v (body=%s)", err, body)
	}
	return resp
}

// TestListarUsuariosHandler_403ParaPapelInsuficiente prova a AC1 na fronteira
// HTTP: uma conta `usuario` ou `almoxarife` que chama GET /api/usuarios
// diretamente recebe 403 FORBIDDEN e o handler de listagem nunca executa (o
// corpo é o envelope de erro, nunca `{"usuarios":...}`).
func TestListarUsuariosHandler_403ParaPapelInsuficiente(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Usuária", "listar-usuario@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Almoxarife", "listar-almox@empresa.com", "senha-123456", "almoxarife")

	casos := []struct{ nome, email string }{
		{"papel usuario", "listar-usuario@empresa.com"},
		{"papel almoxarife", "listar-almox@empresa.com"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			token := tokenDeLogin(t, db, c.email, "senha-123456")
			w := getUsuarios(db, "Bearer "+token)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
			}
			env := decodeErro(t, w.Body.Bytes())
			if env.Error.Code != "FORBIDDEN" {
				t.Errorf("code = %q, want %q", env.Error.Code, "FORBIDDEN")
			}
		})
	}
}

// TestListarUsuariosHandler_GestorRecebeRecorte prova a AC1+AC3: um gestor
// recebe 200 e apenas contas `usuario`/`almoxarife`.
func TestListarUsuariosHandler_GestorRecebeRecorte(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Usuária", "recorte-usuario@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Almoxarife", "recorte-almox@empresa.com", "senha-123456", "almoxarife")
	criarContaComPapel(t, db, "Gestora", "recorte-gestor@empresa.com", "senha-123456", "gestor")
	criarContaComPapel(t, db, "Adm", "recorte-adm@empresa.com", "senha-123456", "adm")

	token := tokenDeLogin(t, db, "recorte-gestor@empresa.com", "senha-123456")
	w := getUsuarios(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeUsuarios(t, w.Body.Bytes())
	if len(resp.Usuarios) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(resp.Usuarios), resp.Usuarios)
	}
	for _, u := range resp.Usuarios {
		if u.Papel != "usuario" && u.Papel != "almoxarife" {
			t.Errorf("gestor recebeu conta de papel %q — fora do escopo", u.Papel)
		}
	}
}

// TestListarUsuariosHandler_AdmRecebeTudo prova a AC3: um adm recebe 200 e
// todas as contas, incluindo `gestor`/`adm`.
func TestListarUsuariosHandler_AdmRecebeTudo(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Usuária", "tudo-usuario@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Almoxarife", "tudo-almox@empresa.com", "senha-123456", "almoxarife")
	criarContaComPapel(t, db, "Gestora", "tudo-gestor@empresa.com", "senha-123456", "gestor")
	criarContaComPapel(t, db, "Adm", "tudo-adm@empresa.com", "senha-123456", "adm")

	token := tokenDeLogin(t, db, "tudo-adm@empresa.com", "senha-123456")
	w := getUsuarios(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeUsuarios(t, w.Body.Bytes())
	if len(resp.Usuarios) != 4 {
		t.Fatalf("len = %d, want 4 (%+v)", len(resp.Usuarios), resp.Usuarios)
	}
	papeis := map[string]bool{}
	for _, u := range resp.Usuarios {
		papeis[u.Papel] = true
	}
	for _, p := range []string{"usuario", "almoxarife", "gestor", "adm"} {
		if !papeis[p] {
			t.Errorf("adm não recebeu conta de papel %q", p)
		}
	}
}

// TestListarUsuariosHandler_SemToken401 prova o cenário "sem token em rota
// gestor+" da I/O Matrix: RequireAuth responde 401 TOKEN_EXPIRED antes de
// RequireRole rodar.
func TestListarUsuariosHandler_SemToken401(t *testing.T) {
	db := testDB(t)

	w := getUsuarios(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}
}
