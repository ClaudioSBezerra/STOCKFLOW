-- Feedback de treinamento (Karla, Ferreira Costa, 2026-10-06): "colocar um
-- exemplo em cada template" — quem preenche o cadastro vê o molde
-- (`CABO [TIPO] [TENSÃO] Ø[SEÇÃO]MM² [COR] [COMPLEMENTO]`) mas não tem uma
-- referência de como ele fica preenchido de verdade.
--
-- `exemplo` é aditiva e NULLable nas duas tabelas (molde da plataforma
-- `nomenclatura_templates_padrao` e a cópia por Empresa
-- `nomenclatura_templates`, migration 000035) — puramente informativa,
-- nunca validada contra `template` (não é reprocessada por
-- nomeValidoParaTemplate). NULL para o template Genérico ([NOME LIVRE],
-- AD-34): não há nada para exemplificar num campo de texto livre.
ALTER TABLE nomenclatura_templates_padrao ADD COLUMN exemplo VARCHAR(255);
ALTER TABLE nomenclatura_templates ADD COLUMN exemplo VARCHAR(255);

UPDATE nomenclatura_templates_padrao SET exemplo = v.exemplo FROM (VALUES
  ('Cabos — Elétrico', 'CABO FLEXÍVEL 750V Ø2,5MM² AZUL ISOLAÇÃO DUPLA'),
  ('Cabos — Rede', 'CABO REDE BLINDADO CAT6 NBR14703 CINZA'),
  ('Cabos — Coaxial/Fibra/Especial', 'CABO COAXIAL RG6 ANATEL'),
  ('Elétrica — Luminárias', 'LUMINÁRIA LED EMBUTIR 60X60CM 36W 4000K'),
  ('Elétrica — Painéis/Quadros', 'QUADRO ELÉTRICO 380V DISTRIBUIÇÃO'),
  ('Elétrica — Tomadas/Interruptores', 'TOMADA INDUSTRIAL EXTERNA 2P+T 32A 220V IP66 VERMELHA'),
  ('Elétrica — Refletores', 'REFLETOR LED 100W 6500K ÁREA EXTERNA'),
  ('Elétrica — Abraçadeiras/Acess.', 'ABRAÇADEIRA NYLON PLÁSTICO 4,8MM'),
  ('Hidráulica — Conexões PVC', 'JOELHO 90° PVC SOLDÁVEL DN25 BRANCO'),
  ('Hidráulica — Válvulas/Registros', 'REGISTRO GAVETA DN50 BRONZE 150PSI'),
  ('Hidráulica — Louças/Vasos', 'BACIA SANITÁRIA CONVENCIONAL DECA BRANCA NOVA'),
  ('Hidráulica — Torneiras/Chuveiros', 'TORNEIRA COZINHA 1/2POL CROMADA'),
  ('Hidráulica — Mangueiras/Incêndio', 'MANGUEIRA INCÊND. 2,5POL 15M TIPO 1'),
  ('Tubo — Aço Carbono', 'TUBO AÇO CARBONO GALVANIZADO 1POL 6M'),
  ('Tubo — Aço Inox', 'TUBO INOX AISI304 Ø50MM 6M'),
  ('Tubo — PVC Esgoto/Água', 'TUBO PVC ESGOTO DN100 BRANCO NBR5688'),
  ('Tubo — PEAD/PPR', 'TUBO PEAD PN80 DN32'),
  ('Perfil — Aço Estrutural', 'PERFIL U AÇO 100X50MM 6M'),
  ('Perfil — Alumínio', 'PERFIL ALUMÍNIO TIPO U 20X20MM DIVISÓRIA'),
  ('Perfil — Cartola/Estrutural', 'PERFIL CARTOLA 30X30MM 3M'),
  ('Ferragem — Barras Roscadas', 'BARRA ROSCADA GALVANIZADA 3/8POL L=1M'),
  ('Ferragem — Telas de Aço', 'TELA AÇO SOLDADA Q-138 NBR7481 2,45X6M'),
  ('Ferragem — Chumbadores', 'CHUMBADOR EXP 3/8POL 80MM'),
  ('Ferragem — Estruturas Metálicas', 'ESTRUTURA METÁLICA TUBULAR GALPÃO 10X5M'),
  ('Mat. Construção — Pisos/Porcel.', 'PORCELANATO PORTOBELLO 60X60CM POLIDO 4PÇ/CX'),
  ('Mat. Construção — Parafusos/Fix.', 'PARAFUSO SEXTAVADO 1/4X50MM INOX'),
  ('Mat. Construção — Forro/Gesso', 'PLACA GESSO LISA 60X60CM'),
  ('Telha/Calha/Rufo', 'TELHA TRAPEZOIDAL AÇO 6X1,07M')
) AS v(subtipo, exemplo)
WHERE nomenclatura_templates_padrao.subtipo = v.subtipo;

-- Backfill das cópias já existentes por Empresa (Ferreira Costa e qualquer
-- outra já provisionada) — casando pelo MESMO subtipo do padrão. Uma Empresa
-- que já renomeou o próprio `subtipo` simplesmente não casa aqui e fica sem
-- exemplo (nada quebra: o campo é só informativo).
UPDATE nomenclatura_templates t
SET exemplo = p.exemplo
FROM nomenclatura_templates_padrao p
WHERE t.subtipo = p.subtipo AND p.exemplo IS NOT NULL;
