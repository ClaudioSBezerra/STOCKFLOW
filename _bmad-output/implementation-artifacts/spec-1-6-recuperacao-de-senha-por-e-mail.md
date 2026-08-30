---
title: 'Story 1.6 — Recuperação de senha por e-mail'
type: 'feature'
created: '2026-08-29'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '606c1ea960c772ee4feee93295ddbf7d160390b7'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      Um POST /api/auth/login com a senha antiga concorrente à transação de RedefinirSenha pode criar uma sessão que sobrevive ao "revoga todas as sessões".
    evidence: |-
      RedefinirSenha roda sob READ COMMITTED e faz UPDATE sessoes SET revogado_em = now() WHERE revogado_em IS NULL; uma sessão inserida por um Login concorrente que valida a senha antiga antes do commit do novo senha_hash não é vista por esse UPDATE. Janela de milissegundos; após o commit a senha antiga deixa de funcionar e a sessão sobrevivente expira em <=2h. Correção proporcional exige SELECT ... FOR UPDATE na linha de usuarios tanto em RedefinirSenha quanto no caminho de Login — mudança de dois lados sobre um padrão estabelecido do repo (nenhum acesso usa lock de linha hoje).
    location: >-
      backend/services/auth.go RedefinirSenha / Login
    severity: low
  - summary: >-
      O token de redefinição permanece na URL e no histórico do navegador após o mount de RedefinirSenhaPage e na tela de sucesso; sem history.replaceState e sem meta Referrer-Policy.
    evidence: |-
      RedefinirSenhaPage lê ?token= e nunca o remove da barra de endereço. Espelha o padrão já existente de VerificarEmailPage (Story 1.3), mas o token de redefinição é mais sensível (permite definir senha). Mitigações naturais: single-use, TTL 30min, consumido no primeiro uso bem-sucedido, e o uso revoga sessões. A página não carrega subrecursos de terceiros hoje, então o vetor de Referer é teórico.
    location: >-
      frontend/src/pages/RedefinirSenhaPage.tsx
    severity: low
  - summary: >-
      RedefinirSenhaPage não tem retry no lugar após falha transitória do GET de validação no mount — um token válido que pega um 5xx/erro de rede força o usuário a pedir um link novo.
    evidence: |-
      O useEffect grava tokenValidado.current = token antes do fetch; em erro a fase vira 'erro' e o guard de early-return impede nova validação naquela aba. Estrutura copiada de VerificarEmailPage. Existe caminho de recuperação (botão "Solicitar novo link"), porém mais pesado (novo round-trip de e-mail). Falhas transitórias são pouco frequentes.
    location: >-
      frontend/src/pages/RedefinirSenhaPage.tsx:62-87
    severity: low
---

<intent-contract>

## Intent

**Problem:** Não existe caminho de recuperação de acesso: quem esquece a senha depende de suporte. O mecanismo `TOKENS_ACAO` (`tipo=redefinicao_senha`) e o enum `emails_pendentes.tipo=redefinicao_senha` já foram provisionados na migration 000002 (Story 1.3) e nunca tiveram consumidor — nenhum endpoint, template de e-mail ou tela.

**Approach:** Dois endpoints públicos — `POST /api/auth/esqueci-senha` (sempre responde a mesma mensagem genérica; se a conta existir, grava token de 30min + linha na outbox na MESMA transação) e `POST /api/auth/redefinir-senha` (valida token + força da nova senha, troca `senha_hash`, marca o token usado e revoga todas as sessões da conta, tudo numa transação) — mais um `GET /api/auth/redefinir-senha` que só checa a validade do token sem consumi-lo, para a tela explicar um link morto ao ser aberta. Frontend: telas públicas `/esqueci-senha` e `/redefinir-senha`, link "Esqueci minha senha" no Login, e o template `redefinicao_senha` no worker de e-mail.

## Boundaries & Constraints

**Always:**
- `POST /api/auth/esqueci-senha` responde SEMPRE `200` com corpo byte-idêntico `{"mensagem":"Se o e-mail existir, você receberá um link."}`, exista ou não a conta — nunca revela existência por status, corpo ou latência perceptível (sem bcrypt neste caminho; sem ramo condicional após a resposta). Só JSON malformado → `400 VALIDATION_ERROR`. Erro de infraestrutura → `500 INTERNAL_ERROR`.
- Quando (e só quando) existe linha em `usuarios` com `lower(email)` igual ao informado (match case-insensitive, normalização de `normalizeEmail`), grava numa única transação: um `tokens_acao` (`tipo='redefinicao_senha'`, `expira_em = now()+30min`, `usado_em` nulo, valor via `gerarTokenAcao()`) + um `emails_pendentes` via `EnfileirarEmail(tx, email, usuarioID, "redefinicao_senha", {nome, link})`, com `link = "{APP_URL}/redefinir-senha?token={token}"`. Conta só-SSO (`senha_hash` nulo) NÃO é exceção — recebe token e e-mail normalmente.
- `POST /api/auth/redefinir-senha` (`{token, senha}`): resolve o token filtrando por `token + tipo='redefinicao_senha'` (o valor já é globalmente único — mesmo padrão de `VerificarEmail`, sem filtro extra por `usuario_id`). Token inexistente → `404 NOT_FOUND`; existente porém expirado ou já usado → `400 TOKEN_EXPIRED`. Nova senha reprovada na política → `400 VALIDATION_ERROR`, SEM consumir o token (o mesmo link continua válido para nova tentativa). Sucesso, numa transação: `UPDATE usuarios SET senha_hash` (bcrypt, `bcrypt.DefaultCost`); `UPDATE tokens_acao SET usado_em = now()` guardado por `usado_em IS NULL AND expira_em > now()` (`RowsAffected()==0` → `TOKEN_EXPIRED`, fecha corrida SELECT→UPDATE como em `VerificarEmail`); `UPDATE sessoes SET revogado_em = now() WHERE usuario_id = $1 AND revogado_em IS NULL`. Resposta `200 {"mensagem": ...}`.
- Política de força de senha em `services.ValidarForcaSenha(senha) error` (nova, exportada — semente que a Story 1.10 vai reusar/estender): mínimo 8 caracteres (`utf8.RuneCountInString`), ao menos uma letra (`unicode.IsLetter`) e ao menos um dígito (`unicode.IsDigit`), no máximo 72 bytes (limite do bcrypt) — falha → `ErrSenhaFraca`. Espelho mínimo no frontend em `lib/senha.ts` (`senhaAtendePolitica`), duplicação entre linguagens documentada como o espelho `rankPapel` da Story 1.5; a autoridade é sempre o backend.
- `GET /api/auth/redefinir-senha?token=` chama `services.ValidarTokenRedefinicao(db, token)` (SELECT puro, nenhuma escrita): válido → `200 {"valido":true}`; inexistente → `404 NOT_FOUND`; expirado/usado → `400 TOKEN_EXPIRED`.
- Conta só-SSO que redefine a senha por este fluxo passa a ter os dois caminhos de login: nenhum campo além de `senha_hash` muda, e um `POST /api/auth/login` subsequente com a nova senha autentica (contanto que `ativo` e `email_verificado` já fossem verdadeiros).
- Novas rotas registradas em `newMux` (`main.go`) e cobertas em `TestNewMux_RegistraRotasDeAutenticacao`. Todos os `code` de erro saem do vocabulário fixo AD-14 (`VALIDATION_ERROR`, `NOT_FOUND`, `TOKEN_EXPIRED`, `INTERNAL_ERROR`) — nenhum código novo. Handlers usam `escreverErro`/`escreverJSON` e `http.MaxBytesReader` (mesmo `authRequestMaxBytes`) como os handlers de auth existentes.
- Frontend: `/esqueci-senha` e `/redefinir-senha` são rotas públicas irmãs de `/login`, fora de `RotaProtegida`. `EsqueciSenhaPage` espelha `CadastroPage` (um campo, submit, estado de sucesso genérico). `RedefinirSenhaPage` espelha `VerificarEmailPage` no bootstrap: lê `?token=`, valida no mount via `GET`; sem token ou `GET` reprovado → estado explicativo ("link expirado ou já usado" / "link inválido") com botão para `/esqueci-senha`; `GET` ok → formulário de nova senha (um campo `senha`, `autocomplete="new-password"`). Submit reprovado no cliente pela política → erro inline sem chamar a API. Submit com `TOKEN_EXPIRED`/`NOT_FOUND` (corrida: expirou entre o mount e o submit) → mesmo estado explicativo. Sucesso → estado final com botão para `/login`. Link "Esqueci minha senha" adicionado ao `LoginPage`, apontando para `/esqueci-senha`.

**Block If:** nenhuma decisão desta story depende de aprovação humana. Provisionar credenciais SMTP reais é operação de ambiente já registrada (`.env.example`) e não bloqueia código nem testes — o worker degrada graciosamente com `SMTP_PASSWORD` vazio.

**Never:**
- Nenhum limite de taxa / anti-força-bruta / captcha no endpoint de solicitação — Story 1.10 (bloqueio de conta e política de senha). Risco residual documentado.
- Nenhuma mudança em `email_verificado` ou `ativo` durante a redefinição — o intent enumera exatamente três efeitos (senha, token usado, sessões revogadas); nada mais.
- Nenhum histórico/reuso de senha, nenhuma expiração periódica de senha, nenhuma verificação de senha vazada.
- Nenhuma tela de "sair de todos os dispositivos", lista de sessões, nem campo de confirmação de senha (o `CadastroPage` também tem só um campo `senha`).
- Nenhuma mudança no fluxo de cadastro/`verificacao_email`, em `RequireAuth`, no formato do access token, nem em `RenovarSessao`/`EmitirSessao` — a revogação é um `UPDATE` direto em `sessoes` (o access JWT stateless de ≤30min expira sozinho; o efeito observável é `POST /api/auth/refresh` com um cookie pré-redefinição passar a devolver `401`).
- Nenhum filtro por `usuario_id` na validação do token além do que `VerificarEmail` já faz (o token é único); nenhum endpoint que exponha se um e-mail existe.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Solicitação, conta existe | `POST /esqueci-senha` `{email}` casa uma linha | `200` msg genérica; 1 `tokens_acao` (`redefinicao_senha`, +30min) + 1 `emails_pendentes` (`redefinicao_senha`) na mesma tx | — |
| Solicitação, conta não existe | e-mail sem match | `200` com corpo byte-idêntico ao caso acima; nenhuma linha gravada | — |
| Solicitação, conta só-SSO | `senha_hash IS NULL` | igual ao caso "conta existe" — token + e-mail gerados | — |
| Solicitação, e-mail com maiúsculas / espaços | `"  User@Empresa.com "` | casa via `lower(email)` normalizado; token gerado | — |
| Solicitação, JSON malformado | corpo não-JSON | `400 VALIDATION_ERROR` | — |
| Validação de link no mount | `GET /redefinir-senha?token=<válido>` | `200 {"valido":true}`; token NÃO consumido (`usado_em` continua nulo) | — |
| Validação de link inexistente | `GET` com token aleatório | `404 NOT_FOUND` | — |
| Validação de link expirado/usado | `GET` com token vencido ou `usado_em` preenchido | `400 TOKEN_EXPIRED` | — |
| Redefinição bem-sucedida | `POST /redefinir-senha` `{token válido, senha forte}` | `200`; `senha_hash` novo (bcrypt casa a nova, não a antiga); `usado_em` preenchido; todas as `sessoes` da conta com `revogado_em`; `sessoes` de outras contas intactas | — |
| Redefinição, senha fraca | senha `<8`, ou sem letra, ou sem dígito, ou `>72` bytes | `400 VALIDATION_ERROR`; token NÃO consumido (`usado_em` nulo) | `ErrSenhaFraca` |
| Redefinição, token inexistente | token aleatório | `404 NOT_FOUND` | `ErrTokenNaoEncontrado` |
| Redefinição, token expirado/usado | token vencido ou já `usado_em` | `400 TOKEN_EXPIRED` | `ErrTokenExpirado` |
| Redefinição, reuso do mesmo token | segundo `POST` com o token já consumido | `400 TOKEN_EXPIRED` (o `UPDATE` guardado afeta 0 linhas) | — |
| Token de `verificacao_email` no fluxo de redefinição | token válido mas `tipo='verificacao_email'` | `404 NOT_FOUND` — fluxos isolados (AD-18) | `ErrTokenNaoEncontrado` |
| Redefinição de conta só-SSO | `senha_hash` era nulo, token válido, senha forte | `200`; `POST /api/auth/login` com a nova senha passa a autenticar | — |
| Sessão pré-redefinição após sucesso | cookie de refresh emitido antes do reset | `POST /api/auth/refresh` → `401 TOKEN_EXPIRED` | — |

</intent-contract>

## Code Map

- `backend/services/auth.go` -- adicionar `ValidarForcaSenha(senha string) error`, `SolicitarRedefinicaoSenha(db, emailCfg EmailConfig, email string) error` (nil tanto no envio quanto no "e-mail sem match"; padrão tx de `Cadastrar`, linhas 141-178), `ValidarTokenRedefinicao(db, token string) error` (SELECT puro, molde de `VerificarEmail` linhas 200-217), `RedefinirSenha(db, token, senha string) error` (tx: update `senha_hash` + `marcarUsado` guardado como linhas 219-229 + `UPDATE sessoes ... revogado_em`). Novo `var ErrSenhaFraca`. Reusa `ErrTokenNaoEncontrado`/`ErrTokenExpirado`, `gerarTokenAcao`, `normalizeEmail`, `bcrypt`, `EnfileirarEmail`. Novos imports: `unicode`.
- `backend/handlers/auth.go` -- `EsqueciSenhaHandler(db, emailCfg)`, `ValidarRedefinicaoSenhaHandler(db)` (GET, lê `?token=`), `RedefinirSenhaHandler(db)`. Reusam `erroEnvelope`/`escreverErro`/`escreverJSON`, `authRequestMaxBytes`, `http.MaxBytesReader`, mapeamento `errors.Is` → envelope como `VerificarEmailHandler`/`LoginHandler` (linhas 92-111, 191-242).
- `backend/services/email.go` -- em `renderizarTemplate` (linha 92) adicionar `case "redefinicao_senha":` (assunto "Redefinição de senha — stockflow", corpo com `nome`+`link`, "Este link expira em 30 minutos.", `html.EscapeString` no nome — molde do `case "verificacao_conta"`). Atualizar o comentário de doc que diz "só ganha template na Story 1.6".
- `backend/main.go` -- `newMux` (linha 185): registrar `POST /api/auth/esqueci-senha`, `GET /api/auth/redefinir-senha`, `POST /api/auth/redefinir-senha`. Atualizar o doc de pacote (linhas 1-9).
- `backend/migrations/` -- SEM mudança: `tokens_acao.tipo` e `emails_pendentes.tipo` já aceitam `redefinicao_senha` (000002).
- `backend/services/auth_test.go` -- testes de `ValidarForcaSenha` (tabela pura), `SolicitarRedefinicaoSenha`, `ValidarTokenRedefinicao`, `RedefinirSenha` via `testDB(t)` (Postgres real). Helpers existentes: `criarUsuarioParaLogin` (linha 500), `criarUsuarioComToken` como referência.
- `backend/handlers/auth_test.go` -- casos da I/O Matrix na fronteira HTTP via `newMux`/httptest; incluir o encadeamento reset → `POST /api/auth/login` (nova senha) e reset → `POST /api/auth/refresh` (cookie antigo → 401). Helpers: `criarUsuarioLogin` (linha 337), `criarUsuarioLoginComEstado` (linha 359).
- `backend/main_test.go` -- estender `TestNewMux_RegistraRotasDeAutenticacao` (linha 188) com as 3 rotas novas (ex.: `GET /api/auth/redefinir-senha` sem token → `404`; `POST` com JSON inválido → `400`; `POST /api/auth/esqueci-senha` com JSON inválido → `400`).
- `frontend/src/pages/EsqueciSenhaPage.tsx` (novo) -- molde de `CadastroPage.tsx`.
- `frontend/src/pages/RedefinirSenhaPage.tsx` (novo) -- bootstrap no mount como `VerificarEmailPage.tsx` (linhas 34-75) + formulário de nova senha.
- `frontend/src/lib/senha.ts` (novo) -- `senhaAtendePolitica(senha: string): boolean` (espelho de `ValidarForcaSenha`).
- `frontend/src/pages/LoginPage.tsx` -- adicionar `<Link to="/esqueci-senha">Esqueci minha senha</Link>`.
- `frontend/src/App.tsx` -- registrar as 2 rotas públicas; atualizar o comentário que enumera as rotas públicas (linhas 9-22).
- `frontend/src/lib/senha.test.ts`, `frontend/src/pages/EsqueciSenhaPage.test.tsx`, `frontend/src/pages/RedefinirSenhaPage.test.tsx` (novos); `frontend/src/pages/LoginPage.test.tsx`, `frontend/src/App.test.tsx` (estendem).

## Tasks & Acceptance

**Execution:**
- `backend/services/auth.go` + `auth_test.go` -- `ValidarForcaSenha` (tabela: 7 chars, sem dígito, sem letra, 8 ok, 73 bytes, acento contando como 1 rune) e as 3 funções de fluxo com `testDB`.
- `backend/handlers/auth.go` + `auth_test.go` -- 3 handlers; toda a I/O Matrix na fronteira HTTP, incl. corpo genérico byte-idêntico e os encadeamentos login/refresh pós-reset.
- `backend/services/email.go` + `email_test.go` -- `case "redefinicao_senha"` em `renderizarTemplate`; teste de assunto/corpo/link e do `html.EscapeString` no nome.
- `backend/main.go` + `main_test.go` -- registro das 3 rotas + extensão do teste de inventário de rotas.
- `frontend/src/lib/senha.ts` + `senha.test.ts` -- `senhaAtendePolitica`, mesma tabela do backend.
- `frontend/src/pages/EsqueciSenhaPage.tsx` + `.test.tsx` -- submit chama `POST /api/auth/esqueci-senha`; estado de sucesso genérico aparece em qualquer `2xx`; erro de rede → mensagem de retry.
- `frontend/src/pages/RedefinirSenhaPage.tsx` + `.test.tsx` -- sem token → estado inválido; mount `GET 200` → formulário; mount `GET 400` → estado "expirado" com link para `/esqueci-senha`; submit senha fraca → erro inline sem POST; submit `TOKEN_EXPIRED` → estado explicativo; submit ok → estado final com link para `/login`.
- `frontend/src/pages/LoginPage.tsx` + `.test.tsx` -- link "Esqueci minha senha" (`href="/esqueci-senha"`) presente.
- `frontend/src/App.tsx` + `App.test.tsx` -- `/esqueci-senha` e `/redefinir-senha` renderizam suas páginas sem passar por `RotaProtegida`.

**Acceptance Criteria:**
- Given a tela "Esqueci minha senha" com um e-mail informado, when o formulário é enviado, then a resposta é sempre a mensagem genérica "Se o e-mail existir, você receberá um link." (mesmo status e corpo para conta existente e inexistente) e, só quando a conta existe, um `tokens_acao` (`tipo=redefinicao_senha`, expira em 30min, uso único) e um `emails_pendentes` são inseridos na mesma transação.
- Given um link de redefinição válido e não expirado, when o usuário define uma nova senha com no mínimo 8 caracteres contendo letra e número, then `senha_hash` é atualizado, o token fica marcado como usado e todas as sessões ativas da conta são revogadas (um refresh com cookie anterior passa a devolver 401).
- Given uma conta que só tinha login via SSO (`senha_hash` nulo), when ela conclui este fluxo pela primeira vez, then uma senha própria é criada e o login por e-mail/senha passa a funcionar para essa conta, sem que os demais caminhos deixem de funcionar.
- Given um link expirado ou já usado, when o usuário abre a página de redefinição com esse token (ou envia o formulário e o token expira nesse meio-tempo), then a tela explica que o link expirou ou já foi usado e oferece um caminho para gerar um novo, sem consumir nem alterar nada.
- Given uma nova senha que não cumpre a política (curta demais, sem letra ou sem número), when o formulário de redefinição é enviado, then a resposta é 400 VALIDATION_ERROR, o token permanece válido para nova tentativa e nenhuma senha é alterada.

## Design Notes

- **`GET` de validação sem consumo:** a AC4 fala em "quando o usuário tenta acessá-lo" — a leitura fiel é a tela explicar o link morto já na abertura, não só depois de preencher a senha. Como a redefinição precisa de um formulário (nova senha), não dá para consumir no mount como `VerificarEmailPage` faz. Daí um `GET` leve que só reusa o SELECT que o `POST` já faria. O `POST` continua sendo a autoridade — revalida e trata a corrida "expirou entre abrir e enviar".
- **Não revelar existência:** o único canal relevante aqui é a resposta (status + corpo idênticos). Sem bcrypt neste caminho e sem trabalho condicional após escrever a resposta, não há oráculo de temporização à altura do de `Login` — equalização explícita fica fora de escopo (consistente com o repo, onde só os caminhos com bcrypt ganharam hash dummy).
- **Revogar sessões = `UPDATE sessoes`:** `RequireAuth` não consulta `sessoes` (access JWT stateless). Revogar as linhas de `sessoes` derruba só a capacidade de refresh; os access tokens em voo (≤30min) expiram sozinhos. Efeito observável e testável: `RenovarSessao` passa a devolver `ErrSessaoInvalida` para qualquer refresh emitido antes do reset.
- **Assimetria dos enums:** `emails_pendentes.tipo` usa `verificacao_conta`, mas `tokens_acao.tipo` usa `verificacao_email`; para redefinição AMBOS são `redefinicao_senha`.

Golden — registro em `newMux`:

```go
mux.HandleFunc("POST /api/auth/esqueci-senha", handlers.EsqueciSenhaHandler(db, emailCfg))
mux.HandleFunc("GET /api/auth/redefinir-senha", handlers.ValidarRedefinicaoSenhaHandler(db))
mux.HandleFunc("POST /api/auth/redefinir-senha", handlers.RedefinirSenhaHandler(db))
```

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: build limpo, sem warnings.
- `docker compose up -d db && cd backend && go test -p 1 ./...` -- expected: todos passam, incl. `ValidarForcaSenha`, `SolicitarRedefinicaoSenha` (conta existe / não existe / só-SSO / case-insensitive), `ValidarTokenRedefinicao`, `RedefinirSenha` (sucesso / senha fraca não consome / reuso / sessões revogadas / login pós-reset) e as rotas em `newMux`. Se o binário `docker` não existir no ambiente, subir um Postgres 16 descartável via `initdb`/`pg_ctl` e apontar `DATABASE_URL` para ele (mesmo procedimento registrado na Story 1.5).
- `cd frontend && npm run build && npm run lint && npm run test` -- expected: build/lint limpos; testes de `senha`, `EsqueciSenhaPage`, `RedefinirSenhaPage`, `LoginPage` (link) e `App` (rotas públicas) passam.
- `docker compose up --build` -- expected: `api`/`web` sobem saudáveis; o fluxo `/esqueci-senha` → linha em `emails_pendentes` → `/redefinir-senha?token=...` responde através do proxy `/api`.

**Manual checks (if no CLI):**
- Criar conta via `/cadastro` + verificação. Em `/esqueci-senha`, informar o e-mail: a resposta é a mensagem genérica; `SELECT * FROM emails_pendentes WHERE tipo='redefinicao_senha'` mostra a linha e o `variaveis_json.link`. Abrir o link, definir senha `abc12345`: a tela confirma o sucesso; `curl` no `POST /api/auth/refresh` com um cookie de refresh antigo → `401`; login com a nova senha → `200`. Repetir a solicitação com um e-mail inexistente: resposta idêntica, nenhuma linha nova em `emails_pendentes`.

## Spec Change Log

Nenhuma alteração de spec: nenhuma passagem de review disparou `bad_spec` nem `intent_gap`.

## Review Triage Log

### 2026-08-29 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 5: (high 0, medium 0, low 5)
- defer: 3
- reject: 21: (high 0, medium 0, low 21)
- addressed_findings:
  - `[low]` `[patch]` `SolicitarRedefinicaoSenha` acumulava links de redefinição válidos: cada solicitação inseria um novo `tokens_acao` sem invalidar os anteriores ainda não usados, deixando vários links de 30min válidos ao mesmo tempo. Corrigido com `UPDATE tokens_acao SET usado_em = now() WHERE usuario_id = $1 AND tipo = 'redefinicao_senha' AND usado_em IS NULL` na MESMA transação, antes do INSERT do novo token — só o link mais recente vale. Novo teste `TestSolicitarRedefinicaoSenha_InvalidaTokensAnteriores`.
  - `[low]` `[patch]` `EsqueciSenhaPage` exibia uma paráfrase ("...um link para redefinir a senha.") divergente do texto exato da AC1 e do corpo do backend (`mensagemEsqueciSenha`). Alinhado para exibir exatamente "Se o e-mail existir, você receberá um link."; `EsqueciSenhaPage.test.tsx` atualizado para asseverar a string exata.
  - `[low]` `[patch]` A linha da I/O Matrix "Validação de link expirado/usado" só era exercitada forçando `expira_em` no passado — o sub-ramo `usadoEm.Valid` de `ValidarTokenRedefinicao` nunca ficava fixado isoladamente. Adicionado caso "token com `usado_em` preenchido e `expira_em` no futuro → `ErrTokenExpirado` / `400 TOKEN_EXPIRED`" em `TestValidarTokenRedefinicao` e `TestValidarRedefinicaoSenhaHandler`.
  - `[low]` `[patch]` `TestRedefinirSenha_TokenExpirado` tinha um `_ = usuarioID` morto e só verificava o erro retornado, não a ausência de mutação. Agora afirma que após o erro `senha_hash` continua casando a senha antiga (e não a nova) e que `usado_em` do token continua nulo.
  - `[low]` `[patch]` Em `RedefinirSenhaPage`, um duplo-submit muito rápido (antes do re-render que reflete `enviando`) podia deixar dois `POST` passarem: o primeiro conclui (`setFase('concluido')`) e o segundo, já em voo, resolvia com `TOKEN_EXPIRED` (token já consumido) e trocava a tela de sucesso por "link expirado". Adicionado `concluidoRef` marcado antes de `setFase('concluido')`; os ramos de resposta não-ok e o `catch` retornam cedo se `concluidoRef.current`. Novo teste com dois submits no mesmo `act()`.
- Achados roteados para `defer` (3): (1) `Login` concorrente com a senha antiga durante a transação de `RedefinirSenha` pode criar sessão que sobrevive ao "revoga todas as sessões" (READ COMMITTED, janela de ms; correção proporcional precisa de `SELECT ... FOR UPDATE` em dois caminhos, padrão inexistente no repo); (2) token de redefinição permanece na URL/histórico (espelha `VerificarEmailPage`; sem `history.replaceState` nem `Referrer-Policy`); (3) `RedefinirSenhaPage` sem retry no lugar após falha transitória do `GET` de validação no mount (espelha o guard de efeito de `VerificarEmailPage`). Todos `low`, todos registrados em `deferred`.
- Achados roteados para `reject` (21): equalização de temporização no `esqueci-senha` — decisão consciente já documentada nas Design Notes; o corpo/status já são idênticos, o resíduo é um sinal de escrita-vs-no-op muito abaixo da escala do bcrypt e o vetor real de abuso (rate limit) é explicitamente da Story 1.10. Erro de infra (500) quebrar a garantia byte-idêntica — oráculo de falha parcial desprezível. `res.RowsAffected()` com erro descartado (`n, _ :=`) — convenção idêntica à de `VerificarEmail` no próprio repo (`lib/pq` nunca retorna erro aí). `RedefinirSenha` faz hash antes de reivindicar o token — correto sob a transação (rollback desfaz), corrida rara. Link de redefinição montado sem escape de URL — o token é URL-safe por construção (`base64.RawURLEncoding`, documentado em `gerarTokenAcao`), `AppURL` é config confiável, e `Cadastrar` monta o link de verificação da mesma forma. Template de e-mail sem parte `text/plain`, `link` sem `html.EscapeString`, "Olá, ." com nome vazio — espelha o `case "verificacao_conta"` por decisão de spec, as entradas não são controladas pelo atacante e nome vazio é inalcançável (validado como não-vazio no cadastro). Campo `message` do envelope de erro declarado e nunca exibido — convenção de todo o frontend (mensagem do backend não é confiável para exibição direta). `EsqueciSenhaPage` com `noValidate`/submit vazio chega à rede — igual a `CadastroPage`/`LoginPage`, e a resposta genérica no vazio é inofensiva. Ordem "força da senha antes de resolver o token" — as duas ordens são corretas, a matriz não fixa precedência e "sem consumir o token" é respeitado. Ausência de teste de rollback do par token+outbox — a transação única é estruturalmente evidente no diff e replica o padrão de `Cadastrar`. Ramo `VALIDATION_ERROR` de `RedefinirSenhaPage.handleSubmit` sem teste — defensivo praticamente inalcançável (espelho cliente idêntico à regra do backend), mesmo precedente do guard de contexto de `MeHandler`. Token ausente/vazio no `GET` → 404 e não 400 — espelho deliberado de `VerificarEmailHandler`, sancionado pela spec e pelo teste de inventário de rotas. Duplicação do SELECT entre `ValidarTokenRedefinicao` e `RedefinirSenha` e três seeders de teste parecidos — duplicação pequena e legível, `VerificarEmail` também não compartilha o seu SELECT. Ausência de log/auditoria do reset bem-sucedido — auditoria é da Story 1.12. Ausência de teste do "access token sobrevive <=30min após o reset" — é comportamento documentado do AD-6 (Story 1.4), não introduzido aqui. Índice funcional para `lower(email)` supostamente ausente — existe (`idx_usuarios_email_lower`, migration 000001). Ausência de gating por `ativo`/`email_verificado` e de testes desses estados — a seção `Never` da spec exclui isso e o comportamento é seguro (`Login` continua barrando conta inativa/não verificada). `ValidarForcaSenha` colapsa ">72 bytes" em `ErrSenhaFraca` com mensagem de "mín. 8" — espelha `Cadastrar`, entrada exótica, e o espelho no cliente barra antes do POST. "Corpo POST gigante cai na mensagem de senha fraca" — falso: `http.MaxBytesReader` faz o `json.Decode` falhar → 400 "payload inválido". Comentário "sem ramo condicional após a resposta" em `EsqueciSenhaHandler` — o comentário é exato (o trabalho condicional é antes da resposta), cosmético. Nota de risco residual do `esqueci-senha` incompleta quanto a flood de outbox/inbox — endereçado no `## Auto Run Result` (risco residual), sem mudança de código.
  - Nota do auditor de alinhamento de intenção (descritiva, sem ação): a AC4 ("quando o usuário tenta acessá-lo") admite as leituras "abrir o link" e "usar o link"; o diff implementa a leitura mais forte (explica na abertura, via `GET` de validação) — documentada nas Design Notes, satisfaz as duas. A AC2 ("todas as sessões ativas revogadas") é ancorada no nível da tabela `sessoes`/`RenovarSessao`, não na autorização do access token — coerente com o AD-6 (JWT stateless de <=30min) e explícito na seção `Never`. A não-revelação de existência é defendida no conteúdo da resposta (status+corpo idênticos), não em temporização. `ValidarForcaSenha` exportado é superfície além do fluxo estrito, defensável por necessidade de implementação e pelo precedente `rankPapel`.

### 2026-08-29 — Review pass (acompanhamento)
- intent_gap: 0
- bad_spec: 0
- patch: 1: (high 0, medium 0, low 1)
- defer: 0
- reject: 26: (high 0, medium 0, low 26)
- addressed_findings:
  - `[low]` `[patch]` `TestProcessarProximoEmailPendente_TipoDesconhecido` (em `backend/services/email_worker_test.go`, arquivo não tocado por esta story) ficou obsoleto e enganoso: ele enfileirava uma linha `emails_pendentes` com `tipo='redefinicao_senha'` e provava "tipo sem template → falha não-fatal". Com o novo `case "redefinicao_senha"` em `renderizarTemplate`, a renderização passa a ter sucesso e o worker segue para o `EnviarSMTP` — o teste continuava verde mas virou duplicata exata de `TestProcessarProximoEmailPendente_SemSMTP` e não exercitava mais o ramo de erro de renderização. Como o CHECK de `emails_pendentes.tipo` só admite `verificacao_conta`/`redefinicao_senha` (ambos com template agora), nenhuma linha armazenável dispara mais esse ramo — ele é defensivo e segue coberto isoladamente por `TestRenderizarTemplate_TipoDesconhecidoRetornaErro`. Teste refocado e renomeado para `TestProcessarProximoEmailPendente_RedefinicaoSenhaTemTemplate`: enfileira a linha com `nome`+`link` e afirma que o worker passa da renderização (falha só no envio SMTP, `ultimo_erro` sem "tipo de e-mail desconhecido") — cobertura positiva de que o template da Story 1.6 é alcançável a partir do worker.
- Achados roteados para `reject` (26):
  - Já triados e rejeitados na passagem anterior, re-levantados sem fato novo: equalização de temporização / canal lateral de latência no `esqueci-senha` (decisão consciente das Design Notes; só bcrypt teria escala relevante); enumeração de conta pela via 500 (oráculo de falha parcial desprezível); `link` do template sem escape de URL/HTML (token URL-safe por `base64.RawURLEncoding`, `AppURL` é config confiável, espelha `Cadastrar`); mensagem de `ErrSenhaFraca` não menciona o teto de 72 bytes (espelha `Cadastrar`, entrada exótica, espelho no cliente barra antes); token vazio/ausente no `GET` → 404 e não 400 (espelho deliberado de `VerificarEmailHandler`, sancionado pela spec e pelo `main_test`); ausência de gating por `ativo`/`email_verificado` (seção `Never` exclui; `Login` continua barrando); ausência de log/auditoria do reset (Story 1.12); `RedefinirSenha` gera o hash antes de reivindicar o token (correto sob a transação, rollback desfaz); duplicação do bloco SELECT `expira_em/usado_em` e dos seeders de teste (duplicação pequena e legível, `VerificarEmail` também não compartilha o seu); `EsqueciSenhaPage` `noValidate`/submit vazio chega à rede (igual a `CadastroPage`/`LoginPage`); ausência de teste e2e pelo `newMux` (a spec escopa `main_test.go` ao inventário de rotas); token na URL/histórico e `GET` sem retry no lugar (já registrados no ledger de deferidos pela passagem anterior — não reabertos aqui); guard de duplo-submit "fraco + `concluidoRef`" em vez de ref síncrono no topo do handler (o código atual funciona e tem teste; refinamento cosmético); `res.RowsAffected()` com erro descartado (convenção idêntica à de `VerificarEmail`).
  - Novos nesta passagem, rejeitados: corpo grande demais mapeado para `400 "payload inválido"` indistinguível de JSON malformado (comportamento de `http.MaxBytesReader` já aceito na passagem anterior; AD-14 não tem código para 413); relógios mistos (app grava `expira_em`, enforcement app + `now()` do banco) — espelho exato de `VerificarEmail`, deriva de relógio desprezível num deploy único; `RedefinirSenha` com `UPDATE usuarios` afetando 0 linhas se o usuário sumisse entre o SELECT e o UPDATE dentro da transação — inalcançável (nenhum caminho apaga `usuarios`; FK garante a linha); `GET` de validação sem timeout no cliente deixando a página presa em "Validando o link..." — espelha `VerificarEmailPage`, mesmo tema do deferido nº 3; `SolicitarRedefinicaoSenha` com múltiplos matches de `lower(email)` — impossível (`idx_usuarios_email_lower` é `UNIQUE`); "só um link de redefinição válido por vez" não garantido sob concorrência de duas solicitações simultâneas (READ COMMITTED, sem lock de linha — padrão inexistente no repo; o pior caso degrada exatamente para a leitura literal do intent "1 token + 1 e-mail por solicitação", sem dano); "Este link expira em 30 minutos" como string fixa desacoplada de `tokenRedefinicaoExpiracao` (cosmético, espelha o template `verificacao_conta`); campos de dica/erro dos formulários sem `aria-describedby` (convenção de TODAS as páginas de auth irmãs — `CadastroPage`/`LoginPage`/`VerificarEmailPage` usam o mesmo `<p role="alert">` sem associação; a spec manda espelhar `CadastroPage`); mensagem de sucesso do `RedefinirSenhaHandler` "morta" e divergente do texto da página (o corpo de resposta do backend nunca é exibido — convenção de todo o frontend, mesmo caso do campo `message` do envelope de erro já rejeitado).
  - Nota do auditor de alinhamento de intenção (descritiva, sem ação): o `UPDATE tokens_acao SET usado_em = now() ... WHERE usuario_id = $1` adicionado a `SolicitarRedefinicaoSenha` na passagem anterior (invalida links de redefinição anteriores não usados) é uma escrita que o intent não enumera explicitamente e fica adjacente ao escopo de anti-força-bruta deferido à Story 1.10. Avaliado e mantido: não é rate-limit nem captcha; é higiene de token de uso único ("trancar o acesso ao pedir a redefinição"), decisão consciente da passagem anterior com teste dedicado (`TestSolicitarRedefinicaoSenha_InvalidaTokensAnteriores`); a bala "Always" enumera o conteúdo mínimo da transação, não uma lista fechada, e a proibição de filtro por `usuario_id` é especificamente sobre a *validação* do token. Reverter reabriria o achado que a passagem anterior corrigiu. Sem `bad_spec`/`intent_gap`.

## Auto Run Result

**Resumo:** Passagem de review de acompanhamento sobre a Story 1.6 (recomendada pela passagem anterior). Quatro camadas de review em paralelo (blind-hunter, edge-case-hunter, verification-gap, intent-alignment) sobre o diff completo desde `606c1ea`. Um único achado sobreviveu à triagem como acionável (`patch`, `low`); todo o resto era re-levantamento de achados já triados/rejeitados na passagem anterior, achados inalcançáveis, ou refinamentos cosméticos alinhados com convenções estabelecidas do repo. Nenhum `intent_gap`, nenhum `bad_spec`, nenhum novo `defer`. Comportamento funcional da story inalterado.

**Arquivos alterados nesta passagem:**
- `backend/services/email_worker_test.go` — `TestProcessarProximoEmailPendente_TipoDesconhecido` refocado e renomeado para `TestProcessarProximoEmailPendente_RedefinicaoSenhaTemTemplate` (ver `## Review Triage Log`); novo import `strings`.
- `_bmad-output/implementation-artifacts/spec-1-6-recuperacao-de-senha-por-e-mail.md` — este arquivo (triage log + Auto Run Result + frontmatter).

**Achados de revisão desta passagem:** patch 1 (aplicado). defer 0. reject 26.

**Recomendação de review de acompanhamento:** `false` — 1 achado `patch` nesta passagem (high 0, medium 0, low 1); score = 3×0 + 1×1 = 1 (< 5), sem `high`.

**Verificação executada:**
- `cd backend && go build ./... && go vet ./...` — limpo (exit 0).
- `go test -p 1 -count=1 ./...` — Docker indisponível (`docker: command not found`); subido cluster PostgreSQL 16 descartável via `initdb`/`pg_ctl` (binários de `/usr/lib/postgresql/16/bin`, TCP em `127.0.0.1:5433`, role/db `stockflow`, `pgcrypto`+`citext`), `DATABASE_URL` apontado para ele. **Todos os 5 pacotes passam** (`backend`, `backend/cmd/seed-admin`, `backend/handlers`, `backend/middleware`, `backend/services`). Teste refocado `TestProcessarProximoEmailPendente_RedefinicaoSenhaTemTemplate` roda e passa (`-run` explícito confirmado). Cluster parado e removido ao final.
- `cd frontend && npm run build && npm run lint && npm run test` — build limpo (tsc + vite), `oxlint` sem achados, **13 arquivos / 101 testes passando** (nenhum arquivo de frontend mudou nesta passagem).
- `docker compose up --build` — **não executado**: Docker indisponível (idêntico à passagem anterior). A superfície HTTP segue coberta por testes de integração contra Postgres real.

**Riscos residuais:**
- Inalterados em relação à passagem anterior (ver `deferred`/ledger do orquestrador): corrida `Login`-antigo × transação de reset; token de redefinição na URL/histórico; `RedefinirSenhaPage` sem retry no lugar após falha transitória do `GET`; ausência de rate limit no `esqueci-senha` (Story 1.10); janela do AD-6 (access token sobrevive ≤30min ao reset).
- `docker compose up --build` não pôde rodar por ausência do binário `docker` — risco residual baixo.
- A árvore de trabalho versionada retém modificações em `_bmad-output/implementation-artifacts/deferred-work.md` e `sprint-status.yaml` feitas pelo orquestrador antes desta invocação — intencionalmente não tocadas (são de propriedade do orquestrador).

