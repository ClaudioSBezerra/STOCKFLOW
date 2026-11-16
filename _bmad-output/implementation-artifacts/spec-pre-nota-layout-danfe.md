---
title: 'Pré-Nota impressa no layout de DANFE'
type: 'feature'
created: '2026-11-16'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'aa0086e7d3636681a706b22e11ebf1239c780db2'
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-20-5-documento-impresso-da-pre-nota.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O PDF da Pré-Nota (Story 20.5) é uma lista de linhas soltas — o usuário achou ruim de ler e pediu algo "semelhante a um DANFE de nota fiscal": quadros separados, linhas (underline) entre as linhas da tabela, e Remetente e Destinatário bem identificados. Hoje origem/destino mostram só a Filial; numa transferência entre estoques da MESMA filial, origem e destino ficam idênticos e o documento não diz de onde para onde o material foi.

**Approach:** Redesenhar só a camada de layout do PDF (`RenderizarPreNotaPDF` e as funções puras de conteúdo em `backend/services/pre_notas.go`), no estilo DANFE, em A4 retrato: quadros com borda fina e rótulo pequeno em maiúsculas no canto superior esquerdo, tabela de produtos com cabeçalho em caixa e uma linha horizontal entre cada item. Acrescentar o NOME DO ESTOQUE de origem e de destino ao conteúdo. Dados, regras de preço/totais, rota, autorização e frontend NÃO mudam.

## Boundaries & Constraints

**Always:**
- Quadros, de cima pra baixo:
  1. **Cabeçalho** (uma caixa dividida em 3): esquerda = Empresa (razão social) + CNPJ; centro = "PRÉ-NOTA" em destaque + "Documento interno de controle — não substitui a Nota Fiscal"; direita = "Nº 000.001" (docnum com 6 dígitos e ponto de milhar) + "Emissão: dd/mm/aaaa hh:mm" (fuso de São Paulo, como hoje).
  2. **Para cada rota** (itens agrupados por par Estoque origem -> Estoque destino; Baixa agrupada por Estoque origem), na ordem estável atual:
     - Dois quadros lado a lado: **REMETENTE (ORIGEM)** e **DESTINATÁRIO (DESTINO)**, cada um com Empresa + CNPJ, Filial e Estoque (rótulo + valor). Em Baixa, o quadro de destino diz "BAIXA DE ESTOQUE — consumo, sem destinatário".
     - Linha "Natureza da operação: Transferência" ou "Baixa de estoque".
     - Quadro **DADOS DOS PRODUTOS** em tabela com colunas Código | Descrição do produto | Un. | Qtd. | Valor unit. | Valor total; cabeçalho da tabela em caixa; uma linha horizontal fina entre cada item; descrição longa quebra dentro da própria coluna; subtotal da rota ao final da tabela.
  3. **CÁLCULO DO TOTAL**: "Valor total dos produtos" — mesmo total e mesmo aviso de itens sem valor de hoje.
  4. **DADOS ADICIONAIS**: Aprovador e Solicitante(s).
  5. Rodapé em toda página: "Documento interno de controle — não substitui a Nota Fiscal" + "Página N".
- Quebra de página: um quadro de Remetente/Destinatário nunca fica separado do início da sua tabela; se a tabela continuar na próxima página, o cabeçalho da tabela se repete; uma linha de item nunca é dividida.
- Tudo que já vale continua valendo: valores e totais exatos (centavos/milésimos, sem float), "—" pra preço/unidade ausente, item com quantidade aprovada 0 não aparece, "Nenhum item movimentado nesta Pré-Nota", fuso de São Paulo, determinismo (mesma entrada -> mesmos bytes), fonte goregular com acentos.
- Nome do Estoque de origem vem do snapshot `pedido_itens.estoque_nome`; o de destino vem do cadastro atual (`estoques.nome` via `estoque_destino_id`), mesmo critério já aceito pra Filial.

**Ask First:** nenhuma — agrupamento por rota e o quadro de Baixa sem destinatário foram decididos para atender o pedido de "identificar bem Remetente e Destinatário" numa Pré-Nota que pode juntar rotas diferentes.

**Never:** mudar regras de preço/snapshot/totais, a rota `GET /api/pre-notas/{id}/pdf`, autorização, frontend ou o recibo de Pedido; inventar CNPJ/endereço de Filial (não existem no cadastro — Filial só tem nome).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Mesma filial, estoques diferentes | 3 transferências Almoxarifado Central -> Canteiro de Obras, mesma Filial | 1 bloco de rota: Remetente mostra Estoque "Almoxarifado Central", Destinatário "Canteiro de Obras"; 3 linhas na tabela | N/A |
| Rotas misturadas | 2 transferências de rotas diferentes + 1 Baixa | 3 blocos, cada um com seu Remetente/Destinatário e sua tabela; Baixa com "sem destinatário" | N/A |
| Muitos itens | Rota com itens suficientes pra passar de uma página | Tabela continua na página seguinte com o cabeçalho repetido; nenhum item cortado; rodapé com número da página | N/A |
| Sem itens | Todos os itens com quantidade aprovada 0 | Cabeçalho + "Nenhum item movimentado nesta Pré-Nota" | N/A |

</frozen-after-approval>

## Code Map

- `backend/services/pre_notas.go:70-91` (`PreNotaItem`/`PreNotaConteudo`) -- acrescentar `EstoqueOrigem`/`EstoqueDestino` (strings) ao item.
- `backend/services/pre_notas.go:100` (`MontarPreNotaConteudo`) -- ler `pi.estoque_nome` (snapshot) e `estoques.nome` do destino (LEFT JOIN por `estoque_destino_id`, com `empresa_id`).
- `backend/services/pre_notas.go:319-423` (`linhaPreNota`/`blocoPreNota`/`blocosPreNota`/`linhasPreNota`) -- substituir por um modelo de layout puro (ex.: grupos por rota + linhas da tabela já formatadas) que os testes conseguem conferir sem ler bytes de PDF.
- `backend/services/pre_notas.go:423` (`RenderizarPreNotaPDF`) -- desenhar quadros com `pdf.Line`/`pdf.RectFromUpperLeftWithStyle`, textos posicionados por coluna (`pdf.SetXY` + `pdf.Cell`/`SplitText`), tamanho de fonte pequeno nos rótulos (6-7pt) e maior nos valores (8-9pt), título "PRÉ-NOTA" em destaque.
- `backend/services/pre_notas_test.go` -- ajustar os testes de texto ao novo modelo; cobrir as 4 linhas da I/O Matrix.

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/pre_notas.go` -- estoques de origem/destino no conteúdo + modelo de layout por rota + renderização estilo DANFE
- [x] `backend/services/pre_notas_test.go` -- testes do modelo (I/O Matrix) + determinismo + PDF de várias páginas
- [x] Gerar um PDF de amostra com o cenário "Mesma filial, estoques diferentes" (3 itens, R$ 12,00, quantidades 5/10/8, Empresa "Fortes Bezerra Tecnologia e Consultoria Ltda", CNPJ 38149716000128, Filial "FBTECHIA - Treinamento") e outro com rotas misturadas + muitos itens, salvar em `/tmp/claude-1000/-home-claudio-projetos-stockflow/bfd7d6cd-3af7-4e39-b2f7-5e982b85635a/scratchpad/`, e ABRIR com a ferramenta Read pra conferir visualmente bordas, alinhamento das colunas, linhas entre itens e quebra de página antes de dar por pronto

**Acceptance Criteria:**
- Given uma Pré-Nota com transferências entre estoques da mesma filial, when o Gestor baixa o PDF, then Remetente e Destinatário mostram estoques diferentes e dá pra saber de onde para onde o material foi
- Given `go build ./...`, `go test -p 1 ./...`, `npm run build` e `npx vitest run`, when rodados após a mudança, then terminam sem erro

## Verification

**Commands:**
- `cd backend && go build ./... && go test ./services/... -run 'PreNota'` -- expected: build limpo, testes passando
- `cd backend && go test -p 1 ./...` -- expected: suíte completa do backend limpa (DATABASE_URL do Postgres de teste na porta 55432)
- `cd frontend && npm run build && npx vitest run` -- expected: sem mudança no frontend; ainda verde
