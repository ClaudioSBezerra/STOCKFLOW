// Package handlers, arquivo plataforma_auth.go: login, refresh, logout e
// `/me` do Dono da Plataforma — Story 9.2 (Epic 9), spec-9-2, AD-21.
//
// Rotas próprias em `/api/plataforma/auth/...`, fora do prefixo de Empresa e
// sem RequireEmpresa. O cookie de refresh também é próprio
// (`refresh_token_plataforma`, Path `/api/plataforma/auth`): o navegador
// nunca o anexa às rotas de sessão de uma Empresa, nem o cookie de uma
// Empresa às rotas da Plataforma.
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

const (
	refreshTokenPlataformaCookieName = "refresh_token_plataforma"
	refreshTokenPlataformaCookiePath = "/api/plataforma/auth"
)

// plataformaLoginRequest é o payload de POST /api/plataforma/auth/login: os
// três fatores numa única chamada (a MFA do Dono é estrutural).
type plataformaLoginRequest struct {
	Email  string `json:"email"`
	Senha  string `json:"senha"`
	Codigo string `json:"codigo"`
}

// donoResposta é o Dono devolvido no login e em `/me`.
type donoResposta struct {
	ID    string `json:"id"`
	Nome  string `json:"nome"`
	Email string `json:"email"`
}

func donoRespostaDe(d services.DonoSessao) donoResposta {
	return donoResposta{ID: d.ID, Nome: d.Nome, Email: d.Email}
}

func setRefreshCookiePlataforma(w http.ResponseWriter, r *http.Request, token string, expiraEm time.Time) {
	maxAge := int(time.Until(expiraEm).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{
		Name:     refreshTokenPlataformaCookieName,
		Value:    token,
		Path:     refreshTokenPlataformaCookiePath,
		HttpOnly: true,
		Secure:   cookieEhSeguro(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

func clearRefreshCookiePlataforma(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshTokenPlataformaCookieName,
		Value:    "",
		Path:     refreshTokenPlataformaCookiePath,
		HttpOnly: true,
		Secure:   cookieEhSeguro(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// PlataformaLoginHandler expõe POST /api/plataforma/auth/login: e-mail +
// senha + código TOTP. Qualquer falha de credencial -> a MESMA resposta 401
// INVALID_CREDENTIALS; campo em branco -> 400 VALIDATION_ERROR. No sucesso,
// 200 `{token, dono}` + cookie de refresh da Plataforma.
func PlataformaLoginHandler(db *sql.DB, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)

		var req plataformaLoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		donoID, err := services.LoginDonoPlataforma(db, req.Email, req.Senha, req.Codigo)
		switch {
		case err == nil:
			// segue abaixo
		case errors.Is(err, services.ErrLoginValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "e-mail, senha e código são obrigatórios")
			return
		case errors.Is(err, services.ErrCredenciaisInvalidas):
			slog.Warn("login do dono da plataforma recusado", "email_informado", req.Email)
			escreverErro(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "E-mail, senha ou código inválidos.")
			return
		default:
			slog.Error("falha ao processar login do dono da plataforma", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar login")
			return
		}

		dono, err := services.BuscarDonoSessao(db, donoID)
		if err != nil {
			slog.Error("falha ao carregar dono da plataforma recém-autenticado", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar login")
			return
		}

		accessToken, refreshToken, expiraRefresh, err := services.EmitirSessaoPlataforma(db, jwtSecret, dono.ID)
		if err != nil {
			slog.Error("falha ao emitir sessão da plataforma", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao emitir sessão")
			return
		}

		slog.Info("login do dono da plataforma", "dono_id", dono.ID)
		setRefreshCookiePlataforma(w, r, refreshToken, expiraRefresh)
		escreverJSON(w, http.StatusOK, map[string]any{
			"token": accessToken,
			"dono":  donoRespostaDe(dono),
		})
	}
}

// PlataformaRefreshHandler expõe POST /api/plataforma/auth/refresh: rotaciona
// a sessão do cookie. Cookie ausente/expirado/revogado/inexistente (ou Dono
// inativo) -> 401 TOKEN_EXPIRED com o cookie limpo.
func PlataformaRefreshHandler(db *sql.DB, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(refreshTokenPlataformaCookieName)
		if err != nil {
			clearRefreshCookiePlataforma(w, r)
			escreverErro(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "sessão expirada, faça login novamente")
			return
		}

		novoAccess, novoRefresh, expiraRefresh, err := services.RenovarSessaoPlataforma(db, jwtSecret, cookie.Value)
		switch {
		case err == nil:
			setRefreshCookiePlataforma(w, r, novoRefresh, expiraRefresh)
			escreverJSON(w, http.StatusOK, map[string]string{"token": novoAccess})
		case errors.Is(err, services.ErrSessaoInvalida):
			clearRefreshCookiePlataforma(w, r)
			escreverErro(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "sessão expirada, faça login novamente")
		default:
			slog.Error("falha ao renovar sessão da plataforma", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao renovar sessão")
		}
	}
}

// PlataformaLogoutHandler expõe POST /api/plataforma/auth/logout: revoga a
// sessão do cookie (se houver) e SEMPRE limpa o cookie, com 204. Idempotente.
func PlataformaLogoutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(refreshTokenPlataformaCookieName); err == nil {
			if err := services.RevogarSessaoPlataforma(db, cookie.Value); err != nil {
				slog.Error("falha ao revogar sessão da plataforma no logout", "error", err)
			}
		}
		clearRefreshCookiePlataforma(w, r)
		w.WriteHeader(http.StatusNoContent)
	}
}

// PlataformaMeHandler expõe GET /api/plataforma/auth/me, atrás de
// middleware.RequireDonoPlataforma: devolve o Dono resolvido do Postgres.
func PlataformaMeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dono, ok := donoDaRequisicao(w, r)
		if !ok {
			return
		}
		escreverJSON(w, http.StatusOK, donoRespostaDe(dono))
	}
}

// donoDaRequisicao extrai o Dono que RequireDonoPlataforma injetou. Ausente
// = handler registrado fora do middleware (erro de composição) -> 500.
func donoDaRequisicao(w http.ResponseWriter, r *http.Request) (services.DonoSessao, bool) {
	dono, ok := middleware.DonoDaSessao(r.Context())
	if !ok {
		slog.Error("handler da plataforma chamado sem Dono no contexto — RequireDonoPlataforma não foi aplicado", "rota", r.URL.Path)
		escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver a sessão")
		return services.DonoSessao{}, false
	}
	return dono, true
}
