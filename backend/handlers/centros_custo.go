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

// Handlers de Centros de Custo — Story 12.3 (spec-12-3, FR-51, AD-28). Fronteira HTTP
// pura sobre services/centros_custo.go; o gate `adm`+ da escrita é decidido
// inteiramente por RequireRole no roteamento (main.go).
//
//   - POST /api/centros-custo {"nome"} -> 201 {"centroCusto":{"id","nome"}}
//   - GET  /api/centros-custo          -> 200 {"centrosCusto":[...]} (só RequireAuth: o
//     almoxarife precisa listar para escolher o Centro de Custo do Pedido)
//
// 400 VALIDATION_ERROR (nome inválido), 409 CONFLICT (nome duplicado na
// Empresa). `empresa_id` nunca vem de body/query: é o da Empresa do contexto.

type centroCustoRequest struct {
	Nome string `json:"nome"`
}

// CriarCentroCustoHandler expõe POST /api/centros-custo.
func CriarCentroCustoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("CriarCentroCustoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req centroCustoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		centro, err := services.CriarCentroCusto(db, empresa.ID, req.Nome)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusCreated, map[string]any{"centroCusto": centro})
		case errors.Is(err, services.ErrCentroCustoValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", services.ErrCentroCustoValidacao.Error())
		case errors.Is(err, services.ErrNomeCentroCustoDuplicado):
			escreverErro(w, http.StatusConflict, "CONFLICT", services.ErrNomeCentroCustoDuplicado.Error())
		default:
			slog.Error("falha ao criar centro de custo", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao criar centro de custo")
		}
	}
}

// ListarCentrosCustoHandler expõe GET /api/centros-custo.
func ListarCentrosCustoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("ListarCentrosCustoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		centros, err := services.ListarCentrosCusto(db, empresa.ID)
		if err != nil {
			slog.Error("falha ao listar centros de custo", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar centros de custo")
			return
		}
		escreverJSON(w, http.StatusOK, map[string]any{"centrosCusto": centros})
	}
}
