---
title: 'Story 1.1 — Bootstrap do primeiro Adm e fundação do backend'
type: 'feature'
created: '2026-08-29'
status: 'done'
baseline_revision: '4de78d9c7335ed1ef7137ca0fc4b237b42bb2007'
review_loop_iteration: 0
followup_review_recommended: true
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      Não há pipeline de CI que efetivamente rode os testes de integração
      contra um Postgres real; sem DATABASE_URL definido, `go test` reporta
      PASS mesmo pulando (`t.Skip`) toda a cobertura da CHECK constraint em
      `papel`, do índice único e da garantia de admin único.
    evidence: |-
      Confirmado rodando `cd backend && unset DATABASE_URL && go test ./... -v`:
      todos os testes com dependência de banco imprimem `--- SKIP` e o
      pacote ainda reporta PASS. Não existe `.github/workflows`, `Makefile`
      nem script no repo que suba um Postgres e exporte DATABASE_URL antes
      de `go test`. Configurar CI/CD é um item de AD-16 (envelope
      operacional de todo o projeto), não uma AC desta story.
    location: '.github/workflows (inexistente)'
    severity: medium
  - summary: >-
      `cmd/seed-admin` recebe a senha do primeiro Adm via flag `--senha` em
      texto plano, visível no histórico do shell e em `ps`/`/proc` de outros
      usuários locais.
    evidence: |-
      `backend/cmd/seed-admin/main.go` usa `flag.String("senha", ...)`. Uma
      alternativa mais segura (prompt interativo com eco desligado, ou leitura
      via stdin/variável de ambiente) exigiria uma decisão de UX/segurança
      fora do escopo desta story, cujo Code Map já especificava as três
      flags `--nome/--email/--senha`.
    location: 'backend/cmd/seed-admin/main.go:31'
    severity: medium
  - summary: >-
      Não há README (ou doc equivalente) explicando como subir o stack, aplicar
      migrations ou invocar `seed-admin` — inclusive a partir da imagem Docker,
      cujo `ENTRYPOINT` fixo (`./api`) exige saber sobrescrevê-lo para rodar o
      seed-admin.
    evidence: |-
      Nenhum `README.md` em `backend/` ou na raiz cobre esses passos; a única
      documentação está nos comentários do próprio código-fonte.
    location: 'backend/ (sem README)'
    severity: low
  - summary: >-
      Chamadas ao banco em `seedAdmin`, `runMigrations` e `healthHandler` não
      usam `context.WithTimeout`/`context.WithDeadline` — uma conexão travada
      após um `Ping` bem-sucedido bloqueia indefinidamente em vez de falhar
      rápido.
    evidence: |-
      `backend/cmd/seed-admin/main.go` usa `db.QueryRow(query, ...)` sem
      contexto; `backend/main.go`'s `runMigrations` chama `m.Up()` sem
      deadline. `healthHandler` já usa `PingContext(r.Context())`, mas as
      demais chamadas não têm proteção equivalente. Adicionar timeouts exigiria
      re-plumbing de assinaturas fora do escopo mínimo desta story.
    location: 'backend/cmd/seed-admin/main.go:112; backend/main.go (runMigrations)'
    severity: low
  - summary: >-
      A correção do shutdown gracioso (aguardar `shutdownDone` antes de
      `db.Close()`, feita na revisão anterior desta story) não tem nenhum
      teste que prove a ordenação sob um SIGTERM real com requisição em voo.
    evidence: |-
      Busca por `SIGTERM` e `Shutdown(` no repo mostra ocorrências só dentro de
      `backend/main.go` (linhas do próprio handler de sinal); nenhum teste
      invoca `main()`, envia sinal, ou observa a ordem entre o fechamento do
      listener e o fechamento do pool. Revertendo o `<-shutdownDone` para o
      comportamento anterior ao patch, nenhum teste existente falharia. Testar
      isso de forma não-flaky exigiria subir o servidor real e orquestrar
      sinais/requisições concorrentes — não é uma correção trivial de uma
      revisão automatizada.
    location: 'backend/main.go (shutdownDone)'
    severity: low
  - summary: >-
      `cmd/seed-admin` não valida o tamanho de `--nome`/`--email` contra o
      limite `VARCHAR(255)` das colunas `usuarios.nome`/`usuarios.email` antes
      do INSERT — um valor maior surge como um erro cru do Postgres em vez de
      uma mensagem clara.
    evidence: |-
      `backend/migrations/000001_create_usuarios.up.sql` declara `nome
      VARCHAR(255)` e `email VARCHAR(255)`; `seedAdmin` em
      `backend/cmd/seed-admin/main.go` só faz `strings.TrimSpace` e
      `normalizeEmail`, sem checagem de comprimento. Não há AC que exija essa
      validação — string/UX é decisão fora do escopo mínimo desta story.
    location: 'backend/cmd/seed-admin/main.go:97 (seedAdmin)'
    severity: low
---

<intent-contract>

## Intent

**Problem:** O backend do stockflow ainda não existe (repositório vazio, sem `backend/`). Não há como provisionar a primeira conta `adm` sem um endpoint HTTP de auto-promoção — um vetor de escalação de privilégio (AD-12) — nem schema de banco para sustentar qualquer story futura.

**Approach:** Criar o módulo Go do backend (`backend/`) com um runner de migrations SQL aplicado de forma síncrona no startup da aplicação (bloqueia até concluir, antes de aceitar tráfego), a tabela `usuarios` inicial, e um comando CLI dedicado `cmd/seed-admin` — nunca uma rota HTTP — que cria a primeira conta `adm` e recusa rodar novamente se já existir uma.

## Boundaries & Constraints

**Always:**
- Migrations SQL versionadas, aplicadas sequencialmente e de forma síncrona no startup de `main.go`, antes do servidor HTTP aceitar conexões (`Fatal` em caso de falha — nunca subir com schema divergente).
- `cmd/seed-admin` é um binário Go separado (`backend/cmd/seed-admin/main.go`), nunca um handler HTTP nem rota registrada em `main.go` (AD-12).
- E-mail sempre normalizado para minúsculas antes de gravar; unicidade garantida por índice único funcional sobre `lower(email)` (AD-14) — nunca só unicidade no valor bruto.
- Senha do Adm semeado sempre hasheada com bcrypt (`golang.org/x/crypto/bcrypt`) — nunca texto plano, nunca logada.
- `id` UUID v4 (via `gen_random_uuid()` do Postgres, nativo desde PG13 — sem lib Go de UUID), `criado_em timestamptz` (UTC).
- Convenções já ratificadas na spine: tabelas/colunas em português, pacotes/tipos Go em inglês, `log/slog` estruturado (nunca `log`/`fmt.Print`), Go 1.27, `postgres:15-alpine`.

**Block If:** nenhuma decisão deste escopo depende de aprovação humana — DNS/servidor real (AD-13 Deferred) não é tocado por esta story.

**Never:**
- Nenhum endpoint HTTP equivalente a `seed-admin` (checado como AC explícito).
- Nenhuma dependência de ORM, framework web, Redis ou RabbitMQ (AD-1, AD-2).
- Nenhum outro domínio (handlers/services/middleware/iam/realtime, tabelas além de `usuarios`) criado nesta story — fundação estritamente no escopo dos ACs; demais stories do épico criam o restante.
- `seed-admin` nunca altera uma conta `adm` já existente ao rodar de novo — falha e sai sem tocar no banco.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Startup limpo | banco sem tabela `usuarios` | migrations aplicadas, tabela `usuarios` criada com todas as colunas do AC1 | app não sobe se uma migration falhar (`log.Fatal`) |
| Seed inicial | banco migrado, nenhum `adm` cadastrado, `seed-admin --nome=X --email=Y --senha=Z` | conta criada com `papel=adm`, `senha_hash` bcrypt, `email` normalizado, `email_verificado=true`, `ativo=true` | saída 0, mensagem de sucesso no stdout |
| Seed duplicado | já existe conta `papel=adm` | comando falha, nenhuma linha alterada/inserida | saída != 0, mensagem clara no stderr |
| Seed com e-mail maiúsculo | `--email=Admin@Empresa.COM` | gravado como `admin@empresa.com` | — |
| Seed com senha fraca | senha não atende à política (Story 1.10, ainda não implementada) | fora de escopo desta story — sem validação de força aqui, apenas hashing | — |

</intent-contract>

## Code Map

- `backend/go.mod` -- novo módulo Go 1.27; deps: `golang-migrate/migrate/v4` (+ driver `postgres` e source `iofs`), `lib/pq`, `golang.org/x/crypto/bcrypt`, `joho/godotenv` (carregar `.env` local, padrão já validado no `FB_APU02`).
- `backend/main.go` -- bootstrap: `godotenv.Load()`, abre `*sql.DB` (`DATABASE_URL`), roda migrations embutidas (`go:embed` em `migrations/*.sql`) de forma síncrona e bloqueante, configura `log/slog`, sobe `net/http` com `GET /api/health` (liveness, único endpoint desta story — usado pelo healthcheck do compose/CI, AD-16).
- `backend/migrations/000001_create_usuarios.up.sql` / `.down.sql` -- cria `usuarios` (ver DDL em Design Notes) + índice único `lower(email)`.
- `backend/cmd/seed-admin/main.go` -- CLI com flags `--nome`, `--email`, `--senha`; conecta no mesmo `DATABASE_URL`; checa `EXISTS(SELECT 1 FROM usuarios WHERE papel='adm')`; insere ou falha conforme I/O Matrix.
- `backend/Dockerfile` -- multi-stage `golang:1.27-alpine` build → `alpine` runtime, padrão AD-13.
- `docker-compose.yml` (raiz) -- serviços `db` (`postgres:15-alpine`, healthcheck `pg_isready`) e `api` (build do `backend/Dockerfile`, `depends_on: db healthy`), AD-13.
- `.env.example` (raiz) -- `DATABASE_URL`, `PORT` documentados, sem segredos reais.
- Referência lida (read-only, não copiado literalmente): `/home/claudio/projetos/FB_APU02/backend/main.go` -- padrão de pool de conexão (`SetMaxOpenConns(50)`, `SetMaxIdleConns(15)`, `SetConnMaxLifetime(15min)`) e de bcrypt (`bcrypt.GenerateFromPassword`); **divergências deliberadas**: aqui migrations rodam síncronas com `golang-migrate` real (não o runner manual do FB_APU02), `email` é normalizado na escrita (não só na leitura), `papel` tem `CHECK` (não `VARCHAR` livre), logging via `log/slog` (não `log`).

## Tasks & Acceptance

**Execution:**
- `backend/go.mod`, `backend/go.sum` -- inicializar módulo `stockflow/backend` e declarar dependências -- base para todo o resto.
- `backend/migrations/000001_create_usuarios.up.sql` + `.down.sql` -- schema da tabela `usuarios` -- satisfaz AC1.
- `backend/main.go` -- config via env, conexão `*sql.DB`, execução síncrona das migrations, `log/slog`, `GET /api/health` -- satisfaz AC1 (startup aplica migrations) e dá um processo executável para a fundação do backend.
- `backend/cmd/seed-admin/main.go` -- CLI de bootstrap do primeiro Adm -- satisfaz AC2 e AC3.
- `backend/cmd/seed-admin/main_test.go` / `backend/*_test.go` -- testes de integração contra Postgres real (via `docker-compose`) cobrindo a I/O Matrix (seed inicial, seed duplicado, normalização de e-mail) -- prova as ACs sem depender de inspeção manual.
- `backend/Dockerfile`, `docker-compose.yml`, `.env.example` -- fundação executável localmente -- necessário para rodar os testes de integração e para AD-13/AD-16.

**Acceptance Criteria:**
- Given a tabela `usuarios` inexistente, when as migrations SQL são aplicadas no startup da aplicação, then a tabela `usuarios` é criada com colunas `id` (UUID v4), `nome`, `email` (único, normalizado para minúsculas), `senha_hash` (nullable), `papel` (restrito a `usuario`/`almoxarife`/`gestor`/`adm`), `email_verificado` (bool), `ativo` (bool), `criado_em`.
- Given o banco migrado e nenhum Adm cadastrado, when o operador executa `cmd/seed-admin` informando nome, e-mail e senha, then uma conta é criada com papel `adm`, senha hasheada (bcrypt), e-mail normalizado, and não existe nenhum endpoint HTTP equivalente a este comando.
- Given já existe uma conta com papel `adm`, when o operador tenta rodar `cmd/seed-admin` novamente, then o comando falha com mensagem clara, sem alterar a conta existente.

## Review Triage Log

### 2026-08-29 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 5: (high 0, medium 4, low 1)
- defer: 3: (high 0, medium 2, low 1)
- reject: 11: (high 0, medium 0, low 11)
- addressed_findings:
  - `[medium]` `[patch]` seed-admin's `INSERT ... WHERE NOT EXISTS` duplicate-admin check could lose a race between two concurrent runs — added a DB-level partial unique index (`idx_usuarios_unico_adm` on `papel` where `papel='adm'`) and mapped the resulting unique-violation (SQLSTATE 23505) to `errAdminAlreadyExists`; added `TestRunMigrations_UniqueAdminIndex` proving the DB itself rejects a second row.
  - `[medium]` `[patch]` `backend/Dockerfile`'s `COPY . .` had no `.dockerignore`, risking a local `.env`/`.git` baked into the image — added `backend/.dockerignore`.
  - `[low]` `[patch]` `backend/Dockerfile`'s final stage ran as root — added a non-root `appuser` and `USER appuser` before `ENTRYPOINT`.
  - `[medium]` `[patch]` `docker-compose.yml`'s `api` service had no `healthcheck` despite `main.go`'s own comment claiming one — added a `wget --spider` healthcheck against `/api/health`.
  - `[medium]` `[patch]` the `backend` and `backend/cmd/seed-admin` test packages share the same live `usuarios` table but `go test ./...` runs packages concurrently by default, risking cross-package flakiness (one package's `TRUNCATE` racing another's assertions) — updated the spec's Verification command to `go test -p 1 ./...` and documented the shared-table constraint in both packages' `testDB` comments.

### 2026-08-29 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 3: (high 0, medium 2, low 1)
- defer: 0
- reject: 22: (high 0, medium 3, low 19)
- addressed_findings:
  - `[low]` `[patch]` `backend/cmd/seed-admin/main.go`'s required-flags check used `strings.TrimSpace` for `--nome`/`--email` but only `*senha == ""` (untrimmed) for `--senha`, so a whitespace-only password (e.g. `" "`) would pass validation and get hashed as the first admin's real password — changed the check to `strings.TrimSpace(*senha) == ""`, matching the other two flags.
  - `[medium]` `[patch]` `backend/main.go`'s graceful-shutdown goroutine called `server.Shutdown(ctx)` on SIGTERM/SIGINT, but `server.ListenAndServe()` returns `http.ErrServerClosed` as soon as `Shutdown()` closes the listener — before `Shutdown()` finishes draining in-flight connections (up to its 10s timeout) — so `main()` could fall through to `defer db.Close()` and close the DB pool while requests were still being drained, defeating the graceful-shutdown window entirely. Added a `shutdownDone` channel closed at the end of the signal-handling goroutine and made `main()` block on it (`<-shutdownDone`) after `ListenAndServe()` returns, so `db.Close()` only runs once `Shutdown()` has actually completed.
  - `[medium]` `[patch]` `backend/cmd/seed-admin/main.go`'s SQLSTATE-23505-to-`errAdminAlreadyExists` mapping (the code path added in the previous review pass's admin-race fix, guarding `pqErr.Constraint == "idx_usuarios_unico_adm"`) had zero test coverage — the existing `TestRunMigrations_UniqueAdminIndex` only proves the DB constraint itself via a raw SQL insert, and `TestSeedAdmin_Duplicado` only calls `seedAdmin` sequentially (hitting the `sql.ErrNoRows` branch, never the `pq.Error` branch), so a regression in the constraint-name string or the `errors.As` type assertion would go undetected. Added `TestSeedAdmin_Concorrente` in `backend/cmd/seed-admin/main_test.go`, which starts two goroutines calling `seedAdmin` concurrently (synchronized on a shared start channel to maximize the race window) and asserts exactly one succeeds and the other's error satisfies `errors.Is(err, errAdminAlreadyExists)`, and that only one `adm` row exists afterward.
  - `[reject]` 22 findings from the four review layers were routed to reject: three duplicated already-deferred items in this spec's `deferred:` list with identical root cause and no new evidence (no CI running integration tests against real Postgres — DW-1; `--senha` plaintext CLI flag — DW-2; missing README for stack/migrations/seed-admin invocation — DW-3, raised independently by more than one reviewer); several were explicitly out of scope per the intent itself (password-strength/complexity validation, explicitly deferred to Story 1.10 per the I/O Matrix); two were factually incorrect against the actual repository state (claimed `backend/go.sum` was missing — it exists, 66 lines; claimed `backend/main_test.go` doesn't exist — it does, 174 lines, both reviewers were working from an incomplete diff excerpt); the rest were speculative/premature (panic-recovery middleware, CORS, Go/Alpine version doubts with no supporting evidence, `atualizado_em` column, compose restart policy) or genuinely cosmetic with no functional or security consequence (email-format validation not required by any AC, `senha_hash` nullability matches the spec's own Design Notes DDL verbatim, `defer db.Close()` after `os.Exit()` is a harmless no-op, `.env.example`/`docker-compose.yml` DSN duplication, a same-email concurrent-seed race whose user-facing message would degrade to a raw Postgres error but whose data integrity is unaffected by the existing `idx_usuarios_email_lower` constraint either way).

### 2026-08-29 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 2: (high 0, medium 2, low 0)
- defer: 3: (high 0, medium 0, low 3)
- reject: 14: (high 0, medium 2, low 12)
- addressed_findings:
  - `[medium]` `[patch]` `healthHandler`'s error branch (503 + `{"status":"unhealthy"}` when `db.PingContext` fails) had zero test coverage — the only existing test (`TestHealthHandler`) only exercises the healthy path, so a regression that always returned 200 would go undetected even though the docker-compose healthcheck (`wget --spider` against `/api/health`) depends on this branch to correctly report the container unhealthy when the DB is down. Added `TestHealthHandler_Unhealthy` in `backend/main_test.go`, which closes the test DB before invoking the handler and asserts `503` + `{"status":"unhealthy"}`.
  - `[medium]` `[patch]` the whitespace-only `--senha` rejection fixed in the previous review pass lives entirely inside `main()`'s flag validation, but every existing test calls `seedAdmin` directly, bypassing `main()` — a regression reverting that fix (e.g. back to unstrimmed `*senha == ""`) would pass the full suite undetected. Extracted the check into a testable `validateFlags(nome, email, senha string) error` function (called from `main()` the same way) and added `TestValidateFlags` in `backend/cmd/seed-admin/main_test.go`, covering empty and whitespace-only `--nome`/`--email`/`--senha`.
  - `[reject]` 14 findings were routed to reject: two were factually incorrect (claimed `backend/go.sum` and `backend/main_test.go` were missing — both exist, verified with `wc -l`; the same two false claims were already rejected in the previous pass, this time caused by an incomplete diff excerpt handed to one reviewer rather than the actual repository state); five duplicated already-deferred items with no new evidence (no CI running integration tests — DW-1; `--senha` plaintext CLI flag — DW-2; missing README — DW-3); five duplicated findings already explicitly rejected in the previous pass with identical root cause (Go/Alpine version doubts, `senha_hash` nullability / missing `atualizado_em` matching the spec's own Design Notes DDL, no email-format validation, `.env.example`/`docker-compose.yml` DSN duplication, the same-email concurrent-seed race degrading to a raw Postgres error with no data-integrity impact, `defer db.Close()` after `os.Exit()` being a harmless no-op — this last one raised again independently for both `backend/main.go` and `backend/cmd/seed-admin/main.go`, deduplicated to one reject); the rest were out of scope for a foundation story (Docker image digest pinning vs. the spine's own tag-based convention, a `Makefile`/helper-scripts request that is the same "no operator docs" gap as DW-3, and a single liveness/readiness health endpoint distinction with no orchestrator in scope to consume it).

## Design Notes

DDL de referência para `usuarios` (Design Note, não Verification -- a migration real é a fonte da verdade):

```sql
CREATE TABLE usuarios (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  nome VARCHAR(255) NOT NULL,
  email VARCHAR(255) NOT NULL,
  senha_hash TEXT,
  papel VARCHAR(20) NOT NULL CHECK (papel IN ('usuario','almoxarife','gestor','adm')),
  email_verificado BOOLEAN NOT NULL DEFAULT false,
  ativo BOOLEAN NOT NULL DEFAULT true,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_usuarios_email_lower ON usuarios (lower(email));
```

`papel` usa `VARCHAR + CHECK` em vez de `ENUM` nativo do Postgres -- evita a fricção de `ALTER TYPE ADD VALUE` em migrations futuras, mantendo a mesma garantia de valores fechados.

Conta semeada por `seed-admin` sai com `email_verificado=true` e `ativo=true`: é o único ponto de entrada do sistema, então precisa estar imediatamente utilizável para login assim que a Story 1.4 existir -- não faz sentido depender do fluxo de verificação por e-mail (Story 1.3) que ainda não existe.

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: build limpo, sem erros.
- `cd backend && go vet ./...` -- expected: sem warnings.
- `docker compose up -d db && cd backend && go test -p 1 ./...` -- expected: todos os testes de integração passam (migrations aplicam, seed cria adm, segundo seed falha sem alterar dado). `-p 1` é obrigatório: os pacotes `backend` e `backend/cmd/seed-admin` compartilham a mesma tabela `usuarios` ao vivo no Postgres do `docker compose`, e `go test ./...` roda pacotes diferentes como processos concorrentes por padrão -- sem serializar, um pacote pode truncar linhas que o outro acabou de inserir e ainda vai verificar.
- `docker compose up --build` -- expected: `api` sobe saudável, `GET /api/health` responde 200.

## Auto Run Result

**Resumo da mudança implementada:** este ciclo é uma passagem de revisão sobre a story já implementada (fundação do backend Go: migrations síncronas no startup, tabela `usuarios`, CLI `cmd/seed-admin` para o primeiro Adm, `main.go` com `GET /api/health`, Dockerfile e docker-compose). Nenhuma linha de código relativa aos ACs foi alterada nesta passagem — apenas dois gaps de cobertura de teste foram corrigidos.

**Arquivos alterados nesta passagem:**
- `backend/main_test.go` -- import `encoding/json` adicionado; novo teste `TestHealthHandler_Unhealthy`, provando o ramo de erro (503 + `{"status":"unhealthy"}`) de `healthHandler` (código de `healthHandler` em `backend/main.go` não foi alterado, só coberto).
- `backend/cmd/seed-admin/main.go` -- extraída a validação de flags obrigatórias para uma função nomeada `validateFlags(nome, email, senha string) error`, chamada por `main()` no lugar da checagem inline.
- `backend/cmd/seed-admin/main_test.go` -- novo teste `TestValidateFlags`, cobrindo nome/e-mail/senha vazios e senha somente com espaços.
- `_bmad-output/implementation-artifacts/spec-1-1-bootstrap-do-primeiro-adm-e-fundacao-do-backend.md` -- `status`, `deferred` (3 itens novos) e `## Review Triage Log` atualizados por esta passagem.

**Resultado da revisão:** intent_gap 0, bad_spec 0, patch 2 (medium 2), defer 3 (low 3), reject 14 (medium 2, low 12). Detalhe completo em `## Review Triage Log` — passagem de 2026-08-29.

**Recomendação de revisão de acompanhamento:** `true` — os 2 achados corrigidos nesta passagem são `medium`; `3 × 2 (medium) + 1 × 0 (low) = 6 ≥ 5`.

**Verificação executada:**
- `cd backend && go build ./...` -- OK, build limpo.
- `cd backend && go vet ./...` -- OK, sem warnings.
- `go test -p 1 ./...` sem `DATABASE_URL` -- OK: todos os testes de integração pulam (`--- SKIP`) de forma limpa; os testes puramente unitários (`TestNormalizeEmail`, `TestValidateFlags`) passam.
- `go test -p 1 -v ./...` com `DATABASE_URL` apontando para um Postgres 16 local (docker não estava disponível neste sandbox — usado um cluster Postgres já em execução no host, com um banco descartável `stockflow_test` criado e removido só para esta verificação) -- OK: todos os 11 testes passam, incluindo os dois novos (`TestHealthHandler_Unhealthy`, `TestValidateFlags`).
- `docker compose up --build` -- **não executado neste sandbox** (binário `docker` indisponível); mesma limitação de ambiente já registrada em DW-1.

**Riscos residuais:** nenhum novo além dos já registrados em `deferred` (6 itens: CI ausente para testes de integração, senha em texto plano na flag `--senha`, ausência de README, DB calls sem timeout de contexto, ordenação do shutdown gracioso sem teste sob sinal real, tamanho de `--nome`/`--email` não validado contra `VARCHAR(255)`).

