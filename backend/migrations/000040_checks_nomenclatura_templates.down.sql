-- Reverte 000040: derruba os CHECKs de não-vazio.
ALTER TABLE nomenclatura_templates_padrao
  DROP CONSTRAINT nomenclatura_templates_padrao_subtipo_nao_vazio,
  DROP CONSTRAINT nomenclatura_templates_padrao_template_nao_vazio;

ALTER TABLE nomenclatura_templates
  DROP CONSTRAINT nomenclatura_templates_subtipo_nao_vazio,
  DROP CONSTRAINT nomenclatura_templates_template_nao_vazio;
