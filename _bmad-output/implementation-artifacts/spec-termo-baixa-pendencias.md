---
title: 'Termo de Baixa: Solicitante baixa o seu Termo, listagem com reimpressão e justificativa por item'
type: 'feature'
created: '2026-11-19'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'da6c20794b1d4939bfcee90eb8e56723d833ab5a'
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-pedido-outras-saidas.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** (Pendências levantadas com a Karla; decisões do usuário em 2026-11-19.) (1) O Solicitante assina o Termo de Baixa mas não consegue baixá-lo (PDF é gestor+). (2) Não há tela para listar/buscar/reimprimir Termos. (3) O Termo não liga cada item à justificativa do pedido de origem nem mostra a quantidade solicitada.

**Approach:** (1) O PDF do Termo passa a poder ser baixado por Gestor+ (qualquer Termo da Empresa) e pelo Solicitante de pelo menos um pedido do Termo (só dos Termos dele). (2) Nova tela "Termos de Baixa" com lista e busca, e botão de baixar PDF. (3) O PDF mostra, por item, o pedido de origem: solicitante, quantidade solicitada, quantidade aprovada e a justificativa daquele pedido.

## Boundaries & Constraints

**Always:**
- Sem mudar numeração, contador, nem a geração do Termo na aprovação. Migration só se for indispensável (a ligação item -> pedido já existe via `pedido_itens.pedido_id`; `pedidos.termo_baixa_id` liga pedido -> Termo).
- **Acesso ao PDF** `GET /e/{slug}/api/termos-baixa/{id}/pdf`: `RequireAuth` + (Gestor+ qualquer Termo da Empresa | qualquer outro papel autenticado SOMENTE se for o Solicitante de ao menos um pedido do Termo — compare por id do usuário solicitante se `pedidos` o guarda; senão pelo mesmo campo que identifica o solicitante hoje, nunca só pelo nome em texto se houver id). Negado -> 404 (mesmo colapso de "não encontrado", sem vazar existência). Outra Empresa -> 404 (AD-20).
- **Listagem** `GET /e/{slug}/api/termos-baixa` (RequireAuth): Gestor+ vê todos os Termos da Empresa; demais papéis veem só os dos seus pedidos. Campos: id, número, motivo, data, aprovador, solicitantes, quantidade de itens. Filtros opcionais por query: `numero`, `motivo`, `de`/`ate` (data), `q` (texto: solicitante/aprovador). Paginação simples (limite padrão 50, máx 200, ordem decrescente por número). Isolada por Empresa.
- **Tela** (rota `/termos-baixa`, item de menu "Termos de Baixa" no grupo onde ficam Pedidos/Aprovações, visível a todos os papéis autenticados): tabela com número, motivo (pílula), data, aprovador, solicitantes, itens e botão "Baixar PDF"; filtros de número, motivo e período; estado vazio; erro com "Tentar de novo". Baixar PDF usa o mesmo mecanismo já usado para Pré-Nota/Termo no frontend.
- **PDF do Termo**: no quadro de itens, cada item exibe o pedido de origem: Solicitante, Quantidade solicitada, Quantidade aprovada e a Justificativa daquele pedido (quadro/coluna "Justificativa" por item; texto longo quebra linha sem estourar a página; pedido legado sem justificativa usa o texto já existente). Mantém logo, faixa do motivo, assinaturas e paginação. O quadro JUSTIFICATIVA geral atual pode ser substituído pela justificativa por item; evite repetir texto idêntico dezenas de vezes (agrupe itens do mesmo pedido sob um subtítulo com a justificativa uma vez, e liste os itens abaixo).
- Determinismo do PDF e layout puro testável (padrão de `layoutDoTermoBaixa`).
- Testes pelo `newMux` REAL: Gestor baixa qualquer Termo; Solicitante baixa o dele (200) e NÃO o de outro (404); Almoxarife/Usuário não solicitante 404; sem token 401; lista filtra por papel.

**Never:** mexer na Pré-Nota, no fluxo de aprovação, nos contadores; expor Termos de outra Empresa; alterar a regra de que a Pré-Nota continua gestor+.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Solicitante baixa o seu Termo | Usuário solicitou pedido no Termo | 200 PDF | N/A |
| Solicitante tenta Termo alheio | Termo sem pedido dele | Nada | 404 |
| Gestor/Adm | qualquer Termo da Empresa | 200 PDF | N/A |
| Outra Empresa / id malformado | id alheio | Nada | 404 |
| Lista do Solicitante | vários Termos | só os dele | N/A |
| Lista do Gestor com filtros | número/motivo/período/q | filtra, mais novos primeiro | filtro inválido -> 400 |
| Termo com 2 pedidos de justificativas diferentes | itens de cada pedido | cada item sob o pedido certo, com solicitante, qtd solicitada, aprovada e justificativa | N/A |
| Justificativa longa | 1500 caracteres | quebra de linha/página sem cortar | N/A |
| Pedido legado sem justificativa | observação vazia | texto "Sem justificativa informada…" | N/A |

</frozen-after-approval>

## Code Map

- `backend/main.go` (~962 rota do PDF; nova rota GET lista), `backend/handlers/` (BaixarTermoBaixaPDFHandler + novo handler de lista), `backend/services/termos_baixa.go` (+ testes: `MontarTermoBaixaConteudo`, `layoutDoTermoBaixa`, nova função de listagem e de autorização por solicitante).
- `frontend/src/components/shell/nav-items.ts`, `frontend/src/App.tsx`, nova página/seção `TermosBaixa*` (+ testes), reaproveitar o download de PDF existente (ver `AprovacaoLotePage`).
- `backend/services/manual.go` (nova seção/menção).

## Tasks & Acceptance

**Execution:**
- [x] Serviço: autorização por solicitante, listagem com filtros/papel, conteúdo do Termo com itens agrupados por pedido (solicitado/aprovado/justificativa) + testes
- [x] Handlers/rotas: PDF aberto ao Solicitante dono; GET lista; testes pelo newMux real
- [x] PDF: layout por pedido com justificativa por item; testes de layout puro, determinismo, justificativa longa, legado
- [x] Frontend: tela Termos de Baixa, menu, rota, download, filtros; testes
- [x] Manual e textos
- [x] Conferência no navegador local (banco novo): gerar um Termo, ver como Solicitante e como Gestor, baixar PDF e olhar as páginas

**Acceptance Criteria:**
- Given um Solicitante, when abre Termos de Baixa, then vê e baixa só os Termos dos seus pedidos
- Given um Termo com pedidos de justificativas diferentes, when o PDF é gerado, then cada item aparece sob o seu pedido com solicitante, quantidade solicitada, aprovada e justificativa
- Given `go build ./...`, `go test -p 1 ./...` (uma suíte por vez), `npm run build` e `npx vitest run`, when rodados, then terminam sem erro

## Verification

- `cd backend && go build ./... && go vet ./... && go test ./services/... ./handlers/... -run 'Termo'`
- `cd backend && go test -p 1 -timeout 40m ./...` (DATABASE_URL=postgres://stockflow:stockflow@localhost:55432/stockflow?sslmode=disable)
- `cd frontend && npm run build && npx vitest run`

## Suggested Review Order

**Acesso (Solicitante baixa o seu Termo)**

- PDF: Gestor+ mantém o gate de MFA; os demais só baixam se solicitaram pedido com item aprovado no Termo (404 caso contrário)
  [`main.go`](../../backend/main.go)

- Regra de autorização e listagem por papel, com filtros, total e paginação
  [`termos_baixa.go`](../../backend/services/termos_baixa.go)

**PDF por pedido**

- Itens agrupados por pedido: solicitante, quantidade solicitada/aprovada e justificativa uma vez; cabeçalho nunca órfão
  [`termos_baixa.go`](../../backend/services/termos_baixa.go)

- Colunas do Termo isoladas das da Pré-Nota (campo explícito, sem truque por contagem)
  [`documento_pdf.go`](../../backend/services/documento_pdf.go)

**Tela**

- Termos de Baixa: lista, filtros, paginação e download
  [`TermosBaixaPage.tsx`](../../frontend/src/pages/TermosBaixaPage.tsx)
