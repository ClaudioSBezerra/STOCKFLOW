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

// --- ?escopo=todos — Fila do Almoxarife (Story 7.4, spec-7-4) --------------
//
// MESMA rota GET /api/pedidos (getPedidos, definido acima) — só o parâmetro
// de query muda. TestListarPedidosHandler_AlmoxarifeSoVeOsProprios acima
// permanece intocado: cobre a chamada SEM `escopo`.

// TestListarPedidosHandler_EscopoTodosAlmoxarifeVeDeTodos cobre a linha
// "Fila escopada a todos" da I/O Matrix: almoxarife+ com `?escopo=todos`
// recebe os Pedidos de QUALQUER usuário.
func TestListarPedidosHandler_EscopoTodosAlmoxarifeVeDeTodos(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H74 Escopo Dono", "h-pedidos-74-escopo-dono@empresa.com")
	criarContaComPapel(t, db, "H74 Escopo Almox", "h-pedidos-74-escopo-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-74-escopo-almox@empresa.com", "senha-123456")

	pedido := seedPedidoViaServico(t, db, dono, "H74 Escopo Todos", 1)

	w := getPedidos(db, "Bearer "+tokenAlmox, "escopo=todos")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedidos []services.PedidoResumo `json:"pedidos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Pedidos) != 1 || resp.Pedidos[0].ID != pedido.ID {
		t.Fatalf("pedidos = %+v, want só %s (do dono, visto pelo almoxarife na fila)", resp.Pedidos, pedido.ID)
	}
}

// TestListarPedidosHandler_EscopoTodosPapelInsuficienteCaiNoProprio cobre a
// linha "Escopo todos ignorado para papel insuficiente": usuário comum com
// `?escopo=todos` recebe só os próprios Pedidos, NUNCA 403 (AD-8, epics.md
// Story 7.4 AC2).
func TestListarPedidosHandler_EscopoTodosPapelInsuficienteCaiNoProprio(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	usuarioA, tokenA := seedContaComumECarrinho(t, db, "H74 Insuf A", "h-pedidos-74-insuf-a@empresa.com")
	usuarioB, _ := seedContaComumECarrinho(t, db, "H74 Insuf B", "h-pedidos-74-insuf-b@empresa.com")

	proprio := seedPedidoViaServico(t, db, usuarioA, "H74 Insuf Proprio", 1)
	seedPedidoViaServico(t, db, usuarioB, "H74 Insuf Alheio", 1)

	w := getPedidos(db, "Bearer "+tokenA, "escopo=todos")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedidos []services.PedidoResumo `json:"pedidos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Pedidos) != 1 || resp.Pedidos[0].ID != proprio.ID {
		t.Fatalf("pedidos = %+v, want só o próprio %s", resp.Pedidos, proprio.ID)
	}
}

// TestListarPedidosHandler_EscopoTodosFiltroInvalido cobre "Filtro de status
// inválido no escopo todos" -> 400 VALIDATION_ERROR, requisição rejeitada
// antes de tocar o banco.
func TestListarPedidosHandler_EscopoTodosFiltroInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "H74 Filtro Ruim Almox", "h-pedidos-74-filtro-ruim-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-74-filtro-ruim-almox@empresa.com", "senha-123456")

	w := getPedidos(db, "Bearer "+tokenAlmox, "escopo=todos&status=banana")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// TestListarPedidosHandler_EscopoTodosFiltroPorStatus cobre "Fila filtrada
// por status": almoxarife+ com `?escopo=todos&status=pendente` recebe só os
// Pedidos `pendente` de QUALQUER usuário.
func TestListarPedidosHandler_EscopoTodosFiltroPorStatus(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H74 Fila Status Dono", "h-pedidos-74-fila-status-dono@empresa.com")
	criarContaComPapel(t, db, "H74 Fila Status Almox", "h-pedidos-74-fila-status-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-74-fila-status-almox@empresa.com", "senha-123456")

	pendente := seedPedidoViaServico(t, db, dono, "H74 Fila Status Pend", 1)
	aprovado := seedPedidoViaServico(t, db, dono, "H74 Fila Status Aprov", 1)
	if _, err := db.Exec(`UPDATE pedidos SET status = 'aprovado' WHERE id = $1`, aprovado.ID); err != nil {
		t.Fatalf("seed status: %v", err)
	}

	w := getPedidos(db, "Bearer "+tokenAlmox, "escopo=todos&status=pendente")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedidos []services.PedidoResumo `json:"pedidos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Pedidos) != 1 || resp.Pedidos[0].ID != pendente.ID {
		t.Fatalf("pedidos = %+v, want só %s", resp.Pedidos, pendente.ID)
	}
}

// TestListarPedidosHandler_EscopoTodosFiltroPorStatusParcialmenteAprovado
// cobre, na fronteira HTTP, o mesmo valor novo de status introduzido pela
// Story 7.5 (spec-7-5) — nenhum teste até aqui exercitava
// `?status=parcialmente_aprovado` além dos testes de RESULTADO de
// DecidirPedidoHandler; este cobre o FILTRO da Fila por esse valor.
func TestListarPedidosHandler_EscopoTodosFiltroPorStatusParcialmenteAprovado(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H75 Fila Status Dono", "h-pedidos-75-fila-status-dono@empresa.com")
	criarContaComPapel(t, db, "H75 Fila Status Almox", "h-pedidos-75-fila-status-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-75-fila-status-almox@empresa.com", "senha-123456")

	pendente := seedPedidoViaServico(t, db, dono, "H75 Fila Status Pend", 1)
	parcial := seedPedidoViaServico(t, db, dono, "H75 Fila Status Parcial", 1)
	if _, err := db.Exec(`UPDATE pedidos SET status = 'parcialmente_aprovado' WHERE id = $1`, parcial.ID); err != nil {
		t.Fatalf("seed status: %v", err)
	}

	w := getPedidos(db, "Bearer "+tokenAlmox, "escopo=todos&status=parcialmente_aprovado")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedidos []services.PedidoResumo `json:"pedidos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Pedidos) != 1 || resp.Pedidos[0].ID != parcial.ID {
		t.Fatalf("pedidos = %+v, want só %s", resp.Pedidos, parcial.ID)
	}
	_ = pendente
}

// TestListarPedidosHandler_EscopoDesconhecidoCaiNoProprio cobre "Valor de
// escopo desconhecido": só `"todos"` ativa a fila — qualquer outro valor
// cai no escopo próprio, sem erro.
func TestListarPedidosHandler_EscopoDesconhecidoCaiNoProprio(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H74 Escopo Banana Dono", "h-pedidos-74-escopo-banana-dono@empresa.com")
	criarContaComPapel(t, db, "H74 Escopo Banana Almox", "h-pedidos-74-escopo-banana-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-74-escopo-banana-almox@empresa.com", "senha-123456")

	seedPedidoViaServico(t, db, dono, "H74 Escopo Banana Alheio", 1)

	w := getPedidos(db, "Bearer "+tokenAlmox, "escopo=banana")
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
		t.Fatalf("pedidos = %+v, want [] (almoxarife sem Pedidos próprios; escopo=banana não ativa a fila)", resp.Pedidos)
	}
}

// --- DecidirPedidoHandler (Story 7.5, spec-7-5) -----------------------------
//
// POST /api/pedidos/{id}/decisao -> RequireAuth -> RequireRole(almoxarife) ->
// handler. Molde de postPedido acima, com RequireRole na composição (mesmo
// molde de getInconsistencias, normalizacao_test.go).

func postDecisaoPedido(db *sql.DB, authHeader, id, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/pedidos/{id}/decisao",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				DecidirPedidoHandler(db, realtime.NewRegistry()))))
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, "/api/pedidos/"+id+"/decisao", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, "/api/pedidos/"+id+"/decisao", nil)
	}
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// TestDecidirPedidoHandler_200AprovacaoTotal cobre "Aprovação total" na
// fronteira HTTP: saldo suficiente -> 200, status `aprovado`, item com
// `quantidadeAprovada` igual ao solicitado.
func TestDecidirPedidoHandler_200AprovacaoTotal(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H75 Aprov Total Dono", "h-pedidos-75-aprov-total-dono@empresa.com")
	criarContaComPapel(t, db, "H75 Aprov Total Almox", "h-pedidos-75-aprov-total-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-75-aprov-total-almox@empresa.com", "senha-123456")
	pedido := seedPedidoViaServico(t, db, dono, "H75 Aprov Total", 3)

	w := postDecisaoPedido(db, "Bearer "+tokenAlmox, pedido.ID, `{"aprovar":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedido services.PedidoDetalhe `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Pedido.Status != "aprovado" {
		t.Errorf("Status = %q, want aprovado", resp.Pedido.Status)
	}
	if len(resp.Pedido.Itens) != 1 || resp.Pedido.Itens[0].QuantidadeAprovada == nil || *resp.Pedido.Itens[0].QuantidadeAprovada != 3 {
		t.Errorf("itens = %+v, want 1 item com quantidadeAprovada=3", resp.Pedido.Itens)
	}
}

// TestDecidirPedidoHandler_200AprovacaoParcial cobre "Aprovação parcial" na
// fronteira HTTP: saldo real cai abaixo do solicitado entre o envio e a
// decisão -> 200, status `parcialmente_aprovado`, `quantidadeAprovada`
// reflete só o disponível.
func TestDecidirPedidoHandler_200AprovacaoParcial(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H75 Aprov Parcial Dono", "h-pedidos-75-aprov-parcial-dono@empresa.com")
	criarContaComPapel(t, db, "H75 Aprov Parcial Almox", "h-pedidos-75-aprov-parcial-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-75-aprov-parcial-almox@empresa.com", "senha-123456")
	pedido := seedPedidoViaServico(t, db, dono, "H75 Aprov Parcial", 10)

	var produtoID, estoqueID string
	if err := db.QueryRow(`SELECT produto_id, estoque_id FROM pedido_itens WHERE pedido_id = $1`, pedido.ID).
		Scan(&produtoID, &estoqueID); err != nil {
		t.Fatalf("seed: buscar item do pedido: %v", err)
	}
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 4 WHERE produto_id = $1 AND estoque_id = $2`, produtoID, estoqueID); err != nil {
		t.Fatalf("seed: reduzir saldo real: %v", err)
	}

	w := postDecisaoPedido(db, "Bearer "+tokenAlmox, pedido.ID, `{"aprovar":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedido services.PedidoDetalhe `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Pedido.Status != "parcialmente_aprovado" {
		t.Errorf("Status = %q, want parcialmente_aprovado", resp.Pedido.Status)
	}
	if len(resp.Pedido.Itens) != 1 || resp.Pedido.Itens[0].QuantidadeAprovada == nil || *resp.Pedido.Itens[0].QuantidadeAprovada != 4 {
		t.Errorf("itens = %+v, want 1 item com quantidadeAprovada=4", resp.Pedido.Itens)
	}
	if resp.Pedido.Itens[0].Quantidade != 10 {
		t.Errorf("Quantidade solicitada = %v, want 10 (lado a lado com quantidadeAprovada)", resp.Pedido.Itens[0].Quantidade)
	}
}

// TestDecidirPedidoHandler_200Rejeicao cobre "Rejeição" na fronteira HTTP:
// `{"aprovar":false}` -> 200, status `rejeitado`, item `quantidadeAprovada=0`.
func TestDecidirPedidoHandler_200Rejeicao(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H75 Rejeicao Dono", "h-pedidos-75-rejeicao-dono@empresa.com")
	criarContaComPapel(t, db, "H75 Rejeicao Almox", "h-pedidos-75-rejeicao-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-75-rejeicao-almox@empresa.com", "senha-123456")
	pedido := seedPedidoViaServico(t, db, dono, "H75 Rejeicao", 2)

	w := postDecisaoPedido(db, "Bearer "+tokenAlmox, pedido.ID, `{"aprovar":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedido services.PedidoDetalhe `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Pedido.Status != "rejeitado" {
		t.Errorf("Status = %q, want rejeitado", resp.Pedido.Status)
	}
	if len(resp.Pedido.Itens) != 1 || resp.Pedido.Itens[0].QuantidadeAprovada == nil || *resp.Pedido.Itens[0].QuantidadeAprovada != 0 {
		t.Errorf("itens = %+v, want 1 item com quantidadeAprovada=0", resp.Pedido.Itens)
	}
}

// TestDecidirPedidoHandler_403PapelInsuficiente cobre "Papel insuficiente"
// na fronteira HTTP: papel `usuario` -> 403 FORBIDDEN, decidido por
// RequireRole ANTES de qualquer leitura/escrita em produto_estoque —
// status do Pedido continua `pendente`.
func TestDecidirPedidoHandler_403PapelInsuficiente(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, tokenDono := seedContaComumECarrinho(t, db, "H75 Papel Insuf", "h-pedidos-75-papel-insuf@empresa.com")
	pedido := seedPedidoViaServico(t, db, dono, "H75 Papel Insuf", 1)

	w := postDecisaoPedido(db, "Bearer "+tokenDono, pedido.ID, `{"aprovar":true}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM pedidos WHERE id = $1`, pedido.ID).Scan(&status); err != nil {
		t.Fatalf("reler status: %v", err)
	}
	if status != "pendente" {
		t.Errorf("status = %q, want pendente (nenhuma escrita antes do service)", status)
	}
}

// TestDecidirPedidoHandler_409PedidoJaDecidido cobre "Pedido já decidido" na
// fronteira HTTP -> 409 CONFLICT.
func TestDecidirPedidoHandler_409PedidoJaDecidido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H75 JaDecidido Dono", "h-pedidos-75-ja-decidido-dono@empresa.com")
	criarContaComPapel(t, db, "H75 JaDecidido Almox", "h-pedidos-75-ja-decidido-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-75-ja-decidido-almox@empresa.com", "senha-123456")
	pedido := seedPedidoViaServico(t, db, dono, "H75 JaDecidido", 1)
	if _, err := db.Exec(`UPDATE pedidos SET status = 'aprovado' WHERE id = $1`, pedido.ID); err != nil {
		t.Fatalf("seed status: %v", err)
	}

	w := postDecisaoPedido(db, "Bearer "+tokenAlmox, pedido.ID, `{"aprovar":true}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

// TestDecidirPedidoHandler_400CorpoSemAprovar cobre "Corpo sem aprovar" na
// fronteira HTTP -> 400 VALIDATION_ERROR, requisição recusada antes de
// tocar o banco.
func TestDecidirPedidoHandler_400CorpoSemAprovar(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H75 SemAprovar Dono", "h-pedidos-75-sem-aprovar-dono@empresa.com")
	criarContaComPapel(t, db, "H75 SemAprovar Almox", "h-pedidos-75-sem-aprovar-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-75-sem-aprovar-almox@empresa.com", "senha-123456")
	pedido := seedPedidoViaServico(t, db, dono, "H75 SemAprovar", 1)

	casos := map[string]string{
		"corpo vazio":        `{}`,
		"aprovar nulo":       `{"aprovar":null}`,
		"payload malformado": `{"aprovar":`,
	}
	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			w := postDecisaoPedido(db, "Bearer "+tokenAlmox, pedido.ID, corpo)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
				t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
			}
		})
	}
}

// TestDecidirPedidoHandler_404IdInexistenteOuMalformado cobre "Id
// inexistente/malformado" na fronteira HTTP -> 404 NOT_FOUND para os dois
// casos.
func TestDecidirPedidoHandler_404IdInexistenteOuMalformado(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "H75 IdRuim Almox", "h-pedidos-75-id-ruim-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-75-id-ruim-almox@empresa.com", "senha-123456")

	w1 := postDecisaoPedido(db, "Bearer "+tokenAlmox, "nao-e-uuid", `{"aprovar":true}`)
	if w1.Code != http.StatusNotFound {
		t.Fatalf("id malformado: status = %d, want 404 (body=%s)", w1.Code, w1.Body.String())
	}
	w2 := postDecisaoPedido(db, "Bearer "+tokenAlmox, "00000000-0000-0000-0000-000000000000", `{"aprovar":true}`)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("id inexistente: status = %d, want 404 (body=%s)", w2.Code, w2.Body.String())
	}
}

// TestDecidirPedidoHandler_401SemToken cobre a ausência de Authorization.
func TestDecidirPedidoHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	w := postDecisaoPedido(db, "", "00000000-0000-0000-0000-000000000000", `{"aprovar":true}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// TestDecidirPedidoHandler_PublicaEventoNoSucesso prova que uma decisão
// bem-sucedida publica `{"resource":"pedidos","id":<pedido>,"change":<novo
// status>}` no canal `pedidos` — o badge muda via SSE, sem recarregar a
// página (AC2). Molde de TestSubmeterPedidoHandler_PublicaEventoNoSucesso.
func TestDecidirPedidoHandler_PublicaEventoNoSucesso(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H75 Evento Dono", "h-pedidos-75-evento-dono@empresa.com")
	criarContaComPapel(t, db, "H75 Evento Almox", "h-pedidos-75-evento-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-75-evento-almox@empresa.com", "senha-123456")
	pedido := seedPedidoViaServico(t, db, dono, "H75 Evento", 1)

	registro := realtime.NewRegistry()
	eventos, cancelar := registro.Subscribe()
	defer cancelar()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/pedidos/{id}/decisao",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				DecidirPedidoHandler(db, registro))))

	r := httptest.NewRequest(http.MethodPost, "/api/pedidos/"+pedido.ID+"/decisao", strings.NewReader(`{"aprovar":true}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+tokenAlmox)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	select {
	case ev := <-eventos:
		if ev.Resource != "pedidos" || ev.ID != pedido.ID || ev.Change != "aprovado" {
			t.Fatalf("evento = %+v, want {pedidos %s aprovado}", ev, pedido.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("nenhum evento publicado em 1s após DecidirPedidoHandler bem-sucedido")
	}
}
