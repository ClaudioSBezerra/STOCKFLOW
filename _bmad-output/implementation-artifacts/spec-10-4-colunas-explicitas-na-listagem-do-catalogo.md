---
title: 'Story 10.4: Colunas explícitas na listagem do Catálogo'
type: 'feature'
created: '2026-09-20'
status: 'done'
baseline_revision: 'bb9c5547e612929aed0a212b38619f3484c9b47f'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** A listagem do Catálogo mostra hoje só nome, categoria/código (grade) ou nome/dimensões/quantidade (tabela): o Usuário precisa abrir o detalhe para ver código, categoria, estoque total e embalagem+unidade (FR6/FR45, epic-10-context.md).

**Approach:** Estender as projeções da grade (`CatalogoItem`) e da tabela agrupada (`CatalogoGrupo`) com os campos que faltam e renderizá-los como campo de card / colunas próprias. Na tabela agrupada, um grupo pode cobrir Produtos distintos: quando divergem em código, categoria ou embalagem+unidade a coluna mostra "Múltiplos"; quando concordam, o valor comum (decisão do usuário, 2026-09-20 — sem alterar a chave de agrupamento da Story 4.3).

## Boundaries & Constraints

**Always:**
- `CatalogoItem` (grade) ganha `UnidadeMedida *string \`json:"unidadeMedida"\`` e `Embalagem *string \`json:"embalagem"\`` (ponteiro, `NULL` -> `null`, mesmo padrão de `Codigo`); código, nome, categoria e `quantidadeTotal` já existem.
- `CatalogoGrupo` ganha `Codigo *string`, `Categoria *Categoria`, `Embalagem *string`, `UnidadeMedida *string` e `Multiplos struct{ Codigo, Categoria, EmbalagemUnidade bool }` (`json:"multiplos"`). Quando o campo diverge no grupo, o valor vira `nil` e a flag correspondente `true`; quando todos os Produtos concordam, o valor comum (ou `nil`/`null` se comum for ausente) e flag `false`. Divergência é medida com `NULL` contando como um valor (Produto com código e Produto sem código = divergem); embalagem+unidade é comparada como PAR (embalagem, unidade_medida).
- A agregação é feita no SQL de `catalogoGrupoQueryBase` (`count(DISTINCT ...)` + `min(...)` sobre colunas de `p`/`c`; `c.id::text` porque `min(uuid)` não existe), sem tocar em `colunasChaveGrupo`/`GROUP BY`/`chave`/paginação/contagem de grupos. Os DOIS laços de scan que consomem essa query (`ListarCatalogoAgrupado` e `ListarTodosGruposCatalogo`) são atualizados; a exportação XLSX (`relatorios.go`) continua com as mesmas colunas de hoje.
- Frontend (`CatalogoListagem.tsx`): card da grade mostra explicitamente código (mono, quando houver), nome, categoria, estoque total (`formatarQuantidade(quantidadeTotal)`, rótulo "Estoque total") e embalagem+unidade; tabela agrupada ganha colunas próprias Código, Categoria e Embalagem/Unidade (Produto/nome, Dimensões, Quantidade = estoque total, Disponibilidade já existem), todas visíveis sem expandir; `colSpan` da linha expandida acompanha o número de colunas.
- Formatação de embalagem+unidade em `formatacao.tsx` (`formatarEmbalagemUnidade(embalagem, unidade)`): ambos -> "Caixa c/ 12 · un"; só unidade -> "— · un"; só embalagem -> só a embalagem; nenhum -> "—". Embalagem ausente nunca quebra o layout. Um valor `Multiplos` renderiza o texto literal "Múltiplos".

**Block If:** Nenhuma decisão bloqueante identificada (a ambiguidade de agregação foi resolvida em epics.md, Story 10.4 AC4).

**Never:** Não alterar a chave de agrupamento (nome + 5 dimensões), o filtro `montarFiltrosCatalogo`, a paginação, a exportação XLSX nem o handler de exportação. Não adicionar `codigo_fornecedor`/`ean13` à listagem (Never de spec-10-3). Não criar cálculo novo de estoque: `quantidadeTotal` já é a soma entre Estoques. Não introduzir navegação de linha na tabela agrupada (Story 4.4).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Grade, Produto completo | Produto com código, categoria, embalagem "Cx c/ 12", unidade "un", estoque somado 15 | Card mostra código, nome, categoria, "Estoque total 15", "Cx c/ 12 · un" | Sem erro |
| Grade, sem embalagem | `embalagem = NULL`, `unidade_medida = 'un'` | Embalagem/unidade mostra "— · un" | Layout intacto |
| Grade, sem embalagem nem unidade | ambos `NULL` (ex. importado) | Mostra "—" | Layout intacto |
| Tabela, grupo homogêneo | 2 Produtos mesmo nome+dimensões, mesmo código/categoria/embalagem+unidade | Colunas mostram o valor comum; `multiplos` todo `false` | Sem erro |
| Tabela, códigos divergem | mesmo grupo, códigos diferentes (ou um `NULL`) | Coluna Código = "Múltiplos" (`codigo=null`, `multiplos.codigo=true`) | Sem erro |
| Tabela, categorias divergem | mesmo grupo, categorias diferentes | Coluna Categoria = "Múltiplos" | Sem erro |
| Tabela, embalagem OU unidade divergem | mesmo grupo, pares (embalagem, unidade) diferentes | Coluna Embalagem/Unidade = "Múltiplos" | Sem erro |
| Tabela, grupo unitário | 1 Produto | Valores do próprio Produto, nunca "Múltiplos" | Sem erro |
| Tabela, estoque total | grupo com Produtos em 2 Estoques | Coluna Quantidade = soma entre Estoques (já existente), sem expandir | Sem erro |

</intent-contract>

## Code Map

- `backend/services/catalogo.go:49-57` (`CatalogoItem`) — acrescentar `UnidadeMedida`/`Embalagem *string`.
- `backend/services/catalogo.go:71-78` (`CatalogoGrupo`) — acrescentar `Codigo`, `Categoria`, `Embalagem`, `UnidadeMedida`, `Multiplos` (tipo novo `MultiplosGrupo`).
- `backend/services/catalogo.go:234-250` (`catalogoGradeQueryBase`) e scan em `ListarCatalogoGrade` (l.287-321) — acrescentar `p.unidade_medida, p.embalagem` (`sql.NullString` -> ponteiros, padrão de `ObterProdutoDetalhe`, l.706+).
- `backend/services/catalogo.go:369-389` (`catalogoGrupoQueryBase`) — acrescentar agregados `count(DISTINCT coalesce(p.codigo,''))`, `min(p.codigo)`, `count(DISTINCT c.id)`, `min(c.id::text)`, `min(c.codigo)`, `min(c.nome)`, `count(DISTINCT coalesce(p.embalagem,'') || chr(31) || coalesce(p.unidade_medida::text,''))`, `min(p.embalagem)`, `min(p.unidade_medida::text)` depois de `quantidade_total`/antes de `produto_ids` ou ao fim.
- `backend/services/catalogo.go:461-497` (`ListarCatalogoAgrupado`) e `:537-573` (`ListarTodosGruposCatalogo`) — scan dos novos agregados; extrair helper privado que converte os agregados em campos de `CatalogoGrupo` para não duplicar a regra "divergiu -> nil + flag".
- `backend/services/catalogo_test.go:255-322` (`TestListarCatalogoAgrupado_AgrupaPorNomeEDimensoes`) e helpers de seed no topo do arquivo — molde para os testes novos; `:675` (`TestObterProdutoDetalhe_SemUnidadeMedidaNemEmbalagem`) mostra como inserir Produto com/sem unidade/embalagem.
- `frontend/src/components/catalogo/formatacao.tsx` — `formatarEmbalagemUnidade` e constante `MULTIPLOS = 'Múltiplos'`.
- `frontend/src/components/catalogo/CatalogoListagem.tsx:84-107` (interfaces), `:510-568` (grade/tabela), `:599-656` (`FragmentLinhaGrupo`, `colSpan={5}`) — tipos, card e colunas novas.
- `frontend/src/components/catalogo/CatalogoListagem.test.tsx` — fixtures/asserts existentes a estender.

## Tasks & Acceptance

**Execution:**
- `backend/services/catalogo.go` -- estender `CatalogoItem`/`CatalogoGrupo`, queries e os três scans (grade, agrupado, todos-os-grupos) conforme Code Map.
- `backend/services/catalogo_test.go` -- testes: grade devolve `unidadeMedida`/`embalagem` (com valor e `nil`); grupo homogêneo devolve valores comuns; grupo com código divergente (incluindo um `NULL`), categoria divergente e par embalagem+unidade divergente devolve `nil` + flag; grupo unitário nunca `Multiplos`; `ListarTodosGruposCatalogo` também preenche os campos.
- `frontend/src/components/catalogo/formatacao.tsx` -- `formatarEmbalagemUnidade` + teste unitário dos 4 casos (se já existir arquivo de teste de formatação, estendê-lo; senão cobrir via `CatalogoListagem.test.tsx`).
- `frontend/src/components/catalogo/CatalogoListagem.tsx` -- card e tabela com as colunas novas, "Múltiplos" via `multiplos`, `colSpan` atualizado.
- `frontend/src/components/catalogo/CatalogoListagem.test.tsx` -- atualizar fixtures dos dois modos; asserts: card mostra código/categoria/"Estoque total"/embalagem+unidade; tabela tem cabeçalhos Código/Categoria/Embalagem e mostra valores sem expandir; "Múltiplos" nas 3 colunas; "—" com embalagem/unidade ausentes.

**Acceptance Criteria:**
- Given o Catálogo em grade, when um Usuário o acessa, then cada card mostra código, nome, categoria, estoque total (soma entre Estoques) e embalagem+unidade.
- Given o Catálogo em tabela agrupada, when um Usuário o acessa, then as mesmas informações aparecem como colunas próprias, sem expandir a linha.
- Given um Produto sem embalagem, when aparece na listagem, then a embalagem mostra traço/vazio sem quebrar o layout.
- Given um grupo que cobre Produtos com código, categoria ou embalagem+unidade divergentes, when listado, then a coluna correspondente mostra "Múltiplos"; se concordam, mostra o valor comum, sem alterar a chave de agrupamento.

## Spec Change Log

## Review Triage Log

### 2026-09-20 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 3 (high 0, medium 1, low 2)
- defer: 0
- reject: 17
- addressed_findings:
  - `[medium]` `[patch]` Nenhum teste fixava as tags JSON novas (só campos Go / fixtures do frontend) — adicionado `TestListarCatalogoHandler_ContratoJSONColunasExplicitas` em `backend/handlers/produtos_test.go`.
  - `[low]` `[patch]` Células Código/Categoria da tabela usavam `??` e mostrariam vazio para string vazia — trocado por `||` (mostra "—").
  - `[low]` `[patch]` `colSpan={8}` fixo — extraído para `COLUNAS_TABELA` ao lado do `<thead>`.

### 2026-09-20 — Review pass (revisão de acompanhamento)
- intent_gap: 0
- bad_spec: 0
- patch: 0
- defer: 0
- reject: 30
- addressed_findings:
  - none

## Design Notes

`min()` sobre a coluna só é o "valor comum" quando `count(DISTINCT ...) = 1`; nos demais casos o valor é descartado e a flag sobe, então o `min` arbitrário nunca vaza. `NULL` conta como valor distinto na contagem (via `coalesce(..., '')`), senão um grupo com um Produto sem código e outro com código mostraria falsamente o código como comum.

```go
// helper: n == 1 -> valor comum (pode ser nil); n > 1 -> nil + multiplo=true
```

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 -p 1 ./services/... ./handlers/...` -- expected: todos passam, incluindo os novos de catálogo.
- `cd frontend && npx vitest run CatalogoListagem CatalogoPage` -- expected: suíte passa.
- `cd frontend && npm run build` -- expected: sem erros de tipo.

## Auto Run Result

**Resumo:** grade e tabela agrupada do Catálogo mostram código, nome, categoria, estoque total e embalagem+unidade como campos/colunas próprias; na tabela agrupada, coluna cujos Produtos do grupo divergem mostra "Múltiplos" (decisão de 2026-09-20), senão o valor comum. Chave de agrupamento, filtros, paginação e exportação XLSX intactos. Esta execução foi uma passada de revisão de acompanhamento sobre a implementação já entregue (baseline `bb9c5547e612929aed0a212b38619f3484c9b47f`); nenhum código foi alterado.

**Arquivos (da implementação, sem mudança nesta passada):**
- `backend/services/catalogo.go` — `CatalogoItem`/`CatalogoGrupo` estendidos, `MultiplosGrupo`, agregados `count(DISTINCT)`+`min()` na query de grupos, helper `agregadosGrupo.aplicar`.
- `backend/services/catalogo_test.go` — testes de grade e de grupo (homogêneo, unitário, divergências, `ListarTodosGruposCatalogo`).
- `backend/handlers/produtos_test.go` — teste de contrato JSON das chaves novas.
- `frontend/src/components/catalogo/formatacao.tsx` — `formatarEmbalagemUnidade`, `MULTIPLOS`.
- `frontend/src/components/catalogo/CatalogoListagem.tsx` — card e colunas novas, `COLUNAS_TABELA`.
- `frontend/src/components/catalogo/CatalogoListagem.test.tsx` — fixtures e testes novos.

**Review:** 0 patches aplicados, 0 deferidos, 30 rejeitados (ruído/estilo, ou já cobertos: `NULL` vs `''` não ocorre pois `validarTextoLivreOpcional` normaliza vazio para `NULL`; `COLUNAS_TABELA` já extraído; deploy frontend-antes-do-backend já registrado como risco; skip de testes sem `DATABASE_URL` é convenção preexistente do repo). Follow-up recomendado: `false` (patches: 0 high, 0 medium, 0 low; score 0).

**Verificação:** `go build ./... && go vet ./...` limpos; `go test -count=1 -p 1 ./services/... ./handlers/...` ok (Postgres real); `npx vitest run CatalogoListagem CatalogoPage` 45/45; `npm run build` ok.

**Riscos residuais:** `produtos.codigo` é único por Empresa, então um grupo com 2+ Produtos só mostra código comum se todos forem `NULL` — na prática a coluna Código vira "Múltiplos" em grupos multi-Produto (consequência da decisão do usuário). Frontend lê `multiplos` sem guarda: deploy do frontend antes do backend quebra a tabela agrupada até o backend subir.
