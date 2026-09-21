-- Reverte 000045: remove `pedidos.centro_custo_id` e `centros_custo`.
DROP INDEX IF EXISTS idx_pedidos_centro_custo_id;
ALTER TABLE pedidos DROP COLUMN IF EXISTS centro_custo_id;
DROP TABLE IF EXISTS centros_custo;
