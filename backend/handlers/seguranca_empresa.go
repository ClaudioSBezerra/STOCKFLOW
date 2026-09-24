package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// exigenciaMFARequestMaxBytes limita o corpo de PUT /api/seguranca/mfa-empresa.
const exigenciaMFARequestMaxBytes = 4 * 1024

// Story 14.3 (FR-53, AD-35): o `adm` lê e altera a exigência de MFA da
// própria Empresa e consulta a trilha `auditoria_seguranca`. As três rotas
// ficam SEMPRE em newMux atrás de RequireAuth -> RequireRole(services.PapelAdm):
// 401, 403 FORBIDDEN e 403 MFA_SETUP_REQUIRED (inclusive para o próprio `adm`)
// são decididos pelos middlewares, nunca aqui.

// ObterExigenciaMFAHandler expõe GET /api/seguranca/mfa-empresa:
// 200 {"mfaObrigatorio": bool, "contasSemMfa": int}. O flag vem da Empresa
// resolvida pelo middleware nesta requisição (sem cache).
func ObterExigenciaMFAHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("ObterExigenciaMFAHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		n, err := services.ContarContasSemMFA(db, empresa.ID)
		if err != nil {
			slog.Error("falha ao contar contas sem MFA", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao ler a exigência de MFA")
			return
		}
		escreverJSON(w, http.StatusOK, map[string]any{"mfaObrigatorio": empresa.MFAObrigatorio, "contasSemMfa": n})
	}
}

type alterarExigenciaMFARequest struct {
	MFAObrigatorio *bool `json:"mfaObrigatorio"`
}

// AlterarExigenciaMFAHandler expõe PUT /api/seguranca/mfa-empresa, corpo
// {"mfaObrigatorio": bool} -> 200 {"mfaObrigatorio": bool, "alterado": bool}.
// Campo ausente, null ou não-booleano -> 400 VALIDATION_ERROR. Reenviar o
// valor atual é idempotente (alterado:false, sem auditoria). A confirmação
// "N contas ficarão sem acesso" é só de UI — o servidor não a exige.
func AlterarExigenciaMFAHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("AlterarExigenciaMFAHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, exigenciaMFARequestMaxBytes)
		var req alterarExigenciaMFARequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MFAObrigatorio == nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "informe 'mfaObrigatorio' como true ou false")
			return
		}
		novo := *req.MFAObrigatorio

		alterado, err := services.AlterarExigenciaMFA(db, empresa.ID, usuario.ID, novo)
		if err != nil {
			slog.Error("falha ao alterar exigência de MFA", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao alterar a exigência de MFA")
			return
		}
		escreverJSON(w, http.StatusOK, map[string]any{"mfaObrigatorio": novo, "alterado": alterado})
	}
}

// ListarAuditoriaSegurancaHandler expõe GET /api/seguranca/auditoria:
// 200 {"eventos": [...]}, só da Empresa da requisição, mais recentes primeiro.
// Não há rota de escrita sobre a auditoria (append-only).
func ListarAuditoriaSegurancaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("ListarAuditoriaSegurancaHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		eventos, err := services.ListarAuditoriaSeguranca(db, empresa.ID)
		if err != nil {
			slog.Error("falha ao listar auditoria de segurança", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar auditoria de segurança")
			return
		}
		escreverJSON(w, http.StatusOK, map[string]any{"eventos": eventos})
	}
}
