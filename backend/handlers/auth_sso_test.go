package handlers

import (
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"stockflow/backend/iam"
)

const (
	ssoIss = "https://kc.example/realms/ferreiracosta"
	ssoAzp = "stockflow-web"
	ssoKid = "kid-sso-1"
)

func ssoGerarChave(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gerar chave RSA: %v", err)
	}
	return k
}

func ssoJWKSBody(pub *rsa.PublicKey) string {
	doc := map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"kid": ssoKid,
			"use": "sig",
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}},
	}
	b, _ := json.Marshal(doc)
	return string(b)
}

func ssoJWKSServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func ssoClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss":            ssoIss,
		"azp":            ssoAzp,
		"email":          "carlos@fc.com",
		"email_verified": true,
		"exp":            time.Now().Add(5 * time.Minute).Unix(),
		"iat":            time.Now().Add(-time.Minute).Unix(),
	}
}

func ssoAssinar(t *testing.T, priv *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("assinar token: %v", err)
	}
	return s
}

// ssoHandler monta a MESMA composição de newMux: iam.Middleware(jwks, cfg)
// envolvendo KeycloakSSOHandler. jwksURL vazio usa um JWKS falso servindo ssoKid.
func ssoHandler(t *testing.T, db *sql.DB, priv *rsa.PrivateKey, jwksURL string) http.HandlerFunc {
	t.Helper()
	if jwksURL == "" {
		srv := ssoJWKSServer(t, ssoJWKSBody(&priv.PublicKey), http.StatusOK)
		jwksURL = srv.URL
	}
	jwks := iam.NewJWKSClient(jwksURL, time.Hour)
	cfg := iam.Config{RealmURL: ssoIss, AllowedClientIDs: []string{ssoAzp}}
	return iam.Middleware(jwks, cfg)(KeycloakSSOHandler(db, testJWTSecret))
}

func postSSOKeycloak(h http.HandlerFunc, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/sso/keycloak", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func contarSessoes(t *testing.T, db *sql.DB, usuarioID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sessoes WHERE usuario_id = $1`, usuarioID).Scan(&n); err != nil {
		t.Fatalf("contar sessões: %v", err)
	}
	return n
}

// --- Troca de token (POST /api/auth/sso/keycloak) ---

func TestKeycloakSSO_TrocaValida(t *testing.T) {
	db := testDB(t)
	priv := ssoGerarChave(t)
	id := criarContaComPapel(t, db, "Carlos", "carlos@fc.com", "senha-123456", "usuario")
	h := ssoHandler(t, db, priv, "")

	w := postSSOKeycloak(h, ssoAssinar(t, priv, ssoKid, ssoClaims()))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	var body struct {
		Token   string `json:"token"`
		Usuario struct {
			ID    string `json:"id"`
			Papel string `json:"papel"`
		} `json:"usuario"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Token == "" || body.Usuario.ID != id {
		t.Fatalf("resposta = %+v, want token + usuario.id=%s", body, id)
	}
	if contarSessoes(t, db, id) != 1 {
		t.Fatalf("sessões = %d, want 1", contarSessoes(t, db, id))
	}

	var temCookie bool
	for _, c := range w.Result().Cookies() {
		if c.Name == refreshTokenCookieName {
			temCookie = true
			if !c.HttpOnly {
				t.Error("cookie refresh_token deveria ser HttpOnly")
			}
		}
	}
	if !temCookie {
		t.Fatal("Set-Cookie refresh_token ausente na resposta")
	}
}

// TestKeycloakSSO_ContaBloqueadaPorSenhaAindaEntra prova a AC4 / a linha "SSO
// com conta bloqueada por senha" da I/O Matrix: uma conta com
// tentativas_login_falhas=5 e bloqueado_ate no futuro (bloqueio do login por
// senha, Story 1.10) continua autenticando por SSO, e o caminho SSO não lê nem
// altera essas colunas.
func TestKeycloakSSO_ContaBloqueadaPorSenhaAindaEntra(t *testing.T) {
	db := testDB(t)
	priv := ssoGerarChave(t)
	id := criarContaComPapel(t, db, "Carlos", "carlos@fc.com", "senha-123456", "usuario")
	if _, err := db.Exec(
		`UPDATE usuarios SET tentativas_login_falhas = 5, bloqueado_ate = now() + interval '15 minutes' WHERE id = $1`, id,
	); err != nil {
		t.Fatalf("falha ao bloquear conta: %v", err)
	}

	h := ssoHandler(t, db, priv, "")
	w := postSSOKeycloak(h, ssoAssinar(t, priv, ssoKid, ssoClaims()))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — SSO não deve ser afetado pelo bloqueio de senha (body=%s)", w.Code, w.Body.String())
	}

	var temCookie bool
	for _, c := range w.Result().Cookies() {
		if c.Name == refreshTokenCookieName {
			temCookie = true
		}
	}
	if !temCookie {
		t.Fatal("Set-Cookie refresh_token ausente — a troca SSO deveria ter emitido sessão")
	}

	// As colunas de bloqueio continuam exatamente como antes: SSO não as tocou.
	var tentativas int
	var bloqueadoAte sql.NullTime
	if err := db.QueryRow(
		`SELECT tentativas_login_falhas, bloqueado_ate FROM usuarios WHERE id = $1`, id,
	).Scan(&tentativas, &bloqueadoAte); err != nil {
		t.Fatalf("reler colunas de bloqueio: %v", err)
	}
	if tentativas != 5 {
		t.Errorf("tentativas_login_falhas = %d, want 5 (inalterado pelo SSO)", tentativas)
	}
	if !bloqueadoAte.Valid || !bloqueadoAte.Time.After(time.Now()) {
		t.Errorf("bloqueado_ate = %v, want um instante no futuro (inalterado pelo SSO)", bloqueadoAte)
	}
}

func TestKeycloakSSO_PapelPreservadoDoBanco(t *testing.T) {
	db := testDB(t)
	priv := ssoGerarChave(t)
	criarContaComPapel(t, db, "Gestora", "carlos@fc.com", "senha-123456", "gestor")
	h := ssoHandler(t, db, priv, "")

	// O token não carrega papel; mesmo que carregasse, o handler ignora.
	w := postSSOKeycloak(h, ssoAssinar(t, priv, ssoKid, ssoClaims()))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body struct {
		Usuario struct {
			Papel string `json:"papel"`
		} `json:"usuario"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Usuario.Papel != "gestor" {
		t.Fatalf("papel = %q, want gestor (do banco)", body.Usuario.Papel)
	}
}

// TestKeycloakSSO_OrigemSSO prova que uma sessão de login federado é
// observável de ponta a ponta como origem="sso" (Story 1.11) — uma
// regressão pontual em KeycloakSSOHandler que trocasse o literal "sso" por
// "senha" na chamada de emitirSessaoEResponder quebraria a isenção de MFA
// para SSO sem que nenhum outro teste falhasse.
func TestKeycloakSSO_OrigemSSO(t *testing.T) {
	db := testDB(t)
	priv := ssoGerarChave(t)
	criarContaComPapel(t, db, "Gestora", "carlos@fc.com", "senha-123456", "gestor")
	h := ssoHandler(t, db, priv, "")

	w := postSSOKeycloak(h, ssoAssinar(t, priv, ssoKid, ssoClaims()))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body struct {
		Usuario struct {
			Origem string `json:"origem"`
		} `json:"usuario"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Usuario.Origem != "sso" {
		t.Fatalf("usuario.origem = %q, want %q", body.Usuario.Origem, "sso")
	}
}

func TestKeycloakSSO_EmailCaseInsensitive(t *testing.T) {
	db := testDB(t)
	priv := ssoGerarChave(t)
	id := criarContaComPapel(t, db, "Carlos", "carlos@fc.com", "senha-123456", "usuario")
	h := ssoHandler(t, db, priv, "")

	c := ssoClaims()
	c["email"] = "Carlos@FC.com"
	w := postSSOKeycloak(h, ssoAssinar(t, priv, ssoKid, c))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body struct {
		Usuario struct {
			ID string `json:"id"`
		} `json:"usuario"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Usuario.ID != id {
		t.Fatalf("id = %q, want %q", body.Usuario.ID, id)
	}
}

func TestKeycloakSSO_SemContaLocal(t *testing.T) {
	db := testDB(t)
	priv := ssoGerarChave(t)
	h := ssoHandler(t, db, priv, "")

	w := postSSOKeycloak(h, ssoAssinar(t, priv, ssoKid, ssoClaims()))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
	if got := decodeErro(t, w.Body.Bytes()).Error.Code; got != "SSO_SEM_CONTA" {
		t.Fatalf("code = %q, want SSO_SEM_CONTA", got)
	}
	if n := contarLinhasUsuarios(t, db); n != 0 {
		t.Fatalf("usuarios = %d, want 0 (nenhuma conta criada)", n)
	}
}

func TestKeycloakSSO_EmailNaoVerificado(t *testing.T) {
	db := testDB(t)
	priv := ssoGerarChave(t)
	criarContaComPapel(t, db, "Carlos", "carlos@fc.com", "senha-123456", "usuario")
	h := ssoHandler(t, db, priv, "")

	c := ssoClaims()
	c["email_verified"] = false
	w := postSSOKeycloak(h, ssoAssinar(t, priv, ssoKid, c))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
	if got := decodeErro(t, w.Body.Bytes()).Error.Code; got != "EMAIL_NOT_VERIFIED" {
		t.Fatalf("code = %q, want EMAIL_NOT_VERIFIED", got)
	}
}

func TestKeycloakSSO_ContaDesativada(t *testing.T) {
	db := testDB(t)
	priv := ssoGerarChave(t)
	criarUsuarioLoginComEstado(t, db, "carlos@fc.com", "", false, true) // ativo=false
	h := ssoHandler(t, db, priv, "")

	w := postSSOKeycloak(h, ssoAssinar(t, priv, ssoKid, ssoClaims()))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
	if got := decodeErro(t, w.Body.Bytes()).Error.Code; got != "INVALID_CREDENTIALS" {
		t.Fatalf("code = %q, want INVALID_CREDENTIALS", got)
	}
}

func TestKeycloakSSO_MiddlewareRejeitaAntesDoHandler(t *testing.T) {
	db := testDB(t)
	priv := ssoGerarChave(t)
	outra := ssoGerarChave(t)
	id := criarContaComPapel(t, db, "Carlos", "carlos@fc.com", "senha-123456", "usuario")

	casos := []struct {
		nome   string
		bearer func() string
		bare   bool
	}{
		{nome: "sem Authorization", bare: true},
		{nome: "HS256", bearer: func() string {
			tok := jwt.NewWithClaims(jwt.SigningMethodHS256, ssoClaims())
			tok.Header["kid"] = ssoKid
			s, _ := tok.SignedString([]byte("segredo"))
			return s
		}},
		{nome: "none", bearer: func() string {
			tok := jwt.NewWithClaims(jwt.SigningMethodNone, ssoClaims())
			tok.Header["kid"] = ssoKid
			s, _ := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
			return s
		}},
		{nome: "assinado por outra chave", bearer: func() string {
			return ssoAssinar(t, outra, ssoKid, ssoClaims())
		}},
		{nome: "kid desconhecido", bearer: func() string {
			return ssoAssinar(t, priv, "kid-outro", ssoClaims())
		}},
		{nome: "azp fora da allowlist", bearer: func() string {
			c := ssoClaims()
			c["azp"] = "fb-apu02"
			return ssoAssinar(t, priv, ssoKid, c)
		}},
		{nome: "iss divergente", bearer: func() string {
			c := ssoClaims()
			c["iss"] = "https://kc.example/realms/outro"
			return ssoAssinar(t, priv, ssoKid, c)
		}},
		{nome: "exp ausente", bearer: func() string {
			c := ssoClaims()
			delete(c, "exp")
			return ssoAssinar(t, priv, ssoKid, c)
		}},
		{nome: "exp expirado", bearer: func() string {
			c := ssoClaims()
			c["exp"] = time.Now().Add(-2 * time.Minute).Unix()
			return ssoAssinar(t, priv, ssoKid, c)
		}},
	}

	for _, tc := range casos {
		t.Run(tc.nome, func(t *testing.T) {
			h := ssoHandler(t, db, priv, "")
			var bearer string
			if !tc.bare {
				bearer = tc.bearer()
			}
			w := postSSOKeycloak(h, bearer)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
			}
			if got := decodeErro(t, w.Body.Bytes()).Error.Code; got != "SSO_TOKEN_INVALIDO" {
				t.Fatalf("code = %q, want SSO_TOKEN_INVALIDO", got)
			}
			if contarSessoes(t, db, id) != 0 {
				t.Fatalf("KeycloakSSOHandler executou apesar do middleware rejeitar (sessões = %d)", contarSessoes(t, db, id))
			}
		})
	}
}

func TestKeycloakSSO_JWKSIndisponivel(t *testing.T) {
	db := testDB(t)
	priv := ssoGerarChave(t)
	criarContaComPapel(t, db, "Carlos", "carlos@fc.com", "senha-123456", "usuario")

	srv := ssoJWKSServer(t, `{"keys":[]}`, http.StatusServiceUnavailable)
	h := ssoHandler(t, db, priv, srv.URL)

	w := postSSOKeycloak(h, ssoAssinar(t, priv, ssoKid, ssoClaims()))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (JWKS 503 nunca vira sucesso) (body=%s)", w.Code, w.Body.String())
	}
}

// --- POST /api/auth/logout ---

func postLogout(db *sql.DB, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	LogoutHandler(db)(w, req)
	return w
}

func TestLogoutHandler_RevogaSessaoELimpaCookie(t *testing.T) {
	db := testDB(t)
	priv := ssoGerarChave(t)
	id := criarContaComPapel(t, db, "Carlos", "carlos@fc.com", "senha-123456", "usuario")

	// Emite uma sessão real via troca SSO e captura o cookie.
	h := ssoHandler(t, db, priv, "")
	wSSO := postSSOKeycloak(h, ssoAssinar(t, priv, ssoKid, ssoClaims()))
	if wSSO.Code != http.StatusOK {
		t.Fatalf("pré-condição: troca SSO status = %d (body=%s)", wSSO.Code, wSSO.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range wSSO.Result().Cookies() {
		if c.Name == refreshTokenCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("cookie de refresh não veio da troca SSO")
	}

	w := postLogout(db, cookie)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", w.Code, w.Body.String())
	}

	var revogadoEm sql.NullTime
	if err := db.QueryRow(`SELECT revogado_em FROM sessoes WHERE usuario_id = $1`, id).Scan(&revogadoEm); err != nil {
		t.Fatalf("consulta sessão: %v", err)
	}
	if !revogadoEm.Valid {
		t.Fatal("revogado_em nulo — o logout deveria ter revogado a sessão")
	}

	var limpou bool
	for _, c := range w.Result().Cookies() {
		if c.Name == refreshTokenCookieName && c.MaxAge < 0 {
			limpou = true
		}
	}
	if !limpou {
		t.Fatal("logout não emitiu Set-Cookie limpando o refresh_token")
	}
}

func TestLogoutHandler_Idempotente(t *testing.T) {
	db := testDB(t)

	// Sem cookie nenhum.
	w := postLogout(db, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("sem cookie: status = %d, want 204", w.Code)
	}

	// Cookie apontando para um token inexistente.
	w = postLogout(db, &http.Cookie{Name: refreshTokenCookieName, Value: "nao-existe"})
	if w.Code != http.StatusNoContent {
		t.Fatalf("cookie órfão: status = %d, want 204", w.Code)
	}
}

// --- GET /api/auth/sso/config ---

func getSSOConfig(cfg iam.Config) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/auth/sso/config", nil)
	w := httptest.NewRecorder()
	SSOConfigHandler(cfg)(w, req)
	return w
}

func TestSSOConfigHandler_ConfigIncompleta(t *testing.T) {
	t.Setenv("IAM_CLIENT_ID", "")
	t.Setenv("IAM_REDIRECT_URI", "")

	w := getSSOConfig(iam.Config{})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["enabled"] != false {
		t.Fatalf("body = %v, want {enabled:false}", body)
	}
	if _, temBaseURL := body["base_url"]; temBaseURL {
		t.Fatalf("body = %v, want nada além de enabled:false", body)
	}
}

func TestSSOConfigHandler_ConfigCompleta(t *testing.T) {
	t.Setenv("IAM_CLIENT_ID", "stockflow-web")
	t.Setenv("IAM_REDIRECT_URI", "https://app.example/auth/callback")
	t.Setenv("IAM_SCOPES", "")

	cfg := iam.Config{RealmURL: ssoIss, AllowedClientIDs: []string{ssoAzp}}
	w := getSSOConfig(cfg)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body struct {
		Enabled     bool   `json:"enabled"`
		BaseURL     string `json:"base_url"`
		ClientID    string `json:"client_id"`
		RedirectURI string `json:"redirect_uri"`
		Scopes      string `json:"scopes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Enabled || body.BaseURL != ssoIss || body.ClientID != "stockflow-web" ||
		body.RedirectURI != "https://app.example/auth/callback" || body.Scopes != "openid profile email" {
		t.Fatalf("body = %+v", body)
	}
}

func contarLinhasUsuarios(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM usuarios`).Scan(&n); err != nil {
		t.Fatalf("contar usuarios: %v", err)
	}
	return n
}
