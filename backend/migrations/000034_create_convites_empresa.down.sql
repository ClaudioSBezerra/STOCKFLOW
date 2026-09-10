-- Reverte a 000034 (Story 9.3, spec-9-3). As contas já criadas a partir de um
-- convite permanecem: o convite é a porta de entrada, não o vínculo.
DROP TABLE IF EXISTS convites_empresa;
