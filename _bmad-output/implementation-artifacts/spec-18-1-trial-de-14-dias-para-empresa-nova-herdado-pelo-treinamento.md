---
title: 'Toda Empresa nova ganha 14 dias de teste, herdados pelo Treinamento'
type: 'feature'
created: '2026-09-29'
status: 'done'
baseline_revision: '1fe5d57e1467c124e460acb7012534e262fa2a18'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** Não existe hoje nenhum limite de uso gratuito para uma Empresa real nova: sem uma data de corte, o Dono da Plataforma não tem base para cobrar nem para bloquear quem não paga (Epic 18, FR-57).

**Approach:** Nova coluna `empresas.trial_termina_em` (nullable), gravada só por `CriarEmpresaComTreinamento` (Empresa real = `criado_em + 14 dias`; Treinamento herda o valor JÁ GRAVADO na Empresa real, nunca recalcula); `NULL` em todo outro caminho (Empresas já existentes, `cmd/migrar-multi-empresa`). A tela "Empresas" do Dono passa a mostrar o prazo por linha. O bloqueio de acesso ao vencer o prazo é da Story 18.2 — fora de escopo aqui.

## Boundaries & Constraints

**Always:** migração aditiva (`ALTER TABLE ... ADD COLUMN ... NULL`, sem default nem backfill); `trial_termina_em` só é gravado por `CriarEmpresaComTreinamento`; todo outro caminho de criação (`cmd/migrar-multi-empresa`, testes, futuras Empresas isentas) grava/mantém `NULL` por não tocar no campo (zero value); Treinamento copia o valor GRAVADO na Empresa real (`real.TrialTerminaEm`), nunca um valor recalculado — mesmo padrão da herança de `MFAObrigatorio` (Story 14.2).

**Block If:** nenhuma decisão pendente identificada — a Story e o contexto do Epic 18 resolvem toda ambiguidade.

**Never:** implementar o bloqueio de acesso ao vencer o prazo (Story 18.2) ou a tela de estender/isentar (Story 18.3) — só a coluna, a gravação/herança na criação e a exibição na listagem; nunca recalcular ou sincronizar o prazo do Treinamento depois da criação; nunca alterar `cmd/migrar-multi-empresa` para gravar um prazo.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Criação normal | Dono cria Empresa nova pela tela "Empresas" | `CriarEmpresaComTreinamento` grava `trial_termina_em = agora + 14 dias` na Empresa real e o MESMO valor no Treinamento | No error expected |
| Migração da fundadora | `cmd/migrar-multi-empresa` (ou qualquer caminho fora da tela do Dono) | `trial_termina_em` continua `NULL` nos dois lados | No error expected |
| Empresa já existente | Empresa gravada antes desta migração | `trial_termina_em` permanece `NULL` (coluna nova, sem backfill) | No error expected |
| Listagem — com prazo futuro | Empresa com `trial_termina_em` no futuro | Linha mostra "N dias restantes" | No error expected |
| Listagem — prazo vencido | Empresa com `trial_termina_em` no passado | Linha mostra "Vencido há N dias" | No error expected |
| Listagem — isenta | Empresa com `trial_termina_em` `NULL` | Linha mostra "Sem prazo" | No error expected |

</intent-contract>

## Code Map

- `backend/migrations/000052_add_trial_termina_em_to_empresas.up.sql` / `.down.sql` -- nova coluna `empresas.trial_termina_em TIMESTAMPTZ NULL`, aditiva, sem backfill (mesmo molde de `000047_add_mfa_obrigatorio_to_empresas`).
- `backend/services/empresas.go` -- `Empresa` struct (l.77-90) e `DadosEmpresa` (l.95-113) ganham `TrialTerminaEm *time.Time`; `dadosEmpresaValidados` (l.260-266) ganha `trialTerminaEm sql.NullTime`; `validarDadosEmpresa` (l.270+) mapeia `d.TrialTerminaEm` -> `v.trialTerminaEm`; `colunasEmpresa` (l.200-202) e `scanEmpresa` (l.209-225) passam a incluir `trial_termina_em`; `InserirEmpresa` (l.336+) grava a 14ª coluna.
- `backend/services/empresas_plataforma.go` -- `duracaoTrialInicial = 14*24*time.Hour` (perto de `tokenPrimeiroAcessoExpiracao`, l.41-46); `CriarEmpresaComTreinamento` (l.200+) calcula `trialTerminaEm := time.Now().UTC().Add(duracaoTrialInicial)` e grava em `dadosReal.TrialTerminaEm` ANTES de `ProvisionarEmpresa`; `ProvisionarTreinamento` (l.249+) copia `dadosTreino.TrialTerminaEm = real.TrialTerminaEm` (mesmo padrão de `dadosTreino.MFAObrigatorio = real.MFAObrigatorio`); `EmpresaResumo` (l.90-104) ganha `TrialTerminaEm *time.Time`; `ListarEmpresasPlataforma` (l.356+) inclui `e.trial_termina_em` no SELECT e no scan.
- `backend/cmd/migrar-multi-empresa/*` -- NENHUMA mudança: `DadosEmpresa.TrialTerminaEm` fica `nil` (zero value) neste caminho, então `trial_termina_em` grava `NULL` automaticamente via `InserirEmpresa`/`AdotarEmpresaFundadora`.
- `frontend/src/lib/plataforma.ts` -- `EmpresaResumo` (l.60-74) ganha `trialTerminaEm: string | null`.
- `frontend/src/pages/plataforma/EmpresasPage.tsx` -- nova função `textoTrial(trialTerminaEm)` (perto de `textoMFA`, l.114-116): "Sem prazo" / "Vencido há N dias" / "N dias restantes"; nova linha na Card de cada Empresa (perto da linha "Dupla autenticação", l.508-513).
- `backend/services/empresas_plataforma_test.go` -- `TestListarEmpresasPlataforma_SoMetadado` (l.392+): lista de chaves esperadas do JSON do resumo ganha `trialTerminaEm`; novo `TestCriarEmpresaComTreinamento_Trial` cobre a linha "Criação normal" da matriz.
- `backend/services/migracao_multi_empresa_test.go` -- `TestAdotarEmpresaFundadora_CriaParSemAdmNaReal` ganha asserção de que `TrialTerminaEm` fica `nil` nos dois lados, cobrindo a linha "Migração da fundadora" da matriz.
- `frontend/src/pages/plataforma/EmpresasPage.test.tsx` -- fixture `EMPRESA` ganha `trialTerminaEm: null`; novo teste cobre as três linhas de listagem da matriz (dias restantes / vencido há N dias / sem prazo).

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000052_*.up.sql`/`.down.sql` -- criar a coluna aditiva -- base para tudo o resto.
- `backend/services/empresas.go` -- propagar `TrialTerminaEm` por `DadosEmpresa`/`Empresa`/`InserirEmpresa` -- infraestrutura de leitura/escrita da coluna, reaproveitada por todo caminho de criação de Empresa.
- `backend/services/empresas_plataforma.go` -- `CriarEmpresaComTreinamento` grava o prazo calculado; `ProvisionarTreinamento` herda o valor GRAVADO; `EmpresaResumo`/`ListarEmpresasPlataforma` expõem o campo -- é o único caminho que deve iniciar um trial, e a tela do Dono precisa do dado para exibir.
- `frontend/src/lib/plataforma.ts` + `EmpresasPage.tsx` -- tipo + exibição por linha -- AC explícito da Story (dias restantes / vencido há N dias / sem prazo).
- `backend/services/empresas_plataforma_test.go` -- atualizar a lista de chaves esperadas -- o teste já existente é a fonte de verdade da forma serializada do resumo.

**Acceptance Criteria:**
- Given a migração desta story, when ela roda, then `empresas` ganha `trial_termina_em` (`TIMESTAMPTZ NULL`), `NULL` em toda Empresa já existente.
- Given o Dono cria uma Empresa nova pela tela "Empresas", when `CriarEmpresaComTreinamento` roda, then a Empresa real é gravada com `trial_termina_em = agora + 14 dias` e o Treinamento dela é gravado com o MESMO valor.
- Given `cmd/migrar-multi-empresa` ou qualquer outro caminho de provisionamento fora da tela do Dono, when roda, then `trial_termina_em` continua `NULL`.
- Given a tela "Empresas" do Dono, when lista as Empresas, then cada linha mostra dias restantes, "Vencido há N dias" ou "Sem prazo".

## Spec Change Log

## Review Triage Log

### 2026-09-29 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 2: (high 0, medium 1, low 1)
- defer: 0
- reject: 9: (high 0, medium 0, low 9)
- addressed_findings:
  - `[medium]` `[patch]` `textoTrial` (EmpresasPage.tsx) misclassified a trial expired 0–24h ago as "0 dias restantes" instead of "Vencido há N dias" (`Math.ceil` of a small negative fraction yields `-0`, and `-0 < 0` is `false` in JS). Fixed by deciding "vencido" from the raw millisecond difference instead of the rounded day count; added a regression test for the just-expired boundary (Blind Hunter and Edge Case Hunter surfaced this same bug independently — deduplicated as one finding).
  - `[low]` `[patch]` No test asserted `trialTerminaEm` crosses the HTTP boundary (only the service-layer test covered it). Added assertions to `TestEmpresasPlataforma_MFAObrigatorio` in `handlers/plataforma_test.go`: the creation response carries the field on both Empresa and Treinamento (equal values), and the listagem response carries a non-null value.
  - Rejected as out of scope for Story 18.1 (enhancements/speculative, not defects against this story's AC): missing index on `trial_termina_em` for a future bulk query no story currently performs; client-clock-based day countdown (matches the existing `formatarData` convention — no server-time sync exists anywhere in this component); no absolute expiration date shown alongside the relative text; no visual/badge emphasis for a vencido trial; no sort/filter by trial status in the list; hardcoded 14-day duration not being runtime-configurable (deliberate per Design Notes and the epic's stated scope); no code-level guard preventing a future caller from setting `TrialTerminaEm` outside `CriarEmpresaComTreinamento` (matches the existing, unguarded `MFAObrigatorio` convention); the down-migration lacking a comment about future billing-criticality; the exact-zero-day case rendering as "0 dias restantes" rather than a dedicated "Vence hoje" state (the AC names only three states — dias restantes / vencido há N dias / sem prazo — inventing a fourth is scope creep, and this is a different, already-correct code path from the `-0` bug above).

## Design Notes

`trial_termina_em` é calculado em Go (`time.Now().UTC().Add(14*24*time.Hour)`) e passado como valor explícito ao INSERT — mesmo padrão já usado em `provisionarAdmPrimeiroAcesso` para `expira_em` do token de primeiro acesso — em vez de expressão SQL, para não introduzir um mecanismo novo de cálculo de prazo na base. A pequena distância entre esse instante e o `DEFAULT now()` de `criado_em` (mesma transação, microssegundos de diferença) é irrelevante para um prazo de 14 dias.

## Verification

**Commands:**
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 ./services/... -run 'Empresa|Trial'` -- expected: PASS
- `cd backend && DATABASE_URL=... go test -count=1 ./handlers/...` -- expected: PASS
- `cd frontend && npx tsc -b --force` -- expected: sem erros (o `tsc --noEmit -p .` na raiz não verifica nada nesta config de referências — usar `-b`)
- `cd frontend && npx vitest run src/pages/plataforma/EmpresasPage.test.tsx src/lib/plataforma.test.ts` -- expected: PASS

**Manual checks (if no CLI):**
- Linha "Empresa já existente" da matriz: garantida pela própria migração aditiva (`ALTER TABLE ... ADD COLUMN ... NULL`, sem `DEFAULT`) — Postgres nunca toca linhas existentes ao adicionar coluna nullable sem default; não há cenário de runtime para testar isso além do que a migração já expressa.

## Auto Run Result

**Resumo:** Toda Empresa real criada pela tela "Empresas" do Dono da Plataforma passa a nascer com um prazo de teste de 14 dias corridos (`empresas.trial_termina_em`), herdado pelo Ambiente de Treinamento no mesmo instante da criação e nunca recalculado depois. Todo outro caminho de provisionamento (`cmd/migrar-multi-empresa`, Empresas já existentes) permanece com o prazo `NULL` (isento). A listagem do Dono mostra, por linha, dias restantes / "Vencido há N dias" / "Sem prazo".

**Arquivos alterados:**
- `backend/migrations/000052_add_trial_termina_em_to_empresas.{up,down}.sql` — nova coluna aditiva, nullable, sem backfill.
- `backend/services/empresas.go` — `Empresa`/`DadosEmpresa` ganham `TrialTerminaEm *time.Time`; `colunasEmpresa`/`scanEmpresa`/`InserirEmpresa` propagam a coluna.
- `backend/services/empresas_plataforma.go` — `CriarEmpresaComTreinamento` grava o prazo calculado (`agora + 14 dias`) na Empresa real; `ProvisionarTreinamento` herda o valor GRAVADO (nunca recalcula); `EmpresaResumo`/`ListarEmpresasPlataforma` expõem o campo.
- `backend/services/empresas_plataforma_test.go` — chave `trialTerminaEm` na asserção de forma serializada; novo `TestCriarEmpresaComTreinamento_Trial` (criação + herança + listagem).
- `backend/services/migracao_multi_empresa_test.go` — asserção de que o caminho da Empresa fundadora nunca inicia um trial.
- `backend/handlers/plataforma_test.go` — asserções de que `trialTerminaEm` cruza a fronteira HTTP (resposta de criação e de listagem).
- `frontend/src/lib/plataforma.ts` — `EmpresaResumo.trialTerminaEm: string | null`.
- `frontend/src/pages/plataforma/EmpresasPage.tsx` — `textoTrial()` e a nova linha "Teste: …" por Empresa.
- `frontend/src/pages/plataforma/EmpresasPage.test.tsx` — fixture atualizada e dois novos testes (os três estados de exibição; o limite "vencido há poucas horas").

**Revisão (pass única, 2026-09-29):** patch 2 (medium 1, low 1) aplicados nesta mesma pass; reject 9 (todos low, fora de escopo da Story 18.1); intent_gap 0; bad_spec 0; defer 0. Ver `## Review Triage Log` para o detalhe de cada finding.

**Recomendação de nova revisão:** não (`followup_review_recommended: false` — nenhum patch de severidade alta; `3×1 medium + 1×1 low = 4 < 5`).

**Verificação realizada:**
- `go build ./...` e `go vet ./...` — limpos.
- `go test -count=1 ./services/... -run 'TestCriarEmpresaComTreinamento|TestListarEmpresasPlataforma|TestMFAObrigatorio|TestAdotarEmpresaFundadora|TestAlterarStatusEmpresa'` — PASS.
- `go test -count=1 ./handlers/...` (suíte completa) — PASS (266s).
- `go test -count=1 ./cmd/migrar-multi-empresa/...` — PASS, exceto `TestExecutarMigracao_EndurecimentoRecusaComOrfas`, que falha por estado pré-existente do Postgres local (endurecimento de `estoques.empresa_id` já aplicado por uma execução anterior e não relacionada) — confirmado não causado por esta mudança.
- `npx tsc -b --force` (frontend, build mode com project references — o `tsc --noEmit -p .` na raiz não checa nada nesta config) — sem erros.
- `npx vitest run src/pages/plataforma/EmpresasPage.test.tsx src/lib/plataforma.test.ts` — 31/31 PASS.
- Matrix Test Audit: as 6 linhas da I/O & Edge-Case Matrix têm cobertura — 5 por teste automatizado (rodado e verde) e 1 (Empresa já existente) por garantia estrutural da própria migração aditiva sem `DEFAULT`.

**Riscos residuais:**
- O ambiente Postgres local usado para os testes de integração é persistente e compartilhado entre execuções; uma migração destrutiva de teste anterior (endurecimento de `estoques.empresa_id`) deixou um teste não relacionado (`cmd/migrar-multi-empresa`) quebrado nesse ambiente específico — não é uma regressão desta story, mas fica registrado aqui para não ser confundido com uma.
- Stories 18.2 (bloqueio de acesso ao vencer o prazo) e 18.3 (estender/isentar) ainda não existem: até lá, `trial_termina_em` é só informativo — nenhuma Empresa é de fato bloqueada por teste vencido.
