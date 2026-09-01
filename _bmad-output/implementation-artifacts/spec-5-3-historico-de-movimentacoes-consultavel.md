---
title: 'Story 5.3 — Histórico de Movimentações consultável'
type: 'feature'
created: '2026-09-01'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '18ccf6b34205424e7839001423b5216008086433'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-5-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      ListarMovimentacoes usa JOIN interno em produtos e usuarios — se um
      Produto ou Usuario for algum dia removido em hard-delete (ex. exclusão
      LGPD do Epic 8), a Movimentação correspondente some de uma trilha
      descrita como append-only.
    evidence: |-
      backend/services/movimentacoes.go: `JOIN produtos p` / `JOIN usuarios u`.
      Seguro hoje — a migration 000021 documenta que produtos/estoques/usuarios
      nunca são excluídos, e a anonimização LGPD preserva a linha. Vira risco
      só quando o Epic 8 introduzir exclusão real. LEFT JOIN + COALESCE para um
      rótulo placeholder resolveria.
    location: >-
      backend/services/movimentacoes.go:438
    severity: low
  - summary: >-
      GET /api/movimentacoes não tem paginação nem filtro (produto, tipo,
      autor, período) e o teto é 500 — depois que a Story 5.4 importar o
      histórico legado em massa, a maior parte da trilha ficará inacessível
      pela tela e pela API.
    evidence: |-
      backend/services/movimentacoes.go: `LIMIT 500`, sem OFFSET nem WHERE;
      handler sem query params por decisão da spec. Espelha o teto de
      logs_acesso (que shipou assim), mas logs_acesso tem filtro de período e
      Movimentações não.
    location: >-
      backend/services/movimentacoes.go:437 / backend/handlers/movimentacoes.go:30
    severity: medium
  - summary: >-
      Um Estoque excluído (Story 2.2 permite excluir Estoque sem quantidade
      residual) que aparece como origem/destino de uma Movimentação antiga é
      renderizado como "—" no Histórico, indistinguível de um lado
      genuinamente nulo (ex. o destino de uma Baixa).
    evidence: |-
      LEFT JOIN estoques devolve nome NULL; o frontend faz
      `mov.estoqueOrigemNome ?? '—'`. COALESCE para "(estoque removido)" quando
      o id existe mas o nome é nulo distinguiria os dois casos.
    location: >-
      frontend/src/components/estoques/MovimentacoesSection.tsx:983
    severity: low
---

<intent-contract>

## Intent

**Problem:** As Movimentações (Baixa da Story 5.1, Transferência da Story 5.2) são gravadas em `movimentacoes` mas não há nenhuma forma de consultá-las — o Almoxarife não tem rastreabilidade das saídas e transferências já registradas.

**Approach:** Novo `GET /api/movimentacoes` atrás de `RequireAuth → RequireRole(almoxarife)` devolvendo a lista de Movimentações (com nome de Produto, Estoques e autor resolvidos por JOIN), mais recente primeiro, limitada a 500. No frontend, a página `/estoques` ganha abas "Locais" / "Movimentações"; a aba nova mostra uma tabela somente-leitura (Produto · Tipo · Origem · Destino · Quantidade · Autor · Data) que assina o canal SSE `movimentacoes` e, a cada evento, mostra um toast discreto e refaz o GET — nunca recarrega sozinha.

## Boundaries & Constraints

**Always:**
- `GET /api/movimentacoes` fica atrás de `RequireAuth → RequireRole(services.PapelAlmoxarife)` registrado em `newMux`; o 401/403 é decidido pelos middlewares, o handler só roda depois dos dois gates.
- A regra de negócio (query, JOINs, ordenação, teto) vive na camada de serviço (`services.ListarMovimentacoes`); o handler só decodifica/serializa e traduz erro para o envelope `{"error":{"code","message"}}` (AD-14). SQL explícito, sem ORM.
- Ordenação: mais recente primeiro — `ORDER BY criado_em DESC, id DESC` (mesmo desempate determinístico de `ListarLogsAcesso`).
- Teto de `maxMovimentacoesPorConsulta = 500` linhas por consulta (decisão desta spec, sem parâmetro runtime — espelha `maxLogsAcessoPorConsulta`).
- Datas em UTC no banco, ISO 8601 (`time.Time` → RFC3339) na resposta JSON.
- A tela de Movimentações carrega o GET completo SÓ a partir de `aoMudarStatus('conectado')` de `conectarRealtime` (dispara também na 1ª conexão) — nunca de um `useEffect` de mount separado (AD-3, mesmo padrão de `ProdutoDetalhePage.tsx`). Um evento SSE com `resource === 'movimentacoes'` dispara `toast.info('Movimentações atualizada.')` + o mesmo refetch; status `'reconectando'` mostra um `<output aria-live="polite">Reconectando...</output>` persistente (UX-DR17/UX-DR18). Dado antigo permanece visível até o refetch.
- Gate de papel espelhado no cliente: a aba "Movimentações" e seu conteúdo só renderizam para `rankPapel(papel) >= rankPapel('almoxarife')` (a página `/estoques` já tem esse gate; manter). O servidor continua a autoridade real.

**Block If:**
- _(nenhuma decisão que exija humano)_

**Never:**
- Sem rota de escrita/edição/exclusão de Movimentação — a trilha é append-only.
- Sem alterar `services.RegistrarBaixa`/`RegistrarTransferencia` nem os handlers de POST: eles já publicam no canal `movimentacoes` (AC #2 lado publicação já satisfeita) — esta story só adiciona o lado assinante.
- Sem migration nova (índices `idx_movimentacoes_produto_id` e `idx_movimentacoes_criado_em` já existem).
- Sem paginação, filtros por período/Produto, ou parâmetros de query nesta story (o teto de 500 + aviso de limite bastam).
- A tela nunca se auto-recarrega ao receber um evento SSE — só toast + refetch controlado.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Lista com Movimentações | `GET /api/movimentacoes`, sessão `almoxarife`/`gestor`/`adm`, há Baixas e Transferências | `200 {"movimentacoes":[...]}` mais recente primeiro; cada item traz `produtoNome`, `tipo`, `estoqueOrigemId/Nome`, `estoqueDestinoId/Nome`, `quantidade`, `usuarioId/Nome`, `criadoEm` | Sem erro |
| Baixa (sem destino) | Item `tipo='baixa'` na lista | `estoqueDestinoId` e `estoqueDestinoNome` são `null` (JSON `null`); origem preenchida | Sem erro |
| Nenhuma Movimentação | Tabela `movimentacoes` vazia | `200 {"movimentacoes":[]}`; UI mostra "Nenhuma movimentação registrada." | Sem erro |
| Acima do teto | > 500 Movimentações | Devolve as 500 mais recentes; UI mostra aviso de limite (molde de `LogAcessoSection`) | Sem erro |
| Papel insuficiente | Sessão `usuario` chama `GET /api/movimentacoes` | `403 FORBIDDEN`; `ListarMovimentacoesHandler` nunca executa | `RequireRole` responde |
| Sem token | `GET /api/movimentacoes` sem `Authorization` | `401` | `RequireAuth` responde |
| Falha de banco | `db.Query` devolve erro | `500 INTERNAL_ERROR` no envelope AD-14; `slog.Error` | Erro logado, não vaza detalhe |
| Evento SSE enquanto a aba está aberta | `conectarRealtime` entrega `{resource:'movimentacoes',...}` | `toast.info('Movimentações atualizada.')` + refetch completo; tela não recarrega; dado antigo visível até o refetch chegar | Falha do refetch → `<p role="alert">` inline, dado anterior mantido |
| Reconexão SSE lenta | `aoMudarStatus('reconectando')` | `<output aria-live="polite">Reconectando...</output>` persistente até `'conectado'` | — |

</intent-contract>

## Code Map

- `backend/services/movimentacoes.go` — ESTENDE (Stories 5.1/5.2 criaram): novo tipo `MovimentacaoHistorico` (projeção com nomes resolvidos), const `maxMovimentacoesPorConsulta = 500`, função `ListarMovimentacoes(db *sql.DB) ([]MovimentacaoHistorico, error)`. Molde: `services.ListarLogsAcesso` (`backend/services/logs_acesso.go:83-138`) — `LEFT JOIN` + `sql.NullString` para colunas anuláveis, `ORDER BY ... DESC, id DESC`, `LIMIT <const>`, lista vazia não é erro, `make([]T, 0)`.
- `backend/services/movimentacoes_test.go` — ESTENDE: casos de `ListarMovimentacoes` (ordem cronológica desc, baixa com destino nil, transferência com os dois lados, lista vazia, nomes resolvidos). Reusa `seedProdutoComSaldo`; para criar Movimentações use `RegistrarBaixa`/`RegistrarTransferencia` (evita SQL duplicado no teste).
- `backend/handlers/movimentacoes.go` — ESTENDE: `ListarMovimentacoesHandler(db *sql.DB) http.HandlerFunc`. Molde EXATO: `handlers.ListarLogsAcessoHandler` (`backend/handlers/logs_acesso.go:96-125`) — guard de `middleware.UsuarioDaSessao` → 500, resposta `200 {"movimentacoes": ...}`, `slog.Error` + `escreverErro(... "INTERNAL_ERROR" ...)` na falha. Sem query params.
- `backend/handlers/movimentacoes_test.go` — ESTENDE: `getMovimentacoes(db, authHeader)` helper (molde de `postBaixa`, linha 23), casos 200 / lista vazia / 403 `usuario` / 401 sem token / 500 falha de banco (rename da tabela `movimentacoes`, molde de `TestLoginHandler_FalhaDoInsertDeLogNaoQuebraLogin`) / corpo dos campos. Reusa `seedProdutoComSaldoHandler`.
- `backend/main.go` — nova rota `mux.HandleFunc("GET /api/movimentacoes", middleware.RequireAuth(db, jwtSecret)(middleware.RequireRole(services.PapelAlmoxarife)(handlers.ListarMovimentacoesHandler(db))))` junto ao bloco de Baixa/Transferência (~linhas 505-527) + 1-2 linhas no doc do pacote (bloco ~linhas 84-95).
- `backend/main_test.go` — novo `TestNewMux_MovimentacoesRotaCarregaRequireRole` (molde de `TestNewMux_EstoquesRotaCarregaRequireRole`, linha ~625: `usuario` → 403, `almoxarife`/`gestor`/`adm` passam do gate — não 403). Adicionar 1 linha-caso "movimentacoes sem token chega no RequireAuth antes de RequireRole" na tabela de `TestNewMux_RegistraRotasDeAutenticacao` (molde da linha ~423, `logs-acesso`). TRUNCATE já cobre `movimentacoes`.
- `frontend/src/pages/EstoquesPage.tsx` — envolve o conteúdo em `Tabs` ("Locais" / "Movimentações"), molde de `CatalogoPage.tsx:93-104` (`Tabs`/`TabsList`/`TabsTrigger`/`TabsContent`, `@/components/ui/tabs`). `LocaisEstoqueSection` vai para `TabsContent value="locais"`; `MovimentacoesSection` (nova) para `value="movimentacoes"`. Gate de papel da página inalterado.
- `frontend/src/pages/EstoquesPage.test.tsx` — ESTENDE: as duas abas visíveis para `almoxarife`+; clicar "Movimentações" mostra a seção; mock de `@/lib/realtime/client` (molde de `ProdutoDetalhePage.test.tsx:34-52`).
- `frontend/src/components/estoques/MovimentacoesSection.tsx` — NOVO. Tabela somente-leitura. Molde de estrutura/tabela: `frontend/src/components/logs/LogAcessoSection.tsx` (`Card`/`CardHeader`/`CardContent`, `<div className="overflow-x-auto"><table>`, `seqRef` anti-corrida, aviso de teto). Molde de realtime: `frontend/src/pages/ProdutoDetalhePage.tsx:420-438` (`conectarRealtime`, carregar só via `aoMudarStatus('conectado')`, toast em `resource==='movimentacoes'`, `<output>` "Reconectando..."). Reusa `formatarQuantidade` (`@/components/catalogo/formatacao`), `authHeaders` (padrão local), `new Date(criadoEm).toLocaleString('pt-BR')`.
- `frontend/src/components/estoques/MovimentacoesSection.test.tsx` — NOVO. Mock de `@/lib/realtime/client`, `@/lib/session`, `sonner`; `fetch` stub de `GET /api/movimentacoes`. Casos: renderiza linhas após `aoMudarStatus('conectado')`; "baixa" mostra destino "—"; evento `movimentacoes` → toast + refetch; evento de outro `resource` ignorado; `'reconectando'` → indicador; lista vazia → mensagem; 500 linhas → aviso de teto.

## Tasks & Acceptance

**Execution:**
- `backend/services/movimentacoes.go` (+ `_test.go`) — `MovimentacaoHistorico`, `maxMovimentacoesPorConsulta`, `ListarMovimentacoes` com JOIN `produtos`/`usuarios` e LEFT JOIN `estoques` (origem/destino), `ORDER BY criado_em DESC, id DESC LIMIT 500`. Unit-testar os cenários da I/O Matrix ligados à consulta.
- `backend/handlers/movimentacoes.go` (+ `_test.go`) — `ListarMovimentacoesHandler`, fronteira HTTP pura (molde de `ListarLogsAcessoHandler`). Testar 200 / 403 `usuario` / 401 / campos do corpo.
- `backend/main.go` (+ `main_test.go`) — registrar `GET /api/movimentacoes` atrás de `RequireAuth → RequireRole(almoxarife)`; teste de gate de papel + caso sem-token na tabela de rotas.
- `frontend/src/pages/EstoquesPage.tsx` (+ `.test.tsx`) — abas "Locais" / "Movimentações".
- `frontend/src/components/estoques/MovimentacoesSection.tsx` (+ `.test.tsx`) — tabela somente-leitura Produto · Tipo · Origem · Destino · Quantidade · Autor · Data, assinatura SSE do canal `movimentacoes`, toast + refetch, indicador de reconexão, estados de vazio/erro/teto.

**Acceptance Criteria:**
- Given Movimentações registradas (Baixa e Transferência), when um `almoxarife` (ou acima) abre a aba "Movimentações" em `/estoques`, then vê uma tabela com Produto, tipo, origem, destino, quantidade, autor e data de cada Movimentação, mais recente primeiro; numa linha de Baixa o destino aparece como "—".
- Given uma sessão de papel `usuario`, when ela chama `GET /api/movimentacoes` diretamente pela API, then a resposta é `403 FORBIDDEN` e `ListarMovimentacoesHandler` nunca executa.
- Given a aba "Movimentações" aberta e assinando o canal SSE `movimentacoes`, when uma nova Movimentação é criada em outra sessão (evento `{resource:'movimentacoes',...}`), then a tela mostra `toast.info('Movimentações atualizada.')` e refaz o `GET /api/movimentacoes` — sem recarregar a página, com o dado anterior visível até a resposta chegar.
- Given a conexão SSE caiu e a reconexão demora mais que o limiar, when o status vira `'reconectando'`, then um indicador persistente "Reconectando..." (`aria-live="polite"`) aparece até a reconexão suceder.
- Given mais de 500 Movimentações no banco, when a aba é carregada, then a tabela mostra as 500 mais recentes e um aviso de que a consulta não vai além disso (molde de `LogAcessoSection`).

## Design Notes

- **Reuso do molde `logs_acesso` (consulta de trilha de auditoria):** `ListarLogsAcesso`/`ListarLogsAcessoHandler`/`LogAcessoSection` já resolvem exatamente esta forma — GET só-leitura, gate de papel, `LEFT JOIN` para nome, `ORDER BY ... DESC` com desempate por `id`, teto fixo com aviso de limite na UI, sem ação em nenhuma linha. Story 5.3 copia esse molde trocando `adm`→`almoxarife` e a projeção. A única diferença estrutural é a assinatura SSE (que `logs_acesso` não tem, mas `ProdutoDetalhePage` tem).
- **"Ordem cronológica" = mais recente primeiro:** a AC diz "em ordem cronológica"; a decisão (consistente com `logs_acesso` e com o índice `idx_movimentacoes_criado_em (criado_em DESC)` já criado na migration 000021) é DESC — a leitura natural de uma trilha de auditoria é da última ação para trás. `id DESC` desempata Movimentações no mesmo instante.
- **`LEFT JOIN` em `estoques` para os dois lados:** `estoque_destino_id` é `NULL` para `tipo='baixa'`; `estoque_origem_id` hoje é sempre preenchido mas a coluna é nullable — `LEFT JOIN` + `sql.NullString` cobre os dois sem assumir. `produto_id`/`usuario_id` são `NOT NULL` → `JOIN` simples.
- **Carregar só via `aoMudarStatus('conectado')`:** unifica 1ª carga e refetch pós-reconexão num só caminho (AD-3: "sempre GET completo ao reconectar"), evitando dois caminhos divergentes — idêntico ao que `ProdutoDetalhePage` fez para o canal `produtos`.
- **Sem mudança no lado publicação:** `RegistrarBaixaHandler`/`RegistrarTransferenciaHandler` já fazem `registro.Publish("movimentacoes", ...)`. A metade "um evento é publicado nesse canal" da AC #2 já está pronta desde a Story 5.1; esta story entrega a metade "para qualquer tela assinante atualizar".

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — expected: sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` — expected: Postgres real; todos os pacotes `ok`, incluindo os novos casos de `services/movimentacoes_test.go`, `handlers/movimentacoes_test.go` e `main_test.go`.
- `cd frontend && npm run lint && npm run build && npm run test` — expected: `oxlint` limpo, `tsc`+`vite` build ok, testes de `EstoquesPage.test.tsx` e `MovimentacoesSection.test.tsx` passando.

**Manual checks (if no CLI):**
- Logado como `almoxarife`: registrar uma Baixa e uma Transferência num Produto, abrir `/estoques` → aba "Movimentações" → as duas linhas aparecem, mais recente no topo, Baixa com destino "—", Transferência com origem e destino; registrar outra Movimentação numa segunda aba → toast "Movimentações atualizada." e a nova linha aparece após o refetch, sem reload.
- Sessão `usuario` chamando `GET /api/movimentacoes` direto → `403`.

## Spec Change Log

_Vazio — nenhum loopback de `bad_spec`._

## Review Triage Log

### 2026-09-01 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 4: (high 0, medium 0, low 4)
- defer: 3: (high 0, medium 1, low 2)
- reject: 22
- addressed_findings:
  - `[low]` `[patch]` `MovimentacoesSection` não tinha estado de carregamento — entre o mount e o 1º `aoMudarStatus('conectado')` o card ficava só com o cabeçalho (e ficaria em branco para sempre se a SSE nunca conectasse). Adicionado `<output>Carregando movimentações...</output>` enquanto `!carregou && !erroCarregar` (molde de `ProdutoDetalhePage`), com teste.
  - `[low]` `[patch]` O desempate `ORDER BY m.criado_em DESC, m.id DESC` não tinha cobertura — remover `, m.id DESC` passava em toda a suíte. Adicionado `TestListarMovimentacoes_DesempatePorIDQuandoCriadoEmIgual` (duas linhas via SQL direto com `criado_em` idêntico, afirma ordem por `id` decrescente).
  - `[low]` `[patch]` O branch `catch {}` de `carregar` (rejeição de rede → `MENSAGEM_ERRO_CARREGAR`) não tinha teste. Adicionado caso em que o fetch rejeita e afirma o `role="alert"`.
  - `[low]` `[patch]` O aviso de teto (`noLimite`) renderizava mesmo ao lado do banner de erro e afirmava linhas ocultas ("Registros mais antigos não aparecem aqui.") contra a própria intenção do comentário. Passou a ser `!erroCarregar && carregou && noLimite` e reescrito no molde cuidadoso de `LogAcessoSection` ("...a consulta não vai além disso.").

## Auto Run Result

**Mudança implementada:** Story 5.3 — Histórico de Movimentações consultável. Novo `GET /api/movimentacoes` (só-leitura, atrás de `RequireAuth → RequireRole(almoxarife)`) devolvendo a trilha de Baixas/Transferências com nome de Produto, Estoques e autor resolvidos por JOIN, mais recente primeiro, teto de 500. No frontend, a página `/estoques` ganhou abas "Locais" / "Movimentações"; a aba nova é uma tabela só-leitura (Produto · Tipo · Origem · Destino · Quantidade · Autor · Data) que assina o canal SSE `movimentacoes` e, a cada evento, mostra `toast.info('Movimentações atualizada.')` + refetch, sem recarregar. O lado publicação da AC #2 já existia desde a Story 5.1.

**Arquivos alterados:**
- `backend/services/movimentacoes.go` — novo `MovimentacaoHistorico`, `maxMovimentacoesPorConsulta = 500`, `ListarMovimentacoes` (JOIN produtos/usuarios, LEFT JOIN estoques ×2, `ORDER BY criado_em DESC, id DESC LIMIT 500`).
- `backend/services/movimentacoes_test.go` — testes de ordem/nomes resolvidos, lista vazia, teto de 500, desempate por `id`.
- `backend/handlers/movimentacoes.go` — `ListarMovimentacoesHandler` (fronteira HTTP pura, molde de `ListarLogsAcessoHandler`).
- `backend/handlers/movimentacoes_test.go` — helper `getMovimentacoes` + casos 200/campos, lista vazia, 403 `usuario`, 401, 500 falha de banco.
- `backend/main.go` — rota `GET /api/movimentacoes` + doc do pacote.
- `backend/main_test.go` — `TestNewMux_MovimentacoesRotaCarregaRequireRole` + caso sem-token na tabela de rotas.
- `frontend/src/pages/EstoquesPage.tsx` — `Tabs` "Locais" / "Movimentações" (molde de `CatalogoPage`), gate de papel inalterado.
- `frontend/src/pages/EstoquesPage.test.tsx` — abas visíveis para `almoxarife`+, clique monta a seção, mock de `conectarRealtime`.
- `frontend/src/components/estoques/MovimentacoesSection.tsx` — NOVO. Tabela só-leitura, assinatura SSE, estados de carregamento/vazio/erro/teto.
- `frontend/src/components/estoques/MovimentacoesSection.test.tsx` — NOVO. 12 casos (carga on-connect, destino "—", evento→toast+refetch, outro resource ignorado, reconectando, vazio, erro mantém dado, rejeição de rede, teto de 500, abaixo de 500, unmount desconecta).

**Triagem da revisão:** 4 patches aplicados (todos `low`), 3 itens diferidos (1 `medium`: falta de paginação/filtro que vira relevante após a Story 5.4; 2 `low`: JOIN interno vs. exclusão futura de Produto/Usuário, e Estoque excluído exibido como "—"), 22 findings rejeitados (majoritariamente alinhados a padrões já estabelecidos no código — `authHeaders` duplicado, `export default`, ausência de `AbortController`/debounce igual a `ProdutoDetalhePage`, índice `criado_em DESC` que já existe na migration 000021, lado publicação da SSE já testado nas Stories 5.1/5.2).

**Recomendação de revisão de acompanhamento:** `false`. Patches desta passada: 4 `low`, 0 `medium`, 0 `high`. Score = 3×0 + 1×4 = 4 (< 5) e nenhum `high`.

**Verificação executada:**
- `backend`: `gofmt -l .` sem saída; `go build ./...` e `go vet ./...` limpos; `go test -p 1 -count=1 -timeout 540s ./...` (Postgres real via `DATABASE_URL`) — todos os pacotes `ok` (services 77s, handlers 90s), incluindo os testes novos.
- `frontend`: `npm run lint` (oxlint) limpo; `npm run build` (tsc + vite) ok (só o aviso pré-existente de tamanho de chunk); `npm run test` — 33 arquivos / 375 testes `ok`.
- Matrix Test Audit: todas as 9 linhas da I/O Matrix cobertas por teste que rodou e passou.

**Riscos residuais:** os 3 itens diferidos. Nenhum bloqueia esta story; o mais material (falta de paginação/filtro) só passa a doer depois que a Story 5.4 popular o histórico legado.
