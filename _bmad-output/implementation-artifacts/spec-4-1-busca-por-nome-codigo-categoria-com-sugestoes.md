---
title: 'Story 4.1 — Busca por nome/código/categoria com sugestões'
type: 'feature'
created: '2026-08-31'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '62b98b201cba707582c192ae8583e4bc11021046'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      docker-compose.yml deixa JWT_SECRET cair silenciosamente no segredo de
      desenvolvimento quando a variável não está definida no ambiente, em vez
      de falhar rápido.
    evidence: |-
      Confirmado lendo docker-compose.yml:36: `JWT_SECRET:
      ${JWT_SECRET:-dev-jwt-secret-nao-usar-em-producao}`. Se um operador
      esquecer de configurar JWT_SECRET no Coolify, o container sobe em
      produção assinando/verificando JWTs com um segredo conhecido
      publicamente no repositório, sem nunca falhar — contradiz o padrão
      fail-fast que o próprio main.go documenta para DATABASE_URL/JWT_SECRET.
      Achado independentemente pelo Blind Hunter e pelo Edge Case Hunter na
      revisão de follow-up da Story 4.1; não é causado por esta story
      (introduzido em commits de docker-compose anteriores à implementação
      desta story).
    location: >-
      docker-compose.yml:36
    severity: medium
  - summary: >-
      docker-compose.yml usa valores hardcoded (porta 8080, diretório
      /data/fotos) no healthcheck do `api` e no volume de fotos, em vez de
      referenciar as próprias variáveis PORT/FOTOS_DIR que esses mesmos
      serviços expõem.
    evidence: |-
      Confirmado lendo docker-compose.yml: linhas 32/40 definem
      `${PORT:-8080}`/`${FOTOS_DIR:-/data/fotos}`, mas a linha 78 (healthcheck
      do `api`) usa `http://127.0.0.1:8080/...` literal e a linha 69 (volume)
      usa `/data/fotos` literal. Se um operador sobrescrever PORT ou
      FOTOS_DIR, o healthcheck passa a testar a porta errada (api nunca fica
      "healthy") e/ou fotos gravadas fora do volume persistente
      `stockflow-fotos-data` (perdidas num restart). Achado pelo Edge Case
      Hunter na revisão de follow-up da Story 4.1; não é causado por esta
      story.
    location: >-
      docker-compose.yml
    severity: low
  - summary: >-
      docker-compose.yml removeu a publicação de porta no host para
      db/api/web (substituída por expose interno), restaurada só de forma
      opt-in via docker-compose.override.yml.example — quem roda `docker
      compose up` sem copiar esse arquivo perde acesso via localhost aos
      três serviços.
    evidence: |-
      Confirmado no diff: docker-compose.yml remove `ports: ['5432:5432']`
      (db), `ports: ['8080:8080']` (api) e `ports: ['8081:80']` (web),
      substituindo por `expose`; docker-compose.override.yml.example (novo)
      documenta que ele precisa ser copiado manualmente para restaurar esses
      mapeamentos. Qualquer fluxo baseado em host — por exemplo
      `backend/main_test.go` (comentário de testDB orienta exportar
      DATABASE_URL apontando para localhost:5432) ou o proxy do dev server
      do frontend para localhost:8080 — para de funcionar silenciosamente
      sem esse arquivo copiado. Achado independentemente pelo Edge Case
      Hunter e pelo Verification Gap Reviewer na revisão de follow-up 2 da
      Story 4.1; falha de forma clara (conexão recusada), não incorreta, e
      não é causado por esta story (introduzido nos commits de
      docker-compose anteriores à implementação desta story).
    location: >-
      docker-compose.yml
    severity: low
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

### 2026-08-31 — Review pass (follow-up)
- intent_gap: 0
- bad_spec: 0
- patch: 5 (high 0, medium 1, low 4)
- defer: 2 (medium 1, low 1)
- reject: 9 (low 9)
- addressed_findings:
  - `[medium]` `[patch]` `BuscaCatalogo.tsx` só rechecava `montadoRef` depois do `await res.json()`, nunca `termoAtualRef` — se o Usuário digitasse um termo novo exatamente durante o parse do JSON da resposta anterior (janela entre a checagem de `termoAtualRef` pré-`await` e o `await` resolver), a resposta obsoleta ainda sobrescrevia a lista/mensagem com um resultado que não correspondia mais ao termo atual, violando o `Always` da spec sobre descarte de resposta obsoleta. Achado pelo Edge Case Hunter. Corrigido acrescentando um segundo recheck de `termoAtualRef` logo após o `await res.json()`, antes de `setResultados`/`setTermoBuscado`.
  - `[low]` `[patch]` Nenhum teste travava o `type="search"` do campo de busca — fix aplicado na passada de revisão anterior desta própria story, sem cobertura de regressão. Achado pelo Blind Hunter. Corrigido com um teste dedicado (`toHaveAttribute('type', 'search')`).
  - `[low]` `[patch]` Nenhum teste cobria a transição erro -> sucesso: a mensagem `role="alert"` deveria desaparecer assim que uma busca seguinte resolve com sucesso, mas nenhum caso do `describe` de erro exercitava esse caminho. Achado pelo Blind Hunter. Corrigido com um novo teste (resposta não-ok seguida de uma busca com resultado).
  - `[low]` `[patch]` Nenhum teste cobria desmontar o componente ANTES do debounce (300ms) disparar — só "desmontar com fetch já em voo" estava coberto; o cleanup que cancela o `setTimeout` pendente (`clearTimeout(debounceRef.current)`) ficava sem nenhuma asserção de que `fetch` nunca é chamado nesse caso. Achado pelo Blind Hunter. Corrigido com um novo teste.
  - `[low]` `[patch]` O desempate final `p.id ASC` em `buscarProdutosQuery` (adicionado na passada de revisão anterior desta própria story, para dois Produtos com mesmo rank e mesmo `nome`) não tinha nenhum teste de regressão cobrindo exatamente esse caso — nenhum teste da suíte cria dois Produtos com rank E nome idênticos, então `p.nome ASC` sozinho já ordena toda a suíte hoje sem nunca exercitar o `p.id`. Achado pelo Verification Gap Reviewer. Corrigido com `TestBuscarProdutos_EmpateDeRankENomeDesempataPorID` (dois Produtos com mesmo `nome`/mesmo rank, resultado esperado em ordem ascendente por `id`).
  - `[low]` `[patch]` Comentário em `produtos_test.go` (`TestBuscarProdutos_CoringasLiteraisNaoViramWildcard`) misturava aspa reta com aspa curva (`ESCAPE '\”`), typo cosmético sem efeito funcional. Achado pelo Blind Hunter. Corrigido.
  - `[reject]` Requisições `fetch` em voo de um debounce anterior não são canceladas via `AbortController` quando um termo mais novo dispara ou o componente desmonta — a corrida já é neutralizada do lado do cliente (`termoAtualRef`/`montadoRef` descartam a resposta), então a única consequência é trabalho de rede desperdiçado, não um resultado incorreto; nenhuma AC/NFR exige cancelamento de requisição, mesmo padrão de rejeição de escopo já usado nesta story para itens sem AC correspondente.
  - `[reject]` `services.BuscarProdutos` não valida o tamanho de `termo` por conta própria (só o handler valida) — hoje o único chamador é `BuscarProdutosHandler`, que já valida; um guard duplicado no service defenderia contra um chamador hipotético que não existe no repositório.
  - `[reject]` `buscarProdutosQuery` avalia os padrões `ILIKE` de prefixo/substring duas vezes por linha (uma no `CASE` de rank, outra no `WHERE`) — redundante, mas a checagem manual de NFR já feita nesta story (8.000 linhas, ~13ms) confirma que isso não compromete o orçamento de 300ms p95; otimizar seria trabalho sem benefício mensurável no volume real.
  - `[reject]` `CategoriaBusca.codigo` é lido do envelope de resposta pelo frontend mas nunca renderizado na lista de resultados — premissa de "dado morto" incorreta: a spec define explicitamente `"categoria":{"id","codigo","nome"}` como o formato de resposta (mesmo padrão de `ListarCategorias`); o frontend só usa `nome` porque nenhuma AC pede o código da categoria na UI, não porque o campo é supérfluo no contrato.
  - `[reject]` Nenhum teste cobre `authHeaders()` retornando `{}` quando não há token de acesso — o caminho "sem token" já resulta em 401 do backend, e o branch de erro genérico (`res.ok === false`) já tem cobertura própria; um teste adicional só para a origem específica do 401 seria cobertura desproporcional ao risco, mesmo padrão de rejeição já usado nesta story.
  - `[reject]` Comentário do healthcheck do `api` em `docker-compose.yml` reconhece que o serviço "passa por acidente hoje" (Go escuta dual-stack) sem abrir um item de acompanhamento formal para essa coincidência — o comentário já documenta o risco no próprio código; não é uma lacuna introduzida por esta story nem algo que esta story deveria resolver (não toca `docker-compose.yml` por intent).
  - `[reject]` `epic-4-context.md`/a própria spec desta story não têm nenhuma checagem automatizada cruzando as decisões documentadas (ex. infraestrutura de SSE do épico) com o código realmente alterado por esta story — nenhuma AC/NFR pede esse tipo de checagem de consistência documentação-código, e o próprio arquivo já se declara regenerável ("Regenerate with compile-epic-context if planning docs change").
  - `[reject]` Atalho `/` não trata `evento.isComposing` (IME) — premissa questionável: o atalho só age quando `document.activeElement` NÃO é um campo editável (`elementoEhEditavel`), e composição de IME só fica ativa num elemento editável focado; o estado descrito (IME compondo fora de um campo editável) não é alcançável pela própria guarda já existente.

### 2026-08-31 — Review pass (follow-up 2)
- intent_gap: 0
- bad_spec: 0
- patch: 1 (high 0, medium 0, low 1)
- defer: 1 (medium 0, low 1)
- reject: 17 (low 17)
- addressed_findings:
  - `[low]` `[patch]` Nenhum teste do lado do cliente travava o caminho "digitar só espaços -> termo trimado fica vazio -> nenhuma requisição disparada" em `BuscaCatalogo.tsx` — só o estado inicial (sem nenhuma digitação) tinha teste para "termo vazio"; o branch `termoTrimado === ''` dentro de `aoDigitar`, alcançado só depois de digitar espaços, ficava sem cobertura de regressão. Achado pelo Blind Hunter. Corrigido com um novo teste (`digitar só espaços não dispara requisição`) em `BuscaCatalogo.test.tsx`.
  - `[reject]` Sem correção de acento (busca não é insensível a acento) — já rejeitado na primeira passada de revisão desta story, mesma justificativa (decisão deliberada de escopo, sem `unaccent`/migração).
  - `[reject]` Nenhum estado de carregamento entre o debounce disparar e a resposta chegar — já rejeitado na primeira passada, mesma justificativa (nenhuma AC/NFR exige, latência real ~13ms).
  - `[reject]` Requisições em voo não são canceladas via `AbortController` — já rejeitado na passada de revisão anterior, mesma justificativa (corrida já neutralizada do lado do cliente, sem resultado incorreto).
  - `[reject]` Ausência de rate limiting no endpoint — já rejeitado na primeira passada, mesma justificativa (nenhum outro endpoint do backend tem rate limiting).
  - `[reject]` Nenhum teste automatizado mede a NFR de 300ms p95 sob 8.000 produtos — a própria spec documenta essa checagem como manual (`EXPLAIN ANALYZE`, seção Verification), decisão deliberada registrada nas Design Notes; não é uma lacuna desta revisão.
  - `[reject]` `JOIN categorias` (sem `LEFT JOIN`) esconderia Produtos com `categoria_id` nulo/órfão — premissa falsa: `categoria_id` é `NOT NULL` na migração (`backend/migrations/000011_create_produtos.up.sql`), confirmado pelo Verification Gap Reviewer; o estado descrito não é alcançável.
  - `[reject]` Toda resposta não-OK (incluindo um eventual 401) cai na mesma mensagem genérica de erro — já rejeitado na passada de revisão anterior, mesmo padrão de qualquer outro `fetch` local no app hoje.
  - `[reject]` Nenhum teste isola rank 1 via prefixo de `codigo` (só `nome` é testado isoladamente em `TestBuscarProdutos_MatchPorPrefixo`) — cobertura desproporcional ao risco: `TestBuscarProdutos_MatchExatoVemPrimeiro` já exercita indiretamente um match de prefixo por `codigo` (`"PAR-0010"` contra o termo `"PAR-001"`) dentro do mesmo teste; a lógica SQL é simétrica entre `nome`/`codigo` no mesmo `OR`.
  - `[reject]` Nenhum teste isola o ramo de igualdade exata de `categorias.nome` no rank 2 (só o ramo de prefixo é exercitado por `TestBuscarProdutos_MatchSoPorCategoria`) — cobertura desproporcional ao risco: um termo igual ao nome completo da categoria também bate no padrão de prefixo (`ILIKE 'termo%'`), então os dois ramos produzem o mesmo rank observável: não há divergência de comportamento a proteger com um teste dedicado.
  - `[reject]` Ausência de `aria-live` anunciando resultados/erro da busca — já rejeitado na primeira passada, mesma justificativa (nenhuma AC/NFR exige, único `aria-live` do épico é o toast SSE da Story 4.4).
  - `[reject]` Assimetria em `docker-compose.yml`: `api`/`web` ganharam bloco `expose`, `db` só teve `ports` removido sem `expose` equivalente — cosmético, sem efeito funcional (rede interna do Docker não exige `expose` explícito para comunicação entre serviços); `docker-compose.yml` não é tocado por esta story (Code Map) e a alteração vem de commits anteriores de infraestrutura, não desta story.
  - `[reject]` Nenhuma documentação OpenAPI/API externa para `GET /api/produtos/busca` fora dos doc-comments em Go — nenhum outro endpoint do repositório tem documentação OpenAPI/externa; não é uma convenção existente que esta story deveria introduzir isoladamente.
  - `[reject]` Nenhum teste combina o limite de 255 runes com espaços nas pontas que trimam para exatamente 255 — cobertura desproporcional ao risco: o trim acontece antes da contagem de runes tanto no handler quanto nos testes já existentes (`TestBuscarProdutosHandler_400TermoMuitoLongo` já usa um termo sem espaços de 256 runes); a interação específica não muda o comportamento do código, só duplicaria asserção já coberta.
  - `[reject]` `produto.categoria.nome` acessado em `BuscaCatalogo.tsx` sem validar a forma de cada item da resposta (só `Array.isArray(data.produtos)` é checado) — mesmo padrão de confiança já usado por todo componente irmão que consome a própria API backend do projeto (nenhum componente do repositório faz validação de schema por item de uma resposta própria); não é uma convenção existente nem uma regressão introduzida por esta story.
  - `[reject]` Leitura "R1" do Intent Alignment Auditor: o teto de 255 runes em `q` contradiria "nenhuma AC fixa um mínimo" — leitura equivocada: a frase da spec fala explicitamente de tamanho MÍNIMO ("Nenhum tamanho mínimo de caracteres é exigido"), nunca de máximo; um teto máximo não contradiz essa frase, e já foi decidido corretamente como patch na primeira passada de revisão.
  - `[reject]` Leitura do Intent Alignment Auditor sobre o desempate `p.id ASC`: o texto da spec ("ORDER BY rank ASC, nome ASC") não proibiria um desempate adicional determinístico — já justificado e decidido corretamente como patch na primeira passada (necessário para estabilidade do `LIMIT 7` em empates), não contradiz o texto, só o complementa.
  - `[reject]` Leitura do Intent Alignment Auditor sobre a UX de erro de busca ser "superfície de produto não autorizada": tratar falha de rede/resposta não-OK é comportamento mínimo necessário para qualquer `fetch` funcionar (mesmo padrão de outros componentes do app); já endereçado e mantido como escopo correto na passada de revisão anterior desta própria story.

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

**Resumo da mudança implementada:** Story 4.1 já estava implementada e revisada (2 passadas anteriores). Esta execução é a passada de follow-up automática disparada por reabrir uma spec `done` (rotina do bmad-build-auto): reconstruiu o diff desde `baseline_revision`, rodou as 4 camadas de revisão (Blind Hunter, Edge Case Hunter, Verification Gap Reviewer, Intent Alignment Auditor) e aplicou o único patch real encontrado.

**Arquivos alterados nesta passada:**
- `frontend/src/components/catalogo/BuscaCatalogo.test.tsx` -- novo teste `digitar só espaços não dispara requisição (termo trimado fica vazio)`, cobrindo o branch `termoTrimado === ''` de `aoDigitar` alcançado só depois de digitar espaços (antes só o estado inicial sem digitação tinha teste).

**Review findings breakdown:**
- patch: 1 (low 1) -- aplicado (teste de espaços em branco acima).
- defer: 1 (low 1) -- `docker-compose.yml` removeu publicação de porta no host para `db`/`api`/`web` (restaurada só via `docker-compose.override.yml.example`, opt-in); fluxos baseados em host (`go test` local, proxy do dev server do frontend) param de conectar sem esse arquivo copiado. Pré-existente (commits de infraestrutura anteriores a esta story), não causado pela busca.
- reject: 17 (low 17) -- 11 duplicatas de achados já rejeitados nas duas passadas anteriores desta story (mesma justificativa, sem mudança); 4 novos achados de cobertura desproporcional ao risco ou premissa falsa (`categoria_id` confirmado `NOT NULL`, rank por prefixo/código já exercitado indiretamente, ramo de igualdade exata de categoria indistinguível do ramo de prefixo em resultado observável, sem convenção de doc OpenAPI no repo); 3 leituras alternativas de intenção levantadas pelo Intent Alignment Auditor (teto de 255 runes, desempate `p.id ASC`, mensagem de erro de busca) -- todas já decididas corretamente como patches necessários/consistentes nas passadas anteriores, sem contradição real com o texto da spec.
- intent_gap: 0, bad_spec: 0.

**Verificação executada:**
- `cd frontend && npm run test -- --run BuscaCatalogo` -- 15/15 passou (inclui o novo teste).
- `cd frontend && npm run lint && npm run build && npm run test` -- `oxlint` limpo, `tsc`+`vite build` ok, suíte completa 271/271 passou.
- `cd backend && gofmt -l . && go build ./... && go vet ./...` -- sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` -- todos os pacotes `ok`; testes dependentes de Postgres (`TestBuscarProdutos_*` etc.) reportaram `SKIP` porque este ambiente de execução não tem Docker disponível para subir o banco -- nenhum código de backend foi alterado nesta passada, então isso não representa risco novo, só uma limitação deste ambiente de sandbox (não reproduz um problema do código).

**Riscos residuais:** nenhum novo introduzido por esta passada. Os 3 itens em `deferred` (frontmatter) permanecem em aberto, todos em `docker-compose.yml`, todos pré-existentes a esta story.

**Status final: `blocked` -- decisão humana necessária.** O patch (teste de espaços em branco) e o próprio `spec_file` já estão commitados (`61087ee`), então a revisão do código desta story está completa e não precisa ser refeita. O bloqueio é só de finalização de árvore de trabalho, não de conteúdo: ao terminar esta passada, `_bmad-output/implementation-artifacts/deferred-work.md` e `_bmad-output/implementation-artifacts/sprint-status.yaml` seguem modificados e não commitados na árvore de trabalho -- ambos são de propriedade do orquestrador (instrução explícita desta invocação: nunca escrever nem reverter esses dois arquivos), então este workflow não pode commitá-los nem descartá-los para limpar a árvore. A regra de Finalize deste workflow exige árvore limpa ao final ("Verify the version-controlled working copy is clean. Otherwise HALT... 'finalization left repository dirty'"), e como a única forma de limpá-la envolveria tocar arquivos fora da minha alçada, a decisão sobre o que fazer com essas duas modificações pendentes (commitar via outro processo, descartar, ou é trabalho em andamento de outra sessão concorrente -- HEAD avançou de `35be742` para `4a0b9b8` durante esta revisão com um commit não relacionado de outra sessão, "fix(mfa): send senhaAtual in confirmar-configuracao request") cabe a um humano ou ao próprio orquestrador, não a esta execução.

