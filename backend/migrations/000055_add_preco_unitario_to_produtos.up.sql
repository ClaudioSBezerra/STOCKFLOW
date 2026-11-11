-- Feedback Ferreira Costa (direção, 2026-11-11): o sistema passa a ter um
-- campo de preço unitário próprio de Produto — opcional, puramente
-- informativo (nunca usado em nenhum cálculo de saldo/Pedido/reserva, só
-- exibido e editável nas telas de Cadastrar/Editar produto e no detalhe).
-- NUMERIC(12,2): 10 dígitos antes da vírgula, 2 depois — cobre qualquer
-- preço realista sem o custo de FLOAT (arredondamento binário em dinheiro).
ALTER TABLE produtos ADD COLUMN preco_unitario NUMERIC(12, 2);
