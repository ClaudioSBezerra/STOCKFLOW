// Package handlers, arquivo lotes.go: fronteira HTTP do lançamento de saldo
// com Lote e Data de Validade — Story 11.1 (Epic 11, FR-47). Molde de
// RegistrarBaixaHandler (movimentacoes.go): sem regra de negócio própria,
// traduz os erros de services/lotes.go para o envelope de erro fixo (AD-14).
//
// Registro em newMux (main.go): POST /api/lotes -> RequireAuth ->
// RequireRole(almoxarife). O 401/403 é decidido pelos middlewares.
package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"stockflow/backend/middleware"
	"stockflow/backend/realtime"
	"stockflow/backend/services"
)

// lancarSaldoRequest é o corpo aceito por POST /api/lotes. `dataValidade`
// ausente, `null` ou `""` = validade desconhecida.
type lancarSaldoRequest struct {
	ProdutoID    string  `json:"produtoId"`
	EstoqueID    string  `json:"estoqueId"`
	Quantidade   float64 `json:"quantidade"`
	DataValidade *string `json:"dataValidade"`
}

// LancarSaldoHandler expõe POST /api/lotes: cria SEMPRE um Lote novo e a
// Movimentação `entrada`. `201 {"lote": {...}}` no sucesso, publicando
// `movimentacoes` (created, id da Movimentação) e `produtos` (updated, id do
// Produto) no registro SSE. `400 VALIDATION_ERROR` para payload/quantidade/
// data inválidos; `404 NOT_FOUND` para Produto/Estoque inexistente, alheio,
// mesclado ou malformado.
func LancarSaldoHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("LancarSaldoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req lancarSaldoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}
		dataValidade := ""
		if req.DataValidade != nil {
			dataValidade = *req.DataValidade
		}

		lote, err := services.LancarSaldo(db, empresa.ID, usuario.ID, req.ProdutoID, req.EstoqueID, req.Quantidade, dataValidade)
		var erroValidacao *services.ErroLoteValidacao
		switch {
		case err == nil:
			registro.Publish(empresa.ID, "movimentacoes", realtime.Evento{ID: lote.MovimentacaoID, Change: "created"})
			registro.Publish(empresa.ID, "produtos", realtime.Evento{ID: lote.ProdutoID, Change: "updated"})
			escreverJSON(w, http.StatusCreated, map[string]any{"lote": lote})
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		case errors.Is(err, services.ErrLoteAlvoNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "produto ou estoque não encontrado")
		default:
			slog.Error("falha ao lançar saldo", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao lançar saldo")
		}
	}
}
