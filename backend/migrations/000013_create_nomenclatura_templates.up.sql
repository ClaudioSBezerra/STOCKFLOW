-- Story 3.2: Nomenclatura Guiada por subtipo (FR-9).
--
-- nomenclatura_templates: lista fixa dos 28 templates de nomenclatura, um por
-- subtipo de material (addendum §G) — selecionável (opcional) no formulário
-- de cadastro de Produto, nunca digitável livremente. Seedada AQUI (fonte
-- única, não reconstruída a partir do legado), mesmo padrão de `categorias`
-- (migração 000010): a Story 3.7 encontra as linhas já gravadas e não as
-- reinsere.
--
-- `subtipo` e `template` são cada um `NOT NULL`; `subtipo` também `UNIQUE` —
-- é a chave visível ao usuário no `<Select>` do formulário.
CREATE TABLE nomenclatura_templates (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  subtipo VARCHAR(255) NOT NULL UNIQUE,
  template VARCHAR(255) NOT NULL
);

INSERT INTO nomenclatura_templates (subtipo, template) VALUES
  ('Cabos — Elétrico', 'CABO [TIPO] [TENSÃO] Ø[SEÇÃO]MM² [COR] [COMPLEMENTO]'),
  ('Cabos — Rede', 'CABO REDE [BLINDA] [CAT] [NORMA] [COR]'),
  ('Cabos — Coaxial/Fibra/Especial', 'CABO [TIPO] [ESPECIF] [NORMA/HOMOL]'),
  ('Elétrica — Luminárias', 'LUMINÁRIA [TIPO FONTE] [APLICAÇÃO] [DIM] [POTÊNCIA] [TEMP.COR]'),
  ('Elétrica — Painéis/Quadros', 'PAINEL [OU QUADRO] ELÉTRICO [TENSÃO] [TIPO]'),
  ('Elétrica — Tomadas/Interruptores', 'TOMADA INDUSTRIAL [APLICAÇÃO] [POLOS] [CORRENTE]A [TENSÃO]V [IP] [COR]'),
  ('Elétrica — Refletores', 'REFLETOR LED [POTÊNCIA]W [TEMP.COR] [APLICAÇÃO]'),
  ('Elétrica — Abraçadeiras/Acess.', 'ABRAÇADEIRA [TIPO] [MATERIAL] [DIAM/BITOLA]'),
  ('Hidráulica — Conexões PVC', '[PEÇA] PVC [CLASSE] DN[XX] [COR]'),
  ('Hidráulica — Válvulas/Registros', 'VÁLVULA [OU REGISTRO] [TIPO] DN[XX] [MATERIAL] [PRESSÃO]'),
  ('Hidráulica — Louças/Vasos', 'BACIA SANITÁRIA [TIPO] [MODELO/MARCA] [COR] [ESTADO]'),
  ('Hidráulica — Torneiras/Chuveiros', 'TORNEIRA [APLICAÇÃO] [DN/POL] [MATERIAL/ACAB]'),
  ('Hidráulica — Mangueiras/Incêndio', 'MANGUEIRA INCÊND. [DIAM] [COMP] [TIPO]'),
  ('Tubo — Aço Carbono', 'TUBO AÇO CARBONO [ACAB] [BITOLA] [COMP]'),
  ('Tubo — Aço Inox', 'TUBO INOX [NORMA/LIGA] Ø[XX]MM [COMP]'),
  ('Tubo — PVC Esgoto/Água', 'TUBO PVC [TIPO] DN[XX] [COR] NBR[XXXX]'),
  ('Tubo — PEAD/PPR', 'TUBO PEAD [PN] DN[XX]'),
  ('Perfil — Aço Estrutural', 'PERFIL [SEÇÃO W/I/U/Z/L] AÇO [MEDIDA H]X[BF]MM [COMP]'),
  ('Perfil — Alumínio', 'PERFIL ALUMÍNIO TIPO [H/U/T/Z/CART.] [MEDIDA] [APLICAÇÃO]'),
  ('Perfil — Cartola/Estrutural', 'PERFIL CARTOLA [MEDIDA H]X[LA]MM [COMP]'),
  ('Ferragem — Barras Roscadas', 'BARRA ROSCADA [MATERIAL/ACAB] [BITOLA] L=[XX]M'),
  ('Ferragem — Telas de Aço', 'TELA AÇO SOLDADA Q-[XXX] [NORMA] [LARG]X[COMP]M'),
  ('Ferragem — Chumbadores', 'CHUMBADOR [TIPO: J/CBA/EXP] [BITOLA] [COMP]'),
  ('Ferragem — Estruturas Metálicas', 'ESTRUTURA METÁLICA TUBULAR [APLICAÇÃO] [DIMENSÕES]'),
  ('Mat. Construção — Pisos/Porcel.', 'PORCELANATO [MARCA] [DIM]CM [TIPO/ACAB] [QDE PCJ/CX]'),
  ('Mat. Construção — Parafusos/Fix.', 'PARAFUSO [TIPO] [BITOLA]X[COMP]MM [MATERIAL/ACAB]'),
  ('Mat. Construção — Forro/Gesso', 'PLACA GESSO [TIPO: GRELHA/LISA/ST] [DIM]CM'),
  ('Telha/Calha/Rufo', 'TELHA [PERFIL: TRAP/ONDA] [MATERIAL: AC/AL] [COMP]X[LARG]M');
