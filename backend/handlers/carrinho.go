// Package handlers, arquivo carrinho.go: fronteira HTTP do Carrinho de
// reserva — Story 7.1 (Epic 7, Pedidos de Retirada), spec-7-1. Molde exato
// de handlers/movimentacoes.go: decodifica/serializa JSON, traduz os erros
// de services/carrinho.go para o envelope de erro fixo (AD-14) e nunca
// contém regra de negócio própria.
//
// Registro em newMux (main.go): as três rotas ficam atrás só de
// RequireAuth — SEM RequireRole, qualquer conta autenticada (`usuario`+)
// monta seu próprio carrinho. `usuarioID` vem sempre de
// middleware.UsuarioDaSessao (o `usuario.ID` da sessão), nunca de um campo
// do corpo/rota (Always, spec-7-1).
package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// adicionarItemCarrinhoRequest é o corpo aceito por POST /api/carrinho/itens.
type adicionarItemCarrinhoRequest struct {
	ProdutoID  string  `json:"produtoId"`
	EstoqueID  string  `json:"estoqueId"`
	Quantidade float64 `json:"quantidade"`
}

// AdicionarItemCarrinhoHandler expõe POST /api/carrinho/itens: adiciona
// `quantidade` unidades do Produto `produtoId` no Estoque `estoqueId` ao
// carrinho do usuário da sessão. `201 {"item": {...}}` no sucesso.
// `400 VALIDATION_ERROR` para quantidade inválida; `404 NOT_FOUND` para
// Produto inexistente/mesclado OU Estoque inexistente/malformado;
// `409 CONFLICT` para quantidade indisponível (par sem linha em
// produto_estoque, Estoque existente, ver Design Notes de spec-7-1). Sem
// publicação em canal SSE (Never, spec-7-1): o carrinho sincroniza só por
// refetch da própria aba.
func AdicionarItemCarrinhoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("AdicionarItemCarrinhoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req adicionarItemCarrinhoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		item, err := services.AdicionarItemCarrinho(db, usuario.ID, req.ProdutoID, req.EstoqueID, req.Quantidade)
		var erroValidacao *services.ErroCarrinhoValidacao
		var erroIndisponivel *services.ErroCarrinhoIndisponivel
		switch {
		case err == nil:
			escreverJSON(w, http.StatusCreated, map[string]any{"item": item})
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		case errors.Is(err, services.ErrCarrinhoProdutoNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "produto não encontrado")
		case errors.Is(err, services.ErrCarrinhoEstoqueNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "estoque não encontrado")
		case errors.As(err, &erroIndisponivel):
			escreverErro(w, http.StatusConflict, "CONFLICT", erroIndisponivel.Error())
		default:
			slog.Error("falha ao adicionar item ao carrinho", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao adicionar item ao carrinho")
		}
	}
}

// ListarCarrinhoHandler expõe GET /api/carrinho: os itens ativos do carrinho
// do usuário da sessão, mais a limpeza preguiçosa de itens obsoletos
// (Always, spec-7-1) — Produto mesclado (Story 6.4) ou Estoque excluído
// (Story 2.2) desde a última leitura. Resposta sempre 200
// `{"itens":[...],"removidos":[...]}`, nunca 404 mesmo com carrinho vazio.
func ListarCarrinhoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("ListarCarrinhoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		itens, removidos, err := services.ListarCarrinho(db, usuario.ID)
		if err != nil {
			slog.Error("falha ao listar carrinho", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar carrinho")
			return
		}

		escreverJSON(w, http.StatusOK, map[string]any{"itens": itens, "removidos": removidos})
	}
}

// RemoverItemCarrinhoHandler expõe
// DELETE /api/carrinho/itens/{produtoId}/{estoqueId}: remove a linha do par
// do carrinho do usuário da sessão. `204` sem corpo no sucesso;
// `404 NOT_FOUND` quando o par não está no carrinho deste usuário (mesmo
// que exista no carrinho de outro usuário — o WHERE já escopa por
// usuario_id, Always de spec-7-1). Molde exato de ExcluirEstoqueHandler
// (handlers/estoques.go).
func RemoverItemCarrinhoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("RemoverItemCarrinhoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		produtoID := r.PathValue("produtoId")
		estoqueID := r.PathValue("estoqueId")

		err := services.RemoverItemCarrinho(db, usuario.ID, produtoID, estoqueID)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, services.ErrCarrinhoItemNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "item não encontrado no carrinho")
		default:
			slog.Error("falha ao remover item do carrinho", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao remover item do carrinho")
		}
	}
}
