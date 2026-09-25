---
title: 'Pedidos no padrão de lista'
type: 'feature'
created: '2026-09-25'
status: 'done'
baseline_revision: 'bfd4056856c629dede835a905920aaf1ddbe2abe'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-17-context.md'
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** `MeusPedidosSection` e `FilaPedidosSection` não têm faixa de indicadores (Pendentes, Aprovados no mês, Rejeitados no mês) nem o visual de linha de 60px com ícone redondo e subtítulo compacto aprovado no Épico 17. Os componentes `FaixaIndicadores` e `PilhulaStatus` criados na Story 17.3 já existem em `components/lista/` prontos para reúso.

**Approach:** Backend: novo endpoint `GET /e/{slug}/api/pedidos/indicadores` com escopo automático por papel (próprios vs. todos), registrado antes de `/{id}`. Frontend: adicionar `buscarIndicadoresPedidos` em `lib/pedidos.ts`; adicionar `FaixaIndicadores` em ambas as seções com fetch próprio; redesenhar linhas para 60px com ícone `ClipboardList` e subtítulo compacto; remover wrapper `<Card>` das seções (páginas já têm `p-6`).

## Boundaries & Constraints

**Always:**
- `FaixaIndicadores` recebe `indicadores` como props — nunca faz fetch diretamente.
- `MeusPedidosSection` chama `buscarIndicadoresPedidos()` (sem escopo); `FilaPedidosSection` chama `buscarIndicadoresPedidos('todos')`.
- Se `/indicadores` falhar (rede ou status não-OK): tela continua funcionando, todos os valores da faixa mostram `"—"`.
- Backend: `GET /e/{slug}/api/pedidos/indicadores` atrás de `RequireAuth` (usuario+). Se `?escopo=todos` E `RankPapel(papel) >= RankPapel(PapelAlmoxarife)`: conta todos os pedidos da empresa. Caso contrário (papel insuficiente OU sem `?escopo=todos`): conta só os do próprio usuário — nunca retorna 403 (mesmo comportamento de `ListarPedidosParaSessao`).
- Rota registrada ANTES de `GET /e/{slug}/api/pedidos/{id}` (linha 858 de `main.go`) para evitar conflito de path.
- Indicadores: `Pendentes` = `status='pendente'`; `AprovadosMes` = `status IN ('aprovado','parcialmente_aprovado') AND decidido_em >= date_trunc('month', CURRENT_DATE)`; `RejeitadosMes` = `status='rejeitado' AND decidido_em >= date_trunc('month', CURRENT_DATE)`.
- `Pendentes > 0` → `alerta: true` na `FaixaIndicadores` → cor destructive/vermelho FC.
- `carregarIndicadores` é um `useCallback` acionado nos mesmos eventos que já acionam `carregar`: no `aoMudarStatus('conectado')` e em evento SSE `resource === 'pedidos'`. Não depende do filtro de status selecionado (indicadores são totais globais).
- Linhas: `flex items-center gap-3` com `h-[60px] border-b border-border`; col1 com ícone `ClipboardList` 32px `rounded-full bg-muted p-1.5`; col2 com `solicitante` (font-medium) e subtítulo `text-label text-muted-foreground` no formato `"obraCentroCusto · N iten(s) · data"`; col3 com `StatusPedidoBadge` + botão "Ver itens" (alinhados à direita, sem mudança de comportamento).
- Remover wrappers `<Card>/<CardHeader>/<CardContent>` de ambas as seções; remover `p-6` do container da seção (páginas já aplicam `p-6`).
- `StatusPedidoBadge`, Dialog de itens, filtro por status, SSE, botões de decisão em `FilaPedidosSection` e download de recibo: inalterados.
- Textos em PT BR.

**Block If:** —

**Never:** Alterar backend de listagem/decisão/recibo nem regras de negócio; aplicar padrão em outras páginas (17.5); usar `PilhulaStatus` no lugar de `StatusPedidoBadge` (que já é semântico/colorido); adicionar paginação.

## I/O & Edge-Case Matrix

| Cenário | Estado/Entrada | Esperado | Erro |
|---|---|---|---|
| Indicadores OK (usuario) | `MeusPedidosSection` carrega | Pendentes/Aprovados no mês/Rejeitados no mês dos próprios pedidos | — |
| Indicadores OK (almoxarife) | `FilaPedidosSection` carrega | Totais de toda a empresa (escopo=todos) | — |
| /indicadores falha | rede indisponível | tela funciona, faixa mostra "—" para os três | — |
| Pendentes > 0 | há pedidos pendentes | valor em vermelho FC (alerta=true) | — |
| Pendentes = 0 | sem pendentes | "0" sem alerta (cor normal) | — |
| SSE evento resource="pedidos" | evento chega | carregarIndicadores + carregar acionados | — |
| almoxarife, /indicadores sem escopo=todos | chamada incorreta | backend conta só próprios (não 403) | — |

</intent-contract>

## Code Map

- `backend/services/pedidos.go:100` — adicionar após as vars de erro: struct `IndicadoresPedido{Pendentes,AprovadosMes,RejeitadosMes int}` + função `IndicadoresPedidos(db *sql.DB, empresaID, usuarioID, papel string, escopoTodos bool) (IndicadoresPedido, error)` — uma query com `COUNT(*) FILTER (WHERE ...)` para os 3 indicadores; se `escopoTodos && RankPapel(papel) >= RankPapel(PapelAlmoxarife)` omite `usuario_id`, senão adiciona `AND usuario_id = $2` (nunca retorna erro de autorização — mesmo padrão de `ListarPedidosParaSessao`)
- `backend/handlers/pedidos.go:150` — junto a `ListarPedidosHandler`; novo `IndicadoresPedidosHandler(db *sql.DB)` — lê `usuario` e `empresa` do contexto, lê `escopoTodos := r.URL.Query().Get("escopo") == "todos"`, chama `services.IndicadoresPedidos(db, empresa.ID, usuario.ID, usuario.Papel, escopoTodos)`; retorna `{"pendentes":N,"aprovadosMes":N,"rejeitadosMes":N}`
- `backend/main.go:857` — inserir `registrar("GET /e/{slug}/api/pedidos/indicadores", middleware.RequireAuth(db, jwtSecret)(handlers.IndicadoresPedidosHandler(db)))` entre as linhas atuais 857 e 858 (antes de `/{id}`)
- `backend/services/pedidos_test.go` — adicionar `TestIndicadoresPedidos_*`: escopo próprio (usuario), escopo todos (almoxarife), pendentes correto, aprovados/rejeitados no mês corretos, empresa isolada (pedidos de outra empresa não aparecem)
- `frontend/src/lib/pedidos.ts:159` — adicionar `buscarIndicadoresPedidos(escopo?: 'todos'): Promise<{pendentes:number,aprovadosMes:number,rejeitadosMes:number}>` chamando `GET /api/pedidos/indicadores[?escopo=todos]`; erro → throw (chamador trata)
- `frontend/src/components/lista/FaixaIndicadores.tsx` — reutilizar sem modificação (criado em 17.3)
- `frontend/src/components/pedidos/MeusPedidosSection.tsx:1` — (a) importar `FaixaIndicadores`; (b) `useState<{pendentes,aprovadosMes,rejeitadosMes}|null>(null)` para indicadores; (c) `carregarIndicadores` useCallback chamando `buscarIndicadoresPedidos()`, em try/catch define null no erro; (d) acionar `carregarIndicadores` em `aoMudarStatus('conectado')` e em evento SSE `resource==='pedidos'`; (e) montar `<FaixaIndicadores>` entre o Select de filtro e o bloco de lista, mapeando `[{rotulo:'Pendentes',valor:ind?.pendentes??null,alerta:true},{rotulo:'Aprovados no mês',valor:ind?.aprovadosMes??null},{rotulo:'Rejeitados no mês',valor:ind?.rejeitadosMes??null}]`; (f) redesenhar `<li>`: `flex items-center gap-3 h-[60px] border-b border-border`, ícone `ClipboardList` 32px roundado, col2 com `solicitante` + subtítulo `obraCentroCusto · N iten(s) · data`; (g) remover `<Card>/<CardHeader>/<CardContent>`; container vira `<div className="flex flex-col gap-4">`
- `frontend/src/components/pedidos/FilaPedidosSection.tsx:1` — mesmas mudanças (a)-(g); usa `buscarIndicadoresPedidos('todos')` em `carregarIndicadores`
- `frontend/src/components/pedidos/MeusPedidosSection.test.tsx` — adicionar: mock `buscarIndicadoresPedidos`; testa faixa com valores corretos; `/indicadores` falha → "—" visível; SSE dispara carregarIndicadores
- `frontend/src/components/pedidos/FilaPedidosSection.test.tsx` — mesmos cenários; verifica que `buscarIndicadoresPedidos` foi chamado com `'todos'`

## Tasks & Acceptance

**Execution:**
- `backend/services/pedidos.go` — adicionar `IndicadoresPedidos` struct e `IndicadoresPedidosFunc`
- `backend/handlers/pedidos.go` — adicionar `IndicadoresPedidosHandler`
- `backend/main.go` — registrar rota `GET /e/{slug}/api/pedidos/indicadores` antes de `/{id}`
- `backend/services/pedidos_test.go` — testes de `IndicadoresPedidosFunc`
- `frontend/src/lib/pedidos.ts` — adicionar `buscarIndicadoresPedidos`
- `frontend/src/components/pedidos/MeusPedidosSection.tsx` — FaixaIndicadores + 60px rows + sem Card
- `frontend/src/components/pedidos/FilaPedidosSection.tsx` — mesma atualização + escopo=todos
- `frontend/src/components/pedidos/MeusPedidosSection.test.tsx` — testes de indicadores
- `frontend/src/components/pedidos/FilaPedidosSection.test.tsx` — testes de indicadores

**Acceptance Criteria:**
- Given `MeusPedidosSection` carrega, when página `/pedidos` abre, then faixa exibe Pendentes/Aprovados no mês/Rejeitados no mês buscados de `/pedidos/indicadores` (sem `?escopo=todos`).
- Given `FilaPedidosSection` carrega, when página `/pedidos/fila` abre, then faixa exibe indicadores buscados de `/pedidos/indicadores?escopo=todos`.
- Given `/pedidos/indicadores` retorna erro, when qualquer seção carrega, then tela funciona e todos os valores da faixa mostram `"—"`.
- Given Pendentes > 0, when faixa renderiza, then valor "Pendentes" aparece em cor destructive (vermelho FC).
- Given evento SSE `resource="pedidos"` chega, when a conexão está ativa, then indicadores e lista são recarregados.
- Given qualquer pedido na lista, when renderizado em modo tabela, then linha tem 60px de altura, ícone `ClipboardList` redondo 32px + `solicitante` + subtítulo `obraCentroCusto · N iten(s) · data`, sem zebra, sem `<Card>` wrapper.
- Given Dialog de itens, filtro por status, botões de decisão e download de recibo, when utilizados, then comportamentos existentes preservados sem regressão.

## Design Notes

Faixa de indicadores — reutiliza `FaixaIndicadores` de `components/lista/` (Story 17.3). Sem slot `acoes` (pedidos não têm ação de cadastro/exportação na faixa).

Linha 60px — ícone `ClipboardList` na esma célula de 32px roundada de `CatalogoListagem`:
```tsx
<div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted">
  <ClipboardList aria-hidden="true" className="h-4 w-4 text-muted-foreground" />
</div>
<div className="flex flex-col">
  <span className="text-body font-medium">{pedido.solicitante}</span>
  <span className="text-label text-muted-foreground truncate">
    {pedido.obraCentroCusto}
    {' · '}
    {pedido.qtdItens} {pedido.qtdItens === 1 ? 'item' : 'itens'}
    {' · '}
    {new Date(pedido.criadoEm).toLocaleDateString('pt-BR')}
  </span>
</div>
```
`StatusPedidoBadge` e botão "Ver itens" permanecem no `span` à direita sem alteração.

Backend — SQL de indicadores (escopo próprio, $1=empresaID, $2=usuarioID):
```sql
SELECT
  COUNT(*) FILTER (WHERE status = 'pendente') AS pendentes,
  COUNT(*) FILTER (WHERE status IN ('aprovado','parcialmente_aprovado')
    AND decidido_em >= date_trunc('month', CURRENT_DATE)) AS aprovados_mes,
  COUNT(*) FILTER (WHERE status = 'rejeitado'
    AND decidido_em >= date_trunc('month', CURRENT_DATE)) AS rejeitados_mes
FROM pedidos
WHERE empresa_id = $1 AND usuario_id = $2
```
Para escopo todos (almoxarife+): WHERE `empresa_id = $1` — sem `usuario_id`.

## Verification

**Commands:**
- `cd frontend && npx tsc -b && npm run lint && npm test` -- esperado: limpo e verde
- `cd backend && go test ./services/... ./handlers/...` -- esperado: verde

## Spec Change Log

## Review Triage Log
## Auto Run Result

Status: done

_Appended by the bmad-loop orchestrator (missing-marker repair, #224): the session finalized this spec's frontmatter without its `## Auto Run Result` marker, so the orchestrator synthesized the result from the frontmatter and appended this section._

Synthesized by the bmad-loop orchestrator from frontmatter status `done` for story `17-4-pedidos-no-padrão-de-lista` (session finalized the spec without appending its marker).
