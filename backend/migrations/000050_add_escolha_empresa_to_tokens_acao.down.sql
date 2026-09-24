-- Sem apagar as linhas 'escolha_empresa' antes, restaurar o CHECK original
-- falharia (o CHECK é validado contra os dados já presentes — mesmo motivo
-- de 000020).
DELETE FROM tokens_acao WHERE tipo = 'escolha_empresa';

ALTER TABLE tokens_acao DROP CONSTRAINT tokens_acao_tipo_check;
ALTER TABLE tokens_acao ADD CONSTRAINT tokens_acao_tipo_check CHECK (tipo IN ('verificacao_email', 'redefinicao_senha', 'mfa_login', 'realtime_ticket'));

ALTER TABLE tokens_acao DROP COLUMN contas_escolha;
