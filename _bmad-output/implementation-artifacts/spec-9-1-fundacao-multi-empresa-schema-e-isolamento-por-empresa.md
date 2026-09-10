---
title: 'Story 9.1: Fundação Multi-Empresa — schema e isolamento por Empresa'
type: 'feature'
created: '2026-09-10'
status: 'done'
baseline_revision: '8b1599876d8bfa17584b46695ddc104d66a1ccbd'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** O stockflow é uma instalação única: não existe conceito de Empresa, toda unicidade é global (um só `adm`, um só e-mail, um só `codigo` de Produto na base inteira) e ~160 queries de service leem/escrevem tabelas de domínio sem nenhum filtro de posse — qualquer conta enxerga todo o Catálogo, Estoque, Movimentação, Pedido e Log de Acesso do banco.

**Approach:** Introduzir a Empresa como fronteira de isolamento: tabela `empresas`, coluna `empresa_id` em toda tabela de domínio, unicidades reescopadas por Empresa, prefixo de rota `/e/{slug}/api/...` com a Empresa resolvida **uma única vez** em middleware e injetada no contexto, e `empresa_id` passado como argumento explícito para cada função de service, que passa a filtrar por ele em toda query.

## Boundaries & Constraints

**Always:**
- `empresa_id` é resolvido SÓ do slug da URL, no middleware, uma vez por requisição. Nenhum service o re-deriva; nenhum handler o aceita de body, query ou header.
- Services recebem `empresaID` como **parâmetro explícito** — nunca via `context.Context` (a camada `services/` não importa `net/http` nem `context`; AD-8 forma 3). Regra vigente e inegociável.
- Toda coluna `empresa_id` nasce **NULLABLE** nesta story (fase 1 aditiva de AD-23). O `SET NOT NULL` é da Story 9.4, depois do backfill.
- Índices únicos hoje globais viram compostos com `empresa_id` usando `NULLS NOT DISTINCT` (Postgres 15), para que as linhas legadas com `empresa_id IS NULL` continuem sujeitas à unicidade global de hoje durante a fase 1.
- "Pertence a outra Empresa" colapsa no MESMO sentinela de "não encontrado" já usado para UUID malformado (`ErrProdutoNaoEncontrado`, `ErrPedidoNaoEncontrado`, ...) — nunca 403, nunca revelar existência.
- Slug desconhecido ou Empresa com `status='inativa'` → `404 NOT_FOUND`. Sessão cujo `usuarios.empresa_id` diverge da Empresa da URL → `401 SESSION_REVOKED`.
- Estilo da casa: SQL escrito à mão com `$N`, `const` local antes da chamada, erros `fmt.Errorf("falha ao ...: %w", err)` em PT-BR, migrations com bloco de comentário citando Story/FR/AD, `.down.sql` sempre presente.

**Block If:**
- Um requisito exigir que a Empresa seja escolhida pelo usuário (dropdown/seletor) em vez de resolvida pelo contexto de acesso.
- Não for possível manter alguma unicidade global hoje existente durante a fase 1 sem violar dado já gravado.

**Never:**
- Não criar nenhuma linha em `empresas` por migration nem por seed automático. A Empresa "Ferreira Costa" **não pode existir** antes da Story 9.4 (AC explícito dela); a criação pela UI é da Story 9.2.
- Não aplicar `SET NOT NULL`, não fazer backfill de dado existente, não criar `donos_plataforma`, `convites_empresa`, Ambiente de Treinamento nem tela de Empresas — 9.2/9.3/9.4.
- Não usar Row-Level Security, schema-por-tenant nem `SET LOCAL current_setting` — AD-20 manda filtro explícito no service.
- Não migrar `cmd/migrate-legado` (15 INSERTs próprios) — pertence à Story 9.4, que é dona do corte de dados da Ferreira Costa. `empresa_id` nullable mantém o binário compilando.
- Não tornar `tokens_acao.token` nem `sessoes.refresh_token` por Empresa — são segredos opacos e continuam globalmente únicos.
- Não criar tela/rota de listagem de Empresas para o usuário final (vazaria a existência de outros clientes).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Slug válido, sessão da mesma Empresa | `GET /e/acme/api/estoques`, token de conta com `empresa_id`=Acme | 200 com apenas Estoques da Acme | — |
| Slug válido, sessão de outra Empresa | `GET /e/acme/api/estoques`, token de conta da Beta | 401 | `SESSION_REVOKED` |
| Slug inexistente | `GET /e/nao-existe/api/estoques` | 404 | `NOT_FOUND` (nunca 403) |
| Empresa inativa | slug de Empresa com `status='inativa'` | 404 em toda rota sob o prefixo, inclusive login | `NOT_FOUND` |
| Recurso de outra Empresa por id | `GET /e/acme/api/produtos/{id-da-beta}` | 404 | `ErrProdutoNaoEncontrado` → `NOT_FOUND` |
| Escrita cruzada por id | `POST /e/acme/api/produtos/{id-da-beta}/renomear` | 404, nenhuma linha alterada | `NOT_FOUND` |
| Login com e-mail existente em duas Empresas | `POST /e/acme/api/auth/login` com e-mail que existe em Acme e Beta | autentica a conta da Acme, nunca a da Beta | — |
| Mesmo e-mail cadastrado em duas Empresas | `POST /e/beta/api/auth/cadastro` com e-mail já usado na Acme | 201, conta criada na Beta | — |
| E-mail repetido na mesma Empresa | `POST /e/acme/api/auth/cadastro` com e-mail já usado na Acme | comportamento atual (resposta genérica antienumeração) | — |
| Dois `adm` em Empresas distintas | seed/promoção cria `adm` na Acme e na Beta | ambos existem | — |
| Dois `adm` na mesma Empresa | promoção tentando segundo `adm` na Acme | recusado | 23505 em `idx_usuarios_unico_adm` |
| Estoque de mesmo nome em Empresas distintas | `POST /e/beta/api/estoques` com nome já usado na Acme | 201 | — |
| Produto de mesmo `codigo` em Empresas distintas | importação da Beta com `codigo` já usado na Acme | cria produto novo na Beta (não atualiza o da Acme) | — |
| Duplicatas cruzando Empresas | Acme e Beta com Produto de mesmo nome/dimensão/estoque | grupos nunca misturam Empresas | — |
| Mesclagem com id de outra Empresa | `POST /e/acme/api/normalizacao/mesclar` incluindo produto da Beta | recusado, nada escrito | `ErroMesclagemInvalida` |
| SSE | stream aberto em `/e/acme/...` | recebe só eventos publicados para a Acme | — |
| Health check | `GET /api/health` (sem prefixo) | 200 | — |
| Frontend sem slug na URL | app servido em `/` | `apiUrl()` devolve o caminho sem prefixo e o router usa `basename` vazio (preserva os testes atuais) | — |

</intent-contract>

## Code Map

**Schema (`backend/migrations/`, maior atual `000030`)**
- `000001_create_usuarios.up.sql:12` `idx_usuarios_email_lower`; `:17` `idx_usuarios_unico_adm ON usuarios (papel) WHERE papel='adm'` — os dois bloqueadores diretos.
- `000008_create_estoques.up.sql:24` coluna gerada `nome_normalizado`; `:27` `idx_estoques_nome_normalizado`.
- `000010_create_categorias.up.sql:15-16` uniques em `codigo` e `nome`; `:19-44` seed de 25 linhas.
- `000013_create_nomenclatura_templates.up.sql:14` unique em `subtipo`; `:18-48` seed de 28 linhas.
- `000017_add_unique_index_produtos_codigo.up.sql:12` `idx_produtos_codigo (codigo) WHERE codigo IS NOT NULL`.
- Migrations rodam embutidas no binário: `backend/main.go:128` `//go:embed migrations/*.sql`, `:186` `runMigrations(db)`, `:272-292`. Cada arquivo roda em UMA transação.
- Tabelas de domínio a receber `empresa_id` (lista do AC): `produtos, estoques, movimentacoes, pedidos, pedido_itens, categorias, logs_acesso, solicitacoes_promocao, mesclagens_duplicatas, mesclagem_produtos_removidos, importacoes, nomenclatura_templates, usuarios`.

**Roteamento e middleware**
- `backend/main.go:298` `newMux(db, emailCfg, jwtSecret, iamCfg, fotosDir)` — ponto único de wiring; `:310-701` são as 63 chamadas `mux.HandleFunc` com prefixo literal `/api/`. Go 1.22 `ServeMux`; parâmetros por `r.PathValue`.
- `backend/middleware/auth.go:26-28` `ctxKey`/`usuarioSessaoCtxKey`; `:55` `RequireAuth(db, jwtSecret) func(http.HandlerFunc) http.HandlerFunc`; `:96-104` recarrega a conta do Postgres a cada request e injeta; `:114` `UsuarioDaSessao(ctx)`; `:35-48` cópia local de `escreverErro`.
- `backend/middleware/roles.go:47` `RequireRole(papelMinimo)`. Composição atual: `RequireAuth(...)(RequireRole(...)(h))`.
- `backend/services/auth.go:330-338` `UsuarioSessao{ID,Nome,Email,Papel,Ativo,MFAHabilitado,Origem}`; `:1037` `BuscarUsuarioSessao`; `:484-516` claims do JWT (só `sub`,`exp`,`iat`,`origem` — não estender).
- `backend/handlers/auth.go:22-48` envelope de erro + `escreverJSON`; `:130-131` `refreshTokenCookiePath = "/api/auth"`; `:194` `emitirSessaoEResponder`.

**Realtime**
- `backend/realtime/registry.go` — `canaisValidos` (produtos/estoques/movimentacoes/pedidos), `Publish(canal, evento)` (panic em canal inválido), `Subscribe() (<-chan Evento, func())` **sem nenhum filtro**: hoje todo assinante recebe todo evento.
- 10 call sites de `Publish`: `handlers/produtos.go:137,207`; `handlers/movimentacoes.go:74,159`; `handlers/pedidos.go:65,168`; `handlers/normalizacao.go:155,283,285,287`.
- `backend/services/realtime.go:30` `EmitirTicketRealtime(db, usuarioID)`, `:56` `ConsumirTicketRealtime(db, token)`; `backend/handlers/realtime.go:84` `StreamRealtimeHandler` (rota sem `RequireAuth`, autentica por ticket).

**Services — pontos de maior alavancagem** (todos em `backend/services/`)
- `catalogo.go:165` `montarFiltrosCatalogo(f, primeiroPlaceholder) (string, []any)` — choke point único: cobre grade (`:242`), agrupado (`:413`), contagens (`:251,:422`), `ListarTodosGruposCatalogo` (`:505`) e por tabela a exportação XLSX (`relatorios.go:57`). Cuidado: cada chamador recalcula LIMIT/OFFSET como `len(args)+1/+2`.
- `normalizacao.go:849` `DetectarDuplicatas` e `:217` `AnalisarInconsistencias` fazem full scan de `produtos` sem WHERE; `:728` `carregarLocaisProduto(db executorSQL)` faz full scan de `produto_estoque`; `:375` `carregarIgnoradas` idem; `:901-904` o balde de agrupamento é `baldes[nomeNormalizado]` (sem componente de Empresa); `:1068` `MesclarDuplicatas`, com o lock em `:1096` (`WHERE id = ANY($1) ... FOR UPDATE`) e a revalidação em `:1145-1173`; escritas destrutivas em `:1237,1247,1253,1277-1343,1347`.
- `movimentacoes.go:311` `travarLinhaProdutoEstoque(tx, produtoID, estoqueID)` — compartilhado por transferência e mesclagem.
- `importacoes.go:937` `encontrarOuCriarEstoque(tx, nome)` — hoje colapsaria a importação de uma Empresa no Estoque de outra; `:260` `ObterUltimaImportacao(db)` pega a mais recente da base inteira.
- `exclusao_conta.go:218-220` conta "último adm ativo" globalmente; `:117` `ListarSolicitacoesExclusao(db)` sem nenhum parâmetro.
- `pedidos.go:341` `ListarPedidosFila(db, filtroStatus)` sem WHERE; `:535` `DecidirPedido` sem checagem de posse; `:292/:346` concatenam `$2` na mão (cuidado com a numeração ao inserir um placeholder).
- Sem escopo algum: `usuarios.go:33`, `estoques.go:69`, `produtos.go:548` (`ListarCategorias`), `nomenclatura.go:83`, `movimentacoes.go:72`, `logs_acesso.go:88`, `promocao.go:173`.
- Auth por e-mail global: `auth.go:162` `Cadastrar`, `:358` `Login`, `:863` `SolicitarRedefinicaoSenha`; `auth_sso.go:19` `BuscarUsuarioPorEmailSSO`.
- Convenções: exportadas recebem `*sql.DB`; helpers internos transacionais recebem `*sql.Tx`. Única interface existente: `normalizacao.go:715` `executorSQL{ Query }`.

**CLI**
- `backend/cmd/seed-admin/main.go:116-123` — `INSERT ... WHERE NOT EXISTS (SELECT 1 FROM usuarios WHERE papel='adm')` e trata 23505 de `idx_usuarios_unico_adm`. Invocado no deploy em `.github/workflows/deploy-cliente-aws.yml:167` com `--nome/--email/--senha` e `|| true`.
- `backend/cmd/migrate-legado/` — **fora de escopo** (ver Never).

**Testes (backend)**
- 5 cópias privadas de `testDB(t)`: `main_test.go:157`, `services/auth_test.go:34`, `handlers/auth_test.go:35`, `middleware/auth_test.go:36`, `cmd/seed-admin/main_test.go`. DSN por `DATABASE_URL`; `t.Skip` se vazio; migrations uma vez por pacote via `sync.Once` com retry; reset por `TRUNCATE TABLE usuarios CASCADE`.
- `main_test.go:66` `seedContaMux`; `:96` `tokenDeMux` (faz login real + TOTP quando papel ≥ gestor); `newMux` chamado em ~24 testes; `main_test.go:308-481` tabela de rotas com caminhos literais; `main_test.go:631-640` TRUNCATE multi-tabela.
- Rodar com `go test -p 1 ./...` (banco único compartilhado).

**Frontend**
- **Não existe api client**: 71 literais `fetch('/api/...')` em 25 arquivos de produção; `authHeaders()` duplicado em 17 arquivos; token em memória em `src/lib/session.ts:13-25`.
- `src/App.tsx:109-130` `createBrowserRouter` (sem `basename`); `:78-105` `RotaProtegida`.
- `src/lib/realtime/client.ts:127` ticket, `:150` `new EventSource('/api/realtime/stream?ticket=...')`.
- `frontend/nginx.conf` — `location /api/` (proxy), `location /assets/`, `location /` (`try_files ... /index.html`).
- `frontend/vite.config.ts:16-24` proxy de dev `'/api' → localhost:8080`.
- Testes: vitest, 48 arquivos `*.test.tsx` que mockam `globalThis.fetch` e asseguram a URL literal `/api/...`. Em jsdom `location.pathname === '/'` → sem slug → o helper devolve o caminho intocado e **nenhum teste existente precisa mudar**.

## Tasks & Acceptance

**Execution:**

*Schema*
- `backend/migrations/000031_create_empresas.{up,down}.sql` -- criar `empresas` (id UUID PK, nome_fantasia, razao_social, cnpj CHAR(14), logradouro, numero, complemento NULL, bairro, cidade, cep, uf CHAR(2), slug, status CHECK ativa/inativa DEFAULT 'ativa', `empresa_origem_id` UUID NULL REFERENCES empresas(id), criado_em) + `UNIQUE (cnpj)` + `UNIQUE (slug)` + índice em `empresa_origem_id` -- a fronteira de isolamento; CNPJ único na plataforma inteira, slug é a chave de resolução em toda requisição. Nenhum INSERT.
- `backend/migrations/000032_add_empresa_id_dominio.{up,down}.sql` -- adicionar `empresa_id UUID NULL REFERENCES empresas(id)` às 13 tabelas do AC, criar um índice por tabela e **substituir** os índices únicos globais por compostos `NULLS NOT DISTINCT`: `idx_usuarios_email_lower`→`(empresa_id, lower(email))`, `idx_usuarios_unico_adm`→`(empresa_id, papel) WHERE papel='adm'`, `idx_estoques_nome_normalizado`→`(empresa_id, nome_normalizado)`, `idx_produtos_codigo`→`(empresa_id, codigo) WHERE codigo IS NOT NULL`, `categorias_codigo_key`/`categorias_nome_key`→índices `(empresa_id, codigo)`/`(empresa_id, nome)`, `nomenclatura_templates_subtipo_key`→`(empresa_id, subtipo)` -- unicidade deixa de ser global sem afrouxar nada na fase 1: com `NULLS NOT DISTINCT` as linhas legadas (`empresa_id IS NULL`) continuam colidindo entre si exatamente como hoje.

*Fundação de domínio*
- `backend/services/empresas.go` -- novo: tipo `Empresa{ID,NomeFantasia,RazaoSocial,CNPJ,Endereco,Slug,Status,EmpresaOrigemID}`, `BuscarEmpresaPorSlug(db, slug) (Empresa, error)` com `ErrEmpresaNaoEncontrada` (colapsa slug inexistente, inativa e texto inválido), `NormalizarCNPJ`/`ValidarCNPJ` (14 dígitos + dois verificadores), `NormalizarSlug`, e `ProvisionarEmpresa(tx, dados) (Empresa, error)` que insere a Empresa e **copia** as `categorias` e `nomenclatura_templates` padrão (as linhas semeadas com `empresa_id IS NULL`) para ela -- 9.2 e 9.4 reutilizam esta função em vez de duplicar o provisionamento.
- `backend/services/empresas_test.go` -- novo: cobrir validação de CNPJ (válido/dígito errado/tamanho), slug, busca por slug (achado/inexistente/inativa) e a cópia das listas padrão no provisionamento.

*Middleware e roteamento*
- `backend/middleware/empresa.go` -- novo: `empresaCtxKey`, `RequireEmpresa(db) func(http.HandlerFunc) http.HandlerFunc` que lê `r.PathValue("slug")`, resolve via `services.BuscarEmpresaPorSlug` e injeta; 404 `NOT_FOUND` em qualquer falha de resolução; `EmpresaDaRequisicao(ctx) (services.Empresa, bool)` -- resolução única por requisição (AD-19), fora do service.
- `backend/middleware/auth.go` -- adicionar `EmpresaID` a `services.UsuarioSessao` (via `BuscarUsuarioSessao`) e, quando houver Empresa no contexto, recusar com 401 `SESSION_REVOKED` se `usuario.EmpresaID` não for a da URL -- impede que um token válido de uma Empresa opere sob o slug de outra.
- `backend/main.go` -- reescrever as 63 registrações para `/e/{slug}/api/...` (exceto `GET /api/health`, que permanece sem prefixo) e envolver todas em `RequireEmpresa(db)(...)` por fora de `RequireAuth`; ajustar `handlers/auth.go:131` `refreshTokenCookiePath` para o prefixo da Empresa da requisição -- o cookie de refresh precisa ficar escopado por Empresa, senão a sessão vaza de slug para slug.

*Camada de services — filtro por Empresa*
- `backend/services/catalogo.go` -- `empresaID` em `FiltrosCatalogo` e condição `p.empresa_id = $N` em `montarFiltrosCatalogo`; `ObterProdutoDetalhe(db, empresaID, id)`; `preencherPorEstoque` filtrando por Empresa; corrigir a aritmética de placeholders nos 4 chamadores -- uma edição cobre grade, agrupado, contagens e exportação.
- `backend/services/relatorios.go` -- repassar o filtro de Empresa ao `ListarTodosGruposCatalogo` -- a exportação XLSX é hoje o maior vazamento (não paginada).
- `backend/services/normalizacao.go` -- `empresaID` explícito em `AnalisarInconsistencias`, `AplicarCorrecoes`, `IgnorarSugestao`, `DetectarDuplicatas`, `MesclarDuplicatas`, `carregarIgnoradas` e `carregarLocaisProduto`; qualificar o balde de agrupamento por Empresa; no lock de `MesclarDuplicatas` acrescentar `AND empresa_id = $2` (a checagem de contagem já existente converte id alheio em `ErroMesclagemInvalida`); escopar as escritas de `produto_estoque`, `movimentacoes`, `pedido_itens` e `produtos` -- mesclar através de Empresas passa a ser impossível, não desencorajado (AC 4).
- `backend/services/produtos.go`, `estoques.go`, `fotos.go`, `movimentacoes.go`, `carrinho.go`, `pedidos.go`, `importacoes.go`, `usuarios.go`, `gestao_usuarios.go`, `promocao.go`, `logs_acesso.go`, `exclusao_conta.go`, `nomenclatura.go`, `privacidade.go`, `realtime.go` -- acrescentar `empresaID` como parâmetro explícito e `AND empresa_id = $N` (ou `AND <alias>.empresa_id = $N` em joins) em toda leitura e escrita de tabela de domínio; gravar `empresa_id` em todo INSERT; escopar por Empresa a contagem de "último adm ativo" (`exclusao_conta.go`), `ObterUltimaImportacao`, `encontrarOuCriarEstoque` e `ListarPedidosFila` -- id de outra Empresa passa a colapsar no sentinela de "não encontrado" já existente.
- `backend/services/auth.go`, `auth_sso.go` -- `empresaID` em `Cadastrar`, `Login`, `SolicitarRedefinicaoSenha`, `BuscarUsuarioPorEmailSSO` (busca por `lower(email) AND empresa_id`), e nos consumos de token (`VerificarEmail`, `RedefinirSenha`, `ValidarTokenRedefinicao`, `ConcluirLoginMFA`) validar por join que o dono do token pertence à Empresa da URL; `BuscarUsuarioSessao` passa a devolver `EmpresaID` -- o mesmo e-mail pode existir em Empresas diferentes e o login nunca pergunta qual.
- `backend/services/email.go` -- incluir o slug nos links gerados a partir de `APP_URL` (`{APP_URL}/e/{slug}/verificar-email?token=...` e idem redefinição) -- senão o link do e-mail cai fora do prefixo e não resolve.

*Handlers e realtime*
- `backend/handlers/*.go` -- em cada handler, obter a Empresa por `middleware.EmpresaDaRequisicao` e repassá-la ao service; nunca aceitar `empresa_id` de entrada -- 53 call sites seguem o guard-clause já padronizado.
- `backend/realtime/registry.go` -- chavear as inscrições por Empresa: `Publish(empresaID, canal string, evento Evento)` e `Subscribe(empresaID string) (<-chan Evento, func())` -- hoje todo assinante recebe todo evento do processo.
- `backend/handlers/realtime.go` -- `StreamRealtimeHandler` resolve a Empresa pelo slug (a rota fica sob o prefixo mesmo sem `RequireAuth`), consome o ticket e recusa com 401 `SESSION_REVOKED` se a conta do ticket for de outra Empresa; assina só o canal daquela Empresa. Atualizar os 10 call sites de `Publish`.

*CLI*
- `backend/cmd/seed-admin/main.go` -- flag **opcional** `--empresa-slug`: quando informada, resolve a Empresa e cria o `adm` dentro dela (`NOT EXISTS ... AND empresa_id = $N`); quando ausente, mantém exatamente o comportamento atual (`empresa_id` NULL) -- o deploy em CI passa só `--nome/--email/--senha` e não pode quebrar.

*Frontend*
- `frontend/src/lib/api.ts` -- novo: `slugDaURL()` (lê o primeiro segmento após `/e/` de `location.pathname`), `prefixoEmpresa()` (`/e/{slug}` ou `''`), `apiUrl(caminho)` e `authHeaders()` compartilhado -- sem slug devolve o caminho intocado, o que preserva os 48 arquivos de teste que mockam `fetch` sob `location.pathname === '/'`.
- `frontend/src/**` (25 arquivos de produção com literais `/api`) -- trocar cada `fetch('/api/...')` por `fetch(apiUrl('/api/...'))`, incluindo `lib/realtime/client.ts` (ticket e `EventSource`) -- é o único caminho para o browser alcançar a API prefixada.
- `frontend/src/App.tsx` -- passar `{ basename: prefixoEmpresa() }` ao `createBrowserRouter` -- as rotas do SPA passam a viver sob o mesmo prefixo sem reescrever nenhum `to=`/`navigate()`.
- `frontend/nginx.conf` -- acrescentar `location ~ ^/e/[^/]+/api/` com o mesmo `proxy_pass` do bloco atual (mantendo `location /api/` para `/api/health`) -- o `try_files` do bloco `/` já cobre o deep-link do SPA.
- `frontend/vite.config.ts` -- acrescentar a chave `'/e'` ao proxy de dev -- senão `npm run dev` deixa de alcançar a API.

*Testes*
- `backend/services/isolamento_test.go` -- novo: o teste-critério do AC 3 e do SM-7 — cria duas Empresas com dados equivalentes e, para **cada** área (Catálogo/busca/exportação, Estoques, Movimentações, Pedidos próprios e fila, Log de Acesso, Inconsistências, Duplicatas e mesclagem, Gestão de Contas, Promoção, Importações, Categorias), afirma que a consulta de uma nunca devolve linha da outra e que escrita por id alheio falha sem efeito.
- `backend/middleware/empresa_test.go` -- novo: slug válido injeta a Empresa; slug inexistente e Empresa inativa → 404 `NOT_FOUND`; token de outra Empresa → 401 `SESSION_REVOKED`.
- `backend/realtime/registry_test.go` -- acrescentar: evento publicado para a Empresa A não chega ao assinante da Empresa B.
- `backend/main_test.go` e demais `*_test.go` do backend -- prefixar os caminhos literais com `/e/{slug}`, criar a Empresa nos helpers de seed (`seedContaMux`, `criarUsuario`, ...), estender o TRUNCATE para `empresas` e ajustar as chamadas de service às novas assinaturas.

**Acceptance Criteria:**
- Given o schema após as migrations 000031/000032, when se inspeciona o banco, then existe `empresas` com os campos do AC e cada uma das 13 tabelas de domínio tem `empresa_id` nullable com FK para `empresas(id)`.
- Given duas Empresas ativas com dados equivalentes, when qualquer área listada no AC 3 é consultada sob o slug de uma delas, then nenhuma linha da outra aparece — comprovado por `backend/services/isolamento_test.go`.
- Given uma requisição sob `/e/{slug}/api/...`, when ela é processada, then `empresa_id` é resolvido uma única vez no middleware e nenhum service o re-deriva nem o aceita de body/query/header — comprovado por `grep` não encontrar leitura de `empresa_id` a partir de entrada do cliente em `handlers/`.
- Given Produtos de mesmo nome, dimensão e Estoque em duas Empresas, when a detecção de duplicatas roda, then nenhum grupo mistura Empresas; e when a mesclagem é chamada com um id de outra Empresa, then falha com `ErroMesclagemInvalida` sem nenhuma escrita.
- Given a Empresa A já com um `adm` ativo, when um `adm` é criado na Empresa B, then é aceito; when um segundo `adm` é criado na Empresa A, then é recusado pelo índice único.
- Given o backend em execução, when `go build ./...` e `go test -p 1 ./...` rodam com `DATABASE_URL` definido, then compilam e passam; e `npm run build`, `npm run lint` e `npm run test` passam no frontend.

## Spec Change Log

- **2026-09-10 — `empresas` NÃO entra no TRUNCATE dos testes.** A task de testes
  pedia "estender o TRUNCATE para `empresas`". Feito o contrário, de propósito:
  `empresas` é referenciada por `categorias`/`nomenclatura_templates` (e por
  toda tabela de domínio), então um `TRUNCATE TABLE ... empresas CASCADE`
  leva embora o seed das migrations 000010/000013 — que nenhuma migration
  recria, porque já estão aplicadas. Como as suítes compartilham um único
  banco (`go test -p 1 ./...`), isso quebrava todos os pacotes seguintes. No
  lugar: cada suíte provisiona a sua Empresa uma vez, de forma idempotente
  (`garantirEmpresaTeste`/`garantirEmpresaSeed`, SELECT por slug antes de
  criar), e nenhuma delas trunca `empresas`.

## Review Triage Log

## Design Notes

**Por que `NULLS NOT DISTINCT`.** A fase 1 de AD-23 exige `empresa_id` nullable, mas trocar `UNIQUE (lower(email))` por `UNIQUE (empresa_id, lower(email))` com semântica padrão faria toda linha legada (`empresa_id IS NULL`) deixar de colidir — o banco de produção perderia a unicidade de e-mail no intervalo entre 9.1 e 9.4. `NULLS NOT DISTINCT` (Postgres 15, e o compose usa `postgres:15-alpine`) trata NULL como valor comparável, então as linhas legadas continuam sujeitas exatamente à regra de hoje e as linhas novas passam a ser únicas por Empresa. Vale para os 7 índices reescopados.

**Estado do sistema depois desta story.** Nenhuma Empresa é criada aqui — a Story 9.4 exige explicitamente que "Ferreira Costa" ainda não exista em `empresas`, e a criação pela interface é da 9.2. Consequência assumida e prevista pelo sequenciamento do épico: entre 9.1 e 9.2/9.4 não há slug que resolva, e as linhas já gravadas (com `empresa_id IS NULL`) não aparecem em nenhuma consulta. Os testes criam suas próprias Empresas por SQL, que é como o próprio AC 3 descreve a verificação.

**Por que parâmetro explícito e não `context.Context`.** `backend/services/` não importa `net/http` nem `context` em nenhuma das ~5.500 linhas de produção, e três arquivos documentam a regra em comentário (AD-8 forma 3: o escopo chega ao service como argumento já resolvido). `empresaID` segue o mesmo caminho de `papel`.

**Fotos em disco.** `fotosDir` é plano, com nomes `<produtoID>-<unix>.jpg`, e `produtoID` é UUID. Com a checagem de posse do Produto no banco (em `SalvarFotoProduto`, `ListarFotosProduto` e no handler que serve o arquivo), não há leitura cruzada pela API e não é preciso particionar o diretório.

**Handoffs registrados.** (a) `cmd/migrate-legado` continua gravando `empresa_id` NULL — a Story 9.4 precisa passar a Empresa nos seus ~15 INSERTs antes do `SET NOT NULL`. (b) Acessar o app sem slug (`/`) não tem tela própria nesta story; a 9.2, que passa a conhecer os slugs, é o lugar natural para a página de entrada. (c) `IAM_REDIRECT_URI` do Keycloak passa a precisar do prefixo de slug da Ferreira Costa quando a 9.4 definir o slug dela. (d) `cmd/migrate-legado` carrega o mapa
`lower(categorias.nome) -> id` sem filtro de Empresa (`produtos.go:344`) e
checa o seed com `count(*) FROM categorias` (`:229`): com as cópias por
Empresa criadas por `ProvisionarEmpresa`, o mesmo nome passa a existir N+1
vezes e o mapa fica ambíguo. Inofensivo nesta story (em produção não existe
Empresa nenhuma até a 9.4, então só as linhas padrão existem) e deliberadamente
não tocado (Never), mas a Story 9.4 precisa recortar as duas consultas pela
Empresa "Ferreira Costa" junto do resto do corte.

## Auto Run Result

Status: done (retomada manual)

A passagem anterior parou em `blocked` (HTTP 429, limite de sessão da API às
10:47) com a implementação de produção escrita e a árvore SUJA — 94 arquivos
modificados e 8 novos, todos não commitados. Esta passagem foi a retomada
manual: nada foi descartado, o que faltava foi terminado e a story fechada.

Estado herdado (verificado, não assumido): `go build ./...` limpo, as
migrations 000031/000032, `services/empresas.go`, `middleware/empresa.go`, o
reroteamento de `main.go` para `/e/{slug}/api/...`, os 63 registros sob
`RequireEmpresa`, os services com `empresaID` explícito, o realtime
particionado, `cmd/seed-admin --empresa-slug` e todo o frontend
(`lib/api.ts`, os 25 arquivos com `fetch`, `basename` do router, nginx, vite)
já estavam prontos. Faltavam **os testes** — nenhum arquivo `*_test.go` de
`backend/`, `handlers/` ou `middleware/` havia sido ajustado, e os três
arquivos de teste novos exigidos pela spec não existiam.

Feito nesta passagem:
- **`services/`**: removida a duplicação `empresaTeste, empresaTeste` de 38
  chamadas; `empresa_id` nos 20 INSERTs de seed direto (usuarios, logs_acesso,
  importacoes, solicitacoes_promocao, movimentacoes); `EmpresaID` nos literais
  de `FiltrosCatalogo`/`RegistroTentativaLogin`; buscas de
  `categorias`/`nomenclatura_templates` recortadas pela Empresa (as cópias por
  Empresa tornaram a busca por `codigo`/`subtipo` ambígua); links de e-mail
  esperados agora com o prefixo `/e/{slug}`.
- **`handlers/`**: novo `empresa_teste_test.go` (Empresa padrão idempotente +
  `comEmpresa`, que compõe `RequireEmpresa` por fora como `newMux`); os 46
  padrões de rota dos muxes locais e todos os caminhos literais migrados para
  `/e/{slug}/api/...`; as 26 chamadas diretas de handler despachadas sob
  `RequireEmpresa`; assinaturas de service e `Subscribe(empresaID)` ajustadas.
- **`backend/` (pacote main)**: novo `empresa_teste_test.go`; 90 caminhos
  literais prefixados (menos `GET /api/health`, que segue sem prefixo);
  `seedContaMux` e os seeds diretos gravando na Empresa.
- **Testes novos exigidos pela spec**: `services/empresas_test.go` (CNPJ,
  slug, resolução por slug, cópia das listas padrão, unicidade de CNPJ/slug na
  plataforma), `services/isolamento_test.go` (o teste-critério do AC 3: as 12
  áreas na leitura + 6 escritas por id alheio) e `middleware/empresa_test.go`
  (injeção pelo slug, o colapso de todas as falhas em 404, e o 401
  SESSION_REVOKED de token de outra Empresa e de conta legada). O teste
  cross-Empresa do `realtime/registry_test.go` já vinha pronto.
- **Frontend**: novo `src/lib/api.test.ts` (9 casos) — `lib/api.ts` era o
  único arquivo novo da story sem teste.
- **Dois vazamentos de teste corrigidos** (não estavam na spec, mas quebravam
  a suíte inteira num banco compartilhado): `cmd/seed-admin` truncava
  `usuarios, empresas CASCADE`, o que apagava o seed de
  `categorias`/`nomenclatura_templates`; e o snapshot de `categorias` do
  `cmd/migrate-legado` (`TestMigrarProdutos_SeedAusente`) restaurava as linhas
  sem `empresa_id`, apagando as cópias das outras suítes. Também os quatro
  `ON CONFLICT (lower(email))` dos testes do migrate-legado, que deixaram de
  casar o índice depois de ele virar `(empresa_id, lower(email))` (42P10).

Duas causas de falha que atravessavam pacotes (o banco de testes é único,
`go test -p 1 ./...`), ambas em código de teste e ambas corrigidas:

1. `services/exclusao_conta_test.go` derruba `idx_usuarios_unico_adm` de
   propósito num teste (para provar "adm com outro adm ativo prossegue") e,
   no cleanup, recriava o índice na forma ANTIGA e global (`(papel)`) —
   desfazendo a migration 000032 no banco compartilhado. O efeito só
   aparecia depois: na execução seguinte, `cmd/seed-admin` e o teste de
   isolamento eram recusados ao criar o segundo `adm` de outra Empresa
   (justamente o AC 5). O cleanup agora recria a forma vigente,
   `(empresa_id, papel) NULLS NOT DISTINCT WHERE papel = 'adm'`. O arquivo
   de migration sempre estava correto.
2. `services/empresas_test.go` criava Empresas com CNPJ/slug fixos, e
   `empresas` não é truncada entre testes nem entre execuções — o segundo
   `go test` esbarrava em "já existe uma empresa com este CNPJ". Cada caso
   passou a partir de um estado conhecido via `removerEmpresaDeTeste`, que
   apaga a Empresa e apenas as linhas que existem por causa dela (as cópias
   de `categorias`/`nomenclatura_templates`), nunca as linhas padrão.

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: sem erros.
- `cd backend && go vet ./...` -- expected: sem achados.
- `cd backend && DATABASE_URL=... go test -p 1 ./...` -- expected: todos os pacotes passam, incluindo `services/isolamento_test.go` e `middleware/empresa_test.go`.
- `cd frontend && npm run lint && npm run test && npm run build` -- expected: sem erros.
- `grep -rn "empresa_id" backend/handlers/ | grep -iE "PathValue|FormValue|URL.Query|json:"` -- expected: nenhum resultado (nenhum handler aceita `empresa_id` do cliente).
