package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// Suíte HTTP dos convites nominais — Story 9.3, spec-9-3. Cada teste despacha
// pela MESMA composição de newMux (RequireEmpresa por fora, RequireAuth +
// RequireRole por dentro), nunca chamando o handler cru: o gate de papel é do
// middleware e precisa ser exercitado junto.

// conviteDeTeste grava um convite pendente direto na tabela e devolve o token
// — setup, nunca o objeto sob teste (o mesmo helper existe na suíte de
// `services`). Gravar direto evita acoplar dezenas de testes aos guards de
// EmitirConvite (por exemplo o 409 de e-mail já cadastrado, que impediria
// montar o cenário de e-mail duplicado do cadastro).
func conviteDeTeste(t *testing.T, db *sql.DB, empresaID, email string) string {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("conviteDeTeste: gerar token: %v", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	_, err := db.Exec(
		`INSERT INTO convites_empresa (empresa_id, email, token, expira_em)
		 VALUES ($1, $2, $3, $4)`,
		empresaID, strings.ToLower(strings.TrimSpace(email)), token, time.Now().UTC().Add(7*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("conviteDeTeste(%s): %v", email, err)
	}
	return token
}

// comSessaoGestor compõe a pilha real de uma rota de convite: RequireEmpresa
// (por fora) -> RequireAuth -> RequireRole(gestor) -> handler.
func comSessaoConvite(db *sql.DB, papelMinimo string, h http.HandlerFunc) http.HandlerFunc {
	return comEmpresa(db, middleware.RequireAuth(db, testJWTSecret)(
		middleware.RequireRole(papelMinimo)(h)))
}

// contaComPapel insere uma conta ativa/verificada com MFA habilitado e devolve
// o id. MFA habilitado é necessário para papéis `gestor`+ passarem pelo
// segundo gate de RequireRole (Story 1.11) quando a sessão é por senha.
func contaComPapel(t *testing.T, db *sql.DB, nome, email, papel string) string {
	t.Helper()
	var id string
	err := db.QueryRow(
		`INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, empresa_id, mfa_habilitado, mfa_secret)
		 VALUES ($1, $2, 'hash-qualquer', $3, true, true, $4, true, 'SEGREDO') RETURNING id`,
		nome, email, papel, empresaTeste,
	).Scan(&id)
	if err != nil {
		t.Fatalf("contaComPapel(%s): %v", email, err)
	}
	return id
}

// tokenDeSessao emite um access token para a conta — mesma via de produção
// (services.EmitirSessao), para que os claims (papel, origem, mfa) sejam os
// reais.
func tokenDeSessao(t *testing.T, db *sql.DB, usuarioID string) string {
	t.Helper()
	accessToken, _, _, err := services.EmitirSessao(db, testJWTSecret, usuarioID, "senha")
	if err != nil {
		t.Fatalf("EmitirSessao: %v", err)
	}
	return accessToken
}

func requisicaoConvite(t *testing.T, db *sql.DB, metodo, caminho, corpo, autorizacao string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if corpo == "" {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(corpo)
	}
	req := httptest.NewRequest(metodo, prefixoEmpresaTeste+caminho, body)
	req.Header.Set("Content-Type", "application/json")
	if autorizacao != "" {
		req.Header.Set("Authorization", "Bearer "+autorizacao)
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

type conviteJSON struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Situacao      string `json:"situacao"`
	ExpiraEm      string `json:"expiraEm"`
	CriadoEm      string `json:"criadoEm"`
	CriadoPorNome string `json:"criadoPorNome"`
	Link          string `json:"link"`
}

// TestEmitirConviteHandler_Sucesso prova a linha "Emitir convite" da I/O
// Matrix na fronteira HTTP: 201 com o convite e o link, e-mail normalizado, e
// a linha gravada em convites_empresa.
func TestEmitirConviteHandler_Sucesso(t *testing.T) {
	db := testDB(t)
	gestor := contaComPapel(t, db, "Maria Gestora", "maria.convite@empresa.com", services.PapelGestor)
	auth := tokenDeSessao(t, db, gestor)

	w := requisicaoConvite(t, db, http.MethodPost, "/api/convites", `{"email":"Fulano@X.com"}`, auth,
		comSessaoConvite(db, services.PapelGestor, EmitirConviteHandler(db, testEmailCfg)))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}

	var body struct {
		Convite conviteJSON `json:"convite"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("corpo inválido: %v (body=%s)", err, w.Body.String())
	}
	if body.Convite.Email != "fulano@x.com" {
		t.Errorf("email = %q, want %q", body.Convite.Email, "fulano@x.com")
	}
	if body.Convite.Situacao != "pendente" {
		t.Errorf("situacao = %q, want pendente", body.Convite.Situacao)
	}
	wantPrefixo := "http://test.local" + prefixoEmpresaTeste + "/cadastro?token="
	if !strings.HasPrefix(body.Convite.Link, wantPrefixo) {
		t.Errorf("link = %q, want prefixo %q", body.Convite.Link, wantPrefixo)
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM convites_empresa WHERE empresa_id = $1`, empresaTeste).Scan(&n); err != nil {
		t.Fatalf("contar convites: %v", err)
	}
	if n != 1 {
		t.Errorf("count(convites_empresa) = %d, want 1", n)
	}
}

// TestEmitirConviteHandler_ErrosDaMatriz prova as linhas "E-mail
// vazio/inválido" (400 VALIDATION_ERROR) e "E-mail já tem conta na Empresa"
// (409 CONFLICT), ambas sem gravar nenhuma linha.
func TestEmitirConviteHandler_ErrosDaMatriz(t *testing.T) {
	db := testDB(t)
	gestor := contaComPapel(t, db, "Maria", "maria.erros@empresa.com", services.PapelGestor)
	auth := tokenDeSessao(t, db, gestor)
	contaComPapel(t, db, "Já Tem", "jatem@empresa.com", services.PapelUsuario)

	casos := []struct {
		desc       string
		corpo      string
		wantStatus int
		wantCode   string
	}{
		{"e-mail em branco", `{"email":"  "}`, http.StatusBadRequest, "VALIDATION_ERROR"},
		{"e-mail sem arroba", `{"email":"fulano"}`, http.StatusBadRequest, "VALIDATION_ERROR"},
		{"payload não-JSON", `{isto nao e json`, http.StatusBadRequest, "VALIDATION_ERROR"},
		{"e-mail já tem conta", `{"email":"JaTem@empresa.com"}`, http.StatusConflict, "CONFLICT"},
	}
	for _, c := range casos {
		t.Run(c.desc, func(t *testing.T) {
			w := requisicaoConvite(t, db, http.MethodPost, "/api/convites", c.corpo, auth,
				comSessaoConvite(db, services.PapelGestor, EmitirConviteHandler(db, testEmailCfg)))
			if w.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, c.wantStatus, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != c.wantCode {
				t.Errorf("code = %q, want %q", env.Error.Code, c.wantCode)
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM convites_empresa`).Scan(&n); err != nil {
				t.Fatalf("contar convites: %v", err)
			}
			if n != 0 {
				t.Errorf("count(convites_empresa) = %d, want 0 — nenhuma escrita esperada", n)
			}
		})
	}
}

// TestEmitirConviteHandler_PapelInsuficiente prova a linha "Papel
// insuficiente": 403 FORBIDDEN decidido por RequireRole, com o handler nunca
// executando (nenhuma linha gravada).
func TestEmitirConviteHandler_PapelInsuficiente(t *testing.T) {
	db := testDB(t)

	for _, papel := range []string{services.PapelUsuario, services.PapelAlmoxarife} {
		t.Run(papel, func(t *testing.T) {
			conta := contaComPapel(t, db, "Sem Poder "+papel, "sempoder-"+papel+"@empresa.com", papel)
			auth := tokenDeSessao(t, db, conta)

			w := requisicaoConvite(t, db, http.MethodPost, "/api/convites", `{"email":"alguem@x.com"}`, auth,
				comSessaoConvite(db, services.PapelGestor, EmitirConviteHandler(db, testEmailCfg)))
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "FORBIDDEN" {
				t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM convites_empresa`).Scan(&n); err != nil {
				t.Fatalf("contar convites: %v", err)
			}
			if n != 0 {
				t.Errorf("count(convites_empresa) = %d, want 0 — o handler não podia executar", n)
			}
		})
	}
}

// TestListarConvitesHandler prova a linha "Listar convites": 200 com a lista
// da Empresa do slug, cada item com `situacao`, e `link` só nos pendentes.
func TestListarConvitesHandler(t *testing.T) {
	db := testDB(t)
	gestor := contaComPapel(t, db, "Maria", "maria.listar@empresa.com", services.PapelGestor)
	auth := tokenDeSessao(t, db, gestor)

	conviteDeTeste(t, db, empresaTeste, "pendente@x.com")
	revogado := conviteDeTeste(t, db, empresaTeste, "revogado@x.com")
	if _, err := db.Exec(`UPDATE convites_empresa SET revogado_em = now() WHERE token = $1`, revogado); err != nil {
		t.Fatalf("marcar revogado: %v", err)
	}

	w := requisicaoConvite(t, db, http.MethodGet, "/api/convites", "", auth,
		comSessaoConvite(db, services.PapelGestor, ListarConvitesHandler(db, testEmailCfg)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body struct {
		Convites []conviteJSON `json:"convites"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("corpo inválido: %v", err)
	}
	if len(body.Convites) != 2 {
		t.Fatalf("len(convites) = %d, want 2", len(body.Convites))
	}
	for _, c := range body.Convites {
		switch c.Email {
		case "pendente@x.com":
			if c.Situacao != "pendente" || c.Link == "" {
				t.Errorf("pendente: situacao = %q, link = %q — want pendente com link", c.Situacao, c.Link)
			}
		case "revogado@x.com":
			if c.Situacao != "revogado" || c.Link != "" {
				t.Errorf("revogado: situacao = %q, link = %q — want revogado sem link", c.Situacao, c.Link)
			}
		default:
			t.Errorf("convite inesperado na listagem: %q", c.Email)
		}
	}
}

// TestRevogarConviteHandler prova as linhas "Revogar pendente" (200) e
// "Revogar usado/inexistente" (409 / 404).
func TestRevogarConviteHandler(t *testing.T) {
	db := testDB(t)
	gestor := contaComPapel(t, db, "Maria", "maria.revogar@empresa.com", services.PapelGestor)
	auth := tokenDeSessao(t, db, gestor)

	revogar := func(t *testing.T, id string) *httptest.ResponseRecorder {
		t.Helper()
		h := comSessaoConvite(db, services.PapelGestor, RevogarConviteHandler(db))
		req := httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/convites/"+id+"/revogacao", nil)
		req.SetPathValue("id", id)
		req.Header.Set("Authorization", "Bearer "+auth)
		w := httptest.NewRecorder()
		h(w, req)
		return w
	}

	idDoToken := func(t *testing.T, token string) string {
		t.Helper()
		var id string
		if err := db.QueryRow(`SELECT id FROM convites_empresa WHERE token = $1`, token).Scan(&id); err != nil {
			t.Fatalf("ler id do convite: %v", err)
		}
		return id
	}

	t.Run("pendente", func(t *testing.T) {
		id := idDoToken(t, conviteDeTeste(t, db, empresaTeste, "revoga-ok@x.com"))
		if w := revogar(t, id); w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		var revogadoEm sql.NullTime
		if err := db.QueryRow(`SELECT revogado_em FROM convites_empresa WHERE id = $1`, id).Scan(&revogadoEm); err != nil {
			t.Fatalf("reler convite: %v", err)
		}
		if !revogadoEm.Valid {
			t.Error("revogado_em vazio após 200")
		}
	})

	t.Run("já usado", func(t *testing.T) {
		id := idDoToken(t, conviteDeTeste(t, db, empresaTeste, "revoga-usado@x.com"))
		if _, err := db.Exec(`UPDATE convites_empresa SET usado_em = now() WHERE id = $1`, id); err != nil {
			t.Fatalf("marcar usado: %v", err)
		}
		w := revogar(t, id)
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409 (body=%s)", w.Code, w.Body.String())
		}
		if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
			t.Errorf("code = %q, want CONFLICT", env.Error.Code)
		}
	})

	t.Run("inexistente", func(t *testing.T) {
		w := revogar(t, "11111111-1111-1111-1111-111111111111")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
		}
		if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "NOT_FOUND" {
			t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
		}
	})

	t.Run("id malformado", func(t *testing.T) {
		if w := revogar(t, "nao-e-uuid"); w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
		}
	})
}

func getValidarConvite(db *sql.DB, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, prefixoEmpresaTeste+"/api/auth/convite?token="+token, nil)
	w := httptest.NewRecorder()
	comEmpresa(db, ValidarConviteHandler(db))(w, req)
	return w
}

// TestValidarConviteHandler prova a linha "Validar link ao abrir" e cada
// motivo de recusa — códigos distintos para a tela explicar o porquê.
func TestValidarConviteHandler(t *testing.T) {
	db := testDB(t)

	w := getValidarConvite(db, conviteDeTeste(t, db, empresaTeste, "Fulano@X.com"))
	if w.Code != http.StatusOK {
		t.Fatalf("válido: status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("corpo inválido: %v", err)
	}
	if body.Email != "fulano@x.com" {
		t.Errorf("email = %q, want %q", body.Email, "fulano@x.com")
	}

	casos := []struct {
		desc       string
		preparar   func(t *testing.T) string
		wantStatus int
		wantCode   string
	}{
		{
			desc:       "inexistente",
			preparar:   func(*testing.T) string { return "token-que-nao-existe" },
			wantStatus: http.StatusNotFound,
			wantCode:   "NOT_FOUND",
		},
		{
			desc: "expirado",
			preparar: func(t *testing.T) string {
				tk := conviteDeTeste(t, db, empresaTeste, "exp@x.com")
				if _, err := db.Exec(`UPDATE convites_empresa SET expira_em = now() - interval '1 hour' WHERE token = $1`, tk); err != nil {
					t.Fatalf("forçar expiração: %v", err)
				}
				return tk
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "TOKEN_EXPIRED",
		},
		{
			desc: "já usado",
			preparar: func(t *testing.T) string {
				tk := conviteDeTeste(t, db, empresaTeste, "usd@x.com")
				if _, err := db.Exec(`UPDATE convites_empresa SET usado_em = now() WHERE token = $1`, tk); err != nil {
					t.Fatalf("marcar usado: %v", err)
				}
				return tk
			},
			wantStatus: http.StatusConflict,
			wantCode:   "CONFLICT",
		},
		{
			desc: "revogado",
			preparar: func(t *testing.T) string {
				tk := conviteDeTeste(t, db, empresaTeste, "rev@x.com")
				if _, err := db.Exec(`UPDATE convites_empresa SET revogado_em = now() WHERE token = $1`, tk); err != nil {
					t.Fatalf("marcar revogado: %v", err)
				}
				return tk
			},
			wantStatus: http.StatusForbidden,
			wantCode:   "FORBIDDEN",
		},
	}
	for _, c := range casos {
		t.Run(c.desc, func(t *testing.T) {
			w := getValidarConvite(db, c.preparar(t))
			if w.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, c.wantStatus, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != c.wantCode {
				t.Errorf("code = %q, want %q", env.Error.Code, c.wantCode)
			}
		})
	}
}

// TestCadastroHandler_ConviteNaMatriz prova, na fronteira HTTP, cada linha de
// recusa do cadastro por convite — código E mensagem distintos por motivo,
// nenhuma conta criada.
func TestCadastroHandler_ConviteNaMatriz(t *testing.T) {
	db := testDB(t)

	casos := []struct {
		desc         string
		preparar     func(t *testing.T) string
		email        string
		wantStatus   int
		wantCode     string
		wantMensagem string
	}{
		{
			desc:       "sem token",
			preparar:   func(*testing.T) string { return "" },
			email:      "semtoken@x.com",
			wantStatus: http.StatusNotFound,
			wantCode:   "NOT_FOUND",
		},
		{
			desc:       "token inexistente",
			preparar:   func(*testing.T) string { return "token-inexistente" },
			email:      "inexistente@x.com",
			wantStatus: http.StatusNotFound,
			wantCode:   "NOT_FOUND",
		},
		{
			desc:         "e-mail diferente do convidado",
			preparar:     func(t *testing.T) string { return conviteDeTeste(t, db, empresaTeste, "convidado@x.com") },
			email:        "outro@x.com",
			wantStatus:   http.StatusForbidden,
			wantCode:     "FORBIDDEN",
			wantMensagem: "Este convite foi emitido para outro e-mail.",
		},
		{
			desc: "convite expirado",
			preparar: func(t *testing.T) string {
				tk := conviteDeTeste(t, db, empresaTeste, "expirou@x.com")
				if _, err := db.Exec(`UPDATE convites_empresa SET expira_em = now() - interval '1 hour' WHERE token = $1`, tk); err != nil {
					t.Fatalf("forçar expiração: %v", err)
				}
				return tk
			},
			email:        "expirou@x.com",
			wantStatus:   http.StatusBadRequest,
			wantCode:     "TOKEN_EXPIRED",
			wantMensagem: "Este convite expirou.",
		},
		{
			desc: "convite já usado",
			preparar: func(t *testing.T) string {
				tk := conviteDeTeste(t, db, empresaTeste, "jausado@x.com")
				if _, err := db.Exec(`UPDATE convites_empresa SET usado_em = now() WHERE token = $1`, tk); err != nil {
					t.Fatalf("marcar usado: %v", err)
				}
				return tk
			},
			email:        "jausado@x.com",
			wantStatus:   http.StatusConflict,
			wantCode:     "CONFLICT",
			wantMensagem: "Este convite já foi utilizado.",
		},
		{
			desc: "convite revogado",
			preparar: func(t *testing.T) string {
				tk := conviteDeTeste(t, db, empresaTeste, "revogou@x.com")
				if _, err := db.Exec(`UPDATE convites_empresa SET revogado_em = now() WHERE token = $1`, tk); err != nil {
					t.Fatalf("marcar revogado: %v", err)
				}
				return tk
			},
			email:        "revogou@x.com",
			wantStatus:   http.StatusForbidden,
			wantCode:     "FORBIDDEN",
			wantMensagem: "Este convite foi cancelado pela empresa.",
		},
	}

	for _, c := range casos {
		t.Run(c.desc, func(t *testing.T) {
			token := c.preparar(t)
			var antes int
			if err := db.QueryRow(`SELECT count(*) FROM usuarios`).Scan(&antes); err != nil {
				t.Fatalf("contar usuarios: %v", err)
			}

			w := postCadastro(db, `{"token":"`+token+`","nome":"Alguém","email":"`+c.email+`","senha":"senha-123456"}`)
			if w.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, c.wantStatus, w.Body.String())
			}
			env := decodeErro(t, w.Body.Bytes())
			if env.Error.Code != c.wantCode {
				t.Errorf("code = %q, want %q", env.Error.Code, c.wantCode)
			}
			if c.wantMensagem != "" && env.Error.Message != c.wantMensagem {
				t.Errorf("message = %q, want %q", env.Error.Message, c.wantMensagem)
			}

			var depois int
			if err := db.QueryRow(`SELECT count(*) FROM usuarios`).Scan(&depois); err != nil {
				t.Fatalf("contar usuarios: %v", err)
			}
			if depois != antes {
				t.Errorf("count(usuarios) = %d, want %d — nenhuma conta podia nascer", depois, antes)
			}
		})
	}
}

// TestCadastroHandler_PayloadInvalidoNaoQueimaConvite prova a última linha da
// I/O Matrix na fronteira HTTP: senha fraca com token válido -> 400
// VALIDATION_ERROR, e o convite continua pendente e utilizável.
func TestCadastroHandler_PayloadInvalidoNaoQueimaConvite(t *testing.T) {
	db := testDB(t)
	token := conviteDeTeste(t, db, empresaTeste, "naoqueima@x.com")

	w := postCadastro(db, `{"token":"`+token+`","nome":"Alguém","email":"naoqueima@x.com","senha":"abc"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}

	var usadoEm sql.NullTime
	if err := db.QueryRow(`SELECT usado_em FROM convites_empresa WHERE token = $1`, token).Scan(&usadoEm); err != nil {
		t.Fatalf("reler convite: %v", err)
	}
	if usadoEm.Valid {
		t.Fatal("usado_em preenchido — um payload inválido não pode queimar o convite")
	}

	if w := postCadastro(db, `{"token":"`+token+`","nome":"Alguém","email":"naoqueima@x.com","senha":"senha-123456"}`); w.Code != http.StatusCreated {
		t.Fatalf("cadastro válido após a tentativa inválida: status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
}
