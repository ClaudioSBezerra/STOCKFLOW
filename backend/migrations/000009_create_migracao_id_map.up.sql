-- Story 2.3: migração dos Estoques legados (AD-15, PRD §9).
--
-- migracao_id_map: tabela de mapeamento COMPARTILHADA id-antigo -> id-novo,
-- usada pelo corte "big-bang" único do sistema legado (protótipo Firestore,
-- hoje espelhado num PostgreSQL local mantido pela empresa — addendum §F) para
-- o schema novo. Cada entidade legada recriada no schema novo grava aqui uma
-- linha ligando o id legado (doc id textual do Firestore) ao UUID v4 novo,
-- para que as demais migrações de dados preservem as referências cruzadas no
-- corte.
--
-- A Story 2.3 é a primeira migração de dados a rodar, então ela cria esta
-- tabela e migra os Estoques. As migrações seguintes (Produtos, Categorias,
-- Templates, Movimentações, Pedidos, Usuários — Stories 3.7, 5.4, 7.7, ...)
-- só INSEREM linhas com outro valor de `entidade`: nenhuma delas precisa
-- alterar este schema.
--
-- Vocabulário previsto de `entidade` (uma story de migração para cada):
--   estoque, produto, categoria, template_nomenclatura, movimentacao,
--   pedido, usuario
--
-- `entidade` é TEXT de propósito — NÃO é enum nem tem CHECK. Um CHECK/enum
-- obrigaria cada story de migração seguinte a também migrar este schema só
-- para acrescentar o seu valor ao conjunto permitido. A lista acima é
-- documentação, não constraint.
--
-- PK (entidade, id_legado): é a idempotência do corte. Reexecutar a migração
-- consulta esta PK — se a linha existe, a entidade já foi migrada (mesmo que
-- alguém tenha renomeado o registro no alvo depois). Amarrar pelo id legado
-- imutável é mais forte que amarrar pelo nome.
--
-- UNIQUE (entidade, id_novo): um id novo nunca é reaproveitado para duas
-- linhas legadas da mesma entidade.
--
-- SEM FK de id_novo para estoques(id) (nem para qualquer outra tabela): o
-- mapa é multi-entidade e é trilha de corte, não integridade referencial
-- viva — ele sobrevive à exclusão de um Estoque migrado. O runtime da
-- aplicação NUNCA escreve nesta tabela; só os binários one-off
-- (`cmd/migrate-legado` e as futuras migrações de dados) a populam, sempre
-- disparados manualmente por uma pessoa.
CREATE TABLE migracao_id_map (
  entidade TEXT NOT NULL,
  id_legado TEXT NOT NULL,
  id_novo UUID NOT NULL,
  migrado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (entidade, id_legado),
  UNIQUE (entidade, id_novo)
);
