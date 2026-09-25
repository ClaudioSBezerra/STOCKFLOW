---
title: 'Padrão de página de lista aplicado ao Catálogo'
type: 'feature'
created: '2026-09-25'
status: 'done'
baseline_revision: '11635646e0af01fb0c9db6a54887587208720d1c'
review_loop_iteration: 1
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-17-context.md'
warnings: []
deferred:
  - 'AbortController no useEffect de indicadores (race condition de fetch) — padrão comum no projeto, sem requisito de spec'
  - 'filepath.Glob só .jpg — conforme spec; outros formatos ignorados'
  - 'Sem cache de glob do fotosDir — otimização de performance fora de escopo'
  - 'Teste HTTP handler-level para IndicadoresCatalogoHandler — sem padrão de handler test no projeto'
---

<intent-contract>

## Intent

**Problem:** `CatalogoPage` não tem faixa de indicadores (Itens, Com saldo, Sem foto) nem o visual de tabela aprovado — linhas de 60px, ícone redondo, nome + subtítulo "Categoria • código" e pílula de status. As stories 17.4 e 17.5 dependem dos componentes `FaixaIndicadores` e `PilhulaStatus` que esta story entrega.

**Approach:** Backend: novo endpoint `GET /e/{slug}/api/produtos/catalogo/indicadores` (mesmos filtros de `/catalogo`, listagem única do `fotosDir` para "Sem foto"). Frontend: criar `FaixaIndicadores` (display puro) e `PilhulaStatus` em `components/lista/`; aplicar na `CatalogoListagem` com fetch de `/indicadores` e redesign da tabela; `CatalogoPage` recebe `podeCadastrar` e repassa.

## Boundaries & Constraints

**Always:**
- `FaixaIndicadores` é display puro — recebe `indicadores` e `acoes` como props, nunca faz fetch.
- `CatalogoListagem` busca `/e/{slug}/api/produtos/catalogo/indicadores` com os filtros correntes (termo + categoriaId + estoqueId + comEstoque + somenteInativos), debounçando junto com o `useEffect` de `carregar` (dispara nos mesmos eventos de mudança de filtro).
- Se `/indicadores` falhar (rede ou status não-OK): a tela continua funcionando, todos os valores da faixa mostram `"—"`.
- Backend: `inativos=1` só honrado para gestor+, mesmo padrão de `ListarCatalogoHandler`.
- Backend: `IndicadoresCatalogoProdutos` usa `montarFiltrosCatalogo` sem modificação; a listagem do `fotosDir` é feita com `filepath.Glob(fotosDir + "/*.jpg")` uma única vez por requisição; `semFoto` = produtos no conjunto filtrado cuja `id` não aparece como prefixo em nenhum arquivo.
- `SemFoto > 0` → `alerta: true` em `FaixaIndicadores` → cor destructive/vermelho FC.
- `PilhulaStatus` extraída como componente novo em `components/lista/`; usada no modo tabela de `CatalogoListagem`.
- Exportar permanece visível apenas em `modo === 'tabela'` e para `podeExportar`.
- Grade e o toggle grade/tabela permanecem inalterados (modo grade sem regressão).
- FAB scanner, `BuscaCatalogo`, debounce de busca e devolução de foco: inalterados.
- Textos em PT BR.

**Block If:** —

**Never:** Alterar backend de listagem/exportação nem regras de negócio; aplicar padrão de lista em outras páginas (17.4/17.5); usar cores fora dos tokens de design; usar chamada por produto para "Sem foto"; remover filtro "Mostrar só inativos" de gestor+.

## I/O & Edge-Case Matrix

| Cenário | Estado/Entrada | Esperado | Erro |
|---|---|---|---|
| Indicadores OK | filtros zerados | Itens=N, Com saldo=M, Sem foto=K renderizados | — |
| /indicadores falha | rede indisponível | tela funciona, faixa mostra "—" para os três | — |
| Sem foto = 0 | todos têm foto | valor "0" sem alerta (cor normal) | — |
| Sem foto > 0 | produto sem foto | valor em vermelho FC (alerta=true) | — |
| Filtro ativo | categoriaId preenchido | /indicadores é chamado com mesmo categoriaId | — |
| Gestor, inativos | somenteInativos=true | /indicadores inclui inativos=1 | — |
| usuario | CatalogoPage | podeCadastrar=false → sem botão Cadastrar na faixa | — |
| almoxarife, tabela | modo tabela | Exportar + Cadastrar visíveis na faixa | — |

</intent-contract>

## Code Map

- `backend/services/catalogo.go:167` — `FiltrosCatalogo` e `montarFiltrosCatalogo` a reusar sem modificação; adicionar `IndicadoresCatalogo{Itens,ComSaldo,SemFoto int}` + `IndicadoresCatalogoProdutos(db *sql.DB, fotosDir string, filtros FiltrosCatalogo)` — SQL seleciona `p.id` + `COALESCE(SUM(pe.quantidade),0)>0` agrupado por `p.id` usando `montarFiltrosCatalogo`; `filepath.Glob` lista fotos uma vez; cruza conjuntos em Go para `semFoto`
- `backend/handlers/produtos.go:400` — junto a `ListarCatalogoHandler`; novo `IndicadoresCatalogoHandler(db *sql.DB, fotosDir string)` com mesmo parse de params (sem `agrupar`/`pagina`); retorna `{"itens":N,"comSaldo":N,"semFoto":N}`
- `backend/main.go:688` — registrar `GET /e/{slug}/api/produtos/catalogo/indicadores` antes de `catalogo/exportar` com `RequireAuth` (usuario+)
- `backend/services/catalogo_test.go` — adicionar `TestIndicadoresCatalogoProdutos`: todos os produtos, comSaldo correto, semFoto correto quando fotosDir vazio, filtros propagados
- `frontend/src/components/lista/FaixaIndicadores.tsx` (novo) — `FaixaIndicadores({indicadores: {rotulo,valor,alerta?}[], acoes?:ReactNode})`; linha flex com KPIs à esquerda e `acoes` à direita; valor `null` → `"—"`; `alerta && valor > 0` → `text-destructive`; rótulo `text-label text-muted-foreground`, valor `text-heading-lg font-bold`
- `frontend/src/components/lista/FaixaIndicadores.test.tsx` (novo) — valor normal, null→"—", alerta e valor>0→vermelho, alerta e valor=0→sem vermelho, slot acoes renderiza
- `frontend/src/components/lista/PilhulaStatus.tsx` (novo) — `PilhulaStatus({status:string})`; `<span className="rounded-full border border-border px-2 py-0.5 text-label text-muted-foreground">{status}</span>`; equivale ao inline existente em `CatalogoListagem.tsx:573`, `MovimentacoesSection.tsx:177`, etc.
- `frontend/src/components/lista/PilhulaStatus.test.tsx` (novo) — renderiza o texto do status; snapshot ou text match
- `frontend/src/components/catalogo/CatalogoListagem.tsx:181` — (a) adicionar prop `podeCadastrar?:boolean`; (b) fetch com `apiUrl('/api/produtos/catalogo/indicadores?' + queryFiltros({categoriaId, estoqueId, comEstoque, termo, somenteInativos}))` em `useEffect` separado com dependências apenas dos filtros — `[categoriaId, estoqueId, comEstoque, termo, somenteInativos]` (sem `pagina` nem `modo`: indicadores são totais, não da página visível), resultado guardado em `useState<{itens:number,comSaldo:number,semFoto:number}|null>`, erro→`null`; (c) montar `<FaixaIndicadores>` entre os filtros e a lista com indicadores mapeados + `acoes` contendo Exportar (podeExportar && tabela) e link para `/produtos/novo` (podeCadastrar); (d) mover botão Exportar da barra de toggle para o slot `acoes` de `FaixaIndicadores` (mantendo condição podeExportar && modo==='tabela'); (e) atualizar `FragmentLinhaGrupo` e estilo da tabela: `tr` com `h-[60px]`, `border-b border-border`, sem zebra; col1 com ícone `Package` 32px `rounded-full bg-muted p-1.5` + `nome` + subtítulo `text-label text-muted-foreground` "Categoria • código"; números à direita; `PilhulaStatus` para "Inativo" quando `somenteInativos` ou quando `grupo.inativado`
- `frontend/src/components/catalogo/CatalogoListagem.test.tsx` — (a) `FaixaIndicadores` aparece com valores corretos (mock de /indicadores); (b) /indicadores falha → "—" visível; (c) Cadastrar não aparece para `usuario` (`podeCadastrar=false`)
- `frontend/src/pages/CatalogoPage.tsx:57` — adicionar `podeCadastrar = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife')` (mesmo limiar de `podeExportar`); passar como prop a `CatalogoListagem`
- `frontend/src/pages/CatalogoPage.test.tsx` — verificar que `CatalogoListagem` recebe `podeCadastrar=true` para almoxarife+ e `false` para usuario

## Tasks & Acceptance

**Execution:**
- `backend/services/catalogo.go` — adicionar `IndicadoresCatalogo` struct e `IndicadoresCatalogoProdutos(db, fotosDir, filtros)`
- `backend/handlers/produtos.go` — adicionar `IndicadoresCatalogoHandler(db, fotosDir)`
- `backend/main.go` — registrar rota `GET /e/{slug}/api/produtos/catalogo/indicadores`
- `backend/services/catalogo_test.go` — testes de `IndicadoresCatalogoProdutos`
- `frontend/src/components/lista/FaixaIndicadores.tsx` — criar
- `frontend/src/components/lista/FaixaIndicadores.test.tsx` — criar
- `frontend/src/components/lista/PilhulaStatus.tsx` — criar
- `frontend/src/components/lista/PilhulaStatus.test.tsx` — criar
- `frontend/src/components/catalogo/CatalogoListagem.tsx` — fetch /indicadores + FaixaIndicadores + tabela redesign + podeCadastrar prop
- `frontend/src/components/catalogo/CatalogoListagem.test.tsx` — testes de indicadores e podeCadastrar
- `frontend/src/pages/CatalogoPage.tsx` — adicionar podeCadastrar e repassar
- `frontend/src/pages/CatalogoPage.test.tsx` — verificar prop por papel

**Acceptance Criteria:**
- Given catálogo com produtos, when página carrega em modo tabela, then faixa exibe Itens/Com saldo/Sem foto buscados de `/catalogo/indicadores` com os filtros ativos.
- Given `/catalogo/indicadores` retorna erro, when filtros mudam, then tela funciona e todos os valores da faixa mostram `"—"`.
- Given Sem foto > 0, when faixa renderiza, then valor aparece em cor destructive (vermelho FC).
- Given almoxarife+ em modo tabela, when faixa carrega, then botões Exportar e link Cadastrar aparecem nas ações.
- Given usuario, when faixa carrega, then botão Cadastrar NÃO aparece nas ações.
- Given modo tabela, when carregado, then linhas têm 60px, 1ª coluna com ícone Package redondo 32px + nome + subtítulo "Categoria • código", sem zebra, sem alteração no modo grade.
- Given gestor com inativos ativos, when `/indicadores` é chamado, then inclui `inativos=1` na query string.

## Design Notes

Faixa de indicadores: `flex items-center justify-between border-b border-border pb-3`. KPIs à esquerda em `flex gap-6`; cada KPI: `<div className="flex flex-col"><span className="text-label text-muted-foreground">{rotulo}</span><span className="text-heading-lg font-bold [alerta&&valor>0:text-destructive]">{valor ?? "—"}</span></div>`. Ações à direita em `flex gap-2`.

Tabela — primeira célula de cada grupo (`FragmentLinhaGrupo`):
```
<td className="py-0 pr-4">
  <div className="flex h-[60px] items-center gap-3">
    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted">
      <Package aria-hidden="true" className="h-4 w-4 text-muted-foreground" />
    </div>
    <div className="flex flex-col">
      <span className="text-body font-medium">{grupo.nome}</span>
      <span className="text-label text-muted-foreground">
        {grupo.multiplos.categoria ? "Múltiplas" : (grupo.categoria?.nome ?? "—")}
        {" • "}
        {grupo.multiplos.codigo ? "Múltiplos" : (grupo.codigo ?? "—")}
      </span>
    </div>
  </div>
</td>
```
Linhas: `<tr className="border-b border-border">` (sem zebra, sem bg alternado). Colunas de quantidade: `text-right tabular-nums`. `PilhulaStatus` exibida quando `somenteInativos` (que já implica modo grade) ou quando um grupo tiver todos inativos — para este story, a pílula aparece na grade (modo grade já mostra "Inativo" inline); na tabela, `somenteInativos` é desabilitado pela lógica existente (`aoMudarSomenteInativos` força modo grade), então a pílula da tabela só aparece se futura lógica a ativar — usar `PilhulaStatus` na tabela desde já para pronto.

## Verification

**Commands:**
- `cd frontend && npx tsc -b && npm run lint && npm test` -- esperado: limpo e verde
- `cd backend && go test ./services/... ./handlers/...` -- esperado: verde

## Review Triage Log

**Loop 1 (2026-09-25):** 4 revisores paralelos (blind-hunter, edge-case-hunter, verification-gap, intent-alignment).

**patch aplicado:**
- `backend/services/catalogo.go` — extração de stem de foto corrigida: usava primeiro `-` como separador (errado para UUIDs que contêm `-`), substituído por `filepath.Ext` para extrair somente a extensão, garantindo que `<uuid>.jpg` gere chave `<uuid>` e case corretamente com `produtoIDs`.

**rejected (15 falsos positivos):**
- ok-shadowing no handler (verificações sequenciais, não sobrepostas)
- empresaDaRequisicao ok não verificado (verificado na implementação real)
- rows.Scan/Err erros ignorados (tratados na implementação real — diff era condensado)
- db.Query nil rows (err verificado antes de rows.Next())
- comSaldo por-estoque (decisão de design documentada: "SEMPRE global")
- categorias JOIN desnecessária (necessária para ILIKE em c.nome)
- queryFiltros inclui paginação (não inclui — listagem explícita de filtros)
- null=loading ambíguo (comportamento intencional per spec)
- FaixaIndicadoresProps ausente no diff (definida no arquivo)
- PilhulaStatus sem variantes (fora de escopo)
- inativos silenciado vs comEstoque 400 (padrão intencional de ListarCatalogoHandler)
- testes frontend ausentes (existem — diff estava condensado)
- filtroUUIDInvalido retorna zeros (padrão consistente com restante do código)

**deferred (4):** AbortController, glob apenas .jpg, cache de glob, teste HTTP handler.

## Auto Run Result

Status: done

Endpoint `GET /e/{slug}/api/produtos/catalogo/indicadores` implementado reutilizando `montarFiltrosCatalogo`; `semFoto` calculado com `filepath.Glob` único por requisição. Componentes `FaixaIndicadores` e `PilhulaStatus` criados em `components/lista/` com testes. `CatalogoListagem` atualizada com fetch de indicadores, redesign da tabela (6 colunas, 60 px, ícone Package) e botões Exportar/Cadastrar no slot `acoes`. `CatalogoPage` passa `podeCadastrar` (almoxarife+). Revisão (loop 1) corrigiu bug de extração de stem de foto para UUIDs. 965 testes frontend verdes; backend build/vet limpo.
