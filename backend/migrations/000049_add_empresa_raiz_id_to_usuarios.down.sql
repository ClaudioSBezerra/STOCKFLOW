-- Reverte a Story 15.1: remove a restrição de e-mail único entre Empresas
-- reais, o trigger, a função e a coluna `empresa_raiz_id`.
--
-- A extensão btree_gist fica: pode ter sido criada antes desta migration ou
-- passar a ser usada por outro objeto, e removê-la exigiria DROP ... CASCADE
-- sobre objetos que não são desta migration.
ALTER TABLE usuarios DROP CONSTRAINT IF EXISTS usuarios_email_unico_entre_empresas_reais;
DROP TRIGGER IF EXISTS usuarios_preencher_empresa_raiz ON usuarios;
DROP FUNCTION IF EXISTS usuarios_preencher_empresa_raiz();
DROP INDEX IF EXISTS idx_usuarios_empresa_raiz_id;
ALTER TABLE usuarios DROP COLUMN IF EXISTS empresa_raiz_id;
