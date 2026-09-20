-- Reverte 000038: remove as 4 colunas aditivas de `produtos` e o tipo enum
-- `unidade_medida_produto` — nenhuma outra tabela/coluna depende deles.
ALTER TABLE produtos
  DROP COLUMN codigo_fornecedor,
  DROP COLUMN ean13,
  DROP COLUMN unidade_medida,
  DROP COLUMN embalagem;

DROP TYPE unidade_medida_produto;
