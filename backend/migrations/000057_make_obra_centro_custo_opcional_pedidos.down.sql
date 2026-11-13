-- Remove o CHECK antes do backfill/NOT NULL (ordem inversa do up.sql).
ALTER TABLE pedidos DROP CONSTRAINT pedidos_obra_centro_custo_nula_so_baixa;

-- Backfill antes de reimpor NOT NULL — qualquer Pedido de Baixa gravado com
-- obra_centro_custo NULL enquanto esta migration esteve aplicada receberia um
-- valor vazio (mesmo "nunca NULL" de antes desta story).
UPDATE pedidos SET obra_centro_custo = '' WHERE obra_centro_custo IS NULL;
ALTER TABLE pedidos ALTER COLUMN obra_centro_custo SET NOT NULL;
