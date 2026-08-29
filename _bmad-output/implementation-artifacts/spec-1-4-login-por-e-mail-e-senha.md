---
title: 'Story 1.4 — Login por e-mail e senha'
type: 'feature'
created: '2026-08-29'
status: 'done'
baseline_revision: '802c9a31d1e78ce020ea123dda99d10f02ebd129'
review_loop_iteration: 0
followup_review_recommended: false
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      Duplicação de `erroEnvelope`/`erroDetalhe`/`escreverErro` entre
      `backend/middleware/auth.go` e `backend/handlers/auth.go`, criada
      deliberadamente para evitar um ciclo de import entre os dois pacotes.
    evidence: |-
      middleware/auth.go define seu próprio erroEnvelope/erroDetalhe/escreverErro
      idênticos aos de handlers/auth.go porque middleware nunca pode importar
      handlers (a composição RequireAuth(handlers.MeHandler()) acontece em
      main.go, na direção oposta). Uma extração para um pacote de baixo nível
      compartilhado (ex. apperror) removeria a duplicação, mas é uma mudança
      estrutural maior que um patch trivial desta passagem — mesmo padrão já
      usado para a duplicação de testDB() entre três arquivos na Story 1.3.
    location: 'backend/middleware/auth.go:35, backend/handlers/auth.go:17'
    severity: low
  - summary: >-
      Duplicação de helpers de teste ("inserir usuário direto em `usuarios`
      com controle de estado" e `testJWTSecret`) entre
      `backend/handlers/auth_test.go`, `backend/middleware/auth_test.go` e
      `backend/services/auth_test.go`.
    evidence: |-
      criarUsuarioLogin/criarUsuarioLoginComEstado (handlers), criarUsuario
      (middleware) e criarUsuarioParaLogin (services) são três variações
      quase idênticas do mesmo helper, e testJWTSecret é redeclarado
      verbatim nos três arquivos — mesmo padrão de duplicação já deferido
      para testDB() na Story 1.3 (arquivos de teste em pacotes Go diferentes
      não podem compartilhar um helper não-exportado sem um pacote de
      suporte de teste dedicado, mudança estrutural maior que um patch
      trivial desta passagem).
    location: 'backend/handlers/auth_test.go:337-380, backend/middleware/auth_test.go:1018-1035, backend/services/auth_test.go:500-521'
    severity: low
---

<intent-contract>

## Intent

**Problem:** Hoje só existe a conta do Adm semeada via CLI (Story 1.1) e o autocadastro (Story 1.3), que persiste `email_verificado` mas não oferece nenhum jeito de uma conta confirmada autenticar — nenhum token de sessão existe no sistema, então nenhum endpoint pode ser protegido ainda.

**Approach:** Endpoint público de login que valida e-mail/senha e emite os dois tokens de sessão do AD-6: um JWT de acesso curto (30min, devolvido no corpo da resposta) e um refresh token opaco rotativo (2h, persistido em nova tabela `sessoes`, entregue em cookie `HttpOnly`). Um middleware de autenticação (`Authorization: Bearer`) resolve o usuário sempre a partir do Postgres a cada requisição, expondo o primeiro endpoint protegido (`GET /api/auth/me`) — prova viva da AC de 401. Frontend ganha a tela pública de Login.

## Boundaries & Constraints

**Always:**
- Login só sucede com `usuarios.ativo=true`, `email_verificado=true`, `senha_hash` não nulo e senha batendo via bcrypt; qualquer uma dessas condições falhando (incluindo e-mail inexistente) devolve a MESMA resposta — 401 `{"error":{"code":"INVALID_CREDENTIALS","message":"E-mail ou senha inválidos."}}` — nunca revela qual delas falhou nem se o e-mail existe (regra explícita do contexto do épico: "erros de login... nunca revelam se um e-mail existe"). `INVALID_CREDENTIALS` é um code novo, fora do vocabulário fixo de sessão do AD-14 (que cobre `TOKEN_EXPIRED`/`SESSION_REVOKED`/`FORBIDDEN`/etc., não "credencial errada"); mesmo tratamento já aceito para `INTERNAL_ERROR` como fallback fora da lista.
- Sessão emitida (AD-6): access JWT via `golang-jwt/jwt/v5`, HS256, claim `sub`=`usuario_id`, `exp` 30min, devolvido no corpo `{"token": "...", "usuario": {"id","nome","email","papel"}}`; refresh token opaco (`crypto/rand` 32 bytes + `base64.RawURLEncoding`, mesmo padrão de `gerarTokenAcao` em `services/auth.go`) gravado em `sessoes`, entregue via `Set-Cookie` (`refresh_token`, `HttpOnly`, `Path=/api/auth`, `SameSite=Lax`, `Secure` quando `r.TLS!=nil` ou `X-Forwarded-Proto=https`, `Max-Age` 2h).
- `POST /api/auth/refresh` lê o cookie, busca a linha em `sessoes` com `refresh_token` igual, `revogado_em IS NULL`, `expira_em > now()`; se encontrada, marca essa linha revogada e insere uma nova (rotação) atomicamente numa única transação, devolve novo access token e novo cookie. Ausente/expirado/revogado/inexistente → 401 `TOKEN_EXPIRED`, limpa o cookie (`Max-Age=0`).
- Middleware `middleware/auth.go` (`RequireAuth`) exige `Authorization: Bearer <token>`; token ausente/malformado/assinatura inválida/expirado → 401 `TOKEN_EXPIRED`. Token válido mas usuário não encontrado ou `ativo=false` → 401 `SESSION_REVOKED` — garante que uma conta desativada perde acesso já na próxima requisição, sem esperar o TTL de 30min do access token.
- Papel do usuário nunca é lido do claim do JWT (o claim só carrega `sub`) — o middleware sempre resolve `papel`/`ativo`/`nome`/`email` consultando `usuarios` no Postgres a cada requisição (regra já fixada no contexto do épico).
- `GET /api/auth/me`, protegido por `RequireAuth`, devolve `{"id","nome","email","papel"}` do usuário resolvido pelo middleware.
- `/login` é rota pública no frontend, irmã de `/cadastro`/`/verificar-email` fora do `AppShell`, mesmo layout mínimo (Story 1.3). Login bem-sucedido guarda o access token em memória via `lib/session.ts` (nunca `localStorage`/`sessionStorage`) e navega para `/`.
- `JWT_SECRET` obrigatório no boot do `api` — mesmo tratamento fail-fast já aplicado a `DATABASE_URL` em `main.go` (log + `os.Exit(1)` se vazio); documentado em `.env.example`; `docker-compose.yml` recebe um valor de desenvolvimento explícito (nunca usado em produção).

**Block If:** nenhuma decisão desta story depende de aprovação humana.

**Never:**
- Nenhum endpoint/UI de logout — fora do escopo de qualquer AC desta story; não existe ainda tela em Configurações para hospedá-lo.
- Nenhum gating de navegação por papel em `AppShell`/`nav-items.ts` — explicitamente adiado para a Story 1.5 (comentário já existente em `nav-items.ts`).
- Nenhuma criação/redefinição de senha para conta só-SSO (`senha_hash` nulo) — Story 1.6; aqui essa conta só falha login com a mesma mensagem genérica.
- Nenhum bloqueio por tentativas nem validação de força de senha no login — Story 1.10.
- Nenhuma detecção de reuso de refresh token em cadeia (revogar todas as sessões se um token já revogado for reapresentado) — só a rejeição direta do token já revogado/expirado é exigida.
- Nenhum bootstrap automático de sessão no carregamento do app (silent refresh via cookie ao montar `App.tsx`) — sem consumidor real nesta story, já que `AppShell` não gateia nada ainda; fica para a Story 1.5.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Login válido | conta ativa, `email_verificado=true`, senha correta | 200; corpo com `token`+`usuario`; cookie `refresh_token` setado; linha nova em `sessoes` | — |
| Senha incorreta | senha não bate com o hash | 401 `INVALID_CREDENTIALS` | nenhuma sessão criada |
| E-mail inexistente | nenhuma conta com esse e-mail normalizado | 401 `INVALID_CREDENTIALS` (mesma mensagem do caso acima) | — |
| E-mail não verificado | `email_verificado=false` | 401 `INVALID_CREDENTIALS` (mesma mensagem) | — |
| Conta desativada | `ativo=false` | 401 `INVALID_CREDENTIALS` (mesma mensagem) | — |
| Conta só-SSO | `senha_hash IS NULL` | 401 `INVALID_CREDENTIALS` (mesma mensagem) | — |
| Campo obrigatório ausente | e-mail ou senha em branco | 400 `VALIDATION_ERROR` | nenhuma consulta ao banco |
| Refresh válido | cookie com token não expirado/não revogado | 200; novo access token; cookie rotacionado | linha antiga marcada revogada na mesma transação |
| Refresh ausente/expirado/revogado | sem cookie, ou linha expirada/revogada/inexistente | 401 `TOKEN_EXPIRED`; cookie limpo | — |
| `GET /api/auth/me` sem token | sem header `Authorization` | 401 `TOKEN_EXPIRED` | — |
| `GET /api/auth/me`, usuário desativado após emissão do token | `ativo=false` no momento da requisição | 401 `SESSION_REVOKED` | — |

</intent-contract>

## Code Map

- `backend/migrations/000003_create_sessoes.up.sql`/`.down.sql` (novo) -- tabela `sessoes` (refresh tokens), FK `usuario_id → usuarios(id) ON DELETE CASCADE`, índice em `usuario_id` (mesmo padrão de `tokens_acao`/`emails_pendentes`, migration 000002).
- `backend/services/auth.go` (estende) -- `Login(db, email, senha string) (usuarioID string, err error)` reutilizando `normalizeEmail`; `EmitirSessao(db, jwtSecret []byte, usuarioID string) (accessToken, refreshToken string, expiraRefresh time.Time, err error)`; `RenovarSessao(db, jwtSecret []byte, refreshTokenAtual string) (novoAccess, novoRefresh string, err error)`; `BuscarUsuarioSessao(db, usuarioID string) (UsuarioSessao, error)` para o middleware. Reusa `pqUniqueViolation`/`gerarTokenAcao`-mesmo estilo já existente no arquivo.
- `backend/middleware/auth.go` (novo pacote) -- `RequireAuth(db *sql.DB, jwtSecret []byte) func(http.HandlerFunc) http.HandlerFunc`; parseia `Authorization: Bearer`, valida via `golang-jwt/jwt/v5`, injeta `UsuarioSessao` resolvido no contexto (`context.WithValue`, chave tipada não-exportada).
- `backend/handlers/auth.go` (estende) -- `POST /api/auth/login`, `POST /api/auth/refresh`, `GET /api/auth/me` (`RequireAuth(MeHandler)`); mesmo padrão de envelope de erro/`escreverErro`/`escreverJSON` já existente no arquivo.
- `backend/main.go` -- lê `JWT_SECRET` do env (fail-fast, mesmo padrão de `DATABASE_URL`); registra as três rotas novas em `newMux` (a de `/me` já passando pelo `RequireAuth`).
- `frontend/src/lib/session.ts` (novo) -- `getAccessToken()`/`setAccessToken(token)`/`clearAccessToken()`, guardados só em memória (variável de módulo), nunca `localStorage`.
- `frontend/src/pages/LoginPage.tsx` (novo) -- formulário e-mail/senha (mesmos componentes shadcn de `CadastroPage.tsx`: `Input`/`Label`/`Card`/`Button`), `POST /api/auth/login`, guarda o token via `lib/session.ts`, navega para `/` no sucesso; erro exibido inline (mesma mensagem genérica do backend).
- `frontend/src/App.tsx` -- rota pública `/login`, irmã de `/cadastro`/`/verificar-email`; link "Entrar" de `CadastroPage.tsx` já aponta para cá.
- `.env.example` -- documenta `JWT_SECRET`.
- `docker-compose.yml` -- `api.environment.JWT_SECRET` com valor de desenvolvimento.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000003_*.sql` -- schema de `sessoes` -- base para emissão/rotação de refresh token.
- `backend/services/auth.go` + `auth_test.go` (integração, Postgres real) -- `Login`/`EmitirSessao`/`RenovarSessao`/`BuscarUsuarioSessao` cobrindo a I/O Matrix -- satisfaz todas as ACs de sessão.
- `backend/middleware/auth.go` + `auth_test.go` -- `RequireAuth`, incluindo o caso de usuário desativado pós-emissão do token -- expõe o comportamento de 401/`SESSION_REVOKED`.
- `backend/handlers/auth.go` + `auth_test.go` (httptest via `newMux`) -- `POST /api/auth/login`, `POST /api/auth/refresh`, `GET /api/auth/me` -- expõe a I/O Matrix na fronteira HTTP.
- `backend/main.go` -- `JWT_SECRET` fail-fast, registro das rotas.
- `frontend/src/lib/session.ts` + teste -- guarda/limpa o token em memória.
- `frontend/src/pages/LoginPage.tsx` + teste RTL -- fluxo de login, erro genérico, redirecionamento.
- `frontend/src/App.tsx` -- rota `/login`.
- `.env.example` / `docker-compose.yml` -- `JWT_SECRET`.

**Acceptance Criteria:**
- Given uma conta ativa, com e-mail verificado e senha correta, when o usuário submete a tela de Login, then um JWT de acesso (30min) é emitido e um refresh token rotativo é definido em cookie `HttpOnly` com TTL de 2h.
- Given uma sessão sem atividade por mais de 2h, when o usuário tenta usar o refresh token expirado, then a sessão é encerrada (`TOKEN_EXPIRED`) e é necessário logar novamente.
- Given credenciais inválidas (senha errada, e-mail inexistente, e-mail não verificado, conta desativada ou conta só-SSO), when o usuário submete o formulário, then o sistema responde com a mesma mensagem genérica em todos os casos — nunca revela qual condição falhou nem se o e-mail existe.
- Given qualquer endpoint autenticado (`GET /api/auth/me`), when uma requisição chega sem token válido, then a resposta é 401; login e refresh continuam acessíveis sem token.
- Given um usuário com token de acesso ainda válido cuja conta é desativada entre a emissão e o uso, when ele chama `GET /api/auth/me`, then a resposta é 401 `SESSION_REVOKED` — o middleware nunca confia em um papel/estado vindo do claim do JWT.

## Spec Change Log

## Review Triage Log

### 2026-08-29 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 7: (high 0, medium 1, low 6)
- defer: 1: (low 1)
- reject: 13: (high 0, medium 2, low 11)
- addressed_findings:
  - `[medium]` `[patch]` `Login` (`backend/services/auth.go`) retornava imediatamente em `sql.ErrNoRows` (e-mail inexistente) mas sempre chegava a `bcrypt.CompareHashAndPassword` (deliberadamente lento) quando o e-mail existe — a diferença de tempo de resposta entre os dois casos permite a um atacante enumerar e-mails cadastrados, contrariando a regra explícita do contexto do épico de nunca revelar se um e-mail existe. Corrigido para sempre executar uma comparação bcrypt de custo equivalente (contra um hash fixo) quando o e-mail não existe, igualando o tempo dos dois caminhos.
  - `[low]` `[patch]` `backend/go.mod` marcava `github.com/golang-jwt/jwt/v5` como `// indirect`, apesar de importado diretamente em `services/auth.go` e `middleware/auth.go` — `go mod tidy` não tinha sido rodado. Corrigido.
  - `[low]` `[patch]` `RenovarSessao` calcula e persiste `expiraEm` mas não o devolve; `RefreshHandler` recalculava `time.Now().UTC().Add(RefreshTokenExpiracao)` de forma independente para montar o cookie, podendo divergir do valor real gravado em `sessoes` pelo tempo do round-trip ao banco. Corrigido para `RenovarSessao` devolver o `expiraEm` realmente persistido, e `RefreshHandler` usar esse valor (mesmo padrão já usado por `EmitirSessao`/`LoginHandler`).
  - `[low]` `[patch]` Nenhum teste lia o atributo `Secure` do cookie `refresh_token` (nem no caminho HTTP simples, nem simulando TLS/`X-Forwarded-Proto: https`) — uma regressão em `cookieEhSeguro` (lógica invertida, ou hardcode) passaria despercebida. Adicionado teste cobrindo os dois casos.
  - `[low]` `[patch]` Nenhum teste lia `cookie.MaxAge` no caminho de sucesso de login/refresh — só os caminhos de cookie limpo tinham asserção sobre `MaxAge`. Adicionado teste afirmando `MaxAge > 0` e próximo do TTL de 2h no sucesso.
  - `[low]` `[patch]` `RequireAuth` já restringe corretamente a família de assinatura a HMAC (`*jwt.SigningMethodHMAC`) antes de usar `jwtSecret`, mas nenhum teste provava que um token assinado com outro algoritmo (ex. `alg=none` ou RS256) é rejeitado — uma regressão nessa checagem não seria pega por nenhum teste existente. Adicionado teste cobrindo a rejeição.
  - `[low]` `[patch]` `LoginPage.test.tsx` cobria `INVALID_CREDENTIALS`, `VALIDATION_ERROR` e exceção de rede, mas nunca uma resposta HTTP real com `code` desconhecido/ausente (ex. `INTERNAL_ERROR`) — caminho distinto do de exceção de rede, ainda que caia na mesma mensagem genérica. Adicionado teste cobrindo esse caso.
- Achados roteados para `defer`: duplicação de `erroEnvelope`/`erroDetalhe`/`escreverErro` entre `backend/middleware/auth.go` e `backend/handlers/auth.go` (deliberada, para evitar ciclo de import) — uma extração para um pacote de baixo nível compartilhado (ex. `apperror`) removeria a duplicação, mas é uma mudança estrutural maior que um patch trivial desta passagem, mesmo padrão já usado para a duplicação de `testDB()` na Story 1.3.
- Achados roteados para `reject` (13): `RenovarSessao` não checa `usuarios.ativo` antes de rotacionar — o limite real de acesso já é imposto por `RequireAuth` em todo endpoint protegido, que sempre revalida `ativo` no Postgres, então nenhum recurso protegido fica alcançável mesmo se o refresh "suceder" para uma conta desativada; ausência de proteção CSRF em `POST /api/auth/login` — preocupação sistêmica que já se aplica a `/api/auth/cadastro` (Story 1.3) sem ter sido levantada, nenhuma AC/AD pede mitigação nesta story; refresh token gravado em texto puro — decisão já registrada e justificada nas Design Notes desta spec, mesmo tratamento já aceito para `tokens_acao` na Story 1.3; ausência de job de expurgo para `sessoes` — mesmo padrão já rejeitado na Story 1.3 para `tokens_acao`/`emails_pendentes`, fora de escopo; `BuscarUsuarioSessao` falhando logo após `EmitirSessao` já ter persistido a sessão deixaria uma linha órfã — sem consequência de segurança (nenhum cookie/token chega ao cliente) e a linha expira naturalmente; `JWT_SECRET` só falha-fast por estar vazio, sem checagem de força/entropia mínima — nenhuma AC/AD pede, mesma família de validação explicitamente adiada para a Story 1.10 (força de senha); ausência de teste para o fail-fast de `JWT_SECRET` em `main.go` — mesmo padrão já aceito (nunca testado) para o fail-fast equivalente de `DATABASE_URL`; `cookieEhSeguro` compara `X-Forwarded-Proto` com igualdade de string exata (sensível a maiúsculas/lista multi-hop) — implementação bate literalmente com o texto da spec, nenhuma AD descreve uma topologia de proxy que exigisse tratar variações; `RequireAuth` aceita toda a família HMAC (HS256/HS384/HS512), não só HS256 — não há ataque conhecido entre variantes HMAC quando a chave é a mesma, a checagem de tipo já bloqueia a confusão real (RS256/`alg=none`); `claims.Subject` não validado como UUID antes da consulta — só seria alcançável com o segredo já comprometido (nesse caso o sistema inteiro já estaria comprometido) ou um bug em outro lugar, nenhum caminho legítimo produz um `sub` malformado; conta desativada entre a checagem de `Login` e a leitura de `BuscarUsuarioSessao` no `LoginHandler` — mesma linha de raciocínio do achado de `RenovarSessao` acima, `RequireAuth` barra qualquer uso subsequente do token contra um recurso protegido; ausência de retry em colisão de token único (`gerarTokenAcao`/novo refresh) — 32 bytes de `crypto/rand`, mesmo padrão já aceito sem retry para `tokens_acao` em três passagens de revisão da Story 1.3; `LoginPage.tsx` trata corpo 200 com `token` vazio como sucesso — só ocorreria com o backend violando seu próprio contrato, cenário que não pode acontecer sob operação normal.

### 2026-08-29 — Review pass (follow-up)
- intent_gap: 0
- bad_spec: 0
- patch: 4: (high 0, medium 1, low 3)
- defer: 1: (low 1)
- reject: 12: (high 0, medium 0, low 12)
- addressed_findings:
  - `[medium]` `[patch]` `Login` (`backend/services/auth.go`) fechava o side-channel de tempo só para "e-mail inexistente" (comparação bcrypt contra hash fixo), mas o ramo `!ativo || !emailVerificado || !senhaHash.Valid` (conta desativada, e-mail não verificado, conta só-SSO) retornava `ErrCredenciaisInvalidas` imediatamente, sem nenhuma comparação bcrypt — tornando essa resposta mensuravelmente mais rápida que os demais caminhos de falha e revelando por temporização que a conta existe e em que estado ela está, contrariando a regra explícita do contexto do épico. Corrigido para sempre executar uma comparação bcrypt de custo equivalente (hash real quando presente, hash fixo caso contrário) antes de qualquer retorno de credencial inválida, igualando o tempo de todos os caminhos de falha.
  - `[low]` `[patch]` A I/O Matrix desta story especifica, na fronteira HTTP, os cinco sub-casos de "credenciais inválidas" (senha errada, e-mail inexistente, e-mail não verificado, conta desativada, conta só-SSO) e dois sub-casos de refresh inválido (token expirado, token já revogado); só 2 dos 5 sub-casos de login e nenhum dos 2 sub-casos de refresh tinham teste no nível HTTP (`TestLoginHandler_CredenciaisInvalidas`/`TestRefreshHandler_*`) — os demais só eram provados no nível de serviço (`services.TestLogin_CredenciaisInvalidas`/`TestRenovarSessao_TokenExpirado`/`TestRenovarSessao_TokenJaRevogado`), uma camada abaixo da superfície que a própria Matrix descreve. Adicionados os 3 sub-casos de login faltantes em `TestLoginHandler_CredenciaisInvalidas` e dois novos testes (`TestRefreshHandler_TokenExpirado`, `TestRefreshHandler_TokenJaRevogado`) provando os dois sub-casos de refresh na fronteira HTTP real.
  - `[low]` `[patch]` Nenhum teste provava que `authRequestMaxBytes` (64KB) rejeita um corpo grande em `POST /api/auth/login`, ao contrário do endpoint irmão `/api/auth/cadastro` (`TestCadastroHandler_CorpoMuitoGrande`) — uma regressão que removesse ou desalinhasse o limite em login passaria despercebida. Adicionado `TestLoginHandler_CorpoMuitoGrande` espelhando o teste existente de cadastro.
  - `[low]` `[patch]` Comentário de doc typo em `backend/handlers/auth.go`: `usuarioRespota` deveria ser `usuarioResposta` (nome do próprio tipo documentado). Corrigido.
- Achados roteados para `defer`: duplicação de helpers de teste (inserir usuário direto com controle de estado, e `testJWTSecret`) entre `backend/handlers/auth_test.go`, `backend/middleware/auth_test.go` e `backend/services/auth_test.go` — mesmo padrão já deferido para `testDB()` na Story 1.3; ver `deferred` (frontmatter).
- Achados roteados para `reject` (12): `X-Forwarded-Proto` comparado por igualdade exata, sensível a lista multi-hop separada por vírgula — mesma família já rejeitada nesta spec, nenhuma AD descreve essa topologia de proxy; esquema `Authorization` aceito só como `Bearer` (case-sensitive) — RFC permite variação de caixa, mas nenhum cliente deste repositório envia outra forma, implementação bate com o texto literal da spec; ausência de índice/expurgo para linhas antigas em `sessoes` — mesmo padrão já rejeitado para `tokens_acao`/`emails_pendentes` na Story 1.3, fora de escopo; `BuscarUsuarioSessao`/`middleware.RequireAuth` não valida que o `sub` do JWT é um UUID bem formado antes da consulta — só alcançável com o segredo já comprometido, mesmo raciocínio já rejeitado nesta spec; `cookieEhSeguro` confia incondicionalmente em `X-Forwarded-Proto` sem checar se a requisição realmente passou por um proxy confiável — implementação bate com o texto literal da spec (AD-6), nenhuma AD descreve uma topologia de proxy exigindo validação adicional; `JWT_SECRET` sem checagem de força/entropia mínima — mesmo raciocínio já rejeitado nesta spec, adiado para a Story 1.10; duas consultas a `usuarios` por login bem-sucedido (`Login` + `BuscarUsuarioSessao`) — reuso deliberado da mesma função usada pelo middleware, custo tolerável (uma consulta indexada extra); ausência de defesa CSRF em `POST /api/auth/refresh` além de `SameSite=Lax` — preocupação sistêmica já rejeitada para `/api/auth/login` nesta spec, nenhuma AC/AD pede mitigação; `LoginPage.tsx` ignora o campo `usuario` do corpo de resposta do login — nenhuma AC/AD desta story exige consumir esse campo no frontend, escopo explicitamente limitado a token+redirecionamento; comentários de pacote não documentam `INVALID_CREDENTIALS` como adição ao vocabulário fixo de código de erro do AD-14 — o `<intent-contract>` já registra essa decisão como fonte de verdade, nota cosmética sem efeito funcional; `EmitirSessao` commitando a sessão antes de `BuscarUsuarioSessao` no `LoginHandler` poderia deixar uma linha órfã em `sessoes` numa corrida estreita (conta removida entre as duas chamadas) — mesmo raciocínio já rejeitado nesta spec para o caso de conta desativada na mesma janela, sem consequência de segurança e a linha expira naturalmente.

### 2026-08-29 — Review pass (fresh follow-up)
- intent_gap: 0
- bad_spec: 0
- patch: 1: (high 0, medium 0, low 1)
- defer: 0
- reject: 22: (high 0, medium 0, low 22)
- addressed_findings:
  - `[low]` `[patch]` `clearRefreshCookie` (`backend/handlers/auth.go`) grava `Secure`/`SameSite` no cookie de refresh LIMPO usando `cookieEhSeguro(r)`, mas nenhum teste provava esse caminho sob TLS/`X-Forwarded-Proto: https` — só o caminho de SUCESSO (`setRefreshCookie`, via `TestLoginHandler_CookieSecure`) tinha essa cobertura; `TestRefreshHandler_CookieAusente`/`CookieInvalidoOuExpirado` só rodam sobre HTTP simples e nunca leem `Secure`/`SameSite`. Uma regressão que desacoplasse `clearRefreshCookie` de `cookieEhSeguro` passaria despercebida. Adicionado `TestRefreshHandler_CookieLimpoSecure`, espelhando `TestLoginHandler_CookieSecure` para o caminho de limpeza (TLS direto e proxy com `X-Forwarded-Proto: https`).
- Achados roteados para `reject` (22): 4 achados dos revisores (família de assinatura HMAC além de HS256; confiança incondicional em `X-Forwarded-Proto`; `RenovarSessao` não checa `usuarios.ativo`; linha órfã em `sessoes` se `BuscarUsuarioSessao` falhar após `EmitirSessao`) repetem, sem fato novo, achados já revisados e rejeitados nas duas passagens de review anteriores desta mesma spec (mesmo raciocínio: nenhum ataque conhecido, nenhuma AC/AD exige, ou nenhuma consequência de segurança residual); 1 achado (`LoginPage.tsx` ignora o campo `usuario` do corpo de resposta) repete achado já rejeitado na primeira passagem (nenhuma AC/AD exige consumir esse campo); 1 achado (duplicação de helpers de teste entre os três arquivos `_test.go`) repete, sem fato novo, item já registrado em `deferred` (frontmatter) na passagem anterior — não é um novo defer, é o mesmo achado re-encontrado. Dos achados restantes: ausência de logout, ausência de bloqueio por tentativas/rate limiting em login, e ausência de bootstrap automático de sessão (silent refresh no `App.tsx`) estão explicitamente fora de escopo pelo próprio `<intent-contract>` (seção `Never`, que já lista os três explicitamente e/ou os adia para as Stories 1.5/1.10); ausência de expurgo de linhas antigas em `sessoes` é o mesmo padrão já rejeitado nesta spec e na Story 1.3 para `tokens_acao`/`emails_pendentes`; duas consultas a `usuarios` por login bem-sucedido (`Login`+`BuscarUsuarioSessao`) é reuso deliberado já aceito nesta spec; `JWT_SECRET` sem checagem de força/entropia é a mesma família de validação já rejeitada nesta spec e adiada para a Story 1.10; ausência de `Cache-Control: no-store` nas respostas com token é hardening não exigido por nenhuma AC/AD desta story (respostas são `POST`, não cacheadas por padrão por navegadores/proxies); ausência de fluxo de refresh proativo/interceptor no frontend é o mesmo bootstrap de sessão explicitamente adiado para a Story 1.5 (seção `Never`); `LoginResposta` no frontend não tipa o campo `usuario` do corpo de login é o mesmo achado já rejeitado (nenhuma AC/AD exige consumi-lo); a ausência de teste de temporização para o fechamento do side-channel de tempo (`dummyBcryptHash`) e a ausência de um teste que espione chamadas ao banco para provar "nenhuma consulta ao banco" na validação de campo obrigatório são observações do auditor de alinhamento de intenção, não achados acionáveis — nenhuma AC/AD exige teste de temporização (inerentemente não-determinístico) nem uma camada de espionagem de banco que não existe em nenhum outro teste do repositório; a defesa adicional contra confusão de algoritmo (RS256/`alg:none`) é hardening aditivo já implementado e testado, não um problema; `scripts/notify_telegram.py` (3 achados: falta de checagem de `returncode`/timeout no subprocess, exceções engolidas sem retry no envio Telegram, e regex de parsing frágil ao formato de saída do `bmad-loop status`) e as duas entradas novas em `.gitignore` (`.telegram-notify-state.txt`, `.env.telegram`) pertencem a uma ferramenta de orquestração do bmad-loop sem nenhuma relação com o `<intent-contract>` desta story (login/sessão) — foram incluídas no diff apenas por estarem no intervalo de commits entre o baseline e HEAD, não por fazerem parte da Story 1.4; fora de escopo pela própria autoridade do intent, que não menciona esse arquivo em nenhum ponto.

## Design Notes

DDL de referência (a migration real é a fonte da verdade):

```sql
CREATE TABLE sessoes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  usuario_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  refresh_token TEXT NOT NULL UNIQUE,
  expira_em TIMESTAMPTZ NOT NULL,
  revogado_em TIMESTAMPTZ,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sessoes_usuario_id ON sessoes (usuario_id);
```

Refresh token guardado em texto puro (não hash) — mesma decisão e mesma justificativa já registrada em `tokens_acao` (Story 1.3, spec-1-3): nenhuma AD exige hash, mesmo nível de proteção já aplicado ao restante do schema desta fase. A rotação em si (o token antigo é imediatamente marcado revogado e nunca mais aceito) já limita a janela de exposição de um valor eventualmente vazado.

Rotação: a busca+revogação da linha antiga usa `WHERE refresh_token = $1 AND revogado_em IS NULL AND expira_em > now()` dentro da mesma transação que insere a nova linha — mesmo padrão de fechamento de janela de corrida já usado em `VerificarEmail` (`marcarUsado`, spec-1-3): se `RowsAffected() == 0`, outra requisição já rotacionou ou o prazo expirou entre a leitura e o update, e o resultado é `TOKEN_EXPIRED`.

Access token: claim mínimo (`sub` apenas) — deliberado, para que o middleware nunca tenha a tentação de confiar em papel/estado carimbado no token em vez de reconsultar `usuarios`.

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: build limpo.
- `cd backend && go vet ./...` -- expected: sem warnings.
- `docker compose up -d db && cd backend && go test -p 1 ./...` -- expected: todos os testes de integração passam (login, refresh/rotação, middleware, `/me`).
- `cd frontend && npm run build && npm run lint && npm run test` -- expected: build/lint limpos, testes de `LoginPage`/`session.ts` passam.
- `docker compose up --build` -- expected: `api`/`web` sobem saudáveis; `POST /api/auth/login`, `POST /api/auth/refresh` e `GET /api/auth/me` respondem através do proxy `/api`.

**Manual checks (if no CLI):**
- Abrir `/login` no navegador, submeter credenciais de uma conta já verificada (criada via `/cadastro` + link de verificação), confirmar redirecionamento para `/` e a presença do cookie `refresh_token` (`HttpOnly`) nas DevTools.

## Auto Run Result

**Resumo:** Esta execução automática partiu de uma spec já `done` (implementação e duas passagens de review anteriores já haviam sido commitadas) e conduziu apenas uma passagem de review adicional (fresh follow-up), sem re-implementar nada. Um gap de verificação real foi encontrado e corrigido: o caminho de LIMPEZA do cookie de refresh (`clearRefreshCookie`) nunca tinha sua atribuição `Secure`/`SameSite` testada sob TLS/proxy HTTPS — só o caminho de sucesso (`setRefreshCookie`) tinha essa cobertura. Corrigido com um novo teste; todos os demais achados dos 4 revisores paralelos (blind-hunter, edge-case-hunter, verification-gap, intent-alignment) foram avaliados e rejeitados como ruído, repetição de achados já revisados/rejeitados/deferidos nas duas passagens anteriores desta mesma spec, ou fora de escopo pela própria autoridade do `<intent-contract>` (seção `Never`) — incluindo achados sobre `scripts/notify_telegram.py`, um script de orquestração do bmad-loop sem nenhuma relação com esta story, presente no diff apenas por coincidência de intervalo de commits.

**Arquivos alterados nesta passagem:**
- `backend/handlers/auth_test.go` -- adicionado `TestRefreshHandler_CookieLimpoSecure` (2 subtestes: TLS direto e proxy `X-Forwarded-Proto: https`), fechando o gap de verificação do atributo `Secure`/`SameSite` no cookie de refresh limpo.

**Achados desta passagem:**
- `patch`: 1 (low) -- aplicado (ver Review Triage Log acima).
- `defer`: 0 -- nenhum item novo; um achado repetiu, sem fato novo, item já presente em `deferred` (frontmatter) de passagem anterior.
- `reject`: 22 (low) -- ruído/repetição/fora de escopo (detalhamento completo no Review Triage Log acima).

**Recomendação de review de acompanhamento:** `false` -- nesta passagem, patch: 0 high, 0 medium, 1 low → score = 3×0 + 1×1 = 1 (< 5), sem high.

**Verificação executada:**
- `cd backend && go build ./...` -- OK, build limpo.
- `cd backend && go vet ./...` -- OK, sem warnings.
- `go test -p 1 ./...` -- Docker não está disponível neste sandbox (`docker: command not found`); em vez de `docker compose up -d db`, foi inicializado um cluster Postgres 16 descartável via `initdb`/`pg_ctl` (binários do pacote `postgresql-16` já instalados no host), com role/db `stockflow` e extensão `pgcrypto`, e `DATABASE_URL` apontado para ele. **Todos os testes de integração passam** nos 5 pacotes (`backend`, `backend/cmd/seed-admin`, `backend/handlers`, `backend/middleware`, `backend/services`), incluindo o novo `TestRefreshHandler_CookieLimpoSecure`. O cluster descartável foi parado e removido ao final (nenhum artefato de teste permanece).
- `cd frontend && npm run build && npm run lint && npm run test` -- OK: build limpo, `oxlint` sem achados, 7 arquivos de teste / 50 testes passando.
- `docker compose up --build` -- **não executado**: Docker não está disponível neste sandbox (mesma limitação acima). A composição real API+web+proxy não foi verificada nesta passagem; a cobertura de integração via Postgres real acima cobre o mesmo comportamento de backend que o compose exercitaria.

**Riscos residuais:**
- A verificação end-to-end via `docker compose up --build` (API+web+proxy juntos) não pôde ser executada neste sandbox por ausência do binário `docker` -- risco residual baixo, já que toda a superfície HTTP relevante (login, refresh, `/me`) foi exercitada via testes de integração reais contra Postgres através do mux real (`newMux`), a mesma composição usada em produção.
- Os 22 achados rejeitados nesta passagem não representam risco novo: são a mesma classe de hardening/tópicos já avaliados e conscientemente aceitos como fora de escopo nas duas passagens de review anteriores desta spec, ou pertencem a uma ferramenta não relacionada (`notify_telegram.py`).

