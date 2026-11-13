---
title: 'Documento impresso da Pré-Nota'
type: 'feature'
created: '2026-11-13'
status: 'done'
review_loop_iteration: 0
baseline_commit: '17c3fca85fb5dd5081ab83cbf246f797e0f8a7ae'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-20-context.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A Pré-Nota (Story 20.4) existe só como registro no banco — o Gestor não tem o documento com os dados fiscais pra levar ao processo de emissão de NF da Ferreira Costa. E o pedido não guarda preço nem unidade no momento da aprovação, então um documento gerado a partir do cadastro atual mudaria de valor sempre que alguém editasse o Produto.

**Approach:** (1) Na aprovação em lote, gravar em `pedido_itens` um snapshot de `preco_unitario` e `unidade_medida` do Produto, dentro da MESMA transação. (2) Novo `GET /api/pre-notas/{id}/pdf` (gestor+, isolado por Empresa) que gera o PDF rotulado "Pré-Nota", no molde EXATO do recibo de Pedido (montar conteúdo -> desenhar, determinístico). (3) Botão "Baixar Pré-Nota" no resultado da Aprovação em lote e no detalhe do Pedido (Fila/Meus Pedidos) quando ele tiver Pré-Nota.

## Boundaries & Constraints

**Always:**
- Valor e Unidade de Medida no PDF vêm SEMPRE do snapshot gravado na aprovação (`pedido_itens`), nunca do cadastro atual do Produto — editar o preço ou a unidade do Produto depois nunca muda uma Pré-Nota já emitida. Código do produto, nomes das Filiais, razão social/CNPJ da Empresa e nome do Aprovador vêm do cadastro atual no momento do download (mesmo critério do recibo de Pedido; decisão do usuário, 2026-11-13). Renderização determinística: mesma entrada -> mesmos bytes (sem `time.Now()`, sem `SetInfo`).
- Cada item mostra o valor da linha (`quantidade_aprovada` × preço do snapshot) e o documento mostra o total geral; item sem preço mostra "—" na linha, não entra na soma, e o total traz um aviso de que há itens sem valor.
- Rodapé com o texto "Documento interno de controle — não substitui a Nota Fiscal".
- Snapshot ausente (`NULL`: Produto sem preço cadastrado, ou Pré-Nota criada antes desta story) aparece como "—", nunca quebra a geração.
- Quantidade no PDF é a `quantidade_aprovada` (o que de fato foi movimentado). Item com `quantidade_aprovada = 0` (aprovação parcial zerada) não aparece — o documento reflete só o que saiu do estoque.
- Cabeçalho: rótulo "Pré-Nota", `docnum`, Empresa (razão social + CNPJ), Data (`pre_notas.criado_em`), Aprovador (`pre_notas.decidido_por` -> nome), Solicitante(s) (distintos, dos Pedidos vinculados).
- Por item: Produto (código interno), Descrição (nome do produto, do snapshot), Quantidade, Unidade, Valor unitário; itens de Transferência mostram Filial origem e Filial destino (via `estoques.filial_id`); itens de Baixa não mostram destino.
- `RequireRole(PapelGestor)` na rota + filtro por `empresa_id` na consulta: Pré-Nota de outra Empresa, id inexistente ou malformado -> 404 (mesmo colapso do recibo de Pedido).

**Ask First:** nenhuma — snapshot de preço/unidade na aprovação e os dois pontos de download foram validados com o usuário antes desta spec ser aprovada.

**Never:** recalcular preço a partir do cadastro atual; mudar o recibo de Pedido existente (`GerarReciboPedidoPDF`) ou `DecidirPedido` (retirada); criar uma tela de listagem/histórico de Pré-Notas (fora de escopo); impostos (não pedidos pelas ACs).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Pré-Nota mista | 1 Transferência + 1 Baixa aprovadas no lote | PDF "Pré-Nota nº N" com os 2 itens; Transferência mostra Filial origem/destino, Baixa não | N/A |
| Preço alterado depois | Produto teve `preco_unitario` editado após a aprovação | PDF mostra o preço do momento da aprovação | N/A |
| Sem preço cadastrado | Produto sem `preco_unitario` na aprovação | Valor "—", PDF gerado normalmente | N/A |
| Item zerado | Item do lote aprovado com `quantidade_aprovada = 0` | Item não aparece no PDF | N/A |
| Totais com item sem preço | 1 item com preço R$ 10,00 × 3 + 1 item sem preço | Linha 1 "R$ 30,00", linha 2 "—", total "R$ 30,00" com aviso de item sem valor | N/A |
| Papel insuficiente | `almoxarife` pede o PDF | Nada gerado | 403 |
| Outra Empresa / inexistente | id de Pré-Nota de outra Empresa ou que não existe | Nada gerado | 404 |

</frozen-after-approval>

## Code Map

- `backend/migrations/000060_*` (nova) -- `ALTER TABLE pedido_itens ADD COLUMN preco_unitario NUMERIC(12,2) NULL, ADD COLUMN unidade_medida <mesmo tipo de produtos.unidade_medida> NULL` — sem backfill (Pré-Notas antigas ficam "—"; produção tinha zero Pré-Notas no deploy da 20.4).
- `backend/services/pedidos.go:1687` (`decidirUmPedidoDoLoteTx`), UPDATE de `quantidade_aprovada` em ~1711 -- no ramo `aprovar`, gravar também `preco_unitario`/`unidade_medida` lidos de `produtos` (mesma transação, mesmo `empresa_id`). NÃO tocar o UPDATE equivalente de `DecidirPedido` (~1200, retirada).
- `backend/services/pedidos.go:1974-2198` (`ReciboPedidoItem`/`ReciboPedidoConteudo`/`MontarReciboPedidoConteudo`/`RenderizarReciboPedidoPDF`/`GerarReciboPedidoPDF`) -- molde EXATO para `PreNotaConteudo`/`MontarPreNotaConteudo(db, empresaID, preNotaID)`/`RenderizarPreNotaPDF`/`GerarPreNotaPDF`: struct de dados sem gopdf, fonte `goregular`, quebra de página manual, sem metadata dinâmica. Pode ficar num arquivo novo `services/pre_notas.go`.
- `backend/handlers/pedidos.go:553` (`BaixarReciboPedidoHandler`) -- molde para `BaixarPreNotaPDFHandler` (`Content-Disposition: attachment; filename="pre-nota-{docnum}.pdf"`, 404/500).
- `backend/main.go:912` (rota do recibo) -- molde para `GET /e/{slug}/api/pre-notas/{id}/pdf` com `RequireAuth` + `RequireRole(services.PapelGestor)`.
- `frontend/src/lib/pedidos.ts:306` (download do recibo como Blob) -- molde para `baixarPreNotaPDF(preNotaId)`.
- `frontend/src/pages/AprovacaoLotePage.tsx:56` (`mensagemResultadoLote`) -- quando a resposta do lote trouxer `preNotaId`/`docnum` (Story 20.4), mostrar "Pré-Nota nº N" com botão "Baixar Pré-Nota".
- `frontend/src/components/pedidos/FilaPedidosSection.tsx:247` e `MeusPedidosSection.tsx:221` (`aoBaixarRecibo`) -- botão irmão "Baixar Pré-Nota" no Dialog de detalhe quando o Pedido tem `preNotaId` e o usuário é `gestor`+.

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/000060_*.up/down.sql` -- snapshot de preço/unidade em `pedido_itens`
- [x] `backend/services/pedidos.go` -- gravar o snapshot no ramo de aprovação de `decidirUmPedidoDoLoteTx`
- [x] `backend/services/pre_notas.go` (novo) + `_test.go` -- montar/desenhar/gerar a Pré-Nota; testes cobrindo as linhas de serviço da I/O Matrix + determinismo (2 gerações = mesmos bytes) + conteúdo montado (sem ler bytes de PDF)
- [x] `backend/handlers/` + `backend/main.go` -- rota `GET /api/pre-notas/{id}/pdf`, com teste HTTP (200/403/404) e teste pelo `newMux` real confirmando o `RequireRole(PapelGestor)`
- [x] `frontend/src/lib/pedidos.ts` (+ `.test.ts`) -- `baixarPreNotaPDF`
- [x] `frontend/src/pages/AprovacaoLotePage.tsx` (+ `.test.tsx`) -- "Pré-Nota nº N" + botão de download no resultado do lote
- [x] `frontend/src/components/pedidos/FilaPedidosSection.tsx`/`MeusPedidosSection.tsx` (+ testes) -- botão "Baixar Pré-Nota" quando houver `preNotaId` e papel `gestor`+

**Acceptance Criteria:**
- Given uma Pré-Nota já gerada, when o Gestor pede o PDF, then o documento é rotulado "Pré-Nota" e mostra docnum, Empresa, Data, Solicitante(s) e Aprovador, e cada item lista Produto, Descrição, Quantidade, Unidade e Valor
- Given o Gestor acabou de aprovar um lote, when a resposta volta com Pré-Nota, then a tela mostra o número dela e permite baixar o PDF sem sair da página
- Given `go build ./...`, `go test -p 1 ./...`, `npm run build` e `npx vitest run`, when rodados após a story, then terminam sem erro

## Spec Change Log

- **2026-11-13, revisão (renegociação humana + patches):** a revisão em triplo achou que a regra frozen prometia o PDF "idêntico byte a byte daqui a um ano", mas código do produto, Filiais, Empresa e Aprovador eram lidos do cadastro atual (intent_gap). O usuário decidiu: congelar só preço e unidade (os nomes seguem o cadastro atual, como no recibo de Pedido), incluir valor por linha e total geral (item sem preço fica "—", fora da soma, com aviso), e acrescentar o rodapé "Documento interno de controle — não substitui a Nota Fiscal". Regra frozen e I/O Matrix ajustadas por ele. **KEEP:** snapshot de preço/unidade, rota gestor+ com 404 entre Empresas, molde montar->desenhar, os 3 pontos de download — tudo correto e mantido sem reverter código. Demais achados aplicados como patch nesta rodada (ver mensagem de triagem); itens de menor risco foram para `deferred-work.md`.

## Verification

**Commands:**
- `cd backend && go build ./... && go test ./services/... -run 'PreNota'` -- expected: build limpo, testes novos passando
- `cd backend && go test -p 1 ./...` -- expected: suíte completa do backend limpa
- `cd frontend && npm run build && npx vitest run` -- expected: build limpo, suíte completa do frontend limpa

## Suggested Review Order

**Snapshot na aprovação (a base do "preço do momento da aprovação")**

- Preço e unidade gravados no item, na mesma transação do lote, numa leitura só de `produtos`
  [`pedidos.go:1721`](../../backend/services/pedidos.go#L1721)

- Colunas novas em `pedido_itens`, sem backfill
  [`000060_add_snapshot_preco_unidade_pedido_itens.up.sql:1`](../../backend/migrations/000060_add_snapshot_preco_unidade_pedido_itens.up.sql#L1)

**Documento**

- Monta o conteúdo numa transação só-leitura; preço/unidade do snapshot, nomes do cadastro atual
  [`pre_notas.go:100`](../../backend/services/pre_notas.go#L100)

- Texto do documento em funções puras: totais, "—", aviso, rodapé, tipos — é o que os testes conferem
  [`pre_notas.go:335`](../../backend/services/pre_notas.go#L335)

- Desenho do PDF: quebra de linha, item nunca dividido entre páginas, fuso de São Paulo
  [`pre_notas.go:423`](../../backend/services/pre_notas.go#L423)

**Acesso**

- Rota gestor+, 404 entre Empresas, sem cache
  [`main.go:942`](../../backend/main.go#L942)
  [`handlers/pedidos.go:604`](../../backend/handlers/pedidos.go#L604)

**Frontend**

- Botão único de download, reaproveitado nos 3 pontos
  [`BaixarPreNotaButton.tsx:22`](../../frontend/src/components/pedidos/BaixarPreNotaButton.tsx#L22)

- "Pré-Nota nº N gerada" logo após aprovar o lote
  [`AprovacaoLotePage.tsx:311`](../../frontend/src/pages/AprovacaoLotePage.tsx#L311)

- Chamada da API
  [`lib/pedidos.ts:331`](../../frontend/src/lib/pedidos.ts#L331)
