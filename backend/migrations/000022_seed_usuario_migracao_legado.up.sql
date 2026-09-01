-- Story 5.4: Migração do Histórico de Movimentações legado (spec-5-4).
--
-- A coleção `historico` do sistema legado (addendum §F) NÃO tem campo de
-- autor, mas `movimentacoes.usuario_id` é NOT NULL (migration 000021) e a
-- Story 5.3 (`services.ListarMovimentacoes`) faz `JOIN usuarios` interno. Este
-- seed cria um usuário sintético "Migração do sistema legado" que serve de
-- autor NOT NULL para toda linha de `historico` recriada em `movimentacoes`,
-- sem tornar a coluna anulável nem reescrever o JOIN da 5.3.
--
--   papel='almoxarife' : Movimentação é ação de almoxarife+; `adm` é single-row
--                        por idx_usuarios_unico_adm (migration 000001).
--   ativo=false        : a conta nunca é usada para login.
--   senha_hash=NULL    : não há como autenticar.
--   e-mail sentinela   : @sistema.stockflow.local, resolvido por
--                        `migrarMovimentacoes` numa pré-condição de seed.
--
-- Seed por migration — mesmo padrão de `categorias` (000010) e
-- `nomenclatura_templates` (000013); nunca criado pelo binário do corte (só
-- `cmd/seed-admin` cria usuário). ON CONFLICT sobre a EXPRESSÃO lower(email)
-- porque o índice único é `idx_usuarios_email_lower` (migration 000001), não
-- um UNIQUE de coluna.
INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
VALUES ('Migração do sistema legado', 'migracao-legado@sistema.stockflow.local', NULL, 'almoxarife', false, false)
ON CONFLICT (lower(email)) DO NOTHING;
