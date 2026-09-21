package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"stockflow/backend/services"
)

// Handlers de escrita de Templates de Nomenclatura — Story 10.6 (spec-10-6).
// Fronteira HTTP pura sobre services/nomenclatura_crud.go; o gate `adm`+ é
// decidido inteiramente por RequireRole no roteamento (main.go).
// GET /api/nomenclatura-templates permanece em
// ListarNomenclaturaTemplatesHandler (produtos.go), só RequireAuth.
//
//   - POST   /api/nomenclatura-templates       {"subtipo","template"} -> 201 {"template":{...}}
//   - PUT    /api/nomenclatura-templates/{id}  {"subtipo","template"} -> 200 {"template":{...}}
//   - DELETE /api/nomenclatura-templates/{id}                         -> 204 sem corpo
//
// 400 VALIDATION_ERROR, 404 NOT_FOUND (id alheio/inexistente/malformado),
// 409 CONFLICT (subtipo duplicado, template em uso ou fallback obrigatório).

// templateNomenclaturaRequest é o corpo de POST/PUT. Campos ausentes
// decodificam como "" e são rejeitados pela service.
type templateNomenclaturaRequest struct {
	Subtipo  string `json:"subtipo"`
	Template string `json:"template"`
}

// mapearErroTemplateNomenclatura escreve a resposta de erro para `err`.
func mapearErroTemplateNomenclatura(w http.ResponseWriter, err error, acao string) {
	var emUso *services.ErroTemplateEmUso
	switch {
	case errors.Is(err, services.ErrTemplateValidacao):
		escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", services.ErrTemplateValidacao.Error())
	case errors.Is(err, services.ErrTemplateNaoEncontrado):
		escreverErro(w, http.StatusNotFound, "NOT_FOUND", "template não encontrado")
	case errors.Is(err, services.ErrTemplateDuplicado):
		escreverErro(w, http.StatusConflict, "CONFLICT", services.ErrTemplateDuplicado.Error())
	case errors.Is(err, services.ErrTemplateFallbackObrigatorio):
		escreverErro(w, http.StatusConflict, "CONFLICT", services.ErrTemplateFallbackObrigatorio.Error())
	case errors.As(err, &emUso):
		escreverErro(w, http.StatusConflict, "CONFLICT", emUso.Error())
	default:
		slog.Error("falha ao "+acao+" template de nomenclatura", "error", err)
		escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao "+acao+" template de nomenclatura")
	}
}

// CriarTemplateNomenclaturaHandler expõe POST /api/nomenclatura-templates.
func CriarTemplateNomenclaturaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		empresaID, ok := contextoCategoria(w, r, "CriarTemplateNomenclaturaHandler")
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req templateNomenclaturaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}
		t, err := services.CriarNomenclaturaTemplate(db, empresaID, req.Subtipo, req.Template)
		if err != nil {
			mapearErroTemplateNomenclatura(w, err, "criar")
			return
		}
		escreverJSON(w, http.StatusCreated, map[string]any{"template": t})
	}
}

// AtualizarTemplateNomenclaturaHandler expõe PUT /api/nomenclatura-templates/{id}.
func AtualizarTemplateNomenclaturaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		empresaID, ok := contextoCategoria(w, r, "AtualizarTemplateNomenclaturaHandler")
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req templateNomenclaturaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}
		t, err := services.AtualizarNomenclaturaTemplate(db, empresaID, r.PathValue("id"), req.Subtipo, req.Template)
		if err != nil {
			mapearErroTemplateNomenclatura(w, err, "atualizar")
			return
		}
		escreverJSON(w, http.StatusOK, map[string]any{"template": t})
	}
}

// ExcluirTemplateNomenclaturaHandler expõe DELETE /api/nomenclatura-templates/{id}: 204 sem corpo.
func ExcluirTemplateNomenclaturaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		empresaID, ok := contextoCategoria(w, r, "ExcluirTemplateNomenclaturaHandler")
		if !ok {
			return
		}
		if err := services.ExcluirNomenclaturaTemplate(db, empresaID, r.PathValue("id")); err != nil {
			mapearErroTemplateNomenclatura(w, err, "excluir")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
