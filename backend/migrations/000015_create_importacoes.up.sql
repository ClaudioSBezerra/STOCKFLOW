-- Story 3.3: importação em massa via planilha padronizada (FR-10).
--
-- importacoes: uma linha por arquivo `.xlsx` submetido a
-- POST /api/importacoes — o cabeçalho da execução (quem, quando, quantas
-- linhas de dado, se já terminou de processar). `status` só tem dois valores
-- porque o processamento nunca falha por completo: uma linha inválida vira
-- `rejeitada` em `importacao_linhas`, nunca aborta a importação inteira — só
-- um erro de infraestrutura interrompe `processarPendentes` no meio,
-- deixando a importação `em_andamento` para uma chamada futura de
-- POST /api/importacoes/{id}/continuar retomar.
--
-- `total_linhas` é fixado no INSERT inicial (linhas de dado não-em-branco da
-- planilha) e nunca muda depois — é o "de M" do banner "parou na linha N de
-- M" (spec-3-3).
CREATE TYPE importacao_status AS ENUM ('em_andamento', 'concluida');

CREATE TABLE importacoes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  nome_arquivo VARCHAR(255) NOT NULL,
  status importacao_status NOT NULL DEFAULT 'em_andamento',
  total_linhas INTEGER NOT NULL,
  criado_por UUID NOT NULL REFERENCES usuarios(id),
  iniciado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
  concluido_em TIMESTAMPTZ NULL
);

-- GET /api/importacoes/ultima ordena por `iniciado_em DESC LIMIT 1` — índice
-- direto para essa consulta, sem sequential scan à medida que o histórico de
-- importações cresce.
CREATE INDEX idx_importacoes_iniciado_em ON importacoes (iniciado_em DESC);
