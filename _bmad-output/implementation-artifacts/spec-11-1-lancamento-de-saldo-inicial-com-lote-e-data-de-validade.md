---
title: 'Story 11.1: Lançamento de saldo inicial com Lote e Data de Validade'
type: 'feature'
created: '2026-09-20'
baseline_revision: 'd81e0226f112cc4e87dd59eae530d693de016470'
status: done
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: [oversized]
deferred:
  - summary: >-
      `vencido` usa `CURRENT_DATE` do banco (fuso da sessão Postgres), não o fuso da Empresa.
    evidence: |-
      Com o banco em UTC, um Lote que vence "hoje" aparece como vencido a partir de ~21h no Brasil. O intent-contract fixa `CURRENT_DATE`; revisar quando houver fuso por Empresa.
    location: >-
      backend/services/lotes.go, backend/services/catalogo.go (preencherLotesDetalhe)
    severity: low
---

<intent-contract>

## Intent

**Problem:** O saldo de um Produto num Estoque é hoje uma quantidade única em `produto_estoque`, sem Lote nem validade; o Almoxarife não tem como dar entrada de saldo com rastreabilidade de recebimento (FR-47).

**Approach:** Nova tabela `lotes` (AD-24) + endpoint `POST /api/lotes` (`almoxarife`+) que cria SEMPRE um Lote novo e registra a Movimentação `entrada` na mesma transação (AD-10); as leituras de saldo do Catálogo/detalhe passam a somar `produto_estoque` (saldo legado, até a 11.2) + Lotes via view; detalhe lista os Lotes e sinaliza vencido; tela "Lançar saldo" na página `/estoques`.

## Boundaries & Constraints

**Always:**
- `POST /api/lotes` só `almoxarife`+: `RequireAuth` + `RequireRole(services.PapelAlmoxarife)`; `usuario` -> 403 decidido pelo middleware. Corpo `{produtoId, estoqueId, quantidade, dataValidade?}`; `dataValidade` opcional (`null`/ausente/`""` = desconhecida), formato estrito `YYYY-MM-DD`; data no passado É aceita (Lote nasce vencido, só sinalizado).
- Validação SEM tocar o banco -> 400: `quantidade` > 0 e ≤ `limiteNumeric103` (mesma checagem de `RegistrarBaixa`), `dataValidade` parseável. `produtoId`/`estoqueId` de outra Empresa, inexistente, soft-deleted (`deleted_at`), malformado (`pq` 22P02) colapsam em 404 (nunca 403).
- Numa transação: `INSERT INTO lotes ... SELECT` filtrando `produtos`/`estoques` por `empresa_id` (a FK de `estoques` faz o KEY SHARE que serializa com `ExcluirEstoque`), depois `INSERT INTO movimentacoes (tipo='entrada', estoque_origem_id NULL, estoque_destino_id, lote_id, empresa_id)`. Nunca escreve/atualiza um Lote existente nem `produto_estoque`; cada chamada = 1 linha nova em `lotes` (mesmo par produto/estoque, mesma validade, ainda assim Lote novo).
- `lotes`: `id` UUID, `produto_id`/`estoque_id` FK (sem CASCADE), `quantidade NUMERIC(10,3) CHECK (>= 0)`, `data_validade DATE NULL`, `empresa_id NOT NULL`, `criado_em timestamptz`. Lote zerado nunca é apagado.
- Saldo lido = `produto_estoque.quantidade` (saldo legado, ainda não migrado — Story 11.2) + soma dos `lotes` do par, via view `saldo_produto_estoque(produto_id, estoque_id, quantidade)` agregada por par; TODAS as leituras de saldo de `catalogo.go` (grade, contagem/filtros `ComEstoque`/`EstoqueID`, agrupado, `porEstoque`, detalhe) e da exportação passam a usá-la, sem mudar o JSON existente.
- Detalhe (`GET /api/produtos/{id}`): cada item de `porEstoque` ganha `lotes: [{id|null, quantidade, dataValidade|null, vencido, legado}]` (Lotes reais + 1 entrada `legado:true` com o `produto_estoque.quantidade` > 0 do par, validade desconhecida). `vencido` = `data_validade < CURRENT_DATE` (o dia da validade ainda não é vencido). Vencido nunca oculta nem bloqueia saldo.
- Tabelas agrupadas/grade NÃO ganham `lotes` (só o detalhe).
- Exclusão de Estoque (AD-31): `ExcluirEstoque` também é barrada (`*ErroEstoqueComResiduo`, nomes dos Produtos) se existir QUALQUER linha em `lotes` no Estoque, mesmo zerada.
- Mesclagem de duplicatas (AD-11 estendida): `produto_id` dos `lotes` dos removidos é reescrito para o sobrevivente na mesma transação, antes do soft-delete.
- Sucesso publica `movimentacoes` (`created`, id da Movimentação) e `produtos` (`updated`, id do Produto) no `registro` SSE.
- Frontend: sem `window.confirm`; erros inline `role="alert"`; gate de papel espelhado (`rankPapel >= almoxarife`); badge de vencido em âmbar (nunca a cor destrutiva); "validade desconhecida" para `dataValidade` nula; rótulo `entrada: 'Entrada'` no Histórico.

**Block If:** Nenhuma decisão bloqueante identificada.

**Never:** Não migrar `produto_estoque` nem descontinuá-la (Story 11.2); não alterar Baixa/Transferência/Pedidos/Carrinho/importação/cadastro de Produto (Stories 11.3–11.6, AD-30); não criar reserva; não expor escolha manual de Lote; sem alerta de vencimento por e-mail; não tocar `sprint-status.yaml`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Lançar com validade | `almoxarife`, `{produtoId, estoqueId, quantidade:10, dataValidade:"2027-03-01"}` | 201 `{"lote":{id,produtoId,estoqueId,quantidade,dataValidade,vencido:false}}`; +1 linha em `lotes`; +1 Movimentação `entrada` | — |
| Sem validade | `dataValidade` ausente/`null`/`""` | 201, `dataValidade:null` | — |
| Segundo lançamento no par | par já tem Lote(s) | Lote adicional; total do Catálogo = soma de todos (+ legado) | — |
| Validade passada | `"2020-01-01"` | 201, `vencido:true`; detalhe sinaliza vencido, saldo continua contado | — |
| Quantidade inválida | `0`, negativa, > limite, ausente | 400 VALIDATION_ERROR, nada gravado | — |
| Data inválida | `"01/03/2027"`, `"2027-13-40"` | 400 | — |
| Alvo inválido | Produto/Estoque alheio, inexistente, mesclado, malformado | 404 | — |
| Papel `usuario` | POST | 403; 401 sem token | — |
| Excluir Estoque com Lote | Lote (qtd 0 ou >0) no Estoque | 409 (residual), Estoque permanece | — |
| Mesclagem | removido tem Lotes | Lotes passam ao sobrevivente | — |

</intent-contract>

## Code Map

- `backend/migrations/000040_*` -- última existente; nova `000041_create_lotes.{up,down}.sql` (tabela, índices `(produto_id,estoque_id)`/`(estoque_id)`/`(empresa_id)`, view `saldo_produto_estoque`, `movimentacoes.lote_id` + CHECK de `tipo` incluindo `'entrada'` — nome padrão `movimentacoes_tipo_check`, confirmar no banco; `.down` apaga Movimentações `entrada` antes de restaurar o CHECK).
- `backend/services/movimentacoes.go:215-300` -- `RegistrarBaixa`: MOLDE (validação prévia, `limiteNumeric103`, `pqInvalidTextRepresentation`, INSERT de `movimentacoes` com `empresa_id`). Novo `services/lotes.go` (`LancarSaldo`, `LoteLancado`, `ErroLoteValidacao`, `ErrLoteAlvoNaoEncontrado`).
- `backend/handlers/movimentacoes.go:50-89` -- MOLDE de handler (`empresaDaRequisicao`, `authRequestMaxBytes`, `registro.Publish`); novo `handlers/lotes.go`.
- `backend/main.go:~590` -- registrar `POST /e/{slug}/api/lotes` junto de baixa/transferência (RequireAuth + RequireRole(Almoxarife)); `main_test.go` (teste de rota/papel, molde `TestNewMux_CategoriasEscritaCarregaRequireRoleAdm`).
- `backend/services/catalogo.go:227,238,270,416,444,776` -- leituras de `produto_estoque` a trocar pela view; `produtoDetalheQuery` (~740) e `ObterProdutoDetalhe` ganham `lotes`. `EstoqueQuantidade` ganha `Lotes []LoteSaldo` (`omitempty`, só no detalhe). Conferir `services/relatorios.go` (exportação) e `estoques.go` (`selectResiduo`, ~160).
- `backend/services/estoques.go:~160` -- guard AD-31 em `ExcluirEstoque`; `services/normalizacao.go:~1214-1300` -- mesclagem: acrescentar `UPDATE lotes SET produto_id`.
- `frontend/src/pages/EstoquesPage.tsx` -- nova aba "Lançar saldo" (`LancamentoSaldoSection`, molde `LocaisEstoqueSection`; busca de Produto via `GET /api/produtos/busca?q=` como `BuscaCatalogo`, Estoques via `GET /api/estoques`); `frontend/src/pages/ProdutoDetalhePage.tsx:137,573` -- tipo `EstoqueQuantidade` + lista de Lotes; `components/estoques/MovimentacoesSection.tsx:56` -- rótulo `entrada`.
- Testes moldes: `services/movimentacoes_test.go`, `handlers/movimentacoes_test.go`, `services/catalogo_test.go`, `services/isolamento_test.go` (isolamento entre Empresas), `frontend/.../LocaisEstoqueSection.test.tsx`.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000041_create_lotes.{up,down}.sql` -- tabela, view, `lote_id`, CHECK `entrada` -- schema AD-24 aditivo
- `backend/services/lotes.go` (+ `lotes_test.go`) -- `LancarSaldo` transacional; cobrir toda a matriz (validação, alvo alheio/malformado/soft-deleted, Lote adicional, `entrada` gerada, nada em `produto_estoque`)
- `backend/handlers/lotes.go` (+ `lotes_test.go`) + `backend/main.go` (+ `main_test.go`) -- POST 201/400/404, publish SSE, 403 por papel/401 pelo `newMux` real
- `backend/services/catalogo.go` (+ testes) -- view nas leituras de saldo; `lotes` no detalhe (vencido, legado, desconhecida); total = legado + Lotes
- `backend/services/estoques.go` e `normalizacao.go` (+ testes) -- guard AD-31 e reescrita de `lotes` na mesclagem
- `frontend/.../LancamentoSaldoSection.tsx` (+ teste), `EstoquesPage.tsx`, `ProdutoDetalhePage.tsx`, `MovimentacoesSection.tsx` (+ testes ajustados) -- UI

**Acceptance Criteria:**
- Given a tela Lançar saldo, when o Almoxarife informa Produto, Estoque, quantidade e validade opcional, then nasce uma linha nova em `lotes`, nunca sobrescrevendo outro Lote do par, com Movimentação `entrada` registrada.
- Given um Produto com Lotes no Estoque, when lança de novo, then há um Lote adicional e a quantidade do Catálogo/detalhe é a soma de todos.
- Given um Lote com validade passada, when o detalhe é consultado, then aparece "Vencido" (âmbar) e o saldo segue contado.
- Given `usuario`, when chama `POST /api/lotes`, then 403; given quantidade ≤ 0, then 400.

## Spec Change Log

## Review Triage Log

### 2026-09-20 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 3: (high 0, medium 2, low 1)
- defer: 1: (high 0, medium 0, low 1)
- reject: 20
- addressed_findings:
  - `[medium]` `[patch]` `LancarSaldo` inseria Lote sem travar `produtos`/`estoques`: mesclagem concorrente (que trava `produtos FOR UPDATE`) podia deixar um Lote órfão num Produto soft-deleted. Corrigido com `FOR SHARE OF p, e` no `INSERT ... SELECT` (`backend/services/lotes.go`).
  - `[low]` `[patch]` Violação de FK (23503) numa corrida com `ExcluirEstoque` virava 500; agora colapsa em `ErrLoteAlvoNaoEncontrado` -> 404.
  - `[medium]` `[patch]` Filtro `ComEstoque` sobre a view não tinha teste com saldo só de Lote; adicionado `TestCatalogo_ComEstoqueConsideraSaldoSoDeLote`.

### 2026-09-20 — Review pass (2ª passada, revisão nova sobre o estado `done`)
- intent_gap: 0
- bad_spec: 0
- patch: 3: (high 0, medium 2, low 1)
- defer: 0
- reject: 24
- addressed_findings:
  - `[medium]` `[patch]` Faltava teste de corrida `LancarSaldo` × `ExcluirEstoque` (o comentário de `LancarSaldo` afirma a serialização): adicionado `TestLancarSaldo_CorridaComExcluirEstoque` (`backend/services/lotes_test.go`).
  - `[medium]` `[patch]` A Movimentação `entrada` (origem NULL) não tinha teste passando por `ListarMovimentacoes`/`ListarMovimentacoesDoUsuario` (trilha e exportação LGPD): adicionado `TestListarMovimentacoes_EntradaSemOrigem`.
  - `[low]` `[patch]` Ordem dos Lotes no detalhe (validade crescente, NULL por último) e ausência de entrada `legado` com `produto_estoque = 0` não eram afirmadas: adicionado `TestObterProdutoDetalhe_LotesOrdemELegadoZerado`.
- Rejeitados (resumo): saldo só-em-Lote não consumível por Baixa/Transferência/Carrinho/Pedidos (limitação transitória explícita no `Never`/Design Notes, Stories 11.2–11.5); `CURRENT_DATE`/fuso (já registrado como DW-79 no ledger — intocado); `produtoId`/`estoqueId` vazio -> 404 (o intent-contract manda malformado 22P02 colapsar em 404); NaN/Inf e arredondamento de casas (mesma checagem do molde `RegistrarBaixa`, JSON não produz NaN, teto ≤ `limiteNumeric103` barra o overflow); `down.sql` destrutivo (inerente ao rollback, já especificado); idempotência/edição/estorno/nº de lote/FEFO/alerta de vencimento (fora do escopo do intent); leituras do detalhe fora de transação (padrão pré-existente); resíduo de Estoque por Lote de Produto soft-deleted (o contrato manda QUALQUER Lote barrar; mesclagem reescreve os Lotes); a11y/UX menores do formulário e parsing `paraNumero`/data parcial (nativo do `<input>`, cosmético).

### 2026-09-20 — Review pass (3ª passada, revisão nova sobre o estado `done`)
- intent_gap: 0
- bad_spec: 0
- patch: 1: (high 0, medium 0, low 1)
- defer: 0
- reject: 30
- addressed_findings:
  - `[low]` `[patch]` A ordem "entrada `legado` por último" no detalhe não era afirmada (só contagem/pertencimento): `TestLancarSaldo_SegundoLancamentoCriaLoteAdicional` agora exige `lotes[len(lotes)-1].Legado` (`backend/services/lotes_test.go`).
- Rejeitados (resumo): saldo só-em-Lote não consumível por Baixa/Transferência/Carrinho/Pedidos (transitório explícito no `Never`, Stories 11.2–11.5); corrida `LancarSaldo` × `MesclarDuplicatas` (a mesclagem trava `produtos ... FOR UPDATE` em `normalizacao.go:1133` antes do `UPDATE lotes`, e `LancarSaldo` usa `FOR SHARE OF p, e` — já coberto); `CURRENT_DATE`/fuso (DW-79, intocado); idempotência/edição/estorno/nº de Lote/FEFO/lançamento em massa (fora do intent); Lote zerado no detalhe e Estoque nunca excluível após Lote (o contrato manda QUALQUER Lote barrar; Lote zerado só existirá com a 11.4); ramo `lotes` do resíduo sem `deleted_at` (o contrato manda QUALQUER Lote barrar; mesclagem reescreve os Lotes); 413/`DisallowUnknownFields`/JSON residual no handler (paridade com o molde `RegistrarBaixaHandler`); arredondamento a 3 casas e `paraNumero` (mesma checagem do molde, cosmético); exportação (não lê `produto_estoque` direto — reusa as consultas do Catálogo já trocadas para a view); migração `down` destrutiva (inerente e especificada); a11y/UX menores, duplicação do `TRUNCATE` nos testes, documentação/OpenAPI (ruído).

## Design Notes

`lotes` só substitui `produto_estoque` na Story 11.2; até lá o saldo é ADITIVO (legado + Lotes) por uma view, e o Lote legado aparece no detalhe como entrada `legado`, exatamente o que 11.2 materializará (sem dupla contagem: 11.2 move o legado para `lotes` e zera/descontinua `produto_estoque`). Baixa/Transferência/Pedidos continuam validando contra `produto_estoque` até 11.4/11.5 — limitação transitória conhecida do Épico 11, não defeito desta story. A Movimentação usa `tipo='entrada'` (não `ajuste`, reservado a correções) com `lote_id` para rastrear qual Lote a originou; a invariante "soma de Movimentações = quantidade" continua valendo para o que 11.4 passar a debitar.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros e sem arquivos listados.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 -p 1 ./...` -- expected: tudo passa (`services`/`handlers` são lentos; rodar separados se necessário).
- `cd frontend && npx vitest run LancamentoSaldo EstoquesPage ProdutoDetalhePage MovimentacoesSection` -- expected: passa.
- `cd frontend && npm run build` -- expected: sem erros de tipo.

## Auto Run Result

Status: done

**Resumo:** terceira passada de revisão (revisão nova sobre o estado `done`) da Story 11.1 — tabela `lotes` + view `saldo_produto_estoque`, `POST /api/lotes` (`almoxarife`+, Lote sempre novo + Movimentação `entrada` na mesma transação), guard de exclusão de Estoque, reescrita de Lotes na mesclagem, detalhe com Lotes/vencido/legado e tela "Lançar saldo". Nenhuma mudança de código de produção; um patch de teste.

**Arquivos alterados nesta passada:**
- `backend/services/lotes_test.go` — `TestLancarSaldo_SegundoLancamentoCriaLoteAdicional` passa a afirmar que a entrada `legado` vem por último.
- `_bmad-output/implementation-artifacts/spec-11-1-...md` — Review Triage Log e este resultado.

**Achados:** patches aplicados 1 (low 1); adiados 0 (o DW-79 pré-existente e o ledger `deferred-work.md` permanecem intocados); rejeitados 30.

**Revisão de acompanhamento recomendada:** `false` — patches desta passada: 0 high, 0 medium, 1 low; pontuação 3×0 + 1×1 = 1 (< 5).

**Verificação:** `go build ./...`, `go vet ./services/`, `gofmt -l .` sem saída; `go test -count=1 -p 1 ./...` todos os pacotes `ok`. Frontend não foi alterado nesta passada (última verificação: `vitest` 63/63 e `npm run build` ok na passada anterior).

**Riscos residuais:** saldo lançado só em Lote aparece no Catálogo/detalhe mas Baixa/Transferência/Carrinho/Pedidos ainda validam contra `produto_estoque` (transitório, Stories 11.2–11.5); `vencido` usa o fuso do banco (DW-79). `deferred-work.md` e `sprint-status.yaml` têm alterações não commitadas de responsabilidade do orquestrador, deixadas intactas por instrução.
