---
title: 'Story 4.4 — Detalhe do produto por Estoque com atualização em tempo real'
type: 'feature'
created: '2026-08-31'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: 'f4d37736124b1f6c056ea2e90d816d5681f4c2fc'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      Nenhuma linha de tokens_acao é limpa depois de expirada/usada — vale
      para todos os tipos (verificacao_email, redefinicao_senha, mfa_login,
      e agora realtime_ticket), não é algo que esta story introduziu.
    evidence: |-
      grep por `DELETE FROM tokens_acao` no backend inteiro (fora de
      migrations) não encontra nenhum job/rotina de limpeza para nenhum dos 4
      tipos — o gap já existia para os 3 tipos anteriores antes da Story
      4.4 acrescentar `realtime_ticket`.
    location: >-
      backend/services/auth.go, backend/services/realtime.go
    severity: low
  - summary: >-
      GET /api/realtime/stream não tem limite de conexões simultâneas por
      usuário nem no total — um vetor de esgotamento de recursos em tese.
    evidence: |-
      Cada conexão aceita mantém uma goroutine + um channel bufferizado no
      Registry, sem teto. A AD-3 já enquadra a escala horizontal/limites de
      carga como uma revisão arquitetural deliberadamente adiada
      ("escalar horizontalmente exige revisitar esta decisão... Redis
      Pub/Sub volta à mesa"), então um limite de conexão não é algo que a
      spec desta story exigia construir agora.
    location: >-
      backend/handlers/realtime.go (StreamRealtimeHandler)
    severity: medium
  - summary: >-
      frontend/src/lib/realtime/client.ts trata um 401 (sessão expirada) na
      emissão do ticket exatamente como qualquer outra falha transitória —
      fica retentando/"Reconectando..." para sempre em vez de sinalizar que
      só um novo login resolveria.
    evidence: |-
      Nenhum outro componente do app (ex. CatalogoListagem, BuscaCatalogo)
      trata expiração de sessão como um caso especial durante um fetch em
      andamento — é uma lacuna consistente com o resto da base, não
      específica desta story, e corrigi-la bem exigiria tocar a
      infraestrutura de sessão compartilhada (AuthProvider), fora do Code
      Map desta spec.
    location: >-
      frontend/src/lib/realtime/client.ts
    severity: low
---

<intent-contract>

## Intent

**Problem:** O Catálogo (Stories 4.1/4.3) lista Produtos mas não tem tela de detalhe — clicar num card/resultado não leva a lugar nenhum, e não existe nenhuma infraestrutura de atualização em tempo real no projeto (nenhum pacote `realtime/`, nenhum SSE, nenhum broadcaster). Sem isso, um Usuário não confia que a quantidade vista é a atual (FR-7) — a promessa central do produto.

**Approach:** Constrói do zero a infraestrutura de tempo real da AD-3 (`backend/realtime/`: registry SSE in-process; `POST /api/realtime/ticket` + `GET /api/realtime/stream`) e o primeiro consumidor dela: `GET /api/produtos/{id}` (detalhe com quantidade por Estoque) e a página `ProdutoDetalhePage` (`/produtos/:id`), alcançável clicando um card da grade (Story 4.3) ou um resultado de busca (Story 4.1). A tela assina o canal `produtos`; evento sobre o Produto aberto dispara um refetch completo + toast "Catálogo atualizado."; reconexão lenta mostra "Reconectando...". `CriarProduto`/`AtualizarNomeProduto` (Stories 3.1/3.2) passam a publicar no canal — únicos pontos de escrita de Produto hoje.

## Boundaries & Constraints

**Always:**
- `GET /api/produtos/{id}`: só `RequireAuth` (qualquer papel, `usuario`+), mesmo padrão de `GET /api/produtos/catalogo`. `200 {"produto":{"id","nome","codigo":string|null,"categoria":{"id","codigo","nome"},"dimensoes":<Dimensoes>,"quantidadeTotal":number,"disponivel":bool,"porEstoque":[{"estoqueId","estoqueNome","quantidade"}, ...]}}` — mesmos tipos/formatação de `CatalogoItem`/`EstoqueQuantidade` (Story 4.3). `id` inexistente OU malformado (não-UUID) -> `404 NOT_FOUND` "produto não encontrado" (`ErrProdutoNaoEncontrado`, mesmo colapso de `AtualizarNomeProduto`). `porEstoque` sempre não-`nil` (pode ser `[]`), ordenado `estoqueNome ASC, estoqueId ASC` (mesmo critério de `preencherPorEstoque`).
- `POST /api/realtime/ticket`: só `RequireAuth`. `201 {"ticket": "<43 chars url-safe>"}`. Reaproveita o padrão de `tokens_acao`/`gerarTokenAcao` (AD-18) com `tipo='realtime_ticket'`, `expira_em = now() + 30s` — nova migration `000020` amplia o `CHECK` de `tokens_acao.tipo` (mesmo molde exato da `000006` para `mfa_login`). Ao contrário de `IniciarLoginMFA`, **não invalida** tickets anteriores não usados do mesmo usuário: cada aba abre sua própria conexão SSE e cada uma precisa do seu próprio ticket; invalidar o anterior quebraria múltiplas abas simultâneas, e não há requisito de segurança (AD-18) que exija essa invalidação para este `tipo` — só para `mfa_login`, onde um código antigo válido é um risco real.
- `GET /api/realtime/stream?ticket=<token>`: **NÃO** leva `RequireAuth` (`EventSource` não envia header `Authorization`). Consome o ticket atomicamente (mesmo padrão de corrida fechada de `VerificarEmail`: SELECT distingue "não existe" de "expirado/usado", UPDATE repete as mesmas condições), resolve `usuario_id`, revalida via `services.BuscarUsuarioSessao` (conta ainda `ativo`, defesa em profundidade igual a `RequireAuth`) -- falha em qualquer passo -> `401` (envelope de erro fixo, mesmo fora do fluxo JSON normal, antes de promover a resposta a `text/event-stream`). Ticket válido: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`, registra-se no `*realtime.Registry` compartilhado, escreve `data: <envelope json>\n\n` por evento recebido, `: keep-alive\n\n` a cada 15s (evita timeout de proxy/idle numa conexão longa), até `r.Context().Done()` (cliente desconectou) -- então desinscreve.
- `backend/realtime/registry.go` (novo, package `realtime`, **sem** import de `database/sql` -- tickets são responsabilidade de `services/`, ver Design Notes): `Registry` com `Publish(canal string, evento Evento)` (fan-out não bloqueante -- assinante lento perde evento, nunca trava o produtor; `canal` fora de `{produtos,estoques,movimentacoes,pedidos}` -> `panic`, fail-fast, mesmo padrão de `RequireRole` em papel desconhecido) e `Subscribe() (<-chan Evento, func())` (um único fan-out global -- o endpoint de stream não filtra por canal na URL, então cada assinante recebe os 4 canais e filtra client-side por `resource`, exatamente como a AD-3 desenha o endpoint sem parâmetro de canal). `Evento{Resource, ID, Change string}` -- envelope fixo da AD-3, `json:"resource"/"id"/"change"`.
- `CriarProduto`/`AtualizarNomeProduto` continuam com a MESMA assinatura (não tocam `realtime` -- ver Design Notes sobre blast radius). O `Publish("produtos", ...)` acontece em `handlers/produtos.go`, logo após cada chamada bem-sucedida: `change:"created"` (id do Produto criado) em `CriarProdutoHandler`, `change:"updated"` (id do Produto renomeado) em `AtualizarNomeProdutoHandler`. `newMux` cria `registro := realtime.NewRegistry()` localmente (não vira parâmetro novo de `newMux` -- evitaria reescrever todas as chamadas de teste existentes) e passa para os 2 handlers acima + os 2 handlers novos de `realtime`.
- Frontend `frontend/src/lib/realtime/client.ts` (novo): `conectarRealtime(aoReceberEvento, aoMudarStatus): () => void`. Fluxo: `POST /api/realtime/ticket` -> abre `new EventSource('/api/realtime/stream?ticket=' + ticket)` -> `onopen` chama `aoMudarStatus('conectado')` -> `onmessage` faz `JSON.parse` e chama `aoReceberEvento`. `onerror`: fecha a conexão atual (nunca deixa o retry nativo do `EventSource` reenviar o MESMO ticket já consumido), inicia um temporizador de 3000ms -- se ainda não reconectou quando ele dispara, `aoMudarStatus('reconectando')`; tenta de novo (novo ticket, novo `EventSource`) a cada 1000ms até suceder. Função de cleanup fecha o `EventSource` atual e cancela qualquer temporizador pendente.
- `ProdutoDetalhePage` (`frontend/src/pages/ProdutoDetalhePage.tsx`, rota `produtos/:id`, filha de `RotaProtegida`, sem gate de papel próprio -- `usuario`+): a busca inicial E o refetch pós-reconexão são o MESMO caminho de código -- `carregarDetalhe()` só é chamado a partir de `aoMudarStatus('conectado')` (dispara também na primeira conexão), nunca de um `useEffect` de mount separado (AD-3: "sempre GET completo ao reconectar" -- unificar os dois evita dois caminhos divergentes para a mesma responsabilidade). Evento SSE recebido com `resource==='produtos' && id===<id da rota>` -> refetch + `toast.info('Catálogo atualizado.')` (`sonner`, mesmo padrão de `LocaisEstoqueSection`/`CadastroProdutoSection`; o `Toaster` global de `main.tsx` já expõe uma região `aria-live="polite"` nativa do `sonner`, sem wrapper extra). Status `'reconectando'` -> `<output>` persistente "Reconectando..." (`aria-live="polite"`) enquanto durar; unmount desconecta a SSE.
- Mostra nome, código (`font-mono` quando presente, igual Story 4.1/4.3), categoria, dimensões (`resumirDimensoes`, extraído — ver abaixo), indicador de disponibilidade (`IndicadorDisponibilidade`, extraído), lista `porEstoque` (Estoque + quantidade, mesmo formato de `FragmentLinhaGrupo` da 4.3) e as fotos do Produto via `GET /api/produtos/{id}/fotos` (Story 3.6) — mesmo padrão fetch-com-auth + `blob()` + `URL.createObjectURL` de `CadastroProdutoSection.tsx:397-425` (um `<img src>` direto na URL da API falharia: a rota é `RequireAuth`, e `<img>` não envia `Authorization`), miniaturas em grade abrindo lightbox em tela cheia (`Dialog`/`DialogContent`/`DialogClose` de `@/components/ui/dialog`, mesmo molde de `CadastroProdutoSection.tsx:693-720`). 0 fotos -> sem seção de fotos, sem erro.
- Extrai de `CatalogoListagem.tsx` para `frontend/src/components/catalogo/formatacao.tsx` (novo, sem lógica nova): `formatarQuantidade`, `resumirDimensoes`, `IndicadorDisponibilidade` (com seus tipos `Dimensoes`/`DimensaoValor`) — reusados por `CatalogoListagem.tsx` (import) e `ProdutoDetalhePage.tsx`, evitando duplicar a mesma formatação (Story 4.3 já corrigiu uma divergência de formatação equivalente).
- `CatalogoListagem.tsx` (grade, `agrupar=false` apenas — linhas de tabela agrupada continuam SEM navegação, ver Never) e `BuscaCatalogo.tsx`: cada item passa a envolver seu conteúdo num `<Link to={\`/produtos/${id}\`}>` (react-router), preservando estilos/`min-h-touch-target-min` existentes.
- Migration `000020_add_realtime_ticket_to_tokens_acao`: `ALTER TABLE tokens_acao DROP CONSTRAINT tokens_acao_tipo_check; ALTER TABLE tokens_acao ADD CONSTRAINT tokens_acao_tipo_check CHECK (tipo IN ('verificacao_email','redefinicao_senha','mfa_login','realtime_ticket'));` — nenhum backfill necessário (coluna já existe, só o `CHECK` amplia).

**Block If:** nenhuma condição desta story exige decisão humana em runtime — segue direto.

**Never:**
- Nenhuma navegação a partir de uma linha EXPANDIDA da tabela agrupada (Story 4.3): um grupo pode conter vários Produtos distintos (mesmo nome+dimensões, categorias diferentes) — não existe um único `id` de Produto para navegar. O `porEstoque` do grupo já mostra a mesma discriminação por Estoque inline (Story 4.3); a AC "expandir mostra quantidade por Estoque" já está satisfeita ali, sem depender desta story.
- Nenhum evento publicado a partir da importação em massa (Story 3.3/3.4) nem do upload de foto (Story 3.5): a importação processa até milhares de linhas numa requisição — publicar um evento por linha inundaria qualquer assinante de toasts; nenhuma AC desta story ou de UX exige isso. Fica para quando (se) uma story futura precisar.
- Nenhuma ação de edição na tela de detalhe (renomear, upload de foto) — é tela de CONSULTA (FR-7); reaproveita os endpoints já existentes de outra tela só se uma story futura pedir.
- Nenhum WebSocket, Redis Pub/Sub ou fila externa (AD-2/AD-3). Nenhum replay de eventos perdidos — reconexão sempre refaz GET completo.
- `backend/realtime/` não acessa `*sql.DB` — gestão do ticket (linha em `tokens_acao`) fica em `services/realtime.go`, nunca no pacote `realtime` (ver Design Notes sobre a Structural Seed).
- `CriarProduto`/`AtualizarNomeProduto` (`services/produtos.go`) NÃO ganham parâmetro `*realtime.Registry` — só os 2 handlers que os chamam em produção mudam de assinatura (ver Design Notes).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Detalhe de Produto existente | `GET /api/produtos/{id}` válido | `200` com `porEstoque` discriminado | No error |
| Produto sem nenhum `produto_estoque` | id de Produto existente, 0 linhas | `quantidadeTotal:0`, `disponivel:false`, `porEstoque:[]` | `200`, no error |
| `id` inexistente/malformado | UUID que não existe, ou string não-UUID | — | `404 NOT_FOUND` "produto não encontrado" |
| Papel `usuario` chama direto | token `usuario` | `200` | nunca `403` |
| Ticket válido, uso único | `POST /api/realtime/ticket` -> `GET /api/realtime/stream?ticket=` | conecta, stream `text/event-stream` | `200`(stream) |
| Ticket reaproveitado (2ª vez) | mesmo ticket já consumido | — | `401`, conexão recusada |
| Ticket expirado (>30s) | ticket emitido há mais de 30s | — | `401` |
| Sem `ticket` na query / vazio | `GET /api/realtime/stream` sem `ticket` | — | `401` |
| `CriarProduto` bem-sucedido | `POST /api/produtos` | evento `{"resource":"produtos","id":<novo>,"change":"created"}` publicado | `201`, no error |
| `AtualizarNomeProduto` bem-sucedido | `POST /api/produtos/{id}/renomear` | evento `{"resource":"produtos","id":<id>,"change":"updated"}` publicado | `200`, no error |
| SSE entrega evento do Produto aberto | tela de detalhe montada, evento com `id` igual ao da rota | refetch + toast "Catálogo atualizado." | client-side |
| SSE entrega evento de OUTRO Produto | evento com `id` diferente | nenhum refetch/toast nesta tela | client-side |
| Reconexão rápida (<3s) | erro de conexão, reconecta antes do temporizador | nenhum indicador visível | client-side |
| Reconexão lenta (>=3s) | erro de conexão, sem reconectar a tempo | "Reconectando..." persistente até suceder | client-side |

</intent-contract>

## Code Map

- `backend/services/catalogo.go` (Story 4.3, l.44-63 `CatalogoItem`/`EstoqueQuantidade`, l.270-278 `catalogoPorEstoqueQuery`) -- reusar tipos/query; adicionar `ProdutoDetalhe` struct + `ObterProdutoDetalhe(db *sql.DB, id string) (ProdutoDetalhe, error)`: 1 query (colunas de `catalogoGradeQuery` l.126-144, trocando `LIMIT/OFFSET` por `WHERE p.id = $1`, sem paginação) + `catalogoPorEstoqueQuery` com `pq.Array([]string{id})`, agregação simples em Go (sem mapa multi-grupo — só 1 produto). `sql.ErrNoRows`/`pqInvalidTextRepresentation` (constante em `services/promocao.go:17`) -> `services.ErrProdutoNaoEncontrado` (`services/produtos.go:107`).
- `backend/services/produtos.go:180` (`CriarProduto`), `:345` (`AtualizarNomeProduto`) -- **assinatura inalterada**; só os handlers publicam.
- `backend/services/realtime.go` (novo) -- `EmitirTicketRealtime(db *sql.DB, usuarioID string) (token string, err error)` e `ConsumirTicketRealtime(db *sql.DB, token string) (usuarioID string, err error)`, moldados em `services/auth.go:608-634` (`IniciarLoginMFA`, SEM a invalidação de tokens anteriores — ver Boundaries) e `:250-296` (`VerificarEmail`, mesma corrida fechada SELECT+UPDATE condicional). Reusa `gerarTokenAcao` (`auth.go:147`), `ErrTokenNaoEncontrado`/`ErrTokenExpirado` (já existem, mesmo pacote). `tipo='realtime_ticket'`, TTL constante `realtimeTicketExpiracao = 30 * time.Second` (mesmo molde de `mfaLoginTokenExpiracao`, `auth.go:48`).
- `backend/realtime/registry.go` (novo pacote) -- `Evento{Resource,ID,Change string}`, `Registry{mu sync.Mutex; subs map[chan Evento]struct{}}`, `NewRegistry()`, `Publish(canal string, evento Evento)`, `Subscribe() (<-chan Evento, func())`. Canais válidos: `produtos`,`estoques`,`movimentacoes`,`pedidos` (AD-3) — só `produtos` é publicado nesta story.
- `backend/handlers/produtos.go:80` (`CriarProdutoHandler`), `:159` (`AtualizarNomeProdutoHandler`) -- ganham parâmetro `registro *realtime.Registry`; após `err == nil`, `registro.Publish("produtos", realtime.Evento{ID: produto.ID, Change: "created"/"updated"})`. Novo `ObterProdutoHandler(db *sql.DB) http.HandlerFunc` logo após `ListarCatalogoHandler` (:246-294, mesmo molde de guarda `UsuarioDaSessao` + switch de erro de `AtualizarNomeProdutoHandler:174-187`), `r.PathValue("id")`.
- `backend/handlers/realtime.go` (novo) -- `EmitirTicketRealtimeHandler(db *sql.DB) http.HandlerFunc` (`201 {"ticket":...}`, erro de banco -> 500); `StreamRealtimeHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc` (parse `?ticket=`, `ConsumirTicketRealtime`, `BuscarUsuarioSessao` p/ revalidar ativo, `http.Flusher`, loop `select` em `evento`/`time.NewTicker(15s)`/`r.Context().Done()`).
- `backend/main.go:262` (`newMux`) -- `registro := realtime.NewRegistry()` logo no topo; `CriarProdutoHandler(db, registro)` (l.341), `AtualizarNomeProdutoHandler(db, registro)` (l.356) ganham o argumento; novo bloco de rotas após l.410 (catálogo 4.3): `GET /api/produtos/{id}` (`RequireAuth` só), `POST /api/realtime/ticket` (`RequireAuth` só), `GET /api/realtime/stream` (SEM `RequireAuth` -- autenticação própria via ticket). Doc comment do topo (l.1-64) ganha frase da Story 4.4. Import novo: `stockflow/backend/realtime`.
- `backend/migrations/000020_add_realtime_ticket_to_tokens_acao.{up,down}.sql` (novo) -- molde exato de `000006_add_mfa_to_usuarios.up.sql` linhas do `ALTER TABLE tokens_acao DROP/ADD CONSTRAINT`.
- `backend/main_test.go:64` (`seedContaMux`), `:94` (`tokenDeMux`), `:1674` (`TestNewMux_ProdutosCatalogoRotaSoRequireAuth`) -- molde para `TestNewMux_ProdutosDetalheRotaSoRequireAuth`, `TestNewMux_RealtimeTicketRotaSoRequireAuth`, e um teste de fluxo completo ticket->stream->evento (usar `httptest.NewServer(mux)` real, não só `ResponseRecorder`, para exercitar a conexão longa; cancelar via `context`/fechar a resposta após ler o 1º evento).
- `backend/handlers/produtos_test.go:48,102` (`postProdutos`/composição local) -- as 2 chamadas diretas de `CriarProdutoHandler(db)`/`AtualizarNomeProdutoHandler(db)` (fora de `newMux`) precisam de um `realtime.NewRegistry()` extra. **Nenhum outro `_test.go`** muda (services/estoques_test.go, services/importacoes_test.go, services/fotos_test.go, services/catalogo_test.go, handlers/estoques_test.go, handlers/fotos_test.go chamam `services.CriarProduto` diretamente, cuja assinatura não muda).
- `frontend/src/components/catalogo/formatacao.tsx` (novo) -- `formatarQuantidade`, `resumirDimensoes`, `IndicadorDisponibilidade`, `Dimensoes`/`DimensaoValor`, extraídos de `CatalogoListagem.tsx:37-143` (mesmo corpo, sem alteração de lógica).
- `frontend/src/components/catalogo/CatalogoListagem.tsx:37-143` (remove o que foi extraído, importa de `./formatacao`), `:306-320` (envolve cada `<li>` da grade num `<Link to={\`/produtos/${item.id}\`}>`; linhas de tabela agrupada `:391-448` **não mudam**).
- `frontend/src/components/catalogo/BuscaCatalogo.tsx` -- resultado de busca ganha `<Link to={\`/produtos/${produto.id}\`}>` (mesmo padrão).
- `frontend/src/lib/realtime/client.ts` (novo) -- `conectarRealtime`, descrito em Boundaries.
- `frontend/src/pages/ProdutoDetalhePage.tsx` (novo) -- página de detalhe, descrita em Boundaries; usa `useParams()` (react-router) para o `id`, `getAccessToken`/`authHeaders` (mesmo padrão de `CatalogoListagem`).
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:397-425` (`carregarFotos`, fetch+blob+`ObjectURL`), `:693-720` (`Dialog` lightbox) -- molde a reaproveitar (não importar diretamente: estado local incompatível) na galeria de `ProdutoDetalhePage.tsx`.
- `frontend/src/App.tsx:89-99` (`router`) -- nova rota filha `{ path: 'produtos/:id', element: <ProdutoDetalhePage /> }`.
- `frontend/src/test/setup.ts` -- **sem alteração**; o teste de `ProdutoDetalhePage`/`client.ts` sobrescreve `window.EventSource` localmente com uma classe fake (jsdom não implementa `EventSource`; nenhuma lib nova — mesmo espírito de sobrescrever `window.matchMedia` localmente que os testes de tabela da Story 4.3 já fazem).
- `frontend/src/components/catalogo/CatalogoListagem.test.tsx`, `BuscaCatalogo.test.tsx` -- ajustar para os novos `<Link>` (podem quebrar seletores que assumiam `<li>`/resultado sem link).

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000020_*.{up,down}.sql` -- amplia `CHECK` de `tokens_acao.tipo` com `'realtime_ticket'`.
- `backend/realtime/registry.go` (+ teste) -- registry in-process: `Publish`/`Subscribe`, canal inválido faz panic, fan-out não bloqueia em assinante lento.
- `backend/services/realtime.go` (+ teste) -- `EmitirTicketRealtime`/`ConsumirTicketRealtime`: emissão, consumo único (2ª tentativa falha), expiração.
- `backend/services/catalogo.go` (+ teste) -- `ProdutoDetalhe`/`ObterProdutoDetalhe`: com/sem estoque, id inexistente/malformado.
- `backend/handlers/produtos.go` (+ teste) -- `ObterProdutoHandler`; `CriarProdutoHandler`/`AtualizarNomeProdutoHandler` publicam evento no sucesso.
- `backend/handlers/realtime.go` (+ teste) -- ticket handler; stream handler (conecta, recebe evento publicado por outra goroutine/requisição, desconecta ao cancelar contexto).
- `backend/main.go` (+ `main_test.go`) -- registra as 3 rotas novas, injeta `registro` nos 2 handlers existentes, doc comment.
- `frontend/src/components/catalogo/formatacao.tsx` (+ ajusta `CatalogoListagem.tsx`) -- extração sem mudança de comportamento (testes existentes de `CatalogoListagem` continuam verdes).
- `frontend/src/lib/realtime/client.ts` (+ teste com `EventSource` fake) -- conecta, reconecta após erro, indicador "reconectando" só após o limiar.
- `frontend/src/pages/ProdutoDetalhePage.tsx` (+ teste) -- carrega detalhe, mostra `porEstoque`, fotos (lightbox), refetch+toast em evento do mesmo id, ignora evento de outro id, indicador de reconexão.
- `frontend/src/components/catalogo/CatalogoListagem.tsx`, `BuscaCatalogo.tsx` (+ testes) -- navegação por `<Link>`.
- `frontend/src/App.tsx` -- rota `produtos/:id`.

**Acceptance Criteria:**
- Given o detalhe de um Produto, when ele é aberto, then mostra a quantidade discriminada por cada Estoque onde o Produto está presente.
- Given a tela de detalhe aberta assinando o canal `produtos` (AD-3), when um evento sobre o MESMO Produto chega, then um toast discreto (`aria-live="polite"`, via `sonner`) avisa "Catálogo atualizado.", refaz o GET completo, e não recarrega a tela sozinha (UX-DR17).
- Given uma conexão SSE que falha e demora 3s ou mais para reconectar, when isso acontece, then um indicador "Reconectando..." aparece; uma reconexão em menos de 3s permanece silenciosa (UX-DR18).
- Given a reconexão SSE concluída (inclusive a primeira conexão), when o cliente fica online, then ele sempre faz um GET completo do estado atual, nunca espera replay de eventos perdidos (AD-3).
- Given um Usuário com papel `usuario`, when chama `GET /api/produtos/{id}` ou `POST /api/realtime/ticket` diretamente pela API, then a resposta é `200`/`201` (nunca `403`).
- Given um card da grade (Story 4.3) ou um resultado de busca (Story 4.1), when o Usuário clica, then navega para `/produtos/{id}` do Produto clicado.
- Given `POST /api/produtos` ou `POST /api/produtos/{id}/renomear` bem-sucedidos, when a escrita é confirmada, then um evento é publicado no canal `produtos` com o `id` e `change` corretos.

## Spec Change Log

## Review Triage Log

### 2026-08-31 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 6 (high 0, medium 1, low 5)
- defer: 3
- reject: 4 (+ all findings about `.github/workflows/deploy-cliente-aws.yml`/`installer/cliente-aws/*`, not part of this story's diff — see note below)
- addressed_findings:
  - `[low]` `[patch]` `frontend/src/lib/realtime/client.ts` retentava a cada 1000ms para sempre, sem teto/backoff — após um restart do servidor (app single-instance, AD-3), todos os clientes conectados martelariam `POST /api/realtime/ticket` (grava linha em `tokens_acao`) em lockstep indefinidamente. Achado pelo Blind Hunter. Corrigido com backoff progressivo com teto (~5s) e jitter.
  - `[medium]` `[patch]` `ProdutoDetalhePage` não tinha guarda contra o `id` da rota mudar SEM desmontar o componente (React Router não remonta em mudança só de parâmetro) — uma resposta de `carregarDetalhe` de um `id` antigo podia sobrescrever a tela com o Produto ERRADO, contrariando a promessa central do épico ("quantidade sempre confiável"); o cache `objectUrlCacheRef` de fotos (chaveado só por `nome`) tinha o mesmo risco lógico. Achado pelo Blind Hunter e pelo Verification Gap Reviewer (o cache de fotos), de forma independente. Corrigido com guarda de sequência (mesmo padrão de `BuscaCatalogo`/`CatalogoListagem`) que reseta `produto`/`fotos`/`objectUrlCacheRef` e descarta resposta obsoleta quando o `id` muda.
  - `[low]` `[patch]` `ProdutoDetalhePage` colapsava `404` (Produto inexistente) e `500`/erro de rede na mesma mensagem genérica "tente novamente" — enganoso quando o Produto realmente não existe. Achado pelo Blind Hunter. Corrigido com mensagem "Produto não encontrado." específica para `404`, com teste.
  - `[low]` `[patch]` `backend/handlers/realtime.go` (`StreamRealtimeHandler`) não enviava `X-Accel-Buffering: no` — um proxy reverso na frente do deploy (Coolify/Traefik) poderia bufferizar a resposta `text/event-stream` e atrasar a entrega "quase em tempo real", o próprio propósito da story. Achado pelo Edge Case Hunter. Corrigido acrescentando o header junto aos demais headers de SSE.
  - `[low]` `[patch]` `ProdutoDetalhePage`: se `fotos` mudasse (ex. refetch disparado por evento SSE) enquanto o lightbox estava aberto num índice que deixou de existir, o lightbox continuava mostrando uma foto errada/vazia. Achado pelo Edge Case Hunter. Corrigido fechando o lightbox quando `fotos[lightboxIndex]` deixa de existir.
  - `[low]` `[patch]` Nenhum teste afirmava o destino real do `<Link>` dos cards da grade (`CatalogoListagem`) nem dos resultados de busca (`BuscaCatalogo`) — a AC "clicar navega para `/produtos/{id}`" podia regredir (link errado ou revertido para `<li>` sem link) sem nenhum teste falhar. Achado pelo Verification Gap Reviewer. Corrigido com asserções de `href`/`to` nos dois arquivos de teste.
- rejected (amostra do raciocínio):
  - Ticket do SSE trafega em query string (`?ticket=...`), aparecendo em log de proxy/acesso: é a própria decisão da AD-3 (`EventSource` não aceita header customizado); a AD-3 já declara a mitigação (ticket ≠ token de sessão, uso único, TTL 30s) — não é defeito desta implementação.
  - `registro.Publish` síncrono antes de responder, sem teste cobrindo um `panic`/falha dentro de `Publish` afetando a resposta: `canal` é sempre o literal fixo `"produtos"` nos 2 call sites reais — não há caminho de execução alcançável que dispare o `panic` de canal desconhecido em produção.
  - Ordem `ConsumirTicketRealtime` (consome o ticket) antes de checar `http.Flusher`: o `http.ResponseWriter` real do `net/http` desta stack sempre implementa `Flusher` — inalcançável na prática, não é defeito.
  - Todos os achados (13 de 15 do Blind Hunter; 2 de 6 do Edge Case Hunter) sobre `.github/workflows/deploy-cliente-aws.yml` e `installer/cliente-aws/*`: esses arquivos NÃO fazem parte do diff desta story. `baseline_revision` (`f4d37736...`) ficou defasado durante a execução — o commit `a096b98d` ("Add GitHub Actions self-hosted deploy for Ferreira Costa AWS server"), de outra sessão/processo concorrente, chegou em `main` enquanto este run estava em andamento (mesma classe de corrida já registrada no histórico deste repo: commit `f76b076`, "git-race, not a real spec block"). `git status` confirma que o working tree desta story contém exatamente e só os arquivos do Code Map. O diff de review foi reconstruído contra o `HEAD` atual (que já inclui o commit alheio) para as fases seguintes, e nenhuma alteração foi feita nesses arquivos.
- verification (após os patches): `gofmt`/`go build`/`go vet` limpos; `go test -p 1 -count=1 ./...` (contra Postgres 16 real) — todos os pacotes OK; `npm run lint`/`npm run build` OK; `npm run test` — todos os arquivos passam.

## Design Notes

- **`backend/realtime/` sem `*sql.DB`, tickets em `services/`.** A Structural Seed da arquitetura lista "`realtime/` — registry SSE in-process, tickets de conexão" no mesmo pacote, mas `epic-4-context.md` (Technical Decisions) fixa uma regra mais específica e vinculante para este épico: "nenhum acesso a banco fora de `services/`". As duas se reconciliam mantendo `backend/realtime/` como o mecanismo puro de fan-out em memória (sem *sql.DB*), e a leitura/escrita de `tokens_acao` (a parte "tickets de conexão" da frase da spine) em `services/realtime.go`, como qualquer outra escrita de banco do projeto.
- **`CriarProduto`/`AtualizarNomeProduto` não ganham `*realtime.Registry`.** O diagrama da AD-1 mostra `services --> realtime`, mas mudar a assinatura dessas duas funções obrigaria a editar ~35 call sites de teste espalhados por `services/estoques_test.go`, `services/importacoes_test.go`, `services/fotos_test.go`, `services/catalogo_test.go` e outros — todos usando `CriarProduto` só como seed, sem relação nenhuma com tempo real. Publicar a partir do handler (o único lugar de produção que chama essas 2 funções hoje) é o mesmo comportamento observável, sem esse raio de explosão em arquivos de outras stories.
- **Um único fan-out, sem filtro por canal na URL.** A AD-3 desenha `GET /api/realtime/stream?ticket=...` sem parâmetro de canal — um único stream por conexão, cliente filtra por `resource` no envelope. Por isso o `Registry` não particiona assinantes por canal (só valida que `Publish` recebe um dos 4 nomes fixos); isso mantém a infraestrutura pronta para os Épicos 5-7 publicarem em `estoques`/`movimentacoes`/`pedidos` sem precisar de um novo endpoint.
- **Linha de tabela agrupada (Story 4.3) não navega.** Um grupo agrega Produtos de nomes+dimensões iguais mas pode conter `id`s de Produtos distintos (categorias diferentes, por exemplo) — não há um único destino de detalhe. A discriminação por Estoque que a 4.3 já mostra ao expandir permanece a única forma de ver esse dado no modo tabela.
- **Filtro do evento por `id`, não por qualquer evento `produtos`.** A tela de detalhe só reage a um evento cujo `id` bate com o Produto aberto — um evento sobre outro Produto não afeta o que esta tela está mostrando, e mostrar o toast mesmo assim seria ruído sem relação com o que o Usuário está olhando.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` -- sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` -- cobre `realtime`, `services.*Realtime*`/`*ProdutoDetalhe*`, `handlers.*Realtime*`/`ObterProduto*`, `TestNewMux_*Realtime*`/`*ProdutosDetalhe*`.
- `cd frontend && npm run lint && npm run build && npm run test` -- `oxlint`, `tsc`+`vite`, e os testes de `formatacao`, `client.ts` (SSE fake), `ProdutoDetalhePage`, `CatalogoListagem`/`BuscaCatalogo` (link novo).

**Manual checks (if no CLI):**
- `docker compose up --build`, logar como `usuario`: abrir o Catálogo, clicar num card -> detalhe mostra quantidade por Estoque. Em outra aba/sessão, renomear esse Produto (`almoxarife`+) -> a aba do detalhe mostra "Catálogo atualizado." e os dados atualizam sem F5.
- Derrubar a rede (DevTools "Offline") por >=3s com o detalhe aberto -> "Reconectando..." aparece; religar -> volta a atualizar silenciosamente na próxima reconexão rápida.
