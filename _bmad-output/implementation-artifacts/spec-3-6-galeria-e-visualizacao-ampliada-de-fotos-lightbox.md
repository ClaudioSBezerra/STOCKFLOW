---
title: 'Story 3.6 — Galeria e visualização ampliada de fotos (lightbox)'
type: 'feature'
created: '2026-08-31'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: 'fcb4048ff420256e293e61fcbcbd522bcdf746cb'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      A ordenação de ListarFotosProduto por nome de arquivo depende de o
      retry anti-colisão de SalvarFotoProduto nunca produzir um timestamp
      menor que um upload anterior, sob concorrência real.
    evidence: |-
      SalvarFotoProduto (Story 3.5) só avança o timestamp em colisões, então
      a ordem é preservada sob uploads sequenciais; sob uploads verdadeiramente
      concorrentes ao mesmo Produto essa garantia não tem teste cobrindo-a —
      característica pré-existente da Story 3.5, não introduzida por esta story.
    location: >-
      backend/services/fotos.go (ListarFotosProduto, SalvarFotoProduto)
    severity: low
  - summary: >-
      ListarFotosProduto e ServirFotoProdutoHandler usam o id da URL sem
      canonicalizar case ao montar o glob/regex do nome de arquivo, embora a
      checagem no banco seja case-insensitive.
    evidence: |-
      Um id com case diferente do usado no upload faria a listagem/GET não
      encontrar arquivos existentes mesmo com o Produto existindo; mesmo
      padrão já presente em ServirFotoProdutoHandler desde a Story 3.5
      (regex via regexp.QuoteMeta) — risco prático desprezível, pois o id
      sempre vem do mesmo fluxo de resposta do servidor, nunca digitado.
    location: >-
      backend/services/fotos.go:116 (ListarFotosProduto), backend/handlers/fotos.go (ServirFotoProdutoHandler)
    severity: low
  - summary: >-
      GET /api/produtos/{id}/fotos pode listar, via filepath.Glob, um
      arquivo que SalvarFotoProduto ainda está escrevendo, servindo bytes
      truncados a um visualizador concorrente.
    evidence: |-
      Janela de milissegundos (entre os.OpenFile e Close em
      SalvarFotoProduto), autolimitante — uma rebusca seguinte corrige, sem
      perda permanente de dado. Corrigir exigiria mudar a escrita de
      SalvarFotoProduto (Story 3.5), fora do escopo desta story pelo Never
      da própria spec.
    location: >-
      backend/services/fotos.go (ListarFotosProduto vs SalvarFotoProduto)
    severity: low
  - summary: >-
      GET /api/produtos/{id}/fotos pode devolver 200 com lista vazia para um
      Produto excluído entre a checagem de existência e o filepath.Glob, em
      vez de 404 NOT_FOUND.
    evidence: |-
      ListarFotosProduto verifica a existência do Produto e só depois roda
      filepath.Glob, sem reverificar; numa janela de milissegundos entre as
      duas operações uma exclusão concorrente do Produto faria a resposta
      cair para "0 fotos" em vez de "produto não encontrado". Hoje não existe
      nenhum fluxo de exclusão de Produto no sistema, então é impraticável de
      disparar no estado atual do app.
    location: >-
      backend/services/fotos.go (ListarFotosProduto)
    severity: low
  - summary: >-
      Se a busca do blob de uma foto no MEIO de uma rebusca de galeria falhar,
      o Object URL já criado para uma foto anterior na mesma chamada fica
      órfão no cache local até o componente desmontar.
    evidence: |-
      carregarFotos (CadastroProdutoSection.tsx) grava cada Object URL
      resolvido em objectUrlCacheRef.current dentro do for, mas só publica
      `fotos` (setFotos) depois do loop inteiro terminar com sucesso; se uma
      foto posterior falhar, a função retorna false antes do setFotos, e o
      Object URL da foto anterior nunca é exibido nem revogado até o
      componente desmontar (quando todo o cache é revogado em bloco) — vazamento
      pequeno e limitado a essa sessão de tela, sem impacto funcional visível.
    location: >-
      frontend/src/components/produtos/CadastroProdutoSection.tsx (carregarFotos)
    severity: low
---

<intent-contract>

## Intent

**Problem:** Um Produto com múltiplas fotos (Story 3.5) hoje só expõe a mais recente enviada na sessão de cadastro — sem galeria nem forma de ampliar, ninguém confirma visualmente o material certo antes de reservar (FR-29); o endpoint de listagem plural foi deliberadamente adiado da Story 3.5 para esta.

**Approach:** Novo `GET /api/produtos/{id}/fotos` (RequireAuth, qualquer papel) lista todas as fotos do Produto. No bloco "Adicionar foto" de `CadastroProdutoSection.tsx` (único ponto da UI hoje com um `produto_id`, mesma limitação documentada na spec-3-5), cada envio bem-sucedido rebusca essa listagem e renderiza uma galeria de miniaturas autenticadas; tocar numa miniatura abre um novo `components/ui/dialog.tsx` em tela cheia com a foto ampliada — fechar (fora, Esc, ou botão) nunca navega nem recarrega, então a posição de rolagem nunca se perde.

## Boundaries & Constraints

**Always:**
- `GET /api/produtos/{id}/fotos`: `RequireAuth` apenas (mesmo padrão do GET singular — qualquer papel autenticado, visualização liberada a todos). `id` inexistente/malformado (SQLSTATE `22P02`, mesmo tratamento das demais rotas de Produto) -> `404 NOT_FOUND`, antes de tocar o disco.
- `services.ListarFotosProduto(db, fotosDir, produtoID)`: reaproveita a mesma checagem de existência de Produto de `SalvarFotoProduto` (services/fotos.go), depois `filepath.Glob(fotosDir/"{id}-*.jpg")`, devolve `[]FotoProduto{Nome,URL}` ordenado por `Nome` (== ordem de envio: timestamp unix de largura fixa) — nunca `nil`, slice vazia se o Produto não tiver foto.
- Resposta: `200 {"fotos":[{"nome","url"}, ...]}`, array vazio (nunca `null`) quando não há foto.
- Frontend (`enviarFoto`, CadastroProdutoSection.tsx): cada upload bem-sucedido passa a buscar `GET /api/produtos/{id}/fotos` e, para cada `nome` ainda sem Object URL em cache local, busca `GET /api/produtos/{id}/fotos/{nome}` (mesmo `authHeaders()`) e monta `URL.createObjectURL` — miniaturas já resolvidas não são rebuscadas. Renderiza a lista completa como galeria: grade de `<button>` (cada um com a `<img>` miniatura, `aria-label="Ampliar foto N de M"`).
- `frontend/src/components/ui/dialog.tsx` (novo): shadcn padrão sobre `Dialog` de `radix-ui` (mesmo primitivo que já sustenta `SheetContent`, `components/ui/sheet.tsx`), mesma convenção de `alert-dialog.tsx`/`sheet.tsx` (`DialogPortal`/`DialogOverlay`/`DialogContent`/`DialogTitle`/`DialogClose`). Overlay e `Esc` fecham por comportamento nativo do Radix, sem código extra.
- Lightbox (CadastroProdutoSection.tsx, novo estado `lightboxIndex: number | null`): clique numa miniatura abre `Dialog` com `DialogContent` estilizado em tela cheia (classes sobrescrevendo o tamanho padrão para `w-screen h-screen max-w-none`, imagem com `object-contain`), `DialogTitle` com classe `sr-only` (ex. "Foto ampliada de {produtoCriado.nome}"), e um botão "Fechar" (`DialogPrimitive.Close`, ícone `XIcon` de `lucide-react`) — cobrindo as 3 formas de fechar da AC.
- Fechar o lightbox só muda estado local (`open=false`); nenhuma navegação, nenhum reload, nenhum scroll programático.

**Block If:** nenhuma condição desta story exige decisão humana em runtime — segue direto.

**Never:**
- Nenhuma navegação prev/next dentro do lightbox aberto — a AC descreve "galeria navegável" (a grade de miniaturas) e "expande em lightbox" como duas coisas distintas; nenhuma AC do épico pede setas de navegação dentro do lightbox.
- Nenhuma alteração em `EnviarFotoProdutoHandler`/`ServirFotoProdutoHandler`/`SalvarFotoProduto` (Story 3.5) — o endpoint novo só lê o disco, nunca escreve.
- Nenhuma tabela nova — mesma decisão da 3.5 (nome do arquivo é o único vínculo Produto↔foto).
- Nenhuma galeria/lightbox no Catálogo (card/detalhe de Produto) — Epic 4 ainda não existe; a galeria fica no mesmo bloco pós-cadastro da 3.5, mesma limitação já documentada lá.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Produto com 3 fotos enviadas na sessão | `GET /api/produtos/{id}/fotos` após 3 uploads bem-sucedidos | 3 miniaturas na galeria, ordem de envio | `200`, 3 itens |
| Produto sem nenhuma foto | `GET /api/produtos/{id}/fotos` | Galeria vazia, sem miniatura, sem erro na tela | `200 {"fotos":[]}` |
| `id` de Produto inexistente ou malformado | `GET /api/produtos/{id-invalido}/fotos` | Nenhuma leitura de disco | `404 NOT_FOUND` |
| Usuário clica numa miniatura da galeria | `lightboxIndex` passa a N | `Dialog` abre em tela cheia com a foto N ampliada | — |
| Lightbox aberto, Usuário aperta `Esc` / clica fora / clica "Fechar" | qualquer uma das 3 ações | `Dialog` fecha; página permanece exatamente na mesma posição de rolagem, sem reload | — |

</intent-contract>

## Code Map

- `backend/services/fotos.go` -- `ListarFotosProduto(db, fotosDir, produtoID) ([]FotoProduto, error)` (novo, ao lado de `SalvarFotoProduto`): reaproveita a checagem de existência de Produto (mesma query `SELECT true FROM produtos WHERE id = $1`); `filepath.Glob` + `sort.Slice` por `Nome`.
- `backend/services/fotos_test.go` -- casos novos: lista ordenada com N fotos, lista vazia, produto inexistente/malformado (mesmo molde de `TestSalvarFotoProduto_ProdutoInexistente`/`_IDMalformado`).
- `backend/handlers/fotos.go` -- `ListarFotosProdutoHandler(db, fotosDir)` (novo, ao lado de `EnviarFotoProdutoHandler`/`ServirFotoProdutoHandler`): só `RequireAuth`, chama `services.ListarFotosProduto`, `200`/`404`.
- `backend/handlers/fotos_test.go` -- casos novos: sucesso com N fotos, galeria vazia, `404`, qualquer papel autenticado acessa (mesmo padrão de `TestServirFotoProdutoHandler_QualquerPapelAutenticado`).
- `backend/main.go:365-377` -- registra `GET /api/produtos/{id}/fotos` (`RequireAuth`, sem `RequireRole`) junto às outras 2 rotas de fotos; comentário do bloco (hoje só cita a Story 3.5) ganha nota da 3.6.
- `frontend/src/components/ui/dialog.tsx` (novo) -- shadcn `Dialog` sobre `Dialog` de `radix-ui`, mesma convenção de `sheet.tsx`/`alert-dialog.tsx` já existentes no projeto.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx:195-215,341-387,543-579` -- substitui `fotoThumbUrl` (miniatura única) por `fotos: FotoProduto[]` + cache de Object URLs por nome; `enviarFoto` passa a rebuscar `GET /api/produtos/{id}/fotos` após sucesso; novo bloco de galeria (grade de miniaturas clicáveis) + lightbox (`Dialog` em tela cheia); docstring do topo do arquivo ganha a Story 3.6.
- `frontend/src/components/produtos/CadastroProdutoSection.test.tsx` -- casos novos: galeria com 2 miniaturas após 2 uploads, clique abre o lightbox com a foto certa, `Esc`/clique fora/botão fecham sem alterar o restante da tela.

## Tasks & Acceptance

**Execution:**
- `backend/services/fotos.go` (+ teste) -- `ListarFotosProduto` -- lista todas as fotos de um Produto, ordenada, vazia se nenhuma.
- `backend/handlers/fotos.go` (+ teste) -- `ListarFotosProdutoHandler` -- fronteira HTTP `RequireAuth`-only da listagem.
- `backend/main.go` -- registra `GET /api/produtos/{id}/fotos`.
- `frontend/src/components/ui/dialog.tsx` (novo) -- primitivo de modal reutilizável, base do lightbox.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx` (+ teste) -- galeria de miniaturas + lightbox em tela cheia.

**Acceptance Criteria:**
- Given um Produto com múltiplas fotos, when um Usuário abre a seção "Adicionar foto" desse Produto, then vê uma galeria com todas as fotos enviadas, em miniatura.
- Given uma foto na galeria, when o Usuário clica nela, then ela expande em lightbox de tela cheia.
- Given o lightbox aberto, when o Usuário fecha (clique fora, `Esc`, ou botão fechar), then o lightbox fecha sem navegação nem reload, mantendo a página exatamente como estava antes de abrir.
- Given um Produto sem nenhuma foto, when a listagem é buscada, then a resposta é `200` com lista vazia, nunca erro.

## Spec Change Log

## Review Triage Log

### 2026-08-31 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 4 (high 1, medium 2, low 1)
- defer: 3
- reject: 9
- addressed_findings:
  - `[high]` `[patch]` `DialogContent` do lightbox (CadastroProdutoSection.tsx) sobrescreve `max-w-[calc(100%-2rem)]`/`translate-x-[-50%]` etc. via `className`, mas a classe base `sm:max-w-lg` (dialog.tsx) sobrevive ao merge do `twMerge` (variante `sm:` não é reconhecida como o mesmo grupo do `max-w-none` sem variante — confirmado rodando `twMerge` diretamente) — em qualquer viewport >= 640px o lightbox fica limitado a 32rem de largura, não "tela cheia". Corrigido forçando a largura/altura com `!` (`sm:!max-w-none` ou equivalente) na instância do lightbox, sem alterar `dialog.tsx`.
  - `[medium]` `[patch]` Upload de foto bem-sucedido (`enviarFoto`, CadastroProdutoSection.tsx) que falha só na rebusca pós-upload (`carregarFotos` devolve `false`) mostra a MESMA mensagem de erro genérica de upload — o usuário acredita que a foto não foi salva (ela foi) e pode reenviar, criando um arquivo duplicado. Achado independentemente pelo Blind Hunter e pelo Edge Case Hunter. Corrigido distinguindo os dois casos: falha no POST em si mantém o erro atual; falha só na rebusca pós-sucesso mostra `toast.success` (a foto FOI salva) mais uma mensagem própria informando que a galeria não atualizou, com um teste novo cobrindo esse caminho.
  - `[medium]` `[patch]` Botão "Fechar" do lightbox (ícone `XIcon` em `dialog.tsx`) não define cor própria e herda `text-foreground` (`#0f1729`, navy escuro) do `body` — sobre o fundo `bg-black/95` do lightbox (CadastroProdutoSection.tsx) o ícone fica com contraste quase nulo, praticamente invisível. Achado pelo Blind Hunter, confirmado lendo `frontend/src/index.css`. Corrigido com uma cor clara explícita só na instância do lightbox (sem alterar o padrão de `dialog.tsx`, usado também por fundos claros).
  - `[low]` `[patch]` Nenhum teste distingue clicar na 2ª miniatura (de uma galeria com 2+ fotos) abrindo o lightbox na foto certa — o mock global de `URL.createObjectURL` devolve sempre a MESMA string, então um teste que abrisse o lightbox com 2 fotos não conseguiria diferenciar `fotos[0]` de `fotos[1]`; um bug futuro que sempre abrisse a 1ª foto passaria despercebido. Achado independentemente pelo Blind Hunter e pelo Verification Gap Reviewer. Corrigido tornando o mock de `createObjectURL` sensível ao conteúdo do blob e adicionando um teste que clica na 2ª miniatura e confirma que o lightbox mostra a 2ª foto, não a 1ª.
  - `[defer]` Ordenação por `Nome` (`ListarFotosProduto`) depende de o retry anti-colisão de `SalvarFotoProduto` (Story 3.5) nunca produzir um timestamp menor que o de um upload anterior — verdade sob uploads sequenciais (o retry só avança o relógio), mas não testado sob concorrência real; característica pré-existente da Story 3.5, não introduzida por esta story.
  - `[defer]` `ListarFotosProduto`/`ServirFotoProdutoHandler` usam o `id` da URL como veio (sem canonicalizar case) tanto na query ao banco (case-insensitive) quanto no glob/regex do nome de arquivo (case-sensitive) — um `id` com case diferente do usado no upload faria a listagem/GET não encontrar arquivos existentes. Mesmo padrão já presente em `ServirFotoProdutoHandler` desde a Story 3.5 (regex via `regexp.QuoteMeta`), não introduzido por esta story; risco prático desprezível (o `id` sempre vem do mesmo fluxo de resposta do servidor, nunca digitado).
  - `[defer]` `GET /api/produtos/{id}/fotos` (novo) pode, em teoria, listar via `filepath.Glob` um arquivo que `SalvarFotoProduto` ainda está escrevendo (entre `OpenFile` e `Close`), servindo bytes truncados a um visualizador concorrente — janela de milissegundos, autolimitante (corrige na próxima rebusca), sem perda de dado. Corrigir exigiria mudar a escrita de `SalvarFotoProduto` (Story 3.5, fora do escopo desta story pelo `Never` da própria spec); risco desprezível para o volume de uso desta ferramenta interna.
  - `[reject]` Ausência de card/detalhe de Produto persistente (Epic 4) para revisitar a galeria depois da sessão de cadastro — mesma limitação já documentada e aceita na Story 3.5 (`done`); Epic 4 objetivamente não existe no código ainda, não é uma leitura em aberto da intenção.
  - `[reject]` "Retorna à posição de rolagem anterior" não é verificado por asserção literal de `scrollTop` — o lightbox é um `Dialog` local que nunca navega nem desmonta a página, então não há nada que pudesse mover o scroll; uma asserção de `scrollTop` em jsdom seria teatro, não verificação real.
  - `[reject]` Busca sequencial (não paralela) das miniaturas em `carregarFotos` — nitpick de performance sem consequência funcional no volume real desta ferramenta interna.
  - `[reject]` Ausência de paginação/limite de fotos por Produto — nenhuma AC exige, especulativo.
  - `[reject]` Ausência de navegação prev/next dentro do lightbox aberto — Boundaries "Never" da própria spec já exclui isso explicitamente, nenhuma AC do épico pede.
  - `[reject]` Ausência de exclusão de foto — fora do escopo desde a Story 3.5, nenhuma AC pede.
  - `[reject]` Falta teste explícito de "miniatura já em cache não é rebuscada" — a consequência de regressão aqui é só uma rebusca redundante (mais lenta), nunca comportamento incorreto visível.
  - `[reject]` Branch de erro de `filepath.Glob` em `ListarFotosProduto` sem teste — inalcançável na prática (o `produtoID` já passou pela checagem de UUID válido no banco antes do Glob rodar).
  - `[reject]` Ausência de teto de tamanho de resposta/rate limiting em `GET /api/produtos/{id}/fotos` — nenhuma AC exige, desproporcional para uma ferramenta interna.

### 2026-08-31 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 1 (high 0, medium 1, low 0)
- defer: 2
- reject: 18
- addressed_findings:
  - `[medium]` `[patch]` `carregarFotos` (CadastroProdutoSection.tsx) não tinha nenhum teste cobrindo o caso em que a listagem (`GET /api/produtos/{id}/fotos`) tem sucesso mas a busca do BLOB de uma foto específica (`GET /api/produtos/{id}/fotos/{nome}`) falha dentro do `for` — uma regressão nesse branch (ex.: trocar `return false` por `continue`) publicaria uma galeria com miniaturas faltando sem nenhum aviso, contradizendo o próprio propósito de `MENSAGEM_ERRO_GALERIA` desta story. Achado pelo Verification Gap Reviewer. Corrigido com um teste novo em `CadastroProdutoSection.test.tsx` que faz a listagem ter sucesso com 2 fotos e a busca do blob da 2ª falhar, confirmando que nenhuma miniatura é renderizada e o aviso de galeria aparece; `gofmt`/`go build`/`go vet`/`go test`, `oxlint`/`tsc`+`vite build`/`vitest` (255 testes) todos limpos após a mudança.
  - `[defer]` `GET /api/produtos/{id}/fotos` pode devolver `200 {"fotos":[]}` em vez de `404 NOT_FOUND` para um Produto excluído entre a checagem de existência e o `filepath.Glob` (janela de corrida) em `ListarFotosProduto` — hoje não existe nenhum fluxo de exclusão de Produto no sistema, impraticável de disparar no estado atual do app.
  - `[defer]` Se a busca do blob de uma foto no meio de uma rebusca de galeria falhar, o Object URL já criado para uma foto anterior na mesma chamada (`carregarFotos`, CadastroProdutoSection.tsx) fica órfão no cache local até o componente desmontar — vazamento pequeno, sem impacto funcional visível.
  - `[reject]` `ListarFotosProduto` compara o `id` da URL byte a byte contra o nome do arquivo sem canonicalizar representações UUID equivalentes (com/sem hífen, maiúsculas) — mesma categoria de risco já aceita no item de case-sensitivity acima; o `id` sempre vem do mesmo fluxo de resposta do servidor, nunca digitado.
  - `[reject]` Busca sequencial (não paralela) dos blobs de foto em `carregarFotos` — mesmo nitpick de performance já rejeitado nesta spec, sem consequência funcional no volume real desta ferramenta interna.
  - `[reject]` `carregarFotos` não registra log (`console.error`) no `catch` que devolve `false` — sem consequência funcional visível ao usuário, mensagem de erro já é exibida na tela.
  - `[reject]` Ausência de um botão "Tentar novamente" na mensagem de erro de galeria — nenhuma AC exige; a mensagem já orienta a recarregar a página.
  - `[reject]` `objectUrlCacheRef` nunca remove entradas de fotos que desapareçam de uma listagem futura (só limpa tudo ao trocar de Produto ou desmontar) — hoje não existe exclusão de foto no sistema, cenário especulativo.
  - `[reject]` Ausência de navegação prev/next dentro do lightbox aberto — Boundaries "Never" da própria spec já exclui isso explicitamente, nenhuma AC do épico pede (mesmo achado da rodada anterior).
  - `[reject]` `DialogTitle`/`alt` da imagem ampliada no lightbox não indicam "foto N de M" para leitor de tela — a spec já prescreve literalmente o texto do `DialogTitle` ("Foto ampliada de {produto}"), sem exigir índice; a miniatura de origem já expõe o índice via `aria-label`.
  - `[reject]` Lightbox reaproveita a mesma miniatura (já redimensionada/recomprimida pela Story 3.5) em vez de buscar uma versão em resolução maior — a spec descreve explicitamente reaproveitar o mesmo Object URL, sem prever um endpoint de imagem em resolução plena.
  - `[reject]` Ausência de paginação/limite de fotos por Produto em `ListarFotosProduto` — nenhuma AC exige, especulativo (mesmo achado da rodada anterior).
  - `[reject]` Falta teste de isolamento entre Produtos diferentes no `filepath.Glob` por prefixo — mesma convenção de nome de arquivo já usada e aceita desde a Story 3.5, sem teste dedicado também lá.
  - `[reject]` Botão "Fechar" do lightbox duplica a marcação de `DialogClose` em vez de reconfigurar o padrão de `dialog.tsx` — decisão estilística da própria spec (`showCloseButton={false}` + instância própria por causa da cor), sem consequência funcional.
  - `[reject]` Ausência de um estado visual distinto entre "enviando foto" e "atualizando galeria" — nenhuma AC exige essa distinção.
  - `[reject]` Ausência de `AbortController`/guarda de desmontagem nas buscas de `carregarFotos`/`enviarFoto` — mesmo padrão (sem timeout/abort) já presente desde a Story 3.5, não introduzido por esta story.
  - `[reject]` Comentários de topo (`CadastroProdutoSection.tsx`, `main.go`) não detalham a busca sequencial nem a ausência de paginação — nitpicks de documentação sem consequência funcional.
  - `[reject]` Classificação de erro do Postgres em `ListarFotosProduto` só trata um código específico (`22P02`) para `id` malformado — reaproveita literalmente o mesmo padrão de `SalvarFotoProduto` (Story 3.5) que a própria spec manda reutilizar.
  - `[reject]` Chave React (`key={foto.nome}`) poderia colidir se duas fotos tivessem o mesmo nome — nomes incluem timestamp de upload e `SalvarFotoProduto` já garante unicidade via retry anti-colisão; cenário inalcançável na prática.
  - `[reject]` Mensagem de erro de galeria (`MENSAGEM_ERRO_GALERIA`) e o reordenamento do toast de sucesso antes da rebusca não estavam no I/O Matrix original da spec — comportamento já revisado e corrigido na rodada anterior desta mesma spec (achado idêntico, não um novo gap).
  - `[reject]` Nenhum teste dirige o cenário literal "3 uploads reais -> 200 com 3 itens" ponta a ponta — a cobertura existente prova o mesmo comportamento por partes (ordenação com 3 fixtures no backend, contagem com 2 uploads reais no frontend), equivalente funcionalmente; nenhuma AC exige o número exato 3 via upload real.

## Design Notes

- **Escopo do "card ou detalhe do Produto" (PRD/épico):** o Catálogo (Epic 4) ainda não existe — mesma lacuna documentada na spec-3-5. A galeria/lightbox desta story vive no mesmo bloco pós-cadastro de `CadastroProdutoSection.tsx`, único lugar da UI hoje com um `produto_id` em mãos. Quando o Epic 4 entregar o card/detalhe, a galeria pode ser extraída para lá sem mudar o contrato do endpoint novo.
- **"Galeria navegável" (FR-29) lida como a grade de miniaturas em si** (navegável = percorrível), não como setas prev/next dentro do lightbox aberto — nenhuma AC do épico (epics.md, Story 3.6) descreve navegação dentro do lightbox, só as 3 formas de fechar.
- **"Sem recarregar a página" / manter a posição de rolagem:** como o `Dialog` é local (nenhuma navegação, nenhuma troca de rota, nenhum unmount de página) e o Radix trava/destrava o scroll do body sem jamais movê-lo, a garantia é estrutural — nenhuma linha desta story lê ou escreve `scrollTop`, então não há nada para "salvar e restaurar".
- **Ordenação por `Nome` do arquivo:** o timestamp unix tem largura fixa (10 dígitos até o ano 2286) e o prefixo `{produto_id}-` é constante por Produto, então ordenar a STRING do nome é equivalente a ordenar pelo timestamp numérico, sem parse extra.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` -- sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` -- cobre os casos novos de `services/fotos_test.go`/`handlers/fotos_test.go`.
- `cd frontend && npm run lint && npm run build && npm run test` -- `oxlint`, `tsc`+`vite` e os testes novos de `CadastroProdutoSection` passam.
- `docker compose up --build` -- logado como `almoxarife`, cadastrar um Produto, enviar 2+ fotos; a galeria mostra as miniaturas; clicar numa abre o lightbox em tela cheia; `Esc` fecha.

**Manual checks (if no CLI):**
- Produto recém-cadastrado sem foto nenhuma -- bloco "Adicionar foto" aparece sem nenhuma miniatura (galeria vazia), sem erro na tela.

## Auto Run Result

**Resumo da mudança implementada:** `GET /api/produtos/{id}/fotos` (backend) lista todas as fotos de um Produto (`RequireAuth` apenas), e `CadastroProdutoSection.tsx` (frontend) passou a rebuscar essa listagem a cada upload bem-sucedido, renderizando uma galeria de miniaturas clicáveis que abrem um novo `Dialog` (`components/ui/dialog.tsx`) em tela cheia como lightbox — fechável por clique fora, `Esc`, ou botão "Fechar", sempre sem navegação/reload. Já implementada e revisada numa rodada anterior (commit `2d3d791`); esta é uma segunda passada de revisão (`followup_review_recommended: true` herdado dessa rodada).

**Arquivos alterados nesta rodada de revisão:**
- `frontend/src/components/produtos/CadastroProdutoSection.test.tsx` -- adicionado 1 teste novo cobrindo o caso em que a listagem de fotos tem sucesso mas a busca do blob de uma foto específica falha.
- `_bmad-output/implementation-artifacts/spec-3-6-galeria-e-visualizacao-ampliada-de-fotos-lightbox.md` -- registro desta rodada de revisão (Review Triage Log, `deferred`, `Auto Run Result`, `status`, `followup_review_recommended`).

(Nenhum outro arquivo de produto mudou nesta rodada — a implementação em si já estava em `2d3d791`; ver `Code Map` acima para a lista completa de arquivos da story.)

**Revisão desta rodada:**
- intent_gap: 0
- bad_spec: 0
- patch: 1 (medium 1) -- aplicado (teste novo cobrindo falha de busca de blob no meio de uma rebusca de galeria multi-foto)
- defer: 2 (low 2) -- adicionados ao `deferred` do frontmatter
- reject: 18 -- descartados (ver Review Triage Log para o detalhamento)

**Recomendação de revisão de acompanhamento:** `false` -- único patch desta rodada foi `medium` (score 3×1 + 1×0 = 3, abaixo do limiar 5), nenhum `high`.

**Verificação executada:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` -- limpo.
- `cd backend && go test -p 1 -count=1 ./...` -- todos os pacotes `ok` (nenhuma mudança de código backend nesta rodada).
- `cd frontend && npm run lint && npm run build && npm run test` -- `oxlint` limpo, `tsc`+`vite build` OK, 255 testes passando (26 arquivos), incluindo o teste novo.

**Riscos residuais:** os 2 itens `defer` desta rodada (janela de corrida entre checagem de existência e `filepath.Glob` em `ListarFotosProduto`; Object URL órfão no cache local quando uma foto no meio de uma rebusca de galeria falha) são de severidade `low`, autolimitados, e não têm caminho prático de disparo no estado atual do app (sem fluxo de exclusão de Produto). Nenhum risco novo de severidade `medium`/`high` identificado nesta rodada.

