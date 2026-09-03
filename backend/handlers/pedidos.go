// Package handlers, arquivo pedidos.go: fronteira HTTP do Envio de Pedido —
// Story 7.2 (Epic 7, Pedidos de Retirada), spec-7-2. Molde exato de
// handlers/movimentacoes.go (RegistrarBaixaHandler): decodifica/serializa
// JSON, traduz os erros de services/pedidos.go para o envelope de erro fixo
// (AD-14) e nunca contém regra de negócio própria.
//
// Registro em newMux (main.go): POST /api/pedidos fica atrás só de
// RequireAuth — SEM RequireRole, qualquer conta autenticada (`usuario`+)
// envia seu próprio Pedido, mesmo mínimo de papel do Carrinho (Story 7.1).
// `usuarioID` vem sempre de middleware.UsuarioDaSessao dentro do handler,
// nunca de um campo do corpo (Always, spec-7-2).
package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"stockflow/backend/middleware"
	"stockflow/backend/realtime"
	"stockflow/backend/services"
)

// submeterPedidoRequest é o corpo aceito por POST /api/pedidos. Um eventual
// campo `usuarioId` no corpo é ignorado — nem sequer decodificado — a
// identidade gravada em `pedidos.usuario_id` vem sempre da sessão (Always,
// spec-7-2).
type submeterPedidoRequest struct {
	Solicitante     string `json:"solicitante"`
	ObraCentroCusto string `json:"obraCentroCusto"`
	Observacao      string `json:"observacao"`
}

// SubmeterPedidoHandler expõe POST /api/pedidos: envia o carrinho ativo do
// usuário da sessão como um Pedido `pendente`. `201 {"pedido": {...}}` no
// sucesso, publicando `{"resource":"pedidos","id":<novo>,"change":"created"}`
// no canal `pedidos` (AD-3 do epic-7-context.md, payload mínimo — Never de
// spec-7-2: nunca inclui os itens). `400 VALIDATION_ERROR` para
// solicitante/obraCentroCusto ausentes; `409 CONFLICT` para carrinho vazio
// ou disponibilidade insuficiente em algum item (mensagem já cita os
// nomes).
func SubmeterPedidoHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("SubmeterPedidoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req submeterPedidoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		pedido, err := services.SubmeterPedido(db, usuario.ID, req.Solicitante, req.ObraCentroCusto, req.Observacao)
		var erroValidacao *services.ErroPedidoValidacao
		var erroIndisponivel *services.ErroPedidoIndisponivel
		switch {
		case err == nil:
			registro.Publish("pedidos", realtime.Evento{ID: pedido.ID, Change: "created"})
			escreverJSON(w, http.StatusCreated, map[string]any{"pedido": pedido})
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		case errors.Is(err, services.ErrPedidoCarrinhoVazio):
			escreverErro(w, http.StatusConflict, "CONFLICT", services.ErrPedidoCarrinhoVazio.Error())
		case errors.As(err, &erroIndisponivel):
			escreverErro(w, http.StatusConflict, "CONFLICT", erroIndisponivel.Error())
		default:
			slog.Error("falha ao submeter pedido", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao enviar pedido")
		}
	}
}

// ListarPedidosHandler expõe GET /api/pedidos (Story 7.3, spec-7-3; escopo
// `?escopo=todos` da Fila, Story 7.4, spec-7-4), registrado em newMux atrás
// SÓ de RequireAuth (SEM RequireRole — mesmo mínimo de papel do envio).
// Devolve os Pedidos do PRÓPRIO usuário da sessão por padrão; com
// `?escopo=todos` E papel `almoxarife`+, devolve TODOS os Pedidos da
// organização (a Fila) — a decisão de escopo é inteiramente de
// `services.ListarPedidosParaSessao` (AD-8 forma 3): este handler só repassa
// `usuario.Papel` adiante, nunca chama RankPapel ele mesmo. Qualquer outro
// caso (papel insuficiente OU `escopo` ausente/outro valor) cai no
// comportamento de sempre — nunca 403. Filtro opcional `?status=` restrito a
// `pendente|aprovado|rejeitado`; valor fora disso -> `400 VALIDATION_ERROR`
// (o service rejeita antes de tocar o banco). Sucesso -> `200
// {"pedidos":[...]}` (nunca nil -> `[]`). Sem regra de negócio própria —
// molde de ListarMovimentacoesHandler (movimentacoes.go).
func ListarPedidosHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("ListarPedidosHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		escopoTodos := r.URL.Query().Get("escopo") == "todos"
		filtroStatus := r.URL.Query().Get("status")
		resumos, err := services.ListarPedidosParaSessao(db, usuario.ID, usuario.Papel, escopoTodos, filtroStatus)
		var erroValidacao *services.ErroPedidoValidacao
		switch {
		case err == nil:
			if resumos == nil {
				resumos = []services.PedidoResumo{}
			}
			escreverJSON(w, http.StatusOK, map[string]any{"pedidos": resumos})
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		default:
			slog.Error("falha ao listar pedidos próprios", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar pedidos")
		}
	}
}

// decisaoPedidoRequest é o corpo aceito por POST /api/pedidos/{id}/decisao.
// `Aprovar` é um ponteiro DE PROPÓSITO — mesmo molde de `decisaoRequest`
// (handlers/promocao.go): um corpo `{}`, `{"aprovar":null}` ou com a chave
// errada decodifica sem erro e, com um `bool` puro, viraria silenciosamente
// `aprovar=false` — uma rejeição involuntária de um Pedido pendente. Nil ->
// 400 VALIDATION_ERROR.
type decisaoPedidoRequest struct {
	Aprovar *bool `json:"aprovar"`
}

// DecidirPedidoHandler expõe POST /api/pedidos/{id}/decisao (Story 7.5,
// spec-7-5), registrado em newMux atrás de
// RequireAuth(db,jwtSecret)(RequireRole(services.PapelAlmoxarife)(...)) —
// RequireRole roda a cada requisição, nunca cacheado, o que já satisfaz "o
// papel do aprovador é revalidado na submissão da decisão" só por
// composição; este handler NÃO faz checagem de papel adicional.
//
// Lê `{"aprovar": bool}` sob http.MaxBytesReader — corpo malformado OU sem a
// chave `aprovar` (nil) -> 400 VALIDATION_ERROR, nunca uma rejeição
// silenciosa. Sucesso -> `200 {"pedido": PedidoDetalhe}` (cada item com
// `quantidade` e `quantidadeAprovada` lado a lado), publicando
// `{"resource":"pedidos","id":<pedido>,"change":<novo status>}` no canal
// `pedidos` — mesmo padrão de SubmeterPedidoHandler.
func DecidirPedidoHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("DecidirPedidoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req decisaoPedidoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}
		if req.Aprovar == nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		pedido, err := services.DecidirPedido(db, r.PathValue("id"), usuario.ID, usuario.Papel, *req.Aprovar)
		switch {
		case err == nil:
			registro.Publish("pedidos", realtime.Evento{ID: pedido.ID, Change: pedido.Status})
			escreverJSON(w, http.StatusOK, map[string]any{"pedido": pedido})
		case errors.Is(err, services.ErrPedidoNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "pedido não encontrado")
		case errors.Is(err, services.ErrPedidoNaoPendente):
			escreverErro(w, http.StatusConflict, "CONFLICT", "este pedido não está mais pendente")
		default:
			slog.Error("falha ao decidir pedido", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao decidir pedido")
		}
	}
}

// BuscarPedidoHandler expõe GET /api/pedidos/{id} (Story 7.3, spec-7-3),
// registrado em newMux atrás SÓ de RequireAuth. Devolve o cabeçalho + os
// itens em snapshot do Pedido `{id}` se o solicitante for o dono da sessão
// OU tiver papel `almoxarife`+ (padrão de escopo AD-8, resolvido em
// `services.BuscarPedidoProprio`). Qualquer outro caso — Pedido de outro
// usuário sem papel suficiente, id inexistente, id malformado — colapsa em
// `404 NOT_FOUND` com a MESMA mensagem: nunca revela a existência de um
// Pedido alheio, nunca responde `403`. Sucesso -> `200 {"pedido": {...}}`.
func BuscarPedidoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("BuscarPedidoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		detalhe, err := services.BuscarPedidoProprio(db, r.PathValue("id"), usuario.ID, usuario.Papel)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]any{"pedido": detalhe})
		case errors.Is(err, services.ErrPedidoNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "pedido não encontrado")
		default:
			slog.Error("falha ao buscar pedido", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao buscar pedido")
		}
	}
}
