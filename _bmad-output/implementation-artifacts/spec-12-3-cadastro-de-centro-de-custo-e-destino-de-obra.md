---
title: 'Story 12.3: Cadastro de Centro de Custo e Destino de Obra'
type: 'feature'
created: '2026-09-21'
status: 'done'
baseline_revision: '564056f804723c2cc7e827472874b22983bc32cd'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** O envio de Pedido só aceita obra/centro de custo como texto livre, sem lista padronizada por Empresa; o mesmo destino aparece escrito de vários jeitos (FR-51, AD-28).

**Approach:** Nova tabela `centros_custo` (cópia por Empresa, molde de `filiais`, Story 12.1) e `pedidos.centro_custo_id` NULLABLE. `adm`+ cadastra por API e por uma seção em Configurações; qualquer conta autenticada lista; `POST /api/pedidos` aceita `centro_custo_id` opcional, sempre revalidado contra a Empresa do contexto; o texto livre `obraCentroCusto` segue obrigatório e inalterado.

## Boundaries & Constraints

**Always:**
- Migration 000045 (+ `.down.sql`): `centros_custo(id UUID PK gen_random_uuid, empresa_id UUID NOT NULL FK empresas, nome VARCHAR(255) NOT NULL, criado_em TIMESTAMPTZ default now(), nome_normalizado` gerada como em `filiais`, índice único `(empresa_id, nome_normalizado)`, índice em `empresa_id`); `pedidos.centro_custo_id UUID NULL REFERENCES centros_custo(id)` + índice. Aditiva: sem backfill, nenhum Pedido existente alterado.
- `POST /e/{slug}/api/centros-custo` (`{nome}`) exige `RequireRole(PapelAdm)` (abaixo de `adm` → 403 pelo middleware); `GET /e/{slug}/api/centros-custo` só `RequireAuth` (quem envia Pedido precisa listar). Sempre escopado pela Empresa do contexto — `empresa_id` nunca vem de body/query. Nome: trim, obrigatório, ≤255 runes, sem byte NUL (400 `VALIDATION_ERROR`); nome normalizado duplicado na Empresa → 409 `CONFLICT` (colisão do índice, sem SELECT prévio). Resposta `201 {"centroCusto":{"id","nome"}}` / `200 {"centrosCusto":[...]}` ordenada por nome normalizado (slice vazio, nunca `null`). Sem editar/excluir (fora do escopo).
- `POST /api/pedidos` aceita `centro_custo_id` opcional (ausente, `null` ou só espaços = sem Centro de Custo). Informado: revalidado contra a Empresa do contexto ANTES de ler o carrinho ou escrever; inexistente, malformado (22P02) ou de outra Empresa → 400 `VALIDATION_ERROR` ("centro de custo inválido"), nada gravado, carrinho intacto. Válido: gravado em `pedidos.centro_custo_id` na mesma transação do Pedido; `Pedido` (resposta 201) ganha `centro_custo_id` (`null` quando ausente).
- `obraCentroCusto` (texto livre) continua obrigatório, com a mesma validação, mesmo com `centro_custo_id` válido — os dois coexistem, sem cópia entre eles.
- `services.SubmeterPedido` mantém a assinatura atual (delega com `centroCustoID` vazio) para não tocar os ~41 chamadores; novo `SubmeterPedidoComCentroCusto` recebe o id e é o que o handler chama.
- Frontend: seção "Centros de custo" (lista + formulário) em `ConfiguracoesPage`, montada só para `adm`+ (molde `FiliaisSection`); no diálogo "Enviar Pedido" do Carrinho, `<select>` OPCIONAL "Centro de custo cadastrado" (carregado de `GET /api/centros-custo` ao abrir; falha ou lista vazia → o `<select>` some, o envio segue só com o texto livre); `enviarPedido` ganha 4º parâmetro opcional e só envia `centro_custo_id` quando escolhido. O campo de texto continua obrigatório.

**Block If:** Nenhuma decisão bloqueante identificada.

**Never:** Não tornar `centro_custo_id` obrigatório; não migrar/inferir Centro de Custo de Pedidos históricos nem alterar `obra_centro_custo`; não editar/excluir Centro de Custo; não expor `centro_custo_id` nas listagens/detalhe/recibo de Pedido; não tocar Filiais (12.1/12.2), fotos de treinamento (12.4), `sprint-status.yaml`; `empresa_id` nunca de body/query.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Cadastrar | `adm`, `{nome:"Estoque do Cabo"}` | `201`, na Empresa do contexto | — |
| Papel insuficiente | `almoxarife`/`usuario` em `POST` | `403` | pelo middleware |
| Nome inválido/duplicado | vazio/>255/NUL; `"estoque do cabo "` já existe | `400` / `409` | envelope AD-14 |
| Mesmo nome, outra Empresa | idem em Empresa B | `201` | — |
| Listar | qualquer conta autenticada | só Centros da Empresa do contexto, por nome | — |
| Pedido sem Centro | `centro_custo_id` ausente/`null`/`"  "` | `201`, `centro_custo_id` `null`; comportamento de hoje | — |
| Pedido com Centro válido | id da própria Empresa + texto livre | `201`, gravado no Pedido e devolvido | — |
| Centro de outra Empresa | id existente em Empresa B | `400`, nenhum Pedido, carrinho intacto | `ErroPedidoValidacao` |
| Centro inexistente/malformado | UUID sem linha; `"abc"` | `400`, nada gravado | idem |
| Texto livre ausente | `centro_custo_id` válido, `obraCentroCusto` vazio | `400` (texto livre segue obrigatório) | idem |

</intent-contract>

## Code Map

- `backend/migrations/000044_create_filiais.{up,down}.sql` -- MOLDE (tabela por Empresa, `nome_normalizado` gerada, índice único); a nova é `000045_create_centros_custo`. `pedidos` nasce em `000026`.
- `backend/services/filiais.go` (+ `filiais_test.go`) -- MOLDE do service/erros/validação (`pqUniqueViolation`, `pqStringDataRightTruncation`, `pqInvalidByteSequence`, `pqInvalidTextRepresentation`). Novo `backend/services/centros_custo.go` (`CentroCusto`, `CriarCentroCusto`, `ListarCentrosCusto`, `ErrCentroCustoValidacao`, `ErrNomeCentroCustoDuplicado`, `validarCentroCustoDaEmpresa`).
- `backend/services/pedidos.go:172` -- `SubmeterPedido` (validações sem banco → `ListarCarrinho` → tx → `INSERT INTO pedidos` linha ~242); `Pedido` struct (linha 24). `ErroPedidoValidacao` (400) reaproveitado.
- `backend/handlers/filiais.go` (+ `filiais_test.go`) -- MOLDE dos handlers e do teste HTTP (`comEmpresa`, `criarContaComPapel`, `tokenDeLogin`). Novo `backend/handlers/centros_custo.go`. `backend/handlers/pedidos.go:30-64` -- `submeterPedidoRequest` + chamada do service.
- `backend/main.go:~432` -- registrar as rotas junto de Filiais; `backend/main_test.go` já testa 403 via `newMux` real para Filiais (molde).
- `frontend/src/components/filiais/FiliaisSection.tsx` (+ teste) -- MOLDE da nova `frontend/src/components/centroscusto/CentrosCustoSection.tsx`; `frontend/src/pages/ConfiguracoesPage.tsx:558` -- montar ao lado de `FiliaisSection` (gate `adm`+).
- `frontend/src/pages/CarrinhoPage.tsx:72-250` (diálogo de envio) + `CarrinhoPage.test.tsx` (mock de `useCarrinho`; `enviarPedidoMock` passa a receber 4º argumento); `frontend/src/lib/carrinho.tsx:92,243` (`enviarPedido`) + `carrinho.test.tsx`.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000045_create_centros_custo.{up,down}.sql` -- tabela, coluna e índices conforme contrato; down reverte -- schema aditivo
- `backend/services/centros_custo.go` (+ `centros_custo_test.go`) -- criar/listar/validar por Empresa; testes: validação, duplicidade, isolamento entre Empresas, listagem ordenada -- núcleo
- `backend/services/pedidos.go` (+ `pedidos_test.go`) -- `SubmeterPedidoComCentroCusto` + wrapper; `centro_custo_id` no `INSERT` e no `Pedido`; testes: sem Centro, Centro válido gravado, outra Empresa/inexistente/malformado → erro sem escrever e carrinho intacto, texto livre continua obrigatório -- coexistência e AD-20
- `backend/handlers/centros_custo.go` + `handlers/pedidos.go` + `backend/main.go` (+ testes HTTP, incl. 403 abaixo de `adm` via `newMux`) -- rotas, `centro_custo_id` no corpo, mapeamento de erros -- API
- `frontend/src/components/centroscusto/CentrosCustoSection.tsx`, `ConfiguracoesPage.tsx`, `CarrinhoPage.tsx`, `lib/carrinho.tsx` (+ testes) -- UI conforme contrato; testes: seção só para `adm`+, cadastro/409, `<select>` opcional e `centro_custo_id` só quando escolhido, some sem Centros/em erro -- UX

**Acceptance Criteria:**
- Given um `adm`+ autenticado, when cadastra um Centro de Custo com nome, then ele é criado na Empresa dele e aparece na listagem.
- Given o envio de um Pedido, when o solicitante preenche o texto livre de obra/centro de custo, then continua obrigatório e inalterado; when também escolhe um Centro cadastrado, then o Pedido guarda o `centro_custo_id`; sem escolha, o Pedido nasce como hoje (`centro_custo_id` nulo).
- Given um `centro_custo_id` de outra Empresa (ou inexistente/malformado), when o Pedido é enviado, then a resposta é 400 e nada é gravado.
- Given um Usuário abaixo de `adm`, when chama `POST /api/centros-custo`, then recebe 403.
- Given Pedidos anteriores à migration, when ela roda, then permanecem intactos (sem backfill).

## Spec Change Log

## Review Triage Log

## Design Notes

Chave `centro_custo_id` (snake) no corpo e na resposta segue o texto literal da story e o precedente `filial_id` (12.1), apesar de o restante de `Pedido` ser camelCase. Validação do Centro antes do carrinho/transação: Centro de Custo não tem editar/excluir nesta story, então não há janela de corrida entre checagem e `INSERT`; a FK garante apenas existência, a checagem por `empresa_id` garante o isolamento. `SubmeterPedido` fica como wrapper para não migrar ~41 chamadores de teste.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros e sem arquivos listados.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 -p 1 ./services/ -run 'CentroCusto|SubmeterPedido' && DATABASE_URL=... go test -count=1 -p 1 ./handlers/ ./ -run 'CentroCusto|SubmeterPedido|Pedido'` -- expected: passa.
- `cd frontend && npx tsc --noEmit -p . && npx vitest run src/pages/CarrinhoPage.test.tsx src/components/centroscusto src/lib/carrinho.test.tsx src/pages/ConfiguracoesPage` -- expected: passa.

## Auto Run Result

O `bmad-loop` (run `20260921-091348-b666`) parou com a story em `in-review`,
sem review independente sobre o diff. Recuperação manual (Claude, 2026-09-21):
`go build`/`go vet` limpos; `go test -count=1 -p 1 ./...` (Postgres real) `ok`
em todos os pacotes (services 319s, handlers 221s, cmd/*); frontend
(centroscusto, filiais, carrinho, CarrinhoPage, ConfiguracoesPage) 95/95 e
`npm run build` sem erros. Spot-check: `SubmeterPedidoComCentroCusto`
revalida `centro_custo_id` contra a Empresa do contexto antes de ler o
carrinho ou gravar; o texto livre `obra_centro_custo` segue obrigatório;
migration 000045 só cria `centros_custo` e a coluna nullable em `pedidos`.
**Sem review adversarial independente** — recomendo `bmad-code-review`.
