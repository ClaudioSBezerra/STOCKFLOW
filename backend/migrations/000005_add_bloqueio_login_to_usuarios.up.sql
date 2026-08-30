-- Story 1.10: bloqueio de conta por excesso de tentativas de login (FR-36, SM-6).
--
-- tentativas_login_falhas + bloqueado_ate em `usuarios`: contador de falhas de
-- senha CONSECUTIVAS por conta e o instante até o qual o login fica recusado
-- (mesmo com a senha correta). Estado na própria linha da conta — mesma
-- filosofia de `usuarios.ativo` —, sem tabela nova: `services.Login` faz o
-- UPDATE atômico que fecha a corrida entre tentativas concorrentes. Aditiva,
-- não destrutiva.
ALTER TABLE usuarios ADD COLUMN tentativas_login_falhas INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usuarios ADD COLUMN bloqueado_ate TIMESTAMPTZ;
