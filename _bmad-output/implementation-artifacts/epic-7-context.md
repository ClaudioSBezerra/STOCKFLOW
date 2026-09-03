# Epic 7 Context: Pedidos de Retirada

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Fechar o ciclo completo de retirada de material: qualquer Usuário monta um Carrinho de itens disponíveis, envia como Pedido formal, o Almoxarife decide (aprova/rejeita) com o estoque revalidado no exato momento da decisão — nunca um sucesso parcial silencioso — e qualquer parte interessada baixa um recibo em PDF fiel ao que foi decidido. O objetivo de negócio é dar confiança ao Almoxarife (o estoque mostrado na aprovação é real) e transparência ao solicitante (status sempre visível, comprovante formal disponível).

## Stories

- Story 7.1: Carrinho de reserva
- Story 7.2: Envio de Pedido
- Story 7.3: Consulta de Pedidos próprios
- Story 7.4: Consulta de todos os Pedidos (Fila, Almoxarife+)
- Story 7.5: Aprovação/rejeição com revalidação de estoque item a item
- Story 7.6: Recibo do Pedido em PDF gerado pelo servidor
- Story 7.7: Migração de Pedidos e vínculo com Histórico

## Requirements & Constraints

- Adicionar item ao Carrinho valida disponibilidade no momento, somando quantidades já reservadas no próprio carrinho para o mesmo Produto/Estoque.
- Item de Carrinho cujo Produto ou Estoque some (mesclado ou excluído) é removido automaticamente na próxima abertura do Carrinho, com aviso do motivo.
- Envio de Pedido rejeita carrinho vazio e revalida disponibilidade de cada item de novo no momento do envio (não confia no snapshot da montagem do carrinho).
- "Solicitante" é sempre texto livre no formulário, mas identidade real usada em auditoria/"Meus Pedidos" é sempre a do Usuário autenticado, nunca o texto digitado.
- Consulta de Pedidos: Usuário comum vê só os próprios; Almoxarife+ vê todos, filtrável por status — é filtro de escopo, nunca erro 403 para quem tem menos papel.
- Acesso direto por id a um Pedido de outro Usuário é negado ao dono não-Almoxarife.
- Aprovação/rejeição revalida estoque item a item no servidor no momento exato da decisão; item com estoque insuficiente permite aprovação parcial explícita (nunca aprovação total silenciosa que ignora o item); o restante não atendido vira pendência separada, nunca é descartado.
- Papel do aprovador é revalidado na submissão da decisão, não no carregamento da tela.
- Recibo em PDF só existe para Pedido já decidido (aprovado ou parcialmente aprovado); Pedido pendente não oferece a opção.
- Conteúdo do recibo nunca muda depois de gerado, mesmo que o Produto referenciado seja editado depois.
- Toda escrita de estoque dependente de leitura prévia é atômica no servidor (NFR7) — vale tanto para a adição ao carrinho quanto para o débito na aprovação.
- Telas de Carrinho e Fila de Pedidos são parte do fluxo principal: funcionam em viewport a partir de 360px (NFR9).

## Technical Decisions

- Autorização por papel: hierarquia como ordem total (`adm=4 > gestor=3 > almoxarife=2 > usuario=1`), decisão sempre em middleware; filtro de escopo de listagem (Pedidos próprios vs. todos) é responsabilidade do `service`, consumindo o papel já resolvido no contexto da requisição, sem re-consultar o banco.
- Concorrência de estoque: toda escrita em `produto_estoque.quantidade` usa `SELECT ... FOR UPDATE`; toda escrita gera uma Movimentação correspondente na mesma transação. Ao aprovar um Pedido com múltiplos itens, o conjunto completo de pares `(produto_id, estoque_id)` é ordenado ascendentemente antes de adquirir qualquer lock — sobre o lote inteiro, nunca na ordem de inserção do carrinho.
- Atualização em tempo real via canal SSE fixo `pedidos` (entre os 4 canais existentes), envelope de evento `{resource, id, change}`, payload mínimo — cliente sempre rebusca via GET ao receber o evento ou ao reconectar (nunca espera replay de eventos perdidos).
- Recibo PDF (`signintech/gopdf`) é sempre renderizado a partir dos campos já capturados em `PEDIDO_ITENS` no momento do envio/aprovação (nome, unidade, estoque, quantidade, categoria) — nunca um join ao vivo com `PRODUTOS`. Vale tanto para download sob demanda quanto para qualquer anexo futuro.
- Camadas: `handlers/pedidos.go` → `services/` → acesso a dados via `database/sql`, sem framework web nem ORM.
- Convenções gerais do projeto: tabelas/colunas em português, UUID v4, `timestamptz` UTC, envelope de erro HTTP `{"error":{"code","message"}}`, logging estruturado via `log/slog`.
- Mesclagem de duplicatas (Epic 6) reescreve `produto_id` em `PEDIDO_ITENS` do produto removido para o sobrevivente antes do soft-delete — relevante para qualquer leitura histórica de Pedidos feita por esta epic.

## UX & Interaction Patterns

- `cart-badge`: contador circular sobre o ícone de Carrinho, preenchimento `destructive`; desaparece por completo com carrinho vazio (nunca mostra "0"); atualiza a cada adição/remoção, sempre acompanhado de toast de confirmação — o badge nunca é o único sinal da ação.
- `fab-scanner`: presente em Catálogo e Carrinho para adicionar item por QR Code/código de barras (ver Epic 4); nunca em telas administrativas.
- Badges de status (`status-pendente`/`status-aprovado`/`status-rejeitado`): formato pill, sempre ícone + texto (nunca só cor), texto na variante `text-on-tint-*` correspondente.
- Linha de aprovação item-a-item na Fila de Pedidos: mostra "Solicitado: X · Disponível: Y" em caso de divergência; nunca um botão único "Aprovar tudo" que esconde a divergência.
- Toda confirmação de ação assíncrona (adicionar/remover do Carrinho, aprovar/rejeitar Pedido) usa toast com `aria-live="polite"`.
- Ação destrutiva/irreversível (ex. rejeitar Pedido) sempre usa `AlertDialog` (`ConfirmDialog` reutilizável), nunca `window.confirm()`.
- Carrinho vazio mostra mensagem orientando buscar produto ou usar a câmera — nunca uma tela em branco.
- Botão "Baixar recibo" aparece só em Pedido já decidido, gera e baixa o PDF diretamente, sem tela intermediária.
- Navegação: "Carrinho" e "Pedidos" (Meus Pedidos / Fila) são itens de primeiro nível no rail/bottom nav; a aba "Fila" só é visível para Almoxarife+.
- Indicador de atualização em tempo real (toast discreto ao chegar evento SSE) e indicador de reconexão lenta ("Reconectando...") seguem o mesmo padrão dos demais canais.

## Cross-Story Dependencies

- Story 7.1 depende de Produto/Estoque já existirem com quantidade em `PRODUTO_ESTOQUE` (Epic 3) e pode ser alimentada pela leitura de QR Code/código de barras (Epic 4, Story 4.5).
- Story 7.2 completa o segundo guard de exclusão de Estoque deixado pendente na Story 2.2 (Epic 2): Pedido `pendente` referenciando um Estoque passa a bloquear sua exclusão a partir desta story.
- Story 7.5 usa o mesmo padrão de lock/Movimentação atômica já estabelecido nas Stories 5.1/5.2 (Epic 5) e publica no canal SSE `pedidos` como as demais listagens publicam nos seus canais.
- Stories 7.1–7.5 dependem da autorização por papel e do contexto de requisição estabelecidos na Story 1.5 (Epic 1), e do `ConfirmDialog`/`Toaster` globais estabelecidos na Story 1.2.
- Item em Carrinho ou Pedido pendente referenciando um Produto mesclado (Epic 6, Story 6.4) é redirecionado automaticamente para o Produto sobrevivente — tratado no momento da mesclagem, não nesta epic, mas consumido por 7.1/7.2.
- Story 7.7 depende da tabela de mapeamento id-antigo→id-novo e do Histórico já migrado na Story 5.4 (Epic 5) para preservar o vínculo Movimentação↔Pedido.
