-- Remove o usuário sintético "Migração do sistema legado" (Story 5.4).
-- Se já houver linhas em `movimentacoes` apontando para ele (corte de
-- Movimentações já executado), este DELETE falha pela FK `movimentacoes.
-- usuario_id` — comportamento esperado de um down: não se apaga um autor de
-- trilha de auditoria em uso.
DELETE FROM usuarios WHERE lower(email) = lower('migracao-legado@sistema.stockflow.local');
