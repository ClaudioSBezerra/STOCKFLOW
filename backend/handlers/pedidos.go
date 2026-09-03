// Package handlers, arquivo pedidos.go: fronteira HTTP do Envio de Pedido —
// Story 7.2 (Epic 7, Pedidos de Retirada), spec-7-2. Molde exato de
// handlers/movimentacoes.go (RegistrarBaixaHandler): decodifica/serializa
// JSON, traduz os erros de services/pedidos.go para o envelope de erro fixo
// (AD-14) e nunca contém regra de negócio própria.
//
// Registro em newMux (main.go): POST /api/pedidos fica atrás só de
// RequireAuth — SEM RequireRole, qualquer conta autenticada (`usuario`+)
// envia seu próprio Pedido, mesmo mínimo de papel do Carrinho (Story 7.1).
// `usuarioID` vem sempre de middleware.UsuarioDaSessao dentro do handler,
// nunca de um campo do corpo (Always, spec-7-2).
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

// submeterPedidoRequest é o corpo aceito por POST /api/pedidos. Um eventual
// campo `usuarioId` no corpo é ignorado — nem sequer decodificado — a
// identidade gravada em `pedidos.usuario_id` vem sempre da sessão (Always,
// spec-7-2).
type submeterPedidoRequest struct {
	Solicitante     string `json:"solicitante"`
	ObraCentroCusto string `json:"obraCentroCusto"`
	Observacao      string `json:"observacao"`
}

// SubmeterPedidoHandler expõe POST /api/pedidos: envia o carrinho ativo do
// usuário da sessão como um Pedido `pendente`. `201 {"pedido": {...}}` no
// sucesso, publicando `{"resource":"pedidos","id":<novo>,"change":"created"}`
// no canal `pedidos` (AD-3 do epic-7-context.md, payload mínimo — Never de
// spec-7-2: nunca inclui os itens). `400 VALIDATION_ERROR` para
// solicitante/obraCentroCusto ausentes; `409 CONFLICT` para carrinho vazio
// ou disponibilidade insuficiente em algum item (mensagem já cita os
// nomes).
func SubmeterPedidoHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("SubmeterPedidoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req submeterPedidoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		pedido, err := services.SubmeterPedido(db, usuario.ID, req.Solicitante, req.ObraCentroCusto, req.Observacao)
		var erroValidacao *services.ErroPedidoValidacao
		var erroIndisponivel *services.ErroPedidoIndisponivel
		switch {
		case err == nil:
			registro.Publish("pedidos", realtime.Evento{ID: pedido.ID, Change: "created"})
			escreverJSON(w, http.StatusCreated, map[string]any{"pedido": pedido})
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		case errors.Is(err, services.ErrPedidoCarrinhoVazio):
			escreverErro(w, http.StatusConflict, "CONFLICT", services.ErrPedidoCarrinhoVazio.Error())
		case errors.As(err, &erroIndisponivel):
			escreverErro(w, http.StatusConflict, "CONFLICT", erroIndisponivel.Error())
		default:
			slog.Error("falha ao submeter pedido", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao enviar pedido")
		}
	}
}
