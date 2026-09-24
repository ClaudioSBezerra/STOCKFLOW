-- Story 15.2 (AD-36): login na raiz do domínio pela conta. Quando a senha
-- confere em duas contas do mesmo e-mail (Empresa real + Treinamento dela),
-- POST /api/auth/entrar devolve um `escolhaToken` de uso único (5 min) em vez
-- de uma sessão; POST /api/auth/entrar/escolha o consome com o slug
-- escolhido. Reaproveita `tokens_acao` (mesmo molde de 000020): amplia o
-- CHECK de `tipo` e acrescenta `contas_escolha` — os ids das contas cuja
-- senha conferiu. `usuario_id` recebe a primeira delas (NOT NULL + FK com
-- cascade). Uma coluna de array (em vez de uma linha por conta) porque
-- `token` é UNIQUE e o consumo precisa ser um único UPDATE atômico.
ALTER TABLE tokens_acao ADD COLUMN contas_escolha UUID[] NULL;

ALTER TABLE tokens_acao DROP CONSTRAINT tokens_acao_tipo_check;
ALTER TABLE tokens_acao ADD CONSTRAINT tokens_acao_tipo_check CHECK (tipo IN ('verificacao_email', 'redefinicao_senha', 'mfa_login', 'realtime_ticket', 'escolha_empresa'));
