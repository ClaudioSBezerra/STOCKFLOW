// Package handlers, arquivo reservas.go: fronteira HTTP da lista de quem
// reservou o saldo de um Produto num Estoque — Story 11.3 (Epic 11, FR-50).
//
// Registro em newMux (main.go): GET /api/produtos/{id}/estoques/{estoqueId}/reservas
// -> RequireAuth (qualquer conta autenticada, como o detalhe do Produto).
package handlers

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// ListarReservasSaldoHandler expõe
// GET /api/produtos/{id}/estoques/{estoqueId}/reservas:
// `200 {"reservas":[{"pedidoId","solicitante","quantidade","criadoEm"}, ...]}`
// dos Pedidos pendentes com reserva ativa do par, no escopo da Empresa.
// Produto/Estoque inexistente, alheio ou malformado -> `404 NOT_FOUND`.
func ListarReservasSaldoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("ListarReservasSaldoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		reservas, err := services.ListarReservasSaldo(db, empresa.ID, r.PathValue("id"), r.PathValue("estoqueId"))
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]any{"reservas": reservas})
		case errors.Is(err, services.ErrReservaAlvoNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "produto ou estoque não encontrado")
		default:
			slog.Error("falha ao listar reservas de saldo", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar reservas")
		}
	}
}
