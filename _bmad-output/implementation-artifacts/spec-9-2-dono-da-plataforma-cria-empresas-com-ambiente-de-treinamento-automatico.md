---
title: 'Story 9.2: Dono da Plataforma cria Empresas com Ambiente de Treinamento automático'
type: 'feature'
created: '2026-09-10'
status: 'done'
baseline_revision: 'af36b307e4a2119eb8d641df4492a2f25e6aa9fe'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Depois da 9.1 existe a fronteira de Empresa, mas nenhuma forma de criar uma: não há identidade "Dono da Plataforma", nem área de gestão de Empresas, nem provisionamento do primeiro `adm`, nem o Ambiente de Treinamento que todo cliente novo deveria ganhar.

**Approach:** Criar a identidade `donos_plataforma` (tabela, sessões e login próprios, fora de `/e/{slug}`, MFA sempre ativa, primeiro registro só por CLI) e a área "Empresas", onde o Dono cria numa única transação a Empresa real, o primeiro `adm` dela, a Empresa-irmã "{Nome Fantasia} - Treinamento" (com dados de exemplo fixos e o mesmo `adm`), lista só metadado e desativa/reativa o par Empresa+Treinamento.

## Boundaries & Constraints

**Always:**
- `donos_plataforma` é disjunta de `usuarios`: tabela, sessões (`sessoes_plataforma`), cookie, rotas (`/api/plataforma/...`, sem `RequireEmpresa`) e middleware próprios. O papel NÃO entra na escala de `RankPapel`.
- A sessão do Dono reaproveita o formato AD-6 (JWT de 30min HS256 com o mesmo `JWT_SECRET` + refresh opaco rotativo de 2h), com o claim `aud = "plataforma"`. `RequireDonoPlataforma` exige esse `aud`; `RequireAuth` (usuarios) recusa com 401 `SESSION_REVOKED` qualquer token que o traga. Nenhuma rota aceita sessão de um tipo no lugar do outro.
- A MFA do Dono não é opcional: toda linha nasce com `mfa_habilitado = true` e `mfa_secret` preenchido (CHECK no banco). O login pede e-mail + senha + código TOTP numa única chamada. Qualquer falha (e-mail inexistente, senha errada, código errado/reusado, conta inativa) → a MESMA resposta 401 `INVALID_CREDENTIALS`, com bcrypt dummy no caminho sem linha (mesma defesa de timing de `services.Login`) e `mfa_ultimo_passo_usado` contra reuso.
- A criação é UMA transação: `ProvisionarEmpresa` (real) → `adm` real → `ProvisionarEmpresa` (treino, `EmpresaOrigemID` = real) → dados de exemplo → `adm` treino → tokens + e-mails. Qualquer falha desfaz tudo.
- O Dono nunca vê nem define a senha do `adm`. O `adm` nasce `papel='adm'`, `email_verificado=true`, `senha_hash NULL`, e recebe no e-mail um link `/e/{slug}/redefinir-senha?token=...` (token `redefinicao_senha` válido por 7 dias, e-mail do tipo novo `primeiro_acesso`). Mesmo caminho para o `adm` do Treinamento (conta separada, mesmo nome/e-mail).
- Os dados de exemplo do Treinamento são fixos no código (nunca lidos da Empresa real): 2 Estoques e 5 Produtos em ao menos 3 Categorias (resolvidas por `codigo` na cópia de `categorias` DO TREINAMENTO), com uma quantidade baixa de propósito (≤ 2) para exercitar o caso de Pedido que ultrapassa o disponível. Todas as linhas levam `empresa_id` do Treinamento. Cadastro não gera `movimentacoes` (AD-10).
- Treinamento: `nome_fantasia` = `"{Nome Fantasia} - Treinamento"`, `slug` = `"{slug}-treinamento"`, mesmos razão social/CNPJ/endereço da real. O slug real é opcional na entrada (padrão `NormalizarSlug(NomeFantasia)`), com no máximo 51 caracteres, para caber o sufixo nos 63. O nome fantasia real tem no máximo 241 caracteres.
- A listagem do Dono devolve SÓ metadado: nome fantasia, razão social, CNPJ, endereço, slug, status, data de criação, `adm` responsável (nome + e-mail) e slug/status do Treinamento. Nenhum service do Dono lê `produtos`, `estoques`, `movimentacoes`, `pedidos`, `logs_acesso` nem tabelas de normalização. A exceção é a gravação dos dados de exemplo na criação.
- Desativar/reativar recebe o id de uma Empresa REAL e altera o status dela e do seu Treinamento na mesma instrução. Desativar também revoga as `sessoes` ativas das contas das duas Empresas. Nenhuma linha é apagada. Um id de Treinamento, inexistente ou malformado dá 404 `NOT_FOUND`.
- Estilo da casa: SQL à mão com `$N`, erros `fmt.Errorf("falha ao ...: %w")` em PT-BR, envelope `{"error":{"code","message"}}`, migration com comentário citando Story/FR/AD e `.down.sql`, `slog` para auditoria (login do Dono, Empresa criada/desativada/reativada).

**Block If:**
- Algum requisito exigir que o Dono veja, edite ou impersone conteúdo operacional de uma Empresa.
- For impossível permitir que o Treinamento compartilhe o CNPJ da Empresa real sem afrouxar a unicidade de CNPJ entre Empresas reais.

**Never:**
- Nenhuma rota HTTP cria linha em `donos_plataforma`. Nenhum `if eh_treinamento` / checagem de `empresa_origem_id` em service de domínio ou em isolamento (AD-23). O único uso desse valor fora da gestão de Empresas é o flag de exibição `ambienteTreinamento` na resposta de sessão.
- Não criar convites (9.3), a migração/backfill da Ferreira Costa nem o `SET NOT NULL` (9.4). Não mexer em `cmd/migrate-legado` nem em `cmd/seed-admin`.
- Não implementar bloqueio de força bruta nem rate limit para o login do Dono. A arquitetura difere isso junto do FR-36 (ver Design Notes).
- Não criar FK de `empresas` para `donos_plataforma` (ver Design Notes: armadilha do TRUNCATE CASCADE).
- Não expor a senha nem o token de primeiro acesso do `adm` em nenhuma resposta ao Dono. Não listar Empresas para o usuário final, nem na página sem slug.
- Não rodar o CLI do Dono no pipeline de deploy: o segredo TOTP precisa ser capturado por uma pessoa.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Bootstrap do Dono | `seed-dono-plataforma --nome N --email E --senha S` com a tabela vazia | 1 linha com bcrypt, `mfa_habilitado=true` e segredo TOTP; imprime o segredo e a URL `otpauth://` | — |
| Bootstrap repetido | já existe um Dono | exit 1 e nada é alterado | `errDonoJaExiste` |
| Senha fraca no CLI | `--senha abc` | exit 1 e nada é gravado | `ErrSenhaFraca` |
| Login do Dono ok | `POST /api/plataforma/auth/login {email,senha,codigo}` corretos | 200 `{token, dono:{id,nome,email}}` + cookie `refresh_token_plataforma` (Path `/api/plataforma/auth`) | — |
| Login do Dono falho | senha, código ou e-mail errados, ou código já usado | 401 com a mesma mensagem | `INVALID_CREDENTIALS` |
| Login do Dono com campo em branco | algum campo vazio | 400 | `VALIDATION_ERROR` |
| Refresh / logout do Dono | cookie válido / qualquer | 200 com novo token e rotação / 204 revogando | cookie inválido → 401 `TOKEN_EXPIRED` + cookie limpo |
| Criar Empresa | Dono autenticado, dados válidos + `admNome`/`admEmail` | 201 com `{empresa, treinamento}`: 2 linhas em `empresas` (treino com `empresa_origem_id`), 2 `adm`, 2 e-mails `primeiro_acesso`, dados de exemplo só no treino, a real com 0 Produtos/Estoques | — |
| CNPJ já usado | CNPJ de outra Empresa real | 409 e nada gravado | `CONFLICT` |
| Slug já usado | slug (ou `{slug}-treinamento`) em uso | 409 e nada gravado | `CONFLICT` |
| Dados inválidos | CNPJ com dígito errado, UF/CEP inválidos, e-mail do adm sem `@`, slug > 51 | 400 com a mensagem do campo | `VALIDATION_ERROR` |
| Token de `usuario` na área do Dono | `GET /api/plataforma/empresas` com o JWT de um `adm` | 401 | `TOKEN_EXPIRED` (sem `aud` plataforma) |
| Token do Dono numa Empresa | `GET /e/{slug}/api/usuarios` com o JWT do Dono | 401 | `SESSION_REVOKED` |
| Listar Empresas | Dono autenticado | 200 `{empresas:[...]}` só com metadado; o Treinamento aparece aninhado à real | — |
| Desativar | `POST /api/plataforma/empresas/{id}/desativacao` com id real | 200; real e treino `inativa`; sessões revogadas; nenhuma linha apagada | id de treino/inexistente/malformado → 404 `NOT_FOUND` |
| Login em Empresa desativada | `POST /e/{slug}/api/auth/login` | 404 (RequireEmpresa); Produtos/Estoques continuam no banco | `NOT_FOUND` |
| Reativar | `POST .../{id}/reativacao` | 200; as duas `ativa`, o slug volta a resolver | mesmos 404 |
| Sessão no Treinamento | `/e/{slug}-treinamento/api/auth/me` | `ambienteTreinamento: true`, `empresaNome` = nome do treino | — |

</intent-contract>

## Code Map

**Fundação da 9.1 a reutilizar (não reescrever)**
- `backend/services/empresas.go:321` `ProvisionarEmpresa(tx, DadosEmpresa)`: valida, insere e copia `categorias`/`nomenclatura_templates` padrão. Mapeia `empresas_cnpj_unico` → `ErrCNPJDuplicado` e `empresas_slug_unico` → `ErrSlugDuplicado` pelo nome da constraint (`:343-346`). O índice novo PRECISA manter o nome `empresas_cnpj_unico`. `:169` `NormalizarSlug`, `:135` `ValidarCNPJ`, `:54` `ErroEmpresaValidacao`, `:190` `colunasEmpresa`/`scanEmpresa`. `:221` `BuscarEmpresaPorSlug` já filtra `status='ativa'`, então desativar derruba login e todas as rotas sob o slug sem nenhum código novo.
- `backend/migrations/000031_create_empresas.up.sql`: `CONSTRAINT empresas_cnpj_unico UNIQUE (cnpj)` (a trocar), `empresa_origem_id`, `criado_em`. A maior migration atual é a `000032`.
- `backend/middleware/empresa.go:29` `empresaCtxKey ctxKey = 1` → a chave nova do Dono é `2`. `:69` `EmpresaDaRequisicao`.
- `backend/middleware/auth.go:55` `RequireAuth`: faz o parse de `services.AcessoClaims` (que embute `jwt.RegisteredClaims`, então `Audience` já decodifica). O ponto de recusa do `aud` plataforma é logo depois de `:77`. `:35-48` tem uma cópia local de `escreverErro`.
- `backend/services/auth.go:57-60` TTLs (`accessTokenExpiracao`, `RefreshTokenExpiracao`); `:147` `gerarTokenAcao`; `:324` `dummyBcryptHash`; `:517` `gerarAccessToken` (molde; não alterar os claims do token de `usuarios`); `:541` `EmitirSessao` e `:573` `RenovarSessao` (molde de rotação para `sessoes_plataforma`); `:860` `ValidarForcaSenha`; `:813` `ConfirmarConfiguracaoMFA` (padrão `mfa_ultimo_passo_usado`); `:1004` `RedefinirSenha`, que é o que o `adm` consome no primeiro acesso (seta a senha e revoga sessões; não muda `email_verificado`, por isso o `adm` já nasce verificado).
- `backend/services/totp.go`: `GerarSegredoTOTP`, `URLProvisionamentoTOTP(email, segredo)`, `ValidarCodigoTOTP`, `PassoAtualTOTP`.
- `backend/services/email.go:45` `LinkDaEmpresa(appURL, slug, caminho, token)`; `:80` `EnfileirarEmail(tx, destinatario, usuarioID, tipo, variaveis)`; `:106` `renderizarTemplate`, onde entra o `case "primeiro_acesso"` com as variáveis `nome`, `empresa` e `link`, citando "expira em 7 dias" e o "Esqueci minha senha".
- `backend/migrations/000002_create_outbox_e_tokens.up.sql`: o CHECK de `emails_pendentes.tipo` é `emails_pendentes_tipo_check` (`'verificacao_conta','redefinicao_senha'`), a estender. `tokens_acao` já aceita `redefinicao_senha`.
- `backend/services/produtos.go:196` `CriarProduto`: forma do INSERT em `produtos` (colunas + `empresa_id`) e `produto_estoque (produto_id, estoque_id, quantidade)`. Recebe `*sql.DB`, então os dados de exemplo usam SQL próprio dentro da `tx`. `estoques(nome, empresa_id)` tem `nome_normalizado` gerado.
- Categorias padrão (000010): `04.001` Materiais Civis, `04.002` Materiais Elétricos, `05.001` EPI/EPC, `10.001` Ferramentas Adquiridas (Consumo), entre outras.

**Fronteira HTTP**
- `backend/main.go:298` `newMux`: as rotas do Dono vão com `mux.HandleFunc` direto (NÃO `registrar`), ao lado de `GET /api/health` (`:325`).
- `backend/handlers/auth.go:32-42` `escreverErro`/`escreverJSON`; `:189-207` `usuarioResposta`/`usuarioRespostaDe` (ganha `empresaNome`/`ambienteTreinamento` a partir de `middleware.EmpresaDaRequisicao(r.Context())`; chamado em `:253`, `:546` e via `emitirSessaoEResponder` no SSO); `:214` `cookieEhSeguro`; `:221-273` o molde dos cookies de refresh.
- `backend/handlers/auth_sso.go:119` `LogoutHandler`: molde do logout idempotente.
- `backend/Dockerfile`: compila `api` e `seed-admin`; acrescentar `seed-dono-plataforma`.
- `backend/cmd/seed-admin/main.go`: molde de CLI (flags, `godotenv`, `DATABASE_URL`, mensagens em PT-BR, `os.Exit(1)`).
- `.github/workflows/deploy-cliente-aws.yml:167` roda o `seed-admin` automaticamente. O CLI do Dono NÃO entra aqui.

**Testes backend**
- `DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable` (Postgres local aceitando conexões); rodar com `go test -p 1 ./...`.
- Helpers: `services/empresa_teste_test.go` (`criarEmpresaDeTeste`, `cnpjDeTeste`, `removerEmpresaDeTeste`, que só apaga categorias/templates e a Empresa); `handlers/empresa_teste_test.go` (`comEmpresa`, `prefixoEmpresaTeste`); `main_test.go:66` `seedContaMux`, `:96` `tokenDeMux`, `:40` `totpCodigoTesteAtual`, `:~310` a tabela de rotas; `middleware/auth_test.go:~95` `gerarAccessTokenTeste`.
- ARMADILHA: o banco é compartilhado entre pacotes e `empresas` nunca é truncada. Uma Empresa criada por teste precisa ser removida com TODAS as linhas que existem por causa dela (`produto_estoque`, `produtos`, `estoques`, `usuarios`, `categorias`, `nomenclatura_templates`, depois `empresas`, treino antes da real) via `DELETE`, nunca com `TRUNCATE ... CASCADE` sobre tabela referenciada. `donos_plataforma` é limpa com `DELETE` (cascata para `sessoes_plataforma`).

**Frontend**
- `frontend/src/main.tsx` renderiza `<App/>` sempre. Ponto de escolha entre app da Empresa, app da Plataforma e página sem Empresa.
- `frontend/src/App.tsx:110` `router` com `basename: prefixoEmpresa()`. Os testes importam `App`/`router` diretamente (nunca `main.tsx`), então a escolha no `main.tsx` não quebra os 48 testes.
- `frontend/src/lib/api.ts`: `slugDaURL`, `prefixoEmpresa`, `apiUrl`, `authHeaders`. `lib/session.ts` guarda o token de `usuarios` em memória; o Dono tem guarda PRÓPRIA.
- `frontend/src/lib/auth.tsx:32` `UsuarioSessao` (ganha `empresaNome?`/`ambienteTreinamento?`). `src/components/shell/AppShell.tsx:147`: o header em `:219` é onde entra a faixa do Treinamento. `AppShell.test.tsx` mocka um usuário sem esses campos, então o layout atual continua intacto.
- `frontend/src/pages/LoginPage.tsx:22` `mensagemDeErro` ganha o `NOT_FOUND` (Empresa inexistente/desativada).
- Reuso de UI: `components/ConfirmDialog.tsx` (`confirmVariant="destructive"`), `components/ui/{card,input,label,button}`, `sonner` (toast), token `--color-warning`/`text-on-tint-warning` (`src/index.css:28,31`), `min-h-touch-target-min`. Molde de página de formulário: `pages/LoginPage.tsx`/`CadastroPage.tsx`. Molde de teste: `pages/LoginPage.test.tsx` (roteador de `fetch` por URL + `MemoryRouter`).

## Tasks & Acceptance

**Execution:**

*Schema*
- `backend/migrations/000033_create_donos_plataforma.{up,down}.sql` -- criar:
  - `donos_plataforma` (id UUID, nome VARCHAR(255), email VARCHAR(255), senha_hash TEXT NOT NULL, `mfa_habilitado BOOLEAN NOT NULL DEFAULT true CHECK (mfa_habilitado)`, mfa_secret TEXT NOT NULL, mfa_ultimo_passo_usado BIGINT, ativo BOOLEAN DEFAULT true, criado_em) + `UNIQUE INDEX (lower(email))`;
  - `sessoes_plataforma` (id, `dono_id REFERENCES donos_plataforma ON DELETE CASCADE`, refresh_token TEXT UNIQUE, expira_em, revogado_em, criado_em) + índice em `dono_id`;
  - trocar a `CONSTRAINT empresas_cnpj_unico` por `CREATE UNIQUE INDEX empresas_cnpj_unico ON empresas (cnpj) WHERE empresa_origem_id IS NULL` e acrescentar `UNIQUE INDEX empresas_treinamento_unico ON empresas (empresa_origem_id) WHERE empresa_origem_id IS NOT NULL`;
  - estender `emails_pendentes_tipo_check` com `'primeiro_acesso'`.

  -- Justificativa: identidade disjunta (AD-21), MFA estrutural, Treinamento compartilhando o CNPJ da origem sem afrouxar a unicidade entre Empresas reais, e no máximo um Treinamento por Empresa.

*Services*
- `backend/services/plataforma.go` -- novo:
  - `AudienciaPlataforma = "plataforma"`, `PlataformaClaims` e `DonoSessao{ID,Nome,Email,Ativo}`;
  - `LoginDonoPlataforma(db, email, senha, codigo) (string, error)`: `ErrLoginValidacao` ou `ErrCredenciaisInvalidas`, com bcrypt dummy e `mfa_ultimo_passo_usado` gravado atomicamente;
  - `EmitirSessaoPlataforma`, `RenovarSessaoPlataforma` (rotação em transação, molde de `RenovarSessao`) e `RevogarSessaoPlataforma`;
  - `BuscarDonoSessao` (`ErrDonoSessaoNaoEncontrado`);
  - `CriarPrimeiroDonoPlataforma(db, nome, email, senha) (id, segredo string, err error)`, que valida a força da senha, gera o TOTP e faz `LOCK TABLE donos_plataforma IN EXCLUSIVE MODE` + `NOT EXISTS` numa transação (`ErrDonoJaExiste`).

  -- Justificativa: formato AD-6 com sujeito e audiência próprios, e o CLI usa a mesma regra do service.
- `backend/services/empresas_plataforma.go` -- novo:
  - `NovaEmpresaInput{DadosEmpresa, AdmNome, AdmEmail}`;
  - `CriarEmpresaComTreinamento(db, emailCfg, input) (Empresa, Empresa, error)`: a transação única descrita em Always, com `semearDadosTreinamento(tx, empresaID)` (fixture fixa) e `provisionarAdmPrimeiroAcesso(tx, emailCfg, empresa, nome, email)` (usuario + token de 7 dias + e-mail `primeiro_acesso`); validar `AdmNome`/`AdmEmail` (não vazios, ≤255, e-mail com `@`), slug ≤51 e nome ≤241 ANTES da transação;
  - `ListarEmpresasPlataforma(db) ([]EmpresaResumo, error)` (reais, com `LEFT JOIN` do treino e `LATERAL` do `adm`, ordenadas por `criado_em DESC`);
  - `AlterarStatusEmpresa(db, empresaID, status) error` (`UPDATE ... WHERE (id=$1 AND empresa_origem_id IS NULL) OR empresa_origem_id=$1`, 0 linhas ou 22P02 → `ErrEmpresaNaoEncontrada`; ao desativar, revoga `sessoes` das contas das duas Empresas na mesma transação).

  -- Justificativa: FR-41/FR-43 reutilizando `ProvisionarEmpresa`, sem segundo mecanismo de isolamento.
- `backend/services/email.go` -- `case "primeiro_acesso"` em `renderizarTemplate` (assunto "Seu acesso de administrador — stockflow"; corpo com nome, Empresa, link, validade de 7 dias e "Esqueci minha senha" como plano B; `html.EscapeString` em nome/empresa). -- Justificativa: o outbox recusa tipo sem template.

*Middleware e handlers*
- `backend/middleware/plataforma.go` -- novo: `RequireDonoPlataforma(db, jwtSecret)` (Bearer; parse de `PlataformaClaims` com `jwt.WithAudience(services.AudienciaPlataforma)`; falha → 401 `TOKEN_EXPIRED`; dono ausente/inativo → 401 `SESSION_REVOKED`; injeta com a chave `2`) e `DonoDaSessao(ctx)`. -- Justificativa: a sessão de `usuarios` jamais passa.
- `backend/middleware/auth.go` -- em `RequireAuth`, token cujo `Audience` contenha `services.AudienciaPlataforma` → 401 `SESSION_REVOKED`, antes de consultar `usuarios`. -- Justificativa: separação explícita, não só pela ausência do id.
- `backend/handlers/plataforma_auth.go` -- novo: `PlataformaLoginHandler`, `PlataformaRefreshHandler`, `PlataformaLogoutHandler` (204 idempotente) e `PlataformaMeHandler`. Cookie `refresh_token_plataforma`, Path `/api/plataforma/auth`, HttpOnly, SameSite=Lax, Secure por `cookieEhSeguro`. Corpo limitado a 64KiB. -- Justificativa: rota de login própria (AD-21).
- `backend/handlers/empresas.go` -- novo: `CriarEmpresaHandler` (201 `{empresa, treinamento}`; `ErroEmpresaValidacao` → 400; CNPJ/slug duplicado → 409 `CONFLICT` com mensagem que nomeia qual), `ListarEmpresasHandler` (200 `{empresas}`), `DesativarEmpresaHandler`/`ReativarEmpresaHandler` (200; `ErrEmpresaNaoEncontrada` → 404). Nenhuma resposta traz token nem senha do `adm`. -- Justificativa: a superfície da área "Empresas".
- `backend/handlers/auth.go` -- `usuarioResposta` ganha `EmpresaNome string json:"empresaNome"` e `AmbienteTreinamento bool json:"ambienteTreinamento"` (`EmpresaOrigemID != nil`), preenchidos a partir da Empresa do contexto em login, MFA, SSO e `/me`. -- Justificativa: o frontend precisa saber que está no Treinamento.
- `backend/main.go` -- registrar sem `RequireEmpresa`:
  - `POST /api/plataforma/auth/login`, `POST /api/plataforma/auth/refresh`, `POST /api/plataforma/auth/logout`;
  - `GET /api/plataforma/auth/me`, `GET|POST /api/plataforma/empresas`, `POST /api/plataforma/empresas/{id}/desativacao` e `/reativacao`, os cinco atrás de `RequireDonoPlataforma`.

  -- Justificativa: rotas fora do prefixo de Empresa.

*CLI e build*
- `backend/cmd/seed-dono-plataforma/main.go` -- novo: flags `--nome/--email/--senha` (obrigatórias); chama `services.CriarPrimeiroDonoPlataforma`; no sucesso imprime em stdout o id, o e-mail, o segredo TOTP e a URL `otpauth://`, com a instrução de cadastrá-los no autenticador agora (não serão exibidos de novo); `ErrDonoJaExiste`/`ErrSenhaFraca` → mensagem PT-BR + exit 1. -- Justificativa: bootstrap por CLI (AD-21/AD-12).
- `backend/Dockerfile` -- compilar e copiar `seed-dono-plataforma`. -- Justificativa: o operador roda o binário dentro do container `api`.

*Frontend*
- `frontend/src/lib/entrada.ts` -- novo: `escolherApp(pathname): 'plataforma' | 'empresa' | 'sem-empresa'` (`/plataforma` ou `/plataforma/...` → plataforma; `slugDaURL` não vazio → empresa; senão → sem-empresa). -- Justificativa: separação total entre as duas apps no cliente (handoff (b) da 9.1).
- `frontend/src/main.tsx` -- renderizar `<PlataformaApp/>`, `<App/>` ou `<SemEmpresaPage/>` conforme `escolherApp(window.location.pathname)`, mantendo `<Toaster/>`. -- Justificativa: a app da Empresa nunca sobe sem slug e a da Plataforma nunca carrega `AuthProvider`/`CarrinhoProvider`.
- `frontend/src/pages/SemEmpresaPage.tsx` -- novo: Card explicando que o acesso é pelo endereço da própria empresa (`.../e/nome-da-empresa`), sem listar Empresas nem linkar a Plataforma. -- Justificativa: substitui um login quebrado.
- `frontend/src/lib/plataforma.ts` -- novo:
  - token do Dono em memória (módulo próprio);
  - `loginPlataforma`, `renovarSessaoPlataforma`, `logoutPlataforma`, `buscarDono`;
  - `listarEmpresas`, `criarEmpresa`, `desativarEmpresa`, `reativarEmpresa`, todas para `/api/plataforma/...` e SEM `apiUrl` (nunca prefixar);
  - `ErroPlataforma{codigo,mensagem}`, com tipos `EmpresaResumo`/`NovaEmpresa`.

  -- Justificativa: cliente isolado da sessão de `usuarios`.
- `frontend/src/pages/plataforma/PlataformaApp.tsx` -- novo: router próprio com `/plataforma/login` (`PlataformaLoginPage`) e `/plataforma` (`EmpresasPage` atrás de um guard que tenta `renovarSessaoPlataforma` ao montar: carregando → "Carregando...", sem sessão → `Navigate` para `/plataforma/login`). -- Justificativa: área administrativa fora da navegação de qualquer Empresa.
- `frontend/src/pages/plataforma/PlataformaLoginPage.tsx` -- novo: um form com E-mail, Senha e Código de verificação (6 dígitos numéricos, `autocomplete="one-time-code"`); sucesso → `navigate('/plataforma')`; `INVALID_CREDENTIALS` → "E-mail, senha ou código inválidos."; `VALIDATION_ERROR` → "Preencha e-mail, senha e código." -- Justificativa: MFA sem exceção desde o primeiro login.
- `frontend/src/pages/plataforma/EmpresasPage.tsx` -- novo:
  - cabeçalho "Plataforma — Empresas" + "Sair" (logout → `/plataforma/login`);
  - Card "Nova Empresa" com Nome Fantasia, Razão Social, CNPJ, Endereço de acesso (slug, pré-preenchido a partir do Nome Fantasia até ser editado à mão), Logradouro, Número, Complemento, Bairro, Cidade, CEP, UF, Nome e E-mail do primeiro administrador; sucesso → toast "Empresa criada. O administrador receberá um e-mail para definir a senha." + recarregar a lista; erro 400/409 → a mensagem do servidor num `role="alert"`;
  - lista com os campos de metadado, `/e/{slug}` e `/e/{slug}-treinamento`, badge de status e botão Desativar (`ConfirmDialog` destrutivo, explicando que ninguém das duas Empresas consegue entrar e nada é apagado) / Reativar;
  - "Carregando..." (`<output>`) enquanto carrega, porque não existe `Skeleton` em `components/ui`; nenhum dado operacional.

  -- Justificativa: AC 2, 4 e 5 pela interface.
- `frontend/src/lib/auth.tsx` -- `UsuarioSessao` ganha `empresaNome?: string` e `ambienteTreinamento?: boolean`. -- Justificativa: espelhar a resposta de sessão.
- `frontend/src/components/shell/AppShell.tsx` -- quando `usuario?.ambienteTreinamento`: faixa persistente no topo em TODAS as larguras (fundo `--color-warning`, texto escuro, ícone, "AMBIENTE DE TREINAMENTO — dados de exemplo, nada aqui afeta a operação real"), borda grossa na cor warning em volta do shell e header "stockflow · Treinamento". Sem alterar nada quando o flag está ausente. -- Justificativa: distinção difícil de ignorar (UX do épico).
- `frontend/src/pages/LoginPage.tsx` -- `NOT_FOUND` → "Este endereço de acesso não está disponível. Confira o endereço com o administrador da sua empresa." -- Justificativa: login em Empresa desativada é recusado com mensagem honesta, não "tente em instantes".

*Testes*
- `backend/services/plataforma_test.go` -- login do Dono (ok; senha, código e e-mail errados; código reusado; inativo), rotação e revogação de sessão, `CriarPrimeiroDonoPlataforma` (cria, recusa o segundo, recusa senha fraca, grava o segredo válido). -- Justificativa: linhas 1-5 da I/O Matrix.
- `backend/services/empresas_plataforma_test.go` -- criação completa (2 Empresas, origem, CNPJ igual, 2 `adm` com `senha_hash NULL` e `email_verificado`, 2 e-mails `primeiro_acesso` com o link do slug certo, token de 7 dias, 5 Produtos/2 Estoques/≥3 Categorias só no treino e 0 na real, quantidade ≤2 presente); CNPJ duplicado e slug duplicado → nada gravado; validações; listagem só com metadado; desativar/reativar o par, 404 para id de treino/inexistente/malformado, sessões revogadas, dados preservados. Cada teste remove as Empresas que criou (ver a ARMADILHA no Code Map). -- Justificativa: AC 2, 3, 4 e 5 no service.
- `backend/middleware/plataforma_test.go` -- `RequireDonoPlataforma`: token do Dono ok; token de `usuarios` (sem `aud`) → 401; expirado → 401; dono inativo/inexistente → 401. `RequireAuth` com token `aud=plataforma` → 401 `SESSION_REVOKED`. -- Justificativa: AC 6.
- `backend/handlers/plataforma_test.go` -- login/refresh/logout (cookie e Path), `/empresas` POST 201/400/409 e GET, desativação 200/404, sem token → 401. -- Justificativa: a fronteira HTTP.
- `backend/main_test.go` -- as rotas novas registradas no mux real; ponta a ponta: o Dono cria a Empresa, desativa, e o login em `/e/{slug}/api/auth/login` dá 404 com os dados ainda no banco; o token do Dono em `/e/{slug}/api/auth/me` → 401; `/me` no treino devolve `ambienteTreinamento:true`. -- Justificativa: AC 5 e 6 pela composição real.
- `backend/cmd/seed-dono-plataforma/main_test.go` -- primeira execução cria; a segunda recusa sem alterar; flags vazias falham. -- Justificativa: AC 1.
- `frontend/src/lib/entrada.test.ts`, `src/lib/plataforma.test.ts`, `src/pages/SemEmpresaPage.test.tsx`, `src/pages/plataforma/PlataformaLoginPage.test.tsx`, `src/pages/plataforma/EmpresasPage.test.tsx` (criar com sucesso/409, lista de metadado, desativar via diálogo), `AppShell.test.tsx` (faixa presente só com `ambienteTreinamento`) e `LoginPage.test.tsx` (`NOT_FOUND`). -- Justificativa: a superfície do cliente.

**Acceptance Criteria:**
- Given um banco sem nenhum Dono, when o operador roda `seed-dono-plataforma` com nome/e-mail/senha válidos, then a primeira linha de `donos_plataforma` é criada com MFA ativa e o segredo é exibido uma única vez; e nenhuma rota do mux real cria linha nessa tabela.
- Given um Dono autenticado em `/plataforma`, when ele preenche o formulário "Nova Empresa" e envia, then a Empresa real, o `adm` dela, a Empresa "{Nome Fantasia} - Treinamento" (com `empresa_origem_id` apontando para a real e dados de exemplo próprios) e o `adm` do Treinamento passam a existir, e o `adm` recebe o link de definição de senha sob o slug de cada Empresa.
- Given a lista da área "Empresas", when o Dono a visualiza, then vê só nome fantasia, razão social, CNPJ, endereço, status, `adm` responsável, data de criação e endereços de acesso, e nenhum endpoint `/api/plataforma/*` devolve Catálogo, Estoque, Pedido ou Log de Acesso.
- Given uma Empresa desativada pelo Dono, when qualquer conta dela (ou do seu Treinamento) tenta entrar, then o login é recusado, a tela mostra a mensagem de endereço indisponível e todas as linhas de domínio continuam no banco.
- Given o backend e o frontend desta story, when `go build ./...`, `go vet ./...`, `go test -p 1 ./...` (com `DATABASE_URL`), `npm run lint`, `npm run test` e `npm run build` rodam, then todos passam.

## Spec Change Log

## Review Triage Log

## Design Notes

**Por que o índice parcial de CNPJ.** O AC pede CNPJ único na plataforma e também pede que o Treinamento seja "uma Empresa comum". `empresas.cnpj` é NOT NULL e o Treinamento é legalmente o mesmo cliente, então herdar o CNPJ da origem é o dado verdadeiro. `UNIQUE (cnpj) WHERE empresa_origem_id IS NULL` mantém exatamente a regra "uma Empresa real por CNPJ" (409 em duplicata) e deixa o Treinamento compartilhar o valor. `empresas_treinamento_unico` impede um segundo Treinamento para a mesma Empresa.

**Por que o Dono não define a senha do `adm`.** Se o Dono soubesse a credencial, poderia entrar como `adm` e ver o Catálogo, o que contradiz FR-41 ("só metadado") e a separação estrutural Dono ≠ `adm`. Por isso a conta nasce sem senha, com e-mail já verificado (a posse do e-mail é provada ao consumir o link), e o link reutiliza o fluxo de redefinição da Story 1.6 (`RedefinirSenhaPage` já existe sob o slug). Sete dias porque é um convite de primeiro acesso, não uma recuperação. Se o link expirar, o "Esqueci minha senha" da tela de login cobre sem ação do Dono.

**Por que o `adm` também no Treinamento.** Sem nenhuma conta, o Treinamento nasceria inalcançável: convites (9.3) são emitidos por `adm`/`gestor` da própria Empresa. Mesmo nome e e-mail, conta separada (e-mail é único POR Empresa).

**Login do Dono em uma etapa.** Como a MFA é estrutural, não existe login do Dono sem código. Pedir os três campos juntos elimina o token intermediário (que em `usuarios` vive em `tokens_acao`, com FK para `usuarios`) e não revela qual fator falhou. Força bruta e rate limit ficam com o item diferido de FR-36/AD-21 (Deferred da arquitetura). Até lá, a MFA obrigatória e o bcrypt são as defesas.

**Sem FK `empresas → donos_plataforma`.** Os testes limpam tabelas de identidade com `TRUNCATE ... CASCADE`. Uma FK de `empresas` para `donos_plataforma` faria esse CASCADE apagar `empresas` e, em cadeia, o seed de `categorias`/`nomenclatura_templates` que nenhuma migration recria (o mesmo incidente registrado na spec-9-1). A autoria da criação fica no `slog`.

**Desativação em par.** O Dono gerencia clientes, não linhas: desativar um cliente e deixar o Treinamento dele no ar seria a surpresa pior. A instrução é um `UPDATE` sobre `empresas` na gestão de Empresas, não um `if eh_treinamento` em código de isolamento. O efeito "login recusado" já vem de `BuscarEmpresaPorSlug` (9.1), e revogar `sessoes` só adianta o corte das sessões em voo.

**Frontend em três apps.** `main.tsx` decide antes de montar qualquer provider. Assim a app da Plataforma nunca faz `refresh` de `usuarios` nem busca carrinho, a app da Empresa nunca sobe sem slug, e os 48 testes existentes (que importam `App` e rodam em `/`) ficam intocados.

**Dados de exemplo (golden):**
```
Estoques: "Almoxarifado Central", "Canteiro de Obras"
Produtos (categoria por código na cópia do Treinamento):
  Cimento CP II 50kg          04.001  Almoxarifado Central 40
  Cabo flexível 2,5mm 100m    04.002  Almoxarifado Central 6
  Capacete de segurança       05.001  Canteiro de Obras 15
  Luva de raspa (par)         05.001  Canteiro de Obras 2   <- estoque baixo
  Trena 5m                    10.001  Almoxarifado Central 8
```

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -p 1 ./...` -- expected: todos os pacotes passam, incluindo os testes novos de plataforma e empresas.
- `cd frontend && npm run lint && npm run test && npm run build` -- expected: sem erros.
- `grep -rn "RequireEmpresa\|registrar(" backend/main.go | grep plataforma` -- expected: nenhum resultado (rotas do Dono fora do prefixo de Empresa).

## Auto Run Result

Status: done (retomada manual)

A execução automática (`20260910-160254-c1a1`) parou duas vezes no limite de sessão
da API. O primeiro registro escrito aqui dizia que "nenhuma linha de produção ou de
teste foi escrita" e ficou DESATUALIZADO: depois dele a sessão voltou a rodar,
implementou a story inteira e foi interrompida na fase de verificação, deixando o
código na árvore de trabalho (não commitado) e a spec ainda em `blocked`. O commit
`0c272c3` carregou apenas a spec. Esta passagem foi a retomada manual: nada foi
descartado, o trabalho foi verificado de ponta a ponta e a story fechada.

Entregue (conforme Tasks & Acceptance, sem desvio):
- Schema `000033`: `donos_plataforma` (com `CHECK (mfa_habilitado)` — não existe
  Dono sem segundo fator) e `sessoes_plataforma`; `empresas_cnpj_unico` passa a
  valer só entre Empresas reais, e `empresas_treinamento_unico` garante no máximo
  um Treinamento por Empresa; `emails_pendentes` aceita `primeiro_acesso`.
- Services `plataforma.go` (login com MFA, rotação de sessão, bootstrap) e
  `empresas_plataforma.go` (Empresa + Treinamento + os dois `adm` em UMA
  transação, listagem só de metadado, desativação em par).
- `RequireDonoPlataforma` e a recusa explícita, em `RequireAuth`, de um token com
  `aud=plataforma`; handlers de login e de Empresas; rotas `/api/plataforma/*`
  registradas FORA de `RequireEmpresa`.
- CLI `seed-dono-plataforma` (única forma de criar o primeiro Dono) e Dockerfile.
- Frontend: `escolherApp` separando as três entradas, app própria da Plataforma
  (login + Empresas), `SemEmpresaPage`, e a faixa persistente de Ambiente de
  Treinamento no shell.

Verificação independente desta passagem (não só "os testes passaram"): as
invariantes de AD-21/AD-23 foram conferidas direto no código — MFA obrigatório
no banco, identidade disjunta de `usuarios`, bootstrap inexistente por HTTP,
listagem sem nenhuma coluna operacional, ausência total de `if eh_treinamento`
(`grep` vazio) e desativação atingindo Empresa e Treinamento na mesma transação.

