// Package middleware, arquivo plataforma.go: a sessão do Dono da Plataforma
// — Story 9.2 (Epic 9, Multi-Empresa e Plataforma), spec-9-2, AD-21.
//
// RequireDonoPlataforma é o gêmeo de RequireAuth para a identidade disjunta
// `donos_plataforma`: mesmo formato de token (AD-6), mas exige o claim
// `aud = "plataforma"` — um access token de `usuarios` (sem `aud`) nunca
// passa aqui, e RequireAuth recusa o token do Dono. As rotas do Dono vivem em
// `/api/plataforma/...`, fora do prefixo de Empresa.
package middleware

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"stockflow/backend/services"
)

// donoPlataformaCtxKey é a chave de contexto do Dono resolvido — continua a
// sequência de usuarioSessaoCtxKey (0) e empresaCtxKey (1).
const donoPlataformaCtxKey ctxKey = 2

// RequireDonoPlataforma exige `Authorization: Bearer <token>` com um access
// JWT do Dono (`aud = "plataforma"`). Token ausente, malformado, expirado,
// com assinatura inválida ou sem esse `aud` -> 401 TOKEN_EXPIRED. Dono
// inexistente ou inativo -> 401 SESSION_REVOKED. O Dono é relido do Postgres
// a cada requisição (AD-6), nunca do claim.
func RequireDonoPlataforma(db *sql.DB, jwtSecret []byte) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			const prefixo = "Bearer "
			authHeader := r.Header.Get("Authorization")
			tokenStr := strings.TrimSpace(strings.TrimPrefix(authHeader, prefixo))
			if !strings.HasPrefix(authHeader, prefixo) || tokenStr == "" {
				escreverErro(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "token de acesso ausente ou inválido")
				return
			}

			claims := &services.PlataformaClaims{}
			parsedToken, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("método de assinatura inesperado: %v", t.Header["alg"])
				}
				return jwtSecret, nil
			}, jwt.WithAudience(services.AudienciaPlataforma))
			if err != nil || !parsedToken.Valid || claims.Subject == "" {
				escreverErro(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "token de acesso inválido ou expirado")
				return
			}

			dono, err := services.BuscarDonoSessao(db, claims.Subject)
			if err != nil {
				if errors.Is(err, services.ErrDonoSessaoNaoEncontrado) {
					escreverErro(w, http.StatusUnauthorized, "SESSION_REVOKED", "sessão revogada")
					return
				}
				slog.Error("falha ao resolver dono da plataforma da sessão", "error", err)
				escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver a sessão")
				return
			}
			if !dono.Ativo {
				escreverErro(w, http.StatusUnauthorized, "SESSION_REVOKED", "sessão revogada")
				return
			}

			ctx := context.WithValue(r.Context(), donoPlataformaCtxKey, dono)
			next(w, r.WithContext(ctx))
		}
	}
}

// DonoDaSessao extrai o Dono injetado por RequireDonoPlataforma. O segundo
// retorno é false fora dessa composição (erro de montagem das rotas).
func DonoDaSessao(ctx context.Context) (services.DonoSessao, bool) {
	d, ok := ctx.Value(donoPlataformaCtxKey).(services.DonoSessao)
	return d, ok
}
