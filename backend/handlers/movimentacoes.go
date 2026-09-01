// Package handlers, arquivo movimentacoes.go: fronteira HTTP de Registrar
// Baixa (consumo) — Story 5.1, spec-5-1. Molde exato de handlers/produtos.go
// (CriarProdutoHandler): decodifica/serializa JSON, traduz os erros de
// services/movimentacoes.go para o envelope de erro fixo (AD-14) e nunca
// contém regra de negócio própria.
//
// Registro em newMux (main.go):
//   - POST /api/produtos/{id}/estoques/{estoqueId}/baixa -> RequireAuth ->
//     RequireRole(almoxarife); corpo `{"quantidade": number}`. O 403 para
//     papel abaixo de `almoxarife` é decidido inteiramente por RequireRole
//     — este handler só executa quando o papel já passou nesse gate.
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

// registrarBaixaRequest é o corpo aceito por
// POST /api/produtos/{id}/estoques/{estoqueId}/baixa.
type registrarBaixaRequest struct {
	Quantidade float64 `json:"quantidade"`
}

// RegistrarBaixaHandler expõe POST /api/produtos/{id}/estoques/{estoqueId}/baixa:
// registra o consumo de `quantidade` unidades do Produto `{id}` no Estoque
// `{estoqueId}`. `201 {"movimentacao": {...}}` no sucesso, publicando
// `{"resource":"movimentacoes","id":<nova>,"change":"created"}` no canal
// `movimentacoes` (AD-3 do epic-5-context.md — mesmo padrão de
// CriarProdutoHandler para o canal `produtos`). `400 VALIDATION_ERROR` para
// quantidade inválida; `409 CONFLICT` para quantidade indisponível
// (incluindo par Produto/Estoque sem linha ou id malformado, ver Design
// Notes de spec-5-1).
func RegistrarBaixaHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("RegistrarBaixaHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req registrarBaixaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		produtoID := r.PathValue("id")
		estoqueID := r.PathValue("estoqueId")

		mov, err := services.RegistrarBaixa(db, produtoID, estoqueID, usuario.ID, req.Quantidade)
		var erroValidacao *services.ErroMovimentacaoValidacao
		var erroIndisponivel *services.ErroQuantidadeIndisponivel
		switch {
		case err == nil:
			registro.Publish("movimentacoes", realtime.Evento{ID: mov.ID, Change: "created"})
			escreverJSON(w, http.StatusCreated, map[string]any{"movimentacao": mov})
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		case errors.As(err, &erroIndisponivel):
			escreverErro(w, http.StatusConflict, "CONFLICT", erroIndisponivel.Error())
		default:
			slog.Error("falha ao registrar baixa", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao registrar baixa")
		}
	}
}
