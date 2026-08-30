package iam

import (
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	issTeste = "https://kc.example/realms/ferreiracosta"
	azpTeste = "stockflow-web"
)

// assinarRS256 assina um token RS256 com a chave/kid informados.
func assinarRS256(t *testing.T, priv *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("assinar token RS256: %v", err)
	}
	return s
}

// middlewareDeTeste monta um Middleware real apontado para um JWKS falso
// servindo `kid-1` -> priv.PublicKey, e devolve um handler que responde 200 +
// o e-mail/verificado extraídos do contexto.
func middlewareDeTeste(t *testing.T, priv *rsa.PrivateKey, cfg Config) http.HandlerFunc {
	t.Helper()
	body := jwksDeChave(map[string]*rsa.PublicKey{"kid-1": &priv.PublicKey})
	srv, _ := servidorJWKS(t, http.StatusOK, body)
	jwks := NewJWKSClient(srv.URL, time.Hour)

	final := func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"email":          EmailDaSessaoSSO(r.Context()),
			"email_verified": EmailVerificadoSSO(r.Context()),
		})
	}
	return Middleware(jwks, cfg)(final)
}

func claimsValidas() jwt.MapClaims {
	return jwt.MapClaims{
		"iss":            issTeste,
		"azp":            azpTeste,
		"email":          "carlos@fc.com",
		"email_verified": true,
		"exp":            time.Now().Add(5 * time.Minute).Unix(),
		"iat":            time.Now().Add(-time.Minute).Unix(),
	}
}

func TestMiddleware_TokenValidoInjetaEmail(t *testing.T) {
	priv := gerarChaveRSA(t)
	h := middlewareDeTeste(t, priv, Config{RealmURL: issTeste, AllowedClientIDs: []string{azpTeste}})

	tok := assinarRS256(t, priv, "kid-1", claimsValidas())
	req := httptest.NewRequest(http.MethodPost, "/api/auth/sso/keycloak", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var got struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Email != "carlos@fc.com" || !got.EmailVerified {
		t.Fatalf("contexto = %+v, want email=carlos@fc.com email_verified=true", got)
	}
}

func TestMiddleware_Rejeicoes(t *testing.T) {
	priv := gerarChaveRSA(t)
	outra := gerarChaveRSA(t)
	cfg := Config{RealmURL: issTeste, AllowedClientIDs: []string{azpTeste}}

	casos := []struct {
		nome  string
		token func() string
		bare  bool // sem header Authorization
	}{
		{
			nome: "sem Authorization",
			bare: true,
		},
		{
			nome: "alg HS256",
			token: func() string {
				tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsValidas())
				tok.Header["kid"] = "kid-1"
				s, err := tok.SignedString([]byte("segredo-hs256"))
				if err != nil {
					t.Fatalf("assinar HS256: %v", err)
				}
				return s
			},
		},
		{
			nome: "alg none",
			token: func() string {
				tok := jwt.NewWithClaims(jwt.SigningMethodNone, claimsValidas())
				tok.Header["kid"] = "kid-1"
				s, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
				if err != nil {
					t.Fatalf("assinar none: %v", err)
				}
				return s
			},
		},
		{
			nome: "assinatura de outra chave",
			token: func() string {
				return assinarRS256(t, outra, "kid-1", claimsValidas())
			},
		},
		{
			nome: "kid desconhecido",
			token: func() string {
				return assinarRS256(t, priv, "kid-9", claimsValidas())
			},
		},
		{
			nome: "iss divergente",
			token: func() string {
				c := claimsValidas()
				c["iss"] = "https://kc.example/realms/outro"
				return assinarRS256(t, priv, "kid-1", c)
			},
		},
		{
			nome: "azp fora da allowlist",
			token: func() string {
				c := claimsValidas()
				c["azp"] = "fb-apu02"
				return assinarRS256(t, priv, "kid-1", c)
			},
		},
		{
			nome: "exp ausente",
			token: func() string {
				c := claimsValidas()
				delete(c, "exp")
				return assinarRS256(t, priv, "kid-1", c)
			},
		},
		{
			nome: "exp expirado além do leeway",
			token: func() string {
				c := claimsValidas()
				c["exp"] = time.Now().Add(-time.Minute).Unix()
				return assinarRS256(t, priv, "kid-1", c)
			},
		},
	}

	for _, tc := range casos {
		t.Run(tc.nome, func(t *testing.T) {
			h := middlewareDeTeste(t, priv, cfg)
			req := httptest.NewRequest(http.MethodPost, "/api/auth/sso/keycloak", nil)
			if !tc.bare {
				req.Header.Set("Authorization", "Bearer "+tc.token())
			}
			w := httptest.NewRecorder()
			h(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
			}
			var env erroEnvelope
			if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode envelope: %v (body=%s)", err, w.Body.String())
			}
			if env.Error.Code != "SSO_TOKEN_INVALIDO" {
				t.Fatalf("code = %q, want SSO_TOKEN_INVALIDO", env.Error.Code)
			}
		})
	}
}

func TestMiddleware_LeewayToleraDesvioPequeno(t *testing.T) {
	priv := gerarChaveRSA(t)
	h := middlewareDeTeste(t, priv, Config{RealmURL: issTeste, AllowedClientIDs: []string{azpTeste}})

	c := claimsValidas()
	c["exp"] = time.Now().Add(-15 * time.Second).Unix() // expirado, mas dentro dos 30s de leeway
	tok := assinarRS256(t, priv, "kid-1", c)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/sso/keycloak", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — 15s de expiração deveria caber no leeway de 30s (body=%s)", w.Code, w.Body.String())
	}
}

func TestCarregarConfig(t *testing.T) {
	t.Setenv("IAM_BASE_URL", "https://kc.example/realms/ferreiracosta/")
	t.Setenv("IAM_ALLOWED_CLIENT_IDS", " stockflow-web , , fb-apu02 ")

	cfg := CarregarConfig()
	if cfg.RealmURL != "https://kc.example/realms/ferreiracosta" {
		t.Fatalf("RealmURL = %q, want sem barra final", cfg.RealmURL)
	}
	if !reflect.DeepEqual(cfg.AllowedClientIDs, []string{"stockflow-web", "fb-apu02"}) {
		t.Fatalf("AllowedClientIDs = %#v, want [stockflow-web fb-apu02] (trim + descarta vazios)", cfg.AllowedClientIDs)
	}
	if !cfg.Habilitado() {
		t.Fatal("Habilitado() = false, want true com RealmURL preenchido")
	}
}

func TestCarregarConfig_VaziaDesabilita(t *testing.T) {
	t.Setenv("IAM_BASE_URL", "")
	t.Setenv("IAM_ALLOWED_CLIENT_IDS", "")

	cfg := CarregarConfig()
	if cfg.Habilitado() {
		t.Fatal("Habilitado() = true, want false sem IAM_BASE_URL")
	}
	if cfg.AllowedClientIDs != nil {
		t.Fatalf("AllowedClientIDs = %#v, want nil", cfg.AllowedClientIDs)
	}
}
