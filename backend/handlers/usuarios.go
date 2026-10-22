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

// ListarUsuariosHandler expõe GET /api/usuarios (Story 1.5), sempre
// registrado atrás de `RequireAuth(...)(RequireRole("gestor")(...))` em
// newMux. O 403 para papel abaixo de `gestor` é decidido inteiramente por
// RequireRole — este handler só executa quando o papel já passou nesse gate.
//
// O filtro de escopo (adm vê tudo, gestor vê só usuario/almoxarife) é
// aplicado em services.ListarUsuarios a partir do papel JÁ resolvido pelo
// middleware e extraído do contexto aqui — nunca reconsultando `usuarios`
// (AD-8 forma 3). Guard de contexto ausente -> 500, igual a MeHandler.
func ListarUsuariosHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("ListarUsuariosHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		usuarios, err := services.ListarUsuarios(db, empresa.ID, usuario.Papel)
		if err != nil {
			slog.Error("falha ao listar usuários", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar usuários")
			return
		}

		escreverJSON(w, http.StatusOK, map[string]any{"usuarios": usuarios})
	}
}

// criarUsuarioRequest é o corpo aceito por POST /api/usuarios — cadastro
// direto pelo `adm`, sem convite (feedback Ferreira Costa, direção,
// 2026-10-21). Diferente do convite (só e-mail, papel sempre `usuario`),
// aqui o `adm` escolhe nome/e-mail/senha/papel/exigência individual de MFA
// na hora — a conta nasce pronta para logar, sem link nem e-mail.
type criarUsuarioRequest struct {
	Nome                     string `json:"nome"`
	Email                    string `json:"email"`
	Senha                    string `json:"senha"`
	Papel                    string `json:"papel"`
	MFAObrigatorioIndividual bool   `json:"mfaObrigatorioIndividual"`
}

// CriarUsuarioHandler expõe POST /api/usuarios, sempre registrado atrás de
// `RequireAuth(...)(RequireRole("adm")(...))` em newMux — só `adm` cadastra
// direto, em qualquer papel (inclusive outro `adm`). O gate de papel é do
// middleware, nunca deste handler.
func CriarUsuarioHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req criarUsuarioRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		usuario, err := services.CriarUsuarioDireto(
			db, empresa.ID, req.Nome, req.Email, req.Senha, req.Papel, req.MFAObrigatorioIndividual,
		)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusCreated, map[string]any{"usuario": usuario})
		case errors.Is(err, services.ErrCadastroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "nome, e-mail e senha são obrigatórios.")
		case errors.Is(err, services.ErrPapelInvalido):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "papel inválido.")
		case errors.Is(err, services.ErrSenhaFraca):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "a senha precisa ter 8+ caracteres com letra e dígito.")
		case errors.Is(err, services.ErrEmailDuplicado):
			escreverErro(w, http.StatusConflict, "CONFLICT", "este e-mail já tem conta.")
		default:
			slog.Error("falha ao criar usuário direto", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao criar usuário")
		}
	}
}
