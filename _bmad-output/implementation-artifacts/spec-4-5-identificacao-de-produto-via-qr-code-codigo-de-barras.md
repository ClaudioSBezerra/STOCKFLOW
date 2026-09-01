---
title: 'Story 4.5 — Identificação de Produto via QR Code / código de barras'
type: 'feature'
created: '2026-09-01'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: 'dfba31652f312a279f6d436beeb34c9ff97b7e63'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** O Catálogo (Stories 4.1/4.3/4.4) só localiza Produto por texto digitado. FR-35 pede identificar um Produto apontando a câmera para o QR Code / código de barras físico que a Ferreira Costa já imprime hoje — o valor codificado é o mesmo Código de Identificação (`produtos.codigo`, índice único parcial `idx_produtos_codigo`). Não existe nenhum scanner, nenhum `fab-scanner`, nenhum endpoint que resolva `codigo` -> Produto.

**Approach:** Backend: um endpoint de resolução exata `GET /api/produtos/por-codigo?codigo=<valor>` (`RequireAuth`, `usuario`+) que devolve o Produto com aquele `codigo` exato ou `404`. Frontend: um componente `ScannerProdutoFab` na `CatalogoPage` — o botão flutuante `fab-scanner` (UX-DR4) + uma câmera em tela cheia que decodifica QR/código de barras com `@zxing/browser` (carregado sob demanda), resolve o valor lido pelo novo endpoint e navega para `/produtos/{id}` (Story 4.4). Falha de contexto seguro, permissão, hardware ou código não reconhecido cai numa mensagem clara e devolve o foco ao campo de busca por texto (Story 4.1).

## Boundaries & Constraints

**Always:**
- `GET /api/produtos/por-codigo?codigo=<valor>`: só `RequireAuth` (qualquer papel `usuario`+), mesmo padrão de `GET /api/produtos/busca` — sem `RequireRole`. `codigo` lido com `strings.TrimSpace`; vazio (ausente ou só espaços) -> `400 VALIDATION_ERROR` "código obrigatório", NENHUMA consulta ao banco. `codigo` trimado com mais de 255 runes -> `400 VALIDATION_ERROR` "código muito longo", sem consulta. Sucesso -> `200 {"produto": <ProdutoBusca>}` (mesma projeção `id`/`nome`/`codigo`/`categoria` de `GET /api/produtos/busca`). Nenhum Produto com aquele `codigo` exato -> `404 NOT_FOUND` "produto não encontrado" (`services.ErrProdutoNaoEncontrado`, mesmo colapso de `ObterProdutoHandler`). Erro de banco -> `500 INTERNAL_ERROR` + `slog`.
- `services.BuscarProdutoPorCodigo(db, codigo string) (ProdutoBusca, error)`: match EXATO `WHERE p.codigo = $1` (case-sensitive, coerente com o índice único `idx_produtos_codigo`), `JOIN categorias` como `BuscarProdutos`. `codigo` é assumido não-vazio e já trimado (o handler valida). `sql.ErrNoRows` -> `services.ErrProdutoNaoEncontrado`. Nunca usa `LIKE`/`escaparCoringasLike` — é igualdade, não padrão.
- Rota registrada em `newMux` como segmento literal `GET /api/produtos/por-codigo`, ANTES de `GET /api/produtos/{id}` no arquivo. Segmento literal vence o wildcard `{id}` no mux do Go 1.22 — sem conflito, mesmo caso já provado por `busca`/`catalogo` coexistindo com `{id}`.
- `frontend/src/lib/scanner/leitor.ts` (novo): `criarLeitorCodigo()` -> `{ iniciar(video: HTMLVideoElement, aoLer: (texto: string) => void): Promise<LeituraAtiva> }`, `LeituraAtiva` = `{ parar(): void }`. `iniciar` faz `await import('@zxing/browser')` (decoder só baixa quando o usuário toca o FAB), `new BrowserMultiFormatReader()`, `decodeFromConstraints({ video: { facingMode: 'environment' } }, video, cb)` — cada `result` chama `aoLer(result.getText())`. Erro do `getUserMedia` PROPAGA para fora de `iniciar` (rejeita a promise). `parar()` para os controls do zxing E todas as tracks de `video.srcObject`.
- `frontend/src/components/catalogo/ScannerProdutoFab.tsx` (novo), prop `aoFalharLeitura: () => void`. FAB: `<button>` fixo, `right-fab-margin`, `bottom-fab-offset-mobile md:bottom-fab-margin`, `size-fab-size`, `rounded-full bg-primary text-primary-foreground shadow-lg z-40`, ícone (`ScanLine`/`QrCode` de `lucide-react`), `aria-label="Escanear código do produto"` — presente só onde este componente for montado (hoje: `CatalogoPage`).
- Toque no FAB: se `!window.isSecureContext` -> exibe mensagem "O scanner exige uma conexão segura (HTTPS). A câmera do navegador não está disponível aqui." e NÃO chama `criarLeitorCodigo`/`getUserMedia`. Caso contrário abre a câmera em tela cheia (`Dialog`/`DialogContent` de `@/components/ui/dialog`, `<video>` com `playsInline muted autoPlay`, texto de instrução, botão "Cancelar").
- Código lido com sucesso: guarda contra disparo duplo (`resolvendoRef`), `parar()`, fecha a câmera, `fetch('/api/produtos/por-codigo?codigo=' + encodeURIComponent(texto.trim()), { headers: authHeaders() })` (mesmo `authHeaders`/`getAccessToken` de `BuscaCatalogo`): `res.ok` -> `navigate('/produtos/' + (await res.json()).produto.id)`; `res.status === 404` -> `falhar('Código não reconhecido: nenhum produto com esse código. Use a busca por texto.')`; outro -> `falhar('Não foi possível abrir o produto agora. Tente novamente.')`.
- `iniciar` rejeita (`DOMException`): `err.name` em `NotAllowedError`/`SecurityError` -> `falhar('Permissão de câmera negada. Libere o acesso à câmera nas configurações do navegador ou use a busca por texto.')`; em `NotFoundError`/`OverconstrainedError`/`DevicesNotFoundError` -> `falhar('Nenhuma câmera disponível. Use a busca por texto para encontrar o produto.')`; qualquer outro -> `falhar('Não foi possível abrir a câmera. Use a busca por texto.')`.
- `falhar(msg)`: `parar()` (se ativo), fecha a câmera, `toast.error(msg)` (`sonner`, mesmo padrão `aria-live` de `spec-4-4`), então `aoFalharLeitura()`. Desmontar o componente também chama `parar()`.
- `frontend/src/components/catalogo/BuscaCatalogo.tsx`: nova prop opcional `inputRef?: RefObject<HTMLInputElement | null>`, mesclada ao `inputRef` interno via callback `ref` no `<Input>` — o atalho `/` e todo o resto do comportamento continuam idênticos.
- `frontend/src/pages/CatalogoPage.tsx`: `const buscaInputRef = useRef<HTMLInputElement>(null)`, passado a `<BuscaCatalogo inputRef={buscaInputRef} />`; renderiza `<ScannerProdutoFab aoFalharLeitura={() => buscaInputRef.current?.focus()} />` ao final do container da página.
- `frontend/package.json`: adiciona `@zxing/browser` (`^0.2.1`) e `@zxing/library` (`^0.23.0`); `npm install` atualiza `package-lock.json`.

**Block If:** nenhuma condição desta story exige decisão humana em runtime — segue direto. HTTPS de produção já é entregue pelo deploy (Coolify/AWS); em HTTP puro o scanner degrada com mensagem, sem ação de operador.

**Never:**
- Nenhuma geração ou impressão de etiqueta de QR Code / código de barras pelo sistema (PRD FR-35 Out of Scope) — só LEITURA do código físico já existente.
- Nenhuma adição ao Carrinho: a tela de Carrinho é de outro épico e não existe. `ScannerProdutoFab` sempre navega para o detalhe hoje; quando o Carrinho chegar, ele reusa este componente com um callback de adicionar-ao-carrinho em vez de `navigate` (não construir esse caminho agora).
- Nenhum branch com `BarcodeDetector` nativo — um único caminho de decodificação (`@zxing/browser`) para Chrome Android e Safari iOS (ver Design Notes).
- Nenhuma mudança em `services.BuscarProdutos`/`buscarProdutosQuery` nem no endpoint `GET /api/produtos/busca` — o novo endpoint é separado.
- Nenhuma alteração de rota no frontend: `/produtos/:id` já existe (Story 4.4).
- Produto sem `codigo` cadastrado: nenhum tratamento novo — ele já é acessível por busca textual (Story 4.1), só não tem o atalho de scanner. O endpoint nunca casa `codigo IS NULL` (o handler barra `codigo` vazio antes da consulta).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Código existente resolvido | `GET /api/produtos/por-codigo?codigo=CAB-004` (Produto existe) | `200 {"produto":{id,nome,codigo,categoria}}` | No error |
| Código não cadastrado | `codigo` sem Produto correspondente | — | `404 NOT_FOUND` "produto não encontrado" |
| `codigo` vazio / só espaços / ausente | `?codigo=` | — | `400 VALIDATION_ERROR` "código obrigatório", sem consulta |
| `codigo` > 255 runes | string longa | — | `400 VALIDATION_ERROR` "código muito longo", sem consulta |
| Papel `usuario` chama direto | token `usuario` | `200`/`404` conforme o código | nunca `403` |
| Rota não colide com `{id}` | `?codigo=` bate no handler `por-codigo` | `400` "código obrigatório" (não `404` de `ObterProdutoHandler`) | — |
| FAB tocado, decodificação OK | contexto seguro, câmera libera, lê `CAB-004` de Produto existente | câmera fecha, `navigate('/produtos/<id>')` | — |
| FAB tocado, código lido inexistente | leitura OK mas `404` do endpoint | câmera fecha, `toast.error` "não reconhecido", foco volta à busca | client-side |
| Permissão de câmera negada | `iniciar` rejeita `NotAllowedError` | mensagem de permissão, câmera não abre, foco na busca | client-side |
| Sem câmera / hardware | `iniciar` rejeita `NotFoundError` | mensagem "nenhuma câmera disponível", foco na busca | client-side |
| Sem contexto seguro (HTTP) | `window.isSecureContext === false` | mensagem "exige HTTPS", `criarLeitorCodigo` nunca chamado | client-side |
| Cancelar / desmontar durante leitura | usuário fecha o `Dialog` ou troca de rota | `parar()` chamado, tracks encerradas, sem `navigate` | client-side |

</intent-contract>

## Code Map

- `backend/services/produtos.go:103-107` (`ErrProdutoNaoEncontrado`, reusar), `:392-403` (`ProdutoBusca`, reusar a projeção), `:430-442` (`buscarProdutosQuery`, molde do `SELECT ... JOIN categorias`), `:444-484` (`BuscarProdutos`, molde de scan de 6 colunas + `sql.NullString` do `codigo`) -- adicionar `BuscarProdutoPorCodigo(db *sql.DB, codigo string) (ProdutoBusca, error)`: `SELECT p.id, p.nome, p.codigo, c.id, c.codigo, c.nome FROM produtos p JOIN categorias c ON c.id = p.categoria_id WHERE p.codigo = $1` via `db.QueryRow`; `sql.ErrNoRows` -> `ErrProdutoNaoEncontrado`.
- `backend/services/produtos_test.go` (testes de `BuscarProdutos` como molde) -- `TestBuscarProdutoPorCodigo_*`: match exato encontrado; código inexistente -> `ErrProdutoNaoEncontrado`; case-sensitivity (`cab-004` != `CAB-004`).
- `backend/handlers/produtos.go:18-45` (doc comment do arquivo — acrescentar linha do `GET /api/produtos/por-codigo`), `:210-247` (`BuscarProdutosHandler`, molde de guarda `UsuarioDaSessao` + `TrimSpace` + teto de runes), `:353-380` (`ObterProdutoHandler`, molde do `switch` de erro `err == nil`/`ErrProdutoNaoEncontrado`/default) -- adicionar `BuscarProdutoPorCodigoHandler(db *sql.DB) http.HandlerFunc`.
- `backend/handlers/produtos_test.go` (testes de `BuscarProdutosHandler` como molde) -- `200` com shape `{"produto":...}`; `400` "código obrigatório" (`?codigo=`); `404` código não reconhecido.
- `backend/main.go:413-419` (bloco+registro de `GET /api/produtos/busca`, molde), `:441-446` (`GET /api/produtos/{id}` — inserir `por-codigo` imediatamente ANTES), doc comment do topo (lista de `GET /api/produtos/...`) -- registrar `mux.HandleFunc("GET /api/produtos/por-codigo", middleware.RequireAuth(db, jwtSecret)(handlers.BuscarProdutoPorCodigoHandler(db)))`.
- `backend/main_test.go:1721-1760` (`TestNewMux_ProdutosDetalheRotaSoRequireAuth`, molde) -- `TestNewMux_ProdutosPorCodigoRotaSoRequireAuth` + uma asserção de precedência: `GET /api/produtos/por-codigo?codigo=` responde `400` "código obrigatório", não `404`.
- `frontend/src/lib/scanner/leitor.ts` (novo) -- wrapper de `@zxing/browser` descrito em Boundaries; isolado para os testes mockarem via `vi.mock('@/lib/scanner/leitor')` (mesmo espírito do wrapper `lib/realtime/client.ts`).
- `frontend/src/lib/scanner/leitor.test.ts` (novo) -- `vi.mock('@zxing/browser')`: `iniciar` liga o callback (result decodificado -> `aoLer` com o texto); `parar()` para controls + tracks.
- `frontend/src/components/catalogo/ScannerProdutoFab.tsx` (novo) -- FAB + `Dialog` da câmera + resolução via endpoint + mapeamento de falhas, descrito em Boundaries. `useNavigate` (react-router), `authHeaders`/`getAccessToken` (`@/lib/session`), `toast` (`sonner`).
- `frontend/src/components/catalogo/ScannerProdutoFab.test.tsx` (novo) -- `vi.mock('@/lib/scanner/leitor')`, `vi.mock('sonner')`, `vi.mock('@/lib/session')`, `MemoryRouter` + rota-espia de `navigate`. Cobre as linhas 8-13 da matriz (decodificação OK -> navigate; `404`; `NotAllowedError`; `NotFoundError`; `isSecureContext=false`; cancelar/desmontar).
- `frontend/src/components/catalogo/BuscaCatalogo.tsx:77-98` (props + merge de `inputRef`), `:197-206` (`<Input ref>`), `:99-111` (atalho `/` — continua igual) -- prop `inputRef` opcional encaminhada.
- `frontend/src/components/catalogo/BuscaCatalogo.test.tsx` -- caso: `inputRef` externo recebe o nó do input; atalho `/` segue focando.
- `frontend/src/pages/CatalogoPage.tsx:46-88` -- `buscaInputRef`, `<BuscaCatalogo inputRef=...>`, `<ScannerProdutoFab aoFalharLeitura=...>`.
- `frontend/src/pages/CatalogoPage.test.tsx` -- FAB presente; leitura que falha (leitor mockado rejeitando) devolve o foco ao campo de busca (`document.activeElement`).
- `frontend/package.json` + `frontend/package-lock.json` -- `@zxing/browser@^0.2.1`, `@zxing/library@^0.23.0`; `npm install`.
- `frontend/src/index.css:80-84` (tokens `--spacing-fab-size` 56px / `--spacing-fab-offset-mobile` 72px / `--spacing-fab-margin` 16px já existem — DESIGN.md) -- read-only, sem alteração.
- `frontend/src/App.tsx:96-102` (rota `produtos/:id` já existe, Story 4.4) -- read-only, sem alteração.

## Tasks & Acceptance

**Execution:**
- `backend/services/produtos.go` (+ `produtos_test.go`) -- `BuscarProdutoPorCodigo`: match exato, `ErrProdutoNaoEncontrado` em `sql.ErrNoRows`, case-sensitive.
- `backend/handlers/produtos.go` (+ `produtos_test.go`) -- `BuscarProdutoPorCodigoHandler`: `200 {"produto":...}`, `400` "código obrigatório"/"código muito longo", `404` não reconhecido, `500` erro de banco; doc comment.
- `backend/main.go` (+ `main_test.go`) -- registra `GET /api/produtos/por-codigo` antes de `{id}`; `TestNewMux_ProdutosPorCodigoRotaSoRequireAuth` + asserção de precedência sobre `{id}`.
- `frontend/package.json` -- adiciona `@zxing/browser`/`@zxing/library`; `npm install`.
- `frontend/src/lib/scanner/leitor.ts` (+ teste) -- wrapper `criarLeitorCodigo` sobre `@zxing/browser` com `import()` dinâmico; `parar()` encerra tracks.
- `frontend/src/components/catalogo/ScannerProdutoFab.tsx` (+ teste) -- FAB, câmera em `Dialog`, resolução por `GET /api/produtos/por-codigo`, navegação no sucesso, mensagens distintas para HTTPS/permissão/hardware/código-não-reconhecido, `aoFalharLeitura` em toda falha.
- `frontend/src/components/catalogo/BuscaCatalogo.tsx` (+ teste) -- prop `inputRef` opcional encaminhada ao `<Input>`, sem outra mudança de comportamento.
- `frontend/src/pages/CatalogoPage.tsx` (+ teste) -- monta `ScannerProdutoFab`, liga `aoFalharLeitura` ao foco do campo de busca.

**Acceptance Criteria:**
- Given o Catálogo em contexto seguro e um QR Code / código de barras de um Produto com `codigo` cadastrado, when o Usuário toca no `fab-scanner` e a câmera lê o código, then a câmera fecha e a navegação vai para `/produtos/{id}` desse Produto (detalhe da Story 4.4).
- Given a câmera sem permissão, sem hardware, ou um código lido que não corresponde a nenhum Produto, when a leitura falha, then uma mensagem clara e específica do motivo aparece (`toast.error`, `aria-live`) e o campo de busca por texto (Story 4.1) fica disponível e recebe o foco.
- Given um ambiente sem contexto seguro (`window.isSecureContext === false`), when o Usuário toca no `fab-scanner`, then aparece a mensagem explicando que a câmera exige HTTPS e nenhuma tentativa de acesso à câmera é feita.
- Given um Produto sem `codigo` cadastrado, when alguém tenta escaneá-lo, then ele permanece acessível por busca textual e o endpoint `GET /api/produtos/por-codigo` nunca o retorna (código vazio -> `400`, sem consulta).
- Given `GET /api/produtos/por-codigo?codigo=<valor>` chamado por papel `usuario`, when o `codigo` existe exatamente, then a resposta é `200 {"produto":...}` (nunca `403`); `codigo` inexistente -> `404 NOT_FOUND`; `codigo` vazio -> `400 VALIDATION_ERROR`.
- Given a rota literal `GET /api/produtos/por-codigo` registrada junto de `GET /api/produtos/{id}`, when uma requisição chega a `/api/produtos/por-codigo`, then ela é atendida pelo handler de código (não colapsada em `ObterProdutoHandler`), e `go vet`/`go build`/`go test ./...` seguem limpos.

## Spec Change Log

## Review Triage Log

### 2026-09-01 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 1: (high 0, medium 0, low 1)
- defer: 0
- reject: 17: (high 0, medium 0, low 17)
- addressed_findings:
  - `[low]` `[patch]` `ScannerProdutoFab.tsx` (`fecharCamera`) não devolvia o foco ao campo de busca ao "Cancelar"/Esc — só em falha "dura" (permissão/hardware/404). Achado pelo Intent Alignment Auditor: AC2 lista "incapaz de reconhecer o código" ao lado de permissão/hardware como motivo de "leitura falha", mas uma leitura ótica que nunca reconhece nada não produz um evento de erro discreto do decoder numa varredura contínua (`@zxing/browser`) — Cancelar é o jeito real de desistir desse caso, e por isso deveria devolver o foco à busca como qualquer outra saída sem sucesso. Corrigido: `fecharCamera()` agora seta `devolverFocoRef` como `falhar()`, com teste atualizado (`ScannerProdutoFab.test.tsx`, "Cancelar durante a leitura") asserindo `aoFalharLeitura` chamado.
- rejected (amostra do raciocínio):
  - Match case-sensitive de `BuscarProdutoPorCodigo` (Blind Hunter): decisão deliberada da spec, coerente com o índice único `idx_produtos_codigo` (também case-sensitive) — um código físico impresso é uma string exata, não um termo de busca a normalizar.
  - Erros não-404 do endpoint colapsados numa mensagem genérica (Blind Hunter): mesmo padrão de `BuscaCatalogo`/todo o resto do frontend (nenhum componente trata 401/403/500 de forma diferenciada); gap sistêmico pré-existente, já registrado como `deferred` em `spec-4-4` para o `realtime/client.ts` — não introduzido por esta story.
  - Classes Tailwind "não definidas" (`size-fab-size`, `bottom-fab-offset-mobile`, `right-fab-margin`, `bottom-fab-margin`) (Blind Hunter): FALSO — os tokens `--spacing-fab-size`/`--spacing-fab-offset-mobile`/`--spacing-fab-margin` já existem em `frontend/src/index.css` (`@theme`, DESIGN.md) desde antes desta story; `npm run build` gera as classes normalmente.
  - `@zxing/library@0.23.0` declara `engines.node >= 24.0.0` (Blind Hunter): `frontend/.npmrc` não tem `engine-strict`, e o `Dockerfile`/CI do frontend usam Node 22 — verificado que `npm install`/`npm run build` completam sem erro no Node 22 local (só um aviso `EBADENGINE` possível, não bloqueante).
  - `facingMode: 'environment'` sem fallback para desktop (Blind Hunter): fora do alvo de plataforma da story — o NFR de usabilidade em campo (epic-4-context.md) nomeia só Chrome Android/Safari iOS, ambos com câmera traseira real; é um valor "ideal" (não `exact`), não um requisito rígido.
  - `getUserMedia` sem branch para `NotReadableError`/`TrackStartError` (Blind Hunter): já cai no fallback genérico "Não foi possível abrir a câmera. Use a busca por texto." — mensagem clara e ação correta (AC não exige uma frase por `DOMException.name`).
  - Ausência de rate limiting no endpoint novo (Blind Hunter): mesmo padrão de TODOS os outros endpoints de leitura do catálogo (`busca`, `catalogo`, `{id}`) — nenhum tem, e produto não é dado sensível neste app interno.
  - Ausência de `mergeRefs` genérico / doc OpenAPI (Blind Hunter): nenhum dos dois existe em nenhum outro lugar do repo hoje — não é uma convenção quebrada por esta story.
  - Ramo "adiciona ao Carrinho" da AC1 não implementado (Intent Alignment Auditor): `epic-4-context.md` (Cross-Story Dependencies) já declara essa dependência "fora deste épico" — não é uma leitura ambígua, é a única leitura possível quando a tela de Carrinho não existe em nenhum lugar do código.
  - Checagem de HTTPS reativa (no toque do FAB) em vez de FAB desabilitado antecipadamente (Intent Alignment Auditor): a mensagem explicativa no toque satisfaz melhor "mensagem explicando o motivo" (EXPERIENCE.md) do que um botão silenciosamente desabilitado — a própria UX do produto rejeita falha silenciosa.
  - Falta de teste novo para "produto sem código continua buscável por texto" (Intent Alignment Auditor): `BuscarProdutos`/`BuscaCatalogo` não foram tocados por este diff — a cobertura já existente da Story 4.1 continua válida, nada de novo a re-provar.
  - Demais achados do Blind Hunter (loading state durante abertura da câmera, `DialogDescription` ausente, falta de log de erro no catch do frontend, checagem defensiva de `dados.produto.id`, teste de fronteira exata em 255 runes): melhorias cosméticas/de robustez fora do que a spec ou a matriz de I/O exigem, sem cenário de falha real observável pelo usuário.
- verification (após o patch): `cd backend && gofmt -l . && go build ./... && go vet ./...` limpo; `go test -p 1 -count=1 ./...` (Postgres 16 real) — todos os pacotes OK; `cd frontend && npm run lint && npm run build && npm run test` — `oxlint`/`tsc`+`vite`/`vitest` (342 testes) OK.

## Design Notes

- **Um único caminho de decodificação (`@zxing/browser`), sem `BarcodeDetector` nativo.** O `BarcodeDetector` não existe no Safari iOS (navegador nomeado explicitamente no NFR de usabilidade em campo do PRD e no `epic-4-context.md`), então um branch nativo cobriria só Chrome Android e deixaria um segundo caminho não testado. `@zxing/browser` decodifica QR + os códigos de barras 1D comuns uniformemente em cima de `getUserMedia` nos dois navegadores. O `await import('@zxing/browser')` dentro de `iniciar()` mantém o bundle do decoder fora do carregamento inicial do Catálogo — ele só baixa quando o Usuário toca o FAB.
- **Endpoint de match exato separado, não reúso de `GET /api/produtos/busca`.** A busca é ranqueada, difusa e limitada a 7; um Código de Identificação escaneado é uma chave exata (índice único parcial `idx_produtos_codigo`), e "código não reconhecido" precisa de um `404` inequívoco. `WHERE p.codigo = $1` é igualdade — nada de `LIKE`/escaping de curinga.
- **`por-codigo` é segmento literal.** No mux do Go 1.22 o literal vence o wildcard `{id}` na mesma posição, sem panic de conflito — exatamente como `GET /api/produtos/busca` e `GET /api/produtos/catalogo` já coexistem com `GET /api/produtos/{id}` hoje.
- **`window.isSecureContext` checado ANTES de `getUserMedia`.** Mantém a mensagem "exige HTTPS" distinta das falhas de permissão/hardware (ACs separadas). Produção é HTTPS (Coolify no staging, AWS Ferreira Costa em prod); `http://<ip-da-lan>` local não tem câmera por design (EXPERIENCE.md, Responsive & Platform).
- **`ScannerProdutoFab` vive na `CatalogoPage`, não no `AppShell`.** É a única superfície real hoje; o FAB só aparece onde o componente é montado, satisfazendo "nunca em telas administrativas" sem lógica de rota. Quando a tela de Carrinho (outro épico) existir, ela monta o mesmo componente trocando o `navigate` por um callback de adicionar-ao-carrinho.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` -- sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` -- cobre `services.BuscarProdutoPorCodigo*`, `handlers.*BuscarProdutoPorCodigo*`, `TestNewMux_ProdutosPorCodigoRotaSoRequireAuth`.
- `cd frontend && npm install && npm run lint && npm run build && npm run test` -- `oxlint`, `tsc`+`vite` (inclui `@zxing/browser`), e os testes de `leitor`, `ScannerProdutoFab`, `BuscaCatalogo` (prop `inputRef`) e `CatalogoPage` (FAB + foco pós-falha).

**Manual checks (if no CLI):**
- `docker compose up --build` sobre HTTPS (ou `localhost`), logar como `usuario`: no Catálogo, tocar no FAB do scanner, apontar para um QR Code/código de barras cujo valor seja o `codigo` de um Produto cadastrado -> a câmera fecha e abre `/produtos/{id}`.
- Negar a permissão de câmera -> mensagem de permissão aparece e o cursor volta para o campo de busca. Abrir por `http://<ip-da-lan>` (sem HTTPS) -> tocar no FAB mostra a mensagem de HTTPS, a câmera nunca liga.

## Auto Run Result

**Resumo:** Endpoint de resolução exata `GET /api/produtos/por-codigo?codigo=<valor>` (backend) + `ScannerProdutoFab` (frontend): FAB `fab-scanner` na `CatalogoPage` que abre a câmera em tela cheia, decodifica QR Code/código de barras com `@zxing/browser` (carregado sob demanda) e navega para `/produtos/{id}` no sucesso. Toda falha (HTTPS ausente, permissão negada, sem hardware, código não reconhecido, ou "Cancelar"/Esc manual — patch desta revisão) mostra uma mensagem clara (`toast.error`) e devolve o foco ao campo de busca por texto (Story 4.1), que nunca deixa de ser o caminho alternativo.

**Arquivos alterados:**
- `backend/services/produtos.go` -- `BuscarProdutoPorCodigo`: match exato de `codigo` (case-sensitive), reusa a projeção `ProdutoBusca`.
- `backend/services/produtos_test.go` -- testes de match exato, código inexistente e case-sensitivity.
- `backend/handlers/produtos.go` -- `BuscarProdutoPorCodigoHandler`: validação (`código obrigatório`/`código muito longo`), `200`/`404`/`500`.
- `backend/handlers/produtos_test.go` -- testes do handler (200/400×2/404/401/papel `usuario`).
- `backend/main.go` -- registra `GET /api/produtos/por-codigo` (segmento literal, antes de `GET /api/produtos/{id}`).
- `backend/main_test.go` -- `TestNewMux_ProdutosPorCodigoRotaSoRequireAuth` (RequireAuth sem RequireRole + precedência sobre `{id}`).
- `frontend/package.json`, `frontend/package-lock.json` -- adiciona `@zxing/browser`/`@zxing/library`.
- `frontend/src/lib/scanner/leitor.ts` (novo) -- wrapper de decodificação sobre `@zxing/browser`, `import()` dinâmico.
- `frontend/src/lib/scanner/leitor.test.ts` (novo) -- testes do wrapper com `@zxing/browser` mockado.
- `frontend/src/components/catalogo/ScannerProdutoFab.tsx` (novo) -- FAB + câmera em `Dialog` + resolução/navegação + mensagens de falha distintas.
- `frontend/src/components/catalogo/ScannerProdutoFab.test.tsx` (novo) -- cobre as 6 linhas de falha/sucesso da matriz da spec.
- `frontend/src/components/catalogo/BuscaCatalogo.tsx` -- prop `inputRef` opcional, mesclada ao ref interno do atalho `/`.
- `frontend/src/components/catalogo/BuscaCatalogo.test.tsx` -- testes do `inputRef` externo.
- `frontend/src/pages/CatalogoPage.tsx` -- monta `ScannerProdutoFab`, liga `aoFalharLeitura` ao foco de `BuscaCatalogo`.
- `frontend/src/pages/CatalogoPage.test.tsx` -- FAB presente + foco pós-falha (integração).

**Findings da revisão:** 1 `patch` (low, aplicado — ver Review Triage Log), 0 `defer`, 17 `reject` (14 do Blind Hunter, 3 do Intent Alignment Auditor — nenhum do Edge Case Hunter/Verification Gap Reviewer).

**Recomendação de revisão de acompanhamento:** `false` (1 patch `low`; `3×medium + 1×low` = `1` < 5, nenhum `high`).

**Verificação executada:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` -- limpo.
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@127.0.0.1:5432/stockflow?sslmode=disable go test -p 1 -count=1 ./...` -- todos os pacotes OK (backend, cmd/migrate-legado, cmd/seed-admin, handlers, iam, middleware, realtime, services).
- `cd frontend && npm install && npm run lint && npm run build && npm run test` -- `oxlint` limpo; `tsc -b && vite build` OK; `vitest run` -- 342 testes, 32 arquivos, todos OK (após o patch de foco em `fecharCamera`).
- Matrix Test Audit: as 12 linhas da I/O & Edge-Case Matrix (6 do endpoint, 6 do FAB/câmera) têm teste correspondente executado e verde na rodada acima.

**Riscos residuais:**
- Nenhum teste manual em hardware real (câmera de um celular Chrome Android/Safari iOS) foi executado nesta sessão automatizada — só testes automatizados com `@zxing/browser`/`getUserMedia` mockados; a verificação manual descrita acima depende de um dispositivo físico.
- `facingMode: 'environment'` sem fallback para desktop/webcam frontal — aceito (fora do alvo de plataforma da story: NFR nomeia só Chrome Android/Safari iOS, ambos com câmera traseira).
- Erros não-404 do endpoint (`401`/`403`/`500`) colapsam numa mensagem genérica no frontend — gap sistêmico pré-existente em todo o app (mesmo padrão de `BuscaCatalogo`, já registrado como `deferred` em `spec-4-4` para `realtime/client.ts`), não introduzido por esta story.
