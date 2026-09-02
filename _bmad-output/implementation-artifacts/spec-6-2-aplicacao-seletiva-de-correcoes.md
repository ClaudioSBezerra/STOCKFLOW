---
title: 'Story 6.2 — Aplicação seletiva de correções'
type: 'feature'
created: '2026-09-02'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '0b49957dcf9eecc188691ae0c61186a4b902ddc2'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-6-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      Um item de lote que fica obsoleto entre a listagem e o clique de "Aplicar selecionadas" some silenciosamente da tabela sem mensagem explicando por quê.
    evidence: |-
      InconsistenciasSection.tsx só remove da tabela as linhas confirmadas em `aplicadas`; um item que não veio na resposta (campo preenchido por outra ação nesse meio-tempo) simplesmente permanece na tabela sem nenhum aviso ao usuário sobre por que aquela correção específica não foi aplicada.
    location: >-
      frontend/src/components/normalizacao/InconsistenciasSection.tsx
    severity: low
  - summary: >-
      `chaveIgnorada` compara valores por chave textual `%.3f` em vez de igualdade exata de float64, o que é uma escolha deliberada mas carrega um risco teórico de arredondamento em valores exatamente no limite de 3 casas decimais.
    evidence: |-
      Um valor nascido de `strconv.ParseFloat` (Go) e o mesmo valor ida-e-volta por uma coluna `NUMERIC(10,3)` (Postgres) podem, em tese, formatar diferente em `%.3f` num caso de arredondamento de fronteira (ex. algo terminando em `.xxx5`), quebrando silenciosamente o match do "ignorar" para esse produto/campo específico. Documentado como trade-off aceito nas Design Notes da spec; nenhum caso real observado nos testes.
    location: >-
      backend/services/normalizacao.go (chaveIgnorada)
    severity: low
  - summary: >-
      Ações de aplicar/ignorar bem-sucedidas não são anunciadas a leitores de tela — só o caminho de falha usa `role="alert"`.
    evidence: |-
      A tabela remove a linha e o rótulo do botão "Aplicar selecionadas (N)" atualiza visualmente em sucesso, mas nenhuma região `aria-live` cobre esse caminho; um usuário de leitor de tela não recebe nenhuma confirmação de que a ação funcionou, só silêncio.
    location: >-
      frontend/src/components/normalizacao/InconsistenciasSection.tsx
    severity: low
  - summary: >-
      `normalizacao_ignoradas.produto_id` referencia `produtos(id)` sem política `ON DELETE` definida, e a interação com a futura mesclagem de duplicatas (Story 6.3/6.4) não está documentada.
    evidence: |-
      A FK vai bloquear a exclusão de qualquer Produto com sugestão ignorada associada (mesmo padrão de `importacao_linhas`/`produto_estoque`), mas nenhuma Design Note trata explicitamente como a mesclagem de duplicatas (que provavelmente remove/funde linhas de Produto) deve lidar com as linhas de `normalizacao_ignoradas` do produto removido.
    location: >-
      backend/migrations/000023_create_normalizacao_ignoradas.up.sql
    severity: low
---

<intent-contract>

## Intent

**Problem:** `GET /api/normalizacao/inconsistencias` (Story 6.1) só lista sugestões — nada permite ao Almoxarife aplicar um valor sugerido ao Produto, nem descartar uma sugestão que ele já revisou e decidiu não aplicar; sem isso, ele teria que corrigir 8.000 Produtos fora do sistema.
**Approach:** `services.AplicarCorrecoes` grava o valor sugerido nos campos vazios indicados (individualmente, em lote por produto ou em lote geral — os 3 modos são só tamanhos diferentes da mesma lista); `services.IgnorarSugestao` grava a tupla exata (produto, campo, valor, unidade) numa tabela nova `normalizacao_ignoradas`, e `AnalisarInconsistencias` (Story 6.1) passa a excluir da lista qualquer sugestão cuja tupla já esteja lá — reaparece sozinha se o valor mudar, pois a chave é o valor exato.

## Boundaries & Constraints

**Always:**
- `POST /api/normalizacao/correcoes` aceita `{"correcoes":[{"produtoId","campo","valorSugerido":{"valor","unidade"}}]}` (1..N itens — mesmo shape de `Sugestao.MarshalJSON`); cobre aceitar individual (1 item), lote por produto e lote geral (N itens, o front-end é quem decide o agrupamento via seleção de checkbox).
- Toda validação de `correcoes` (campo ∈ {comprimento,largura,diametro,altura,espessura}, valor>0 e ≤`limiteNumeric103`, unidade ∈ {mm,cm,m} — mesmas regras de `validarDimensao`, produtos.go) roda ANTES de qualquer escrita; uma lista vazia também é erro de validação.
- Cada correção só é aplicada (`UPDATE produtos SET {campo}_valor=$,{campo}_unidade=$ ...`) se o campo AINDA estiver `NULL` nas duas colunas no momento do UPDATE (`WHERE ... AND {campo}_valor IS NULL AND {campo}_unidade IS NULL`) — nunca sobrescreve um valor que outra ação já preencheu enquanto a lista estava aberta na tela; a resposta `{"aplicadas":[{"produtoId","campo"}]}` só lista as que realmente afetaram uma linha, para o front-end remover da tabela só essas.
- `POST /api/normalizacao/ignoradas` aceita `{"produtoId","campo","valorSugerido":{"valor","unidade"}}` (um item — ação inline "Ignorar" por linha), mesma validação de campo/valor/unidade; grava em `normalizacao_ignoradas` com `ON CONFLICT (produto_id,campo,valor,unidade) DO NOTHING` (idempotente — reenviar a mesma tupla nunca é erro).
- Os dois endpoints ficam atrás de `RequireAuth`+`RequireRole(almoxarife)`, mesmo gate de `GET /api/normalizacao/inconsistencias`.
- `AnalisarInconsistencias` carrega `normalizacao_ignoradas` inteira antes de varrer `produtos` e descarta qualquer sugestão cuja tupla `(produtoId,campo,valor,unidade)` já exista lá — comparação por chave textual (`fmt.Sprintf("%.3f", valor)`, mesma escala de `NUMERIC(10,3)`) para não depender de igualdade exata de float64 entre o valor computado em Go e o que veio ida-e-volta pelo Postgres.
- Cada `UPDATE produtos` bem-sucedido publica `{"resource":"produtos","id":<produtoId>,"change":"updated"}` no canal `produtos` (uma vez por `produtoId` distinto tocado no lote, não por campo).

**Block If:**
- _(nenhuma decisão que exija humano — schema e convenções já existentes cobrem toda a story)_

**Never:**
- Reavaliar a sugestão contra o estado atual do Produto antes de aplicar (ex. rerodar `parseDimensaoTexto`/`extrairValorDoNome`) — o cliente já tem o `valorSugerido` exato de uma chamada recente a `GET /api/normalizacao/inconsistencias`; só o guard `IS NULL` acima protege contra sobrescrita.
- Trilha de auditoria (quem aplicou/ignorou, quando) — nenhuma AC desta story exige isso, ao contrário da mesclagem (Story 6.4, que tem `MESCLAGEM_PRODUTOS_REMOVIDOS` dedicada).
- Endpoint para "reverter" um ignorar ou uma correção aplicada — fora do escopo das ACs.
- Detecção/mesclagem de duplicatas ou aba "Duplicatas" — Story 6.3/6.4; `NormalizacaoPage` continua com seção única, sem `Tabs`.
- Nova coluna em `produtos` — as 5 colunas de dimensão já existem (migration 000011); só a tabela `normalizacao_ignoradas` é nova (migration 000023).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Aplicar individual | 1 correção, campo vazio | Campo gravado, `aplicadas=[item]` | No error expected |
| Aplicar lote com item obsoleto | 2 correções; uma delas já tem o campo preenchido (concorrência) | A outra é gravada normalmente; a obsoleta some de `aplicadas` sem abortar o lote | No error expected |
| Correção com campo inválido | `campo:"peso"` | Nenhuma escrita | `400 VALIDATION_ERROR` |
| Correção com `correcoes:[]` | Lista vazia | Nenhuma escrita | `400 VALIDATION_ERROR` |
| Ignorar uma sugestão | `{produtoId,campo,valorSugerido}` | Linha gravada em `normalizacao_ignoradas`; nova chamada a `GET /api/normalizacao/inconsistencias` não traz mais essa tupla | No error expected |
| Ignorar a mesma tupla duas vezes | Mesmo `{produtoId,campo,valorSugerido}` reenviado | Sucesso idempotente, nenhuma linha duplicada | No error expected |
| Valor muda para outro inconsistente depois de ignorado | Tupla antiga ignorada; `dimensoes_pendentes_revisao`/`nome` agora produz um valor DIFERENTE | Nova análise traz a sugestão de novo (tupla nova, chave diferente) | No error expected |
| Chamada com papel `usuario` | `POST /api/normalizacao/correcoes` ou `/ignoradas` | Nenhuma escrita | `403 FORBIDDEN` |

</intent-contract>

## Code Map

- `backend/migrations/000023_create_normalizacao_ignoradas.up.sql`/`.down.sql` (novo) -- `CREATE TABLE normalizacao_ignoradas (produto_id UUID NOT NULL REFERENCES produtos(id), campo VARCHAR(20) NOT NULL CHECK (campo IN ('comprimento','largura','diametro','altura','espessura')), valor NUMERIC(10,3) NOT NULL, unidade dimensao_unidade NOT NULL, criado_em TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY (produto_id, campo, valor, unidade))` -- `dimensao_unidade` já existe (migration 000011).
- `backend/services/normalizacao.go` -- `ordemCamposDimensao` já é a lista fechada dos 5 campos, reusar para validar `campo`; adicionar `AplicarCorrecoes(db, correcoes []CorrecaoInput) ([]CorrecaoAplicada, error)`, `IgnorarSugestao(db, produtoID, campo string, valor float64, unidade string) error`, e um carregamento de `normalizacao_ignoradas` no início de `AnalisarInconsistencias` para filtrar sugestões já ignoradas.
- `backend/services/produtos.go:22` (`unidadesDimensaoValidas`), `:32` (`limiteNumeric103`), `:97` (`ErroProdutoValidacao`) -- reusar as 3 para a validação de `AplicarCorrecoes`/`IgnorarSugestao`, mesmo erro/mensagens de `CriarProduto`.
- `backend/services/produtos.go:107` (`ErrProdutoNaoEncontrado`), `:16` (`pqForeignKeyViolation`) -- `IgnorarSugestao` mapeia violação de FK/UUID inválido em `produtoId` para este erro, mesmo colapso de `CriarProduto`.
- `backend/services/auth.go:32` (`pqUniqueViolation`) -- não deveria disparar com `ON CONFLICT DO NOTHING`, mas documentar a mesma constante se precisar tratar corrida.
- `backend/handlers/normalizacao.go` -- `AplicarCorrecoesHandler(db, registro *realtime.Registry)` e `IgnorarSugestaoHandler(db)`, molde de `AtualizarNomeProdutoHandler` (produtos.go:188): decodifica sob `http.MaxBytesReader`, chama o service, mapeia `ErroProdutoValidacao`→400, `ErrProdutoNaoEncontrado`→404, publica no canal `produtos` só no handler de aplicar.
- `backend/main.go:553-555` -- registrar as 2 rotas novas logo após `GET /api/normalizacao/inconsistencias`, mesmo `RequireAuth`+`RequireRole(almoxarife)`.
- `backend/services/normalizacao_test.go` -- cobre a I/O Matrix dos services.
- `backend/handlers/normalizacao_test.go` -- cobre a I/O Matrix dos handlers + 403/401.
- `frontend/src/components/normalizacao/InconsistenciasSection.tsx` -- ganha coluna de `Checkbox` por linha + "Aplicar selecionadas", ação inline "Aceitar" (1 item) e "Ignorar" por linha; usa `authHeaders()`/padrão de fetch já presentes no arquivo.
- `frontend/src/components/normalizacao/InconsistenciasSection.test.tsx` -- cobre seleção + aplicar lote, aceitar individual, ignorar, e as duas falhas de rede.
- `frontend/src/components/ui/checkbox.tsx` -- componente já usado em `CatalogoListagem.tsx`, reusar sem alteração.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000023_create_normalizacao_ignoradas.up.sql`/`.down.sql` -- criar/derrubar a tabela -- fonte de verdade do estado "ignorado".
- `backend/services/normalizacao.go` -- `CorrecaoInput{ProdutoID, Campo string; Valor float64; Unidade string}`, `AplicarCorrecoes` (valida tudo, abre 1 transação, um `UPDATE` guardado por item, devolve só as que afetaram linha), `IgnorarSugestao` (`INSERT ... ON CONFLICT DO NOTHING`), filtro de ignoradas em `AnalisarInconsistencias` -- núcleo da story.
- `backend/services/normalizacao_test.go` -- cobre a I/O Matrix + o filtro de ignoradas dentro de `AnalisarInconsistencias`.
- `backend/handlers/normalizacao.go` -- `AplicarCorrecoesHandler`, `IgnorarSugestaoHandler` -- fronteira HTTP.
- `backend/handlers/normalizacao_test.go` -- 200/400/403/401 dos 2 handlers.
- `backend/main.go` -- registrar `POST /api/normalizacao/correcoes` e `POST /api/normalizacao/ignoradas`.
- `frontend/src/components/normalizacao/InconsistenciasSection.tsx` -- checkbox por linha, "Aplicar selecionadas", "Aceitar"/"Ignorar" inline; remove da tabela só o que o servidor confirmou (`aplicadas` na resposta, ou sucesso do ignorar).
- `frontend/src/components/normalizacao/InconsistenciasSection.test.tsx` -- cobre os 3 fluxos + falha de rede em cada ação.

**Acceptance Criteria:**
- Given uma lista de sugestões de inconsistência, when o Almoxarife aplica uma correção individualmente, em lote por produto (via seleção de checkboxes das linhas desse produto) ou em lote geral (seleção de todas), then os campos correspondentes são atualizados para o valor estruturado sugerido e essas linhas somem da tabela sem reload manual.
- Given uma sugestão marcada como "Ignorar" para um valor específico, when a mesma inconsistência (mesmo produto, campo, valor) é reavaliada depois (`GET /api/normalizacao/inconsistencias` roda de novo), then ela não reaparece.
- Given um campo cujo valor de origem muda depois para um novo valor inconsistente diferente do que foi ignorado, when a análise roda de novo, then a sugestão reaparece com o novo valor.
- Given uma conta com papel `usuario`, when ela chama `POST /api/normalizacao/correcoes` ou `POST /api/normalizacao/ignoradas` diretamente, then a API responde `403 FORBIDDEN`.

## Spec Change Log

## Review Triage Log

### 2026-09-02 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 4 (high 0, medium 1, low 3)
- defer: 2 (low 2)
- reject: 5
- addressed_findings:
  - `[medium]` `[patch]` `AplicarCorrecoes` abortava o lote INTEIRO (500) quando um `produtoId` malformado/inexistente causava erro no `UPDATE` — contradizia o próprio contrato "item obsoleto nunca aborta o lote". Corrigido com `SAVEPOINT`/`ROLLBACK TO SAVEPOINT` por item: um `produtoId` malformado (mesma classe de erro `pqForeignKeyViolation`/`pqInvalidTextRepresentation` que `IgnorarSugestao` já tratava) reverte só aquele item e o lote continua; erro de banco genuinamente inesperado ainda falha o lote inteiro. Testes novos: `TestAplicarCorrecao_LoteComProdutoIdMalformadoNaoAbortaLote` (service), `TestAplicarCorrecaoHandler_200LoteComProdutoIdMalformadoNaoAborta` (handler).
  - `[low]` `[patch]` `POST /api/normalizacao/correcoes` reusava `authRequestMaxBytes` (64KB, dimensionado para login) — pequeno demais para um "lote geral" real. Nova constante dedicada `normalizacaoCorrecoesRequestMaxBytes` (1MB) só para esse endpoint; `/ignoradas` (1 item por chamada) manteve `authRequestMaxBytes`.
  - `[low]` `[patch]` Nenhum teste cobria falha de banco (500 INTERNAL_ERROR) para os 2 handlers novos, ao contrário do padrão já estabelecido para o GET (`TestAnalisarInconsistenciasHandler_500FalhaDeBanco`). Adicionados `TestAplicarCorrecaoHandler_500FalhaDeBanco` e `TestSugestaoIgnoradaHandler_500FalhaDeBanco`.
  - `[low]` `[patch]` Botão "Analisar todos os produtos" (`InconsistenciasSection.tsx`) só desabilitava por `carregando`, permitindo clique durante uma ação de aplicar/ignorar em andamento (`processando`) — corrida entre as duas famílias de ação. Corrigido para `disabled={carregando || processando}`.

### 2026-09-02 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 8 (high 0, medium 0, low 8)
- defer: 0
- reject: 12
- addressed_findings:
  - `[low]` `[patch]` Botões inline "Aceitar"/"Ignorar" (`InconsistenciasSection.tsx`) não tinham `aria-label`, ao contrário do `Checkbox` da mesma linha — leitor de tela via muitos botões "Aceitar"/"Ignorar" indistinguíveis. Adicionado `aria-label={`Aceitar/Ignorar ${produtoNome} - ${campo}`}` nos dois; testes que localizavam por `getByRole('button', {name:'Aceitar'})` dentro da linha atualizados para o novo nome acessível.
  - `[low]` `[patch]` Comentário de `AplicarCorrecoes` (normalizacao.go) afirmava que campo já preenchido, `produtoId` inexistente e `produtoId` malformado "colapsam no mesmo tratamento via SAVEPOINT" — impreciso: só o `produtoId` malformado dispara erro do Postgres e usa `ROLLBACK TO SAVEPOINT`; os outros dois só resultam em 0 linhas afetadas, sem erro. Comentário corrigido para descrever os dois caminhos separadamente.
  - `[low]` `[patch]` `correcaoRequest` e `ignorarSugestaoRequest` (handlers/normalizacao.go) declaravam os mesmos 3 campos verbatim. `IgnorarSugestaoHandler` passou a decodificar direto em `correcaoRequest`, removendo o tipo duplicado.
  - `[low]` `[patch]` Nenhum teste cobria um lote em que TODOS os itens já estão obsoletos (só "um obsoleto + um válido" era coberto). Adicionado `TestAplicarCorrecao_LoteTotalmenteObsoletoRetornaVazio`, provando `200 {"aplicadas":[]}` sem erro.
  - `[low]` `[patch]` `valor > limiteNumeric103` (borda de `validarCorrecao`, nova nesta story) não tinha teste para `AplicarCorrecoes` nem `IgnorarSugestao` — um regressão nessa checagem viraria erro de overflow do Postgres (500) em vez de `400 VALIDATION_ERROR`. Estendido `TestAplicarCorrecao_ValorZeroOuUnidadeInvalidaSaoRejeitados` com esse caso e adicionado `TestSugestaoIgnorada_ValorAcimaDoLimiteRejeitado`.
  - `[low]` `[patch]` `IgnorarSugestao` com `produtoId` malformado (não-UUID) não tinha teste — só o caso "UUID bem-formado mas inexistente" (`pqForeignKeyViolation`) era coberto; o caminho `pqInvalidTextRepresentation` do mesmo `if` ficava sem verificação. Adicionado `TestSugestaoIgnorada_ProdutoIdMalformado`.
  - `[low]` `[patch]` `normalizacaoCorrecoesRequestMaxBytes` (1MB, decisão deliberada desta story) não tinha teste confirmando que o teto realmente é aplicado. Adicionado `TestAplicarCorrecaoHandler_400CorpoAcimaDoLimite`.
  - `[low]` `[patch]` O fix de corrida `disabled={carregando || processando}` (pass anterior) não tinha teste de regressão — reverter para `disabled={carregando}` não quebraria nenhum teste existente. Adicionado teste que mantém `/correcoes` pendente, clica "Aceitar" e confirma que "Analisar todos os produtos" fica desabilitado até a resposta resolver.

  Rejeitados nesta passada (12, todos ruído ou já cobertos): item que fica obsoleto some sem aviso no caso de LOTE (generaliza DW-58, já deferido); ausência de "desfazer Ignorar" (fora de escopo explícito no Never da intent-contract); falta de agrupamento/ordenação por produto na tabela (melhoria de UX, não exigida pelas ACs); ausência de teto de itens por lote (coberto indiretamente pelo teto de 1MB); `carregarIgnoradas` sem paginação (preocupação especulativa de escala, não um defeito atual); "reject: 5" do pass anterior não listado individualmente (meta-observação sobre o processo, não acionável via patch); teste de `produtoId` bem-formado mas inexistente em `AplicarCorrecoes` (mesmo caminho de código 0-linhas-afetadas já coberto por `TestAplicarCorrecao_LoteComItemObsoleto`); 2 achados do edge-case-hunter duplicados de DW-58/DW-59 (já deferidos); risco de colisão de `key` React ao reusar `chave(produtoId,campo)` (verificado: `sugeridoPorMigracao` em `AnalisarInconsistencias` garante no máximo 1 sugestão por par produto+campo, colisão inalcançável); assimetria entre `AplicarCorrecoes` (item some silenciosamente) e `IgnorarSugestao` (404) para `produtoId` inexistente/malformado (diferença defensável: tolerância em lote vs. erro explícito em ação única, sem violar nenhuma cláusula Always/Never); testes 403 verificam só o status HTTP, não a ausência de escrita no banco (garantia já é estrutural via `RequireRole` rodar antes do handler).

### 2026-09-02 — Review pass (follow-up, via bmad-build-auto)
- intent_gap: 0
- bad_spec: 0
- patch: 1 (high 0, medium 0, low 1)
- defer: 2 (low 2)
- reject: 12
- addressed_findings:
  - `[low]` `[patch]` `AnalisarInconsistencias` (services/normalizacao.go) carrega `normalizacao_ignoradas` ANTES de varrer `produtos` e propaga o erro corretamente (`return nil, err`), mas nenhum teste tornava essa tabela indisponível para provar o caminho de falha — só `TestAnalisarInconsistenciasHandler_500FalhaDeBanco` (renomeia `produtos`) e `TestSugestaoIgnoradaHandler_500FalhaDeBanco` (renomeia `normalizacao_ignoradas`, mas via `IgnorarSugestao`, não `AnalisarInconsistencias`) existiam; uma regressão que engolisse o erro de `carregarIgnoradas` (ex. `ignoradas, _ := carregarIgnoradas(db)`) reapareceria sugestões já ignoradas com `200` em vez de `500`, sem nenhum teste acusando. Adicionado `TestAnalisarInconsistenciasHandler_500FalhaAoCarregarIgnoradas` (handlers/normalizacao_test.go), mesmo molde dos dois testes 500 já existentes, renomeando `normalizacao_ignoradas` e chamando `GET /api/normalizacao/inconsistencias`.

  Deferidos nesta passada (2, ambos reais mas não bloqueiam nenhuma AC): ações de aplicar/ignorar bem-sucedidas não têm região `aria-live` anunciando sucesso a leitores de tela (só a falha usa `role="alert"`); `normalizacao_ignoradas.produto_id` referencia `produtos(id)` sem `ON DELETE` definido e sem Design Note cobrindo a interação com a futura mesclagem de duplicatas (Story 6.3/6.4).

  Rejeitados nesta passada (12, todos ruído, já cobertos, ou consistentes com convenção já estabelecida no arquivo/codebase): front-end descarta a mensagem de validação do servidor em erro não-2xx, mostrando sempre o texto genérico de "tente novamente" (mesmo padrão de `analisar()`, já existente desde Story 6.1 — `res.ok`/`catch` genéricos — e o caminho 400 é praticamente inalcançável via UI real, já que campo/valor/unidade vêm direto da sugestão que o próprio servidor gerou); erro de validação em lote não identifica qual item falhou por índice/produtoId (a Code Map desta story exige explicitamente "mesmo erro/mensagens de CriarProduto", que também não identifica item — comportamento é o pedido pela spec, não um defeito); `POST /api/normalizacao/ignoradas` não tem teste dedicado provando que `authRequestMaxBytes` é respeitado (mecanismo já coberto para os outros endpoints que reusam a mesma constante, cobertura redundante do mesmo `http.MaxBytesReader`); ausência de teto de itens no lote de correções (generaliza achado já rejeitado no pass anterior — "coberto indiretamente pelo teto de 1MB"); tabela de Inconsistências sem paginação/virtualização para até 8.000 Produtos (generaliza achado já rejeitado no pass anterior — "preocupação especulativa de escala, não um defeito atual"); `processando` desabilita todas as linhas/botões da tabela em vez de só a linha/ação em andamento (é a correção deliberada de corrida feita no primeiro pass de review desta story — reverter para granularidade por linha reabriria exatamente aquela corrida); checkbox "Selecionar todas" sem estado indeterminado quando só parte das linhas está marcada (melhoria de UX não exigida por nenhuma AC, mesma classe de achados de UX já rejeitados nos passes anteriores); `camposDimensaoValidos` (normalizacao.go), `ordemCamposDimensao` e o `CHECK` da migration 000023 repetem a mesma lista de 5 campos em 3 lugares (verificado: `camposDimensaoValidos` é documentado no próprio código como "`ordemCamposDimensao` como conjunto", reuso deliberado pedido pelo Code Map da spec; o `CHECK` no banco é defesa em camada separada, prática padrão, não elimina a necessidade de validação em Go); `fetch` de `aplicar`/`ignorar` sem `AbortController`/guarda de unmount (mesmo padrão já existente em `analisar()` desde Story 6.1; React 19 não emite mais warning de `setState` após unmount, é um no-op inofensivo nesta versão); `slog.Error` dos 2 handlers novos não inclui contexto extra (batch size, produtoId, usuário) — confere exatamente com a convenção de log de TODOS os outros handlers do projeto (produtos.go, movimentacoes, etc.), nenhum inclui esse contexto; 2 achados do edge-case-hunter duplicados verbatim de DW-58 (item obsoleto some sem aviso) e DW-59 (`chaveIgnorada` `%.3f`), ambos já deferidos nos passes anteriores.

## Design Notes

**Guard `IS NULL` em vez de reavaliar a sugestão:** a alternativa seria `AplicarCorrecoes` rechamar `parseDimensaoTexto`/`extrairValorDoNome` para confirmar que o valor enviado ainda é "a sugestão atual" antes de gravar — mas isso duplica a lógica de análise dentro do caminho de escrita só para o caso raro de duas abas do mesmo Almoxarife aplicando o mesmo campo ao mesmo tempo. O guard `{campo}_valor IS NULL AND {campo}_unidade IS NULL` já garante a invariante que importa (nunca sobrescrever um valor real com um valor sugerido desatualizado) com uma única cláusula `WHERE`, sem reabrir a heurística de parsing no caminho de escrita.

**Chave textual para comparar `valor` ignorado:** o valor de uma sugestão nasce de `strconv.ParseFloat` (Go) e o valor gravado em `normalizacao_ignoradas` passa por uma coluna `NUMERIC(10,3)` (Postgres) — os dois **devem** representar o mesmo número quando a tupla é a mesma, mas comparar `float64 == float64` entre uma origem Go e uma origem Postgres-scan-pra-float64 é frágil o bastante para preferir uma chave textual `fmt.Sprintf("%s|%s|%.3f|%s", produtoID, campo, valor, unidade)` dos dois lados (mesma escala de casas decimais da coluna) em vez de comparar os floats diretamente.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros.
- `cd backend && go test ./services/... ./handlers/... -run 'Normalizacao|Correcao|Ignorada|Inconsistencia'` -- expected: PASS, cobrindo a I/O Matrix.
- `cd frontend && npx tsc --noEmit` -- expected: sem erros de tipo.
- `cd frontend && npx vitest run src/components/normalizacao/InconsistenciasSection.test.tsx` -- expected: PASS.

## Auto Run Result

Status: done

**Resumo da mudança implementada:** Story 6.2 já estava implementada e revisada (status `done`, duas passagens de review anteriores) quando esta execução iniciou. Esta foi uma passagem de review de follow-up (`followup_review_recommended: true` na entrada), sem re-implementação: `services.AplicarCorrecoes`/`services.IgnorarSugestao`/filtro de ignoradas em `AnalisarInconsistencias`, os handlers `AplicarCorrecoesHandler`/`IgnorarSugestaoHandler`, a migration `000023_create_normalizacao_ignoradas`, e a UI de seleção/ações em `InconsistenciasSection.tsx` já cobriam a Intent Contract integralmente antes desta passagem.

**Arquivos alterados nesta passagem:**
- `backend/handlers/normalizacao_test.go` — adicionado `TestAnalisarInconsistenciasHandler_500FalhaAoCarregarIgnoradas`, fechando a lacuna de verificação encontrada pelo verification-gap reviewer.
- `_bmad-output/implementation-artifacts/spec-6-2-aplicacao-seletiva-de-correcoes.md` (este arquivo) — status, Review Triage Log, `deferred` e `Auto Run Result`.

**Review findings breakdown desta passagem:** patch: 1 (low), defer: 2 (low, low), reject: 12. Nenhum `intent_gap` nem `bad_spec`.

**Follow-up review recommendation:** `false`. Score = 3×medium(0) + 1×low(1) = 1, abaixo do limiar 5; nenhum patch de severidade `high`.

**Verificação realizada:**
- `cd backend && go build ./... && go vet ./...` — sem erros.
- `cd backend && go test ./services/... ./handlers/... -run 'Normalizacao|Correcao|Ignorada|Inconsistencia'` — PASS (`ok stockflow/backend/services`, `ok stockflow/backend/handlers`), incluindo o teste novo.
- `cd frontend && npx tsc --noEmit` — sem erros de tipo.
- `cd frontend && npx vitest run src/components/normalizacao/InconsistenciasSection.test.tsx` — PASS (12/12).

**Riscos residuais:** os 4 itens em `deferred` (2 desta passagem + 2 das passagens anteriores) — nenhum bloqueia as ACs desta story; todos de severidade `low`.

