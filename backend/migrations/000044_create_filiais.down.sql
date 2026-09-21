-- Reverte 000044: restaura a unicidade de nome de Estoque por Empresa
-- (estado da 000032) e remove `filial_id` e `filiais`.
DROP INDEX IF EXISTS idx_estoques_nome_normalizado;
DROP INDEX IF EXISTS idx_estoques_filial_id;
ALTER TABLE estoques DROP COLUMN IF EXISTS filial_id;
CREATE UNIQUE INDEX idx_estoques_nome_normalizado
  ON estoques (empresa_id, nome_normalizado) NULLS NOT DISTINCT;
DROP TABLE IF EXISTS filiais;
