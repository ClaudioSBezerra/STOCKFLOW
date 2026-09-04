---
title: 'Story 7.7 — Migração de Pedidos e vínculo com Histórico'
type: 'feature'
created: '2026-09-03'
status: 'in-progress'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '8eec4f67dbbdf8bbdc1155bedbab1da6c86e6307'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-7-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** As Stories 7.1–7.6 entregam montar, enviar, consultar, decidir e emitir recibo de Pedidos no schema novo, mas os Pedidos que já existem no sistema legado (protótipo Firestore, hoje espelhado num PostgreSQL local — addendum §F, coleção `pedidos`) ainda não têm caminho para as tabelas `pedidos`/`pedido_itens`. No corte "big-bang" (AD-15, PRD §9) as solicitações e aprovações anteriores ao corte se perderiam, e o vínculo entre uma Movimentação migrada (Story 5.4) e o Pedido legado que a originou desapareceria.

**Approach:** Novo `migrarPedidos(alvo, legado *sql.DB, executar bool)` em `backend/cmd/migrate-legado`, chamado sequencialmente **depois** de `migrarMovimentacoes` no mesmo `main()`. Lê `legado.pedidos`, recria cada Pedido com seus itens referenciando os novos `produto_id` (via `migracao_id_map` `entidade='produto'`) e `estoque_id` (via `estoques.nome_normalizado`), preservando `status`, `criado_em` e — para Pedidos decididos — `decidido_em`. Nova migration `000028` acrescenta a coluna anulável `movimentacoes.pedido_id` (ERD herdado, addendum §A: "MOVIMENTACOES … + pedido nullable"); depois de recriar um Pedido, `migrarPedidos` faz `UPDATE movimentacoes SET pedido_id = <novo>` nas Movimentações já migradas cujas linhas de `legado.historico` referenciam aquele Pedido legado. Idempotência pelo PK `(entidade, id_legado)` de `migracao_id_map` (`entidade='pedido'`). Todo Pedido que não resolve por inteiro vai para o relatório efêmero `PendentesRevisao` e é pulado, sem interromper o lote. O corte real em produção é humano-disparado → `status: awaiting-operator`.

## Boundaries & Constraints

**Always:**
- `migrarPedidos(alvo, legado *sql.DB, executar bool) (ResultadoMigracaoPedidos, error)` — molde estrutural de `migrarMovimentacoes` (`movimentacoes.go:136`): pré-condição de seed → carga dos mapas fora de transação → **uma única transação para todo o lote** → `migracao_id_map`. Pedido já em `migracao_id_map` (`entidade='pedido'`, `id_legado = legado.pedidos.id`) → `JaMigrados++`, pula (inclusive o `UPDATE` de vínculo, já feito na 1ª passada). `!executar` (dry-run) só conta `Migrados`/`JaMigrados`/`PendentesRevisao`/`AvisosData` via `SELECT`, sem escrever.
- **Pré-condição de seed:** reaproveita `emailUsuarioMigracaoLegado` e `errSeedUsuarioMigracaoAusente` (`movimentacoes.go:42,49`). Resolve o id do usuário sintético "Migração do sistema legado" por `SELECT id FROM usuarios WHERE lower(email) = lower($1)` ANTES de ler qualquer linha legada. Ausente → devolve `errSeedUsuarioMigracaoAusente`, nada escrito. (`main()` já faz o fast-fail dessa checagem no topo — spec-5-4.)
- **Migration nova `backend/migrations/000028_add_pedido_to_movimentacoes.{up,down}.sql`** — único artefato de schema. `up`: `ALTER TABLE movimentacoes ADD COLUMN pedido_id UUID REFERENCES pedidos(id)` (ANULÁVEL, **SEM `ON DELETE CASCADE`** — Movimentação é trilha de auditoria, mesmo padrão das outras FKs de `movimentacoes`, migration 000021) + `CREATE INDEX idx_movimentacoes_pedido_id ON movimentacoes (pedido_id)`. `down`: `DROP INDEX` + `DROP COLUMN`.
- **Resolução de `usuario_id` do Pedido:** `lower(legado.pedidos.email)` casado contra `usuarios.email` (índice `idx_usuarios_email_lower`). Sem match (ou `email` nulo/vazio) → usa o id do usuário sintético e registra em `AvisosData` (`uid` legado é auth id do Firestore, sem mapeamento no schema novo — ignorado). Nunca é pendência: o Pedido continua rastreável pelo texto livre `solicitante`.
- **Itens (`legado.pedidos.itens`, array de `{prodId, nome, unidade, estoque, qtd, categoria}`):** para cada item — `prodId` → `migracao_id_map (entidade='produto', id_legado)` → `produto_id` novo; `estoque` (nome) normalizado **pelo próprio Postgres** com a mesma expressão de `estoques.nome_normalizado` / `normExpr` (migration 000008 — nunca reimplementada em Go) → casada contra `estoques.nome_normalizado` no alvo; `nome` → `produto_nome` (snapshot verbatim); `categoria` → `categoria_nome` (snapshot verbatim); `qtd` lida como texto e validada com `strconv.ParseFloat` + `math.IsNaN`/`math.IsInf` + faixa `[0.001, 9999999.999]` de `NUMERIC(10,3) CHECK (quantidade > 0)` (mesma guarda endurecida de `migrarMovimentacoes`). `unidade` **não migra** (o modelo-alvo `pedido_itens` não tem essa coluna — precedente 5.4 com `obs`/`unidade`).
- **Falha de resolução é POR PEDIDO, não por item:** qualquer item irresolúvel (produto fora de `migracao_id_map`, `estoque` sem match, `nome`/`categoria` ausente, `qtd` inválida) OU `status` fora de `{pendente, aprovado, rejeitado}` → o **Pedido inteiro** entra em `PendentesRevisao` (com `id_legado`, `solicitante`, `status`, `qtd_itens` e `motivo`) e NADA dele é inserido; os demais Pedidos do lote migram. Um Pedido gravado sem um de seus itens misrepresentaria o que foi solicitado (AC #1: "cada Pedido é recriado com seus itens").
- **Itens colidindo no par `(produto_id, estoque_id)` dentro do MESMO Pedido legado** (PK-alvo `(pedido_id, produto_id, estoque_id)`) → soma as quantidades numa única linha (molde soma-e-descarta de `MesclarDuplicatas`, `services/normalizacao.go:1265`), nunca duas linhas nem pendência.
- **`INSERT INTO pedidos`** inclui explicitamente `criado_em` = `COALESCE(legado.pedidos.criadoEm, now())` (preservação; `now()` de fallback → `AvisosData`). `status` verbatim. `solicitante` = `COALESCE(solicitante,'')`, `obra_centro_custo` = `COALESCE(obra,'')`, `observacao` = `obs` (anulável). Para `status ∈ {aprovado, rejeitado}`: `decidido_por` = id do usuário sintético e `decidido_em` = `COALESCE(atualizadoEm, criadoEm, now())`; para `pendente`: ambos `NULL`.
- **`INSERT INTO pedido_itens`** grava `quantidade_aprovada` conforme o `status`: `aprovado` → `= quantidade`; `rejeitado` → `0`; `pendente` → `NULL` (respeita `CHECK (quantidade_aprovada IS NULL OR (>= 0 AND <= quantidade))` e a semântica da Story 7.5 — nunca `NULL` depois de decidido).
- **Vínculo Movimentação↔Pedido:** depois de inserir o Pedido, `migrarPedidos` lê os `id` de `legado.historico` que referenciam aquele Pedido legado (campo `pedido` de `legado.historico` — ver Design Notes e `operator_actions`), resolve cada um por `migracao_id_map (entidade='movimentacao', id_legado)` → `id_novo`, e faz `UPDATE movimentacoes SET pedido_id = $1 WHERE id = ANY($2)`. Linha de `historico` referenciada mas ausente de `migracao_id_map` (ela própria foi `PendenteRevisao` na 5.4) → contabilizada em `AvisosData` ("pedido X: N movimentação(ões) do histórico não migradas — vínculo não estabelecido"), nunca aborta.
- **Transação única do lote** (molde `migrarEstoques`/`migrarMovimentacoes`): o script roda fora do runtime, nenhum efeito colateral pós-commit, nenhum evento SSE no canal `pedidos`. Pré-checagens (seed, leitura de `legado.pedidos`, carga de `migracao_id_map` produto + `estoques.nome_normalizado`) rodam antes de `alvo.Begin()`. Só erros inesperados (conexão, `legado.pedidos` ilegível, seed ausente, backstop `23505`) abortam; o resto vira pendência.
- **`main()` (`main.go:83`)** chama `migrarPedidos` **depois** de `migrarMovimentacoes` (precisa das Movimentações já em `migracao_id_map` para o vínculo). Ramos de stderr novos: `errSeedUsuarioMigracaoAusente` (reconhecido com `errors.Is`); `legado.pedidos` ilegível; impressão de `PendentesRevisao`/`AvisosData` no fim (não é erro). Doc do pacote (topo de `main.go`): 1 linha citando Pedidos.
- **Testes (`pedidos_test.go`, pacote `main`):** `testDB` (`main_test.go`) ganha a fixture `legado.pedidos` (`id text primary key, solicitante text, obra text, obs text, email text, uid text, itens jsonb, status text, criado_em timestamptz, atualizado_em timestamptz`) e a coluna anulável `pedido text` em `legado.historico`; `limparTabelas` ganha `DELETE FROM legado.pedidos`, `DELETE FROM pedido_itens` e `DELETE FROM pedidos` — `movimentacoes` (FK `pedido_id` sem cascade) e `pedido_itens` limpos ANTES de `pedidos`; `usuarios` NUNCA é limpa. Cobrir toda a I/O & Edge-Case Matrix.

**Block If:**
- _(nenhuma decisão que exija humano em tempo de implementação — o corte real de produção é `operator_action`, não `blocked`)_

**Never:**
- Não tocar `services.DecidirPedido` (Story 7.5) para gravar `pedido_id` nas Movimentações do runtime — fora do escopo desta AC (só migração). A coluna nova fica só-migração por ora, exatamente como `movimentacoes.criado_em` explícito e `migracao_id_map` já são (migration 000021: "schema pensado para não exigir migration de alteração depois").
- Não alterar `services.ListarMovimentacoes` (Story 5.3), o JSON de `GET /api/pedidos*` (Stories 7.3/7.4) nem `MontarReciboPedidoConteudo` (Story 7.6) — a coluna `pedido_id` é anulável e nenhum SELECT existente a lê.
- Não criar rota HTTP, cron ou chamada do runtime para a migração (AD-15) — só o binário `cmd/migrate-legado` com a flag `--executar`.
- Não rodar o corte contra o espelho Postgres real da empresa — `operator_action` (AD-15, PRD §9), mesma restrição das Stories 2.3/3.7/5.4.
- Não abortar o lote por Pedido irresolúvel — só erros inesperados abortam; o resto vira pendência.
- Não migrar `unidade` do item nem criar coluna nova em `pedido_itens`/`pedidos`; não inferir o vínculo Movimentação↔Pedido por heurística de nome/qtd/timestamp — só pelo campo de referência de `legado.historico`.
- Não mapear `status` legado para `parcialmente_aprovado` — o legado nunca produz esse valor (Story 7.5).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Corte inicial | `legado.pedidos` com N Pedidos resolvíveis (itens com produto migrado, estoque conhecido, `qtd`/`status` válidos), `--executar` | N linhas em `pedidos` + as linhas de `pedido_itens` correspondentes (`produto_id`/`estoque_id` resolvidos, `produto_nome`/`categoria_nome` snapshot, `criado_em` legado, `status` verbatim); N linhas `entidade='pedido'` em `migracao_id_map`; resumo `Migrados=N` | — |
| Reexecução (idempotência) | Mesma entrada, corte já rodado, `--executar` | Nenhuma linha nova em `pedidos`/`pedido_itens`/`migracao_id_map`; `pedido_id` das Movimentações inalterado; resumo `JaMigrados=N` | Pedido já no mapa → pula |
| Pedido pendente | `legado.pedidos.status='pendente'`, itens resolvíveis | `pedidos.status='pendente'`, `decidido_por`/`decidido_em` NULL, cada `pedido_itens.quantidade_aprovada` NULL | — |
| Pedido aprovado | `status='aprovado'` | `pedidos.status='aprovado'`, `decidido_por`=usuário sintético, `decidido_em`=`atualizadoEm` legado, cada `quantidade_aprovada` = `quantidade` | — |
| Pedido rejeitado | `status='rejeitado'` | `pedidos.status='rejeitado'`, `decidido_por`=usuário sintético, `decidido_em`=`atualizadoEm`, cada `quantidade_aprovada` = 0 | — |
| Autor resolvido por e-mail | `legado.pedidos.email` casa uma linha de `usuarios` | `pedidos.usuario_id` = essa linha; sem `AvisosData` | — |
| Autor não resolvido | `email` nulo/vazio ou sem match em `usuarios` | `pedidos.usuario_id` = usuário sintético; entra em `AvisosData` | não é pendência |
| Item com produto não migrado | Um item cujo `prodId` não está em `migracao_id_map` (`entidade='produto'`) | Pedido INTEIRO em `PendentesRevisao` ("item com produto não migrado: prodId=…"); nada dele inserido; demais Pedidos migram | não aborta |
| Item com estoque desconhecido | `item.estoque` sem match em `estoques.nome_normalizado` | Pedido INTEIRO em `PendentesRevisao` ("item com estoque '<nome>' não encontrado"); demais migram | não aborta |
| Item sem nome/categoria | `item.nome` ou `item.categoria` nulo/vazio | Pedido INTEIRO em `PendentesRevisao` ("item sem nome" / "item sem categoria") | não aborta |
| Quantidade inválida | `item.qtd` nula, não-numérica, `NaN`/`Inf`, `< 0.001` ou `> 9999999.999` | Pedido INTEIRO em `PendentesRevisao` ("quantidade inválida: '<v>'") | não aborta |
| Status inválido | `legado.pedidos.status` ∉ {pendente, aprovado, rejeitado} | Pedido INTEIRO em `PendentesRevisao` ("status inválido: '<v>'") | não aborta |
| Itens colidindo no par produto/estoque | Dois itens do mesmo Pedido com o mesmo `prodId`+`estoque` | Uma linha `pedido_itens` com `quantidade` = soma; Pedido migrado normalmente | não é pendência |
| `criadoEm` ausente | `legado.pedidos.criadoEm` NULL, Pedido resolvível | Pedido migrado com `criado_em = now()`; entra em `AvisosData` | não é pendência |
| Vínculo Movimentação↔Pedido | Pedido aprovado legado + linhas de `legado.historico` com `pedido = <id legado>` já migradas por 5.4 | `movimentacoes.pedido_id` dessas linhas = id novo do Pedido | — |
| Movimentação do vínculo não migrada | `legado.historico.pedido` referencia o Pedido, mas a linha do historico ficou `PendenteRevisao` na 5.4 | Pedido migra; `AvisosData` registra as movimentações não vinculadas | não aborta |
| Dry-run | Entrada válida, sem `--executar` | stdout: `migraria N, já migrados J, pendências P`; `SELECT count(*) FROM pedidos` inalterado; nenhum `pedido_id` alterado | — |
| `legado.pedidos` ilegível | `SELECT ... FROM legado.pedidos` falha (tabela ausente / estrutura divergente) | `error` não-nil; `os.Exit(1)`; nada escrito | erro propagado |
| Seed do usuário ausente | Nenhuma linha `usuarios` com o e-mail sentinela (migration 000022 não aplicada) | `errSeedUsuarioMigracaoAusente` antes de qualquer escrita; `os.Exit(1)` | pré-condição, antes do loop |
| Falha inesperada no INSERT | `INSERT`/`UPDATE` falha no meio do lote (ex. backstop `23505`) | `tx.Rollback()` via defer; nada escrito (transação única do lote); `error` cita `id_legado`; `os.Exit(1)` | rollback, nada parcial |

</intent-contract>

## Code Map

- `backend/migrations/000028_add_pedido_to_movimentacoes.up.sql` (+ `.down.sql`) — **NOVO**, único artefato de schema. `ADD COLUMN pedido_id UUID REFERENCES pedidos(id)` anulável, sem CASCADE + `idx_movimentacoes_pedido_id`. Molde de comentário/decisão: `000021_create_movimentacoes.up.sql` ("Nenhuma FK usa ON DELETE CASCADE") e `000027_add_decisao_pedidos.up.sql` (ALTER em `pedidos`/`pedido_itens`).
- `backend/cmd/migrate-legado/pedidos.go` — **NOVO**. `migrarPedidos`, `resolverPedido` (resolução por Pedido, todas as classes de pendência), `pendenteDePedido`, structs de relatório (`ResultadoMigracaoPedidos` com `Migrados`/`JaMigrados`/`PendentesRevisao []PendenteRevisaoPedido`/`AvisosData []string`; `PendenteRevisaoPedido` com `IDLegado`/`Solicitante`/`Status`/`QtdItens`/`Motivo`). Reusa `emailUsuarioMigracaoLegado`, `errSeedUsuarioMigracaoAusente`, `pqUniqueViolation`, e a expressão de normalização de `normExpr` (`main.go:47`) aplicada Postgres-side ao `item.estoque`.
- `backend/cmd/migrate-legado/movimentacoes.go` — **read-only, molde direto**: `migrarMovimentacoes` (pré-condição de seed, carga de mapas fora de transação, transação única do lote, `PendentesRevisao`/`AvisosData`, guarda de `qtd` endurecida com `math.IsNaN`/`math.IsInf`/faixa). Reaproveitar as constantes/sentinelas exportadas no pacote; extrair a guarda de quantidade para um helper compartilhado se ficar natural.
- `backend/cmd/migrate-legado/main.go` — `main()` (`main.go:83`): chamada sequencial a `migrarPedidos` após `migrarMovimentacoes` (após o bloco `resMov`, ~linha 305); ramos de stderr para "seed ausente", "legado.pedidos ilegível" e impressão de `PendentesRevisao`/`AvisosData`; `os.Exit(1)` nos caminhos de erro. Doc do pacote (topo, linhas 1–17): 1 linha citando Pedidos e o vínculo com o Histórico.
- `backend/cmd/migrate-legado/main_test.go` — `testDB` (~linha 117): criar `legado.pedidos` (`id text primary key, solicitante text, obra text, obs text, email text, uid text, itens jsonb, status text, criado_em timestamptz, atualizado_em timestamptz`) e `ALTER TABLE legado.historico ADD COLUMN IF NOT EXISTS pedido text`. `limparTabelas` (~linha 166): acrescentar `DELETE FROM legado.pedidos`, `DELETE FROM pedido_itens`, `DELETE FROM pedidos` — `movimentacoes` e `pedido_itens` ANTES de `pedidos` (FK `movimentacoes.pedido_id` sem cascade; `pedido_itens.produto_id` → `produtos` sem cascade). NUNCA `DELETE FROM usuarios`.
- `backend/cmd/migrate-legado/pedidos_test.go` — **NOVO**. `TestMigrarPedidos_*` (molde de `TestMigrarMovimentacoes_*`). Helpers: `inserirLegadoPedido(t, alvo, input)` (monta o `itens` jsonb), seeder que cria Produto no alvo + linha em `migracao_id_map` (`entidade='produto'`) + `seedEstoqueAlvo` (`INSERT INTO estoques (nome) RETURNING id`), e um seeder de Movimentação migrada + linha `legado.historico` com `pedido` preenchido. Cobre toda a I/O & Edge-Case Matrix.
- `backend/migrations/000026_create_pedidos.up.sql` / `000027_add_decisao_pedidos.up.sql` — **read-only, referência de schema**: `pedidos` (`solicitante`/`obra_centro_custo` `NOT NULL`, `status CHECK`, `decidido_por`/`decidido_em` anuláveis, sem CASCADE); `pedido_itens` (PK `(pedido_id, produto_id, estoque_id)`, `estoque_id` sem FK, `produto_nome`/`categoria_nome` `NOT NULL`, `quantidade_aprovada CHECK (NULL OR 0..quantidade)`).
- `backend/migrations/000009_create_migracao_id_map.up.sql` — **read-only**: mapa compartilhado; `entidade='pedido'` já no vocabulário documentado; PK `(entidade, id_legado)` = idempotência.
- `backend/services/pedidos.go:778` `MontarReciboPedidoConteudo` — **read-only, referência**: `SELECT u.nome, p.decidido_em FROM pedidos p JOIN usuarios u ON u.id = p.decidido_por` é INNER JOIN — por isso Pedido migrado `aprovado`/`rejeitado` recebe `decidido_por` = usuário sintético (não NULL), para o recibo (Story 7.6) continuar funcionando.
- `backend/services/movimentacoes.go:71` `ListarMovimentacoes` / `backend/services/pedidos.go` (SELECTs de `GET /api/pedidos*`) — **read-only**: SELECT com colunas explícitas, nenhum lê `pedido_id` — a coluna nova não perturba nenhuma listagem.
- `backend/services/normalizacao.go:1265` `MesclarDuplicatas` — **read-only, molde**: soma-e-descarta na colisão de chave composta `(pedido_id, produto_id, estoque_id)`.
- `_bmad-output/planning-artifacts/prds/prd-stockflow-2026-08-29/addendum.md` §F "Coleção `pedidos`" — **read-only**: estrutura legada assumida (`solicitante`, `obra`, `obs`, `email`/`uid`, `itens` de `{prodId, nome, unidade, estoque, qtd, categoria}`, `status`, `criadoEm`/`atualizadoEm`). Reverificação contra o espelho real (incl. o campo de referência ao Pedido em `historico`) = `operator_action`.
- `_bmad-output/implementation-artifacts/spec-5-4-migracao-do-historico-de-movimentacoes-legado.md` / `spec-3-7-...md` / `spec-2-3-...md` — **read-only, molde direto**: frontmatter `status: awaiting-operator` + `operator_actions`; relatório efêmero de falha branda vs. abortar; estrutura de teste e de `Design Notes`.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000028_add_pedido_to_movimentacoes.up.sql` (+ `.down.sql`) — coluna anulável `movimentacoes.pedido_id` (FK sem CASCADE) + `idx_movimentacoes_pedido_id`; sustenta o vínculo Movimentação↔Pedido sem tocar Stories 5.3/7.5.
- `backend/cmd/migrate-legado/pedidos.go` — `migrarPedidos`: pré-condição de seed; leitura de `legado.pedidos` + itens (normalização de `estoque` Postgres-side); carga dos mapas (`migracao_id_map` produto, `estoques.nome_normalizado`→id); resolução por Pedido com falha-por-Pedido; soma-e-descarta na colisão de item; transação única do lote; `INSERT INTO pedidos` (com `criado_em`/`decidido_*` explícitos) + `INSERT INTO pedido_itens` (com `quantidade_aprovada` por status) + `INSERT INTO migracao_id_map (entidade='pedido')`; `UPDATE movimentacoes SET pedido_id` pelas linhas de `legado.historico.pedido`; dry-run só conta.
- `backend/cmd/migrate-legado/main.go` — wiring de `migrarPedidos` após `migrarMovimentacoes`; ramos de stderr (seed ausente, `legado.pedidos` ilegível, `PendentesRevisao`, `AvisosData`); doc do pacote.
- `backend/cmd/migrate-legado/main_test.go` — fixture `legado.pedidos` + coluna `legado.historico.pedido` em `testDB`; limpeza de `legado.pedidos`/`pedido_itens`/`pedidos` em `limparTabelas` (ordem de FK; nunca `usuarios`).
- `backend/cmd/migrate-legado/pedidos_test.go` — `TestMigrarPedidos_*` + helpers, cobrindo toda a I/O & Edge-Case Matrix.

**Acceptance Criteria:**
- Given `legado.pedidos` com Pedidos resolvíveis, when `migrate-legado --executar` roda após `migrarMovimentacoes` na mesma execução, then cada Pedido vira uma linha `pedidos` com seus itens em `pedido_itens`, referenciando os novos `produto_id` (via `migracao_id_map` `entidade='produto'`) e `estoque_id` (via `estoques.nome_normalizado`), com `status` legado preservado e `criado_em` = `criadoEm` legado quando presente.
- Given um Pedido legado aprovado cujas Movimentações no `legado.historico` referenciam esse Pedido, when ambos são migrados (Movimentações pela 5.4, Pedido por esta story na mesma execução), then `movimentacoes.pedido_id` dessas linhas aponta para o id novo do Pedido migrado.
- Given a migração já aplicada, when `migrate-legado` roda de novo, then nenhuma linha nova em `pedidos`/`pedido_itens`/`migracao_id_map` (`entidade='pedido'`) e nenhum `pedido_id` alterado — idempotência pelo PK `(entidade, id_legado)`.
- Given qualquer execução do binário, when ela ocorre, then é sempre um comando disparado por uma pessoa (flag `--executar`), sem rota HTTP, cron ou chamada do runtime — o corte real em produção fica registrado em `operator_actions` com `status: awaiting-operator`.
- Given a migration `000028` (ou `000022`) não aplicada no alvo, when `migrarPedidos` roda, then ela aborta antes de qualquer escrita (`000022` → seed ausente; `000028` → `INSERT`/`UPDATE` falha e a transação única reverte), sem estado parcial.

## Spec Change Log

_Vazio — nenhum loopback de `bad_spec`._

## Review Triage Log

## Design Notes

- **`awaiting-operator`, não `done`.** Igual a 2.3/3.7/5.4: o corte real contra o espelho da empresa é humano-disparado (AD-15, PRD §9). Código, migration e testes ficam prontos e verificados contra Postgres real; o resto está em `operator_actions`. O run finaliza a frontmatter para `status: awaiting-operator`.
- **Vínculo Movimentação↔Pedido via campo de referência em `legado.historico`.** O addendum §F documenta `historico` (sem campo de Pedido) e `pedidos` (sem lista de movimentações) — nenhum lado tem, no papel, a referência cruzada. Mas a AC #2 pressupõe um vínculo descobrível ("um Pedido legado aprovado que gerou uma Movimentação no histórico legado … o vínculo … é preservado"), e o próprio §F está marcado como "precisa de nova verificação antes da implementação das stories de migração". Esta story assume, então, que a linha de `historico` gerada por uma aprovação carrega o id do Pedido legado num campo `pedido` — exatamente o mesmo tipo de suposição que a 5.4 fez sobre a estrutura de `historico`. A fixture de teste cria `legado.historico.pedido`; a confirmação/ajuste do nome real do campo (ou a constatação de que o protótipo não guardou essa referência, caso em que o vínculo pré-corte simplesmente não existe) é `operator_action`. `migrarMovimentacoes` (5.4) **não muda** — ignora a coluna nova.
- **Coluna `movimentacoes.pedido_id` anulável, só-migração por ora.** O ERD herdado (addendum §A: "MOVIMENTACOES (origem obrigatória + destino nullable + **pedido nullable**)") já previa essa coluna; a migration 000021 não a criou porque nenhuma story de runtime até a 7.5 precisou dela. Adicioná-la agora (anulável, sem CASCADE — Movimentação é auditoria) dá ao corte onde gravar o vínculo sem forçar mudança em `DecidirPedido`/`ListarMovimentacoes`/recibo. Que só a migração escreva a coluna é consistente com `movimentacoes.criado_em` explícito e com `migracao_id_map`, ambos já só-migração (migration 000021: "schema pensado para não exigir migration de alteração depois"). Um follow-up pode ligar o runtime quando/se houver AC para isso.
- **Falha é por Pedido, não por item (diferente da 5.4).** A 5.4 pula linhas individuais de `historico` porque cada linha é uma Movimentação autônoma. Aqui um Pedido sem um de seus itens misrepresentaria o que foi solicitado — a AC diz "cada Pedido é recriado **com seus itens**". Então qualquer item irresolúvel joga o Pedido inteiro para `PendentesRevisao`. Só erros inesperados de banco abortam o lote.
- **`decidido_por` = usuário sintético para Pedido migrado decidido.** `MontarReciboPedidoConteudo` (Story 7.6) faz `JOIN usuarios ON u.id = p.decidido_por` (INNER) — um Pedido `aprovado` migrado com `decidido_por` NULL quebraria o endpoint de recibo. Atribuir o usuário sintético "Migração do sistema legado" (mesma proveniência honesta usada pela 5.4 para o autor de Movimentação) mantém o recibo funcional e não mente sobre quem decidiu. `decidido_em` = `atualizadoEm` legado (o timestamp mais próximo da decisão que o legado oferece).
- **`quantidade_aprovada` por status.** Story 7.5: depois de decidido, `quantidade_aprovada` nunca volta a ser NULL; o caminho de rejeição zera todos os itens. Migrado: `aprovado` → `quantidade` (o legado não tinha aprovação parcial), `rejeitado` → `0`, `pendente` → `NULL`.
- **`unidade` do item não migra.** `pedido_itens` não tem essa coluna e nenhuma story pediu (precedente 3.7 com `lateral`, 5.4 com `obs`/`unidade`). O snapshot-alvo é `produto_nome`/`categoria_nome`/`estoque_nome`/`quantidade`.
- **Transação única do lote (molde `migrarEstoques`/`migrarMovimentacoes`).** Sem efeito colateral pós-commit (script fora do runtime, nenhum SSE no canal `pedidos`). Pré-checagens antes de `alvo.Begin()`. "Nada escrito se falhar" fica trivial; a reexecução é idempotente via `migracao_id_map` e retoma.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída de `gofmt`; build/vet limpos.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@127.0.0.1:5432/stockflow?sslmode=disable go test -p 1 -count=1 ./cmd/migrate-legado/` — Postgres real; `TestMigrarPedidos_*` novos + `TestMain_Processo` cobrindo pendência sem abortar e seed ausente; sem regressão em `TestMigrarEstoques_*`/`TestMigrarProdutos_*`/`TestMigrarMovimentacoes_*`.
- `cd backend && DATABASE_URL=... go test -p 1 -count=1 ./services/... ./handlers/...` — PASS; `movimentacoes.pedido_id` anulável não perturba `ListarMovimentacoes` (5.3), `GET /api/pedidos*` (7.3/7.4) nem o recibo (7.6).
- `cd frontend && npm run lint && npm run build` — inalterado (sem mudança de frontend); roda só para confirmar que nada quebrou.

**Manual checks (if no CLI):**
- Inspecionar `backend/migrations/000028_add_pedido_to_movimentacoes.up.sql`: `ADD COLUMN pedido_id UUID REFERENCES pedidos(id)` sem `ON DELETE CASCADE` + `CREATE INDEX idx_movimentacoes_pedido_id`.
- `go run ./cmd/migrate-legado` (dry-run, sem `--executar`) contra um Postgres local com `legado.pedidos` de teste → o relatório de estimativa não escreve nada (`SELECT count(*) FROM pedidos` igual antes/depois; nenhum `pedido_id` alterado).
