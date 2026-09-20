-- Reverte 000037: `contadores_produto` não é referenciada por nenhuma outra
-- tabela, então o DROP sozinho é suficiente — nenhum dado de `produtos`
-- (`codigo` já gravado) é afetado.
DROP TABLE contadores_produto;
