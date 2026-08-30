package handlers

import (
	"database/sql"
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

		usuarios, err := services.ListarUsuarios(db, usuario.Papel)
		if err != nil {
			slog.Error("falha ao listar usuários", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar usuários")
			return
		}

		escreverJSON(w, http.StatusOK, map[string]any{"usuarios": usuarios})
	}
}
