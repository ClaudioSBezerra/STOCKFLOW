-- Sem isto, restaurar o CHECK original falha assim que qualquer linha
-- 'mfa_login' existir (o que ocorre quase imediatamente depois que a
-- up-migration é usada) — o CHECK é validado contra os dados já presentes na
-- tabela, não só contra inserções futuras.
DELETE FROM tokens_acao WHERE tipo = 'mfa_login';

ALTER TABLE tokens_acao DROP CONSTRAINT tokens_acao_tipo_check;
ALTER TABLE tokens_acao ADD CONSTRAINT tokens_acao_tipo_check CHECK (tipo IN ('verificacao_email', 'redefinicao_senha'));

ALTER TABLE sessoes DROP COLUMN IF EXISTS origem;

ALTER TABLE usuarios DROP COLUMN IF EXISTS mfa_ultimo_passo_usado;
ALTER TABLE usuarios DROP COLUMN IF EXISTS mfa_secret;
ALTER TABLE usuarios DROP COLUMN IF EXISTS mfa_habilitado;
