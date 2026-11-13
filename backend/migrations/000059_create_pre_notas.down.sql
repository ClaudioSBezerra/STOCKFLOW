-- Reverte 000059: `pedidos.pre_nota_id` primeiro (referencia `pre_notas`),
-- depois `pre_notas` e `contadores_pre_nota` — nenhuma das duas é
-- referenciada por outra tabela além de `pedidos`.
ALTER TABLE pedidos DROP COLUMN pre_nota_id;
DROP TABLE pre_notas;
DROP TABLE contadores_pre_nota;
