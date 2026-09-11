---
title: 'Story 9.4: Migração da Ferreira Costa para o modelo multi-Empresa'
type: 'feature'
created: '2026-09-10'
baseline_revision: '0675fa369643341f9c0a25ee363f957e46c9c19a'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Depois das Stories 9.1–9.3 o banco de produção da Ferreira Costa está órfão: `empresa_id` existe em 13 tabelas mas é NULL em toda linha já gravada, não há nenhuma linha em `empresas`, e como toda query de service filtra por Empresa e todo endpoint vive sob `/e/{slug}`, nenhum dado legado aparece para ninguém e nenhum slug resolve. Além disso, `cmd/migrate-legado` (o corte do Firestore, ainda pendente em produção) grava `empresa_id` NULL nos seus 6 INSERTs de domínio, e as listas padrão de Categoria/Nomenclatura moram como linhas `empresa_id IS NULL` dentro das próprias tabelas de domínio — o que impede `SET NOT NULL`.

**Approach:** Um CLI operacional (`cmd/migrar-multi-empresa`), disparado à mão por uma pessoa, em três etapas idempotentes: (1) cria a Empresa "Ferreira Costa" com os dados fornecidos pelo operador e, na mesma transação, a Empresa-irmã "… - Treinamento" reaproveitando o mecanismo da 9.2; (2) faz o backfill em lotes, resumível, atribuindo essa Empresa a toda linha ainda sem Empresa nas 13 tabelas e completando as listas padrão dela; (3) só depois, e só com zero linha órfã, endurece `empresa_id` para `NOT NULL` sem lock longo. As listas padrão saem das tabelas de domínio para `categorias_padrao`/`nomenclatura_templates_padrao`, e `migrate-legado` passa a exigir `--empresa-slug`.

## Boundaries & Constraints

**Always:**
- **Duas fases aditivas, nunca um `ALTER ... SET NOT NULL` direto (AD-23).** Fase 1 (`empresa_id` nullable) já é a migration `000032`. O backfill roda em lotes (`--lote`, default 1000), cada lote na sua própria transação. O endurecimento é `ADD CONSTRAINT ... CHECK (empresa_id IS NOT NULL) NOT VALID` → `VALIDATE CONSTRAINT` (`SHARE UPDATE EXCLUSIVE`, não bloqueia leitura nem escrita) → `SET NOT NULL` (Postgres 12+ aproveita a constraint validada e não varre a tabela) → `DROP CONSTRAINT`, uma tabela por transação.
- **Resumível e idempotente sem tabela de checkpoint:** o próprio predicado `empresa_id IS NULL` é a marca de progresso. Reexecutar depois de uma interrupção continua de onde parou; reexecutar depois do fim atualiza 0 linhas. As três etapas são idempotentes: a etapa `empresa` reconhece a Empresa/Treinamento já criados e segue, o endurecimento pula coluna já `NOT NULL`.
- **O endurecimento é recusado enquanto restar UMA linha órfã** em qualquer das 13 tabelas — a checagem roda para todas as tabelas ANTES de qualquer DDL, e a mensagem nomeia tabela e contagem.
- **A conta `adm` global vira o `adm` da Ferreira Costa por adoção**, nunca por criação: o backfill de `usuarios` preenche o `empresa_id` da conta existente, preservando senha, MFA e histórico. O `adm` do Treinamento é conta separada (mesmo nome/e-mail, outra Empresa), provisionada pelo mecanismo da 9.2 — sem senha, com e-mail `primeiro_acesso`.
- **Reuso, não duplicação (dependência 9.4→9.2):** o Treinamento é criado por uma função extraída de `CriarEmpresaComTreinamento`, que passa a chamá-la. Nenhuma cópia do `semearDadosTreinamento`/`provisionarAdmPrimeiroAcesso`.
- **Disparo humano (AD-15, PRD §9):** dry-run é o default; sem `--executar` nada é escrito. O binário não é invocado por nenhum workflow, job ou entrypoint — só por `docker compose exec` de uma pessoa.
- `cmd/migrate-legado` passa a exigir `--empresa-slug`: grava `empresa_id` nos 6 INSERTs de domínio e recorta por Empresa TODAS as consultas de resolução (estoques, usuários por e-mail, códigos de produto, categorias, templates).

**Block If:**
- Cumprir a adoção exigiria apagar, reescrever ou remapear linha de domínio já existente em produção (Produto, Estoque, Movimentação, Pedido, conta) — a migração é aditiva sobre `empresa_id`, e só sobre ele.
- Alcançar `SET NOT NULL` exigiria rodar o endurecimento como migration embutida (ver Design Notes: o `api` migra no boot e um erro derruba o container em laço, impedindo o próprio `exec` que rodaria o backfill).

**Never:**
- Rodar a migração de produção de forma autônoma nesta sessão, ou registrá-la em `.github/workflows/`, `docker-compose.yml`, `installer/` ou `ENTRYPOINT`.
- Inventar CNPJ, Razão Social, endereço ou slug da Ferreira Costa: são entrada obrigatória do operador (exceto o slug, derivável do Nome Fantasia).
- Criar um segundo `adm` para a Empresa real, ou trocar a senha/MFA de qualquer conta existente.
- Apagar as listas padrão: elas são COPIADAS para `categorias_padrao`/`nomenclatura_templates_padrao` antes de qualquer remoção, e as linhas ainda referenciadas por Produtos são adotadas pela Empresa, nunca deletadas.
- Tocar em frontend, rotas HTTP, middleware ou qualquer superfície de usuário — esta story não tem tela.
- Estender `migracao_id_map` com Empresa ou preparar um segundo corte legado para outro cliente (fora de escopo do épico).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Dry-run (default) | banco com linhas órfãs, sem `--executar` | imprime contagem de órfãs por tabela, se a Empresa/Treinamento existem e quais colunas já são `NOT NULL`; **nada é escrito** | — |
| Etapa `empresa` | `--executar` + CNPJ/razão social/endereço válidos | uma transação: `empresas` (real, `ativa`, sem cópia de listas) + "… - Treinamento" (`{slug}-treinamento`, `empresa_origem_id`=real, listas padrão, 2 Estoques/5 Produtos de exemplo) + `adm` do Treinamento sem senha + e-mail `primeiro_acesso` | — |
| Etapa `empresa` repetida | Empresa e Treinamento já existem | reconhece pelo slug, não recria, reporta "já existente" e sai com 0 | — |
| CNPJ inválido / campo vazio | `--cnpj 123` | recusa antes de qualquer escrita, mensagem nomeando o campo | `*ErroEmpresaValidacao`, exit 1 |
| CNPJ de outra Empresa real | CNPJ já em `empresas` com `empresa_origem_id IS NULL` | recusa, nada gravado | `ErrCNPJDuplicado`, exit 1 |
| Etapa `backfill` | 4.000 linhas órfãs, `--lote 1000` | lotes sucessivos por tabela, cada um commitado; ao fim, 0 órfãs e relatório por tabela; listas da Empresa completadas a partir de `categorias_padrao` | — |
| Backfill interrompido | processo morto no meio | as linhas já commitadas permanecem; reexecução continua e termina; nenhuma linha duplicada ou perdida | — |
| Backfill reexecutado no fim | 0 órfãs | 0 linhas atualizadas em todas as tabelas, exit 0 | — |
| Backfill com slug inexistente/inativo | `--slug nao-existe` | recusa antes de qualquer UPDATE | `ErrEmpresaNaoEncontrada`, exit 1 |
| Etapa `endurecimento` com órfãs | resta 1 linha órfã em `pedidos` | nenhum DDL executado; erro nomeando `pedidos` e a contagem | exit 1 |
| Etapa `endurecimento` limpa | 0 órfãs | as 13 colunas viram `NOT NULL` via CHECK NOT VALID → VALIDATE → SET NOT NULL → DROP; sem varredura sob lock exclusivo | — |
| Endurecimento repetido | colunas já `NOT NULL` | pula cada coluna já endurecida, exit 0 | — |
| `migrate-legado` sem `--empresa-slug` | flag ausente | recusa antes de conectar ao legado | exit 1 |
| `migrate-legado` com Empresa | `--empresa-slug ferreira-costa --executar` | todo Estoque/Produto/Movimentação/Pedido/Item nasce com `empresa_id` da Empresa; categorias e templates resolvidos DENTRO dela | — |

</intent-contract>

## Code Map

**Schema (maior migration atual: `000034`)**
- `backend/migrations/000010_create_categorias.up.sql:13-17` `categorias (id, codigo VARCHAR(10), nome VARCHAR(255))` + 25 linhas semeadas sem `empresa_id`; `000013_create_nomenclatura_templates.up.sql` idem com `(subtipo, template)` e 28 linhas. São os "moldes" copiados por `ProvisionarEmpresa` — e a razão de `categorias`/`nomenclatura_templates` não poderem virar `NOT NULL` hoje.
- `backend/migrations/000032_add_empresa_id_dominio.up.sql:25-37` — a lista canônica das **13 tabelas**: `usuarios, produtos, estoques, movimentacoes, pedidos, pedido_itens, categorias, logs_acesso, solicitacoes_promocao, mesclagens_duplicatas, mesclagem_produtos_removidos, importacoes, nomenclatura_templates`. `:9-13` diz por que `produto_estoque`, `carrinho_itens`, `importacao_linhas`, `normalizacao_ignoradas`, `sessoes`, `tokens_acao`, `emails_pendentes` **não** entram (escopo por JOIN com o pai). `:42-54` os índices `idx_<t>_empresa_id` (já existem — o "índice" da fase 2 do AD-23 não precisa ser criado de novo). `:58-90` os 7 índices únicos reescopados com `NULLS NOT DISTINCT`.
- ATENÇÃO: `pedido_itens` (`000026_create_pedidos.up.sql`, PK `(pedido_id, produto_id, estoque_id)`) e `mesclagem_produtos_removidos` (PK `(mesclagem_id, produto_removido_id)`) **não têm coluna `id`** — o lote do backfill deve paginar por `ctid`, que serve às 13 tabelas sem exceção.
- FKs para os moldes: só `produtos.categoria_id` (`000011_create_produtos.up.sql:27`, NOT NULL) e `produtos.template_id` (`000014_add_template_id_to_produtos.up.sql:8`, nullable).
- Molde de cabeçalho/estilo para a migration nova: `backend/migrations/000034_create_convites_empresa.up.sql:1-11` (bloco `--` citando Story/Epic/FR/AD + o porquê de cada índice). Cada arquivo roda numa transação implícita única (`backend/main.go:272-291`, golang-migrate com `MultiStatementEnabled=false`).
- `backend/migrations/000022_seed_usuario_migracao_legado.up.sql` — a conta sintética "Migração do sistema legado" nasce sem `empresa_id`; é autora de toda Movimentação migrada e será adotada pelo backfill.

**Services a reaproveitar/refatorar**
- `backend/services/empresas.go:321` `ProvisionarEmpresa(tx *sql.Tx, DadosEmpresa) (Empresa, error)` — hoje faz validação + INSERT (`:327-352`, mapeando `empresas_cnpj_unico`/`empresas_slug_unico` por nome de constraint) + as duas cópias `SELECT ... WHERE empresa_id IS NULL` (`:354-367`). É esse trecho que se divide em `InserirEmpresa` + `CopiarListasPadrao`. Também: `:89` `DadosEmpresa`, `:62` `EnderecoEmpresa`, `:75` `Empresa`, `:100` `NormalizarCNPJ`, `:135` `ValidarCNPJ`, `:169` `NormalizarSlug`, `:221` `BuscarEmpresaPorSlug` (só `status='ativa'`), `:259` `validarDadosEmpresa`, `:40-58` `ErrEmpresaNaoEncontrada`/`ErrCNPJDuplicado`/`ErrSlugDuplicado`/`ErroEmpresaValidacao`.
- `backend/services/empresas_plataforma.go:173-215` `CriarEmpresaComTreinamento` — o bloco `:196-211` (dados do Treinamento + `ProvisionarEmpresa` + `semearDadosTreinamento` + `provisionarAdmPrimeiroAcesso`, com a tradução `ErrSlugDuplicado`→`ErrSlugTreinamentoDuplicado`) é EXATAMENTE o que a 9.4 precisa: extrair como função e chamar dos dois lados. `:31-45` `sufixoSlugTreinamento`/`sufixoNomeTreinamento`/`slugRealMaxRunes=51`/`nomeFantasiaRealMaxRunes=241`/`tokenPrimeiroAcessoExpiracao`; `:120` `validarNovaEmpresa` (limites + `campoObrigatorio` + `normalizeEmail`/`emailPlausivel`); `:222` `provisionarAdmPrimeiroAcesso` (INSERT `usuarios` com `senha_hash NULL, papel 'adm', email_verificado true` + `tokens_acao` 7 dias + `EnfileirarEmail(..., "primeiro_acesso", ...)`); `:254` `semearDadosTreinamento` (2 Estoques, 5 Produtos por código de categoria DA PRÓPRIA Empresa); `:291` `ListarEmpresasPlataforma`; `:352` `AlterarStatusEmpresa`.
- `backend/services/email.go:28` `EmailConfig{Host,Port,User,Password,From,AppURL}` — o CLI monta a sua a partir do ambiente (só `AppURL` importa para o link); `:45` `LinkDaEmpresa`.
- `backend/services/fotos.go:46-56` `produtoDaEmpresa` — o guard `(($2 = '' AND empresa_id IS NULL) OR empresa_id::text = $2)` existe só para `cmd/migrate-legado`; com a Empresa passando a ser real, o ramo `''` deixa de ter chamador (manter o guard, não reescrever a função).

**CLI existentes (moldes)**
- `backend/cmd/seed-dono-plataforma/main.go` — molde EXATO do CLI operacional: `slog` JSON em stderr (`:34`), flags (`:36-39`), `validateFlags` (`:76`), `godotenv.Load()` tolerante (`:47`), `DATABASE_URL` obrigatório (`:51-55`), `sql.Open`+`Ping` (`:57-67`), função testável com `io.Writer` injetado (`:86`), `mensagemDeErro` traduzindo sentinelas (`:101-111`), `os.Exit(1)`. O doc do pacote (`:8-14`) é o molde da justificativa "este binário não roda no pipeline".
- `backend/cmd/seed-admin/main.go:41` `--empresa-slug` opcional; `:141-142` resolução `SELECT id FROM empresas WHERE slug=$1 AND status='ativa'`; `:156-163` o INSERT com `$4 IS NOT DISTINCT FROM empresa_id`. `.github/workflows/deploy-cliente-aws.yml:146-171` roda esse binário em TODO deploy com `|| true` e **sem** `--empresa-slug` — é o último caminho vivo que ainda cria conta órfã.
- `backend/Dockerfile:10-12` (3 linhas `RUN CGO_ENABLED=0 go build -o /out/<bin> ./cmd/<bin>`) e `:20-22` (3 `COPY --from=build`) — `migrate-legado` **não** está lá; o binário novo precisa entrar nas duas listas, antes de `USER appuser` (`:24`).

**`cmd/migrate-legado` — os pontos exatos a mudar**
- Flags: `main.go:88` só `--executar`. Env: `main.go:100` `DATABASE_URL`, `:105` `LEGADO_DATABASE_URL`, `:159-166` `FOTOS_DIR`. Fases em `main.go:161,211,284,313`.
- **6 INSERTs de domínio sem `empresa_id`**: `main.go:579` (`estoques (nome)`), `produtos.go:734-753` (`produtos`), `produtos.go:775` (`produto_estoque` — não tem a coluna, nada a fazer), `movimentacoes.go:408-414` (`movimentacoes`), `pedidos.go:133-143` (`insertPedido`), `pedidos.go:144-148` (`insertPedidoItem`).
- **6 consultas de resolução sem recorte**: `produtos.go:393`, `pedidos.go:247`, `movimentacoes.go:233` (todas `SELECT nome_normalizado, id FROM estoques`), `pedidos.go:268` (`SELECT lower(email), id FROM usuarios`), `produtos.go:545` (`SELECT codigo FROM produtos WHERE codigo = ANY($1)`), e o par `produtos.go:236,239` (contagem de seed) + `produtos.go:355` (mapa `lower(btrim(nome))→id`), hoje `WHERE empresa_id IS NULL`.
- `produtos.go:662` `const empresaIDLegado = ""` — o ponto único de injeção previsto pelo HANDOFF 9.4 (`produtos.go:228-233,656-661`); passa a ser o id resolvido.
- `movimentacoes.go` resolve o autor sintético por e-mail (`emailUsuarioMigracaoLegado`, erro `errSeedUsuarioMigracaoAusente`) — a busca passa a ser dentro da Empresa, o que exige que o backfill já tenha adotado essa conta (ver Design Notes: ordem do runbook).
- Testes (5 arquivos, ~5.800 linhas, todos contra Postgres real): `main_test.go:38` `testDB`, `:82-145` fixtures `legado.*`, `:176-182` seed do usuário sintético com `ON CONFLICT (empresa_id, lower(email))`, `:194-232` `limparTabelas` (nunca apaga `categorias`/`nomenclatura_templates`/`usuarios`), `:587` `TestMain_Processo` (re-executa o binário como subprocesso). `main_produtos_test.go:87-99` `categoriaExistente` (`WHERE empresa_id IS NULL`) é fixture de quase todo teste de produto; `:530-570` o teste de categoria com acento/prefixo; `:827-882` `TestMigrarProdutos_SeedAusente`, que DELETA `categorias` e restaura por snapshot.

**Testes — armadilhas do banco compartilhado**
- `DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable`; sempre `go test -p 1 ./...`.
- `empresas` NUNCA é truncada (`backend/empresa_teste_test.go:16-20`); helpers idempotentes: `garantirEmpresaTeste` (`backend/empresa_teste_test.go:36-70`), `services/empresa_teste_test.go` (`criarEmpresaDeTeste`, `cnpjDeTeste`, `removerEmpresaDeTeste`), `handlers/empresa_teste_test.go`.
- Precedentes registrados na spec-9-1: um teste que desfaz DDL global (índice único recriado na forma antiga) ou que restaura `categorias` sem `empresa_id` quebra TODOS os pacotes seguintes. **Nenhum teste desta story pode aplicar `NOT NULL` nas tabelas reais** — o endurecimento se testa numa tabela temporária própria e pelo caminho de recusa (que não emite DDL).

## Tasks & Acceptance

**Execution:**

*Schema*
- `backend/migrations/000035_create_listas_padrao.{up,down}.sql` -- criar `categorias_padrao (codigo VARCHAR(10) PRIMARY KEY, nome VARCHAR(255) NOT NULL UNIQUE)` e `nomenclatura_templates_padrao (subtipo VARCHAR(255) PRIMARY KEY, template VARCHAR(255) NOT NULL)`, populá-las com `INSERT ... SELECT ... WHERE empresa_id IS NULL` das tabelas atuais e, só depois, `DELETE` das linhas molde que **nenhum Produto referencia** (`NOT EXISTS (SELECT 1 FROM produtos p WHERE p.categoria_id = c.id)` / `p.template_id = t.id`) -- tirar o molde de dentro da tabela de domínio é o que torna `SET NOT NULL` alcançável; as linhas ainda referenciadas sobrevivem para serem adotadas pela Empresa no corte, e numa instalação nova (sem Produto algum) a limpeza é total. O `.down.sql` recria as linhas molde a partir das tabelas `_padrao` e as derruba.

*Services*
- `backend/services/empresas.go` -- dividir `ProvisionarEmpresa` em `InserirEmpresa(tx, dados) (Empresa, error)` (validação + INSERT + mapeamento de erro, sem cópia) e `CopiarListasPadrao(tx, empresaID) error`, que passa a copiar de `categorias_padrao`/`nomenclatura_templates_padrao` e a ser **idempotente** (`WHERE NOT EXISTS` por `codigo`/`subtipo` dentro da Empresa); `ProvisionarEmpresa` vira a composição das duas e mantém assinatura e comportamento -- a Empresa fundadora precisa nascer SEM cópia (ela adota as listas legadas) e precisa completar a lista DEPOIS do backfill, sem duplicar.
- `backend/services/empresas_plataforma.go` -- extrair de `CriarEmpresaComTreinamento` a função `ProvisionarTreinamento(tx, emailCfg, real Empresa, dadosReal DadosEmpresa, admNome, admEmail string) (Empresa, error)` (nome/slug derivados, `empresa_origem_id`, `ProvisionarEmpresa`, `semearDadosTreinamento`, `provisionarAdmPrimeiroAcesso`, tradução para `ErrSlugTreinamentoDuplicado`) e passar a chamá-la; exportar também `ValidarDadosNovaEmpresa` a partir de `validarNovaEmpresa` (mesmos limites 241/51 e validação do `adm`) -- a 9.4 depende da 9.2 reaproveitando o mecanismo, nunca reimplementando-o.
- `backend/services/migracao_multi_empresa.go` -- novo, o coração da story:
  - `var TabelasComEmpresaID = []string{...}` (as 13, na ordem da migration 000032) -- lista canônica única, usada por contagem, backfill e endurecimento;
  - `ContarLinhasSemEmpresa(db) (map[string]int, error)`;
  - `AdotarEmpresaFundadora(db, emailCfg, dados DadosEmpresa, admNome, admEmail string) (Empresa, Empresa, error)` -- uma transação: `InserirEmpresa` (sem listas) + `ProvisionarTreinamento`; se o slug já existir, resolve as duas Empresas e devolve sem escrever (idempotência);
  - `BuscarAdmSemEmpresa(db) (nome, email string, err error)` -- lê a conta `papel='adm' AND empresa_id IS NULL` para dar nome/e-mail ao `adm` do Treinamento quando o operador não os informar;
  - `BackfillEmpresa(db, empresaID string, lote int, progresso func(tabela string, atualizadas int)) (map[string]int, error)` -- por tabela, laço de `UPDATE <t> SET empresa_id=$1 WHERE ctid IN (SELECT ctid FROM <t> WHERE empresa_id IS NULL LIMIT $2)` até 0 linhas, cada lote em sua transação, e ao final `CopiarListasPadrao` para completar as listas da Empresa;
  - `EndurecerEmpresaID(db) ([]string, error)` -- checa TODAS as tabelas primeiro (qualquer órfã ⇒ erro nomeando tabela/contagem, zero DDL) e então, por tabela e por transação, `endurecerColunaEmpresa` (CHECK NOT VALID → VALIDATE → SET NOT NULL → DROP CONSTRAINT), pulando coluna já `NOT NULL` via `information_schema.columns`.
  -- Justificativa: AC 1 e AC 2 (duas fases, lotes, retomada) e AC 3 (Empresa + `adm` adotado + Treinamento na mesma migração).

*CLI*
- `backend/cmd/migrar-multi-empresa/main.go` -- novo, molde `seed-dono-plataforma`: flags `--etapa` (`tudo|empresa|backfill|endurecimento`, default `tudo`), `--executar` (default false = dry-run), `--nome-fantasia --razao-social --cnpj --logradouro --numero --complemento --bairro --cidade --cep --uf --slug --adm-nome --adm-email --lote`. Dry-run imprime o diagnóstico completo sem escrever. `--slug` (ou o slug derivado) identifica a Empresa nas etapas `backfill`/`endurecimento`. Doc de pacote explicando por que este binário nunca entra em pipeline (AD-15, PRD §9) e o runbook das quatro invocações. -- Justificativa: AC 4 — a migração é sempre disparada por uma pessoa.
- `backend/Dockerfile` -- compilar e copiar `migrar-multi-empresa` -- o operador roda `docker compose exec -T api ./migrar-multi-empresa ...`.
- `backend/cmd/migrate-legado/{main,produtos,movimentacoes,pedidos}.go` -- flag `--empresa-slug` obrigatória, resolvida uma vez em `main` (`SELECT id FROM empresas WHERE slug=$1 AND status='ativa'`) e passada às 4 fases; `empresa_id` nos 6 INSERTs de domínio; `AND empresa_id = $N` nas 6 consultas de resolução (3× estoques, usuários por e-mail, códigos de produto) e nas de categorias/templates, que trocam `empresa_id IS NULL` pela Empresa; `empresaIDLegado` deixa de ser constante vazia -- sem isso o corte legado grava linhas órfãs e o endurecimento fica inalcançável (HANDOFF da spec-9-1).
- `.github/workflows/deploy-cliente-aws.yml` -- ler `ADMIN_EMPRESA_SLUG` do `.env` do servidor e, quando não vazio, repassá-lo como `--empresa-slug` ao `seed-admin` -- é o único caminho automático que ainda cria conta órfã a cada deploy; depois do endurecimento ele falharia silenciosamente sob `|| true`.

*Testes*
- `backend/services/migracao_multi_empresa_test.go` -- novo: adoção cria Empresa sem listas + Treinamento com listas/exemplos/`adm` sem senha; adoção repetida não recria; backfill com `--lote` menor que o volume percorre vários lotes e zera as órfãs; backfill interrompido (chamada com lote 1 e cancelamento simulado) retomado termina sem duplicar nem perder linha; backfill reexecutado atualiza 0; `CopiarListasPadrao` idempotente deixa 25 categorias e 28 templates na Empresa; `EndurecerEmpresaID` recusa com órfã presente **sem emitir DDL**; `endurecerColunaEmpresa` aplicado a uma tabela temporária criada pelo próprio teste vira `NOT NULL` e é idempotente. Cada teste remove as Empresas que criou (`removerEmpresaDeTeste` e as linhas que existem por causa dela).
- `backend/services/empresas_test.go`, `backend/services/empresas_plataforma_test.go` -- ajustar às funções novas: `ProvisionarEmpresa` continua copiando 25/28 (agora de `_padrao`), `InserirEmpresa` não copia nada, `CopiarListasPadrao` chamada duas vezes não duplica, e `CriarEmpresaComTreinamento` segue idêntico via `ProvisionarTreinamento`.
- `backend/cmd/migrar-multi-empresa/main_test.go` -- novo: flags obrigatórias por etapa; dry-run não escreve nada (contagens antes/depois idênticas); `--executar` na etapa `empresa` cria o par; etapa `endurecimento` com órfã sai 1 com a mensagem certa; e um teste que varre `.github/workflows/*.yml`, `docker-compose.yml` e `installer/` afirmando que o nome do binário **não** aparece (AC 4 como invariante verificável).
- `backend/cmd/migrate-legado/*_test.go` -- adaptar todos os chamadores das 4 fases à Empresa; fixture de Empresa idempotente no pacote; `categoriaExistente` e o teste de categoria acentuada passam a ler as categorias DA Empresa; `TestMigrarProdutos_SeedAusente` apaga/restaura as categorias da Empresa (nunca as linhas `_padrao`); casos novos: sem `--empresa-slug` falha, slug inexistente falha, e toda linha gravada pelo corte tem `empresa_id` da Empresa.

**Acceptance Criteria:**
- Given um banco com linhas de domínio órfãs e nenhuma Empresa, when o operador roda a etapa `empresa` e depois a etapa `backfill` com `--lote` menor que o volume, then existe a Empresa "Ferreira Costa" com o CNPJ/razão social/endereço informados, a Empresa "Ferreira Costa - Treinamento" com `empresa_origem_id` apontando para ela, e nenhuma das 13 tabelas tem linha com `empresa_id IS NULL`.
- Given o backfill interrompido no meio, when ele é reexecutado, then termina sem duplicar nem perder vínculo de nenhuma linha, e uma terceira execução atualiza zero linhas.
- Given a conta `adm` global existente antes da migração, when a migração termina, then ela é o `adm` da Empresa "Ferreira Costa" (mesma senha, mesmo MFA, mesmo id) e a Empresa não tem um segundo `adm`; o `adm` do Treinamento é outra conta, sem senha, com e-mail `primeiro_acesso` enfileirado.
- Given uma linha de domínio ainda órfã, when a etapa `endurecimento` é executada, then nenhum `ALTER TABLE` é emitido e o erro nomeia a tabela e a contagem; e given zero órfãs, when ela é executada, then as 13 colunas ficam `NOT NULL` e uma segunda execução é inócua.
- Given o repositório desta story, when se procura o binário da migração em `.github/workflows/`, `docker-compose.yml`, `installer/` e no `ENTRYPOINT` do Dockerfile, then não há nenhuma ocorrência — a migração só roda por invocação humana (AD-15, PRD §9).
- Given o backend desta story, when `go build ./...`, `go vet ./...` e `go test -p 1 ./...` (com `DATABASE_URL`) rodam, then todos passam.

## Spec Change Log

## Review Triage Log

## Design Notes

**Por que o endurecimento é CLI e não migration.** O `api` roda `runMigrations` no boot e sai com erro se qualquer migration falhar (`backend/main.go:184-190`), e o operador só alcança o backfill por `docker compose exec -T api ...`. Uma migration `SET NOT NULL` embutida rodaria automaticamente no deploy, ANTES do backfill, falharia contra as linhas órfãs e derrubaria o container em laço — impedindo justamente o `exec` que resolveria o problema. Consequência assumida: numa instalação NOVA o schema permanece nullable até alguém rodar `migrar-multi-empresa --etapa endurecimento` (que ali passa de primeira, já que a 000035 deixou as tabelas sem molde órfão). Fica registrado como dívida no `deferred-work.md`, não como AC desta story.

**Por que as listas padrão saem das tabelas de domínio.** Os Produtos de produção apontam para as linhas molde de `categorias` (FK `categoria_id NOT NULL`). Se a Empresa fundadora recebesse CÓPIAS das listas, ela teria 25 categorias que nenhum Produto usa e 25 categorias "de ninguém" continuariam sendo o alvo real dos Produtos — o filtro por Categoria do Catálogo devolveria vazio. Adotar as linhas legadas resolve isso sem tocar em um único Produto; e como o molde precisa sobreviver para as próximas Empresas, ele é copiado antes para `categorias_padrao`/`nomenclatura_templates_padrao`. A ordem no CLI é obrigatória: criar a Empresa **sem** listas → backfill (adota) → `CopiarListasPadrao` (completa o que faltar) — inverter duplica código de categoria dentro da Empresa e viola `idx_categorias_empresa_codigo`.

**Por que `ctid` no lote.** `pedido_itens` e `mesclagem_produtos_removidos` têm PK composta e nenhuma coluna `id`; `ctid` é o único identificador de linha comum às 13 tabelas e é estável dentro do statement. O predicado `empresa_id IS NULL` é a própria marca de progresso, então não existe checkpoint a perder: é essa a forma da resumabilidade exigida pelo AC 2, sem a tabela de estado que a importação da Story 3.3 precisa ter (lá cada linha carrega um resultado; aqui só existe "com Empresa" e "sem Empresa").

**Runbook do operador (ordem obrigatória).**
```
1) migrar-multi-empresa                      # dry-run: diagnóstico
2) migrar-multi-empresa --etapa empresa --executar --cnpj ... --razao-social ... --uf PE ...
3) migrar-multi-empresa --etapa backfill --executar --slug ferreira-costa
4) migrate-legado --empresa-slug ferreira-costa            # dry-run do corte legado
   migrate-legado --empresa-slug ferreira-costa --executar # se o corte ainda estiver pendente
5) migrar-multi-empresa --etapa endurecimento --executar
```
O backfill vem ANTES do corte legado de propósito: `migrate-legado` resolve o autor sintético das Movimentações e as Categorias por consulta dentro da Empresa, e essas linhas só entram na Empresa no passo 3.

**Endurecimento sem lock longo (golden).**
```sql
ALTER TABLE produtos ADD CONSTRAINT produtos_empresa_id_nao_nula
  CHECK (empresa_id IS NOT NULL) NOT VALID;          -- ACCESS EXCLUSIVE, instantâneo
ALTER TABLE produtos VALIDATE CONSTRAINT produtos_empresa_id_nao_nula; -- SHARE UPDATE EXCLUSIVE
ALTER TABLE produtos ALTER COLUMN empresa_id SET NOT NULL;            -- PG12+: sem varredura
ALTER TABLE produtos DROP CONSTRAINT produtos_empresa_id_nao_nula;
```

## Auto Run Result

Status: done (retomada manual) — **execução em produção PENDENTE do operador**

A execução automática (`20260910-201447-9b77`) foi interrompida com a spec em
`in-review`: a implementação estava COMPLETA na árvore (nada commitado) e a
sessão morreu antes da fase de revisão. Nada foi descartado. Como na 9.3, esta
story não teve a sessão de revisão independente — a verificação abaixo é da
retomada manual.

O CÓDIGO da migração está pronto; a MIGRAÇÃO em si não foi executada e não
pode ser executada por um agente (AC 4, AD-15, PRD §9). O operador roda o
runbook documentado no doc de pacote de `cmd/migrar-multi-empresa`.

Verificado direto no código:
- **Disparo humano é invariante verificável:** o binário é compilado e copiado
  no `Dockerfile`, o `ENTRYPOINT` continua `./api`, e o nome não aparece em
  `.github/workflows/`, `docker-compose.yml` nem `installer/` — há teste que
  varre esses arquivos.
- **Dry-run é o default:** sem `--executar` nada é escrito.
- **Idempotência:** `AdotarEmpresaFundadora` reconhece a Empresa pelo slug e
  sai sem escrever; o backfill usa `empresa_id IS NULL` como marca de
  progresso; o endurecimento pula coluna já `NOT NULL`.
- **Adoção, não criação:** a conta `adm` existente recebe o `empresa_id` —
  senha, MFA e id preservados.
- **Endurecimento recusado com órfã:** a checagem das 13 tabelas roda ANTES de
  qualquer DDL.
- **Deploy compatível:** o workflow só passa `--empresa-slug` ao `seed-admin`
  quando `ADMIN_EMPRESA_SLUG` está no `.env`; vazio mantém o comportamento
  anterior.

Verificação executada: `go build ./...`, `go vet ./...` e
`DATABASE_URL=... go test -p 1 ./...` (10 pacotes, 0 falhas). O frontend não é
tocado por esta story.

**Contexto de produção no momento deste commit:** `suprimentos.fcxlabs.com`
recebeu as Stories 9.1–9.3 por deploy automático (push em `main`) e está sem
login desde 2026-09-10 ~17:52 UTC — as rotas exigem `/e/{slug}` e nenhuma
Empresa existe (`/e/ferreira-costa/api/auth/login` → 404 `empresa não
encontrada`). O runbook desta story é o que restaura o acesso. Dados
cadastrais fornecidos pelo operador: Ferreira Costa & Cia Ltda, CNPJ
10230480000130, Av. Santo Antonio 515, Centro, Garanhuns/PE, 55290-000, slug
`ferreira-costa`.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -p 1 ./...` -- expected: todos os pacotes passam, incluindo `services/migracao_multi_empresa_test.go` e a suíte adaptada de `cmd/migrate-legado`.
- `cd backend && DATABASE_URL=... go run ./cmd/migrar-multi-empresa` -- expected: relatório de dry-run, exit 0, e `SELECT count(*) ... WHERE empresa_id IS NULL` inalterado depois da execução.
- `grep -rn "migrar-multi-empresa" .github/ docker-compose.yml installer/ backend/Dockerfile` -- expected: só a linha de `RUN go build` e a de `COPY` no Dockerfile; nenhuma invocação.
- `grep -rn "empresa_id" backend/cmd/migrate-legado/*.go | grep INSERT -A2` e revisão dos 6 INSERTs -- expected: todos gravam `empresa_id`.
