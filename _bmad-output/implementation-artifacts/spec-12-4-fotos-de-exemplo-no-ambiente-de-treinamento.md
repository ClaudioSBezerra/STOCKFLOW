---
title: 'Story 12.4: Fotos de exemplo no Ambiente de Treinamento'
type: 'feature'
created: '2026-09-21'
status: awaiting-operator
baseline_revision: 'cd2878bdc0d56d844636f4202ce442eefa1d250e'
review_loop_iteration: 0
followup_review_recommended: true
context: []
warnings: ['oversized']
deferred: []
operator_actions:
  - 'Combinar com o Adm o conteúdo/quantidade das fotos de exemplo (decisão de UX da story); se quiser fotos reais em vez das ilustrações "EXEMPLO", sobrescrever os JPEGs de `backend/services/fotos_treinamento/` (mesmo nome, ≤500px) antes do build.'
  - 'Fazer o deploy desta versão (o binário `seed-fotos-treinamento` vem no `Dockerfile` da API) no ambiente alvo (staging `stockflow.fbtechia.com` e/ou produção `suprimentos.fcxlabs.com`).'
  - 'Para CADA Ambiente de Treinamento que deve ganhar fotos, rodar no container `api`, primeiro em dry-run: `./seed-fotos-treinamento --empresa <slug>-treinamento` (confira `diretório de fotos: /data/fotos` e `a semear: 5`) e depois `./seed-fotos-treinamento --empresa <slug>-treinamento --executar` — disparado manualmente por uma pessoa, nunca por agente autônomo (AD-15).'
  - 'Conferir no app, logado no Treinamento, que os 5 Produtos de exemplo mostram foto no catálogo; Treinamentos antigos só ganham fotos se o comando for rodado para eles (não há reseed automático).'
---

<intent-contract>

## Intent

**Problem:** Os 5 Produtos semeados no Ambiente de Treinamento (Story 9.2) nascem sem foto; o catálogo de treinamento parece uma lista vazia de nomes (FR-51).

**Approach:** Binário one-off `cmd/seed-fotos-treinamento` (dry-run por padrão, `--executar` aplica), disparado À MÃO por uma pessoa para UM Treinamento por vez (`--empresa <slug>`): grava uma foto de exemplo por Produto de exemplo, com `services.SalvarFotoProduto` (o mesmo armazenamento versionado da Story 3.5, `<produto_id>-<timestamp>.jpg`, sem overwrite). Sem storage novo, sem reseed automático, sem tocar o provisionamento.

## Boundaries & Constraints

**Always:**
- Novo `services/fotos_treinamento.go`: `SemearFotosTreinamento(db, fotosDir, slug, executar)` resolve a Empresa por slug (`BuscarEmpresaPorSlug`) e exige `EmpresaOrigemID != nil`; Empresa real → `ErrEmpresaNaoTreinamento`, slug desconhecido/inativo → `ErrEmpresaNaoEncontrada`. Nada é escrito nesses casos.
- Para cada Produto de `produtosExemploTreinamento` (busca por `empresa_id` + `nome` exato): já tem foto (`ListarFotosProduto` não vazio) → pula (preserva foto real enviada pelo Adm); ausente na Empresa → só reportado; senão grava a foto embarcada. Idempotente: reexecutar não grava nada. Dry-run relata as mesmas contagens sem gravar em disco.
- Fotos embarcadas com `go:embed` em `services/fotos_treinamento/*.jpg` (JPEG ≤500px, q≈82, mesma regra da Story 3.5), mapeadas por nome de Produto em `fotosExemploTreinamento`. Um teste garante que todo Produto de exemplo tem foto embarcada válida (JPEG decodificável). O conjunto atual é ilustração gerada (rótulo "EXEMPLO"), substituível por fotos reais trocando o arquivo de mesmo nome — conteúdo/quantidade é decisão de UX com o Adm (fora do código).
- CLI no molde de `cmd/migrar-estoques-filial`: `godotenv`, `DATABASE_URL` obrigatório, `--empresa` obrigatório, `--fotos-dir` (padrão `FOTOS_DIR`, senão `./fotos`), `--executar`, runbook no cabeçalho, `executar…(db, out, …)` testável. Registrar no `Dockerfile` (build + copy).
- O Adm tem que rodar o binário no container `api` (é ele que enxerga o volume `/data/fotos`); nada no pipeline de deploy, entrypoint, cron ou rota HTTP o dispara (AD-15).

**Block If:** Nenhuma decisão bloqueante identificada.

**Never:** Não alterar `semearDadosTreinamento` nem `CriarEmpresaComTreinamento` (o provisionamento segue transacional e sem disco); não reseedar automaticamente Treinamentos existentes; não aceitar Empresa real; não sobrescrever/remover foto existente; não criar tabela/coluna/migration; não adicionar `if eh_treinamento` em service de domínio (a guarda vive só neste service de seed); não tocar `sprint-status.yaml`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Treinamento recém-criado | `--empresa X-treinamento --executar`, 5 Produtos sem foto | 5 arquivos `<id>-<ts>.jpg` em `fotos-dir`; relatório `semeadas=5` | — |
| Dry-run | sem `--executar` | relatório `a semear=5`; nenhum arquivo criado | — |
| Reexecução | os 5 já com foto | `semeadas=0`, `já com foto=5`; nada escrito | — |
| Foto real do Adm | 1 Produto já tem foto própria | esse é pulado, os demais recebem | — |
| Produto renomeado/removido | Produto de exemplo ausente | listado em `ausentes`; os demais seguem | — |
| Empresa real | `--empresa X` (sem `empresa_origem_id`) | nada escrito | `ErrEmpresaNaoTreinamento` |
| Slug inexistente | `--empresa nao-existe` | nada escrito | `ErrEmpresaNaoEncontrada` |
| Isolamento | produto homônimo em outra Empresa | não recebe foto | — |
| Sem `--empresa` | flag ausente | exit 1 com mensagem | — |

</intent-contract>

## Code Map

- `backend/services/empresas_plataforma.go:92-116,279-320` -- `produtoExemplo`/`produtosExemploTreinamento` (5 Produtos fixos, fonte dos nomes) e `semearDadosTreinamento` (NÃO alterar).
- `backend/services/fotos.go` -- `SalvarFotoProduto` (grava versionado, `O_EXCL`), `ListarFotosProduto`, `produtoDaEmpresa` (guarda de posse); reaproveitados como estão.
- `backend/services/empresas.go:223` -- `BuscarEmpresaPorSlug`; `Empresa.EmpresaOrigemID` marca o Treinamento.
- `backend/cmd/migrar-estoques-filial/main.go` (+ `main_test.go`) -- MOLDE do CLI e do teste (`testDB` com migrations reais).
- `backend/services/empresas_plataforma_test.go` -- `novaEmpresaTeste`, `comParLimpo`, `contar`, `testEmailCfg` para os testes de service.
- `backend/Dockerfile:15,28` -- padrão de build/copy dos binários one-off.

## Tasks & Acceptance

**Execution:**
- `backend/services/fotos_treinamento/*.jpg` + `backend/services/fotos_treinamento.go` -- serviço, erros, mapa nome→arquivo, `go:embed` -- núcleo
- `backend/services/fotos_treinamento_test.go` -- matriz de I/O (semear, dry-run, reexecução, foto existente, ausente, Empresa real, inexistente, isolamento, embarcadas válidas p/ todo Produto de exemplo) -- prova
- `backend/cmd/seed-fotos-treinamento/main.go` (+ `main_test.go`) -- CLI com runbook, flags, relatório -- entrega manual (AD-15)
- `backend/Dockerfile` -- build e copy do binário -- disponível no container `api`

**Acceptance Criteria:**
- Given um Ambiente de Treinamento com os Produtos de exemplo, when uma pessoa roda o binário com `--executar`, then cada Produto de exemplo passa a ter ao menos uma foto listada por `ListarFotosProduto` (mesmo armazenamento e nomenclatura da Story 3.5).
- Given o binário sem `--executar` ou uma reexecução, then nada é escrito em disco.
- Given Treinamentos anteriores a esta story, then nada muda até alguém rodar o binário para eles.
- Given uma Empresa real, when o binário é apontado a ela, then é recusado sem escrever.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 4 (high 0, medium 1, low 3)
- defer: 0
- reject: 12
- addressed_findings:
  - `[medium]` `[patch]` Diretório de fotos default (`./fotos`) podia divergir do volume da API sem o operador perceber: extraído `resolverFotosDir` (flag > `FOTOS_DIR` > `./fotos`, testado) e o relatório do CLI agora imprime o diretório usado.
  - `[low]` `[patch]` Nenhum teste afirmava o CONTEÚDO gravado: teste agora compara os bytes de cada arquivo semeado com a foto embarcada do próprio Produto.
  - `[low]` `[patch]` Treinamento sem nenhum Produto de exemplo saía com exit 0 sem gravar: novo `ErrTreinamentoSemProdutosExemplo` (mensagem própria no CLI), testado.
  - `[low]` `[patch]` Busca do Produto ignorava `deleted_at` (Produto mesclado/apagado receberia foto): filtro `deleted_at IS NULL`, testado.
  - Rejeitados (resumo): corrida check-then-write entre execuções simultâneas (execução manual de uma pessoa; efeito máximo = 2 fotos), checagem de escrita no dry-run, busca por nome vs. chave estável (nome é a chave do seed 9.2; renomeado é reportado como ausente, por contrato), normalização de slug no service (o CLI valida), renomear `Executado`, helpers de teste duplicados (padrão do repositório), lint cosmético do teste, linhas `# binário novo` (artefato do diff de revisão), decode em runtime das fotos embarcadas (coberto por teste), smoke da imagem Docker, `--json`/exit code para ausentes parciais, rollback/remoção de fotos (fora do escopo), leitura preguiçosa das fotos.
  - Intent Alignment: o diff implementa a leitura "CLI manual por Treinamento" (leitura A), a única que o `<intent-contract>` e o AD-15 admitem; semear no provisionamento contradiz "script one-off, nunca automático".

## Design Notes

"Rotas normais de upload" é atendido no nível do service (`SalvarFotoProduto`, o mesmo que `EnviarFotoProdutoHandler` chama): um CLI não tem sessão HTTP nem `FOTOS_DIR` do servidor em rede, e passar por HTTP exigiria credenciais. As fotos já estão no formato final (≤500px, JPEG q82), então dispensam o pipeline de decode/resize do handler. Semear só quando uma pessoa roda o binário cumpre "provisionados a partir de agora" sem reseed automático: cada Treinamento novo é semeado explicitamente, por slug.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros e sem arquivos listados.
- `cd backend && go test -count=1 -p 1 ./services/ -run 'FotosTreinamento' && go test -count=1 -p 1 ./cmd/seed-fotos-treinamento/` (com `DATABASE_URL` do Postgres local) -- expected: passa.

## Auto Run Result

Status: awaiting-operator
Blocking condition: nenhuma — todo o código está entregue e verificado contra Postgres real; falta a execução do seed em cada Ambiente de Treinamento, que AD-15 reserva a uma pessoa (ver `operator_actions`).

**Resumo:** binário one-off `backend/cmd/seed-fotos-treinamento` (dry-run por padrão, `--executar` grava) sobre `services.SemearFotosTreinamento`: resolve o Treinamento por `--empresa <slug>` (recusa Empresa real e slug inexistente), e para cada um dos 5 Produtos de exemplo (Story 9.2) sem foto grava uma foto embarcada com `SalvarFotoProduto` (armazenamento versionado da Story 3.5, sem overwrite). Idempotente; preserva foto real do Adm; reporta Produto ausente; recusa Treinamento sem nenhum Produto de exemplo. O provisionamento (`semearDadosTreinamento`) não foi alterado.

**Arquivos:**
- `backend/services/fotos_treinamento.go` — serviço, erros, mapa nome→foto, `go:embed`.
- `backend/services/fotos_treinamento/*.jpg` — 5 ilustrações "EXEMPLO" (400x400, JPEG q82), substituíveis por fotos reais.
- `backend/services/fotos_treinamento_test.go` — matriz de I/O (semear com conteúdo, dry-run, reexecução, foto existente, ausente, sem Produtos/apagados, Empresa real, inexistente, isolamento, embarcadas válidas).
- `backend/cmd/seed-fotos-treinamento/main.go` (+ `main_test.go`) — CLI com runbook, `resolverFotosDir`, `validarArgumentos`.
- `backend/Dockerfile` — build e copy do binário.

**Revisão:** 4 patches aplicados (1 medium, 3 low; score 3×1+3 = 6 ≥ 5 → `followup_review_recommended: true`), 0 deferidos, 12 rejeitados.

**Verificação:** `go build ./... && go vet ./... && gofmt -l .` limpos; `go test -count=1 -p 1 ./services/ -run FotosTreinamento` (7 testes) e `./cmd/seed-fotos-treinamento/` (4 testes) passam contra o Postgres local. Não rodei a suíte completa `./...` nem o build Docker.

**Riscos residuais:** as fotos são ilustrações geradas (conteúdo definido com o Adm é decisão de UX pendente); execuções simultâneas do CLI podem gerar 2 fotos para o mesmo Produto (improvável, execução manual).
