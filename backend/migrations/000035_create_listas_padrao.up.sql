-- Story 9.4: migração da Ferreira Costa para o modelo multi-Empresa
-- (Epic 9, FR-40/FR-41; AD-23), spec-9-4.
--
-- As listas padrão de Categoria (25 linhas, migration 000010) e de
-- Nomenclatura Guiada (28 linhas, migration 000013) moram HOJE dentro das
-- próprias tabelas de domínio, como linhas `empresa_id IS NULL` que servem de
-- molde a `services.ProvisionarEmpresa`. Enquanto for assim, `categorias` e
-- `nomenclatura_templates` NUNCA podem receber `SET NOT NULL` em
-- `empresa_id` — o molde é, por definição, de Empresa nenhuma.
--
-- Esta migration tira o molde de dentro do domínio: copia as duas listas para
-- tabelas PRÓPRIAS (`categorias_padrao`/`nomenclatura_templates_padrao`) e só
-- então apaga as linhas molde que NENHUM Produto referencia.
--
-- O `NOT EXISTS` é o ponto delicado: em produção (Ferreira Costa) os Produtos
-- legados apontam para essas mesmas linhas (`produtos.categoria_id` é NOT
-- NULL, migration 000011). Elas SOBREVIVEM aqui de propósito — o backfill da
-- Story 9.4 (`cmd/migrar-multi-empresa --etapa backfill`) as ADOTA para a
-- Empresa "Ferreira Costa", sem tocar em um único Produto. Numa instalação
-- nova (sem Produto algum) a limpeza é total e as duas tabelas de domínio
-- ficam sem nenhuma linha órfã, prontas para o endurecimento.
--
-- Sem `empresa_id`: estas tabelas são catálogo da PLATAFORMA, não de uma
-- Empresa — são a fonte a partir da qual cada Empresa nova recebe a sua
-- cópia (services.CopiarListasPadrao). `codigo`/`subtipo` são a chave natural
-- da cópia (o `WHERE NOT EXISTS` de CopiarListasPadrao casa por ela), por
-- isso são a PRIMARY KEY em vez de um UUID sintético.
CREATE TABLE categorias_padrao (
  codigo VARCHAR(10) PRIMARY KEY,
  nome VARCHAR(255) NOT NULL UNIQUE
);

CREATE TABLE nomenclatura_templates_padrao (
  subtipo VARCHAR(255) PRIMARY KEY,
  template VARCHAR(255) NOT NULL
);

INSERT INTO categorias_padrao (codigo, nome)
SELECT codigo, nome FROM categorias WHERE empresa_id IS NULL;

INSERT INTO nomenclatura_templates_padrao (subtipo, template)
SELECT subtipo, template FROM nomenclatura_templates WHERE empresa_id IS NULL;

-- Só DEPOIS da cópia, e só as linhas que ninguém referencia.
DELETE FROM categorias c
WHERE c.empresa_id IS NULL
  AND NOT EXISTS (SELECT 1 FROM produtos p WHERE p.categoria_id = c.id);

DELETE FROM nomenclatura_templates t
WHERE t.empresa_id IS NULL
  AND NOT EXISTS (SELECT 1 FROM produtos p WHERE p.template_id = t.id);
