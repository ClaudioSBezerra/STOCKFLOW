-- Story 6.4: reverte 000024 — remove a trilha de auditoria de mesclagem de
-- duplicatas (filha antes da pai) e a coluna de soft-delete de produtos.
DROP TABLE mesclagem_produtos_removidos;
DROP TABLE mesclagens_duplicatas;
ALTER TABLE produtos DROP COLUMN deleted_at;
