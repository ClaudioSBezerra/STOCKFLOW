package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// --- despacho pela MESMA composição de newMux (main.go) --------------------
//
// POST /api/produtos -> RequireAuth -> RequireRole(almoxarife) -> handler.
// GET /api/categorias -> RequireAuth -> handler (SEM RequireRole).

func limparProdutosHandler(t *testing.T, db *sql.DB) {
	t.Helper()
	// `produto_estoque` e `produtos` entram na mesma TRUNCATE que `estoques`
	// por causa da FK de produto_estoque.estoque_id -> estoques(id).
	// `importacao_linhas` entra pelo mesmo motivo (Story 3.3):
	// importacao_linhas.produto_id -> produtos(id), sem CASCADE.
	// `categorias` NUNCA é truncada aqui — seed fixo da migração 000010.
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, produto_estoque, produtos, estoques`); err != nil {
		t.Fatalf("falha ao limpar produtos/produto_estoque/estoques: %v", err)
	}
}

func categoriaIDPorCodigoHandler(t *testing.T, db *sql.DB, codigo string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE codigo = $1`, codigo).Scan(&id); err != nil {
		t.Fatalf("falha ao buscar categoria %q: %v", codigo, err)
	}
	return id
}

func postProdutos(db *sql.DB, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/produtos",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				CriarProdutoHandler(db))))
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, "/api/produtos", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, "/api/produtos", nil)
	}
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func getCategorias(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/categorias",
		middleware.RequireAuth(db, testJWTSecret)(
			ListarCategoriasHandler(db)))
	r := httptest.NewRequest(http.MethodGet, "/api/categorias", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// getNomenclaturaTemplates despacha GET /api/nomenclatura-templates pela
// MESMA composição de newMux (RequireAuth apenas — Story 3.2).
func getNomenclaturaTemplates(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/nomenclatura-templates",
		middleware.RequireAuth(db, testJWTSecret)(
			ListarNomenclaturaTemplatesHandler(db)))
	r := httptest.NewRequest(http.MethodGet, "/api/nomenclatura-templates", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// postRenomear despacha POST /api/produtos/{id}/renomear pela MESMA
// composição de newMux (RequireAuth -> RequireRole(almoxarife) ->
// AtualizarNomeProdutoHandler — Story 3.2).
func postRenomear(db *sql.DB, authHeader, id, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/produtos/{id}/renomear",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				AtualizarNomeProdutoHandler(db))))
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, "/api/produtos/"+id+"/renomear", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, "/api/produtos/"+id+"/renomear", nil)
	}
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// templateIDPorSubtipoHandler devolve o id de um dos 28 templates fixos de
// seed (migração 000013), pelo `subtipo` (addendum §G).
func templateIDPorSubtipoHandler(t *testing.T, db *sql.DB, subtipo string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`SELECT id FROM nomenclatura_templates WHERE subtipo = $1`, subtipo).Scan(&id); err != nil {
		t.Fatalf("falha ao buscar template %q: %v", subtipo, err)
	}
	return id
}

// TestCriarProdutoHandler_201ParaAlmoxarifeGestorAdm prova a AC1 na fronteira
// HTTP: uma sessão `almoxarife`+ com corpo válido (incluindo as 5 dimensões
// pareadas) recebe 201 e o corpo `{"produto":{"id","nome"}}`.
func TestCriarProdutoHandler_201ParaAlmoxarifeGestorAdm(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")

	casos := []struct{ papel, email, nome string }{
		{"almoxarife", "prod-almox@empresa.com", "Tubo PVC 100mm"},
		{"gestor", "prod-gestor@empresa.com", "Cabo Elétrico X"},
		{"adm", "prod-adm@empresa.com", "Capacete Branco"},
	}
	for _, c := range casos {
		t.Run(c.papel, func(t *testing.T) {
			criarContaComPapel(t, db, "Conta "+c.papel, c.email, "senha-123456", c.papel)
			token := tokenDeLogin(t, db, c.email, "senha-123456")

			estoque, err := services.CriarEstoque(db, "Canteiro "+c.papel)
			if err != nil {
				t.Fatalf("seed CriarEstoque: %v", err)
			}

			corpo := `{
				"nome": "` + c.nome + `",
				"categoria_id": "` + categoriaID + `",
				"estoque_id": "` + estoque.ID + `",
				"quantidade_inicial": 10,
				"comprimento": {"valor": 6, "unidade": "m"},
				"largura": {"valor": 100, "unidade": "mm"},
				"diametro": {"valor": 10, "unidade": "cm"},
				"altura": {"valor": 2, "unidade": "m"},
				"espessura": {"valor": 5, "unidade": "mm"}
			}`
			w := postProdutos(db, "Bearer "+token, corpo)
			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
			}
			var resp struct {
				Produto map[string]any `json:"produto"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
			}
			if id, ok := resp.Produto["id"].(string); !ok || id == "" {
				t.Errorf("produto.id ausente/vazio: %v", resp.Produto["id"])
			}
			if resp.Produto["nome"] != c.nome {
				t.Errorf("produto.nome = %v, want %q", resp.Produto["nome"], c.nome)
			}
		})
	}
}

// TestCriarProdutoHandler_201SemDimensoes prova a AC1 no caso "sem
// dimensões": corpo válido sem nenhuma das 5 dimensões -> 201.
func TestCriarProdutoHandler_201SemDimensoes(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.002")
	criarContaComPapel(t, db, "Almox", "prod-semdim-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "prod-semdim-almox@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Sem Dimensão")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	corpo := `{"nome":"Produto Simples","categoria_id":"` + categoriaID + `","estoque_id":"` + estoque.ID + `","quantidade_inicial":1}`
	w := postProdutos(db, "Bearer "+token, corpo)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
}

// TestCriarProdutoHandler_400DimensaoIncompleta prova a AC2 na fronteira:
// dimensão só com `valor` (sem `unidade`) -> 400 VALIDATION_ERROR citando o
// campo específico na mensagem.
func TestCriarProdutoHandler_400DimensaoIncompleta(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.003")
	criarContaComPapel(t, db, "Almox", "prod-dim-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "prod-dim-almox@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Dimensão Incompleta")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	corpo := `{
		"nome": "Produto Dimensão Incompleta",
		"categoria_id": "` + categoriaID + `",
		"estoque_id": "` + estoque.ID + `",
		"quantidade_inicial": 1,
		"largura": {"valor": 10}
	}`
	w := postProdutos(db, "Bearer "+token, corpo)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
	if !strings.Contains(strings.ToLower(env.Error.Message), "largura") {
		t.Errorf("message = %q, want conter %q", env.Error.Message, "largura")
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM produtos`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("linhas em produtos = %d, want 0 (nada deveria ser gravado)", n)
	}
}

// TestCriarProdutoHandler_400PayloadInvalido prova a linha "nome ausente/JSON
// inválido/corpo > limite" da I/O Matrix: 400 VALIDATION_ERROR.
func TestCriarProdutoHandler_400PayloadInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox", "prod-payload-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "prod-payload-almox@empresa.com", "senha-123456")

	casos := map[string]string{
		"json inválido": `{"nome":`,
		"nome vazio":    `{"nome":"","categoria_id":"x","estoque_id":"y","quantidade_inicial":1}`,
	}
	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			w := postProdutos(db, "Bearer "+token, corpo)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
			}
			env := decodeErro(t, w.Body.Bytes())
			if env.Error.Code != "VALIDATION_ERROR" {
				t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
			}
		})
	}
}

// TestCriarProdutoHandler_400CategoriaOuEstoqueInexistente prova a AC2 na
// fronteira: `categoria_id`/`estoque_id` com UUID válido mas sem linha
// correspondente -> 400 VALIDATION_ERROR, nada gravado.
func TestCriarProdutoHandler_400CategoriaOuEstoqueInexistente(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox", "prod-fk-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "prod-fk-almox@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro FK")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	corpo := `{
		"nome": "Produto Categoria Ausente",
		"categoria_id": "00000000-0000-4000-8000-000000000000",
		"estoque_id": "` + estoque.ID + `",
		"quantidade_inicial": 1
	}`
	w := postProdutos(db, "Bearer "+token, corpo)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// TestCriarProdutoHandler_403ParaUsuario prova a AC3: papel `usuario`
// chamando POST /api/produtos direto -> 403 FORBIDDEN, corpo do envelope
// (nunca `{"produto":...}`), e nada é gravado.
func TestCriarProdutoHandler_403ParaUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.004")
	criarContaComPapel(t, db, "Usuária", "prod-forb-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "prod-forb-usuario@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Proibido Produto")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	corpo := `{"nome":"Produto Proibido","categoria_id":"` + categoriaID + `","estoque_id":"` + estoque.ID + `","quantidade_inicial":1}`
	w := postProdutos(db, "Bearer "+token, corpo)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "FORBIDDEN" {
		t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
	}
	if strings.Contains(w.Body.String(), `"produto"`) {
		t.Errorf("corpo do 403 contém \"produto\": %s", w.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM produtos`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("linhas = %d, want 0 (handler não deve ter executado)", n)
	}
}

// TestCriarProdutoHandler_401SemToken prova a linha "sem autenticação" da I/O
// Matrix: RequireAuth responde 401 antes de RequireRole rodar.
func TestCriarProdutoHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)

	w := postProdutos(db, "", `{"nome":"Produto X"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestListarCategoriasHandler_200PorQualquerPapel prova a AC4: qualquer conta
// autenticada — inclusive `usuario` — recebe 200 com as 25 categorias fixas.
func TestListarCategoriasHandler_200PorQualquerPapel(t *testing.T) {
	db := testDB(t)

	for _, papel := range []string{"usuario", "almoxarife", "gestor", "adm"} {
		t.Run(papel, func(t *testing.T) {
			email := "cat-" + papel + "@empresa.com"
			criarContaComPapel(t, db, "Conta "+papel, email, "senha-123456", papel)
			token := tokenDeLogin(t, db, email, "senha-123456")

			w := getCategorias(db, "Bearer "+token)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
			}
			var resp struct {
				Categorias []map[string]any `json:"categorias"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
			}
			if len(resp.Categorias) != 25 {
				t.Errorf("len(categorias) = %d, want 25", len(resp.Categorias))
			}
		})
	}
}

// TestListarCategoriasHandler_401SemToken prova que GET /api/categorias sem
// Authorization -> 401 (RequireAuth), embora não leve RequireRole.
func TestListarCategoriasHandler_401SemToken(t *testing.T) {
	db := testDB(t)

	w := getCategorias(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// --- Story 3.2: Nomenclatura Guiada por subtipo -----------------------------

// TestCriarProdutoHandler_201ComTemplateValido prova a AC1 na fronteira: um
// `template_id` válido + `nome` preenchendo todos os placeholders -> 201.
func TestCriarProdutoHandler_201ComTemplateValido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	templateID := templateIDPorSubtipoHandler(t, db, "Tubo — PEAD/PPR")
	criarContaComPapel(t, db, "Almox", "prod-tpl-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "prod-tpl-almox@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Template Válido")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	corpo := `{
		"nome": "TUBO PEAD PN80 DN50",
		"categoria_id": "` + categoriaID + `",
		"estoque_id": "` + estoque.ID + `",
		"template_id": "` + templateID + `",
		"quantidade_inicial": 1
	}`
	w := postProdutos(db, "Bearer "+token, corpo)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
}

// TestCriarProdutoHandler_400TemplateNaoCorrespondeAoNome prova a AC2 na
// fronteira: `nome` que não preenche o formato do `template_id` selecionado
// -> 400 VALIDATION_ERROR, nada gravado.
func TestCriarProdutoHandler_400TemplateNaoCorrespondeAoNome(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	templateID := templateIDPorSubtipoHandler(t, db, "Tubo — PEAD/PPR")
	criarContaComPapel(t, db, "Almox", "prod-tpl-invalido-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "prod-tpl-invalido-almox@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Template Inválido")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	corpo := `{
		"nome": "TUBO PEAD PN80",
		"categoria_id": "` + categoriaID + `",
		"estoque_id": "` + estoque.ID + `",
		"template_id": "` + templateID + `",
		"quantidade_inicial": 1
	}`
	w := postProdutos(db, "Bearer "+token, corpo)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM produtos`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("linhas em produtos = %d, want 0", n)
	}
}

// TestCriarProdutoHandler_400TemplateInexistente prova a linha "Cadastro com
// template_id inexistente" da I/O Matrix na fronteira HTTP: UUID válido sem
// linha em nomenclatura_templates -> 400 VALIDATION_ERROR "template
// selecionado não existe", nada gravado. TestCriarProduto_TemplateInexistente
// (produtos_test.go) já prova o mesmo na camada de serviço; este teste fecha
// a lacuna no boundary HTTP.
func TestCriarProdutoHandler_400TemplateInexistente(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	criarContaComPapel(t, db, "Almox", "prod-tpl-inexistente-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "prod-tpl-inexistente-almox@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Template Inexistente")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	corpo := `{
		"nome": "TUBO PEAD PN80 DN50",
		"categoria_id": "` + categoriaID + `",
		"estoque_id": "` + estoque.ID + `",
		"template_id": "00000000-0000-4000-8000-000000000000",
		"quantidade_inicial": 1
	}`
	w := postProdutos(db, "Bearer "+token, corpo)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, "template selecionado não existe") {
		t.Errorf("message = %q, want conter %q", env.Error.Message, "template selecionado não existe")
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM produtos`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("linhas em produtos = %d, want 0", n)
	}
}

// TestListarNomenclaturaTemplatesHandler_200PorQualquerPapel prova que
// qualquer conta autenticada — inclusive `usuario` — recebe 200 com os 28
// templates fixos.
func TestListarNomenclaturaTemplatesHandler_200PorQualquerPapel(t *testing.T) {
	db := testDB(t)

	for _, papel := range []string{"usuario", "almoxarife", "gestor", "adm"} {
		t.Run(papel, func(t *testing.T) {
			email := "tpl-" + papel + "@empresa.com"
			criarContaComPapel(t, db, "Conta "+papel, email, "senha-123456", papel)
			token := tokenDeLogin(t, db, email, "senha-123456")

			w := getNomenclaturaTemplates(db, "Bearer "+token)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
			}
			var resp struct {
				Templates []map[string]any `json:"templates"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
			}
			if len(resp.Templates) != 28 {
				t.Errorf("len(templates) = %d, want 28", len(resp.Templates))
			}

			var subtipoAnterior string
			for i, tpl := range resp.Templates {
				id, _ := tpl["id"].(string)
				subtipo, _ := tpl["subtipo"].(string)
				template, _ := tpl["template"].(string)
				if id == "" || subtipo == "" || template == "" {
					t.Errorf("templates[%d] com campo vazio: %+v", i, tpl)
				}
				if i > 0 && subtipo < subtipoAnterior {
					t.Errorf("templates fora de ordem: %q veio depois de %q, want ORDER BY subtipo ASC", subtipo, subtipoAnterior)
				}
				subtipoAnterior = subtipo
			}
		})
	}
}

// TestListarNomenclaturaTemplatesHandler_401SemToken prova que GET
// /api/nomenclatura-templates sem Authorization -> 401.
func TestListarNomenclaturaTemplatesHandler_401SemToken(t *testing.T) {
	db := testDB(t)

	w := getNomenclaturaTemplates(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestAtualizarNomeProdutoHandler_200AlmoxarifeSucesso prova a AC3/AC4 na
// fronteira: `almoxarife` renomeando um Produto sem template com um nome
// válido -> 200 {"produto":{"id","nome"}}.
func TestAtualizarNomeProdutoHandler_200AlmoxarifeSucesso(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.002")
	criarContaComPapel(t, db, "Almox", "renomear-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "renomear-almox@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Renomear Handler")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	produto, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome:              "Nome Original",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	w := postRenomear(db, "Bearer "+token, produto.ID, `{"nome":"Nome Renomeado"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		Produto map[string]any `json:"produto"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Produto["nome"] != "Nome Renomeado" {
		t.Errorf("produto.nome = %v, want %q", resp.Produto["nome"], "Nome Renomeado")
	}
}

// TestAtualizarNomeProdutoHandler_400NomeIncompativelComTemplate prova a AC3:
// Produto com template aplicado, novo nome que não bate mais com o template
// -> 400 VALIDATION_ERROR, nome no banco permanece o anterior.
func TestAtualizarNomeProdutoHandler_400NomeIncompativelComTemplate(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.003")
	templateID := templateIDPorSubtipoHandler(t, db, "Tubo — PEAD/PPR")
	criarContaComPapel(t, db, "Almox", "renomear-tpl-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "renomear-tpl-almox@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Renomear Template Handler")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	produto, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome:              "TUBO PEAD PN80 DN50",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		TemplateID:        templateID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	w := postRenomear(db, "Bearer "+token, produto.ID, `{"nome":"TUBO PEAD PN80"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}

	var nomeGravado string
	if err := db.QueryRow(`SELECT nome FROM produtos WHERE id = $1`, produto.ID).Scan(&nomeGravado); err != nil {
		t.Fatalf("falha ao ler produto: %v", err)
	}
	if nomeGravado != "TUBO PEAD PN80 DN50" {
		t.Errorf("nome gravado = %q, want o nome original preservado", nomeGravado)
	}
}

// TestAtualizarNomeProdutoHandler_404IDInexistente prova que um `id` sem
// linha correspondente -> 404 NOT_FOUND.
func TestAtualizarNomeProdutoHandler_404IDInexistente(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Almox", "renomear-404-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "renomear-404-almox@empresa.com", "senha-123456")

	w := postRenomear(db, "Bearer "+token, "00000000-0000-4000-8000-000000000000", `{"nome":"Nome Qualquer"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
	}
}

// TestAtualizarNomeProdutoHandler_400PayloadInvalido prova que um corpo JSON
// malformado em POST /api/produtos/{id}/renomear -> 400 VALIDATION_ERROR
// "payload inválido", sem chegar a tocar o banco.
func TestAtualizarNomeProdutoHandler_400PayloadInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.002")
	criarContaComPapel(t, db, "Almox", "renomear-payload-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "renomear-payload-almox@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Renomear Payload Inválido")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	produto, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome:              "Nome Original",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	w := postRenomear(db, "Bearer "+token, produto.ID, `{"nome":`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}

	var nomeGravado string
	if err := db.QueryRow(`SELECT nome FROM produtos WHERE id = $1`, produto.ID).Scan(&nomeGravado); err != nil {
		t.Fatalf("falha ao ler produto: %v", err)
	}
	if nomeGravado != "Nome Original" {
		t.Errorf("nome gravado = %q, want %q (nada deveria ter sido alterado)", nomeGravado, "Nome Original")
	}
}

// TestAtualizarNomeProdutoHandler_403ParaUsuario prova a AC5: papel `usuario`
// chamando POST /api/produtos/{id}/renomear direto -> 403 FORBIDDEN, nada é
// gravado.
func TestAtualizarNomeProdutoHandler_403ParaUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.004")
	criarContaComPapel(t, db, "Usuária", "renomear-forb-usuario@empresa.com", "senha-123456", "usuario")
	tokenUsuario := tokenDeLogin(t, db, "renomear-forb-usuario@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Renomear Proibido")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	produto, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome:              "Nome Original",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	w := postRenomear(db, "Bearer "+tokenUsuario, produto.ID, `{"nome":"Nome Trocado Por Usuario"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "FORBIDDEN" {
		t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
	}

	var nomeGravado string
	if err := db.QueryRow(`SELECT nome FROM produtos WHERE id = $1`, produto.ID).Scan(&nomeGravado); err != nil {
		t.Fatalf("falha ao ler produto: %v", err)
	}
	if nomeGravado != "Nome Original" {
		t.Errorf("nome gravado = %q, want %q (nada deveria ter sido alterado)", nomeGravado, "Nome Original")
	}
}

// --- Story 4.1: Busca por nome/código/categoria com sugestões --------------

// getProdutosBusca despacha GET /api/produtos/busca?q=<termo> pela MESMA
// composição de newMux (RequireAuth apenas, SEM RequireRole — Story 4.1).
func getProdutosBusca(db *sql.DB, authHeader, termo string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/produtos/busca",
		middleware.RequireAuth(db, testJWTSecret)(
			BuscarProdutosHandler(db)))
	r := httptest.NewRequest(http.MethodGet, "/api/produtos/busca?q="+url.QueryEscape(termo), nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// TestBuscarProdutosHandler_200ComResultados prova que um termo com match ->
// 200 e o envelope `{"produtos":[...]}` traz o Produto esperado, incluindo a
// Categoria completa.
func TestBuscarProdutosHandler_200ComResultados(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	criarContaComPapel(t, db, "Buscadora", "busca-200@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "busca-200@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Busca Handler")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	produto, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome:              "Parafuso Sextavado M8",
		Codigo:            "PAR-BUSCA-1",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	w := getProdutosBusca(db, "Bearer "+token, "parafuso")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		Produtos []struct {
			ID        string  `json:"id"`
			Nome      string  `json:"nome"`
			Codigo    *string `json:"codigo"`
			Categoria struct {
				ID     string `json:"id"`
				Codigo string `json:"codigo"`
				Nome   string `json:"nome"`
			} `json:"categoria"`
		} `json:"produtos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Produtos) != 1 {
		t.Fatalf("len(produtos) = %d, want 1", len(resp.Produtos))
	}
	if resp.Produtos[0].ID != produto.ID {
		t.Errorf("ID = %q, want %q", resp.Produtos[0].ID, produto.ID)
	}
	if resp.Produtos[0].Codigo == nil || *resp.Produtos[0].Codigo != "PAR-BUSCA-1" {
		t.Errorf("Codigo = %v, want PAR-BUSCA-1", resp.Produtos[0].Codigo)
	}
	if resp.Produtos[0].Categoria.ID != categoriaID {
		t.Errorf("Categoria.ID = %q, want %q", resp.Produtos[0].Categoria.ID, categoriaID)
	}
}

// TestBuscarProdutosHandler_200SemResultados prova que um termo sem nenhum
// match -> 200 com `{"produtos":[]}` (nunca `null`).
func TestBuscarProdutosHandler_200SemResultados(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Buscadora Vazio", "busca-vazio@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "busca-vazio@empresa.com", "senha-123456")

	w := getProdutosBusca(db, "Bearer "+token, "xyzxyz-inexistente")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"produtos":[]`) {
		t.Errorf("body = %s, want produtos:[] (slice vazio, nunca null)", w.Body.String())
	}
}

// TestBuscarProdutosHandler_400TermoVazio prova a linha "Termo vazio/só
// espaços" da matriz: `q` ausente ou só espaços -> 400 VALIDATION_ERROR,
// nenhuma consulta ao banco.
func TestBuscarProdutosHandler_400TermoVazio(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Buscadora Termo Vazio", "busca-termo-vazio@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "busca-termo-vazio@empresa.com", "senha-123456")

	for _, termo := range []string{"", "   "} {
		w := getProdutosBusca(db, "Bearer "+token, termo)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("termo=%q: status = %d, want %d (body=%s)", termo, w.Code, http.StatusBadRequest, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "VALIDATION_ERROR" {
			t.Errorf("termo=%q: code = %q, want VALIDATION_ERROR", termo, env.Error.Code)
		}
	}
}

// TestBuscarProdutosHandler_400TermoMuitoLongo prova que `q` (trimado) com
// mais de 255 runes -> 400 VALIDATION_ERROR "termo de busca muito longo",
// mesmo teto aplicado a `nome`/`codigo` por services.CriarProduto — nenhuma
// consulta ao banco acontece.
func TestBuscarProdutosHandler_400TermoMuitoLongo(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Buscadora Termo Longo", "busca-termo-longo@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "busca-termo-longo@empresa.com", "senha-123456")

	termoMuitoLongo := strings.Repeat("a", 256)
	w := getProdutosBusca(db, "Bearer "+token, termoMuitoLongo)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
	if env.Error.Message != "termo de busca muito longo" {
		t.Errorf("message = %q, want %q", env.Error.Message, "termo de busca muito longo")
	}
}

// TestBuscarProdutosHandler_401SemToken prova que sem Authorization -> 401
// (RequireAuth), embora a rota não leve RequireRole.
func TestBuscarProdutosHandler_401SemToken(t *testing.T) {
	db := testDB(t)

	w := getProdutosBusca(db, "", "qualquer")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestBuscarProdutosHandler_200ParaUsuario prova a linha "Papel usuario
// chama a rota direto pela API" da matriz: token `usuario` -> 200, NUNCA
// 403 — a rota não leva RequireRole (mesmo padrão de GET /api/categorias).
func TestBuscarProdutosHandler_200ParaUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Papel Usuario", "busca-papel-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "busca-papel-usuario@empresa.com", "senha-123456")

	w := getProdutosBusca(db, "Bearer "+token, "qualquer-termo")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s) — rota não deveria exigir RequireRole", w.Code, http.StatusOK, w.Body.String())
	}
}
