package handlers

import (
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

var (
	migrateOnce sync.Once
	migrateErr  error
)

// testDB abre uma conexão contra DATABASE_URL, aplica as migrations reais do
// projeto e limpa usuarios/tokens_acao/emails_pendentes antes de cada teste —
// mesmo padrão de backend/services/auth_test.go e
// backend/cmd/seed-admin/main_test.go. Pula o teste quando nenhum Postgres
// foi configurado.
func testDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL não definido — suba o banco (docker compose up -d db) para rodar os testes de integração")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("falha ao abrir conexão: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := db.Ping(); err != nil {
		t.Fatalf("banco indisponível em %s: %v", dsn, err)
	}

	migrateOnce.Do(func() {
		for attempt := 1; attempt <= 5; attempt++ {
			var m *migrate.Migrate
			m, migrateErr = migrate.New("file://../migrations", dsn)
			if migrateErr == nil {
				migrateErr = m.Up()
				m.Close()
			}
			if migrateErr == nil || errors.Is(migrateErr, migrate.ErrNoChange) {
				migrateErr = nil
				return
			}
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}
	})
	if migrateErr != nil {
		t.Fatalf("falha ao aplicar migrations: %v", migrateErr)
	}

	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("falha ao limpar tabelas entre testes: %v", err)
	}

	return db
}

var testEmailCfg = services.EmailConfig{
	Host:     "smtp.invalid",
	Port:     "587",
	Password: "",
	From:     "stockflow <noreply@stockflow.local>",
	AppURL:   "http://test.local",
}

func decodeErro(t *testing.T, body []byte) erroEnvelope {
	t.Helper()
	var env erroEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("falha ao decodificar envelope de erro: %v (body=%s)", err, body)
	}
	return env
}

func postCadastro(db *sql.DB, jsonBody string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/cadastro", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	CadastroHandler(db, testEmailCfg)(w, req)
	return w
}

// TestCadastroHandler_Sucesso prova o cenário "Cadastro válido" na fronteira
// HTTP: 201 e mensagem de sucesso.
func TestCadastroHandler_Sucesso(t *testing.T) {
	db := testDB(t)

	w := postCadastro(db, `{"nome":"Fulano de Tal","email":"fulano@empresa.com","senha":"senha-123456"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
}

// TestCadastroHandler_PapelForjadoIgnorado prova a AC desta story: mesmo com
// `"papel":"adm"` no payload, a conta é criada com papel='usuario'.
func TestCadastroHandler_PapelForjadoIgnorado(t *testing.T) {
	db := testDB(t)

	w := postCadastro(db, `{"nome":"Forjador","email":"forjador@empresa.com","senha":"senha-123456","papel":"adm"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}

	var papel string
	if err := db.QueryRow(`SELECT papel FROM usuarios WHERE email = 'forjador@empresa.com'`).Scan(&papel); err != nil {
		t.Fatalf("falha ao ler conta criada: %v", err)
	}
	if papel != "usuario" {
		t.Errorf("papel = %q, want %q — payload forjado não pode decidir o papel", papel, "usuario")
	}
}

// TestCadastroHandler_EmailDuplicado prova o envelope de erro CONFLICT (AD-14)
// para o cenário "E-mail duplicado" da I/O Matrix, com a mesma normalização
// usada para o e-mail já existente.
func TestCadastroHandler_EmailDuplicado(t *testing.T) {
	db := testDB(t)

	w1 := postCadastro(db, `{"nome":"Primeiro","email":"Duplicado@Empresa.com","senha":"senha-123456"}`)
	if w1.Code != http.StatusCreated {
		t.Fatalf("primeiro cadastro: status = %d, want %d", w1.Code, http.StatusCreated)
	}

	w2 := postCadastro(db, `{"nome":"Segundo","email":"duplicado@empresa.com","senha":"outra-senha1"}`)
	if w2.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", w2.Code, http.StatusConflict, w2.Body.String())
	}
	env := decodeErro(t, w2.Body.Bytes())
	if env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want %q", env.Error.Code, "CONFLICT")
	}
}

// TestCadastroHandler_CampoObrigatorioVazio prova o envelope VALIDATION_ERROR
// (AD-14) para o cenário "Campo obrigatório ausente/vazio".
func TestCadastroHandler_CampoObrigatorioVazio(t *testing.T) {
	db := testDB(t)

	w := postCadastro(db, `{"nome":"","email":"semanome@empresa.com","senha":"senha-123456"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want %q", env.Error.Code, "VALIDATION_ERROR")
	}
}

// TestCadastroHandler_PayloadInvalido prova que um corpo que nem sequer é
// JSON válido também retorna VALIDATION_ERROR, nunca 500.
func TestCadastroHandler_PayloadInvalido(t *testing.T) {
	db := testDB(t)

	w := postCadastro(db, `{isto nao é json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want %q", env.Error.Code, "VALIDATION_ERROR")
	}
}

// TestCadastroHandler_CorpoMuitoGrande prova que cadastroRequestMaxBytes
// (64KB) rejeita um corpo maior antes mesmo de tentar decodificar — sem essa
// cobertura, o limite poderia ser removido ou desalinhado silenciosamente
// (nenhum teste existente envia um corpo perto desse tamanho).
func TestCadastroHandler_CorpoMuitoGrande(t *testing.T) {
	db := testDB(t)

	nomeGigante := strings.Repeat("a", 70*1024)
	w := postCadastro(db, `{"nome":"`+nomeGigante+`","email":"grande@empresa.com","senha":"senha-123456"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want %q", env.Error.Code, "VALIDATION_ERROR")
	}
}

func getVerificarEmail(db *sql.DB, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/auth/verificar-email?token="+token, nil)
	w := httptest.NewRecorder()
	VerificarEmailHandler(db)(w, req)
	return w
}

// TestVerificarEmailHandler_Sucesso prova o cenário "Link de verificação
// válido" na fronteira HTTP: 200 e email_verificado=true no banco.
func TestVerificarEmailHandler_Sucesso(t *testing.T) {
	db := testDB(t)
	usuarioID, err := services.Cadastrar(db, testEmailCfg, "Verificando", "verificando@empresa.com", "senha-123456")
	if err != nil {
		t.Fatalf("Cadastrar falhou: %v", err)
	}
	var token string
	if err := db.QueryRow(`SELECT token FROM tokens_acao WHERE usuario_id = $1`, usuarioID).Scan(&token); err != nil {
		t.Fatalf("falha ao ler token: %v", err)
	}

	w := getVerificarEmail(db, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}

	var emailVerificado bool
	if err := db.QueryRow(`SELECT email_verificado FROM usuarios WHERE id = $1`, usuarioID).Scan(&emailVerificado); err != nil {
		t.Fatalf("falha ao reler usuario: %v", err)
	}
	if !emailVerificado {
		t.Error("email_verificado = false, want true")
	}
}

// TestVerificarEmailHandler_TokenInexistente prova o envelope NOT_FOUND
// (AD-14) para o cenário "Token inexistente/malformado".
func TestVerificarEmailHandler_TokenInexistente(t *testing.T) {
	db := testDB(t)

	w := getVerificarEmail(db, "token-nunca-existiu")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want %q", env.Error.Code, "NOT_FOUND")
	}
}

// TestVerificarEmailHandler_TokenAusente prova que uma query string sem
// `?token=` (distinto de um token presente mas inexistente) também retorna
// NOT_FOUND — VerificarEmailHandler nunca trata token vazio como caso
// especial, só repassa a string vazia para services.VerificarEmail, que não
// encontra nenhuma linha com token=”.
func TestVerificarEmailHandler_TokenAusente(t *testing.T) {
	db := testDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/verificar-email", nil)
	w := httptest.NewRecorder()
	VerificarEmailHandler(db)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want %q", env.Error.Code, "NOT_FOUND")
	}
}

// TestVerificarEmailHandler_TokenExpirado prova o envelope TOKEN_EXPIRED
// (AD-14) para o cenário "Link expirado".
func TestVerificarEmailHandler_TokenExpirado(t *testing.T) {
	db := testDB(t)
	usuarioID, err := services.Cadastrar(db, testEmailCfg, "Expirando", "expirando@empresa.com", "senha-123456")
	if err != nil {
		t.Fatalf("Cadastrar falhou: %v", err)
	}
	var token string
	if err := db.QueryRow(`SELECT token FROM tokens_acao WHERE usuario_id = $1`, usuarioID).Scan(&token); err != nil {
		t.Fatalf("falha ao ler token: %v", err)
	}
	if _, err := db.Exec(`UPDATE tokens_acao SET expira_em = now() - interval '1 hour' WHERE token = $1`, token); err != nil {
		t.Fatalf("falha ao forçar expiração: %v", err)
	}

	w := getVerificarEmail(db, token)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}
}

// TestVerificarEmailHandler_TokenJaUsado prova o envelope TOKEN_EXPIRED
// (AD-14) para o cenário "Link já usado" — idempotente, sem reaplicar o
// efeito.
func TestVerificarEmailHandler_TokenJaUsado(t *testing.T) {
	db := testDB(t)
	usuarioID, err := services.Cadastrar(db, testEmailCfg, "Reusando", "reusando@empresa.com", "senha-123456")
	if err != nil {
		t.Fatalf("Cadastrar falhou: %v", err)
	}
	var token string
	if err := db.QueryRow(`SELECT token FROM tokens_acao WHERE usuario_id = $1`, usuarioID).Scan(&token); err != nil {
		t.Fatalf("falha ao ler token: %v", err)
	}

	if w := getVerificarEmail(db, token); w.Code != http.StatusOK {
		t.Fatalf("primeira verificação: status = %d, want %d", w.Code, http.StatusOK)
	}

	w := getVerificarEmail(db, token)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("segunda verificação: status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}
}

// testJWTSecret é o segredo usado para assinar/validar tokens de sessão
// nesta suíte — mesmo padrão de testEmailCfg acima.
var testJWTSecret = []byte("segredo-de-teste-nao-usar-em-producao")

// criarUsuarioLogin insere uma conta ativa/verificada com senha conhecida
// diretamente em usuarios — services.Cadastrar sempre cria
// email_verificado=false, então não serve para exercitar o caminho feliz de
// login sem um passo extra de verificação.
func criarUsuarioLogin(t *testing.T, db *sql.DB, email, senha string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("falha ao gerar hash de senha de teste: %v", err)
	}
	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
		VALUES ('Usuário Teste', $1, $2, 'usuario', true, true)
		RETURNING id`
	if err := db.QueryRow(insert, email, string(hash)).Scan(&id); err != nil {
		t.Fatalf("falha ao criar usuario de teste: %v", err)
	}
	return id
}

// criarUsuarioLoginComEstado é a variante de criarUsuarioLogin com controle
// total sobre ativo/emailVerificado/senha (senha vazia grava senha_hash
// NULL, conta só-SSO) — necessária para provar na fronteira HTTP os 3
// sub-casos de "credenciais inválidas" que criarUsuarioLogin (sempre
// ativo=true/email_verificado=true) não alcança.
func criarUsuarioLoginComEstado(t *testing.T, db *sql.DB, email, senha string, ativo, emailVerificado bool) string {
	t.Helper()
	var senhaHash sql.NullString
	if senha != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
		if err != nil {
			t.Fatalf("falha ao gerar hash de senha de teste: %v", err)
		}
		senhaHash = sql.NullString{String: string(hash), Valid: true}
	}
	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
		VALUES ('Usuário Teste', $1, $2, 'usuario', $3, $4)
		RETURNING id`
	if err := db.QueryRow(insert, email, senhaHash, emailVerificado, ativo).Scan(&id); err != nil {
		t.Fatalf("falha ao criar usuario de teste: %v", err)
	}
	return id
}

func postLogin(db *sql.DB, jsonBody string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	LoginHandler(db, testJWTSecret)(w, req)
	return w
}

// refreshCookieDoResultado extrai o Set-Cookie de nome refresh_token de uma
// resposta gravada — usado para encadear login->refresh nos testes abaixo.
func refreshCookieDoResultado(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	res := w.Result()
	for _, c := range res.Cookies() {
		if c.Name == refreshTokenCookieName {
			return c
		}
	}
	t.Fatal("cookie refresh_token não encontrado na resposta")
	return nil
}

func postRefresh(db *sql.DB, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	RefreshHandler(db, testJWTSecret)(w, req)
	return w
}

// getMe despacha através da MESMA composição usada em produção
// (main.go: middleware.RequireAuth(db, jwtSecret)(handlers.MeHandler())) —
// nunca chama MeHandler isoladamente, para provar o contrato real da rota.
func getMe(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	middleware.RequireAuth(db, testJWTSecret)(MeHandler())(w, req)
	return w
}

// TestLoginHandler_Sucesso prova o cenário "Login válido" na fronteira HTTP
// (Story 1.4): 200, corpo com token+usuario, e cookie refresh_token
// HttpOnly/Path=/api/auth/SameSite=Lax setado.
func TestLoginHandler_Sucesso(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioLogin(t, db, "login-handler-ok@empresa.com", "senha-123456")

	w := postLogin(db, `{"email":"Login-Handler-OK@Empresa.com","senha":"senha-123456"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}

	var body struct {
		Token   string `json:"token"`
		Usuario struct {
			ID    string `json:"id"`
			Nome  string `json:"nome"`
			Email string `json:"email"`
			Papel string `json:"papel"`
		} `json:"usuario"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("falha ao decodificar corpo: %v (body=%s)", err, w.Body.String())
	}
	if body.Token == "" {
		t.Error("token vazio")
	}
	if body.Usuario.ID != usuarioID {
		t.Errorf("usuario.id = %q, want %q", body.Usuario.ID, usuarioID)
	}
	if body.Usuario.Email != "login-handler-ok@empresa.com" {
		t.Errorf("usuario.email = %q, want %q", body.Usuario.Email, "login-handler-ok@empresa.com")
	}
	if body.Usuario.Papel != "usuario" {
		t.Errorf("usuario.papel = %q, want %q", body.Usuario.Papel, "usuario")
	}

	cookie := refreshCookieDoResultado(t, w)
	if cookie.Value == "" {
		t.Error("cookie refresh_token com valor vazio")
	}
	if !cookie.HttpOnly {
		t.Error("cookie refresh_token não é HttpOnly")
	}
	if cookie.Path != "/api/auth" {
		t.Errorf("cookie Path = %q, want %q", cookie.Path, "/api/auth")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie SameSite = %v, want SameSiteLaxMode", cookie.SameSite)
	}
	// postLogin monta uma requisição HTTP simples (sem req.TLS nem
	// X-Forwarded-Proto) — cookieEhSeguro deve devolver false aqui. O caminho
	// Secure=true (TLS direto ou proxy HTTPS) é coberto por
	// TestLoginHandler_CookieSecure abaixo.
	if cookie.Secure {
		t.Error("cookie refresh_token Secure = true em requisição HTTP simples, want false")
	}
	// Max-Age precisa refletir o TTL real da sessão emitida (services.
	// RefreshTokenExpiracao) — sem esta asserção, uma regressão que zerasse ou
	// invertesse o cálculo do Max-Age passaria despercebida (só os testes de
	// cookie LIMPO checam MaxAge hoje).
	if cookie.MaxAge <= 0 {
		t.Errorf("cookie MaxAge = %d, want > 0", cookie.MaxAge)
	}
	wantMaxAge := int(services.RefreshTokenExpiracao.Seconds())
	if diff := wantMaxAge - cookie.MaxAge; diff < -60 || diff > 60 {
		t.Errorf("cookie MaxAge = %d, want ~%d (dentro de 60s)", cookie.MaxAge, wantMaxAge)
	}

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sessoes WHERE usuario_id = $1 AND refresh_token = $2`, usuarioID, cookie.Value).Scan(&count); err != nil {
		t.Fatalf("falha ao consultar sessoes: %v", err)
	}
	if count != 1 {
		t.Errorf("linhas em sessoes para o refresh emitido = %d, want 1", count)
	}
}

// TestLoginHandler_CookieSecure prova as duas pontas de cookieEhSeguro que
// TestLoginHandler_Sucesso não cobre (ele só exercita o caminho HTTP simples):
// Secure=true quando a requisição chega via TLS direto (req.TLS setado) ou
// atrás de um proxy reverso que declara X-Forwarded-Proto: https — sem este
// teste, uma regressão em cookieEhSeguro (lógica invertida ou valor fixo)
// passaria despercebida, já que nenhum teste existente seta req.TLS nem esse
// header.
func TestLoginHandler_CookieSecure(t *testing.T) {
	casos := []struct {
		nome       string
		email      string
		configurar func(r *http.Request)
	}{
		{
			nome:       "TLS direto",
			email:      "cookie-secure-tls@empresa.com",
			configurar: func(r *http.Request) { r.TLS = &tls.ConnectionState{} },
		},
		{
			nome:       "proxy com X-Forwarded-Proto https",
			email:      "cookie-secure-proxy@empresa.com",
			configurar: func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") },
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			db := testDB(t)
			criarUsuarioLogin(t, db, c.email, "senha-123456")

			req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"`+c.email+`","senha":"senha-123456"}`))
			req.Header.Set("Content-Type", "application/json")
			c.configurar(req)
			w := httptest.NewRecorder()
			LoginHandler(db, testJWTSecret)(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
			}
			cookie := refreshCookieDoResultado(t, w)
			if !cookie.Secure {
				t.Error("cookie refresh_token Secure = false, want true")
			}
		})
	}
}

// TestLoginHandler_CredenciaisInvalidas prova o envelope 401
// INVALID_CREDENTIALS (I/O Matrix) para senha errada e e-mail inexistente —
// a MESMA mensagem genérica para os dois casos.
func TestLoginHandler_CredenciaisInvalidas(t *testing.T) {
	db := testDB(t)
	criarUsuarioLogin(t, db, "login-handler-senha-errada@empresa.com", "senha-correta")
	criarUsuarioLoginComEstado(t, db, "login-handler-nao-verificado@empresa.com", "senha-123456", true, false)
	criarUsuarioLoginComEstado(t, db, "login-handler-desativado@empresa.com", "senha-123456", false, true)
	criarUsuarioLoginComEstado(t, db, "login-handler-so-sso@empresa.com", "", true, true)

	casos := []struct {
		nome, jsonBody string
	}{
		{"senha incorreta", `{"email":"login-handler-senha-errada@empresa.com","senha":"senha-incorreta"}`},
		{"e-mail inexistente", `{"email":"nunca-existiu-handler@empresa.com","senha":"qualquer-senha"}`},
		{"e-mail não verificado", `{"email":"login-handler-nao-verificado@empresa.com","senha":"senha-123456"}`},
		{"conta desativada", `{"email":"login-handler-desativado@empresa.com","senha":"senha-123456"}`},
		{"conta só-SSO (senha_hash nulo)", `{"email":"login-handler-so-sso@empresa.com","senha":"qualquer-senha"}`},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w := postLogin(db, c.jsonBody)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
			}
			env := decodeErro(t, w.Body.Bytes())
			if env.Error.Code != "INVALID_CREDENTIALS" {
				t.Errorf("code = %q, want %q", env.Error.Code, "INVALID_CREDENTIALS")
			}
			if env.Error.Message != "E-mail ou senha inválidos." {
				t.Errorf("message = %q, want %q", env.Error.Message, "E-mail ou senha inválidos.")
			}
		})
	}
}

// TestLoginHandler_CampoObrigatorioAusente prova o envelope 400
// VALIDATION_ERROR (I/O Matrix) quando e-mail ou senha vêm em branco.
func TestLoginHandler_CampoObrigatorioAusente(t *testing.T) {
	db := testDB(t)

	w := postLogin(db, `{"email":"","senha":"senha-123456"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want %q", env.Error.Code, "VALIDATION_ERROR")
	}
}

// TestLoginHandler_PayloadInvalido prova que um corpo que nem sequer é JSON
// válido retorna 400 VALIDATION_ERROR, nunca 500 — mesmo precedente de
// TestCadastroHandler_PayloadInvalido.
func TestLoginHandler_PayloadInvalido(t *testing.T) {
	db := testDB(t)

	w := postLogin(db, `{isto nao e json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want %q", env.Error.Code, "VALIDATION_ERROR")
	}
}

// TestLoginHandler_CorpoMuitoGrande prova que authRequestMaxBytes (64KB)
// rejeita um corpo maior antes mesmo de tentar decodificar — mesmo teste que
// TestCadastroHandler_CorpoMuitoGrande já prova para /api/auth/cadastro; sem
// esta cobertura, o limite em /api/auth/login poderia ser removido ou
// desalinhado silenciosamente.
func TestLoginHandler_CorpoMuitoGrande(t *testing.T) {
	db := testDB(t)

	senhaGigante := strings.Repeat("a", 70*1024)
	w := postLogin(db, `{"email":"grande@empresa.com","senha":"`+senhaGigante+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want %q", env.Error.Code, "VALIDATION_ERROR")
	}
}

// TestRefreshHandler_Sucesso prova o cenário "Refresh válido" na fronteira
// HTTP: a partir do cookie devolvido por um login real, RefreshHandler
// devolve 200, um novo access token e um novo cookie rotacionado.
func TestRefreshHandler_Sucesso(t *testing.T) {
	db := testDB(t)
	criarUsuarioLogin(t, db, "refresh-handler-ok@empresa.com", "senha-123456")

	wLogin := postLogin(db, `{"email":"refresh-handler-ok@empresa.com","senha":"senha-123456"}`)
	if wLogin.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want %d", wLogin.Code, http.StatusOK)
	}
	cookieAntigo := refreshCookieDoResultado(t, wLogin)

	w := postRefresh(db, cookieAntigo)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("falha ao decodificar corpo: %v", err)
	}
	if body.Token == "" {
		t.Error("token vazio")
	}

	cookieNovo := refreshCookieDoResultado(t, w)
	if cookieNovo.Value == "" || cookieNovo.Value == cookieAntigo.Value {
		t.Errorf("cookie refresh_token novo (%q) deveria ser diferente do antigo (%q)", cookieNovo.Value, cookieAntigo.Value)
	}
	// Mesma asserção de Max-Age de TestLoginHandler_Sucesso: o caminho de
	// sucesso do refresh nunca era coberto por MaxAge, só os cenários de
	// cookie LIMPO (TestRefreshHandler_CookieAusente/CookieInvalidoOuExpirado)
	// — uma regressão que fizesse RefreshHandler recalcular um Max-Age
	// divergente do prazo persistido passaria despercebida sem isto.
	if cookieNovo.MaxAge <= 0 {
		t.Errorf("cookie MaxAge = %d, want > 0", cookieNovo.MaxAge)
	}
	wantMaxAge := int(services.RefreshTokenExpiracao.Seconds())
	if diff := wantMaxAge - cookieNovo.MaxAge; diff < -60 || diff > 60 {
		t.Errorf("cookie MaxAge = %d, want ~%d (dentro de 60s)", cookieNovo.MaxAge, wantMaxAge)
	}
}

// TestRefreshHandler_CookieAusente prova o cenário "Refresh ausente" da I/O
// Matrix: sem cookie, 401 TOKEN_EXPIRED e o cookie é limpo (Max-Age <= 0) na
// resposta.
func TestRefreshHandler_CookieAusente(t *testing.T) {
	db := testDB(t)

	w := postRefresh(db, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}
	cookie := refreshCookieDoResultado(t, w)
	if cookie.MaxAge > 0 {
		t.Errorf("cookie MaxAge = %d, want <= 0 (limpo)", cookie.MaxAge)
	}
}

// TestRefreshHandler_CookieInvalidoOuExpirado prova o mesmo cenário para um
// valor de cookie que nunca existiu em `sessoes`.
func TestRefreshHandler_CookieInvalidoOuExpirado(t *testing.T) {
	db := testDB(t)

	w := postRefresh(db, &http.Cookie{Name: refreshTokenCookieName, Value: "token-que-nunca-existiu"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}
	cookie := refreshCookieDoResultado(t, w)
	if cookie.MaxAge > 0 {
		t.Errorf("cookie MaxAge = %d, want <= 0 (limpo)", cookie.MaxAge)
	}
}

// TestRefreshHandler_CookieLimpoSecure prova que o cookie de refresh LIMPO
// (clearRefreshCookie, disparado quando RefreshHandler rejeita o token
// apresentado) também respeita cookieEhSeguro — mesmo comportamento já
// provado para o cookie de SUCESSO por TestLoginHandler_CookieSecure, mas
// nunca antes exercitado no caminho de limpeza: TestRefreshHandler_
// CookieAusente/CookieInvalidoOuExpirado só rodam sobre HTTP simples e nunca
// leem Secure/SameSite, então uma regressão que desacoplasse
// clearRefreshCookie de cookieEhSeguro (ex. hardcode Secure=false) passaria
// despercebida.
func TestRefreshHandler_CookieLimpoSecure(t *testing.T) {
	casos := []struct {
		nome       string
		configurar func(r *http.Request)
	}{
		{
			nome:       "TLS direto",
			configurar: func(r *http.Request) { r.TLS = &tls.ConnectionState{} },
		},
		{
			nome:       "proxy com X-Forwarded-Proto https",
			configurar: func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") },
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			db := testDB(t)
			req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
			c.configurar(req)
			w := httptest.NewRecorder()
			RefreshHandler(db, testJWTSecret)(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
			}
			cookie := refreshCookieDoResultado(t, w)
			if !cookie.Secure {
				t.Error("cookie refresh_token limpo Secure = false, want true")
			}
			if cookie.SameSite != http.SameSiteLaxMode {
				t.Errorf("cookie SameSite = %v, want SameSiteLaxMode", cookie.SameSite)
			}
			if cookie.MaxAge > 0 {
				t.Errorf("cookie MaxAge = %d, want <= 0 (limpo)", cookie.MaxAge)
			}
		})
	}
}

// TestRefreshHandler_TokenExpirado prova, na fronteira HTTP, o sub-caso
// "token expirado" da I/O Matrix de refresh — já provado a nível de serviço
// por services.TestRenovarSessao_TokenExpirado, mas nunca antes através de
// RefreshHandler/postRefresh.
func TestRefreshHandler_TokenExpirado(t *testing.T) {
	db := testDB(t)
	criarUsuarioLogin(t, db, "refresh-handler-expirado@empresa.com", "senha-123456")

	wLogin := postLogin(db, `{"email":"refresh-handler-expirado@empresa.com","senha":"senha-123456"}`)
	if wLogin.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want %d", wLogin.Code, http.StatusOK)
	}
	cookie := refreshCookieDoResultado(t, wLogin)

	if _, err := db.Exec(`UPDATE sessoes SET expira_em = now() - interval '1 hour' WHERE refresh_token = $1`, cookie.Value); err != nil {
		t.Fatalf("falha ao forçar expiração: %v", err)
	}

	w := postRefresh(db, cookie)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}
}

// TestRefreshHandler_TokenJaRevogado prova, na fronteira HTTP, o sub-caso
// "token já revogado" da I/O Matrix de refresh — já provado a nível de
// serviço por services.TestRenovarSessao_TokenJaRevogado, mas nunca antes
// através de RefreshHandler/postRefresh: reapresentar o refresh token antigo
// depois de uma rotação bem-sucedida deve falhar.
func TestRefreshHandler_TokenJaRevogado(t *testing.T) {
	db := testDB(t)
	criarUsuarioLogin(t, db, "refresh-handler-revogado@empresa.com", "senha-123456")

	wLogin := postLogin(db, `{"email":"refresh-handler-revogado@empresa.com","senha":"senha-123456"}`)
	if wLogin.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want %d", wLogin.Code, http.StatusOK)
	}
	cookieAntigo := refreshCookieDoResultado(t, wLogin)

	if w := postRefresh(db, cookieAntigo); w.Code != http.StatusOK {
		t.Fatalf("primeira renovação: status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}

	w := postRefresh(db, cookieAntigo)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("segunda renovação: status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}
}

// TestMeHandler_Sucesso prova GET /api/auth/me através da MESMA composição
// usada em produção: um access token válido de login real devolve
// id/nome/email/papel do usuário.
func TestMeHandler_Sucesso(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioLogin(t, db, "me-handler-ok@empresa.com", "senha-123456")

	wLogin := postLogin(db, `{"email":"me-handler-ok@empresa.com","senha":"senha-123456"}`)
	if wLogin.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want %d", wLogin.Code, http.StatusOK)
	}
	var loginBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(wLogin.Body.Bytes(), &loginBody); err != nil {
		t.Fatalf("falha ao decodificar corpo do login: %v", err)
	}

	w := getMe(db, "Bearer "+loginBody.Token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	var body struct {
		ID    string `json:"id"`
		Nome  string `json:"nome"`
		Email string `json:"email"`
		Papel string `json:"papel"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("falha ao decodificar corpo: %v", err)
	}
	if body.ID != usuarioID {
		t.Errorf("id = %q, want %q", body.ID, usuarioID)
	}
	if body.Email != "me-handler-ok@empresa.com" {
		t.Errorf("email = %q, want %q", body.Email, "me-handler-ok@empresa.com")
	}
	if body.Papel != "usuario" {
		t.Errorf("papel = %q, want %q", body.Papel, "usuario")
	}
}

// TestMeHandler_SemToken prova o cenário "GET /api/auth/me sem token" da I/O
// Matrix: 401 TOKEN_EXPIRED.
func TestMeHandler_SemToken(t *testing.T) {
	db := testDB(t)

	w := getMe(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}
}

// --- Story 1.6: recuperação de senha por e-mail -------------------------

func postEsqueciSenha(db *sql.DB, jsonBody string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/esqueci-senha", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	EsqueciSenhaHandler(db, testEmailCfg)(w, req)
	return w
}

func getValidarRedefinicao(db *sql.DB, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/auth/redefinir-senha?token="+token, nil)
	w := httptest.NewRecorder()
	ValidarRedefinicaoSenhaHandler(db)(w, req)
	return w
}

func postRedefinirSenha(db *sql.DB, jsonBody string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/redefinir-senha", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	RedefinirSenhaHandler(db)(w, req)
	return w
}

// seedTokenRedefinicao cria uma conta ativa/verificada com senha conhecida e
// devolve o id + um token de redefinição válido (via o próprio service).
func seedTokenRedefinicao(t *testing.T, db *sql.DB, email string) (usuarioID, token string) {
	t.Helper()
	usuarioID = criarUsuarioLogin(t, db, email, "senha-antiga1")
	if err := services.SolicitarRedefinicaoSenha(db, testEmailCfg, email); err != nil {
		t.Fatalf("SolicitarRedefinicaoSenha falhou: %v", err)
	}
	if err := db.QueryRow(
		`SELECT token FROM tokens_acao WHERE usuario_id = $1 AND tipo = 'redefinicao_senha'`, usuarioID,
	).Scan(&token); err != nil {
		t.Fatalf("falha ao ler token de redefinição: %v", err)
	}
	return usuarioID, token
}

// TestEsqueciSenhaHandler_RespostaGenericaByteIdentica prova a garantia
// central da I/O Matrix: conta existente e inexistente produzem o MESMO
// status e o MESMO corpo byte-a-byte — e só a conta existente grava
// token+outbox.
func TestEsqueciSenhaHandler_RespostaGenericaByteIdentica(t *testing.T) {
	db := testDB(t)
	criarUsuarioLogin(t, db, "esqueci-existe@empresa.com", "senha-123456")

	wExiste := postEsqueciSenha(db, `{"email":"Esqueci-Existe@Empresa.com"}`)
	wNaoExiste := postEsqueciSenha(db, `{"email":"esqueci-fantasma@empresa.com"}`)

	if wExiste.Code != http.StatusOK || wNaoExiste.Code != http.StatusOK {
		t.Fatalf("status = %d / %d, want 200 / 200", wExiste.Code, wNaoExiste.Code)
	}
	if wExiste.Body.String() != wNaoExiste.Body.String() {
		t.Fatalf("corpos diferentes:\n existe    = %q\n nao existe = %q", wExiste.Body.String(), wNaoExiste.Body.String())
	}
	if wExiste.Body.String() != `{"mensagem":"Se o e-mail existir, você receberá um link."}`+"\n" {
		t.Errorf("corpo = %q, want a mensagem genérica fixa", wExiste.Body.String())
	}

	var tokens, emails int
	if err := db.QueryRow(`SELECT count(*) FROM tokens_acao WHERE tipo = 'redefinicao_senha'`).Scan(&tokens); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM emails_pendentes WHERE tipo = 'redefinicao_senha'`).Scan(&emails); err != nil {
		t.Fatalf("count emails: %v", err)
	}
	if tokens != 1 || emails != 1 {
		t.Errorf("tokens=%d emails=%d, want 1 e 1 (só a conta existente grava)", tokens, emails)
	}
}

// TestEsqueciSenhaHandler_PayloadInvalido prova que só JSON malformado ->
// 400 VALIDATION_ERROR.
func TestEsqueciSenhaHandler_PayloadInvalido(t *testing.T) {
	db := testDB(t)

	w := postEsqueciSenha(db, `{isto nao e json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want %q", env.Error.Code, "VALIDATION_ERROR")
	}
}

// TestValidarRedefinicaoSenhaHandler cobre os três desfechos do GET de
// validação de link (I/O Matrix): válido -> 200 {"valido":true} sem consumir;
// inexistente -> 404 NOT_FOUND; expirado/usado -> 400 TOKEN_EXPIRED.
func TestValidarRedefinicaoSenhaHandler(t *testing.T) {
	db := testDB(t)
	_, token := seedTokenRedefinicao(t, db, "valida-handler@empresa.com")

	w := getValidarRedefinicao(db, token)
	if w.Code != http.StatusOK {
		t.Fatalf("token válido: status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body struct {
		Valido bool `json:"valido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || !body.Valido {
		t.Fatalf("corpo = %s, want {\"valido\":true}", w.Body.String())
	}
	var usado *string
	if err := db.QueryRow(`SELECT usado_em::text FROM tokens_acao WHERE token = $1`, token).Scan(&usado); err != nil {
		t.Fatalf("falha ao reler token: %v", err)
	}
	if usado != nil {
		t.Error("GET de validação consumiu o token (usado_em preenchido)")
	}

	wInexistente := getValidarRedefinicao(db, "token-que-nunca-existiu")
	if wInexistente.Code != http.StatusNotFound {
		t.Fatalf("inexistente: status = %d, want 404 (body=%s)", wInexistente.Code, wInexistente.Body.String())
	}
	if env := decodeErro(t, wInexistente.Body.Bytes()); env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want %q", env.Error.Code, "NOT_FOUND")
	}

	// Token usado mas AINDA não expirado (sub-ramo `usadoEm.Valid` isolado da
	// expiração) -> 400 TOKEN_EXPIRED.
	_, tokenUsado := seedTokenRedefinicao(t, db, "valida-handler-usado@empresa.com")
	if _, err := db.Exec(`UPDATE tokens_acao SET usado_em = now() WHERE token = $1`, tokenUsado); err != nil {
		t.Fatalf("falha ao marcar token como usado: %v", err)
	}
	wUsado := getValidarRedefinicao(db, tokenUsado)
	if wUsado.Code != http.StatusBadRequest {
		t.Fatalf("usado: status = %d, want 400 (body=%s)", wUsado.Code, wUsado.Body.String())
	}
	if env := decodeErro(t, wUsado.Body.Bytes()); env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("usado: code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}

	if _, err := db.Exec(`UPDATE tokens_acao SET expira_em = now() - interval '1 minute' WHERE token = $1`, token); err != nil {
		t.Fatalf("falha ao forçar expiração: %v", err)
	}
	wExpirado := getValidarRedefinicao(db, token)
	if wExpirado.Code != http.StatusBadRequest {
		t.Fatalf("expirado: status = %d, want 400 (body=%s)", wExpirado.Code, wExpirado.Body.String())
	}
	if env := decodeErro(t, wExpirado.Body.Bytes()); env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}
}

// TestRedefinirSenhaHandler_Sucesso prova o caminho feliz na fronteira HTTP:
// 200; a nova senha passa a autenticar em POST /api/auth/login; um refresh
// com um cookie emitido antes do reset passa a devolver 401 TOKEN_EXPIRED.
func TestRedefinirSenhaHandler_Sucesso(t *testing.T) {
	db := testDB(t)
	_, token := seedTokenRedefinicao(t, db, "redefine-handler-ok@empresa.com")

	// Sessão emitida ANTES do reset (via login real).
	wLogin := postLogin(db, `{"email":"redefine-handler-ok@empresa.com","senha":"senha-antiga1"}`)
	if wLogin.Code != http.StatusOK {
		t.Fatalf("login pré-reset: status = %d, want 200", wLogin.Code)
	}
	cookieAntigo := refreshCookieDoResultado(t, wLogin)

	w := postRedefinirSenha(db, `{"token":"`+token+`","senha":"nova-senha1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	// Encadeamento reset -> login com a nova senha.
	wNovoLogin := postLogin(db, `{"email":"redefine-handler-ok@empresa.com","senha":"nova-senha1"}`)
	if wNovoLogin.Code != http.StatusOK {
		t.Fatalf("login com a nova senha: status = %d, want 200 (body=%s)", wNovoLogin.Code, wNovoLogin.Body.String())
	}

	// Encadeamento reset -> refresh com o cookie antigo -> 401 TOKEN_EXPIRED.
	wRefresh := postRefresh(db, cookieAntigo)
	if wRefresh.Code != http.StatusUnauthorized {
		t.Fatalf("refresh com cookie pré-reset: status = %d, want 401 (body=%s)", wRefresh.Code, wRefresh.Body.String())
	}
	if env := decodeErro(t, wRefresh.Body.Bytes()); env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}
}

// TestRedefinirSenhaHandler_SenhaFraca prova o cenário "Redefinição, senha
// fraca": 400 VALIDATION_ERROR e o token permanece válido para nova tentativa.
func TestRedefinirSenhaHandler_SenhaFraca(t *testing.T) {
	db := testDB(t)
	_, token := seedTokenRedefinicao(t, db, "redefine-handler-fraca@empresa.com")

	w := postRedefinirSenha(db, `{"token":"`+token+`","senha":"curta1"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want %q", env.Error.Code, "VALIDATION_ERROR")
	}

	wRetry := postRedefinirSenha(db, `{"token":"`+token+`","senha":"nova-senha1"}`)
	if wRetry.Code != http.StatusOK {
		t.Fatalf("retry com senha forte: status = %d, want 200 (body=%s)", wRetry.Code, wRetry.Body.String())
	}
}

// TestRedefinirSenhaHandler_TokenInexistente prova 404 NOT_FOUND.
func TestRedefinirSenhaHandler_TokenInexistente(t *testing.T) {
	db := testDB(t)

	w := postRedefinirSenha(db, `{"token":"token-que-nunca-existiu","senha":"nova-senha1"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want %q", env.Error.Code, "NOT_FOUND")
	}
}

// TestRedefinirSenhaHandler_TokenExpiradoEReuso prova 400 TOKEN_EXPIRED para
// token vencido e para o reuso de um token já consumido.
func TestRedefinirSenhaHandler_TokenExpiradoEReuso(t *testing.T) {
	db := testDB(t)
	_, tokenVencido := seedTokenRedefinicao(t, db, "redefine-handler-venc@empresa.com")
	if _, err := db.Exec(`UPDATE tokens_acao SET expira_em = now() - interval '1 minute' WHERE token = $1`, tokenVencido); err != nil {
		t.Fatalf("falha ao forçar expiração: %v", err)
	}
	wVencido := postRedefinirSenha(db, `{"token":"`+tokenVencido+`","senha":"nova-senha1"}`)
	if wVencido.Code != http.StatusBadRequest {
		t.Fatalf("vencido: status = %d, want 400 (body=%s)", wVencido.Code, wVencido.Body.String())
	}
	if env := decodeErro(t, wVencido.Body.Bytes()); env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("vencido: code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}

	_, tokenReuso := seedTokenRedefinicao(t, db, "redefine-handler-reuso@empresa.com")
	if w := postRedefinirSenha(db, `{"token":"`+tokenReuso+`","senha":"nova-senha1"}`); w.Code != http.StatusOK {
		t.Fatalf("primeiro uso: status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	wReuso := postRedefinirSenha(db, `{"token":"`+tokenReuso+`","senha":"outra-senha2"}`)
	if wReuso.Code != http.StatusBadRequest {
		t.Fatalf("reuso: status = %d, want 400 (body=%s)", wReuso.Code, wReuso.Body.String())
	}
	if env := decodeErro(t, wReuso.Body.Bytes()); env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("reuso: code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}
}

// TestRedefinirSenhaHandler_TokenDeVerificacaoEmail prova o isolamento entre
// fluxos (AD-18) na fronteira HTTP: um token válido de
// tipo='verificacao_email' -> 404 NOT_FOUND.
func TestRedefinirSenhaHandler_TokenDeVerificacaoEmail(t *testing.T) {
	db := testDB(t)
	usuarioID, err := services.Cadastrar(db, testEmailCfg, "isola-handler", "isola-handler@empresa.com", "senha-123456")
	if err != nil {
		t.Fatalf("Cadastrar falhou: %v", err)
	}
	var tokenVerificacao string
	if err := db.QueryRow(`SELECT token FROM tokens_acao WHERE usuario_id = $1`, usuarioID).Scan(&tokenVerificacao); err != nil {
		t.Fatalf("falha ao ler token: %v", err)
	}

	w := postRedefinirSenha(db, `{"token":"`+tokenVerificacao+`","senha":"nova-senha1"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want %q", env.Error.Code, "NOT_FOUND")
	}
}

// TestRedefinirSenhaHandler_PayloadInvalido prova 400 VALIDATION_ERROR para
// corpo não-JSON.
func TestRedefinirSenhaHandler_PayloadInvalido(t *testing.T) {
	db := testDB(t)

	w := postRedefinirSenha(db, `{isto nao e json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want %q", env.Error.Code, "VALIDATION_ERROR")
	}
}

// TestMeHandler_ContaDesativadaAposEmissao prova a AC principal da Story
// 1.4: um token de acesso ainda válido, para uma conta desativada entre a
// emissão e o uso, é rejeitado com 401 SESSION_REVOKED — provado através da
// composição real (main.go), não chamando o middleware isoladamente.
func TestMeHandler_ContaDesativadaAposEmissao(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioLogin(t, db, "me-handler-desativado@empresa.com", "senha-123456")

	wLogin := postLogin(db, `{"email":"me-handler-desativado@empresa.com","senha":"senha-123456"}`)
	if wLogin.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want %d", wLogin.Code, http.StatusOK)
	}
	var loginBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(wLogin.Body.Bytes(), &loginBody); err != nil {
		t.Fatalf("falha ao decodificar corpo do login: %v", err)
	}

	if _, err := db.Exec(`UPDATE usuarios SET ativo = false WHERE id = $1`, usuarioID); err != nil {
		t.Fatalf("falha ao desativar conta: %v", err)
	}

	w := getMe(db, "Bearer "+loginBody.Token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "SESSION_REVOKED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "SESSION_REVOKED")
	}
}

// --- Story 1.10: bloqueio de conta e política de senha na fronteira HTTP ---

// TestLoginHandler_SextaTentativaResponde429 prova a AC principal da Story
// 1.10 na fronteira HTTP: 5 logins errados seguidos contra a mesma conta e o
// 6º responde 429 com code ACCOUNT_LOCKED e uma mensagem SEM o tempo restante
// (nenhum dígito, nenhuma menção a minutos/segundos).
func TestLoginHandler_SextaTentativaResponde429(t *testing.T) {
	db := testDB(t)
	criarUsuarioLogin(t, db, "http-brute@empresa.com", "senha-123456")

	for i := 1; i <= 5; i++ {
		w := postLogin(db, `{"email":"http-brute@empresa.com","senha":"errada"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("tentativa %d: status = %d, want 401 (body=%s)", i, w.Code, w.Body.String())
		}
		if got := decodeErro(t, w.Body.Bytes()).Error.Code; got != "INVALID_CREDENTIALS" {
			t.Fatalf("tentativa %d: code = %q, want INVALID_CREDENTIALS", i, got)
		}
	}

	// 6ª tentativa, agora com a senha CORRETA — ainda assim bloqueada.
	w := postLogin(db, `{"email":"http-brute@empresa.com","senha":"senha-123456"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("6ª tentativa: status = %d, want 429 (body=%s)", w.Code, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "ACCOUNT_LOCKED" {
		t.Fatalf("code = %q, want ACCOUNT_LOCKED", env.Error.Code)
	}
	if strings.ContainsAny(env.Error.Message, "0123456789") {
		t.Errorf("mensagem contém dígitos (tempo restante vazado): %q", env.Error.Message)
	}
	for _, termo := range []string{"minuto", "segundo", "hora"} {
		if strings.Contains(strings.ToLower(env.Error.Message), termo) {
			t.Errorf("mensagem menciona %q (tempo restante vazado): %q", termo, env.Error.Message)
		}
	}
}

// TestLoginHandler_ContaBloqueadaAindaPodeRedefinirSenha prova a AC2: uma
// conta com bloqueado_ate no futuro continua podendo acionar
// POST /api/auth/esqueci-senha — resposta 200 e token de redefinição + linha
// de outbox gravados normalmente.
func TestLoginHandler_ContaBloqueadaAindaPodeRedefinirSenha(t *testing.T) {
	db := testDB(t)
	id := criarUsuarioLogin(t, db, "http-bloqueada-reset@empresa.com", "senha-123456")
	if _, err := db.Exec(
		`UPDATE usuarios SET tentativas_login_falhas = 5, bloqueado_ate = now() + interval '15 minutes' WHERE id = $1`, id,
	); err != nil {
		t.Fatalf("falha ao bloquear conta: %v", err)
	}

	// Sanidade: o login por senha está de fato bloqueado.
	wLogin := postLogin(db, `{"email":"http-bloqueada-reset@empresa.com","senha":"senha-123456"}`)
	if wLogin.Code != http.StatusTooManyRequests {
		t.Fatalf("pré-condição: login status = %d, want 429", wLogin.Code)
	}

	w := postEsqueciSenha(db, `{"email":"http-bloqueada-reset@empresa.com"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("esqueci-senha: status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	var tokens, emails int
	if err := db.QueryRow(
		`SELECT count(*) FROM tokens_acao WHERE usuario_id = $1 AND tipo = 'redefinicao_senha'`, id,
	).Scan(&tokens); err != nil {
		t.Fatalf("contar tokens: %v", err)
	}
	if err := db.QueryRow(
		`SELECT count(*) FROM emails_pendentes WHERE usuario_id = $1 AND tipo = 'redefinicao_senha'`, id,
	).Scan(&emails); err != nil {
		t.Fatalf("contar emails: %v", err)
	}
	if tokens != 1 || emails != 1 {
		t.Errorf("tokens=%d emails=%d, want 1 / 1 — o bloqueio de login não pode afetar a redefinição", tokens, emails)
	}
}

// TestCadastroHandler_SenhaFraca prova a linha "UI cadastro senha fraca" no
// nível HTTP: POST /api/auth/cadastro com "abc" -> 400 VALIDATION_ERROR com o
// critério da política, e count(usuarios) inalterado.
func TestCadastroHandler_SenhaFraca(t *testing.T) {
	db := testDB(t)
	antes := contarLinhasUsuarios(t, db)

	w := postCadastro(db, `{"nome":"Fulano","email":"fraca-http@empresa.com","senha":"abc"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Fatalf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
	if env.Error.Message != "A senha deve ter ao menos 8 caracteres, incluindo uma letra e um número." {
		t.Errorf("mensagem = %q, want a string idêntica à de RedefinirSenhaHandler", env.Error.Message)
	}
	if depois := contarLinhasUsuarios(t, db); depois != antes {
		t.Errorf("count(usuarios) = %d, want %d — cadastro com senha fraca não pode criar conta", depois, antes)
	}
}
