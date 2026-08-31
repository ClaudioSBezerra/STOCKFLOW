---
title: 'Story 3.2 — Nomenclatura Guiada por subtipo'
type: 'feature'
created: '2026-08-30'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: 'cfc687c04a0beced58c013c615d795b04f68c549'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md', '{project-root}/_bmad-output/planning-artifacts/prds/prd-stockflow-2026-08-29/addendum.md']
warnings: ['oversized']
deferred:
  - summary: >-
      A tabela `nomenclatura_templates` (seed fixo dos 28 templates) usa
      `subtipo` (texto livre com em-dash, ex. "Cabos — Elétrico") como único
      handle natural para uma futura migração legada (Story 3.7) reencontrar
      cada linha — sem chave estável/checksum, uma retipagem futura do texto
      (espaçamento, travessão diferente) faria a Story 3.7 não encontrar a
      linha esperada.
    evidence: |-
      Comentário da migration 000013 e do Code Map da spec-3-2 citam
      explicitamente addendum §G como fonte única e a Story 3.7 como quem
      "encontra as linhas já gravadas e não as reinsere" — mas nada além do
      texto de `subtipo` amarra essa expectativa. Achado do Blind Hunter
      (review automatizado) na primeira revisão desta story.
    location: >-
      backend/migrations/000013_create_nomenclatura_templates.up.sql
    severity: low
---

<intent-contract>

## Intent

**Problem:** Hoje `produtos.nome` (Story 3.1) é sempre texto livre — não existe `nomenclatura_templates` nem `template_id`, então nomes de um mesmo subtipo de material variam entre quem cadastra, sem nenhuma forma guiada de padronizar (FR-9).

**Approach:** Nova tabela `nomenclatura_templates` (seed fixo dos 28 templates do addendum §G) + `ALTER TABLE produtos ADD COLUMN template_id` (nullable, FK). `services.CriarProduto` ganha validação opcional: com `template_id`, `nome` deve preencher todos os placeholders `[X]` do template, na mesma ordem, ou é rejeitado; sem `template_id`, comportamento inalterado (texto livre). Nova função/endpoint dedicados só a editar `nome` (`services.AtualizarNomeProduto` + `POST /api/produtos/{id}/renomear`) revalidam contra o mesmo template quando aplicado — não existe hoje, em nenhum épico do roadmap, uma tela de edição geral de Produto; este endpoint é a única superfície de edição que a AC3 exige, e cobre só `nome`.

## Boundaries & Constraints

**Always:**
- **`nomenclatura_templates`** (migration `000013`): `id UUID PK DEFAULT gen_random_uuid()`, `subtipo VARCHAR(255) NOT NULL UNIQUE`, `template VARCHAR(255) NOT NULL`. Seed via `INSERT` no próprio `.up.sql` com as 28 linhas do addendum §G (fonte única, não reconstruir a partir do legado), `(subtipo, template)`: `('Cabos — Elétrico', 'CABO [TIPO] [TENSÃO] Ø[SEÇÃO]MM² [COR] [COMPLEMENTO]')`, `('Cabos — Rede', 'CABO REDE [BLINDA] [CAT] [NORMA] [COR]')`, `('Cabos — Coaxial/Fibra/Especial', 'CABO [TIPO] [ESPECIF] [NORMA/HOMOL]')`, `('Elétrica — Luminárias', 'LUMINÁRIA [TIPO FONTE] [APLICAÇÃO] [DIM] [POTÊNCIA] [TEMP.COR]')`, `('Elétrica — Painéis/Quadros', 'PAINEL [OU QUADRO] ELÉTRICO [TENSÃO] [TIPO]')`, `('Elétrica — Tomadas/Interruptores', 'TOMADA INDUSTRIAL [APLICAÇÃO] [POLOS] [CORRENTE]A [TENSÃO]V [IP] [COR]')`, `('Elétrica — Refletores', 'REFLETOR LED [POTÊNCIA]W [TEMP.COR] [APLICAÇÃO]')`, `('Elétrica — Abraçadeiras/Acess.', 'ABRAÇADEIRA [TIPO] [MATERIAL] [DIAM/BITOLA]')`, `('Hidráulica — Conexões PVC', '[PEÇA] PVC [CLASSE] DN[XX] [COR]')`, `('Hidráulica — Válvulas/Registros', 'VÁLVULA [OU REGISTRO] [TIPO] DN[XX] [MATERIAL] [PRESSÃO]')`, `('Hidráulica — Louças/Vasos', 'BACIA SANITÁRIA [TIPO] [MODELO/MARCA] [COR] [ESTADO]')`, `('Hidráulica — Torneiras/Chuveiros', 'TORNEIRA [APLICAÇÃO] [DN/POL] [MATERIAL/ACAB]')`, `('Hidráulica — Mangueiras/Incêndio', 'MANGUEIRA INCÊND. [DIAM] [COMP] [TIPO]')`, `('Tubo — Aço Carbono', 'TUBO AÇO CARBONO [ACAB] [BITOLA] [COMP]')`, `('Tubo — Aço Inox', 'TUBO INOX [NORMA/LIGA] Ø[XX]MM [COMP]')`, `('Tubo — PVC Esgoto/Água', 'TUBO PVC [TIPO] DN[XX] [COR] NBR[XXXX]')`, `('Tubo — PEAD/PPR', 'TUBO PEAD [PN] DN[XX]')`, `('Perfil — Aço Estrutural', 'PERFIL [SEÇÃO W/I/U/Z/L] AÇO [MEDIDA H]X[BF]MM [COMP]')`, `('Perfil — Alumínio', 'PERFIL ALUMÍNIO TIPO [H/U/T/Z/CART.] [MEDIDA] [APLICAÇÃO]')`, `('Perfil — Cartola/Estrutural', 'PERFIL CARTOLA [MEDIDA H]X[LA]MM [COMP]')`, `('Ferragem — Barras Roscadas', 'BARRA ROSCADA [MATERIAL/ACAB] [BITOLA] L=[XX]M')`, `('Ferragem — Telas de Aço', 'TELA AÇO SOLDADA Q-[XXX] [NORMA] [LARG]X[COMP]M')`, `('Ferragem — Chumbadores', 'CHUMBADOR [TIPO: J/CBA/EXP] [BITOLA] [COMP]')`, `('Ferragem — Estruturas Metálicas', 'ESTRUTURA METÁLICA TUBULAR [APLICAÇÃO] [DIMENSÕES]')`, `('Mat. Construção — Pisos/Porcel.', 'PORCELANATO [MARCA] [DIM]CM [TIPO/ACAB] [QDE PCJ/CX]')`, `('Mat. Construção — Parafusos/Fix.', 'PARAFUSO [TIPO] [BITOLA]X[COMP]MM [MATERIAL/ACAB]')`, `('Mat. Construção — Forro/Gesso', 'PLACA GESSO [TIPO: GRELHA/LISA/ST] [DIM]CM')`, `('Telha/Calha/Rufo', 'TELHA [PERFIL: TRAP/ONDA] [MATERIAL: AC/AL] [COMP]X[LARG]M')`.
- **`produtos`** (migration `000014`, `ALTER TABLE`): `ADD COLUMN template_id UUID REFERENCES nomenclatura_templates(id)` — nullable, sem `ON DELETE` especial (linhas de `nomenclatura_templates` são seed fixo, nunca excluídas). `down`: `ALTER TABLE produtos DROP COLUMN template_id`.
- **Validação de nome contra template** (novo helper em `services/nomenclatura.go`, ex. `nomeValidoParaTemplate(templateTexto, nome string) bool`): extrai os tokens `\[([^\]]+)\]` do template mantendo a ordem; constrói um padrão âncorado (`^...$`) escapando (`regexp.QuoteMeta`) cada trecho literal entre tokens e substituindo cada token por um grupo de captura não-guloso `(.+?)`; casa `nome` contra esse padrão — sem casar, ou com algum grupo capturado vazio/só espaço após trim, é inválido. Comparação do esqueleto literal é **case-sensitive** (mesma grafia do template, ex. `CABO`) — decisão de implementação registrada em Design Notes, nenhuma AC testa variação de caixa.
- **`services.CriarProduto`** ganha `TemplateID string` em `CriarProdutoInput`. `TemplateID` vazio (trim) → comportamento da Story 3.1 inalterado (nome livre). `TemplateID` preenchido → antes de abrir a transação (junto às demais validações), `SELECT template FROM nomenclatura_templates WHERE id = $1`: zero linhas ou `id` malformado (`pq` SQLSTATE `22P02`, `pqInvalidTextRepresentation`) → `ErroProdutoValidacao{"template selecionado não existe"}`; `nomeValidoParaTemplate(templateTexto, nomeTrimado)` falso → `ErroProdutoValidacao{"nome não corresponde ao formato do template selecionado"}`. Sucesso: `template_id` entra na mesma `INSERT INTO produtos (...)` já existente (mesma transação, sem coluna extra de escrita separada).
- **Novo `services.ErrProdutoNaoEncontrado = errors.New("produto não encontrado")`** — mesmo padrão de `ErrEstoqueNaoEncontrado`/`ErrContaNaoEncontrada`: `id` inexistente OU malformado (`22P02`) colapsam nele.
- **Nova `services.AtualizarNomeProduto(db *sql.DB, id string, novoNome string) (Produto, error)`** — escopo é só `nome` (dimensões/categoria/estoque/observações ficam fora, sem endpoint de edição para eles nesta story): valida `novoNome` (trim, 1..255 runes, mesma regra de `CriarProduto`) → `ErroProdutoValidacao` se falhar; `SELECT template_id FROM produtos WHERE id = $1` (zero linhas ou `22P02` → `ErrProdutoNaoEncontrado`); `template_id` `NULL` → segue para o `UPDATE`; `template_id` preenchido → busca o `template` correspondente e revalida com `nomeValidoParaTemplate` (AC3 — burlar editando depois não funciona), inválido → `ErroProdutoValidacao{"nome não corresponde ao formato do template aplicado a este produto"}`; válido → `UPDATE produtos SET nome = $1 WHERE id = $2 RETURNING id, nome`.
- **`services.ListarNomenclaturaTemplates(db) ([]NomenclaturaTemplate, error)`** — `SELECT id, subtipo, template FROM nomenclatura_templates ORDER BY subtipo ASC` (molde direto de `ListarCategorias`). `NomenclaturaTemplate{ID, Subtipo, Template string}` com tags `json:"id"`/`"subtipo"`/`"template"`.
- **`handlers.CriarProdutoHandler`**: `criarProdutoRequest` ganha `TemplateID string `json:"template_id"`` repassado 1:1 para `services.CriarProdutoInput.TemplateID` (mesmo padrão dos demais campos — zero value quando ausente, sem validação de formato no handler).
- **Nova `handlers.AtualizarNomeProdutoHandler(db)`** expõe `POST /api/produtos/{id}/renomear` (nomeada como ação, não `PUT`/`PATCH` — o projeto não usa esses métodos hoje; mesmo padrão de `/desativacao`, `/rebaixamento`, `/decisao`): corpo `{"nome": string}`; `r.PathValue("id")`; sucesso → `200 {"produto":{"id","nome"}}`; `errors.As(err, &erroValidacao)` → `400 VALIDATION_ERROR`; `errors.Is(err, services.ErrProdutoNaoEncontrado)` → `404 NOT_FOUND`; demais → `500 INTERNAL_ERROR` + `slog.Error`. `RequireAuth → RequireRole(almoxarife)` — mesmo papel mínimo do cadastro.
- **Nova `handlers.ListarNomenclaturaTemplatesHandler(db)`** expõe `GET /api/nomenclatura-templates` (só `RequireAuth`, mesmo padrão de `GET /api/categorias`) → `200 {"templates":[{"id","subtipo","template"}, ...]}`.
- **`main.go`**: registra as duas rotas novas no mesmo bloco de Produtos (linhas ~294-298 hoje), mesmo molde de composição de middleware do `POST /api/produtos`/`GET /api/categorias`; comentário-doc do bloco (linhas ~28-31) estendido citando a Story 3.2.
- **Frontend — `CadastroProdutoSection.tsx`**: novo state `templates: NomenclaturaTemplate[]` (`{id, subtipo, template}`) e `templateId: string`, carregado de `GET /api/nomenclatura-templates` no mesmo `Promise.all` de `carregarListas` (junto com categorias/estoques). Novo `<Select>` "Template de nomenclatura (opcional)" com opção vazia = nome livre + uma `SelectItem` por template (rótulo = `subtipo`). Quando `templateId` não é vazio, um texto de apoio abaixo do campo Nome mostra o padrão exato do template selecionado (ex. "Formato: CABO [TIPO] [TENSÃO] Ø[SEÇÃO]MM² [COR] [COMPLEMENTO]") — só orientação visual; nenhuma validação de formato no cliente, mesmo padrão já documentado no cabeçalho do arquivo (o cliente não duplica validação do servidor). Payload de `POST /api/produtos` ganha `template_id: templateId === '' ? undefined : templateId`.

**Block If:** nada aqui depende de decisão humana ou de ação de operador fora do repositório — schema, endpoints e UI são inteiramente implementáveis por um agente. Status final esperado: `done`.

**Never:**
- **Nenhuma tela de edição geral de Produto** (dimensões/categoria/estoque/código/observações) — não existe em nenhum épico do roadmap (confirmado: nenhuma story de nenhum épico cobre "editar produto"); só `nome` é editável, e só pelo novo endpoint desta story.
- **Nenhuma troca de `template_id` depois do cadastro** — a AC3 fala só de editar `nome`; trocar de template não é pedido e fica fora de escopo.
- **Nenhuma UI de "editar produto"** no frontend — a AC3 é validada via API (teste de integração de `services`/`handlers`), como já é o padrão desta base para regras sem tela própria ainda.
- **Nenhuma emissão SSE** no canal `produtos` (AD-3) — mesmo precedente da Story 3.1, chega só na 4.4.
- **Nenhuma migração de dados legados de nomenclatura/templates** — Story 3.7.
- **Nenhuma listagem/busca de Produtos** (`GET /api/produtos`) — Epic 4.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Cadastro com template, nome completo | `POST /api/produtos` com `template_id` válido e `nome` preenchendo todos os placeholders na ordem | `201`; `produtos.template_id` grava o template escolhido | — |
| Cadastro com template, placeholder faltando | mesmo `template_id`, `nome` sem um dos placeholders (ou fora de ordem) | `400 VALIDATION_ERROR` citando que o nome não corresponde ao template; nada gravado | envelope de erro |
| Cadastro com `template_id` inexistente | UUID válido sem linha em `nomenclatura_templates` | `400 VALIDATION_ERROR` "template selecionado não existe"; nada gravado | idem |
| Cadastro sem template | `template_id` ausente/vazio | `201`; nome livre, sem validação de estrutura (regressão da Story 3.1) | — |
| Renomear com template aplicado, nome válido | `POST /api/produtos/{id}/renomear`, Produto tem `template_id`, novo nome preenche o template | `200 {"produto":{"id","nome"}}`; `produtos.nome` atualizado | — |
| Renomear com template aplicado, nome inválido | mesmo Produto, novo nome não bate com o template | `400 VALIDATION_ERROR`; `nome` no banco permanece o antigo | envelope de erro |
| Renomear Produto sem template | `produtos.template_id IS NULL` | `200`; qualquer texto aceito | — |
| Renomear `id` inexistente | UUID sem linha correspondente (ou malformado) | `404 NOT_FOUND` | `ErrProdutoNaoEncontrado` |
| `GET /api/nomenclatura-templates` | qualquer sessão autenticada | `200` com as 28 linhas, ordenadas por `subtipo` | — |
| Papel `usuario` chama `/renomear` direto | sessão `usuario` | `403 FORBIDDEN`; handler não executa, banco não é tocado | decidido por `RequireRole` |

</intent-contract>

## Code Map

- `backend/migrations/000013_create_nomenclatura_templates.{up,down}.sql` (novos) — tabela + seed das 28 linhas (addendum §G, carregado via `context:`). `down`: `DROP TABLE IF EXISTS nomenclatura_templates`.
- `backend/migrations/000014_add_template_id_to_produtos.{up,down}.sql` (novos) — `ALTER TABLE produtos ADD COLUMN template_id UUID REFERENCES nomenclatura_templates(id)`. `down`: `ALTER TABLE produtos DROP COLUMN template_id`.
- `backend/services/nomenclatura.go` (novo) — `NomenclaturaTemplate struct{ID, Subtipo, Template string}`; `ListarNomenclaturaTemplates(db)`; `nomeValidoParaTemplate(templateTexto, nome string) bool` (regex de tokens `\[([^\]]+)\]`).
- `backend/services/nomenclatura_test.go` (novo) — `nomeValidoParaTemplate`: template sem token (nome deve casar exato); template com 1/vários tokens preenchidos corretamente; placeholder vazio; placeholder fora de ordem/faltando; caracteres especiais do template (`Ø`, `²`, `=`) escapados corretamente.
- `backend/services/produtos.go` (editar) — `CriarProdutoInput.TemplateID`; `ErrProdutoNaoEncontrado`; validação de template dentro de `CriarProduto` (antes da transação); nova `AtualizarNomeProduto`; `INSERT` de `produtos` ganha coluna `template_id`.
- `backend/services/produtos_test.go` (editar) — `CriarProduto` com template válido/inválido/inexistente e sem template (regressão); `AtualizarNomeProduto`: nome válido sem template, nome válido revalidado contra template, nome inválido contra template (nome antigo preservado), `id` inexistente.
- `backend/handlers/produtos.go` (editar) — `criarProdutoRequest.TemplateID`; nova `AtualizarNomeProdutoHandler`; nova `ListarNomenclaturaTemplatesHandler`.
- `backend/handlers/produtos_test.go` (editar) — `POST /api/produtos` com `template_id` (válido/inválido); `POST /api/produtos/{id}/renomear` (`almoxarife` sucesso, `usuario` `403`, `id` inexistente `404`, nome incompatível `400`); `GET /api/nomenclatura-templates` `200` com 28 itens para qualquer papel autenticado.
- `backend/main.go` (editar) — registra `GET /api/nomenclatura-templates` e `POST /api/produtos/{id}/renomear`; comentário-doc do pacote menciona a Story 3.2.
- `backend/main_test.go` (editar) — `TestNewMux_ProdutosRenomearRotaCarregaRequireRole` (molde de `TestNewMux_ProdutosRotaCarregaRequireRole`): `usuario` → `403`, `almoxarife` → não-`403` em `POST /api/produtos/{id}/renomear`.
- `frontend/src/components/produtos/CadastroProdutoSection.tsx` (editar) — `Select` de template (novo), texto de apoio com o padrão do template selecionado, `template_id` no payload de `POST /api/produtos`.
- `frontend/src/components/produtos/CadastroProdutoSection.test.tsx` (editar) — carrega e popula o `Select` de template; payload inclui `template_id` quando selecionado e o omite quando não.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000013-000014_*.{up,down}.sql` — `nomenclatura_templates` (+ seed) e `produtos.template_id`.
- `backend/services/nomenclatura.go` (+ teste) — `ListarNomenclaturaTemplates`, `nomeValidoParaTemplate`.
- `backend/services/produtos.go` (+ teste) — `CriarProduto` com validação de template, `AtualizarNomeProduto`, `ErrProdutoNaoEncontrado`.
- `backend/handlers/produtos.go` (+ teste) — `AtualizarNomeProdutoHandler`, `ListarNomenclaturaTemplatesHandler`, `template_id` em `criarProdutoRequest`.
- `backend/main.go` (+ teste) — `GET /api/nomenclatura-templates` (`RequireAuth`); `POST /api/produtos/{id}/renomear` (`RequireRole(almoxarife)`).
- `frontend/src/components/produtos/CadastroProdutoSection.tsx` (+ teste) — `Select` de template + payload.

**Acceptance Criteria:**
- Given a lista de 28 templates de Nomenclatura Guiada (addendum §G), when o Almoxarife seleciona um template ao cadastrar um Produto e preenche `nome` com todos os placeholders na mesma ordem dos tokens, then `201` e `produtos.template_id` grava o template escolhido.
- Given um template selecionado, when o `nome` enviado não preenche algum placeholder do template (ou quebra a ordem/estrutura), then `400 VALIDATION_ERROR` e nenhuma linha é gravada.
- Given nenhum template selecionado, when o Almoxarife informa qualquer `nome`, then `201` sem exigência de estrutura (texto livre, comportamento inalterado da Story 3.1).
- Given um Produto cadastrado com template aplicado, when o Almoxarife renomeia via `POST /api/produtos/{id}/renomear` com um nome que não bate mais com o mesmo template, then `400 VALIDATION_ERROR` e o `nome` gravado permanece o anterior — não é possível burlar a regra editando depois do cadastro.
- Given o mesmo Produto, when o novo nome preenche corretamente o mesmo template aplicado, then `200` e `produtos.nome` é atualizado.
- Given uma sessão de papel `usuario`, when ela chama `POST /api/produtos` ou `POST /api/produtos/{id}/renomear` diretamente pela API, then `403 FORBIDDEN` e nada é gravado.

## Spec Change Log

## Review Triage Log

### 2026-08-30 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 5 (high 1, medium 1, low 3)
- defer: 1
- reject: 11
- addressed_findings:
  - `[high]` `[patch]` `CadastroProdutoSection.tsx` incluía `GET /api/nomenclatura-templates` no mesmo gate `!ok` de `categorias`/`estoques`, então uma falha só no endpoint opcional de templates bloqueava o cadastro inteiro (que não depende dele). Corrigido para degradar silenciosamente (`templates = []`, só a opção "Nome livre") sem acionar `erroCarregar`; teste novo cobre o cenário.
  - `[medium]` `[patch]` `GET /api/nomenclatura-templates` nunca era exercitado pela instância real de `newMux` (só por um mux local em `handlers/produtos_test.go`) — um `RequireRole` indevido adicionado à rota (bloqueando `usuario`, que o formulário consulta) passaria despercebido pela suíte. Adicionado subteste em `TestNewMux_ProdutosRotaCarregaRequireRole` (molde do caso já existente para `GET /api/categorias`).
  - `[low]` `[patch]` `nomeValidoParaTemplate` usava `.` sem a flag `(?s)`; RE2 (Go regexp) não casa `\n` com `.` por padrão, então um `nome` com quebra de linha embutida num placeholder (válido pela checagem básica de `CriarProduto`, que não proíbe `\n`) seria rejeitado como "não corresponde ao template" mesmo preenchendo tudo corretamente. Adicionada a flag `(?s)` + teste.
  - `[low]` `[patch]` Doc comment de `CriarProduto` não mencionava a validação de Nomenclatura Guiada acrescentada por esta story nem que o `INSERT` em `produtos` agora também grava `template_id` — atualizado.
  - `[low]` `[patch]` Faltava teste cobrindo o fluxo de voltar o `<Select>` de template para "Nome livre (sem template)" depois de selecionar um (limpa `templateId`, some o texto de formato, `template_id` sai do payload). O comportamento já estava correto no código (`onValueChange` já traduzia o sentinel de volta para `''`); teste adicionado fechando a lacuna de cobertura.

### 2026-08-30 — Review pass (follow-up)
- intent_gap: 0
- bad_spec: 0
- patch: 1 (high 1, medium 0, low 0)
- defer: 0
- reject: 12
- addressed_findings:
  - `[high]` `[patch]` `CadastroProdutoSection.tsx`: o patch `high` da passada anterior isolou só o caso em que `GET /api/nomenclatura-templates` resolve com status não-`ok`, mas o `fetch` continuava dentro do MESMO `Promise.all` de `categorias`/`estoques` — se a chamada de templates rejeitasse (falha de rede isolada nesse endpoint, sem afetar os outros dois), `Promise.all` rejeitava por inteiro e o formulário inteiro ficava bloqueado (`erroCarregar`), incluindo categoria/estoque que tinham carregado com sucesso — reproduzindo, por um caminho diferente, o mesmo defeito que o patch anterior pretendia eliminar. Corrigido movendo a busca de templates para fora do `Promise.all`, com seu próprio `try/catch` isolado; teste novo cobre o `fetch` rejeitando (distinto do teste existente, que só cobre resposta resolvida com `ok:false`). Achado confirmado de forma independente pelo Edge Case Hunter e pelo Verification Gap Reviewer (mesma reivindicação e mesma ação corretiva — deduplicados).

### 2026-08-30 — Review pass (follow-up 2)
- intent_gap: 0
- bad_spec: 0
- patch: 5 (high 0, medium 0, low 5)
- defer: 0
- reject: 12
- addressed_findings:
  - `[low]` `[patch]` `AtualizarNomeProdutoHandler` (`backend/handlers/produtos.go`) não tinha nenhum teste cobrindo o corpo JSON malformado — a suíte só exercitava corpos bem-formados. Adicionado `TestAtualizarNomeProdutoHandler_400PayloadInvalido` (mesmo molde de `TestCriarProdutoHandler_400PayloadInvalido`), provando `400 VALIDATION_ERROR` e que o `nome` gravado não muda.
  - `[low]` `[patch]` O texto de apoio "Formato: ..." abaixo do campo Nome (`CadastroProdutoSection.tsx`) não tinha vínculo semântico com o `Input` — usuários de leitor de tela não recebiam a orientação de formato ao focar o campo. Adicionado `aria-describedby="produto-nome-formato"` no `Input` (condicional a haver template selecionado) e `id="produto-nome-formato"` no parágrafo do texto de apoio.
  - `[low]` `[patch]` Faltava cobertura para o reset pós-cadastro quando um template estava selecionado no momento do submit — o comportamento (`limparFormulario` já zera `templateId`) estava correto, mas nenhum teste provava que o texto de formato some e o `<Select>` volta a "Nome livre (sem template)" após o sucesso. Estendido o teste `cadastro com template selecionado: mostra o formato e envia template_id no payload` com essas duas asserções pós-`toastSuccess`.
  - `[low]` `[patch]` `TestListarNomenclaturaTemplatesHandler_200PorQualquerPapel` só checava `len(templates) == 28`, sem provar a ordenação por `subtipo` nem que `id`/`subtipo`/`template` chegam não-vazios na fronteira HTTP (só a camada de serviço tinha essa cobertura). Estendido o teste para validar campos não-vazios e ordem ascendente de `subtipo` em cada resposta.
  - `[low]` `[patch]` A linha "Cadastro com `template_id` inexistente" da I/O Matrix (UUID válido sem linha em `nomenclatura_templates` → `400 VALIDATION_ERROR` "template selecionado não existe") só tinha prova na camada de serviço (`TestCriarProduto_TemplateInexistente`), não na fronteira HTTP — o teste `400` existente para `POST /api/produtos` cobre um erro diferente (nome incompatível com o template, não template inexistente). Adicionado `TestCriarProdutoHandler_400TemplateInexistente` fechando essa lacuna no mesmo boundary que a matriz descreve.

## Design Notes

- **Endpoint de edição restrito a `nome`, sem tela de "editar Produto":** a AC3 exige revalidação ao editar, mas nenhum épico do roadmap (checado epics.md, EXPERIENCE.md e ARCHITECTURE-SPINE.md) planeja uma tela geral de edição de Produto — nem Epic 4 (Catálogo, cuja Story 4.4 é só visualização em tempo real). A leitura mais estreita e não-fantasiada é entregar a capacidade que a AC pede (revalidar nome contra o template no servidor, de forma que "editar depois" não burle a regra) como um endpoint dedicado (`POST /api/produtos/{id}/renomear`), testável via API — sem inventar uma tela de edição que nenhum documento de planejamento descreve. Uma futura tela de edição geral (se/quando um épico a especificar) reaproveitaria esta função de serviço.
- **`/renomear` em vez de `PUT`/`PATCH`:** o projeto não usa hoje nenhum desses métodos — mutações que não são criação/exclusão simples já seguem o padrão `POST /api/{recurso}/{id}/{ação}` (`/desativacao`, `/rebaixamento`, `/decisao`). Reaproveitar essa convenção evita introduzir um estilo de rota novo só para esta story.
- **Casamento de nome contra template — esqueleto literal case-sensitive:** nenhum documento de planejamento especifica sensibilidade a maiúsculas/minúsculas para o "texto fixo" do template (ex. `CABO`, `PVC`). Exigir a grafia exata do template (sempre maiúscula no addendum §G) é a leitura mais simples e replica a convenção do domínio (identificadores/códigos em caixa alta, ex. SKU em `JetBrains Mono` no frontend). Os placeholders em si aceitam qualquer texto não-vazio.
- **Exemplo do regex de validação** (`nomeValidoParaTemplate`): para o template `CABO [TIPO] [TENSÃO]`, o padrão construído é `^CABO\ (.+?)\ (.+?)$` (literais escapados via `regexp.QuoteMeta`, tokens viram `(.+?)`); `nome = "CABO PP 220V"` casa com grupos `["PP", "220V"]`, ambos não-vazios → válido. `nome = "CABO 220V"` não casa (falta um segmento) → inválido.

## Verification

**Commands:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída de `gofmt`, build/vet limpos.
- `cd backend && go test -p 1 -count=1 ./...` — Postgres real; migrations `000013`-`000014` aplicam sem erro; cobre `nomenclatura_test.go`, `produtos_test.go` (template + renomear) e `main_test.go`.
- `cd frontend && npm run lint && npm run build && npm run test` — `oxlint`, `tsc`+`vite` e os novos testes de `CadastroProdutoSection` passam.
- `docker compose up --build` — logado como `almoxarife`, cadastrar um Produto selecionando um template e preenchendo o nome conforme o formato exibido; tentar `POST /api/produtos/{id}/renomear` (via curl/Postman) com um nome que quebra o template → `400`; com um nome válido → `200`.

**Manual checks (if no CLI):**
- `SELECT count(*) FROM nomenclatura_templates` = 28 logo após a migration `000013`.
- `SELECT template_id FROM produtos WHERE id = '<id>'` reflete o template escolhido no cadastro.

## Auto Run Result

**Resumo:** Story já implementada (`status: done` na dispatch desta run). Esta run executou uma passada de revisão de acompanhamento (`followup_review_recommended: true` herdado das duas passadas anteriores), aplicou 5 patches `low` de cobertura de teste e acessibilidade, e reconfirmou verificação completa.

**Arquivos alterados nesta passada:**
- `backend/handlers/produtos_test.go` — 2 testes novos (`TestAtualizarNomeProdutoHandler_400PayloadInvalido`, `TestCriarProdutoHandler_400TemplateInexistente`) + `TestListarNomenclaturaTemplatesHandler_200PorQualquerPapel` estendido (ordenação + campos não-vazios na fronteira HTTP).
- `frontend/src/components/produtos/CadastroProdutoSection.tsx` — `aria-describedby` ligando o `Input` de Nome ao texto de apoio "Formato: ...".
- `frontend/src/components/produtos/CadastroProdutoSection.test.tsx` — teste de template estendido com asserções pós-reset (texto de formato some, `<Select>` volta a "Nome livre (sem template)").

**Findings desta passada:** 17 achados brutos (Blind Hunter 13, Edge Case Hunter 1, Verification Gap Reviewer 1 nota informal, Intent Alignment Auditor 2 divergências descritivas) → 5 `patch` (todos `low`), 0 `defer`, 12 `reject`. 0 `intent_gap`, 0 `bad_spec`. Rejeitados por serem: decisões de design já registradas e explícitas no Design Notes/`<intent-contract>` (case-sensitivity, ausência de validação de formato no cliente), consistentes com convenção já existente no resto da base (seed sem `ON CONFLICT`, decoder sem `DisallowUnknownFields`), impossíveis dado uma garantia do próprio contrato (linhas de `nomenclatura_templates` nunca são excluídas, então a "corrida" de FK do Edge Case Hunter não ocorre na prática), especulativos sobre templates futuros que não existem hoje, ou duplicata do item já registrado em `deferred` (handle `subtipo` para a Story 3.7).

**Follow-up review recommendation:** `true`. Score desta passada: 5 patches `low`, 0 `medium`, 0 `high` → `3×0 + 1×5 = 5` (≥ 5).

**Verificação executada:**
- `cd backend && gofmt -l . && go build ./... && go vet ./...` — sem saída, limpo.
- `cd backend && go test -p 1 -count=1 ./...` — todos os pacotes `ok` (Postgres real).
- `cd frontend && npm run lint` — `oxlint` sem erros.
- `cd frontend && npm run build` — `tsc -b && vite build` limpo.
- `cd frontend && npm run test` — suíte completa: 25 arquivos, 228 testes, todos passando.
- `docker compose up --build` (checagem manual via UI) — **não executado** nesta run (execução não-assistida, sem navegador); coberto indiretamente pelos testes de integração HTTP (`handlers/produtos_test.go`) e pelos testes de componente (`CadastroProdutoSection.test.tsx`), que exercitam os mesmos caminhos.

**Riscos residuais:**
- O item já registrado em `deferred` (handle `subtipo` texto-livre para a futura Story 3.7) permanece em aberto — fora do escopo desta story, nenhuma ação nova necessária.
- Checagem manual via `docker compose up --build` não foi executada nesta passada (ambiente não-assistido); recomenda-se uma passagem manual antes de expor a funcionalidade a usuários finais, embora a cobertura automatizada já exercite os mesmos fluxos.

**Nota sobre o working copy:** `_bmad-output/implementation-artifacts/deferred-work.md` e `_bmad-output/implementation-artifacts/sprint-status.yaml` permanecem modificados e não commitados ao final desta run — por instrução explícita do disparo desta run, esses dois arquivos são propriedade do orquestrador (ledger de deferred-work e board de sprint-status) e não devem ser escritos, revertidos ou commitados por esta run. Todos os demais arquivos revisados nesta passada (código + este spec) foram commitados.

