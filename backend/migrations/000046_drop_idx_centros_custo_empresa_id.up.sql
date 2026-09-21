-- Code review dos Épicos 10-12 (2026-09-21): `idx_centros_custo_empresa_id`
-- (000045) é prefixo do índice único `(empresa_id, nome_normalizado)` — o
-- Postgres já usa o composto para qualquer filtro só por `empresa_id`, então o
-- índice simples só custa escrita. Remoção segura: nenhuma constraint depende
-- dele.
DROP INDEX IF EXISTS idx_centros_custo_empresa_id;
