---
title: 'Story 1.7 — Solicitação de promoção de papel'
type: 'feature'
created: '2026-08-29'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: '8a53c0fcd060d71a6479ce1359000dd1400a28b2'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Não existe caminho de promoção de papel dentro do produto: o autocadastro sempre cria `usuario` (Story 1.3) e a hierarquia é aplicada no servidor (Story 1.5), mas quem precisa de mais acesso depende de alguém alterar `usuarios.papel` direto no banco. A tabela `solicitacoes_promocao` é citada no ERD da arquitetura (AD-8, FR-33) e nunca foi criada. A superfície "Meu Perfil" (`/configuracoes`) hoje é a `PlaceholderPage`.

**Approach:** Nova tabela `solicitacoes_promocao` (migration 000004) com máquina de estado `pendente → aprovada|rejeitada` e índice único parcial garantindo no máximo uma solicitação `pendente` por conta. Quatro endpoints REST em `handlers/promocao.go` + regra em `services/promocao.go`: `POST /api/promocoes` (a própria conta solicita promoção para o papel imediatamente acima), `GET /api/promocoes/minha` (estado da própria solicitação, para o botão), `GET /api/promocoes` (fila de pendentes, mínimo `gestor`, com recorte de escopo) e `POST /api/promocoes/{id}/decisao` (aprovar/rejeitar, mínimo `gestor`, com `adm` obrigatório para alvo `gestor`). Aprovação troca `usuarios.papel` na mesma transação — efeito imediato porque o middleware relê o papel do Postgres a cada requisição (AD-6). Frontend: `ConfiguracoesPage` real substitui a `PlaceholderPage` na rota `/configuracoes` (dentro do `AppShell`/`RotaProtegida`), com a seção "Meu Perfil" (identidade + botão "Solicitar promoção") e, para `gestor`/`adm`, a seção "Decidir promoções".

## Boundaries & Constraints

**Always:**
- **Papel imediatamente acima (ordem total AD-8, `services.RankPapel`):** `usuario → almoxarife`, `almoxarife → gestor`. `gestor` e `adm` NÃO têm promoção disponível (promoção a `adm` nunca existe — `idx_usuarios_unico_adm` da migration 000001 permite uma só conta `adm`). O alvo é sempre derivado no servidor a partir do papel atual do solicitante — nunca vem do corpo da requisição.
- `POST /api/promocoes` (só `RequireAuth`, qualquer conta autenticada; corpo ignorado): resolve o papel-alvo a partir de `usuario.Papel` do contexto. Papel sem promoção disponível (`gestor`/`adm`) → `403 FORBIDDEN`. Já existe uma solicitação `pendente` para essa conta → `409 CONFLICT` (uma pendente por vez — AC4). Caso contrário, insere uma linha `solicitacoes_promocao` (`solicitante_id`, `papel_alvo`, `status='pendente'`) e responde `201` com `{"solicitacao":{"id","papel_alvo","status","criado_em"}}`. Uma solicitação anterior `rejeitada`/`aprovada` NÃO bloqueia a nova (AC5 — sem período de espera): o gate olha apenas `status='pendente'`.
- `GET /api/promocoes/minha` (só `RequireAuth`): devolve `{"solicitacao": <obj>|null}` — a solicitação MAIS RECENTE (`ORDER BY criado_em DESC LIMIT 1`) da própria conta, com `{"id","papel_alvo","status","criado_em","decidido_em"}` (`decidido_em` nulo enquanto `pendente`); `null` se a conta nunca solicitou. Nenhuma escrita.
- `GET /api/promocoes` (`RequireAuth` + `RequireRole(services.PapelGestor)`): lista só solicitações `status='pendente'`, ordenadas por `criado_em, id`. Recorte de escopo (mesmo padrão de `services.ListarUsuarios`, papel resolvido pelo contexto, nunca reconsultado): `adm` vê todas as pendentes; `gestor` vê só as de `papel_alvo='almoxarife'` (não pode decidir promoção a `gestor`). Cada item: `{"id","solicitante_nome","solicitante_email","papel_atual","papel_alvo","criado_em"}`. Lista vazia não é erro.
- `POST /api/promocoes/{id}/decisao` (`RequireAuth` + `RequireRole(services.PapelGestor)`; corpo `{"aprovar": bool}`): `id` inexistente OU não-UUID (`pq` code `22P02`) → `404 NOT_FOUND`. Solicitação não-`pendente` → `409 CONFLICT` (já decidida — inclui reuso e corrida entre dois decisores). `papel_alvo='gestor'` e papel do decisor ≠ `adm` → `403 FORBIDDEN` (AC3), sem tocar em nada. Decisão válida, numa única transação:
  - `aprovar=true`: `UPDATE usuarios SET papel = <papel_alvo> WHERE id = <solicitante_id> AND papel = <papel_imediatamente_abaixo_do_alvo>` — `RowsAffected()==0` → `409 CONFLICT` (o papel do solicitante mudou desde a solicitação); depois `UPDATE solicitacoes_promocao SET status='aprovada', decidido_por=<decisor_id>, decidido_em=now() WHERE id=$1 AND status='pendente'` — `RowsAffected()==0` → `409 CONFLICT` (fecha a corrida SELECT→UPDATE, molde de `VerificarEmail`).
  - `aprovar=false`: `UPDATE solicitacoes_promocao SET status='rejeitada', decidido_por=<decisor_id>, decidido_em=now() WHERE id=$1 AND status='pendente'` — `RowsAffected()==0` → `409 CONFLICT`. Nenhuma mudança em `usuarios`.
  - Sucesso → `200` com `{"solicitacao":{"id","status","papel_alvo","decidido_em"}}`.
- Toda decisão registra `decidido_por` (uuid do decisor) + `decidido_em` (`now()`) — trilha de auditoria da solicitação (FR-33). `decidido_por` é FK para `usuarios(id)` `ON DELETE SET NULL` (preserva a linha de auditoria; nenhum fluxo do app apaga contas hoje).
- Efeito imediato da aprovação (AC2): não há invalidação de sessão nem recálculo no cliente — o `middleware.RequireAuth` já relê `papel` do Postgres via `services.BuscarUsuarioSessao` a cada requisição (AD-6). Superfície observável/testável: logo após a aprovação, `GET /api/auth/me` do solicitante devolve o novo `papel`, e uma rota protegida por `RequireRole` no novo nível passa a responder `200` para ele (antes `403`).
- Todos os `code` de erro saem do vocabulário fixo AD-14 (`VALIDATION_ERROR`, `NOT_FOUND`, `FORBIDDEN`, `CONFLICT`, `INTERNAL_ERROR`). Handlers usam `escreverErro`/`escreverJSON`, `http.MaxBytesReader` com `authRequestMaxBytes` (rotas com corpo) e o mapeamento `errors.Is(...)` → envelope, exatamente como os handlers de auth existentes. Novas rotas registradas em `newMux` (`main.go`) e cobertas em `main_test.go`.
- Frontend: a rota `/configuracoes` deixa de cair na `PlaceholderPage` e passa a renderizar `ConfiguracoesPage` — registrada como rota-filha de `/` (dentro de `RotaProtegida`/`AppShell`), ANTES do `{ path: '*' }`. Identidade (`nome`/`email`/`papel`) vem de `useAuth()`. Botão "Solicitar promoção para <Papel do alvo>" só aparece para `usuario`/`almoxarife`; fica `disabled` enquanto `GET /api/promocoes/minha` indicar `status='pendente'` (texto "Solicitação pendente de aprovação."); após uma `rejeitada`, o botão volta a habilitar com a nota "Sua última solicitação foi recusada.". `gestor`/`adm` veem "Não há promoção disponível para o seu papel." e nenhum botão. A seção "Decidir promoções" (lista de `GET /api/promocoes` com botões "Aprovar"/"Recusar" por item, chamando `POST /api/promocoes/{id}/decisao`) só é montada quando `rankPapel(papel) >= rankPapel('gestor')`. Chamadas autenticadas usam `Authorization: Bearer ${getAccessToken()}` (`lib/session`), padrão do bootstrap em `lib/auth.tsx` — não há interceptor de 401 nesta story; falha de rede/HTTP vira mensagem inline (`role="alert"`), sem toast, consistente com `CadastroPage`/`RedefinirSenhaPage`.
- Espelho mínimo em `frontend/src/lib/promocao.ts` (`proximoPapel(papel): Papel | null`, `rotuloPapel(papel): string`) — mesma duplicação documentada de `rankPapel` (nav-items.ts) / `senha.ts`; a autoridade é sempre o backend.

**Block If:** nenhuma decisão desta story depende de aprovação humana nem de ação de operador fora do repositório. Todo o trabalho é schema + código + testes.

**Never:**
- Nenhuma promoção a `adm`, nenhum caminho de auto-promoção sem decisão de `gestor`/`adm` (PRD §4.1 FR-33), nenhum endpoint que aceite `papel_alvo` do cliente.
- Nenhum rebaixamento, desativação ou edição de conta por terceiros — é a Story 1.8. Esta story só PROMOVE, e só via aprovação de solicitação.
- Nenhuma notificação por e-mail/outbox e nenhum toast de "sua promoção foi decidida" na próxima abertura do app (EXPERIENCE.md, "Fluxos de apoio") — fica para uma story posterior; aqui o estado é consultado sob demanda por `GET /api/promocoes/minha`.
- Nenhuma aba horizontal real no `AppShell` (`tabs` prop continua sem consumidor — Story 1.2): "Meu Perfil" e "Decidir promoções" são seções empilhadas na mesma página, simplificação deliberada do "aba dentro de Configurações" do EXPERIENCE.md.
- Nenhum "trocar senha", "método de login (senha/SSO)", "baixar meus dados" ou "solicitar exclusão de conta" em Meu Perfil — SSO é Story 1.9, LGPD é Epic 8. `ConfiguracoesPage` nesta story = identidade + promoção apenas.
- Nenhum histórico de solicitações na UI (a de decisão lista só pendentes; a de Meu Perfil mostra só a mais recente). Nenhuma paginação. Nenhum campo de justificativa (o PRD marca "justificativa opcional" como `[ASSUMPTION]` — fora do escopo mínimo).
- Nenhuma mudança em `middleware/`, no formato de sessão, em `RankPapel`, ou nas rotas/handlers de auth existentes.
- Nenhum `ON DELETE CASCADE` em `decidido_por` (perderia a trilha); `solicitante_id` sim é `ON DELETE CASCADE` (mesma convenção de `tokens_acao`/`sessoes`).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Solicitar, `usuario` sem pendente | `POST /api/promocoes`, papel `usuario` | `201` `{"solicitacao":{papel_alvo:"almoxarife",status:"pendente",...}}`; 1 linha `solicitacoes_promocao` | — |
| Solicitar, `almoxarife` sem pendente | idem, papel `almoxarife` | `201`, `papel_alvo:"gestor"` | — |
| Solicitar, `gestor` ou `adm` | idem | `403 FORBIDDEN`; nenhuma linha gravada | `ErrPromocaoIndisponivel` |
| Solicitar, já há pendente | segunda chamada com uma `pendente` viva | `409 CONFLICT`; nenhuma linha nova | `ErrSolicitacaoPendenteExiste` |
| Solicitar após rejeição | única solicitação anterior tem `status='rejeitada'` | `201`; nova linha `pendente` (sem espera) | — |
| Solicitar sem token | `POST /api/promocoes` sem `Authorization` | `401 TOKEN_EXPIRED` (via `RequireAuth`) | — |
| Minha solicitação, existe | `GET /api/promocoes/minha`, conta com histórico | `200` `{"solicitacao":{status,decidido_em,...}}` (a mais recente) | — |
| Minha solicitação, nunca pediu | idem, conta sem linhas | `200` `{"solicitacao":null}` | — |
| Fila, decisor `gestor` | `GET /api/promocoes` | `200` só pendentes de `papel_alvo='almoxarife'`, ordenadas por `criado_em,id` | — |
| Fila, decisor `adm` | idem | `200` todas as pendentes (`almoxarife` e `gestor`) | — |
| Fila, papel abaixo de `gestor` | `usuario`/`almoxarife` chama `GET /api/promocoes` | `403 FORBIDDEN` (via `RequireRole`); handler nunca executa | — |
| Aprovar promoção a `almoxarife` por `gestor` | `POST /api/promocoes/{id}/decisao` `{"aprovar":true}` | `200`; `usuarios.papel` do solicitante = `almoxarife`; solicitação `aprovada` com `decidido_por`/`decidido_em`; `GET /api/auth/me` do solicitante reflete o novo papel | — |
| Aprovar promoção a `gestor` por `adm` | idem, alvo `gestor`, decisor `adm` | `200`; papel do solicitante = `gestor` | — |
| Decidir promoção a `gestor` por não-`adm` | alvo `gestor`, decisor `gestor` | `403 FORBIDDEN`; nada muda | `ErrDecisaoNaoAutorizada` |
| Rejeitar | `{"aprovar":false}` sobre uma `pendente` | `200`; solicitação `rejeitada` + auditoria; `usuarios` intacto | — |
| Decidir solicitação inexistente | `id` aleatório (uuid válido) | `404 NOT_FOUND` | `ErrSolicitacaoNaoEncontrada` |
| Decidir com `id` malformado | `id` não-uuid no path | `404 NOT_FOUND` (`pq` `22P02` tratado como não encontrado) | `ErrSolicitacaoNaoEncontrada` |
| Decidir solicitação já decidida | segunda decisão sobre a mesma linha | `409 CONFLICT` (guard `status='pendente'` afeta 0 linhas) | `ErrSolicitacaoNaoPendente` |
| Decidir, papel do solicitante mudou | alvo `almoxarife` mas solicitante já é `gestor` | `409 CONFLICT`; solicitação continua `pendente` | `ErrEstadoContaMudou` |
| Decidir, corpo malformado | corpo não-JSON | `400 VALIDATION_ERROR` | — |
| Decidir sem token / papel insuficiente | sem `Authorization` / papel `usuario` | `401 TOKEN_EXPIRED` / `403 FORBIDDEN` | — |
| UI Meu Perfil, `usuario` com pendente | `GET /api/promocoes/minha` → `pendente` | botão "Solicitar promoção" visível e `disabled`; texto "Solicitação pendente de aprovação." | — |
| UI Meu Perfil, `gestor` | papel do `useAuth` = `gestor` | sem botão de solicitar; texto "Não há promoção disponível para o seu papel."; seção "Decidir promoções" montada | — |
| UI decisão, aprovar item | clique em "Aprovar" → `POST .../decisao` `200` | item some da lista (refetch); mensagem inline "Promoção aprovada." | falha → `role="alert"` "Não foi possível concluir a decisão." |

</intent-contract>

## Code Map

- `backend/migrations/000004_create_solicitacoes_promocao.up.sql` (novo) — `CREATE TABLE solicitacoes_promocao (id UUID PK DEFAULT gen_random_uuid(), solicitante_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE, papel_alvo VARCHAR(20) NOT NULL CHECK (papel_alvo IN ('almoxarife','gestor')), status VARCHAR(20) NOT NULL DEFAULT 'pendente' CHECK (status IN ('pendente','aprovada','rejeitada')), criado_em TIMESTAMPTZ NOT NULL DEFAULT now(), decidido_por UUID REFERENCES usuarios(id) ON DELETE SET NULL, decidido_em TIMESTAMPTZ)`. Índices: `CREATE UNIQUE INDEX idx_solicitacoes_promocao_pendente_unica ON solicitacoes_promocao (solicitante_id) WHERE status = 'pendente';` (backstop de corrida para "uma pendente por vez"), `CREATE INDEX idx_solicitacoes_promocao_solicitante ON solicitacoes_promocao (solicitante_id);`, `CREATE INDEX idx_solicitacoes_promocao_decidido_por ON solicitacoes_promocao (decidido_por);` (FK alvo de `ON DELETE SET NULL` — mesma justificativa dos comentários de índice das migrations 000002/000003), `CREATE INDEX idx_solicitacoes_promocao_status ON solicitacoes_promocao (status, criado_em);` (consulta da fila). Comentário `-- Story 1.7 ...` no topo, no estilo das migrations anteriores.
- `backend/migrations/000004_create_solicitacoes_promocao.down.sql` (novo) — `DROP TABLE IF EXISTS solicitacoes_promocao;` (molde do 000003.down).
- `backend/services/promocao.go` (novo) — pacote `services`. Tipos `SolicitacaoPromocao{ID, PapelAlvo, Status string; CriadoEm time.Time; DecididoEm *time.Time}` e `SolicitacaoPendente{ID, SolicitanteNome, SolicitanteEmail, PapelAtual, PapelAlvo string; CriadoEm time.Time}`. Erros exportados `var ErrPromocaoIndisponivel, ErrSolicitacaoPendenteExiste, ErrSolicitacaoNaoEncontrada, ErrSolicitacaoNaoPendente, ErrDecisaoNaoAutorizada, ErrEstadoContaMudou`. Não-exportado `proximoPapelPromocao(papel string) (string, bool)` — `usuario→almoxarife`, `almoxarife→gestor`, senão `("", false)`; e o inverso `papelAbaixoDe(alvo string) string` para o guard do `UPDATE usuarios`. Funções: `SolicitarPromocao(db *sql.DB, solicitanteID, papelAtual string) (SolicitacaoPromocao, error)` (checa `proximoPapelPromocao`; `SELECT` por pendente existente; `INSERT ... RETURNING`; trata `pq` `23505` do índice parcial como `ErrSolicitacaoPendenteExiste`); `BuscarMinhaSolicitacao(db *sql.DB, solicitanteID string) (*SolicitacaoPromocao, error)` (`SELECT ... ORDER BY criado_em DESC LIMIT 1`; `sql.ErrNoRows` → `nil, nil`); `ListarSolicitacoesPendentes(db *sql.DB, papelDecisor string) ([]SolicitacaoPendente, error)` (`JOIN usuarios` no `solicitante_id`; `WHERE status='pendente'` + (`papelDecisor != PapelAdm` → `AND papel_alvo = 'almoxarife'`); `ORDER BY s.criado_em, s.id`; molde de `ListarUsuarios`); `DecidirSolicitacao(db *sql.DB, solicitacaoID, decisorID, papelDecisor string, aprovar bool) (SolicitacaoPromocao, error)` (SELECT inicial `solicitante_id, papel_alvo, status`, tratando `sql.ErrNoRows` e `pq` `22P02` como `ErrSolicitacaoNaoEncontrada`; guards de `status`/autorização; transação com os `UPDATE`s guardados descritos em Boundaries; molde de janela de corrida de `VerificarEmail`/`RenovarSessao`). Reusa `pqUniqueViolation` (auth.go:28); novo `const pqInvalidTextRepresentation = "22P02"`.
- `backend/services/auth.go` — SEM mudança (referência: `pqUniqueViolation`, padrão de transação/guard, `RankPapel` em `papel.go`).
- `backend/handlers/promocao.go` (novo) — pacote `handlers`. `SolicitarPromocaoHandler(db)`, `MinhaSolicitacaoHandler(db)`, `ListarPromocoesHandler(db)`, `DecidirPromocaoHandler(db)`. Extraem `middleware.UsuarioDaSessao(r.Context())` (guard ausente → `500`, igual a `ListarUsuariosHandler`/`MeHandler`). `DecidirPromocaoHandler` lê `r.PathValue("id")` e decodifica `{"aprovar": bool}` sob `http.MaxBytesReader(w, r.Body, authRequestMaxBytes)`. Mapeamento `errors.Is` → envelope: `ErrPromocaoIndisponivel`/`ErrDecisaoNaoAutorizada` → `403 FORBIDDEN`; `ErrSolicitacaoPendenteExiste`/`ErrSolicitacaoNaoPendente`/`ErrEstadoContaMudou` → `409 CONFLICT`; `ErrSolicitacaoNaoEncontrada` → `404 NOT_FOUND`; JSON inválido → `400 VALIDATION_ERROR`; default → `500` + `slog.Error`. Reusa `erroEnvelope`/`escreverErro`/`escreverJSON`/`authRequestMaxBytes` (handlers/auth.go). Structs de resposta locais com tags JSON em snake_case português (molde de `services.UsuarioResumo`).
- `backend/handlers/usuarios.go` — SEM mudança (molde de `ListarUsuariosHandler`: guard de contexto, papel do contexto para escopo).
- `backend/main.go` — `newMux` (linha 186): registrar `POST /api/promocoes` e `GET /api/promocoes/minha` atrás de `RequireAuth`; `GET /api/promocoes` e `POST /api/promocoes/{id}/decisao` atrás de `RequireAuth(...)(RequireRole(services.PapelGestor)(...))`. Atualizar o doc de pacote (linhas 1-9) mencionando "solicitação de promoção de papel — Story 1.7".
- `backend/main_test.go` — estender `TestNewMux_RegistraRotasDeAutenticacao` (linha 188) com as 4 rotas: ex. `POST /api/promocoes` sem token → `401`; `GET /api/promocoes/minha` sem token → `401`; `GET /api/promocoes` sem token → `401`; `POST /api/promocoes/{id}/decisao` com `id` qualquer sem token → `401` (todas antes de tocar no banco, resolvidas por `RequireAuth`). Helper existente `testDB(t)`.
- `backend/services/promocao_test.go` (novo) — `testDB(t)` (Postgres real, auth_test.go:34). Seeders locais para inserir contas com papel (`INSERT INTO usuarios ...`, molde de `criarUsuarioParaLogin`/`criarContaComPapel`) e solicitações em cada `status`. Cobrir toda a I/O Matrix no nível de service: alvo derivado por papel; `gestor`/`adm` → `ErrPromocaoIndisponivel`; pendente única (incl. corrida via índice `23505`); rejeitada não bloqueia; `BuscarMinhaSolicitacao` (mais recente / nil); recorte `ListarSolicitacoesPendentes` (`gestor` vs `adm`); `DecidirSolicitacao` aprovar (papel trocado + auditoria), rejeitar, `gestor` decidindo alvo `gestor` → `ErrDecisaoNaoAutorizada`, inexistente, `id` malformado, já decidida, papel do solicitante divergente → `ErrEstadoContaMudou`.
- `backend/handlers/promocao_test.go` (novo) — fronteira HTTP via a MESMA composição de `newMux` (molde de `getUsuarios` em usuarios_test.go: `middleware.RequireAuth(db, testJWTSecret)(middleware.RequireRole(...)(Handler(db)))`). Helpers `criarContaComPapel`/`tokenDeLogin` já existem em usuarios_test.go (mesmo pacote `handlers`). Cobrir os status HTTP e `code` de cada linha da Matrix, incluindo o encadeamento aprovar → `GET /api/auth/me` do solicitante devolve o novo papel, e aprovar → uma rota `RequireRole` no novo nível passa de `403` para `200`.
- `frontend/src/pages/ConfiguracoesPage.tsx` (novo) — página "Meu Perfil". `useAuth()` para identidade; `useEffect` no mount busca `GET /api/promocoes/minha` (e `GET /api/promocoes` se `rankPapel(usuario.papel) >= rankPapel('gestor')`). Layout com `Card` (molde de `CadastroPage`): seção identidade (`nome`/`email`/`papel`), seção "Solicitar promoção" (`Button` "Solicitar promoção para {rotuloPapel(proximoPapel(papel))}", `disabled` enquanto `enviando` ou solicitação `pendente`; estados de texto conforme Boundaries), seção "Decidir promoções" condicional (lista simples com `Button` "Aprovar"/"Recusar" por item). Mensagens inline via `<p role="alert">` / `<output>` (sem toast). Guarda de duplo-submit checando o flag `enviando` diretamente (molde de `CadastroPage`).
- `frontend/src/pages/ConfiguracoesPage.test.tsx` (novo) — `vi.stubGlobal('fetch', ...)` / `vi.spyOn(global, 'fetch')` roteando por URL; `useAuth` mockado como em App.test.tsx (`vi.mock('@/lib/auth')`). Casos: `usuario` sem pendente vê o botão habilitado e o alvo correto; com pendente vê `disabled` + texto; após POST `201` refaz o fetch e desabilita; `gestor` vê "Decidir promoções" com itens e some um item após aprovar; erro HTTP → `role="alert"`.
- `frontend/src/lib/promocao.ts` (novo) — `proximoPapel(papel: string): Papel | null` e `rotuloPapel(papel: string): string` (`usuario`→"Usuário", `almoxarife`→"Almoxarife", `gestor`→"Gestor", `adm`→"Adm"). Reusa `Papel`/`rankPapel` de `components/shell/nav-items.ts`.
- `frontend/src/lib/promocao.test.ts` (novo) — tabela: `proximoPapel` para os 4 papéis + valor desconhecido; `rotuloPapel`.
- `frontend/src/App.tsx` — adicionar `{ path: 'configuracoes', element: <ConfiguracoesPage /> }` como filho da rota `/` ANTES de `{ path: '*', element: <PlaceholderPage /> }`; `import { ConfiguracoesPage }`; atualizar o comentário de doc (linhas 11-25) que enumera as rotas.
- `frontend/src/App.test.tsx` — novo caso: com `useAuth` mockado `autenticado` e `fetch` mockado, navegar a `/configuracoes` renderiza `ConfiguracoesPage` (ex.: heading "Meu Perfil") dentro do shell, não a `PlaceholderPage`.
- `frontend/src/components/shell/nav-items.ts` — SEM mudança (o item `perfil` já aponta `to: '/configuracoes'`, `papelMinimo: 'usuario'`).

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000004_create_solicitacoes_promocao.up.sql` + `.down.sql` — criar a tabela + índices descritos no Code Map. As migrations rodam no startup (`runMigrations`, main.go) e no início de `testDB`.
- `backend/services/promocao.go` + `promocao_test.go` — tipos, erros, `proximoPapelPromocao`/`papelAbaixoDe`, `SolicitarPromocao`, `BuscarMinhaSolicitacao`, `ListarSolicitacoesPendentes`, `DecidirSolicitacao`; cobrir toda a I/O Matrix no nível de service com `testDB`.
- `backend/handlers/promocao.go` + `promocao_test.go` — 4 handlers; toda a Matrix na fronteira HTTP via a composição real de middleware, incl. os encadeamentos pós-aprovação (`/api/auth/me` e uma rota `RequireRole`).
- `backend/main.go` + `main_test.go` — registrar as 4 rotas em `newMux`; estender o teste de inventário de rotas; atualizar o doc de pacote.
- `frontend/src/lib/promocao.ts` + `promocao.test.ts` — helpers de papel-alvo/rótulo espelhando o backend.
- `frontend/src/pages/ConfiguracoesPage.tsx` + `.test.tsx` — página Meu Perfil com solicitação e (para `gestor`/`adm`) decisão; estados de botão conforme `GET /api/promocoes/minha`.
- `frontend/src/App.tsx` + `App.test.tsx` — rota `/configuracoes` renderizando `ConfiguracoesPage` dentro de `RotaProtegida`; atualizar o comentário de rotas.

**Acceptance Criteria:**
- Given uma conta `usuario` (ou `almoxarife`) sem solicitação pendente, when ela aciona "Solicitar promoção" em Meu Perfil, then é criada uma linha `solicitacoes_promocao` com `papel_alvo` igual ao papel imediatamente acima na hierarquia (`almoxarife`, resp. `gestor`) e `status='pendente'`, e o botão passa a aparecer desabilitado enquanto essa solicitação seguir pendente.
- Given uma solicitação `pendente` de promoção a `almoxarife`, when um `gestor` ou um `adm` a aprova, then `usuarios.papel` do solicitante muda para `almoxarife` na mesma transação da decisão (sem esperar a sessão expirar — a próxima requisição autenticada do solicitante já resolve com o novo papel) e a solicitação passa a `aprovada` com `decidido_por` e `decidido_em` preenchidos.
- Given uma solicitação `pendente` de promoção a `gestor`, when qualquer papel diferente de `adm` (inclusive `gestor`) tenta decidi-la, then a resposta é `403 FORBIDDEN` e nem a solicitação nem `usuarios` são alterados.
- Given uma conta que já tem uma solicitação `pendente`, when a mesma conta tenta criar outra, then a resposta é `409 CONFLICT` e nenhuma linha nova é gravada (no máximo uma pendente por vez, reforçado pelo índice único parcial).
- Given uma conta cuja única solicitação anterior está `rejeitada`, when ela solicita promoção de novo, then a nova solicitação `pendente` é criada normalmente, sem período de espera.
- Given uma conta autenticada com papel abaixo de `gestor`, when ela chama `GET /api/promocoes` ou `POST /api/promocoes/{id}/decisao`, then recebe `403 FORBIDDEN` de `RequireRole` e o handler de promoção nunca executa.

## Spec Change Log

Nenhuma alteração de spec: nenhuma passagem de review disparou `bad_spec` nem `intent_gap`.

## Review Triage Log

### 2026-08-29 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 8: (high 0, medium 2, low 6)
- defer: 0
- reject: 16
- addressed_findings:
  - `[medium]` `[patch]` `DecidirPromocaoHandler` decodificava `{"aprovar": bool}` num struct de valor: corpo `{}`, `{"aprovar":null}` ou chave com typo passavam sem erro e viravam `aprovar=false` — uma REJEIÇÃO silenciosa e auditada de uma promoção pendente em vez de `400`. Trocado para `*bool`; `nil` (campo ausente/não-booleano) → `400 VALIDATION_ERROR`. Novo caso de teste no `handlers/promocao_test.go` (`{}` → 400) e no `main_test.go`.
  - `[low]` `[patch]` `ErrEstadoContaMudou` era mapeado para `409 CONFLICT` com a mensagem "esta solicitação não está mais pendente" — factualmente errada (a solicitação SEGUE pendente; foi o papel do solicitante que mudou). Ganhou mensagem própria ("o estado da conta do solicitante mudou; recarregue a fila").
  - `[low]` `[patch]` `ConfiguracoesPage.decidir()` não recarregava a fila quando a decisão falhava (404/409): um item já decidido por outro `gestor` ficava na lista e era re-tentado indefinidamente. Agora o ramo de erro também chama `carregarPendentes()`, derrubando o item obsoleto.
  - `[low]` `[patch]` `ConfiguracoesPage.carregarPendentes()`/`carregarMinha()` engoliam falha de rede/HTTP em silêncio — um `GET /api/promocoes` que falhava deixava o `gestor` vendo "Nenhuma solicitação pendente." (falso "nada a fazer", escondendo promoções reais). Adicionado estado de erro inline (`role="alert"`) quando a carga da fila falha.
  - `[low]` `[patch]` Botões "Aprovar"/"Recusar" da fila não tinham nome acessível por linha (N botões idênticos sem contexto). Adicionado `aria-label` com o nome do solicitante em cada um.
  - `[medium]` `[patch]` O gate `RequireRole(gestor)` das duas rotas novas (`GET /api/promocoes`, `POST /api/promocoes/{id}/decisao`) só era verificado via `newMux` no caso "sem token" (401, produzido só por `RequireAuth`) — remover `RequireRole` de `newMux` deixaria a suíte verde (regressão de escalonamento de privilégio + vazamento de nomes/e-mails da fila). Adicionado, no molde de `TestNewMux_UsuariosRotaCarregaRequireRole` (Story 1.5), despacho das duas rotas pelo `newMux` real com token `usuario`/`almoxarife` (espera 403) e `gestor`/`adm` (espera não-403).
  - `[low]` `[patch]` Migration `000004` não tinha `CHECK` acoplando `status` a `decidido_em` (uma linha `aprovada`/`rejeitada` podia existir com `decidido_em` nulo, contrariando a trilha de auditoria que o próprio comentário da migration afirma). Adicionado `CHECK ((status = 'pendente') = (decidido_em IS NULL))` (não acopla `decidido_por`, que é `ON DELETE SET NULL`).
  - `[low]` `[patch]` A migration `000004` não tinha teste de schema (`TestRunMigrations_*`) como as migrations 000001/000002. Adicionado teste afirmando os `CHECK` de `papel_alvo`/`status`, o novo `CHECK` de auditoria e os quatro índices de `solicitacoes_promocao`.
- Achados roteados para `reject` (16): mensagens de erro genéricas no frontend em vez de específicas por código (`CadastroPage`/`RedefinirSenhaPage` fazem idêntico — padrão do repo apontado pela spec); ausência de link "Meu Perfil" na navegação (**falso** — `nav-items.ts` já tem o item `perfil` apontando para `/configuracoes` desde a Story 1.2, e a spec registra "SEM mudança"); ausência de interceptor de 401 / auth-fetch compartilhado (explicitamente fora de escopo — `auth.tsx` documenta "não há interceptor de 401 nesta story"; comportamento de todo o app desde a Story 1.4); `criado_em` carregado mas não exibido na fila (campo previsto na forma de resposta da spec; exibir é polimento fora das ACs); ausência de `LIMIT`/paginação em `ListarSolicitacoesPendentes` (a seção `Never` da spec exclui paginação; `ListarUsuarios` da Story 1.5 é idêntico); `ListarSolicitacoesPendentes` fixa `PapelAlmoxarife` no ramo não-adm sem revalidar o papel (espelho deliberado de `ListarUsuarios`, apontado pela spec; a rota sempre compõe `RequireRole`); `DecidirSolicitacao` faz o `SELECT` inicial fora da transação (o `UPDATE` guardado `WHERE status='pendente'` + `RETURNING` já fecha a corrida, com teste de concorrência dedicado; molde exato de `VerificarEmail`); `SolicitarPromocaoHandler` não embrulha o corpo em `http.MaxBytesReader` (o handler não lê o corpo — `MeHandler`/`ListarUsuariosHandler` também não embrulham); `rotuloPapel('adm')` = "Adm" (vocabulário do glossário do PRD); `TestSolicitarPromocaoHandler_SemToken401` espera `TOKEN_EXPIRED` para requisição sem token (comportamento pré-existente de `RequireAuth`, sancionado desde a Story 1.4, idêntico a `TestListarUsuariosHandler_SemToken401`); ausência de teste de "solicitar após APROVAÇÃO" (o gate é `status='pendente'`; o caso `rejeitada` já prova que linha não-pendente não bloqueia, e a derivação de alvo por papel já é testada isoladamente — composição cobre); ausência de teste de contrato cross-linguagem Go↔TS para a regra de promoção (duplicação aceita e documentada nas Stories 1.5/1.6 para `rankPapel`/`senha.ts`; backend é a autoridade); confirmação `solicitar()` sem live-region (o botão desabilitado + texto "Solicitação pendente" já é mudança visível de estado); cleanup de `setState` no unmount de `ConfiguracoesPage` (React 18 não avisa mais; inócuo aqui); `minha.status === 'aprovada'` cair no botão habilitado com papel stale (o backend deriva o alvo do papel LIDO na hora — ação segura; staleness do papel no cliente é reconhecida nas Design Notes); `dec.More()` para conteúdo JSON após o primeiro objeto (`json.Decoder` para no primeiro valor — comportamento padrão aceito em todos os handlers do repo).
  - Nota do auditor de alinhamento de intenção (descritiva, sem ação): a verificação das AC1/AC2 no fluxo de UI é fatiada em duas fronteiras dubladas (`fetch` mockado no frontend, `httptest` no backend) — não há teste E2E de um clique renderizado até uma linha real; isso é a estratégia de teste de todo o repositório (o smoke E2E é `docker compose up --build`, na seção Verification). O rótulo do botão inclui o papel-alvo ("Solicitar promoção para Almoxarife") — mais informativo que o "Solicitar promoção" literal da AC, e ainda o contém; a spec especifica esse rótulo. "Quem decidiu" é gravado e testado no nível do banco; a AC diz "registra", não "expõe na API/UI"; histórico na UI está fora de escopo (`Never`). O branch de orquestração (`awaiting-operator`/`operator_actions`) está corretamente dormente: nenhuma AC exige ação humana fora do repo.

## Design Notes

- **Efeito imediato sem invalidar sessão:** AC2 diz "não espera a sessão expirar". Como `RequireAuth` já relê `papel` do Postgres a cada requisição (AD-6, `BuscarUsuarioSessao`), basta o `UPDATE usuarios.papel` — não há cache de papel para invalidar, nenhuma sessão para revogar. A única "latência" é o access JWT não carregar papel (ele nunca carregou). No frontend, `useAuth().usuario.papel` só atualiza num reload/re-bootstrap; isso é aceitável e fora do escopo (nenhuma AC exige a navegação do cliente mudar sem reload) — o que a AC exige é o servidor autorizar no novo nível já na próxima chamada.
- **Guard duplo na aprovação:** o `UPDATE usuarios SET papel=<alvo> WHERE id=<solicitante> AND papel=<abaixo_do_alvo>` protege contra o solicitante ter sido promovido/rebaixado por outro caminho entre a solicitação e a decisão (`RowsAffected()==0` → `ErrEstadoContaMudou`). O `UPDATE solicitacoes_promocao ... WHERE id=$1 AND status='pendente'` fecha a corrida entre dois decisores simultâneos — idêntico ao padrão `marcarUsado` de `VerificarEmail`.
- **`id` malformado no path → 404, não 500:** `WHERE id = $1` com `$1` não-uuid faz o Postgres devolver erro `22P02`. Tratar `pq` `22P02` como `ErrSolicitacaoNaoEncontrada` (mesma classe de "input de cliente inválido não deve virar 500" já aplicada em `Cadastrar` para VARCHAR estourado). `sql.ErrNoRows` cai no mesmo erro.
- **Recorte da fila espelha `ListarUsuarios`:** papel do decisor vem do contexto (resolvido pelo middleware) e é passado como argumento — o service nunca reconsulta `usuarios` para descobrir quem chama (AD-8 forma 3). `adm` vê tudo; qualquer outro papel que passe pelo `RequireRole(gestor)` (na prática só `gestor`) vê só `papel_alvo='almoxarife'`.
- **Uma pendente por vez = índice parcial + checagem no service:** o `SELECT` prévio dá o `409` amigável no caso comum; o `CREATE UNIQUE INDEX ... WHERE status='pendente'` é o backstop de corrida (violação `23505` também mapeia para `ErrSolicitacaoPendenteExiste`). Mesma filosofia do `idx_usuarios_unico_adm` (migration 000001).

Golden — registro em `newMux`:

```go
mux.HandleFunc("POST /api/promocoes", middleware.RequireAuth(db, jwtSecret)(handlers.SolicitarPromocaoHandler(db)))
mux.HandleFunc("GET /api/promocoes/minha", middleware.RequireAuth(db, jwtSecret)(handlers.MinhaSolicitacaoHandler(db)))
mux.HandleFunc("GET /api/promocoes", middleware.RequireAuth(db, jwtSecret)(
    middleware.RequireRole(services.PapelGestor)(handlers.ListarPromocoesHandler(db))))
mux.HandleFunc("POST /api/promocoes/{id}/decisao", middleware.RequireAuth(db, jwtSecret)(
    middleware.RequireRole(services.PapelGestor)(handlers.DecidirPromocaoHandler(db))))
```

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` — build limpo, sem warnings.
- `docker compose up -d db && cd backend && go test -p 1 ./...` — todos passam, incl. os novos `promocao_test.go` de `services` e `handlers` e o inventário de rotas em `main_test.go`. Se `docker` não existir no ambiente, subir um Postgres 16 descartável via `initdb`/`pg_ctl` e apontar `DATABASE_URL` (mesmo procedimento das Stories 1.5/1.6).
- `cd frontend && npm run build && npm run lint && npm run test` — build/lint limpos; testes de `promocao` (lib), `ConfiguracoesPage` e `App` (rota `/configuracoes`) passam.
- `docker compose up --build` — `api`/`web` sobem saudáveis; migration 000004 aplica; o fluxo `/configuracoes` → "Solicitar promoção" → linha em `solicitacoes_promocao` → decisão por um `gestor` responde através do proxy `/api`.

**Manual checks (if no CLI):**
- Com um `usuario` logado, abrir `/configuracoes`: ver identidade e o botão "Solicitar promoção para Almoxarife". Clicar: `SELECT status, papel_alvo FROM solicitacoes_promocao` mostra `pendente/almoxarife`; recarregar a página deixa o botão desabilitado. Logar como `gestor`, abrir `/configuracoes`, aprovar a solicitação. Voltar ao `usuario`, chamar `GET /api/auth/me` com o token dele → `papel:"almoxarife"`. `SELECT decidido_por, decidido_em FROM solicitacoes_promocao` preenchidos.

## Auto Run Result

**Resumo:** Story 1.7 — solicitação de promoção de papel (FR-33). Nova tabela `solicitacoes_promocao` (migration 000004, máquina de estado `pendente → aprovada|rejeitada`, índice único parcial de "uma pendente por conta", `CHECK` de consistência de auditoria), regra em `services/promocao.go` (alvo derivado do papel no servidor; `usuario→almoxarife`, `almoxarife→gestor`; `adm` inalcançável), quatro endpoints em `handlers/promocao.go` (`POST /api/promocoes` e `GET /api/promocoes/minha` só com `RequireAuth`; `GET /api/promocoes` e `POST /api/promocoes/{id}/decisao` com `RequireRole(gestor)` e `adm` obrigatório para alvo `gestor`), e a página real `ConfiguracoesPage` ("Meu Perfil" + "Decidir promoções") substituindo a `PlaceholderPage` na rota `/configuracoes`. Aprovação troca `usuarios.papel` na mesma transação — efeito imediato via releitura de papel do middleware (AD-6).

**Arquivos alterados:**
- `backend/migrations/000004_create_solicitacoes_promocao.up.sql` / `.down.sql` (novos) — tabela + 4 índices + `CHECK`s (enum de `papel_alvo`/`status` e `((status='pendente') = (decidido_em IS NULL))`).
- `backend/services/promocao.go` (novo) — tipos, erros, `proximoPapelPromocao`/`papelAbaixoDe`, `SolicitarPromocao`, `BuscarMinhaSolicitacao`, `ListarSolicitacoesPendentes`, `DecidirSolicitacao` (guards + transação com `UPDATE` guardado, molde de `VerificarEmail`).
- `backend/services/promocao_test.go` (novo) — cobertura da I/O Matrix no nível de service, incl. 2 testes de concorrência.
- `backend/handlers/promocao.go` (novo) — 4 handlers; `aprovar` é `*bool` (ausente → 400); mapeamento `errors.Is` → envelope AD-14.
- `backend/handlers/promocao_test.go` (novo) — I/O Matrix na fronteira HTTP via composição real de middleware; encadeamentos pós-aprovação (`/api/auth/me`, `GET /api/usuarios` 403→200).
- `backend/main.go` — registro das 4 rotas em `newMux`; doc de pacote.
- `backend/main_test.go` — `TestNewMux_RegistraRotasDeAutenticacao` estendido; `TestNewMux_PromocoesRotasCarregamRequireRole` (gate de papel via `newMux` com tokens reais); `TestRunMigrations_SolicitacoesPromocaoSchema`.
- `frontend/src/pages/ConfiguracoesPage.tsx` / `.test.tsx` (novos) — Meu Perfil + fila de decisão; erro de carga da fila em `role="alert"`; refetch da fila em decisão falha; `aria-label` por linha.
- `frontend/src/lib/promocao.ts` / `.test.ts` (novos) — espelho de `proximoPapel`/`rotuloPapel`.
- `frontend/src/App.tsx` / `App.test.tsx` — rota-filha `/configuracoes` → `ConfiguracoesPage`.
- `_bmad-output/implementation-artifacts/spec-1-7-solicitacao-de-promocao-de-papel.md` — esta spec.

**Achados de revisão:** patch 8 (aplicados), defer 0, reject 16. Ver `## Review Triage Log`.

**Recomendação de review de acompanhamento:** `true` — 8 achados `patch` nesta passagem (high 0, medium 2, low 6); score = 3×2 + 1×6 = 12 (≥ 5), sem `high`.

**Verificação executada:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` — limpo (exit 0).
- `go test -p 1 -count=1 ./...` — Docker indisponível (`docker: command not found`); usado cluster PostgreSQL 16 descartável via `initdb`/`pg_ctl` (TCP `127.0.0.1:5433`, role/db `stockflow`, extensões `pgcrypto`+`citext`), `DATABASE_URL` apontado para ele. **Todos os 5 pacotes passam** (`backend`, `backend/cmd/seed-admin`, `backend/handlers`, `backend/middleware`, `backend/services`), incl. os novos testes de `promocao`, o gate de papel via `newMux` e o teste de schema da migration 000004. Cluster removido ao final.
- `cd frontend && npm run lint && npm run build && npm run test` — lint (`oxlint`) e build (`tsc` + `vite`) limpos; **15 arquivos / 121 testes passando**.
- `docker compose up --build` — **não executado**: Docker indisponível (idêntico às Stories 1.5/1.6). A superfície HTTP segue coberta por testes de integração contra Postgres real; o frontend por testes de componente com `fetch` mockado.
- **Matrix Test Audit:** todas as 24 linhas da I/O & Edge-Case Matrix cobertas por ≥1 teste que rodou e passou (service + fronteira HTTP para o backend; componente para as 3 linhas de UI).

**Riscos residuais:**
- `docker compose up --build` (smoke E2E da seção Verification) não pôde rodar por ausência do binário `docker` — risco residual baixo; todas as camadas têm cobertura automatizada equivalente.
- Verificação de AC1/AC2 no fluxo de UI é fatiada em fronteiras dubladas (`fetch` mockado no frontend, `httptest` no backend) — não há teste E2E de clique-renderizado-até-linha-real. É a estratégia de teste de todo o repositório.
- `useAuth().usuario.papel` no cliente só reflete uma promoção após reload/re-bootstrap (sem interceptor de 401 / refresh proativo nesta story — limitação reconhecida desde a Story 1.4). O backend sempre autoriza pelo papel lido na hora, então a ação permanece segura.
- Notificação ao solicitante da decisão (toast na próxima abertura, EXPERIENCE.md) fica para uma story posterior — aqui o estado é consultado sob demanda por `GET /api/promocoes/minha`.
- A árvore de trabalho retém modificações do orquestrador em `deferred-work.md`/`sprint-status.yaml` feitas antes desta invocação — intencionalmente não tocadas.
