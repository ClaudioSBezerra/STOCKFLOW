---
title: 'Histórico de alterações do Produto'
type: 'feature'
created: '2026-09-25'
status: 'done'
baseline_revision: '679a689f85b8aac0916dd3985f01b976f965a4ad'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** A 16.1 já grava `inativado`/`reativado` em `produto_historico`, mas a troca de nome (edição e renomear) não deixa rastro e nenhuma tela ou rota mostra o histórico; uma alteração indevida não pode ser identificada.

**Approach:** Gravar `nome_alterado` `{antes, depois}` na mesma transação da edição/renomear quando o nome muda de fato, expor `GET /api/produtos/{id}/historico` (`almoxarife`+) e mostrar a seção "Histórico do produto" no detalhe.

## Boundaries & Constraints

**Always:**
- Tabela, CHECK (`nome_alterado` já permitido) e índice já existem (migration 000051) — sem migration nova. Reusar `registrarHistoricoProdutoTx` (`services/produtos_inativacao.go`) com a nova constante `AcaoProdutoNomeAlterado = "nome_alterado"`.
- `AtualizarProduto` e `AtualizarNomeProduto` ganham o parâmetro `atorID string` (logo após `empresaID`); handlers passam `usuario.ID`. Compara o nome atual (lido sob `FOR UPDATE`) com o novo nome já trimado, comparação exata; igual → nada gravado. `AtualizarNomeProduto` passa a usar transação com `SELECT nome, template_id ... FOR UPDATE` (Produto não mesclado, `deleted_at IS NULL`) e grava o UPDATE + histórico juntos; validações e mensagens atuais não mudam.
- Serviço `ListarHistoricoProduto(db, empresaID, produtoID)` em `services/produtos_historico.go`: 404 (`ErrProdutoNaoEncontrado`) se o Produto não existe na Empresa (inclui id malformado e outra Empresa; inativo existe e é listado; mesclado, `deleted_at` preenchido, → 404); ordena `criado_em DESC, id DESC`; `LEFT JOIN usuarios` p/ `autor` (nome; vazio se ausente). Item JSON: `{id, acao, autor, detalhe (objeto), criadoEm}`; lista vazia = `[]`.
- Rota `GET /e/{slug}/api/produtos/{id}/historico` atrás de `RequireAuth` + `RequireRole(PapelAlmoxarife)`, em `main.go`, resposta `{"historico":[...]}`. `usuario` → 403.
- Frontend: seção "Histórico do produto" no `ProdutoDetalhePage.tsx`, só se `rankPapel(papel) >= rankPapel('almoxarife')` (`usuario` não faz o fetch). Renderiza: nome_alterado "«antes» → «depois»"; inativado "Inativado" + "Motivo: …" se houver; reativado "Reativado"; sempre autor e data/hora pt-BR. Recarrega junto com `carregarDetalhe` (evento SSE `produtos` já dispara). Erro de fetch: mensagem discreta, sem quebrar o detalhe. Vazio: "Nenhuma alteração registrada."
- `empresa_id` sempre do contexto; append-only (nenhuma rota de escrita/edição/exclusão do histórico).

**Block If:** —

**Never:** Migration/colunas novas. Histórico campo a campo de outras propriedades. EAN único (16.4) — não tocar `garantirEANLivreTx` nem mudar validações. Alterar inativar/reativar. Gravar histórico ao criar Produto.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Edição muda nome | PUT com nome novo | 1 linha `nome_alterado` `{antes,depois}`, autor, na mesma transação | — |
| Edição sem mudar nome | PUT com nome igual (outros campos mudam) | nenhuma linha gravada | — |
| Renomear muda nome | POST renomear | 1 linha `nome_alterado` | — |
| Renomear igual | mesmo nome | nenhuma linha | — |
| Falha de validação | nome fora do template | nome e histórico intactos | 400 como hoje |
| Listar | almoxarife+ | do mais recente ao mais antigo, com nome do autor | — |
| Papel usuario | GET historico | — | 403 |
| Outra Empresa / inexistente | GET historico | — | 404 sem revelar |

</intent-contract>

## Code Map

- `backend/services/produtos.go:532,595` -- `AtualizarNomeProduto` (hoje sem transação) e `AtualizarProduto` (transação, `FOR UPDATE` já lê `template_id, unidade_medida`: incluir `nome`); ponto do UPDATE/`Commit` no fim.
- `backend/services/produtos_inativacao.go:29,156` -- constantes de ação e `registrarHistoricoProdutoTx` (reuso).
- `backend/handlers/produtos.go:228,285` -- chamadas a alterar (passar `usuario.ID`; hoje descartam o usuário); molde de handler GET: `ObterProdutoHandler`.
- `backend/main.go:587-602` -- registro das rotas de Produto (`RequireRole`).
- `backend/services/catalogo.go:839` -- `ObterProdutoDetalhe` (molde de leitura escopada por Empresa).
- `backend/migrations/000051_add_inativacao_produtos.up.sql` -- schema de `produto_historico` (read-only).
- Testes-molde: `services/produtos_inativacao_test.go` (`criarProdutoInativacao`, leitura do histórico), `handlers/produtos_test.go:2394,2503` (`postInativacaoProduto`, `comEmpresa`); ~23 chamadas de teste existentes a `AtualizarProduto(`/`AtualizarNomeProduto(` precisam do novo argumento.
- `frontend/src/pages/ProdutoDetalhePage.tsx:278,281,356,940` -- `rankPapel`, `carregarDetalhe`, fetch de fotos (molde de fetch com `authHeaders()`/`apiUrl`/`seqRef`); seção nova entra depois do bloco de Fotos no Card. Teste: `ProdutoDetalhePage.test.tsx`.

## Tasks & Acceptance

**Execution:**
- `backend/services/produtos.go`, `produtos_inativacao.go` -- `atorID` e gravação de `nome_alterado` quando o nome muda -- rastro na mesma transação.
- `backend/services/produtos_historico.go` -- `ListarHistoricoProduto` -- leitura ordenada com autor.
- `backend/handlers/produtos.go`, `backend/main.go` -- passar o ator; `ListarHistoricoProdutoHandler` + rota `almoxarife`+.
- Testes Go (services e handlers, incluindo os 23 call sites atualizados) cobrindo toda a matriz, incluindo 403, 404 de outra Empresa e atomicidade (validação falha → sem linha).
- `frontend/src/pages/ProdutoDetalhePage.tsx` + vitest -- seção "Histórico do produto", oculta para `usuario` (sem fetch).

**Acceptance Criteria:**
- Given a edição ou renomear que muda o nome, when salva, then existe `nome_alterado` `{antes,depois}` com autor e horário; sem mudança, nada é gravado.
- Given um `almoxarife`+ no detalhe, when a seção carrega, then vê troca de nome, inativação (motivo) e reativação, do mais recente ao mais antigo, com autor e data/hora.
- Given um `usuario`, when abre o detalhe, then não vê a seção e a rota responde 403.
- Given Produto de outra Empresa, when o histórico é pedido, then 404.

## Spec Change Log

## Review Triage Log

### 2026-09-25 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 3: (high 0, medium 2, low 1)
- defer: 0
- reject: muitos (ruído: paginação/i18n/acessibilidade fora da intenção, colunas `NOT NULL` já garantem `detalhe`, `key={id}` já reinicia o estado, recarga do histórico já ocorre em `carregarDetalhe`, auditoria de outros campos fora do escopo)
- addressed_findings:
  - `[low]` `[patch]` UPDATE de `AtualizarNomeProduto` sem `deleted_at IS NULL` — predicado adicionado.
  - `[medium]` `[patch]` PUT que muda o nome sem teste de handler do autor — teste adicionado.
  - `[medium]` `[patch]` renomear Produto excluído/mesclado sem teste — teste adicionado.

### 2026-09-25 — Review pass (follow-up)
- intent_gap: 0
- bad_spec: 0
- patch: 3: (high 0, medium 0, low 3)
- defer: 0
- reject: muitos (paginação, i18n, histórico de outros campos/importação, renomear Produto inativo, ordem fotos→histórico, testes de wiring via `newMux` — fora da intenção ou ruído)
- addressed_findings:
  - `[low]` `[patch]` `detalhe` JSON `null` virava mapa nil no Go — normalizado para `{}`.
  - `[low]` `[patch]` front sem guarda para `detalhe` nulo — normalizado ao carregar.
  - `[low]` `[patch]` estado de erro do histórico sem teste — teste adicionado.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- sem erros
- `cd backend && go test -p 1 -timeout 25m ./services/... ./handlers/...` -- verde (com `DATABASE_URL`)
- `cd frontend && npx tsc -b && npx vitest run src/pages/ProdutoDetalhePage.test.tsx` -- verde

## Auto Run Result

Status: done

- Mudança: edição e renomear gravam `nome_alterado` `{antes, depois}` (mesma transação, só se o nome mudou); `GET /api/produtos/{id}/historico` (`almoxarife`+, 404 entre Empresas); seção "Histórico do produto" no detalhe, oculta para `usuario`.
- Arquivos: `backend/services/produtos.go`, `produtos_inativacao.go`, `produtos_historico.go` (novo), `handlers/produtos.go`, `main.go`, testes Go e `frontend/src/pages/ProdutoDetalhePage.tsx` (+ vitest).
- Revisão (passe de follow-up): patches 3 (0 high, 0 medium, 3 low), adiados 0, rejeitados muitos.
- Follow-up recomendado: false (patches: 0 high, 0 medium, 3 low; score 3).
- Verificação: `go build`/`go vet` OK; `npx tsc -b` e vitest do detalhe (66) verdes; testes Go de histórico sem `DATABASE_URL` neste ambiente (rodaram em 0,05s, provavelmente pulados) — verde no passe anterior com banco.
- Riscos residuais: histórico sem paginação (volume baixo esperado); nomes alterados por importação não geram histórico.
