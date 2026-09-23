# Epic 13 Context: Edição de Produto

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

O Almoxarife corrige um Produto já cadastrado sem precisar cadastrar outro. Surgiu dos testes reais de treinamento: a única rota de edição existente (`/renomear`, só nome) nunca teve tela, e o PRD prevê edição com revalidação do nome contra o template de Nomenclatura.

## Stories

- Story 13.1: Editar um Produto já cadastrado

## Requirements & Constraints

- Edição por `almoxarife`+; `usuario` recebe 403 na API e nem vê o botão "Editar".
- Produto de outra Empresa ou inexistente responde 404, sem revelar existência (AD-20).
- Nome revalidado contra o template escolhido (mínimo 10 caracteres, formato do template, "Genérico" aceita qualquer nome); erro mostra o formato esperado.
- Produto legado sem template pode ser editado sem escolher template; ao escolher um, o nome é revalidado.
- Validação no servidor: EAN-13 com dígito verificador, dimensão com valor+unidade, unidade dentro da lista; falha não grava nada.
- Código (automático, Story 10.2), saldo/Lotes, reservas e histórico de Movimentações não são editáveis nem alterados.
- Edição bem-sucedida publica evento `produtos` (`updated`) para outras sessões.

## Technical Decisions

- Toda query filtra por `empresa_id` do contexto (slug da URL); `empresa_id` nunca vem de body/query.
- Reaproveitar as regras de validação do cadastro (Stories 3.1, 10.1, 10.3) e a rota existente `/renomear`.

## Cross-Story Dependencies

- Depende do cadastro de Produto (3.1/3.2), template obrigatório (10.1), código automático (10.2), campos de fornecedor/EAN/unidade/embalagem (10.3) e do desacoplamento de estoque do cadastro (11.6).
