---
title: 'Inativar e reativar um Produto'
type: 'feature'
created: '2026-09-25'
status: 'done'
baseline_revision: '2c90923e0ac9e0591f8b6fd8d75728560bc5a1a2'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Não existe como tirar de uso um Produto sem apagá-lo (o único "sumir" é a mesclagem, irreversível); `gestor`/`adm` precisam aposentar Produtos sem saldo e poder reativá-los.

**Approach:** Migration aditiva (`produtos.inativado_em/inativado_por` + tabela `produto_historico`), service `produtos_inativacao.go` com inativar (só sem saldo/reserva, `FOR UPDATE`) e reativar (com checagem de EAN-13 entre ativos), rotas `POST .../inativacao` e `.../reativacao` para `gestor`+, trava `FOR SHARE` + recusa de inativo no lançamento de saldo e no envio de Pedido, carrinho remove o inativo com aviso, e o detalhe do Produto ganha marca "Inativo" e os botões.

## Boundaries & Constraints

**Always:**
- `empresa_id` só do contexto; Produto inexistente, malformado, mesclado (`deleted_at`) ou de outra Empresa → 404 NOT_FOUND sem revelar existência.
- "Ativo" = `inativado_em IS NULL AND deleted_at IS NULL`. Nunca reutilizar `deleted_at`.
- Inativar: numa transação, `SELECT ... FROM produtos WHERE id AND empresa_id AND deleted_at IS NULL FOR UPDATE`; já inativo → 409 `PRODUTO_JA_INATIVO`; saldo físico > 0 em qualquer Estoque (view `saldo_produto_estoque` = `produto_estoque` + `lotes`) ou qualquer linha de `reservas_pedido_item` do Produto → 409 `PRODUTO_COM_SALDO` com mensagem listando os Estoques com saldo e/ou dizendo que há reserva de Pedido pendente, e orientando a transferir ou dar baixa; nada gravado. Senão grava `inativado_em = now()`, `inativado_por = ator` e uma linha `produto_historico` `inativado` com `detalhe = {"motivo": <texto ou null>}`.
- Motivo opcional: trim; vazio → `null`; > 500 runas → 400 VALIDATION_ERROR antes de abrir transação.
- Reativar: mesma trava `FOR UPDATE`; ativo → 409 `PRODUTO_JA_ATIVO`; se tem EAN-13, `pg_advisory_xact_lock(hashtext(empresa_id::text || ':' || ean))` e busca outro Produto ativo da mesma Empresa com o mesmo EAN → 409 `EAN_EM_USO` "Este EAN já está no produto {código} — {nome}", nada gravado; senão limpa `inativado_em/por` e grava `reativado` (`detalhe = {}`). O helper de EAN fica isolado e reaproveitável (a 16.4 o usará em cadastro/edição).
- Rotas `POST /e/{slug}/api/produtos/{id}/inativacao` (corpo `{"motivo"?}`) e `POST /e/{slug}/api/produtos/{id}/reativacao` com `RequireRole(gestor)`; sucesso 200 `{"produto": <detalhe>}` e publica `produtos`/`updated`.
- `LancarSaldo` e `SubmeterPedidoComCentroCusto` tomam `FOR SHARE` na linha de `produtos` na mesma transação e recusam Produto inativo com 409 `PRODUTO_INATIVO` (o envio trava os Produtos distintos em ordem de id ANTES de `travarSaldoParesTx`).
- `ListarCarrinho` trata item de Produto inativo como removido: apaga a linha e devolve em `removidos` com motivo `produto_inativo`; o frontend mostra "\"{nome}\" foi removido do carrinho: o produto foi inativado.".
- `GET /api/produtos/{id}` continua devolvendo o Produto inativo, agora com `inativo: boolean` e `inativadoEm: string|null`.
- Frontend: marca "Inativo" no cabeçalho do detalhe; `gestor`/`adm` veem "Inativar produto" (diálogo com motivo opcional + confirmar; erro 409 mostrado no diálogo) quando ativo e "Reativar" quando inativo; `almoxarife`/`usuario` não veem nenhum dos dois.

**Block If:** —

**Never:** Exclusão definitiva ou inativação em lote. Filtrar o inativo de Catálogo/busca/leitura por código/exportação/Normalização/importação, recusar adicionar ao Carrinho, filtro "Inativos" (Story 16.2). Rota de leitura do histórico e `nome_alterado` (Story 16.3). EAN único em cadastro/edição (Story 16.4). Índice único de EAN. Rota de escrita direta em `produto_historico`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Inativar ok | gestor, ativo, sem saldo/reserva, motivo "fora de linha" | 200, `inativo:true`, histórico `inativado` com motivo | — |
| Inativar sem motivo | corpo `{}` ou vazio | 200, `detalhe.motivo = null` | — |
| Com saldo | lote de 5 no "Almox A" | 409 `PRODUTO_COM_SALDO`, mensagem cita "Almox A" | nada gravado |
| Com reserva | reserva pendente | 409 `PRODUTO_COM_SALDO`, mensagem cita reserva de Pedido | nada gravado |
| Já inativo | inativo | 409 `PRODUTO_JA_INATIVO` | — |
| Reativar ok | inativo, EAN livre ou sem EAN | 200, `inativo:false`, histórico `reativado` | — |
| Reativar EAN em uso | outro ativo com o mesmo EAN | 409 `EAN_EM_USO` citando código e nome do outro | continua inativo |
| Reativar ativo | ativo | 409 `PRODUTO_JA_ATIVO` | — |
| Papel insuficiente | almoxarife/usuario | 403 | — |
| Outra Empresa / inexistente / malformado | — | 404 | — |
| Lançar saldo em inativo | POST lotes | 409 `PRODUTO_INATIVO` | nenhum Lote |
| Corrida | inativar + lançar saldo juntos | um espera o outro; nunca inativo com saldo | — |

</intent-contract>

## Code Map

- `backend/migrations/000051_add_inativacao_produtos.{up,down}.sql` -- nova; molde de `000048_create_auditoria_seguranca` (CHECK de `acao`, índice `(empresa_id, produto_id, criado_em DESC)`, FKs `empresa_id`, `produto_id`, `ator_id`).
- `backend/services/produtos_inativacao.go` -- novo: `InativarProduto`, `ReativarProduto`, `ErroProdutoComSaldo`, `ErrProdutoJaInativo`, `ErrProdutoJaAtivo`, `ErroEANEmUso{Codigo,Nome}`, `garantirEANLivreTx`, `ErrProdutoInativo`.
- `backend/services/lotes.go:~96` -- `LancarSaldo`: INSERT…SELECT já faz `FOR SHARE OF p, e`; somar `AND p.inativado_em IS NULL` e, com zero linhas, distinguir inativo (→ `ErrProdutoInativo`) de 404.
- `backend/services/pedidos.go:~240` -- `SubmeterPedidoComCentroCusto`: travar produtos (`FOR SHARE`, ids distintos ordenados) antes de `travarSaldoParesTx`; algum inativo → `&ErroPedidoProdutoInativo{Itens}`.
- `backend/services/carrinho.go:278` -- `ListarCarrinho`: selecionar `p.inativado_em`; novo `MotivoCarrinhoProdutoInativo = "produto_inativo"`.
- `backend/services/catalogo.go:759,795,826` -- `ProdutoDetalhe` + `produtoDetalheQuery` + `ObterProdutoDetalhe`: `inativo`, `inativadoEm`.
- `backend/handlers/produtos.go` -- novos `InativarProdutoHandler`, `ReativarProdutoHandler` (molde `AtualizarProdutoHandler`; `middleware.UsuarioDaSessao` dá o ator; envelope `escreverErro(code,message)`).
- `backend/handlers/lotes.go`, `backend/handlers/pedidos.go` -- mapear `ErrProdutoInativo`/`ErroPedidoProdutoInativo` → 409 `PRODUTO_INATIVO`.
- `backend/main.go:~592` -- registrar as duas rotas com `RequireRole(services.PapelGestor)`.
- `backend/**/*_test.go` com `TRUNCATE TABLE ... mesclagem_produtos_removidos, ...` -- incluir `produto_historico` na lista (FK para `produtos` quebraria o TRUNCATE sem CASCADE).
- `frontend/src/pages/ProdutoDetalhePage.tsx:205,270,708` -- interface `ProdutoDetalhe`, gate de papel (`rankPapel`), cabeçalho do Card; usar `AlertDialog`/`Dialog` de `components/ui`.
- `frontend/src/lib/carrinho.tsx:44,114` -- tipo do motivo + mensagem.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000051_add_inativacao_produtos.up.sql`/`.down.sql` -- colunas + tabela (down: drop tabela e colunas).
- `backend/services/produtos_inativacao.go` -- inativar/reativar/helper EAN conforme Always.
- `backend/services/lotes.go`, `pedidos.go`, `carrinho.go`, `catalogo.go` -- trava/recusa, carrinho, detalhe.
- `backend/handlers/produtos.go`, `lotes.go`, `pedidos.go`, `main.go` -- handlers, mapeamento de erros e rotas.
- testes `backend/services/produtos_inativacao_test.go` + `backend/handlers/produtos_test.go` -- cobrir a matriz inteira, incluindo corrida inativar×lançar saldo (goroutines) e carrinho/pedido com inativo; atualizar as listas de TRUNCATE.
- `frontend/src/pages/ProdutoDetalhePage.tsx` + `.test.tsx`, `frontend/src/lib/carrinho.tsx` (+ teste) -- UI e mensagens.

**Acceptance Criteria:**
- Given a migration 000051, when roda num banco com Produtos, then todos seguem ativos e `produto_historico` existe; o down a desfaz.
- Given um gestor no detalhe de um Produto ativo sem saldo, when inativa e confirma, then vê a marca "Inativo" e o botão "Reativar".
- Given um Produto com saldo, when o gestor tenta inativar pela tela, then o diálogo mostra a mensagem com os Estoques e nada muda.
- Given um almoxarife ou usuario no detalhe, when a tela carrega, then nenhum dos botões aparece.
- Given um Produto inativo no Carrinho de alguém, when o Carrinho carrega, then o item sai e aparece o aviso de produto inativado.

## Design Notes

- Carrinho: a AD-37 diz "remove os itens de `carrinho_itens`" na inativação, mas apagar ali perderia o aviso exigido pelo AC; a remoção é preguiçosa em `ListarCarrinho` (mesmo mecanismo do Produto mesclado), e `SubmeterPedido` já chama `ListarCarrinho` antes da transação.
- `produtos.inativado_por` fica SEM FK para `usuarios`: com FK, o `TRUNCATE usuarios CASCADE` das suítes passaria a truncar `produtos` e tudo que o referencia. O autor com integridade referencial fica em `produto_historico.ator_id`.
- Ordem de locks sem deadlock: inativar só trava `produtos` (lê saldo/reserva sem lock); lançar/enviar travam `produtos` FOR SHARE antes de `produto_estoque`/`lotes`.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- sem erros
- `cd backend && go test -p 1 -timeout 25m ./...` -- tudo verde (com `DATABASE_URL`)
- `cd frontend && npx tsc --noEmit && npx vitest run src/pages/ProdutoDetalhePage.test.tsx src/lib` -- verde

## Auto Run Result

Status: done (recuperado manualmente)

**O que aconteceu:** a sessão de dev do `bmad-loop` (run `20260925-103639-c86a`) atingiu o tempo máximo de sessão (90 min) já na verificação final, com a spec em `in-review`. O orquestrador pausou com a árvore sem commit. **Não houve review pass em sessão separada**; a verificação foi feita à mão na recuperação.

**Verificação na recuperação:**
- `go build`, `go vet` e `gofmt`: limpos.
- Migration 000051 aplicada a partir de um schema **vazio** (chega à versão 51 limpa). Ela só acrescenta colunas nulas e uma tabela nova, sem depender de dado existente.
- Suíte Go completa (`-p 1`): verde.
- `tsc -b` e `vitest run` completo: 864 testes, verdes.
- Leitura das invariantes da AD-37:
  - `InativarProduto` trava a linha com `FOR UPDATE`, recusa com `ErroProdutoComSaldo` (lista os Estoques) quando há saldo ou reserva, e grava o histórico `inativado` na mesma transação;
  - `ReativarProduto` passa por `garantirEANLivreTx` (advisory lock por Empresa+EAN);
  - o envio de Pedido trava os Produtos com `FOR SHARE`, em ordem de id, antes de saldo e lotes, e recusa se algum estiver inativo;
  - `LancarSaldo` exige `inativado_em IS NULL` na leitura sob `FOR SHARE`;
  - o Carrinho remove o item de Produto inativo com o motivo `produto_inativo`.

**Observação:** `produtos.inativado_por` não tem FK para `usuarios`, diferente do texto da AD-37. É inócuo, porque a conta nunca é apagada (LGPD anonimiza). Fica registrado.
