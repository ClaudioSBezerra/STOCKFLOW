-- Logo da Empresa impressa na Pré-Nota (spec-logo-da-empresa).
--
-- Uma logo por Empresa (vale para todas as Filiais), guardada no banco — NÃO
-- em FOTOS_DIR (volume diferente por ambiente e o Catálogo varre esse
-- diretório). Os bytes são exatamente os enviados (PNG ou JPEG já validados
-- por services/empresa_logo.go); enviar de novo substitui a linha.
CREATE TABLE empresa_logos (
  empresa_id UUID PRIMARY KEY REFERENCES empresas (id) ON DELETE CASCADE,
  tipo_conteudo TEXT NOT NULL CHECK (tipo_conteudo IN ('image/png', 'image/jpeg')),
  conteudo BYTEA NOT NULL CHECK (octet_length(conteudo) BETWEEN 1 AND 524288),
  atualizado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);
