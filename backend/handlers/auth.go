// Package handlers implementa a fronteira HTTP de autenticação: Story 1.3
// (cadastro/verificação de e-mail), Story 1.4 (login, refresh de sessão e
// `/me`) e Story 1.6 (esqueci-senha, validação de link de redefinição e
// redefinição de senha) — decodifica/serializa JSON, mapeia erros de
// `services/` para o envelope de erro fixo (AD-14), e nunca contém regra de
// negócio própria.
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

// erroEnvelope é o formato fixo de erro (AD-14): {"error":{"code","message"}}.
type erroEnvelope struct {
	Error erroDetalhe `json:"error"`
}

type erroDetalhe struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func escreverErro(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(erroEnvelope{Error: erroDetalhe{Code: code, Message: message}})
}

func escreverJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// cadastroRequestMaxBytes limita o corpo aceito por POST /api/auth/cadastro
// — rota pública e não autenticada, então json.Decode nunca deve ler um
// corpo arbitrariamente grande antes de rejeitá-lo.
const cadastroRequestMaxBytes = 64 * 1024

// cadastroRequest é o payload aceito por POST /api/auth/cadastro. O campo
// Papel existe só para ser explicitamente ignorado abaixo: nenhum valor
// vindo do formulário jamais decide o papel da conta criada (FR-3) — mesmo
// que o payload envie, por exemplo, `"papel":"adm"`.
type cadastroRequest struct {
	Nome  string `json:"nome"`
	Email string `json:"email"`
	Senha string `json:"senha"`
	Papel string `json:"papel"`
}

// CadastroHandler expõe POST /api/auth/cadastro: cria a conta sempre como
// `usuario`, enfileira o e-mail de verificação e nunca usa `req.Papel`.
func CadastroHandler(db *sql.DB, emailCfg services.EmailConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, cadastroRequestMaxBytes)

		var req cadastroRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		// req.Papel é lido acima apenas para existir no struct de decodificação
		// — deliberadamente nunca repassado a services.Cadastrar, que não tem
		// sequer um parâmetro de papel.
		_, err := services.Cadastrar(db, emailCfg, req.Nome, req.Email, req.Senha)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusCreated, map[string]string{
				"mensagem": "Cadastro realizado. Verifique seu e-mail para confirmar a conta.",
			})
		case errors.Is(err, services.ErrCadastroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "nome, e-mail e senha são obrigatórios")
		case errors.Is(err, services.ErrEmailDuplicado):
			escreverErro(w, http.StatusConflict, "CONFLICT", "Este e-mail já está cadastrado.")
		default:
			slog.Error("falha ao processar cadastro", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar cadastro")
		}
	}
}

// VerificarEmailHandler expõe GET /api/auth/verificar-email?token=...: libera
// `email_verificado` quando o token é válido, ou mapeia o erro correspondente
// da I/O Matrix.
func VerificarEmailHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")

		err := services.VerificarEmail(db, token)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]string{
				"mensagem": "E-mail verificado com sucesso.",
			})
		case errors.Is(err, services.ErrTokenNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "token de verificação não encontrado")
		case errors.Is(err, services.ErrTokenExpirado):
			escreverErro(w, http.StatusBadRequest, "TOKEN_EXPIRED", "token de verificação expirado ou já utilizado")
		default:
			slog.Error("falha ao verificar e-mail", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao verificar e-mail")
		}
	}
}

// authRequestMaxBytes limita o corpo aceito por POST /api/auth/login — mesma
// justificativa de cadastroRequestMaxBytes: rota pública e não autenticada.
const authRequestMaxBytes = 64 * 1024

// refreshTokenCookieName/refreshTokenCookiePath são compartilhados por
// LoginHandler e RefreshHandler (setar o cookie) e por RefreshHandler (lê-lo
// de volta) — o Path restrito a /api/auth (AD-6) garante que o navegador só
// anexa esse cookie nas próprias rotas de sessão, nunca em toda a API.
const (
	refreshTokenCookieName = "refresh_token"
	refreshTokenCookiePath = "/api/auth"
)

// usuarioResposta é o formato de usuário devolvido em POST /api/auth/login e
// GET /api/auth/me.
type usuarioResposta struct {
	ID    string `json:"id"`
	Nome  string `json:"nome"`
	Email string `json:"email"`
	Papel string `json:"papel"`
}

// cookieEhSeguro decide a flag Secure do cookie de refresh (AD-6): true
// quando a requisição chegou via TLS direto, ou via proxy reverso que
// declarou `X-Forwarded-Proto: https` (caso do compose/produção atrás de um
// proxy) — nunca setado incondicionalmente, para não quebrar o
// desenvolvimento local em HTTP puro.
func cookieEhSeguro(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// setRefreshCookie grava o refresh token rotacionado como cookie HttpOnly
// (AD-6): Path restrito a /api/auth, SameSite=Lax, Secure condicional, e
// Max-Age igual ao TTL restante da sessão em segundos.
func setRefreshCookie(w http.ResponseWriter, r *http.Request, token string, expiraEm time.Time) {
	maxAge := int(time.Until(expiraEm).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{
		Name:     refreshTokenCookieName,
		Value:    token,
		Path:     refreshTokenCookiePath,
		HttpOnly: true,
		Secure:   cookieEhSeguro(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

// clearRefreshCookie limpa o cookie de refresh (Max-Age=0, AD-6) — usado
// sempre que RefreshHandler rejeita o token apresentado (ausente, expirado,
// revogado ou inexistente), para que o navegador nunca reapresente um
// refresh token já sabidamente inválido.
func clearRefreshCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshTokenCookieName,
		Value:    "",
		Path:     refreshTokenCookiePath,
		HttpOnly: true,
		Secure:   cookieEhSeguro(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1, // negativo == "Max-Age: 0" no header (net/http), i.e. apaga o cookie agora.
	})
}

// loginRequest é o payload aceito por POST /api/auth/login.
type loginRequest struct {
	Email string `json:"email"`
	Senha string `json:"senha"`
}

// LoginHandler expõe POST /api/auth/login (Story 1.4, AD-6): valida
// e-mail/senha, e em caso de sucesso emite o access JWT no corpo da resposta
// e o refresh token em cookie HttpOnly. Todo cenário de credencial inválida
// (senha errada, e-mail inexistente, e-mail não verificado, conta
// desativada, conta só-SSO) devolve a MESMA resposta 401 INVALID_CREDENTIALS
// — nunca revela qual condição falhou nem se o e-mail existe.
func LoginHandler(db *sql.DB, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)

		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		usuarioID, err := services.Login(db, req.Email, req.Senha)
		switch {
		case err == nil:
			// segue abaixo
		case errors.Is(err, services.ErrLoginValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "e-mail e senha são obrigatórios")
			return
		case errors.Is(err, services.ErrCredenciaisInvalidas):
			escreverErro(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "E-mail ou senha inválidos.")
			return
		default:
			slog.Error("falha ao processar login", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar login")
			return
		}

		accessToken, refreshToken, expiraRefresh, err := services.EmitirSessao(db, jwtSecret, usuarioID)
		if err != nil {
			slog.Error("falha ao emitir sessão", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao emitir sessão")
			return
		}

		usuario, err := services.BuscarUsuarioSessao(db, usuarioID)
		if err != nil {
			slog.Error("falha ao carregar usuário recém-autenticado", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar login")
			return
		}

		setRefreshCookie(w, r, refreshToken, expiraRefresh)
		escreverJSON(w, http.StatusOK, map[string]any{
			"token": accessToken,
			"usuario": usuarioResposta{
				ID:    usuario.ID,
				Nome:  usuario.Nome,
				Email: usuario.Email,
				Papel: usuario.Papel,
			},
		})
	}
}

// RefreshHandler expõe POST /api/auth/refresh (Story 1.4, AD-6): lê o
// cookie de refresh, rotaciona a sessão (RenovarSessao) e devolve um novo
// access token + cookie. Cookie ausente/expirado/revogado/inexistente
// resultam todos em 401 TOKEN_EXPIRED com o cookie limpo — o cliente
// precisa logar novamente.
func RefreshHandler(db *sql.DB, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(refreshTokenCookieName)
		if err != nil {
			clearRefreshCookie(w, r)
			escreverErro(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "sessão expirada, faça login novamente")
			return
		}

		novoAccess, novoRefresh, expiraRefresh, err := services.RenovarSessao(db, jwtSecret, cookie.Value)
		switch {
		case err == nil:
			// segue abaixo
		case errors.Is(err, services.ErrSessaoInvalida):
			clearRefreshCookie(w, r)
			escreverErro(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "sessão expirada, faça login novamente")
			return
		default:
			slog.Error("falha ao renovar sessão", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao renovar sessão")
			return
		}

		// expiraRefresh é o prazo EFETIVAMENTE persistido por RenovarSessao, não
		// um recálculo local — evita que o cookie divirja do valor gravado em
		// `sessoes` pelo tempo do round-trip ao banco (mesmo padrão de
		// LoginHandler/EmitirSessao acima).
		setRefreshCookie(w, r, novoRefresh, expiraRefresh)
		escreverJSON(w, http.StatusOK, map[string]string{"token": novoAccess})
	}
}

// mensagemEsqueciSenha é o corpo devolvido por EsqueciSenhaHandler em TODO
// caso não-erro (200), exista ou não a conta. O intent desta story exige que
// seja byte-idêntico nos dois casos — um json.Encode de um map de chave única
// é determinístico, então nada aqui ramifica pela existência do e-mail.
const mensagemEsqueciSenha = "Se o e-mail existir, você receberá um link."

// esqueciSenhaRequest é o payload aceito por POST /api/auth/esqueci-senha.
type esqueciSenhaRequest struct {
	Email string `json:"email"`
}

// EsqueciSenhaHandler expõe POST /api/auth/esqueci-senha (Story 1.6): sempre
// responde 200 com a MESMA mensagem genérica — nunca revela por status, corpo
// ou latência perceptível se um e-mail está cadastrado (sem bcrypt neste
// caminho; sem ramo condicional após escrever a resposta). Só JSON malformado
// -> 400 VALIDATION_ERROR; erro real de infraestrutura -> 500 INTERNAL_ERROR.
// Não há limite de taxa aqui — é escopo da Story 1.10.
func EsqueciSenhaHandler(db *sql.DB, emailCfg services.EmailConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)

		var req esqueciSenhaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		if err := services.SolicitarRedefinicaoSenha(db, emailCfg, req.Email); err != nil {
			slog.Error("falha ao processar solicitação de redefinição de senha", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar solicitação")
			return
		}

		escreverJSON(w, http.StatusOK, map[string]string{"mensagem": mensagemEsqueciSenha})
	}
}

// ValidarRedefinicaoSenhaHandler expõe GET /api/auth/redefinir-senha?token=...
// (Story 1.6): checa a validade do link SEM consumi-lo, para a tela de
// redefinição explicar um link morto já ao abrir. Válido -> 200
// {"valido":true}; inexistente -> 404 NOT_FOUND; expirado/usado -> 400
// TOKEN_EXPIRED.
func ValidarRedefinicaoSenhaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")

		err := services.ValidarTokenRedefinicao(db, token)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]bool{"valido": true})
		case errors.Is(err, services.ErrTokenNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "link de redefinição não encontrado")
		case errors.Is(err, services.ErrTokenExpirado):
			escreverErro(w, http.StatusBadRequest, "TOKEN_EXPIRED", "link de redefinição expirado ou já utilizado")
		default:
			slog.Error("falha ao validar link de redefinição", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao validar link de redefinição")
		}
	}
}

// redefinirSenhaRequest é o payload aceito por POST /api/auth/redefinir-senha.
type redefinirSenhaRequest struct {
	Token string `json:"token"`
	Senha string `json:"senha"`
}

// RedefinirSenhaHandler expõe POST /api/auth/redefinir-senha (Story 1.6):
// valida o token + a força da nova senha, troca `senha_hash`, marca o token
// usado e revoga todas as sessões da conta. Nova senha reprovada na política
// -> 400 VALIDATION_ERROR SEM consumir o token; token inexistente -> 404
// NOT_FOUND; token expirado/usado (inclui a corrida "expirou entre o GET e o
// POST" e o reuso do mesmo link) -> 400 TOKEN_EXPIRED.
func RedefinirSenhaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)

		var req redefinirSenhaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		err := services.RedefinirSenha(db, req.Token, req.Senha)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]string{
				"mensagem": "Senha redefinida com sucesso. Faça login com a nova senha.",
			})
		case errors.Is(err, services.ErrSenhaFraca):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "A senha deve ter ao menos 8 caracteres, incluindo uma letra e um número.")
		case errors.Is(err, services.ErrTokenNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "link de redefinição não encontrado")
		case errors.Is(err, services.ErrTokenExpirado):
			escreverErro(w, http.StatusBadRequest, "TOKEN_EXPIRED", "link de redefinição expirado ou já utilizado")
		default:
			slog.Error("falha ao redefinir senha", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao redefinir senha")
		}
	}
}

// MeHandler expõe GET /api/auth/me, sempre registrado atrás de
// middleware.RequireAuth (main.go): devolve o usuário já resolvido pelo
// middleware a partir do Postgres — nunca do claim do JWT.
func MeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("MeHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		escreverJSON(w, http.StatusOK, usuarioResposta{
			ID:    usuario.ID,
			Nome:  usuario.Nome,
			Email: usuario.Email,
			Papel: usuario.Papel,
		})
	}
}
