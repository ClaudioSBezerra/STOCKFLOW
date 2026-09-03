-- Story 7.1: Carrinho de reserva (Epic 7, Pedidos de Retirada).
-- `carrinho_itens` acumula Produto + Estoque + quantidade por Usuário ANTES
-- de um Pedido de Retirada existir (Stories 7.2+) — nenhum vínculo com
-- Pedido nesta story, só o carrinho isolado.
--
-- `usuario_id ... ON DELETE CASCADE`: o carrinho é estado transitório e
-- pessoal — se a conta some, o carrinho some junto, sem trilha de
-- auditoria a preservar (ao contrário de `movimentacoes`/mesclagem).
--
-- `produto_id REFERENCES produtos(id)` SEM CASCADE: um item de carrinho cujo
-- Produto foi mesclado (soft-delete, Story 6.4, `produtos.deleted_at`)
-- precisa continuar existindo até o próximo `GET /carrinho` limpar a linha e
-- avisar o motivo (spec-7-1, Always) — CASCADE apagaria a linha em silêncio,
-- sem chance de avisar o Usuário.
--
-- `estoque_id UUID NOT NULL` SEM NENHUMA FK (Design Notes de spec-7-1):
-- Estoques são hard-deleted (`DELETE FROM estoques`, estoques.go) sem
-- soft-delete. Uma FK com CASCADE apagaria a linha antes do aviso (mesmo
-- problema do `produto_id` acima); uma FK sem CASCADE bloquearia a exclusão
-- do Estoque, contradizendo a Story 2.2 (que só bloqueia por saldo residual
-- ou Pedido pendente). A existência do Estoque é checada por leitura em
-- `ListarCarrinho`, nunca por constraint.
--
-- `PRIMARY KEY (usuario_id, produto_id, estoque_id)`: no máximo 1 linha por
-- par Produto/Estoque por Usuário — `AdicionarItemCarrinho` faz upsert
-- incrementando a quantidade (`ON CONFLICT ... DO UPDATE`) em vez de duas
-- linhas para o mesmo par.
CREATE TABLE carrinho_itens (
  usuario_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  produto_id UUID NOT NULL REFERENCES produtos(id),
  estoque_id UUID NOT NULL,
  quantidade NUMERIC(10, 3) NOT NULL CHECK (quantidade > 0),
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
  atualizado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (usuario_id, produto_id, estoque_id)
);

-- Índice em `estoque_id` sozinho: sem ele, a limpeza preguiçosa de
-- `ListarCarrinho` (checar se cada `estoque_id` referenciado ainda existe em
-- `estoques`) e qualquer consulta futura "quem tem este Estoque no
-- carrinho?" (ex.: guard de exclusão de Estoque de uma story futura)
-- fariam sequential scan.
CREATE INDEX idx_carrinho_itens_estoque_id ON carrinho_itens (estoque_id);
