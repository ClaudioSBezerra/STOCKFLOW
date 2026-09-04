-- Story 7.7: Migração de Pedidos e vínculo com Histórico (Epic 7, Pedidos de
-- Retirada). Acrescenta a coluna `pedido_id` a `movimentacoes` para que o
-- corte "big-bang" (cmd/migrate-legado) possa restabelecer o vínculo entre
-- uma Movimentação migrada (Story 5.4) e o Pedido legado que a originou
-- (campo de referência em `legado.historico`).
--
-- O ERD herdado (addendum §A: "MOVIMENTACOES ... + pedido nullable") já
-- previa esta coluna; a migration 000021 não a criou porque nenhuma story de
-- runtime até a 7.5 precisou dela. A coluna é ANULÁVEL e, por ora, SÓ A
-- MIGRAÇÃO escreve nela — consistente com `movimentacoes.criado_em`
-- explícito e com `migracao_id_map`, ambos já só-migração (migration 000021:
-- "schema pensado para não exigir migration de alteração depois").
-- `services.DecidirPedido` (Story 7.5), `services.ListarMovimentacoes`
-- (Story 5.3), o JSON de `GET /api/pedidos*` (7.3/7.4) e o recibo (7.6) NÃO
-- são alterados: nenhum SELECT existente lê esta coluna.
--
-- SEM `ON DELETE CASCADE`: mesma decisão de todas as FKs de `movimentacoes`
-- (migration 000021, "Nenhuma FK usa ON DELETE CASCADE") — Movimentação é
-- trilha de auditoria e nunca deve sumir silenciosamente por causa da
-- exclusão de um Pedido referenciado.
ALTER TABLE movimentacoes ADD COLUMN pedido_id UUID REFERENCES pedidos(id);

-- O corte consulta "quais Movimentações já apontam para este Pedido?" e o
-- vínculo tende a ser lido por relatório por Pedido no futuro — sem índice
-- seria sequential scan (mesmo motivo de idx_movimentacoes_produto_id,
-- migration 000021).
CREATE INDEX idx_movimentacoes_pedido_id ON movimentacoes (pedido_id);
