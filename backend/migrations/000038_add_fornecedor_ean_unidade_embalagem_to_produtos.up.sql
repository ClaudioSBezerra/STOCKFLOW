-- Story 10.3: Código do Fornecedor, EAN-13, Unidade de Medida e Embalagem
-- (Epic 10, FR45/FR46), spec-10-3.
--
-- Quatro colunas aditivas em `produtos`, todas NULLable:
--   - `codigo_fornecedor` (VARCHAR(255)) e `embalagem` (VARCHAR(255)): texto
--     livre opcional, sem checagem de unicidade, sem relação com
--     `produtos.codigo` (Story 10.2) nem com o Código de Identificação
--     (Story 4.5).
--   - `ean13` (CHAR(13)): opcional; quando informado, `services.CriarProduto`
--     já validou formato + dígito verificador ANTES do INSERT, então todo
--     valor gravado tem exatamente 13 caracteres — `CHAR` segue literalmente
--     AD-32, sem diferença prática de padding.
--   - `unidade_medida` (`unidade_medida_produto`, enum fechado com os 12
--     valores de addendum.md §F): NULLable no banco — PERMANECE NULLable
--     mesmo sendo obrigatório no formulário de cadastro (services.CriarProduto)
--     porque o INSERT em massa da importação (services/importacoes.go,
--     Story 3.3/3.4) não preenche esta coluna e fica fora do escopo desta
--     story; um NOT NULL quebraria esse INSERT.
--
-- O backfill roda ANTES de qualquer obrigatoriedade valer no cadastro
-- (AD-32): todo Produto já existente (cadastro manual anterior a esta
-- migration, ou importado) sem `unidade_medida` recebe `'un'` em lote.
CREATE TYPE unidade_medida_produto AS ENUM (
  'un', 'm', 'm²', 'm³', 'kg', 'L', 'cx', 'rolo', 'barra', 'mm', 'cm', 'kg/m²'
);

ALTER TABLE produtos
  ADD COLUMN codigo_fornecedor VARCHAR(255),
  ADD COLUMN ean13 CHAR(13),
  ADD COLUMN unidade_medida unidade_medida_produto,
  ADD COLUMN embalagem VARCHAR(255);

UPDATE produtos SET unidade_medida = 'un' WHERE unidade_medida IS NULL;
