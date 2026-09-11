-- Reverte 000035 (Story 9.4): devolve as listas padrão para dentro das
-- tabelas de domínio como linhas `empresa_id IS NULL` (o molde de antes da
-- 9.4) e derruba as tabelas `_padrao`.
--
-- O `WHERE NOT EXISTS` evita colidir com as linhas molde que a 000035 deixou
-- vivas por estarem referenciadas por Produtos (`idx_categorias_empresa_codigo`
-- / `idx_nomenclatura_templates_empresa_subtipo`, ambos NULLS NOT DISTINCT).

INSERT INTO categorias (codigo, nome, empresa_id)
SELECT p.codigo, p.nome, NULL
FROM categorias_padrao p
WHERE NOT EXISTS (
  SELECT 1 FROM categorias c WHERE c.empresa_id IS NULL AND (c.codigo = p.codigo OR c.nome = p.nome)
);

INSERT INTO nomenclatura_templates (subtipo, template, empresa_id)
SELECT p.subtipo, p.template, NULL
FROM nomenclatura_templates_padrao p
WHERE NOT EXISTS (
  SELECT 1 FROM nomenclatura_templates t WHERE t.empresa_id IS NULL AND t.subtipo = p.subtipo
);

DROP TABLE nomenclatura_templates_padrao;
DROP TABLE categorias_padrao;
