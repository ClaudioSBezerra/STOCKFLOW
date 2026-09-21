-- Story 11.3: Reserva de saldo ao enviar Pedido (Epic 11, AD-25, FR-50).
--
-- `reservas_pedido_item`: 1 linha por item de Pedido `pendente`. Ativa = a
-- linha existe; liberar = apagar (DecidirPedido apaga todas as reservas do
-- Pedido na decisão). O saldo DISPONÍVEL nunca é coluna materializada: é
-- sempre saldo físico (produto_estoque + lotes) menos a soma destas linhas.
--
-- `estoque_id` SEM FK, como `pedido_itens.estoque_id` (migração 000026):
-- Estoques são hard-deletados e o guard de exclusão consulta por leitura.
-- `pedido_id` ON DELETE CASCADE: a reserva não sobrevive ao Pedido.
CREATE TABLE reservas_pedido_item (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  pedido_id UUID NOT NULL REFERENCES pedidos(id) ON DELETE CASCADE,
  produto_id UUID NOT NULL REFERENCES produtos(id),
  estoque_id UUID NOT NULL,
  quantidade NUMERIC(10, 3) NOT NULL CHECK (quantidade > 0),
  empresa_id UUID NOT NULL REFERENCES empresas(id),
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (pedido_id, produto_id, estoque_id)
);

CREATE INDEX idx_reservas_pedido_item_produto_estoque ON reservas_pedido_item (produto_id, estoque_id);
CREATE INDEX idx_reservas_pedido_item_empresa_id ON reservas_pedido_item (empresa_id);

-- Backfill: cada item dos Pedidos JÁ pendentes ganha uma reserva, para a
-- invariante "pendente => reservado" valer desde o deploy.
INSERT INTO reservas_pedido_item (pedido_id, produto_id, estoque_id, quantidade, empresa_id)
SELECT pi.pedido_id, pi.produto_id, pi.estoque_id, pi.quantidade, p.empresa_id
FROM pedido_itens pi
JOIN pedidos p ON p.id = pi.pedido_id
WHERE p.status = 'pendente' AND p.empresa_id IS NOT NULL;
