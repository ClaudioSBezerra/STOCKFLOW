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

// Handlers de escrita de Categorias — Story 10.5 (spec-10-5). Fronteira HTTP
// pura sobre services/categorias.go; o gate `adm`+ é decidido inteiramente
// por RequireRole no roteamento (main.go). GET /api/categorias permanece em
// ListarCategoriasHandler (produtos.go), só RequireAuth.
//
//   - POST   /api/categorias       {"codigo","nome"} -> 201 {"categoria":{...}}
//   - PUT    /api/categorias/{id}  {"codigo","nome"} -> 200 {"categoria":{...}}
//   - DELETE /api/categorias/{id}                    -> 204 sem corpo
//
// 400 VALIDATION_ERROR, 404 NOT_FOUND (id alheio/inexistente/malformado),
// 409 CONFLICT (duplicado, ou categoria em uso na exclusão).

// categoriaRequest é o corpo de POST/PUT. Campos ausentes decodificam como ""
// e são rejeitados pela service como ErrCategoriaValidacao.
type categoriaRequest struct {
	Codigo string `json:"codigo"`
	Nome   string `json:"nome"`
}

// mapearErroCategoria escreve a resposta de erro para `err`.
func mapearErroCategoria(w http.ResponseWriter, err error, acao string) {
	var duplicada *services.ErrCategoriaDuplicada
	var emUso *services.ErroCategoriaEmUso
	switch {
	case errors.Is(err, services.ErrCategoriaValidacao):
		escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", services.ErrCategoriaValidacao.Error())
	case errors.Is(err, services.ErrCategoriaNaoEncontrada):
		escreverErro(w, http.StatusNotFound, "NOT_FOUND", "categoria não encontrada")
	case errors.As(err, &duplicada):
		escreverErro(w, http.StatusConflict, "CONFLICT", duplicada.Error())
	case errors.As(err, &emUso):
		escreverErro(w, http.StatusConflict, "CONFLICT", emUso.Error())
	default:
		slog.Error("falha ao "+acao+" categoria", "error", err)
		escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao "+acao+" categoria")
	}
}

// contextoCategoria valida a sessão e resolve a Empresa da requisição.
func contextoCategoria(w http.ResponseWriter, r *http.Request, nome string) (string, bool) {
	if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
		slog.Error(nome + " chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
		escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
		return "", false
	}
	empresa, ok := empresaDaRequisicao(w, r)
	if !ok {
		return "", false
	}
	return empresa.ID, true
}

// CriarCategoriaHandler expõe POST /api/categorias.
func CriarCategoriaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		empresaID, ok := contextoCategoria(w, r, "CriarCategoriaHandler")
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req categoriaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}
		c, err := services.CriarCategoria(db, empresaID, req.Codigo, req.Nome)
		if err != nil {
			mapearErroCategoria(w, err, "criar")
			return
		}
		escreverJSON(w, http.StatusCreated, map[string]any{"categoria": c})
	}
}

// AtualizarCategoriaHandler expõe PUT /api/categorias/{id}.
func AtualizarCategoriaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		empresaID, ok := contextoCategoria(w, r, "AtualizarCategoriaHandler")
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req categoriaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}
		c, err := services.AtualizarCategoria(db, empresaID, r.PathValue("id"), req.Codigo, req.Nome)
		if err != nil {
			mapearErroCategoria(w, err, "atualizar")
			return
		}
		escreverJSON(w, http.StatusOK, map[string]any{"categoria": c})
	}
}

// ExcluirCategoriaHandler expõe DELETE /api/categorias/{id}: 204 sem corpo.
func ExcluirCategoriaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		empresaID, ok := contextoCategoria(w, r, "ExcluirCategoriaHandler")
		if !ok {
			return
		}
		if err := services.ExcluirCategoria(db, empresaID, r.PathValue("id")); err != nil {
			mapearErroCategoria(w, err, "excluir")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
