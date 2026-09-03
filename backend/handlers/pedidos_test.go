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
