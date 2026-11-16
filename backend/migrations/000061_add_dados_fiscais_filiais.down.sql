-- Reverte 000061: remove os dados fiscais/endereço de `filiais` e as Filiais/
-- nome do Estoque destino gravados em `pedido_itens` na aprovação.
ALTER TABLE pedido_itens
  DROP COLUMN estoque_destino_nome,
  DROP COLUMN filial_destino_id,
  DROP COLUMN filial_origem_id;
DROP INDEX IF EXISTS idx_filiais_empresa_cnpj;
ALTER TABLE filiais
  DROP COLUMN uf,
  DROP COLUMN cep,
  DROP COLUMN cidade,
  DROP COLUMN bairro,
  DROP COLUMN complemento,
  DROP COLUMN numero,
  DROP COLUMN logradouro,
  DROP COLUMN inscricao_estadual,
  DROP COLUMN cnpj;
