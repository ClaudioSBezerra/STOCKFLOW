-- Story 2.1: criar e listar locais de Estoque (FR12).
--
-- estoques: os locais físicos (canteiros, almoxarifados) onde os Produtos
-- ficam armazenados. Fundação referencial dos épicos seguintes (Produtos,
-- Movimentação, Pedidos), que passam a referenciar `estoques(id)`.
--
-- nome_normalizado é uma coluna GERADA (minúsculas + colapso de espaços) sob
-- CREATE UNIQUE INDEX: a unicidade de nome — insensível a maiúsculas/
-- minúsculas e a espaçamento — é imposta pelo próprio Postgres. A
-- concorrência resolve na COLISÃO do índice único (SQLSTATE 23505), nunca
-- numa sequência "SELECT antes de INSERT" em nível de aplicação, que teria
-- janela de corrida entre duas requisições. Mesma filosofia de
-- idx_usuarios_unico_adm (000001) e idx_solicitacoes_promocao_pendente_unica
-- (000004).
--
-- regexp_replace(text, text, text, text) é IMMUTABLE — condição para uso numa
-- coluna GENERATED ALWAYS AS ... STORED —, assim como lower() e btrim().
-- `regexp_replace(..., '\s+', ' ', 'g')` colapsa qualquer sequência de
-- espaços internos para um único espaço; btrim() remove os das pontas.
CREATE TABLE estoques (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  nome VARCHAR(255) NOT NULL,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
  nome_normalizado TEXT GENERATED ALWAYS AS (lower(regexp_replace(btrim(nome), '\s+', ' ', 'g'))) STORED
);

CREATE UNIQUE INDEX idx_estoques_nome_normalizado ON estoques (nome_normalizado);
