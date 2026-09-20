---
title: 'Story 10.4: Colunas explícitas na listagem do Catálogo'
type: 'feature'
created: '2026-09-20'
status: 'blocked'
baseline_revision: 'ff11f57a19deed3ee0c9d8f1d3b8ea79fad99c40'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** Hoje a listagem do Catálogo (grade em `CatalogoListagem.tsx:509-527` e tabela agrupada em `:529-563`) não expõe código, categoria, estoque total e embalagem+unidade como campos próprios em nenhum dos dois modos — a grade mostra só nome/código+categoria/disponibilidade, a tabela agrupada só nome/dimensões/quantidade/disponibilidade — obrigando o Usuário a abrir o detalhe do Produto para ver o essencial (FR-49/epic-10-context.md).

**Approach:** Adicionar código, categoria, estoque total e embalagem+unidade como colunas/campos próprios nos dois modos de listagem (grade e tabela agrupada), reaproveitando dados já persistidos e já calculados — sem introduzir cálculo novo além da soma de estoque entre Estoques, que já existe.

## Boundaries & Constraints

**Always:**
- Card de grade e linha de tabela agrupada devem mostrar código, nome, categoria, estoque total e embalagem+unidade como campos/colunas próprias, sem exigir abrir o detalhe (epics.md:1567,1571).
- Produto sem Embalagem preenchida mostra traço/vazio na coluna, nunca quebra o layout (epics.md:1573-1575).

**Block If:**
- **Dependência dura não satisfeita — Story 10.3 ainda não implementada.** `unidade_medida` e `embalagem` não existem como colunas em `produtos` nem em nenhuma migration (`backend/migrations/`, confirmado por busca exaustiva) — são responsabilidade exclusiva da Story 10.3 ("Código do Fornecedor, EAN-13, Unidade de Medida e Embalagem"), que está em `backlog` em `sprint-status.yaml` (`10-3-código-do-fornecedor-ean-13-unidade-de-medida-e-embalagem: backlog`), sem spec e sem nenhuma linha de código escrita. O próprio `epic-10-context.md:49` é explícito: "Story 10.4 consome diretamente os campos persistidos pela Story 10.3 [...] não introduz cálculo próprio além da soma de estoque entre Estoques" — ou seja, esta story está proibida de criar essas colunas ela mesma; ela só pode exibir o que 10.3 já tiver persistido. Sem 10.3, não há embalagem/unidade para exibir.
- **Ambiguidade de agregação não resolvida pelo intent.** `CatalogoGrupo` (`backend/services/catalogo.go:71-79`) agrupa por `nome` + as 5 dimensões estruturadas — deliberadamente, uma linha de grupo pode conter vários Produtos DISTINTOS com categorias diferentes (comentário do próprio componente, `CatalogoListagem.tsx:26-29`: "um grupo pode conter vários Produtos distintos (mesmo nome+dimensões, categorias diferentes)"). Uma vez que Categoria (e, quando 10.3 existir, código, embalagem e unidade) podem divergir DENTRO de um mesmo grupo, não há no intent (epics.md Story 10.4, epic-10-context.md) nenhuma regra de desempate para o que mostrar na coluna de categoria/código/embalagem+unidade da linha agrupada quando os Produtos do grupo discordam nesses campos. Leituras defensáveis e mutuamente exclusivas, todas compatíveis com o texto da AC ("as mesmas colunas aparecem como colunas próprias da tabela"): (a) mostrar o valor do primeiro Produto do grupo por alguma ordem arbitrária; (b) mostrar "Múltiplos"/traço quando os Produtos do grupo divergem; (c) listar todos os valores distintos concatenados; (d) mudar a chave de agrupamento para incluir categoria (e futuramente código/embalagem/unidade), fragmentando grupos que hoje são únicos. Cada uma produz um comportamento observável diferente e nenhuma decorre do texto do épico — não é uma decisão que esta automação pode tomar sem fantasiar.

**Never:** Esta story não deve criar `unidade_medida`/`embalagem`/qualquer coluna nova em `produtos` — isso é escopo exclusivo da Story 10.3 (epic-10-context.md:49).

</intent-contract>

## Code Map

- `frontend/src/components/catalogo/CatalogoListagem.tsx:77-89` (`CatalogoItem`) -- já tem `codigo`, `categoria`, `quantidadeTotal`; falta `embalagem`/`unidadeMedida` (dependem da Story 10.3 no backend).
- `frontend/src/components/catalogo/CatalogoListagem.tsx:509-527` (card de grade) -- hoje só renderiza `nome`, `codigo`+`categoria.nome` e `IndicadorDisponibilidade`; falta `quantidadeTotal` (já vem na API, só não é exibido na grade) e embalagem+unidade (não existem ainda).
- `frontend/src/components/catalogo/CatalogoListagem.tsx:529-563` (tabela agrupada, colunas do `<thead>`) -- hoje só tem Produto/Dimensões/Quantidade/Disponibilidade; faltam código/categoria/embalagem+unidade — ver ambiguidade de agregação acima antes de adicionar.
- `backend/services/catalogo.go:49-58` (`CatalogoItem`) -- já expõe `Codigo *string`, `Categoria Categoria`, `QuantidadeTotal float64`; sem campos de embalagem/unidade (Story 10.3).
- `backend/services/catalogo.go:71-79` (`CatalogoGrupo`) -- agrupa por nome+dimensões; sem `Codigo`/`Categoria` hoje — adicionar exigiria antes resolver a ambiguidade de agregação acima.

## Auto Run Result

Status: blocked
Blocking condition: dependência dura não satisfeita (Story 10.3 ainda em `backlog`, sem as colunas `unidade_medida`/`embalagem` no schema — esta story está proibida de criá-las, per `epic-10-context.md:49`) somada a uma ambiguidade de agregação não resolvida no intent (que valor de categoria/código/embalagem+unidade mostrar numa linha da tabela agrupada quando ela cobre Produtos distintos com esses campos divergentes — `CatalogoGrupo` agrupa só por nome+dimensões, `backend/services/catalogo.go:71-79`). Nenhuma das duas é uma decisão que esta automação pode tomar sem fantasiar ou sem invadir o escopo da Story 10.3. Recomendação: (1) rodar/concluir a Story 10.3 primeiro; (2) ao replanejar 10.4, decidir explicitamente (PM/arquitetura) a regra de desempate de categoria/código/embalagem+unidade na tabela agrupada quando um grupo cobre mais de um Produto distinto.
