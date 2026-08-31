---
title: 'Story 4.1 — Busca por nome/código/categoria com sugestões'
type: 'feature'
created: '2026-08-31'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: '62b98b201cba707582c192ae8583e4bc11021046'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** O Catálogo (`/`) hoje só mostra um aviso "chega em breve" para busca — qualquer Usuário autenticado (papel mínimo `usuario`) não tem nenhuma forma de encontrar um Produto por nome, código ou categoria, o que é o ponto de entrada mais comum do produto (FR-4).

**Approach:** Novo endpoint `GET /api/produtos/busca?q=<termo>` (só `RequireAuth`, qualquer papel) devolve até 7 Produtos ranqueados por relevância (match exato > prefixo > substring, em nome/código/nome da categoria). No frontend, `CatalogoPage` ganha um campo de busca com atalho `/` (desktop) que, a cada digitação (debounce), busca e renderiza a lista de sugestões inline na própria tela — sem dropdown flutuante, já que a navegação ao selecionar um resultado depende do detalhe do Produto (Story 4.4, ainda não existe).

## Boundaries & Constraints

**Always:**
- `GET /api/produtos/busca`: `RequireAuth` apenas (sem `RequireRole`), mesmo padrão de `GET /api/categorias`/`GET /api/estoques` — qualquer conta autenticada, papel mínimo `usuario`.
- Parâmetro de query `q`: `strings.TrimSpace`; vazio (ausente ou só espaços) -> `400 VALIDATION_ERROR`, mensagem "termo de busca obrigatório". Nenhum tamanho mínimo de caracteres é exigido além de não-vazio — nenhuma AC fixa um mínimo, e o `LIMIT 7` já contém o custo de termos curtos.
- Caracteres especiais de `LIKE`/`ILIKE` (`%`, `_`, `\`) no termo são escapados antes de compor o padrão (`ESCAPE '\'`) — um código de Produto contendo `_` (comum em SKUs) não deve virar wildcard não intencional.
- Query ranqueia com `CASE` em SQL: rank 0 = `nome`/`codigo` == termo (case-insensitive); rank 1 = `nome`/`codigo` começa com o termo; rank 2 = `categorias.nome` == ou começa com o termo; rank 3 = qualquer outro match por substring em `nome`/`codigo`/`categorias.nome`. `ORDER BY rank ASC, nome ASC LIMIT 7`. `WHERE` exige match (substring, `ILIKE '%termo%'`) em pelo menos um dos três campos — sem match em nenhum, nenhuma linha.
- Resposta: `200 {"produtos":[{"id","nome","codigo","categoria":{"id","codigo","nome"}}, ...]}` (até 7, `[]` quando nenhum match — nunca `null`, mesmo padrão de `ListarCategorias`). `codigo` é `null` quando o Produto não tem código cadastrado (coluna opcional).
- Frontend: campo de busca sempre visível no topo do conteúdo de `CatalogoPage`, para qualquer papel (`usuario`+) — não depende do gate `podeCadastrar` existente. Digitação dispara a busca só após um debounce (300ms, decisão desta spec — nenhum documento de planejamento fixa um valor); resposta de uma requisição obsoleta (usuário já digitou um termo diferente antes dela voltar) é descartada, nunca sobrescreve a lista com um resultado que não corresponde mais ao termo atual (mesma classe de corrida endereçada na Story 3.5 para upload de foto).
- Enquanto o termo está vazio, nenhuma requisição é disparada e nenhuma lista/mensagem de resultado aparece — só o texto "chega em breve" atualizado (ver `Never`).
- Resultado de uma busca com 1+ Produtos: lista simples abaixo do campo, cada item mostrando nome, código (fonte `JetBrains Mono`, quando presente) e nome da categoria — sem badge de disponibilidade/quantidade (isso é Story 4.3/4.4, fora do escopo aqui).
- Resultado de uma busca completa (após o debounce resolver) sem nenhum Produto: mensagem exata `Nenhum produto encontrado para '{busca}'.` (com o termo efetivamente buscado, já sem espaços nas pontas), sem qualquer sugestão de compra externa.
- Atalho de teclado `/` (UX epic-level) foca o campo de busca quando o foco não está já em outro campo editável (`input`/`textarea`/`[contenteditable]`) e nenhum modificador (`ctrl`/`meta`/`alt`) está pressionado — só ativo em desktop por natureza (o teclado físico não está disponível em mobile, nenhuma branch de viewport é necessária).
- Alvo de toque mínimo 48px no campo de busca e em cada item de resultado (NFR de usabilidade em campo).

**Block If:** nenhuma condição desta story exige decisão humana em runtime — segue direto.

**Never:**
- Nenhuma migração nova / nenhum índice novo (`pg_trgm`, `tsvector`, GIN): 8.000 linhas em `produtos` com `JOIN` em `categorias` (25 linhas) é um volume trivial para um `ILIKE` sequencial em Postgres — a NFR de 300ms p95 não exige infraestrutura de full-text que a base de dados atual não tem em nenhum outro lugar do repositório.
- Nenhuma navegação ao clicar/selecionar um item da lista de resultados — o detalhe do Produto por Estoque é a Story 4.4, ainda não implementada; um item de resultado é só informativo (texto), nunca um link/botão que leva a lugar nenhum.
- Nenhum filtro por categoria/estoque/disponibilidade (Story 4.2) nem alternância grade/tabela (Story 4.3) — a lista de resultados desta story é sempre uma lista simples, não um grid nem uma tabela agrupada.
- Nenhuma alteração em `middleware/roles.go`, `services/papel.go` ou nas rotas/gates já existentes de Produtos — só uma rota nova (`GET`), sem tocar nas existentes.
- Nenhum SSE/`EventSource` — atualização em tempo real é a Story 4.4, não esta.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Match exato de nome ou código | `q=PAR-001`, Produto com `codigo="PAR-001"` | Esse Produto em 1º lugar (rank 0) | `200`, no error |
| Match por prefixo | `q=paraf`, Produtos "Parafuso Sextavado", "Parafina" | Ambos retornam, ordenados por rank 1 depois nome | `200`, no error |
| Match só por categoria | `q=elétric`, categoria "Material Elétrico" | Produtos dessa categoria aparecem (rank 2), mesmo sem match em nome/código | `200`, no error |
| Mais de 7 matches | 10 Produtos batendo `q=parafuso` | Só os 7 melhor ranqueados voltam | `200`, no error |
| Nenhum match | `q=xyzxyz-inexistente` | `{"produtos":[]}`; UI mostra "Nenhum produto encontrado para 'xyzxyz-inexistente'." | `200`, no error |
| Termo vazio/só espaços | `q=` ou `q=%20%20` | Nenhuma linha consultada | `400 VALIDATION_ERROR` |
| Termo com `%`/`_` literais | `q=50%` (Produto código contém `50%` literalmente) | `%` tratado como caractere literal, não wildcard | `200`, no error |
| Usuário digita rápido, respostas fora de ordem | busca "a" (lenta) seguida de "ab" (rápida) | Lista final reflete "ab" — resposta de "a" chegando depois é descartada | `200`, no error (client-side) |
| Papel `usuario` chama a rota direto pela API | token `usuario` | Handler executa normalmente (sem `RequireRole`) | `200`, nunca `403` |

</intent-contract>

## Code Map

- `backend/services/produtos.go:38-54` (`Produto`, `Categoria`) -- structs existentes de referência; novo `ProdutoBusca{ID, Nome string; Codigo *string; Categoria Categoria}` e `BuscarProdutos(db *sql.DB, termo string) ([]ProdutoBusca, error)` entram no mesmo arquivo, logo após `ListarCategorias` (~linha 415).
- `backend/services/produtos.go` -- helper `escaparCoringasLike(s string) string` (escapa `\`, `%`, `_`) usado por `BuscarProdutos` ao montar o padrão `ILIKE`.
- `backend/services/produtos_test.go` -- novos casos `TestBuscarProdutos_*` cobrindo a matriz acima (ranking, limite de 7, escaping, termo vazio devolve erro de validação — na verdade a validação de vazio fica no handler, não no service; o service assume termo não-vazio e já trimado).
- `backend/handlers/produtos.go:1-30` (imports, doc comment do arquivo) -- `BuscarProdutosHandler(db)` novo, mesmo molde de `ListarCategoriasHandler` (`produtos.go:121-138`): guarda `UsuarioDaSessao`, lê `r.URL.Query().Get("q")`, valida vazio -> `escreverErro(w, 400, "VALIDATION_ERROR", "termo de busca obrigatório")`, chama `services.BuscarProdutos`, `escreverJSON(w, 200, map[string]any{"produtos": resultado})`.
- `backend/handlers/produtos_test.go` -- novos `TestBuscarProdutosHandler_*` (200 com resultados, 200 vazio, 400 termo vazio, 200 para papel `usuario` sem `RequireRole`).
- `backend/main.go:355-384` (bloco de rotas de Produtos/Fotos) -- nova linha `mux.HandleFunc("GET /api/produtos/busca", middleware.RequireAuth(db, jwtSecret)(handlers.BuscarProdutosHandler(db)))`, com comentário próprio citando Story 4.1; doc comment do topo do arquivo (linha ~56-58) ganha a menção à Story 4.1.
- `backend/main_test.go:623` (`TestNewMux_EstoquesRotaCarregaRequireRole`) -- molde do subteste "GET ... NÃO leva RequireRole: token usuario -> 200" a reaproveitar; novo `TestNewMux_ProdutosBuscaRotaSoRequireAuth` (sem token -> 401; token `usuario` -> 200, nunca 403).
- `frontend/src/pages/CatalogoPage.tsx:24-49` -- remove o texto fixo "Busca e visualização do catálogo chegam em breve." e insere `<BuscaCatalogo />` no topo; texto residual passa a citar só o que ainda falta ("Visualização em grade e tabela chega em breve.").
- `frontend/src/pages/CatalogoPage.test.tsx:50-56` -- assertions do texto antigo atualizadas para o novo texto residual; mock de `fetch` ganha (quando necessário) a rota `/api/produtos/busca`.
- `frontend/src/components/catalogo/BuscaCatalogo.tsx` (novo) -- campo de busca (shadcn `Input`, ícone `Search` do `lucide-react`), debounce, guarda de corrida por termo mais recente, atalho `/`, lista de resultados / mensagem de "nenhum produto encontrado"; usa `authHeaders()` local (mesmo padrão duplicado em `CadastroProdutoSection.tsx:110`) e `getAccessToken` de `frontend/src/lib/session.ts`.
- `frontend/src/components/catalogo/BuscaCatalogo.test.tsx` (novo) -- cobre a matriz de I/O do lado do cliente (debounce, ranking exibido, mensagem vazia com o termo certo, descarte de resposta obsoleta, atalho `/`).

## Tasks & Acceptance

**Execution:**
- `backend/services/produtos.go` (+ teste) -- `ProdutoBusca`, `escaparCoringasLike`, `BuscarProdutos` -- query ranqueada com `CASE`/`LIMIT 7`, escaping de coringas.
- `backend/handlers/produtos.go` (+ teste) -- `BuscarProdutosHandler`: parse/validação de `q`, chamada ao service, envelope de resposta/erro.
- `backend/main.go` (+ `main_test.go`) -- registra `GET /api/produtos/busca` (só `RequireAuth`), atualiza doc comments, novo teste de wiring de rota.
- `frontend/src/components/catalogo/BuscaCatalogo.tsx` (+ teste) -- campo de busca, debounce, guarda de corrida, atalho `/`, lista de resultados, mensagem de vazio.
- `frontend/src/pages/CatalogoPage.tsx` (+ teste) -- integra `BuscaCatalogo`, atualiza texto residual de "chega em breve".

**Acceptance Criteria:**
- Given o campo de busca do Catálogo, when o Usuário digita alguns caracteres, then até 7 sugestões aparecem, ordenadas por relevância, atualizando conforme ele digita (respeitando o debounce e descartando respostas obsoletas).
- Given uma busca sem nenhum resultado, when o Usuário completa a digitação, then a tela mostra "Nenhum produto encontrado para '{busca}'.", sem sugestão de comprar externamente.
- Given a NFR de desempenho (≤300ms p95, até 8.000 produtos/30 estoques), when a busca é executada sob carga típica, then o tempo de resposta cumpre esse limite (validado via `EXPLAIN ANALYZE` com dados semeados — ver Verification).
- Given um Usuário com papel `usuario`, when ele chama `GET /api/produtos/busca` diretamente pela API, then a resposta é `200` (nunca `403` — a rota não leva `RequireRole`).
- Given o campo de busca em foco por engano fora de um outro campo editável, when o Usuário pressiona `/` no desktop, then o foco vai para o campo de busca sem digitar `/` nele.

## Spec Change Log

## Review Triage Log

### 2026-08-31 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 5 (high 0, medium 1, low 4)
- defer: 0
- reject: 14 (low 14)
- addressed_findings:
  - `[low]` `[patch]` `buscarProdutosQuery` (services/produtos.go) tinha `ORDER BY rank ASC, p.nome ASC` sem desempate final — dois Produtos com mesmo rank e mesmo `nome` podem trocar de ordem entre execuções (Postgres não garante ordem física estável), o que pode alterar QUAIS 7 aparecem quando o empate cai exatamente na fronteira do `LIMIT 7`. Achado pelo Blind Hunter e pelo Edge Case Hunter, independentemente. Corrigido acrescentando `p.id ASC` como desempate final.
  - `[low]` `[patch]` `BuscarProdutosHandler` não limitava o tamanho de `q` — um termo arbitrariamente longo chega inteiro ao `ILIKE`, inconsistente com o teto de 255 já aplicado a `nome`/`codigo` em `CriarProduto`. Achado pelo Blind Hunter. Corrigido rejeitando `q` com mais de 255 runes com `400 VALIDATION_ERROR`.
  - `[low]` `[patch]` `BuscaCatalogo.tsx` não guardava contra `setState` após desmontar com um `fetch` ainda em voo — o `termoAtualRef` só descarta respostas de um termo DIFERENTE, não uma resposta tardia do MESMO termo chegando depois do componente sair da árvore. Achado pelo Blind Hunter e pelo Edge Case Hunter, independentemente. Corrigido com uma flag de montagem (`montadoRef`) checada antes de cada `setErro`/`setResultados` nos callbacks `.then`/`.catch`, mais um teste cobrindo desmontagem com requisição pendente.
  - `[medium]` `[patch]` O branch de erro de `BuscaCatalogo` (`res.ok === false` e `fetch` rejeitado, ambos disparando `setErro(true)` e a mensagem `role="alert"`) não tinha NENHUM teste cobrindo — toda resposta mockada em `BuscaCatalogo.test.tsx`/`CatalogoPage.test.tsx` resolve `ok: true`. Achado pelo Verification Gap Reviewer. Corrigido com um novo `describe` cobrindo resposta `ok: false` e rejeição de rede, além de uma asserção extra confirmando a classe de alvo de toque (`min-h-touch-target-min`) no campo de busca (gap menor apontado pelo Intent Alignment Auditor).
  - `[low]` `[patch]` Campo de busca usava `type="text"` — `type="search"` é semanticamente mais correto para este caso de uso (botão nativo de limpar, teclado/IME mobile apropriado) sem custo nem risco aos testes existentes. Achado pelo Blind Hunter. Corrigido.
  - `[reject]` Nenhum tamanho mínimo de caracteres antes de consultar o banco a cada tecla — a própria spec (Boundaries `Always`) já decide isso explicitamente ("nenhuma AC fixa um mínimo, e o LIMIT 7 já contém o custo"), e a checagem manual de NFR feita na etapa de Verify (8.000 linhas semeadas, `EXPLAIN ANALYZE` ~13ms) confirma a decisão na prática.
  - `[reject]` Busca não é insensível a acento (“eletrico” não encontra “Elétrico”) — nenhum documento de planejamento (PRD/épicos/UX) exige normalização de acento; a spec já documentou deliberadamente um ranking simples por `CASE` sem infraestrutura de similaridade/normalização (Design Notes). Corrigir direito exigiria a extensão `unaccent` (uma migração), o que colidiria com o `Never` da própria spec — fora do escopo desta story tal como o intent a define.
  - `[reject]` Código (`categorias.codigo`, ex. "04.001") não é pesquisável, só o nome da categoria — leitura defensável do enunciado "busca por... categoria" como o NOME da categoria (o dado voltado ao usuário), não o código interno usado só em listas internas.
  - `[reject]` `LIMIT 7` sem sinalizar que existem mais resultados — bate exatamente com a AC ("até 7 sugestões"), nenhuma AC pede indicação de resultados adicionais.
  - `[reject]` Nenhum estado de carregamento (spinner/"buscando...") entre o debounce disparar e a resposta chegar — nenhuma AC/NFR exige isso; com o volume real (8.000 linhas, ~13ms de execução) a latência percebida é mínima, e adicionar um estado de loading só para isso seria escopo além do pedido.
  - `[reject]` Toda resposta não-OK (incluindo um eventual 401 por sessão expirada) cai na mesma mensagem genérica de erro — mesmo padrão de qualquer outro `fetch` local no app hoje (nenhum componente irmão tem tratamento especial de 401); não é uma regressão introduzida por esta story.
  - `[reject]` Ausência de `aria-live` anunciando resultados/erro da busca para leitor de tela — nenhuma AC/NFR desta story exige isso (o único `aria-live` do épico é o toast de SSE da Story 4.4); acessibilidade geral do app não é um requisito estabelecido em nenhum documento de planejamento para esta story especificamente.
  - `[reject]` Lista de resultados não segue o padrão ARIA de combobox (`role="listbox"`, navegação por seta) — contraria diretamente a decisão de design já registrada na spec ("Sem dropdown flutuante, resultado inline na tela").
  - `[reject]` Itens de resultado são inertes, sem clique/navegação — exatamente o que o `Never` da spec proíbe nesta story (Story 4.4, detalhe do Produto, ainda não existe).
  - `[reject]` Ausência de rate limiting no endpoint — nenhum outro endpoint do backend tem rate limiting; fora do escopo desta story introduzir esse padrão isoladamente.
  - `[reject]` Atalho `/` sem um handler de `Esc` para desfocar/limpar o campo — `Esc` já tem um significado específico e diferente no épico (fechar a câmera do scanner, Story 4.5); não é uma lacuna desta story.
  - `[reject]` `produto.codigo` vazio (`""`) ficaria indistinguível de `null` na UI — premissa falsa: `services.CriarProduto` (produtos.go:188-193, 261-264) só grava `codigo` como `NULL` ou um valor trimado não-vazio; `""` nunca é persistido, então esse estado é inalcançável.
  - `[reject]` Atalho `/` testado só isoladamente, nunca dentro da árvore real de `CatalogoPage` (com `Tabs`/formulários coexistindo) — a lógica de guarda (campo editável/modificador) já está coberta a nível de unidade, que é a superfície de risco real; um teste de integração redundante seria cobertura desproporcional ao risco (mesmo padrão de rejeição já usado na Story 3.5 para nitpicks de cobertura desproporcional).
  - `[reject]` Guarda de campo editável (`elementoEhEditavel`) testada só para `<input>`, não para `<textarea>`/`[contenteditable]` — o próprio Verification Gap Reviewer marcou este ponto como não atingindo a régua de achado por si só.

## Design Notes

- **Sem dropdown flutuante, resultado inline na tela**: a AC "a tela mostra 'Nenhum produto encontrado...'" fala da TELA, não de um menu suspenso — e como a Story 4.3 (grade/tabela) e a Story 4.4 (detalhe, destino de um clique) ainda não existem, tratar os até-7 resultados como uma lista informativa simples (sem link/clique) evita inventar uma navegação que esta story não tem para onde levar, e evita descartar depois um componente de dropdown/combobox que a 4.3 substituiria de qualquer forma.
- **Sem índice novo de busca**: 8.000 linhas é um volume pequeno para Postgres — um `ILIKE` sequencial com `JOIN` em 25 linhas de `categorias` roda em baixos milissegundos sem `pg_trgm`/`tsvector`; introduzir essa infraestrutura agora seria complexidade sem benefício mensurável para o volume real do NFR (ver Verification para a checagem manual que confirma isso, em vez de assumir).
- **Ranking por `CASE`, não por `similarity()`**: sem `pg_trgm` habilitado (decisão acima), uma pontuação de similaridade real não está disponível; a hierarquia exato > prefixo > substring com `ORDER BY rank, nome` é determinística, fácil de testar e suficiente para "até 7 sugestões por relevância" sem inventar uma métrica de similaridade fuzzy que nenhuma AC pede.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` -- sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` -- cobre `TestBuscarProdutos_*`, `TestBuscarProdutosHandler_*` e `TestNewMux_ProdutosBuscaRotaSoRequireAuth`.
- `cd frontend && npm run lint && npm run build && npm run test` -- `oxlint`, `tsc`+`vite` e os testes de `BuscaCatalogo`/`CatalogoPage` passam.

**Manual checks (if no CLI):**
- Semear ~8.000 Produtos (script ad-hoc ou `INSERT ... SELECT generate_series`) e rodar `EXPLAIN ANALYZE SELECT ...` (a query de `BuscarProdutos`) direto no Postgres do `docker compose` -- tempo de execução reportado bem abaixo de 300ms confirma que nenhum índice novo é necessário para a NFR.
- `docker compose up --build`, logar como `usuario`, digitar no campo de busca do Catálogo um nome/código/categoria conhecido -- sugestões aparecem ranqueadas; digitar algo inexistente -- mensagem exata de "nenhum produto encontrado" com o termo digitado; pressionar `/` fora de um campo -- foco vai para a busca.

## Auto Run Result

**Resumo da implementação:** Story 4.1 entregue de ponta a ponta — novo endpoint `GET /api/produtos/busca?q=<termo>` (só `RequireAuth`, qualquer papel `usuario`+) rankeando até 7 Produtos por relevância (exato > prefixo > categoria > substring) via `CASE`/`ILIKE` com escaping de coringas, sem migração/índice novo; no frontend, `BuscaCatalogo` (novo componente) fica sempre visível no topo de `CatalogoPage`, com debounce de 300ms, descarte de resposta obsoleta (por termo E por desmontagem), atalho de teclado `/`, e mensagem exata de "nenhum produto encontrado" — sem dropdown/navegação (fora do escopo, depende da Story 4.4).

**Arquivos alterados:**
- `backend/services/produtos.go` -- `ProdutoBusca`, `escaparCoringasLike`, `buscarProdutosQuery` (rank por `CASE`, `LIMIT 7`, desempate final por `id`), `BuscarProdutos`.
- `backend/services/produtos_test.go` -- 7 novos testes cobrindo a matriz de I/O (ranking, limite de 7, coringas literais, código ausente).
- `backend/handlers/produtos.go` -- `BuscarProdutosHandler` (trim, teto de 255 runes, `400`/`200`, sem `RequireRole`).
- `backend/handlers/produtos_test.go` -- 6 novos testes (200 com/sem resultado, 400 termo vazio, 400 termo muito longo, 401, 200 para `usuario`).
- `backend/main.go` -- registro de `GET /api/produtos/busca` (só `RequireAuth`) + doc comments atualizados.
- `backend/main_test.go` -- `TestNewMux_ProdutosBuscaRotaSoRequireAuth` (prova que a rota real não leva `RequireRole`).
- `frontend/src/components/catalogo/BuscaCatalogo.tsx` (novo) -- campo de busca, debounce, guarda de corrida por termo E por desmontagem, atalho `/`, lista de resultados, mensagem de vazio/erro, `type="search"`.
- `frontend/src/components/catalogo/BuscaCatalogo.test.tsx` (novo) -- 11 testes (vazio, debounce, campos exibidos, mensagem vazia, corrida por termo, desmontagem com fetch pendente, erro `ok:false`/rede, atalho `/` em 3 variações).
- `frontend/src/pages/CatalogoPage.tsx` -- integra `<BuscaCatalogo />` no topo; texto residual "Visualização em grade e tabela chega em breve."
- `frontend/src/pages/CatalogoPage.test.tsx`, `frontend/src/App.test.tsx` -- assertions do texto antigo atualizadas.
- `_bmad-output/implementation-artifacts/epic-4-context.md` (novo) -- contexto do Epic 4 compilado para esta e futuras stories do épico.

**Findings da revisão:** 4 reviewers em paralelo (Blind Hunter, Edge Case Hunter, Verification Gap Reviewer, Intent Alignment Auditor) contra o diff completo desde `baseline_revision`. 5 patches aplicados (0 alta, 1 média, 4 baixas): desempate final por `id` no ranking, teto de 255 runes em `q`, guarda contra `setState` pós-desmontagem (+ teste), cobertura de teste do estado de erro (+ asserção de alvo de toque), `type="search"`. 14 achados rejeitados (busca sem acento, categoria por código, ausência de "mais resultados", sem loading, 401 genérico, sem `aria-live`, sem padrão combobox, itens inertes, sem rate limiting, sem `Esc`, código vazio impossível, atalho `/` só testado isolado, guarda de campo editável parcialmente testada, tamanho mínimo de termo) — todos com justificativa registrada no `## Review Triage Log`, a maioria por decisão explícita já documentada na própria spec (`Never`/`Design Notes`) ou por ausência de exigência em qualquer documento de planejamento. 0 intent_gap, 0 bad_spec, 0 defer.

**Recomendação de revisão de acompanhamento:** `true` — patches desta passada: 0 alta, 1 média, 4 baixas; `3×1 + 1×4 = 7 ≥ 5`.

**Verificação realizada:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` -- limpo (re-executado de forma independente após os patches).
- `cd backend && go test -p 1 -count=1 ./...` -- todos os 7 pacotes `ok`, incluindo os testes novos/patchados, verificados individualmente com `-v -run`.
- `cd frontend && npm run lint && npm run build && npm run test` -- `oxlint` limpo, `tsc`+`vite build` limpo, 266/266 testes passando (11/11 em `BuscaCatalogo.test.tsx`, verificados individualmente com `--reporter=verbose`).
- NFR de desempenho (≤300ms p95, 8.000 Produtos): checagem manual feita durante a etapa de Verify (antes dos patches) -- ~8.000 linhas semeadas em Postgres local, `EXPLAIN ANALYZE` na query de `BuscarProdutos` (termo estreito e termo pior-caso batendo tudo) reportou ~13ms, bem abaixo do limite; dados de seed removidos após a checagem.
- Auditoria da Matrix de I/O: as 9 linhas da matriz têm cobertura de teste que executou e passou (checado nominalmente, não só por inferência).

**Riscos residuais:**
- Busca não é insensível a acento (decisão consciente, ver Review Triage Log) -- pode gerar alguma frustração real de usuário buscando "eletrico" sem achar "Elétrico"; resolver direito exigiria a extensão `unaccent` (migração), fora do `Never` desta spec.
- Nenhuma navegação ao selecionar um resultado -- intencional (Story 4.4 ainda não existe), mas o valor prático da busca fica limitado até a Story 4.4 landar.
- Flakiness observada anteriormente em `CadastroProdutoSection.test.tsx` (arquivo não tocado por esta story) sob carga da suíte completa não se repetiu nas execuções finais (266/266 passando) -- não investigada a fundo por ser não-relacionada ao escopo desta story.
