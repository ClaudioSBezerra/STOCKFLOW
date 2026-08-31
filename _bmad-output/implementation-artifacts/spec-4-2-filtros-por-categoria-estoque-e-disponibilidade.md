---
title: 'Story 4.2 — Filtros por categoria, estoque e disponibilidade'
type: 'feature'
created: '2026-08-31'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: 'ac06e7b448524c3d168d7e3b60c94212e3977518'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** `GET /api/produtos/catalogo` (Story 4.3) sempre lista TODOS os Produtos — qualquer Usuário autenticado (`usuario`+) não tem como restringir a grade/tabela por categoria, por Estoque ou só aos Produtos com quantidade disponível, nem combinar isso com o termo já digitado em `BuscaCatalogo` (Story 4.1) — apesar de a jornada central do produto (FR-6) depender de "encontrar material sobrando" rápido num catálogo de até 8.000 itens.

**Approach:** `GET /api/produtos/catalogo` ganha 4 query params opcionais — `q`, `categoriaId`, `estoqueId`, `comEstoque` — combinados sempre por E lógico entre si e com `pagina`/`agrupar` já existentes, filtrando as linhas de `produtos` ANTES de agrupar (o `agrupar=true` já filtra os Produtos que entram em cada grupo, não filtra grupos prontos). No frontend, `CatalogoListagem` ganha 3 controles (`Select` de categoria, `Select` de Estoque, checkbox "Com estoque") que disparam refetch; `BuscaCatalogo` passa a reportar o termo digitado (bruto, a cada tecla) para `CatalogoPage`, que o debounca (300ms) e repassa como `termo` para `CatalogoListagem` — sem alterar o comportamento próprio de `BuscaCatalogo` (sugestões inline de até 7 itens continuam exatamente como na Story 4.1).

## Boundaries & Constraints

**Always:**
- `GET /api/produtos/catalogo` aceita `q`, `categoriaId`, `estoqueId`, `comEstoque`, todos opcionais e combináveis por E lógico entre si e com `agrupar`/`pagina` — nenhum filtro substitui outro, todos juntos restringem ainda mais o resultado (epic-4-context.md).
- `q`: `strings.TrimSpace`; ausente ou vazio ⇒ sem filtro de texto (nenhuma AC exige mínimo). Presente e >255 runes (trimado) ⇒ `400 VALIDATION_ERROR` "termo de busca muito longo" (mesmo teto/mensagem de `BuscarProdutosHandler`, Story 4.1), sem consulta ao banco. Presente e ≤255 runes ⇒ substring case-insensitive (`ILIKE '%termo%' ESCAPE '\'`, coringas `%`/`_`/`\` escapados via `escaparCoringasLike` já existente) em `nome` OU `codigo` OU `categorias.nome` — mesmos 3 campos da Story 4.1, sem ranking (aqui é filtro de listagem paginada, não sugestão).
- `categoriaId`: ausente/vazio ⇒ sem filtro. Presente ⇒ `p.categoria_id = categoriaId` exato. UUID malformado (não passa pela conversão implícita do Postgres) colapsa em ZERO linhas (mesmo padrão de colapso de `pqInvalidTextRepresentation` usado em `ObterProdutoDetalhe`/`ExcluirEstoque`/etc.) — nunca um erro HTTP, já que o valor só chega malformado por chamada direta à API (o `<Select>` do frontend só oferece ids reais de `GET /api/categorias`).
- `estoqueId`: ausente/vazio ⇒ sem filtro. Presente ⇒ o Produto precisa ter ALGUMA linha em `produto_estoque` para esse `estoque_id` (`EXISTS`, qualquer quantidade — inclusive `0`: todo cadastro de Produto grava uma linha em `produto_estoque` para o Estoque informado, mesmo com quantidade inicial `0` — `services.CriarProduto`/`produtos.go:312`). UUID malformado colapsa em ZERO linhas, mesmo padrão de `categoriaId`.
- `comEstoque`: só `true`, `false` ou ausente (⇒ sem filtro); qualquer outro valor ⇒ `400 VALIDATION_ERROR` "parâmetro comEstoque inválido" (mesmo padrão/mensagem shape de `agrupar`), sem consulta. `true` ⇒ só Produtos com `quantidadeTotal > 0` somando TODOS os Estoques do Produto (a mesma definição de `disponivel`, sempre global — nunca escopada ao `estoqueId` filtrado na mesma chamada, mesmo quando os dois filtros estão ativos ao mesmo tempo: são independentes, cada um filtra por seu próprio critério, e a composição por E lógico é intencional mesmo quando produz um resultado "Produto está neste Estoque, mas com 0 aqui e disponível só em outro"). `false` ⇒ só Produtos com `quantidadeTotal == 0` (suportado pelo backend para simetria/teste; a UI só expõe a versão "on" do checkbox — desmarcado envia `comEstoque` ausente, não `false`).
- `agrupar=true`: os filtros acima restringem QUAIS Produtos entram na formação de cada grupo (`WHERE` antes do `GROUP BY`) — um grupo cujos Produtos foram todos excluídos pelo filtro simplesmente não aparece; um grupo que sobrevive mostra `quantidadeTotal`/`porEstoque` de TODOS os Estoques dos Produtos restantes no grupo (o filtro decide "entra ou não", nunca recorta o que é mostrado sobre quem entrou). A busca por `categorias.nome` (`q`) e a contagem de grupos (`catalogoGrupoCountQuery`) precisam do mesmo `JOIN categorias c ON c.id = p.categoria_id` que a grade já tem — a query de grupo hoje não tem esse `JOIN`.
- Frontend `CatalogoListagem`: busca `GET /api/categorias`/`GET /api/estoques` uma vez no mount (mesmo padrão de `CadastroProdutoSection.carregarListas`) para popular os dois `<Select>`; falha de qualquer um dos dois não bloqueia a listagem em si (os filtros ficam indisponíveis/mostram só a opção "Todas"/"Todos", mesma degradação silenciosa do template opcional em `CadastroProdutoSection`). `<Select>` de categoria (opção sentinela "Todas as categorias") e de Estoque (opção sentinela "Todos os Estoques") — Radix `Select.Item` proíbe `value=""`, mesmo padrão `SEM_TEMPLATE` já usado. Checkbox "Com estoque disponível" (shadcn `Checkbox`, novo import — nenhum componente do Catálogo usa `Checkbox` hoje).
- Qualquer mudança de filtro (categoria, Estoque, "Com estoque") ou do `termo` recebido via prop volta `pagina` para `1` (mesmo padrão de `alternarModo`) e redispara o `fetch`.
- `CatalogoPage` recebe de `BuscaCatalogo` o valor bruto digitado a cada tecla (`onTermoChange`, novo prop opcional chamado de dentro do handler `aoDigitar` já existente, sem alterar nenhum outro comportamento de `BuscaCatalogo`), debounca 300ms (mesmo valor da Story 4.1) e passa o termo trimado como prop `termo` para `CatalogoListagem`. `BuscaCatalogo` continua disparando sua PRÓPRIA busca de sugestões (`/api/produtos/busca`, até 7 itens) de forma independente — as duas requisições (sugestão + listagem filtrada) coexistem, cada uma no seu próprio debounce.
- Alvo de toque mínimo 48px nos novos controles de filtro (NFR de usabilidade em campo, mesmo padrão dos controles existentes de `CatalogoListagem`).

**Block If:** nenhuma condição desta story exige decisão humana em runtime — segue direto.

**Never:**
- Nenhuma migração nova / índice novo — o índice `idx_produto_estoque_estoque_id` (migração 000012) já cobre o `EXISTS` do filtro de Estoque; 8.000 Produtos permanece volume trivial para Postgres (mesma justificativa de Design Notes das Stories 4.1/4.3).
- Nenhum filtro multi-seleção (várias categorias/Estoques ao mesmo tempo) — cada `<Select>` escolhe no máximo um valor por vez, mesmo padrão de `CadastroProdutoSection`. Nenhuma AC pede multi-seleção.
- Nenhuma alteração no contrato de `GET /api/produtos/busca` (Story 4.1) — mesma resposta, mesmo limite de 7, mesmo ranking. Só o COMPONENTE `BuscaCatalogo.tsx` ganha o prop `onTermoChange`; nenhuma mudança na sua própria lógica de sugestão/debounce/descarte de resposta obsoleta.
- Nenhuma navegação nova a partir de um card/linha do catálogo (já resolvida pela Story 4.4) — esta story só filtra o que já é mostrado.
- Nenhum SSE/`EventSource` (Story 4.4), nenhuma exportação Excel (Story 4.6 — que vai reusar estes MESMOS parâmetros de filtro, mas isso é escopo da 4.6, não desta), nenhum `fab-scanner`/QR Code (Story 4.5).
- `comEstoque` nunca é escopado ao `estoqueId` filtrado na mesma chamada (ver `Always`) — não inventar uma variante "disponível NESSE Estoque"; nenhuma AC pede essa composição.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Filtro por categoria isolado | `?categoriaId=<id de "Material Elétrico">` | Só Produtos dessa categoria | `200`, no error |
| Filtro por Estoque isolado | `?estoqueId=<id>`, Produto com linha `produto_estoque` nesse Estoque com quantidade `0` | Produto aparece (linha existe, quantidade não importa) | `200`, no error |
| Filtro "Com estoque" isolado | `?comEstoque=true` | Só Produtos com soma > 0 em qualquer Estoque | `200`, no error |
| Todos os filtros + `q` combinados | `?q=paraf&categoriaId=X&estoqueId=Y&comEstoque=true` | Interseção (E lógico) das 4 condições | `200`, no error |
| Estoque + "Com estoque" sem sobreposição | Produto tem linha `quantidade=0` no Estoque `Y` filtrado, mas `quantidade=5` em outro Estoque | Produto APARECE (`estoqueId=Y` casa a linha zerada; `comEstoque=true` casa a soma global > 0) | `200`, no error |
| `agrupar=true` com filtro que esvazia um grupo | Grupo com 2 Produtos, só 1 casa `categoriaId` filtrado | Grupo aparece só com a soma/porEstoque do Produto que casou | `200`, no error |
| `agrupar=true` com filtro que remove o grupo inteiro | Nenhum Produto do grupo casa o filtro | Grupo não aparece; `paginacao.total` não conta esse grupo | `200`, no error |
| `categoriaId`/`estoqueId` malformado (não-UUID) | `?categoriaId=abc` | `{"produtos":[]}` (ou `{"grupos":[]}`), `paginacao.total:0` | `200`, no error (nunca 500) |
| `comEstoque` inválido | `?comEstoque=talvez` | — | `400 VALIDATION_ERROR` "parâmetro comEstoque inválido" |
| `q` maior que 255 runes | termo trimado com 256 runes | — | `400 VALIDATION_ERROR` "termo de busca muito longo" |
| Nenhum filtro presente | sem query params além de `pagina`/`agrupar` | Comportamento idêntico à Story 4.3 (sem regressão) | `200`, no error |
| Usuário digita na busca e a listagem reflete | digitar "parafuso" em `BuscaCatalogo` | Após ~300ms, `CatalogoListagem` reflete `?q=parafuso` combinado com os filtros ativos, ao MESMO TEMPO que `BuscaCatalogo` mostra suas próprias até-7 sugestões | client-side |

</intent-contract>

## Code Map

- `backend/services/catalogo.go:127-269` (`catalogoGradeQuery`, `catalogoGrupoCountQuery`, `catalogoGrupoQuery`, `ListarCatalogoGrade`, `ListarCatalogoAgrupado`) -- ponto central da mudança: as 3 queries precisam de um `WHERE` dinâmico construído a partir dos filtros (placeholders numerados depois dos existentes de `LIMIT`/`OFFSET`, ou renumerados — decisão de implementação); `catalogoGrupoQuery`/`catalogoGrupoCountQuery` ganham `JOIN categorias c ON c.id = p.categoria_id` (não têm hoje) para suportar `q` sobre `categorias.nome`. Novo tipo `FiltrosCatalogo{Q, CategoriaID, EstoqueID string; ComEstoque *bool}` no mesmo arquivo; `ListarCatalogoGrade`/`ListarCatalogoAgrupado` ganham parâmetro `filtros FiltrosCatalogo`.
- `backend/services/produtos.go:413-416` (`escaparCoringasLike`) -- reusar tal como está para o padrão `%termo%` de `q` (mesma função da Story 4.1, já testada).
- `backend/services/produtos.go:430-442` (`buscarProdutosQuery`) -- referência de como compor `ILIKE ... ESCAPE '\'' ` sobre `nome`/`codigo`/`categorias.nome` (sem a parte de `rank`, que não se aplica aqui).
- `backend/migrations/000012_create_produto_estoque.up.sql` (somente leitura) -- confirma `idx_produto_estoque_estoque_id` (cobre o `EXISTS` do filtro de Estoque) e que `quantidade` pode ser `0` com a linha ainda existindo (o "Always" sobre `estoqueId` depende disso).
- `backend/services/catalogo_test.go` -- novos `TestListarCatalogoGrade_Filtro*`/`TestListarCatalogoAgrupado_Filtro*` cobrindo a matriz acima (isolados e combinados, grade e agrupado).
- `backend/handlers/produtos.go:266-314` (`ListarCatalogoHandler`) -- ganha leitura/validação de `q` (mesmo teto de 255 runes de `BuscarProdutosHandler:228-236`), `categoriaId`, `estoqueId` (repassados como estão, sem validação de formato — o colapso acontece no service/banco), `comEstoque` (`switch` igual ao de `agrupar`, linhas 284-293); monta `services.FiltrosCatalogo` e passa para `ListarCatalogoGrade`/`ListarCatalogoAgrupado`.
- `backend/handlers/produtos_test.go` -- novos `TestListarCatalogoHandler_Filtro*` (200 com cada filtro isolado, 200 combinando todos, 400 `comEstoque` inválido, 400 `q` muito longo, 200 vazio para id malformado).
- `backend/main.go:421-430` (doc comment do bloco de rota `GET /api/produtos/catalogo`) -- atualiza para citar a Story 4.2 e os novos query params; rota em si não muda (mesmo `HandleFunc`, só o handler por trás ganha capacidade).
- `frontend/src/components/catalogo/BuscaCatalogo.tsx:69` (`export function BuscaCatalogo()`) -- ganha prop opcional `{ onTermoChange?: (valor: string) => void }`; dentro de `aoDigitar` (linha ~106-108), logo após `setTermo(valor)`, chama `onTermoChange?.(valor)` com o valor BRUTO (não trimado — quem debounça/trima é `CatalogoPage`). Nenhuma outra linha de `BuscaCatalogo.tsx` muda.
- `frontend/src/components/catalogo/BuscaCatalogo.test.tsx` -- novo teste cobrindo `onTermoChange` chamado a cada digitação com o valor bruto.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:265-300` (`carregarListas`, padrão `Promise.all` para `/api/categorias`+`/api/estoques`, degradação silenciosa) -- molde para o `useEffect` de carregamento dos dois `<Select>` de filtro em `CatalogoListagem`; `frontend/src/components/produtos/CadastroProdutoSection.tsx:103-104,501-516` (`SEM_TEMPLATE`, uso de `Select`/`SelectItem` com sentinela) -- molde para as opções "Todas as categorias"/"Todos os Estoques".
- `frontend/src/components/catalogo/CatalogoListagem.tsx` -- ganha prop `termo: string`; estado novo (`categoriaId`, `estoqueId`, `comEstoque`, `categorias`, `estoques`); a linha do `fetch` (139-148) monta a query string com `q`/`categoriaId`/`estoqueId`/`comEstoque` quando presentes; `carregar` (assinatura atual `(modoAtual, paginaAtual)`) ganha os filtros como parâmetro adicional para não depender de closure stale (mesmo cuidado já usado no componente); 3 novos controles (`Select` categoria, `Select` Estoque, `Checkbox` "Com estoque disponível") acima da grade/tabela, cada `onChange`/`onCheckedChange` chamando `setPagina(1)` (mesmo padrão de `alternarModo:190-196`); `useEffect` reagindo a `termo` (prop) chamando `setPagina(1)`.
- `frontend/src/components/catalogo/CatalogoListagem.test.tsx` -- novos testes: filtros isolados e combinados disparam o `fetch` certo; troca de filtro volta para página 1; falha ao carregar categorias/Estoques não impede a listagem; `termo` (prop) mudando dispara refetch com `q`.
- `frontend/src/pages/CatalogoPage.tsx:29-34` (`export function CatalogoPage()`) -- novo estado `termoFiltro` + `useRef` de debounce (300ms, cleanup no unmount, mesmo padrão de `BuscaCatalogo`); `<BuscaCatalogo onTermoChange={...} />` e `<CatalogoListagem termo={termoFiltro} />`.
- `frontend/src/pages/CatalogoPage.test.tsx` -- mocks de `fetch` ganham `/api/categorias`/`/api/estoques` (chamadas agora também por `CatalogoListagem`, não só por `CadastroProdutoSection` quando `podeCadastrar`); novo teste cobrindo termo digitado em `BuscaCatalogo` refletindo em `?q=` na chamada de `/api/produtos/catalogo` após o debounce.

## Tasks & Acceptance

**Execution:**
- `backend/services/catalogo.go` (+ `catalogo_test.go`) -- `FiltrosCatalogo`, `WHERE` dinâmico nas 3 queries (grade, contagem de grupos, grupos), `JOIN categorias` na query de grupo, filtragem antes do `GROUP BY`.
- `backend/handlers/produtos.go` (+ `produtos_test.go`) -- parse/validação de `q`/`categoriaId`/`estoqueId`/`comEstoque` em `ListarCatalogoHandler`, monta `FiltrosCatalogo`.
- `backend/main.go` -- atualiza doc comment do bloco de rota do Catálogo citando a Story 4.2.
- `frontend/src/components/catalogo/BuscaCatalogo.tsx` (+ teste) -- prop `onTermoChange`.
- `frontend/src/components/catalogo/CatalogoListagem.tsx` (+ teste) -- prop `termo`, 3 controles de filtro, `fetch` com os novos params, reset de página.
- `frontend/src/pages/CatalogoPage.tsx` (+ teste) -- estado/debounce do termo, conecta `BuscaCatalogo`↔`CatalogoListagem`.

**Acceptance Criteria:**
- Given o Catálogo, when o Usuário seleciona uma categoria, um Estoque e marca "Com estoque disponível" ao mesmo tempo, then a grade/tabela mostra só os Produtos que satisfazem AS TRÊS condições simultaneamente (E lógico, nunca substituição).
- Given um filtro de categoria/Estoque/disponibilidade ativo, when o Usuário digita um termo no campo de busca do topo, then a listagem (grade/tabela) passa a refletir também esse termo combinado com os filtros já ativos, enquanto a lista de até-7 sugestões de `BuscaCatalogo` continua funcionando normalmente e sem depender dos filtros.
- Given a visualização em tabela agrupada (`agrupar=true`) com um filtro ativo, when um grupo tem Produtos que casam o filtro e Produtos que não casam, then o grupo aparece só com a soma/discriminação por Estoque dos Produtos que casaram.
- Given um Usuário com papel `usuario`, when ele chama `GET /api/produtos/catalogo` com qualquer combinação de filtros diretamente pela API, then a resposta é `200` (nunca `403` — a rota não leva `RequireRole`, mesmo padrão das Stories 4.1/4.3).
- Given qualquer mudança de filtro (categoria, Estoque, disponibilidade) ou do termo de busca, when a mudança acontece com o Usuário numa página >1, then a listagem volta para a página 1 automaticamente.

## Spec Change Log

## Review Triage Log

### 2026-08-31 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 4 (high 0, medium 1, low 3)
- defer: 0
- reject: 11 (low 11)
- addressed_findings:
  - `[medium]` `[patch]` `CatalogoListagem` carregava `/api/categorias`/`/api/estoques` com `Promise.all` num único `try/catch` — falha em só UM dos dois endpoints derrubava os DOIS `<Select>` para a opção sentinela, mesmo quando o outro endpoint respondeu com sucesso; contradiz a degradação silenciosa POR CAMPO que a própria spec pede ("mesma degradação silenciosa do template opcional"). Achado pelo Blind Hunter. Corrigido: as duas buscas passam a ter `try/catch` independentes (cada uma popula seu próprio `<Select>` ou degrada sozinha), com um novo teste cobrindo "categorias falha, estoques funciona" (e vice-versa).
  - `[low]` `[patch]` Nenhum teste de HTTP (`produtos_test.go`) combinava `agrupar=true` com algum filtro novo — a combinação só era provada na camada de serviço. Achado pelo Blind Hunter. Corrigido com `TestListarCatalogoHandler_FiltroComAgrupar` (agrupar=true + categoriaId, ponta a ponta pelo handler).
  - `[low]` `[patch]` Nenhum teste provava que a paginação reflete o conjunto FILTRADO (não o total sem filtro) numa página >1 com filtro ativo. Achado pelo Blind Hunter. Corrigido com `TestListarCatalogoGrade_PaginacaoSobreConjuntoFiltrado` (filtro ativo reduzindo o total, página 2 mostra o recorte certo do conjunto filtrado).
  - `[low]` `[patch]` Nenhum teste travava a classe de alvo de toque (`min-h-touch-target-min`) nos 3 novos controles de filtro (`Select` categoria, `Select` Estoque, `Checkbox` "Com estoque"), mesmo padrão de asserção já adicionado na Story 4.1. Achado pelo Blind Hunter. Corrigido com uma asserção por controle em `CatalogoListagem.test.tsx`.
  - `[reject]` `ListarCatalogoGrade` duplica o `FROM ... JOIN categorias` como literal na query de contagem em vez de derivar de `catalogoGradeQueryBase` — nitpick de manutenibilidade (drift futuro hipotético), sem risco funcional atual; mesmo padrão de rejeição de duplicação cosmética já usado nas Stories 4.1/4.3.
  - `[reject]` Nenhum teste combina `categoriaId` E `estoqueId` malformados ao mesmo tempo — cobertura desproporcional ao risco: o Verification Gap Reviewer confirmou independentemente que o colapso por SQLSTATE 22P02 é idêntico não importa qual/quantos parâmetros estão malformados (mesma mensagem, mesmo comportamento).
  - `[reject]` Estado de filtro (categoria/Estoque/disponibilidade/termo) só existe em estado React, nunca na URL — sem persistência a um refresh/link compartilhado, sem botão "limpar filtros". Nenhuma AC/NFR desta story pede isso; escopo além do que o epic define para esta story.
  - `[reject]` `filtroUUIDInvalido` colapsa QUALQUER SQLSTATE 22P02 nas 4 queries, não só o de `categoriaId`/`estoqueId` especificamente — preocupação especulativa sobre um filtro futuro hipotético; hoje só esses dois parâmetros podem produzir esse erro nessas queries, mesmo padrão já usado em `ObterProdutoDetalhe`/`ExcluirEstoque`/etc. em todo o resto do repositório.
  - `[reject]` Falha ao carregar `/api/categorias`/`/api/estoques` não mostra nenhum sinal visível (`role="alert"`) — a spec pede explicitamente degradação SILENCIOSA (mesmo padrão do template opcional de `CadastroProdutoSection`, que também não mostra banner de erro); implementa a spec como escrita, não é uma lacuna.
  - `[reject]` Verificação de NFR (300ms p95) só manual (`EXPLAIN ANALYZE`), sem teste automatizado/CI — decisão já documentada como deliberada na seção Verification desta spec, mesmo padrão já aceito nas Stories 4.1/4.3.
  - `[reject]` `produtos.categoria_id` sem índice dedicado para o novo filtro — preocupação especulativa sem evidência de problema real; a spec já proíbe explicitamente índice novo (`Never`), mesma justificativa (~8.000 linhas é volume trivial) das Stories 4.1/4.3.
  - `[reject]` `comEstoque=false`/quantidade negativa (Edge Case Hunter) — premissa falsa: `produto_estoque.quantidade` tem `CHECK (quantidade >= 0)` na migração 000012, então a soma nunca é negativa; o trecho de código citado pelo achado (`op = "<="`) também não corresponde ao código real (`op = "="`).
  - `[reject]` `JOIN categorias` (em vez de `LEFT JOIN`) esconderia Produtos com `categoria_id` nulo/órfão na query de grupo (Edge Case Hunter, confiança "baixa" já sinalizada pelo próprio achado) — premissa falsa: `categoria_id` é `NOT NULL` na migração, já confirmado no mesmo repositório na revisão da Story 4.1.
  - `[reject]` Segunda checagem de `filtroUUIDInvalido` (depois da query de linhas/grupos) é código morto na prática, já que a query de contagem sempre falha primeiro com o mesmo parâmetro malformado (Verification Gap Reviewer) — o próprio achado já classifica isto como "não é uma lacuna de verificação"; código defensivo redundante sem risco funcional, manter é mais seguro que remover.
  - `[reject]` Auditoria de Intent Alignment sobre `sprint-status.yaml`/`operator_actions`/`status` de frontmatter não tocados por este diff — confirma exatamente o comportamento correto: esta story não tem nenhuma ação que só um humano possa fazer (`Block If` da própria spec já resolve isso), então nenhuma bookkeeping de operador é devida; achado descritivo, não aponta um defeito.

## Design Notes

- **Filtro decide "entra no grupo", nunca recorta o que é mostrado sobre quem entrou**: o `WHERE` dos filtros atua sobre `produtos p` ANTES do `GROUP BY` — isso é suficiente para decidir quais Produtos formam cada grupo, mas depois de formado, `quantidadeTotal`/`porEstoque` de um grupo continuam somando/discriminando TODOS os Estoques dos Produtos que sobraram (nunca só o Estoque filtrado) — coerente com a Story 4.3, que já define `porEstoque` como "todos os Estoques com alguma linha", e evita uma segunda semântica de "recorte" que nenhuma AC pede.
- **`comEstoque` sempre global, nunca escopado a `estoqueId`**: a definição de "Com estoque" no `epic-4-context.md` já é `"quantidade > 0 em ao menos um Estoque"` sem menção a "no Estoque filtrado" — é literalmente o mesmo campo `disponivel` que a Story 4.3 já expõe por Produto/grupo. Escopar mudaria o significado de um campo já contratado por outra story; a composição "Produto está no Estoque X (quantidade 0 lá), mas disponível em outro lugar" é um resultado válido de dois filtros independentes por E lógico, não um bug.
- **`estoqueId` = "tem linha", não "tem quantidade"**: todo cadastro de Produto (Story 3.1) grava uma linha `produto_estoque` para o Estoque escolhido, mesmo com quantidade inicial `0` — se `estoqueId` exigisse `quantidade > 0`, ficaria indistinguível de `estoqueId + comEstoque=true` combinados, tornando um dos dois filtros redundante. Manter os dois independentes (presença vs. quantidade) preserva a composabilidade por E lógico que o epic pede.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` -- sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` -- cobre `TestListarCatalogo*Filtro*`, `TestListarCatalogoHandler_Filtro*`.
- `cd frontend && npm run lint && npm run build && npm run test` -- `oxlint`, `tsc`+`vite` e os testes de `CatalogoListagem`/`BuscaCatalogo`/`CatalogoPage` passam.

**Manual checks (if no CLI):**
- Com os ~8.000 Produtos já semeados para a NFR da Story 4.1/4.3 (se ainda disponíveis no ambiente), rodar `EXPLAIN ANALYZE` da query de grade/grupo com os 4 filtros ativos ao mesmo tempo -- tempo de execução bem abaixo de 300ms confirma que o `EXISTS`/`JOIN` extra não compromete a NFR já validada nas stories anteriores.
- `docker compose up --build`, logar como `usuario`, no Catálogo selecionar uma categoria + um Estoque + marcar "Com estoque disponível" + digitar um termo -- a grade/tabela reflete a interseção dos 4; desmarcar/limpar cada filtro isoladamente restaura o comportamento sem ele.

## Auto Run Result

**Resumo da mudança implementada:** `GET /api/produtos/catalogo` ganhou 4 query params opcionais combináveis por E lógico (`q`, `categoriaId`, `estoqueId`, `comEstoque`), aplicados ANTES do `GROUP BY` nas 3 queries do catálogo (grade, contagem de grupos, grupos — as duas últimas ganharam `JOIN categorias` para suportar `q` sobre `categorias.nome`). No frontend, `CatalogoListagem` ganhou 3 controles de filtro (`Select` categoria, `Select` Estoque, `Checkbox` "Com estoque disponível") e uma prop `termo`; `BuscaCatalogo` passou a reportar o termo digitado bruto via `onTermoChange`; `CatalogoPage` debounça (300ms) e conecta os dois. `estoqueId` casa presença de linha em `produto_estoque` (qualquer quantidade); `comEstoque` é sempre a soma global do Produto, nunca escopada a `estoqueId` na mesma chamada. UUID malformado em `categoriaId`/`estoqueId` colapsa em página vazia (nunca erro), mesmo padrão de colapso já usado no resto do repositório.

**Arquivos alterados:**
- `backend/services/catalogo.go` -- `FiltrosCatalogo`, `montarFiltrosCatalogo` (WHERE dinâmico + args), `filtroUUIDInvalido`, `JOIN categorias` nas queries de grupo, `ListarCatalogoGrade`/`ListarCatalogoAgrupado` ganham parâmetro `filtros`.
- `backend/services/catalogo_test.go` -- novos `TestListarCatalogoGrade_Filtro*`/`TestListarCatalogoAgrupado_Filtro*` cobrindo a matriz (isolados, combinados, malformados, grade e agrupado) + `TestListarCatalogoGrade_PaginacaoSobreConjuntoFiltrado` (patch da revisão).
- `backend/handlers/produtos.go` -- `ListarCatalogoHandler` parseia/valida `q`/`categoriaId`/`estoqueId`/`comEstoque`, monta `FiltrosCatalogo`.
- `backend/handlers/produtos_test.go` -- novos `TestListarCatalogoHandler_Filtro*`/`_400ComEstoqueInvalido`/`_400QMuitoLongo`/`_200VazioParaIDMalformado` + `TestListarCatalogoHandler_FiltroComAgrupar` (patch da revisão).
- `backend/main.go` -- doc comment do bloco de rota do Catálogo atualizado citando a Story 4.2 e os novos query params.
- `frontend/src/components/ui/checkbox.tsx` (novo) -- primitivo shadcn/radix-ui, usado pela primeira vez pelo Catálogo.
- `frontend/src/components/catalogo/BuscaCatalogo.tsx` (+ teste) -- prop opcional `onTermoChange`, chamado de dentro do `aoDigitar` já existente, sem nenhuma outra mudança de comportamento.
- `frontend/src/components/catalogo/CatalogoListagem.tsx` (+ teste) -- prop `termo`, 3 controles de filtro, `fetch` com os novos params, reset de página em qualquer mudança de filtro/termo; carregamento de `/api/categorias`/`/api/estoques` com `try/catch` INDEPENDENTES (corrigido na revisão — ver abaixo).
- `frontend/src/pages/CatalogoPage.tsx` (+ teste) -- estado/debounce (300ms) do termo, conecta `BuscaCatalogo` a `CatalogoListagem`.
- `_bmad-output/implementation-artifacts/spec-4-2-filtros-por-categoria-estoque-e-disponibilidade.md` (este arquivo, novo).

**Review findings breakdown:**
- patch: 4 (medium 1, low 3) -- todos aplicados: (1) `CatalogoListagem` usava `Promise.all` num único `try/catch` para `/api/categorias`+`/api/estoques`, derrubando os DOIS `<Select>` quando só UM dos dois falhava — corrigido com `try/catch` independentes por endpoint, mais 2 novos testes (falha isolada em cada endpoint); (2) nenhum teste HTTP combinava `agrupar=true` com um filtro novo — corrigido com `TestListarCatalogoHandler_FiltroComAgrupar`; (3) nenhum teste provava paginação sobre o conjunto FILTRADO numa página >1 — corrigido com `TestListarCatalogoGrade_PaginacaoSobreConjuntoFiltrado`; (4) nenhum teste travava a classe de alvo de toque nos 3 novos controles de filtro — corrigido com um novo teste dedicado.
- defer: 0.
- reject: 11 (low 11) -- ver `## Review Triage Log` para o detalhe de cada um (duplicação cosmética de query, cobertura desproporcional de dois-ids-malformados-ao-mesmo-tempo, estado de filtro fora da URL, escopo de `filtroUUIDInvalido`, ausência de banner de erro visível na falha de carregar filtros — a spec pede degradação silenciosa deliberadamente —, ausência de teste automatizado de NFR, ausência de índice novo, duas premissas falsas do Edge Case Hunter — quantidade negativa é inalcançável por `CHECK (quantidade >= 0)`, `categoria_id` é `NOT NULL` —, segunda checagem `filtroUUIDInvalido` redundante/inofensiva, e a observação do Intent Alignment Auditor sobre `sprint-status.yaml`/`operator_actions`, que confirma o comportamento correto em vez de apontar um defeito).
- intent_gap: 0, bad_spec: 0.
- Follow-up review recomendado: `true` -- score = 3×1 (medium) + 1×3 (low) = 6 ≥ 5.

**Verificação executada (após os patches):**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` -- sem saída de `gofmt`, build/vet limpos.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -p 1 -count=1 ./...` -- todos os pacotes `ok` (Postgres local do ambiente, sem Docker disponível neste sandbox).
- `cd frontend && npm run lint && npm run build && npm run test` -- `oxlint` limpo, `tsc`+`vite build` ok, 321/321 testes passaram (30 arquivos).
- Auditoria da Matriz I/O: as 12 linhas da matriz têm cobertura de teste correspondente, todos executados e passando (serviço + handler no backend; componente no frontend).

**Riscos residuais:**
- `EXPLAIN ANALYZE` com os 4 filtros ativos simultaneamente sob os ~8.000 Produtos semeados nas Stories 4.1/4.3 não foi reexecutado nesta passada (dados de seed não estavam disponíveis neste ambiente de sandbox sem Docker) -- risco baixo: o volume documentado nas Design Notes das Stories 4.1/4.3 já cobre a mesma ordem de grandeza, e os filtros novos usam o índice existente `idx_produto_estoque_estoque_id` + comparação direta de `categoria_id`, sem nenhum `SEQ SCAN` novo de alto custo introduzido.
- `followup_review_recommended: true` (score 6) -- uma passada de follow-up é esperada antes deste run avançar; nenhum item ficou pendente de decisão humana nesta passada (todos os 4 patches foram aplicados e verificados).
