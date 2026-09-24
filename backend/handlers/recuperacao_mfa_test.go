package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// ===== Story 14.4: recuperação de MFA — reset por adm e desligamento =====

// postMFAReset despacha pela MESMA composição de newMux (main.go), com um
// ServeMux local para exercitar `r.PathValue("id")`:
// RequireEmpresa -> RequireAuth -> RequireRole(adm) -> handler.
func postMFAReset(db *sql.DB, id, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /e/{slug}/api/usuarios/{id}/mfa-reset",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(
				middleware.RequireRole(services.PapelAdm)(
					ResetarMFAUsuarioHandler(db)))))
	r := httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/usuarios/"+id+"/mfa-reset", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// postMFADesligar despacha por RequireEmpresa -> RequireAuth -> handler (sem
// RequireRole), como em main.go.
func postMFADesligar(db *sql.DB, authHeader, corpo string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/auth/mfa/desligar", strings.NewReader(corpo))
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	comEmpresa(db, middleware.RequireAuth(db, testJWTSecret)(MFADesligarHandler(db)))(w, req)
	return w
}

// rotaGestorTeste chama uma rota RequireRole(gestor) — a mesma composição de
// TestRequireRole_GateCondicionalAEmpresa — e devolve a resposta.
func rotaGestorTeste(db *sql.DB, token string) *httptest.ResponseRecorder {
	rota := comEmpresa(db, middleware.RequireAuth(db, testJWTSecret)(middleware.RequireRole(services.PapelGestor)(
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })))
	req := httptest.NewRequest(http.MethodGet, prefixoEmpresaTeste+"/api/usuarios", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	rota(w, req)
	return w
}

func corpoDesligar(senha, codigo string) string {
	return fmt.Sprintf(`{"senhaAtual":%q,"codigo":%q}`, senha, codigo)
}

// codigoInvalidoHandlerTeste devolve um código de 6 dígitos que NÃO é aceito
// para `segredo` agora (evita o raro acaso de "000000" ser válido).
func codigoInvalidoHandlerTeste(t *testing.T, segredo string) string {
	t.Helper()
	for _, c := range []string{"000000", "111111", "222222", "333333"} {
		if !services.ValidarCodigoTOTP(segredo, c) {
			return c
		}
	}
	t.Fatal("não achou código inválido")
	return ""
}

type estadoMFAHandler struct {
	habilitado bool
	segredo    sql.NullString
	passo      sql.NullInt64
	tentativas int
	bloqueado  sql.NullTime
}

func lerEstadoMFAHandler(t *testing.T, db *sql.DB, id string) estadoMFAHandler {
	t.Helper()
	var e estadoMFAHandler
	if err := db.QueryRow(`
		SELECT mfa_habilitado, mfa_secret, mfa_ultimo_passo_usado, tentativas_login_falhas, bloqueado_ate
		FROM usuarios WHERE id = $1`, id,
	).Scan(&e.habilitado, &e.segredo, &e.passo, &e.tentativas, &e.bloqueado); err != nil {
		t.Fatalf("ler estado MFA: %v", err)
	}
	return e
}

// ultimaAuditoria devolve quantas linhas `acao` existem na Empresa padrão e o
// ator/alvo da mais recente.
func ultimaAuditoria(t *testing.T, db *sql.DB, acao string) (n int, ator, alvo string) {
	t.Helper()
	if err := db.QueryRow(`SELECT count(*) FROM auditoria_seguranca WHERE empresa_id = $1 AND acao = $2`, empresaTeste, acao).Scan(&n); err != nil {
		t.Fatalf("contar auditoria: %v", err)
	}
	if n > 0 {
		var a sql.NullString
		if err := db.QueryRow(`SELECT ator_id, alvo_id FROM auditoria_seguranca WHERE empresa_id = $1 AND acao = $2 ORDER BY criado_em DESC LIMIT 1`,
			empresaTeste, acao).Scan(&ator, &a); err != nil {
			t.Fatalf("ler auditoria: %v", err)
		}
		alvo = a.String
	}
	return n, ator, alvo
}

func esperarErro(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, status, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != code {
		t.Errorf("code = %q, want %q", env.Error.Code, code)
	}
}

// --- POST /api/usuarios/{id}/mfa-reset ----------------------------------------

// resetarGestorTeste cria um adm e um gestor com MFA (e uma sessão viva do
// gestor), reseta o MFA do gestor e devolve o id do gestor.
func resetarGestorTeste(t *testing.T, db *sql.DB, prefixo string) (admID, gestorID string) {
	t.Helper()
	admID = criarContaComPapel(t, db, "Adm", prefixo+"-adm@empresa.com", "senha-123456", "adm")
	gestorID = criarContaComPapel(t, db, "Gestor", prefixo+"-gestor@empresa.com", "senha-123456", "gestor")
	admToken := tokenDeLogin(t, db, prefixo+"-adm@empresa.com", "senha-123456")
	tokenDeLogin(t, db, prefixo+"-gestor@empresa.com", "senha-123456")
	if n := sessoesVivasDe(t, db, gestorID); n != 1 {
		t.Fatalf("sessões vivas do gestor antes do reset = %d, want 1", n)
	}

	w := postMFAReset(db, gestorID, "Bearer "+admToken)
	if w.Code != http.StatusOK {
		t.Fatalf("reset: status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	b := decodeUsuarioGestao(t, w.Body.Bytes())
	if b.Usuario == nil || b.Usuario.ID != gestorID || b.Usuario.MFAHabilitado {
		t.Fatalf("resposta = %s, want usuario %s com mfaHabilitado=false", w.Body.String(), gestorID)
	}
	if !strings.Contains(w.Body.String(), `"mfaHabilitado":false`) {
		t.Errorf("corpo sem mfaHabilitado:false: %s", w.Body.String())
	}
	return admID, gestorID
}

func TestResetarMFAHandler_Sucesso(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	admID, gestorID := resetarGestorTeste(t, db, "rmfa-ok")

	e := lerEstadoMFAHandler(t, db, gestorID)
	if e.habilitado || e.segredo.Valid || e.passo.Valid {
		t.Errorf("colunas MFA não zeradas: %+v", e)
	}
	if n := sessoesVivasDe(t, db, gestorID); n != 0 {
		t.Errorf("sessões vivas do gestor = %d, want 0", n)
	}
	n, ator, alvo := ultimaAuditoria(t, db, services.AcaoMFAResetado)
	if n != 1 || ator != admID || alvo != gestorID {
		t.Errorf("auditoria = (%d, %s, %s), want (1, %s, %s)", n, ator, alvo, admID, gestorID)
	}
}

func TestResetarMFAHandler_ReentradaEmpresaExige(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	resetarGestorTeste(t, db, "rmfa-exige")
	definirMFAObrigatorioEmpresaTeste(t, db, true)

	w := postLogin(db, `{"email":"rmfa-exige-gestor@empresa.com","senha":"senha-123456"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("login: status = %d (body=%s)", w.Code, w.Body.String())
	}
	var body struct {
		Token        string `json:"token"`
		MfaRequerido bool   `json:"mfaRequerido"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.MfaRequerido || body.Token == "" {
		t.Fatalf("login após reset = %s, want sessão sem mfaRequerido", w.Body.String())
	}
	esperarErro(t, rotaGestorTeste(db, body.Token), http.StatusForbidden, "MFA_SETUP_REQUIRED")
}

func TestResetarMFAHandler_ReentradaEmpresaNaoExige(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	resetarGestorTeste(t, db, "rmfa-naoexige")

	token := tokenDeLogin(t, db, "rmfa-naoexige-gestor@empresa.com", "senha-123456")
	if w := rotaGestorTeste(db, token); w.Code != http.StatusOK {
		t.Fatalf("rota gestor: status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
}

func TestResetarMFAHandler_AlvoAdm(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	// O índice idx_usuarios_unico_adm admite um único `adm` por Empresa: o
	// caso "adm sobre adm" na fronteira HTTP é o adm sobre si mesmo (a regra
	// de rank igual tem cobertura de service com dois gestor).
	admID := criarContaComPapel(t, db, "Adm", "rmfa-self-adm@empresa.com", "senha-123456", "adm")
	token := tokenDeLogin(t, db, "rmfa-self-adm@empresa.com", "senha-123456")

	esperarErro(t, postMFAReset(db, admID, "Bearer "+token), http.StatusForbidden, "FORBIDDEN")
	if !lerEstadoMFAHandler(t, db, admID).habilitado {
		t.Error("MFA do adm foi alterado")
	}
	if n := contarAuditoriaSeguranca(t, db, empresaTeste); n != 0 {
		t.Errorf("auditoria = %d, want 0", n)
	}
}

func TestResetarMFAHandler_ForaDeEscopo404(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	criarContaComPapel(t, db, "Adm", "rmfa-404-adm@empresa.com", "senha-123456", "adm")
	token := tokenDeLogin(t, db, "rmfa-404-adm@empresa.com", "senha-123456")

	const slugAlheia = "rmfa-empresa-alheia"
	removerEmpresaPlataformaHandlers(t, db, slugAlheia)
	t.Cleanup(func() { removerEmpresaPlataformaHandlers(t, db, slugAlheia) })
	var alheiaID, alvoAlheio string
	if err := db.QueryRow(
		`INSERT INTO empresas (nome_fantasia, razao_social, cnpj, logradouro, numero, bairro, cidade, cep, uf, slug)
		 VALUES ('RMFA Alheia', 'RMFA Alheia LTDA', '99888777014400', 'Rua', '1', 'Centro', 'Recife', '50000000', 'PE', $1)
		 RETURNING id`, slugAlheia,
	).Scan(&alheiaID); err != nil {
		t.Fatalf("criar empresa alheia: %v", err)
	}
	if err := db.QueryRow(`
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, mfa_habilitado, mfa_secret, empresa_id)
		VALUES ('Alheio', 'rmfa-404-alheio@empresa.com', 'x', 'gestor', true, true, true, 'SEGREDO', $1) RETURNING id`,
		alheiaID).Scan(&alvoAlheio); err != nil {
		t.Fatalf("criar conta alheia: %v", err)
	}

	for nome, id := range map[string]string{
		"outra Empresa": alvoAlheio,
		"inexistente":   "00000000-0000-0000-0000-000000000000",
		"não-UUID":      "abc",
	} {
		t.Run(nome, func(t *testing.T) {
			esperarErro(t, postMFAReset(db, id, "Bearer "+token), http.StatusNotFound, "NOT_FOUND")
		})
	}
	if !lerEstadoMFAHandler(t, db, alvoAlheio).habilitado {
		t.Error("MFA da conta alheia foi alterado")
	}
}

func TestResetarMFAHandler_PapelAbaixoDeAdm403(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	alvoID := criarContaComPapel(t, db, "Almox", "rmfa-papel-alvo@empresa.com", "senha-123456", "almoxarife")
	habilitarMFAConta(t, db, alvoID)
	criarContaComPapel(t, db, "Gestor", "rmfa-papel-gestor@empresa.com", "senha-123456", "gestor")
	criarUsuarioLogin(t, db, "rmfa-papel-usuario@empresa.com", "senha-123456")

	for _, email := range []string{"rmfa-papel-gestor@empresa.com", "rmfa-papel-usuario@empresa.com"} {
		token := tokenDeLogin(t, db, email, "senha-123456")
		esperarErro(t, postMFAReset(db, alvoID, "Bearer "+token), http.StatusForbidden, "FORBIDDEN")
	}
	if !lerEstadoMFAHandler(t, db, alvoID).habilitado {
		t.Error("MFA do alvo foi alterado")
	}
}

func TestResetarMFAHandler_AlvoSemMFA(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	criarContaComPapel(t, db, "Adm", "rmfa-semmfa-adm@empresa.com", "senha-123456", "adm")
	alvoID := criarUsuarioLogin(t, db, "rmfa-semmfa-alvo@empresa.com", "senha-123456")
	token := tokenDeLogin(t, db, "rmfa-semmfa-adm@empresa.com", "senha-123456")

	esperarErro(t, postMFAReset(db, alvoID, "Bearer "+token), http.StatusConflict, "MFA_NAO_CONFIGURADO")
	if n := contarAuditoriaSeguranca(t, db, empresaTeste); n != 0 {
		t.Errorf("auditoria = %d, want 0", n)
	}
}

// --- POST /api/auth/mfa/desligar ----------------------------------------------

// contaComMFAParaDesligar cria uma conta por senha no papel pedido, faz login
// SEM MFA (para não consumir o passo TOTP atual) e só depois liga o MFA —
// RequireAuth relê `mfa_habilitado` a cada requisição.
func contaComMFAParaDesligar(t *testing.T, db *sql.DB, email, papel string) (id, token, segredo string) {
	t.Helper()
	id, token = contaSenhaSemMFA(t, db, email, papel)
	segredo = habilitarMFAConta(t, db, id)
	return id, token, segredo
}

func TestMFADesligarHandler_Sucesso(t *testing.T) {
	for _, papel := range []string{"usuario", "gestor"} {
		t.Run(papel, func(t *testing.T) {
			db := testDB(t)
			prepararSegurancaEmpresaTeste(t, db)
			id, token, segredo := contaComMFAParaDesligar(t, db, "dmfa-ok-"+papel+"@empresa.com", papel)

			w := postMFADesligar(db, "Bearer "+token, corpoDesligar("senha-123456", codigoTOTPHandlerTeste(t, segredo)))
			if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "{}" {
				t.Fatalf("status = %d body=%s, want 200 {}", w.Code, w.Body.String())
			}
			e := lerEstadoMFAHandler(t, db, id)
			if e.habilitado || e.segredo.Valid || e.passo.Valid {
				t.Errorf("colunas MFA não zeradas: %+v", e)
			}
			n, ator, alvo := ultimaAuditoria(t, db, services.AcaoMFADesligado)
			if n != 1 || ator != id || alvo != id {
				t.Errorf("auditoria = (%d, %s, %s), want (1, %s, %s)", n, ator, alvo, id, id)
			}
			if w := getMe(db, "Bearer "+token); w.Code != http.StatusOK {
				t.Errorf("/me após desligar: status = %d, want 200 (sessão continua válida)", w.Code)
			}
		})
	}
}

func TestMFADesligarHandler_SenhaOuCodigoErrado(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	id, token, segredo := contaComMFAParaDesligar(t, db, "dmfa-erro@empresa.com", "usuario")

	w := postMFADesligar(db, "Bearer "+token, corpoDesligar("senha-errada1", codigoTOTPHandlerTeste(t, segredo)))
	esperarErro(t, w, http.StatusUnauthorized, "INVALID_CREDENTIALS")
	msgSenha := decodeErro(t, w.Body.Bytes()).Error.Message
	if e := lerEstadoMFAHandler(t, db, id); !e.habilitado || e.tentativas != 1 {
		t.Errorf("após senha errada: %+v, want MFA intacto e tentativas=1", e)
	}

	w = postMFADesligar(db, "Bearer "+token, corpoDesligar("senha-123456", codigoInvalidoHandlerTeste(t, segredo)))
	esperarErro(t, w, http.StatusUnauthorized, "INVALID_CREDENTIALS")
	if msg := decodeErro(t, w.Body.Bytes()).Error.Message; msg != msgSenha {
		t.Errorf("mensagens diferentes para senha (%q) e código (%q) — oráculo", msgSenha, msg)
	}
	if e := lerEstadoMFAHandler(t, db, id); !e.habilitado || e.tentativas != 2 {
		t.Errorf("após código errado: %+v, want MFA intacto e tentativas=2", e)
	}
	if n := contarAuditoriaSeguranca(t, db, empresaTeste); n != 0 {
		t.Errorf("auditoria = %d, want 0", n)
	}
}

func TestMFADesligarHandler_Bloqueio(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	id, token, segredo := contaComMFAParaDesligar(t, db, "dmfa-bloqueio@empresa.com", "usuario")

	for i := 1; i <= 5; i++ {
		w := postMFADesligar(db, "Bearer "+token, corpoDesligar("senha-errada1", codigoTOTPHandlerTeste(t, segredo)))
		esperarErro(t, w, http.StatusUnauthorized, "INVALID_CREDENTIALS")
	}
	if e := lerEstadoMFAHandler(t, db, id); !e.bloqueado.Valid {
		t.Fatalf("5ª falha não gravou bloqueado_ate: %+v", e)
	}
	w := postMFADesligar(db, "Bearer "+token, corpoDesligar("senha-123456", codigoTOTPHandlerTeste(t, segredo)))
	esperarErro(t, w, http.StatusTooManyRequests, "ACCOUNT_LOCKED")
	if !lerEstadoMFAHandler(t, db, id).habilitado {
		t.Error("MFA desligado apesar do bloqueio")
	}
}

func TestMFADesligarHandler_ExigidoPelaEmpresa(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	id, token, segredo := contaComMFAParaDesligar(t, db, "dmfa-exigido@empresa.com", "gestor")
	definirMFAObrigatorioEmpresaTeste(t, db, true)

	w := postMFADesligar(db, "Bearer "+token, corpoDesligar("senha-123456", codigoTOTPHandlerTeste(t, segredo)))
	esperarErro(t, w, http.StatusConflict, "MFA_EXIGIDO_PELA_EMPRESA")
	if msg := decodeErro(t, w.Body.Bytes()).Error.Message; msg != "A Empresa exige dupla autenticação para o seu papel; ela não pode ser desligada." {
		t.Errorf("mensagem = %q", msg)
	}
	if e := lerEstadoMFAHandler(t, db, id); !e.habilitado || e.tentativas != 0 {
		t.Errorf("estado = %+v, want MFA intacto e tentativas=0", e)
	}
}

func TestMFADesligarHandler_SemMFAEPayloadInvalido(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	_, token := contaSenhaSemMFA(t, db, "dmfa-semmfa@empresa.com", "usuario")

	esperarErro(t, postMFADesligar(db, "Bearer "+token, corpoDesligar("senha-123456", "123456")), http.StatusConflict, "MFA_NAO_CONFIGURADO")
	esperarErro(t, postMFADesligar(db, "Bearer "+token, "não-json"), http.StatusBadRequest, "VALIDATION_ERROR")
}

func TestMFADesligarHandler_CamposVaziosNaoContamTentativa(t *testing.T) {
	db := testDB(t)
	prepararSegurancaEmpresaTeste(t, db)
	id, token, segredo := contaComMFAParaDesligar(t, db, "dmfa-vazio@empresa.com", "usuario")

	for _, corpo := range []string{
		corpoDesligar("", codigoTOTPHandlerTeste(t, segredo)),
		corpoDesligar("senha-123456", "   "),
		`{}`,
	} {
		esperarErro(t, postMFADesligar(db, "Bearer "+token, corpo), http.StatusBadRequest, "VALIDATION_ERROR")
	}
	if e := lerEstadoMFAHandler(t, db, id); !e.habilitado || e.tentativas != 0 {
		t.Errorf("estado = %+v, want MFA intacto e tentativas=0", e)
	}
}

func TestRecuperacaoMFA_SemToken401(t *testing.T) {
	db := testDB(t)
	if w := postMFADesligar(db, "", corpoDesligar("x", "123456")); w.Code != http.StatusUnauthorized {
		t.Errorf("desligar sem token: status = %d, want 401", w.Code)
	}
	if w := postMFAReset(db, "00000000-0000-0000-0000-000000000000", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("mfa-reset sem token: status = %d, want 401", w.Code)
	}
}

// --- Redefinição de senha não mexe no MFA --------------------------------------

func TestRedefinirSenha_PreservaMFA(t *testing.T) {
	db := testDB(t)
	id, token := seedTokenRedefinicao(t, db, "rmfa-redefinir@empresa.com")
	habilitarMFAConta(t, db, id)

	if w := postRedefinirSenha(db, `{"token":"`+token+`","senha":"nova-senha1"}`); w.Code != http.StatusOK {
		t.Fatalf("redefinir: status = %d (body=%s)", w.Code, w.Body.String())
	}
	if !lerEstadoMFAHandler(t, db, id).habilitado {
		t.Fatal("redefinição de senha desligou o MFA")
	}
	w := postLogin(db, `{"email":"rmfa-redefinir@empresa.com","senha":"nova-senha1"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"mfaRequerido":true`) {
		t.Fatalf("login após redefinir: status = %d body=%s, want mfaRequerido:true", w.Code, w.Body.String())
	}
}
