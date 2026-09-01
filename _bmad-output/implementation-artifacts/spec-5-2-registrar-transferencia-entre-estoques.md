---
title: 'Story 5.2 — Registrar Transferência entre Estoques'
type: 'feature'
created: '2026-09-01'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: '6be6d9e27938be1226cd1fe3a879550d07ca4a5f'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-5-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      RegistrarTransferencia/RegistrarBaixa não propagam context.Context para a
      transação; uma desconexão do cliente no meio deixa a transação rodando
      segurando os locks de linha de produto_estoque.
    evidence: |-
      backend/services/movimentacoes.go usa db.Begin() + tx.QueryRow/tx.Exec sem
      variante *Context. Padrão pré-existente de toda a camada de serviço (Story
      5.1 inclusive) — não introduzido por esta story; corrigir exige mudar a
      convenção de assinatura em várias funções.
    location: >-
      backend/services/movimentacoes.go
    severity: low
  - summary: >-
      A lista de Estoques destino do diálogo Transferir é buscada uma vez por
      instância do componente e nunca recarregada (nem ao reabrir o diálogo, nem
      em reconexão SSE) — um Estoque criado/renomeado noutra aba fica ausente da
      opção até sair da tela.
    evidence: |-
      frontend/src/pages/ProdutoDetalhePage.tsx: carregarEstoquesDestino só roda
      quando estoquesDestino === null. Janela de defasagem pequena e o servidor
      continua a autoridade (rejeita origem==destino / destino inválido).
    location: >-
      frontend/src/pages/ProdutoDetalhePage.tsx
    severity: low
  - summary: >-
      travarLinhaProdutoEstoque emite um UPDATE no-op (ON CONFLICT DO UPDATE SET
      quantidade = produto_estoque.quantidade) mesmo quando a linha já existe,
      gerando uma tupla morta por trava.
    evidence: |-
      Custo de MVCC menor numa tabela de baixo volume (uma linha por par
      produto/estoque) e sem triggers em produto_estoque. Um fast-path
      SELECT ... FOR UPDATE com fallback para o upsert evitaria a escrita
      mantendo a garantia de "linha de destino pode não existir".
    location: >-
      backend/services/movimentacoes.go
    severity: low
  - summary: >-
      quantidade NaN/±Inf passa pelas guardas <=0 e >limiteNumeric103 em
      RegistrarTransferencia/RegistrarBaixa.
    evidence: |-
      Inalcançável pelo único chamador real (o handler HTTP): encoding/json
      rejeita literais NaN/Inf com erro de decode -> 400. Só afeta chamadas
      diretas ao serviço; seria um ajuste de consistência nas duas funções.
    location: >-
      backend/services/movimentacoes.go
    severity: low
  - summary: >-
      Se carregarDetalhe() falhar logo após uma transferência/baixa bem-sucedida,
      o catch chama setErro* num diálogo já fechado enquanto o toast de sucesso
      já apareceu — o usuário vê sucesso e números defasados.
    evidence: |-
      Padrão herdado verbatim de confirmarBaixa (Story 5.1):
      toast.success -> setXEstoque(null) -> await carregarDetalhe() -> catch
      setErroX(...). Deveria ser corrigido nos dois fluxos de forma consistente.
    location: >-
      frontend/src/pages/ProdutoDetalhePage.tsx
    severity: low
  - summary: >-
      Os inputs de quantidade dos diálogos de Baixa e Transferir (type="number")
      não têm min/step, permitindo digitar/rolar valores zero ou negativos antes
      de qualquer round-trip ao servidor.
    evidence: |-
      frontend/src/pages/ProdutoDetalhePage.tsx: <Input type="number"
      inputMode="decimal"> em ambos os diálogos, sem atributo min. Padrão
      herdado verbatim de confirmarBaixa (Story 5.1) e replicado em
      confirmarTransferencia (Story 5.2); o servidor já rejeita com 400, mas o
      feedback só chega depois do POST.
    location: >-
      frontend/src/pages/ProdutoDetalhePage.tsx
    severity: low
  - summary: >-
      GET /api/estoques devolve a lista completa sem filtro nem paginação, tanto
      no Select de Estoque destino do diálogo Transferir quanto no cadastro de
      Produto — não escala bem para um catálogo de Estoques grande.
    evidence: |-
      frontend/src/pages/ProdutoDetalhePage.tsx (carregarEstoquesDestino) e
      CadastroProdutoSection.tsx:269 usam o mesmo GET /api/estoques sem
      paginação. Padrão pré-existente reaproveitado tal qual por esta story,
      não introduzido por ela.
    location: >-
      frontend/src/pages/ProdutoDetalhePage.tsx
    severity: low
  - summary: >-
      A guarda anti-duplo-submit (enviandoBaixa/enviandoTransferencia) é lida e
      setada dentro da própria função assíncrona, não sincronamente no
      disparo — dois gatilhos quase simultâneos (Enter + clique) podem passar
      pela guarda antes do primeiro setState commitar.
    evidence: |-
      frontend/src/pages/ProdutoDetalhePage.tsx: confirmarBaixa (Story 5.1) e
      confirmarTransferencia (Story 5.2) checam `enviando... ` como primeira
      linha da função async, mas só chamam `setEnviando...(true)` depois —
      mesma janela de corrida nos dois fluxos. Padrão herdado verbatim de
      Story 5.1, não introduzido por esta story.
    location: >-
      frontend/src/pages/ProdutoDetalhePage.tsx
    severity: low
---

<intent-contract>

## Intent

**Problem:** Não existe forma de mover uma quantidade de um Produto de um Estoque para outro sem passar por uma Baixa manual seguida de um cadastro/reentrada — perde o rastro de que se tratou de uma transferência, e não há garantia atômica entre o débito na origem e o crédito no destino.

**Approach:** `services.RegistrarTransferencia(db, produtoID, estoqueOrigemID, estoqueDestinoID, usuarioID, quantidade)` — mesmo molde transacional de `RegistrarBaixa` (Story 5.1), mas travando DUAS linhas de `produto_estoque` (origem e destino) na ordem canônica ascendente de `estoque_id` (AD-10), débito+crédito atômicos, Movimentação `tipo='transferencia'` com origem E destino preenchidos. `handlers.RegistrarTransferenciaHandler` expõe `POST /api/produtos/{id}/estoques/{estoqueId}/transferencia` atrás do mesmo gate `RequireAuth → RequireRole(almoxarife)`, publica no canal SSE `movimentacoes`. No frontend, cada linha de "Quantidade por Estoque" em `ProdutoDetalhePage` ganha um segundo botão "Transferir" ao lado de "Registrar Baixa", abrindo um diálogo com um `Select` de Estoque destino (via `GET /api/estoques`) e um input de quantidade. Nenhuma migration nova: a tabela `movimentacoes` já foi desenhada na Story 5.1 com `estoque_destino_id` nullable e `tipo IN ('baixa','transferencia','ajuste')` justamente para esta story.

## Boundaries & Constraints

**Always:**
- **`backend/services/movimentacoes.go`** (estende o arquivo da Story 5.1, nenhum arquivo novo): `func RegistrarTransferencia(db *sql.DB, produtoID, estoqueOrigemID, estoqueDestinoID, usuarioID string, quantidade float64) (Movimentacao, error)`. Validação ANTES de `tx.Begin()`, nenhuma escrita: `quantidade <= 0` → `&ErroMovimentacaoValidacao{"quantidade deve ser maior que zero"}` (mesmo texto de `RegistrarBaixa` — carregado por analogia direta: a coluna `movimentacoes.quantidade` tem `CHECK (quantidade > 0)`, sem esta guarda um valor inválido vaza como 500 de constraint violation em vez de 400); `quantidade > limiteNumeric103` → mesmo erro/texto de `RegistrarBaixa` (mesma razão, coluna `NUMERIC(10,3)`); `estoqueOrigemID == estoqueDestinoID` (comparação de string pura, sem tocar o banco) → `&ErroMovimentacaoValidacao{"estoque de origem e destino devem ser diferentes"}`.
- **Trava em ordem canônica (AD-10):** função nova não-exportada `travarLinhaProdutoEstoque(tx *sql.Tx, produtoID, estoqueID string) (float64, error)` — `INSERT INTO produto_estoque (produto_id, estoque_id, quantidade) VALUES ($1, $2, 0) ON CONFLICT (produto_id, estoque_id) DO UPDATE SET quantidade = produto_estoque.quantidade RETURNING quantidade`. Usa `INSERT ... ON CONFLICT DO UPDATE`, NUNCA `SELECT ... FOR UPDATE` puro, porque a linha do Estoque DESTINO pode não existir ainda (Produto nunca esteve lá) — `SELECT ... FOR UPDATE` não trava nada quando não há linha; o `DO UPDATE` (no-op lógico) adquire o MESMO lock de linha que `FOR UPDATE` adquiriria se a linha já existisse, e cria a linha com `quantidade=0` quando ausente. `RegistrarTransferencia` ordena `primeiro, segundo := estoqueOrigemID, estoqueDestinoID; if segundo < primeiro { primeiro, segundo = segundo, primeiro }` (comparação de string simples — `produto_id` é igual nos dois pares, então ordenar os pares reduz a ordenar por `estoque_id`) e chama `travarLinhaProdutoEstoque` para `primeiro` DEPOIS `segundo`, nunca na ordem origem/destino declarada pelo chamador. Erro de `travarLinhaProdutoEstoque` (SQLSTATE `22P02` produtoID/estoqueID malformado, OU `23503` `pqForeignKeyViolation` já existente em `produtos.go:16` — destino referenciando um Estoque inexistente) retorna `&ErroQuantidadeIndisponivel{Disponivel: 0}` — mesmo colapso "malformado/inexistente → 0 disponível" da Story 5.1 (Design Notes de spec-5-1), aplicado agora também ao lado destino: nenhuma AC desta story pede um código de erro dedicado para Estoque destino inválido.
- **Checagem + escrita:** depois de travar as duas linhas, identifica qual saldo (`saldoPrimeiro`/`saldoSegundo`) é o da ORIGEM; `quantidade > disponivelOrigem` → `&ErroQuantidadeIndisponivel{Disponivel: disponivelOrigem}`, `tx.Rollback()` via `defer` (nenhuma linha fantasma de destino sobrevive: se `travarLinhaProdutoEstoque` criou a linha do destino com `quantidade=0` e a transação não commita, a criação desfaz junto). Senão: `UPDATE produto_estoque SET quantidade = quantidade - $1 WHERE produto_id=$2 AND estoque_id=$3` (origem), `UPDATE produto_estoque SET quantidade = quantidade + $1 WHERE produto_id=$2 AND estoque_id=$3` (destino), `INSERT INTO movimentacoes (produto_id, tipo, estoque_origem_id, estoque_destino_id, quantidade, usuario_id) VALUES ($1,'transferencia',$2,$3,$4,$5) RETURNING id, produto_id, tipo, estoque_origem_id, estoque_destino_id, quantidade, usuario_id, criado_em` na MESMA transação, `tx.Commit()`. `Movimentacao.EstoqueDestinoID` (já `*string` desde a Story 5.1) preenchido.
- **`backend/handlers/movimentacoes.go`** (estende, nenhum arquivo novo): `RegistrarTransferenciaHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc` — molde exato de `RegistrarBaixaHandler`: guard `middleware.UsuarioDaSessao`; decodifica `{"estoqueDestinoId": string, "quantidade": float64}` (`http.MaxBytesReader` + `json.Decode`, inválido → 400 VALIDATION_ERROR); `produtoID := r.PathValue("id")`, `estoqueOrigemID := r.PathValue("estoqueId")`; chama `services.RegistrarTransferencia(db, produtoID, estoqueOrigemID, req.EstoqueDestinoID, usuario.ID, req.Quantidade)`; mesmo `switch` de erro-para-HTTP de `RegistrarBaixaHandler` (`ErroMovimentacaoValidacao`→400, `ErroQuantidadeIndisponivel`→409, sucesso→201 + `registro.Publish("movimentacoes", ...)`).
- **`backend/main.go`**: registrar `mux.HandleFunc("POST /api/produtos/{id}/estoques/{estoqueId}/transferencia", middleware.RequireAuth(db, jwtSecret)(middleware.RequireRole(services.PapelAlmoxarife)(handlers.RegistrarTransferenciaHandler(db, registro))))`, mesmo bloco/comentário-doc da rota de Baixa (linhas ~494-505) — atualizar também o comentário-doc do pacote no topo (linhas 84-88).
- **Frontend — `frontend/src/pages/ProdutoDetalhePage.tsx`**: renomear `podeRegistrarBaixa` → `podeRegistrarMovimentacao` (mesmo gate `rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife')`, agora usado pelos DOIS botões). Cada `<li>` de "Quantidade por Estoque" ganha um segundo botão "Transferir" (`variant="outline"`, `size="sm"`) ao lado de "Registrar Baixa". Clique abre um `Dialog` (estado `transferenciaEstoque: EstoqueQuantidade | null`, mesmo padrão de `baixaEstoque`) com um `Select` (`@/components/ui/select`, molde de `CadastroProdutoSection.tsx:189-202`) de Estoque destino e um `Input type="number"` de quantidade. A lista de Estoques (`GET /api/estoques`, `{estoques: {id,nome}[]}`, molde de `CadastroProdutoSection.tsx:269`) é buscada quando o diálogo abre (lazy — só quando `transferenciaEstoque !== null`), NÃO no mount da página (só `almoxarife`+ vê o botão, e nem todo `almoxarife`+ abre o diálogo); a linha clicada (`transferenciaEstoque.estoqueId`) é excluída das opções do `Select` (UX: impede escolher origem=destino no cliente, o servidor continua a autoridade real). Falha ao carregar a lista → mensagem no diálogo, `Select` e Confirmar desabilitados. Confirmar dispara `POST /api/produtos/${id}/estoques/${transferenciaEstoque.estoqueId}/transferencia`, `body: JSON.stringify({ estoqueDestinoId, quantidade: Number(valor) })` (molde de `confirmarBaixa`). `res.ok` → `toast.success('Transferência registrada.')`, fecha o diálogo, `await carregarDetalhe()`. `!res.ok` → mensagem de `(await res.json()).error.message` dentro do diálogo, sem fechar.
- **Testes** em todas as camadas: `services/movimentacoes_test.go` (estende — `TestRegistrarTransferencia_Sucesso`, `_DestinoSemLinhaAinda` (destino nunca teve saldo → linha criada com a quantidade transferida), `_OrigemIgualDestino`, `_QuantidadeZeroOuNegativa`, `_QuantidadeMaiorQueDisponivel` (mensagem cita o saldo real da ORIGEM), `_ConcorrenciaSemDeadlock` (duas transferências concorrentes entre os MESMOS dois Estoques em direções opostas, montadas simultaneamente — molde de `TestRegistrarBaixa_ConcorrenciaDuasBaixasMesmaLinha`; nenhum dos dois erros é um `*pq.Error` com `Code == "40P01"` (deadlock_detected), soma dos dois saldos finais preservada)); `handlers/movimentacoes_test.go` (estende — 201/400×2 (quantidade inválida, origem=destino)/409/403/401, molde dos casos de Baixa); `main_test.go` — novo `TestNewMux_ProdutosTransferenciaRotaCarregaRequireRole` (molde de `TestNewMux_ProdutosBaixaRotaCarregaRequireRole`, linha 966 — TRUNCATE já inclui `movimentacoes` desde a Story 5.1, nenhuma edição adicional necessária); `ProdutoDetalhePage.test.tsx` — estende `stubPadrao` para `GET /api/estoques` e `POST .../transferencia`; botão "Transferir" só aparece para `almoxarife`+, diálogo lista Estoques exceto a origem, submissão bem-sucedida chama `toast.success` + refetch, erro do servidor aparece no diálogo.

**Block If:** nada nesta story depende de decisão humana nem de ação de operador fora do repositório — schema já existe (Story 5.1), serviço, handler, rota e UI são inteiramente implementáveis por um agente com o estado atual do repositório. Status final esperado: `done`.

**Never:**
- **Nenhuma migration nova** — `movimentacoes.estoque_destino_id` e `tipo IN (...,'transferencia',...)` já existem desde a Story 5.1 (migration 000021), exatamente para esta story.
- **Não implementar a tela de Histórico nem a assinatura do canal SSE no frontend** (Story 5.3) — a publicação acontece, nenhuma tela assina; a própria tela de Transferência se atualiza sozinha via `carregarDetalhe()`.
- **Nenhum `SELECT ... FOR UPDATE` puro nas duas linhas de `produto_estoque`** — usar `INSERT ... ON CONFLICT DO UPDATE` (ver Boundaries) é obrigatório porque a linha destino pode não existir.
- **Nenhuma trava adquirida fora da ordem canônica ascendente de `estoque_id`** — travar na ordem origem-depois-destino (ordem de argumento, não de valor) reintroduz o deadlock que AD-10 existe para evitar.
- **Nenhum `ON DELETE CASCADE` novo, nenhuma alteração de `ErroQuantidadeIndisponivel`/`ErroMovimentacaoValidacao`** — os dois tipos de erro da Story 5.1 são reaproveitados tal qual, sem novo tipo.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Transferência válida, destino já com linha | `POST .../transferencia` com `estoqueDestinoId` de outro Estoque válido, `quantidade` positiva ≤ disponível na origem, sessão `almoxarife`+ | `201 {"movimentacao": {...tipo:"transferencia", estoqueOrigemId, estoqueDestinoId...}}`; origem debitada, destino creditado, ambos na mesma transação; evento publicado | — |
| Transferência válida, destino NUNCA teve linha nesse Estoque | idem, mas Produto nunca esteve no Estoque destino | `201`; nova linha `produto_estoque` criada para o par (produto, destino) com a quantidade transferida | — |
| Origem igual ao destino | `estoqueDestinoId` == `estoqueId` do path | `400 VALIDATION_ERROR`, nenhuma escrita | rejeitado antes de abrir transação |
| Quantidade zero ou negativa | `quantidade: 0` ou `-5` | `400 VALIDATION_ERROR`, nenhuma escrita | rejeitado antes de abrir transação |
| Quantidade maior que a disponível na origem | `quantidade` > saldo atual da origem | `409 CONFLICT`, mensagem cita o saldo real da ORIGEM; nada debitado nem creditado | ambas as linhas travadas, comparado, `tx.Rollback()` |
| Estoque destino malformado/inexistente | `estoqueDestinoId` não-UUID ou UUID sem Estoque correspondente | `409 CONFLICT`, mesma mensagem de indisponibilidade (0) | `22P02`/`23503` colapsam em `ErroQuantidadeIndisponivel{0}` |
| Papel `usuario` | `POST .../transferencia`, sessão `usuario` | `403 FORBIDDEN` | decidido por `RequireRole`, handler nunca executa |
| Duas transferências concorrentes entre os MESMOS dois Estoques, direções opostas | T1: A→B; T2: B→A, simultâneas | ambas travam os pares na MESMA ordem canônica (nunca a ordem origem/destino declarada) — uma espera a outra, nenhuma trava indefinidamente, nenhum erro de deadlock do Postgres | serialização pelos locks em ordem ascendente |

</intent-contract>

## Code Map

- `backend/services/movimentacoes.go` — estende (Story 5.1 já criou o arquivo): `RegistrarTransferencia`, `travarLinhaProdutoEstoque` (novo, não-exportado). Reusa `Movimentacao`, `ErroMovimentacaoValidacao`, `ErroQuantidadeIndisponivel`, `limiteNumeric103`/`limiteNumeric103Texto` (`produtos.go:32,37`), `pqInvalidTextRepresentation` (`promocao.go:17`), `pqForeignKeyViolation` (`produtos.go:16`). Molde de `RegistrarBaixa` (mesmo arquivo) para a validação pré-transação e o `defer tx.Rollback()`.
- `backend/services/movimentacoes_test.go` — estende: novos casos de `RegistrarTransferencia`, reusa `seedProdutoComSaldo`/`saldoProdutoEstoque`/`contarMovimentacoes` já existentes.
- `backend/handlers/movimentacoes.go` — estende: `RegistrarTransferenciaHandler`. Molde: `RegistrarBaixaHandler` (mesmo arquivo).
- `backend/handlers/movimentacoes_test.go` — estende: novos casos, reusa `seedProdutoComSaldoHandler`/`postBaixa` (novo `postTransferencia` análogo).
- `backend/main.go` — nova rota (junto ao bloco de Baixa, linhas ~494-505) + doc do pacote (linhas 84-88).
- `backend/main_test.go` — novo `TestNewMux_ProdutosTransferenciaRotaCarregaRequireRole` (molde linha 966); TRUNCATE já cobre `movimentacoes`, sem edição extra.
- `frontend/src/pages/ProdutoDetalhePage.tsx` — renomeia `podeRegistrarBaixa`→`podeRegistrarMovimentacao`; botão "Transferir" + diálogo com `Select` de destino, dentro do bloco "Quantidade por Estoque"; reusa `carregarDetalhe`, `authHeaders`, `rankPapel`/`useAuth`.
- `frontend/src/pages/ProdutoDetalhePage.test.tsx` — estende `stubPadrao` (`GET /api/estoques`, `POST .../transferencia`); novos casos, ver Boundaries.
- `backend/services/produtos.go:16` — `pqForeignKeyViolation`, reusado sem mudança.

## Tasks & Acceptance

**Execution:**
- `backend/services/movimentacoes.go` — `travarLinhaProdutoEstoque` (upsert-lock) + `RegistrarTransferencia` (validação, ordenação canônica, débito/crédito atômicos).
- `backend/services/movimentacoes_test.go` — casos de sucesso, destino sem linha prévia, validação, indisponibilidade, concorrência sem deadlock.
- `backend/handlers/movimentacoes.go` (+ `_test.go`) — `RegistrarTransferenciaHandler`, fronteira HTTP pura.
- `backend/main.go` (+ `main_test.go`) — rota atrás de `RequireAuth → RequireRole(almoxarife)`.
- `frontend/src/pages/ProdutoDetalhePage.tsx` (+ teste) — botão "Transferir", diálogo com `Select` de destino, `POST`, toast, refetch.

**Acceptance Criteria:**
- Given dois Estoques diferentes com o Produto presente na origem, when um `almoxarife` (ou acima) registra uma Transferência de origem para destino com quantidade válida, then a checagem de disponibilidade e o débito/crédito são atômicos na mesma transação, com locks adquiridos na ordem canônica `(produto_id, estoque_id)` ascendente, e uma Movimentação `tipo='transferencia'` é criada com origem e destino registrados.
- Given origem igual ao destino, when o Almoxarife tenta transferir, then o sistema rejeita com `400 VALIDATION_ERROR` antes de qualquer escrita.
- Given uma quantidade maior que a disponível na origem, when a transferência é tentada, then o sistema rejeita com `409 CONFLICT` informando a quantidade real disponível na origem, sem debitar ou creditar nada.
- Given duas transferências concorrentes envolvendo os mesmos dois Estoques, montadas em ordens de inserção opostas, when ambas tentam rodar ao mesmo tempo, then a ordem canônica de lock evita deadlock — uma espera a outra, nenhuma trava indefinidamente, nenhum erro de deadlock do Postgres.
- Given uma sessão de papel `usuario`, when ela tenta registrar uma Transferência diretamente pela API, then a resposta é `403 FORBIDDEN` e o handler nunca executa.
- Given um `almoxarife` na tela de detalhe do Produto, when ele clica "Transferir" numa linha de Estoque, escolhe um destino e uma quantidade válida e confirma, then vê um toast de sucesso e as quantidades de origem/destino atualizam na tela (via refetch); se o servidor rejeitar, a mensagem de erro do servidor aparece no diálogo, sem fechar.

## Spec Change Log

_Vazio — sem loopback de `bad_spec` ainda._

## Review Triage Log

### 2026-09-01 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 7: (high 0, medium 4, low 3)
- defer: 5: (high 0, medium 0, low 5)
- reject: 8
- addressed_findings:
  - `[medium]` `[patch]` Teste de concorrência sem deadlock era leniente demais (aceitava `ErroQuantidadeIndisponivel`, só checava a soma) — reescrito para exigir `nil` das duas transferências, saldos finais exatos, e repetir 20 rodadas para tornar uma regressão de ordem de trava observável.
  - `[medium]` `[patch]` Faltava teste de concorrência drenando a MESMA origem — adicionado `TestRegistrarTransferencia_ConcorrenciaMesmaOrigemNuncaFicaNegativo` (duas transferências de 6 sobre saldo 10: uma vence, a outra recebe `ErroQuantidadeIndisponivel{4}`, saldo nunca negativo, 1 Movimentação).
  - `[medium]` `[patch]` Teste de submissão do frontend não verificava o corpo do POST — agora captura e afirma `{ estoqueDestinoId: 'e2', quantidade: 2 }` (chaves camelCase, quantidade numérica).
  - `[medium]` `[patch]` Faltava teste do caminho de falha de `GET /api/estoques` no diálogo — adicionado: mensagem `MENSAGEM_ERRO_LISTAR_ESTOQUES` exibida e Confirmar desabilitado.
  - `[low]` `[patch]` Guarda de limite superior (`> limiteNumeric103`) não era exercida — `TestRegistrarTransferencia_QuantidadeZeroOuNegativa` estendido com dois casos acima do limite.
  - `[low]` `[patch]` Branch de payload JSON malformado do handler sem teste — adicionado `TestRegistrarTransferenciaHandler_400PayloadInvalido` (molde do de Baixa).
  - `[low]` `[patch]` Lista de destino filtrada podendo ficar vazia (instalação com um único Estoque) sem feedback — adicionada a mensagem "Nenhum outro estoque disponível para transferência." + teste; Confirmar permanece desabilitado.

### 2026-09-01 — Review pass (follow-up)
- intent_gap: 0
- bad_spec: 0
- patch: 3: (high 0, medium 0, low 3)
- defer: 3: (high 0, medium 0, low 3)
- reject: 10
- addressed_findings:
  - `[low]` `[patch]` `estoqueOrigemID == estoqueDestinoID` era uma comparação de string sensível a maiúsculas — dois UUIDs idênticos com capitalização diferente escapavam do guard "origem e destino devem ser diferentes" e produziam um autotransferência (débito+crédito na MESMA linha, Movimentação espúria com origem==destino de fato). Trocado para `strings.EqualFold`; adicionado `TestRegistrarTransferencia_OrigemIgualDestinoCaseInsensitive`.
  - `[low]` `[patch]` A linha "Estoque destino malformado/inexistente" da I/O Matrix (→ `409 CONFLICT`) só era verificada em `services/movimentacoes_test.go`, nunca na fronteira HTTP — nada provava que `RegistrarTransferenciaHandler` de fato traduzia esse `*ErroQuantidadeIndisponivel` específico em `409` no envelope HTTP. Adicionado `TestRegistrarTransferenciaHandler_409EstoqueDestinoMalformadoOuInexistente`.
  - `[low]` `[patch]` Dois comentários (`ProdutoDetalhePage.tsx`, linhas ~194 e ~646) diziam que a busca de Estoques destino era disparada por um `useEffect` inexistente — o disparo real é o clique em "Transferir" (`abrirTransferencia`), já documentado corretamente num terceiro comentário na mesma tela. Corrigidos os dois comentários desatualizados.
- Verificação re-executada após os patches: `gofmt -l .` limpo, `go build ./...`/`go vet ./...` limpos, `go test -p 1 -count=1 ./...` (Postgres real) — todos os pacotes `ok`, incluindo os testes novos; `npm run lint`/`npm run build`/`npm run test` (frontend) — 32 arquivos, 363 testes, todos `ok`.

## Design Notes

- **`INSERT ... ON CONFLICT DO UPDATE` em vez de `SELECT ... FOR UPDATE`:** a Story 5.1 só precisava travar UMA linha que o fluxo normal garante existir (o Produto já está listado nessa linha na tela). A Transferência introduz um caso novo: o Estoque DESTINO pode nunca ter tido esse Produto. `SELECT ... FOR UPDATE` sobre uma linha inexistente não trava nada (zero linhas retornadas) — não dá para "reservar" um lock sobre algo que não existe. O upsert-lock resolve isso numa única instrução atômica: cria a linha com `quantidade=0` se ausente (e held sob o lock implícito da própria criação) ou trava a linha existente via a cláusula `DO UPDATE` (um no-op lógico que ainda assim adquire o lock de escrita). Se a transação não chegar a commitar (ex.: saldo insuficiente detectado depois), o `defer tx.Rollback()` desfaz a criação junto — nunca sobra uma linha fantasma de quantidade zero.
- **Ordenação canônica por `estoque_id`, não por papel origem/destino:** AD-10 (epic-5-context.md) exige que o CONJUNTO de pares seja ordenado ascendentemente antes de qualquer lock — nunca na ordem em que o chamador declarou origem/destino. Duas transferências concorrentes entre os mesmos dois Estoques em direções opostas (A→B e B→A) tocam exatamente o mesmo par de linhas; se cada uma travasse "origem primeiro", uma travaria A-depois-B e a outra B-depois-A — deadlock clássico. Ordenando por valor de `estoque_id` (não por papel), as duas travam sempre na MESMA ordem física — uma sempre espera a outra terminar, nunca as duas travam parcialmente e ficam bloqueadas uma na outra.
- **Reaproveitamento total dos tipos de erro da Story 5.1:** `ErroQuantidadeIndisponivel`/`ErroMovimentacaoValidacao` já cobrem exatamente os dois motivos de rejeição desta story (indisponibilidade e validação) — nenhum tipo novo, nenhum código HTTP novo. O colapso "malformado/inexistente → 0 disponível" de spec-5-1 se estende naturalmente ao lado destino (Estoque destino inexistente é, na prática, tão inacessível pelo fluxo normal — que só oferece Estoques reais no `Select` — quanto um `id` malformado era na Story 5.1).
- **Zero migration:** a Story 5.1 já desenhou `movimentacoes.estoque_destino_id` (nullable) e `tipo IN ('baixa','transferencia','ajuste')` justamente para não exigir uma alteração de schema aqui (spec-5-1, Design Notes) — confirmado: nenhuma coluna ou constraint nova é necessária.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` — Postgres real; cobre os novos casos de `movimentacoes_test.go`/`handlers/movimentacoes_test.go`/`main_test.go`, incluindo o teste de concorrência sem deadlock.
- `cd frontend && npm run lint && npm run build && npm run test` — `oxlint`, `tsc`+`vite`, e os casos novos de `ProdutoDetalhePage.test.tsx`.
- `docker compose up --build` (se disponível): logado como `almoxarife`, abrir um Produto com saldo, clicar "Transferir", escolher um Estoque destino e quantidade válida → toast + saldos atualizados nos dois Estoques; tentar origem=destino → rejeitado; tentar quantidade maior que o saldo → mensagem com o valor real disponível; sessão `usuario` chamando a API diretamente → `403`.

**Manual checks (if no CLI):**
- `SELECT quantidade FROM produto_estoque WHERE produto_id=... AND estoque_id IN (origem, destino)` reflete o débito/crédito exatos após um `201`.
- `SELECT * FROM movimentacoes WHERE produto_id=... AND tipo='transferencia'` mostra `estoque_origem_id` e `estoque_destino_id` ambos preenchidos.

## Auto Run Result

**Resumo:** Story 5.2 já chegou a este run com `status: done` (implementação original registrada no commit `f766931`). Este run executou apenas uma passagem de review de follow-up (`followup_review_recommended: true` herdado do run anterior) — nenhuma reimplementação, apenas 4 camadas de review em paralelo sobre o diff completo (`blind-hunter`, `edge-case-hunter`, `verification-gap`, `intent-alignment`) contra `{baseline_revision}` = `6be6d9e27938be1226cd1fe3a879550d07ca4a5f`, seguidas de triagem, 3 patches aplicados e reverificação completa.

**Arquivos alterados nesta passagem de review:**
- `backend/services/movimentacoes.go` — comparação `estoqueOrigemID == estoqueDestinoID` trocada para `strings.EqualFold` (case-insensitive); comentário da função atualizado.
- `backend/services/movimentacoes_test.go` — novo `TestRegistrarTransferencia_OrigemIgualDestinoCaseInsensitive`.
- `backend/handlers/movimentacoes_test.go` — novo `TestRegistrarTransferenciaHandler_409EstoqueDestinoMalformadoOuInexistente` (prova a fronteira HTTP da linha "Estoque destino malformado/inexistente" da I/O Matrix).
- `frontend/src/pages/ProdutoDetalhePage.tsx` — dois comentários desatualizados (referências a um `useEffect` inexistente) corrigidos para descrever o disparo real (clique em "Transferir").

**Findings desta passagem:** 4 camadas em paralelo, 16 findings brutos após dedupe → 3 `patch` (low), 3 `defer` novos (low, ver frontmatter `deferred`, itens 6-8), 10 `reject` (ruído ou já coberto por comportamento explicitamente especificado). 0 `intent_gap`, 0 `bad_spec`.

**Follow-up review recommendation:** `false` — nesta passagem só houve patches `low` (3× low = score 3, abaixo do limiar 5, nenhum `high`).

**Verificação executada:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — limpo.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@127.0.0.1:5432/stockflow?sslmode=disable go test -p 1 -count=1 ./...` — Postgres real local (não Docker, indisponível neste ambiente) — todos os pacotes `ok` (`services` 78.2s, `handlers` 86.9s, incluindo os testes de concorrência e os 2 testes novos desta passagem).
- `cd frontend && npm run lint && npm run build && npm run test -- --run` — `oxlint` limpo, `tsc`+`vite build` ok, `vitest run`: 32 arquivos / 363 testes, todos `ok`.
- `docker compose up --build` não executado (Docker indisponível neste ambiente); checks manuais de banco não repetidos nesta passagem — sem mudança de schema ou de contrato de API nos patches aplicados.

**Riscos residuais:** os 8 itens em `deferred` (frontmatter) permanecem em aberto, todos `severity: low` — nenhum bloqueia o uso da funcionalidade. Nenhum risco novo introduzido pelos 3 patches desta passagem (mudanças pontuais e cobertas por teste novo/existente).

