---
title: 'Story 10.3: Código do Fornecedor, EAN-13, Unidade de Medida e Embalagem'
type: 'feature'
created: '2026-09-20'
status: 'done'
baseline_revision: 'c5b7ea4385e8358ac333c44241df6f97afa1d2c7'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** `produtos` não tem Código do Fornecedor, EAN-13, Unidade de Medida nem Embalagem — o cadastro (`CriarProduto`) não aceita nenhum dos quatro, então o Almoxarife não consegue registrar informações reais de compra/logística já usadas no dia a dia (FR45/FR46, epic-10-context.md).

**Approach:** Quatro colunas aditivas em `produtos`: `codigo_fornecedor`/`ean13`/`embalagem` texto livre opcional (sem unicidade), `unidade_medida` um enum fechado (`addendum.md` §F) obrigatório só para Produto NOVO cadastrado pelo formulário. `CriarProduto` valida e grava os quatro; `ObterProdutoDetalhe` (FR7) devolve `unidade_medida`/`embalagem` como campos próprios.

## Boundaries & Constraints

**Always:**
- Migration `000038`: `ALTER TABLE produtos ADD COLUMN codigo_fornecedor VARCHAR(255), ADD COLUMN ean13 CHAR(13), ADD COLUMN unidade_medida unidade_medida_produto, ADD COLUMN embalagem VARCHAR(255)` — todas NULLable; tipo `unidade_medida_produto` é um `CREATE TYPE ... AS ENUM` com exatamente `'un','m','m²','m³','kg','L','cx','rolo','barra','mm','cm','kg/m²'` (addendum.md §F, mesma grafia). Backfill na mesma migration: `UPDATE produtos SET unidade_medida = 'un' WHERE unidade_medida IS NULL` — roda ANTES de qualquer obrigatoriedade valer no cadastro (AD-32).
- `CriarProduto` (`backend/services/produtos.go`) valida, ANTES de abrir a `tx` (mesmo ponto das demais validações): `codigo_fornecedor`/`embalagem` trimados, cada um <=255 runas se não vazios (senão `ErroProdutoValidacao` citando o campo — mesma razão de `limiteNumeric103`: evitar 500 de "value too long"); `unidade_medida` trimada, não pode ser vazia (`ErroProdutoValidacao` "unidade de medida é obrigatória") e deve estar no conjunto fechado dos 12 valores (senão "unidade de medida inválida"); `ean13` trimado — vazio é sempre válido (NULL), não-vazio deve ter exatamente 13 caracteres ASCII `0-9` com dígito verificador correto (algoritmo EAN-13 padrão: soma ponderada 1/3 alternada nas 12 primeiras posições, dígito = `(10 - soma%10) % 10`), senão `ErroProdutoValidacao` nomeando o problema (formato vs. dígito verificador, mesmo estilo de `ValidarCNPJ`/`digitoVerificadorCNPJ`, `backend/services/empresas.go:117-149`).
- INSERT de `CriarProduto` grava os quatro campos (`codigo_fornecedor`, `ean13 sql.NullString`, `unidade_medida`, `embalagem sql.NullString`) na mesma transação/INSERT já existente.
- `ObterProdutoDetalhe`/`ProdutoDetalhe` (`backend/services/catalogo.go`) ganham `UnidadeMedida *string \`json:"unidadeMedida"\`` e `Embalagem *string \`json:"embalagem"\`` (mesmo padrão ponteiro de `Codigo`) — `NULL` no banco (Produto legado ainda não passado pelo backfill, ou criado via importação) vira `null` no JSON, nunca "un" forçado.
- `handlers/produtos.go` (`criarProdutoRequest`): acrescenta `CodigoFornecedor`, `EAN13`, `UnidadeMedida`, `Embalagem` (`json:"codigo_fornecedor"`/`"ean13"`/`"unidade_medida"`/`"embalagem"`), repassados 1:1 para `services.CriarProdutoInput`.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx`: quatro campos novos no formulário — `Input` "Código do Fornecedor" e "EAN-13" (texto livre, opcionais), `Select` "Unidade de Medida" com as 12 opções fixas (mesma lista do enum, obrigatório — entra em `desabilitado`), `Input` "Embalagem" (texto livre, opcional). Nenhuma validação de formato de EAN-13 no cliente — o servidor é a única fonte de verdade (mesmo princípio já usado para o formato de nome/template).

**Block If:** Nenhuma decisão bloqueante identificada.

**Never:** Não alterar `backend/services/importacoes.go`/`processarProximaLinha` — o INSERT da importação em massa (Story 3.3/3.4) não preenche nenhum dos 4 campos novos, fica fora do escopo desta story (mesmo raciocínio de `codigo`, Story 10.2), e é exatamente por isso que `unidade_medida` PERMANECE NULLable no banco (nunca `NOT NULL`): um `NOT NULL` quebraria esse INSERT. Não adicionar `codigo_fornecedor`/`ean13` a `ProdutoDetalhe` nem à listagem do Catálogo — a AC5 desta story (epics.md:1553-1555) e o requisito de listagem em `epic-10-context.md:26` citam explicitamente só Unidade de Medida e Embalagem para round-trip via API/Catálogo; os outros dois ficam gravados, sem endpoint de leitura nesta story (a formulação "ambos os pares" de AD-32 é mais ampla que o corte confirmado nas stories — ver Design Notes). Não criar endpoint de edição para nenhum dos 4 campos (não existe formulário geral de edição de Produto hoje, só `AtualizarNomeProduto`/renomear). Não checar unicidade de `codigo_fornecedor`/`ean13`. Não relacionar nenhum dos dois com `produtos.codigo` (Story 10.2) nem com o Código de Identificação de QR/código de barras (Story 4.5).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Fornecedor e EAN-13 informados | `codigo_fornecedor="ABC-123"`, `ean13="7891234567895"` (dígito verificador correto) | Ambos gravados, `codigo_fornecedor`/`ean13 sql.NullString.Valid=true` | Sem erro |
| Fornecedor/EAN-13 ausentes | ambos campos vazios/omitidos | Gravados como `NULL` | Sem erro — nunca rejeitado |
| EAN-13 com dígito verificador errado | `ean13="7891234567890"` (13 dígitos, checksum não confere) | Nada é gravado | 400 `ErroProdutoValidacao` "EAN-13 inválido: dígito verificador não confere" |
| EAN-13 com tamanho/caracteres errados | `ean13="123"` ou `"789123456789A"` | Nada é gravado | 400 `ErroProdutoValidacao` "EAN-13 deve ter 13 dígitos" |
| Unidade de Medida ausente no cadastro | `unidade_medida=""` | Nada é gravado | 400 `ErroProdutoValidacao` "unidade de medida é obrigatória" |
| Unidade de Medida fora do enum | `unidade_medida="litro"` | Nada é gravado | 400 `ErroProdutoValidacao` "unidade de medida inválida" |
| Embalagem ausente | `embalagem=""` | Gravada como `NULL` | Sem erro — sempre opcional |
| Produto legado sem Unidade de Medida (pré-migration) | migration `000038` roda | `unidade_medida="un"` para toda linha `NULL`, `embalagem` continua `NULL` | Sem erro |
| Detalhe de Produto com campos preenchidos | `GET /api/produtos/{id}` | `unidadeMedida`/`embalagem` retornam como string | Sem erro |
| Detalhe de Produto criado via importação (sem unidade/embalagem) | `GET /api/produtos/{id}` | `unidadeMedida`/`embalagem` retornam `null` | Sem erro |

</intent-contract>

## Code Map

- `backend/migrations/000038_add_fornecedor_ean_unidade_embalagem_to_produtos.up.sql`/`.down.sql` (novos) — `CREATE TYPE unidade_medida_produto`, `ALTER TABLE produtos ADD COLUMN` dos 4 campos, backfill `unidade_medida='un'`; `.down.sql` reverte as 4 colunas e o `DROP TYPE`.
- `backend/services/produtos.go:18-22` (`unidadesDimensaoValidas`) — acrescentar `unidadesMedidaValidas`, mesmo molde (`map[string]bool`) com os 12 valores do enum.
- `backend/services/produtos.go:78-90` (`CriarProdutoInput`) — acrescentar `CodigoFornecedor`, `EAN13`, `UnidadeMedida`, `Embalagem string`.
- `backend/services/produtos.go` (novas funções privadas, ao lado de `validarDimensao`) — `validarEAN13(ean13 string) (sql.NullString, error)` (algoritmo de dígito verificador) e uma validação inline de `unidade_medida`/`codigo_fornecedor`/`embalagem` (trim + limite de 255 runas + pertencimento ao enum).
- `backend/services/produtos.go:228-254` (`CriarProduto`, bloco de validação antes da `tx`) — chamar as validações novas, na mesma ordem das existentes.
- `backend/services/produtos.go:335-358` (`insertProduto`) — acrescentar as 4 colunas/params ao INSERT (Produto struct/`RETURNING` continuam só `id, nome, codigo` — os 4 campos novos não voltam na resposta de criação, só via detalhe).
- `backend/services/catalogo.go:684-693` (`ProdutoDetalhe`) — acrescentar `UnidadeMedida *string` e `Embalagem *string`.
- `backend/services/catalogo.go:660-676` (`produtoDetalheQuery`) — acrescentar `p.unidade_medida, p.embalagem` ao `SELECT`.
- `backend/services/catalogo.go:696-717` (`ObterProdutoDetalhe`) — `Scan` dos 2 novos campos (via `sql.NullString`, mesmo padrão de `codigo`) e preenchimento dos ponteiros em `det`.
- `backend/handlers/produtos.go:77-89` (`criarProdutoRequest`) — acrescentar os 4 campos com as tags `json` em snake_case.
- `backend/handlers/produtos.go:124-136` (montagem de `services.CriarProdutoInput`) — repassar os 4 campos novos.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:106` (`UNIDADES`) — acrescentar `UNIDADES_MEDIDA` com os 12 valores do enum (mesma ordem do backend).
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:204-214` (estados) — acrescentar `codigoFornecedor`, `ean13`, `unidadeMedida`, `embalagem`.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:292-304` (`limparFormulario`) — resetar os 4 novos estados.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:308-314` (`desabilitado`) — acrescentar `unidadeMedida === ''`.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:326-341` (corpo do `POST`) — acrescentar os 4 campos (`codigo_fornecedor`/`ean13`/`embalagem` só quando não vazios, mesmo padrão de `observacoes`; `unidade_medida` sempre).
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:498-521` (JSX, entre Código e Categoria) — 4 novos campos: `Input` Código do Fornecedor, `Input` EAN-13, `Select` Unidade de Medida (12 `SelectItem`), `Input` Embalagem.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000038_*.up.sql`/`.down.sql` — criar enum + 4 colunas + backfill, conforme Boundaries.
- `backend/services/produtos.go` — `unidadesMedidaValidas`, `validarEAN13`, validação de `codigo_fornecedor`/`embalagem`/`unidade_medida`, e gravação dos 4 campos no INSERT de `CriarProduto`.
- `backend/services/catalogo.go` — `ProdutoDetalhe.UnidadeMedida`/`.Embalagem`, query e `Scan` correspondentes.
- `backend/handlers/produtos.go` — `criarProdutoRequest` + repasse para `CriarProdutoInput`.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx` — 4 campos novos no formulário, estados, `limparFormulario`, `desabilitado`, corpo do POST.
- `backend/services/produtos_test.go` — estender `TestCriarProduto_SucessoCompleto` com os 4 campos novos (assert via `SELECT` direto em `produtos` para `codigo_fornecedor`/`ean13`, via `ObterProdutoDetalhe` para `unidade_medida`/`embalagem`, já que `Produto` não os expõe); acrescentar `TestCriarProduto_EAN13Invalido` (tabela: tamanho errado, caractere não-dígito, dígito verificador errado), `TestCriarProduto_EAN13VazioAceito`, `TestCriarProduto_UnidadeMedidaObrigatoria`, `TestCriarProduto_UnidadeMedidaInvalida`, `TestCriarProduto_CodigoFornecedorEEmbalagemAcimaDoLimiteRejeitados` — cobre a I/O Matrix.
- `backend/services/catalogo_test.go` — estender o teste de `ObterProdutoDetalhe` (seed com os 4 campos) para afirmar `unidadeMedida`/`embalagem` no retorno; acrescentar um caso com Produto inserido via SQL direto sem `unidade_medida`/`embalagem` (simulando o caminho de importação) confirmando `nil`/`null` nos dois ponteiros.
- `frontend/src/components/produtos/CadastroProdutoSection.test.tsx` — acrescentar um teste que preenche os 4 campos novos e confirma o payload enviado a `POST /api/produtos`; confirmar que o botão continua desabilitado com Unidade de Medida vazia.

**Acceptance Criteria:**
- Given o formulário de cadastro de Produto, when o Almoxarife informa Código do Fornecedor e/ou EAN-13 válido, then ambos são salvos como opcionais, sem checagem de unicidade e sem relação com `produtos.codigo` (FR45).
- Given um EAN-13 informado com formato ou dígito verificador inválido, when o Almoxarife salva, then o cadastro é rejeitado nomeando o problema — campo vazio nunca é rejeitado.
- Given o cadastro de um Produto novo sem Unidade de Medida, when o Almoxarife salva, then o cadastro é rejeitado — Embalagem continua opcional no mesmo cadastro.
- Given a migration `000038` rodando contra Produtos já em produção, when ela termina, then todo Produto sem Unidade de Medida recebe `"un"` em lote, e só depois a obrigatoriedade vale no cadastro novo.
- Given Unidade de Medida e Embalagem persistidas, when consultadas via `GET /api/produtos/{id}`, then retornam como campos próprios (`unidadeMedida`/`embalagem`), sem transformação.

## Design Notes

AD-32 (architecture-spine) descreve o requisito de exposição como "ambos os pares aparecem como colunas próprias no Catálogo e no detalhe" — lido isoladamente, isso incluiria Código do Fornecedor/EAN-13. As stories confirmadas (epics.md, Story 10.3 AC5 e Story 10.4, e `epic-10-context.md:26`, que lista literalmente "código, nome, categoria, estoque total... e embalagem+unidade" como colunas do Catálogo) restringem esse round-trip só a Unidade de Medida/Embalagem — corte mais específico e mais recente que a formulação geral da arquitetura. Esta spec segue o corte das stories: Código do Fornecedor/EAN-13 ficam gravados, sem superfície de leitura nesta epic.

`ean13` usa `CHAR(13)` (não `VARCHAR`) por seguir literalmente AD-32; como todo valor gravado já tem exatamente 13 caracteres (validado antes do INSERT), não há diferença prática de padding.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -p 1 ./services/... ./handlers/...` -- expected: todos os testes passam, incluindo os novos de EAN-13/Unidade de Medida.
- `cd frontend && npx vitest run CadastroProdutoSection` -- expected: suíte passa, incluindo o novo teste dos 4 campos.
- `cd frontend && npm run build` -- expected: sem erros de tipo.

## Auto Run Result

O `bmad-loop` (run `20260920-033814-f90b`) parou com a story em `in-review`
no frontmatter, sem evidência de um segundo agente de review ter rodado sobre o
diff (as duas tentativas anteriores, em outros runs, travaram por sessão antes
de produzir spec). Recuperação manual (Claude, 2026-09-20):

- `go build ./... && go vet ./...`: sem erros.
- `go test -count=1 -p 1 ./services/... ./handlers/... ./cmd/...` (Postgres real, sem cache): `ok` em `services` (191s), `handlers` (178s) e nos 4 pacotes de `cmd/`.
- `npx vitest run CadastroProdutoSection`: 30/30 passam. `npm run build`: sem erros de tipo.
- Spot-check contra o `intent-contract`: migration 000038 (enum de 12 valores, 4 colunas NULLable, backfill `unidade_medida='un'`, `.down.sql` reversível); `validarEAN13` (13 dígitos ASCII, dígito verificador 1/3 alternado, vazio = NULL); `unidade_medida` continua NULLable no banco para não quebrar o INSERT da importação em massa.
- **Honestidade:** não houve review adversarial independente. Recomendo `bmad-code-review` nas Stories 10.1-10.3 antes do deploy.
