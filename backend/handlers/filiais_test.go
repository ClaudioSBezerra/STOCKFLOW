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

// Testes HTTP das Filiais — Story 12.1 (spec-12-1), despachados pela MESMA
// composição de newMux: POST -> RequireAuth -> RequireRole(adm); GET ->
// RequireAuth apenas.

func postFiliais(db *sql.DB, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /e/{slug}/api/filiais",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(
				middleware.RequireRole(services.PapelAdm)(
					CriarFilialHandler(db)))))
	r := httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/filiais", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func getFiliais(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /e/{slug}/api/filiais",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(
				ListarFiliaisHandler(db))))
	r := httptest.NewRequest(http.MethodGet, prefixoEmpresaTeste+"/api/filiais", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func limparFiliaisDeTeste(t *testing.T, db *sql.DB, prefixo string) {
	t.Helper()
	limpar := func() {
		_, _ = db.Exec(`DELETE FROM estoques WHERE filial_id IN (SELECT id FROM filiais WHERE nome LIKE $1)`, prefixo+"%")
		_, _ = db.Exec(`DELETE FROM filiais WHERE nome LIKE $1`, prefixo+"%")
	}
	limpar()
	t.Cleanup(limpar)
}

func TestCriarFilialHandler_201ParaAdmEListagem(t *testing.T) {
	db := testDB(t)
	limparFiliaisDeTeste(t, db, "HF12 ")
	criarContaComPapel(t, db, "Adm", "filial-adm@empresa.com", "senha-123456", "adm")
	tokenAdm := tokenDeLogin(t, db, "filial-adm@empresa.com", "senha-123456")

	w := postFiliais(db, "Bearer "+tokenAdm, `{"nome":"  HF12 Recife "}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Filial map[string]any `json:"filial"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Filial) != 2 || resp.Filial["nome"] != "HF12 Recife" || resp.Filial["id"] == "" {
		t.Errorf("filial = %v, want {id, nome=\"HF12 Recife\"}", resp.Filial)
	}
	var empresaDaFilial string
	if err := db.QueryRow(`SELECT empresa_id FROM filiais WHERE id = $1`, resp.Filial["id"]).Scan(&empresaDaFilial); err != nil || empresaDaFilial != empresaTeste {
		t.Errorf("empresa_id da filial = %q (err=%v), want a Empresa do contexto", empresaDaFilial, err)
	}

	// Qualquer conta autenticada lista (almoxarife precisa escolher a Filial).
	criarContaComPapel(t, db, "Almox", "filial-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "filial-almox@empresa.com", "senha-123456")
	lw := getFiliais(db, "Bearer "+tokenAlmox)
	if lw.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body=%s)", lw.Code, lw.Body.String())
	}
	var lista struct {
		Filiais []map[string]any `json:"filiais"`
	}
	if err := json.Unmarshal(lw.Body.Bytes(), &lista); err != nil {
		t.Fatalf("decode lista: %v", err)
	}
	achou := false
	for _, f := range lista.Filiais {
		if f["nome"] == "HF12 Recife" {
			achou = true
		}
	}
	if !achou {
		t.Errorf("filial criada não aparece na listagem: %v", lista.Filiais)
	}
	if w := getFiliais(db, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("GET sem token: status = %d, want 401", w.Code)
	}
}

func TestCriarFilialHandler_403AbaixoDeAdm(t *testing.T) {
	db := testDB(t)
	limparFiliaisDeTeste(t, db, "HF12 ")
	for _, papel := range []string{"usuario", "almoxarife", "gestor"} {
		t.Run(papel, func(t *testing.T) {
			email := "filial-403-" + papel + "@empresa.com"
			criarContaComPapel(t, db, "Conta "+papel, email, "senha-123456", papel)
			token := tokenDeLogin(t, db, email, "senha-123456")
			w := postFiliais(db, "Bearer "+token, `{"nome":"HF12 Proibida"}`)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM filiais WHERE nome = 'HF12 Proibida'`).Scan(&n); err != nil || n != 0 {
				t.Errorf("filiais gravadas = %d (err=%v), want 0", n, err)
			}
		})
	}
}

func TestCriarFilialHandler_400E409(t *testing.T) {
	db := testDB(t)
	limparFiliaisDeTeste(t, db, "HF12 ")
	criarContaComPapel(t, db, "Adm", "filial-val-adm@empresa.com", "senha-123456", "adm")
	token := tokenDeLogin(t, db, "filial-val-adm@empresa.com", "senha-123456")

	casos := map[string]string{
		"vazio":         `{"nome":""}`,
		"em branco":     `{"nome":"   "}`,
		"ausente":       `{}`,
		"json inválido": `{"nome":`,
		"> 255 runes":   `{"nome":"` + strings.Repeat("é", 256) + `"}`,
	}
	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			w := postFiliais(db, "Bearer "+token, corpo)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
				t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
			}
		})
	}

	if w := postFiliais(db, "Bearer "+token, `{"nome":"HF12 Dup"}`); w.Code != http.StatusCreated {
		t.Fatalf("primeiro: status = %d, want 201", w.Code)
	}
	w := postFiliais(db, "Bearer "+token, `{"nome":"hf12   dup "}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicado: status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}
