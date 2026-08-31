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

// --- despacho pela MESMA composição de newMux (main.go) --------------------
//
// POST /api/produtos -> RequireAuth -> RequireRole(almoxarife) -> handler.
// GET /api/categorias -> RequireAuth -> handler (SEM RequireRole).

func limparProdutosHandler(t *testing.T, db *sql.DB) {
	t.Helper()
	// `produto_estoque` e `produtos` entram na mesma TRUNCATE que `estoques`
	// por causa da FK de produto_estoque.estoque_id -> estoques(id).
	// `categorias` NUNCA é truncada aqui — seed fixo da migração 000010.
	if _, err := db.Exec(`TRUNCATE TABLE produto_estoque, produtos, estoques`); err != nil {
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
