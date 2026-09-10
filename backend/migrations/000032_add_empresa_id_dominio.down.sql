-- Reverte 000032 (Story 9.1). Recriar as unicidades GLOBAIS só é possível
-- enquanto nenhuma chave se repete entre Empresas — com dado de mais de uma
-- Empresa já gravado, o CREATE UNIQUE abaixo falha e a transação inteira da
-- migração é desfeita (nada fica pela metade).

DROP INDEX IF EXISTS idx_nomenclatura_templates_empresa_subtipo;
ALTER TABLE nomenclatura_templates ADD CONSTRAINT nomenclatura_templates_subtipo_key UNIQUE (subtipo);

DROP INDEX IF EXISTS idx_categorias_empresa_nome;
DROP INDEX IF EXISTS idx_categorias_empresa_codigo;
ALTER TABLE categorias ADD CONSTRAINT categorias_nome_key UNIQUE (nome);
ALTER TABLE categorias ADD CONSTRAINT categorias_codigo_key UNIQUE (codigo);

DROP INDEX IF EXISTS idx_produtos_codigo;
CREATE UNIQUE INDEX idx_produtos_codigo ON produtos (codigo) WHERE codigo IS NOT NULL;

DROP INDEX IF EXISTS idx_estoques_nome_normalizado;
CREATE UNIQUE INDEX idx_estoques_nome_normalizado ON estoques (nome_normalizado);

DROP INDEX IF EXISTS idx_usuarios_unico_adm;
CREATE UNIQUE INDEX idx_usuarios_unico_adm ON usuarios (papel) WHERE papel = 'adm';

DROP INDEX IF EXISTS idx_usuarios_email_lower;
CREATE UNIQUE INDEX idx_usuarios_email_lower ON usuarios (lower(email));

-- DROP COLUMN leva junto o índice simples e a FK de cada tabela.
ALTER TABLE nomenclatura_templates DROP COLUMN IF EXISTS empresa_id;
ALTER TABLE importacoes DROP COLUMN IF EXISTS empresa_id;
ALTER TABLE mesclagem_produtos_removidos DROP COLUMN IF EXISTS empresa_id;
ALTER TABLE mesclagens_duplicatas DROP COLUMN IF EXISTS empresa_id;
ALTER TABLE solicitacoes_promocao DROP COLUMN IF EXISTS empresa_id;
ALTER TABLE logs_acesso DROP COLUMN IF EXISTS empresa_id;
ALTER TABLE categorias DROP COLUMN IF EXISTS empresa_id;
ALTER TABLE pedido_itens DROP COLUMN IF EXISTS empresa_id;
ALTER TABLE pedidos DROP COLUMN IF EXISTS empresa_id;
ALTER TABLE movimentacoes DROP COLUMN IF EXISTS empresa_id;
ALTER TABLE estoques DROP COLUMN IF EXISTS empresa_id;
ALTER TABLE produtos DROP COLUMN IF EXISTS empresa_id;
ALTER TABLE usuarios DROP COLUMN IF EXISTS empresa_id;
