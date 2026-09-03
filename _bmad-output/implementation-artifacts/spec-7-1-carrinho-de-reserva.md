---
title: 'Carrinho de reserva'
type: 'feature'
created: '2026-09-02'
status: 'done'
baseline_revision: '477a4b8c48284b28bd90974c8384b45a52dd38a4'
review_loop_iteration: 0
followup_review_recommended: true
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-7-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Não existe hoje nenhuma forma de acumular itens (Produto + Estoque + quantidade) antes de formalizar um Pedido de Retirada — Épico 7 completo depende de um Carrinho persistente por Usuário.

**Approach:** Nova tabela `carrinho_itens` (por `usuario_id`) + serviço/handlers CRUD (`adicionar`, `listar`, `remover`) com validação de disponibilidade atômica (`SELECT ... FOR UPDATE` em `produto_estoque`, molde de `RegistrarBaixa`); frontend ganha página `/carrinho`, botão "Adicionar ao Carrinho" na tabela "Quantidade por Estoque" do detalhe do Produto, e o `cart-badge` no shell.

## Boundaries & Constraints

**Always:**
- Toda operação de carrinho é escopada ao `usuario_id` da sessão (`middleware.UsuarioDaSessao`) — nunca um id vindo do cliente.
- Adicionar item é atômico e dependente de leitura prévia (NFR7): dentro de uma transação, trava a linha de `produto_estoque` (`SELECT ... FOR UPDATE`, ausência de linha = 0 disponível, sem criar linha nova) e valida `quantidade_já_no_carrinho_para_o_par + quantidade_pedida <= disponível` antes de gravar (upsert incrementando, `PRIMARY KEY (usuario_id, produto_id, estoque_id)`).
- `GET /carrinho` limpa preguiçosamente: qualquer linha cujo Produto tenha `deleted_at` preenchido (mesclado, Story 6.4) ou cujo `estoque_id` não exista mais em `estoques` (excluído, Story 2.2) é apagada e devolvida em `removidos` com o motivo (`produto_removido` | `estoque_excluido`), para o frontend exibir o aviso.
- `cart-badge` nunca mostra "0" — desaparece por completo com carrinho vazio; toda adição/remoção sempre acompanhada de toast (UX-DR5, UX-DR11).

**Block If:** _Nenhuma decisão bloqueante identificada — investigação e leitura literal das ACs da Story 7.1 resolveram os pontos em aberto (ver Design Notes)._

**Never:**
- Não implementa envio de Pedido, fila, aprovação/rejeição ou recibo (Stories 7.2–7.6) — carrinho isolado nesta story.
- Não modifica `MesclarDuplicatas` (Story 6.4) nem a exclusão de Estoque (Story 2.2): a resolução de item obsoleto é só leitura-e-limpeza no `GET /carrinho`, não um gancho nos dois fluxos de escrita já existentes.
- Não adiciona canal SSE nem publica em `pedidos`: o carrinho é sincronizado só por refetch após a própria ação do usuário nesta aba — sem tempo real entre abas/dispositivos nesta story.
- Não mexe em `ScannerProdutoFab.tsx` nem na tabela agrupada do Catálogo (`CatalogoListagem.tsx`, modo `tabela`): a leitura de QR Code já navega ao detalhe do Produto (Story 4.5), e é lá — na tabela "Quantidade por Estoque" — que mora o botão "Adicionar ao Carrinho".

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Adição feliz | Produto com 10 disponíveis no Estoque X, nada no carrinho | `201`, item no carrinho com quantidade pedida, toast + badge atualizam | No error expected |
| Incremento do mesmo par | 3 já no carrinho para Produto/Estoque X, disponível 10, pede +5 | `201`, linha vira 8 (upsert soma) | No error expected |
| Disponibilidade insuficiente | 8 já no carrinho, disponível 10, pede +5 (total 13) | Requisição rejeitada, carrinho inalterado | `409 CONFLICT` |
| Produto/Estoque sem linha em `produto_estoque` | Par nunca teve saldo lançado | Tratado como 0 disponível, qualquer quantidade > 0 rejeitada | `409 CONFLICT` |
| Produto inexistente/mesclado | `produtoId` não existe ou tem `deleted_at` preenchido | Adição recusada | `404 NOT_FOUND` |
| Abrir carrinho com item obsoleto | Linha no carrinho aponta para Produto mesclado ou Estoque excluído | Linha removida do carrinho, devolvida em `removidos` com motivo | No error expected (resposta 200 normal) |
| Carrinho vazio | Nenhuma linha para o usuário | `itens: []`, frontend mostra mensagem de carrinho vazio | No error expected |
| Remover item inexistente | `DELETE` para par que não está no carrinho do usuário | Nada é alterado | `404 NOT_FOUND` |

</intent-contract>

## Code Map

- `backend/migrations/000024_create_mesclagem_duplicatas.{up,down}.sql` -- última migration; a nova é `000025_create_carrinho_itens.{up,down}.sql`.
- `backend/services/movimentacoes.go:172-233` (`RegistrarBaixa`) -- molde de transação: validar antes de `tx.Begin()`, `SELECT ... FOR UPDATE` na linha de `produto_estoque`, `defer tx.Rollback()`, `tx.Commit()`.
- `backend/services/movimentacoes.go:251-268` (`travarLinhaProdutoEstoque`) -- padrão de lock via `INSERT ... ON CONFLICT DO UPDATE` para linha que pode não existir; referência, mas o carrinho NÃO deve criar linha de `produto_estoque` — tratar ausência como 0 via `sql.ErrNoRows`.
- `backend/services/produtos.go:440` -- padrão `p.deleted_at IS NULL` a reaplicar na checagem de Produto ainda ativo.
- `backend/services/estoques.go:184` -- Estoques são hard-deleted (`DELETE FROM estoques`), sem coluna de soft-delete e sem guarda hoje contra carrinho — por isso `carrinho_itens.estoque_id` não deve ter FK `REFERENCES estoques(id)` (bloquearia ou exigiria `ON DELETE CASCADE`, perdendo a chance de avisar o motivo); existência é checada por leitura no `GET /carrinho`.
- `backend/middleware/auth.go:114` (`UsuarioDaSessao`) -- fonte do `usuario.ID` para escopar toda operação de carrinho.
- `backend/handlers/movimentacoes.go:33-76` (`RegistrarBaixaHandler`) -- molde exato de handler: decode, chamar service, `errors.As` sobre erros sentinela, `escreverJSON`/`escreverErro` (`handlers/auth.go:32,38`).
- `backend/main.go:514-528` -- bloco de registro de rota (`mux.HandleFunc` com `middleware.RequireAuth(db, jwtSecret)(...)`) a replicar; rotas de carrinho SEM `RequireRole` (qualquer `usuario`+).
- `frontend/src/pages/ProdutoDetalhePage.tsx:107-127` (`EstoqueQuantidade`), `:184-200` (estado do diálogo de Baixa), `:295-336` (`confirmarBaixa`) -- molde exato para o novo diálogo "Adicionar ao Carrinho" por linha da tabela "Quantidade por Estoque".
- `frontend/src/lib/auth.tsx` -- molde de Context+Provider (`AuthProvider`/`useAuth`) para o novo `CarrinhoProvider`/`useCarrinho` (`frontend/src/lib/carrinho.tsx`, novo).
- `frontend/src/App.tsx:100-105` (rotas filhas de `RotaProtegida`), `:118-120` (`<AuthProvider><RouterProvider .../></AuthProvider>`) -- adicionar rota `carrinho` e envolver com `<CarrinhoProvider>` por dentro de `AuthProvider`.
- `frontend/src/components/shell/nav-items.ts:58` -- item de nav "Carrinho" já existe (Story 1.2), sem mudança.
- `frontend/src/components/shell/AppShell.tsx:61-99` (`RailNavIcon`/`BottomNavIcon`) -- adicionar badge condicional (`item.id === 'carrinho' && count > 0`) usando `useCarrinho()`.
- `frontend/src/components/ui/sonner.tsx` -- `toast.success`/`toast.info` já existente, reaproveitar.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000025_create_carrinho_itens.up.sql` -- criar tabela `carrinho_itens` (`usuario_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE`, `produto_id UUID NOT NULL REFERENCES produtos(id)`, `estoque_id UUID NOT NULL` sem FK, `quantidade NUMERIC(10,3) NOT NULL CHECK (quantidade > 0)`, `criado_em`/`atualizado_em TIMESTAMPTZ NOT NULL DEFAULT now()`, `PRIMARY KEY (usuario_id, produto_id, estoque_id)`) + índice em `estoque_id` -- fundação de dados da story.
- `backend/migrations/000025_create_carrinho_itens.down.sql` -- `DROP TABLE carrinho_itens` -- reversibilidade padrão do projeto.
- `backend/services/carrinho.go` -- `AdicionarItemCarrinho`, `ListarCarrinho`, `RemoverItemCarrinho` + erros sentinela (`ErroCarrinhoValidacao`, `ErroCarrinhoIndisponivel`, `ErrCarrinhoProdutoNaoEncontrado`, `ErrCarrinhoItemNaoEncontrado`) -- regra de negócio (lock, upsert, limpeza preguiçosa).
- `backend/handlers/carrinho.go` -- `AdicionarItemCarrinhoHandler`, `ListarCarrinhoHandler`, `RemoverItemCarrinhoHandler` -- fronteira HTTP, molde de `handlers/movimentacoes.go`.
- `backend/main.go` -- registrar `POST /api/carrinho/itens`, `GET /api/carrinho`, `DELETE /api/carrinho/itens/{produtoId}/{estoqueId}`, todas atrás só de `RequireAuth` -- expõe a API.
- `backend/services/carrinho_test.go` / `backend/handlers/carrinho_test.go` -- cobrir a I/O Matrix acima (feliz, incremento, indisponível, sem linha em `produto_estoque`, produto mesclado, limpeza na leitura, carrinho vazio, remoção de item inexistente) -- molde de `movimentacoes_test.go`.
- `frontend/src/lib/carrinho.tsx` -- `CarrinhoProvider`/`useCarrinho()` (itens, contagem, `refresh`, `adicionarItem`, `removerItem`) -- estado global mínimo consumido pelo badge e pela página.
- `frontend/src/pages/CarrinhoPage.tsx` -- lista itens (nome, estoque, quantidade, botão remover), estado vazio ("Seu carrinho está vazio. Busque um produto ou aponte a câmera para um código."), toast por item removido automaticamente (`removidos`) -- superfície principal da story.
- `frontend/src/App.tsx` -- rota `{ path: 'carrinho', element: <CarrinhoPage /> }` dentro da árvore de `RotaProtegida`, envolver `<RouterProvider>` com `<CarrinhoProvider>` -- liga a página à navegação.
- `frontend/src/pages/ProdutoDetalhePage.tsx` -- por linha da tabela "Quantidade por Estoque": botão "Adicionar ao Carrinho" abrindo diálogo de quantidade (molde de "Registrar Baixa"), `POST /api/carrinho/itens`, sucesso -> toast + `useCarrinho().refresh()` -- primeiro ponto de entrada da AC1.
- `frontend/src/components/shell/AppShell.tsx` -- badge no ícone do item `carrinho` (rail, bottom nav e Sheet "Mais") a partir de `useCarrinho().count`, nunca "0" -- UX-DR5.
- `frontend/src/pages/CarrinhoPage.test.tsx`, `frontend/src/lib/carrinho.test.tsx` -- cobrir estado vazio, remoção, aviso de item obsoleto -- convenção Vitest + RTL do projeto.

**Acceptance Criteria:**
- Given um Produto com saldo disponível num Estoque, when o Usuário clica "Adicionar ao Carrinho" na tabela "Quantidade por Estoque" do detalhe do Produto e confirma uma quantidade válida, then o item entra no carrinho, um toast confirma e o `cart-badge` do shell atualiza o contador.
- Given um item no carrinho cujo Produto foi mesclado (soft-deletado) ou cujo Estoque foi excluído, when o Usuário abre `/carrinho`, then esse item some da lista automaticamente e um aviso explica o motivo.
- Given o carrinho vazio, when o Usuário acessa `/carrinho`, then vê a mensagem de carrinho vazio, sem o `cart-badge` visível em nenhuma superfície de navegação.
- Given um item no carrinho, when o Usuário o remove e confirma, then um toast confirma a remoção e o `cart-badge` atualiza (some por completo se o carrinho ficar vazio).
- Given uma requisição de adição para um par Produto/Estoque cuja soma (carrinho atual + quantidade pedida) excede o disponível real, then o servidor rejeita com `409` e o carrinho permanece inalterado, mesmo sob duas adições concorrentes do mesmo usuário.

## Spec Change Log

## Review Triage Log

### 2026-09-02 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 10 (medium 5, low 5)
- defer: 0
- reject: 8
- addressed_findings:
  - `[medium]` `[patch]` Nenhum teste em `main_test.go` verifica as 3 novas rotas de carrinho no nível de `newMux` (padrão usado por toda outra família de rota) — adicionado `TestNewMux_CarrinhoRotasSoRequireAuth`.
  - `[low]` `[patch]` `AdicionarItemCarrinho` tratava "estoque inexistente" como "quantidade insuficiente" (mensagem enganosa) — agora distingue e devolve 404 quando o Estoque não existe.
  - `[low]` `[patch]` Faltava teste do ramo `pqInvalidTextRepresentation` para `estoqueId` malformado — adicionado.
  - `[medium]` `[patch]` `App.test.tsx` renderizava `CarrinhoProvider` real sem stub de `fetch` (risco de flake) — mockado `@/lib/carrinho`, mesmo padrão de `AppShell.test.tsx`.
  - `[medium]` `[patch]` `refresh()` engolia falha de `GET /api/carrinho` e ficava indistinguível de carrinho vazio — adicionado estado de erro distinto, `CarrinhoPage` mostra mensagem própria.
  - `[low]` `[patch]` Botão "Adicionar ao Carrinho" habilitado mesmo com `quantidade` zerada — desabilitado nesse caso.
  - `[low]` `[patch]` Fronteira 99/100 do `cart-badge` sem teste — adicionados casos 99 e 100.
  - `[low]` `[patch]` `mensagemItemCarrinhoRemovido` tratava qualquer `motivo` desconhecido como `estoque_excluido` — trocado por `switch` exaustivo com mensagem genérica de fallback.
  - `[medium]` `[patch]` Corrida sem proteção em `refresh()`: troca rápida de conta na mesma aba podia deixar uma resposta tardia do usuário anterior sobrescrever o estado do usuário atual — adicionado contador de geração de requisição.
  - `[medium]` `[patch]` `removerItem` só tinha teste do caminho de sucesso (sem paridade com `adicionarItem`) — adicionado teste de falha; corrigido também o mock de sucesso pré-existente (`{ok:false, status:204}`, combinação que o Fetch real nunca produz).

## Design Notes

**Redirecionamento de mesclagem (Story 6.4) vs. remoção (Story 7.1):** `epic-7-context.md` (Cross-Story Dependencies) descreve um redirecionamento automático de item de Carrinho para o Produto sobrevivente "tratado no momento da mesclagem" — mas a Story 6.4 já foi implementada e revisada ANTES de `carrinho_itens` existir, então esse gancho nunca foi (nem podia ter sido) construído em `MesclarDuplicatas`. A própria Story 7.1 já resolve o caso por texto literal e completo: "esse item é removido automaticamente, com aviso explicando o motivo" (AC citando explicitamente "ex. mesclado — Story 6.4" como gatilho de "deixou de existir"). Esta spec segue a leitura literal e autoritativa da própria AC de 7.1 — remoção preguiçosa na leitura do carrinho, nunca redirecionamento — e não reabre nem modifica `MesclarDuplicatas`. Um retrofit de 6.4 para redirecionar em vez de deixar a limpeza preguiçosa é um refinamento de UX possível no futuro, fora do escopo desta story.

**Por que `estoque_id` sem FK:** `produto_estoque.estoque_id` usa `REFERENCES estoques(id) ON DELETE CASCADE` porque não precisa explicar nada ao excluir — o saldo simplesmente some. `carrinho_itens` precisa mostrar POR QUE o item sumiu; uma FK com cascade apagaria a linha antes que o usuário pudesse ver o aviso, e uma FK sem cascade bloquearia a exclusão do Estoque (contradizendo a Story 2.2, que só bloqueia por saldo residual ou Pedido pendente). Por isso a existência do Estoque é checada por leitura no `GET /carrinho`, nunca por constraint.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros de compilação.
- `cd backend && go test ./services/... ./handlers/... -run Carrinho` -- expected: todos os cenários da I/O Matrix passam.
- `cd frontend && npm run lint && npm run typecheck` -- expected: sem erros.
- `cd frontend && npx vitest run src/pages/CarrinhoPage.test.tsx src/lib/carrinho.test.tsx src/components/shell/AppShell.test.tsx` -- expected: todos os testes passam, incluindo os já existentes de `AppShell.test.tsx` (badge não deve quebrar asserções atuais).

## Auto Run Result

**Resumo:** Implementado o Carrinho de reserva (Story 7.1) de ponta a ponta: tabela `carrinho_itens` por usuário, adição atômica com trava de disponibilidade em `produto_estoque`, listagem com limpeza preguiçosa de itens obsoletos (Produto mesclado/Estoque excluído), remoção, `cart-badge` no shell e ponto de entrada "Adicionar ao Carrinho" na tabela "Quantidade por Estoque" do detalhe do Produto. Duas rodadas: implementação inicial + 10 correções de revisão.

**Arquivos alterados:**
- `backend/migrations/000025_create_carrinho_itens.{up,down}.sql` -- nova tabela `carrinho_itens`.
- `backend/services/carrinho.go` -- `AdicionarItemCarrinho`/`ListarCarrinho`/`RemoverItemCarrinho` + erros sentinela, incl. `ErrCarrinhoEstoqueNaoEncontrado` (correção de revisão).
- `backend/handlers/carrinho.go` -- fronteira HTTP das 3 rotas de carrinho.
- `backend/main.go` -- registro das 3 rotas (`RequireAuth` apenas).
- `backend/main_test.go` -- +`TestNewMux_CarrinhoRotasSoRequireAuth`; `TRUNCATE` de 7 arquivos de teste (`estoques_test.go`, `fotos_test.go`, `importacoes_test.go`, `produtos_test.go` em handlers/services) passam a incluir `carrinho_itens`.
- `backend/services/carrinho_test.go`, `backend/handlers/carrinho_test.go` -- cobertura completa da I/O Matrix + casos de revisão (estoque inexistente/malformado).
- `frontend/src/lib/carrinho.tsx` -- `CarrinhoProvider`/`useCarrinho` (novo), com estado de erro distinto e guarda de geração de requisição (correções de revisão).
- `frontend/src/pages/CarrinhoPage.tsx` -- página do carrinho (novo).
- `frontend/src/App.tsx` -- rota `/carrinho` + `<CarrinhoProvider>`.
- `frontend/src/pages/ProdutoDetalhePage.tsx` -- botão "Adicionar ao Carrinho" por linha de Estoque, desabilitado quando `quantidade <= 0` (correção de revisão).
- `frontend/src/components/shell/AppShell.tsx` -- `cart-badge` no rail/bottom nav.
- `frontend/src/lib/carrinho.test.tsx`, `frontend/src/pages/CarrinhoPage.test.tsx`, `frontend/src/components/shell/AppShell.test.tsx`, `frontend/src/pages/ProdutoDetalhePage.test.tsx`, `frontend/src/App.test.tsx` -- testes novos/ajustados.
- `_bmad-output/implementation-artifacts/epic-7-context.md` -- contexto do Épico 7 compilado nesta execução.

**Revisão (achados):** patch: 10 (medium 5, low 5); defer: 0; reject: 8; intent_gap: 0; bad_spec: 0. Todos os 10 patches foram aplicados nesta mesma passada (ver Review Triage Log). Os 8 rejeitados: mismatch inofensivo entre a Code Map da spec e `SheetNavRow` (item `carrinho` nunca aparece no Sheet "Mais", então é inatingível), falta de dica de "Disponível: N" no diálogo (fora do AC), efeito colateral de limpeza dentro de um `GET` (mandado pela própria spec, risco prático baixo nesta SPA sem cache HTTP), ausência de registro persistente de itens removidos (o toast já satisfaz o AC), sobre-reserva entre usuários diferentes (fora de escopo por texto literal do AC1, tratado só na revalidação real da Story 7.5), ruído de ponto flutuante na mensagem de erro (verificado empiricamente como já não-reprodutível, dado o arredondamento existente), falta de `encodeURIComponent` na URL de remoção (inatingível — ids são sempre UUIDs válidos) e corrida entre a limpeza de `ListarCarrinho` e uma remoção concorrente do mesmo item (já tratada sem erro pelo `RowsAffected == 0`).

**Verificação:** `go build`/`go vet` limpos; `go test -p 1 -count=1 ./...` -- todos os 8 pacotes `ok` (Postgres real, local); `npm run lint`/`npx tsc -b` limpos; `npm test` -- 38 arquivos, 442 testes, todos passando. Confirmado de forma independente pelo orquestrador (não só relatado pelos subagentes), incluindo inspeção direta do código das duas correções mais delicadas (checagem de existência do Estoque, guarda de geração de requisição).

**Riscos residuais:** Nenhum bloqueante. Um commit não relacionado a esta story (`51163d5`, correção de `migrate-legado`) aconteceu em `main` durante esta execução — o diff revisado e o commit desta story o excluem corretamente (nenhuma sobreposição de arquivos).
