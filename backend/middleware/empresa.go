// Package middleware, arquivo empresa.go: resolução da Empresa a partir do
// slug da rota — Story 9.1 (Epic 9, Multi-Empresa e Plataforma), spec-9-1.
//
// Toda rota de negócio vive sob `/e/{slug}/api/...`. RequireEmpresa é o
// ÚNICO ponto do sistema que traduz esse slug em uma Empresa (AD-19):
// resolve UMA vez por requisição, injeta a Empresa no contexto e a repassa,
// dali em diante, como argumento explícito a cada função de service (AD-8
// forma 3 — a camada `services/` não importa `net/http` nem `context`).
//
// Nenhum service re-deriva a Empresa; nenhum handler a aceita de corpo,
// query string ou header. Slug desconhecido, sintaticamente inválido ou de
// Empresa `inativa` colapsam TODOS em 404 NOT_FOUND — nunca 403, nunca
// revelando que um slug existe mas está desligado.
package middleware

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"stockflow/backend/services"
)

// empresaCtxKey é a chave de contexto da Empresa resolvida. Vive no mesmo
// tipo não-exportado `ctxKey` de usuarioSessaoCtxKey (auth.go) — a constante
// abaixo continua a sequência iota daquele arquivo sem colidir com ela.
const empresaCtxKey ctxKey = 1

// RequireEmpresa resolve a Empresa do `{slug}` da rota e a injeta no
// contexto da requisição. É composto POR FORA de RequireAuth em newMux —
// `RequireEmpresa(db)(RequireAuth(...)(...))` — para que uma requisição a um
// slug inexistente receba 404 antes de qualquer validação de token: a
// Empresa é a fronteira mais externa do produto, e um slug que não resolve
// não tem nem rota nem sessão.
//
// Qualquer falha de resolução (slug ausente da rota, fora da forma canônica,
// inexistente ou de Empresa `inativa`) -> 404 NOT_FOUND com o mesmo corpo
// `{"error":{"code":"NOT_FOUND",...}}`. Erro de banco -> 500 INTERNAL_ERROR.
func RequireEmpresa(db *sql.DB) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			slug := r.PathValue("slug")

			empresa, err := services.BuscarEmpresaPorSlug(db, slug)
			if err != nil {
				if errors.Is(err, services.ErrEmpresaNaoEncontrada) {
					escreverErro(w, http.StatusNotFound, "NOT_FOUND", "empresa não encontrada")
					return
				}
				slog.Error("falha ao resolver empresa do slug", "slug", slug, "error", err)
				escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver empresa da requisição")
				return
			}

			ctx := context.WithValue(r.Context(), empresaCtxKey, empresa)
			next(w, r.WithContext(ctx))
		}
	}
}

// EmpresaDaRequisicao extrai a Empresa injetada por RequireEmpresa no
// contexto. O segundo retorno é false se o handler foi chamado fora de
// RequireEmpresa (nunca deveria acontecer em produção — toda rota de negócio
// é registrada através dele em newMux); os handlers tratam esse caso como
// erro de composição, com 500 INTERNAL_ERROR, mesmo padrão de
// UsuarioDaSessao.
func EmpresaDaRequisicao(ctx context.Context) (services.Empresa, bool) {
	e, ok := ctx.Value(empresaCtxKey).(services.Empresa)
	return e, ok
}
