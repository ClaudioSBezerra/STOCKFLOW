-- Story 5.1: Registrar Baixa (consumo) — abertura do domínio de
-- Movimentação de estoque (Epic 5).
--
-- movimentacoes é a trilha de auditoria de TODA escrita em
-- produto_estoque.quantidade (AD do epic-5-context.md: "não existe caminho
-- de escrita em quantidade sem a Movimentação correspondente"). `tipo` já
-- inclui os 3 valores que as Stories 5.1/5.2 e o futuro ajuste manual vão
-- usar: `baixa` (esta story, `estoque_destino_id` sempre NULL),
-- `transferencia` (Story 5.2, os dois lados preenchidos) e `ajuste`
-- (reservado, nenhuma story atual escreve esse tipo).
--
-- `estoque_origem_id`/`estoque_destino_id` são NULLABLE desde já — schema
-- pensado para não exigir uma migration de alteração na Story 5.2 (Design
-- Notes de spec-5-1): a Story 5.1 só preenche `estoque_origem_id`, nunca
-- `estoque_destino_id`.
--
-- Nenhuma FK usa ON DELETE CASCADE: Movimentação é registro de auditoria e
-- nunca deve desaparecer silenciosamente por causa da exclusão de um
-- Produto/Estoque/Usuário referenciado (nenhum dos três é excluído hoje).
CREATE TABLE movimentacoes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  produto_id UUID NOT NULL REFERENCES produtos(id),
  tipo VARCHAR(20) NOT NULL CHECK (tipo IN ('baixa', 'transferencia', 'ajuste')),
  estoque_origem_id UUID REFERENCES estoques(id),
  estoque_destino_id UUID REFERENCES estoques(id),
  quantidade NUMERIC(10, 3) NOT NULL CHECK (quantidade > 0),
  usuario_id UUID NOT NULL REFERENCES usuarios(id),
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Story 5.3 (Histórico) consulta por Produto e em ordem cronológica — os
-- dois índices que essa listagem vai precisar, criados aqui para não exigir
-- migration extra quando aquela story chegar.
CREATE INDEX idx_movimentacoes_produto_id ON movimentacoes (produto_id);
CREATE INDEX idx_movimentacoes_criado_em ON movimentacoes (criado_em DESC);
