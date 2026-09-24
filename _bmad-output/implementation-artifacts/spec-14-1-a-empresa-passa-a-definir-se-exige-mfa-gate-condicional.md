---
title: 'Story 14.1 — A Empresa passa a definir se exige MFA (gate condicional)'
type: 'feature'
created: '2026-09-24'
status: 'done'
baseline_revision: '5f0a50ed0686a91c78af7b83f7f83cced9ce1367'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-14-context.md'
warnings: ['oversized']
deferred:
  - summary: >-
      O frontend só lê `empresa.mfaObrigatorio` (e `mfaHabilitado`) quando a sessão começa; uma mudança do flag no meio da sessão não atualiza o bloqueio de navegação nem o rótulo até recarregar.
    evidence: |-
      `/api/auth/me` só é consultado no bootstrap do AuthProvider e nada trata o 403 MFA_SETUP_REQUIRED globalmente. O servidor continua sendo a autoridade (o gate vale na próxima requisição). A falta de refresh do /me é anterior a esta story (papel e MFA também ficam defasados), mas pesa na 14.3, quando o adm passa a alterar o flag.
    location: >-
      frontend/src/lib/auth.tsx
    severity: medium
---

<intent-contract>

## Intent

**Problem:** Hoje o gate `403 MFA_SETUP_REQUIRED` em `middleware.RequireRole` vale para todo `gestor`/`adm` autenticado por senha e sem MFA, em qualquer Empresa. Os sócios e os clientes querem que cada Empresa decida isso.

**Approach:** Criar a coluna `empresas.mfa_obrigatorio BOOLEAN NOT NULL DEFAULT false` (todas as Empresas existentes ficam `false`). O gate único em `RequireRole` ganha uma 4ª condição: a Empresa da requisição, lida do contexto por `EmpresaDaRequisicao`, precisa exigir MFA. `GET /api/auth/me` (e as demais respostas `usuarioResposta`) passa a devolver `empresa.mfaObrigatorio`. O frontend espelha a mesma condição no bloqueio de navegação (`RotaProtegida`) e no rótulo "obrigatório"/"opcional" de Configurações → Segurança.

## Boundaries & Constraints

**Always:**
- O gate vive só em `RequireRole`: rank mínimo da rota >= `gestor` && `Origem == "senha"` && `!MFAHabilitado` && `empresa.MFAObrigatorio`. A ordem se mantém: papel insuficiente (403 FORBIDDEN) vence antes.
- A Empresa vem de `EmpresaDaRequisicao(ctx)`, resolvida por slug a cada requisição (sem cache). Uma mudança do flag vale na próxima requisição, sem novo login.
- Se as três primeiras condições valem e a Empresa não está no contexto, a resposta é 500 INTERNAL_ERROR com `slog.Error`. É erro de composição e nunca abre a rota (fail-closed).
- O pedido do código TOTP no login de conta com `mfa_habilitado=true` continua como está, com ou sem exigência.
- Migração aditiva e reversível (`000047_*.up/down.sql`).
- Frontend: a condição fica num helper único, usado por `App.tsx` e `ConfiguracoesPage`. O campo ausente na resposta equivale a "não exige". O servidor continua sendo a autoridade.

**Block If:** nada previsto.

**Never:**
- Nenhuma tela ou rota para alterar o flag. Isso fica para a 14.2 (cadastro) e a 14.3 (adm).
- Não mexer no MFA do Dono da Plataforma (`/api/plataforma/*`, `RequireDonoPlataforma`). Ele não lê o flag de nenhuma Empresa.
- Não reimplementar o gate em handler ou service. Não cachear a Empresa.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Não exige | gestor/adm, senha, sem MFA, `mfa_obrigatorio=false`, rota gestor+ | handler executa (200) | — |
| Exige | mesma sessão, `mfa_obrigatorio=true` | 403 `MFA_SETUP_REQUIRED` | handler não executa |
| Papel baixo | usuario/almoxarife, rota almoxarife-, Empresa exige | passa | — |
| SSO | `origem=sso`, sem MFA, Empresa exige | passa | — |
| MFA ligado | `mfa_habilitado=true`, Empresa exige | passa | — |
| Flag alterado | UPDATE `mfa_obrigatorio` entre duas requisições do mesmo token | a 2ª requisição reflete o novo valor | — |
| Sem Empresa no ctx | condições 1-3 verdadeiras, contexto sem Empresa | 500 INTERNAL_ERROR | slog.Error |
| Papel insuficiente | usuario em rota gestor+, Empresa exige | 403 FORBIDDEN (não MFA_SETUP_REQUIRED) | — |

</intent-contract>

## Code Map

- `backend/migrations/000046_*.up.sql` -- última migração. A nova é `000047_add_mfa_obrigatorio_to_empresas`, com comentário no cabeçalho no mesmo estilo.
- `backend/services/empresas.go:77` -- `type Empresa`: adicionar `MFAObrigatorio bool` com a tag `json:"mfaObrigatorio"`. `:192` `colunasEmpresa` e `:201` `scanEmpresa` são a projeção única, usada também por `migracao_multi_empresa.go:149` e pelo `RETURNING` de `:338`. Adicionar a coluna ao fim dos dois.
- `backend/middleware/roles.go:64` -- a condição atual do gate. Acrescentar a leitura de `EmpresaDaRequisicao(r.Context())` (mesmo pacote, `empresa.go:69`) e atualizar o doc-comment.
- `backend/middleware/roles_test.go` -- `chamarRequireRole` injeta só o usuário. Estender para injetar a Empresa (`empresaCtxKey`). Os testes de gate existentes (`:154`–`:331`) passam a precisar de uma Empresa com `MFAObrigatorio: true` para continuar esperando MFA_SETUP_REQUIRED. Os testes de composição com RequireAuth (`:257`+) montam o contexto próprio.
- `backend/handlers/auth.go:216` -- `usuarioResposta`/`usuarioRespostaDe`: adicionar `Empresa *empresaResposta` com a tag `json:"empresa,omitempty"`, contendo `{MfaObrigatorio bool json:"mfaObrigatorio"}`, preenchido quando `EmpresaDaRequisicao` resolve. `MeHandler` fica em `:606`.
- `backend/handlers/empresa_teste_test.go:87` -- `comEmpresa`/`garantirEmpresaTeste`: helpers para os testes de handler com Empresa real (slug `slugEmpresaTeste`).
- `backend/handlers/usuarios_test.go:62`, `promocao_test.go:386` -- ligam o MFA para escapar do gate. Continuam válidos e não precisam mudar.
- `backend/main.go:320-352` -- toda rota com RequireRole está sob `registrar`, que aplica `RequireEmpresa`. As rotas `/api/plataforma/*` usam `requireDono` e ficam fora. Só leitura.
- `frontend/src/lib/auth.tsx:32` -- `UsuarioSessao`: adicionar `empresa?: { mfaObrigatorio: boolean }` e exportar o helper `mfaSetupPendente(usuario)`.
- `frontend/src/App.tsx:94` -- `mfaPendente` em `RotaProtegida` deve usar o helper.
- `frontend/src/pages/ConfiguracoesPage.tsx:130` -- `mfaObrigatorio` em `SegurancaCard` deve usar o helper. Atualizar o doc-comment de `:108`.
- `frontend/src/App.test.tsx:132,153`, `frontend/src/pages/ConfiguracoesPage.test.tsx:454` -- testes que dependem do gate. Passar `empresa: { mfaObrigatorio: true }` (e o `authState` equivalente) e acrescentar os casos "Empresa não exige".

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000047_add_mfa_obrigatorio_to_empresas.up.sql` / `.down.sql` -- `ALTER TABLE empresas ADD COLUMN mfa_obrigatorio BOOLEAN NOT NULL DEFAULT false`. O down faz `DROP COLUMN` -- AC de migração.
- `backend/services/empresas.go` -- adicionar o campo `MFAObrigatorio`, a coluna em `colunasEmpresa` e o scan correspondente -- para que o middleware leia o flag.
- `backend/services/empresas_test.go` -- teste: uma Empresa provisionada nasce com `MFAObrigatorio=false`, e `BuscarEmpresaPorSlug` reflete um UPDATE para `true` -- prova o default e a leitura.
- `backend/middleware/roles.go` -- adicionar a 4ª condição e o 500 quando a Empresa falta no contexto. Atualizar o comentário.
- `backend/middleware/roles_test.go` -- adaptar o helper, os testes existentes e os casos da matriz (não exige → passa; exige → 403; sem Empresa → 500; SSO/MFA ligado/papel baixo não disparam com a Empresa exigindo).
- `backend/handlers/auth.go` -- adicionar `empresa.mfaObrigatorio` à `usuarioResposta`.
- `backend/handlers/auth_test.go` (ou arquivo de teste existente de `MeHandler`) -- teste de integração com Empresa real: `/me` devolve `empresa.mfaObrigatorio`. Com `RequireEmpresa`+`RequireAuth`+`RequireRole(gestor)`, gestor por senha sem MFA recebe 200 com a flag false. Depois de um UPDATE para true, o mesmo token recebe 403 MFA_SETUP_REQUIRED. Restaurar a flag ao final.
- `frontend/src/lib/auth.tsx` -- adicionar o tipo e o helper `mfaSetupPendente`.
- `frontend/src/App.tsx`, `frontend/src/pages/ConfiguracoesPage.tsx` -- usar o helper e atualizar os comentários.
- `frontend/src/App.test.tsx`, `frontend/src/pages/ConfiguracoesPage.test.tsx` -- adaptar os testes e adicionar os casos "Empresa não exige": sem redirect e com rótulo "Opcional".

**Acceptance Criteria:**
- Given o banco com Empresas existentes, when a migração 000047 roda, then todas ficam `mfa_obrigatorio=false` e ninguém é bloqueado.
- Given `GET /api/auth/me`, when o frontend consulta, then a resposta traz `empresa.mfaObrigatorio`. `RotaProtegida` só redireciona para `/configuracoes` quando a sessão é por senha, o papel é >= gestor, a conta não tem MFA e a Empresa exige. Configurações → Segurança mostra "Obrigatório para o seu papel…" nesse caso e "Opcional para o seu papel." nos demais.
- Given uma conta com `mfa_habilitado=true`, when faz login por senha, then o código TOTP continua sendo pedido (fluxo de login inalterado).
- Given o Dono da Plataforma, when faz login, then o TOTP continua obrigatório (rotas `/api/plataforma/*` intocadas).

## Spec Change Log

## Review Triage Log

### 2026-09-24 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 1 (high 0, medium 0, low 1)
- defer: 1 (high 0, medium 1, low 0)
- reject: 16 (high 0, medium 3, low 13)
- addressed_findings:
  - `[low]` `[patch]` Os testes de handler da 14.1 (`TestMeHandler_ExpoeEmpresaMfaObrigatorio`, `TestRequireRole_GateCondicionalAEmpresa`) agora zeram `mfa_obrigatorio` da Empresa compartilhada no início. Assim uma execução abortada antes do `t.Cleanup` não deixa a suíte herdar `true`.

## Design Notes

A resposta de `/me` hoje é plana (`empresaNome`, `ambienteTreinamento`). O AC pede literalmente `empresa.mfaObrigatorio`, então entra um objeto aninhado `empresa` só com esse campo, sem mover os campos planos existentes. As stories seguintes podem pendurar mais metadados da Empresa ali.

Esboço do gate:

```go
if rankMinimo >= services.RankPapel(services.PapelGestor) && usuario.Origem == "senha" && !usuario.MFAHabilitado {
    empresa, ok := EmpresaDaRequisicao(r.Context())
    if !ok { /* slog.Error + 500 INTERNAL_ERROR */ }
    if empresa.MFAObrigatorio { /* 403 MFA_SETUP_REQUIRED */ }
}
```

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: limpo
- `cd backend && DATABASE_URL='postgres://stockflow:stockflow@127.0.0.1:5432/stockflow_t91?sslmode=disable' go test -p 1 -count=1 ./middleware/... && go test -p 1 -count=1 -run 'Empresa|Me|MFA|Auth|Role' ./services/... ./handlers/...` -- expected: verde
- `cd frontend && npx tsc -b && npx vitest run src/App.test.tsx src/pages/ConfiguracoesPage.test.tsx src/lib/auth.test.tsx` -- expected: verde

## Auto Run Result

Status: done

**Resumo:** `empresas.mfa_obrigatorio` (NOT NULL DEFAULT false, migração 000047) passa a decidir se o gate `403 MFA_SETUP_REQUIRED` de `RequireRole` dispara. O gate exige papel >= gestor, sessão por senha, conta sem MFA e Empresa que exige. A Empresa é relida por requisição, sem cache. Se a Empresa falta no contexto dentro do gate, a resposta é 500 (fail-closed). `usuarioResposta` (`/me`, login, verificação de MFA, SSO) ganha `empresa.mfaObrigatorio`. O frontend espelha a regra no helper único `mfaSetupPendente`, usado pelo bloqueio de navegação (`RotaProtegida`) e pelo rótulo "obrigatório"/"opcional" de Segurança. O fluxo de login com TOTP e as rotas do Dono da Plataforma não foram tocados.

**Arquivos:**
- `backend/migrations/000047_add_mfa_obrigatorio_to_empresas.{up,down}.sql`: coluna nova, reversível.
- `backend/services/empresas.go`: campo `MFAObrigatorio`, coluna em `colunasEmpresa`/`scanEmpresa`.
- `backend/services/empresas_test.go`: default false e leitura depois de UPDATE.
- `backend/middleware/roles.go`: 4ª condição do gate e 500 sem Empresa.
- `backend/middleware/roles_test.go`: helper com Empresa; casos da matriz; composição real RequireEmpresa→RequireAuth→RequireRole alternando o flag.
- `backend/handlers/auth.go`: `empresa.mfaObrigatorio` na resposta de usuário.
- `backend/handlers/auth_mfa_test.go`: `/me` expõe o flag; o mesmo token passa de 200 a 403 quando o flag vira true.
- `frontend/src/lib/auth.tsx` (+ teste): tipo `empresa?` e helper `mfaSetupPendente`.
- `frontend/src/App.tsx`, `frontend/src/pages/ConfiguracoesPage.tsx` (+ testes): usam o helper; casos "Empresa não exige".

**Revisão:** 1 patch aplicado (low), 1 item adiado (medium: refresh do `/me` no meio da sessão, relevante para a 14.3), 16 rejeitados. Motivos da rejeição:
- O default false para as Empresas existentes é exigência explícita do AC e da AD-35.
- A herança pelo Treinamento é da 14.2.
- A auditoria de mudanças é da 14.3.
- O CI roda `go test -p 1`.
- O CNPJ do teste é a base de 12 dígitos do helper.
- `nav-items` não importa `lib/auth`, então não há ciclo de import.
- O 500 condicional é o desenho da spec.
- O login usa a mesma `usuarioRespostaDe`.
- O restante são itens cosméticos ou de cobertura redundante.

**Follow-up recomendado:** false (patches: 0 high, 0 medium, 1 low; score 3×0 + 1 = 1 < 5).

**Verificação:**
- `go build ./...` e `go vet ./...`: limpos.
- `go test -p 1 -count=1 ./middleware/...` e `-run 'Empresa|Me|MFA|Auth|Role' ./services/... ./handlers/...` contra `stockflow_t91`: verdes, antes e depois do patch.
- O subagente de implementação também rodou a suíte backend completa (`go test -p 1 ./...`), verde.
- Frontend: `npx tsc -b` limpo; vitest em `App.test.tsx`, `ConfiguracoesPage.test.tsx` e `auth.test.tsx` com 89 testes verdes. O subagente rodou a suíte completa: 59 arquivos, 782 testes.
- Auditoria da matriz: todas as linhas têm teste que rodou e passou.

**Riscos residuais:**
- Depois do deploy nenhuma Empresa exige MFA até a 14.2/14.3 permitir ligar o flag. É intencional (AD-35), mas é um relaxamento em relação à Story 1.11.
- O frontend só reflete mudanças do flag depois de recarregar (item adiado).
- Uma rota gestor+ registrada fora de `RequireEmpresa` passaria a responder 500 a quem cai no gate. Hoje nenhuma rota está nessa situação.

