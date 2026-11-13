-- Reverte 000060: remove o snapshot de preço/unidade de `pedido_itens` —
-- nenhuma outra tabela/coluna depende dele.
ALTER TABLE pedido_itens
  DROP COLUMN unidade_medida,
  DROP COLUMN preco_unitario;
