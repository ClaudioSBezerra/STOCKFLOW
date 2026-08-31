package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"stockflow/backend/middleware"
	"stockflow/backend/realtime"
	"stockflow/backend/services"
)

// Handlers de tempo real (Story 4.4, spec-4-4, AD-2/AD-3): a infraestrutura
// SSE in-process do produto — POST /api/realtime/ticket emite um ticket de
// curta duração (30s, uso único) e GET /api/realtime/stream troca esse
// ticket por uma conexão SSE de longa duração, assinando o
// `*realtime.Registry` compartilhado (criado uma vez em `newMux`, main.go).
//
// Registro em newMux (main.go):
//   - POST /api/realtime/ticket -> RequireAuth apenas: qualquer conta
//     autenticada pode abrir sua própria conexão em tempo real.
//   - GET /api/realtime/stream -> SEM RequireAuth: um `EventSource` do
//     navegador nunca envia o header `Authorization`, então a autenticação
//     acontece pelo próprio ticket na query string (ver
//     StreamRealtimeHandler).

// EmitirTicketRealtimeHandler expõe POST /api/realtime/ticket: só
// RequireAuth, qualquer papel (`usuario`+). `201 {"ticket": "<43 chars
// url-safe>"}` no sucesso. Erro de banco -> 500 INTERNAL_ERROR + slog.
func EmitirTicketRealtimeHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("EmitirTicketRealtimeHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		ticket, err := services.EmitirTicketRealtime(db, usuario.ID)
		if err != nil {
			slog.Error("falha ao emitir ticket de conexão em tempo real", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao emitir ticket")
			return
		}

		escreverJSON(w, http.StatusCreated, map[string]any{"ticket": ticket})
	}
}

// intervaloKeepAliveSSE é o intervalo do comentário `: keep-alive\n\n`
// enviado por StreamRealtimeHandler enquanto nenhum evento chega — evita
// timeout de proxy/idle numa conexão HTTP de longa duração (Story 4.4,
// AD-3).
const intervaloKeepAliveSSE = 15 * time.Second

// StreamRealtimeHandler expõe GET /api/realtime/stream?ticket=<token>: SEM
// RequireAuth — um `EventSource` do navegador não envia `Authorization`, e
// a AD-3 desenha exatamente essa autenticação alternativa por ticket na
// query string. Consome o ticket atomicamente
// (services.ConsumirTicketRealtime, mesma corrida fechada de
// VerificarEmail: SELECT distingue "não existe" de "expirado/usado", UPDATE
// repete as mesmas condições), resolve `usuario_id`, revalida via
// services.BuscarUsuarioSessao (conta ainda `ativo` — defesa em
// profundidade igual a middleware.RequireAuth, MESMO vocabulário de código
// de erro: TOKEN_EXPIRED para o ticket, SESSION_REVOKED para a conta) —
// falha em QUALQUER passo -> 401 (envelope de erro fixo, antes de promover
// a resposta a `text/event-stream`; nenhuma falha aqui vira 500 — o cliente
// simplesmente tenta de novo com um novo ticket, ver
// frontend/src/lib/realtime/client.ts).
//
// Ticket válido: `Content-Type: text/event-stream`, `Cache-Control:
// no-cache`, `Connection: keep-alive`, assina `registro` (compartilhado
// com toda a aplicação), escreve `data: <evento json>\n\n` por evento
// recebido, `: keep-alive\n\n` a cada intervaloKeepAliveSSE, até
// `r.Context().Done()` (cliente desconectou) — então desinscreve
// (`defer cancelar()`).
func StreamRealtimeHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
		if ticket == "" {
			escreverErro(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "ticket de conexão ausente ou inválido")
			return
		}

		usuarioID, err := services.ConsumirTicketRealtime(db, ticket)
		if err != nil {
			if !errors.Is(err, services.ErrTokenNaoEncontrado) && !errors.Is(err, services.ErrTokenExpirado) {
				slog.Error("falha ao consumir ticket de conexão em tempo real", "error", err)
			}
			escreverErro(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "ticket de conexão ausente ou inválido")
			return
		}

		usuario, err := services.BuscarUsuarioSessao(db, usuarioID)
		if err != nil {
			if !errors.Is(err, services.ErrUsuarioSessaoNaoEncontrado) {
				slog.Error("falha ao revalidar usuário para conexão em tempo real", "error", err)
			}
			escreverErro(w, http.StatusUnauthorized, "SESSION_REVOKED", "sessão revogada")
			return
		}
		if !usuario.Ativo {
			escreverErro(w, http.StatusUnauthorized, "SESSION_REVOKED", "sessão revogada")
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			slog.Error("StreamRealtimeHandler: http.ResponseWriter não implementa http.Flusher")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "streaming não suportado")
			return
		}

		eventos, cancelar := registro.Subscribe()
		defer cancelar()

		// O http.Server compartilhado (main.go) tem WriteTimeout: 15s — sem
		// zerar o write deadline desta conexão específica, o servidor
		// derrubaria todo stream SSE aos 15s, quase no mesmo instante do
		// primeiro keep-alive. Nunca falha de verdade (ResponseController
		// sobre um ResponseWriter padrão do net/http sempre suporta
		// SetWriteDeadline); erro aqui só indicaria um adapter exótico, e
		// mesmo assim o stream seguiria funcionando até o WriteTimeout.
		if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
			slog.Warn("StreamRealtimeHandler: falha ao zerar write deadline", "error", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		// X-Accel-Buffering: no — desliga o buffering de proxy reverso
		// (Nginx/Traefik na frente do app em Coolify) para esta resposta;
		// sem isso um proxy poderia reter o stream SSE, atrasando a entrega
		// e comprometendo o propósito de "tempo quase real" da story.
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ticker := time.NewTicker(intervaloKeepAliveSSE)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case evento, ok := <-eventos:
				if !ok {
					return
				}
				corpo, err := json.Marshal(evento)
				if err != nil {
					slog.Error("falha ao serializar evento SSE", "error", err)
					continue
				}
				if _, err := fmt.Fprintf(w, "data: %s\n\n", corpo); err != nil {
					return
				}
				flusher.Flush()
			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}
