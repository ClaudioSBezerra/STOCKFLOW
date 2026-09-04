-- Story 8.1: exportação dos próprios dados pessoais (Epic 8, Privacidade/LGPD).
--
-- ExportarDadosUsuario (services/privacidade.go) compõe, a cada exportação,
-- TRÊS varreduras escopadas ao usuário chamador, todas sem `LIMIT`:
--   1. ListarLogsAcessoDoUsuario   -> `logs_acesso WHERE usuario_id = $1`
--   2. ListarMovimentacoesDoUsuario -> `movimentacoes WHERE usuario_id = $1`
--   3. ListarPedidosProprios(…, "") -> `pedidos WHERE usuario_id = $1`
--
-- Nenhuma das três tabelas tinha índice em `usuario_id`: `logs_acesso`
-- (migration 000007) e `movimentacoes` (migration 000021) só indexam
-- `criado_em` (e `movimentacoes` também `produto_id`/`pedido_id`), e
-- `pedidos` (migration 000026) só indexa `pedido_itens.estoque_id`. As
-- consultas administrativas dessas trilhas varrem por período, não por
-- usuário, então até esta story nenhuma precisou do índice.
--
-- Sem estes três índices a exportação LGPD faz sequential scan na trilha de
-- auditoria inteira toda vez — mais caro justamente em `pedidos`, a que
-- mais cresce. O índice em `pedidos.usuario_id` também acelera um scan
-- pré-existente: "Meus Pedidos" (ListarPedidosProprios, Story 7.3) já
-- filtrava `pedidos WHERE usuario_id = $1` sem índice. Todos são índices
-- simples em coluna NOT NULL (`movimentacoes`/`pedidos`) ou anulável
-- (`logs_acesso`); mesmo motivo de idx_movimentacoes_produto_id (migration
-- 000021) e idx_movimentacoes_pedido_id (migration 000028).
CREATE INDEX idx_logs_acesso_usuario_id ON logs_acesso (usuario_id);
CREATE INDEX idx_movimentacoes_usuario_id ON movimentacoes (usuario_id);
CREATE INDEX idx_pedidos_usuario_id ON pedidos (usuario_id);
