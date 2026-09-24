-- Story 14.3 (FR-53, AD-35): trilha append-only de eventos de segurança da
-- Empresa. Nasce com a alteração da exigência de MFA pelo `adm`
-- (`exigencia_alterada`, detalhe {"anterior","novo"}); a Story 14.4 reaproveita
-- a tabela para `mfa_resetado`/`mfa_desligado`, já permitidos no CHECK.
--
-- Append-only como `logs_acesso`: nenhuma rota de edição ou exclusão, só o
-- INSERT feito pelo service. Sem trigger. Escopada por `empresa_id` e
-- consultável só pelo `adm` da própria Empresa. `alvo_id` é NULL quando o
-- evento é da Empresa, não de uma conta.
CREATE TABLE auditoria_seguranca (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  empresa_id UUID NOT NULL REFERENCES empresas(id),
  ator_id UUID NOT NULL REFERENCES usuarios(id),
  alvo_id UUID NULL REFERENCES usuarios(id),
  acao VARCHAR(40) NOT NULL CHECK (acao IN ('exigencia_alterada', 'mfa_resetado', 'mfa_desligado')),
  detalhe JSONB NOT NULL DEFAULT '{}',
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_auditoria_seguranca_empresa_criado_em ON auditoria_seguranca (empresa_id, criado_em DESC);
