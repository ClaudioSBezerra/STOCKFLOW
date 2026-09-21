-- Reverte 000042: remove o comentário de descontinuação (só metadado — NÃO
-- desfaz o corte de dados do saldo para `lotes`; a tabela não tinha comentário antes).
COMMENT ON TABLE produto_estoque IS NULL;
