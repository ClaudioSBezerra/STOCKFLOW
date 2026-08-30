---
title: 'Story 1.5 — Autorização por papel aplicada no servidor'
type: 'feature'
created: '2026-08-29'
status: 'done'
baseline_revision: '561988bb07c28fb4407b90b589309a16c3df6891'
review_loop_iteration: 0
followup_review_recommended: false
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** O middleware `RequireAuth` (Story 1.4) só distingue "autenticado" de "não autenticado" — nenhum endpoint consegue exigir um papel mínimo, então a hierarquia `usuario < almoxarife < gestor < adm` (AD-8) hoje só existe como intenção de design, sem nenhum ponto de aplicação no servidor. O gating de navegação por papel e o bootstrap de sessão no frontend foram explicitamente adiados pela spec-1-4 para esta story.

**Approach:** Uma tabela de hierarquia de papel como ordem total única em `services` (`RankPapel`), consumida por um novo decorator `middleware.RequireRole(papelMinimo)` que compõe após `RequireAuth` e devolve 403 `FORBIDDEN` quando o papel resolvido está abaixo do mínimo. Um primeiro endpoint real protegido por papel — `GET /api/usuarios`, mínimo `gestor`, com filtro de escopo aplicado no service a partir do papel já resolvido pelo contexto da requisição — é a prova viva do 403 (AC1) e do padrão filtro-de-escopo-via-contexto (AC3). No frontend, um `AuthProvider` faz bootstrap silencioso de sessão ao montar o app (`POST /api/auth/refresh` → `GET /api/auth/me`), a navegação passa a esconder itens acima do papel do usuário, e a rota protegida redireciona conta anônima para `/login`.

## Boundaries & Constraints

**Always:**
- A hierarquia de papel é uma ordem total codificada UMA vez no backend, em `services.RankPapel`: `usuario=1 < almoxarife=2 < gestor=3 < adm=4`; papel desconhecido/vazio → `0` (sempre abaixo de qualquer mínimo). Nenhuma rota reimplementa comparação de papel nem usa allow-list de pares (AD-8 forma 1).
- `RequireRole(papelMinimo string)` devolve `func(http.HandlerFunc) http.HandlerFunc` (mesma forma de `RequireAuth`) e é SEMPRE composto depois de `RequireAuth` em `newMux`. Lê o `services.UsuarioSessao` do contexto via `middleware.UsuarioDaSessao`. Contexto sem `UsuarioSessao` (composição sem `RequireAuth` antes) → 500 `INTERNAL_ERROR` + `slog.Error` (mesmo guard de `MeHandler`). Papel abaixo do mínimo → 403 `{"error":{"code":"FORBIDDEN","message":...}}` — código já no vocabulário fixo do AD-14, nenhum código novo. Papel suficiente → chama o próximo handler sem tocar o banco. A decisão allow/deny vive só no middleware, nunca no handler.
- `GET /api/usuarios` é registrado como `RequireAuth(db, jwtSecret)(RequireRole("gestor")(handlers.ListarUsuariosHandler(db)))`. Responde `{"usuarios":[{"id","nome","email","papel","ativo"}]}` ordenado por `criado_em`. O filtro de escopo vive em `services.ListarUsuarios(db, papelSolicitante string)`, que recebe o papel resolvido pelo middleware como argumento explícito e NUNCA reconsulta `usuarios` para descobrir o papel de quem chama (AD-8 forma 3): `adm` recebe todas as contas; `gestor` recebe só `papel IN ('usuario','almoxarife')` (EXPERIENCE.md, Configurações→Usuários: `gestor` escopo limitado, `adm` completo).
- Frontend: `AuthProvider` (`src/lib/auth.tsx`) envolve o router. Ao montar, tenta `POST /api/auth/refresh` (cookie same-origin); em 200 guarda o access token via `lib/session.ts` e chama `GET /api/auth/me` com `Authorization: Bearer`; em 200, estado `autenticado` com `{id,nome,email,papel}`. Falha em qualquer passo → estado `anonimo`, sem erro visível (bootstrap silencioso). Enquanto os fetches não resolvem → estado `carregando`.
- `useAuth()` expõe `{estado, usuario}` e `definirSessao(usuario, token)`. `LoginPage` chama `definirSessao(body.usuario, body.token)` no sucesso do login (a resposta de `/api/auth/login` já traz `token`+`usuario`) em vez de `setAccessToken` direto — sem isso, `navigate('/')` cairia na rota protegida ainda `anonimo` e voltaria para `/login`.
- A árvore de rotas do `AppShell` (`/`) fica atrás de um wrapper `RotaProtegida`: `carregando` → tela mínima de carregamento; `anonimo` → `<Navigate to="/login" replace />`; `autenticado` → `<AppShell />`. `/login`, `/cadastro`, `/verificar-email` continuam públicas, fora do wrapper.
- `nav-items.ts`: cada item ganha `papelMinimo` — `primary` (Catálogo/Carrinho/Pedidos) e `profile` (Meu Perfil) → `'usuario'`; `admin` (Estoques/Normalização/Relatórios) → `'almoxarife'` (EXPERIENCE.md, tabela de superfícies). Um helper puro `filtrarNavPorPapel(items, papel)` usa a mesma ordem total via `rankPapel` em TS (espelho mínimo e documentado de `services.RankPapel`). `AppShell` só renderiza itens cujo `papelMinimo` o papel do usuário alcança, nas três superfícies (rail, bottom nav, Sheet "Mais") — item sem permissão simplesmente não aparece, nunca desabilitado, nunca tela de "acesso negado".

**Block If:** nenhuma decisão desta story depende de aprovação humana.

**Never:**
- Nenhuma mutação de conta (desativar/rebaixar/promover), nenhuma tela de Gestão de Usuários / Promoções / Log de Acesso — Stories 1.7/1.8/1.12. `GET /api/usuarios` aqui é somente leitura, sem paginação/busca/ordenação configurável — só o necessário para provar 403 + filtro de escopo.
- Nenhuma comparação "ator pode agir sobre alvo" (`rank(ator) > rank(alvo)`, AD-8 forma 2) — sem consumidor nesta story; entra na 1.7/1.8, primeiras a agir sobre uma conta-alvo.
- Nenhum endpoint de logout / revogação de sessão; nenhum refresh proativo nem interceptor de 401 no frontend — o único gatilho de bootstrap é a montagem do `AuthProvider`.
- Nenhum redirecionamento de `/login`→`/` para conta já autenticada — não exigido por nenhuma AC.
- Nenhuma mudança no formato do access token nem em `RequireAuth`/`middleware/auth.go` — `RequireRole` é aditivo e consome o `UsuarioSessao` que `RequireAuth` já injeta.
- Nenhum gating por MFA/rota de Segurança nem bloqueio de navegação por MFA obrigatório — Story 1.11.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| `usuario` chama rota `gestor`+ | JWT válido, `papel=usuario`, `GET /api/usuarios` | 403 `FORBIDDEN` | handler nunca executa |
| `almoxarife` chama rota `gestor`+ | JWT válido, `papel=almoxarife` | 403 `FORBIDDEN` | handler nunca executa |
| `gestor` chama `GET /api/usuarios` | JWT válido, `papel=gestor` | 200; só contas `usuario`/`almoxarife` | — |
| `adm` chama `GET /api/usuarios` | JWT válido, `papel=adm` | 200; todas as contas (inclui `gestor`/`adm`) | — |
| Sem token em rota `gestor`+ | sem header `Authorization` | 401 `TOKEN_EXPIRED` (via `RequireAuth`, antes de `RequireRole`) | — |
| Conta desativada entre emissão e uso do token | `ativo=false` no momento da requisição | 401 `SESSION_REVOKED` (via `RequireAuth`) | `RequireRole` nunca roda |
| `RequireRole` sem `RequireAuth` antes | contexto sem `UsuarioSessao` | 500 `INTERNAL_ERROR` + `slog.Error` | erro de composição, não de request |
| Papel fora do enum conhecido | `RankPapel("")` / valor inesperado | rank `0` → abaixo de qualquer mínimo → 403 | defensivo |
| Bootstrap: sem cookie de refresh | `POST /api/auth/refresh` → 401 | estado `anonimo`; rota protegida → redireciona `/login` | sem erro visível |
| Bootstrap: refresh ok, `/me` falha | refresh 200, `/me` → 401/erro | estado `anonimo` | sem erro visível |
| Bootstrap: sessão válida | refresh 200 + `/me` 200 | estado `autenticado`; nav filtrada por `usuario.papel` | — |
| Login bem-sucedido | `LoginPage` chama `definirSessao` | estado `autenticado` imediato; `navigate('/')` permanece em `/` | — |
| Nav para papel `usuario` | `estado=autenticado`, `papel=usuario` | Catálogo/Carrinho/Pedidos/Meu Perfil visíveis; Estoques/Normalização/Relatórios ausentes nas 3 superfícies | — |

</intent-contract>

## Code Map

- `backend/services/papel.go` (novo, pacote `services`) -- consts `PapelUsuario`/`PapelAlmoxarife`/`PapelGestor`/`PapelAdm` + `RankPapel(papel string) int` (ordem total AD-8; desconhecido → 0). Fonte única da hierarquia no backend.
- `backend/middleware/roles.go` (novo, pacote `middleware`) -- `RequireRole(papelMinimo string) func(http.HandlerFunc) http.HandlerFunc`; usa `UsuarioDaSessao` (`middleware/auth.go:106`), compara via `services.RankPapel`, reusa `escreverErro` do pacote (`middleware/auth.go:44`). Novo `import "log/slog"` aqui.
- `backend/middleware/auth.go` -- SEM mudança; `UsuarioDaSessao`/`usuarioSessaoCtxKey` já expõem o papel resolvido do Postgres.
- `backend/services/usuarios.go` (novo) -- `UsuarioResumo` struct + `ListarUsuarios(db *sql.DB, papelSolicitante string) ([]UsuarioResumo, error)`; `SELECT id,nome,email,papel,ativo FROM usuarios [WHERE papel IN ('usuario','almoxarife')] ORDER BY criado_em`. Lista vazia não é erro.
- `backend/handlers/usuarios.go` (novo, pacote `handlers`) -- `ListarUsuariosHandler(db *sql.DB) http.HandlerFunc`; `middleware.UsuarioDaSessao(r.Context())` → `services.ListarUsuarios(db, u.Papel)` → `escreverJSON` (`handlers/auth.go:35`) com `{"usuarios":[...]}`. Guard de contexto ausente → 500, igual a `MeHandler` (`handlers/auth.go:284`).
- `backend/main.go:184-193` (`newMux`) -- registra `mux.HandleFunc("GET /api/usuarios", middleware.RequireAuth(db, jwtSecret)(middleware.RequireRole(services.PapelGestor)(handlers.ListarUsuariosHandler(db))))`. Atualiza o comentário de doc do pacote (rotas servidas).
- `backend/services/papel_test.go`, `backend/middleware/roles_test.go`, `backend/services/usuarios_test.go`, `backend/handlers/usuarios_test.go` (novos) -- `RankPapel` (tabela pura, sem DB); demais via `testDB(t)` (Postgres real, padrão de `middleware/auth_test.go:32`).
- `frontend/src/lib/auth.tsx` (novo) -- `AuthProvider`, `useAuth()`, tipo `UsuarioSessao` (`{id,nome,email,papel}`), estado `carregando|autenticado|anonimo`, `definirSessao`. Bootstrap em `useEffect` na montagem.
- `frontend/src/lib/session.ts` -- comportamento inalterado; atualizar o comentário que cita "fica para a Story 1.5".
- `frontend/src/components/shell/nav-items.ts` -- `papelMinimo` por item + `rankPapel` (TS) + `filtrarNavPorPapel`. Atualizar o comentário "Nenhum item é gated por papel nesta story".
- `frontend/src/components/shell/AppShell.tsx` -- `useAuth()`; filtra `primaryNavItems`/`adminNavItems`/`profileNavItem` por `usuario.papel` no rail, na bottom nav e no Sheet "Mais".
- `frontend/src/App.tsx` -- `<AuthProvider>` em volta do `<RouterProvider>`; `RotaProtegida` como `element` da rota `/`. Atualizar o comentário sobre bootstrap adiado.
- `frontend/src/pages/LoginPage.tsx` -- trocar `setAccessToken(body.token)` por `definirSessao(body.usuario, body.token)`; estender `LoginResposta` com `usuario`.
- `frontend/src/lib/auth.test.tsx` (novo), `frontend/src/components/shell/nav-items.test.ts` (novo), `frontend/src/components/shell/AppShell.test.tsx` (estende), `frontend/src/pages/LoginPage.test.tsx` (estende) -- testes RTL/vitest.

## Tasks & Acceptance

**Execution:**
- `backend/services/papel.go` + `papel_test.go` -- hierarquia como ordem total + `RankPapel`; teste de tabela dos 4 papéis e do caso desconhecido→0 -- satisfaz AC2.
- `backend/middleware/roles.go` + `roles_test.go` -- `RequireRole`: 403 abaixo do mínimo, passa-adiante no mínimo/acima, 500 sem `RequireAuth`, e ordem `RequireAuth`→`RequireRole` (401 antes de 403 quando não há token) -- comportamento de 403 no nível do middleware.
- `backend/services/usuarios.go` + `usuarios_test.go` -- `ListarUsuarios` com filtro de escopo por `papelSolicitante` (Postgres real: semeia contas dos 4 papéis, verifica recorte `gestor` vs `adm`) -- filtro de escopo da AC3.
- `backend/handlers/usuarios.go` + `usuarios_test.go` -- `ListarUsuariosHandler` via `newMux` (httptest): I/O Matrix na fronteira HTTP (403 para `usuario`/`almoxarife`, 200 recortado para `gestor`, 200 completo para `adm`, 401 sem token) -- prova viva da AC1.
- `backend/main.go` -- registro de `GET /api/usuarios` com a cadeia `RequireAuth`→`RequireRole(services.PapelGestor)`.
- `frontend/src/lib/auth.tsx` + `auth.test.tsx` -- `AuthProvider`/`useAuth`/`definirSessao`; bootstrap silencioso (sucesso, refresh falha, `/me` falha) mockando `fetch`.
- `frontend/src/components/shell/nav-items.ts` + `nav-items.test.ts` -- `papelMinimo`, `rankPapel`, `filtrarNavPorPapel` (usuario esconde admin; almoxarife+ mostra).
- `frontend/src/components/shell/AppShell.tsx` + `AppShell.test.tsx` -- nav filtrada por papel nas três superfícies, sob `AuthProvider` mockado.
- `frontend/src/App.tsx` -- `AuthProvider` + `RotaProtegida` (anônimo → `/login`; carregando → tela mínima).
- `frontend/src/pages/LoginPage.tsx` + `LoginPage.test.tsx` -- usa `definirSessao`; teste confirma estado `autenticado` e permanência em `/` após login.

**Acceptance Criteria:**
- Given uma conta autenticada com papel `usuario` (ou `almoxarife`), when ela chama `GET /api/usuarios` diretamente (sem passar pela interface), then a resposta é 403 `FORBIDDEN` e o handler de listagem nunca executa.
- Given a hierarquia `adm=4 > gestor=3 > almoxarife=2 > usuario=1` definida uma única vez em `services.RankPapel`, when `RequireRole` avalia uma rota com papel mínimo exigido, then a decisão usa `RankPapel(papel) >= RankPapel(mínimo)` — nenhuma rota reimplementa a comparação nem usa allow-list de pares.
- Given `GET /api/usuarios` protegido por `RequireRole("gestor")`, when um `gestor` e um `adm` chamam a rota, then o `gestor` recebe apenas contas `usuario`/`almoxarife` e o `adm` recebe todas — o filtro é aplicado no service a partir do papel recebido do contexto da requisição, sem reconsultar `usuarios`.
- Given o app sendo aberto com um cookie de refresh válido, when o `AuthProvider` monta, then ele resolve a sessão via `POST /api/auth/refresh` + `GET /api/auth/me` e a navegação passa a esconder os itens acima do papel do usuário; sem sessão válida, a rota protegida redireciona para `/login`.
- Given um usuário com papel `usuario` autenticado, when o `AppShell` renderiza, then Estoques, Normalização e Relatórios não aparecem em nenhuma das três superfícies de navegação (rail, bottom nav, "Mais"), sem tela de "acesso negado".

## Spec Change Log

## Review Triage Log

### 2026-08-29 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 8: (high 0, medium 1, low 7)
- defer: 0
- reject: 15: (high 0, medium 0, low 15)
- addressed_findings:
  - `[low]` `[patch]` `RequireRole` (`backend/middleware/roles.go`) falhava aberto quando o papel mínimo da rota fosse uma string fora do enum: `services.RankPapel` devolve 0 para desconhecido, e `RankPapel(papel) < 0` nunca é verdadeiro — todo chamador passaria. Corrigido: o rank do `papelMinimo` é resolvido uma vez na construção do decorator e, se 0, `panic` (fail-fast no boot de `newMux`, mesmo espírito de `DATABASE_URL`/`JWT_SECRET`). Espelho no frontend: `filtrarNavPorPapel` (`nav-items.ts`) agora exige `min > 0` — um `papelMinimo` não mapeado esconde o item em vez de mostrá-lo a todos. Testes adicionados nos dois lados.
  - `[low]` `[patch]` Nenhum teste provava que a rota `GET /api/usuarios` REGISTRADA em `newMux` carrega `RequireRole(services.PapelGestor)` — `main_test.go` só cobria o caso sem token (401, produzido só por `RequireAuth`); remover `RequireRole` de `main.go` deixava toda a suíte verde. Adicionado `TestNewMux_UsuariosRotaCarregaRequireRole` despachando pela composição real do `newMux` (token obtido via `POST /api/auth/login` no mesmo mux): `usuario`/`almoxarife` → 403, `gestor`/`adm` → 200.
  - `[low]` `[patch]` O religamento real de `App.tsx` (`AuthProvider` + `RotaProtegida` como `element` da rota `/`) nunca era renderizado por teste — `App.test.tsx` só montava `RotaProtegida` num router montado à mão. Adicionado um bloco que renderiza o `<App />` real com `@/lib/auth` NÃO mockado e `fetch` stubado: refresh 401 → LoginPage em `/login`; refresh+`/me` 200 → `AppShell` em `/`. (`App.tsx` ganhou `export const router` — único identificador extra, sem mudança de runtime — para o teste resetar o router entre casos.)
  - `[medium]` `[patch]` `AuthProvider` (`frontend/src/lib/auth.tsx`) não era seguro no ciclo de vida: sob `StrictMode` (usado em `main.tsx`) o efeito dispara duas vezes → dois `POST /api/auth/refresh` → a rotação de refresh token da Story 1.4 revoga o primeiro e o segundo cai em 401 → a sessão despenca para `anonimo` no carregamento. Corrigido com um `useRef` que garante a sequência de bootstrap rodar no máximo uma vez por montagem; um segundo `useRef` marcado por `definirSessao` faz o bootstrap abortar (sem `setState`) se um login já estabeleceu a sessão (fecha a corrida "login rápido enquanto o refresh ainda está pendente"); o caminho de falha agora chama `clearAccessToken()` antes de `anonimo` (antes, um refresh 200 seguido de `/me` falho deixava o access token rotacionado na memória). Três testes novos (StrictMode chama refresh uma vez; `definirSessao` durante refresh pendente que depois rejeita → continua `autenticado`; refresh 200 + `/me` falho → `getAccessToken()` nulo).
  - `[low]` `[patch]` `services.ListarUsuarios` (`backend/services/usuarios.go`) ordenava por `criado_em` sem desempate — linhas com o mesmo timestamp saíam em ordem arbitrária. Corrigido para `ORDER BY criado_em, id` nos dois ramos da query.
  - `[low]` `[patch]` O filtro de escopo de `ListarUsuarios` usava literais `'usuario'`/`'almoxarife'` em vez das constantes do pacote. Corrigido para `WHERE papel IN ($1, $2)` parametrizado com `PapelUsuario`/`PapelAlmoxarife`.
  - `[low]` `[patch]` `AppShell.tsx` computava a visibilidade do item de perfil com um `rankPapel(...) >= rankPapel(...)` inline — segunda implementação da regra que `filtrarNavPorPapel` já aplica. Unificado para `filtrarNavPorPapel([profileNavItem], papel).length > 0`.
  - `[low]` `[patch]` `RotaProtegida` (`frontend/src/App.tsx`) renderizava o `AppShell` em qualquer estado que não fosse `carregando`/`anonimo`. Reordenado para falhar fechado: `carregando` → loader; `autenticado` → `<AppShell/>`; qualquer outro → `<Navigate to="/login" replace/>`.
- Achados roteados para `reject` (15): dependência de `POST /api/auth/login` devolver `usuario` — a Story 1.4 já devolve `usuarioResposta` com `papel` (testado em `handlers/auth_test.go`), premissa do achado é falsa; cast direto do corpo 200 de `/api/auth/me` sem validação de schema — convenção já usada em todo o frontend (LoginPage/CadastroPage) para o próprio backend confiável; ausência de timeout/`AbortController` nos fetches do bootstrap — nenhum fetch do repositório usa, `catch` já leva a `anonimo`; `ListarUsuarios` usa `db.Query` e não `QueryContext`/`r.Context()` — nenhum acesso a banco no repositório usa variantes `Context` (só `healthHandler`), threading só aqui seria inconsistente; drift entre a tabela de rank Go e o espelho TS sem teste de contrato — duplicação entre linguagens conscientemente aceita nas Design Notes, AC2 fala do middleware do backend não reimplementar por rota, não de fonte única entre linguagens; ausência de teste afirmando que o JSON de `/api/usuarios` não traz `senha_hash` — `UsuarioResumo` é uma projeção dedicada sem o campo, vazamento é estruturalmente impossível; `Ativo` devolvido mas sem asserção de valor — nenhuma AC/linha de matriz o discrimina, todas as seeds são `ativo=true`, a Story 1.8 testa a própria exibição; guarda de autorização dentro de `ListarUsuarios` (`if RankPapel(solicitante) < RankPapel(gestor) { erro }`) — violaria o AD-8 ("decisão allow/deny sempre no middleware, nunca re-derivando o papel no service"); erro de `escreverJSON` engolido sem log — mesma convenção de `escreverJSON`/`escreverErro` em todo `handlers/`; Sheet "Mais"/bottom nav vazios para papel de rank 0 — inalcançável sob o CHECK de enum de `usuarios.papel`, papel de usuário autenticado real é sempre válido; `estado` fora da união em runtime caindo no `AppShell` — endereçado de outra forma pelo P8 (fail-closed), o resto é impossível sob o tipo; `LoginPage` sem `AuthProvider` ancestral fazendo `useAuth` lançar — `LoginPage` só é renderizada dentro de `<App/>`, que sempre provê o contexto; mensagem do guard 500 divergir entre `RequireRole` e `ListarUsuariosHandler` — cosmético, o `code` é `INTERNAL_ERROR` nos dois; `App.test.tsx` ausente do inventário de testes do Code Map — o Code Map é contexto de planejamento, não contrato, e o arquivo foi criado como resposta correta à auditoria de matriz; `<output>` em vez de `role="status"` no loader — troca dirigida pelo `oxlint` (`jsx-a11y/prefer-tag-over-role`), `<output>` já tem `role=status` implícito; "Carregando..." piscar a cada reload — consequência deliberada do token só-em-memória (decisão de spec), sem AC que exija skeleton.
  - Nota do auditor de alinhamento de intenção (descritiva, sem ação): a AC1 cita "endpoint restrito a `almoxarife`+" e o diff aplica a prova viva num endpoint de mínimo `gestor` (`GET /api/usuarios`). A leitura adotada (mecanismo + endpoint real de prova, com `/api/usuarios`@`gestor` ancorado em EXPERIENCE.md) satisfaz a AC1 em substância — um `usuario` chamando a rota diretamente recebe 403 `FORBIDDEN` independentemente da interface — e a fórmula de `RankPapel` vale para qualquer mínimo (provada para `gestor` no HTTP e para os 4 papéis no teste unitário). Modelar a aba "Configurações → Usuários" (`gestor`/`adm`) e comparações "ator sobre alvo" são explicitamente Story 1.7/1.8 pela autoridade do próprio intent (seção `Never`).

### 2026-08-29 — Review pass (follow-up)
- intent_gap: 0
- bad_spec: 0
- patch: 0
- defer: 0
- reject: 24: (high 0, medium 0, low 24)
- addressed_findings:
  - none
- Passagem de review fresca sobre a story já implementada e commitada (`4fb8a35`), disparada por `followup_review_recommended: true` da passagem anterior. Quatro camadas de review (blind hunter, edge-case hunter, verification-gap, intent-alignment) rodaram em paralelo. O auditor de verificação de lacunas retornou explicitamente "No verification gaps found"; o auditor de intenção confirmou implementação de leitura única, sem violação de nenhuma cláusula `Never`, resolvendo toda ambiguidade para o lado fail-closed. Nenhum achado sobreviveu à triagem — todos rejeitados por autoridade do próprio `<intent-contract>`, por convenção estabelecida do repositório, por já terem sido rejeitados na passagem anterior, ou por serem cosméticos/inalcançáveis. Principais rejeições: filtro de escopo `gestor` como par literal `usuario`/`almoxarife` em vez de derivado de rank — o intent especifica o par exato; ausência de paginação/`LIMIT` em `GET /api/usuarios` — o intent exclui explicitamente ("sem paginação/busca/ordenação configurável"); `RequireRole` não checa `Ativo` — o intent atribui o tratamento de conta desativada ao `RequireAuth` (linha da matriz: 401 `SESSION_REVOKED` via `RequireAuth`); rotas `/estoques`|`/normalizacao`|`/relatorios` ainda alcançáveis por URL direta para papel `usuario` — o gating de frontend do intent é visibilidade de navegação + redirect de anônimo, as rotas são placeholders, nenhuma AC cobre digitar a URL; guarda de contexto ausente de `ListarUsuariosHandler` sem teste direto — mesma convenção do repositório para o guard equivalente de `MeHandler` (também sem teste isolado), guarda defensiva estruturalmente inalcançável na rota registrada; duplicação da tabela de rank Go↔TS sem teste de contrato, cast de payload de `/me` sem validação de schema, ausência de timeout/`AbortController` no bootstrap, `db.Query` sem `QueryContext`, ausência de teste de omissão de `senha_hash`, Sheet "Mais" vazio para papel de rank 0, loader sem skeleton/`aria-busy` — todos já rejeitados na passagem anterior pelas mesmas razões, ainda válidas; `export const router` em `App.tsx` só para teste — decisão consciente da passagem anterior, documentada; ordenação com desempate `, id` e ramo genérico não-`adm` de `ListarUsuarios` sub-testados — caminhos inalcançáveis na prática (route exige `RequireRole("gestor")`, `usuarios.papel` sob CHECK), comportamento correto para todas as entradas alcançáveis.

## Design Notes

- **Ordem total única (AD-8 forma 1 / AC2):** `services.RankPapel` é a única tabela no backend. `middleware` já importa `services` (`middleware/auth.go:21`), então `RequireRole` a consome sem ciclo de import. O frontend tem um espelho mínimo (`rankPapel` em `nav-items.ts`, 4 entradas) — duplicação inevitável entre linguagens, documentada; a autoridade de aplicação é o backend, o frontend só decide visibilidade.
- **Filtro de escopo via contexto (AD-8 forma 3 / AC3):** o handler extrai `UsuarioSessao` do contexto e passa `u.Papel` como argumento explícito para `services.ListarUsuarios`. Por assinatura, o service não recebe identidade do requester nem consulta `usuarios` para descobrir o papel de quem chama — ele recebe o papel pronto. Mesmo padrão que Epic 7 (FR-24) usará para "usuário vê só os próprios pedidos".
- **`RequireRole` compõe após `RequireAuth`:** ambos têm a forma `func(http.HandlerFunc) http.HandlerFunc`, então `RequireAuth(...)(RequireRole(...)(h))` encadeia naturalmente e a ausência de token é resolvida por `RequireAuth` (401) antes de `RequireRole` rodar. `RequireRole` isolado sem `RequireAuth` é erro de programação → 500 (mesmo tratamento de `MeHandler` sem contexto).
- **Bootstrap silencioso:** falha de refresh/me nunca vira toast nem erro na tela — só define `anonimo`. Sem refresh proativo nem interceptor de 401 (adiado): o único gatilho é a montagem do `AuthProvider`.

Golden — composição da rota em `newMux`:

```go
mux.HandleFunc("GET /api/usuarios",
    middleware.RequireAuth(db, jwtSecret)(
        middleware.RequireRole(services.PapelGestor)(
            handlers.ListarUsuariosHandler(db))))
```

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: build limpo.
- `cd backend && go vet ./...` -- expected: sem warnings.
- `docker compose up -d db && cd backend && go test -p 1 ./...` -- expected: todos passam, incluindo `RankPapel`, `RequireRole` (403 / passa-adiante / 500), `ListarUsuarios` (recorte gestor vs adm) e os testes HTTP de `GET /api/usuarios` via `newMux`.
- `cd frontend && npm run build && npm run lint && npm run test` -- expected: build/lint limpos; testes de `auth.tsx`, `nav-items`, `AppShell` (nav filtrada por papel) e `LoginPage` (usa `definirSessao`) passam.
- `docker compose up --build` -- expected: `api`/`web` sobem saudáveis; `GET /api/usuarios` responde 403 para token de `usuario` e 200 para token de `gestor`/`adm` através do proxy `/api`.

**Manual checks (if no CLI):**
- Logar como conta `usuario` (criada via `/cadastro` + verificação, papel default `usuario`): confirmar que Estoques/Normalização/Relatórios não aparecem no rail nem no "Mais"; `curl -H "Authorization: Bearer <token>" .../api/usuarios` → 403 `FORBIDDEN`. Promover a conta a `gestor` via SQL direto e repetir: os itens aparecem e `/api/usuarios` → 200 sem contas `adm`/`gestor` na lista.

## Auto Run Result

**Resumo:** Passagem de review de acompanhamento sobre a Story 1.5 já implementada e commitada em `4fb8a35` (autorização por papel aplicada no servidor). Nenhum código foi alterado nesta passagem. Backend: `services.RankPapel` — hierarquia como ordem total única (`usuario=1 < almoxarife=2 < gestor=3 < adm=4`, desconhecido → 0); `middleware.RequireRole(papelMinimo)` — decorator com a mesma forma de `RequireAuth`, composto depois dele, 403 `FORBIDDEN` abaixo do mínimo, 500 se aplicado sem `RequireAuth`, `panic` no boot se o mínimo for um papel desconhecido; primeira rota real protegida por papel `GET /api/usuarios` (mínimo `gestor`) com filtro de escopo em `services.ListarUsuarios` a partir do papel resolvido pelo middleware e passado como argumento explícito (`gestor` vê `usuario`/`almoxarife`, `adm` vê tudo). Frontend: `AuthProvider` faz bootstrap silencioso da sessão ao montar (`POST /api/auth/refresh` → `GET /api/auth/me`), à prova de StrictMode e de corrida com login; `RotaProtegida` gateia a árvore do `AppShell` (loader / redireciona anônimo para `/login` / renderiza autenticado, falhando fechado); navegação filtrada por `papelMinimo` nas três superfícies (rail, bottom nav, "Mais"); `LoginPage` usa `definirSessao(usuario, token)`.

**Arquivos alterados nesta passagem:** apenas `_bmad-output/implementation-artifacts/spec-1-5-autorizacao-por-papel-aplicada-no-servidor.md` (frontmatter `status`/`followup_review_recommended`, novo bloco no `## Review Triage Log`, este `## Auto Run Result`). Os 21 arquivos de código/teste da story permanecem como em `4fb8a35`:
- `backend/services/papel.go` + `papel_test.go` — hierarquia como ordem total + `RankPapel`.
- `backend/middleware/roles.go` + `roles_test.go` — `RequireRole` (403 / passa-adiante / 500 / `panic` no mínimo desconhecido / cadeia `RequireAuth`→`RequireRole`).
- `backend/services/usuarios.go` + `usuarios_test.go` — `UsuarioResumo` + `ListarUsuarios` com filtro de escopo por argumento, `WHERE papel IN ($1,$2)`, `ORDER BY criado_em, id`.
- `backend/handlers/usuarios.go` + `usuarios_test.go` — `ListarUsuariosHandler` (guard 500 igual a `MeHandler`, `{"usuarios":[...]}`); fronteira HTTP pela composição real.
- `backend/main.go` + `main_test.go` — registro de `GET /api/usuarios` com `RequireAuth`→`RequireRole(services.PapelGestor)` + `TestNewMux_UsuariosRotaCarregaRequireRole`.
- `frontend/src/lib/auth.tsx` + `auth.test.tsx` — `AuthProvider`/`useAuth`/`definirSessao`, bootstrap único à prova de StrictMode, sem downgrade de sessão de login, `clearAccessToken()` no caminho de falha.
- `frontend/src/lib/session.ts` — comentário atualizado.
- `frontend/src/components/shell/nav-items.ts` + `nav-items.test.ts` — `papelMinimo` por item, `rankPapel` (espelho TS), `filtrarNavPorPapel` fail-closed (`min > 0`).
- `frontend/src/components/shell/AppShell.tsx` + `AppShell.test.tsx` — nav filtrada por papel nas três superfícies.
- `frontend/src/App.tsx` + `App.test.tsx` — `AuthProvider` + `RotaProtegida` (fail-closed); `export const router` (afordância de teste).
- `frontend/src/pages/LoginPage.tsx` + `LoginPage.test.tsx` — usa `definirSessao`.

**Achados de revisão:** patch 0. defer 0. reject 24 (0 high / 0 medium / 24 low) — todos rejeitados: por autoridade do `<intent-contract>` (paginação/`LIMIT` excluídos, filtro de escopo `gestor` especificado como par literal, tratamento de conta desativada atribuído ao `RequireAuth`, gating de frontend = visibilidade de nav + redirect de anônimo), por convenção do repositório (`db.Query` sem `QueryContext`, guard de contexto sem teste isolado como `MeHandler`, cast de payload de backend confiável, sem timeout/`AbortController`), por já terem sido rejeitados na passagem anterior pelas mesmas razões ainda válidas (drift Go↔TS, `senha_hash`, Sheet "Mais" vazio sob rank 0, loader sem skeleton), ou por serem cosméticos/inalcançáveis (`export const router` decidido conscientemente antes, desempate `, id` e ramo genérico não-`adm` de `ListarUsuarios` inalcançáveis mas corretos). O auditor de verificação de lacunas retornou "No verification gaps found"; o auditor de intenção não apontou `intent_gap` nem `bad_spec`.

**Recomendação de review de acompanhamento:** `false` — 0 achados triados como `patch` nesta passagem (score = 3×0 + 1×0 = 0 < 5).

**Verificação executada:**
- `cd backend && go build ./...` — OK, build limpo (exit 0).
- `cd backend && go vet ./...` — OK, sem warnings (exit 0).
- `go test -p 1 -count=1 ./...` — Docker indisponível neste sandbox (`docker: command not found`); em vez de `docker compose up -d db`, subido um cluster PostgreSQL 16 descartável via `initdb`/`pg_ctl` (binários de `postgresql-16` no host, TCP em `127.0.0.1:5433`), role/db `stockflow` + extensão `pgcrypto`, `DATABASE_URL` apontado para ele. **Todos os testes passam** nos 5 pacotes (`backend` 1.13s, `backend/cmd/seed-admin` 0.78s, `backend/handlers` 4.63s, `backend/middleware` 0.70s, `backend/services` 3.70s), incluindo `RankPapel`, `RequireRole`, `ListarUsuarios`, `GET /api/usuarios` na fronteira HTTP e o `newMux` que prova o `RequireRole` na rota registrada. Cluster parado e removido ao final.
- `cd frontend && npm run build && npm run lint && npm run test` — OK: build limpo, `oxlint` sem achados, **10 arquivos / 77 testes passando**.
- `docker compose up --build` — **não executado**: Docker indisponível (mesma limitação). A composição real API+web+proxy não foi verificada; a superfície HTTP de autorização (`GET /api/usuarios` com 403/200 por papel, cadeia `RequireAuth`→`RequireRole` via `newMux` real) foi exercitada por testes de integração contra Postgres real.

**Riscos residuais:**
- `docker compose up --build` (API+web+proxy juntos) não pôde rodar por ausência do binário `docker` — risco residual baixo: a superfície HTTP de autorização foi exercitada via `newMux` real contra Postgres real, a mesma composição usada em produção.
- Duplicação deliberada da tabela de ordem total entre `backend/services/papel.go` (`RankPapel`) e `frontend/src/components/shell/nav-items.ts` (`rankPapel`) — documentada nos dois arquivos; a autoridade de aplicação é sempre o servidor. Uma mudança futura na hierarquia precisa tocar os dois lados (sem teste de contrato entre linguagens — tradeoff aceito).
- Rotas `/estoques`|`/normalizacao`|`/relatorios` continuam alcançáveis por URL direta para papel `usuario` (só a navegação é filtrada, não há gate de rota por papel). São placeholders nesta fase e o gating por rota entra quando cada tela real chegar; nenhuma AC desta story cobre o acesso por URL digitada.

