### DW-1: Não há pipeline de CI que efetivamente rode os testes de integração contra um Postgres real; sem DATABASE_URL definido, `go test` reporta PASS mesmo pulando (`t.Skip`) toda a cobertura da CHECK constr
origin: spec-deferred faa51d58447a
location: .github/workflows (inexistente)
source_spec: `spec-1-1-bootstrap-do-primeiro-adm-e-fundacao-do-backend.md`
severity: medium
reason: Confirmado rodando `cd backend && unset DATABASE_URL && go test ./... -v`: todos os testes com dependência de banco imprimem `--- SKIP` e o pacote ainda reporta PASS. Não existe `.github/workflows`, `Makefile` nem script no repo que suba um Postgres e exporte DATABASE_URL antes de `go test`. Configurar CI/CD é um item de AD-16 (envelope operacional de todo o projeto), não uma AC desta story.
status: open

### DW-2: `cmd/seed-admin` recebe a senha do primeiro Adm via flag `--senha` em texto plano, visível no histórico do shell e em `ps`/`/proc` de outros usuários locais.
origin: spec-deferred 104c865dc04c
location: backend/cmd/seed-admin/main.go:31
source_spec: `spec-1-1-bootstrap-do-primeiro-adm-e-fundacao-do-backend.md`
severity: medium
reason: `backend/cmd/seed-admin/main.go` usa `flag.String("senha", ...)`. Uma alternativa mais segura (prompt interativo com eco desligado, ou leitura via stdin/variável de ambiente) exigiria uma decisão de UX/segurança fora do escopo desta story, cujo Code Map já especificava as três flags `--nome/--email/--senha`.
status: open

### DW-3: Não há README (ou doc equivalente) explicando como subir o stack, aplicar migrations ou invocar `seed-admin` — inclusive a partir da imagem Docker, cujo `ENTRYPOINT` fixo (`./api`) exige saber sobresc
origin: spec-deferred 561082372671
location: backend/ (sem README)
source_spec: `spec-1-1-bootstrap-do-primeiro-adm-e-fundacao-do-backend.md`
severity: low
reason: Nenhum `README.md` em `backend/` ou na raiz cobre esses passos; a única documentação está nos comentários do próprio código-fonte.
status: open

### DW-4: Chamadas ao banco em `seedAdmin`, `runMigrations` e `healthHandler` não usam `context.WithTimeout`/`context.WithDeadline` — uma conexão travada após um `Ping` bem-sucedido bloqueia indefinidamente em
origin: spec-deferred 1bd992d5ba95
location: backend/cmd/seed-admin/main.go:112; backend/main.go (runMigrations)
source_spec: `spec-1-1-bootstrap-do-primeiro-adm-e-fundacao-do-backend.md`
severity: low
reason: `backend/cmd/seed-admin/main.go` usa `db.QueryRow(query, ...)` sem contexto; `backend/main.go`'s `runMigrations` chama `m.Up()` sem deadline. `healthHandler` já usa `PingContext(r.Context())`, mas as demais chamadas não têm proteção equivalente. Adicionar timeouts exigiria re-plumbing de assinaturas fora do escopo mínimo desta story.
status: open

### DW-5: A correção do shutdown gracioso (aguardar `shutdownDone` antes de `db.Close()`, feita na revisão anterior desta story) não tem nenhum teste que prove a ordenação sob um SIGTERM real com requisição em
origin: spec-deferred 02391f84c353
location: backend/main.go (shutdownDone)
source_spec: `spec-1-1-bootstrap-do-primeiro-adm-e-fundacao-do-backend.md`
severity: low
reason: Busca por `SIGTERM` e `Shutdown(` no repo mostra ocorrências só dentro de `backend/main.go` (linhas do próprio handler de sinal); nenhum teste invoca `main()`, envia sinal, ou observa a ordem entre o fechamento do listener e o fechamento do pool. Revertendo o `<-shutdownDone` para o comportamento anterior ao patch, nenhum teste existente falharia. Testar isso de forma não-flaky exigiria subir o servidor real e orquestrar sinais/requisições concorrentes — não é uma correção trivial de uma revisão automatizada.
status: open

### DW-6: `cmd/seed-admin` não valida o tamanho de `--nome`/`--email` contra o limite `VARCHAR(255)` das colunas `usuarios.nome`/`usuarios.email` antes do INSERT — um valor maior surge como um erro cru do Postg
origin: spec-deferred f3611e928d0f
location: backend/cmd/seed-admin/main.go:97 (seedAdmin)
source_spec: `spec-1-1-bootstrap-do-primeiro-adm-e-fundacao-do-backend.md`
severity: low
reason: `backend/migrations/000001_create_usuarios.up.sql` declara `nome VARCHAR(255)` e `email VARCHAR(255)`; `seedAdmin` em `backend/cmd/seed-admin/main.go` só faz `strings.TrimSpace` e `normalizeEmail`, sem checagem de comprimento. Não há AC que exija essa validação — string/UX é decisão fora do escopo mínimo desta story.
status: open

### DW-7: Follow-up review still recommended for 1-1-bootstrap-do-primeiro-adm-e-fundação-do-backend after the damping cap was spent
origin: review-budget-followup
location: n/a
source_spec: `spec-1-1-bootstrap-do-primeiro-adm-e-fundacao-do-backend.md`
severity: low
reason: The follow-up-review damping cap (limits.max_followup_reviews = 1) was spent with the story finalized (status: done, verify green) while the review pass still recommended an independent follow-up. The work was committed by bmad-loop run 20260829-150733-63a0; this entry preserves the lingering recommendation for a deliberate later review.
status: open
