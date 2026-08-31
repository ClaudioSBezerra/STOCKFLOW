---
title: 'Story 4.3 — Visualização em grade e tabela agrupada'
type: 'feature'
created: '2026-08-31'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '1522f0345316e0faa1ce87119b48a8d799af9475'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** O Catálogo (`/`) hoje só tem o campo de busca (Story 4.1) e um texto "chega em breve" — qualquer Usuário autenticado (`usuario`+) não consegue navegar o catálogo de Produtos nem ver onde há material disponível, que é a jornada central do produto (FR-6).

**Approach:** Novo endpoint `GET /api/produtos/catalogo` (`RequireAuth` apenas) devolve a listagem paginada de Produtos em dois modos: `agrupar=false` (grade — um Produto por linha, com quantidade total somada e disponibilidade) e `agrupar=true` (tabela — Produtos com mesmo nome + dimensões estruturadas colapsados numa linha, quantidades somadas, com a discriminação por Estoque embutida para a expansão). No frontend, `CatalogoPage` ganha `CatalogoListagem`: alterna grade/tabela em viewport ≥768px; abaixo disso a grade é sempre o padrão e o alternador some.

## Boundaries & Constraints

**Always:**
- `GET /api/produtos/catalogo`: `RequireAuth` apenas (sem `RequireRole`), mesmo padrão de `GET /api/produtos/busca` / `GET /api/categorias` — qualquer conta autenticada, papel mínimo `usuario`.
- Query `pagina`: inteiro ≥1, ausente ⇒ `1`; `0`, negativo, não-numérico ⇒ `400 VALIDATION_ERROR`, mensagem "página inválida". Página além da última ⇒ `200` com lista vazia (a paginação ainda reporta `total`/`totalPaginas`).
- Query `agrupar`: só `true`, `false` ou ausente (⇒ `false`); qualquer outro valor ⇒ `400 VALIDATION_ERROR`, mensagem "parâmetro agrupar inválido".
- Tamanho de página é uma constante do backend (`TamanhoPaginaCatalogo = 24`) — não é parâmetro de query. Decisão desta spec; nenhum documento de planejamento fixa um valor.
- Envelope `agrupar=false`: `200 {"produtos":[{"id","nome","codigo":string|null,"categoria":{"id","codigo","nome"},"dimensoes":<Dimensoes>,"quantidadeTotal":number,"disponivel":bool}, ...],"paginacao":{"pagina","tamanho","total","totalPaginas"}}`. `quantidadeTotal` = `SUM(produto_estoque.quantidade)` do Produto (0 quando não há nenhuma linha). `disponivel` = `quantidadeTotal > 0`. Ordena por `nome ASC, id ASC`.
- Envelope `agrupar=true`: `200 {"grupos":[{"chave":string,"nome","dimensoes":<Dimensoes>,"quantidadeTotal":number,"disponivel":bool,"porEstoque":[{"estoqueId","estoqueNome","quantidade":number}, ...]}, ...],"paginacao":{...}}`. Um grupo = Produtos com **mesmo `nome` e mesmas 5 dimensões estruturadas** (`{campo}_valor` + `{campo}_unidade` para comprimento, largura, diâmetro, altura, espessura; `NULL` agrupa com `NULL`). `quantidadeTotal` = soma de todas as linhas `produto_estoque` de todos os Produtos do grupo. `porEstoque` = essa soma quebrada por Estoque, só Estoques onde o grupo tem alguma linha `produto_estoque`, ordenado por `estoqueNome ASC` (pode ser `[]`). `chave` = hash estável das colunas da chave de grupo (`md5` da concatenação de `nome` + os 10 valores de dimensão), para servir de `key` no React. Ordena por `nome ASC`, depois pelas colunas de dimensão de forma determinística, depois `chave`.
- `<Dimensoes>` = `{"comprimento":<Dim>|null,"largura":<Dim>|null,"diametro":<Dim>|null,"altura":<Dim>|null,"espessura":<Dim>|null}` onde `<Dim>` = `{"valor":number,"unidade":"mm"|"cm"|"m"}`. Um par com `valor` e `unidade` ambos `NULL` no banco ⇒ `null`.
- Paginação: sempre paginação numérica, nunca scroll infinito (UX epic-level). `agrupar=true` pagina sobre **grupos**, `agrupar=false` sobre **Produtos** — a contagem e o `OFFSET`/`LIMIT` operam na mesma unidade que as linhas retornadas, para que a soma de um grupo nunca fique partida entre páginas.
- Frontend `CatalogoListagem`: sempre visível abaixo de `BuscaCatalogo`, para qualquer papel (não depende do gate `podeCadastrar`). Alternador grade/tabela só é renderizado e utilizável quando `window.matchMedia('(min-width: 768px)').matches` — abaixo de 768px (a partir de 360px) o modo é sempre grade e o alternador não aparece (UX-DR16). O componente escuta mudança de viewport (`addEventListener('change', ...)`, com cleanup) e volta para grade se a viewport encolher abaixo de 768px.
- Card de grade: nome, código (fonte `font-mono` quando presente, igual à Story 4.1), nome da categoria e o indicador de disponibilidade `status-disponivel` — **sempre ícone + texto** ("Disponível" / "Sem estoque"), nunca só cor (UX-DR10). Alvo de toque mínimo 48px (`min-h-touch-target-min`) no card e nos controles de paginação/alternância (NFR de usabilidade em campo).
- Linha de tabela agrupada: nome, dimensões resumidas, quantidade total, o mesmo indicador de disponibilidade, e um botão de expandir (`aria-expanded`) que revela `porEstoque` (Estoque + quantidade) numa sub-linha; `porEstoque` vazio ⇒ a sub-linha mostra "Sem quantidade registrada por estoque."
- Estados de `CatalogoListagem`: carregando (enquanto o `fetch` está em voo), erro (resposta não-OK ou rede — mensagem `role="alert"` "Não foi possível carregar o catálogo. Tente novamente em instantes."), e vazio (`total === 0` — "Nenhum produto no catálogo.").

**Block If:** nenhuma condição desta story exige decisão humana em runtime — segue direto.

**Never:**
- Nenhuma migração nova / índice novo. Agrupar/ordenar/contar ~8.000 linhas de `produtos` com `JOIN` em `produto_estoque`/`estoques` é volume trivial para Postgres — a NFR de 300ms p95 (vale para Stories 4.1–4.3) não exige infraestrutura nova (confirmar por `EXPLAIN ANALYZE`, ver Verification, em vez de assumir).
- Nenhum filtro por categoria/estoque/disponibilidade (Story 4.2) — a listagem desta story mostra sempre todos os Produtos, sem parâmetros de filtro.
- Nenhuma integração com o campo de busca da Story 4.1 — `BuscaCatalogo` continua exatamente como está (lista inline própria); combinar busca + listagem é a Story 4.2.
- Nenhuma navegação ao clicar num card ou numa linha (o detalhe do Produto por Estoque é a Story 4.4, ainda não existe) — a única interação é expandir/recolher a linha agrupada da tabela.
- Nenhuma foto no card. A AC desta story só exige o badge de disponibilidade; exibir foto exige varrer o volume de fotos por Produto (`filepath.Glob` por id, sem tabela) — desproporcional aqui, endereçado quando a Story 4.4 construir a exibição real de foto do Produto.
- Nenhum SSE/`EventSource` (Story 4.4), nenhuma exportação Excel (Story 4.6), nenhum `fab-scanner`/QR Code (Story 4.5).
- Nenhum campo de "unidade de quantidade em estoque" — ele não existe no schema e está explicitamente fora de escopo desde a migração 000011. "mesmo nome/unidade/dimensões" da AC é lido como nome + as 5 dimensões estruturadas (cada uma já é um par `{valor, unidade}`).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Grade, página 1 | `?agrupar=false&pagina=1`, 30 Produtos | 24 Produtos ordenados por nome; `paginacao` = `{1,24,30,2}` | `200`, no error |
| Grade, Produto sem estoque | Produto sem nenhuma linha `produto_estoque` | `quantidadeTotal:0`, `disponivel:false`, ainda aparece na lista | `200`, no error |
| Tabela agrupa por nome+dimensões | 3 Produtos "Parafuso" mesmas dimensões, quantidades 10/5/2 em 2 Estoques | 1 grupo, `quantidadeTotal:17`, `porEstoque` com a soma por Estoque | `200`, no error |
| Tabela, dimensões distintas | 2 Produtos "Parafuso", comprimentos diferentes | 2 grupos separados | `200`, no error |
| Tabela, dimensões todas nulas | 2 Produtos "Cimento" sem nenhuma dimensão | agrupam num único grupo (`NULL` = `NULL`) | `200`, no error |
| Grupo sem linhas de estoque | grupo cujos Produtos não têm `produto_estoque` | `quantidadeTotal:0`, `porEstoque:[]`; expandir mostra "Sem quantidade registrada por estoque." | `200`, no error |
| Página além da última | `?pagina=99` | `{"produtos":[]...}` / `{"grupos":[]...}`, `paginacao.total` correto | `200`, no error |
| `pagina` inválida | `?pagina=0` / `?pagina=abc` | — | `400 VALIDATION_ERROR` "página inválida" |
| `agrupar` inválido | `?agrupar=talvez` | — | `400 VALIDATION_ERROR` "parâmetro agrupar inválido" |
| Papel `usuario` chama direto | token `usuario` | Handler executa normalmente | `200`, nunca `403` |
| Viewport < 768px | `matchMedia('(min-width:768px)').matches === false` | modo grade forçado, alternador ausente | client-side |
| Catálogo vazio | 0 Produtos | "Nenhum produto no catálogo.", nenhum controle de paginação | `200`, no error |

</intent-contract>

## Code Map

- `backend/migrations/000011_create_produtos.up.sql` / `000012_create_produto_estoque.up.sql` -- (somente leitura) schema base: `produtos` (nome, codigo, categoria_id, 5×`{dim}_valor`/`{dim}_unidade`), `produto_estoque(produto_id, estoque_id, quantidade NUMERIC(10,3))`, `estoques(id, nome)`. Sem coluna de unidade de quantidade — confirma o `Never` sobre "unidade".
- `backend/services/produtos.go:398-509` (`ProdutoBusca`, `BuscarProdutos`, `ListarCategorias`, `Categoria`) -- padrões de referência para os novos structs/queries (scan de `sql.NullString`, slice sempre não-`nil`, `Categoria` reutilizado no envelope).
- `backend/services/catalogo.go` (novo) -- `DimensaoValor{Valor float64; Unidade string}`, `DimensoesProduto` (5 ponteiros), `CatalogoItem`, `CatalogoGrupo`, `EstoqueQuantidade`, `Paginacao`; const `TamanhoPaginaCatalogo = 24`; `ListarCatalogoGrade(db *sql.DB, pagina int) ([]CatalogoItem, Paginacao, error)` e `ListarCatalogoAgrupado(db *sql.DB, pagina int) ([]CatalogoGrupo, Paginacao, error)`. Grade: query de contagem (`SELECT count(*) FROM produtos`) + query de página (`LEFT JOIN` a um agregado de `produto_estoque` por `produto_id`, `ORDER BY p.nome, p.id`, `LIMIT $1 OFFSET $2`). Agrupado: contagem de grupos (`SELECT count(*) FROM (SELECT 1 FROM produtos GROUP BY nome, <10 col dim>) t`) + query de página de grupos (`GROUP BY` nas mesmas colunas, `md5(...)` como `chave`, `array_agg(p.id)` para o mapeamento, `SUM` da quantidade via `LEFT JOIN produto_estoque`) + query de `porEstoque` (`WHERE pe.produto_id = ANY($1)` com todos os ids de produto da página, `JOIN estoques`, agregada em Go por grupo).
- `backend/services/produtos_test.go:1-40` (`limparProdutos`, `categoriaIDPorCodigo`, `testDB`) -- helpers do mesmo pacote reutilizados pelo novo `catalogo_test.go`.
- `backend/services/catalogo_test.go` (novo) -- `TestListarCatalogo*` cobrindo a matriz de I/O do lado do serviço (grade com/sem estoque, agrupamento por nome+dimensões, dimensões nulas agrupando, grupo sem estoque, paginação e contagem, página além da última).
- `backend/handlers/produtos.go:199-226` (`BuscarProdutosHandler`) -- molde exato para `ListarCatalogoHandler(db *sql.DB) http.HandlerFunc` (novo, logo após): guarda `UsuarioDaSessao`, lê `pagina`/`agrupar` de `r.URL.Query()`, valida (`400 VALIDATION_ERROR` com as mensagens do contrato), despacha para `ListarCatalogoGrade`/`ListarCatalogoAgrupado`, `escreverJSON(w, 200, ...)`; erro de banco ⇒ `500 INTERNAL_ERROR` + `slog`.
- `backend/handlers/produtos_test.go` -- novos `TestListarCatalogoHandler_*` (200 grade, 200 agrupado com `porEstoque`, 400 página inválida, 400 agrupar inválido, 200 para papel `usuario`).
- `backend/main.go:58` (doc comment do topo) e `backend/main.go:388-394` (bloco da Story 4.1) -- nova linha `mux.HandleFunc("GET /api/produtos/catalogo", middleware.RequireAuth(db, jwtSecret)(handlers.ListarCatalogoHandler(db)))` com comentário citando a Story 4.3; menção à Story 4.3 no doc comment.
- `backend/main_test.go:1612-1660` (`TestNewMux_ProdutosBuscaRotaSoRequireAuth`) -- molde para `TestNewMux_ProdutosCatalogoRotaSoRequireAuth` (sem token ⇒ 401; token `usuario` ⇒ 200, nunca 403).
- `frontend/src/components/catalogo/BuscaCatalogo.tsx:51-54,44-49` (`authHeaders`, interfaces `ProdutoBusca`/`CategoriaBusca`, uso de `font-mono` e `min-h-touch-target-min`) -- padrões a espelhar no novo componente.
- `frontend/src/components/catalogo/CatalogoListagem.tsx` (novo) -- `fetch` de `/api/produtos/catalogo?agrupar=<>&pagina=<>`, detecção de viewport via `matchMedia`, estado de modo (grade/tabela), grade de cards, tabela com linhas expansíveis, controles de paginação, estados carregando/erro/vazio, indicador de disponibilidade (ícone `lucide-react` + texto). Ícones sugeridos: `PackageCheck` / `PackageX`.
- `frontend/src/components/catalogo/CatalogoListagem.test.tsx` (novo) -- cobre a matriz do lado do cliente: grade como padrão sob `matchMedia` falso; alternância para tabela quando `window.matchMedia` é sobrescrito para `matches:true` (padrão já usado em `LogAcessoSection.test.tsx`); expandir linha agrupada mostra `porEstoque`; paginação; estados vazio e erro; badge com ícone + texto.
- `frontend/src/pages/CatalogoPage.tsx:35-38` -- remove o `<p>` "Visualização em grade e tabela chega em breve." e insere `<CatalogoListagem />` logo abaixo de `<BuscaCatalogo />`; ajusta o doc comment (linhas 8-27).
- `frontend/src/pages/CatalogoPage.test.tsx:30-41,54-56,67-69` -- remove as asserções do texto antigo (3 ocorrências); acrescenta a rota `/api/produtos/catalogo` ao mock de `fetch` (resposta grade vazia basta); a asserção "papel usuario não dispara fetch" passa a considerar que `CatalogoListagem` busca no mount (ajustar para o novo comportamento).
- `frontend/src/test/setup.ts:24-36` -- stub global de `window.matchMedia` já existe (`matches:false`) — os testes de tabela precisam sobrescrevê-lo localmente.

## Tasks & Acceptance

**Execution:**
- `backend/services/catalogo.go` (+ `catalogo_test.go`) -- structs do catálogo, `TamanhoPaginaCatalogo`, `ListarCatalogoGrade`, `ListarCatalogoAgrupado`; agrupamento por nome + 5 dimensões, somas por Produto e por Estoque, paginação/contagem na unidade certa (Produto vs. grupo), `chave` `md5` estável.
- `backend/handlers/produtos.go` (+ `produtos_test.go`) -- `ListarCatalogoHandler`: parse/validação de `pagina` e `agrupar`, despacho, envelope de resposta/erro.
- `backend/main.go` (+ `main_test.go`) -- registra `GET /api/produtos/catalogo` (só `RequireAuth`), atualiza doc comments, novo teste de wiring de rota.
- `frontend/src/components/catalogo/CatalogoListagem.tsx` (+ teste) -- listagem grade/tabela, alternador dependente de viewport ≥768px, linhas agrupadas expansíveis, paginação, estados carregando/erro/vazio, indicador de disponibilidade ícone + texto.
- `frontend/src/pages/CatalogoPage.tsx` (+ teste) -- integra `CatalogoListagem`, remove o texto "chega em breve", atualiza mocks/asserções.

**Acceptance Criteria:**
- Given o Catálogo em viewport ≥768px, when o Usuário alterna para a visualização em tabela, then Produtos com mesmo nome e mesmas dimensões estruturadas aparecem numa única linha com a soma das quantidades.
- Given o Catálogo em viewport <768px (a partir de 360px), when o Usuário o acessa, then a visualização padrão é em grade (cards), o alternador grade/tabela não aparece, e cada card mostra o indicador `status-disponivel` ("Disponível"/"Sem estoque") sempre com ícone + texto, nunca só cor.
- Given uma linha agrupada na tabela, when o Usuário a expande, then vê a quantidade discriminada por Estoque para aquele grupo (ou "Sem quantidade registrada por estoque." quando não há nenhuma).
- Given um Usuário com papel `usuario`, when ele chama `GET /api/produtos/catalogo` diretamente pela API, then a resposta é `200` (nunca `403` — a rota não leva `RequireRole`).
- Given `?pagina=0`, `?pagina=abc` ou `?agrupar=talvez`, when a requisição chega, then a resposta é `400 VALIDATION_ERROR` com a mensagem correspondente e nenhuma linha é consultada.
- Given o catálogo com mais Produtos/grupos que o tamanho de página, when o Usuário avança de página, then a listagem mostra o recorte seguinte e `paginacao` reflete `pagina`/`total`/`totalPaginas` corretos.

## Spec Change Log

## Review Triage Log

### 2026-08-31 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 8 (high 0, medium 0, low 8)
- defer: 0
- reject: 18 (low 18)
- addressed_findings:
  - `[low]` `[patch]` `CatalogoListagem` — o branch `catch` do `carregar` (falha de rede / `res.json()` que rejeita) não tinha nenhum teste; só a resposta não-OK era coberta, embora a spec exija o estado de erro "resposta não-OK **ou rede**". Achado pelo Verification Gap Reviewer e pelo Blind Hunter. Corrigido com um teste onde `fetch` rejeita, afirmando que o `role="alert"` aparece e a mensagem de vazio não.
  - `[low]` `[patch]` Estado "carregando" (nomeado na spec como um dos três estados) sem nenhuma asserção — apagar a linha `<p>Carregando catálogo...</p>` não quebraria nenhum teste. Achado pelo Blind Hunter e pelo Verification Gap Reviewer. Corrigido com um teste que afirma "Carregando catálogo..." antes de o `fetch` resolver, e `role="status"` acrescentado ao parágrafo de carregamento.
  - `[low]` `[patch]` Cobertura de frontend faltando: paginação no modo tabela (`agrupar=true`) e `formatarQuantidade` com quantidade fracionária (`15,5`, `NUMERIC(10,3)` do `produto_estoque`). Achado pelo Blind Hunter. Corrigido com testes de avanço de página no modo tabela e de renderização de quantidade fracionária.
  - `[low]` `[patch]` Linha de matriz "Página além da última" só tinha o meio `grade` coberto (`TestListarCatalogoGrade_PaginaAlemDaUltima`); o caminho agrupado tem lógica de retorno vazio própria (`preencherPorEstoque` pulado quando `len(todosIDs)==0`, slice `grupos` não-`nil`). Achado pelo Verification Gap Reviewer. Corrigido com `TestListarCatalogoAgrupado_PaginaAlemDaUltima`.
  - `[low]` `[patch]` `ListarCatalogoHandler` rejeitava `pagina` `<1`/não-numérica mas não um inteiro gigante porém parseável (ex. `pagina=400000000000000000`): `(pagina-1)*TamanhoPaginaCatalogo` estoura o `int`, gera `OFFSET` negativo e o Postgres devolve `500` em cima de input do usuário. Achado pelo Edge Case Hunter e pelo Blind Hunter. Corrigido rejeitando `pagina` acima de um teto são (`maxPaginaCatalogo`) com `400 VALIDATION_ERROR "página inválida"`, com teste.
  - `[low]` `[patch]` Botão "Próxima" da paginação não travava `pagina` em `totalPaginas` — cliques rápidos no último recorte pediam uma página além da última e a grade renderizava um `<ul>` vazio sem mensagem. Achado pelo Edge Case Hunter. Corrigido com `Math.min(totalPaginas, p + 1)` e desabilitando a navegação enquanto `carregando`.
  - `[low]` `[patch]` A navegação de paginação era renderizada mesmo com um único recorte (`totalPaginas > 0`), mostrando dois botões desabilitados e "Página 1 de 1". Achado pelo Blind Hunter. Corrigido para `totalPaginas > 1`.
  - `[low]` `[patch]` Comentário de cabeçalho de um teste em `catalogo_test.go` citava um nome de função (`..._NomeIgualDimensaoDiferenteNaoColapsa`) diferente do real (`..._NomeIgualDimensaoParcialSepara`). Achado pelo Blind Hunter. Corrigido.

### 2026-08-31 — Review pass (follow-up)
- intent_gap: 0
- bad_spec: 0
- patch: 3 (high 0, medium 0, low 3)
- defer: 0
- reject: 27 (low 27)
- addressed_findings:
  - `[low]` `[patch]` `resumirDimensoes` (`CatalogoListagem.tsx`) interpolava `dim.valor` cru (`${dim.valor}${dim.unidade}` → "6.5m"), enquanto `formatarQuantidade` usa `toLocaleString('pt-BR')` ("6,5") — decimal com ponto e com vírgula na mesma tela. Achado pelo Blind Hunter. Corrigido: `resumirDimensoes` passa a formatar o valor com `formatarQuantidade`.
  - `[low]` `[patch]` `resumirDimensoes` era renderizada na célula "Dimensões" de toda linha da tabela agrupada mas não tinha nenhuma asserção — um regressão (rótulo errado, separador perdido, `—` virando `''`) passaria a suíte inteira verde. Achado pelo Verification Gap Reviewer e pelo Blind Hunter. Corrigido com um teste que afirma `C 6,5m · ⌀ 10cm` para um grupo com dimensões e `—` para um grupo totalmente nulo.
  - `[low]` `[patch]` Atribuições mortas `p1 := ...; _ = p1` (captura-e-descarta) em `backend/handlers/produtos_test.go` e `backend/services/catalogo_test.go` — a função de seed já roda pelo efeito colateral e Go não exige usar o retorno. Achado pelo Blind Hunter. Corrigido removendo a atribuição.
- rejected (amostra do raciocínio):
  - Grade usa `JOIN categorias` mas conta com `count(*) FROM produtos` → "Produtos sem categoria somem": `categoria_id UUID NOT NULL REFERENCES categorias(id)` — o inner join nunca descarta linha, a contagem sempre bate. Não é defeito.
  - `agrupar=true` não devolve `categoria` / Produtos de categorias diferentes colapsam: o envelope da spec para `agrupar=true` omite `categoria` de propósito; a chave de grupo é, por contrato, "mesmo `nome` + mesmas 5 dimensões". Por design.
  - Teto `MaxPaginaCatalogo` desloca páginas > 1.000.000 de "200 vazio" para "400": decisão deliberada já registrada e revisada no pass anterior deste log (guard de overflow de `(pagina-1)*24`); não se reabre.
  - `status-disponivel` citado como classe literal: não existe nenhuma classe/token `status-disponivel` no código; a AC observável (ícone + texto, nunca só cor) é atendida por `IndicadorDisponibilidade` (ícone `lucide` + texto + `data-status`).
  - `porEstoque`/`paginacao` ausentes no 200, `addEventListener` sem fallback `addListener`, ausência de transação/escopo de tenant, retry no estado de erro, `aria-controls` na linha expansível: ou contrariam o contrato explícito do backend/da intent, ou são defesa contra violação do próprio contrato, ou polimento fora de escopo.
- verification: `gofmt`/`go build`/`go vet` limpos; `go test -p 1 -count=1 ./...` — todos os pacotes OK (handlers 92s, services 72s); `npm run lint`/`npm run build` OK; `npm run test` — 285/285 (a falha isolada de `CadastroProdutoSection` no 1º run foi timeout por carga da máquina; passou sozinha e no 2º run completo).

## Design Notes

- **"unidade" da AC = as 5 dimensões estruturadas.** Não existe coluna de unidade de quantidade em `produtos` (a migração 000011 declara isso explicitamente fora de escopo, e nenhum PRD/épico/UX a define). Cada dimensão já é um par `{valor, unidade}` (AD-9), então "mesmo nome/unidade/dimensões" é satisfeito agrupando por `nome` + os 10 campos de dimensão. Reintroduzir um campo de unidade seria migração nova — contraria o `Never` e a decisão do Epic 3.
- **Paginação na unidade das linhas.** Se o backend paginasse Produtos e o frontend agrupasse só a página, um grupo dividido na fronteira da página somaria errado. Por isso `agrupar=true` conta e recorta **grupos** no SQL (`GROUP BY` + `count` sobre subconsulta), e o `porEstoque` é resolvido por `array_agg(p.id)` + um `WHERE pe.produto_id = ANY(...)` — sem `IN` composto com `NULL`.
- **Sem foto no card / sem índice novo.** A AC pede só o badge; foto exige varredura de filesystem por Produto (`filepath.Glob`, sem tabela) — fora de escopo aqui. Volume real (~8.000 Produtos, 30 Estoques) roda `GROUP BY`/`SUM`/`count` em baixos milissegundos sem `pg_trgm`/índice; a checagem de NFR é manual via `EXPLAIN ANALYZE` (mesmo critério da Story 4.1).
- **Alternância grade/tabela é estado, não CSS.** O `AppShell` faz responsividade só com classes `md:`, mas aqui a diferença é comportamental (modo default + presença do alternador), então é preciso `matchMedia` em JS com listener e cleanup; os testes de tabela sobrescrevem `window.matchMedia`.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` -- sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` -- cobre `TestListarCatalogo*`, `TestListarCatalogoHandler_*` e `TestNewMux_ProdutosCatalogoRotaSoRequireAuth`.
- `cd frontend && npm run lint && npm run build && npm run test` -- `oxlint`, `tsc`+`vite` e os testes de `CatalogoListagem`/`CatalogoPage` passam.

**Manual checks (if no CLI):**
- Semear ~8.000 Produtos + linhas em `produto_estoque` (30 Estoques) e rodar `EXPLAIN ANALYZE` das queries de `ListarCatalogoGrade` e `ListarCatalogoAgrupado` direto no Postgres do `docker compose` -- tempo de execução bem abaixo de 300ms confirma que nenhum índice novo é necessário.
- `docker compose up --build`, logar como `usuario`: em viewport larga, alternar grade↔tabela; na tabela, expandir uma linha e conferir a quantidade por Estoque; estreitar a janela abaixo de 768px e confirmar que volta para grade e o alternador some; paginar até a última página.

## Auto Run Result

Status: done

### Resumo da mudança

Story 4.3 já estava implementada e finalizada (`done`); esta invocação foi um **pass de revisão follow-up** (`followup_review_recommended: true`). A funcionalidade: novo `GET /api/produtos/catalogo` (só `RequireAuth`) com dois modos — `agrupar=false` (grade, um Produto por linha, `quantidadeTotal` somada, `disponivel`) e `agrupar=true` (tabela agrupada por `nome` + 5 dimensões estruturadas, `porEstoque` embutido para a expansão) — e o componente `CatalogoListagem` na `CatalogoPage`, com alternador grade/tabela dependente de viewport ≥768px, linhas expansíveis, paginação numérica e estados carregando/erro/vazio.

Nenhum `intent_gap` nem `bad_spec` neste pass; 3 patches de baixa severidade aplicados.

### Arquivos alterados neste pass

- `frontend/src/components/catalogo/CatalogoListagem.tsx` — `resumirDimensoes` passa a formatar o valor da dimensão com `formatarQuantidade` (decimal pt-BR), eliminando "6.5m" vs "6,5" na mesma tela.
- `frontend/src/components/catalogo/CatalogoListagem.test.tsx` — novo teste da célula "Dimensões" da tabela agrupada: `C 6,5m · ⌀ 10cm` para grupo com dimensões, `—` para grupo totalmente nulo.
- `backend/handlers/produtos_test.go` — removida atribuição morta `p1 := ...; _ = p1` em `TestListarCatalogoHandler_200AgrupadoComPorEstoque`.
- `backend/services/catalogo_test.go` — removida atribuição morta `p1 := ...; _ = p1` em `TestListarCatalogoAgrupado_AgrupaPorNomeEDimensoes`.
- `_bmad-output/implementation-artifacts/spec-4-3-visualizacao-em-grade-e-tabela-agrupada.md` — nova entrada no Review Triage Log e este bloco.

### Findings deste pass

- **patches aplicados: 3** — todos `low` (formatação decimal inconsistente em `resumirDimensoes`; ausência de teste da célula "Dimensões"; atribuições mortas em 2 testes de backend).
- **deferidos: 0**
- **rejeitados: 27** (`low`) — ver Review Triage Log para a amostra do raciocínio (inner join × count não é defeito por causa do `NOT NULL REFERENCES`; ausência de `categoria` no modo agrupado é por design do envelope; teto `MaxPaginaCatalogo` é decisão já revisada; `status-disponivel` não tem referente no código; defesas contra violação do próprio contrato do backend; polimento de UX fora de escopo).

### Recomendação de novo review

`followup_review_recommended: false` — patches deste pass: 0 high, 0 medium, 3 low. Score = 3×0 + 1×3 = 3 (< 5) e nenhum high.

### Verificação executada

- `cd backend && gofmt -l .` — sem saída.
- `cd backend && go build ./... && go vet ./...` — limpos.
- `cd backend && go test -p 1 -count=1 ./...` — todos os pacotes OK (`handlers` 92.0s, `services` 71.6s, demais < 11s). Obs.: rodar `handlers` + `services` em paralelo (sem `-p 1`) polui o estado do banco compartilhado e faz o helper de login devolver 500 — por isso a spec exige `-p 1`; com `-p 1` tudo passa.
- `cd frontend && npm run lint` — `oxlint` limpo.
- `cd frontend && npm run build` — `tsc -b` + `vite build` OK.
- `cd frontend && npm run test` — 285/285. No 1º run completo, `CadastroProdutoSection.test.tsx` teve 1 timeout (5s) sob carga da máquina; passou isolado (25/25) e no 2º run completo (285/285). Não tem relação com esta story.

### Riscos residuais

- Formatação de números depende de ICU completo no runtime (`toLocaleString('pt-BR')`); o CI usa Node com ICU completo e os testes já dependiam disso desde o pass anterior.
- A checagem de NFR (300ms p95, sem índice novo) continua sendo manual via `EXPLAIN ANALYZE` com volume semeado — não coberta por teste automatizado (mesmo critério das Stories 4.1–4.3).

