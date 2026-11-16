---
title: 'Filial com CNPJ, Inscrição Estadual e endereço'
type: 'feature'
created: '2026-11-16'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'bfb2c8d5bcdb64784ab97fb44bbffe3673acb198'
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-pre-nota-layout-danfe.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Filial só tem nome. A Pré-Nota em layout de DANFE precisa identificar Remetente e Destinatário como estabelecimentos (CNPJ, Inscrição Estadual, endereço), mas esses dados não existem no cadastro — e hoje nem dá pra editar uma Filial depois de criada.

**Approach:** Acrescentar à Filial CNPJ, Inscrição Estadual e endereço completo, todos opcionais (decisão do usuário, 2026-11-16), com criação e EDIÇÃO pela tela de Filiais. Estoques herdam o endereço da Filial (sem endereço próprio). Os quadros REMETENTE/DESTINATÁRIO do PDF da Pré-Nota passam a mostrar os dados da Filial de cada lado, lidos do cadastro atual.

## Boundaries & Constraints

**Always:**
- Campos novos em `filiais`, todos NULL por padrão: `cnpj` CHAR(14), `inscricao_estadual` VARCHAR(20), `logradouro` VARCHAR(255), `numero` VARCHAR(20), `complemento` VARCHAR(255), `bairro` VARCHAR(255), `cidade` VARCHAR(255), `cep` CHAR(8), `uf` CHAR(2) — mesmos tipos de `empresas`.
- CNPJ, quando informado: reaproveitar `ValidarCNPJ`/`NormalizarCNPJ` (14 dígitos + dígito verificador); único por Empresa entre as Filiais (índice único parcial `(empresa_id, cnpj) WHERE cnpj IS NOT NULL`) -> 409 se repetido.
- A raiz do CNPJ da Filial (8 primeiros dígitos) tem que ser igual à raiz do CNPJ da Empresa — senão 400 com mensagem clara (decisão do usuário, 2026-11-16, na revisão).
- Endereço é um bloco: se QUALQUER campo de endereço for preenchido, logradouro, número, bairro, cidade, CEP (8 dígitos) e UF (2 letras, uma das 27 UFs) passam a ser obrigatórios; complemento continua opcional. Tudo vazio = sem endereço.
- Inscrição Estadual: texto livre até 20 caracteres (aceita "ISENTO").
- Edição: `PUT /api/filiais/{id}` (nome + dados fiscais), isolado por Empresa (Filial de outra Empresa/inexistente -> 404). Nome continua obrigatório e único por Empresa, como hoje.
- Permissão (pedido do usuário, 2026-11-16): o cadastro de Filiais passa de `adm` para `gestor`+ — `RequireRole(services.PapelGestor)` no POST e no PUT, item de menu `filiais` com `papelMinimo: 'gestor'` e gate da `FiliaisPage` em `gestor`. A listagem (GET) continua só com RequireAuth. Locais/Estoques já são `almoxarife`+ (o Gestor já tem acesso) — não mudam.
- Tela de Filiais: o formulário de criação ganha os campos novos (opcionais) e cada Filial da lista ganha "Editar" abrindo o mesmo formulário preenchido; a lista mostra CNPJ formatado e Cidade/UF.
- PDF da Pré-Nota: os quadros REMETENTE (ORIGEM) e DESTINATÁRIO (DESTINO) mostram, da Filial de cada lado: Razão social da Empresa, CNPJ da Filial, IE da Filial, endereço da Filial em até 2 linhas, Filial e Estoque. Dado ausente aparece "—" (nunca cai no CNPJ da Empresa, pra não parecer o CNPJ do estabelecimento). Baixa continua "consumo, sem destinatário".
- A aprovação em lote grava em `pedido_itens` QUAL Filial era a de origem e a de destino e o nome do Estoque destino (só identificadores/nome, não os dados fiscais), para uma Pré-Nota antiga não perder Filial/CNPJ/endereço quando um Estoque for excluído depois; os dados fiscais continuam lidos do cadastro atual da Filial. Pré-Notas anteriores sem essa gravação usam o caminho atual (via `estoques.filial_id`).
- Canhoto de recebimento no fim do documento (decisão do usuário, 2026-11-16): quadro "RECEBIMENTO" com campos em branco para Data, Nome legível e Assinatura de quem recebeu o material.

**Ask First:** nenhuma — campos, nível (só Filial) e obrigatoriedade (opcional) decididos pelo usuário em 2026-11-16.

**Never:** endereço próprio no Estoque; tornar os campos obrigatórios; mudar a regra de snapshot da Pré-Nota (dados da Filial são lidos do cadastro atual, como os nomes); excluir Filial (fora de escopo).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Criar só com nome | nome, sem dados fiscais | Filial criada, campos NULL | N/A |
| Editar com tudo válido | CNPJ válido, IE, endereço completo | Dados gravados normalizados (CNPJ/CEP só dígitos, UF maiúscula) | N/A |
| CNPJ inválido | dígito verificador errado | Nada gravado | 400 |
| CNPJ repetido | mesmo CNPJ de outra Filial da mesma Empresa | Nada gravado | 409 |
| CNPJ de outra raiz | CNPJ válido com raiz diferente da Empresa | Nada gravado | 400 |
| Estoque excluído depois | Pré-Nota emitida, depois o Estoque de origem é excluído | PDF continua mostrando Filial, CNPJ e endereço da Filial de origem | N/A |
| Endereço incompleto | só cidade preenchida | Nada gravado | 400 (lista os obrigatórios) |
| Editar Filial de outra Empresa | id de outra Empresa | Nada gravado | 404 |
| Gestor cadastra/edita Filial | papel `gestor` | Filial criada/editada | N/A |
| Almoxarife tenta cadastrar/editar | papel `almoxarife` | Nada gravado | 403 |
| PDF com Filial preenchida | Transferência entre Filiais com CNPJ/endereço | Quadros mostram CNPJ, IE e endereço de cada lado | N/A |
| PDF com Filial vazia | Filial sem dados fiscais | Quadros mostram "—" nesses campos | N/A |

</frozen-after-approval>

## Code Map

- `backend/migrations/000061_*` (nova) -- ALTER TABLE `filiais` com as colunas novas + índice único parcial `(empresa_id, cnpj) WHERE cnpj IS NOT NULL`.
- `backend/services/filiais.go:20` (`Filial`), `:42` (`CriarFilial`), `:66` (`ListarFiliais`) -- struct ganha os campos (endereço no mesmo formato de `EnderecoEmpresa`, empresas.go:64); `CriarFilial` aceita os dados opcionais; nova `AtualizarFilial`. Validação reaproveita `ValidarCNPJ`/`NormalizarCNPJ` e a mesma checagem de CEP/UF de `validarDadosEmpresa` (empresas.go:~288-330).
- `backend/handlers/` + `backend/main.go:543` (rotas de Filiais) -- POST passa a `RequireRole(services.PapelGestor)`; novo `PUT /e/{slug}/api/filiais/{id}` com `RequireAuth` + `RequireRole(services.PapelGestor)`.
- `frontend/src/components/shell/nav-items.ts:119` (item `filiais`, `papelMinimo: 'adm'`) e `frontend/src/pages/cadastros/FiliaisPage.tsx:11` (gate `adm`) -- passar para `gestor`; ajustar o comentário de `FiliaisSection.tsx:11`.
- `frontend/src/components/filiais/FiliaisSection.tsx` + `frontend/src/pages/cadastros/FiliaisPage.tsx` -- campos no formulário, botão Editar por linha, colunas CNPJ e Cidade/UF.
- `backend/services/pre_notas.go` (`MontarPreNotaConteudo` + quadros REMETENTE/DESTINATÁRIO do layout DANFE) -- ler CNPJ/IE/endereço da Filial de origem (via `estoques.filial_id` do estoque de origem) e de destino, e exibir nos quadros.

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/000061_*.up/down.sql` -- colunas fiscais + índice único parcial
- [x] `backend/services/filiais.go` (+ `_test.go`) -- criar/editar com validação; testes das 6 linhas de cadastro da I/O Matrix
- [x] `backend/handlers/` + `backend/main.go` (+ testes) -- `PUT /api/filiais/{id}` (200/400/403/404/409) e teste pelo `newMux` real do RequireRole: gestor passa, almoxarife recebe 403, no POST e no PUT
- [x] `frontend` Filiais (+ testes) -- formulário com campos novos, Editar, colunas novas
- [x] `backend/services/pre_notas.go` (+ `_test.go`) -- dados da Filial nos quadros; testes das 2 linhas de PDF da I/O Matrix; gerar PDF de amostra e conferir visualmente

**Acceptance Criteria:**
- Given uma Filial já existente sem dados fiscais, when o adm abre Editar e preenche CNPJ/IE/endereço, then os dados ficam salvos e passam a aparecer no PDF da Pré-Nota
- Given `go build ./...`, `go test -p 1 ./...`, `npm run build` e `npx vitest run`, when rodados após a story, then terminam sem erro

## Spec Change Log

- **2026-11-16, revisão (renegociação humana + patches):** revisão em triplo do layout DANFE + Filial. O usuário decidiu: exigir a raiz do CNPJ da Filial igual à da Empresa; incluir canhoto de recebimento; CNPJ alfanumérico (Receita, desde 07/2026) fica para uma story separada logo em seguida. Achado do edge-case/blind-hunter: Estoque pode ser excluído e a Pré-Nota antiga perderia a Filial — passa a gravar na aprovação os ids das Filiais e o nome do Estoque destino. Demais achados como patch; itens menores em `deferred-work.md`. **KEEP:** todo o layout DANFE e o cadastro/edição de Filial como estão.

## Verification

**Commands:**
- `cd backend && go build ./... && go test ./services/... -run 'Filial|PreNota'` -- expected: build limpo, testes passando
- `cd backend && go test -p 1 ./...` -- expected: suíte completa limpa
- `cd frontend && npm run build && npx vitest run` -- expected: build limpo, suíte limpa

## Suggested Review Order

**Cadastro da Filial (o ponto central)**

- Criar com dados fiscais opcionais; validação do CNPJ (dígito + raiz igual à da Empresa), CEP, UF e endereço "tudo ou nada"
  [`filiais.go:362`](../../backend/services/filiais.go#L362)

- Editar: PUT parcial (campo ausente mantém o valor, `""` limpa), na mesma transação da leitura
  [`filiais.go:385`](../../backend/services/filiais.go#L385)

- Colunas fiscais da Filial + ids/nome gravados no item na aprovação (Pré-Nota antiga não perde a Filial se o Estoque for excluído)
  [`000061_add_dados_fiscais_filiais.up.sql:1`](../../backend/migrations/000061_add_dados_fiscais_filiais.up.sql#L1)

- Gravação dos ids das Filiais no mesmo UPDATE do snapshot de preço
  [`pedidos.go:1729`](../../backend/services/pedidos.go#L1729)

**Acesso (adm -> gestor)**

- Rota PUT e POST atrás de `RequireRole(PapelGestor)`
  [`filiais.go:80`](../../backend/handlers/filiais.go#L80)
  [`nav-items.ts:119`](../../frontend/src/components/shell/nav-items.ts#L119)

**PDF**

- Conteúdo: Filial de cada lado pelos ids gravados, dados fiscais do cadastro atual, fallback pro caminho antigo
  [`pre_notas.go:120`](../../backend/services/pre_notas.go#L120)

- Layout em funções puras (quadros, rotas por id, canhoto) — é o que os testes conferem
  [`pre_notas.go:580`](../../backend/services/pre_notas.go#L580)

- Desenho: quebra de página, subtotal nunca órfão, rodapé com número da página
  [`pre_notas.go:730`](../../backend/services/pre_notas.go#L730)

**Tela de Filiais**

- Formulário único de criar/editar, tabela com CNPJ e Cidade/UF, confirmação antes de descartar alterações
  [`FiliaisSection.tsx:114`](../../frontend/src/components/filiais/FiliaisSection.tsx#L114)

- Formatadores de CNPJ/CEP compartilhados
  [`formatacao.ts`](../../frontend/src/lib/formatacao.ts)
