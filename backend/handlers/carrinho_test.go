package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// --- despacho pela MESMA composição de newMux (main.go) --------------------
//
// POST /api/carrinho/itens, GET /api/carrinho, DELETE
// /api/carrinho/itens/{produtoId}/{estoqueId} -> RequireAuth -> handler,
// SEM RequireRole (Story 7.1, spec-7-1: qualquer conta autenticada monta
// seu próprio carrinho).

func postItemCarrinho(db *sql.DB, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/carrinho/itens",
		middleware.RequireAuth(db, testJWTSecret)(AdicionarItemCarrinhoHandler(db)))
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, "/api/carrinho/itens", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, "/api/carrinho/itens", nil)
	}
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func getCarrinho(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/carrinho",
		middleware.RequireAuth(db, testJWTSecret)(ListarCarrinhoHandler(db)))
	r := httptest.NewRequest(http.MethodGet, "/api/carrinho", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func deleteItemCarrinho(db *sql.DB, authHeader, produtoID, estoqueID string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/carrinho/itens/{produtoId}/{estoqueId}",
		middleware.RequireAuth(db, testJWTSecret)(RemoverItemCarrinhoHandler(db)))
	r := httptest.NewRequest(http.MethodDelete, "/api/carrinho/itens/"+produtoID+"/"+estoqueID, nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// seedContaComumECarrinho cria uma conta `usuario` comum e devolve
// (usuarioID, token) — molde mínimo reaproveitado por toda a suíte de
// Carrinho (nenhum teste precisa de papel além do mínimo, ao contrário de
// Baixa/Transferência).
func seedContaComumECarrinho(t *testing.T, db *sql.DB, nome, email string) (usuarioID, token string) {
	t.Helper()
	usuarioID = criarContaComPapel(t, db, nome, email, "senha-123456", "usuario")
	token = tokenDeLogin(t, db, email, "senha-123456")
	return usuarioID, token
}

// --- AdicionarItemCarrinhoHandler ------------------------------------------

// TestAdicionarItemCarrinhoHandler_201 prova a AC1: adição válida por
// qualquer conta autenticada (`usuario`, sem RequireRole) devolve 201 com o
// item gravado.
func TestAdicionarItemCarrinhoHandler_201(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Carrinho 201", "carrinho-201-usuario@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Carrinho 201", 10)

	corpo := `{"produtoId":"` + produtoID + `","estoqueId":"` + estoqueID + `","quantidade":4}`
	w := postItemCarrinho(db, "Bearer "+token, corpo)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Item services.ItemCarrinho `json:"item"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Item.ProdutoID != produtoID || resp.Item.EstoqueID != estoqueID {
		t.Errorf("item ids = (%q,%q), want (%q,%q)", resp.Item.ProdutoID, resp.Item.EstoqueID, produtoID, estoqueID)
	}
	if resp.Item.Quantidade != 4 {
		t.Errorf("Quantidade = %v, want 4", resp.Item.Quantidade)
	}
}

// TestAdicionarItemCarrinhoHandler_IgnoraUsuarioIdDoCorpo prova o Always de
// spec-7-1: mesmo que o corpo tente forçar um usuarioId (campo que o
// handler nem decodifica), o item é sempre gravado no carrinho do usuário
// AUTENTICADO da sessão, nunca em outro.
func TestAdicionarItemCarrinhoHandler_IgnoraUsuarioIdDoCorpo(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	usuarioID, token := seedContaComumECarrinho(t, db, "Usuario Carrinho Escopo", "carrinho-escopo-usuario@empresa.com")
	outroID := criarContaComPapel(t, db, "Usuario Carrinho Alvo Falso", "carrinho-alvo-falso@empresa.com", "senha-123456", "usuario")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Carrinho Escopo", 10)

	corpo := `{"produtoId":"` + produtoID + `","estoqueId":"` + estoqueID + `","quantidade":2,"usuarioId":"` + outroID + `"}`
	w := postItemCarrinho(db, "Bearer "+token, corpo)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}

	var quantidadeDono, quantidadeOutro float64
	errDono := db.QueryRow(
		`SELECT quantidade FROM carrinho_itens WHERE usuario_id = $1 AND produto_id = $2 AND estoque_id = $3`,
		usuarioID, produtoID, estoqueID,
	).Scan(&quantidadeDono)
	if errDono != nil {
		t.Fatalf("item deveria ter sido gravado no usuário autenticado: %v", errDono)
	}
	if quantidadeDono != 2 {
		t.Errorf("quantidade do dono = %v, want 2", quantidadeDono)
	}
	errOutro := db.QueryRow(
		`SELECT quantidade FROM carrinho_itens WHERE usuario_id = $1 AND produto_id = $2 AND estoque_id = $3`,
		outroID, produtoID, estoqueID,
	).Scan(&quantidadeOutro)
	if !errors.Is(errOutro, sql.ErrNoRows) {
		t.Errorf("nenhuma linha deveria ter sido gravada para o usuarioId do corpo, err=%v", errOutro)
	}
}

// TestAdicionarItemCarrinhoHandler_400QuantidadeInvalida prova a linha
// "quantidade inválida" -> 400 VALIDATION_ERROR.
func TestAdicionarItemCarrinhoHandler_400QuantidadeInvalida(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Carrinho 400", "carrinho-400-usuario@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Carrinho 400", 10)

	for _, corpo := range []string{
		`{"produtoId":"` + produtoID + `","estoqueId":"` + estoqueID + `","quantidade":0}`,
		`{"produtoId":"` + produtoID + `","estoqueId":"` + estoqueID + `","quantidade":-3}`,
	} {
		w := postItemCarrinho(db, "Bearer "+token, corpo)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("corpo=%s: status = %d, want %d (body=%s)", corpo, w.Code, http.StatusBadRequest, w.Body.String())
		}
		env := decodeErro(t, w.Body.Bytes())
		if env.Error.Code != "VALIDATION_ERROR" {
			t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
		}
	}
}

// TestAdicionarItemCarrinhoHandler_400PayloadInvalido prova que um corpo não
// decodificável -> 400 VALIDATION_ERROR.
func TestAdicionarItemCarrinhoHandler_400PayloadInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Carrinho Payload", "carrinho-payload-usuario@empresa.com")

	w := postItemCarrinho(db, "Bearer "+token, `{"quantidade":`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// TestAdicionarItemCarrinhoHandler_404ProdutoNaoEncontrado prova a linha
// "Produto inexistente/mesclado" -> 404 NOT_FOUND.
func TestAdicionarItemCarrinhoHandler_404ProdutoNaoEncontrado(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Carrinho 404", "carrinho-404-usuario@empresa.com")

	corpo := `{"produtoId":"00000000-0000-0000-0000-000000000000","estoqueId":"00000000-0000-0000-0000-000000000000","quantidade":1}`
	w := postItemCarrinho(db, "Bearer "+token, corpo)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
	}
}

// TestAdicionarItemCarrinhoHandler_409QuantidadeIndisponivel prova a linha
// "Disponibilidade insuficiente" -> 409 CONFLICT.
func TestAdicionarItemCarrinhoHandler_409QuantidadeIndisponivel(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Carrinho 409", "carrinho-409-usuario@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Carrinho 409", 2.5)

	corpo := `{"produtoId":"` + produtoID + `","estoqueId":"` + estoqueID + `","quantidade":100}`
	w := postItemCarrinho(db, "Bearer "+token, corpo)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

// TestAdicionarItemCarrinhoHandler_401SemToken prova que uma requisição sem
// Authorization -> 401.
func TestAdicionarItemCarrinhoHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Carrinho 401", 10)

	corpo := `{"produtoId":"` + produtoID + `","estoqueId":"` + estoqueID + `","quantidade":1}`
	w := postItemCarrinho(db, "", corpo)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// --- ListarCarrinhoHandler --------------------------------------------------

// TestListarCarrinhoHandler_200ItensERemovidos prova a linha "Abrir carrinho
// com item obsoleto": um item ativo e um item de Produto mesclado convivem
// no carrinho; a leitura devolve o ativo em "itens" e o obsoleto em
// "removidos" com o motivo, limpando a linha obsoleta do banco.
func TestListarCarrinhoHandler_200ItensERemovidos(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Carrinho Listar", "carrinho-listar-usuario@empresa.com")

	produtoAtivoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Carrinho Listar Ativo", 10)
	w := postItemCarrinho(db, "Bearer "+token, `{"produtoId":"`+produtoAtivoID+`","estoqueId":"`+estoqueID+`","quantidade":3}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed item ativo: status = %d (body=%s)", w.Code, w.Body.String())
	}

	produtoMescladoID, estoqueID2 := seedProdutoComSaldoHandler(t, db, "Canteiro Carrinho Listar Mesclado", 10)
	w = postItemCarrinho(db, "Bearer "+token, `{"produtoId":"`+produtoMescladoID+`","estoqueId":"`+estoqueID2+`","quantidade":1}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed item obsoleto: status = %d (body=%s)", w.Code, w.Body.String())
	}
	if _, err := db.Exec(`UPDATE produtos SET deleted_at = now() WHERE id = $1`, produtoMescladoID); err != nil {
		t.Fatalf("seed soft-delete: %v", err)
	}

	w = getCarrinho(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Itens     []services.ItemCarrinho         `json:"itens"`
		Removidos []services.ItemCarrinhoRemovido `json:"removidos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Itens) != 1 || resp.Itens[0].ProdutoID != produtoAtivoID {
		t.Fatalf("Itens = %+v, want 1 item de %q", resp.Itens, produtoAtivoID)
	}
	if len(resp.Removidos) != 1 || resp.Removidos[0].ProdutoID != produtoMescladoID {
		t.Fatalf("Removidos = %+v, want 1 item de %q", resp.Removidos, produtoMescladoID)
	}
	if resp.Removidos[0].Motivo != services.MotivoCarrinhoProdutoRemovido {
		t.Errorf("Motivo = %q, want %q", resp.Removidos[0].Motivo, services.MotivoCarrinhoProdutoRemovido)
	}
}

// TestListarCarrinhoHandler_200CarrinhoVazio prova a linha "Carrinho vazio":
// 200 {"itens":[],"removidos":[]}, nunca 404.
func TestListarCarrinhoHandler_200CarrinhoVazio(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Carrinho Vazio", "carrinho-vazio-usuario@empresa.com")

	w := getCarrinho(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if body := strings.TrimSpace(w.Body.String()); body != `{"itens":[],"removidos":[]}` {
		t.Errorf("body = %s, want {\"itens\":[],\"removidos\":[]}", body)
	}
}

// TestListarCarrinhoHandler_EscopadoPorUsuario prova o Always de spec-7-1: o
// carrinho de um usuário nunca aparece na listagem de outro.
func TestListarCarrinhoHandler_EscopadoPorUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, tokenDono := seedContaComumECarrinho(t, db, "Usuario Carrinho Dono Listar", "carrinho-dono-listar@empresa.com")
	_, tokenOutro := seedContaComumECarrinho(t, db, "Usuario Carrinho Outro Listar", "carrinho-outro-listar@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Carrinho Escopo Listar", 10)

	w := postItemCarrinho(db, "Bearer "+tokenDono, `{"produtoId":"`+produtoID+`","estoqueId":"`+estoqueID+`","quantidade":2}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed: status = %d (body=%s)", w.Code, w.Body.String())
	}

	w = getCarrinho(db, "Bearer "+tokenOutro)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if body := strings.TrimSpace(w.Body.String()); body != `{"itens":[],"removidos":[]}` {
		t.Errorf("carrinho do outro usuário deveria estar vazio, got %s", body)
	}
}

// TestListarCarrinhoHandler_401SemToken prova que uma requisição sem
// Authorization -> 401.
func TestListarCarrinhoHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)

	w := getCarrinho(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// --- RemoverItemCarrinhoHandler ---------------------------------------------

// TestRemoverItemCarrinhoHandler_204 prova a remoção normal de um item
// existente -> 204 sem corpo.
func TestRemoverItemCarrinhoHandler_204(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Carrinho Remover", "carrinho-remover-usuario@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Carrinho Remover", 10)

	w := postItemCarrinho(db, "Bearer "+token, `{"produtoId":"`+produtoID+`","estoqueId":"`+estoqueID+`","quantidade":1}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed: status = %d (body=%s)", w.Code, w.Body.String())
	}

	w = deleteItemCarrinho(db, "Bearer "+token, produtoID, estoqueID)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusNoContent, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Errorf("corpo do 204 deveria estar vazio, got %q", w.Body.String())
	}

	w = getCarrinho(db, "Bearer "+token)
	if body := strings.TrimSpace(w.Body.String()); body != `{"itens":[],"removidos":[]}` {
		t.Errorf("carrinho deveria estar vazio após remoção, got %s", body)
	}
}

// TestRemoverItemCarrinhoHandler_404ItemInexistente prova a linha "Remover
// item inexistente" -> 404 NOT_FOUND.
func TestRemoverItemCarrinhoHandler_404ItemInexistente(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Usuario Carrinho 404 Remover", "carrinho-404-remover@empresa.com")

	w := deleteItemCarrinho(db, "Bearer "+token, "00000000-0000-0000-0000-000000000000", "00000000-0000-0000-0000-000000000000")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
	}
}

// TestRemoverItemCarrinhoHandler_404ItemDeOutroUsuario prova que remover o
// par de OUTRO usuário devolve 404 (o WHERE já escopa por usuario_id) e não
// afeta o item do dono.
func TestRemoverItemCarrinhoHandler_404ItemDeOutroUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, tokenDono := seedContaComumECarrinho(t, db, "Usuario Carrinho Dono Remover", "carrinho-dono-remover@empresa.com")
	_, tokenOutro := seedContaComumECarrinho(t, db, "Usuario Carrinho Outro Remover", "carrinho-outro-remover@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Carrinho Escopo Remover", 10)

	w := postItemCarrinho(db, "Bearer "+tokenDono, `{"produtoId":"`+produtoID+`","estoqueId":"`+estoqueID+`","quantidade":2}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed: status = %d (body=%s)", w.Code, w.Body.String())
	}

	w = deleteItemCarrinho(db, "Bearer "+tokenOutro, produtoID, estoqueID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusNotFound, w.Body.String())
	}

	w = getCarrinho(db, "Bearer "+tokenDono)
	var resp struct {
		Itens []services.ItemCarrinho `json:"itens"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Itens) != 1 {
		t.Fatalf("item do dono deveria continuar intacto, Itens = %+v", resp.Itens)
	}
}

// TestRemoverItemCarrinhoHandler_401SemToken prova que uma requisição sem
// Authorization -> 401.
func TestRemoverItemCarrinhoHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)

	w := deleteItemCarrinho(db, "", "00000000-0000-0000-0000-000000000000", "00000000-0000-0000-0000-000000000000")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}
