---
title: 'EAN-13 único entre os Produtos ativos da Empresa'
type: 'feature'
created: '2026-09-25'
status: 'done'
baseline_revision: '7b81eebb5eeea3f9fa6018df63f4374e52b3da58'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** Cadastro e edição aceitam um EAN-13 já usado por outro Produto ativo, gerando o mesmo item com dois códigos internos.

**Approach:** Reusar `garantirEANLivreTx` (advisory lock Empresa+EAN + busca de outro ativo) em `CriarProduto` e `AtualizarProduto`; handlers mapeiam `ErroEANEmUso` para 409 `EAN_EM_USO`.

## Boundaries & Constraints

**Always:** EAN em Produto inativo ou de outra Empresa é aceito; nada é gravado na recusa; duplicatas antigas não são alteradas (editar uma só salva após corrigir o EAN ou inativar o outro); Produto inativo editado não disputa EAN; edição sem conflito se comporta como antes; a mensagem é "Este EAN já está no produto {código} — {nome}".

**Never:** Índice único de EAN; alterar dados existentes; mexer na importação por planilha.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected | Error |
|---|---|---|---|
| Cadastro com EAN de ativo | mesmo EAN | 409 `EAN_EM_USO`, nada gravado | — |
| EAN de inativo/outra Empresa | — | aceito | — |
| Duas gravações simultâneas | mesmo EAN | só uma aceita | outra 409 |
| Duplicata antiga | editar um dos dois | 409 até corrigir/inativar o outro | — |

</intent-contract>

## Code Map

- `backend/services/produtos_inativacao.go` -- `garantirEANLivreTx` (já existente, da 16.1).
- `backend/services/produtos.go` -- `CriarProduto` e `AtualizarProduto` chamam o helper na transação.
- `backend/handlers/produtos.go` -- 409 `EAN_EM_USO` em criar e atualizar.
- `frontend` -- já exibe `error.message` do servidor no cadastro e na edição; sem mudança.

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/produtos.go` -- checagem de EAN no cadastro e na edição (só se o Produto está ativo)
- [x] `backend/handlers/produtos.go` -- mapear erro
- [x] `backend/services/produtos_ean_unico_test.go` -- cobre a matriz, incluindo corrida

**Acceptance Criteria:**
- Given EAN de outro ativo, when salva, then 409 `EAN_EM_USO` com código e nome do outro.
- Given EAN só em inativo/outra Empresa, when salva, then aceito.

## Verification

- `cd backend && go build ./... && go vet ./...` -- limpo
- `go test -p 1 ./services -run 'EAN|Produto|Inativ|Catalogo|Importa|Mescla|Duplicat'` e `./handlers` com `DATABASE_URL` -- verde

## Auto Run Result

Status: done. Testes de serviços (filtrados) e handlers completos verdes; suíte completa não rodou inteira por exceder 10 min.
