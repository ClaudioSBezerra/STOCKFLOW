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

// Testes HTTP dos Centros de Custo — Story 12.3 (spec-12-3), despachados pela
// MESMA composição de newMux: POST -> RequireAuth -> RequireRole(adm); GET ->
// RequireAuth apenas.

func postCentrosCusto(db *sql.DB, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /e/{slug}/api/centros-custo",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(
				middleware.RequireRole(services.PapelAdm)(
					CriarCentroCustoHandler(db)))))
	r := httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/centros-custo", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func getCentrosCusto(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /e/{slug}/api/centros-custo",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(
				ListarCentrosCustoHandler(db))))
	r := httptest.NewRequest(http.MethodGet, prefixoEmpresaTeste+"/api/centros-custo", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// limparCentrosCustoHandler remove (antes e depois) os Centros de Custo de
// teste pelo prefixo do nome, com os Pedidos que os referenciam (FK).
func limparCentrosCustoHandler(t *testing.T, db *sql.DB, prefixo string) {
	t.Helper()
	limpar := func() {
		const sub = `(SELECT id FROM pedidos WHERE centro_custo_id IN (SELECT id FROM centros_custo WHERE nome LIKE $1))`
		_, _ = db.Exec(`DELETE FROM pedido_itens WHERE pedido_id IN `+sub, prefixo+"%")
		_, _ = db.Exec(`DELETE FROM reservas_pedido_item WHERE pedido_id IN `+sub, prefixo+"%")
		_, _ = db.Exec(`DELETE FROM pedidos WHERE centro_custo_id IN (SELECT id FROM centros_custo WHERE nome LIKE $1)`, prefixo+"%")
		_, _ = db.Exec(`DELETE FROM centros_custo WHERE nome LIKE $1`, prefixo+"%")
	}
	limpar()
	t.Cleanup(limpar)
}

func TestCriarCentroCustoHandler_201ParaAdmEListagem(t *testing.T) {
	db := testDB(t)
	limparCentrosCustoHandler(t, db, "HCC12 ")
	criarContaComPapel(t, db, "Adm", "cc-adm@empresa.com", "senha-123456", "adm")
	tokenAdm := tokenDeLogin(t, db, "cc-adm@empresa.com", "senha-123456")

	w := postCentrosCusto(db, "Bearer "+tokenAdm, `{"nome":"  HCC12 Estoque do Cabo "}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		CentroCusto map[string]any `json:"centroCusto"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.CentroCusto) != 2 || resp.CentroCusto["nome"] != "HCC12 Estoque do Cabo" || resp.CentroCusto["id"] == "" {
		t.Errorf("centroCusto = %v", resp.CentroCusto)
	}
	var empresa string
	if err := db.QueryRow(`SELECT empresa_id FROM centros_custo WHERE id = $1`, resp.CentroCusto["id"]).Scan(&empresa); err != nil || empresa != empresaTeste {
		t.Errorf("empresa_id = %q (err=%v), want a Empresa do contexto", empresa, err)
	}

	// Qualquer conta autenticada lista.
	criarContaComPapel(t, db, "Usuario", "cc-usuario@empresa.com", "senha-123456", "usuario")
	tokenUsuario := tokenDeLogin(t, db, "cc-usuario@empresa.com", "senha-123456")
	lw := getCentrosCusto(db, "Bearer "+tokenUsuario)
	if lw.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body=%s)", lw.Code, lw.Body.String())
	}
	var lista struct {
		CentrosCusto []map[string]any `json:"centrosCusto"`
	}
	if err := json.Unmarshal(lw.Body.Bytes(), &lista); err != nil {
		t.Fatalf("decode lista: %v", err)
	}
	achou := false
	for _, c := range lista.CentrosCusto {
		if c["nome"] == "HCC12 Estoque do Cabo" {
			achou = true
		}
	}
	if !achou {
		t.Errorf("centro criado não aparece na listagem: %v", lista.CentrosCusto)
	}
	if w := getCentrosCusto(db, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("GET sem token: status = %d, want 401", w.Code)
	}
}

func TestCriarCentroCustoHandler_403AbaixoDeAdm(t *testing.T) {
	db := testDB(t)
	limparCentrosCustoHandler(t, db, "HCC12 ")
	for _, papel := range []string{"usuario", "almoxarife", "gestor"} {
		t.Run(papel, func(t *testing.T) {
			email := "cc-403-" + papel + "@empresa.com"
			criarContaComPapel(t, db, "Conta "+papel, email, "senha-123456", papel)
			token := tokenDeLogin(t, db, email, "senha-123456")
			w := postCentrosCusto(db, "Bearer "+token, `{"nome":"HCC12 Proibido"}`)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM centros_custo WHERE nome = 'HCC12 Proibido'`).Scan(&n); err != nil || n != 0 {
				t.Errorf("centros gravados = %d (err=%v), want 0", n, err)
			}
		})
	}
}

func TestCriarCentroCustoHandler_400E409(t *testing.T) {
	db := testDB(t)
	limparCentrosCustoHandler(t, db, "HCC12 ")
	criarContaComPapel(t, db, "Adm", "cc-val-adm@empresa.com", "senha-123456", "adm")
	token := tokenDeLogin(t, db, "cc-val-adm@empresa.com", "senha-123456")

	casos := map[string]string{
		"vazio":         `{"nome":""}`,
		"em branco":     `{"nome":"   "}`,
		"ausente":       `{}`,
		"json inválido": `{"nome":`,
		"> 255 runes":   `{"nome":"` + strings.Repeat("é", 256) + `"}`,
	}
	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			w := postCentrosCusto(db, "Bearer "+token, corpo)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
				t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
			}
		})
	}

	if w := postCentrosCusto(db, "Bearer "+token, `{"nome":"HCC12 Dup"}`); w.Code != http.StatusCreated {
		t.Fatalf("primeiro: status = %d, want 201", w.Code)
	}
	w := postCentrosCusto(db, "Bearer "+token, `{"nome":"hcc12   dup "}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicado: status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

// TestSubmeterPedidoHandler_CentroCusto prova o `centro_custo_id` opcional em
// POST /api/pedidos: ausente/null/"  " -> 201 com `centro_custo_id` null;
// válido -> gravado e devolvido; de outra Empresa/inexistente/malformado ->
// 400, nada gravado, carrinho intacto; texto livre segue obrigatório.
func TestSubmeterPedidoHandler_CentroCusto(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	limparCentrosCustoHandler(t, db, "HCC12P ")
	_, token := seedContaComumECarrinho(t, db, "Usuario Pedido CC", "pedido-cc-usuario@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Pedido CC", 50)

	proprio, err := services.CriarCentroCusto(db, empresaTeste, "HCC12P Proprio")
	if err != nil {
		t.Fatalf("seed centro: %v", err)
	}

	// Centro de OUTRA Empresa.
	const slugOutra = "hcc12p-outra"
	removerEmpresaPlataformaHandlers(t, db, slugOutra)
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	outra, err := services.ProvisionarEmpresa(tx, services.DadosEmpresa{
		NomeFantasia: "HCC12P Outra", RazaoSocial: "HCC12P Outra LTDA", CNPJ: "11444777000161", Slug: slugOutra,
		Endereco: services.EnderecoEmpresa{Logradouro: "Rua", Numero: "1", Bairro: "Centro", Cidade: "Recife", CEP: "50000000", UF: "PE"},
	})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("provisionar outra empresa: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	t.Cleanup(func() {
		limparCentrosCustoHandler(t, db, "HCC12P ")
		removerEmpresaPlataformaHandlers(t, db, slugOutra)
	})
	alheio, err := services.CriarCentroCusto(db, outra.ID, "HCC12P Alheio")
	if err != nil {
		t.Fatalf("seed centro alheio: %v", err)
	}

	abastecer := func() {
		t.Helper()
		w := postItemCarrinho(db, "Bearer "+token, `{"produtoId":"`+produtoID+`","estoqueId":"`+estoqueID+`","quantidade":1}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("seed carrinho: status = %d (body=%s)", w.Code, w.Body.String())
		}
	}
	contarPedidosDB := func() int {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pedidos`).Scan(&n); err != nil {
			t.Fatalf("count pedidos: %v", err)
		}
		return n
	}

	// Inválidos: 400, nada gravado, carrinho intacto (1 item).
	abastecer()
	for nome, cid := range map[string]string{
		"outra empresa": `"` + alheio.ID + `"`,
		"inexistente":   `"00000000-0000-0000-0000-000000000000"`,
		"malformado":    `"abc"`,
	} {
		antes := contarPedidosDB()
		w := postPedido(db, "Bearer "+token, `{"solicitante":"Fulano","obraCentroCusto":"Obra X","centro_custo_id":`+cid+`}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400 (body=%s)", nome, w.Code, w.Body.String())
		}
		if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
			t.Errorf("%s: code = %q", nome, env.Error.Code)
		}
		if depois := contarPedidosDB(); depois != antes {
			t.Errorf("%s: pedidos = %d, want %d", nome, depois, antes)
		}
		if body := strings.TrimSpace(getCarrinho(db, "Bearer "+token).Body.String()); strings.Contains(body, `"itens":[]`) {
			t.Errorf("%s: carrinho deveria estar intacto, got %s", nome, body)
		}
	}

	// Texto livre segue obrigatório mesmo com Centro válido.
	w := postPedido(db, "Bearer "+token, `{"solicitante":"Fulano","obraCentroCusto":"  ","centro_custo_id":"`+proprio.ID+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("texto livre ausente: status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}

	// Válido: 201, gravado e devolvido.
	w = postPedido(db, "Bearer "+token, `{"solicitante":"Fulano","obraCentroCusto":"Obra Norte","centro_custo_id":"`+proprio.ID+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("válido: status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedido map[string]any `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Pedido["centro_custo_id"] != proprio.ID || resp.Pedido["obraCentroCusto"] != "Obra Norte" {
		t.Errorf("pedido = %v, want centro_custo_id=%s e texto livre preservado", resp.Pedido, proprio.ID)
	}
	var gravado sql.NullString
	if err := db.QueryRow(`SELECT centro_custo_id FROM pedidos WHERE id = $1`, resp.Pedido["id"]).Scan(&gravado); err != nil || gravado.String != proprio.ID {
		t.Errorf("centro_custo_id gravado = %v (err=%v)", gravado, err)
	}

	// Ausente / null / só espaços: 201 com `centro_custo_id` null.
	for nome, corpo := range map[string]string{
		"ausente":    `{"solicitante":"Fulano","obraCentroCusto":"Obra X"}`,
		"null":       `{"solicitante":"Fulano","obraCentroCusto":"Obra X","centro_custo_id":null}`,
		"só espaços": `{"solicitante":"Fulano","obraCentroCusto":"Obra X","centro_custo_id":"  "}`,
	} {
		abastecer()
		w := postPedido(db, "Bearer "+token, corpo)
		if w.Code != http.StatusCreated {
			t.Fatalf("%s: status = %d, want 201 (body=%s)", nome, w.Code, w.Body.String())
		}
		var r struct {
			Pedido map[string]any `json:"pedido"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
			t.Fatalf("%s: decode: %v", nome, err)
		}
		v, existe := r.Pedido["centro_custo_id"]
		if !existe || v != nil {
			t.Errorf("%s: centro_custo_id = %v (presente=%v), want null presente", nome, v, existe)
		}
	}
}
