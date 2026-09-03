---
title: 'Recibo do Pedido em PDF gerado pelo servidor'
type: 'feature'
created: '2026-09-03'
status: 'done'
baseline_revision: '7bfd2fc7bffad2d2b8640b207c779de1099a6887'
review_loop_iteration: 0
followup_review_recommended: false
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-7-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      Nenhum log estruturado de sucesso (slog.Info) registra quem baixou qual
      recibo em PDF.
    evidence: |-
      Mesmo padrão pré-existente já aceito em SubmeterPedidoHandler e
      DecidirPedidoHandler (nenhum dos dois loga sucesso, só falhas via
      slog.Error) — escolha de filosofia de logging já estabelecida no
      projeto (ver Review Triage Log de spec-7-5), não uma regressão desta
      story.
    location: 'backend/handlers/pedidos.go (BaixarReciboPedidoHandler)'
    severity: low
  - summary: >-
      GET /api/pedidos/{id}/recibo não define Cache-Control: no-store, mesmo
      contendo solicitante/obra/aprovador/itens no corpo do PDF.
    evidence: |-
      Mesmo padrão pré-existente de ExportarCatalogoHandler
      (handlers/produtos.go) — nenhum endpoint de download binário do
      projeto define cache headers hoje; não é uma regressão introduzida por
      esta story.
    location: 'backend/handlers/pedidos.go (BaixarReciboPedidoHandler)'
    severity: low
  - summary: >-
      A quebra de página manual do recibo pode separar as duas linhas de um
      mesmo item (produto/categoria/estoque e a linha de quantidades) em
      páginas diferentes quando o Pedido tem muitos itens.
    evidence: |-
      Puramente cosmético — nenhum dado é perdido ou incorreto, só a quebra
      de página cai num ponto visualmente menos ideal em Pedidos com muitos
      itens (caso raro dado o uso típico do Carrinho).
    location: 'backend/services/pedidos.go (RenderizarReciboPedidoPDF)'
    severity: low
---

<intent-contract>

## Intent

**Problem:** Um Pedido já decidido (Story 7.5) não tem nenhum comprovante formal para o solicitante ou o Almoxarife — a única evidência da retirada fica só na tela.

**Approach:** Novo `GET /api/pedidos/{id}/recibo` gera um PDF no servidor (`signintech/gopdf`) a partir do snapshot já em `pedido_itens`/`pedidos` (nunca join ao vivo com `produtos`, AD-17), liberado ao dono ou a `almoxarife`+ (mesmo padrão AD-8 de `BuscarPedidoProprio`). Um Pedido `aprovado`/`parcialmente_aprovado` gera o PDF; `pendente`/`rejeitado` devolvem `409`. Botão "Baixar recibo" novo no Dialog "Ver itens" (`FilaPedidosSection`, `MeusPedidosSection`) quando o Pedido está decidido.

## Boundaries & Constraints

**Always:**
- Rota `GET /api/pedidos/{id}/recibo`, registrada atrás só de `RequireAuth` (molde EXATO de `GET /api/pedidos/{id}`, main.go:625-626) — SEM `RequireRole`: dono ou `almoxarife`+ acessa, mesmo mínimo de papel de `BuscarPedidoHandler`.
- Separar EXPLICITAMENTE "montar o conteúdo" de "desenhar o PDF" (Design Notes): `MontarReciboPedidoConteudo(db, pedidoID, usuarioID, papel string) (ReciboPedidoConteudo, error)` chama `BuscarPedidoProprio` PRIMEIRO (reaproveita auth + o mesmo colapso 404 de `ErrPedidoNaoEncontrado` — dono, `almoxarife`+, id malformado/inexistente, Pedido alheio, tudo no MESMO erro), depois checa `det.Status` (fora de `{aprovado, parcialmente_aprovado}` -> novo `ErrPedidoSemRecibo`, 409 CONFLICT) e só então busca aprovador/monta `ReciboPedidoConteudo`/`ReciboPedidoItem` (structs simples, sem gopdf). `RenderizarReciboPedidoPDF(conteudo) ([]byte, error)` só desenha a partir do struct já pronto, puro e determinístico. `GerarReciboPedidoPDF(db, pedidoID, usuarioID, papel string) ([]byte, error)` = as duas em sequência — é o que o handler chama.
- Aprovador: `SELECT u.nome FROM pedidos p JOIN usuarios u ON u.id = p.decidido_por WHERE p.id = $1` (nova query dedicada — NUNCA estende o SELECT de `BuscarPedidoProprio`/JSON de `GET /api/pedidos*`, que continua sem `decididoPor`/`decididoEm`, Never de spec-7-5). Nome do aprovador é resolvido no momento (não é join com PRODUTOS — AD-17 não se aplica a `usuarios`); documentar essa leitura em Design Notes.
- Data do recibo = `pedidos.decidido_em` (já gravado atomicamente por `DecidirPedido`, Story 7.5) — nunca `criado_em`, nunca um timestamp de geração/download (`time.Now()`): o PDF nunca embute metadata dinâmica (sem `pdf.SetInfo` com data corrente) para permanecer byte-idêntico entre downloads do MESMO Pedido (AC "conteúdo não muda").
- Fonte: `pdf.AddTTFFontData("gofont", goregular.TTF)` de `golang.org/x/image/font/gofont/goregular` (dependência JÁ pinada, `go.mod` atual — zero dependência nova só para acentuação PT-BR; `signintech/gopdf` precisa de TTF embutido para caracteres acentuados, fontes core não bastam). `go get github.com/signintech/gopdf@v0.38.0` (única dependência nova, confirmada via `go list -m -versions` nesta rodada).
- Cada item do recibo mostra Produto/Categoria/Estoque (origem) e "Retirado" (`quantidadeAprovada`, formatado via `strconv.FormatFloat(v, 'f', -1, 64)`, molde de `movimentacoes.go:148`); quando `quantidadeAprovada != quantidade`, TAMBÉM mostra "Solicitado" e "Pendente" (`quantidade - quantidadeAprovada`) — mesma regra condicional já usada por `FilaPedidosSection` (linha ~338-375, Story 7.5), nunca escondido quando diverge.
- Sucesso -> `200`, `Content-Type: application/pdf`, `Content-Disposition: attachment; filename="recibo-pedido-{id}.pdf"`, corpo = bytes do PDF (molde EXATO de `ExportarCatalogoHandler`, handlers/produtos.go:420-464).
- Frontend: `buscarReciboPedidoBlob(id): Promise<Blob>` novo em `lib/pedidos.ts` (fetch + `authHeaders()` + tratamento de erro, molde dos demais exports do módulo — SEM lógica de DOM, mantendo o módulo livre de estado/efeito). Botão "Baixar recibo" em `FilaPedidosSection`/`MeusPedidosSection`: cada Dialog "Ver itens" ganha seu PRÓPRIO handler local (molde EXATO de `aoExportar`, CatalogoListagem.tsx:357-380 — `Blob` -> `URL.createObjectURL` -> `<a download>` clicado e removido -> `URL.revokeObjectURL`), guarda de duplo-clique local (`baixandoRecibo`, boolean), `toast.error` na falha. Visível só quando `detalheDe?.status === 'aprovado' || detalheDe?.status === 'parcialmente_aprovado'`. Sem tela intermediária, sem `ConfirmDialog` (ação não-destrutiva, EXPERIENCE.md linha 53).

**Block If:** _Nenhuma decisão bloqueante — rota, autorização, gate de status e fonte já fixados por precedente direto (`BuscarPedidoProprio`, `ExportarCatalogoHandler`, `aoExportar`) e pela stack pinada em epics.md._

**Never:**
- Não adiciona coluna nova nem migration — `decidido_por`/`decidido_em`/`quantidade_aprovada` já existem (migração 000027, Story 7.5).
- Não expõe `decididoPor`/`decididoEm` no JSON de `GET /api/pedidos`/`GET /api/pedidos/{id}` — a query do aprovador é exclusiva do caminho do PDF.
- Não anexa o PDF a e-mail nem gera automaticamente na decisão — só sob demanda via este endpoint (PRD FR-26, `[ASSUMPTION]`).
- Não implementa Story 7.7 (migração/vínculo com Histórico) nem toca `movimentacoes`.
- Não mostra papel/cargo do aprovador (só o nome) — o papel é mutável e não está capturado em snapshot; mostrar exigiria uma nova coluna fora do escopo desta story.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Pedido aprovado, dono | `GET /api/pedidos/{id}/recibo`, dono autenticado | `200`, `application/pdf`, corpo inicia com `%PDF-` | No error expected |
| Pedido parcialmente aprovado, almoxarife alheio | Mesmo Pedido, `almoxarife`+ não-dono | `200`, mesmo PDF | No error expected |
| Recibo baixado 2x, Produto editado entre downloads | Mesmo Pedido decidido, 2 chamadas, Produto do item editado entre elas | Os dois PDFs são byte-a-byte IDÊNTICOS | No error expected |
| Pedido pendente | Pedido `status='pendente'` | Requisição recusada, nenhum PDF gerado | `409 CONFLICT` |
| Pedido rejeitado | Pedido `status='rejeitado'` | Requisição recusada | `409 CONFLICT` |
| Pedido alheio, papel `usuario` | Pedido de outro dono, papel insuficiente | Nunca revela existência do Pedido | `404 NOT_FOUND` |
| Id malformado/inexistente | id não-UUID ou sem Pedido correspondente | Mesmo colapso de `BuscarPedidoHandler` | `404 NOT_FOUND` |
| Sem token | Nenhum `Authorization` | Recusada antes do handler | `401` |

</intent-contract>

## Code Map

- `backend/go.mod` -- `go get github.com/signintech/gopdf@v0.38.0` (nova); `golang.org/x/image` já presente, só passa a importar `font/gofont/goregular`.
- `backend/services/pedidos.go` -- `ErrPedidoSemRecibo` (novo); `ReciboPedidoConteudo`/`ReciboPedidoItem` (novos, sem tag json — uso interno); `MontarReciboPedidoConteudo` (busca+gate+formata, chama `BuscarPedidoProprio`)/`RenderizarReciboPedidoPDF` (só desenha)/`GerarReciboPedidoPDF` (compõe as duas, chamado pelo handler) — todas novas, descritas em Boundaries. Reaproveita `BuscarPedidoProprio` (linha ~410) inalterado.
- `backend/handlers/pedidos.go` -- `BaixarReciboPedidoHandler(db)` (novo, molde de `ExportarCatalogoHandler`, produtos.go:420-464, e de `BuscarPedidoHandler` para os erros 404/409).
- `backend/main.go` -- registrar `GET /api/pedidos/{id}/recibo` logo após `GET /api/pedidos/{id}` (~linha 626), atrás só de `RequireAuth`.
- `backend/services/pedidos_test.go` -- testes de `MontarReciboPedidoConteudo` (conteúdo correto por item, aprovador via join, gate de status) e de `RenderizarReciboPedidoPDF`/`GerarReciboPedidoPDF` (bytes começam com `%PDF-`, determinismo byte-a-byte entre duas chamadas com Produto editado no meio). Reaproveita `seedPedidoComItens`/`DecidirPedido` (Story 7.5) para montar Pedidos decididos.
- `backend/handlers/pedidos_test.go` -- helper novo `getRecibo(db, authHeader, id)`; testes cobrindo a I/O Matrix na fronteira HTTP (`Content-Type`/`Content-Disposition`, 409/404/401). Reaproveita `seedPedidoViaServico`+`services.DecidirPedido`.
- `frontend/src/lib/pedidos.ts` -- `buscarReciboPedidoBlob(id): Promise<Blob>` (novo) + `MENSAGEM_ERRO_RECIBO`.
- `frontend/src/lib/pedidos.test.ts` -- testes de `buscarReciboPedidoBlob` (URL/método/erro).
- `frontend/src/components/pedidos/FilaPedidosSection.tsx`+`.test.tsx` -- botão "Baixar recibo" no Dialog "Ver itens" (molde de `aoExportar`, CatalogoListagem.tsx:357-380).
- `frontend/src/components/pedidos/MeusPedidosSection.tsx`+`.test.tsx` -- mesmo botão, mesmo molde.
- `backend/handlers/produtos.go:420-464` (`ExportarCatalogoHandler`) / `frontend/src/components/catalogo/CatalogoListagem.tsx:352-380` (`aoExportar`) -- moldes diretos de download binário já revisados no repositório.

## Tasks & Acceptance

**Execution:**
- `backend/go.mod` -- `go get github.com/signintech/gopdf@v0.38.0` -- dependência de geração de PDF.
- `backend/services/pedidos.go` -- `ErrPedidoSemRecibo`+`ReciboPedidoConteudo`+`MontarReciboPedidoConteudo`+`RenderizarReciboPedidoPDF`+`GerarReciboPedidoPDF` -- regra de negócio e renderização do recibo.
- `backend/handlers/pedidos.go` -- `BaixarReciboPedidoHandler` -- fronteira HTTP do download.
- `backend/main.go` -- registrar `GET /api/pedidos/{id}/recibo`.
- `backend/services/pedidos_test.go`+`backend/handlers/pedidos_test.go` -- cobertura da I/O Matrix nos dois níveis, incluindo o teste de determinismo byte-a-byte.
- `frontend/src/lib/pedidos.ts`+`.test.ts` -- `buscarReciboPedidoBlob`.
- `frontend/src/components/pedidos/FilaPedidosSection.tsx`+`.test.tsx` -- botão "Baixar recibo".
- `frontend/src/components/pedidos/MeusPedidosSection.tsx`+`.test.tsx` -- botão "Baixar recibo".

**Acceptance Criteria:**
- Given um Pedido `aprovado` ou `parcialmente_aprovado` com itens de Produtos/Estoques diferentes, when o recibo é gerado, then o PDF traz nome/categoria/estoque/quantidade retirada de cada item, solicitante, nome do aprovador e a data da decisão (`decidido_em`).
- Given o mesmo Pedido decidido, when o recibo é baixado de novo depois do Produto referenciado ser editado, then os bytes do PDF são idênticos ao primeiro download.
- Given um Pedido ainda `pendente`, when alguém tenta baixar o recibo, then a chamada é recusada com `409`, sem nenhum PDF gerado.
- Given um Pedido `rejeitado`, when alguém tenta baixar o recibo, then a chamada é recusada com `409` (nenhuma retirada ocorreu).
- Given um Pedido já decidido visto na Fila ou em "Meus Pedidos", when o Dialog "Ver itens" é aberto, then o botão "Baixar recibo" aparece e, ao clicar, baixa o PDF sem tela intermediária.

## Spec Change Log

## Review Triage Log

### 2026-09-03 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 4 (high 0, medium 2, low 2)
- defer: 3 (high 0, medium 0, low 3)
- reject: 14
- addressed_findings:
  - `[medium]` `[patch]` `RenderizarReciboPedidoPDF` formatava `decidido_em` (`TIMESTAMPTZ`, chega em UTC) direto com `.Format("02/01/2006 15:04")`, sem converter para o fuso local — divergindo do padrão pt-BR (`toLocaleString('pt-BR')`, horário do navegador) usado em todo o resto do app; o recibo podia mostrar uma data/hora diferente da que o Almoxarife/solicitante viveram. Corrigido: converte para `America/Sao_Paulo` antes de formatar, com fallback seguro (rótulo explícito) se `time.LoadLocation` falhar.
  - `[medium]` `[patch]` `escreverLinha` desenhava toda linha com `pdf.Cell` (sem quebra) — nome de Produto/Categoria/Estoque ou a linha de quantidades muito longos estouravam a margem direita da página em vez de quebrar. Corrigido: passou a usar `MultiCell`, medindo a altura real ocupada antes de avançar `Y`.
  - `[low]` `[patch]` `baixandoRecibo` era um boolean único por componente (`FilaPedidosSection`/`MeusPedidosSection`), não escopado por Pedido — fechar o Dialog com um download em voo e abrir um Pedido DIFERENTE deixava o botão "Baixar recibo" do novo Pedido temporariamente desabilitado até a requisição antiga resolver. Corrigido: guarda agora escopada por id (`baixandoReciboId`), desabilitando só o botão do MESMO Pedido em download.
  - `[low]` `[patch]` A resolução do nome do aprovador via `JOIN usuarios` (ao vivo, não snapshot — decisão deliberada, AD-17 não cobre `usuarios`) não tinha nenhum teste travando esse comportamento intencional. Adicionado teste que muda `usuarios.nome` do decisor entre dois downloads e confirma que o SEGUNDO PDF reflete o nome NOVO — documenta a escolha como intencional, não um bug futuro.

## Design Notes

- **Conteúdo vs. renderização separados de propósito:** `MontarReciboPedidoConteudo` é testável com asserções Go simples (sem gopdf); `RenderizarReciboPedidoPDF` só precisa provar bytes válidos (`%PDF-`) e determinismo — evita depender de uma biblioteca de LEITURA de PDF (nenhuma no repo) para verificar texto embutido em fonte TTF (`AddTTFFontData` do gopdf usa subfont Unicode — não é ASCII simples no content stream).
- **Aprovador via join a `usuarios`, não snapshot:** AD-17 (spec-7-5, ARCHITECTURE-SPINE.md) fixa só a imutabilidade do snapshot em `PEDIDO_ITENS` contra `PRODUTOS`; não há coluna de snapshot para o nome do aprovador (migração 000027 só guarda o UUID). Resolver o nome atual de `usuarios` no momento do download é a leitura mínima suficiente — criar uma nova coluna de snapshot só para isso extrapolaria o que a story pede.
- **`golang.org/x/image/font/gofont/goregular` em vez de um `.ttf` novo no repo:** `golang.org/x/image` já é dependência direta pinada (go.mod); a fonte "Go Regular" cobre Latin Extended (acentuação PT-BR) sem adicionar nenhum arquivo binário nem dependência nova só para isso.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: build limpo com a dependência nova.
- `DATABASE_URL=postgres://stockflow:stockflow@127.0.0.1:5432/stockflow?sslmode=disable go test -p 1 -count=1 ./...` -- expected: todos os pacotes passando, incluindo os testes novos de recibo (conteúdo, determinismo, gate 409, fronteira HTTP).
- `cd frontend && npx tsc --noEmit && npx vitest run` -- expected: sem erro de tipo, suíte 100% passando (incluindo `pedidos.test.ts`, `FilaPedidosSection.test.tsx`, `MeusPedidosSection.test.tsx` estendidos).
