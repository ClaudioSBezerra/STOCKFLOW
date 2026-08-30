// Package middleware implementa RequireAuth (Story 1.4, AD-6): exige
// `Authorization: Bearer <token>`, valida o access JWT via
// golang-jwt/jwt/v5, e resolve o usuário SEMPRE a partir do Postgres a cada
// requisição (services.BuscarUsuarioSessao) — o claim do token só carrega
// `sub`, nunca papel/ativo/nome/email, para que nenhum handler downstream
// tenha a tentação de confiar em um estado carimbado no token em vez de
// reconsultar `usuarios`.
package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"stockflow/backend/services"
)

// ctxKey é um tipo não-exportado para a chave de contexto — evita colisão
// com chaves de outros pacotes (mesmo padrão recomendado por context.WithValue).
type ctxKey int

const usuarioSessaoCtxKey ctxKey = iota

// erroEnvelope espelha o mesmo formato fixo de erro (AD-14) usado em
// handlers/auth.go: {"error":{"code","message"}}. Duplicado aqui
// deliberadamente — middleware nunca importa handlers (a composição
// RequireAuth(handlers.MeHandler(...)) acontece em main.go, na direção
// oposta) — para não criar um ciclo de import.
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

// RequireAuth exige `Authorization: Bearer <token>` válido. Token
// ausente/malformado/assinatura inválida/expirado -> 401 TOKEN_EXPIRED.
// Token válido mas usuário não encontrado ou `ativo=false` -> 401
// SESSION_REVOKED — garante que uma conta desativada perde acesso já na
// próxima requisição, sem esperar o TTL de 30min do access token.
func RequireAuth(db *sql.DB, jwtSecret []byte) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			const prefixo = "Bearer "
			if !strings.HasPrefix(authHeader, prefixo) {
				escreverErro(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "token de acesso ausente ou inválido")
				return
			}
			tokenStr := strings.TrimSpace(strings.TrimPrefix(authHeader, prefixo))
			if tokenStr == "" {
				escreverErro(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "token de acesso ausente ou inválido")
				return
			}

			claims := &services.AcessoClaims{}
			parsedToken, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("método de assinatura inesperado: %v", t.Header["alg"])
				}
				return jwtSecret, nil
			})
			if err != nil || !parsedToken.Valid || claims.Subject == "" {
				escreverErro(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "token de acesso inválido ou expirado")
				return
			}

			usuario, err := services.BuscarUsuarioSessao(db, claims.Subject)
			if err != nil {
				if errors.Is(err, services.ErrUsuarioSessaoNaoEncontrado) {
					escreverErro(w, http.StatusUnauthorized, "SESSION_REVOKED", "sessão revogada")
					return
				}
				escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário da sessão")
				return
			}
			if !usuario.Ativo {
				escreverErro(w, http.StatusUnauthorized, "SESSION_REVOKED", "sessão revogada")
				return
			}

			// origem (Story 1.11) vem SEMPRE do claim do token, nunca de
			// BuscarUsuarioSessao (que não é sequer capaz de preenchê-lo — não é
			// estado de conta). Um access JWT emitido antes desta story (sem o
			// claim `origem`) decodifica como string vazia — o gate de MFA em
			// RequireRole trata isso como "não senha" (fail-open só para o gate de
			// MFA, nunca para autenticidade), até a sessão expirar naturalmente.
			usuario.Origem = claims.Origem

			ctx := context.WithValue(r.Context(), usuarioSessaoCtxKey, usuario)
			next(w, r.WithContext(ctx))
		}
	}
}

// UsuarioDaSessao extrai o UsuarioSessao injetado por RequireAuth no
// contexto da requisição. O segundo retorno é false se o handler foi
// chamado fora de RequireAuth (nunca deveria acontecer em produção, já que
// todo endpoint protegido é sempre registrado através dele).
func UsuarioDaSessao(ctx context.Context) (services.UsuarioSessao, bool) {
	u, ok := ctx.Value(usuarioSessaoCtxKey).(services.UsuarioSessao)
	return u, ok
}
