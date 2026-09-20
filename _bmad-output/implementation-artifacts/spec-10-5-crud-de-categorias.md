---
title: 'Story 10.5: CRUD de Categorias'
type: 'feature'
created: '2026-09-20'
status: 'done'
baseline_revision: 'ed914fc7995f42b90600ed48ceed461bfc896129'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: [oversized]
deferred: []
---

<intent-contract>

## Intent

**Problem:** Categorias são hoje uma lista somente-leitura (`GET /api/categorias`) copiada do seed em cada Empresa; nenhum `adm` consegue cadastrar, renomear ou remover uma Categoria, e os limites de tamanho do banco (`codigo` 10, `nome` 255) não são os de AD-33 (8 / 50).

**Approach:** Endpoints de escrita `adm`+ (criar, editar, excluir) sobre `categorias`, sempre filtrados por `empresa_id`, com exclusão bloqueada por Produto referenciando (molde de `ExcluirEstoque`), limites 8/50 aplicados no banco por migration, e uma `CategoriasSection` na página `/configuracoes` (só `adm`+) usando `ConfirmDialog`.

## Boundaries & Constraints

**Always:**
- Escrita só por `adm`+: `RequireAuth` + `RequireRole(services.PapelAdm)`; papel abaixo -> 403 (decidido por `RequireRole`, sem lógica no handler). `GET /api/categorias` continua só `RequireAuth`, inalterado.
- Toda query filtra `empresa_id` (AD-20). `id` de outra Empresa, inexistente ou não-UUID (`pq` 22P02) colapsam em `ErrCategoriaNaoEncontrada` -> 404, nunca 403.
- Validação: `codigo` e `nome` trimados; vazio ou > 8 / > 50 runes -> `ErrCategoriaValidacao` -> 400 sem tocar o banco. Unicidade por Empresa vem dos índices `idx_categorias_empresa_codigo`/`_nome` (23505 -> 409, mensagem diz qual campo), sem SELECT prévio.
- Migration `000039` aplica os limites no banco: `categorias.codigo VARCHAR(8)`, `categorias.nome VARCHAR(50)`, e o mesmo em `categorias_padrao` (senão `CopiarListasPadrao` quebraria ao provisionar Empresa), mais `CHECK (btrim(...) <> '')` em ambas. As 25 Categorias seed e as linhas de Produto ficam intactas, com UMA exceção necessária: `09.001` "Peças/Materiais para Equipamentos/Veículos/Máquinas" tem 51 caracteres e não cabe em 50 -> a migration renomeia essa linha (em `categorias` e `categorias_padrao`) para "Peças/Materiais p/ Equipamentos/Veículos/Máquinas" (49) ANTES do `ALTER`. `.down.sql` restaura tipos (10/255) e derruba os CHECKs (o nome encurtado não é revertido).
- Excluir: numa transação, `SELECT id FROM categorias WHERE id=$1 AND empresa_id=$2 FOR UPDATE` (serializa com `CriarProduto`, mesma razão de `ExcluirEstoque`), depois `SELECT count(*) FROM produtos WHERE categoria_id=$1`; > 0 -> `*ErroCategoriaEmUso{Produtos int}` -> 409 com a contagem na mensagem; senão `DELETE`. Uma FK violation (23503) residual também vira `ErroCategoriaEmUso`.
- Editar `codigo`/`nome` de Categoria em uso é permitido (Produtos apontam por `id`).
- Frontend: exclusão via `ConfirmDialog` (nunca `window.confirm`); erros inline `role="alert"`; `<Input maxLength>` 8 e 50 espelhando o banco; 409 mostra a mensagem do servidor.

**Block If:** Nenhuma decisão bloqueante identificada.

**Never:** Não criar Categoria "padrão"/protegida (qualquer uma sem Produto é excluível). Não migrar nem renumerar dados além do rename de `09.001`. Não tocar Templates (Story 10.6), `categorias_padrao` como CRUD (é molde da plataforma, sem endpoint), `GET /api/categorias`, nem a busca/catálogo. Não permitir edição de `empresa_id`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Criar | `adm`, `{codigo:"14.001", nome:"Brindes"}` | 201 `{"categoria":{id,codigo,nome}}`, na Empresa do `adm` | — |
| Criar acima do limite | codigo 9 runes ou nome 51 runes | 400 VALIDATION_ERROR, nada gravado | — |
| Criar vazio | codigo/nome só espaços | 400 VALIDATION_ERROR | — |
| Criar duplicado | codigo (ou nome) já existe na Empresa | 409 CONFLICT citando o campo | — |
| Mesmo código, outra Empresa | Empresa B tem `14.001` | 201 (isolamento) | — |
| Editar | `PUT /api/categorias/{id}` `{codigo,nome}` | 200 `{"categoria":{...}}` | 404 id alheio/inexistente/malformado; 409 duplicado; 400 inválido |
| Excluir com Produto | ≥1 Produto com `categoria_id` | 409 CONFLICT, Categoria permanece | — |
| Excluir sem Produto | nenhum Produto referencia | 204, linha removida | 404 id alheio/inexistente |
| Papel abaixo de adm | `usuario`/`almoxarife`/`gestor` em POST/PUT/DELETE | 403 | — |
| Seed pós-migration | Empresa existente com as 25 linhas | intactas, editáveis; `09.001` com nome de 49 caracteres | — |
| Provisionar Empresa nova | `ProvisionarEmpresa` copia `categorias_padrao` | 25 linhas copiadas sem violar 8/50 | — |

</intent-contract>

## Code Map

- `backend/migrations/000010_create_categorias.up.sql`, `000032_add_empresa_id_dominio.up.sql:78-86` (índices únicos por Empresa), `000035_create_listas_padrao.up.sql` (`categorias_padrao`, PK `codigo`) — só leitura; base da migration nova `000039_limites_categorias.{up,down}.sql` (última existente: `000038`).
- `backend/services/produtos.go:136-142` (`Categoria`), `:731-758` (`ListarCategorias`) — tipo e listagem reaproveitados; novo `backend/services/categorias.go` com `CriarCategoria`, `AtualizarCategoria`, `ExcluirCategoria`, erros. Constantes `pqUniqueViolation` (`auth.go:32`), `pqInvalidTextRepresentation` já existem (23503/22001 declarar local).
- `backend/services/estoques.go:36-155` — molde de erros, trim/runes, 23505, 22P02 e `FOR UPDATE` em `ExcluirEstoque`.
- `backend/services/empresas.go:361-390` (`CopiarListasPadrao`) — deve continuar passando com os tipos novos.
- `backend/handlers/estoques.go` — molde de handler (`UsuarioDaSessao`, `empresaDaRequisicao`, `escreverErro`/`escreverJSON`, `authRequestMaxBytes`); novo `backend/handlers/categorias.go` (`ListarCategoriasHandler` permanece em `handlers/produtos.go:168`).
- `backend/main.go:~438` — junto de `GET /e/{slug}/api/categorias`: `POST /e/{slug}/api/categorias`, `PUT /e/{slug}/api/categorias/{id}`, `DELETE /e/{slug}/api/categorias/{id}` (RequireAuth + RequireRole(PapelAdm), molde `logs-acesso` l.~405).
- `backend/services/estoques_test.go` (`TestExcluirEstoque_*`), `backend/handlers/estoques_test.go` (`postEstoques`/`deleteEstoques`, casos 201/400/403/404/409) e `services/isolamento_test.go` — moldes de teste (DB real via `DATABASE_URL`).
- `frontend/src/components/estoques/LocaisEstoqueSection.tsx` — molde de seção CRUD (form, lista, `ConfirmDialog`, toasts, `apiUrl`/`authHeaders`); novo `frontend/src/components/categorias/CategoriasSection.tsx`.
- `frontend/src/pages/ConfiguracoesPage.tsx:541` — montar `{rankPapel(papel) >= rankPapel('adm') && <CategoriasSection />}` ao lado de `LogAcessoSection`; atualizar o comentário de cabeçalho. `ConfiguracoesPage.test.tsx:627` — molde do teste de gate por papel.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000039_limites_categorias.up.sql` + `.down.sql` -- rename de `09.001`, `ALTER ... TYPE VARCHAR(8/50)` e `CHECK` em `categorias` e `categorias_padrao` -- limites no banco (AD-33)
- `backend/services/categorias.go` -- criar/atualizar/excluir + erros (`ErrCategoriaValidacao`, `ErrCategoriaDuplicada{Campo}`, `ErrCategoriaNaoEncontrada`, `ErroCategoriaEmUso`) -- regra de negócio isolada do HTTP
- `backend/handlers/categorias.go` -- 3 handlers e mapeamento para 201/200/204/400/404/409 (envelope AD-14) -- fronteira HTTP
- `backend/main.go` -- 3 rotas atrás de `RequireRole(PapelAdm)` -- gate `adm`+
- `backend/services/categorias_test.go` + `backend/handlers/categorias_test.go` -- cobrir toda a matriz: sucesso, limites 8/50 (na service E direto no SQL provando o limite no banco), vazio, duplicado por código/nome, mesmo código em outra Empresa, 404 (alheio/inexistente/malformado), excluir em uso (409) e sem uso (204), 403 por papel, 401 sem token; seed intacto e `ProvisionarEmpresa` ainda copia 25 linhas
- `frontend/src/components/categorias/CategoriasSection.tsx` (+ `.test.tsx`) -- listar, cadastrar, editar inline, excluir com `ConfirmDialog`, erros 400/409/genérico -- UI `adm`+
- `frontend/src/pages/ConfiguracoesPage.tsx` (+ teste) -- montar a seção só para `adm` -- gate de papel também no cliente

**Acceptance Criteria:**
- Given um `adm` autenticado, when cadastra Categoria com código ≤ 8 e nome ≤ 50 caracteres, then ela é criada na Empresa dele e os limites são impostos pelo banco (tipos `VARCHAR(8)`/`VARCHAR(50)`), não só pelo handler.
- Given uma Categoria referenciada por ≥1 Produto, when o `adm` tenta excluí-la, then a exclusão é bloqueada (409) e a linha permanece.
- Given uma Categoria sem Produto, when o `adm` confirma no `ConfirmDialog`, then ela é removida.
- Given um Usuário abaixo de `adm`, when chama POST/PUT/DELETE, then 403; a seção nem aparece em `/configuracoes`.
- Given as 25 Categorias seed por Empresa, when a story é implantada, then continuam existindo (editáveis) e nenhum Produto muda; só o nome de `09.001` é encurtado para caber em 50.

## Spec Change Log

## Review Triage Log

### 2026-09-20 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 8 (high 0, medium 1, low 7)
- defer: 0
- reject: ~55 (ruído, estilo ou fora do intent: auditoria de mutações, concorrência otimista, `Content-Type`, contagem UTF-16 vs runes no `maxLength`, teste de corrida do `FOR UPDATE`, pré-check de linhas longas na migration etc.)
- addressed_findings:
  - `[medium]` `[patch]` Nenhum teste exercia as rotas POST/PUT/DELETE de `/api/categorias` pelo `newMux` real (os testes de handler montam mux próprio) — adicionado `TestNewMux_CategoriasEscritaCarregaRequireRoleAdm` em `backend/main_test.go` (401 sem token, 403 para `usuario`/`almoxarife`/`gestor`, `adm` passa do gate).
  - `[low]` `[patch]` `traduzirErroEscritaCategoria` rotulava qualquer 23505 fora do índice de código como "nome duplicado" — agora só os dois índices conhecidos viram `ErrCategoriaDuplicada`; outro 23505 segue como erro inesperado.
  - `[low]` `[patch]` Texto com byte NUL (SQLSTATE 22021) virava 500 — agora `ErrCategoriaValidacao` (400); caso acrescentado em `TestCriarCategoria_Validacao`.
  - `[low]` `[patch]` `ExcluirCategoria` ignorava erro de `RowsAffected()` — agora propaga o erro.
  - `[low]` `[patch]` `CategoriasSection`: DELETE 404 caía na mensagem genérica — agora "Categoria não encontrada. A lista foi atualizada."; teste novo. Teste novo também para PUT 404.
  - `[low]` `[patch]` `CategoriasSection`: alertas de ações anteriores (`erro`/`erroExclusao`) persistiam ao iniciar nova ação — agora são limpos no início de cadastrar/salvar/excluir.
  - `[low]` `[patch]` Linha de stub `/api/categorias` duplicada e mal indentada em `ConfiguracoesPage.test.tsx` — removida.

### 2026-09-20 — Review pass (follow-up)
- intent_gap: 0
- bad_spec: 0
- patch: 3 (high 0, medium 0, low 3)
- defer: 0
- reject: ~45 (fora do intent ou já decidido nele: pré-check/relatório de linhas longas na migration e guarda por nome exato do `09.001`, `Produtos: 1` no fallback de FK 23503 (o intent manda "residual também vira `ErroCategoriaEmUso`"), auditoria de mutações, `DisallowUnknownFields`/`Content-Type`/413, contagem UTF-16 vs runes no `maxLength`, corrida entre `carregar()`, foco/`aria-describedby`, invalidação de outras telas, testes de concorrência e do `.down`, unicidade case-insensitive etc.)
- addressed_findings:
  - `[low]` `[patch]` Byte NUL no `id` de PUT virava 400 (via 22021 -> `ErrCategoriaValidacao`) e no DELETE virava 500; o intent manda id malformado -> 404. `AtualizarCategoria`/`ExcluirCategoria` agora devolvem `ErrCategoriaNaoEncontrada` antes de tocar o banco; `validarCategoria` rejeita NUL em `codigo`/`nome`. Caso "id com NUL" acrescentado nos testes de atualizar e excluir.
  - `[low]` `[patch]` `CategoriasSection` exibia "Nenhuma categoria cadastrada ainda." antes da primeira resposta do GET — novo estado `carregou` só libera a mensagem depois do primeiro carregamento com sucesso.
  - `[low]` `[patch]` `categorias_padrao` (molde do provisionamento, parte explícita do intent) não tinha teste de limite no banco — `TestCategorias_LimitesNoBanco` agora insere os quatro casos (codigo 9, nome 51, vazios -> 22001/23514) e confere `VARCHAR(8)`/`VARCHAR(50)` também em `categorias_padrao`.

## Design Notes

`VARCHAR(50)` sozinho falharia no `ALTER` por causa de `09.001` (51 chars) e, se só `categorias` mudasse, `CopiarListasPadrao` passaria a violar o limite em toda Empresa nova — por isso rename + `categorias_padrao` na mesma migration. Endpoint de edição é `PUT` (o repo ainda não usa `PUT`; o `mux` do Go 1.22 e o proxy não restringem métodos). Sem evento realtime: as telas que consomem categorias buscam `GET /api/categorias` ao montar.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 -p 1 ./...` -- expected: tudo passa (inclui `empresas`, `isolamento`, `migracao_multi_empresa`).
- `cd frontend && npx vitest run CategoriasSection ConfiguracoesPage` -- expected: passa.
- `cd frontend && npm run build` -- expected: sem erros de tipo.

## Auto Run Result

**Resumo:** CRUD de Categorias para `adm`+ (`POST`/`PUT`/`DELETE` em `/e/{slug}/api/categorias`), sempre escopado por Empresa, com exclusão bloqueada (409 com contagem) quando há Produto referenciando, limites 8/50 impostos no banco (migration `000039`, também em `categorias_padrao` para o provisionamento de Empresa) e seção "Categorias" em `/configuracoes` visível só para `adm`+ (edição inline, `ConfirmDialog` na exclusão).

**Arquivos:**
- `backend/migrations/000039_limites_categorias.{up,down}.sql` — `VARCHAR(8)`/`VARCHAR(50)` + `CHECK` de não-vazio em `categorias` e `categorias_padrao`; renomeia o seed `09.001` (51 -> 49 caracteres).
- `backend/services/categorias.go` — criar/atualizar/excluir, erros de domínio, `FOR UPDATE` na exclusão; id com NUL -> 404.
- `backend/handlers/categorias.go`, `backend/main.go` — 3 handlers e rotas atrás de `RequireRole(PapelAdm)`.
- `backend/services/categorias_test.go`, `backend/handlers/categorias_test.go`, `backend/main_test.go` — matriz completa, limites direto no SQL (agora também em `categorias_padrao`), gate no `newMux` real.
- `frontend/src/components/categorias/CategoriasSection.tsx` (+ teste), `frontend/src/pages/ConfiguracoesPage.tsx` (+ teste) — UI e gate por papel; sem flash de "lista vazia" antes do primeiro carregamento.

**Review:** passada de follow-up: 3 patches aplicados (0 high, 0 medium, 3 low), 0 deferidos, ~45 rejeitados. (Passada anterior: 8 patches, 0 deferidos.) Follow-up recomendado: `false` (score 3×0 + 1×3 = 3 < 5).

**Verificação:** `go build ./... && go vet ./...` e `gofmt -l` limpos; `go test -count=1 -p 1` com Postgres real: todos os pacotes `ok` (`services` ~307s e `handlers` ~263s rodados separadamente por serem lentos); `npx vitest run CategoriasSection ConfiguracoesPage` 53/53; `npm run build` ok.

**Riscos residuais:**
- **Contradição no intent (decisão registrada):** a AC diz "seed intacto, sem migração de dado" e "limite de 50 no banco", mas o seed `09.001` tem 51 caracteres. Resolvido renomeando só essa linha ("para" -> "p/") em todas as Empresas e em `categorias_padrao`; vale confirmar com o dono do produto que o rótulo abreviado é aceitável.
- A migration falha (alto e claro) se alguma Empresa em produção tiver Categoria com código > 8 ou nome > 50 além de `09.001`, ou se o nome de `09.001` tiver sido editado e continuar > 50; convém checar antes do deploy na Ferreira Costa.
- A contagem "em uso" inclui Produtos removidos por soft-delete (a FK continua valendo); o fallback de FK 23503 informa 1 Produto mesmo que haja mais.
- Sem trilha de auditoria de criação/renomeação/exclusão de Categoria (fora do escopo do intent).
