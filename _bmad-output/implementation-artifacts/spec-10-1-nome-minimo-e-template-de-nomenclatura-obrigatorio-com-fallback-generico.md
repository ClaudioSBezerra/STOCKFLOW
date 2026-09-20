---
title: 'Story 10.1: Nome mínimo e Template de Nomenclatura obrigatório com fallback Genérico'
type: 'feature'
created: '2026-09-19'
status: 'done'
baseline_revision: '856a3b18e1adeec3ca01d8d4897faf90832e3431'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** O cadastro de Produto hoje só rejeita nome vazio (sem mínimo) e trata o Template de Nomenclatura como totalmente opcional, deixando entrar nomes curtos demais ou fora de padrão que atrapalham busca e consistência do catálogo (FR8).

**Approach:** Exigir nome com no mínimo 10 caracteres (mantendo o máximo 255) no cadastro e em toda edição de nome, e tornar a seleção de Template sempre obrigatória no cadastro — introduzindo um template "Genérico" (`[NOME LIVRE]`, AD-34) como fallback universal para as ~16/25 categorias sem template estrutural.

## Boundaries & Constraints

**Always:**
- Nome: manter máximo 255 runas, adicionar mínimo 10 runas (trim), validado em `CriarProduto` E `AtualizarNomeProduto` (mesma regra nos dois pontos, já hoje duplicados).
- `CriarProduto` passa a rejeitar `template_id` vazio (após trim) com `ErroProdutoValidacao` — não existe mais caminho de cadastro sem template.
- `nomeValidoParaTemplate` trata `templateTexto == "[NOME LIVRE]"` como caso especial: aceita qualquer nome não vazio (após trim), sem checar tokens/ordem (AD-34).
- Uma migration nova insere a linha Genérico (`subtipo='Genérico'`, `template='[NOME LIVRE]'`) em `nomenclatura_templates_padrao` e faz backfill idempotente (mesmo padrão `WHERE NOT EXISTS` de `CopiarListasPadrao`) em `nomenclatura_templates` de toda Empresa já existente — nunca via lazy-init em runtime.
- `AtualizarNomeProduto` continua revalidando contra o template já aplicado (incluindo Genérico) quando o Produto tem `template_id`; Produto legado sem `template_id` continua aceitando qualquer nome que passe só na validação básica (min/max) — o endpoint não ganha campo de seleção de template.
- Nenhuma regra nova dispara varredura/reedição retroativa: vale só para cadastro novo e para a próxima edição de nome de cada Produto (FR8, AD-33).
- Frontend (`CadastroProdutoSection.tsx`): remove a opção "Nome livre (sem template)"/sentinela `SEM_TEMPLATE`; rótulo do campo perde "(opcional)"; `desabilitado` passa a exigir `nome.trim().length >= 10` e `templateId !== ''`; falha ao carregar `/api/nomenclatura-templates` passa a acionar `erroCarregar` (mesmo tratamento hoje dado a categorias/estoques) em vez de degradar silenciosamente — sem a lista, o cadastro não pode ser concluído.

**Block If:** Nenhuma decisão bloqueante identificada — investigação resolveu as ambiguidades de escopo (edição de Produto legado sem template permanece sem exigir template; falha ao carregar templates passa a bloquear o formulário).

**Never:** Não implementar Story 10.2 (código automático sequencial); não criar tela/endpoint geral de edição de Produto (`/renomear` continua escopado só a `nome`); não fazer varredura/reedição retroativa de Produtos legados; não alterar a validação estrutural dos 28 templates existentes além do branch especial do marcador.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Nome curto no cadastro | nome com 9 caracteres (trim) | Rejeitado | 400 VALIDATION_ERROR, nome exige mínimo/máximo |
| Nome no limite mínimo | nome com exatos 10 caracteres | Aceito | Sem erro |
| Cadastro sem template | `template_id` vazio/ausente | Rejeitado | 400 VALIDATION_ERROR "template de nomenclatura é obrigatório" |
| Cadastro com Genérico | `template_id` do template Genérico, nome qualquer ≥10 chars | Aceito, sem checar tokens | Sem erro |
| Edição com template estrutural aplicado | novo nome não casa com o template | Rejeitado, nome no banco não muda | 400 VALIDATION_ERROR "não corresponde ao formato do template" |
| Edição de Produto legado sem template | novo nome com 12 caracteres | Aceito (sem exigir template) | Sem erro |
| Edição de Produto legado sem template, nome curto | novo nome com 5 caracteres | Rejeitado | 400 VALIDATION_ERROR nome mínimo |
| Empresa já existente antes da migration | migration roda | Ganha automaticamente a linha Genérico | Sem erro |

</intent-contract>

## Code Map

- `backend/services/produtos.go:196-276` -- `CriarProduto`: checagem de nome (197-202, só máximo hoje) e bloco de validação de template (255-276, hoje pula quando vazio) — os dois pontos a alterar para AC1/AC2.
- `backend/services/produtos.go:384-416` -- `AtualizarNomeProduto`: checagem de nome (385-390, só máximo hoje) e revalidação contra template já aplicado (404-416, preservar) — alterar só a checagem de mínimo.
- `backend/services/nomenclatura.go:44-77` -- `nomeValidoParaTemplate`: motor de validação por tokens; adicionar branch para o marcador `[NOME LIVRE]` (AD-34) antes da lógica de regex.
- `backend/migrations/000035_create_listas_padrao.up.sql` e `backend/services/empresas.go:374-399` (`CopiarListasPadrao`) -- padrão de tabela-molde + cópia idempotente por Empresa a seguir na nova migration; não alterar `CopiarListasPadrao` em si.
- `backend/migrations/000032_add_empresa_id_dominio.up.sql:88-90` -- índice único `(empresa_id, subtipo)` de `nomenclatura_templates`, garante que o backfill da migration nova não duplique a linha Genérico por Empresa.
- `backend/services/nomenclatura_test.go:8-110` -- testes unitários de `nomeValidoParaTemplate`; local do novo teste do marcador Genérico.
- `backend/services/produtos_test.go:570-873` -- testes de `CriarProduto`/`AtualizarNomeProduto` (nome/template) a estender.
- `backend/handlers/produtos_test.go:408-720` -- testes HTTP de `CriarProdutoHandler`/`AtualizarNomeProdutoHandler` a estender.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:103-108,320-325,494-512,260-295` -- sentinela `SEM_TEMPLATE`, `desabilitado`, `<Select>` de template, e `carregarListas` (tratamento hoje tolerante a falha de template).
- `frontend/src/components/produtos/CadastroProdutoSection.test.tsx:128-306` -- suíte de testes dependente da opção "Nome livre (sem template)", a reescrever.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000036_add_template_generico.up.sql` -- inserir linha Genérico em `nomenclatura_templates_padrao` e fazer backfill idempotente (`WHERE NOT EXISTS`, casando por `empresa_id`+`subtipo`) em `nomenclatura_templates` para toda Empresa existente -- é o único jeito de Empresas novas (via `CopiarListasPadrao`) e existentes (via backfill) ganharem o fallback sem lazy-init.
- `backend/migrations/000036_add_template_generico.down.sql` -- reverter: `DELETE` das linhas Genérico em `nomenclatura_templates` (todas as Empresas) e da linha em `nomenclatura_templates_padrao`.
- `backend/services/nomenclatura.go` -- constante do marcador (`"[NOME LIVRE]"`) e branch em `nomeValidoParaTemplate` que aceita qualquer nome não vazio quando `templateTexto` é o marcador -- implementa AD-34.
- `backend/services/produtos.go` (`CriarProduto`, ~197-202) -- somar checagem de mínimo 10 runas à checagem de máximo já existente -- AC1.
- `backend/services/produtos.go` (`CriarProduto`, ~255-276) -- trocar o `if templateIDTrimado != ""` que hoje pula a validação por: `template_id` vazio -> `ErroProdutoValidacao` "template de nomenclatura é obrigatório"; preenchido -> validar como hoje -- AC2.
- `backend/services/produtos.go` (`AtualizarNomeProduto`, ~385-390) -- somar a mesma checagem de mínimo 10 runas -- AC1 aplicado à edição.
- `backend/services/nomenclatura_test.go` -- teste do branch Genérico: nome vazio/só espaço rejeitado, qualquer nome não vazio aceito independente de formato -- cobre AD-34 isoladamente.
- `backend/services/produtos_test.go` -- estender/adicionar: nome de 9 chars rejeitado (create e rename), 10 chars aceito, `template_id` vazio rejeitado no create, create com template Genérico aceita nome livre ≥10 chars, rename de Produto legado sem template continua aceitando nome livre ≥10 chars -- cobre AC1/AC2/AC3/AC5.
- `backend/handlers/produtos_test.go` -- adicionar teste HTTP: `POST /api/produtos` sem `template_id` -> `400 VALIDATION_ERROR` -- cobre AC2 na camada HTTP.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx` -- remover sentinela `SEM_TEMPLATE`/opção "Nome livre (sem template)"; rótulo sem "(opcional)"; `desabilitado` passa a exigir `nome.trim().length >= 10` e `templateId !== ''`; falha ao carregar `/api/nomenclatura-templates` passa a setar `erroCarregar` -- AC1/AC2 na UI.
- `frontend/src/components/produtos/CadastroProdutoSection.test.tsx` -- reescrever os testes que dependem de "Nome livre (sem template)" (incluir "Genérico" na lista mockada de templates) e cobrir botão desabilitado com nome curto e sem template -- mantém a suíte alinhada ao novo comportamento.

**Acceptance Criteria:**
- Given uma Empresa provisionada antes da migration, when a migration roda, then essa Empresa passa a ter a linha Genérico em `nomenclatura_templates`, sem duplicar nem afetar as demais linhas.
- Given uma Empresa nova provisionada depois da migration, when `ProvisionarEmpresa` roda, then ela recebe a linha Genérico automaticamente via `CopiarListasPadrao`, sem mudança de código nessa função.
- Given um Produto com template estrutural aplicado, when o Almoxarife edita o nome para algo que não corresponde ao template, then a edição é rejeitada e o nome no banco permanece o anterior.
- Given o formulário de cadastro de Produto, when nenhum template está selecionado, then o botão de cadastro permanece desabilitado no cliente e, se submetido mesmo assim via API direta, o backend rejeita com 400.

## Design Notes

O `subtipo` "Genérico" entra na ordenação alfabética natural de `ListarNomenclaturaTemplates` (`ORDER BY subtipo ASC`) junto aos outros 28 — nenhuma AC exige posição fixa no `<Select>`, então não é preciso ordenação especial.

A unificação do erro de carregamento de templates com `erroCarregar` é consequência direta de o template virar obrigatório: o padrão já existe para categorias/estoques (`Promise.all` + `MENSAGEM_ERRO_CARREGAR`); replicar para templates evita deixar o Almoxarife preso num formulário sem nenhum template disponível e sem explicação.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -p 1 ./services/... ./handlers/...` -- expected: todos os testes passam, incluindo os novos de nome mínimo/template obrigatório/marcador Genérico.
- `cd frontend && npx vitest run CadastroProdutoSection` -- expected: suíte reescrita passa, sem referência a "Nome livre (sem template)".
- `cd frontend && npm run build` (roda `tsc -b`) -- expected: sem erros de tipo após remover `SEM_TEMPLATE`.

## Auto Run Result

O `bmad-loop` (run `20260919-223841-6742`) parou logo após o dev implementar
a story, sem chegar à fase de review independente — nenhum agente de review
rodou sobre este diff. Recuperação manual (Claude, 2026-09-20):

- `go build ./... && go vet ./...`: sem erros.
- `go test -p 1 ./services/... ./handlers/...` (contra Postgres local): `ok` em `services` (164s) e `handlers` (167s), sem falhas.
- `npx vitest run CadastroProdutoSection`: 26/26 passam.
- `npm run build` (`tsc -b && vite build`): sem erros de tipo, build de produção completo.
- Spot-check manual do diff contra o `intent-contract`: `CriarProduto`/`AtualizarNomeProduto` (min 10/max 255 runas), `template_id` obrigatório em `CriarProduto`, branch `[NOME LIVRE]` em `nomeValidoParaTemplate` isolado antes da lógica de regex, migration 000036 com backfill idempotente (`WHERE NOT EXISTS`, mesma chave do índice único de 000032) e `down.sql` preservando linhas Genérico já referenciadas por Produto — todos conferem com a spec.
- **Honestidade:** isto substitui a fase de review do `bmad-loop`, mas não é equivalente a ela — não houve um segundo agente adversarial sobre este código. Recomendo `bmad-code-review` nesta story antes ou depois do deploy, sem bloquear o commit local.
