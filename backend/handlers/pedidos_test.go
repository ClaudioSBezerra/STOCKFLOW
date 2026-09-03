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

// --- despacho pela MESMA composição de newMux (main.go) --------------------
//
// POST /api/pedidos -> RequireAuth -> handler, SEM RequireRole (Story 7.2,
// spec-7-2: mesmo mínimo de papel do Carrinho — qualquer conta autenticada
// envia seu próprio Pedido).

func postPedido(db *sql.DB, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/pedidos",
		middleware.RequireAuth(db, testJWTSecret)(SubmeterPedidoHandler(db, realtime.NewRegistry())))
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, "/api/pedidos", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, "/api/pedidos", nil)
	}
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// --- SubmeterPedidoHandler ---------------------------------------------------

// TestSubmeterPedidoHandler_201 prova a AC1 na fronteira HTTP: carrinho com
// item disponível + solicitante/obra preenchidos -> 201 com o Pedido
// `pendente`, carrinho esvaziado.
func TestSubmeterPedidoHandler_201(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Pedido 201", "pedido-201-usuario@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Pedido 201", 10)

	wCarrinho := postItemCarrinho(db, "Bearer "+token, `{"produtoId":"`+produtoID+`","estoqueId":"`+estoqueID+`","quantidade":3}`)
	if wCarrinho.Code != http.StatusCreated {
		t.Fatalf("seed carrinho: status = %d (body=%s)", wCarrinho.Code, wCarrinho.Body.String())
	}

	corpo := `{"solicitante":"Fulano de Tal","obraCentroCusto":"Obra Norte","observacao":"urgente"}`
	w := postPedido(db, "Bearer "+token, corpo)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Pedido services.Pedido `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Pedido.ID == "" {
		t.Error("Pedido.ID vazio")
	}
	if resp.Pedido.Status != "pendente" {
		t.Errorf("Status = %q, want pendente", resp.Pedido.Status)
	}
	if resp.Pedido.Solicitante != "Fulano de Tal" {
		t.Errorf("Solicitante = %q", resp.Pedido.Solicitante)
	}

	wCarrinhoDepois := getCarrinho(db, "Bearer "+token)
	if body := strings.TrimSpace(wCarrinhoDepois.Body.String()); body != `{"itens":[],"removidos":[]}` {
		t.Errorf("carrinho deveria estar vazio após o envio, got %s", body)
	}

	var nItens int
	if err := db.QueryRow(`SELECT count(*) FROM pedido_itens WHERE pedido_id = $1`, resp.Pedido.ID).Scan(&nItens); err != nil {
		t.Fatalf("count pedido_itens: %v", err)
	}
	if nItens != 1 {
		t.Errorf("pedido_itens = %d linhas, want 1", nItens)
	}
}

// TestSubmeterPedidoHandler_PublicaEventoNoSucesso prova que um envio
// bem-sucedido publica `{"resource":"pedidos","id":<novo>,"change":"created"}`
// no canal `pedidos` (AD-3, Never de spec-7-2: payload mínimo, sem itens).
// Molde de TestRegistrarBaixaHandler_PublicaEventoNoSucesso.
func TestSubmeterPedidoHandler_PublicaEventoNoSucesso(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Pedido Evento", "pedido-evento-usuario@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Pedido Evento", 10)

	wCarrinho := postItemCarrinho(db, "Bearer "+token, `{"produtoId":"`+produtoID+`","estoqueId":"`+estoqueID+`","quantidade":1}`)
	if wCarrinho.Code != http.StatusCreated {
		t.Fatalf("seed carrinho: status = %d (body=%s)", wCarrinho.Code, wCarrinho.Body.String())
	}

	registro := realtime.NewRegistry()
	eventos, cancelar := registro.Subscribe()
	defer cancelar()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/pedidos",
		middleware.RequireAuth(db, testJWTSecret)(SubmeterPedidoHandler(db, registro)))

	r := httptest.NewRequest(http.MethodPost, "/api/pedidos", strings.NewReader(`{"solicitante":"Fulano","obraCentroCusto":"Obra X"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Pedido services.Pedido `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}

	select {
	case ev := <-eventos:
		if ev.Resource != "pedidos" || ev.ID != resp.Pedido.ID || ev.Change != "created" {
			t.Fatalf("evento = %+v, want {pedidos %s created}", ev, resp.Pedido.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("nenhum evento publicado em 1s após SubmeterPedidoHandler bem-sucedido")
	}
}

// TestSubmeterPedidoHandler_IgnoraUsuarioIdDoCorpo prova o Always de
// spec-7-2: `pedidos.usuario_id` é sempre o id da sessão, mesmo que o corpo
// tente forçar outro (campo que o handler nem decodifica).
func TestSubmeterPedidoHandler_IgnoraUsuarioIdDoCorpo(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	usuarioID, token := seedContaComumECarrinho(t, db, "Usuario Pedido Escopo", "pedido-escopo-usuario@empresa.com")
	outroID := criarContaComPapel(t, db, "Usuario Pedido Alvo Falso", "pedido-alvo-falso@empresa.com", "senha-123456", "usuario")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Pedido Escopo", 10)

	wCarrinho := postItemCarrinho(db, "Bearer "+token, `{"produtoId":"`+produtoID+`","estoqueId":"`+estoqueID+`","quantidade":1}`)
	if wCarrinho.Code != http.StatusCreated {
		t.Fatalf("seed carrinho: status = %d (body=%s)", wCarrinho.Code, wCarrinho.Body.String())
	}

	corpo := `{"solicitante":"Fulano","obraCentroCusto":"Obra X","usuarioId":"` + outroID + `"}`
	w := postPedido(db, "Bearer "+token, corpo)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Pedido services.Pedido `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Pedido.UsuarioID != usuarioID {
		t.Errorf("UsuarioID = %q, want %q (sempre a sessão)", resp.Pedido.UsuarioID, usuarioID)
	}
}

// TestSubmeterPedidoHandler_400CamposAusentes prova a linha "Solicitante/obra
// ausente" -> 400 VALIDATION_ERROR.
func TestSubmeterPedidoHandler_400CamposAusentes(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Pedido 400", "pedido-400-usuario@empresa.com")

	casos := map[string]string{
		"solicitante ausente":       `{"obraCentroCusto":"Obra X"}`,
		"solicitante em branco":     `{"solicitante":"   ","obraCentroCusto":"Obra X"}`,
		"obraCentroCusto ausente":   `{"solicitante":"Fulano"}`,
		"obraCentroCusto em branco": `{"solicitante":"Fulano","obraCentroCusto":"   "}`,
	}
	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			w := postPedido(db, "Bearer "+token, corpo)
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

// TestSubmeterPedidoHandler_400PayloadInvalido prova que um corpo não
// decodificável -> 400 VALIDATION_ERROR.
func TestSubmeterPedidoHandler_400PayloadInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Pedido Payload", "pedido-payload-usuario@empresa.com")

	w := postPedido(db, "Bearer "+token, `{"solicitante":`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// TestSubmeterPedidoHandler_409CarrinhoVazio prova a linha "Carrinho vazio"
// -> 409 CONFLICT.
func TestSubmeterPedidoHandler_409CarrinhoVazio(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Pedido 409 Vazio", "pedido-409-vazio-usuario@empresa.com")

	w := postPedido(db, "Bearer "+token, `{"solicitante":"Fulano","obraCentroCusto":"Obra X"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

// TestSubmeterPedidoHandler_409DisponibilidadeInsuficiente prova a linha
// "Disponibilidade insuficiente num item" -> 409 CONFLICT, carrinho
// permanece intacto (nenhum item removido).
func TestSubmeterPedidoHandler_409DisponibilidadeInsuficiente(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Pedido 409 Indisp", "pedido-409-indisp-usuario@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Pedido 409 Indisp", 3)

	wCarrinho := postItemCarrinho(db, "Bearer "+token, `{"produtoId":"`+produtoID+`","estoqueId":"`+estoqueID+`","quantidade":3}`)
	if wCarrinho.Code != http.StatusCreated {
		t.Fatalf("seed carrinho: status = %d (body=%s)", wCarrinho.Code, wCarrinho.Body.String())
	}
	// Saldo real cai para 0 depois da montagem do carrinho.
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 0 WHERE produto_id = $1 AND estoque_id = $2`, produtoID, estoqueID); err != nil {
		t.Fatalf("seed zerar saldo: %v", err)
	}

	w := postPedido(db, "Bearer "+token, `{"solicitante":"Fulano","obraCentroCusto":"Obra X"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}

	wCarrinhoDepois := getCarrinho(db, "Bearer "+token)
	var respCarrinho struct {
		Itens []services.ItemCarrinho `json:"itens"`
	}
	if err := json.Unmarshal(wCarrinhoDepois.Body.Bytes(), &respCarrinho); err != nil {
		t.Fatalf("decode carrinho: %v", err)
	}
	if len(respCarrinho.Itens) != 1 {
		t.Errorf("carrinho deveria continuar com 1 item, got %+v", respCarrinho.Itens)
	}
}

// TestSubmeterPedidoHandler_401SemToken prova que uma requisição sem
// Authorization -> 401.
func TestSubmeterPedidoHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)

	w := postPedido(db, "", `{"solicitante":"Fulano","obraCentroCusto":"Obra X"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// --- Consulta de Pedidos próprios — Story 7.3, spec-7-3 -------------------
//
// GET /api/pedidos e GET /api/pedidos/{id} ficam atrás SÓ de RequireAuth
// (SEM RequireRole — mesmo mínimo de papel do envio). Molde de despacho de
// getCarrinho (carrinho_test.go).

func getPedidos(db *sql.DB, authHeader, query string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/pedidos",
		middleware.RequireAuth(db, testJWTSecret)(ListarPedidosHandler(db)))
	url := "/api/pedidos"
	if query != "" {
		url += "?" + query
	}
	r := httptest.NewRequest(http.MethodGet, url, nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func getPedido(db *sql.DB, authHeader, id string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/pedidos/{id}",
		middleware.RequireAuth(db, testJWTSecret)(BuscarPedidoHandler(db)))
	r := httptest.NewRequest(http.MethodGet, "/api/pedidos/"+id, nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// seedPedidoViaServico monta e envia um Pedido de 1 item para `usuarioID`
// direto pela camada de serviço (sem a dança HTTP de carrinho+envio).
func seedPedidoViaServico(t *testing.T, db *sql.DB, usuarioID, nomeBase string, qtd float64) services.Pedido {
	t.Helper()
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, nomeBase, qtd+10)
	if _, err := services.AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, qtd); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho (%s): %v", nomeBase, err)
	}
	pedido, err := services.SubmeterPedido(db, usuarioID, "Solicitante "+nomeBase, "Obra "+nomeBase, "")
	if err != nil {
		t.Fatalf("seed SubmeterPedido (%s): %v", nomeBase, err)
	}
	return pedido
}

// --- ListarPedidosHandler -----------------------------------------------

// TestListarPedidosHandler_200Escopado cobre "Lista escopada ao dono" na
// fronteira HTTP: a sessão A só recebe os Pedidos de A.
func TestListarPedidosHandler_200Escopado(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	usuarioA, tokenA := seedContaComumECarrinho(t, db, "H Lista A 73", "h-pedidos-73-lista-a@empresa.com")
	usuarioB, _ := seedContaComumECarrinho(t, db, "H Lista B 73", "h-pedidos-73-lista-b@empresa.com")

	seedPedidoViaServico(t, db, usuarioA, "H73 Lista A1", 1)
	seedPedidoViaServico(t, db, usuarioA, "H73 Lista A2", 1)
	seedPedidoViaServico(t, db, usuarioB, "H73 Lista B1", 1)

	w := getPedidos(db, "Bearer "+tokenA, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedidos []services.PedidoResumo `json:"pedidos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Pedidos) != 2 {
		t.Fatalf("len(pedidos) = %d, want 2", len(resp.Pedidos))
	}
	for _, p := range resp.Pedidos {
		if p.UsuarioID != usuarioA {
			t.Errorf("pedido %s: UsuarioID = %q, want %q", p.ID, p.UsuarioID, usuarioA)
		}
		if p.QtdItens != 1 {
			t.Errorf("pedido %s: QtdItens = %d, want 1", p.ID, p.QtdItens)
		}
	}
}

// TestListarPedidosHandler_FiltroPorStatus cobre "Filtro por status".
func TestListarPedidosHandler_FiltroPorStatus(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	usuarioID, token := seedContaComumECarrinho(t, db, "H Filtro 73", "h-pedidos-73-filtro@empresa.com")

	seedPedidoViaServico(t, db, usuarioID, "H73 Filtro Pend", 1)
	aprovado := seedPedidoViaServico(t, db, usuarioID, "H73 Filtro Aprov", 1)
	if _, err := db.Exec(`UPDATE pedidos SET status = 'aprovado' WHERE id = $1`, aprovado.ID); err != nil {
		t.Fatalf("seed status: %v", err)
	}

	w := getPedidos(db, "Bearer "+token, "status=aprovado")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedidos []services.PedidoResumo `json:"pedidos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Pedidos) != 1 || resp.Pedidos[0].ID != aprovado.ID {
		t.Fatalf("pedidos = %+v, want só %s", resp.Pedidos, aprovado.ID)
	}
}

// TestListarPedidosHandler_400FiltroInvalido cobre "Filtro de status
// inválido" -> 400 VALIDATION_ERROR.
func TestListarPedidosHandler_400FiltroInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "H Filtro Ruim 73", "h-pedidos-73-filtro-ruim@empresa.com")

	w := getPedidos(db, "Bearer "+token, "status=banana")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// TestListarPedidosHandler_200Vazio cobre "Sem Pedidos" -> {"pedidos":[]}.
func TestListarPedidosHandler_200Vazio(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "H Vazio 73", "h-pedidos-73-vazio@empresa.com")

	w := getPedidos(db, "Bearer "+token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedidos []services.PedidoResumo `json:"pedidos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Pedidos == nil || len(resp.Pedidos) != 0 {
		t.Errorf("pedidos = %+v, want []", resp.Pedidos)
	}
}

// TestListarPedidosHandler_401SemToken cobre a ausência de Authorization.
func TestListarPedidosHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	w := getPedidos(db, "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// --- BuscarPedidoHandler ----------------------------------------------------

// TestBuscarPedidoHandler_200Dono cobre "Detalhe pelo dono".
func TestBuscarPedidoHandler_200Dono(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	usuarioID, token := seedContaComumECarrinho(t, db, "H Detalhe Dono 73", "h-pedidos-73-detalhe-dono@empresa.com")
	pedido := seedPedidoViaServico(t, db, usuarioID, "H73 Detalhe Dono", 2)

	w := getPedido(db, "Bearer "+token, pedido.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedido services.PedidoDetalhe `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Pedido.ID != pedido.ID || resp.Pedido.UsuarioID != usuarioID {
		t.Errorf("cabeçalho = {ID:%s UsuarioID:%s}", resp.Pedido.ID, resp.Pedido.UsuarioID)
	}
	if len(resp.Pedido.Itens) != 1 || resp.Pedido.Itens[0].Quantidade != 2 {
		t.Errorf("itens = %+v, want 1 item qtd 2", resp.Pedido.Itens)
	}
}

// TestBuscarPedidoHandler_404PedidoAlheioComoUsuario cobre "Detalhe de
// Pedido alheio, papel usuario" -> 404 NOT_FOUND (nunca 403, nunca revela).
func TestBuscarPedidoHandler_404PedidoAlheioComoUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H Dono Alheio 73", "h-pedidos-73-dono-alheio@empresa.com")
	_, tokenOutro := seedContaComumECarrinho(t, db, "H Outro 73", "h-pedidos-73-outro@empresa.com")
	pedido := seedPedidoViaServico(t, db, dono, "H73 Alheio", 1)

	w := getPedido(db, "Bearer "+tokenOutro, pedido.ID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
	}
}

// TestBuscarPedidoHandler_200ComoAlmoxarife cobre "Detalhe de Pedido alheio,
// papel almoxarife+" -> 200 (padrão de escopo AD-8).
func TestBuscarPedidoHandler_200ComoAlmoxarife(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H Dono p/ Almox 73", "h-pedidos-73-dono-almox@empresa.com")
	criarContaComPapel(t, db, "H Almox 73", "h-pedidos-73-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-73-almox@empresa.com", "senha-123456")
	pedido := seedPedidoViaServico(t, db, dono, "H73 Almox Ve", 1)

	w := getPedido(db, "Bearer "+tokenAlmox, pedido.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedido services.PedidoDetalhe `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Pedido.ID != pedido.ID || len(resp.Pedido.Itens) != 1 {
		t.Errorf("pedido = {ID:%s Itens:%d}, want {ID:%s Itens:1}", resp.Pedido.ID, len(resp.Pedido.Itens), pedido.ID)
	}
}

// TestBuscarPedidoHandler_404IdMalformado cobre "Detalhe com id
// inexistente/malformado" -> mesma resposta 404 NOT_FOUND.
func TestBuscarPedidoHandler_404IdMalformado(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "H Id Ruim 73", "h-pedidos-73-id-ruim@empresa.com")

	w := getPedido(db, "Bearer "+token, "nao-e-uuid")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
	}
}

// TestBuscarPedidoHandler_404IdInexistenteBemFormado cobre "Detalhe com id
// inexistente" na variante de UUID bem-formado (mas de nenhum Pedido) — a
// mesma resposta 404 NOT_FOUND, agora provada na fronteira HTTP e não só no
// service (TestBuscarPedidoProprio_IdMalformadoOuInexistente cobre isso em
// services/pedidos_test.go; este caso faltava aqui).
func TestBuscarPedidoHandler_404IdInexistenteBemFormado(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "H Id Inexistente 73", "h-pedidos-73-id-inexistente@empresa.com")

	w := getPedido(db, "Bearer "+token, "00000000-0000-0000-0000-000000000000")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
	}
}

// TestListarPedidosHandler_AlmoxarifeSoVeOsProprios cobre que GET
// /api/pedidos permanece escopado à sessão MESMO para papel almoxarife+: o
// padrão de escopo AD-8 (dono OU almoxarife+) vale só para o acesso por id
// (BuscarPedidoHandler) — a listagem (ListarPedidosHandler) nunca devolve
// Pedidos de outro usuário, para nenhum papel. ListarPedidosHandler nem
// recebe o papel da sessão como parâmetro; este teste trava esse contrato na
// fronteira HTTP para que uma futura mudança não estenda por engano a mesma
// escalada de papel da consulta por id para a listagem.
func TestListarPedidosHandler_AlmoxarifeSoVeOsProprios(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H Lista Dono Alheio 73", "h-pedidos-73-lista-dono-alheio@empresa.com")
	criarContaComPapel(t, db, "H Lista Almox 73", "h-pedidos-73-lista-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-73-lista-almox@empresa.com", "senha-123456")

	seedPedidoViaServico(t, db, dono, "H73 Lista Almox Alheio", 1)

	w := getPedidos(db, "Bearer "+tokenAlmox, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedidos []services.PedidoResumo `json:"pedidos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Pedidos) != 0 {
		t.Fatalf("pedidos = %+v, want [] (almoxarife não tem Pedidos próprios e não deve ver os do dono)", resp.Pedidos)
	}
}
