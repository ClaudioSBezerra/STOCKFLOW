---
title: 'Story 1.10 — Bloqueio de conta e política de senha'
type: 'feature'
created: '2026-08-30'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '445002949f1c51393f9a9bdd5b6b14b9f225bab4'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md']
warnings: ['multiple-goals', 'oversized']
deferred:
  - summary: >-
      ValidarForcaSenha rejeita senha acima de 72 bytes com a mensagem "ao menos
      8 caracteres, incluindo uma letra e um número" — enganosa para uma
      passphrase longa; a Story 1.10 propaga esse texto também ao autocadastro.
    evidence: |-
      ValidarForcaSenha (services/auth.go) devolve ErrSenhaFraca tanto para
      <8 runes quanto para >72 bytes, e handlers/auth.go (Cadastro e Redefinir)
      + frontend (MENSAGEM_SENHA_FRACA em CadastroPage/RedefinirSenhaPage)
      exibem só o critério de mínimo. Um usuário colando uma passphrase forte de
      >72 bytes é informado de que ela é "curta". Comportamento pré-existente da
      Story 1.6, agora também visível no cadastro. Uma mensagem própria de
      "máximo 72 caracteres" resolveria.
    location: >-
      backend/services/auth.go (ValidarForcaSenha) / frontend/src/lib/senha.ts
    severity: low
  - summary: >-
      POST /api/auth/esqueci-senha não tem limite de taxa — cada chamada
      enfileira um e-mail no outbox, servindo de vetor de e-mail-bomba contra
      um endereço conhecido.
    evidence: |-
      EsqueciSenhaHandler (handlers/auth.go) responde sempre 200 e chama
      SolicitarRedefinicaoSenha, que enfileira uma linha em emails_pendentes por
      requisição (invalida tokens anteriores, mas não limita e-mails enviados).
      Gap pré-existente da Story 1.6; a Story 1.10 o torna mais relevante ao
      empurrar usuários bloqueados para esse endpoint. Fora do escopo de FR-36
      (que só pede bloqueio no login por senha), mas merece uma passagem
      dedicada de segurança.
    location: >-
      backend/handlers/auth.go (EsqueciSenhaHandler) / services.SolicitarRedefinicaoSenha
    severity: low
---

<intent-contract>

## Intent

**Problem:** O login por senha (`services.Login`, Story 1.4) não tem nenhuma proteção contra força bruta — um atacante pode tentar senhas indefinidamente para a mesma conta (FR-36, SM-6). E a política mínima de força de senha (`services.ValidarForcaSenha`, Story 1.6) só é aplicada na redefinição (`RedefinirSenha`); o autocadastro (`Cadastrar`, Story 1.3) ainda aceita qualquer senha não vazia de até 72 bytes.

**Approach:** (1) Nova migration `000005` adiciona `usuarios.tentativas_login_falhas INT NOT NULL DEFAULT 0` e `usuarios.bloqueado_ate TIMESTAMPTZ`. `services.Login` passa a contar falhas consecutivas por conta: na 5ª falha grava `bloqueado_ate = now() + 15min`; enquanto `bloqueado_ate` está no futuro, recusa toda tentativa (mesmo com senha correta) sem revelar o tempo restante; um login bem-sucedido ou um bloqueio já expirado zera o contador. (2) `services.Cadastrar` passa a chamar `ValidarForcaSenha` (já existente) antes de qualquer escrita. Nenhuma rota nova; o caminho SSO (Story 1.9) não é tocado.

## Boundaries & Constraints

**Always:**
- **Contador de força bruta em coluna da própria conta**, não tabela nova: `tentativas_login_falhas` + `bloqueado_ate` em `usuarios` (mesma filosofia de "estado na linha da conta" de `usuarios.ativo`). `maxTentativasLogin = 5` e `duracaoBloqueioLogin = 15 * time.Minute` como constantes em `services/auth.go` (o `[ASSUMPTION]` do PRD §4.1 fixa 5 e 15min).
- **`services.Login`**: o `SELECT` inicial também lê `tentativas_login_falhas` e `bloqueado_ate`. A comparação bcrypt (real ou `dummyBcryptHash`) continua acontecendo SEMPRE para uma linha encontrada, ANTES de qualquer `return` — a defesa contra side-channel de tempo da Story 1.4 não pode regredir (o caminho "conta bloqueada" não pode responder mensuravelmente mais rápido).
  - `bloqueado_ate` não nulo e no futuro → retorna `ErrContaBloqueada`, sem incrementar o contador nem estender o prazo, mesmo que a senha esteja correta.
  - `bloqueado_ate` não nulo e no passado (bloqueio expirado) → zera `tentativas_login_falhas`/`bloqueado_ate` (`UPDATE ... WHERE id = $1 AND bloqueado_ate <= now()`) e segue o fluxo normal como conta destravada.
  - Falha de credencial (`!ativo || !emailVerificado || !senhaHash.Valid || !senhaCorreta`) COM `senhaHash.Valid && !senhaCorreta` (senha realmente errada numa conta com senha) → incrementa via `UPDATE` atômico no banco (nunca `contador+1` calculado em Go e regravado): `tentativas_login_falhas = tentativas_login_falhas + 1`, e `bloqueado_ate = now() + duracao` quando o novo valor alcança 5. Retorna `ErrCredenciaisInvalidas` (a 5ª falha ainda é `ErrCredenciaisInvalidas`; só a 6ª tentativa é `ErrContaBloqueada`).
  - Falha de credencial por conta desativada / e-mail não verificado / conta só-SSO com a senha CORRETA → `ErrCredenciaisInvalidas` sem incrementar (não é sinal de força bruta).
  - E-mail sem linha → inalterado (`ErrCredenciaisInvalidas` + bcrypt dummy); nada a contar, sem `UPDATE`.
  - Sucesso → se `tentativas_login_falhas != 0` ou `bloqueado_ate` não nulo, `UPDATE usuarios SET tentativas_login_falhas = 0, bloqueado_ate = NULL WHERE id = $1` antes de retornar o id.
- **`services.Cadastrar`**: após o guard de campos obrigatórios (`ErrCadastroValidacao` para nome/e-mail/senha vazios) e antes do `bcrypt.GenerateFromPassword`, chamar `ValidarForcaSenha(senha)` e propagar `ErrSenhaFraca`. O guard redundante `len(senha) > 72` é removido (já coberto por `ValidarForcaSenha`). Nenhuma linha em `usuarios`/`tokens_acao`/`emails_pendentes` é escrita quando a senha reprova.
- **Handlers (`handlers/auth.go`), vocabulário de erro AD-14**:
  - `LoginHandler`: novo `case errors.Is(err, services.ErrContaBloqueada)` → `429 Too Many Requests`, code `ACCOUNT_LOCKED`, mensagem sem tempo restante (ex.: "Muitas tentativas de login. Sua conta foi bloqueada temporariamente. Use \"Esqueci minha senha\" para voltar a acessar."). Demais casos inalterados.
  - `CadastroHandler`: novo `case errors.Is(err, services.ErrSenhaFraca)` → `400 VALIDATION_ERROR` com a MESMA string já usada em `RedefinirSenhaHandler` ("A senha deve ter ao menos 8 caracteres, incluindo uma letra e um número.").
- **Frontend**:
  - `LoginPage.tsx` — `mensagemDeErro` ganha `ACCOUNT_LOCKED` → texto de bloqueio sem tempo restante. O link "Esqueci minha senha" já é sempre renderizado (AC2, nada muda ali).
  - `CadastroPage.tsx` — pré-checagem client com `senhaAtendePolitica` (`@/lib/senha`) antes do `fetch`, no molde de `RedefinirSenhaPage.tsx`: senha reprovada → `setErro(MENSAGEM_SENHA_FRACA)` e `return` sem chamar a API. Constante de mensagem idêntica à de `RedefinirSenhaPage`.
- **SSO intocado (AC4)**: `KeycloakSSOHandler`/`BuscarUsuarioPorEmailSSO`/`EmitirSessao` não leem nem escrevem `tentativas_login_falhas`/`bloqueado_ate`. Uma conta bloqueada por senha continua entrando por SSO; falhas de senha não afetam o caminho SSO.

**Block If:** nenhuma decisão desta story depende de aprovação humana nem de ação de operador fora do repositório — é migration aditiva + código + testes, sem provisionamento externo. Status final esperado: `done`.

**Never:**
- Nenhuma tabela nova de tentativas/auditoria de login (a trilha de acesso é a Story 1.12/FR-38). Nenhum contador por IP, janela deslizante ou backoff progressivo — só bloqueio temporal por conta após N falhas consecutivas.
- Nenhum rate-limit em `POST /api/auth/esqueci-senha` ou nas rotas de redefinição — fora do escopo das 4 ACs (o comentário "escopo da Story 1.10" em `EsqueciSenhaHandler` era especulativo; FR-36 não pede isso).
- Nenhuma rota, item de navegação, migration destrutiva, mudança em `middleware/`, no formato de sessão, em `EmitirSessao`/`RenovarSessao`, ou no caminho SSO. Nenhuma mudança na regra de "não revelar se o e-mail existe" para senha errada / e-mail inexistente (só o estado "bloqueada" tem mensagem própria — ver Design Notes).
- Nenhum desbloqueio manual por adm nesta story; nenhuma notificação ao dono da conta; nenhuma configuração runtime dos limites (5/15min são constantes).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| 5ª falha consecutiva | `Login`, conta com senha, `tentativas=4`, senha errada | `ErrCredenciaisInvalidas`; `tentativas→5`; `bloqueado_ate = now()+15min` | 401 INVALID_CREDENTIALS |
| 6ª tentativa (bloqueada), senha correta | `Login`, `bloqueado_ate` no futuro, senha CORRETA | `ErrContaBloqueada`; bcrypt ainda executado; `tentativas`/`bloqueado_ate` inalterados | 429 ACCOUNT_LOCKED |
| Sucesso antes do limite | `Login`, `tentativas=3`, ativo+verificado, senha correta | id do usuário; `tentativas→0`, `bloqueado_ate→NULL` | — |
| Bloqueio expirado + senha correta | `Login`, `bloqueado_ate` no passado, senha correta | sucesso; `tentativas→0`, `bloqueado_ate→NULL` | — |
| Bloqueio expirado + senha errada | `Login`, `bloqueado_ate` no passado, senha errada | `ErrCredenciaisInvalidas`; `tentativas→1` (streak novo), `bloqueado_ate→NULL` | 401 INVALID_CREDENTIALS |
| Falhas não consecutivas | 3 falhas, 1 sucesso, 2 falhas | nunca bloqueia; `tentativas` = 0 após o sucesso, depois = 2 | — |
| E-mail inexistente martelado | `Login`, sem linha, N vezes | sempre `ErrCredenciaisInvalidas`; nenhum `UPDATE`; sem panic | 401 INVALID_CREDENTIALS |
| Conta desativada/não verificada, senha correta | `Login` | `ErrCredenciaisInvalidas`; contador NÃO incrementa | 401 INVALID_CREDENTIALS |
| SSO com conta bloqueada por senha | `POST /api/auth/sso/keycloak`, token válido, conta ativa, `bloqueado_ate` no futuro | `200` + par de tokens de sessão; `tentativas_login_falhas`/`bloqueado_ate` inalterados | — |
| Cadastro com senha fraca | `POST /api/auth/cadastro`, senha `"abc"` (<8) ou `"abcdefgh"` (sem dígito) | `ErrSenhaFraca`; ZERO linhas em `usuarios`/`tokens_acao`/`emails_pendentes` | 400 VALIDATION_ERROR |
| Cadastro com senha forte | senha `"abcd1234"` | `201`; conta criada como `usuario`, `email_verificado=false` | — |
| Conta bloqueada usa "Esqueci minha senha" | `POST /api/auth/esqueci-senha`, conta com `bloqueado_ate` no futuro | `200` mensagem genérica; `tokens_acao(redefinicao_senha)` + `emails_pendentes` gravados normalmente | — |
| Redefinição continua validando força | `POST /api/auth/redefinir-senha`, senha fraca | `ErrSenhaFraca` (comportamento da Story 1.6 preservado) | 400 VALIDATION_ERROR |
| UI login bloqueado | `fetch` login → `429 ACCOUNT_LOCKED` | `<p role="alert">` com texto sem tempo restante; link "Esqueci minha senha" visível | — |
| UI cadastro senha fraca | submit com senha `"abc"` | `senhaAtendePolitica` barra o submit; `role="alert"` com o critério; nenhum `POST /api/auth/cadastro` | — |

</intent-contract>

## Code Map

- `backend/migrations/000005_add_bloqueio_login_to_usuarios.up.sql` (novo) — `ALTER TABLE usuarios ADD COLUMN tentativas_login_falhas INTEGER NOT NULL DEFAULT 0; ADD COLUMN bloqueado_ate TIMESTAMPTZ;` + comentário citando Story 1.10 / FR-36 (molde de cabeçalho da 000004). Aditiva, não destrutiva.
- `backend/migrations/000005_add_bloqueio_login_to_usuarios.down.sql` (novo) — `ALTER TABLE usuarios DROP COLUMN IF EXISTS bloqueado_ate; DROP COLUMN IF EXISTS tentativas_login_falhas;` (molde one-liner da 000004 down).
- `backend/services/auth.go` — pacote `services`. Novas constantes `maxTentativasLogin = 5` e `duracaoBloqueioLogin = 15 * time.Minute` (perto de `accessTokenExpiracao`). Novo erro exportado `ErrContaBloqueada = errors.New("conta temporariamente bloqueada por excesso de tentativas de login")` (no bloco `var` junto a `ErrCredenciaisInvalidas`, com comentário sobre a mensagem própria ser exceção deliberada à regra de não-enumeração — ver Design Notes). `Login` (auth.go:292): `selectUsuario` passa a trazer `tentativas_login_falhas, bloqueado_ate` (usar `sql.NullTime` para `bloqueado_ate`); após computar `senhaCorreta` (auth.go:332), inserir a lógica de bloqueio descrita em **Always** — helper não-exportado `registrarFalhaLogin(db *sql.DB, usuarioID string) error` com o `UPDATE` atômico (`tentativas_login_falhas = tentativas_login_falhas + 1`, `bloqueado_ate = CASE WHEN tentativas_login_falhas + 1 >= $2 THEN now() + make_interval(mins => $3) ELSE bloqueado_ate END`). `Cadastrar` (auth.go:120): remover o guard `len(senha) > 72` (auth.go:141-143) e inserir `if err := ValidarForcaSenha(senha); err != nil { return "", err }` logo após o guard de obrigatórios (auth.go:123-125). Atualizar o doc de pacote (auth.go:1-8) mencionando Story 1.10.
- `backend/services/auth_test.go` — reusa `testDB`, `criarUsuarioParaLogin` (auth_test.go:500), `testEmailCfg`. Novos testes cobrindo cada linha da Matrix no nível de service: sequência de 5 falhas → 6ª chamada retorna `ErrContaBloqueada` com senha correta e `bloqueado_ate`/`tentativas` persistidos; reset em sucesso antes do limite; bloqueio expirado (setar `bloqueado_ate` via `UPDATE` direto para `now() - 1min`) destrava e zera; falhas não consecutivas nunca bloqueiam; e-mail inexistente martelado não faz `UPDATE` nem panica; conta desativada/não verificada com senha correta não incrementa; `Cadastrar` com senha fraca → `ErrSenhaFraca` e as três tabelas vazias; `Cadastrar` com senha forte segue criando a conta. Cuidado com testes sensíveis a tempo: preferir manipular `bloqueado_ate` por `UPDATE` direto a `time.Sleep`.
- `backend/handlers/auth.go` — pacote `handlers`. `LoginHandler` (auth.go:193): novo `case errors.Is(err, services.ErrContaBloqueada)` → `escreverErro(w, http.StatusTooManyRequests, "ACCOUNT_LOCKED", "...")`. `CadastroHandler` (auth.go:61): novo `case errors.Is(err, services.ErrSenhaFraca)` → `escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "A senha deve ter ao menos 8 caracteres, incluindo uma letra e um número.")`. Atualizar docstrings de ambos e o doc de pacote (auth.go:1-6).
- `backend/handlers/auth_test.go` — reusa `criarUsuarioParaLogin`/helper de inserção (auth_test.go:345,371), `postLogin` (auth_test.go:380). Novos: 6 `POST /api/auth/login` para a mesma conta → o 6º responde `429` com `code == "ACCOUNT_LOCKED"` e corpo sem menção a minutos/segundos; conta com `bloqueado_ate` no futuro → `POST /api/auth/esqueci-senha` responde `200` e grava `tokens_acao`/`emails_pendentes` (AC2); `POST /api/auth/cadastro` com senha `"abc"` → `400 VALIDATION_ERROR` e `SELECT count(*) FROM usuarios` inalterado.
- `backend/handlers/auth_sso_test.go` — reusa o helper de assinatura de token (auth_sso_test.go:77) e o molde de `TestKeycloakSSO_TrocaValida` (auth_sso_test.go:116). Novo `TestKeycloakSSO_ContaBloqueadaPorSenhaAindaEntra`: seed de conta ativa+verificada, `UPDATE usuarios SET tentativas_login_falhas = 5, bloqueado_ate = now() + interval '15 minutes'`, troca de token SSO válida → `200` + `Set-Cookie` refresh; reler as duas colunas e afirmar que continuam `5` e no futuro (SSO não mexeu).
- `backend/main.go` / `backend/main_test.go` — SEM rota nova. Opcional: nenhuma mudança necessária no inventário de rotas de `main_test.go`.
- `frontend/src/pages/LoginPage.tsx` — `mensagemDeErro` (LoginPage.tsx:21): `if (codigo === 'ACCOUNT_LOCKED') return 'Muitas tentativas de login. Sua conta foi bloqueada temporariamente. Use "Esqueci minha senha" para voltar a acessar.';`. Nenhuma mudança no JSX (link já presente, LoginPage.tsx:156).
- `frontend/src/pages/LoginPage.test.tsx` — novo caso: `fetch` mockado devolvendo `429` `{error:{code:'ACCOUNT_LOCKED'}}` → `role="alert"` com o texto de bloqueio; o texto NÃO contém dígitos de tempo; link "Esqueci minha senha" presente.
- `frontend/src/pages/CadastroPage.tsx` — `import { senhaAtendePolitica } from '@/lib/senha';`; constante `MENSAGEM_SENHA_FRACA` idêntica à de `RedefinirSenhaPage.tsx:47`; em `handleSubmit`, antes do `fetch` (CadastroPage.tsx:53), `if (!senhaAtendePolitica(senha)) { setErro(MENSAGEM_SENHA_FRACA); return; }`. `mensagemDeErro` (`VALIDATION_ERROR`) permanece como está — a pré-checagem client torna o `VALIDATION_ERROR` de senha fraca vindo do backend uma borda rara (JS desabilitado); documentado abaixo.
- `frontend/src/pages/CadastroPage.test.tsx` — novo caso: digitar senha `"abc"` e submeter → `role="alert"` com o critério e `fetch` NÃO chamado (molde de `RedefinirSenhaPage.test.tsx:88`). Ajustar mocks existentes que assumem senha forte se necessário.
- `frontend/src/lib/senha.ts` — apenas o comentário de cabeçalho: registrar que o espelho agora cobre TAMBÉM o autocadastro (`CadastroPage`), não só a redefinição. Sem mudança de lógica.
- `frontend/src/lib/senha.test.ts` — sem mudança (a regra não muda); revisar se algum caso novo de borda vale a pena.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000005_add_bloqueio_login_to_usuarios.{up,down}.sql` — colunas `tentativas_login_falhas` + `bloqueado_ate` em `usuarios`, aditivas.
- `backend/services/auth.go` — constantes + `ErrContaBloqueada`; contagem/bloqueio de força bruta em `Login` (`registrarFalhaLogin`, reset em sucesso/expiração, bcrypt sempre executado); `ValidarForcaSenha` em `Cadastrar`.
- `backend/services/auth_test.go` — cobrir toda a Matrix no nível de service, incl. reset consecutivo, expiração e ausência de escrita para e-mail inexistente; `Cadastrar` senha fraca não escreve nada.
- `backend/handlers/auth.go` + `auth_test.go` — `ACCOUNT_LOCKED`/`429` em `LoginHandler`; `VALIDATION_ERROR` de `ErrSenhaFraca` em `CadastroHandler`; testes de fronteira HTTP incl. 6ª tentativa → 429, esqueci-senha disponível para conta bloqueada, cadastro fraco não cria conta.
- `backend/handlers/auth_sso_test.go` — prova de que conta bloqueada por senha ainda autentica por SSO e o contador não é tocado.
- `frontend/src/pages/LoginPage.tsx` + `.test.tsx` — mensagem de `ACCOUNT_LOCKED` sem tempo restante.
- `frontend/src/pages/CadastroPage.tsx` + `.test.tsx` — pré-checagem `senhaAtendePolitica` antes do submit.
- `frontend/src/lib/senha.ts` — comentário: espelho agora cobre cadastro + redefinição.

**Acceptance Criteria:**
- Given uma conta com senha e 5 tentativas de login malsucedidas consecutivas, when a 6ª tentativa chega (mesmo com a senha correta), then a resposta é `429 ACCOUNT_LOCKED`, a mensagem não contém o tempo exato restante, e `usuarios.bloqueado_ate` está ~15 min à frente; passados os 15 min, uma nova tentativa volta a ser avaliada normalmente.
- Given uma conta bloqueada por tentativas, when o usuário aciona `POST /api/auth/esqueci-senha`, then o fluxo responde `200` e grava o token de redefinição + a linha de outbox como sempre — o bloqueio de login não afeta a redefinição.
- Given o autocadastro (`POST /api/auth/cadastro`) ou a redefinição (`POST /api/auth/redefinir-senha`) com uma senha de menos de 8 caracteres ou sem letra e número, when a requisição é processada, then a resposta é `400 VALIDATION_ERROR` explicando o critério e nenhuma conta é criada nem senha alterada.
- Given falhas de senha acumuladas para uma conta, when o mesmo usuário autentica via SSO (`POST /api/auth/sso/keycloak`) com token válido, then a sessão é emitida normalmente e o contador de bloqueio da conta não é lido nem alterado.
- Given um login bem-sucedido antes de atingir o limite, when a autenticação conclui, then `usuarios.tentativas_login_falhas` volta a `0` e `bloqueado_ate` a `NULL` (falhas precisam ser consecutivas para bloquear).

## Design Notes

- **Coluna na conta, não tabela nova:** a spine (ARCHITECTURE-SPINE.md:243,275) deixa o mecanismo de contador/lockout explicitamente em aberto "para escolher e verificar no momento da story". `tentativas_login_falhas` + `bloqueado_ate` em `usuarios` é o mínimo que atende as 4 ACs, com `UPDATE` atômico fechando a corrida entre tentativas concorrentes, e espelha o padrão "estado na linha" de `usuarios.ativo`. Uma tabela dedicada só se justificaria para contagem por IP / janelas deslizantes / analytics — nada disso está nas ACs, e a trilha de acesso auditável é a Story 1.12 (FR-38).
- **Mensagem própria do estado "bloqueada" é exceção deliberada e restrita:** o épico exige AO MESMO TEMPO "nunca revelar se um e-mail existe" e um estado de UI "conta bloqueada por tentativas" com mensagem própria (epic-1-context §UX). A leitura adotada: a regra de não-enumeração continua valendo integralmente para senha errada / e-mail inexistente / conta desativada (todas seguem `INVALID_CREDENTIALS`); só o estado pós-5-falhas tem `ACCOUNT_LOCKED` — o usuário legítimo precisa entender por que não entra nem com a senha certa e que "Esqueci minha senha" é a saída. O `429` só aparece para quem já fez 5 tentativas falhas contra aquela conta.
- **bcrypt sempre executado no caminho bloqueado:** a checagem de `bloqueado_ate` fica DEPOIS do `bcrypt.CompareHashAndPassword` (real ou dummy) que a Story 1.4 já faz para toda linha encontrada — senão o caminho "bloqueada" responderia sem o custo do bcrypt e viraria um oráculo de tempo para "esta conta existe e está bloqueada".
- **Senha correta em conta inativa/não verificada não conta como falha:** só `senhaHash.Valid && !senhaCorreta` incrementa. Uma conta desativada cujo dono digita a senha certa não é força bruta; contar ali só puniria o usuário legítimo.
- Golden — trecho do `Login` após computar `senhaCorreta` (auth.go:332):

```go
agora := time.Now().UTC()
if bloqueadoAte.Valid && bloqueadoAte.Time.After(agora) {
    return "", ErrContaBloqueada // bcrypt já rodou acima
}
if bloqueadoAte.Valid { // expirado: destrava e segue
    _, _ = db.Exec(`UPDATE usuarios SET tentativas_login_falhas = 0, bloqueado_ate = NULL
                    WHERE id = $1 AND bloqueado_ate <= now()`, id)
}
if !ativo || !emailVerificado || !senhaHash.Valid || !senhaCorreta {
    if senhaHash.Valid && !senhaCorreta {
        _ = registrarFalhaLogin(db, id)
    }
    return "", ErrCredenciaisInvalidas
}
if tentativas != 0 || bloqueadoAte.Valid {
    _, _ = db.Exec(`UPDATE usuarios SET tentativas_login_falhas = 0, bloqueado_ate = NULL WHERE id = $1`, id)
}
return id, nil
```

- **`CadastroPage` e o `VALIDATION_ERROR` ambíguo:** o backend usa `VALIDATION_ERROR` tanto para "campo obrigatório" quanto para "senha fraca". A pré-checagem client com `senhaAtendePolitica` (molde de `RedefinirSenhaPage`) resolve o caso comum com mensagem específica antes da rede; o mapeamento genérico de `VALIDATION_ERROR` fica como está para não regredir a mensagem de campos vazios.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` — Postgres real (`docker compose up -d db` ou cluster descartável via `initdb`/`pg_ctl` com `DATABASE_URL` apontado, como nas Stories 1.5–1.9). Passam os novos testes de `services/auth_test.go`, `handlers/auth_test.go` e `handlers/auth_sso_test.go`; a migration `000005` aplica sem erro.
- `cd frontend && npm run lint && npm run build && npm run test` — `oxlint` e build (`tsc` + `vite`) limpos; passam os novos casos de `LoginPage.test.tsx` e `CadastroPage.test.tsx`.
- `docker compose up --build` — `api`/`web` sobem saudáveis; 6 logins errados seguidos numa conta pelo proxy `/api` passam a devolver `429`; `POST /api/auth/esqueci-senha` para essa conta ainda responde `200`. Se `docker` indisponível, mesma nota das stories anteriores (cobertura equivalente por testes de integração contra Postgres real).

**Manual checks (if no CLI):**
- Migrar o banco, criar uma conta verificada, errar a senha 5 vezes via `POST /api/auth/login`; `SELECT tentativas_login_falhas, bloqueado_ate FROM usuarios WHERE ...` mostra `5` e um instante ~15 min à frente. A 6ª tentativa (com a senha CORRETA) responde `429 ACCOUNT_LOCKED`. `POST /api/auth/esqueci-senha` para o mesmo e-mail responde `200` e insere `tokens_acao(tipo='redefinicao_senha')`. Depois de `UPDATE usuarios SET bloqueado_ate = now() - interval '1 minute'`, o login com a senha correta volta a funcionar e zera o contador.
- `POST /api/auth/cadastro` com `"senha":"abc"` responde `400 VALIDATION_ERROR`; `SELECT count(*) FROM usuarios` não muda.

## Review Triage Log

### 2026-08-30 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 7: (high 0, medium 2, low 5)
- defer: 2: (high 0, medium 0, low 2)
- reject: 15: (high 0, medium 0, low 15)
- addressed_findings:
  - `[medium]` `[patch]` A mensagem de `ACCOUNT_LOCKED` (handler + `LoginPage`) dizia `Use "Esqueci minha senha" para voltar a acessar`, mas redefinir a senha NÃO destrava a conta (`RedefinirSenha` não toca nas colunas de bloqueio; só a expiração dos 15 min destrava). Texto reescrito nos dois lados para não prometer recuperação imediata via redefinição; assertivas de `LoginPage.test.tsx` ajustadas (mantidas as propriedades "sem dígitos / sem unidade de tempo").
  - `[medium]` `[patch]` `CadastroPage` exibia "Preencha nome, e-mail e senha para continuar." para um `400 VALIDATION_ERROR` de senha fraca vindo do servidor (só o espelho client `senhaAtendePolitica` entregava o critério — falha se os dois lados divergirem). Adotado o padrão de `RedefinirSenhaPage`: guarda client de campos obrigatórios + `mensagemDeErro('VALIDATION_ERROR')` agora devolve o critério; teste do caso `VALIDATION_ERROR` ajustado + novo caso da guarda de campos vazios.
  - `[low]` `[patch]` As escritas de bookkeeping do bloqueio em `Login` (`registrarFalhaLogin` e os dois `db.Exec` de reset) descartavam o erro em silêncio. Agora registram via `slog.Warn` — sem mudar o fluxo (uma falha de bookkeeping nunca transforma 401 em 500, e o sucesso ainda devolve o id).
  - `[low]` `[patch]` Nenhum teste concorrente cobria o `UPDATE` atômico do contador (uma regressão para read-modify-write em Go passaria em todos os testes sequenciais). Novo `TestLogin_FalhasConcorrentes` (20 goroutines, molde de `TestRedefinirSenha_Concorrente`): a conta termina bloqueada com `tentativas_login_falhas >= maxTentativasLogin`.
  - `[low]` `[patch]` `make_interval(mins => int(duracao.Minutes()))` truncava durações abaixo de 1 min (footgun se a constante mudasse). Trocado para `make_interval(secs => int(duracao.Seconds()))`.
  - `[low]` `[patch]` Uma rajada de falhas concorrentes exatamente na 5ª re-carimbava `bloqueado_ate` cada vez, empurrando o desbloqueio além dos 15 min. Adicionado `bloqueado_ate IS NULL AND` ao `CASE` — o prazo é gravado uma única vez.
  - `[low]` `[patch]` `Login` fazia dois `UPDATE` idênticos no caminho "bloqueio expirado + login bem-sucedido". A ramificação de expiração agora zera os locais (`tentativas`/`bloqueadoAte`) para o ramo de sucesso não re-disparar.

### 2026-08-30 — Review pass (acompanhamento sobre spec `done`)
- intent_gap: 0
- bad_spec: 0
- patch: 2: (high 0, medium 0, low 2)
- defer: 0
- reject: 30: (high 0, medium 0, low 30)
- addressed_findings:
  - `[low]` `[patch]` AC1 fixa "`usuarios.bloqueado_ate` está ~15 min à frente", mas a única assertiva sobre a distância (em `TestLogin_BloqueiaNaQuintaFalhaERecusaSexta`) comparava `bloqueado_ate` com `time.Now().Add(duracaoBloqueioLogin)` — os dois lados derivam da mesma constante, então uma alteração silenciosa de `duracaoBloqueioLogin` (para 5 min, 1 h, etc.) passaria verde violando a AC. Adicionada uma segunda checagem contra um limite literal (13–17 min), independente da constante (mesmo molde da checagem de ~30 min em `TestSolicitarRedefinicaoSenha_ContaExiste`).
  - `[low]` `[patch]` A regra "Always" do `<intent-contract>` incrementa o contador em `senhaHash.Valid && !senhaCorreta` SEM condicionar a `ativo`/`emailVerificado` (só a senha CORRETA em conta inelegível é isenta). Nenhum teste cobria esse lado — só o oposto (`TestLogin_SenhaCorretaEmContaNaoElegivelNaoIncrementa`) —, então um refactor que apertasse o guard para `ativo && emailVerificado && senhaHash.Valid && !senhaCorreta` passaria em toda a suíte existente. Novo `TestLogin_SenhaErradaEmContaNaoElegivelIncrementa` (conta desativada e e-mail não verificado): 1 senha errada → `tentativas=1`; 5 seguidas → `tentativas=5` + `bloqueado_ate` preenchido.
- rejeitados de nota (contexto que o revisor cego não tinha):
  - bcrypt roda antes da checagem de bloqueio; `429 ACCOUNT_LOCKED` distingue conta-com-senha de e-mail inexistente; DoS direcionado sem throttle por IP; redefinir a senha não destrava a conta; sem desbloqueio manual por adm; contagem em coluna da conta (não tabela nova) com contenção na linha de `usuarios`; 5ª falha ainda é `401`; ordem "bloqueio antes de `!ativo`/`!emailVerificado`" — **todos explicitamente decididos no `<intent-contract>`/Design Notes ou fora do escopo das 4 ACs por autoridade do próprio intent** (trilha de auditoria é a Story 1.12/FR-38).
  - remoção do guard `len(senha) > 72` em `Cadastrar` é segura: `ValidarForcaSenha` já rejeita `> 72 bytes` com `ErrSenhaFraca` (verificado em `services/auth.go:561-564`); a mensagem enganosa para passphrase longa já está no `deferred` (item 1).
  - `E1` "bloqueio expirado não-nulo nunca é limpo / `CASE` só re-bloqueia quando `IS NULL`": leitura equivocada do fluxo — a ramificação de expiração (`if bloqueadoAte.Valid`) zera a linha ANTES de `registrarFalhaLogin`, então o `CASE bloqueado_ate IS NULL` casa nas falhas seguintes; o único caminho residual exige falha persistente e parcial do `UPDATE` de reset, que é deliberadamente não-fatal.
  - `E2` "corrida do reset em sucesso com `tentativas` local defasado": o Golden snippet das Design Notes mostra exatamente o código atual; a AC5 é redigida em termos sequenciais; a falha só ocorre sob ataque concorrente no exato instante de um login bem-sucedido, é auto-corrigida no login limpo seguinte e falha para o lado seguro (em direção ao bloqueio). Não corrigido para não abrir código de produção numa passagem de acompanhamento sobre spec `done`.
  - metade `redefinir-senha` da AC3 coberta por `TestRedefinirSenha_SenhaFracaNaoConsomeToken` / `TestRedefinirSenhaHandler_SenhaFraca` (Story 1.6, caminho intocado).
  - `CadastroPage` mapeando `VALIDATION_ERROR` → critério de senha: o caminho nome/e-mail > 255 runes exige input patológico e já exibia mensagem imperfeita antes desta story; a passagem de revisão anterior triou deliberadamente esse trecho — reabrir é churn.

## Auto Run Result

Status: done

**Resumo da mudança implementada:** Story 1.10 — bloqueio de conta por força bruta (FR-36, SM-6) + política mínima de força de senha no autocadastro. O grosso já havia sido implementado e revisado no commit `6bc78ac` (migration `000005` com `usuarios.tentativas_login_falhas`/`bloqueado_ate`; contagem de falhas consecutivas com `UPDATE` atômico e `CASE` em `services.Login`; `ValidarForcaSenha` em `services.Cadastrar`; `429 ACCOUNT_LOCKED` / `400 VALIDATION_ERROR` nos handlers; mensagens no `LoginPage`/`CadastroPage`; SSO intocado). Esta invocação foi uma **passagem de revisão de acompanhamento** sobre a spec já em `done` (`followup_review_recommended: true`), com 4 camadas de revisão em paralelo (blind hunter, edge-case hunter, verification-gap, intent-alignment).

**Arquivos alterados nesta passagem:**
- `backend/services/auth_test.go` — (1) `TestLogin_BloqueiaNaQuintaFalhaERecusaSexta`: segunda assertiva prendendo `bloqueado_ate` a um limite literal de 13–17 min, independente da constante `duracaoBloqueioLogin`; (2) novo `TestLogin_SenhaErradaEmContaNaoElegivelIncrementa` (subtestes conta desativada / e-mail não verificado) fixando que uma senha errada em conta inelegível ainda incrementa o contador e bloqueia na 5ª.
- `_bmad-output/implementation-artifacts/spec-1-10-bloqueio-de-conta-e-politica-de-senha.md` — frontmatter (`status: done`, `followup_review_recommended: false`), nova entrada no `## Review Triage Log` e este `## Auto Run Result`.

Nenhum código de produção foi alterado nesta passagem.

**Achados de revisão (esta passagem):** patch 2 (aplicados), defer 0, reject 30. Detalhe em `## Review Triage Log`.
- `[low]` `[patch]` Janela de 15 min da AC1 não estava presa a um valor literal em nenhum teste (assertiva comparava com a própria constante). Corrigido.
- `[low]` `[patch]` Regra "Always" do intent (senha errada em conta inelegível incrementa) sem cobertura — um refactor a apertaria sem falhar a suíte. Novo teste.
- reject 30: em sua maioria decisões de produto já registradas explicitamente no `<intent-contract>`/Design Notes (bcrypt antes da checagem de bloqueio; exceção deliberada de não-enumeração no `429`; sem throttle por IP/CAPTCHA; sem desbloqueio manual por adm; contador em coluna da conta; 5ª falha ainda `401`), itens fora do escopo das 4 ACs por autoridade do intent (trilha de auditoria = Story 1.12/FR-38; rate-limit de `esqueci-senha`), ou duplicatas de itens já no `deferred` (mensagem enganosa de `ValidarForcaSenha` para > 72 bytes).

**Recomendação de review de acompanhamento:** `false`. Achados `patch` nesta passagem: high 0, medium 0, low 2. Score = 3×0 + 1×2 = 2 (< 5), sem `high`.

**Verificação executada (após os 2 patches):**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — limpo (sem saída de `gofmt`, exit 0).
- `cd backend && go test -p 1 -count=1 ./...` — Docker indisponível (`docker: command not found`); usado cluster PostgreSQL 16 descartável já em execução (TCP `127.0.0.1:54329`, role/db `stockflow`, `sslmode=disable`), `DATABASE_URL` apontado; a migration `000005` aplica sem erro via `migrate.Up()`. **Todos os 6 pacotes passam** (`backend`, `backend/cmd/seed-admin`, `backend/handlers`, `backend/iam`, `backend/middleware`, `backend/services`). `TestLogin_SenhaErradaEmContaNaoElegivelIncrementa` (2 subtestes) e `TestLogin_BloqueiaNaQuintaFalhaERecusaSexta` verificados em execução `-v` dedicada — PASS.
- `cd frontend && npm run lint && npm run build && npm run test` — `oxlint` e build (`tsc` + `vite`) limpos; **20 arquivos / 171 testes passando** (frontend intocado nesta passagem).
- `docker compose up --build` — **não executado**: Docker indisponível (idêntico às Stories 1.5–1.9). Superfície HTTP coberta por testes de integração contra Postgres real; frontend por testes de componente com `fetch` mockado.

**Riscos residuais:**
- `docker compose up --build` (smoke E2E) não pôde rodar por ausência do binário `docker` — risco baixo; todas as camadas têm cobertura automatizada equivalente contra Postgres real.
- **DoS direcionado** (inerente ao mecanismo pedido pelo intent): quem sabe o e-mail da vítima pode mantê-la bloqueada com 5 tentativas erradas a cada 15 min. FR-36/SM-6 especificam bloqueio por conta e nada de throttle por IP/CAPTCHA — mitigação fica para uma story de segurança dedicada.
- **Enumeração limitada:** após 5 falhas de senha, o `429 ACCOUNT_LOCKED` distingue uma conta com senha de um e-mail inexistente / conta só-SSO — exceção deliberada e restrita da regra de não-enumeração (nota de UX do épico). Senha errada / e-mail inexistente / conta desativada seguem em `401 INVALID_CREDENTIALS`.
- **Redefinir a senha não destrava a conta** — só a expiração dos 15 min; a mensagem do `429` foi redigida para não prometer o contrário.
- **Reset do contador em login bem-sucedido usa `tentativas` lido no `SELECT`** (não a linha viva): sob falhas concorrentes exatamente durante um login bem-sucedido o contador pode não zerar; é auto-corrigido no próximo login limpo e falha para o lado seguro. Corresponde ao Golden snippet das Design Notes; não alterado nesta passagem de acompanhamento.
- A árvore de trabalho retém `_bmad-output/implementation-artifacts/sprint-status.yaml` e `deferred-work.md` modificados pelo orquestrador (em paralelo a esta invocação) — intencionalmente não tocados nem revertidos.
