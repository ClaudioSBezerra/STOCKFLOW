---
title: 'Story 14.2 — Cadastro da Empresa pergunta se exige MFA; Treinamento herda'
type: 'feature'
created: '2026-09-24'
status: 'done'
baseline_revision: '26603a09b65358a9619e828db71f02354a9551e6'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-14-context.md'
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** A Story 14.1 criou `empresas.mfa_obrigatorio`, mas toda Empresa nasce `false` e não existe forma de criá-la exigindo MFA. O Dono da Plataforma precisa responder isso no cadastro, e o Ambiente de Treinamento criado junto precisa nascer com a mesma escolha.

**Approach:** `DadosEmpresa` ganha `MFAObrigatorio bool`, gravado por `InserirEmpresa`. Como `ProvisionarTreinamento` já deriva `dadosTreino := dadosReal`, o Treinamento herda o valor na criação. `POST /api/plataforma/empresas` lê o campo `mfa_obrigatorio` (ausente = `false`). A listagem do Dono passa a devolver a escolha da Empresa real e a do Treinamento. A `EmpresasPage` ganha a pergunta (rádio Sim/Não, **Não** pré-selecionado) e mostra a escolha na lista.

## Boundaries & Constraints

**Always:**
- O campo é opcional no corpo: ausente ou `null` vira `false`, e nunca gera 400. Um valor que não é booleano (ex.: `"sim"`) já falha no decode JSON e vira o 400 `payload inválido` de hoje.
- O nome do campo no corpo é `mfa_obrigatorio`, literal do AC. `mfaObrigatorio` (camelCase, padrão do resto do payload) é aceito como sinônimo. Se os dois vierem com valores diferentes, a resposta é 400 VALIDATION_ERROR. O frontend envia `mfa_obrigatorio`.
- A herança acontece só na criação, dentro da mesma transação de `CriarEmpresaComTreinamento`. Nada propaga depois.
- A listagem mostra o valor gravado de cada Empresa, sem derivar o do Treinamento a partir do da real.
- Quem não é Dono da Plataforma mantém exatamente a resposta de hoje em `POST /api/plataforma/empresas` (`RequireDonoPlataforma` → 401 `TOKEN_EXPIRED` para token de usuário), com ou sem o campo, e nada é gravado.

**Block If:** nada previsto.

**Never:**
- Nenhuma rota para o Dono alterar o flag depois do cadastro. A alteração posterior é do `adm` (14.3).
- Não mudar o gate de `RequireRole` nem `/api/auth/me` (14.1).
- Não criar migration: a coluna já existe (000047).
- `cmd/migrar-multi-empresa` e os demais chamadores de `DadosEmpresa` não mudam. O zero value `false` mantém o comportamento atual.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Sim | Dono, corpo com `"mfa_obrigatorio": true` | 201; real e Treinamento com `mfa_obrigatorio=true` no banco e `mfaObrigatorio:true` na resposta | — |
| Não | `"mfa_obrigatorio": false` | 201; as duas `false` | — |
| Ausente | corpo sem o campo | 201; as duas `false` | nunca 400 |
| Sinônimo | `"mfaObrigatorio": true` | 201; as duas `true` | — |
| Conflito | `mfa_obrigatorio:true` e `mfaObrigatorio:false` | 400 VALIDATION_ERROR; nada gravado | — |
| Não booleano | `"mfa_obrigatorio": "sim"` | 400 VALIDATION_ERROR `payload inválido` | — |
| Independência | real criada com `true`; UPDATE da real para `false` | Treinamento continua `true` e a listagem mostra cada valor | — |
| Não-Dono | token de usuário `adm`, corpo com o campo | 401 TOKEN_EXPIRED (igual a hoje); nenhuma Empresa criada | — |

</intent-contract>

## Code Map

- `backend/services/empresas.go:95` -- `type DadosEmpresa`: adicionar `MFAObrigatorio bool`. `:~300` `dadosEmpresaValidados` + `validarDadosEmpresa` levam o valor. `InserirEmpresa` (`:~345`) inclui `mfa_obrigatorio` no INSERT; o `RETURNING colunasEmpresa` já devolve o campo.
- `backend/services/empresas_plataforma.go:57` -- `NovaEmpresaInput` embute `DadosEmpresa`, então `input.MFAObrigatorio` já existe. `ValidarDadosNovaEmpresa` (`:125`) copia `input.DadosEmpresa`, preservando o campo. `ProvisionarTreinamento` (`:230`) faz `dadosTreino := dadosReal`, então herda sem código novo. Atualizar os doc-comments.
- `backend/services/empresas_plataforma.go:66-90` -- `TreinamentoResumo` e `EmpresaResumo`: adicionar `MFAObrigatorio bool` com a tag `json:"mfaObrigatorio"`. `ListarEmpresasPlataforma` (`:325`) seleciona `e.mfa_obrigatorio` e `t.mfa_obrigatorio`. O do Treinamento é nulável por causa do LEFT JOIN: usar `sql.NullBool`.
- `backend/handlers/empresas.go:24` -- `novaEmpresaRequest`: adicionar `MFAObrigatorio *bool` (`json:"mfa_obrigatorio"`) e `MFAObrigatorioCamel *bool` (`json:"mfaObrigatorio"`). Resolver os dois antes de chamar o service, com 400 no conflito. Atualizar o doc-comment.
- `backend/services/empresas_plataforma_test.go:19` -- `novaEmpresaTeste`, `comParLimpo`, `TestCriarEmpresaComTreinamento_CriacaoCompleta` (`:158`), `TestListarEmpresasPlataforma_SoMetadado` (`:379`): padrão para os novos testes.
- `backend/handlers/plataforma_test.go:278` -- `corpoNovaEmpresa` (sem o campo = cenário "ausente"), `TestEmpresasPlataforma_SemTokenOuComTokenDeUsuario` (`:284`), `TestEmpresasPlataforma_CriarListarDesativarReativar` (`:316`), `limparParesPlataforma`, `despacharPlataforma`.
- `backend/middleware/plataforma.go` -- `RequireDonoPlataforma` responde 401 TOKEN_EXPIRED a token que não é de Dono. Só leitura.
- `frontend/src/lib/plataforma.ts:59-80` -- `EmpresaResumo` (+ `mfaObrigatorio: boolean`, também em `treinamento`), `NovaEmpresa` (+ `mfa_obrigatorio: boolean`), `EmpresaCriada`.
- `frontend/src/pages/plataforma/EmpresasPage.tsx` -- `FormEmpresa`/`FORM_VAZIO`, o formulário (`:~300-390`), o payload em `handleSubmit` e o card da lista (`:~410-440`).
- `frontend/src/components/normalizacao/DuplicatasSection.tsx:260` -- exemplo de `input type="radio"` nativo já usado na casa.
- `frontend/src/pages/plataforma/EmpresasPage.test.tsx` -- `EMPRESA` (fixture), o teste de payload (`:158`, usa `toEqual`, então precisa do campo novo) e o teste de lista (`:126`).

## Tasks & Acceptance

**Execution:**
- `backend/services/empresas.go` -- adicionar o campo em `DadosEmpresa`, levá-lo pela validação e gravá-lo no INSERT de `InserirEmpresa` -- para a Empresa nascer com a escolha.
- `backend/services/empresas_plataforma.go` -- adicionar `MFAObrigatorio` a `EmpresaResumo` e a `TreinamentoResumo`, selecioná-lo em `ListarEmpresasPlataforma` e atualizar os comentários de `NovaEmpresaInput`, `CriarEmpresaComTreinamento` e `ProvisionarTreinamento` (herança só na criação).
- `backend/services/empresas_plataforma_test.go` -- testes: criação com `true` deixa real e Treinamento `true` no banco; o default (campo não setado) deixa as duas `false`; depois de um UPDATE da real para `false`, o Treinamento continua `true` e a listagem mostra cada valor.
- `backend/handlers/empresas.go` -- ler `mfa_obrigatorio` e o sinônimo `mfaObrigatorio`, recusar com 400 quando os dois divergem e repassar o valor em `NovaEmpresaInput`.
- `backend/handlers/plataforma_test.go` -- cobrir as linhas da matriz na fronteira HTTP: Sim (banco e resposta), Ausente (o `corpoNovaEmpresa` atual → `false`), Sinônimo, Conflito (400 e nada gravado), Não booleano (400) e Não-Dono (token de usuário com o campo → 401 e nenhuma Empresa com aquele slug). A listagem traz `mfaObrigatorio` na real e no `treinamento`.
- `frontend/src/lib/plataforma.ts` -- atualizar os tipos.
- `frontend/src/pages/plataforma/EmpresasPage.tsx` -- adicionar ao formulário um `fieldset` com a legenda "Esta Empresa exige dupla autenticação?" e dois rádios nativos, "Sim" e "Não", com **Não** marcado por padrão e restaurado após criar. Incluir uma dica curta dizendo que o Ambiente de Treinamento nasce com a mesma escolha. Enviar `mfa_obrigatorio`. No card da lista, mostrar "Dupla autenticação: Exigida" ou "Não exigida" para a Empresa e, quando houver Treinamento, o valor dele.
- `frontend/src/pages/plataforma/EmpresasPage.test.tsx` -- testes: o rádio "Não" começa marcado; o payload padrão leva `mfa_obrigatorio: false`; marcar "Sim" envia `true`, e depois de criar o formulário volta a "Não"; a lista mostra a escolha da Empresa e a do Treinamento.

**Acceptance Criteria:**
- Given a `EmpresasPage`, when ela abre, then a pergunta "Esta Empresa exige dupla autenticação?" aparece com **Não** marcado, e a escolha vai no corpo do POST como `mfa_obrigatorio`.
- Given uma Empresa criada com "Sim", when o provisionamento termina, then `empresas.mfa_obrigatorio = true` na real e no Treinamento. Um UPDATE posterior numa delas não altera a outra.
- Given a listagem do Dono, when é exibida, then cada Empresa (e o seu Treinamento) mostra a própria escolha de MFA, sem nenhum dado operacional novo.

## Spec Change Log

## Review Triage Log

### 2026-09-24 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 2 (high 0, medium 0, low 2)
- defer: 0
- reject: 21 (high 0, medium 2, low 19)
- addressed_findings:
  - `[low]` `[patch]` `ProvisionarTreinamento` agora herda `MFAObrigatorio` do valor gravado na Empresa real (`real.MFAObrigatorio`), e não só de `dadosReal`. Assim, um chamador que monte `DadosEmpresa` sem o campo não desalinha o Treinamento.
  - `[low]` `[patch]` Teste HTTP do corpo com `"mfa_obrigatorio": null` (→ `false`), regra do "Always" que estava sem cobertura.

## Design Notes

**Nome do campo:** o AC fixa `mfa_obrigatorio` no corpo, mas todo o resto de `novaEmpresaRequest` é camelCase. Aceitar os dois nomes atende ao texto literal e ao padrão da casa, sem obrigar ninguém a adivinhar. Se os dois vierem com valores diferentes, o servidor responde 400 em vez de escolher um em silêncio. As respostas continuam camelCase (`mfaObrigatorio`), como na 14.1.

**403 × 401 para quem não é Dono:** o AC diz "403, como já é hoje". Hoje `RequireDonoPlataforma` responde 401 TOKEN_EXPIRED a um token de usuário (`plataforma_test.go:284`). O que o AC pede é "nenhuma mudança", então a story mantém o 401 e só prova que o campo novo não abre caminho.

Esboço do handler:

```go
mfa := false
if req.MFAObrigatorio != nil { mfa = *req.MFAObrigatorio }
if req.MFAObrigatorioCamel != nil {
    if req.MFAObrigatorio != nil && *req.MFAObrigatorio != *req.MFAObrigatorioCamel { /* 400 */ }
    mfa = *req.MFAObrigatorioCamel
}
```

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: limpo
- `cd backend && DATABASE_URL='postgres://stockflow:stockflow@127.0.0.1:5432/stockflow_t91?sslmode=disable' go test -p 1 -count=1 ./services/... ./handlers/... ./middleware/...` -- expected: verde
- `cd frontend && npx tsc -b && npx vitest run src/pages/plataforma src/lib/plataforma.test.ts` -- expected: verde

## Auto Run Result

Status: done

**Resumo:** o cadastro de Empresa pelo Dono da Plataforma pergunta "Esta Empresa exige dupla autenticação?", com rádios Sim/Não e **Não** pré-selecionado. A escolha vai no corpo de `POST /api/plataforma/empresas` como `mfa_obrigatorio`, e `mfaObrigatorio` é aceito como sinônimo. Campo ausente ou `null` vira `false`; os dois nomes com valores diferentes dão 400. `DadosEmpresa.MFAObrigatorio` é gravado por `InserirEmpresa`, e o Treinamento herda o valor gravado da real na mesma transação de criação. Depois disso, nada propaga. A listagem do Dono devolve e mostra o valor gravado da Empresa e o do seu Treinamento. Quem não é Dono continua recebendo exatamente a resposta de hoje (401 TOKEN_EXPIRED), com ou sem o campo.

**Arquivos:**
- `backend/services/empresas.go`: campo `MFAObrigatorio` em `DadosEmpresa`, levado pela validação e gravado no INSERT de `InserirEmpresa`.
- `backend/services/empresas_plataforma.go`: herança explícita em `ProvisionarTreinamento`; `mfaObrigatorio` em `EmpresaResumo`/`TreinamentoResumo` e na query de `ListarEmpresasPlataforma` (`sql.NullBool` para o LEFT JOIN); comentários.
- `backend/services/empresas_plataforma_test.go`: criação com true e com o default; independência depois de um UPDATE na real; listagem com os dois valores.
- `backend/handlers/empresas.go`: `mfa_obrigatorio` + sinônimo, `resolverMFAObrigatorio` e 400 no conflito.
- `backend/handlers/plataforma_test.go`: matriz na fronteira HTTP (Sim, Não, null, Ausente, Sinônimo, Conflito, Não booleano, Não-Dono) e listagem.
- `frontend/src/lib/plataforma.ts` (+ teste): tipos `mfaObrigatorio`/`mfa_obrigatorio`.
- `frontend/src/pages/plataforma/EmpresasPage.tsx` (+ teste): fieldset com os rádios e a dica de herança; payload; "Dupla autenticação: Exigida/Não exigida" no card, com o valor do Treinamento.

**Revisão:** 2 patches aplicados (low), 0 adiados, 21 rejeitados. Motivos da rejeição:
- 403 × 401 para quem não é Dono: o AC diz "como já é hoje", e hoje é 401 (Design Notes).
- Nome snake × camel: decisão da spec.
- Os testes reaproveitam CNPJ: a asserção de status já pegaria o 409.
- Usuário de teste sem limpeza: `testDB` já trunca, e as suítes passaram em execuções repetidas.
- Chaves com outra caixa (`MFAOBRIGATORIO`): exótico.
- Resposta sem o campo durante o deploy: frontend e backend vão juntos.
- O restante são itens cosméticos, cobertura redundante ou metadados da própria spec.

**Follow-up recomendado:** false (patches: 0 high, 0 medium, 2 low; score 3×0 + 2 = 2 < 5).

**Verificação:**
- `go build ./...` e `go vet ./...`: limpos; `gofmt -l`: nada.
- `go test -p 1 -count=1 ./services/... ./handlers/... ./middleware/...` contra `stockflow_t91`: verde antes e depois dos patches (services ~225s, handlers ~200s).
- Os testes novos rodaram com `-v` e passaram: `TestCriarEmpresaComTreinamento_MFAObrigatorio`, `TestMFAObrigatorio_HerancaSoNaCriacao`, `TestEmpresasPlataforma_MFAObrigatorio`, `TestEmpresasPlataforma_MFAObrigatorioNaoDono`.
- Frontend: `npx tsc -b` limpo; vitest em `src/pages/plataforma` e `src/lib/plataforma.test.ts` com 35 testes verdes.
- Auditoria da matriz: todas as linhas têm teste que rodou e passou. A linha "Não" (false explícito) e o caso `null` foram acrescentados nesta execução.

**Riscos residuais:**
- A API aceita dois nomes para o mesmo campo, e isso passa a ser contrato.
- A independência entre real e Treinamento só foi provada com UPDATE direto no banco, porque a rota de alteração pelo `adm` é da 14.3.
- Os arquivos do frontend não passaram por Prettier ou ESLint, que não têm config no repositório.
