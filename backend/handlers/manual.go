package handlers

import (
	"database/sql"
	"log/slog"
	"net/http"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// Manual Online (feedback Ferreira Costa, direção, 2026-10-22). Duas rotas,
// ambas atrás só de RequireAuth (sem RequireRole: o recorte por papel é feito
// dentro do service, a mesma forma de ListarUsuarios):
//
//   - GET /e/{slug}/api/manual        -> seções visíveis para o papel de
//     quem chamou (services.SecoesManualParaPapel). Um `adm` recebe o manual
//     completo.
//   - GET /e/{slug}/api/manual/pdf    -> SEMPRE o manual COMPLETO em PDF,
//     mas só para `adm` (RequireRole aqui sim, na rota — ver main.go): é a
//     ação de "gerar o PDF para encaminhar aos usuários", não uma leitura
//     pessoal.

// ManualHandler expõe GET /api/manual.
func ManualHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("ManualHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		secoes := services.SecoesManualParaPapel(usuario.Papel)
		escreverJSON(w, http.StatusOK, map[string]any{"secoes": secoes})
	}
}

// BaixarManualPDFHandler expõe GET /api/manual/pdf, atrás de
// RequireRole(adm) em newMux — sempre o manual completo (services.GerarManualPDF
// não recebe papel nenhum: só existe um jeito de chamá-la).
func BaixarManualPDFHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pdf, err := services.GerarManualPDF()
		if err != nil {
			slog.Error("falha ao gerar PDF do manual", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao gerar o manual em PDF")
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="manual-stockflow.pdf"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(pdf)
	}
}
