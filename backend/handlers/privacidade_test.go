// Testes HTTP de ExportarDadosUsuarioHandler — Story 8.1 (Epic 8,
// Privacidade/LGPD), spec-8-1. Molde de handlers/pedidos_test.go
// (BaixarReciboPedidoHandler): mux mínimo com só esta rota atrás de
// RequireAuth, sem RequireRole.
package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

func getExportarDados(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/usuarios/me/exportar-dados",
		middleware.RequireAuth(db, testJWTSecret)(ExportarDadosUsuarioHandler(db)))
	r := httptest.NewRequest(http.MethodGet, "/api/usuarios/me/exportar-dados", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// TestExportarDadosUsuarioHandler_200ComHistorico cobre "Happy path com
// histórico" da I/O Matrix: 200, Content-Type: application/json,
// Content-Disposition de download de "meus-dados.json", corpo com
// nome/email da sessão e o Pedido próprio do usuário.
func TestExportarDadosUsuarioHandler_200ComHistorico(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	usuarioID, token := seedContaComumECarrinho(t, db, "H81 Dono", "h-privacidade-81-dono@empresa.com")
	pedido := seedPedidoViaServico(t, db, usuarioID, "H81", 2)

	w := getExportarDados(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	wantDisposition := `attachment; filename="meus-dados.json"`
	if cd := w.Header().Get("Content-Disposition"); cd != wantDisposition {
		t.Errorf("Content-Disposition = %q, want %q", cd, wantDisposition)
	}

	var body services.DadosPessoaisExportados
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v (body=%s)", err, w.Body.String())
	}
	if body.Nome != "H81 Dono" {
		t.Errorf("Nome = %q, want %q", body.Nome, "H81 Dono")
	}
	if body.Email != "h-privacidade-81-dono@empresa.com" {
		t.Errorf("Email = %q, want %q", body.Email, "h-privacidade-81-dono@empresa.com")
	}
	if body.LogAcesso == nil {
		t.Error("LogAcesso = nil, want array (mesmo vazio)")
	}
	if body.Movimentacoes == nil {
		t.Error("Movimentacoes = nil, want array (mesmo vazio)")
	}
	if len(body.Pedidos) != 1 || body.Pedidos[0].ID != pedido.ID {
		t.Fatalf("Pedidos = %+v, want 1 pedido com ID %q", body.Pedidos, pedido.ID)
	}
}

// TestExportarDadosUsuarioHandler_200SemHistorico cobre "Usuário sem
// Movimentação/Pedido próprios" da I/O Matrix: mesmo 200/mesmo formato,
// Movimentacoes/Pedidos vêm como array vazio. LogAcesso NÃO é testado vazio
// aqui: `seedContaComumECarrinho` faz um login real (tokenDeLogin) para
// obter o token, e todo login concluído grava sua própria linha em
// logs_acesso (RegistrarTentativaLogin) — o cenário "log de acesso vazio"
// já está coberto em services/privacidade_test.go
// (TestExportarDadosUsuario_SemNenhumRegistro), que semeia a conta sem
// passar pelo fluxo de login.
func TestExportarDadosUsuarioHandler_200SemHistorico(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	_, token := seedContaComumECarrinho(t, db, "H81 Vazio", "h-privacidade-81-vazio@empresa.com")

	w := getExportarDados(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	var body services.DadosPessoaisExportados
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v (body=%s)", err, w.Body.String())
	}
	if len(body.Movimentacoes) != 0 || len(body.Pedidos) != 0 {
		t.Errorf("esperava Movimentacoes/Pedidos vazios, got Movimentacoes=%v Pedidos=%v",
			body.Movimentacoes, body.Pedidos)
	}
	if len(body.LogAcesso) != 1 {
		t.Errorf("esperava 1 linha de log de acesso (o login feito para obter o token), got %d: %v",
			len(body.LogAcesso), body.LogAcesso)
	}
}

// TestExportarDadosUsuarioHandler_401SemToken cobre "Sem sessão válida" da
// I/O Matrix: 401 padrão de RequireAuth, sem lógica própria desta story.
func TestExportarDadosUsuarioHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	w := getExportarDados(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}
