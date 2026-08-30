package handlers

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // HMAC-SHA1 é o algoritmo do TOTP (RFC 6238/4226), não hashing de segredo.
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// criarContaComPapel insere uma conta ativa/verificada com papel e senha
// controlados — criarUsuarioLogin (auth_test.go) só cria papel 'usuario'.
//
// Story 1.11 (FR-37/SM-2): uma conta `gestor`/`adm` autenticada por senha SEM
// MFA configurado é recusada pelo gate de middleware.RequireRole em qualquer
// rota `RequireRole(gestor+)` — exatamente o comportamento que esta story
// introduz. Como a suíte pré-existente usa `criarContaComPapel` +
// `tokenDeLogin` (login real por senha) para exercitar essas rotas com
// intenção de testar OUTRA coisa (escopo de ação, decisão de promoção,
// listagem), não o gate de MFA em si, contas `gestor`/`adm` nascem aqui já
// com `mfa_habilitado=true` e um segredo TOTP real — `tokenDeLogin` completa
// o segundo fator sozinho quando precisa. O gate de MFA em si tem cobertura
// dedicada em middleware/roles_test.go, com controle direto sobre o campo.
func criarContaComPapel(t *testing.T, db *sql.DB, nome, email, senha, papel string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("falha ao gerar hash: %v", err)
	}
	mfaHabilitado := services.RankPapel(papel) >= services.RankPapel(services.PapelGestor)
	var mfaSecret sql.NullString
	if mfaHabilitado {
		segredo, err := services.GerarSegredoTOTP()
		if err != nil {
			t.Fatalf("GerarSegredoTOTP: %v", err)
		}
		mfaSecret = sql.NullString{String: segredo, Valid: true}
	}
	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, mfa_habilitado, mfa_secret)
		VALUES ($1, $2, $3, $4, true, true, $5, $6)
		RETURNING id`
	if err := db.QueryRow(insert, nome, email, string(hash), papel, mfaHabilitado, mfaSecret).Scan(&id); err != nil {
		t.Fatalf("falha ao criar conta %q: %v", email, err)
	}
	return id
}

// habilitarMFATeste liga `mfa_habilitado` com um segredo TOTP real para uma
// conta já existente — usado quando um teste precisa que uma conta ainda
// abaixo de `gestor` JÁ tenha MFA configurado ANTES de uma promoção (Story
// 1.7/1.11): sem isso, uma conta promovida a gestor/adm por senha e sem MFA
// esbarraria no gate de middleware.RequireRole (403 MFA_SETUP_REQUIRED) logo
// na primeira ação restrita após a promoção — um comportamento correto da
// Story 1.11, mas fora do que o teste em questão quer provar (efeito
// imediato da PROMOÇÃO em si).
func habilitarMFATeste(t *testing.T, db *sql.DB, usuarioID string) {
	t.Helper()
	segredo, err := services.GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP: %v", err)
	}
	if _, err := db.Exec(`UPDATE usuarios SET mfa_habilitado = true, mfa_secret = $1 WHERE id = $2`, segredo, usuarioID); err != nil {
		t.Fatalf("falha ao habilitar MFA de teste para %q: %v", usuarioID, err)
	}
}

// totpCodigoTesteAtual gera um código TOTP válido para `segredo` no instante
// atual — mesmo algoritmo HOTP/RFC 6238 de services.ValidarCodigoTOTP,
// reimplementado aqui porque o pacote de teste não acessa o gerador
// não-exportado de services (mesmo padrão de duplicação de
// middleware/auth_test.go, que assina seus próprios JWTs de teste em vez de
// chamar a função não-exportada equivalente de services).
func totpCodigoTesteAtual(t *testing.T, segredo string) string {
	t.Helper()
	chave, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(segredo)
	if err != nil {
		t.Fatalf("segredo TOTP de teste inválido: %v", err)
	}
	contador := uint64(time.Now().UTC().Unix()) / 30
	var contadorBytes [8]byte
	binary.BigEndian.PutUint64(contadorBytes[:], contador)
	mac := hmac.New(sha1.New, chave)
	mac.Write(contadorBytes[:])
	soma := mac.Sum(nil)
	offset := soma[len(soma)-1] & 0x0f
	truncado := (uint32(soma[offset])&0x7f)<<24 |
		uint32(soma[offset+1])<<16 |
		uint32(soma[offset+2])<<8 |
		uint32(soma[offset+3])
	return fmt.Sprintf("%06d", truncado%1000000)
}

// tokenDeLogin faz um login real e devolve o access token — o mesmo caminho
// que o frontend percorre. Story 1.11: se a conta tem `mfa_habilitado=true`
// (`criarContaComPapel` acima, para papel gestor+), a resposta do login vem
// como `mfaRequerido:true` — este helper completa o segundo fator sozinho
// via POST /api/auth/mfa/verificar, lendo o segredo TOTP direto do banco
// (gravado por criarContaComPapel) e computando o código válido do instante
// atual, para sempre devolver um access token de sessão de verdade.
func tokenDeLogin(t *testing.T, db *sql.DB, email, senha string) string {
	t.Helper()
	w := postLogin(db, `{"email":"`+email+`","senha":"`+senha+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("login (%s): status = %d, want %d (body=%s)", email, w.Code, http.StatusOK, w.Body.String())
	}
	var body struct {
		Token        string `json:"token"`
		MfaRequerido bool   `json:"mfaRequerido"`
		MfaToken     string `json:"mfaToken"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("falha ao decodificar login: %v", err)
	}
	if !body.MfaRequerido {
		return body.Token
	}

	var segredo string
	if err := db.QueryRow(`SELECT mfa_secret FROM usuarios WHERE lower(email) = lower($1)`, email).Scan(&segredo); err != nil {
		t.Fatalf("login (%s): mfaRequerido=true mas falha ao ler mfa_secret: %v", email, err)
	}
	codigo := totpCodigoTesteAtual(t, segredo)

	reqVerificar := httptest.NewRequest(http.MethodPost, "/api/auth/mfa/verificar",
		strings.NewReader(`{"mfaToken":"`+body.MfaToken+`","codigo":"`+codigo+`"}`))
	reqVerificar.Header.Set("Content-Type", "application/json")
	wVerificar := httptest.NewRecorder()
	MFAVerificarHandler(db, testJWTSecret)(wVerificar, reqVerificar)
	if wVerificar.Code != http.StatusOK {
		t.Fatalf("mfa/verificar (%s): status = %d, want %d (body=%s)", email, wVerificar.Code, http.StatusOK, wVerificar.Body.String())
	}
	var bodyVerificar struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(wVerificar.Body.Bytes(), &bodyVerificar); err != nil {
		t.Fatalf("falha ao decodificar mfa/verificar: %v", err)
	}
	return bodyVerificar.Token
}

// getUsuarios despacha através da MESMA composição registrada em newMux
// (main.go): RequireAuth -> RequireRole("gestor") -> ListarUsuariosHandler —
// nunca chama o handler isoladamente, para provar o contrato real da rota.
func getUsuarios(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/usuarios", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	middleware.RequireAuth(db, testJWTSecret)(
		middleware.RequireRole(services.PapelGestor)(
			ListarUsuariosHandler(db)))(w, req)
	return w
}

type usuariosResposta struct {
	Usuarios []services.UsuarioResumo `json:"usuarios"`
}

func decodeUsuarios(t *testing.T, body []byte) usuariosResposta {
	t.Helper()
	var resp usuariosResposta
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("falha ao decodificar resposta de usuários: %v (body=%s)", err, body)
	}
	return resp
}

// TestListarUsuariosHandler_403ParaPapelInsuficiente prova a AC1 na fronteira
// HTTP: uma conta `usuario` ou `almoxarife` que chama GET /api/usuarios
// diretamente recebe 403 FORBIDDEN e o handler de listagem nunca executa (o
// corpo é o envelope de erro, nunca `{"usuarios":...}`).
func TestListarUsuariosHandler_403ParaPapelInsuficiente(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Usuária", "listar-usuario@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Almoxarife", "listar-almox@empresa.com", "senha-123456", "almoxarife")

	casos := []struct{ nome, email string }{
		{"papel usuario", "listar-usuario@empresa.com"},
		{"papel almoxarife", "listar-almox@empresa.com"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			token := tokenDeLogin(t, db, c.email, "senha-123456")
			w := getUsuarios(db, "Bearer "+token)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
			}
			env := decodeErro(t, w.Body.Bytes())
			if env.Error.Code != "FORBIDDEN" {
				t.Errorf("code = %q, want %q", env.Error.Code, "FORBIDDEN")
			}
		})
	}
}

// TestListarUsuariosHandler_GestorRecebeRecorte prova a AC1+AC3: um gestor
// recebe 200 e apenas contas `usuario`/`almoxarife`.
func TestListarUsuariosHandler_GestorRecebeRecorte(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Usuária", "recorte-usuario@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Almoxarife", "recorte-almox@empresa.com", "senha-123456", "almoxarife")
	criarContaComPapel(t, db, "Gestora", "recorte-gestor@empresa.com", "senha-123456", "gestor")
	criarContaComPapel(t, db, "Adm", "recorte-adm@empresa.com", "senha-123456", "adm")

	token := tokenDeLogin(t, db, "recorte-gestor@empresa.com", "senha-123456")
	w := getUsuarios(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeUsuarios(t, w.Body.Bytes())
	if len(resp.Usuarios) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(resp.Usuarios), resp.Usuarios)
	}
	for _, u := range resp.Usuarios {
		if u.Papel != "usuario" && u.Papel != "almoxarife" {
			t.Errorf("gestor recebeu conta de papel %q — fora do escopo", u.Papel)
		}
	}
}

// TestListarUsuariosHandler_AdmRecebeTudo prova a AC3: um adm recebe 200 e
// todas as contas, incluindo `gestor`/`adm`.
func TestListarUsuariosHandler_AdmRecebeTudo(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Usuária", "tudo-usuario@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Almoxarife", "tudo-almox@empresa.com", "senha-123456", "almoxarife")
	criarContaComPapel(t, db, "Gestora", "tudo-gestor@empresa.com", "senha-123456", "gestor")
	criarContaComPapel(t, db, "Adm", "tudo-adm@empresa.com", "senha-123456", "adm")

	token := tokenDeLogin(t, db, "tudo-adm@empresa.com", "senha-123456")
	w := getUsuarios(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeUsuarios(t, w.Body.Bytes())
	if len(resp.Usuarios) != 4 {
		t.Fatalf("len = %d, want 4 (%+v)", len(resp.Usuarios), resp.Usuarios)
	}
	papeis := map[string]bool{}
	for _, u := range resp.Usuarios {
		papeis[u.Papel] = true
	}
	for _, p := range []string{"usuario", "almoxarife", "gestor", "adm"} {
		if !papeis[p] {
			t.Errorf("adm não recebeu conta de papel %q", p)
		}
	}
}

// TestListarUsuariosHandler_SemToken401 prova o cenário "sem token em rota
// gestor+" da I/O Matrix: RequireAuth responde 401 TOKEN_EXPIRED antes de
// RequireRole rodar.
func TestListarUsuariosHandler_SemToken401(t *testing.T) {
	db := testDB(t)

	w := getUsuarios(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "TOKEN_EXPIRED")
	}
}
