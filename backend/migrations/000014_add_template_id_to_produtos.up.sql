-- Story 3.2: Nomenclatura Guiada por subtipo (FR-9).
--
-- `template_id` é nullable e sem `ON DELETE` especial: linhas de
-- `nomenclatura_templates` são seed fixo, nunca excluídas em regime — não há
-- necessidade de decidir um comportamento de cascata/nulificação aqui.
-- `NULL` significa "Produto sem template aplicado" -> nome livre, mesmo
-- comportamento da Story 3.1 (services.CriarProduto).
ALTER TABLE produtos ADD COLUMN template_id UUID REFERENCES nomenclatura_templates(id);
