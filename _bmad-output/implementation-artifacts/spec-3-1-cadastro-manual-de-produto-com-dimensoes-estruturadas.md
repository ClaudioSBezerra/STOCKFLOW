---
title: 'Story 3.1 — Cadastro manual de Produto com dimensões estruturadas'
type: 'feature'
created: '2026-08-30'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '75514934f3bf1b79b2f0db2d012b95a60fb12473'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      Nenhuma chamada ao banco (services/produtos.go, services/estoques.go) propaga
      *Context a partir de r.Context() do handler, então uma requisição cancelada/
      expirada não interrompe a transação em andamento nem libera locks antes do
      timeout do driver.
    evidence: |-
      Padrão pré-existente em todo o pacote services (CriarEstoque, ExcluirEstoque
      antes desta story, ListarUsuarios, etc.) — nenhum usa QueryContext/ExecContext/
      BeginTx com contexto. Esta story replica o padrão já estabelecido, não o
      introduz.
    location: >-
      backend/services/produtos.go, backend/services/estoques.go
    severity: low
  - summary: >-
      Componentes de seção que buscam dados no mount (CadastroProdutoSection e
      irmãos) não guardam contra setState após unmount durante um fetch em
      andamento.
    evidence: |-
      Mesmo padrão já presente em LocaisEstoqueSection.tsx (Story 2.1, pré-
      existente) — nenhuma seção do projeto usa um guard de "still mounted" nos
      efeitos de carregamento inicial. Baixo risco prático: são as primeiras telas
      carregadas após navegação, raramente desmontadas antes do fetch resolver.
    location: >-
      frontend/src/components/produtos/CadastroProdutoSection.tsx
    severity: low
---

<intent-contract>

## Intent

**Problem:** O catálogo hoje não tem nenhuma forma de cadastrar um Produto — não existem tabelas `categorias`, `produtos` nem `produto_estoque`. Sem elas, o Almoxarife não consegue popular o catálogo, e o guard de "quantidade residual" da exclusão de Estoque (Story 2.2) fica sem como ser verificado, porque `PRODUTO_ESTOQUE` ainda não existe.

**Approach:** Três migrations novas — `categorias` (tabela de seed, 25 linhas fixas do addendum §H), `produtos` (dimensões estruturadas valor+unidade, `categoria_id`) e `produto_estoque` (M:N Produto↔Estoque com quantidade). Novo `services/produtos.go` + `handlers/produtos.go` expõem `POST /api/produtos` (`RequireRole(almoxarife)`) e `GET /api/categorias` (`RequireAuth`). `services.ExcluirEstoque` ganha o guard de quantidade residual que a Story 2.2 deixou pendente, agora que `produto_estoque` existe. No frontend, a rota raiz (`/`, item de nav "Catálogo") deixa de ser `PlaceholderPage` e passa a ser `CatalogoPage`, empilhando (sem abas horizontais ainda — mesma simplificação de `ConfiguracoesPage`) um aviso de "busca em breve" (Epic 4) e a seção de cadastro, visível só a `almoxarife`+.

## Boundaries & Constraints

**Always:**
- **`categorias`** (`000010`): `id UUID PK DEFAULT gen_random_uuid()`, `codigo VARCHAR(10) NOT NULL UNIQUE`, `nome VARCHAR(255) NOT NULL UNIQUE`. Seed via `INSERT` no próprio `.up.sql` com as 25 linhas do addendum §H (fonte única, não digitável): `04.001` Materiais Civis, `04.002` Materiais Elétricos, `04.003` Materiais de Acabamentos/Cobertura, `04.004` Materiais de Instalações Especiais, `04.005` Materiais/Estruturas Metálicas, `04.006` Materiais Hidrossanitários, `04.007` Madeiramento, `05.001` EPI/EPC, `05.002` Medicina do Trabalho, `05.003` Fardamentos, `05.004` Programa de Segurança, `06.001` Materiais de Escritório, `06.002` Materiais de Limpeza, `07.001` Equipamentos/Máquinas Alugados, `07.002` Veículos Alugados, `08.001` Equipamentos/Máquinas Adquiridos, `08.002` Ferramentas Adquiridas (Imobilizado/Ativos), `08.003` Veículos Adquiridos, `09.001` Peças/Materiais para Equipamentos/Veículos/Máquinas, `10.001` Ferramentas Adquiridas (Consumo), `10.002` Ferramentas Alugadas, `11.001` Combustíveis e Lubrificantes, `12.001` Verbas, Licenças e Alvarás, `12.002` Impostos, `13.001` Equipamentos Esportivos e Recreativos.
- **`produtos`** (`000011`): `CREATE TYPE dimensao_unidade AS ENUM ('mm','cm','m')` + tabela `id UUID PK DEFAULT gen_random_uuid()`, `nome VARCHAR(255) NOT NULL`, `codigo VARCHAR(255) NULL` (sem unicidade nesta story — Story 3.4 decide o comportamento de "atualiza por código"; nomes duplicados são esperados e tratados pela Story 6.3, não pelo banco), `categoria_id UUID NOT NULL REFERENCES categorias(id)`, 5 pares `{campo}_valor NUMERIC(10,3) NULL` + `{campo}_unidade dimensao_unidade NULL` para `comprimento`, `largura`, `diametro`, `altura`, `espessura`, `observacoes TEXT NULL`, `criado_em TIMESTAMPTZ NOT NULL DEFAULT now()`. Sem `template_id` (Story 3.2 adiciona via `ALTER TABLE`) e sem `deleted_at` (Epic 6 adiciona quando a mesclagem existir) — nenhum dos dois é usado por esta story.
- **`produto_estoque`** (`000012`): `produto_id UUID NOT NULL REFERENCES produtos(id)`, `estoque_id UUID NOT NULL REFERENCES estoques(id)`, `quantidade NUMERIC(10,3) NOT NULL DEFAULT 0 CHECK (quantidade >= 0)`, `PRIMARY KEY (produto_id, estoque_id)`.
- **`services.CriarProduto`**: valida antes de qualquer escrita (nunca salva Produto parcial) — `nome` obrigatório (trim, 1..255 runes); `codigo`/`observacoes` opcionais (trim; vazio vira `NULL`); `categoria_id`/`estoque_id` obrigatórios (formato); `quantidade_inicial` obrigatória, `>= 0`; cada uma das 5 dimensões é opcional como PAR — se `valor` OU `unidade` vier preenchido sem o outro, erro nomeando o campo específico (ex. "largura: valor e unidade devem ser informados juntos") e nada é gravado; quando ambos vêm, `valor > 0` e `unidade` ∈ `{mm,cm,m}`. Sucesso: uma transação única faz `INSERT INTO produtos ... RETURNING id` seguido de `INSERT INTO produto_estoque (produto_id, estoque_id, quantidade) VALUES (...)`, commit. `categoria_id`/`estoque_id` inexistentes (violação de FK, `pq` SQLSTATE `23503`, nova constante `pqForeignKeyViolation` em `produtos.go`) → erro de validação (referência inválida), rollback, `400`.
- **`services.ListarCategorias(db) ([]Categoria, error)`**: `SELECT id, codigo, nome FROM categorias ORDER BY codigo ASC`.
- **`handlers.CriarProdutoHandler(db)`**: `RequireAuth → RequireRole(almoxarife)` em `newMux`; sucesso → `201 {"produto":{"id","nome"}}`; erro de validação (genérico ou de campo específico) → `400 VALIDATION_ERROR` com a mensagem específica do campo quando aplicável; `usuario` chamando direto → `403 FORBIDDEN` (decidido por `RequireRole`, handler nunca executa).
- **`handlers.ListarCategoriasHandler(db)`**: só `RequireAuth` (qualquer conta autenticada — mesmo padrão de `GET /api/estoques`); `200 {"categorias":[{"id","codigo","nome"},...]}`.
- **Guard de resíduo em `services.ExcluirEstoque`** (completa a Story 2.2, sem reabri-la): numa única transação, `SELECT p.nome FROM produto_estoque pe JOIN produtos p ON p.id = pe.produto_id WHERE pe.estoque_id = $1 AND pe.quantidade > 0 ORDER BY p.nome`; se houver linhas, `ROLLBACK` e devolver novo erro `*ErroEstoqueComResiduo{Produtos []string}` (nunca chega a fazer o `DELETE`); lista vazia → segue o `DELETE` já existente, mesma transação. `handlers.ExcluirEstoqueHandler` ganha um `case` com `errors.As` para `*services.ErroEstoqueComResiduo` → `409 CONFLICT`, `message` cita os nomes (ex. "estoque possui quantidade residual de: Tubo PVC 100mm, Cabo Elétrico X") — o envelope de erro é sempre `{"error":{"code","message"}}` (AD-14), sem campo extra; a lista entra na própria mensagem.
- **`newMux` (main.go)**: `mux.HandleFunc("POST /api/produtos", middleware.RequireAuth(db, jwtSecret)(middleware.RequireRole(services.PapelAlmoxarife)(handlers.CriarProdutoHandler(db))))`; `mux.HandleFunc("GET /api/categorias", middleware.RequireAuth(db, jwtSecret)(handlers.ListarCategoriasHandler(db)))`. Atualizar o comentário-doc do pacote citando a Story 3.1.
- **Frontend — `frontend/src/components/ui/select.tsx`** (novo): shadcn "new-york" `Select` sobre `import { Select as SelectPrimitive } from "radix-ui"` (mesmo molde de import de `alert-dialog.tsx`; pacote `radix-ui` já é dependência — nenhum `npm install` novo).
- **Frontend — `CatalogoPage`** (`frontend/src/pages/CatalogoPage.tsx`, substitui `PlaceholderPage` na rota índice `/`): sempre mostra um aviso "Busca e visualização do catálogo chegam em breve." (Epic 4, para todo papel); quando `rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife')`, também renderiza `<CadastroProdutoSection />` abaixo. Mesma simplificação deliberada de `ConfiguracoesPage` (sem abas horizontais do `AppShell` ainda).
- **Frontend — `CadastroProdutoSection`** (`frontend/src/components/produtos/CadastroProdutoSection.tsx`, novo): `Card` com formulário — `nome`/`codigo`/`observacoes` (`Input`), `categoria`/`estoque` (`Select`, carregados de `GET /api/categorias` e `GET /api/estoques` no mount), 5 pares dimensão (`Input` numérico + `Select` de unidade `mm/cm/m`, todos opcionais), `quantidade_inicial` (`Input` numérico). Submit → `POST /api/produtos`; sucesso → `toast.success('Produto cadastrado.')` (molde de `LocaisEstoqueSection`) e limpa o formulário; `400` de campo específico → `<p role="alert">` com a mensagem devolvida pelo servidor; outro erro → `<p role="alert">` genérico. Botão desabilitado durante o envio ou com `nome`/`categoria_id`/`estoque_id`/`quantidade_inicial` em branco.
- **Testes** em todas as camadas (ver Code Map); backend com Postgres real via `testDB(t)`, frontend com `vitest` + `fetch` mockado.

**Block If:** nada aqui depende de decisão humana ou de ação de operador fora do repositório — schema, endpoints e UI são inteiramente implementáveis por um agente. Status final esperado: `done`.

**Never:**
- **Nenhum campo/upload de foto** — Stories 3.5/3.6 dependem de um Produto já existente para ter onde anexar fotos.
- **Nenhum `template_id`/Nomenclatura Guiada** — Story 3.2, com sua própria migration `ALTER TABLE produtos ADD COLUMN template_id`.
- **Nenhuma emissão SSE** no canal `produtos` (AD-3) — `realtime/` não existe (mesmo precedente da Story 2.1); a Story 4.4 é onde isso primeiro é testado.
- **Nenhuma `MOVIMENTACOES`** para o `INSERT` inicial em `produto_estoque.quantidade` — AD-10 vincula só FR-14/15/25 (baixa, transferência, aprovação de Pedido), nunca FR-8/cadastro.
- **Nenhuma listagem/busca de Produtos** (`GET /api/produtos`) — é Epic 4; esta story só cria.
- **Nenhuma dimensão "lateral"** — AD-9 a cita, mas FR-8 e a AC desta story listam só comprimento/largura/diâmetro/altura/espessura; a AC testável é a autoridade aqui.
- **Nenhuma unicidade de `codigo` de Produto** nem de `nome` — duplicatas de nome são esperadas e tratadas pela Story 6.3, não pelo banco.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Cadastro válido completo | `POST /api/produtos`, sessão `almoxarife`+, todos os campos + as 5 dimensões pareadas | `201 {"produto":{"id","nome"}}`; 1 linha em `produtos` + 1 em `produto_estoque` com a quantidade inicial | — |
| Cadastro válido sem dimensões | idem, sem nenhuma dimensão informada | `201`; colunas de dimensão `NULL` | — |
| Dimensão com valor sem unidade | `largura: {"valor": 10}` | `400 VALIDATION_ERROR` citando "largura"; nada gravado (nem em `produtos` nem em `produto_estoque`) | envelope de erro |
| Dimensão com unidade sem valor | `altura: {"unidade": "cm"}` | `400 VALIDATION_ERROR` citando "altura" | idem |
| `categoria_id`/`estoque_id` inexistente | UUID válido mas sem linha correspondente | `400 VALIDATION_ERROR`; nada gravado | violação de FK (`23503`) mapeada |
| Papel `usuario` cadastra via API | `POST /api/produtos`, sessão `usuario` | `403 FORBIDDEN`; handler não executa, banco não é tocado | decidido por `RequireRole` |
| Categoria carregada no formulário | `GET /api/categorias`, qualquer sessão autenticada | `200` com as 25 categorias fixas, ordenadas por `codigo` | — |
| Exclusão de Estoque com resíduo | `DELETE /api/estoques/{id}`, `produto_estoque` tem linha com `quantidade > 0` para esse estoque | `409 CONFLICT`, mensagem cita os Produtos com resíduo; linha do estoque não é removida | novo `ErroEstoqueComResiduo` |
| Exclusão de Estoque sem resíduo | `produto_estoque` sem linha, ou só com `quantidade = 0`, para esse estoque | `204` (comportamento já existente da Story 2.2, agora exercitado de verdade) | — |
| Frontend, `usuario` acessa `/` | navegação direta | vê só o aviso de "busca em breve" — sem formulário de cadastro | — |
| Frontend, `almoxarife` cadastra com sucesso | preenche nome/categoria/estoque/quantidade e envia | `toast.success`, formulário limpo | — |
| Frontend, cadastro com dimensão inválida | envia só o valor de uma dimensão | `<p role="alert">` com a mensagem específica do servidor | — |

</intent-contract>

## Code Map

- `backend/migrations/000010_create_categorias.{up,down}.sql` (novos) — tabela + `INSERT` das 25 linhas (ver `Always`). `down`: `DROP TABLE IF EXISTS categorias`.
- `backend/migrations/000011_create_produtos.{up,down}.sql` (novos) — `CREATE TYPE dimensao_unidade` + tabela `produtos` (ver `Always`). `down`: `DROP TABLE IF EXISTS produtos; DROP TYPE IF EXISTS dimensao_unidade;`.
- `backend/migrations/000012_create_produto_estoque.{up,down}.sql` (novos) — tabela `produto_estoque` (ver `Always`). `down`: `DROP TABLE IF EXISTS produto_estoque`.
- `backend/services/produtos.go` (novo) — `type Produto struct{ ID, Nome string }`; `type Categoria struct{ ID, Codigo, Nome string }`; `type CriarProdutoInput struct{...}` (nome, código, categoria/estoque id, 5 `*DimensaoInput{Valor float64; Unidade string}`, quantidade inicial, observações); `type ErroEstoqueComResiduo struct{ Produtos []string }` com `Error() string` (usado por `estoques.go`, mesmo pacote); `const pqForeignKeyViolation = "23503"`; `CriarProduto(db, input)` (validação campo-a-campo, transação `INSERT produtos` + `INSERT produto_estoque`); `ListarCategorias(db)`.
- `backend/services/produtos_test.go` (novo) — sucesso completo e sem dimensões; cada uma das 5 dimensões testada isoladamente para "valor sem unidade"/"unidade sem valor"; `categoria_id`/`estoque_id` inexistente; `quantidade_inicial` negativa; `ListarCategorias` devolve as 25 linhas ordenadas.
- `backend/services/estoques.go` (editar) — `ExcluirEstoque` ganha o `SELECT` de resíduo dentro de uma transação antes do `DELETE` (ver `Always`); atualizar o comentário que hoje diz "Story 3.1... quando essas tabelas existirem" para refletir que o guard já está implementado.
- `backend/services/estoques_test.go` (editar) — novo teste: Estoque com `produto_estoque.quantidade > 0` → `ErroEstoqueComResiduo` com os nomes certos, nenhuma linha removida; Estoque com `quantidade = 0` → exclusão segue normalmente.
- `backend/handlers/produtos.go` (novo) — `criarProdutoRequest` (JSON, 5 pares `*dimensaoRequest{Valor *float64; Unidade *string}`); `CriarProdutoHandler(db)`, `ListarCategoriasHandler(db)`, molde de `estoques.go` (guard `UsuarioDaSessao`, `http.MaxBytesReader`, `switch` sobre erros de `services`).
- `backend/handlers/produtos_test.go` (novo) — via composição real (molde de `estoques_test.go`/`gestao_usuarios_test.go`): `almoxarife`+ com corpo válido → `201`; `usuario` → `403`; corpo com dimensão só com valor → `400` citando o campo; `GET /api/categorias` para qualquer papel autenticado → `200` com 25 itens.
- `backend/handlers/estoques.go` (editar) — `ExcluirEstoqueHandler` ganha `case errors.As(err, &residuo)` → `409 CONFLICT` com a mensagem citando os Produtos.
- `backend/handlers/estoques_test.go` (editar) — caso `DELETE` com resíduo → `409` e o Estoque continua existindo.
- `backend/main.go` (editar) — registra as duas rotas novas (ver `Always`); comentário-doc do pacote menciona a Story 3.1.
- `backend/main_test.go` (editar) — `TestNewMux_ProdutosRotaCarregaRequireRole` (molde de `TestNewMux_EstoquesRotaCarregaRequireRole`): `usuario` → `403`, `almoxarife` → não-`403` em `POST /api/produtos`.
- `frontend/src/components/ui/select.tsx` (novo) — ver `Always`.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx` (+ `.test.tsx`, novos) — ver `Always`.
- `frontend/src/pages/CatalogoPage.tsx` (+ `.test.tsx`, novos) — ver `Always`; teste cobre `usuario` (só aviso) e `almoxarife` (aviso + formulário).
- `frontend/src/App.tsx` (editar) — troca `{ index: true, element: <PlaceholderPage /> }` por `<CatalogoPage />`; importa `CatalogoPage`; comentário-doc do bloco de rotas atualizado (a raiz deixa de ser placeholder).
- `frontend/src/App.test.tsx` (editar) — novo teste "`/` renderiza `CatalogoPage` dentro do shell, não a `PlaceholderPage`" (molde do teste equivalente de `/configuracoes`, linha 239).

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000010-000012_*.{up,down}.sql` — `categorias` (+ seed), `produtos`, `produto_estoque`.
- `backend/services/produtos.go` (+ teste) — `CriarProduto`, `ListarCategorias`, `ErroEstoqueComResiduo`.
- `backend/services/estoques.go` (+ teste) — guard de resíduo em `ExcluirEstoque`.
- `backend/handlers/produtos.go` (+ teste) — `CriarProdutoHandler`, `ListarCategoriasHandler`.
- `backend/handlers/estoques.go` (+ teste) — `409` no guard de resíduo.
- `backend/main.go` (+ teste) — `POST /api/produtos` atrás de `RequireRole(almoxarife)`; `GET /api/categorias` só `RequireAuth`.
- `frontend/src/components/ui/select.tsx` — primitivo `Select` novo.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx` (+ teste) — formulário de cadastro.
- `frontend/src/pages/CatalogoPage.tsx` (+ teste) — substitui o placeholder da rota `/`.
- `frontend/src/App.tsx` (+ teste) — rota índice usa `CatalogoPage`.

**Acceptance Criteria:**
- Given um Almoxarife autenticado, when ele envia nome, categoria, estoque destino e quantidade inicial (com ou sem as 5 dimensões pareadas), then `201` com o Produto criado e uma linha em `produto_estoque` vinculando-o ao Estoque com a quantidade informada.
- Given uma dimensão com valor mas sem unidade (ou vice-versa), when o cadastro é enviado, then `400 VALIDATION_ERROR` citando o campo específico e nenhuma linha é gravada em `produtos` nem `produto_estoque`.
- Given uma sessão de papel `usuario`, when ela chama `POST /api/produtos` diretamente pela API, then `403 FORBIDDEN` e nada é gravado.
- Given a lista fixa de 25 Categorias, when o formulário carrega via `GET /api/categorias`, then a Categoria é selecionada dessa lista, nunca digitada livremente.
- Given um Estoque com quantidade residual de algum Produto (via `produto_estoque`), when um Almoxarife tenta excluí-lo, then `409 CONFLICT` citando os Produtos com resíduo, e o Estoque permanece; sem resíduo, a exclusão (Story 2.2) continua funcionando normalmente.

## Spec Change Log

## Review Triage Log

### 2026-08-30 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 7 (high 2, medium 2, low 3)
- defer: 2
- reject: 5
- addressed_findings:
  - `[high]` `[patch]` Guard de resíduo em `ExcluirEstoque` tinha janela de corrida (SELECT sem lock antes do DELETE): um `CriarProduto` concorrente podia inserir uma linha residual em `produto_estoque` entre o SELECT e o DELETE, e o `ON DELETE CASCADE` a apagaria junto do Estoque, sem violar nenhuma constraint. Corrigido com `SELECT ... FROM estoques WHERE id=$1 FOR UPDATE` no início da transação.
  - `[high]` `[patch]` `LocaisEstoqueSection.tsx` tratava o novo `409` de resíduo como qualquer outra falha (mensagem genérica de "tente novamente"), perdendo a mensagem específica citando os Produtos que o backend já devolve. Adicionado tratamento dedicado (molde do `409` de nome duplicado no cadastro) + teste.
  - `[medium]` `[patch]` `CriarProduto` não validava o tamanho de `codigo` (`VARCHAR(255)`) como já fazia com `nome` — um valor maior estourava a coluna e virava `500` em vez de `400`. Adicionada a mesma checagem de 255 runes.
  - `[medium]` `[patch]` Nenhuma checagem de limite superior em `valor` de dimensão / `quantidade_inicial` (`NUMERIC(10,3)`) — um valor muito grande estourava a coluna e virava `500`. Adicionado limite superior com `ErroProdutoValidacao`.
  - `[low]` `[patch]` Faltava índice em `produto_estoque(estoque_id)` para a consulta do guard de resíduo (só a PK composta existia, sem a coluna como líder). Adicionado `CREATE INDEX` na migration `000012`.
  - `[low]` `[patch]` Cobertura de teste incompleta: unidade fora de `{mm,cm,m}`, `valor <= 0` com unidade presente, e mensagem de `ErroEstoqueComResiduo` com mais de um Produto nunca eram exercitados. Testes adicionados.
  - `[low]` `[patch]` Comentário desatualizado em `ExcluirEstoque`: o branch de `pqInvalidTextRepresentation` no `DELETE` ficou inalcançável na prática (um `id` malformado já falha antes, no `SELECT` do guard) — comentário corrigido para não confundir mantenedores futuros.

### 2026-08-30 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 2 (high 0, medium 0, low 2)
- defer: 0
- reject: 12
- addressed_findings:
  - `[low]` `[patch]` A seção `## Design Notes` deste spec estava corrompida (cabeçalho e o começo do primeiro item perdidos, restando o fragmento solto ", não só na 3.7:**..."). Reconstruído o cabeçalho e o título do item ("Seed de `categorias` entregue já na Story 3.1, não só na 3.7:") a partir do texto explicativo remanescente e do padrão das specs irmãs (`spec-2-2-...md`).
  - `[low]` `[patch]` `CadastroProdutoSection.tsx` renderiza `erroCarregar` (`role="alert"`) quando `GET /api/categorias` ou `GET /api/estoques` falha no mount, mas nenhum teste exercitava esse branch. Adicionado teste cobrindo a falha de `/api/categorias`.

## Design Notes

- **Seed de `categorias` entregue já na Story 3.1, não só na 3.7:** o contexto do épico permite explicitamente "seed equivalente antes" da migração legada (Story 3.7); sem isso, a AC4 desta story (categoria de uma lista fixa) não seria verificável agora. A 3.7 encontra as linhas já lá e não as reinsere.
- **Unidade de dimensão como `enum {mm,cm,m}`:** AD-9 fixa o tipo como par `{valor: numeric, unidade: enum}`, mas nenhum documento de planejamento enumera os valores para dimensão (só para a unidade de quantidade em estoque, que é um campo diferente e fora do escopo de FR-8). `{mm,cm,m}` replica os exemplos do legado (`"6m"`, `"100mm"`) e nenhuma AC testa um valor fora desse conjunto — decisão de implementação, não gap de intenção.
- **Guard de resíduo dentro do envelope de erro fixo:** a AC pede que "a resposta lista quais Produtos ainda têm quantidade" mas o envelope `{"error":{"code","message"}}` não tem campo extra (AD-14); a lista entra na própria `message`.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` — Postgres real; migrations `000010`-`000012` aplicam sem erro; cobre `produtos_test.go`, `estoques_test.go` (guard) e `main_test.go`.
- `cd frontend && npm run lint && npm run build && npm run test` — `oxlint`, `tsc`+`vite` e os novos testes de `CatalogoPage`/`CadastroProdutoSection` passam.
- `docker compose up --build` — logado como `almoxarife`, cadastrar um Produto com categoria/estoque/quantidade (com e sem dimensões) e conferir o toast de sucesso; tentar excluir o Estoque usado → `409` citando o Produto; sessão `usuario` não vê o formulário em `/`.

**Manual checks (if no CLI):**
- `SELECT * FROM produto_estoque` após um cadastro mostra a linha com a quantidade inicial exata.
- `SELECT count(*) FROM categorias` = 25 logo após a migration `000010`.

## Auto Run Result

Status: done
Blocking condition: nenhuma

### Resumo da mudança
Story 3.1 entrega o cadastro manual de Produto com dimensões estruturadas. Backend: três migrations novas (`categorias` com seed fixo de 25 linhas, `produtos` com 5 pares valor+unidade de dimensão, `produto_estoque` M:N Produto↔Estoque); `services/produtos.go` (`CriarProduto` — validação completa antes de qualquer escrita, transação única `produtos`+`produto_estoque`; `ListarCategorias`) e `handlers/produtos.go` expondo `POST /api/produtos` (`RequireRole(almoxarife)`) e `GET /api/categorias` (`RequireAuth`); `services.ExcluirEstoque` ganha o guard de quantidade residual pendente da Story 2.2 (`SELECT ... FOR UPDATE` + checagem de `produto_estoque.quantidade > 0`, `409 CONFLICT` citando os Produtos). Frontend: rota raiz `/` deixa de ser `PlaceholderPage` e passa a ser `CatalogoPage` (aviso "busca em breve" + `CadastroProdutoSection` só para `almoxarife`+), novo primitivo `Select` (shadcn) e o formulário de cadastro completo.

### Arquivos alterados
- `backend/migrations/000010_create_categorias.{up,down}.sql` — tabela `categorias` + seed das 25 linhas.
- `backend/migrations/000011_create_produtos.{up,down}.sql` — `CREATE TYPE dimensao_unidade` + tabela `produtos`.
- `backend/migrations/000012_create_produto_estoque.{up,down}.sql` — tabela `produto_estoque` + índice em `estoque_id`.
- `backend/services/produtos.go` (+ `produtos_test.go`) — `CriarProduto`, `ListarCategorias`, `ErroProdutoValidacao`, `ErroEstoqueComResiduo`.
- `backend/services/estoques.go` (+ `estoques_test.go`) — guard de resíduo em `ExcluirEstoque` com `SELECT ... FOR UPDATE`.
- `backend/handlers/produtos.go` (+ `produtos_test.go`) — `CriarProdutoHandler`, `ListarCategoriasHandler`.
- `backend/handlers/estoques.go` (+ `estoques_test.go`) — `409 CONFLICT` no guard de resíduo.
- `backend/main.go` (+ `main_test.go`) — rotas `POST /api/produtos`/`GET /api/categorias`.
- `frontend/src/components/ui/select.tsx` — primitivo `Select` (shadcn "new-york").
- `frontend/src/components/produtos/CadastroProdutoSection.tsx` (+ teste) — formulário de cadastro.
- `frontend/src/pages/CatalogoPage.tsx` (+ teste) — substitui o placeholder da rota `/`.
- `frontend/src/App.tsx` (+ teste) — rota índice usa `CatalogoPage`.
- `frontend/src/components/estoques/LocaisEstoqueSection.tsx` (+ teste) — tratamento dedicado do `409` de resíduo.
- `_bmad-output/implementation-artifacts/epic-3-context.md` — contexto do épico compilado.
- `_bmad-output/implementation-artifacts/deferred-work.md` — 2 itens de baixa severidade herdados do `deferred` desta story.

### Achados da revisão (pass de 2026-08-30, follow-up)
- **Patches aplicados: 2** (ambos `low`) — reconstrução da seção `## Design Notes` deste spec, corrompida por uma edição anterior; teste novo cobrindo o branch de erro de carregamento (`erroCarregar`) em `CadastroProdutoSection.tsx`, antes sem cobertura.
- **Itens adiados (`defer`) nesta pass: 0.**
- **Itens rejeitados nesta pass: 12** — heading truncado em `deferred-work.md` (arquivo do orquestrador, fora do escopo desta sessão); nil-check inatingível em `dimensaoRequest.paraInput()` (código defensivo inofensivo, sem efeito observável); reuso de `authRequestMaxBytes` (64KB, convenção já estabelecida em todos os handlers do pacote, folga ampla para o payload de Produto); ausência de índice em `produtos.categoria_id`/`codigo` (nenhuma AC exige, sem consulta filtrada por esses campos ainda); risco de bloqueio do novo `SELECT ... FOR UPDATE` sem timeout (mesma causa-raiz já coberta pelo item `deferred` sobre ausência de propagação de `context.Context`); Produto órfão de estoque após exclusão com resíduo zerado (estado válido por design — Produto é entidade de catálogo, independente de ter estoque); coerção `Number()`/`NaN` no formulário (só alcançável burlando o `type="number"` do input, resultado seria `quantidade_inicial: 0`, valor já válido e testado); mensagem de erro não distingue FK inexistente de UUID malformado (o contrato só pede "erro de validação (referência inválida)" genérico); testes de handler não replicam toda a matriz de validação do service (o mapeamento de erro é genérico e já verificado uma vez); timeout de teste elevado para 15000ms em vez de diagnosticar a causa (prática pragmática documentada em comentário, sem efeito de correção); ausência de teste do `down` das migrations (convenção pré-existente em todas as stories anteriores; `golang-migrate` sempre reverte na ordem inversa, sem risco real de FK); `quantidade_inicial` ausente no JSON decodifica para `0` em vez de erro (leitura defensável, mas `0` já é valor de domínio válido e testado — sem consequência real).
- **Recomendação de revisão de follow-up:** `false`. Patches desta pass por severidade: high 0, medium 0, low 2. Score = 3×0 + 1×2 = 2 (< 5), sem `high`.

### Verificação executada
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — limpo (sem saída de `gofmt`, build e vet ok).
- `cd backend && go test -p 1 -count=1 ./...` — executado contra Postgres 16 real (instância local já provisionada com as migrations aplicadas, `DATABASE_URL` exportado manualmente nesta sessão pois não vinha definida no ambiente). 373 testes, 0 `SKIP`, 0 `FAIL`, nos 7 pacotes do módulo. Migrations `000010`-`000012` já aplicadas e íntegras.
- `cd frontend && npm run lint && npm run build && npm run test` — `oxlint` sem achados; `tsc -b && vite build` limpos; 25 arquivos de teste, 223/223 testes (incluindo o teste novo de `erroCarregar`).
- `SELECT count(*) FROM categorias` — confirmado `25`.
- `docker compose up --build` — **não executado**: `docker` indisponível neste ambiente (mesma limitação já registrada nas stories 2.1/2.2/2.3); coberto por equivalência pelos testes de integração contra o Postgres real acima (o fluxo HTTP completo — `POST /api/produtos`, `GET /api/categorias`, `409` de resíduo — está exercitado por `handlers/produtos_test.go`, `handlers/estoques_test.go` e `main_test.go`).

### Riscos residuais
- Os 2 itens em `deferred` no frontmatter (herdados desta mesma story: falta de propagação de `context.Context` nas chamadas a banco; falta de guard de unmount em `CadastroProdutoSection`) — ambos de baixa severidade, padrões pré-existentes no restante do codebase, não introduzidos por esta pass.
- `docker compose up --build` (verificação manual fim-a-fim via UI) não foi executado nesta sessão por indisponibilidade de `docker` no ambiente — mitigado pela cobertura de integração contra Postgres real listada acima.

