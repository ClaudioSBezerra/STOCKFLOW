-- Story 9.2: Dono da Plataforma cria Empresas com Ambiente de Treinamento
-- automático (Epic 9, FR-41/FR-43; AD-6, AD-12, AD-21, AD-23), spec-9-2.
--
-- donos_plataforma: a identidade "Dono da Plataforma" (AD-21), DISJUNTA de
-- `usuarios` — tabela, sessões, cookie, rotas (`/api/plataforma/...`) e
-- middleware próprios. O papel não entra na escala de RankPapel e nunca
-- pertence a uma Empresa (não há `empresa_id` aqui). A primeira linha só nasce
-- pelo CLI `cmd/seed-dono-plataforma` (AD-12): nenhuma rota HTTP insere nesta
-- tabela.
--
-- MFA é ESTRUTURAL, não uma opção de conta: `mfa_habilitado` só aceita true
-- (CHECK) e `mfa_secret` é NOT NULL — não existe Dono sem segundo fator.
-- `mfa_ultimo_passo_usado` recusa o reuso do mesmo código TOTP (mesmo padrão
-- de `usuarios`, Story 1.11).
CREATE TABLE donos_plataforma (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  nome VARCHAR(255) NOT NULL,
  email VARCHAR(255) NOT NULL,
  senha_hash TEXT NOT NULL,
  mfa_habilitado BOOLEAN NOT NULL DEFAULT true
    CONSTRAINT donos_plataforma_mfa_obrigatoria CHECK (mfa_habilitado),
  mfa_secret TEXT NOT NULL,
  mfa_ultimo_passo_usado BIGINT,
  ativo BOOLEAN NOT NULL DEFAULT true,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_donos_plataforma_email_lower ON donos_plataforma (lower(email));

-- sessoes_plataforma: refresh tokens rotativos do Dono — mesmo formato AD-6
-- de `sessoes` (Story 1.4), mas em tabela própria: um refresh de `usuarios`
-- nunca rotaciona uma sessão do Dono, nem o contrário. ON DELETE CASCADE:
-- remover um Dono (só por operação manual) leva as sessões dele junto.
CREATE TABLE sessoes_plataforma (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  dono_id UUID NOT NULL REFERENCES donos_plataforma(id) ON DELETE CASCADE,
  refresh_token TEXT NOT NULL UNIQUE,
  expira_em TIMESTAMPTZ NOT NULL,
  revogado_em TIMESTAMPTZ,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- FK consultada diretamente e alvo de ON DELETE CASCADE (mesma justificativa
-- de idx_sessoes_usuario_id, migration 000003).
CREATE INDEX idx_sessoes_plataforma_dono_id ON sessoes_plataforma (dono_id);

-- De propósito, NENHUMA FK de `empresas` para `donos_plataforma`: as suítes de
-- teste limpam tabelas de identidade com `TRUNCATE ... CASCADE`, e uma FK
-- dessas levaria `empresas` junto e, em cadeia, o seed de `categorias`/
-- `nomenclatura_templates` que nenhuma migration recria (incidente da
-- spec-9-1). A autoria da criação de uma Empresa fica no log (slog).

-- CNPJ único só entre Empresas REAIS: o Ambiente de Treinamento é legalmente o
-- mesmo cliente e herda o CNPJ da origem (o dado verdadeiro). O índice parcial
-- mantém exatamente a regra "uma Empresa real por CNPJ" e deixa o Treinamento
-- compartilhar o valor. O NOME `empresas_cnpj_unico` é preservado:
-- services.ProvisionarEmpresa reconhece a duplicata pelo nome da constraint.
ALTER TABLE empresas DROP CONSTRAINT empresas_cnpj_unico;
CREATE UNIQUE INDEX empresas_cnpj_unico ON empresas (cnpj) WHERE empresa_origem_id IS NULL;

-- No máximo UM Ambiente de Treinamento por Empresa.
CREATE UNIQUE INDEX empresas_treinamento_unico ON empresas (empresa_origem_id)
  WHERE empresa_origem_id IS NOT NULL;

-- E-mail de primeiro acesso do `adm` provisionado pelo Dono: o outbox recusa
-- um tipo fora deste CHECK.
ALTER TABLE emails_pendentes DROP CONSTRAINT emails_pendentes_tipo_check;
ALTER TABLE emails_pendentes ADD CONSTRAINT emails_pendentes_tipo_check
  CHECK (tipo IN ('verificacao_conta', 'redefinicao_senha', 'primeiro_acesso'));
