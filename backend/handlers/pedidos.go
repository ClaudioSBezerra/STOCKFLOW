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
	// CentroCustoID (Story 12.3): Centro de Custo cadastrado, opcional.
	// Ausente, `null` ou só espaços = sem Centro. Sempre revalidado no
	// service contra a Empresa do contexto.
	CentroCustoID *string `json:"centro_custo_id"`
}

// pedidoCriadoResponse é o Pedido do 201 de POST /api/pedidos: o `Pedido`
// mais `centro_custo_id` (`null` quando ausente). Só esta resposta o expõe —
// listagens/detalhe/recibo não (Never de spec-12-3).
type pedidoCriadoResponse struct {
	services.Pedido
	CentroCustoID *string `json:"centro_custo_id"`
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
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req submeterPedidoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		centroCustoID := ""
		if req.CentroCustoID != nil {
			centroCustoID = *req.CentroCustoID
		}
		pedido, err := services.SubmeterPedidoComCentroCusto(db, empresa.ID, usuario.ID, req.Solicitante, req.ObraCentroCusto, req.Observacao, centroCustoID)
		var erroValidacao *services.ErroPedidoValidacao
		var erroIndisponivel *services.ErroPedidoIndisponivel
		var erroInativo *services.ErroPedidoProdutoInativo
		switch {
		case err == nil:
			registro.Publish(empresa.ID, "pedidos", realtime.Evento{ID: pedido.ID, Change: "created"})
			// Story 11.3: o envio reserva saldo — atualiza detalhes abertos.
			for _, produtoID := range idsUnicos(pedido.ProdutoIDs) {
				registro.Publish(empresa.ID, "produtos", realtime.Evento{ID: produtoID, Change: "updated"})
			}
			escreverJSON(w, http.StatusCreated, map[string]any{"pedido": pedidoCriadoResponse{Pedido: pedido, CentroCustoID: pedido.CentroCustoID}})
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		case errors.Is(err, services.ErrPedidoCarrinhoVazio):
			escreverErro(w, http.StatusConflict, "CONFLICT", services.ErrPedidoCarrinhoVazio.Error())
		case errors.As(err, &erroIndisponivel):
			escreverErro(w, http.StatusConflict, "CONFLICT", erroIndisponivel.Error())
		case errors.As(err, &erroInativo):
			escreverErro(w, http.StatusConflict, "PRODUTO_INATIVO", erroInativo.Error())
		default:
			slog.Error("falha ao submeter pedido", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao enviar pedido")
		}
	}
}

// submeterPedidoTransferenciaRequest é o corpo aceito por POST
// /api/pedidos/transferencia (Story 20.1, Epic 20, spec-20-1) — envio DIRETO
// de 1 item (sem Carrinho), tela própria. `usuarioId` nunca é lido do corpo
// (Always, mesmo padrão de submeterPedidoRequest) — a identidade vem sempre
// de middleware.UsuarioDaSessao. `solicitante` também NÃO é lido do corpo
// (requisito do usuário, pós-revisão, mesmo molde de
// submeterPedidoBaixaRequest: "Solicitante é o usuário logado, não precisa
// pedir") — o handler resolve `usuario.Nome` da sessão e o passa direto ao
// service, nunca aceitando um valor vindo da requisição.
type submeterPedidoTransferenciaRequest struct {
	ObraCentroCusto  string  `json:"obraCentroCusto"`
	Observacao       string  `json:"observacao"`
	ProdutoID        string  `json:"produtoId"`
	EstoqueOrigemID  string  `json:"estoqueOrigemId"`
	EstoqueDestinoID string  `json:"estoqueDestinoId"`
	Quantidade       float64 `json:"quantidade"`
}

// SubmeterPedidoTransferenciaHandler expõe POST /api/pedidos/transferencia
// (Story 20.1, Epic 20, spec-20-1): envia um Pedido de Transferência entre
// Filiais `pendente` — reserva dura só da ORIGEM, nunca debita/credita saldo
// físico (isso é a Story 20.3). Molde de SubmeterPedidoBaixaHandler: registrado
// atrás SÓ de RequireAuth (mesmo mínimo de papel do Pedido de retirada/Baixa —
// qualquer conta autenticada `usuario`+ envia seu próprio Pedido).
// `solicitante` é SEMPRE `usuario.Nome` da sessão (middleware.UsuarioDaSessao)
// — nunca aceito do corpo da requisição (requisito do usuário, pós-revisão,
// mesmo molde de SubmeterPedidoBaixaHandler). `201 {"pedido": {...}}` no
// sucesso, publicando
// `{"resource":"pedidos","id":<novo>,"change":"created"}` no canal
// `pedidos` + um evento `produtos` (`updated`) para o único Produto do
// Pedido (Story 11.3: o envio reserva saldo, atualiza detalhes abertos).
// `400 VALIDATION_ERROR` para campos ausentes/vazios (inclusive
// produtoId/estoqueOrigemId/estoqueDestinoId e origem==destino/destino
// inexistente — Always, spec-20-1: nunca colapsa em 409; `obraCentroCusto`
// ausente/vazio NÃO está nesta lista — Story 20.1 pós-revisão, mesmo molde do
// Pedido de Baixa, campo opcional);
// `409 CONFLICT` para disponibilidade insuficiente na origem;
// `409 PRODUTO_INATIVO` para Produto inativo (mesmo código do Pedido de
// retirada).
func SubmeterPedidoTransferenciaHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("SubmeterPedidoTransferenciaHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req submeterPedidoTransferenciaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		pedido, err := services.SubmeterPedidoTransferencia(
			db, empresa.ID, usuario.ID, usuario.Nome, req.ObraCentroCusto, req.Observacao,
			req.ProdutoID, req.EstoqueOrigemID, req.EstoqueDestinoID, req.Quantidade,
		)
		var erroValidacao *services.ErroPedidoValidacao
		var erroIndisponivel *services.ErroPedidoIndisponivel
		var erroInativo *services.ErroPedidoProdutoInativo
		switch {
		case err == nil:
			registro.Publish(empresa.ID, "pedidos", realtime.Evento{ID: pedido.ID, Change: "created"})
			for _, produtoID := range idsUnicos(pedido.ProdutoIDs) {
				registro.Publish(empresa.ID, "produtos", realtime.Evento{ID: produtoID, Change: "updated"})
			}
			escreverJSON(w, http.StatusCreated, map[string]any{"pedido": pedido})
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		case errors.As(err, &erroIndisponivel):
			escreverErro(w, http.StatusConflict, "CONFLICT", erroIndisponivel.Error())
		case errors.As(err, &erroInativo):
			escreverErro(w, http.StatusConflict, "PRODUTO_INATIVO", erroInativo.Error())
		default:
			slog.Error("falha ao submeter pedido de transferência", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao enviar pedido de transferência")
		}
	}
}

// submeterPedidoBaixaRequest é o corpo aceito por POST /api/pedidos/baixa
// (Story 20.2, Epic 20, spec-20-2) — envio DIRETO de 1 item (sem Carrinho),
// tela própria, SEM Estoque destino (molde de
// submeterPedidoTransferenciaRequest, sem `estoqueDestinoId`). `usuarioId`
// nunca é lido do corpo (Always, mesmo padrão dos demais) — a identidade vem
// sempre de middleware.UsuarioDaSessao. `solicitante` também NÃO é lido do
// corpo (requisito do usuário, pós-revisão: "Solicitante é o usuário logado,
// não precisa pedir") — o handler resolve `usuario.Nome` da sessão e o passa
// direto ao service, nunca aceitando um valor vindo da requisição.
type submeterPedidoBaixaRequest struct {
	ObraCentroCusto string  `json:"obraCentroCusto"`
	Observacao      string  `json:"observacao"`
	ProdutoID       string  `json:"produtoId"`
	EstoqueID       string  `json:"estoqueId"`
	Quantidade      float64 `json:"quantidade"`
}

// SubmeterPedidoBaixaHandler expõe POST /api/pedidos/baixa (Story 20.2, Epic
// 20, spec-20-2): envia um Pedido de Baixa de Estoque `pendente` — reserva
// dura do par (Produto, Estoque), nunca debita saldo físico (RegistrarBaixa,
// movimentacoes.go, continua a ação direta sem aprovação; a decisão de
// verdade é a Story 20.3). Molde de SubmeterPedidoTransferenciaHandler, sem o
// campo de destino e sem `solicitante` no corpo (requisito do usuário,
// pós-revisão): registrado atrás SÓ de RequireAuth (mesmo mínimo de papel do
// Pedido de retirada/Transferência — qualquer conta autenticada `usuario`+
// envia seu próprio Pedido). `solicitante` é SEMPRE `usuario.Nome` da sessão
// (middleware.UsuarioDaSessao) — nunca aceito do corpo da requisição.
// `201 {"pedido": {...}}` no sucesso,
// publicando `{"resource":"pedidos","id":<novo>,"change":"created"}` no canal
// `pedidos` + um evento `produtos` (`updated`) para o único Produto do
// Pedido. `400 VALIDATION_ERROR` para campos ausentes/vazios (inclusive
// produtoId/estoqueId — Always, spec-20-2: nunca colapsa em 409);
// `409 CONFLICT` para disponibilidade insuficiente; `409 PRODUTO_INATIVO`
// para Produto inativo (mesmo código do Pedido de retirada/Transferência).
func SubmeterPedidoBaixaHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("SubmeterPedidoBaixaHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req submeterPedidoBaixaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		pedido, err := services.SubmeterPedidoBaixa(
			db, empresa.ID, usuario.ID, usuario.Nome, req.ObraCentroCusto, req.Observacao,
			req.ProdutoID, req.EstoqueID, req.Quantidade,
		)
		var erroValidacao *services.ErroPedidoValidacao
		var erroIndisponivel *services.ErroPedidoIndisponivel
		var erroInativo *services.ErroPedidoProdutoInativo
		switch {
		case err == nil:
			registro.Publish(empresa.ID, "pedidos", realtime.Evento{ID: pedido.ID, Change: "created"})
			for _, produtoID := range idsUnicos(pedido.ProdutoIDs) {
				registro.Publish(empresa.ID, "produtos", realtime.Evento{ID: produtoID, Change: "updated"})
			}
			escreverJSON(w, http.StatusCreated, map[string]any{"pedido": pedido})
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		case errors.As(err, &erroIndisponivel):
			escreverErro(w, http.StatusConflict, "CONFLICT", erroIndisponivel.Error())
		case errors.As(err, &erroInativo):
			escreverErro(w, http.StatusConflict, "PRODUTO_INATIVO", erroInativo.Error())
		default:
			slog.Error("falha ao submeter pedido de baixa", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao enviar pedido de baixa")
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
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		escopoTodos := r.URL.Query().Get("escopo") == "todos"
		filtroStatus := r.URL.Query().Get("status")
		resumos, err := services.ListarPedidosParaSessao(db, empresa.ID, usuario.ID, usuario.Papel, escopoTodos, filtroStatus)
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

// IndicadoresPedidosHandler expõe GET /api/pedidos/indicadores (Story 17.4,
// spec-17-4), registrado em newMux ANTES de GET /api/pedidos/{id} para evitar
// conflito de path. Atrás SÓ de RequireAuth (usuario+) — molde de
// ListarPedidosHandler. Com `?escopo=todos` E papel almoxarife+: conta todos
// os Pedidos da Empresa; caso contrário: só os do próprio Usuário — nunca 403.
// Sucesso -> `200 {"pendentes":N,"aprovadosMes":N,"rejeitadosMes":N}`.
func IndicadoresPedidosHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("IndicadoresPedidosHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		escopoTodos := r.URL.Query().Get("escopo") == "todos"
		ind, err := services.IndicadoresPedidos(db, empresa.ID, usuario.ID, usuario.Papel, escopoTodos)
		if err != nil {
			slog.Error("falha ao calcular indicadores de pedidos", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao calcular indicadores de pedidos")
			return
		}
		escreverJSON(w, http.StatusOK, ind)
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
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
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

		pedido, err := services.DecidirPedido(db, empresa.ID, r.PathValue("id"), usuario.ID, usuario.Papel, *req.Aprovar)
		switch {
		case err == nil:
			registro.Publish(empresa.ID, "pedidos", realtime.Evento{ID: pedido.ID, Change: pedido.Status})
			// Story 11.3: a decisão libera a reserva (e debita a parte aprovada).
			produtoIDs := make([]string, 0, len(pedido.Itens))
			for _, it := range pedido.Itens {
				produtoIDs = append(produtoIDs, it.ProdutoID)
			}
			for _, produtoID := range idsUnicos(produtoIDs) {
				registro.Publish(empresa.ID, "produtos", realtime.Evento{ID: produtoID, Change: "updated"})
			}
			escreverJSON(w, http.StatusOK, map[string]any{"pedido": pedido})
		case errors.Is(err, services.ErrPedidoNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "pedido não encontrado")
		case errors.Is(err, services.ErrPedidoNaoPendente):
			escreverErro(w, http.StatusConflict, "CONFLICT", "este pedido não está mais pendente")
		case errors.Is(err, services.ErrPedidoTipoNaoSuportado):
			// Story 20.1 (guard crítico, Spec Change Log): nenhuma aprovação/
			// rejeição real acontece para um Pedido de tipo != 'retirada' —
			// só a Story 20.3 implementa a decisão de verdade por tipo.
			escreverErro(w, http.StatusConflict, "CONFLICT", services.ErrPedidoTipoNaoSuportado.Error())
		default:
			slog.Error("falha ao decidir pedido", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao decidir pedido")
		}
	}
}

// decisaoLotePedidosRequest é o corpo aceito por POST
// /api/pedidos/decisao-lote (Story 20.3, Epic 20, spec-20-3). `Aprovar` é um
// ponteiro DE PROPÓSITO — mesmo molde de decisaoPedidoRequest acima: um
// corpo `{}`/`{"aprovar":null}` decodifica sem erro e, com um `bool` puro,
// viraria silenciosamente `aprovar=false` (rejeição involuntária do lote
// inteiro). Nil -> 400 VALIDATION_ERROR.
type decisaoLotePedidosRequest struct {
	PedidoIDs []string `json:"pedidoIds"`
	Aprovar   *bool    `json:"aprovar"`
}

// DecidirLotePedidosHandler expõe POST /api/pedidos/decisao-lote (Story
// 20.3, Epic 20, spec-20-3), registrado em newMux atrás de
// RequireAuth(db,jwtSecret)(RequireRole(services.PapelGestor)(...)) — nível
// de acesso MAIOR que o `almoxarife` de DecidirPedidoHandler (requisito
// explícito do usuário: "o gerente tem que aprovar"). RequireRole roda a
// CADA requisição, nunca cacheado — esta função não faz checagem de papel
// adicional.
//
// Lê `{"pedidoIds": [...], "aprovar": bool}` sob http.MaxBytesReader — corpo
// malformado OU sem a chave `aprovar` (nil) -> 400 VALIDATION_ERROR. Sucesso
// -> `200 {"pedidos": [PedidoDetalhe, ...]}` (um por id, na mesma ordem
// enviada), publicando `{"resource":"pedidos","id":<pedido>,"change":<novo
// status>}` por Pedido REALMENTE decidido agora nesta chamada (nunca para
// um Pedido pulado por já estar decidido — nenhuma mudança de estado
// aconteceu por esta requisição) + um evento `produtos` (`updated`) por
// Produto único entre todos os itens decididos agora — mesmo padrão de
// DecidirPedidoHandler.
func DecidirLotePedidosHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("DecidirLotePedidosHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req decisaoLotePedidosRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Revisão de código (findings do triplo-review): mensagem
			// DIFERENTE da de `aprovar` ausente logo abaixo — um corpo que
			// nem decodifica (JSON malformado) é uma causa distinta de um
			// corpo válido só sem a chave obrigatória; quem consome a API
			// precisa distinguir as duas.
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "corpo da requisição malformado")
			return
		}
		if req.Aprovar == nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "campo \"aprovar\" é obrigatório")
			return
		}

		pedidos, err := services.DecidirLotePedidos(db, empresa.ID, usuario.ID, req.PedidoIDs, *req.Aprovar)
		var erroValidacao *services.ErroPedidoValidacao
		switch {
		case err == nil:
			produtoIDs := make([]string, 0)
			for _, pedido := range pedidos {
				// Pedido pulado (já decidido antes desta chamada): o único
				// sinal disponível aqui é `decididoPor`/`decididoEm` já
				// preenchidos mesmo sem o cliente ter mandado nada — não dá
				// pra distinguir com certeza "decidido agora" de "já estava
				// decidido" só pela forma da resposta (de propósito, ver
				// services.lerPedidoDetalheTx). Publicar o evento mesmo
				// assim é inofensivo (idempotente — o canal só pede um
				// refetch) e mantém qualquer tela aberta do Pedido
				// atualizada, então este handler publica para TODOS os
				// Pedidos da resposta, não só os decididos agora.
				registro.Publish(empresa.ID, "pedidos", realtime.Evento{ID: pedido.ID, Change: pedido.Status})
				for _, it := range pedido.Itens {
					produtoIDs = append(produtoIDs, it.ProdutoID)
				}
			}
			for _, produtoID := range idsUnicos(produtoIDs) {
				registro.Publish(empresa.ID, "produtos", realtime.Evento{ID: produtoID, Change: "updated"})
			}
			escreverJSON(w, http.StatusOK, map[string]any{"pedidos": pedidos})
		case errors.Is(err, services.ErrPedidoNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "pedido não encontrado")
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		default:
			slog.Error("falha ao decidir lote de pedidos", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao decidir o lote de pedidos")
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
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		detalhe, err := services.BuscarPedidoProprio(db, empresa.ID, r.PathValue("id"), usuario.ID, usuario.Papel)
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

// BaixarReciboPedidoHandler expõe GET /api/pedidos/{id}/recibo (Story 7.6,
// spec-7-6), registrado em newMux atrás SÓ de RequireAuth — SEM RequireRole,
// molde EXATO de BuscarPedidoHandler: dono da sessão OU `almoxarife`+ baixa
// (AD-8, resolvido inteiramente em services.MontarReciboPedidoConteudo, que
// chama BuscarPedidoProprio). Pedido de outro usuário sem papel suficiente,
// id inexistente ou malformado colapsam no MESMO `404 NOT_FOUND`; Pedido
// ainda `pendente`/`rejeitado` -> `409 CONFLICT` (nenhuma retirada ocorreu,
// nenhum PDF gerado). Sucesso -> `200`, `Content-Type: application/pdf`,
// `Content-Disposition: attachment; filename="recibo-pedido-{id}.pdf"`, corpo
// = bytes do PDF — molde EXATO de ExportarCatalogoHandler
// (handlers/produtos.go).
func BaixarReciboPedidoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("BaixarReciboPedidoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}
		empresa, ok := empresaDaRequisicao(w, r)
		if !ok {
			return
		}

		id := r.PathValue("id")
		pdf, err := services.GerarReciboPedidoPDF(db, empresa.ID, id, usuario.ID, usuario.Papel)
		switch {
		case err == nil:
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", `attachment; filename="recibo-pedido-`+id+`.pdf"`)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(pdf)
		case errors.Is(err, services.ErrPedidoNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "pedido não encontrado")
		case errors.Is(err, services.ErrPedidoSemRecibo):
			escreverErro(w, http.StatusConflict, "CONFLICT", services.ErrPedidoSemRecibo.Error())
		default:
			slog.Error("falha ao gerar recibo do pedido", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao gerar recibo do pedido")
		}
	}
}

// idsUnicos remove ids repetidos preservando a ordem — o mesmo Produto em
// vários Estoques/itens de um Pedido publica um único evento `produtos`.
func idsUnicos(ids []string) []string {
	vistos := make(map[string]struct{}, len(ids))
	unicos := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := vistos[id]; ok {
			continue
		}
		vistos[id] = struct{}{}
		unicos = append(unicos, id)
	}
	return unicos
}
