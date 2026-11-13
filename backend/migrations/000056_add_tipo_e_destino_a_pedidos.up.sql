-- Story 20.1 (Epic 20, Pedido de Transferência entre Filiais): estende o
-- modelo existente de pedidos/pedido_itens com um tipo novo, em vez de criar
-- tabelas paralelas — reaproveita toda a reserva dura, snapshot imutável e
-- autorização já existentes (Epic 7/11, epic-20-context.md). `tipo` fecha o
-- conjunto de Pedidos suportados: `retirada` (Epic 7, já existente — valor
-- DEFAULT para toda linha já gravada, nenhuma migração de dado), e os dois
-- tipos novos do Epic 20: `transferencia` (esta story) e `baixa` (Story
-- 20.2, ainda sem nenhum código que o produza nesta migration).
ALTER TABLE pedidos ADD COLUMN tipo TEXT NOT NULL DEFAULT 'retirada' CHECK (tipo IN ('retirada', 'transferencia', 'baixa'));

-- `estoque_destino_id`: só preenchido quando `pedidos.tipo = 'transferencia'`
-- (Story 20.1) — a Filial de origem/destino é derivada de
-- `estoques.filial_id` de cada lado (Story 12.1), sem precisar de campo
-- próprio para Filial. Nullable: NULL para toda linha de Pedido de retirada,
-- existente ou futura. SEM FK, mesmo motivo de `estoque_id` (migração
-- 000026): Estoques são hard-deletados (`DELETE FROM estoques`,
-- estoques.go) — uma FK apagaria a linha (CASCADE) ou bloquearia a exclusão
-- sem distinção do guard de Pedido pendente já existente.
ALTER TABLE pedido_itens ADD COLUMN estoque_destino_id UUID NULL;
