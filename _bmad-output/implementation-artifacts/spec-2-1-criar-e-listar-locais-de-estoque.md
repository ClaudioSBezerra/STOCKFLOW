---
title: 'Story 2.1 — Criar e listar locais de Estoque'
type: 'feature'
created: '2026-08-30'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: 'adf5a6c92b5ce4454369ab487de45e85a1cd3824'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-2-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** O Epic 2 abre o domínio de Estoques — os locais físicos onde os Produtos ficam. Hoje não existe tabela `estoques`, endpoint nem tela: todos os épicos seguintes (Produtos, Movimentação, Pedidos) referenciam Estoques e precisam do ciclo mínimo criar + listar, com nome único garantido no banco (bug real do protótipo: nomes duplicados por capitalização/espaço).

**Approach:** Nova migration `000008_create_estoques` cria `estoques` com `id` UUID v4 e uma coluna gerada `nome_normalizado` (lowercase + colapso de espaços) sob índice `UNIQUE` — a atomicidade sob concorrência é da colisão do índice, nunca de um `SELECT`-antes-de-`INSERT`. Novo `services/estoques.go` (`CriarEstoque`, `ListarEstoques`) e `handlers/estoques.go` expõem `POST /api/estoques` (atrás de `RequireAuth → RequireRole(almoxarife)`) e `GET /api/estoques` (só `RequireAuth` — qualquer conta autenticada lista). No frontend, nova rota `/estoques` (o item de nav "Estoques" já existe, `nav-items.ts`) com uma tela "Locais" que tem formulário de cadastro + lista.

## Boundaries & Constraints

**Always:**
- **`estoques`**: `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`, `nome VARCHAR(255) NOT NULL`, `criado_em TIMESTAMPTZ NOT NULL DEFAULT now()`, e `nome_normalizado TEXT GENERATED ALWAYS AS (lower(regexp_replace(btrim(nome), '\s+', ' ', 'g'))) STORED` com `CREATE UNIQUE INDEX idx_estoques_nome_normalizado ON estoques (nome_normalizado)`. Comentário no molde das migrations anteriores (Story 2.1, FR12; por que coluna gerada + índice único, não checagem na aplicação). `down`: `DROP TABLE IF EXISTS estoques`.
- **`services.CriarEstoque(db *sql.DB, nome string) (Estoque, error)`**: faz `btrim` no `nome` recebido; nome vazio após trim ou com mais de 255 runes → `ErrEstoqueValidacao`. Caso válido, `INSERT INTO estoques (nome) VALUES ($1) RETURNING id, nome`. Violação de unicidade (`pq` SQLSTATE `23505`) → `ErrNomeEstoqueDuplicado` (backstop de corrida — nenhuma checagem prévia). `type Estoque struct { ID, Nome string }` com tags JSON `id`/`nome`.
- **`services.ListarEstoques(db *sql.DB) ([]Estoque, error)`**: `SELECT id, nome FROM estoques ORDER BY nome_normalizado ASC`. Lista vazia não é erro. Sem filtro de escopo (AC4: qualquer conta autenticada vê todos).
- **`handlers.CriarEstoqueHandler(db)`**: corpo `{"nome": string}` lido sob `http.MaxBytesReader(w, r.Body, authRequestMaxBytes)`; JSON inválido ou `nome` ausente → `400 VALIDATION_ERROR`. `ErrEstoqueValidacao` → `400 VALIDATION_ERROR`; `ErrNomeEstoqueDuplicado` → `409 CONFLICT`; sucesso → `201 {"estoque": {"id","nome"}}`; guard de `UsuarioDaSessao` ausente → `500 INTERNAL_ERROR` + `slog.Error` (molde de `ListarUsuariosHandler`).
- **`handlers.ListarEstoquesHandler(db)`**: sucesso → `200 {"estoques": [{"id","nome"}, ...]}`; erro de banco → `500 INTERNAL_ERROR` + `slog.Error`. Mesmo guard de contexto ausente.
- **Envelope de erro** `{"error":{"code","message"}}` (AD-14) via os helpers `escreverErro`/`escreverJSON` já existentes no pacote `handlers`. Vocabulário de `code`: `VALIDATION_ERROR`, `CONFLICT`, `FORBIDDEN`, `INTERNAL_ERROR`.
- **`newMux` (main.go)**: registrar `mux.HandleFunc("POST /api/estoques", middleware.RequireAuth(db, jwtSecret)(middleware.RequireRole(services.PapelAlmoxarife)(handlers.CriarEstoqueHandler(db))))` e `mux.HandleFunc("GET /api/estoques", middleware.RequireAuth(db, jwtSecret)(handlers.ListarEstoquesHandler(db)))`. `GET` **não** leva `RequireRole` — a lista é para qualquer conta autenticada. Atualizar o comentário-doc do pacote `main` mencionando a Story 2.1.
- **Frontend — rota**: em `frontend/src/App.tsx`, adicionar `{ path: 'estoques', element: <EstoquesPage /> }` como rota-filha de `/` (dentro de `AppShell`/`RotaProtegida`), antes do `{ path: '*' }` catch-all; importar `EstoquesPage`.
- **Frontend — `EstoquesPage`** (`frontend/src/pages/EstoquesPage.tsx`): renderiza a tela "Locais" só quando `rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife')` (mesma `rankPapel` de `nav-items.ts`, mesmo espelho de autoridade — o servidor é a fonte real). Papel abaixo → mensagem curta "Você não tem acesso à área de Estoques." (o item de nav já não aparece para esses papéis; isto cobre navegação direta pela URL). Layout no molde de `ConfiguracoesPage` (`<div className="flex flex-col gap-6 p-6">`).
- **Frontend — `LocaisEstoqueSection`** (`frontend/src/components/estoques/LocaisEstoqueSection.tsx`): um `Card` (molde de `GestaoUsuariosSection`) com (a) formulário `<Input>` + `<Button>` "Adicionar estoque" → `POST /api/estoques` com `{ nome }` e `authHeaders()` via `getAccessToken`; (b) lista de `GET /api/estoques` carregada no `useEffect` de mount. Sucesso do cadastro: `toast.success('Estoque criado.')` (`sonner`, `Toaster` já montado em `main.tsx`), limpa o input e refaz o `GET`. `409` → `<p role="alert">` "Já existe um estoque com esse nome." Outro erro de cadastro ou erro de carga da lista → `<p role="alert" className="text-body text-destructive">` genérico (molde de `GestaoUsuariosSection`). Lista vazia → "Nenhum estoque cadastrado ainda." Botão desabilitado enquanto `enviando` ou `nome` em branco (defesa contra duplo-submit, molde de `ConfiguracoesPage`).
- **Testes** em todas as camadas (ver Code Map). Backend usa Postgres real via `testDB(t)` (pula sem `DATABASE_URL`); frontend usa `vitest` com `fetch`/`@/lib/auth`/`@/lib/session` mockados.

**Block If:** nada nesta story depende de decisão humana nem de ação de operador fora do repositório — tabela, endpoints e UI são inteiramente implementáveis por um agente. Status final esperado: `done`.

**Never:**
- **`DELETE /api/estoques/{id}` não é desta story.** A exclusão de Estoque (endpoint, gate de papel para exclusão, `ConfirmDialog`, tratamento de resíduo/pedido pendente) é toda da Story 2.2 — o Epic 2 divide assim explicitamente. A cláusula "ou excluir" da AC3 desta story é atendida quando a 2.2 adiciona a rota `DELETE` atrás do mesmo `RequireRole(almoxarife)`. Não criar rota, handler nem botão de excluir aqui.
- **Nenhuma emissão de evento SSE** no canal `estoques` (AD-3): o registry `realtime/` ainda não existe no código e as ACs desta story não o mencionam. Quando a infra AD-3 chegar, `CriarEstoque` publica `{"resource":"estoques","id":...,"change":"created"}`; até lá, a tela só busca no mount.
- **Nenhuma sub-navegação de abas** Locais/Movimentações no módulo Estoques — Movimentações é o Epic 5. `/estoques` é uma página única (a tela "Locais"). `AppShell` `tabs`/`sideNav` ficam sem consumidor.
- **Nenhuma edição/renome de Estoque** — fora do escopo (a AC cobre só criar e listar).
- **Nenhuma mudança em `nav-items.ts`** — o item `estoques` (`to: '/estoques'`, `papelMinimo: 'almoxarife'`) já existe desde a Story 1.2.
- **Nenhuma tabela ou coluna nova** além de `estoques`; nenhuma alteração nas tabelas existentes.
- **Nenhum filtro/paginação/busca** na listagem — `GET /api/estoques` devolve todos os Estoques ordenados por nome.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Cadastro válido | `POST /api/estoques` `{"nome":"Canteiro A"}`, sessão `almoxarife`+ | `201 {"estoque":{"id":"<uuid>","nome":"Canteiro A"}}`; 1 linha em `estoques` | — |
| Nome duplicado exato | nome `"Canteiro A"` já existe | `409 CONFLICT` | envelope de erro, nenhuma linha nova |
| Nome duplicado por caixa/espaço | existe `"Canteiro A"`, envia `"  canteiro   a "` | `409 CONFLICT` (mesmo `nome_normalizado`) | idem |
| Cadastro concorrente do mesmo nome | 2 requisições simultâneas com nome equivalente | exatamente 1 → `201`; a outra → `409` (colisão do índice único) | perdedora recebe `23505` → `ErrNomeEstoqueDuplicado` |
| Nome em branco | `{"nome":"   "}` ou `{"nome":""}` | `400 VALIDATION_ERROR` | envelope de erro |
| `nome` ausente / JSON inválido / corpo > limite | `{}` ou `"{"` ou corpo enorme | `400 VALIDATION_ERROR` | envelope de erro |
| Papel `usuario` cadastra via API | `POST /api/estoques`, sessão `usuario` | `403 FORBIDDEN`, corpo do envelope (nunca `{"estoque":...}`); handler não executa, banco não é tocado | decidido por `RequireRole` |
| Sem autenticação | `POST` ou `GET /api/estoques` sem `Authorization` | `401 TOKEN_EXPIRED` | decidido por `RequireAuth` |
| Listagem por qualquer conta | `GET /api/estoques`, sessão `usuario`/`almoxarife`/`gestor`/`adm` | `200 {"estoques":[{"id","nome"}...]}` ordenado por nome (normalizado) asc; `[]` se vazio | — |
| Frontend, sessão abaixo de `almoxarife` acessa `/estoques` | navegação direta pela URL | página mostra "Você não tem acesso à área de Estoques."; sem formulário nem lista | — |
| Frontend, cadastro bem-sucedido | `almoxarife` preenche o nome e envia | `toast.success`, input limpo, lista recarregada com o novo estoque | — |
| Frontend, cadastro com nome já existente | `almoxarife` envia nome duplicado | `<p role="alert">` "Já existe um estoque com esse nome."; lista inalterada | — |
| Frontend, falha de carga da lista | `GET /api/estoques` responde `!ok` no mount | `<p role="alert">` genérico; sem lista fantasma | — |

</intent-contract>

## Code Map

- `backend/migrations/000008_create_estoques.up.sql` / `.down.sql` (novos) — `up`: `CREATE TABLE estoques (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), nome VARCHAR(255) NOT NULL, criado_em TIMESTAMPTZ NOT NULL DEFAULT now(), nome_normalizado TEXT GENERATED ALWAYS AS (lower(regexp_replace(btrim(nome), '\s+', ' ', 'g'))) STORED)` + `CREATE UNIQUE INDEX idx_estoques_nome_normalizado ON estoques (nome_normalizado)`. Comentário-cabeçalho no molde de `000004`/`000007` (por que coluna gerada + índice único imposto pelo banco, não `SELECT`-antes-de-`INSERT`; `regexp_replace(...,'\s+',' ','g')` é `IMMUTABLE`, requisito de coluna gerada). `down`: `DROP TABLE IF EXISTS estoques;`.
- `backend/services/estoques.go` (novo) — `type Estoque struct { ID string \`json:"id"\`; Nome string \`json:"nome"\` }`; `var ErrEstoqueValidacao`, `var ErrNomeEstoqueDuplicado` (`errors.New`); `const pqUniqueViolation = "23505"` (molde de `services/auth.go:32` — pacote não o exporta entre arquivos? ele é `const` de pacote; **reusar o já existente**, não redeclarar); `CriarEstoque(db, nome)` (trim, valida 1..255 runes, `INSERT ... RETURNING id, nome`, mapeia `pq.Error.Code == pqUniqueViolation` → `ErrNomeEstoqueDuplicado`); `ListarEstoques(db)` (`SELECT id, nome FROM estoques ORDER BY nome_normalizado ASC`; molde de `ListarUsuarios`, `services/usuarios.go`).
- `backend/services/estoques_test.go` (novo) — `CriarEstoque`: sucesso grava a linha; duplicata exata e duplicata por caixa/espaço → `ErrNomeEstoqueDuplicado`; nome em branco / > 255 → `ErrEstoqueValidacao`; concorrência (duas goroutines, molde de `promocao_test.go` `sync`) → 1 sucesso + 1 `ErrNomeEstoqueDuplicado`. `ListarEstoques`: vazio → `[]`; ordena por nome normalizado asc.
- `backend/handlers/estoques.go` (novo) — `type criarEstoqueRequest struct { Nome string \`json:"nome"\` }`; `CriarEstoqueHandler(db)` e `ListarEstoquesHandler(db)` no molde de `gestao_usuarios.go`/`usuarios.go` (guard `middleware.UsuarioDaSessao`, `http.MaxBytesReader` com `authRequestMaxBytes`, `switch` sobre os erros de `services`, `escreverJSON`/`escreverErro`). Comentário-doc explicando que `POST` fica atrás de `RequireRole(almoxarife)` em `newMux` e `GET` só de `RequireAuth`.
- `backend/handlers/estoques_test.go` (novo) — despacha pela composição real (molde de `postDesativacao`/`getUsuarios`, `gestao_usuarios_test.go` / `usuarios_test.go`): helper `criarContaComPapel` + `tokenDeLogin` já existentes. `POST`: `almoxarife`/`gestor`/`adm` com nome válido → `201` e corpo `{"estoque":{...}}`; nome duplicado → `409`; nome em branco / `{}` / JSON inválido → `400`; `usuario` → `403` (corpo é envelope, nunca `{"estoque":...}`); sem token → `401`. `GET`: cada papel autenticado (inclusive `usuario`) → `200` com as linhas ordenadas; sem token → `401`. Trava o conjunto de chaves de fio decodificando em `[]map[string]any` (`id`, `nome`).
- `backend/main.go` — `newMux`: registrar as duas rotas (ver `Always`); atualizar o comentário-doc do pacote (linhas 1-19) citando a Story 2.1.
- `backend/main_test.go` — adicionar `TestNewMux_EstoquesRotaCarregaRequireRole` (molde de `TestNewMux_UsuariosRotaCarregaRequireRole`, main_test.go:521): `POST /api/estoques` com token `usuario` → `403`, com `almoxarife` → `201`/`400` (não `403`); e um caso em `TestNewMux_RegistraRotasDeAutenticacao` (ou equivalente) provando que `GET /api/estoques` sem token → `401` mas **não** carrega `RequireRole` (token `usuario` → `200`).
- `frontend/src/App.tsx` — importar `EstoquesPage`; adicionar `{ path: 'estoques', element: <EstoquesPage /> }` como filha de `/` antes do `{ path: '*' }`. Atualizar o comentário-doc do bloco de rotas.
- `frontend/src/App.test.tsx` — se algum teste enumera as rotas-filhas do shell, incluir `/estoques`.
- `frontend/src/pages/EstoquesPage.tsx` (novo) — gate `rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife')` (import de `@/components/shell/nav-items` e `@/lib/auth`); acima do gate, `<LocaisEstoqueSection />`; abaixo, `<p>` de acesso restrito. Molde estrutural de `ConfiguracoesPage`.
- `frontend/src/pages/EstoquesPage.test.tsx` (novo) — `almoxarife`/`gestor`/`adm` veem a seção "Locais"; `usuario` vê a mensagem de acesso restrito e nenhum formulário.
- `frontend/src/components/estoques/LocaisEstoqueSection.tsx` (novo) — ver `Always`. `authHeaders()` local (molde de `GestaoUsuariosSection.tsx:47`), `useState` para `nome`, `estoques`, `enviando`, `erro`, `erroCarregar`; `carregar` (`GET`), `useEffect` no mount, `enviar` (`POST`, trata `201`/`409`/erro). `toast` de `sonner`.
- `frontend/src/components/estoques/LocaisEstoqueSection.test.tsx` (novo) — lista o que vem do `GET`; cadastro `201` → `fetch` do `POST` com `{nome}`, `toast.success`, `GET` refeito, input limpo; `409` → `role="alert"` específico; `GET` `!ok` no mount → `role="alert"`; botão desabilitado com input vazio.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000008_create_estoques.{up,down}.sql` — tabela `estoques` + coluna gerada `nome_normalizado` + índice `UNIQUE`.
- `backend/services/estoques.go` (+ `estoques_test.go`) — `CriarEstoque` (validação, `INSERT`, `23505` → duplicado) e `ListarEstoques` (ordenado por nome normalizado).
- `backend/handlers/estoques.go` (+ `estoques_test.go`) — `CriarEstoqueHandler` (201/400/409) e `ListarEstoquesHandler` (200), fronteira HTTP pura.
- `backend/main.go` (+ `main_test.go`) — `POST /api/estoques` atrás de `RequireAuth → RequireRole(almoxarife)`; `GET /api/estoques` só `RequireAuth`; doc do pacote atualizada.
- `frontend/src/App.tsx` (+ teste se aplicável) — rota `/estoques` dentro do shell.
- `frontend/src/pages/EstoquesPage.tsx` (+ teste) — gate de papel `almoxarife`+ na tela.
- `frontend/src/components/estoques/LocaisEstoqueSection.tsx` (+ teste) — formulário de cadastro + lista, com `toast` no sucesso e `role="alert"` nos erros.

**Acceptance Criteria:**
- Given uma sessão `almoxarife` (ou acima), when ela faz `POST /api/estoques` com um nome novo, then a resposta é `201` com `id` (UUID v4) e `nome`, e existe uma linha em `estoques` cujo `nome_normalizado` é o nome em minúsculas com espaços colapsados.
- Given um Estoque já cadastrado, when outra requisição — inclusive concorrente — envia o mesmo nome com capitalização/espaçamento diferente, then a resposta é `409 CONFLICT` (envelope de erro) e nenhuma segunda linha é criada para aquele nome.
- Given uma sessão de papel `usuario`, when ela chama `POST /api/estoques` diretamente pela API, then a resposta é `403 FORBIDDEN` com o corpo do envelope de erro e o handler nunca executa (nada é gravado).
- Given qualquer conta autenticada, when ela chama `GET /api/estoques`, then a resposta é `200 {"estoques":[...]}` com `id` e `nome` de cada Estoque ordenados por nome; uma sessão sem `Authorization` recebe `401`.
- Given um `almoxarife` na tela `/estoques`, when cadastra um nome já existente, then vê a mensagem "Já existe um estoque com esse nome." e a lista não muda; ao cadastrar um nome novo, vê um toast de sucesso e o novo Estoque aparece na lista.

## Design Notes

- **Por que coluna gerada `nome_normalizado` + índice único (e não checagem na aplicação):** a AC exige unicidade "de forma atômica, inclusive sob requisições concorrentes". Um `SELECT ... WHERE nome_normalizado = $1` seguido de `INSERT` tem janela de corrida. A coluna `GENERATED ALWAYS AS ... STORED` sob `UNIQUE INDEX` faz o próprio Postgres recusar a segunda linha; o serviço só traduz o `23505`. Mesma filosofia de `idx_usuarios_unico_adm` (000001) e `idx_solicitacoes_promocao_pendente_unica` (000004).
- **Normalização escolhida:** `lower(regexp_replace(btrim(nome), '\s+', ' ', 'g'))` — minúsculas, remove espaços das pontas, colapsa qualquer sequência de espaços internos para um único espaço. Cobre "Canteiro A" ≡ "canteiro  a" ≡ " CANTEIRO A ". `regexp_replace` (3/4-arg) é `IMMUTABLE`, condição para uso em coluna gerada.
- **`GET /api/estoques` sem `RequireRole`:** deliberado — AC4 diz "qualquer Usuário autenticado". O item de nav "Estoques" continua com `papelMinimo: 'almoxarife'` (a *tela* de gestão é `almoxarife`+); o *endpoint* de leitura é aberto porque telas de Produto/Catálogo (Epic 4) vão listar nomes de Estoque para contas `usuario`.
- **Escopo do "excluir" na AC3:** a Story 2.1 entrega criar + listar; a exclusão (rota `DELETE`, `ConfirmDialog`, guards de resíduo/pedido) é inteiramente da Story 2.2, que "entrega a exclusão funcional agora". O gate de papel para exclusão nasce junto com a rota, na 2.2 — por isso esta spec não cria `DELETE /api/estoques/{id}`.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` — Postgres real (mesmo setup das Stories 1.5–1.12). Cobre `services/estoques_test.go`, `handlers/estoques_test.go` e `main_test.go`; a migration `000008` aplica sem erro e o índice único rejeita nome normalizado duplicado.
- `cd frontend && npm run lint && npm run build && npm run test` — `oxlint`, `tsc`+`vite` e os novos casos de `EstoquesPage.test.tsx` / `LocaisEstoqueSection.test.tsx` passam.
- `docker compose up --build` — logado como `almoxarife`, abrir "Estoques", cadastrar "Canteiro A" e depois "  canteiro a " → o segundo tentativa mostra "Já existe um estoque com esse nome."; `GET /api/estoques` com uma sessão `usuario` responde `200`; `POST /api/estoques` com sessão `usuario` responde `403`. Se `docker` indisponível, mesma nota das stories anteriores (cobertura equivalente via testes de integração contra Postgres real).

**Manual checks (if no CLI):**
- `SELECT nome, nome_normalizado FROM estoques` após cadastrar "  Canteiro  A " mostra `nome` preservado e `nome_normalizado = 'canteiro a'`.
- Navegar direto para `/estoques` com uma conta `usuario` mostra "Você não tem acesso à área de Estoques." e nenhum formulário.
