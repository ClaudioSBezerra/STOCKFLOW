-- Story 10.2: Código de Produto automático e sequencial por Empresa
-- (Epic 10, FR-45), spec-10-2.
--
-- `contadores_produto` guarda, por Empresa, o último número de código de
-- Produto já emitido. É a PRIMEIRA tabela do projeto com `empresa_id` como
-- CHAVE PRIMÁRIA (nunca coluna de filtro numa tabela multi-linha) — uma linha
-- por Empresa, nunca mais de uma (Design Notes da spec-10-2).
--
-- `services.CriarProduto` incrementa `ultimo_numero` via `UPDATE ...
-- RETURNING` atômico, na MESMA transação do INSERT em `produtos`, e formata o
-- valor com zero-padding de 6 dígitos (`fmt.Sprintf("%06d", numero)`).
--
-- O backfill abaixo garante que toda Empresa já existente (provisionada antes
-- desta migration) ganhe sua linha de contador com `ultimo_numero=0` — a
-- ausência de linha é tratada por CriarProduto como erro interno, nunca
-- lazy-init.
--
-- NOTA DE COBERTURA (linha "Empresa provisionada antes da migration 000037"
-- da I/O Matrix da spec-10-2): esta garantia é de infraestrutura de
-- migration, não de código Go — o banco de teste sempre aplica as migrations
-- do zero (auth_test.go, `migrateOnce`), então não existe um estado real de
-- "Empresa criada antes desta migration rodar" para simular num teste
-- unitário. A cobertura aqui é por REVISÃO MANUAL do SQL de backfill acima
-- (idempotente por natureza — um `INSERT` simples sem `ON CONFLICT`, mesmo
-- padrão de outras migrations aditivas do projeto, ex. 000032/000035/000036),
-- e transitivamente pelos testes de `services` que dependem de toda Empresa
-- (inclusive `empresaTeste`, criada no primeiro `testDB` de cada pacote) ter
-- uma linha de contador — se o backfill estivesse quebrado, TODOS os testes
-- de CriarProduto falhariam com "contador ausente", não só um caso isolado.
CREATE TABLE contadores_produto (
  empresa_id UUID PRIMARY KEY REFERENCES empresas(id),
  ultimo_numero INTEGER NOT NULL DEFAULT 0
);

INSERT INTO contadores_produto (empresa_id, ultimo_numero)
SELECT e.id, 0 FROM empresas e;
