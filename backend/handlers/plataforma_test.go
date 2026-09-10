package handlers

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // HMAC-SHA1 é o algoritmo do TOTP (RFC 6238/4226), não hashing de segredo.
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// Testes da fronteira HTTP do Dono da Plataforma — Story 9.2, spec-9-2:
// login/refresh/logout (cookie e Path) e a área "Empresas".

const senhaDonoHandlers = "senha-dono-123"

var segredoJWTPlataformaHandlers = []byte("segredo-plataforma-handlers")

// codigoTOTPPlataformaTeste gera o código TOTP válido de `segredo` agora —
// mesmo algoritmo de services.ValidarCodigoTOTP (o gerador de lá não é
// exportado).
func codigoTOTPPlataformaTeste(t *testing.T, segredo string) string {
	t.Helper()
	chave, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(segredo)
	if err != nil {
		t.Fatalf("segredo TOTP inválido: %v", err)
	}
	var contador [8]byte
	binary.BigEndian.PutUint64(contador[:], uint64(time.Now().UTC().Unix())/30)
	mac := hmac.New(sha1.New, chave)
	mac.Write(contador[:])
	soma := mac.Sum(nil)
	o := soma[len(soma)-1] & 0x0f
	v := (uint32(soma[o])&0x7f)<<24 | uint32(soma[o+1])<<16 | uint32(soma[o+2])<<8 | uint32(soma[o+3])
	return fmt.Sprintf("%06d", v%1000000)
}

// criarDonoHandlers cria o único Dono do teste (caminho do CLI) e o remove ao
// fim — DELETE, com cascata para `sessoes_plataforma`.
func criarDonoHandlers(t *testing.T, db *sql.DB) (id, segredo string) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM donos_plataforma`); err != nil {
		t.Fatalf("limpar donos_plataforma: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM donos_plataforma`) })
	id, segredo, err := services.CriarPrimeiroDonoPlataforma(db, "Dona Handlers", "dona-handlers@plataforma.com", senhaDonoHandlers)
	if err != nil {
		t.Fatalf("CriarPrimeiroDonoPlataforma: %v", err)
	}
	return id, segredo
}

// muxPlataformaTeste compõe as rotas do Dono exatamente como newMux.
func muxPlataformaTeste(db *sql.DB) *http.ServeMux {
	mux := http.NewServeMux()
	requireDono := middleware.RequireDonoPlataforma(db, segredoJWTPlataformaHandlers)
	mux.HandleFunc("POST /api/plataforma/auth/login", PlataformaLoginHandler(db, segredoJWTPlataformaHandlers))
	mux.HandleFunc("POST /api/plataforma/auth/refresh", PlataformaRefreshHandler(db, segredoJWTPlataformaHandlers))
	mux.HandleFunc("POST /api/plataforma/auth/logout", PlataformaLogoutHandler(db))
	mux.HandleFunc("GET /api/plataforma/auth/me", requireDono(PlataformaMeHandler()))
	mux.HandleFunc("GET /api/plataforma/empresas", requireDono(ListarEmpresasHandler(db)))
	mux.HandleFunc("POST /api/plataforma/empresas", requireDono(CriarEmpresaHandler(db, testEmailCfg)))
	mux.HandleFunc("POST /api/plataforma/empresas/{id}/desativacao", requireDono(DesativarEmpresaHandler(db)))
	mux.HandleFunc("POST /api/plataforma/empresas/{id}/reativacao", requireDono(ReativarEmpresaHandler(db)))
	return mux
}

type reqPlataforma struct {
	metodo, caminho, token, corpo string
	cookie                        *http.Cookie
}

func despacharPlataforma(mux *http.ServeMux, r reqPlataforma) *httptest.ResponseRecorder {
	var req *http.Request
	if r.corpo != "" {
		req = httptest.NewRequest(r.metodo, r.caminho, strings.NewReader(r.corpo))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(r.metodo, r.caminho, nil)
	}
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	if r.cookie != nil {
		req.AddCookie(r.cookie)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func codigoDeErro(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var env erroEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decodificar envelope de erro: %v (body=%s)", err, w.Body.String())
	}
	return env.Error.Code
}

func cookiePlataforma(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == refreshTokenPlataformaCookieName {
			return c
		}
	}
	t.Fatalf("resposta sem cookie %s (headers=%v)", refreshTokenPlataformaCookieName, w.Header())
	return nil
}

func TestPlataformaLogin_SucessoComCookieProprio(t *testing.T) {
	db := testDB(t)
	id, segredo := criarDonoHandlers(t, db)
	mux := muxPlataformaTeste(db)

	w := despacharPlataforma(mux, reqPlataforma{metodo: http.MethodPost, caminho: "/api/plataforma/auth/login",
		corpo: `{"email":"Dona-Handlers@Plataforma.com","senha":"` + senhaDonoHandlers + `","codigo":"` + codigoTOTPPlataformaTeste(t, segredo) + `"}`})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body struct {
		Token string       `json:"token"`
		Dono  donoResposta `json:"dono"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Token == "" || body.Dono.ID != id || body.Dono.Email != "dona-handlers@plataforma.com" {
		t.Errorf("body = %+v", body)
	}

	c := cookiePlataforma(t, w)
	if c.Path != "/api/plataforma/auth" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.MaxAge <= 0 || c.Value == "" {
		t.Errorf("cookie = %+v", c)
	}

	wMe := despacharPlataforma(mux, reqPlataforma{metodo: http.MethodGet, caminho: "/api/plataforma/auth/me", token: body.Token})
	if wMe.Code != http.StatusOK || !strings.Contains(wMe.Body.String(), id) {
		t.Errorf("/me: status = %d body=%s", wMe.Code, wMe.Body.String())
	}
}

func TestPlataformaLogin_Falhas(t *testing.T) {
	db := testDB(t)
	_, segredo := criarDonoHandlers(t, db)
	mux := muxPlataformaTeste(db)
	codigo := codigoTOTPPlataformaTeste(t, segredo)

	casos := []struct {
		nome, corpo string
		status      int
		code        string
	}{
		{"senha errada", `{"email":"dona-handlers@plataforma.com","senha":"errada-123","codigo":"` + codigo + `"}`, http.StatusUnauthorized, "INVALID_CREDENTIALS"},
		{"e-mail inexistente", `{"email":"ninguem@plataforma.com","senha":"` + senhaDonoHandlers + `","codigo":"` + codigo + `"}`, http.StatusUnauthorized, "INVALID_CREDENTIALS"},
		{"código em branco", `{"email":"dona-handlers@plataforma.com","senha":"` + senhaDonoHandlers + `","codigo":""}`, http.StatusBadRequest, "VALIDATION_ERROR"},
		{"payload inválido", `{isto nao e json`, http.StatusBadRequest, "VALIDATION_ERROR"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w := despacharPlataforma(mux, reqPlataforma{metodo: http.MethodPost, caminho: "/api/plataforma/auth/login", corpo: c.corpo})
			if w.Code != c.status || codigoDeErro(t, w) != c.code {
				t.Fatalf("status/code = %d/%s, want %d/%s (body=%s)", w.Code, codigoDeErro(t, w), c.status, c.code, w.Body.String())
			}
			if len(w.Result().Cookies()) != 0 {
				t.Error("login recusado não pode setar cookie")
			}
		})
	}
}

func TestPlataformaRefreshELogout(t *testing.T) {
	db := testDB(t)
	id, _ := criarDonoHandlers(t, db)
	mux := muxPlataformaTeste(db)

	_, refresh, _, err := services.EmitirSessaoPlataforma(db, segredoJWTPlataformaHandlers, id)
	if err != nil {
		t.Fatalf("EmitirSessaoPlataforma: %v", err)
	}
	cookieAntigo := &http.Cookie{Name: refreshTokenPlataformaCookieName, Value: refresh}

	w := despacharPlataforma(mux, reqPlataforma{metodo: http.MethodPost, caminho: "/api/plataforma/auth/refresh", cookie: cookieAntigo})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("refresh: status = %d, body=%s", w.Code, w.Body.String())
	}
	novo := cookiePlataforma(t, w)
	if novo.Value == refresh || novo.Path != "/api/plataforma/auth" {
		t.Errorf("cookie rotacionado = %+v", novo)
	}

	// O cookie antigo morreu na rotação.
	w = despacharPlataforma(mux, reqPlataforma{metodo: http.MethodPost, caminho: "/api/plataforma/auth/refresh", cookie: cookieAntigo})
	if w.Code != http.StatusUnauthorized || codigoDeErro(t, w) != "TOKEN_EXPIRED" {
		t.Fatalf("refresh com cookie antigo: status = %d (body=%s)", w.Code, w.Body.String())
	}
	if c := cookiePlataforma(t, w); c.MaxAge >= 0 {
		t.Errorf("cookie inválido não foi limpo: %+v", c)
	}

	// Logout revoga a sessão nova e limpa o cookie; é idempotente.
	w = despacharPlataforma(mux, reqPlataforma{metodo: http.MethodPost, caminho: "/api/plataforma/auth/logout", cookie: &http.Cookie{Name: refreshTokenPlataformaCookieName, Value: novo.Value}})
	if w.Code != http.StatusNoContent {
		t.Fatalf("logout: status = %d", w.Code)
	}
	if c := cookiePlataforma(t, w); c.MaxAge >= 0 || c.Path != "/api/plataforma/auth" {
		t.Errorf("logout não limpou o cookie: %+v", c)
	}
	w = despacharPlataforma(mux, reqPlataforma{metodo: http.MethodPost, caminho: "/api/plataforma/auth/refresh", cookie: &http.Cookie{Name: refreshTokenPlataformaCookieName, Value: novo.Value}})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("refresh após logout: status = %d, want 401", w.Code)
	}
	if w := despacharPlataforma(mux, reqPlataforma{metodo: http.MethodPost, caminho: "/api/plataforma/auth/logout"}); w.Code != http.StatusNoContent {
		t.Errorf("logout sem cookie: status = %d, want 204", w.Code)
	}
	if w := despacharPlataforma(mux, reqPlataforma{metodo: http.MethodPost, caminho: "/api/plataforma/auth/refresh"}); w.Code != http.StatusUnauthorized {
		t.Errorf("refresh sem cookie: status = %d, want 401", w.Code)
	}
}

// removerEmpresaPlataformaHandlers apaga a Empresa `slug` com todas as linhas
// que existem por causa dela (ARMADILHA da spec-9-2: DELETE, nunca TRUNCATE).
func removerEmpresaPlataformaHandlers(t *testing.T, db *sql.DB, slug string) {
	t.Helper()
	var id string
	err := db.QueryRow(`SELECT id FROM empresas WHERE slug = $1`, slug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return
	}
	if err != nil {
		t.Fatalf("remover empresa %s: %v", slug, err)
	}
	for _, stmt := range []string{
		`DELETE FROM produto_estoque WHERE produto_id IN (SELECT id FROM produtos WHERE empresa_id = $1)`,
		`DELETE FROM produtos WHERE empresa_id = $1`,
		`DELETE FROM estoques WHERE empresa_id = $1`,
		`DELETE FROM logs_acesso WHERE empresa_id = $1`,
		`DELETE FROM usuarios WHERE empresa_id = $1`,
		`DELETE FROM categorias WHERE empresa_id = $1`,
		`DELETE FROM nomenclatura_templates WHERE empresa_id = $1`,
		`DELETE FROM empresas WHERE id = $1`,
	} {
		if _, err := db.Exec(stmt, id); err != nil {
			t.Fatalf("remover empresa %s [%s]: %v", slug, stmt, err)
		}
	}
}

func limparParesPlataforma(t *testing.T, db *sql.DB, slugs ...string) {
	t.Helper()
	remover := func() {
		for _, s := range slugs {
			removerEmpresaPlataformaHandlers(t, db, s+"-treinamento")
			removerEmpresaPlataformaHandlers(t, db, s)
		}
	}
	remover()
	t.Cleanup(remover)
}

func corpoNovaEmpresa(slug, cnpj, uf string) string {
	return `{"nomeFantasia":"Cliente Handlers","razaoSocial":"Cliente Handlers LTDA","cnpj":"` + cnpj + `",` +
		`"slug":"` + slug + `","endereco":{"logradouro":"Rua A","numero":"1","complemento":"","bairro":"Centro",` +
		`"cidade":"Recife","cep":"50000-000","uf":"` + uf + `"},"admNome":"Adm Handlers","admEmail":"adm-handlers@cliente.com"}`
}

func TestEmpresasPlataforma_SemTokenOuComTokenDeUsuario(t *testing.T) {
	db := testDB(t)
	mux := muxPlataformaTeste(db)

	var usuarioID string
	if err := db.QueryRow(`INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, empresa_id)
		VALUES ('Adm', 'adm-empresa@empresa.com', 'h', 'adm', true, $1) RETURNING id`, empresaTeste).Scan(&usuarioID); err != nil {
		t.Fatalf("criar usuario: %v", err)
	}
	tokenUsuario, _, _, err := services.EmitirSessao(db, segredoJWTPlataformaHandlers, usuarioID, "senha")
	if err != nil {
		t.Fatalf("EmitirSessao: %v", err)
	}

	rotas := []struct{ metodo, caminho string }{
		{http.MethodGet, "/api/plataforma/auth/me"},
		{http.MethodGet, "/api/plataforma/empresas"},
		{http.MethodPost, "/api/plataforma/empresas"},
		{http.MethodPost, "/api/plataforma/empresas/qualquer-id/desativacao"},
		{http.MethodPost, "/api/plataforma/empresas/qualquer-id/reativacao"},
	}
	for _, r := range rotas {
		for _, token := range []string{"", tokenUsuario} {
			w := despacharPlataforma(mux, reqPlataforma{metodo: r.metodo, caminho: r.caminho, token: token})
			if w.Code != http.StatusUnauthorized || codigoDeErro(t, w) != "TOKEN_EXPIRED" {
				t.Errorf("%s %s (token de usuario=%v): status/code = %d/%s, want 401/TOKEN_EXPIRED",
					r.metodo, r.caminho, token != "", w.Code, codigoDeErro(t, w))
			}
		}
	}
}

func TestEmpresasPlataforma_CriarListarDesativarReativar(t *testing.T) {
	db := testDB(t)
	limparParesPlataforma(t, db, "plat-handlers", "plat-handlers-b")
	id, _ := criarDonoHandlers(t, db)
	mux := muxPlataformaTeste(db)
	token, _, _, err := services.EmitirSessaoPlataforma(db, segredoJWTPlataformaHandlers, id)
	if err != nil {
		t.Fatalf("EmitirSessaoPlataforma: %v", err)
	}
	post := func(caminho, corpo string) *httptest.ResponseRecorder {
		return despacharPlataforma(mux, reqPlataforma{metodo: http.MethodPost, caminho: caminho, token: token, corpo: corpo})
	}

	w := post("/api/plataforma/empresas", corpoNovaEmpresa("plat-handlers", "92.345.678/0001-24", "PE"))
	if w.Code != http.StatusCreated {
		t.Fatalf("criar: status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var criada struct {
		Empresa     services.Empresa `json:"empresa"`
		Treinamento services.Empresa `json:"treinamento"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &criada); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if criada.Empresa.Slug != "plat-handlers" || criada.Treinamento.Slug != "plat-handlers-treinamento" ||
		criada.Treinamento.EmpresaOrigemID == nil || *criada.Treinamento.EmpresaOrigemID != criada.Empresa.ID {
		t.Errorf("resposta de criação = %+v", criada)
	}
	// Nenhuma resposta ao Dono carrega o token de primeiro acesso nem senha.
	var tokens []string
	rows, err := db.Query(`SELECT t.token FROM tokens_acao t JOIN usuarios u ON u.id = t.usuario_id WHERE u.empresa_id IN ($1, $2)`,
		criada.Empresa.ID, criada.Treinamento.ID)
	if err != nil {
		t.Fatalf("ler tokens: %v", err)
	}
	for rows.Next() {
		var tk string
		_ = rows.Scan(&tk)
		tokens = append(tokens, tk)
	}
	rows.Close()
	if len(tokens) != 2 {
		t.Fatalf("tokens de primeiro acesso = %d, want 2", len(tokens))
	}
	for _, tk := range tokens {
		if strings.Contains(w.Body.String(), tk) {
			t.Error("a resposta de criação expõe o token de primeiro acesso do adm")
		}
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "senha") {
		t.Error("a resposta de criação menciona senha")
	}

	// Conflitos: slug e CNPJ, cada um com a sua mensagem.
	w = post("/api/plataforma/empresas", corpoNovaEmpresa("plat-handlers", "93.456.789/0001-70", "PE"))
	if w.Code != http.StatusConflict || codigoDeErro(t, w) != "CONFLICT" || !strings.Contains(w.Body.String(), "endereço de acesso") {
		t.Errorf("slug duplicado: status = %d body=%s", w.Code, w.Body.String())
	}
	w = post("/api/plataforma/empresas", corpoNovaEmpresa("plat-handlers-b", "92.345.678/0001-24", "PE"))
	if w.Code != http.StatusConflict || codigoDeErro(t, w) != "CONFLICT" || !strings.Contains(w.Body.String(), "CNPJ") {
		t.Errorf("CNPJ duplicado: status = %d body=%s", w.Code, w.Body.String())
	}
	// Validação: a mensagem do campo chega ao cliente.
	w = post("/api/plataforma/empresas", corpoNovaEmpresa("plat-handlers-b", "93.456.789/0001-70", "9"))
	if w.Code != http.StatusBadRequest || codigoDeErro(t, w) != "VALIDATION_ERROR" || !strings.Contains(w.Body.String(), "UF") {
		t.Errorf("UF inválida: status = %d body=%s", w.Code, w.Body.String())
	}
	if w := post("/api/plataforma/empresas", `{isto nao e json`); w.Code != http.StatusBadRequest {
		t.Errorf("payload inválido: status = %d, want 400", w.Code)
	}

	// Listagem.
	w = despacharPlataforma(mux, reqPlataforma{metodo: http.MethodGet, caminho: "/api/plataforma/empresas", token: token})
	if w.Code != http.StatusOK {
		t.Fatalf("listar: status = %d", w.Code)
	}
	var lista struct {
		Empresas []services.EmpresaResumo `json:"empresas"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &lista); err != nil {
		t.Fatalf("decode lista: %v", err)
	}
	var achou bool
	for _, e := range lista.Empresas {
		if e.ID == criada.Empresa.ID {
			achou = e.Treinamento != nil && e.Treinamento.Slug == "plat-handlers-treinamento" &&
				e.Adm != nil && e.Adm.Email == "adm-handlers@cliente.com"
		}
	}
	if !achou {
		t.Errorf("empresa criada ausente ou incompleta na listagem: %s", w.Body.String())
	}

	// Desativação/reativação.
	w = post("/api/plataforma/empresas/"+criada.Empresa.ID+"/desativacao", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"inativa"`) {
		t.Errorf("desativar: status = %d body=%s", w.Code, w.Body.String())
	}
	for _, idInvalido := range []string{criada.Treinamento.ID, "00000000-0000-4000-8000-000000000000", "nao-e-uuid"} {
		if w := post("/api/plataforma/empresas/"+idInvalido+"/desativacao", ""); w.Code != http.StatusNotFound || codigoDeErro(t, w) != "NOT_FOUND" {
			t.Errorf("desativar %q: status = %d, want 404", idInvalido, w.Code)
		}
	}
	w = post("/api/plataforma/empresas/"+criada.Empresa.ID+"/reativacao", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ativa"`) {
		t.Errorf("reativar: status = %d body=%s", w.Code, w.Body.String())
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM empresas WHERE id = $1`, criada.Treinamento.ID).Scan(&status); err != nil || status != "ativa" {
		t.Errorf("status do treino após reativar = %q (%v), want ativa", status, err)
	}
}

// TestMeHandler_AmbienteTreinamento prova o flag de exibição da resposta de
// sessão pela composição real (RequireEmpresa -> RequireAuth -> MeHandler):
// false numa Empresa comum, true (com o nome do Treinamento) num Treinamento.
func TestMeHandler_AmbienteTreinamento(t *testing.T) {
	db := testDB(t)
	limparParesPlataforma(t, db, "plat-me")

	_, treino, err := services.CriarEmpresaComTreinamento(db, testEmailCfg, services.NovaEmpresaInput{
		DadosEmpresa: services.DadosEmpresa{
			NomeFantasia: "Cliente Me",
			RazaoSocial:  "Cliente Me LTDA",
			CNPJ:         "95678901000143",
			Slug:         "plat-me",
			Endereco: services.EnderecoEmpresa{
				Logradouro: "Rua C", Numero: "3", Bairro: "Centro", Cidade: "Recife", CEP: "50000000", UF: "PE",
			},
		},
		AdmNome:  "Adm Me",
		AdmEmail: "adm-me@cliente.com",
	})
	if err != nil {
		t.Fatalf("CriarEmpresaComTreinamento: %v", err)
	}
	var nomeEmpresaTeste string
	if err := db.QueryRow(`SELECT nome_fantasia FROM empresas WHERE id = $1`, empresaTeste).Scan(&nomeEmpresaTeste); err != nil {
		t.Fatalf("ler empresa padrão: %v", err)
	}

	casos := []struct {
		nome, slug, empresaID, empresaNome string
		want                               bool
	}{
		{"empresa comum", slugEmpresaTeste, empresaTeste, nomeEmpresaTeste, false},
		{"treinamento", treino.Slug, treino.ID, treino.NomeFantasia, true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			var usuarioID string
			if err := db.QueryRow(`INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, empresa_id)
				VALUES ('Fulano', 'fulano-me@empresa.com', 'h', 'usuario', true, $1) RETURNING id`, c.empresaID).Scan(&usuarioID); err != nil {
				t.Fatalf("criar usuario: %v", err)
			}
			token, _, _, err := services.EmitirSessao(db, testJWTSecret, usuarioID, "senha")
			if err != nil {
				t.Fatalf("EmitirSessao: %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "/e/"+c.slug+"/api/auth/me", nil)
			req.SetPathValue("slug", c.slug)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			comEmpresa(db, middleware.RequireAuth(db, testJWTSecret)(MeHandler()))(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
			}
			var resp usuarioResposta
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.AmbienteTreinamento != c.want || resp.EmpresaNome != c.empresaNome {
				t.Errorf("resposta = %+v, want ambienteTreinamento=%v empresaNome=%q", resp, c.want, c.empresaNome)
			}
		})
	}
}
