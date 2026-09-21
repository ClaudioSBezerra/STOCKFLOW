---
title: 'Story 12.2: Migração dos Estoques legados para Filial padrão'
type: 'feature'
created: '2026-09-21'
baseline_revision: 'af62addf1e814dbce3c93ebcbe78325a239df38e'
status: awaiting-operator
review_loop_iteration: 0
followup_review_recommended: true
context: []
warnings: ['oversized']
deferred:
  - summary: >-
      O workflow de CI roda `go test ./...` sem serviço Postgres nem `DATABASE_URL`, então os testes de integração da migração de Estoques (como toda a suíte de integração) são pulados e reportam verde.
    evidence: |-
      `grep -n "DATABASE_URL\|services:" .github/workflows/*.yml` não retorna nada; `testDB` faz `t.Skip` sem `DATABASE_URL`. Pré-existente (já registrado na spec-11-2).
    location: >-
      .github/workflows/deploy-cliente-aws.yml:27
    severity: low
operator_actions:
  - 'Confirmar que a migração multi-Empresa da Story 9.4 (`migrar-multi-empresa`) já rodou por completo em produção (todo Estoque com `empresa_id`) e que o deploy da Story 12.1 (API com `filial_id` obrigatório no cadastro de Estoque) já está no ar.'
  - 'Fazer backup do banco de produção (`pg_dump`) imediatamente antes do corte — o binário cria Filiais, vincula Estoques e aplica `NOT NULL` em `estoques.filial_id`, sem rollback automático.'
  - 'Rodar `./migrar-estoques-filial` (dry-run) no container `api` e revisar o relatório (Empresas sem Filial, Estoques a vincular, ~11 na Ferreira Costa) até reportar `problemas: nenhum`; corrigir à mão qualquer Estoque sem `empresa_id` ou com nome duplicado apontado.'
  - 'Na janela de baixo uso acordada, rodar `./migrar-estoques-filial --executar` — disparado manualmente por uma pessoa, nunca por um agente autônomo (AD-15, PRD §9).'
  - 'Após o corte, conferir que `SELECT count(*) FROM estoques WHERE filial_id IS NULL` é 0, que `is_nullable` de `estoques.filial_id` é `NO` e que a lista de Estoques no app mostra todos vinculados à Filial padrão.'
---

<intent-contract>

## Intent

**Problem:** A Story 12.1 criou `filiais` e `estoques.filial_id` NULLABLE, mas todo Estoque já existente (~11 na Ferreira Costa) continua sem Filial, e Empresas anteriores à 12.1 podem nem ter uma Filial. Enquanto isso o sistema pode subir com Estoque órfão de Filial e o índice `(filial_id, nome_normalizado)` não protege os legados (FR-51, AD-27).

**Approach:** Novo binário one-off `backend/cmd/migrar-estoques-filial` (molde de `cmd/migrar-saldo-lotes`, AD-15) sobre `services.MigrarEstoquesParaFilial`: numa transação, cria a Filial padrão (nome = Nome Fantasia) das Empresas sem nenhuma Filial, vincula todo Estoque com `filial_id IS NULL` à Filial padrão da própria Empresa (a mais antiga) e, só então, aplica `ALTER TABLE estoques ALTER COLUMN filial_id SET NOT NULL`. Disparo manual por pessoa, dry-run por padrão (`--executar` aplica). O `SET NOT NULL` fica no binário (não numa migration SQL) para não derrubar o deploy antes do backfill.

## Boundaries & Constraints

**Always:**
- Uma única transação com `SET LOCAL lock_timeout = '30s'`: (1) pré-checagem; (2) `INSERT INTO filiais (empresa_id, nome) SELECT id, nome_fantasia FROM empresas WHERE NOT EXISTS (Filial da Empresa)`; (3) `UPDATE estoques SET filial_id = <Filial padrão da empresa_id do Estoque> WHERE filial_id IS NULL` (Filial padrão = `ORDER BY criado_em, id LIMIT 1`, a mesma de `filialPadraoDaEmpresa`); (4) confere `count(filial_id IS NULL) = 0` e `count` do UPDATE = nº de Estoques a vincular; (5) `ALTER COLUMN filial_id SET NOT NULL`; commit. Qualquer divergência → rollback + erro, nada gravado.
- Pré-checagem (dry-run e `--executar`) ANTES de escrever: Estoque com `filial_id IS NULL` e `empresa_id IS NULL` (9.4 não rodou) → `ErroEstoqueNaoMigravel` (motivo "sem empresa_id"); Estoque legado cujo `nome_normalizado` colide na Filial padrão da Empresa com outro Estoque (já vinculado a ela ou outro legado da mesma Empresa) → mesmo erro (motivo "nome duplicado na Filial padrão") em vez de estourar o índice único. Relatório lista os Estoques; nada escrito.
- Aditiva e idempotente: Estoque já com `filial_id` nunca é tocado nem reprocessado; 2ª execução → 0 Filiais criadas, 0 Estoques vinculados, `SET NOT NULL` inócuo. Empresa que já tem Filial (criada via 12.1/provisionamento) usa a mais antiga; nenhuma Filial existente é alterada. Empresa sem Estoque legado e sem Filial também ganha a Filial padrão (nenhuma Empresa fica sem Filial).
- Dry-run (sem `--executar`) só relata: Empresas sem Filial (a criar), Estoques a vincular, Estoques já vinculados, se `filial_id` já é `NOT NULL`, problemas; não escreve nada. Rejeita argumento posicional (`flag.NArg() > 0`); exige `DATABASE_URL`. Saída em stdout, erro em stderr + exit 1.
- `Dockerfile` compila e copia `migrar-estoques-filial` (runbook manda rodar no container `api`, como `migrar-saldo-lotes`). Cabeçalho do `main.go` traz o runbook: rodar só depois da 9.4 e do deploy da 12.1; backup antes.
- Não altera dados fora de `estoques.filial_id`/`filiais`; não gera Movimentação; não usa `migracao_id_map`.

**Block If:** Nenhuma decisão bloqueante identificada.

**Never:** Não executar o corte em produção (AD-15, PRD §9: só pessoa → `awaiting-operator`); nenhuma rota HTTP, cron, entrypoint ou chamada do runtime; não criar migration SQL com `SET NOT NULL` (quebraria o deploy com Estoques NULL); não reorganizar Estoques em várias Filiais nem editar/excluir Filial; não tocar Centro de Custo (12.3), fotos (12.4), UI, `sprint-status.yaml`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Corte inicial | Empresa sem Filial, N Estoques `filial_id` NULL, `--executar` | 1 Filial (nome = Nome Fantasia), N Estoques vinculados a ela, coluna `NOT NULL` | — |
| Empresa já com Filial | Empresa com Filial(is) e Estoques NULL | Estoques vão para a mais antiga; 0 Filiais criadas | — |
| Reexecução | Mesmo banco, `--executar` de novo | 0 Filiais, 0 Estoques; Estoque vinculado não muda | — |
| Estoque já vinculado | `filial_id` preenchido (outra Filial) | Não tocado | — |
| Duas Empresas | Estoques NULL em Empresas distintas | Cada um na Filial padrão da própria Empresa | — |
| Empresa não backfilled | Estoque NULL com `empresa_id` NULL | Aborta com relatório; nada escrito; coluna segue NULLABLE | `ErroEstoqueNaoMigravel` |
| Nome duplicado | Legado "Central" + Estoque "central" já na Filial padrão | Aborta com relatório; nada escrito | `ErroEstoqueNaoMigravel` |
| Dry-run | sem `--executar` | Relatório; banco inalterado (coluna e Filiais intactas) | — |
| Sem Estoque legado | Todos vinculados | `Vinculados=0`, exit 0, `NOT NULL` aplicado | — |

</intent-contract>

## Code Map

- `backend/cmd/migrar-saldo-lotes/main.go` (+ `main_test.go`) -- MOLDE do CLI (dry-run x `--executar`, `executarMigracao(db, out, executar)` testável, `mensagemDeErro`, `testDB` com `file://../../migrations`, `semear*` com cleanup).
- `backend/services/migracao_saldo_lotes.go` (+ teste) -- MOLDE do service (diagnóstico + transação + erro tipado + `lock_timeout`). Novo `backend/services/migracao_estoques_filial.go` (`DiagnosticarEstoquesFilial`, `MigrarEstoquesParaFilial`, `ErroEstoqueNaoMigravel`).
- `backend/migrations/000044_create_filiais.up.sql` -- `filiais`, `estoques.filial_id` NULLABLE, índice `(filial_id, nome_normalizado)` (read-only, referência); `services/filiais.go:filialPadraoDaEmpresa` -- regra "Filial padrão = mais antiga".
- `backend/services/empresas.go:433` -- provisionamento insere Filial com `NomeFantasia` (mesmo nome usado no backfill: `empresas.nome_fantasia`).
- `backend/Dockerfile:13-26` -- acrescentar build/copy do novo binário.
- Testes: como o teste roda `SET NOT NULL` no banco COMPARTILHADO, o cleanup deve restaurar `ALTER COLUMN filial_id DROP NOT NULL` (os demais testes inserem Estoque legado sem Filial). `services/filial_teste_test.go`, `services/filiais_test.go:284` (`empresaSemFilialDeTeste`) e `limparProdutos` são helpers reutilizáveis.

## Tasks & Acceptance

**Execution:**
- `backend/services/migracao_estoques_filial.go` (+ `migracao_estoques_filial_test.go`) -- diagnóstico, migração transacional, pré-checagem, conferência, `SET NOT NULL`; cobrir toda a matriz; cleanup restaura nullability -- núcleo testável
- `backend/cmd/migrar-estoques-filial/main.go` (+ `main_test.go`) -- CLI dry-run/`--executar` com runbook no cabeçalho; testes de dry-run, execução + reexecução, abort -- disparo manual
- `backend/Dockerfile` -- build e copy do binário -- operador roda no container `api`

**Acceptance Criteria:**
- Given Estoques legados sem Filial, when o operador roda `migrar-estoques-filial --executar`, then existe uma Filial padrão por Empresa e todo Estoque está vinculado a ela, e a lista de Estoques (`ListarEstoques`) devolve `filial_id`/`filial_nome` preenchidos.
- Given a migração concluída, when se tenta inserir Estoque sem `filial_id`, then o banco rejeita (`NOT NULL`).
- Given a migração já executada, when roda de novo, then nada é reprocessado.
- Given o corte, when o binário é executado, then só por pessoa (sem rota/cron) e sem `--executar` só relata.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 4 (high 0, medium 2, low 2)
- defer: 1 (low 1)
- reject: ~40
- addressed_findings:
  - `[medium]` `[patch]` Diagnóstico e escrita rodavam sem lock: Estoque/Filial surgindo (ou 2ª execução do binário) entre o diagnóstico e o UPDATE. Adicionado `LOCK TABLE estoques IN SHARE ROW EXCLUSIVE MODE` no início da transação (`backend/services/migracao_estoques_filial.go`).
  - `[low]` `[patch]` Reexecução refazia o `ALTER ... SET NOT NULL` (lock exclusivo + scan à toa): agora só roda quando a coluna ainda é nullable.
  - `[medium]` `[patch]` Faltava teste da regra "colide só na Filial padrão": novo `TestMigrarEstoquesParaFilial_HomonimoEmFilialNaoPadrao` (homônimo em Filial não padrão migra; o de Filial padrão já aborta nos testes existentes).
  - `[low]` `[patch]` Teste de abort por nome duplicado passou a afirmar que nenhuma Filial é criada para Empresa sem Filial (nada escrito).
- Rejeitados (resumo): `nome_fantasia` vazio/NULL/longo (coluna é `NOT NULL VARCHAR(255)` igual a `filiais.nome`), Estoque já vinculado a Filial de outra Empresa (inalcançável pelos services), snapshot do dry-run, `statement_timeout`/audit trail/confirmação interativa (fora do intent; backup e conferência entram em `operator_actions`), duplicação da regra "mais antiga" (contrato), Empresas inativas (não existe flag), hardening de conexão/DSN no teste e duplicação de helpers (padrão do repositório), UI/HTTP fora do intent (a lista de Estoques 12.1 já expõe Filial e é coberta por `ListarEstoques` no teste).

## Auto Run Result

Status: awaiting-operator
Blocking condition: nenhuma — todo o código está entregue e verificado contra Postgres real; falta o corte em produção, que AD-15/PRD §9 reservam a uma pessoa (ver `operator_actions`).

**Resumo:** binário one-off `backend/cmd/migrar-estoques-filial` (dry-run por padrão, `--executar` aplica) sobre `services.MigrarEstoquesParaFilial`: numa transação (com `lock_timeout` e lock de tabela em `estoques`), pré-checa Estoques sem `empresa_id` ou com nome duplicado na Filial padrão (aborta sem escrever), cria a Filial padrão (nome = Nome Fantasia) das Empresas sem Filial, vincula todo Estoque com `filial_id NULL` à Filial mais antiga da própria Empresa, confere contagens e só então aplica `SET NOT NULL` em `estoques.filial_id`. Idempotente (2ª execução: 0 Filiais, 0 Estoques, sem `ALTER`). O `NOT NULL` fica no binário, não em migration SQL, para não derrubar o boot da API antes do backfill.

**Arquivos:**
- `backend/services/migracao_estoques_filial.go` — diagnóstico, migração transacional, `ErroEstoqueNaoMigravel`.
- `backend/services/migracao_estoques_filial_test.go` — cobre toda a matriz de I/O (corte inicial, Empresa com Filial, homônimo em Filial não padrão, idempotência/vinculados, duas Empresas, sem Estoque, abort sem Empresa/nome duplicado, dry-run); cleanup restaura `DROP NOT NULL` no banco compartilhado.
- `backend/cmd/migrar-estoques-filial/main.go` (+ `main_test.go`) — CLI com runbook no cabeçalho; testes de dry-run, execução + reexecução, abort e sem Estoque legado.
- `backend/Dockerfile` — build e copy do novo binário.

**Achados da revisão:** patches aplicados 4 (medium 2, low 2); adiados 1 (low); rejeitados ~40.
**Recomendação de nova revisão:** `true` (patches: high 0, medium 2, low 2; pontuação 3×2 + 2 = 8 ≥ 5).
**Verificação:** `go build ./...`, `go vet ./...` e `gofmt -l .` limpos; `go test -count=1 -p 1 ./services/ ./cmd/...` com `DATABASE_URL` passa (suíte completa de `services` ~470s); após os patches, testes de migração e de todos os `cmd/...` reexecutados e passando.
**Riscos residuais:** a migração é global (cria Filial para toda Empresa sem uma e vincula todo Estoque legado) — o dry-run antes do `--executar` é essencial; após o `NOT NULL`, qualquer build da API anterior à 12.1 que crie Estoque sem Filial passa a falhar (rollback de deploy exige cuidado); o CI não roda os testes de integração (pré-existente, registrado em `deferred`).

## Design Notes

`SET NOT NULL` no binário e não em migration: o deploy da 12.1 já aplicou as migrations com Estoques NULL em produção; uma migration `NOT NULL` derrubaria o `api` no boot antes de o operador rodar o backfill. Fechar dentro da mesma transação do backfill garante "nunca órfão" sem janela intermediária. A Filial padrão é criada para toda Empresa sem Filial (não só as com Estoque) para que a importação e o `migrate-legado` (que exigem Filial padrão) funcionem em Empresas anteriores à 12.1.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros e sem arquivos listados.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 -p 1 ./services/ ./cmd/...` -- expected: tudo passa.

### Review Findings

Code review independente (2026-09-21, 4 revisores: Blind Hunter, Edge Case Hunter, Verification Gap, Acceptance Auditor; achados verificados no código antes de classificar).

- [x] [Review][Patch][aplicado 2026-09-21] Erro `lock_timeout` (55P03) chega cru ao operador e o dry-run não avisa `nome_fantasia` vazio/longo que só falharia no `--executar` [backend/services/migracao_estoques_filial.go, backend/cmd/migrar-estoques-filial/main.go] — traduzir a mensagem e diagnosticar no dry-run
- [x] [Review][Patch][aplicado 2026-09-21] Nada confirma qual banco será alterado antes de `--executar` (irreversível: `SET NOT NULL`) — ecoar host/nome do banco no relatório
- [x] [Review][Defer] Sem caminho de reversão/auditoria da execução (só stdout) — deferred, a spec declara "sem rollback automático" e o runbook exige `pg_dump`
