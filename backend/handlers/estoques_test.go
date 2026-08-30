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

func limparEstoquesHandler(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`TRUNCATE TABLE estoques`); err != nil {
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
