-- Story 20.4 (Epic 20, Geração de Pré-Nota ao aprovar lote), spec-20-4.
--
-- `contadores_pre_nota` guarda, por Empresa, o último `docnum` (equivalente
-- ao MIRO do SAP) já emitido — MESMO molde de `contadores_produto`
-- (migration 000037, Story 10.2): uma linha por Empresa (`empresa_id` como
-- CHAVE PRIMÁRIA), nunca lazy-init (toda Empresa nasce com sua linha via
-- InserirEmpresa/ProvisionarEmpresa, AD-26; o backfill abaixo cobre as
-- Empresas já existentes antes desta migration).
--
-- `services.proximoDocnumPreNota` avança `ultimo_numero` via `UPDATE ...
-- RETURNING` atômico, DENTRO da MESMA transação de `DecidirLotePedidos` —
-- mais simples que `proximoCodigoProduto`: `docnum` não tem equivalente de
-- código legado/importado por planilha, então não precisa de `GREATEST`
-- contra um MAX() de outra tabela.
CREATE TABLE contadores_pre_nota (
  empresa_id UUID PRIMARY KEY REFERENCES empresas(id),
  ultimo_numero INTEGER NOT NULL DEFAULT 0 CHECK (ultimo_numero >= 0)
);

INSERT INTO contadores_pre_nota (empresa_id, ultimo_numero)
SELECT e.id, 0 FROM empresas e;

-- `pre_notas`: 1 linha por LOTE aprovado que decidiu de fato ao menos 1
-- Pedido nesta chamada (Approach, spec-20-4) — nunca para um lote rejeitado,
-- nunca para um lote 100% "pulado" (todos os ids já decididos por outra
-- transação antes). `UNIQUE (empresa_id, docnum)` fecha o par: dois `docnum`
-- iguais na MESMA Empresa são uma corrupção de dado, nunca esperados sob o
-- contador atômico acima. Documento impresso é Story 20.5, fora de escopo
-- desta migration — aqui só o registro.
CREATE TABLE pre_notas (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  empresa_id UUID NOT NULL REFERENCES empresas(id),
  docnum INTEGER NOT NULL CHECK (docnum > 0),
  -- `decidido_por` REFERENCES usuarios(id) sem ON DELETE: mesmo padrão de
  -- `pedidos.decidido_por` (migration 000027) — auditoria de quem aprovou o
  -- lote, nunca removida silenciosamente se a conta autora sumir por outro
  -- caminho no futuro.
  decidido_por UUID NOT NULL REFERENCES usuarios(id),
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (empresa_id, docnum)
);

-- `pedidos.pre_nota_id`: NULLABLE — Pedido de retirada, pendente, rejeitado,
-- ou decidido fora de lote (DecidirPedido, Never de spec-20-4) nunca tem
-- Pré-Nota. Só os Pedidos decididos de fato (aprovado/parcialmente_aprovado)
-- POR UMA CHAMADA de DecidirLotePedidos recebem o vínculo, gravado na MESMA
-- transação da decisão.
ALTER TABLE pedidos ADD COLUMN pre_nota_id UUID NULL REFERENCES pre_notas(id);

-- Índice em `pedidos.pre_nota_id` (achado de revisão, Story 20.4): a Story
-- 20.5 (documento impresso) vai buscar TODOS os Pedidos de UMA Pré-Nota —
-- sem índice, seria sequential scan de `pedidos` inteira.
CREATE INDEX idx_pedidos_pre_nota_id ON pedidos (pre_nota_id);
