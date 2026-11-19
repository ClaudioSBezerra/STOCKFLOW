---
title: 'Pedido de Outras Saídas e Termo de Baixa'
type: 'feature'
created: '2026-11-19'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'b75ad22383f41b2fc158fb1603d8244ee939e427'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-20-context.md', '{project-root}/_bmad-output/implementation-artifacts/spec-pre-nota-layout-danfe.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** (Pedido da Karla, 2026-11-19.) O "Pedido de Baixa de Estoque" não diz POR QUE o material saiu, sai impresso junto com as transferências na mesma Pré-Nota e a tela de Aprovação em lote mistura os dois tipos.

**Approach:** (1) Renomear o programa para **Pedido de Outras Saídas** e exigir um **motivo**: *Baixa de Estoque por Perda ou Roubo*, *Produto Vencido ou Avariado* ou *Por Utilização na Obra da Empresa* (acrescentado pelo usuário em 2026-11-19), mais uma justificativa obrigatória. (2) Novo documento **Termo de Baixa**, no estilo DANFE da Pré-Nota mas com o MOTIVO em destaque e numeração PRÓPRIA (independente da Pré-Nota), gerado na aprovação em lote — um documento por motivo. (3) A Pré-Nota passa a cobrir só Transferências. (4) A tela Aprovação em lote separa **Transferências** de **Outras Saídas**. O botão **Registrar Baixa** do Detalhe do Produto (que hoje debita na hora, sem aprovação) passa a entrar no MESMO fluxo: vira um Pedido de Outras Saídas e deixa de existir baixa direta no servidor (decisão do usuário, 2026-11-19). Decisão da aprovação continua só do Gestor ou acima; Usuário e Almoxarife apenas solicitam (hoje já é assim e passa a ser coberto por teste).

## Boundaries & Constraints

**Always:**
- O valor interno `tipo = 'baixa'` em `pedidos` e `"baixa"` nas Movimentações NÃO muda; "Outras Saídas" é o nome exibido. Rota nova `/pedidos/outras-saidas`; `/pedidos/baixa` redireciona para ela. O endpoint `POST /api/pedidos/baixa` continua (ganha o campo `motivo`).
- Motivo: coluna nova `pedidos.motivo_saida` (`perda_roubo` | `vencido_avariado` | `utilizacao_obra`, NULL permitido, com CHECK `motivo_saida IS NULL OR tipo = 'baixa'`). Obra / centro de custo continua OPCIONAL em todos os motivos (decisão do usuário); quando informada aparece no Termo (quadro de justificativa/dados adicionais). Pedido NOVO de Outras Saídas exige motivo válido (senão 400) e **justificativa** (`observacao`) preenchida após trim (senão 400). Pedidos `baixa` antigos, sem motivo, continuam existindo e aprováveis: aparecem como "Outras saídas — motivo não informado".
- Termo de Baixa: tabelas novas `termos_baixa` (`id`, `empresa_id`, `numero` INTEGER > 0, `motivo_saida` NULL permitido, `decidido_por` REFERENCES usuarios, `criado_em`, UNIQUE `(empresa_id, numero)`) e `contadores_termo_baixa` (um por Empresa, mesmo molde de `contadores_pre_nota`, criado junto com a Empresa e com backfill das existentes); `pedidos.termo_baixa_id` (nullable, FK, com índice). Numeração sequencial por Empresa, atômica na MESMA transação da decisão (`UPDATE ... RETURNING`), INDEPENDENTE da Pré-Nota (Termo nº 1 e Pré-Nota nº 1 coexistem).
- Em `DecidirLotePedidos`, aprovação: Pedidos de **transferência** decididos nesta chamada seguem gerando UMA Pré-Nota (como hoje); Pedidos de **baixa** decididos nesta chamada NÃO entram em Pré-Nota — geram **um Termo de Baixa por motivo** (grupos em ordem fixa: `perda_roubo`, `vencido_avariado`, `utilizacao_obra`, sem motivo), cada grupo com seu número. Pedido "pulado" (já decidido) nunca entra em documento novo. Rejeição nunca gera documento. A API aceita lote misto (gera os dois tipos de documento); a tela nunca mistura.
- A resposta de `DecidirLotePedidos` e as leituras de Pedido (listas, detalhe) trazem `termoBaixaId` e `numeroTermo` (como `preNotaId`/`docnum`) e `motivoSaida`.
- Rota `GET /e/{slug}/api/termos-baixa/{id}/pdf`: `RequireAuth` + `RequireRole(PapelGestor)`, filtro por Empresa (outra Empresa/inexistente/malformado -> 404), `Content-Disposition: attachment; filename="termo-baixa-{numero}.pdf"`, `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`.
- PDF do Termo de Baixa, A4, mesmo padrão visual da Pré-Nota DANFE (reaproveite funções de cabeçalho, tabela, paginação, rodapé com "Página N", logo da Empresa, formatação de moeda/quantidade e determinismo — mesma entrada, mesmos bytes): cabeçalho em 3 caixas (Empresa+CNPJ+logo | **"TERMO DE BAIXA"** + "Documento interno de controle — não substitui a Nota Fiscal" | "Nº 000.001" + Emissão no fuso de São Paulo); logo abaixo, uma **faixa de destaque em largura total** (fundo escuro, texto branco em negrito, caixa alta) com o MOTIVO — `BAIXA DE ESTOQUE POR PERDA OU ROUBO`, `PRODUTO VENCIDO OU AVARIADO`, `SAÍDA POR UTILIZAÇÃO NA OBRA DA EMPRESA` ou `BAIXA DE ESTOQUE — MOTIVO NÃO INFORMADO`; quadro **ORIGEM DA SAÍDA** (Razão social, CNPJ, IE, endereço da Filial, Filial e Estoque — sem destinatário); tabela de produtos (Código | Descrição | Un. | Qtd. | Valor unit. | Valor total, linha entre itens, subtotal por estoque de origem, cabeçalho repetido ao quebrar página); total geral com o mesmo aviso de itens sem valor; quadro **JUSTIFICATIVA** listando, por pedido, solicitante + texto da justificativa; **DADOS ADICIONAIS** (Aprovador, Solicitantes); quadro **ASSINATURAS** com linhas em branco para Solicitante e Aprovador (Data, Nome legível, Assinatura). O rodapé de TODA página repete "TERMO DE BAIXA Nº N — <MOTIVO>". Valor e Unidade vêm do snapshot de `pedido_itens` (Story 20.5); Filial/Empresa do cadastro atual (mesma regra da Pré-Nota).
- Telas: **Aprovação em lote** com duas abas, **Transferências** e **Outras saídas**, cada uma com sua contagem, seleção e botões Aprovar/Rejeitar; trocar de aba limpa a seleção; um lote nunca mistura tipos. Outras Saídas mostram motivo e justificativa em cada linha. Após aprovar: Transferências -> botão "Pré-Nota nº N" (como hoje); Outras Saídas -> um botão "Termo de Baixa nº N" por documento gerado, só gestor+. Nas listas Fila e Meus Pedidos: badge "Outras saídas", motivo em texto curto, e botão "Termo de Baixa nº N" na linha quando houver (gestor+).
- Permissões: `POST /api/pedidos/baixa` e `/transferencia` continuam `RequireAuth` sem papel mínimo (Usuário e Almoxarife solicitam); `decisao-lote` e os PDFs continuam `RequireRole(PapelGestor)`. Tela e menu do Pedido de Outras Saídas ficam em `papelMinimo: 'usuario'`.
- **Registrar Baixa no mesmo fluxo de aprovação:** o diálogo "Registrar baixa" do Detalhe do Produto (`ProdutoDetalhePage.tsx`, estado `baixaEstoque`) deixa de chamar a baixa direta e passa a enviar um Pedido de Outras Saídas (`POST /api/pedidos/baixa`) para o Produto e o Estoque da linha: o diálogo ganha o **motivo** (3 opções, obrigatório), a **justificativa** (obrigatória) e a Obra / centro de custo (opcional), mantém quantidade e o "Disponível: N", e ao concluir mostra "Pedido de baixa enviado para aprovação do Gestor. O saldo ficou reservado." — o saldo NÃO é debitado nessa hora. Quem vê o botão não muda (as mesmas regras de papel de hoje).
- **Fechar a baixa direta no servidor:** `POST /e/{slug}/api/produtos/{id}/estoques/{estoqueId}/baixa` (handler em `backend/handlers/movimentacoes.go:~73`, rota em `backend/main.go:~771`) NUNCA mais debita: passa a responder `410 Gone` com a mensagem "A baixa direta foi substituída pelo Pedido de Outras Saídas, que precisa da aprovação do Gestor." para qualquer papel autenticado (sem tocar no banco). A função de serviço `RegistrarBaixa` (movimentacoes.go:381) permanece no código e testada pelos testes de serviço, mas nenhuma rota HTTP a chama.
- Toda criação de Empresa (e todo helper de teste que apaga Empresa) trata `termos_baixa`/`contadores_termo_baixa` como já trata `pre_notas`/`contadores_pre_nota` (Story 20.4).

**Ask First:** nenhuma — nome do documento, um-documento-por-motivo e justificativa obrigatória decididos pelo usuário em 2026-11-19.

**Never:** renomear `tipo='baixa'` no banco; mexer em `DecidirPedido`/Pedido de retirada; mexer no botão/fluxo direto "Transferir" do Detalhe do Produto e em `RegistrarTransferencia` (continuam como estão; o usuário só pediu a Baixa); reescrever Pré-Notas já emitidas (as antigas que contêm itens de baixa continuam baixando como hoje, sem mudança no desenho); mudar a escolha de lote do FEFO (o Termo registra o que o FEFO consumiu, sem permitir escolher lote específico nesta entrega).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Solicitar com motivo | Perda ou Roubo, justificativa "Furto no galpão 3, BO 123", 5 un | Pedido `pendente`, `motivo_saida=perda_roubo`, saldo reservado | N/A |
| Sem motivo / motivo inválido | `motivo` ausente ou "outro" | Nada gravado | 400 |
| Sem justificativa | observação vazia/espaços | Nada gravado | 400 |
| Usuário/Almoxarife solicita | papel `usuario` ou `almoxarife` | 201, pedido criado | N/A |
| Usuário/Almoxarife decide ou baixa PDF | `decisao-lote` ou `GET /termos-baixa/{id}/pdf` | Nada feito | 403 |
| Lote só Outras Saídas, 3 motivos | 2 `perda_roubo` + 1 `vencido_avariado` + 1 `utilizacao_obra` aprovados | 3 Termos (números consecutivos), cada pedido vinculado ao do seu motivo; nenhuma Pré-Nota | N/A |
| Utilização na obra | motivo `utilizacao_obra`, justificativa, obra informada ou não | Pedido aceito nos dois casos; Termo com a faixa "SAÍDA POR UTILIZAÇÃO NA OBRA DA EMPRESA" | N/A |
| Lote só Transferências | 3 transferências aprovadas | 1 Pré-Nota como hoje; nenhum Termo | N/A |
| Lote misto via API | 2 transferências + 2 baixas | 1 Pré-Nota + Termo(s) por motivo | N/A |
| Rejeição | `aprovar=false` | Nenhum documento; contadores não avançam | N/A |
| Numeração independente | Pré-Nota nº 7 já existe, primeiro Termo | Termo nº 1; Pré-Nota seguinte nº 8 | N/A |
| Duas Empresas / concorrência | lotes simultâneos | Cada Empresa numera a própria sequência, sem colisão nem buraco | N/A |
| Registrar Baixa no Detalhe do Produto | Almoxarife+ abre o diálogo, informa quantidade, motivo e justificativa | Cria Pedido de Outras Saídas `pendente` com reserva; NENHUM saldo debitado, nenhuma Movimentação gravada | N/A |
| Baixa direta via API | `POST .../produtos/{id}/estoques/{estoqueId}/baixa` com qualquer papel | Nada debitado nem gravado | 410 com a mensagem de substituição |
| Baixa legada sem motivo | pedido `baixa` antigo pendente | Aprovável; Termo "MOTIVO NÃO INFORMADO" | N/A |
| Pedido pulado | já decidido por outra requisição | Não entra em documento novo | N/A |
| PDF do Termo | Termo com 1 motivo, várias páginas | Faixa de destaque do motivo, justificativas, assinaturas, rodapé com nº e motivo em toda página | N/A |
| PDF de outra Empresa | id de Termo de outra Empresa/inexistente | Nada gerado | 404 |
| Pré-Nota antiga com baixa | Pré-Nota já emitida com itens de baixa | Continua baixando igual a hoje | N/A |

</frozen-after-approval>

## Code Map

- `backend/migrations/000063_*` (nova): `pedidos.motivo_saida` + CHECK; `termos_baixa`; `contadores_termo_baixa` (+ backfill de `empresas`); `pedidos.termo_baixa_id` + índice. Testar do ZERO (subir a API real contra banco vazio, versão 63, dirty=false) e down/up.
- `backend/services/empresas.go:~433` (insert de `contadores_pre_nota` em InserirEmpresa) e todos os `removerEmpresaDeTeste`/helpers de limpeza de teste que já tratam `pre_notas`/`contadores_pre_nota` (arquivos `*_test.go` em services, handlers, cmd/*, main_test.go): acrescentar `contadores_termo_baixa`/`termos_baixa`.
- `backend/services/pedidos.go:661` (`SubmeterPedidoBaixa`): parâmetro `motivo`, validações de motivo e justificativa, grava `motivo_saida`. `:1480-1700` (`DecidirLotePedidos`, bloco que gera a Pré-Nota ~1631-1660): separar ids decididos agora por tipo; Pré-Nota só com transferências; novo `proximoNumeroTermoBaixa` (molde de `proximoDocnumPreNota`, :1410) e geração de Termos por motivo na mesma transação, preenchendo `TermoBaixaID`/`NumeroTermo` na resposta. Listas/detalhe (`ListarPedidosProprios`, `ListarPedidosFila`, `BuscarPedidoProprio`, structs `Pedido`/`PedidoResumo`/`PedidoDetalhe`): `motivoSaida`, `termoBaixaId`, `numeroTermo` (LEFT JOIN `termos_baixa`).
- `backend/services/` novo `termos_baixa.go` (+ `_test.go`): `MontarTermoBaixaConteudo`, `RenderizarTermoBaixaPDF`, `GerarTermoBaixaPDF`, reaproveitando de `pre_notas.go:129,898,1363` (`MontarPreNotaConteudo`, `RenderizarPreNotaPDF`, `GerarPreNotaPDF`) o que for comum (cabeçalho/logo `prepararLogoPreNota`, tabela, paginação, moeda/quantidade, dados fiscais da Filial). Extraia o compartilhável para funções comuns em vez de copiar blocos; NÃO mude o resultado da Pré-Nota existente (rode os testes dela). Teste o conteúdo por funções puras (sem ler bytes de PDF), como foi feito na Pré-Nota.
- `backend/handlers/pedidos.go:228` (`SubmeterPedidoBaixaHandler`): campo `motivo`; `:604` (`BaixarPreNotaPDFHandler`) é o molde de `BaixarTermoBaixaPDFHandler`; `backend/main.go:~902,958` rotas (POST baixa sem papel; nova rota do PDF com `RequireRole(PapelGestor)`); `main_test.go`: teste pelo `newMux` REAL (usuário e almoxarife: POST baixa 201, decisao-lote 403, PDF 403; gestor passa).
- Baixa direta: `backend/handlers/movimentacoes.go:~73` e `backend/main.go:~771` (rota passa a `410 Gone`; ajuste `main_test.go` — o teste `TestNewMux_ProdutosBaixaRotaCarregaRequireRole` e correlatos — e os testes do handler `handlers/movimentacoes_test.go`); `frontend/src/pages/ProdutoDetalhePage.tsx:~98,279,337-340,~590` (diálogo `baixaEstoque`: troca a chamada direta por `submeterPedidoBaixa` de `lib/pedidos.ts`, com motivo/justificativa/obra) e `ProdutoDetalhePage.test.tsx`. Procure TODO texto/ajuda que descreva "registrar baixa" como imediata (comentários de tela, manual em `backend/services/manual.go`, `epic-20-context.md` não precisa mudar).
- Frontend: `frontend/src/components/shell/nav-items.ts:79` (label "Pedido de Outras Saídas", rota `/pedidos/outras-saidas`), `frontend/src/App.tsx:154` (rota nova + redirect de `/pedidos/baixa`), `frontend/src/pages/PedidoBaixaPage.tsx` (renomear para `PedidoOutrasSaidasPage`: título, select de motivo obrigatório com as duas opções e texto de ajuda, justificativa obrigatória, mensagens), `frontend/src/lib/pedidos.ts:36,182-217` (tipo `MotivoSaida`, `motivo` no envio, `motivoSaida`/`termoBaixaId`/`numeroTermo` nos tipos, `baixarTermoBaixaPDF`), `frontend/src/components/pedidos/TipoPedidoBadge.tsx:34` (rótulo "Outras saídas"), novo `BaixarTermoBaixaButton.tsx` (molde de `BaixarPreNotaButton.tsx`), `frontend/src/pages/AprovacaoLotePage.tsx:115` (abas Transferências | Outras saídas, seleção por aba, botões de documento após aprovar), `FilaPedidosSection.tsx`/`MeusPedidosSection.tsx` (badge, motivo, botão do Termo na linha; o texto "sem aprovação individual" aponta para Aprovação em lote). Procure TODO texto "Pedido de Baixa"/"pedido de baixa" (inclusive manual em `backend/services/manual.go`, ajudas e comentários de tela) e atualize para Outras Saídas.
- Testes de frontend: página de Outras Saídas (motivo com as 3 opções e justificativa obrigatórios, envio), abas da Aprovação em lote (separação, contagens, seleção limpa ao trocar, lote não mistura), botões do Termo, rota/redirect, nav-items.

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/000063_*.up/down.sql` -- motivo, termos, contador, vínculo
- [x] `backend/services/empresas.go` + helpers de teste -- contador do Termo nasce com a Empresa; limpezas de teste
- [x] `backend/services/pedidos.go` (+ testes) -- motivo/justificativa no envio; Pré-Nota só de transferências; Termos por motivo na aprovação; campos nas leituras; testes de TODAS as linhas da I/O Matrix (incluindo concorrência e 2 Empresas)
- [x] `backend/services/termos_baixa.go` (+ testes) -- conteúdo, layout em funções puras e PDF
- [x] `backend/handlers/` + `backend/main.go` (+ testes) -- campo `motivo`, rota do PDF, teste de papéis pelo `newMux` real
- [x] Baixa direta: rota `410`, diálogo do Detalhe do Produto enviando Pedido de Outras Saídas, testes (rota pelo `newMux` real: 410 para almoxarife e gestor; diálogo: motivo e justificativa obrigatórios, envia o pedido, não debita)
- [x] Frontend: renomeação, motivo/justificativa, abas da Aprovação em lote, botões do Termo, badge/motivo nas listas (+ testes)
- [x] Conferência visual obrigatória: PDF de amostra do Termo de Baixa para cada um dos 3 motivos (com a logo real em /tmp/claude-1000/-home-claudio-projetos-stockflow/bfd7d6cd-3af7-4e39-b2f7-5e982b85635a/amostras/logo-ferreira-costa.png e sem logo) e um com várias páginas, abrir com Read; e no navegador (backend + Vite locais, banco de teste novo): solicitar como usuário comum (motivo e justificativa obrigatórios), entrar como gestor, ver as duas abas separadas, aprovar um lote de cada tipo, baixar Pré-Nota e Termo de Baixa

**Acceptance Criteria:**
- Given um Usuário ou Almoxarife, when solicita um Pedido de Outras Saídas com motivo e justificativa, then o pedido é criado e só um Gestor ou acima consegue aprová-lo
- Given um lote de Outras Saídas aprovado, when a aprovação é commitada, then sai um Termo de Baixa por motivo, com numeração própria independente da Pré-Nota, e o PDF destaca o motivo
- Given o botão Registrar Baixa do Detalhe do Produto, when o usuário confirma, then é criado um Pedido de Outras Saídas pendente e nenhum saldo é debitado até o Gestor aprovar
- Given a baixa direta antiga via API, when alguém chama a rota, then recebe 410 e nada é debitado
- Given a tela de Aprovação em lote, when o Gestor abre, then Transferências e Outras Saídas aparecem separadas em abas
- Given `go build ./...`, `go test -p 1 ./...` (UMA suíte por vez), `npm run build` e `npx vitest run`, when rodados após a story, then terminam sem erro

## Spec Change Log

- **2026-11-19, renegociação humana (frozen alterado pelo usuário):** o usuário decidiu que "Registrar Baixa" (botão direto do Detalhe do Produto) deve estar no mesmo fluxo de aprovação. Remove a proibição anterior de mexer nesse botão e em `RegistrarBaixa`, e a decisão do Epic 20 (FR62) de manter a baixa direta lado a lado só vale agora para o "Transferir", que NÃO foi alterado. Terceiro motivo `utilizacao_obra` acrescentado no mesmo dia (Obra opcional em todos os motivos).

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test ./services/... -run 'Saida|Termo|PreNota|Lote'` -- expected: build limpo, testes passando
- `cd backend && go test -p 1 ./...` -- expected: suíte completa limpa (Postgres de teste, porta 55432; confira com `ps -eo cmd | grep '^go test' | grep -v grep | wc -l` que não há outra rodando)
- `cd frontend && npm run build && npx vitest run` -- expected: build limpo, suíte limpa

## Suggested Review Order

**Pedido de Outras Saídas (o ponto de entrada)**

- Exige motivo (3 opções) e justificativa (máx. 1000), aceita Obra opcional; só `pedidos.tipo='baixa'` interno não muda
  [`pedidos.go:684`](../../backend/services/pedidos.go#L684)

- Tela nova com seletor de motivo e justificativa obrigatória; `/pedidos/baixa` redireciona para ela
  [`PedidoOutrasSaidasPage.tsx:100`](../../frontend/src/pages/PedidoOutrasSaidasPage.tsx#L100)
  [`App.tsx:154`](../../frontend/src/App.tsx#L154)

**Aprovação e numeração**

- Contador próprio do Termo (independente da Pré-Nota), na mesma transação da decisão
  [`pedidos.go:1480`](../../backend/services/pedidos.go#L1480)

- Tabelas e vínculo novos, com CHECK dos 3 motivos
  [`000063_create_termos_baixa.up.sql`](../../backend/migrations/000063_create_termos_baixa.up.sql)

**Termo de Baixa (PDF)**

- Conteúdo e layout em funções puras; faixa de destaque do motivo, justificativas, assinaturas e rodapé com nº e motivo
  [`termos_baixa.go:295`](../../backend/services/termos_baixa.go#L295)

- Base de desenho compartilhada com a Pré-Nota (comprovada byte-idêntica em 6 PDFs)
  [`documento_pdf.go`](../../backend/services/documento_pdf.go)

**Acesso**

- PDF do Termo só gestor+, isolado por Empresa
  [`pedidos.go:648`](../../backend/handlers/pedidos.go#L648)
  [`main.go:964`](../../backend/main.go#L964)

- Baixa direta antiga responde 410 para qualquer papel; nada é debitado
  [`movimentacoes.go:44`](../../backend/handlers/movimentacoes.go#L44)

**Telas do Gestor**

- Aprovação em lote em abas (Transferências | Outras saídas), seleção limpa ao trocar, botões de Pré-Nota/Termo
  [`AprovacaoLotePage.tsx`](../../frontend/src/pages/AprovacaoLotePage.tsx)

- "Solicitar baixa" no Detalhe do Produto cria um Pedido de Outras Saídas, sem débito de saldo
  [`ProdutoDetalhePage.tsx`](../../frontend/src/pages/ProdutoDetalhePage.tsx)
