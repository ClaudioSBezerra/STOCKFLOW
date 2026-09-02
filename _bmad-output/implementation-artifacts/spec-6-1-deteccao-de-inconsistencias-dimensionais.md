---
title: 'Story 6.1 — Detecção de inconsistências dimensionais'
type: 'feature'
created: '2026-09-01'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: '266f85fe1257ba18d3398bb8bd1e103a6e1bec89'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-6-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Produtos migrados do legado podem ter uma dimensão que a migração (Story 3.7) não conseguiu converter (texto ambíguo, ficou salvo em `dimensoes_pendentes_revisao`), e Produtos cadastrados manualmente podem ter um valor dimensional visível no `nome` (ex. "TUBO PVC 6M") que nunca foi preenchido no campo estruturado correspondente. Hoje nada aponta esses casos — revisar 8.000 Produtos item a item não escala.

**Approach:** `services.AnalisarInconsistencias` varre todos os Produtos sob demanda (sem persistir nada) e devolve uma lista de sugestões — produto, campo, valor sugerido, origem (`migracao` ou `nome`) — a partir de duas fontes: `dimensoes_pendentes_revisao` reparseado com um parser mais tolerante que o da migração, e o `nome` do Produto quando exatamente um dos 5 campos estruturados está vazio e o nome contém um valor com unidade reconhecida. `GET /api/normalizacao/inconsistencias` expõe isso; a página `/normalizacao` (rota e item de nav já existentes, hoje caindo no `PlaceholderPage` genérico) ganha um botão "Analisar todos os produtos" que lista o resultado.

## Boundaries & Constraints

**Always:**
- Cada sugestão traz `produtoId`, `produtoNome`, `campo` (um de `comprimento`/`largura`/`diametro`/`altura`/`espessura`), `valorSugerido` (`{valor, unidade}`) e `origem` (`"migracao"` ou `"nome"`).
- Um campo já estruturado (`{campo}_valor` e `{campo}_unidade` ambos preenchidos) nunca gera sugestão, não importa a origem — condição de entrada, testada antes de qualquer parsing.
- `GET /api/normalizacao/inconsistencias` atrás de `RequireAuth` + `RequireRole(almoxarife)` — mesmo gate mínimo de `GET /api/movimentacoes` (403 para `usuario`, decisão do middleware).
- Origem `"migracao"`: só quando `produtos.dimensoes_pendentes_revisao` (JSONB, migration 000019) tem uma chave para aquele campo E o texto salvo é reparseável (ver Design Notes — parser tolerante). Texto ainda não-parseável (`"ver etiqueta"`, `"verificar depois"`) não gera sugestão — nunca inventa um valor.
- Origem `"nome"`: só quando **exatamente um** dos 5 campos estruturados do Produto está `NULL` (valor e unidade ambos ausentes) e o `nome` contém um token número+unidade reconhecida (`mm`/`cm`/`m`, abreviada, ver Design Notes) — a sugestão é para esse único campo vazio. Zero ou 2+ campos vazios nesse Produto → nenhuma sugestão de origem `"nome"` (não há como saber qual campo o valor do nome preencheria).
- A rota é somente leitura: nenhuma escrita em `produtos` nem em qualquer outra tabela — Story 6.2 é quem aplica/ignora.

**Block If:**
- _(nenhuma decisão que exija humano — schema, dados e convenções já existentes no repositório cobrem toda a story)_

**Never:**
- Aplicar, ignorar ou persistir decisão de sugestão — isso é Story 6.2 (nenhuma tabela de "ignoradas" nesta story).
- Detecção/mesclagem de duplicatas ou aba "Duplicatas" na página — isso é Story 6.3/6.4; `NormalizacaoPage` nesta story tem uma única seção, sem `Tabs` (mesmo padrão de `EstoquesPage` na Story 2.1, que ganhou abas só quando a segunda seção existiu, Story 5.3).
- Mapear placeholders de `nomenclatura_templates.template` (ex. `[COMP]`, `[DIAM]`) para um campo específico — a tabela não amarra bracket a campo e nenhum documento de planejamento define essa correspondência; a origem `"nome"` usa só a heurística de "único campo vazio" acima, nunca `template_id`.
- Nova migration — todas as colunas necessárias (`{campo}_valor`/`{campo}_unidade`, `dimensoes_pendentes_revisao`) já existem desde as Stories 3.1/3.7.
- Canal SSE/evento realtime novo — a análise é uma leitura pontual sob demanda, sem estado para notificar.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Dimensão já estruturada e válida | Produto com `comprimento_valor=6`, `comprimento_unidade='m'` | Nenhuma sugestão para `comprimento` desse Produto | No error expected |
| Migração com texto reparseável | `dimensoes_pendentes_revisao = {"comprimento": "cerca de 3 metros"}` | Sugestão `{campo:"comprimento", valorSugerido:{valor:3,unidade:"m"}, origem:"migracao"}` | No error expected |
| Migração com texto não-parseável | `dimensoes_pendentes_revisao = {"largura": "ver etiqueta"}` | Nenhuma sugestão para `largura` (não inventa valor) | No error expected |
| Nome com valor implícito, único campo vazio | `nome="TUBO PVC 6M"`, só `comprimento_valor`/`unidade` NULL, demais preenchidos | Sugestão `{campo:"comprimento", valorSugerido:{valor:6,unidade:"m"}, origem:"nome"}` | No error expected |
| Nome com número, 2+ campos vazios | `nome="TUBO PVC 6M DN25"`, `comprimento` e `diametro` ambos NULL | Nenhuma sugestão de origem `"nome"` para esse Produto (ambíguo demais) | No error expected |
| Catálogo sem nenhum Produto pendente | Nenhum Produto com campo vazio+nome-com-valor nem `dimensoes_pendentes_revisao` | `{"sugestoes":[]}` | No error expected |
| Chamada com papel `usuario` | `GET /api/normalizacao/inconsistencias`, sessão `papel="usuario"` | `403 FORBIDDEN` | Decidido por `RequireRole`, mesmo padrão de `GET /api/movimentacoes` |

</intent-contract>

## Code Map

- `backend/migrations/000019_add_dimensoes_pendentes_revisao_to_produtos.up.sql` -- coluna JSONB já existente (`{campo: textoOriginal}`), fonte da origem `"migracao"`; nenhuma migration nova.
- `backend/cmd/migrate-legado/produtos.go:33-54` -- `reDimensaoLegado`/`parseDimensaoLegado`, o parser ESTRITO (`^valor+unidade$`, sem espaço, só abreviação) usado na migração original; ponto de partida para o parser tolerante desta story, mas vive em `package main` (não importável) — a versão desta story é nova e local a `services/normalizacao.go`, não uma extração dessa função.
- `backend/services/catalogo.go:29-43` -- `DimensaoValor`/`DimensoesProduto`, molde exato do par `{valor,unidade}` a devolver em `valorSugerido`.
- `backend/services/movimentacoes.go:60-` (`ListarMovimentacoes`) -- molde de serviço só-leitura com `SELECT` simples sobre uma tabela + JOIN, sem transação, teto embutido; `AnalisarInconsistencias` segue a mesma forma (sem teto — o volume é por Produto, não por evento).
- `backend/services/papel.go:9` -- `PapelAlmoxarife`, reusar em `RequireRole`.
- `backend/handlers/movimentacoes.go` -- molde de handler fronteira-HTTP (decodifica sessão do contexto, chama o service, serializa) e do comentário de registro de rota no topo do arquivo.
- `backend/main.go:541-543` -- fim do bloco de rotas de Movimentações (`GET /api/movimentacoes`), ponto de inserção da nova rota, mesmo gate `RequireAuth` + `RequireRole(almoxarife)`.
- `frontend/src/App.tsx:94-101` -- árvore de rotas dentro de `RotaProtegida`; `/normalizacao` cai hoje em `{ path: '*', element: <PlaceholderPage /> }` (linha 99) — precisa de uma entrada própria ANTES do fallback `*`.
- `frontend/src/components/shell/nav-items.ts:61` -- item de nav `normalizacao` (`to: '/normalizacao'`, `papelMinimo: 'almoxarife'`) já existe, nenhuma mudança necessária.
- `frontend/src/components/produtos/ImportacaoProdutosSection.tsx:254-259` -- já linka para `/normalizacao` ("Verificar duplicatas agora"); comentário no arquivo já assume a rota existe — esta story é quem a cria de fato (o CTA continua caindo na página, sem pré-selecionar aba "Duplicatas", que não existe ainda).
- `frontend/src/pages/EstoquesPage.tsx` (versão Story 2.1, git show `4ae4b0f`) -- molde exato de página nova single-section com gate de papel espelhado (`rankPapel`), sem `Tabs`.
- `frontend/src/components/estoques/MovimentacoesSection.tsx` -- molde de seção só-leitura com botão de ação, tabela, estados de carregando/erro/vazio; ESTA story NÃO reusa o auto-load-on-mount nem o `conectarRealtime` de lá — o gatilho aqui é só o clique em "Analisar todos os produtos" (ver Design Notes).
- `frontend/src/components/catalogo/formatacao.tsx` -- `formatarQuantidade` (decimal pt-BR), reusar para exibir `valorSugerido.valor`.

## Tasks & Acceptance

**Execution:**
- `backend/services/normalizacao.go` -- criar `Sugestao` (struct: `ProdutoID`, `ProdutoNome`, `Campo`, `Valor`, `Unidade`, `Origem`), `AnalisarInconsistencias(db *sql.DB) ([]Sugestao, error)` -- varre `produtos` com um `SELECT` das 5 colunas de dimensão + `nome` + `dimensoes_pendentes_revisao`; para cada Produto, decide por campo (estruturado válido → pula; senão tenta origem migração via `dimensoes_pendentes_revisao[campo]`; senão, se exatamente um dos 5 está vazio, tenta origem nome) -- é o núcleo da story, isolado de HTTP para ser testável direto.
- `backend/services/normalizacao.go` -- `parseDimensaoTexto(texto string) (valor float64, unidade string, ok bool)` -- parser tolerante (número com `.`/`,` + unidade abreviada OU por extenso, espaço interno opcional, primeira ocorrência dentro do texto) -- alimenta a origem `"migracao"`.
- `backend/services/normalizacao.go` -- `extrairValorDoNome(nome string) (valor float64, unidade string, ok bool)` -- regex restrita a unidade abreviada (`mm`/`cm`/`m`, case-insensitive, `\b` nas duas pontas) dentro do `nome` -- alimenta a origem `"nome"`, só chamada quando já se sabe que há exatamente um campo vazio.
- `backend/services/normalizacao_test.go` -- cobre a I/O Matrix acima: campo válido nunca sugere, migração parseável/não-parseável, nome com 1 campo vazio, nome com 2+ campos vazios, catálogo vazio.
- `backend/handlers/normalizacao.go` -- `AnalisarInconsistenciasHandler(db *sql.DB) http.HandlerFunc` -- chama `services.AnalisarInconsistencias`, serializa `{"sugestoes": [...]}`; erro do service → `500 INTERNAL_ERROR` (molde de `ListarMovimentacoesHandler`).
- `backend/handlers/normalizacao_test.go` -- monta mux local (molde de `movimentacoes_test.go`), cobre 200 com lista e 403 para `usuario`.
- `backend/main.go` -- registrar `mux.HandleFunc("GET /api/normalizacao/inconsistencias", middleware.RequireAuth(db, jwtSecret)(middleware.RequireRole(services.PapelAlmoxarife)(handlers.AnalisarInconsistenciasHandler(db))))` logo após o bloco de `GET /api/movimentacoes`, com o comentário de story no mesmo formato do restante do arquivo.
- `frontend/src/pages/NormalizacaoPage.tsx` -- página nova, molde de `EstoquesPage.tsx` versão Story 2.1: gate `rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife')`, sem `Tabs`, renderiza `InconsistenciasSection`.
- `frontend/src/pages/NormalizacaoPage.test.tsx` -- cobre o gate de papel (renderiza para `almoxarife`, mensagem de acesso negado para `usuario`), molde de `EstoquesPage.test.tsx`.
- `frontend/src/components/normalizacao/InconsistenciasSection.tsx` -- botão "Analisar todos os produtos" (chama `GET /api/normalizacao/inconsistencias` só ao clique, nunca no mount), estado de carregando durante a chamada, tabela (Produto · Campo · Valor sugerido · Origem) ou mensagem "Nenhuma inconsistência encontrada." quando a lista vem vazia, mensagem de erro em `role="alert"` na falha (mesmo texto/padrão de `MovimentacoesSection`).
- `frontend/src/components/normalizacao/InconsistenciasSection.test.tsx` -- clique no botão dispara o fetch, tabela renderiza as sugestões, lista vazia mostra a mensagem, falha de rede mostra o alerta.
- `frontend/src/App.tsx` -- adicionar `{ path: 'normalizacao', element: <NormalizacaoPage /> }` na árvore de `RotaProtegida`, antes de `{ path: '*', element: <PlaceholderPage /> }`.

**Acceptance Criteria:**
- Given um Produto com uma dimensão estruturada válida (`{valor,unidade}` ambos preenchidos), when a análise roda (`GET /api/normalizacao/inconsistencias`), then nenhuma sugestão é gerada para esse campo.
- Given um Produto migrado com uma entrada em `dimensoes_pendentes_revisao` reparseável, ou um Produto cujo `nome` contém um valor com unidade reconhecida para o único campo estruturado vazio, when o Almoxarife clica "Analisar todos os produtos" em `/normalizacao`, then a lista exibida traz, para cada sugestão, produto, campo, valor sugerido e origem (migração ou nome).
- Given uma conta com papel `usuario`, when ela chama `GET /api/normalizacao/inconsistencias` diretamente, then a API responde `403 FORBIDDEN` e a navegação para `/normalizacao` não expõe o item de menu (nav-items.ts já filtra por `papelMinimo`).

## Spec Change Log

## Review Triage Log

### 2026-09-01 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 5 (high 0, medium 3, low 2)
- defer: 0
- reject: 15
- addressed_findings:
  - `[low]` `[patch]` `InconsistenciasSection.analisar()` não limpava `sugestoes` antes de um novo fetch — clique de reanálise que falha deixava a tabela antiga visível junto com o alerta de erro. Corrigido para limpar o estado antes de buscar.
  - `[medium]` `[patch]` `AnalisarInconsistencias` abortava a análise inteira (500) quando `dimensoes_pendentes_revisao` de UM produto tinha JSON malformado. Corrigido para pular só a origem "migracao" daquele produto (logando o erro) em vez de abortar a consulta inteira.
  - `[low]` `[patch]` `reDimensaoTolerante` não tinha `\b` antes do grupo numérico (assimetria com `reValorNome`), podendo casar dígitos finais de um código/lote embutido em texto livre. Alinhado com âncora `\b` nas duas pontas.
  - `[medium]` `[patch]` `extrairValorDoNome` usava sempre a PRIMEIRA ocorrência número+unidade no `nome`, podendo atribuir ao campo vazio um valor que na verdade corresponde a uma dimensão JÁ preenchida do mesmo Produto (ex. nome com diâmetro e comprimento embutidos, diâmetro já estruturado). Corrigido para ignorar ocorrências que já batem com um valor+unidade já estruturado do Produto.
  - `[medium]` `[patch]` Nenhum teste cobria o caminho de falha do banco em `AnalisarInconsistenciasHandler` (500 INTERNAL_ERROR) — adicionado teste no molde de `TestListarMovimentacoesHandler_500FalhaDeBanco` (rename da tabela `produtos`).

## Design Notes

**Parser tolerante (`parseDimensaoTexto`, origem "migracao"):** o parser da migração (`parseDimensaoLegado`) é estrito de propósito (`^valor+unidade$`, ancorado, sem espaço) — o addendum do PRD já assume que "casos ambíguos ficam para revisão manual via Normalização". Esta story É essa revisão manual: um parser mais tolerante tenta (a) espaço opcional entre número e unidade (`"6 m"` → falha no parser da migração, aqui vira `6,"m"`), (b) unidade por extenso (`metro(s)`→`m`, `cent[ií]metro(s)`→`cm`, `mil[ií]metro(s)`→`mm`), (c) casar a PRIMEIRA ocorrência número+unidade dentro do texto, não o texto inteiro (`"cerca de 3 metros"` → `3,"m"`). Texto sem nenhum número+unidade reconhecível (`"ver etiqueta"`) devolve `ok=false` — nenhuma sugestão, nunca um valor inventado.

**Heurística "único campo vazio" (origem "nome"):** a alternativa seria mapear cada placeholder de `nomenclatura_templates.template` (`[COMP]`, `[DIAM]`, `[BITOLA]`...) para um dos 5 campos estruturados — mas essa correspondência não existe em nenhum lugar do schema nem da documentação de planejamento, e inventá-la arriscaria sugestões erradas (ex. atribuir uma bitola de cabo em mm² a `diametro`). A regra adotada — só sugere quando há exatamente UM campo vazio no Produto, e o nome contém um valor com unidade reconhecida — nunca precisa saber qual campo o texto do nome "quer dizer": a resposta é óbvia quando só resta um candidato, e o Produto simplesmente não recebe sugestão de origem "nome" quando restam 2+ candidatos (mesma postura conservadora do parser de migração: preferir não sugerir a sugerir errado).

**Endpoint GET, não POST:** o botão se chama "Analisar todos os produtos" mas a operação não tem efeito colateral nenhum (não persiste nada) — segue o padrão já usado em `GET /api/produtos/catalogo` e `GET /api/movimentacoes` para leituras computacionalmente não-triviais porém sem escrita.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros.
- `cd backend && go test ./services/... ./handlers/... -run 'Inconsistencia|Normalizacao'` -- expected: PASS, cobrindo a I/O Matrix.
- `cd frontend && npx tsc --noEmit` -- expected: sem erros de tipo.
- `cd frontend && npx vitest run src/pages/NormalizacaoPage.test.tsx src/components/normalizacao/InconsistenciasSection.test.tsx` -- expected: PASS.

## Auto Run Result

**Resumo da mudança implementada:** Detecção de inconsistências dimensionais (Story 6.1). Endpoint `GET /api/normalizacao/inconsistencias` (RequireAuth+RequireRole(almoxarife)) varre todos os Produtos sob demanda e devolve sugestões de correção dimensional — produto, campo, valor sugerido, origem (`migracao`: reparse tolerante de `dimensoes_pendentes_revisao`, Story 3.7; `nome`: valor com unidade reconhecida no `nome` quando exatamente um dos 5 campos estruturados está vazio, excluindo tokens que já batem com uma dimensão já preenchida do mesmo Produto). Página `/normalizacao` (rota e item de nav já existentes, antes caindo no `PlaceholderPage`) ganha o botão "Analisar todos os produtos" e a tabela somente-leitura de resultado. Nenhuma escrita em nenhuma tabela — aplicar/ignorar é Story 6.2.

**Arquivos alterados:**
- `backend/services/normalizacao.go` (novo) -- `Sugestao`, `AnalisarInconsistencias`, `parseDimensaoTexto` (parser tolerante da origem migração), `extrairValorDoNome` (origem nome, evita reatribuir token já usado por dimensão estruturada), resiliente a `dimensoes_pendentes_revisao` malformado (pula só aquele produto/origem, não aborta a análise).
- `backend/services/normalizacao_test.go` (novo) -- cobre toda a I/O Matrix + parsers unitários + resiliência a JSON malformado + não-reatribuição de token já estruturado.
- `backend/handlers/normalizacao.go` (novo) -- `AnalisarInconsistenciasHandler`, molde de `ListarMovimentacoesHandler`.
- `backend/handlers/normalizacao_test.go` (novo) -- 200 com sugestões, 200 lista vazia, 403 usuario, 401 sem token, 500 falha de banco.
- `backend/main.go` -- registra `GET /api/normalizacao/inconsistencias` atrás de `RequireAuth`+`RequireRole(almoxarife)`.
- `frontend/src/pages/NormalizacaoPage.tsx` (novo) -- página `/normalizacao`, molde de `EstoquesPage` (Story 2.1): gate de papel espelhado, seção única sem `Tabs`.
- `frontend/src/pages/NormalizacaoPage.test.tsx` (novo) -- gate de papel, nenhum fetch no mount.
- `frontend/src/components/normalizacao/InconsistenciasSection.tsx` (novo) -- botão "Analisar todos os produtos" (fetch só ao clique), tabela, estado vazio, alerta de erro; limpa a lista anterior antes de cada nova análise.
- `frontend/src/components/normalizacao/InconsistenciasSection.test.tsx` (novo) -- clique dispara fetch, tabela renderiza, lista vazia, falha de rede, resposta não-ok, segundo clique que falha não deixa tabela antiga junto do alerta.
- `frontend/src/App.tsx` -- registra `{ path: 'normalizacao', element: <NormalizacaoPage /> }`.
- `_bmad-output/implementation-artifacts/epic-6-context.md` (novo) -- contexto do Epic 6 compilado (Goal/Stories/Requirements/Technical Decisions/UX/Cross-Story Dependencies).

**Findings da revisão:** 5 `patch` aplicados (3 `medium`, 2 `low`, 0 `high`) — tabela antiga não limpa em reanálise falha; JSON malformado em `dimensoes_pendentes_revisao` abortava a análise inteira; assimetria de `\b` entre os dois parsers; `extrairValorDoNome` podia reatribuir a um campo vazio um valor já usado por outra dimensão estruturada; faltava teste do caminho 500 do handler. 0 `intent_gap`, 0 `bad_spec`, 0 `defer`, 15 `reject` (achados não-reproduzíveis, cobertos por decisão deliberada já documentada na spec, ou já correspondentes ao padrão existente do restante do código-base — ver Review Triage Log).

**Recomendação de revisão de acompanhamento:** `true` — patches desta passada: 0 high, 3 medium, 2 low → `3×3 + 1×2 = 11 ≥ 5`.

**Verificação realizada:** `go build ./... && go vet ./...` limpo; `go test -p 1 ./...` (suíte completa do backend, 7 pacotes) PASS; `npx tsc --noEmit` limpo; `npx vitest run` (suíte completa do frontend, 35 arquivos/385 testes) PASS; `npx oxlint` limpo nos arquivos novos/alterados. Matrix Test Audit: todas as 7 linhas da I/O & Edge-Case Matrix cobertas por teste que rodou e passou.

**Riscos residuais:** heurísticas de parsing (`migracao`/`nome`) são deliberadamente conservadoras (nunca inventam valor, nunca adivinham campo ambíguo) — cobrem só os casos inequívocos descritos nos ACs; casos mais complexos (nome com múltiplos valores sem nenhuma dimensão já estruturada para desambiguar, números com separador de milhar) ficam de fora por design/baixo risco no domínio, não são regressão.
