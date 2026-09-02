-- Story 6.4: Mesclagem de duplicatas com trilha de auditoria (Epic 6,
-- Normalização de Dados). services.MesclarDuplicatas consolida um
-- GrupoDuplicata (Story 6.3) num único Produto sobrevivente: soma a
-- quantidade dos demais no Produto mantido, soft-deleta os removidos
-- (`produtos.deleted_at`, coluna nova por esta migration) e grava a trilha
-- de auditoria abaixo — quem, quando, quais Produtos foram removidos e a
-- quantidade consolidada de cada um.
--
-- `produtos.deleted_at`: NULL = Produto ativo (todo leitura existente de
-- `produtos` passa a filtrar `deleted_at IS NULL`, ver Code Map de
-- spec-6-4); preenchido = mesclado por uma execução de MesclarDuplicatas,
-- nunca mais aparece em busca/catálogo/detalhe/importação por
-- código/nova detecção de duplicatas. A foto do Produto removido permanece
-- em disco sob o id antigo, sem qualquer alteração — auditoria permanente.
ALTER TABLE produtos ADD COLUMN deleted_at TIMESTAMPTZ;

-- `mesclagens_duplicatas`: 1 linha por EXECUÇÃO de mesclagem (1 grupo
-- confirmado pelo Almoxarife) — quem confirmou, quando, e qual Produto foi
-- escolhido para sobreviver. Os Produtos removidos daquela execução ficam em
-- `mesclagem_produtos_removidos` (abaixo), FK para esta tabela.
--
-- Sem `ON DELETE CASCADE` em nenhuma FK — mesmo padrão de `movimentacoes`
-- (Epic 5): a trilha de auditoria é permanente, nunca expurgada por nenhuma
-- rotina de retenção (ao contrário da política de 12 meses aplicada a
-- Movimentações/Pedidos em outras partes do sistema) e nunca deve
-- desaparecer mesmo que o Produto/usuário referenciado seja removido por
-- outro caminho no futuro.
CREATE TABLE mesclagens_duplicatas (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  produto_mantido_id UUID NOT NULL REFERENCES produtos(id),
  usuario_id UUID NOT NULL REFERENCES usuarios(id),
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- `mesclagem_produtos_removidos`: 1 linha por Produto removido DENTRO de uma
-- execução de mesclagem (`mesclagem_id`) — o id do Produto removido e a
-- quantidade total que ele carregava (somada por todos os `estoque_id` em
-- que tinha saldo) no momento da consolidação. PRIMARY KEY composta
-- (mesclagem_id, produto_removido_id) — um mesmo Produto nunca aparece
-- duas vezes na MESMA execução (o serviço valida ids duplicados antes de
-- escrever); índice em `produto_removido_id` sozinho, para consultas
-- futuras de investigação direta no banco ("este Produto já foi removido
-- por alguma mesclagem?").
CREATE TABLE mesclagem_produtos_removidos (
  mesclagem_id UUID NOT NULL REFERENCES mesclagens_duplicatas(id),
  produto_removido_id UUID NOT NULL REFERENCES produtos(id),
  quantidade_consolidada NUMERIC(10, 3) NOT NULL CHECK (quantidade_consolidada >= 0),
  PRIMARY KEY (mesclagem_id, produto_removido_id)
);

CREATE INDEX idx_mesclagem_produtos_removidos_produto_removido_id
  ON mesclagem_produtos_removidos (produto_removido_id);
