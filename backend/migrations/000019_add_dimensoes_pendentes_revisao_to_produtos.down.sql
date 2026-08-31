-- Story 3.7: reverte 000019 — remove a coluna de dimensões pendentes de
-- revisão manual. Nenhuma outra tabela referencia esta coluna.
ALTER TABLE produtos DROP COLUMN dimensoes_pendentes_revisao;
