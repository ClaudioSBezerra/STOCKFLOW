---
title: 'Story 2.3 — Migração dos Estoques legados'
type: 'feature'
created: '2026-08-30'
status: 'awaiting-operator'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: '9332770ba054f6d535b6b146e33b7bd9f46e73c0'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-2-context.md']
warnings: ['oversized']
deferred: []
operator_actions:
  - 'Provisionar/confirmar acesso ao PostgreSQL espelho do Firestore mantido pela empresa e definir `LEGADO_DATABASE_URL` apenas no ambiente de onde o corte será disparado — nunca no `.env` versionado nem no `docker-compose.yml`.'
  - 'Confirmar que a tabela `estoques` do espelho expõe as colunas `id` (textual, doc id do Firestore) e `nome` (texto), conforme addendum §F; se a estrutura divergir, ajustar `backend/cmd/migrate-legado/main.go` (query de leitura e scan) antes do corte.'
  - 'Rodar `migrate-legado` em dry-run (sem `--executar`), com `DATABASE_URL` apontando para o schema novo de produção e `LEGADO_DATABASE_URL` para o espelho, e revisar o relatório de nomes inválidos, nomes equivalentes e colisões com o alvo.'
  - 'Resolver manualmente no espelho legado todos os nomes inválidos e conflitos reportados (unificando registros equivalentes) e repetir o dry-run até ele reportar 0 problemas.'
  - 'Durante a janela de baixo uso acordada, com o plano de rollback pronto (PRD §9), rodar `migrate-legado --executar` — disparado manualmente por uma pessoa, nunca por um agente autônomo (AD-15, PRD §9).'
  - 'Após o corte, conferir que `SELECT count(*) FROM estoques` no alvo corresponde ao nº de Estoques do espelho somado ao que já existia, e que `SELECT count(*) FROM migracao_id_map WHERE entidade = ''estoque''` é igual ao nº de Estoques legados.'
---

<intent-contract>

## Intent

**Problem:** O Epic 2 entregou criar/listar/excluir Estoques no schema novo (Stories 2.1/2.2), mas os Estoques que já existem no sistema legado (protótipo Firestore, hoje espelhado num PostgreSQL local mantido pela empresa — addendum §F) ainda não têm caminho para o schema novo. Sem isso, o corte "big-bang" (AD-15, PRD §9) perderia todos os locais de armazenamento cadastrados e quebraria as referências que Produtos/Movimentações/Pedidos (épicos seguintes) fazem a `estoques(id)`.

**Approach:** Novo binário one-off `backend/cmd/migrate-legado` (molde de `cmd/seed-admin`, AD-15) que lê a tabela `estoques` do banco legado (`LEGADO_DATABASE_URL`) e recria cada local no banco alvo (`DATABASE_URL`) com um UUID v4 novo (gerado pelo `DEFAULT gen_random_uuid()` de `estoques.id`), gravando a correspondência id-antigo→id-novo numa tabela de mapeamento compartilhada nova, `migracao_id_map` (migration `000009`), que as migrações de Produtos/Movimentações/Pedidos/Usuários vão reusar. Idempotência vem do PK `(entidade, id_legado)` do mapa; a detecção de nomes legados equivalentes por caixa/espaço roda como pré-checagem no próprio banco legado usando a mesma expressão de normalização do índice `idx_estoques_nome_normalizado`, e aborta o corte sem escrever nada. O corte real em produção contra o espelho da empresa é disparado por uma pessoa (AD-15, PRD §9) → `status: awaiting-operator`.

## Boundaries & Constraints

**Always:**
- **Migration `backend/migrations/000009_create_migracao_id_map.up.sql`** (+ `.down.sql` com `DROP TABLE IF EXISTS migracao_id_map;`): cria `migracao_id_map (entidade TEXT NOT NULL, id_legado TEXT NOT NULL, id_novo UUID NOT NULL, migrado_em TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY (entidade, id_legado), UNIQUE (entidade, id_novo))`. Comentário SQL no arquivo enumera o vocabulário previsto de `entidade` (`estoque`, `produto`, `categoria`, `template_nomenclatura`, `movimentacao`, `pedido`, `usuario`) e explica que é `TEXT` (não enum/CHECK) de propósito — cada story de migração seguinte só insere linhas, nunca precisa alterar schema. Sequencial após `000008`; aplicada no startup como todas as outras (`main.go` `runMigrations`), mas o runtime da aplicação **nunca escreve** nela.
- **`backend/cmd/migrate-legado/main.go`** (novo, `package main`): comentário-cabeçalho de pacote no molde de `cmd/seed-admin/main.go` deixando explícito que é binário standalone, nunca rota HTTP, e que o corte é disparado por pessoa (AD-15, PRD §9). `main()` carrega `.env` via `godotenv.Load()` (ignora `os.IsNotExist`), lê `DATABASE_URL` e `LEGADO_DATABASE_URL` (ambas obrigatórias — ausência → `fmt.Fprintln(os.Stderr, ...)` + `os.Exit(1)`), abre os dois pools (`sql.Open("postgres", ...)` + `Ping`), e chama `migrarEstoques(alvo, legado, executar)`. `slog` para log estruturado, `fmt.Fprint*(os.Stderr/os.Stdout)` para o relatório humano — nunca `fmt.Print` sem destino.
- **Flag `--executar` (bool, default `false`)**: sem ela, o binário roda em **dry-run** — conecta, carrega as linhas legadas, roda a pré-checagem de conflito, imprime o que faria (`migraria N`, `já migrados M`, `conflitos C`) e **não escreve nada** no alvo. Com `--executar`, aplica a migração. Sob `--executar`, se a pré-checagem achar conflito, ainda assim **nada é escrito** (aborta antes de abrir a transação de escrita).
- **`migrarEstoques(alvo, legado *sql.DB, executar bool) (ResultadoMigracao, error)`** (função testável, não em `main`): 1) `SELECT id, nome FROM estoques ORDER BY id` no `legado` (colunas conforme addendum §F: `id` textual = doc id do Firestore, `nome`). 2) Pré-checagem de conflito no `legado`: `SELECT lower(regexp_replace(btrim(nome), '\s+', ' ', 'g')) AS norm, array_agg(id ORDER BY id), array_agg(nome ORDER BY id) FROM estoques GROUP BY 1 HAVING count(*) > 1` — **a mesma expressão da coluna gerada `nome_normalizado` (migration `000008`)**, executada pelo Postgres, não reimplementada em Go. Qualquer grupo devolvido → devolve `ResultadoMigracao{Conflitos: [...]}` e um `error` não-nil; nada é escrito. 3) Sem conflito e `executar == true`: abre **uma transação** no `alvo`; para cada linha legada, `SELECT id_novo FROM migracao_id_map WHERE entidade = 'estoque' AND id_legado = $1` — achou → conta como `JaMigrados++` e segue; não achou → `INSERT INTO estoques (nome) VALUES ($1) RETURNING id` (o UUID v4 vem do `DEFAULT`), depois `INSERT INTO migracao_id_map (entidade, id_legado, id_novo) VALUES ('estoque', $1, $2)`, conta `Migrados++`. `Commit` no fim. 4) `executar == false`: computa `Migrados`/`JaMigrados` do mesmo jeito porém **sem** `INSERT` e sem transação de escrita (só o `SELECT` no mapa).
- **Colisão de nome com o alvo:** se o `INSERT INTO estoques` devolver `pq` SQLSTATE `23505` (a linha legada não está no mapa, mas o `nome_normalizado` já existe no alvo — ex.: alguém criou o local manualmente pela tela antes do corte), `migrarEstoques` faz `tx.Rollback()` e devolve `error` identificando o `id_legado` e o `nome` — nada parcial fica gravado. Reusar a constante local `const pqUniqueViolation = "23505"` (declarada neste `package main`; `services.pqUniqueViolation` não é exportada e não se cruza limite de pacote).
- **`ResultadoMigracao`**: struct com `Migrados int`, `JaMigrados int`, `Conflitos []ConflitoNome` (`{Norm string; IDs []string; Nomes []string}`). `main()` imprime um resumo legível em `os.Stdout` no sucesso; em conflito/erro, imprime o detalhe em `os.Stderr` e `os.Exit(1)`.
- **Testes** (`backend/cmd/migrate-legado/main_test.go`): helper `testDB` no molde de `cmd/seed-admin/main_test.go` (`file://../../migrations`, `-p 1`, skip sem `DATABASE_URL`). O banco legado é simulado no **mesmo servidor Postgres**, no schema `legado` (`CREATE SCHEMA IF NOT EXISTS legado; CREATE TABLE legado.estoques (id text primary key, nome text not null)`), alcançado por uma segunda conexão cujo DSN é `DATABASE_URL` + parâmetro `search_path=legado`. Cada teste limpa `legado.estoques`, `estoques` e `migracao_id_map`. Cobrir toda a I/O & Edge-Case Matrix.

**Block If:**
- A estrutura real do espelho legado diverge do documentado no addendum §F (tabela `estoques` com `id` textual + `nome`) de forma que nenhum mapeamento de coluna trivial resolva — isso é ambiguidade de intent, não decisão de implementação. (Não deve disparar: o addendum §F documenta a estrutura; a confirmação contra o espelho real fica em `operator_actions`.)

**Never:**
- **Não executar o corte de produção** — o agente não tem (nem deve ter) acesso ao espelho Postgres real da empresa, e AD-15/PRD §9 exigem disparo humano. O binário e os testes são entregues e verificados agora; rodar `--executar` contra o espelho real é `operator_action`.
- **Nenhuma rota HTTP, handler, nem registro em `newMux`** — `migrate-legado` é binário one-off, igual a `seed-admin`. Não tocar `main.go` (fora do `runMigrations`, que pega `000009` automaticamente via `//go:embed migrations/*.sql`), `handlers/`, `services/estoques.go`, frontend.
- **Não reimplementar a normalização de nome em Go** — a autoridade é a expressão SQL do índice `000008`; a pré-checagem roda no banco, e a colisão real no corte é sinalizada pelo `23505` do índice único.
- **Não migrar Produtos, Categorias, Templates, Movimentações, Pedidos nem Usuários** — cada um é sua própria story (3.7, 5.4, 7.7, ...). Esta story só entrega Estoques + a tabela `migracao_id_map` que as demais reusam.
- **Nenhuma conversão de dados além de copiar `nome`** — Estoque legado só tem `nome` (addendum §F). Sem `criado_em` legado preservado (não existe no schema legado de `estoques`); o `estoques.criado_em` do alvo fica no `DEFAULT now()`.
- **Nenhum `CHECK`/enum em `migracao_id_map.entidade`**, nenhuma FK de `id_novo` para `estoques(id)` (o mapa é multi-entidade e sobrevive à exclusão de um Estoque migrado — é trilha de corte, não integridade referencial viva).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Corte inicial | `legado.estoques` com N linhas de nomes distintos (normalizados), `estoques`/`migracao_id_map` vazias, `--executar` | N linhas novas em `estoques` com `id` UUID v4 e `nome` preservado; N linhas em `migracao_id_map` com `entidade='estoque'`, `id_legado` = doc id legado, `id_novo` = UUID gerado; resumo `Migrados=N, JaMigrados=0` | — |
| Reexecução (idempotência) | Mesma entrada, migração já rodada uma vez, `--executar` de novo | Nenhuma linha nova em `estoques` nem em `migracao_id_map`; resumo `Migrados=0, JaMigrados=N`; exit 0 | linha já no mapa → pula |
| Nomes legados equivalentes | `legado.estoques` contém `"Canteiro A"` e `"canteiro  a"` (ids distintos), `--executar` | `error` não-nil; `os.Exit(1)`; **nada escrito** em `estoques`/`migracao_id_map`; stderr lista `norm`, os dois ids e os dois nomes | pré-checagem `HAVING count(*)>1` aborta antes da transação |
| Dry-run | Entrada válida sem conflito, **sem** `--executar` | stdout: `migraria N, já migrados M, conflitos 0`; `estoques` e `migracao_id_map` inalteradas; exit 0 | — |
| Dry-run com conflito | Entrada com nomes equivalentes, sem `--executar` | stderr lista o conflito; exit 1; nada escrito | — |
| Colisão com o alvo | `estoques` já tem `"Canteiro A"` (criado pela tela, fora do mapa); `legado.estoques` tem `"Canteiro A"`; `--executar` | `tx.Rollback()`; `error` citando `id_legado` + `nome`; exit 1; nenhuma linha parcial de outras entradas do mesmo lote | `INSERT INTO estoques` → `pq` `23505` |
| Env var ausente | `LEGADO_DATABASE_URL` (ou `DATABASE_URL`) não definida | mensagem em stderr, `os.Exit(1)`; nenhuma conexão de escrita aberta | checagem antes de `sql.Open` |
| Legado vazio | `legado.estoques` sem linhas | resumo `Migrados=0, JaMigrados=0, Conflitos=0`; exit 0; nada escrito | — |

</intent-contract>

## Code Map

- `backend/migrations/000008_create_estoques.up.sql` — **read-only**. A coluna gerada `nome_normalizado = lower(regexp_replace(btrim(nome), '\s+', ' ', 'g'))` e o índice `idx_estoques_nome_normalizado` são a autoridade de unicidade que a pré-checagem replica (mesma string de expressão) e que sinaliza colisão no corte via `23505`.
- `backend/migrations/000009_create_migracao_id_map.up.sql` / `.down.sql` — **novos**. Tabela de mapeamento compartilhada (AD-15). PK `(entidade, id_legado)` = idempotência; `UNIQUE (entidade, id_novo)`. Sequencial após `000008`; pega no `//go:embed migrations/*.sql` de `main.go` sem mudança de código.
- `backend/cmd/seed-admin/main.go` — **molde**. `godotenv.Load()` + `os.IsNotExist`; leitura de env var obrigatória com `os.Exit(1)`; `sql.Open`+`Ping`; `flag.*`; `errors.As(&pqErr)` + `pqErr.Code == "23505"`; `slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))`.
- `backend/cmd/seed-admin/main_test.go` — **molde do teste**. `testDB(t)` com `migrate.New("file://../../migrations", dsn)`, `migrateOnce`, retry, skip sem `DATABASE_URL`, nota `-p 1`.
- `backend/cmd/migrate-legado/main.go` / `main_test.go` — **novos**. Ver Boundaries. `migrarEstoques(alvo, legado *sql.DB, executar bool) (ResultadoMigracao, error)` é o ponto testável; `main()` só faz env/flags/pools/print/exit.
- `backend/services/estoques.go` — **read-only, referência**. `CriarEstoque` faz `INSERT INTO estoques (nome) VALUES ($1) RETURNING id` e mapeia `23505`→`ErrNomeEstoqueDuplicado`; **não reusar direto** (precisa de `*sql.Tx` para atomicidade com a linha do mapa, e `CriarEstoque` recebe `*sql.DB`). O padrão SQL é o mesmo.
- `backend/go.mod` — `github.com/lib/pq`, `github.com/joho/godotenv`, `github.com/golang-migrate/migrate/v4` já presentes. **Nenhuma dependência nova** (sem lib de UUID — o `DEFAULT gen_random_uuid()` do Postgres cobre AC1).
- `.env.example` — adicionar `LEGADO_DATABASE_URL` (comentado, com nota de que é preenchido pelo operador para o corte; ausente por padrão, o binário não roda).
- `_bmad-output/planning-artifacts/prds/prd-stockflow-2026-08-29/addendum.md` §F — **read-only**. Estrutura legada de `estoques`: só `nome` (string, único) + o doc id do Firestore. Nota explícita: reverificar se a estrutura do espelho divergir.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000009_create_migracao_id_map.up.sql` (+ `.down.sql`) — criar `migracao_id_map` compartilhada (PK `(entidade,id_legado)`, `UNIQUE (entidade,id_novo)`); comentário com vocabulário de `entidade` e a justificativa de `TEXT` sem `CHECK`.
- `backend/cmd/migrate-legado/main.go` — binário one-off: env (`DATABASE_URL`, `LEGADO_DATABASE_URL`), flag `--executar`, dois pools, `migrarEstoques`, resumo em stdout / detalhe de erro em stderr + `os.Exit(1)`. Cabeçalho de pacote citando AD-15/PRD §9 (corte disparado por pessoa).
- `backend/cmd/migrate-legado` — `migrarEstoques(alvo, legado, executar)`: lê `legado.estoques`; pré-checagem de conflito via expressão SQL do índice `000008`; transação única no alvo com `SELECT` no mapa (idempotência) + `INSERT estoques RETURNING id` + `INSERT migracao_id_map`; `23505` → `Rollback` + erro; dry-run não escreve.
- `backend/cmd/migrate-legado/main_test.go` — `testDB` (molde seed-admin); schema `legado` simulado via segunda conexão com `search_path=legado`; casos de toda a I/O & Edge-Case Matrix.
- `.env.example` — entrada `LEGADO_DATABASE_URL` comentada, com nota de operador.

**Acceptance Criteria:**
- Given a tabela `estoques` do banco legado com locais de nomes normalizados distintos e `migracao_id_map` vazia, when um operador roda `migrate-legado --executar` apontando `LEGADO_DATABASE_URL` para o espelho e `DATABASE_URL` para o schema novo, then cada Estoque legado vira uma linha em `estoques` com `id` UUID v4 novo e `nome` preservado, e uma linha `entidade='estoque'` em `migracao_id_map` liga o id legado ao id novo.
- Given a migração já executada uma vez, when o script roda de novo com `--executar`, then nenhum Estoque é duplicado — todas as linhas legadas são reconhecidas pelo mapa e o resumo reporta `Migrados=0`.
- Given dois Estoques legados com nomes equivalentes por caixa/espaço, when o script roda (dry-run ou `--executar`), then ele aborta com código de saída 1, relata os ids/nomes em conflito para revisão manual e não escreve nada em `estoques` nem em `migracao_id_map`.
- Given qualquer execução do binário, when ela ocorre, then é sempre um comando disparado por uma pessoa (AD-15, PRD §9) — não há rota HTTP, cron, nem chamada a partir do runtime da aplicação, e sem `--executar` o binário só relata, sem escrever.

## Spec Change Log

_Nenhuma alteração — não houve loopback de `bad_spec`._

## Review Triage Log

### 2026-08-30 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 13: (high 0, medium 5, low 8)
- defer: 0
- reject: 13
- addressed_findings:
  - `[medium]` `[patch]` Dry-run e `--executar` agora rodam pré-checagem das linhas legadas contra `estoques.nome_normalizado` do alvo (linhas fora do mapa) e relatam **todas** as colisões com o alvo de uma vez, antes de abrir transação — o `23505` no `INSERT` continua como backstop. Antes o dry-run só olhava o mapa e dava falsa confiança; a colisão só aparecia uma-a-uma no meio do corte.
  - `[medium]` `[patch]` Pré-validação de cada nome legado, reportada em bloco como classe de erro própria (não misturada no grupo de equivalência): nome `NULL` (scan via `sql.NullString`), vazio após normalização, ou > 255 runes. Antes: nome vazio entrava como Estoque em branco; nome > 255 ou `NULL` abortava o corte inteiro com erro opaco no meio da transação.
  - `[medium]` `[patch]` `main()` recusa quando `DATABASE_URL` e `LEGADO_DATABASE_URL` são strings iguais (erro + `os.Exit(1)`) — evita ler e reescrever o próprio alvo, duplicando linhas por engano de cópia/cola.
  - `[medium]` `[patch]` `main()` rejeita argumentos posicionais inesperados (`flag.NArg() > 0` → erro + `os.Exit(1)`) — `migrate-legado executar` (sem os hifens) não pode mais rodar um dry-run silencioso que o operador acha que foi o corte real.
  - `[medium]` `[patch]` Cobertura do processo: o harness de subprocesso (`MIGRATE_LEGADO_SUBPROC`) agora exercita o caminho de conflito de `main()` (exit 1 + stderr com o nome normalizado e os dois ids legados) e o caminho de resumo de sucesso (linha `corte aplicado:` / `dry-run (nada escrito):` no stdout). Antes só o early-exit de env var ausente passava por `main()`.
  - `[low]` `[patch]` Removido o `Conflitos=%d` (sempre `0`) da linha de sucesso do `--executar` — conflito aborta antes, era saída morta e enganosa.
  - `[low]` `[patch]` `.env.example`: o exemplo de `LEGADO_DATABASE_URL` não fixa mais `sslmode=disable`; comentário orienta ajustar conforme o ambiente.
  - `[low]` `[patch]` `000009_create_migracao_id_map.down.sql` ganhou comentário avisando que dropar a tabela descarta a trilha id-antigo→id-novo da qual as migrações de dados seguintes dependem.
  - `[low]` `[patch]` `pareceUUIDv4` (teste) agora também confere o nibble de variante (`s[19]` ∈ `8/9/a/b`), não só o dígito de versão — asserção de AC1 mais forte.
  - `[low]` `[patch]` Comentário em `migrarEstoques` registrando que o `estoques` legado não tem `criadoEm` (addendum §F), então `criado_em` do alvo assume o `DEFAULT now()` — contexto para um auditor futuro.
  - `[low]` `[patch]` `TestMigrarEstoques_ColisaoComAlvo`: `idPre` não era lido; scan agora vai para `_`.
  - `[low]` `[patch]` O ramo de erro genérico (não-conflito, não-colisão) de `main()` agora diz "(transação revertida, nada foi escrito)" — antes só a mensagem de colisão dava essa garantia explícita.
  - `[low]` `[patch]` Novo teste `--executar` com estado de mapa misto (uma linha já mapeada, uma nova) numa única execução — limita o `continue` após `JaMigrados++` diretamente.

Notas de triagem (rejeitados, não repetidos acima):
- **reject** — `defer alvo.Close()`/`legado.Close()` não roda sob `os.Exit` (padrão idêntico ao de `cmd/seed-admin`); sem logging de progresso em `migrarEstoques` (escala de ~30 Estoques, PRD); sem batching/`--limit` (fora de escopo — story é só Estoques); sem teste dos ramos de `Ping` falho (trivial, mesmo padrão não-testado de `seed-admin`); sem relatório de reconciliação no binário (o spec o define explicitamente como *manual check*); env var não documentada em `docker-compose.yml`/runbook (mesmo precedente de `seed-admin`; `operator_actions` carrega o runbook); sem `context`/timeout nas queries (convenção de todo o codebase); asserção de ordenação de ids sintéticos no teste (o teste está correto — `array_agg ORDER BY id` é determinístico); `.env` malformado só gera `slog.Warn` (mesmo padrão de `seed-admin`/`main.go`); sem limite de tamanho em `id_legado TEXT` (não é ameaça real para doc id do Firestore); `comSearchPath` ramo keyword/value sem teste (código de helper, não de produto); `000009` down nunca exercitado (convenção pré-existente de todas as migrations).

## Design Notes

- **`awaiting-operator`, não `done`:** todo o código (binário + migration + testes) é implementável e verificável agora contra um Postgres real. O que falta é a AC "execução sempre disparada manualmente por uma pessoa" (AD-15, PRD §9) exercida contra o **espelho Postgres real da empresa**, ao qual o agente não tem acesso e não deve ter. Isso é ação de operador fora do repositório — mesma categoria de `spec-1-9` (provisionamento Keycloak). `blocked` seria errado: a story está pronta até onde um agente pode levá-la.
- **Duas conexões, não uma:** o corte lê de um Postgres (espelho do Firestore, `LEGADO_DATABASE_URL`) e escreve noutro (schema novo, `DATABASE_URL`). Em produção são servidores distintos; no teste é o mesmo servidor com o "legado" isolado no schema `legado`, alcançado por um DSN com `search_path=legado` (parâmetro repassado pelo `lib/pq` ao servidor). Assim a suíte precisa de um só Postgres, como as demais.
- **Idempotência pelo PK do mapa, não por `SELECT` de nome:** reexecutar consulta `migracao_id_map (entidade='estoque', id_legado)` — se a linha existe, o Estoque já foi migrado, mesmo que alguém tenha renomeado o local no alvo depois. Amarrar pelo id legado (imutável) é mais forte que amarrar pelo nome.
- **Pré-checagem no banco, com a expressão do índice:** `lower(regexp_replace(btrim(nome), '\s+', ' ', 'g'))` é copiada verbatim de `000008`. Rodando no Postgres legado (que é Postgres, addendum §F), o resultado é idêntico ao que o índice `idx_estoques_nome_normalizado` imporia — sem risco de uma reimplementação Go divergir (`\s` do Postgres ≠ `unicode.IsSpace`). É pré-checagem para dar um relatório acionável **antes** do corte (AC2); o backstop final continua sendo o `23505` do índice.
- **`migracao_id_map` é criada aqui e reusada:** AD-15 manda uma tabela de mapeamento id-antigo→id-novo compartilhada entre as migrações de Produtos/Estoques/Movimentações/Pedidos/Usuários. A Story 2.3 é a primeira a rodar, então cria a tabela; 3.7/5.4/7.7 só inserem linhas com outro `entidade`. `TEXT` sem `CHECK` em `entidade` de propósito: um `CHECK`/enum obrigaria cada story seguinte a também migrar o schema. O runtime da aplicação nunca escreve nessa tabela.
- **Sem lib de UUID:** `estoques.id` já é `UUID PRIMARY KEY DEFAULT gen_random_uuid()` (`000008`) — `gen_random_uuid()` produz v4. `INSERT INTO estoques (nome) VALUES ($1) RETURNING id` devolve o UUID novo, atendendo a AC1 sem dependência Go nova.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída de `gofmt`; build e vet limpos (o novo pacote `cmd/migrate-legado` compila).
- `cd backend && go test -p 1 -count=1 ./...` — Postgres real (mesmo setup das Stories 1.5–2.2). `cmd/migrate-legado/main_test.go` cobre: corte inicial (UUID v4 + linha no mapa), idempotência (2ª execução não duplica), conflito de nomes equivalentes (aborta, nada escrito), dry-run (não escreve), colisão com linha pré-existente no alvo (`23505` → rollback), env var ausente, legado vazio. `migrate` aplica `000009` automaticamente.
- `cd frontend && npm run lint && npm run build && npm run test` — inalterado (nenhuma mudança de frontend); roda só para confirmar que nada quebrou.

**Manual checks (if no CLI):**
- Inspecionar `backend/migrations/000009_create_migracao_id_map.up.sql`: PK `(entidade, id_legado)`, `UNIQUE (entidade, id_novo)`, `id_novo UUID`.
- `SELECT count(*) FROM estoques` no alvo == `SELECT count(*) FROM estoques` no legado (mais o que já existia no alvo antes do corte), e `SELECT count(*) FROM migracao_id_map WHERE entidade='estoque'` == nº de Estoques legados, após um `--executar` bem-sucedido.

## Auto Run Result

Status: awaiting-operator
Blocking condition: nenhuma — todo o código (binário + migration + testes) está entregue e verificado contra Postgres real. O que falta é a AC "execução sempre disparada manualmente por uma pessoa" (AD-15, PRD §9) exercida contra o espelho Postgres real da empresa, ao qual o agente não tem acesso — ação de operador fora do repositório (ver `operator_actions` na frontmatter).

### Resumo da mudança
Story 2.3 entrega o binário one-off `backend/cmd/migrate-legado` que traz os Estoques do sistema legado (espelho Postgres do Firestore, `LEGADO_DATABASE_URL`) para o schema novo (`DATABASE_URL`). Cada Estoque legado é recriado em `estoques` com UUID v4 novo (do `DEFAULT gen_random_uuid()` da migration `000008`), preservando o `nome`, e uma linha `entidade='estoque'` em `migracao_id_map` liga o id legado ao id novo. Nova migration `000009_create_migracao_id_map` cria essa tabela de mapeamento **compartilhada** (PK `(entidade, id_legado)` = idempotência; `UNIQUE (entidade, id_novo)`; `entidade` é `TEXT` sem `CHECK` de propósito, para as migrações seguintes só inserirem linhas). Sem a flag `--executar`, o binário roda em dry-run e não escreve nada. Antes de qualquer escrita ele roda três pré-checagens no banco (nunca reimplementando a normalização em Go — usa a expressão verbatim do índice `idx_estoques_nome_normalizado`): (1) nomes legados inválidos (nulo / vazio após normalização / > 255 runes); (2) nomes legados equivalentes entre si por caixa/espaço; (3) colisão de nome com uma linha já existente em `estoques` no alvo fora do mapa. Qualquer uma → aborta, lista tudo no stderr, `os.Exit(1)`, nada escrito. O corte real (`--executar`) roda numa única transação; o `23505` do índice único fica como backstop. O corte de produção contra o espelho real é ação de operador (AD-15, PRD §9) → `status: awaiting-operator`.

### Arquivos alterados
- `backend/migrations/000009_create_migracao_id_map.up.sql` — nova tabela de mapeamento compartilhada id-antigo→id-novo; comentário enumera o vocabulário de `entidade` e justifica `TEXT` sem `CHECK`.
- `backend/migrations/000009_create_migracao_id_map.down.sql` — `DROP TABLE IF EXISTS migracao_id_map;` com comentário de aviso (dropar descarta a trilha do corte da qual as migrações seguintes dependem).
- `backend/cmd/migrate-legado/main.go` — binário one-off: env (`DATABASE_URL`, `LEGADO_DATABASE_URL`, recusa se iguais), flag `--executar`, rejeita argumento posicional, dois pools, `migrarEstoques(alvo, legado, executar)`, relatório em stdout / detalhe de cada classe de problema em stderr + `os.Exit(1)`.
- `backend/cmd/migrate-legado/main_test.go` — helper `testDB` (molde `cmd/seed-admin`), legado simulado no schema `legado` via segunda conexão com `search_path=legado`; 13 funções de teste / 20 subtestes cobrindo toda a I/O & Edge-Case Matrix mais o caminho de processo de `main()` (exit codes + stdout/stderr) via subprocesso.
- `.env.example` — entrada `LEGADO_DATABASE_URL` comentada, com nota de operador e de `sslmode`.
- `_bmad-output/implementation-artifacts/spec-2-3-migracao-dos-estoques-legados.md` — este spec.

### Achados da revisão
- **Patches aplicados: 13** — (medium 5) pré-checagem de colisão com o alvo nos dois modos, em bloco; pré-validação de nome legado (nulo/vazio/>255) como classe de erro própria; recusa `alvo == legado`; rejeita argumento posicional (evita dry-run silencioso); cobertura de processo de `main()` (exit codes + stderr/stdout) por subprocesso. (low 8) remoção do `Conflitos=0` da linha de sucesso do `--executar`; `.env.example` sem `sslmode=disable` fixo; comentário de aviso no `000009` down; `pareceUUIDv4` confere nibble de variante; comentário sobre `criado_em`/`DEFAULT now()`; `idPre` de teste não usado → `_`; reassurance "transação revertida" no erro genérico; teste `--executar` de mapa misto.
- **Itens adiados (`deferred`): 0.**
- **Itens rejeitados: 13** — ver `## Review Triage Log` (defer+`os.Exit` como no `seed-admin`; sem logging de progresso; sem batching/`--limit` — fora de escopo; sem teste de `Ping` falho; sem relatório de reconciliação no binário — é *manual check* do spec; env var fora de `docker-compose`/runbook — precedente `seed-admin`; sem `context`/timeout — convenção do codebase; ordenação de ids sintéticos no teste; `.env` malformado só `slog.Warn`; sem limite de tamanho em `id_legado TEXT`; `comSearchPath` ramo sem teste; `000009` down nunca exercitado).
- **Recomendação de revisão de follow-up:** `true`. Patches deste pass por severidade: high 0, medium 5, low 8. Score = 3×5 + 1×8 = 23 (≥ 5).

### Verificação executada
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — limpo (sem saída de `gofmt`, build e vet ok).
- `cd backend && go test -p 1 -count=1 ./...` — PASS nos 7 pacotes (`backend`, `cmd/migrate-legado`, `cmd/seed-admin`, `handlers`, `iam`, `middleware`, `services`), contra cluster PostgreSQL efêmero (`docker` indisponível no ambiente — mesma nota das stories anteriores; cobertura equivalente pelos testes de integração contra Postgres real). `cmd/migrate-legado`: 13 funções / 20 subtestes, todas rodaram e passaram. Auditoria da Matrix: cada linha da I/O & Edge-Case Matrix tem teste que rodou e passou (Corte inicial → `TestMigrarEstoques_CorteInicial`; Reexecução → `_Idempotente` + `_ExecutarMapaMisto`; Nomes equivalentes → `_NomesEquivalentes/executar` + `TestMain_Processo/conflito...`; Dry-run → `_DryRun` + `TestMain_Processo/sucesso_dry-run`; Dry-run com conflito → `_NomesEquivalentes/dry-run`; Colisão com o alvo → `_ColisaoComAlvo/{executar,dry-run}`; Env var ausente → `TestMain_Processo/{DATABASE_URL,LEGADO_DATABASE_URL}_ausente`; Legado vazio → `_LegadoVazio`).
- `frontend`: nenhum arquivo tocado — `npm run lint/build/test` não executado.

### Riscos residuais
- **Estrutura do espelho legado assumida** (addendum §F: `estoques` com `id` textual + `nome`). Os testes exercitam um schema `legado.estoques` sintético no mesmo servidor; se o espelho real divergir, `cmd/migrate-legado` precisa de ajuste antes do corte (coberto em `operator_actions` e no `Block If` do intent-contract).
- **`--executar` real não rodado** — por design (AD-15/PRD §9). O caminho HTTP não existe; o caminho DB está coberto por testes de integração contra Postgres real.
- **Estabilidade do `id_legado`** (doc id do Firestore) entre refreshes do espelho é premissa da idempotência (AC3); testável só contra a fonte real.
- `followup_review_recommended: true` — 13 patches num pass (5 medium), pontuação 23. Uma segunda passada de revisão focada é recomendada quando/se a story voltar ao loop.
