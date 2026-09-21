-- Story 11.2: Migração do saldo existente para Lote legado (Epic 11, AD-24,
-- FR-47).
--
-- Marca `produto_estoque` como DESCONTINUADA: o saldo passa a ser lido de
-- `lotes` (via a view `saldo_produto_estoque`). A tabela NÃO é removida aqui:
-- Baixa, Transferência, Pedidos, Cadastro e Importação ainda a leem/escrevem
-- até as Stories 11.3-11.6, e o corte de dados é um ato manual de operador
-- (`cmd/migrar-saldo-lotes`, AD-15). Só um COMMENT — nenhum dado e nenhuma
-- view são alterados.
COMMENT ON TABLE produto_estoque IS
  'DESCONTINUADA (Story 11.2, AD-24): o saldo migra para `lotes` pelo corte cmd/migrar-saldo-lotes (view saldo_produto_estoque soma as duas até lá). Só será removida quando nenhum código a ler ou escrever.';
