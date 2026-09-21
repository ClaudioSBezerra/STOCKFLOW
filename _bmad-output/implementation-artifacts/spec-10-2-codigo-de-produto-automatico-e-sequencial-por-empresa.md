---
title: 'Story 10.2: Código de Produto automático e sequencial por Empresa'
type: 'feature'
created: '2026-09-20'
status: 'done'
baseline_revision: 'c11136bcfa95981ddd69f841007d01206a35e8ac'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Hoje `codigo` de Produto é texto livre opcional digitado pelo Almoxarife no cadastro (`CriarProduto`), sem geração automática — o usuário inventa o valor, com risco de erro de digitação e nenhuma garantia de sequência por Empresa (FR45/epic-10-context.md).

**Approach:** `codigo` deixa de ser um campo de entrada em `CriarProduto`/`POST /api/produtos`/no formulário; passa a ser gerado pelo servidor, sequencial e zero-padded (6 dígitos), a partir de uma tabela dedicada `contadores_produto` (`empresa_id` como chave primária), incrementada via `UPDATE ... RETURNING` atômico na MESMA transação do `INSERT` em `produtos`. A primeira linha do contador de cada Empresa nasce dentro de `ProvisionarEmpresa`, na mesma transação que já copia Categorias/Templates.

## Boundaries & Constraints

**Always:**
- Nova migration `000037` cria `contadores_produto (empresa_id UUID PRIMARY KEY REFERENCES empresas(id), ultimo_numero INTEGER NOT NULL DEFAULT 0)` e faz backfill: uma linha `ultimo_numero=0` para toda Empresa já existente em `empresas` (mesma lógica que garante que Empresas provisionadas antes da migration não fiquem sem contador).
- `ProvisionarEmpresa` (`backend/services/empresas.go:410-419`) passa a inserir a linha inicial do contador (`ultimo_numero=0`) para a Empresa recém-criada, na MESMA `tx` recebida — cobre tanto a Empresa real quanto a Empresa-treino (`empresas_plataforma.go:190` e `:229` chamam `ProvisionarEmpresa` com `tx`s distintas, cada uma ganha sua própria linha de contador).
- `CriarProduto` (`backend/services/produtos.go:201-377`) para de aceitar `codigo` como entrada: gera o próximo número via `UPDATE contadores_produto SET ultimo_numero = ultimo_numero + 1 WHERE empresa_id = $1 RETURNING ultimo_numero`, dentro da `tx` já aberta (linha 299), formata com `fmt.Sprintf("%06d", numero)`, e grava esse valor em `produtos.codigo` no mesmo INSERT — nunca lazy-init do contador (ausência de linha é erro interno, não validação de cliente).
- `Produto` (`backend/services/produtos.go:42-45`), a projeção devolvida por `CriarProduto`/`POST /api/produtos`, ganha o campo `Codigo string` (`json:"codigo"`) para que o cliente saiba o código gerado.
- Códigos manuais/legados já gravados em `produtos.codigo` (cadastro manual anterior a esta story, ou importação em massa via `backend/services/importacoes.go`) permanecem intactos — nenhuma varredura/renumeração retroativa.
- Índice único `idx_produtos_codigo` (`empresa_id, codigo`, migration `000032`) continua valendo tal como está; uma colisão eventual entre um código legado e um futuro código auto-gerado ainda mapeia para `ErroProdutoValidacao` "código já cadastrado" (`produtos.go:345-347`) — comportamento já existente, não precisa de lógica nova de desvio.

**Block If:** Nenhuma decisão bloqueante identificada.

**Never:** Não alterar `backend/services/importacoes.go`/`processarProximaLinha` (INSERT próprio em `produtos.go:548`, fora do escopo — a importação em massa continua usando o `codigo` da planilha, Story 3.3/3.4, e essa convivência com a nova sequência é o comportamento esperado, não um bug); não implementar Filial/`filiais` (menção em `epic-10-context.md:32` é referência futura à Story 12.1 — não existe hoje no schema, e o requisito real desta story, confirmado em `epics.md:1511` ("Story 9.2"), é só a transação de `ProvisionarEmpresa`); não adicionar retry/realocação automática de código em caso de colisão com legado; não tocar Código do Fornecedor/EAN-13 (Story 10.3, campos ainda não existem).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Primeiro Produto de Empresa nova | Empresa provisionada, `contadores_produto.ultimo_numero=0` | `codigo="000001"` | Sem erro |
| Segundo Produto da mesma Empresa | 1 Produto já criado | `codigo="000002"` | Sem erro |
| Duas Empresas distintas | cada uma cadastra seu 1º Produto | ambas recebem `"000001"`, sequências independentes | Sem erro |
| Contador ausente para a Empresa (estado impossível em produção, só defensivo) | linha de `contadores_produto` não existe | Falha, nenhum Produto gravado | 500 INTERNAL_ERROR (erro interno, não input do cliente) |
| Cliente envia `codigo` no payload de `POST /api/produtos` | qualquer valor em `"codigo"` | Campo ignorado — código sempre o gerado pelo servidor | Sem erro (campo simplesmente não é lido) |
| Empresa provisionada antes da migration 000037 | migration roda | Empresa ganha linha de contador com `ultimo_numero=0`, sem afetar `produtos.codigo` já gravados | Sem erro |

</intent-contract>

## Code Map

- `backend/migrations/000037_create_contadores_produto.up.sql` / `.down.sql` (novos) -- criar `contadores_produto` + backfill de uma linha por Empresa existente; `.down.sql` só `DROP TABLE`.
- `backend/services/empresas.go:410-419` (`ProvisionarEmpresa`) -- inserir a linha inicial do contador (`ultimo_numero=0`) para `e.ID`, na mesma `tx`, depois de `CopiarListasPadrao`.
- `backend/services/produtos.go:39-45` (`Produto`) -- acrescentar `Codigo string \`json:"codigo"\``.
- `backend/services/produtos.go:66-90` (`CriarProdutoInput`) -- remover o campo `Codigo string` (linha 79) e o comentário que o descreve, se houver.
- `backend/services/produtos.go:209-214` -- remover a validação de tamanho de `codigo` de entrada (não existe mais entrada).
- `backend/services/produtos.go:290-293` -- trocar o bloco que monta `codigo sql.NullString` a partir do input por uma chamada que gera o próximo número via `UPDATE contadores_produto ... RETURNING` (na `tx` já aberta em `produtos.go:299-303`, então essa geração deve mover para DEPOIS da abertura da transação, antes do INSERT em `produtos`).
- `backend/services/produtos.go:311-334` (`insertProduto`) -- `RETURNING id, nome` -> `RETURNING id, nome, codigo`; `Scan(&p.ID, &p.Nome)` -> `Scan(&p.ID, &p.Nome, &p.Codigo)`; parâmetro `$2` passa a ser o código gerado (string), não mais `codigo sql.NullString` do input.
- `backend/handlers/produtos.go:77-90` (`criarProdutoRequest`) -- remover o campo `Codigo string \`json:"codigo"\`` (linha 79).
- `backend/handlers/produtos.go:122-135` -- remover `Codigo: req.Codigo,` do preenchimento de `services.CriarProdutoInput`.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:119-122` (`ProdutoCriado`) -- acrescentar `codigo: string;`.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:200` -- remover `const [codigo, setCodigo] = useState('')`.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:290` (`limparFormulario`) -- remover `setCodigo('')`.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:328` -- remover `codigo: codigo.trim() === '' ? undefined : codigo.trim(),` do corpo do `POST /api/produtos`.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:496-503` (`<Input id="produto-codigo">`) -- trocar `value={codigo}`/`onChange={...}` por `value={produtoCriado?.codigo ?? ''}`, `disabled` (ou `readOnly`) — campo deixa de ser editável, só exibe o código já gerado depois da criação (AC do epics.md:1517-1519).
- `backend/services/produtos_test.go:506-547` (`TestCriarProduto_CodigoAcimaDe255Caracteres`) -- remover (não há mais entrada de código a validar por tamanho).
- `backend/services/produtos_test.go:549-594` (`TestCriarProduto_CodigoJaCadastrado`) -- remover (colisão de entrada manual não existe mais neste fluxo).
- `backend/services/produtos_test.go:113-161` (`TestCriarProduto_SucessoCompleto`) -- remover `Codigo: "  SKU-1  "` do input; trocar a asserção `if codigo != "SKU-1"` por uma que capture `ultimo_numero` de `contadores_produto` ANTES da chamada e confirme que o `codigo` gravado é `fmt.Sprintf("%06d", numeroAntes+1)` (evita assumir um valor fixo numa Empresa compartilhada entre testes).
- `backend/services/catalogo_test.go:560-568` (seed de `TestObterProdutoDetalhe...`) -- remover `Codigo: "COD-DETALHE-1"`; trocar a asserção em `catalogo_test.go:577` (`*det.Codigo != "COD-DETALHE-1"`) para comparar com o código devolvido por `CriarProduto` (ajustar `criarProdutoCat`, `catalogo_test.go:19-33`, para devolver também o código, não só o `ID`).
- `backend/services/isolamento_test.go:88` -- remover `Codigo: "SKU-ISOLAMENTO"` (não usado em nenhuma asserção do arquivo).
- `backend/services/importacoes_test.go:801,918,1081,1161` (4 seeds de `CriarProduto`) -- remover cada `Codigo: "SKU-..."`; trocar o literal usado depois em `linhaBase(...)` pelo `Codigo` devolvido pela chamada de seed (`produtoExistente.Codigo`), já que a importação casa por código exato e precisa do valor real gerado.
- `backend/handlers/produtos_test.go:838` -- remover `Codigo: "PAR-BUSCA-1"` (busca é por nome, não usa o valor).
- `backend/handlers/produtos_test.go:1885,2009` -- remover `Codigo: "CAB-004"`/`"PU-001"`; trocar o literal passado a `getProdutoPorCodigo(db, ..., "...")` (linhas seguintes, `:1897`/`:2013`) pelo `produto.Codigo` devolvido pelo seed.
- `backend/services/empresas_test.go:147-181` (`TestProvisionarEmpresa_CopiaListasPadrao`) -- referência de estilo para o novo teste de contador (mesmo padrão `criarEmpresaDeTeste` + assert de contagem/valor).

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000037_create_contadores_produto.up.sql` -- criar tabela `contadores_produto (empresa_id UUID PRIMARY KEY REFERENCES empresas(id), ultimo_numero INTEGER NOT NULL DEFAULT 0)` e inserir uma linha `ultimo_numero=0` para cada Empresa já existente em `empresas` -- é o único jeito de Empresas provisionadas antes desta migration ganharem contador sem lazy-init.
- `backend/migrations/000037_create_contadores_produto.down.sql` -- `DROP TABLE contadores_produto`.
- `backend/services/empresas.go` (`ProvisionarEmpresa`, ~410-419) -- inserir a linha inicial do contador (`ultimo_numero=0`) para `e.ID` na mesma `tx`, depois de `CopiarListasPadrao` -- garante que toda Empresa nova (real e treino) nasça com contador, nunca via lazy-init (AD-26).
- `backend/services/produtos.go` -- nova função privada (ex. `proximoCodigoProduto(tx *sql.Tx, empresaID string) (string, error)`) que roda `UPDATE contadores_produto SET ultimo_numero = ultimo_numero + 1 WHERE empresa_id = $1 RETURNING ultimo_numero` e formata `fmt.Sprintf("%06d", numero)`; se `sql.ErrNoRows` (nenhuma linha afetada), devolve erro interno (`fmt.Errorf`, não `ErroProdutoValidacao` -- é falha de infraestrutura, não input inválido) -- AC1/AC2.
- `backend/services/produtos.go` (`CriarProduto`) -- remover a validação/leitura de `input.Codigo`; chamar `proximoCodigoProduto(tx, empresaID)` logo após abrir `tx` (linha ~303) e usar o valor retornado no INSERT (`$2`), com `RETURNING id, nome, codigo` -- AC1/AC2/AC3.
- `backend/services/produtos.go` (`Produto` struct) -- acrescentar `Codigo string` -- necessário para o handler devolver o código gerado ao cliente.
- `backend/handlers/produtos.go` (`criarProdutoRequest`, `CriarProdutoHandler`) -- remover o campo `Codigo`/seu repasse -- o código deixou de ser entrada de cliente.
- `backend/services/produtos_test.go` -- remover os dois testes de validação de `codigo` de entrada (255 chars, duplicidade manual); ajustar `TestCriarProduto_SucessoCompleto` para não enviar `Codigo` e para validar o código gerado dinamicamente (contra o valor de `ultimo_numero` lido antes da chamada); acrescentar `TestCriarProduto_CodigoSequencialPorEmpresa` (duas chamadas seguidas na mesma Empresa nova -> `"000001"` depois `"000002"`) e `TestCriarProduto_CodigoIndependentePorEmpresa` (duas Empresas distintas, cada uma cadastra 1 Produto -> ambas `"000001"`) -- usar `criarEmpresaDeTeste` (mesmo padrão de `empresas_test.go:147-181`) para isolar o contador do compartilhado por `empresaTeste` -- cobre a I/O Matrix.
- `backend/services/empresas_test.go` -- acrescentar `TestProvisionarEmpresa_CriaContadorDeCodigo`, mesmo molde de `TestProvisionarEmpresa_CopiaListasPadrao` (linha 147): após `criarEmpresaDeTeste`, `SELECT ultimo_numero FROM contadores_produto WHERE empresa_id = $1` deve devolver exatamente uma linha com `0` -- prova AD-26 (contador nasce na transação de provisionamento, nunca lazy-init).
- `backend/services/catalogo_test.go`, `backend/services/isolamento_test.go`, `backend/services/importacoes_test.go`, `backend/handlers/produtos_test.go` -- remover cada `Codigo: "..."` de `CriarProdutoInput{}` listado no Code Map; nos 4 casos de `importacoes_test.go` e nos 2 de `handlers/produtos_test.go` (linhas 1885/2009) que reusam o literal depois (respectivamente em `linhaBase(...)` e em `getProdutoPorCodigo(...)`), substituir o literal pelo `Codigo` devolvido pela chamada de seed -- mantém a suíte compilando e testando o comportamento real (match por código exato, agora auto-gerado).
- `frontend/src/components/produtos/CadastroProdutoSection.tsx` -- remover o estado `codigo`/seu uso em `limparFormulario` e no corpo do POST; `ProdutoCriado` ganha `codigo: string`; o `<Input id="produto-codigo">` passa a `disabled`, com `value={produtoCriado?.codigo ?? ''}`, sem `onChange` -- implementa a AC "campo não é mais editável... só exibido, já preenchido, depois da criação" (epics.md:1517-1519).
- `frontend/src/components/produtos/CadastroProdutoSection.test.tsx` -- acrescentar um teste que, após submissão bem-sucedida (mock de `POST /api/produtos` devolvendo `{ produto: { id, nome, codigo: '000007' } }`), confirma que o input `produto-codigo` fica desabilitado e mostra `'000007'`, e que antes da submissão ele está vazio e desabilitado.

**Acceptance Criteria:**
- Given uma Empresa recém-provisionada sem nenhum Produto, when o primeiro Produto é cadastrado, then o código gravado é `"000001"`.
- Given dois cadastros sequenciais de Produto na mesma Empresa, when ambos completam, then os códigos são consecutivos (`N` e `N+1`), nunca repetidos.
- Given duas Empresas diferentes, when cada uma cadastra seu primeiro Produto, then ambas recebem `"000001"` — a sequência nunca é compartilhada entre Empresas.
- Given um payload de `POST /api/produtos` que inclui `"codigo"` com um valor arbitrário, when o Produto é criado, then o valor enviado é ignorado e o código gravado é o gerado pelo servidor.
- Given Produtos já cadastrados antes desta story com códigos manuais livres, when a nova sequência automática passa a valer, then esses códigos antigos permanecem inalterados no banco.

## Design Notes

> **Atualização (2026-09-21, sugestão do usuário + code review):** o parágrafo abaixo sobre "não varrer o maior código legado" foi SUPERADO. `proximoCodigoProduto` passou a calcular `GREATEST(contador, maior código puramente numérico da Empresa) + 1` no mesmo `UPDATE` que trava o contador (AD-26 refinada); colisão com importação concorrente é refeita em até 3 tentativas. A seção "Never" acima ("não adicionar retry/realocação") vale só para a colisão com LEGADO, que agora nem ocorre.

Uma colisão entre um código legado numérico de 6 dígitos (ex. um Produto importado com `codigo="000001"` antes desta story) e um futuro código auto-gerado é teoricamente possível, mas não tratada por lógica nova: o índice único existente (`idx_produtos_codigo`) já rejeita a segunda gravação com `ErroProdutoValidacao` "código já cadastrado" (`produtos.go:345-347`). O epics.md (Story 10.2, AC "códigos antigos permanecem intactos, convivendo com a nova sequência") aceita essa convivência sem exigir prevenção de colisão — não é escopo desta story inventar uma varredura do maior código legado para "adiantar" o contador.

`contadores_produto` é a primeira tabela do projeto com `empresa_id` como CHAVE PRIMÁRIA (não coluna de filtro em tabela multi-linha) — todas as demais tabelas de domínio usam `id UUID PRIMARY KEY` + `empresa_id` como FK de filtro. Essa exceção é intencional (uma linha por Empresa, nunca mais de uma) e já esperada pelo `epic-10-context.md:33`.

**Nota de cobertura — linha "Empresa provisionada antes da migration 000037" da I/O Matrix:** não há teste automatizado dedicado a essa linha. O banco de teste sempre aplica todas as migrations do zero (`auth_test.go`, `migrateOnce`), então não existe, dentro de um teste Go, um estado real de "Empresa que já existia quando a migration 000037 rodou" para simular — é uma garantia de infraestrutura de migration, não de código Go. A cobertura é por revisão manual do backfill (`backend/migrations/000037_create_contadores_produto.up.sql`: `INSERT INTO contadores_produto SELECT e.id, 0 FROM empresas e`, idempotente por natureza, mesmo padrão aditivo de 000032/000035/000036) e, transitivamente, pelo fato de que TODA a suíte de testes de `CriarProduto` depende de `empresaTeste` (criada antes de qualquer teste rodar) ter uma linha de contador — se o backfill estivesse quebrado, a suíte inteira falharia com "contador ausente", não um caso isolado. Ver o comentário equivalente na própria migration.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -p 1 ./services/... ./handlers/...` -- expected: todos os testes passam, incluindo os novos de sequência/independência por Empresa e o de contador criado por `ProvisionarEmpresa`.
- `cd frontend && npx vitest run CadastroProdutoSection` -- expected: suíte passa, incluindo o novo teste do campo código desabilitado/preenchido pós-criação.
- `cd frontend && npm run build` (roda `tsc -b`) -- expected: sem erros de tipo após remover o estado `codigo` e ajustar `ProdutoCriado`.

## Auto Run Result

O `bmad-loop` (run `20260920-000954-37f2`) parou com a story em `in-review`
no frontmatter, mas sem evidência de um segundo agente de review ter de fato
rodado sobre o diff. Recuperação manual (Claude, 2026-09-20):

- `go build ./... && go vet ./...`: sem erros.
- `go test -count=1 -p 1 ./services/... ./handlers/...` (Postgres real, cache descartado explicitamente para confirmar execução real): `ok` em `services` (170s) e `handlers` (174s).
- `go test ./cmd/...`: `ok` nos 4 pacotes (migrar-multi-empresa, migrate-legado, seed-admin, seed-dono-plataforma).
- `npx vitest run CadastroProdutoSection`: 27/27 passam.
- `npm run build`: sem erros de tipo, build de produção completo.
- Spot-check manual contra o `intent-contract`: `proximoCodigoProduto` roda `UPDATE ... RETURNING` dentro da mesma `tx` do INSERT em `produtos`, zero-padding de 6 dígitos, `sql.ErrNoRows` vira erro interno (nunca lazy-init); `ProvisionarEmpresa` insere a linha do contador na mesma transação, depois de `CopiarListasPadrao`; migration 000037 faz backfill simples (`INSERT ... SELECT`) para toda Empresa já existente; `codigo` removido de `CriarProdutoInput`/`criarProdutoRequest`, campo do frontend virou somente-leitura pós-criação — todos conferem com a spec.
- **Honestidade:** substitui, mas não equivale a, a fase de review do `bmad-loop` — nenhum segundo agente adversarial rodou sobre este diff. Recomendo `bmad-code-review` nesta e na Story 10.1 antes do deploy.

### Review Findings

Code review independente (2026-09-21, 4 revisores: Blind Hunter, Edge Case Hunter, Verification Gap, Acceptance Auditor; achados verificados no código antes de classificar).

- [x] [Review][Patch][aplicado 2026-09-21] Empresa fundadora adotada por `AdotarEmpresaFundadora` nasce SEM linha em `contadores_produto` (só `ProvisionarEmpresa` a cria) — `CriarProduto` falharia com 500 "contador ausente" em instalação nova/nova adoção [backend/services/migracao_multi_empresa.go:235, backend/services/empresas.go:417-425] — semear o contador dentro de `InserirEmpresa` (ou no caminho de adoção) e cobrir com teste. Produção NÃO é afetada: a Ferreira Costa foi adotada antes da 000037, que faz backfill.
- [x] [Review][Patch][aplicado 2026-09-21] Colisão de código com importação concorrente (a importação não trava o contador e o MAX não enxerga INSERT em voo) vira 400 "código já cadastrado" num campo que o usuário não controla [backend/services/produtos.go: CriarProduto, ramo 23505] — repetir a transação algumas vezes ou responder erro reexecutável
- [x] [Review][Patch][aplicado 2026-09-21] Campo Código somente-leitura continua mostrando o código do Produto anterior enquanto o usuário preenche o próximo (`produtoCriado` nunca é zerado ao editar) [frontend/src/components/produtos/CadastroProdutoSection.tsx:534]
- [x] [Review][Patch][aplicado 2026-09-21] Testes ausentes do gerador de código: escopo por Empresa (Empresa B com 000500 não pode empurrar a A) e código legado numérico com 10+ dígitos [backend/services/produtos_test.go]
- [x] [Review][Patch][aplicado 2026-09-21] Spec/comentários desatualizados após o refinamento `ff82e0e` (Design Notes ainda dizem que não se varre o maior código legado; comentário do ramo 23505 e doc de `proximoCodigoProduto`) e comentário de 000037 diz "idempotente" para INSERT sem ON CONFLICT [spec-10-2, backend/services/produtos.go, backend/migrations/000037_*]
- [x] [Review][Defer] O MAX() por INSERT varre os Produtos da Empresa (sem índice de expressão) segurando o lock do contador — ~milhares de linhas, custo desprezível hoje — deferred
