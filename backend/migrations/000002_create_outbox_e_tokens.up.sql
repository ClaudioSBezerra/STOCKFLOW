-- Story 1.3: autocadastro com verificação de e-mail (AD-4, AD-18).
--
-- tokens_acao: token de uso único, tipado por `tipo`, consumido pelo fluxo de
-- verificação de e-mail desta story e reutilizado por redefinição de senha
-- (Story 1.6). Validação real sempre filtra por token+usuario_id+tipo+não
-- expirado+não usado — o filtro por usuario_id acontece na aplicação, não
-- aqui, pois o token já é globalmente único (UNIQUE em `token`).
CREATE TABLE tokens_acao (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  usuario_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  token TEXT NOT NULL UNIQUE,
  tipo VARCHAR(30) NOT NULL CHECK (tipo IN ('verificacao_email','redefinicao_senha')),
  expira_em TIMESTAMPTZ NOT NULL,
  usado_em TIMESTAMPTZ,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- FK consultada diretamente (ex.: "tokens de um usuário") e alvo de todo
-- ON DELETE CASCADE ao remover um usuario — sem índice, cada cascade faria
-- um full scan de tokens_acao.
CREATE INDEX idx_tokens_acao_usuario_id ON tokens_acao (usuario_id);

-- emails_pendentes: outbox transacional (AD-4). O produtor nunca grava HTML
-- pré-renderizado aqui — só `variaveis_json`; o worker resolve o template
-- pelo `tipo` no momento do envio.
CREATE TABLE emails_pendentes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  usuario_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  destinatario VARCHAR(255) NOT NULL,
  tipo VARCHAR(30) NOT NULL CHECK (tipo IN ('verificacao_conta','redefinicao_senha')),
  variaveis_json JSONB NOT NULL,
  status VARCHAR(10) NOT NULL DEFAULT 'pendente' CHECK (status IN ('pendente','enviado','falho')),
  tentativas INT NOT NULL DEFAULT 0,
  ultimo_erro TEXT,
  enviado_em TIMESTAMPTZ,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Consultado pelo worker a cada ciclo de polling (services/email_worker.go).
CREATE INDEX idx_emails_pendentes_status_criado_em ON emails_pendentes (status, criado_em);

-- Mesma justificativa de idx_tokens_acao_usuario_id: FK consultada
-- diretamente e alvo de ON DELETE CASCADE.
CREATE INDEX idx_emails_pendentes_usuario_id ON emails_pendentes (usuario_id);
