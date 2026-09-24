package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"stockflow/backend/services"
)

// Story 15.2 (AD-36): POST /api/auth/entrar e /api/auth/entrar/escolha —
// login na raiz do domínio, sem Empresa na URL. Estes testes despacham por
// um mux com os MESMOS padrões de main.go (fora de RequireEmpresa).

const (
	slugEntradaH      = "entrada-h152"
	slugEntradaHTrein = "entrada-h152-treinamento"
)

func limparEmpresasEntradaH(t *testing.T, db *sql.DB) {
	t.Helper()
	removerEmpresaPlataformaHandlers(t, db, slugEntradaHTrein)
	removerEmpresaPlataformaHandlers(t, db, slugEntradaH)
}

func provisionarEmpresaEntradaH(t *testing.T, db *sql.DB, slug, cnpj string, origem *string) services.Empresa {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	e, err := services.ProvisionarEmpresa(tx, services.DadosEmpresa{
		NomeFantasia: "Entrada H", RazaoSocial: "Entrada H LTDA", CNPJ: cnpj, Slug: slug,
		EmpresaOrigemID: origem,
		Endereco: services.EnderecoEmpresa{
			Logradouro: "Rua de Teste", Numero: "152", Bairro: "Centro",
			Cidade: "Recife", CEP: "50000000", UF: "PE",
		},
	})
	if err != nil {
		t.Fatalf("provisionar %s: %v", slug, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return e
}

// prepararEntradaH cria a Empresa real e o Treinamento dela.
func prepararEntradaH(t *testing.T, db *sql.DB) (real, trein services.Empresa) {
	t.Helper()
	limparEmpresasEntradaH(t, db)
	t.Cleanup(func() { limparEmpresasEntradaH(t, db) })
	real = provisionarEmpresaEntradaH(t, db, slugEntradaH, "15225225000113", nil)
	trein = provisionarEmpresaEntradaH(t, db, slugEntradaHTrein, "15225225000202", &real.ID)
	return real, trein
}

func criarContaEntradaH(t *testing.T, db *sql.DB, empresaID, email, senha string, ativo, emailVerificado, mfa bool) string {
	t.Helper()
	var senhaHash sql.NullString
	if senha != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		senhaHash = sql.NullString{String: string(hash), Valid: true}
	}
	var mfaSecret sql.NullString
	if mfa {
		segredo, err := services.GerarSegredoTOTP()
		if err != nil {
			t.Fatal(err)
		}
		mfaSecret = sql.NullString{String: segredo, Valid: true}
	}
	var id string
	if err := db.QueryRow(`
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, mfa_habilitado, mfa_secret, empresa_id)
		VALUES ('Conta Entrada', $1, $2, 'usuario', $3, $4, $5, $6, $7) RETURNING id`,
		email, senhaHash, emailVerificado, ativo, mfa, mfaSecret, empresaID).Scan(&id); err != nil {
		t.Fatalf("criar conta %s: %v", email, err)
	}
	return id
}

func muxEntrada(db *sql.DB) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/entrar", EntrarHandler(db, testJWTSecret))
	mux.HandleFunc("POST /api/auth/entrar/escolha", EntrarEscolhaHandler(db, testJWTSecret))
	return mux
}

func postEntrada(t *testing.T, db *sql.DB, caminho, corpo string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, caminho, strings.NewReader(corpo))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	muxEntrada(db).ServeHTTP(w, req)
	return w
}

type respostaEntrada struct {
	Slug         string `json:"slug"`
	MfaRequerido bool   `json:"mfaRequerido"`
	MfaToken     string `json:"mfaToken"`
	Escolha      []struct {
		Slug         string `json:"slug"`
		NomeFantasia string `json:"nomeFantasia"`
		Treinamento  bool   `json:"treinamento"`
	} `json:"escolha"`
	EscolhaToken string `json:"escolhaToken"`
}

func decodeEntrada(t *testing.T, w *httptest.ResponseRecorder) respostaEntrada {
	t.Helper()
	var r respostaEntrada
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	return r
}

func semCookieRefresh(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == refreshTokenCookieName {
			t.Fatalf("não deveria haver cookie de refresh: %+v", c)
		}
	}
}

func contarLogsEntrada(t *testing.T, db *sql.DB, empresaID string, sucesso bool) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM logs_acesso WHERE empresa_id = $1 AND sucesso = $2 AND metodo = 'senha'`, empresaID, sucesso).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEntrar_UmaContaSemMFA(t *testing.T) {
	db := testDB(t)
	real, _ := prepararEntradaH(t, db)
	id := criarContaEntradaH(t, db, real.ID, "ana@entrada-h.test", "senha-certa-1", true, true, false)

	w := postEntrada(t, db, "/api/auth/entrar", `{"email":"ana@entrada-h.test","senha":"senha-certa-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", w.Code, w.Body.String())
	}
	r := decodeEntrada(t, w)
	if r.Slug != slugEntradaH || r.MfaRequerido || r.EscolhaToken != "" {
		t.Errorf("resposta = %+v", r)
	}
	c := refreshCookieDoResultado(t, w)
	if c.Path != "/e/"+slugEntradaH+"/api/auth" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Secure || c.MaxAge <= 0 {
		t.Errorf("cookie = %+v", c)
	}
	var usuarioSessao string
	if err := db.QueryRow(`SELECT usuario_id FROM sessoes WHERE refresh_token = $1 AND origem = 'senha'`, c.Value).Scan(&usuarioSessao); err != nil || usuarioSessao != id {
		t.Errorf("sessão: usuario=%q err=%v", usuarioSessao, err)
	}
	var usuarioLog sql.NullString
	if err := db.QueryRow(`SELECT usuario_id FROM logs_acesso WHERE empresa_id = $1 AND sucesso`, real.ID).Scan(&usuarioLog); err != nil || usuarioLog.String != id {
		t.Errorf("logs_acesso: usuario=%v err=%v", usuarioLog, err)
	}
}

func TestEntrar_CookieSecureAtrasDeProxyHTTPS(t *testing.T) {
	db := testDB(t)
	real, _ := prepararEntradaH(t, db)
	criarContaEntradaH(t, db, real.ID, "sec@entrada-h.test", "senha-certa-1", true, true, false)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/entrar", strings.NewReader(`{"email":"sec@entrada-h.test","senha":"senha-certa-1"}`))
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	muxEntrada(db).ServeHTTP(w, req)
	if w.Code != http.StatusOK || !refreshCookieDoResultado(t, w).Secure {
		t.Fatalf("status=%d, cookie Secure esperado", w.Code)
	}
}

func TestEntrar_UmaContaComMFA(t *testing.T) {
	db := testDB(t)
	real, _ := prepararEntradaH(t, db)
	id := criarContaEntradaH(t, db, real.ID, "bia@entrada-h.test", "senha-certa-1", true, true, true)

	w := postEntrada(t, db, "/api/auth/entrar", `{"email":"bia@entrada-h.test","senha":"senha-certa-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", w.Code, w.Body.String())
	}
	r := decodeEntrada(t, w)
	if r.Slug != slugEntradaH || !r.MfaRequerido || r.MfaToken == "" {
		t.Errorf("resposta = %+v", r)
	}
	semCookieRefresh(t, w)
	var dono string
	if err := db.QueryRow(`SELECT usuario_id FROM tokens_acao WHERE token = $1 AND tipo = 'mfa_login'`, r.MfaToken).Scan(&dono); err != nil || dono != id {
		t.Errorf("mfa token: dono=%q err=%v", dono, err)
	}
}

func TestEntrar_RealETreinamentoEscolha(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEntradaH(t, db)
	criarContaEntradaH(t, db, real.ID, "caio@entrada-h.test", "senha-certa-1", true, true, false)
	idTrein := criarContaEntradaH(t, db, trein.ID, "caio@entrada-h.test", "senha-certa-1", true, true, false)

	w := postEntrada(t, db, "/api/auth/entrar", `{"email":"caio@entrada-h.test","senha":"senha-certa-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", w.Code, w.Body.String())
	}
	semCookieRefresh(t, w)
	r := decodeEntrada(t, w)
	if r.Slug != "" || r.EscolhaToken == "" || len(r.Escolha) != 2 ||
		r.Escolha[0].Slug != slugEntradaH || r.Escolha[0].Treinamento || r.Escolha[0].NomeFantasia != "Entrada H" ||
		r.Escolha[1].Slug != slugEntradaHTrein || !r.Escolha[1].Treinamento {
		t.Fatalf("resposta = %+v", r)
	}
	if contarLogsEntrada(t, db, real.ID, true) != 1 || contarLogsEntrada(t, db, trein.ID, true) != 1 {
		t.Errorf("esperava uma linha de sucesso em cada Empresa")
	}

	w2 := postEntrada(t, db, "/api/auth/entrar/escolha", `{"escolhaToken":"`+r.EscolhaToken+`","slug":"`+slugEntradaHTrein+`"}`)
	if w2.Code != http.StatusOK {
		t.Fatalf("escolha: status = %d (body=%s)", w2.Code, w2.Body.String())
	}
	if r2 := decodeEntrada(t, w2); r2.Slug != slugEntradaHTrein {
		t.Errorf("escolha: resposta = %+v", r2)
	}
	c := refreshCookieDoResultado(t, w2)
	if c.Path != "/e/"+slugEntradaHTrein+"/api/auth" {
		t.Errorf("cookie path = %q", c.Path)
	}
	var dono string
	if err := db.QueryRow(`SELECT usuario_id FROM sessoes WHERE refresh_token = $1`, c.Value).Scan(&dono); err != nil || dono != idTrein {
		t.Errorf("sessão: dono=%q err=%v", dono, err)
	}

	// Reuso do mesmo token.
	w3 := postEntrada(t, db, "/api/auth/entrar/escolha", `{"escolhaToken":"`+r.EscolhaToken+`","slug":"`+slugEntradaH+`"}`)
	if w3.Code != http.StatusUnauthorized || decodeErro(t, w3.Body.Bytes()).Error.Code != "ESCOLHA_INVALIDA" {
		t.Fatalf("reuso: status=%d body=%s", w3.Code, w3.Body.String())
	}
	semCookieRefresh(t, w3)
}

func TestEntrar_EscolhaComMFA(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEntradaH(t, db)
	criarContaEntradaH(t, db, real.ID, "mfa2@entrada-h.test", "senha-certa-1", true, true, true)
	criarContaEntradaH(t, db, trein.ID, "mfa2@entrada-h.test", "senha-certa-1", true, true, false)

	r := decodeEntrada(t, postEntrada(t, db, "/api/auth/entrar", `{"email":"mfa2@entrada-h.test","senha":"senha-certa-1"}`))
	w := postEntrada(t, db, "/api/auth/entrar/escolha", `{"escolhaToken":"`+r.EscolhaToken+`","slug":"`+slugEntradaH+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", w.Code, w.Body.String())
	}
	semCookieRefresh(t, w)
	if r2 := decodeEntrada(t, w); r2.Slug != slugEntradaH || !r2.MfaRequerido || r2.MfaToken == "" {
		t.Errorf("resposta = %+v", r2)
	}
}

func TestEntrarEscolha_Invalida(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEntradaH(t, db)
	criarContaEntradaH(t, db, real.ID, "dani@entrada-h.test", "senha-certa-1", true, true, false)
	criarContaEntradaH(t, db, trein.ID, "dani@entrada-h.test", "senha-certa-1", true, true, false)

	novoToken := func() string {
		t.Helper()
		return decodeEntrada(t, postEntrada(t, db, "/api/auth/entrar", `{"email":"dani@entrada-h.test","senha":"senha-certa-1"}`)).EscolhaToken
	}
	esperar401 := func(t *testing.T, w *httptest.ResponseRecorder) {
		t.Helper()
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d (body=%s)", w.Code, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "ESCOLHA_INVALIDA" || env.Error.Message != "A escolha expirou. Faça login novamente." {
			t.Errorf("erro = %+v", env.Error)
		}
		semCookieRefresh(t, w)
	}

	t.Run("slug fora das contas", func(t *testing.T) {
		esperar401(t, postEntrada(t, db, "/api/auth/entrar/escolha", `{"escolhaToken":"`+novoToken()+`","slug":"`+slugEmpresaTeste+`"}`))
	})
	t.Run("vencido", func(t *testing.T) {
		token := novoToken()
		if _, err := db.Exec(`UPDATE tokens_acao SET expira_em = now() - interval '1 second' WHERE token = $1`, token); err != nil {
			t.Fatal(err)
		}
		esperar401(t, postEntrada(t, db, "/api/auth/entrar/escolha", `{"escolhaToken":"`+token+`","slug":"`+slugEntradaH+`"}`))
	})
	t.Run("sem token", func(t *testing.T) {
		esperar401(t, postEntrada(t, db, "/api/auth/entrar/escolha", `{"slug":"`+slugEntradaH+`"}`))
	})
	t.Run("payload inválido", func(t *testing.T) {
		if w := postEntrada(t, db, "/api/auth/entrar/escolha", `{isto nao e json`); w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestEntrar_SenhaCertaSoNuma(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEntradaH(t, db)
	criarContaEntradaH(t, db, real.ID, "edu@entrada-h.test", "senha-real-1", true, true, false)
	criarContaEntradaH(t, db, trein.ID, "edu@entrada-h.test", "senha-trein-1", true, true, false)

	w := postEntrada(t, db, "/api/auth/entrar", `{"email":"edu@entrada-h.test","senha":"senha-real-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", w.Code, w.Body.String())
	}
	if r := decodeEntrada(t, w); r.Slug != slugEntradaH || len(r.Escolha) != 0 {
		t.Errorf("resposta = %+v", r)
	}
	if refreshCookieDoResultado(t, w).Path != "/e/"+slugEntradaH+"/api/auth" {
		t.Error("cookie deveria ser da Empresa real")
	}
	// A conta do Treinamento (senha diferente) não é avaliada: nenhuma linha
	// em logs_acesso e nenhuma falha — como no login pelo endereço da Empresa.
	if contarLogsEntrada(t, db, real.ID, true) != 1 {
		t.Error("esperava uma linha de sucesso na Empresa real")
	}
	var linhasTrein int
	if err := db.QueryRow(`SELECT count(*) FROM logs_acesso WHERE empresa_id = $1`, trein.ID).Scan(&linhasTrein); err != nil || linhasTrein != 0 {
		t.Errorf("linhas em logs_acesso do Treinamento = %d err=%v, want 0", linhasTrein, err)
	}
	var falhas int
	if err := db.QueryRow(`SELECT tentativas_login_falhas FROM usuarios WHERE empresa_id = $1`, trein.ID).Scan(&falhas); err != nil || falhas != 0 {
		t.Errorf("falhas no Treinamento = %d err=%v, want 0", falhas, err)
	}
}

// Caso misto: conta real bloqueada, Treinamento livre, senha certa nas duas
// -> entra direto no Treinamento (200, cookie da Empresa dele), sem 429.
func TestEntrar_RealBloqueadaTreinamentoLivre(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEntradaH(t, db)
	idReal := criarContaEntradaH(t, db, real.ID, "iris@entrada-h.test", "senha-certa-1", true, true, false)
	idTrein := criarContaEntradaH(t, db, trein.ID, "iris@entrada-h.test", "senha-certa-1", true, true, false)
	if _, err := db.Exec(`UPDATE usuarios SET tentativas_login_falhas = 5, bloqueado_ate = now() + interval '15 minutes' WHERE id = $1`, idReal); err != nil {
		t.Fatal(err)
	}

	w := postEntrada(t, db, "/api/auth/entrar", `{"email":"iris@entrada-h.test","senha":"senha-certa-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", w.Code, w.Body.String())
	}
	if r := decodeEntrada(t, w); r.Slug != slugEntradaHTrein || len(r.Escolha) != 0 {
		t.Errorf("resposta = %+v", r)
	}
	c := refreshCookieDoResultado(t, w)
	if c.Path != "/e/"+slugEntradaHTrein+"/api/auth" {
		t.Errorf("cookie path = %q", c.Path)
	}
	var dono string
	if err := db.QueryRow(`SELECT usuario_id FROM sessoes WHERE refresh_token = $1`, c.Value).Scan(&dono); err != nil || dono != idTrein {
		t.Errorf("sessão: dono=%q err=%v", dono, err)
	}
}

func TestEntrar_CredenciaisInvalidasCorpoIdentico(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEntradaH(t, db)
	criarContaEntradaH(t, db, real.ID, "fabi@entrada-h.test", "senha-certa-1", true, true, false)
	criarContaEntradaH(t, db, real.ID, "desativada@entrada-h.test", "senha-certa-1", false, true, false)
	criarContaEntradaH(t, db, real.ID, "naoconfirmada@entrada-h.test", "senha-certa-1", true, false, false)
	criarContaEntradaH(t, db, real.ID, "sso@entrada-h.test", "", true, true, false)
	criarContaEntradaH(t, db, trein.ID, "inativa@entrada-h.test", "senha-certa-1", true, true, false)
	if _, err := db.Exec(`UPDATE empresas SET status = 'inativa' WHERE id = $1`, trein.ID); err != nil {
		t.Fatal(err)
	}

	semConta := postEntrada(t, db, "/api/auth/entrar", `{"email":"ninguem@entrada-h.test","senha":"senha-certa-1"}`)
	if semConta.Code != http.StatusUnauthorized {
		t.Fatalf("sem conta: status = %d", semConta.Code)
	}
	env := decodeErro(t, semConta.Body.Bytes())
	if env.Error.Code != "INVALID_CREDENTIALS" || env.Error.Message != "E-mail ou senha inválidos." {
		t.Errorf("sem conta: erro = %+v", env.Error)
	}
	if n := contarLogsEntrada(t, db, real.ID, false) + contarLogsEntrada(t, db, trein.ID, false); n != 0 {
		t.Errorf("sem conta não deveria gravar logs_acesso (n=%d)", n)
	}

	for _, c := range []struct{ nome, corpo string }{
		{"senha errada", `{"email":"fabi@entrada-h.test","senha":"senha-errada-1"}`},
		{"desativada", `{"email":"desativada@entrada-h.test","senha":"senha-certa-1"}`},
		{"não confirmada", `{"email":"naoconfirmada@entrada-h.test","senha":"senha-certa-1"}`},
		{"só-SSO", `{"email":"sso@entrada-h.test","senha":"senha-certa-1"}`},
		{"Empresa inativa", `{"email":"inativa@entrada-h.test","senha":"senha-certa-1"}`},
	} {
		t.Run(c.nome, func(t *testing.T) {
			w := postEntrada(t, db, "/api/auth/entrar", c.corpo)
			if w.Code != http.StatusUnauthorized || w.Body.String() != semConta.Body.String() {
				t.Errorf("status=%d body=%s, want 401 idêntico a %s", w.Code, w.Body.String(), semConta.Body.String())
			}
			semCookieRefresh(t, w)
		})
	}
}

func TestEntrar_Validacao(t *testing.T) {
	db := testDB(t)
	for _, corpo := range []string{`{isto nao e json`, `{"email":"","senha":"x"}`, `{"email":"a@b.c","senha":"   "}`} {
		if w := postEntrada(t, db, "/api/auth/entrar", corpo); w.Code != http.StatusBadRequest || decodeErro(t, w.Body.Bytes()).Error.Code != "VALIDATION_ERROR" {
			t.Errorf("%s: status=%d body=%s", corpo, w.Code, w.Body.String())
		}
	}
	grande := `{"email":"a@b.c","senha":"` + strings.Repeat("x", authRequestMaxBytes) + `"}`
	if w := postEntrada(t, db, "/api/auth/entrar", grande); w.Code != http.StatusBadRequest {
		t.Errorf("payload grande: status = %d", w.Code)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM logs_acesso WHERE email_informado IN ('', 'a@b.c')`).Scan(&n); err != nil || n != 0 {
		t.Errorf("validação não deveria gravar logs_acesso (n=%d err=%v)", n, err)
	}
}

func TestEntrar_Bloqueio(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEntradaH(t, db)
	criarContaEntradaH(t, db, real.ID, "gabi@entrada-h.test", "senha-certa-1", true, true, false)
	criarContaEntradaH(t, db, trein.ID, "gabi@entrada-h.test", "senha-certa-1", true, true, false)

	for i := 1; i <= 5; i++ {
		if w := postEntrada(t, db, "/api/auth/entrar", `{"email":"gabi@entrada-h.test","senha":"senha-errada-1"}`); w.Code != http.StatusUnauthorized {
			t.Fatalf("tentativa %d: status = %d", i, w.Code)
		}
	}
	for _, id := range []string{real.ID, trein.ID} {
		var falhas, comUsuario int
		if err := db.QueryRow(`SELECT count(*), count(usuario_id) FROM logs_acesso WHERE empresa_id = $1 AND NOT sucesso`, id).Scan(&falhas, &comUsuario); err != nil {
			t.Fatal(err)
		}
		if falhas != 5 || comUsuario != 0 {
			t.Errorf("empresa %s: falhas=%d com usuario_id=%d, want 5/0", id, falhas, comUsuario)
		}
	}
	w := postEntrada(t, db, "/api/auth/entrar", `{"email":"gabi@entrada-h.test","senha":"senha-certa-1"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("6ª: status = %d (body=%s)", w.Code, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "ACCOUNT_LOCKED" || env.Error.Message != mensagemContaBloqueada {
		t.Errorf("erro = %+v", env.Error)
	}
	semCookieRefresh(t, w)

	for _, slug := range []string{slugEntradaH, slugEntradaHTrein} {
		req := httptest.NewRequest(http.MethodPost, "/e/"+slug+"/api/auth/login", strings.NewReader(`{"email":"gabi@entrada-h.test","senha":"senha-certa-1"}`))
		req.SetPathValue("slug", slug)
		wl := httptest.NewRecorder()
		comEmpresa(db, LoginHandler(db, testJWTSecret))(wl, req)
		if wl.Code != http.StatusTooManyRequests {
			t.Errorf("login por Empresa %s: status = %d", slug, wl.Code)
		}
	}
}
