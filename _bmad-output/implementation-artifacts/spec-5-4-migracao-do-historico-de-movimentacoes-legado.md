---
title: 'Story 5.4 — Migração do Histórico de Movimentações legado'
type: 'feature'
created: '2026-09-01'
status: 'awaiting-operator'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: '138b5cc96f5a81ea304be5dd2844a581b3ef7940'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-5-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      O usuário sintético "Migração do sistema legado" (semeado pela migration
      000022, `papel='almoxarife'`, `ativo=false`) pode aparecer em superfícies
      de gestão de usuários — a listagem da Story 1.8, algum seletor de
      almoxarife, contagens de "usuários ativos" — e na futura exportação LGPD
      (Epic 8).
    evidence: |-
      A migration cria a linha em `usuarios`; a mitigação atual é `ativo=false`
      + `senha_hash=NULL` (não loga). Nenhum reviewer demonstrou uma superfície
      concretamente quebrada, e o `JOIN usuarios` interno de
      `services.ListarMovimentacoes` (Story 5.3) precisa da linha para os
      registros migrados aparecerem no Histórico. Confirmar se as telas de
      gestão de usuários filtram `ativo=false` (ou se a presença é aceitável) e
      tratar na Story 8.x quando a exportação/anonimização LGPD existir.
    location: >-
      backend/migrations/000022_seed_usuario_migracao_legado.up.sql
    severity: low
operator_actions:
  - 'Confirmar acesso ao PostgreSQL espelho do Firestore e definir `LEGADO_DATABASE_URL` apenas no ambiente de onde o corte é disparado (mesma variável das Stories 2.3/3.7) — nunca no `.env` versionado nem no `docker-compose.yml`.'
  - 'Confirmar que a tabela `historico` do espelho expõe as colunas documentadas no addendum §F (`id` textual, `produto`, `tipo`, `origem`, `destino`, `qtd`, `unidade`, `obs`, `timestamp`). Se o espelho real carregar um campo de autor (e-mail/uid/nome), estender `migrarMovimentacoes` (backend/cmd/migrate-legado/movimentacoes.go) para resolvê-lo contra `usuarios` antes do corte, no lugar do usuário sintético.'
  - 'Aplicar todas as migrations no banco alvo de produção — inclui `000022`, que semeia o usuário sintético "Migração do sistema legado".'
  - 'Rodar `migrate-legado` em dry-run (sem `--executar`), com `DATABASE_URL` no schema novo de produção e `LEGADO_DATABASE_URL` no espelho, e revisar o relatório `PendentesRevisao` (produto não migrado / não encontrado / nome ambíguo, estoque não encontrado, tipo inválido, quantidade inválida, origem igual ao destino).'
  - 'Resolver no espelho legado os registros pendentes reportados (ou aceitar conscientemente que ficarão de fora do corte) e repetir o dry-run até o relatório estar no estado esperado.'
  - 'Durante a janela de baixo uso acordada, com o plano de rollback pronto (PRD §9), rodar `migrate-legado --executar` — disparado manualmente por uma pessoa, nunca por um agente autônomo (AD-15, PRD §9).'
  - 'Após o corte, conferir que `SELECT count(*) FROM movimentacoes` e `SELECT count(*) FROM migracao_id_map WHERE entidade = ''movimentacao''` no alvo correspondem ao nº de registros de `historico` menos os pendentes reportados.'
---

<intent-contract>

## Intent

**Problem:** As Stories 5.1/5.2/5.3 entregam registrar e consultar Movimentações no schema novo, mas o histórico de baixas e transferências que já existe no sistema legado (protótipo Firestore, hoje espelhado num PostgreSQL local da empresa — addendum §F, coleção `historico`) ainda não tem caminho para a tabela `movimentacoes`. No corte "big-bang" (AD-15, PRD §9) a rastreabilidade anterior ao corte se perderia e o Histórico da Story 5.3 nasceria só com o que for registrado depois.

**Approach:** Novo `migrarMovimentacoes(alvo, legado *sql.DB, executar bool)` em `backend/cmd/migrate-legado` (Stories 2.3/3.7), chamado sequencialmente após `migrarEstoques` e `migrarProdutos` no mesmo `main()`. Lê `legado.historico`, resolve o Produto pelo nome desnormalizado via `legado.produtos` → `migracao_id_map` (`entidade='produto'`), resolve os Estoques pelo nome normalizado contra `estoques.nome_normalizado`, e recria cada registro como uma linha `movimentacoes` preservando `tipo`, quantidade e `timestamp` originais. O `historico` legado não tem campo de autor: `usuario_id` aponta para um usuário sintético "Migração do sistema legado" semeado por uma migration nova (`000022`). Idempotência pelo PK `(entidade, id_legado)` de `migracao_id_map` (`entidade='movimentacao'`). Todo registro que não resolve (produto não migrado etc.) vai para um relatório `PendentesRevisao` e é pulado, sem interromper o lote. O corte real em produção é humano-disparado → `status: awaiting-operator`.

## Boundaries & Constraints

**Always:**
- `migrarMovimentacoes(alvo, legado *sql.DB, executar bool) (ResultadoMigracaoMovimentacoes, error)` — molde estrutural de `migrarEstoques` (`main.go:200`): pré-checagens fora de transação → **uma única transação para todo o lote** → `migracao_id_map`. Registro já em `migracao_id_map` (`entidade='movimentacao'`, `id_legado = historico.id`) → `JaMigrados++`, pula. `!executar` (dry-run) só conta `Migrados`/`JaMigrados`/`PendentesRevisao` via `SELECT`, sem escrever.
- **Pré-condição de seed:** antes de ler qualquer linha legada, resolve o id do usuário sintético por `SELECT id FROM usuarios WHERE lower(email) = lower('migracao-legado@sistema.stockflow.local')`. Ausente (migration `000022` não aplicada) → `error` não-nil, nada escrito.
- **Resolução de Produto:** `historico.produto` é o nome desnormalizado. Caminho obrigatório pela tabela de mapeamento (AC): `btrim(historico.produto)` casa `btrim(legado.produtos.nome)` (match exato, sem case-fold — mesma decisão de `codigo` na 3.7) → `legado.produtos.id` → `migracao_id_map(entidade='produto', id_legado)` → `produto_id` novo. `0` matches em `legado.produtos` → pendência "produto não encontrado no legado"; `>1` match → pendência "nome de produto ambíguo no legado"; casou `1` mas o id não está no mapa → pendência "produto não migrado" (AC #2).
- **Resolução de Estoque:** mesma semântica de normalização de `normExpr` (`main.go:45` — `lower` + colapso de `\s+`) aplicada a `historico.origem`/`historico.destino`, casada contra `estoques.nome_normalizado` no alvo (populada pela 2.3). Nome sem match → pendência "estoque '<nome>' não encontrado". Numa `baixa`, `destino` igual a `'—'`, vazio ou nulo → `estoque_destino_id = NULL` (não é pendência). Numa `transferencia`, origem e destino têm de resolver.
- **`tipo`:** só `'baixa'` e `'transferencia'` do legado são aceitos e vão verbatim para `movimentacoes.tipo`. Qualquer outro valor → pendência "tipo inválido: '<v>'". (`'ajuste'` é reservado do schema, nenhum registro legado o usa.)
- **`quantidade`:** lê `historico.qtd` como texto e valida com `strconv.ParseFloat` (molde da quantidade não-numérica em `migrarProdutos`); valor nulo, não-numérico ou `<= 0` → pendência "quantidade inválida: '<v>'" (o schema tem `CHECK (quantidade > 0)`).
- **`transferencia` com origem = destino** após resolução → pendência "origem igual ao destino" (dado corrompido; o modelo-alvo não impede, mas não se importa lixo).
- **`criado_em`:** `INSERT INTO movimentacoes (...)` **inclui explicitamente `criado_em`** com `historico.timestamp` (o runtime nunca seta essa coluna — usa `DEFAULT now()` —; aqui é preservação). `timestamp` nulo/ausente → `criado_em = now()` (momento do corte), contabilizado em `AvisosData` no resumo; não é pendência (paralelo a "autor quando disponível").
- **`usuario_id`:** sempre o id do usuário sintético resolvido na pré-condição de seed, para todos os registros migrados.
- **`obs` e `unidade` do `historico` não migram** — o modelo-alvo `movimentacoes` não tem essas colunas e nenhuma story pediu (precedente 3.7: `lateral`).
- **Relatório `PendentesRevisao`:** lista efêmera (stderr no fim da execução, molde de `FotosComFalha` da 3.7), cada item com `id_legado`, `produto`, `tipo`, `origem`, `destino`, `qtd`, `timestamp` e `motivo`. Recomputada a cada execução — nenhuma tabela nova de pendências, nenhuma migration de schema além do seed do usuário.
- **Migration nova `backend/migrations/000022_seed_usuario_migracao_legado.{up,down}.sql`:** único artefato de schema. `up`: `INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo) VALUES ('Migração do sistema legado', 'migracao-legado@sistema.stockflow.local', NULL, 'almoxarife', false, false) ON CONFLICT (lower(email)) DO NOTHING`. `down`: `DELETE FROM usuarios WHERE lower(email) = lower('migracao-legado@sistema.stockflow.local')` (falha se já houver `movimentacoes` apontando — comportamento esperado de um down).
- **`main()` (`main.go:81`)** chama `migrarMovimentacoes` **depois** de `migrarProdutos` na mesma execução; um erro anterior já saiu com `os.Exit(1)` antes de chegar aqui. Ramos de stderr novos: seed do usuário ausente; `historico` ilegível; relatório `PendentesRevisao` no fim (não é erro).
- **Testes (`movimentacoes_test.go`, pacote `main`):** `testDB` ganha a fixture `legado.historico` (`id text primary key, produto text, tipo text, origem text, destino text, qtd text, unidade text, obs text, "timestamp" timestamptz`); `limparTabelas` ganha `DELETE FROM movimentacoes` (antes de `produtos`/`estoques`) e `DELETE FROM legado.historico`. `usuarios` NUNCA é limpa (o usuário sintético é seed compartilhado, igual a `categorias`/`nomenclatura_templates`). Cobrir toda a I/O & Edge-Case Matrix.

**Block If:**
- _(nenhuma decisão que exija humano em tempo de implementação — o corte real de produção é `operator_action`, não `blocked`)_

**Never:**
- Não tornar `movimentacoes.usuario_id` anulável nem alterar `services.ListarMovimentacoes` (Story 5.3) — o usuário sintético existe justamente para manter o `JOIN usuarios` interno da 5.3 intacto.
- Não criar rota HTTP, cron ou chamada do runtime para a migração (AD-15) — só o binário `cmd/migrate-legado` com a flag `--executar`.
- Não rodar o corte contra o espelho Postgres real da empresa — `operator_action` (AD-15, PRD §9), mesma restrição das Stories 2.3/3.7.
- Não abortar o lote por registro irresolúvel — só erros inesperados (conexão, `historico` ilegível, seed ausente, backstop SQL) abortam; o resto vira pendência.
- Não migrar `obs`/`unidade`, nem criar coluna nova em `movimentacoes` para acomodá-los.
- Não migrar Pedidos nem o vínculo Movimentação↔Pedido — Story 7.7.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Corte inicial | `legado.historico` com N linhas resolvíveis (produto migrado, estoques conhecidos, `tipo`/`qtd`/`timestamp` válidos), `--executar` | N linhas em `movimentacoes` (`produto_id`/origem/destino resolvidos, `criado_em` = `timestamp` legado, `usuario_id` = usuário sintético); N linhas `entidade='movimentacao'` em `migracao_id_map`; resumo `Migrados=N` | — |
| Reexecução (idempotência) | Mesma entrada, corte já rodado, `--executar` | Nenhuma linha nova em `movimentacoes` nem em `migracao_id_map`; resumo `JaMigrados=N` | linha já no mapa → pula |
| Baixa | Linha `tipo='baixa'`, `destino='—'` | `movimentacoes` com `estoque_destino_id` NULL, `estoque_origem_id` resolvido, `tipo='baixa'` | — |
| Transferência | Linha `tipo='transferencia'`, origem e destino conhecidos | `movimentacoes` com os dois lados preenchidos, `tipo='transferencia'` | — |
| Produto não migrado | `historico.produto` casa 1 linha em `legado.produtos`, mas o id não está em `migracao_id_map` | Registro NÃO inserido; entra em `PendentesRevisao` (motivo "produto não migrado"); demais registros do lote migram | não aborta |
| Produto não encontrado no legado | `historico.produto` não casa nenhuma linha de `legado.produtos` | `PendentesRevisao` ("produto não encontrado no legado"); não interrompe | não aborta |
| Nome de produto ambíguo | `historico.produto` casa >1 linha em `legado.produtos` | `PendentesRevisao` ("nome de produto ambíguo no legado"); não interrompe | não aborta |
| Estoque não encontrado | `origem` (ou `destino` fora de baixa) sem match em `estoques.nome_normalizado` | `PendentesRevisao` ("estoque '<nome>' não encontrado"); não interrompe | não aborta |
| Tipo inválido | `historico.tipo` ∉ {`baixa`, `transferencia`} | `PendentesRevisao` ("tipo inválido: '<v>'"); não interrompe | não aborta |
| Quantidade inválida | `historico.qtd` nulo, não-numérico ou `<= 0` | `PendentesRevisao` ("quantidade inválida: '<v>'"); não interrompe | não aborta |
| Transferência origem = destino | `tipo='transferencia'`, origem e destino resolvem para o mesmo `estoque_id` | `PendentesRevisao` ("origem igual ao destino"); não interrompe | não aborta |
| Timestamp ausente | `historico.timestamp` NULL, linha resolvível | Linha migrada com `criado_em = now()`; contabilizada em `AvisosData` no resumo | não é pendência |
| Seed do usuário ausente | Nenhuma linha `usuarios` com o e-mail sentinela (migration `000022` não aplicada) | `error` não-nil antes de qualquer escrita; `os.Exit(1)`; nada escrito | pré-condição, antes do loop |
| `historico` ilegível | `SELECT ... FROM legado.historico` falha (tabela ausente / estrutura divergente) | `error` não-nil; `os.Exit(1)`; nada escrito | erro propagado |
| Dry-run | Entrada válida, sem `--executar` | stdout relata `migraria N, já migrados J, pendências P`; `SELECT count(*) FROM movimentacoes` inalterado | — |
| Falha inesperada no INSERT | `INSERT INTO movimentacoes` falha no meio do lote (ex. backstop) | `tx.Rollback()` via defer; nada escrito (transação única do lote); `error` cita `id_legado`; `os.Exit(1)` | rollback, nada parcial |

</intent-contract>

## Code Map

- `backend/migrations/000022_seed_usuario_migracao_legado.up.sql` (+ `.down.sql`) — **NOVO**, único artefato de schema. Semeia o usuário sintético autor das Movimentações migradas. Molde: `000010_create_categorias.up.sql` / `000013_create_nomenclatura_templates.up.sql` (seed por migration). `INSERT ... ON CONFLICT (lower(email)) DO NOTHING` (o índice único é `idx_usuarios_email_lower` sobre `lower(email)`, migration `000001` — `ON CONFLICT` precisa da expressão). `papel='almoxarife'`, `ativo=false`, `senha_hash=NULL`.
- `backend/cmd/migrate-legado/movimentacoes.go` — **NOVO**. `migrarMovimentacoes`, `migrarUmaMovimentacao` (ou resolução inline), structs de relatório (`ResultadoMigracaoMovimentacoes` com `Migrados`/`JaMigrados`/`PendentesRevisao []PendenteRevisao`/`AvisosData []string`; `PendenteRevisao` com `IDLegado`/`Produto`/`Tipo`/`Origem`/`Destino`/`Qtd`/`Timestamp`/`Motivo`). Reusa `pqUniqueViolation` (`main.go:38`) como backstop e a semântica de `normExpr` (`main.go:45`).
- `backend/cmd/migrate-legado/main.go` — `main()` (`main.go:81`): chamada sequencial a `migrarMovimentacoes` após `migrarProdutos` (após o bloco de `resProdutos`, ~linha 200); ramos de stderr para "seed do usuário ausente", "historico ilegível" e impressão de `PendentesRevisao`/`AvisosData` no fim; `os.Exit(1)` nos caminhos de erro. Doc do pacote (topo, ~linhas 1-16): 1 linha citando Movimentações.
- `backend/cmd/migrate-legado/main_test.go` — `testDB` (`main_test.go:38`): criar `legado.historico` (`id text primary key, produto text, tipo text, origem text, destino text, qtd text, unidade text, obs text, "timestamp" timestamptz` — `timestamp` é palavra reservada, precisa de aspas, igual a `"lateral"` em `legado.produtos`). `limparTabelas` (`main_test.go:128`): acrescentar `DELETE FROM movimentacoes` (ANTES de `produtos`/`estoques` — FK sem cascade, migration `000021`) e `DELETE FROM legado.historico`. NUNCA `DELETE FROM usuarios`.
- `backend/cmd/migrate-legado/movimentacoes_test.go` — **NOVO**. `TestMigrarMovimentacoes_*` (molde de `TestMigrarProdutos_*`). Helpers: `inserirLegadoHistorico(t, alvo, input)`, e um seeder que cria Produto no alvo + linha em `migracao_id_map` (`entidade='produto'`) + linha em `legado.produtos` com o mesmo `nome` (molde de `main_produtos_test.go:322-330` e `:101`), mais `seedEstoqueAlvo` (`INSERT INTO estoques (nome) RETURNING id`). Cobre toda a I/O & Edge-Case Matrix.
- `backend/migrations/000021_create_movimentacoes.up.sql` — **read-only, referência de schema**: `tipo` CHECK IN (`baixa`,`transferencia`,`ajuste`); `estoque_origem_id`/`estoque_destino_id` NULLABLE; `quantidade NUMERIC(10,3) CHECK (quantidade > 0)`; `usuario_id UUID NOT NULL REFERENCES usuarios(id)`; `criado_em ... DEFAULT now()` (a migração sobrescreve com o `timestamp` legado). Nenhuma FK com `ON DELETE CASCADE`.
- `backend/migrations/000009_create_migracao_id_map.up.sql` — **read-only**: mapa compartilhado; `entidade='movimentacao'` já está no vocabulário documentado; PK `(entidade, id_legado)` = idempotência; UNIQUE `(entidade, id_novo)`. Nenhuma alteração de schema aqui.
- `backend/cmd/migrate-legado/main.go:200` `migrarEstoques` — **read-only, molde estrutural direto**: transação única para o lote, `defer tx.Rollback()`, `SELECT id_novo FROM migracao_id_map WHERE entidade=... AND id_legado=$1` para idempotência, backstop `23505`.
- `backend/cmd/migrate-legado/produtos.go` — **read-only, molde**: `migrarProdutos` (transação por linha — NÃO seguir aqui, ver Design Notes), leitura de `qtd` como texto + `strconv.ParseFloat` para quantidade não-numérica, structs de relatório de problema (`FotoFalha` etc.).
- `backend/services/movimentacoes.go:71` `ListarMovimentacoes` — **read-only**: `JOIN usuarios u ON u.id = m.usuario_id` (interno) — o usuário sintético mantém os registros migrados visíveis no Histórico da Story 5.3. NÃO alterar este arquivo.
- `backend/services/movimentacoes.go:216` / `:363` — **read-only, referência**: `INSERT INTO movimentacoes` do runtime NÃO seta `criado_em`; a migração seta.
- `backend/migrations/000001_create_usuarios.up.sql` — **read-only, referência de schema**: colunas de `usuarios`, `papel` CHECK, `idx_usuarios_email_lower` (unique sobre `lower(email)`), `idx_usuarios_unico_adm` (só um `adm` — por isso o usuário sintético é `almoxarife`).
- `_bmad-output/planning-artifacts/prds/prd-stockflow-2026-08-29/addendum.md` §F "Coleção `historico`" — **read-only**: estrutura legada assumida (`produto` nome desnorm, `tipo` baixa|transferencia, `origem`/`destino` string com `destino:'—'` para baixa, `qtd`, `unidade`, `obs`, `timestamp`). **Sem campo de autor** → usuário sintético. Reverificação contra o espelho real = `operator_action`.
- `_bmad-output/implementation-artifacts/spec-3-7-migracao-de-produtos-categorias-e-fotos-legadas.md` / `spec-2-3-migracao-dos-estoques-legados.md` — **read-only, molde direto**: frontmatter `status: awaiting-operator` + `operator_actions`; relatório efêmero de falha branda (`FotosComFalha`) vs. abortar; estrutura de teste e de `Design Notes`.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000022_seed_usuario_migracao_legado.up.sql` (+ `.down.sql`) — semeia o usuário sintético "Migração do sistema legado" (`papel='almoxarife'`, `ativo=false`, `senha_hash=NULL`, e-mail sentinela) — autor `NOT NULL` das Movimentações migradas sem tocar a Story 5.3.
- `backend/cmd/migrate-legado/movimentacoes.go` — `migrarMovimentacoes`: pré-condição de seed; leitura de `legado.historico`; carga dos mapas (`migracao_id_map` produto, `legado.produtos` nome→id, `estoques.nome_normalizado`→id); resolução por registro com as 5 classes de pendência; transação única do lote; `INSERT INTO movimentacoes` com `criado_em` explícito + `INSERT INTO migracao_id_map (entidade='movimentacao', ...)`; dry-run só conta.
- `backend/cmd/migrate-legado/main.go` — wiring de `migrarMovimentacoes` após `migrarProdutos`; ramos de stderr (seed ausente, `historico` ilegível, `PendentesRevisao`, `AvisosData`); doc do pacote.
- `backend/cmd/migrate-legado/main_test.go` — fixture `legado.historico` em `testDB`; limpeza de `movimentacoes` e `legado.historico` em `limparTabelas` (nunca `usuarios`).
- `backend/cmd/migrate-legado/movimentacoes_test.go` — `TestMigrarMovimentacoes_*` + helpers, cobrindo toda a I/O & Edge-Case Matrix.

**Acceptance Criteria:**
- Given `legado.historico` com registros resolvíveis, when `migrate-legado --executar` roda após `migrarEstoques`/`migrarProdutos` na mesma execução, then cada registro vira uma linha `movimentacoes` vinculada ao `produto_id` novo (resolvido por `legado.produtos.nome` → `migracao_id_map` `entidade='produto'`), com `tipo`, origem/destino e `quantidade` do legado, `criado_em` = `timestamp` legado quando presente, e `usuario_id` apontando para o usuário sintético "Migração do sistema legado".
- Given um registro do `historico` que referencia um Produto ausente de `migracao_id_map`, when a migração processa o lote, then esse registro é listado em `PendentesRevisao` com motivo e dados brutos e NÃO é inserido, enquanto todos os demais registros resolvíveis são migrados.
- Given a migração já aplicada, when `migrate-legado` roda de novo, then nenhuma linha nova em `movimentacoes` nem em `migracao_id_map` (`entidade='movimentacao'`) — idempotência pelo PK `(entidade, id_legado)`.
- Given qualquer execução do binário, when ela ocorre, then é sempre um comando disparado por uma pessoa (flag `--executar`), sem rota HTTP, cron ou chamada do runtime — o corte real em produção fica registrado em `operator_actions` com `status: awaiting-operator`.
- Given a migration `000022` não aplicada no alvo, when `migrarMovimentacoes` roda, then ela aborta antes de qualquer escrita, com mensagem indicando que o seed do usuário de migração está ausente.

## Spec Change Log

_Vazio — nenhum loopback de `bad_spec`._

## Review Triage Log

### 2026-09-01 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 4: (high 0, medium 2, low 2)
- defer: 1: (high 0, medium 0, low 1)
- reject: 13
- addressed_findings:
  - `[medium]` `[patch]` `resolver` validava `qtd` só com `strconv.ParseFloat` + `q <= 0` — `"NaN"`/`"Inf"`/`"Infinity"` passam com erro nil e `q <= 0` é falso, migrando um `quantidade` não-finito para a trilha (Postgres: `NaN > 0` é true) ou abortando o lote inteiro em Postgres antigo; um valor bem-formado fora do alcance de `NUMERIC(10,3)` (`< 0.001` arredonda para 0 → CHECK; `> 9999999.999` → overflow) também abortava o lote todo por uma linha. Guarda estendida com `math.IsNaN`/`math.IsInf`/faixa → tudo vira pendência "quantidade inválida"; `TestMigrarMovimentacoes_QuantidadeInvalida` ganhou os casos `NaN`/`Inf`/`Infinity`/`0.0004`/`99999999999`.
  - `[medium]` `[patch]` A pré-condição do usuário sintético só era checada dentro de `migrarMovimentacoes`, depois de `migrarEstoques`/`migrarProdutos` já terem commitado — e `main()` imprimia "Nada foi escrito" (falso). Adicionada checagem fast-fail no topo de `main()` (após os `Ping`, antes de `migrarEstoques`) com mensagem correta + `os.Exit(1)`; a checagem interna fica como defesa em profundidade. Novo subteste de `TestMain_Processo` (dropa o usuário, roda o filho com `--executar`, afirma exit≠0, "migration 000022" no stderr, `count(estoques)=0`/`count(movimentacoes)=0`).
  - `[low]` `[patch]` `AvisosData` (linhas cujo `timestamp` legado é nulo → `criado_em = now()`) só era preenchido no ramo `executar==true` — o dry-run obrigatório do operador não mostrava essas linhas. Dry-run passou a preencher `AvisosData` no mesmo ponto; novo `TestMigrarMovimentacoes_DryRunAvisaTimestampAusente`.
  - `[low]` `[patch]` Uma linha com `origem`/`destino` ausente (nula/vazia) gerava a pendência "estoque '' não encontrado" — enganosa. `resolver` agora devolve "origem ausente no historico" / "destino ausente no historico" para o caso vazio, reservando "estoque '<nome>' não encontrado" para nome não-vazio sem match; novos `TestMigrarMovimentacoes_OrigemAusente` e `_DestinoAusenteEmTransferencia`.

Notas de triagem (rejeitados, não repetidos acima):
- **reject** — separador decimal vírgula / espaços em `qtd` não tratados: `qtd` é `number` no addendum §F (o espelho Postgres reflete a mesma estrutura → coluna numérica, forma canônica com ponto); `migrarProdutos` faz o mesmo `strconv.ParseFloat` sem tratar vírgula e passou pela revisão da 3.7.
- **reject** — expressão de normalização inline em vez de reusar a `const normExpr`: `normExpr` é fixa na coluna `nome` e não pode ser reusada literalmente para `origem`/`destino`; a expressão inline é idêntica em semântica, comentada e coberta por teste (`_TransferenciaOrigemIgualDestino` exercita a normalização); mesmo trade-off de duplicação documentada aceito na 3.7.
- **reject** — `tipo` casado sem `btrim`/`lower` (diferente de `produto`): a spec diz "verbatim, só `baixa` e `transferencia`", e o desfecho de um `"Baixa "` é uma pendência segura (nem aborta nem grava), exatamente a linha "Tipo inválido" da I/O Matrix.
- **reject** — N+1 no `SELECT` de `migracao_id_map` (`entidade='movimentacao'`) por linha: molde estrutural direto de `migrarEstoques` (Code Map), e o achado idêntico foi rejeitado na revisão da 3.7 (sem consequência funcional na escala desta ferramenta).
- **reject** — `down.sql` falha por FK quando `movimentacoes` já referenciam o usuário: comportamento padrão de um down sobre pai referenciado (o mesmo vale para `000010`/`000013`), documentado no comentário; CI não roda migrations down neste projeto (`migrateOnce` só faz `m.Up()`).
- **reject** — contagens de `res` não zeradas nos caminhos de erro: `main()` não as imprime no ramo de erro de Movimentações (só o erro + `os.Exit(1)`), sem consequência observável; espelha `migrarEstoques`/`migrarProdutos`.
- **reject** — `historico.id` não-único não guardado: doc id do Firestore é único por construção (o espelho reflete a estrutura), e as fixtures `legado.*` das Stories 2.3/3.7 assumem o mesmo `id text primary key` sem guarda.
- **reject** — mapa `estoqueIDPorNorm` sobrescreve em colisão de `nome_normalizado`: `idx_estoques_nome_normalizado` (migration 000008) é único — colisão é impossível pelo schema.
- **reject** — `produto_id` do mapa apontando para `produtos` já excluído → FK abortaria o lote: inalcançável no corte big-bang (estoques→produtos→movimentações rodam numa execução, segundos entre si) e o app nunca faz hard-delete de Produto (migration 000011 sem `deleted_at`; nota `deferred` da 5.3 confirma).
- **reject** — `000022` poderia falhar se `usuarios` ganhar outra coluna `NOT NULL` sem default: especulativo; o `INSERT` é válido contra o schema atual e a migration foi verificada aplicando limpo.
- **reject** — folding de acento na comparação de nome de Estoque: mesmo padrão aceito desde a 2.3/3.7 (`normExpr` folda só caixa/espaço), especulativo (`historico.origem`/`destino` e `estoques.nome` vêm da mesma fonte legada), e o desfecho é uma pendência segura.
- **reject** — ramo do backstop `23505` no `INSERT` de `migracao_id_map` sem teste: só alcançável por um inseridor concorrente correndo com o `SELECT` de idempotência na mesma transação — impossível no design single-run humano-disparado; o ramo espelha `migrarEstoques`.
- **reject** — `PendenteRevisao.Timestamp` (formato RFC3339 / vazio quando nulo) sem asserção dedicada: campo só-display do relatório efêmero; `_ProdutoNaoMigrado` já afirma `.Produto`/`.Tipo`/`.Qtd` do mesmo item.

## Design Notes

- **Autor: usuário sintético, não NULL nem o operador.** O `historico` legado (addendum §F) não tem campo de autor, e `movimentacoes.usuario_id` é `NOT NULL` com `JOIN usuarios` interno em `services.ListarMovimentacoes` (Story 5.3). Um `usuario_id` NULL exigiria tornar a coluna anulável **e** reescrever o JOIN da 5.3 (LEFT JOIN + COALESCE) — código já revisado, e a própria spec-5-3 anotou esse JOIN como `deferred`. Atribuir ao operador do corte seria falso (ele não registrou aquelas baixas). O usuário sintético "Migração do sistema legado" (`papel='almoxarife'` — Movimentação é ação de `almoxarife`+; `ativo=false` e `senha_hash=NULL` — não loga; e-mail sentinela `@sistema.stockflow.local`) preserva o invariante `NOT NULL`, mantém a 5.3 intacta e rotula a proveniência honestamente no Histórico. Seed por migration (molde de `categorias`/`templates`), nunca criado pelo script (só `seed-admin` cria usuário). É a leitura de "autor original quando disponível" (AC) quando a fonte não tem autor. Se o espelho real trouxer autor, a substituição é `operator_action`.
- **"Sem interromper a migração dos demais" (AC #2) estende-se a toda falha de resolução de linha.** A AC cita só "Produto não migrado", mas `movimentacoes` não tem caminho de escrita parcial fiel para uma linha com estoque/tipo/quantidade irresolúvel. Toda linha irresolúvel → `PendentesRevisao` + skip; só erros inesperados (conexão, `historico` ilegível, seed ausente, backstop SQL) abortam. É o oposto de `migrarProdutos`, que aborta o lote em categoria/estoque desconhecido — lá `categoria_id NOT NULL` força; aqui a AC pede que não interrompa.
- **Relatório efêmero, sem tabela de pendências.** A 3.7 só persistiu `dimensoes_pendentes_revisao` porque a Normalização (Epic 6) consome; aqui nenhuma story consome "movimentação pendente" → relatório stderr recomputado a cada execução (idempotente por construção, molde de `FotosComFalha`), operador age manualmente pela lista. Única migration de schema é o seed do usuário.
- **`obs` e `unidade` não migram.** O modelo-alvo não tem essas colunas e nenhuma story pediu (precedente 3.7 com `lateral`). A AC e o epic-context enumeram o que é recriado: produto, tipo, origem/destino, quantidade, timestamp.
- **Transação única do lote (molde `migrarEstoques`, não `migrarProdutos`).** Não há efeito colateral pós-commit — o script roda fora do runtime da aplicação, então nenhum evento SSE no canal `movimentacoes` é publicado no corte (o cliente rebusca via GET quando a app voltar). "Nada escrito se falhar" fica trivial. As pré-checagens (seed, leitura do `historico`, carga dos três mapas) rodam antes de `alvo.Begin()`.
- **Resolução de Produto por nome, obrigatoriamente via o mapa.** `historico.produto` é nome desnormalizado, não id; a AC manda passar pela "tabela de mapeamento id-antigo→id-novo". `btrim` nos dois lados, sem case-fold (consistente com a decisão de `codigo` da 3.7 — nomes podem legitimamente diferir por caixa). Três desfechos de falha distintos (`0`/`>1`/não-mapeado) dão ao operador um motivo acionável.
- **`awaiting-operator`, não `done`.** Igual a 2.3/3.7: o corte real contra o espelho da empresa é humano-disparado (AD-15, PRD §9). Código, migration e testes ficam prontos e verificados contra Postgres real; o resto está em `operator_actions`. O run finaliza a frontmatter para `status: awaiting-operator`.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída de `gofmt`; build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` — Postgres real via `DATABASE_URL`; `cmd/migrate-legado` inclui os `TestMigrarMovimentacoes_*` novos; sem regressão em `services`/`handlers` (inalterados).
- `cd frontend && npm run lint && npm run build && npm run test` — inalterado (sem mudança de frontend); roda só para confirmar que nada quebrou.

**Manual checks (if no CLI):**
- Inspecionar `backend/migrations/000022_seed_usuario_migracao_legado.up.sql`: uma linha em `usuarios` com e-mail `migracao-legado@sistema.stockflow.local`, `papel='almoxarife'`, `ativo=false`, `senha_hash` NULL; `ON CONFLICT (lower(email)) DO NOTHING`.
- `go run ./cmd/migrate-legado` (dry-run, sem `--executar`) contra um Postgres local com `legado.historico` de teste → o relatório de estimativa não escreve nada (`SELECT count(*) FROM movimentacoes` igual antes/depois).

## Auto Run Result

Status: awaiting-operator
Blocking condition: nenhuma — a migration `000022`, o `migrarMovimentacoes` e os testes estão entregues e verificados contra Postgres real. O que falta é a AC "execução sempre disparada manualmente por uma pessoa" (AD-15, PRD §9) exercida contra o espelho Postgres real da empresa, ao qual o agente não tem acesso — ação de operador fora do repositório (ver `operator_actions` na frontmatter), mesmo padrão de `spec-2-3` / `spec-3-7`.

### Resumo da mudança
Story 5.4 estende `backend/cmd/migrate-legado` (Stories 2.3/3.7) com `migrarMovimentacoes`, chamada sequencialmente depois de `migrarEstoques` e `migrarProdutos` no mesmo `main()`. Nova migration `000022_seed_usuario_migracao_legado` semeia um usuário sintético "Migração do sistema legado" (`papel='almoxarife'`, `ativo=false`, `senha_hash=NULL`, e-mail sentinela `migracao-legado@sistema.stockflow.local`) — autor `NOT NULL` de toda Movimentação migrada, já que o `historico` legado (addendum §F) não tem campo de autor e `services.ListarMovimentacoes` (Story 5.3) faz `JOIN usuarios` interno. `migrarMovimentacoes` resolve, numa pré-condição, o id desse usuário (ausente → aborta, nada escrito) — e `main()` também faz um fast-fail dessa checagem no topo, antes de qualquer escrita de Estoques/Produtos, com mensagem precisa. Lê `legado.historico`, resolve o Produto pelo nome desnormalizado via `btrim(historico.produto)` → `btrim(legado.produtos.nome)` → `legado.produtos.id` → `migracao_id_map (entidade='produto')` → `produto_id` novo, e os Estoques pela mesma normalização de `normExpr` (caixa + colapso de espaço) casada contra `estoques.nome_normalizado`. Cada linha vira uma `movimentacoes` numa **única transação para o lote** (molde de `migrarEstoques` — não há efeito colateral pós-commit, o script roda fora do runtime), com `criado_em` explícito (`COALESCE(timestamp_legado, now())`) e `usuario_id` = usuário sintético; idempotência pelo PK `(entidade, id_legado)` de `migracao_id_map` (`entidade='movimentacao'`). Toda linha irresolúvel — produto não encontrado/ambíguo/não migrado, estoque não encontrado, origem/destino ausente, `tipo` fora de {baixa, transferencia}, quantidade nula/não-numérica/não-finita/fora do alcance de `NUMERIC(10,3)`/`<= 0`, transferência com origem = destino — entra no relatório efêmero `PendentesRevisao` (stderr, molde de `FotosComFalha` da 3.7) e é pulada, sem interromper o lote; só erros inesperados (conexão, `historico` ilegível, seed ausente, backstop `23505`) abortam. `obs` e `unidade` do `historico` não migram (o modelo-alvo não tem essas colunas). Dry-run (sem `--executar`) só conta `Migrados`/`JaMigrados`/`PendentesRevisao`/`AvisosData` — inclusive os avisos de `timestamp` ausente, para o operador vetar o corte de antemão — sem escrever.

Uma revisão adversarial (Blind Hunter, Edge Case Hunter, Verification Gap, Intent Alignment) rodou sobre o diff completo; 4 findings reais triados como `patch` (2 médios, 2 baixos — ver `## Review Triage Log`), todos aplicados nesta mesma passada, 1 `defer` (baixo), 13 rejeitados (majoritariamente precedentes já aceitos nas Stories 2.3/3.7: N+1 no mapa, folding de acento, `id` legado único, FK do down, schema futuro especulativo).

### Arquivos alterados
- `backend/migrations/000022_seed_usuario_migracao_legado.up.sql` / `.down.sql` — **novo**. Seed do usuário sintético "Migração do sistema legado" (`ON CONFLICT (lower(email)) DO NOTHING`); down remove por `lower(email)` (falha se já houver `movimentacoes` apontando — comportamento esperado).
- `backend/cmd/migrate-legado/movimentacoes.go` — **novo**. `migrarMovimentacoes`, `resolver` (5 classes de pendência), `pendenteDeHistorico`, `avisoTimestampAusente`, structs `ResultadoMigracaoMovimentacoes`/`PendenteRevisao`/`linhaHistoricoLegado`/`movimentacaoResolvida`, `const emailUsuarioMigracaoLegado`, `var errSeedUsuarioMigracaoAusente`. Guarda de quantidade endurecida na revisão (`math.IsNaN`/`math.IsInf`/faixa de `NUMERIC(10,3)`); mensagens "origem/destino ausente no historico"; dry-run preenche `AvisosData`.
- `backend/cmd/migrate-legado/main.go` — wiring de `migrarMovimentacoes` após `migrarProdutos`; fast-fail da pré-condição do seed no topo de `main()` (revisão); ramos de stderr (seed ausente, `historico` ilegível, `PendentesRevisao`, `AvisosData`); doc do pacote.
- `backend/cmd/migrate-legado/main_test.go` — fixture `legado.historico` em `testDB`; `INSERT` idempotente do usuário sintético em `testDB` (outras suítes fazem `TRUNCATE usuarios CASCADE`); `DELETE FROM movimentacoes` + `DELETE FROM legado.historico` em `limparTabelas` (nunca `usuarios`); subteste de `TestMain_Processo` para pendência no stderr sem abortar e (revisão) para o fast-fail do seed `000022` ausente.
- `backend/cmd/migrate-legado/movimentacoes_test.go` — **novo**. 22 funções `TestMigrarMovimentacoes_*` cobrindo toda a I/O & Edge-Case Matrix, incluindo os casos adicionados na revisão (`QuantidadeInvalida` com `NaN`/`Inf`/`Infinity`/`0.0004`/`99999999999`, `_OrigemAusente`, `_DestinoAusenteEmTransferencia`, `_DryRunAvisaTimestampAusente`, `_FalhaInesperadaNoInsert`).
- `_bmad-output/implementation-artifacts/spec-5-4-migracao-do-historico-de-movimentacoes-legado.md` — este spec.

### Achados da revisão
- **Patches aplicados: 4** — (medium 2) guarda de quantidade não-finita/fora-de-faixa (`NaN`/`Inf` migrava lixo ou abortava o lote; valor fora do alcance de `NUMERIC(10,3)` abortava o lote por uma linha) → tudo vira pendência; pré-condição do seed checada tarde demais + mensagem "Nada foi escrito" falsa → fast-fail no topo de `main()` com mensagem precisa. (low 2) dry-run não preenchia `AvisosData` (timestamp ausente) → passou a preencher; pendência "estoque '' não encontrado" enganosa para origem/destino ausente → "origem/destino ausente no historico".
- **Itens adiados (`deferred`): 1** — exposição do usuário sintético em superfícies de gestão de usuários / LGPD (Epic 8) — baixo, mitigado por `ativo=false`, nenhuma superfície concretamente quebrada.
- **Itens rejeitados: 13** — ver `## Review Triage Log`.
- **Recomendação de revisão de acompanhamento:** `true`. Patches desta passada por severidade: high 0, medium 2, low 2. Score = 3×2 + 1×2 = 8 (≥ 5) → `followup_review_recommended: true`.

### Verificação executada
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — limpo, antes e depois dos patches (rodado de forma independente pelo orquestrador).
- `cd backend && DATABASE_URL=... go test -p 1 -count=1 ./cmd/migrate-legado/` — PASS contra Postgres real (`localhost:5432`), antes e depois dos patches. Pacote inteiro `ok`; `TestMigrarMovimentacoes_*` (22 funções, incluindo os casos novos da revisão) e `TestMain_Processo` (incl. o subteste do seed `000022` ausente) todos PASS.
- `cd backend && go test -p 1 -count=1 ./services/... ./handlers/... ./middleware/... ./iam/... ./realtime/... ./cmd/seed-admin/... .` — PASS em todos os pacotes (services 96s, handlers 102s), sem regressão (nenhum arquivo de `services`/`handlers` tocado; o seed do usuário sintético não perturba as suítes de listagem de usuário / contagem SSO).
- `cd frontend && npm run lint && npm run build` — PASS (`oxlint` sem achados; `tsc + vite` ok, só o aviso pré-existente de tamanho de chunk). Nenhum arquivo de frontend tocado por esta story.
- **Matrix Test Audit:** as 16 linhas da I/O & Edge-Case Matrix têm cada uma pelo menos um teste correspondente que rodou e passou (a linha "Falha inesperada no INSERT" ganhou `TestMigrarMovimentacoes_FalhaInesperadaNoInsert`, adicionado pelo orquestrador ao fim do step-03).

### Riscos residuais
- **Estrutura do espelho legado assumida** (addendum §F, coleção `historico`): os testes exercitam uma fixture `legado.historico` sintética. Se o espelho real divergir — sobretudo se carregar um campo de autor (e-mail/uid/nome) — `migrarMovimentacoes` precisa ser estendido para resolvê-lo contra `usuarios` no lugar do usuário sintético, antes do corte (`operator_action`).
- **`criado_em = now()` para `timestamp` legado ausente:** fiel a "data original quando disponível" (AC), mas colapsa a cronologia dessas linhas para o momento do corte. Contabilizado em `AvisosData` (agora também no dry-run) para revisão do operador; nenhuma AC observa o valor de fallback.
- **`--executar` real não rodado** — por design (AD-15/PRD §9). O caminho de escrita está coberto por testes de integração contra Postgres real (schema `legado` sintético no mesmo servidor); o corte contra o espelho real da empresa é `operator_action`.
- **Transação única do lote:** um erro inesperado no meio do `--executar` reverte tudo que foi migrado *nessa execução* (nada parcial) — a reexecução é idempotente via `migracao_id_map` e retoma. Diferente de "sem interromper os demais", que vale para linhas irresolúveis (pendência), não para falha inesperada de banco.
- `followup_review_recommended: true` — 2 patches `medium` numa passada, acima do limiar. Uma segunda passada focada de revisão é recomendada se/quando a story voltar ao loop, mesmo com a revisão adversarial já tendo rodado.
