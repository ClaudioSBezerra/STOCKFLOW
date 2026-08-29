CREATE TABLE usuarios (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  nome VARCHAR(255) NOT NULL,
  email VARCHAR(255) NOT NULL,
  senha_hash TEXT,
  papel VARCHAR(20) NOT NULL CHECK (papel IN ('usuario','almoxarife','gestor','adm')),
  email_verificado BOOLEAN NOT NULL DEFAULT false,
  ativo BOOLEAN NOT NULL DEFAULT true,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_usuarios_email_lower ON usuarios (lower(email));

-- Backstop de correção contra corrida entre execuções concorrentes de
-- cmd/seed-admin: garante no nível do banco que nunca existe mais de uma
-- conta com papel='adm', independente da checagem sequencial na aplicação.
CREATE UNIQUE INDEX idx_usuarios_unico_adm ON usuarios (papel) WHERE papel = 'adm';
