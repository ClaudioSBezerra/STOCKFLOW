-- Story 20.2 (Epic 20, Pedido de Baixa de Estoque), revisão pós-aprovação
-- (requisito do usuário): "Obra/Centro de Custo" deixa de ser obrigatório no
-- Pedido de Baixa — o Pedido de retirada (Epic 7) e o Pedido de
-- Transferência (Story 20.1) continuam SEMPRE preenchendo este campo (nada
-- muda pra eles, validado no app, não no banco); só o Pedido de Baixa pode
-- gravar NULL agora. Solta o NOT NULL de toda a coluna (compartilhada pelos
-- três tipos) em vez de um CHECK condicional por `tipo` — mais simples, e os
-- dois tipos antigos nunca produzem NULL de qualquer forma.
ALTER TABLE pedidos ALTER COLUMN obra_centro_custo DROP NOT NULL;

-- Revisão (2 revisores): soltar o NOT NULL sozinho não impede um bug futuro
-- de gravar NULL num Pedido de retirada/Transferência (só a validação da
-- aplicação impedia isso). Este CHECK fecha a garantia no nível do banco —
-- só `tipo = 'baixa'` pode ter `obra_centro_custo IS NULL`; qualquer outro
-- tipo com NULL viola a constraint e o INSERT/UPDATE falha.
ALTER TABLE pedidos ADD CONSTRAINT pedidos_obra_centro_custo_nula_so_baixa
	CHECK (tipo = 'baixa' OR obra_centro_custo IS NOT NULL);
