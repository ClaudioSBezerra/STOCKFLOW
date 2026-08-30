---
title: 'Story 1.8 — Gestão de contas: desativação e rebaixamento'
type: 'feature'
created: '2026-08-30'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '0791ec2782d8394b01b52d761d493da41afc9a5b'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Não existe caminho no produto para `gestor`/`adm` desativarem ou rebaixarem uma conta (FR-31). A coluna `usuarios.ativo` já existe (migration 000001) e o `middleware.RequireAuth` já derruba conta inativa e relê o papel a cada requisição (AD-6), mas nenhuma rota escreve nesses campos e a tela "Configurações → Usuários" (EXPERIENCE.md) não existe — hoje só há `GET /api/usuarios` (Story 1.5, somente leitura, já com recorte de escopo).

**Approach:** Duas rotas de escrita em `handlers/gestao_usuarios.go` + regra em `services/gestao_usuarios.go`: `POST /api/usuarios/{id}/desativacao` (corpo `{"ativo": bool}` — desativa e revoga sessões, ou reativa) e `POST /api/usuarios/{id}/rebaixamento` (sem corpo — rebaixa um degrau na hierarquia, alvo derivado no servidor). Ambas atrás de `RequireAuth(...)(RequireRole(gestor)(...))`, com recorte de autoridade no service (`gestor` só age sobre `usuario`/`almoxarife`; `adm` sobre qualquer conta; ninguém sobre a própria). Frontend: nova seção "Gestão de Usuários" (componente `GestaoUsuariosSection`) montada em `ConfiguracoesPage` para `gestor`/`adm`, listando `GET /api/usuarios` com ações "Desativar"/"Reativar"/"Rebaixar" por linha, cada ação destrutiva confirmada via `ConfirmDialog` (Story 1.2). Nenhuma migration nova.

## Boundaries & Constraints

**Always:**
- **Rebaixamento = um degrau (inverso exato de `proximoPapelPromocao`, Story 1.7):** `gestor → almoxarife`, `almoxarife → usuario`. `usuario` não tem papel abaixo → `409 CONFLICT`. O alvo é SEMPRE derivado no servidor de `papelImediatamenteAbaixo(papelAtual)` — nenhum corpo de requisição informa papel (mesma regra `Never` da Story 1.7).
- **Escopo de autoridade (AC3/AC4), decidido no service a partir do papel do ator já resolvido pelo contexto (`middleware.UsuarioDaSessao`), NUNCA reconsultando `usuarios` (AD-8 forma 3):** `RankPapel(papelAtor) < RankPapel(PapelAdm)` (ou seja, `gestor`) agindo sobre alvo de papel `gestor` ou `adm` → `403 FORBIDDEN`, sem tocar em nada. `adm` age sobre qualquer conta.
- **Guarda de auto-ação:** `alvoID == atorID` → `403 FORBIDDEN` (`ErrGestaoForaDeEscopo`), antes de qualquer escrita — evita um `adm` se trancar para fora.
- `POST /api/usuarios/{id}/desativacao` (`RequireAuth` + `RequireRole(gestor)`; corpo `{"ativo": bool}` sob `http.MaxBytesReader(authRequestMaxBytes)`): o campo é decodificado em um `*bool` — corpo `{}`, `{"ativo":null}`, chave errada ou não-JSON → `400 VALIDATION_ERROR` (molde de `decisaoRequest`, Story 1.7). `id` inexistente OU não-UUID (`pq` `22P02`) → `404 NOT_FOUND`. Escopo/auto-ação → `403`. Caso válido, numa única transação: `UPDATE usuarios SET ativo = $1 WHERE id = $2`; se `ativo=false`, também `UPDATE sessoes SET revogado_em = now() WHERE usuario_id = $2 AND revogado_em IS NULL` (molde de `RedefinirSenha`, auth.go). Resposta `200` `{"usuario": <UsuarioResumo>}` com o estado já atualizado.
- `POST /api/usuarios/{id}/rebaixamento` (`RequireAuth` + `RequireRole(gestor)`; sem corpo): mesmos guards de `id`/escopo/auto-ação. `papelImediatamenteAbaixo(papelAlvoAtual)` sem resultado (`usuario`) → `409 CONFLICT` (`ErrRebaixamentoIndisponivel`). Caso válido: `UPDATE usuarios SET papel = $1 WHERE id = $2 AND papel = $3` (guard do papel atual fecha a corrida com promoção/rebaixamento concorrente — `RowsAffected()==0` → `409 CONFLICT` `ErrEstadoContaMudou`, reusado de `services/promocao.go`). NÃO revoga sessões: a conta segue autenticando, só com menos privilégio (o `RequireAuth` relê o papel na próxima requisição). Resposta `200` `{"usuario": <UsuarioResumo>}`.
- Efeito imediato (AC2, "encerrada na próxima requisição"): nenhuma invalidação ativa de token além da revogação de `sessoes` na desativação. `RequireAuth` já responde `401 SESSION_REVOKED` para conta com `ativo=false` a cada requisição (middleware/auth.go), e `Login` já recusa conta inativa (auth.go). Superfície testável: após desativar, `POST /api/auth/login` com a senha correta → `401`, e qualquer rota `RequireAuth` com um access token pré-existente do alvo → `401`.
- Todos os `code` de erro saem do vocabulário fixo AD-14 (`VALIDATION_ERROR`, `NOT_FOUND`, `FORBIDDEN`, `CONFLICT`, `INTERNAL_ERROR`). Handlers reusam `erroEnvelope`/`escreverErro`/`escreverJSON`/`authRequestMaxBytes` (handlers/auth.go) e o mapeamento `errors.Is(...)` → envelope, exatamente como `DecidirPromocaoHandler`. Rotas novas registradas em `newMux` (main.go) e cobertas em `main_test.go`.
- Frontend: `GestaoUsuariosSection` só é montada quando `rankPapel(usuario.papel) >= rankPapel('gestor')` (mesmo gate da seção "Decidir promoções"). Busca `GET /api/usuarios` no mount (`{"usuarios": UsuarioResumo[]}`). Por linha, exceto a do próprio ator (`useAuth().usuario.id`): botão "Desativar" ou "Reativar" conforme `ativo`; botão "Rebaixar para {rótulo}" só quando `papelAbaixo(papel)` existe. "Desativar" e "Rebaixar" abrem um `ConfirmDialog` único (nunca `window.confirm()`); "Reativar" é direto (não reduz acesso). Cada botão tem `aria-label` com o nome do alvo. Chamadas autenticadas com `Authorization: Bearer ${getAccessToken()}` (`lib/session`). Falha de carga da lista e falha de ação viram mensagem inline `role="alert"` (sem toast, molde de `ConfiguracoesPage`); toda ação bem-sucedida OU falha refaz o `GET /api/usuarios` (molde de `ConfiguracoesPage.decidir`, Story 1.7).
- Espelho mínimo em `frontend/src/lib/promocao.ts`: `papelAbaixo(papel): Papel | null` (`gestor→almoxarife`, `almoxarife→usuario`, senão `null`) — mesma duplicação Go↔TS já documentada; `rotuloPapel` é reusada.

**Block If:** nenhuma decisão desta story depende de aprovação humana nem de ação de operador fora do repositório. É schema-inalterado + código + testes.

**Never:**
- Nenhuma migration nova (a coluna `ativo` já existe; nenhum campo novo é necessário).
- Nenhum endpoint que aceite `papel`/`papel_alvo` do cliente; nenhum rebaixamento de mais de um degrau por chamada; nenhuma promoção (é a Story 1.7).
- Nenhuma exclusão/anonimização de conta (LGPD é o Epic 8); nenhuma edição de nome/e-mail/senha de terceiros.
- Nenhuma mudança em `middleware/`, no formato de sessão, em `RankPapel`, nas rotas/handlers de auth existentes, ou em `services.ListarUsuarios`/`ListarUsuariosHandler` (a AC1 já é atendida pelo recorte de escopo que eles têm desde a Story 1.5).
- Nenhuma rota nem item de navegação novos: "Gestão de Usuários" é uma seção empilhada em `ConfiguracoesPage`, como "Decidir promoções" (a `tabs` do `AppShell` continua sem consumidor — Story 1.2).
- Nenhum tratamento do caminho SSO: a troca de token SSO ainda não existe (Story 1.9); quando existir, herda a checagem de `ativo` do `RequireAuth` a cada requisição e a recusa da troca para conta inativa é item da 1.9.
- Nenhuma paginação, filtro, ordenação configurável ou histórico de ações na tela; nenhum motivo/justificativa de desativação; nenhum toast/notificação ao alvo.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Desativar almoxarife por gestor | `POST /api/usuarios/{id}/desativacao` `{"ativo":false}`, ator `gestor`, alvo `almoxarife` ativo | `200` `{"usuario":{ativo:false,...}}`; `sessoes` do alvo com `revogado_em` preenchido | — |
| Reativar conta por gestor | `{"ativo":true}`, alvo inativo | `200` `{"usuario":{ativo:true,...}}` | — |
| Desativar/rebaixar por gestor sobre gestor | ator `gestor`, alvo `gestor` | `403 FORBIDDEN`; `usuarios`/`sessoes` intactos | `ErrGestaoForaDeEscopo` |
| Desativar por gestor sobre adm | ator `gestor`, alvo `adm` | `403 FORBIDDEN` | `ErrGestaoForaDeEscopo` |
| Desativar/rebaixar por adm sobre gestor | ator `adm`, alvo `gestor` | `200`; ação aplicada | — |
| Ação sobre a própria conta | `alvoID == atorID` (qualquer papel) | `403 FORBIDDEN`; nada muda | `ErrGestaoForaDeEscopo` |
| `id` inexistente | uuid válido sem linha | `404 NOT_FOUND` | `ErrContaNaoEncontrada` |
| `id` malformado | path não-uuid | `404 NOT_FOUND` (`pq` `22P02` tratado como não encontrado) | `ErrContaNaoEncontrada` |
| Desativacao sem/má chave | corpo `{}`, `{"ativo":null}` ou não-JSON | `400 VALIDATION_ERROR` | — |
| Rebaixar gestor por adm | `POST /api/usuarios/{id}/rebaixamento`, alvo `gestor` | `200` `{"usuario":{papel:"almoxarife"}}`; próxima req do alvo numa rota `RequireRole(gestor)` → `403` | — |
| Rebaixar almoxarife por gestor | alvo `almoxarife` | `200` `{"usuario":{papel:"usuario"}}` | — |
| Rebaixar conta já `usuario` | alvo papel `usuario` | `409 CONFLICT`; nada muda | `ErrRebaixamentoIndisponivel` |
| Rebaixar, papel do alvo mudou na corrida | guard `AND papel = $3` afeta 0 linhas | `409 CONFLICT`; solicitação intacta | `ErrEstadoContaMudou` |
| Desativar então logar | após `{"ativo":false}`, `POST /api/auth/login` com a senha correta do alvo | `401` (credenciais inválidas) | — |
| Desativar então usar token antigo | após desativar, alvo chama qualquer rota `RequireAuth` com access token pré-existente | `401 SESSION_REVOKED` | — |
| Sem token / papel < gestor | sem `Authorization` / token `usuario`/`almoxarife` em qualquer das 2 rotas | `401 TOKEN_EXPIRED` / `403 FORBIDDEN` (via `RequireAuth`/`RequireRole`); handler nunca executa | — |
| UI: gestor abre "Gestão de Usuários" | `GET /api/usuarios` → só `usuario`/`almoxarife` | tabela com as linhas; nenhuma linha `gestor`/`adm` (recorte do backend) | falha de carga → `role="alert"` |
| UI: clicar "Desativar" | abre `ConfirmDialog`; confirmar → `POST .../desativacao` `{"ativo":false}` | linha atualiza após refetch; sem `window.confirm()` | falha (403/409) → `role="alert"` + refetch |
| UI: linha do próprio ator | `id === useAuth().usuario.id` | nenhum botão de ação nessa linha | — |
| UI: linha de papel `usuario` | alvo papel `usuario` | sem botão "Rebaixar" | — |

</intent-contract>

## Code Map

- `backend/services/gestao_usuarios.go` (novo) — pacote `services`. Erros exportados `ErrGestaoForaDeEscopo`, `ErrContaNaoEncontrada`, `ErrRebaixamentoIndisponivel`; reusa `ErrEstadoContaMudou` (promocao.go:37) e `const pqInvalidTextRepresentation` (promocao.go:17). Não-exportado `papelImediatamenteAbaixo(papel string) (string, bool)` — `gestor→almoxarife`, `almoxarife→usuario`, senão `("", false)` (shape idêntico a `proximoPapelPromocao`). Não-exportado `carregarAlvoParaGestao(db *sql.DB, alvoID, atorID, papelAtor string) (papelAlvo string, err error)` — `SELECT papel FROM usuarios WHERE id = $1` (trata `sql.ErrNoRows` e `pq` `22P02` como `ErrContaNaoEncontrada`); guarda `alvoID == atorID` → `ErrGestaoForaDeEscopo`; guarda `RankPapel(papelAtor) < RankPapel(PapelAdm) && (papelAlvo == PapelGestor || papelAlvo == PapelAdm)` → `ErrGestaoForaDeEscopo`. Funções: `AlterarAtivacaoUsuario(db *sql.DB, alvoID, atorID, papelAtor string, ativo bool) (UsuarioResumo, error)` — chama `carregarAlvoParaGestao`; `tx`: `UPDATE usuarios SET ativo = $1 WHERE id = $2` + (se `!ativo`) `UPDATE sessoes SET revogado_em = now() WHERE usuario_id = $2 AND revogado_em IS NULL`; relê e devolve `UsuarioResumo`. `RebaixarUsuario(db *sql.DB, alvoID, atorID, papelAtor string) (UsuarioResumo, error)` — `carregarAlvoParaGestao`; `abaixo, ok := papelImediatamenteAbaixo(papelAlvo)`, `!ok` → `ErrRebaixamentoIndisponivel`; `UPDATE usuarios SET papel = $1 WHERE id = $2 AND papel = $3` guardado (`RowsAffected()==0` → `ErrEstadoContaMudou`); relê e devolve `UsuarioResumo`.
- `backend/services/gestao_usuarios_test.go` (novo) — `testDB(t)` + `semearConta` (usuarios_test.go:11). Cobre toda a Matrix no nível de service, incl.: revogação de `sessoes` na desativação (inserir linha em `sessoes`, desativar, afirmar `revogado_em` preenchido); rebaixamento NÃO revoga sessões; corrida de papel via guard do `UPDATE`.
- `backend/services/promocao.go` — SEM mudança (referência: `ErrEstadoContaMudou`, `pqInvalidTextRepresentation`, padrão SELECT-inicial-fora-da-tx + `UPDATE` guardado).
- `backend/services/auth.go` — SEM mudança (referência: revogação de sessões em `RedefinirSenha` auth.go:644-648; `Login` já recusa `ativo=false` auth.go:334; `RenovarSessao` NÃO checa `ativo` — motiva revogar `sessoes` na desativação).
- `backend/services/usuarios.go` — SEM mudança (referência: `UsuarioResumo` usuarios.go:11-17; `ListarUsuarios` já aplica o recorte da AC1, usuarios.go:33-51).
- `backend/middleware/auth.go` / `middleware/roles.go` — SEM mudança (referência: `RequireAuth` rejeita `!usuario.Ativo` → `401 SESSION_REVOKED` e relê papel a cada requisição, auth.go:82-94; `RequireRole` decide o 403 antes do handler).
- `backend/handlers/gestao_usuarios.go` (novo) — pacote `handlers`. `DesativarUsuarioHandler(db)`, `RebaixarUsuarioHandler(db)`. Extraem `middleware.UsuarioDaSessao` (ausente → `500`, igual a `ListarUsuariosHandler`). `DesativarUsuarioHandler`: `r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)`, decodifica um struct local `ativacaoRequest` com um único campo `Ativo *bool` (tag json `ativo`); `Ativo == nil` → `400 VALIDATION_ERROR` (molde de `decisaoRequest` promocao.go:60). Ambos leem `r.PathValue("id")`. Mapeamento `errors.Is` → envelope (molde de `DecidirPromocaoHandler` promocao.go:193-214): `ErrGestaoForaDeEscopo` → `403 FORBIDDEN`; `ErrContaNaoEncontrada` → `404 NOT_FOUND`; `ErrRebaixamentoIndisponivel`/`ErrEstadoContaMudou` → `409 CONFLICT`; default → `500` + `slog.Error`. Sucesso → `200` `{"usuario": <UsuarioResumo>}`.
- `backend/handlers/gestao_usuarios_test.go` (novo) — fronteira HTTP via a MESMA composição de middleware (molde de `getUsuarios` em usuarios_test.go:55). Reusa `criarContaComPapel`/`tokenDeLogin` (usuarios_test.go, mesmo pacote). Cobre status + `code` de cada linha da Matrix, incl. os encadeamentos: desativar → `POST /api/auth/login` do alvo → `401`; desativar → `GET /api/auth/me` do alvo com token antigo → `401 SESSION_REVOKED`; rebaixar `gestor`→`almoxarife` → `GET /api/usuarios` com o token daquela conta → `403`.
- `backend/handlers/usuarios.go` — SEM mudança.
- `backend/main.go` — `newMux` (main.go:189-214): registrar `POST /api/usuarios/{id}/desativacao` e `POST /api/usuarios/{id}/rebaixamento` atrás de `RequireAuth(db, jwtSecret)(RequireRole(services.PapelGestor)(...))`. Atualizar o doc de pacote (main.go:1-13) mencionando "desativação e rebaixamento de conta — Story 1.8".
- `backend/main_test.go` — estender `TestNewMux_RegistraRotasDeAutenticacao` (main_test.go:188) com as 2 rotas sem token → `401`; novo `TestNewMux_GestaoUsuariosRotasCarregamRequireRole` (molde de `TestNewMux_PromocoesRotasCarregamRequireRole` main_test.go:478: token `usuario`/`almoxarife` → `403`; `gestor`/`adm` → não-`403`).
- `frontend/src/components/usuarios/GestaoUsuariosSection.tsx` (novo) — seção "Gestão de Usuários" (`Card` + `ul`). `useAuth()` para o `id` do ator e o gate. `useEffect` no mount → `GET /api/usuarios`. Estado `acaoPendente: { id, tipo: 'desativar' | 'rebaixar', nome, alvoRotulo? } | null` para o `ConfirmDialog` único. Ações: `POST /api/usuarios/${id}/desativacao` (`{ ativo: false | true }`) e `POST /api/usuarios/${id}/rebaixamento`. Erros de carga/ação em `<p role="alert">`; refetch pós-ação (sucesso e falha). Reusa `Button`, `Card`, `ConfirmDialog`, `getAccessToken` (`lib/session`), `rankPapel` (nav-items.ts), `rotuloPapel`/`papelAbaixo` (`lib/promocao`).
- `frontend/src/components/usuarios/GestaoUsuariosSection.test.tsx` (novo) — `vi.stubGlobal('fetch', ...)` roteando por URL, `vi.mock('@/lib/auth')` (molde de ConfiguracoesPage.test.tsx). Casos: lista renderiza linhas; sem botão de ação na linha do próprio ator; sem "Rebaixar" numa linha `usuario`; "Desativar" abre `ConfirmDialog` e confirmar chama `POST .../desativacao` `{ativo:false}` + refetch; "Rebaixar" idem; falha de carga → `role="alert"`; falha de ação (403/409) → `role="alert"` + refetch.
- `frontend/src/lib/promocao.ts` — adicionar `papelAbaixo(papel: string): Papel | null` (inverso de `proximoPapel`). Comentário curto ligando à Story 1.8.
- `frontend/src/lib/promocao.test.ts` — casos de `papelAbaixo` para os 4 papéis + valor desconhecido.
- `frontend/src/pages/ConfiguracoesPage.tsx` — `import { GestaoUsuariosSection }`; montar `<GestaoUsuariosSection />` como terceiro `Card` quando `podeDecidir` (ConfiguracoesPage.tsx:61). Atualizar o doc de módulo (linhas 9-27).
- `frontend/src/pages/ConfiguracoesPage.test.tsx` — **mudança obrigatória**: os casos de caminho `gestor` (`it('gestor: ...')` linha 170 e todo o `describe('ConfiguracoesPage — Decidir promoções')` linha 223) fazem `throw` em URL != `/api/promocoes*`; adicionar `if (url === '/api/usuarios') return jsonOk({ usuarios: [] })` a cada `stubFetch` desses casos. Novo caso: `gestor` vê o `heading` "Gestão de Usuários"; `usuario` não.
- `frontend/src/App.tsx` / `App.test.tsx` — SEM mudança (rota `/configuracoes` já existe; o teste em App.test.tsx:165 usa `papel:'usuario'`, que não monta a seção).
- `frontend/src/components/shell/nav-items.ts` — SEM mudança (o item `perfil` → `/configuracoes` já cobre o acesso).

## Tasks & Acceptance

**Execution:**
- `backend/services/gestao_usuarios.go` + `gestao_usuarios_test.go` — erros, `papelImediatamenteAbaixo`, `carregarAlvoParaGestao`, `AlterarAtivacaoUsuario`, `RebaixarUsuario`; cobrir toda a I/O Matrix no nível de service com `testDB`, incl. a revogação de `sessoes` na desativação.
- `backend/handlers/gestao_usuarios.go` + `gestao_usuarios_test.go` — 2 handlers; toda a Matrix na fronteira HTTP via a composição real de middleware, incl. os encadeamentos pós-desativação (`/api/auth/login`, `/api/auth/me`) e pós-rebaixamento (`GET /api/usuarios` 403).
- `backend/main.go` + `main_test.go` — registrar as 2 rotas em `newMux`; estender o inventário de rotas; novo teste do gate de papel via `newMux`; atualizar o doc de pacote.
- `frontend/src/lib/promocao.ts` + `promocao.test.ts` — `papelAbaixo`.
- `frontend/src/components/usuarios/GestaoUsuariosSection.tsx` + `.test.tsx` — lista + ações com `ConfirmDialog`, estados de erro inline e refetch.
- `frontend/src/pages/ConfiguracoesPage.tsx` + `.test.tsx` — montar a seção para `gestor`/`adm`; ajustar os stubs de `fetch` dos casos `gestor` e cobrir a visibilidade da seção.

**Acceptance Criteria:**
- Given um `gestor` autenticado na seção "Gestão de Usuários", when a lista carrega, then aparecem apenas contas de papel `usuario`/`almoxarife` e nenhuma `gestor`/`adm` (recorte aplicado no servidor por `services.ListarUsuarios`).
- Given um `gestor` desativando uma conta `almoxarife` confirmada via `ConfirmDialog`, when a ação conclui, then `POST /api/auth/login` com a senha correta do alvo passa a responder `401`, qualquer requisição autenticada do alvo com um access token já emitido responde `401 SESSION_REVOKED` na requisição seguinte, e as linhas ativas de `sessoes` do alvo ficam com `revogado_em` preenchido.
- Given um `gestor` tentando desativar ou rebaixar uma conta `gestor` ou `adm` (ou a própria conta), when a requisição chega, then a resposta é `403 FORBIDDEN` e nem `usuarios` nem `sessoes` mudam.
- Given um `adm` agindo sobre qualquer conta, inclusive `gestor`, when ele desativa ou rebaixa, then a ação é aplicada (`200`): a desativação revoga as sessões, o rebaixamento troca o papel para o imediatamente abaixo na hierarquia.
- Given uma conta recém-rebaixada de `gestor` para `almoxarife`, when ela faz a próxima requisição a uma rota protegida por `RequireRole(gestor)`, then recebe `403 FORBIDDEN` sem re-login (o middleware relê `papel` do Postgres a cada requisição).
- Given a seção "Gestão de Usuários", when o papel do usuário é abaixo de `gestor`, then a seção não é montada e nenhuma chamada a `GET /api/usuarios` é feita.

## Review Triage Log

### 2026-08-30 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 2: (high 0, medium 1, low 1)
- defer: 0
- reject: 22: (high 0, medium 0, low 22)
- addressed_findings:
  - `[medium]` `[patch]` `AlterarAtivacaoUsuario` tinha um TOCTOU de escopo que `RebaixarUsuario` não tem: `carregarAlvoParaGestao` faz um `SELECT papel` sem lock fora da transação e o `UPDATE usuarios SET ativo` não tinha guard de `papel` — um alvo `almoxarife` que passa no recorte do `gestor` e é promovido concorrentemente a `gestor`/`adm` ainda seria desativado (viola a AC3). Corrigido: o `UPDATE` agora é `... WHERE id = $2 AND papel = $3` com o `papelAlvo` lido no pré-check, antes do revoke de `sessoes`; `RowsAffected()==0` → `ErrEstadoContaMudou` (mesmo erro do rebaixamento). `DesativarUsuarioHandler` ganhou o `case errors.Is(..., ErrEstadoContaMudou)` → `409 CONFLICT`. Novo `TestAlterarAtivacaoUsuario_CorridaGuardaPapelAtual` (molde do teste de corrida do rebaixamento).
  - `[low]` `[patch]` A Design Note justifica revogar `sessoes` na desativação justamente porque `POST /api/auth/refresh` não passa por `RequireAuth`, mas nenhum teste provava que um refresh token pré-desativação é recusado depois. `TestDesativarUsuarioHandler_DesativaEEncerraSessoes` agora captura o cookie `refresh_token` do alvo no login (`refreshCookieDoResultado`) e afirma `postRefresh(db, cookieDoAlvo).Code == 401` após a desativação, além das asserções já existentes.

## Design Notes

- **Sem migration:** `usuarios.ativo` já existe (migration 000001) e `RequireAuth` já derruba conta inativa e relê `papel` a cada requisição (middleware/auth.go:82-94). Story 1.8 é só a superfície de escrita + a tela.
- **Rebaixamento = um degrau:** inverso exato de `proximoPapelPromocao` da Story 1.7. Alvo derivado no servidor (`papelImediatamenteAbaixo`), nunca do cliente — mesma regra `Never` da 1.7 e coerente com a ordem total do épico. `adm` nunca é alvo de rebaixamento a `adm` (índice único `idx_usuarios_unico_adm`), e `gestor`→`almoxarife` é o teto do `adm`; rebaixar duas vezes é duas chamadas.
- **Revogar `sessoes` na desativação (não no rebaixamento):** `UPDATE sessoes SET revogado_em = now() WHERE usuario_id = $1 AND revogado_em IS NULL`, na mesma transação — molde de `RedefinirSenha`. Necessário porque `POST /api/auth/refresh` NÃO passa por `RequireAuth` e `RenovarSessao` não checa `ativo`: sem revogar, um refresh cookie pré-desativação seguiria rotacionando (ainda que toda rota protegida já responda `401`). Rebaixamento não revoga: a conta segue válida, só com menos privilégio.
- **Escopo e auto-ação decididos no service, papel do ator do contexto:** `gestor` só sobre `usuario`/`almoxarife`; `adm` sobre qualquer; `alvoID == atorID` → `ErrGestaoForaDeEscopo` (evita um `adm` se trancar para fora). O service nunca reconsulta `usuarios` para descobrir o papel de quem chama (AD-8 forma 3, molde de `ListarUsuarios`/`DecidirSolicitacao`).
- **Reativação pelo mesmo endpoint (`{"ativo": true}`):** inverso do toggle; a lista já expõe `ativo`, então sem reativação uma desativação seria irreversível fora do banco. Não abre `ConfirmDialog` (não reduz acesso).
- **Seção, não rota/aba nova:** mesmo padrão de "Decidir promoções" (Story 1.7); extraída para `GestaoUsuariosSection` com teste isolado porque `ConfiguracoesPage` já está grande.

Golden — registro em `newMux`:

```go
mux.HandleFunc("POST /api/usuarios/{id}/desativacao", middleware.RequireAuth(db, jwtSecret)(
    middleware.RequireRole(services.PapelGestor)(handlers.DesativarUsuarioHandler(db))))
mux.HandleFunc("POST /api/usuarios/{id}/rebaixamento", middleware.RequireAuth(db, jwtSecret)(
    middleware.RequireRole(services.PapelGestor)(handlers.RebaixarUsuarioHandler(db))))
```

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` — build/vet limpos, `gofmt -l` sem saída.
- `cd backend && go test -p 1 -count=1 ./...` — Postgres real (`docker compose up -d db`, ou cluster descartável via `initdb`/`pg_ctl` como nas Stories 1.5–1.7, com `DATABASE_URL` apontado). Passam os novos `gestao_usuarios_test.go` (services + handlers), o inventário de rotas e o gate de papel em `main_test.go`.
- `cd frontend && npm run lint && npm run build && npm run test` — lint (`oxlint`) e build (`tsc` + `vite`) limpos; passam `GestaoUsuariosSection`, `promocao` (`papelAbaixo`) e `ConfiguracoesPage` (gestor vê "Gestão de Usuários"; usuario não).
- `docker compose up --build` — `api`/`web` sobem saudáveis; fluxo `/configuracoes` (gestor) → "Gestão de Usuários" → "Desativar" um `almoxarife` → aquela conta não loga mais através do proxy `/api`. Se `docker` indisponível, mesma nota das stories anteriores (cobertura equivalente por testes de integração contra Postgres real).

**Manual checks (if no CLI):**
- Logar como `gestor`, abrir `/configuracoes`: a seção "Gestão de Usuários" lista só `usuario`/`almoxarife`. Desativar um `almoxarife` (confirmar no `ConfirmDialog`): `SELECT ativo FROM usuarios WHERE id = ...` = `false`; `SELECT revogado_em FROM sessoes WHERE usuario_id = ...` preenchido; tentar logar com aquela conta → `401`. Reativar → login volta a funcionar.
- Logar como `adm`, rebaixar um `gestor`: `SELECT papel FROM usuarios WHERE id = ...` = `almoxarife`; com o access token antigo daquela conta, `GET /api/usuarios` → `403`.

## Auto Run Result

**Resumo:** Story 1.8 — gestão de contas: desativação e rebaixamento (FR-31). Duas rotas de escrita sobre `usuarios`, ambas atrás de `RequireAuth` + `RequireRole(gestor)`: `POST /api/usuarios/{id}/desativacao` (corpo `{"ativo": bool}` — desativa e revoga todas as sessões vivas do alvo na mesma transação, ou reativa) e `POST /api/usuarios/{id}/rebaixamento` (sem corpo — desce o papel um degrau, alvo derivado no servidor por `papelImediatamenteAbaixo`, inverso de `proximoPapelPromocao` da Story 1.7). Recorte de autoridade e guarda de auto-ação no service, a partir do papel do ator resolvido pelo contexto: `gestor` só age sobre `usuario`/`almoxarife`, `adm` sobre qualquer conta, ninguém sobre a própria. As duas operações usam `UPDATE ... WHERE id = $2 AND papel = $3` guardado (fecha a corrida com promoção/rebaixamento concorrente → `ErrEstadoContaMudou` → `409`). Nenhuma migration (a coluna `usuarios.ativo` já existe; `RequireAuth` já derruba conta inativa e relê o papel a cada requisição). Frontend: nova seção "Gestão de Usuários" (`GestaoUsuariosSection`), montada em `ConfiguracoesPage` para `gestor`/`adm`, listando `GET /api/usuarios` com "Desativar"/"Reativar"/"Rebaixar" por linha; "Desativar" e "Rebaixar" passam por `ConfirmDialog` (nunca `window.confirm()`), "Reativar" é direto.

**Arquivos alterados:**
- `backend/services/gestao_usuarios.go` (novo) — erros `ErrGestaoForaDeEscopo`/`ErrContaNaoEncontrada`/`ErrRebaixamentoIndisponivel` (reusa `ErrEstadoContaMudou`/`pqInvalidTextRepresentation` de `promocao.go`); `papelImediatamenteAbaixo`; `carregarAlvoParaGestao` (SELECT papel + guards de id/auto-ação/escopo); `AlterarAtivacaoUsuario` (UPDATE guardado + revoke de `sessoes`); `RebaixarUsuario` (UPDATE guardado, sem revoke); `relerUsuarioResumoTx`.
- `backend/services/gestao_usuarios_test.go` (novo) — cobertura da I/O Matrix no nível de service, incl. revogação de `sessoes` na desativação (e não no rebaixamento) e os dois testes de corrida de papel.
- `backend/handlers/gestao_usuarios.go` (novo) — `DesativarUsuarioHandler` (`ativacaoRequest{Ativo *bool}` sob `MaxBytesReader`; `nil` → 400) e `RebaixarUsuarioHandler`; mapeamento `errors.Is` → envelope AD-14 (403/404/409/500).
- `backend/handlers/gestao_usuarios_test.go` (novo) — I/O Matrix na fronteira HTTP via a composição real de middleware; encadeamentos pós-desativação (`/api/auth/login` → 401, `/api/auth/me` com token antigo → 401 `SESSION_REVOKED`, `POST /api/auth/refresh` com o cookie pré-desativação → 401) e pós-rebaixamento (`GET /api/usuarios` 200→403 na mesma sessão).
- `backend/main.go` — registro das 2 rotas em `newMux`; doc de pacote.
- `backend/main_test.go` — `TestNewMux_RegistraRotasDeAutenticacao` estendido (2 rotas sem token → 401); `TestNewMux_GestaoUsuariosRotasCarregamRequireRole` (gate de papel via `newMux` com tokens reais).
- `frontend/src/lib/promocao.ts` / `.test.ts` — `papelAbaixo` (inverso de `proximoPapel`).
- `frontend/src/components/usuarios/GestaoUsuariosSection.tsx` / `.test.tsx` (novos) — seção com lista, ações, `ConfirmDialog`, erros inline em `role="alert"` e refetch pós-ação.
- `frontend/src/pages/ConfiguracoesPage.tsx` / `.test.tsx` — monta `<GestaoUsuariosSection />` para `gestor`/`adm`; stubs de `fetch` dos casos `gestor` ajustados para `/api/usuarios`; novo caso de visibilidade da seção.
- `_bmad-output/implementation-artifacts/spec-1-8-gestao-de-contas-desativacao-e-rebaixamento.md` — esta spec.

**Achados de revisão:** patch 2 (aplicados), defer 0, reject 22. Ver `## Review Triage Log`.
- `[medium]` `[patch]` TOCTOU de escopo em `AlterarAtivacaoUsuario` — `UPDATE` agora guardado por `AND papel = $3` (simétrico ao rebaixamento) → `ErrEstadoContaMudou` → `409`; handler e teste de corrida adicionados.
- `[low]` `[patch]` Teste de que um refresh token pré-desativação é recusado após a desativação (`POST /api/auth/refresh` → 401), fechando o laço da Design Note sobre `/api/auth/refresh` não passar por `RequireAuth`.

**Recomendação de review de acompanhamento:** `false` — 2 achados `patch` nesta passagem (high 0, medium 1, low 1); score = 3×1 + 1×1 = 4 (< 5), sem `high`.

**Verificação executada (após os patches):**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — limpo (exit 0, sem saída de `gofmt`).
- `cd backend && go test -p 1 -count=1 ./...` — Docker indisponível (`docker: command not found`); usado cluster PostgreSQL 16 descartável via `initdb`/`pg_ctl` (TCP `127.0.0.1`, role/db `stockflow`), `DATABASE_URL` apontado. **Todos os 5 pacotes passam** (`backend`, `backend/cmd/seed-admin`, `backend/handlers`, `backend/middleware`, `backend/services`), incl. os novos `gestao_usuarios_test.go`, o gate de papel via `newMux` e o inventário de rotas. Os dois testes de corrida (`TestRebaixarUsuario_CorridaGuardaPapelAtual`, `TestAlterarAtivacaoUsuario_CorridaGuardaPapelAtual`) rodaram 5× extra sem flake. Cluster removido ao final.
- `cd frontend && npm run lint && npm run build && npm run test` — `oxlint` e build (`tsc` + `vite`) limpos; **16 arquivos / 140 testes passando**.
- `docker compose up --build` — **não executado**: Docker indisponível (idêntico às Stories 1.5–1.7). A superfície HTTP segue coberta por testes de integração contra Postgres real; o frontend por testes de componente com `fetch` mockado.
- **Matrix Test Audit:** todas as linhas da I/O & Edge-Case Matrix cobertas por ≥1 teste que rodou e passou (service + fronteira HTTP para o backend; componente para as 4 linhas de UI).

**Riscos residuais:**
- `docker compose up --build` (smoke E2E) não pôde rodar por ausência do binário `docker` — risco residual baixo; todas as camadas têm cobertura automatizada equivalente contra Postgres real.
- Os dois testes de corrida usam `time.Sleep(150ms)` + goroutine + `SELECT ... FOR UPDATE` para abrir a janela — determinísticos na prática (o lock força a ordem; verificados 5× sem flake), mas são os únicos testes sensíveis a tempo desta story.
- Caminho SSO: a troca de token SSO ainda não existe (Story 1.9). O mecanismo universal (`ativo` + releitura de papel/`ativo` pelo `RequireAuth` a cada requisição) cobre SSO por construção quando ele chegar; recusar a troca de token para conta inativa é item da 1.9.
- `useAuth().usuario.papel`/`.id` no cliente só reflete um rebaixamento/uma desativação do próprio ator após reload/re-bootstrap (sem interceptor de 401 nesta story — limitação reconhecida desde a Story 1.4). O backend sempre autoriza pelo papel/`ativo` lidos na hora.
- A árvore de trabalho retém um commit do orquestrador (`b3e9578`, `scripts/sync_kanban.py`) feito em paralelo a esta invocação — fora do escopo desta story, intencionalmente não tocado.
