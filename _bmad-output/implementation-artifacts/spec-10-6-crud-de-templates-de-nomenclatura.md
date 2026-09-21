---
title: 'Story 10.6: CRUD de Templates de Nomenclatura'
type: 'feature'
created: '2026-09-20'
baseline_revision: 'af44b4e465d7e721ec8e2cda8a03338eff6b63d5'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: [oversized]
deferred: []
---

<intent-contract>

## Intent

**Problem:** Templates de Nomenclatura (`nomenclatura_templates`) são hoje uma lista somente-leitura (`GET /api/nomenclatura-templates`) copiada do seed em cada Empresa; nenhum `adm` consegue cadastrar, editar ou remover um Template, e a Empresa fica presa ao padrão fixo dos 28 + "Genérico".

**Approach:** Endpoints de escrita `adm`+ (criar, editar, excluir) sobre `nomenclatura_templates`, sempre filtrados por `empresa_id`, com exclusão bloqueada por Produto referenciando (molde de `ExcluirCategoria`, Story 10.5), edição nunca retroativa, proteção do fallback "Genérico" (`[NOME LIVRE]`) e uma `TemplatesNomenclaturaSection` em `/configuracoes` (só `adm`+) usando `ConfirmDialog`.

## Boundaries & Constraints

**Always:**
- Escrita só por `adm`+: `RequireAuth` + `RequireRole(services.PapelAdm)`; papel abaixo -> 403 (decidido por `RequireRole`, sem lógica no handler). `GET /api/nomenclatura-templates` continua só `RequireAuth`, inalterado.
- Toda query filtra `empresa_id` (AD-20). `id` de outra Empresa, inexistente, não-UUID (`pq` 22P02) ou com byte NUL colapsam em `ErrTemplateNaoEncontrado` -> 404, nunca 403.
- Corpo `{"subtipo","template"}`; ambos trimados. Validação (-> `ErrTemplateValidacao` -> 400, sem tocar o banco): `subtipo` e `template` não vazios, ≤ 255 runes, sem NUL; `template` deve ser o marcador `[NOME LIVRE]` (`TemplateGenericoMarcador`) OU conter ≥ 1 token `[...]` (regex `tokenTemplate`), cada token com texto não-branco e sem `[` interno, e nenhum `[`/`]` solto fora dos tokens.
- Unicidade de `subtipo` por Empresa vem do índice `idx_nomenclatura_templates_empresa_subtipo` (23505 -> 409, sem SELECT prévio); outro 23505 segue como erro inesperado.
- Editar NUNCA toca `produtos`: Produto já cadastrado sob o padrão antigo permanece como está; a regra nova vale só no próximo cadastro/`renomear` (que já lê `nomenclatura_templates.template` no momento da ação). Editar Template em uso é permitido.
- Excluir: numa transação, `SELECT ... WHERE id=$1 AND empresa_id=$2 FOR UPDATE` (lê `template`), depois `SELECT count(*) FROM produtos WHERE template_id=$1`; > 0 -> `*ErroTemplateEmUso{Produtos int}` -> 409 com a contagem; senão `DELETE`. FK violation (23503) residual também vira `ErroTemplateEmUso`.
- Fallback "Genérico" (AD-34): identificado pelo texto `template == [NOME LIVRE]` (não pelo `subtipo`). Excluir esse Template, ou editar seu `template` para outro texto, é bloqueado (`ErrTemplateFallbackObrigatorio` -> 409) quando ele é o ÚNICO template-marcador da Empresa (travar as linhas-marcador com `FOR UPDATE` e contar na mesma transação). Renomear o `subtipo` dele é permitido; havendo outro template-marcador, ele é excluível/editável. Checagem do fallback vem ANTES da de "em uso".
- Migration `000040` aplica `CHECK (btrim(subtipo) <> '' AND btrim(template) <> '')` em `nomenclatura_templates` e `nomenclatura_templates_padrao`; `.down.sql` derruba os CHECKs. Seed e Produtos ficam intactos.
- Frontend: exclusão via `ConfirmDialog` (nunca `window.confirm`); erros inline `role="alert"`; `<Input maxLength>` 255 espelhando o banco; 400/409 mostram a mensagem do servidor; o botão Excluir do template-marcador único NUNCA é oferecido (renderizado desabilitado com explicação "fallback obrigatório").

**Block If:** Nenhuma decisão bloqueante identificada.

**Never:** Não migrar nem reeditar Produtos (nem revalidar nomes existentes). Não tocar Categorias (Story 10.5), `nomenclatura_templates_padrao` como CRUD (molde da plataforma, sem endpoint), `GET /api/nomenclatura-templates`, `nomeValidoParaTemplate`, nem o cadastro de Produto. Não permitir edição de `empresa_id`. Não criar template "protegido" além do fallback acima.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Criar | `adm`, `{subtipo:"Cabos — Especial", template:"CABO [TIPO] [BITOLA]"}` | 201 `{"template":{id,subtipo,template}}`, na Empresa do `adm` | — |
| Criar inválido | vazio/só espaços, > 255 runes, sem token, `[` aberto, token só espaços, NUL | 400 VALIDATION_ERROR, nada gravado | — |
| Criar marcador | `template:"[NOME LIVRE]"` | 201 (segundo fallback permitido) | — |
| Criar duplicado | `subtipo` já existe na Empresa | 409 CONFLICT | — |
| Mesmo subtipo, outra Empresa | Empresa B tem o subtipo | 201 (isolamento) | — |
| Editar | `PUT /api/nomenclatura-templates/{id}` `{subtipo,template}` | 200 `{"template":{...}}` | 404 id alheio/inexistente/malformado/NUL; 409 duplicado; 400 inválido |
| Editar em uso | Produto usa o template; `adm` muda `template` | 200; `produtos.nome`/`template_id` intactos; próximo `renomear` valida contra o texto novo | — |
| Excluir em uso | ≥ 1 Produto com `template_id` | 409 CONFLICT com contagem; linha permanece | — |
| Excluir sem uso | nenhum Produto referencia | 204, linha removida | 404 id alheio/inexistente |
| Excluir Genérico único | único `[NOME LIVRE]` da Empresa | 409 (fallback obrigatório), permanece | — |
| Editar Genérico único | troca `template` para texto estrutural | 409; renomear só o `subtipo` -> 200 | — |
| Excluir Genérico com outro marcador | existe 2º `[NOME LIVRE]` | 204 (ou 409 se em uso) | — |
| Papel abaixo de adm | `usuario`/`almoxarife`/`gestor` em POST/PUT/DELETE | 403 | 401 sem token |

</intent-contract>

## Code Map

- `backend/services/nomenclatura.go` -- `NomenclaturaTemplate`, `tokenTemplate`, `TemplateGenericoMarcador`, `ListarNomenclaturaTemplates`; ganha as funções de escrita (ou novo `services/nomenclatura_crud.go` se preferir manter o arquivo enxuto).
- `backend/services/categorias.go` -- MOLDE integral (erros, `validarCategoria`, `traduzirErroEscrita*`, `FOR UPDATE` + contagem + 23503 em `ExcluirCategoria`); reusar `pqUniqueViolation`, `pqInvalidTextRepresentation`, `pqForeignKeyViolation`, `pqStringDataRightTruncation`, `pqCheckViolation`, `pqInvalidByteSequence`.
- `backend/handlers/categorias.go` -- MOLDE de handler (`contextoCategoria`, mapeamento de erros, `authRequestMaxBytes`); novo `backend/handlers/nomenclatura.go` (o `ListarNomenclaturaTemplatesHandler` permanece em `handlers/produtos.go:546`).
- `backend/main.go:~455-462` -- junto do GET: `POST /e/{slug}/api/nomenclatura-templates`, `PUT .../{id}`, `DELETE .../{id}` (RequireAuth + RequireRole(PapelAdm), molde das rotas de categorias).
- `backend/migrations/000013`, `000032:78-90`, `000035`, `000036` -- só leitura (schema, índice único por Empresa, molde `_padrao`, seed "Genérico"); nova `000040_checks_nomenclatura_templates.{up,down}.sql` (última existente: `000039`).
- `backend/services/produtos.go:390,567` -- leem `template` na hora do cadastro/`renomear`; base do teste de "não retroativo".
- `backend/services/categorias_test.go`, `backend/handlers/categorias_test.go`, `backend/main_test.go` (`TestNewMux_CategoriasEscritaCarregaRequireRoleAdm`) -- moldes de teste (DB real via `DATABASE_URL`).
- `frontend/src/components/categorias/CategoriasSection.tsx` (+ `.test.tsx`) -- molde de seção CRUD; novo `frontend/src/components/nomenclatura/TemplatesNomenclaturaSection.tsx`.
- `frontend/src/pages/ConfiguracoesPage.tsx:~548` -- montar `{rankPapel(papel) >= rankPapel('adm') && <TemplatesNomenclaturaSection />}` após `CategoriasSection`; atualizar o comentário de cabeçalho; `ConfiguracoesPage.test.tsx` -- stub de `/api/nomenclatura-templates` e teste do gate por papel.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000040_checks_nomenclatura_templates.{up,down}.sql` -- CHECK de não-vazio em `nomenclatura_templates` e `_padrao` -- limite no banco (AD-33)
- `backend/services/nomenclatura.go` (ou arquivo novo) -- `CriarNomenclaturaTemplate`, `AtualizarNomenclaturaTemplate`, `ExcluirNomenclaturaTemplate`, `validarTemplateNomenclatura` e erros (`ErrTemplateValidacao`, `ErrTemplateNaoEncontrado`, `ErrTemplateDuplicado`, `ErroTemplateEmUso`, `ErrTemplateFallbackObrigatorio`) -- regra de negócio isolada do HTTP
- `backend/handlers/nomenclatura.go` -- 3 handlers e mapeamento 201/200/204/400/404/409 (envelope AD-14) -- fronteira HTTP
- `backend/main.go` -- 3 rotas atrás de `RequireRole(PapelAdm)` -- gate `adm`+
- `backend/services/nomenclatura_test.go` + `backend/handlers/nomenclatura_test.go` + `backend/main_test.go` -- cobrir toda a matriz: sucesso, validação (cada caso), duplicado, outra Empresa, 404 (alheio/inexistente/malformado/NUL), editar em uso sem tocar Produtos + `renomear` validando pelo texto novo, excluir em uso (409) e sem uso (204), fallback único (excluir/editar 409, renomear 200, 2º marcador libera), CHECK direto no SQL, 403 por papel e 401 pelo `newMux` real, `ProvisionarEmpresa` ainda copia os templates
- `frontend/src/components/nomenclatura/TemplatesNomenclaturaSection.tsx` (+ `.test.tsx`) -- listar, cadastrar, editar inline, excluir com `ConfirmDialog`, erros 400/404/409/genérico, Excluir desabilitado no fallback único -- UI `adm`+
- `frontend/src/pages/ConfiguracoesPage.tsx` (+ teste) -- montar a seção só para `adm`+ -- gate de papel também no cliente

**Acceptance Criteria:**
- Given um `adm` autenticado, when cadastra um Template com estrutura de tokens válida, then ele é criado na Empresa dele, independente das demais Empresas.
- Given um Template em uso por Produtos, when o `adm` edita sua estrutura, then nenhum Produto existente é alterado/reeditado; a nova estrutura só vale no próximo cadastro/renomeação.
- Given um Template referenciado por ≥ 1 Produto, when o `adm` tenta excluí-lo, then 409 e a linha permanece.
- Given a lista de Templates, when o `adm` a visualiza, then "Genérico" aparece como os demais, mas a UI (e a API) nunca permitem excluí-lo enquanto for o único fallback `[NOME LIVRE]` da Empresa.
- Given um Usuário abaixo de `adm`, when chama POST/PUT/DELETE, then 403; a seção nem aparece em `/configuracoes`.

## Spec Change Log

## Review Triage Log

## Design Notes

O fallback é identificado pelo TEXTO `[NOME LIVRE]`, não pelo `subtipo` "Genérico", porque o `adm` pode renomear o subtipo e porque é o texto que `nomeValidoParaTemplate` (AD-34) trata como caso especial. Bloquear excluir/editar o único marcador garante que "template obrigatório" (Story 10.1) nunca deixe uma Empresa sem opção alguma. Exigir ≥ 1 token em template estrutural evita um template sem placeholder, que só aceitaria nome idêntico ao próprio texto. Resposta usa a chave `template` (`{"template":{id,subtipo,template}}`), coerente com `{"templates":[...]}` da listagem.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros e sem arquivos listados.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 -p 1 ./...` -- expected: tudo passa (inclui `empresas`, `isolamento`, `migracao_multi_empresa`; `services` e `handlers` são lentos, rodar separados se necessário).
- `cd frontend && npx vitest run TemplatesNomenclaturaSection ConfiguracoesPage` -- expected: passa.
- `cd frontend && npm run build` -- expected: sem erros de tipo.

## Auto Run Result

O `bmad-loop` (run `20260920-091613-28d2`) parou com a story em `in-review`,
sem evidência de review independente sobre o diff. Recuperação manual (Claude,
2026-09-20): `go build`/`go vet` limpos; `go test -count=1 -p 1` (Postgres real)
`ok` em `services` (226s), `handlers` (205s) e `backend` (20s); frontend
60/60 (ConfiguracoesPage + nomenclatura) e `npm run build` sem erros.
Spot-check: rotas de escrita restritas por papel, Genérico protegido de
exclusão/alteração quando é o único (contagem sob lock), exclusão bloqueada
por Produto referenciando, migration 000040 só adiciona CHECKs de não-vazio.
**Sem review adversarial independente** — recomendo `bmad-code-review`.

### Review Findings

Code review independente (2026-09-21, 4 revisores: Blind Hunter, Edge Case Hunter, Verification Gap, Acceptance Auditor; achados verificados no código antes de classificar).

- [x] [Review][Defer] Template só com tokens adjacentes (`[A][B]`) ou um token sem literal degenera em regex ambígua que aceita qualquer nome [backend/services/nomenclatura_crud.go: validarTemplateNomenclatura] — deferred, exige decidir a regra de validação
- [ ] [Review][Patch] Concorrência de `travarTemplateEMarcadores` (FOR UPDATE ordenado) e os desvios de FK só são exercitados em teste sequencial; duas exclusões simultâneas dos dois últimos `[NOME LIVRE]` poderiam deixar a Empresa sem fallback se o lock regredir [backend/services/nomenclatura_test.go]
- [ ] [Review][Patch] Teste de "1 produto" aceita também "1 produtos" (substring) — plural invertido passaria [backend/services/nomenclatura_test.go, backend/handlers/nomenclatura_test.go:191]
- [ ] [Review][Patch] AD-34 diz "nunca removível via CRUD" mas o código (e o AC do epics) só protege o ÚLTIMO `[NOME LIVRE]`; alinhar o texto da AD [ARCHITECTURE-SPINE.md AD-34]
