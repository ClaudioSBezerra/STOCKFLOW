---
title: 'Story 6.3 — Detecção de duplicatas'
type: 'feature'
created: '2026-09-02'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '3b0a1dedda90585d13fb260aa4d168c841f98d8d'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-6-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      A detecção de duplicatas (FR-19) não considera `categoria_id` — dois Produtos com nome
      normalizado igual, dimensões equivalentes e local em comum, mas em categorias diferentes,
      são agrupados como candidatos a duplicata mesmo assim.
    evidence: |-
      FR-19 do PRD e o Intent Contract de spec-6-3 definem o agrupamento explicitamente como
      nome normalizado + dimensões equivalentes + locais coincidentes — sem menção a categoria;
      `DetectarDuplicatas` (backend/services/normalizacao.go) segue essa definição à risca. Em
      teoria dois Produtos de categorias diferentes poderiam colidir nesses 3 critérios, mas a
      nomenclatura guiada por subtipo (Story 3.2) torna nomes idênticos entre categorias distintas
      pouco prováveis na prática.
    location: >-
      backend/services/normalizacao.go (DetectarDuplicatas/dimensoesEquivalentes)
    severity: medium
  - summary: >-
      `DuplicatasSection` não distingue visualmente/acessivelmente onde um grupo de duplicatas
      termina e o próximo começa quando `GET /api/normalizacao/duplicatas` devolve múltiplos
      grupos — cada grupo é só uma tabela com os mesmos cabeçalhos "Produto"/"Dimensões", sem
      heading ou caption tipo "Grupo N".
    evidence: |-
      frontend/src/components/normalizacao/DuplicatasSection.tsx renderiza `grupos.map(...)` como
      uma sequência de `<table>` com espaçamento visual (`gap-4`) mas nenhum landmark/heading por
      grupo — leitor de tela não tem como anunciar a fronteira entre grupos.
    location: >-
      frontend/src/components/normalizacao/DuplicatasSection.tsx
    severity: low
  - summary: >-
      `DuplicatasSection` não anuncia a conclusão bem-sucedida da análise para leitor de tela —
      só o caminho de erro tem `role="alert"`; um usuário de leitor de tela que clica "Analisar
      duplicatas" não recebe nenhum sinal de que a operação terminou quando ela dá certo.
    evidence: |-
      frontend/src/components/normalizacao/DuplicatasSection.tsx só usa `role="alert"` no `<p>`
      de erro (linha ~120); não há `aria-live`/`role="status"` para o resultado de sucesso. Este é
      o mesmo padrão pré-existente de `InconsistenciasSection.tsx` (Story 6.1, também só
      `role="alert"` no erro, confirmado por grep) — `DuplicatasSection` é molde explícito desse
      componente (Code Map de spec-6-3), então herdou fielmente a lacuna em vez de introduzi-la.
    location: >-
      frontend/src/components/normalizacao/DuplicatasSection.tsx (e, na origem, InconsistenciasSection.tsx)
    severity: low
---

<intent-contract>

## Intent

**Problem:** Nada no sistema aponta Produtos duplicados (mesmo item cadastrado 2+ vezes — comum após reimportação de planilha sem atualização por código, addendum PRD item 4) — revisar até 8.000 Produtos manualmente não escala.

**Approach:** `services.DetectarDuplicatas` varre todos os Produtos sob demanda (sem persistir nada) e agrupa por nome normalizado + dimensões estruturadas equivalentes + local em comum, expondo `GET /api/normalizacao/duplicatas`; `/normalizacao` ganha uma segunda aba "Duplicatas" (molde `EstoquesPage`, Story 5.3) e o CTA "Verificar duplicatas agora" do relatório de importação (Story 3.4) passa a levar direto a essa aba com a análise já em andamento.

## Boundaries & Constraints

**Always:**
- Um grupo exige as 3 condições simultâneas (FR-19): nome normalizado (sem acento, case-insensitive, aparado nas pontas — glossário do PRD) igual + as 5 dimensões estruturadas equivalentes (par a par, convertendo mm/cm/m para a mesma unidade antes de comparar) + ao menos 1 `estoque_id` em comum entre TODOS os Produtos do grupo (via `produto_estoque`).
- Um campo dimensional só é "equivalente" entre dois Produtos quando os dois lados estão no MESMO estado: ambos `NULL` nesse campo, ou ambos preenchidos com valor numericamente igual após conversão. Um campo preenchido de um lado e vazio do outro torna os dois Produtos NÃO equivalentes (nunca agrupa) — evita falso positivo entre um Produto já corrigido (Story 6.1/6.2) e um ainda pendente de revisão.
- Rota só-leitura: nenhuma escrita, nenhum soft-delete, nenhuma mesclagem — isso é Story 6.4. `GET /api/normalizacao/duplicatas` atrás do mesmo gate `RequireAuth` + `RequireRole(almoxarife)` das outras rotas de Normalização.
- Clique em "Verificar duplicatas agora" (relatório de importação) leva a `/normalizacao` já na aba Duplicatas com a análise em andamento — sem exigir um segundo clique do Almoxarife.
- Sem publicação em tempo real (SSE): análise sob demanda, nenhum estado persistido para notificar — mesmo padrão de `GET /api/normalizacao/inconsistencias`.

**Never:**
- Mesclar, soft-deletar ou alterar qualquer Produto — fora de escopo (Story 6.4).
- Persistir grupos detectados — cada chamada recalcula do zero, mesmo padrão de `AnalisarInconsistencias`.
- Usar `codigo`/SKU como parte da chave de agrupamento — FR-19 é só nome + dimensão + local.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Duplicata clara | 2 Produtos "Tubo PVC 25mm" — `{diametro:25,mm}` e `{diametro:2.5,cm}` — ambos com linha em `produto_estoque` para o mesmo Estoque | 1 grupo com os 2 Produtos | Nenhum erro |
| Mesmo nome, dimensão diferente | 2 "Parafuso M6" — `comprimento` 20mm vs 30mm | Não agrupados | Nenhum erro |
| Mesmo nome+dimensão, sem local em comum | Produto A só em Estoque X, Produto B só em Estoque Y | Não agrupados | Nenhum erro |
| Campo dimensional parcialmente preenchido | A com `altura`=10mm, B com `altura` NULL (pendente de revisão) | Não agrupados | Nenhum erro |
| Falha de banco durante a consulta | Conexão indisponível | — | `500 INTERNAL_ERROR` |
| Papel `usuario` chama a rota diretamente | `GET /api/normalizacao/duplicatas` sem `almoxarife`+ | — | `403 FORBIDDEN` |

</intent-contract>

## Code Map

- `backend/services/normalizacao.go` -- acrescentar `DetectarDuplicatas(db) ([]GrupoDuplicata, error)`: 1 query em `produtos` (mesmas 12 colunas de `AnalisarInconsistencias`, sem paginação) + 1 query em `produto_estoque` (produto_id, estoque_id) carregando `map[string]map[string]bool` de locais por produto; agrupa por `nomeNormalizado` (novo `normalizarNomeProduto`, `strings.NewReplacer` de acentos pt-BR — sem dependência nova) e, dentro de cada balde, testa pares por dimensão equivalente (novo `converterParaMM(valor float64, unidade string) float64` + comparação `%.3f`, mesmo truque de `chaveIgnorada`) + interseção de locais não-vazia; una pares que batem via union-find simples (`map[string]string`) e devolva componentes conectados de tamanho >= 2, ordenados por `(nome, id)` do primeiro membro.
- `backend/services/catalogo.go:31` (`DimensaoValor`), `:38` (`DimensoesProduto`), `:108-121` (`parDimensao`/`paraDimensao`) -- reusar sem alteração para montar `Dimensoes` de cada Produto do grupo.
- `backend/services/normalizacao.go:78` (`ordemCamposDimensao`) -- reusar para iterar os 5 campos na comparação par a par.
- `backend/services/normalizacao_test.go` -- cobre a I/O Matrix (`TestDetectarDuplicatas_*`).
- `backend/handlers/normalizacao.go` -- acrescentar `DetectarDuplicatasHandler(db) http.HandlerFunc`, molde exato de `AnalisarInconsistenciasHandler` (linha 40-57): guarda de `UsuarioDaSessao` ausente -> 500, chama o service, `200 {"grupos":[...]}`.
- `backend/handlers/normalizacao_test.go` -- cobre 200 com/sem grupos, 403, 401, 500 do novo handler.
- `backend/main.go:570` -- registrar `GET /api/normalizacao/duplicatas` logo após a rota `POST /api/normalizacao/ignoradas`, mesmo `RequireAuth`+`RequireRole(almoxarife)`.
- `frontend/src/pages/NormalizacaoPage.tsx` -- ganha `Tabs` (molde `EstoquesPage.tsx`, Story 5.3): abas "Inconsistências" (`InconsistenciasSection`, já existente) e "Duplicatas" (`DuplicatasSection`, novo); lê `useSearchParams()` — `verificarDuplicatas=1` define a aba inicial como Duplicatas.
- `frontend/src/components/normalizacao/DuplicatasSection.tsx` (novo) -- molde de `InconsistenciasSection.tsx`: botão "Analisar duplicatas" chama `GET /api/normalizacao/duplicatas`; renderiza cada grupo (produtos + dimensões); prop `autoAnalisar?: boolean` dispara a análise uma vez no mount via `useEffect` quando `true` (idioma de `VerificarEmailPage.tsx`, mas disparando uma ação em vez de decidir estado inicial a partir de `token`).
- `frontend/src/components/normalizacao/DuplicatasSection.test.tsx` (novo) -- cobre clique manual, `autoAnalisar`, vazio, e falha de rede.
- `frontend/src/components/produtos/ImportacaoProdutosSection.tsx:257-259` -- `Link to="/normalizacao"` passa a `Link to="/normalizacao?verificarDuplicatas=1"`; atualizar o comentário (linhas 252-256) que hoje documenta o disparo como fora de escopo da Story 3.4 — agora é o escopo desta story.
- `frontend/src/components/produtos/ImportacaoProdutosSection.test.tsx:342` -- atualizar o `expect(cta).toHaveAttribute('href', '/normalizacao')` para o novo href com query param.
- `frontend/src/components/ui/tabs.tsx` -- componente já usado por `EstoquesPage`/`CatalogoPage`, reusar sem alteração.

## Tasks & Acceptance

**Execution:**
- `backend/services/normalizacao.go` -- `DetectarDuplicatas`, `normalizarNomeProduto`, `converterParaMM`, união por pares -- núcleo da story.
- `backend/services/normalizacao_test.go` -- I/O Matrix completa.
- `backend/handlers/normalizacao.go` -- `DetectarDuplicatasHandler` -- fronteira HTTP.
- `backend/handlers/normalizacao_test.go` -- 200/403/401/500.
- `backend/main.go` -- registrar `GET /api/normalizacao/duplicatas`.
- `frontend/src/pages/NormalizacaoPage.tsx` -- `Tabs` com Inconsistências/Duplicatas, leitura de `?verificarDuplicatas=1`.
- `frontend/src/components/normalizacao/DuplicatasSection.tsx` -- botão manual + `autoAnalisar` on-mount.
- `frontend/src/components/normalizacao/DuplicatasSection.test.tsx` -- cobre os 4 cenários citados no Code Map.
- `frontend/src/components/produtos/ImportacaoProdutosSection.tsx` -- CTA passa a incluir `?verificarDuplicatas=1`.
- `frontend/src/components/produtos/ImportacaoProdutosSection.test.tsx` -- assert do novo href.

**Acceptance Criteria:**
- Given dois ou mais Produtos com nome normalizado igual, dimensões equivalentes (considerando conversão de unidade) e locais coincidentes, when a detecção de duplicatas roda, then esses Produtos aparecem agrupados como candidatos a mesclagem.
- Given Produtos com nome normalizado igual mas dimensões diferentes, when a detecção roda, then eles não são agrupados como duplicatas.
- Given o relatório de importação (Stories 3.3/3.4), when o Almoxarife clica "Verificar duplicatas agora", then é levado direto à aba Duplicatas de `/normalizacao` com a análise já em andamento.

## Spec Change Log

## Review Triage Log

### 2026-09-02 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 6 (high 0, medium 2, low 4)
- defer: 2 (medium 1, low 1)
- reject: 6
- addressed_findings:
  - `[low]` `[patch]` `DetectarDuplicatas` (normalizacao.go) fazia `rows.Close()` manual em 3 pontos de retorno em vez de `defer rows.Close()` (ao contrário de `carregarLocaisProduto`, no mesmo arquivo) — mais frágil a um futuro early-return que esqueça o close. Trocado por `defer rows.Close()`, mesmo padrão da função irmã.
  - `[low]` `[patch]` Nenhum teste provava um grupo genuíno de 3+ membros com interseção TOTAL de locais (só o caso de falha "encadeado" — `TestDetectarDuplicatas_TresMembrosSemInterseccaoTotalNaoAgrupa` — e casos de 2 membros existiam). Adicionado `TestDetectarDuplicatas_TresMembrosComInterseccaoTotalAgrupaEmUmGrupo`.
  - `[low]` `[patch]` Nenhum teste provava 2 grupos de duplicata independentes numa mesma chamada (sem contaminação cruzada, ordenação determinística). Adicionado `TestDetectarDuplicatas_MultiplosGruposIndependentesNaoContaminam`.
  - `[low]` `[patch]` O caso "catálogo com Produtos, nenhum duplicado" só era provado na camada de serviço — a fronteira HTTP não tinha o equivalente. Adicionado `TestDetectarDuplicatasHandler_200ListaVaziaComProdutosNaoDuplicados`.
  - `[medium]` `[patch]` `?verificarDuplicatas=1` disparava a análise automática TODA vez que `DuplicatasSection` remontava (Radix `TabsContent` desmonta a aba inativa por padrão — trocar para "Inconsistências" e voltar para "Duplicatas" remontava o componente e resetava o guard `useRef` local), não só uma vez ao aterrissar vindo do CTA — desviava do Intent Contract ("análise já em andamento" descreve UM disparo, não um re-disparo a cada troca de aba). Guard "já disparei nesta visita" movido de `DuplicatasSection` (que remonta) para `NormalizacaoPage` (que não remonta ao trocar de aba): novo estado `autoAnalisarConsumido` + prop `onAutoAnalisado` chamada no instante do disparo automático. Teste novo em `NormalizacaoPage.test.tsx` prova que trocar de aba e voltar não redispara o fetch.
  - `[medium]` `[patch]` O caminho de erro de `carregarLocaisProduto` (falha ao consultar `produto_estoque`) nunca era exercitado — os únicos testes de falha de banco (`TestDetectarDuplicatas_FalhaDeBanco`, `TestDetectarDuplicatasHandler_500FalhaDeBanco`) renomeiam `produtos`, o que quebra a query ANTES de `carregarLocaisProduto` ser alcançada. Uma regressão que engolisse esse erro específico (`locais, _ := carregarLocaisProduto(db)`) devolveria silenciosamente `200 {"grupos":[]}` em vez de `500`, sem nenhum teste acusando. Adicionado `TestDetectarDuplicatas_FalhaDeBancoAoCarregarLocais` (renomeia `produto_estoque`).

  Deferidos nesta passada (2, ambos reais mas fora do escopo desta story): agrupamento não considera `categoria_id` — FR-19/Intent Contract definem a chave de agrupamento explicitamente como nome+dimensão+local, sem categoria; `DuplicatasSection` não distingue visual/acessivelmente onde um grupo termina e o próximo começa quando há múltiplos grupos (melhoria de UX/a11y, não exigida por nenhuma AC).

  Rejeitados nesta passada (6, todos por design, já cobertos, ou inalcançáveis): `carregarLocaisProduto` carrega `produto_estoque` inteira sem filtro prévio (mesmo padrão de scan completo já estabelecido por `AnalisarInconsistencias`/Story 6.1, sem SLA de performance definido em nenhum lugar do projeto); comparação par a par dentro de um balde de nome é O(k²) sem teto (mesma classe de "sem paginação/teto" já deliberada nas stories 6.1/6.2, catálogo na casa de milhares, não milhões); o design "pré-filtro par a par + validação de interseção total" descarta silenciosamente um par A-B genuíno quando um Produto C não relacionado encadeia via outro local (exatamente o comportamento documentado nas Design Notes desta spec e coberto por teste dedicado — decisão deliberada, não defeito); `converterParaMM` trata unidade não reconhecida como "mm" (`default`) em vez de erro — inalcançável na prática, `unidade` é um ENUM do Postgres fechado em mm/cm/m, sempre validado antes de gravar; `useEffect` do auto-disparo sem `AbortController`/guarda de unmount — mesma classe de achado já rejeitado no review de spec-6-2 como "no-op inofensivo em React 19", padrão consistente com `analisar()` de `InconsistenciasSection` desde Story 6.1; ausência de teste específico para linha de `produto_estoque` com `quantidade=0` — a query de `carregarLocaisProduto` nem sequer lê a coluna `quantidade` (só `produto_id`,`estoque_id`), não há comportamento condicional a testar.

### 2026-09-02 — Review pass (fresh, spec status was `done`)
- intent_gap: 0
- bad_spec: 0
- patch: 3 (high 0, medium 0, low 3)
- defer: 1 (medium 0, low 1)
- reject: 11
- addressed_findings:
  - `[low]` `[patch]` `TestDetectarDuplicatasHandler_200ListaVazia` e `TestDetectarDuplicatasHandler_200ListaVaziaComProdutosNaoDuplicados` comparavam o corpo bruto contra duas strings literais (com/sem `\n` final) em vez de decodificar o JSON — um hedge contra um detalhe de serialização em vez de afirmar o comportamento real. Trocado por decode + `len(resp.Grupos) == 0`, mesmo padrão de decode já usado pelos outros testes do arquivo.
  - `[low]` `[patch]` `TestDetectarDuplicatasHandler_200ComGrupos` nunca afirmava `len(resp.Grupos) == 1` — um grupo espúrio extra passaria despercebido. Adicionada a asserção de cardinalidade antes do loop de busca do grupo esperado.
  - `[low]` `[patch]` O teste "resposta não-ok (ex. 500) mostra o mesmo alerta" (`DuplicatasSection.test.tsx`) não repetia a asserção `queryByRole('table')).not.toBeInTheDocument()` presente no teste irmão de falha de rede — o caminho 500 nunca provava que a tabela ficava oculta. Adicionada a mesma asserção.

  Deferido nesta passada (1, real mas herdado, não introduzido por esta story): `DuplicatasSection` não anuncia a conclusão bem-sucedida da análise a leitor de tela (só `role="alert"` no erro) — confirmado por grep que `InconsistenciasSection.tsx` (Story 6.1), do qual `DuplicatasSection` é molde explícito, tem exatamente a mesma lacuna; padrão herdado fielmente, não defeito novo desta story.

  Rejeitados nesta passada (11): heading truncado dos itens DW-62/DW-63 recém-adicionados em `deferred-work.md` — fora da minha autoridade nesta execução (o ledger é propriedade do orquestrador, instrução explícita da invocação de não modificar/reabrir entradas existentes) e não é código desta story; resultado da análise de duplicatas se perde ao trocar de aba e voltar (`DuplicatasSection` remonta, `grupos` reseta) — por design: cada chamada recalcula do zero, nenhum estado persistido (Always da spec), reclicar é custo tolerável; `onAutoAnalisado` passado sem `useCallback` — o próprio achado admite ser inofensivo hoje graças ao guard `useRef` interno; `Tabs` usa `defaultValue` não-controlado (risco teórico de dessincronia com `verificarDuplicatas=1` numa segunda visita sem remount) — investigado via `App.tsx`/rotas: `ImportacaoProdutosSection` vive em rota diferente de `NormalizacaoPage`, então todo caminho alcançável até `/normalizacao?verificarDuplicatas=1` já causa remount completo, cenário não alcançável na prática; `converterParaMM` unidade desconhecida vira "mm" — mesmo achado já rejeitado nesta própria spec na passada anterior (unidade é ENUM Postgres fechado); falta de teste unitário dedicado para `converterParaMM`/`dimensaoEquivalente` — já cobertos indiretamente pelos testes de integração de `DetectarDuplicatas` (incluindo o caso exato de conversão mm/cm), mesmo estilo de teste já usado por `dimensoesEquivalentes`/`locaisEmComumPar` no resto do arquivo; seeds de teste fixam sempre a mesma `categoria_id` sem teste documentando colisão entre categorias — redundante com DW-62, já deferido; falta de teste de bucket de nome com dezenas/centenas de Produtos (O(k²)) — mesmo achado já rejeitado nesta própria spec na passada anterior (mesma classe de "sem paginação/teto" já deliberada nas stories 6.1/6.2); ausência de comentário inline sobre o custo O(k²) do algoritmo — cosmético, a razão já está documentada nas Design Notes; `useEffect` de `analisar()` sem `AbortController`/guarda de unmount (2 variantes do mesmo achado) — mesma classe já rejeitada no review de spec-6-2 como "no-op inofensivo em React 19", padrão consistente com `InconsistenciasSection` desde Story 6.1.

## Design Notes

**"Locais coincidentes" = interseção não vazia entre TODOS os membros do grupo:** a AC fala em "dois ou mais Produtos" com locais coincidentes, sem detalhar o caso de 3+ Produtos sem sobreposição total (A∩B não-vazio, mas C não compartilha nenhum local com A nem B). Esta story resolve o cluster completo por interseção total (todos os membros do grupo compartilham ao menos 1 `estoque_id` em comum) em vez de fechamento transitivo par a par — mais simples, cobre o caso real descrito no PRD (reimportação da mesma planilha, mesmos locais) e evita grupos "encadeados" onde dois membros do mesmo grupo não têm nenhum local em comum entre si, o que confundiria a revisão humana da Story 6.4.

**Dimensão parcialmente preenchida nunca agrupa:** a alternativa seria comparar só os campos que os dois lados têm preenchidos, ignorando os vazios — mas isso arrisca agrupar um Produto genuinamente diferente (ex. dois comprimentos possíveis para o mesmo diâmetro) só porque um dos dois ainda não passou pela Story 6.1/6.2. Exigir o mesmo estado (`NULL`/`NULL` ou preenchido/preenchido com valor igual) em cada um dos 5 campos é mais conservador — prioriza menos falsos positivos, já que a mesclagem (Story 6.4) é destrutiva/irreversível.

**Conversão para milímetros + comparação textual `%.3f`:** mesma técnica de `chaveIgnorada` (Story 6.2) — evita comparar `float64` de origens diferentes (leitura direta do banco) por igualdade exata, que é frágil a arredondamento na conversão de unidade (ex. 2.5cm -> 25.0mm sem perda, mas outros valores podem gerar dízima).

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros.
- `cd backend && go test ./services/... ./handlers/... -run 'Normalizacao|Duplicata'` -- expected: PASS, cobrindo a I/O Matrix.
- `cd frontend && npx tsc --noEmit` -- expected: sem erros de tipo.
- `cd frontend && npx vitest run src/components/normalizacao/DuplicatasSection.test.tsx src/components/produtos/ImportacaoProdutosSection.test.tsx` -- expected: PASS.

## Auto Run Result

**Resumo:** Story 6.3 (Detecção de duplicatas) já estava com `status: done` ao início desta execução. Como o modo `bmad-build-auto` reabre specs `done` para uma passada de revisão fresca, esta execução rodou apenas a revisão (4 camadas em paralelo: blind hunter, edge-case hunter, verification-gap, intent-alignment) sobre o diff completo desde `baseline_revision` (3b0a1dedda90585d13fb260aa4d168c841f98d8d) e aplicou 3 patches de baixa severidade em testes já existentes. Nenhuma mudança de comportamento em produção.

**Arquivos alterados nesta passada:**
- `backend/handlers/normalizacao_test.go` -- `TestDetectarDuplicatasHandler_200ListaVazia` e `TestDetectarDuplicatasHandler_200ListaVaziaComProdutosNaoDuplicados` agora decodificam o JSON e afirmam `len(resp.Grupos) == 0` em vez de comparar a string bruta do corpo contra dois literais; `TestDetectarDuplicatasHandler_200ComGrupos` agora afirma `len(resp.Grupos) == 1` antes de procurar o grupo esperado.
- `frontend/src/components/normalizacao/DuplicatasSection.test.tsx` -- o teste de resposta 500 agora também afirma `queryByRole('table')).not.toBeInTheDocument()`, no mesmo padrão do teste irmão de falha de rede.

**Review findings:** patch 3 (low 3, medium 0, high 0); defer 1 (low 1); reject 11; intent_gap 0; bad_spec 0.

**Follow-up review recommendation:** `false` (patched-only score = 3×0 medium + 1×3 low = 3, abaixo de 5; nenhum patch de severidade high).

**Verificação executada nesta passada:**
- `cd backend && go build ./... && go vet ./...` -- sem erros.
- `cd backend && go test ./services/... ./handlers/... -run 'Normalizacao|Duplicata'` -- PASS (`ok stockflow/backend/services`, `ok stockflow/backend/handlers`).
- `cd frontend && npx tsc --noEmit` -- sem erros de tipo.
- `cd frontend && npx vitest run src/components/normalizacao/DuplicatasSection.test.tsx src/components/produtos/ImportacaoProdutosSection.test.tsx src/pages/NormalizacaoPage.test.tsx` -- PASS (3 arquivos, 24 testes).

**Riscos residuais:** o item deferido nesta passada (falta de anúncio de conclusão bem-sucedida para leitor de tela) é herdado de `InconsistenciasSection` (Story 6.1) e não exclusivo desta story — corrigi-lo isoladamente aqui criaria inconsistência de padrão entre as duas seções da mesma página; melhor tratado numa passada dedicada de a11y que cubra ambas. Os 2 itens já deferidos na passada anterior (`categoria_id` fora da chave de agrupamento; grupos sem heading acessível) permanecem em aberto, sem mudança nesta passada.

