-- Reverte 000051: remove a trilha `produto_historico` e as colunas de
-- inativação de `produtos`.
DROP TABLE IF EXISTS produto_historico;
ALTER TABLE produtos
  DROP COLUMN IF EXISTS inativado_por,
  DROP COLUMN IF EXISTS inativado_em;
