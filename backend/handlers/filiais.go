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
// pura sobre services/filiais.go; o gate `gestor`+ da escrita é decidido
// inteiramente por RequireRole no roteamento (main.go) —
// spec-filial-dados-fiscais (antes `adm`+).
//
//   - POST /api/filiais {"nome","cnpj","inscricaoEstadual","endereco":{...}}
//     -> 201 {"filial":{...}}
//   - PUT  /api/filiais/{id} (mesmo corpo, PARCIAL: campo ausente mantém o
//     gravado, "" limpa) -> 200 {"filial":{...}}
//   - GET  /api/filiais          -> 200 {"filiais":[...]} (só RequireAuth: o
//     almoxarife precisa listar para escolher a Filial do Estoque)
//
// 400 VALIDATION_ERROR (nome inválido, CNPJ inválido, endereço incompleto...),
// 404 NOT_FOUND (PUT de Filial inexistente/de outra Empresa), 409 CONFLICT
// (nome ou CNPJ duplicado na Empresa). `empresa_id` nunca vem de body/query:
// é o da Empresa do contexto.

// escreverErroFilial mapeia os erros de escrita de Filial para HTTP.
func escreverErroFilial(w http.ResponseWriter, err error, operacao string) {
	var erroDados *services.ErroDadosFilial
	switch {
	case errors.Is(err, services.ErrFilialValidacao):
		escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", services.ErrFilialValidacao.Error())
	case errors.As(err, &erroDados):
		escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroDados.Mensagem)
	case errors.Is(err, services.ErrNomeFilialDuplicado), errors.Is(err, services.ErrCNPJFilialDuplicado):
		escreverErro(w, http.StatusConflict, "CONFLICT", err.Error())
	case errors.Is(err, services.ErrFilialNaoEncontrada):
		escreverErro(w, http.StatusNotFound, "NOT_FOUND", services.ErrFilialNaoEncontrada.Error())
	default:
		slog.Error("falha ao "+operacao+" filial", "error", err)
		escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao "+operacao+" filial")
	}
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
		var req services.DadosFilial
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		filial, err := services.CriarFilial(db, empresa.ID, req)
		if err != nil {
			escreverErroFilial(w, err, "criar")
			return
		}
		escreverJSON(w, http.StatusCreated, map[string]any{"filial": filial})
	}
}

// AtualizarFilialHandler expõe PUT /api/filiais/{id}: regrava nome e dados
// fiscais (substituição completa — campo vazio vira NULL).
func AtualizarFilialHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("AtualizarFilialHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		// PUT parcial: campo ausente (ou null) mantém o valor gravado; ""
		// limpa. O resultado da mescla é validado por inteiro no service.
		var req services.AtualizacaoFilial
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		filial, err := services.AtualizarFilial(db, empresa.ID, r.PathValue("id"), req)
		if err != nil {
			escreverErroFilial(w, err, "atualizar")
			return
		}
		escreverJSON(w, http.StatusOK, map[string]any{"filial": filial})
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
