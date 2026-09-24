---
title: 'Story 14.3 — O adm altera a exigência de MFA, com aviso e auditoria'
type: 'feature'
created: '2026-09-24'
status: 'done'
baseline_revision: '252f649793a088134e02da6eec59ffa5604f7647'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-14-context.md'
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** `empresas.mfa_obrigatorio` (14.1) só é definido no cadastro pelo Dono da Plataforma (14.2). O `adm` da Empresa não tem como ligar ou desligar a exigência depois, e não existe trilha de quem mudou a política.

**Approach:** Criar a tabela append-only `auditoria_seguranca` (AD-35) e três rotas atrás de `RequireAuth → RequireRole(adm)`: ler a exigência atual (com a contagem de `gestor`/`adm` sem MFA), alterá-la e listar o histórico. A alteração acontece numa transação que faz um UPDATE condicional e grava a linha de auditoria só quando o valor muda. No frontend, uma seção "Dupla autenticação da Empresa" em Configurações, visível só ao `adm`, mostra a escolha e o histórico. Ao passar de "não" para "sim", ela abre uma confirmação com o aviso de contas sem MFA.

## Boundaries & Constraints

**Always:**
- Rotas novas (prefixo `/e/{slug}`), todas atrás de `RequireAuth → RequireRole(services.PapelAdm)`:
  - `GET /api/seguranca/mfa-empresa` → 200 `{"mfaObrigatorio": bool, "contasSemMfa": int}`;
  - `PUT /api/seguranca/mfa-empresa`, corpo `{"mfaObrigatorio": bool}` → 200 `{"mfaObrigatorio": bool, "alterado": bool}`;
  - `GET /api/seguranca/auditoria` → 200 `{"eventos": [...]}`.
- Não há PUT/PATCH/DELETE sobre a auditoria.
- Papel abaixo de `adm` recebe 403 FORBIDDEN do `RequireRole`. Um `adm` por senha sem MFA numa Empresa que exige recebe 403 `MFA_SETUP_REQUIRED` do mesmo gate. Nenhum handler reimplementa isso.
- `contasSemMfa` conta as contas da Empresa da requisição com `ativo = true`, `papel IN ('gestor','adm')`, `mfa_habilitado = false` e `senha_hash IS NOT NULL`. Só essas podem ser bloqueadas pelo gate: conta só-SSO nunca é. O próprio `adm` entra na conta se se encaixar.
- A alteração é atômica e idempotente: `UPDATE empresas SET mfa_obrigatorio = $novo WHERE id = $empresa AND mfa_obrigatorio <> $novo`. Se 0 linhas mudarem, a resposta é `alterado:false` e nenhuma linha de auditoria é gravada. Se 1 linha mudar, na mesma transação entra `INSERT auditoria_seguranca (empresa_id, ator_id, alvo_id NULL, acao 'exigencia_alterada', detalhe {"anterior": !novo, "novo": novo})`.
- Desligar a exigência só muda `empresas.mfa_obrigatorio`. Nenhuma coluna de `usuarios` (`mfa_habilitado`, `mfa_secret`, `mfa_ultimo_passo_usado`) é tocada.
- A leitura da auditoria é escopada por `empresa_id` da requisição, ordenada por `criado_em DESC, id DESC`, com limite de 200 linhas. Cada evento traz `id`, `acao`, `atorId`, `atorNome`, `alvoId`, `alvoNome`, `detalhe` (objeto JSON) e `criadoEm`.
- O corpo do PUT sem `mfaObrigatorio`, com `null` ou com um valor que não é booleano recebe 400 VALIDATION_ERROR. O limite do corpo é 4 KiB.
- Frontend: só a confirmação aplica a mudança de "não" para "sim". A mudança de "sim" para "não" aplica direto (não bloqueia ninguém). Depois do sucesso, `atualizarUsuario` reflete `empresa.mfaObrigatorio` na sessão. Assim o espelho de `App.tsx` passa a valer sem novo login, e o próprio `adm` sem MFA é redirecionado para configurar.

**Block If:** nada previsto.

**Never:**
- Nenhuma rota do Dono da Plataforma altera o flag.
- Não mexer no gate de `RequireRole` nem em `/api/auth/me` (14.1).
- Nada de reset ou desligamento de MFA de conta (14.4). A 14.4 só reaproveita a tabela e os valores `mfa_resetado`/`mfa_desligado` já permitidos no CHECK.
- Nada de trigger no banco. Append-only = nenhuma rota de escrita além do INSERT do service, como em `logs_acesso`.
- Não exigir confirmação no servidor: a confirmação é de UI.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Ligar | adm com MFA; Empresa `false`; PUT `true` | 200 `{mfaObrigatorio:true, alterado:true}`; banco `true`; 1 linha `exigencia_alterada` com `{"anterior":false,"novo":true}` e `ator_id` = adm | — |
| Desligar | Empresa `true`; gestor com `mfa_habilitado=true`; PUT `false` | 200 `alterado:true`; banco `false`; o gestor continua com `mfa_habilitado=true` e `mfa_secret` intacto; 1 linha `{"anterior":true,"novo":false}` | — |
| Idempotente | Empresa `true`; PUT `true` (reenvio) | 200 `{mfaObrigatorio:true, alterado:false}`; nenhuma linha nova de auditoria | — |
| Efeito do gate | depois de ligar, gestor por senha sem MFA chama uma rota `RequireRole(gestor)` | 403 MFA_SETUP_REQUIRED (comportamento da 14.1, sem novo login) | — |
| Contagem | Empresa com gestor sem MFA, adm sem MFA, gestor com MFA, gestor sem MFA inativo, gestor só-SSO sem MFA, almoxarife sem MFA, gestor sem MFA de outra Empresa | `contasSemMfa` = 2 | — |
| Papel insuficiente | token de `gestor` em qualquer uma das 3 rotas | 403 FORBIDDEN; banco inalterado | — |
| Adm bloqueado | adm por senha sem MFA; Empresa `true`; PUT `false` | 403 MFA_SETUP_REQUIRED; banco inalterado | — |
| Corpo inválido | `{}`, `{"mfaObrigatorio":null}`, `{"mfaObrigatorio":"sim"}` | 400 VALIDATION_ERROR; nada gravado | — |
| Escopo | auditoria com linhas da Empresa A e da B; adm da A consulta | só as linhas da A, mais recentes primeiro, com `atorNome` | — |
| Sem token | qualquer rota nova | 401 (RequireAuth) | — |

</intent-contract>

## Code Map

- `backend/migrations/000048_create_auditoria_seguranca.{up,down}.sql` -- nova tabela: `id UUID PK DEFAULT gen_random_uuid()`, `empresa_id UUID NOT NULL REFERENCES empresas(id)`, `ator_id UUID NOT NULL REFERENCES usuarios(id)`, `alvo_id UUID NULL REFERENCES usuarios(id)`, `acao VARCHAR(40) NOT NULL CHECK (acao IN ('exigencia_alterada','mfa_resetado','mfa_desligado'))`, `detalhe JSONB NOT NULL DEFAULT '{}'`, `criado_em TIMESTAMPTZ NOT NULL DEFAULT now()`, e índice `(empresa_id, criado_em DESC)`. O comentário segue o molde de `000045`/`000047`. O down faz `DROP TABLE IF EXISTS`.
- `backend/services/logs_acesso.go:77-150` -- molde de struct e listagem escopada (`LogAcesso`, `ListarLogsAcesso`: LEFT JOIN em `usuarios` com `u.empresa_id`, `ORDER BY ... , id DESC`, `LIMIT`).
- `backend/services/empresas.go:77` -- `Empresa.MFAObrigatorio` já existe (14.1). Só leitura.
- novo `backend/services/seguranca_empresa.go` (+ `_test.go`) -- `ContarContasSemMFA(db, empresaID) (int, error)`, `AlterarExigenciaMFA(db, empresaID, atorID string, novo bool) (alterado bool, err error)` (tx: UPDATE condicional + INSERT) e `type EventoAuditoriaSeguranca` + `ListarAuditoriaSeguranca(db, empresaID)` (limite `maxEventosAuditoriaSeguranca = 200`). `detalhe` é escaneado como `[]byte` e exposto como `json.RawMessage`.
- `backend/handlers/logs_acesso.go:100-129` -- molde de handler: guard `middleware.UsuarioDaSessao` → 500, `empresaDaRequisicao(w, r)` (`handlers/auth.go:53`), `escreverJSON`/`escreverErro`.
- novo `backend/handlers/seguranca_empresa.go` (+ `_test.go`) -- `ObterExigenciaMFAHandler`, `AlterarExigenciaMFAHandler` e `ListarAuditoriaSegurancaHandler`. O PUT decodifica em `struct{ MFAObrigatorio *bool \`json:"mfaObrigatorio"\` }` com `http.MaxBytesReader`, e `nil` vira 400. Para `mfaObrigatorio` na resposta, o GET usa a Empresa do contexto e o PUT devolve o valor pedido.
- `backend/handlers/logs_acesso_test.go:49-76` -- molde de despacho de teste: `comEmpresa(db, RequireAuth(...)(RequireRole(PapelAdm)(h)))`, `prefixoEmpresaTeste`, `testJWTSecret`. Para criar contas e tokens (adm com/sem MFA, gestor), reutilizar os helpers já usados em `handlers/auth_mfa_test.go` e `handlers/gestao_usuarios_test.go`.
- `backend/middleware/roles_test.go:278-345` -- molde de `definirMFAObrigatorio` com `t.Cleanup` que devolve `false`. A Empresa padrão da suíte é compartilhada (`handlers/empresa_teste_test.go`), então todo teste que liga o flag precisa restaurar `false` e apagar suas linhas de `auditoria_seguranca`.
- `backend/main.go:399-406` -- registrar as 3 rotas logo depois de `logs-acesso`, com a mesma composição e um comentário.
- `backend/main_test.go:471` -- tabela de "sem token chega no RequireAuth": acrescentar as rotas novas.
- `backend/services/empresas_plataforma_test.go:45` (`removerEmpresaComDados`) e o equivalente em `handlers/plataforma_test.go` -- acrescentar `DELETE FROM auditoria_seguranca WHERE empresa_id = $1` antes de `usuarios` (FK). `TRUNCATE usuarios CASCADE` já cobre o resto.
- `frontend/src/lib/auth.tsx:33-90` -- `UsuarioSessao.empresa?.mfaObrigatorio`, `mfaSetupPendente` e `atualizarUsuario`.
- `frontend/src/components/ConfirmDialog.tsx` -- confirmação padrão (nunca `window.confirm`).
- `frontend/src/components/logs/LogAcessoSection.tsx` -- molde de seção só-`adm`: fetch no mount, `role="alert"` na falha e tabela somente-leitura.
- `frontend/src/pages/ConfiguracoesPage.tsx:~555` -- montar a nova seção logo depois de `<SegurancaCard />`, com o gate `rankPapel(papel) >= rankPapel('adm')`. Atualizar o doc-comment.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000048_create_auditoria_seguranca.up.sql` / `.down.sql` -- criar a tabela e o índice -- trilha AD-35.
- `backend/services/seguranca_empresa.go` -- contagem, alteração transacional idempotente e listagem escopada -- regra de negócio num lugar só.
- `backend/services/seguranca_empresa_test.go` -- testes: contagem (linha "Contagem" da matriz), ligar/desligar gravam 1 linha com o `detalhe` certo, o idempotente não grava, desligar preserva o MFA das contas, e a listagem escopa por Empresa em ordem DESC.
- `backend/handlers/seguranca_empresa.go` -- os 3 handlers -- fronteira HTTP.
- `backend/handlers/seguranca_empresa_test.go` -- cobrir na fronteira HTTP: Ligar, Desligar, Idempotente, Efeito do gate (PUT `true` e depois gestor sem MFA → 403 MFA_SETUP_REQUIRED numa rota `RequireRole(gestor)`), Papel insuficiente, Adm bloqueado, Corpo inválido (os 3 corpos), Escopo e Sem token. Restaurar o flag e limpar a auditoria em `t.Cleanup`.
- `backend/main.go` + `backend/main_test.go` -- registrar e cobrir as rotas no mux real.
- `backend/services/empresas_plataforma_test.go`, `backend/handlers/plataforma_test.go` -- limpar `auditoria_seguranca` nos helpers de remoção de Empresa.
- `frontend/src/lib/segurancaEmpresa.ts` (+ `.test.ts`) -- tipos e funções `obterExigenciaMFA`, `alterarExigenciaMFA(valor)` e `listarAuditoriaSeguranca()` sobre `apiUrl`/`authHeaders`. Erro HTTP lança exceção.
- `frontend/src/components/seguranca/MfaEmpresaSection.tsx` -- card "Dupla autenticação da Empresa":
  - Mostra "Exigida" ou "Não exigida" e o botão "Passar a exigir" ou "Deixar de exigir".
  - Ao ligar, abre `ConfirmDialog` com a descrição: "N conta(s) gestor/adm ainda sem dupla autenticação ficarão sem acesso até cadastrar." Se o próprio usuário não tem MFA, acrescenta: "Isso inclui você."
  - Cancelar não chama a API. Ao desligar, chama direto.
  - Depois do sucesso: toast, `atualizarUsuario` com `empresa.mfaObrigatorio` e recarga do histórico.
  - O histórico é uma lista somente-leitura: data/hora, ator e "Não exigida → Exigida". Falha vira `role="alert"`.
- `frontend/src/components/seguranca/MfaEmpresaSection.test.tsx` -- testes:
  - exibe o estado atual;
  - ligar abre a confirmação com a contagem, e cancelar não faz o PUT;
  - confirmar faz o PUT com `{mfaObrigatorio:true}` e atualiza a sessão;
  - desligar faz o PUT sem diálogo;
  - o histórico é renderizado;
  - erro de carga vira alerta.
- `frontend/src/pages/ConfiguracoesPage.tsx` (+ teste) -- montar a seção só para `adm`. No teste, `adm` vê a seção e `gestor` não.

**Acceptance Criteria:**
- Given Configurações aberta por um `adm`, when a página carrega, then a seção "Dupla autenticação da Empresa" mostra a escolha atual, o controle de alteração e o histórico. Para `gestor` ou papéis abaixo, a seção não é renderizada.
- Given a exigência desligada, when o `adm` aciona "Passar a exigir", then um diálogo informa quantas contas `gestor`/`adm` sem MFA ficarão sem acesso. Nada é enviado ao servidor antes de "Confirmar".
- Given a exigência recém-ligada pelo `adm`, when a sessão do próprio `adm` não tem MFA, then a sessão reflete `empresa.mfaObrigatorio=true` e o bloqueio de navegação da 14.1 passa a valer sem novo login.

## Spec Change Log

## Review Triage Log

### 2026-09-24 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 4 (high 0, medium 0, low 4)
- defer: 0
- reject: 27 (high 0, medium 3, low 24)
- addressed_findings:
  - `[low]` `[patch]` A contagem `contasSemMfa` ficava parada depois de uma alteração. Agora `aplicar` recarrega a exigência junto com o histórico, e o teste confere a segunda leitura.
  - `[low]` `[patch]` "Isso inclui você." aparecia para `adm` em sessão SSO, que o gate nunca bloqueia. Agora exige `origem === 'senha'`, com teste.
  - `[low]` `[patch]` O histórico dizia "MFA desligado por <alvo>". O alvo é a conta afetada, então passou a "MFA desligado de <alvo>", igual a `mfa_resetado`, com teste.
  - `[low]` `[patch]` Faltava teste do caminho de falha do PUT: agora ele confere toast de erro, sessão intacta, histórico não recarregado e estado mantido.

## Design Notes

**Por que contar só contas com senha:** o gate da 14.1 dispara só para sessão `origem=senha`. Uma conta sem `senha_hash` só entra por SSO e nunca é bloqueada. Contá-la tornaria o aviso "ficarão sem acesso" falso.

**Idempotência sem SELECT prévio:** o `WHERE mfa_obrigatorio <> $novo` faz o próprio UPDATE decidir. Duas requisições concorrentes com o mesmo valor produzem exatamente uma linha de auditoria, e o valor anterior é sempre `!novo`.

```go
res, err := tx.Exec(`UPDATE empresas SET mfa_obrigatorio = $2 WHERE id = $1 AND mfa_obrigatorio <> $2`, empresaID, novo)
if n, _ := res.RowsAffected(); n == 0 { return false, tx.Commit() }
detalhe, _ := json.Marshal(map[string]bool{"anterior": !novo, "novo": novo})
_, err = tx.Exec(`INSERT INTO auditoria_seguranca (empresa_id, ator_id, acao, detalhe) VALUES ($1, $2, 'exigencia_alterada', $3)`, empresaID, atorID, detalhe)
```

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: limpo
- `cd backend && DATABASE_URL='postgres://stockflow:stockflow@127.0.0.1:5432/stockflow_t91?sslmode=disable' go test -p 1 -count=1 . ./services/... ./handlers/... ./middleware/...` -- expected: verde
- `cd frontend && npx tsc -b && npx vitest run src/components/seguranca src/lib/segurancaEmpresa.test.ts src/pages/ConfiguracoesPage.test.tsx src/App.test.tsx` -- expected: verde

## Auto Run Result

Status: done

**Resumo:** o `adm` liga ou desliga a exigência de MFA da própria Empresa na nova seção "Dupla autenticação da Empresa" de Configurações, logo abaixo de Segurança.
- **Ligar:** abre uma confirmação com a quantidade de contas `gestor`/`adm` (ativas, com senha e sem MFA) que ficarão sem acesso. Se o próprio `adm` estiver nessa situação por senha, o aviso acrescenta "Isso inclui você.".
- **Desligar:** aplica direto e não toca no MFA de ninguém.
- **Servidor:** faz um UPDATE condicional e grava `auditoria_seguranca` (`exigencia_alterada`, `{anterior, novo}`) na mesma transação, só quando o valor muda. O reenvio do mesmo valor é idempotente (`alterado:false`, sem linha de auditoria).
- **Rotas:** as três ficam atrás de `RequireAuth → RequireRole(adm)`, então papel menor recebe 403 FORBIDDEN, e `adm` sem MFA numa Empresa que exige recebe 403 MFA_SETUP_REQUIRED.
- **Sessão:** depois de alterar, a sessão recebe `empresa.mfaObrigatorio`, e o bloqueio da 14.1 vale sem novo login.
- **Histórico:** escopado por Empresa, mais recente primeiro, com no máximo 200 linhas.

**Arquivos:**
- `backend/migrations/000048_create_auditoria_seguranca.{up,down}.sql`: tabela append-only (CHECK de `acao` já com os valores da 14.4) e índice `(empresa_id, criado_em DESC)`.
- `backend/services/seguranca_empresa.go` (+ teste): `ContarContasSemMFA`, `AlterarExigenciaMFA` e `ListarAuditoriaSeguranca`.
- `backend/handlers/seguranca_empresa.go` (+ teste): GET/PUT `/api/seguranca/mfa-empresa` e GET `/api/seguranca/auditoria`.
- `backend/main.go` / `backend/main_test.go`: registro das rotas e casos "sem token".
- `backend/services/empresas_plataforma_test.go`, `backend/handlers/plataforma_test.go`: limpeza de `auditoria_seguranca` antes de `usuarios`.
- `frontend/src/lib/segurancaEmpresa.ts` (+ teste): cliente das três rotas.
- `frontend/src/components/seguranca/MfaEmpresaSection.tsx` (+ teste): card com estado, confirmação e histórico.
- `frontend/src/pages/ConfiguracoesPage.tsx` (+ teste): a seção montada só para `adm`.

**Revisão:** 4 patches aplicados (todos low), 0 adiados, 27 rejeitados. Motivos da rejeição:
- Append-only sem trigger, limite de 200 sem paginação e confirmação só na UI: decisões da spec.
- A FK para `usuarios` bloquearia hard delete: não existe hard delete em produção, só anonimização.
- Sem proteção no servidor contra o `adm` se trancar: o epic sujeita o `adm` ao gate de propósito.
- Corpo JSON com lixo depois do objeto: mesmo padrão dos outros handlers.
- Estado compartilhado entre pacotes nos testes: a verificação usa `-p 1`, como o resto da suíte.
- Toast quando `alterado:false`: o estado final descrito continua correto.
- Duplo clique em "Confirmar": o `ConfirmDialog` já garante uma única confirmação.
- O restante são itens cosméticos ou pedidos de escopo novo (IP na auditoria, botão "tentar novamente").

**Follow-up recomendado:** false (patches: 0 high, 0 medium, 4 low; score 3×0 + 4 = 4 < 5).

**Verificação:**
- `go build ./...`, `go vet ./...` e `gofmt -l`: limpos, antes e depois dos patches.
- `go test -p 1 -count=1 . ./services/... ./handlers/... ./middleware/...` contra `stockflow_t91`: verde (root 23s, services 267s, handlers 202s, middleware 5s). Os patches só tocaram o frontend, então a suíte do backend não foi repetida depois deles.
- Testes novos do backend rodados com `-v`, todos PASS: `TestContarContasSemMFA`, `TestAlterarExigenciaMFA_LigarDesligarIdempotente`, `TestListarAuditoriaSeguranca_EscopoEOrdem`, `TestExigenciaMFA_{LigarIdempotenteDesligar,ContagemEEfeitoDoGate,PapelInsuficiente,AdmBloqueado,CorpoInvalido,AuditoriaEscopadaPorEmpresa,SemToken}`.
- Frontend: `npx tsc -b` limpo; vitest em `src/components/seguranca`, `src/lib/segurancaEmpresa.test.ts`, `ConfiguracoesPage.test.tsx` e `App.test.tsx` com 92 testes verdes depois dos patches.
- Auditoria da matriz: todas as 10 linhas têm teste que rodou e passou.

**Riscos residuais:**
- O append-only é só por convenção (sem trigger nem REVOKE), como em `logs_acesso`.
- `ator_id`/`alvo_id` têm FK sem `ON DELETE`: um hard delete futuro de usuário que aparece na auditoria vai falhar.
- O histórico mostra no máximo 200 eventos, sem aviso de corte.
- A seção fica num card próprio logo depois de "Segurança", e não dentro dele.
- Os testes antigos de `ConfiguracoesPage` para `adm` não fazem stub das rotas novas e passam porque a seção mostra um alerta.
- Os arquivos do frontend não passaram por Prettier ou ESLint, que não têm config no repositório.
