---
title: 'Pedido de Transferência entre Filiais (solicitar)'
type: 'feature'
created: '2026-11-12'
status: 'done'
review_loop_iteration: 1
baseline_commit: '153074bc6f0900995391165db0e5bea9894f55d9'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-20-context.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O botão "Transferir" move saldo entre Estoques (potencialmente de Filiais diferentes) na hora, sem aprovação — arriscado para um processo que gera documento fiscal (NF de transferência, Ferreira Costa). Precisa existir um caminho de SOLICITAÇÃO, separado, que não mexa em saldo até um Gestor decidir (Story 20.3, fora de escopo aqui).

**Approach:** Novo tipo de Pedido ("transferencia"), ao lado do Pedido de retirada já existente (Epic 7): usuário escolhe Produto, Estoque origem, Estoque destino e quantidade numa tela nova e dedicada (envio direto de 1 item, SEM passar pelo carrinho multi-item) — cria `pedidos`/`pedido_itens`/`reservas_pedido_item` reaproveitando a MESMA reserva dura já usada pelo Pedido de retirada, nunca debitando nem creditando saldo físico.

## Boundaries & Constraints

**Always:**
- Reaproveitar `travarSaldoParesTx`/`saldoDisponivelParTx` (reservas.go) para a disponibilidade da ORIGEM, mesma regra de `ErroPedidoIndisponivel` do Pedido de retirada.
- Estoque origem ≠ destino (mesma regra de `RegistrarTransferencia`, movimentacoes.go:518-520); produto/estoque destino precisam existir na MESMA Empresa.
- `pedidos.tipo` default `'retirada'` para linhas já existentes — nenhuma migração de dado, só a coluna nova.
- Nunca chamar nem duplicar a lógica física de `RegistrarTransferencia` (débito/crédito FEFO) — isso é Story 20.3.
- `DecidirPedido` recusa decidir (aprovar OU rejeitar) qualquer Pedido com `tipo != 'retirada'` com um erro dedicado, ANTES de qualquer trava/débito — nenhum Pedido de Transferência pode ser aprovado pela Fila atual causando baixa física sem crédito no destino, até a Story 20.3 implementar a decisão real por tipo.
- `SubmeterPedidoTransferencia` recusa Produto inativo (reaproveitar `recusarProdutosInativosTx`, mesma regra do Pedido de retirada) ANTES de travar/reservar saldo.
- `produtoId`/`estoqueOrigemId`/`estoqueDestinoId` vazios (string em branco) são `ErroPedidoValidacao` (400), nunca colapsam em "disponibilidade insuficiente" (409).

**Ask First:** nenhuma — decisões de arquitetura já fechadas no contexto do épico (coluna `tipo` em `pedidos`, `estoque_destino_id` nullable em `pedido_itens`, envio direto sem carrinho).

**Never:** tocar `SubmeterPedido`/carrinho existentes (Epic 7) além de aceitar `tipo='retirada'` como já era. Nunca IMPLEMENTAR aprovação de verdade, débito/crédito ou Pré-Nota (Stories 20.2-20.5) — a única mudança permitida em `DecidirPedido` é um guard que RECUSA decidir um Pedido de tipo diferente de `'retirada'` (ver Always acima), nunca sua lógica de aprovação em si.

**Renegociação autônoma (2026-11-12, revisão da Story, ver Spec Change Log):** a versão anterior deste "Never" proibia qualquer toque em `DecidirPedido`. Dois revisores independentes confirmaram que isso deixava um buraco real: a Fila já existente, sem nenhuma mudança, aprova um Pedido de Transferência pendente debitando a origem por FEFO como baixa física — sem creditar o destino — perda de estoque de verdade. Resolvido autonomamente (sessão noturna, autorização do usuário para seguir sem parar) porque a única leitura sã é "nunca aceitar essa perda", nunca uma question de design em aberto — sinalizado aqui para revisão humana de qualquer forma.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Solicitação válida | Produto com 10 disponíveis na origem, pede 5 | Pedido `pendente` tipo=transferencia, reserva de 5 na origem | N/A |
| Disponível insuficiente | Pede mais que o disponível da origem | Nada gravado | `ErroPedidoIndisponivel` (409) |
| Origem == destino | Mesmo Estoque nos dois campos | Nada gravado | `ErroPedidoValidacao` (400) |
| Destino inexistente/outra Empresa | `estoqueDestinoID` inválido | Nada gravado | `ErroPedidoValidacao` (400) |

</frozen-after-approval>

## Code Map

- `backend/migrations/000056_add_tipo_e_destino_a_pedidos.up.sql` (novo) -- `ALTER TABLE pedidos ADD COLUMN tipo TEXT NOT NULL DEFAULT 'retirada' CHECK (tipo IN ('retirada','transferencia','baixa'))`; `ALTER TABLE pedido_itens ADD COLUMN estoque_destino_id UUID NULL`. `.down.sql` remove as duas colunas.
- `backend/services/pedidos.go` -- nova `SubmeterPedidoTransferencia(db, empresaID, usuarioID, solicitante, obraCentroCusto, observacao, produtoID, estoqueOrigemID, estoqueDestinoID string, quantidade float64) (Pedido, error)`, mesmo molde de `SubmeterPedidoComCentroCusto` (linhas 201-341): valida solicitante/obra, `travarSaldoParesTx` só do par origem, valida destino existente na Empresa (molde do guard em `movimentacoes.go:538-550`), `saldoDisponivelParTx` vs quantidade, insere 1 linha em cada tabela (`pedidos.tipo='transferencia'`, `pedido_itens.estoque_destino_id`, `reservas_pedido_item` só na origem).
- `backend/handlers/pedidos.go:56` (`SubmeterPedidoHandler`) -- molde para `SubmeterPedidoTransferenciaHandler` (mesmo padrão de decode/publish/mapeamento de erro).
- `backend/main.go:867-904` -- registrar `POST /e/{slug}/api/pedidos/transferencia` com `middleware.RequireAuth`, mesmo nível de acesso do Pedido comum (qualquer `usuario`+).
- `frontend/src/components/shell/nav-items.ts:68-72` -- novo item no grupo `'Pedidos'`: `{ id: 'pedido-transferencia', label: 'Pedido de Transferência entre Filiais', to: '/pedidos/transferencia', papelMinimo: 'usuario' }`.
- `frontend/src/pages/ProdutoDetalhePage.tsx` (diálogo "Transferir", ~linha 1306+) -- molde de UX a reaproveitar (Select de Estoque destino + Input de quantidade); a página nova precisa também de um seletor de Produto e de Estoque origem, por ser tela própria (não parte do detalhe de um Produto específico).
- `backend/services/pedidos.go` (`DecidirPedido`, em torno da linha ~851-900, ANTES de qualquer `travarSaldoParesTx`/trava) -- novo guard: `SELECT tipo FROM pedidos WHERE id = $1`; `tipo != 'retirada'` -> novo erro dedicado (`ErroPedidoTipoNaoSuportado` ou similar), mapeado para 409 CONFLICT no handler de decisão, mensagem citando que esse tipo de Pedido ainda não tem aprovação implementada.
- `backend/services/pedidos.go` (`recusarProdutosInativosTx`, já existe, usado por `SubmeterPedidoComCentroCusto`) -- reaproveitar tal qual dentro de `SubmeterPedidoTransferencia`, para o único Produto do pedido.
- `backend/handlers/pedidos_test.go` -- molde de `SubmeterPedidoHandler` (testes já existentes nesse arquivo) para `SubmeterPedidoTransferenciaHandler`.
- `frontend/src/components/pedidos/StatusPedidoBadge.test.tsx` (molde) -- usar como referência de estrutura de teste para o badge de tipo novo.

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/000056_*.up.sql`/`.down.sql` -- criar colunas `tipo`/`estoque_destino_id` -- base de dados da story
- [x] `backend/services/pedidos.go` -- `SubmeterPedidoTransferencia` -- lógica central, reaproveitando reserva/validação do Pedido de retirada; chama `recusarProdutosInativosTx`; rejeita produtoId/estoqueOrigemId/estoqueDestinoId vazios como `ErroPedidoValidacao` antes de tocar o banco
- [x] `backend/services/pedidos.go` (`DecidirPedido`) -- guard recusando decisão de Pedido com `tipo != 'retirada'` (ver Always) -- CRÍTICO, corrige o achado de revisão (perda de estoque real pela Fila atual)
- [x] `backend/services/pedidos_test.go` -- testes cobrindo as 4 linhas da I/O Matrix + concorrência + produto inativo rejeitado + `DecidirPedido` recusando um Pedido de Transferência pendente
- [x] `backend/handlers/pedidos.go` + `backend/main.go` -- rota `POST /api/pedidos/transferencia`
- [x] `backend/handlers/pedidos_test.go` -- testes HTTP do handler novo (201/400/409, publicação de eventos realtime), molde de `SubmeterPedidoHandler`
- [x] `frontend/src/pages/` (nova página) + `nav-items.ts` -- formulário de solicitação e item de menu; usar `<Input type="number">` nativo para quantidade (nunca parsing manual de string com vírgula/separador de milhar); resetar o Estoque destino selecionado se ele empatar com a origem escolhida depois; guardar contra `setState` após desmontar no fetch de Estoques (mesmo padrão já usado no efeito de busca de Produto); mostrar o nome da Filial ao lado do nome de cada Estoque nas duas listas
- [x] `frontend/src/components/pedidos/TipoPedidoBadge.test.tsx` (novo) + fixtures com `tipo: 'transferencia'` em `FilaPedidosSection.test.tsx`/`MeusPedidosSection.test.tsx` -- cobrir o badge novo nos dois pontos de uso

**Acceptance Criteria:**
- Given um Pedido de Transferência pendente, when listado em Meus Pedidos/Fila, then aparece com um rótulo que o distingue do Pedido de retirada comum (ex. coluna/badge "Transferência")
- Given um Pedido de Transferência pendente, when um Gestor tenta aprovar ou rejeitar pela Fila/`DecidirPedido` atual, then a decisão é recusada (nenhum saldo físico é tocado em nenhum dos dois Estoques) — só a Story 20.3 habilita decidir este tipo
- Given um Produto inativo, when o usuário tenta solicitar uma Transferência dele, then a solicitação é recusada, mesma mensagem do Pedido de retirada
- Given `go build ./...` e `npm run build`, when rodados após a story, then terminam sem erro

## Spec Change Log

- **2026-11-12, revisão (loop 1), bad_spec:** 2 revisores independentes (verification-gap, edge-case-hunter) confirmaram que `DecidirPedido`/a Fila, SEM NENHUMA mudança, aprovariam um Pedido de Transferência pendente debitando a origem por FEFO como baixa física (`consumirFEFOTx` + Movimentação `"baixa"`) sem creditar o destino nenhum — perda de estoque real na primeira vez que alguém clicasse "Aprovar". Causa raiz: o "Never" original ("nunca tocar `DecidirPedido`") não previu que o fluxo de aprovação JÁ EXISTENTE seria imediatamente alcançável pelo novo `tipo`. Emendado: `DecidirPedido` ganha um guard (ver Always) que recusa decidir qualquer Pedido `tipo != 'retirada'`, sem implementar nenhuma lógica de aprovação nova — consistente com o restante do "Never" (só Story 20.3 implementa decisão de verdade). Produto inativo não bloqueado (blind-hunter + edge-case-hunter) e falta de validação de campos vazios (blind-hunter) também corrigidos nesta mesma rodada, mesma causa raiz de categoria (Tasks incompletas). **KEEP:** todo o desenho original (reserva dura, só a origem, nunca debitar/creditar na solicitação, envio direto sem carrinho) se manteve correto e sobrevive tal qual.
- **2026-11-12, revisão (loop 1), patch (mesma rodada):** testes HTTP do handler ausentes, teste do `TipoPedidoBadge` ausente nos 2 pontos de uso, `estoqueDestinoId` não resetado na UI quando empata com a origem escolhida depois, risco de `setState` após desmontar no fetch de Estoques, parsing manual de quantidade com vírgula/separador de milhar (trocado por `<input type="number">` nativo), Filial não exibida nas opções de Estoque — todos de baixo risco, incluídos na mesma rodada de re-derivação para não abrir um novo loop só para eles.

## Verification

**Commands:**
- `cd backend && go build ./... && go test ./services/... -run 'Transferencia|DecidirPedido'` -- expected: build limpo, testes novos e os já existentes de `DecidirPedido` passando
- `cd backend && go test -p 1 ./...` -- expected: suíte completa do backend limpa (DATABASE_URL apontando pro Postgres de teste, migrations aplicadas)
- `cd frontend && npm run build && npx vitest run` -- expected: build limpo, suíte completa do frontend limpa

## Suggested Review Order

**Solicitação (o caminho novo)**

- Ponto de entrada: valida, trava só a origem, nunca debita/credita saldo
  [`pedidos.go:446`](../../backend/services/pedidos.go#L446)

- Schema novo: `tipo` fecha o conjunto de Pedidos suportados, `estoque_destino_id` só para transferência
  [`000056_add_tipo_e_destino_a_pedidos.up.sql:1`](../../backend/migrations/000056_add_tipo_e_destino_a_pedidos.up.sql#L1)

- Rota HTTP dedicada, mesmo nível de acesso do Pedido comum
  [`pedidos.go:138`](../../backend/handlers/pedidos.go#L138)

**Guard crítico (achado da revisão — perda de estoque real evitada)**

- `DecidirPedido` recusa decidir qualquer Pedido `tipo != 'retirada'`, antes de qualquer trava
  [`pedidos.go:919`](../../backend/services/pedidos.go#L919)

- Sentinela do guard, mapeado para 409 no handler de decisão
  [`pedidos.go:133`](../../backend/services/pedidos.go#L133)

- Fila esconde Aprovar/Rejeitar para `tipo != 'retirada'`, evitando o usuário bater no guard
  [`FilaPedidosSection.tsx:492`](../../frontend/src/components/pedidos/FilaPedidosSection.tsx#L492)

**Frontend novo**

- Tela dedicada de solicitação (produto, origem, destino, quantidade)
  [`PedidoTransferenciaPage.tsx:77`](../../frontend/src/pages/PedidoTransferenciaPage.tsx#L77)

- Badge que distingue o tipo do Pedido nas listagens
  [`TipoPedidoBadge.tsx:46`](../../frontend/src/components/pedidos/TipoPedidoBadge.tsx#L46)
