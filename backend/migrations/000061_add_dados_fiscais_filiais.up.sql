-- Filial com CNPJ, Inscrição Estadual e endereço (spec-filial-dados-fiscais).
--
-- Todos os campos são OPCIONAIS (NULL por padrão, decisão do usuário em
-- 2026-11-16) e têm os mesmos tipos das colunas equivalentes de `empresas`
-- (000031). As regras (CNPJ com dígito verificador, endereço como bloco —
-- tudo ou nada, salvo complemento —, CEP com 8 dígitos, UF válida) ficam na
-- camada de services (services/filiais.go), como em `empresas`.
--
-- Estoques NÃO ganham endereço: herdam o da Filial.
--
-- CNPJ é único por Empresa entre as Filiais (índice único parcial: Filial sem
-- CNPJ nunca colide). Colisão -> 23505 em `idx_filiais_empresa_cnpj` -> 409.
ALTER TABLE filiais
  ADD COLUMN cnpj CHAR(14) NULL,
  ADD COLUMN inscricao_estadual VARCHAR(20) NULL,
  ADD COLUMN logradouro VARCHAR(255) NULL,
  ADD COLUMN numero VARCHAR(20) NULL,
  ADD COLUMN complemento VARCHAR(255) NULL,
  ADD COLUMN bairro VARCHAR(255) NULL,
  ADD COLUMN cidade VARCHAR(255) NULL,
  ADD COLUMN cep CHAR(8) NULL,
  ADD COLUMN uf CHAR(2) NULL;

CREATE UNIQUE INDEX idx_filiais_empresa_cnpj ON filiais (empresa_id, cnpj) WHERE cnpj IS NOT NULL;

-- Revisão (spec-filial-dados-fiscais, Spec Change Log 2026-11-16): a aprovação
-- em lote (`decidirUmPedidoDoLoteTx`, services/pedidos.go) grava em
-- `pedido_itens` QUAL Filial era a de origem e a de destino e o nome do
-- Estoque destino — só identificadores/nome, NUNCA os dados fiscais (esses
-- seguem lidos do cadastro atual da Filial). Assim uma Pré-Nota antiga não
-- perde Filial/CNPJ/endereço quando um Estoque for excluído depois.
-- Sem FK (mesmo desenho de `pedido_itens.estoque_id`/`estoque_destino_id`).
-- NULL em Pedidos aprovados antes desta migration, em Baixa (destino) e em
-- retirada: o PDF cai no caminho antigo via `estoques.filial_id`.
-- `estoque_destino_nome` tem o mesmo tipo de `estoque_nome` (TEXT, 000026).
ALTER TABLE pedido_itens
  ADD COLUMN filial_origem_id UUID NULL,
  ADD COLUMN filial_destino_id UUID NULL,
  ADD COLUMN estoque_destino_nome TEXT NULL;
