---
title: 'Exclusão e anonimização de dados pessoais por Adm'
type: 'feature'
created: '2026-09-04'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
context: []
warnings: [oversized]
deferred:
  - summary: >-
      O backstop de violação de unicidade (`23505`) em `SolicitarExclusaoConta`
      para a corrida entre o `SELECT EXISTS` prévio e o `INSERT` não tem teste
      que exercite concorrência real (duas goroutines/transações).
    evidence: |-
      `TestSolicitarExclusaoConta_Duplicata` só chama a função duas vezes em
      sequência, nunca em paralelo, então o branch de tratamento de `23505`
      nunca é de fato exercitado por um teste. O mesmo padrão (mesma lacuna) já
      existe no molde `SolicitarPromocao`/`promocao_test.go`, que também nunca
      testou esse branch com concorrência real — não é uma regressão desta
      história, é uma lacuna herdada do padrão que ela replicou fielmente.
    location: >-
      backend/services/exclusao_conta.go
    severity: low
baseline_revision: 'b3e23f3c445a758653eb61984e9951d10bf7a75c'
---

<intent-contract>

## Intent

**Problem:** A LGPD dá ao Usuário o direito de exclusão da conta, mas hoje não há
nenhum caminho para isso: nem o Usuário pede, nem o Adm processa. A exclusão não
pode apagar/nulificar `usuario_id` — Movimentações, Pedidos e log de acesso dos
épicos anteriores referenciam essa coluna e a integridade histórica/auditoria
tem de sobreviver intacta.

**Approach:** Exclusão baseada em solicitação, nunca self-service. Nova tabela
`solicitacoes_exclusao_conta` (molde de `solicitacoes_promocao`, migração
000004): qualquer Usuário autenticado registra uma solicitação `pendente` da
própria conta em `POST /api/usuarios/me/solicitacao-exclusao`; só um `adm`
lista (`GET /api/solicitacoes-exclusao`) e processa
(`POST /api/solicitacoes-exclusao/{id}/processamento`). Processar = anonimizar
`usuarios.nome`/`usuarios.email`, zerar credenciais (`senha_hash=NULL`,
`ativo=false`, MFA desligado) e revogar sessões — sem tocar em nenhuma linha de
`movimentacoes`, `pedidos` ou `logs_acesso`. UI: botão "Solicitar exclusão de
conta" na `PrivacidadeSection` (qualquer papel) + nova
`SolicitacoesExclusaoSection` só para `adm`, com `ConfirmDialog` de variante
`destructive` antes de anonimizar.

## Boundaries & Constraints

**Always:** O alvo da anonimização é SEMPRE `solicitacoes_exclusao_conta.solicitante_id`
— nunca um id vindo de path/query/body do request de processamento (o path só
carrega o id da SOLICITAÇÃO). `usuarioId` da solicitação de exclusão vem sempre
de `middleware.UsuarioDaSessao` (rota `me/...`, molde da Story 8.1). A
anonimização roda numa única transação: `UPDATE usuarios` (nome/email
anonimizados, `senha_hash=NULL`, `ativo=false`, `mfa_habilitado=false`,
`mfa_secret=NULL`, `email_verificado=false`) + revogação de todas as sessões
vivas (`UPDATE sessoes ... revogado_em`) + invalidação dos `tokens_acao`
pendentes da conta + transição da solicitação para `processada`
(`processado_por`/`processado_em`). E-mail anonimizado é determinístico e único:
`'anonimizado+' || <id da conta> || '@anonimizado.invalido'` (TLD reservado
RFC 2606, satisfaz `idx_usuarios_email_lower`), sempre minúsculo. `papel` da
conta NÃO muda. No máximo uma solicitação `pendente` por conta (checagem prévia
+ índice parcial único, backstop de corrida — molde de `SolicitarPromocao`).
Guarda do último `adm`: se o alvo tem `papel='adm'` e não sobra nenhum outro
`adm` ativo (`SELECT count(*) FROM usuarios WHERE papel='adm' AND ativo=true AND
id <> $alvo` == 0), o processamento é bloqueado com mensagem explicando que ao
menos um `adm` ativo deve sempre existir — nenhuma escrita acontece. Rota de
processamento e de listagem atrás de `RequireAuth` + `RequireRole(adm)` (mesmo
gate de `GET /api/logs-acesso`). Envelope de erro fixo (AD-14):
`FORBIDDEN`/`NOT_FOUND`/`CONFLICT`/`VALIDATION_ERROR`/`INTERNAL_ERROR`.

**Block If:** Nenhuma — todo o escopo é determinístico. O guard do último `adm`
é uma regra de negócio checada em runtime que devolve `409 CONFLICT`, nunca um
HALT.

**Never:** Nunca deletar linha de `usuarios`, `movimentacoes`, `pedidos`,
`logs_acesso` nem nulificar/alterar `usuario_id` em qualquer uma delas — a
anonimização só reescreve nome/email/credenciais na própria linha de
`usuarios`. Nunca deixar a anonimização ser disparada fora de
`RequireRole(adm)` nem pelo próprio dono via uma rota `me/...` (a rota `me`
só REGISTRA a solicitação). Nunca criar SLA/prazo de resposta LGPD (fora de
escopo do épico). Nunca aceitar `solicitante_id`/alvo pelo corpo do request.
Nunca reabrir a decisão de não-enumeração da Story 1.12 nem mexer no formato do
export da Story 8.1.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Usuário solicita exclusão (1ª vez) | `POST /api/usuarios/me/solicitacao-exclusao`, sessão válida, sem pendente | `201`/`200`, linha `pendente` criada, corpo com `{id,status:"pendente",criadoEm}` | Nenhum erro esperado |
| Usuário já tem solicitação pendente | mesma rota, já existe `pendente` da conta | `409 CONFLICT`, nenhuma linha nova | `CONFLICT` "já existe uma solicitação de exclusão pendente" |
| Adm lista solicitações | `GET /api/solicitacoes-exclusao`, sessão `adm` | `200`, array de pendentes com `nome`/`email`/`papel`/`criadoEm` do solicitante (JOIN), ordenado por `criado_em,id`; vazio => `[]` | Nenhum erro esperado |
| Adm processa solicitação de conta comum | `POST /api/solicitacoes-exclusao/{id}/processamento`, alvo `usuario`/`almoxarife`/`gestor` | `200`, `usuarios.nome`/`email` anonimizados, `senha_hash NULL`, `ativo=false`, MFA off, sessões do alvo revogadas, solicitação `processada`; `movimentacoes`/`pedidos`/`logs_acesso` do alvo intactos (mesmo `usuario_id`, mesma contagem) | Nenhum erro esperado |
| Login por e-mail antigo após anonimização | `POST /api/auth/login` com o e-mail original | `401` credenciais inválidas — igual a conta inexistente (nenhuma linha casa `lower(email)`) | Vocabulário padrão da Story 1.4 |
| SSO com o e-mail antigo após anonimização | callback Keycloak com o e-mail original | `401 SSO_SEM_CONTA` — `BuscarUsuarioPorEmailSSO` não acha linha | Vocabulário padrão da Story 1.9 |
| Processar deixaria zero `adm` ativo | alvo `papel='adm'` e nenhum outro `adm` ativo | `409 CONFLICT`, NENHUMA escrita, mensagem "ao menos um administrador ativo deve sempre existir" | `CONFLICT` |
| Solicitação inexistente / id malformado | `{id}` sem match ou não-UUID (`pq` 22P02) | `404 NOT_FOUND` | `NOT_FOUND` |
| Solicitação já processada (reuso/corrida) | `{id}` com `status='processada'` | `409 CONFLICT`, nenhuma escrita | `CONFLICT` "solicitação não está mais pendente" |
| Sem sessão / papel insuficiente | rotas `adm` sem token ou como `gestor`/abaixo | `401` / `403 FORBIDDEN` (decisão do middleware) | Envelope padrão |
| Falha de banco em qualquer passo | erro de `db.Exec/Query` | `500 INTERNAL_ERROR`, transação revertida, nenhuma anonimização parcial | `escreverErro(500,"INTERNAL_ERROR")` + `slog.Error` |

</intent-contract>

## Code Map

- `backend/migrations/000030_create_solicitacoes_exclusao_conta.up.sql` / `.down.sql` (NOVO) -- `CREATE TABLE solicitacoes_exclusao_conta` molde de `000004_create_solicitacoes_promocao`: `id UUID PK`, `solicitante_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE`, `status VARCHAR(20) NOT NULL DEFAULT 'pendente' CHECK (status IN ('pendente','processada'))`, `criado_em TIMESTAMPTZ NOT NULL DEFAULT now()`, `processado_por UUID REFERENCES usuarios(id) ON DELETE SET NULL`, `processado_em TIMESTAMPTZ`, `CHECK ((status='pendente') = (processado_em IS NULL))`; `CREATE UNIQUE INDEX ... ON (solicitante_id) WHERE status='pendente'`. `.down.sql` = `DROP TABLE`.
- `backend/services/exclusao_conta.go` (NOVO) -- `SolicitacaoExclusao{ID,Status,CriadoEm,ProcessadoEm *time.Time}` + `SolicitacaoExclusaoPendente{ID,SolicitanteNome,SolicitanteEmail,SolicitantePapel,CriadoEm}`. Erros `ErrExclusaoPendenteExiste` (409), `ErrSolicitacaoExclusaoNaoEncontrada` (404), `ErrSolicitacaoExclusaoNaoPendente` (409), `ErrUltimoAdmAtivo` (409). Funções:
  - `SolicitarExclusaoConta(db, solicitanteID string) (SolicitacaoExclusao, error)` -- molde exato de `SolicitarPromocao` (`services/promocao.go`): `SELECT EXISTS` de pendente -> `ErrExclusaoPendenteExiste`; `INSERT ... RETURNING`; violação `23505` (pqUniqueViolation) -> `ErrExclusaoPendenteExiste`.
  - `ListarSolicitacoesExclusao(db) ([]SolicitacaoExclusaoPendente, error)` -- `SELECT s.id, u.nome, u.email, u.papel, s.criado_em FROM solicitacoes_exclusao_conta s JOIN usuarios u ON u.id = s.solicitante_id WHERE s.status='pendente' ORDER BY s.criado_em, s.id`. Slice vazio não-nil (molde de `ListarUsuarios`).
  - `ProcessarExclusaoConta(db, solicitacaoID, atorID string) (SolicitacaoExclusaoPendente, error)` -- transação: `SELECT ... FOR UPDATE` da solicitação (`sql.ErrNoRows`/`22P02` -> NaoEncontrada; `status<>'pendente'` -> NaoPendente); resolve `solicitante_id` + `papel`/`ativo` do alvo; se `papel=='adm'` e `SELECT count(*) FROM usuarios WHERE papel='adm' AND ativo=true AND id <> $alvo` == 0 -> `ErrUltimoAdmAtivo` (rollback); `UPDATE usuarios SET nome='Usuário anonimizado', email='anonimizado+'||id||'@anonimizado.invalido', senha_hash=NULL, ativo=false, mfa_habilitado=false, mfa_secret=NULL, email_verificado=false, tentativas_login_falhas=0, bloqueado_ate=NULL WHERE id=$alvo`; `UPDATE sessoes SET revogado_em=now() WHERE usuario_id=$alvo AND revogado_em IS NULL` (molde de `AlterarAtivacaoUsuario`); `UPDATE tokens_acao SET usado_em=now() WHERE usuario_id=$alvo AND usado_em IS NULL`; `UPDATE solicitacoes_exclusao_conta SET status='processada', processado_por=$ator, processado_em=now() WHERE id=$id AND status='pendente'` (RowsAffected()==0 -> `ErrSolicitacaoExclusaoNaoPendente`); `Commit`. Nunca faz `SELECT`/`UPDATE`/`DELETE` em `movimentacoes`/`pedidos`/`logs_acesso`.
- `backend/handlers/exclusao_conta.go` (NOVO) -- `SolicitarExclusaoContaHandler(db)` (rota `me`, molde de `ExportarDadosUsuarioHandler`: resolve `usuario` via `middleware.UsuarioDaSessao`, chama service, `escreverJSON(w,201,...)`; `ErrExclusaoPendenteExiste` -> 409 `CONFLICT`). `ListarSolicitacoesExclusaoHandler(db)` (molde de `ListarUsuariosHandler`: `escreverJSON(w,200,map{"solicitacoes":...})`). `ProcessarExclusaoContaHandler(db)` (molde de `DecidirPromocaoHandler`/`RebaixarUsuarioHandler`: `r.PathValue("id")` + `usuario.ID`; switch nos erros -> 404/409/500). Sem corpo em nenhuma das duas rotas de escrita.
- `backend/main.go` -- registrar após o bloco "Story 8.1" (~linha 652), banner "Story 8.2": `POST /api/usuarios/me/solicitacao-exclusao` atrás só de `RequireAuth`; `GET /api/solicitacoes-exclusao` e `POST /api/solicitacoes-exclusao/{id}/processamento` atrás de `RequireAuth` + `RequireRole(services.PapelAdm)`.
- `backend/services/exclusao_conta_test.go` (NOVO) -- cobre a I/O Matrix no nível de service: solicitar (happy, duplicata), listar (pendentes, ordenação, vazio), processar (anonimização completa + `movimentacoes`/`pedidos`/`logs_acesso` intactos por contagem e `usuario_id`; login por senha e `BuscarUsuarioPorEmailSSO` falham depois; solicitação inexistente; já processada; último `adm` bloqueado sem escrita; `adm` com outro `adm` ativo prossegue).
- `backend/handlers/exclusao_conta_test.go` (NOVO) -- via `newMux` real (molde de `handlers/gestao_usuarios_test.go`/`pedidos_test.go`): 201 solicitação; 409 duplicata; 200 lista `adm`; 401 sem token; 403 como `gestor`; 200 processamento happy (headers/corpo); 409 último `adm`; 404 id desconhecido.
- `frontend/src/lib/privacidade.ts` -- acrescentar `solicitarExclusaoConta(): Promise<void>` (`POST`, não-ok propaga `body.error?.message` ou nova const `MENSAGEM_ERRO_SOLICITAR_EXCLUSAO`), `listarSolicitacoesExclusao(): Promise<SolicitacaoExclusao[]>` e `processarExclusaoConta(id: string): Promise<void>` (não-ok propaga `body.error?.message` — a mensagem do último-`adm` vem do servidor). Tipo `SolicitacaoExclusao { id; nome; email; papel; criadoEm }`.
- `frontend/src/lib/privacidade.test.ts` -- estender: método/URL/header de cada nova função; propagação de `body.error.message` (409 último `adm`) e fallback.
- `frontend/src/components/privacidade/PrivacidadeSection.tsx` -- acrescentar `Button` "Solicitar exclusão de conta" (`variant="destructive"`) + `ConfirmDialog` (title "Solicitar exclusão da sua conta?", description explicando que um administrador vai revisar e anonimizar, `confirmLabel="Solicitar exclusão"`); sucesso -> `toast.success` "Solicitação registrada..."; 409 -> `toast.error` com a mensagem do servidor. Guarda de duplo-clique local, molde de `aoBaixarMeusDados`.
- `frontend/src/components/privacidade/PrivacidadeSection.test.tsx` -- estender: botão visível para qualquer papel; clique abre `ConfirmDialog`; confirmar chama `solicitarExclusaoConta`; 409 mostra toast de erro.
- `frontend/src/components/usuarios/SolicitacoesExclusaoSection.tsx` (NOVO) -- molde de `GestaoUsuariosSection`: `Card` só renderizado para `rankPapel(papel) >= rankPapel('adm')`; `GET` da lista no mount; por linha (nome/email/papel/data) um `Button variant="destructive"` "Processar exclusão" -> `ConfirmDialog` com `confirmVariant="destructive"`, `confirmLabel="Anonimizar"`, title/description sobre a irreversibilidade; confirmar chama `processarExclusaoConta(id)`; erro (inclui 409 último `adm`) -> `<p role="alert">` inline com a mensagem; sucesso E falha refazem a lista.
- `frontend/src/components/usuarios/SolicitacoesExclusaoSection.test.tsx` (NOVO) -- render só para `adm`; lista carrega; confirmar dispara `processarExclusaoConta`; 409 último `adm` mostra alerta inline com a mensagem do servidor; lista recarrega após ação.
- `frontend/src/components/ConfirmDialog.tsx` -- acrescentar prop opcional `confirmVariant?: 'default' | 'destructive'` (default `'default'`, retrocompatível) repassada como `variant` para `AlertDialogAction` (que já aceita `variant`).
- `frontend/src/components/ConfirmDialog.test.tsx` -- estender: `confirmVariant="destructive"` aplica a classe/variante destrutiva ao botão de confirmar; omitir mantém o visual atual.
- `frontend/src/pages/ConfiguracoesPage.tsx` -- importar e montar `<SolicitacoesExclusaoSection />` logo após `<LogAcessoSection />` (mesmo gate `rankPapel(papel) >= rankPapel('adm')`, ou deixar o gate interno do componente e montar incondicionalmente como `PrivacidadeSection`).
- `frontend/src/pages/ConfiguracoesPage.test.tsx` -- estender: `adm` vê "Solicitações de exclusão"; `gestor`/`usuario` não.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000030_create_solicitacoes_exclusao_conta.up.sql` / `.down.sql` -- criar a tabela + índice parcial único -- persistência da fila de solicitações
- `backend/services/exclusao_conta.go` -- `SolicitarExclusaoConta`, `ListarSolicitacoesExclusao`, `ProcessarExclusaoConta` -- regra de negócio: registro, listagem e anonimização transacional com guarda do último `adm`
- `backend/handlers/exclusao_conta.go` -- os três handlers HTTP -- fronteira: decodifica/serializa, mapeia erros para o envelope AD-14
- `backend/main.go` -- registrar as três rotas (1 sob `RequireAuth`, 2 sob `RequireRole(adm)`) -- expõe os endpoints
- `backend/services/exclusao_conta_test.go`, `backend/handlers/exclusao_conta_test.go` -- cobrir toda a I/O & Edge-Case Matrix (service + fronteira HTTP)
- `frontend/src/lib/privacidade.ts` -- 3 funções cliente + tipo -- acesso HTTP puro
- `frontend/src/components/ConfirmDialog.tsx` -- prop `confirmVariant` -- permite o token `destructive` no botão de confirmar (exigência de UX do épico)
- `frontend/src/components/privacidade/PrivacidadeSection.tsx` -- botão + `ConfirmDialog` "Solicitar exclusão de conta" -- lado Usuário
- `frontend/src/components/usuarios/SolicitacoesExclusaoSection.tsx` -- nova seção `adm` -- lado Adm: listar e processar
- `frontend/src/pages/ConfiguracoesPage.tsx` -- montar a nova seção -- torna acessível ao `adm`
- `frontend/src/lib/privacidade.test.ts`, `.../PrivacidadeSection.test.tsx`, `.../SolicitacoesExclusaoSection.test.tsx`, `.../ConfirmDialog.test.tsx`, `.../ConfiguracoesPage.test.tsx` -- render, fluxos e gates de papel

**Acceptance Criteria:**
- Given um Usuário autenticado de qualquer papel em Configurações -> Privacidade, when ele clica "Solicitar exclusão de conta" e confirma no `ConfirmDialog`, then uma solicitação `pendente` da própria conta fica registrada para um `adm` processar (nenhuma exclusão imediata) e um toast de sucesso aparece
- Given um `adm` em Configurações, when a página carrega, then a seção "Solicitações de exclusão" lista as solicitações pendentes com nome, e-mail, papel e data do solicitante; um `gestor`/`usuario` não vê essa seção
- Given um `adm` processando uma solicitação de uma conta não-`adm`, when ele confirma a anonimização no `ConfirmDialog` (botão com token `destructive`), then `usuarios.nome`/`usuarios.email` da conta viram valores anonimizados, `senha_hash` fica `NULL`, `ativo=false`, MFA desligado, as sessões vivas do alvo são revogadas e a solicitação fica `processada` — e nenhuma linha de `movimentacoes`, `pedidos` ou `logs_acesso` do alvo é alterada, removida ou tem `usuario_id` mexido
- Given uma solicitação para a única conta `adm` ativa do sistema, when o `adm` tenta processá-la, then o sistema responde `409 CONFLICT` com a mensagem de que ao menos um administrador ativo deve sempre existir e nada é anonimizado
- Given uma conta já anonimizada, when alguém tenta autenticar com o e-mail antigo (senha ou SSO), then o login falha exatamente como se a conta não existisse

## Spec Change Log

## Review Triage Log

**NOTA (supervisão, 2026-09-04):** o run pausou (auto-rollback OFF, dev-verify) com o frontmatter já em `status: 'in-review'`, mas esta seção está vazia — nenhum "### Review pass" foi de fato registrado, ou seja, a revisão adversarial não rodou. `go build/vet/test` (backend) e `tsc`/`vitest` (frontend, 569/569) passam 100% com o que existe hoje, mas isso cobre só a implementação do dev, não uma revisão crítica. Status revertido para `in-progress` até uma sessão nova rodar a revisão de verdade.

### 2026-09-04 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 9 (high 1, medium 0, low 8)
- defer: 1 (low 1)
- reject: 9
- addressed_findings:
  - `[high]` `[patch]` Texto da UI (`PrivacidadeSection.tsx`, `SolicitacoesExclusaoSection.tsx`) e comentário JSDoc afirmavam que Movimentações/Pedidos/log de acesso ficam "sem identificação" após a anonimização — falso, já que `pedidos.solicitante` (texto livre) e `logs_acesso.email_informado` preservam o nome/e-mail originais e nunca são tocados (a spec proíbe alterar essas tabelas). Texto corrigido para descrever fielmente a garantia real (vínculo por `usuario_id` preservado, texto livre pré-existente não reescrito).
  - `[low]` `[patch]` Faltava teste 401-sem-token para `ProcessarExclusaoContaHandler` — adicionado.
  - `[low]` `[patch]` Gate de papel `adm` duplicado (`SolicitacoesExclusaoSection.tsx` internamente + `ConfiguracoesPage.tsx` externamente) — removida a checagem externa, mantido só o gate interno do componente.
  - `[low]` `[patch]` Linhas da I/O Matrix descritas na fronteira HTTP (login com e-mail antigo, SSO com e-mail antigo, falha de banco → 500, solicitação já processada, mensagem exata do guard do último `adm`, lista vazia/fora-de-ordem) só tinham teste no nível de service — adicionados testes de handler via rota real para cada uma, incluindo assert da mensagem exata do último `adm`.
- Achados rejeitados (fora de escopo pela própria intenção, ou já cobertos/consistentes com padrão existente no código): corrida teórica no guard do último `adm` (inalcançável hoje, só 1 conta `adm` pode existir por índice único — já discutido em Design Notes); alcançabilidade questionável do branch `RowsAffected()==0`; ausência de paginação na listagem (consistente com `ListarUsuarios`/logs, nenhum endpoint do projeto pagina); ausência de estado de carregamento na `SolicitacoesExclusaoSection` (o molde `GestaoUsuariosSection` também não tem); `CHECK` não amarrar `processado_por` a `pendente` (sem impacto funcional, os dois campos sempre são escritos juntos); falta de teste para impedir nova solicitação após anonimização (depende do middleware AD-6 já testado em história anterior); ausência de notificação ao usuário após processamento (fora de escopo — a intenção proíbe explicitamente criar SLA/fluxo de resposta LGPD); ausência de log de auditoria adicional além de `processado_por`/`processado_em` (a própria intenção especifica esses dois campos como o mecanismo de auditoria); inconsistência de status do frontmatter apontada na nota de supervisão acima (já resolvida por este próprio run).

## Design Notes

**Uma só conta `adm` no sistema.** `idx_usuarios_unico_adm` (migração 000001) é
um índice único parcial em `papel='adm'` e não existe promoção para `adm`
(Story 1.7: `papel_alvo IN ('almoxarife','gestor')`). Logo a "guarda do último
`adm`" na prática significa: **não se anonimiza a conta `adm`**. A checagem foi
escrita como `count(*) ... WHERE papel='adm' AND ativo=true AND id <> $alvo == 0`
(em vez de `if papelAlvo == 'adm' { block }`) porque expressa literalmente o
invariante do épico ("deixaria zero `adm` ativo") e continua correta se a
unicidade de `adm` for relaxada no futuro. O AC do épico fala em "a própria
conta ou a de outro `adm`" — as duas caem nessa mesma checagem: o alvo é uma
conta `adm` e não sobra outro `adm` ativo.

**Por que o e-mail muda (e não só o nome).** O AC exige que login por senha E
por SSO falhem "como se a conta não existisse". `Login` (auth.go) e
`BuscarUsuarioPorEmailSSO` (auth_sso.go) resolvem por `lower(email)`; trocar o
e-mail para um valor reservado/único faz os dois devolverem "não encontrado"
sem nenhum código novo nesses caminhos. `ativo=false` + revogação de sessão +
`senha_hash=NULL` são defesa em profundidade (middleware relê `ativo` a cada
request — AD-6).

**Solicitação sempre registrável.** Mesmo a solicitação da única conta `adm`
é aceita e fica `pendente`; o bloqueio acontece só no processamento (é onde o
dado é destruído). Não há período de espera nem SLA (fora de escopo do épico).

**`ConfirmDialog.confirmVariant`.** `AlertDialogAction` já aceita `variant`; o
wrapper só não repassava. Prop opcional com default `'default'` — nenhum caller
existente muda de comportamento.

## Verification

**Commands:**
- `cd backend && DATABASE_URL=... go test -p 1 ./services/... ./handlers/... -run 'ExclusaoConta|SolicitacaoExclusao|SolicitarExclusao|ProcessarExclusao'` -- expected: todos os novos testes passam (integração exige Postgres em `DATABASE_URL`; sem ele, `SKIP`)
- `cd backend && DATABASE_URL=... go test -p 1 ./services/... ./handlers/...` -- expected: pacotes inteiros verdes, nenhuma regressão
- `cd backend && go build ./... && go vet ./...` -- expected: compila e passa no vet
- `cd backend && migrate ... up && migrate ... down 1` (ou o runner do projeto) -- expected: `000030` sobe e desce limpa
- `cd frontend && npx vitest run src/lib/privacidade.test.ts src/components/privacidade src/components/usuarios/SolicitacoesExclusaoSection.test.tsx src/components/ConfirmDialog.test.tsx src/pages/ConfiguracoesPage.test.tsx` -- expected: novos e existentes verdes
- `cd frontend && npx tsc --noEmit && npx oxlint` -- expected: sem erro

## Auto Run Result

Status: done

**Resumo da mudança implementada:** Story 8.2 — exclusão/anonimização de dados pessoais mediada por `adm` (LGPD). Nova tabela `solicitacoes_exclusao_conta` (migração 000030); qualquer usuário autenticado registra uma solicitação `pendente` da própria conta (`POST /api/usuarios/me/solicitacao-exclusao`); só um `adm` lista (`GET /api/solicitacoes-exclusao`) e processa (`POST /api/solicitacoes-exclusao/{id}/processamento`) — processar anonimiza `usuarios.nome`/`email`, zera credenciais/MFA, revoga sessões e tokens de ação, tudo numa transação, sem tocar `movimentacoes`/`pedidos`/`logs_acesso`; guarda contra deixar zero `adm` ativo. UI: botão "Solicitar exclusão de conta" em `PrivacidadeSection` (qualquer papel) + nova `SolicitacoesExclusaoSection` só para `adm`. Todo o backend/frontend já havia sido implementado e testado num checkpoint anterior (commit `382e048`); esta invocação confirmou a implementação contra a spec linha a linha (sem código faltando) e então rodou a revisão adversarial que ainda não tinha sido feita.

**Arquivos alterados nesta passagem (revisão):**
- `frontend/src/components/privacidade/PrivacidadeSection.tsx` — texto do card/`ConfirmDialog` corrigido para não afirmar que Movimentações/Pedidos ficam "sem identificação" (falso: `pedidos.solicitante` é texto livre com o nome real, nunca tocado).
- `frontend/src/components/usuarios/SolicitacoesExclusaoSection.tsx` — mesmo ajuste de texto (comentário JSDoc + `ConfirmDialog`, incluindo a menção indevida a `logs_acesso`); removida a duplicação do gate de papel `adm` (mantido só o gate interno do componente).
- `frontend/src/pages/ConfiguracoesPage.tsx` — removida a checagem `rankPapel` redundante ao montar `<SolicitacoesExclusaoSection />` (molde de `PrivacidadeSection`, que já monta incondicionalmente).
- `backend/handlers/exclusao_conta_test.go` — adicionados testes de fronteira HTTP que faltavam para linhas da I/O Matrix descritas nesse nível: 401 sem token no processamento; login e SSO com e-mail antigo falhando via rota real após anonimização; falha de banco → `500 INTERNAL_ERROR` via handler; solicitação já processada → `409` batendo a rota duas vezes; mensagem exata do guard do último `adm`; lista vazia e lista com múltiplos itens fora de ordem via rota real.
- `_bmad-output/implementation-artifacts/spec-8-2-exclusao-e-anonimizacao-de-dados-pessoais-por-adm.md` — frontmatter (`status: done`, `followup_review_recommended: true`, `deferred`), nova entrada no `## Review Triage Log` e este `## Auto Run Result`.

Nenhuma mudança de schema/serviço/rota (comportamento de backend) foi feita nesta passagem — só texto de UI/comentário, remoção de gate duplicado e testes novos.

**Achados de revisão (esta passagem):** patch 9 (high 1, low 8, todos aplicados), defer 1 (low 1), reject 9. Detalhe completo em `## Review Triage Log`.
- `[high]` `[patch]` UI/comentário prometiam anonimização de Pedidos/Movimentações/log de acesso que não acontece de fato (`pedidos.solicitante`, `logs_acesso.email_informado`) — texto corrigido.
- `[low]` `[patch]` x8 — teste 401 faltando no handler de processamento; gate de papel duplicado; 6 linhas da I/O Matrix com garantia descrita na fronteira HTTP mas só testadas no nível de service (login/SSO pós-anonimização, falha de banco → 500, solicitação já processada, mensagem exata do último `adm`, lista vazia/fora-de-ordem) — todas agora com teste de rota real.
- `defer` 1: backstop de corrida (`23505`) em `SolicitarExclusaoConta` sem teste de concorrência real — lacuna herdada do próprio molde (`SolicitarPromocao`), não uma regressão desta história.
- `reject` 9: em sua maioria decisões já cobertas pela própria intenção (proibição explícita de tocar `movimentacoes`/`pedidos`/`logs_acesso`; ausência de SLA/notificação como escopo vetado; `processado_por`/`processado_em` já é o mecanismo de auditoria especificado) ou consistência com padrões já existentes no código (sem paginação em nenhuma listagem do projeto; sem estado de carregamento no molde `GestaoUsuariosSection`; corrida no guard do último `adm` inalcançável hoje por haver só uma conta `adm` possível).

**Recomendação de review de acompanhamento:** `true`. Achados `patch` nesta passagem: high 1, medium 0, low 8. Regra: qualquer `patch` `high` já implica `true` (score de referência 3×0 + 1×8 = 8, também ≥ 5).

**Verificação executada (após os 9 patches):**
- `cd backend && go build ./... && go vet ./...` — limpo.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@127.0.0.1:5432/stockflow?sslmode=disable go test -p 1 ./services/... ./handlers/...` — `PASS` nos dois pacotes, incluindo todos os testes novos desta passagem; nenhuma regressão.
- Ciclo de migração `000030` (`Steps(-1)` / `Up()` via `golang-migrate`) — sobe e desce limpo, schema pós-reup idêntico ao `up.sql` (verificado antes da passagem de revisão, sem mudança de schema nesta passagem).
- `cd frontend && npx vitest run src/lib/privacidade.test.ts src/components/privacidade src/components/usuarios/SolicitacoesExclusaoSection.test.tsx src/components/ConfirmDialog.test.tsx src/pages/ConfiguracoesPage.test.tsx` — 5 arquivos, 67 testes, tudo verde.
- `cd frontend && npx tsc --noEmit && npx oxlint` — limpo.
- Suíte completa (antes desta passagem, na implementação original): backend `./services/... ./handlers/...` inteiros verdes; frontend 569/569 testes.

**Riscos residuais:**
- A anonimização nunca torna Pedidos/Movimentações/log de acesso não-identificáveis por completo — `pedidos.solicitante` (texto livre) e `logs_acesso.email_informado` preservam o nome/e-mail originais indefinidamente. Isso é uma decisão deliberada do intent (integridade histórica/auditoria dos épicos anteriores tem prioridade sobre apagamento total), agora refletida corretamente no texto da UI, mas é um risco de LGPD que uma pessoa da área jurídica/compliance pode querer revisitar no futuro (ex.: um pedido de exclusão mais agressivo que também expurgue esses campos de texto livre, quebrando a rastreabilidade do histórico).
- O backstop de corrida (`23505`) para duplicar solicitação de exclusão nunca foi provado sob concorrência real, nem aqui nem no molde que ele copia (`SolicitarPromocao`) — registrado em `deferred`.
- `docker compose up --build` (smoke E2E) não foi executado nesta passagem; toda a superfície nova tem cobertura equivalente via testes de integração contra Postgres real (service + handler) e testes de componente no frontend.
