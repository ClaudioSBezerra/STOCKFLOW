-- Reverte 000036: remove a linha Genérico de toda Empresa e do molde.
--
-- Mesmo cuidado de 000035 (`DELETE ... WHERE NOT EXISTS`): um rollback nunca
-- pode deixar `produtos.template_id` órfão. Diferente de 000035 (que só
-- limpa linhas-molde `empresa_id IS NULL`, nunca referenciadas por Produtos
-- de Empresa nenhuma), Genérico É o próprio template aplicado a Produtos
-- reais assim que o cadastro passa a exigi-lo (Story 10.1) — a linha de uma
-- Empresa que já tenha algum Produto com esse `template_id` sobrevive; as
-- demais (sem Produto algum usando Genérico ainda) são removidas.
DELETE FROM nomenclatura_templates t
WHERE t.subtipo = 'Genérico'
  AND NOT EXISTS (SELECT 1 FROM produtos p WHERE p.template_id = t.id);

DELETE FROM nomenclatura_templates_padrao WHERE subtipo = 'Genérico';
