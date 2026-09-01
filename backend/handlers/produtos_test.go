package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"stockflow/backend/middleware"
	"stockflow/backend/realtime"
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
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
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
				CriarProdutoHandler(db, realtime.NewRegistry()))))
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
				AtualizarNomeProdutoHandler(db, realtime.NewRegistry()))))
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

// --- Story 4.3: Visualização em grade e tabela agrupada -------------------

// getProdutosCatalogo despacha GET /api/produtos/catalogo pela MESMA
// composição de newMux (RequireAuth apenas, SEM RequireRole — Story 4.3).
// `query` é a query string já montada (sem o `?`), ex. "agrupar=true&pagina=2".
func getProdutosCatalogo(db *sql.DB, authHeader, query string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/produtos/catalogo",
		middleware.RequireAuth(db, testJWTSecret)(
			ListarCatalogoHandler(db)))
	alvo := "/api/produtos/catalogo"
	if query != "" {
		alvo += "?" + query
	}
	r := httptest.NewRequest(http.MethodGet, alvo, nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// seedProdutoCatalogoHandler cadastra um Produto mínimo para os testes de
// ListarCatalogoHandler.
func seedProdutoCatalogoHandler(t *testing.T, db *sql.DB, estoqueID, nome, categoriaID string, qtd float64) string {
	t.Helper()
	p, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome:              nome,
		CategoriaID:       categoriaID,
		EstoqueID:         estoqueID,
		QuantidadeInicial: qtd,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto(%q): %v", nome, err)
	}
	return p.ID
}

// TestListarCatalogoHandler_200Grade prova a linha "Grade, página 1": sem
// `agrupar` -> 200 com o envelope `{"produtos":[...],"paginacao":{...}}`.
func TestListarCatalogoHandler_200Grade(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	criarContaComPapel(t, db, "Catalogo Grade", "catalogo-grade@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "catalogo-grade@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Catalogo Handler Grade")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	seedProdutoCatalogoHandler(t, db, estoque.ID, "Alfa", categoriaID, 5)
	seedProdutoCatalogoHandler(t, db, estoque.ID, "Beta", categoriaID, 0)

	w := getProdutosCatalogo(db, "Bearer "+token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		Produtos []struct {
			Nome            string  `json:"nome"`
			QuantidadeTotal float64 `json:"quantidadeTotal"`
			Disponivel      bool    `json:"disponivel"`
			Categoria       struct {
				Nome string `json:"nome"`
			} `json:"categoria"`
		} `json:"produtos"`
		Paginacao struct {
			Pagina       int `json:"pagina"`
			Tamanho      int `json:"tamanho"`
			Total        int `json:"total"`
			TotalPaginas int `json:"totalPaginas"`
		} `json:"paginacao"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Produtos) != 2 {
		t.Fatalf("len(produtos) = %d, want 2", len(resp.Produtos))
	}
	if resp.Produtos[0].Nome != "Alfa" || resp.Produtos[0].QuantidadeTotal != 5 || !resp.Produtos[0].Disponivel {
		t.Errorf("produtos[0] = %+v, want Alfa/5/true", resp.Produtos[0])
	}
	if resp.Produtos[1].Nome != "Beta" || resp.Produtos[1].Disponivel {
		t.Errorf("produtos[1] = %+v, want Beta/disponivel false", resp.Produtos[1])
	}
	if resp.Paginacao != (struct {
		Pagina       int `json:"pagina"`
		Tamanho      int `json:"tamanho"`
		Total        int `json:"total"`
		TotalPaginas int `json:"totalPaginas"`
	}{Pagina: 1, Tamanho: 24, Total: 2, TotalPaginas: 1}) {
		t.Errorf("paginacao = %+v, want {1 24 2 1}", resp.Paginacao)
	}
}

// TestListarCatalogoHandler_200AgrupadoComPorEstoque prova a linha "Tabela
// agrupa por nome+dimensões" pelo handler: `agrupar=true` -> 200 com
// `{"grupos":[...],"paginacao":{...}}`, cada grupo com `porEstoque`.
func TestListarCatalogoHandler_200AgrupadoComPorEstoque(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	criarContaComPapel(t, db, "Catalogo Tabela", "catalogo-tabela@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "catalogo-tabela@empresa.com", "senha-123456")

	estA, err := services.CriarEstoque(db, "Estoque Handler A")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	estB, err := services.CriarEstoque(db, "Estoque Handler B")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	seedProdutoCatalogoHandler(t, db, estA.ID, "Bucha", categoriaID, 10)
	p2 := seedProdutoCatalogoHandler(t, db, estA.ID, "Bucha", categoriaID, 5)
	if _, err := db.Exec(
		`INSERT INTO produto_estoque (produto_id, estoque_id, quantidade) VALUES ($1,$2,$3)`,
		p2, estB.ID, 2,
	); err != nil {
		t.Fatalf("insert produto_estoque: %v", err)
	}

	w := getProdutosCatalogo(db, "Bearer "+token, "agrupar=true")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		Grupos []struct {
			Chave           string  `json:"chave"`
			Nome            string  `json:"nome"`
			QuantidadeTotal float64 `json:"quantidadeTotal"`
			PorEstoque      []struct {
				EstoqueNome string  `json:"estoqueNome"`
				Quantidade  float64 `json:"quantidade"`
			} `json:"porEstoque"`
		} `json:"grupos"`
		Paginacao struct {
			Total int `json:"total"`
		} `json:"paginacao"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Grupos) != 1 || resp.Paginacao.Total != 1 {
		t.Fatalf("grupos = %d, total = %d, want 1/1 (body=%s)", len(resp.Grupos), resp.Paginacao.Total, w.Body.String())
	}
	g := resp.Grupos[0]
	if g.Nome != "Bucha" || g.Chave == "" || g.QuantidadeTotal != 17 {
		t.Errorf("grupo = %+v, want Bucha / chave != '' / qtd 17", g)
	}
	if len(g.PorEstoque) != 2 {
		t.Fatalf("porEstoque len = %d, want 2 (%+v)", len(g.PorEstoque), g.PorEstoque)
	}
	if g.PorEstoque[0].EstoqueNome != "Estoque Handler A" || g.PorEstoque[0].Quantidade != 15 {
		t.Errorf("porEstoque[0] = %+v, want {Estoque Handler A 15}", g.PorEstoque[0])
	}
	if g.PorEstoque[1].EstoqueNome != "Estoque Handler B" || g.PorEstoque[1].Quantidade != 2 {
		t.Errorf("porEstoque[1] = %+v, want {Estoque Handler B 2}", g.PorEstoque[1])
	}
}

// TestListarCatalogoHandler_400PaginaInvalida prova a linha "`pagina`
// inválida": `pagina=0` / `pagina=abc` -> 400 VALIDATION_ERROR "página
// inválida".
func TestListarCatalogoHandler_400PaginaInvalida(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Catalogo Pag", "catalogo-pag@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "catalogo-pag@empresa.com", "senha-123456")

	casos := []string{
		"0", "-1", "abc", "1.5",
		// Acima do teto: sem o guard, "9999999999999999" passa por Atoi e
		// estoura `(pagina-1)*TamanhoPaginaCatalogo` -> OFFSET negativo -> 500.
		"9999999999999999",
		strconv.Itoa(services.MaxPaginaCatalogo + 1),
	}
	for _, pagina := range casos {
		w := getProdutosCatalogo(db, "Bearer "+token, "pagina="+pagina)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("pagina=%q: status = %d, want %d (body=%s)", pagina, w.Code, http.StatusBadRequest, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "VALIDATION_ERROR" || env.Error.Message != "página inválida" {
			t.Errorf("pagina=%q: erro = %+v, want VALIDATION_ERROR / 'página inválida'", pagina, env.Error)
		}
	}
}

// TestListarCatalogoHandler_400AgruparInvalido prova a linha "`agrupar`
// inválido": qualquer valor fora de {true,false,ausente} -> 400
// VALIDATION_ERROR "parâmetro agrupar inválido".
func TestListarCatalogoHandler_400AgruparInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Catalogo Agr", "catalogo-agr@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "catalogo-agr@empresa.com", "senha-123456")

	for _, agrupar := range []string{"talvez", "1", "TRUE", "yes"} {
		w := getProdutosCatalogo(db, "Bearer "+token, "agrupar="+agrupar)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("agrupar=%q: status = %d, want %d (body=%s)", agrupar, w.Code, http.StatusBadRequest, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "VALIDATION_ERROR" || env.Error.Message != "parâmetro agrupar inválido" {
			t.Errorf("agrupar=%q: erro = %+v, want VALIDATION_ERROR / 'parâmetro agrupar inválido'", agrupar, env.Error)
		}
	}
}

// TestListarCatalogoHandler_401SemToken prova que sem Authorization -> 401
// (RequireAuth), embora a rota não leve RequireRole.
func TestListarCatalogoHandler_401SemToken(t *testing.T) {
	db := testDB(t)

	w := getProdutosCatalogo(db, "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestListarCatalogoHandler_200ParaUsuario prova a linha "Papel `usuario`
// chama direto": token `usuario` -> 200, NUNCA 403 — a rota não leva
// RequireRole (mesmo padrão de GET /api/produtos/busca).
func TestListarCatalogoHandler_200ParaUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Catalogo Usuario", "catalogo-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "catalogo-usuario@empresa.com", "senha-123456")

	w := getProdutosCatalogo(db, "Bearer "+token, "agrupar=false&pagina=1")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s) — rota não deveria exigir RequireRole", w.Code, http.StatusOK, w.Body.String())
	}
}

// --- Story 4.2: Filtros por categoria, estoque e disponibilidade ----------

// catalogoRespostaProdutos decodifica o envelope `{"produtos":[...],"paginacao":{...}}`
// devolvido por GET /api/produtos/catalogo (agrupar=false) para os testes de
// filtro do handler.
type catalogoRespostaProdutos struct {
	Produtos []struct {
		ID   string `json:"id"`
		Nome string `json:"nome"`
	} `json:"produtos"`
	Paginacao struct {
		Total int `json:"total"`
	} `json:"paginacao"`
}

func decodeCatalogoProdutos(t *testing.T, w *httptest.ResponseRecorder) catalogoRespostaProdutos {
	t.Helper()
	var resp catalogoRespostaProdutos
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	return resp
}

// TestListarCatalogoHandler_FiltroCategoriaIsolado prova a linha "Filtro por
// categoria isolado" da matriz via HTTP: `?categoriaId=<id>` -> 200 só com
// Produtos dessa categoria.
func TestListarCatalogoHandler_FiltroCategoriaIsolado(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	civil := categoriaIDPorCodigoHandler(t, db, "04.001")
	eletrico := categoriaIDPorCodigoHandler(t, db, "04.002")
	criarContaComPapel(t, db, "Catalogo FiltroCat", "catalogo-filtrocat@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "catalogo-filtrocat@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Handler FiltroCat")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	seedProdutoCatalogoHandler(t, db, estoque.ID, "Cimento", civil, 1)
	seedProdutoCatalogoHandler(t, db, estoque.ID, "Cabo Flexível", eletrico, 1)

	w := getProdutosCatalogo(db, "Bearer "+token, "categoriaId="+civil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeCatalogoProdutos(t, w)
	if resp.Paginacao.Total != 1 || len(resp.Produtos) != 1 || resp.Produtos[0].Nome != "Cimento" {
		t.Fatalf("resp = %+v, want só 'Cimento'", resp)
	}
}

// TestListarCatalogoHandler_FiltroEstoqueIsolado prova a linha "Filtro por
// Estoque isolado" da matriz via HTTP.
func TestListarCatalogoHandler_FiltroEstoqueIsolado(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	criarContaComPapel(t, db, "Catalogo FiltroEst", "catalogo-filtroest@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "catalogo-filtroest@empresa.com", "senha-123456")

	estA, err := services.CriarEstoque(db, "Estoque Handler FiltroEst A")
	if err != nil {
		t.Fatalf("seed CriarEstoque A: %v", err)
	}
	estB, err := services.CriarEstoque(db, "Estoque Handler FiltroEst B")
	if err != nil {
		t.Fatalf("seed CriarEstoque B: %v", err)
	}
	seedProdutoCatalogoHandler(t, db, estA.ID, "Só em A", categoriaID, 0)
	seedProdutoCatalogoHandler(t, db, estB.ID, "Só em B", categoriaID, 5)

	w := getProdutosCatalogo(db, "Bearer "+token, "estoqueId="+estA.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeCatalogoProdutos(t, w)
	if resp.Paginacao.Total != 1 || len(resp.Produtos) != 1 || resp.Produtos[0].Nome != "Só em A" {
		t.Fatalf("resp = %+v, want só 'Só em A' (linha existe, quantidade 0 não importa)", resp)
	}
}

// TestListarCatalogoHandler_FiltroComEstoqueIsolado prova a linha "Filtro
// 'Com estoque' isolado" da matriz via HTTP.
func TestListarCatalogoHandler_FiltroComEstoqueIsolado(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	criarContaComPapel(t, db, "Catalogo FiltroDisp", "catalogo-filtrodisp@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "catalogo-filtrodisp@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Handler FiltroDisp")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	seedProdutoCatalogoHandler(t, db, estoque.ID, "Disponível", categoriaID, 3)
	seedProdutoCatalogoHandler(t, db, estoque.ID, "Zerado", categoriaID, 0)

	w := getProdutosCatalogo(db, "Bearer "+token, "comEstoque=true")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeCatalogoProdutos(t, w)
	if resp.Paginacao.Total != 1 || len(resp.Produtos) != 1 || resp.Produtos[0].Nome != "Disponível" {
		t.Fatalf("resp = %+v, want só 'Disponível'", resp)
	}
}

// TestListarCatalogoHandler_TodosFiltrosCombinados prova a linha "Todos os
// filtros + q combinados" da matriz via HTTP: os 4 juntos (E lógico).
func TestListarCatalogoHandler_TodosFiltrosCombinados(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	outraCategoria := categoriaIDPorCodigoHandler(t, db, "04.002")
	criarContaComPapel(t, db, "Catalogo FiltroTodos", "catalogo-filtrotodos@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "catalogo-filtrotodos@empresa.com", "senha-123456")

	estAlvo, err := services.CriarEstoque(db, "Estoque Handler Alvo Todos")
	if err != nil {
		t.Fatalf("seed CriarEstoque alvo: %v", err)
	}
	estOutro, err := services.CriarEstoque(db, "Estoque Handler Outro Todos")
	if err != nil {
		t.Fatalf("seed CriarEstoque outro: %v", err)
	}
	alvo := seedProdutoCatalogoHandler(t, db, estAlvo.ID, "Parafuso Sextavado", categoriaID, 10)
	seedProdutoCatalogoHandler(t, db, estAlvo.ID, "Parafuso Allen", outraCategoria, 10)
	seedProdutoCatalogoHandler(t, db, estOutro.ID, "Parafuso Philips", categoriaID, 10)
	seedProdutoCatalogoHandler(t, db, estAlvo.ID, "Arruela Lisa", categoriaID, 10)

	query := fmt.Sprintf("q=paraf&categoriaId=%s&estoqueId=%s&comEstoque=true", categoriaID, estAlvo.ID)
	w := getProdutosCatalogo(db, "Bearer "+token, query)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeCatalogoProdutos(t, w)
	if resp.Paginacao.Total != 1 || len(resp.Produtos) != 1 || resp.Produtos[0].ID != alvo {
		t.Fatalf("resp = %+v, want só %q (Parafuso Sextavado)", resp, alvo)
	}
}

// TestListarCatalogoHandler_400ComEstoqueInvalido prova a linha "comEstoque
// inválido" da matriz: qualquer valor fora de {true,false,ausente} -> 400
// VALIDATION_ERROR "parâmetro comEstoque inválido".
func TestListarCatalogoHandler_400ComEstoqueInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Catalogo ComEstInv", "catalogo-comestinv@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "catalogo-comestinv@empresa.com", "senha-123456")

	for _, comEstoque := range []string{"talvez", "1", "TRUE", "yes"} {
		w := getProdutosCatalogo(db, "Bearer "+token, "comEstoque="+comEstoque)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("comEstoque=%q: status = %d, want %d (body=%s)", comEstoque, w.Code, http.StatusBadRequest, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "VALIDATION_ERROR" || env.Error.Message != "parâmetro comEstoque inválido" {
			t.Errorf("comEstoque=%q: erro = %+v, want VALIDATION_ERROR / 'parâmetro comEstoque inválido'", comEstoque, env.Error)
		}
	}
}

// TestListarCatalogoHandler_400QMuitoLongo prova a linha "q maior que 255
// runes" da matriz: mesmo teto/mensagem de BuscarProdutosHandler.
func TestListarCatalogoHandler_400QMuitoLongo(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Catalogo QLongo", "catalogo-qlongo@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "catalogo-qlongo@empresa.com", "senha-123456")

	termo := strings.Repeat("a", 256)
	w := getProdutosCatalogo(db, "Bearer "+token, "q="+termo)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" || env.Error.Message != "termo de busca muito longo" {
		t.Errorf("erro = %+v, want VALIDATION_ERROR / 'termo de busca muito longo'", env.Error)
	}
}

// TestListarCatalogoHandler_200VazioParaIDMalformado prova a linha
// "categoriaId/estoqueId malformado (não-UUID)" da matriz via HTTP: NUNCA
// 500, sempre 200 com lista vazia.
func TestListarCatalogoHandler_200VazioParaIDMalformado(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	criarContaComPapel(t, db, "Catalogo IDMalformado", "catalogo-idmalformado@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "catalogo-idmalformado@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Handler IDMalformado")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	seedProdutoCatalogoHandler(t, db, estoque.ID, "Qualquer", categoriaID, 1)

	for _, query := range []string{"categoriaId=abc", "estoqueId=xyz"} {
		w := getProdutosCatalogo(db, "Bearer "+token, query)
		if w.Code != http.StatusOK {
			t.Fatalf("query=%q: status = %d, want %d (body=%s)", query, w.Code, http.StatusOK, w.Body.String())
		}
		resp := decodeCatalogoProdutos(t, w)
		if resp.Paginacao.Total != 0 || len(resp.Produtos) != 0 {
			t.Errorf("query=%q: resp = %+v, want lista vazia / total 0", query, resp)
		}
	}
}

// TestListarCatalogoHandler_FiltroComAgrupar prova que `agrupar=true`
// combina com um filtro ponta a ponta pelo HANDLER (não só na camada de
// serviço, já coberta em catalogo_test.go) — a query string precisa chegar
// intacta até `ListarCatalogoAgrupado` pelo mesmo caminho de parsing usado
// no modo grade. Achado pelo Blind Hunter na revisão desta story.
func TestListarCatalogoHandler_FiltroComAgrupar(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	civil := categoriaIDPorCodigoHandler(t, db, "04.001")
	eletrico := categoriaIDPorCodigoHandler(t, db, "04.002")
	criarContaComPapel(t, db, "Catalogo FiltroAgrupar", "catalogo-filtroagrupar@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "catalogo-filtroagrupar@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Handler FiltroAgrupar")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	seedProdutoCatalogoHandler(t, db, estoque.ID, "Bucha", civil, 10)
	seedProdutoCatalogoHandler(t, db, estoque.ID, "Cabo Flexível", eletrico, 10)

	w := getProdutosCatalogo(db, "Bearer "+token, "agrupar=true&categoriaId="+civil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		Grupos []struct {
			Nome string `json:"nome"`
		} `json:"grupos"`
		Paginacao struct {
			Total int `json:"total"`
		} `json:"paginacao"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Paginacao.Total != 1 || len(resp.Grupos) != 1 || resp.Grupos[0].Nome != "Bucha" {
		t.Fatalf("resp = %+v, want só o grupo 'Bucha'", resp)
	}
}

// --- Story 4.6: Exportação da tabela do catálogo para Excel ---------------

// getProdutosCatalogoExportar despacha GET /api/produtos/catalogo/exportar
// pela MESMA composição de newMux (RequireAuth -> RequireRole(almoxarife) —
// Story 4.6). `query` é a query string já montada (sem o `?`).
func getProdutosCatalogoExportar(db *sql.DB, authHeader, query string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/produtos/catalogo/exportar",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				ExportarCatalogoHandler(db))))
	alvo := "/api/produtos/catalogo/exportar"
	if query != "" {
		alvo += "?" + query
	}
	r := httptest.NewRequest(http.MethodGet, alvo, nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// TestExportarCatalogoHandler_200ComHeadersEXLSXValido prova a linha
// "Exportação com grupos e Estoques" da matriz: 200, `Content-Type`/
// `Content-Disposition` de download corretos e um `.xlsx` válido
// (excelize.OpenReader no corpo) refletindo o Produto cadastrado.
func TestExportarCatalogoHandler_200ComHeadersEXLSXValido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	criarContaComPapel(t, db, "Exportar Almox", "exportar-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "exportar-almox@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Exportar Handler")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	seedProdutoCatalogoHandler(t, db, estoque.ID, "Exportar Handler Produto", categoriaID, 7)

	w := getProdutosCatalogoExportar(db, "Bearer "+token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	wantContentType := "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	if ct := w.Header().Get("Content-Type"); ct != wantContentType {
		t.Errorf("Content-Type = %q, want %q", ct, wantContentType)
	}
	wantDisposition := `attachment; filename="catalogo.xlsx"`
	if cd := w.Header().Get("Content-Disposition"); cd != wantDisposition {
		t.Errorf("Content-Disposition = %q, want %q", cd, wantDisposition)
	}

	f, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("corpo não é um .xlsx válido: %v", err)
	}
	defer f.Close()
	linhas, err := f.GetRows(f.GetSheetList()[0])
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	// cabeçalho + 1 detalhe + subtotal + total geral = 4.
	if len(linhas) != 4 || linhas[1][0] != "Exportar Handler Produto" {
		t.Fatalf("linhas = %+v, want 4 linhas com 'Exportar Handler Produto' na linha de detalhe", linhas)
	}
}

// TestExportarCatalogoHandler_400TermoMuitoLongo prova a linha "`q` > 255
// runes" da matriz.
func TestExportarCatalogoHandler_400TermoMuitoLongo(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Exportar Termo Longo", "exportar-termo-longo@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "exportar-termo-longo@empresa.com", "senha-123456")

	termo := strings.Repeat("a", 256)
	w := getProdutosCatalogoExportar(db, "Bearer "+token, "q="+termo)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" || env.Error.Message != "termo de busca muito longo" {
		t.Errorf("erro = %+v, want VALIDATION_ERROR / 'termo de busca muito longo'", env.Error)
	}
}

// TestExportarCatalogoHandler_400ComEstoqueInvalido prova a linha
// "`comEstoque` inválido" da matriz.
func TestExportarCatalogoHandler_400ComEstoqueInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Exportar ComEstoque Invalido", "exportar-comestoque-invalido@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "exportar-comestoque-invalido@empresa.com", "senha-123456")

	w := getProdutosCatalogoExportar(db, "Bearer "+token, "comEstoque=talvez")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" || env.Error.Message != "parâmetro comEstoque inválido" {
		t.Errorf("erro = %+v, want VALIDATION_ERROR / 'parâmetro comEstoque inválido'", env.Error)
	}
}

// TestExportarCatalogoHandler_403ParaUsuario prova a linha "Papel `usuario`
// chama direto" da matriz: RequireRole(almoxarife) barra o papel `usuario`
// antes do handler rodar.
func TestExportarCatalogoHandler_403ParaUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Exportar Usuario", "exportar-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "exportar-usuario@empresa.com", "senha-123456")

	w := getProdutosCatalogoExportar(db, "Bearer "+token, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "FORBIDDEN" {
		t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
	}
}

// TestExportarCatalogoHandler_FiltrosRepassadosAoService prova que os
// filtros da query string chegam intactos a services.GerarCatalogoXLSX pelo
// mesmo caminho de parsing de ListarCatalogoHandler: `categoriaId` filtra o
// Produto certo, deixando o outro (categoria diferente) de fora do .xlsx.
func TestExportarCatalogoHandler_FiltrosRepassadosAoService(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	civil := categoriaIDPorCodigoHandler(t, db, "04.001")
	eletrico := categoriaIDPorCodigoHandler(t, db, "04.002")
	criarContaComPapel(t, db, "Exportar Filtro", "exportar-filtro@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "exportar-filtro@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Exportar Filtro Handler")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	seedProdutoCatalogoHandler(t, db, estoque.ID, "Exportar Filtro Civil", civil, 3)
	seedProdutoCatalogoHandler(t, db, estoque.ID, "Exportar Filtro Eletrico", eletrico, 3)

	w := getProdutosCatalogoExportar(db, "Bearer "+token, "categoriaId="+civil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	f, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("corpo não é um .xlsx válido: %v", err)
	}
	defer f.Close()
	linhas, err := f.GetRows(f.GetSheetList()[0])
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(linhas) != 4 || linhas[1][0] != "Exportar Filtro Civil" {
		t.Fatalf("linhas = %+v, want só o Produto 'Exportar Filtro Civil' (filtro categoriaId)", linhas)
	}
}

// --- Story 4.4: Detalhe do produto por Estoque com atualização em tempo
// real -----------------------------------------------------------------

// getProdutoDetalhe despacha GET /api/produtos/{id} pela MESMA composição de
// newMux (RequireAuth apenas, SEM RequireRole — Story 4.4).
func getProdutoDetalhe(db *sql.DB, authHeader, id string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/produtos/{id}",
		middleware.RequireAuth(db, testJWTSecret)(
			ObterProdutoHandler(db)))
	r := httptest.NewRequest(http.MethodGet, "/api/produtos/"+id, nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// TestObterProdutoHandler_200ComPorEstoque prova a linha "Detalhe de Produto
// existente": 200 com `porEstoque` discriminado, mesmos tipos/formatação de
// ListarCatalogoHandler.
func TestObterProdutoHandler_200ComPorEstoque(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	criarContaComPapel(t, db, "Detalhe Handler", "detalhe-handler@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "detalhe-handler@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Detalhe Handler")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	produtoID := seedProdutoCatalogoHandler(t, db, estoque.ID, "Produto Detalhe Handler", categoriaID, 7)

	w := getProdutoDetalhe(db, "Bearer "+token, produtoID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		Produto struct {
			ID              string  `json:"id"`
			Nome            string  `json:"nome"`
			QuantidadeTotal float64 `json:"quantidadeTotal"`
			Disponivel      bool    `json:"disponivel"`
			PorEstoque      []struct {
				EstoqueID   string  `json:"estoqueId"`
				EstoqueNome string  `json:"estoqueNome"`
				Quantidade  float64 `json:"quantidade"`
			} `json:"porEstoque"`
		} `json:"produto"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Produto.ID != produtoID || resp.Produto.Nome != "Produto Detalhe Handler" {
		t.Fatalf("produto = %+v", resp.Produto)
	}
	if resp.Produto.QuantidadeTotal != 7 || !resp.Produto.Disponivel {
		t.Errorf("quantidadeTotal = %v, disponivel = %v, want 7/true", resp.Produto.QuantidadeTotal, resp.Produto.Disponivel)
	}
	if len(resp.Produto.PorEstoque) != 1 || resp.Produto.PorEstoque[0].EstoqueNome != "Canteiro Detalhe Handler" {
		t.Fatalf("porEstoque = %+v", resp.Produto.PorEstoque)
	}
}

// TestObterProdutoHandler_404IDInexistenteOuMalformado prova a linha "id
// inexistente/malformado" da matriz: 404 NOT_FOUND "produto não encontrado"
// nos dois casos.
func TestObterProdutoHandler_404IDInexistenteOuMalformado(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Detalhe 404", "detalhe-404@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "detalhe-404@empresa.com", "senha-123456")

	for _, id := range []string{"00000000-0000-0000-0000-000000000000", "nao-e-um-uuid"} {
		w := getProdutoDetalhe(db, "Bearer "+token, id)
		if w.Code != http.StatusNotFound {
			t.Fatalf("id=%q: status = %d, want %d (body=%s)", id, w.Code, http.StatusNotFound, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "NOT_FOUND" || env.Error.Message != "produto não encontrado" {
			t.Errorf("id=%q: erro = %+v, want NOT_FOUND / 'produto não encontrado'", id, env.Error)
		}
	}
}

// TestObterProdutoHandler_401SemToken prova que sem Authorization -> 401
// (RequireAuth).
func TestObterProdutoHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	w := getProdutoDetalhe(db, "", "00000000-0000-0000-0000-000000000000")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestObterProdutoHandler_200ParaUsuario prova a linha "Papel `usuario` chama
// direto": token `usuario` -> 200, NUNCA 403 — a rota não leva RequireRole.
func TestObterProdutoHandler_200ParaUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	criarContaComPapel(t, db, "Detalhe Papel Usuario", "detalhe-papel-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "detalhe-papel-usuario@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro Detalhe Papel Usuario")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	produtoID := seedProdutoCatalogoHandler(t, db, estoque.ID, "Produto Detalhe Papel Usuario", categoriaID, 1)

	w := getProdutoDetalhe(db, "Bearer "+token, produtoID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s) — rota não deveria exigir RequireRole", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestCriarProdutoHandler_PublicaEventoNoSucesso prova a linha "CriarProduto
// bem-sucedido" da matriz de spec-4-4: um Produto criado com sucesso publica
// `{"resource":"produtos","id":<novo>,"change":"created"}` no canal
// `produtos` do `*realtime.Registry` injetado no handler.
func TestCriarProdutoHandler_PublicaEventoNoSucesso(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	estoque, err := services.CriarEstoque(db, "Canteiro Evento Criar")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	criarContaComPapel(t, db, "Evento Criar", "evento-criar@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "evento-criar@empresa.com", "senha-123456")

	registro := realtime.NewRegistry()
	eventos, cancelar := registro.Subscribe()
	defer cancelar()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/produtos",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				CriarProdutoHandler(db, registro))))

	body := `{"nome":"Produto Evento","categoria_id":"` + categoriaID + `","estoque_id":"` + estoque.ID + `","quantidade_inicial":1}`
	r := httptest.NewRequest(http.MethodPost, "/api/produtos", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Produto struct {
			ID string `json:"id"`
		} `json:"produto"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	select {
	case ev := <-eventos:
		if ev.Resource != "produtos" || ev.ID != resp.Produto.ID || ev.Change != "created" {
			t.Fatalf("evento = %+v, want {produtos %s created}", ev, resp.Produto.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("nenhum evento publicado em 1s após CriarProdutoHandler bem-sucedido")
	}
}

// TestAtualizarNomeProdutoHandler_PublicaEventoNoSucesso prova a linha
// "AtualizarNomeProduto bem-sucedido" da matriz de spec-4-4: um Produto
// renomeado com sucesso publica
// `{"resource":"produtos","id":<id>,"change":"updated"}`.
func TestAtualizarNomeProdutoHandler_PublicaEventoNoSucesso(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	estoque, err := services.CriarEstoque(db, "Canteiro Evento Renomear")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	produto, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome: "Nome Original Evento", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}
	criarContaComPapel(t, db, "Evento Renomear", "evento-renomear@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "evento-renomear@empresa.com", "senha-123456")

	registro := realtime.NewRegistry()
	eventos, cancelar := registro.Subscribe()
	defer cancelar()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/produtos/{id}/renomear",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				AtualizarNomeProdutoHandler(db, registro))))

	body := `{"nome":"Nome Novo Evento"}`
	r := httptest.NewRequest(http.MethodPost, "/api/produtos/"+produto.ID+"/renomear", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}

	select {
	case ev := <-eventos:
		if ev.Resource != "produtos" || ev.ID != produto.ID || ev.Change != "updated" {
			t.Fatalf("evento = %+v, want {produtos %s updated}", ev, produto.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("nenhum evento publicado em 1s após AtualizarNomeProdutoHandler bem-sucedido")
	}
}

// --- Story 4.5: Identificação de Produto via QR Code / código de barras ----

// getProdutoPorCodigo despacha GET /api/produtos/por-codigo?codigo=<valor>
// pela MESMA composição de newMux (RequireAuth apenas, SEM RequireRole —
// Story 4.5).
func getProdutoPorCodigo(db *sql.DB, authHeader, codigo string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/produtos/por-codigo",
		middleware.RequireAuth(db, testJWTSecret)(
			BuscarProdutoPorCodigoHandler(db)))
	r := httptest.NewRequest(http.MethodGet, "/api/produtos/por-codigo?codigo="+url.QueryEscape(codigo), nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// TestBuscarProdutoPorCodigoHandler_200ComProduto prova a linha "Código
// existente resolvido" da matriz de spec-4-5: `codigo` exato de um Produto
// -> 200 com o envelope `{"produto":{id,nome,codigo,categoria}}` (mesma
// projeção de GET /api/produtos/busca).
func TestBuscarProdutoPorCodigoHandler_200ComProduto(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	criarContaComPapel(t, db, "PorCodigo 200", "porcodigo-200@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "porcodigo-200@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro PorCodigo Handler")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	produto, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome:              "Cabo Flexível 4mm",
		Codigo:            "CAB-004",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	w := getProdutoPorCodigo(db, "Bearer "+token, "CAB-004")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		Produto struct {
			ID        string  `json:"id"`
			Nome      string  `json:"nome"`
			Codigo    *string `json:"codigo"`
			Categoria struct {
				ID     string `json:"id"`
				Codigo string `json:"codigo"`
				Nome   string `json:"nome"`
			} `json:"categoria"`
		} `json:"produto"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Produto.ID != produto.ID || resp.Produto.Nome != "Cabo Flexível 4mm" {
		t.Fatalf("produto = %+v", resp.Produto)
	}
	if resp.Produto.Codigo == nil || *resp.Produto.Codigo != "CAB-004" {
		t.Errorf("Codigo = %v, want CAB-004", resp.Produto.Codigo)
	}
	if resp.Produto.Categoria.ID != categoriaID {
		t.Errorf("Categoria.ID = %q, want %q", resp.Produto.Categoria.ID, categoriaID)
	}
}

// TestBuscarProdutoPorCodigoHandler_400CodigoVazio prova a linha "`codigo`
// vazio / só espaços / ausente" da matriz: 400 VALIDATION_ERROR "código
// obrigatório", nenhuma consulta ao banco.
func TestBuscarProdutoPorCodigoHandler_400CodigoVazio(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "PorCodigo Vazio", "porcodigo-vazio@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "porcodigo-vazio@empresa.com", "senha-123456")

	for _, codigo := range []string{"", "   "} {
		w := getProdutoPorCodigo(db, "Bearer "+token, codigo)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("codigo=%q: status = %d, want %d (body=%s)", codigo, w.Code, http.StatusBadRequest, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "VALIDATION_ERROR" || env.Error.Message != "código obrigatório" {
			t.Errorf("codigo=%q: erro = %+v, want VALIDATION_ERROR / 'código obrigatório'", codigo, env.Error)
		}
	}
}

// TestBuscarProdutoPorCodigoHandler_400CodigoMuitoLongo prova a linha
// "`codigo` > 255 runes" da matriz: 400 VALIDATION_ERROR "código muito
// longo", sem consulta ao banco.
func TestBuscarProdutoPorCodigoHandler_400CodigoMuitoLongo(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "PorCodigo Longo", "porcodigo-longo@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "porcodigo-longo@empresa.com", "senha-123456")

	w := getProdutoPorCodigo(db, "Bearer "+token, strings.Repeat("a", 256))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" || env.Error.Message != "código muito longo" {
		t.Errorf("erro = %+v, want VALIDATION_ERROR / 'código muito longo'", env.Error)
	}
}

// TestBuscarProdutoPorCodigoHandler_404CodigoNaoReconhecido prova a linha
// "Código não cadastrado" da matriz: leitura resolvida mas sem Produto ->
// 404 NOT_FOUND "produto não encontrado".
func TestBuscarProdutoPorCodigoHandler_404CodigoNaoReconhecido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "PorCodigo 404", "porcodigo-404@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "porcodigo-404@empresa.com", "senha-123456")

	w := getProdutoPorCodigo(db, "Bearer "+token, "NAO-EXISTE-999")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "NOT_FOUND" || env.Error.Message != "produto não encontrado" {
		t.Errorf("erro = %+v, want NOT_FOUND / 'produto não encontrado'", env.Error)
	}
}

// TestBuscarProdutoPorCodigoHandler_401SemToken prova que sem Authorization
// -> 401 (RequireAuth), embora a rota não leve RequireRole.
func TestBuscarProdutoPorCodigoHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	w := getProdutoPorCodigo(db, "", "CAB-004")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestBuscarProdutoPorCodigoHandler_200ParaUsuario prova a linha "Papel
// `usuario` chama direto" da matriz: token `usuario` -> 200, NUNCA 403 — a
// rota não leva RequireRole (mesmo padrão de GET /api/produtos/busca).
func TestBuscarProdutoPorCodigoHandler_200ParaUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	criarContaComPapel(t, db, "PorCodigo Papel Usuario", "porcodigo-papel-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "porcodigo-papel-usuario@empresa.com", "senha-123456")

	estoque, err := services.CriarEstoque(db, "Canteiro PorCodigo Papel Usuario")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	if _, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome: "Produto Papel Usuario", Codigo: "PU-001", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 1,
	}); err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	w := getProdutoPorCodigo(db, "Bearer "+token, "PU-001")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s) — rota não deveria exigir RequireRole", w.Code, http.StatusOK, w.Body.String())
	}
}
