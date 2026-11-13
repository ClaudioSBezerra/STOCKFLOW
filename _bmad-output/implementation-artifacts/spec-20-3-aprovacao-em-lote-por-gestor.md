---
title: 'Aprovação em lote por Gestor'
type: 'feature'
created: '2026-11-13'
status: 'done'
review_loop_iteration: 0
baseline_commit: '6f5d8b2e3e5d1c3ee35cb418d25c39ea69946d2b'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-20-context.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Pedidos de Transferência (Story 20.1) e de Baixa (Story 20.2) ficam `pendente` pra sempre — `DecidirPedido` recusa (409) qualquer `tipo != 'retirada'` de propósito, até esta story existir pra decidir de verdade.

**Approach:** Novo caminho de decisão, PARALELO a `DecidirPedido` (que continua intocado, só pra `retirada`, só `almoxarife`+): um Gestor (`gestor`/`adm`) seleciona vários Pedidos pendentes de Transferência e/ou Baixa numa tela dedicada e aprova/rejeita o lote inteiro numa chamada só. Aprovar reexecuta, por pedido, a MESMA mecânica física já usada pelos botões diretos — `consumirFEFOTx`+`creditarLoteDestinoTx` (Transferência) ou só `consumirFEFOTx` (Baixa), ambas já extraídas como funções internas por `RegistrarTransferencia`/`RegistrarBaixa` (movimentacoes.go) — nunca duplicada. Pré-Nota/docnum (Story 20.4) fica fora: esta story só decide.

## Boundaries & Constraints

**Always:**
- Novo endpoint `POST /api/pedidos/decisao-lote` atrás de `RequireRole(PapelGestor)` — nível de acesso MAIOR que o `almoxarife` do `DecidirPedido`/`decisao` existente (requisito explícito do usuário: "o gerente tem que aprovar").
- O novo serviço só aceita pedidos com `tipo IN ('transferencia', 'baixa')` — qualquer id da lista com `tipo = 'retirada'` (ou ausente/legado) rejeita A LISTA INTEIRA com `ErroPedidoValidacao` (400), nada gravado; `DecidirPedido`/retirada continua exclusivamente pelo caminho de hoje.
- UMA transação cobre o lote inteiro: todos os pares (produto, estoque) de TODOS os pedidos do lote entram juntos em `travarSaldoParesTx` (AD-10, ordenado), nunca pedido-a-pedido com commits parciais.
- Dentro dessa transação, cada ITEM revalida reserva x saldo físico e pode ser parcialmente aprovado (mesmo comportamento já existente do Pedido de retirada) — isso nunca aborta o lote nem os demais pedidos.
- Um pedido da lista que não está mais `pendente` no momento do UPDATE (já decidido por outra requisição concorrente) é PULADO (reportado como já decidido), sem abortar os demais pedidos do lote — mesma filosofia de "falha isolada não trava o resto" já aplicada a nível de item.
- RBAC é responsabilidade do servidor, nunca só da UI: `RequireRole` roda a cada requisição (mesmo padrão do `DecidirPedido` existente).
- Isolamento por Empresa (AD-20) em toda query nova, mesmo padrão já usado em todo o restante do código de Pedidos.

**Ask First:** nenhuma — RBAC (gestor+), reuso das funções Tx internas e o tratamento pedido-pulado-não-aborta-lote já cobrem as decisões de maior risco; qualquer achado novo de revisão vira loopback normal.

**Never:** tocar `DecidirPedido`/`POST /api/pedidos/{id}/decisao` (continua intocado, só `retirada`, só `almoxarife`+), `RegistrarTransferencia`/`RegistrarBaixa`/seus botões diretos (continuam sem gate de aprovação, FR62), implementar Pré-Nota/docnum ou documento impresso (Stories 20.4/20.5 — fora de escopo, mesmo que o desenho do lote precise, no futuro, ganhar um `docnum`).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Lote válido misto | 2 Transferência + 1 Baixa, todos pendentes, saldo suficiente | Todos `aprovado`, origem debitada/destino creditado (Transferência) ou só debitada (Baixa) por FEFO | N/A |
| Saldo caiu entre envio e aprovação | 1 item de 1 pedido do lote com saldo físico < reservado | Esse item (e só esse) sai `parcialmente_aprovado`; resto do lote segue normal | N/A |
| Pedido já decidido por outra requisição | 1 dos ids do lote não está mais `pendente` | Esse pedido é pulado (reportado), resto do lote decide normalmente | N/A (não é erro de lote) |
| Lote com `retirada` misturado | 1 dos ids é `tipo='retirada'` | Lote inteiro rejeitado, nada gravado | `ErroPedidoValidacao` (400) |
| Lote vazio | `pedidoIds: []` | Nada gravado | `ErroPedidoValidacao` (400) |
| Papel abaixo de gestor | `almoxarife` chama o endpoint | Nada gravado | 403 (servidor, mesmo que a UI esconda o botão) |

</frozen-after-approval>

## Code Map

- `backend/services/movimentacoes.go:347,462,499` (`inserirMovimentacaoConsumoTx`, `creditarLoteDestinoTx`, dentro de `RegistrarTransferencia`) e `services/fefo.go:98` (`consumirFEFOTx`) -- funções Tx internas já existentes, a REUSAR tal qual (nunca duplicar a mecânica FEFO/crédito).
- `backend/services/pedidos.go:1091-1320` (`DecidirPedido`) -- molde EXATO de estrutura a copiar (snapshot de itens, `travarSaldoParesTx`, revalidação item-a-item, `liberarReservasPedidoTx`, `RETURNING` com `COALESCE(obra_centro_custo,'')`) pra uma nova função `DecidirLotePedidos(db *sql.DB, empresaID, decisorID string, pedidoIDs []string, aprovar bool) ([]PedidoDetalhe, error)` -- iterando pedidos (branch por `tipo`: `transferencia` chama `creditarLoteDestinoTx` + Movimentação `"transferencia"`; `baixa` só Movimentação `"baixa"`), rejeitando de cara (ANTES de `tx.Begin()`) qualquer `tipo='retirada'` na lista.
- `backend/handlers/pedidos.go:373` (`DecidirPedidoHandler`) -- molde pro novo `DecidirLotePedidosHandler` (decode `{pedidoIds []string, aprovar bool}`, mapeamento de erro: `ErroPedidoValidacao`->400, demais->500/409 conforme já mapeado).
- `backend/main.go:921` (rota `POST /api/pedidos/{id}/decisao`, `RequireRole(PapelAlmoxarife)`) -- molde pra `POST /api/pedidos/decisao-lote`, trocando pra `RequireRole(services.PapelGestor)`.
- `backend/main.go:896` (`GET /api/pedidos`, só `RequireAuth`, escopo por papel resolvido no service) -- REUSAR tal qual pra listar os pendentes de Transferência/Baixa na tela nova (gestor já tem rank >= almoxarife, nenhuma mudança de backend necessária pra listagem).
- `frontend/src/components/pedidos/FilaPedidosSection.tsx:484-514` -- o texto "ainda não tem aprovação implementada nesta versão" (pendente, `tipo != 'retirada'`) muda pra apontar pro novo caminho ("Use Aprovação em lote"); NÃO adicionar botões individuais aqui.
- `frontend/src/lib/pedidos.ts:250` (`decidirPedido`) -- molde pra `decidirLotePedidos(pedidoIds: string[], aprovar: boolean): Promise<PedidoDetalhe[]>`.
- `frontend/src/components/shell/nav-items.ts:84` (item `fila-aprovacao`, `papelMinimo: 'almoxarife'`) -- novo item irmão `{ id: 'aprovacao-lote', label: 'Aprovação em lote', to: '/pedidos/aprovacao-lote', papelMinimo: 'gestor' }` no mesmo grupo `'Pedidos'`.
- `frontend/src/pages/PedidoBaixaPage.tsx` / `PedidoTransferenciaPage.tsx` -- padrão de página a seguir (fetch, loading/erro, toda a UI já usa os mesmos componentes de Button/Dialog) pra nova `AprovacaoLotePage.tsx`: lista pendentes (filtrado client-side de `GET /api/pedidos?status=pendente` por `tipo IN (transferencia, baixa)`), checkboxes + ação em lote (Aprovar/Rejeitar selecionados) com `ConfirmDialog` próprio (molde de `FilaPedidosSection.tsx`).

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/pedidos.go` -- `DecidirLotePedidos` -- decisão em lote, reaproveitando as funções Tx de `movimentacoes.go`/`fefo.go`
- [x] `backend/services/pedidos_test.go` -- testes cobrindo as 6 linhas da I/O Matrix + concorrência (2 decisões simultâneas sobre o mesmo pedido do lote) + prova de que `DecidirPedido` (retirada) continua intocado
- [x] `backend/handlers/pedidos.go` + `backend/main.go` -- rota `POST /api/pedidos/decisao-lote`, `RequireRole(PapelGestor)`
- [x] `backend/handlers/pedidos_test.go` -- testes HTTP (200/400/403) do handler novo
- [x] `frontend/src/lib/pedidos.ts` (+ `.test.ts`) -- `decidirLotePedidos`
- [x] `frontend/src/pages/AprovacaoLotePage.tsx` (nova) + `.test.tsx` -- lista + seleção múltipla + ação em lote
- [x] `frontend/src/components/shell/nav-items.ts` (+ `.test.ts`) -- item `aprovacao-lote`, `papelMinimo: 'gestor'`
- [x] `frontend/src/components/pedidos/FilaPedidosSection.tsx` (+ `.test.tsx`) -- atualizar o texto do estado "sem aprovação" pra apontar pro novo caminho

**Acceptance Criteria:**
- Given vários Pedidos de Transferência e/ou Baixa pendentes, when um Gestor abre a Aprovação em lote, then vê todos os pendentes dos dois tipos com Solicitante/Produto/quantidade/Estoque(s)/data
- Given uma seleção mista (Transferência + Baixa), when o Gestor aprova o lote, then cada um executa a mecânica física correta e nenhum debita/credita duas vezes
- Given um papel abaixo de `gestor`, when tenta chamar `POST /api/pedidos/decisao-lote` direto, then 403
- Given os botões diretos "Transferir"/"Registrar Baixa", when esta story conclui, then continuam funcionando exatamente como hoje (FR62)
- Given `go build ./...`, `go test -p 1 ./...`, `npm run build` e `npx vitest run`, when rodados após a story, then terminam sem erro

## Spec Change Log

- 2026-11-13: Revisão em triplo (blind-hunter, edge-case-hunter, verification-gap) encontrou 7 achados reais, todos classificados "patch" (sem intent_gap/bad_spec — frozen-after-approval intocado). Todos corrigidos nesta mesma rodada:
  1. `decidirUmPedidoDoLoteTx` (pedidos.go): `creditarLoteDestinoTx` usava `it.EstoqueDestinoID.String` sem checar `.Valid` — agora falha alto com erro interno se um item `tipo=transferencia` tiver `estoque_destino_id` NULL (invariante violada), em vez de creditar com string vazia.
  2. `main_test.go`: novo `TestNewMux_PedidosDecisaoLoteRotaCarregaRequireRole` — despacha pelo `newMux` REAL (não um mux de teste à mão), provando que `POST /api/pedidos/decisao-lote` está de fato atrás de `RequireRole(PapelGestor)` em produção.
  3. `handlers/pedidos_test.go`: novo `TestDecidirLotePedidosHandler_PublicaEventoNoSucesso` — assina o registry e prova 1 evento `pedidos` por pedido da resposta (inclusive o "pulado") + eventos `produtos` deduplicados.
  4. `services/pedidos_test.go`: `TestDecidirLotePedidos_PedidoJaDecidido_PuladoSemAbortarLote` agora decide o pedido "já decidido" de verdade (não um UPDATE sintético) e assere que o relato do lote (Itens, DecididoPor, DecididoEm, CentroCustoNome) bate byte a byte com o ground truth da decisão real.
  5. `services/pedidos_test.go`: novo `TestDecidirLotePedidos_IdInexistenteOuDeOutraEmpresa` — cobre id inexistente/malformado/de outra Empresa dentro de `pedidoIds` -> `ErrPedidoNaoEncontrado`.
  6. `frontend/src/pages/AprovacaoLotePage.tsx`: falha de `buscarPedido` por linha agora é rastreada e exibida (nunca mais um `.catch(() => null)` silencioso) — checkbox da linha com erro fica desabilitado e nunca selecionável, e os botões "Aprovar/Rejeitar selecionados" ficam desabilitados enquanto qualquer linha SELECIONADA ainda não terminou de carregar (sucesso ou erro).
  7. `handlers/pedidos.go`: `DecidirLotePedidosHandler` agora devolve mensagens distintas para JSON malformado ("corpo da requisição malformado") vs. `aprovar` ausente/nulo ("campo \"aprovar\" é obrigatório"), em vez do mesmo "payload inválido" genérico para as duas causas.

## Verification

**Commands:**
- `cd backend && go build ./... && go test ./services/... -run 'Lote|DecidirPedido'` -- expected: build limpo, testes novos e os já existentes de `DecidirPedido` (retirada) passando intocados
- `cd backend && go test -p 1 ./...` -- expected: suíte completa do backend limpa
- `cd frontend && npm run build && npx vitest run` -- expected: build limpo, suíte completa do frontend limpa

## Suggested Review Order

**Decisão em lote (o caminho novo)**

- Ponto de entrada: valida tipo de TODOS os ids antes de abrir transação, deduplica, ordena pra evitar deadlock entre lotes concorrentes
  [`pedidos.go:1401`](../../backend/services/pedidos.go#L1401)

- Decide UM pedido dentro da transação compartilhada do lote — trava a própria linha, revalida item a item, credita destino só se `estoque_destino_id` for válido (achado da revisão)
  [`pedidos.go:1543`](../../backend/services/pedidos.go#L1543)

- Relê um pedido "pulado" (já decidido por requisição concorrente) com a MESMA forma de um decidido agora — nunca distinguível pelo shape da resposta
  [`pedidos.go:1741`](../../backend/services/pedidos.go#L1741)

- Rota HTTP dedicada: publica evento realtime por pedido (inclusive pulados), mapeia erro de tipo pra 400
  [`handlers/pedidos.go:454`](../../backend/handlers/pedidos.go#L454)

**Guard de RBAC (gestor+, mais restrito que o `DecidirPedido` existente)**

- Rota atrás de `RequireRole(PapelGestor)` — nível maior que o `almoxarife` do `/decisao` individual
  [`main.go:933`](../../backend/main.go#L933)

**Frontend novo**

- Tela de seleção múltipla: busca detalhe por pedido, rastreia falha por linha (achado da revisão — nunca aprovar sem ter visto o conteúdo)
  [`AprovacaoLotePage.tsx:71`](../../frontend/src/pages/AprovacaoLotePage.tsx#L71)

- Gate client-side `gestor`+ (servidor continua a autoridade real)
  [`AprovacaoLotePage.tsx:419`](../../frontend/src/pages/AprovacaoLotePage.tsx#L419)

- Chamada da API nova
  [`lib/pedidos.ts:279`](../../frontend/src/lib/pedidos.ts#L279)

- Item de menu novo, `papelMinimo: 'gestor'`
  [`nav-items.ts:86`](../../frontend/src/components/shell/nav-items.ts#L86)
