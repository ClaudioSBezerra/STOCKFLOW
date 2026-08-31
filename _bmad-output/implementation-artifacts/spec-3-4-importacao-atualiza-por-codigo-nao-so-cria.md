---
title: 'Story 3.4 — Importação atualiza por código, não só cria'
type: 'feature'
created: '2026-08-31'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '27c807f83ba92db768ecaec6c1266a3e428f7723'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** A importação (Story 3.3) sempre cria um Produto novo, mesmo quando o `código` da linha já existe no catálogo — reimportar uma planilha corrigida ou reprocessar um catálogo legado gera duplicatas em vez de atualizar (FR-11).

**Approach:** Em `processarProximaLinha` (services/importacoes.go), depois de validar a linha e resolver a categoria, procurar um Produto existente pelo `código` da linha (match exato, só quando não-vazio); se encontrado, faz `UPDATE` nele (revalidando o nome contra o template aplicado, se houver) e upsert de `produto_estoque`, marcando a linha `atualizado`; senão segue o fluxo de criação da Story 3.3 inalterado. Um novo índice único parcial em `produtos.codigo` (migration 000017, decisão já apontada pela migration 000011) torna esse match determinístico e passa a valer também para o cadastro manual.

## Boundaries & Constraints

**Always:**
- **Match só por `código`** (nunca por nome): `Código` da linha, trimado; vazio -> sempre cria (fluxo 3.3 inalterado, nunca busca). Não-vazio -> `SELECT id, template_id FROM produtos WHERE codigo = $1` dentro da MESMA transação de `processarProximaLinha` (depois de resolver `categoria_id`, antes de resolver o Estoque). Encontrado -> caminho de atualização abaixo; não encontrado -> caminho de criação da Story 3.3, sem mudança.
- **Migration 000017**: `CREATE UNIQUE INDEX idx_produtos_codigo ON produtos (codigo) WHERE codigo IS NOT NULL`. `down`: `DROP INDEX IF EXISTS idx_produtos_codigo`. Torna o match determinístico (no máximo um Produto por `código` não-nulo).
- **`services/produtos.go`, `CriarProduto`**: o branch de erro do `INSERT INTO produtos` (já mapeia `pqForeignKeyViolation`/`pqInvalidTextRepresentation` -> "categoria informada não existe") ganha `pqUniqueViolation` (`"23505"`, já declarada em `auth.go`) -> `ErroProdutoValidacao{"código já cadastrado"}` — cadastro manual respeita a mesma unicidade que a importação agora depende.
- **Caminho de atualização** (produto encontrado por código): se `template_id` do Produto encontrado for não-nulo, revalida `nome` da linha contra esse template (`nomeValidoParaTemplate`, services/nomenclatura.go — mesma função de `AtualizarNomeProduto`/Story 3.2); inválido -> linha rejeitada (mesmo tratamento de erro de validação, `rejeitarECommitar`) citando "nome não corresponde ao formato do template aplicado a este produto", Produto NÃO é alterado. Válido (ou sem template) -> `UPDATE produtos SET nome, codigo, categoria_id, observacoes, comprimento_valor/unidade, largura_valor/unidade, diametro_valor/unidade, altura_valor/unidade, espessura_valor/unidade WHERE id = <encontrado>` — sobrescreve TODOS os campos que a planilha carrega com os novos valores da linha; `template_id` NUNCA é tocado (o que já estava no Produto sobrevive). Depois, resolve o Estoque da linha (`encontrarOuCriarEstoque`, inalterado) e faz `INSERT INTO produto_estoque (produto_id, estoque_id, quantidade) VALUES (...) ON CONFLICT (produto_id, estoque_id) DO UPDATE SET quantidade = EXCLUDED.quantidade` — substitui (nunca soma) a quantidade só no par (Produto, Estoque da linha); outros pares do mesmo Produto com outros Estoques ficam intactos. `UPDATE importacao_linhas SET status = 'atualizado', produto_id = <encontrado> WHERE id = $1`, commit.
- **Migration 000018**: `ALTER TYPE importacao_linha_status ADD VALUE 'atualizado'` — sozinha no arquivo (não pode ser usada na mesma transação em que é criada; nenhuma outra instrução deste arquivo a referencia). `down`: no-op documentado (Postgres não suporta `DROP VALUE` de enum).
- **`RelatorioImportacao`** (services/importacoes.go) ganha `Atualizados int \`json:"atualizados"\`` — agregado em `montarRelatorio` junto a `criados`/`rejeitados` (novo `case "atualizado":` no mesmo `GROUP BY status` já existente). Linha atualizada NUNCA conta em `criados`, mesmo quando nenhum valor observável mudou (o `UPDATE` roda de qualquer forma — idempotente, sem diff prévio).
- **Frontend `ImportacaoProdutosSection.tsx`**: interface `RelatorioImportacao` ganha `atualizados: number`; o parágrafo do relatório e os dois `toast.success` (envio e continuar) passam a citar `criados/atualizados/rejeitados`. Um CTA "Verificar duplicatas agora" (`<Button asChild variant="outline"><Link to="/normalizacao">...`) aparece ao lado do relatório sempre que `relatorio` está definido — `/normalizacao` já é rota do nav (`nav-items.ts`), hoje caindo em `PlaceholderPage` (Epic 6 ainda não existe).

**Block If:** se a migration 000017 falhar por já existirem dois ou mais Produtos com o mesmo `código` não-nulo no banco de destino, HALT com status `blocked` — resolver os duplicados é decisão humana, fora do alcance desta story.

**Never:**
- Nenhuma correspondência por `nome` — linha sem código sempre cria Produto novo, mesmo com nome idêntico a um existente (Duplicatas é Epic 6, FR-19).
- Nenhuma soma/incremento de `quantidade` no update — sempre substitui pelo valor da linha; nenhuma `MOVIMENTACOES` é gerada (mesmo precedente de cadastro/3.3, AD-10 reserva isso a baixa/transferência/aprovação).
- Nenhuma tela ou lógica de Normalização/Duplicatas (Epic 6) — o CTA só navega para `/normalizacao`, sem "análise já disparada".
- Nenhuma rota nova em `handlers/importacoes.go`/`main.go` — os 3 endpoints da Story 3.3 continuam os mesmos, só o payload do relatório ganha `atualizados`.
- Nenhuma alteração em `AtualizarNomeProduto` — o import não a chama, escreve via `UPDATE` próprio dentro de `processarProximaLinha`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Linha com código já existente, campos diferentes | código bate com Produto existente | Produto atualizado com os novos valores; linha `atualizado`, não conta em `criados` | — |
| Reimportação idêntica | mesmo código, mesmos valores de todos os campos | `UPDATE` roda mesmo assim; linha `atualizado`, nunca `criado` | — |
| Produto encontrado tem `template_id` aplicado, nome da linha não bate com o template | código existente, nome fora do formato | linha `rejeitada`, Produto não é alterado | `linhas_rejeitadas` |
| Linha sem código, nome parecido com Produto existente | `Código` vazio | novo Produto sempre criado | — |
| Duas linhas da mesma planilha com o mesmo código novo (inexistente antes da importação) | linha N cria; linha M (M>N) mesmo código | linha N cria; linha M atualiza o Produto que a linha N acabou de criar (não duplica) | — |
| Linha com código existente referenciando Estoque diferente dos que o Produto já tem | código existente, Estoque novo para esse Produto | novo par `produto_estoque` criado com a quantidade da linha; pares existentes com outros Estoques intactos | — |

</intent-contract>

## Code Map

- `backend/migrations/000017_add_unique_index_produtos_codigo.{up,down}.sql` (novos) — índice único parcial em `produtos.codigo`.
- `backend/migrations/000018_add_atualizado_to_importacao_linha_status.{up,down}.sql` (novos) — novo valor de enum, sozinho no arquivo.
- `backend/services/importacoes.go` (editar) — `RelatorioImportacao.Atualizados`; `montarRelatorio` (`case "atualizado"`); `processarProximaLinha` reestruturado: lookup por código dentro da transação existente, ramo de atualização (revalidação de template via `nomeValidoParaTemplate`, `UPDATE produtos`, upsert `produto_estoque`), ramo de criação inalterado quando código vazio ou não encontrado.
- `backend/services/importacoes_test.go` (editar) — testes dos cenários da I/O Matrix (atualização, template inválido, reimportação idêntica, duas linhas mesmo código novo, novo par produto_estoque).
- `backend/services/produtos.go` (editar) — `CriarProduto`: branch `pqUniqueViolation` no INSERT de `produtos`.
- `backend/services/produtos_test.go` (editar) — teste de `CriarProduto` com `código` já existente -> `ErroProdutoValidacao`.
- `frontend/src/components/produtos/ImportacaoProdutosSection.tsx` (editar) — `atualizados` no relatório/toasts, CTA `Link` para `/normalizacao`.
- `frontend/src/components/produtos/ImportacaoProdutosSection.test.tsx` (editar) — cobre `atualizados` exibido e presença do CTA.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000017_*.{up,down}.sql` -- índice único parcial em `produtos.codigo` -- torna o match por código determinístico.
- `backend/migrations/000018_*.{up,down}.sql` -- novo valor `atualizado` no enum `importacao_linha_status`.
- `backend/services/importacoes.go` (+ teste) -- match por código, ramo de atualização com revalidação de template, `Atualizados` no relatório.
- `backend/services/produtos.go` (+ teste) -- `CriarProduto` mapeia violação de unicidade de código.
- `frontend/src/components/produtos/ImportacaoProdutosSection.tsx` (+ teste) -- exibe `atualizados`, CTA para `/normalizacao`.

**Acceptance Criteria:**
- Given uma planilha com uma linha cujo código já existe no catálogo, when a importação processa essa linha, then o Produto existente é atualizado com os novos valores em vez de criar um Produto duplicado.
- Given uma planilha reimportada sem nenhuma mudança, when a importação processa, then nenhum Produto novo é criado e a linha nunca aparece como "criado" no relatório.
- Given o relatório final de importação, when ele é exibido, then discrimina quantas linhas foram criadas, atualizadas e rejeitadas, com um CTA "Verificar duplicatas agora" apontando para `/normalizacao`.
- Given uma linha sem código cujo nome é parecido com um Produto existente, when a importação processa, then um novo Produto é criado.
- Given um Produto encontrado por código que tem um template de nomenclatura aplicado, when a linha traz um nome fora do formato desse template, then a linha é rejeitada e o Produto não é alterado.

## Spec Change Log

## Review Triage Log

### 2026-08-31 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 2 (high 1, medium 1, low 0)
- defer: 0
- reject: 12 (medium 2, low 10)
- addressed_findings:
  - `[high]` `[patch]` `processarLinhaDeAtualizacao` (services/importacoes.go) rodava `UPDATE produtos` ANTES de resolver o Estoque da linha — se `encontrarOuCriarEstoque` devolvesse `ErrEstoqueValidacao`, `rejeitarECommitar` commitava a transação com o `UPDATE produtos` já aplicado, deixando o Produto alterado mesmo com a linha reportada como `rejeitada`. Achado independentemente pelo Blind Hunter. Corrigido invertendo a ordem: resolve o Estoque primeiro, só então roda o `UPDATE produtos` — mesmo padrão do caminho de criação (`encontrarOuCriarEstoque` sempre antes do `INSERT INTO produtos`).
  - `[medium]` `[patch]` `INSERT INTO produtos` do caminho de criação (services/importacoes.go) não tratava violação do novo índice `idx_produtos_codigo` (SQLSTATE 23505) — alcançável quando duas linhas com o MESMO código novo são processadas por transações concorrentes (duas chamadas simultâneas a `POST /api/importacoes/{id}/continuar` na mesma importação, cada uma reivindicando uma linha diferente via `FOR UPDATE SKIP LOCKED`), aí a segunda `INSERT` perde a corrida e vira erro de infraestrutura genérico, interrompendo o laço inteiro. Achado independentemente pelo Edge Case Hunter e pelo Verification Gap Reviewer. Corrigido mapeando `pqUniqueViolation` no `INSERT INTO produtos` para nova busca do produto (mesmo comportamento do caminho de atualização) em vez de abortar o laço.
  - `[reject]` migration 000017 falhar se já existirem códigos duplicados no banco de destino — já é o comportamento coberto pelo "Block If" do spec (HALT explícito), não um caso não tratado.
  - `[reject]` match de `código` case-sensitive — decisão de spec deliberada (Design Notes), consistente com o precedente de não normalizar identificadores no restante do código.
  - `[reject]` `quantidade` substituída, nunca somada, no update — decisão de spec deliberada (Design Notes), confirmada pelo Intent Alignment Auditor como a leitura que o spec escolhe.
  - `[reject]` CTA "Verificar duplicatas agora" aparece mesmo com `atualizados=0` — Boundaries do spec é explícito ("sempre que `relatorio` está definido"), confirmado pelo Intent Alignment Auditor.
  - `[reject]` alinhamento de campos de `RelatorioImportacao` — `gofmt -l .` já roda limpo na verificação desta story, achado infundado.
  - `[reject]` ausência de checagem de `RowsAffected` no `UPDATE produtos`/`UPDATE importacao_linhas` — nenhuma capacidade de excluir Produto existe em nenhuma story implementada, caminho não alcançável hoje (mesmo raciocínio já aplicado a um achado análogo na Review Triage Log da spec-3-3).
  - `[reject]` busca de produto por código sem `FOR UPDATE` (TOCTOU) — o `UPDATE` subsequente é uma sobrescrita completa (não um read-modify-write sobre o valor lido), então uma corrida com `AtualizarNomeProduto` não corrompe dado nenhum.
  - `[reject]` colisão de código novo dentro do mesmo arquivo é silenciosa — comportamento deliberado do I/O Matrix (linha 5, "Duas linhas... mesmo código novo"), não um caso não tratado.
  - `[reject]` normalização de código vazio "não verificada" — coberta por `TestCriarImportacao_LinhaSemCodigo_NomeParecidoAindaAssimCria`, achado incorreto.
  - `[reject]` falta teste explícito do CTA com `atualizados=0` e relatório presente — nitpick de cobertura de teste sobre uma condição de renderização sem lógica de branch de risco, fora do I/O Matrix do spec.
  - `[reject]` ausência de `MOVIMENTACOES` não testada diretamente — satisfeita por omissão de código (nenhum caminho novo toca a tabela), mesmo padrão de proporcionalidade já aplicado na Story 3.3.

### 2026-08-31 — Review pass (follow-up)
- intent_gap: 0
- bad_spec: 0
- patch: 0
- defer: 0
- reject: 19 (low 19)
- addressed_findings:
  - none
- notas:
  - `encontrarOuCriarEstoque` chamado duas vezes no caminho de recuperação por SAVEPOINT (Blind Hunter) — real, mas sem consequência observável (idempotente, só round-trip extra numa corrida já rara); custo de tocar código transacional testado não justifica o ganho.
  - Revalidação de template duplicada em vez de reusar `AtualizarNomeProduto` (Blind Hunter) — decisão deliberada do próprio spec ("Never": "Nenhuma alteração em AtualizarNomeProduto — o import não a chama, escreve via UPDATE próprio").
  - Alinhamento de campos de `RelatorioImportacao` (Blind Hunter) — verificado: `gofmt -l .` roda limpo, achado infundado (mesma conclusão da passada anterior).
  - Match de código case/whitespace-sensitive, CTA sempre visível, CTA para rota placeholder, quantidade substituída (nunca somada) no reimport, migration 000017 falhar com duplicatas pré-existentes (Blind Hunter, 5 achados) — decisões deliberadas já registradas nesta spec (Design Notes / Boundaries) e já triadas como reject na passada anterior.
  - Comentários referenciando número de linha hard-coded (Blind Hunter) — nitpick cosmético, sem risco funcional.
  - Falta teste "corrida perdida + template inválido" (Blind Hunter) — cenário inalcançável: um Produto criado pela importação nunca recebe `template_id` (o INSERT do caminho de criação não o define), então o produto que uma transação perdedora encontra na re-busca após o SAVEPOINT nunca tem template aplicado.
  - Falta teste HTTP-handler-level para `atualizados` no JSON / falta teste de frontend para o banner de retomada ignorar `atualizados` (Blind Hunter, 2 achados) — nitpicks de cobertura sobre serialização padrão do Go (`json:"atualizados"`, já coberta a nível de serviço) e sobre um comentário só-documentação sem lógica nova, respectivamente.
  - `pqUniqueViolation` no `INSERT INTO produtos`/`CriarProduto` tratado sem checar o nome da constraint (Edge Case Hunter, 2 achados) — verificado contra as migrations: `produtos` tem exatamente um índice único (`idx_produtos_codigo`, migration 000017), então uma violação 23505 nesse INSERT só pode vir dali; premissa do achado é falsa.
  - Falta checagem de `RowsAffected` no `UPDATE produtos` (Edge Case Hunter) — duplicata do achado já rejeitado na passada anterior (nenhuma capacidade de excluir Produto existe ainda).
  - Ordem "resolve Estoque antes do UPDATE produtos" e SAVEPOINT no caminho de criação divergindo da prosa literal do spec (Intent Alignment Auditor, 2 pontos) — são exatamente os Bugs 1 e 2 já corrigidos e documentados na passada de review anterior (commit ead94fa); não são achados novos.
  - CTA "ao lado" do relatório vs. renderizado empilhado abaixo dele (Intent Alignment Auditor) — leitura de prosa ambígua ("ao lado" no sentido de "junto/acompanhando", não necessariamente lado a lado horizontalmente); o AC correspondente só exige presença e destino do link, não posicionamento.

## Design Notes

- **Índice único em `produtos.codigo`, não só lógica de aplicação**: a migration 000011 (Story 3.1) já deixou o comentário "Story 3.4 decide o comportamento de 'atualiza por código'" — sem unicidade no banco, dois Produtos cadastrados manualmente com o mesmo código deixariam o match da importação ambíguo (qual dos dois atualizar?). O índice parcial (`WHERE codigo IS NOT NULL`) resolve isso na origem e estende a mesma regra ao cadastro manual (`CriarProduto`), não só à importação.
- **Substituir, nunca somar, a quantidade no update**: a planilha reimportada é tratada como o estado atual completo daquela linha — mesma semântica de "sobrescreve com os novos valores" já aplicada a nome/dimensões/categoria/observações. Somar exigiria decidir o que fazer quando o Produto já sofreu baixas/transferências desde a última importação, fora do escopo de FR-11 e sem nenhuma AC cobrindo esse caso.
- **Revalidação de template no update**: a Story 3.2 já estabeleceu a regra "editar um Produto com template aplicado revalida o nome contra esse template — não dá para burlar editando depois". O `UPDATE` desta story É uma edição de `nome`; ignorá-la abriria exatamente a brecha que a Story 3.2 fechou, só que via reimportação em vez do endpoint de edição.
- **CTA sem "análise já disparada"**: UX-DR20 (epics.md) descreve o CTA levando à Normalização "com a análise já em andamento", mas essa tela (Epic 6) não existe ainda — implementá-la aqui seria escopo de outro épico inteiro. O CTA desta story é só o link para `/normalizacao` (rota já cadastrada no nav, hoje `PlaceholderPage`); dar o "salto direto com análise em andamento" fica para quando a Story 6.3 existir.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` — Postgres real; migrations 000017-000018 aplicam sem erro; cobre os novos testes de `importacoes_test.go`/`produtos_test.go`.
- `cd frontend && npm run lint && npm run build && npm run test` — `oxlint`, `tsc`+`vite` e os testes atualizados de `ImportacaoProdutosSection` passam.
- `docker compose up --build` — logado como `almoxarife`, importar a mesma planilha de exemplo duas vezes; a segunda vez mostra `atualizados` no relatório e nenhum Produto novo (`SELECT count(*) FROM produtos` inalterado entre as duas importações).

**Manual checks (if no CLI):**
- `SELECT codigo, count(*) FROM produtos WHERE codigo IS NOT NULL GROUP BY codigo HAVING count(*) > 1` retorna vazio após a migration 000017.
- `SELECT quantidade FROM produto_estoque WHERE produto_id = '<id>' AND estoque_id = '<id>'` reflete o valor da linha mais recentemente reimportada para aquele par, não a soma.

## Auto Run Result

**Resumo da mudança implementada:** a importação (Story 3.3) passa a atualizar um Produto existente em vez de sempre criar um novo, quando o `código` da linha bate com um Produto já cadastrado (match exato, só quando não-vazio). Um índice único parcial em `produtos.codigo` torna esse match determinístico e estende a mesma unicidade ao cadastro manual. O caminho de atualização revalida o nome contra o template aplicado (se houver), sobrescreve os campos que a planilha carrega, faz upsert de `produto_estoque` (substitui, nunca soma, a quantidade) e marca a linha `atualizado`. O relatório da importação (backend e frontend) passa a discriminar `atualizados`, com um CTA "Verificar duplicatas agora" apontando para `/normalizacao`.

**Arquivos alterados:**
- `backend/migrations/000017_add_unique_index_produtos_codigo.{up,down}.sql` — índice único parcial em `produtos.codigo`.
- `backend/migrations/000018_add_atualizado_to_importacao_linha_status.{up,down}.sql` — novo valor `atualizado` no enum `importacao_linha_status`.
- `backend/services/importacoes.go` — match por código, `processarLinhaDeAtualizacao` (revalidação de template, UPDATE, upsert de estoque), `RelatorioImportacao.Atualizados`, recuperação via SAVEPOINT de corrida de código novo no INSERT.
- `backend/services/importacoes_test.go` — testes dos cenários da I/O Matrix e das duas correções da passada de review anterior.
- `backend/services/produtos.go` — `CriarProduto` mapeia violação de unicidade de código para `ErroProdutoValidacao`.
- `backend/services/produtos_test.go` — teste de `CriarProduto` com código já cadastrado.
- `backend/handlers/importacoes.go` — comentário de doc do payload do relatório atualizado (`atualizados`).
- `frontend/src/components/produtos/ImportacaoProdutosSection.tsx` — exibe `atualizados`, CTA para `/normalizacao`.
- `frontend/src/components/produtos/ImportacaoProdutosSection.test.tsx` — cobre `atualizados` exibido e presença do CTA.

**Achados de review (passada de follow-up):** 4 reviewers em paralelo (Blind Hunter, Edge Case Hunter, Verification Gap Reviewer, Intent Alignment Auditor) sobre o diff completo desde `baseline_revision`. 0 patches aplicados, 0 itens adiados, 19 achados rejeitados (todos severidade baixa) — decisões deliberadas de spec já documentadas (match case-sensitive, quantidade substituída não somada, CTA sempre visível, CTA para rota placeholder, `down` de enum como no-op), achados já corrigidos e documentados na passada de review anterior (ordem Estoque-antes-de-UPDATE, recuperação por SAVEPOINT), premissas verificadas como falsas contra o schema real (violação de unicidade "não relacionada" — `produtos` só tem um índice único), e nitpicks de cobertura de teste desproporcionais ao risco. Verification Gap Reviewer não encontrou nenhuma lacuna. Detalhe completo em `## Review Triage Log`.

**Follow-up review recommendation:** `false` — 0 achados triados como `patch` nesta passada (score 0).

**Verificação realizada:** nenhuma alteração de código nesta passada (todos os achados foram rejeitados), portanto os comandos de `## Verification` já executados durante a implementação (commit `ead94fa`) permanecem válidos. Reconfirmado nesta passada: `cd backend && gofmt -l .` sem saída; `go build ./...` e `go vet ./...` limpos.

**Riscos residuais:** nenhum identificado por esta passada de review. A dependência de que `produtos` nunca ganhe um segundo índice único sem revisar o tratamento de `pqUniqueViolation` em `importacoes.go`/`produtos.go` é implícita (documentada nesta nota de review, não no código) — se uma story futura adicionar outra constraint única em `produtos`, o mapeamento de `pqUniqueViolation` para "corrida de código"/"código já cadastrado" precisará ser revisitado.

