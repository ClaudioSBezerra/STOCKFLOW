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

// Testes de handler do CRUD de Templates de Nomenclatura — Story 10.6
// (spec-10-6). Despacho pela MESMA composição de newMux: RequireAuth ->
// RequireRole(adm) -> handler. Os Templates criados usam subtipos "T10.6…" e
// são removidos ao fim.

const idAusenteTemplateHandler = "00000000-0000-4000-8000-000000000000"

func limparTemplatesHandler(t *testing.T, db *sql.DB) {
	t.Helper()
	del := func() {
		limparEstoquesHandler(t, db) // Produtos de teste referenciam Templates "T10.6…" (FK)
		if _, err := db.Exec(`DELETE FROM nomenclatura_templates WHERE subtipo LIKE 'T10.6%'`); err != nil {
			t.Fatalf("limpar templates de teste: %v", err)
		}
	}
	del()
	t.Cleanup(del)
}

func despacharTemplates(db *sql.DB, metodo, id, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	gate := func(h http.HandlerFunc) http.HandlerFunc {
		return comEmpresa(db, middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAdm)(h)))
	}
	mux.HandleFunc("POST /e/{slug}/api/nomenclatura-templates", gate(CriarTemplateNomenclaturaHandler(db)))
	mux.HandleFunc("PUT /e/{slug}/api/nomenclatura-templates/{id}", gate(AtualizarTemplateNomenclaturaHandler(db)))
	mux.HandleFunc("DELETE /e/{slug}/api/nomenclatura-templates/{id}", gate(ExcluirTemplateNomenclaturaHandler(db)))

	caminho := prefixoEmpresaTeste + "/api/nomenclatura-templates"
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

func tokenAdmTemplates(t *testing.T, db *sql.DB, email string) string {
	t.Helper()
	criarContaComPapel(t, db, "Adm", email, "senha-123456", "adm")
	return "Bearer " + tokenDeLogin(t, db, email, "senha-123456")
}

func criarTemplateViaHandler(t *testing.T, db *sql.DB, auth, subtipo, template string) string {
	t.Helper()
	corpo, _ := json.Marshal(map[string]string{"subtipo": subtipo, "template": template})
	w := despacharTemplates(db, http.MethodPost, "", auth, string(corpo))
	if w.Code != http.StatusCreated {
		t.Fatalf("seed POST: status = %d (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Template map[string]any `json:"template"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.Template["id"].(string)
}

func TestTemplatesNomenclaturaHandler_CriarEditarExcluir(t *testing.T) {
	db := testDB(t)
	limparTemplatesHandler(t, db)
	auth := tokenAdmTemplates(t, db, "tpl-adm@empresa.com")

	w := despacharTemplates(db, http.MethodPost, "", auth, `{"subtipo":"T10.6 Cabos — Especial","template":"CABO [TIPO] [BITOLA]"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status = %d (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Template map[string]any `json:"template"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Template) != 3 || resp.Template["subtipo"] != "T10.6 Cabos — Especial" || resp.Template["template"] != "CABO [TIPO] [BITOLA]" {
		t.Errorf("template = %v, want {id,subtipo,template}", resp.Template)
	}
	id := resp.Template["id"].(string)

	w = despacharTemplates(db, http.MethodPut, id, auth, `{"subtipo":"T10.6 Cabos 2","template":"CABO [TIPO]"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"T10.6 Cabos 2"`) {
		t.Fatalf("PUT status = %d (body=%s)", w.Code, w.Body.String())
	}

	w = despacharTemplates(db, http.MethodDelete, id, auth, "")
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("DELETE status = %d body=%q, want 204 vazio", w.Code, w.Body.String())
	}
	if w = despacharTemplates(db, http.MethodDelete, id, auth, ""); w.Code != http.StatusNotFound {
		t.Errorf("DELETE repetido status = %d, want 404", w.Code)
	}
}

func TestTemplatesNomenclaturaHandler_400_409_404(t *testing.T) {
	db := testDB(t)
	limparTemplatesHandler(t, db)
	auth := tokenAdmTemplates(t, db, "tpl-adm-erros@empresa.com")
	id := criarTemplateViaHandler(t, db, auth, "T10.6 Um", "UM [X]")

	corpoGrande := `{"subtipo":"T","template":"` + strings.Repeat("x", authRequestMaxBytes+1) + `"}`
	invalidos := map[string]string{
		"subtipo 256":    `{"subtipo":"` + strings.Repeat("x", 256) + `","template":"A [X]"}`,
		"sem token":      `{"subtipo":"T10.6 s","template":"SEM TOKEN"}`,
		"colchete":       `{"subtipo":"T10.6 s","template":"A [X"}`,
		"vazios":         `{"subtipo":"  ","template":"  "}`,
		"ausentes":       `{}`,
		"json inválido":  `{"subtipo":`,
		"corpo > limite": corpoGrande,
	}
	for nome, corpo := range invalidos {
		for _, req := range []struct{ metodo, id string }{{http.MethodPost, ""}, {http.MethodPut, id}} {
			t.Run(nome+" "+req.metodo, func(t *testing.T) {
				w := despacharTemplates(db, req.metodo, req.id, auth, corpo)
				if w.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
				}
				if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
					t.Errorf("code = %q", env.Error.Code)
				}
			})
		}
	}

	w := despacharTemplates(db, http.MethodPost, "", auth, `{"subtipo":"T10.6 Um","template":"OUTRO [X]"}`)
	if w.Code != http.StatusConflict || decodeErro(t, w.Body.Bytes()).Error.Code != "CONFLICT" {
		t.Errorf("duplicado: status=%d body=%s", w.Code, w.Body.String())
	}

	for _, alvo := range []string{idAusenteTemplateHandler, "nao-e-uuid"} {
		if w := despacharTemplates(db, http.MethodPut, alvo, auth, `{"subtipo":"T10.6 Nove","template":"N [X]"}`); w.Code != http.StatusNotFound {
			t.Errorf("PUT %s status = %d, want 404", alvo, w.Code)
		}
		if w := despacharTemplates(db, http.MethodDelete, alvo, auth, ""); w.Code != http.StatusNotFound {
			t.Errorf("DELETE %s status = %d, want 404", alvo, w.Code)
		}
	}
}

func TestTemplatesNomenclaturaHandler_ExcluirEmUso409(t *testing.T) {
	db := testDB(t)
	limparTemplatesHandler(t, db)
	auth := tokenAdmTemplates(t, db, "tpl-adm-uso@empresa.com")
	id := criarTemplateViaHandler(t, db, auth, "T10.6 Em Uso", "TUBO [TIPO]")

	e, err := services.CriarEstoque(db, empresaTeste, "Canteiro Template Uso")
	if err != nil {
		t.Fatalf("seed estoque: %v", err)
	}
	if _, err := criarProdutoComSaldo(db, empresaTeste, services.CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "TUBO PVC 50MM",
		CategoriaID:   categoriaIDPorCodigoHandler(t, db, "04.001"),
		TemplateID:    id,
	}, e.ID, 1); err != nil {
		t.Fatalf("seed produto: %v", err)
	}

	w := despacharTemplates(db, http.MethodDelete, id, auth, "")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "CONFLICT" || !strings.Contains(env.Error.Message, "1 produto") {
		t.Errorf("envelope = %+v", env)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM nomenclatura_templates WHERE id = $1`, id).Scan(&n)
	if n != 1 {
		t.Errorf("template removido apesar do 409")
	}

	// Editar em uso é permitido (200).
	if w := despacharTemplates(db, http.MethodPut, id, auth, `{"subtipo":"T10.6 Em Uso","template":"FITA [COR]"}`); w.Code != http.StatusOK {
		t.Errorf("PUT em uso status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
}

// TestTemplatesNomenclaturaHandler_FallbackUnico409 prova o 409 do único
// [NOME LIVRE] da Empresa padrão (Genérico): excluir e trocar o texto.
func TestTemplatesNomenclaturaHandler_FallbackUnico409(t *testing.T) {
	db := testDB(t)
	limparTemplatesHandler(t, db)
	auth := tokenAdmTemplates(t, db, "tpl-adm-fallback@empresa.com")
	id := templateIDPorSubtipoHandler(t, db, "Genérico")

	if w := despacharTemplates(db, http.MethodDelete, id, auth, ""); w.Code != http.StatusConflict {
		t.Errorf("DELETE fallback status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if w := despacharTemplates(db, http.MethodPut, id, auth, `{"subtipo":"Genérico","template":"X [Y]"}`); w.Code != http.StatusConflict {
		t.Errorf("PUT fallback status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	var texto string
	db.QueryRow(`SELECT template FROM nomenclatura_templates WHERE id = $1`, id).Scan(&texto)
	if texto != services.TemplateGenericoMarcador {
		t.Errorf("fallback alterado: %q", texto)
	}
}

func TestTemplatesNomenclaturaHandler_403AbaixoDeAdm(t *testing.T) {
	db := testDB(t)
	limparTemplatesHandler(t, db)
	authAdm := tokenAdmTemplates(t, db, "tpl-adm-403@empresa.com")
	id := criarTemplateViaHandler(t, db, authAdm, "T10.6 Quatro", "Q [X]")

	for _, papel := range []string{"usuario", "almoxarife", "gestor"} {
		t.Run(papel, func(t *testing.T) {
			email := "tpl-403-" + papel + "@empresa.com"
			criarContaComPapel(t, db, papel, email, "senha-123456", papel)
			auth := "Bearer " + tokenDeLogin(t, db, email, "senha-123456")

			for _, req := range []struct{ metodo, id, corpo string }{
				{http.MethodPost, "", `{"subtipo":"T10.6 Cinco","template":"C [X]"}`},
				{http.MethodPut, id, `{"subtipo":"T10.6 Quatro","template":"Mudou [X]"}`},
				{http.MethodDelete, id, ""},
			} {
				w := despacharTemplates(db, req.metodo, req.id, auth, req.corpo)
				if w.Code != http.StatusForbidden {
					t.Errorf("%s status = %d, want 403 (body=%s)", req.metodo, w.Code, w.Body.String())
				}
			}
		})
	}
	var texto string
	db.QueryRow(`SELECT template FROM nomenclatura_templates WHERE id = $1`, id).Scan(&texto)
	if texto != "Q [X]" {
		t.Errorf("template alterado/removido por papel sem permissão: %q", texto)
	}
}

func TestTemplatesNomenclaturaHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	for _, req := range []struct{ metodo, id, corpo string }{
		{http.MethodPost, "", `{"subtipo":"T10.6 x","template":"C [X]"}`},
		{http.MethodPut, idAusenteTemplateHandler, `{"subtipo":"T10.6 x","template":"C [X]"}`},
		{http.MethodDelete, idAusenteTemplateHandler, ""},
	} {
		if w := despacharTemplates(db, req.metodo, req.id, "", req.corpo); w.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401", req.metodo, w.Code)
		}
	}
}
