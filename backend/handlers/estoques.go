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

// Handlers de Estoques — criar e listar locais (Story 2.1, spec-2-1).
// Fronteira HTTP pura: decodifica/serializa JSON, mapeia os erros de
// services/estoques.go para o envelope de erro fixo (AD-14) e nunca contém
// regra de negócio própria.
//
// Registro em newMux (main.go):
//   - POST /api/estoques -> RequireAuth -> RequireRole(almoxarife); corpo
//     {"nome": string}. O 403 para papel abaixo de `almoxarife` é decidido
//     inteiramente por RequireRole — este handler só executa quando o papel
//     já passou nesse gate.
//   - GET /api/estoques -> RequireAuth apenas: qualquer conta autenticada
//     lista os Estoques (AC4).

// criarEstoqueRequest é o corpo aceito por POST /api/estoques. `nome`
// ausente decodifica como "" e é rejeitado por services.CriarEstoque como
// ErrEstoqueValidacao -> 400, junto com nome só de espaços; um JSON inválido
// ou um corpo acima de authRequestMaxBytes falham no Decode -> 400.
type criarEstoqueRequest struct {
	Nome string `json:"nome"`
}

// CriarEstoqueHandler expõe POST /api/estoques: cadastra um novo local de
// estoque. `201 {"estoque":{"id","nome"}}` no sucesso; `400 VALIDATION_ERROR`
// para payload/nome inválido; `409 CONFLICT` quando o nome normalizado já
// existe (colisão do índice único, backstop de corrida).
func CriarEstoqueHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("CriarEstoqueHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req criarEstoqueRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		estoque, err := services.CriarEstoque(db, req.Nome)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusCreated, map[string]any{"estoque": estoque})
		case errors.Is(err, services.ErrEstoqueValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "nome de estoque inválido")
		case errors.Is(err, services.ErrNomeEstoqueDuplicado):
			escreverErro(w, http.StatusConflict, "CONFLICT", "já existe um estoque com esse nome")
		default:
			slog.Error("falha ao criar estoque", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao criar estoque")
		}
	}
}

// ListarEstoquesHandler expõe GET /api/estoques: devolve
// `200 {"estoques":[{"id","nome"}, ...]}` ordenado por nome normalizado, para
// qualquer conta autenticada. Erro de banco -> 500 INTERNAL_ERROR + slog.
func ListarEstoquesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("ListarEstoquesHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		estoques, err := services.ListarEstoques(db)
		if err != nil {
			slog.Error("falha ao listar estoques", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar estoques")
			return
		}

		escreverJSON(w, http.StatusOK, map[string]any{"estoques": estoques})
	}
}
