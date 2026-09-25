---
title: 'Produto inativo some das operações; filtro "Inativos"'
type: 'feature'
created: '2026-09-25'
status: 'done'
baseline_revision: 'cb859d5a31cbd46781a9a1d3621edb43e2229274'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** A Story 16.1 já permite inativar um Produto, mas ele ainda aparece no Catálogo, busca, leitura por código, exportação, Normalização e importação, e ainda pode entrar no Carrinho; ninguém pode ver só os inativos para reativá-los.

**Approach:** Somar `inativado_em IS NULL` nas leituras operacionais, recusar inativo no Carrinho (409) e na importação (linha rejeitada), manter o inativo nos históricos com a marca "Inativo", e criar o filtro "Inativos" do Catálogo restrito a `gestor`+.

## Boundaries & Constraints

**Always:**
- "Ativo" = `inativado_em IS NULL AND deleted_at IS NULL`. Nunca tocar em `deleted_at`.
- Catálogo (grade, agrupado, todos os grupos) e exportação: por padrão só ativos, via `montarFiltrosCatalogo` (`FiltrosCatalogo.SomenteInativos bool`; true → só `inativado_em IS NOT NULL`). Busca (`buscarProdutosQuery`) e leitura por código (`buscarProdutoPorCodigoQuery`, que já responde 404 "não encontrado") só ativos.
- `?inativos=1` no `GET /api/produtos/catalogo`: honrado só para papel >= `gestor` (`services.RankPapel`); para `almoxarife`/`usuario` é ignorado. A exportação nunca inclui inativos.
- `AdicionarItemCarrinho` com Produto inativo → `ErrProdutoInativo` (existente) mapeado no handler para 409 `PRODUTO_INATIVO` (mesmo molde de `handlers/lotes.go`); Produto inexistente continua 404. Lançar saldo e enviar Pedido já recusam (16.1) — não refazer.
- Importação: linha cujo código pertence a Produto inativo (nos dois pontos de `buscarProdutoPorCodigo`, inclusive após `pqUniqueViolation`) é rejeitada via `rejeitarECommitar` com o texto exato "Produto inativo — reative antes de importar", sem interromper as demais.
- Normalização: `DetectarDuplicatas` e `AnalisarInconsistencias` ignoram inativos.
- Movimentações (`ListarMovimentacoes`, `ListarMovimentacoesDoUsuario`) continuam listando o inativo e passam a devolver `inativo: boolean`; a tela mostra a marca "Inativo" ao lado do nome. Pedidos (detalhe, filas) e recibo PDF continuam mostrando o item: o item de Pedido ganha `inativo` via `LEFT JOIN produtos` na leitura (o snapshot AD-17 permanece a fonte de nome/quantidade) e a UI/PDF exibem "Inativo".
- Frontend Catálogo: checkbox "Mostrar só inativos" visível só para `gestor`/`adm` (`rankPapel`); ligado, envia `inativos=1`, a lista traz só inativos e cada linha abre o detalhe (onde reativar). Linha inativa no Catálogo exibe a marca "Inativo".
- `empresa_id` sempre do contexto; nenhuma mudança de comportamento para Produtos ativos.

**Block If:** —

**Never:** Novas colunas/migrations. Reescrever `LancarSaldo`, `SubmeterPedido`, `ListarCarrinho`, inativar/reativar (16.1). Histórico de alterações (16.3). EAN único (16.4). Filtrar inativo de Movimentações/Pedidos/recibos/detalhe. Inativação em lote.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Catálogo padrão | 1 ativo + 1 inativo | só o ativo (grade, agrupado, exportação) | — |
| Busca / código | inativo | busca não o traz; leitura por código → 404 não encontrado | — |
| Filtro gestor | gestor, `?inativos=1` | só o inativo | — |
| Filtro almoxarife | almoxarife, `?inativos=1` | parâmetro ignorado: só ativos | — |
| Carrinho | POST item de inativo | — | 409 `PRODUTO_INATIVO`, nada gravado |
| Importação | planilha com 1 código inativo + 1 ativo | linha inativa `rejeitada` "Produto inativo — reative antes de importar"; a outra processa | — |
| Normalização | inativo duplicado/inconsistente | não aparece | — |
| Histórico | inativo com movimentação e pedido | Movimentações e Pedido/recibo mostram o item com `inativo: true` | — |

</intent-contract>

## Code Map

- `backend/services/catalogo.go:175,215` -- `FiltrosCatalogo` + `montarFiltrosCatalogo` (linha 221 `p.deleted_at IS NULL`); usado por `ListarCatalogoGrade`, `ListarCatalogoAgrupado`, `ListarTodosGruposCatalogo` (exportação via `services/relatorios.go:57`).
- `backend/handlers/produtos.go:421` -- `ListarCatalogoHandler` (hoje descarta o usuário na l.423; usar `usuario.Papel` + `services.RankPapel`/`services.PapelGestor`); `:518` `ExportarCatalogoHandler` (sem `inativos`).
- `backend/services/produtos.go:785,853` -- `buscarProdutosQuery`, `buscarProdutoPorCodigoQuery`.
- `backend/services/carrinho.go:152,169` -- `AdicionarItemCarrinho`; molde de distinção inativo/inexistente em `services/lotes.go:110-131`; `handlers/carrinho.go:67-74` switch de erros; `ErrProdutoInativo` em `services/produtos_inativacao.go:43`.
- `backend/services/importacoes.go:473,520,591,636,743` -- `processarProximaLinha`, `buscarProdutoPorCodigo`, `rejeitarECommitar`.
- `backend/services/normalizacao.go:217,235,882` -- `AnalisarInconsistencias`, `DetectarDuplicatas`.
- `backend/services/movimentacoes.go:48,75,135` -- `MovimentacaoHistorico`, listagens com `JOIN produtos`.
- `backend/services/pedidos.go:61,560,895` -- `PedidoItem`, `BuscarPedidoProprio` e demais leituras de itens, `ReciboPedidoItem`/`MontarReciboPedidoConteudo`.
- `frontend/src/pages/CatalogoPage.tsx:57,66` -- padrão `rankPapel(...) >= rankPapel('almoxarife')`, prop `podeExportar`.
- `frontend/src/components/catalogo/CatalogoListagem.tsx:86,104,142,150,250,503` -- tipos, `FiltrosAtivos`, `queryFiltros`, fetch, checkbox "Com estoque" (molde).
- `frontend/src/components/estoques/MovimentacoesSection.tsx:30,173`; `frontend/src/lib/pedidos.ts:56`; `components/pedidos/FilaPedidosSection.tsx:400`; `components/pedidos/MeusPedidosSection.tsx:342` -- marca "Inativo".
- Testes-molde: `services/produtos_inativacao_test.go` (`criarProdutoInativacao`), `services/catalogo_test.go`, `handlers/produtos_test.go:2397`, `CatalogoListagem.test.tsx`, `CatalogoPage.test.tsx`.

## Tasks & Acceptance

**Execution:**
- `backend/services/catalogo.go`, `services/produtos.go`, `handlers/produtos.go` -- filtro ativo por padrão, `SomenteInativos`, `?inativos=1` só gestor+.
- `backend/services/carrinho.go`, `handlers/carrinho.go` -- recusa 409 `PRODUTO_INATIVO`.
- `backend/services/importacoes.go`, `services/normalizacao.go` -- rejeição de linha e exclusão de inativos.
- `backend/services/movimentacoes.go`, `services/pedidos.go` (+ recibo PDF) -- campo `inativo` sem filtrar.
- Testes em `services/*_test.go` e `handlers/*_test.go` cobrindo toda a matriz.
- `frontend/...` (Catálogo, Movimentações, Pedidos, `lib/pedidos.ts`) + testes vitest -- filtro e marcas.

**Acceptance Criteria:**
- Given um Produto inativo, when alguém abre Catálogo, busca, leitura por código ou exporta, then ele não aparece.
- Given um gestor no Catálogo, when liga "Mostrar só inativos", then vê só os inativos e abre o detalhe; um almoxarife não vê o filtro.
- Given um inativo, when se tenta adicioná-lo ao Carrinho por API, then 409 `PRODUTO_INATIVO`.
- Given movimentações e pedidos de um Produto depois inativado, when as telas carregam, then o item aparece com "Inativo".

## Spec Change Log

## Review Triage Log

### 2026-09-25 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 4: (high 0, medium 3, low 1)
- defer: 0
- reject: muitos (ruído: itens já tratados na 16.1, comportamento pré-existente, estilo)
- addressed_findings:
  - `[medium]` `[patch]` recibo/PDF sem teste do flag Inativo — teste adicionado.
  - `[medium]` `[patch]` marcas "Inativo" em Movimentações, Fila e Meus Pedidos sem teste — testes adicionados.
  - `[medium]` `[patch]` leitura por código de inativo sem teste de handler (404) — teste adicionado.
  - `[low]` `[patch]` texto vazio do filtro "só inativos" — "Nenhum produto inativo.".

### 2026-09-25 — Review pass (revisão de acompanhamento)
- intent_gap: 0
- bad_spec: 0
- patch: 0
- defer: 0
- reject: muitos (ruído: escopo já excluído pelo contrato — SubmeterPedido/LancarSaldo da 16.1, histórico 16.3, EAN 16.4; estilo/indentação; melhorias de UX/documentação fora da intenção; corrida de importação de difícil teste, já coberta pela regra)
- addressed_findings:
  - none

## Auto Run Result

Status: done

- Mudança: Produto inativo some de Catálogo, busca, leitura por código, exportação, Normalização e importação; Carrinho recusa com 409 `PRODUTO_INATIVO`; Movimentações, Pedidos e recibo mostram "Inativo"; filtro "Mostrar só inativos" no Catálogo (gestor+).
- Arquivos: 23 em `backend/` e `frontend/` (serviços, handlers, testes Go e vitest, componentes de Catálogo, Movimentações e Pedidos).
- Revisão (passe de acompanhamento): patches 0, adiados 0, rejeitados muitos.
- Follow-up recomendado: false (patches: 0 high, 0 medium, 0 low; score 0).
- Verificação: `go build ./... && go vet ./...` OK; `npx tsc -b` OK. Testes Go/vitest não reexecutados neste passe (sem alteração de código).
- Riscos residuais: ramo de recuperação de corrida na importação sem teste dedicado.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- sem erros
- `cd backend && go test -p 1 -timeout 25m ./...` -- verde (com `DATABASE_URL`)
- `cd frontend && npx tsc -b && npx vitest run` -- verde

