-- Story 1.11: MFA obrigatório para papéis administrativos (FR-37/SM-2).
--
-- usuarios.mfa_habilitado/mfa_secret: estado de MFA por conta — mesma
-- filosofia de usuarios.ativo/bloqueado_ate (Story 1.8/1.10), sem tabela
-- nova. mfa_secret fica em texto puro: mesma decisão/justificativa já
-- registrada para sessoes.refresh_token/tokens_acao.token (nenhuma AD exige
-- hash/cifra, e a proteção é a mesma superfície de banco).
ALTER TABLE usuarios ADD COLUMN mfa_habilitado BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE usuarios ADD COLUMN mfa_secret TEXT;

-- usuarios.mfa_ultimo_passo_usado: índice do passo TOTP (RFC 6238 §5.2, o
-- contador HOTP derivado do relógio, ~30s cada) já consumido por esta conta
-- — grava-se atomicamente no sucesso de ConcluirLoginMFA/
-- ConfirmarConfiguracaoMFA, e um novo sucesso com o MESMO passo é recusado
-- como reuso de código (mesmo vocabulário de código inválido). NULL até o
-- primeiro sucesso.
ALTER TABLE usuarios ADD COLUMN mfa_ultimo_passo_usado BIGINT;

-- sessoes.origem: proveniência de CADA sessão ('senha' ou 'sso') — é a única
-- forma de o middleware.RequireRole saber, a cada requisição, se deve ou não
-- exigir MFA daquele usuário (o access JWT nunca consulta `sessoes`, então o
-- claim `origem` do token é a via real; a coluna aqui é o registro
-- persistido da mesma decisão, consultado só em /login e /refresh).
-- DEFAULT 'senha' é só para satisfazer linhas hipotéticas pré-existentes
-- (não há nenhuma: sessoes é populada só por login/SSO já em produção) sem
-- exigir um valor explícito de um backfill.
ALTER TABLE sessoes ADD COLUMN origem VARCHAR(10) NOT NULL DEFAULT 'senha' CHECK (origem IN ('senha', 'sso'));

-- tokens_acao ganha o tipo 'mfa_login': token opaco de uso único emitido por
-- LoginHandler quando mfa_habilitado=true, trocado por sessão em
-- POST /api/auth/mfa/verificar — mesmo padrão de verificacao_email/
-- redefinicao_senha (Story 1.3/1.6).
ALTER TABLE tokens_acao DROP CONSTRAINT tokens_acao_tipo_check;
ALTER TABLE tokens_acao ADD CONSTRAINT tokens_acao_tipo_check CHECK (tipo IN ('verificacao_email', 'redefinicao_senha', 'mfa_login'));

-- Qualquer sessão já em voo no momento deste deploy foi emitida ANTES de
-- `sessoes.origem` existir de verdade — o DEFAULT 'senha' acima é só para
-- satisfazer o NOT NULL da coluna nova, não uma afirmação de que a sessão
-- veio de login por senha. Uma sessão SSO nesse estado rotacionaria
-- (RenovarSessao) carregando `origem='senha'` indefinidamente, violando o
-- invariante "SSO nunca é gated por MFA". Revoga tudo que ainda está
-- pendente de refresh: o cliente cai para /login e a próxima sessão já nasce
-- com `origem` correta (gravada por EmitirSessao a partir do caminho real de
-- autenticação).
UPDATE sessoes SET revogado_em = now() WHERE revogado_em IS NULL;
