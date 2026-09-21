-- Reverte 000041. Apaga as Movimentações `entrada` antes de restaurar o CHECK
-- de `tipo` (senão a constraint restaurada falharia) e antes de derrubar
-- `lotes` (FK de `movimentacoes.lote_id`).
DROP VIEW IF EXISTS saldo_produto_estoque;

DELETE FROM movimentacoes WHERE tipo = 'entrada';
ALTER TABLE movimentacoes DROP CONSTRAINT movimentacoes_tipo_check;
ALTER TABLE movimentacoes ADD CONSTRAINT movimentacoes_tipo_check
  CHECK (tipo IN ('baixa', 'transferencia', 'ajuste'));

DROP INDEX IF EXISTS idx_movimentacoes_lote_id;
ALTER TABLE movimentacoes DROP COLUMN lote_id;

DROP TABLE lotes;
