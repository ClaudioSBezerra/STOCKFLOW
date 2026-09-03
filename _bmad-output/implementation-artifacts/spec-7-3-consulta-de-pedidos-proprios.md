---
title: 'Consulta de Pedidos próprios'
type: 'feature'
created: '2026-09-03'
status: 'done'
baseline_revision: 'c3d0fcf9c5fb1225c98fe73dd80ac4f9f2c48255'
review_loop_iteration: 0
followup_review_recommended: true
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-7-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      GET /api/pedidos e a contagem de itens não têm índice/escopo de query
      dedicado: falta índice em pedidos.usuario_id e a subquery de contagem
      de ListarPedidosProprios agrega pedido_itens da empresa inteira antes
      do join, não só os do usuário.
    evidence: |-
      backend/migrations/000026_create_pedidos.up.sql não cria índice em
      pedidos.usuario_id nem em pedido_itens.pedido_id; a subquery de
      contagem em ListarPedidosProprios (backend/services/pedidos.go)
      agrega pedido_itens inteiro antes do join com o p filtrado —
      inconsistente com o padrão já usado em movimentacoes
      (idx_movimentacoes_produto_id/idx_movimentacoes_criado_em, criados
      especificamente para essa forma de query).
    location: >-
      backend/services/pedidos.go (ListarPedidosProprios) /
      backend/migrations/000026_create_pedidos.up.sql
    severity: low
  - summary: >-
      O campo observacao (nota livre do solicitante no envio) é buscado e
      transportado ponta a ponta mas nunca é exibido em "Meus Pedidos" —
      nem na lista, nem no diálogo "Ver itens".
    evidence: |-
      MeusPedidosPage.tsx nunca lê pedido.observacao/detalhe.observacao em
      nenhum JSX; o Code Map/Tasks desta story enumeram explicitamente só
      solicitante, obra, data, badge, qtd de itens e "Ver itens" — não
      incluem a observação, então não é um requisito claro desta story,
      mas o dado já chega ao cliente sem uso.
    location: >-
      frontend/src/pages/MeusPedidosPage.tsx
    severity: low
  - summary: >-
      A invariância de snapshot do AD-17 (rótulo do item não muda se o
      Produto for editado/mesclado depois) só tem teste de correção no
      momento da leitura, não de invariância ao longo do tempo.
    evidence: |-
      TestBuscarPedidoProprio_DonoComItens e
      TestBuscarPedidoProprio_ItensOrdenadosPorNome
      (backend/services/pedidos_test.go) só provam correção no momento da
      leitura; a query em BuscarPedidoProprio de fato nunca faz join com
      produtos/estoques, então a implementação está correta hoje, mas
      nenhum teste edita o Produto/Estoque depois do envio e rebusca o
      Pedido para confirmar que o rótulo do item permanece congelado.
    location: >-
      backend/services/pedidos_test.go (BuscarPedidoProprio)
    severity: low
---

<intent-contract>

## Intent

**Problem:** A Story 7.2 passou a criar Pedidos `pendente`, mas o solicitante não tem hoje nenhuma tela para acompanhar o que enviou — não existe rota de leitura de Pedidos nem a página "Meus Pedidos" (o item de nav "Pedidos" cai no `PlaceholderPage`).

**Approach:** Duas rotas de leitura atrás só de `RequireAuth` — `GET /api/pedidos` (lista os Pedidos do próprio usuário da sessão, filtrável por `status`) e `GET /api/pedidos/{id}` (cabeçalho + itens em snapshot de um Pedido, liberado ao dono ou a `almoxarife`+ pelo padrão de escopo AD-8) — mais a página `/pedidos` ("Meus Pedidos"): lista com badge de status (ícone + texto), filtro por status e atualização em tempo real pelo canal SSE `pedidos` já existente.

## Boundaries & Constraints

**Always:**
- A identidade de escopo (listagem e acesso por id) vem sempre de `middleware.UsuarioDaSessao` e do papel já resolvido no contexto da requisição — nunca re-consulta o banco por papel, nunca lê `usuarioId` de corpo/query (AD-8, epic-7-context.md).
- `GET /api/pedidos` devolve só os Pedidos cujo `pedidos.usuario_id` é o da sessão (filtro de escopo no `service`), ordenados por `criado_em` DESC. Filtro opcional `?status=` restrito a `pendente|aprovado|rejeitado`; valor fora desse conjunto → `400 VALIDATION_ERROR` antes de tocar o banco; ausente/vazio → sem filtro de status.
- `GET /api/pedidos/{id}` devolve o Pedido só se o solicitante for o dono da sessão OU `RankPapel(papel da sessão) >= RankPapel(PapelAlmoxarife)`. Qualquer outro caso (Pedido de outro usuário sem papel suficiente, id inexistente, id malformado) → `404 NOT_FOUND` com a MESMA mensagem — nunca revela a existência de um Pedido alheio, nunca responde `403`.
- Itens exibidos vêm sempre do SNAPSHOT em `pedido_itens` (`produto_nome`/`categoria_nome`/`estoque_nome`/`quantidade`) — nunca um join ao vivo com `produtos`/`estoques` (AD-17, epic-7-context.md).
- A página "Meus Pedidos" carrega e rebusca pelo MESMO caminho de código, disparado por `aoMudarStatus('conectado')` (inclusive a 1ª conexão) e por eventos SSE com `resource === 'pedidos'` — nunca por um `useEffect` de mount separado (AD-3, molde de `MovimentacoesSection`). Um evento dispara um `toast.info` discreto + o refetch; a tela nunca se auto-recarrega inteira. Status `'reconectando'` mostra um `<output aria-live="polite">Reconectando...</output>`.
- Badge de status: formato pill, SEMPRE ícone + texto (nunca só cor), texto na variante `text-on-tint-*` correspondente (UX-DR6/UX-DR10, epic-7-context.md); `switch` exaustivo sobre o status com `default` genérico para um valor inesperado.

**Block If:** _Nenhuma decisão bloqueante — o boundary de escopo (próprios vs. todos) já está fixado pelo epic-7-context.md e pela Story 7.2; a Fila do Almoxarife é a Story 7.4._

**Never:**
- Não implementa a Fila do Almoxarife (`Pedidos → Fila`, aba visível só a `almoxarife`+, listagem de TODOS os Pedidos da organização) — é a Story 7.4. Esta story entrega só "Meus Pedidos" e o acesso por id com o padrão de escopo já embutido no `service`.
- Não implementa aprovação/rejeição (7.5), recibo em PDF (7.6) nem migração de Pedidos legados (7.7).
- Não publica nada no canal SSE `pedidos` — esta story só CONSOME eventos; a publicação de mudança de status é da Story 7.5 (o `change:"created"` do envio já é publicado pela Story 7.2).
- Não cria migration — as tabelas `pedidos`/`pedido_itens` já existem (`000026`, Story 7.2).
- Não adiciona paginação/limite à listagem de "Meus Pedidos" (volume por usuário é baixo); se um teto for necessário, é decisão de uma story futura, como em `movimentacoes`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Lista escopada ao dono | Usuário A tem 2 Pedidos, Usuário B tem 1 | `GET /api/pedidos` (sessão A) → `200 {"pedidos":[...]}` só com os 2 de A, `criado_em` DESC | No error expected |
| Filtro por status | `?status=aprovado`, sessão com Pedidos em vários status | `200`, só os Pedidos `aprovado` do próprio usuário | No error expected |
| Filtro de status inválido | `?status=banana` | Requisição rejeitada antes de consultar o banco | `400 VALIDATION_ERROR` |
| Sem Pedidos | Usuário sem nenhuma linha em `pedidos` | `200 {"pedidos":[]}` | No error expected |
| Detalhe pelo dono | `GET /api/pedidos/{id}` de um Pedido do próprio usuário | `200 {"pedido":{...cabeçalho, itens:[snapshot]}}` | No error expected |
| Detalhe de Pedido alheio, papel `usuario` | `GET /api/pedidos/{id de B}` (sessão A, papel `usuario`) | Acesso negado sem revelar existência | `404 NOT_FOUND` |
| Detalhe de Pedido alheio, papel `almoxarife`+ | mesmo id, sessão com papel `almoxarife` | `200` com cabeçalho + itens (padrão de escopo, AD-8) | No error expected |
| Detalhe com id inexistente/malformado | `GET /api/pedidos/nao-e-uuid` | Acesso negado, mesma resposta de "não existe" | `404 NOT_FOUND` |

</intent-contract>

## Code Map

- `backend/services/pedidos.go` (Story 7.2) -- adicionar aqui: struct `PedidoItem` (snapshot: `ProdutoID`, `ProdutoNome`, `CategoriaNome`, `EstoqueID`, `EstoqueNome`, `Quantidade float64`), struct `PedidoDetalhe` (embute `Pedido` + `Itens []PedidoItem`), struct `PedidoResumo` (campos de `Pedido` + `QtdItens int`), `var ErrPedidoNaoEncontrado = errors.New(...)`, `var statusPedidoValido = map[string]bool{"pendente":true,"aprovado":true,"rejeitado":true}`. Reaproveitar o struct `Pedido` existente (linhas ~19-27) e o `ErroPedidoValidacao` existente (linhas ~34-40, molde para o erro de filtro inválido).
- `backend/services/pedidos.go` -- `ListarPedidosProprios(db *sql.DB, usuarioID, filtroStatus string) ([]PedidoResumo, error)`: se `filtroStatus != ""` e não estiver em `statusPedidoValido` → `&ErroPedidoValidacao{Mensagem: "status inválido"}` (nenhuma query). `SELECT ... FROM pedidos p LEFT JOIN (SELECT pedido_id, count(*) qtd FROM pedido_itens GROUP BY pedido_id) i ON i.pedido_id = p.id WHERE p.usuario_id = $1 [AND p.status = $2] ORDER BY p.criado_em DESC`. Molde de leitura/scan: `ListarMovimentacoes` (`backend/services/movimentacoes.go:72`).
- `backend/services/pedidos.go` -- `BuscarPedidoProprio(db *sql.DB, pedidoID, usuarioID, papel string) (PedidoDetalhe, error)`: `SELECT id, usuario_id, solicitante, obra_centro_custo, observacao, status, criado_em FROM pedidos WHERE id = $1`; `sql.ErrNoRows` OU erro de sintaxe de UUID (`22P02`, mesmo colapso de `ObterProdutoDetalhe`) → `ErrPedidoNaoEncontrado`. Se `pedido.UsuarioID != usuarioID` E `RankPapel(papel) < RankPapel(PapelAlmoxarife)` → `ErrPedidoNaoEncontrado`. Senão, `SELECT produto_id, produto_nome, categoria_nome, estoque_id, estoque_nome, quantidade FROM pedido_itens WHERE pedido_id = $1 ORDER BY produto_nome` e devolve `PedidoDetalhe`. `RankPapel`/`PapelAlmoxarife`: `backend/services/papel.go`.
- `backend/handlers/pedidos.go` (Story 7.2) -- adicionar `ListarPedidosHandler(db *sql.DB) http.HandlerFunc` (`GET /api/pedidos`: `usuario := middleware.UsuarioDaSessao`, lê `r.URL.Query().Get("status")`, chama `services.ListarPedidosProprios`, `errors.As` em `*services.ErroPedidoValidacao` → `400 VALIDATION_ERROR`, sucesso → `escreverJSON(w, 200, map[string]any{"pedidos": resumos})` com `resumos` nunca nil → `[]`) e `BuscarPedidoHandler(db *sql.DB) http.HandlerFunc` (`GET /api/pedidos/{id}`: `services.BuscarPedidoProprio(db, r.PathValue("id"), usuario.ID, usuario.Papel)`, `errors.Is(err, services.ErrPedidoNaoEncontrado)` → `404 NOT_FOUND` "pedido não encontrado", sucesso → `200 {"pedido": detalhe}`). Molde exato de erro/escopo: `ListarMovimentacoesHandler` (`backend/handlers/movimentacoes.go:97`) e o guard de contexto de `SubmeterPedidoHandler` (mesmo arquivo).
- `backend/main.go:610-613` (registro de `POST /api/pedidos`) -- acrescentar logo abaixo, mesma composição só com `RequireAuth`: `mux.HandleFunc("GET /api/pedidos", middleware.RequireAuth(db, jwtSecret)(handlers.ListarPedidosHandler(db)))` e `mux.HandleFunc("GET /api/pedidos/{id}", middleware.RequireAuth(db, jwtSecret)(handlers.BuscarPedidoHandler(db)))` (mux Go 1.22: os dois padrões coexistem com o `POST`).
- `backend/services/pedidos_test.go` -- helpers `testDB`, `limparProdutos`, `semearConta`, `seedProdutoComSaldo`, `AdicionarItemCarrinho`, `listarPedidoItens` já existem; adicionar testes de `ListarPedidosProprios` (escopo do dono, filtro por status, filtro inválido sem query, ordem DESC, `QtdItens`) e `BuscarPedidoProprio` (dono com itens; outro usuário papel `usuario` → `ErrPedidoNaoEncontrado`; `almoxarife` vê de qualquer um; id malformado → `ErrPedidoNaoEncontrado`).
- `backend/handlers/pedidos_test.go` -- helpers `testDB`, `limparProdutosHandler`, `seedContaComumECarrinho`, `criarContaComPapel`, `tokenDeLogin`, `seedProdutoComSaldoHandler`, `postItemCarrinho`, `postPedido` já existem; adicionar `getPedidos(db, auth, query)` / `getPedido(db, auth, id)` (molde de `getCarrinho`, `backend/handlers/carrinho_test.go:42`) e cobrir a I/O Matrix na fronteira HTTP.
- `frontend/src/lib/promocao.ts` -- molde de módulo `.ts` puro (sem Context) para o novo `frontend/src/lib/pedidos.ts`: tipos `StatusPedido = 'pendente'|'aprovado'|'rejeitado'`, `PedidoResumo`, `PedidoItem`, `PedidoDetalhe`; `listarPedidos(status?: StatusPedido): Promise<PedidoResumo[]>` (`GET /api/pedidos` + `?status=` quando informado) e `buscarPedido(id: string): Promise<PedidoDetalhe>`; `authHeaders()` copiado de `carrinho.tsx` (`getAccessToken`). Resposta não-ok → `throw new Error(body.error?.message ?? <fallback>)`.
- `frontend/src/components/estoques/MovimentacoesSection.tsx` -- MOLDE COMPLETO da página `/pedidos`: `conectarRealtime` no `useEffect`, `carregar` só chamado de `aoMudarStatus('conectado')` e de `evento.resource === 'pedidos'`, `seqRef` anti-corrida, três estados (carregando/erro/vazio), `<output aria-live="polite">Reconectando...</output>` em `'reconectando'`, `toast.info('Meus Pedidos atualizada.')` no evento.
- `frontend/src/pages/CarrinhoPage.tsx` -- molde de página filha de `RotaProtegida` sem gate de papel próprio (`Card`/`CardHeader`/`CardContent`, `p-6`, `Dialog` para o detalhe de itens) e de uso de `formatarQuantidade` (`frontend/src/components/catalogo/formatacao`).
- `frontend/src/components/catalogo/CatalogoListagem.tsx:8,455-491` -- molde de `Select`/`SelectTrigger`/`SelectItem` com opção "Todos" para o filtro de status (`min-h-touch-target-min` no trigger).
- `frontend/src/index.css:27-33,72-76` -- tokens `--color-text-on-tint-{success,warning,info,destructive}` e `--radius-full` para o `StatusPedidoBadge`.
- `frontend/src/App.tsx:104-121` -- acrescentar `{ path: 'pedidos', element: <MeusPedidosPage /> }` como rota-filha da raiz (dentro de `AppShell`/`RotaProtegida`), com o import correspondente; o item de nav `pedidos → /pedidos` já existe em `frontend/src/components/shell/nav-items.ts` (papelMinimo `usuario`).
- `frontend/src/pages/ProdutoDetalhePage.test.tsx:46-70` e `frontend/src/components/estoques/MovimentacoesSection.test.tsx` -- molde de mock de `conectarRealtime` (captura `aoReceberEvento`/`aoMudarStatus`) para os testes da nova página.

## Tasks & Acceptance

**Execution:**
- `backend/services/pedidos.go` -- adicionar `PedidoItem`/`PedidoResumo`/`PedidoDetalhe`, `ErrPedidoNaoEncontrado`, `statusPedidoValido`, `ListarPedidosProprios` e `BuscarPedidoProprio` conforme o Code Map -- leitura de Pedidos escopada por sessão + padrão de escopo AD-8 no acesso por id.
- `backend/handlers/pedidos.go` -- adicionar `ListarPedidosHandler` e `BuscarPedidoHandler` (`GET /api/pedidos`, `GET /api/pedidos/{id}`), traduzindo `ErroPedidoValidacao` → `400` e `ErrPedidoNaoEncontrado` → `404 NOT_FOUND` -- fronteira HTTP, sem regra de negócio própria.
- `backend/main.go` -- registrar as duas rotas GET atrás só de `RequireAuth`, abaixo do `POST /api/pedidos` -- expõe a API de leitura (mesmo mínimo de papel do envio).
- `backend/services/pedidos_test.go` -- testes de `ListarPedidosProprios` e `BuscarPedidoProprio` cobrindo escopo, filtro, filtro inválido, ordenação, `QtdItens` e as três variações de acesso por id -- molde de `carrinho_test.go`/`movimentacoes_test.go`.
- `backend/handlers/pedidos_test.go` -- helpers `getPedidos`/`getPedido` + testes cobrindo toda a I/O Matrix na fronteira HTTP (200 escopado, filtro, `400` filtro inválido, `[]` vazio, `200` detalhe do dono, `404` de Pedido alheio como `usuario`, `200` como `almoxarife`, `404` id malformado).
- `frontend/src/lib/pedidos.ts` (novo) -- tipos + `listarPedidos(status?)` e `buscarPedido(id)` -- cliente HTTP puro da nova API.
- `frontend/src/lib/pedidos.test.ts` (novo) -- URL/headers corretos (com e sem `?status=`), parse do corpo, propagação de erro do servidor -- convenção Vitest do projeto.
- `frontend/src/components/pedidos/StatusPedidoBadge.tsx` (novo) -- pill ícone + texto (`Clock`/`CheckCircle2`/`XCircle` de `lucide-react`), classe de tint por status, `switch` exaustivo com `default` -- UX-DR6/UX-DR10.
- `frontend/src/components/pedidos/StatusPedidoBadge.test.tsx` (novo) -- cada status renderiza ícone + rótulo textual; status desconhecido cai no rótulo genérico, nunca só cor.
- `frontend/src/pages/MeusPedidosPage.tsx` (novo) -- página `/pedidos` "Meus Pedidos": lista via `aoMudarStatus('conectado')`, refetch em evento `resource === 'pedidos'` + `toast.info`, filtro por status (`Select`), `<output>Reconectando...</output>`, três estados (carregando/erro/vazio com mensagem orientadora), cada linha com solicitante, obra, data, `StatusPedidoBadge`, qtd de itens e botão "Ver itens" → `Dialog` que chama `buscarPedido(id)` e lista os itens em snapshot -- superfície principal da story.
- `frontend/src/pages/MeusPedidosPage.test.tsx` (novo) -- mock de `conectarRealtime` e de `lib/pedidos`: carrega no `'conectado'`, renderiza badges (ícone + texto), filtro por status refaz a busca, evento SSE refaz a busca, estado vazio, estado de erro (`role="alert"`), "Ver itens" abre o diálogo com os itens.
- `frontend/src/App.tsx` -- registrar a rota-filha `pedidos` → `<MeusPedidosPage />` e o import -- troca o `PlaceholderPage` pela tela real.

**Acceptance Criteria:**
- Given um Usuário com Pedidos enviados e Pedidos de outros usuários no sistema, when ele abre "Meus Pedidos" (`/pedidos`), then vê só os próprios Pedidos, do mais recente ao mais antigo, cada um com um badge de status em ícone + texto (nunca só cor).
- Given a tela "Meus Pedidos" aberta, when o Usuário escolhe um status no filtro, then a lista passa a mostrar só os próprios Pedidos naquele status (e "Todos" volta a mostrar todos).
- Given a tela "Meus Pedidos" aberta e conectada ao canal SSE `pedidos`, when chega um evento nesse canal (ex.: envio em outra aba, ou uma decisão do Almoxarife numa story futura), then a lista rebusca sozinha e um toast discreto avisa — a tela nunca se recarrega inteira.
- Given um Usuário sem papel `almoxarife`+, when ele tenta acessar por id um Pedido que não é dele (`GET /api/pedidos/{id}`), then a resposta é `404 NOT_FOUND` idêntica à de um Pedido inexistente, sem revelar que o Pedido existe e sem responder `403`.
- Given um Usuário com papel `almoxarife`+, when ele acessa por id um Pedido de outro usuário, then recebe o Pedido normalmente (cabeçalho + itens em snapshot), pelo padrão de escopo do sistema (AD-8).
- Given o item de navegação "Pedidos" (visível a qualquer papel `usuario`+), when o Usuário clica nele, then chega à tela "Meus Pedidos" real, não mais ao `PlaceholderPage`.

## Spec Change Log

## Review Triage Log

### 2026-09-03 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 5 (high 0, medium 2, low 3)
- defer: 0
- reject: 10
- addressed_findings:
  - `[medium]` `[patch]` "Ver itens" tinha corrida sem guarda: fechar o diálogo de um Pedido A antes da resposta de `buscarPedido` chegar e abrir o de um Pedido B podia deixar a resposta tardia de A sobrescrever os itens já exibidos de B — corrigido com `detalheIdRef` (mesmo molde de `seqRef`/`geracaoRef` já usados no arquivo/`lib/carrinho.tsx`), teste de regressão adicionado em `MeusPedidosPage.test.tsx`.
  - `[medium]` `[patch]` `GET /api/pedidos` e `GET /api/pedidos/{id}` não tinham nenhum teste despachando pela instância REAL de `newMux` (`main.go`) — só pelo mux local hand-rolled de `handlers/pedidos_test.go` — um erro de registro (handler trocado, `RequireRole` indevido, path errado) passaria pela suíte inteira; adicionado `TestNewMux_PedidosConsultaRotasSoRequireAuth` em `main_test.go`, molde de `TestNewMux_CarrinhoRotasSoRequireAuth`.
  - `[low]` `[patch]` `carregar`/`abrirItens` (`MeusPedidosPage.tsx`) sempre mostravam uma mensagem genérica no erro, descartando a mensagem específica que `lib/pedidos.ts` já lança do servidor — agora usam `err.message` (com fallback para a mensagem genérica quando a falha não é um `Error`), testes cobrindo os dois caminhos.
  - `[low]` `[patch]` a ordenação alfabética dos itens do snapshot (`ORDER BY produto_nome` em `BuscarPedidoProprio`) nunca foi exercitada — todo teste existente semeava só 1 item por Pedido; adicionado `TestBuscarPedidoProprio_ItensOrdenadosPorNome` com 2 itens inseridos em ordem reversa.
  - `[low]` `[patch]` a mensagem de vazio específica do filtro (`MENSAGEM_VAZIO_FILTRO`) e a direção "voltar para Todos" do filtro de status nunca eram exercitadas — dois testes adicionados a `MeusPedidosPage.test.tsx`.

### 2026-09-03 — Review pass (2)
- intent_gap: 0
- bad_spec: 0
- patch: 8 (high 0, medium 2, low 6)
- defer: 3
- reject: 8
- addressed_findings:
  - `[low]` `[patch]` o `toast.info` do refetch por SSE dizia "Meus Pedidos atualizada." — "Pedidos" é masculino plural, concordância errada; corrigido para "Meus Pedidos atualizados.", asserção do teste ajustada.
  - `[medium]` `[patch]` a falha de `buscarPedido` no diálogo "Ver itens" (`erroItens`/`role="alert"`) nunca era exercitada por nenhum teste — só a falha de `listarPedidos` (lista) tinha cobertura; dois testes adicionados a `MeusPedidosPage.test.tsx` (mensagem do servidor e fallback genérico quando a falha não é `Error`).
  - `[medium]` `[patch]` `GET /api/pedidos` (a listagem) não tinha nenhum teste provando que permanece escopada à sessão mesmo para um papel `almoxarife`+ — só o acesso por id (`BuscarPedidoProprio`/`AD-8`) tinha esse caso; `ListarPedidosHandler`/`ListarPedidosProprios` nem recebem o papel como parâmetro, então uma futura extensão por engano da mesma escalada do id para a listagem passaria despercebida. `TestListarPedidosHandler_AlmoxarifeSoVeOsProprios` adicionado em `handlers/pedidos_test.go`.
  - `[low]` `[patch]` `TestBuscarPedidoHandler_404IdMalformado` cobria só a variante de id malformado (não-UUID) na fronteira HTTP; a variante de UUID bem-formado mas de nenhum Pedido só tinha cobertura no service (`TestBuscarPedidoProprio_IdMalformadoOuInexistente`). `TestBuscarPedidoHandler_404IdInexistenteBemFormado` adicionado.
  - `[low]` `[patch]` nenhum teste provava que `/pedidos` monta `MeusPedidosPage` (e não a `PlaceholderPage`) através do roteador real de `<App />` — mesmo padrão já usado para `/configuracoes` e `/` faltava para esta rota nova. Teste de wiring adicionado em `App.test.tsx` (com `@/lib/realtime/client` mockado por inteiro, mesmo motivo do `CarrinhoProvider` já mockado no arquivo).
  - `[low]` `[patch]` o diálogo "Ver itens" e o botão de cada linha não davam nenhum contexto de qual Pedido — todo botão tinha o mesmo nome acessível "Ver itens" e o título do diálogo era sempre "Itens do pedido" genérico, sem solicitante/obra. `aria-label` no botão e `DialogTitle` agora incluem solicitante + obra; testes existentes ajustados para localizar pelo novo nome acessível.
  - `[low]` `[patch]` a data de cada Pedido usava `toLocaleDateString` (sem hora), divergindo do molde citado no próprio arquivo (`MovimentacoesSection`, que usa `toLocaleString`) e escondendo a hora que o próprio desempate da query (`ORDER BY criado_em DESC, id DESC`) existe para distinguir. Trocado para `toLocaleString`.
  - `[low]` `[patch]` `StatusPedidoBadge.test.tsx` verificava só texto + presença de um `<svg>`, nunca a classe de tint por status — um `aprovado` colorido como `destructive` (com o texto "Aprovado" certo) passaria despercebido. Asserção da classe de cor esperada por status adicionada.
- deferred:
  - GET /api/pedidos e a contagem de itens (subquery em `ListarPedidosProprios`) não têm índice/escopo dedicado — `pedidos.usuario_id` sem índice e a subquery de contagem agrega `pedido_itens` da empresa inteira antes do join, escalando com o volume total, não o do usuário; fora do escopo desta story (`Não cria migration`).
  - `observacao` do Pedido é buscada e transportada ponta a ponta mas nunca exibida em "Meus Pedidos" (nem na lista, nem no diálogo) — Code Map/Tasks não a enumeram entre os campos exibidos, então não é um requisito claro desta story.
  - a invariância do snapshot (AD-17: rótulo do item não muda se o Produto for editado/mesclado depois) só tem teste de correção no momento da leitura — nenhum teste edita o Produto e rebusca o Pedido para confirmar que o rótulo permanece congelado; a query já não faz join ao vivo, então é lacuna de cobertura, não bug conhecido.

### 2026-09-03 — Review pass (3)
- intent_gap: 0
- bad_spec: 0
- patch: 4 (high 0, medium 1, low 3)
- defer: 0
- reject: 12
- addressed_findings:
  - `[medium]` `[patch]` reabrir o MESMO Pedido em "Ver itens" antes da resposta anterior chegar não era coberto pela guarda `detalheIdRef` (só comparava id, e o id é igual nas duas chamadas) — uma rejeição tardia da 1ª abertura podia sobrescrever com erro o resultado já exibido pela reabertura mais recente. Trocado por `detalheSeqRef` (mesmo molde de `seqRef` já usado em `carregar` no mesmo arquivo), que distingue cada chamada independente do id. Teste de regressão adicionado em `MeusPedidosPage.test.tsx`.
  - `[low]` `[patch]` o `aria-label` do botão "Ver itens" ("Ver itens do pedido de X — Y") e o `DialogTitle` ("Itens do pedido — X · Y") usavam frases e separadores diferentes para o mesmo Pedido. Unificado o formato ("—" em ambos).
  - `[low]` `[patch]` o `aria-label` do botão e o `DialogTitle` não distinguiam dois Pedidos do mesmo solicitante na mesma obra (par solicitante+obra repetido é um caso comum, não hipotético) — usuários de leitor de tela não conseguiam saber a qual Pedido cada botão/diálogo se referia. Acrescentada a data/hora (`toLocaleString('pt-BR')`) como desambiguador em ambos; testes que localizavam o botão pelo nome acessível exato trocados para casar só o prefixo (evita acoplamento ao fuso horário do executor dos testes).
  - `[low]` `[patch]` `TestListarPedidosHandler_200Vazio` comparava o corpo da resposta como string literal (`{"pedidos":[]}`) — uma mudança futura e inofensiva em `escreverJSON` (espaço, nova chave estável) quebraria o teste por um motivo alheio ao comportamento que ele protege. Trocado para decodificar e comparar estruturalmente.
  - descartados sem ação (ruído ou já coberto por decisão anterior/spec): contagem de itens via subquery e falta de índice em `pedidos.usuario_id`/`pedido_itens.pedido_id` (já registrado como deferido nesta mesma spec, `Não cria migration` no Boundaries), `observacao` nunca exibida na UI (já deferido), invariância do AD-17 sem teste temporal (já deferido), `authHeaders()` duplicado em `lib/pedidos.ts` (o próprio Code Map manda copiar de `carrinho.tsx`), `estiloDoStatus` aceitar `status: string` com `as StatusPedido` (por design — o Boundaries pede `switch` exaustivo com `default` genérico para tolerar valor inesperado do servidor), `## Spec Change Log` vazio (correto — só é preenchido em loopback de `bad_spec`, que não ocorreu em nenhuma rodada), SSE sem debounce em rajada de eventos (replica fielmente o molde `MovimentacoesSection`, não é comportamento introduzido por esta story), `<output>Carregando pedidos...</output>` sem `aria-live` explícito (role `status` implícito de `<output>` já implica `aria-live="polite"`; inconsistência cosmética sem efeito funcional), branch de "pedido sem itens" no diálogo sem teste dedicado (defensivo, provavelmente inalcançável dado que o carrinho exige item para envio), listagem sem parâmetro de papel (design intencional já travado por `TestListarPedidosHandler_AlmoxarifeSoVeOsProprios`), demais achados redundantes.

## Design Notes

- **Escopo próprios vs. todos:** o epic-7-context.md fixa que o filtro de escopo da listagem é responsabilidade do `service`, consumindo o papel já resolvido no contexto. Esta story implementa só o lado "próprios": `ListarPedidosProprios` sempre filtra por `usuario_id` da sessão, para QUALQUER papel. A Story 7.4 (Fila do Almoxarife) acrescenta a rota/opção que devolve TODOS os Pedidos a `almoxarife`+ — não é retrabalho deste `service`, é uma função de leitura adicional. O único ponto onde o padrão de escopo AD-8 já entra AQUI é o acesso por id (`BuscarPedidoProprio`), porque a Story 7.5 (aprovação) e a 7.6 (recibo) precisam que um `almoxarife` carregue um Pedido que não é dele — negar isso agora obrigaria a reescrever a checagem depois.
- **`404` e não `403` no acesso por id:** o comentário do epic-7-context.md sobre "nunca erro 403 para quem tem menos papel" é sobre a LISTAGEM (que é escopo, não erro). Para acesso direto por id a um Pedido alheio, `404 NOT_FOUND` (mesma resposta de id inexistente/malformado) é o certo: não vaza a existência do Pedido de outro usuário. Mesmo colapso "não existe / não acessível / malformado → 404" já usado por `ObterProdutoDetalheHandler` (`handlers/produtos.go`).
- **Tempo real por consumo, sem publicação:** a página assina o canal `pedidos` só para reagir — a carga inicial e todo refetch passam por `aoMudarStatus('conectado')` + eventos `resource === 'pedidos'`, idêntico a `MovimentacoesSection` (Story 5.3). Esta story não chama `registro.Publish` em lugar nenhum; o `change:"created"` do envio já é publicado pelo `SubmeterPedidoHandler` (7.2) e as mudanças de status virão do `Aprovar/RejeitarPedidoHandler` (7.5).
- **Snapshot, nunca join ao vivo:** o detalhe (`GET /api/pedidos/{id}`) lê `pedido_itens` direto (`produto_nome`/`categoria_nome`/`estoque_nome`/`quantidade`), sem tocar em `produtos`/`estoques` — AD-17: o que o Usuário pediu não muda de rótulo se o Produto for editado/mesclado depois.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test ./...` -- expected: build limpo, todos os testes (incluindo os novos de `ListarPedidosProprios`/`BuscarPedidoProprio` em services e handlers) passando.
- `cd frontend && npx tsc --noEmit && npx vitest run` -- expected: sem erro de tipo, suíte Vitest 100% passando (incluindo `lib/pedidos.test.ts`, `StatusPedidoBadge.test.tsx` e `MeusPedidosPage.test.tsx`).

## Auto Run Result

**Resumo da mudança implementada:** duas rotas de leitura escopadas à sessão (`GET /api/pedidos`, `GET /api/pedidos/{id}`) e a página "Meus Pedidos" (`/pedidos`), substituindo o `PlaceholderPage` — lista dos Pedidos do próprio usuário com badge de status (ícone + texto), filtro por status, tempo real via SSE `pedidos` (só consumo) e diálogo "Ver itens" com o snapshot de cada Pedido.

**Arquivos alterados nesta rodada (pass 3, sobre o `done` da pass 2):**
- `frontend/src/pages/MeusPedidosPage.tsx` — guarda anti-corrida de "Ver itens" trocada de `detalheIdRef` (só id) para `detalheSeqRef` (sequência, molde de `seqRef`), cobrindo a reabertura do MESMO Pedido; `aria-label` do botão e `DialogTitle` unificados e com data/hora acrescentada para desambiguar Pedidos do mesmo solicitante+obra.
- `frontend/src/pages/MeusPedidosPage.test.tsx` — teste novo de regressão para a corrida de reabertura do mesmo Pedido; asserções do nome acessível do botão trocadas de string exata para prefixo (`RegExp`), já que o nome agora inclui data/hora dependente do fuso do executor.
- `backend/handlers/pedidos_test.go` — `TestListarPedidosHandler_200Vazio` decodifica e compara estruturalmente em vez de comparar o corpo como string literal.

(Arquivos de código originais das passes 1-2: `backend/services/pedidos.go`, `backend/handlers/pedidos.go`, `backend/main.go`, `backend/services/pedidos_test.go`, `backend/handlers/pedidos_test.go`, `backend/main_test.go`, `frontend/src/lib/pedidos.ts`, `frontend/src/lib/pedidos.test.ts`, `frontend/src/components/pedidos/StatusPedidoBadge.tsx`(+`.test.tsx`), `frontend/src/pages/MeusPedidosPage.tsx`(+`.test.tsx`), `frontend/src/App.tsx`(+`.test.tsx`) — ver Review Triage Log acima para o detalhe de cada rodada.)

**Review findings desta rodada:** patch 4 (high 0, medium 1, low 3) — todos corrigidos nesta rodada; defer 0; reject 12 (ver Review Triage Log, pass 3, para o detalhe dos descartes — inclui reafirmações dos 3 itens já deferidos nas passes 1-2 e decisões de escopo/design já fixadas pela spec).

**Recomendação de follow-up review:** `true` — nesta rodada, 1 médio + 3 baixos = `3×1 + 1×3 = 6 ≥ 5`.

**Verificação executada (pass 3):** cluster PostgreSQL 16 já ativo no sandbox (127.0.0.1:5432, role/db `stockflow`).
- `cd backend && go build ./... && go vet ./...` — limpo.
- `DATABASE_URL=postgres://stockflow:stockflow@127.0.0.1:5432/stockflow?sslmode=disable go test -p 1 -count=1 ./...` — todos os 8 pacotes OK (`backend` 11.4s, `cmd/migrate-legado` 2.7s, `cmd/seed-admin` 1.3s, `handlers` 102.8s, `iam` 0.4s, `middleware` 1.8s, `realtime` 0.0s, `services` 86.3s).
- `cd frontend && npx tsc --noEmit` — sem erro de tipo.
- `npx vitest run` — 475 testes, 41 arquivos, 100% passando (1 teste novo desta rodada).

**Riscos residuais:** os três itens deferidos nas rodadas 1-2 (performance de query em volume alto, `observacao` não exibida, cobertura de invariância do AD-17) seguem registrados para atenção futura — nenhum bloqueia esta story; nenhum risco novo identificado nesta rodada.

