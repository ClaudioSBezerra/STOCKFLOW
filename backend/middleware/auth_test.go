package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
)

var (
	migrateOnce sync.Once
	migrateErr  error
)

// testDB abre uma conexão contra DATABASE_URL, aplica as migrations reais do
// projeto e limpa usuarios/tokens_acao/emails_pendentes/sessoes antes de cada
// teste — mesmo padrão de backend/services/auth_test.go e
// backend/handlers/auth_test.go. Pula o teste quando nenhum Postgres foi
// configurado.
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

var testJWTSecret = []byte("segredo-de-teste-nao-usar-em-producao")

// criarUsuario insere uma conta diretamente em usuarios para exercitar
// RequireAuth com controle total sobre `ativo` — sem depender de
// services.Cadastrar (que sempre cria email_verificado=false e não tem
// parâmetro de ativo).
func criarUsuario(t *testing.T, db *sql.DB, email string, ativo bool) string {
	t.Helper()
	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
		VALUES ('Usuário Teste', $1, 'hash-qualquer', 'usuario', true, $2)
		RETURNING id`
	if err := db.QueryRow(insert, email, ativo).Scan(&id); err != nil {
		t.Fatalf("falha ao criar usuario de teste: %v", err)
	}
	return id
}

// gerarAccessTokenTeste assina um JWT com o mesmo formato de
// services.gerarAccessToken (claim mínimo `sub`), mas com controle direto
// sobre segredo/expiração/subject para exercitar todos os ramos de
// RequireAuth.
func gerarAccessTokenTeste(t *testing.T, secret []byte, subject string, exp time.Time) string {
	t.Helper()
	claims := jwt.RegisteredClaims{
		Subject:   subject,
		ExpiresAt: jwt.NewNumericDate(exp),
		IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("falha ao assinar token de teste: %v", err)
	}
	return signed
}

func chamarComToken(db *sql.DB, authHeader string) (*httptest.ResponseRecorder, bool) {
	var usuarioViaContexto bool
	next := func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UsuarioDaSessao(r.Context()); ok {
			usuarioViaContexto = true
		}
		w.WriteHeader(http.StatusOK)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	RequireAuth(db, testJWTSecret)(next)(w, req)
	return w, usuarioViaContexto
}

func decodeErro(t *testing.T, body []byte) erroEnvelope {
	t.Helper()
	var env erroEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("falha ao decodificar envelope de erro: %v (body=%s)", err, body)
	}
	return env
}

// TestRequireAuth_Sucesso prova o caminho feliz: token válido de um usuário
// ativo passa para o handler seguinte, com UsuarioSessao disponível via
// UsuarioDaSessao no contexto.
func TestRequireAuth_Sucesso(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuario(t, db, "requireauth-ok@empresa.com", true)
	token := gerarAccessTokenTeste(t, testJWTSecret, usuarioID, time.Now().UTC().Add(30*time.Minute))

	w, usuarioViaContexto := chamarComToken(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	if !usuarioViaContexto {
		t.Error("UsuarioDaSessao não encontrou UsuarioSessao no contexto do handler seguinte")
	}
}

// TestRequireAuth_TokenAusenteOuMalformado prova o cenário "sem token" da
// I/O Matrix: header ausente, sem prefixo Bearer, ou com valor vazio depois
// do prefixo devolvem 401 TOKEN_EXPIRED — nenhum chega a consultar o banco.
func TestRequireAuth_TokenAusenteOuMalformado(t *testing.T) {
	db := testDB(t)

	casos := []struct {
		nome       string
		authHeader string
	}{
		{"header ausente", ""},
		{"sem prefixo Bearer", "token-sem-prefixo"},
		{"Bearer vazio", "Bearer "},
		{"Bearer com espaços", "Bearer    "},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w, _ := chamarComToken(db, c.authHeader)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
			}
			env := decodeErro(t, w.Body.Bytes())
			if env.Error.Code != "TOKEN_EXPIRED" {
				t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
			}
		})
	}
}

// TestRequireAuth_AssinaturaInvalida prova que um token assinado com um
// segredo diferente do configurado é rejeitado com 401 TOKEN_EXPIRED — nunca
// aceito mesmo com claims bem formados.
func TestRequireAuth_AssinaturaInvalida(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuario(t, db, "assinatura-invalida@empresa.com", true)
	token := gerarAccessTokenTeste(t, []byte("outro-segredo-completamente-diferente"), usuarioID, time.Now().UTC().Add(30*time.Minute))

	w, _ := chamarComToken(db, "Bearer "+token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}
}

// TestRequireAuth_TokenExpirado prova que um access token com exp no passado
// é rejeitado com 401 TOKEN_EXPIRED, mesmo com assinatura e claims válidos.
func TestRequireAuth_TokenExpirado(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuario(t, db, "token-expirado@empresa.com", true)
	token := gerarAccessTokenTeste(t, testJWTSecret, usuarioID, time.Now().UTC().Add(-time.Minute))

	w, _ := chamarComToken(db, "Bearer "+token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}
}

// TestRequireAuth_UsuarioNaoEncontrado prova que um token válido cujo `sub`
// não corresponde a nenhum usuário (ex.: conta removida após a emissão) é
// tratado como sessão revogada, não como erro interno.
func TestRequireAuth_UsuarioNaoEncontrado(t *testing.T) {
	db := testDB(t)
	token := gerarAccessTokenTeste(t, testJWTSecret, "00000000-0000-0000-0000-000000000000", time.Now().UTC().Add(30*time.Minute))

	w, _ := chamarComToken(db, "Bearer "+token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "SESSION_REVOKED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "SESSION_REVOKED")
	}
}

// TestRequireAuth_AlgoritmoInesperadoRejeitado prova a defesa contra
// "algorithm confusion" já presente em RequireAuth (o callback de
// jwt.ParseWithClaims só aceita o token se t.Method for
// *jwt.SigningMethodHMAC antes de devolver jwtSecret) — não é uma
// vulnerabilidade encontrada, é um invariante de segurança sem cobertura até
// este teste. Cobre dois ataques distintos: (1) um token assinado com RS256
// através de uma chave RSA descartável, cuja chave pública nunca é conhecida
// do middleware; e (2) um token "alg: none" construído à mão, sem assinatura
// nenhuma. Os dois precisam ser rejeitados com 401 TOKEN_EXPIRED, nunca
// aceitos.
func TestRequireAuth_AlgoritmoInesperadoRejeitado(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuario(t, db, "algoritmo-inesperado@empresa.com", true)

	t.Run("assinado com RS256", func(t *testing.T) {
		chaveRSA, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("falha ao gerar chave RSA de teste: %v", err)
		}
		claims := jwt.RegisteredClaims{
			Subject:   usuarioID,
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(30 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		signed, err := token.SignedString(chaveRSA)
		if err != nil {
			t.Fatalf("falha ao assinar token RS256 de teste: %v", err)
		}

		w, _ := chamarComToken(db, "Bearer "+signed)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "TOKEN_EXPIRED" {
			t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
		}
	})

	t.Run("alg none sem assinatura", func(t *testing.T) {
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
		claimsJSON := fmt.Sprintf(
			`{"sub":%q,"exp":%d,"iat":%d}`,
			usuarioID,
			time.Now().UTC().Add(30*time.Minute).Unix(),
			time.Now().UTC().Unix(),
		)
		payload := base64.RawURLEncoding.EncodeToString([]byte(claimsJSON))
		tokenAlgNone := header + "." + payload + "." // assinatura vazia, formato "alg: none"

		w, _ := chamarComToken(db, "Bearer "+tokenAlgNone)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "TOKEN_EXPIRED" {
			t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
		}
	})
}

// TestRequireAuth_ContaDesativadaAposEmissao prova a AC principal desta
// story: um token de acesso ainda válido, emitido antes da conta ser
// desativada, é rejeitado com 401 SESSION_REVOKED na próxima requisição — o
// middleware nunca confia em um estado carimbado no token.
func TestRequireAuth_ContaDesativadaAposEmissao(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuario(t, db, "desativado-apos-emissao@empresa.com", true)
	token := gerarAccessTokenTeste(t, testJWTSecret, usuarioID, time.Now().UTC().Add(30*time.Minute))

	if _, err := db.Exec(`UPDATE usuarios SET ativo = false WHERE id = $1`, usuarioID); err != nil {
		t.Fatalf("falha ao desativar conta: %v", err)
	}

	w, _ := chamarComToken(db, "Bearer "+token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "SESSION_REVOKED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "SESSION_REVOKED")
	}
}
