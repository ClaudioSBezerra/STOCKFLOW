-- Feedback Ferreira Costa (direção, 2026-10-21): o cadastro direto de conta
-- pelo `adm` (sem convite) deixa o próprio `adm` escolher, por PESSOA, se
-- aquela conta é obrigada a configurar MFA (TOTP) — independente do papel da
-- conta e independente de `empresas.mfa_obrigatorio` (que só vale para
-- gestor+ da Empresa inteira). Default `false` para toda conta existente:
-- ninguém é bloqueado no deploy. O gate que lê esta coluna vive só em
-- middleware.RequireRole, ao lado do gate por Empresa já existente.
ALTER TABLE usuarios ADD COLUMN mfa_obrigatorio_individual BOOLEAN NOT NULL DEFAULT false;
