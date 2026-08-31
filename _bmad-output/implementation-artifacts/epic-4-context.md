# Epic 4 Context: Catálogo — Consulta, Descoberta e Exportação

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Este épico entrega os meios pelos quais qualquer Usuário autenticado encontra e consulta material disponível no catálogo: busca com sugestões, filtros combináveis, alternância entre grade e tabela agrupada, detalhe por Estoque atualizado quase em tempo real, identificação rápida via QR Code/código de barras, e exportação da tabela para Excel. Importa porque é o ponto de entrada de todo o produto — a jornada mais comum (encontrar material sobrando antes de comprar de novo) depende inteiramente destas telas, e é onde a promessa "quantidade sempre confiável antes de reservar" precisa se sustentar mesmo com dado mudando em tempo real. Opera sobre Produtos/Estoques/fotos já populados pelo Epic 3.

## Stories

- Story 4.1: Busca por nome/código/categoria com sugestões
- Story 4.2: Filtros por categoria, estoque e disponibilidade
- Story 4.3: Visualização em grade e tabela agrupada
- Story 4.4: Detalhe do produto por Estoque com atualização em tempo real
- Story 4.5: Identificação de Produto via QR Code / código de barras
- Story 4.6: Exportação da tabela do catálogo para Excel

## Requirements & Constraints

- Busca por nome/código/categoria retorna até 7 sugestões por relevância, atualizando conforme o usuário digita; sem resultado, mensagem "Nenhum produto encontrado para '{busca}'." sem sugestão de comprar externamente.
- Filtros por categoria, estoque e "Com estoque" (quantidade > 0 em ao menos um Estoque) são combináveis simultaneamente entre si e com a busca por texto — sempre E lógico, nunca um substituindo o outro.
- Visualização alterna entre grade (cards) e tabela agrupada em viewport ≥768px; abaixo disso, grade é sempre o padrão. Tabela agrupa produtos por mesmo nome/unidade/dimensões, somando quantidades; expandir uma linha mostra a quantidade discriminada por Estoque.
- Detalhe do Produto mostra a quantidade exata por Estoque; some material só é confiável se refletir o estado real no momento da consulta — não um snapshot antigo.
- Código de Identificação (mesmo código/SKU já cadastrado no Produto) é reaproveitável como valor de QR Code/código de barras impresso fisicamente pela empresa hoje — o sistema não gera nem imprime etiquetas. Leitura reconhecida abre o detalhe do Produto ou adiciona ao Carrinho, conforme o contexto de uso (consulta vs. montagem de pedido, este último realizado pelo Carrinho de outro épico). Produto sem código cadastrado continua acessível normalmente por busca textual.
- Câmera do scanner exige contexto seguro (HTTPS); em ambiente sem HTTPS a funcionalidade fica indisponível com mensagem explicando o motivo. Falha de leitura (sem permissão, sem hardware, código não reconhecido) mostra mensagem clara e mantém o campo de busca por texto disponível e em foco — o scanner nunca é a única forma de encontrar um Produto.
- Exportação Excel reflete exatamente os filtros ativos da visualização em tabela, com subtotais dinâmicos por grupo (fórmula `SUBTOTAL`, não soma estática, para permanecer correta mesmo com filtro aplicado na planilha já exportada); filtro com zero resultados ainda gera `.xlsx` válido, só com cabeçalho. Exportação é restrita a `almoxarife`+; chamada direta à API por `usuario` retorna 403.
- NFR de desempenho: busca/listagem do catálogo ≤300ms p95 sob carga típica (até 8.000 produtos, 30 Estoques) — vale para Stories 4.1–4.3.
- Usabilidade em campo: toda tela do fluxo principal (busca, leitura de QR Code) funcional a partir de 360px de viewport, testada em Chrome Android e Safari iOS; alvo de toque mínimo 48px, calibrado para uso com luvas.

## Technical Decisions

- Backend em camadas: `handlers/produtos.go` (fronteira HTTP, valida input) → `services/` (busca, filtro combinado, exportação) → `database/sql` explícito, sem ORM; nenhum acesso a banco fora de `services/`. Autorização por rank de papel decidida em `middleware/` (`adm=4 > gestor=3 > almoxarife=2 > usuario=1`); export de Excel exige checagem de rota exigindo `almoxarife`+.
- Dimensões (comprimento, largura, diâmetro, altura, espessura) são sempre `{valor: numeric, unidade: enum}` estruturado — a busca/exibição do catálogo lê esse formato diretamente, nunca faz parsing de texto livre.
- Atualização quase em tempo real via broadcaster in-process + SSE (sem Redis/RabbitMQ): canal dedicado `produtos`, envelope de evento fixo `{"resource":"produtos","id":"<uuid>","change":"created"|"updated"|"deleted"}`, payload sempre mínimo — o cliente rebusca via GET, nunca confia no payload do evento como dado atual. Conexão `EventSource` autenticada via ticket de curta duração (`POST /api/realtime/ticket`, TTL 30s, uso único) na query string; token de sessão nunca aparece ali. Ao reconectar, o cliente sempre faz um GET completo do estado atual — nunca espera replay de eventos perdidos. Só correto com instância única da aplicação.
- Exportação Excel via `qax-os/excelize` (v2.11.0), gerada em `services/relatorios.go`.
- Convenções transversais: envelope de erro `{"error":{"code","message"}}` com vocabulário fixo (inclui `FORBIDDEN`, `VALIDATION_ERROR`); logging via `log/slog`; UUID v4 como id; `timestamptz` UTC no banco.

## UX & Interaction Patterns

- Catálogo é a superfície inicial pós-login, papel mínimo `usuario`, alcançável pelo rail (desktop)/bottom nav (mobile). Card de Produto (grade, mobile) mostra foto, nome e badge `status-disponivel` sempre com ícone + texto ("Disponível"/"Sem estoque"), nunca só cor. Linha agrupada (tabela, desktop) expande ao clicar para revelar quantidade por Estoque.
- `fab-scanner` (botão circular flutuante, canto inferior direito) presente só em Catálogo e Carrinho, nunca em telas administrativas. Toque solicita permissão de câmera se necessário; leitura reconhecida fecha a câmera e navega direto ao resultado.
- Indicador de tempo real: toast discreto `aria-live="polite"` ("Catálogo atualizado.") quando um evento SSE chega, nunca recarrega a tela sozinho — usuário decide quando olhar. Reconexão que demora mais que alguns segundos mostra indicador persistente "Reconectando..."; reconexão rápida permanece silenciosa.
- Código de Produto/SKU e valor lido do QR Code sempre em `JetBrains Mono`, nunca na fonte sans.
- Paginação, nunca scroll infinito, dado o volume de até 8.000 produtos. Atalho de teclado `/` foca a busca (desktop); `Esc` fecha a câmera do scanner aberta.
- Botão "Exportar" na visualização em tabela (aba/ação visível só a `almoxarife`+, some da navegação para `usuario`).

## Cross-Story Dependencies

- Story 4.2 combina com Story 4.1 (busca + filtros como E lógico) — não são caminhos independentes.
- Story 4.3 (tabela agrupada) e Story 4.4 (detalhe por Estoque) compartilham a mesma exibição de "quantidade discriminada por Estoque": expandir uma linha agrupada na tabela mostra o mesmo dado que abrir o detalhe do Produto.
- Story 4.4 depende do canal SSE `produtos` (infraestrutura `realtime/` compartilhada com os demais épicos que publicam nesse mesmo canal, ex. Epic 3 em criação/edição de Produto).
- Story 4.5 abre o detalhe do Produto (Story 4.4) por padrão; quando usado a partir da tela de Carrinho, adiciona o item ao Carrinho em vez disso — depende da feature de Carrinho (fora deste épico).
- Story 4.6 exporta exatamente o que a Story 4.3 (tabela) e a Story 4.2 (filtros) produzem como resultado filtrado — não reimplementa a lógica de filtro/agrupamento separadamente.
- Todas as stories deste épico consomem Produtos/Estoques/fotos populados pelo Epic 3 (cadastro, importação, migração legada).
