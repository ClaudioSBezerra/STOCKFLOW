---
title: 'Story 11.4: Baixa e Transferência consomem Lote automaticamente e respeitam saldo reservado'
type: 'feature'
created: '2026-09-21'
baseline_revision: '1a8b983740618d7a84d805ff648d7e0a556d736f'
status: done
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: [oversized]
deferred:
  - summary: >-
      A aprovação de Pedido (`DecidirPedido`) ainda lê/debita só `produto_estoque`, então saldo que a Transferência agora credita apenas em `lotes` no destino não é aprovável até a Story 11.5.
    evidence: |-
      `RegistrarTransferencia` nunca escreve `produto_estoque` do destino (fonte única `lotes`, AD-24); `DecidirPedido` segue com débito legado. Janela já documentada no Epic 11 e fechada pela 11.5 (que deve reusar `consumirFEFOTx`).
    location: >-
      backend/services/pedidos.go (DecidirPedido)
    severity: medium
---

<intent-contract>

## Intent

**Problem:** `RegistrarBaixa`/`RegistrarTransferencia` (Stories 5.1/5.2) só enxergam `produto_estoque`: ignoram `lotes` (Story 11.1), validam contra o saldo físico e podem debitar saldo já reservado por Pedido pendente (Story 11.3), além de não preservar validade (FR-14, FR-15, AD-24, AD-25).

**Approach:** Uma função FEFO única e reutilizável (`consumirFEFOTx`, a 11.5 vai reaproveitá-la). Baixa/Transferência travam os pares sob a ordem canônica de 11.3, validam contra `saldoDisponivelParTx` (físico − reservas) e debitam por FEFO, gerando 1 Movimentação por fonte consumida. Na Transferência o destino recebe Lote(s) com a mesma `data_validade`.

## Boundaries & Constraints

**Always:**
- Validações prévias inalteradas (quantidade ≤ 0, > `limiteNumeric103`, origem = destino → `ErroMovimentacaoValidacao`, antes de abrir a transação).
- Travar com `travarSaldoParesTx` (pares ordenados `(produto_id, estoque_id)`; por par `produto_estoque` e depois `lotes ORDER BY id FOR UPDATE`) ANTES de ler o disponível ou escrever; origem e destino da Transferência entram no MESMO conjunto ordenado (nunca origem-depois-destino).
- Validar com `saldoDisponivelParTx` (nunca saldo bruto). `quantidade` > disponível → `ErroQuantidadeIndisponivel{Disponivel: max(disponível, 0)}`, nada gravado. Produto/Estoque malformado, inexistente ou de outra Empresa (origem ou destino) colapsa em `Disponivel: 0`.
- FEFO: fontes de consumo do par em ordem: Lotes com `data_validade` (`ORDER BY data_validade, criado_em, id`), depois o saldo legado de `produto_estoque` (quando > 0; é saldo sem Lote e sem validade, o mais antigo), depois Lotes sem validade (`criado_em, id`). Só considera fontes com quantidade > 0; nenhuma tela/endpoint escolhe Lote. Aritmética arredondada a 3 casas (`arredondar3`); o débito nunca deixa `quantidade` negativa.
- Toda escrita em `lotes.quantidade`/`produto_estoque.quantidade` gera uma Movimentação na MESMA transação: 1 linha por fonte consumida, com a quantidade dessa fonte e `lote_id` (NULL quando a fonte é `produto_estoque`). Transferência: `tipo='transferencia'`, origem e destino preenchidos, `lote_id` = Lote de origem.
- Transferência preserva a validade: cada fonte debitada credita o Estoque destino em Lote com a MESMA `data_validade` (`IS NOT DISTINCT FROM`): soma no Lote de destino mais antigo com essa validade, ou cria Lote novo (com `criado_em` da fonte). Saldo do destino nunca é escrito em `produto_estoque`. Lote esgotado não é apagado.
- A assinatura e o JSON de `RegistrarBaixa`/`RegistrarTransferencia` não mudam: devolvem UMA `Movimentacao` — a da primeira fonte consumida, com `Quantidade` = total pedido (o handler publica o `ID` dela no canal `movimentacoes`).
- UI: os diálogos de Baixa e Transferência mostram o saldo DISPONÍVEL da linha (`linha.disponivel`) e avisam que saldo reservado por Pedidos não pode ser baixado/transferido; nenhum seletor de Lote.

**Block If:** Nenhuma decisão bloqueante identificada.

**Never:** Não alterar Carrinho/Envio (11.3), Aprovação/`DecidirPedido` (11.5), Cadastro (11.6), a migração 11.2 nem o schema; não criar migration; não expor Lote na API de Baixa/Transferência; não materializar saldo; não tocar `sprint-status.yaml`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| FEFO simples | Lotes A (val. 10/2026, 3), B (val. 08/2026, 4); Baixa 5 | B → 0, A → 2; 2 Movimentações (B:4, A:1) | — |
| Sem validade por último | Lote N (sem val., 5) e D (com val., 2); Baixa 4 | D → 0, N → 3 | — |
| Legado + Lotes | `produto_estoque` 3, Lote datado 2, Lote sem val. 4; Baixa 6 | datado 2, legado 3, sem val. 1 | — |
| Reservado | Físico 10, reservado 8; Baixa 3 | rejeitada, nada muda | `ErroQuantidadeIndisponivel{2}` (409) |
| Dentro do disponível | Físico 10, reservado 8; Baixa 2 | ok; físico 8, disponível 0 | — |
| Transferência parcial | Lote val. X qtd 5 → destino; Transferir 2 | origem 3; destino ganha Lote val. X qtd 2 | — |
| Destino já tem mesma validade | Destino com Lote val. X | soma nesse Lote (sem Lote novo) | — |
| Transferência multi-Lote | 2 Lotes de validades distintas, transferir tudo | destino com 2 Lotes, cada um com a validade original | — |
| Transferência de legado | só `produto_estoque` | destino recebe Lote sem validade | — |
| Origem = destino / qtd ≤ 0 | — | rejeitado antes da transação | `ErroMovimentacaoValidacao` (400) |
| Alheio/malformado | Estoque de outra Empresa ou id inválido | nada gravado | `Disponivel: 0` (409) |
| Corrida | 2 Baixas concorrentes, disponível 5, cada uma pede 5 | 1 sucesso, outra `Indisponivel` | 409 |
| Locks opostos | Transferências A→B e B→A concorrentes | sem deadlock | — |

</intent-contract>

## Code Map

- `backend/services/movimentacoes.go:214-460` -- `RegistrarBaixa`/`RegistrarTransferencia` (hoje só `produto_estoque`, upsert-lock do destino via `travarLinhaProdutoEstoque`/`erroTravarProdutoEstoque`); passam a usar os helpers abaixo. Remover o que ficar sem uso.
- `backend/services/reservas.go:45-119` -- `travarSaldoParesTx` (lock canônico) e `saldoDisponivelParTx` (físico − reservas), REUSAR sem alterar contrato.
- `backend/services/lotes.go` -- molde de INSERT em `lotes` e `movimentacoes` (coluna `lote_id`, migration 000041).
- `backend/migrations/000041_create_lotes.up.sql` -- schema (`lotes.quantidade NUMERIC(10,3) CHECK >= 0`); `arredondar3` já existe em `services/catalogo.go`.
- `backend/handlers/movimentacoes.go:50-180` -- handlers; sem mudança (mesma `Movimentacao`).
- `backend/services/movimentacoes_test.go` (`seedProdutoComSaldo`, testes de 5.1/5.2), `services/lotes_test.go`, `services/reservas_test.go` -- moldes; testes de Transferência que leem `produto_estoque` do destino passam a ler a view `saldo_produto_estoque`.
- `frontend/src/pages/ProdutoDetalhePage.tsx:955-1085` (+ `.test.tsx`) -- diálogos de Baixa/Transferência; `linha.disponivel` já vem do detalhe (11.3).

## Tasks & Acceptance

**Execution:**
- `backend/services/fefo.go` (+ `fefo_test.go`) -- `consumirFEFOTx(tx, empresaID, produtoID, estoqueID, quantidade) ([]consumoFonte, error)` (lê as fontes já travadas, debita, devolve por fonte: `LoteID *string`, `Quantidade`, `DataValidade sql.NullString`, `CriadoEm`), e a ordem FEFO testada isoladamente
- `backend/services/movimentacoes.go` -- reescrever Baixa/Transferência (travar, disponível, FEFO, Movimentação por fonte, crédito no destino com validade preservada)
- `backend/services/movimentacoes_test.go`, `reservas_test.go` -- cobrir a matriz (FEFO, legado, reservado, validade no destino, merge, corrida, locks opostos com `-race`); ajustar testes de 5.2 que liam `produto_estoque` do destino
- `frontend/src/pages/ProdutoDetalhePage.tsx` (+ teste) -- mostrar disponível e aviso de reserva nos dois diálogos

**Acceptance Criteria:**
- Given uma Baixa/Transferência de quantidade X, when confirmada, then o débito segue FEFO (`data_validade` mais próxima primeiro, sem validade por último) sem escolha manual de Lote.
- Given saldo físico suficiente mas disponível (menos reserva) insuficiente, when Baixa/Transferência é pedida, then é rejeitada e nada é debitado.
- Given uma Transferência de um Lote inteiro ou parte dele, when confirmada, then o destino recebe saldo com a Data de Validade original.
- Given débito em várias linhas, when os locks são adquiridos, then seguem `(produto_id, estoque_id, lote_id)` ascendente antes de qualquer escrita.
- Given qualquer escrita em saldo, when ocorre, then há Movimentação correspondente na mesma transação.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 3: (high 0, medium 1, low 2)
- defer: 1: (high 0, medium 1, low 0)
- reject: 27
- addressed_findings:
  - `[medium]` `[patch]` Quantidade positiva que arredonda a 0 (ex.: 0,0004) não tinha teste: sem a guarda, Baixa/Transferência devolveriam sucesso com Movimentação vazia; casos 0,0004 adicionados aos testes de quantidade inválida de Baixa e Transferência.
  - `[low]` `[patch]` `validarQuantidadeMovimentacao` deixava passar NaN/±Inf: guarda `math.IsNaN`/`math.IsInf` + casos de teste.
  - `[low]` `[patch]` Fallback de `consumirFEFOTx` (fontes < disponível, inalcançável) devolvia `ErroQuantidadeIndisponivel` com valor enganoso: agora erro interno explícito.
- Adiado: aprovação de Pedido sobre saldo só em `lotes` (Story 11.5) — ver `deferred`.
- Rejeitados (resumo): Lote vencido consumido (o épico manda só sinalizar, nunca bloquear); N Movimentações por operação e ausência de id de correlação / Lote de destino na Movimentação (decisão do contrato: 1 por fonte, JSON inalterado); Lotes duplicados de mesma validade em Transferências concorrentes (sem unique, benigno, funde nas seguintes); `criado_em` do legado transferido (Lote novo no destino); overflow de `numeric(10,3)` na soma do Lote de destino (inalcançável na prática); lock `FOR SHARE OF e` vs `ExcluirEstoque` (mesmo padrão da 11.3); `travarLinhaProdutoEstoque` (ainda usada por `normalizacao.go`); ordenação FEFO em Go (função pura testada); pedidos de UI (max no input, aria-describedby, aviso sem reserva, dados de teste) e de mais testes de rollback/handler (cosméticos).

### 2026-09-21 — Review pass (2)
- intent_gap: 0
- bad_spec: 0
- patch: 1: (high 0, medium 0, low 1)
- defer: 0
- reject: 30
- addressed_findings:
  - `[low]` `[patch]` Docstring de `consumirFEFOTx` ainda dizia que devolvia `*ErroQuantidadeIndisponivel` quando as fontes não cobrem a quantidade, mas o código (desde a passada anterior) devolve erro interno; comentário corrigido em `backend/services/fefo.go`.
- Rejeitados (resumo): `sql.ErrNoRows` em `erroSaldoAlvo` (`saldoDisponivelParTx` é agregado e sempre devolve linha); legado negativo (`produto_estoque` tem `CHECK quantidade >= 0`); destino soft-deleted (não existe `deletado_em`); `consumos` vazio (quantidade > 0 e ≤ disponível garante ao menos uma fonte); leitura de `lotes` sem `FOR UPDATE` (o lock do par por `travarSaldoParesTx` já cobre); Lotes duplicados em Transferências concorrentes ao mesmo destino, Lote vencido, N Movimentações por operação, ordem das Movimentações e ausência de id de operação (decisões do contrato / já rejeitados na passada anterior); `TestExcluirEstoque_ComPedidoPendente` ajustado (consequência esperada da reserva); pedidos de UI e testes extras de handler/concorrência com reserva (cosméticos). A janela de `DecidirPedido` sobre saldo só em `lotes` já consta em `deferred` (Story 11.5).

## Design Notes

- **Legado como fonte:** enquanto a 11.2 não rodar em cada ambiente (corte humano), saldo vive em `produto_estoque`; tratá-lo como fonte sem validade e a mais antiga faz Baixa/Transferência corretas antes e depois do corte. O crédito no destino vai sempre para `lotes` (fonte única, AD-24).
- **Movimentação por fonte:** dá rastreio por Lote; a `Movimentacao` devolvida é a primeira, para não quebrar o contrato JSON.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros e sem arquivos listados.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 -p 1 ./...` -- expected: tudo passa.
- `cd frontend && npx tsc -b && npx vitest run` -- expected: sem erros de tipo, testes passam.

## Auto Run Result

Status: done

- **Resumo:** nova passada de revisão sobre a implementação já entregue (commit `7b4bc71`). Baixa e Transferência consomem Lote por FEFO (`consumirFEFOTx`), validam contra o saldo disponível (físico − reservas), geram 1 Movimentação por fonte e preservam a validade no destino; diálogos mostram o saldo disponível e o aviso de reserva.
- **Arquivos alterados nesta passada:** `backend/services/fefo.go` — docstring de `consumirFEFOTx` corrigida (erro interno, não `ErroQuantidadeIndisponivel`). Demais arquivos da story inalterados (`movimentacoes.go`, `fefo_test.go`, `movimentacoes_test.go`, `estoques_test.go`, `handlers/movimentacoes_test.go`, `ProdutoDetalhePage.tsx` e teste).
- **Achados:** patches aplicados 1 (baixa); adiados 0 novos (o item de `DecidirPedido` já estava em `deferred`); rejeitados 30.
- **Recomendação de nova revisão:** `false` (patches: high 0, medium 0, low 1; pontuação 3×0 + 1×1 = 1 < 5).
- **Verificação:** `go build ./... && go vet ./... && gofmt -l .` sem erros e sem arquivos listados; `go test -count=1 -p 1 ./...` todos os pacotes `ok`; `npx tsc -b` sem erros; `npx vitest run` com 716/719 na execução completa — 3 timeouts de 5 s em `CadastroProdutoSection.test.tsx` (arquivo não tocado pela story, lentidão por carga), que passam isolados (79/79 junto de `ProdutoDetalhePage.test.tsx`).
- **Riscos residuais:** aprovação de Pedido (`DecidirPedido`) ainda debita só `produto_estoque`; saldo transferido apenas para `lotes` no destino só é aprovável após a Story 11.5. Possível Lote duplicado de mesma validade em Transferências concorrentes ao mesmo destino (benigno, sem unique).
