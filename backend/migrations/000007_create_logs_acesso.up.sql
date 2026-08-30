-- Story 1.12: log de acesso e auditoria (FR-38/NFR-3).
--
-- logs_acesso: uma linha append-only por tentativa de login concluída (por
-- senha ou por SSO, sucesso ou falha), gravada por
-- services.RegistrarTentativaLogin com um único INSERT não-fatal. Consultada
-- só por `adm` via GET /api/logs-acesso. NÃO há rota/serviço de UPDATE ou
-- DELETE sobre esta tabela — a trilha de auditoria é imutável.
--
-- usuario_id usa ON DELETE SET NULL (e não ON DELETE CASCADE como
-- sessoes/tokens_acao): o log é a evidência de auditoria — apagar registros
-- junto com a conta destruiria justamente o que ele existe para preservar.
-- Na prática o produto nunca faz hard-delete de `usuarios` (só desativação —
-- Story 1.8; anonimização em UPDATE — Story 8.2, que preserva usuario_id),
-- então usuario_id nunca fica NULL por esse caminho; o SET NULL é a rede de
-- segurança correta caso um hard-delete passe a existir.
--
-- email_informado é sempre gravado (NOT NULL), mesmo quando não há conta
-- correspondente: o `adm` precisa ver a tentativa contra um e-mail que não
-- existe. O handler de login NUNCA muda a resposta ao solicitante por causa
-- deste registro — a não-enumeração de e-mail (FR-32) fica intacta.
CREATE TABLE logs_acesso (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  usuario_id UUID REFERENCES usuarios(id) ON DELETE SET NULL,
  email_informado VARCHAR(255) NOT NULL,
  metodo VARCHAR(10) NOT NULL CHECK (metodo IN ('senha', 'sso')),
  sucesso BOOLEAN NOT NULL,
  ip VARCHAR(64) NOT NULL,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- GET /api/logs-acesso ordena sempre por criado_em DESC (e filtra por
-- período sobre a mesma coluna) — o índice descendente serve as duas coisas.
CREATE INDEX idx_logs_acesso_criado_em ON logs_acesso (criado_em DESC);
