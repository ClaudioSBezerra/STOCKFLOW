---
title: 'Story 5.1 — Registrar Baixa (consumo)'
type: 'feature'
created: '2026-09-01'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '798a1f484515c4d1d99863daa8d03ea449b68f9e'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-5-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Não existe forma de registrar que um Almoxarife consumiu (deu baixa em) uma quantidade de um Produto num Estoque — o saldo em `produto_estoque` só muda hoje via cadastro inicial (Story 3.1). Sem uma Baixa transacional e auditável, o saldo nunca reflete o consumo real e não há rastro de quem tirou o quê.

**Approach:** Nova tabela `movimentacoes` (schema já pensado para acomodar a Transferência da Story 5.2: `tipo`, `estoque_origem_id`, `estoque_destino_id` nullable, `usuario_id`, `criado_em`). `services.RegistrarBaixa(db, produtoID, estoqueID, usuarioID, quantidade)` valida a quantidade, abre uma transação, trava a linha de `produto_estoque` com `SELECT ... FOR UPDATE`, debita e insere a Movimentação (`tipo='baixa'`) — commit único, nunca uma escrita sem a outra. `handlers.RegistrarBaixaHandler` expõe `POST /api/produtos/{id}/estoques/{estoqueId}/baixa` atrás de `RequireAuth → RequireRole(almoxarife)`, publica no canal SSE `movimentacoes` (AD-3, decisão já fixada no epic-5-context). No frontend, cada linha de "Quantidade por Estoque" em `ProdutoDetalhePage` ganha um botão "Registrar Baixa" que abre um diálogo com input numérico.

## Boundaries & Constraints

**Always:**
- **Migration `000021_create_movimentacoes.up.sql`** (+ `.down.sql`): `CREATE TABLE movimentacoes (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), produto_id UUID NOT NULL REFERENCES produtos(id), tipo VARCHAR(20) NOT NULL CHECK (tipo IN ('baixa','transferencia','ajuste')), estoque_origem_id UUID REFERENCES estoques(id), estoque_destino_id UUID REFERENCES estoques(id), quantidade NUMERIC(10,3) NOT NULL CHECK (quantidade > 0), usuario_id UUID NOT NULL REFERENCES usuarios(id), criado_em TIMESTAMPTZ NOT NULL DEFAULT now())` + `CREATE INDEX idx_movimentacoes_produto_id ON movimentacoes (produto_id)` + `CREATE INDEX idx_movimentacoes_criado_em ON movimentacoes (criado_em DESC)`. Nenhuma FK usa `ON DELETE CASCADE` (nem `produtos`/`estoques`/`usuarios` são excluídos hoje). `estoque_destino_id` fica `NULL` para `tipo='baixa'` — só a Story 5.2 preenche.
- **`services/movimentacoes.go`** (novo): `type Movimentacao struct { ID, ProdutoID, Tipo, EstoqueOrigemID string; EstoqueDestinoID *string; Quantidade float64; UsuarioID string; CriadoEm time.Time }` com tags json `id/produtoId/tipo/estoqueOrigemId/estoqueDestinoId/quantidade/usuarioId/criadoEm`. `ErroMovimentacaoValidacao{Mensagem string}` (mesmo molde de `ErroProdutoValidacao`) e `ErroQuantidadeIndisponivel{Disponivel float64}` cujo `Error()` devolve `fmt.Sprintf("quantidade indisponível: apenas %s unidade(s) disponível(is)", strconv.FormatFloat(e.Disponivel, 'f', -1, 64))` — `strconv.FormatFloat(..., 'f', -1, 64)`, nunca `%v`/`%g` (evita notação científica em valores pequenos, mesmo cuidado de `limiteNumeric103Texto`).
- **`services.RegistrarBaixa(db *sql.DB, produtoID, estoqueID, usuarioID string, quantidade float64) (Movimentacao, error)`**: `quantidade <= 0` → `ErroMovimentacaoValidacao` ("quantidade deve ser maior que zero"), `quantidade > limiteNumeric103` → `ErroMovimentacaoValidacao` (mesmo texto/constante de `CriarProduto`) — ambos ANTES de abrir transação, nenhuma escrita. Depois: `tx.Begin()` + `defer tx.Rollback()` (molde de `ExcluirEstoque`, `services/estoques.go:127-132`); `SELECT quantidade FROM produto_estoque WHERE produto_id=$1 AND estoque_id=$2 FOR UPDATE` — SQLSTATE `22P02` (produtoID OU estoqueID malformado) OU `sql.ErrNoRows` (par válido mas sem linha, ou Produto nunca teve saldo nesse Estoque) colapsam AMBOS em `&ErroQuantidadeIndisponivel{Disponivel: 0}` (ver Design Notes — nenhuma AC desta story pede um 404 distinto aqui); `quantidade > disponivel` → `&ErroQuantidadeIndisponivel{Disponivel: disponivel}`. Senão: `UPDATE produto_estoque SET quantidade = quantidade - $1 WHERE produto_id=$2 AND estoque_id=$3`, depois `INSERT INTO movimentacoes (produto_id, tipo, estoque_origem_id, quantidade, usuario_id) VALUES ($1,'baixa',$2,$3,$4) RETURNING id, produto_id, tipo, estoque_origem_id, quantidade, usuario_id, criado_em` na MESMA transação, `tx.Commit()`. Nunca um debit sem o INSERT correspondente (rollback via `defer` em qualquer erro no meio).
- **`handlers/movimentacoes.go`** (novo): `RegistrarBaixaHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc` — molde exato de `CriarProdutoHandler` (`handlers/produtos.go:103-146`): guard `middleware.UsuarioDaSessao` ausente → 500; decodifica `{"quantidade": float64}` (`http.MaxBytesReader` + `json.Decode`, payload inválido → 400 VALIDATION_ERROR); `produtoID := r.PathValue("id")`, `estoqueID := r.PathValue("estoqueId")`; chama `services.RegistrarBaixa(db, produtoID, estoqueID, usuario.ID, req.Quantidade)`; `switch`: `nil` → `registro.Publish("movimentacoes", realtime.Evento{ID: mov.ID, Change: "created"})` + `escreverJSON(w, 201, map[string]any{"movimentacao": mov})`; `errors.As(&erroValidacao)` → 400 `VALIDATION_ERROR`; `errors.As(&erroIndisponivel)` → 409 `CONFLICT`; `default` → `slog.Error` + 500. Reusar `escreverErro`/`escreverJSON` já existentes no pacote `handlers` (`handlers/auth.go`) — não redeclarar.
- **`main.go`**: registrar `mux.HandleFunc("POST /api/produtos/{id}/estoques/{estoqueId}/baixa", middleware.RequireAuth(db, jwtSecret)(middleware.RequireRole(services.PapelAlmoxarife)(handlers.RegistrarBaixaHandler(db, registro))))`, mesmo bloco/estilo de comentário do registro de `POST /api/produtos/{id}/renomear` (linhas 375-386) — atualizar também o comentário-doc do pacote no topo do arquivo (linhas 1-83) citando a Story 5.1.
- **Truncar `movimentacoes` em TODAS as 16 ocorrências literais de `` `TRUNCATE TABLE importacao_linhas, produto_estoque, produtos, estoques` `` no repo (adicionar `, movimentacoes` ao final da lista em cada uma — mesma string, uma edição mecânica idêntica em cada arquivo): `backend/services/estoques_test.go:22`, `backend/services/produtos_test.go:24`, `backend/handlers/estoques_test.go:27`, `backend/handlers/produtos_test.go:35`, `backend/handlers/importacoes_test.go:30`, `backend/handlers/fotos_test.go:28`, `backend/main_test.go:633,773,878,1003,1628,1681,1731,1800,1925,2062`. Sem essa edição, QUALQUER truncagem de `produtos`/`estoques` nesses arquivos passa a falhar com `0A000` assim que a FK de `movimentacoes` existir — quebra a suíte inteira, não só os testes novos. (`TRUNCATE TABLE usuarios CASCADE`, usado em outros pontos, já cobre `movimentacoes` automaticamente — não precisa de edição.)
- **Frontend — `frontend/src/pages/ProdutoDetalhePage.tsx`**: dentro do bloco "Quantidade por Estoque" (linhas 283-302), cada `<li>` (linha 292-295) ganha um botão "Registrar Baixa" (`variant="outline"`, `size="sm"`), visível só quando `rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife')` (molde de `podeCadastrar`/`podeExportar`, `CatalogoPage.tsx:57,63`, import de `rankPapel` de `@/components/shell/nav-items`; `usuario` vem de `useAuth()`, `@/lib/auth`). Clique abre um `Dialog` (`@/components/ui/dialog`, molde estrutural de `ProdutoDetalhePage.tsx` já usado no lightbox) com um `Input type="number"` (`@/components/ui/input`, molde de `CadastroProdutoSection.tsx:562-567`) para a quantidade e um botão "Confirmar". Ao confirmar: `POST /api/produtos/${id}/estoques/${linha.estoqueId}/baixa` com `headers: { 'Content-Type': 'application/json', ...authHeaders() }`, `body: JSON.stringify({ quantidade: Number(valor) })` (molde exato de `CadastroProdutoSection.tsx:342-350`). `res.ok` → `toast.success('Baixa registrada.')`, fecha o diálogo, `await carregarDetalhe()` (linha 194 — MESMA função que já refaz o `GET /api/produtos/{id}` e atualiza a lista, reusada tal qual). `!res.ok` → mensagem de erro dentro do diálogo lendo `(await res.json()).error.message` (envelope AD-14) — usar o `message` do servidor (já traz a quantidade disponível no 409), não uma string genérica fixa.
- **Testes** em todas as camadas: `services/movimentacoes_test.go` (novo — `TestRegistrarBaixa_Sucesso`, `_QuantidadeZeroOuNegativa`, `_QuantidadeMaiorQueDisponivel` (checa a mensagem cita o valor real), `_ProdutoSemSaldoNesteEstoque` (linha ausente → 0 disponível), `_ConcorrenciaDuasBaixasMesmaLinha` (molde de `TestExcluirEstoque_CorridaComCriarProdutoResidual`, `services/estoques_test.go:333` — duas baixas concorrentes na mesma linha nunca deixam quantidade negativa)); `handlers/movimentacoes_test.go` (novo — 201/400/409/403/401, molde de `handlers/produtos_test.go`); `main_test.go` — novo `TestNewMux_ProdutosBaixaRotaCarregaRequireRole` (molde de `TestNewMux_ProdutosRenomearRotaCarregaRequireRole`, linha 873); `ProdutoDetalhePage.test.tsx` — botão só aparece para `almoxarife`+, submissão bem-sucedida chama `toast.success` + refetch, `409` mostra a mensagem do servidor no diálogo.

**Block If:** nada nesta story depende de decisão humana nem de ação de operador fora do repositório — schema, serviço, handler, rota e UI são inteiramente implementáveis por um agente com o estado atual do repositório. Status final esperado: `done`.

**Never:**
- **Não implementar a Transferência (Story 5.2)** — `estoque_destino_id` fica na tabela (schema já pensado para não exigir migration extra), mas nenhum código desta story o preenche ou lê.
- **Não implementar a tela de Histórico nem a assinatura do canal SSE `movimentacoes` no frontend** (Story 5.3) — a publicação no canal acontece (decisão já fixada em `epic-5-context.md`, AD-3), mas nenhuma tela desta story assina esse canal; o próprio fluxo de Baixa já se atualiza sozinho via `carregarDetalhe()` direto, sem depender de SSE.
- **Não criar rota `GET` para listar Movimentações** — fora do escopo desta story.
- **Nenhuma tela/nav-item novo** — a ação vive inteiramente dentro de `ProdutoDetalhePage`, já acessível a qualquer papel autenticado; o servidor continua a autoridade real (403 para `usuario` mesmo que o botão nunca apareça para esse papel).
- **Nenhum `ON DELETE CASCADE` nas FKs de `movimentacoes`** — Movimentação é registro de auditoria, nunca deve desaparecer silenciosamente.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Baixa válida | `POST .../baixa` com `quantidade` positiva ≤ disponível, sessão `almoxarife`+ | `201 {"movimentacao": {...tipo:"baixa"...}}`; `produto_estoque.quantidade` debitada; evento publicado no canal `movimentacoes` | — |
| Quantidade zero ou negativa | `quantidade: 0` ou `quantidade: -5` | `400 VALIDATION_ERROR`, nenhuma escrita | rejeitado antes de abrir transação |
| Quantidade maior que a disponível | `quantidade` > saldo atual da linha | `409 CONFLICT`, mensagem cita a quantidade real disponível no momento; nada debitado | lock adquirido, comparado, `tx.Rollback()` |
| Produto sem saldo neste Estoque | par `(produto_id, estoque_id)` sem linha em `produto_estoque` | `409 CONFLICT`, mensagem cita "0" disponível | `sql.ErrNoRows` no `SELECT ... FOR UPDATE` |
| `id`/`estoqueId` malformado (não-UUID) | `POST /api/produtos/abc/estoques/xyz/baixa` | `409 CONFLICT`, mesma mensagem de indisponibilidade (0) | `pq` `22P02` colapsa junto de `sql.ErrNoRows` |
| Papel `usuario` | `POST .../baixa`, sessão `usuario` | `403 FORBIDDEN` | decidido por `RequireRole`, handler nunca executa |
| Duas baixas concorrentes na mesma linha somando mais que o disponível | duas transações simultâneas debitando a mesma `(produto_id, estoque_id)` | uma espera a outra (`FOR UPDATE`); a segunda vê o saldo já atualizado e pode ser aceita ou rejeitada corretamente — saldo nunca fica negativo | serialização pelo lock pessimista |

</intent-contract>

## Code Map

- `backend/migrations/000021_create_movimentacoes.up.sql` / `.down.sql` — nova tabela `movimentacoes`, molde de comentário de `000012_create_produto_estoque.up.sql`.
- `backend/services/movimentacoes.go` — `Movimentacao`, `ErroMovimentacaoValidacao`, `ErroQuantidadeIndisponivel`, `RegistrarBaixa`. Reusa `pqInvalidTextRepresentation` (`services/promocao.go:17`), `limiteNumeric103`/`limiteNumeric103Texto` (`services/produtos.go:32,37`). Molde transacional: `ExcluirEstoque` (`services/estoques.go:127-210`).
- `backend/services/movimentacoes_test.go` — novo, ver Boundaries.
- `backend/handlers/movimentacoes.go` — `RegistrarBaixaHandler`. Molde: `CriarProdutoHandler` (`handlers/produtos.go:103-146`).
- `backend/handlers/movimentacoes_test.go` — novo, molde `handlers/produtos_test.go`.
- `backend/main.go` — nova rota (linhas ~375-386, junto ao bloco de Produtos) + doc do pacote.
- `backend/main_test.go` — novo `TestNewMux_ProdutosBaixaRotaCarregaRequireRole` (molde linha 873) + 10 edições de TRUNCATE (ver Boundaries).
- `backend/services/estoques_test.go:22`, `backend/services/produtos_test.go:24`, `backend/handlers/estoques_test.go:27`, `backend/handlers/produtos_test.go:35`, `backend/handlers/importacoes_test.go:30`, `backend/handlers/fotos_test.go:28` — cada um, adicionar `, movimentacoes` ao TRUNCATE.
- `frontend/src/pages/ProdutoDetalhePage.tsx` — botão + diálogo dentro do bloco "Quantidade por Estoque" (linhas 283-302); reusa `carregarDetalhe` (linha 194), `authHeaders` (linha 95), `rankPapel`/`useAuth`.
- `frontend/src/pages/ProdutoDetalhePage.test.tsx` (ou equivalente já existente) — novos casos, ver Boundaries.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000021_*.sql` — tabela `movimentacoes` + índices.
- `backend/services/movimentacoes.go` (+ `_test.go`) — `RegistrarBaixa` transacional com `FOR UPDATE`, validação pré-transação, erros tipados.
- `backend/handlers/movimentacoes.go` (+ `_test.go`) — `RegistrarBaixaHandler`, fronteira HTTP pura, publica SSE no sucesso.
- `backend/main.go` (+ `main_test.go`) — rota atrás de `RequireAuth → RequireRole(almoxarife)`; 16 edições de TRUNCATE nos arquivos de teste listados.
- `frontend/src/pages/ProdutoDetalhePage.tsx` (+ teste) — botão "Registrar Baixa" por linha (gate de papel client-side), diálogo com input numérico, `POST`, toast, refetch via `carregarDetalhe`.

**Acceptance Criteria:**
- Given um Produto com quantidade disponível num Estoque, when um `almoxarife` (ou acima) registra uma Baixa de quantidade válida, then o sistema trava a linha de `produto_estoque` com `FOR UPDATE`, debita a quantidade e cria uma Movimentação `tipo='baixa'` na MESMA transação — nunca uma escrita sem a outra — e publica um evento no canal SSE `movimentacoes`.
- Given uma quantidade zero ou negativa, when o Almoxarife tenta registrar, then o sistema rejeita com `400 VALIDATION_ERROR` antes de qualquer escrita.
- Given uma quantidade maior que a disponível (incluindo o caso de nenhuma linha existir para o par Produto/Estoque), when a Baixa é tentada, then o sistema rejeita com `409 CONFLICT` informando a quantidade real disponível no momento, sem debitar nada.
- Given uma sessão de papel `usuario`, when ela tenta registrar uma Baixa diretamente pela API, then a resposta é `403 FORBIDDEN` e o handler nunca executa.
- Given um `almoxarife` na tela de detalhe do Produto, when ele clica "Registrar Baixa" numa linha de Estoque, informa uma quantidade válida e confirma, then vê um toast de sucesso e a quantidade daquele Estoque atualiza na tela (via refetch); se o servidor rejeitar, a mensagem de erro do servidor aparece no diálogo, sem fechar.

## Spec Change Log

_Vazio — sem loopback de `bad_spec` ainda._

## Review Triage Log

_Vazio — sem pass de review ainda._

## Design Notes

- **Malformado/inexistente colapsa em "quantidade indisponível (0)", não em 404:** as ACs desta story (epics.md, Story 5.1) só preveem dois motivos de rejeição — quantidade inválida e quantidade indisponível — nenhum 404 dedicado. Um `id`/`estoqueId` malformado ou um par `(produto_id, estoque_id)` sem linha em `produto_estoque` são, na prática, sempre acessados a partir de uma tela que já carregou o Produto e a lista de Estoques existentes (fluxo normal nunca produz esses casos); tratá-los como "0 disponível" via o mesmo erro tipado evita inventar um código de erro fora do texto da AC e mantém o serviço simples. Mesmo padrão de colapso de UUID inválido já usado em `ExcluirEstoque`/`ObterProdutoDetalhe`/filtros do Catálogo.
- **Publicação SSE imediata, não adiada para a Story 5.3:** a Story 5.1 do epics.md, isolada, não menciona SSE — mas `epic-5-context.md` (Technical Decisions) é explícito: "Toda criação de Movimentação publica um evento no canal `movimentacoes`", mesmo padrão que `CriarProdutoHandler` já aplica para `produtos` desde a Story 4.4. O canal já está reservado em `realtime/registry.go` (`canaisValidos["movimentacoes"]`), então publicar aqui não exige mudança nenhuma nesse pacote — só a chamada no handler.
- **`estoque_destino_id` nullable desde já:** a Story 5.3 (Histórico) já lista "origem, destino" como colunas exibidas (epics.md), e a Story 5.2 (Transferência) precisa dos dois lados. Criar a coluna agora (sempre `NULL` para `tipo='baixa'`) evita uma migration de alteração de schema na Story 5.2 — decisão estrutural, não funcionalidade antecipada: nenhum código desta story lê ou escreve `estoque_destino_id`.
- **Frontend sem tela nova:** a UI de Movimentações/Histórico (nav "Estoques → Movimentações") é entregue pela Story 5.3. Aqui, o gatilho de Baixa vive dentro de `ProdutoDetalhePage`, que já mostra a quantidade por Estoque (Story 4.4) — extensão natural, mesmo padrão da Story 2.2 (botão "Excluir" adicionado a uma tela existente, não uma tela nova).

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` — Postgres real; cobre `services/movimentacoes_test.go`, `handlers/movimentacoes_test.go`, `main_test.go`, e confirma que as 16 edições de TRUNCATE não quebraram nenhuma suíte existente.
- `cd frontend && npm run lint && npm run build && npm run test` — `oxlint`, `tsc`+`vite`, e os casos novos de `ProdutoDetalhePage.test.tsx`.
- `docker compose up --build` (se disponível): logado como `almoxarife`, abrir um Produto com saldo, clicar "Registrar Baixa", informar quantidade válida → toast + saldo atualizado; tentar quantidade maior que o saldo → mensagem com o valor real disponível; sessão `usuario` chamando a API diretamente → `403`.

**Manual checks (if no CLI):**
- `SELECT quantidade FROM produto_estoque WHERE produto_id=... AND estoque_id=...` reflete o débito exato após um `201`.
- `SELECT * FROM movimentacoes WHERE produto_id=...` mostra a linha `tipo='baixa'` com `estoque_destino_id IS NULL`.
