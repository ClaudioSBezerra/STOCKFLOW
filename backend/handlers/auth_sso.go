package handlers

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"stockflow/backend/iam"
	"stockflow/backend/services"
)

// scopesSSOPadrao é o valor de `scopes` devolvido por SSOConfigHandler quando
// IAM_SCOPES não está definido.
const scopesSSOPadrao = "openid profile email"

// SSOConfigHandler expõe GET /api/auth/sso/config (público, sem middleware): a
// tela de Login usa a resposta para decidir se mostra o botão "Entrar com
// Ferreira Costa". Se QUALQUER parâmetro obrigatório faltar -> `{"enabled":
// false}` e nada mais. Nenhum valor sensível é exposto — é o mesmo dado já
// visível na URL de authorize de um client público OIDC/PKCE.
func SSOConfigHandler(cfg iam.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientID := os.Getenv("IAM_CLIENT_ID")
		redirectURI := os.Getenv("IAM_REDIRECT_URI")

		if cfg.RealmURL == "" || clientID == "" || redirectURI == "" || len(cfg.AllowedClientIDs) == 0 {
			escreverJSON(w, http.StatusOK, map[string]any{"enabled": false})
			return
		}

		scopes := os.Getenv("IAM_SCOPES")
		if scopes == "" {
			scopes = scopesSSOPadrao
		}

		escreverJSON(w, http.StatusOK, map[string]any{
			"enabled":      true,
			"base_url":     cfg.RealmURL,
			"client_id":    clientID,
			"redirect_uri": redirectURI,
			"scopes":       scopes,
		})
	}
}

// KeycloakSSOHandler expõe POST /api/auth/sso/keycloak, SEMPRE montado atrás
// do middleware iam.Middleware em newMux (nunca exposto direto). Não lê corpo:
// reusa o header Authorization já validado pelo middleware. Troca o access
// token do Keycloak por uma sessão própria do stockflow (mesmo par
// access+refresh do login por senha, AD-6) — busca a conta por e-mail
// case-insensitive e NUNCA cria. O papel é sempre o de `usuarios`, nunca do
// token; o token do Keycloak nunca é persistido.
func KeycloakSSOHandler(db *sql.DB, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := iam.EmailDaSessaoSSO(r.Context())
		if email == "" {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "o token do SSO não trouxe e-mail")
			return
		}

		if !iam.EmailVerificadoSSO(r.Context()) {
			escreverErro(w, http.StatusUnauthorized, "EMAIL_NOT_VERIFIED",
				"Confirme o e-mail da sua conta corporativa no Ferreira Costa antes de entrar.")
			return
		}

		usuario, err := services.BuscarUsuarioPorEmailSSO(db, email)
		if err != nil {
			if errors.Is(err, services.ErrContaSSONaoEncontrada) {
				escreverErro(w, http.StatusUnauthorized, "SSO_SEM_CONTA",
					"Não encontramos uma conta do stockflow para este e-mail. Cadastre-se primeiro.")
				return
			}
			slog.Error("falha ao buscar conta para troca de SSO", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar login federado")
			return
		}

		if !usuario.Ativo {
			// Conta desativada não autentica por SSO — coerente com a Story 1.8 e
			// o epic-context. Mesmo código do login por senha, sem mensagem
			// distinta.
			escreverErro(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "E-mail ou senha inválidos.")
			return
		}

		// origem="sso" (Story 1.11): o realm Keycloak já impõe MFA a
		// gestor/adm — esta sessão NUNCA passa pelo gate de MFA local
		// (middleware.RequireRole), então emitirSessaoEResponder nunca é chamado
		// com "sso" fora daqui.
		emitirSessaoEResponder(w, r, db, jwtSecret, usuario, "sso")
	}
}

// LogoutHandler expõe POST /api/auth/logout (sem middleware): revoga a sessão
// do cookie de refresh (se houver) e SEMPRE limpa o cookie, respondendo 204.
// Idempotente: cookie ausente ou já revogado também é 204. Não distingue
// sessão SSO de sessão por senha — o RP-initiated logout é decidido no
// cliente.
func LogoutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(refreshTokenCookieName); err == nil {
			if err := services.RevogarSessaoPorRefreshToken(db, cookie.Value); err != nil {
				slog.Error("falha ao revogar sessão no logout", "error", err)
				// Segue mesmo assim: o cliente já considerou a sessão encerrada e o
				// cookie precisa ser limpo de qualquer forma.
			}
		}
		clearRefreshCookie(w, r)
		w.WriteHeader(http.StatusNoContent)
	}
}
