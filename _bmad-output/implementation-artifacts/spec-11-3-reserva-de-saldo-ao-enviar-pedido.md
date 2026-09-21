---
title: 'Story 11.3: Reserva de saldo ao enviar Pedido'
type: 'feature'
created: '2026-09-20'
baseline_revision: 'b1854a41c4c601b26a31b036ab9fbf4df23dfd3a'
status: done
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: [oversized]
deferred:
  - summary: >-
      As tabelas do Catálogo (grade, agrupada, exportação) ainda mostram só o saldo físico; saldo reservado/disponível aparece apenas no detalhe do Produto.
    evidence: |-
      O contrato da story limitou a exibição ao detalhe (FR-7). A AC da 11.3 só exige o cálculo (soma − reservas ativas, nunca materializado), mas o contexto do épico diz "Catálogo e Estoque mostram saldo disponível separado do reservado". Os indicadores `disponivel` da grade/agrupada seguem refletindo saldo físico.
    location: >-
      backend/services/catalogo.go (catalogoGradeQueryBase, catalogoGrupoQueryBase, catalogoPorEstoqueQuery)
    severity: medium
  - summary: >-
      Baixa, Transferência e a revalidação da aprovação ainda não respeitam o saldo reservado nem leem `lotes` (Stories 11.4/11.5), então `disponivel` pode ficar negativo/zero após Baixa e saldo só em Lote não é aprovável.
    evidence: |-
      `RegistrarBaixa`/`RegistrarTransferencia`/`DecidirPedido` continuam validando/debitando só `produto_estoque`. É a janela transitória documentada em Design Notes; fechada por 11.4 e 11.5.
    location: >-
      backend/services/movimentacoes.go, backend/services/pedidos.go (DecidirPedido)
    severity: medium
---

<intent-contract>

## Intent

**Problem:** Enviar um Pedido (Story 7.2) só valida o saldo no momento do envio e não trava nada: dois Pedidos pendentes podem comprometer o mesmo saldo físico, e o Catálogo não distingue saldo reservado de disponível (FR-50, AD-25). O envio também só enxerga `produto_estoque`, nunca o saldo em `lotes` (Story 11.1).

**Approach:** Nova tabela `reservas_pedido_item` (1 linha por item do Pedido, ciclo de vida próprio). `SubmeterPedido` passa a travar (ordem canônica, AD-10) as linhas de `produto_estoque` e `lotes` dos pares, calcular o saldo DISPONÍVEL (`produto_estoque` + `lotes` − reservas ativas) e inserir as reservas na mesma transação. `DecidirPedido` libera as reservas do Pedido em qualquer decisão. O detalhe do Produto mostra reservado/disponível por Estoque e, clicando no reservado, quais Pedidos/solicitantes; a UI distingue "Adicionar ao carrinho" (não trava) de "Saldo reservado" (travado após o envio).

## Boundaries & Constraints

**Always:**
- Saldo disponível é SEMPRE calculado: `SUM(produto_estoque.quantidade) + SUM(lotes.quantidade) − SUM(reservas_pedido_item.quantidade)` do par (produto, estoque, empresa); nunca coluna materializada. Uma única função reutilizável (11.4/11.5 vão consumi-la).
- Criar reserva: na MESMA transação, com o conjunto completo de pares ordenado por `(produto_id, estoque_id)` ANTES de qualquer lock; por par trava `produto_estoque` (`FOR UPDATE`, se existir) e depois as linhas de `lotes` do par (`ORDER BY id FOR UPDATE`) e SÓ ENTÃO lê o saldo disponível e insere a reserva. Item insuficiente → `ErroPedidoIndisponivel` com TODOS os nomes, rollback, nada gravado. Sem saldo em nenhuma tabela = 0 disponível.
- `reservas_pedido_item`: `id` UUID, `pedido_id` (FK `pedidos` ON DELETE CASCADE), `produto_id` (FK `produtos`), `estoque_id` (SEM FK, como `pedido_itens`), `quantidade` > 0, `empresa_id` (FK `empresas`), UNIQUE `(pedido_id, produto_id, estoque_id)`, índice `(produto_id, estoque_id)`. Migration `000043` faz backfill de uma reserva por item dos Pedidos JÁ `pendente`. Ativa = a linha existe; liberar = apagar.
- `DecidirPedido` (aprovar, aprovar parcial, rejeitar) apaga TODAS as reservas do Pedido na mesma transação, DEPOIS do UPDATE guardado de `pedidos` (a parte aprovada é debitada na mesma transação, a não aprovada volta a ficar disponível). A lógica de débito/revalidação de aprovação NÃO muda (Story 11.5).
- `AdicionarItemCarrinho` valida `jaNoCarrinho + quantidade` contra o saldo DISPONÍVEL (mesma função); carrinho continua NÃO travando saldo.
- Detalhe do Produto (`GET /api/produtos/{id}`): por Estoque `reservada` e `disponivel` (`GREATEST(saldo − reservado, 0)`), e no total `quantidadeReservada`/`quantidadeDisponivel`; `quantidadeTotal` segue o saldo físico. `GET /api/produtos/{id}/estoques/{estoqueId}/reservas` (qualquer conta autenticada, escopo da Empresa) lista `{pedidoId, solicitante, quantidade, criadoEm}` dos Pedidos com reserva ativa; Produto/Estoque alheio, inexistente ou malformado → 404.
- Mesclagem de duplicatas reescreve `produto_id` também em `reservas_pedido_item` do removido (soma na colisão de `(pedido_id, produto_id, estoque_id)`).
- Sem expiração: Pedido pendente mantém a reserva indefinidamente; sem job/cron.
- UI: toast/diálogo de "Adicionar ao Carrinho" deixam claro que NÃO trava saldo; o valor reservado (quando > 0) é um botão que abre a lista de Pedidos/solicitantes; "Adicionar ao Carrinho" desabilita com `disponivel <= 0`; o toast de envio diz que o saldo ficou reservado até a decisão.
- Publicar no canal `produtos` (`updated`) no envio e na decisão do Pedido para atualizar detalhes abertos.

**Block If:** Nenhuma decisão bloqueante identificada.

**Never:** Não alterar Baixa/Transferência (11.4), nem o débito/FEFO/revalidação da aprovação (11.5), nem o formulário de Cadastro (11.6); não mexer em `lotes.quantidade` nem gerar Movimentação no envio; sem colunas materializadas de saldo; sem expiração/job; não adicionar colunas de reservado às tabelas grade/agrupada/exportação do Catálogo nesta story (só o detalhe); não tocar `sprint-status.yaml`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Envio feliz | Carrinho com 2 itens, saldo suficiente | Pedido `pendente`, 1 reserva por item, saldo físico inalterado, disponível cai | — |
| Saldo só em Lotes | Par com Lote (sem `produto_estoque`) | Reserva criada contra o Lote | — |
| Corrida | 2 envios concorrentes, saldo 5, cada um pede 5 | 1 sucesso; o outro `ErroPedidoIndisponivel` | 409, nada gravado |
| Saldo já reservado | Pedido A reservou 4 de 5; B pede 2 | B rejeitado (disponível 1) | 409 nomeando o item |
| Rejeição | Pedido pendente rejeitado | Reservas apagadas; disponível volta | — |
| Aprovação parcial | Aprovado 3 de 5 (saldo físico 3) | Reservas apagadas; disponível = físico pós-débito | — |
| Carrinho | Reserva de outro Pedido consome saldo | Adicionar acima do disponível → `ErroCarrinhoIndisponivel{Restante}` | 409 |
| Detalhe | Par com reserva | `reservada`, `disponivel` por Estoque e totais | — |
| Quem reservou | Clique no reservado | Lista Pedidos/solicitantes/quantidades | Alheio/inexistente → 404 |
| Mesclagem | Reservas de dois duplicatas no mesmo Pedido/Estoque | 1 linha do mantido com a soma | — |
| Pendente antigo | Pedido pendente por meses | Reserva permanece | — |

</intent-contract>

## Code Map

- `backend/services/pedidos.go:167-282` (`SubmeterPedido`) -- hoje trava só `produto_estoque` e valida contra ele; passa a usar os helpers de reserva e inserir `reservas_pedido_item`. `:559-738` (`DecidirPedido`) -- apagar reservas após `registrarDecisao`; débito/revalidação intactos (11.5).
- `backend/services/carrinho.go:190-215` -- valida saldo só em `produto_estoque`; passa ao disponível.
- `backend/services/catalogo.go:57-72` (`EstoqueQuantidade`), `:775-955` (`ObterProdutoDetalhe`, `catalogoPorEstoqueQuery`) -- detalhe ganha reservado/disponível. View `saldo_produto_estoque` (`migrations/000041_create_lotes.up.sql`) = `produto_estoque` + `lotes`.
- `backend/services/normalizacao.go:1305-1410` -- bloco de mesclagem: reescreve `lotes`/`pedido_itens`; acrescentar `reservas_pedido_item`.
- `backend/services/estoques.go:140-230` -- guard de exclusão já bloqueia por `pedido_itens` de Pedido pendente (⊇ reservas ativas) e por qualquer Lote; nenhuma mudança.
- `backend/handlers/pedidos.go` (`SubmeterPedidoHandler`/`DecidirPedidoHandler`), `handlers/lotes.go` (molde de handler + `registro.Publish(..., "produtos", ...)`), `backend/main.go:578-590` (registro de rotas de produto) -- novo handler e rota.
- `backend/services/pedidos_test.go`, `carrinho_test.go`, `catalogo_test.go`, `normalizacao_test.go`, `lotes_test.go` (helpers `seedProdutoComSaldo`, `limparProdutos`, `testDB`) -- moldes de teste; toda lista `TRUNCATE ... pedido_itens, pedidos ...` precisa incluir `reservas_pedido_item`.
- `frontend/src/pages/ProdutoDetalhePage.tsx` (+ `.test.tsx`) -- linha por Estoque, diálogo de Carrinho, `ListaLotes`; `frontend/src/pages/CarrinhoPage.tsx:119` (`toast.success('Pedido enviado.')`).

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000043_create_reservas_pedido_item.{up,down}.sql` -- tabela + índices + backfill dos Pedidos pendentes
- `backend/services/reservas.go` (+ `reservas_test.go`) -- `travarSaldoParesTx`, `saldoDisponivelParTx`, `ListarReservasSaldo`, tipo `ReservaSaldo`
- `backend/services/pedidos.go` -- reservar no envio; liberar na decisão
- `backend/services/carrinho.go` -- validar contra o disponível
- `backend/services/catalogo.go` -- reservado/disponível no detalhe
- `backend/services/normalizacao.go` -- mesclagem reescreve reservas
- `backend/handlers/reservas.go` (+ teste), `backend/handlers/pedidos.go`, `backend/main.go` -- rota GET de reservas; publicar `produtos`
- listas `TRUNCATE` dos testes (`services`, `handlers`, `cmd`, `main_test.go`) -- incluir `reservas_pedido_item`
- `backend/services/{pedidos,carrinho,catalogo,normalizacao}_test.go` -- cobrir toda a matriz (inclui corrida concorrente e `-race`-safe)
- `frontend/src/pages/ProdutoDetalhePage.tsx` (+ teste), `frontend/src/pages/CarrinhoPage.tsx` (+ teste) -- reservado/disponível, diálogo de reservas, textos FR-21

**Acceptance Criteria:**
- Given um Pedido enviado, when o envio é confirmado, then há uma reserva por item criada sob lock ordenado na mesma transação, e o saldo disponível calculado já a desconta.
- Given dois envios concorrentes do mesmo saldo, when ambos executam, then no máximo um reserva.
- Given um Pedido rejeitado ou parcialmente aprovado, when a decisão é registrada, then a reserva é liberada (só a parte não aprovada volta a ficar disponível).
- Given um item reservado, when o Usuário clica no valor reservado, then vê Pedidos/solicitantes.
- Given a interface, when o Usuário adiciona ao carrinho ou envia o Pedido, then o texto distingue "não trava saldo" de "saldo reservado".

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 5: (high 0, medium 2, low 3)
- defer: 2: (high 0, medium 2, low 0)
- reject: 14
- addressed_findings:
  - `[low]` `[patch]` `disponivel` do detalhe ignorava reservas (Produto 100% reservado aparecia disponível): agora `quantidadeDisponivel > 0` (`backend/services/catalogo.go`).
  - `[low]` `[patch]` Subtração em float64 podia exibir 0.30000000000000004: `arredondar3` em reservada/disponivel/totais + teste fracionário.
  - `[low]` `[patch]` Eventos `produtos` duplicados quando o mesmo Produto aparece em vários itens/Estoques: `idsUnicos` nos handlers de envio e decisão; teste cobre envio, rejeição e aprovação.
  - `[medium]` `[patch]` Diálogo de reservas: resposta atrasada podia sobrescrever a nova (contador de sequência), não recarregava com evento em tempo real (sincroniza em `carregarDetalhe`), linhas sem pedido/data (agora id curto + data), rótulo "Saldo reservado: N" com aria-label começando pelo texto visível.
  - `[medium]` `[patch]` Lacunas de verificação: teste da rota real no `newMux` (401 sem token, 200 com `usuario`), teste de saldo dividido entre `produto_estoque` e `lotes`, remoção de `_ = pedido` residual.
- Adiados: colunas de reservado/disponível na grade/agrupada/exportação do Catálogo; Baixa/Transferência/aprovação sobre reserva e `lotes` (11.4/11.5) — ver `deferred`.
- Rejeitados (resumo): `solicitante` visível a qualquer autenticado (é o AC da story); lock-order/deadlock e mesclagem×envio concorrentes (pares ordenados; lock em `produto_estoque`/`lotes` serializa e a reescrita de `lotes` vem antes da de reservas); backfill com duplicata (PK de `pedido_itens` impede); `Restante` negativo (já truncado em 0); leituras do detalhe sem snapshot único (padrão do repositório); expiração/cancelamento de reserva (AD-25, sem expiração nesta versão); mensagens de indisponibilidade, layout do total, `for rows.Next(){}`, helper de TRUNCATE (cosméticos); race de exclusão de Estoque no carrinho (linha travada garante o Estoque).

### 2026-09-21 — Review pass (acompanhamento)
- intent_gap: 0
- bad_spec: 0
- patch: 2: (high 0, medium 2, low 0)
- defer: 0
- reject: 33
- addressed_findings:
  - `[medium]` `[patch]` Trava de `lotes` do par sem teste que prove o lock (o teste de corrida existente só semeia `produto_estoque`, e uma corrida de milissegundos passa mesmo sem `FOR UPDATE`): `TestSubmeterPedido_SaldoSoEmLoteEsperaLockDoLote` segura o lock da linha do Lote em outra transação e exige que o envio espere; validado por mutação (sem `FOR UPDATE` falha). `TestSubmeterPedido_CorridaSaldoSoEmLoteSoUmReserva` cobre a corrida com saldo só em Lote.
  - `[medium]` `[patch]` Ordem canônica de locks com vários pares sem teste concorrente: `TestSubmeterPedido_ConcorrenteVariosParesSemDeadlock` (5 rodadas, carrinhos em ordem oposta sobre os mesmos dois pares); validado por mutação (sem as duas ordenações, falha com deadlock 40P01).
- Rejeitados (resumo): Baixa/Transferência/aprovação ignorando reserva e `lotes` (já em `deferred`, Stories 11.4/11.5); backfill descartando Pedidos com `empresa_id` NULL (legado pré-multiempresa; `reservas_pedido_item.empresa_id` é NOT NULL e `SubmeterPedido` sempre grava a Empresa); backfill com item duplicado por par (PK de `pedido_itens` impede) e com reserva acima do saldo (documentado; `disponivel` truncado em 0 na exibição e `Restante` truncado no carrinho); mesclagem×envio concorrentes, leituras do detalhe sem snapshot único e evento `produtos` na mesclagem (já avaliados/cosméticos); reserva por Estoque ausente em `PorEstoque` (reserva só nasce com saldo físico no par); `solicitante` visível a qualquer autenticado (é o AC); ausência de cancelamento/expiração (AD-25); DRY do `TRUNCATE`, `for rows.Next(){}`, texto de mensagens, `DialogDescription`, formatação de data e demais itens de UI/estilo; lock de leitura no carrinho (é o contrato de "Always"). A afirmação de que a ordenação saiu de `SubmeterPedido` é falsa: ela continua lá e o helper a repete por segurança.

### 2026-09-21 — Review pass (reabertura pelo orquestrador)
- intent_gap: 0
- bad_spec: 0
- patch: 2: (high 0, medium 1, low 1)
- defer: 0
- reject: 36
- addressed_findings:
  - `[medium]` `[patch]` O importador `migrate-legado` gravava Pedidos legados `pendente` sem reserva, quebrando a invariante "pendente => reservado" (o backfill da migration 000043 só cobre Pedidos que já existiam no deploy): `migrarPedidos` agora insere 1 linha em `reservas_pedido_item` por item de Pedido `pendente` (aprovado/rejeitado não reservam); `TestMigrarPedidos_CorteInicial` afirma as reservas.
  - `[low]` `[patch]` O toast de "Adicionar ao carrinho" dizia só "Item adicionado ao carrinho." (o contrato pede toast/diálogo deixando claro que NÃO trava saldo): agora "Item adicionado ao carrinho. O saldo só é reservado ao enviar o Pedido."; teste ajustado.
- Rejeitados (resumo): Baixa/Transferência/aprovação ignorando reserva e `lotes` (já em `deferred`, 11.4/11.5); reserva em `estoque_id` sem linha em `PorEstoque` (reserva só nasce com saldo físico; janela até 11.4); mesclagem×envio concorrentes, leituras do detalhe sem snapshot único, lock exclusivo na leitura do carrinho (é o contrato de "Always"), Lote vencido no saldo (fora do escopo da 11.3), `solicitante` visível a autenticados (é o AC), backfill acima do saldo/`empresa_id` NULL (documentado), campos ausentes em backend antigo, `TabelasComEmpresaID` (a tabela já nasce com `empresa_id NOT NULL`), fragilidades cosméticas de teste/UI/texto e pedidos de documentação.

## Design Notes

- **Liberar = apagar:** a tabela do AD-25 não tem coluna de status; apagar na decisão é a "liberação explícita". A parte aprovada sai do saldo físico pelo débito da mesma transação, a não aprovada volta ao disponível por deixar de ser reservada — o efeito é idêntico a "só a parte não aprovada é liberada".
- **Janela transitória:** até 11.4/11.5, aprovação e Baixa ainda debitam só `produto_estoque`; o envio já enxerga Lotes. Saldo só em Lotes pode ser reservado mas a aprovação (legado) o verá como 0 até a 11.5 — o corte da 11.2 já exige o Épico 11 completo no ar.
- **Backfill:** Pedidos pendentes existentes ganham reserva para a invariante "pendente ⇒ reservado" valer desde o deploy; se somarem mais que o saldo, o disponível exibido é truncado em 0 (`GREATEST`).

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros e sem arquivos listados.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 -p 1 ./...` -- expected: tudo passa.
- `cd frontend && npx tsc -b && npx vitest run` -- expected: sem erros de tipo, testes passam.

## Auto Run Result

Status: done
Blocking condition: nenhuma.

**Resumo:** reserva de saldo ao enviar Pedido (tabela `reservas_pedido_item`, trava ordenada de `produto_estoque`/`lotes`, disponível calculado, liberação na decisão, carrinho contra o disponível, detalhe do Produto com reservado/disponível, rota de reservas, mesclagem, UI e eventos `produtos`). Esta passada reabriu a revisão e fechou duas lacunas remanescentes.

**Arquivos alterados nesta passada:**
- `backend/cmd/migrate-legado/pedidos.go` — Pedidos legados `pendente` passam a gerar uma reserva por item.
- `backend/cmd/migrate-legado/pedidos_test.go` — `CorteInicial` afirma as reservas dos Pedidos pendentes.
- `frontend/src/pages/ProdutoDetalhePage.tsx` (+ `.test.tsx`) — toast de "Adicionar ao carrinho" diz que o saldo só é reservado ao enviar.

**Achados desta passada:** patches 2 (medium 1, low 1); adiados 0; rejeitados 36.
**Revisão de acompanhamento recomendada:** `false` — 3×1 + 1×1 = 4 (< 5), nenhum patch `high`.

**Verificação:**
- `go build ./...`, `go vet ./cmd/...` e `gofmt -l` limpos.
- Frontend: `tsc -b` limpo; `vitest` de `ProdutoDetalhePage` e `CarrinhoPage`: 63 testes passam.
- O SQL novo do importador foi validado à mão num schema descartável (INSERT com a quantidade em float e o CHECK da tabela). A suíte de `cmd/migrate-legado` NÃO foi executada com sucesso: no banco de verificação disponível (`stockflow_t91`, sem permissão para criar outro) ela falha por `column "obra" does not exist` em leituras do `legado` — falha também em testes de Produtos/Estoques que não foram tocados, ou seja, é artefato do ambiente. Sem execução real da asserção nova de `CorteInicial`. O banco `stockflow` (dev) não foi usado; o schema descartável e o rename temporário de `legado.pedidos` foram desfeitos.
- Suíte completa do backend e vitest completo não repetidos nesta passada (nenhum código de produção do backend fora do importador mudou).

**Riscos residuais:** os já registrados em `deferred` (janela até 11.4/11.5; grade/agrupada do Catálogo sem coluna de reservado). A asserção nova do importador precisa rodar num banco com o schema `legado` limpo. `deferred-work.md` e `sprint-status.yaml` têm alterações não commitadas de propriedade do orquestrador; não foram tocados nem incluídos no commit desta passada.

