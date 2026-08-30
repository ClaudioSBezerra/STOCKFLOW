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
