---
title: 'Envio de Pedido'
type: 'feature'
created: '2026-09-03'
status: 'in-review'
baseline_revision: '722ce9e90053c56f17dae1f25f999c6a58b9af6b'
review_loop_iteration: 1
followup_review_recommended: false
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-7-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** O Carrinho (Story 7.1) só acumula itens — não existe hoje nenhuma forma de formalizar esses itens como um Pedido de Retirada que o Almoxarife possa enfileirar e decidir.

**Approach:** Nova tabela `pedidos` (+ `pedido_itens`, snapshot imutável de nome/categoria/estoque/quantidade no momento do envio) e `services.SubmeterPedido`: reaproveita `ListarCarrinho` (limpeza preguiçosa + itens ativos), revalida a disponibilidade de cada item travando `produto_estoque` em ordem canônica ascendente (sem debitar — o débito real é da Story 7.5, na aprovação), grava o Pedido com `status='pendente'` e esvazia o carrinho, tudo numa única transação de escrita.

## Boundaries & Constraints

**Always:**
- `pedidos.usuario_id` vem sempre de `middleware.UsuarioDaSessao`, nunca do corpo da requisição — é a identidade usada em auditoria/"Meus Pedidos" (Story 7.3), sempre, independente do texto livre em `solicitante`.
- Envio revalida `quantidade <= produto_estoque.quantidade` (SELECT...FOR UPDATE, pares ordenados ascendente por `(produto_id, estoque_id)`) no momento do envio, nunca confia no snapshot do carrinho; falha de QUALQUER item aborta a transação inteira (nenhum Pedido parcial).
- Envio bem-sucedido esvazia `carrinho_itens` do usuário na MESMA transação do INSERT em `pedidos`/`pedido_itens`.
- `pedido_itens` grava nome do Produto, nome do Estoque, categoria e quantidade como SNAPSHOT no momento do envio (mesma decisão arquitetural do recibo, epic-7-context.md) — nunca um join ao vivo com `produtos`/`estoques` para exibição futura.
- `ExcluirEstoque` (estoques.go) passa a bloquear com um segundo guard: Estoque referenciado por `pedido_itens` de um Pedido `status='pendente'` não pode ser excluído (completa o guard que a Story 2.2 deixou pendente).

**Block If:** _Nenhuma decisão bloqueante identificada — a Story 7.1, o epic-7-context.md e o comentário-TODO já existente em estoques.go resolveram os pontos em aberto (ver Design Notes)._

**Never:**
- Não debita `produto_estoque` nem grava Movimentação no envio — a revalidação aqui é só leitura+trava; o débito real e a Movimentação correspondente são da Story 7.5 (aprovação).
- Não implementa listagem/consulta de Pedidos (Stories 7.3/7.4), aprovação/rejeição (7.5), recibo em PDF (7.6) nem migração de Pedidos legados (7.7).
- Não adiciona campo "unidade de medida" a `pedido_itens`: esse atributo não existe hoje em `produtos` (só as 5 unidades dimensionais mm/cm/m); se a Story 7.6 precisar dele, é decisão dela, não desta.
- Não publica no canal SSE `pedidos` nada além de `{resource:"pedidos", id, change:"created"}` — sem payload de itens.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Envio feliz | Carrinho com 2 itens, ambos com saldo suficiente, solicitante+obra preenchidos | `201`, Pedido `pendente` criado, `pedido_itens` com snapshot, carrinho esvaziado, evento SSE `pedidos` | No error expected |
| Carrinho vazio | Usuário sem nenhuma linha em `carrinho_itens` (ou só linhas obsoletas, limpas no próprio envio) | Requisição rejeitada, nada é criado | `409 CONFLICT` |
| Disponibilidade insuficiente num item | 2 itens no carrinho, um deles com saldo real menor que o pedido (mudou desde a montagem) | Pedido inteiro rejeitado, carrinho inalterado, nenhum item debitado | `409 CONFLICT` |
| Solicitante/obra ausente | Corpo sem `solicitante` ou sem `obraCentroCusto` (vazio/whitespace) | Requisição rejeitada antes de tocar o banco | `400 VALIDATION_ERROR` |
| Solicitante ≠ usuário autenticado | `solicitante: "Fulano de Tal"`, sessão de outro usuário | Pedido criado normalmente; `pedidos.usuario_id` = id da sessão, `solicitante` gravado como texto livre | No error expected |
| Exclusão de Estoque com Pedido pendente | Estoque referenciado por `pedido_itens` de um Pedido `pendente` | Exclusão recusada, Estoque preservado | `409 CONFLICT` |

</intent-contract>

## Code Map

- `backend/services/carrinho.go:273-352` (`ListarCarrinho`) -- reaproveitar tal qual para obter itens ativos já limpos (lazy cleanup) antes de abrir a transação de envio; carrinho vazio após a chamada -> rejeitar.
- `backend/services/movimentacoes.go:172-230` (`RegistrarBaixa`) e `:306-376` (`RegistrarTransferencia`, trava em ordem canônica ascendente `estoqueOrigemID/estoqueDestinoID` nas linhas 325-336) -- molde de transação + ordenação de locks para revalidar N pares `(produto_id, estoque_id)` sem deadlock.
- `backend/services/normalizacao.go:1236-1258` (`MesclarDuplicatas`: soma quantidade em `produto_estoque` via UPDATE+DELETE nas linhas 1236-1249, rewrite de `movimentacoes.produto_id` nas linhas 1251-1258) -- para `pedido_itens`, mesmo padrão de soma-e-descarta já usado para `produto_estoque` nas linhas acima: quando a linha do produto removido e uma linha do produto mantido colidem no MESMO `(pedido_id, estoque_id)` (chave primária composta de `pedido_itens`), somar a quantidade na linha mantida e apagar a do removido; só fazer UPDATE simples de `produto_id` quando não há colisão. NUNCA reescrever `produto_nome`/`categoria_nome`/`estoque_nome` do snapshot em nenhum dos dois casos (histórico do que foi realmente pedido é imutável, só o vínculo de auditoria e a quantidade consolidam).
- `backend/services/estoques.go:127-210` (`ExcluirEstoque`) -- inserir o segundo guard (`SELECT DISTINCT` sobre `pedido_itens JOIN pedidos WHERE estoque_id=$1 AND pedidos.status='pendente'`) entre o guard de resíduo (linhas 152-182) e o `DELETE` (linha 184), mesma transação já travada; comentário-TODO existente nas linhas 105-106 marca exatamente este ponto.
- `backend/services/produtos.go:109-123` (`ErroEstoqueComResiduo`) -- molde de struct+`Error()` para o novo `ErroEstoqueComPedidoPendente{Produtos []string}`.
- `backend/services/catalogo.go:229,660` (join `produtos p JOIN categorias c ON c.id = p.categoria_id`) -- molde para resolver `categoria_nome` do snapshot.
- `backend/handlers/carrinho.go` (padrão completo: decode -> service -> `errors.As` -> `escreverJSON`/`escreverErro`) e `backend/handlers/movimentacoes.go:33-76` (`RegistrarBaixaHandler`, publica no Registry) -- molde exato para `SubmeterPedidoHandler`.
- `backend/main.go:601-606` (registro das rotas de carrinho, só `RequireAuth`) -- mesmo padrão para `POST /api/pedidos`; `realtime.NewRegistry()` (linha 308) e `canaisValidos["pedidos"]` já habilitado em `backend/realtime/registry.go:18-22` -- só falta publicar.
- `backend/middleware/auth.go:114` (`UsuarioDaSessao`) -- fonte do `usuario.ID`.
- `backend/migrations/000025_create_carrinho_itens.{up,down}.sql` -- última migration; a nova é `000026_create_pedidos.{up,down}.sql` (duas tabelas numa migration, molde de `000024_create_mesclagem_duplicatas`).
- `backend/services/estoques_test.go` (`limparEstoques`, `TestExcluirEstoque_ComResiduo`) -- adicionar `pedidos`/`pedido_itens` ao TRUNCATE e um teste espelhado para o novo guard.
- `frontend/src/lib/carrinho.tsx:188-227` (`adicionarItem`/`removerItem`) -- molde exato para a nova função `enviarPedido(solicitante, obraCentroCusto, observacao)` no mesmo Context (POST + `refresh()` em caso de sucesso, que já reflete o carrinho vazio).
- `frontend/src/pages/CarrinhoPage.tsx:43-132` -- adicionar botão "Enviar Pedido" (só quando `itens.length > 0`) abrindo um `Dialog` de formulário (molde de diálogo em `frontend/src/pages/ProdutoDetalhePage.tsx`), nunca `ConfirmDialog` puro (não tem slot de input).
- `frontend/src/lib/auth.tsx:31-33` (`UsuarioSessao.nome`) -- valor inicial sugerido (editável) do campo "solicitante".

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000026_create_pedidos.up.sql` -- criar `pedidos` (`id UUID PK`, `usuario_id UUID NOT NULL REFERENCES usuarios(id)`, `solicitante TEXT NOT NULL`, `obra_centro_custo TEXT NOT NULL`, `observacao TEXT`, `status TEXT NOT NULL DEFAULT 'pendente' CHECK (status IN ('pendente','aprovado','rejeitado'))`, `criado_em TIMESTAMPTZ NOT NULL DEFAULT now()`) e `pedido_itens` (`pedido_id UUID NOT NULL REFERENCES pedidos(id) ON DELETE CASCADE`, `produto_id UUID NOT NULL REFERENCES produtos(id)`, `produto_nome TEXT NOT NULL`, `categoria_nome TEXT NOT NULL`, `estoque_id UUID NOT NULL` sem FK (Estoques são hard-deleted, mesmo motivo de `carrinho_itens`), `estoque_nome TEXT NOT NULL`, `quantidade NUMERIC(10,3) NOT NULL CHECK (quantidade > 0)`, `PRIMARY KEY (pedido_id, produto_id, estoque_id)`) + índice em `estoque_id` -- fundação de dados.
- `backend/migrations/000026_create_pedidos.down.sql` -- `DROP TABLE pedido_itens; DROP TABLE pedidos;` -- reversibilidade padrão.
- `backend/services/pedidos.go` (novo) -- `SubmeterPedido(db, usuarioID, solicitante, obraCentroCusto, observacao string) (Pedido, error)`: valida campos obrigatórios, chama `ListarCarrinho` (rejeita se vazio -> `ErrPedidoCarrinhoVazio`), abre transação, trava `produto_estoque` de cada par em ordem ascendente, revalida saldo (falha em qualquer item -> `&ErroPedidoIndisponivel{Itens: [...]}`, rollback), insere `pedidos`+`pedido_itens` (com `categoria_nome` via join `categorias`), esvazia `carrinho_itens` do usuário, commit -- regra de negócio central da story.
- `backend/services/normalizacao.go` -- em `MesclarDuplicatas`, logo após o rewrite de `movimentacoes.produto_id` (linhas 1251-1258), tratar `pedido_itens.produto_id` com segurança de chave composta `(pedido_id, produto_id, estoque_id)`: para cada linha de um produto removido, se já existir uma linha do produto mantido no MESMO `(pedido_id, estoque_id)`, somar a quantidade da removida na mantida e apagar a linha do removido; caso contrário, `UPDATE` simples do `produto_id` -- mantém `pedido_itens.produto_id` correto após mesclagem SEM violar a chave primária (um mesmo Pedido pode ter itens do produto sobrevivente e do removido no mesmo Estoque), sem tocar no snapshot de nome/categoria/estoque.
- `backend/services/normalizacao_test.go` -- teste adicional cobrindo a colisão de chave composta: `pedido_itens` já tem uma linha do produto MANTIDO e uma linha do produto REMOVIDO no mesmo `(pedido_id, estoque_id)` antes da mesclagem; depois de `MesclarDuplicatas`, as duas viram uma só linha com a quantidade somada, mesclagem bem-sucedida (sem erro de chave duplicada) -- prova a consolidação exigida acima.
- `backend/services/estoques.go` -- em `ExcluirEstoque`, entre o guard de resíduo e o `DELETE`, acrescentar o `SELECT DISTINCT` sobre `pedido_itens JOIN pedidos` com `status='pendente'`; erro novo `ErroEstoqueComPedidoPendente` em `produtos.go` (mesmo molde de `ErroEstoqueComResiduo`) -- completa o guard pendente desde a Story 2.2.
- `backend/handlers/pedidos.go` (novo) -- `SubmeterPedidoHandler`: decode, `services.SubmeterPedido`, `errors.As`/`errors.Is` sobre os erros novos, `registro.Publish("pedidos", ...)` no sucesso -- fronteira HTTP.
- `backend/handlers/estoques.go` -- `case errors.As(err, &pedidoPendente)` no `ExcluirEstoqueHandler`, mesmo `409 CONFLICT` -- mapeia o novo guard.
- `backend/main.go` -- registrar `POST /api/pedidos` atrás só de `RequireAuth` (qualquer `usuario`+, mesmo mínimo do carrinho) -- expõe a API.
- `backend/services/pedidos_test.go`, `backend/handlers/pedidos_test.go` -- cobrir a I/O Matrix (feliz, carrinho vazio, indisponibilidade, campos ausentes, solicitante≠usuário) -- molde de `carrinho_test.go`/`movimentacoes_test.go`.
- `backend/services/estoques_test.go` -- `TestExcluirEstoque_ComPedidoPendente` (molde de `TestExcluirEstoque_ComResiduo`) + atualizar `limparEstoques` com as duas tabelas novas.
- `backend/services/normalizacao_test.go` -- teste cobrindo o rewrite de `pedido_itens.produto_id` numa mesclagem.
- `frontend/src/lib/carrinho.tsx` -- `enviarPedido(solicitante, obraCentroCusto, observacao)`: `POST /api/pedidos` + `refresh()` em caso de sucesso -- expõe a ação ao contexto global.
- `frontend/src/pages/CarrinhoPage.tsx` -- botão "Enviar Pedido" + `Dialog` de formulário (solicitante pré-preenchido com `useAuth().usuario.nome`, obra/centro de custo, observação opcional), sucesso -> `toast.success` -- superfície principal da story.
- `frontend/src/pages/CarrinhoPage.test.tsx` -- cobrir envio feliz, carrinho vazio (botão ausente/desabilitado), erro do servidor (toast) -- convenção Vitest + RTL do projeto.

**Acceptance Criteria:**
- Given um Carrinho com ao menos um item com saldo disponível, when o Usuário informa solicitante e obra/centro de custo e envia, then um Pedido `pendente` é criado com os itens revalidados, o carrinho fica vazio e um toast confirma.
- Given um Carrinho vazio (ou só com itens obsoletos, limpos no próprio envio), when o Usuário tenta enviar, then a requisição é rejeitada e nenhum Pedido é criado.
- Given um item cuja disponibilidade real caiu abaixo do pedido desde a montagem do carrinho, when o envio é revalidado, then a requisição inteira é rejeitada, nenhum item é debitado e o carrinho permanece como estava.
- Given "solicitante" preenchido como texto livre diferente do nome do Usuário autenticado, when o Pedido é registrado, then `pedidos.usuario_id` é sempre o id da sessão, nunca inferido do texto de `solicitante`.
- Given um Estoque referenciado por `pedido_itens` de um Pedido `pendente`, when a exclusão desse Estoque é tentada, then ela é recusada.
- Given um Pedido com itens do produto sobrevivente e do produto removido no MESMO Estoque, when uma mesclagem une esses Produtos, then as duas linhas de `pedido_itens` são consolidadas (quantidade somada) sem violar a chave primária, e a mesclagem é bem-sucedida.

## Spec Change Log

### 2026-09-03 — bad_spec: colisão de chave composta em pedido_itens durante MesclarDuplicatas
- **Achado que disparou a mudança:** dois revisores independentes (edge-case-hunter e verification-gap) apontaram que o rewrite de `pedido_itens.produto_id` especificado literalmente no Code Map/Tasks originais (`UPDATE pedido_itens SET produto_id = $1 WHERE produto_id = ANY($2)`) viola a `PRIMARY KEY (pedido_id, produto_id, estoque_id)` quando o mesmo Pedido já tem uma linha do produto mantido no mesmo `(pedido_id, estoque_id)` de uma linha do produto removido — a mesclagem inteira falha (rollback, nenhum produto mesclado) exatamente no cenário que a deduplicação existe para resolver (pedir "duas versões" do mesmo item no mesmo Estoque).
- **O que foi emendado:** Code Map e Tasks & Acceptance (fora do `<intent-contract>`) agora exigem soma-e-descarta na colisão (mesmo padrão já usado no bloco de `produto_estoque` da própria função, linhas 1236-1249) em vez do `UPDATE` ingênuo; acrescentada uma Acceptance Criterion e uma linha de Design Notes explicando a correção; acrescentado um item de teste dedicado.
- **Estado ruim evitado:** mesclagem de duplicatas falhando (erro de chave duplicada, transação abortada) sempre que um Pedido pendente ou histórico referenciar tanto o produto sobrevivente quanto o removido no mesmo Estoque.
- **KEEP — o que já estava certo e deve sobreviver à re-derivação:** todo o resto da implementação original passou nos 4 outros lentes de review (blind-hunter, edge-case-hunter, verification-gap, intent-alignment) sem nenhum outro achado `bad_spec`/`intent_gap`: migration `000026_create_pedidos` (schema de `pedidos`/`pedido_itens` inalterado), `services.SubmeterPedido` (estrutura de transação, reaproveitamento de `ListarCarrinho`, ordenação canônica de locks, revalidação, snapshot via join de categorias, esvaziamento do carrinho), o segundo guard em `ExcluirEstoque` (`ErroEstoqueComPedidoPendente`), `handlers/pedidos.go`, o registro da rota em `main.go`, e as mudanças de frontend (`carrinho.tsx` `enviarPedido`, `CarrinhoPage.tsx` com o diálogo "Enviar Pedido") — build/vet/test (backend) e tsc/vitest (frontend) 100% verdes, todas as linhas da I/O Matrix cobertas por teste. Nenhuma dessas partes precisa mudar; só o bloco de rewrite de `pedido_itens.produto_id` em `MesclarDuplicatas` (e seu teste) precisa ser re-derivado com a lógica de soma-e-descarta.

## Review Triage Log

### 2026-09-03 — Review pass
- intent_gap: 0
- bad_spec: 1 (high 1, medium 0, low 0)
- patch: 9 (high 0, medium 5, low 4)
- defer: 1 (high 0, medium 1, low 0)
- reject: 2
- addressed_findings:
  - `[high]` `[bad_spec]` MesclarDuplicatas rewrite of `pedido_itens.produto_id` (spec Tasks) used a plain `UPDATE` that violates the composite PK `(pedido_id, produto_id, estoque_id)` when a Pedido already holds an item for the merge survivor at the same estoque as one for the removed product — spec amended to require sum-and-discard (mirroring the existing `produto_estoque` consolidation in the same function) instead of a naive `UPDATE`; implementation reverted for re-derivation.

## Design Notes

- **Sem débito no envio (Never):** o Technical Decision do epic-7-context.md ("toda escrita em `produto_estoque.quantidade` ... ao aprovar um Pedido") deixa claro que o débito real acontece na aprovação (Story 7.5) — o envio só revalida e trava-e-libera dentro da mesma transação de leitura, para impedir dois envios concorrentes de contarem o mesmo saldo de forma inconsistente, mas sem reservar fisicamente a quantidade. Isso é coerente com a própria razão de existir da revalidação-na-aprovação (7.5): se o envio já debitasse, revalidar de novo na decisão seria redundante.
- **`pedido_itens.produto_id` rewrite na mesclagem, mas não o snapshot:** a Story 6.4 (`MesclarDuplicatas`) já reescreve `movimentacoes.produto_id` para preservar a navegação por Produto após uma fusão; como o epic-7-context.md documenta a mesma expectativa para `PEDIDO_ITENS` e a tabela só passa a existir nesta story, o rewrite é acrescentado aqui, no mesmo padrão. O nome/categoria/estoque do snapshot em si NUNCA são reescritos — representam o que foi pedido no momento do envio, mesma imutabilidade do futuro recibo (Story 7.6). **Colisão de chave composta (achado de review):** como `pedido_itens` tem `PRIMARY KEY (pedido_id, produto_id, estoque_id)`, um `UPDATE` ingênuo de `produto_id` quebra quando o MESMO Pedido já tem uma linha do produto mantido no mesmo `(pedido_id, estoque_id)` — cenário plausível (pedir os dois "duplicados" no mesmo Estoque é exatamente o tipo de entrada que a deduplicação existe para resolver). A correção é somar-e-descartar, mesmo espírito do bloco de `produto_estoque` logo acima na própria função (linhas 1236-1249): a linha mantida absorve a quantidade da removida, a linha do removido é apagada; só cai no `UPDATE` simples quando não há colisão.
- **Sem campo "unidade":** o epic-7-context.md cita "nome, unidade, estoque, quantidade, categoria" como os campos que o recibo (Story 7.6) precisa, mas `produtos` não tem hoje nenhum atributo de unidade de medida (só as 5 unidades dimensionais mm/cm/m, que não se aplicam aqui). Como nenhuma AC desta story depende desse campo, ele fica de fora — decisão da Story 7.6 se e como introduzi-lo.
- **`ListarCarrinho` reaproveitado, não duplicado:** chamar `ListarCarrinho` antes de abrir a transação de envio já resolve a limpeza preguiçosa (Produto mesclado/Estoque excluído) exatamente como o `GET /carrinho` faz — evita duas implementações divergentes do mesmo "o que está realmente ativo no carrinho agora".

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test ./...` -- expected: build limpo, todos os testes (incluindo os novos de `pedidos`/`estoques`/`normalizacao`) passando.
- `cd frontend && npx tsc --noEmit && npx vitest run` -- expected: sem erro de tipo, suíte Vitest 100% passando (incluindo os novos testes de `CarrinhoPage`).
