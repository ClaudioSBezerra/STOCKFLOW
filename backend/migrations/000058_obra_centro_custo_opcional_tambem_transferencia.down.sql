-- Remove o CHECK mais permissivo antes do backfill (ordem inversa do up.sql).
ALTER TABLE pedidos DROP CONSTRAINT pedidos_obra_centro_custo_nula_so_baixa_ou_transferencia;

-- Backfill: qualquer Pedido de Transferência gravado com obra_centro_custo
-- NULL enquanto esta migration esteve aplicada receberia um valor vazio
-- (mesmo "nunca NULL" de antes desta story) — sem isto, reimpor o CHECK
-- antigo (só `tipo = 'baixa'` pode ser NULL) falharia nessas linhas.
UPDATE pedidos SET obra_centro_custo = '' WHERE tipo = 'transferencia' AND obra_centro_custo IS NULL;

ALTER TABLE pedidos ADD CONSTRAINT pedidos_obra_centro_custo_nula_so_baixa
	CHECK (tipo = 'baixa' OR obra_centro_custo IS NOT NULL);
