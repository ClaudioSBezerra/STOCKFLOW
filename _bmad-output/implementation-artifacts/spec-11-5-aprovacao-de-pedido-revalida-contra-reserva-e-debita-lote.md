---
title: 'Story 11.5: Aprovação de Pedido revalida contra reserva e debita Lote'
type: 'feature'
created: '2026-09-21'
baseline_revision: '3d0c3472a16e572f270726864e8f03e7f1f4e1cb'
status: done
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** `DecidirPedido` (aprovar) ainda revalida cada item contra o saldo físico de `produto_estoque` e o debita direto dali (Story 7.5). Ignora `lotes` (Story 11.1), não usa a reserva do próprio Pedido (Story 11.3) e não segue FEFO (Story 11.4): saldo que só existe em Lote não é aprovável (FR-25, AD-24, AD-25).

**Approach:** Aprovar trava os pares de todos os itens na ordem canônica (`travarSaldoParesTx`), revalida cada item contra a reserva do PRÓPRIO Pedido (limitada ao saldo físico) e debita por `consumirFEFOTx`, gerando as Movimentações por fonte na mesma transação que libera as reservas e registra a decisão.

## Boundaries & Constraints

**Always:**
- Travar com `travarSaldoParesTx` o conjunto COMPLETO de pares dos itens (ele mesmo ordena por `(produto_id, estoque_id)`) ANTES de ler reserva/saldo e de qualquer escrita; substitui o `FOR UPDATE` de `produto_estoque` item a item.
- Por item: `quantidadeAprovada = min(quantidade solicitada, reserva do Pedido para o par, saldo físico do par)`, arredondada a 3 casas (`arredondar3`), nunca negativa. Reserva do Pedido = linha de `reservas_pedido_item` `(pedido_id, produto_id, estoque_id, empresa_id)`, ausente = 0. Saldo físico = `produto_estoque` + `lotes` do par (novo helper em `reservas.go`, sem reservas subtraídas). NÃO comparar com o saldo livre: reserva de outros Pedidos nunca reduz o aprovável.
- Em condição normal (reserva íntegra) `quantidadeAprovada == quantidade`; só um bug de reserva (reserva ausente/menor, saldo físico abaixo da reserva) reduz o item. Nesse caso mantém-se o comportamento do Epic 7: status `parcialmente_aprovado` e `quantidadeAprovada` gravada e devolvida item a item (a lista exata do que ficou com problema é `quantidadeAprovada < quantidade`), sem sucesso parcial silencioso e sem contrato JSON novo.
- Se `quantidadeAprovada > 0`: `consumirFEFOTx` (FEFO único da 11.4) + 1 Movimentação `tipo='baixa'` por fonte consumida via `inserirMovimentacaoConsumoTx` (`lote_id` NULL para o legado, `usuario_id` = DECISOR, origem = Estoque do item), na MESMA transação. `quantidade_aprovada` do item é sempre gravada, mesmo 0.
- Liberação das reservas (`liberarReservasPedidoTx`), decisão (`UPDATE` guardado) e débito continuam atômicos numa transação; rollback desfaz tudo.
- O papel do decisor continua sendo revalidado na submissão pelo handler (comportamento existente, preservado). Rejeição não trava nada, só zera itens e libera reservas (inalterado).
- Assinatura, resposta e códigos HTTP de `DecidirPedido` inalterados. Nenhuma releitura pós-commit.

**Block If:** Nenhuma decisão bloqueante identificada.

**Never:** Não alterar Envio/Reserva (11.3), Baixa/Transferência (11.4), Cadastro (11.6), a migração 11.2 nem o schema; sem migration; não expor Lote na API; não materializar saldo; não criar segunda implementação de FEFO; não tocar `sprint-status.yaml`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Aprovação normal | Pedido reservado 4, físico 10 (legado) | `aprovado`, físico 6, reserva apagada, 1 Movimentação | — |
| Saldo só em Lotes | Reserva 5, 2 Lotes (val. 08/2026 qtd 3, val. 10/2026 qtd 4) | FEFO: 3 do primeiro, 2 do segundo; 2 Movimentações com `lote_id` | — |
| Outro Pedido reservou o resto | Físico 10, Pedido A reservou 4, Pedido B reservou 6 | A aprova 4 (não vira parcial por o livre ser 0) | — |
| Legado + Lotes | `produto_estoque` 3, Lote datado 2, Lote sem val. 4; aprovar 6 | datado 2, legado 3, sem val. 1 | — |
| Bug de reserva: sem reserva | Item sem linha em `reservas_pedido_item` | `quantidadeAprovada=0`, sem Movimentação, `parcialmente_aprovado` | — |
| Bug de reserva: físico < reserva | Reserva 10, físico 4 | aprova 4, `parcialmente_aprovado` | — |
| Rejeição | Pedido pendente | reservas apagadas, nada debitado | — |
| Decisões concorrentes | Aprovar e rejeitar/aprovar do mesmo Pedido | só a primeira vence | `ErrPedidoNaoPendente` |
| Locks opostos | Dois Pedidos com os mesmos pares em ordens opostas | sem deadlock | — |

</intent-contract>

## Code Map

- `backend/services/pedidos.go:570-` (`DecidirPedido`) -- ramo `aprovar`: hoje `selectDisponivel FOR UPDATE` + `UPDATE produto_estoque` + INSERT de `baixa` sem `lote_id`; reescrever com os helpers abaixo e atualizar o docstring. `registrarDecisao`/`liberarReservasPedidoTx` e o ramo de rejeição ficam como estão.
- `backend/services/reservas.go` -- `travarSaldoParesTx` e `liberarReservasPedidoTx` (reusar); acrescentar `saldoFisicoParTx` (produto_estoque + lotes, escopo Empresa) ao lado de `saldoDisponivelParTx`.
- `backend/services/fefo.go` -- `consumirFEFOTx` (pré-condição: par travado e quantidade ≤ físico validado).
- `backend/services/movimentacoes.go:255` -- `inserirMovimentacaoConsumoTx` (Movimentação por fonte, com `lote_id`).
- `backend/services/pedidos_test.go:843-1350` -- testes de `DecidirPedido`/recibo (`seedPedidoComItens`, `saldoProdutoEstoque`, `contarMovimentacoes`); `lotes_test.go`/`reservas_test.go`/`fefo_test.go` -- moldes para Lotes e concorrência.
- `backend/handlers/pedidos.go` (`DecidirPedidoHandler`) e frontend -- sem mudança (mesma resposta).

## Tasks & Acceptance

**Execution:**
- `backend/services/reservas.go` -- `saldoFisicoParTx` -- base da revalidação sem subtrair reservas
- `backend/services/pedidos.go` -- reescrever o ramo `aprovar` de `DecidirPedido` (travar todos os pares, reserva do Pedido, `consumirFEFOTx`, Movimentação por fonte) e o docstring
- `backend/services/pedidos_test.go` (+ `reservas_test.go` se couber) -- cobrir a matriz: FEFO em Lotes, legado + Lotes, reserva de outro Pedido não bloqueia, bug de reserva (ausente e físico < reserva), reserva apagada após aprovar/parcial, atomicidade (rollback), concorrência/locks com `-race`; ajustar testes existentes que dependam do débito antigo

**Acceptance Criteria:**
- Given um Pedido pendente com itens reservados, when o Almoxarife aprova, then cada item é revalidado contra a própria reserva (não contra o saldo livre) e o saldo só em Lotes é aprovável.
- Given um item aprovado, when o débito acontece, then consome Lote(s) do Estoque de origem por FEFO (`consumirFEFOTx`) e a reserva some na mesma transação.
- Given falha de revalidação em algum item, when a aprovação é processada, then o status é `parcialmente_aprovado` com `quantidadeAprovada` por item — nunca sucesso parcial silencioso.
- Given débito, liberação de reserva e Movimentação, when a aprovação é confirmada, then os três são atômicos.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 0
- defer: 0
- reject: 30
- addressed_findings:
  - none
- Rejeitados (resumo): teto físico ignorar validade / Lote vencido fora do FEFO (o épico manda só sinalizar vencido, `consumirFEFOTx` consome vencidos, decidido na 11.4); par (produto, estoque) duplicado no Pedido, Pedido sem itens, `consumos` vazio e linhas negativas (inalcançáveis: PK de `pedido_itens`, carrinho não vazio no envio, `CHECK quantidade >= 0` e teto físico ≥ quantidade); `it.Quantidade` sem `arredondar3` no ramo integral (vem de NUMERIC(10,3), preserva `quantidadeAprovada == quantidade`); consumo de saldo reservado por outro Pedido em cenário de bug (limite pelo físico, definido no contrato); ausência de log/`pedido_id` na Movimentação e N+1 dentro da transação (fora do escopo/cosméticos); pedidos de mais testes e helpers de teste (cosméticos; matriz inteira coberta); "sucesso parcial" sem lista distinta e ausência de teste de handler/papel (o contrato manda manter o comportamento do Epic 7: `parcialmente_aprovado` com `quantidadeAprovada` por item; papel e handler inalterados).

## Design Notes

- **Por que físico e não livre:** a reserva do próprio Pedido já é o "direito" ao saldo; o livre (físico − reservas) exclui esse mesmo direito e faria todo Pedido falhar. O físico entra só como teto de segurança para o débito FEFO nunca faltar fonte.
- **Movimentação por fonte:** igual à 11.4 (1 linha por Lote consumido), o que dá rastreio por Lote.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros e sem arquivos listados.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 -p 1 ./...` -- expected: tudo passa.

## Auto Run Result

Status: done

- **Resumo:** `DecidirPedido` (aprovar) passou a travar todos os pares dos itens com `travarSaldoParesTx`, revalidar cada item contra a reserva do PRÓPRIO Pedido (`min(solicitada, reserva, saldo físico)`) e debitar por `consumirFEFOTx`, gravando 1 Movimentação `baixa` por fonte consumida. Débito, liberação das reservas e decisão seguem atômicos. Saldo só em Lotes (transferido pela 11.4) agora é aprovável, fechando o `deferred` da 11.4.
- **Arquivos alterados:**
  - `backend/services/reservas.go` — novo `saldoFisicoParTx` (produto_estoque + lotes, sem subtrair reservas).
  - `backend/services/pedidos.go` — ramo `aprovar` de `DecidirPedido` reescrito e docstring atualizado.
  - `backend/services/pedidos_test.go` — 7 testes novos (FEFO só em Lotes, legado + Lotes, reserva de outro Pedido não bloqueia, bug de reserva em 3 variações, rollback atômico, aprovações concorrentes, locks opostos com Lotes).
- **Achados da revisão:** patches aplicados 0; adiados 0; rejeitados 30.
- **Recomendação de nova revisão:** `false` (patches: high 0, medium 0, low 0; pontuação 0 < 5).
- **Verificação:** `go build ./... && go vet ./... && gofmt -l .` sem erros e sem arquivos listados; `go test -count=1 -p 1 ./...` todos os pacotes `ok`; testes de `DecidirPedido` também passaram com `-race`.
- **Riscos residuais:** bug de reserva (reserva ausente/menor ou saldo físico abaixo da reserva) é absorvido como `parcialmente_aprovado`, sem log/alerta distinto (comportamento do Epic 7 preservado); em cenário anômalo com reservas acima do saldo, um Pedido pode consumir saldo reservado por outro (limitado pelo físico).
