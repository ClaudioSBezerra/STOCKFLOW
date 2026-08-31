-- Story 3.3: importação em massa via planilha padronizada (FR-10).
--
-- importacao_linhas: uma linha por linha de dado da planilha (cabeçalho
-- excluído). `dados` grava o array JSON das 16 células brutas, na mesma
-- ordem do cabeçalho fixo (colNome..colObservacoes em
-- services/importacoes.go) — nunca o arquivo `.xlsx` original, só os valores
-- de célula já parseados pelo excelize.
--
-- `numero_linha` é o número REAL da linha na planilha (cabeçalho = 1,
-- primeira linha de dado = 2) — não um índice sequencial das linhas de
-- dado — para que "parou na linha N" leve direto à célula certa ao reabrir o
-- arquivo original. `UNIQUE(importacao_id, numero_linha)` também é o que
-- torna `ON CONFLICT` desnecessário no INSERT em lote de CriarImportacao: as
-- linhas de uma mesma importação nunca colidem entre si.
--
-- `status`: `pendente` (recém-gravada, ainda não tocada por
-- processarPendentes) -> `processando` (reivindicada por uma transação em
-- andamento — mas esse UPDATE só é visível DENTRO dessa própria transação
-- até ela commitar; sob READ COMMITTED nenhuma outra sessão o enxerga antes
-- disso) -> `criado` (Produto inserido com sucesso) OU `rejeitada`
-- (validação falhou, `erro` explica por quê). Se a transação morrer antes de
-- commitar (processo derrubado, conexão caída), o Postgres desfaz TUDO
-- automaticamente — a linha volta a exibir seu último status COMMITADO
-- (tipicamente `pendente`, nunca um `processando` visível de fora), que uma
-- chamada futura de processarPendentes reivindica de novo via `status IN
-- ('pendente', 'processando')`. `produto_id` só é preenchido no caminho
-- `criado`; SEM `ON DELETE CASCADE` para `produtos` — o Produto criado por
-- uma importação nunca é excluído por esta story.
CREATE TYPE importacao_linha_status AS ENUM ('pendente', 'processando', 'criado', 'rejeitada');

CREATE TABLE importacao_linhas (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  importacao_id UUID NOT NULL REFERENCES importacoes(id) ON DELETE CASCADE,
  numero_linha INTEGER NOT NULL,
  dados JSONB NOT NULL,
  status importacao_linha_status NOT NULL DEFAULT 'pendente',
  produto_id UUID NULL REFERENCES produtos(id),
  erro TEXT NULL,
  UNIQUE (importacao_id, numero_linha)
);

-- processarPendentes reivindica a próxima linha pendente/processando de uma
-- importação (`WHERE importacao_id = $1 AND status IN (...) ORDER BY
-- numero_linha ... LIMIT 1`) e o relatório agregado agrupa por status
-- (`GROUP BY status`) — índice composto direto para as duas consultas, sem
-- sequential scan em `importacao_linhas` à medida que a tabela cresce.
CREATE INDEX idx_importacao_linhas_status ON importacao_linhas (importacao_id, status);
