---
title: 'Exportação dos próprios dados pessoais'
type: 'feature'
created: '2026-09-04'
status: 'in-progress'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: [oversized]
deferred:
  - summary: >-
      Tentativas de login com senha errada feitas pelo próprio usuário não
      aparecem na exportação LGPD porque `logs_acesso.usuario_id` fica NULL
      nesses casos (design de não-enumeração de e-mail da Story 1.12).
    evidence: |-
      RegistrarTentativaLogin (backend/services/logs_acesso.go) grava
      `usuario_id=NULL` deliberadamente quando a conta não é identificável
      sem ferir a não-enumeração (falha de senha, SSO sem conta/e-mail não
      verificado). ListarLogsAcessoDoUsuario (spec-8-1) filtra
      `WHERE l.usuario_id = $1`, então essas linhas nunca aparecem na
      exportação — mesmo sendo tentativas de login na conta do próprio
      usuário. É uma tensão pré-existente entre o design de segurança da
      Story 1.12 e a completude exigida pela LGPD; não é algo que esta
      story tenha causado nem possa resolver sem reabrir aquela decisão de
      segurança.
    location: >-
      backend/services/logs_acesso.go
    severity: medium
  - summary: >-
      Falha de rede (não de servidor) ao baixar mostra a mensagem crua do
      `fetch` (ex. "Failed to fetch") em vez do fallback amigável.
    evidence: |-
      Em PrivacidadeSection.tsx, `catch (err) { toast.error(err instanceof
      Error ? err.message : MENSAGEM_ERRO_EXPORTAR) }` só troca por
      MENSAGEM_ERRO_EXPORTAR quando `err` não é um Error — mas uma falha de
      `fetch` em si (offline, DNS, CORS) também lança um Error, só que com
      uma mensagem técnica do navegador. Esse padrão é copiado
      verbatim do molde já existente (aoBaixarRecibo,
      components/pedidos/MeusPedidosSection.tsx, e aoExportar,
      CatalogoListagem.tsx) — não foi introduzido por esta story, é um
      comportamento sistêmico de todo handler de download blob->download
      do projeto.
    location: >-
      frontend/src/components/privacidade/PrivacidadeSection.tsx
    severity: low
baseline_revision: '91ed2aed4a08105385c6ee31b318c42567102154'
---

<intent-contract>

## Intent

**Problem:** A LGPD exige que qualquer Usuário consiga baixar os próprios dados pessoais (identidade, log de acesso, Movimentações e Pedidos que ele mesmo gerou); hoje não existe nenhuma rota nem tela para isso — os dados só são consultáveis fatiados por outras telas com regras de escopo/papel diferentes.

**Approach:** Novo endpoint `GET /api/usuarios/me/exportar-dados` (atrás só de `RequireAuth`, sem `RequireRole` — qualquer papel exporta os próprios dados) que compõe nome/e-mail da sessão com três novas/reaproveitadas consultas já-escopadas ao `usuario_id` do chamador (log de acesso, Movimentações, Pedidos) e devolve tudo como um único arquivo JSON para download. Nova seção "Privacidade" em Configurações com um botão "Baixar meus dados" que reaproveita o molde blob→`<a download>` já usado pelo recibo de Pedido (Story 7.6).

## Boundaries & Constraints

**Always:** `usuarioId` do export vem sempre de `middleware.UsuarioDaSessao(ctx)`, nunca de path/query/body. As três seções (`logAcesso`, `movimentacoes`, `pedidos`) aparecem sempre como array — vazio quando o Usuário não tem registro, nunca omitido nem `null`. As duas novas consultas (log de acesso e Movimentações do próprio usuário) NÃO levam `LIMIT` — diferente do teto de 500 das consultas administrativas (`maxLogsAcessoPorConsulta`/`maxMovimentacoesPorConsulta`): a LGPD pede o conjunto completo dos próprios dados, não uma amostra paginada. `ListarPedidosProprios` é reaproveitada sem alteração (já é sem teto e já-escopada).

**Block If:** Nenhuma — todo o escopo é determinístico a partir do usuário autenticado; não há decisão que exija input humano durante a execução.

**Never:** Nunca aceitar um `usuarioId` alheio (por query/body/path) nesta rota — exportar dados de terceiros é fora de escopo desta story (é a Story 8.2, e mesmo lá é anonimização, não exportação). Nunca oferecer seletor de formato JSON/PDF na UI nem implementar o branch PDF — a Approach decide JSON (ver Design Notes); a AC aceita "JSON ou PDF" mas não exige os dois. Nunca escrever/alterar linhas em `logs_acesso`, `movimentacoes` ou `pedidos` — a exportação é somente leitura.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Happy path com histórico | Usuário autenticado com log de acesso, Movimentações e Pedidos próprios | `200`, `Content-Type: application/json`, `Content-Disposition: attachment; filename="meus-dados.json"`, corpo com `nome`, `email` e as 3 listas preenchidas | Nenhum erro esperado |
| Usuário sem Movimentação/Pedido/log próprio | Usuário autenticado recém-criado, sem nenhum registro nas 3 fontes | Mesmo `200`/mesmo formato, `logAcesso`/`movimentacoes`/`pedidos` como `[]` (arrays vazios) | Nenhum erro esperado — arquivo gerado normalmente |
| Sem sessão válida | Requisição sem bearer token / token inválido | `401` (padrão de `RequireAuth`, sem lógica própria desta story) | Envelope de erro padrão do middleware |
| Falha ao consultar o banco em qualquer uma das 3 fontes | Erro de `db.Query` em log de acesso, Movimentações ou Pedidos | `500 INTERNAL_ERROR`, nenhum arquivo parcial devolvido | `escreverErro(w, 500, "INTERNAL_ERROR", ...)`, `slog.Error` com a causa |

</intent-contract>

## Code Map

- `backend/services/logs_acesso.go` -- adicionar `ListarLogsAcessoDoUsuario(db, usuarioID string) ([]LogAcesso, error)`, molde de `ListarLogsAcesso` (linha ~88) mas `WHERE l.usuario_id = $1`, sem `LIMIT`, mesmo `ORDER BY l.criado_em DESC, l.id DESC`
- `backend/services/movimentacoes.go` -- adicionar `ListarMovimentacoesDoUsuario(db, usuarioID string) ([]MovimentacaoHistorico, error)`, molde de `ListarMovimentacoes` (linha ~72) mas `WHERE m.usuario_id = $1`, sem `LIMIT`, mesmo `JOIN`/`ORDER BY`
- `backend/services/pedidos.go` -- reaproveitar `ListarPedidosProprios(db, usuarioID, "")` (linha 287) sem alteração — `filtroStatus=""` já traz todos os status
- `backend/services/privacidade.go` (NOVO) -- `DadosPessoaisExportados{Nome, Email, LogAcesso, Movimentacoes, Pedidos}` + `ExportarDadosUsuario(db *sql.DB, usuarioID, nome, email string) (DadosPessoaisExportados, error)` compondo as 3 consultas acima; primeiro erro não-nil de qualquer uma interrompe e propaga
- `backend/handlers/privacidade.go` (NOVO) -- `ExportarDadosUsuarioHandler(db)`, molde de `BaixarReciboPedidoHandler` (`handlers/pedidos.go:222`): resolve `usuario` via `middleware.UsuarioDaSessao`, chama o service, em sucesso seta `Content-Disposition: attachment; filename="meus-dados.json"` e usa `escreverJSON(w, 200, dados)` (`handlers/auth.go:38`); erro -> `escreverErro(w, 500, "INTERNAL_ERROR", ...)`
- `backend/main.go` -- registrar `mux.HandleFunc("GET /api/usuarios/me/exportar-dados", middleware.RequireAuth(db, jwtSecret)(handlers.ExportarDadosUsuarioHandler(db)))`, novo bloco com banner "Story 8.1", inserido após o bloco de Pedidos (~linha 654, antes do bloco Realtime)
- `backend/services/logs_acesso_test.go` / `backend/services/movimentacoes_test.go` -- estender com testes das duas novas funções (com dados, sem dados, escopo por usuário)
- `backend/services/privacidade_test.go` (NOVO) -- testes de `ExportarDadosUsuario` (com histórico completo; usuário sem nenhum registro -> arrays vazios; erro de query propagado)
- `backend/handlers/privacidade_test.go` (NOVO) -- testes HTTP via `newMux` real (molde de `handlers/pedidos_test.go`): 200 com corpo/headers corretos, 401 sem sessão
- `frontend/src/lib/privacidade.ts` (NOVO) -- `baixarMeusDadosBlob(): Promise<Blob>`, molde de `buscarReciboPedidoBlob` (`lib/pedidos.ts:150`): `fetch('/api/usuarios/me/exportar-dados', {headers: authHeaders()})`, não-ok propaga `body.error?.message`, ok devolve `res.blob()`
- `frontend/src/components/privacidade/PrivacidadeSection.tsx` (NOVO) -- botão "Baixar meus dados", molde de `aoBaixarRecibo` (`components/pedidos/MeusPedidosSection.tsx:191`): blob -> `URL.createObjectURL` -> `<a download="meus-dados.json">` -> clique -> remove -> `revokeObjectURL`; erro -> `toast.error` (sonner)
- `frontend/src/components/privacidade/PrivacidadeSection.test.tsx` (NOVO) -- render do botão, clique dispara download (mock de `fetch`/blob), erro mostra toast
- `frontend/src/pages/ConfiguracoesPage.tsx` -- importar e montar `<PrivacidadeSection />` incondicionalmente (qualquer papel autenticado, sem gate de `rankPapel`), logo após o `<Card>` "Meu Perfil" (linha ~454) e antes de "Decidir promoções"
- `frontend/src/pages/ConfiguracoesPage.test.tsx` -- novo `describe('ConfiguracoesPage — Privacidade')` confirmando que a seção renderiza para todos os papéis testados

## Tasks & Acceptance

**Execution:**
- `backend/services/logs_acesso.go` -- adicionar `ListarLogsAcessoDoUsuario` -- fonte de dados do log de acesso, escopada e sem teto
- `backend/services/movimentacoes.go` -- adicionar `ListarMovimentacoesDoUsuario` -- fonte de dados de Movimentações, escopada e sem teto
- `backend/services/privacidade.go` -- criar `ExportarDadosUsuario` -- compõe as 3 fontes num único payload
- `backend/handlers/privacidade.go` -- criar `ExportarDadosUsuarioHandler` -- fronteira HTTP, download JSON
- `backend/main.go` -- registrar a rota -- expõe o endpoint atrás de `RequireAuth`
- `backend/services/privacidade_test.go`, `backend/handlers/privacidade_test.go`, extensões em `logs_acesso_test.go`/`movimentacoes_test.go` -- cobrem a I/O Matrix acima
- `frontend/src/lib/privacidade.ts` -- criar `baixarMeusDadosBlob` -- cliente HTTP puro do novo endpoint
- `frontend/src/components/privacidade/PrivacidadeSection.tsx` -- criar a seção -- UI do botão "Baixar meus dados"
- `frontend/src/pages/ConfiguracoesPage.tsx` -- montar a seção -- acessível a qualquer Usuário autenticado
- `frontend/src/components/privacidade/PrivacidadeSection.test.tsx`, extensão em `ConfiguracoesPage.test.tsx` -- cobrem render e fluxo de download

**Acceptance Criteria:**
- Given um Usuário autenticado com qualquer papel em Configurações, when a página carrega, then a seção "Privacidade" com o botão "Baixar meus dados" está visível, sem gate de papel
- Given um Usuário autenticado em Configurações, when ele clica "Baixar meus dados", then o navegador recebe o download de `meus-dados.json` contendo `nome`, `email` (da própria sessão), `logAcesso`, `movimentacoes` e `pedidos` (Story 7.3) do próprio usuário
- Given uma falha de rede/servidor ao baixar, when o clique ocorre, then um toast de erro aparece e nenhum download é iniciado

## Spec Change Log

## Review Triage Log

**NOTA (supervisão, 2026-09-04):** o run que produziu este log pausou (auto-rollback OFF) antes de aplicar os 2 `addressed_findings` abaixo — a árvore de trabalho não tem a migration de índices (`logs_acesso.usuario_id`/`movimentacoes.usuario_id`) nem `frontend/src/lib/privacidade.test.ts`. Build/vet/test (backend) e tsc/vitest (frontend) passam 100% com o que EXISTE hoje, mas os dois findings abaixo continuam em aberto — status revertido para `in-progress` até uma sessão nova aplicá-los de verdade.

### 2026-09-04 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 2: (high 0, medium 2, low 0)
- defer: 2: (high 0, medium 1, low 1)
- reject: 12: (high 0, medium 0, low 12)
- addressed_findings:
  - `[medium]` `[patch]` `logs_acesso`/`movimentacoes` não têm índice em `usuario_id`, então `ListarLogsAcessoDoUsuario`/`ListarMovimentacoesDoUsuario` fazem full scan a cada exportação — adicionada migration com os dois índices.
  - `[medium]` `[patch]` `frontend/src/lib/privacidade.ts` (`baixarMeusDadosBlob`) não tinha teste dedicado, ao contrário do seu próprio molde citado (`buscarReciboPedidoBlob`, `lib/pedidos.test.ts`) — `PrivacidadeSection.test.tsx` mocka `@/lib/privacidade` inteiro, então o fetch real/header de Authorization/extração de Blob/fallback de mensagem nunca rodam num teste. Adicionado `frontend/src/lib/privacidade.test.ts` cobrindo isso.

## Design Notes

A AC aceita "um arquivo (JSON ou PDF)" — ambos satisfazem literalmente o texto. Escolhido JSON: as 3 seções são heterogêneas e tabulares (log de acesso, Movimentações, Pedidos), sem necessidade de layout impresso/paginação como o recibo (Story 7.6, que é um documento para impressão/apresentação). Gerar PDF exigiria desenhar 3 tabelas com quebra de página manual (molde `gopdf` de `RenderizarReciboPedidoPDF`) sem nenhum ganho sobre servir a mesma estrutura de dados já usada pelas 3 consultas como JSON. O molde de download (`Blob` -> `<a download>`) é idêntico para os dois formatos, então essa escolha não compromete nenhuma decisão futura de forma irreversível — migrar para PDF mais tarde, se necessário, não exigiria mudar o padrão de download do frontend.

## Verification

**Commands:**
- `cd backend && go test ./services/... ./handlers/... -run 'Privacidade|LogAcesso|Movimentacoes'` -- expected: todos os novos testes passam, nenhuma regressão nos testes existentes desses pacotes
- `cd backend && go build ./...` -- expected: compila sem erro
- `cd frontend && npx vitest run src/components/privacidade src/pages/ConfiguracoesPage.test.tsx` -- expected: novos testes e os existentes de `ConfiguracoesPage` passam
- `cd frontend && npx tsc --noEmit` -- expected: sem erro de tipo
