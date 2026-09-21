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

// Testes de handler do CRUD de Categorias — Story 10.5 (spec-10-5). Despacho
// pela MESMA composição de newMux: RequireAuth -> RequireRole(adm) -> handler.
// As Categorias criadas usam códigos "T…" e são removidas ao fim.

func limparCategoriasHandler(t *testing.T, db *sql.DB) {
	t.Helper()
	del := func() {
		limparEstoquesHandler(t, db) // Produtos de teste referenciam Categorias "T…" (FK)
		if _, err := db.Exec(`DELETE FROM categorias WHERE codigo LIKE 'T%'`); err != nil {
			t.Fatalf("limpar categorias de teste: %v", err)
		}
	}
	del()
	t.Cleanup(del)
}

func despacharCategorias(db *sql.DB, metodo, id, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	gate := func(h http.HandlerFunc) http.HandlerFunc {
		return comEmpresa(db, middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAdm)(h)))
	}
	mux.HandleFunc("POST /e/{slug}/api/categorias", gate(CriarCategoriaHandler(db)))
	mux.HandleFunc("PUT /e/{slug}/api/categorias/{id}", gate(AtualizarCategoriaHandler(db)))
	mux.HandleFunc("DELETE /e/{slug}/api/categorias/{id}", gate(ExcluirCategoriaHandler(db)))

	caminho := prefixoEmpresaTeste + "/api/categorias"
	if id != "" {
		caminho += "/" + id
	}
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(metodo, caminho, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(metodo, caminho, nil)
	}
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func tokenAdmCategorias(t *testing.T, db *sql.DB, email string) string {
	t.Helper()
	criarContaComPapel(t, db, "Adm", email, "senha-123456", "adm")
	return "Bearer " + tokenDeLogin(t, db, email, "senha-123456")
}

func criarCategoriaViaHandler(t *testing.T, db *sql.DB, auth, codigo, nome string) string {
	t.Helper()
	w := despacharCategorias(db, http.MethodPost, "", auth, `{"codigo":"`+codigo+`","nome":"`+nome+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed POST: status = %d (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Categoria map[string]any `json:"categoria"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.Categoria["id"].(string)
}

func TestCategoriasHandler_CriarEditarExcluir(t *testing.T) {
	db := testDB(t)
	limparCategoriasHandler(t, db)
	auth := tokenAdmCategorias(t, db, "cat-adm@empresa.com")

	w := despacharCategorias(db, http.MethodPost, "", auth, `{"codigo":"T14.001","nome":"Brindes"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status = %d (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Categoria map[string]any `json:"categoria"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Categoria) != 3 || resp.Categoria["codigo"] != "T14.001" || resp.Categoria["nome"] != "Brindes" {
		t.Errorf("categoria = %v, want {id,codigo,nome}", resp.Categoria)
	}
	id := resp.Categoria["id"].(string)

	w = despacharCategorias(db, http.MethodPut, id, auth, `{"codigo":"T14.002","nome":"Brindes 2"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"T14.002"`) {
		t.Errorf("PUT body = %s", w.Body.String())
	}

	w = despacharCategorias(db, http.MethodDelete, id, auth, "")
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("DELETE status = %d body=%q, want 204 vazio", w.Code, w.Body.String())
	}
	w = despacharCategorias(db, http.MethodDelete, id, auth, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("DELETE repetido status = %d, want 404", w.Code)
	}
}

func TestCategoriasHandler_400_409_404(t *testing.T) {
	db := testDB(t)
	limparCategoriasHandler(t, db)
	auth := tokenAdmCategorias(t, db, "cat-adm-erros@empresa.com")
	id := criarCategoriaViaHandler(t, db, auth, "T1", "Um")

	corpoGrande := `{"codigo":"T","nome":"` + strings.Repeat("x", authRequestMaxBytes+1) + `"}`
	invalidos := map[string]string{
		"codigo 9 runes": `{"codigo":"T12345678","nome":"Nome"}`,
		"nome 51 runes":  `{"codigo":"T2","nome":"` + strings.Repeat("x", 51) + `"}`,
		"vazios":         `{"codigo":"  ","nome":"  "}`,
		"ausentes":       `{}`,
		"json inválido":  `{"codigo":`,
		"corpo > limite": corpoGrande,
	}
	for nome, corpo := range invalidos {
		for _, req := range []struct{ metodo, id string }{{http.MethodPost, ""}, {http.MethodPut, id}} {
			t.Run(nome+" "+req.metodo, func(t *testing.T) {
				w := despacharCategorias(db, req.metodo, req.id, auth, corpo)
				if w.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
				}
				if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
					t.Errorf("code = %q", env.Error.Code)
				}
			})
		}
	}

	w := despacharCategorias(db, http.MethodPost, "", auth, `{"codigo":"T1","nome":"Outro"}`)
	if w.Code != http.StatusConflict || !strings.Contains(decodeErro(t, w.Body.Bytes()).Error.Message, "código") {
		t.Errorf("duplicado código: status=%d body=%s", w.Code, w.Body.String())
	}
	w = despacharCategorias(db, http.MethodPost, "", auth, `{"codigo":"T2","nome":"Um"}`)
	if w.Code != http.StatusConflict || !strings.Contains(decodeErro(t, w.Body.Bytes()).Error.Message, "nome") {
		t.Errorf("duplicado nome: status=%d body=%s", w.Code, w.Body.String())
	}

	for _, alvo := range []string{"00000000-0000-4000-8000-000000000000", "nao-e-uuid"} {
		if w := despacharCategorias(db, http.MethodPut, alvo, auth, `{"codigo":"T9","nome":"Nove"}`); w.Code != http.StatusNotFound {
			t.Errorf("PUT %s status = %d, want 404", alvo, w.Code)
		}
		if w := despacharCategorias(db, http.MethodDelete, alvo, auth, ""); w.Code != http.StatusNotFound {
			t.Errorf("DELETE %s status = %d, want 404", alvo, w.Code)
		}
	}
}

func TestCategoriasHandler_ExcluirEmUso409(t *testing.T) {
	db := testDB(t)
	limparCategoriasHandler(t, db)
	auth := tokenAdmCategorias(t, db, "cat-adm-uso@empresa.com")
	id := criarCategoriaViaHandler(t, db, auth, "T3", "Em Uso")

	e, err := services.CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Canteiro Categoria Uso")
	if err != nil {
		t.Fatalf("seed estoque: %v", err)
	}
	if _, err := criarProdutoComSaldo(db, empresaTeste, services.CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Produto Categoria Em Uso",
		CategoriaID:   id,
		TemplateID:    templateIDPorSubtipoHandler(t, db, "Genérico"),
	}, e.ID, 1); err != nil {
		t.Fatalf("seed produto: %v", err)
	}

	w := despacharCategorias(db, http.MethodDelete, id, auth, "")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "CONFLICT" || !strings.Contains(env.Error.Message, "1 produto") {
		t.Errorf("envelope = %+v", env)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM categorias WHERE id = $1`, id).Scan(&n)
	if n != 1 {
		t.Errorf("categoria removida apesar do 409")
	}
}

func TestCategoriasHandler_403AbaixoDeAdm(t *testing.T) {
	db := testDB(t)
	limparCategoriasHandler(t, db)
	authAdm := tokenAdmCategorias(t, db, "cat-adm-403@empresa.com")
	id := criarCategoriaViaHandler(t, db, authAdm, "T4", "Quatro")

	for _, papel := range []string{"usuario", "almoxarife", "gestor"} {
		t.Run(papel, func(t *testing.T) {
			email := "cat-403-" + papel + "@empresa.com"
			criarContaComPapel(t, db, papel, email, "senha-123456", papel)
			auth := "Bearer " + tokenDeLogin(t, db, email, "senha-123456")

			for _, req := range []struct{ metodo, id, corpo string }{
				{http.MethodPost, "", `{"codigo":"T5","nome":"Cinco"}`},
				{http.MethodPut, id, `{"codigo":"T4","nome":"Mudou"}`},
				{http.MethodDelete, id, ""},
			} {
				w := despacharCategorias(db, req.metodo, req.id, auth, req.corpo)
				if w.Code != http.StatusForbidden {
					t.Errorf("%s status = %d, want 403 (body=%s)", req.metodo, w.Code, w.Body.String())
				}
			}
		})
	}
	var codigo, nome string
	db.QueryRow(`SELECT codigo, nome FROM categorias WHERE id = $1`, id).Scan(&codigo, &nome)
	if codigo != "T4" || nome != "Quatro" {
		t.Errorf("categoria alterada/removida por papel sem permissão: %q %q", codigo, nome)
	}
}

func TestCategoriasHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	for _, req := range []struct{ metodo, id, corpo string }{
		{http.MethodPost, "", `{"codigo":"T5","nome":"Cinco"}`},
		{http.MethodPut, "00000000-0000-4000-8000-000000000000", `{"codigo":"T5","nome":"Cinco"}`},
		{http.MethodDelete, "00000000-0000-4000-8000-000000000000", ""},
	} {
		if w := despacharCategorias(db, req.metodo, req.id, "", req.corpo); w.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401", req.metodo, w.Code)
		}
	}
}
