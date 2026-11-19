---
title: 'Depósitos dentro da Filial (Empresa > Filial > Depósito)'
type: 'feature'
created: '2026-11-19'
status: 'done'
review_loop_iteration: 0
baseline_commit: '9c691c06f0e0258afa2855b635767f04f00d1b09'
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-filial-dados-fiscais.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** (Pedido do usuário, 2026-11-19.) O cadastro de Depósito (o que o sistema chama de "Local"/Estoque) mora em um menu à parte (Estoque > Locais), longe da Filial, e é aberto a Almoxarife. A hierarquia é Empresa > Filial > Depósito: o Depósito é filho da Filial, que é filha da Empresa. Os dados já seguem isso (todo Estoque exige Filial), mas a tela não.

**Approach:** O cadastro e a exclusão de Depósitos passam a ficar DENTRO da Filial (Cadastros > Filiais), só para Gestor ou acima. O item de menu Estoque > Locais vira **Depósitos**, somente consulta (lista agrupada por Filial, com indicadores e saldo), sem formulário de cadastro. Nome de tela e textos: "Depósito" no lugar de "Local".

## Boundaries & Constraints

**Always:**
- Nenhuma mudança no modelo de dados: o Depósito continua sendo a tabela `estoques` (com `filial_id` obrigatório para os novos). Nada de migration.
- Permissão: `POST /e/{slug}/api/estoques` e `DELETE /e/{slug}/api/estoques/{id}` passam de `RequireRole(PapelAlmoxarife)` para `RequireRole(PapelGestor)` (Gestor e Adm). `GET /api/estoques` e `/indicadores` continuam como estão. O servidor é a autoridade (403 para Almoxarife e Usuário, mesmo que a UI esconda).
- **Cadastros > Filiais** (`FiliaisSection.tsx`): cada Filial da tabela ganha uma área expansível **"Depósitos (N)"** com a lista de depósitos DAQUELA Filial (nome), um campo + botão **"Adicionar depósito"** (o nome é o único campo; a Filial é implícita — sem seletor de Filial) e um botão **Excluir** por depósito com o mesmo diálogo de confirmação e as mesmas mensagens de erro que a tela Locais usa hoje (depósito com saldo, com pedido pendente, etc. — reaproveite a lógica de `LocaisEstoqueSection.tsx`, extraindo-a para um componente `DepositosDaFilial` em vez de copiar). Estado vazio: "Nenhum depósito nesta filial ainda." Atualiza a lista e a contagem após criar/excluir, e preserva o que o usuário digitou se der erro.
- **Estoque > Locais -> Depósitos** (`LocaisEstoqueSection.tsx`, rota `/estoques` mantida): item de menu e título "Depósitos" (`papelMinimo: 'almoxarife'` continua, é consulta); lista agrupada por Filial (Filial como título do grupo, depósitos dentro, depósito legado sem Filial em "Sem filial"), com os indicadores que a tela já tem; REMOVE o formulário de cadastro e o botão de excluir. Um texto de apoio: para Gestor/Adm, "Para cadastrar ou excluir depósitos, use Cadastros > Filiais." com link para `/cadastros/filiais`; para Almoxarife, "Peça a um gestor ou administrador para cadastrar ou excluir depósitos."
- Troque os textos visíveis "Local/Locais" por "Depósito/Depósitos" na tela, no menu, em mensagens de sucesso/erro e no manual (`backend/services/manual.go`) e ajudas; NÃO renomeie rotas, identificadores internos, tabelas nem a palavra "Estoque" nas telas de saldo/Detalhe do Produto/Pedidos (fora de escopo).
- Testes de permissão pelo `newMux` REAL: Gestor e Adm criam/excluem depósito (201/204); Almoxarife e Usuário recebem 403 no POST e no DELETE; GET continua liberado como hoje. Ajuste todo teste existente que cria/exclui Estoque via HTTP como Almoxarife.

**Ask First:** nenhuma — permissão (Gestor+) e destino do menu (Depósitos, só consulta) decididos pelo usuário em 2026-11-19.

**Never:** migration; mudar `CriarEstoque`/`ExcluirEstoque` (serviço) além do necessário; renomear tabela, rotas ou tipos internos; mexer em Lançar saldo, Movimentações, Pedidos ou Detalhe do Produto; criar uma tela nova só para a árvore Empresa > Filial > Depósito.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Gestor adiciona depósito na Filial | nome "Almoxarifado Central" na Filial Recife | Depósito criado com `filial_id` da Recife; aparece na lista da Filial | N/A |
| Nome repetido na mesma Filial | mesmo nome (sem diferenciar caixa/espaços) | Nada criado | Mensagem de duplicado já usada hoje (409) |
| Nome vazio | "   " | Nada enviado | Botão desabilitado / 400 |
| Excluir depósito vazio | sem saldo e sem pedido pendente | Excluído e some da lista; contagem diminui | N/A |
| Excluir com saldo ou pedido pendente | depósito com resíduo ou pedido pendente | Não exclui | Mesma mensagem de hoje |
| Almoxarife / Usuário | POST ou DELETE `/api/estoques` | Nada feito | 403 |
| Almoxarife abre Depósitos | consulta | Vê a lista por Filial, sem formulário nem Excluir, com o texto "Peça a um gestor…" | N/A |
| Gestor abre Depósitos | consulta | Lista por Filial + link para Cadastros > Filiais | N/A |
| Depósito legado sem Filial | `filial_id` nulo | Aparece em "Sem filial" na consulta; não aparece dentro de nenhuma Filial | N/A |
| Empresa sem Filial | nenhuma Filial cadastrada | Filiais mostra o estado vazio de sempre; Depósitos explica que primeiro se cadastra uma Filial | N/A |

</frozen-after-approval>

## Code Map

- `backend/main.go:~529,536` (rotas `POST`/`DELETE /api/estoques`, hoje `RequireRole(services.PapelAlmoxarife)`) -> `PapelGestor`; comentários de cabeçalho do arquivo que citam "mínimo `almoxarife`" para criar/excluir estoque; `backend/main_test.go` e os testes de handler de estoques (`backend/handlers/estoques_test.go` ou equivalente): papéis.
- `frontend/src/components/estoques/LocaisEstoqueSection.tsx` (+ `.test.tsx`): vira consulta agrupada por Filial; a lógica de criar/excluir (e as mensagens de erro do `ExcluirEstoque`) é extraída para `frontend/src/components/filiais/DepositosDaFilial.tsx` (+ teste).
- `frontend/src/components/filiais/FiliaisSection.tsx` (+ `.test.tsx`): linha expansível com `DepositosDaFilial`; carregar `GET /api/estoques` uma vez e filtrar por `filial_id` (ou o filtro que a API já oferecer); atualizar após criar/excluir.
- `frontend/src/components/shell/nav-items.ts:98` (label 'Locais' -> 'Depósitos'; `nav-items.test.ts`), `frontend/src/pages/` que embrulha a seção (título da página), `frontend/src/pages/ConfiguracoesPage.tsx`/testes que mencionem "Locais".
- `backend/services/manual.go` e qualquer ajuda/texto que diga "Locais" como menu ou "cadastre um local".
- Mensagem existente "Nenhuma filial cadastrada. Peça a um gestor ou administrador para cadastrar uma filial antes de criar estoques." -> "…antes de cadastrar depósitos."

## Tasks & Acceptance

**Execution:**
- [x] `backend/main.go` (+ testes) -- POST/DELETE de estoques passam a `RequireRole(PapelGestor)`; testes pelo `newMux` real (Gestor/Adm ok; Almoxarife/Usuário 403); ajustar testes existentes
- [x] `frontend/src/components/filiais/DepositosDaFilial.tsx` (novo, + teste) e `FiliaisSection.tsx` (+ teste) -- depósitos dentro da Filial: listar, adicionar, excluir com confirmação e mensagens de erro
- [x] `frontend/src/components/estoques/LocaisEstoqueSection.tsx` (+ teste) e `nav-items.ts` (+ teste) -- vira "Depósitos", consulta agrupada por Filial, sem cadastro; textos de apoio por papel; título, menu e demais textos "Local" -> "Depósito"
- [x] `backend/services/manual.go` e ajudas -- textos
- [x] Conferência no navegador (backend + Vite locais, banco de teste novo e vazio, SEM tocar produção): como Gestor, criar uma Filial, abrir Cadastros > Filiais, expandir, adicionar dois depósitos, excluir um, ver a contagem; abrir Depósitos e ver a lista agrupada sem formulário; como Almoxarife, ver Depósitos só em consulta com o texto de orientação e (por API) 403 no POST

**Acceptance Criteria:**
- Given um Gestor, when abre Cadastros > Filiais e expande uma Filial, then cadastra e exclui os depósitos dela ali mesmo, sem escolher Filial
- Given um Almoxarife ou Usuário, when tenta criar ou excluir depósito (tela ou API), then é recusado (403)
- Given o item de menu Depósitos, when qualquer papel com acesso o abre, then vê a lista agrupada por Filial, sem formulário de cadastro
- Given `go build ./...`, `go test -p 1 ./...` (UMA suíte por vez), `npm run build` e `npx vitest run`, when rodados após a story, then terminam sem erro

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test ./handlers/... ./services/... -run 'Estoque|Deposito'` -- expected: build limpo, testes passando
- `cd backend && go test -p 1 ./...` -- expected: suíte completa limpa (Postgres de teste, porta 55432; confirme com `ps -eo cmd | grep '^go test' | grep -v grep | wc -l` que não há outra rodando)
- `cd frontend && npm run build && npx vitest run` -- expected: build limpo, suíte limpa

## Suggested Review Order

**Permissão (servidor é a autoridade)**

- POST/DELETE de estoques passam a exigir Gestor; Almoxarife/Usuário recebem 403
  [`main.go`](../../backend/main.go)

**Depósitos dentro da Filial**

- Componente novo: lista, adiciona e exclui depósitos de uma Filial, com confirmação e mensagens de erro
  [`DepositosDaFilial.tsx`](../../frontend/src/components/filiais/DepositosDaFilial.tsx)

- Botão "Depósitos (N)" por Filial, carga única e filtro por `filial_id`
  [`FiliaisSection.tsx`](../../frontend/src/components/filiais/FiliaisSection.tsx)

**Menu Depósitos (só consulta)**

- Lista agrupada por Filial, sem formulário nem Excluir, com texto de apoio por papel
  [`LocaisEstoqueSection.tsx`](../../frontend/src/components/estoques/LocaisEstoqueSection.tsx)
