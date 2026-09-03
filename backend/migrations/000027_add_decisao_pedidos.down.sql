-- Reverte 000027 (Story 7.5). Descarta linhas com o novo valor de status
-- ANTES de restaurar o CHECK original — o CHECK é validado contra os dados
-- já presentes na tabela, não só contra inserções futuras (mesmo motivo da
-- down-migration 000020).
DELETE FROM pedidos WHERE status = 'parcialmente_aprovado';

ALTER TABLE pedido_itens DROP COLUMN quantidade_aprovada;

ALTER TABLE pedidos DROP CONSTRAINT pedidos_status_check;
ALTER TABLE pedidos ADD CONSTRAINT pedidos_status_check
  CHECK (status IN ('pendente', 'aprovado', 'rejeitado'));

ALTER TABLE pedidos DROP COLUMN decidido_em;
ALTER TABLE pedidos DROP COLUMN decidido_por;
