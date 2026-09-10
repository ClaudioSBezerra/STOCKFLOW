-- Story 9.1: fundação Multi-Empresa — schema e isolamento por Empresa
-- (Epic 9, FR-40/FR-41; AD-20, AD-23), spec-9-1.
--
-- Fase 1 ADITIVA de AD-23: toda tabela de domínio ganha `empresa_id`
-- NULLABLE com FK para `empresas(id)`. Nenhum backfill, nenhum SET NOT NULL
-- aqui — os dois são da Story 9.4 (corte de dados da Ferreira Costa), depois
-- de toda linha legada já ter recebido sua Empresa.
--
-- Tabelas filhas (`produto_estoque`, `carrinho_itens`, `normalizacao_ignoradas`,
-- `importacao_linhas`, `sessoes`, `tokens_acao`, `emails_pendentes`) NÃO
-- ganham a coluna: a posse delas é sempre a do pai (Produto, Usuário,
-- Importação), e os services escopam por JOIN com o pai. `tokens_acao.token`
-- e `sessoes.refresh_token` continuam globalmente únicos (segredos opacos).
--
-- Unicidades hoje GLOBAIS viram compostas com `empresa_id`, usando
-- `NULLS NOT DISTINCT` (Postgres 15+): o valor NULL é tratado como comparável,
-- então as linhas legadas (`empresa_id IS NULL`) continuam colidindo entre si
-- EXATAMENTE como hoje — nenhuma unicidade é afrouxada entre 9.1 e 9.4 — e as
-- linhas novas passam a ser únicas por Empresa (o mesmo e-mail, o mesmo `adm`,
-- o mesmo nome de Estoque, o mesmo `codigo` de Produto e as mesmas listas
-- padrão de Categoria/Nomenclatura podem existir em Empresas distintas).
-- Os nomes de índice existentes são preservados (cmd/seed-admin reconhece a
-- corrida de `adm` pelo nome `idx_usuarios_unico_adm`).

ALTER TABLE usuarios ADD COLUMN empresa_id UUID NULL REFERENCES empresas(id);
ALTER TABLE produtos ADD COLUMN empresa_id UUID NULL REFERENCES empresas(id);
ALTER TABLE estoques ADD COLUMN empresa_id UUID NULL REFERENCES empresas(id);
ALTER TABLE movimentacoes ADD COLUMN empresa_id UUID NULL REFERENCES empresas(id);
ALTER TABLE pedidos ADD COLUMN empresa_id UUID NULL REFERENCES empresas(id);
ALTER TABLE pedido_itens ADD COLUMN empresa_id UUID NULL REFERENCES empresas(id);
ALTER TABLE categorias ADD COLUMN empresa_id UUID NULL REFERENCES empresas(id);
ALTER TABLE logs_acesso ADD COLUMN empresa_id UUID NULL REFERENCES empresas(id);
ALTER TABLE solicitacoes_promocao ADD COLUMN empresa_id UUID NULL REFERENCES empresas(id);
ALTER TABLE mesclagens_duplicatas ADD COLUMN empresa_id UUID NULL REFERENCES empresas(id);
ALTER TABLE mesclagem_produtos_removidos ADD COLUMN empresa_id UUID NULL REFERENCES empresas(id);
ALTER TABLE importacoes ADD COLUMN empresa_id UUID NULL REFERENCES empresas(id);
ALTER TABLE nomenclatura_templates ADD COLUMN empresa_id UUID NULL REFERENCES empresas(id);

-- Um índice por tabela: toda consulta de service passa a filtrar por
-- `empresa_id`, e a FK sem índice faria cada checagem de integridade (e cada
-- DELETE de Empresa, se um dia existir) varrer a tabela inteira.
CREATE INDEX idx_usuarios_empresa_id ON usuarios (empresa_id);
CREATE INDEX idx_produtos_empresa_id ON produtos (empresa_id);
CREATE INDEX idx_estoques_empresa_id ON estoques (empresa_id);
CREATE INDEX idx_movimentacoes_empresa_id ON movimentacoes (empresa_id);
CREATE INDEX idx_pedidos_empresa_id ON pedidos (empresa_id);
CREATE INDEX idx_pedido_itens_empresa_id ON pedido_itens (empresa_id);
CREATE INDEX idx_categorias_empresa_id ON categorias (empresa_id);
CREATE INDEX idx_logs_acesso_empresa_id ON logs_acesso (empresa_id);
CREATE INDEX idx_solicitacoes_promocao_empresa_id ON solicitacoes_promocao (empresa_id);
CREATE INDEX idx_mesclagens_duplicatas_empresa_id ON mesclagens_duplicatas (empresa_id);
CREATE INDEX idx_mesclagem_produtos_removidos_empresa_id ON mesclagem_produtos_removidos (empresa_id);
CREATE INDEX idx_importacoes_empresa_id ON importacoes (empresa_id);
CREATE INDEX idx_nomenclatura_templates_empresa_id ON nomenclatura_templates (empresa_id);

-- usuarios: e-mail único POR EMPRESA (o login nunca pergunta a Empresa — ela
-- vem do slug da URL) e no máximo um `adm` POR EMPRESA.
DROP INDEX idx_usuarios_email_lower;
CREATE UNIQUE INDEX idx_usuarios_email_lower
  ON usuarios (empresa_id, lower(email)) NULLS NOT DISTINCT;

DROP INDEX idx_usuarios_unico_adm;
CREATE UNIQUE INDEX idx_usuarios_unico_adm
  ON usuarios (empresa_id, papel) NULLS NOT DISTINCT WHERE papel = 'adm';

-- estoques: nome normalizado único POR EMPRESA.
DROP INDEX idx_estoques_nome_normalizado;
CREATE UNIQUE INDEX idx_estoques_nome_normalizado
  ON estoques (empresa_id, nome_normalizado) NULLS NOT DISTINCT;

-- produtos: `codigo` não-nulo único POR EMPRESA (o match "atualiza por
-- código" da importação passa a ser por Empresa — nunca atualiza o Produto de
-- outro cliente).
DROP INDEX idx_produtos_codigo;
CREATE UNIQUE INDEX idx_produtos_codigo
  ON produtos (empresa_id, codigo) NULLS NOT DISTINCT WHERE codigo IS NOT NULL;

-- categorias / nomenclatura_templates: cada Empresa recebe a própria cópia
-- das listas padrão (services.ProvisionarEmpresa); as linhas semeadas pelas
-- migrações 000010/000013 (empresa_id NULL) são o molde dessa cópia.
ALTER TABLE categorias DROP CONSTRAINT categorias_codigo_key;
ALTER TABLE categorias DROP CONSTRAINT categorias_nome_key;
CREATE UNIQUE INDEX idx_categorias_empresa_codigo
  ON categorias (empresa_id, codigo) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX idx_categorias_empresa_nome
  ON categorias (empresa_id, nome) NULLS NOT DISTINCT;

ALTER TABLE nomenclatura_templates DROP CONSTRAINT nomenclatura_templates_subtipo_key;
CREATE UNIQUE INDEX idx_nomenclatura_templates_empresa_subtipo
  ON nomenclatura_templates (empresa_id, subtipo) NULLS NOT DISTINCT;
