-- Story 3.1: cadastro manual de Produto com dimensões estruturadas (FR-8).
--
-- categorias: lista fixa de ~25 categorias de Produto (addendum §H) —
-- selecionável no formulário de cadastro, nunca digitável livremente (AC4).
-- Seedada AQUI, não só na migração legada (Story 3.7): o contexto do épico
-- permite explicitamente "seed equivalente antes" da 3.7, e sem isso a AC4
-- desta story não seria verificável agora. A 3.7 encontra as linhas já
-- gravadas e não as reinsere.
--
-- `codigo` (ex. "04.001") e `nome` são cada um únicos — mesma fonte hoje
-- duplicada em dois lugares do sistema legado, agora tabela única no schema
-- alvo.
CREATE TABLE categorias (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  codigo VARCHAR(10) NOT NULL UNIQUE,
  nome VARCHAR(255) NOT NULL UNIQUE
);

INSERT INTO categorias (codigo, nome) VALUES
  ('04.001', 'Materiais Civis'),
  ('04.002', 'Materiais Elétricos'),
  ('04.003', 'Materiais de Acabamentos/Cobertura'),
  ('04.004', 'Materiais de Instalações Especiais'),
  ('04.005', 'Materiais/Estruturas Metálicas'),
  ('04.006', 'Materiais Hidrossanitários'),
  ('04.007', 'Madeiramento'),
  ('05.001', 'EPI/EPC'),
  ('05.002', 'Medicina do Trabalho'),
  ('05.003', 'Fardamentos'),
  ('05.004', 'Programa de Segurança'),
  ('06.001', 'Materiais de Escritório'),
  ('06.002', 'Materiais de Limpeza'),
  ('07.001', 'Equipamentos/Máquinas Alugados'),
  ('07.002', 'Veículos Alugados'),
  ('08.001', 'Equipamentos/Máquinas Adquiridos'),
  ('08.002', 'Ferramentas Adquiridas (Imobilizado/Ativos)'),
  ('08.003', 'Veículos Adquiridos'),
  ('09.001', 'Peças/Materiais para Equipamentos/Veículos/Máquinas'),
  ('10.001', 'Ferramentas Adquiridas (Consumo)'),
  ('10.002', 'Ferramentas Alugadas'),
  ('11.001', 'Combustíveis e Lubrificantes'),
  ('12.001', 'Verbas, Licenças e Alvarás'),
  ('12.002', 'Impostos'),
  ('13.001', 'Equipamentos Esportivos e Recreativos');
