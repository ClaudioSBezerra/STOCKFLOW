---
title: 'Story 6.4 — Mesclagem de duplicatas com trilha de auditoria'
type: 'feature'
created: '2026-09-02'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '2b6bfa397853fa4e227dd1618b24b7a26361e588'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-6-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** A detecção de duplicatas (Story 6.3) só aponta grupos candidatos — nada no sistema consolida um grupo num único registro, então o catálogo continua com Produtos redundantes e reimportações continuam acumulando linhas duplicadas.

**Approach:** `services.MesclarDuplicatas` mescla um grupo detectado num único Produto escolhido pelo Almoxarife: soma a quantidade dos demais no Produto mantido, reescreve `produto_id` nas Movimentações históricas dos removidos, soft-deleta os removidos (`produtos.deleted_at`, coluna nova) e grava trilha de auditoria permanente (quem, quando, produtos removidos, quantidade). Exposto via `POST /api/normalizacao/mesclar`; `DuplicatasSection` ganha seleção de "manter este" por grupo + confirmação via `ConfirmDialog`.

## Boundaries & Constraints

**Always:**
- `POST /api/normalizacao/mesclar` atrás do mesmo gate `RequireAuth`+`RequireRole(almoxarife)` das outras rotas de Normalização.
- O serviço NUNCA confia na lista de ids enviada pelo cliente: revalida, dentro da mesma transação e sob lock, que `produtoMantidoId`+`produtoRemovidoIds` formam um grupo de duplicata válido nos MESMOS critérios de `DetectarDuplicatas` (nome normalizado igual, 5 dimensões equivalentes par a par, interseção total de locais) e que nenhum já está soft-deletado — grupo inválido ou membro já mesclado aborta tudo (nada é escrito).
- A quantidade somada é sempre recalculada a partir do banco DENTRO da transação (nunca aceita um total pré-calculado do cliente) — resolve por construção o requisito de nunca usar um snapshot antigo.
- Lock ordering (AD-10): as linhas de `produtos` (mantido+removidos) são travadas em ordem ascendente de `id`; o conjunto completo de pares `(produto_id, estoque_id)` tocados (mantido + todos os removidos) é ordenado ascendente antes de qualquer lock, reusando `travarLinhaProdutoEstoque`.
- `produto_id` das linhas de `movimentacoes` dos removidos é reescrito para o mantido ANTES do soft-delete, na mesma transação — preserva "soma de Movimentações == quantidade atual" (AD-11) sem inserir uma Movimentação nova (ver Design Notes).
- Linhas de `produto_estoque` dos removidos são deletadas depois de suas quantidades serem somadas na linha correspondente do mantido — evita resíduo fantasma em relatórios/guards que ainda fazem `JOIN produtos`.
- Soft-delete via `produtos.deleted_at = now()`; toda leitura existente de `produtos` passa a filtrar `deleted_at IS NULL` (ver Code Map) — um Produto mesclado nunca mais aparece em busca, catálogo, detalhe, importação por código ou nova detecção de duplicatas.
- Fotos do(s) Produto(s) removido(s) permanecem em disco, sob o id antigo, sem qualquer alteração — auditoria permanente (AD-11).
- Auditoria (`mesclagens_duplicatas` + `mesclagem_produtos_removidos`) é gravada na MESMA transação da mesclagem; nenhuma rotina de expurgo/retenção toca essas tabelas.
- Após commit: publica `{"resource":"produtos","change":"updated"}` para o mantido, `{"resource":"produtos","change":"deleted"}` para cada removido, e um único `{"resource":"movimentacoes","change":"updated"}` (mesmo padrão de payload mínimo — clientes re-buscam via GET).

**Block If:** nada nesta story depende de decisão humana ou de ação de operador fora do repositório — schema (migration nova), serviço, handler, rota e UI são inteiramente implementáveis com o estado atual do repositório.

**Never:**
- Redirecionar item de Carrinho ou Pedido pendente para o Produto mantido — `pedidos`/`pedido_itens`/Carrinho não existem no código ainda (Epic 7 é backlog); não há tabela para reescrever hoje. Quando Epic 7 existir, tratar um `produto_id` soft-deletado como referência quebrada é responsabilidade daquela story (mesmo padrão do AC de Story 7.1), não desta.
- Reescrever `PEDIDO_ITENS` — mesma razão (tabela inexistente).
- Inserir uma nova linha em `movimentacoes` (tipo `ajuste`) para a consolidação de quantidade — a rescrita de `produto_id` nas linhas históricas do removido já preserva a invariante sozinha; uma linha extra duplicaria a contagem (Design Notes).
- Renomear/mover arquivos de foto do removido para o id do mantido.
- Reexecutar `DetectarDuplicatas` sobre todo o catálogo na confirmação — revalida só o grupo especificamente submetido.
- Tela ou endpoint de consulta da trilha de auditoria — nenhuma AC pede uma UI de leitura; a tabela existe para investigação futura direta no banco.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Mesclagem simples | Grupo A(qtd 5, Estoque X) + B(qtd 3, Estoque X); Almoxarife mantém A | `produto_estoque(A,X).quantidade=8`; B com `deleted_at` setado; 1 linha em `mesclagens_duplicatas` + 1 em `mesclagem_produtos_removidos` | Nenhum erro |
| Removido com histórico de Movimentações | B tem 2 linhas em `movimentacoes` | as 2 linhas passam a `produto_id=A`, antes do soft-delete de B | Nenhum erro |
| Grupo de 3+, locais em interseção total | A, B, C mantidos juntos por interseção total (Story 6.3) | quantidades de B e C somadas em A; B e C soft-deletados | Nenhum erro |
| Produto já mesclado por execução concorrente | B já está com `deleted_at` setado quando a transação tenta travar | rollback completo, nada escrito | `409 CONFLICT` |
| Grupo não é mais válido | Dimensão de B foi corrigida (Story 6.2) entre a listagem e a confirmação, deixando de ser equivalente à de A | rollback completo, nada escrito | `409 CONFLICT` |
| Papel `usuario` chama a rota diretamente | `POST /api/normalizacao/mesclar` sem `almoxarife`+ | — | `403 FORBIDDEN` |
| Falha de banco durante a transação | Conexão cai no meio | rollback completo, nenhum estado parcial | `500 INTERNAL_ERROR` |

</intent-contract>

## Code Map

- `backend/migrations/000024_create_mesclagem_duplicatas.up.sql` (novo) -- `ALTER TABLE produtos ADD COLUMN deleted_at TIMESTAMPTZ;` + `CREATE TABLE mesclagens_duplicatas (id UUID PK, produto_mantido_id UUID NOT NULL REFERENCES produtos(id), usuario_id UUID NOT NULL REFERENCES usuarios(id), criado_em TIMESTAMPTZ NOT NULL DEFAULT now())` + `CREATE TABLE mesclagem_produtos_removidos (id UUID PK, mesclagem_id UUID NOT NULL REFERENCES mesclagens_duplicatas(id), produto_removido_id UUID NOT NULL REFERENCES produtos(id), quantidade_consolidada NUMERIC(10,3) NOT NULL CHECK (>=0), PRIMARY KEY (mesclagem_id, produto_removido_id))` + índice em `produto_removido_id`; sem `ON DELETE CASCADE` em nenhuma FK (auditoria permanente, mesmo padrão de `movimentacoes`); comentário no estilo de `000023_create_normalizacao_ignoradas.up.sql` explicando "nunca expurgada".
- `backend/migrations/000024_create_mesclagem_duplicatas.down.sql` (novo) -- drop das 2 tabelas (ordem: filha antes da pai) + `ALTER TABLE produtos DROP COLUMN deleted_at`.
- `backend/services/normalizacao.go` -- acrescentar `MesclarDuplicatas(db *sql.DB, produtoMantidoID string, produtoRemovidoIDs []string, usuarioID string) (ResultadoMesclagem, error)`: valida shape (mantido não-vazio, >=1 removido, sem duplicatas, mantido não está entre os removidos) -> `*ErroProdutoValidacao`; abre transação; trava `produtos` (mantido+removidos) via `SELECT ... WHERE id = ANY($1) ORDER BY id FOR UPDATE`, confere `deleted_at IS NULL` em todos -> senão `*ErroMesclagemInvalida`; revalida o grupo reusando `normalizarNomeProduto`, `dimensoesEquivalentes`, `carregarLocaisProduto`+`interseccaoTotalNaoVazia` (mesmos helpers de Story 6.3) sobre o conjunto submetido -> inválido gera `*ErroMesclagemInvalida`; monta o conjunto de pares `(produto_id,estoque_id)` tocados (mantido+removidos), ordena ascendente, trava cada um via `travarLinhaProdutoEstoque` (movimentacoes.go); soma por `estoque_id` a quantidade dos removidos, `UPDATE produto_estoque` incremental na linha do mantido (criando-a se não existir, mesmo upsert de `travarLinhaProdutoEstoque`), depois `DELETE FROM produto_estoque WHERE produto_id = ANY(removidos)`; `UPDATE movimentacoes SET produto_id = $mantido WHERE produto_id = ANY($removidos)`; `UPDATE produtos SET deleted_at = now() WHERE id = ANY($removidos)`; 1 INSERT em `mesclagens_duplicatas` + N INSERTs em `mesclagem_produtos_removidos` (quantidade total por removido, somada por todos os seus `estoque_id`); commit; devolve `ResultadoMesclagem{ProdutoMantidoID, ProdutosRemovidosIDs []string, QuantidadeConsolidada float64}`.
- `backend/services/normalizacao.go` (novo tipo) -- `ErroMesclagemInvalida{Motivo string}` com `Error() string`, mesmo padrão de `ErroEstoqueComResiduo`/`ErroProdutoValidacao` -- cobre "produto já mesclado" e "grupo não é mais válido".
- `backend/services/normalizacao.go:235` (`AnalisarInconsistencias`, query `FROM produtos`) -- acrescentar `WHERE deleted_at IS NULL`.
- `backend/services/normalizacao.go:842` (`DetectarDuplicatas`, query `FROM produtos`) -- acrescentar `WHERE deleted_at IS NULL` -- Produto já mesclado nunca reentra em novo grupo (AC explícita).
- `backend/services/movimentacoes.go:251` (`travarLinhaProdutoEstoque`) -- reusar sem alteração para os locks/upsert de `produto_estoque` da consolidação.
- `backend/services/catalogo.go:165` (`montarFiltrosCatalogo`) -- sempre incluir `p.deleted_at IS NULL` como primeira condição da lista (deixa de ser opcional); as 3 call-sites (`:242` grade, `:413` grupo, `:501` export/relatório) herdam o filtro automaticamente, sem mudança própria.
- `backend/services/catalogo.go:654-660` (`produtoDetalheQuery`) -- acrescentar `AND p.deleted_at IS NULL` ao `WHERE p.id = $1` -- detalhe de Produto mesclado passa a 404, disparando a tela "Produto não encontrado." já existente em `ProdutoDetalhePage.tsx:144/277`.
- `backend/services/produtos.go:438` (`buscarProdutosQuery`) -- acrescentar `AND p.deleted_at IS NULL` ao WHERE (busca, Story 4.1, não deve sugerir Produto mesclado).
- `backend/services/produtos.go:494` (`buscarProdutoPorCodigoQuery`) -- acrescentar `AND p.deleted_at IS NULL` (leitura de QR/código, Story 4.5, não deve resolver Produto mesclado).
- `backend/services/importacoes.go:631` (`selectProdutoPorCodigo`) -- acrescentar `AND deleted_at IS NULL`; sem essa condição, reimportar uma planilha com o código do Produto removido ressuscitaria seus dados via `processarProximaLinha` em vez de tratar como Produto novo/inexistente.
- `backend/services/estoques.go:155` (`selectResiduo`, guard de `ExcluirEstoque`) -- acrescentar `AND p.deleted_at IS NULL` por defensividade (as linhas de `produto_estoque` dos removidos já são deletadas pela mesclagem; o filtro fecha o caso se a ordem de operações mudar no futuro).
- `backend/handlers/normalizacao.go` -- acrescentar `mesclarDuplicatasRequest{ProdutoMantidoID string; ProdutoRemovidoIDs []string}` e `MesclarDuplicatasHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc`, molde de `AplicarCorrecoesHandler` (guarda de sessão -> 500; `json.Decode` -> 400 VALIDATION_ERROR; chama `services.MesclarDuplicatas` com `usuario.ID`); sucesso -> `200 {"produtoMantidoId","produtosRemovidosIds","quantidadeConsolidada"}` + os 3 `registro.Publish(...)` descritos em Always; `errors.As(&erroValidacao)` -> 400; `errors.As(&erroMesclagemInvalida)` -> 409 CONFLICT; default -> 500.
- `backend/main.go:580` (logo após a rota `GET /api/normalizacao/duplicatas`) -- registrar `POST /api/normalizacao/mesclar`, mesmo gate `RequireAuth`+`RequireRole(almoxarife)`, passando `registro`.
- `backend/services/normalizacao_test.go` -- `TestMesclarDuplicatas_*` cobrindo a I/O Matrix.
- `backend/handlers/normalizacao_test.go` -- `TestMesclarDuplicatasHandler_*`: 200, 400 payload inválido, 403 papel usuario (`criarContaComPapel`+`tokenDeLogin`, mesmo padrão de `TestRegistrarBaixaHandler_403PapelUsuario`), 401, 409.
- `frontend/src/components/normalizacao/DuplicatasSection.tsx` -- cada `<tr>` de produto (~linhas 143-148) ganha um rádio "manter este" (`name={chaveDoGrupo}`); grupo ganha um botão "Mesclar" (após `</table>`, ~linha 150) desabilitado até haver seleção; clique abre `ConfirmDialog` citando o nome do mantido e a lista dos removidos ("Esta ação não pode ser desfeita."); `onConfirm` faz `POST /api/normalizacao/mesclar`, remove o grupo de `grupos` (estado local) no sucesso e mostra retorno citando `quantidadeConsolidada`; erro `409` mostra alerta e mantém o grupo (Almoxarife pode reabrir a análise).
- `frontend/src/components/normalizacao/DuplicatasSection.test.tsx` -- cobre seleção de rádio, botão desabilitado sem seleção, fluxo de confirmação via `ConfirmDialog`, sucesso remove o grupo da lista, `409` mostra erro sem remover o grupo.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000024_create_mesclagem_duplicatas.{up,down}.sql` -- schema novo (coluna + 2 tabelas de auditoria) -- fundação da story.
- `backend/services/normalizacao.go` -- `MesclarDuplicatas`, `ErroMesclagemInvalida`, filtros `deleted_at` em `AnalisarInconsistencias`/`DetectarDuplicatas` -- núcleo da regra de negócio.
- `backend/services/catalogo.go`, `produtos.go`, `importacoes.go`, `estoques.go` -- filtros `deleted_at IS NULL` nas leituras existentes de `produtos` -- fecha o soft-delete em toda a superfície já implementada.
- `backend/handlers/normalizacao.go` -- `MesclarDuplicatasHandler` -- fronteira HTTP + publicação em tempo real.
- `backend/main.go` -- registrar `POST /api/normalizacao/mesclar`.
- `backend/services/normalizacao_test.go`, `backend/handlers/normalizacao_test.go` -- I/O Matrix completa + 403/401/409.
- `frontend/src/components/normalizacao/DuplicatasSection.tsx` -- seleção "manter este" + botão "Mesclar" + `ConfirmDialog`.
- `frontend/src/components/normalizacao/DuplicatasSection.test.tsx` -- cobre os cenários citados no Code Map.

**Acceptance Criteria:**
- Given um grupo de Produtos duplicados (Story 6.3), when o Almoxarife confirma a mesclagem (via `ConfirmDialog`) escolhendo qual Produto mantém, then a quantidade dos demais é somada no Produto mantido, os demais são soft-deletados (`deleted_at`), e uma auditoria é registrada (quem, quando, produtos removidos, valores) em tabela permanente, nunca expurgada.
- Given as linhas históricas de Movimentações dos Produtos removidos, when a mesclagem é confirmada, then o `produto_id` dessas linhas é reescrito para o Produto sobrevivente antes do soft-delete, preservando "soma de Movimentações == quantidade atual".
- Given a quantidade somada calculada no momento da revisão, when o Almoxarife confirma a mesclagem depois de um tempo, then a quantidade é revalidada na confirmação, nunca usa um snapshot antigo.
- Given um Produto já soft-deletado por uma mesclagem anterior, when ele aparece em uma nova análise de duplicatas ou é alvo de uma nova tentativa de mesclagem, then ele nunca reentra em uma nova mesclagem, mas sua foto permanece em disco para auditoria permanente.

## Design Notes

**Sem Movimentação `ajuste` nova para a consolidação:** a AC exige reescrever `produto_id` nas linhas históricas do removido para o mantido ANTES do soft-delete. Essa rescrita, sozinha, já preserva "soma de Movimentações == quantidade atual": se `soma(Movimentações do mantido para Estoque X) == quantidade_antiga(mantido,X)` e `soma(Movimentações do removido para Estoque X) == quantidade_antiga(removido,X)`, então, depois de relabelar as linhas do removido para o `produto_id` do mantido, `soma(Movimentações agora com produto_id=mantido, Estoque X) == quantidade_antiga(mantido,X) + quantidade_antiga(removido,X)`, que é exatamente a nova `quantidade` do mantido após a consolidação. Inserir uma Movimentação `ajuste` adicional para o mesmo valor duplicaria a soma. AD-10 ("toda escrita em `quantidade` insere uma Movimentação") rege eventos de negócio novos (baixa/transferência/correção manual) — a consolidação de merge é realocação de titularidade de saldo já auditado, não um evento novo.

**Revalidação do grupo reusa os helpers de Story 6.3, não `DetectarDuplicatas` inteira:** rodar a detecção completa sobre ~8.000 Produtos a cada confirmação de mesclagem seria desproporcional. Como `dimensoesEquivalentes`, `carregarLocaisProduto`/`interseccaoTotalNaoVazia` e `normalizarNomeProduto` já são funções puras e reutilizáveis, aplicá-las só ao conjunto (mantido+removidos) submetido é suficiente para detectar o caso relevante (dimensão corrigida entre a listagem e a confirmação) sem custo adicional relevante.

**`produto_estoque` do removido é deletado, não zerado:** manter uma linha com `quantidade=0` para um Produto soft-deletado não serve a nenhuma leitura (ele já não aparece em nenhuma listagem), mas continuaria alcançável por qualquer `JOIN produto_estoque` que não passe por `produtos` — deletar a linha elimina essa classe de resíduo fantasma na origem, em vez de depender de todo consumidor futuro lembrar de filtrar `deleted_at`.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros.
- `cd backend && go test ./services/... ./handlers/... -run 'Normalizacao|Duplicata|Mesclagem|Mesclar'` -- expected: PASS, cobrindo a I/O Matrix.
- `cd backend && go test ./services/... ./handlers/... -run 'Catalogo|Produto|Importacao|Estoque'` -- expected: PASS (garante que os filtros `deleted_at IS NULL` novos não quebraram nenhum comportamento existente).
- `cd frontend && npx tsc --noEmit` -- expected: sem erros de tipo.
- `cd frontend && npx vitest run src/components/normalizacao/DuplicatasSection.test.tsx` -- expected: PASS.
