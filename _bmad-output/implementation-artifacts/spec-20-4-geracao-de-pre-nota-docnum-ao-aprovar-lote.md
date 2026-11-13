---
title: 'Geração de Pré-Nota (docnum) ao aprovar lote'
type: 'feature'
created: '2026-11-13'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'e72aabc109d5e73d04ad68940215108792871f03'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-20-context.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Um lote aprovado (Story 20.3) debita/credita saldo de verdade mas não deixa nenhum registro rastreável como UM documento só — o Gestor não tem como referenciar "aquele conjunto de movimentações" (igual ao MIRO do SAP).

**Approach:** Ao final de `DecidirLotePedidos`, DENTRO da MESMA transação, se `aprovar=true` e ao menos 1 Pedido foi decidido (aprovado/parcialmente_aprovado) DE FATO por esta chamada (nunca um "pulado"), gera UMA `pre_nota` com `docnum` sequencial por Empresa (mesmo padrão atômico de `proximoCodigoProduto`) e vincula `pedidos.pre_nota_id` só nesses Pedidos. PDF/documento impresso é Story 20.5, fora de escopo aqui — esta story só grava o registro.

## Boundaries & Constraints

**Always:**
- `docnum` avança via `UPDATE contadores_pre_nota ... RETURNING` (mesmo molde exato de `proximoCodigoProduto`, pedidos.go/produtos.go:323) DENTRO da transação do lote — nunca um `SELECT MAX()+1` solto, nunca fora da transação.
- `pre_notas`/`contadores_pre_nota` são isolados por Empresa (AD-20/AD-26): cada Empresa numera a partir do próprio maior `docnum`, nunca mistura contagem.
- `contadores_pre_nota` nasce junto com TODA Empresa (nova via `ProvisionarEmpresa`, e as já existentes via backfill na migration) — nunca lazy-init.
- A Pré-Nota só vincula Pedidos decididos de fato POR ESTA chamada de `DecidirLotePedidos` (aprovados ou parcialmente aprovados) — NUNCA um Pedido "pulado" (já decidido por outra requisição/transação antes, Story 20.3): esse já tem sua própria Pré-Nota de quando foi decidido de verdade.
- Rejeição (`aprovar=false`) nunca gera Pré-Nota, mesmo que o lote tenha vários Pedidos.
- Se TODOS os Pedidos do lote vierem "pulados" (zero decididos agora), nenhuma Pré-Nota nova é gerada — nada de novo a documentar, `docnum` não avança.
- `pedidos.pre_nota_id` é nullable (Pedidos de retirada, pendentes, rejeitados, ou decididos fora de lote nunca têm Pré-Nota).

**Ask First:** nenhuma — a leitura "só pedidos decididos nesta chamada, zero Pré-Nota se zero decididos agora" foi validada com o usuário antes desta spec ser escrita.

**Never:** gerar PDF/documento impresso (Story 20.5, fora de escopo), tocar `DecidirPedido`/retirada (nunca gera Pré-Nota, continua fora desse conceito inteiro), mexer no formato/campos do recibo de Pedido individual já existente (`GerarReciboPedidoPDF`).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Lote aprovado, todos decididos agora | 3 Pedidos pendentes, aprovar=true | 1 Pré-Nota nova, docnum sequencial, `pre_nota_id` nos 3 | N/A |
| Lote aprovado, misto decidido+pulado | 2 decididos agora + 1 já pulado (outra transação) | 1 Pré-Nota só com os 2 decididos agora; o pulado mantém sua Pré-Nota antiga | N/A |
| Lote 100% pulado | Todos os ids já estavam decididos | Nenhuma Pré-Nota nova, `docnum` não avança | N/A |
| Lote rejeitado | aprovar=false | Nenhuma Pré-Nota gerada | N/A |
| Dois lotes em sequência, mesma Empresa | 2 aprovações consecutivas | `docnum` estritamente crescente, sem gap nem repetição | N/A |
| Duas Empresas aprovando ao mesmo tempo | Lotes concorrentes, Empresas diferentes | Cada Empresa numera a partir do próprio maior `docnum`, nunca mistura | N/A |

</frozen-after-approval>

## Code Map

- `backend/services/produtos.go:323` (`proximoCodigoProduto`) -- molde EXATO do contador atômico a copiar para `proximoDocnumPreNota(tx *sql.Tx, empresaID string) (int, error)`: `UPDATE contadores_pre_nota SET ultimo_numero = ultimo_numero + 1 WHERE empresa_id = $1 RETURNING ultimo_numero` (mais simples que o de Produto — não precisa de `GREATEST` com código legado, `docnum` não tem equivalente de planilha importada).
- `backend/services/empresas.go:392-396` (`ProvisionarEmpresa`, insert de `contadores_produto`) -- molde EXATO pra inserir também `contadores_pre_nota (empresa_id, ultimo_numero) VALUES ($1, 0) ON CONFLICT (empresa_id) DO NOTHING`, no mesmo lugar/função.
- `backend/migrations/000037_create_contadores_produto.up.sql` -- molde EXATO pra nova migration `000059`: `CREATE TABLE contadores_pre_nota (empresa_id UUID PRIMARY KEY REFERENCES empresas(id), ultimo_numero INTEGER NOT NULL DEFAULT 0)` + backfill `INSERT INTO contadores_pre_nota SELECT e.id, 0 FROM empresas e` + nova tabela `pre_notas (id UUID PK, empresa_id UUID NOT NULL REFERENCES empresas(id), docnum INTEGER NOT NULL, decidido_por UUID NOT NULL, criado_em TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE (empresa_id, docnum))` + `ALTER TABLE pedidos ADD COLUMN pre_nota_id UUID NULL REFERENCES pre_notas(id)`.
- `backend/services/pedidos.go:1401-1534` (`DecidirLotePedidos`) -- depois do loop que decide todos os Pedidos do lote (linha ~1522, `for _, id := range idsUnicos`) e ANTES do `tx.Commit()` (linha ~1530): se `aprovar` e ao menos 1 `det` em `porID` teve status `aprovado`/`parcialmente_aprovado` decidido NESTA chamada (não via `lerPedidoDetalheTx`/pulado — usar a flag `pulado` que `decidirUmPedidoDoLoteTx` já devolve, que o chamador já tem em mãos no loop), gera a Pré-Nota (`proximoDocnumPreNota` + `INSERT INTO pre_notas` + `UPDATE pedidos SET pre_nota_id = $1 WHERE id = ANY($2)` só nos ids decididos agora) dentro da MESMA transação.
- `backend/services/pedidos.go` -- `PedidoDetalhe`/`PedidoResumo` ganham `PreNotaID *string`/`Docnum *int` (via JOIN com `pre_notas` nos SELECTs existentes de `ListarPedidosProprios`/`ListarPedidosFila`/`BuscarPedidoProprio`) -- exposição mínima pra Story 20.5 ter o que referenciar, sem implementar nada do PDF aqui.

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/000059_*.up/down.sql` -- `contadores_pre_nota` + `pre_notas` + `pedidos.pre_nota_id`, com backfill do contador pra Empresas existentes
- [x] `backend/services/empresas.go` -- `ProvisionarEmpresa` cria a linha de `contadores_pre_nota` junto com a Empresa, mesmo molde de `contadores_produto`
- [x] `backend/services/pedidos.go` -- `proximoDocnumPreNota` + geração da Pré-Nota dentro de `DecidirLotePedidos`, só com Pedidos decididos nesta chamada
- [x] `backend/services/pedidos_test.go` -- testes cobrindo as 6 linhas da I/O Matrix + concorrência (2 lotes da mesma Empresa em paralelo -> docnum sem colisão) + 2 Empresas em paralelo -> contagem independente
- [x] `backend/services/pedidos.go` -- expor `PreNotaID`/`Docnum` em `PedidoDetalhe`/`PedidoResumo` (JOIN em `ListarPedidosProprios`/`ListarPedidosFila`/`BuscarPedidoProprio`), com teste cobrindo o JSON

**Acceptance Criteria:**
- Given um lote de Pedidos aprovado, when a aprovação é commitada, then o sistema grava uma Pré-Nota com `docnum` sequencial único por Empresa, vinculada a todos os Pedidos decididos naquele lote
- Given dois lotes aprovados em sequência pela mesma Empresa, when os dois commitam, then os `docnum` são estritamente crescentes, sem gap nem repetição
- Given duas Empresas distintas aprovando lotes ao mesmo tempo, when ambos commitam, then cada Empresa numera a partir do próprio maior `docnum`
- Given um lote rejeitado, when a decisão é commitada, then nenhuma Pré-Nota é gerada
- Given `go build ./...`, `go test -p 1 ./...`, `npm run build` e `npx vitest run`, when rodados após a story, then terminam sem erro

## Spec Change Log

- **2026-11-13, revisão (patches, sem loopback):** revisão em triplo (blind-hunter, edge-case-hunter, verification-gap) — os 3 convergiram no mesmo achado principal: `DecidirLotePedidos` montava a resposta ANTES de gerar a Pré-Nota, então a resposta da própria chamada que cria a Pré-Nota devolvia `preNotaId`/`docnum` nulos. Corrigido (resposta preenchida depois do INSERT/UPDATE), com teste no serviço e teste HTTP lendo as chaves camelCase do JSON cru. Demais patches: FK `pre_notas.decidido_por -> usuarios(id)` (mesmo padrão de `pedidos.decidido_por`), índice em `pedidos.pre_nota_id` (Story 20.5), CHECKs `ultimo_numero >= 0`/`docnum > 0`, limpeza de `pre_notas` em `removerEmpresaDeTeste`, comentários de doc reposicionados, teste de lote misto `aprovado`+`parcialmente_aprovado` caindo na mesma Pré-Nota, checagem explícita de status antes de vincular, e o teste `IdInexistenteOuDeOutraEmpresa` (Story 20.3) passou a limpar a Empresa extra que cria (quebrava em toda segunda execução no mesmo banco). Lock no `contadores_pre_nota` analisado e descartado como risco de deadlock: é um recurso único por Empresa, sempre adquirido por último na transação — só serializa os lotes da mesma Empresa, que é exatamente o comportamento desejado para `docnum` sem buraco. 3 itens de menor risco foram para `deferred-work.md`.

## Verification

**Commands:**
- `cd backend && go build ./... && go test ./services/... -run 'PreNota|Lote'` -- expected: build limpo, testes novos passando
- `cd backend && go test -p 1 ./...` -- expected: suíte completa do backend limpa
- `cd frontend && npm run build && npx vitest run` -- expected: build limpo, suíte completa do frontend limpa (sem mudança de UI nesta story — só os campos novos `preNotaId`/`docnum` passam a existir no JSON de Pedido, sem uso ainda)

## Suggested Review Order

**Geração da Pré-Nota (o ponto central)**

- Ponto de entrada: só os Pedidos decididos nesta chamada viram Pré-Nota, dentro da mesma transação do lote
  [`pedidos.go:1631`](../../backend/services/pedidos.go#L1631)

- Contador atômico do `docnum`, molde de `proximoCodigoProduto`
  [`pedidos.go:1410`](../../backend/services/pedidos.go#L1410)

- Função do lote, agora devolvendo `preNotaId`/`docnum` já na resposta
  [`pedidos.go:1480`](../../backend/services/pedidos.go#L1480)

**Esquema**

- Tabelas novas, backfill do contador pras Empresas existentes, FK, CHECKs e índice
  [`000059_create_pre_notas.up.sql:1`](../../backend/migrations/000059_create_pre_notas.up.sql#L1)

- Toda Empresa nova já nasce com seu contador de Pré-Nota
  [`empresas.go:401`](../../backend/services/empresas.go#L401)

**Leitura (exposição pra Story 20.5)**

- `preNotaId`/`docnum` nas listagens e no detalhe do Pedido via LEFT JOIN
  [`pedidos.go:832`](../../backend/services/pedidos.go#L832)
