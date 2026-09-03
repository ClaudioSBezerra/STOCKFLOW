-- Story 7.2: Envio de Pedido (Epic 7, Pedidos de Retirada). Formaliza o
-- conteúdo do Carrinho (Story 7.1) como um Pedido de Retirada que o
-- Almoxarife poderá enfileirar e decidir (Stories 7.3-7.5).
--
-- `pedidos`: 1 linha por envio. `usuario_id` é SEMPRE a identidade da
-- sessão que enviou (middleware.UsuarioDaSessao, nunca o corpo da
-- requisição) — a mesma identidade usada em auditoria e em "Meus Pedidos"
-- (Story 7.3), independente do texto livre em `solicitante`. Sem
-- `ON DELETE CASCADE` em `usuario_id`: um Pedido é trilha de auditoria,
-- mesmo padrão de `movimentacoes`/`mesclagens_duplicatas` — nunca some
-- silenciosamente se a conta autora for removida por outro caminho no
-- futuro. `status` fica `pendente` até a decisão do Almoxarife (Story 7.5,
-- fora do escopo desta migration) — `CHECK` fecha o conjunto de valores
-- válidos desde já.
CREATE TABLE pedidos (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  usuario_id UUID NOT NULL REFERENCES usuarios(id),
  solicitante TEXT NOT NULL,
  obra_centro_custo TEXT NOT NULL,
  observacao TEXT,
  status TEXT NOT NULL DEFAULT 'pendente' CHECK (status IN ('pendente', 'aprovado', 'rejeitado')),
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- `pedido_itens`: 1 linha por (Pedido, Produto, Estoque) enviado — SNAPSHOT
-- imutável de `produto_nome`/`categoria_nome`/`estoque_nome`/`quantidade`
-- no momento do envio (mesma decisão arquitetural do futuro recibo em PDF,
-- Story 7.6, epic-7-context.md): consultar `pedido_itens` NUNCA depende de
-- um join ao vivo com `produtos`/`estoques` para exibição futura — o
-- conteúdo do que foi pedido não muda mesmo que o Produto seja editado ou
-- mesclado depois.
--
-- `produto_id UUID NOT NULL REFERENCES produtos(id)` SEM `ON DELETE
-- CASCADE`: `produtos` é soft-delete (Story 6.4), a linha nunca é
-- fisicamente apagada — a FK sempre resolve. `MesclarDuplicatas`
-- (normalizacao.go) reescreve este `produto_id` do produto removido para o
-- sobrevivente antes do soft-delete, com segurança de chave composta
-- (soma-e-descarta na colisão, ver services/normalizacao.go) — o snapshot
-- de nome/categoria/estoque NUNCA é reescrito por essa rotina.
--
-- `estoque_id UUID NOT NULL` SEM NENHUMA FK: mesmo motivo de
-- `carrinho_itens.estoque_id` (migração 000025) — Estoques são
-- hard-deletados (`DELETE FROM estoques`, estoques.go), uma FK apagaria a
-- linha (CASCADE) ou bloquearia a exclusão sem distinção do guard de
-- Pedido pendente abaixo (sem CASCADE). O guard de exclusão de Estoque
-- (`ExcluirEstoque`, completando o segundo guard pendente da Story 2.2)
-- consulta esta tabela por leitura, nunca por constraint.
--
-- `PRIMARY KEY (pedido_id, produto_id, estoque_id)`: no máximo 1 linha por
-- par Produto/Estoque por Pedido — é essa mesma chave composta que exige o
-- cuidado de soma-e-descarta em `MesclarDuplicatas` quando o Pedido já tem
-- uma linha do produto mantido no mesmo Estoque de uma linha do produto
-- removido.
CREATE TABLE pedido_itens (
  pedido_id UUID NOT NULL REFERENCES pedidos(id) ON DELETE CASCADE,
  produto_id UUID NOT NULL REFERENCES produtos(id),
  produto_nome TEXT NOT NULL,
  categoria_nome TEXT NOT NULL,
  estoque_id UUID NOT NULL,
  estoque_nome TEXT NOT NULL,
  quantidade NUMERIC(10, 3) NOT NULL CHECK (quantidade > 0),
  PRIMARY KEY (pedido_id, produto_id, estoque_id)
);

-- Índice em `estoque_id` sozinho: sem ele, o guard de exclusão de Estoque
-- ("este Estoque tem `pedido_itens` de algum Pedido pendente?") faria
-- sequential scan — mesmo motivo de `idx_carrinho_itens_estoque_id`
-- (migração 000025).
CREATE INDEX idx_pedido_itens_estoque_id ON pedido_itens (estoque_id);
