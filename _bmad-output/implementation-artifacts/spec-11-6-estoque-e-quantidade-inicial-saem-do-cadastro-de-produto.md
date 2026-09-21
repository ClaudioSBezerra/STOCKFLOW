---
title: 'Story 11.6: Estoque e quantidade inicial saem do Cadastro de Produto'
type: 'feature'
created: '2026-09-21'
baseline_revision: '813f6e50eaabbb284b940e4e7e9775f4bd38dc54'
status: done
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: []
deferred:
  - summary: >-
      A importação em massa (`services/importacoes.go`) ainda grava saldo em `produto_estoque` (sem Lote) ao criar/atualizar Produto por linha da planilha.
    evidence: |-
      O épico manda a importação gerar Lote com `data_validade = NULL` (FR10, AD-30); hoje ela escreve a tabela legada. Comportamento pré-existente, não alterado pela 11.6 (a story trata só do formulário de cadastro).
    location: >-
      backend/services/importacoes.go
    severity: medium
---

<intent-contract>

## Intent

**Problem:** O Cadastro de Produto (Story 3.1) ainda exige Estoque destino e quantidade inicial e grava uma linha em `produto_estoque` na criação — um segundo caminho de entrada de saldo, sem Lote nem validade, que contradiz a Story 11.1 (Lançamento de Saldo é o único caminho) e o Épico 11 (FR8, AD-29).

**Approach:** Remover `estoque_id`/`quantidade_inicial` do formulário, do request/handler e de `services.CriarProduto`: o Produto nasce sem nenhuma linha de saldo e aparece no Catálogo com quantidade 0 (o Catálogo já usa LEFT JOIN). Produtos já cadastrados não mudam; testes que semeavam saldo via cadastro passam a semear por helper de teste.

## Boundaries & Constraints

**Always:**
- `CriarProduto` deixa de ter `EstoqueID`/`QuantidadeInicial` no input e de validá-los; não insere em `produto_estoque` nem em `lotes`. Todas as demais validações (nome, categoria, template, dimensões, 10.3) e a geração do código (10.2) ficam idênticas, na mesma transação.
- `POST /api/produtos` deixa de declarar os campos em `criarProdutoRequest`; JSON com `estoque_id`/`quantidade_inicial` de clientes antigos é IGNORADO (sem erro, sem saldo criado). Resposta `201` e demais erros inalterados.
- Formulário: some o Select de Estoque e o Input de quantidade inicial, o estado correspondente, o `fetch('/api/estoques')` do `Promise.all` de carga (categorias + templates continuam) e as condições em `desabilitado`; o corpo do POST não os envia. O restante (foto, dimensões, 10.1–10.3) não muda.
- Produto sem linha de saldo aparece no Catálogo (grade/tabela/detalhe/filtros) com quantidade total 0, sem estado especial; o filtro "com estoque" já o exclui.
- Nenhuma migration; não alterar dados existentes; Lançamento de Saldo (11.1) segue como único caminho de entrada de saldo.

**Block If:** Nenhuma decisão bloqueante identificada.

**Never:** Não alterar `lotes`, Lançamento de Saldo, migração 11.2, importação em massa, reservas/pedidos/FEFO, nem `sprint-status.yaml`; não deixar caminho oculto que crie saldo no cadastro; não relaxar as demais validações do cadastro.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Cadastro sem saldo | Payload válido sem `estoque_id`/`quantidade_inicial` | `201`; nenhuma linha em `produto_estoque`/`lotes` para o Produto | — |
| Cliente antigo | Payload válido COM `estoque_id` e `quantidade_inicial: 5` | `201`; campos ignorados, nenhuma linha de saldo | — |
| Catálogo | Produto recém-criado sem saldo | Aparece com quantidade total 0; some com `comEstoque=true` | — |
| Validações mantidas | Nome curto, categoria/template ausentes ou inexistentes, dimensão incompleta | `400 VALIDATION_ERROR` como antes; nada gravado | mensagens inalteradas |
| Formulário | Preenchidos nome, categoria, template e unidade | Botão habilita sem Estoque/quantidade; POST sem os dois campos; nenhum GET `/api/estoques` | — |
| Produto antigo | Produto com `produto_estoque` prévio | Saldo e Catálogo inalterados | — |

</intent-contract>

## Code Map

- `backend/services/produtos.go:155-188` -- `CriarProdutoInput` (remover `EstoqueID`/`QuantidadeInicial` + docstring); `:326-525` `CriarProduto` (remover validações `estoque é obrigatório`/quantidade, bloco `insertProdutoEstoque` e docstring); `ErroProdutoValidacao` docstring cita estoque/quantidade. `limiteNumeric103` continua usado pelas dimensões.
- `backend/handlers/produtos.go:73-163` -- `criarProdutoRequest`/`CriarProdutoHandler` (remover os dois campos e docstring que cita `produto_estoque`).
- `backend/services/catalogo.go:288,436` -- LEFT JOIN de saldo já cobre Produto sem linha; sem mudança esperada (só cobrir por teste).
- `backend/services/catalogo_test.go:120-140` -- helpers `criarProdutoCat`/`limparEstoqueDe` (molde de seed de saldo).
- ~25 arquivos `*_test.go` (services, handlers, `main_test.go`, `empresa_teste_test.go`) usam `CriarProdutoInput{EstoqueID, QuantidadeInicial}` como seed de saldo: `services/{movimentacoes,catalogo,produtos,importacoes,estoques,relatorios,normalizacao,fotos,isolamento}_test.go`, `handlers/{produtos,normalizacao,estoques,fotos,movimentacoes,nomenclatura,categorias}_test.go`, `main_test.go`. `movimentacoes_test.go:19` `seedProdutoComSaldo` e `normalizacao_test.go` (`seedProdutoParaMesclagem`) são os pontos centrais.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:29-54,234-260,299-320,353-377,614-640` -- estados, carga de listas, `desabilitado`, corpo do POST e JSX do Select de Estoque/Input de quantidade; `CadastroProdutoSection.test.tsx` (940 linhas) espelha isso.

## Tasks & Acceptance

**Execution:**
- `backend/services/produtos.go` -- remover campos, validações e INSERT em `produto_estoque` de `CriarProduto`; atualizar docstrings -- Produto nasce sem saldo
- `backend/handlers/produtos.go` -- remover campos de `criarProdutoRequest` e do repasse; atualizar docstrings -- API deixa de aceitar saldo
- `backend/**/*_test.go` -- migrar seeds: helper de teste por pacote (`semearSaldoLegado`: INSERT em `produto_estoque`) chamado após `CriarProduto` onde o teste precisa de saldo; remover os campos dos literais; ajustar/remover testes que asseguravam "estoque é obrigatório"/quantidade negativa/limite
- `backend/services/produtos_test.go` e `backend/handlers/produtos_test.go` -- novos testes: sem linha em `produto_estoque`/`lotes` após cadastro (service e HTTP), campos antigos ignorados, Catálogo com quantidade 0 e fora do filtro `comEstoque=true`, validações mantidas
- `frontend/src/components/produtos/CadastroProdutoSection.tsx` (+ `.test.tsx`) -- remover Select de Estoque, Input de quantidade, estados, `fetch('/api/estoques')` e do corpo do POST; testes: campos ausentes, botão habilita sem eles, POST sem `estoque_id`/`quantidade_inicial`, nenhuma chamada a `/api/estoques`

**Acceptance Criteria:**
- Given a tela de Cadastro de Produto, when o Almoxarife cadastra um Produto novo, then não há campos de Estoque destino nem quantidade inicial e o Produto é criado sem linha de saldo.
- Given um Produto recém-cadastrado sem saldo, when aparece no Catálogo, then mostra quantidade total 0, sem estado de erro ou visibilidade especial.
- Given a tela de Lançamento de Saldo (11.1), when é preciso dar entrada num Produto novo, then ela é o único caminho — nenhum outro formulário/endpoint lança saldo inicial.
- Given Produtos cadastrados antes desta story, when a mudança entra em vigor, then nada muda para eles.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 0
- defer: 1 (medium 1)
- reject: 22
- addressed_findings:
  - none
- Rejeitados (resumo): campos legados ignorados sem `400`/aviso (decidido no contrato: cliente antigo recebe `201` sem saldo); INNER JOIN em consultas de saldo (todas são consultas específicas de saldo, a listagem/detalhe/busca usam LEFT JOIN ou lista vazia; catálogo e XLSX já cobertos por testes de Produto sem linha); helper de seed duplicado por pacote, sem checagem de Empresa, variádico, formato/nomes/comentários de teste obsoletos, seed sem Lote (a suíte legada de 11.4/11.5 cobre legado + Lotes), teste de corrida reescrito, dica de UI após o cadastro, testes extras de frontend, ordem de subtestes em mapa (cosméticos/fora do contrato). Adiado: importação em massa ainda grava `produto_estoque` (pré-existente, item próprio do épico).

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros e sem arquivos listados.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 -p 1 ./...` -- expected: tudo passa.
- `cd frontend && npx tsc --noEmit && npx vitest run` -- expected: sem erros de tipo e testes passando.

## Auto Run Result

Status: done

- **Resumo:** o Cadastro de Produto não recebe mais Estoque destino nem quantidade inicial. `CriarProduto` cria só a linha em `produtos` (código sequencial + demais validações inalterados) e nenhuma linha em `produto_estoque`/`lotes`; `POST /api/produtos` ignora `estoque_id`/`quantidade_inicial` de clientes antigos; o formulário perdeu o Select de Estoque, o Input de quantidade e o `GET /api/estoques`. O Catálogo já usava LEFT JOIN: Produto sem saldo aparece com quantidade 0 e some com `comEstoque=true`.
- **Arquivos alterados:**
  - `backend/services/produtos.go`, `backend/handlers/produtos.go` — remoção dos campos, validações e INSERT em `produto_estoque`; docstrings.
  - `backend/**/saldo_seed_test.go` (3 novos: services, handlers, main) — helpers de teste que semeiam saldo legado após o cadastro; ~17 arquivos `*_test.go` migrados para eles.
  - `backend/services/produtos_test.go`, `backend/handlers/produtos_test.go` — novos: sem saldo/lote após cadastro, Catálogo com 0 e fora de `comEstoque=true`, cliente antigo com `201`; removidos os testes de estoque/quantidade inexistentes.
  - `frontend/src/components/produtos/CadastroProdutoSection.tsx` (+ `.test.tsx`) — remoção dos campos e da chamada a `/api/estoques`; testes ajustados e novo teste de ausência dos campos.
- **Achados da revisão:** patches aplicados 0; adiados 1 (importação em massa ainda grava `produto_estoque`, pré-existente); rejeitados 22.
- **Recomendação de nova revisão:** `false` (patches: high 0, medium 0, low 0; pontuação 0 < 5).
- **Verificação:** `go build ./... && go vet ./... && gofmt -l .` sem erros e sem arquivos listados; `go test -count=1 -p 1 ./...` todos os pacotes `ok`; `tsc --noEmit` limpo; `vitest run src/components/produtos` 43/43. Na suíte completa do vitest o subagente relatou 3 timeouts em `CadastroProdutoSection.test.tsx` sob carga, que também ocorrem no baseline.
- **Riscos residuais:** cliente antigo que ainda envia saldo recebe `201` sem saldo, silenciosamente (decisão do contrato); a importação em massa continua criando saldo legado sem Lote (item adiado).
