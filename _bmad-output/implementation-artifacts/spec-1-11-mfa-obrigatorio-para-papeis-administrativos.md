---
title: 'Story 1.11 — MFA obrigatório para papéis administrativos'
type: 'feature'
created: '2026-08-30'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '07bbb5e458598dea31149ca41a1cc3916d1c69d1'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      Não há fluxo de recuperação/desativação de MFA para uma conta que perdeu
      o dispositivo autenticador — só uma edição manual no banco
      (`mfa_habilitado=false`) desbloqueia, o que é especialmente sensível
      porque MFA agora é obrigatório para `gestor`/`adm` operarem.
    evidence: |-
      `<intent-contract>` (Never) exclui deliberadamente qualquer opção de
      desativar/reconfigurar um MFA já habilitado ou qualquer backup/recovery
      code — fora das 4 ACs do FR-37. Combinado com a obrigatoriedade de MFA
      para ações restritas, um dispositivo perdido bloqueia permanentemente as
      ações administrativas daquela conta até uma intervenção manual no banco.
      Recomenda-se uma story futura dedicada de reset/recuperação de MFA
      (provavelmente com um fluxo assistido por `adm`, análogo ao
      desativação/rebaixamento da Story 1.8).
    location: >-
      backend/services/auth.go (ConfirmarConfiguracaoMFA) / frontend/src/pages/ConfiguracoesPage.tsx
    severity: medium
---

<intent-contract>

## Intent

**Problem:** FR-37/SM-2 exigem um segundo fator (TOTP) para contas `gestor`/`adm` autenticadas por senha antes de liberar qualquer ação restrita ao papel, com o realm Keycloak corporativo já cobrindo o caminho SSO — hoje `services.Login`/`EmitirSessao` não têm noção alguma de MFA nem de "por qual caminho esta sessão entrou", então nada distingue uma sessão SSO de uma por senha depois do login.

**Approach:** Duas colunas em `usuarios` (`mfa_habilitado`, `mfa_secret`) guardam o estado de MFA por conta; uma nova coluna `sessoes.origem` (`'senha'|'sso'`) e um novo claim `origem` no access JWT (mintado por `gerarAccessToken`/propagado por `RenovarSessao`) marcam a proveniência de CADA sessão — é a única forma de o `middleware.RequireRole` saber, a cada requisição, se deve ou não exigir MFA daquele usuário. `RequireRole` passa a recusar (`403 MFA_SETUP_REQUIRED`) uma sessão `origem=senha` de papel ≥ `gestor` sem `mfa_habilitado`. `LoginHandler`, após senha válida, verifica `mfa_habilitado`: se true, não emite sessão — devolve um token opaco de uso único (reaproveita `tokens_acao`, novo `tipo='mfa_login'`) que `POST /api/auth/mfa/verificar` troca por sessão mediante um código TOTP válido. Enrollment (`POST /api/auth/mfa/iniciar` + `/confirmar`) vive em Configurações → Segurança. TOTP (RFC 6238/4226) é implementado com `crypto/hmac`+`encoding/base32` da stdlib — sem nova dependência Go (a spine deixou a biblioteca em aberto e marcou manutenção do candidato `pquerna/otp` como não confirmada).

## Boundaries & Constraints

**Always:**
- **Gate único no servidor é `middleware.RequireRole`** (roles.go:35-58): quando `rankMinimo >= services.RankPapel(services.PapelGestor)` E `usuario.Origem == "senha"` E `!usuario.MFAHabilitado`, responde `403 MFA_SETUP_REQUIRED` ANTES de checar rank (papel insuficiente continua vencendo — checar MFA só depois de já confirmar `RankPapel(usuario.Papel) >= rankMinimo`, para não vazar "esta rota existe e é restrita" a quem nem tem o papel). Cobre hoje `GET /api/usuarios`, `GET /api/promocoes`, `POST /api/promocoes/{id}/decisao`, `POST /api/usuarios/{id}/desativacao`, `POST /api/usuarios/{id}/rebaixamento` (main.go:212-229) e qualquer rota `RequireRole(gestor+)` futura, de graça.
- **`origem` de sessão é claim do JWT, não coluna de `usuarios`**: `UsuarioSessao` (auth.go:303-309) ganha `Origem string`, mas ele é preenchido pelo `middleware.RequireAuth` a partir do claim do token (`claims.Origem`), NUNCA por `BuscarUsuarioSessao`/Postgres — é metadado de UMA sessão, não estado de conta, e por isso não fere o invariante de auth.go:1-8/middleware/auth.go:1-7 ("claim só carrega sub, nunca papel/ativo/nome/email"): aquele invariante é sobre não reconfiar em ESTADO DE CONTA carimbado no token; `origem` não é estado de conta, é o único lugar onde essa informação pode morrer sem existir.
- `gerarAccessToken(jwtSecret, usuarioID, origem)` grava `origem` num claim custom (struct embutindo `jwt.RegisteredClaims`); `EmitirSessao(db, jwtSecret, usuarioID, origem)` grava a mesma string em `sessoes.origem`; `RenovarSessao` LÊ `origem` da linha de `sessoes` que está rotacionando e a repassa ao novo JWT + à nova linha — uma sessão SSO nunca vira "senha" (nem vice-versa) por refresh.
- `handlers.LoginHandler` (auth.go:231) chama `EmitirSessao(..., "senha")`; `handlers.KeycloakSSOHandler` (auth_sso.go:89) chama `EmitirSessao(..., "sso")`.
- **Enforcement de login (2º fator) só quando `mfa_habilitado=true`**: `LoginHandler`, logo após `services.Login` retornar sucesso, chama `services.BuscarUsuarioSessao` (já preciso para montar a resposta hoje) ANTES de `EmitirSessao`. Se `usuario.MFAHabilitado`: gera token opaco (`gerarTokenAcao`, reaproveitado) em `tokens_acao(tipo='mfa_login', expira_em=now()+5min)`, responde `200 {"mfaRequerido":true,"mfaToken":"..."}`, NÃO chama `EmitirSessao`. Se não: segue para `EmitirSessao` usando o `usuario` já carregado (elimina a segunda consulta que hoje roda depois do `EmitirSessao`, auth.go:238).
- `POST /api/auth/mfa/verificar {mfaToken, codigo}` (público, sem `RequireAuth` — ainda não há sessão): resolve o token por `token+tipo='mfa_login'` (mesmos sentinelas `ErrTokenNaoEncontrado`/`ErrTokenExpirado` de `ValidarTokenRedefinicao`); código errado -> `registrarFalhaLogin` (mesmo contador/bloqueio da Story 1.10 — reaproveitado, não uma trilha nova) + `401 MFA_CODIGO_INVALIDO`, token NÃO é consumido (permite nova tentativa até expirar ou até a conta bloquear); `bloqueado_ate` no futuro nesse meio-tempo -> `429 ACCOUNT_LOCKED`, mesmo vocabulário da Story 1.10; código certo -> marca `usado_em` (mesmo padrão de corrida `usado_em IS NULL AND expira_em > now()` de `RedefinirSenha`), zera contador se sujo, e emite sessão (`EmitirSessao(...,"senha")`) — resposta idêntica à do login sem MFA.
- **Enrollment exige sessão já autenticada** (`RequireAuth`, sem `RequireRole` — `usuario`/`almoxarife` também podem configurar, opcionalmente): `POST /api/auth/mfa/iniciar` gera segredo (`GerarSegredoTOTP`, 20 bytes/base32) e URL `otpauth://` (`URLProvisionamentoTOTP`) SEM gravar nada ainda; `POST /api/auth/mfa/confirmar {segredo, codigo}` valida `codigo` contra `segredo` (`ValidarCodigoTOTP`) e só então grava `UPDATE usuarios SET mfa_secret=$1, mfa_habilitado=true WHERE id=$2 AND mfa_habilitado=false` (guarda de corrida: `RowsAffected==0` -> já configurado). Ambos `409 MFA_JA_CONFIGURADO` se `mfa_habilitado` já é true.
- TOTP: HMAC-SHA1, 6 dígitos, passo de 30s, janela de tolerância ±1 passo (RFC 6238 §5.2), comparação do código com `subtle.ConstantTimeCompare`. `mfa_secret` fica em texto puro (mesma decisão/justificativa já registrada para `sessoes.refresh_token`/`tokens_acao.token`: nenhuma AD exige hash/cifra e a proteção é a mesma superfície de banco).
- Frontend espelha o gate: `UsuarioSessao` (lib/auth.tsx:31) ganha `mfaHabilitado: boolean` e `origem: string`; `RotaProtegida` (App.tsx) redireciona para `/configuracoes` (replace) sempre que `origem==='senha' && rankPapel(papel)>=rankPapel('gestor') && !mfaHabilitado` e a rota atual não é `/configuracoes` — bloqueia navegação sem esconder itens do rail (mesmo texto do UX-DR22: "bloqueando a navegação normal até configurar", não "escondendo").
- Nova dependência frontend `qrcode.react` (exporta `QRCodeSVG`) — única lib nova do story, para renderizar o QR a partir da `otpauthUrl` devolvida por `/mfa/iniciar`; nenhuma lib nova no backend.

**Block If:** nenhuma decisão desta story depende de aprovação humana nem de ação de operador fora do repositório — biblioteca TOTP e mecanismo de origem de sessão são decisões técnicas resolvidas nesta spec (a spine já autorizava resolver a biblioteca "no momento da story"). Status final esperado: `done`.

**Never:**
- Nenhuma opção de desativar/reconfigurar um MFA já `habilitado`, nenhum backup/recovery code — fora das 4 ACs do FR-37; perda de dispositivo autenticador é limitação aceita, registrada em `deferred`.
- Caminho SSO intocado além da UMA linha que passa `"sso"` para `EmitirSessao`: `KeycloakSSOHandler`, `iam/`, `BuscarUsuarioPorEmailSSO` (exceto a coluna nova no SELECT) permanecem como estão. Sessão SSO nunca é bloqueada pelo gate de MFA nem vê a tela de configuração forçada.
- Nenhuma tabela nova — reaproveita `usuarios` (2 colunas), `sessoes` (1 coluna) e `tokens_acao` (novo valor de `tipo`, mesmo padrão da Story 1.6).
- Nenhuma mudança em `RefreshTokenExpiracao`/`accessTokenExpiracao`, no formato do cookie de refresh, ou em `RenovarSessao` além de propagar `origem`.
- Sessões/JWTs emitidos ANTES desta migration não têm claim `origem` (string vazia ao decodificar) — tratadas como "não senha" pelo gate (fail-open só para o gate de MFA, nunca para autenticidade) até expirarem naturalmente (≤30min de access token); não vale a pena migrar/invalidar sessões em voo por isso.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Login por senha, MFA não configurado | `POST /login`, senha correta, `mfa_habilitado=false` | `200` com `token`+`usuario` (sessão `origem=senha`), igual hoje | — |
| Login por senha, MFA configurado | `POST /login`, senha correta, `mfa_habilitado=true` | `200 {"mfaRequerido":true,"mfaToken":"..."}`, NENHUMA sessão emitida | — |
| Código TOTP correto | `POST /mfa/verificar` com `mfaToken` válido + código correto | `200` com `token`+`usuario`, token de `mfa_login` marcado usado | — |
| Código TOTP errado | idem, código incorreto | contador de falhas incrementa (Story 1.10); token `mfa_login` continua válido | `401 MFA_CODIGO_INVALIDO` |
| 5ª falha de código TOTP | 5 códigos errados seguidos para a mesma conta | conta bloqueada por 15min, igual bloqueio de senha | `429 ACCOUNT_LOCKED` |
| `mfaToken` expirado/reusado | `POST /mfa/verificar` com token de 6+min ou já consumido | nenhuma sessão emitida | `401 MFA_TOKEN_INVALIDO` |
| `gestor`/`adm` sem MFA acessa rota restrita | sessão `origem=senha`, `mfa_habilitado=false`, `GET /api/usuarios` | requisição recusada, handler nunca executa | `403 MFA_SETUP_REQUIRED` |
| `gestor` via SSO acessa rota restrita | sessão `origem=sso`, `mfa_habilitado=false`, `GET /api/usuarios` | `200` normal — gate nunca dispara para `origem=sso` | — |
| `usuario`/`almoxarife` sem MFA | qualquer rota sem `RequireRole(gestor+)` | acesso normal — gate nunca se aplica abaixo de `gestor` | — |
| Enrollment: QR escaneado + código válido | `POST /mfa/iniciar` -> `POST /mfa/confirmar` com código certo | `mfa_habilitado=true`, `mfa_secret` gravado | — |
| Enrollment: código de confirmação errado | `POST /mfa/confirmar` com código incorreto | nenhuma coluna gravada | `400 MFA_CODIGO_INVALIDO` |
| Enrollment duplicado | `POST /mfa/iniciar` ou `/confirmar` com `mfa_habilitado` já true | nenhuma mudança | `409 MFA_JA_CONFIGURADO` |
| Sessão emitida pré-migration em uso | JWT sem claim `origem` | gate de MFA não dispara para essa sessão específica até expirar (≤30min) | — |

</intent-contract>

## Code Map

- `backend/migrations/000006_add_mfa_to_usuarios.{up,down}.sql` (novo) — `usuarios ADD mfa_habilitado BOOLEAN NOT NULL DEFAULT false, ADD mfa_secret TEXT`; `sessoes ADD origem VARCHAR(10) NOT NULL DEFAULT 'senha' CHECK (origem IN ('senha','sso'))`; `ALTER TABLE tokens_acao DROP CONSTRAINT tokens_acao_tipo_check, ADD CONSTRAINT tokens_acao_tipo_check CHECK (tipo IN ('verificacao_email','redefinicao_senha','mfa_login'))`. Tudo aditivo; down reverte as 3 mudanças.
- `backend/services/totp.go` (novo) — `GerarSegredoTOTP() (string, error)` (20 bytes `crypto/rand`, `base32.StdEncoding.WithPadding(base32.NoPadding)`); `URLProvisionamentoTOTP(email, segredo string) string` (`otpauth://totp/StockFlow:<email>?secret=<segredo>&issuer=StockFlow&algorithm=SHA1&digits=6&period=30`); `ValidarCodigoTOTP(segredo, codigo string) bool` (HOTP RFC4226 sobre `crypto/hmac`+`crypto/sha1`, contador = `time.Now().Unix()/30`, testa `{-1,0,+1}`, compara com `subtle.ConstantTimeCompare`). Zero dependências novas.
- `backend/services/totp_test.go` (novo) — vetores de teste RFC 6238 conhecidos (segredo `"12345678901234567890"` em ASCII/base32, códigos esperados em timestamps fixos) + rejeição de código fora da janela + `GerarSegredoTOTP` produz segredos de tamanho/alfabeto corretos.
- `backend/services/auth.go` — `UsuarioSessao` (auth.go:303-309) ganha `MFAHabilitado bool` e `Origem string` (este último NUNCA preenchido por `BuscarUsuarioSessao`, só documentado ali). `BuscarUsuarioSessao` (auth.go:769-783) e `BuscarUsuarioPorEmailSSO` (auth_sso.go:19-33): SELECT ganha `mfa_habilitado`. `gerarAccessToken` (auth.go:452-464) ganha parâmetro `origem string`, claim custom (`type acessoClaims struct { jwt.RegisteredClaims; Origem string \`json:"origem"\` }`). `EmitirSessao` (auth.go:471-491) ganha parâmetro `origem string`, propagado ao claim e ao novo `INSERT INTO sessoes (..., origem)`. `RenovarSessao` (auth.go:503-551): `marcarRevogada` ganha `origem` no `RETURNING`, repassado a `gerarAccessToken` e ao novo `INSERT`. Novas funções: `IniciarLoginMFA(db, usuarioID) (token string, err error)` (gera + insere `tokens_acao` tipo `mfa_login`, `expira_em=now()+mfaLoginTokenExpiracao`); `ConcluirLoginMFA(db, mfaToken, codigo string) (usuarioID string, err error)` (resolve token, checa `bloqueado_ate`, valida código, marca usado, zera contador — molde de `RedefinirSenha`); `IniciarConfiguracaoMFA(email string) (segredo, otpauthURL string, err error)`; `ConfirmarConfiguracaoMFA(db, usuarioID, segredo, codigo string) error`. Novos erros: `ErrMFACodigoInvalido`, `ErrMFAJaConfigurado`. Nova constante `mfaLoginTokenExpiracao = 5 * time.Minute`.
- `backend/services/auth_test.go` / `auth_sso_test.go` — todo call site de `EmitirSessao(db, jwtSecret, id)` (services/auth_test.go: linhas 604,655,729,750,772,1137,1142; auth_sso_test.go: 71,101) ganha o 4º argumento (`"senha"` ou `"sso"` conforme o teste). Novos testes: fluxo completo de `IniciarLoginMFA`/`ConcluirLoginMFA` cobrindo a I/O Matrix (código certo, errado, 5ª falha bloqueia, token expirado/reusado); `ConfirmarConfiguracaoMFA` feliz + código errado + já configurado; `RenovarSessao` preserva `origem` através da rotação.
- `backend/middleware/auth.go` — `RequireAuth` (auth.go:70-80) parseia `services.AcessoClaims` (ou struct equivalente exportado por `services`) em vez de `jwt.RegisteredClaims{}` puro, e faz `usuario.Origem = claims.Origem` logo após `BuscarUsuarioSessao` (auth.go:82), antes de gravar no contexto.
- `backend/middleware/roles.go` — `RequireRole` (roles.go:41-57): após `RankPapel(usuario.Papel) >= rankMinimo` passar, novo `if rankMinimo >= services.RankPapel(services.PapelGestor) && usuario.Origem == "senha" && !usuario.MFAHabilitado { escreverErro(w, http.StatusForbidden, "MFA_SETUP_REQUIRED", "configure a autenticação em duas etapas em Configurações → Segurança para continuar."); return }`.
- `backend/middleware/roles_test.go` / `auth_test.go` — novos casos: sessão `origem=senha`+`mfa_habilitado=false`+papel `gestor` -> `403 MFA_SETUP_REQUIRED` numa rota `RequireRole(gestor)`; mesma sessão com `origem=sso` -> passa; `mfa_habilitado=true` -> passa; papel insuficiente ainda vence `403 FORBIDDEN` antes do gate de MFA (ordem preservada).
- `backend/handlers/auth.go` — `usuarioResposta` (auth.go:136-141) ganha `MfaHabilitado bool \`json:"mfaHabilitado"\`` e `Origem string \`json:"origem"\``. `LoginHandler` (auth.go:202-256): reordena para chamar `BuscarUsuarioSessao` logo após `services.Login` ter sucesso (antes de `EmitirSessao`); branch `usuario.MFAHabilitado` -> `IniciarLoginMFA` + resposta `{"mfaRequerido":true,"mfaToken":...}`; branch contrário -> extrai a emissão de sessão + resposta para uma função privada `emitirSessaoEResponder(w, r, db, jwtSecret, usuario, origem)` reaproveitada pelo novo `MFAVerificarHandler`. `MeHandler` (auth.go:401-416): inclui `MfaHabilitado`/`Origem` na resposta.
- `backend/handlers/auth_mfa.go` (novo) — `MFAVerificarHandler(db, jwtSecret)` (`POST /api/auth/mfa/verificar`, público, `authRequestMaxBytes`): mapeia `ErrTokenNaoEncontrado`/`ErrTokenExpirado` -> `401 MFA_TOKEN_INVALIDO`, `ErrCredenciaisInvalidas`-equivalente (código errado) -> `401 MFA_CODIGO_INVALIDO`, `ErrContaBloqueada` -> `429 ACCOUNT_LOCKED`, sucesso -> `emitirSessaoEResponder(...,"senha")`. `MFAIniciarHandler(db)` (`POST /api/auth/mfa/iniciar`, atrás de `RequireAuth`): lê `UsuarioDaSessao`, chama `IniciarConfiguracaoMFA`, `409 MFA_JA_CONFIGURADO` se `usuario.MFAHabilitado`, senão `200 {"segredo":...,"otpauthUrl":...}`. `MFAConfirmarHandler(db)` (`POST /api/auth/mfa/confirmar`, atrás de `RequireAuth`): `ConfirmarConfiguracaoMFA`, `400 MFA_CODIGO_INVALIDO` / `409 MFA_JA_CONFIGURADO` / `200 {}`.
- `backend/handlers/auth_mfa_test.go` (novo) — cobre a I/O Matrix completa dos 3 endpoints novos, reaproveitando `criarUsuarioLoginComEstado`/`postLogin` (auth_test.go:359,380) como base.
- `backend/main.go` — 3 novas rotas: `POST /api/auth/mfa/verificar` (pública), `POST /api/auth/mfa/iniciar` e `POST /api/auth/mfa/confirmar` (`RequireAuth`, sem `RequireRole`), ao lado do bloco de rotas de auth existente (main.go:211).
- `frontend/package.json` — nova dependência `qrcode.react` (exporta `QRCodeSVG`).
- `frontend/src/lib/auth.tsx` — `UsuarioSessao` (auth.tsx:31-36) ganha `mfaHabilitado: boolean` e `origem: string`; `AuthContextValue` ganha `atualizarUsuario: (usuario: UsuarioSessao) => void` (auth.tsx:40-60), implementado como `useCallback((u) => setUsuario(u), [])`, exposto no `value` (auth.tsx:193-196) — usado pela tela de Segurança para refletir `mfaHabilitado:true` sem round-trip extra após confirmar.
- `frontend/src/App.tsx` — `RotaProtegida` (App.tsx:41-57) ganha `useLocation()` + `rankPapel` (de `@/components/shell/nav-items`, já usado em ConfiguracoesPage.tsx:6,66); quando `estado==='autenticado'` e `usuario` indica MFA pendente (`origem==='senha' && rankPapel(papel)>=rankPapel('gestor') && !mfaHabilitado`) e `location.pathname !== '/configuracoes'`, `<Navigate to="/configuracoes" replace/>` em vez de `<AppShell/>`.
- `frontend/src/pages/LoginPage.tsx` — `LoginResposta` (LoginPage.tsx:44-47) vira união com `{mfaRequerido:true; mfaToken:string}`; `mensagemDeErro` (linhas 21-42) ganha `MFA_CODIGO_INVALIDO`/`MFA_TOKEN_INVALIDO`; novo estado local `etapa: 'senha'|'codigo'` + `mfaToken`; `handleSubmit` (87-119) na etapa `'senha'` detecta `body.mfaRequerido` e troca para `'codigo'` em vez de `definirSessao`; novo formulário de código (input numérico 6 dígitos) que faz `POST /api/auth/mfa/verificar` e, no sucesso, `definirSessao`+`navigate('/')` como hoje; erro de token inválido/expirado volta a `etapa='senha'`.
- `frontend/src/pages/LoginPage.test.tsx` — novos casos: `mfaRequerido:true` troca de tela sem chamar `definirSessao`; código válido conclui login; código inválido mostra erro e mantém a etapa de código; token expirado volta para a etapa de senha.
- `frontend/src/pages/ConfiguracoesPage.tsx` — nova seção "Segurança" (molde das `Card` existentes, ConfiguracoesPage.tsx:186-249), visível a todos: mensagem "obrigatório para o seu papel" quando `rankPapel(papel)>=rankPapel('gestor') && origem==='senha' && !mfaHabilitado`, "opcional" caso contrário quando `!mfaHabilitado`, "ativo" quando `mfaHabilitado`. Fluxo de configuração: botão -> `POST /mfa/iniciar` -> `QRCodeSVG value={otpauthUrl}` + `segredo` em `font-mono` (token JetBrains Mono da Story 1.2) + input de código -> `POST /mfa/confirmar` -> sucesso chama `atualizarUsuario({...usuario, mfaHabilitado:true})` + toast (`sonner`, molde já usado nas demais ações desta página).
- `frontend/src/pages/ConfiguracoesPage.test.tsx` — novos casos cobrindo as 3 mensagens de estado (obrigatório/opcional/ativo) e o fluxo de confirmação feliz + código errado.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000006_add_mfa_to_usuarios.{up,down}.sql` — colunas de MFA em `usuarios`, `origem` em `sessoes`, novo `tipo` em `tokens_acao`.
- `backend/services/totp.go` + `totp_test.go` — implementação TOTP RFC 6238 sem dependência externa.
- `backend/services/auth.go` (+ `auth_sso.go`) — `origem` de ponta a ponta (`UsuarioSessao`, `gerarAccessToken`, `EmitirSessao`, `RenovarSessao`, ambos os `BuscarUsuario*`); `IniciarLoginMFA`/`ConcluirLoginMFA`/`IniciarConfiguracaoMFA`/`ConfirmarConfiguracaoMFA`.
- `backend/services/auth_test.go` + `auth_sso_test.go` — ajuste dos call sites de `EmitirSessao` + cobertura nova da Matrix.
- `backend/middleware/auth.go` + `roles.go` (+ testes) — claim `origem` no contexto; gate `403 MFA_SETUP_REQUIRED` em `RequireRole`.
- `backend/handlers/auth.go` + `auth_mfa.go` (+ testes) — `LoginHandler` com 2º fator, `MeHandler` expõe `mfaHabilitado`/`origem`, 3 handlers novos de MFA.
- `backend/main.go` — registra as 3 rotas novas.
- `frontend/src/lib/auth.tsx` — `origem`/`mfaHabilitado` em `UsuarioSessao`, `atualizarUsuario`.
- `frontend/src/App.tsx` — gate de navegação para `/configuracoes`.
- `frontend/src/pages/LoginPage.tsx` (+ teste) — 2º passo de código TOTP no login.
- `frontend/src/pages/ConfiguracoesPage.tsx` (+ teste) — seção Segurança com QR + confirmação.
- `frontend/package.json` — dependência `qrcode.react`.

**Acceptance Criteria:**
- Given uma conta `gestor`/`adm` autenticada por senha (`origem=senha`) sem MFA configurado, when ela tenta acessar qualquer rota `RequireRole(gestor+)`, then a API responde `403 MFA_SETUP_REQUIRED` e a navegação do frontend é redirecionada para Configurações → Segurança antes de liberar a ação.
- Given a tela de configuração de MFA, when o usuário escaneia o QR Code (ou digita o segredo manualmente) e confirma um código TOTP válido, then `usuarios.mfa_habilitado` vira `true` e logins futuros por senha dessa conta passam a exigir o código na segunda etapa.
- Given uma conta `usuario`/`almoxarife`, when ela acessa Configurações → Segurança, then a configuração de MFA aparece como opcional (nenhuma rota sua é bloqueada por falta de MFA) e nunca é forçada.
- Given um login via SSO (`origem=sso`) para conta `gestor`/`adm`, when o usuário autentica pelo Keycloak, then a tela de MFA do stockflow nunca aparece nem no login nem como bloqueio de navegação — o `403 MFA_SETUP_REQUIRED` nunca dispara para essa sessão.
- Given uma conta com `mfa_habilitado=true`, when o login por senha valida as credenciais, then nenhuma sessão é emitida até um código TOTP correto ser enviado a `POST /api/auth/mfa/verificar`; um código errado nunca cria sessão e conta como tentativa para o bloqueio por força bruta da Story 1.10.

## Review Triage Log

### 2026-08-30 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 10: (high 1, medium 3, low 6)
- defer: 0
- reject: 4: (high 0, medium 0, low 4)
- addressed_findings:
  - `[high]` `[patch]` `POST /api/auth/mfa/confirmar` não exigia nenhuma reautenticação além do access token válido (`RequireAuth`) — um access token roubado (válido até 30min) bastava para um atacante habilitar MFA com o PRÓPRIO autenticador na conta da vítima, sequestrando os logins futuros dela mesmo depois do token expirar. Adicionado campo `senhaAtual` a `POST /mfa/confirmar`, verificado (bcrypt) contra `usuarios.senha_hash` antes de gravar `mfa_secret`/`mfa_habilitado`; formulário de confirmação no frontend ganhou o campo correspondente.
  - `[medium]` `[patch]` A migration `000006` backfilava `sessoes.origem` existente com `DEFAULT 'senha'` — uma sessão SSO viva no momento do deploy seria mal-rotulada e, ao rotacionar via `RenovarSessao`, propagaria `origem=senha` indefinidamente (violando o invariante "SSO nunca é gated" além da janela transitória de 30min já aceita para o claim do JWT). Adicionado `UPDATE sessoes SET revogado_em = now() WHERE revogado_em IS NULL` ao final do `up.sql` da migration `000006`: toda sessão em voo é revogada no deploy, forçando um novo login (por senha ou SSO) que já nasce com `origem` correta.
  - `[medium]` `[patch]` O redirecionamento de `RotaProtegida` para `/configuracoes` (metade frontend da AC1) não tinha nenhum teste cobrindo o caso `papel=gestor`, `origem=senha`, `mfaHabilitado=false`. Adicionados casos a `App.test.tsx`: sessão nessas condições em rota != `/configuracoes` é redirecionada; a mesma sessão já em `/configuracoes` não é redirecionada.
  - `[medium]` `[patch]` Nenhum teste observava `origem` de ponta a ponta no login via SSO (`handlers.KeycloakSSOHandler`) — uma regressão pontual (ex.: trocar `"sso"` por `"senha"` no call site) quebraria o invariante de isenção de MFA para SSO sem que nenhum teste falhasse. Adicionado um caso em `auth_sso_test.go` que decodifica `origem` da resposta de um login SSO de uma conta `gestor` e afirma `"sso"`.
  - `[low]` `[patch]` O down-migration de `000006` restaura o `CHECK` original de `tokens_acao.tipo` (sem `'mfa_login'`) sem antes remover linhas desse tipo — falharia se qualquer linha `mfa_login` existisse (o que ocorre quase imediatamente após o up-migration ser usado). Adicionado `DELETE FROM tokens_acao WHERE tipo = 'mfa_login'` antes de restaurar o `CHECK` no `down.sql`.
  - `[low]` `[patch]` `ValidarCodigoTOTP` não impedia o reuso do mesmo código dentro da mesma janela de validade (~90s) — um código interceptado por quem já conhece a senha poderia autenticar duas vezes na mesma janela. Adicionada coluna `usuarios.mfa_ultimo_passo_usado BIGINT`, atualizada atomicamente (`UPDATE ... WHERE mfa_ultimo_passo_usado IS DISTINCT FROM $1 OR mfa_ultimo_passo_usado IS NULL`) em `ConcluirLoginMFA`/`ConfirmarConfiguracaoMFA` no sucesso; um passo já usado é rejeitado mesmo com o código matematicamente correto.
  - `[low]` `[patch]` `IniciarLoginMFA` inseria um novo token `mfa_login` a cada tentativa de login sem invalidar tokens anteriores ainda válidos da mesma conta — inconsistente com o precedente já usado por `SolicitarRedefinicaoSenha` (Story 1.6), que invalida tokens anteriores do mesmo tipo. Adicionado `UPDATE tokens_acao SET usado_em = now() WHERE usuario_id = $1 AND tipo = 'mfa_login' AND usado_em IS NULL` antes de inserir o novo token.
  - `[low]` `[patch]` Nenhum registro (`slog`) marcava um enrollment de MFA concluído ou um login concluído via segundo fator — sem sinal positivo em log para investigação futura ("quando esta conta passou a exigir/usar MFA"). Adicionado `slog.Info` em `ConfirmarConfiguracaoMFA` e `ConcluirLoginMFA` no caminho de sucesso.
  - `[low]` `[patch]` `<QRCodeSVG>` em `ConfiguracoesPage.tsx` não tinha `role`/`aria-label` — usuários de leitor de tela não recebiam nenhuma indicação de que um QR Code está presente. Adicionado `role="img"` + `aria-label="QR Code para configurar o autenticador"`.
  - `[low]` `[patch]` O campo de código na tela de configuração de MFA não tinha `autoComplete="one-time-code"` (presente no campo equivalente de `LoginPage.tsx`), inconsistência trivial entre os dois formulários quase idênticos. Adicionado.
- rejeitados de nota (contexto que o revisor cego não tinha):
  - ausência de fluxo de recuperação/desativação de MFA (perda de dispositivo): decisão explícita do `<intent-contract>` (`Never`), já registrada como limitação aceita — corrigido nesta passagem apenas o esquecimento de bookkeeping (o item não estava de fato na lista `deferred:` do frontmatter apesar do texto do `Never` prometer isso; adicionado agora).
  - `mfa_secret` em texto puro / sem endpoint de rotação: decisão deliberada e justificada nas Design Notes, mesmo precedente de `sessoes.refresh_token`/`tokens_acao.token` já usado no restante do código.
  - ausência de rate-limit dedicado em `POST /mfa/iniciar`: endpoint autenticado, sem nenhuma escrita em banco até a confirmação — custo de abuso é desprezível (só CPU para gerar um segredo aleatório), sem exposição anônima.
  - ambiguidade de `RowsAffected==0` em `ConfirmarConfiguracaoMFA` (já configurado vs. conta excluída no meio do request): não existe fluxo de auto-exclusão de conta nesta aplicação (só desativação, Story 1.8); a corrida exigiria uma exclusão de linha em `usuarios` entre o `RequireAuth` e o `UPDATE`, alguns milissegundos depois — cenário não realista para o código atual.

## Design Notes

- **Por que `origem` no JWT e não só em `sessoes`:** o access JWT (usado em toda requisição autenticada) nunca consulta `sessoes` — só o refresh token faz isso, em `/login`/`/refresh`. Sem o claim, `RequireRole` não teria como saber a proveniência da sessão corrente sem uma consulta extra por requisição a uma tabela que não guarda esse vínculo hoje. Colocar `origem` no claim é a mesma classe de decisão que já existe para `sub`: um dado imutável para a vida da sessão, nunca um estado de conta que possa ficar defasado.
- **Por que reaproveitar `tentativas_login_falhas`/`bloqueado_ate` para código TOTP errado:** um código de 6 dígitos tem só 10^6 combinações — sem throttle, seria mais fácil de forçar por brute force que a própria senha. Criar um contador dedicado duplicaria a Story 1.10 sem necessidade; a conta já bloqueia após 5 falhas de QUALQUER natureza de login (senha ou código), o que é estritamente mais seguro.
- **Por que não persistir o segredo pendente durante o enrollment:** `POST /mfa/confirmar` recebe `segredo` de volta do cliente (não um handle server-side) porque a operação só afeta a própria conta de quem já está autenticado (`RequireAuth`) — validar o código contra o segredo enviado é suficiente; não há ganho de segurança em manter estado pendente no servidor, só complexidade (expiração de rascunho, limpeza).
- Exemplo do núcleo HOTP (RFC 4226) em `ValidarCodigoTOTP`:

```go
contador := uint64(time.Now().Unix()) / 30
for _, delta := range []int64{-1, 0, 1} {
    if gerarCodigoHOTP(segredo, contador+uint64(delta)) == codigoNormalizado {
        return true
    }
}
return false
```

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` — Postgres real (mesmo setup das Stories 1.5–1.10). Cobre `totp_test.go`, os novos casos de `services/auth_test.go`, `middleware/roles_test.go`/`auth_test.go` e `handlers/auth_mfa_test.go`; migration `000006` aplica sem erro.
- `cd frontend && npm run lint && npm run build && npm run test` — `oxlint`, `tsc`+`vite`, e os novos casos de `LoginPage.test.tsx`/`ConfiguracoesPage.test.tsx` passam; `qrcode.react` resolve no build.
- `docker compose up --build` — 6 tentativas de código TOTP errado para uma conta com MFA ativo devolvem `429` na 6ª; um `gestor` sem MFA recebe `403 MFA_SETUP_REQUIRED` em `GET /api/usuarios` e é redirecionado para `/configuracoes` no frontend; um `gestor` via SSO acessa a mesma rota sem esse bloqueio. Se `docker` indisponível, mesma nota das stories anteriores (cobertura equivalente via testes de integração contra Postgres real).

**Manual checks (if no CLI):**
- Criar conta `gestor` por senha, confirmar que `GET /api/usuarios` responde `403 MFA_SETUP_REQUIRED` e que a UI redireciona para Configurações → Segurança. Escanear o QR com um autenticador real (Google Authenticator/Authy), confirmar o código, e verificar `SELECT mfa_habilitado, mfa_secret FROM usuarios` gravado. Deslogar e logar de novo: a segunda etapa de código aparece; um código do autenticador conclui o login; um código errado 5x bloqueia a conta por 15min.
