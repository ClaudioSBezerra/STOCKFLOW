-- Story 1.4: login por e-mail e senha (AD-6).
--
-- sessoes: refresh tokens rotativos emitidos junto com o access JWT (30min)
-- em cada login/refresh bem-sucedido. Guardado em texto puro (não hash) —
-- mesma decisão e justificativa já registrada para tokens_acao (Story 1.3):
-- nenhuma AD exige hash, e a rotação em si (o token antigo é imediatamente
-- marcado revogado e nunca mais aceito) já limita a janela de exposição de um
-- valor eventualmente vazado.
CREATE TABLE sessoes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  usuario_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  refresh_token TEXT NOT NULL UNIQUE,
  expira_em TIMESTAMPTZ NOT NULL,
  revogado_em TIMESTAMPTZ,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Mesma justificativa de idx_tokens_acao_usuario_id (migration 000002): FK
-- consultada diretamente e alvo de ON DELETE CASCADE.
CREATE INDEX idx_sessoes_usuario_id ON sessoes (usuario_id);
