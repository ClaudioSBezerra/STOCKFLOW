-- Story 10.1: template "Genérico" ([NOME LIVRE], AD-34) como fallback
-- universal de Nomenclatura Guiada — spec-10-1.
--
-- `CriarProduto` passa a exigir `template_id` sempre (nenhum caminho de
-- cadastro sem template); das 25 Categorias só ~9 têm um template estrutural
-- fiel (addendum §G), então as ~16 restantes precisam de um template que
-- aceite qualquer nome não vazio — é o que `nomeValidoParaTemplate` (branch
-- do marcador `[NOME LIVRE]`) implementa em services/nomenclatura.go.
--
-- Mesmo padrão de 000035: insere no MOLDE (`nomenclatura_templates_padrao`)
-- e faz backfill idempotente (`WHERE NOT EXISTS`, casando por
-- `empresa_id`+`subtipo` — mesma chave do índice único
-- `idx_nomenclatura_templates_empresa_subtipo`, migration 000032) em
-- `nomenclatura_templates` de toda Empresa já existente. Empresas
-- provisionadas DEPOIS desta migration recebem a linha automaticamente via
-- `services.CopiarListasPadrao` (chamada por `ProvisionarEmpresa`), sem
-- nenhuma mudança de código nela — o molde já contém a linha nova.
--
-- Nunca via lazy-init em runtime: o fallback tem de existir ANTES de
-- qualquer cadastro rodar, para que "template obrigatório" nunca deixe uma
-- Empresa sem opção alguma.
INSERT INTO nomenclatura_templates_padrao (subtipo, template)
VALUES ('Genérico', '[NOME LIVRE]');

INSERT INTO nomenclatura_templates (subtipo, template, empresa_id)
SELECT 'Genérico', '[NOME LIVRE]', e.id
FROM empresas e
WHERE NOT EXISTS (
  SELECT 1 FROM nomenclatura_templates t
  WHERE t.empresa_id = e.id AND t.subtipo = 'Genérico'
);
