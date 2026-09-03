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

### DW-8: `next-themes` está instalado e `sonner.tsx` chama `useTheme()`, mas nenhum `ThemeProvider` é montado em `main.tsx`/`App.tsx` e `index.css` só define tokens de tema claro — a capacidade de dark mode ex
origin: spec-deferred b13d40f0abcc
location: frontend/src/components/ui/sonner.tsx
source_spec: `spec-1-2-fundacao-do-shell-de-navegacao-e-design-tokens.md`
severity: low
reason: `DESIGN.md` não define nenhuma paleta escura (produto utilitário, sem requisito de dark mode); `frontend/src/main.tsx` e `App.tsx` não importam `ThemeProvider`. Decisão de produto sobre dark mode está fora do escopo desta story.
status: open

### DW-9: O serviço `web` do `docker-compose.yml` (Nginx) não tem headers de cache/segurança (`Cache-Control`, `X-Content-Type-Options` etc.) nem roda como usuário não-root, ao contrário do cuidado já aplicado
origin: spec-deferred 160c370f38c8
location: frontend/nginx.conf, frontend/Dockerfile
source_spec: `spec-1-2-fundacao-do-shell-de-navegacao-e-design-tokens.md`
severity: low
reason: `frontend/nginx.conf` só define `try_files` para fallback de SPA; `frontend/Dockerfile` não define `USER` na etapa `nginx:alpine` final. Hardening de produção é um item de AD-16 (envelope operacional), não uma AC desta story.
status: open

### DW-10: Os testes que verificam breakpoint responsivo (rail vs. bottom nav) e alvo de toque de 48px checam a presença das classes Tailwind (`md:flex`, `min-h-touch-target-min`) em vez do layout/tamanho comput
origin: spec-deferred f2b582bc371f
location: frontend/src/components/shell/AppShell.test.tsx
source_spec: `spec-1-2-fundacao-do-shell-de-navegacao-e-design-tokens.md`
severity: medium
reason: `frontend/src/components/shell/AppShell.test.tsx` e `ConfirmDialog.test.tsx` usam `element.className.toContain(...)`: prova que a classe certa foi escrita, não que o navegador de fato esconde/mostra os elementos nos breakpoints certos. Resolver isso exigiria infraestrutura de teste em navegador real (Playwright), que não existe em nenhuma story deste projeto ainda.
status: open

### DW-11: `frontend/.npmrc` define `legacy-peer-deps=true` para todo o projeto, mascarando qualquer conflito real de peer dependency entre React 19.2/TypeScript 7.0/Vite 8.0 (deliberadamente à frente do ecossis
origin: spec-deferred a271140e10ff
location: frontend/.npmrc
source_spec: `spec-1-2-fundacao-do-shell-de-navegacao-e-design-tokens.md`
severity: medium
reason: `npm install` sem essa flag falha por causa de `@vitejs/plugin-react` e outros pacotes com peer range desatualizado para as versões pinadas. Trade-off inerente à decisão de arquitetura de usar versões de ponta, não uma escolha desta story.
status: open

### DW-12: Não há pipeline de CI que rode `npm run build`/`lint`/`test` do frontend automaticamente — mesmo gap já registrado para o backend na Story 1.1 (DW-1), agora também presente no frontend.
origin: spec-deferred 8619f8c4b8e1
location: .github/workflows (inexistente)
source_spec: `spec-1-2-fundacao-do-shell-de-navegacao-e-design-tokens.md`
severity: low
reason: Não existe `.github/workflows` no repositório. Configurar CI/CD é AD-16 (envelope operacional de todo o projeto), não uma AC desta story.
status: open

### DW-13: Não há README (ou doc equivalente) explicando como rodar o `frontend/` localmente (`npm run dev`) ou o novo serviço `web` do `docker-compose.yml` (porta `8081:80`) — mesmo gap já registrado para o bac
origin: spec-deferred 902e8c13d3de
location: README.md (inexistente)
source_spec: `spec-1-2-fundacao-do-shell-de-navegacao-e-design-tokens.md`
severity: low
reason: Não existe `README.md` no repositório documentando nenhum dos dois stacks (backend ou frontend).
status: open

### DW-14: Não há checagem automatizada de acessibilidade (ex. `axe-core`/ `jest-axe`) apesar do `AppShell` compor ARIA não trivial (dois `<nav>` com o mesmo `aria-label`, `Tooltip`/`Sheet`/`DropdownMenu`) — os
origin: spec-deferred f342af664dd2
location: frontend/src/components/shell/AppShell.test.tsx
source_spec: `spec-1-2-fundacao-do-shell-de-navegacao-e-design-tokens.md`
severity: low
reason: `frontend/package.json` não inclui `jest-axe`/`@axe-core/react` nem equivalente para Vitest. Nenhuma AC desta story exige verificação automatizada de acessibilidade além do que já está coberto por `@testing-library` (papéis/nomes acessíveis).
status: open

### DW-15: `frontend/nginx.conf`'s `try_files $uri $uri/ /index.html;` faz fallback para o SPA em qualquer requisição não encontrada, incluindo assets estáticos com hash (JS/CSS) — uma requisição para um asset o
origin: spec-deferred 498b57af2462
location: frontend/nginx.conf
source_spec: `spec-1-2-fundacao-do-shell-de-navegacao-e-design-tokens.md`
severity: low
reason: `frontend/nginx.conf` não tem um `location` separado para assets estáticos com `try_files $uri =404;`. Mesmo tema do item já registrado sobre hardening de produção do Nginx (headers de cache/segurança, usuário não-root) — AD-16 (envelope operacional), não uma AC desta story.
status: open

### DW-16: `sonner.tsx` (Toaster global) só passa `--normal-bg`/`--normal-text`/ `--normal-border`/`--border-radius` como CSS custom properties; nenhuma das variáveis por-tipo do `sonner` (`--success-bg`, `--err
origin: spec-deferred a4a72f3a1b61
location: frontend/src/components/ui/sonner.tsx
source_spec: `spec-1-2-fundacao-do-shell-de-navegacao-e-design-tokens.md`
severity: low
reason: `frontend/src/components/ui/sonner.tsx`'s `style` prop no `<Sonner>` só popula as 4 variáveis genéricas do `sonner`. Nenhuma AC desta story exige toasts com cor por tipo — AC1 só exige que os tokens fiquem "disponíveis nas classes Tailwind geradas", não que todo consumidor gerado pelo `shadcn` já os utilize (mesmo padrão do item de dark mode acima: capacidade presente, não conectada).
status: open

### DW-17: Follow-up review still recommended for 1-2-fundação-do-shell-de-navegação-e-design-tokens after the damping cap was spent
origin: review-budget-followup
location: n/a
source_spec: `spec-1-2-fundacao-do-shell-de-navegacao-e-design-tokens.md`
severity: low
reason: The follow-up-review damping cap (limits.max_followup_reviews = 1) was spent with the story finalized (status: done, verify green) while the review pass still recommended an independent follow-up. The work was committed by bmad-loop run 20260829-164557-1d23; this entry preserves the lingering recommendation for a deliberate later review.
status: open

### DW-18: EnviarSMTP's full success path (STARTTLS/AUTH/MAIL/RCPT/DATA) has zero test coverage — every existing test short-circuits on the empty SMTP_PASSWORD guard before reaching it.
origin: spec-deferred 66f6c936967e
location: backend/services/email.go:126 (EnviarSMTP)
source_spec: `spec-1-3-autocadastro-com-verificacao-de-e-mail.md`
severity: medium
reason: email_test.go's only EnviarSMTP test (TestEnviarSMTP_SemPasswordFalhaImediatamente) asserts the immediate-failure path when Password == "". A protocol-level regression in the STARTTLS/AUTH/MAIL/RCPT/DATA sequence or message formatting (e.g. the malformed MAIL FROM envelope this review pass just fixed) would ship undetected without a fake/mocked SMTP server, which this story's test suite deliberately avoids depending on (no test may depend on real SMTP credentials or network access, per this story's own AC4).
status: open

### DW-19: If the emails_pendentes UPDATE to status='enviado' or its transaction commit fails right after EnviarSMTP already succeeded, the row stays 'pendente' and the worker resends the same e-mail to the real
origin: spec-deferred 96f0710f2c65
location: backend/services/email_worker.go:98 (processarProximoEmailPendente)
source_spec: `spec-1-3-autocadastro-com-verificacao-de-e-mail.md`
severity: medium
reason: processarProximoEmailPendente calls EnviarSMTP (an external, already irreversible side effect) and then commits the 'enviado' status update in the same local transaction — a DB-side failure between the two leaves no record that the send already happened, so the row is picked up again by the next poll cycle. Low probability (requires a DB failure in the narrow window right after a successful independent SMTP call), and the consequence is a harmless duplicate verification e-mail (the link itself is idempotent — a second click is a no-op), not data corruption.
status: open

### DW-20: The nginx /api reverse proxy and the Vite dev-server proxy (both added earlier in this story's own prior review pass) have no automated check that they actually forward requests to the backend end-to-
origin: spec-deferred 1b40f3197b89
location: frontend/nginx.conf:13, frontend/vite.config.ts:19
source_spec: `spec-1-3-autocadastro-com-verificacao-de-e-mail.md`
severity: medium
reason: docker is unavailable in this sandbox (same limitation already recorded in Stories 1.1/1.2), so `docker compose up --build` plus a manual browser check remain the only way to validate the composed web→api proxy chain. The web service's healthcheck only probes `/`, not `/api/...` through the proxy, so a regression to the pre-fix state (no /api proxying) would not be caught by any automated check in this repo.
status: open

### DW-21: backend/handlers/auth_test.go, backend/services/auth_test.go e backend/cmd/seed-admin/main_test.go carregam cada um sua própria cópia quase idêntica de testDB()/migrateOnce (incluindo o mesmo comentár
origin: spec-deferred 6d601ab3a123
location: backend/main_test.go:36, backend/services/auth_test.go:33, backend/handlers/auth_test.go:451
source_spec: `spec-1-3-autocadastro-com-verificacao-de-e-mail.md`
severity: low
reason: Esta story adicionou a segunda e a terceira cópia (handlers/auth_test.go e services/auth_test.go) do padrão já existente desde a Story 1.1 (cmd/seed-admin/main_test.go). Qualquer mudança futura de schema/retry de migration (ex. adicionar uma quarta tabela, ou mudar a política de retry) precisa ser replicada manualmente nas três cópias. Nenhuma AC/AD desta story pede a extração de um helper compartilhado, e os três arquivos pertencem a pacotes Go diferentes (main, services, handlers), então a extração exigiria decidir onde colocar um pacote de teste interno compartilhado — mudança estrutural maior do que um patch trivial desta passagem.
status: open

### DW-22: Follow-up review still recommended for 1-3-autocadastro-com-verificação-de-e-mail after the damping cap was spent
origin: review-budget-followup
location: n/a
source_spec: `spec-1-3-autocadastro-com-verificacao-de-e-mail.md`
severity: low
reason: The follow-up-review damping cap (limits.max_followup_reviews = 1) was spent with the story finalized (status: done, verify green) while the review pass still recommended an independent follow-up. The work was committed by bmad-loop run 20260829-175442-6327; this entry preserves the lingering recommendation for a deliberate later review.
status: open

### DW-23: Duplicação de `erroEnvelope`/`erroDetalhe`/`escreverErro` entre `backend/middleware/auth.go` e `backend/handlers/auth.go`, criada deliberadamente para evitar um ciclo de import entre os dois pacotes.
origin: spec-deferred fa03231fb145
location: backend/middleware/auth.go:35, backend/handlers/auth.go:17
source_spec: `spec-1-4-login-por-e-mail-e-senha.md`
severity: low
reason: middleware/auth.go define seu próprio erroEnvelope/erroDetalhe/escreverErro idênticos aos de handlers/auth.go porque middleware nunca pode importar handlers (a composição RequireAuth(handlers.MeHandler()) acontece em main.go, na direção oposta). Uma extração para um pacote de baixo nível compartilhado (ex. apperror) removeria a duplicação, mas é uma mudança estrutural maior que um patch trivial desta passagem — mesmo padrão já usado para a duplicação de testDB() entre três arquivos na Story 1.3.
status: open

### DW-24: Duplicação de helpers de teste ("inserir usuário direto em `usuarios` com controle de estado" e `testJWTSecret`) entre `backend/handlers/auth_test.go`, `backend/middleware/auth_test.go` e `backend/ser
origin: spec-deferred ebd706af62c3
location: backend/handlers/auth_test.go:337-380, backend/middleware/auth_test.go:1018-1035, backend/services/auth_test.go:500-521
source_spec: `spec-1-4-login-por-e-mail-e-senha.md`
severity: low
reason: criarUsuarioLogin/criarUsuarioLoginComEstado (handlers), criarUsuario (middleware) e criarUsuarioParaLogin (services) são três variações quase idênticas do mesmo helper, e testJWTSecret é redeclarado verbatim nos três arquivos — mesmo padrão de duplicação já deferido para testDB() na Story 1.3 (arquivos de teste em pacotes Go diferentes não podem compartilhar um helper não-exportado sem um pacote de suporte de teste dedicado, mudança estrutural maior que um patch trivial desta passagem).
status: open

### DW-25: Um POST /api/auth/login com a senha antiga concorrente à transação de RedefinirSenha pode criar uma sessão que sobrevive ao "revoga todas as sessões".
origin: spec-deferred 0964c24538d3
location: backend/services/auth.go RedefinirSenha / Login
source_spec: `spec-1-6-recuperacao-de-senha-por-e-mail.md`
severity: low
reason: RedefinirSenha roda sob READ COMMITTED e faz UPDATE sessoes SET revogado_em = now() WHERE revogado_em IS NULL; uma sessão inserida por um Login concorrente que valida a senha antiga antes do commit do novo senha_hash não é vista por esse UPDATE. Janela de milissegundos; após o commit a senha antiga deixa de funcionar e a sessão sobrevivente expira em <=2h. Correção proporcional exige SELECT ... FOR UPDATE na linha de usuarios tanto em RedefinirSenha quanto no caminho de Login — mudança de dois lados sobre um padrão estabelecido do repo (nenhum acesso usa lock de linha hoje).
status: open

### DW-26: O token de redefinição permanece na URL e no histórico do navegador após o mount de RedefinirSenhaPage e na tela de sucesso; sem history.replaceState e sem meta Referrer-Policy.
origin: spec-deferred f82731ca5bbd
location: frontend/src/pages/RedefinirSenhaPage.tsx
source_spec: `spec-1-6-recuperacao-de-senha-por-e-mail.md`
severity: low
reason: RedefinirSenhaPage lê ?token= e nunca o remove da barra de endereço. Espelha o padrão já existente de VerificarEmailPage (Story 1.3), mas o token de redefinição é mais sensível (permite definir senha). Mitigações naturais: single-use, TTL 30min, consumido no primeiro uso bem-sucedido, e o uso revoga sessões. A página não carrega subrecursos de terceiros hoje, então o vetor de Referer é teórico.
status: open

### DW-27: RedefinirSenhaPage não tem retry no lugar após falha transitória do GET de validação no mount — um token válido que pega um 5xx/erro de rede força o usuário a pedir um link novo.
origin: spec-deferred ec696cc6a325
location: frontend/src/pages/RedefinirSenhaPage.tsx:62-87
source_spec: `spec-1-6-recuperacao-de-senha-por-e-mail.md`
severity: low
reason: O useEffect grava tokenValidado.current = token antes do fetch; em erro a fase vira 'erro' e o guard de early-return impede nova validação naquela aba. Estrutura copiada de VerificarEmailPage. Existe caminho de recuperação (botão "Solicitar novo link"), porém mais pesado (novo round-trip de e-mail). Falhas transitórias são pouco frequentes.
status: open

### DW-28: ValidarForcaSenha rejeita senha acima de 72 bytes com a mensagem "ao menos 8 caracteres, incluindo uma letra e um número" — enganosa para uma passphrase longa; a Story 1.10 propaga esse texto também a
origin: spec-deferred 99225d7c096c
location: backend/services/auth.go (ValidarForcaSenha) / frontend/src/lib/senha.ts
source_spec: `spec-1-10-bloqueio-de-conta-e-politica-de-senha.md`
severity: low
reason: ValidarForcaSenha (services/auth.go) devolve ErrSenhaFraca tanto para <8 runes quanto para >72 bytes, e handlers/auth.go (Cadastro e Redefinir) + frontend (MENSAGEM_SENHA_FRACA em CadastroPage/RedefinirSenhaPage) exibem só o critério de mínimo. Um usuário colando uma passphrase forte de >72 bytes é informado de que ela é "curta". Comportamento pré-existente da Story 1.6, agora também visível no cadastro. Uma mensagem própria de "máximo 72 caracteres" resolveria.
status: open

### DW-29: POST /api/auth/esqueci-senha não tem limite de taxa — cada chamada enfileira um e-mail no outbox, servindo de vetor de e-mail-bomba contra um endereço conhecido.
origin: spec-deferred ee83b54dbd7f
location: backend/handlers/auth.go (EsqueciSenhaHandler) / services.SolicitarRedefinicaoSenha
source_spec: `spec-1-10-bloqueio-de-conta-e-politica-de-senha.md`
severity: low
reason: EsqueciSenhaHandler (handlers/auth.go) responde sempre 200 e chama SolicitarRedefinicaoSenha, que enfileira uma linha em emails_pendentes por requisição (invalida tokens anteriores, mas não limita e-mails enviados). Gap pré-existente da Story 1.6; a Story 1.10 o torna mais relevante ao empurrar usuários bloqueados para esse endpoint. Fora do escopo de FR-36 (que só pede bloqueio no login por senha), mas merece uma passagem dedicada de segurança.
status: open

### DW-30: Tentativas no SEGUNDO fator (código TOTP errado em POST /api/auth/mfa/verificar, e o bloqueio na 6ª falha de código) não geram linha em `logs_acesso` — só a etapa de senha aparece, marcada `sucesso=tr
origin: spec-deferred 29649c86f9bb
location: backend/handlers/auth_mfa.go (MFAVerificarHandler)
source_spec: `spec-1-12-log-de-acesso-e-auditoria.md`
severity: medium
reason: A AC do épico enumera o método como "senha ou SSO"; MFAVerificarHandler (backend/handlers/auth_mfa.go) não chama registrarTentativaLogin. A força bruta de código já é contada pelo lockout da Story 1.10, mas o `adm` não enxerga no log de acesso quando um segundo fator falhou repetidamente. Decisão deliberada da spec (`<intent-contract>` → Never), registrada aqui.
status: open

### DW-31: Não há rotina de expurgo/arquivamento dos 12 meses de retenção que o PRD §9 define para o log de acesso — a tabela `logs_acesso` cresce sem teto.
origin: spec-deferred 9b86eb332a37
location: backend/migrations/000007_create_logs_acesso.up.sql
source_spec: `spec-1-12-log-de-acesso-e-auditoria.md`
severity: low
reason: PRD §9 deixa a política de retenção "a detalhar na Arquitetura" e a ARCHITECTURE-SPINE não a fixa. Uma story de operação futura precisa de um job de purge/partição por tempo. Cada tentativa de login falha (inclusive contra e-mail inexistente) grava uma linha + entrada de índice.
status: open

### DW-32: GET /api/logs-acesso só filtra por período — não por resultado (sucesso/falha) nem por método — e `logs_acesso` não tem índice em `sucesso`, `email_informado` ou `usuario_id`.
origin: spec-deferred 6b777c5a7148
location: backend/services/logs_acesso.go (ListarLogsAcesso)
source_spec: `spec-1-12-log-de-acesso-e-auditoria.md`
severity: low
reason: O uso primário de um log de acesso é "mostre as falhas" ou "as falhas de uma conta"; hoje isso é varredura completa da tabela. Fora do escopo da AC do épico (que pede só filtro por período), mas provável necessidade quando o volume crescer sob credential stuffing.
status: open

### DW-33: Ao confirmar uma exclusão, o foco do teclado cai para o <body> porque o <Button> "Excluir" da linha (o elemento que abriu o ConfirmDialog) é desmontado quando a linha some, e o AlertDialog do Radix nã
origin: spec-deferred 3aedea89daab
location: frontend/src/components/estoques/LocaisEstoqueSection.tsx
source_spec: `spec-2-2-exclusao-de-estoque-trata-residuos-e-pedidos-pendentes.md`
severity: medium
reason: LocaisEstoqueSection.tsx: cada <li> tem seu próprio botão "Excluir"; após o DELETE bem-sucedido, carregar() remove a linha e o botão que era o trigger deixa de existir. O ConfirmDialog/AlertDialog restaura foco no trigger ao fechar; sem trigger, o foco vai para o body — regressão de navegação por teclado/leitor de tela. GestaoUsuariosSection (o outro consumidor de ConfirmDialog) não expõe isso porque lá o trigger nunca é desmontado pela ação confirmada. Correção provável: mover o foco para um elemento estável (heading "Locais" ou o input de nome) no onOpenChange do diálogo quando a exclusão foi confirmada.
status: open

### DW-34: Numa corrida em que outro operador já excluiu o mesmo Estoque, o DELETE volta 404 e o frontend exibe o alerta genérico "Não foi possível excluir o estoque agora. Tente novamente." — enganoso, já que a
origin: spec-deferred 4d2fc001b929
location: frontend/src/components/estoques/LocaisEstoqueSection.tsx
source_spec: `spec-2-2-exclusao-de-estoque-trata-residuos-e-pedidos-pendentes.md`
severity: low
reason: LocaisEstoqueSection.tsx `excluir()`: o contrato da story (intent-contract > Boundaries > Always) determina "qualquer `!res.ok` → setErro( MENSAGEM_ERRO_EXCLUIR)", então 404 cai no ramo de erro. Como o `finally` sempre chama carregar(), o usuário vê simultaneamente a linha sumir e um alerta vermelho de falha. Baixa frequência (exige exclusão concorrente do mesmo id). Correção provável: tratar `res.status === 404` como sucesso idempotente (toast de sucesso + recarga), sem alerta — mas isso desvia do texto literal do intent-contract e deve ser confirmado por um humano.
status: open

### DW-35: Nenhuma chamada ao banco (services/produtos.go, services/estoques.go) propaga *Context a partir de r.Context() do handler, então uma requisição cancelada/ expirada não interrompe a transação em andame
origin: spec-deferred 34228004349e
location: backend/services/produtos.go, backend/services/estoques.go
source_spec: `spec-3-1-cadastro-manual-de-produto-com-dimensoes-estruturadas.md`
severity: low
reason: Padrão pré-existente em todo o pacote services (CriarEstoque, ExcluirEstoque antes desta story, ListarUsuarios, etc.) — nenhum usa QueryContext/ExecContext/ BeginTx com contexto. Esta story replica o padrão já estabelecido, não o introduz.
status: open

### DW-36: Componentes de seção que buscam dados no mount (CadastroProdutoSection e irmãos) não guardam contra setState após unmount durante um fetch em andamento.
origin: spec-deferred 5427cc2a5364
location: frontend/src/components/produtos/CadastroProdutoSection.tsx
source_spec: `spec-3-1-cadastro-manual-de-produto-com-dimensoes-estruturadas.md`
severity: low
reason: Mesmo padrão já presente em LocaisEstoqueSection.tsx (Story 2.1, pré- existente) — nenhuma seção do projeto usa um guard de "still mounted" nos efeitos de carregamento inicial. Baixo risco prático: são as primeiras telas carregadas após navegação, raramente desmontadas antes do fetch resolver.
status: open

### DW-37: A tabela `nomenclatura_templates` (seed fixo dos 28 templates) usa `subtipo` (texto livre com em-dash, ex. "Cabos — Elétrico") como único handle natural para uma futura migração legada (Story 3.7) ree
origin: spec-deferred 1746e011bd7a
location: backend/migrations/000013_create_nomenclatura_templates.up.sql
source_spec: `spec-3-2-nomenclatura-guiada-por-subtipo.md`
severity: low
reason: Comentário da migration 000013 e do Code Map da spec-3-2 citam explicitamente addendum §G como fonte única e a Story 3.7 como quem "encontra as linhas já gravadas e não as reinsere" — mas nada além do texto de `subtipo` amarra essa expectativa. Achado do Blind Hunter (review automatizado) na primeira revisão desta story.
status: open

### DW-38: Follow-up review still recommended for 3-2-nomenclatura-guiada-por-subtipo after the damping cap was spent
origin: review-budget-followup
location: n/a
source_spec: `spec-3-2-nomenclatura-guiada-por-subtipo.md`
severity: low
reason: The follow-up-review damping cap (limits.max_followup_reviews = 1) was spent with the story finalized (status: done, verify green) while the review pass still recommended an independent follow-up. The work was committed by bmad-loop run 20260830-172544-5790; this entry preserves the lingering recommendation for a deliberate later review.
status: open

### DW-39: A ordenação de ListarFotosProduto por nome de arquivo depende de o retry anti-colisão de SalvarFotoProduto nunca produzir um timestamp menor que um upload anterior, sob concorrência real.
origin: spec-deferred f56dfb167f43
location: backend/services/fotos.go (ListarFotosProduto, SalvarFotoProduto)
source_spec: `spec-3-6-galeria-e-visualizacao-ampliada-de-fotos-lightbox.md`
severity: low
reason: SalvarFotoProduto (Story 3.5) só avança o timestamp em colisões, então a ordem é preservada sob uploads sequenciais; sob uploads verdadeiramente concorrentes ao mesmo Produto essa garantia não tem teste cobrindo-a — característica pré-existente da Story 3.5, não introduzida por esta story.
status: open

### DW-40: ListarFotosProduto e ServirFotoProdutoHandler usam o id da URL sem canonicalizar case ao montar o glob/regex do nome de arquivo, embora a checagem no banco seja case-insensitive.
origin: spec-deferred b12f8a8aa840
location: backend/services/fotos.go:116 (ListarFotosProduto), backend/handlers/fotos.go (ServirFotoProdutoHandler)
source_spec: `spec-3-6-galeria-e-visualizacao-ampliada-de-fotos-lightbox.md`
severity: low
reason: Um id com case diferente do usado no upload faria a listagem/GET não encontrar arquivos existentes mesmo com o Produto existindo; mesmo padrão já presente em ServirFotoProdutoHandler desde a Story 3.5 (regex via regexp.QuoteMeta) — risco prático desprezível, pois o id sempre vem do mesmo fluxo de resposta do servidor, nunca digitado.
status: open

### DW-41: GET /api/produtos/{id}/fotos pode listar, via filepath.Glob, um arquivo que SalvarFotoProduto ainda está escrevendo, servindo bytes truncados a um visualizador concorrente.
origin: spec-deferred 0b74ba4bb09d
location: backend/services/fotos.go (ListarFotosProduto vs SalvarFotoProduto)
source_spec: `spec-3-6-galeria-e-visualizacao-ampliada-de-fotos-lightbox.md`
severity: low
reason: Janela de milissegundos (entre os.OpenFile e Close em SalvarFotoProduto), autolimitante — uma rebusca seguinte corrige, sem perda permanente de dado. Corrigir exigiria mudar a escrita de SalvarFotoProduto (Story 3.5), fora do escopo desta story pelo Never da própria spec.
status: open

### DW-42: GET /api/produtos/{id}/fotos pode devolver 200 com lista vazia para um Produto excluído entre a checagem de existência e o filepath.Glob, em vez de 404 NOT_FOUND.
origin: spec-deferred 4e8a99c14089
location: backend/services/fotos.go (ListarFotosProduto)
source_spec: `spec-3-6-galeria-e-visualizacao-ampliada-de-fotos-lightbox.md`
severity: low
reason: ListarFotosProduto verifica a existência do Produto e só depois roda filepath.Glob, sem reverificar; numa janela de milissegundos entre as duas operações uma exclusão concorrente do Produto faria a resposta cair para "0 fotos" em vez de "produto não encontrado". Hoje não existe nenhum fluxo de exclusão de Produto no sistema, então é impraticável de disparar no estado atual do app.
status: open

### DW-43: Se a busca do blob de uma foto no MEIO de uma rebusca de galeria falhar, o Object URL já criado para uma foto anterior na mesma chamada fica órfão no cache local até o componente desmontar.
origin: spec-deferred a614f1030394
location: frontend/src/components/produtos/CadastroProdutoSection.tsx (carregarFotos)
source_spec: `spec-3-6-galeria-e-visualizacao-ampliada-de-fotos-lightbox.md`
severity: low
reason: carregarFotos (CadastroProdutoSection.tsx) grava cada Object URL resolvido em objectUrlCacheRef.current dentro do for, mas só publica `fotos` (setFotos) depois do loop inteiro terminar com sucesso; se uma foto posterior falhar, a função retorna false antes do setFotos, e o Object URL da foto anterior nunca é exibido nem revogado até o componente desmontar (quando todo o cache é revogado em bloco) — vazamento pequeno e limitado a essa sessão de tela, sem impacto funcional visível.
status: open

### DW-44: docker-compose.yml deixa JWT_SECRET cair silenciosamente no segredo de desenvolvimento quando a variável não está definida no ambiente, em vez de falhar rápido.
origin: spec-deferred a2e567d75dc1
location: docker-compose.yml:36
source_spec: `spec-4-1-busca-por-nome-codigo-categoria-com-sugestoes.md`
severity: medium
reason: Confirmado lendo docker-compose.yml:36: `JWT_SECRET: ${JWT_SECRET:-dev-jwt-secret-nao-usar-em-producao}`. Se um operador esquecer de configurar JWT_SECRET no Coolify, o container sobe em produção assinando/verificando JWTs com um segredo conhecido publicamente no repositório, sem nunca falhar — contradiz o padrão fail-fast que o próprio main.go documenta para DATABASE_URL/JWT_SECRET. Achado independentemente pelo Blind Hunter e pelo Edge Case Hunter na revisão de follow-up da Story 4.1; não é causado por esta story (introduzido em commits de docker-compose anteriores à implementação desta story).
status: open

### DW-45: docker-compose.yml usa valores hardcoded (porta 8080, diretório /data/fotos) no healthcheck do `api` e no volume de fotos, em vez de referenciar as próprias variáveis PORT/FOTOS_DIR que esses mesmos s
origin: spec-deferred 82a6e75d5262
location: docker-compose.yml
source_spec: `spec-4-1-busca-por-nome-codigo-categoria-com-sugestoes.md`
severity: low
reason: Confirmado lendo docker-compose.yml: linhas 32/40 definem `${PORT:-8080}`/`${FOTOS_DIR:-/data/fotos}`, mas a linha 78 (healthcheck do `api`) usa `http://127.0.0.1:8080/...` literal e a linha 69 (volume) usa `/data/fotos` literal. Se um operador sobrescrever PORT ou FOTOS_DIR, o healthcheck passa a testar a porta errada (api nunca fica "healthy") e/ou fotos gravadas fora do volume persistente `stockflow-fotos-data` (perdidas num restart). Achado pelo Edge Case Hunter na revisão de follow-up da Story 4.1; não é causado por esta story.
status: open

### DW-46: RegistrarTransferencia/RegistrarBaixa não propagam context.Context para a transação; uma desconexão do cliente no meio deixa a transação rodando segurando os locks de linha de produto_estoque.
origin: spec-deferred 3e3e221835f7
location: backend/services/movimentacoes.go
source_spec: `spec-5-2-registrar-transferencia-entre-estoques.md`
severity: low
reason: backend/services/movimentacoes.go usa db.Begin() + tx.QueryRow/tx.Exec sem variante *Context. Padrão pré-existente de toda a camada de serviço (Story 5.1 inclusive) — não introduzido por esta story; corrigir exige mudar a convenção de assinatura em várias funções.
status: open

### DW-47: A lista de Estoques destino do diálogo Transferir é buscada uma vez por instância do componente e nunca recarregada (nem ao reabrir o diálogo, nem em reconexão SSE) — um Estoque criado/renomeado noutr
origin: spec-deferred 345fc6dafd2c
location: frontend/src/pages/ProdutoDetalhePage.tsx
source_spec: `spec-5-2-registrar-transferencia-entre-estoques.md`
severity: low
reason: frontend/src/pages/ProdutoDetalhePage.tsx: carregarEstoquesDestino só roda quando estoquesDestino === null. Janela de defasagem pequena e o servidor continua a autoridade (rejeita origem==destino / destino inválido).
status: open

### DW-48: travarLinhaProdutoEstoque emite um UPDATE no-op (ON CONFLICT DO UPDATE SET quantidade = produto_estoque.quantidade) mesmo quando a linha já existe, gerando uma tupla morta por trava.
origin: spec-deferred 22d02513b147
location: backend/services/movimentacoes.go
source_spec: `spec-5-2-registrar-transferencia-entre-estoques.md`
severity: low
reason: Custo de MVCC menor numa tabela de baixo volume (uma linha por par produto/estoque) e sem triggers em produto_estoque. Um fast-path SELECT ... FOR UPDATE com fallback para o upsert evitaria a escrita mantendo a garantia de "linha de destino pode não existir".
status: open

### DW-49: quantidade NaN/±Inf passa pelas guardas <=0 e >limiteNumeric103 em RegistrarTransferencia/RegistrarBaixa.
origin: spec-deferred 0a6b44264ae4
location: backend/services/movimentacoes.go
source_spec: `spec-5-2-registrar-transferencia-entre-estoques.md`
severity: low
reason: Inalcançável pelo único chamador real (o handler HTTP): encoding/json rejeita literais NaN/Inf com erro de decode -> 400. Só afeta chamadas diretas ao serviço; seria um ajuste de consistência nas duas funções.
status: open

### DW-50: Se carregarDetalhe() falhar logo após uma transferência/baixa bem-sucedida, o catch chama setErro* num diálogo já fechado enquanto o toast de sucesso já apareceu — o usuário vê sucesso e números defas
origin: spec-deferred 49e73aa0fe3e
location: frontend/src/pages/ProdutoDetalhePage.tsx
source_spec: `spec-5-2-registrar-transferencia-entre-estoques.md`
severity: low
reason: Padrão herdado verbatim de confirmarBaixa (Story 5.1): toast.success -> setXEstoque(null) -> await carregarDetalhe() -> catch setErroX(...). Deveria ser corrigido nos dois fluxos de forma consistente.
status: open

### DW-51: Os inputs de quantidade dos diálogos de Baixa e Transferir (type="number") não têm min/step, permitindo digitar/rolar valores zero ou negativos antes de qualquer round-trip ao servidor.
origin: spec-deferred e1f476d4edfc
location: frontend/src/pages/ProdutoDetalhePage.tsx
source_spec: `spec-5-2-registrar-transferencia-entre-estoques.md`
severity: low
reason: frontend/src/pages/ProdutoDetalhePage.tsx: <Input type="number" inputMode="decimal"> em ambos os diálogos, sem atributo min. Padrão herdado verbatim de confirmarBaixa (Story 5.1) e replicado em confirmarTransferencia (Story 5.2); o servidor já rejeita com 400, mas o feedback só chega depois do POST.
status: open

### DW-52: GET /api/estoques devolve a lista completa sem filtro nem paginação, tanto no Select de Estoque destino do diálogo Transferir quanto no cadastro de Produto — não escala bem para um catálogo de Estoque
origin: spec-deferred 0865194f7b12
location: frontend/src/pages/ProdutoDetalhePage.tsx
source_spec: `spec-5-2-registrar-transferencia-entre-estoques.md`
severity: low
reason: frontend/src/pages/ProdutoDetalhePage.tsx (carregarEstoquesDestino) e CadastroProdutoSection.tsx:269 usam o mesmo GET /api/estoques sem paginação. Padrão pré-existente reaproveitado tal qual por esta story, não introduzido por ela.
status: open

### DW-53: A guarda anti-duplo-submit (enviandoBaixa/enviandoTransferencia) é lida e setada dentro da própria função assíncrona, não sincronamente no disparo — dois gatilhos quase simultâneos (Enter + clique) po
origin: spec-deferred 5d56b6722f5e
location: frontend/src/pages/ProdutoDetalhePage.tsx
source_spec: `spec-5-2-registrar-transferencia-entre-estoques.md`
severity: low
reason: frontend/src/pages/ProdutoDetalhePage.tsx: confirmarBaixa (Story 5.1) e confirmarTransferencia (Story 5.2) checam `enviando... ` como primeira linha da função async, mas só chamam `setEnviando...(true)` depois — mesma janela de corrida nos dois fluxos. Padrão herdado verbatim de Story 5.1, não introduzido por esta story.
status: open

### DW-54: ListarMovimentacoes usa JOIN interno em produtos e usuarios — se um Produto ou Usuario for algum dia removido em hard-delete (ex. exclusão LGPD do Epic 8), a Movimentação correspondente some de uma tr
origin: spec-deferred 3ca0b9e0ff77
location: backend/services/movimentacoes.go:438
source_spec: `spec-5-3-historico-de-movimentacoes-consultavel.md`
severity: low
reason: backend/services/movimentacoes.go: `JOIN produtos p` / `JOIN usuarios u`. Seguro hoje — a migration 000021 documenta que produtos/estoques/usuarios nunca são excluídos, e a anonimização LGPD preserva a linha. Vira risco só quando o Epic 8 introduzir exclusão real. LEFT JOIN + COALESCE para um rótulo placeholder resolveria.
status: open

### DW-55: GET /api/movimentacoes não tem paginação nem filtro (produto, tipo, autor, período) e o teto é 500 — depois que a Story 5.4 importar o histórico legado em massa, a maior parte da trilha ficará inacess
origin: spec-deferred fc7d3dca1d1c
location: backend/services/movimentacoes.go:437 / backend/handlers/movimentacoes.go:30
source_spec: `spec-5-3-historico-de-movimentacoes-consultavel.md`
severity: medium
reason: backend/services/movimentacoes.go: `LIMIT 500`, sem OFFSET nem WHERE; handler sem query params por decisão da spec. Espelha o teto de logs_acesso (que shipou assim), mas logs_acesso tem filtro de período e Movimentações não.
status: open

### DW-56: Um Estoque excluído (Story 2.2 permite excluir Estoque sem quantidade residual) que aparece como origem/destino de uma Movimentação antiga é renderizado como "—" no Histórico, indistinguível de um lad
origin: spec-deferred a4bc271208e0
location: frontend/src/components/estoques/MovimentacoesSection.tsx:983
source_spec: `spec-5-3-historico-de-movimentacoes-consultavel.md`
severity: low
reason: LEFT JOIN estoques devolve nome NULL; o frontend faz `mov.estoqueOrigemNome ?? '—'`. COALESCE para "(estoque removido)" quando o id existe mas o nome é nulo distinguiria os dois casos.
status: open

### DW-57: O usuário sintético "Migração do sistema legado" (semeado pela migration 000022, `papel='almoxarife'`, `ativo=false`) pode aparecer em superfícies de gestão de usuários — a listagem da Story 1.8, algu
origin: spec-deferred 58b0df4030fb
location: backend/migrations/000022_seed_usuario_migracao_legado.up.sql
source_spec: `spec-5-4-migracao-do-historico-de-movimentacoes-legado.md`
severity: low
reason: A migration cria a linha em `usuarios`; a mitigação atual é `ativo=false` + `senha_hash=NULL` (não loga). Nenhum reviewer demonstrou uma superfície concretamente quebrada, e o `JOIN usuarios` interno de `services.ListarMovimentacoes` (Story 5.3) precisa da linha para os registros migrados aparecerem no Histórico. Confirmar se as telas de gestão de usuários filtram `ativo=false` (ou se a presença é aceitável) e tratar na Story 8.x quando a exportação/anonimização LGPD existir.
status: open

### DW-58: Um item de lote que fica obsoleto entre a listagem e o clique de "Aplicar selecionadas" some silenciosamente da tabela sem mensagem explicando por quê.
origin: spec-deferred 7abb3432de8e
location: frontend/src/components/normalizacao/InconsistenciasSection.tsx
source_spec: `spec-6-2-aplicacao-seletiva-de-correcoes.md`
severity: low
reason: InconsistenciasSection.tsx só remove da tabela as linhas confirmadas em `aplicadas`; um item que não veio na resposta (campo preenchido por outra ação nesse meio-tempo) simplesmente permanece na tabela sem nenhum aviso ao usuário sobre por que aquela correção específica não foi aplicada.
status: open

### DW-59: `chaveIgnorada` compara valores por chave textual `%.3f` em vez de igualdade exata de float64, o que é uma escolha deliberada mas carrega um risco teórico de arredondamento em valores exatamente no li
origin: spec-deferred 28861dffadb6
location: backend/services/normalizacao.go (chaveIgnorada)
source_spec: `spec-6-2-aplicacao-seletiva-de-correcoes.md`
severity: low
reason: Um valor nascido de `strconv.ParseFloat` (Go) e o mesmo valor ida-e-volta por uma coluna `NUMERIC(10,3)` (Postgres) podem, em tese, formatar diferente em `%.3f` num caso de arredondamento de fronteira (ex. algo terminando em `.xxx5`), quebrando silenciosamente o match do "ignorar" para esse produto/campo específico. Documentado como trade-off aceito nas Design Notes da spec; nenhum caso real observado nos testes.
status: open

### DW-60: Ações de aplicar/ignorar bem-sucedidas não são anunciadas a leitores de tela — só o caminho de falha usa `role="alert"`.
origin: spec-deferred 1de02063b156
location: frontend/src/components/normalizacao/InconsistenciasSection.tsx
source_spec: `spec-6-2-aplicacao-seletiva-de-correcoes.md`
severity: low
reason: A tabela remove a linha e o rótulo do botão "Aplicar selecionadas (N)" atualiza visualmente em sucesso, mas nenhuma região `aria-live` cobre esse caminho; um usuário de leitor de tela não recebe nenhuma confirmação de que a ação funcionou, só silêncio.
status: open

### DW-61: `normalizacao_ignoradas.produto_id` referencia `produtos(id)` sem política `ON DELETE` definida, e a interação com a futura mesclagem de duplicatas (Story 6.3/6.4) não está documentada.
origin: spec-deferred 50c0e4d49eb5
location: backend/migrations/000023_create_normalizacao_ignoradas.up.sql
source_spec: `spec-6-2-aplicacao-seletiva-de-correcoes.md`
severity: low
reason: A FK vai bloquear a exclusão de qualquer Produto com sugestão ignorada associada (mesmo padrão de `importacao_linhas`/`produto_estoque`), mas nenhuma Design Note trata explicitamente como a mesclagem de duplicatas (que provavelmente remove/funde linhas de Produto) deve lidar com as linhas de `normalizacao_ignoradas` do produto removido.
status: open

### DW-62: A detecção de duplicatas (FR-19) não considera `categoria_id` — dois Produtos com nome normalizado igual, dimensões equivalentes e local em comum, mas em categorias diferentes, são agrupados como cand
origin: spec-deferred 61097cadc94a
location: backend/services/normalizacao.go (DetectarDuplicatas/dimensoesEquivalentes)
source_spec: `spec-6-3-deteccao-de-duplicatas.md`
severity: medium
reason: FR-19 do PRD e o Intent Contract de spec-6-3 definem o agrupamento explicitamente como nome normalizado + dimensões equivalentes + locais coincidentes — sem menção a categoria; `DetectarDuplicatas` (backend/services/normalizacao.go) segue essa definição à risca. Em teoria dois Produtos de categorias diferentes poderiam colidir nesses 3 critérios, mas a nomenclatura guiada por subtipo (Story 3.2) torna nomes idênticos entre categorias distintas pouco prováveis na prática.
status: open

### DW-63: `DuplicatasSection` não distingue visualmente/acessivelmente onde um grupo de duplicatas termina e o próximo começa quando `GET /api/normalizacao/duplicatas` devolve múltiplos grupos — cada grupo é só
origin: spec-deferred 39f7df537c01
location: frontend/src/components/normalizacao/DuplicatasSection.tsx
source_spec: `spec-6-3-deteccao-de-duplicatas.md`
severity: low
reason: frontend/src/components/normalizacao/DuplicatasSection.tsx renderiza `grupos.map(...)` como uma sequência de `<table>` com espaçamento visual (`gap-4`) mas nenhum landmark/heading por grupo — leitor de tela não tem como anunciar a fronteira entre grupos.
status: open

### DW-64: `DuplicatasSection` não anuncia a conclusão bem-sucedida da análise para leitor de tela — só o caminho de erro tem `role="alert"`; um usuário de leitor de tela que clica "Analisar duplicatas" não rece
origin: spec-deferred 4819e417116d
location: frontend/src/components/normalizacao/DuplicatasSection.tsx (e, na origem, InconsistenciasSection.tsx)
source_spec: `spec-6-3-deteccao-de-duplicatas.md`
severity: low
reason: frontend/src/components/normalizacao/DuplicatasSection.tsx só usa `role="alert"` no `<p>` de erro (linha ~120); não há `aria-live`/`role="status"` para o resultado de sucesso. Este é o mesmo padrão pré-existente de `InconsistenciasSection.tsx` (Story 6.1, também só `role="alert"` no erro, confirmado por grep) — `DuplicatasSection` é molde explícito desse componente (Code Map de spec-6-3), então herdou fielmente a lacuna em vez de introduzi-la.
status: open

### DW-65: GET /api/pedidos e a contagem de itens não têm índice/escopo de query dedicado: falta índice em pedidos.usuario_id e a subquery de contagem de ListarPedidosProprios agrega pedido_itens da empresa inte
origin: spec-deferred fd80220707b9
location: backend/services/pedidos.go (ListarPedidosProprios) / backend/migrations/000026_create_pedidos.up.sql
source_spec: `spec-7-3-consulta-de-pedidos-proprios.md`
severity: low
reason: backend/migrations/000026_create_pedidos.up.sql não cria índice em pedidos.usuario_id nem em pedido_itens.pedido_id; a subquery de contagem em ListarPedidosProprios (backend/services/pedidos.go) agrega pedido_itens inteiro antes do join com o p filtrado — inconsistente com o padrão já usado em movimentacoes (idx_movimentacoes_produto_id/idx_movimentacoes_criado_em, criados especificamente para essa forma de query).
status: open

### DW-66: O campo observacao (nota livre do solicitante no envio) é buscado e transportado ponta a ponta mas nunca é exibido em "Meus Pedidos" — nem na lista, nem no diálogo "Ver itens".
origin: spec-deferred ad7472caf1aa
location: frontend/src/pages/MeusPedidosPage.tsx
source_spec: `spec-7-3-consulta-de-pedidos-proprios.md`
severity: low
reason: MeusPedidosPage.tsx nunca lê pedido.observacao/detalhe.observacao em nenhum JSX; o Code Map/Tasks desta story enumeram explicitamente só solicitante, obra, data, badge, qtd de itens e "Ver itens" — não incluem a observação, então não é um requisito claro desta story, mas o dado já chega ao cliente sem uso.
status: open

### DW-67: A invariância de snapshot do AD-17 (rótulo do item não muda se o Produto for editado/mesclado depois) só tem teste de correção no momento da leitura, não de invariância ao longo do tempo.
origin: spec-deferred 0640432d43c1
location: backend/services/pedidos_test.go (BuscarPedidoProprio)
source_spec: `spec-7-3-consulta-de-pedidos-proprios.md`
severity: low
reason: TestBuscarPedidoProprio_DonoComItens e TestBuscarPedidoProprio_ItensOrdenadosPorNome (backend/services/pedidos_test.go) só provam correção no momento da leitura; a query em BuscarPedidoProprio de fato nunca faz join com produtos/estoques, então a implementação está correta hoje, mas nenhum teste edita o Produto/Estoque depois do envio e rebusca o Pedido para confirmar que o rótulo do item permanece congelado.
status: open

### DW-68: Follow-up review still recommended for 7-3-consulta-de-pedidos-próprios after the damping cap was spent
origin: review-budget-followup
location: n/a
source_spec: `spec-7-3-consulta-de-pedidos-proprios.md`
severity: low
reason: The follow-up-review damping cap (limits.max_followup_reviews = 1) was spent with the story finalized (status: done, verify green) while the review pass still recommended an independent follow-up. The work was committed by bmad-loop run 20260903-104700-a410; this entry preserves the lingering recommendation for a deliberate later review.
status: open
