package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"stockflow/backend/middleware"
	"stockflow/backend/realtime"
	"stockflow/backend/services"
)

// --- despacho pela MESMA composição de newMux (main.go) --------------------
//
// POST /api/produtos/{id}/estoques/{estoqueId}/baixa -> RequireAuth ->
// RequireRole(almoxarife) -> handler.

func postBaixa(db *sql.DB, authHeader, produtoID, estoqueID, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/produtos/{id}/estoques/{estoqueId}/baixa",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				RegistrarBaixaHandler(db, realtime.NewRegistry()))))
	caminho := "/api/produtos/" + produtoID + "/estoques/" + estoqueID + "/baixa"
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, caminho, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, caminho, nil)
	}
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// seedProdutoComSaldoHandler cadastra um Produto com `quantidadeInicial` num
// Estoque novo — devolve (produtoID, estoqueID).
func seedProdutoComSaldoHandler(t *testing.T, db *sql.DB, nomeEstoque string, quantidadeInicial float64) (produtoID, estoqueID string) {
	t.Helper()
	estoque, err := services.CriarEstoque(db, nomeEstoque)
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigoHandler(t, db, "04.001")
	produto, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome:              "Produto " + nomeEstoque,
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: quantidadeInicial,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}
	return produto.ID, estoque.ID
}

// TestRegistrarBaixaHandler_201 prova a AC1: baixa válida por um `almoxarife`
// devolve 201 com a Movimentação tipo='baixa'.
func TestRegistrarBaixaHandler_201(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Baixa 201", "baixa-201-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "baixa-201-almox@empresa.com", "senha-123456")

	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa 201", 10)

	w := postBaixa(db, "Bearer "+token, produtoID, estoqueID, `{"quantidade": 4}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Movimentacao map[string]any `json:"movimentacao"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Movimentacao["tipo"] != "baixa" {
		t.Errorf("tipo = %v, want baixa", resp.Movimentacao["tipo"])
	}
	if resp.Movimentacao["quantidade"] != float64(4) {
		t.Errorf("quantidade = %v, want 4", resp.Movimentacao["quantidade"])
	}
	if id, ok := resp.Movimentacao["id"].(string); !ok || id == "" {
		t.Errorf("movimentacao.id ausente/vazio: %v", resp.Movimentacao["id"])
	}

	var saldo float64
	if err := db.QueryRow(
		`SELECT quantidade FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2`,
		produtoID, estoqueID,
	).Scan(&saldo); err != nil {
		t.Fatalf("falha ao ler saldo: %v", err)
	}
	if saldo != 6 {
		t.Errorf("saldo pós-baixa = %v, want 6", saldo)
	}
}

// TestRegistrarBaixaHandler_PublicaEventoNoSucesso prova a linha "Baixa
// válida" da matriz de spec-5-1 (AD-3/epic-5-context.md): uma Baixa
// bem-sucedida publica `{"resource":"movimentacoes","id":<movimentação
// criada>,"change":"created"}` no canal `movimentacoes`. Molde exato de
// TestCriarProdutoHandler_PublicaEventoNoSucesso (produtos_test.go): mux
// próprio com um *realtime.Registry compartilhado e assinado ANTES do
// despacho — postBaixa não serve aqui porque constrói seu próprio Registry
// interno, nunca observável pelo teste.
func TestRegistrarBaixaHandler_PublicaEventoNoSucesso(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Baixa Evento", "baixa-evento-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "baixa-evento-almox@empresa.com", "senha-123456")

	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa Evento", 10)

	registro := realtime.NewRegistry()
	eventos, cancelar := registro.Subscribe()
	defer cancelar()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/produtos/{id}/estoques/{estoqueId}/baixa",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				RegistrarBaixaHandler(db, registro))))

	caminho := "/api/produtos/" + produtoID + "/estoques/" + estoqueID + "/baixa"
	r := httptest.NewRequest(http.MethodPost, caminho, strings.NewReader(`{"quantidade": 4}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Movimentacao struct {
			ID string `json:"id"`
		} `json:"movimentacao"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}

	select {
	case ev := <-eventos:
		if ev.Resource != "movimentacoes" || ev.ID != resp.Movimentacao.ID || ev.Change != "created" {
			t.Fatalf("evento = %+v, want {movimentacoes %s created}", ev, resp.Movimentacao.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("nenhum evento publicado em 1s após RegistrarBaixaHandler bem-sucedido")
	}
}

// TestRegistrarBaixaHandler_400QuantidadeInvalida prova a AC2: quantidade
// zero/negativa -> 400 VALIDATION_ERROR.
func TestRegistrarBaixaHandler_400QuantidadeInvalida(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Baixa 400", "baixa-400-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "baixa-400-almox@empresa.com", "senha-123456")

	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa 400", 10)

	for _, corpo := range []string{`{"quantidade": 0}`, `{"quantidade": -3}`} {
		w := postBaixa(db, "Bearer "+token, produtoID, estoqueID, corpo)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("corpo=%s: status = %d, want %d (body=%s)", corpo, w.Code, http.StatusBadRequest, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "VALIDATION_ERROR" {
			t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
		}
	}
}

// TestRegistrarBaixaHandler_400PayloadInvalido prova que um corpo não
// decodificável (JSON malformado) -> 400 VALIDATION_ERROR.
func TestRegistrarBaixaHandler_400PayloadInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Baixa Payload", "baixa-payload-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "baixa-payload-almox@empresa.com", "senha-123456")

	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa Payload", 10)

	w := postBaixa(db, "Bearer "+token, produtoID, estoqueID, `{"quantidade":`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// TestRegistrarBaixaHandler_409QuantidadeIndisponivel prova a AC3: quantidade
// acima da disponível -> 409 CONFLICT citando o valor real disponível.
func TestRegistrarBaixaHandler_409QuantidadeIndisponivel(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Baixa 409", "baixa-409-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "baixa-409-almox@empresa.com", "senha-123456")

	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa 409", 2.5)

	w := postBaixa(db, "Bearer "+token, produtoID, estoqueID, `{"quantidade": 100}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
	mensagemEsperada := "quantidade indisponível: apenas " + strconv.FormatFloat(2.5, 'f', -1, 64) + " unidade(s) disponível(is)"
	if env.Error.Message != mensagemEsperada {
		t.Errorf("message = %q, want %q", env.Error.Message, mensagemEsperada)
	}
}

// TestRegistrarBaixaHandler_403PapelUsuario prova que uma sessão `usuario`
// recebe 403 FORBIDDEN — decidido por RequireRole, o handler nunca executa
// (nenhuma escrita).
func TestRegistrarBaixaHandler_403PapelUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Usuario Baixa 403", "baixa-403-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "baixa-403-usuario@empresa.com", "senha-123456")

	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa 403", 10)

	w := postBaixa(db, "Bearer "+token, produtoID, estoqueID, `{"quantidade": 1}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "FORBIDDEN" {
		t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
	}

	var saldo float64
	if err := db.QueryRow(
		`SELECT quantidade FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2`,
		produtoID, estoqueID,
	).Scan(&saldo); err != nil {
		t.Fatalf("falha ao ler saldo: %v", err)
	}
	if saldo != 10 {
		t.Errorf("saldo não deveria ter mudado, got %v, want 10", saldo)
	}
}

// TestRegistrarBaixaHandler_401SemToken prova que uma requisição sem
// Authorization -> 401, produzido só por RequireAuth.
func TestRegistrarBaixaHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)

	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa 401", 10)

	w := postBaixa(db, "", produtoID, estoqueID, `{"quantidade": 1}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}
