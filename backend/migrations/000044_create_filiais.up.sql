-- Story 12.1: Cadastro de Filiais e vínculo de Estoque (Epic 12, FR-51,
-- AD-27), spec-12-1.
--
-- Filial é um nível de organização física DENTRO da Empresa (Empresa ->
-- Filial -> Estoque); o isolamento de dados continua só por Empresa (AD-20).
--
-- Fase ADITIVA (AD-20): `estoques.filial_id` nasce NULLABLE. O backfill dos
-- Estoques legados na Filial padrão e o SET NOT NULL são da Story 12.2 —
-- nenhuma linha existente é alterada aqui. Todo Estoque NOVO recebe Filial
-- pela camada de services.
--
-- `nome_normalizado` de `filiais` é gerada como em `estoques` (000008): a
-- unicidade de nome por Empresa é imposta pelo índice, sem SELECT prévio.
CREATE TABLE filiais (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  empresa_id UUID NOT NULL REFERENCES empresas(id),
  nome VARCHAR(255) NOT NULL,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
  nome_normalizado TEXT GENERATED ALWAYS AS (lower(regexp_replace(btrim(nome), '\s+', ' ', 'g'))) STORED
);

CREATE UNIQUE INDEX idx_filiais_empresa_nome_normalizado ON filiais (empresa_id, nome_normalizado);
CREATE INDEX idx_filiais_empresa_id ON filiais (empresa_id);

ALTER TABLE estoques ADD COLUMN filial_id UUID NULL REFERENCES filiais(id);
CREATE INDEX idx_estoques_filial_id ON estoques (filial_id);

-- Unicidade de nome de Estoque passa a ser POR FILIAL (mesmo nome de índice,
-- preservado). `filial_id` NULL (Estoque legado, até a 12.2) nunca colide.
DROP INDEX idx_estoques_nome_normalizado;
CREATE UNIQUE INDEX idx_estoques_nome_normalizado
  ON estoques (filial_id, nome_normalizado);
