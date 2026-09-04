// Package handlers, arquivo privacidade.go: fronteira HTTP da exportação dos
// próprios dados pessoais — Story 8.1 (Epic 8, Privacidade/LGPD), spec-8-1.
package handlers

import (
	"database/sql"
	"log/slog"
	"net/http"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// ExportarDadosUsuarioHandler expõe GET /api/usuarios/me/exportar-dados
// (Story 8.1, spec-8-1), registrado em newMux atrás SÓ de RequireAuth — SEM
// RequireRole: qualquer papel autenticado exporta os PRÓPRIOS dados (LGPD).
// `usuarioId` do export vem sempre de middleware.UsuarioDaSessao — nunca de
// path/query/body (Always, spec-8-1); esta rota nunca aceita um `usuarioId`
// alheio. Molde de BaixarReciboPedidoHandler (handlers/pedidos.go).
//
// Sucesso -> `200`, `Content-Type: application/json` (via escreverJSON,
// handlers/auth.go), `Content-Disposition: attachment;
// filename="meus-dados.json"`, corpo = services.DadosPessoaisExportados
// (nome/email da sessão + logAcesso/movimentacoes/pedidos do próprio
// usuário, sempre arrays, nunca `null`). Erro de qualquer uma das três
// fontes (services.ExportarDadosUsuario) -> `500 INTERNAL_ERROR`, nenhum
// arquivo parcial devolvido.
func ExportarDadosUsuarioHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("ExportarDadosUsuarioHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		dados, err := services.ExportarDadosUsuario(db, usuario.ID, usuario.Nome, usuario.Email)
		if err != nil {
			slog.Error("falha ao exportar dados pessoais do usuário", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao exportar dados pessoais")
			return
		}

		// Content-Disposition precisa ser setado ANTES de escreverJSON — que
		// já chama w.WriteHeader (nenhum header pode ser adicionado depois).
		w.Header().Set("Content-Disposition", `attachment; filename="meus-dados.json"`)
		escreverJSON(w, http.StatusOK, dados)
	}
}
