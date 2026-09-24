// Story 1.11: MFA obrigatório para papéis administrativos (FR-37/SM-2).
// MFAVerificarHandler troca o token opaco emitido por LoginHandler (quando a
// conta já tem `mfa_habilitado=true`) por uma sessão de verdade, mediante um
// código TOTP correto — rota pública, sem RequireAuth (ainda não há sessão
// neste ponto do fluxo). MFAIniciarHandler/MFAConfirmarHandler expõem o
// enrollment (Configurações → Segurança), sempre atrás de RequireAuth (sem
// RequireRole: qualquer papel pode configurar MFA, ainda que só
// gestor/adm o exijam para acessar rotas restritas).
package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// mfaVerificarRequest é o payload aceito por POST /api/auth/mfa/verificar.
type mfaVerificarRequest struct {
	MfaToken string `json:"mfaToken"`
	Codigo   string `json:"codigo"`
}

// MFAVerificarHandler expõe POST /api/auth/mfa/verificar (público, sem
// RequireAuth): troca `mfaToken` + `codigo` TOTP por uma sessão de verdade,
// idêntica à de um login sem MFA. Token inexistente/expirado/já usado -> 401
// MFA_TOKEN_INVALIDO; código incorreto -> 401 MFA_CODIGO_INVALIDO (o token
// continua válido, permitindo nova tentativa até expirar); conta bloqueada
// por excesso de tentativas nesse meio-tempo (mesmo contador da Story 1.10,
// compartilhado com senha) -> 429 ACCOUNT_LOCKED, mesma mensagem do login.
func MFAVerificarHandler(db *sql.DB, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)

		var req mfaVerificarRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		usuarioID, err := services.ConcluirLoginMFA(db, empresa.ID, req.MfaToken, req.Codigo)
		switch {
		case err == nil:
			// segue abaixo
		case errors.Is(err, services.ErrTokenNaoEncontrado), errors.Is(err, services.ErrTokenExpirado):
			escreverErro(w, http.StatusUnauthorized, "MFA_TOKEN_INVALIDO", "código de login expirado ou inválido. Faça login novamente.")
			return
		case errors.Is(err, services.ErrMFACodigoInvalido):
			escreverErro(w, http.StatusUnauthorized, "MFA_CODIGO_INVALIDO", "Código de autenticação inválido.")
			return
		case errors.Is(err, services.ErrContaBloqueada):
			escreverErro(w, http.StatusTooManyRequests, "ACCOUNT_LOCKED", "Muitas tentativas de login sem sucesso. Por segurança, novas tentativas ficam bloqueadas temporariamente. Tente novamente mais tarde.")
			return
		default:
			slog.Error("falha ao concluir login por MFA", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar login")
			return
		}

		usuario, err := services.BuscarUsuarioSessao(db, usuarioID)
		if err != nil {
			slog.Error("falha ao carregar usuário após login por MFA", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar login")
			return
		}

		emitirSessaoEResponder(w, r, db, jwtSecret, usuario, "senha")
	}
}

// MFAIniciarHandler expõe POST /api/auth/mfa/iniciar, sempre atrás de
// RequireAuth (sem RequireRole — qualquer papel pode iniciar o enrollment).
// Conta já com `mfa_habilitado=true` -> 409 MFA_JA_CONFIGURADO, sem gerar
// segredo novo (não há opção de reconfigurar nesta story). Senão, gera um
// segredo TOTP novo e a URL otpauth:// correspondente — nada é gravado
// ainda; só POST /mfa/confirmar persiste.
func MFAIniciarHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("MFAIniciarHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		if usuario.MFAHabilitado {
			escreverErro(w, http.StatusConflict, "MFA_JA_CONFIGURADO", "autenticação em duas etapas já configurada para esta conta")
			return
		}

		segredo, otpauthURL, err := services.IniciarConfiguracaoMFA(usuario.Email)
		if err != nil {
			slog.Error("falha ao iniciar configuração de MFA", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao iniciar configuração de MFA")
			return
		}

		escreverJSON(w, http.StatusOK, map[string]string{
			"segredo":    segredo,
			"otpauthUrl": otpauthURL,
		})
	}
}

// mfaConfirmarRequest é o payload aceito por POST /api/auth/mfa/confirmar.
// SenhaAtual (Story 1.11) é obrigatória: sem ela, um access token roubado
// (válido até 30min) bastaria para habilitar MFA com o autenticador do
// atacante na conta da vítima — ver ConfirmarConfiguracaoMFA.
type mfaConfirmarRequest struct {
	Segredo    string `json:"segredo"`
	Codigo     string `json:"codigo"`
	SenhaAtual string `json:"senhaAtual"`
}

// MFAConfirmarHandler expõe POST /api/auth/mfa/confirmar, sempre atrás de
// RequireAuth (sem RequireRole). Conta já com `mfa_habilitado=true` -> 409
// MFA_JA_CONFIGURADO (mesmo guard de MFAIniciarHandler, checado ANTES de
// tocar em services — evita validar um código contra um enrollment que já
// não faz sentido). Senha atual incorreta -> 401 INVALID_CREDENTIALS, mesmo
// vocabulário do LoginHandler (Story 1.11: RequireAuth sozinho não é prova
// suficiente de posse da senha). Código incorreto -> 400 MFA_CODIGO_INVALIDO,
// nenhuma coluna gravada, o mesmo QR/segredo continua válido para nova
// tentativa. Sucesso -> `usuarios.mfa_habilitado=true`, `usuarios.mfa_secret`
// gravado.
func MFAConfirmarHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("MFAConfirmarHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		if usuario.MFAHabilitado {
			escreverErro(w, http.StatusConflict, "MFA_JA_CONFIGURADO", "autenticação em duas etapas já configurada para esta conta")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req mfaConfirmarRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		err := services.ConfirmarConfiguracaoMFA(db, usuario.ID, req.SenhaAtual, req.Segredo, req.Codigo)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]any{})
		case errors.Is(err, services.ErrCredenciaisInvalidas):
			escreverErro(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "E-mail ou senha inválidos.")
		case errors.Is(err, services.ErrMFACodigoInvalido):
			escreverErro(w, http.StatusBadRequest, "MFA_CODIGO_INVALIDO", "Código de autenticação inválido.")
		case errors.Is(err, services.ErrMFAJaConfigurado):
			escreverErro(w, http.StatusConflict, "MFA_JA_CONFIGURADO", "autenticação em duas etapas já configurada para esta conta")
		default:
			slog.Error("falha ao confirmar configuração de MFA", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao confirmar configuração de MFA")
		}
	}
}

// mfaDesligarRequest é o payload aceito por POST /api/auth/mfa/desligar.
type mfaDesligarRequest struct {
	SenhaAtual string `json:"senhaAtual"`
	Codigo     string `json:"codigo"`
}

// MFADesligarHandler expõe POST /api/auth/mfa/desligar (Story 14.4), atrás só
// de RequireAuth (qualquer papel): a própria conta desliga o seu MFA com senha
// atual + código TOTP vigente. Corpo inválido ou campo vazio -> 400 (sem
// contar tentativa); sessão sem MFA -> 409
// MFA_NAO_CONFIGURADO; `gestor`/`adm` numa Empresa que exige -> 409
// MFA_EXIGIDO_PELA_EMPRESA; conta bloqueada -> 429 ACCOUNT_LOCKED; senha OU
// código errado -> o MESMO 401 INVALID_CREDENTIALS (as duas falhas contam
// tentativa — sem oráculo da senha para quem tem um access token roubado).
func MFADesligarHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("MFADesligarHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req mfaDesligarRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}
		// Campo vazio nunca chega ao service: um envio em branco não pode
		// consumir tentativa do bloqueio.
		if strings.TrimSpace(req.SenhaAtual) == "" || strings.TrimSpace(req.Codigo) == "" {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		if !usuario.MFAHabilitado {
			escreverErro(w, http.StatusConflict, "MFA_NAO_CONFIGURADO", "autenticação em duas etapas não configurada para esta conta")
			return
		}

		err := services.DesligarMFAPropria(db, usuario.ID, usuario.Papel, empresa.MFAObrigatorio, req.SenhaAtual, req.Codigo)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]any{})
		case errors.Is(err, services.ErrMFAExigidoPelaEmpresa):
			escreverErro(w, http.StatusConflict, "MFA_EXIGIDO_PELA_EMPRESA", "A Empresa exige dupla autenticação para o seu papel; ela não pode ser desligada.")
		case errors.Is(err, services.ErrMFANaoConfigurado):
			escreverErro(w, http.StatusConflict, "MFA_NAO_CONFIGURADO", "autenticação em duas etapas não configurada para esta conta")
		case errors.Is(err, services.ErrContaBloqueada):
			escreverErro(w, http.StatusTooManyRequests, "ACCOUNT_LOCKED", "Muitas tentativas de login sem sucesso. Por segurança, novas tentativas ficam bloqueadas temporariamente. Tente novamente mais tarde.")
		case errors.Is(err, services.ErrCredenciaisInvalidas), errors.Is(err, services.ErrMFACodigoInvalido):
			escreverErro(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Senha ou código inválido.")
		default:
			slog.Error("falha ao desligar MFA", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao desligar MFA")
		}
	}
}
