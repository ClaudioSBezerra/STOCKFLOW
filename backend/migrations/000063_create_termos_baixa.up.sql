-- Pedido de Outras Saídas e Termo de Baixa (spec-pedido-outras-saidas).
--
-- `pedidos.motivo_saida`: motivo do Pedido de Outras Saídas (tipo='baixa').
-- NULL permitido: pedidos `baixa` antigos não têm motivo.
ALTER TABLE pedidos ADD COLUMN motivo_saida TEXT NULL
  CHECK (motivo_saida IS NULL OR motivo_saida IN ('perda_roubo', 'vencido_avariado', 'utilizacao_obra'));
ALTER TABLE pedidos ADD CONSTRAINT pedidos_motivo_saida_so_baixa
  CHECK (motivo_saida IS NULL OR tipo = 'baixa');

-- Contador do Termo de Baixa: um por Empresa, MESMO molde de
-- `contadores_pre_nota` (000059), numeração INDEPENDENTE da Pré-Nota.
CREATE TABLE contadores_termo_baixa (
  empresa_id UUID PRIMARY KEY REFERENCES empresas(id),
  ultimo_numero INTEGER NOT NULL DEFAULT 0 CHECK (ultimo_numero >= 0)
);

INSERT INTO contadores_termo_baixa (empresa_id, ultimo_numero)
SELECT e.id, 0 FROM empresas e;

-- 1 Termo de Baixa por motivo decidido numa aprovação em lote.
CREATE TABLE termos_baixa (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  empresa_id UUID NOT NULL REFERENCES empresas(id),
  numero INTEGER NOT NULL CHECK (numero > 0),
  motivo_saida TEXT NULL
    CHECK (motivo_saida IS NULL OR motivo_saida IN ('perda_roubo', 'vencido_avariado', 'utilizacao_obra')),
  decidido_por UUID NOT NULL REFERENCES usuarios(id),
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (empresa_id, numero)
);

ALTER TABLE pedidos ADD COLUMN termo_baixa_id UUID NULL REFERENCES termos_baixa(id);
CREATE INDEX idx_pedidos_termo_baixa_id ON pedidos (termo_baixa_id);
