-- Story 14.1 (FR-37 revisado, AD-35): cada Empresa passa a decidir se exige
-- MFA (TOTP) de `gestor`/`adm` autenticados por senha. Default `false` para
-- toda Empresa, inclusive as existentes — ninguém é bloqueado no deploy. O
-- gate que lê a coluna vive só em middleware.RequireRole.
ALTER TABLE empresas ADD COLUMN mfa_obrigatorio BOOLEAN NOT NULL DEFAULT false;
