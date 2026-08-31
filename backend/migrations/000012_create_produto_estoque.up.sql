-- Story 3.1: cadastro manual de Produto com dimensões estruturadas (FR-8).
--
-- produto_estoque: relação M:N Produto<->Estoque com a quantidade do Produto
-- naquele Estoque. O INSERT inicial (cadastro de Produto) nunca gera
-- MOVIMENTACOES — AD-10 vincula esse registro só a baixa/transferência/
-- aprovação de Pedido (FR-14/15/25), nunca ao cadastro (FR-8).
--
-- Esta tabela também é a peça que faltava para o guard de "quantidade
-- residual" da exclusão de Estoque (Story 2.2, FR12): sem ela, o SELECT que
-- barra a exclusão de um Estoque com Produto em quantidade positiva não tinha
-- como existir.
--
-- `estoque_id` usa ON DELETE CASCADE: o guard de resíduo em
-- services.ExcluirEstoque já barra a exclusão enquanto existir alguma linha
-- com `quantidade > 0` para o Estoque; uma vez que o guard deixa passar (só
-- sobra `quantidade = 0`, ou nenhuma linha), o `DELETE FROM estoques` não
-- pode esbarrar na própria FK que este guard acabou de aprovar — sem
-- CASCADE aqui, a I/O Matrix "sem resíduo... exclusão continua funcionando
-- normalmente" (linhas zeradas ainda presentes) violaria a constraint.
-- `produto_id` fica SEM CASCADE — Produto nunca é excluído por esta story.
CREATE TABLE produto_estoque (
  produto_id UUID NOT NULL REFERENCES produtos(id),
  estoque_id UUID NOT NULL REFERENCES estoques(id) ON DELETE CASCADE,
  quantidade NUMERIC(10, 3) NOT NULL DEFAULT 0 CHECK (quantidade >= 0),
  PRIMARY KEY (produto_id, estoque_id)
);

-- O guard de resíduo (services.ExcluirEstoque) e o `ErroEstoqueComResiduo`
-- filtram por `estoque_id` (`WHERE pe.estoque_id = $1`), que não é a coluna
-- líder da PK composta (produto_id, estoque_id) — sem um índice próprio essa
-- consulta cai em sequential scan à medida que a tabela cresce.
CREATE INDEX idx_produto_estoque_estoque_id ON produto_estoque (estoque_id);
