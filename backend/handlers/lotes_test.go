package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stockflow/backend/middleware"
	"stockflow/backend/realtime"
	"stockflow/backend/services"
)

// Story 11.1 — POST /api/lotes despachado pela mesma composição de newMux
// (RequireAuth -> RequireRole(almoxarife) -> handler).

func postLote(db *sql.DB, registro *realtime.Registry, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /e/{slug}/api/lotes",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(
				middleware.RequireRole(services.PapelAlmoxarife)(
					LancarSaldoHandler(db, registro)))))
	r := httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/lotes", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func contarLotesHandler(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM lotes`).Scan(&n); err != nil {
		t.Fatalf("count lotes: %v", err)
	}
	return n
}

func TestLancarSaldoHandler_201(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Lote 201", "lote-201-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "lote-201-almox@empresa.com", "senha-123456")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Lote 201", 0)

	corpos := []struct {
		corpo string
		valid any
	}{
		{`{"produtoId":"` + produtoID + `","estoqueId":"` + estoqueID + `","quantidade":10,"dataValidade":"2999-03-01"}`, "2999-03-01"},
		{`{"produtoId":"` + produtoID + `","estoqueId":"` + estoqueID + `","quantidade":2}`, nil},
		{`{"produtoId":"` + produtoID + `","estoqueId":"` + estoqueID + `","quantidade":2,"dataValidade":null}`, nil},
		{`{"produtoId":"` + produtoID + `","estoqueId":"` + estoqueID + `","quantidade":2,"dataValidade":""}`, nil},
	}
	for _, c := range corpos {
		w := postLote(db, realtime.NewRegistry(), "Bearer "+token, c.corpo)
		if w.Code != http.StatusCreated {
			t.Fatalf("corpo=%s: status = %d (body=%s)", c.corpo, w.Code, w.Body.String())
		}
		var resp struct {
			Lote map[string]any `json:"lote"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Lote["dataValidade"] != c.valid || resp.Lote["vencido"] != false {
			t.Errorf("lote = %v, want dataValidade=%v vencido=false", resp.Lote, c.valid)
		}
		for _, k := range []string{"id", "produtoId", "estoqueId", "quantidade"} {
			if resp.Lote[k] == nil {
				t.Errorf("lote sem %s: %v", k, resp.Lote)
			}
		}
	}
	if n := contarLotesHandler(t, db); n != 4 {
		t.Errorf("lotes = %d, want 4", n)
	}

	// Passado é aceito e sinalizado.
	w := postLote(db, realtime.NewRegistry(), "Bearer "+token,
		`{"produtoId":"`+produtoID+`","estoqueId":"`+estoqueID+`","quantidade":1,"dataValidade":"2020-01-01"}`)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"vencido":true`) {
		t.Errorf("validade passada: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestLancarSaldoHandler_PublicaEventos(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Lote Evento", "lote-evento-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "lote-evento-almox@empresa.com", "senha-123456")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Lote Evento", 0)

	registro := realtime.NewRegistry()
	eventos, cancelar := registro.Subscribe(empresaTeste)
	defer cancelar()

	w := postLote(db, registro, "Bearer "+token,
		`{"produtoId":"`+produtoID+`","estoqueId":"`+estoqueID+`","quantidade":3}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d (body=%s)", w.Code, w.Body.String())
	}
	var movID string
	if err := db.QueryRow(`SELECT id FROM movimentacoes WHERE tipo = 'entrada'`).Scan(&movID); err != nil {
		t.Fatal(err)
	}

	recebidos := map[string]realtime.Evento{}
	for len(recebidos) < 2 {
		select {
		case ev := <-eventos:
			recebidos[ev.Resource] = ev
		case <-time.After(time.Second):
			t.Fatalf("eventos recebidos = %+v, want movimentacoes e produtos", recebidos)
		}
	}
	if ev := recebidos["movimentacoes"]; ev.ID != movID || ev.Change != "created" {
		t.Errorf("evento movimentacoes = %+v", ev)
	}
	if ev := recebidos["produtos"]; ev.ID != produtoID || ev.Change != "updated" {
		t.Errorf("evento produtos = %+v", ev)
	}
}

func TestLancarSaldoHandler_400(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Lote 400", "lote-400-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "lote-400-almox@empresa.com", "senha-123456")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Lote 400", 0)
	ids := `"produtoId":"` + produtoID + `","estoqueId":"` + estoqueID + `"`

	for _, corpo := range []string{
		`{` + ids + `,"quantidade":0}`,
		`{` + ids + `,"quantidade":-2}`,
		`{` + ids + `}`,
		`{` + ids + `,"quantidade":99999999}`,
		`{` + ids + `,"quantidade":1,"dataValidade":"01/03/2027"}`,
		`{` + ids + `,"quantidade":1,"dataValidade":"2027-13-40"}`,
		`{` + ids + `,"quantidade":`,
	} {
		w := postLote(db, realtime.NewRegistry(), "Bearer "+token, corpo)
		if w.Code != http.StatusBadRequest {
			t.Errorf("corpo=%s: status = %d, want 400 (body=%s)", corpo, w.Code, w.Body.String())
			continue
		}
		if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
			t.Errorf("code = %q", env.Error.Code)
		}
	}
	if n := contarLotesHandler(t, db); n != 0 {
		t.Errorf("lotes = %d, want 0", n)
	}
}

func TestLancarSaldoHandler_404(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Lote 404", "lote-404-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "lote-404-almox@empresa.com", "senha-123456")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Lote 404", 0)
	const ausente = "00000000-0000-4000-8000-000000000000"

	for _, corpo := range []string{
		`{"produtoId":"` + ausente + `","estoqueId":"` + estoqueID + `","quantidade":1}`,
		`{"produtoId":"` + produtoID + `","estoqueId":"` + ausente + `","quantidade":1}`,
		`{"produtoId":"x","estoqueId":"` + estoqueID + `","quantidade":1}`,
		`{"produtoId":"` + produtoID + `","estoqueId":"y","quantidade":1}`,
	} {
		w := postLote(db, realtime.NewRegistry(), "Bearer "+token, corpo)
		if w.Code != http.StatusNotFound {
			t.Errorf("corpo=%s: status = %d, want 404 (body=%s)", corpo, w.Code, w.Body.String())
		}
	}
}

func TestLancarSaldoHandler_403Usuario401SemToken(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Usuario Lote 403", "lote-403-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "lote-403-usuario@empresa.com", "senha-123456")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Lote 403", 0)
	corpo := `{"produtoId":"` + produtoID + `","estoqueId":"` + estoqueID + `","quantidade":1}`

	if w := postLote(db, realtime.NewRegistry(), "Bearer "+token, corpo); w.Code != http.StatusForbidden {
		t.Errorf("usuario: status = %d, want 403", w.Code)
	}
	if w := postLote(db, realtime.NewRegistry(), "", corpo); w.Code != http.StatusUnauthorized {
		t.Errorf("sem token: status = %d, want 401", w.Code)
	}
	if n := contarLotesHandler(t, db); n != 0 {
		t.Errorf("lotes = %d, want 0", n)
	}
}
