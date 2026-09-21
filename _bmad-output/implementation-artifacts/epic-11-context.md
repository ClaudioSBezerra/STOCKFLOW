# Epic 11 Context: Estoque Preciso — Lote, Validade e Reserva de Saldo

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

O saldo deixa de ser uma quantidade única por par Produto/Estoque e passa a ser rastreado por Lote com Data de Validade opcional. O consumo é FEFO automático (sem escolha manual de Lote). Enviar um Pedido passa a reservar de fato o saldo dos itens até a decisão do Almoxarife, fechando a corrida entre Pedidos concorrentes que hoje não é tratada. O objetivo é reduzir perda por vencimento, dar rastreabilidade de recebimentos e garantir que Baixa, Transferência e aprovação nunca vendam duas vezes o mesmo saldo físico.

## Stories

- Story 11.1: Lançamento de saldo inicial com Lote e Data de Validade
- Story 11.2: Migração do saldo existente para Lote legado
- Story 11.3: Reserva de saldo ao enviar Pedido
- Story 11.4: Baixa e Transferência consomem Lote automaticamente e respeitam saldo reservado
- Story 11.5: Aprovação de Pedido revalida contra reserva e debita Lote
- Story 11.6: Estoque e quantidade inicial saem do Cadastro de Produto

## Requirements & Constraints

- Lançamento de saldo (FR47): tela dedicada, restrita a `almoxarife`+ (papel `usuario` recebe 403). Informa Produto, Estoque, quantidade > 0 e Data de Validade opcional. Cada lançamento cria um Lote novo, sem sobrescrever Lote existente. Um Produto pode ter vários Lotes ativos no mesmo Estoque.
- Consulta de catálogo e detalhe (FR6/FR7): a quantidade total é a soma de todos os Lotes ativos, e o detalhe discrimina por Estoque. Lote vencido é apenas sinalizado visualmente. O saldo continua contável, nunca bloqueado nem oculto. Não há alerta proativo por e-mail. Lote sem validade aparece como "validade desconhecida", nunca com data inventada.
- Reserva (FR50): o saldo dos itens fica indisponível para qualquer outro Pedido ou Carrinho a partir do envio (FR22) até a decisão (FR25).
  - Catálogo e Estoque mostram saldo disponível separado do reservado. Clicar na quantidade reservada mostra para quais Pedidos e solicitantes ela vai.
  - Rejeição libera a reserva inteira. Aprovação parcial libera só a parte não aprovada.
  - Não há expiração automática nesta versão, então um Pedido pendente mantém o saldo reservado indefinidamente.
- Baixa (FR14) e Transferência (FR15) validam contra o saldo DISPONÍVEL (físico menos reservado), nunca contra o total. Quantidade zero, negativa ou origem = destino continuam rejeitadas. Na Transferência, a Data de Validade original é preservada no destino.
- Aprovação (FR25): revalida cada item contra a reserva do próprio Pedido, não contra o saldo livre. Falha aí só ocorre por bug de reserva. Nunca há sucesso parcial silencioso: devolve a lista exata de itens com problema. O papel do aprovador é revalidado na submissão. Débito, liberação da reserva e Movimentação são atômicos.
- Terminologia (FR21): a UI deve distinguir "Adicionar ao carrinho" (não trava saldo contra outros) de "Saldo reservado" (travado após o envio do Pedido).
- Cadastro de Produto (FR8): Estoque destino e quantidade inicial saem do formulário novo. Produto sem saldo aparece no Catálogo com quantidade 0, sem estado especial. Produtos já cadastrados não mudam.
- Concorrência (NFR7): toda escrita dependente de estado lido antes é atômica no servidor.

## Technical Decisions

- **Tabela `lotes`** (`id` UUID, `produto_id`, `estoque_id`, `quantidade`, `data_validade` nullable, `empresa_id`, `criado_em`). Substitui `produto_estoque`, que é descontinuada após a migração. `lotes` passa a ser a única fonte de saldo. Todas as tabelas de domínio são escopadas por `empresa_id`.
- **Tabela `reservas_pedido_item`** (`id`, `pedido_id`, `produto_id`, `estoque_id`, `quantidade`, `empresa_id`). É separada de `pedido_itens` para ter ciclo de vida independente. Reserva só é liberada por decisão explícita (aprovar, rejeitar ou parcial). Não há job, cron nem expiração.
- **Saldo disponível** é sempre calculado: soma de `lotes.quantidade` menos soma das reservas ativas. Nunca é coluna materializada. Baixa, Transferência e aprovação sempre revalidam contra esse cálculo, nunca contra o saldo bruto do Lote.
- **FEFO** é o único critério de consumo, com o mesmo ordenamento em Baixa, Transferência e aprovação: `ORDER BY data_validade NULLS LAST, criado_em ASC`. Lote sem validade só é consumido depois de esgotados os que têm validade real. Nenhuma tela ou endpoint expõe escolha manual de Lote.
- **Concorrência e lock:**
  - Toda escrita em `lotes.quantidade` usa `SELECT ... FOR UPDATE` e insere uma `MOVIMENTACOES` na mesma transação, sem exceção. Correção manual de saldo futura usaria o tipo `ajuste`.
  - Criar reserva também adquire o lock sobre as linhas de `lotes` afetadas ANTES de calcular o saldo disponível e inserir a reserva, mesmo sem escrever em `quantidade`. Sem isso, dois envios concorrentes reservariam o mesmo saldo.
  - Com múltiplas linhas (Pedido com N itens), o conjunto completo é ordenado por `(produto_id, estoque_id, lote_id)` ascendente antes de qualquer lock. Nunca se usa a ordem do carrinho.
- **Migração (11.2):** cada linha de `produto_estoque` vira um Lote legado com `data_validade = NULL` e a mesma quantidade. `empresa_id` é copiado da linha de origem, nunca recalculado. Deve ser idempotente e disparada manualmente por uma pessoa, nunca por agente autônomo (corte único humano-disparado).
- **Produto sem Lote** aparece no Catálogo com quantidade 0. Quem não quer vê-lo usa o filtro "com estoque" já existente. Nenhum status novo como "rascunho".
- **Efeitos em outras áreas já decididos:**
  - Importação em massa gera Lote com `data_validade = NULL`. A planilha não ganha colunas de Lote nem de Validade.
  - Mesclagem de duplicatas reescreve `produto_id` também em `lotes` e `reservas_pedido_item` do produto removido.
  - Exclusão de Estoque é bloqueada se houver qualquer Lote (mesmo zerado) ou reserva ativa naquele Estoque. Lote zerado nunca é apagado automaticamente.

## UX & Interaction Patterns

- Lançamento de Saldo é tela dedicada e o único caminho para dar entrada de saldo inicial. Data de Validade é opcional.
- Lote vencido usa sinalização visual de aviso (âmbar, com texto de badge no token de contraste adequado). Nunca use a cor de erro/destrutivo para isso.
- Saldo total, reservado e disponível aparecem de forma distinta. O valor reservado é clicável e abre o detalhe de Pedidos e solicitantes.
- O fluxo de reserva no detalhe do Produto tem toast e badge no ícone de Carrinho. O texto precisa deixar claro que isso NÃO trava saldo contra outros usuários.

## Cross-Story Dependencies

- 11.1 e 11.2 criam `lotes`. Tudo o mais depende deles. 11.2 pressupõe que o backfill de `empresa_id` (Epic 9, Story 9.4) já rodou.
- 11.3 depende de `lotes` e define a reserva que 11.4 e 11.5 consomem. Os três seguem o mesmo protocolo de lock e o mesmo cálculo de saldo disponível.
- 11.4 e 11.5 compartilham o critério FEFO. Convém uma única implementação reutilizada.
- 11.5 e 11.3 estendem o fluxo de Pedidos (Epic 7: envio, aprovação, rejeição, aprovação parcial, item a item). A liberação da reserva se encaixa nessas decisões.
- 11.6 depende de 11.1 (caminho único de entrada de saldo). Se as Stories 10.1 a 10.4 já rodaram, o formulário já tem os campos novos e a listagem já tem colunas explícitas.
- Baixa, Transferência e Histórico (Epic 5), Catálogo (Epic 4) e exclusão de Estoque (Epic 2) passam a ler de `lotes` e das reservas.
