package iam

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Config carrega, uma vez no startup, os parâmetros do realm Keycloak lidos do
// ambiente (molde de services.CarregarEmailConfig). Nenhum valor aqui é
// segredo — são os mesmos dados já visíveis na URL de authorize de um client
// público OIDC/PKCE.
type Config struct {
	// RealmURL é IAM_BASE_URL com a barra final removida — usado tanto como
	// `iss` esperado quanto como base das URLs OIDC do realm.
	RealmURL string
	// AllowedClientIDs é a allowlist de `azp` (IAM_ALLOWED_CLIENT_IDS, split
	// por vírgula, sem entradas vazias).
	AllowedClientIDs []string
}

// CarregarConfig lê IAM_BASE_URL e IAM_ALLOWED_CLIENT_IDS do ambiente.
func CarregarConfig() Config {
	return Config{
		RealmURL:         strings.TrimRight(os.Getenv("IAM_BASE_URL"), "/"),
		AllowedClientIDs: parseClientIDs(os.Getenv("IAM_ALLOWED_CLIENT_IDS")),
	}
}

// Habilitado reporta se o login federado deve ser montado — basta o realm
// estar configurado. A allowlist vazia não desabilita o registro da rota (o
// middleware simplesmente recusaria todo token no passo do `azp`); main.go
// loga um aviso nesse caso.
func (c Config) Habilitado() bool {
	return c.RealmURL != ""
}

func parseClientIDs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var ids []string
	for _, parte := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(parte); v != "" {
			ids = append(ids, v)
		}
	}
	return ids
}

// ctxKey é um tipo não-exportado para as chaves de contexto injetadas pelo
// Middleware — mesma proteção contra colisão usada em middleware/auth.go.
type ctxKey int

const (
	emailCtxKey ctxKey = iota
	emailVerifiedCtxKey
)

// EmailDaSessaoSSO devolve a claim `email` extraída do token pelo Middleware,
// ou "" se o middleware não rodou ou o token não trouxe e-mail.
func EmailDaSessaoSSO(ctx context.Context) string {
	email, _ := ctx.Value(emailCtxKey).(string)
	return email
}

// EmailVerificadoSSO devolve a claim `email_verified` extraída do token pelo
// Middleware, ou false se ausente. Quem usa o e-mail como identidade (o
// handler de troca) DEVE checar isto antes de confiar na claim `email`: um
// e-mail não verificado pode ter sido trocado pelo próprio usuário no
// Keycloak sem reconfirmação.
func EmailVerificadoSSO(ctx context.Context) bool {
	v, _ := ctx.Value(emailVerifiedCtxKey).(bool)
	return v
}

// erroEnvelope espelha o formato fixo de erro (AD-14) de handlers/auth.go —
// duplicado aqui de propósito: este pacote nunca importa `handlers`.
type erroEnvelope struct {
	Error erroDetalhe `json:"error"`
}

type erroDetalhe struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func escreverErro(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(erroEnvelope{Error: erroDetalhe{Code: code, Message: message}})
}

// Middleware valida um access token do realm Keycloak antes de deixar a
// requisição chegar ao handler de troca. Exige `Authorization: Bearer
// <token>`; assinatura RS256 (qualquer outro `alg`, incl. `none`, é
// rejeitado); `exp` obrigatório com leeway de 30s; `iss` exatamente igual a
// cfg.RealmURL; `azp` na allowlist. Qualquer falha -> 401
// SSO_TOKEN_INVALIDO no envelope AD-14, e o handler seguinte nunca roda. Em
// sucesso, injeta `email`/`email_verified` no contexto.
func Middleware(jwks *JWKSClient, cfg Config) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			const prefixo = "Bearer "
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, prefixo) {
				escreverErro(w, http.StatusUnauthorized, "SSO_TOKEN_INVALIDO", "token de acesso ausente ou inválido")
				return
			}
			tokenStr := strings.TrimSpace(strings.TrimPrefix(auth, prefixo))
			if tokenStr == "" {
				escreverErro(w, http.StatusUnauthorized, "SSO_TOKEN_INVALIDO", "token de acesso ausente ou inválido")
				return
			}

			claims := jwt.MapClaims{}
			_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
				kid, _ := t.Header["kid"].(string)
				return jwks.GetKey(kid)
			},
				jwt.WithValidMethods([]string{"RS256"}),
				jwt.WithIssuer(cfg.RealmURL),
				jwt.WithExpirationRequired(),
				jwt.WithLeeway(30*time.Second),
			)
			if err != nil {
				slog.Warn("iam: token SSO rejeitado", "error", err)
				escreverErro(w, http.StatusUnauthorized, "SSO_TOKEN_INVALIDO", "token de acesso inválido ou expirado")
				return
			}

			azp, _ := claims["azp"].(string)
			if !clientIDPermitido(azp, cfg.AllowedClientIDs) {
				slog.Warn("iam: azp fora da allowlist", "azp", azp)
				escreverErro(w, http.StatusUnauthorized, "SSO_TOKEN_INVALIDO", "token emitido por cliente não autorizado")
				return
			}

			email, _ := claims["email"].(string)
			emailVerificado, _ := claims["email_verified"].(bool)

			ctx := context.WithValue(r.Context(), emailCtxKey, email)
			ctx = context.WithValue(ctx, emailVerifiedCtxKey, emailVerificado)
			next(w, r.WithContext(ctx))
		}
	}
}

func clientIDPermitido(azp string, allowlist []string) bool {
	if azp == "" {
		return false
	}
	for _, id := range allowlist {
		if id == azp {
			return true
		}
	}
	return false
}
