-- Story 7.5: Aprovação/rejeição com revalidação de estoque item a item
-- (Epic 7, Pedidos de Retirada). Acrescenta a auditoria da decisão em
-- `pedidos` e o resultado por item em `pedido_itens` — nenhuma tabela nova
-- (Design Notes de spec-7-5: a pendência não atendida é um NÚMERO no
-- próprio item, nunca um novo Pedido).
--
-- `decidido_por`/`decidido_em`: NULLABLE — ambas ficam NULL enquanto
-- `status = 'pendente'`, preenchidas atomicamente pela MESMA decisão que
-- muda o status (services.DecidirPedido). `decidido_por` SEM
-- `ON DELETE CASCADE`: mesmo padrão de `pedidos.usuario_id` (migração
-- 000026) — trilha de auditoria, nunca some se a conta do decisor for
-- removida por outro caminho no futuro.
ALTER TABLE pedidos ADD COLUMN decidido_por UUID REFERENCES usuarios(id);
ALTER TABLE pedidos ADD COLUMN decidido_em TIMESTAMPTZ;

-- `parcialmente_aprovado`: o único status honesto quando o Almoxarife
-- aprovou mas a disponibilidade real, revalidada no momento da decisão,
-- cobriu só uma parte (ou nenhuma) do solicitado em ao menos um item —
-- nunca reclassificado como 'aprovado' (mentiria) nem 'rejeitado' (o
-- Almoxarife não rejeitou).
ALTER TABLE pedidos DROP CONSTRAINT pedidos_status_check;
ALTER TABLE pedidos ADD CONSTRAINT pedidos_status_check
  CHECK (status IN ('pendente', 'aprovado', 'parcialmente_aprovado', 'rejeitado'));

-- `quantidade_aprovada`: NULL enquanto `pendente` (nenhuma decisão ainda);
-- a partir da decisão, um valor concreto de 0 até `quantidade` — nunca NULL
-- de novo depois de decidido (Always de spec-7-5). O CHECK aceita NULL OU o
-- intervalo válido: não pode simplesmente exigir `>= 0 AND <= quantidade`
-- porque isso rejeitaria o NULL legítimo do estado `pendente`.
ALTER TABLE pedido_itens ADD COLUMN quantidade_aprovada NUMERIC(10, 3)
  CHECK (quantidade_aprovada IS NULL OR (quantidade_aprovada >= 0 AND quantidade_aprovada <= quantidade));
