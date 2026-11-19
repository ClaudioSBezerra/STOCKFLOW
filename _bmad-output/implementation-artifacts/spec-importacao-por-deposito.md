---
title: 'Importação de planilha para UM Depósito escolhido na tela'
type: 'feature'
created: '2026-11-19'
status: 'done'
review_loop_iteration: 0
baseline_commit: '665a72a'
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-depositos-na-filial.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** (Pedido do usuário, 2026-11-19: "na hora da importação perguntar para qual depósito"; "somente 1 depósito por importação".) Hoje a importação lê o depósito da coluna "Estoque" de cada linha e CRIA o depósito (na Filial padrão) se o nome não existe. Isso fura a regra de que só Gestor+ cadastra depósito e põe o saldo em lugar que o usuário não escolheu.

**Approach:** A tela de importação passa a pedir o Depósito de destino (lista dos depósitos da Empresa agrupada por Filial). Toda a planilha entra nesse único depósito. A importação nunca cria depósito. O depósito escolhido fica gravado na importação, para que "continuar" uma importação interrompida use o mesmo destino.

## Boundaries & Constraints

**Always:**
- Nova migration `000064`: coluna `importacoes.estoque_id UUID NULL REFERENCES estoques(id)` (nula só para importações antigas já gravadas, que seguem pelo caminho legado ao serem continuadas) + `.down.sql`. Testar migrando banco vazio e o ciclo down/up.
- `POST /api/importacoes` (almoxarife+, inalterado) exige o campo multipart `estoque_id`. Ausente, mal formado, de outra Empresa (AD-20) ou inexistente -> 400/404 com mensagem clara, nada gravado. O servidor valida; a tela não é a autoridade.
- Todas as linhas gravam saldo no depósito escolhido (`produto_estoque`), tanto na criação de produto quanto na atualização (upsert que substitui a quantidade, como hoje, só no par Produto/depósito escolhido).
- `encontrarOuCriarEstoque` deixa de ser usada pela importação nova; nenhuma linha cria depósito. Importação antiga (sem `estoque_id`) continua ao ser retomada com o comportamento legado, para não quebrar linhas pendentes.
- **Coluna "Estoque" do modelo: MANTIDA (decisão do usuário, 2026-11-19).** manter o cabeçalho fixo de 17 colunas (nenhuma planilha existente quebra e nenhuma linha já gravada muda de tamanho), mas a coluna "Estoque" passa a ser IGNORADA (pode vir vazia ou preenchida); o modelo para download e as instruções avisam "o depósito é escolhido na tela". Remover a coluna do modelo fica como melhoria futura.
- **Vazio vira zero (decisão do usuário, 2026-11-19: "se vier nulo, coloque zero; o mesmo vale para preço")**: célula vazia em "Quantidade" passa a valer 0 (hoje é erro "quantidade é obrigatória") e célula vazia em "Preço Unitário" passa a valer 0 (hoje é NULL). Valor preenchido inválido (texto, negativo) continua sendo recusado. Atualização de produto existente (decisão do usuário, 2026-11-19: "vazio ou zero, mantém o valor atual se o produto existir"): quantidade vazia OU 0 mantém o saldo atual do par Produto/depósito, e preço vazio OU 0 mantém o preço atual do produto (nada é zerado). Produto novo: vazio ou 0 grava 0. Quantidade 0 numa atualização é, portanto, "não mexer"; zerar saldo continua sendo feito por Lançar saldo/Baixa. O relatório conta quantas linhas mantiveram o valor atual.
- Tela (`ImportacaoProdutosSection.tsx`): seletor "Depósito de destino" obrigatório, agrupado por Filial (`GET /api/estoques` + `/api/filiais`), com o botão de enviar desabilitado até escolher; sem depósitos cadastrados, mensagem "Nenhum depósito cadastrado. Peça a um gestor para cadastrar em Cadastros > Filiais." e envio bloqueado. O relatório/resumo da importação mostra o depósito usado. Preserva o arquivo e a escolha se o envio der erro.
- Manual (`manual.go`) e textos de ajuda atualizados.

**Never:** criar depósito pela importação; permitir mais de um depósito por importação; mudar o cabeçalho de 17 colunas; mexer em Pedidos, Movimentações ou Lançar saldo.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Importação normal | planilha válida + depósito da Empresa | Produtos criados/atualizados, saldo no depósito escolhido | N/A |
| Sem depósito escolhido | `estoque_id` ausente | Nada gravado | 400 "escolha o depósito de destino" |
| Depósito de outra Empresa / inexistente | `estoque_id` alheio | Nada gravado | 404 |
| Quantidade ou preço vazio/zero, produto NOVO | célula em branco ou 0 | Grava 0 e a linha é importada | N/A |
| Quantidade ou preço vazio/zero, produto EXISTENTE | célula em branco ou 0 | Mantém saldo/preço atuais (sem zerar) | N/A |
| Quantidade/preço inválido | texto ou negativo | Linha rejeitada como hoje | motivo na linha |
| Coluna "Estoque" preenchida | linha com nome qualquer | Ignorada; nenhum depósito criado | N/A |
| Retomar importação | interrompida no meio | Continua no mesmo depósito | N/A |
| Importação antiga sem `estoque_id` | linhas pendentes legadas | Continua pelo caminho legado | N/A |
| Empresa sem depósito | nenhum cadastrado | Tela bloqueia o envio e orienta | N/A |
| Depósito excluído no meio | continuar após exclusão | Linhas restantes rejeitadas com motivo claro | N/A |

</frozen-after-approval>

## Code Map

- `backend/migrations/000064_importacoes_estoque_id.{up,down}.sql` (nova).
- `backend/services/importacoes.go`: `CriarImportacao` recebe/valida/grava `estoque_id`; `processarProximaLinha` (~584) e `processarLinhaDeAtualizacao` (~782) usam o depósito da importação em vez de `encontrarOuCriarEstoque`; `validarLinhaImportacao` deixa de validar o nome da coluna Estoque; `Importacao`/relatório expõem o depósito.
- `backend/handlers/importacoes.go`: lê `estoque_id` do multipart; testes de handler.
- `backend/main_test.go`: papéis (almoxarife+ importa; usuário 403) inalterados.
- `frontend/src/components/produtos/ImportacaoProdutosSection.tsx` (+ teste), `ImportarProdutosPage.tsx`; modelo de planilha e textos; `backend/services/manual.go`.

## Tasks & Acceptance

**Execution:**
- [x] Migration 000064 (+ down) e teste de ciclo
- [x] Serviço: criar importação com `estoque_id` validado por Empresa; processamento usa o depósito; nunca cria depósito; legado preservado
- [x] Handler: campo `estoque_id`, erros 400/404; testes
- [x] Frontend: seletor agrupado por Filial, bloqueios, depósito no relatório; testes
- [x] Modelo/manual/textos: avisar que a coluna "Estoque" é ignorada
- [x] Conferência no navegador (banco de teste novo, sem tocar produção): importar para um depósito, retomar, tentar sem depósito

**Acceptance Criteria:**
- Given um Almoxarife, when escolhe um depósito e importa uma planilha, then todo o saldo entra nesse depósito e nenhum depósito novo é criado
- Given `estoque_id` ausente ou de outra Empresa, when envia, then é recusado sem gravar nada
- Given `go build ./...`, `go test -p 1 ./...` (uma suíte por vez), `npm run build` e `npx vitest run`, when rodados, then terminam sem erro

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test ./services/... ./handlers/... -run 'Importa'`
- `cd backend && go test -p 1 -timeout 40m ./...` (DATABASE_URL do Postgres de teste, porta 55432)
- `cd frontend && npm run build && npx vitest run`

## Suggested Review Order

**Destino da importação**

- Valida o depósito dentro da transação (FOR SHARE, por Empresa) e grava `estoque_id` + `por_deposito`; FK vira 404
  [`importacoes.go`](../../backend/services/importacoes.go)

- Cada linha grava no depósito da importação; nunca cria depósito; legado (`por_deposito=false`) preservado
  [`importacoes.go`](../../backend/services/importacoes.go)

- Migration: `estoque_id` com `ON DELETE SET NULL` e `por_deposito`
  [`000064_importacoes_estoque_id.up.sql`](../../backend/migrations/000064_importacoes_estoque_id.up.sql)

**Vazio e zero**

- `ehZeroNaColuna`: vazio/0/arredondado a 0 -> produto novo grava 0; existente mantém saldo e preço; relatório usa a mesma decisão
  [`importacoes.go`](../../backend/services/importacoes.go)

**Entrada e tela**

- Campo `estoque_id` obrigatório (400/404)
  [`importacoes.go`](../../backend/handlers/importacoes.go)

- Seletor "Depósito de destino" por Filial, bloqueios, "Tentar de novo", depósito no resumo
  [`ImportacaoProdutosSection.tsx`](../../frontend/src/components/produtos/ImportacaoProdutosSection.tsx)
