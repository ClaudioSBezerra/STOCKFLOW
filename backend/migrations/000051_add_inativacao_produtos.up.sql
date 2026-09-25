-- Story 16.1 (FR-55, AD-37): inativar e reativar um Produto.
--
-- "Ativo" = `inativado_em IS NULL AND deleted_at IS NULL`. `deleted_at`
-- continua exclusivo da mesclagem (Story 6.4) e nunca é reutilizado aqui.
-- Aditiva: todos os Produtos existentes seguem ativos (colunas NULL).
--
-- `inativado_por` fica SEM FK para `usuarios`: com FK, o
-- `TRUNCATE usuarios CASCADE` das suítes de teste passaria a truncar
-- `produtos` e tudo que o referencia. O autor com integridade referencial
-- fica em `produto_historico.ator_id`.
ALTER TABLE produtos
  ADD COLUMN inativado_em TIMESTAMPTZ NULL,
  ADD COLUMN inativado_por UUID NULL;

-- Trilha append-only do Produto (molde de `auditoria_seguranca`, 000048):
-- nenhuma rota de edição ou exclusão, só o INSERT feito pelo service. Nasce
-- com `inativado` (detalhe {"motivo"}) e `reativado` (detalhe {}); a Story
-- 16.3 reaproveita a tabela para `nome_alterado` ({"antes","depois"}), já
-- permitido no CHECK.
CREATE TABLE produto_historico (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  empresa_id UUID NOT NULL REFERENCES empresas(id),
  produto_id UUID NOT NULL REFERENCES produtos(id),
  ator_id UUID NOT NULL REFERENCES usuarios(id),
  acao VARCHAR(40) NOT NULL CHECK (acao IN ('nome_alterado', 'inativado', 'reativado')),
  detalhe JSONB NOT NULL DEFAULT '{}',
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_produto_historico_empresa_produto_criado_em
  ON produto_historico (empresa_id, produto_id, criado_em DESC);
