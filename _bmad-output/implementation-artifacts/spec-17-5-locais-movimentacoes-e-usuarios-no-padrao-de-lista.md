---
title: 'Locais, Movimentações e Usuários no padrão de lista'
type: 'feature'
created: '2026-09-25'
status: 'done'
baseline_revision: '4bb10dcb46f6c6721f66efdca3f26c1f5f250c96'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-17-context.md'
warnings: ['oversized']
deferred:
  - summary: >-
      Filtro de período compara datas do navegador com `criado_em` no fuso da sessão do banco; perto da meia-noite a borda pode divergir em até um dia.
    evidence: |-
      `whereMovimentacoes` usa `$n::date` sem fuso; o front envia a data local. A UI só envia `de`.
    location: >-
      backend/services/movimentacoes.go (whereMovimentacoes)
    severity: low
  - summary: >-
      `de` posterior a `ate` devolve lista vazia em vez de 400 (a UI não envia `ate`).
    evidence: |-
      Sem validação cruzada dos dois parâmetros.
    location: >-
      backend/services/movimentacoes.go (whereMovimentacoes)
    severity: low
---

<intent-contract>

## Intent

**Problem:** Locais (`/estoques`), Movimentações (`/estoques/movimentacoes`) e Usuários (`/admin/usuarios`) ainda usam `Card` + listas/tabelas antigas, sem faixa de indicadores, filtros no padrão nem linhas de 60px. Os componentes `FaixaIndicadores` e `PilhulaStatus` (17.3) e o padrão de linha da 17.4 já existem.

**Approach:** Backend aditivo: `GET /api/estoques/indicadores`, campo `totalItens` em `GET /api/estoques`, filtros (`de`, `ate`, `tipo`, `estoque`) em `GET /api/movimentacoes` e rota irmã `GET /api/movimentacoes/indicadores` com os mesmos filtros. Frontend: reescrever as três seções no padrão (título, filtros, faixa, linhas 60px, sem `Card`). Usuários usa a lista completa já carregada do servidor (sem paginação) para filtros e indicadores.

## Boundaries & Constraints

**Always:**
- `FaixaIndicadores` só recebe props; cada seção busca os indicadores e, se falhar (rede ou não-OK), a tela funciona e a faixa mostra `"—"`.
- Locais: `locais` = nº de Estoques da Empresa; `itensEmEstoque` = nº de Produtos ativos (`inativado_em IS NULL`) distintos com `quantidade > 0` em qualquer Estoque da Empresa. `totalItens` por Estoque = nº de Produtos ativos distintos com `quantidade > 0` naquele Estoque. Indicadores: papel mínimo igual ao da listagem (`RequireAuth`), escopo por `empresa_id`. Tabela: ícone redondo 32px + nome + subtítulo "Filial: X" (ou "—"), `totalItens` à direita, "Excluir" (ConfirmDialog, 409 com mensagem do servidor) e formulário de cadastro (filial + nome) preservados.
- Movimentações: filtros no servidor (nunca sobre as 500 linhas): `de`/`ate` (`YYYY-MM-DD`, `criado_em >= de` e `< ate + 1 dia`), `tipo` (`baixa|transferencia|ajuste|entrada`), `estoque` (UUID; casa origem OU destino). Valor inválido → `400 VALIDATION_ERROR`. `GET /api/movimentacoes/indicadores` (mesmo gate `almoxarife`+, mesmos filtros) devolve `{baixas, transferencias}` contando `tipo` no filtro (`tipo` filtrado a outro valor zera o outro contador). UI: período (Últimos 7 dias, Últimos 30 dias [padrão], Últimos 90 dias, Todo o período), tipo, estoque, "Limpar filtros" (volta ao padrão). Linha 60px: 1ª coluna ícone `ArrowLeftRight` 32px + nome do Produto + subtítulo "origem → destino" (— quando ausente); `PilhulaStatus` com o rótulo do tipo (e "Inativo" quando `inativo`); quantidade à direita; autor; data (`toLocaleString('pt-BR')`). Mantém SSE (`resource==='movimentacoes'` → toast + recarrega lista e indicadores; `conectado` carrega ambos), `seqRef`, aviso de teto de 500, "Reconectando...", estados carregando/vazio/erro.
- Usuários: busca por nome/e-mail (sem acento/caixa não obrigatório: `toLowerCase` basta), filtro por papel (`usuario|almoxarife|gestor|adm`, só os que aparecem na lista) e situação (`Todas|Ativas|Inativas`), "Limpar filtros". Indicadores calculados sobre a lista COMPLETA carregada (não sobre o filtrado): `Ativos` = `ativo`; `Sem MFA` = `ativo && !mfaHabilitado`, com `alerta` só se `usuario.empresa?.mfaObrigatorio === true` (senão sem alerta). Linha 60px: ícone `User` 32px + nome + e-mail; papel via `PilhulaStatus` (`rotuloPapel`) e "Inativa" quando `!ativo`; ações atuais (Desativar/Reativar, Rebaixar, Resetar MFA, ConfirmDialog) inalteradas, ocultas na própria linha.
- Sem `Card`/`CardHeader` nas três seções; título `h1` "Locais" na seção; em Movimentações/Usuários o `h1` continua na página (remover `h2` interno). Container `flex flex-col gap-4`. Textos em PT BR.
- Rotas antigas/gates de papel inalterados; servidor é a autoridade.

**Block If:** —

**Never:** Alterar regras de Baixa/Transferência/exclusão/desativação; adicionar paginação; mudar a resposta existente de `GET /api/usuarios`; contar indicadores de Locais/Movimentações no navegador; mexer em `LancamentoSaldoSection`.

## I/O & Edge-Case Matrix

| Cenário | Estado/Entrada | Esperado | Erro |
|---|---|---|---|
| Locais OK | 2 Estoques, 3 Produtos ativos com saldo | faixa Locais=2, Itens em estoque=3; linhas com `totalItens` | — |
| Produto inativo com saldo | só inativo tem saldo | não conta em indicador nem em `totalItens` | — |
| Indicadores falham | `/indicadores` 500/rede | tela funciona, faixa "—" | — |
| Movimentações filtro | `tipo=baixa&de=…` | lista e indicadores só com filtro aplicado | inválido → 400 |
| Filtro por estoque | `estoque=<id>` | movimentações com origem OU destino = id | UUID malformado → 400 |
| Empresa isolada | movimentações/estoques de outra Empresa | nunca contados nem listados | — |
| Usuários sem MFA | 2 ativos sem MFA; empresa exige | "Sem MFA" = 2 em vermelho | — |
| Usuários sem MFA, empresa não exige | idem | "Sem MFA" = 2, sem vermelho | — |
| Busca | "ana" | só nome/e-mail contendo; indicadores não mudam | — |

</intent-contract>

## Code Map

- `backend/services/estoques.go:19,96` — `Estoque` ganha `TotalItens int json:"totalItens"`; `ListarEstoques` calcula via `LEFT JOIN (SELECT estoque_id, COUNT(DISTINCT pe.produto_id) ... JOIN produtos p ON p.id=pe.produto_id AND p.inativado_em IS NULL WHERE pe.quantidade>0 GROUP BY estoque_id)`. Novo `IndicadoresEstoques(db, empresaID)` → `{Locais, ItensEmEstoque}`. `produto_estoque` tem `empresa_id` (migração 000032): filtrar por ele.
- `backend/services/movimentacoes.go:76` — `ListarMovimentacoes(db, empresaID)` vira `(db, empresaID, filtro FiltroMovimentacoes)`; helper único monta o `WHERE` (também usado por `IndicadoresMovimentacoes`); validação de `tipo`/datas/UUID via `*ErroMovimentacaoValidacao`. Atualizar chamadores (`grep ListarMovimentacoes(`; `ListarMovimentacoesDoUsuario` intacto).
- `backend/handlers/estoques.go:84`, `backend/handlers/movimentacoes.go:101` — novos `IndicadoresEstoquesHandler`, `IndicadoresMovimentacoesHandler`; `ListarMovimentacoesHandler` lê query params (molde de `IndicadoresPedidosHandler`, 17.4; 400 via `ErroMovimentacaoValidacao`).
- `backend/main.go:516,774` — registrar `GET /e/{slug}/api/estoques/indicadores` (RequireAuth) e `GET /e/{slug}/api/movimentacoes/indicadores` (RequireAuth + RequireRole almoxarife). Verificar que não há `GET /api/estoques/{id}` conflitante.
- `frontend/src/components/estoques/LocaisEstoqueSection.tsx` — reescrever visual; manter `carregar`/`carregarFiliais`/`enviar`/`excluir`. Novo `carregarIndicadores` (mount + após criar/excluir).
- `frontend/src/components/estoques/MovimentacoesSection.tsx` — filtros + `carregarIndicadores`; monta querystring; `GET /api/estoques` para o select de estoque.
- `frontend/src/components/usuarios/GestaoUsuariosSection.tsx` — reescrever visual e filtros; `useAuth().usuario.empresa?.mfaObrigatorio`. Página `pages/admin/UsuariosPage.tsx` mantém o `h1`.
- Reuso: `components/lista/FaixaIndicadores.tsx`, `PilhulaStatus.tsx`; molde de linha 60px em `components/pedidos/MeusPedidosSection.tsx` (17.4); `lib/pedidos.ts:buscarIndicadoresPedidos` como molde de fetch.
- Testes a atualizar/estender: `LocaisEstoqueSection.test.tsx`, `MovimentacoesSection.test.tsx`, `GestaoUsuariosSection.test.tsx`, `EstoquesPage.test.tsx`, `services/estoques_test.go`, `services/movimentacoes_test.go`, `handlers` (se houver teste de rota).

## Tasks & Acceptance

**Execution:**
- `backend/services/estoques.go` — `totalItens` + `IndicadoresEstoques` — dados de Locais
- `backend/services/movimentacoes.go` — filtros + `IndicadoresMovimentacoes` — período/tipo/estoque no servidor
- `backend/handlers/estoques.go`, `backend/handlers/movimentacoes.go`, `backend/main.go` — handlers e rotas
- `backend/services/*_test.go` — escopo de Empresa, produto inativo, filtros, 400 inválido, contadores
- `frontend/src/components/estoques/LocaisEstoqueSection.tsx` — padrão de lista + faixa
- `frontend/src/components/estoques/MovimentacoesSection.tsx` — padrão + filtros + faixa
- `frontend/src/components/usuarios/GestaoUsuariosSection.tsx` — padrão + filtros + faixa
- `frontend/src/**/*.test.tsx` das três seções — faixa com valores, falha → "—", filtros, alerta condicional de MFA, comportamentos antigos preservados

**Acceptance Criteria:**
- Given Locais carrega, when `/estoques` abre, then mostra título, faixa Locais/Itens em estoque e linhas com nome, filial e total de itens, sem `Card`.
- Given Movimentações, when período/tipo/estoque mudam, then lista e indicadores Baixas/Transferências são rebuscados do servidor com os mesmos parâmetros e a tabela mostra Produto (ícone+nome+subtítulo), tipo em pílula, quantidade à direita, origem/destino, autor e data.
- Given Usuários, when busca/papel/situação mudam, then a lista filtra; Ativos e Sem MFA refletem a lista completa; Sem MFA fica em vermelho só se a Empresa exige MFA.
- Given `/indicadores` falha em qualquer das telas, then a tela funciona e a faixa mostra "—".
- Given cadastro/exclusão de Estoque, Baixa/Transferência via SSE e ações de Usuário, when usados, then comportamento existente preservado (incl. toasts, 409, ConfirmDialog, isolamento por papel).

## Spec Change Log

## Review Triage Log

### 2026-09-25 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 6 (high 0, medium 2, low 4)
- defer: 2 (high 0, medium 0, low 2)
- reject: 20+
- addressed_findings:
  - `[medium]` `[patch]` Usuários: faixa mostrava 0 antes da carga; agora "—" até carregar (flag `carregou`).
  - `[medium]` `[patch]` Testes Go faltando: saldo só em `lotes`, isolamento entre Empresas dos novos indicadores/filtros, handlers 400/403/401 e forma do corpo.
  - `[low]` `[patch]` Usuários: filtro de papel obsoleto após recarga volta a "todos".
  - `[low]` `[patch]` Movimentações: mensagem de vazio específica quando há filtros.
  - `[low]` `[patch]` Locais: guarda de sequência nos indicadores.
  - `[low]` `[patch]` Docblocks desatualizados corrigidos.

### 2026-09-25 — Review pass (2ª, revisão de acompanhamento)
- intent_gap: 0
- bad_spec: 0
- patch: 1 (high 0, medium 0, low 1)
- defer: 0
- reject: 40+ (fuso/`de>ate` já estão em `deferred`; teto de 500 e contadores, SSE, acessibilidade, acentos, `mfaHabilitado` indefinido = comportamento da spec ou fora do escopo)
- addressed_findings:
  - `[low]` `[patch]` Movimentações: faltava teste de resposta de indicadores fora de ordem (`seqIndRef`); teste adicionado.

## Auto Run Result

Status: done

- Mudança: Locais, Movimentações e Usuários no padrão de lista, com endpoints aditivos de indicadores e filtros de Movimentações no servidor.
- Esta passagem: revisão de acompanhamento; 1 patch (teste em `frontend/src/components/estoques/MovimentacoesSection.test.tsx`), 0 deferidos novos, demais rejeitados.
- Recomendação de follow-up: false (patch low=1, pontuação 1).
- Verificação: `tsc -b` limpo; `npm test` 985/985 verde; `npm run lint` sem erros nos arquivos da história (erros pré-existentes em outros arquivos). Testes Go não reexecutados nesta passagem (sem mudança de backend).
- Riscos residuais: os dois itens em `deferred` (fuso do filtro de período; `de` > `ate`).

## Design Notes

Filtros de Movimentações/Usuários usam controles simples (`select`/`Input`) + "Limpar filtros"; não há componente de pílulas removíveis reutilizável (só o inline do Catálogo), então não se cria abstração nova. Padrão de linha: `flex items-center gap-3 h-[60px] border-b border-border`, ícone `flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted`; no celular a linha pode quebrar (`flex-wrap`) sem scroll horizontal.

## Verification

**Commands:**
- `cd frontend && npx tsc -b && npm run lint && npm test` -- expected: limpo e verde
- `cd backend && go build ./... && go vet ./... && go test ./services/... ./handlers/...` -- expected: verde (testes com banco dependem do ambiente; reportar se indisponível)

