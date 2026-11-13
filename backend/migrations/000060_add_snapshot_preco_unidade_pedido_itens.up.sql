-- Story 20.5 (Epic 20, Documento impresso da Pré-Nota), spec-20-5.
--
-- Snapshot de `preco_unitario` e `unidade_medida` do Produto em
-- `pedido_itens`, gravado por `decidirUmPedidoDoLoteTx` (services/pedidos.go)
-- no ramo de APROVAÇÃO em lote, na MESMA transação da decisão. O PDF da
-- Pré-Nota lê SEMPRE daqui, nunca do cadastro atual de `produtos` — editar o
-- preço/unidade do Produto depois nunca muda um documento já emitido. Só
-- preço e unidade são congelados: código do produto, Filiais, Empresa e
-- Aprovador seguem o cadastro atual (decisão do usuário, spec-20-5).
--
-- Mesmos tipos das colunas de origem: `produtos.preco_unitario NUMERIC(12,2)`
-- (migration 000055) e `produtos.unidade_medida unidade_medida_produto`
-- (migration 000038). Ambas NULLABLE e SEM backfill: Produto sem preço
-- cadastrado, Pedido de retirada (DecidirPedido não grava snapshot) e
-- Pré-Notas anteriores a esta migration ficam NULL — exibidas como "—".
ALTER TABLE pedido_itens
  ADD COLUMN preco_unitario NUMERIC(12, 2) NULL,
  ADD COLUMN unidade_medida unidade_medida_produto NULL;
