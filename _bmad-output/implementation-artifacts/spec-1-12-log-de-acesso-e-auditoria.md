---
title: 'Story 1.12 — Log de acesso e auditoria'
type: 'feature'
created: '2026-08-30'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '11bc8820566b028a81419518781b9beb8ff6836f'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      Tentativas no SEGUNDO fator (código TOTP errado em POST /api/auth/mfa/verificar,
      e o bloqueio na 6ª falha de código) não geram linha em `logs_acesso` — só a
      etapa de senha aparece, marcada `sucesso=true`.
    evidence: |-
      A AC do épico enumera o método como "senha ou SSO"; MFAVerificarHandler
      (backend/handlers/auth_mfa.go) não chama registrarTentativaLogin. A força
      bruta de código já é contada pelo lockout da Story 1.10, mas o `adm` não
      enxerga no log de acesso quando um segundo fator falhou repetidamente.
      Decisão deliberada da spec (`<intent-contract>` → Never), registrada aqui.
    location: >-
      backend/handlers/auth_mfa.go (MFAVerificarHandler)
    severity: medium
  - summary: >-
      Não há rotina de expurgo/arquivamento dos 12 meses de retenção que o PRD §9
      define para o log de acesso — a tabela `logs_acesso` cresce sem teto.
    evidence: |-
      PRD §9 deixa a política de retenção "a detalhar na Arquitetura" e a
      ARCHITECTURE-SPINE não a fixa. Uma story de operação futura precisa de um
      job de purge/partição por tempo. Cada tentativa de login falha (inclusive
      contra e-mail inexistente) grava uma linha + entrada de índice.
    location: >-
      backend/migrations/000007_create_logs_acesso.up.sql
    severity: low
  - summary: >-
      GET /api/logs-acesso só filtra por período — não por resultado
      (sucesso/falha) nem por método — e `logs_acesso` não tem índice em
      `sucesso`, `email_informado` ou `usuario_id`.
    evidence: |-
      O uso primário de um log de acesso é "mostre as falhas" ou "as falhas de
      uma conta"; hoje isso é varredura completa da tabela. Fora do escopo da AC
      do épico (que pede só filtro por período), mas provável necessidade quando
      o volume crescer sob credential stuffing.
    location: >-
      backend/services/logs_acesso.go (ListarLogsAcesso)
    severity: low
---

<intent-contract>

## Intent

**Problem:** FR-38/NFR-3 exigem que TODA tentativa de login (sucesso ou falha, por senha ou SSO) fique registrada de forma append-only com usuário (quando identificável), timestamp, IP e método, consultável só por `adm` — hoje `handlers.LoginHandler` e `handlers.KeycloakSSOHandler` não persistem nada sobre a tentativa, e não existe tabela nem rota de consulta.

**Approach:** Nova tabela `logs_acesso` (migration `000007`) recebe uma linha por tentativa de login. Um novo serviço `services.RegistrarTentativaLogin` faz um único `INSERT` não-fatal, chamado pelos dois handlers de login no ponto em que o desfecho da tentativa já é conhecido. `email_informado` é sempre gravado (o `adm` vê a tentativa mesmo sem conta correspondente), mas o handler nunca muda a resposta ao solicitante — a não-enumeração de e-mail da Story 1.4/1.6 fica intacta. Uma nova rota `GET /api/logs-acesso` atrás de `RequireAuth → RequireRole(adm)` lista os registros filtráveis por período. No frontend, uma nova seção "Log de Acesso" em `ConfiguracoesPage`, montada só para `adm`, mostra uma tabela somente-leitura (sem nenhuma ação de edição/exclusão).

## Boundaries & Constraints

**Always:**
- **Uma linha de `logs_acesso` por tentativa de login concluída**, gravada por `services.RegistrarTentativaLogin(db, RegistroTentativaLogin{...})` — `db.Exec` único, sem transação, **não-fatal**: falha só emite `slog.Warn` e o fluxo de login segue (mesmo precedente de `registrarFalhaLogin`, services/auth.go:464). Nunca transforma um login em `500`.
- **`metodo`** é `'senha'` (chamada de `LoginHandler`) ou `'sso'` (chamada de `KeycloakSSOHandler`) — mesmo vocabulário de `sessoes.origem`/claim `origem`. Nenhum outro valor.
- **`sucesso`** (booleano) para `metodo='senha'` = `services.Login` retornou `nil` (senha primária aceita), **independente de MFA depois ser exigido** — o registro reflete o fator senha, não a emissão de sessão. Para `metodo='sso'` = o handler chegou em `emitirSessaoEResponder(...,"sso")`.
- **`usuario_id`** é preenchido só quando a conta é identificável sem ferir a não-enumeração: sucesso por senha (`usuarioID` de `services.Login`), e qualquer desfecho do SSO em que `BuscarUsuarioPorEmailSSO` achou a conta (inclusive conta inativa). Falha de senha (`ErrCredenciaisInvalidas`/`ErrContaBloqueada`) e SSO sem conta / e-mail não verificado gravam `usuario_id = NULL`.
- **`email_informado`** sempre gravado, `NOT NULL`, normalizado por `services.normalizeEmail` (senha: `req.Email`; SSO: e-mail do token), truncado a 255 runes para caber na coluna.
- **`ip`** vem de `handlers.ipDaRequisicao(r)`: primeiro salto de `X-Forwarded-For` se presente (mesmo motivo de `cookieEhSeguro` já confiar em `X-Forwarded-Proto`, auth.go:165), senão o host de `r.RemoteAddr` (`net.SplitHostPort`, com fallback ao valor bruto). `NOT NULL`; string vazia vira `'desconhecido'`.
- **Tentativa NÃO registrada** quando não há identidade de tentativa real: `services.ErrLoginValidacao` (e-mail ou senha em branco no login por senha) e SSO com e-mail vazio no token (`400 VALIDATION_ERROR`). Erro de infraestrutura (`500` de `services.Login`) também não gera linha. Documentar nas Design Notes.
- **`GET /api/logs-acesso`** registrado em `newMux` como `middleware.RequireAuth(db, jwtSecret)(middleware.RequireRole(services.PapelAdm)(handlers.ListarLogsAcessoHandler(db)))` — mesma composição de `GET /api/usuarios` (main.go:221-223). `RequireRole(services.PapelAdm)` reaproveita o gate de papel + MFA da Story 1.11 de graça.
- **Filtro por período**: query params `inicio` e `fim`, ambos opcionais, aceitos em RFC3339 **ou** `YYYY-MM-DD`; `fim` só com data é tratado como fim de dia inclusivo. Formato inválido → `400 VALIDATION_ERROR`. Resultado ordenado por `criado_em DESC`, limitado à constante `maxLogsAcessoPorConsulta = 500`.
- **Resposta** `200 {"logs":[{id, usuarioId (nullable), usuarioNome (nullable), emailInformado, metodo, sucesso, ip, criadoEm}]}` — `usuarioNome` vem de `LEFT JOIN usuarios` no serviço (o `adm` vê quem foi sem outra chamada; conta anonimizada pela Story 8.2 exibe o nome anonimizado, o que é correto).
- **Append-only**: nenhuma rota/handler/serviço de `UPDATE` ou `DELETE` sobre `logs_acesso` — só o `INSERT` de `RegistrarTentativaLogin` e o `SELECT` de `ListarLogsAcesso`. `usuario_id` usa `ON DELETE SET NULL` (não `CASCADE` como `sessoes`/`tokens_acao`): a trilha de auditoria sobrevive a uma eventual remoção de conta.
- **Frontend**: nova `LogAcessoSection` em `frontend/src/components/logs/LogAcessoSection.tsx`, renderizada em `ConfiguracoesPage` só quando `rankPapel(papel) >= rankPapel('adm')` — mesmo padrão de gate de `GestaoUsuariosSection`/"Decidir promoções". Tabela `<table>` somente-leitura (Data/Hora, Usuário, E-mail informado, Método, Resultado); dois `<input type="date">` (início/fim) + botão "Filtrar" repassando os valores crus como query params; carga inicial sem filtro no mount. Erro de carga → `<p role="alert">` inline (molde de `GestaoUsuariosSection`). Nenhum botão de ação em nenhuma linha.

**Block If:** nada nesta story depende de decisão humana nem de ação de operador fora do repositório — tabela, endpoint e UI são inteiramente implementáveis por um agente. Status final esperado: `done`.

**Never:**
- Nenhuma mudança no corpo, status ou latência perceptível das respostas de `LoginHandler`/`KeycloakSSOHandler` — registrar a tentativa nunca revela ao solicitante se o e-mail existe (FR-32).
- Nenhum item novo no rail (`nav-items.ts` intocado): "Log de Acesso" é seção dentro de Configurações (EXPERIENCE.md IA), visível só a `adm` — a AC de "o item de navegação nem aparece" é atendida pela seção não ser montada para não-`adm`.
- Nenhuma captura do segundo fator: um código TOTP errado em `POST /api/auth/mfa/verificar` não gera linha em `logs_acesso` (`metodo` é senha/sso only; a força bruta de código já é contada pela Story 1.10). Registrado em `deferred`.
- Nenhuma rotina de expurgo/arquivamento dos 12 meses (PRD §9) — política de retenção fica para uma story de operação futura.
- Nenhuma tabela ou coluna nova além de `logs_acesso`; nenhuma mudança em `usuarios`, `sessoes` ou `tokens_acao`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Login por senha bem-sucedido | `POST /login`, senha correta | Resposta 200 igual a hoje; 1 linha `logs_acesso` `metodo='senha'`, `sucesso=true`, `usuario_id` preenchido, `email_informado`, `ip` | — |
| Login por senha, MFA exigido | `POST /login`, senha correta, `mfa_habilitado=true` | Resposta `{"mfaRequerido":true,...}` igual a hoje; ainda **1 linha** `sucesso=true` `metodo='senha'` | — |
| Senha errada / e-mail inexistente | `POST /login`, credencial inválida | Resposta `401 INVALID_CREDENTIALS` igual a hoje; 1 linha `sucesso=false`, `usuario_id=NULL`, `email_informado` = e-mail tentado | linha gravada mesmo no caminho de erro |
| Conta bloqueada (Story 1.10) | `POST /login`, `bloqueado_ate` no futuro | Resposta `429 ACCOUNT_LOCKED` igual a hoje; 1 linha `sucesso=false`, `usuario_id=NULL` | — |
| Campos em branco no login | `POST /login`, e-mail ou senha `""` | `400 VALIDATION_ERROR`; **nenhuma** linha `logs_acesso` | — |
| Falha do `INSERT` de log | `logs_acesso` indisponível durante um login | Login responde normalmente (200/401/429); `slog.Warn` | erro engolido, nunca 500 |
| SSO bem-sucedido | `POST /sso/keycloak`, token válido, conta ativa | Resposta 200 igual a hoje; 1 linha `metodo='sso'`, `sucesso=true`, `usuario_id` preenchido | — |
| SSO sem conta local | token válido, e-mail sem match | `401 SSO_SEM_CONTA` igual a hoje; 1 linha `sucesso=false`, `usuario_id=NULL`, `email_informado` = e-mail do token | — |
| SSO e-mail não verificado no token | `email_verified=false` | `401 EMAIL_NOT_VERIFIED` igual a hoje; 1 linha `sucesso=false`, `usuario_id=NULL` | — |
| SSO token sem e-mail | claim `email` ausente | `400 VALIDATION_ERROR`; **nenhuma** linha | — |
| `adm` consulta o log | `GET /api/logs-acesso` com sessão `adm` | `200 {"logs":[...]}` ordenado por `criado_em DESC`, no máx. 500 | — |
| `adm` filtra por período | `GET /api/logs-acesso?inicio=2026-08-01&fim=2026-08-15` | só linhas com `criado_em` no intervalo (`fim` inclusivo até o fim do dia) | `inicio`/`fim` malformado → `400 VALIDATION_ERROR` |
| Não-`adm` consulta o log | `GET /api/logs-acesso` com sessão `usuario`/`almoxarife`/`gestor` | `403 FORBIDDEN`, corpo é o envelope de erro (nunca `{"logs":...}`); handler nunca executa | — |
| Sem autenticação | `GET /api/logs-acesso` sem `Authorization` | `401 TOKEN_EXPIRED` | — |
| Frontend, sessão não-`adm` | `ConfiguracoesPage` montada por `gestor`/`usuario` | seção "Log de Acesso" não é renderizada | — |

</intent-contract>

## Code Map

- `backend/migrations/000007_create_logs_acesso.{up,down}.sql` (novo) — `up`: `CREATE TABLE logs_acesso (id UUID PK DEFAULT gen_random_uuid(), usuario_id UUID REFERENCES usuarios(id) ON DELETE SET NULL, email_informado VARCHAR(255) NOT NULL, metodo VARCHAR(10) NOT NULL CHECK (metodo IN ('senha','sso')), sucesso BOOLEAN NOT NULL, ip VARCHAR(64) NOT NULL, criado_em TIMESTAMPTZ NOT NULL DEFAULT now())` + `CREATE INDEX idx_logs_acesso_criado_em ON logs_acesso (criado_em DESC)`. `down`: `DROP TABLE IF EXISTS logs_acesso`. Comentário no molde das migrations anteriores (Story 1.12, FR-38/NFR-3; por que `SET NULL` e não `CASCADE`).
- `backend/services/logs_acesso.go` (novo) — `type RegistroTentativaLogin struct { UsuarioID *string; EmailInformado, Metodo, IP string; Sucesso bool }`; `RegistrarTentativaLogin(db *sql.DB, r RegistroTentativaLogin) error` (normaliza/trunca `EmailInformado` via `normalizeEmail`, default `ip=''→'desconhecido'`, `INSERT` único); `type LogAcesso struct` com tags JSON (`id`,`usuarioId`,`usuarioNome`,`emailInformado`,`metodo`,`sucesso`,`ip`,`criadoEm`; `UsuarioID *string`/`UsuarioNome *string`); `const maxLogsAcessoPorConsulta = 500`; `ListarLogsAcesso(db *sql.DB, inicio, fim *time.Time) ([]LogAcesso, error)` (`SELECT l.*, u.nome FROM logs_acesso l LEFT JOIN usuarios u ON u.id = l.usuario_id WHERE ($1::timestamptz IS NULL OR l.criado_em >= $1) AND ($2::timestamptz IS NULL OR l.criado_em <= $2) ORDER BY l.criado_em DESC LIMIT 500`; lista vazia não é erro; molde de `ListarUsuarios`, services/usuarios.go).
- `backend/services/logs_acesso_test.go` (novo) — `RegistrarTentativaLogin` grava os campos certos (sucesso, falha com `usuario_id` NULL, truncamento de e-mail, `ip` default); `ListarLogsAcesso` filtra por período (limite inferior, superior, ambos), ordena `DESC`, respeita o `LIMIT 500`, traz `usuarioNome` do join e `nil` quando `usuario_id` é NULL.
- `backend/handlers/logs_acesso.go` (novo) — `ipDaRequisicao(r *http.Request) string` (X-Forwarded-For 1º salto → `net.SplitHostPort(r.RemoteAddr)` → bruto); `registrarTentativaLogin(r *http.Request, db *sql.DB, metodo, emailInformado string, usuarioID *string, sucesso bool)` (helper fino: monta `RegistroTentativaLogin` com `ipDaRequisicao`, chama o serviço, `slog.Warn` em erro); `ListarLogsAcessoHandler(db *sql.DB) http.HandlerFunc` (parseia `inicio`/`fim` de `r.URL.Query()` com `parsePeriodoLog` aceitando RFC3339 ou `2006-01-02` — data pura em `fim` vira `23:59:59.999999` do dia; inválido → `400 VALIDATION_ERROR`; chama `services.ListarLogsAcesso`; `escreverJSON(w, 200, map[string]any{"logs": logs})`; guard de contexto ausente → 500, igual a `MeHandler`).
- `backend/handlers/logs_acesso_test.go` (novo) — despacha pela composição real `RequireAuth→RequireRole(PapelAdm)→ListarLogsAcessoHandler` (molde de `getUsuarios`, usuarios_test.go:158): `adm` → 200 + linhas; `usuario`/`almoxarife`/`gestor` → 403 e corpo é envelope de erro; sem `Authorization` → 401; `inicio`/`fim` malformado → 400; filtro de período restringe o conjunto.
- `backend/handlers/auth.go` — `LoginHandler` (auth.go:250-302): no `switch` de `services.Login`, ramos `ErrCredenciaisInvalidas` e `ErrContaBloqueada` chamam `registrarTentativaLogin(r, db, "senha", req.Email, nil, false)` **antes** do `return`; ramo `ErrLoginValidacao` e `default` (500) **não** registram; no caminho de sucesso, após `usuarioID` resolvido (linha ~262, ou logo após `BuscarUsuarioSessao` na ~279 usando `usuario.ID`), `registrarTentativaLogin(r, db, "senha", req.Email, &usuarioID, true)` — uma vez, antes da bifurcação de MFA (auth.go:286). Atualizar o comentário-doc do handler.
- `backend/handlers/auth_sso.go` — `KeycloakSSOHandler` (auth_sso.go:55-95): ramo `email == ""` **não** registra; ramo `!EmailVerificadoSSO` chama `registrarTentativaLogin(r, db, "sso", email, nil, false)`; ramo `ErrContaSSONaoEncontrada` idem com `nil`; ramo `!usuario.Ativo` chama `registrarTentativaLogin(r, db, "sso", email, &usuario.ID, false)`; no sucesso, antes de `emitirSessaoEResponder(...,"sso")` (auth_sso.go:93), `registrarTentativaLogin(r, db, "sso", email, &usuario.ID, true)`. Ramo `err != nil` genérico (500) **não** registra.
- `backend/handlers/auth_test.go` / `auth_sso_test.go` — novos casos: pós-login por senha (sucesso e falha) a contagem/linha esperada em `logs_acesso` (helper `contarLogsAcesso`/`ultimoLogAcesso` no molde de `contarSessoes`, auth_sso_test.go:105); campos em branco não geram linha; SSO sucesso grava `metodo='sso'`/`sucesso=true`/`usuario_id`; SSO sem conta grava `sucesso=false`/`usuario_id NULL`/`email_informado` = e-mail do token.
- `backend/handlers/auth_mfa_test.go` — 1 caso: login que dispara MFA ainda grava exatamente uma linha `metodo='senha'`/`sucesso=true`.
- `backend/main.go` — `newMux`: registrar `GET /api/logs-acesso` com a composição `RequireAuth(...)(RequireRole(services.PapelAdm)(handlers.ListarLogsAcessoHandler(db)))`, ao lado do bloco de rotas `RequireRole` (main.go:221-239). Atualizar o comentário-doc do pacote (linhas 1-17) mencionando a Story 1.12.
- `backend/main_test.go` — se houver teste que enumera as rotas do mux, incluir a nova.
- `frontend/src/components/logs/LogAcessoSection.tsx` (novo) — `Card` no molde de `GestaoUsuariosSection.tsx`: `authHeaders()` (via `getAccessToken`), `useAuth()` para o gate `rankPapel(papel) >= rankPapel('adm')` (retorna `null` se não), `useState` para `logs`, `inicio`, `fim`, `erroCarregar`; `carregar` faz `GET /api/logs-acesso` montando `?inicio=&fim=` só com os valores preenchidos; `useEffect` chama `carregar` no mount; `<table>` somente-leitura com `<thead>` Data/Hora · Usuário · E-mail informado · Método · Resultado e uma `<tr>` por log (`usuarioNome ?? '—'`, `metodo === 'sso' ? 'SSO' : 'Senha'`, `sucesso ? 'Sucesso' : 'Falha'`, `new Date(criadoEm).toLocaleString('pt-BR')`); dois `<input type="date">` rotulados + `<Button>` "Filtrar" que chama `carregar`; sem nenhum `<Button>` de ação por linha; erro → `<p role="alert" className="text-body text-destructive">`.
- `frontend/src/components/logs/LogAcessoSection.test.tsx` (novo) — render com `fetch` mockado devolvendo linhas: tabela mostra as colunas e os valores mapeados; não há botão de editar/excluir; preencher datas + "Filtrar" refaz o `fetch` com `?inicio=...&fim=...`; `res.ok === false` no mount → `role="alert"`.
- `frontend/src/pages/ConfiguracoesPage.tsx` — importar e renderizar `<LogAcessoSection />` após `{podeDecidir && <GestaoUsuariosSection />}` (linha ~503), com o gate `rankPapel(papel) >= rankPapel('adm')` inline (o próprio componente também se auto-protege, como `GestaoUsuariosSection`). Atualizar o comentário-doc (linhas 14-43) listando a nova seção.
- `frontend/src/pages/ConfiguracoesPage.test.tsx` — novos casos: seção "Log de Acesso" presente para `adm`, ausente para `gestor` e `usuario`.
- `frontend/src/components/shell/nav-items.ts` — **não** modificado; `rankPapel` (nav-items.ts:43) é reaproveitado pelo gate.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000007_create_logs_acesso.{up,down}.sql` — tabela `logs_acesso` append-only + índice por `criado_em DESC`.
- `backend/services/logs_acesso.go` (+ `logs_acesso_test.go`) — `RegistrarTentativaLogin` (INSERT não-fatal) e `ListarLogsAcesso` (filtro por período, `LIMIT 500`, join do nome).
- `backend/handlers/logs_acesso.go` (+ `logs_acesso_test.go`) — `ipDaRequisicao`, helper `registrarTentativaLogin`, `ListarLogsAcessoHandler` com parse de `inicio`/`fim`.
- `backend/handlers/auth.go` — `LoginHandler` registra a tentativa nos ramos de sucesso e de falha de credencial; nunca nos ramos de validação/500.
- `backend/handlers/auth_sso.go` — `KeycloakSSOHandler` registra a tentativa nos ramos de sucesso, e-mail não verificado, sem conta e conta inativa.
- `backend/handlers/auth_test.go` / `auth_sso_test.go` / `auth_mfa_test.go` — cobertura da Matrix (linha gravada / não gravada, campos por cenário).
- `backend/main.go` (+ `main_test.go`) — rota `GET /api/logs-acesso` atrás de `RequireAuth → RequireRole(adm)`; doc do pacote atualizada.
- `frontend/src/components/logs/LogAcessoSection.tsx` (+ teste) — seção só-`adm`, tabela somente-leitura, filtro por período.
- `frontend/src/pages/ConfiguracoesPage.tsx` (+ teste) — monta `<LogAcessoSection />` só para `adm`.

**Acceptance Criteria:**
- Given qualquer tentativa de login por senha ou SSO (sucesso ou falha), when ela ocorre, then existe exatamente uma linha em `logs_acesso` com `metodo` (`senha`/`sso`), `criado_em`, `ip` e `usuario_id` preenchido quando a conta é identificável — e a resposta HTTP ao solicitante é byte-idêntica à de antes desta story.
- Given uma tentativa de login com e-mail inexistente, when ela é registrada, then a linha tem `usuario_id = NULL` e `email_informado` com o e-mail tentado, e nenhuma diferença de status/corpo/latência revela ao solicitante se o e-mail existe.
- Given uma sessão `adm` em `GET /api/logs-acesso` com filtro de período, when a consulta roda, then a resposta é `200` com as linhas do intervalo ordenadas do mais recente ao mais antigo, e a tela "Log de Acesso" em Configurações as mostra em tabela sem nenhuma ação de edição ou exclusão.
- Given uma sessão de papel abaixo de `adm`, when ela chama `GET /api/logs-acesso`, then a resposta é `403` (corpo do envelope de erro, nunca `{"logs":...}`) e a seção "Log de Acesso" não é renderizada em Configurações para essa conta.
- Given a falha do `INSERT` em `logs_acesso` durante um login, when o login prossegue, then a resposta continua `200`/`401`/`429` conforme o caso e nenhum `500` é retornado por causa do registro de auditoria.

## Review Triage Log

### 2026-08-30 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 10: (high 0, medium 3, low 7)
- defer: 3: (medium 1, low 2)
- reject: 9: (high 0, medium 0, low 9)
- addressed_findings:
  - `[low]` `[patch]` A rota real `GET /api/logs-acesso` não tinha teste através do `newMux` que fixasse o argumento `RequireRole(services.PapelAdm)` (o caso sem-token → 401 é insensível ao papel). Adicionado `TestNewMux_LogsAcessoRotaCarregaRequireRole` no molde de `TestNewMux_UsuariosRotaCarregaRequireRole`: `usuario`/`almoxarife`/`gestor` → 403 FORBIDDEN, `adm` → 200.
  - `[low]` `[patch]` O ramo RFC3339 de `parsePeriodoLog` ficava sem cobertura (os testes só passavam data pura ou valor inválido). Adicionado `TestListarLogsAcessoHandler_FiltroRFC3339` com `inicio`/`fim` em RFC3339 completo e linhas straddling os instantes.
  - `[low]` `[patch]` Todo teste decodificava a resposta em `services.LogAcesso` (a mesma struct que a serializou), então uma renomeação de tag JSON round-tripava sem quebrar nada. `TestListarLogsAcessoHandler_AdmRecebe200ComLinhas` agora também decodifica em `[]map[string]any` e trava o conjunto exato de chaves de fio (`id, usuarioId, usuarioNome, emailInformado, metodo, sucesso, ip, criadoEm`).
  - `[medium]` `[patch]` Um `X-Forwarded-For` longo/não-IP na rota pública de login fazia o `INSERT` em `logs_acesso.ip VARCHAR(64)` falhar (não-fatal → linha de auditoria descartada), permitindo suprimir a própria trilha. `ipDaRequisicao` passa a só aceitar o 1º salto do XFF quando `net.ParseIP` valida; `RegistrarTentativaLogin` trunca `ip` a 64 chars (espelho do truncamento de `email` a 255). Novo teste `TestLoginHandler_XForwardedForLixoNaoSuprimeAuditoria`.
  - `[medium]` `[patch]` A tabela do frontend capturava `ip` no modelo mas nunca o exibia, contrariando a EXPERIENCE.md ("tabela com usuário/timestamp/IP/método"). Adicionada a coluna "IP" entre "E-mail informado" e "Método"; testes atualizados.
  - `[medium]` `[patch]` `<input type="date">` produz uma data-calendário local, mas o backend parseava `AAAA-MM-DD` puro como UTC — skew de 3h nos dois extremos para pt-BR (UTC-3). `LogAcessoSection` passa a converter cada valor do seletor para um instante RFC3339 dos limites do dia LOCAL (`00:00:00` / `23:59:59.999`) antes de montar a query; o teste de filtro calcula o esperado com a mesma lógica (timezone-agnóstico).
  - `[low]` `[patch]` O `LIMIT 500` era silencioso — o `adm` não sabia se estava vendo um recorte. `LogAcessoSection` agora mostra uma linha "Mostrando os 500 registros mais recentes do período. Refine o filtro…" quando `logs.length === 500`. Dois testes novos.
  - `[low]` `[patch]` `LogAcessoSection` não tinha estado de carregamento (flash de "Nenhum registro no período." antes da 1ª carga) nem guarda contra respostas fora de ordem. Adicionados `carregando` (gate da mensagem vazia) e um contador de sequência via `useRef` que descarta respostas obsoletas. Teste de out-of-order novo.
  - `[low]` `[patch]` O comentário-doc de `LoginHandler` afirmava "nunca altera … a latência" — um `INSERT` síncrono adiciona latência ínfima porém real. Reformulado para "nunca altera o corpo nem o status da resposta ao solicitante (é um único INSERT indexado, não-fatal)".
  - `[low]` `[patch]` Typo no comentário-doc de `backend/services/logs_acesso.go` (`maxLogsAcessoPorconsulta` → `maxLogsAcessoPorConsulta`).
- deferred nesta passagem:
  - `[medium]` Segundo fator (código TOTP errado / bloqueio na 6ª falha em `POST /api/auth/mfa/verificar`) não gera linha em `logs_acesso` — decisão deliberada do `<intent-contract>` (Never), agora registrada na lista `deferred` (o texto das Design Notes já prometia isso mas o campo estava vazio).
  - `[low]` Sem rotina de expurgo/arquivamento dos 12 meses de retenção (PRD §9).
  - `[low]` Sem filtro por resultado/método em `GET /api/logs-acesso` e sem índice em `sucesso`/`email_informado`/`usuario_id`.
- rejeitados de nota (contexto que o revisor cego não tinha):
  - `sucesso=true` no login por senha que ainda exige MFA: definição deliberada e documentada nas Design Notes (`sucesso` = "o fator senha foi aceito"). Um brute-forcer que conhece a senha mas não o TOTP gerando linhas `sucesso=true` é justamente o sinal "suspeito" que o `adm` quer ver.
  - Append-only só por convenção (sem trigger/`REVOKE`): a AC do épico exige append-only "na interface" — atendido (nenhuma rota de escrita). `sessoes`/`tokens_acao` também não têm proteção de nível de banco; segue a norma do código.
  - `ON DELETE SET NULL` "reescreve" linhas de auditoria: não há hard-delete de `usuarios` no produto; guardar um snapshot denormalizado de nome/e-mail (sugestão do revisor) DERROTARIA a anonimização da LGPD (Story 8.2) — manter o FK + join no read é o correto.
  - `usuario_id = NULL` para `ErrContaBloqueada`: `services.Login` não devolve o id nesses ramos (não-enumeração); `email_informado` já permite correlação. Decisão uniforme e documentada no `Always`.
  - `email_informado` normalizado (minúsculas/trim): deliberado e consistente com o armazenamento de `usuarios.email`.
  - `slog.Warn` (e não `Error`) na falha do INSERT de auditoria: mesmo nível do precedente `registrarFalhaLogin` para bookkeeping não-fatal; infra de métricas/alerta não existe neste código.
  - Allowlist de proxy confiável para `X-Forwarded-For`, meta-auditoria de quem lê o log, e os campos de id/htmlFor dos inputs de data (que já existem no código): fora do escopo da AC / já atendido.

### 2026-08-30 — Review pass (follow-up)
- intent_gap: 0
- bad_spec: 0
- patch: 2: (high 0, medium 0, low 2)
- defer: 0
- reject: 18: (high 0, medium 0, low 18)
- addressed_findings:
  - `[low]` `[patch]` `ListarLogsAcesso` ordenava só por `l.criado_em DESC`, sem desempate — duas tentativas de login no mesmo microssegundo (plausível sob credential stuffing, justo quando o log importa) compartilham `criado_em` e a fronteira do `LIMIT 500` ficaria não-determinística entre consultas. `ORDER BY` passa a `l.criado_em DESC, l.id DESC` (`backend/services/logs_acesso.go`), com comentário. Suíte de integração real (`go test -p 1 ./...`) segue verde.
  - `[low]` `[patch]` O aviso de resultado no teto ("Mostrando os 500 registros mais recentes do período. Refine o filtro para ver outros.") afirmava que havia linhas ocultas mesmo quando o período tem exatamente 500 e nada foi truncado. Reformulado para "Cada consulta mostra no máximo 500 registros (os mais recentes do período). Refine o período se precisar ver além disso." — verdadeiro nos dois casos; `LogAcessoSection.test.tsx` atualizado.
- rejeitados de nota (revisitados ou já cobertos por passagem anterior / `<intent-contract>`):
  - Segundo fator (TOTP errado em `POST /api/auth/mfa/verificar`) não gera linha — já em `deferred` (entrada #1); `<intent-contract>` → Never. Sem ação.
  - Sem rotina de retenção/expurgo e sem filtro por resultado/método/índices — já em `deferred` (#2, #3); `<intent-contract>` → Never / Always (só período). Sem ação.
  - `usuario_id = NULL` em `ErrContaBloqueada`: mandatado pelo `<intent-contract>` → Always. Sem ação.
  - `sucesso=true` gravado antes de `emitirSessaoEResponder`/`IniciarLoginMFA` poder falhar com 500: `<intent-contract>` define `sucesso` (senha) = "`services.Login` retornou `nil`", desacoplado da emissão de sessão; SSO = "chegou em `emitirSessaoEResponder`". Comportamento é o especificado.
  - Allowlist de proxy confiável para XFF, `slog.Warn` (e não `Error`) na falha do INSERT, sem métricas/alerta: já rejeitados na passagem anterior; sem infra de métricas no código.
  - Intervalo invertido (`inicio > fim`) → `200` com lista vazia: contrato de API defensável (nenhuma linha casa); `<intent-contract>` só pede `400` para formato inválido. Baixo.
  - `ip[:64]` poderia partir uma rune multibyte: `r.RemoteAddr` do `net/http` é sempre ASCII (`IP:porta`); gatilho não é alcançável.
  - Coluna IP extra, conversão de fuso no seletor de data, gate `net.ParseIP` no XFF: divergências apontadas pelo auditor de intenção que são justamente os patches deliberados da passagem anterior — funcionando como pretendido.
  - Logout/refresh não registrados, sem export CSV, sem `<caption>`/`scope="col"`/`<time>`, fragilidade de isolamento por `TRUNCATE` (suíte roda `-p 1` por convenção): fora do escopo do FR-38 / cosméticos / convenção da suíte.

## Design Notes

- **Por que `sucesso=true` no login por senha que ainda exige MFA:** o campo `sucesso` não é ditado pela AC (que só pede "registro para sucesso e falha") — é derivado para a visibilidade de "acessos legítimos e suspeitos". A definição escolhida para `metodo='senha'` é "o fator senha foi aceito" (`services.Login` retornou `nil`). O segundo fator é um passo distinto, fora do vocabulário `senha|sso` do log; um código TOTP errado já é contabilizado pelo lockout da Story 1.10. Fica em `deferred` a ausência desse evento no log de acesso.
- **Por que não registrar campos em branco / erros 500:** "e-mail e senha são obrigatórios" é rejeitado antes de qualquer consulta (`ErrLoginValidacao`) — não há identidade de tentativa nem e-mail utilizável; gravar linhas vazias a cada request malformado seria ruído e um vetor de crescimento não-limitado numa rota não autenticada. Erro de infraestrutura não é um desfecho de autenticação.
- **Por que `ON DELETE SET NULL` (e não `CASCADE` como `sessoes`/`tokens_acao`):** o log é trilha de auditoria — apagar registros junto com a conta destruiria justamente a evidência. Não há fluxo de remoção de linha em `usuarios` no produto (só desativação, Story 1.8; anonimização em `UPDATE`, Story 8.2 — que preserva `usuario_id`), então na prática `usuario_id` nunca fica NULL por esse caminho; o `SET NULL` é a rede de segurança correta caso um hard-delete passe a existir.
- **INSERT síncrono no caminho de login:** um `INSERT` indexado a mais é desprezível ao lado do `bcrypt` que o login já executa (~100ms) — não fere o critério de sucesso de "não deixar o fluxo perceptivelmente mais lento".

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` — Postgres real (mesmo setup das Stories 1.5–1.11). Cobre `services/logs_acesso_test.go`, `handlers/logs_acesso_test.go` e os novos casos de `handlers/auth_test.go`/`auth_sso_test.go`/`auth_mfa_test.go`; a migration `000007` aplica sem erro.
- `cd frontend && npm run lint && npm run build && npm run test` — `oxlint`, `tsc`+`vite` e os novos casos de `LogAcessoSection.test.tsx`/`ConfiguracoesPage.test.tsx` passam.
- `docker compose up --build` — um login por senha malsucedido seguido de um bem-sucedido geram duas linhas em `SELECT metodo, sucesso, usuario_id, email_informado, ip FROM logs_acesso ORDER BY criado_em`; um `adm` vê ambas em Configurações → Log de Acesso e não encontra nenhum botão de editar/excluir; um `gestor` recebe `403` em `GET /api/logs-acesso`. Se `docker` indisponível, mesma nota das stories anteriores (cobertura equivalente via testes de integração contra Postgres real).

**Manual checks (if no CLI):**
- Tentar login com um e-mail que não existe; conferir `SELECT * FROM logs_acesso` com `usuario_id IS NULL`, `email_informado` igual ao tentado, `metodo='senha'`, `sucesso=false`, `ip` preenchido, e que a resposta HTTP foi o mesmo `401 INVALID_CREDENTIALS` de sempre.
- Logar como `adm`, abrir Configurações → Log de Acesso, filtrar por um intervalo de datas e confirmar que só linhas do intervalo aparecem, em ordem decrescente, sem nenhuma ação de linha.

## Auto Run Result

Status: done

### Resumo da mudança implementada
Story 1.12 — log de acesso e auditoria (FR-38/NFR-3). Nova tabela append-only `logs_acesso` (migration `000007`) recebe uma linha por tentativa de login concluída, gravada por `services.RegistrarTentativaLogin` num `INSERT` único não-fatal disparado de dentro de `LoginHandler` (método `senha`) e `KeycloakSSOHandler` (método `sso`). Nova rota `GET /api/logs-acesso` atrás de `RequireAuth → RequireRole(adm)` lista os registros filtráveis por período (`inicio`/`fim` em RFC3339 ou `AAAA-MM-DD`, `fim` só-data = fim de dia inclusivo), ordem `criado_em DESC`, `LIMIT 500`. No frontend, nova seção somente-leitura "Log de Acesso" em `ConfiguracoesPage`, montada só para `adm`. A resposta HTTP dos handlers de login permanece byte-idêntica; o registro nunca vira `500`.

Esta passagem foi um **follow-up review** sobre um spec já em `done` (`followup_review_recommended: true` da passagem anterior). O código da story já estava implementado e commitado (`53f6764`); esta passagem revisou o diff desde `baseline_revision` e aplicou 2 patches de severidade baixa.

### Arquivos alterados nesta passagem (follow-up)
- `backend/services/logs_acesso.go` — `ORDER BY l.criado_em DESC, l.id DESC` (desempate determinístico na fronteira do `LIMIT`), com comentário.
- `frontend/src/components/logs/LogAcessoSection.tsx` — texto do aviso de teto de 500 reformulado para não afirmar linhas ocultas quando o período tem exatamente 500; comentário de `MAX_LOGS` ajustado.
- `frontend/src/components/logs/LogAcessoSection.test.tsx` — regex do aviso de teto atualizada para o novo texto.

(Arquivos da implementação original — migration `000007`, `services/logs_acesso.go`, `handlers/logs_acesso.go`, wiring em `auth.go`/`auth_sso.go`/`main.go`, `LogAcessoSection.tsx`, `ConfiguracoesPage.tsx` e as respectivas suítes de teste — já constam do commit `53f6764`.)

### Breakdown das findings desta passagem
- Patches aplicados: 2 (baixa 2) — desempate no `ORDER BY`; texto do aviso de teto de 500.
- Itens deferidos: 0 nesta passagem (a revisão re-observou os 3 já em `deferred`; nenhum novo).
- Itens rejeitados: 18 (baixa 18) — segundo fator / retenção / filtros extra já deferidos; `usuario_id NULL` em conta bloqueada e `sucesso` desacoplado da sessão são mandatados pelo `<intent-contract>`; allowlist de proxy e nível de log já rejeitados antes; demais cosméticos ou fora do escopo do FR-38.

### Recomendação de follow-up review
`followup_review_recommended: false`. Patches desta passagem: 0 high, 0 medium, 2 low. Score = 3×0 + 1×2 = 2 (< 5) e nenhum high.

### Verificação executada
- `cd backend && gofmt -l .` — sem saída. `go build ./...` e `go vet ./...` — limpos.
- `cd backend && go test -p 1 -count=1 ./...` — Postgres 16 real (instância efêmera via `initdb`, `DATABASE_URL` apontando para ela, pois `docker` não está disponível neste ambiente — mesma nota das stories anteriores). Todos os pacotes passam: `stockflow/backend`, `.../cmd/seed-admin`, `.../handlers`, `.../iam`, `.../middleware`, `.../services`. Cobre `services/logs_acesso_test.go`, `handlers/logs_acesso_test.go` e os casos novos de `auth_test.go`/`auth_sso_test.go`/`auth_mfa_test.go`; migration `000007` aplica sem erro.
- `cd frontend && npm run lint` (`oxlint`) — limpo. `npm run build` (`tsc -b && vite build`) — ok. `npm run test` (`vitest run`) — 21 arquivos, 195 testes, todos passam.

### Riscos residuais
- Cobertura de integração contra Postgres real rodou em instância efêmera local, não via `docker compose`; a checagem manual de `docker compose up --build` no `## Verification` continua pendente para um ambiente com Docker.
- Itens em `deferred` permanecem em aberto (segundo fator não auditado; sem rotina de retenção dos 12 meses do PRD §9; sem filtro por resultado/método nem índices de apoio em `logs_acesso`) — todos decisões deliberadas do `<intent-contract>`, para stories futuras.

