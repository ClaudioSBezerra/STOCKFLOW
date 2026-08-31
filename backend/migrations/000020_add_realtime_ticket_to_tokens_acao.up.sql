-- Story 4.4: infraestrutura de tempo real (AD-3) — ticket de conexão SSE de
-- curta duração emitido por POST /api/realtime/ticket e consumido uma única
-- vez por GET /api/realtime/stream. Reaproveita `tokens_acao` (mesmo padrão
-- de verificacao_email/redefinicao_senha/mfa_login — Story 1.3/1.6/1.11),
-- só ampliando o CHECK de `tipo`: nenhuma coluna nova, nenhum backfill
-- necessário. Mesmo molde exato da migration 000006 (Story 1.11,
-- acréscimo de 'mfa_login').
ALTER TABLE tokens_acao DROP CONSTRAINT tokens_acao_tipo_check;
ALTER TABLE tokens_acao ADD CONSTRAINT tokens_acao_tipo_check CHECK (tipo IN ('verificacao_email', 'redefinicao_senha', 'mfa_login', 'realtime_ticket'));
