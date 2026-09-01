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

// --- RegistrarTransferenciaHandler (Story 5.2, spec-5-2) ------------------
//
// POST /api/produtos/{id}/estoques/{estoqueId}/transferencia -> RequireAuth
// -> RequireRole(almoxarife) -> handler. Mesmo molde de postBaixa.

func postTransferencia(db *sql.DB, authHeader, produtoID, estoqueOrigemID, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/produtos/{id}/estoques/{estoqueId}/transferencia",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				RegistrarTransferenciaHandler(db, realtime.NewRegistry()))))
	caminho := "/api/produtos/" + produtoID + "/estoques/" + estoqueOrigemID + "/transferencia"
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

// TestRegistrarTransferenciaHandler_201 prova a linha "Transferência válida"
// da I/O Matrix: transferência válida por um `almoxarife` devolve 201 com a
// Movimentação tipo='transferencia', origem e destino preenchidos.
func TestRegistrarTransferenciaHandler_201(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Transf 201", "transf-201-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "transf-201-almox@empresa.com", "senha-123456")

	produtoID, estoqueOrigemID := seedProdutoComSaldoHandler(t, db, "Canteiro Transf 201 Origem", 10)
	estoqueDestino, err := services.CriarEstoque(db, "Canteiro Transf 201 Destino")
	if err != nil {
		t.Fatalf("seed CriarEstoque destino: %v", err)
	}

	corpo := `{"estoqueDestinoId":"` + estoqueDestino.ID + `","quantidade":4}`
	w := postTransferencia(db, "Bearer "+token, produtoID, estoqueOrigemID, corpo)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Movimentacao map[string]any `json:"movimentacao"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Movimentacao["tipo"] != "transferencia" {
		t.Errorf("tipo = %v, want transferencia", resp.Movimentacao["tipo"])
	}
	if resp.Movimentacao["estoqueOrigemId"] != estoqueOrigemID {
		t.Errorf("estoqueOrigemId = %v, want %q", resp.Movimentacao["estoqueOrigemId"], estoqueOrigemID)
	}
	if resp.Movimentacao["estoqueDestinoId"] != estoqueDestino.ID {
		t.Errorf("estoqueDestinoId = %v, want %q", resp.Movimentacao["estoqueDestinoId"], estoqueDestino.ID)
	}

	var saldoOrigem, saldoDestino float64
	if err := db.QueryRow(
		`SELECT quantidade FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2`,
		produtoID, estoqueOrigemID,
	).Scan(&saldoOrigem); err != nil {
		t.Fatalf("falha ao ler saldo origem: %v", err)
	}
	if err := db.QueryRow(
		`SELECT quantidade FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2`,
		produtoID, estoqueDestino.ID,
	).Scan(&saldoDestino); err != nil {
		t.Fatalf("falha ao ler saldo destino: %v", err)
	}
	if saldoOrigem != 6 {
		t.Errorf("saldo origem = %v, want 6", saldoOrigem)
	}
	if saldoDestino != 4 {
		t.Errorf("saldo destino = %v, want 4", saldoDestino)
	}
}

// TestRegistrarTransferenciaHandler_PublicaEventoNoSucesso prova a parte
// "evento publicado no canal `movimentacoes`" da linha "Transferência
// válida" da I/O Matrix (AD-3/epic-5-context.md). Molde exato de
// TestRegistrarBaixaHandler_PublicaEventoNoSucesso: mux próprio com um
// *realtime.Registry compartilhado, assinado ANTES do despacho.
func TestRegistrarTransferenciaHandler_PublicaEventoNoSucesso(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Transf Evento", "transf-evento-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "transf-evento-almox@empresa.com", "senha-123456")

	produtoID, estoqueOrigemID := seedProdutoComSaldoHandler(t, db, "Canteiro Transf Evento Origem", 10)
	estoqueDestino, err := services.CriarEstoque(db, "Canteiro Transf Evento Destino")
	if err != nil {
		t.Fatalf("seed CriarEstoque destino: %v", err)
	}

	registro := realtime.NewRegistry()
	eventos, cancelar := registro.Subscribe()
	defer cancelar()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/produtos/{id}/estoques/{estoqueId}/transferencia",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				RegistrarTransferenciaHandler(db, registro))))

	caminho := "/api/produtos/" + produtoID + "/estoques/" + estoqueOrigemID + "/transferencia"
	corpo := `{"estoqueDestinoId":"` + estoqueDestino.ID + `","quantidade":4}`
	r := httptest.NewRequest(http.MethodPost, caminho, strings.NewReader(corpo))
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
		t.Fatal("nenhum evento publicado em 1s após RegistrarTransferenciaHandler bem-sucedido")
	}
}

// TestRegistrarTransferenciaHandler_400QuantidadeInvalida prova a linha
// "Quantidade zero ou negativa" da I/O Matrix: -> 400 VALIDATION_ERROR.
func TestRegistrarTransferenciaHandler_400QuantidadeInvalida(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Transf 400q", "transf-400q-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "transf-400q-almox@empresa.com", "senha-123456")

	produtoID, estoqueOrigemID := seedProdutoComSaldoHandler(t, db, "Canteiro Transf 400q Origem", 10)
	estoqueDestino, err := services.CriarEstoque(db, "Canteiro Transf 400q Destino")
	if err != nil {
		t.Fatalf("seed CriarEstoque destino: %v", err)
	}

	for _, q := range []string{"0", "-3"} {
		corpo := `{"estoqueDestinoId":"` + estoqueDestino.ID + `","quantidade":` + q + `}`
		w := postTransferencia(db, "Bearer "+token, produtoID, estoqueOrigemID, corpo)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("quantidade=%s: status = %d, want %d (body=%s)", q, w.Code, http.StatusBadRequest, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "VALIDATION_ERROR" {
			t.Errorf("quantidade=%s: code = %q, want VALIDATION_ERROR", q, env.Error.Code)
		}
	}
}

// TestRegistrarTransferenciaHandler_400PayloadInvalido prova que um corpo
// não decodificável (JSON malformado) -> 400 VALIDATION_ERROR, mesmo molde
// de TestRegistrarBaixaHandler_400PayloadInvalido.
func TestRegistrarTransferenciaHandler_400PayloadInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Transf Payload", "transf-payload-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "transf-payload-almox@empresa.com", "senha-123456")

	produtoID, estoqueOrigemID := seedProdutoComSaldoHandler(t, db, "Canteiro Transf Payload Origem", 10)

	w := postTransferencia(db, "Bearer "+token, produtoID, estoqueOrigemID, `{"quantidade":`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// TestRegistrarTransferenciaHandler_400OrigemIgualDestino prova a linha
// "Origem igual ao destino" da I/O Matrix: -> 400 VALIDATION_ERROR.
func TestRegistrarTransferenciaHandler_400OrigemIgualDestino(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Transf 400od", "transf-400od-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "transf-400od-almox@empresa.com", "senha-123456")

	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Transf 400od", 10)

	corpo := `{"estoqueDestinoId":"` + estoqueID + `","quantidade":1}`
	w := postTransferencia(db, "Bearer "+token, produtoID, estoqueID, corpo)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// TestRegistrarTransferenciaHandler_409QuantidadeIndisponivel prova a linha
// "Quantidade maior que a disponível na origem" da I/O Matrix: -> 409
// CONFLICT citando o saldo real da origem.
func TestRegistrarTransferenciaHandler_409QuantidadeIndisponivel(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Transf 409", "transf-409-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "transf-409-almox@empresa.com", "senha-123456")

	produtoID, estoqueOrigemID := seedProdutoComSaldoHandler(t, db, "Canteiro Transf 409 Origem", 2.5)
	estoqueDestino, err := services.CriarEstoque(db, "Canteiro Transf 409 Destino")
	if err != nil {
		t.Fatalf("seed CriarEstoque destino: %v", err)
	}

	corpo := `{"estoqueDestinoId":"` + estoqueDestino.ID + `","quantidade":100}`
	w := postTransferencia(db, "Bearer "+token, produtoID, estoqueOrigemID, corpo)
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

// TestRegistrarTransferenciaHandler_409EstoqueDestinoMalformadoOuInexistente
// prova, na fronteira HTTP, a linha "Estoque destino malformado/inexistente"
// da I/O Matrix: mesmo colapso "malformado/inexistente -> 0 disponível" já
// coberto em services.TestRegistrarTransferencia_EstoqueDestinoMalformadoOuInexistente,
// mas verificando que RegistrarTransferenciaHandler de fato traduz o
// *services.ErroQuantidadeIndisponivel resultante em 409 CONFLICT no
// envelope HTTP (não só no valor de retorno do serviço).
func TestRegistrarTransferenciaHandler_409EstoqueDestinoMalformadoOuInexistente(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Transf 409Destino", "transf-409destino-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "transf-409destino-almox@empresa.com", "senha-123456")

	produtoID, estoqueOrigemID := seedProdutoComSaldoHandler(t, db, "Canteiro Transf 409Destino Origem", 10)

	casos := map[string]string{
		"malformado":  "nao-e-um-uuid",
		"inexistente": "00000000-0000-0000-0000-000000000000",
	}
	for nome, destino := range casos {
		t.Run(nome, func(t *testing.T) {
			corpo := `{"estoqueDestinoId":"` + destino + `","quantidade":1}`
			w := postTransferencia(db, "Bearer "+token, produtoID, estoqueOrigemID, corpo)
			if w.Code != http.StatusConflict {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusConflict, w.Body.String())
			}
			env := decodeErro(t, w.Body.Bytes())
			if env.Error.Code != "CONFLICT" {
				t.Errorf("code = %q, want CONFLICT", env.Error.Code)
			}
		})
	}

	var saldo float64
	if err := db.QueryRow(
		`SELECT quantidade FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2`,
		produtoID, estoqueOrigemID,
	).Scan(&saldo); err != nil {
		t.Fatalf("falha ao ler saldo: %v", err)
	}
	if saldo != 10 {
		t.Errorf("saldo origem não deveria ter mudado, got %v, want 10", saldo)
	}
}

// TestRegistrarTransferenciaHandler_403PapelUsuario prova a linha "Papel
// usuario" da I/O Matrix: -> 403 FORBIDDEN, decidido por RequireRole, o
// handler nunca executa.
func TestRegistrarTransferenciaHandler_403PapelUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Usuario Transf 403", "transf-403-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "transf-403-usuario@empresa.com", "senha-123456")

	produtoID, estoqueOrigemID := seedProdutoComSaldoHandler(t, db, "Canteiro Transf 403 Origem", 10)
	estoqueDestino, err := services.CriarEstoque(db, "Canteiro Transf 403 Destino")
	if err != nil {
		t.Fatalf("seed CriarEstoque destino: %v", err)
	}

	corpo := `{"estoqueDestinoId":"` + estoqueDestino.ID + `","quantidade":1}`
	w := postTransferencia(db, "Bearer "+token, produtoID, estoqueOrigemID, corpo)
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
		produtoID, estoqueOrigemID,
	).Scan(&saldo); err != nil {
		t.Fatalf("falha ao ler saldo: %v", err)
	}
	if saldo != 10 {
		t.Errorf("saldo não deveria ter mudado, got %v, want 10", saldo)
	}
}

// TestRegistrarTransferenciaHandler_401SemToken prova que uma requisição
// sem Authorization -> 401, produzido só por RequireAuth.
func TestRegistrarTransferenciaHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)

	produtoID, estoqueOrigemID := seedProdutoComSaldoHandler(t, db, "Canteiro Transf 401 Origem", 10)
	estoqueDestino, err := services.CriarEstoque(db, "Canteiro Transf 401 Destino")
	if err != nil {
		t.Fatalf("seed CriarEstoque destino: %v", err)
	}

	corpo := `{"estoqueDestinoId":"` + estoqueDestino.ID + `","quantidade":1}`
	w := postTransferencia(db, "", produtoID, estoqueOrigemID, corpo)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}
