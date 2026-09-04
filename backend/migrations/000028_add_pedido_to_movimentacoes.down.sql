-- Reverte 000028 (Story 7.7): remove o índice e a coluna de vínculo
-- Movimentação↔Pedido. `pedido_id` é anulável e só a migração escreve nela,
-- então o DROP não perde dado de runtime.
DROP INDEX IF EXISTS idx_movimentacoes_pedido_id;
ALTER TABLE movimentacoes DROP COLUMN pedido_id;
