# Epic 1 Context: Autenticação e Gestão de Acesso

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Qualquer pessoa cria a própria conta, entra com segurança (por senha ou pelo SSO corporativo Ferreira Costa via Keycloak), a organização controla quem acessa o quê por papel (`usuario < almoxarife < gestor < adm`), e contas administrativas ficam protegidas por segundo fator. Este épico fecha uma brecha de segurança crítica herdada do protótipo: hoje a autorização por papel só existe na interface — qualquer ação de escrita sensível, incluindo aprovar uma retirada que debita estoque, está exposta sem checagem no servidor. Autenticação, autorização, ciclo de vida de conta, MFA e auditoria de acesso entregues aqui são a fundação consumida por todos os épicos seguintes.

## Stories

- Story 1.1: Bootstrap do primeiro Adm e fundação do backend
- Story 1.2: Fundação do shell de navegação e design tokens
- Story 1.3: Autocadastro com verificação de e-mail
- Story 1.4: Login por e-mail e senha
- Story 1.5: Autorização por papel aplicada no servidor
- Story 1.6: Recuperação de senha por e-mail
- Story 1.7: Solicitação de promoção de papel
- Story 1.8: Gestão de contas — desativação e rebaixamento
- Story 1.9: Login federado via Keycloak — SSO Ferreira Costa
- Story 1.10: Bloqueio de conta e política de senha
- Story 1.11: MFA obrigatório para papéis administrativos
- Story 1.12: Log de acesso e auditoria

## Requirements & Constraints

- Toda requisição autenticada exige token válido, exceto login e cadastro; autorização por papel é sempre validada no servidor (hierarquia estrita, não lista de pares).
- Autocadastro nunca aceita papel vindo do formulário (sempre `usuario`); e-mail único atomicamente; confirmação de e-mail obrigatória antes do primeiro login.
- Sessão expira por inatividade em 2h; login e SSO emitem o mesmo formato de sessão.
- SSO nunca cria conta nova (busca por e-mail existente, case-insensitive) nem define papel — só troca *quem autentica*; exige e-mail verificado no token; é opção adicional, nunca redirecionamento automático.
- Rebaixamento/desativação derruba acesso já na próxima requisição. `gestor` age só sobre `almoxarife`/`usuario`; `adm` age sobre qualquer conta.
- Recuperação de senha e erros de login/cadastro nunca revelam se um e-mail existe. Conta só-SSO pode criar senha própria pela primeira vez por esse fluxo.
- Bloqueio após 5 tentativas malsucedidas (15min, sem revelar tempo restante); senha mínima 8 caracteres com letra e número; nada disso afeta o caminho SSO.
- MFA (TOTP) obrigatório para `gestor`/`adm` por senha antes de liberar ações restritas ao papel; opcional para `usuario`/`almoxarife`; nunca aparece no caminho SSO (o realm já impõe MFA a esses papéis).
- Log de acesso é append-only, cobre toda tentativa (sucesso/falha, senha ou SSO), só `adm` consulta, sem edição/exclusão na interface.
- Critérios de sucesso relevantes: 100% das ações sensíveis recusadas no servidor para papel sem permissão (testável); 100% das tentativas de força bruta bloqueadas; migração não pode deixar cadastro/movimentação perceptivelmente mais lentos que o protótipo atual.

## Technical Decisions

- Papel do usuário é sempre lido do Postgres a cada requisição, nunca cacheado — garante efeito imediato de rebaixamento/desativação.
- Sessão: JWT de acesso curto (30min, `golang-jwt/jwt/v5`) + refresh token rotativo em cookie `HttpOnly` (TTL 2h, rotaciona a cada uso) — mesmo formato para login por senha e troca SSO.
- Autorização: hierarquia como ordem total (`adm=4 > gestor=3 > almoxarife=2 > usuario=1`). Decisão allow/deny sempre em middleware; filtro de escopo em listagem sempre em service, consumindo o papel já resolvido pelo contexto — nunca re-derivando.
- Keycloak isolado em pacote `iam/`: JWKS cache em memória (TTL 1h), validação RS256 via `kid`, `iss`=URL do realm, `azp` (não `aud`) contra allowlist, endpoint de troca de token, config runtime (não build-time), client id próprio no realm `ferreiracosta`, RP-initiated logout.
- Bootstrap do primeiro Adm só via CLI (`cmd/seed-admin`), nunca endpoint HTTP; senha hasheada com bcrypt.
- E-mail transacional sempre assíncrono via outbox (`emails_pendentes`, `tipo` enum) + worker por polling — nunca SMTP síncrono no handler HTTP.
- `TOKENS_ACAO` tipado por `tipo` (enum) e de uso único; validação sempre filtra por token+usuario_id+tipo+não expirado+não usado.
- Convenções: UUID v4, `timestamptz` UTC, e-mail normalizado para minúsculas com índice único, envelope de erro `{"error":{"code","message"}}` com vocabulário fixo para autenticação/sessão, logging via `log/slog`.
- Em aberto: biblioteca TOTP não fixada (`pquerna/otp` candidata); mecanismo de contador/lockout de força bruta sem decisão de arquitetura dedicada.

## UX & Interaction Patterns

- Login: e-mail/senha é o caminho padrão visível; "Entrar com Ferreira Costa" é opção adicional, nunca redirecionamento automático.
- Navegação é gated por papel: item sem permissão simplesmente não aparece — nunca tela de "acesso negado".
- MFA obrigatório redireciona para Configurações → Segurança antes de liberar ação restrita, bloqueando a navegação normal até configurar.
- Conta bloqueada por tentativas: mensagem não revela tempo restante; "Esqueci minha senha" continua visível.
- Ação destrutiva/irreversível usa `AlertDialog` via `ConfirmDialog` reutilizável — nunca `window.confirm()`; toda confirmação assíncrona usa toast (`sonner`, `aria-live="polite"`).
- Fundação de design (Story 1.2): paleta `primary`/`accent`/`destructive`/`warning`/`info` + `text-on-tint-*` para contraste AA; Inter geral, JetBrains Mono para código/UUID; espaçamentos rail 56px, bottom nav 56px, FAB 56px, submenu 224px, toque mínimo 48px. Shell ≥768px com rail+abas, <768px (a partir de 360px) vira bottom nav com itens admin atrás de "Mais".
- Log de Acesso: tabela filtrável por período, só leitura, visível só a `adm`.

## Cross-Story Dependencies

- Story 1.2 (shell, tokens, `ConfirmDialog`, `Toaster`) é fundação de UI consumida por todas as demais stories do épico e pelos épicos seguintes.
- Story 1.5 (autorização em middleware) é pré-requisito para qualquer endpoint restrito por papel em qualquer épico posterior.
- Story 1.9 (SSO) depende de conta já existente via Story 1.3 — SSO nunca cria conta.
- Story 1.11 (MFA) depende do login por senha (1.4); o caminho SSO (1.9) é isento pois o realm já impõe MFA.
- Story 1.10 (bloqueio/senha) e Story 1.6 (recuperação) permanecem independentes entre si — bloqueio não afeta redefinição.
- Story 1.7 (promoção) depende da checagem de autorização da Story 1.5.
- Epic 2 (Story 2.2) e Epic 7 (Story 7.2) reutilizam o framework de autorização deste épico, mas suas dependências de dados só existem a partir do Epic 3/7 — sem bloqueio sobre este épico.
