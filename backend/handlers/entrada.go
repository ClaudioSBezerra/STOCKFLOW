package handlers

// Login na raiz do domínio pela conta — Story 15.2 (AD-36). As duas rotas
// daqui vivem FORA do prefixo `/e/{slug}` e SEM RequireEmpresa: a Empresa é
// descoberta pela conta (services.EntrarPelaConta), nunca pedida ao cliente.
// Por isso estes handlers nunca chamam empresaDaRequisicao — o slug da
// resposta e o Path do cookie vêm da conta cuja senha conferiu.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"stockflow/backend/services"
)

// entrarRequest é o payload de POST /api/auth/entrar.
type entrarRequest struct {
	Email string `json:"email"`
	Senha string `json:"senha"`
}

// entrarEscolhaRequest é o payload de POST /api/auth/entrar/escolha.
type entrarEscolhaRequest struct {
	EscolhaToken string `json:"escolhaToken"`
	Slug         string `json:"slug"`
}

// opcaoEscolha é um botão da pergunta "Ambiente real ou Treinamento?".
type opcaoEscolha struct {
	Slug         string `json:"slug"`
	NomeFantasia string `json:"nomeFantasia"`
	Treinamento  bool   `json:"treinamento"`
}

const mensagemContaBloqueada = "Muitas tentativas de login sem sucesso. Por segurança, novas tentativas ficam bloqueadas temporariamente. Tente novamente mais tarde."

// EntrarHandler expõe POST /api/auth/entrar: e-mail + senha, sem Empresa na
// URL. Uma conta conferida -> entra nela (sessão com cookie
// `Path=/e/{slug}/api/auth`, ou `mfaToken` se a conta tem MFA); duas ->
// `{escolha, escolhaToken}`. Mesmos códigos/mensagens de LoginHandler.
// `logs_acesso`: uma linha por conta avaliada, na Empresa dela — "avaliada" é
// conta em que a tentativa contou (services.EntrarPelaConta: as contas cuja
// senha conferiu, ou todas se nenhuma conferiu). Entrar na Empresa real não
// grava falha no Treinamento de senha diferente, como pelo endereço da
// Empresa.
func EntrarHandler(db *sql.DB, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)

		var req entrarRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		avaliadas, aceitas, err := services.EntrarPelaConta(db, req.Email, req.Senha)
		if errors.Is(err, services.ErrLoginValidacao) {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "e-mail e senha são obrigatórios")
			return
		}
		if err != nil && !errors.Is(err, services.ErrCredenciaisInvalidas) && !errors.Is(err, services.ErrContaBloqueada) {
			slog.Error("falha ao processar login pela conta", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar login")
			return
		}

		aceita := make(map[string]bool, len(aceitas))
		for _, c := range aceitas {
			aceita[c.UsuarioID] = true
		}
		for _, c := range avaliadas {
			if aceita[c.UsuarioID] {
				usuarioID := c.UsuarioID
				registrarTentativaLogin(r, db, c.EmpresaID, "senha", req.Email, &usuarioID, true)
			} else {
				registrarTentativaLogin(r, db, c.EmpresaID, "senha", req.Email, nil, false)
			}
		}

		switch {
		case errors.Is(err, services.ErrContaBloqueada):
			escreverErro(w, http.StatusTooManyRequests, "ACCOUNT_LOCKED", mensagemContaBloqueada)
			return
		case errors.Is(err, services.ErrCredenciaisInvalidas):
			escreverErro(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "E-mail ou senha inválidos.")
			return
		}

		if len(aceitas) == 1 {
			entrarNaConta(w, r, db, jwtSecret, aceitas[0])
			return
		}

		escolhaToken, err := services.IniciarEscolhaEmpresa(db, aceitas)
		if err != nil {
			slog.Error("falha ao iniciar escolha de Empresa", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar login")
			return
		}
		escolha := make([]opcaoEscolha, len(aceitas))
		for i, c := range aceitas {
			escolha[i] = opcaoEscolha{Slug: c.Slug, NomeFantasia: c.NomeFantasia, Treinamento: c.Treinamento}
		}
		escreverJSON(w, http.StatusOK, map[string]any{
			"escolha":      escolha,
			"escolhaToken": escolhaToken,
		})
	}
}

// EntrarEscolhaHandler expõe POST /api/auth/entrar/escolha: consome o
// `escolhaToken` e segue exatamente o ramo "uma conta" de EntrarHandler na
// conta da Empresa escolhida. Token vencido/reusado ou slug fora das contas
// conferidas -> 401 ESCOLHA_INVALIDA, sem cookie.
func EntrarEscolhaHandler(db *sql.DB, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)

		var req entrarEscolhaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		conta, err := services.ConcluirEscolhaEmpresa(db, req.EscolhaToken, req.Slug)
		switch {
		case err == nil:
		case errors.Is(err, services.ErrEscolhaInvalida):
			escreverErro(w, http.StatusUnauthorized, "ESCOLHA_INVALIDA", "A escolha expirou. Faça login novamente.")
			return
		default:
			slog.Error("falha ao concluir escolha de Empresa", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar login")
			return
		}

		entrarNaConta(w, r, db, jwtSecret, conta)
	}
}

// entrarNaConta é o ramo "uma conta": com MFA -> `{slug, mfaRequerido,
// mfaToken}` sem cookie (o código é verificado em
// `/e/{slug}/api/auth/mfa/verificar`); sem MFA -> sessão "senha" com o cookie
// de refresh escopado à Empresa da conta e `{slug}` — o frontend recarrega
// em `/e/{slug}/` e o AuthProvider restaura a sessão pelo refresh.
func entrarNaConta(w http.ResponseWriter, r *http.Request, db *sql.DB, jwtSecret []byte, conta services.ContaEntrada) {
	usuario, err := services.BuscarUsuarioSessao(db, conta.UsuarioID)
	if err != nil {
		slog.Error("falha ao carregar usuário recém-autenticado", "error", err)
		escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar login")
		return
	}

	if usuario.MFAHabilitado {
		mfaToken, err := services.IniciarLoginMFA(db, usuario.ID)
		if err != nil {
			slog.Error("falha ao iniciar login por MFA", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar login")
			return
		}
		escreverJSON(w, http.StatusOK, map[string]any{
			"slug":         conta.Slug,
			"mfaRequerido": true,
			"mfaToken":     mfaToken,
		})
		return
	}

	_, refreshToken, expiraRefresh, err := services.EmitirSessao(db, jwtSecret, usuario.ID, "senha")
	if err != nil {
		slog.Error("falha ao emitir sessão", "error", err)
		escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao emitir sessão")
		return
	}
	setRefreshCookieNoPath(w, r, refreshTokenCookiePathDoSlug(conta.Slug), refreshToken, expiraRefresh)
	escreverJSON(w, http.StatusOK, map[string]any{"slug": conta.Slug})
}
