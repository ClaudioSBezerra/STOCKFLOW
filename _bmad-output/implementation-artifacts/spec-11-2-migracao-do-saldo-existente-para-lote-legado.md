---
title: 'Story 11.2: Migração do saldo existente para Lote legado'
type: 'feature'
created: '2026-09-20'
baseline_revision: 'f32d2fdbb4f135d4e2e6c6928dcba7ae6537d3c0'
status: done
review_loop_iteration: 0
followup_review_recommended: true
context: []
warnings: [oversized]
deferred:
  - summary: >-
      O workflow de CI (`.github/workflows/deploy-cliente-aws.yml`) roda `go test ./...` sem serviço Postgres nem `DATABASE_URL`, então todos os testes de integração (inclusive os do corte de saldo) são pulados (`t.Skip`) e reportam verde.
    evidence: |-
      `grep -n "DATABASE_URL\|services:" .github/workflows/*.yml` não retorna nada; o `testDB` de `services` e dos `cmd/*` faz `t.Skip` sem `DATABASE_URL`. Pré-existente, vale para toda a suíte de integração do repositório.
    location: >-
      .github/workflows/deploy-cliente-aws.yml:27
    severity: low
operator_actions:
  - 'Confirmar que a migração multi-Empresa da Story 9.4 (`migrar-multi-empresa`) já rodou por completo em produção (todo Produto e Estoque com `empresa_id`) antes do corte de saldo.'
  - 'Fazer backup do banco de produção (`pg_dump`) imediatamente antes do corte de saldo — o corte zera `produto_estoque` e não tem rollback automático.'
  - 'Só depois de as Stories 11.3, 11.4 e 11.5 (Baixa, Transferência e Pedidos lendo/debitando `lotes`) estarem em produção, rodar `./migrar-saldo-lotes` (dry-run) no container `api` e revisar o relatório (linhas a migrar, quantidade total, problemas de Empresa) até reportar `problemas: nenhum`.'
  - 'Na janela de baixo uso acordada, rodar `./migrar-saldo-lotes --executar` — disparado manualmente por uma pessoa, nunca por um agente autônomo (AD-15, PRD §9).'
  - 'Após o corte, conferir que `SELECT COALESCE(SUM(quantidade),0) FROM produto_estoque` é 0, que `SELECT count(*) FROM lotes` cresceu no nº de linhas migradas e que o total do Catálogo é o mesmo de antes do corte.'
---

<intent-contract>

## Intent

**Problem:** Todo saldo já registrado vive em `produto_estoque` (quantidade única por par Produto/Estoque, sem Lote). A Story 11.1 criou `lotes` e a view `saldo_produto_estoque` soma os dois, mas o saldo antigo nunca vira Lote; sem migrá-lo, `lotes` não pode ser a única fonte de saldo (FR-47, AD-24).

**Approach:** Novo binário one-off `backend/cmd/migrar-saldo-lotes` (molde de `cmd/migrar-multi-empresa`, AD-15) sobre a função testável `services.MigrarSaldoParaLotes`: numa transação, cada linha de `produto_estoque` com `quantidade > 0` gera 1 Lote legado (`data_validade NULL`, mesma quantidade, `empresa_id` copiado do Produto de origem) e a linha de origem é ZERADA na mesma instrução — o zero é a marca de progresso (idempotência) e evita dupla contagem na view. Disparo manual por pessoa, dry-run por padrão (`--executar` aplica).

## Boundaries & Constraints

**Always:**
- `produto_estoque` não tem `empresa_id` (filha, posse do Produto — migration 000032): o `empresa_id` do Lote é o `produtos.empresa_id` da linha de origem (já backfilled pela 9.4), lido do banco e copiado; nunca recalculado por slug, sessão ou Estoque.
- Uma única transação, uma única instrução SQL (CTE): trava as linhas `produto_estoque` com `quantidade > 0` (`FOR UPDATE OF pe`, mesmo lock de Baixa/Transferência/Pedido → serializa com elas), `UPDATE ... SET quantidade = 0` e `INSERT INTO lotes (produto_id, estoque_id, quantidade, data_validade, empresa_id)` a partir do MESMO conjunto travado. Nunca `INSERT` de Lote sem zerar a origem. Antes do commit confere `SUM(lotes inseridos) = SUM(quantidade zerada)` e `count` igual; divergência → rollback + erro.
- Linha com `quantidade = 0` NÃO gera Lote (sem saldo a preservar; um Lote zerado bloquearia a exclusão do Estoque). `produto_estoque` e suas linhas zeradas permanecem (Baixa/Transferência/Pedidos/Cadastro ainda a usam até as Stories 11.3–11.6).
- Idempotente: 2ª execução não encontra linha `> 0` → 0 Lotes novos. Saldo que voltar a aparecer em `produto_estoque` depois (cadastro/importação ainda escrevem lá até a 11.6) é migrado por nova execução, sem duplicar o já migrado. Saldo total do Catálogo (view) idêntico antes e depois.
- Pré-checagem (dry-run e `--executar`) ANTES de escrever, em bloco: linha `> 0` cujo Produto ou Estoque tem `empresa_id IS NULL`, ou cujos `empresa_id` de Produto e Estoque diferem → aborta com relatório (`ErroSaldoNaoMigravel`), nada escrito (9.4 não rodou/incompleta).
- Dry-run (sem `--executar`) só relata: linhas a migrar, linhas zeradas puladas, soma da quantidade, Lotes existentes, problemas; não escreve nada. Rejeita argumento posicional (`flag.NArg() > 0`); exige `DATABASE_URL`.
- Não gera Movimentação (o saldo total não muda; o saldo inicial de `produto_estoque` nunca gerou). Não usa `migracao_id_map`.
- Migration `000042`: `COMMENT ON TABLE produto_estoque` marcando-a descontinuada (saldo lido de `lotes`; a tabela só é removida quando nenhum código a ler/escrever) — sem alterar dados nem a view.
- O Lote legado aparece no detalhe como Lote comum com "validade desconhecida" (já tratado pela 11.1); nenhuma mudança de frontend.

**Block If:** Nenhuma decisão bloqueante identificada.

**Never:** Não executar o corte em produção (AD-15, PRD §9: só pessoa, `awaiting-operator`); nenhuma rota HTTP, cron, entrypoint ou chamada do runtime; não dropar `produto_estoque` nem alterar a view; não tocar Baixa/Transferência/Pedidos/Carrinho/Importação/Cadastro (11.3–11.6); não criar Movimentação; não inventar `data_validade`; não tocar `sprint-status.yaml`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Corte inicial | N linhas `produto_estoque` com qtd > 0, empresas válidas, `--executar` | N Lotes (`data_validade NULL`, mesma qtd, `empresa_id` do Produto); origem zerada; view total inalterado | — |
| Reexecução | Mesmo banco, `--executar` de novo | 0 Lotes novos; `Migrados=0` | — |
| Linha zerada | `produto_estoque.quantidade = 0` | Sem Lote; linha permanece | — |
| Empresa não backfilled | Produto/Estoque com `empresa_id NULL` ou empresas distintas, qtd > 0 | Aborta com relatório; nada escrito | `ErroSaldoNaoMigravel` |
| Saldo novo pós-corte | Nova linha `> 0` em `produto_estoque` depois do 1º corte | Nova execução migra só ela | — |
| Dry-run | sem `--executar` | Relatório; banco inalterado | — |
| Duas Empresas | Linhas em Empresas distintas | Cada Lote leva o `empresa_id` do respectivo Produto | — |
| Lotes prévios (11.1) | Par já tem Lote real | Lote legado é adicional; nenhum Lote existente alterado | — |
| Sem saldo | Nenhuma linha `> 0` | `Migrados=0`, exit 0 | — |

</intent-contract>

## Code Map

- `backend/cmd/migrar-multi-empresa/main.go` (+ `main_test.go`) -- MOLDE do CLI: `godotenv`, `DATABASE_URL`, `flag.NArg()`, dry-run x `--executar`, `executarMigracao(db, out, ...)` testável, `testDB` com `file://../../migrations`.
- `backend/services/migracao_multi_empresa.go` -- molde de service de corte (diagnóstico + escrita + erro tipado). Novo `backend/services/migracao_saldo_lotes.go` (`DiagnosticarSaldoLotes`, `MigrarSaldoParaLotes`, `ErroSaldoNaoMigravel`).
- `backend/migrations/000041_create_lotes.up.sql` -- `lotes` e view `saldo_produto_estoque` (read-only); nova `000042_comment_produto_estoque_descontinuada.{up,down}.sql`.
- `backend/services/movimentacoes.go:258-282` -- Baixa trava `produto_estoque ... FOR UPDATE`: o corte usa o mesmo lock (read-only, referência).
- `backend/services/movimentacoes_test.go:18` (`seedProdutoComSaldo`), `services/lotes_test.go` (helpers `contarLotes`, `empresaAlheiaLotes`), `services/produtos_test.go:26` (`limparProdutos`) -- moldes/helpers de teste.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000042_comment_produto_estoque_descontinuada.{up,down}.sql` -- `COMMENT ON TABLE` de descontinuação (down remove) -- marca AC2 sem quebrar leitores vivos
- `backend/services/migracao_saldo_lotes.go` (+ `migracao_saldo_lotes_test.go`) -- diagnóstico + migração transacional em CTE + conferência de soma + pré-checagem de empresa; cobrir toda a matriz
- `backend/cmd/migrar-saldo-lotes/main.go` (+ `main_test.go`) -- CLI dry-run/`--executar`, cabeçalho com runbook (rodar só depois de 9.4 e com 11.4/11.5 no ar), saída em stdout, erro em stderr + exit 1

**Acceptance Criteria:**
- Given linhas de `produto_estoque` com saldo, when o operador roda `migrar-saldo-lotes --executar`, then cada uma vira um Lote `data_validade NULL` com mesma quantidade e `empresa_id` do Produto, e o saldo total do Catálogo não muda.
- Given a migração concluída, when o sistema lê saldo, then todo saldo migrado vem de `lotes` e `produto_estoque` está zerada e marcada descontinuada.
- Given a migração já executada, when roda de novo, then nenhum Lote é duplicado.
- Given o corte, when o binário é executado, then só por pessoa (sem rota/cron) e sem `--executar` só relata.

## Spec Change Log

## Review Triage Log

### 2026-09-20 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 6: (high 0, medium 3, low 3)
- defer: 1: (high 0, medium 0, low 1)
- reject: 28
- addressed_findings:
  - `[medium]` `[patch]` A pré-checagem de Empresa roda sem lock e o CTE só juntava `produtos`: uma linha que ficasse inválida entre as duas (ou Empresa divergente do Estoque) podia gerar Lote com `empresa_id` errado/NULL. `alvo` agora junta `estoques` e repete o filtro de Empresa, e a re-checagem pós-escrita (`diagnosticarSaldoLotes(tx)`) aborta o corte inteiro com `ErroSaldoNaoMigravel` se sobrar linha com saldo fora do alvo (`backend/services/migracao_saldo_lotes.go`).
  - `[medium]` `[patch]` O lock `FOR UPDATE OF pe` que serializa com Baixa/Transferência/Pedido não tinha teste: adicionado `TestMigrarSaldoParaLotes_SerializaComEscritaConcorrente` (o corte espera a escrita concorrente e migra a quantidade já debitada).
  - `[medium]` `[patch]` O runbook manda rodar o binário no container `api`, mas o `Dockerfile` não o compilava/copiava: `migrar-saldo-lotes` adicionado aos estágios build e final (`backend/Dockerfile`).
  - `[low]` `[patch]` Sem teto de espera nos locks: `SET LOCAL lock_timeout = '30s'` antes do CTE.
  - `[low]` `[patch]` Mensagens/comentário desonestos: o erro de Empresa mandava "rodar a 9.4" mesmo para Empresas divergentes, e o `COMMENT` afirmava que o saldo "é lido de `lotes`" antes do corte; reescritos. `.down.sql` documenta que não desfaz o corte.
  - `[low]` `[patch]` Cleanup do teste do CLI rodava `DELETE ... = ''` se `ProvisionarEmpresa` falhasse; guarda `empresaID == ""`.
- Rejeitados (resumo): quantidade negativa (CHECK `>= 0` em `produto_estoque` a impede); `float64` (NUMERIC(10,3) cabe exato); unicidade de `lotes` por par/validade (não existe — a 11.1 permite N Lotes); prompt de confirmação, trilha de auditoria e passo de backup no binário (fora do intent; backup entra em `operator_actions`); dry-run sem snapshot, dois diagnósticos por execução, saída do `--help`, `TRUNCATE` amplo e slug/CNPJ fixos nos testes (padrão do repositório e cosmético); ramo de divergência da conferência de soma (defensivo, inalcançável); down do `COMMENT` (tabela não tinha comentário); `AC2` como "única fonte de saldo" no nível leitura/escrita (a remoção de leituras/escritas de `produto_estoque` é das Stories 11.3–11.6 — o corte esvazia a tabela e a marca descontinuada); zero-linhas sem Lote e `empresa_id` vindo do Produto (decisões do intent-contract, produto_estoque não tem `empresa_id`).

## Design Notes

- **Zerar em vez de marcar:** `produto_estoque` não tem coluna de progresso e `migracao_id_map` (PK por par) travaria saldo que reaparece antes da 11.6. Zerar a origem na mesma instrução que cria o Lote é a marca natural, evita dupla contagem na view (`legado + lotes`) e mantém o total. Efeito colateral deliberado: Baixa/Transferência/Pedidos (ainda lendo `produto_estoque`) só enxergam saldo migrado após 11.4/11.5 — por isso o runbook manda executar o corte com o Épico 11 completo no ar.
- **`awaiting-operator`:** o código é verificável contra Postgres real; o corte no banco de produção é ato humano (AD-15/PRD §9).

## Auto Run Result

Status: awaiting-operator
Blocking condition: nenhuma — todo o código está entregue e verificado contra Postgres real; falta o corte em produção, que AD-15/PRD §9 reservam a uma pessoa (ver `operator_actions`).

**Resumo:** binário one-off `backend/cmd/migrar-saldo-lotes` (dry-run por padrão, `--executar` aplica) sobre `services.MigrarSaldoParaLotes`: numa transação e numa única instrução (CTE com `FOR UPDATE OF pe`), cada linha de `produto_estoque` com saldo `> 0` vira um Lote legado (`data_validade NULL`, mesma quantidade, `empresa_id` do Produto) e a origem é zerada — o zero é a marca de idempotência e evita dupla contagem na view; o saldo total do Catálogo não muda e nenhuma Movimentação é gerada. Pré-checagem de Empresa (nulo/divergente aborta sem escrever) e re-checagem pós-escrita. Migration `000042` marca `produto_estoque` como descontinuada (COMMENT).

**Arquivos:**
- `backend/services/migracao_saldo_lotes.go` (+ `_test.go`) — diagnóstico, migração transacional, `ErroSaldoNaoMigravel`; 10 testes (matriz completa + concorrência).
- `backend/cmd/migrar-saldo-lotes/main.go` (+ `main_test.go`) — CLI, runbook no cabeçalho; 4 testes.
- `backend/migrations/000042_comment_produto_estoque_descontinuada.{up,down}.sql` — COMMENT de descontinuação.
- `backend/Dockerfile` — compila e copia o novo binário para a imagem `api`.

**Achados:** patches 6 (medium 3, low 3); adiados 1 (CI sem Postgres, pré-existente); rejeitados 28.
**Revisão de acompanhamento recomendada:** `true` — 3×3 + 1×3 = 12 (≥ 5).

**Verificação:** `go build ./...`, `go vet ./...`, `gofmt -l .` limpos; `go test -count=1 -p 1 ./...` todos os pacotes `ok`; reexecutada após os patches: todos os pacotes `ok`. Frontend não alterado.

**Riscos residuais:** AC2 ("`lotes` única fonte de saldo") é atendida por esvaziamento + marca de descontinuação, não por remoção de leitores/escritores de `produto_estoque` (Baixa/Transferência/Pedidos/Cadastro/Importação, Stories 11.3–11.6) nem da view; o corte só deve rodar com o Épico 11 completo no ar, ou Baixa/Transferência/Pedidos não enxergarão o saldo migrado. Corte de produção não executado (por design).

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros e sem arquivos listados.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 -p 1 ./services/ ./cmd/...` -- expected: tudo passa.

## Dispensa das ações do operador (2026-09-21)

Decisão do usuário: **o corte do saldo (`./migrar-saldo-lotes`) não será executado** — o sistema ainda está em testes, em base de treinamento, sem saldo real a migrar. O código da story está entregue e em produção; as `operator_actions` (backup, dry-run, `--executar`) ficam **dispensadas**, não cumpridas.

Consequência aceita: o saldo antigo em `produto_estoque` continua sendo lido pelo Catálogo e consumido pelo FEFO como "saldo legado" (sem Lote e sem validade); saldo novo entra por Lançamento de Saldo (Lote). Nada quebra com os dois coexistindo. O binário `migrar-saldo-lotes` segue na imagem caso o corte venha a ser necessário depois (aí valem as ações originais).

## Operator Confirmation

Confirmed 2026-09-21: the external actions this story owed were carried out.

- Confirmar que a migração multi-Empresa da Story 9.4 (`migrar-multi-empresa`) já rodou por completo em produção (todo Produto e Estoque com `empresa_id`) antes do corte de saldo.
- Fazer backup do banco de produção (`pg_dump`) imediatamente antes do corte de saldo — o corte zera `produto_estoque` e não tem rollback automático.
- Só depois de as Stories 11.3, 11.4 e 11.5 (Baixa, Transferência e Pedidos lendo/debitando `lotes`) estarem em produção, rodar `./migrar-saldo-lotes` (dry-run) no container `api` e revisar o relatório (linhas a migrar, quantidade total, problemas de Empresa) até reportar `problemas: nenhum`.
- Na janela de baixo uso acordada, rodar `./migrar-saldo-lotes --executar` — disparado manualmente por uma pessoa, nunca por um agente autônomo (AD-15, PRD §9).
- Após o corte, conferir que `SELECT COALESCE(SUM(quantidade),0) FROM produto_estoque` é 0, que `SELECT count(*) FROM lotes` cresceu no nº de linhas migradas e que o total do Catálogo é o mesmo de antes do corte.

_Appended by the bmad-loop orchestrator (`bmad-loop confirm`, #335): a human confirmed these external actions out of band, and the story was advanced from `awaiting-operator` to `done`._
