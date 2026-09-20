-- Story 10.5: CRUD de Categorias (Epic 10, AD-33) — limites de tamanho no
-- banco: `codigo` VARCHAR(8) e `nome` VARCHAR(50), mais CHECK de não-vazio.
--
-- `categorias_padrao` (molde de ProvisionarEmpresa/CopiarListasPadrao) recebe
-- os MESMOS tipos: se só `categorias` mudasse, a cópia para cada Empresa nova
-- passaria a violar o limite.
--
-- Única linha seed que não cabe em 50: '09.001' "Peças/Materiais para
-- Equipamentos/Veículos/Máquinas" (51 caracteres). Ela é renomeada (49) ANTES
-- do ALTER, em `categorias` (todas as Empresas) e em `categorias_padrao`.
-- Nenhum Produto muda — apontam por `id`.
UPDATE categorias
   SET nome = 'Peças/Materiais p/ Equipamentos/Veículos/Máquinas'
 WHERE codigo = '09.001'
   AND nome = 'Peças/Materiais para Equipamentos/Veículos/Máquinas';

UPDATE categorias_padrao
   SET nome = 'Peças/Materiais p/ Equipamentos/Veículos/Máquinas'
 WHERE codigo = '09.001'
   AND nome = 'Peças/Materiais para Equipamentos/Veículos/Máquinas';

ALTER TABLE categorias
  ALTER COLUMN codigo TYPE VARCHAR(8),
  ALTER COLUMN nome TYPE VARCHAR(50),
  ADD CONSTRAINT categorias_codigo_nao_vazio CHECK (btrim(codigo) <> ''),
  ADD CONSTRAINT categorias_nome_nao_vazio CHECK (btrim(nome) <> '');

ALTER TABLE categorias_padrao
  ALTER COLUMN codigo TYPE VARCHAR(8),
  ALTER COLUMN nome TYPE VARCHAR(50),
  ADD CONSTRAINT categorias_padrao_codigo_nao_vazio CHECK (btrim(codigo) <> ''),
  ADD CONSTRAINT categorias_padrao_nome_nao_vazio CHECK (btrim(nome) <> '');
