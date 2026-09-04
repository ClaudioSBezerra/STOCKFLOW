-- Story 8.2: exclusão e anonimização de dados pessoais por Adm (Epic 8,
-- Privacidade/LGPD), spec-8-2.
--
-- solicitacoes_exclusao_conta: molde de solicitacoes_promocao (migração
-- 000004). Qualquer Usuário autenticado registra uma solicitação `pendente`
-- da PRÓPRIA conta (POST /api/usuarios/me/solicitacao-exclusao); só um `adm`
-- lista e processa. Processar = anonimizar `usuarios.nome`/`usuarios.email`,
-- zerar credenciais e revogar sessões na mesma transação — sem tocar em
-- nenhuma linha de `movimentacoes`, `pedidos` ou `logs_acesso` (a integridade
-- histórica/auditoria dos épicos anteriores tem de sobreviver intacta).
-- Máquina de estado `pendente -> processada`.
--
-- Nenhum campo de alvo vem do cliente: `solicitante_id` é sempre a conta da
-- sessão que registra a solicitação; o processamento age sempre sobre
-- `solicitante_id` da linha, nunca sobre um id de path/query/body.
CREATE TABLE solicitacoes_exclusao_conta (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  solicitante_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  status VARCHAR(20) NOT NULL DEFAULT 'pendente' CHECK (status IN ('pendente','processada')),
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- ON DELETE SET NULL (nunca CASCADE): apagar a conta do `adm` que processou
  -- não pode destruir a trilha de auditoria da solicitação. Mesma convenção
  -- de `solicitacoes_promocao.decidido_por`.
  processado_por UUID REFERENCES usuarios(id) ON DELETE SET NULL,
  processado_em TIMESTAMPTZ,
  -- Invariante da máquina de estado: `processado_em` é preenchido SE E SOMENTE
  -- SE a solicitação saiu de `pendente`. `processado_por` NÃO entra aqui de
  -- propósito — é ON DELETE SET NULL, então uma linha já processada pode
  -- legitimamente ter `processado_por = NULL` depois.
  CONSTRAINT solicitacoes_exclusao_conta_processamento_consistente
    CHECK ((status = 'pendente') = (processado_em IS NULL))
);

-- Backstop de corrida para "no máximo uma solicitação pendente por conta": a
-- checagem no service dá o 409 amigável no caso comum, este índice parcial
-- fecha a janela entre duas requisições concorrentes (molde de
-- idx_solicitacoes_promocao_pendente_unica).
CREATE UNIQUE INDEX idx_solicitacoes_exclusao_pendente_unica
  ON solicitacoes_exclusao_conta (solicitante_id) WHERE status = 'pendente';

-- FK consultada diretamente e alvo de ON DELETE CASCADE ao remover um usuario.
CREATE INDEX idx_solicitacoes_exclusao_solicitante
  ON solicitacoes_exclusao_conta (solicitante_id);

-- FK alvo de ON DELETE SET NULL: sem índice, cada SET NULL em cascata faria
-- um full scan da tabela.
CREATE INDEX idx_solicitacoes_exclusao_processado_por
  ON solicitacoes_exclusao_conta (processado_por);

-- Consulta da fila de pendentes (GET /api/solicitacoes-exclusao), ordenada
-- por criado_em.
CREATE INDEX idx_solicitacoes_exclusao_status
  ON solicitacoes_exclusao_conta (status, criado_em);
