-- ATENÇÃO: dropar migracao_id_map descarta TODA a trilha id-antigo -> id-novo
-- do corte "big-bang" (AD-15). As migrações de dados seguintes
-- (Produtos/Categorias/Templates/Movimentações/Pedidos/Usuários) dependem
-- dessas linhas para reconstruir as referências cruzadas no schema novo; sem
-- elas, a idempotência e a rastreabilidade do corte se perdem. Só reverta esta
-- migration se estiver descartando o corte inteiro.
DROP TABLE IF EXISTS migracao_id_map;
