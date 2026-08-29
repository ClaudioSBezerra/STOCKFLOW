package handlers

import (
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

	w2 := postCadastro(db, `{"nome":"Segundo","email":"duplicado@empresa.com","senha":"outra-senha"}`)
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
