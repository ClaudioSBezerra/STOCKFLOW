---
title: 'Story 4.6 — Exportação da tabela do catálogo para Excel'
type: 'feature'
created: '2026-09-01'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '96a1bb73403eba518e52fbab3f52744c6e50a951'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** O Catálogo (Stories 4.1-4.5) só existe na tela. FR-30 pede que o Almoxarife exporte a visualização em tabela — com os filtros ativos (Story 4.2) — para uma planilha `.xlsx` real, com subtotais por grupo que continuam corretos mesmo depois de o arquivo já exportado ser filtrado no Excel. Não existe nenhum endpoint de exportação nem `services/relatorios.go`.

**Approach:** Backend: `GET /api/produtos/catalogo/exportar` (`RequireAuth`+`RequireRole(almoxarife)`) aplica os mesmos 4 filtros de `GET /api/produtos/catalogo` (sem `pagina`/`agrupar` — exporta SEMPRE a tabela agrupada inteira, não uma página) e devolve um `.xlsx` gerado por `services.GerarCatalogoXLSX` (`excelize`, novo `services/relatorios.go`): uma linha por combinação grupo×Estoque, uma linha de subtotal por grupo (fórmula `SUBTOTAL`) e uma linha de total geral. Frontend: botão "Exportar" em `CatalogoListagem`, visível só quando `modo === 'tabela'` e o papel é `almoxarife`+, que baixa o arquivo via `fetch` autenticado + `Blob`.

## Boundaries & Constraints

**Always:**
- `GET /api/produtos/catalogo/exportar?q=&categoriaId=&estoqueId=&comEstoque=`: `RequireAuth` + `RequireRole(services.PapelAlmoxarife)` (mesma composição de `POST /api/produtos`) — papel `usuario` -> `403 FORBIDDEN` (middleware, handler nunca roda). Mesma validação de `q`/`categoriaId`/`estoqueId`/`comEstoque` de `ListarCatalogoHandler` (trim + teto 255 runes em `q`; `comEstoque` só `true`/`false`/ausente); SEM `pagina` nem `agrupar` — sempre exporta a tabela agrupada COMPLETA (todos os grupos que casam o filtro, não uma página).
- `services.ListarTodosGruposCatalogo(db, filtros) ([]CatalogoGrupo, error)` (novo, `catalogo.go`): reusa `catalogoGrupoQueryBase`/`catalogoGrupoQuerySuffix`/`preencherPorEstoque` de `ListarCatalogoAgrupado`, SEM `LIMIT`/`OFFSET` — mesma ordem (`nome ASC`, dimensões, `chave`), mesmo colapso `filtroUUIDInvalido` -> slice vazio (nunca erro) para `categoriaId`/`estoqueId` malformado.
- `services.GerarCatalogoXLSX(db, filtros) ([]byte, error)`: cabeçalho fixo de 13 colunas — `Nome, Comprimento (valor), Comprimento (unidade), Largura (valor), Largura (unidade), Diâmetro (valor), Diâmetro (unidade), Altura (valor), Altura (unidade), Espessura (valor), Espessura (unidade), Estoque, Quantidade` (mesmos rótulos de dimensão de `services.CabecalhoEsperado`, Story 3.3 — vocabulário único de colunas para import/export). Uma linha por item de `grupo.PorEstoque` (Nome+dimensões repetidos, `Estoque`=nome, `Quantidade`=valor numérico); `PorEstoque` vazio -> 1 linha com `Estoque` vazio e `Quantidade` 0 (nunca omite o grupo). Após as linhas de um grupo: 1 linha "Subtotal — {nome}" com fórmula `SUBTOTAL(9,<intervalo Quantidade do grupo>)` na coluna Quantidade. Ao final (só quando há >=1 grupo): 1 linha "Total geral" com `SUBTOTAL(9,<intervalo Quantidade completo, incluindo os subtotais>)` — o `SUBTOTAL` do Excel ignora nativamente subtotais aninhados no mesmo intervalo, então o total geral nunca soma em dobro. Zero grupos -> só a linha de cabeçalho, nenhuma linha de dado nem de total. `AutoFilter` na linha de cabeçalho (para o filtro em Excel de que fala o `Always` acima permanecer coerente com o `SUBTOTAL`).
- Handler `ExportarCatalogoHandler(db)`: sucesso -> `200`, `Content-Type: application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`, `Content-Disposition: attachment; filename="catalogo.xlsx"`, corpo = bytes do `.xlsx`. Erro de banco -> `500 INTERNAL_ERROR` (envelope JSON de erro, `slog`).
- `frontend/src/components/catalogo/CatalogoListagem.tsx`: nova prop opcional `podeExportar?: boolean` (default `false`). Botão "Exportar" (`min-h-touch-target-min`) ao lado do alternador grade/tabela, renderizado só quando `podeExportar && modo === 'tabela'`. Clique: reusa a MESMA serialização de filtros de `carregar` (extrair um helper comum `queryFiltros`, sem `agrupar`/`pagina`) contra `/api/produtos/catalogo/exportar`, `fetch` com `authHeaders()`; `res.ok` -> `res.blob()` + `URL.createObjectURL` + `<a download="catalogo.xlsx">` clicado e removido (`URL.revokeObjectURL` depois); `!res.ok` ou exceção de rede -> `toast.error('Não foi possível exportar o catálogo. Tente novamente em instantes.')` (`sonner`, mesmo padrão de `ScannerProdutoFab`). Estado local desabilita o botão ("Exportando..." ) enquanto a requisição está em voo.
- `frontend/src/pages/CatalogoPage.tsx`: calcula `podeExportar = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife')` e repassa a `<CatalogoListagem podeExportar={podeExportar} .../>`.

**Block If:** nenhuma condição desta story exige decisão humana em runtime — segue direto.

**Never:**
- Nenhum parâmetro `pagina`/`agrupar` no endpoint de exportação — ele nunca exporta uma página, sempre o filtro completo (Design Notes).
- Nenhuma soma estática (célula com valor calculado em Go) na linha de subtotal/total — sempre fórmula `SUBTOTAL`, para permanecer correta quando o próprio Excel filtra o arquivo já exportado (FR-30).
- Nenhuma coluna `Código`/`Categoria` no `.xlsx` — a tabela agrupada (Story 4.3) já omite os dois por design (um grupo pode juntar Produtos de código/categoria diferentes); exportar teria que inventar um valor arbitrário.
- Nenhuma mudança em `ListarCatalogoGrade`/`ListarCatalogoAgrupado`/`ListarCatalogoHandler` (Stories 4.2/4.3) nem no botão exportar aparecendo para o modo grade — exportação é só do modo tabela agrupada.
- Nenhuma exportação em CSV/PDF/outro formato — só `.xlsx` (FR-30).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Exportação com grupos e Estoques | papel `almoxarife`, filtro casa 2 grupos com Estoques | `200`, `.xlsx`: 1 linha de detalhe por grupo×Estoque + 1 subtotal (`SUBTOTAL`) por grupo + 1 total geral | No error |
| Grupo sem `produto_estoque` | `porEstoque` vazio | 1 linha (`Estoque` vazio, `Quantidade` 0), subtotal do grupo = 0 | No error |
| Filtro sem resultado | filtro não casa nenhum Produto | `200`, `.xlsx` só com a linha de cabeçalho, sem total geral | No error |
| `categoriaId`/`estoqueId` malformado | id não-UUID | `200`, `.xlsx` só com cabeçalho (mesmo colapso de `ListarCatalogoAgrupado`) | No error |
| Papel `usuario` chama direto | token `usuario` | — | `403 FORBIDDEN` |
| `q` > 255 runes | termo longo | — | `400 VALIDATION_ERROR` "termo de busca muito longo" |
| `comEstoque` inválido | `?comEstoque=talvez` | — | `400 VALIDATION_ERROR` "parâmetro comEstoque inválido" |
| Botão Exportar no modo grade | `modo==='grade'` | botão não aparece, mesmo para `almoxarife`+ | client-side |
| Botão Exportar para papel `usuario` | `podeExportar=false` | botão não aparece | client-side |
| Clique exporta com sucesso | `modo==='tabela'`, `almoxarife`+ | download de `catalogo.xlsx` com os filtros ativos na query | client-side |
| Exportação falha (rede/não-OK) | `fetch` rejeita ou `res.ok===false` | `toast.error` "Não foi possível exportar..." | client-side |

</intent-contract>

## Code Map

- `backend/services/catalogo.go:348-388` (`catalogoGrupoQueryBase`/`catalogoGrupoQuerySuffix`), `:408-487` (`ListarCatalogoAgrupado`, molde exato sem `LIMIT`/`OFFSET`), `:493-545` (`preencherPorEstoque`, reusar tal qual) -- adicionar `ListarTodosGruposCatalogo(db *sql.DB, filtros FiltrosCatalogo) ([]CatalogoGrupo, error)`.
- `backend/services/catalogo_test.go` -- `TestListarTodosGruposCatalogo_*`: sem paginação (mais grupos que `TamanhoPaginaCatalogo`), filtros aplicados, `categoriaId` malformado -> vazio sem erro.
- `backend/services/relatorios.go` (novo) -- `GerarCatalogoXLSX(db *sql.DB, filtros FiltrosCatalogo) ([]byte, error)`: chama `ListarTodosGruposCatalogo`, monta o `.xlsx` via `excelize.NewFile()`/`SetCellStr`/`SetCellFloat`/`SetCellFormula`/`AutoFilter`/`WriteToBuffer` (mesmas chamadas do helper de teste `construirXLSXMux` em `main_test.go`, mas escrevendo, não só lendo).
- `backend/services/relatorios_test.go` (novo) -- `TestGerarCatalogoXLSX_*`: abre o `[]byte` resultante com `excelize.OpenReader` e lê células/fórmulas — grupo com Estoques (linhas + fórmula `SUBTOTAL` do subtotal), grupo sem Estoque (linha única, subtotal 0), múltiplos grupos (fórmula do total geral cobre o intervalo certo), zero grupos (só cabeçalho, sem linha de total).
- `backend/handlers/produtos.go:326-399` (`ListarCatalogoHandler`, molde de parse/validação de `q`/`categoriaId`/`estoqueId`/`comEstoque`) -- adicionar `ExportarCatalogoHandler(db *sql.DB) http.HandlerFunc` (sem `pagina`/`agrupar`): valida os 4 filtros, chama `services.GerarCatalogoXLSX`, escreve os headers de download + corpo binário; erro de banco -> `500 INTERNAL_ERROR` (`slog`).
- `backend/handlers/produtos_test.go` -- `TestExportarCatalogoHandler_*`: `200` com `Content-Type`/`Content-Disposition` corretos e `.xlsx` válido (`excelize.OpenReader` no `w.Body.Bytes()`); `400` "termo de busca muito longo"/"parâmetro comEstoque inválido"; filtros repassados corretamente ao service.
- `backend/main.go:58-78` (doc comment do topo), `:442-443` (bloco de `GET /api/produtos/catalogo`, Story 4.3) -- nova linha `mux.HandleFunc("GET /api/produtos/catalogo/exportar", middleware.RequireAuth(db, jwtSecret)(middleware.RequireRole(services.PapelAlmoxarife)(handlers.ExportarCatalogoHandler(db))))`, comentário citando a Story 4.6/FR-30; segmento de 2 níveis (`catalogo/exportar`), nunca colide com `GET /api/produtos/{id}` independente de ordem de registro.
- `backend/main_test.go:998-1056` (`TestNewMux_ImportacoesRotaCarregaRequireRole`, molde de RequireRole almoxarife) -- `TestNewMux_ProdutosCatalogoExportarRotaRequireRoleAlmoxarife` (`usuario` -> 403; `almoxarife` -> 200 com `Content-Type` do `.xlsx`).
- `frontend/src/components/catalogo/CatalogoListagem.tsx:184-241` (`carregar`, serialização de filtros em `query`) -- extrair a montagem de `q`/`categoriaId`/`estoqueId`/`comEstoque` num helper `queryFiltros(filtros: FiltrosAtivos): string` reusado por `carregar` (que prefixa `agrupar=&pagina=`) e pela nova função `aoExportar`; novo botão "Exportar" condicional a `podeExportar && modo === 'tabela'`, estado `exportando`, download via `Blob`/`URL.createObjectURL`, `toast.error` em falha (import `toast` de `sonner`, mesmo padrão de `ScannerProdutoFab.tsx`).
- `frontend/src/components/catalogo/CatalogoListagem.test.tsx` -- botão ausente em `modo='grade'`/`podeExportar=false`; presente e clicável em `modo='tabela'`+`podeExportar=true`; clique dispara `fetch('/api/produtos/catalogo/exportar?...')` com os filtros ativos; sucesso (mock de `URL.createObjectURL`/`revokeObjectURL`, ausentes em jsdom) cria/clica o link; falha mostra `toast.error` (mock de `sonner`).
- `frontend/src/pages/CatalogoPage.tsx:56-57,85` (`podeCadastrar`, `<CatalogoListagem termo={termoFiltro} />`) -- `podeExportar` calculado com `rankPapel` (mesmo padrão de `podeCadastrar`), repassado como prop.
- `frontend/src/pages/CatalogoPage.test.tsx` -- ajusta o mock de `fetch` se necessário para não quebrar com a nova prop; nenhuma asserção existente deve mudar de comportamento.

## Tasks & Acceptance

**Execution:**
- `backend/services/catalogo.go` (+ `catalogo_test.go`) -- `ListarTodosGruposCatalogo`: mesma query/filtros/ordem de `ListarCatalogoAgrupado`, sem paginação.
- `backend/services/relatorios.go` (novo, + `relatorios_test.go`) -- `GerarCatalogoXLSX`: 13 colunas, linha por grupo×Estoque, subtotal por grupo (`SUBTOTAL`), total geral, `AutoFilter`, zero grupos -> só cabeçalho.
- `backend/handlers/produtos.go` (+ `produtos_test.go`) -- `ExportarCatalogoHandler`: valida filtros (sem `pagina`/`agrupar`), gera e serve o `.xlsx` com os headers de download corretos.
- `backend/main.go` (+ `main_test.go`) -- registra `GET /api/produtos/catalogo/exportar` atrás de `RequireAuth`+`RequireRole(almoxarife)`.
- `frontend/src/components/catalogo/CatalogoListagem.tsx` (+ teste) -- botão "Exportar" condicional, helper `queryFiltros` compartilhado, download via `Blob`, `toast.error` em falha.
- `frontend/src/pages/CatalogoPage.tsx` (+ teste) -- calcula e repassa `podeExportar`.

**Acceptance Criteria:**
- Given a visualização em tabela do Catálogo com um ou mais filtros ativos (Story 4.2), when o Almoxarife exporta, then o `.xlsx` gerado reflete exatamente o filtro aplicado (mesmos grupos/ordem da tabela agrupada, Story 4.3), com uma linha de subtotal por grupo usando fórmula `SUBTOTAL` (não soma estática).
- Given um filtro que resulta em zero Produtos, when o Almoxarife exporta mesmo assim, then o `.xlsx` é gerado válido, contendo só o cabeçalho.
- Given um Usuário com papel `usuario`, when ele tenta chamar `GET /api/produtos/catalogo/exportar` diretamente pela API, then a resposta é `403 FORBIDDEN` — exportação restrita a `almoxarife`+; o botão "Exportar" também não aparece para esse papel na interface.
- Given o Catálogo em modo grade (não tabela), when qualquer papel visualiza a tela, then o botão "Exportar" não aparece (a exportação é sempre da tabela agrupada).

## Spec Change Log

## Review Triage Log

## Design Notes

- **"totais e metragem calculada" (FR-30) = os totais por `SUBTOTAL`.** `epic-4-context.md` (contexto compilado, fonte primária desta story) e as 3 ACs de `epics.md` para a Story 4.6 já decompõem FR-30 sem nenhuma menção a uma métrica derivada de dimensão×quantidade — não existe, em nenhum lugar do schema/código, uma unidade de "metragem" combinando as 5 dimensões estruturadas (cada uma já é `{valor,unidade}` independente, AD-9) com `produto_estoque.quantidade` (contagem sem unidade declarada, Never de spec-4-3). Inventar essa fórmula seria fantasiar um requisito sem base textual. A leitura adotada: "totais" = subtotal por grupo + total geral (ambos via `SUBTOTAL`); "metragem calculada" = a própria quantidade, já somada pela fórmula (não uma coluna nova).
- **Colunas do `.xlsx` = vocabulário de `CabecalhoEsperado` (Story 3.3) + a forma da tabela agrupada.** PRD (`prd.md`, "Excel/CSV") define um "modelo único padronizado de colunas... para importação/exportação" — os rótulos de dimensão (`Comprimento (valor)`, `Comprimento (unidade)`, etc.) desta spec são os MESMOS de `CabecalhoEsperado` (import, Story 3.3). Mas a tabela agrupada (Story 4.3) não carrega `Código`/`Categoria` por design (um grupo pode juntar Produtos de código/categoria diferentes) — por isso essas 2 colunas do import NÃO entram aqui; `Estoque` entra porque é a discriminação que a tabela expande em tela.
- **`SUBTOTAL` ignora subtotais aninhados nativamente.** O total geral usa `SUBTOTAL(9,<coluna Quantidade inteira, incluindo as linhas de subtotal>)` — comportamento documentado do Excel: uma célula com `SUBTOTAL` dentro do intervalo de outra `SUBTOTAL` é ignorada automaticamente, então o total geral nunca soma em dobro sem precisar de um intervalo que pule as linhas de subtotal.
- **Exportação é sempre do conjunto filtrado completo, nunca de uma página.** `TamanhoPaginaCatalogo` (24) é uma decisão de UI de tela (Story 4.3); "levar os dados para uma planilha... externa" (epics.md) só faz sentido com o filtro inteiro — por isso `ListarTodosGruposCatalogo` não aceita `pagina`.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` -- sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` -- cobre `TestListarTodosGruposCatalogo*`, `TestGerarCatalogoXLSX*`, `TestExportarCatalogoHandler_*`, `TestNewMux_ProdutosCatalogoExportarRotaRequireRoleAlmoxarife`.
- `cd frontend && npm run lint && npm run build && npm run test` -- `oxlint`, `tsc`+`vite`, testes de `CatalogoListagem`/`CatalogoPage`.

**Manual checks (if no CLI):**
- `docker compose up --build`, logar como `almoxarife`: no Catálogo, alternar para tabela, aplicar um filtro, clicar "Exportar" -> arquivo `catalogo.xlsx` baixa; abrir no Excel, aplicar um AutoFilter na coluna Estoque e conferir que as linhas de subtotal/total se recalculam.
- Logar como `usuario`: confirmar que o botão "Exportar" não aparece mesmo em modo tabela.
