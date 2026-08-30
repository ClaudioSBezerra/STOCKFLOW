package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// Handlers da solicitação de promoção de papel (Story 1.7). Fronteira HTTP
// pura: decodifica/serializa JSON, mapeia os erros de `services/promocao.go`
// para o envelope de erro fixo (AD-14) e nunca contém regra de negócio.
//
// Registro em newMux (main.go):
//   - POST /api/promocoes            -> RequireAuth (qualquer conta autenticada)
//   - GET  /api/promocoes/minha      -> RequireAuth
//   - GET  /api/promocoes            -> RequireAuth + RequireRole(gestor)
//   - POST /api/promocoes/{id}/decisao -> RequireAuth + RequireRole(gestor)

// solicitacaoResumo é o objeto devolvido ao próprio solicitante em
// POST /api/promocoes e GET /api/promocoes/minha. `DecididoEm` é um ponteiro:
// serializa como `null` enquanto a solicitação está `pendente`.
type solicitacaoResumo struct {
	ID         string     `json:"id"`
	PapelAlvo  string     `json:"papel_alvo"`
	Status     string     `json:"status"`
	CriadoEm   time.Time  `json:"criado_em"`
	DecididoEm *time.Time `json:"decidido_em"`
}

// solicitacaoPendenteResposta é um item da fila de decisão
// (GET /api/promocoes).
type solicitacaoPendenteResposta struct {
	ID               string    `json:"id"`
	SolicitanteNome  string    `json:"solicitante_nome"`
	SolicitanteEmail string    `json:"solicitante_email"`
	PapelAtual       string    `json:"papel_atual"`
	PapelAlvo        string    `json:"papel_alvo"`
	CriadoEm         time.Time `json:"criado_em"`
}

// decisaoResumo é a resposta de POST /api/promocoes/{id}/decisao.
type decisaoResumo struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	PapelAlvo  string     `json:"papel_alvo"`
	DecididoEm *time.Time `json:"decidido_em"`
}

// decisaoRequest é o corpo aceito por POST /api/promocoes/{id}/decisao.
// `Aprovar` é um ponteiro DE PROPÓSITO: um corpo `{}`, `{"aprovar":null}` ou
// com a chave errada decodifica sem erro e, com um `bool` puro, viraria
// silenciosamente `aprovar=false` — uma rejeição involuntária de uma
// solicitação pendente. Nil -> 400 VALIDATION_ERROR.
type decisaoRequest struct {
	Aprovar *bool `json:"aprovar"`
}

// SolicitarPromocaoHandler expõe POST /api/promocoes: a própria conta pede
// promoção para o papel imediatamente acima. O corpo é ignorado — o alvo é
// sempre derivado de `usuario.Papel` do contexto.
func SolicitarPromocaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("SolicitarPromocaoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		s, err := services.SolicitarPromocao(db, usuario.ID, usuario.Papel)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusCreated, map[string]any{
				"solicitacao": solicitacaoResumo{
					ID:         s.ID,
					PapelAlvo:  s.PapelAlvo,
					Status:     s.Status,
					CriadoEm:   s.CriadoEm,
					DecididoEm: s.DecididoEm,
				},
			})
		case errors.Is(err, services.ErrPromocaoIndisponivel):
			escreverErro(w, http.StatusForbidden, "FORBIDDEN", "não há promoção de papel disponível para o seu papel")
		case errors.Is(err, services.ErrSolicitacaoPendenteExiste):
			escreverErro(w, http.StatusConflict, "CONFLICT", "você já tem uma solicitação de promoção pendente")
		default:
			slog.Error("falha ao solicitar promoção", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao solicitar promoção")
		}
	}
}

// MinhaSolicitacaoHandler expõe GET /api/promocoes/minha: o estado da
// solicitação mais recente da própria conta, ou `{"solicitacao":null}` se ela
// nunca solicitou.
func MinhaSolicitacaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("MinhaSolicitacaoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		s, err := services.BuscarMinhaSolicitacao(db, usuario.ID)
		if err != nil {
			slog.Error("falha ao buscar solicitação de promoção da conta", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao buscar solicitação de promoção")
			return
		}
		if s == nil {
			escreverJSON(w, http.StatusOK, map[string]any{"solicitacao": nil})
			return
		}
		escreverJSON(w, http.StatusOK, map[string]any{
			"solicitacao": solicitacaoResumo{
				ID:         s.ID,
				PapelAlvo:  s.PapelAlvo,
				Status:     s.Status,
				CriadoEm:   s.CriadoEm,
				DecididoEm: s.DecididoEm,
			},
		})
	}
}

// ListarPromocoesHandler expõe GET /api/promocoes, sempre atrás de
// RequireRole(gestor). O recorte de escopo (adm vê tudo, gestor só alvo
// `almoxarife`) fica em services.ListarSolicitacoesPendentes, a partir do
// papel já resolvido pelo middleware.
func ListarPromocoesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("ListarPromocoesHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		pendentes, err := services.ListarSolicitacoesPendentes(db, usuario.Papel)
		if err != nil {
			slog.Error("falha ao listar solicitações de promoção pendentes", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar solicitações de promoção")
			return
		}

		itens := make([]solicitacaoPendenteResposta, 0, len(pendentes))
		for _, p := range pendentes {
			itens = append(itens, solicitacaoPendenteResposta{
				ID:               p.ID,
				SolicitanteNome:  p.SolicitanteNome,
				SolicitanteEmail: p.SolicitanteEmail,
				PapelAtual:       p.PapelAtual,
				PapelAlvo:        p.PapelAlvo,
				CriadoEm:         p.CriadoEm,
			})
		}
		escreverJSON(w, http.StatusOK, map[string]any{"solicitacoes": itens})
	}
}

// DecidirPromocaoHandler expõe POST /api/promocoes/{id}/decisao, sempre atrás
// de RequireRole(gestor). Lê `{"aprovar": bool}` sob http.MaxBytesReader —
// corpo malformado OU sem a chave `aprovar` (nil) -> 400 VALIDATION_ERROR,
// nunca uma rejeição silenciosa.
func DecidirPromocaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("DecidirPromocaoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req decisaoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}
		if req.Aprovar == nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		s, err := services.DecidirSolicitacao(db, r.PathValue("id"), usuario.ID, usuario.Papel, *req.Aprovar)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]any{
				"solicitacao": decisaoResumo{
					ID:         s.ID,
					Status:     s.Status,
					PapelAlvo:  s.PapelAlvo,
					DecididoEm: s.DecididoEm,
				},
			})
		case errors.Is(err, services.ErrDecisaoNaoAutorizada):
			escreverErro(w, http.StatusForbidden, "FORBIDDEN", "papel insuficiente para decidir esta promoção")
		case errors.Is(err, services.ErrSolicitacaoNaoEncontrada):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "solicitação de promoção não encontrada")
		case errors.Is(err, services.ErrSolicitacaoNaoPendente):
			escreverErro(w, http.StatusConflict, "CONFLICT", "esta solicitação não está mais pendente")
		case errors.Is(err, services.ErrEstadoContaMudou):
			escreverErro(w, http.StatusConflict, "CONFLICT", "o estado da conta do solicitante mudou; recarregue a fila")
		default:
			slog.Error("falha ao decidir promoção", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao decidir promoção")
		}
	}
}
