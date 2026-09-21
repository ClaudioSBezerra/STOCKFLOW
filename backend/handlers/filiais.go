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

// Handlers de Filiais — Story 12.1 (spec-12-1, FR-51, AD-27). Fronteira HTTP
// pura sobre services/filiais.go; o gate `adm`+ da escrita é decidido
// inteiramente por RequireRole no roteamento (main.go).
//
//   - POST /api/filiais {"nome"} -> 201 {"filial":{"id","nome"}}
//   - GET  /api/filiais          -> 200 {"filiais":[...]} (só RequireAuth: o
//     almoxarife precisa listar para escolher a Filial do Estoque)
//
// 400 VALIDATION_ERROR (nome inválido), 409 CONFLICT (nome duplicado na
// Empresa). `empresa_id` nunca vem de body/query: é o da Empresa do contexto.

type filialRequest struct {
	Nome string `json:"nome"`
}

// CriarFilialHandler expõe POST /api/filiais.
func CriarFilialHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("CriarFilialHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req filialRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		filial, err := services.CriarFilial(db, empresa.ID, req.Nome)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusCreated, map[string]any{"filial": filial})
		case errors.Is(err, services.ErrFilialValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", services.ErrFilialValidacao.Error())
		case errors.Is(err, services.ErrNomeFilialDuplicado):
			escreverErro(w, http.StatusConflict, "CONFLICT", services.ErrNomeFilialDuplicado.Error())
		default:
			slog.Error("falha ao criar filial", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao criar filial")
		}
	}
}

// ListarFiliaisHandler expõe GET /api/filiais.
func ListarFiliaisHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("ListarFiliaisHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		filiais, err := services.ListarFiliais(db, empresa.ID)
		if err != nil {
			slog.Error("falha ao listar filiais", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar filiais")
			return
		}
		escreverJSON(w, http.StatusOK, map[string]any{"filiais": filiais})
	}
}
