-- Story 18.1 (FR-57, AD-39): toda Empresa real criada a partir de agora pela
-- tela do Dono ganha 14 dias corridos de teste; esgotado o prazo sem
-- pagamento, a Empresa fica bloqueada por completo (Story 18.2). `NULL` =
-- sem prazo (isenta) — o default de toda Empresa já existente e de qualquer
-- caminho de provisionamento fora de CriarEmpresaComTreinamento (ex.
-- cmd/migrar-multi-empresa, que adota a Empresa fundadora): nenhuma delas é
-- afetada por esta migração. Só o Dono da Plataforma altera o valor depois
-- (estender/isentar, Story 18.3).
ALTER TABLE empresas ADD COLUMN trial_termina_em TIMESTAMPTZ NULL;
