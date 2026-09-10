package middleware

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"stockflow/backend/services"
)

// Testes da fronteira entre a sessão do Dono da Plataforma e a de `usuarios`
// — Story 9.2, spec-9-2 (AC 6).

// criarDonoMiddleware grava o único Dono do teste direto no banco, com
// controle sobre `ativo`, e o remove ao fim.
func criarDonoMiddleware(t *testing.T, db *sql.DB, ativo bool) string {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM donos_plataforma`); err != nil {
		t.Fatalf("limpar donos_plataforma: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM donos_plataforma`) })
	var id string
	if err := db.QueryRow(`INSERT INTO donos_plataforma (nome, email, senha_hash, mfa_secret, ativo)
		VALUES ('Dona Middleware', 'dona-mw@plataforma.com', 'hash', 'SEGREDO', $1) RETURNING id`, ativo).Scan(&id); err != nil {
		t.Fatalf("criar dono: %v", err)
	}
	return id
}

func tokenPlataformaTeste(t *testing.T, secret []byte, subject string, aud []string, exp time.Time) string {
	t.Helper()
	claims := services.PlataformaClaims{RegisteredClaims: jwt.RegisteredClaims{
		Subject:   subject,
		Audience:  aud,
		ExpiresAt: jwt.NewNumericDate(exp),
		IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
	}}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	if err != nil {
		t.Fatalf("assinar token de teste: %v", err)
	}
	return signed
}

func chamarPlataforma(db *sql.DB, authHeader string) (*httptest.ResponseRecorder, services.DonoSessao, bool) {
	var dono services.DonoSessao
	var ok bool
	next := func(w http.ResponseWriter, r *http.Request) {
		dono, ok = DonoDaSessao(r.Context())
		w.WriteHeader(http.StatusOK)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/plataforma/empresas", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	RequireDonoPlataforma(db, testJWTSecret)(next)(w, req)
	return w, dono, ok
}

func TestRequireDonoPlataforma_TokenDoDonoPassa(t *testing.T) {
	db := testDB(t)
	id := criarDonoMiddleware(t, db, true)
	token := tokenPlataformaTeste(t, testJWTSecret, id, []string{services.AudienciaPlataforma}, time.Now().UTC().Add(30*time.Minute))

	w, dono, ok := chamarPlataforma(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if !ok || dono.ID != id || dono.Email != "dona-mw@plataforma.com" {
		t.Errorf("DonoDaSessao = %+v, %v", dono, ok)
	}
}

// TestRequireDonoPlataforma_RecusaTokenSemAudPlataforma prova que nenhum
// token fora do formato do Dono passa — inclusive um token de `usuarios`
// (sem `aud`) cujo `sub` é o id do próprio Dono.
func TestRequireDonoPlataforma_RecusaTokenSemAudPlataforma(t *testing.T) {
	db := testDB(t)
	id := criarDonoMiddleware(t, db, true)
	valido := time.Now().UTC().Add(30 * time.Minute)

	casos := []struct{ nome, header string }{
		{"sem header", ""},
		{"sem prefixo Bearer", "token"},
		{"token de usuarios (sem aud)", "Bearer " + gerarAccessTokenTeste(t, testJWTSecret, id, valido)},
		{"aud de outra audiência", "Bearer " + tokenPlataformaTeste(t, testJWTSecret, id, []string{"outra"}, valido)},
		{"expirado", "Bearer " + tokenPlataformaTeste(t, testJWTSecret, id, []string{services.AudienciaPlataforma}, time.Now().UTC().Add(-time.Minute))},
		{"assinatura inválida", "Bearer " + tokenPlataformaTeste(t, []byte("outro-segredo"), id, []string{services.AudienciaPlataforma}, valido)},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w, _, ok := chamarPlataforma(db, c.header)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "TOKEN_EXPIRED" {
				t.Errorf("code = %q, want TOKEN_EXPIRED", env.Error.Code)
			}
			if ok {
				t.Error("o handler seguinte rodou com um token recusado")
			}
		})
	}
}

func TestRequireDonoPlataforma_DonoInativoOuInexistente(t *testing.T) {
	db := testDB(t)
	id := criarDonoMiddleware(t, db, false)
	valido := time.Now().UTC().Add(30 * time.Minute)

	for nome, sub := range map[string]string{
		"dono inativo":     id,
		"dono inexistente": "00000000-0000-4000-8000-000000000000",
	} {
		t.Run(nome, func(t *testing.T) {
			token := tokenPlataformaTeste(t, testJWTSecret, sub, []string{services.AudienciaPlataforma}, valido)
			w, _, _ := chamarPlataforma(db, "Bearer "+token)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "SESSION_REVOKED" {
				t.Errorf("code = %q, want SESSION_REVOKED", env.Error.Code)
			}
		})
	}
}

// TestRequireAuth_RecusaTokenDaPlataforma prova que a recusa do `aud`
// plataforma em RequireAuth é explícita: o `sub` do token aponta para uma
// conta de `usuarios` ativa e mesmo assim a sessão não passa.
func TestRequireAuth_RecusaTokenDaPlataforma(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuario(t, db, "alvo-aud-plataforma@empresa.com", true)
	token := tokenPlataformaTeste(t, testJWTSecret, usuarioID, []string{services.AudienciaPlataforma}, time.Now().UTC().Add(30*time.Minute))

	w, usuarioViaContexto := chamarComToken(db, "Bearer "+token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "SESSION_REVOKED" {
		t.Errorf("code = %q, want SESSION_REVOKED", env.Error.Code)
	}
	if usuarioViaContexto {
		t.Error("RequireAuth deixou passar um token com aud=plataforma")
	}
}
