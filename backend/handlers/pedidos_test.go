package handlers

import (
	"bytes"
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
	mux.HandleFunc("POST /e/{slug}/api/pedidos",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(SubmeterPedidoHandler(db, realtime.NewRegistry()))))
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos", nil)
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
	eventos, cancelar := registro.Subscribe(empresaTeste)
	defer cancelar()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /e/{slug}/api/pedidos",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(SubmeterPedidoHandler(db, registro))))

	r := httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos", strings.NewReader(`{"solicitante":"Fulano","obraCentroCusto":"Obra X"}`))
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
	mux.HandleFunc("GET /e/{slug}/api/pedidos",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(ListarPedidosHandler(db))))
	url := prefixoEmpresaTeste + "/api/pedidos"
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
	mux.HandleFunc("GET /e/{slug}/api/pedidos/{id}",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(BuscarPedidoHandler(db))))
	r := httptest.NewRequest(http.MethodGet, prefixoEmpresaTeste+"/api/pedidos/"+id, nil)
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
	if _, err := services.AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoID, estoqueID, qtd); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho (%s): %v", nomeBase, err)
	}
	pedido, err := services.SubmeterPedido(db, empresaTeste, usuarioID, "Solicitante "+nomeBase, "Obra "+nomeBase, "")
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
	mux.HandleFunc("POST /e/{slug}/api/pedidos/{id}/decisao",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(
				middleware.RequireRole(services.PapelAlmoxarife)(
					DecidirPedidoHandler(db, realtime.NewRegistry())))))
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos/"+id+"/decisao", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos/"+id+"/decisao", nil)
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
	eventos, cancelar := registro.Subscribe(empresaTeste)
	defer cancelar()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /e/{slug}/api/pedidos/{id}/decisao",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(
				middleware.RequireRole(services.PapelAlmoxarife)(
					DecidirPedidoHandler(db, registro)))))

	r := httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos/"+pedido.ID+"/decisao", strings.NewReader(`{"aprovar":true}`))
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

// --- BaixarReciboPedidoHandler — Story 7.6, spec-7-6 ------------------------
//
// GET /api/pedidos/{id}/recibo fica atrás SÓ de RequireAuth — SEM
// RequireRole, mesma composição de getPedido acima (molde EXATO de
// BuscarPedidoHandler).

func getRecibo(db *sql.DB, authHeader, id string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /e/{slug}/api/pedidos/{id}/recibo",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(BaixarReciboPedidoHandler(db))))
	r := httptest.NewRequest(http.MethodGet, prefixoEmpresaTeste+"/api/pedidos/"+id+"/recibo", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// decidirPedidoViaServico decide (aprova/rejeita) o Pedido `id` direto pela
// camada de serviço — sem a dança HTTP de POST /decisao — para os testes
// desta seção que só precisam de um Pedido JÁ decidido.
func decidirPedidoViaServico(t *testing.T, db *sql.DB, pedidoID, almoxID string, aprovar bool) services.PedidoDetalhe {
	t.Helper()
	det, err := services.DecidirPedido(db, empresaTeste, pedidoID, almoxID, services.PapelAlmoxarife, aprovar)
	if err != nil {
		t.Fatalf("seed DecidirPedido: %v", err)
	}
	return det
}

// TestBaixarReciboPedidoHandler_200Dono cobre "Pedido aprovado, dono" da I/O
// Matrix: 200, `Content-Type: application/pdf`, `Content-Disposition` de
// download com o id do Pedido, corpo inicia com `%PDF-`.
func TestBaixarReciboPedidoHandler_200Dono(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, tokenDono := seedContaComumECarrinho(t, db, "H76 Dono", "h-pedidos-76-dono@empresa.com")
	almoxID := criarContaComPapel(t, db, "H76 Dono Almox", "h-pedidos-76-dono-almox@empresa.com", "senha-123456", "almoxarife")
	pedido := seedPedidoViaServico(t, db, dono, "H76 Dono", 3)
	decidirPedidoViaServico(t, db, pedido.ID, almoxID, true)

	w := getRecibo(db, "Bearer "+tokenDono, pedido.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
	wantDisposition := `attachment; filename="recibo-pedido-` + pedido.ID + `.pdf"`
	if cd := w.Header().Get("Content-Disposition"); cd != wantDisposition {
		t.Errorf("Content-Disposition = %q, want %q", cd, wantDisposition)
	}
	if !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF-")) {
		t.Errorf("corpo não começa com %%PDF-: %q", w.Body.Bytes()[:min(20, w.Body.Len())])
	}
}

// TestBaixarReciboPedidoHandler_200AlmoxarifeAlheio cobre "Pedido
// parcialmente aprovado, almoxarife alheio" da I/O Matrix: mesmo PDF (200)
// para um `almoxarife`+ que não é o dono do Pedido (AD-8).
func TestBaixarReciboPedidoHandler_200AlmoxarifeAlheio(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H76 Almox Dono", "h-pedidos-76-almox-dono@empresa.com")
	almoxID := criarContaComPapel(t, db, "H76 Almox", "h-pedidos-76-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-76-almox@empresa.com", "senha-123456")
	pedido := seedPedidoViaServico(t, db, dono, "H76 Almox", 10)

	var produtoID, estoqueID string
	if err := db.QueryRow(`SELECT produto_id, estoque_id FROM pedido_itens WHERE pedido_id = $1`, pedido.ID).
		Scan(&produtoID, &estoqueID); err != nil {
		t.Fatalf("seed: buscar item do pedido: %v", err)
	}
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 4 WHERE produto_id = $1 AND estoque_id = $2`, produtoID, estoqueID); err != nil {
		t.Fatalf("seed: reduzir saldo real: %v", err)
	}
	decidirPedidoViaServico(t, db, pedido.ID, almoxID, true)

	w := getRecibo(db, "Bearer "+tokenAlmox, pedido.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF-")) {
		t.Errorf("corpo não começa com %%PDF-")
	}
}

// TestBaixarReciboPedidoHandler_409Pendente cobre "Pedido pendente" da I/O
// Matrix -> 409 CONFLICT, nenhum PDF gerado.
func TestBaixarReciboPedidoHandler_409Pendente(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, tokenDono := seedContaComumECarrinho(t, db, "H76 Pendente", "h-pedidos-76-pendente@empresa.com")
	pedido := seedPedidoViaServico(t, db, dono, "H76 Pendente", 1)

	w := getRecibo(db, "Bearer "+tokenDono, pedido.ID)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

// TestBaixarReciboPedidoHandler_409Rejeitado cobre "Pedido rejeitado" da I/O
// Matrix -> 409 CONFLICT.
func TestBaixarReciboPedidoHandler_409Rejeitado(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, tokenDono := seedContaComumECarrinho(t, db, "H76 Rejeitado", "h-pedidos-76-rejeitado@empresa.com")
	almoxID := criarContaComPapel(t, db, "H76 Rejeitado Almox", "h-pedidos-76-rejeitado-almox@empresa.com", "senha-123456", "almoxarife")
	pedido := seedPedidoViaServico(t, db, dono, "H76 Rejeitado", 1)
	decidirPedidoViaServico(t, db, pedido.ID, almoxID, false)

	w := getRecibo(db, "Bearer "+tokenDono, pedido.ID)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

// TestBaixarReciboPedidoHandler_404PedidoAlheio cobre "Pedido alheio, papel
// usuario" da I/O Matrix -> 404 NOT_FOUND, nunca revela a existência do
// Pedido.
func TestBaixarReciboPedidoHandler_404PedidoAlheio(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H76 Alheio Dono", "h-pedidos-76-alheio-dono@empresa.com")
	outro, tokenOutro := seedContaComumECarrinho(t, db, "H76 Alheio Outro", "h-pedidos-76-alheio-outro@empresa.com")
	_ = outro
	almoxID := criarContaComPapel(t, db, "H76 Alheio Almox", "h-pedidos-76-alheio-almox@empresa.com", "senha-123456", "almoxarife")
	pedido := seedPedidoViaServico(t, db, dono, "H76 Alheio", 1)
	decidirPedidoViaServico(t, db, pedido.ID, almoxID, true)

	w := getRecibo(db, "Bearer "+tokenOutro, pedido.ID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
	}
}

// TestBaixarReciboPedidoHandler_404IdInexistenteOuMalformado cobre "Id
// malformado/inexistente" da I/O Matrix -> 404 NOT_FOUND para os dois casos
// (mesmo colapso de BuscarPedidoHandler).
func TestBaixarReciboPedidoHandler_404IdInexistenteOuMalformado(t *testing.T) {
	db := testDB(t)
	_, token := seedContaComumECarrinho(t, db, "H76 IdRuim", "h-pedidos-76-id-ruim@empresa.com")

	w1 := getRecibo(db, "Bearer "+token, "nao-e-uuid")
	if w1.Code != http.StatusNotFound {
		t.Fatalf("id malformado: status = %d, want 404 (body=%s)", w1.Code, w1.Body.String())
	}
	w2 := getRecibo(db, "Bearer "+token, "00000000-0000-0000-0000-000000000000")
	if w2.Code != http.StatusNotFound {
		t.Fatalf("id inexistente: status = %d, want 404 (body=%s)", w2.Code, w2.Body.String())
	}
}

// TestBaixarReciboPedidoHandler_401SemToken cobre a ausência de Authorization
// -> 401, recusada antes do handler.
func TestBaixarReciboPedidoHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	w := getRecibo(db, "", "00000000-0000-0000-0000-000000000000")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// --- SubmeterPedidoTransferenciaHandler (Story 20.1, Epic 20, spec-20-1) ---

func postPedidoTransferencia(db *sql.DB, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /e/{slug}/api/pedidos/transferencia",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(SubmeterPedidoTransferenciaHandler(db, realtime.NewRegistry()))))
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos/transferencia", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos/transferencia", nil)
	}
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// seedPedidoTransferenciaViaServico cria um Produto com saldo na origem +
// um segundo Estoque (destino vazio, nunca recebe saldo) — trio pronto para
// POST /api/pedidos/transferencia.
func seedPedidoTransferenciaViaServico(t *testing.T, db *sql.DB, nomeBase string, saldoOrigem float64) (produtoID, estoqueOrigemID, estoqueDestinoID string) {
	t.Helper()
	produtoID, estoqueOrigemID = seedProdutoComSaldoHandler(t, db, nomeBase, saldoOrigem)
	estoqueDestino, err := services.CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), nomeBase+" Destino")
	if err != nil {
		t.Fatalf("seed CriarEstoque (destino, %s): %v", nomeBase, err)
	}
	return produtoID, estoqueOrigemID, estoqueDestino.ID
}

// TestSubmeterPedidoTransferenciaHandler_201 prova a AC1 na fronteira HTTP:
// produto com saldo suficiente na origem + campos obrigatórios preenchidos
// -> 201 com o Pedido `pendente` tipo=transferencia.
func TestSubmeterPedidoTransferenciaHandler_201(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Transf H 201", "transf-h-201-usuario@empresa.com")
	produtoID, estoqueOrigemID, estoqueDestinoID := seedPedidoTransferenciaViaServico(t, db, "Canteiro Transf H 201", 10)

	corpo := `{"obraCentroCusto":"Obra Norte","produtoId":"` + produtoID +
		`","estoqueOrigemId":"` + estoqueOrigemID + `","estoqueDestinoId":"` + estoqueDestinoID + `","quantidade":5}`
	w := postPedidoTransferencia(db, "Bearer "+token, corpo)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Pedido services.Pedido `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Pedido.Status != "pendente" {
		t.Errorf("Status = %q, want pendente", resp.Pedido.Status)
	}
	if resp.Pedido.Tipo != "transferencia" {
		t.Errorf("Tipo = %q, want transferencia", resp.Pedido.Tipo)
	}
	if resp.Pedido.Solicitante != "Usuario Transf H 201" {
		t.Errorf("Solicitante = %q, want %q (nome da sessão autenticada)", resp.Pedido.Solicitante, "Usuario Transf H 201")
	}

	var nItens int
	if err := db.QueryRow(`SELECT count(*) FROM pedido_itens WHERE pedido_id = $1 AND estoque_destino_id = $2`, resp.Pedido.ID, estoqueDestinoID).Scan(&nItens); err != nil {
		t.Fatalf("count pedido_itens: %v", err)
	}
	if nItens != 1 {
		t.Errorf("pedido_itens com estoque_destino_id = %d linhas, want 1", nItens)
	}
}

// TestSubmeterPedidoTransferenciaHandler_IgnoraSolicitanteDoCorpo prova que
// um cliente mal-intencionado (ou um client antigo que ainda envie
// `solicitante` no corpo) NUNCA consegue gravar um nome diferente do usuário
// autenticado — o campo nem existe mais em `submeterPedidoTransferenciaRequest`,
// o decode simplesmente ignora a chave extra. Mesmo molde de
// TestSubmeterPedidoBaixaHandler_IgnoraSolicitanteDoCorpo.
func TestSubmeterPedidoTransferenciaHandler_IgnoraSolicitanteDoCorpo(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Transf H IgnoraSolic", "transf-h-ignorasolic@empresa.com")
	produtoID, estoqueOrigemID, estoqueDestinoID := seedPedidoTransferenciaViaServico(t, db, "Canteiro Transf H IgnoraSolic", 10)

	corpo := `{"solicitante":"Nome Forjado","obraCentroCusto":"Obra X","produtoId":"` + produtoID +
		`","estoqueOrigemId":"` + estoqueOrigemID + `","estoqueDestinoId":"` + estoqueDestinoID + `","quantidade":1}`
	w := postPedidoTransferencia(db, "Bearer "+token, corpo)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Pedido services.Pedido `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Pedido.Solicitante != "Usuario Transf H IgnoraSolic" {
		t.Errorf("Solicitante = %q, want %q (nunca \"Nome Forjado\")", resp.Pedido.Solicitante, "Usuario Transf H IgnoraSolic")
	}
}

// TestSubmeterPedidoTransferenciaHandler_201_ObraCentroCustoAusente prova o
// requisito do usuário (pós-revisão): "obraCentroCusto" ausente/vazio nunca
// bloqueia o envio -> 201, `ObraCentroCusto` volta "" (NULL no banco). Mesmo
// molde de TestSubmeterPedidoBaixaHandler_201_ObraCentroCustoAusente.
func TestSubmeterPedidoTransferenciaHandler_201_ObraCentroCustoAusente(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Transf H SemObra", "transf-h-semobra@empresa.com")
	produtoID, estoqueOrigemID, estoqueDestinoID := seedPedidoTransferenciaViaServico(t, db, "Canteiro Transf H SemObra", 10)

	corpo := `{"produtoId":"` + produtoID + `","estoqueOrigemId":"` + estoqueOrigemID +
		`","estoqueDestinoId":"` + estoqueDestinoID + `","quantidade":1}`
	w := postPedidoTransferencia(db, "Bearer "+token, corpo)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Pedido services.Pedido `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Pedido.ObraCentroCusto != "" {
		t.Errorf("ObraCentroCusto = %q, want \"\"", resp.Pedido.ObraCentroCusto)
	}

	var obraNoBanco sql.NullString
	if err := db.QueryRow(`SELECT obra_centro_custo FROM pedidos WHERE id = $1`, resp.Pedido.ID).Scan(&obraNoBanco); err != nil {
		t.Fatalf("ler obra_centro_custo: %v", err)
	}
	if obraNoBanco.Valid {
		t.Errorf("obra_centro_custo no banco = %+v, want NULL", obraNoBanco)
	}
}

// TestSubmeterPedidoTransferenciaHandler_PublicaEventoNoSucesso prova que um
// envio bem-sucedido publica `{"resource":"pedidos","id":<novo>,"change":"created"}`
// no canal `pedidos` — molde de TestSubmeterPedidoHandler_PublicaEventoNoSucesso.
func TestSubmeterPedidoTransferenciaHandler_PublicaEventoNoSucesso(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Transf H Evento", "transf-h-evento-usuario@empresa.com")
	produtoID, estoqueOrigemID, estoqueDestinoID := seedPedidoTransferenciaViaServico(t, db, "Canteiro Transf H Evento", 10)

	registro := realtime.NewRegistry()
	eventos, cancelar := registro.Subscribe(empresaTeste)
	defer cancelar()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /e/{slug}/api/pedidos/transferencia",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(SubmeterPedidoTransferenciaHandler(db, registro))))

	corpo := `{"obraCentroCusto":"Obra X","produtoId":"` + produtoID +
		`","estoqueOrigemId":"` + estoqueOrigemID + `","estoqueDestinoId":"` + estoqueDestinoID + `","quantidade":1}`
	r := httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos/transferencia", strings.NewReader(corpo))
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
		t.Fatal("nenhum evento publicado em 1s após SubmeterPedidoTransferenciaHandler bem-sucedido")
	}
}

// TestSubmeterPedidoTransferenciaHandler_400CamposVazios cobre o Always de
// spec-20-1 na fronteira HTTP: produtoId/estoqueOrigemId/estoqueDestinoId
// vazios -> 400 VALIDATION_ERROR.
func TestSubmeterPedidoTransferenciaHandler_400CamposVazios(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Transf H 400", "transf-h-400-usuario@empresa.com")
	produtoID, estoqueOrigemID, estoqueDestinoID := seedPedidoTransferenciaViaServico(t, db, "Canteiro Transf H 400", 10)

	casos := map[string]string{
		"produtoId vazio":        `{"obraCentroCusto":"Obra X","produtoId":"","estoqueOrigemId":"` + estoqueOrigemID + `","estoqueDestinoId":"` + estoqueDestinoID + `","quantidade":1}`,
		"estoqueOrigemId vazio":  `{"obraCentroCusto":"Obra X","produtoId":"` + produtoID + `","estoqueOrigemId":"","estoqueDestinoId":"` + estoqueDestinoID + `","quantidade":1}`,
		"estoqueDestinoId vazio": `{"obraCentroCusto":"Obra X","produtoId":"` + produtoID + `","estoqueOrigemId":"` + estoqueOrigemID + `","estoqueDestinoId":"","quantidade":1}`,
		"origem == destino":      `{"obraCentroCusto":"Obra X","produtoId":"` + produtoID + `","estoqueOrigemId":"` + estoqueOrigemID + `","estoqueDestinoId":"` + estoqueOrigemID + `","quantidade":1}`,
	}
	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			w := postPedidoTransferencia(db, "Bearer "+token, corpo)
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

// TestSubmeterPedidoTransferenciaHandler_409DisponibilidadeInsuficiente cobre
// a linha "Disponível insuficiente" na fronteira HTTP -> 409 CONFLICT.
func TestSubmeterPedidoTransferenciaHandler_409DisponibilidadeInsuficiente(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Transf H 409", "transf-h-409-usuario@empresa.com")
	produtoID, estoqueOrigemID, estoqueDestinoID := seedPedidoTransferenciaViaServico(t, db, "Canteiro Transf H 409", 3)

	corpo := `{"obraCentroCusto":"Obra X","produtoId":"` + produtoID +
		`","estoqueOrigemId":"` + estoqueOrigemID + `","estoqueDestinoId":"` + estoqueDestinoID + `","quantidade":4}`
	w := postPedidoTransferencia(db, "Bearer "+token, corpo)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

// TestSubmeterPedidoTransferenciaHandler_401SemToken cobre a ausência de
// token -> 401 (molde de TestSubmeterPedidoHandler_401SemToken).
func TestSubmeterPedidoTransferenciaHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)

	w := postPedidoTransferencia(db, "", `{"obraCentroCusto":"Obra X"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestDecidirPedidoHandler_409PedidoTipoNaoSuportado cobre o guard CRÍTICO
// de DecidirPedido (Story 20.1, Spec Change Log) na fronteira HTTP: um
// Pedido de Transferência pendente não pode ser aprovado NEM rejeitado pela
// Fila atual -> 409 CONFLICT, status permanece `pendente`.
func TestDecidirPedidoHandler_409PedidoTipoNaoSuportado(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H201 Transf TipoNaoSup Dono", "h-transf-tiponaosup-dono@empresa.com")
	criarContaComPapel(t, db, "H201 Transf TipoNaoSup Almox", "h-transf-tiponaosup-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-transf-tiponaosup-almox@empresa.com", "senha-123456")

	produtoID, estoqueOrigemID, estoqueDestinoID := seedPedidoTransferenciaViaServico(t, db, "Canteiro Transf TipoNaoSup", 10)
	pedido, err := services.SubmeterPedidoTransferencia(db, empresaTeste, dono, "Fulano", "Obra X", "", produtoID, estoqueOrigemID, estoqueDestinoID, 5)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoTransferencia: %v", err)
	}

	for _, corpo := range []string{`{"aprovar":true}`, `{"aprovar":false}`} {
		w := postDecisaoPedido(db, "Bearer "+tokenAlmox, pedido.ID, corpo)
		if w.Code != http.StatusConflict {
			t.Fatalf("corpo=%s: status = %d, want %d (body=%s)", corpo, w.Code, http.StatusConflict, w.Body.String())
		}
		if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
			t.Errorf("corpo=%s: code = %q, want CONFLICT", corpo, env.Error.Code)
		}
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM pedidos WHERE id = $1`, pedido.ID).Scan(&status); err != nil {
		t.Fatalf("reler status: %v", err)
	}
	if status != "pendente" {
		t.Errorf("status = %q, want pendente", status)
	}
}

// --- Campo `tipo` na fronteira HTTP (Story 20.1, Epic 20, revisão de ------
// --- código) ---------------------------------------------------------------
//
// Até esta rodada só havia cobertura de `tipo` a nível de serviço
// (services.PedidoResumo/PedidoDetalhe); estes testes confirmam que o JSON
// devolvido por GET /api/pedidos (escopo próprio e ?escopo=todos) e GET
// /api/pedidos/{id} realmente carrega "retirada"/"transferencia" — nunca um
// valor vazio/incorreto por falta de SELECT da coluna nova.

// TestPedidosHandlers_CampoTipoNoJSON cobre a fronteira HTTP: um Pedido de
// retirada e um de Transferência, do MESMO usuário, aparecem com o `tipo`
// correto em GET /api/pedidos, GET /api/pedidos?escopo=todos (almoxarife) e
// GET /api/pedidos/{id}.
func TestPedidosHandlers_CampoTipoNoJSON(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	usuarioID, token := seedContaComumECarrinho(t, db, "H Tipo JSON", "h-pedidos-tipo-json@empresa.com")
	criarContaComPapel(t, db, "H Tipo JSON Almox", "h-pedidos-tipo-json-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-pedidos-tipo-json-almox@empresa.com", "senha-123456")

	pedidoRetirada := seedPedidoViaServico(t, db, usuarioID, "H Tipo JSON Retirada", 1)

	produtoID, estoqueOrigemID, estoqueDestinoID := seedPedidoTransferenciaViaServico(t, db, "H Tipo JSON Transf", 10)
	pedidoTransferencia, err := services.SubmeterPedidoTransferencia(
		db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueOrigemID, estoqueDestinoID, 1,
	)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoTransferencia: %v", err)
	}

	esperado := map[string]string{
		pedidoRetirada.ID:      "retirada",
		pedidoTransferencia.ID: "transferencia",
	}

	// GET /api/pedidos (escopo próprio).
	w := getPedidos(db, "Bearer "+token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/pedidos: status = %d (body=%s)", w.Code, w.Body.String())
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
		if want, ok := esperado[p.ID]; !ok || p.Tipo != want {
			t.Errorf("GET /api/pedidos: pedido %s: Tipo = %q, want %q", p.ID, p.Tipo, esperado[p.ID])
		}
	}

	// GET /api/pedidos?escopo=todos (almoxarife — Story 7.4).
	wTodos := getPedidos(db, "Bearer "+tokenAlmox, "escopo=todos")
	if wTodos.Code != http.StatusOK {
		t.Fatalf("GET /api/pedidos?escopo=todos: status = %d (body=%s)", wTodos.Code, wTodos.Body.String())
	}
	var respTodos struct {
		Pedidos []services.PedidoResumo `json:"pedidos"`
	}
	if err := json.Unmarshal(wTodos.Body.Bytes(), &respTodos); err != nil {
		t.Fatalf("decode (todos): %v (body=%s)", err, wTodos.Body.String())
	}
	vistos := 0
	for _, p := range respTodos.Pedidos {
		if want, ok := esperado[p.ID]; ok {
			vistos++
			if p.Tipo != want {
				t.Errorf("GET /api/pedidos?escopo=todos: pedido %s: Tipo = %q, want %q", p.ID, p.Tipo, want)
			}
		}
	}
	if vistos != 2 {
		t.Errorf("GET /api/pedidos?escopo=todos: viu %d dos 2 pedidos semeados, want 2", vistos)
	}

	// GET /api/pedidos/{id} — um por tipo.
	for id, want := range esperado {
		wDetalhe := getPedido(db, "Bearer "+token, id)
		if wDetalhe.Code != http.StatusOK {
			t.Fatalf("GET /api/pedidos/%s: status = %d (body=%s)", id, wDetalhe.Code, wDetalhe.Body.String())
		}
		var respDetalhe struct {
			Pedido services.PedidoDetalhe `json:"pedido"`
		}
		if err := json.Unmarshal(wDetalhe.Body.Bytes(), &respDetalhe); err != nil {
			t.Fatalf("decode detalhe %s: %v (body=%s)", id, err, wDetalhe.Body.String())
		}
		if respDetalhe.Pedido.Tipo != want {
			t.Errorf("GET /api/pedidos/%s: Tipo = %q, want %q", id, respDetalhe.Pedido.Tipo, want)
		}
	}
}

// --- SubmeterPedidoTransferenciaHandler: ramos faltando (revisão de --------
// --- código) -----------------------------------------------------------

// TestSubmeterPedidoTransferenciaHandler_409ProdutoInativo cobre, na
// fronteira HTTP, o Always de spec-20-1 já coberto a nível de serviço por
// TestSubmeterPedidoTransferencia_ProdutoInativoRecusado: Produto inativo ->
// 409 PRODUTO_INATIVO (mesmo código do Pedido de retirada), nada gravado.
func TestSubmeterPedidoTransferenciaHandler_409ProdutoInativo(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Transf H Inativo", "transf-h-inativo-usuario@empresa.com")
	produtoID, estoqueOrigemID, estoqueDestinoID := seedPedidoTransferenciaViaServico(t, db, "Canteiro Transf H Inativo", 10)
	if _, err := db.Exec(`UPDATE produtos SET inativado_em = now() WHERE id = $1`, produtoID); err != nil {
		t.Fatalf("seed inativação: %v", err)
	}

	corpo := `{"obraCentroCusto":"Obra X","produtoId":"` + produtoID +
		`","estoqueOrigemId":"` + estoqueOrigemID + `","estoqueDestinoId":"` + estoqueDestinoID + `","quantidade":1}`
	w := postPedidoTransferencia(db, "Bearer "+token, corpo)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "PRODUTO_INATIVO" {
		t.Errorf("code = %q, want PRODUTO_INATIVO", env.Error.Code)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM pedidos WHERE tipo = 'transferencia'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("pedidos tipo=transferencia = %d (err=%v), want 0 (nada gravado)", n, err)
	}
}

// TestSubmeterPedidoTransferenciaHandler_400DestinoInexistenteOuOutraEmpresa
// cobre, na fronteira HTTP, a linha "Destino inexistente/outra Empresa" da
// I/O Matrix (já coberta a nível de serviço por
// TestSubmeterPedidoTransferencia_DestinoInexistenteOuOutraEmpresa): 400
// VALIDATION_ERROR, nunca 409 — molde dos testes de handler já existentes
// pro Pedido comum (ex. TestSubmeterPedidoHandler_400CamposAusentes).
func TestSubmeterPedidoTransferenciaHandler_400DestinoInexistenteOuOutraEmpresa(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Transf H DestRuim", "transf-h-destruim-usuario@empresa.com")
	produtoID, estoqueOrigemID, _ := seedPedidoTransferenciaViaServico(t, db, "Canteiro Transf H DestRuim", 10)

	const slugAlheia = "transf-h-destruim-alheia"
	removerEmpresaPlataformaHandlers(t, db, slugAlheia)
	t.Cleanup(func() { removerEmpresaPlataformaHandlers(t, db, slugAlheia) })
	var alheiaID string
	if err := db.QueryRow(
		`INSERT INTO empresas (nome_fantasia, razao_social, cnpj, logradouro, numero, bairro, cidade, cep, uf, slug)
		 VALUES ('Transf H DestRuim Alheia', 'Transf H DestRuim Alheia LTDA', '99888777012199', 'Rua', '1', 'Centro', 'Recife', '50000000', 'PE', $1)
		 RETURNING id`, slugAlheia,
	).Scan(&alheiaID); err != nil {
		t.Fatalf("criar empresa alheia: %v", err)
	}
	filialAlheia := filialTeste(t, db, alheiaID)
	estoqueAlheio, err := services.CriarEstoque(db, alheiaID, filialAlheia, "Estoque Alheio Transf H DestRuim")
	if err != nil {
		t.Fatalf("seed CriarEstoque (outra empresa): %v", err)
	}

	casos := map[string]string{
		"inexistente":   "00000000-0000-0000-0000-000000000000",
		"outra empresa": estoqueAlheio.ID,
	}
	for nome, estoqueDestino := range casos {
		t.Run(nome, func(t *testing.T) {
			corpo := `{"obraCentroCusto":"Obra X","produtoId":"` + produtoID +
				`","estoqueOrigemId":"` + estoqueOrigemID + `","estoqueDestinoId":"` + estoqueDestino + `","quantidade":1}`
			w := postPedidoTransferencia(db, "Bearer "+token, corpo)
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

// --- SubmeterPedidoBaixaHandler (Story 20.2, Epic 20, spec-20-2) ----------

func postPedidoBaixa(db *sql.DB, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /e/{slug}/api/pedidos/baixa",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(SubmeterPedidoBaixaHandler(db, realtime.NewRegistry()))))
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos/baixa", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos/baixa", nil)
	}
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// TestSubmeterPedidoBaixaHandler_201 prova a AC1 na fronteira HTTP: produto
// com saldo suficiente + campos obrigatórios preenchidos -> 201 com o Pedido
// `pendente` tipo=baixa. `solicitante` NUNCA é lido do corpo (requisito do
// usuário, pós-revisão) — o corpo nem o envia; `Solicitante` vem sempre de
// `usuario.Nome` da sessão.
func TestSubmeterPedidoBaixaHandler_201(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Baixa H 201", "baixa-h-201-usuario@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa H 201", 10)

	corpo := `{"obraCentroCusto":"Obra Norte","produtoId":"` + produtoID +
		`","estoqueId":"` + estoqueID + `","quantidade":5}`
	w := postPedidoBaixa(db, "Bearer "+token, corpo)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Pedido services.Pedido `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Pedido.Status != "pendente" {
		t.Errorf("Status = %q, want pendente", resp.Pedido.Status)
	}
	if resp.Pedido.Tipo != "baixa" {
		t.Errorf("Tipo = %q, want baixa", resp.Pedido.Tipo)
	}
	if resp.Pedido.Solicitante != "Usuario Baixa H 201" {
		t.Errorf("Solicitante = %q, want %q (nome da sessão autenticada)", resp.Pedido.Solicitante, "Usuario Baixa H 201")
	}

	var estoqueDestinoID sql.NullString
	if err := db.QueryRow(`SELECT estoque_destino_id FROM pedido_itens WHERE pedido_id = $1`, resp.Pedido.ID).Scan(&estoqueDestinoID); err != nil {
		t.Fatalf("ler estoque_destino_id: %v", err)
	}
	if estoqueDestinoID.Valid {
		t.Errorf("estoque_destino_id = %+v, want NULL (Pedido de Baixa nunca tem destino)", estoqueDestinoID)
	}
}

// TestSubmeterPedidoBaixaHandler_IgnoraSolicitanteDoCorpo prova que um
// cliente mal-intencionado (ou um client antigo que ainda envie
// `solicitante` no corpo) NUNCA consegue gravar um nome diferente do usuário
// autenticado — o campo nem existe mais em `submeterPedidoBaixaRequest`, o
// decode simplesmente ignora a chave extra.
func TestSubmeterPedidoBaixaHandler_IgnoraSolicitanteDoCorpo(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Baixa H IgnoraSolic", "baixa-h-ignorasolic@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa H IgnoraSolic", 10)

	corpo := `{"solicitante":"Nome Forjado","obraCentroCusto":"Obra X","produtoId":"` + produtoID +
		`","estoqueId":"` + estoqueID + `","quantidade":1}`
	w := postPedidoBaixa(db, "Bearer "+token, corpo)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Pedido services.Pedido `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Pedido.Solicitante != "Usuario Baixa H IgnoraSolic" {
		t.Errorf("Solicitante = %q, want %q (nunca \"Nome Forjado\")", resp.Pedido.Solicitante, "Usuario Baixa H IgnoraSolic")
	}
}

// TestSubmeterPedidoBaixaHandler_201_ObraCentroCustoAusente prova o
// requisito do usuário (pós-revisão): "obraCentroCusto" ausente/vazio nunca
// bloqueia o envio -> 201, `ObraCentroCusto` volta "" (NULL no banco).
func TestSubmeterPedidoBaixaHandler_201_ObraCentroCustoAusente(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Baixa H SemObra", "baixa-h-semobra@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa H SemObra", 10)

	corpo := `{"produtoId":"` + produtoID + `","estoqueId":"` + estoqueID + `","quantidade":1}`
	w := postPedidoBaixa(db, "Bearer "+token, corpo)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Pedido services.Pedido `json:"pedido"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Pedido.ObraCentroCusto != "" {
		t.Errorf("ObraCentroCusto = %q, want \"\"", resp.Pedido.ObraCentroCusto)
	}

	var obraNoBanco sql.NullString
	if err := db.QueryRow(`SELECT obra_centro_custo FROM pedidos WHERE id = $1`, resp.Pedido.ID).Scan(&obraNoBanco); err != nil {
		t.Fatalf("ler obra_centro_custo: %v", err)
	}
	if obraNoBanco.Valid {
		t.Errorf("obra_centro_custo no banco = %+v, want NULL", obraNoBanco)
	}
}

// TestSubmeterPedidoBaixaHandler_PublicaEventoNoSucesso prova que um envio
// bem-sucedido publica `{"resource":"pedidos","id":<novo>,"change":"created"}`
// no canal `pedidos` — molde de TestSubmeterPedidoTransferenciaHandler_PublicaEventoNoSucesso.
func TestSubmeterPedidoBaixaHandler_PublicaEventoNoSucesso(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Baixa H Evento", "baixa-h-evento-usuario@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa H Evento", 10)

	registro := realtime.NewRegistry()
	eventos, cancelar := registro.Subscribe(empresaTeste)
	defer cancelar()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /e/{slug}/api/pedidos/baixa",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(SubmeterPedidoBaixaHandler(db, registro))))

	corpo := `{"obraCentroCusto":"Obra X","produtoId":"` + produtoID +
		`","estoqueId":"` + estoqueID + `","quantidade":1}`
	r := httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos/baixa", strings.NewReader(corpo))
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
		t.Fatal("nenhum evento publicado em 1s após SubmeterPedidoBaixaHandler bem-sucedido")
	}
}

// TestSubmeterPedidoBaixaHandler_400CamposVazios cobre o Always de
// spec-20-2 na fronteira HTTP: produtoId/estoqueId vazios -> 400
// VALIDATION_ERROR.
func TestSubmeterPedidoBaixaHandler_400CamposVazios(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Baixa H 400", "baixa-h-400-usuario@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa H 400", 10)

	casos := map[string]string{
		"produtoId vazio": `{"obraCentroCusto":"Obra X","produtoId":"","estoqueId":"` + estoqueID + `","quantidade":1}`,
		"estoqueId vazio": `{"obraCentroCusto":"Obra X","produtoId":"` + produtoID + `","estoqueId":"","quantidade":1}`,
	}
	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			w := postPedidoBaixa(db, "Bearer "+token, corpo)
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

// TestSubmeterPedidoBaixaHandler_400PayloadInvalido prova que um corpo não
// decodificável -> 400 VALIDATION_ERROR (único ramo de erro do handler ainda
// sem teste — molde de TestSubmeterPedidoHandler_400PayloadInvalido).
func TestSubmeterPedidoBaixaHandler_400PayloadInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Baixa H Payload", "baixa-h-payload-usuario@empresa.com")

	w := postPedidoBaixa(db, "Bearer "+token, `{"produtoId":`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// TestSubmeterPedidoBaixaHandler_409DisponibilidadeInsuficiente cobre a
// linha "Disponível insuficiente" na fronteira HTTP -> 409 CONFLICT.
func TestSubmeterPedidoBaixaHandler_409DisponibilidadeInsuficiente(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Baixa H 409", "baixa-h-409-usuario@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa H 409", 3)

	corpo := `{"obraCentroCusto":"Obra X","produtoId":"` + produtoID +
		`","estoqueId":"` + estoqueID + `","quantidade":4}`
	w := postPedidoBaixa(db, "Bearer "+token, corpo)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

// TestSubmeterPedidoBaixaHandler_409ProdutoInativo cobre, na fronteira HTTP,
// o Always de spec-20-2: Produto inativo -> 409 PRODUTO_INATIVO (mesmo
// código do Pedido de retirada/Transferência), nada gravado.
func TestSubmeterPedidoBaixaHandler_409ProdutoInativo(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Baixa H Inativo", "baixa-h-inativo-usuario@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa H Inativo", 10)
	if _, err := db.Exec(`UPDATE produtos SET inativado_em = now() WHERE id = $1`, produtoID); err != nil {
		t.Fatalf("seed inativação: %v", err)
	}

	corpo := `{"obraCentroCusto":"Obra X","produtoId":"` + produtoID +
		`","estoqueId":"` + estoqueID + `","quantidade":1}`
	w := postPedidoBaixa(db, "Bearer "+token, corpo)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "PRODUTO_INATIVO" {
		t.Errorf("code = %q, want PRODUTO_INATIVO", env.Error.Code)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM pedidos WHERE tipo = 'baixa'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("pedidos tipo=baixa = %d (err=%v), want 0 (nada gravado)", n, err)
	}
}

// TestSubmeterPedidoBaixaHandler_401SemToken cobre a ausência de token ->
// 401 (molde de TestSubmeterPedidoTransferenciaHandler_401SemToken).
func TestSubmeterPedidoBaixaHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)

	w := postPedidoBaixa(db, "", `{"obraCentroCusto":"Obra X"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestDecidirPedidoHandler_409PedidoDeBaixaTipoNaoSuportado cobre, na
// fronteira HTTP, que o guard genérico de DecidirPedido (`tipo !=
// 'retirada'`) já cobre `'baixa'` sem nenhuma mudança: 409 CONFLICT para
// aprovar OU rejeitar, status permanece `pendente`.
func TestDecidirPedidoHandler_409PedidoDeBaixaTipoNaoSuportado(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H201 Baixa TipoNaoSup Dono", "h-baixa-tiponaosup-dono@empresa.com")
	criarContaComPapel(t, db, "H201 Baixa TipoNaoSup Almox", "h-baixa-tiponaosup-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-baixa-tiponaosup-almox@empresa.com", "senha-123456")

	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Baixa TipoNaoSup", 10)
	pedido, err := services.SubmeterPedidoBaixa(db, empresaTeste, dono, "Fulano", "Obra X", "", produtoID, estoqueID, 5)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}

	for _, corpo := range []string{`{"aprovar":true}`, `{"aprovar":false}`} {
		w := postDecisaoPedido(db, "Bearer "+tokenAlmox, pedido.ID, corpo)
		if w.Code != http.StatusConflict {
			t.Fatalf("corpo=%s: status = %d, want %d (body=%s)", corpo, w.Code, http.StatusConflict, w.Body.String())
		}
		if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
			t.Errorf("corpo=%s: code = %q, want CONFLICT", corpo, env.Error.Code)
		}
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM pedidos WHERE id = $1`, pedido.ID).Scan(&status); err != nil {
		t.Fatalf("reler status: %v", err)
	}
	if status != "pendente" {
		t.Errorf("status = %q, want pendente", status)
	}
}

// TestPedidosHandlers_CampoTipoNoJSON_Baixa cobre a fronteira HTTP para o
// campo `tipo` de um Pedido de Baixa — molde de TestPedidosHandlers_CampoTipoNoJSON
// (que já cobre retirada/transferencia), confirmando que não falta nenhum
// SELECT de coluna nova (lacuna da revisão da Story 20.1 — não repetir aqui).
func TestPedidosHandlers_CampoTipoNoJSON_Baixa(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	usuarioID, token := seedContaComumECarrinho(t, db, "H Tipo JSON Baixa", "h-pedidos-tipo-json-baixa@empresa.com")

	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "H Tipo JSON Baixa", 10)
	pedidoBaixa, err := services.SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 1)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}

	w := getPedidos(db, "Bearer "+token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/pedidos: status = %d (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedidos []services.PedidoResumo `json:"pedidos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	achou := false
	for _, p := range resp.Pedidos {
		if p.ID == pedidoBaixa.ID {
			achou = true
			if p.Tipo != "baixa" {
				t.Errorf("GET /api/pedidos: Tipo = %q, want baixa", p.Tipo)
			}
		}
	}
	if !achou {
		t.Fatalf("Pedido de Baixa semeado não apareceu em GET /api/pedidos")
	}

	wDetalhe := getPedido(db, "Bearer "+token, pedidoBaixa.ID)
	if wDetalhe.Code != http.StatusOK {
		t.Fatalf("GET /api/pedidos/%s: status = %d (body=%s)", pedidoBaixa.ID, wDetalhe.Code, wDetalhe.Body.String())
	}
	var respDetalhe struct {
		Pedido services.PedidoDetalhe `json:"pedido"`
	}
	if err := json.Unmarshal(wDetalhe.Body.Bytes(), &respDetalhe); err != nil {
		t.Fatalf("decode detalhe: %v (body=%s)", err, wDetalhe.Body.String())
	}
	if respDetalhe.Pedido.Tipo != "baixa" {
		t.Errorf("GET /api/pedidos/%s: Tipo = %q, want baixa", pedidoBaixa.ID, respDetalhe.Pedido.Tipo)
	}
}

// --- DecidirLotePedidosHandler (Story 20.3, Epic 20, spec-20-3) -----------
//
// POST /api/pedidos/decisao-lote -> RequireAuth -> RequireRole(gestor) ->
// handler. Molde de postDecisaoPedido acima, trocando RequireRole(almoxarife)
// por RequireRole(gestor) — nível de acesso MAIOR (requisito explícito do
// usuário: "o gerente tem que aprovar").

func postDecisaoLotePedidos(db *sql.DB, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /e/{slug}/api/pedidos/decisao-lote",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(
				middleware.RequireRole(services.PapelGestor)(
					DecidirLotePedidosHandler(db, realtime.NewRegistry())))))
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos/decisao-lote", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos/decisao-lote", nil)
	}
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// TestDecidirLotePedidosHandler_200LoteMisto cobre a linha "Lote válido
// misto" na fronteira HTTP: um Gestor aprova 1 Transferência + 1 Baixa numa
// chamada só -> 200, os dois `aprovado`.
func TestDecidirLotePedidosHandler_200LoteMisto(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "H203 Lote Gestor", "h-lote-gestor@empresa.com", "senha-123456", "gestor")
	tokenGestor := tokenDeLogin(t, db, "h-lote-gestor@empresa.com", "senha-123456")
	dono, _ := seedContaComumECarrinho(t, db, "H203 Lote Dono", "h-lote-dono@empresa.com")

	produtoTransfID, estoqueOrigemID, estoqueDestinoID := seedPedidoTransferenciaViaServico(t, db, "H203 Lote Transf", 10)
	pedidoTransf, err := services.SubmeterPedidoTransferencia(db, empresaTeste, dono, "Fulano", "Obra X", "", produtoTransfID, estoqueOrigemID, estoqueDestinoID, 5)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoTransferencia: %v", err)
	}
	produtoBaixaID, estoqueBaixaID := seedProdutoComSaldoHandler(t, db, "H203 Lote Baixa", 10)
	pedidoBaixa, err := services.SubmeterPedidoBaixa(db, empresaTeste, dono, "Fulano", "Obra Y", "", produtoBaixaID, estoqueBaixaID, 3)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}

	body := `{"pedidoIds":["` + pedidoTransf.ID + `","` + pedidoBaixa.ID + `"],"aprovar":true}`
	w := postDecisaoLotePedidos(db, "Bearer "+tokenGestor, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Pedidos []services.PedidoDetalhe `json:"pedidos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Pedidos) != 2 {
		t.Fatalf("pedidos = %+v, want 2", resp.Pedidos)
	}
	for _, p := range resp.Pedidos {
		if p.Status != "aprovado" {
			t.Errorf("pedido %s: status = %q, want aprovado", p.ID, p.Status)
		}
	}
}

// TestDecidirLotePedidosHandler_400ListaVazia cobre a linha "Lote vazio" na
// fronteira HTTP -> 400 VALIDATION_ERROR.
func TestDecidirLotePedidosHandler_400ListaVazia(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "H203 Vazio Gestor", "h-lote-vazio-gestor@empresa.com", "senha-123456", "gestor")
	tokenGestor := tokenDeLogin(t, db, "h-lote-vazio-gestor@empresa.com", "senha-123456")

	w := postDecisaoLotePedidos(db, "Bearer "+tokenGestor, `{"pedidoIds":[],"aprovar":true}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// TestDecidirLotePedidosHandler_400RetiradaMisturada cobre a linha "Lote com
// retirada misturado" na fronteira HTTP -> 400 VALIDATION_ERROR, nada
// gravado.
func TestDecidirLotePedidosHandler_400RetiradaMisturada(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "H203 Misto Gestor", "h-lote-misto-gestor@empresa.com", "senha-123456", "gestor")
	tokenGestor := tokenDeLogin(t, db, "h-lote-misto-gestor@empresa.com", "senha-123456")
	dono, _ := seedContaComumECarrinho(t, db, "H203 Misto Dono", "h-lote-misto-dono@empresa.com")

	pedidoRetirada := seedPedidoViaServico(t, db, dono, "H203 Misto Retirada", 1)
	produtoBaixaID, estoqueBaixaID := seedProdutoComSaldoHandler(t, db, "H203 Misto Baixa", 10)
	pedidoBaixa, err := services.SubmeterPedidoBaixa(db, empresaTeste, dono, "Fulano", "Obra Y", "", produtoBaixaID, estoqueBaixaID, 3)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}

	body := `{"pedidoIds":["` + pedidoBaixa.ID + `","` + pedidoRetirada.ID + `"],"aprovar":true}`
	w := postDecisaoLotePedidos(db, "Bearer "+tokenGestor, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM pedidos WHERE id = $1`, pedidoBaixa.ID).Scan(&status); err != nil {
		t.Fatalf("reler status: %v", err)
	}
	if status != "pendente" {
		t.Errorf("status do pedido de baixa = %q, want pendente (lote inteiro rejeitado)", status)
	}
}

// TestDecidirLotePedidosHandler_403PapelAlmoxarife cobre a linha "Papel
// abaixo de gestor" na fronteira HTTP: `almoxarife` — que decide Pedidos de
// retirada por POST /api/pedidos/{id}/decisao — NÃO alcança o nível
// MAIOR exigido por esta rota -> 403, nada gravado.
func TestDecidirLotePedidosHandler_403PapelAlmoxarife(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, _ := seedContaComumECarrinho(t, db, "H203 Papel Almox Dono", "h-lote-papel-almox-dono@empresa.com")
	criarContaComPapel(t, db, "H203 Papel Almox", "h-lote-papel-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "h-lote-papel-almox@empresa.com", "senha-123456")

	produtoBaixaID, estoqueBaixaID := seedProdutoComSaldoHandler(t, db, "H203 Papel Almox Baixa", 10)
	pedidoBaixa, err := services.SubmeterPedidoBaixa(db, empresaTeste, dono, "Fulano", "Obra Y", "", produtoBaixaID, estoqueBaixaID, 3)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}

	w := postDecisaoLotePedidos(db, "Bearer "+tokenAlmox, `{"pedidoIds":["`+pedidoBaixa.ID+`"],"aprovar":true}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM pedidos WHERE id = $1`, pedidoBaixa.ID).Scan(&status); err != nil {
		t.Fatalf("reler status: %v", err)
	}
	if status != "pendente" {
		t.Errorf("status = %q, want pendente (nenhuma escrita antes do service)", status)
	}
}

// TestDecidirLotePedidosHandler_PublicaEventoNoSucesso cobre o achado de
// revisão (findings do triplo-review pós-story): nenhum teste existente de
// DecidirLotePedidosHandler assinava o registry pra provar os eventos
// realtime publicados — molde EXATO de
// TestDecidirPedidoHandler_PublicaEventoNoSucesso, mas com um lote MISTO (2
// decididos agora + 1 já "pulado" por já estar decidido antes da chamada):
// publica 1 evento `pedidos` por Pedido da RESPOSTA (inclusive o pulado, de
// propósito — ver comentário do handler) + 1 evento `produtos` por Produto
// ÚNICO entre todos os itens (dedup: o Pedido pulado reaproveita o MESMO
// Produto da Transferência, então só 2 eventos `produtos` no total, não 3).
func TestDecidirLotePedidosHandler_PublicaEventoNoSucesso(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "H203 Evento Gestor", "h-lote-evento-gestor@empresa.com", "senha-123456", "gestor")
	tokenGestor := tokenDeLogin(t, db, "h-lote-evento-gestor@empresa.com", "senha-123456")
	dono, _ := seedContaComumECarrinho(t, db, "H203 Evento Dono", "h-lote-evento-dono@empresa.com")

	// Transferência pendente, decidida agora -> aprovado.
	produtoTransfID, estoqueOrigemID, estoqueDestinoID := seedPedidoTransferenciaViaServico(t, db, "H203 Evento Transf", 10)
	pedidoTransf, err := services.SubmeterPedidoTransferencia(db, empresaTeste, dono, "Fulano", "Obra X", "", produtoTransfID, estoqueOrigemID, estoqueDestinoID, 5)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoTransferencia: %v", err)
	}

	// Baixa pendente, decidida agora -> aprovado (Produto DIFERENTE do de
	// cima — prova que o dedup de eventos `produtos` não colapsa produtos
	// distintos).
	produtoBaixaID, estoqueBaixaID := seedProdutoComSaldoHandler(t, db, "H203 Evento Baixa", 10)
	pedidoBaixa, err := services.SubmeterPedidoBaixa(db, empresaTeste, dono, "Fulano", "Obra Y", "", produtoBaixaID, estoqueBaixaID, 3)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}

	// Pedido "pulado" — já decidido ANTES desta chamada (simulado por
	// UPDATE direto, mesmo molde de
	// TestDecidirLotePedidos_PedidoJaDecidido_PuladoSemAbortarLote),
	// reaproveitando o MESMO Produto/Estoque da Transferência acima (origem)
	// para testar o dedup de eventos `produtos`.
	pedidoPulado, err := services.SubmeterPedidoBaixa(db, empresaTeste, dono, "Fulano", "Obra Z", "", produtoTransfID, estoqueOrigemID, 2)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa (pulado): %v", err)
	}
	if _, err := db.Exec(`UPDATE pedidos SET status = 'rejeitado', decidido_por = $2 WHERE id = $1`, pedidoPulado.ID, dono); err != nil {
		t.Fatalf("seed: marcar pedidoPulado como já decidido: %v", err)
	}

	registro := realtime.NewRegistry()
	eventos, cancelar := registro.Subscribe(empresaTeste)
	defer cancelar()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /e/{slug}/api/pedidos/decisao-lote",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(
				middleware.RequireRole(services.PapelGestor)(
					DecidirLotePedidosHandler(db, registro)))))

	body := `{"pedidoIds":["` + pedidoTransf.ID + `","` + pedidoBaixa.ID + `","` + pedidoPulado.ID + `"],"aprovar":true}`
	r := httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos/decisao-lote", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+tokenGestor)
	w := httptest.NewRecorder()

	// 3 eventos `pedidos` (um por Pedido da resposta, inclusive o pulado) +
	// 2 eventos `produtos` (dedup: produtoTransfID aparece 2x na resposta —
	// Transferência e o Pedido pulado — mas só 1 evento) = 5 no total, MAIS
	// do que o buffer de 4 por assinante (realtime.eventoBufferSize) — o
	// handler publica tudo de uma vez, síncrono, ANTES de devolver a
	// resposta; se o teste só lesse o canal DEPOIS de `mux.ServeHTTP`
	// retornar, o 5º Publish (não-bloqueante) já teria sido descartado por
	// buffer cheio (mesma semântica de "assinante lento" do Registry, nunca
	// um bug do handler). Por isso o `ServeHTTP` roda numa goroutine própria
	// e o dreno do canal começa EM PARALELO — o mesmo padrão que o
	// consumidor real (StreamRealtimeHandler) segue.
	const totalEsperado = 5
	recebidos := make([]struct {
		Resource string
		ID       string
		Change   string
	}, 0, totalEsperado)

	servidoCh := make(chan struct{})
	go func() {
		defer close(servidoCh)
		mux.ServeHTTP(w, r)
	}()

	for len(recebidos) < totalEsperado {
		select {
		case ev := <-eventos:
			recebidos = append(recebidos, struct {
				Resource string
				ID       string
				Change   string
			}{ev.Resource, ev.ID, ev.Change})
		case <-time.After(time.Second):
			t.Fatalf("recebeu só %d de %d eventos esperados em 1s: %+v", len(recebidos), totalEsperado, recebidos)
		}
	}
	<-servidoCh
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	eventosPedidos := map[string]string{}
	eventosProdutos := map[string]bool{}
	for _, ev := range recebidos {
		switch ev.Resource {
		case "pedidos":
			eventosPedidos[ev.ID] = ev.Change
		case "produtos":
			eventosProdutos[ev.ID] = true
		default:
			t.Errorf("evento de resource inesperado: %+v", ev)
		}
	}

	if got, want := eventosPedidos[pedidoTransf.ID], "aprovado"; got != want {
		t.Errorf("evento pedidos[%s] = %q, want %q", pedidoTransf.ID, got, want)
	}
	if got, want := eventosPedidos[pedidoBaixa.ID], "aprovado"; got != want {
		t.Errorf("evento pedidos[%s] = %q, want %q", pedidoBaixa.ID, got, want)
	}
	if got, want := eventosPedidos[pedidoPulado.ID], "rejeitado"; got != want {
		t.Errorf("evento pedidos[%s] (pulado) = %q, want %q (status já gravado antes da chamada, reportado)", pedidoPulado.ID, got, want)
	}
	if len(eventosPedidos) != 3 {
		t.Errorf("eventos pedidos = %+v, want 3 entradas", eventosPedidos)
	}

	if !eventosProdutos[produtoTransfID] {
		t.Errorf("faltou evento produtos[%s] (Transferência + pulado, dedup)", produtoTransfID)
	}
	if !eventosProdutos[produtoBaixaID] {
		t.Errorf("faltou evento produtos[%s] (Baixa)", produtoBaixaID)
	}
	if len(eventosProdutos) != 2 {
		t.Errorf("eventos produtos = %+v, want 2 entradas (dedup de produtoTransfID repetido)", eventosProdutos)
	}
}

// TestDecidirLotePedidosHandler_400PayloadInvalido cobre `aprovar` ausente/
// nulo -> 400 VALIDATION_ERROR (nunca uma rejeição silenciosa do lote
// inteiro) — mesmo molde de decisaoPedidoRequest.
func TestDecidirLotePedidosHandler_400PayloadInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "H203 Payload Gestor", "h-lote-payload-gestor@empresa.com", "senha-123456", "gestor")
	tokenGestor := tokenDeLogin(t, db, "h-lote-payload-gestor@empresa.com", "senha-123456")

	for _, body := range []string{`{}`, `{"pedidoIds":["x"],"aprovar":null}`, `not-json`} {
		w := postDecisaoLotePedidos(db, "Bearer "+tokenGestor, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body=%s: status = %d, want 400 (body=%s)", body, w.Code, w.Body.String())
		}
	}
}

// TestDecidirLotePedidosHandler_400MensagensDistintas cobre o achado de
// revisão (findings do triplo-review pós-story): JSON malformado e `aprovar`
// ausente/nulo são causas DISTINTAS e devem devolver mensagens DISTINTAS no
// 400 — nunca o mesmo "payload inválido" genérico para as duas, que
// impediria quem consome a API de diferenciá-las.
func TestDecidirLotePedidosHandler_400MensagensDistintas(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "H203 Mensagens Gestor", "h-lote-mensagens-gestor@empresa.com", "senha-123456", "gestor")
	tokenGestor := tokenDeLogin(t, db, "h-lote-mensagens-gestor@empresa.com", "senha-123456")

	wMalformado := postDecisaoLotePedidos(db, "Bearer "+tokenGestor, `not-json`)
	if wMalformado.Code != http.StatusBadRequest {
		t.Fatalf("JSON malformado: status = %d, want 400 (body=%s)", wMalformado.Code, wMalformado.Body.String())
	}
	envMalformado := decodeErro(t, wMalformado.Body.Bytes())

	wSemAprovar := postDecisaoLotePedidos(db, "Bearer "+tokenGestor, `{"pedidoIds":["x"]}`)
	if wSemAprovar.Code != http.StatusBadRequest {
		t.Fatalf("aprovar ausente: status = %d, want 400 (body=%s)", wSemAprovar.Code, wSemAprovar.Body.String())
	}
	envSemAprovar := decodeErro(t, wSemAprovar.Body.Bytes())

	if envMalformado.Error.Message == envSemAprovar.Error.Message {
		t.Errorf("as duas causas devolveram a MESMA mensagem (%q) — devem ser distintas", envMalformado.Error.Message)
	}
}
