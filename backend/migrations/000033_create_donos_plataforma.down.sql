-- Reverte a 000033 (Story 9.2, spec-9-2).
--
-- Pré-condição: nenhum Ambiente de Treinamento gravado. Com um deles no
-- banco, a volta da unicidade GLOBAL de CNPJ falha (o Treinamento compartilha
-- o CNPJ da origem) — de propósito: esta migration nunca apaga Empresa.
DELETE FROM emails_pendentes WHERE tipo = 'primeiro_acesso';
ALTER TABLE emails_pendentes DROP CONSTRAINT emails_pendentes_tipo_check;
ALTER TABLE emails_pendentes ADD CONSTRAINT emails_pendentes_tipo_check
  CHECK (tipo IN ('verificacao_conta', 'redefinicao_senha'));

DROP INDEX IF EXISTS empresas_treinamento_unico;
DROP INDEX IF EXISTS empresas_cnpj_unico;
ALTER TABLE empresas ADD CONSTRAINT empresas_cnpj_unico UNIQUE (cnpj);

DROP TABLE IF EXISTS sessoes_plataforma;
DROP TABLE IF EXISTS donos_plataforma;
