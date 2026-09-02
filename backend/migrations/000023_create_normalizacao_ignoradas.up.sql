-- Story 6.2: Aplicação seletiva de correções (Epic 6, Normalização de
-- Dados). normalizacao_ignoradas é a fonte de verdade do estado "ignorado":
-- guarda a tupla EXATA (produto_id, campo, valor, unidade) que o Almoxarife
-- decidiu não aplicar. AnalisarInconsistencias (Story 6.1) passa a excluir
-- da lista qualquer sugestão cuja tupla já esteja aqui.
--
-- A chave é o VALOR exato, não só (produto_id, campo) — se a origem
-- (`dimensoes_pendentes_revisao`/`nome`) mudar depois para um valor
-- DIFERENTE, a tupla nova tem chave diferente e a sugestão reaparece
-- sozinha (AC3 desta story), sem nenhuma lógica extra de expiração.
--
-- `dimensao_unidade` já existe (migration 000011, Story 3.1) — reusado aqui,
-- mesmo enum {mm,cm,m} das 5 dimensões estruturadas de `produtos`.
--
-- `campo` fica CHECK'd contra os 5 nomes fechados (mesmo conjunto de
-- ordemCamposDimensao, services/normalizacao.go) em vez de um enum novo:
-- consistente com o resto do schema, que usa VARCHAR+CHECK para conjuntos
-- fechados pequenos (ex. `movimentacoes.tipo`).
--
-- PRIMARY KEY (produto_id, campo, valor, unidade) É a própria tupla de
-- unicidade — dá o `ON CONFLICT (produto_id,campo,valor,unidade) DO NOTHING`
-- de IgnorarSugestao (idempotente) de graça, sem índice único adicional.
--
-- Sem trilha de auditoria (quem ignorou, Never desta story) — só
-- `criado_em`, e nem esse é exposto por nenhum endpoint ainda.
CREATE TABLE normalizacao_ignoradas (
  produto_id UUID NOT NULL REFERENCES produtos(id),
  campo VARCHAR(20) NOT NULL CHECK (campo IN ('comprimento', 'largura', 'diametro', 'altura', 'espessura')),
  valor NUMERIC(10, 3) NOT NULL,
  unidade dimensao_unidade NOT NULL,
  criado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (produto_id, campo, valor, unidade)
);
