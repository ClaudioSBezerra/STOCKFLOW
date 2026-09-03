---
title: 'Aprovação/rejeição com revalidação de estoque item a item'
type: 'feature'
created: '2026-09-03'
status: 'done'
baseline_revision: '7da31387881bb2da67a8382eb29a003b5a02cf90'
review_loop_iteration: 0
followup_review_recommended: false
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-7-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      `MeusPedidosSection.tsx` (a visão "Meus Pedidos" do próprio solicitante)
      não mostra a quebra Solicitado/Aprovado/Pendente por item que
      `FilaPedidosSection` passou a mostrar para um Pedido já decidido — só
      exibe `item.quantidade`.
    evidence: |-
      `PedidoItem.quantidadeAprovada` já está disponível na mesma resposta de
      `buscarPedido` usada por `MeusPedidosSection.tsx` (idêntica à usada por
      `FilaPedidosSection`), mas o arquivo nunca lê esse campo. As ACs da
      Story 7.5 (epics.md, UJ-2) são todas do lado Almoxarife/Fila; "Meus
      Pedidos" é superfície da Story 7.3, cujas ACs (FR-23) só exigem
      "filtrável por status, com texto explicativo por status" — nenhuma
      exige detalhe por item. O solicitante ainda vê o resultado agregado via
      `StatusPedidoBadge` (já compartilhado, sem mudança).
    location: 'frontend/src/components/pedidos/MeusPedidosSection.tsx'
    severity: medium
  - summary: >-
      O filtro de status de `MeusPedidosSection.tsx` (`OPCOES_FILTRO`) não
      ganhou a opção `parcialmente_aprovado`, mesmo esse valor já sendo aceito
      pelo backend para `?status=` em Meus Pedidos.
    evidence: |-
      Mesma causa raiz do item anterior — superfície da Story 7.3, fora do
      escopo das ACs desta story (7.5).
    location: 'frontend/src/components/pedidos/MeusPedidosSection.tsx'
    severity: low
  - summary: >-
      Nenhum log estruturado de sucesso (`slog.Info`) registra quem
      aprovou/rejeitou qual Pedido — só falhas são logadas via `slog.Error`
      em `DecidirPedidoHandler`.
    evidence: |-
      Mesmo padrão pré-existente em `SubmeterPedidoHandler` e
      `DecidirPromocaoHandler` (nenhum dos dois loga sucesso) — escolha de
      filosofia de logging já estabelecida no projeto, não uma regressão
      desta story. A trilha de auditoria real (`decidido_por`/`decidido_em`)
      já fica na própria linha de `pedidos`.
    location: 'backend/handlers/pedidos.go (DecidirPedidoHandler)'
    severity: low
  - summary: >-
      `DecidirPedido` trava/debita/insere Movimentação um item por vez,
      sequencialmente, dentro da mesma transação — sem nenhum batching —
      estendendo o tempo de retenção dos locks para Pedidos com muitos itens.
    evidence: |-
      Mesmo padrão já usado por `SubmeterPedido` (services/pedidos.go) para o
      mesmo tipo de loop item-a-item — escolha arquitetural pré-existente do
      domínio de Pedidos, não introduzida por esta story.
    location: 'backend/services/pedidos.go (DecidirPedido)'
    severity: low
  - summary: >-
      Nenhum teste de `DecidirPedido`/`BuscarPedidoProprio` cobre uma
      quantidade fracionária (ex.: `10.500`) atravessando o fluxo de
      decisão — toda a I/O Matrix testada usa só quantidades inteiras, apesar
      de `quantidade`/`quantidade_aprovada` serem `NUMERIC(10,3)`.
    evidence: |-
      `formatarQuantidade` (frontend/src/components/catalogo/formatacao.tsx)
      usa `toLocaleString('pt-BR')`, que arredonda para 3 casas decimais por
      padrão — um erro de ponto flutuante do JS na subtração
      `quantidade - quantidadeAprovada` (FilaPedidosSection.tsx) apareceria
      muito além da 3ª casa e seria absorvido por esse arredondamento, então
      o risco prático é baixo; mas nenhum teste do backend ou do frontend
      prova isso com um valor fracionário real.
    location: 'backend/services/pedidos_test.go; frontend/src/components/pedidos/FilaPedidosSection.tsx'
    severity: low
  - summary: >-
      O Dialog "Ver itens" aberto num Pedido `pendente` não reage ao evento
      SSE `pedidos` para o PRÓPRIO Pedido em exibição — se um segundo
      Almoxarife decidir o mesmo Pedido enquanto o Dialog está aberto, os
      botões "Aprovar"/"Rejeitar" continuam visíveis/clicáveis até um clique
      tardio esbarrar no 409 (coberto, mas só reativamente).
    evidence: |-
      `FilaPedidosSection.tsx` já se inscreve no canal `pedidos` para
      atualizar a LISTA (`toast.info('Fila de Pedidos atualizada.')`), mas o
      estado `detalheDe` do Dialog aberto não é invalidado/refeito por esse
      mesmo evento — comportamento coberto pelo 409 CONFLICT da AC de
      concorrência (spec-7-5), não uma perda de dados, só uma janela de UX
      sem feedback proativo.
    location: 'frontend/src/components/pedidos/FilaPedidosSection.tsx'
    severity: low
  - summary: >-
      Nenhum teste prova que `decididoPor`/`decididoEm` permanecem `null` em
      `GET /api/pedidos/{id}` (via `BuscarPedidoProprio`) depois de uma
      decisão — o "Never" da spec-7-5 ("não exibe no frontend quem
      decidiu/quando") depende hoje só do fato de `BuscarPedidoProprio` nunca
      selecionar essas colunas, sem uma trava de regressão testada.
    evidence: |-
      `BuscarPedidoProprio` (services/pedidos.go) faz
      `SELECT id, usuario_id, solicitante, obra_centro_custo, observacao,
      status, criado_em FROM pedidos` — `decidido_por`/`decidido_em` de fato
      nunca entram no SELECT, então os ponteiros ficam `nil` por construção;
      mas se uma mudança futura estender esse SELECT (ex.: para outro campo),
      nada barra `decididoPor`/`decididoEm` de vazar junto sem que nenhum
      teste acuse.
    location: 'backend/services/pedidos.go (BuscarPedidoProprio)'
    severity: low
---

<intent-contract>

## Intent

**Problem:** Story 7.4 só lista a Fila do Almoxarife — não existe hoje nenhuma ação para decidir (aprovar/rejeitar) um Pedido pendente, e uma aprovação ingênua correria o risco de debitar estoque que já não existe mais no momento real da decisão.

**Approach:** Acrescenta `POST /api/pedidos/{id}/decisao` (só `almoxarife`+) que revalida cada item na MESMA transação da decisão — trava `produto_estoque` por par `(produto_id, estoque_id)` em ordem ascendente sobre o LOTE INTEIRO (AD-10), aprova cada item até o disponível real (nunca mais que o solicitado), debita e registra Movimentação atomicamente, e grava o quanto foi de fato aprovado por item (o restante fica visível como pendência, nunca descartado). A Fila ganha botões "Aprovar"/"Rejeitar" no diálogo "Ver itens" de um Pedido `pendente`.

## Boundaries & Constraints

**Always:**
- Rota nova `POST /api/pedidos/{id}/decisao`, composição `RequireAuth(db,jwtSecret)(RequireRole(services.PapelAlmoxarife)(handler))` (molde de `POST /api/estoques`, main.go:369-371) — `RequireRole` roda a cada requisição, nunca cacheado, o que já satisfaz "papel revalidado no momento da submissão" (epics.md Story 7.5 AC4) só por composição.
- Corpo `{"aprovar": bool}`, `*bool` (molde EXATO de `decisaoRequest`/`DecidirPromocaoHandler`, handlers/promocao.go) — ausente/nulo/corpo malformado -> 400 VALIDATION_ERROR, nunca uma rejeição silenciosa por omissão.
- `services.DecidirPedido(db, pedidoID, decisorID, papelDecisor string, aprovar bool) (PedidoDetalhe, error)` (novo, pedidos.go) — molde de `DecidirSolicitacao` (promocao.go:226-290): SELECT inicial de `status` (sem lock) fora da tx para `id` inexistente/malformado -> `ErrPedidoNaoEncontrado` (404) e `status != 'pendente'` -> novo `ErrPedidoNaoPendente` (409 CONFLICT); dentro da tx, o `UPDATE pedidos SET status=$,decidido_por=$,decidido_em=now() WHERE id=$1 AND status='pendente' RETURNING ...` fecha a corrida entre duas decisões concorrentes (`sql.ErrNoRows` -> mesmo `ErrPedidoNaoPendente`, rollback desfaz qualquer débito já feito nessa tx).
- `aprovar=true`: pares `(produto_id,estoque_id)` de TODOS os itens via `SELECT produto_id,estoque_id,quantidade FROM pedido_itens WHERE pedido_id=$1 ORDER BY produto_id, estoque_id` (ordem ascendente sobre o LOTE INTEIRO — mesmo AD-10 de `SubmeterPedido`, pedidos.go:159-170), travados um a um nessa ordem via `SELECT quantidade FROM produto_estoque ... FOR UPDATE` (mesmo colapso de `SubmeterPedido`/`RegistrarBaixa`: ausência de linha = 0 disponível). Por item: `quantidadeAprovada := min(quantidade solicitada, disponível)`; se `> 0`, debita `produto_estoque` e insere `movimentacoes` (`tipo='baixa'`, `estoque_origem_id`=o do item, `usuario_id`=o DECISOR — mesma convenção de `RegistrarBaixa`) na MESMA tx; grava `quantidadeAprovada` em `pedido_itens`.
- Status final do cabeçalho: `'aprovado'` se TODOS os itens com `quantidadeAprovada == quantidade`; senão `'parcialmente_aprovado'` (inclui `quantidadeAprovada == 0` em todos os itens — o Almoxarife escolheu aprovar, não rejeitar; nunca reclassificado como `'rejeitado'` por baixo do capô).
- `aprovar=false`: nenhuma leitura/trava de `produto_estoque`, nenhuma Movimentação; grava `quantidade_aprovada=0` em TODOS os itens (nunca NULL depois de decidido) e `status='rejeitado'`.
- Sucesso -> `200 {"pedido": PedidoDetalhe}`, cada item com `quantidade` (solicitado) e `quantidadeAprovada` lado a lado; publica no canal `pedidos` (`registro.Publish("pedidos", realtime.Evento{ID: pedido.ID, Change: <novo status>})`), mesmo padrão de `SubmeterPedidoHandler`.
- Nova migration `000027`: `pedidos` ganha `decidido_por UUID REFERENCES usuarios(id)` + `decidido_em TIMESTAMPTZ` (ambas nullable) e o CHECK de `status` passa a incluir `'parcialmente_aprovado'`; `pedido_itens` ganha `quantidade_aprovada NUMERIC(10,3)` (NULLABLE — NULL enquanto `pendente`, concreto de 0 a `quantidade` a partir da decisão) com CHECK `IS NULL OR (quantidade_aprovada >= 0 AND quantidade_aprovada <= quantidade)`.
- `statusPedidoValido` (pedidos.go:72-76) ganha a chave `"parcialmente_aprovado"` — filtro `?status=` de Meus Pedidos/Fila passa a aceitar o novo valor.
- `BuscarPedidoProprio` (pedidos.go:391-439) e o Scan de `PedidoItem` passam a trazer `quantidade_aprovada` (nova coluna) — único ponto de leitura tocado além do necessário para expor o novo campo.
- Frontend `FilaPedidosSection`: Dialog "Ver itens" de um Pedido `pendente` ganha botões "Aprovar"/"Rejeitar", cada um atrás de um `ConfirmDialog` PRÓPRIO com texto genérico (sem números ao vivo — molde de `DuplicatasSection`, Story 6.4: a revalidação real só acontece no POST, nunca num preview) — nunca `window.confirm()`. Guard de duplo-submit por id em decisão (molde de `ConfiguracoesPage.decidir`, linha 351-364). Sucesso: fecha o Dialog, `toast` com o resultado a partir do novo `pedido.status` (`'aprovado'`/`'parcialmente_aprovado'`/`'rejeitado'`) — a lista já re-renderiza pelo refetch que o próprio evento SSE `pedidos` dispara (a decisão publica nesse canal), nenhum refetch manual extra.
- Item já decidido (reabrir "Ver itens" de um Pedido não-`pendente`) mostra `quantidade` (Solicitado) e, quando `quantidadeAprovada !== quantidade`, também `quantidadeAprovada` (Aprovado) e `quantidade - quantidadeAprovada` (Pendente) — nunca escondido (EXPERIENCE.md, clímax da Jornada 2).
- `StatusPedidoBadge`/`estiloDoStatus` (StatusPedidoBadge.tsx:21-48) ganha `case 'parcialmente_aprovado'` (rótulo "Parcialmente aprovado", tinta/ícone distintos de `aprovado`/`pendente`); `StatusPedido` (lib/pedidos.ts:22) ganha o novo literal.

**Block If:** _Nenhuma decisão bloqueante — schema, rota, ordenação de locks e a ausência de endpoint de preview já estão fixados por precedente direto do repositório (`SubmeterPedido`, `DecidirSolicitacao`, `MesclarDuplicatas`) e pelas ACs de epics.md/PRD._

**Never:**
- Não implementa Story 7.6 (recibo em PDF) — a resposta da decisão não gera nem referencia PDF.
- Não cria tabela/Pedido derivado para a pendência — o restante não atendido é um NÚMERO no próprio item (`quantidade - quantidadeAprovada`), nunca um novo Pedido formal ou um novo fluxo de re-decisão desse restante (fora do escopo desta story).
- Não adiciona coluna de vínculo `pedido_id` em `movimentacoes` — esse vínculo é responsabilidade da Story 7.7 (epic-7-context.md, Cross-Story Dependencies), não desta.
- Não cria endpoint de preview/revalidação dedicado (`GET .../revalidacao` ou similar) — mesmo padrão das 3 stories precedentes do repositório (6.4, 1.7, 7.2): uma única chamada de escrita revalida e commita atomicamente; a divergência é conhecida e mostrada como RESULTADO da decisão, nunca escondida.
- Não exibe no frontend quem decidiu/quando (`decididoPor`/`decididoEm`) — auditoria fica só no banco/API nesta story, para a Story 7.6 (recibo, campo "aprovador") consumir depois.
- Não modifica `SubmeterPedido`, `ListarPedidosProprios`, `ListarPedidosFila`, `ListarPedidosParaSessao` nem a lógica de autorização de `BuscarPedidoProprio` — só a projeção lida por ele ganha o campo novo.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Aprovação total | Pedido pendente, todos os itens com disponível >= solicitado, `{"aprovar":true}` por almoxarife+ | `200`, status vira `aprovado`, cada item `quantidadeAprovada == quantidade`, estoque debitado + Movimentação por item, SSE `pedidos` publicado | No error expected |
| Aprovação parcial | Pedido pendente, 1 item com disponível(4) < solicitado(10), outro item ok | `200`, status vira `parcialmente_aprovado`, item divergente com `quantidadeAprovada=4` (débito só de 4), item ok com `quantidadeAprovada == quantidade` | No error expected |
| Item sem estoque algum | disponível=0 para 1 item, `{"aprovar":true}` | `200`, esse item `quantidadeAprovada=0`, nenhuma Movimentação para ele, status `parcialmente_aprovado` | No error expected |
| Rejeição | Pedido pendente, `{"aprovar":false}` | `200`, status `rejeitado`, todos os itens `quantidadeAprovada=0`, nenhum débito, nenhuma Movimentação | No error expected |
| Papel insuficiente | papel `usuario`, `POST /api/pedidos/{id}/decisao` | Requisição recusada antes de tocar o service | `403 FORBIDDEN` |
| Pedido já decidido | Pedido com `status != 'pendente'` | Requisição recusada, nenhuma escrita | `409 CONFLICT` |
| Corpo sem `aprovar` | `{}` ou JSON malformado | Requisição recusada antes de tocar o banco | `400 VALIDATION_ERROR` |
| Id inexistente/malformado | id não-UUID ou UUID sem Pedido correspondente | Requisição recusada | `404 NOT_FOUND` |

</intent-contract>

## Code Map

- `backend/migrations/000027_add_decisao_pedidos.up.sql`/`.down.sql` (novo) -- `pedidos.decidido_por`/`decidido_em` + novo valor de `status`; `pedido_itens.quantidade_aprovada`. Confirmar o nome real da constraint de CHECK de `pedidos.status` (esperado `pedidos_status_check`, convenção padrão do Postgres) antes do `DROP CONSTRAINT`.
- `backend/services/pedidos.go` -- `ErrPedidoNaoPendente` (novo, molde de `ErrSolicitacaoNaoPendente`); `DecidirPedido` (novo, descrito em Boundaries); estender `Pedido` (+`DecididoPor *string`, +`DecididoEm *time.Time`) e `PedidoItem` (+`QuantidadeAprovada *float64`); `statusPedidoValido` (linha 72-76) +`"parcialmente_aprovado"`; `BuscarPedidoProprio` (linha 391-439) e seu `selectItens`/Scan ganham `quantidade_aprovada`.
- `backend/handlers/pedidos.go` -- `decisaoPedidoRequest{Aprovar *bool}` (molde de `decisaoRequest`, handlers/promocao.go:59-64); `DecidirPedidoHandler(db, registro)` (molde de `DecidirPromocaoHandler`, handlers/promocao.go:180-220, mais `RequireRole` já resolvendo autorização — sem checagem de papel extra no handler).
- `backend/main.go` -- registrar `POST /api/pedidos/{id}/decisao` logo após as rotas de pedidos existentes (~linha 624), composição `RequireAuth(db,jwtSecret)(RequireRole(services.PapelAlmoxarife)(handlers.DecidirPedidoHandler(db, registro)))`.
- `backend/services/pedidos_test.go` -- testes de `DecidirPedido` cobrindo toda a I/O Matrix, incluindo ordenação de locks (AD-10) sobre um Pedido com múltiplos itens.
- `backend/handlers/pedidos_test.go` -- helper novo `postDecisaoPedido(db, authHeader, id, body string)` (molde de `postPedido`, linha 23-40, com `RequireRole` na composição); testes de `POST /api/pedidos/{id}/decisao` cobrindo a I/O Matrix na fronteira HTTP. Reaproveitar `seedPedidoViaServico`/`criarContaComPapel("almoxarife", ...)`.
- `frontend/src/lib/pedidos.ts` -- `StatusPedido` +`'parcialmente_aprovado'`; `PedidoItem` +`quantidadeAprovada: number | null`; `PedidoCabecalho` +`decididoPor: string | null`, +`decididoEm: string | null`; `decidirPedido(id: string, aprovar: boolean): Promise<PedidoDetalhe>` (novo, `POST /api/pedidos/{id}/decisao`, molde de `ConfiguracoesPage.decidir` linha 351-364 para o fetch, mas seguindo a assinatura/tratamento de erro dos demais exports deste módulo).
- `frontend/src/lib/pedidos.test.ts` -- testes de `decidirPedido` (URL/método/corpo/erro).
- `frontend/src/components/pedidos/StatusPedidoBadge.tsx` -- `case 'parcialmente_aprovado'` em `estiloDoStatus` (linha 21-48).
- `frontend/src/components/pedidos/FilaPedidosSection.tsx` -- Dialog "Ver itens" (linha 253-299): itens ganham exibição de Aprovado/Pendente quando `quantidadeAprovada !== quantidade`; botões "Aprovar"/"Rejeitar" (só quando `detalheDe?.status === 'pendente'`) com `ConfirmDialog` próprio cada, chamando `decidirPedido`, toast de resultado, guarda de duplo-submit.
- `frontend/src/components/pedidos/FilaPedidosSection.test.tsx` -- testes da nova UI de decisão (aprovar total/parcial, rejeitar, ConfirmDialog, guarda de duplo-submit, exibição de pendência num item já decidido, papel/gate já coberto por PedidosPage.test.tsx inalterado).
- `frontend/src/components/ConfirmDialog.tsx` -- reaproveitado sem mudança (molde de uso em `frontend/src/components/estoques/LocaisEstoqueSection.tsx:126-167,228-239`).
- `backend/services/promocao.go` (`DecidirSolicitacao`, linha 226) / `backend/handlers/promocao.go` (`DecidirPromocaoHandler`, linha 180) -- moldes diretos do padrão "decisão com corpo `{aprovar: bool}` + guarda de corrida via UPDATE condicional".

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000027_add_decisao_pedidos.up.sql`+`.down.sql` -- novas colunas/CHECKs conforme Code Map -- base de dados para a decisão.
- `backend/services/pedidos.go` -- `DecidirPedido`+`ErrPedidoNaoPendente`+extensões de struct/`statusPedidoValido`/`BuscarPedidoProprio` -- regra de negócio da decisão, atômica e revalidada.
- `backend/handlers/pedidos.go` -- `DecidirPedidoHandler`+`decisaoPedidoRequest` -- fronteira HTTP da decisão.
- `backend/main.go` -- registrar a rota nova atrás de `RequireRole(almoxarife)`.
- `backend/services/pedidos_test.go` -- cobertura da I/O Matrix no nível de service.
- `backend/handlers/pedidos_test.go` -- cobertura da I/O Matrix na fronteira HTTP.
- `frontend/src/lib/pedidos.ts`+`.test.ts` -- `decidirPedido` e tipos novos.
- `frontend/src/components/pedidos/StatusPedidoBadge.tsx` -- novo caso de status.
- `frontend/src/components/pedidos/FilaPedidosSection.tsx`+`.test.tsx` -- UI de decisão item-a-item na Fila.

**Acceptance Criteria:**
- Given um Pedido pendente com múltiplos itens de Produtos/Estoques diferentes, when a decisão de aprovação processa o lote inteiro, then os locks de `produto_estoque` são adquiridos na ordem ascendente de `(produto_id, estoque_id)` sobre o conjunto ordenado do lote inteiro — nunca a ordem de inserção do Pedido.
- Given uma decisão de aprovação/rejeição concluída, when ela é salva, then o badge do Pedido muda de status na hora via canal SSE `pedidos`, sem recarregar a página.
- Given um papel abaixo de `almoxarife` tentando decidir um Pedido, when a requisição chega, then é recusada (403) antes de qualquer leitura/escrita em `produto_estoque`.
- Given um item cuja disponibilidade real ficou abaixo do solicitado, when o Pedido é aprovado, then o restante não atendido fica visível no próprio item (nunca escondido, nunca descartado) e o débito/Movimentação são atômicos só para a quantidade efetivamente aprovada.
- Given duas decisões concorrentes para o MESMO Pedido, when ambas chegam quase ao mesmo tempo, then só a primeira a commitar decide de fato — a segunda recebe `409 CONFLICT` sem debitar nada.

## Spec Change Log

## Review Triage Log

### 2026-09-03 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 5 (high 0, medium 2, low 3)
- defer: 4 (high 0, medium 1, low 3)
- reject: 11
- addressed_findings:
  - `[medium]` `[patch]` `DecidirPedido` (`backend/services/pedidos.go`) montava a resposta com um `BuscarPedidoProprio` chamado DEPOIS de `tx.Commit()` — uma falha transitória nessa releitura pós-commit devolveria `500` mesmo com a decisão já durável (estoque já debitado, Movimentação já registrada). Corrigido: o cabeçalho agora vem do próprio `RETURNING` do `UPDATE pedidos` de decisão e os itens são montados a partir do snapshot lido no início da MESMA transação — nenhuma consulta acontece depois do commit.
  - `[medium]` `[patch]` Nenhum teste comprovava que `decidido_por`/`decidido_em` (trilha de auditoria exigida pelo PRD, "toda decisão registra quem decidiu e quando") eram gravados corretamente. `TestDecidirPedido_AprovacaoTotal` estendido para asserir `DecididoPor == decisorID` e `DecididoEm` recente — passou a ser possível diretamente a partir do `RETURNING` estendido do patch acima.
  - `[low]` `[patch]` O `ConfirmDialog` de "Aprovar" (`FilaPedidosSection.tsx`) não avisava que a ação é irreversível, enquanto o de "Rejeitar" avisava ("Esta ação não pode ser desfeita") — inconsistente, já que aprovar também debita estoque real de forma irreversível. Mesmo aviso acrescentado à descrição do diálogo de "Aprovar".
  - `[low]` `[patch]` `OPCOES_FILTRO` (filtro de status da Fila, `FilaPedidosSection.tsx`) nunca ganhou a opção `parcialmente_aprovado`, mesmo esse status já sendo aceito pelo backend — um Almoxarife não conseguia filtrar a Fila só pelos Pedidos parcialmente aprovados. Opção acrescentada, com teste cobrindo a seleção.
  - `[low]` `[patch]` A única corrida testada em `DecidirPedido` era duas decisões `aprovar=true` simultâneas. Novo teste `TestDecidirPedido_DecisoesConcorrentesMistasSoAPrimeiraGanha` cobrindo uma corrida mista (`aprovar=true` vs. `aprovar=false`) sobre o MESMO Pedido pendente, confirmando que só a primeira decisão vence e não há débito duplicado.

### 2026-09-03 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 1 (high 0, medium 1, low 0)
- defer: 3 (high 0, medium 0, low 3)
- reject: 14
- addressed_findings:
  - `[medium]` `[patch]` A chave nova `"parcialmente_aprovado"` em `statusPedidoValido` (filtro `?status=` de Meus Pedidos/Fila) nunca era exercitada por nenhum teste passando por `ListarPedidosProprios`/`ListarPedidosFila`/`GET /api/pedidos` de verdade — só os testes de RESULTADO de `DecidirPedido`/`DecidirPedidoHandler` continham a string literal, e o teste de frontend mockava `listarFilaPedidos`. Um erro de digitação ou remoção dessa chave quebraria o filtro em produção sem que a suíte acusasse. Adicionados `TestListarPedidosProprios_FiltroPorStatusParcialmenteAprovado`, `TestListarPedidosFila_FiltroPorStatusParcialmenteAprovado` (services/pedidos_test.go) e `TestListarPedidosHandler_EscopoTodosFiltroPorStatusParcialmenteAprovado` (handlers/pedidos_test.go), cada um filtrando de verdade por esse valor e asserindo o resultado.

Verificação re-executada após o patch: `go build ./... && go vet ./...` limpo; suíte completa do backend (`go test -p 1 -count=1 ./...`) 100% passando; `npx tsc --noEmit` sem erro; `npx vitest run` 521/521 passando.

Achados triados como `defer` nesta passagem (adicionados a `deferred` no frontmatter): ausência de teste com quantidade fracionária no fluxo de decisão; Dialog "Ver itens" não reage ao evento SSE `pedidos` para o próprio Pedido em exibição enquanto aberto; ausência de teste que trave `decididoPor`/`decididoEm` como `null` em `BuscarPedidoProprio`. Achados triados como `reject` (falsos positivos confirmados por inspeção de código/schema, ou comportamento explicitamente mandado pelo `<intent-contract>`): colisão de UPDATE em itens com mesmo `(produto_id, estoque_id)` — impossível, `PRIMARY KEY (pedido_id, produto_id, estoque_id)` já impede duplicata; órfãos de `pedido_itens` na down-migration — impossível, `ON DELETE CASCADE` já cobre; guarda para `disponivel` negativo — impossível, `CHECK (quantidade >= 0)` em `produto_estoque` já impede; Pedido com zero itens aprovado por omissão — impossível, `SubmeterPedido` recusa carrinho vazio; `papelDecisor` não utilizado em `DecidirPedido` e autorização só na composição de rotas — ambos mandados literalmente pelo `<intent-contract>`; entre outros de menor substância (JSON com dados extras não rejeitado — convenção pré-existente do projeto; falta de spinner durante o POST — fora do molde exigido; ausência de docs de API — não é convenção do projeto).

## Design Notes

- **Sem endpoint de preview, de propósito:** os três precedentes mais próximos do repositório para "revalidar estado que pode ter mudado" (`MesclarDuplicatas`/Story 6.4, `DecidirSolicitacao`/Story 1.7, `SubmeterPedido`/Story 7.2) usam TODOS uma única chamada de escrita que revalida e commita atomicamente, devolvendo o resultado ou o motivo da divergência no próprio corpo da resposta — nenhum GET dedicado de preview antes de agir. Esta story segue o mesmo molde: "Solicitado: X · Disponível: Y" (epics.md AC1) é satisfeito pelo próprio `PedidoItem` da resposta (`quantidade` vs. `quantidadeAprovada`) após o clique em "Aprovar", com o `ConfirmDialog` prévio genérico cobrindo a "confirmação explícita" de UJ-2 do PRD.
- **`parcialmente_aprovado` mesmo com `quantidadeAprovada=0` em TODOS os itens:** se o Almoxarife escolheu "Aprovar" mas nada estava disponível, o cabeçalho ainda vira `parcialmente_aprovado` (nunca `aprovado`, que seria falso, nem `rejeitado`, que reclassificaria uma ação que não foi essa) — é o único valor que não mente sobre o que aconteceu.
- **Pendência é um número, não um novo Pedido:** `quantidade - quantidadeAprovada` já é "a pendência separada, nunca escondida" (EXPERIENCE.md) — criar um Pedido derivado exigiria um novo fluxo de decisão para ele, que nenhuma AC desta epic pede; mantém o esquema simples (uma migration, sem tabela nova).

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: build limpo.
- `DATABASE_URL=postgres://stockflow:stockflow@127.0.0.1:5432/stockflow?sslmode=disable go test -p 1 -count=1 ./...` -- expected: todos os pacotes passando, incluindo os testes novos de `DecidirPedido`/`POST /api/pedidos/{id}/decisao`.
- `cd frontend && npx tsc --noEmit && npx vitest run` -- expected: sem erro de tipo, suíte Vitest 100% passando (incluindo `FilaPedidosSection.test.tsx` e `pedidos.test.ts` estendidos).

## Auto Run Result

**Resumo da mudança implementada:** `POST /api/pedidos/{id}/decisao` (só `almoxarife`+) aprova ou rejeita um Pedido `pendente`, revalidando a disponibilidade real de cada item na MESMA transação da decisão (lock ordenado por `(produto_id, estoque_id)` sobre o lote inteiro, AD-10), aprovando até o disponível real por item (nunca mais que o solicitado), debitando `produto_estoque` e registrando `movimentacoes` atomicamente, e gravando `quantidade_aprovada` por item (pendência sempre visível, nunca descartada). Status final do cabeçalho: `aprovado`/`parcialmente_aprovado`/`rejeitado`. A Fila (`FilaPedidosSection`) ganhou botões "Aprovar"/"Rejeitar" no diálogo "Ver itens" de um Pedido `pendente`, cada um atrás de um `ConfirmDialog` próprio, e a exibição de Solicitado/Aprovado/Pendente por item quando um Pedido já decidido é reaberto.

**Arquivos alterados (desde `baseline_revision`):**
- `backend/migrations/000027_add_decisao_pedidos.{up,down}.sql` — novas colunas `pedidos.decidido_por`/`decidido_em`, novo valor de status `parcialmente_aprovado`, `pedido_itens.quantidade_aprovada`.
- `backend/services/pedidos.go` — `DecidirPedido` (novo), `ErrPedidoNaoPendente` (novo), extensões de `Pedido`/`PedidoItem`, `statusPedidoValido` +`parcialmente_aprovado`, `BuscarPedidoProprio` passa a trazer `quantidade_aprovada`.
- `backend/handlers/pedidos.go` — `DecidirPedidoHandler`+`decisaoPedidoRequest` (novos).
- `backend/main.go` — registro da rota `POST /api/pedidos/{id}/decisao` atrás de `RequireRole(almoxarife)`.
- `backend/services/pedidos_test.go`, `backend/handlers/pedidos_test.go` — cobertura da I/O Matrix nos dois níveis, incluindo os 3 testes novos desta passagem de revisão para o filtro `?status=parcialmente_aprovado`.
- `frontend/src/lib/pedidos.ts`+`.test.ts` — `decidirPedido`, novos campos/literal de tipo.
- `frontend/src/components/pedidos/StatusPedidoBadge.tsx`(+`.test.tsx`) — `case 'parcialmente_aprovado'`.
- `frontend/src/components/pedidos/FilaPedidosSection.tsx`+`.test.tsx` — UI de decisão item a item na Fila.

**Revisão desta passagem (2026-09-03):** 4 reviewers em paralelo (Blind Hunter, Edge Case Hunter, Verification Gap, Intent Alignment) sobre o diff completo desde `baseline_revision`. Após classificação e verificação factual contra o schema/código real: 1 `patch` (medium) aplicado — cobertura de teste para o filtro `?status=parcialmente_aprovado` em `ListarPedidosProprios`/`ListarPedidosFila`/`GET /api/pedidos`; 3 `defer` (low) registrados no frontmatter; 14 `reject` (falsos positivos confirmados por inspeção direta do schema/código, ou comportamento literalmente mandado pelo `<intent-contract>`). Detalhe completo na entrada de `## Review Triage Log` acima.

**Verificação executada:**
- `go build ./... && go vet ./...` — limpo.
- `DATABASE_URL=postgres://stockflow:stockflow@127.0.0.1:5432/stockflow?sslmode=disable go test -p 1 -count=1 ./...` — todos os pacotes OK (`stockflow/backend`, `cmd/migrate-legado`, `cmd/seed-admin`, `handlers`, `iam`, `middleware`, `realtime`, `services`), incluindo os 3 testes novos desta passagem.
- `npx tsc --noEmit` — sem erro de tipo.
- `npx vitest run` — 43 arquivos, 521/521 testes passando.

**Riscos residuais:** os 3 itens `defer` desta passagem (quantidade fracionária não testada no fluxo de decisão; Dialog "Ver itens" não reage ao SSE `pedidos` para o próprio Pedido em exibição enquanto aberto — mitigado pelo 409 CONFLICT reativo; ausência de trava de regressão testada para `decididoPor`/`decididoEm` permanecerem `null` em `BuscarPedidoProprio`), além dos 4 itens `defer` já existentes de passagens anteriores (quebra por item ausente em "Meus Pedidos"; filtro `parcialmente_aprovado` ausente em "Meus Pedidos"; ausência de log de sucesso; decisão item a item sem batching). Nenhum é bloqueante — todos são de severidade `low`/`medium` e não correspondem a comportamento incorreto do que foi implementado nesta story.

