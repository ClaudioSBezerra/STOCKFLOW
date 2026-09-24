-- Story 15.1 (FR-42 revisado, AD-36): o e-mail passa a ser único somando todas
-- as Empresas reais. `empresa_raiz_id` é a Empresa real da conta
-- (`COALESCE(empresas.empresa_origem_id, empresas.id)`): para uma conta de
-- Treinamento é a Empresa de origem, para as demais é a própria Empresa.
--
-- A regra é uma restrição de exclusão: o mesmo `lower(email)` não pode existir
-- com `empresa_raiz_id` diferente. O Treinamento repete livremente o e-mail da
-- SUA Empresa real (mesma raiz). O índice único (empresa_id, lower(email)) de
-- 000032 continua valendo dentro de cada Empresa.
--
-- A coluna é preenchida SEMPRE pelo trigger `BEFORE INSERT OR UPDATE OF
-- empresa_id` (sobrescreve o que vier no INSERT); nenhum código de inserção a
-- informa. Conta de domínio nunca troca de Empresa, mas o backfill da
-- Multi-Empresa (`cmd/migrar-multi-empresa`) dá Empresa a uma conta que não
-- tinha — o UPDATE também recalcula a raiz.
--
-- A migration é transacional e falha ANTES de criar a restrição, listando os
-- e-mails repetidos, se já houver duplicata entre Empresas reais.
--
-- Conta sem Empresa (`empresa_id IS NULL`) fica com `empresa_raiz_id` NULL e
-- fora da regra de e-mail (NULL nunca conflita na restrição de exclusão). O
-- único caso real é a conta sintética "Migração do sistema legado" da 000022,
-- que num banco novo (CI, instalação limpa) nunca ganha Empresa — a primeira
-- versão desta migration abortava nesse caso e derrubou o boot de um ambiente
-- novo. Ela nunca faz login (ativo=false, sem senha). O CHECK
-- `usuarios_empresa_raiz_coerente` garante NULL só quando não há Empresa.
--
-- Se a pré-checagem de duplicata abortar, o schema fica na versão 48, mas o
-- golang-migrate grava `schema_migrations` como versão 49 `dirty` e a API
-- não sobe mais. Recuperação: corrigir os dados apontados na mensagem e rodar
-- `migrate force 48` (ou `UPDATE schema_migrations SET version = 48,
-- dirty = false`) antes do próximo boot.

CREATE EXTENSION IF NOT EXISTS btree_gist;

ALTER TABLE usuarios ADD COLUMN empresa_raiz_id UUID NULL REFERENCES empresas(id);

UPDATE usuarios u
   SET empresa_raiz_id = COALESCE(e.empresa_origem_id, e.id)
  FROM empresas e
 WHERE e.id = u.empresa_id;

DO $$
DECLARE
  repetidos TEXT;
BEGIN
  SELECT string_agg(email_lower, ', ' ORDER BY email_lower) INTO repetidos
    FROM (
      SELECT lower(email) AS email_lower
        FROM usuarios
       GROUP BY lower(email)
      HAVING count(DISTINCT empresa_raiz_id) > 1
    ) d;
  IF repetidos IS NOT NULL THEN
    RAISE EXCEPTION 'migration 000049: e-mails com conta em mais de uma Empresa real: %', repetidos;
  END IF;
END $$;

ALTER TABLE usuarios
  ADD CONSTRAINT usuarios_empresa_raiz_coerente
  CHECK ((empresa_id IS NULL) = (empresa_raiz_id IS NULL));

CREATE FUNCTION usuarios_preencher_empresa_raiz() RETURNS trigger AS $$
BEGIN
  SELECT COALESCE(e.empresa_origem_id, e.id) INTO NEW.empresa_raiz_id
    FROM empresas e
   WHERE e.id = NEW.empresa_id;
  RETURN NEW;
END $$ LANGUAGE plpgsql;

CREATE TRIGGER usuarios_preencher_empresa_raiz
  BEFORE INSERT OR UPDATE OF empresa_id ON usuarios
  FOR EACH ROW EXECUTE FUNCTION usuarios_preencher_empresa_raiz();

-- O índice GiST da restrição começa por lower(email) e não serve às buscas da
-- FK (DELETE/UPDATE em empresas); índice próprio para empresa_raiz_id.
CREATE INDEX idx_usuarios_empresa_raiz_id ON usuarios (empresa_raiz_id);

ALTER TABLE usuarios
  ADD CONSTRAINT usuarios_email_unico_entre_empresas_reais
  EXCLUDE USING gist (lower(email) WITH =, empresa_raiz_id WITH <>);
