---
title: 'Pedido de Baixa de Estoque (solicitar)'
type: 'feature'
created: '2026-11-13'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'f032770a12632881250f8f782a09338ecd56c08a'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-20-context.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** "Registrar Baixa" debita o estoque físico na hora, sem aprovação — o usuário foi explícito: "O gerente tem que aprovar o pedido de baixa do estoque. Não pode ser solto como está."

**Approach:** Novo tipo de Pedido (`'baixa'`), irmão do Pedido de Transferência (Story 20.1) e do Pedido de retirada (Epic 7): usuário escolhe Produto, Estoque e quantidade numa tela dedicada (envio direto de 1 item, sem carrinho) — cria `pedidos`/`pedido_itens`/`reservas_pedido_item` reaproveitando a MESMA reserva dura, nunca debitando saldo físico. `DecidirPedido` já recusa (genericamente, Story 20.1) decidir qualquer `tipo != 'retirada'` — este tipo novo já nasce protegido, sem código adicional nesta story.

## Boundaries & Constraints

**Always:**
- Reaproveitar `travarSaldoParesTx`/`saldoDisponivelParTx` para a disponibilidade do par, mesma regra de `ErroPedidoIndisponivel` do Pedido de retirada.
- Reaproveitar `recusarProdutosInativosTx` para o Produto, ANTES de travar/reservar saldo (mesmo molde de `SubmeterPedidoTransferencia`, pedidos.go:506).
- `produtoId`/`estoqueId` vazios são `ErroPedidoValidacao` (400), nunca colapsam em "disponibilidade insuficiente".
- O guard de `DecidirPedido` (Story 20.1, pedidos.go:919, `tipo != 'retirada'`) já cobre `'baixa'` sem nenhuma mudança — confirmar com um teste, nunca duplicar a lógica.

**Ask First:** nenhuma — mesmo padrão de arquitetura já validado e revisado na Story 20.1, só sem Estoque destino.

**Never:** tocar `RegistrarBaixa` (movimentacoes.go:381, ação direta continua existindo sem aprovação), `SubmeterPedido`/carrinho (Epic 7) ou `SubmeterPedidoTransferencia`/sua tela (Story 20.1). Nunca implementar aprovação de verdade, débito ou Pré-Nota (Stories 20.3-20.5) — `DecidirPedido` continua só recusando.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Solicitação válida | Produto com 10 disponíveis, pede 5 | Pedido `pendente` tipo=baixa, reserva de 5 | N/A |
| Disponível insuficiente | Pede mais que o disponível | Nada gravado | `ErroPedidoIndisponivel` (409) |
| Produto inativo | Produto inativado | Nada gravado | Mesmo erro de produto inativo do Pedido de retirada |
| Campos vazios | produtoId/estoqueId em branco | Nada gravado | `ErroPedidoValidacao` (400) |

</frozen-after-approval>

## Code Map

- `backend/services/pedidos.go:446` (`SubmeterPedidoTransferencia`) -- molde EXATO a copiar para `SubmeterPedidoBaixa(db, empresaID, usuarioID, solicitante, obraCentroCusto, observacao, produtoID, estoqueID string, quantidade float64) (Pedido, error)` -- mesma sequência de validação (campos obrigatórios, quantidade, produto+categoria, `recusarProdutosInativosTx`, `travarSaldoParesTx`, `saldoDisponivelParTx`), SEM o bloco de Estoque destino (linhas 510-536 do molde) nem `estoque_destino_id` no INSERT.
- `backend/services/pedidos.go:919` (`DecidirPedido`, guard da Story 20.1) -- NÃO precisa de nenhuma mudança: já recusa `tipo != 'retirada'` genericamente; só precisa de um teste novo provando que cobre `'baixa'` também.
- `backend/handlers/pedidos.go:138` (`SubmeterPedidoTransferenciaHandler`) -- molde para `SubmeterPedidoBaixaHandler` (mesmo padrão de decode/publish/mapeamento de erro, sem `estoqueDestinoId` no corpo).
- `backend/main.go` (rota `POST /e/{slug}/api/pedidos/transferencia`) -- molde para `POST /e/{slug}/api/pedidos/baixa`.
- `frontend/src/pages/PedidoTransferenciaPage.tsx:77` -- molde para `PedidoBaixaPage.tsx` (mesmo formulário, sem o segundo Select de Estoque destino).
- `frontend/src/components/shell/nav-items.ts` -- novo item no grupo `'Pedidos'`: `{ id: 'pedido-baixa', label: 'Pedido de Baixa de Estoque', to: '/pedidos/baixa', papelMinimo: 'usuario' }`.
- `frontend/src/components/pedidos/TipoPedidoBadge.tsx` -- já trata `'baixa'` (Story 20.1 já previu os 3 valores); confirmar que o rótulo exibido faz sentido, sem precisar de mudança de código.

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/pedidos.go` -- `SubmeterPedidoBaixa` -- lógica central, molde de `SubmeterPedidoTransferencia` sem destino
- [x] `backend/services/pedidos_test.go` -- testes cobrindo as 4 linhas da I/O Matrix + concorrência (mesmo par disputado) + `DecidirPedido` recusando um Pedido de Baixa pendente (prova que o guard genérico da Story 20.1 cobre este tipo)
- [x] `backend/handlers/pedidos.go` + `backend/main.go` -- rota `POST /api/pedidos/baixa`
- [x] `backend/handlers/pedidos_test.go` -- testes HTTP do handler novo (201/400/409), incluindo o campo `tipo` no JSON de `GET /api/pedidos`/`GET /api/pedidos/{id}` (não repetir a lacuna da revisão da Story 20.1)
- [x] `frontend/src/pages/PedidoBaixaPage.tsx` (nova) + `.test.tsx` + `nav-items.ts` -- formulário de solicitação e item de menu
- [x] `frontend/src/components/pedidos/FilaPedidosSection.tsx` -- confirmar (sem mudança de código esperada) que o guard de esconder Aprovar/Rejeitar para `tipo != 'retirada'` (Story 20.1) já cobre `'baixa'`; se não cobrir, corrigir

**Acceptance Criteria:**
- Given um Pedido de Baixa pendente, when listado em Meus Pedidos/Fila, then aparece com o rótulo "Baixa" (badge já existente da Story 20.1)
- Given um Pedido de Baixa pendente, when um Gestor tenta decidir pela Fila/`DecidirPedido` atual, then a decisão é recusada, nenhum saldo físico é tocado
- Given `go build ./...`, `go test -p 1 ./...`, `npm run build` e `npx vitest run`, when rodados após a story, then terminam sem erro

## Verification

**Commands:**
- `cd backend && go build ./... && go test -p 1 ./...` -- expected: build limpo, suíte completa do backend passando
- `cd frontend && npm run build && npx vitest run` -- expected: build limpo, suíte completa do frontend passando

## Spec Change Log

- **2026-11-13, revisão (patches, sem loopback):** 3 revisores confirmaram, dois deles independentemente, que `DecidirPedido` (linha ~1257) ainda lia `obra_centro_custo` sem `COALESCE` — inofensivo hoje (o guard de tipo recusa `'baixa'` antes de alcançar essa linha), mas armadilha pra Story 20.3. Corrigido por precaução, mesmo padrão das 3 leituras já protegidas. Também confirmado por 2 revisores: a migration 000057 soltava o `NOT NULL` de `obra_centro_custo` sem nenhum `CHECK` garantindo que só `tipo='baixa'` pode gravar NULL — adicionado `CHECK (tipo = 'baixa' OR obra_centro_custo IS NOT NULL)` na mesma migration. Mais patches menores aplicados na mesma rodada: tipagem opcional de `obraCentroCusto` no frontend, guard contra `fetch`/`res.json()` rejeitando sem mensagem amigável, guard de `setState` pós-desmonte em `PedidoBaixaPage`, limpeza de teste com campo obsoleto, teste de payload malformado, teste de produto/estoque de outra empresa, e o teste que faltou (`TestListarPedidosFila_PedidoDeBaixaSemObraCentroCusto`) escrito depois, diretamente, por interrupção de limite de taxa no meio da rodada anterior.

## Suggested Review Order

**Solicitação (o caminho novo)**

- Ponto de entrada: molde de `SubmeterPedidoTransferencia`, sem Estoque destino
  [`pedidos.go:636`](../../backend/services/pedidos.go#L636)

- Migration: solta o `NOT NULL` de `obra_centro_custo` só pra `tipo='baixa'`, com CHECK garantindo isso no banco
  [`000057_make_obra_centro_custo_opcional_pedidos.up.sql:1`](../../backend/migrations/000057_make_obra_centro_custo_opcional_pedidos.up.sql#L1)

- Rota HTTP dedicada
  [`pedidos.go:219`](../../backend/handlers/pedidos.go#L219)

**Guard do tipo (já existia desde a Story 20.1 — confirma que cobre `'baixa'` também)**

- `DecidirPedido` recusa qualquer `tipo != 'retirada'`, `'baixa'` incluso, sem nenhuma mudança de código
  [`pedidos.go:1092`](../../backend/services/pedidos.go#L1092)

**Frontend novo**

- Tela dedicada de solicitação (sem Solicitante, Obra opcional, mostra saldo disponível)
  [`PedidoBaixaPage.tsx:94`](../../frontend/src/pages/PedidoBaixaPage.tsx#L94)

- Item de menu novo
  [`nav-items.ts:79`](../../frontend/src/components/shell/nav-items.ts#L79)
