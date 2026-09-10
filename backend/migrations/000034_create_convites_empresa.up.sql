-- Story 9.3: Convite nominal de acesso a uma Empresa (Epic 9, FR-42; AD-22),
-- spec-9-3.
--
-- convites_empresa: a ÚNICA porta de entrada do autocadastro a partir desta
-- story. Antes dela, `POST /e/{slug}/api/auth/cadastro` era aberto — qualquer
-- pessoa que descobrisse o slug de uma Empresa criava conta nela. Agora a
-- conta só nasce a partir de um convite NOMINAL (o e-mail do cadastro tem de
-- ser exatamente o do convite) e de USO ÚNICO (`usado_em`, marcado na MESMA
-- transação do INSERT em `usuarios`).
--
-- Tabela PRÓPRIA, nunca `tokens_acao` (AD-22): um convite não tem dono —
-- `tokens_acao.usuario_id` é NOT NULL e a conta ainda não existe no momento
-- da emissão. `token` é o mesmo segredo opaco de 32 bytes base64url de
-- services.gerarTokenAcao e continua globalmente único (como
-- `tokens_acao.token`/`sessoes.refresh_token`): a resolução SEMPRE filtra
-- também por `empresa_id`, então um token da Empresa A usado sob o slug da
-- Empresa B não resolve — nunca cria conta, nunca revela que existe.
--
-- `revogado_em` (nullable) em vez de apagar a linha: o AC exige que a pessoa
-- que abre um link revogado veja o motivo, e apagar colapsaria "revogado" em
-- "inexistente" além de perder a trilha de quem convidou quem.
--
-- NÃO existe coluna `situacao`: ela é DERIVADA na leitura (revogado_em ->
-- revogado; usado_em -> usado; expira_em <= now() -> expirado; senão
-- pendente), então um convite expira sozinho, sem job nenhum.
CREATE TABLE convites_empresa (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  empresa_id UUID NOT NULL REFERENCES empresas(id) ON DELETE CASCADE,
  email VARCHAR(255) NOT NULL,
  token TEXT NOT NULL UNIQUE,
  expira_em TIMESTAMPTZ NOT NULL,
  usado_em TIMESTAMPTZ,
  revogado_em TIMESTAMPTZ,
  criado_por UUID REFERENCES usuarios(id) ON DELETE SET NULL,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- FK consultada em TODA leitura/escrita de convite (a listagem da Empresa e a
-- resolução do token filtram por ela) e alvo de ON DELETE CASCADE — mesma
-- justificativa de idx_usuarios_empresa_id (migration 000032).
CREATE INDEX idx_convites_empresa_empresa_id ON convites_empresa (empresa_id);
