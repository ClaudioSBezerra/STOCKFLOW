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

// Handlers dos convites nominais de acesso a uma Empresa — Story 9.3
// (Epic 9, FR-42; AD-22), spec-9-3. Fronteira HTTP pura: decodifica/serializa
// JSON, mapeia os erros de `services/convites.go` para o envelope de erro
// fixo (AD-14) e nunca contém regra de negócio.
//
// Registro em newMux (main.go), as três atrás de
// RequireAuth + RequireRole(gestor) e, por fora de tudo, RequireEmpresa:
//   - POST /e/{slug}/api/convites                 -> corpo {"email": "..."}
//   - GET  /e/{slug}/api/convites                 -> sem corpo
//   - POST /e/{slug}/api/convites/{id}/revogacao  -> sem corpo
//
// O gate de papel é do middleware, nunca destes handlers: eles só executam
// quando o papel já passou. A Empresa vem SEMPRE do contexto
// (empresaDaRequisicao), nunca de corpo/query/header.

// conviteRequest é o corpo aceito por POST /api/convites. Só o e-mail: o
// convite não carrega papel (a conta nasce sempre `usuario`), nem prazo (a
// validade é a constante conviteExpiracao do service), nem Empresa (ela vem
// do slug da rota).
type conviteRequest struct {
	Email string `json:"email"`
}

// EmitirConviteHandler expõe POST /api/convites: grava o convite nominal e
// devolve 201 com a projeção do convite JÁ com o `link` para o `gestor`
// compartilhar com a pessoa convidada. O convite nunca é enviado por e-mail
// (ver Design Notes da spec-9-3): quem compartilha é o emissor.
func EmitirConviteHandler(db *sql.DB, emailCfg services.EmailConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("EmitirConviteHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req conviteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		convite, err := services.EmitirConvite(db, emailCfg, empresa.ID, empresa.Slug, usuario.ID, req.Email)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusCreated, map[string]any{"convite": convite})
		case errors.Is(err, services.ErrConviteValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "Informe um e-mail válido para convidar.")
		case errors.Is(err, services.ErrConviteEmailJaCadastrado):
			escreverErro(w, http.StatusConflict, "CONFLICT", "Este e-mail já tem conta nesta empresa.")
		default:
			slog.Error("falha ao emitir convite", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao emitir convite")
		}
	}
}

// ListarConvitesHandler expõe GET /api/convites: todos os convites da Empresa
// do slug, com a `situacao` derivada na leitura. O `link` só acompanha os
// pendentes — decisão do service, não daqui.
func ListarConvitesHandler(db *sql.DB, emailCfg services.EmailConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		convites, err := services.ListarConvites(db, emailCfg, empresa.ID, empresa.Slug)
		if err != nil {
			slog.Error("falha ao listar convites", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar convites")
			return
		}

		escreverJSON(w, http.StatusOK, map[string]any{"convites": convites})
	}
}

// RevogarConviteHandler expõe POST /api/convites/{id}/revogacao: cancela um
// convite ainda não resgatado. Sem corpo — o id vem do caminho e a Empresa do
// contexto. Convite já resgatado -> 409 CONFLICT; id inexistente, malformado
// ou de outra Empresa -> 404 NOT_FOUND (indistinguíveis de propósito).
func RevogarConviteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		err := services.RevogarConvite(db, empresa.ID, r.PathValue("id"))
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]string{"mensagem": "Convite cancelado."})
		case errors.Is(err, services.ErrConviteNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "convite não encontrado")
		case errors.Is(err, services.ErrConviteJaUsado):
			escreverErro(w, http.StatusConflict, "CONFLICT", "Este convite já foi utilizado e não pode ser cancelado.")
		default:
			slog.Error("falha ao revogar convite", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao revogar convite")
		}
	}
}
