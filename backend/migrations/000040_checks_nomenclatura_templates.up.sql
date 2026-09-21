-- Story 10.6: CRUD de Templates de Nomenclatura (Epic 10, AD-33) — limite no
-- banco: `subtipo` e `template` não podem ser vazios/só espaços (o tamanho já
-- é limitado por VARCHAR(255), migration 000013).
--
-- `nomenclatura_templates_padrao` (molde de ProvisionarEmpresa/
-- CopiarListasPadrao) recebe os MESMOS CHECKs: se só `nomenclatura_templates`
-- os tivesse, a cópia para cada Empresa nova poderia violá-los. Todas as
-- linhas de seed existentes (000013/000036) já satisfazem o CHECK.
ALTER TABLE nomenclatura_templates
  ADD CONSTRAINT nomenclatura_templates_subtipo_nao_vazio CHECK (btrim(subtipo) <> ''),
  ADD CONSTRAINT nomenclatura_templates_template_nao_vazio CHECK (btrim(template) <> '');

ALTER TABLE nomenclatura_templates_padrao
  ADD CONSTRAINT nomenclatura_templates_padrao_subtipo_nao_vazio CHECK (btrim(subtipo) <> ''),
  ADD CONSTRAINT nomenclatura_templates_padrao_template_nao_vazio CHECK (btrim(template) <> '');
