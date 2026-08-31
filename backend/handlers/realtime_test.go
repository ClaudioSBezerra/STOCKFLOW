package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"stockflow/backend/middleware"
	"stockflow/backend/realtime"
	"stockflow/backend/services"
)

// --- Story 4.4: Detalhe do produto por Estoque com atualização em tempo
// real -----------------------------------------------------------------
//
// O fluxo completo "ticket -> stream -> evento recebido" exige uma conexão
// HTTP de verdade (não só ResponseRecorder) para exercitar o loop de longa
// duração de forma não-flakey — coberto em backend/main_test.go
// (httptest.NewServer real, Story 4.4, spec-4-4, Code Map). Aqui: os
// caminhos de erro (401 em cada passo da autenticação por ticket) e o
// formato dos headers/status de uma conexão válida até o cliente cancelar.

func postRealtimeTicket(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/realtime/ticket",
		middleware.RequireAuth(db, testJWTSecret)(
			EmitirTicketRealtimeHandler(db)))
	r := httptest.NewRequest(http.MethodPost, "/api/realtime/ticket", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// getRealtimeStream despacha GET /api/realtime/stream?ticket=<ticket> direto
// no handler (SEM RequireAuth — a rota real também não leva, ver main.go),
// com o context de `r` já ligado a `ctx` (o chamador controla o
// cancelamento).
func getRealtimeStream(ctx context.Context, db *sql.DB, registro *realtime.Registry, ticket string) *httptest.ResponseRecorder {
	alvo := "/api/realtime/stream"
	if ticket != "" {
		alvo += "?ticket=" + ticket
	}
	r := httptest.NewRequest(http.MethodGet, alvo, nil).WithContext(ctx)
	w := httptest.NewRecorder()
	StreamRealtimeHandler(db, registro)(w, r)
	return w
}

// TestEmitirTicketRealtimeHandler_201ParaQualquerPapel prova que POST
// /api/realtime/ticket devolve 201 com um ticket de 43 caracteres para
// qualquer papel autenticado (usuario+), sem RequireRole.
func TestEmitirTicketRealtimeHandler_201ParaQualquerPapel(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Ticket Usuario", "ticket-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "ticket-usuario@empresa.com", "senha-123456")

	w := postRealtimeTicket(db, "Bearer "+token)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Ticket) != 43 {
		t.Errorf("len(ticket) = %d, want 43", len(resp.Ticket))
	}
}

// TestEmitirTicketRealtimeHandler_401SemToken prova que sem Authorization ->
// 401 (RequireAuth).
func TestEmitirTicketRealtimeHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	w := postRealtimeTicket(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestStreamRealtimeHandler_401SemTicket prova a linha "Sem `ticket` na
// query / vazio" da matriz.
func TestStreamRealtimeHandler_401SemTicket(t *testing.T) {
	db := testDB(t)
	w := getRealtimeStream(context.Background(), db, realtime.NewRegistry(), "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("código = %q, want TOKEN_EXPIRED", env.Error.Code)
	}
}

// TestStreamRealtimeHandler_401TicketInexistente prova que um ticket que
// nunca foi emitido é recusado.
func TestStreamRealtimeHandler_401TicketInexistente(t *testing.T) {
	db := testDB(t)
	w := getRealtimeStream(context.Background(), db, realtime.NewRegistry(), "ticket-que-nunca-existiu")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestStreamRealtimeHandler_401TicketReaproveitado prova a linha "Ticket
// reaproveitado (2ª vez)" da matriz: o mesmo ticket usado duas vezes tem a
// 2ª conexão recusada.
func TestStreamRealtimeHandler_401TicketReaproveitado(t *testing.T) {
	db := testDB(t)
	usuarioID := criarContaComPapel(t, db, "Ticket Reuso", "ticket-reuso@empresa.com", "senha-123456", "usuario")
	ticket, err := services.EmitirTicketRealtime(db, usuarioID)
	if err != nil {
		t.Fatalf("seed EmitirTicketRealtime: %v", err)
	}

	registro := realtime.NewRegistry()

	// 1ª conexão: cancela logo em seguida (o teste só precisa que o ticket
	// seja consumido, não de receber nenhum evento).
	ctx1, cancel1 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel1()
	w1 := getRealtimeStream(ctx1, db, registro, ticket)
	if w1.Code != http.StatusOK {
		t.Fatalf("1ª conexão: status = %d, want %d (body=%s)", w1.Code, http.StatusOK, w1.Body.String())
	}

	// 2ª conexão com o MESMO ticket já consumido -> 401.
	w2 := getRealtimeStream(context.Background(), db, registro, ticket)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("2ª conexão: status = %d, want %d (body=%s)", w2.Code, http.StatusUnauthorized, w2.Body.String())
	}
}

// TestStreamRealtimeHandler_401TicketExpirado prova a linha "Ticket expirado
// (>30s)" da matriz.
func TestStreamRealtimeHandler_401TicketExpirado(t *testing.T) {
	db := testDB(t)
	usuarioID := criarContaComPapel(t, db, "Ticket Expirado", "ticket-expirado@empresa.com", "senha-123456", "usuario")
	ticket, err := services.EmitirTicketRealtime(db, usuarioID)
	if err != nil {
		t.Fatalf("seed EmitirTicketRealtime: %v", err)
	}
	if _, err := db.Exec(`UPDATE tokens_acao SET expira_em = now() - interval '1 second' WHERE token = $1`, ticket); err != nil {
		t.Fatalf("falha ao forçar expiração: %v", err)
	}

	w := getRealtimeStream(context.Background(), db, realtime.NewRegistry(), ticket)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestStreamRealtimeHandler_200TicketValidoPromoveParaSSE prova que um
// ticket válido promove a resposta a `text/event-stream` com os headers
// corretos — a conexão fica aberta até o context ser cancelado (aqui via
// timeout curto, já que ResponseRecorder não modela uma conexão de rede de
// verdade; o fluxo end-to-end com evento recebido está em main_test.go).
func TestStreamRealtimeHandler_200TicketValidoPromoveParaSSE(t *testing.T) {
	db := testDB(t)
	usuarioID := criarContaComPapel(t, db, "Ticket Sucesso", "ticket-sucesso@empresa.com", "senha-123456", "usuario")
	ticket, err := services.EmitirTicketRealtime(db, usuarioID)
	if err != nil {
		t.Fatalf("seed EmitirTicketRealtime: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	w := getRealtimeStream(ctx, db, realtime.NewRegistry(), ticket)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	if conn := w.Header().Get("Connection"); conn != "keep-alive" {
		t.Errorf("Connection = %q, want keep-alive", conn)
	}
	if xab := w.Header().Get("X-Accel-Buffering"); xab != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no (evita buffering de proxy reverso na frente do SSE)", xab)
	}
}

// TestStreamRealtimeHandler_ContaInativaRecusada prova a defesa em
// profundidade: um ticket válido cuja conta foi desativada ENTRE a emissão
// do ticket e a conexão é recusada (mesmo comportamento de
// middleware.RequireAuth para uma conta com `ativo=false`).
func TestStreamRealtimeHandler_ContaInativaRecusada(t *testing.T) {
	db := testDB(t)
	usuarioID := criarContaComPapel(t, db, "Ticket Conta Inativa", "ticket-conta-inativa@empresa.com", "senha-123456", "usuario")
	ticket, err := services.EmitirTicketRealtime(db, usuarioID)
	if err != nil {
		t.Fatalf("seed EmitirTicketRealtime: %v", err)
	}
	if _, err := db.Exec(`UPDATE usuarios SET ativo = false WHERE id = $1`, usuarioID); err != nil {
		t.Fatalf("falha ao desativar conta: %v", err)
	}

	w := getRealtimeStream(context.Background(), db, realtime.NewRegistry(), ticket)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "SESSION_REVOKED" {
		t.Errorf("código = %q, want SESSION_REVOKED", env.Error.Code)
	}
}
