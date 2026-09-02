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
// POST /api/estoques -> RequireAuth -> RequireRole(almoxarife) -> handler.
// GET /api/estoques -> RequireAuth -> handler (SEM RequireRole).

// limparEstoquesHandler trunca `importacao_linhas`/`produto_estoque`/
// `produtos`/`estoques` juntos (FK de produto_estoque.estoque_id ->
// estoques(id), Story 3.1, e de importacao_linhas.produto_id -> produtos(id),
// Story 3.3, exigem truncar as quatro na mesma instrução). `categorias` nunca
// é truncada: seed fixo da migração 000010.
func limparEstoquesHandler(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("falha ao limpar estoques: %v", err)
	}
}

func postEstoques(db *sql.DB, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/estoques",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				CriarEstoqueHandler(db))))
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, "/api/estoques", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, "/api/estoques", nil)
	}
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func getEstoques(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/estoques",
		middleware.RequireAuth(db, testJWTSecret)(
			ListarEstoquesHandler(db)))
	r := httptest.NewRequest(http.MethodGet, "/api/estoques", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// deleteEstoques despacha DELETE /api/estoques/{id} pela MESMA composição de
// newMux (RequireAuth -> RequireRole(almoxarife) -> ExcluirEstoqueHandler).
func deleteEstoques(db *sql.DB, authHeader, id string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/estoques/{id}",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				ExcluirEstoqueHandler(db))))
	r := httptest.NewRequest(http.MethodDelete, "/api/estoques/"+id, nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// decodeEstoquesFio decodifica o corpo de GET /api/estoques travando o
// conjunto de chaves de fio: cada elemento tem exatamente `id` e `nome`.
func decodeEstoquesFio(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var resp struct {
		Estoques []map[string]any `json:"estoques"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("falha ao decodificar estoques: %v (body=%s)", err, body)
	}
	for i, e := range resp.Estoques {
		if len(e) != 2 {
			t.Errorf("estoque[%d] tem chaves %v, want exatamente {id, nome}", i, chaves(e))
		}
		if _, ok := e["id"].(string); !ok {
			t.Errorf("estoque[%d].id ausente ou não-string: %v", i, e["id"])
		}
		if _, ok := e["nome"].(string); !ok {
			t.Errorf("estoque[%d].nome ausente ou não-string: %v", i, e["nome"])
		}
	}
	return resp.Estoques
}

func chaves(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// TestCriarEstoqueHandler_201ParaAlmoxarifeGestorAdm prova a AC1 na fronteira
// HTTP: uma sessão `almoxarife`+ com nome válido recebe 201 e o corpo
// `{"estoque":{"id","nome"}}`.
func TestCriarEstoqueHandler_201ParaAlmoxarifeGestorAdm(t *testing.T) {
	db := testDB(t)
	limparEstoquesHandler(t, db)

	casos := []struct{ papel, email, nome string }{
		{"almoxarife", "cria-almox@empresa.com", "Canteiro Almox"},
		{"gestor", "cria-gestor@empresa.com", "Canteiro Gestor"},
		{"adm", "cria-adm@empresa.com", "Canteiro Adm"},
	}
	for _, c := range casos {
		t.Run(c.papel, func(t *testing.T) {
			criarContaComPapel(t, db, "Conta "+c.papel, c.email, "senha-123456", c.papel)
			token := tokenDeLogin(t, db, c.email, "senha-123456")

			w := postEstoques(db, "Bearer "+token, `{"nome":"`+c.nome+`"}`)
			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
			}
			var resp struct {
				Estoque map[string]any `json:"estoque"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
			}
			if id, ok := resp.Estoque["id"].(string); !ok || id == "" {
				t.Errorf("estoque.id ausente/vazio: %v", resp.Estoque["id"])
			}
			if resp.Estoque["nome"] != c.nome {
				t.Errorf("estoque.nome = %v, want %q", resp.Estoque["nome"], c.nome)
			}
		})
	}
}

// TestCriarEstoqueHandler_409NomeDuplicado prova a AC2 na fronteira: nome já
// existente (mesmo com caixa/espaço diferentes) -> 409 CONFLICT, envelope de
// erro.
func TestCriarEstoqueHandler_409NomeDuplicado(t *testing.T) {
	db := testDB(t)
	limparEstoquesHandler(t, db)
	criarContaComPapel(t, db, "Almox", "dup-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "dup-almox@empresa.com", "senha-123456")

	if w := postEstoques(db, "Bearer "+token, `{"nome":"Canteiro A"}`); w.Code != http.StatusCreated {
		t.Fatalf("primeiro cadastro: status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	w := postEstoques(db, "Bearer "+token, `{"nome":"  canteiro   a "}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

// TestCriarEstoqueHandler_400PayloadInvalido prova a linha "nome ausente /
// JSON inválido / corpo > limite / nome em branco" da I/O Matrix: 400
// VALIDATION_ERROR.
func TestCriarEstoqueHandler_400PayloadInvalido(t *testing.T) {
	db := testDB(t)
	limparEstoquesHandler(t, db)
	criarContaComPapel(t, db, "Almox", "val-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "val-almox@empresa.com", "senha-123456")

	// Corpo acima de authRequestMaxBytes (64KB): http.MaxBytesReader faz o
	// Decode falhar antes de qualquer regra de negócio -> 400.
	corpoGrande := `{"nome":"` + strings.Repeat("x", authRequestMaxBytes+1) + `"}`

	casos := map[string]string{
		"nome em branco": `{"nome":"   "}`,
		"nome vazio":     `{"nome":""}`,
		"nome ausente":   `{}`,
		"json inválido":  `{"nome":`,
		"corpo > limite": corpoGrande,
	}
	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			w := postEstoques(db, "Bearer "+token, corpo)
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

// TestCriarEstoqueHandler_403ParaUsuario prova a AC3: papel `usuario` chamando
// POST /api/estoques direto -> 403 FORBIDDEN, corpo do envelope (nunca
// `{"estoque":...}`), e nada é gravado.
func TestCriarEstoqueHandler_403ParaUsuario(t *testing.T) {
	db := testDB(t)
	limparEstoquesHandler(t, db)
	criarContaComPapel(t, db, "Usuária", "forb-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "forb-usuario@empresa.com", "senha-123456")

	w := postEstoques(db, "Bearer "+token, `{"nome":"Canteiro Proibido"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "FORBIDDEN" {
		t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
	}
	if strings.Contains(w.Body.String(), `"estoque"`) {
		t.Errorf("corpo do 403 contém \"estoque\": %s", w.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM estoques`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("linhas = %d, want 0 (handler não deve ter executado)", n)
	}
}

// TestCriarEstoqueHandler_401SemToken prova a linha "sem autenticação" da I/O
// Matrix: RequireAuth responde 401 antes de RequireRole rodar.
func TestCriarEstoqueHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparEstoquesHandler(t, db)

	w := postEstoques(db, "", `{"nome":"Canteiro X"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestListarEstoquesHandler_200PorQualquerPapel prova a AC4: qualquer conta
// autenticada — inclusive `usuario` — recebe 200 com as linhas ordenadas por
// nome normalizado; o conjunto de chaves de fio é exatamente {id, nome}.
func TestListarEstoquesHandler_200PorQualquerPapel(t *testing.T) {
	db := testDB(t)
	limparEstoquesHandler(t, db)

	for _, nome := range []string{"Zinco", "abc", "Manga"} {
		if _, err := services.CriarEstoque(db, nome); err != nil {
			t.Fatalf("seed CriarEstoque(%q): %v", nome, err)
		}
	}

	for _, papel := range []string{"usuario", "almoxarife", "gestor", "adm"} {
		t.Run(papel, func(t *testing.T) {
			email := "lista-" + papel + "@empresa.com"
			criarContaComPapel(t, db, "Conta "+papel, email, "senha-123456", papel)
			token := tokenDeLogin(t, db, email, "senha-123456")

			w := getEstoques(db, "Bearer "+token)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
			}
			estoques := decodeEstoquesFio(t, w.Body.Bytes())
			nomes := make([]string, len(estoques))
			for i, e := range estoques {
				nomes[i] = e["nome"].(string)
			}
			want := []string{"abc", "Manga", "Zinco"}
			if strings.Join(nomes, "|") != strings.Join(want, "|") {
				t.Errorf("ordem = %v, want %v", nomes, want)
			}
		})
	}
}

// TestListarEstoquesHandler_401SemToken prova que GET /api/estoques sem
// Authorization -> 401 (RequireAuth), embora não leve RequireRole.
func TestListarEstoquesHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparEstoquesHandler(t, db)

	w := getEstoques(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestExcluirEstoqueHandler_204ParaAlmoxarifeGestorAdm prova a AC1 na fronteira
// HTTP: uma sessão `almoxarife`+ excluindo um Estoque existente recebe 204 sem
// corpo e a linha some de `estoques`.
func TestExcluirEstoqueHandler_204ParaAlmoxarifeGestorAdm(t *testing.T) {
	db := testDB(t)

	casos := []struct{ papel, email string }{
		{"almoxarife", "del-almox@empresa.com"},
		{"gestor", "del-gestor@empresa.com"},
		{"adm", "del-adm@empresa.com"},
	}
	for _, c := range casos {
		t.Run(c.papel, func(t *testing.T) {
			limparEstoquesHandler(t, db)
			criarContaComPapel(t, db, "Conta "+c.papel, c.email, "senha-123456", c.papel)
			token := tokenDeLogin(t, db, c.email, "senha-123456")

			e, err := services.CriarEstoque(db, "Canteiro "+c.papel)
			if err != nil {
				t.Fatalf("seed CriarEstoque: %v", err)
			}

			w := deleteEstoques(db, "Bearer "+token, e.ID)
			if w.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusNoContent, w.Body.String())
			}
			if w.Body.Len() != 0 {
				t.Errorf("corpo do 204 não vazio: %q", w.Body.String())
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM estoques WHERE id = $1`, e.ID).Scan(&n); err != nil {
				t.Fatalf("count: %v", err)
			}
			if n != 0 {
				t.Errorf("linhas com id = %d, want 0", n)
			}
		})
	}
}

// TestExcluirEstoqueHandler_404IdDesconhecidoOuMalformado prova a AC2 na
// fronteira: um id UUID sem linha e um id não-UUID caem no mesmo 404 NOT_FOUND
// com envelope de erro.
func TestExcluirEstoqueHandler_404IdDesconhecidoOuMalformado(t *testing.T) {
	db := testDB(t)
	limparEstoquesHandler(t, db)
	criarContaComPapel(t, db, "Almox", "del404-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "del404-almox@empresa.com", "senha-123456")

	casos := map[string]string{
		"uuid sem linha": "00000000-0000-4000-8000-000000000000",
		"id não-uuid":    "nao-e-uuid",
	}
	for nome, id := range casos {
		t.Run(nome, func(t *testing.T) {
			w := deleteEstoques(db, "Bearer "+token, id)
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusNotFound, w.Body.String())
			}
			env := decodeErro(t, w.Body.Bytes())
			if env.Error.Code != "NOT_FOUND" {
				t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
			}
		})
	}
}

// TestExcluirEstoqueHandler_403ParaUsuario prova a AC3: papel `usuario`
// chamando DELETE /api/estoques/{id} direto -> 403 FORBIDDEN, corpo do
// envelope, e o handler nunca executa (a linha continua lá).
func TestExcluirEstoqueHandler_403ParaUsuario(t *testing.T) {
	db := testDB(t)
	limparEstoquesHandler(t, db)
	criarContaComPapel(t, db, "Usuária", "del-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "del-usuario@empresa.com", "senha-123456")

	e, err := services.CriarEstoque(db, "Canteiro Protegido")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	w := deleteEstoques(db, "Bearer "+token, e.ID)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "FORBIDDEN" {
		t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM estoques`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("linhas = %d, want 1 (handler não deve ter executado)", n)
	}
}

// TestExcluirEstoqueHandler_401SemToken prova a linha "sem autenticação" da
// I/O Matrix: RequireAuth responde 401 antes de RequireRole rodar.
func TestExcluirEstoqueHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparEstoquesHandler(t, db)

	w := deleteEstoques(db, "", "00000000-0000-4000-8000-000000000000")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestExcluirEstoqueHandler_409ComResiduo prova a AC5 (Story 3.1, completando
// o guard pendente da Story 2.2) na fronteira HTTP: um Estoque com Produto em
// quantidade residual -> 409 CONFLICT, mensagem citando o nome do Produto; o
// Estoque continua existindo.
func TestExcluirEstoqueHandler_409ComResiduo(t *testing.T) {
	db := testDB(t)
	limparEstoquesHandler(t, db)
	criarContaComPapel(t, db, "Almox", "del-residuo-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "del-residuo-almox@empresa.com", "senha-123456")

	e, err := services.CriarEstoque(db, "Canteiro Com Resíduo Handler")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.005")
	produto, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome:              "Tubo PVC 100mm",
		CategoriaID:       categoriaID,
		EstoqueID:         e.ID,
		QuantidadeInicial: 5,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	w := deleteEstoques(db, "Bearer "+token, e.ID)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, produto.Nome) {
		t.Errorf("message = %q, want conter %q", env.Error.Message, produto.Nome)
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM estoques WHERE id = $1`, e.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("linhas em estoques com id = %d, want 1 (nada removido)", n)
	}
}
