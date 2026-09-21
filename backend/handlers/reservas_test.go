package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stockflow/backend/middleware"
	"stockflow/backend/realtime"
	"stockflow/backend/services"
)

// Story 11.3 — GET /api/produtos/{id}/estoques/{estoqueId}/reservas e os
// eventos `produtos` publicados no envio/decisão do Pedido.

func getReservas(db *sql.DB, authHeader, produtoID, estoqueID string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /e/{slug}/api/produtos/{id}/estoques/{estoqueId}/reservas",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(ListarReservasSaldoHandler(db))))
	r := httptest.NewRequest(http.MethodGet, prefixoEmpresaTeste+"/api/produtos/"+produtoID+"/estoques/"+estoqueID+"/reservas", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestListarReservasSaldoHandler_200(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	dono, token := seedContaComumECarrinho(t, db, "Reservas H Dono", "h-reservas-dono@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Reservas H", 10)
	if _, err := services.AdicionarItemCarrinho(db, empresaTeste, dono, produtoID, estoqueID, 4); err != nil {
		t.Fatal(err)
	}
	pedido, err := services.SubmeterPedido(db, empresaTeste, dono, "Maria", "Obra", "")
	if err != nil {
		t.Fatal(err)
	}

	// Qualquer conta autenticada (papel usuario) enxerga a lista.
	w := getReservas(db, "Bearer "+token, produtoID, estoqueID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Reservas []struct {
			PedidoID    string  `json:"pedidoId"`
			Solicitante string  `json:"solicitante"`
			Quantidade  float64 `json:"quantidade"`
			CriadoEm    string  `json:"criadoEm"`
		} `json:"reservas"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Reservas) != 1 || resp.Reservas[0].PedidoID != pedido.ID ||
		resp.Reservas[0].Solicitante != "Maria" || resp.Reservas[0].Quantidade != 4 || resp.Reservas[0].CriadoEm == "" {
		t.Errorf("reservas = %+v", resp.Reservas)
	}
}

func TestListarReservasSaldoHandler_404E401(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "Reservas H 404", "h-reservas-404@empresa.com")
	produtoID, estoqueID := seedProdutoComSaldoHandler(t, db, "Canteiro Reservas H 404", 10)
	const inexistente = "00000000-0000-0000-0000-000000000000"

	for nome, par := range map[string][2]string{
		"produto malformado":  {"nao-e-uuid", estoqueID},
		"estoque malformado":  {produtoID, "nao-e-uuid"},
		"produto inexistente": {inexistente, estoqueID},
		"estoque inexistente": {produtoID, inexistente},
	} {
		w := getReservas(db, "Bearer "+token, par[0], par[1])
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 (body=%s)", nome, w.Code, w.Body.String())
		}
	}
	if w := getReservas(db, "", produtoID, estoqueID); w.Code != http.StatusUnauthorized {
		t.Errorf("sem token: status = %d, want 401", w.Code)
	}
}

// eventosProdutos esvazia (sem bloquear) o canal e devolve os ids dos eventos
// `produtos` recebidos — os handlers publicam de forma síncrona, então tudo já
// está no buffer quando a resposta HTTP volta.
func eventosProdutos(t *testing.T, eventos <-chan realtime.Evento) []string {
	t.Helper()
	var ids []string
	for {
		select {
		case ev := <-eventos:
			if ev.Resource == "produtos" {
				if ev.Change != "updated" {
					t.Errorf("evento produtos = %+v, want change=updated", ev)
				}
				ids = append(ids, ev.ID)
			}
		default:
			return ids
		}
	}
}

func confereIdsUnicos(t *testing.T, etapa string, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: eventos produtos = %v, want exatamente %v (um por Produto)", etapa, got, want)
	}
	vistos := map[string]bool{}
	for _, id := range got {
		vistos[id] = true
	}
	for _, id := range want {
		if !vistos[id] {
			t.Errorf("%s: faltou evento produtos para %s (got %v)", etapa, id, got)
		}
	}
}

// Pedido com 3 itens: o Produto X em DOIS Estoques e o Produto Y em um —
// o envio e a decisão publicam UM evento `produtos` por Produto (X uma vez).
func TestPedidoHandlers_PublicamProdutosNoEnvioENaDecisao(t *testing.T) {
	for _, aprovar := range []bool{false, true} {
		nome := "rejeicao"
		if aprovar {
			nome = "aprovacao"
		}
		t.Run(nome, func(t *testing.T) {
			db := testDB(t)
			limparProdutosHandler(t, db)
			dono, token := seedContaComumECarrinho(t, db, "Reservas H Evento "+nome, "h-reservas-evento-"+nome+"@empresa.com")
			criarContaComPapel(t, db, "Reservas H Almox "+nome, "h-reservas-almox-"+nome+"@empresa.com", "senha-123456", "almoxarife")
			tokenAlmox := tokenDeLogin(t, db, "h-reservas-almox-"+nome+"@empresa.com", "senha-123456")

			produtoX, estoque1 := seedProdutoComSaldoHandler(t, db, "Canteiro Evento X1 "+nome, 10)
			produtoY, estoqueY := seedProdutoComSaldoHandler(t, db, "Canteiro Evento Y "+nome, 10)
			estoque2, err := services.CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Canteiro Evento X2 "+nome)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := services.LancarSaldo(db, empresaTeste, dono, produtoX, estoque2.ID, 5, ""); err != nil {
				t.Fatal(err)
			}
			for _, it := range []struct{ p, e string }{{produtoX, estoque1}, {produtoX, estoque2.ID}, {produtoY, estoqueY}} {
				if _, err := services.AdicionarItemCarrinho(db, empresaTeste, dono, it.p, it.e, 2); err != nil {
					t.Fatal(err)
				}
			}

			registro := realtime.NewRegistry()
			eventos, cancelar := registro.Subscribe(empresaTeste)
			defer cancelar()

			mux := http.NewServeMux()
			mux.HandleFunc("POST /e/{slug}/api/pedidos",
				comEmpresa(db, middleware.RequireAuth(db, testJWTSecret)(SubmeterPedidoHandler(db, registro))))
			mux.HandleFunc("POST /e/{slug}/api/pedidos/{id}/decisao",
				comEmpresa(db, middleware.RequireAuth(db, testJWTSecret)(
					middleware.RequireRole(services.PapelAlmoxarife)(DecidirPedidoHandler(db, registro)))))

			r := httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos", strings.NewReader(`{"solicitante":"F","obraCentroCusto":"O"}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != http.StatusCreated {
				t.Fatalf("envio: status = %d (body=%s)", w.Code, w.Body.String())
			}
			var resp struct {
				Pedido services.Pedido `json:"pedido"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			confereIdsUnicos(t, "envio", eventosProdutos(t, eventos), produtoX, produtoY)

			corpo := `{"aprovar":false}`
			if aprovar {
				corpo = `{"aprovar":true}`
			}
			r = httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/pedidos/"+resp.Pedido.ID+"/decisao", strings.NewReader(corpo))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+tokenAlmox)
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("decisão: status = %d (body=%s)", w.Code, w.Body.String())
			}
			confereIdsUnicos(t, "decisão", eventosProdutos(t, eventos), produtoX, produtoY)
		})
	}
}
