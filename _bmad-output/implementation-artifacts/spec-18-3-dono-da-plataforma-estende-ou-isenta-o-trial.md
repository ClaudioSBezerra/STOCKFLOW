---
title: 'Dono da Plataforma estende ou isenta o prazo de teste de uma Empresa'
type: 'feature'
created: '2026-09-29'
status: 'done'
baseline_revision: '4a8205f2d029c62f02a15453d6ea11bdef7c930b'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred:
  - summary: >-
      `alterarTrial` (EmpresasPage.tsx) chama `recarregar()` dentro do mesmo
      `try` da escrita — se a escrita tiver sucesso mas o `recarregar()`
      (GET) falhar por motivo transitório, o usuário vê erro mesmo com a
      ação já aplicada no servidor.
    evidence: |-
      Mesmo padrão já existente em `alterarStatus` (não introduzido por esta
      story) — risco pré-existente no arquivo, não causado pela Story 18.3,
      só reaproveitado por ela.
    location: >-
      frontend/src/pages/plataforma/EmpresasPage.tsx (alterarTrial/alterarStatus)
    severity: low
---

<intent-contract>

## Intent

**Problem:** Desde a Story 18.2, uma Empresa cujo `trial_termina_em` venceu fica bloqueada por completo e não existe hoje nenhuma forma de reverter isso — nem estender o prazo nem isentar a Empresa (Epic 18, Story 18.3).

**Approach:** Duas novas rotas de ação, restritas ao Dono da Plataforma, sob `/api/plataforma/empresas/{id}/trial/...`: `extensao` (soma N dias a `now()`, nunca ao prazo antigo) e `isencao` (grava `NULL`). `{id}` é tratado como uma linha qualquer de `empresas` — a mesma rota serve tanto a Empresa real quanto o seu Treinamento, cada um com seu próprio `id`, sem sincronizar os dois. A tela "Empresas" ganha, por linha (Empresa e, se existir, Treinamento), a ação de estender/isentar o teste.

## Boundaries & Constraints

**Always:** o `UPDATE` atinge só a linha de `{id}` (nunca o par via `empresa_origem_id`, ao contrário de `AlterarStatusEmpresa`) — estender/isentar a Empresa real nunca muda o Treinamento e vice-versa; "estender N dias" é sempre `time.Now().Add(N * 24h)`, nunca `trial_termina_em antigo + N dias`; toda rota fica atrás de `middleware.RequireDonoPlataforma` (`requireDono`, já registrado em `main.go`); `dias` deve ser inteiro positivo (`> 0`) ou 400 `VALIDATION_ERROR`; `{id}` inexistente/malformado -> 404 `NOT_FOUND` (nunca 500), mesmo padrão de `alterarStatusEmpresaHandler`; "Isentar" no frontend exige confirmação (`ConfirmDialog`), mesmo padrão de "Desativar" — remove o prazo sem guardar o valor anterior.

**Block If:** nenhuma decisão pendente identificada — o contexto do Epic 18 e a Story 18.2 (já implementada) resolvem toda ambiguidade.

**Never:** sincronizar o prazo da Empresa real com o do Treinamento (ação sempre independente, um `id` por chamada); recalcular a partir do prazo antigo ao estender; qualquer efeito automático/lote sobre mais de uma Empresa; cobrança automática ou fluxo de assinatura (fora de escopo do Epic); expor as rotas fora de `middleware.RequireDonoPlataforma`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Estender Empresa vencida | `POST .../empresas/{id-real}/trial/extensao` `{"dias":30}` | 200, `trial_termina_em` da linha = `now()+30d`; Treinamento inalterado | Nenhum erro |
| Estender Treinamento isolado | `POST .../empresas/{id-treino}/trial/extensao` `{"dias":10}` | 200, só a linha do Treinamento muda | Nenhum erro |
| Isentar | `POST .../empresas/{id}/trial/isencao` (sem corpo) | 200, `trial_termina_em` = `NULL` | Nenhum erro |
| `dias` inválido | `{"dias":0}`, `{"dias":-5}`, ausente, ou não-inteiro | Nada é gravado | 400 `VALIDATION_ERROR` |
| `{id}` inexistente/malformado | qualquer uma das duas rotas | Nada é gravado | 404 `NOT_FOUND` |
| Empresa antes bloqueada (402) | logo após `extensao` ou `isencao` bem-sucedida | próxima requisição sob `/e/{slug}/...` passa a responder normalmente (via `RequireEmpresa`, Story 18.2, sem mudança nele) | Nenhum erro |

</intent-contract>

## Code Map

- `backend/services/empresas_plataforma.go` -- nova função `AtualizarTrialEmpresa(db *sql.DB, empresaID string, trialTerminaEm *time.Time) error`, ao lado de `AlterarStatusEmpresa` (linhas ~442-502): `UPDATE empresas SET trial_termina_em = $2 WHERE id = $1 RETURNING id` (single-row, sem o `WHERE (id=$1 AND empresa_origem_id IS NULL) OR empresa_origem_id=$1` do fan-out de status); `sql.ErrNoRows` ou `pq.Error` código `pqInvalidTextRepresentation` (constante já existe no arquivo) -> `ErrEmpresaNaoEncontrada` (já existe).
- `backend/services/empresas_plataforma.go:373-432` (`ListarEmpresasPlataforma`) e struct `TreinamentoResumo` (~linha 84-91) -- adicionar `t.trial_termina_em` ao `SELECT` (hoje só lê `e.trial_termina_em`), novo campo `TrialTerminaEm *time.Time` em `TreinamentoResumo`, `sql.NullTime` extra no scan (~linha 403-410), preenchido igual ao já existente para `e.TrialTerminaEm`.
- `backend/handlers/empresas.go` -- duas novas funções junto de `DesativarEmpresaHandler`/`ReativarEmpresaHandler` (linhas ~153-188): `EstenderTrialHandler(db)` (decodifica `{"dias": int}`, valida `> 0`, chama `AtualizarTrialEmpresa` com `time.Now().Add(dias*24h)`) e `IsentarTrialHandler(db)` (chama `AtualizarTrialEmpresa` com `nil`, sem corpo). Adicionar import `"time"`. Seguir exatamente o switch de erros de `alterarStatusEmpresaHandler` (linhas 166-188): `nil` -> 200, `ErrEmpresaNaoEncontrada` -> 404 `NOT_FOUND`, resto -> 500 `INTERNAL_ERROR`; payload inválido/`dias<=0` -> 400 `VALIDATION_ERROR` (mesmo padrão de `empresas.go:80/85/119`).
- `backend/main.go:392-395` -- duas novas linhas após a de `reativacao`: `mux.HandleFunc("POST /api/plataforma/empresas/{id}/trial/extensao", requireDono(handlers.EstenderTrialHandler(db)))` e `.../trial/isencao` com `handlers.IsentarTrialHandler(db)`.
- `backend/handlers/plataforma_test.go:70-79` (`muxPlataformaTeste`) -- registrar as mesmas duas rotas novas, mesmo padrão das quatro já existentes.
- `backend/services/empresas_plataforma_test.go` -- novos testes ao lado de `TestAlterarStatusEmpresa_DesativaEReativaOPar` (linha ~567) e `TestAlterarStatusEmpresa_IdNaoEncontrado` (linha ~631): `TestAtualizarTrialEmpresa_EstendeSoAEmpresa` (real e Treinamento têm `id`s distintos; estender um não muda o outro), `TestAtualizarTrialEmpresa_Isenta`, `TestAtualizarTrialEmpresa_IdNaoEncontrado`.
- `backend/handlers/plataforma_test.go` -- novo teste `TestEmpresasPlataforma_EstenderIsentarTrial`, molde em `TestEmpresasPlataforma_MFAObrigatorio` (linha ~472) e `TestEmpresasPlataforma_CriarListarDesativarReativar` (linha ~331): cobre as 6 linhas da matriz, inclusive 400 (`dias` inválido) e 404 (`id` malformado), usando `codigoDeErro(t, w)` já existente.
- `frontend/src/lib/plataforma.ts:60-76` (`EmpresaResumo.treinamento`) -- acrescentar `trialTerminaEm: string | null` ao tipo inline do Treinamento (hoje só tem `id, slug, status, mfaObrigatorio`). Após `acaoDeStatus`/`desativarEmpresa`/`reativarEmpresa` (linhas 215-232): nova `async function acaoDeTrial(id, acao: 'extensao' | 'isencao', dias?: number)` (mesmo `requisitar`, body JSON só quando `dias` é passado) e exports `estenderTrial(id, dias)` / `isentarTrial(id)`.
- `frontend/src/pages/plataforma/EmpresasPage.tsx:535` -- linha "Teste: ..." passa a incluir o Treinamento: `` `${textoTrial(empresa.trialTerminaEm)}${empresa.treinamento ? ` · Treinamento: ${textoTrial(empresa.treinamento.trialTerminaEm)}` : ''}` `` (mesma função `textoTrial`, já genérica, linhas 118-137, sem alteração nela). Ações (bloco `shrink-0`, linhas 540-562): para a Empresa e, se existir, para o Treinamento, um botão "Estender teste" (abre diálogo com `<Input type="number">` `dias`, mesmo padrão de formulário de `frontend/src/components/produtos/EditarProdutoDialog.tsx`) e um botão "Isentar teste" (abre `ConfirmDialog`, mesmo padrão de `paraDesativar`/linhas 572-589). Handler novo `alterarTrial` ao lado de `alterarStatus` (linhas 284-304), mesmo padrão try/catch/`sessaoEncerrada`/`recarregar()`.
- `frontend/src/pages/plataforma/EmpresasPage.test.tsx` -- estender ao lado dos testes "Story 18.1" (linhas 232-253): mostrar trial do Treinamento na mesma linha; estender grava `dias` correto no corpo do POST (molde: linha ~217, `chamadas('POST', '/api/plataforma/empresas')`); isentar exige confirmação e envia `POST .../trial/isencao`; 404/400 do servidor aparecem como erro (sem toast de sucesso), mesmo padrão do teste "409" (linha ~256).

## Tasks & Acceptance

**Execution:**
- `backend/services/empresas_plataforma.go` -- adicionar `AtualizarTrialEmpresa` (single-row) e o campo/`SELECT`/scan de `t.trial_termina_em` em `ListarEmpresasPlataforma`/`TreinamentoResumo` -- é a única forma de estender/isentar cada linha de forma independente e de a listagem expor o prazo do Treinamento para a UI agir sobre ele.
- `backend/handlers/empresas.go` + `backend/main.go` + `backend/handlers/plataforma_test.go` -- dois novos handlers e duas novas rotas, atrás de `requireDono` -- reaproveita o único ponto de autorização do Dono já existente.
- `backend/services/empresas_plataforma_test.go`, `backend/handlers/plataforma_test.go` -- cobrem toda a matriz de I/O no nível de serviço e de HTTP.
- `frontend/src/lib/plataforma.ts` -- tipo + `estenderTrial`/`isentarTrial` -- espelha `desativarEmpresa`/`reativarEmpresa` já existentes.
- `frontend/src/pages/plataforma/EmpresasPage.tsx` -- exibe o trial do Treinamento e adiciona as ações de estender/isentar para Empresa e Treinamento, cada uma agindo só sobre o seu próprio `id`.
- `frontend/src/pages/plataforma/EmpresasPage.test.tsx` -- cobre exibição e as duas ações, inclusive erro do servidor.

**Acceptance Criteria:**
- Given uma Empresa com `trial_termina_em` no passado (bloqueada, Story 18.2), when o Dono da Plataforma estende o prazo em N dias pela tela "Empresas", then a próxima requisição sob `/e/{slug}/...` volta a responder normalmente e o novo prazo é `now() + N dias` (nunca calculado a partir do prazo antigo).
- Given uma Empresa e o seu Treinamento, when o Dono estende ou isenta o prazo de um dos dois, then o outro permanece exatamente como estava antes.
- Given a tela "Empresas", when o Dono isenta o teste de uma Empresa, then a ação exige confirmação e a linha passa a mostrar "Sem prazo".
- Given `dias` ausente, zero, negativo ou não-inteiro no corpo de "estender", when a requisição chega, then a API responde 400 `VALIDATION_ERROR` e nada é gravado.
- Given um `{id}` que não existe ou é malformado, when qualquer uma das duas rotas é chamada, then a API responde 404 `NOT_FOUND`.

## Spec Change Log

## Review Triage Log

### 2026-09-29 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 4 (medium 2, low 2)
- defer: 1 (low 1)
- reject: 11 (medium 3, low 8)
- addressed_findings:
  - `[medium]` `[patch]` `dias` sem limite superior em `EstenderTrialHandler` podia estourar `time.Duration` (int64) e gravar um prazo nonsense — adicionado teto de 36500 dias (100 anos), 400 `VALIDATION_ERROR` acima disso, com teste; `max={36500}` também no `<Input>` do frontend.
  - `[medium]` `[patch]` `alterarTrial` (EmpresasPage.tsx) devolvia `null` tanto no sucesso quanto no caso de sessão encerrada — o chamador mostrava toast de sucesso e fechava o diálogo mesmo quando a ação NÃO foi aplicada (sessão expirou); introduzido um sentinel distinto para "sessão encerrada, nada a mostrar".
  - `[low]` `[patch]` `IsentarTrialHandler` não tinha `http.MaxBytesReader`, inconsistente com `EstenderTrialHandler` — adicionado por consistência/blindagem, mesmo sem decode de corpo.
  - `[low]` `[patch]` `TreinamentoResumo.TrialTerminaEm`/coluna `t.trial_termina_em` (novo nesta story) não tinha nenhuma asserção no teste da listagem `GET /api/plataforma/empresas` — adicionada verificação em `TestEmpresasPlataforma_EstenderIsentarTrial`.

### 2026-09-29 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 3 (low 3)
- defer: 0
- reject: 20 (low 20)
- addressed_findings:
  - `[low]` `[patch]` As duas novas rotas (`.../trial/extensao`, `.../trial/isencao`) não estavam na tabela de rotas de `TestEmpresasPlataforma_SemTokenOuComTokenDeUsuario` (`backend/handlers/plataforma_test.go`) — toda outra rota restrita ao Dono é coberta ali (401/`TOKEN_EXPIRED` sem token e com token de usuário comum); adicionadas as duas.
  - `[low]` `[patch]` `trialEmAndamentoId` (EmpresasPage.tsx) era limpo incondicionalmente (`setTrialEmAndamentoId(null)`) ao fim de `enviarEstenderTrial`/`confirmarIsentarTrial` — se uma ação num `id` diferente (outra linha, ou a Empresa vs. o Treinamento) começasse enquanto a primeira ainda estava em voo, o `null` incondicional apagava o `id` "em andamento" da segunda, reabilitando os botões dela antes da requisição dela terminar; troca para limpar só quando o `id` atual ainda é o da própria chamada.
  - `[low]` `[patch]` `IsentarTrialHandler` (backend/handlers/empresas.go) envolvia `r.Body` em `http.MaxBytesReader` com um comentário alegando blindar contra corpo grande, mas o handler nunca lê `r.Body` depois disso — o servidor Go drena o corpo original capturado antes do handler rodar, independente dessa reatribuição, então o teto nunca era de fato aplicado; removida a linha morta e o comentário incorreto.

## Design Notes

`AlterarStatusEmpresa` (status) atualiza a Empresa real e o Treinamento numa única instrução (`WHERE (id=$1 AND empresa_origem_id IS NULL) OR empresa_origem_id=$1`), porque desativar/reativar sempre afeta o par junto. `AtualizarTrialEmpresa` é o oposto por design: a Story 18.3 exige que estender/isentar um dos dois nunca toque o outro (mesmo espírito de independência já estabelecido pela Story 18.1 -- herança só na criação, nunca propagação depois). Por isso a nova função faz um `UPDATE ... WHERE id = $1` simples, chamada uma vez por `id` -- a mesma rota HTTP serve tanto o `id` da Empresa real quanto o do Treinamento, sem rota `/treinamentos/...` separada.

## Verification

**Commands:**
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 ./services/... ./handlers/... -run 'Trial'` -- expected: PASS
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros
- `cd frontend && npx tsc -b --force` -- expected: sem erros
- `cd frontend && npx vitest run src/pages/plataforma/EmpresasPage.test.tsx` -- expected: PASS

**Manual checks (if no CLI):**
- Nenhum -- todas as linhas da matriz têm cobertura automatizada viável.

## Auto Run Result

**Resumo:** Story 18.3 já estava implementada (commit `99ab1cd`) quando esta execução começou; este ciclo rodou uma passagem de revisão adicional sobre o diff completo desde `4a8205f` (baseline da Story 18.2) e aplicou 3 patches de baixo risco encontrados nela. Nenhum `intent_gap` nem `bad_spec` foi identificado -- o comportamento cobre a matriz de I/O da spec (estender/isentar por `id` isolado, `dias` inválido, `{id}` inexistente, e o desbloqueio de uma Empresa em 402 depois da ação).

**Arquivos alterados nesta passagem de revisão:**
- `backend/handlers/plataforma_test.go` -- as duas novas rotas de trial entraram na tabela de `TestEmpresasPlataforma_SemTokenOuComTokenDeUsuario` (401 sem token / com token de usuário comum).
- `backend/handlers/empresas.go` -- removida a linha morta `r.Body = http.MaxBytesReader(...)` e o comentário incorreto em `IsentarTrialHandler` (o handler nunca lê o corpo; o teto nunca era de fato aplicado).
- `frontend/src/pages/plataforma/EmpresasPage.tsx` -- `trialEmAndamentoId` agora só é limpo se ainda corresponder ao `id` da própria chamada, evitando que uma ação em outro `id` (outra linha, ou Empresa vs. Treinamento) tenha seu estado "em andamento" apagado cedo demais por uma chamada concorrente.

**Achados da revisão (4 camadas: blind hunter, edge-case, verification-gap, intent-alignment):**
- patch: 3 (low 3) -- ver `## Review Triage Log` acima para o detalhe de cada um.
- defer: 0 nesta passagem (o `deferred` já registrado no frontmatter, sobre `alterarTrial`/`recarregar()`, é pré-existente e foi resurfaced por um reviewer, não duplicado).
- reject: 20 (low 20) -- majoritariamente achados que, ao verificar contra o código-base, batem com convenções já estabelecidas e deliberadas (ex.: `noValidate` + validação só no servidor, igual a `EditarProdutoDialog.tsx`; constante `dias` máx. espelhada front/back, igual a `SLUG_MAX_EMPRESA`/`slugRealMaxRunes`; `ConfirmDialog` fecha ao confirmar por design, igual ao fluxo de "Desativar"; ausência de trailing-data check no JSON, padrão em todos os handlers do repo) ou fora do escopo da intenção (sem trilha de auditoria, sem checagem de status ativo/inativo -- nenhuma das duas é exigida pela spec).

**Follow-up review recommendation:** `false` -- nenhum patch de severidade `high`; `3×medium (0) + 1×low (3) = 3 < 5`.

**Verificação executada:**
- `go build ./...` e `go vet ./...` -- sem erros.
- `DATABASE_URL=... go test -count=1 ./services/... ./handlers/... ./middleware/... -run 'Trial'` -- PASS (todos os testes de trial, incluindo o de desbloqueio via `RequireEmpresa`).
- `DATABASE_URL=... go test -count=1 ./handlers/... -run 'TestEmpresasPlataforma_SemTokenOuComTokenDeUsuario'` -- PASS (com as duas rotas novas na tabela).
- `npx tsc -b --force` -- sem erros.
- `npx vitest run src/pages/plataforma/EmpresasPage.test.tsx` -- PASS (20/20).

**Riscos residuais:** nenhum novo identificado nesta passagem. O item já deferido (risco pré-existente de `alterarTrial`/`recarregar()` dentro do mesmo `try`, herdado de `alterarStatus`) permanece registrado no frontmatter `deferred`, sem mudança.

