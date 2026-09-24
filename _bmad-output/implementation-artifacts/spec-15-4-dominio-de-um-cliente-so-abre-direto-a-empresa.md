---
title: 'Story 15.4: Domínio de um cliente só abre direto a Empresa'
type: 'feature'
created: '2026-09-24'
status: 'awaiting-operator'
baseline_revision: '16e790975497bb1ab9f59c4fc4363bb8fe800b54'
review_loop_iteration: 0
followup_review_recommended: true
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-15-context.md'
warnings: [oversized]
deferred: []
operator_actions:
  - "Depois do deploy, acrescentar EMPRESA_PADRAO=ferreira-costa ao /opt/apps/stockflow/cliente-aws/.env do servidor de suprimentos.fcxlabs.com."
  - "Recriar o container da API nesse servidor com `docker compose up -d api` em /opt/apps/stockflow/cliente-aws (um `docker compose restart` não relê o .env)."
  - "Conferir que https://suprimentos.fcxlabs.com/api/entrada responde {\"empresaPadrao\":\"ferreira-costa\"} e que abrir a raiz do domínio cai em /e/ferreira-costa/; se o log da API avisar que EMPRESA_PADRAO não corresponde a uma Empresa ativa, corrigir o slug."
---

<intent-contract>

## Intent

**Problem:** Em `suprimentos.fcxlabs.com` só existe a Ferreira Costa, mas a raiz do domínio mostra o login pela conta (Story 15.2). O colaborador não cai direto no login da sua Empresa, onde ficam o SSO e o visual dela.

**Approach:** Uma variável de backend opcional, `EMPRESA_PADRAO` (slug), e uma rota pública `GET /api/entrada` que devolve só `{empresaPadrao: slug | null}`. A app `sem-empresa` consulta essa rota antes de montar a tela. Com slug, faz `location.replace('/e/{slug}/')`. Sem slug, ou com qualquer falha, mostra o login pela conta de hoje. A variável entra no compose do cliente AWS e é documentada como opcional.

## Boundaries & Constraints

**Always:**
- `GET /api/entrada` fica fora do prefixo `/e/{slug}`, sem `RequireEmpresa` e sem `RequireAuth`, como as rotas `/api/auth/entrar*`. Corpo sempre exatamente `{"empresaPadrao":"<slug>"}` ou `{"empresaPadrao":null}`, sem outro campo. Cabeçalho `Cache-Control: no-store`.
- `empresaPadrao` só vem preenchido se `strings.TrimSpace(EMPRESA_PADRAO)` resolve por `services.BuscarEmpresaPorSlug` (slug canônico, existente, `ativa`). O slug devolvido é o da Empresa encontrada no banco. Vazia, ausente, fora da forma canônica, inexistente ou `inativa` → `null`. Erro de banco → `500 INTERNAL_ERROR` (o frontend trata como `null`).
- A variável é lida uma vez, ao montar o mux (`newMux`), sem mudar a assinatura de `newMux`. A Empresa é consultada a cada requisição, para refletir desativação sem reiniciar.
- Frontend: o redirecionamento vale para toda a app `sem-empresa` (qualquer caminho fora de `/e/{empresa}` e `/plataforma`). Usa `fetch('/api/entrada')` (nunca `apiUrl`) e só redireciona se o slug tiver a forma canônica (`slugDaURL('/e/{slug}/') === slug`). Rede, timeout (3 s), não-`2xx`, JSON inválido ou `null` → renderiza `SemEmpresaPage`, sem mensagem de erro.
- `installer/cliente-aws/docker-compose.yml`: `- EMPRESA_PADRAO=${EMPRESA_PADRAO}` na lista do `api`, com comentário. `.env.example`: `EMPRESA_PADRAO=` vazia e comentada como opcional, só para servidor de um cliente só, nunca em `stockflow.fbtechia.com`.

**Block If:**
- Nenhum previsto.

**Never:**
- Acrescentar `EMPRESA_PADRAO` ao `docker-compose.yml` da raiz (Coolify, `stockflow.fbtechia.com`). Lá ela não é definida.
- Devolver nome, id, status ou outro dado da Empresa. Diferenciar por status ou corpo "variável ausente" de "slug inexistente/desativado".
- Mudar `/api/auth/entrar*`, `/api/auth/esqueci-senha`, rotas `/e/{slug}`, SSO ou a tela `SemEmpresaPage`.
- Mexer no `.env` do servidor, `git push` (todo push em `main` faz deploy em produção).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Padrão válido | `EMPRESA_PADRAO=acme`, `acme` ativa | 200 `{"empresaPadrao":"acme"}`; front faz `location.replace('/e/acme/')` | — |
| Com espaços | `EMPRESA_PADRAO=" acme "` | 200 `{"empresaPadrao":"acme"}` | — |
| Sem variável / vazia | ausente ou `""` | 200 `{"empresaPadrao":null}`; front mostra o login pela conta | — |
| Slug inexistente / fora da forma | `nao-existe`, `ACME/../x` | 200 `{"empresaPadrao":null}` | — |
| Empresa desativada | `acme` com `status='inativa'` | 200 `{"empresaPadrao":null}` | — |
| Banco fora | query falha | 500 `INTERNAL_ERROR`; front mostra o login pela conta | log `slog.Error` |
| Front: falha | rede, timeout, 500, JSON sem campo, slug não canônico | renderiza `SemEmpresaPage`, sem `replace` | — |

</intent-contract>

## Code Map

- `backend/services/entrada.go` -- novo `EmpresaPadrao(db, slug string) (string, error)`: trim; vazio → `"", nil`; `BuscarEmpresaPorSlug` (`services/empresas.go:231`, já colapsa não canônico/inexistente/inativa em `ErrEmpresaNaoEncontrada`) → `"", nil`; outro erro → propaga; achada → `e.Slug`.
- `backend/handlers/entrada.go` -- novo `EntradaHandler(db *sql.DB, empresaPadrao string) http.HandlerFunc`. Resposta com struct `{EmpresaPadrao *string \`json:"empresaPadrao"\`}` (nil → `null`). Reusa `escreverErro`/padrão de JSON do arquivo. Atualizar o comentário de cabeçalho do arquivo (Story 15.4).
- `backend/main.go:321-339` -- em `newMux`, `empresaPadrao := os.Getenv("EMPRESA_PADRAO")` e `mux.HandleFunc("GET /api/entrada", handlers.EntradaHandler(db, empresaPadrao))` junto das rotas da raiz; incluir a rota na lista de exceções do comentário de `requireEmpresa` (:309-315).
- `backend/main_test.go:3669+` -- bloco da Story 15.2/15.3 pelo mux real (`provisionarEmpresaM49`, `slugEntradaMux`). Modelo para testar `GET /api/entrada` pelo `newMux` com `t.Setenv("EMPRESA_PADRAO", ...)` antes de montar.
- `backend/handlers/entrada_test.go` -- helpers `prepararEntradaH`, `provisionarEmpresaEntradaH`, `muxEntrada`. `backend/services/entrada_test.go` -- helpers equivalentes. Para desativar Empresa em teste: `UPDATE empresas SET status='inativa'`. Banco: `stockflow_t91`, `go test -p 1`.
- `frontend/src/lib/entrada.ts` -- nova `buscarEmpresaPadrao(): Promise<string | null>` (fetch com `AbortController` de 3 s; valida com `slugDaURL` de `@/lib/api`) e `abrirEmpresaPadrao(): Promise<boolean>` (chama a anterior; com slug, `window.location.replace(\`/e/${slug}/\`)` e `true`). Atualizar o doc de `escolherApp`.
- `frontend/src/main.tsx:28-31` -- no `default`, `if (await abrirEmpresaPadrao()) return null;` antes de importar `SemEmpresaPage`. Atualizar o comentário.
- `frontend/src/lib/entrada.test.ts` -- testes existentes da lib; estilo de mock de `fetch` com `vi.stubGlobal`.
- `installer/cliente-aws/docker-compose.yml:28-47` -- lista `environment` do `api`.
- `.env.example` -- seção nova no fim, no estilo dos blocos existentes.
- `docker-compose.yml` (raiz) -- somente leitura: não recebe a variável.

## Tasks & Acceptance

**Execution:**
- `backend/services/entrada.go` -- `EmpresaPadrao` -- regra única de "slug válido e ativo".
- `backend/handlers/entrada.go`, `backend/main.go` -- `EntradaHandler` e registro fora de `RequireEmpresa`.
- `backend/services/entrada_test.go`, `backend/handlers/entrada_test.go`, `backend/main_test.go` -- cada linha backend da I/O Matrix; handler conferindo o corpo exato (`{"empresaPadrao":null}` byte a byte para ausente, inexistente e inativa), `Cache-Control: no-store` e 500 com banco fechado; mux real com `t.Setenv` provando a rota sem prefixo e sem sessão.
- `frontend/src/lib/entrada.ts`, `frontend/src/main.tsx` -- consulta e redirecionamento antes de montar a app.
- `frontend/src/lib/entrada.test.ts` -- `buscarEmpresaPadrao` (URL `/api/entrada`; slug → slug; `null`, 500, rede, JSON inválido, slug não canônico → `null`) e `abrirEmpresaPadrao` (`replace('/e/acme/')` só com slug).
- `installer/cliente-aws/docker-compose.yml`, `.env.example` -- variável e documentação.

**Acceptance Criteria:**
- Given o backend com `EMPRESA_PADRAO=ferreira-costa` e essa Empresa ativa, when alguém abre a raiz do domínio, then `GET /api/entrada` devolve `{"empresaPadrao":"ferreira-costa"}` e o frontend faz `location.replace('/e/ferreira-costa/')`, sem montar a tela de login pela conta.
- Given `GET /api/entrada` chamado sem cookie nem token, when responde, then o corpo tem só a chave `empresaPadrao`.
- Given `installer/cliente-aws/docker-compose.yml`, when esta story entra, then o `api` recebe `EMPRESA_PADRAO=${EMPRESA_PADRAO}`, e o `docker-compose.yml` da raiz continua sem ela.
- Given `.env.example`, when esta story entra, then `EMPRESA_PADRAO` aparece vazia, documentada como opcional, só para servidores de um cliente só e nunca em `stockflow.fbtechia.com`.

## Review Triage Log

### 2026-09-24 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 6 (high 0, medium 2, low 4)
- defer: 0
- reject: 14 (high 0, medium 2, low 12)
- addressed_findings:
  - `[medium]` `[patch]` Fora da raiz, o redirecionamento jogava fora o caminho, a query e o hash. O retorno do SSO registrado na raiz (`/auth/callback?code=...`, exemplo de `IAM_REDIRECT_URI` no `.env.template`) perdia o `code`. `abrirEmpresaPadrao` agora leva `pathname + search + hash` para dentro de `/e/{slug}` (`/` continua virando `/e/{slug}/`). Teste novo.
  - `[medium]` `[patch]` A decisão em `main.tsx` não tinha teste, e com redirecionamento ainda montava uma raiz React vazia (contra o próprio comentário). A decisão foi para `decidirEntrada(pathname)` em `lib/entrada.ts`, testada (redirecionado, sem-empresa, Empresa e Plataforma nunca consultam `/api/entrada`). `main.tsx` não chama `createRoot` quando redireciona.
  - `[low]` `[patch]` `EMPRESA_PADRAO` errada falhava calada. `main()` confere o valor ao subir e registra `slog.Warn` se não resolve para Empresa ativa.
  - `[low]` `[patch]` Comentário de `frontend/nginx.conf` dizia que `location /api/` servia só `/api/health`; agora lista as rotas da raiz.
  - `[low]` `[patch]` `installer/cliente-aws/.env.template` (o `.env` do servidor) ganhou `EMPRESA_PADRAO=` documentada.
  - `[low]` `[patch]` A instrução "reinicie o container" era insuficiente: `docker compose restart` não relê o `.env`. `.env.example`, `.env.template` e o compose do cliente agora dizem `docker compose up -d api`.

## Design Notes

Não há documento de implantação separado no repositório. A documentação de implantação são os comentários de `installer/cliente-aws/docker-compose.yml` (sincronizado para o servidor a cada deploy) e o `.env.example`. As duas recebem o texto.

O redirecionamento vale para qualquer caminho da app `sem-empresa`, não só `/`. No contexto do épico, "a raiz" é essa app inteira ("vale para qualquer caminho fora de `/e/{empresa}` e `/plataforma`"). Um link antigo como `/login` também cai no login da Empresa.

O redirecionamento mantém o resto do caminho, a query e o hash (revisão): `/` vira `/e/{slug}/`, `/login` vira `/e/{slug}/login` e `/auth/callback?code=...` vira `/e/{slug}/auth/callback?code=...`.

Validar o slug de novo no frontend impede que um corpo inesperado vire um redirecionamento para caminho arbitrário.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros, sem arquivos listados
- `cd backend && DATABASE_URL='postgres://stockflow:stockflow@127.0.0.1:5432/stockflow_t91?sslmode=disable' go test -p 1 -count=1 -timeout 25m ./...` -- expected: verde (não rodar junto com o vitest)
- `cd frontend && npx tsc --noEmit -p tsconfig.app.json && npx tsc -b && npx vitest run` -- expected: verde
- `docker compose -f installer/cliente-aws/docker-compose.yml config -q` (com variáveis fictícias) -- expected: válido

## Auto Run Result

**Resumo:** num servidor de um cliente só, a raiz do domínio abre direto a Empresa. A variável opcional `EMPRESA_PADRAO` (slug) é lida ao montar o mux. `GET /api/entrada`, público, fora de `/e/{slug}` e de `RequireEmpresa`, devolve só `{"empresaPadrao": slug | null}` com `Cache-Control: no-store`. `null` para variável vazia/ausente, slug fora da forma, inexistente ou Empresa desativada; `500` só em erro de banco. O frontend consulta a rota antes de montar a app `sem-empresa` (timeout de 3 s) e, com slug válido, faz `location.replace('/e/{slug}/...')` sem montar nada. Qualquer falha mostra o login pela conta da Story 15.2. A variável entrou no compose do cliente AWS e está documentada; o `docker-compose.yml` da raiz (Coolify, `stockflow.fbtechia.com`) continua sem ela.

**Arquivos:**
- `backend/services/entrada.go`: `EmpresaPadrao(db, slug)`, regra única de slug válido e ativo.
- `backend/handlers/entrada.go`: `EntradaHandler` (`200` com o corpo exato, `500`).
- `backend/main.go`: rota `GET /api/entrada` fora de `RequireEmpresa`; aviso no log ao subir se `EMPRESA_PADRAO` não resolve.
- `backend/services/entrada_test.go`, `backend/handlers/entrada_test.go`, `backend/main_test.go`: I/O Matrix, corpo byte a byte, `no-store`, 500 com banco fechado, mux real com `t.Setenv` sem sessão e desativação sem remontar o mux.
- `frontend/src/lib/entrada.ts` (+ test): `buscarEmpresaPadrao`, `abrirEmpresaPadrao` (mantém caminho, query e hash) e `decidirEntrada`.
- `frontend/src/main.tsx`: usa `decidirEntrada`; não monta nada quando redireciona.
- `installer/cliente-aws/docker-compose.yml`, `installer/cliente-aws/.env.template`, `.env.example`, `frontend/nginx.conf`: variável e documentação.

**Review:** 4 camadas (blind, edge-case, verification-gap, intent-alignment). 6 patches (2 medium, 4 low), 0 adiados, 14 rejeitados. Entre os rejeitados:
- `${EMPRESA_PADRAO:-}` no compose para evitar o aviso de variável não definida (o AC pede a forma literal `${EMPRESA_PADRAO}`, e o `.env.template` agora a define vazia);
- tela em branco até 3 s na raiz de servidor sem padrão (limite de tempo curto, rota leve);
- rate limit/cache na rota pública (mesma natureza de `/api/health`);
- testes que deixam a Empresa `inativa` (os helpers recriam as Empresas a cada teste);
- testes de handler extras (maiúsculas, 405, cookie inválido), CNPJ fixo em teste, regra de slug duplicada entre front e back;
- escapar do redirecionamento num servidor de um cliente só (fora do escopo da story).

**Recomendação de nova revisão:** `true`. Patches: high 0, medium 2, low 4. Pontuação 3×2 + 4 = 10 (≥ 5).

**Verificação:**
- `go build ./...`, `go vet ./...`, `gofmt -l .`: limpos.
- `go test -p 1 -count=1` contra `stockflow_t91`: suíte inteira verde na implementação; depois dos patches (só `main.go` mudou no backend), pacote raiz `ok` de novo; `handlers` e `services` (filtro `EmpresaPadrao|Entrada|Entrar|Redefinicao`) `ok`.
- `npx tsc --noEmit -p tsconfig.app.json`, `npx tsc -b`, `npx vitest run`: 61 arquivos, 849 testes, verdes.
- `docker compose config -q` não rodou (sem Docker neste ambiente). O compose do cliente foi lido como YAML e o `api` recebe `EMPRESA_PADRAO=${EMPRESA_PADRAO}`.

**Riscos residuais:**
- Enquanto o operador não definir a variável em `suprimentos.fcxlabs.com`, a raiz mostra o login pela conta (que funciona).
- Num servidor sem padrão, cada abertura da raiz espera `GET /api/entrada` (normalmente milissegundos; no máximo 3 s) antes de desenhar a tela.
- O compose do cliente avisa "variable is not set" enquanto o `.env` do servidor não tiver a linha.
- Nada foi publicado: todo push em `main` faz deploy em produção.

**Ações do operador:** ver `operator_actions` no frontmatter.
