---
title: 'Consulta de todos os Pedidos (Fila, Almoxarife+)'
type: 'feature'
created: '2026-09-03'
status: 'done'
baseline_revision: 'de235fc8cd10aca1ca6ef7e24ec7d8375082e4f6'
review_loop_iteration: 0
followup_review_recommended: false
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-7-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      A leitura org-wide da Fila (ListarPedidosFila) não tem índice de apoio
      (pedidos.status/criado_em) nem LIMIT/paginação, e a tela FilaPedidosSection
      renderiza a lista inteira sem paginação/virtualização — ao crescer o
      volume total de Pedidos da organização (não mais por usuário), a query
      vira um full scan + sort sem teto de linhas devolvidas.
    evidence: |-
      backend/services/pedidos.go (ListarPedidosFila) reaproveita a mesma
      SELECT/ORDER BY de ListarPedidosProprios mas sem WHERE usuario_id,
      escaneando pedidos de TODOS os usuários; nenhuma migration de índice
      foi criada nesta story (Never explícito do spec-7-4); estende o gap de
      índice já deferido em spec-7-3 (usuario_id) para o caso agora sem
      nenhum filtro de dono.
    location: >-
      backend/services/pedidos.go (ListarPedidosFila) / frontend/src/components/pedidos/FilaPedidosSection.tsx
    severity: low
  - summary: >-
      O refetch disparado por evento SSE em FilaPedidosSection não tem
      debounce/coalescência: como o canal `pedidos` agora é compartilhado por
      TODOS os usuários (não só o dono, como em MeusPedidosSection), uma
      rajada de envios/decisões de vários usuários pode gerar vários refetches
      e toasts empilhados na Fila em sequência rápida.
    evidence: |-
      frontend/src/components/pedidos/FilaPedidosSection.tsx chama
      toast.info(...) + carregar() a cada evento resource==="pedidos" sem
      nenhuma janela de agrupamento — mesmo padrão (sem debounce) já usado em
      MovimentacoesSection/MeusPedidosSection, mas lá o volume de eventos por
      tela é naturalmente limitado a um usuário; na Fila, é organização
      inteira.
    location: >-
      frontend/src/components/pedidos/FilaPedidosSection.tsx
    severity: low
  - summary: >-
      Alternar entre as abas "Meus Pedidos" e "Fila" em PedidosPage reconecta
      o SSE e refaz a busca do zero a cada troca, porque o Radix TabsContent
      desmonta a seção inativa e cada *Section abre sua própria conexão
      conectarRealtime no mount.
    evidence: |-
      frontend/src/pages/PedidosPage.tsx usa Tabs/TabsContent (Radix,
      desmonta conteúdo inativo por padrão) envolvendo MeusPedidosSection e
      FilaPedidosSection, cada uma com seu próprio useEffect de
      conectarRealtime — mesmo padrão já presente em EstoquesPage
      (Locais/Movimentações), agora também em Pedidos.
    location: >-
      frontend/src/pages/PedidosPage.tsx
    severity: low
---

<intent-contract>

## Intent

**Problem:** O Almoxarife não tem hoje uma fila única de trabalho: só existe "Meus Pedidos" (Story 7.3), sempre escopado à sessão para qualquer papel — não há como ver todos os Pedidos pendentes da organização para atendê-los.

**Approach:** Acrescenta um escopo `todos` opcional em `GET /api/pedidos` (MESMA rota, `?escopo=todos`, honrado só para `almoxarife`+; qualquer outro caso cai no escopo próprio já existente — nunca erro) mais a aba "Fila" dentro da página `/pedidos`, visível só a `almoxarife`+, listando todos os Pedidos da organização, filtrável por status.

## Boundaries & Constraints

**Always:**
- `GET /api/pedidos` continua a MESMA rota — nenhuma rota nova é registrada em `main.go`. O parâmetro opcional `?escopo=todos` só é honrado quando `RankPapel(papel da sessão) >= RankPapel(PapelAlmoxarife)`; qualquer outro caso (papel insuficiente OU parâmetro ausente/com outro valor) cai no comportamento já testado de `ListarPedidosProprios`, SEM alterar seu código (Design Notes de spec-7-3: "função de leitura adicional", nunca retrabalho). `TestListarPedidosHandler_AlmoxarifeSoVeOsProprios` (`backend/handlers/pedidos_test.go`) já trava esse contrato para a chamada sem `escopo` e deve continuar passando sem alteração.
- A decisão de escopo (próprios vs. todos) fica inteiramente no `service`, molde de `ListarUsuarios` (`backend/services/usuarios.go:33`, citado por spec-1-5 como o padrão que a Epic 7 usaria): o handler passa `usuario.Papel` adiante, nunca chama `RankPapel` ele mesmo (AD-8 forma 3).
- Itens exibidos continuam sempre o SNAPSHOT em `pedido_itens` (AD-17) — a Fila reaproveita `BuscarPedidoProprio`/`buscarPedido` sem mudança para o diálogo "Ver itens".
- A aba "Fila" segue o MESMO molde de tempo real de "Meus Pedidos": `carregar` disparado só por `aoMudarStatus('conectado')` + evento SSE `resource === 'pedidos'`, nunca por `useEffect` de mount separado (AD-3).
- Badge de status continua ícone + texto (`StatusPedidoBadge`, reaproveitado sem mudança).
- A aba "Fila" (`/pedidos`) só aparece a `almoxarife`+ no cliente (`rankPapel`, molde de `CatalogoPage`); o servidor continua a autoridade real — um Usuário comum que force `?escopo=todos` recebe só os próprios (nunca 403, epics.md Story 7.4 AC2).

**Block If:** _Nenhuma decisão bloqueante — o boundary de escopo (mesma rota + parâmetro), a estrutura de abas e o padrão AD-8 já estão fixados por epics.md, EXPERIENCE.md e spec-7-3._

**Never:**
- Não implementa aprovação/rejeição (Story 7.5) nem recibo em PDF (Story 7.6) — a aba "Fila" desta story é só-leitura (lista + "Ver itens"), sem nenhuma ação de decisão.
- Não cria migration — reaproveita `pedidos`/`pedido_itens` tal qual (mesmo Never de spec-7-3); índice de performance para a listagem org-wide, se necessário, é decisão de story futura.
- Não modifica `ListarPedidosProprios` nem seus testes existentes — só adiciona `ListarPedidosFila` e a função de orquestração de escopo.
- Não publica nada no canal SSE `pedidos` — esta story só CONSOME (mesmo Never de spec-7-3).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Fila escopada a todos | almoxarife+, Pedidos de vários usuários, `GET /api/pedidos?escopo=todos` | `200`, todos os Pedidos da organização, `criado_em` DESC | No error expected |
| Fila filtrada por status | almoxarife+, `?escopo=todos&status=pendente` | `200`, só os Pedidos `pendente` de QUALQUER usuário | No error expected |
| Escopo `todos` ignorado para papel insuficiente | papel `usuario`, `?escopo=todos` | `200`, só os próprios Pedidos (mesmo resultado de sem o parâmetro) | No error expected (nunca 403) |
| Filtro de status inválido no escopo `todos` | almoxarife+, `?escopo=todos&status=banana` | Requisição rejeitada antes de consultar o banco | `400 VALIDATION_ERROR` |
| Valor de `escopo` desconhecido | almoxarife+, `?escopo=banana` | Tratado como escopo próprio (só `"todos"` ativa a Fila) | No error expected |
| Fila vazia | almoxarife+, nenhum Pedido na organização casa o filtro | `200 {"pedidos":[]}` | No error expected |

</intent-contract>

## Code Map

- `backend/services/pedidos.go` -- adicionar `ListarPedidosFila(db *sql.DB, filtroStatus string) ([]PedidoResumo, error)`: mesma validação de `statusPedidoValido`/`ErroPedidoValidacao`, mesma projeção/scan de `ListarPedidosProprios` (linhas ~261-304), mesma ordenação `ORDER BY p.criado_em DESC, p.id DESC`, mas SEM `WHERE p.usuario_id = $1` — todos os Pedidos da organização. Adicionar também `ListarPedidosParaSessao(db *sql.DB, usuarioID, papel string, escopoTodos bool, filtroStatus string) ([]PedidoResumo, error)`: se `escopoTodos && RankPapel(papel) >= RankPapel(PapelAlmoxarife)` chama `ListarPedidosFila`, senão chama `ListarPedidosProprios(db, usuarioID, filtroStatus)` inalterado — molde de decisão de `services.ListarUsuarios` (`usuarios.go:33`).
- `backend/handlers/pedidos.go` (`ListarPedidosHandler`, linhas 89-114) -- ler `escopoTodos := r.URL.Query().Get("escopo") == "todos"` além de `filtroStatus`; chamar `services.ListarPedidosParaSessao(db, usuario.ID, usuario.Papel, escopoTodos, filtroStatus)` no lugar da chamada direta a `ListarPedidosProprios`. Resto do handler (tradução de erro, `200 {"pedidos":[...]}` nunca nil) inalterado.
- `backend/main.go:623-624` -- NENHUMA mudança: `GET /api/pedidos` continua registrado exatamente como está, só `RequireAuth`.
- `backend/services/pedidos_test.go` -- adicionar testes de `ListarPedidosFila` (todos os Pedidos de vários usuários, filtro por status, filtro inválido, ordenação) e de `ListarPedidosParaSessao` (almoxarife+`escopoTodos=true` → todos; almoxarife+`escopoTodos=false` → só próprios; usuário comum+`escopoTodos=true` → só próprios, sem erro; papel `gestor`/`adm`+`escopoTodos=true` → todos, confirma `>=`).
- `backend/handlers/pedidos_test.go` -- reaproveitar o helper `getPedidos(db, authHeader, query)` (linha 290, já aceita query string livre) para testes na fronteira HTTP: `getPedidos(db, tokenAlmox, "escopo=todos")` (200, todos), `getPedidos(db, tokenUsuario, "escopo=todos")` (200, só os próprios — nunca 403), `getPedidos(db, tokenAlmox, "escopo=todos&status=banana")` (400 VALIDATION_ERROR), `getPedidos(db, tokenAlmox, "escopo=todos&status=pendente")` (200, filtrado). `seedPedidoViaServico`/`criarContaComPapel`/`seedContaComumECarrinho` já existem para os seeds.
- `frontend/src/lib/pedidos.ts` -- adicionar `listarFilaPedidos(status?: StatusPedido): Promise<PedidoResumo[]>`, molde de `listarPedidos` (linha 56): monta `/api/pedidos?escopo=todos` e acrescenta `&status=` quando informado; mesmo tratamento de erro/fallback.
- `frontend/src/lib/pedidos.test.ts` -- testes de construção de URL de `listarFilaPedidos` (com e sem `status`, sempre incluindo `escopo=todos`).
- `frontend/src/pages/MeusPedidosPage.tsx` -- MOVER para `frontend/src/components/pedidos/MeusPedidosSection.tsx`, renomeando o componente exportado de `MeusPedidosPage` para `MeusPedidosSection` (default export também renomeado); nenhuma mudança de lógica/JSX interno — só o novo local/nome, seguindo o padrão Página=Tabs+`*Section` já usado por `EstoquesPage`/`LocaisEstoqueSection`/`MovimentacoesSection`.
- `frontend/src/pages/MeusPedidosPage.test.tsx` -- MOVER para `frontend/src/components/pedidos/MeusPedidosSection.test.tsx`, ajustando só o import (`./MeusPedidosSection`) e o nome do componente renderizado; asserções inalteradas.
- `frontend/src/components/pedidos/FilaPedidosSection.tsx` (novo) -- molde EXATO de `MeusPedidosSection.tsx` (mesma árvore de estado, SSE via `conectarRealtime`, `Select` de filtro, `Dialog` "Ver itens" via `buscarPedido`), trocando só: `listarPedidos` → `listarFilaPedidos`; título `"Fila de Pedidos"`; mensagem de vazio própria (ex. "Nenhum pedido na fila."/"Nenhum pedido na fila neste status."); toast do evento SSE `"Fila de Pedidos atualizada."`; `aria-label`/`DialogTitle` do "Ver itens" no mesmo formato de `MeusPedidosSection` (solicitante — obra — data/hora, já que a Fila mostra Pedidos de VÁRIOS solicitantes, tornando esse desambiguador ainda mais necessário).
- `frontend/src/components/pedidos/FilaPedidosSection.test.tsx` (novo) -- mesmo escopo de casos de `MeusPedidosSection.test.tsx` (carregar/SSE/filtro/reconectando/vazio/erro/"Ver itens" com guarda anti-corrida), mockando `listarFilaPedidos`.
- `frontend/src/pages/PedidosPage.tsx` (novo, substitui `MeusPedidosPage.tsx` como componente da rota `/pedidos`) -- molde de `EstoquesPage.tsx`/`CatalogoPage.tsx`: `useAuth()` + `rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife')` como `podeVerFila`; `Tabs defaultValue="meus"` com `TabsTrigger value="meus"` (sempre) + `TabsContent value="meus"` (`<MeusPedidosSection />`); `TabsTrigger value="fila"` + `TabsContent value="fila"` (`<FilaPedidosSection />`) só renderizados quando `podeVerFila`.
- `frontend/src/pages/PedidosPage.test.tsx` (novo) -- usuário comum: só a aba "Meus Pedidos" existe (sem `TabsTrigger` "Fila"); almoxarife+: as duas abas existem, clicar em "Fila" monta `FilaPedidosSection` (mock) sem desmontar a conexão SSE da aba "Meus Pedidos" incorretamente (molde de `CatalogoPage.test.tsx`/`NormalizacaoPage.test.tsx` para troca de aba).
- `frontend/src/App.tsx` -- trocar `import { MeusPedidosPage } from '@/pages/MeusPedidosPage'` (linha 10) por `import { PedidosPage } from '@/pages/PedidosPage'`; trocar `{ path: 'pedidos', element: <MeusPedidosPage /> }` (linha 115) por `{ path: 'pedidos', element: <PedidosPage /> }`; ajustar o comentário do bloco (linhas ~40-41) para citar a Story 7.4/aba "Fila".
- `frontend/src/App.test.tsx` -- ajustar o teste de wiring de `/pedidos` (linhas 299-324): renderiza `PedidosPage`, heading "Meus Pedidos" continua sendo encontrado (aba default); ajustar o comentário da linha ~44-46 (mock de `EventSource`) para citar `PedidosPage`/`MeusPedidosSection`.
- `frontend/src/components/estoques/MovimentacoesSection.tsx` / `frontend/src/pages/CatalogoPage.tsx` / `frontend/src/pages/EstoquesPage.tsx` -- moldes de leitura (Tabs condicionais por papel, SSE, sem escrever nada aqui).

## Tasks & Acceptance

**Execution:**
- `backend/services/pedidos.go` -- adicionar `ListarPedidosFila` e `ListarPedidosParaSessao` conforme o Code Map -- nova leitura org-wide + orquestração de escopo no service (AD-8 forma 3), sem tocar `ListarPedidosProprios`.
- `backend/handlers/pedidos.go` -- `ListarPedidosHandler` passa a ler `?escopo=` e chamar `ListarPedidosParaSessao` -- expõe o novo escopo pela MESMA rota.
- `backend/services/pedidos_test.go` -- testes de `ListarPedidosFila`/`ListarPedidosParaSessao` cobrindo todos os papéis/combinações da I/O Matrix.
- `backend/handlers/pedidos_test.go` -- testes de `GET /api/pedidos?escopo=todos` na fronteira HTTP cobrindo toda a I/O Matrix, reaproveitando `getPedidos`.
- `frontend/src/lib/pedidos.ts` -- `listarFilaPedidos(status?)` -- cliente HTTP da Fila.
- `frontend/src/lib/pedidos.test.ts` -- testes de URL de `listarFilaPedidos`.
- `frontend/src/pages/MeusPedidosPage.tsx`+`.test.tsx` → `frontend/src/components/pedidos/MeusPedidosSection.tsx`+`.test.tsx` -- mover/renomear sem mudar lógica.
- `frontend/src/components/pedidos/FilaPedidosSection.tsx`+`.test.tsx` (novo) -- superfície principal da Fila: lista org-wide com badge/filtro/tempo real/"Ver itens", testada ponta a ponta.
- `frontend/src/pages/PedidosPage.tsx`+`.test.tsx` (novo) -- Tabs "Meus Pedidos"/"Fila" com gate de papel na aba "Fila".
- `frontend/src/App.tsx` -- registrar `PedidosPage` na rota `/pedidos` no lugar de `MeusPedidosPage`.
- `frontend/src/App.test.tsx` -- ajustar o teste de wiring de `/pedidos` para `PedidosPage`.

**Acceptance Criteria:**
- Given um `almoxarife`+ acessando Pedidos → Fila, when ele filtra por status, then vê todos os Pedidos da organização que casam o filtro, não só os próprios.
- Given um Usuário sem papel `almoxarife`+ chamando `GET /api/pedidos?escopo=todos` (mesma rota), when a requisição chega, then ele recebe só os próprios Pedidos — escopo, não erro (AD-8, epics.md Story 7.4 AC2).
- Given um Usuário sem papel `almoxarife`+, when acessa `/pedidos`, then vê só a aba "Meus Pedidos" — a aba "Fila" não existe na tela nem é alcançável por clique.
- Given um `almoxarife`+, when acessa `/pedidos`, then vê as abas "Meus Pedidos" e "Fila" e pode alternar entre elas, cada uma mostrando os Pedidos corretos para seu escopo.
- Given a aba "Fila" aberta e conectada ao canal SSE `pedidos`, when chega um evento nesse canal, then a fila rebusca sozinha e um toast discreto avisa — a tela nunca se recarrega inteira.

## Spec Change Log

## Review Triage Log

### 2026-09-03 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 3 (high 0, medium 0, low 3)
- defer: 3 (high 0, medium 0, low 3)
- reject: 6
- addressed_findings:
  - `[low]` `[patch]` `ListarPedidosFila`/`ListarPedidosParaSessao` (`backend/services/pedidos_test.go`) só provavam que a Fila inclui Pedidos de OUTROS usuários — nenhum teste seedava um Pedido do próprio almoxarife atuante para confirmar que ele também aparece na Fila (o código já estava correto, sem filtro de exclusão por dono). `TestListarPedidosParaSessao_AlmoxarifeEscopoTodos` estendido para seedar um Pedido do `almox` além do `outro`, asserindo que os dois aparecem.
  - `[low]` `[patch]` `PedidosPage.tsx` usava `Tabs` não-controlado (`defaultValue="meus"`); se `podeVerFila` virasse `false` em runtime (ex. rebaixamento de papel refletido sem reload) com a aba "Fila" ativa, o gatilho/conteúdo da aba somem do JSX mas o estado interno do Radix podia continuar apontando para `"fila"`, deixando a área de conteúdo em branco. Corrigido com `key={String(podeVerFila)}` no `Tabs` (força remount de volta ao `defaultValue` quando o gate muda); teste de regressão adicionado em `PedidosPage.test.tsx` (renderiza como almoxarife, seleciona "Fila", rebaixa o papel, confirma que "Meus Pedidos" volta a ser exibido, sem painel em branco).
  - `[low]` `[patch]` `listarFilaPedidos` (`frontend/src/lib/pedidos.ts`) reaproveitava `MENSAGEM_ERRO_LISTAR` ("...seus pedidos...") como fallback de erro numa resposta não-ok sem `error.message` — copy de "Meus Pedidos" vazando para a tela da Fila. Nova constante `MENSAGEM_ERRO_LISTAR_FILA` adicionada e usada só em `listarFilaPedidos`; teste acrescentado em `pedidos.test.ts` cobrindo resposta não-ok sem corpo de erro (o teste anterior desse caminho sempre fornecia mensagem do servidor, nunca exercitava o fallback).

## Design Notes

- **Mesma rota, escopo por parâmetro — não uma rota nova:** a leitura mais direta de epics.md Story 7.4 AC2 ("Usuário sem papel almoxarife+ chamando a MESMA rota") é que `GET /api/pedidos` ganha um parâmetro opcional, não uma rota irmã. Isso também é o único jeito de manter `TestListarPedidosHandler_AlmoxarifeSoVeOsProprios` (que chama a rota SEM parâmetro) intocado: sem parâmetro, o comportamento é idêntico ao de hoje para qualquer papel.
- **Página=Tabs+Section, não uma segunda rota:** EXPERIENCE.md/epics.md descrevem "Pedidos → Fila" como ABA dentro de Pedidos, não uma URL própria — mesmo padrão de `EstoquesPage` (Locais/Movimentações) e `CatalogoPage` (Cadastro/Importação condicional por papel). A extração de `MeusPedidosPage` para `MeusPedidosSection` é o único jeito de reaproveitar aquela lógica dentro de `Tabs` sem duplicá-la; o `Never` acima proíbe qualquer mudança de COMPORTAMENTO nela — é puramente mover + renomear.
- **`FilaPedidosSection` como cópia deliberada, não abstração compartilhada:** o projeto já tem várias seções de listagem+SSE independentes (`CatalogoListagem`, `MovimentacoesSection`, `MeusPedidosSection`) sem hook/componente genérico compartilhado — cada "molde de X" no código é uma cópia intencional, não uma dependência. `FilaPedidosSection` segue a mesma convenção.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test ./...` -- expected: build limpo, todos os testes (incluindo os novos de `ListarPedidosFila`/`ListarPedidosParaSessao` e da fronteira HTTP) passando, `TestListarPedidosHandler_AlmoxarifeSoVeOsProprios` inalterado e passando.
- `cd frontend && npx tsc --noEmit && npx vitest run` -- expected: sem erro de tipo, suíte Vitest 100% passando (incluindo `MeusPedidosSection.test.tsx` movido, `FilaPedidosSection.test.tsx`, `PedidosPage.test.tsx` e o `App.test.tsx` ajustado).

## Auto Run Result

**Resumo da mudança implementada:** a Fila do Almoxarife (Story 7.4) — `GET /api/pedidos` ganhou o parâmetro opcional `?escopo=todos` (mesma rota, decisão de escopo no `service`, honrado só para `almoxarife`+, nunca 403) e a página `/pedidos` ganhou a aba "Fila" (visível só a `almoxarife`+) ao lado da aba "Meus Pedidos" já existente, listando todos os Pedidos da organização, filtráveis por status, com o mesmo padrão de tempo real e o mesmo diálogo "Ver itens" da Story 7.3.

**Arquivos alterados nesta rodada:**
- `backend/services/pedidos.go` — `ListarPedidosFila` (nova: mesma projeção/ordenação de `ListarPedidosProprios`, sem filtro por `usuario_id`) e `ListarPedidosParaSessao` (nova: decide entre `ListarPedidosFila`/`ListarPedidosProprios` a partir de `escopoTodos` + `RankPapel(papel)`, molde de `ListarUsuarios`). `ListarPedidosProprios` inalterado.
- `backend/handlers/pedidos.go` — `ListarPedidosHandler` passa a ler `?escopo=` e chamar `ListarPedidosParaSessao`, repassando `usuario.Papel` sem chamar `RankPapel` no handler.
- `backend/main.go` — inalterado (mesma rota já registrada).
- `backend/services/pedidos_test.go` / `backend/handlers/pedidos_test.go` — testes novos cobrindo toda a I/O Matrix (fila com todos os usuários incl. o próprio almoxarife atuante, ordenação, filtro por status, filtro inválido, escopo ignorado para papel insuficiente, valor de escopo desconhecido, fila vazia) nos dois níveis (service e fronteira HTTP); `TestListarPedidosHandler_AlmoxarifeSoVeOsProprios` (Story 7.3) inalterado.
- `frontend/src/lib/pedidos.ts` / `.test.ts` — `listarFilaPedidos(status?)` (novo cliente HTTP da Fila, com fallback de erro próprio `MENSAGEM_ERRO_LISTAR_FILA`).
- `frontend/src/pages/MeusPedidosPage.tsx`+`.test.tsx` → `frontend/src/components/pedidos/MeusPedidosSection.tsx`+`.test.tsx` — movidos/renomeados, sem mudança de comportamento.
- `frontend/src/components/pedidos/FilaPedidosSection.tsx`+`.test.tsx` (novos) — seção "Fila" (cópia deliberada de `MeusPedidosSection`, usando `listarFilaPedidos`).
- `frontend/src/pages/PedidosPage.tsx`+`.test.tsx` (novos) — `Tabs` "Meus Pedidos"/"Fila" com a aba "Fila" gateada por `rankPapel(papel) >= rankPapel('almoxarife')` no cliente; `key={String(podeVerFila)}` evita painel em branco se o papel mudar em runtime.
- `frontend/src/App.tsx` / `.test.tsx` — rota `/pedidos` trocada de `MeusPedidosPage` para `PedidosPage`.

**Review findings desta rodada:** patch 3 (high 0, medium 0, low 3) — todos corrigidos nesta rodada (ver Review Triage Log); defer 3 (todos low, riscos de performance/UX org-wide já conscientemente aceitos, ver frontmatter `deferred`); reject 6 (ruído ou padrões pré-existentes já aceitos em stories anteriores — guarda de unmount desnecessária em React 19, duplicação deliberada entre seções, nit de teste sem gap real, ausência de spinner no filtro — padrão já existente, colisão de `aria-label` já mitigada pelo desambiguador de data/hora, ausência de log de auditoria para o downgrade silencioso de escopo — fora do escopo desta story).

**Recomendação de follow-up review:** `false` — nesta rodada, todos os 3 patches foram `low`: `3×0 (medium) + 1×3 (low) = 3 < 5`.

**Verificação executada:** cluster PostgreSQL 16 local (127.0.0.1:5432, role/db `stockflow`).
- `cd backend && go build ./... && go vet ./...` — limpo.
- `DATABASE_URL=postgres://stockflow:stockflow@127.0.0.1:5432/stockflow?sslmode=disable go test -p 1 -count=1 ./...` — todos os pacotes OK, incluindo `services` (91.7s) e `handlers` (108.7s) com os testes novos/estendidos desta story.
- `cd frontend && npx tsc --noEmit` — sem erro de tipo.
- `npx vitest run` — 504 testes, 43 arquivos, 100% passando.
- Auditoria da Matriz I/O: as 6 linhas da matriz têm cada uma pelo menos um teste cobrindo-a, e todos rodaram e passaram na saída de verificação acima.

**Riscos residuais:** os 3 itens deferidos nesta rodada (índice/paginação ausentes na Fila org-wide, refetch SSE sem debounce agora compartilhado pela organização inteira, reconexão SSE a cada troca de aba) são todos `low` e não bloqueiam esta story — nenhum é um bug de correção, todos são trade-offs de escala/performance já registrados para atenção futura caso o volume de Pedidos cresça.
