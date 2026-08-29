---
title: 'Story 1.3 — Autocadastro com verificação de e-mail'
type: 'feature'
created: '2026-08-29'
status: 'done'
baseline_revision: 'a430d773cc6129ac2baca2969da0c35fa2267579'
review_loop_iteration: 0
followup_review_recommended: true
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      EnviarSMTP's full success path (STARTTLS/AUTH/MAIL/RCPT/DATA) has zero
      test coverage — every existing test short-circuits on the empty
      SMTP_PASSWORD guard before reaching it.
    evidence: |-
      email_test.go's only EnviarSMTP test (TestEnviarSMTP_SemPasswordFalhaImediatamente)
      asserts the immediate-failure path when Password == "". A protocol-level
      regression in the STARTTLS/AUTH/MAIL/RCPT/DATA sequence or message
      formatting (e.g. the malformed MAIL FROM envelope this review pass just
      fixed) would ship undetected without a fake/mocked SMTP server, which
      this story's test suite deliberately avoids depending on (no test may
      depend on real SMTP credentials or network access, per this story's own
      AC4).
    location: backend/services/email.go:126 (EnviarSMTP)
    severity: medium
  - summary: >-
      If the emails_pendentes UPDATE to status='enviado' or its transaction
      commit fails right after EnviarSMTP already succeeded, the row stays
      'pendente' and the worker resends the same e-mail to the real recipient
      on the next poll.
    evidence: |-
      processarProximoEmailPendente calls EnviarSMTP (an external, already
      irreversible side effect) and then commits the 'enviado' status update
      in the same local transaction — a DB-side failure between the two
      leaves no record that the send already happened, so the row is picked
      up again by the next poll cycle. Low probability (requires a DB failure
      in the narrow window right after a successful independent SMTP call),
      and the consequence is a harmless duplicate verification e-mail (the
      link itself is idempotent — a second click is a no-op), not data
      corruption.
    location: backend/services/email_worker.go:98 (processarProximoEmailPendente)
    severity: medium
  - summary: >-
      The nginx /api reverse proxy and the Vite dev-server proxy (both added
      earlier in this story's own prior review pass) have no automated check
      that they actually forward requests to the backend end-to-end.
    evidence: |-
      docker is unavailable in this sandbox (same limitation already recorded
      in Stories 1.1/1.2), so `docker compose up --build` plus a manual
      browser check remain the only way to validate the composed web→api
      proxy chain. The web service's healthcheck only probes `/`, not
      `/api/...` through the proxy, so a regression to the pre-fix state (no
      /api proxying) would not be caught by any automated check in this repo.
    location: 'frontend/nginx.conf:13, frontend/vite.config.ts:19'
    severity: medium
  - summary: >-
      backend/handlers/auth_test.go, backend/services/auth_test.go e
      backend/cmd/seed-admin/main_test.go carregam cada um sua própria cópia
      quase idêntica de testDB()/migrateOnce (incluindo o mesmo comentário
      sobre CASCADE), em vez de compartilhar um helper interno único.
    evidence: |-
      Esta story adicionou a segunda e a terceira cópia (handlers/auth_test.go
      e services/auth_test.go) do padrão já existente desde a Story 1.1
      (cmd/seed-admin/main_test.go). Qualquer mudança futura de schema/retry de
      migration (ex. adicionar uma quarta tabela, ou mudar a política de
      retry) precisa ser replicada manualmente nas três cópias. Nenhuma AC/AD
      desta story pede a extração de um helper compartilhado, e os três
      arquivos pertencem a pacotes Go diferentes (main, services, handlers),
      então a extração exigiria decidir onde colocar um pacote de teste
      interno compartilhado — mudança estrutural maior do que um patch trivial
      desta passagem.
    location: 'backend/main_test.go:36, backend/services/auth_test.go:33, backend/handlers/auth_test.go:451'
    severity: low
---

<intent-contract>

## Intent

**Problem:** Hoje o único jeito de existir uma conta é o Adm semeado via CLI (Story 1.1); não há caminho para qualquer colaborador criar a própria conta, nem verificação de que ele controla o e-mail informado.

**Approach:** Endpoint público de cadastro que cria a conta sempre como `usuario` com `email_verificado=false`, enfileira (mesma transação) um e-mail de verificação via outbox Postgres (AD-4) e emite um token de uso único (AD-18); um endpoint público de verificação consome esse token e libera a conta. Frontend ganha a tela pública de Autocadastro e a página que processa o link recebido por e-mail.

## Boundaries & Constraints

**Always:**
- Papel vindo do formulário é sempre ignorado; conta criada sempre com `papel='usuario'` (FR-3), mesmo que o payload envie outro valor.
- E-mail normalizado para minúsculas antes de qualquer escrita/comparação (AD-14); unicidade usa o índice único já existente (`idx_usuarios_email_lower`, Story 1.1) — 409 mapeado a partir da violação desse índice (SQLSTATE 23505), mesmo padrão de `errAdminAlreadyExists` em `cmd/seed-admin/main.go`.
- Cadastro (`usuarios` + `tokens_acao` + `emails_pendentes`) é sempre UMA transação (AD-4, AD-18) — nenhuma linha órfã se qualquer insert falhar.
- Token de verificação: aleatório (`crypto/rand`, ≥32 bytes, url-safe), único, `tipo='verificacao_email'`, expira em 24h (decisão desta story — nenhuma fonte do PRD/épico fixa prazo para este tipo especificamente; os 30min de `redefinicao_senha` são só da Story 1.6). Validação sempre filtra por token+usuario_id+tipo+não expirado+não usado (AD-18) e marca usado atomicamente na mesma transação que libera `email_verificado`.
- `emails_pendentes` segue o contrato fixo do AD-4 (`destinatario`, `tipo`, `variaveis_json` jsonb — nunca HTML pré-renderizado pelo produtor); um único worker goroutine consome por polling, resolve o template pelo `tipo`, envia via `net/smtp` (config por env, mesmos nomes do `FB_APU02`), marca `enviado`/`falho`, incrementando tentativas a cada falha.
- Envelope de erro fixo (AD-14): `CONFLICT` (e-mail duplicado), `VALIDATION_ERROR` (campo obrigatório ausente), `NOT_FOUND`/`TOKEN_EXPIRED` (token de verificação inválido/expirado/já usado).
- `/cadastro` e `/verificar-email` são rotas públicas no frontend, fora do `AppShell` — mesma classificação de superfície pública do Login (`EXPERIENCE.md`), que ainda não existe nesta story.

**Block If:** nenhuma decisão desta story depende de aprovação humana. SMTP corporativo real (host/usuário/senha de produção) é configuração de ambiente via `.env` (AD-16) — mesmo tratamento já dado a `IAM_*` nas stories anteriores: variável documentada em `.env.example`, provisionamento da credencial real é operação futura que não bloqueia código nem testes.

**Never:**
- Nenhuma validação de força de senha nesta story — mínimo 8 caracteres + letra/número é AC da Story 1.10 (ainda backlog), que cita o cadastro só como consumidor da regra. Aqui só se valida presença (não-vazio).
- Nenhum endpoint de login (Story 1.4). `email_verificado=false` fica persistido corretamente para a Story 1.4 consumir; a rejeição de login propriamente dita não é testável aqui porque login não existe ainda.
- Nenhuma tabela/domínio além de `emails_pendentes`/`tokens_acao`; não altera o schema de `usuarios`.
- Nenhum reenvio manual do e-mail de verificação (não pedido por nenhuma AC).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Cadastro válido | nome/e-mail/senha preenchidos, e-mail inédito | 201; conta `usuario`/`email_verificado=false`; token + linha de outbox inseridos na mesma transação | — |
| E-mail duplicado | e-mail já existe (qualquer capitalização) | 409 `{"error":{"code":"CONFLICT"}}`, mensagem "Este e-mail já está cadastrado." | nenhuma linha nova em nenhuma tabela |
| Campo obrigatório ausente/vazio | nome, e-mail ou senha em branco | 400 `{"error":{"code":"VALIDATION_ERROR"}}` | nenhuma escrita |
| Link de verificação válido | token existe, tipo `verificacao_email`, não usado, não expirado | 200; `email_verificado=true`; token marcado usado | — |
| Link expirado | `expira_em` no passado | 400 `{"error":{"code":"TOKEN_EXPIRED"}}` | `email_verificado` inalterado |
| Link já usado | `usado_em` preenchido | 400/404 `{"error":{"code":"TOKEN_EXPIRED"}}` | idempotente, sem reaplicar efeito |
| Token inexistente/malformado | string arbitrária | 404 `{"error":{"code":"NOT_FOUND"}}` | — |

</intent-contract>

## Code Map

- `backend/migrations/000002_create_outbox_e_tokens.up.sql`/`.down.sql` -- cria `emails_pendentes` e `tokens_acao` (DDL em Design Notes), FK `usuario_id → usuarios(id)`.
- `backend/services/auth.go` (novo) -- `Cadastrar(db, nome, email, senha string)` e `VerificarEmail(db, token string)`: transação, hashing bcrypt (mesmo padrão de `cmd/seed-admin/main.go`), geração do token, mapeamento SQLSTATE 23505 → erro de duplicidade.
- `backend/services/email.go` (novo) -- `EmailConfig` lido de env (`SMTP_HOST`/`SMTP_PORT`/`SMTP_USER`/`SMTP_PASSWORD`/`SMTP_FROM`/`APP_URL`, mesmos nomes de `FB_APU02/backend/services/email.go`, lido como referência read-only), `EnfileirarEmail(tx, destinatario, usuarioID, tipo, variaveis)`, `renderizarTemplate(tipo, variaveis)` -- só `verificacao_conta` tem template implementado nesta story.
- `backend/services/email_worker.go` (novo) -- `IniciarWorkerEmail(db, cfg, intervalo) (parar func())`: goroutine de polling sobre `emails_pendentes status='pendente'`, envia via `net/smtp`, marca `enviado`/`falho` com contador de tentativas.
- `backend/handlers/auth.go` (novo) -- `POST /api/auth/cadastro`, `GET /api/auth/verificar-email`: fronteira HTTP, chama `services/`, serializa envelope de erro (AD-14).
- `backend/main.go` -- registra as duas rotas novas no `mux`; inicia o worker após as migrations, para no shutdown gracioso já existente (`shutdownDone`).
- `frontend/src/pages/CadastroPage.tsx` (novo) -- formulário público (nome/e-mail/senha; shadcn `Input`/`Label`/`Card`, `useState` simples como em `FB_APU02/frontend/src/pages/Register.tsx`, lido como referência), `POST /api/auth/cadastro`, mensagem "Verifique seu e-mail para confirmar a conta." no sucesso (`EXPERIENCE.md`), erro inline no 409.
- `frontend/src/pages/VerificarEmailPage.tsx` (novo) -- lê `?token=` via `useSearchParams`, chama `GET /api/auth/verificar-email` ao montar, mostra sucesso/erro/expirado.
- `frontend/src/App.tsx` -- duas rotas novas (`/cadastro`, `/verificar-email`) como irmãs da rota raiz do `AppShell`, não aninhadas nele.
- `frontend/src/components/ui/{input,label,card}.tsx` -- via CLI `shadcn` (ainda não instalados; ver Code Map da Story 1.2).
- `.env.example` -- acrescenta `SMTP_HOST`/`SMTP_PORT`/`SMTP_USER`/`SMTP_PASSWORD`/`SMTP_FROM`/`APP_URL`.
- Referência lida (read-only, não copiada literalmente): `/home/claudio/projetos/FB_APU02/backend/services/email.go` (nomes de env var, `net/smtp`) e `/home/claudio/projetos/FB_APU02/frontend/src/pages/Register.tsx` (form controlado sem libs novas). **Divergência deliberada:** aqui o envio é sempre assíncrono via outbox+worker (AD-4) — nunca síncrono no handler HTTP como no `FB_APU02`.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000002_*.sql` -- schema de `emails_pendentes`/`tokens_acao` -- base para AD-4/AD-18.
- `backend/services/auth.go` + `auth_test.go` (integração, Postgres real) -- `Cadastrar`/`VerificarEmail` cobrindo a I/O Matrix -- satisfaz AC1/AC2/AC4.
- `backend/services/email.go` + `email_worker.go` + testes -- outbox/worker/SMTP -- prova o ciclo `pendente→enviado`/`falho` sem depender de SMTP real.
- `backend/handlers/auth.go` + `auth_test.go` (httptest) -- endpoints HTTP e envelope de erro -- expõe AC1-4 na fronteira HTTP.
- `backend/main.go` -- registra rotas, inicia/para o worker.
- `frontend/src/pages/CadastroPage.tsx` + `VerificarEmailPage.tsx` + testes RTL -- UI pública -- expõe AC1/AC2/AC4 na superfície visível.
- `frontend/src/App.tsx` -- rotas públicas.
- `.env.example` -- documenta as novas variáveis.

**Acceptance Criteria:**
- Given um payload de cadastro com um campo de papel forjado (ex. `"papel":"adm"`), when o backend processa a requisição, then a conta é criada com `papel='usuario'` de qualquer forma — o campo do payload é ignorado.
- Given um e-mail cadastrado com letras maiúsculas, when uma segunda tentativa de cadastro usa o mesmo e-mail em outra capitalização, then o sistema responde 409 (mesma normalização usada para o e-mail já existente).
- Given o worker de e-mail rodando num ambiente sem SMTP configurado (local/CI), when ele tenta enviar uma linha pendente, then a linha registra a falha (tentativas incrementadas, `ultimo_erro` preenchido) sem derrubar o processo nem afetar `/api/health`.
- Given a suíte de testes desta story, when executada localmente ou em CI, then nenhum teste depende de credenciais reais de SMTP corporativo.

## Spec Change Log

## Review Triage Log

### 2026-08-29 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 10: (high 1, medium 4, low 5)
- defer: 0
- reject: 12: (high 0, medium 1, low 11)
- addressed_findings:
  - `[high]` `[patch]` `frontend/nginx.conf` tinha nenhum bloco de proxy para `/api` — gap explicitamente deixado em aberto pela própria Story 1.2 (`Never: "Nginx do serviço web fazendo proxy de /api nesta story"`) para quem primeiro precisasse de chamadas reais ao backend, que é esta story. Sem o proxy, o deploy composto (`web`+`api`) nunca serviria `POST /api/auth/cadastro`/`GET /api/auth/verificar-email` ao navegador, apesar de toda a suíte automatizada passar. Corrigido com um `location /api/ { proxy_pass http://api:8080; }` em `frontend/nginx.conf` e um proxy equivalente em `vite.config.ts` (`server.proxy`) para paridade com `npm run dev`.
  - `[medium]` `[patch]` `EnviarSMTP` (`backend/services/email.go`) chamava `smtp.SendMail` sem nenhum timeout de conexão/escrita; um host SMTP configurado que aceita a conexão TCP mas trava no meio do handshake bloquearia a única goroutine do worker indefinidamente — e como `parar()` só é observado entre ticks, o shutdown gracioso travaria junto. Corrigido com um dialer com timeout explícito antes do handshake SMTP.
  - `[medium]` `[patch]` `POST /api/auth/cadastro` (rota pública, sem autenticação) não limitava o tamanho do corpo da requisição antes do `json.Decode`. Corrigido com `http.MaxBytesReader` no handler.
  - `[medium]` `[patch]` `Cadastrar` não validava o tamanho da senha antes de `bcrypt.GenerateFromPassword`, que retorna erro para entradas acima de 72 bytes — o erro caía no branch genérico e virava 500 (`INTERNAL_ERROR`) em vez de 400 (`VALIDATION_ERROR`) para um input de usuário legítimo. Corrigido com uma checagem de tamanho antes do hash, retornando `ErrCadastroValidacao`.
  - `[medium]` `[patch]` Nenhum teste exercitava duas chamadas concorrentes de `VerificarEmail` disputando o mesmo token — a guarda que fecha a janela de corrida entre o SELECT e o UPDATE (`usado_em IS NULL AND expira_em > now()` no `marcarUsado`) não tinha nenhuma cobertura que falharia se fosse removida. Adicionado um teste de concorrência com duas goroutines sincronizadas por um canal de largada, mesmo padrão de `TestSeedAdmin_Concorrente` (Story 1.1), afirmando que exatamente uma chamada sucede e a outra recebe `ErrTokenExpirado`.
  - `[low]` `[patch]` Nenhum índice existia em `tokens_acao.usuario_id`/`emails_pendentes.usuario_id`, apesar de serem colunas de FK consultadas diretamente. Adicionados índices na migration 000002 (ainda não commitada em nenhuma passagem anterior).
  - `[low]` `[patch]` `Cadastrar` não validava o tamanho do e-mail normalizado antes do INSERT contra o limite `VARCHAR(255)` de `usuarios.email` — um valor maior surgia como erro cru do Postgres (500) em vez de 400. Corrigido com uma checagem de tamanho, mesmo padrão da correção de senha acima.
  - `[low]` `[patch]` Nenhum teste cobria `GET /api/auth/verificar-email` com o parâmetro `token` ausente/vazio (distinto de um token inexistente-mas-presente). Adicionado teste cobrindo esse caso, esperando 404 `NOT_FOUND`.
  - `[low]` `[patch]` Os defaults de `CarregarEmailConfig` (`SMTP_PORT`→`"587"`, `APP_URL`→`"http://localhost:8081"`) nunca eram exercitados por nenhum teste — nenhum teste chama essa função sob nenhuma configuração de variável de ambiente. Adicionado teste tabular com `t.Setenv` cobrindo o default e o override de cada variável.
  - `[low]` `[patch]` `CadastroPage.tsx`'s `handleSubmit` dependia só do atributo `disabled` do botão (que só reflete o novo estado após o re-render do React) para prevenir duplo envio — uma janela estreita antes do repaint permitiria, em tese, dois `POST` simultâneos (sem risco de conta duplicada, já a unicidade de e-mail no banco barra o segundo, mas ainda uma chamada HTTP desperdiçada). Adicionado um `if (enviando) return;` no início da função como defesa em profundidade.
  - `[reject]` 12 achados roteados para reject: `tokens_acao.tipo` usar `'verificacao_email'` enquanto `emails_pendentes.tipo` usa `'verificacao_conta'` foi apontado como inconsistência — falso positivo, são dois enums deliberadamente distintos, cada um definido literalmente com esses valores por AD-18 (`tokens_acao`) e AD-4 (`emails_pendentes`) na arquitetura; o 409 em `POST /api/auth/cadastro` revelar que um e-mail já existe (sem rate limiting) — comportamento literal exigido pela própria AC do épico ("o sistema responde 409 e a tela mostra 'Este e-mail já está cadastrado.'"), deliberadamente diferente do tratamento de login/redefinição de senha no mesmo épico, que sim escondem a existência da conta; ausência de validação de formato de e-mail (só presença é checada) — fora de escopo, nenhuma AC/AD exige, mesmo padrão já estabelecido para a validação de força de senha (explicitamente adiada para a Story 1.10); `GET /api/auth/verificar-email` consumir o token via GET, vulnerável a scanners corporativos de e-mail pré-buscando o link — a própria AC do épico descreve literalmente esse mecanismo ("quando o usuário clica no link, then email_verificado passa a true"), e mesmo no cenário do scanner a conta acaba verificada (não bloqueada), só o usuário real veria uma mensagem de "link já usado"; ausência de mecanismo de retenção/limpeza para `tokens_acao`/`emails_pendentes` — fora de escopo, nenhuma AC/AD desta story pede um job de expurgo; o código `INTERNAL_ERROR` não estar na lista de códigos fixos do AD-14 citada na spec — falso positivo, o vocabulário fixo do AD-14 é para casos de autenticação/sessão já enumerados, não proíbe um código de fallback genérico para falhas inesperadas, que toda API precisa ter; ausência de um teste E2E literal conectando `POST /api/auth/cadastro` ao worker processando a linha gerada — já coberto por composição: a I/O Matrix já exige e tem prova de que a linha de outbox é inserida, e o comportamento do worker em si (`processar`/`enviado`/`falho`) já tem cobertura própria e independente; ausência de `context.Context` propagado às chamadas de banco nos handlers — mesmo padrão já registrado como item de baixa severidade em `deferred` desde a Story 1.1 (`backend/main.go`/`cmd/seed-admin`), nenhuma AC/AD desta story exige timeout por requisição; ausência de visibilidade operacional (métricas de backlog do outbox) — fora de escopo, nenhuma AC/AD desta story pede instrumentação dedicada; falta de sanitização de `variaveis["link"]` no template — o próprio achado reconhece que não é explorável hoje (o link é sempre montado no servidor a partir de `APP_URL`), especulação sobre uma fonte de template futura menos confiável; bloqueio de cabeça-de-fila no worker (uma linha que falha repetidamente ocupa a posição mais antiga por até 5 ciclos, ~50s, antes de virar `falho` terminal e liberar a fila) — impacto limitado e temporário, nenhuma AC/AD exige justiça de ordenação, coerente com a caracterização de "ferramenta interna, volume baixo" já registrada nesta spec; recuperação de pânico em `processarProximoEmailPendente` — especulativo, verificado que `renderizarTemplate` já usa asserção de tipo no formato "comma-ok" (`variaveis["nome"].(string)`) em vez de asserção direta, então não existe caminho de pânico alcançável identificado no código atual.

### 2026-08-29 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 9: (high 1, medium 0, low 8)
- defer: 3: (high 0, medium 3, low 0)
- reject: 12: (high 0, medium 0, low 12)
- addressed_findings:
  - `[high]` `[patch]` `EnviarSMTP` (`backend/services/email.go`) passava `cfg.From` inteiro (formato `"Nome <endereco>"` documentado em `.env.example`) para `client.Mail()`, que monta o comando `MAIL FROM:<Nome <endereco>>` — malformado por RFC 5321, rejeitado por qualquer servidor SMTP real assim que credenciais reais forem provisionadas (nenhum teste existente alcançava esse trecho, todos ficam no guard de `SMTP_PASSWORD` vazio). Corrigido extraindo o endereço nu via `net/mail.ParseAddress` numa função `envelopeAddress`, testada isoladamente (`TestEnvelopeAddress`) sem depender de rede. Verificado com `go1.26.5`'s `net/smtp` (`validateLine`) que a hipótese de injeção CRLF via `destinatario`/`cfg.From` levantada por outro achado é infundada — `Client.Mail`/`Client.Rcpt` já rejeitam qualquer CR/LF antes de montar o comando.
  - `[low]` `[patch]` O guard de senha (>72 bytes, limite do bcrypt) adicionado numa passagem de revisão anterior não tinha nenhum teste cobrindo o caminho que motivou sua criação. Adicionado a `TestCadastrar_ValidacaoDeTamanho`.
  - `[low]` `[patch]` O guard de e-mail (>255 caracteres, limite da coluna) adicionado na mesma passagem anterior também não tinha teste. Adicionado ao mesmo `TestCadastrar_ValidacaoDeTamanho`.
  - `[low]` `[patch]` `Cadastrar` validava o tamanho do e-mail e da senha contra os limites da coluna/do bcrypt, mas não o de `nome` (mesma coluna `VARCHAR(255)` de `usuarios`) — um nome maior causaria o mesmo 500 bruto do Postgres em vez do 400 `VALIDATION_ERROR` esperado. Adicionado o guard em `Cadastrar` e um caso no mesmo teste acima.
  - `[low]` `[patch]` Os dois índices novos da migration 000002 (`idx_tokens_acao_usuario_id`, `idx_emails_pendentes_usuario_id`) não tinham teste, ao contrário do precedente já estabelecido para `idx_usuarios_email_lower`. Adicionado `TestRunMigrations_IndicesDeTokensEEmailsPendentes`.
  - `[low]` `[patch]` Nenhum teste de frontend exercitava uma resposta HTTP real (não uma rejeição de rede) com um código de erro não mapeado (`INTERNAL_ERROR`) em `VerificarEmailPage`. Adicionado um teste cobrindo esse caso.
  - `[low]` `[patch]` `VerificarEmailPage` usava um `useRef<boolean>` para prevenir dupla chamada, mas isso também impedia qualquer reverificação se o `token` da URL mudasse sem desmontar o componente (ex. dois links abertos na mesma aba via navegação client-side) — o usuário ficava preso ao resultado do primeiro token para sempre. Corrigido guardando o próprio token já verificado em vez de um booleano; adicionado teste cobrindo a troca de token no mesmo componente montado.
  - `[low]` `[patch]` `cadastroRequestMaxBytes` (64KB) não tinha nenhum teste forçando o limite a importar — um corpo próximo desse tamanho nunca era exercitado, então a remoção/desalinhamento silencioso do limite não seria detectado. Adicionado `TestCadastroHandler_CorpoMuitoGrande`.
  - `[low]` `[patch]` As rotas novas (`POST /api/auth/cadastro`, `GET /api/auth/verificar-email`) só eram exercitadas chamando os handlers diretamente nos testes, nunca através do `http.ServeMux` real registrado em `main.go` — um erro de digitação no padrão de rota não seria pego por nenhum teste. Extraída a construção do mux para `newMux(db, emailCfg)` e adicionado `TestNewMux_RegistraRotasDeAutenticacao`, despachando requisições pelo mux real.

### 2026-08-29 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 4: (high 0, medium 1, low 3)
- defer: 1: (high 0, medium 0, low 1)
- reject: 17: (high 0, medium 3, low 14)
- addressed_findings:
  - `[medium]` `[patch]` `Cadastrar` (`backend/services/auth.go`) validava o tamanho de `nome`/e-mail com `len()` (bytes), mas `usuarios.nome`/`usuarios.email` são `VARCHAR(255)` do Postgres, que conta caracteres — um nome com acentos (comum em PT-BR) com até 255 caracteres mas mais de 255 bytes UTF-8 era rejeitado com 400 `VALIDATION_ERROR` mesmo sendo um valor que a coluna aceitaria. Corrigido trocando `len()` por `utf8.RuneCountInString()`. Adicionado `TestCadastrar_NomeComAcentosDentroDoLimiteDeCaracteres`.
  - `[low]` `[patch]` `VerificarEmailPage`'s efeito de verificação não conferia se o token ainda era o atual antes de aplicar o resultado da resposta — se o usuário trocasse de link de verificação (mesma aba, sem recarregar) antes da primeira requisição resolver, a resposta obsoleta do token anterior podia sobrescrever o estado já exibido para o token novo. Corrigido guardando o resultado atrás de `if (tokenVerificado.current === token)` antes de aplicar `setEstado`. Adicionado teste reproduzindo a corrida com uma promise controlada manualmente.
  - `[low]` `[patch]` Os `CHECK` constraints de `tokens_acao.tipo` e `emails_pendentes.tipo`/`status` (migration 000002) não tinham nenhum teste que falharia se fossem removidos, ao contrário do precedente já estabelecido para `usuarios.papel` (`TestRunMigrations_CreateUsuariosSchema`). Adicionado `TestRunMigrations_CheckConstraintsDeTokensEEmailsPendentes`.
  - `[low]` `[patch]` O guard `if (enviando) return;` em `CadastroPage.tsx` (defesa contra duplo-submit antes do repaint do botão desabilitado) não tinha nenhum teste que falharia se fosse removido — todos os testes existentes disparam exatamente um submit. Adicionado um teste que dispara dois `submit` sem `await` entre eles (mesmo valor de `enviando` em ambas as chamadas) e afirma exatamente um `fetch`.
  - `[reject]` 17 achados roteados para reject: `docker-compose.yml`'s serviço `api` não repassa `SMTP_*`/`APP_URL` ao contêiner — real, mas fora do escopo desta story pela própria cláusula "Block If" do intent-contract (provisionamento de credenciais/config de ambiente real é operação futura que não bloqueia código nem testes desta story); `EnviarSMTP` autentica via `PlainAuth` sempre que o servidor anuncia `AUTH`, mesmo se `STARTTLS` não tiver sido oferecido (risco de credencial em texto puro) — especulativo sobre comportamento de um servidor SMTP real ainda não provisionado, nenhuma AC/AD exige TLS forçado; ausência de rate limiting/CAPTCHA em `POST /api/auth/cadastro` (ângulo de spam/mail-bombing a terceiros) — mesma família já rejeitada na passagem anterior (ausência de rate limiting no 409), nenhuma AC/AD pede mitigação; shutdown gracioso não tem orçamento de tempo total documentado/testado (HTTP + worker somados, ~20s pior caso) — nenhuma AC/AD exige um teto; `SELECT...FOR UPDATE SKIP LOCKED` mantém a transação (e a conexão) aberta durante toda a chamada `EnviarSMTP` — aceitável dado o volume documentado como baixo ("ferramenta interna"); o worker processa no máximo 1 linha por ciclo de 10s sem lotes, tornando um pico de cadastros um funil de 6 e-mails/minuto — mesma caracterização de volume baixo já registrada nesta spec; `CarregarEmailConfig`/`EnviarSMTP` não validam configuração parcial (ex. `SMTP_HOST` setado sem `SMTP_PASSWORD`) no startup — comportamento por design, é exatamente o que `ultimo_erro` por linha existe para capturar (AC3); `CadastroPage.tsx`'s atributos `required` são inertes por causa do `noValidate` no `<form>` — cosmético, o fluxo de campo vazio já é coberto pelo 400 do servidor (I/O Matrix), sem AC que exija validação nativa do navegador; os campos do formulário continuam editáveis durante o envio (só o botão fica desabilitado) — nitpick de UX fora de qualquer AC; `TestIniciarWorkerEmail_ProcessaAutomaticamente` depende de tempo real (ticker de 20ms, prazo de 2s) — janela de margem de ~100x reduz o risco de flakiness a um nível aceitável, mesmo padrão já usado em testes de goroutine deste repositório; `parar()` (retornado por `IniciarWorkerEmail`) entra em pânico se chamado duas vezes (`close` de canal já fechado) — verificado que nenhum caminho do código atual (produção ou teste) chama `parar()` mais de uma vez, mesmo padrão de achado especulativo já rejeitado em passagem anterior para `processarProximoEmailPendente`; `envelopeAddress` não valida `SMTP_FROM` antecipadamente se for inválido — `EnviarSMTP` já trataria o `MAIL FROM` malformado resultante como falha de envio normal (`ultimo_erro`), comportamento já coberto por design; comentário da migration 000002 afirma que "o filtro por usuario_id acontece na aplicação" mas `VerificarEmail` filtra só por `token`+`tipo` — falso positivo, `tokens_acao.token` é `UNIQUE`, então filtrar por `usuario_id` adicionalmente seria redundante (a linha já está unicamente identificada pelo token, e a rota não tem nenhuma fonte independente de `usuario_id` para comparar); ausência de teste do caminho de sucesso completo de `EnviarSMTP` (STARTTLS/AUTH/MAIL/RCPT/DATA) — já registrado em `deferred` desde a passagem anterior, não é achado novo; ausência de verificação automatizada ponta a ponta do proxy `/api` (nginx/Vite) — já registrado em `deferred`, não é achado novo; reenvio duplicado de e-mail se o commit que marca `enviado` falhar logo após `EnviarSMTP` ter sucesso — já registrado em `deferred`, não é achado novo; guards de tamanho de `nome`/e-mail/senha irem além do texto literal do "Never" desta story ("só... presença, não-vazio") — não contradiz o espírito da cláusula (evita 500 bruto do Postgres/bcrypt, não é validação de força de senha), mesmo padrão já aceito nas duas passagens de revisão anteriores.

## Design Notes

DDL de referência (a migration real é a fonte da verdade):

```sql
CREATE TABLE tokens_acao (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  usuario_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  token TEXT NOT NULL UNIQUE,
  tipo VARCHAR(30) NOT NULL CHECK (tipo IN ('verificacao_email','redefinicao_senha')),
  expira_em TIMESTAMPTZ NOT NULL,
  usado_em TIMESTAMPTZ,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE emails_pendentes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  usuario_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  destinatario VARCHAR(255) NOT NULL,
  tipo VARCHAR(30) NOT NULL CHECK (tipo IN ('verificacao_conta','redefinicao_senha')),
  variaveis_json JSONB NOT NULL,
  status VARCHAR(10) NOT NULL DEFAULT 'pendente' CHECK (status IN ('pendente','enviado','falho')),
  tentativas INT NOT NULL DEFAULT 0,
  ultimo_erro TEXT,
  enviado_em TIMESTAMPTZ,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Token: `crypto/rand` (32 bytes) + `base64.RawURLEncoding` — string opaca de 43 caracteres, colada direto na URL (`{APP_URL}/verificar-email?token=...`); guardado em texto puro em `tokens_acao.token` (nenhuma AD exige hash do token, mesmo nível de proteção já aplicado ao restante do schema desta fase).

Worker: cap de 5 tentativas antes de `status='falho'` (terminal); enquanto `tentativas < 5`, a linha volta para `pendente` e é reprocessada no próximo ciclo de polling (intervalo fixo de 10s, sem backoff — volume baixo, ferramenta interna). Com `SMTP_PASSWORD` vazio (ambiente local/CI, mesmo comportamento do `FB_APU02`), o envio falha de forma determinística e imediata — não há tentativa real de conexão de rede, então os testes do worker não dependem de infraestrutura externa.

`AppShell` continua sem consumidor de rota pública (Story 1.2): `/cadastro` e `/verificar-email` entram como rotas irmãs da raiz em `App.tsx`, cada uma com seu próprio layout mínimo (sem rail/bottom nav) — a tela de Login (Story 1.4) seguirá o mesmo padrão.

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: build limpo.
- `cd backend && go vet ./...` -- expected: sem warnings.
- `docker compose up -d db && cd backend && go test -p 1 ./...` -- expected: todos os testes de integração passam (cadastro, duplicidade, verificação de token, worker sem SMTP).
- `cd frontend && npm run build && npm run lint && npm run test` -- expected: build/lint limpos, testes de `CadastroPage`/`VerificarEmailPage` passam.
- `docker compose up --build` -- expected: `api`/`web` sobem saudáveis; `POST /api/auth/cadastro` e `GET /api/auth/verificar-email` respondem.

**Manual checks (if no CLI):**
- Abrir `/cadastro` no navegador, submeter o formulário, e checar via `psql`/cliente SQL que `usuarios`, `tokens_acao` e `emails_pendentes` ganharam as linhas esperadas; navegar para `/verificar-email?token=<token da linha>` e confirmar que `email_verificado` vira `true`.

## Auto Run Result

**Resumo da mudança implementada:** autocadastro público (FR-3) ponta a ponta — migration `000002`, `services/auth.go`/`email.go`/`email_worker.go`, `handlers/auth.go`, `main.go`, `CadastroPage`/`VerificarEmailPage`, proxy `/api` em `nginx.conf`/`vite.config.ts` — implementado e revisado em passagens anteriores (já commitado). Esta execução (`bmad-build-auto`) foi acionada com o spec já em `status: done` e conduziu apenas uma nova passagem de revisão de acompanhamento (4 revisores em paralelo sobre o diff completo desde `baseline_revision`), seguida da aplicação dos 4 patches encontrados.

**Arquivos alterados nesta passagem:**
- `backend/services/auth.go` -- guard de tamanho de `nome`/e-mail trocado de `len()` (bytes) para `utf8.RuneCountInString()` (caracteres, mesma unidade do `VARCHAR(255)` do Postgres).
- `backend/services/auth_test.go` -- `TestCadastrar_NomeComAcentosDentroDoLimiteDeCaracteres`.
- `backend/main_test.go` -- `TestRunMigrations_CheckConstraintsDeTokensEEmailsPendentes` (CHECK de `tokens_acao.tipo` e `emails_pendentes.tipo`/`status`).
- `frontend/src/pages/VerificarEmailPage.tsx` -- descarta resposta obsoleta de um token anterior quando o token da URL muda antes dela resolver.
- `frontend/src/pages/VerificarEmailPage.test.tsx` -- teste da corrida acima com uma promise controlada manualmente.
- `frontend/src/pages/CadastroPage.test.tsx` -- teste do guard de duplo-submit (`if (enviando) return;`) disparando dois `submit` sem `await` entre eles.

**Resultado da revisão (esta passagem):** intent_gap 0, bad_spec 0, patch 4 (high 0, medium 1, low 3) aplicados, defer 1 (low 1), reject 17 (medium 3, low 14). Detalhe completo em `## Review Triage Log`.

**Recomendação de revisão de acompanhamento:** `true` — score dos patches desta passagem `3×1 (medium) + 1×3 (low) = 6` (≥ 5).

**Verificação executada:**
- `cd backend && go build ./...` -- OK, build limpo.
- `cd backend && go vet ./...` -- OK, sem warnings.
- `go test -p 1 ./...` -- OK, 100% dos testes passam (backend, cmd/seed-admin, handlers, services), incluindo os 2 testes novos desta passagem. Executado contra um Postgres real (`docker` indisponível neste sandbox — substituído por uma instância `initdb`/`pg_ctl` standalone, mesma limitação já registrada nas Stories 1.1/1.2 e nas passagens anteriores desta story).
- `cd frontend && npm run build && npm run lint && npm run test` -- OK, build/lint limpos, 38/38 testes passam (36 anteriores + 2 novos).
- `docker compose up --build` -- não executado neste sandbox (mesma limitação de ambiente já registrada nas passagens anteriores: `docker` não está instalado). Ver risco residual abaixo.

**Riscos residuais:**
- `docker compose up --build` e a checagem manual de navegador continuam não executados neste sandbox (ambiente sem `docker`) — o proxy `/api` (nginx/Vite) permanece inspecionado por leitura, não validado ponta a ponta pelos contêineres reais.
- Os três itens já registrados em `deferred` desde a passagem anterior (caminho de sucesso completo de `EnviarSMTP` sem teste próprio; reenvio duplicado de e-mail numa janela estreita de falha pós-commit; proxy `/api` sem checagem automatizada ponta a ponta) seguem sem correção — nenhum deles foi endereçado nesta passagem, e um quarto item de baixa severidade (duplicação de `testDB()`/`migrateOnce` entre três arquivos de teste) foi adicionado.
- `docker-compose.yml`'s serviço `api` não repassa as variáveis `SMTP_*`/`APP_URL` ao contêiner — real, mas fora do escopo desta story (Block-If do intent-contract trata provisionamento de configuração de ambiente real como operação futura); relevante para quando credenciais SMTP corporativas reais forem provisionadas.
- Credenciais reais de SMTP corporativo (Ferreira Costa) continuam não provisionadas — variáveis documentadas em `.env.example`, provisionamento é operação de ambiente futura que não bloqueia esta story (mesmo tratamento já dado a `IAM_*`).

