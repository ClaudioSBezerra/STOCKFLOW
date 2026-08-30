-- Story 1.7: solicitação de promoção de papel (AD-8, FR-33).
--
-- solicitacoes_promocao: a própria conta pede promoção para o papel
-- imediatamente acima na hierarquia (`usuario -> almoxarife`,
-- `almoxarife -> gestor`); um `gestor`/`adm` aprova ou rejeita. Máquina de
-- estado `pendente -> aprovada|rejeitada`. A aprovação troca `usuarios.papel`
-- na mesma transação da decisão — o efeito é imediato porque o middleware
-- relê o papel do Postgres a cada requisição (AD-6).
--
-- `papel_alvo` nunca vem do cliente: é sempre derivado no servidor a partir
-- do papel atual do solicitante. Promoção a `adm` não existe
-- (`idx_usuarios_unico_adm` da migration 000001 já garante conta `adm` única).
CREATE TABLE solicitacoes_promocao (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  solicitante_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  papel_alvo VARCHAR(20) NOT NULL CHECK (papel_alvo IN ('almoxarife','gestor')),
  status VARCHAR(20) NOT NULL DEFAULT 'pendente' CHECK (status IN ('pendente','aprovada','rejeitada')),
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- ON DELETE SET NULL (nunca CASCADE): apagar a conta do decisor não pode
  -- destruir a trilha de auditoria da solicitação (FR-33). Nenhum fluxo do
  -- app apaga contas hoje, mas a convenção fica registrada no schema.
  decidido_por UUID REFERENCES usuarios(id) ON DELETE SET NULL,
  decidido_em TIMESTAMPTZ,
  -- Invariante da máquina de estado: `decidido_em` é preenchido SE E SOMENTE SE
  -- a solicitação saiu de `pendente`. `decidido_por` NÃO entra aqui de
  -- propósito — é ON DELETE SET NULL, então uma linha já decidida pode
  -- legitimamente ter `decidido_por = NULL` depois que a conta do decisor for
  -- removida.
  CONSTRAINT solicitacoes_promocao_decisao_consistente
    CHECK ((status = 'pendente') = (decidido_em IS NULL))
);

-- Backstop de corrida para "no máximo uma solicitação pendente por conta"
-- (AC4): a checagem no service dá o 409 amigável no caso comum, este índice
-- parcial fecha a janela entre duas requisições concorrentes. Mesma filosofia
-- do idx_usuarios_unico_adm (migration 000001).
CREATE UNIQUE INDEX idx_solicitacoes_promocao_pendente_unica
  ON solicitacoes_promocao (solicitante_id) WHERE status = 'pendente';

-- FK consultada diretamente (a solicitação mais recente de uma conta) e alvo
-- de ON DELETE CASCADE ao remover um usuario — mesma justificativa dos
-- comentários de índice das migrations 000002/000003.
CREATE INDEX idx_solicitacoes_promocao_solicitante
  ON solicitacoes_promocao (solicitante_id);

-- FK alvo de ON DELETE SET NULL: sem índice, cada SET NULL em cascata faria
-- um full scan da tabela.
CREATE INDEX idx_solicitacoes_promocao_decidido_por
  ON solicitacoes_promocao (decidido_por);

-- Consulta da fila de pendentes (GET /api/promocoes), ordenada por criado_em.
CREATE INDEX idx_solicitacoes_promocao_status
  ON solicitacoes_promocao (status, criado_em);
