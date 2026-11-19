-- Importação para UM Depósito escolhido na tela (spec-importacao-por-deposito).
--
-- `estoque_id`: depósito de destino de toda a planilha. NULL só em importações
-- gravadas antes desta migration (seguem pelo caminho legado ao serem
-- retomadas) ou quando o depósito foi excluído depois (ON DELETE SET NULL, para
-- não bloquear a exclusão do depósito).
-- `por_deposito`: TRUE em toda importação criada com depósito escolhido; junto
-- com `estoque_id` NULL significa "depósito excluído no meio" — as linhas
-- restantes são rejeitadas, nunca caem no caminho legado (que criava depósito).
ALTER TABLE importacoes ADD COLUMN estoque_id UUID NULL REFERENCES estoques(id) ON DELETE SET NULL;
ALTER TABLE importacoes ADD COLUMN por_deposito BOOLEAN NOT NULL DEFAULT FALSE;
