// Package handlers, arquivo normalizacao.go: fronteira HTTP da Detecção de
// inconsistências dimensionais — Story 6.1, spec-6-1. Molde exato de
// handlers/movimentacoes.go (ListarMovimentacoesHandler): decodifica sessão
// do contexto, chama o service, serializa; nenhuma regra de negócio própria.
//
// Registro em newMux (main.go):
//   - GET /api/normalizacao/inconsistencias -> RequireAuth ->
//     RequireRole(almoxarife) (mesmo gate mínimo de GET /api/movimentacoes):
//     o 401/403 é decidido pelos middlewares; este handler só executa depois
//     de os dois passarem. Rota só-leitura, sem query params — a regra de
//     análise (parsing tolerante, heurística de campo único vazio) vive em
//     services.AnalisarInconsistencias. Resposta: 200 {"sugestoes":[...]}.
package handlers

import (
	"database/sql"
	"log/slog"
	"net/http"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// AnalisarInconsistenciasHandler expõe GET /api/normalizacao/inconsistencias
// (Story 6.1, spec-6-1): varre todos os Produtos sob demanda e devolve a
// lista de sugestões de correção dimensional — leitura pontual, nenhuma
// escrita (aplicar/ignorar é Story 6.2). Guard de contexto ausente -> 500,
// mesmo padrão de ListarMovimentacoesHandler. Erro do service -> 500
// INTERNAL_ERROR.
func AnalisarInconsistenciasHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("AnalisarInconsistenciasHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		sugestoes, err := services.AnalisarInconsistencias(db)
		if err != nil {
			slog.Error("falha ao analisar inconsistências", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao analisar inconsistências")
			return
		}

		escreverJSON(w, http.StatusOK, map[string]any{"sugestoes": sugestoes})
	}
}
