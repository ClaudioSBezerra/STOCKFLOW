---
title: 'Story 3.3 — Importação em massa via planilha padronizada'
type: 'feature'
created: '2026-08-30'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '2db0a1571c7ae9a28c1f630bffce3d0263bb4f29'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      Nenhum limite no número de linhas de dado de uma planilha importada —
      só o tamanho do arquivo (10 MiB) é limitado; uma planilha muito
      compacta com dezenas/centenas de milhares de linhas ainda cabe nesse
      limite e roda inteira dentro de uma única requisição síncrona.
    evidence: |-
      services/importacoes.go processa todas as linhas pendentes num laço
      dentro da mesma requisição HTTP (sem SSE/worker, decisão documentada em
      Design Notes); nenhuma checagem de contagem de linhas existe além do
      limite de bytes em handlers/importacoes.go. Achado do Blind Hunter e do
      Edge Case Hunter (review automatizado) na primeira revisão desta story.
    location: >-
      backend/handlers/importacoes.go, backend/services/importacoes.go
    severity: low
  - summary: >-
      Nenhuma defesa contra "decompression bomb" em arquivos `.xlsx`
      enviados — só o tamanho comprimido (10 MiB) é limitado antes do
      excelize materializar todas as células em memória via `GetRows`.
    evidence: |-
      handlers/importacoes.go chama excelize.OpenReader/GetRows diretamente
      sobre o upload, sem checagem de volume descomprimido. Mitigado
      parcialmente por ser endpoint autenticado, restrito a `almoxarife`+
      (não público) e por excelize ser biblioteca estabelecida. Achado do
      Edge Case Hunter (review automatizado) na primeira revisão desta
      story.
    location: backend/handlers/importacoes.go
    severity: low
  - summary: >-
      Uma linha que causa um erro de infraestrutura de forma determinística
      (não um erro de validação) vira uma "poison pill": como o laço de
      `processarPendentes` sempre reivindica a linha `pendente`/`processando`
      de menor `numero_linha` primeiro, essa linha volta a ser a primeira
      reivindicada em toda chamada futura de `continuar`, bloqueando o
      progresso das linhas seguintes indefinidamente.
    evidence: |-
      processarProximaLinha (services/importacoes.go) interrompe o laço
      inteiro e devolve o erro quando a transação de uma linha falha por
      motivo de infraestrutura, sem nenhum contador de tentativas ou desvio
      para pular a linha problemática. Nenhuma AC desta story cobre esse
      cenário. Achado do Blind Hunter (review automatizado) na primeira
      revisão desta story.
    location: backend/services/importacoes.go
    severity: low
  - summary: >-
      `importacao_linhas.produto_id` referencia `produtos(id)` sem nenhuma
      política `ON DELETE` (padrão `NO ACTION`/`RESTRICT`) — uma futura
      capacidade de excluir Produto falharia com violação de FK para
      qualquer Produto criado via importação, a menos que essa story também
      trate isso.
    evidence: |-
      backend/migrations/000016_create_importacao_linhas.up.sql declara
      `produto_id UUID NULL REFERENCES produtos(id)` sem `ON DELETE`.
      Irrelevante hoje: nenhuma story implementada em nenhum épico expõe
      exclusão de Produto (confirmado nas boundaries `Never` das specs 3.1 e
      3.2). Achado do Blind Hunter (review automatizado) na primeira revisão
      desta story.
    location: backend/migrations/000016_create_importacao_linhas.up.sql
    severity: low
---

<intent-contract>

## Intent

**Problem:** O catálogo só pode ser populado item a item (Story 3.1) — sem importação em massa, encerrar uma obra ou consolidar um catálogo legado exige recadastrar cada Produto manualmente (FR-10).

**Approach:** Novo `POST /api/importacoes` (multipart, campo `planilha`, `.xlsx` via `github.com/xuri/excelize/v2`) valida o cabeçalho fixo, grava `importacoes` + uma `importacao_linhas` por linha de dado (status `pendente`) numa transação, e processa cada linha sequencialmente (categoria resolvida por nome, Estoque encontrado-ou-criado, Produto inserido) — tudo em `services/importacoes.go`, reaproveitando `validarDimensao`/constantes de `produtos.go` (mesmo pacote). `GET /api/importacoes/ultima` e `POST /api/importacoes/{id}/continuar` sustentam a retomada após interrupção.

## Boundaries & Constraints

**Always:**
- **Cabeçalho fixo** (16 colunas, nesta ordem exata, comparação com trim mas sem normalizar caixa): `Nome, Código, Categoria, Comprimento (valor), Comprimento (unidade), Largura (valor), Largura (unidade), Diâmetro (valor), Diâmetro (unidade), Altura (valor), Altura (unidade), Espessura (valor), Espessura (unidade), Quantidade, Estoque, Observações`. Primeira linha da primeira planilha do arquivo (`f.GetSheetList()[0]`). Cabeçalho ausente, com colunas a mais/menos, fora de ordem, ou arquivo que não abre no excelize (`OpenReader` erro) → `400 VALIDATION_ERROR` citando as colunas esperadas; **nenhuma linha em `importacoes`/`importacao_linhas` é criada**.
- **Migrations `000015`/`000016`**: `CREATE TYPE importacao_status AS ENUM ('em_andamento','concluida')`; `importacoes(id UUID PK DEFAULT gen_random_uuid(), nome_arquivo VARCHAR(255) NOT NULL, status importacao_status NOT NULL DEFAULT 'em_andamento', total_linhas INTEGER NOT NULL, criado_por UUID NOT NULL REFERENCES usuarios(id), iniciado_em TIMESTAMPTZ NOT NULL DEFAULT now(), concluido_em TIMESTAMPTZ NULL)` + `CREATE INDEX idx_importacoes_iniciado_em ON importacoes (iniciado_em DESC)`. `CREATE TYPE importacao_linha_status AS ENUM ('pendente','processando','criado','rejeitada')`; `importacao_linhas(id UUID PK DEFAULT gen_random_uuid(), importacao_id UUID NOT NULL REFERENCES importacoes(id) ON DELETE CASCADE, numero_linha INTEGER NOT NULL, dados JSONB NOT NULL, status importacao_linha_status NOT NULL DEFAULT 'pendente', produto_id UUID NULL REFERENCES produtos(id), erro TEXT NULL, UNIQUE(importacao_id, numero_linha))` + `CREATE INDEX idx_importacao_linhas_status ON importacao_linhas (importacao_id, status)`. `numero_linha` = número real da linha na planilha (cabeçalho = 1, então primeira linha de dado = 2) — usado nas mensagens "parou na linha N". Linhas 100% em branco (todas as 16 células vazias após trim) são descartadas antes de gravar `importacao_linhas` (não contam em `total_linhas`, não geram gap perceptível ao usuário).
- **`services/importacoes.go`** (novo, mesmo pacote `services` de `produtos.go`/`estoques.go` — reaproveita `validarDimensao`, `unidadesDimensaoValidas`, `limiteNumeric103`/`limiteNumeric103Texto`, `pqForeignKeyViolation`, `pqInvalidTextRepresentation`, `pqUniqueViolation` diretamente, sem exportar nada novo dessas duas): `dados` grava um array JSON de 16 strings brutas (índices 0..15 na mesma ordem do cabeçalho). `CriarImportacao(db *sql.DB, criadoPor, nomeArquivo string, linhas [][]string) (Importacao, RelatorioImportacao, error)`: `linhas[0]` já validado pelo handler (ver abaixo); filtra linhas em branco de `linhas[1:]`, insere `importacoes`+`importacao_linhas` numa transação, comita, e chama `processarPendentes` (privado). `ContinuarImportacao(db, id string) (Importacao, RelatorioImportacao, error)`: `id` inexistente/malformado → `ErrImportacaoNaoEncontrada`; senão chama `processarPendentes`. `ObterUltimaImportacao(db) (*Importacao, RelatorioImportacao, error)`: `SELECT ... ORDER BY iniciado_em DESC LIMIT 1` — nenhuma linha → `(nil, RelatorioImportacao{}, nil)`. Escopo global (não filtra por `criado_por`) — Estoques/Categorias já são recursos compartilhados no resto do sistema, e "reabrir a tela" não pressupõe o mesmo usuário que iniciou.
- **`processarPendentes(db *sql.DB, importacaoID string) (RelatorioImportacao, error)`** (privado): laço — cada iteração abre uma **única transação que cobre a linha inteira**, do reivindicar ao resolver (nunca um `UPDATE` de reivindicação solto fora de tx, que abriria janela de corrida entre duas chamadas concorrentes de `continuar`): `tx.QueryRow("SELECT id, numero_linha, dados FROM importacao_linhas WHERE importacao_id=$1 AND status IN ('pendente','processando') ORDER BY numero_linha FOR UPDATE SKIP LOCKED LIMIT 1")` — o lock dura a transação inteira, então uma segunda chamada concorrente pula (`SKIP LOCKED`) qualquer linha ainda em processamento real; `status IN (...,'processando')` só alcança linhas órfãs de uma transação que já morreu (processo derrubado — Postgres libera o lock com a conexão), nunca uma em andamento. Sem linha → `tx.Rollback()`, sai do laço. Com linha: `tx.Exec("UPDATE importacao_linhas SET status='processando' WHERE id=$1")` (mesma tx, marca já visível a quem consultar `GET /api/importacoes/ultima` nesse meio-tempo); valida os 16 campos (nome/código/observações — mesmas regras de `CriarProduto`; quantidade — mesma checagem de `QuantidadeInicial`; as 5 dimensões via `validarDimensao` reaproveitada tal qual, unidade lowercased antes de checar contra `unidadesDimensaoValidas`); inválido → `tx.Exec("UPDATE importacao_linhas SET status='rejeitada', erro=$1 WHERE id=$2")`, commit, próxima linha. Válido → resolve `categoria_id` (`tx.QueryRow("SELECT id FROM categorias WHERE lower(btrim(nome))=lower(btrim($1))")`; sem linha → mesmo tratamento de rejeitada, citando o valor da planilha, commit) → `encontrarOuCriarEstoque(tx, nomeEstoque)` (ver abaixo) → `INSERT INTO produtos (...)` (mesmas 15 colunas de `CriarProduto`, sem `template_id`) → `INSERT INTO produto_estoque (...)` → `tx.Exec("UPDATE importacao_linhas SET status='criado', produto_id=$1 WHERE id=$2")` → commit; erro de infraestrutura (não de validação) faz rollback dessa tx (linha volta a `pendente`/`processando` visível, reclamável por uma futura chamada) e `processarPendentes` interrompe o laço inteiro, devolvendo o erro. Ao esvaziar o laço sem erro: se não sobra nenhuma linha `pendente`/`processando`, `UPDATE importacoes SET status='concluida', concluido_em=now() WHERE id=$1 AND status='em_andamento'`; devolve `RelatorioImportacao` agregado via `SELECT status, count(*) FROM importacao_linhas WHERE importacao_id=$1 GROUP BY status`.
- **`encontrarOuCriarEstoque(tx *sql.Tx, nome string) (Estoque, error)`** (privado, só em `importacoes.go` — **não** altera `estoques.go`): nome vazio/>255 runes → erro de validação da linha (mesma regra de `ErrEstoqueValidacao`). Senão, uma única query — `WITH ins AS (INSERT INTO estoques (nome) VALUES ($1) ON CONFLICT (nome_normalizado) DO NOTHING RETURNING id, nome) SELECT id, nome FROM ins UNION ALL SELECT id, nome FROM estoques WHERE nome_normalizado = lower(regexp_replace(btrim($1), '\s+', ' ', 'g')) LIMIT 1` (CTE de escrita — Postgres suporta `INSERT ... RETURNING` dentro de `WITH` desde 9.1) — cria se ausente, encontra se já existe, sem janela de corrida (reaproveita a MESMA expressão do `nome_normalizado` gerado, migration `000008`).
- **`handlers/importacoes.go`** (novo): `CriarImportacaoHandler(db)` — `RequireRole(almoxarife)`; `r.ParseMultipartForm(importacaoRequestMaxBytes)` (nova const, `10 << 20`); `r.FormFile("planilha")`; `excelize.OpenReader(arquivo)` (erro → `400` "arquivo não é uma planilha .xlsx válida"); `f.GetRows(f.GetSheetList()[0])`; valida cabeçalho contra a constante fixa; chama `services.CriarImportacao(db, usuario.ID, fileHeader.Filename, linhas)`; sucesso → `201 {"importacao":{"id","status","total_linhas"},"relatorio":{"criados","rejeitados","linhas_rejeitadas":[{"linha","erro"}]}}`. `ContinuarImportacaoHandler(db)` — `RequireRole(almoxarife)`, mesmo formato de resposta com `200`; `ErrImportacaoNaoEncontrada` → `404 NOT_FOUND`. `UltimaImportacaoHandler(db)` — `RequireRole(almoxarife)` (mesma restrição do módulo, não é visualização de catálogo); `200 {"importacao": null}` ou `200 {"importacao":{...},"relatorio":{...}}`.
- **`main.go`**: registra as 3 rotas no bloco de Produtos/Importação, mesmo molde `RequireAuth→RequireRole(almoxarife)`; comentário-doc do pacote cita a Story 3.3.
- **Frontend — `components/ui/tabs.tsx`** (novo, shadcn "new-york" sobre `import { Tabs as TabsPrimitive } from "radix-ui"`, mesmo molde de `select.tsx`).
- **Frontend — `CatalogoPage.tsx`** (editar): quando `podeCadastrar`, troca o empilhamento simples por `Tabs` ("Cadastro"/"Importação") envolvendo `CadastroProdutoSection`/`ImportacaoProdutosSection` — resolve o comentário já presente no arquivo antecipando essas abas.
- **Frontend — `ImportacaoProdutosSection.tsx`** (novo): `<input type="file" accept=".xlsx">` + submit envia `FormData` (campo `planilha`) para `POST /api/importacoes` com header `Authorization` (sem `Content-Type` manual — o browser define o boundary). Sucesso → `toast.success` com contagem de criados/rejeitados + tabela "linha/erro" das rejeitadas. No mount, `GET /api/importacoes/ultima`: se `importacao.status === 'em_andamento'`, banner "Última importação parou na linha N de M. Continuar de onde parou?" com botão que chama `POST /api/importacoes/{id}/continuar`. Sem barra de progresso incremental (processamento é síncrono numa única requisição, sem SSE — mesmo precedente de "chega só na 4.4" das Stories 3.1/3.2) — só um estado de carregamento/desabilitado durante o envio.

**Block If:** nada aqui depende de decisão humana ou de ação de operador fora do repositório. Status final esperado: `done`.

**Never:**
- **Nenhuma atualização por código** (Produto existente com o mesmo `codigo`) — toda linha válida sempre cria um Produto novo, mesmo se o código já existir no catálogo; isso é a Story 3.4.
- **Nenhum campo "atualizados" no relatório**, nenhum CTA "Verificar duplicatas agora" — ambos da Story 3.4 (que estende o mesmo pipeline/tabelas).
- **Nenhum `template_id`** nas linhas importadas — Nomenclatura Guiada (Story 3.2) só no cadastro manual; nome sempre livre na importação.
- **Nenhuma emissão SSE** no canal `produtos` — mesmo precedente das Stories 3.1/3.2.
- **Nenhum armazenamento do arquivo `.xlsx` original** — só os valores de célula já parseados em `importacao_linhas.dados`.
- **Nenhuma alteração em `services/estoques.go` ou `services/produtos.go`** — toda lógica nova mora em `services/importacoes.go`, reaproveitando símbolos privados já existentes desses arquivos (mesmo pacote).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Importação válida completa | `.xlsx` com cabeçalho correto, N linhas de dado válidas | `201`; N Produtos criados, Estoques ausentes criados automaticamente, `importacoes.status='concluida'` | — |
| Cabeçalho fora do padrão | coluna faltando/fora de ordem | `400 VALIDATION_ERROR` citando as colunas esperadas; nada gravado | envelope de erro |
| Linha com dimensão sem unidade | 1 de N linhas com `valor` preenchido e `unidade` vazia | linha marcada `rejeitada` no relatório; as demais N-1 são processadas normalmente | `linhas_rejeitadas: [{"linha","erro"}]` |
| Linha com categoria não encontrada | valor da coluna Categoria sem match em `categorias.nome` | linha `rejeitada` citando o valor; Estoque **não** é criado para essa linha | idem |
| Linha referenciando Estoque novo | coluna Estoque com nome sem linha existente | Estoque criado automaticamente (mesma normalização de nome da Story 2.1), Produto vinculado a ele | — |
| Duas linhas com o mesmo Estoque novo | ambas citam "Depósito B", inexistente | só 1 linha em `estoques`; a segunda linha reaproveita o mesmo `estoque_id` | — |
| Importação retomada | `GET /api/importacoes/ultima` com `status='em_andamento'` | banner de retomada; `POST /.../continuar` processa só as linhas `pendente`/`processando`, sem reprocessar as já `criado`/`rejeitada` | — |
| Papel `usuario` chama qualquer endpoint de importação | sessão `usuario` | `403 FORBIDDEN`; nada gravado | decidido por `RequireRole` |

</intent-contract>

## Code Map

- `backend/go.mod`, `backend/go.sum` (editar) — `go get github.com/xuri/excelize/v2@v2.11.0` (mesma biblioteca que ARCHITECTURE-SPINE.md fixa como "qax-os/excelize v2.11.0" para FR-30; caminho de import atual confirmado via pesquisa web em 2026-08-30).
- `backend/migrations/000015_create_importacoes.{up,down}.sql`, `backend/migrations/000016_create_importacao_linhas.{up,down}.sql` (novos) — ver `Always`. `down`: `DROP TABLE`/`DROP TYPE IF EXISTS` na ordem inversa.
- `backend/services/importacoes.go` (novo) — `Importacao`, `RelatorioImportacao`, `LinhaRejeitada`, `ErroImportacaoValidacao`, `ErrImportacaoNaoEncontrada`, `CriarImportacao`, `ContinuarImportacao`, `ObterUltimaImportacao`, `processarPendentes`, `encontrarOuCriarEstoque`, constantes de índice de coluna (`colNome=0`...`colObservacoes=15`).
- `backend/services/importacoes_test.go` (novo) — Postgres real (`testDB(t)`): importação completa válida; cabeçalho inválido (nada gravado); linha com dimensão incompleta (rejeitada, demais seguem); categoria inexistente; Estoque novo criado uma vez para duas linhas iguais; `ContinuarImportacao` só processa linhas pendentes/processando; `ObterUltimaImportacao` sem nenhuma importação.
- `backend/handlers/importacoes.go` (+ `_test.go`, novos) — `CriarImportacaoHandler`, `ContinuarImportacaoHandler`, `UltimaImportacaoHandler`, `importacaoRequestMaxBytes`; testes via multipart real (`almoxarife` sucesso, `usuario` 403, cabeçalho inválido 400, arquivo não-xlsx 400).
- `backend/main.go` (+ `main_test.go`, editar) — 3 rotas novas atrás de `RequireRole(almoxarife)`; teste de gate de papel (molde de `TestNewMux_ProdutosRotaCarregaRequireRole`).
- `frontend/src/components/ui/tabs.tsx` (novo) — primitivo `Tabs` (shadcn).
- `frontend/src/components/produtos/ImportacaoProdutosSection.tsx` (+ `.test.tsx`, novos) — upload, relatório, banner de retomada.
- `frontend/src/pages/CatalogoPage.tsx` (+ `.test.tsx`, editar) — `Tabs` Cadastro/Importação para `almoxarife`+.

## Tasks & Acceptance

**Execution:**
- `backend/go.mod`/`go.sum` — dependência `xuri/excelize/v2`.
- `backend/migrations/000015-000016_*.{up,down}.sql` — `importacoes`, `importacao_linhas`.
- `backend/services/importacoes.go` (+ teste) — pipeline completo de importação/retomada.
- `backend/handlers/importacoes.go` (+ teste) — 3 endpoints.
- `backend/main.go` (+ teste) — rotas atrás de `RequireRole(almoxarife)`.
- `frontend/src/components/ui/tabs.tsx` — primitivo novo.
- `frontend/src/components/produtos/ImportacaoProdutosSection.tsx` (+ teste) — upload + relatório + retomada.
- `frontend/src/pages/CatalogoPage.tsx` (+ teste) — abas Cadastro/Importação.

**Acceptance Criteria:**
- Given uma planilha `.xlsx` com o cabeçalho padronizado, when o Almoxarife a envia via `POST /api/importacoes`, then cada linha válida cria um Produto, Estoques ausentes referenciados são criados automaticamente, e o relatório final discrimina criados/rejeitados.
- Given uma planilha com cabeçalho fora do padrão, when o Almoxarife tenta importar, then `400 VALIDATION_ERROR` antes de processar qualquer linha, e nenhuma linha é gravada.
- Given uma linha com dimensão sem a unidade correspondente, when a planilha é processada, then só essa linha é marcada como erro no relatório, sem interromper as demais.
- Given uma importação com linhas ainda `pendente`/`processando` (interrompida), when o Almoxarife reabre a tela de Importar, then `GET /api/importacoes/ultima` indica até onde chegou e `POST /.../continuar` retoma sem reprocessar linhas já `criado`/`rejeitada`.
- Given uma sessão de papel `usuario`, when ela chama qualquer endpoint de `/api/importacoes` diretamente pela API, then `403 FORBIDDEN` e nada é gravado.

## Spec Change Log

## Review Triage Log

### 2026-08-31 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 7 (high 0, medium 2, low 5)
- defer: 4
- reject: 6
- addressed_findings:
  - `[medium]` `[patch]` Banner de retomada (`ImportacaoProdutosSection.tsx`) mostrava `criados+rejeitados` como se fosse `numero_linha` — mas `numero_linha` começa em 2 (cabeçalho=1) e tem gaps (linhas em branco descartadas), então a contagem nunca bate com a linha real, contrariando a intenção explícita do Design Notes ("numero_linha... para que 'parou na linha N' leve a pessoa direto à célula certa"). Corrigido expondo a linha pendente mais antiga (`MIN(numero_linha)` entre `pendente`/`processando`) em `GET /api/importacoes/ultima`, e o frontend passa a exibir esse valor.
  - `[medium]` `[patch]` Nenhum teste exercitava a propriedade de segurança de concorrência (`FOR UPDATE SKIP LOCKED` impedindo duas chamadas simultâneas de `continuar` processarem a mesma linha) — achado independentemente pelo Blind Hunter e pelo Verification Gap Reviewer. Adicionado teste com duas goroutines/conexões concorrentes contra a mesma importação, provando que a soma criados+rejeitados bate exatamente com o total de linhas (nenhuma linha processada em duplicidade).
  - `[low]` `[patch]` Parsing de `quantidade`/valor de dimensão fazia `ReplaceAll(",", ".")` ingênuo antes de `ParseFloat` — mascarava silenciosamente o separador de milhar PT-BR (“1.500” virava `1.5`, não `1500`) e `strconv.ParseFloat` aceita `"NaN"`/`"Inf"`/`"Infinity"` sem erro. Removida a heurística de vírgula (parsing agora só aceita ponto decimal, mesma convenção do resto do sistema — API JSON/inputs HTML nunca fazem parsing de localidade) e adicionado guard `math.IsNaN`/`math.IsInf` após o parse.
  - `[low]` `[patch]` `nome_arquivo` (nome do arquivo enviado) não tinha checagem de tamanho antes do `INSERT` em `importacoes.nome_arquivo VARCHAR(255)` — um nome de arquivo maior que 255 runas estourava a coluna e virava `500` genérico em vez de `400`. Adicionada a mesma checagem de 255 runas já usada em `nome`/`código`.
  - `[low]` `[patch]` Upload maior que `importacaoRequestMaxBytes` (falha de `ParseMultipartForm`) caía na mesma mensagem "arquivo não é uma planilha .xlsx válida" de um arquivo genuinamente corrompido — diagnóstico enganoso. Mensagem distinta adicionada para o caso de tamanho excedido.
  - `[low]` `[patch]` Botões "Importar planilha" e "Continuar importação" (`ImportacaoProdutosSection.tsx`) não eram mutuamente exclusivos — nada impedia disparar os dois ao mesmo tempo, competindo pelo mesmo estado de relatório exibido. Ambos os botões agora desabilitam quando QUALQUER uma das duas operações está em andamento.
  - `[low]` `[patch]` Comentários enganosos: (a) cabeçalho de `services/importacoes.go` alegava reaproveitar `pqForeignKeyViolation`/`pqUniqueViolation`, mas nenhum dos dois é referenciado no arquivo (só `pqInvalidTextRepresentation` é usado) — corrigido para não alegar reaproveitamento inexistente. (b) Comentários descrevendo o status `processando` como "já visível a quem consultar `GET /api/importacoes/ultima`" durante a transação estão errados sob Postgres READ COMMITTED (um `UPDATE` não commitado nunca é visível a outra sessão) — corrigidos (em `services/importacoes.go`, no comentário SQL da migration `000016` e neste Design Notes) para descrever corretamente que a recuperação após crash funciona porque a transação não commitada volta para `pendente` (já coberto pela query de reivindicação existente), não porque `processando` persiste ou fica visível no meio do caminho.
  - `defer` (4 itens, ver frontmatter `deferred`): ausência de limite no número de linhas da planilha (só o limite de bytes existe); ausência de defesa contra "decompression bomb" em `.xlsx` (endpoint autenticado, só `almoxarife`+, biblioteca `excelize` estabelecida); uma linha que causa erro de infraestrutura de forma determinística vira "poison pill" (sempre a primeira reivindicada, bloqueando as demais em toda chamada futura de `continuar`) — sem AC cobrindo esse cenário; `importacao_linhas.produto_id` sem política `ON DELETE` contra `produtos(id)` — irrelevante hoje porque nenhuma capacidade de excluir Produto existe em nenhuma story implementada.
  - `reject` (6 itens): "processando inalcançável" como defeito funcional — não é: uma transação não commitada some com o crash e a linha volta a `pendente`, já coberta pela mesma query de reivindicação (só o comentário estava errado, já corrigido acima); violação de FK/unicidade abortando o lote inteiro — não é alcançável: `produtos.codigo` não tem unicidade (decisão desta story) e `categoria_id`/`estoque_id` são resolvidos e validados na mesma transação antes do INSERT; checagem de tamanho de `observações` — a coluna é `TEXT` (sem limite), não `VARCHAR(255)`, premissa do achado estava errada; mais de 16 colunas numa linha — truncamento é comportamento deliberado já documentado no spec; ausência de teste de handler para UUID malformado em `continuar` — o handler mapeia genericamente qualquer `ErrImportacaoNaoEncontrada` para `404`, já provado na camada de serviço, cobertura redundante; alegação do Intent Alignment Auditor de inconsistência entre `status: in-review` e o "Block If" do spec dizendo `done` — é o ciclo de vida normal do workflow automatizado (`in-review` é o status transitório desta própria etapa, vira `done` no Finalize), não uma inconsistência real.

## Design Notes

- **Formato `.xlsx` via `excelize`:** FR-10 não especifica o formato do arquivo, mas ARCHITECTURE-SPINE.md já fixa "qax-os/excelize v2.11.0" como a biblioteca de planilha do stack (hoje publicada em `github.com/xuri/excelize/v2`, mesmo código/organização renomeada — confirmado via pesquisa web em 2026-08-30). Reaproveitar essa única biblioteca do stack para leitura evita introduzir uma segunda dependência de planilha só para importação.
- **Categoria casada por `nome`, não `codigo`:** nenhum documento especifica qual coluna a planilha usa, mas o modelo legado (addendum §F, coleção `produtos`) já guarda `categoria` como o nome descritivo de texto (um dos ~25 valores fixos), não o código curto interno — casar por `nome` (case/trim-insensitive) replica o dado que já existe hoje, em vez de inventar uma convenção nova.
- **Retomada sem worker novo:** o único precedente de processamento assíncrono no código é o poller único de e-mail (`services/email_worker.go`). Em vez de replicar esse padrão, a importação processa tudo sincronamente dentro da própria requisição (o handler não propaga `r.Context()` para o banco — mesmo padrão pré-existente documentado como `deferred` na spec-3-1 — então o processamento continua no servidor mesmo se o cliente desconectar); a tabela `importacao_linhas` com status `pendente`/`processando`/`criado`/`rejeitada` é o que torna isso retomável mesmo num crash do processo, sem goroutine dedicada. **Correção (review pass 2026-08-31):** a recuperação após crash NÃO depende de `processando` "persistir" ou ficar visível a outra sessão em algum meio-tempo — sob READ COMMITTED, um `UPDATE` não commitado é invisível a qualquer transação além da que o executou. O que de fato acontece: se o processo morre/a conexão cai antes do commit final de uma linha, o Postgres desfaz aquela transação inteira, e a linha reverte para seu último status COMMITADO (tipicamente `pendente`) — é esse status `pendente` (já coberto pela mesma query de reivindicação `status IN ('pendente','processando')`) que uma chamada futura de `continuar` reclama; `processando` nunca é observado fora da própria transação que o setou, antes dela commitar.
- **`numero_linha` é a linha real da planilha** (cabeçalho = 1), não um índice sequencial das linhas de dado — para que "parou na linha N" leve a pessoa direto à célula certa ao reabrir o arquivo original.
- **Sem barra de progresso incremental:** o processamento é uma única requisição síncrona (sem SSE, mesmo precedente das Stories 3.1/3.2); um indicador de progresso "ao vivo" exigiria polling ou tempo real, fora do escopo desta story. A UI mostra só um estado de carregamento durante o envio e o relatório final ao concluir.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` — Postgres real; migrations `000015`-`000016` aplicam sem erro; cobre `importacoes_test.go`, `handlers/importacoes_test.go`, `main_test.go`.
- `cd frontend && npm run lint && npm run build && npm run test` — `oxlint`, `tsc`+`vite` e os novos testes de `ImportacaoProdutosSection`/`CatalogoPage` passam.
- `docker compose up --build` — logado como `almoxarife`, importar uma planilha `.xlsx` de exemplo (cabeçalho correto, algumas linhas com erro deliberado) e conferir o relatório; tentar importar com cabeçalho errado → `400`.

**Manual checks (if no CLI):**
- `SELECT status, count(*) FROM importacao_linhas WHERE importacao_id = '<id>' GROUP BY status` bate com o relatório devolvido pela API.
- `SELECT count(*) FROM estoques WHERE nome_normalizado = lower(regexp_replace(btrim('Depósito B'), '\s+',' ','g'))` = 1 após importar duas linhas citando o mesmo Estoque novo.
