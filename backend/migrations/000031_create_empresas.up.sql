-- Story 9.1: fundação Multi-Empresa — schema e isolamento por Empresa
-- (Epic 9, FR-40/FR-41; AD-19, AD-20, AD-23), spec-9-1.
--
-- empresas: a fronteira de isolamento do produto. Toda tabela de domínio
-- passa a carregar `empresa_id` (migração 000032) e toda consulta de service
-- filtra por ele explicitamente (AD-20 — nunca RLS, nunca schema-por-tenant).
--
-- `slug` é a chave de resolução em TODA requisição: o prefixo de rota
-- `/e/{slug}/api/...` é resolvido uma única vez por requisição no middleware
-- (middleware.RequireEmpresa, AD-19) e o id resultante é repassado como
-- argumento explícito a cada função de service. `cnpj` é único na
-- plataforma inteira (CHAR(14), só dígitos — a normalização/validação dos
-- dígitos verificadores é de services.ValidarCNPJ, nunca do banco).
--
-- `status = 'inativa'` faz o slug deixar de resolver (404 em toda rota sob o
-- prefixo, inclusive login), sem apagar nenhum dado.
--
-- `empresa_origem_id` é o vínculo de uma Empresa derivada (Ambiente de
-- Treinamento, Story 9.3) com a Empresa de onde foi copiada. NULL para toda
-- Empresa "raiz".
--
-- NENHUM INSERT aqui, de propósito: a Empresa "Ferreira Costa" não pode
-- existir antes da Story 9.4 (AC explícito dela) e a criação pela interface é
-- da Story 9.2 — as duas usam services.ProvisionarEmpresa.
CREATE TABLE empresas (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  nome_fantasia VARCHAR(255) NOT NULL,
  razao_social VARCHAR(255) NOT NULL,
  cnpj CHAR(14) NOT NULL,
  logradouro VARCHAR(255) NOT NULL,
  numero VARCHAR(20) NOT NULL,
  complemento VARCHAR(255),
  bairro VARCHAR(255) NOT NULL,
  cidade VARCHAR(255) NOT NULL,
  cep CHAR(8) NOT NULL,
  uf CHAR(2) NOT NULL,
  slug VARCHAR(63) NOT NULL,
  status VARCHAR(10) NOT NULL DEFAULT 'ativa' CHECK (status IN ('ativa', 'inativa')),
  empresa_origem_id UUID NULL REFERENCES empresas(id),
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT empresas_cnpj_unico UNIQUE (cnpj),
  CONSTRAINT empresas_slug_unico UNIQUE (slug)
);

-- FK auto-referente consultada pela Story 9.3 (listar os Ambientes de
-- Treinamento de uma Empresa) — sem índice, cada busca seria um full scan.
CREATE INDEX idx_empresas_empresa_origem_id ON empresas (empresa_origem_id);
