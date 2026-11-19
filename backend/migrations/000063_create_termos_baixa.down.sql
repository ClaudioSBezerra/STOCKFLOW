-- Reverte 000063: vínculo em pedidos primeiro, depois as tabelas.
DROP INDEX IF EXISTS idx_pedidos_termo_baixa_id;
ALTER TABLE pedidos DROP COLUMN termo_baixa_id;
DROP TABLE termos_baixa;
DROP TABLE contadores_termo_baixa;
ALTER TABLE pedidos DROP CONSTRAINT pedidos_motivo_saida_so_baixa;
ALTER TABLE pedidos DROP COLUMN motivo_saida;
