-- Story 20.1 (Epic 20, Pedido de Transferência entre Filiais), revisão
-- pós-aprovação (requisito do usuário, mesmo pedido que já tornou o campo
-- opcional na Story 20.2/migrations/000057): "Obra/Centro de Custo" deixa de
-- ser obrigatório também no Pedido de Transferência — só o Pedido de
-- retirada (Epic 7) continua SEMPRE exigindo o campo preenchido. A coluna
-- (compartilhada pelos três tipos) já é NULLable desde a 000057; esta
-- migration só troca o CHECK que restringia o NULL a `tipo = 'baixa'`.
ALTER TABLE pedidos DROP CONSTRAINT pedidos_obra_centro_custo_nula_so_baixa;

ALTER TABLE pedidos ADD CONSTRAINT pedidos_obra_centro_custo_nula_so_baixa_ou_transferencia
	CHECK (tipo IN ('baixa', 'transferencia') OR obra_centro_custo IS NOT NULL);
