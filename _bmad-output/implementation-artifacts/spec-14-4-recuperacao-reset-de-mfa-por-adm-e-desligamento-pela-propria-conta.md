---
title: 'Story 14.4 — Recuperação: reset de MFA por adm e desligamento pela própria conta'
type: 'feature'
created: '2026-09-24'
status: 'done'
baseline_revision: '59def023a65479bf75c0f7785ca61268ad51816e'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-14-context.md'
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Quem perde o celular com o autenticador fica preso: não existe forma de zerar o MFA de uma conta nem de a própria conta desligar o seu segundo fator. Com o MFA opcional (14.1), isso vira bloqueio permanente sem suporte da plataforma.

**Approach:** Duas rotas novas que zeram as colunas de MFA e gravam `auditoria_seguranca` (tabela da 14.3) na mesma transação:
- o `adm` reseta o MFA de uma conta de rank menor da própria Empresa, revogando as sessões dela;
- qualquer conta desliga o próprio MFA com senha atual + código TOTP, contando falhas no bloqueio por tentativas.

No frontend: ação "Resetar MFA" na Gestão de Usuários e "Desligar meu MFA" no card Segurança.

## Boundaries & Constraints

**Always:**
- **Reset:** `POST /e/{slug}/api/usuarios/{id}/mfa-reset`, sem corpo, atrás de `RequireAuth → RequireRole(services.PapelAdm)`. Papel abaixo de `adm` recebe 403 FORBIDDEN do próprio `RequireRole`.
  - Alvo buscado por `id AND empresa_id` da requisição. Inexistente, de outra Empresa ou id não-UUID (`pq` 22P02) → 404 NOT_FOUND, sem revelar existência.
  - `RankPapel(papelAlvo) >= RankPapel(papelAtor)` (outro `adm`, ou a própria conta) → 403 FORBIDDEN.
  - Alvo com `mfa_habilitado = false` → 409 `MFA_NAO_CONFIGURADO`, nada gravado.
  - Numa transação: `UPDATE usuarios SET mfa_habilitado=false, mfa_secret=NULL, mfa_ultimo_passo_usado=NULL WHERE id AND empresa_id AND papel=<papel lido> AND mfa_habilitado=true` (0 linhas → 409 CONFLICT via `ErrEstadoContaMudou`); revoga todas as sessões vivas do alvo (`UPDATE sessoes SET revogado_em=now() ...`); invalida tokens `mfa_login` pendentes do alvo; INSERT `auditoria_seguranca` (`acao='mfa_resetado'`, `ator_id`=adm, `alvo_id`=alvo, `detalhe` `{}`).
  - 200 `{"usuario": UsuarioResumo}` já atualizado.
- **Desligar:** `POST /e/{slug}/api/auth/mfa/desligar`, corpo `{"senhaAtual": string, "codigo": string}` (limite `authRequestMaxBytes`), atrás só de `RequireAuth` (qualquer papel). Ordem das checagens:
  1. Corpo não-JSON → 400 VALIDATION_ERROR.
  2. Sessão sem MFA (`usuario.MFAHabilitado == false`) → 409 `MFA_NAO_CONFIGURADO`.
  3. `RankPapel(papel) >= RankPapel(gestor)` e `empresa.MFAObrigatorio` → 409 `MFA_EXIGIDO_PELA_EMPRESA`, mensagem: "A Empresa exige dupla autenticação para o seu papel; ela não pode ser desligada." Nada é alterado e não conta tentativa.
  4. Conta com `bloqueado_ate` no futuro → 429 ACCOUNT_LOCKED (mesma mensagem do login), nada alterado.
  5. Senha errada (bcrypt sempre roda, `dummyBcryptHash` se `senha_hash` nulo) ou código TOTP inválido → `registrarFalhaLogin` + 401 `INVALID_CREDENTIALS` "Senha ou código inválido." (uma resposta única para as duas falhas). Bloqueio expirado é destravado antes, no molde de `Login`.
  6. Sucesso: numa transação, `UPDATE usuarios SET mfa_habilitado=false, mfa_secret=NULL, mfa_ultimo_passo_usado=NULL, tentativas_login_falhas=0, bloqueado_ate=NULL WHERE id=$1 AND mfa_habilitado=true AND mfa_secret=$segredoLido AND (mfa_ultimo_passo_usado IS NULL OR mfa_ultimo_passo_usado <> $passoAtual)`. Se 0 linhas mudarem, é tratado como código inválido: rollback, `registrarFalhaLogin` e 401. Se mudar 1, INSERT `auditoria_seguranca` (`acao='mfa_desligado'`, `ator_id` = `alvo_id` = a própria conta, `detalhe` `{}`). Resposta 200 `{}`. As sessões da própria conta NÃO são revogadas.
- `UsuarioResumo` ganha `MFAHabilitado bool \`json:"mfaHabilitado"\``, lido em `ListarUsuarios` e `relerUsuarioResumoTx`.
- A redefinição de senha por e-mail (`RedefinirSenha`) continua sem tocar em colunas de MFA — a 14.4 só prova isso com teste.
- O gate da 14.1 (`RequireRole`) decide sozinho o que acontece depois do reset; nenhum handler novo o reimplementa.
- Frontend:
  - `GestaoUsuariosSection` mostra "Resetar MFA" só quando o ator é `adm`, a conta tem `mfaHabilitado`, `rankPapel(c.papel) < rankPapel('adm')` e não é o ator. A ação passa pelo `ConfirmDialog` já existente e refaz a lista depois, com sucesso ou falha.
  - `SegurancaCard`, com MFA ativo, mostra "Desligar meu MFA". O botão abre um formulário inline com senha atual e código. No sucesso: `atualizarUsuario({...usuario, mfaHabilitado:false})` e toast. Se `rank>=gestor && empresa.mfaObrigatorio`, o botão não aparece e fica o texto do 409. O 409 do servidor também vira alerta.

**Block If:** nada previsto.

**Never:**
- Não mexer em `RequireRole`, `/api/auth/me`, `Login`, `ConcluirLoginMFA` nem `RedefinirSenha`.
- Sem trigger no banco, sem migração nova: o CHECK de `acao` já aceita `mfa_resetado`/`mfa_desligado` (000048).
- Sem códigos de recuperação impressos, sem reset pelo Dono da Plataforma, sem reconfigurar MFA sem desligar antes.
- `gestor` não reseta MFA de ninguém (a rota é só `adm`).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Reset ok | adm; gestor da mesma Empresa com MFA e 1 sessão viva | 200 com `usuario.mfaHabilitado=false`; colunas MFA nulas/false; sessão revogada; 1 linha `mfa_resetado` com ator=adm, alvo=gestor | — |
| Reentrada exige | Empresa `true`; gestor resetado faz login por senha | login devolve sessão (sem `mfaRequerido`); rota `RequireRole(gestor)` → 403 MFA_SETUP_REQUIRED | — |
| Reentrada não exige | Empresa `false`; gestor resetado faz login | sessão; rota `RequireRole(gestor)` → 200 | — |
| Reset alvo adm | adm reseta outro adm, ou a si mesmo | 403 FORBIDDEN; nada muda; sem auditoria | — |
| Reset outra Empresa / inexistente / não-UUID | id de conta da Empresa B, UUID aleatório, `abc` | 404 NOT_FOUND | — |
| Reset por gestor/usuario | token de gestor ou usuario | 403 FORBIDDEN (RequireRole) | — |
| Reset sem MFA | alvo com `mfa_habilitado=false` | 409 MFA_NAO_CONFIGURADO; sem auditoria | — |
| Desligar ok | usuario (ou gestor em Empresa `false`) com MFA; senha e código certos | 200; colunas MFA zeradas; 1 linha `mfa_desligado` ator=alvo=conta; sessão continua válida | — |
| Desligar senha errada | senha errada, código certo | 401 INVALID_CREDENTIALS; MFA intacto; `tentativas_login_falhas` +1 | — |
| Desligar código errado | senha certa, código `000000` inválido | 401; MFA intacto; tentativas +1 | — |
| Desligar bloqueio | 5 falhas seguidas, depois senha e código certos | 5ª falha grava `bloqueado_ate`; 6ª chamada 429 ACCOUNT_LOCKED; MFA intacto | — |
| Desligar exigido | gestor/adm com MFA; Empresa `true` | 409 MFA_EXIGIDO_PELA_EMPRESA; nada muda; tentativas inalteradas | — |
| Desligar sem MFA | conta sem MFA | 409 MFA_NAO_CONFIGURADO | — |
| Redefinir senha | conta com MFA redefine senha por token | MFA continua `true`; login seguinte devolve `mfaRequerido:true` | — |
| Sem token | as 2 rotas novas | 401 (RequireAuth) | — |

</intent-contract>

## Code Map

- `backend/services/gestao_usuarios.go` -- molde do reset: `carregarAlvoParaGestao` (404 por `pq` 22P02/ErrNoRows escopado por `empresa_id`), `AlterarAtivacaoUsuario` (tx, UPDATE guardado por papel → `ErrEstadoContaMudou`, revogação de sessões), `relerUsuarioResumoTx` (acrescentar `mfa_habilitado`). Erros reaproveitáveis: `ErrGestaoForaDeEscopo`, `ErrContaNaoEncontrada`, `ErrEstadoContaMudou`. O reset NÃO usa `carregarAlvoParaGestao` direto, porque a regra de rank é estrita (`>=` → 403), inclusive adm→adm. Fazer um SELECT próprio de `papel, mfa_habilitado`.
- `backend/services/usuarios.go:11-80` -- `UsuarioResumo` e `ListarUsuarios`: acrescentar `mfa_habilitado` aos dois SELECTs e ao Scan.
- `backend/services/seguranca_empresa.go` -- `AcaoExigenciaAlterada` e o INSERT em `auditoria_seguranca`. Acrescentar as constantes `AcaoMFAResetado = "mfa_resetado"` e `AcaoMFADesligado = "mfa_desligado"`.
- `backend/services/auth.go:480-600` -- `Login`/`registrarFalhaLogin`/`dummyBcryptHash`: molde do bloqueio (checar `bloqueado_ate` depois do bcrypt, destravar o expirado, `registrarFalhaLogin` não-fatal). `:777-880` `ConcluirLoginMFA`: molde de anti-reuso via `PassoAtualTOTP()` e `mfa_ultimo_passo_usado`. `:912` `ConfirmarConfiguracaoMFA`: molde de senha atual. Novos erros: `ErrMFANaoConfigurado`, `ErrMFAExigidoPelaEmpresa`.
- novo `backend/services/recuperacao_mfa.go` (+ `_test.go`):
  - `ResetarMFAUsuario(db, empresaID, alvoID, atorID, papelAtor string) (UsuarioResumo, error)`;
  - `DesligarMFAPropria(db, usuarioID, papel string, empresaExige bool, senhaAtual, codigo string) error`.
- `backend/handlers/gestao_usuarios.go` -- molde de handler/mapeamento de erro. Novo `ResetarMFAUsuarioHandler` no mesmo arquivo.
- `backend/handlers/auth_mfa.go` -- novo `MFADesligarHandler(db)`: `UsuarioDaSessao`, `empresaDaRequisicao` (`handlers/auth.go:53`), `MaxBytesReader`, 409 `MFA_NAO_CONFIGURADO` se `!usuario.MFAHabilitado`, e depois o service. Mapeamento: `ErrMFAExigidoPelaEmpresa`→409, `ErrContaBloqueada`→429 ACCOUNT_LOCKED, `ErrCredenciaisInvalidas`/`ErrMFACodigoInvalido`→401 INVALID_CREDENTIALS.
- `backend/main.go:371-397` -- registrar `POST /e/{slug}/api/auth/mfa/desligar` junto das rotas de MFA (só RequireAuth) e `POST /e/{slug}/api/usuarios/{id}/mfa-reset` junto de desativação/rebaixamento, com `RequireRole(PapelAdm)`. Atualizar o doc-comment do topo e os comentários.
- `backend/main_test.go:~471` -- tabela "sem token chega no RequireAuth": acrescentar as 2 rotas.
- Helpers de teste (reutilizar, não duplicar):
  - `handlers/auth_mfa_test.go`: `habilitarMFAConta`, `codigoTOTPHandlerTeste`, `tokenDeLoginComMFA`, `definirMFAObrigatorioEmpresaTeste`, `postLogin`;
  - `handlers/seguranca_empresa_test.go`: `prepararSegurancaEmpresaTeste`, `contaSenhaSemMFA`, `contarAuditoriaSeguranca`;
  - `handlers/gestao_usuarios_test.go`: `sessoesVivasDe`, `postDesativacao`;
  - `criarContaComPapel` e `tokenDeLogin`;
  - `handlers/auth_test.go:354` `criarUsuarioLogin`.

  O teste de redefinição de senha reutiliza os helpers de `EsqueciSenha`/`RedefinirSenha` já existentes em `handlers/auth_test.go`, ou grava um token `redefinicao_senha` direto em `tokens_acao`. A Empresa padrão é compartilhada: todo teste que liga o flag restaura `false` e limpa a auditoria (via `prepararSegurancaEmpresaTeste`).
- `frontend/src/components/usuarios/GestaoUsuariosSection.tsx` (+ `.test.tsx`) -- `UsuarioResumo.mfaHabilitado` e um terceiro `tipo` `'resetar-mfa'` em `AcaoPendente`/`executar`: `POST /api/usuarios/{id}/mfa-reset`, sem corpo. Confirmação com o título "Resetar a dupla autenticação de {nome}?" e a descrição "As sessões da conta são encerradas e ela precisará configurar um novo MFA se a Empresa exigir.".
- `frontend/src/pages/ConfiguracoesPage.tsx:133-320` (`SegurancaCard`) (+ `ConfiguracoesPage.test.tsx`) -- ramo `mfaHabilitado`: botão "Desligar meu MFA" que alterna para `etapa 'desligando'` (form com `mfa-desligar-senha` e `mfa-desligar-codigo`). Mapeamento de erro: 401 → "Senha ou código inválido.", 409 `MFA_EXIGIDO_PELA_EMPRESA` → a mensagem do servidor, 429 → "Muitas tentativas. Tente novamente mais tarde.", outros → genérica. Exigência: `rankPapel(papel) >= rankPapel('gestor') && usuario.empresa?.mfaObrigatorio`. Atualizar o doc-comment.
- `frontend/src/components/seguranca/MfaEmpresaSection.tsx` -- já rotula `mfa_resetado`/`mfa_desligado` no histórico. Só leitura: nenhuma mudança esperada.

## Tasks & Acceptance

**Execution:**
- `backend/services/usuarios.go`, `backend/services/gestao_usuarios.go` -- `mfaHabilitado` em `UsuarioResumo` e nas duas leituras -- para a UI saber quando oferecer o reset.
- `backend/services/seguranca_empresa.go` -- constantes `AcaoMFAResetado`/`AcaoMFADesligado`.
- `backend/services/auth.go` -- declarar `ErrMFANaoConfigurado` e `ErrMFAExigidoPelaEmpresa` no bloco `var` de erros.
- `backend/services/recuperacao_mfa.go` -- `ResetarMFAUsuario` e `DesligarMFAPropria` conforme Always.
- `backend/services/recuperacao_mfa_test.go` -- testes de service:
  - reset: zera colunas, revoga sessões, 1 linha de auditoria; rank (adm→adm, self → `ErrGestaoForaDeEscopo`); outra Empresa → `ErrContaNaoEncontrada`; sem MFA → `ErrMFANaoConfigurado`;
  - desligar: ok com auditoria; senha errada e código errado incrementam tentativas; exigido → erro sem incremento; bloqueio na 5ª falha;
  - `RedefinirSenha` preserva `mfa_habilitado`/`mfa_secret`.
- `backend/handlers/gestao_usuarios.go`, `backend/handlers/auth_mfa.go` -- os 2 handlers.
- `backend/handlers/recuperacao_mfa_test.go` -- cobrir na fronteira HTTP todas as linhas da matriz. As linhas de reentrada usam `postLogin` e uma rota `RequireRole(gestor)` já usada em `TestRequireRole_GateCondicionalAEmpresa`. A linha "Redefinir senha" passa por `RedefinirSenhaHandler` e depois `postLogin`, e confere `mfaRequerido:true`.
- `backend/main.go`, `backend/main_test.go` -- registro e casos sem token.
- `frontend/src/components/usuarios/GestaoUsuariosSection.tsx` + `.test.tsx` -- testes:
  - "Resetar MFA" aparece para o `adm` numa conta gestor com MFA;
  - não aparece para conta sem MFA, para conta `adm`, para a própria conta nem para o ator `gestor`;
  - confirmar faz `POST .../mfa-reset` e recarrega a lista;
  - cancelar não chama a API.
- `frontend/src/pages/ConfiguracoesPage.tsx` + `ConfiguracoesPage.test.tsx` -- testes:
  - "Desligar meu MFA" envia `{senhaAtual, codigo}` e atualiza a sessão;
  - o 401 vira alerta;
  - gestor em Empresa que exige não vê o botão e vê a explicação.

**Acceptance Criteria:**
- Given a Gestão de Usuários aberta por um `adm`, when ele confirma "Resetar MFA" numa conta `gestor` com MFA, then a lista recarregada mostra a conta sem a ação de reset, e o histórico de auditoria da Empresa (14.3) lista `mfa_resetado` com ator e alvo.
- Given o card Segurança de uma conta com MFA ativo numa Empresa que não exige, when ela informa a senha atual e o código corretos em "Desligar meu MFA", then o card passa a mostrar a opção de configurar, e a auditoria registra `mfa_desligado`.
- Given um `gestor` com MFA ativo numa Empresa que exige, when abre o card Segurança, then não há botão "Desligar meu MFA" e a explicação da exigência é exibida.

## Spec Change Log

## Review Triage Log

## Design Notes

**Por que 401 único para senha ou código:** a rota exige sessão. Quem tem um access token roubado não pode usar respostas diferentes como oráculo da senha da vítima. Por isso as duas falhas contam tentativa e devolvem a mesma resposta.

**Por que o UPDATE do desligamento é guardado por `mfa_secret` e pelo passo:** assim não precisa de `SELECT ... FOR UPDATE`, que travaria com `registrarFalhaLogin(db)` fora da transação. O guarda recusa a reutilização de um código já usado no login dentro dos mesmos ~30s e também uma troca de segredo concorrente.

**Rank estrito no reset:** `carregarAlvoParaGestao` deixa o `adm` agir sobre outro `adm`, e a 14.4 não pode. Por isso o reset faz a checagem própria: `RankPapel(alvo) < RankPapel(ator)`, senão 403.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: limpo
- `cd backend && DATABASE_URL='postgres://stockflow:stockflow@127.0.0.1:5432/stockflow_t91?sslmode=disable' go test -p 1 -count=1 . ./services/... ./handlers/... ./middleware/...` -- expected: verde
- `cd frontend && npx tsc -b && npx vitest run src/components/usuarios src/components/seguranca src/pages/ConfiguracoesPage.test.tsx src/App.test.tsx` -- expected: verde

## Auto Run Result

Status: done (recuperado manualmente)

**O que aconteceu:** a sessão de dev do `bmad-loop` (run `20260924-082417-6448`) bateu o limite de uso da API depois de terminar a implementação e antes da fase de review. O orquestrador marcou `stalled` e pausou com a árvore sem commit. **Esta story NÃO teve o review pass que as 14.1 a 14.3 tiveram.** No lugar dele, a verificação foi feita à mão, na recuperação.

**Verificação na recuperação:**
- `go build`, `go vet` e `gofmt -l`: limpos.
- Suíte Go completa com `-p 1`: verde. Todos os pacotes passaram; `services` levou 475s e precisou de `-timeout 25m`, porque o padrão de 10 min estoura quando a máquina está carregada.
- `tsc -b`: limpo.
- `vitest run` completo: 816 testes. Um timeout em `CadastroProdutoSection` sob carga, que passou rodando o arquivo sozinho (35/35). Nada a ver com esta story.
- Leitura das regras críticas no código:
  - reset só para `adm` (`RequireRole(PapelAdm)`), com rank estrito (`alvo == ator || rank(alvo) >= rank(ator)` → 403) e escopo por `empresa_id` (404 com 22P02);
  - UPDATE guardado por papel e por `mfa_habilitado`, na mesma transação que revoga sessões, invalida `mfa_login` e grava a auditoria `mfa_resetado`;
  - desligar faz bcrypt sempre, confere o bloqueio depois, devolve 401 único para senha ou código errados e conta tentativa;
  - 409 `MFA_EXIGIDO_PELA_EMPRESA` sem contar tentativa;
  - anti-reuso do TOTP pelo passo que casou (`< $3`);
  - frontend esconde os dois botões nas condições da spec.
