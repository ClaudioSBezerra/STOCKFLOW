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

// Handlers da gestão de contas — desativação e rebaixamento (Story 1.8,
// spec-1-8). Fronteira HTTP pura: decodifica/serializa JSON, mapeia os erros
// de `services/gestao_usuarios.go` para o envelope de erro fixo (AD-14) e
// nunca contém regra de negócio.
//
// Registro em newMux (main.go), ambas atrás de
// RequireAuth + RequireRole(gestor):
//   - POST /api/usuarios/{id}/desativacao  -> corpo {"ativo": bool}
//   - POST /api/usuarios/{id}/rebaixamento -> sem corpo

// ativacaoRequest é o corpo aceito por POST /api/usuarios/{id}/desativacao.
// `Ativo` é um ponteiro DE PROPÓSITO (molde de decisaoRequest, promocao.go):
// um corpo `{}`, `{"ativo":null}` ou com a chave errada decodifica sem erro e,
// com um `bool` puro, viraria silenciosamente `ativo=false` — uma desativação
// involuntária. Nil -> 400 VALIDATION_ERROR.
type ativacaoRequest struct {
	Ativo *bool `json:"ativo"`
}

// DesativarUsuarioHandler expõe POST /api/usuarios/{id}/desativacao: desativa
// (e revoga as sessões) ou reativa a conta alvo. O corpo `{"ativo": bool}` é
// lido sob http.MaxBytesReader.
func DesativarUsuarioHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("DesativarUsuarioHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req ativacaoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}
		if req.Ativo == nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		u, err := services.AlterarAtivacaoUsuario(db, r.PathValue("id"), usuario.ID, usuario.Papel, *req.Ativo)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]any{"usuario": u})
		case errors.Is(err, services.ErrGestaoForaDeEscopo):
			escreverErro(w, http.StatusForbidden, "FORBIDDEN", "papel insuficiente para agir sobre esta conta")
		case errors.Is(err, services.ErrContaNaoEncontrada):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "conta não encontrada")
		case errors.Is(err, services.ErrEstadoContaMudou):
			escreverErro(w, http.StatusConflict, "CONFLICT", "o estado da conta mudou; recarregue a lista")
		default:
			slog.Error("falha ao alterar ativação de conta", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao alterar ativação de conta")
		}
	}
}

// RebaixarUsuarioHandler expõe POST /api/usuarios/{id}/rebaixamento: rebaixa a
// conta alvo um degrau na hierarquia. Sem corpo — o papel-alvo é sempre
// derivado no servidor.
func RebaixarUsuarioHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("RebaixarUsuarioHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		u, err := services.RebaixarUsuario(db, r.PathValue("id"), usuario.ID, usuario.Papel)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]any{"usuario": u})
		case errors.Is(err, services.ErrGestaoForaDeEscopo):
			escreverErro(w, http.StatusForbidden, "FORBIDDEN", "papel insuficiente para agir sobre esta conta")
		case errors.Is(err, services.ErrContaNaoEncontrada):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "conta não encontrada")
		case errors.Is(err, services.ErrRebaixamentoIndisponivel):
			escreverErro(w, http.StatusConflict, "CONFLICT", "não há papel abaixo para rebaixar esta conta")
		case errors.Is(err, services.ErrEstadoContaMudou):
			escreverErro(w, http.StatusConflict, "CONFLICT", "o estado da conta mudou; recarregue a lista")
		default:
			slog.Error("falha ao rebaixar conta", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao rebaixar conta")
		}
	}
}
