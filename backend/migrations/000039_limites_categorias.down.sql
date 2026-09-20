-- Reverte 000039: restaura os tipos originais (10/255) e derruba os CHECKs.
-- O nome encurtado de '09.001' NÃO é revertido.
ALTER TABLE categorias_padrao
  DROP CONSTRAINT categorias_padrao_codigo_nao_vazio,
  DROP CONSTRAINT categorias_padrao_nome_nao_vazio,
  ALTER COLUMN codigo TYPE VARCHAR(10),
  ALTER COLUMN nome TYPE VARCHAR(255);

ALTER TABLE categorias
  DROP CONSTRAINT categorias_codigo_nao_vazio,
  DROP CONSTRAINT categorias_nome_nao_vazio,
  ALTER COLUMN codigo TYPE VARCHAR(10),
  ALTER COLUMN nome TYPE VARCHAR(255);
