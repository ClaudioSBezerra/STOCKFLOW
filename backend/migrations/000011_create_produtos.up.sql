-- Story 3.1: cadastro manual de Produto com dimensões estruturadas (FR-8).
--
-- dimensao_unidade: unidade das 5 dimensões físicas do Produto (comprimento,
-- largura, diâmetro, altura, espessura). `{mm,cm,m}` replica os exemplos do
-- sistema legado ("6m", "100mm") — nenhum documento de planejamento enumera
-- valores específicos para dimensão (só para unidade de quantidade em
-- estoque, campo diferente e fora do escopo de FR-8/desta story).
CREATE TYPE dimensao_unidade AS ENUM ('mm', 'cm', 'm');

-- produtos: cada uma das 5 dimensões é um PAR `{campo}_valor` +
-- `{campo}_unidade`, nunca texto livre (AD-9) — validado em
-- services.CriarProduto antes de qualquer escrita: valor sem unidade (ou
-- vice-versa) rejeita só aquele campo, nada é gravado.
--
-- `codigo` NULL e SEM unicidade nesta story: Story 3.4 decide o
-- comportamento de "atualiza por código" da importação; `nome` também sem
-- unicidade — nomes duplicados são esperados e tratados pela Story 6.3
-- (Normalização/Duplicatas), nunca pelo banco.
--
-- Sem `template_id` (Story 3.2 acrescenta via ALTER TABLE, Nomenclatura
-- Guiada) e sem `deleted_at` (Epic 6 acrescenta quando a mesclagem de
-- duplicatas existir) — nenhum dos dois é usado por esta story.
CREATE TABLE produtos (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  nome VARCHAR(255) NOT NULL,
  codigo VARCHAR(255),
  categoria_id UUID NOT NULL REFERENCES categorias(id),
  comprimento_valor NUMERIC(10, 3),
  comprimento_unidade dimensao_unidade,
  largura_valor NUMERIC(10, 3),
  largura_unidade dimensao_unidade,
  diametro_valor NUMERIC(10, 3),
  diametro_unidade dimensao_unidade,
  altura_valor NUMERIC(10, 3),
  altura_unidade dimensao_unidade,
  espessura_valor NUMERIC(10, 3),
  espessura_unidade dimensao_unidade,
  observacoes TEXT,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);
