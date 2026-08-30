package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// Story 1.11: MFA obrigatório para papéis administrativos (FR-37/SM-2).
// Cobre a I/O Matrix completa dos 3 endpoints novos, reaproveitando
// criarUsuarioLoginComEstado/postLogin (auth_test.go) como base — mesmo
// padrão do restante da suíte de handlers.

func postMFAVerificar(db *sql.DB, jsonBody string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/mfa/verificar", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	MFAVerificarHandler(db, testJWTSecret)(w, req)
	return w
}

// postMFAIniciar/postMFAConfirmar despacham através da MESMA composição
// registrada em main.go (RequireAuth, sem RequireRole) — nunca chamam o
// handler isoladamente.
func postMFAIniciar(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/mfa/iniciar", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	middleware.RequireAuth(db, testJWTSecret)(MFAIniciarHandler(db))(w, req)
	return w
}

func postMFAConfirmar(db *sql.DB, authHeader, jsonBody string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/mfa/confirmar", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	middleware.RequireAuth(db, testJWTSecret)(MFAConfirmarHandler(db))(w, req)
	return w
}

// habilitarMFAConta liga mfa_habilitado com um segredo TOTP real numa conta
// já existente — molde de habilitarMFATeste (usuarios_test.go), mas
// devolvendo o segredo para o chamador computar códigos.
func habilitarMFAConta(t *testing.T, db *sql.DB, usuarioID string) (segredo string) {
	t.Helper()
	segredo, err := services.GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP: %v", err)
	}
	if _, err := db.Exec(`UPDATE usuarios SET mfa_habilitado = true, mfa_secret = $1 WHERE id = $2`, segredo, usuarioID); err != nil {
		t.Fatalf("falha ao habilitar MFA de teste: %v", err)
	}
	return segredo
}

// codigoTOTPHandlerTeste gera um código TOTP válido para `segredo` no
// instante atual — mesma reimplementação do algoritmo HOTP/RFC 6238 já usada
// por totpCodigoTesteAtual (usuarios_test.go), duplicada aqui deliberadamente
// (mesmo padrão de duplicação de crypto em testes já usado no projeto) para
// manter este arquivo de teste autocontido.
func codigoTOTPHandlerTeste(t *testing.T, segredo string) string {
	t.Helper()
	return totpCodigoTesteAtual(t, segredo)
}

// TestLoginHandler_MFARequerido prova o cenário "Login por senha, MFA
// configurado" da I/O Matrix: 200 com {"mfaRequerido":true,"mfaToken":...},
// SEM emitir sessão (nenhum cookie de refresh, nenhum "token"/"usuario" no
// corpo).
func TestLoginHandler_MFARequerido(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioLogin(t, db, "login-mfa-requerido@empresa.com", "senha-123456")
	habilitarMFAConta(t, db, usuarioID)

	w := postLogin(db, `{"email":"login-mfa-requerido@empresa.com","senha":"senha-123456"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("falha ao decodificar corpo: %v", err)
	}
	if mfaRequerido, _ := body["mfaRequerido"].(bool); !mfaRequerido {
		t.Errorf("mfaRequerido = %v, want true", body["mfaRequerido"])
	}
	if mfaToken, _ := body["mfaToken"].(string); mfaToken == "" {
		t.Error("mfaToken vazio ou ausente")
	}
	if _, ok := body["token"]; ok {
		t.Error("resposta contém \"token\" — nenhuma sessão deveria ser emitida quando mfaRequerido=true")
	}

	res := w.Result()
	for _, c := range res.Cookies() {
		if c.Name == refreshTokenCookieName {
			t.Error("cookie refresh_token setado — nenhuma sessão deveria ser emitida quando mfaRequerido=true")
		}
	}
}

// TestLoginHandler_MFARequerido_RegistraUmaLinhaDeSucesso prova a linha
// "Login por senha, MFA exigido" da I/O Matrix da Story 1.12: a resposta é
// {"mfaRequerido":true,...} igual a hoje, e mesmo assim exatamente UMA linha
// logs_acesso metodo='senha' sucesso=true é gravada (o campo `sucesso`
// reflete o fator senha, não a emissão de sessão).
func TestLoginHandler_MFARequerido_RegistraUmaLinhaDeSucesso(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioLogin(t, db, "login-mfa-log@empresa.com", "senha-123456")
	habilitarMFAConta(t, db, usuarioID)

	w := postLogin(db, `{"email":"login-mfa-log@empresa.com","senha":"senha-123456"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if mfaRequerido, _ := body["mfaRequerido"].(bool); !mfaRequerido {
		t.Fatalf("mfaRequerido = %v, want true", body["mfaRequerido"])
	}

	if n := contarLogsAcesso(t, db); n != 1 {
		t.Fatalf("logs_acesso = %d, want exatamente 1", n)
	}
	l := ultimoLogAcesso(t, db)
	if l.metodo != "senha" || !l.sucesso || !l.usuarioID.Valid || l.usuarioID.String != usuarioID {
		t.Errorf("linha = %+v, want metodo=senha sucesso=true usuario_id=%q", l, usuarioID)
	}
}

// TestMFAVerificarHandler_CodigoCorreto prova o caminho feliz: mfaToken
// válido + código TOTP correto emitem sessão idêntica à de um login sem MFA.
func TestMFAVerificarHandler_CodigoCorreto(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioLogin(t, db, "mfa-verificar-ok@empresa.com", "senha-123456")
	segredo := habilitarMFAConta(t, db, usuarioID)

	wLogin := postLogin(db, `{"email":"mfa-verificar-ok@empresa.com","senha":"senha-123456"}`)
	var loginBody struct {
		MfaToken string `json:"mfaToken"`
	}
	if err := json.Unmarshal(wLogin.Body.Bytes(), &loginBody); err != nil {
		t.Fatalf("falha ao decodificar login: %v", err)
	}

	codigo := codigoTOTPHandlerTeste(t, segredo)
	w := postMFAVerificar(db, `{"mfaToken":"`+loginBody.MfaToken+`","codigo":"`+codigo+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}

	var body struct {
		Token   string `json:"token"`
		Usuario struct {
			ID            string `json:"id"`
			MfaHabilitado bool   `json:"mfaHabilitado"`
			Origem        string `json:"origem"`
		} `json:"usuario"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("falha ao decodificar resposta: %v", err)
	}
	if body.Token == "" {
		t.Error("token vazio")
	}
	if body.Usuario.ID != usuarioID {
		t.Errorf("usuario.id = %q, want %q", body.Usuario.ID, usuarioID)
	}
	if !body.Usuario.MfaHabilitado {
		t.Error("usuario.mfaHabilitado = false, want true")
	}
	if body.Usuario.Origem != "senha" {
		t.Errorf("usuario.origem = %q, want %q", body.Usuario.Origem, "senha")
	}

	res := w.Result()
	var achouCookie bool
	for _, c := range res.Cookies() {
		if c.Name == refreshTokenCookieName {
			achouCookie = true
		}
	}
	if !achouCookie {
		t.Error("cookie refresh_token não setado")
	}
}

// TestMFAVerificarHandler_CodigoErrado prova que um código incorreto devolve
// 401 MFA_CODIGO_INVALIDO e não consome o mfaToken (nova tentativa continua
// possível).
func TestMFAVerificarHandler_CodigoErrado(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioLogin(t, db, "mfa-verificar-errado@empresa.com", "senha-123456")
	habilitarMFAConta(t, db, usuarioID)

	wLogin := postLogin(db, `{"email":"mfa-verificar-errado@empresa.com","senha":"senha-123456"}`)
	var loginBody struct {
		MfaToken string `json:"mfaToken"`
	}
	if err := json.Unmarshal(wLogin.Body.Bytes(), &loginBody); err != nil {
		t.Fatalf("falha ao decodificar login: %v", err)
	}

	w := postMFAVerificar(db, `{"mfaToken":"`+loginBody.MfaToken+`","codigo":"000000"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "MFA_CODIGO_INVALIDO" {
		t.Errorf("code = %q, want %q", env.Error.Code, "MFA_CODIGO_INVALIDO")
	}

	var usadoEm sql.NullTime
	if err := db.QueryRow(`SELECT usado_em FROM tokens_acao WHERE token = $1`, loginBody.MfaToken).Scan(&usadoEm); err != nil {
		t.Fatalf("falha ao reler token: %v", err)
	}
	if usadoEm.Valid {
		t.Error("mfaToken consumido apesar do código errado")
	}
}

// TestMFAVerificarHandler_SextaTentativaResponde429 prova o mesmo desenho da
// Story 1.10 (TestLoginHandler_SextaTentativaResponde429) aplicado ao segundo
// fator: como um código errado NUNCA consome o mfaToken, o MESMO token
// emitido por um único login aceita repetidas tentativas até expirar ou a
// conta bloquear — 5 códigos errados seguidos contra ele, e a 6ª tentativa
// (mesmo token, agora com o código CORRETO) responde 429 ACCOUNT_LOCKED,
// mesmo vocabulário do bloqueio de senha (contador compartilhado).
func TestMFAVerificarHandler_SextaTentativaResponde429(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioLogin(t, db, "mfa-sexta-falha-handler@empresa.com", "senha-123456")
	segredo := habilitarMFAConta(t, db, usuarioID)

	wLogin := postLogin(db, `{"email":"mfa-sexta-falha-handler@empresa.com","senha":"senha-123456"}`)
	var loginBody struct {
		MfaToken     string `json:"mfaToken"`
		MfaRequerido bool   `json:"mfaRequerido"`
	}
	if err := json.Unmarshal(wLogin.Body.Bytes(), &loginBody); err != nil {
		t.Fatalf("falha ao decodificar login: %v (body=%s)", err, wLogin.Body.String())
	}
	if !loginBody.MfaRequerido {
		t.Fatalf("mfaRequerido=false — conta deveria exigir MFA")
	}

	for i := 1; i <= 5; i++ {
		w := postMFAVerificar(db, `{"mfaToken":"`+loginBody.MfaToken+`","codigo":"000000"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("tentativa %d: status = %d, want %d (body=%s)", i, w.Code, http.StatusUnauthorized, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "MFA_CODIGO_INVALIDO" {
			t.Fatalf("tentativa %d: code = %q, want %q", i, env.Error.Code, "MFA_CODIGO_INVALIDO")
		}
	}

	// 6ª tentativa, mesmo mfaToken (nunca consumido pelas falhas acima), agora
	// com o código CORRETO — ainda assim bloqueada.
	w := postMFAVerificar(db, `{"mfaToken":"`+loginBody.MfaToken+`","codigo":"`+codigoTOTPHandlerTeste(t, segredo)+`"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("6ª tentativa: status = %d, want %d (body=%s)", w.Code, http.StatusTooManyRequests, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "ACCOUNT_LOCKED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "ACCOUNT_LOCKED")
	}
}

// TestMFAVerificarHandler_TokenInvalidoOuExpirado prova o cenário "mfaToken
// expirado/reusado": 401 MFA_TOKEN_INVALIDO tanto para um token inexistente
// quanto para um expirado.
func TestMFAVerificarHandler_TokenInvalidoOuExpirado(t *testing.T) {
	db := testDB(t)

	t.Run("token inexistente", func(t *testing.T) {
		w := postMFAVerificar(db, `{"mfaToken":"token-nunca-existiu","codigo":"123456"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "MFA_TOKEN_INVALIDO" {
			t.Errorf("code = %q, want %q", env.Error.Code, "MFA_TOKEN_INVALIDO")
		}
	})

	t.Run("token expirado", func(t *testing.T) {
		usuarioID := criarUsuarioLogin(t, db, "mfa-token-expirado-handler@empresa.com", "senha-123456")
		segredo := habilitarMFAConta(t, db, usuarioID)

		wLogin := postLogin(db, `{"email":"mfa-token-expirado-handler@empresa.com","senha":"senha-123456"}`)
		var loginBody struct {
			MfaToken string `json:"mfaToken"`
		}
		if err := json.Unmarshal(wLogin.Body.Bytes(), &loginBody); err != nil {
			t.Fatalf("falha ao decodificar login: %v", err)
		}
		if _, err := db.Exec(`UPDATE tokens_acao SET expira_em = now() - interval '1 hour' WHERE token = $1`, loginBody.MfaToken); err != nil {
			t.Fatalf("falha ao forçar expiração: %v", err)
		}

		codigo := codigoTOTPHandlerTeste(t, segredo)
		w := postMFAVerificar(db, `{"mfaToken":"`+loginBody.MfaToken+`","codigo":"`+codigo+`"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "MFA_TOKEN_INVALIDO" {
			t.Errorf("code = %q, want %q", env.Error.Code, "MFA_TOKEN_INVALIDO")
		}
	})
}

// TestMFAIniciarHandler_Sucesso prova o caminho feliz do enrollment: 200 com
// segredo + otpauthUrl, nada gravado no banco ainda.
func TestMFAIniciarHandler_Sucesso(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioLogin(t, db, "mfa-iniciar-handler@empresa.com", "senha-123456")
	token := tokenDeLogin(t, db, "mfa-iniciar-handler@empresa.com", "senha-123456")

	w := postMFAIniciar(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	var body struct {
		Segredo    string `json:"segredo"`
		OtpauthURL string `json:"otpauthUrl"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("falha ao decodificar resposta: %v", err)
	}
	if body.Segredo == "" || body.OtpauthURL == "" {
		t.Fatalf("segredo/otpauthUrl vazios: %+v", body)
	}

	var mfaHabilitado bool
	var mfaSecret sql.NullString
	if err := db.QueryRow(`SELECT mfa_habilitado, mfa_secret FROM usuarios WHERE id = $1`, usuarioID).Scan(&mfaHabilitado, &mfaSecret); err != nil {
		t.Fatalf("falha ao reler usuario: %v", err)
	}
	if mfaHabilitado || mfaSecret.Valid {
		t.Error("MFAIniciarHandler não deveria gravar nada no banco")
	}
}

// TestMFAIniciarHandler_JaConfigurado prova o guard: uma conta com
// mfa_habilitado=true recebe 409 MFA_JA_CONFIGURADO.
func TestMFAIniciarHandler_JaConfigurado(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioLogin(t, db, "mfa-iniciar-ja-config@empresa.com", "senha-123456")
	segredo := habilitarMFAConta(t, db, usuarioID)
	token := tokenDeLoginComMFA(t, db, "mfa-iniciar-ja-config@empresa.com", "senha-123456", segredo)

	w := postMFAIniciar(db, "Bearer "+token)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "MFA_JA_CONFIGURADO" {
		t.Errorf("code = %q, want %q", env.Error.Code, "MFA_JA_CONFIGURADO")
	}
}

// TestMFAConfirmarHandler_Sucesso prova o caminho feliz: código correto
// grava mfa_habilitado=true/mfa_secret.
func TestMFAConfirmarHandler_Sucesso(t *testing.T) {
	db := testDB(t)
	criarUsuarioLogin(t, db, "mfa-confirmar-handler-ok@empresa.com", "senha-123456")
	token := tokenDeLogin(t, db, "mfa-confirmar-handler-ok@empresa.com", "senha-123456")

	wIniciar := postMFAIniciar(db, "Bearer "+token)
	var iniciarBody struct {
		Segredo string `json:"segredo"`
	}
	if err := json.Unmarshal(wIniciar.Body.Bytes(), &iniciarBody); err != nil {
		t.Fatalf("falha ao decodificar /mfa/iniciar: %v", err)
	}

	codigo := codigoTOTPHandlerTeste(t, iniciarBody.Segredo)
	w := postMFAConfirmar(db, "Bearer "+token, `{"segredo":"`+iniciarBody.Segredo+`","codigo":"`+codigo+`","senhaAtual":"senha-123456"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}

	var mfaHabilitado bool
	if err := db.QueryRow(`SELECT mfa_habilitado FROM usuarios WHERE email = 'mfa-confirmar-handler-ok@empresa.com'`).Scan(&mfaHabilitado); err != nil {
		t.Fatalf("falha ao reler usuario: %v", err)
	}
	if !mfaHabilitado {
		t.Error("mfa_habilitado = false após confirmação bem-sucedida")
	}
}

// TestMFAConfirmarHandler_SenhaAtualErrada prova a defesa contra sequestro de
// conta (Story 1.11): um access token válido sozinho não basta — a senha
// atual errada devolve 401 INVALID_CREDENTIALS (mesmo vocabulário do
// LoginHandler) e NENHUMA coluna é gravada, mesmo com o código TOTP correto.
func TestMFAConfirmarHandler_SenhaAtualErrada(t *testing.T) {
	db := testDB(t)
	criarUsuarioLogin(t, db, "mfa-confirmar-senha-errada@empresa.com", "senha-123456")
	token := tokenDeLogin(t, db, "mfa-confirmar-senha-errada@empresa.com", "senha-123456")

	wIniciar := postMFAIniciar(db, "Bearer "+token)
	var iniciarBody struct {
		Segredo string `json:"segredo"`
	}
	if err := json.Unmarshal(wIniciar.Body.Bytes(), &iniciarBody); err != nil {
		t.Fatalf("falha ao decodificar /mfa/iniciar: %v", err)
	}

	codigo := codigoTOTPHandlerTeste(t, iniciarBody.Segredo)
	w := postMFAConfirmar(db, "Bearer "+token, `{"segredo":"`+iniciarBody.Segredo+`","codigo":"`+codigo+`","senhaAtual":"senha-errada"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "INVALID_CREDENTIALS" {
		t.Errorf("code = %q, want %q", env.Error.Code, "INVALID_CREDENTIALS")
	}

	var mfaHabilitado bool
	var mfaSecret sql.NullString
	if err := db.QueryRow(`SELECT mfa_habilitado, mfa_secret FROM usuarios WHERE email = 'mfa-confirmar-senha-errada@empresa.com'`).Scan(&mfaHabilitado, &mfaSecret); err != nil {
		t.Fatalf("falha ao reler usuario: %v", err)
	}
	if mfaHabilitado || mfaSecret.Valid {
		t.Error("MFA habilitado/segredo gravado apesar da senha atual incorreta")
	}
}

// TestMFAConfirmarHandler_CodigoErrado prova 400 MFA_CODIGO_INVALIDO sem
// gravar nada.
func TestMFAConfirmarHandler_CodigoErrado(t *testing.T) {
	db := testDB(t)
	criarUsuarioLogin(t, db, "mfa-confirmar-handler-errado@empresa.com", "senha-123456")
	token := tokenDeLogin(t, db, "mfa-confirmar-handler-errado@empresa.com", "senha-123456")

	segredo, err := services.GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP: %v", err)
	}
	w := postMFAConfirmar(db, "Bearer "+token, `{"segredo":"`+segredo+`","codigo":"000000","senhaAtual":"senha-123456"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "MFA_CODIGO_INVALIDO" {
		t.Errorf("code = %q, want %q", env.Error.Code, "MFA_CODIGO_INVALIDO")
	}

	var mfaHabilitado bool
	if err := db.QueryRow(`SELECT mfa_habilitado FROM usuarios WHERE email = 'mfa-confirmar-handler-errado@empresa.com'`).Scan(&mfaHabilitado); err != nil {
		t.Fatalf("falha ao reler usuario: %v", err)
	}
	if mfaHabilitado {
		t.Error("mfa_habilitado = true após código errado")
	}
}

// TestMFAConfirmarHandler_JaConfigurado prova 409 MFA_JA_CONFIGURADO para uma
// conta que já tem MFA habilitado.
func TestMFAConfirmarHandler_JaConfigurado(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioLogin(t, db, "mfa-confirmar-handler-jaconfig@empresa.com", "senha-123456")
	segredoAtual := habilitarMFAConta(t, db, usuarioID)
	token := tokenDeLoginComMFA(t, db, "mfa-confirmar-handler-jaconfig@empresa.com", "senha-123456", segredoAtual)

	novoSegredo, err := services.GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP: %v", err)
	}
	codigo := codigoTOTPHandlerTeste(t, novoSegredo)
	w := postMFAConfirmar(db, "Bearer "+token, `{"segredo":"`+novoSegredo+`","codigo":"`+codigo+`","senhaAtual":"senha-123456"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "MFA_JA_CONFIGURADO" {
		t.Errorf("code = %q, want %q", env.Error.Code, "MFA_JA_CONFIGURADO")
	}
}

// TestMeHandler_ExpoeMfaHabilitadoEOrigem prova que GET /api/auth/me inclui
// mfaHabilitado/origem na resposta (Story 1.11) — o frontend usa os dois para
// decidir o gate de navegação (RotaProtegida, App.tsx).
func TestMeHandler_ExpoeMfaHabilitadoEOrigem(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioLogin(t, db, "me-mfa-origem@empresa.com", "senha-123456")
	segredo := habilitarMFAConta(t, db, usuarioID)
	token := tokenDeLoginComMFA(t, db, "me-mfa-origem@empresa.com", "senha-123456", segredo)

	w := getMe(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	var body struct {
		MfaHabilitado bool   `json:"mfaHabilitado"`
		Origem        string `json:"origem"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("falha ao decodificar /me: %v", err)
	}
	if !body.MfaHabilitado {
		t.Error("mfaHabilitado = false, want true")
	}
	if body.Origem != "senha" {
		t.Errorf("origem = %q, want %q", body.Origem, "senha")
	}
}

// tokenDeLoginComMFA é o molde de tokenDeLogin (usuarios_test.go) para uma
// conta que JÁ tem MFA habilitado com um segredo conhecido — completa o
// segundo fator diretamente, sem precisar reler o segredo do banco.
func tokenDeLoginComMFA(t *testing.T, db *sql.DB, email, senha, segredo string) string {
	t.Helper()
	wLogin := postLogin(db, `{"email":"`+email+`","senha":"`+senha+`"}`)
	if wLogin.Code != http.StatusOK {
		t.Fatalf("login (%s): status = %d, want %d (body=%s)", email, wLogin.Code, http.StatusOK, wLogin.Body.String())
	}
	var loginBody struct {
		MfaToken string `json:"mfaToken"`
	}
	if err := json.Unmarshal(wLogin.Body.Bytes(), &loginBody); err != nil {
		t.Fatalf("falha ao decodificar login: %v", err)
	}
	codigo := codigoTOTPHandlerTeste(t, segredo)
	w := postMFAVerificar(db, `{"mfaToken":"`+loginBody.MfaToken+`","codigo":"`+codigo+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("mfa/verificar (%s): status = %d, want %d (body=%s)", email, w.Code, http.StatusOK, w.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("falha ao decodificar mfa/verificar: %v", err)
	}
	return body.Token
}
