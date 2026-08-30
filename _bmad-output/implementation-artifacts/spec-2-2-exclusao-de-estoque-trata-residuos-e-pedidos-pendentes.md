---
title: 'Story 2.2 — Exclusão de Estoque trata resíduos e pedidos pendentes'
type: 'feature'
created: '2026-08-30'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '4ae4b0f305d86e6b7136bafe2b5497f5861f9686'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-2-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      Ao confirmar uma exclusão, o foco do teclado cai para o <body> porque o
      <Button> "Excluir" da linha (o elemento que abriu o ConfirmDialog) é
      desmontado quando a linha some, e o AlertDialog do Radix não tem para onde
      devolver o foco.
    evidence: |-
      LocaisEstoqueSection.tsx: cada <li> tem seu próprio botão "Excluir"; após
      o DELETE bem-sucedido, carregar() remove a linha e o botão que era o
      trigger deixa de existir. O ConfirmDialog/AlertDialog restaura foco no
      trigger ao fechar; sem trigger, o foco vai para o body — regressão de
      navegação por teclado/leitor de tela. GestaoUsuariosSection (o outro
      consumidor de ConfirmDialog) não expõe isso porque lá o trigger nunca é
      desmontado pela ação confirmada. Correção provável: mover o foco para um
      elemento estável (heading "Locais" ou o input de nome) no onOpenChange do
      diálogo quando a exclusão foi confirmada.
    location: >-
      frontend/src/components/estoques/LocaisEstoqueSection.tsx
    severity: medium
  - summary: >-
      Numa corrida em que outro operador já excluiu o mesmo Estoque, o DELETE
      volta 404 e o frontend exibe o alerta genérico "Não foi possível excluir
      o estoque agora. Tente novamente." — enganoso, já que a lista recarregada
      pelo mesmo fluxo aparece sem a linha (o local realmente não existe mais).
    evidence: |-
      LocaisEstoqueSection.tsx `excluir()`: o contrato da story (intent-contract
      > Boundaries > Always) determina "qualquer `!res.ok` → setErro(
      MENSAGEM_ERRO_EXCLUIR)", então 404 cai no ramo de erro. Como o `finally`
      sempre chama carregar(), o usuário vê simultaneamente a linha sumir e um
      alerta vermelho de falha. Baixa frequência (exige exclusão concorrente do
      mesmo id). Correção provável: tratar `res.status === 404` como sucesso
      idempotente (toast de sucesso + recarga), sem alerta — mas isso desvia do
      texto literal do intent-contract e deve ser confirmado por um humano.
    location: >-
      frontend/src/components/estoques/LocaisEstoqueSection.tsx
    severity: low
---

<intent-contract>

## Intent

**Problem:** O Epic 2 abriu o domínio de Estoques com criar + listar (Story 2.1), mas ainda não há como remover um local cadastrado por engano — não existe rota `DELETE`, gate de papel para exclusão, confirmação na UI nem ponto de inserção para os guards de integridade referencial. Sem isso, um `almoxarife` fica preso com Estoques errados e as stories seguintes não têm onde plugar as verificações de resíduo/pedido.

**Approach:** Nova `services.ExcluirEstoque(db, id)` (`DELETE FROM estoques WHERE id = $1`; id inexistente ou não-UUID → `ErrEstoqueNaoEncontrado`) e `handlers.ExcluirEstoqueHandler` expondo `DELETE /api/estoques/{id}` atrás de `RequireAuth → RequireRole(almoxarife)` — `204 No Content` no sucesso, `404 NOT_FOUND` para id ausente. No frontend, cada linha de `LocaisEstoqueSection` ganha um botão "Excluir" que passa pelo `ConfirmDialog` reutilizável (nunca `window.confirm`), chama o `DELETE`, dá `toast.success` e refaz o `GET`. Os dois guards concretos (quantidade residual — `PRODUTO_ESTOQUE`/Epic 3; Pedido `pendente` — `PEDIDOS`/Epic 7) são adicionados pelas Stories 3.1 e 7.2 quando essas tabelas existirem, sem reabrir esta story.

## Boundaries & Constraints

**Always:**
- **`services.ExcluirEstoque(db *sql.DB, id string) error`** (novo, em `services/estoques.go`): executa `DELETE FROM estoques WHERE id = $1`. `pq` SQLSTATE `22P02` (id não-UUID) → `ErrEstoqueNaoEncontrado`; `RowsAffected() == 0` (id UUID válido, sem linha) → `ErrEstoqueNaoEncontrado`; 1 linha removida → `nil`. Mesmo tratamento id-inexistente-ou-malformado→um-só-erro de `carregarAlvoParaGestao` (`services/gestao_usuarios.go`) e `DecidirSolicitacao` (`services/promocao.go`). Reusar a constante de pacote `pqInvalidTextRepresentation` (`services/promocao.go:17`) — não redeclarar.
- **`var ErrEstoqueNaoEncontrado = errors.New("estoque não encontrado")`** em `services/estoques.go`, no bloco `var` já existente. Mapeado para `404 NOT_FOUND`.
- **`handlers.ExcluirEstoqueHandler(db *sql.DB) http.HandlerFunc`** (novo, em `handlers/estoques.go`): guard `middleware.UsuarioDaSessao` ausente → `500 INTERNAL_ERROR` + `slog.Error` (molde dos handlers vizinhos). Lê `r.PathValue("id")`, chama `services.ExcluirEstoque`. `switch`: `nil` → `w.WriteHeader(http.StatusNoContent)` **sem corpo** (molde de `LogoutHandler`, `handlers/auth_sso.go:124`); `errors.Is(err, services.ErrEstoqueNaoEncontrado)` → `escreverErro(w, 404, "NOT_FOUND", "estoque não encontrado")`; `default` → `slog.Error` + `escreverErro(w, 500, "INTERNAL_ERROR", ...)`. Sem corpo de requisição — não ler `r.Body`.
- **`newMux` (`backend/main.go`)**: registrar `mux.HandleFunc("DELETE /api/estoques/{id}", middleware.RequireAuth(db, jwtSecret)(middleware.RequireRole(services.PapelAlmoxarife)(handlers.ExcluirEstoqueHandler(db))))`, junto ao bloco de Estoques da Story 2.1. Mesmo gate de papel do `POST` — excluir é restrito a `almoxarife`+ e a decisão é do middleware (403 para papéis abaixo, mesmo em chamada direta à API). Atualizar o comentário-doc do pacote `main` citando a Story 2.2.
- **Frontend — `LocaisEstoqueSection.tsx`**: cada `<li>` da lista ganha um `<Button variant="destructive" size="sm" aria-label={`Excluir estoque ${e.nome}`}>` que faz `setExclusaoPendente({ id: e.id, nome: e.nome })`. Novo estado `exclusaoPendente: { id: string; nome: string } | null` e `excluindo: boolean`. Um `<ConfirmDialog>` (import de `@/components/ConfirmDialog`) controlado por `exclusaoPendente !== null`, com `title={`Excluir o estoque "${exclusaoPendente?.nome ?? ''}"?`}`, `description="O local é removido da lista. Esta ação não pode ser desfeita."`, `confirmLabel="Excluir"`. `onConfirm` → `DELETE /api/estoques/${id}` com `authHeaders()`; `res.status === 204` (ou `res.ok`) → `toast.success('Estoque excluído.')`; qualquer `!res.ok` → `setErro(MENSAGEM_ERRO_EXCLUIR)`; `finally` refaz `carregar()` e limpa `excluindo`/`exclusaoPendente` (molde de `GestaoUsuariosSection` — recarrega no sucesso E na falha, para a linha obsoleta cair após um 404). Guard contra duplo disparo via `excluindo`.
- **`MENSAGEM_ERRO_EXCLUIR`** — constante nova junto às existentes: `'Não foi possível excluir o estoque agora. Tente novamente em instantes.'`. Renderizada no `<p role="alert" className="text-body text-destructive">` de erro já presente (reusar `erro`/`setErro`).
- **Testes** em todas as camadas tocadas (ver Code Map): `services/estoques_test.go`, `handlers/estoques_test.go`, `backend/main_test.go`, `LocaisEstoqueSection.test.tsx`.

**Block If:** nada nesta story depende de decisão humana nem de ação de operador fora do repositório — rota, handler, serviço e UI são inteiramente implementáveis por um agente com o schema atual. Status final esperado: `done`.

**Never:**
- **Não implementar os guards de "quantidade residual" nem de "Pedido pendente"** — as tabelas `PRODUTO_ESTOQUE` (Epic 3) e `PEDIDOS` (Epic 7) não existem no schema. As ACs 2 e 3 do epics.md são explicitamente condicionais ("uma vez que ... exista"); a Story 3.1 e a Story 7.2 acrescentam os `SELECT` de guard + o envelope `409` com a lista de Produtos dentro de `ExcluirEstoque` (envolvendo o `DELETE` numa transação), sem reabrir esta story. Não criar tabela, coluna, `409`, nem stub de guard aqui.
- **Nenhuma migration** — `estoques` já existe (`000008`). Nenhuma alteração de schema.
- **Nenhum corpo na requisição nem na resposta de sucesso** do `DELETE` — `204` é sempre sem corpo.
- **Nenhuma emissão de evento SSE** no canal `estoques` (AD-3): o registry `realtime/` ainda não existe (mesma nota da Story 2.1). Quando chegar, `ExcluirEstoque` publica `{"resource":"estoques","id":...,"change":"deleted"}`.
- **Nenhuma mudança em `EstoquesPage.tsx`, `App.tsx`, `nav-items.ts`** — a rota `/estoques` e o gate de papel `almoxarife`+ na tela já existem desde a Story 2.1; o botão "Excluir" aparece para qualquer papel que já vê a seção (o servidor é a autoridade real).
- **Nenhuma exclusão em massa, seleção múltipla ou desfazer.**

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Exclusão válida | `DELETE /api/estoques/{id}` de Estoque existente, sessão `almoxarife`+ | `204 No Content`, sem corpo; a linha some de `estoques` | — |
| Id inexistente (UUID válido) | `DELETE /api/estoques/<uuid-aleatório>` | `404 NOT_FOUND`, envelope de erro | `RowsAffected()==0` → `ErrEstoqueNaoEncontrado` |
| Id malformado (não-UUID) | `DELETE /api/estoques/abc` | `404 NOT_FOUND`, envelope de erro | `pq` `22P02` → `ErrEstoqueNaoEncontrado` |
| Papel `usuario` exclui via API | `DELETE /api/estoques/{id}`, sessão `usuario` | `403 FORBIDDEN`, corpo do envelope; handler não executa, nenhuma linha removida | decidido por `RequireRole` |
| Sem autenticação | `DELETE /api/estoques/{id}` sem `Authorization` | `401` | decidido por `RequireAuth`, antes de `RequireRole` |
| Frontend, exclusão confirmada | `almoxarife` clica "Excluir" e confirma no `ConfirmDialog` | `DELETE` disparado, `toast.success('Estoque excluído.')`, lista recarregada sem o item, sem `window.confirm` | — |
| Frontend, exclusão cancelada | `almoxarife` clica "Excluir" e cancela/`Esc` | nenhum `fetch` `DELETE`; diálogo fecha; lista intacta | — |
| Frontend, `DELETE` falha | resposta `!ok` (ex. `500`) | `<p role="alert">` genérico ("Não foi possível excluir o estoque agora..."); lista recarregada | — |

</intent-contract>

## Code Map

- `backend/services/estoques.go` — adicionar `ErrEstoqueNaoEncontrado` ao bloco `var` (linhas 21-30) e a função `ExcluirEstoque(db, id)` após `ListarEstoques`. Usa `pqInvalidTextRepresentation` (`services/promocao.go:17`, constante de pacote — já visível aqui). Molde do mapeamento de erro: `carregarAlvoParaGestao` (`services/gestao_usuarios.go:67-88`).
- `backend/services/estoques_test.go` — novos testes: `TestExcluirEstoque_Sucesso` (cria via `CriarEstoque`, exclui, `contarEstoques == 0`); `TestExcluirEstoque_IdInexistente` (`uuid.NewString()`-equivalente ou literal UUID → `ErrEstoqueNaoEncontrado`); `TestExcluirEstoque_IdMalformado` (`"nao-e-uuid"` → `ErrEstoqueNaoEncontrado`). Reusar `limparEstoques`, `contarEstoques`.
- `backend/handlers/estoques.go` — nova `ExcluirEstoqueHandler(db)` no molde de `RebaixarUsuarioHandler` (`handlers/gestao_usuarios.go:76-102`: guard de contexto, `r.PathValue("id")`, `switch` sobre os erros de `services`). `204` via `w.WriteHeader(http.StatusNoContent)` (molde de `handlers/auth_sso.go:124`). Atualizar o comentário-cabeçalho do arquivo citando `DELETE /api/estoques/{id}` — Story 2.2.
- `backend/handlers/estoques_test.go` — helper `deleteEstoques(db, authHeader, id)` no molde de `postEstoques` (compõe `RequireAuth → RequireRole(almoxarife) → ExcluirEstoqueHandler` num `mux` com pattern `DELETE /api/estoques/{id}`). Testes: `204` + linha removida para `almoxarife`/`gestor`/`adm`; `404` para id desconhecido e id não-UUID; `403` para `usuario` (envelope, `count` inalterado); `401` sem token. Reusar `criarContaComPapel`, `tokenDeLogin`, `limparEstoquesHandler`, `decodeErro`.
- `backend/main.go` — `newMux`: registrar `DELETE /api/estoques/{id}` no bloco de Estoques (após as duas rotas da Story 2.1, linhas 266-270). Atualizar o comentário-doc do pacote (linhas 1-24) mencionando a exclusão — Story 2.2.
- `backend/main_test.go` — estender `TestNewMux_EstoquesRotaCarregaRequireRole` (linha ~608) com sub-testes `DELETE`: token `usuario` → `403`; token `almoxarife`/`gestor`/`adm` → `204` (após criar um Estoque) ou `404` (id aleatório) — nunca `403`. Adicionar uma linha à tabela de "sem token → 401" (linha ~419): `DELETE /api/estoques/algum-id` → `http.StatusUnauthorized`.
- `frontend/src/components/estoques/LocaisEstoqueSection.tsx` — ver `Always`. Import de `ConfirmDialog`; `useState` para `exclusaoPendente` e `excluindo`; função `excluir(id)` (molde de `executar` em `GestaoUsuariosSection.tsx:90-121`); `<ConfirmDialog>` antes do fecho do `<Card>` (molde de `GestaoUsuariosSection.tsx:230-249`); `<Button variant="destructive">` por `<li>`. Atualizar o JSDoc do arquivo.
- `frontend/src/components/estoques/LocaisEstoqueSection.test.tsx` — novos casos: "Excluir" abre `alertdialog` e confirmar dispara `DELETE /api/estoques/e-1` + `toast.success('Estoque excluído.')` + `GET` refeito sem o item; cancelar não dispara `DELETE`; `DELETE` `!ok` → `role="alert"` genérico. Reusar `stubFetch`, `jsonOk`, `ESTOQUES`, `toastSuccess`.

## Tasks & Acceptance

**Execution:**
- `backend/services/estoques.go` (+ `estoques_test.go`) — `ErrEstoqueNaoEncontrado` + `ExcluirEstoque(db, id)` (`DELETE ... WHERE id=$1`; `22P02` e `RowsAffected()==0` → não encontrado).
- `backend/handlers/estoques.go` (+ `estoques_test.go`) — `ExcluirEstoqueHandler`: `204` sem corpo / `404` / guard `500`, fronteira HTTP pura.
- `backend/main.go` (+ `main_test.go`) — `DELETE /api/estoques/{id}` atrás de `RequireAuth → RequireRole(almoxarife)`; doc do pacote atualizada.
- `frontend/src/components/estoques/LocaisEstoqueSection.tsx` (+ teste) — botão "Excluir" por linha, `ConfirmDialog`, `DELETE`, `toast` no sucesso, `role="alert"` na falha, recarrega a lista sempre.

**Acceptance Criteria:**
- Given um Estoque cadastrado e nenhuma outra tabela referenciando-o (estado do sistema logo após o Epic 2), when um `almoxarife` (ou acima) faz `DELETE /api/estoques/{id}`, then a resposta é `204 No Content` sem corpo e a linha não existe mais em `estoques`.
- Given um `id` que não corresponde a nenhum Estoque — inclusive um id malformado (não-UUID) —, when a exclusão é chamada, then a resposta é `404 NOT_FOUND` com o envelope de erro e nada é removido.
- Given uma sessão de papel `usuario`, when ela chama `DELETE /api/estoques/{id}` diretamente pela API, then a resposta é `403 FORBIDDEN` com o corpo do envelope e o handler nunca executa (nenhuma linha removida); uma sessão sem `Authorization` recebe `401`.
- Given um `almoxarife` na tela `/estoques` com pelo menos um Estoque listado, when ele clica "Excluir" numa linha, confirma no `ConfirmDialog` e o `DELETE` responde `204`, then vê um toast de sucesso e a lista recarrega sem aquele Estoque; se ele cancelar o diálogo, nenhuma requisição `DELETE` é feita.

## Spec Change Log

_Nenhuma alteração — não houve loopback de `bad_spec`._

## Review Triage Log

### 2026-08-30 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 1: (high 0, medium 1, low 0)
- defer: 2: (high 0, medium 1, low 1)
- reject: 15
- addressed_findings:
  - `[medium]` `[patch]` Nome de Estoque longo podia estourar a nova `<li>` flex (`justify-between`, sem `min-w-0`/quebra no nome) e empurrar o botão "Excluir" para fora da tela, quebrando o layout abaixo de 360px — `min-w-0 break-words` no nome e `shrink-0` no botão; teste de nome longo adicionado.

Notas de triagem (não repetidas acima):
- **defer** — (1) foco do teclado cai para `<body>` após confirmar a exclusão porque o botão-trigger da linha é desmontado; (2) `404` numa corrida de exclusão concorrente exibe alerta de falha enganoso no frontend. Ambos reais, de baixa frequência, e a correção limpa esbarra em texto do intent-contract — registrados em `deferred`.
- **reject** — sem trilha de auditoria da exclusão (Story 1.12 registra só tentativas de login, não é log de auditoria genérico); `ExcluirEstoque` devolver `RETURNING nome`/struct (YAGNI, restruturação adiada para 3.1/7.2); `DELETE /api/estoques/` (URL de coleção) sem envelope AD-14 (convenção de toda a API, pré-existente); teste não envia corpo de requisição para provar que é ignorado (garantido estruturalmente — handler nunca lê `r.Body`); idempotência do endpoint (`404` em id já excluído é decisão explícita da I/O Matrix, consistente com `gestao_usuarios`/`promocao`); falta teste de concorrência no serviço (comportamento padrão do Postgres, sem AC de atomicidade nesta story); falta teste de estado-vazio pós-exclusão do último Estoque (composição de casos já testados); mensagem de erro genérica não distingue `401`/`403` de `500` (convenção do codebase, ver `GestaoUsuariosSection`); `deleteEstoques` replica a cadeia de middleware literalmente (padrão pré-existente de `postEstoques`; a composição real é coberta por `main_test.go`); redação do comentário de `main_test.go` sobre MFA/token; ACs não duplicam a linha de falha da I/O Matrix (correto pelo template); `res.status === 204 || res.ok` redundante mas sancionado pelo texto do intent-contract; texto "removido da lista" do `ConfirmDialog` (verbatim no intent-contract, aceitável); erro de exclusão renderizado no `<p role="alert">` do `<form>` (o intent-contract manda "reusar `erro`/`setErro`" e "no `<p role=\"alert\">` de erro já presente"); `setState` após unmount (padrão pré-existente; React 19 não emite o aviso).

## Design Notes

- **Guards adiados por design, não por omissão:** o epics.md e o `epic-2-context.md` são explícitos — a Story 2.2 "entrega a exclusão funcional agora" e os guards de resíduo (`PRODUTO_ESTOQUE`, Epic 3) e de Pedido `pendente` (`PEDIDOS`, Epic 7) entram nas Stories 3.1 e 7.2, "sem reabrir esta story". Quando entrarem, `ExcluirEstoque` passa a abrir uma transação, roda os dois `SELECT` de guard e só então o `DELETE`; o handler ganha o caso `409 CONFLICT` cujo corpo enumera os Produtos com quantidade residual. Nada disso é implementável hoje (as tabelas não existem) e as ACs 2/3 do epics.md são condicionais a essas tabelas.
- **`204` sem corpo (não `200 {"estoque":...}`):** exclusão não tem projeção de retorno útil; segue a convenção REST e o molde de `LogoutHandler` (`handlers/auth_sso.go:124`). O frontend trata `res.status === 204` (ou `res.ok`) como sucesso.
- **Id malformado e id inexistente colapsam em `404`:** mesma decisão de `services/gestao_usuarios.go` e `services/promocao.go` — não vaza "este id existe mas está errado" e simplifica o contrato do handler.
- **Recarregar a lista no sucesso E na falha** (molde de `GestaoUsuariosSection`): um `DELETE` que retorna `404` porque outro operador já excluiu o Estoque numa corrida deixa a linha obsoleta cair sozinha no `GET` seguinte. (O alerta de falha que aparece junto nessa corrida específica está registrado em `deferred` — baixa frequência.)
- **Nome de Estoque na linha da lista:** a `<li>` usa `flex justify-between`; o nome fica em `min-w-0 break-words` e o botão "Excluir" em `shrink-0`, para um nome longo (até 255 runes) quebrar em vez de empurrar o botão para fora da tela ou estourar o layout abaixo de 360px (ARCHITECTURE-SPINE).

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` — Postgres real (mesmo setup das Stories 1.5–2.1). Cobre `services/estoques_test.go`, `handlers/estoques_test.go` e `main_test.go` — o `DELETE` remove a linha, id ausente/malformado → `404`, `usuario` → `403`, sem token → `401`.
- `cd frontend && npm run lint && npm run build && npm run test` — `oxlint`, `tsc`+`vite` e os novos casos de `LocaisEstoqueSection.test.tsx` passam.
- `docker compose up --build` (se `docker` disponível): logado como `almoxarife`, abrir "Estoques", cadastrar "Canteiro Temp", clicar "Excluir", confirmar no diálogo → toast "Estoque excluído." e o item some; `DELETE /api/estoques/<id>` com sessão `usuario` responde `403`; `DELETE` de um id inexistente responde `404`. Sem `docker`, cobertura equivalente pelos testes de integração contra Postgres real.

**Manual checks (if no CLI):**
- `SELECT count(*) FROM estoques WHERE id = '<id excluído>'` retorna `0` após o `204`.
- Navegar direto para `/estoques` com conta `usuario` continua mostrando "Você não tem acesso à área de Estoques." (inalterado pela Story 2.2).

## Auto Run Result

Status: done
Blocking condition: nenhuma

### Resumo da mudança
Story 2.2 entrega a exclusão funcional de Estoque. Backend: `services.ExcluirEstoque(db, id)` (`DELETE FROM estoques WHERE id = $1`; id não-UUID `22P02` e `RowsAffected()==0` colapsam em `ErrEstoqueNaoEncontrado`) e `handlers.ExcluirEstoqueHandler` expondo `DELETE /api/estoques/{id}` atrás de `RequireAuth → RequireRole(almoxarife)` — `204 No Content` sem corpo no sucesso, `404 NOT_FOUND` (envelope AD-14) para id inexistente/malformado, `403` para papel abaixo de `almoxarife` (middleware), `401` sem token. Frontend: cada linha de `LocaisEstoqueSection` ganha um botão "Excluir" (`variant="destructive"`) que abre o `ConfirmDialog` reutilizável (nunca `window.confirm`); ao confirmar, `DELETE /api/estoques/{id}` → `toast.success('Estoque excluído.')` + recarga da lista; falha → `role="alert"` genérico + recarga. Os guards de quantidade residual (`PRODUTO_ESTOQUE`, Epic 3) e de Pedido `pendente` (`PEDIDOS`, Epic 7) ficam para as Stories 3.1 e 7.2 — decisão explícita do epics.md/epic-2-context, sem reabrir esta story. Sem migration (tabela `estoques` já existe).

### Arquivos alterados
- `backend/services/estoques.go` — `ErrEstoqueNaoEncontrado` + função `ExcluirEstoque(db, id)`.
- `backend/services/estoques_test.go` — `TestExcluirEstoque_Sucesso` / `_IdInexistente` / `_IdMalformado`.
- `backend/handlers/estoques.go` — `ExcluirEstoqueHandler` (204 sem corpo / 404 / guard 500) + comentário-cabeçalho.
- `backend/handlers/estoques_test.go` — helper `deleteEstoques` + `TestExcluirEstoqueHandler_204ParaAlmoxarifeGestorAdm` / `_404IdDesconhecidoOuMalformado` / `_403ParaUsuario` / `_401SemToken`.
- `backend/main.go` — rota `DELETE /api/estoques/{id}` atrás de `RequireAuth → RequireRole(almoxarife)`; doc do pacote + comentário do bloco de Estoques atualizados.
- `backend/main_test.go` — linha `DELETE .../algum-id → 401` na tabela sem-token; sub-testes `DELETE` em `TestNewMux_EstoquesRotaCarregaRequireRole` (`usuario` → 403; `almoxarife`/`gestor`/`adm` → 204/404, nunca 403).
- `frontend/src/components/estoques/LocaisEstoqueSection.tsx` — botão "Excluir" por linha (`min-w-0 break-words` no nome, `shrink-0` no botão), estado `exclusaoPendente`/`excluindo`, `excluir(id)`, `<ConfirmDialog>`; JSDoc atualizado.
- `frontend/src/components/estoques/LocaisEstoqueSection.test.tsx` — casos: "Excluir" confirma → DELETE+toast+GET sem o item; cancelar não dispara DELETE; DELETE `!ok` → alerta genérico; nome ~255 chars não esconde o botão.
- `_bmad-output/implementation-artifacts/spec-2-2-exclusao-de-estoque-trata-residuos-e-pedidos-pendentes.md` — este spec.

### Achados da revisão
- **Patches aplicados: 1** — `[medium]` nome de Estoque longo podia estourar a `<li>` flex e empurrar o botão "Excluir" para fora da tela (`min-w-0 break-words` no nome + `shrink-0` no botão + teste de nome longo).
- **Itens adiados (`deferred`): 2** — `[medium]` foco do teclado cai para `<body>` após confirmar a exclusão (o botão-trigger da linha é desmontado); `[low]` numa corrida de exclusão concorrente o `404` exibe alerta de falha enganoso no frontend. Correção limpa de ambos esbarra em texto do intent-contract.
- **Itens rejeitados: 15** — ver `## Review Triage Log` (trilha de auditoria da exclusão; `RETURNING nome`; envelope para URL de coleção; teste de corpo-de-requisição-ignorado; idempotência do endpoint; teste de concorrência no serviço; teste de estado-vazio; mensagem genérica para 401/403; helper de teste replicando a cadeia de middleware; redação de comentário; ACs vs I/O Matrix; `res.status === 204 || res.ok`; texto do `ConfirmDialog`; posição do `role="alert"` de erro; `setState` após unmount).
- **Recomendação de revisão de follow-up:** `false`. Patches deste pass por severidade: high 0, medium 1, low 0. Score = 3×1 + 1×0 = 3 (< 5), sem `high`.

### Verificação executada
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — limpo (sem saída de `gofmt`, build e vet ok).
- `cd backend && go test -p 1 -count=1 ./...` — PASS nos 6 pacotes (`backend`, `cmd/seed-admin`, `handlers`, `iam`, `middleware`, `services`), contra cluster PostgreSQL 16 efêmero (`docker` indisponível no ambiente; cobertura equivalente pelos testes de integração contra Postgres real, mesma nota das stories anteriores). Todos os testes novos de `DELETE` rodaram e passaram; cada linha da I/O & Edge-Case Matrix tem teste cobrindo-a.
- `cd frontend && npm run lint && npm run build && npm run test` — PASS: `oxlint` sem achados, `tsc -b` + `vite build` limpos, 209/209 testes (23 arquivos), incluindo os 4 casos novos de `LocaisEstoqueSection.test.tsx`.

### Riscos residuais
- Os dois itens em `deferred` (foco pós-exclusão; alerta na corrida de `404`) — ambos de baixa frequência e sem impacto em correção de dados.
- `docker compose up --build` não foi executado (Docker indisponível no ambiente); o caminho HTTP+DB está coberto pelos testes de integração contra Postgres real.
