-- Story 12.3: Cadastro de Centro de Custo e Destino de Obra (Epic 12, FR-51,
-- AD-28), spec-12-3.
--
-- Centro de Custo é uma lista padronizada POR EMPRESA (cópia por Empresa,
-- molde de `filiais`, 000044); o isolamento continua só por Empresa (AD-20).
--
-- Fase ADITIVA: `pedidos.centro_custo_id` nasce NULLABLE, sem backfill —
-- nenhum Pedido existente é alterado. O texto livre `obra_centro_custo`
-- segue obrigatório e inalterado.
--
-- `nome_normalizado` é gerada como em `filiais`: a unicidade de nome por
-- Empresa é imposta pelo índice, sem SELECT prévio.
CREATE TABLE centros_custo (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  empresa_id UUID NOT NULL REFERENCES empresas(id),
  nome VARCHAR(255) NOT NULL,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
  nome_normalizado TEXT GENERATED ALWAYS AS (lower(regexp_replace(btrim(nome), '\s+', ' ', 'g'))) STORED
);

CREATE UNIQUE INDEX idx_centros_custo_empresa_nome_normalizado ON centros_custo (empresa_id, nome_normalizado);
CREATE INDEX idx_centros_custo_empresa_id ON centros_custo (empresa_id);

ALTER TABLE pedidos ADD COLUMN centro_custo_id UUID NULL REFERENCES centros_custo(id);
CREATE INDEX idx_pedidos_centro_custo_id ON pedidos (centro_custo_id);
