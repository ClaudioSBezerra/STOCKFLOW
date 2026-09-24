---
title: 'Story 15.2: Login na raiz do domínio pela conta'
type: 'feature'
created: '2026-09-24'
status: 'done'
baseline_revision: '680f447302767bdd0914b184f753adc553477072'
review_loop_iteration: 0
followup_review_recommended: true
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-15-context.md'
warnings: [oversized]
deferred: []
---

<intent-contract>

## Intent

**Problem:** Hoje só se entra pelo endereço da Empresa (`/e/{slug}`); a raiz do domínio (app `sem-empresa`) mostra só uma página explicativa. Com o e-mail único entre Empresas reais (Story 15.1), o e-mail já identifica a Empresa, mas não há como entrar sem saber o slug.

**Approach:** Duas rotas novas fora do prefixo `/e/{slug}` e sem `RequireEmpresa` — `POST /api/auth/entrar` e `POST /api/auth/entrar/escolha` (AD-36) — que acham as contas do e-mail em Empresas ativas, chamam o `services.Login` atual para cada uma e, conforme o resultado, entram direto (cookie de refresh com `Path=/e/{slug}/api/auth`), pedem o MFA da Empresa ou devolvem a escolha real/Treinamento com um `escolhaToken` de uso único. A `SemEmpresaPage` vira a tela de login pela conta.

## Boundaries & Constraints

**Always:**
- As regras de conta são SEMPRE as de `services.Login(db, empresaID, email, senha)`, chamado uma vez por conta encontrada — bloqueio/contagem de falhas, desativada, e-mail não confirmado, só-SSO. Nenhuma regra de credencial reimplementada.
- Só contas de Empresas com `status = 'ativa'` entram na busca. Sem conta nenhuma → `bcrypt.CompareHashAndPassword(dummyBcryptHash, ...)` e `401 INVALID_CREDENTIALS` "E-mail ou senha inválidos." (mesmo corpo do login de hoje).
- Agregação: ≥1 conta aceita vence; nenhuma aceita e alguma `ErrContaBloqueada` → `429 ACCOUNT_LOCKED` (mesma mensagem do `LoginHandler`); senão `401 INVALID_CREDENTIALS`. E-mail/senha em branco → `400 VALIDATION_ERROR` (sem log). Payload inválido/grande → `400`, com `authRequestMaxBytes`.
- Respostas `200`: uma conta sem MFA → `{slug}` + cookie `refresh_token` (HttpOnly, SameSite=Lax, Secure condicional, `Path=/e/{slug}/api/auth`, via `services.EmitirSessao(..., "senha")`); uma conta com MFA → `{slug, mfaRequerido:true, mfaToken}` (via `services.IniciarLoginMFA`, sem cookie); duas contas → `{escolha:[{slug, nomeFantasia, treinamento}], escolhaToken}` (real primeiro), sem cookie.
- `escolhaToken`: `tokens_acao` com `tipo = 'escolha_empresa'`, 5 min, uso único, preso aos ids das contas conferidas. `entrar/escolha {escolhaToken, slug}` consome o token atomicamente (UPDATE ... `usado_em IS NULL AND expira_em > now()` RETURNING) e só aceita o slug de uma dessas contas, em Empresa ainda ativa e conta ainda `ativo`/não bloqueada; senão `401 ESCOLHA_INVALIDA` "A escolha expirou. Faça login novamente.". Depois segue exatamente o ramo "uma conta" (MFA ou sessão).
- `logs_acesso`: uma linha por conta avaliada, na Empresa dela, método `senha`, `sucesso` = senha aceita naquela conta (`usuario_id` preenchido só no sucesso, como hoje). Sem conta → nenhuma linha.
- MFA: o código continua sendo verificado no `POST /e/{slug}/api/auth/mfa/verificar` atual, na tela de login da Empresa (`/e/{slug}/login`).
- Frontend da raiz não monta `AuthProvider`: chama as rotas da raiz com `fetch('/api/auth/...')` (nunca `apiUrl`) e navega com `window.location.assign`.

**Block If:**
- Nenhum previsto (decisões fixadas em AD-36 e nesta spec).

**Never:**
- `POST /api/auth/esqueci-senha` da raiz (Story 15.3), `GET /api/entrada`/`EMPRESA_PADRAO` (Story 15.4), SSO na raiz, troca de Empresa sem sair.
- Coluna/flag de Treinamento em `usuarios` ou `if treinamento` em regra de login; o `treinamento` da resposta vem só de `empresas.empresa_origem_id IS NOT NULL`, como em `usuarioRespostaDe`.
- Aceitar `empresa_id` de body/query; revelar se o e-mail existe ou em qual Empresa antes de a senha conferir.
- Mudar o comportamento de `/e/{slug}/api/auth/*`, convites ou SSO.
- `git push` (todo push em `main` faz deploy em produção).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Uma conta, sem MFA | conta em `acme`, senha certa | 200 `{slug:"acme"}`, cookie com `Path=/e/acme/api/auth`; `POST /e/acme/api/auth/refresh` com ele → 200 | — |
| Uma conta, com MFA | `mfa_habilitado` | 200 `{slug, mfaRequerido:true, mfaToken}`, sem cookie; o token funciona em `/e/acme/api/auth/mfa/verificar` | — |
| Real + Treinamento, senha certa nas duas | contas em `acme` e `acme-treinamento` | 200 `{escolha:[acme, acme-treinamento], escolhaToken}`, sem cookie | — |
| Escolha válida | `{escolhaToken, slug:"acme-treinamento"}` | 200 como "uma conta" no Treinamento | — |
| Escolha reusada / vencida / slug fora das contas | segundo uso, `expira_em` passado, `slug:"outra"` | 401 `ESCOLHA_INVALIDA`, sem cookie | token consumido no primeiro uso |
| Senha certa só numa | real com senha A, Treinamento com senha B | entra direto na que conferiu; a outra conta NÃO registra falha nem linha em `logs_acesso` (como no login pelo endereço da Empresa) | — |
| E-mail sem conta / senha errada | — | 401 `INVALID_CREDENTIALS`, corpo idêntico nos dois | bcrypt dummy sem conta |
| Bloqueio | 5 senhas erradas | cada conta do e-mail bloqueada; a 6ª → 429 `ACCOUNT_LOCKED` (e `/e/{slug}/api/auth/login` também 429) | — |
| Conta desativada / não confirmada / só-SSO | senha certa | 401 `INVALID_CREDENTIALS` | vem de `services.Login` |
| Empresa desativada | conta em Empresa `inativa` | 401 `INVALID_CREDENTIALS` | busca só Empresas ativas |

</intent-contract>

## Code Map

- `backend/services/auth.go:502-606` -- `Login` (reusar, não mudar); `dummyBcryptHash` (:445); `normalizeEmail` (:158); `gerarTokenAcao` (:166); `IniciarLoginMFA` (:758) é o molde de token tipado; `mfaLoginTokenExpiracao` (:57) → nova const `escolhaEmpresaTokenExpiracao = 5 * time.Minute`; `EmitirSessao` (:662) não depende de Empresa.
- `backend/services/entrada.go` (novo) -- `ContaEntrada{UsuarioID, EmpresaID, Slug, NomeFantasia string; Treinamento bool}`, `EntrarPelaConta(db, email, senha) (avaliadas, aceitas []ContaEntrada, err error)`, `IniciarEscolhaEmpresa(db, aceitas) (token, error)`, `ConcluirEscolhaEmpresa(db, token, slug) (ContaEntrada, error)`, `ErrEscolhaInvalida`. Ordenar `ORDER BY (e.empresa_origem_id IS NOT NULL), e.slug`.
- `backend/migrations/000020_add_realtime_ticket_to_tokens_acao.up.sql` -- molde para trocar `tokens_acao_tipo_check`; lista vigente: `verificacao_email, redefinicao_senha, mfa_login, realtime_ticket`. Última migration: `000049_add_empresa_raiz_id_to_usuarios`.
- `backend/handlers/auth.go:176-203,268-285,355-425` -- `refreshTokenCookiePath(r)` usa `r.PathValue("slug")`: extrair `refreshTokenCookiePathDoSlug(slug)` e um `setRefreshCookie` que aceite o path, sem mudar o comportamento atual. `LoginHandler` é a referência de mapeamento de erros, mensagens e `registrarTentativaLogin` (`handlers/logs_acesso.go:47`).
- `backend/handlers/entrada.go` (novo) -- `EntrarHandler(db, jwtSecret)` e `EntrarEscolhaHandler(db, jwtSecret)`; nunca chamam `empresaDaRequisicao`.
- `backend/main.go:328-346` -- registrar direto no `mux` (como `/api/plataforma/*`), SEM `registrar`/`RequireEmpresa`; atualizar o comentário de "exceções ao prefixo".
- Testes: `backend/services/empresa_teste_test.go:46` (`criarEmpresaDeTeste`; Treinamento = `UPDATE empresas SET empresa_origem_id`), `backend/handlers/empresa_teste_test.go`, `backend/handlers/auth_test.go:354-420` (`criarUsuarioLogin*`, `refreshCookieDoResultado`), `backend/main_test.go:334` (`newMux` real). Banco: `stockflow_t91`, `go test -p 1`; nunca `TRUNCATE empresas`; CNPJs de teste únicos por arquivo.
- `frontend/src/pages/SemEmpresaPage.tsx` + `.test.tsx` -- hoje a página explicativa; vira o login pela conta (mantém o nome do componente, importado por `main.tsx`). Visual e mensagens de erro: `frontend/src/pages/LoginPage.tsx` (`mensagemDeErro`, Card/Input/Label/Button).
- `frontend/src/lib/entrada.ts` -- `escolherApp` fica; ganha os tipos e chamadas `entrarPelaConta`/`concluirEscolha` e o repasse do MFA (`sessionStorage`, chave `entrada_mfa_pendente`, valor `{slug, mfaToken}`).
- `frontend/src/pages/LoginPage.tsx:98-110` -- estado `etapa`/`mfaToken`: iniciar em `codigo` quando há repasse pendente cujo `slug` = `slugDaURL()`; ler no inicializador do `useState` e remover a chave num `useEffect` (StrictMode chama o inicializador duas vezes).
- `frontend/nginx.conf:13`, `frontend/vite.config.ts:20` -- `/api/` já é encaminhado ao backend; nada a mudar.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000050_add_escolha_empresa_to_tokens_acao.{up,down}.sql` -- `tokens_acao_tipo_check` + `'escolha_empresa'`; coluna `contas_escolha UUID[] NULL` (ids das contas conferidas; `usuario_id` = primeira delas). Down apaga as linhas `escolha_empresa`, a coluna e restaura o CHECK.
- `backend/services/entrada.go` -- funções do Code Map, na regra de agregação das Boundaries.
- `backend/handlers/auth.go` -- cookie de refresh parametrizável por slug, sem mudança observável nas rotas atuais.
- `backend/handlers/entrada.go` -- os dois handlers: respostas, códigos, `logs_acesso`, ramo MFA.
- `backend/main.go` -- registro das duas rotas fora de `RequireEmpresa`.
- `backend/services/entrada_test.go`, `backend/handlers/entrada_test.go` -- cada linha da I/O Matrix; a do refresh e a do MFA via `newMux` (em `backend/main_test.go`) para provar que o cookie/token funcionam sob `/e/{slug}`.
- `frontend/src/lib/entrada.ts` + `entrada.test.ts` -- chamadas e repasse do MFA.
- `frontend/src/pages/SemEmpresaPage.tsx` -- formulário (E-mail, Senha, Entrar, botão "Esqueci a senha"); erros com os textos de `LoginPage`; etapa "Ambiente real ou Treinamento?" com um botão por Empresa (nome fantasia + "Ambiente real"/"Treinamento"); `ESCOLHA_INVALIDA` volta ao formulário com mensagem. Sucesso → `location.assign('/e/{slug}/')`; MFA → grava o repasse e `location.assign('/e/{slug}/login')`. "Esqueci a senha" nesta story só mostra, inline, "Para redefinir a senha, use 'Esqueci minha senha' no endereço de acesso da sua empresa." (a Story 15.3 troca pelo pedido na raiz).
- `frontend/src/pages/SemEmpresaPage.test.tsx`, `LoginPage.test.tsx` -- fluxos: direto, MFA (repasse), escolha, erro 401/429, escolha expirada; `LoginPage` abre na etapa de código com repasse do mesmo slug e ignora repasse de outro slug.

**Acceptance Criteria:**
- Given qualquer caminho fora de `/e/{empresa}` e `/plataforma`, when abre, then aparece o login pela conta (e-mail, senha, "Esqueci a senha"), não a página explicativa.
- Given login na raiz com uma conta sem MFA, when a pessoa entra, then o navegador vai a `/e/{slug}/` e o `AuthProvider` restaura a sessão pelo refresh silencioso, sem digitar de novo.
- Given uma conta com MFA, when a senha confere na raiz, then a pessoa digita o código na tela `/e/{slug}/login` e entra como hoje.
- Given as rotas `/e/{slug}/api/auth/*`, convites e SSO, when esta story entra, then as suítes existentes continuam verdes sem alteração de expectativa.

## Spec Change Log

### 2026-09-24 — Correção de uma linha da I/O Matrix no review
- **Achado que motivou:** Blind Hunter e Edge Case Hunter. Chamar `Login` em todas as contas fazia um login certo na conta real contar como senha errada no Treinamento, quando as senhas são diferentes. O Treinamento ficava bloqueado a cada 5 logins normais e ganhava linhas falsas de falha em `logs_acesso`.
- **O que mudou:** a linha "Senha certa só numa" dizia "a outra conta registra falha". Isso contradizia o AC do épico ("exatamente como no login pelo endereço da Empresa"), que só admite uma leitura. A linha passou a dizer que a outra conta não registra falha. A implementação foi para duas fases: primeiro compara o bcrypt sem gravar nada, depois chama `Login` só nas contas cuja senha conferiu (ou em todas, se nenhuma conferiu).
- **Estado ruim evitado:** conta de Treinamento bloqueada sem ninguém ter errado a senha, e auditoria poluída.
- **KEEP:** as regras de conta continuam só em `services.Login`. Senha errada em todas conta falha em cada conta. Sem conta nenhuma roda 2 bcrypts dummy, o mesmo custo de "uma conta, senha errada".

## Review Triage Log

### 2026-09-24 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 4 (high 0, medium 1, low 3)
- defer: 0
- reject: 22 (high 0, medium 3, low 19)
- addressed_findings:
  - `[medium]` `[patch]` Login certo numa conta contava falha e gravava linha de falha na outra conta do e-mail. Agora são duas fases, e `avaliadas` são só as contas em que a tentativa contou (ver Spec Change Log).
  - `[low]` `[patch]` Depois das duas fases, "sem conta" rodava 1 bcrypt e "uma conta, senha errada" rodava 2, o que reabria o oráculo de tempo. O caminho sem conta agora roda 2 bcrypts dummy.
  - `[low]` `[patch]` `ConcluirEscolhaEmpresa` passou a conferir de novo `email_verificado` e `senha_hash IS NOT NULL`.
  - `[low]` `[patch]` Testes que faltavam: caso misto (real bloqueada e Treinamento livre, entra no Treinamento) no service e no handler, e `TestMigration000050_Down`.

## Design Notes

Por que `contas_escolha UUID[]` em vez de uma linha por conta: `tokens_acao.token` é `UNIQUE` e o consumo tem de ser um único UPDATE atômico. `usuario_id` (NOT NULL, FK com cascade) recebe a primeira conta; se ela for apagada, o token some junto — aceitável num token de 5 min.

Consumo da escolha:

```sql
UPDATE tokens_acao SET usado_em = now()
WHERE token = $1 AND tipo = 'escolha_empresa' AND usado_em IS NULL AND expira_em > now()
RETURNING contas_escolha;
-- depois: conta de contas_escolha cuja Empresa tem slug = $2, status 'ativa',
-- u.ativo AND (u.bloqueado_ate IS NULL OR u.bloqueado_ate <= now())
```

O repasse do MFA usa `sessionStorage` (mesma origem, some ao fechar a aba) porque a raiz e `/e/{slug}` são apps diferentes; o `mfaToken` sozinho não dá acesso (exige o código TOTP e vence em 5 min).

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros, sem arquivos listados
- `cd backend && DATABASE_URL='postgres://stockflow:stockflow@127.0.0.1:5432/stockflow_t91?sslmode=disable' go test -p 1 -count=1 -timeout 25m ./...` -- expected: verde (`services` leva ~8 min; não rodar junto com o vitest)
- `cd frontend && npx tsc --noEmit -p tsconfig.app.json && npx tsc -b && npx vitest run` -- expected: verde

## Auto Run Result

**Resumo:** a raiz do domínio (app `sem-empresa`) agora é o login pela conta. `POST /api/auth/entrar` acha as contas do e-mail em Empresas ativas e delega a `services.Login`. Com uma conta, entra direto: cookie de refresh com `Path=/e/{slug}/api/auth`, ou `mfaToken` repassado à tela `/e/{slug}/login` quando a conta tem MFA. Com conta na real e no Treinamento, responde a pergunta "Ambiente real ou Treinamento?" e um `escolhaToken` de uso único (5 min), trocado em `POST /api/auth/entrar/escolha`. "Esqueci a senha" na raiz só orienta; o pedido pela raiz fica para a Story 15.3.

**Arquivos:**
- `backend/migrations/000050_add_escolha_empresa_to_tokens_acao.{up,down}.sql`: tipo `escolha_empresa` e coluna `contas_escolha UUID[]`.
- `backend/services/entrada.go`: `EntrarPelaConta` (duas fases), `IniciarEscolhaEmpresa` e `ConcluirEscolhaEmpresa`.
- `backend/handlers/entrada.go`: `EntrarHandler`, `EntrarEscolhaHandler` e `logs_acesso` por conta.
- `backend/handlers/auth.go`: Path do cookie de refresh parametrizável por slug, sem mudança nas rotas atuais.
- `backend/main.go`: as duas rotas ficam fora de `RequireEmpresa`.
- `backend/services/entrada_test.go`, `backend/handlers/entrada_test.go`, `backend/main_test.go`: I/O Matrix, fluxo real pelo `newMux` (refresh e MFA sob `/e/{slug}`) e migration 000050 down.
- `frontend/src/lib/entrada.ts` (+ test): chamadas da raiz, repasse do MFA por `sessionStorage` e textos de erro compartilhados.
- `frontend/src/pages/SemEmpresaPage.tsx` (+ test): tela de login pela conta e etapa de escolha.
- `frontend/src/pages/LoginPage.tsx` (+ test): abre na etapa de código quando há repasse para o mesmo slug.

**Review:** 4 camadas (blind, edge-case, verification-gap, intent-alignment). 4 patches (medium 1, low 3), 0 adiados, 22 rejeitados. Entre os rejeitados:
- log de sucesso nas duas contas antes da escolha (a spec define `sucesso` pelo fator senha, como no `LoginHandler`);
- "Esqueci a senha" e SSO na raiz (Stories 15.3 e fora de escopo);
- rate limit (não existe em nenhuma rota de login hoje);
- mais de 2 contas por e-mail (a 15.1 impede);
- 401 em vez de 404 para Empresa inativa (a spec escolheu não revelar a Empresa);
- itens cosméticos de organização de mensagens.

**Recomendação de nova revisão:** `true`. Patches: high 0, medium 1, low 3. Pontuação 3×1 + 3 = 6 (≥ 5).

**Verificação:**
- `go build ./...`, `go vet ./...` e `gofmt -l .`: limpos.
- `go test -p 1 -count=1 -timeout 25m` em todos os pacotes (`.`, `handlers`, `middleware`, `cmd/...` e `services`, este em rodada separada), contra `stockflow_t91`: todos `ok`, já com todos os patches aplicados.
- `npx tsc --noEmit -p tsconfig.app.json`, `npx tsc -b` e `npx vitest run`: 61 arquivos, 830 testes, verdes.
- O comando de frontend da seção Verification foi corrigido: `tsc -b` não aceita `-p`.

**Riscos residuais:**
- O tempo de resposta ainda cresce com o número de contas do e-mail (1 contra 2), mas não separa "sem conta" de "uma conta".
- O repasse do `mfaToken` passa por `sessionStorage`. Sozinho não dá acesso: exige o TOTP e vence em 5 min.
- A migration 000050 roda sozinha no boot da API.
- Nada foi publicado: todo push em `main` faz deploy em produção.
