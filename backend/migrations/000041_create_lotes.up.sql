-- Story 11.1: Lançamento de saldo inicial com Lote e Data de Validade
-- (Epic 11, AD-24, FR-47).
--
-- `lotes` é a unidade de saldo com rastreabilidade de recebimento: cada
-- lançamento de entrada cria SEMPRE uma linha nova (nunca atualiza outra).
-- Sem ON DELETE CASCADE (mesma decisão de `movimentacoes`): um Lote nunca
-- some silenciosamente por causa da exclusão de Produto/Estoque; Lote zerado
-- nunca é apagado. `data_validade` é DATE anulável (validade desconhecida).
CREATE TABLE lotes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  produto_id UUID NOT NULL REFERENCES produtos(id),
  estoque_id UUID NOT NULL REFERENCES estoques(id),
  quantidade NUMERIC(10, 3) NOT NULL CHECK (quantidade >= 0),
  data_validade DATE NULL,
  empresa_id UUID NOT NULL REFERENCES empresas(id),
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_lotes_produto_estoque ON lotes (produto_id, estoque_id);
CREATE INDEX idx_lotes_estoque_id ON lotes (estoque_id);
CREATE INDEX idx_lotes_empresa_id ON lotes (empresa_id);

-- Rastreia qual Lote originou a Movimentação `entrada` (nulo para as demais).
ALTER TABLE movimentacoes ADD COLUMN lote_id UUID NULL REFERENCES lotes(id);
CREATE INDEX idx_movimentacoes_lote_id ON movimentacoes (lote_id);

ALTER TABLE movimentacoes DROP CONSTRAINT movimentacoes_tipo_check;
ALTER TABLE movimentacoes ADD CONSTRAINT movimentacoes_tipo_check
  CHECK (tipo IN ('baixa', 'transferencia', 'ajuste', 'entrada'));

-- Saldo lido por par (produto, estoque) = saldo legado de `produto_estoque`
-- (ainda não migrado, Story 11.2) + soma dos Lotes do par. Todas as leituras
-- de saldo do Catálogo passam por esta view.
CREATE VIEW saldo_produto_estoque AS
SELECT produto_id, estoque_id, SUM(quantidade) AS quantidade
FROM (
  SELECT produto_id, estoque_id, quantidade FROM produto_estoque
  UNION ALL
  SELECT produto_id, estoque_id, quantidade FROM lotes
) s
GROUP BY produto_id, estoque_id;
