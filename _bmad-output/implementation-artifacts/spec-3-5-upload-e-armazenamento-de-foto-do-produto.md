---
title: 'Story 3.5 — Upload e armazenamento de foto do Produto'
type: 'feature'
created: '2026-08-31'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: 'e4133840023eaee944fc32e95c71ad55d8ca4584'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Um Produto (Story 3.1) não tem como receber foto hoje — sem imagem, outros Usuários não reconhecem visualmente o material antes de reservar, e o sistema legado só resolvia isso com base64 inline sem redimensionamento consistente (FR-27/FR-28).

**Approach:** Novo endpoint `POST /api/produtos/{id}/fotos` (multipart, campo `foto`, mínimo `almoxarife`) decodifica JPG/PNG/WEBP pelo conteúdo real do arquivo (nunca pela extensão), redimensiona para 500px no maior lado (só reduz, nunca amplia), recomprime em JPEG q=0.82 e grava em `FOTOS_DIR` com nome versionado `<produto_id>-<timestamp_unix>.jpg`, sem overwrite. `GET /api/produtos/{id}/fotos/{arquivo}` (qualquer Usuário autenticado) serve o arquivo salvo. Sem tabela nova: o nome do arquivo é o único vínculo com o Produto; listagem/galeria fica para a Story 3.6.

## Boundaries & Constraints

**Always:**
- `POST /api/produtos/{id}/fotos`: `RequireAuth` -> `RequireRole(almoxarife)` (mesmo padrão de `POST /api/produtos`), corpo multipart, campo `foto`. `id` inexistente ou malformado (SQLSTATE `22P02`, mesmo tratamento de `AtualizarNomeProduto`, services/produtos.go:357) -> `404 NOT_FOUND`, verificado ANTES de tocar o disco.
- Corpo lido sob `http.MaxBytesReader`/`ParseMultipartForm` com um novo `fotoRequestMaxBytes = 15 << 20` (15 MiB — cobre folgadamente uma foto de câmera de celular em JPEG/PNG/WEBP; nenhum documento de planejamento fixa um número, decisão desta spec, mesmo precedente de `importacaoRequestMaxBytes`, handlers/importacoes.go:32). Estourar o limite -> `400 VALIDATION_ERROR`, mensagem específica de TAMANHO (nunca a mesma mensagem de formato).
- Formato aceito decidido só pelo CONTEÚDO do arquivo: registrar decoders de `image/jpeg`, `image/png` e `golang.org/x/image/webp` (import em branco) e decodificar com `image.Decode` — qualquer outro conteúdo (ou arquivo corrompido) falha o decode -> `400 VALIDATION_ERROR`, mensagem específica de FORMATO (nunca a mesma mensagem de tamanho). Extensão do nome do arquivo enviado e `Content-Type` do multipart são ignorados para essa decisão.
- Redimensionamento: se o maior lado decodificado for > 500px, escala para 500px no maior lado mantendo proporção (`golang.org/x/image/draw`); se já for <= 500px, mantém como está — nunca amplia (decisão desta spec: ampliar uma imagem pequena não serve ao propósito de normalizar tamanho e degradaria qualidade sem necessidade; nenhuma AC exige upscale). Sempre recomprime o resultado em JPEG `quality: 82`, mesmo quando o arquivo original já era JPEG e não precisou de resize — mesma regra em qualquer envio (cadastro ou reenvio), não há bifurcação de fluxo.
- Persistência em `services.SalvarFotoProduto(db, fotosDir, produtoID, jpegBytes)`: gera `<produto_id>-<timestamp_unix>.jpg`, abre com `os.O_CREATE|os.O_EXCL` (nunca overwrite); colisão (`os.IsExist`) -> incrementa o timestamp em 1s e tenta de novo, até um teto de tentativas (ex. 1000) antes de devolver erro de infraestrutura. Devolve `{Nome, URL}` (`URL` = `/api/produtos/{id}/fotos/{Nome}`).
- `GET /api/produtos/{id}/fotos/{arquivo}`: só `RequireAuth` (qualquer papel, sem `RequireRole` — visualização liberada a todos). `{arquivo}` validado contra `^<produto_id>-\d+\.jpg$` (via `regexp.QuoteMeta(id)`) antes de resolver o caminho no disco — não bate -> `404 NOT_FOUND`, nunca lê fora de `fotosDir`. Não depende de `produtos.deleted_at` (mesma regra do AD-11: foto sobrevive em disco à soft-delete/mesclagem futura).
- `FOTOS_DIR` (novo env var, `main.go`, mesmo padrão fail-fast de `DATABASE_URL`/`JWT_SECRET` para a criação do diretório): sem valor, default `./fotos` (dev local); `os.MkdirAll(fotosDir, 0o755)` no startup, falha -> `slog.Error` + `os.Exit(1)`. `docker-compose.yml` ganha volume nomeado `stockflow-fotos-data` montado em `/data/fotos` no serviço `api`, com `FOTOS_DIR=/data/fotos` (AD-11/AD-13, volume Docker persistente). `.env.example` documenta a variável.
- `go.mod`/`go.sum` ganham `golang.org/x/image` (decode de WEBP via `x/image/webp`, resize via `x/image/draw`) — única dependência nova, sem framework de imagem pesado.
- Frontend: `CadastroProdutoSection.tsx`, ao receber `201` de `POST /api/produtos`, guarda `{id, nome}` do Produto recém-criado em estado local (a limpeza do formulário para o próximo cadastro já acontece, sem alterar isso) e renderiza um bloco "Adicionar foto" (`<input type="file" accept="image/jpeg,image/png,image/webp">`, atributo `capture` para abrir câmera/galeria em mobile) só para esse Produto — único ponto da UI hoje com um `produto_id` em mãos (Epic 4/Catálogo, com card/detalhe de Produto, ainda não existe). Envio -> `POST /api/produtos/{id}/fotos` (multipart) com `Authorization` (mesmo `authHeaders()` já usado). Sucesso -> `toast.success` e exibe a miniatura da foto salva; exibir essa miniatura exige buscar `GET /api/produtos/{id}/fotos/{nome}` via `fetch` com `Authorization` e montar um Object URL (`URL.createObjectURL`) — nunca um `<img src="/api/...">` direto, que não carrega o header de auth e quebraria a imagem para qualquer papel.
- Erro de upload (`400`/`404`/rede) -> `<p role="alert">` com a mensagem do servidor (ou mensagem genérica em falha de rede/500), mesmo padrão do restante da seção; botão de envio desabilitado durante o envio.

**Block If:** nenhuma condição desta story exige decisão humana em runtime — segue direto.

**Never:**
- Nenhuma tabela nova de fotos — o nome do arquivo é o único vínculo Produto<->foto; endpoint de LISTAGEM (`GET /api/produtos/{id}/fotos` plural) e qualquer galeria/lightbox são escopo da Story 3.6, não desta.
- Nenhum upscale de imagem menor que 500px no maior lado.
- Nenhuma confiança na extensão do arquivo enviado ou no `Content-Type` do multipart para decidir formato — só o conteúdo decodificado.
- Nenhum `DELETE`/edição de foto existente — fora do escopo das ACs desta story.
- Nenhuma alteração em `produtos`/`produto_estoque`/migrations — puramente arquivo em disco + dois endpoints novos.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Upload JPG válido, Produto existente | multipart `foto` = JPG 2000x1000 | Salvo como `<id>-<ts>.jpg`, 1000px no maior lado, JPEG q=0.82 | `201 {"foto":{"nome","url"}}` |
| Upload PNG/WEBP menor que 500px | multipart `foto` = PNG 300x200 | Dimensões preservadas (sem upscale), recomprimido em JPEG q=0.82 | `201` |
| Arquivo maior que 15 MiB | multipart `foto` grande demais | Nenhum arquivo salvo | `400 VALIDATION_ERROR` (mensagem de TAMANHO) |
| Arquivo com conteúdo não reconhecido (ex. PDF renomeado `.jpg`) | multipart `foto` = bytes não decodificáveis | Nenhum arquivo salvo | `400 VALIDATION_ERROR` (mensagem de FORMATO) |
| Segundo upload para o mesmo Produto | Produto já tem `<id>-<ts1>.jpg` | Novo arquivo `<id>-<ts2>.jpg` gravado; `<id>-<ts1>.jpg` intacto | `201`, foto anterior nunca sobrescrita |
| `usuario` chama o endpoint de upload direto | papel `usuario` | Handler nunca executa | `403` (decidido por `RequireRole`, mesmo padrão de `POST /api/produtos`) |
| `id` de Produto inexistente ou malformado | `POST /api/produtos/{id-invalido}/fotos` | Nenhum arquivo salvo | `404 NOT_FOUND` |
| GET de arquivo com nome que não bate no padrão do `id` da URL | `GET /api/produtos/{id}/fotos/outro-arquivo.jpg` | Nenhuma leitura de disco fora do padrão | `404 NOT_FOUND` |

</intent-contract>

## Code Map

- `backend/services/produtos.go:42` (`Produto`), `:103-107` (`ErrProdutoNaoEncontrado`), `:345-390` (`AtualizarNomeProduto`) -- molde de verificação de `id` existente/malformado (SQLSTATE `22P02`) a reaproveitar em `SalvarFotoProduto`.
- `backend/services/fotos.go` (novo) -- `FotoProduto{Nome, URL}`, `SalvarFotoProduto(db, fotosDir, produtoID, jpegBytes) (FotoProduto, error)`: checa Produto existente, gera nome versionado com retry anti-colisão (`O_CREATE|O_EXCL`), escreve em disco.
- `backend/services/fotos_test.go` (novo) -- cenários de existência/colisão/escrita da matriz acima, nível serviço.
- `backend/handlers/importacoes.go:29-105` -- molde de `http.MaxBytesReader` + `ParseMultipartForm` + branch `*http.MaxBytesError` a reaproveitar para `fotoRequestMaxBytes`.
- `backend/handlers/produtos.go` -- molde de `UsuarioDaSessao`/`escreverErro`/`escreverJSON`, `authRequestMaxBytes` como precedente de constante de limite.
- `backend/handlers/fotos.go` (novo) -- `EnviarFotoProdutoHandler(db, fotosDir)` (multipart, decode+resize+encode, chama `services.SalvarFotoProduto`), `ServirFotoProdutoHandler(fotosDir)` (valida `{arquivo}` contra regex do `{id}`, serve bytes com `Content-Type: image/jpeg`).
- `backend/handlers/fotos_test.go` (novo) -- cenários de tamanho/formato/403/404 da matriz acima, nível handler (mesma composição de rota do `newMux`).
- `backend/main.go:230` (`newMux`) -- ganha parâmetro `fotosDir string`; registra as duas rotas novas perto do bloco de Produtos (~linha 307-323); `os.MkdirAll(fotosDir, ...)` fail-fast perto do bloco de `DATABASE_URL`/`JWT_SECRET`; doc comment do topo do arquivo ganha a Story 3.5.
- `backend/main_test.go` -- 14 chamadas a `newMux(db, emailCfg, jwtSecret, iam.Config{...})` (grep `newMux(` no arquivo) ganham um `fotosDir := t.TempDir()` e o argumento extra.
- `backend/go.mod`/`go.sum` -- `golang.org/x/image` (decode WEBP + resize).
- `docker-compose.yml` -- volume nomeado `stockflow-fotos-data`, mount em `/data/fotos` no serviço `api`, `FOTOS_DIR=/data/fotos`.
- `.env.example` -- documenta `FOTOS_DIR` (default local `./fotos`).
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:269-281` (`enviar`, branch de sucesso) -- guarda `{id,nome}` do Produto criado; novo bloco de upload de foto; `frontend/src/lib/session.ts:getAccessToken` reaproveitado via `authHeaders()` já existente no arquivo.
- `frontend/src/components/produtos/CadastroProdutoSection.test.tsx` -- cobre o bloco de upload (sucesso exibe miniatura, erro exibe alerta).

## Tasks & Acceptance

**Execution:**
- `backend/go.mod` (+ `go.sum`) -- adicionar `golang.org/x/image` -- decode WEBP e resize sem framework pesado.
- `backend/services/fotos.go` (+ teste) -- `SalvarFotoProduto`: existência do Produto, nome versionado anti-colisão, escrita sem overwrite.
- `backend/handlers/fotos.go` (+ teste) -- `EnviarFotoProdutoHandler`/`ServirFotoProdutoHandler`: multipart, limite de tamanho, decode+resize+recompressão, serve com validação de nome.
- `backend/main.go` (+ `main_test.go`) -- `fotosDir` (env, fail-fast, `MkdirAll`), registro das 2 rotas, atualização das 14 chamadas de teste a `newMux`.
- `docker-compose.yml`, `.env.example` -- volume nomeado + `FOTOS_DIR`.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx` (+ teste) -- bloco "Adicionar foto" pós-cadastro, exibição autenticada via Object URL.

**Acceptance Criteria:**
- Given um Produto existente, when um Almoxarife envia uma foto JPG/PNG/WEBP (câmera ou galeria), then ela é salva redimensionada a 500px no maior lado e comprimida em JPEG q=0.82, em `FOTOS_DIR`, nunca em base64 no banco.
- Given um arquivo fora do tamanho aceito, when o Almoxarife tenta enviar, then o erro indica especificamente TAMANHO, distinto do erro de formato.
- Given um arquivo fora do formato aceito (conteúdo não decodificável como JPG/PNG/WEBP), when o Almoxarife tenta enviar, then o erro indica especificamente FORMATO, distinto do erro de tamanho.
- Given um Produto já com uma foto, when um Almoxarife envia uma nova foto, then a foto anterior permanece intacta em seu próprio caminho e a nova recebe um nome de arquivo distinto.
- Given um Usuário com papel `usuario`, when ele chama `POST /api/produtos/{id}/fotos` diretamente pela API, then a resposta é `403`; `GET /api/produtos/{id}/fotos/{arquivo}` continua acessível a qualquer papel autenticado.

## Spec Change Log

## Review Triage Log

### 2026-08-31 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 6 (high 2, medium 3, low 1)
- defer: 0
- reject: 8 (low 8)
- addressed_findings:
  - `[high]` `[patch]` `image.Decode` (handlers/fotos.go) decodificava qualquer imagem sem checar as dimensões declaradas antes de alocar — um arquivo pequeno declarando largura/altura enormes causa exaustão de memória (DoS). Achado independentemente pelo Blind Hunter e pelo Edge Case Hunter. Corrigido validando as dimensões via `image.DecodeConfig` antes do decode completo, rejeitando com `mensagemFotoFormato` (400) acima de um teto de megapixels.
  - `[high]` `[patch]` Nenhum teste exercitava o `newMux` real para as rotas de fotos — toda outra família de rota com `RequireRole` tem um `TestNewMux_XRotaCarregaRequireRole` dedicado (convenção de `main_test.go`), Fotos não tinha; `handlers/fotos_test.go` só despachava contra um mux duplicado à mão. Achado pelo Verification Gap Reviewer. Corrigido com `TestNewMux_FotosRotaCarregaRequireRole` em `main_test.go`, mesmo molde das demais.
  - `[medium]` `[patch]` Orientação EXIF nunca era aplicada — fotos de câmera de celular em retrato ficariam de lado/de cabeça para baixo, contrariando o propósito da story (reconhecimento visual do material). Achado pelo Blind Hunter. Corrigido lendo a tag EXIF `Orientation` do JPEG antes do decode e aplicando a rotação/flip correspondente à imagem decodificada antes do resize.
  - `[medium]` `[patch]` `ServirFotoProdutoHandler` (handlers/fotos.go) mapeava QUALQUER erro de `os.Open` para `404 NOT_FOUND` sem log, escondendo falhas reais de infraestrutura (permissão, volume não montado) atrás de um "foto não encontrada". Achado independentemente pelo Blind Hunter e pelo Edge Case Hunter. Corrigido distinguindo `os.IsNotExist` (continua 404) de qualquer outro erro (`slog.Error` + 500 INTERNAL_ERROR).
  - `[medium]` `[patch]` Corrida no frontend: `enviarFoto` (CadastroProdutoSection.tsx) aplicava `toast.success`/miniatura/reset de estado incondicionalmente após o `await`, mesmo se o Almoxarife já tivesse cadastrado um SEGUNDO produto enquanto o upload do primeiro ainda estava em voo — atribuindo visualmente a foto/toast do produto errado. Achado independentemente pelo Blind Hunter e pelo Edge Case Hunter. Corrigido guardando o `id` do produto alvo no início de `enviarFoto` e checando que `produtoCriado?.id` ainda é o mesmo antes de cada atualização de estado pós-`await`.
  - `[low]` `[patch]` O branch de `ParseMultipartForm` para erro que NÃO é `*http.MaxBytesError` (corpo sem `multipart/form-data`) não tinha nenhum teste cobrindo-o. Achado pelo Blind Hunter. Corrigido com um teste que envia `Content-Type: application/json` para o endpoint e confirma `400 VALIDATION_ERROR`/`mensagemFotoFormato`.
  - `[reject]` `regexp.Compile` a cada requisição em `ServirFotoProdutoHandler` — custo de microsegundos, irrelevante para o volume de tráfego real desta ferramenta interna.
  - `[reject]` Ausência de `http.ServeContent`/`Cache-Control`/GET condicional — nenhum documento de planejamento (PRD/epics/arquitetura) menciona cache HTTP para fotos; fora do escopo do intent, não só do spec.
  - `[reject]` Ausência de endpoint de exclusão/limite por Produto — Boundaries "Never" do próprio spec já exclui isso explicitamente, e nenhuma story do roadmap (nem a 3.6) adiciona exclusão de foto.
  - `[reject]` `ParseMultipartForm` sem `RemoveAll` vazando arquivo temporário — premissa falsa: `http.MaxBytesReader` já capa o corpo inteiro em `fotoRequestMaxBytes` (15 MiB), mesmo valor passado como `maxMemory` ao `ParseMultipartForm`, então nenhum spill para disco é alcançável na prática.
  - `[reject]` Comentário de `FOTOS_DIR` em main.go citando "mesmo fail-fast de DATABASE_URL/JWT_SECRET" seria enganoso — o comentário e a implementação batem exatamente com a redação deliberada do próprio spec (fail-fast se refere à criação do diretório, não à presença da env var).
  - `[reject]` Ausência de validação client-side antes do upload — nenhuma AC exige feedback instantâneo no cliente; a validação server-side com mensagens específicas (já testada) é o que as ACs exigem.
  - `[reject]` Falta teste de fronteira exata em 500px — nitpick de cobertura desproporcional sobre uma comparação de uma linha (`<=`) já visivelmente correta e já coberta pelos testes de maior/menor que o teto.
  - `[reject]` Falta teste de corrida com goroutines reais para o retry anti-colisão — testaria a garantia atômica do SO (`O_EXCL`), não lógica de aplicação; desproporcional.

## Design Notes

- **Sem tabela de fotos**: o nome de arquivo `<produto_id>-<timestamp_unix>.jpg` já é o vínculo completo com o Produto (glob por prefixo resolveria "todas as fotos de um Produto" quando a Story 3.6 precisar listar) — criar uma tabela agora seria escopo antecipado de uma necessidade que só a Story 3.6 (galeria) introduz.
- **Resize só reduz, nunca amplia**: a prosa do épico ("redimensionada para 500px no maior lado") é lida como um TETO, não um valor fixo — ampliar uma foto pequena reintroduziria a mesma inconsistência de qualidade que FR-27 existe para eliminar, e nenhuma AC observa o caso de uma foto menor que 500px precisando crescer.
- **Exibição autenticada no frontend via Object URL**: sessão usa access token só em memória (`lib/session.ts`), nunca cookie — um `<img src>` apontando direto para `/api/produtos/{id}/fotos/{arquivo}` nunca carregaria o header `Authorization` e quebraria para todo mundo, inclusive `almoxarife`. `fetch` + `URL.createObjectURL` é a única forma de reaproveitar a mesma sessão Bearer já usada pelo resto do app.
- **15 MiB como teto de upload**: nenhum documento de planejamento fixa esse número; escolhido por analogia a `importacaoRequestMaxBytes` (10 MiB para planilhas) — uma foto de câmera de celular em JPG/PNG/WEBP raramente passa de poucos MB, e o próprio redimensionamento no servidor já normaliza o tamanho final independente do que foi enviado.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` -- sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` -- cobre os novos testes de `services/fotos_test.go`/`handlers/fotos_test.go` e as 14 chamadas atualizadas de `main_test.go`.
- `cd frontend && npm run lint && npm run build && npm run test` -- `oxlint`, `tsc`+`vite` e o teste atualizado de `CadastroProdutoSection` passam.
- `docker compose up --build` -- logado como `almoxarife`, cadastrar um Produto, enviar uma foto JPG/PNG/WEBP grande (>500px) pelo bloco "Adicionar foto"; miniatura aparece; `docker compose exec api ls /data/fotos` mostra o arquivo `<produto_id>-<timestamp>.jpg`.

**Manual checks (if no CLI):**
- Enviar a mesma foto duas vezes para o mesmo Produto -- `ls /data/fotos | grep <produto_id>` mostra dois arquivos com timestamps distintos, nenhum sobrescrito.
- Chamar `POST /api/produtos/{id}/fotos` com um token de papel `usuario` -- `403`.
