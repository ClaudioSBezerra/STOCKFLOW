package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// --- despacho pela MESMA composição de newMux (main.go) --------------------
//
// GET /api/normalizacao/inconsistencias -> RequireAuth ->
// RequireRole(almoxarife) -> handler. Mesmo molde de getMovimentacoes.

func getInconsistencias(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/normalizacao/inconsistencias",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				AnalisarInconsistenciasHandler(db))))
	r := httptest.NewRequest(http.MethodGet, "/api/normalizacao/inconsistencias", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// seedProdutoComPendenciaHandler cadastra um Produto com `nome` e as
// dimensões estruturadas dadas (`CriarProdutoInput` parcial — CategoriaID/
// EstoqueID/Nome são preenchidos aqui) e devolve seu id.
func seedProdutoComPendenciaHandler(t *testing.T, db *sql.DB, nome string, dims services.CriarProdutoInput) string {
	t.Helper()
	estoque, err := services.CriarEstoque(db, "Estoque "+nome)
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	dims.Nome = nome
	dims.CategoriaID = categoriaIDPorCodigoHandler(t, db, "04.001")
	dims.EstoqueID = estoque.ID
	produto, err := services.CriarProduto(db, dims)
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}
	return produto.ID
}

// TestAnalisarInconsistenciasHandler_200ComSugestoes prova a linha "Nome com
// valor implícito, único campo vazio" da I/O Matrix na fronteira HTTP: 200
// com o formato de fio exato `{"sugestoes":[{"produtoId","produtoNome",
// "campo","valorSugerido":{"valor","unidade"},"origem"}]}`.
func TestAnalisarInconsistenciasHandler_200ComSugestoes(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Normalizacao 200", "normalizacao-200-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "normalizacao-200-almox@empresa.com", "senha-123456")

	produtoID := seedProdutoComPendenciaHandler(t, db, "TUBO PVC 6M", services.CriarProdutoInput{
		Largura:   &services.DimensaoInput{Valor: ptrFloatHandler(100), Unidade: ptrStrHandler("mm")},
		Diametro:  &services.DimensaoInput{Valor: ptrFloatHandler(10), Unidade: ptrStrHandler("cm")},
		Altura:    &services.DimensaoInput{Valor: ptrFloatHandler(2), Unidade: ptrStrHandler("m")},
		Espessura: &services.DimensaoInput{Valor: ptrFloatHandler(5), Unidade: ptrStrHandler("mm")},
	})

	w := getInconsistencias(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	var resp struct {
		Sugestoes []struct {
			ProdutoID     string `json:"produtoId"`
			ProdutoNome   string `json:"produtoNome"`
			Campo         string `json:"campo"`
			ValorSugerido struct {
				Valor   float64 `json:"valor"`
				Unidade string  `json:"unidade"`
			} `json:"valorSugerido"`
			Origem string `json:"origem"`
		} `json:"sugestoes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}

	var encontrada bool
	for _, s := range resp.Sugestoes {
		if s.ProdutoID != produtoID {
			continue
		}
		encontrada = true
		if s.ProdutoNome != "TUBO PVC 6M" {
			t.Errorf("produtoNome = %q, want %q", s.ProdutoNome, "TUBO PVC 6M")
		}
		if s.Campo != "comprimento" {
			t.Errorf("campo = %q, want comprimento", s.Campo)
		}
		if s.ValorSugerido.Valor != 6 || s.ValorSugerido.Unidade != "m" {
			t.Errorf("valorSugerido = %+v, want {6 m}", s.ValorSugerido)
		}
		if s.Origem != "nome" {
			t.Errorf("origem = %q, want nome", s.Origem)
		}
	}
	if !encontrada {
		t.Fatalf("nenhuma sugestão para o produto %s em %+v", produtoID, resp.Sugestoes)
	}
}

// TestAnalisarInconsistenciasHandler_200ListaVazia prova a linha "Catálogo
// sem nenhum Produto pendente": 200 {"sugestoes":[]} (array, nunca null).
func TestAnalisarInconsistenciasHandler_200ListaVazia(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Normalizacao Vazia", "normalizacao-vazia-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "normalizacao-vazia-almox@empresa.com", "senha-123456")

	w := getInconsistencias(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != `{"sugestoes":[]}`+"\n" && body != `{"sugestoes":[]}` {
		t.Errorf("body = %s, want {\"sugestoes\":[]}", body)
	}
}

// TestAnalisarInconsistenciasHandler_403PapelUsuario prova a linha "Chamada
// com papel usuario": 403 FORBIDDEN, decidido por RequireRole — o handler
// nunca executa.
func TestAnalisarInconsistenciasHandler_403PapelUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Usuario Normalizacao 403", "normalizacao-403-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "normalizacao-403-usuario@empresa.com", "senha-123456")

	w := getInconsistencias(db, "Bearer "+token)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "FORBIDDEN" {
		t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
	}
	var comLista map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &comLista)
	if _, tem := comLista["sugestoes"]; tem {
		t.Errorf("corpo do 403 contém \"sugestoes\" — o handler nunca deveria ter executado")
	}
}

// TestAnalisarInconsistenciasHandler_401SemToken prova que uma requisição
// sem Authorization -> 401, produzido só por RequireAuth.
func TestAnalisarInconsistenciasHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)

	w := getInconsistencias(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestAnalisarInconsistenciasHandler_500FalhaDeBanco prova que, com a tabela
// `produtos` indisponível, um `almoxarife` autenticado recebe 500
// INTERNAL_ERROR no envelope AD-14 (a autenticação usa `usuarios`, que
// continua de pé). A tabela é renomeada para fora e restaurada por
// t.Cleanup — mesmo molde de TestListarMovimentacoesHandler_500FalhaDeBanco
// (movimentacoes_test.go), execução serial, sem t.Parallel.
func TestAnalisarInconsistenciasHandler_500FalhaDeBanco(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Normalizacao 500", "normalizacao-500-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "normalizacao-500-almox@empresa.com", "senha-123456")

	if _, err := db.Exec(`ALTER TABLE produtos RENAME TO produtos_indisponivel`); err != nil {
		t.Fatalf("renomear produtos: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`ALTER TABLE produtos_indisponivel RENAME TO produtos`); err != nil {
			t.Fatalf("restaurar produtos: %v", err)
		}
	})

	w := getInconsistencias(db, "Bearer "+token)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("code = %q, want INTERNAL_ERROR", env.Error.Code)
	}
}

func ptrFloatHandler(v float64) *float64 { return &v }
func ptrStrHandler(v string) *string     { return &v }
