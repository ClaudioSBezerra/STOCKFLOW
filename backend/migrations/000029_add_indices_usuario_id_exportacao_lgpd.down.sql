-- Reverte 000029 (Story 8.1): remove os índices de escopo por usuário usados
-- pela exportação LGPD (logs_acesso, movimentacoes, pedidos). São puramente
-- índices de desempenho — o DROP não perde nenhum dado.
DROP INDEX IF EXISTS idx_logs_acesso_usuario_id;
DROP INDEX IF EXISTS idx_movimentacoes_usuario_id;
DROP INDEX IF EXISTS idx_pedidos_usuario_id;
