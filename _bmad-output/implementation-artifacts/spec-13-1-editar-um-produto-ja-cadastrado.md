---
title: 'Editar um Produto já cadastrado'
type: 'feature'
created: '2026-09-23'
status: 'done'
baseline_revision: '624dcbfead6e0d7d7055647d83e4ce1c4768f3b5'
review_loop_iteration: 0
followup_review_recommended: true
context: []
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** A única rota de edição de Produto (`POST /produtos/{id}/renomear`, só nome) nunca teve tela; para corrigir qualquer dado o Almoxarife precisa cadastrar outro Produto.

**Approach:** Novo `PUT /api/produtos/{id}` (`almoxarife`+) que valida com as mesmas regras do cadastro e regrava os campos editáveis; o detalhe passa a devolver `templateId`/`observacoes`; a tela de detalhe ganha o botão "Editar" abrindo um diálogo pré-preenchido.

## Boundaries & Constraints

**Always:** `empresa_id` só do contexto; produto de outra Empresa/inexistente/malformado/excluído → 404. Toda validação antes de qualquer escrita (falha não grava nada). Nome 10..255 runas, revalidado contra o template (Genérico aceita qualquer nome; erro mostra o formato esperado via `mensagemNomeForaDoTemplate`). Categoria pertence à Empresa. Sucesso publica `produtos`/`updated`. Reaproveitar os validadores de `services/produtos.go`.

**Block If:** —

**Never:** Editar/alterar Código, saldo/Lotes, reservas, Movimentações, fotos. Não remover `/renomear`. Sem migration.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Edição válida | almoxarife, todos campos válidos | 200 `{"produto":{id,nome,codigo}}`, campos gravados, Código/saldo intactos | — |
| Nome fora do template | template com formato, nome fora | 400 com formato esperado | nada gravado |
| Campo inválido | EAN-13 dígito errado / dimensão só valor / unidade fora da lista | 400 nomeando o campo | nada gravado |
| Legado sem template | `template_id` NULL, body sem `template_id` | 200, template segue NULL, sem exigir template | — |
| Template omitido em Produto com template | body sem `template_id` | mantém template atual e revalida o nome contra ele | — |
| Unidade vazia | Produto legado com unidade NULL | aceita (segue NULL); Produto que já tem unidade → 400 "unidade de medida é obrigatória" | — |
| Papel `usuario` | — | 403 | — |
| Outra Empresa / inexistente | — | 404 NOT_FOUND | — |

</intent-contract>

## Code Map

- `backend/services/produtos.go` -- `CriarProduto` (validação a reaproveitar: validarDimensao/validarEAN13/validarUnidadeMedida/validarTextoLivreOpcional/nomeValidoParaTemplate), `AtualizarNomeProduto` (molde de 404/template), `ErroProdutoValidacao`, `ErrProdutoNaoEncontrado`.
- `backend/handlers/produtos.go` -- `criarProdutoRequest` (corpo reutilizado), `CriarProdutoHandler`/`AtualizarNomeProdutoHandler` (molde do handler + `registro.Publish`).
- `backend/main.go` -- registro das rotas de produto (~L494, `renomear`); comentários de cabeçalho.
- `backend/services/catalogo.go` -- `ProdutoDetalhe`, `produtoDetalheQuery`, `ObterProdutoDetalhe` (acrescentar `templateId`, `observacoes`).
- `frontend/src/pages/ProdutoDetalhePage.tsx` -- `ProdutoDetalhe`, `podeRegistrarMovimentacao` (gate almoxarife+), `carregarDetalhe`, cabeçalho do Card.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx` -- `DimensaoField`, `montarDimensao`, `UNIDADES_MEDIDA` (extrair para reuso).

## Tasks & Acceptance

**Execution:**
- `backend/services/produtos.go` -- `AtualizarProduto(db, empresaID, id, input CriarProdutoInput)`: valida tudo antes, tx com `SELECT ... FOR UPDATE` (404), regra de template/unidade da matriz, `UPDATE` com `categorias` filtrada por Empresa -- edição completa sem tocar Código/saldo
- `backend/handlers/produtos.go` + `backend/main.go` -- `AtualizarProdutoHandler` + `PUT /e/{slug}/api/produtos/{id}` com `RequireRole(almoxarife)`, publica `updated`
- `backend/services/catalogo.go` -- detalhe devolve `templateId` e `observacoes`
- `frontend/src/lib/produtos.ts`, `components/produtos/DimensaoField.tsx` -- extrair constantes/helpers/`DimensaoField`; `CadastroProdutoSection` passa a importá-los
- `frontend/src/components/produtos/EditarProdutoDialog.tsx` -- diálogo pré-preenchido (Código somente-leitura, formato do template abaixo do nome, erro do servidor em `role="alert"`), `PUT`, sucesso fecha + toast + recarrega
- `frontend/src/pages/ProdutoDetalhePage.tsx` -- botão "Editar" só para almoxarife+
- Testes: `services/produtos_test.go`, `handlers/produtos_test.go`, `EditarProdutoDialog.test.tsx`, teste de página (botão oculto p/ `usuario`)

**Acceptance Criteria:**
- Given um almoxarife no detalhe, when clica "Editar", then vê formulário pré-preenchido com Código somente-leitura.
- Given nome fora do template / campos inválidos, when salva, then 400 com mensagem e nada é gravado.
- Given `usuario`, when tenta editar, then 403 na API e sem botão na tela.
- Given outra Empresa/inexistente, then 404.
- Given edição ok, then Código, saldo, reservas e Movimentações intactos e evento `produtos`/`updated` publicado.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test ./services/... ./handlers/...` -- expected: verde (requer Postgres de teste)
- `cd frontend && npm run lint && npx tsc -b && npm test` -- expected: verde

## Review Triage Log

### 2026-09-23 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 2 (high 0, medium 2, low 0)
- defer: 0
- reject: 12
- addressed_findings:
  - `[medium]` `[patch]` `templateId`/`observacoes` do detalhe sem asserção — adicionado `TestObterProdutoDetalhe_TemplateEObservacoes`.
  - `[medium]` `[patch]` ramos de template explícito, template/categoria inexistentes e nome fora do template escolhido sem teste — adicionado `TestAtualizarProduto_TemplateECategoriaExplicitos`.

### 2026-09-23 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 3 (high 0, medium 2, low 1)
- defer: 0
- reject: 20
- addressed_findings:
  - `[medium]` `[patch]` evento `produtos`/`updated` do PUT sem asserção (AC) — adicionado `TestAtualizarProdutoHandler_PublicaEventoUpdated` (assina o Registry; 400/404 não publicam, 200 publica com o id).
  - `[medium]` `[patch]` `RequireRole(almoxarife)` da rota real `PUT /api/produtos/{id}` em `main.go` sem teste via `newMux` — adicionado `TestNewMux_ProdutosPutRotaCarregaRequireRole` (usuario → 403 FORBIDDEN, almoxarife → 200).
  - `[low]` `[patch]` ponte corpo JSON → input do handler (template, 5 dimensões, código do fornecedor, EAN-13, embalagem, observações) e limpeza de opcionais para NULL sem teste — adicionado `TestAtualizarProdutoHandler_MapeiaCamposELimpaOpcionais`.

### 2026-09-23 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 3 (high 0, medium 2, low 1)
- defer: 0
- reject: 19
- addressed_findings:
  - `[medium]` `[patch]` pré-preenchimento de observações/código do fornecedor/EAN-13 sem teste de ida-e-volta (o PUT regrava tudo; um opcional perdido viraria NULL) — adicionado teste em `EditarProdutoDialog.test.tsx` que salva sem editar e compara o corpo inteiro com `toEqual` (mutação no prefill de observações faz o teste falhar).
  - `[medium]` `[patch]` filtro por Empresa de categoria/template no PUT só testado com UUIDs inexistentes — adicionado `TestAtualizarProduto_CategoriaTemplateDeOutraEmpresaEExcluido` com categoria e template reais de outra Empresa (mutação removendo `c.empresa_id = $20` faz o teste falhar).
  - `[low]` `[patch]` Produto excluído (soft delete) → 404 prometido no contrato sem teste — coberto no mesmo teste acima.

## Auto Run Result

Status: done

**Resumo:** `PUT /api/produtos/{id}` (almoxarife+) edita nome, template, categoria, unidade, embalagem, código do fornecedor, EAN-13, dimensões e observações com as regras do cadastro; o detalhe devolve `templateId`/`observacoes`; a tela de detalhe ganha "Editar" (só almoxarife+) com diálogo pré-preenchido. Esta execução foi mais uma revisão de acompanhamento sobre o spec já `done`; só acrescentou testes.

**Arquivos (implementação, commits anteriores):**
- `backend/services/produtos.go` — `AtualizarProduto` (validação prévia, tx com FOR UPDATE, Código/saldo intactos).
- `backend/handlers/produtos.go`, `backend/main.go` — handler e rota PUT, publica `produtos`/`updated`.
- `backend/services/catalogo.go` — detalhe com `templateId`/`observacoes`.
- `frontend/src/lib/produtos.ts`, `components/produtos/DimensaoField.tsx` — helpers extraídos do cadastro.
- `frontend/src/components/produtos/EditarProdutoDialog.tsx` (+ teste), `pages/ProdutoDetalhePage.tsx` (+ teste), `CadastroProdutoSection.tsx` (imports).

**Arquivos (esta passada, só testes):**
- `backend/services/produtos_test.go` — `TestAtualizarProduto_CategoriaTemplateDeOutraEmpresaEExcluido`.
- `frontend/src/components/produtos/EditarProdutoDialog.test.tsx` — teste "salvar sem editar reenvia todos os opcionais pré-preenchidos".

**Revisão (follow-up):** 3 patches aplicados (0 high, 2 medium, 1 low), 0 adiados, 19 rejeitados. Rejeitados por serem decisões de design já registradas nas passadas anteriores ou não reproduzíveis: last-write-wins/sem lock otimista, sem trilha de auditoria/`updated_at`, template não removível pela UI, lookup do template atual sem `empresa_id` (templates `empresa_id NULL` referenciados por Produto são adotados pelo backfill da Story 9.4; o cadastro usa o mesmo filtro), erro pós-salvar em diálogo fechado (`carregarDetalhe` nunca rejeita), categoria validada dentro do próprio `UPDATE` (atômico, nada é gravado), Código mantido ao trocar de categoria (Código é sequencial por Empresa, não derivado da categoria), NaN em dimensão (helper extraído sem mudança do cadastro; input numérico), reuso de `podeRegistrarMovimentacao` (mesmo gate almoxarife+), itens cosméticos.
**Follow-up recomendado:** true (patches desta passada: 0 high, 2 medium, 1 low; score 3×2 + 1 = 7 ≥ 5).

**Verificação:** `go build ./...` e `go vet ./...` ok. `go test -p 1 -count=1 -v` com `DATABASE_URL` no banco de teste `stockflow_t91` para `TestAtualizarProduto*`, `TestCriarProduto*`, `TestObterProdutoDetalhe*`, `TestAtualizarNomeProduto*`, `TestNewMux_Produtos*` (services, handlers, main) — todos verdes. A suíte completa `./services/... ./handlers/...` estourou o limite de 10 min e não terminou nesta passada. Os dois testes novos foram checados por mutação (filtro `c.empresa_id` removido / prefill de observações zerado → falham; código restaurado). Frontend: `tsc -b` limpo; vitest 59 arquivos / 766 testes verdes; `npm run lint` falha só em erros já existentes em outros arquivos (nenhum em `EditarProdutoDialog*`).

**Riscos residuais:** last-write-wins em edições concorrentes (inclusive PUT × `/renomear`); template/unidade não podem ser limpos depois de definidos; o caminho "template omitido mantém o atual" só é alcançável pela API; rota `/renomear` mantida; suíte completa do backend não rodada.
