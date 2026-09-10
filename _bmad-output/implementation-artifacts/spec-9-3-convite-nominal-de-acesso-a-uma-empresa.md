---
title: 'Story 9.3: Convite nominal de acesso a uma Empresa'
type: 'feature'
created: '2026-09-10'
status: 'done'
baseline_revision: 'c89e6dd439541c26f2d26a0a57893f0d9f7209cd'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Depois da 9.2 existem Empresas e o primeiro `adm` de cada uma, mas o autocadastro (`POST /e/{slug}/api/auth/cadastro`) continua ABERTO: qualquer pessoa que descubra o slug de uma Empresa cria conta `usuario` nela e passa a enxergar Catálogo, Estoques e Pedidos daquele cliente. Não existe `convites_empresa`, nem forma de um `adm`/`gestor` convidar alguém nominalmente.

**Approach:** Criar a tabela `convites_empresa` (nominal, uso único, expira, revogável) e a área "Convites" em Configurações, onde `gestor`+ emite um convite para um e-mail específico e recebe um LINK para compartilhar. O autocadastro passa a exigir esse token: sem convite válido não nasce conta nenhuma, e a Empresa da conta é sempre a do convite — nunca um campo do formulário.

## Boundaries & Constraints

**Always:**
- O convite é NOMINAL e de USO ÚNICO (AD-22): `POST /api/auth/cadastro` só aceita se `normalizeEmail(form.email) == convites_empresa.email` — token correto com outro e-mail é recusado. `usado_em` é marcado na MESMA transação do `INSERT` em `usuarios`, com `UPDATE ... WHERE id=$1 AND usado_em IS NULL AND revogado_em IS NULL AND expira_em > now()` e `RowsAffected()==1` obrigatório: dois cadastros simultâneos com o mesmo token produzem exatamente uma conta.
- O convite é resolvido SEMPRE dentro da Empresa do slug da URL (`WHERE token=$1 AND empresa_id=$2`). Um token da Empresa A usado sob o slug da Empresa B é `NOT_FOUND` — nunca cria conta, nunca revela que o token existe.
- Papel da conta criada é sempre `usuario`: `services.Cadastrar` continua sem parâmetro de papel e o convite nunca concede papel (FR-42; promoção continua sendo FR-33/Story 1.7).
- Emissão, listagem e revogação exigem `RequireAuth` + `RequireRole(services.PapelGestor)` sob o prefixo `/e/{slug}` — o gate de papel é do middleware, nunca do handler. Toda query filtra por `empresa.ID` do contexto.
- Token do convite reusa `gerarTokenAcao()` (32 bytes de entropia, base64url) — nunca um código curto adivinhável.
- E-mail do convite é normalizado (`normalizeEmail`) na gravação e na comparação. Ordem em `Cadastrar`: validar nome/e-mail/senha/força/tamanho PRIMEIRO, resolver o convite DEPOIS — um payload inválido nunca queima um convite.
- Login continua sem qualquer seletor de Empresa: nada nesta story acrescenta campo, dropdown ou parâmetro de Empresa à tela ou à rota de login.

**Block If:**
- Cumprir o isolamento exigiria alterar o contrato de `middleware.RequireEmpresa` ou aceitar `empresa_id` vindo de corpo/query/header.

**Never:**
- Link genérico reutilizável, convite por domínio de e-mail, convite que concede papel acima de `usuario`, ou qualquer rota que crie conta sem convite.
- Enviar o convite por e-mail (`emails_pendentes`): o AC pede que o link seja "produzido para ser compartilhado", e quem compartilha é o emissor.
- Rate limit / bloqueio por força bruta na emissão e no resgate — segue no Deferred da arquitetura (AD-22/FR-36), a resolver junto do bloqueio de força bruta. A entropia de 32 bytes é a defesa desta story.
- Trocar de Empresa, conta em duas Empresas, ou reaproveitar `tokens_acao` para convite (AD-22: tabela separada, não há `usuario_id` na emissão).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Emitir convite | `gestor`+ da Empresa A, `POST /e/a/api/convites {"email":"Fulano@X.com"}` | 201 `{convite:{id,email:"fulano@x.com",expiraEm,link:"{APP_URL}/e/a/cadastro?token=…"}}`, linha em `convites_empresa` | — |
| E-mail vazio/inválido | `{"email":"  "}` ou sem `@` | 400 VALIDATION_ERROR, nenhuma linha gravada | VALIDATION_ERROR |
| E-mail já tem conta na Empresa | e-mail já em `usuarios` da Empresa A | 409 CONFLICT, nenhuma linha gravada | CONFLICT |
| Papel insuficiente | `usuario`/`almoxarife` autenticado | 403 FORBIDDEN de `RequireRole`, handler não executa | FORBIDDEN |
| Listar convites | `gestor`+ da Empresa A | 200 `{convites:[…]}` só da Empresa A, cada um com `situacao` ∈ pendente/usado/revogado/expirado; `link` só nos pendentes | — |
| Revogar pendente | `POST /e/a/api/convites/{id}/revogacao` | 200, `revogado_em` preenchido | — |
| Revogar usado/inexistente/de outra Empresa | id usado, id aleatório, ou id da Empresa B | 409 CONFLICT (usado) / 404 NOT_FOUND (demais) | CONFLICT / NOT_FOUND |
| Validar link ao abrir | `GET /e/a/api/auth/convite?token=…` válido | 200 `{email:"fulano@x.com"}` para pré-preencher o formulário | — |
| Cadastro com convite válido | `POST /e/a/api/auth/cadastro {token,nome,email:"FULANO@x.com",senha}` | 201, conta `usuario` com `empresa_id` da Empresa A, `usado_em` marcado, e-mail de verificação enfileirado | — |
| Cadastro com e-mail diferente | token válido, `email:"outro@x.com"` | 403 FORBIDDEN "Este convite foi emitido para outro e-mail.", convite intacto | FORBIDDEN |
| Cadastro sem token / token inexistente / token de outra Empresa | `token:""` ou valor aleatório ou token da Empresa B | 404 NOT_FOUND, nenhuma conta criada | NOT_FOUND |
| Cadastro com convite expirado | `expira_em < now()` | 400 TOKEN_EXPIRED "Este convite expirou." | TOKEN_EXPIRED |
| Cadastro com convite já usado | `usado_em` preenchido | 409 CONFLICT "Este convite já foi utilizado." | CONFLICT |
| Cadastro com convite revogado | `revogado_em` preenchido | 403 FORBIDDEN "Este convite foi cancelado pela empresa." | FORBIDDEN |
| Payload inválido com token válido | senha fraca / nome vazio | 400 VALIDATION_ERROR e convite PERMANECE pendente | VALIDATION_ERROR |

</intent-contract>

## Code Map

**Schema (a maior migration atual é a `000033`)**
- `backend/migrations/000031_create_empresas.up.sql` -- molde de tabela nova (UUID `gen_random_uuid()`, `timestamptz`, comentário de cabeçalho citando story/AD). `backend/migrations/000033_create_donos_plataforma.up.sql` -- molde de índice parcial + `ON DELETE CASCADE` + `idx_*` para FK consultada.
- `backend/migrations/000032_add_empresa_id_dominio.up.sql:58-64` -- `idx_usuarios_email_lower` é `(empresa_id, lower(email)) NULLS NOT DISTINCT`: a colisão de e-mail já é POR Empresa, então `ErrEmailDuplicado` só dispara dentro da mesma Empresa.
- `usuarios.nome`/`usuarios.email` são `VARCHAR(255)` (000001) — `convites_empresa.email` acompanha.

**Backend — autocadastro (o ponto de mudança de contrato)**
- `backend/services/auth.go:169` `Cadastrar(db, emailCfg, empresaID, empresaSlug, nome, email, senha)` -> ganha o parâmetro `tokenConvite` e passa a consumir o convite DENTRO da `tx` que já existe (INSERT usuarios + tokens_acao + emails_pendentes). A ordem atual — validação de campos (`:172`), `ValidarForcaSenha` (`:180`), guards de 255 runes (`:191`) — precisa continuar ANTES da resolução do convite.
- `backend/services/auth.go:139` `normalizeEmail`; `:147` `gerarTokenAcao` (32 bytes, base64url, seguro em URL); `:72-137` bloco `var (...)` dos erros do pacote — os `ErrConvite*` novos entram aqui, no mesmo estilo comentado.
- `backend/services/email.go:45` `LinkDaEmpresa(appURL, slug, caminho, token)` monta o link do convite com `caminho="/cadastro"`. NÃO mexer em `renderizarTemplate` nem em `emails_pendentes.tipo`: convite não vira e-mail.
- `backend/handlers/auth.go:84` `CadastroHandler` -> `cadastroRequest` (`:72`) ganha `Token`; o `switch` de erros (`:103-119`) ganha os ramos de convite. `:53` `empresaDaRequisicao`; `:32-42` `escreverErro`/`escreverJSON`; `:63` `cadastroRequestMaxBytes`.
- `backend/handlers/auth.go:477` `ValidarRedefinicaoSenhaHandler` -- molde EXATO do `GET` que checa um link sem consumir (200 `{valido:true}` / 404 NOT_FOUND / 400 TOKEN_EXPIRED).

**Backend — superfície nova de convites**
- `backend/services/gestao_usuarios.go` e `backend/services/usuarios.go` -- moldes de service que recebem `empresaID` + `papelAtor` explícitos e nunca reconsultam papel (AD-8 forma 3). `services/usuarios.go:17` `UsuarioResumo` é o molde de projeção JSON.
- `backend/handlers/gestao_usuarios.go:36` -- molde de handler `POST /{id}/…` com `r.PathValue("id")`, `middleware.UsuarioDaSessao`, `empresaDaRequisicao` e `switch` de erros. `backend/handlers/usuarios.go` -- molde de handler de listagem.
- `backend/main.go:298` `newMux`; `:345-380` o bloco de rotas de auth/usuarios, onde entram, via `registrar` (que aplica `RequireEmpresa` por fora): `POST|GET /e/{slug}/api/convites`, `POST /e/{slug}/api/convites/{id}/revogacao` (com `RequireAuth`+`RequireRole(services.PapelGestor)`) e `GET /e/{slug}/api/auth/convite` (público, ao lado de `:346` verificar-email).
- `backend/middleware/roles.go:47` `RequireRole` já decide 403 FORBIDDEN e MFA_SETUP_REQUIRED; `backend/services/papel.go:8` `PapelGestor`.

**Testes backend**
- `DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable`; rodar `go test -p 1 ./...`.
- ARMADILHA (bloqueante): `Cadastrar` é chamada em 11 pontos de `services/auth_test.go` (`:115,208,216,250,289,313,326,1630,1648`), `services/realtime_test.go:20` e 4 de `handlers/auth_test.go` (`:221,284,311,1136`), mais o POST HTTP em `handlers/auth_test.go:103`. TODAS precisam de um convite. Criar um helper por pacote (`conviteDeTeste(t, db, empresaID, email) string`) e usá-lo; nos casos de tabela que testam validação inválida (`:250,289,1630`) o token pode ser `""`, porque a validação vence antes.
- Helpers existentes: `services/empresa_teste_test.go` (`criarEmpresaDeTeste`, `cnpjDeTeste`, `removerEmpresaDeTeste`, `empresaTeste`, `slugEmpresaTeste`), `handlers/empresa_teste_test.go` (`comEmpresa`, `prefixoEmpresaTeste`, `empresaTeste`), `handlers/main_test`-style `tokenDeMux`. `removerEmpresaDeTeste` só apaga categorias/templates/empresa — uma Empresa de teste com convites precisa que os convites saiam antes (ou o teste os apague), pois `empresas` NUNCA é truncada e a FK impede o DELETE.
- `backend/services/isolamento_test.go:144` `TestIsolamentoPorEmpresa_LeituraNuncaCruza` -- onde entra a prova de que convite/listagem não cruzam a fronteira.
- `backend/main_test.go:313` `TestNewMux_RegistraRotasDeAutenticacao` -- tabela de rotas; acrescentar os 4 caminhos novos (o caso "cadastro com payload invalido" continua 400).

**Frontend**
- `frontend/src/pages/CadastroPage.tsx` -- passa a ler `?token=` e a exigi-lo. Molde de fase/bootstrap: `frontend/src/pages/RedefinirSenhaPage.tsx:1-80` (`useSearchParams`, `type Fase`, `tokenValidado = useRef`, `faseParaCodigoGet`, estados explicativos com caminho para `/login`). `frontend/src/pages/CadastroPage.test.tsx:40` afirma `url === '/api/auth/cadastro'` e que `papel` nunca vai no payload — os dois continuam valendo.
- `frontend/src/components/usuarios/GestaoUsuariosSection.tsx` -- molde COMPLETO da seção nova (`Card`+`CardHeader`+`CardDescription`, `useAuth`, `rankPapel(usuario?.papel) >= rankPapel('gestor')`, `apiUrl`/`authHeaders`, erro inline `role="alert"`, recarregar a lista após sucesso E falha, `ConfirmDialog` com `confirmVariant="destructive"` para a ação que reduz acesso).
- `frontend/src/pages/ConfiguracoesPage.tsx:529` -- ponto de montagem: `{podeDecidir && <ConvitesSection />}` ao lado de `<GestaoUsuariosSection />`; o docblock do topo (`:17-56`) lista cada seção e precisa da entrada nova.
- `frontend/src/lib/api.ts:57` `apiUrl` / `:70` `authHeaders`; `frontend/src/lib/senha.ts` `senhaAtendePolitica`; `frontend/src/components/ui/{card,input,label,button}`; `frontend/src/components/ConfirmDialog.tsx`.
- Copiar o link: `navigator.clipboard` não existe em jsdom — o input somente-leitura com o link visível é o caminho primário e o botão "Copiar" é conveniência com `try/catch`.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000034_create_convites_empresa.up.sql` / `.down.sql` -- criar `convites_empresa` (`id`, `empresa_id` FK, `email VARCHAR(255)`, `token TEXT UNIQUE`, `expira_em`, `usado_em`, `revogado_em`, `criado_por` FK `usuarios(id)`, `criado_em`) + `idx_convites_empresa_empresa_id` -- AD-22 exige tabela própria; `revogado_em` implementa a revogação do AC.
- `backend/services/convites.go` -- `EmitirConvite(db, empresaID, empresaSlug, emailCfg, criadoPor, email)`, `ListarConvites(db, empresaID)` (com `situacao` derivada), `RevogarConvite(db, empresaID, conviteID)` e os erros `ErrConviteNaoEncontrado`/`ErrConviteExpirado`/`ErrConviteJaUsado`/`ErrConviteRevogado`/`ErrConviteEmailDivergente`/`ErrConviteEmailJaCadastrado` -- toda query filtra por `empresaID`.
- `backend/services/auth.go` -- `Cadastrar` ganha `tokenConvite`: resolve o convite na `tx` (`SELECT … WHERE token=$1 AND empresa_id=$2 FOR UPDATE`), compara e-mail, insere a conta e marca `usado_em` com `UPDATE` condicional exigindo `RowsAffected()==1`; nova `ValidarTokenConvite(db, empresaID, token) (email, error)` para o `GET`.
- `backend/handlers/convites.go` -- `EmitirConviteHandler`, `ListarConvitesHandler`, `RevogarConviteHandler` mapeando os erros à I/O Matrix.
- `backend/handlers/auth.go` -- `cadastroRequest.Token`, repasse a `services.Cadastrar` e os ramos novos do `switch`; `ValidarConviteHandler` para `GET /api/auth/convite`.
- `backend/main.go` -- registrar as 4 rotas em `newMux` (3 atrás de `RequireAuth`+`RequireRole(gestor)`, o `GET` de validação público), sempre via `registrar`.
- `backend/services/convites_test.go`, `backend/handlers/convites_test.go` -- cobrir cada linha da I/O Matrix, incluindo a corrida de duplo-resgate do mesmo token (duas goroutines -> exatamente uma conta).
- `backend/services/auth_test.go`, `backend/services/realtime_test.go`, `backend/handlers/auth_test.go` -- helper `conviteDeTeste` e adaptação de TODOS os chamadores de `Cadastrar`; caso novo: payload inválido não queima o convite.
- `backend/services/isolamento_test.go` -- convite da Empresa A não resolve sob a Empresa B e não aparece em `ListarConvites` de B.
- `backend/main_test.go` -- acrescentar as rotas novas à tabela de `TestNewMux_RegistraRotasDeAutenticacao`.
- `frontend/src/components/usuarios/ConvitesSection.tsx` + `.test.tsx` -- seção "Convites" (gestor+): formulário de e-mail, link exibido em campo somente-leitura + "Copiar", lista com `situacao` e "Revogar" sob `ConfirmDialog`.
- `frontend/src/pages/ConfiguracoesPage.tsx` -- montar `<ConvitesSection />` sob o mesmo gate de `GestaoUsuariosSection` e atualizar o docblock.
- `frontend/src/pages/CadastroPage.tsx` + `.test.tsx` -- exigir `?token=`, validar no mount, pré-preencher o e-mail somente-leitura, enviar `token` no POST e traduzir NOT_FOUND/TOKEN_EXPIRED/CONFLICT/FORBIDDEN em mensagens distintas.

**Acceptance Criteria:**
- Given um `gestor` autenticado na Empresa A sem nenhum convite emitido, when ele emite um convite para `fulano@x.com` e em seguida a pessoa abre o link devolvido e conclui o autocadastro com esse mesmo e-mail, then existe exatamente uma conta `usuario` com `empresa_id` da Empresa A e o convite fica com `usado_em` preenchido.
- Given um convite pendente da Empresa A, when o `gestor` o revoga e só depois alguém abre o link, then o cadastro é recusado e nenhuma conta é criada.
- Given duas Empresas A e B e um convite emitido em A, when o token de A é usado em `POST /e/b/api/auth/cadastro` e quando `GET /e/b/api/convites` é consultado por um `gestor` de B, then o cadastro responde 404 sem criar conta e a listagem de B não contém o convite de A.
- Given a tela de login de qualquer Empresa, when ela é renderizada, then não existe nenhum campo, seletor ou dropdown de Empresa — a Empresa continua vindo só do slug da URL.

## Spec Change Log

## Review Triage Log

## Design Notes

**Por que o convite NÃO é enviado por e-mail.** O AC diz "um link é produzido para ser compartilhado com essa pessoa": quem compartilha é o emissor (WhatsApp de canteiro, conforme o addendum do PRD). Enviar por e-mail exigiria estender `emails_pendentes_tipo_check` e um template, sem nenhum AC pedindo — e o `adm` provisionado da 9.2 já cobre o caso em que o produto precisa alcançar alguém por e-mail. A verificação de e-mail da Story 1.3 continua acontecendo normalmente DEPOIS do cadastro.

**Por que 7 dias de validade.** O prazo ficou como decisão de implementação (PRD FR-42 / Deferred da arquitetura). A 9.2 já fixou 7 dias para o link de primeiro acesso do `adm` (`tokenPrimeiroAcessoExpiracao`); um convite é o mesmo tipo de evento (onboarding, não recuperação de 30min). Uma constante `conviteExpiracao = 7 * 24 * time.Hour` em `services/convites.go`, no mesmo estilo.

**Por que `revogado_em` em vez de apagar a linha.** AD-22 lista as colunas mínimas, mas o AC exige revogação com "mensagem clara do motivo" — apagar a linha colapsaria "revogado" em "inexistente" e perderia a trilha de quem convidou quem. Uma coluna nullable a mais preserva os dois.

**Por que o e-mail volta no `GET` de validação.** Pré-preencher o campo somente-leitura elimina o erro honesto de digitação e torna o caso "e-mail diferente" um ataque, não um acidente. O token já é o segredo: quem o tem é o destinatário pretendido. O servidor continua sendo a autoridade e revalida no POST — o campo somente-leitura é conveniência, nunca a barreira.

**Por que o e-mail já cadastrado é 409 na EMISSÃO.** Sem esse guard o convite nasceria morto: o `INSERT` em `usuarios` bateria em `idx_usuarios_email_lower` e a pessoa veria "e-mail já cadastrado" depois de abrir o link. Falhar cedo, no emissor, é o único momento em que alguém pode agir.

**Forma da linha da listagem (golden):**
```json
{"id":"…","email":"fulano@x.com","situacao":"pendente","expiraEm":"2026-09-17T12:00:00Z",
 "criadoEm":"2026-09-10T12:00:00Z","criadoPorNome":"Maria","link":"https://…/e/acme/cadastro?token=…"}
```
`situacao` é derivada na leitura (`revogado_em` -> revogado; `usado_em` -> usado; `expira_em <= now()` -> expirado; senão pendente), nunca uma coluna — assim um convite expira sem job nenhum. `link` só aparece em `pendente`.

## Auto Run Result

Status: done (retomada manual)

A execução automática (`20260910-174359-4f9a`) foi interrompida com a spec em
`in-review`: a implementação estava COMPLETA na árvore de trabalho (nada
commitado), e a sessão morreu antes da fase de revisão. Nada foi descartado.

Consequência a registrar: esta story NÃO passou pela sessão de revisão
independente que a 9.1 e a 9.2 tiveram — a verificação abaixo é a da retomada
manual, não um segundo par de olhos automático.

Verificado direto no código (não só "os testes passaram"):
- **Nominal:** `Cadastrar` resolve o convite com `SELECT ... FOR UPDATE`
  filtrando por `token` E `empresa_id`, e recusa com `ErrConviteEmailDivergente`
  quando o e-mail do formulário não é exatamente o convidado — a transação
  inteira é revertida, então o convite continua pendente.
- **Uso único:** o `UPDATE ... usado_em` roda na MESMA transação do `INSERT` em
  `usuarios`, repetindo as condições do SELECT e exigindo `RowsAffected()==1`;
  é isso que faz dois resgates simultâneos do mesmo token criarem exatamente
  uma conta (há teste de corrida com duas goroutines).
- **Papel:** `'usuario'` é literal no SQL do INSERT, nunca parâmetro.
- **Isolamento:** toda query de `convites_empresa` filtra por `empresa_id`, e as
  4 rotas são registradas por `registrar(` sob `/e/{slug}` (nenhuma
  `mux.HandleFunc` direta).
- **AC 4:** `LoginPage.tsx` não tem campo, seletor nem dropdown de Empresa; a
  tela de cadastro exige `?token=` e não oferece autocadastro aberto.

Verificação executada: `go build ./...`, `go vet ./...` e
`DATABASE_URL=... go test -p 1 ./...` (9 pacotes, 0 falhas); no frontend
`tsc --noEmit`, `oxlint`, `vitest run` (53 arquivos, 644 testes) e
`npm run build`.

Nota sobre um falso negativo: na primeira execução da suíte do frontend,
`CadastroProdutoSection.test.tsx` (arquivo que esta story não toca) estourou o
timeout de 5s por disputa de CPU com a suíte Go rodando em paralelo. Passa
isolado (25/25) e na re-execução sem concorrência.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -p 1 ./...` -- expected: todos os pacotes passam, incluindo `convites_test.go` e a suíte de auth já adaptada.
- `cd frontend && npm run lint && npm run test && npm run build` -- expected: sem erros.
- `grep -rn "convite" backend/main.go` -- expected: as 4 rotas, todas registradas por `registrar(` (nenhuma `mux.HandleFunc` direta).
- `grep -rn "empresa_id" backend/services/convites.go` -- expected: toda query de leitura/escrita de `convites_empresa` filtra por `empresa_id`.
