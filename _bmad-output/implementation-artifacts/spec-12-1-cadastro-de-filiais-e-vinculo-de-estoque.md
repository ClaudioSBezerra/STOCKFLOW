---
title: 'Story 12.1: Cadastro de Filiais e vínculo de Estoque'
type: 'feature'
created: '2026-09-21'
baseline_revision: 'ff82e0e44f16929ea141a56e6de68f5b92981977'
status: done
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred:
  - summary: >-
      `CriarEstoque` devolve 500 (não 400) quando o nome contém byte NUL/sequência inválida (SQLSTATE 22021); `CriarFilial` já trata esse caso.
    evidence: |-
      Comportamento pré-existente do cadastro de Estoque (Story 2.1), sem tratamento de `pqInvalidByteSequence`.
    location: >-
      backend/services/estoques.go
    severity: low
  - summary: >-
      Etapas de `cmd/migrate-legado` (produtos, pedidos, movimentações) montam mapa nome→id de Estoque assumindo nome único por Empresa; agora a unicidade é por Filial.
    evidence: |-
      `SELECT nome_normalizado, id FROM estoques WHERE empresa_id = $1` em produtos.go:392, pedidos.go:258 e movimentacoes.go:236; com homônimos em Filiais distintas um sobrescreve o outro. Na prática o corte grava tudo na Filial padrão.
    location: >-
      backend/cmd/migrate-legado/{produtos,pedidos,movimentacoes}.go
    severity: low
  - summary: >-
      O `<select>` de Filial em "Locais" só carrega no mount; Filial recém-criada em `FiliaisSection` só aparece após recarregar a página.
    evidence: |-
      Componentes irmãos em `ConfiguracoesPage`/`EstoquesPage` sem estado compartilhado.
    location: >-
      frontend/src/components/estoques/LocaisEstoqueSection.tsx
    severity: low
---

<intent-contract>

## Intent

**Problem:** Estoques (Depósitos) são uma lista plana por Empresa; a organização física por Filial não existe no sistema (FR-51, AD-27), e o nome do Estoque é único na Empresa inteira.

**Approach:** Nova tabela `filiais` (escopo Empresa) e `estoques.filial_id` NULLABLE (aditivo, AD-20). Todo Estoque NOVO exige Filial; a unicidade de nome passa a ser `(filial_id, nome_normalizado)`; `ProvisionarEmpresa` cria a Filial padrão (nome = Nome Fantasia) na mesma transação; `adm`+ cadastra Filiais por API e por uma seção na tela de Configurações; a tela "Locais" ganha o seletor de Filial. Backfill dos Estoques legados e `SET NOT NULL` são da Story 12.2 — fora daqui.

## Boundaries & Constraints

**Always:**
- Migration 000044 (+ `.down.sql`): `filiais(id UUID PK gen_random_uuid, empresa_id UUID NOT NULL FK empresas, nome VARCHAR(255) NOT NULL, criado_em TIMESTAMPTZ default now(), nome_normalizado` gerada como em `estoques`, índice único `(empresa_id, nome_normalizado)`, índice em `empresa_id`); `estoques.filial_id UUID NULL REFERENCES filiais(id)` + índice; troca `idx_estoques_nome_normalizado` de `(empresa_id, nome_normalizado)` para `(filial_id, nome_normalizado)` (mesmo nome de índice). Sem backfill, sem `NOT NULL`, nenhuma linha existente alterada.
- `POST /e/{slug}/api/filiais` (`{nome}`) exige `RequireRole(PapelAdm)` (abaixo de `adm` → 403 pelo middleware); `GET /e/{slug}/api/filiais` só `RequireAuth` (o almoxarife precisa listar para escolher). Sempre escopado pela Empresa do contexto — `empresa_id` nunca vem de body/query. Nome: trim, obrigatório, ≤255 runes (400 `VALIDATION_ERROR`); nome normalizado duplicado na Empresa → 409 `CONFLICT` (colisão do índice, sem SELECT prévio). Resposta `201 {"filial":{"id","nome"}}` / `200 {"filiais":[...]}` ordenada por nome normalizado. Sem editar/excluir Filial (fora do escopo).
- `services.CriarEstoque` passa a receber `filialID` e o exige: vazio, malformado ou de outra Empresa → `ErrFilialInvalida` → 400 `VALIDATION_ERROR` (revalida contra a Empresa do contexto na própria inserção, sem `SELECT` prévio). `POST /api/estoques` aceita `{nome, filial_id}`. Duplicidade agora por `(filial_id, nome_normalizado)` → 409; mesmo nome em Filiais diferentes é permitido.
- `Estoque` (POST/GET `/api/estoques`) ganha `filial_id` e `filial_nome` (JSON `null` para Estoque legado sem Filial, até a 12.2). `ListarEstoques` continua listando todos (LEFT JOIN).
- `ProvisionarEmpresa` insere a Filial padrão (nome = `NomeFantasia`) na MESMA transação, junto de listas/contador; nunca lazy-init. Vale também para a Empresa de Treinamento.
- Demais caminhos que criam Estoque passam a atribuir Filial: `semearDadosTreinamento` usa a Filial padrão da própria Empresa; `encontrarOuCriarEstoque` (importação) busca o nome na Empresa (havendo homônimos, prefere a Filial padrão) e, se não existe, cria na Filial padrão da Empresa (`ON CONFLICT (filial_id, nome_normalizado)`, re-SELECT em instrução própria como hoje); `cmd/migrate-legado` cria na Filial padrão da Empresa-alvo. "Filial padrão" = a mais antiga da Empresa (`ORDER BY criado_em, id`). Empresa sem nenhuma Filial (legada, antes da 12.2) e Estoque a criar → erro claro (`ErrEmpresaSemFilial`; na importação vira erro da linha; no `migrate-legado` aborta antes de escrever), nunca Estoque órfão.
- Frontend: seção "Filiais" (lista + formulário) em `ConfiguracoesPage`, montada só para `adm`+ (molde de `CategoriasSection`); `LocaisEstoqueSection` ganha `<select>` obrigatório de Filial (carrega `GET /api/filiais`), envia `filial_id` e mostra a Filial em cada linha (Estoque legado: "—").

**Block If:** Nenhuma decisão bloqueante identificada.

**Never:** Não fazer backfill, `SET NOT NULL` nem índice legado (Story 12.2); não tocar Centro de Custo/Pedidos (12.3), fotos de treinamento (12.4), `sprint-status.yaml`; não criar terceiro nível de hierarquia nem tornar Filial unidade de isolamento (isolamento continua só por Empresa); não alterar `ExcluirEstoque`; nenhum `eh_treinamento` em services.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Cadastrar Filial | `adm`, `{nome:"Recife"}` | `201`, Filial na Empresa do contexto | — |
| Papel insuficiente | `almoxarife`/`usuario` em `POST /api/filiais` | `403` | pelo middleware |
| Nome de Filial inválido/duplicado | vazio/>255; `"recife "` já existe | `400` / `409` | envelope AD-14 |
| Estoque com Filial | `{nome, filial_id}` válido | `201` com `filial_id`/`filial_nome` | — |
| Estoque sem/errada Filial | `filial_id` ausente, malformado ou de outra Empresa | `400`, nada gravado | `ErrFilialInvalida` |
| Mesmo nome, Filiais distintas | "Central" em A e em B | ambos `201` | — |
| Mesmo nome, mesma Filial | "central" e "Central" | 2º → `409` | — |
| Empresa nova | `ProvisionarEmpresa` | Filial com nome = Nome Fantasia, atomicamente | falha desfaz tudo |
| Estoque legado | `filial_id` NULL | listado normalmente, `filial_id`/`filial_nome` `null` | — |

</intent-contract>

## Code Map

- `backend/migrations/000043_*` -- último número; a nova é `000044_create_filiais`. Molde de coluna gerada/índice: `000008`; molde aditivo/`NULLS NOT DISTINCT`: `000032` (o índice `idx_estoques_nome_normalizado` mora ali).
- `backend/services/estoques.go` -- `Estoque`, `CriarEstoque(db, empresaID, nome)` (INSERT com 23505), `ListarEstoques`; `ExcluirEstoque` inalterado.
- `backend/services/filiais.go` (novo) -- `Filial`, `CriarFilial`, `ListarFiliais`, `filialPadraoDaEmpresa`, `ErrFilialInvalida`, `ErrEmpresaSemFilial`, `ErrFilialValidacao`, `ErrNomeFilialDuplicado`; molde `services/categorias.go` e `estoques.go`.
- `backend/services/empresas.go:415` -- `ProvisionarEmpresa` (adicionar INSERT da Filial padrão; `NomeFantasia` em `Empresa`).
- `backend/services/empresas_plataforma.go:282` -- `semearDadosTreinamento` (estoques de exemplo precisam de `filial_id`).
- `backend/services/importacoes.go:~957` -- `encontrarOuCriarEstoque` (ON CONFLICT hoje em `(empresa_id, nome_normalizado)`).
- `backend/cmd/migrate-legado/main.go:617` -- INSERT de estoque legado (+ testes do cmd).
- `backend/handlers/estoques.go`, `backend/handlers/filiais.go` (novo) -- request `filial_id`, mapeamento de erros; molde `handlers/categorias.go`.
- `backend/main.go:~420` -- registrar `POST`/`GET /e/{slug}/api/filiais` junto de estoques.
- `backend/{services,handlers,.}/*_test.go` -- ~212 chamadas a `CriarEstoque` e 8 `INSERT INTO estoques` em testes; TRUNCATE de testes não cobre `filiais` (a Filial de `empresa-teste` sobrevive). Usar helper por pacote (`filialTeste(t, db, empresaID)`: devolve a Filial padrão da Empresa, criando-a se faltar — a Empresa de teste pode ter sido provisionada antes da migration) e migrar as chamadas.
- `frontend/src/components/estoques/LocaisEstoqueSection.tsx` (+ `.test.tsx`), `frontend/src/components/filiais/FiliaisSection.tsx` (novo, + teste), `frontend/src/pages/ConfiguracoesPage.tsx:554` (gate `adm`+ ao lado de `CategoriasSection`).

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000044_create_filiais.{up,down}.sql` -- tabela, coluna, índices conforme contrato; down reverte (restaura o índice `(empresa_id, nome_normalizado) NULLS NOT DISTINCT`) -- schema aditivo
- `backend/services/filiais.go` + `estoques.go` + `empresas.go` + `empresas_plataforma.go` + `importacoes.go` -- regras de Filial, `CriarEstoque` com `filialID`, Filial padrão no provisionamento, seed de treinamento e importação com Filial -- nenhum Estoque novo sem Filial
- `backend/cmd/migrate-legado/main.go` -- estoques legados na Filial padrão da Empresa-alvo; aborta sem Filial -- idem
- `backend/handlers/{filiais,estoques}.go` + `backend/main.go` -- rotas e mapeamento de erros -- API
- `backend/**/*_test.go` -- helper de Filial de teste; migrar as chamadas; novos testes cobrindo a I/O Matrix (service e HTTP: 403, 400, 409, mesmo nome em Filiais distintas, Filial de outra Empresa, provisionamento atômico com Filial = Nome Fantasia, Estoque legado NULL listado, importação/seed com Filial padrão) -- regressão e cobertura
- `frontend/src/components/filiais/FiliaisSection.tsx`, `LocaisEstoqueSection.tsx`, `ConfiguracoesPage.tsx` (+ testes) -- UI conforme contrato; testes: seção só para `adm`+, cadastro/409, seletor obrigatório e `filial_id` no POST, coluna Filial -- UX

**Acceptance Criteria:**
- Given um `adm`+ autenticado, when cadastra uma Filial com nome, then ela é criada na Empresa dele e aparece na listagem.
- Given o cadastro de um Estoque novo, when não informa Filial válida da própria Empresa, then é rejeitado; when informa, then nasce vinculado a ela.
- Given dois Estoques com o mesmo nome em Filiais diferentes da mesma Empresa, when cadastrados, then ambos são aceitos; na mesma Filial, o segundo dá 409.
- Given uma Empresa nova (inclusive Treinamento), when provisionada, then existe uma Filial com o Nome Fantasia e nenhum Estoque semeado nasce sem Filial.
- Given um Usuário abaixo de `adm`, when chama `POST /api/filiais`, then recebe 403.
- Given Estoques legados sem Filial, when o migration roda, then continuam listados e operáveis (nada é apagado ou alterado).

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 7 (high 0, medium 3, low 4)
- defer: 3 (low 3)
- reject: ~35
- addressed_findings:
  - `[medium]` `[patch]` Aviso em "Locais" quando a Empresa não tem nenhuma Filial (formulário travado sem explicação) — `<p role="alert">` + teste.
  - `[low]` `[patch]` 400 do POST /api/estoques mostra a mensagem do servidor — + teste.
  - `[medium]` `[patch]` Teste de importação (criação e atualização) em Empresa sem Filial: linha rejeitada com `ErrEmpresaSemFilial`, nada gravado.
  - `[medium]` `[patch]` Teste de `encontrarOuCriarEstoque` com Estoque legado `filial_id` NULL (com e sem Filial na Empresa).
  - `[low]` `[patch]` Teste de `migrarEstoques` sem Filial com tudo já mapeado (sem erro).
  - `[medium]` `[patch]` Teste via `newMux` real: 403 para papéis abaixo de `adm` em `POST /api/filiais`.
  - `[low]` `[patch]` Asserção de `filial_nome` igual ao nome da Filial escolhida (service e handler).
- Rejeitados (resumo): unicidade legada NULL fraca até a 12.2 e `down` falhar com homônimos (decisão do contrato/Never), "Filial padrão = mais antiga" sem coluna (decidido no contrato), importação escolher homônimo pela Filial padrão (contrato), rejeição por linha sem Filial, sem editar/excluir Filial, paginação, auditoria, docs/OpenAPI, lock de índice, `DisallowUnknownFields`, estilo/UX/acessibilidade menores, nomes de helpers.

### 2026-09-21 — Review pass (2ª passada, revisão de acompanhamento)
- intent_gap: 0
- bad_spec: 0
- patch: 3 (high 0, medium 0, low 3)
- defer: 0
- reject: ~40
- addressed_findings:
  - `[low]` `[patch]` `TestCriarEstoque_Concorrencia` chamava `filialTeste` (que usa `t.Fatalf`) dentro das goroutines — a Filial agora é resolvida uma vez antes do `go`.
  - `[low]` `[patch]` Faltava teste do erro de `GET /api/filiais` em "Locais": novo teste garante o alerta de carga, ausência do aviso "Nenhuma filial cadastrada" e botão desabilitado.
  - `[low]` `[patch]` `FiliaisSection` usava `??` na mensagem do servidor (mensagem vazia gerava alerta em branco); passou a `||`, como em `LocaisEstoqueSection`.
- Rejeitados/já cobertos (resumo): unicidade de nome legado com `filial_id` NULL e `down` com homônimos (decisão do contrato/Never + Design Notes, Story 12.2); "Filial padrão = mais antiga" sem flag (contrato); NUL em `CriarEstoque` e mapas nome→id do `migrate-legado` e recarga do `<select>` (já no `deferred`, itens preexistentes/registrados); `NomeFantasia` no provisionamento (já validado como obrigatório ≤255 em `empresas.go`); homônimos na importação (contrato); ciclo de vida/auditoria/filtro de Filial, FK composta empresa/filial, índice redundante, duplicação do helper de teste, teste de migration sobre dados (fora do escopo/estilo); `ErrEmpresaSemFilial` no handler de Estoque (`CriarEstoque` não o devolve).

## Design Notes

Unicidade de Filial por Empresa não vem do texto da story; foi adotada por coerência com Categorias/Estoques (evita duas Filiais indistinguíveis) — a Filial padrão do provisionamento não conflita (uma por Empresa). `filial_id` NULL nunca colide no índice novo (NULLs distintos), mas Estoque legado já era único por nome e nenhum caminho novo cria Estoque sem Filial, então a garantia se mantém até a 12.2 fechar com `NOT NULL`.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros e sem arquivos listados.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 -p 1 ./...` -- expected: tudo passa.
- `cd frontend && npx tsc --noEmit && npx vitest run` -- expected: sem erros de tipo e testes passando (timeouts sob carga em `CadastroProdutoSection.test.tsx` já ocorrem no baseline).

## Auto Run Result

Status: done

- **Resumo:** Filiais entram no domínio. Migration 000044 cria `filiais` e `estoques.filial_id` (nullable, aditivo) e move o índice de nome de Estoque para `(filial_id, nome_normalizado)`. `POST /api/filiais` (`adm`+) e `GET /api/filiais` (autenticado); `CriarEstoque` exige Filial da própria Empresa (400 caso contrário); `Estoque` expõe `filial_id`/`filial_nome`; `ProvisionarEmpresa` cria a Filial padrão (Nome Fantasia) na mesma transação; seed de Treinamento, importação e `migrate-legado` gravam Estoques na Filial padrão. Frontend: seção "Filiais" em Configurações (`adm`+) e seletor de Filial em "Locais".
- **Arquivos alterados nesta passada de revisão:**
  - `backend/services/estoques_test.go` — Filial resolvida fora das goroutines no teste de concorrência.
  - `frontend/src/components/estoques/LocaisEstoqueSection.test.tsx` — teste do erro de carga das Filiais.
  - `frontend/src/components/filiais/FiliaisSection.tsx` — `??` → `||` na mensagem do servidor.
  - (implementação original: migration 000044, `services/{filiais,estoques,empresas,empresas_plataforma,importacoes}.go`, `handlers/{filiais,estoques}.go`, `main.go`, `cmd/migrate-legado/main.go`, testes e componentes de frontend, conforme o commit 29cf890.)
- **Achados da revisão (2ª passada):** patches aplicados 3 (low); adiados 0; rejeitados ~40 (a maioria já decidida no contrato ou já registrada em `deferred`).
- **Recomendação de nova revisão:** `false` (patches: high 0, medium 0, low 3; pontuação 3×0 + 3 = 3 < 5).
- **Verificação:** `tsc --noEmit` limpo; vitest de `components/estoques`, `components/filiais` e `ConfiguracoesPage` (84 testes) passa; `gofmt -l` limpo, `go vet ./services/` limpo e `go test ./services/ -run 'CriarEstoque|Filial|Estoque'` passa.
- **Riscos residuais:** Empresas existentes ficam sem Filial até a Story 12.2 (só se cria Estoque após cadastrar uma Filial; a importação rejeita linhas de Estoque novo); unicidade de nome entre Estoques legados sem Filial não é garantida pelo banco até o `NOT NULL` da 12.2; rodar a 12.2 logo após o deploy.
