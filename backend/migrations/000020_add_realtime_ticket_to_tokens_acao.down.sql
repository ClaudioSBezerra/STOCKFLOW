-- Sem isto, restaurar o CHECK original falha assim que qualquer linha
-- 'realtime_ticket' existir — o CHECK é validado contra os dados já
-- presentes na tabela, não só contra inserções futuras (mesmo motivo da
-- down-migration 000006).
DELETE FROM tokens_acao WHERE tipo = 'realtime_ticket';

ALTER TABLE tokens_acao DROP CONSTRAINT tokens_acao_tipo_check;
ALTER TABLE tokens_acao ADD CONSTRAINT tokens_acao_tipo_check CHECK (tipo IN ('verificacao_email', 'redefinicao_senha', 'mfa_login'));
