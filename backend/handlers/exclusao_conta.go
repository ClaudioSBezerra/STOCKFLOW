// Package handlers, arquivo exclusao_conta.go: fronteira HTTP da exclusão e
// anonimização de dados pessoais por Adm — Story 8.2 (Epic 8,
// Privacidade/LGPD), spec-8-2. Decodifica/serializa JSON, mapeia os erros de
// services/exclusao_conta.go para o envelope de erro fixo (AD-14) e nunca
// contém regra de negócio.
//
// Registro em newMux (main.go):
//   - POST /api/usuarios/me/solicitacao-exclusao        -> RequireAuth (qualquer conta)
//   - GET  /api/solicitacoes-exclusao                   -> RequireAuth + RequireRole(adm)
//   - POST /api/solicitacoes-exclusao/{id}/processamento -> RequireAuth + RequireRole(adm)
package handlers

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// solicitacaoExclusaoResumo é a resposta de
// POST /api/usuarios/me/solicitacao-exclusao (o próprio solicitante).
type solicitacaoExclusaoResumo struct {
	ID       string    `json:"id"`
	Status   string    `json:"status"`
	CriadoEm time.Time `json:"criadoEm"`
}

// solicitacaoExclusaoPendenteResposta é um item da fila do `adm`
// (GET /api/solicitacoes-exclusao) e o corpo do processamento bem-sucedido.
type solicitacaoExclusaoPendenteResposta struct {
	ID       string    `json:"id"`
	Nome     string    `json:"nome"`
	Email    string    `json:"email"`
	Papel    string    `json:"papel"`
	CriadoEm time.Time `json:"criadoEm"`
}

// SolicitarExclusaoContaHandler expõe POST /api/usuarios/me/solicitacao-exclusao,
// registrado atrás SÓ de RequireAuth — SEM RequireRole: qualquer papel
// autenticado REGISTRA a solicitação de exclusão da PRÓPRIA conta (nunca
// processa). `usuarioId` vem sempre de middleware.UsuarioDaSessao — nunca de
// path/query/body (molde de ExportarDadosUsuarioHandler). Sem corpo.
func SolicitarExclusaoContaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("SolicitarExclusaoContaHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		s, err := services.SolicitarExclusaoConta(db, usuario.ID)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusCreated, solicitacaoExclusaoResumo{
				ID:       s.ID,
				Status:   s.Status,
				CriadoEm: s.CriadoEm,
			})
		case errors.Is(err, services.ErrExclusaoPendenteExiste):
			escreverErro(w, http.StatusConflict, "CONFLICT", "já existe uma solicitação de exclusão pendente para a sua conta")
		default:
			slog.Error("falha ao solicitar exclusão de conta", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao solicitar exclusão de conta")
		}
	}
}

// ListarSolicitacoesExclusaoHandler expõe GET /api/solicitacoes-exclusao,
// sempre atrás de RequireRole(adm) — mesmo gate de GET /api/logs-acesso.
// Molde de ListarUsuariosHandler: corpo `{"solicitacoes": [...]}`, array
// vazio nunca `null`.
func ListarSolicitacoesExclusaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pendentes, err := services.ListarSolicitacoesExclusao(db)
		if err != nil {
			slog.Error("falha ao listar solicitações de exclusão", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar solicitações de exclusão")
			return
		}

		itens := make([]solicitacaoExclusaoPendenteResposta, 0, len(pendentes))
		for _, p := range pendentes {
			itens = append(itens, solicitacaoExclusaoPendenteResposta{
				ID:       p.ID,
				Nome:     p.SolicitanteNome,
				Email:    p.SolicitanteEmail,
				Papel:    p.SolicitantePapel,
				CriadoEm: p.CriadoEm,
			})
		}
		escreverJSON(w, http.StatusOK, map[string]any{"solicitacoes": itens})
	}
}

// ProcessarExclusaoContaHandler expõe POST
// /api/solicitacoes-exclusao/{id}/processamento, sempre atrás de
// RequireRole(adm). `{id}` é o id da SOLICITAÇÃO — o alvo da anonimização é
// resolvido no service a partir de `solicitante_id`, nunca do request. Sem
// corpo. Molde de DecidirPromocaoHandler/RebaixarUsuarioHandler.
func ProcessarExclusaoContaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("ProcessarExclusaoContaHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		p, err := services.ProcessarExclusaoConta(db, r.PathValue("id"), usuario.ID)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, solicitacaoExclusaoPendenteResposta{
				ID:       p.ID,
				Nome:     p.SolicitanteNome,
				Email:    p.SolicitanteEmail,
				Papel:    p.SolicitantePapel,
				CriadoEm: p.CriadoEm,
			})
		case errors.Is(err, services.ErrSolicitacaoExclusaoNaoEncontrada):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "solicitação de exclusão não encontrada")
		case errors.Is(err, services.ErrSolicitacaoExclusaoNaoPendente):
			escreverErro(w, http.StatusConflict, "CONFLICT", "esta solicitação de exclusão não está mais pendente")
		case errors.Is(err, services.ErrUltimoAdmAtivo):
			escreverErro(w, http.StatusConflict, "CONFLICT", "ao menos um administrador ativo deve sempre existir")
		default:
			slog.Error("falha ao processar exclusão de conta", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar exclusão de conta")
		}
	}
}
