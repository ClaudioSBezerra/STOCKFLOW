# Epic 12 Context: Estrutura Organizacional — Filiais e Centro de Custo

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

O Adm estrutura a organização da Empresa em Filiais, com os Estoques (Depósitos) agrupados por Filial, e cadastra Centros de Custo/Destinos de Obra como lista padronizada referenciável no envio de Pedido. O Ambiente de Treinamento passa a vir com fotos de exemplo nos Produtos semeados, para parecer um catálogo real. O objetivo é refletir a organização física e financeira real do cliente sem quebrar dados nem fluxos já em produção.

## Stories

- Story 12.1: Cadastro de Filiais e vínculo de Estoque
- Story 12.2: Migração dos Estoques legados para Filial padrão
- Story 12.3: Cadastro de Centro de Custo e Destino de Obra
- Story 12.4: Fotos de exemplo no Ambiente de Treinamento

## Requirements & Constraints

- Filial pertence a exatamente uma Empresa; todo Estoque novo exige uma Filial. "Depósito" e "Estoque" são o mesmo conceito: Filial é só o pai do Estoque, sem terceiro nível de hierarquia.
- Filial é hierarquia interna, não unidade de isolamento de dados (isso continua sendo só a Empresa).
- Nome de Estoque é único dentro da Filial (case/espaço-insensitivo, garantido atomicamente), não mais na Empresa toda; Filiais diferentes podem ter Estoques de mesmo nome.
- Somente `adm`+ cadastra Filial e Centro de Custo; papéis abaixo recebem 403 pela API.
- Toda Empresa nova recebe uma Filial padrão (nome = Nome Fantasia) automaticamente, sem passo extra de onboarding.
- Centro de Custo é cópia por Empresa, como Categorias/Templates/Filiais. O campo texto livre de obra/centro de custo do Pedido continua obrigatório e inalterado; o novo campo estruturado é opcional e coexiste com ele, sem migração retroativa de Pedidos históricos. Tornar o estruturado obrigatório fica adiado (decisão de produto futura).
- Fotos de exemplo: valem só para Ambientes de Treinamento provisionados a partir de agora, sem reseed automático dos existentes. Conteúdo e quantidade exatos do conjunto de Produtos/fotos são decisão de conteúdo/UX a definir com o Adm.
- Migração/seed de dados em produção é sempre disparada manualmente por uma pessoa, nunca por agente autônomo.

## Technical Decisions

- Nova tabela `filiais` (`id` UUID, `empresa_id`, `nome`, `criado_em`); `estoques` ganha `filial_id` referenciando `filiais`.
- Migração aditiva no molde do isolamento por Empresa: `filial_id` nasce `NULL`able, backfill de uma Filial padrão única por Empresa existente (~11 Estoques reais da Ferreira Costa), só depois `SET NOT NULL`. Idempotente: Estoques já vinculados não são reprocessados. Reorganizar Estoques em Filiais reais é trabalho manual posterior, fora do escopo.
- O índice único de nome de Estoque muda de `(empresa_id, nome_normalizado)` para `(filial_id, nome_normalizado)`.
- A Filial padrão é criada na MESMA transação de `ProvisionarEmpresa` (Story 9.2), junto com a primeira linha de `contadores_produto`. Nenhum lazy-init.
- Nova tabela `centros_custo` (`id`, `empresa_id`, `nome`); `pedidos.centro_custo_id UUID NULL` referencia `centros_custo`. O `centro_custo_id` vindo do cliente é sempre revalidado contra a Empresa do contexto; `empresa_id` nunca vem de body/query.
- Toda tabela de domínio carrega `empresa_id` e toda query de service filtra por ele; o slug da URL resolve a Empresa no middleware.
- Handlers previstos: `handlers/filiais.go` e `handlers/estoques.go`; pedidos em `handlers/pedidos.go`.
- Fotos de exemplo reaproveitam as rotas normais de upload (Story 3.5) e o armazenamento versionado em volume de disco já existente (arquivo `<produto_id>-<timestamp>.jpg`, nunca overwrite). Sem storage novo. O seed é um script one-off, não parte do runtime.
- Ambiente de Treinamento é tratado como uma Empresa comum (`empresa_origem_id` preenchido), sem condicionais `eh_treinamento` em services.

## UX & Interaction Patterns

- Navegação é gated por papel: itens que o Usuário não pode acessar somem, nunca aparecem desabilitados nem como "acesso negado".
- Filiais, Centros de Custo (e Categorias/Templates) podem ser apresentados como abas/seções de uma única área administrativa. O agrupamento de tela é decisão de UX, não de capacidade.

## Cross-Story Dependencies

- 12.2 depende do schema e das regras de 12.1 (a migração precisa da coluna `filial_id` e da Filial padrão); 12.1 e 12.2 devem fechar juntas antes que qualquer Estoque exista sem Filial.
- 12.1 estende o cadastro de Estoque (Story 2.1) e o provisionamento de Empresa (Story 9.2); a unicidade de nome do Estoque muda para `(filial_id, nome_normalizado)`.
- 12.3 estende o envio de Pedido (Epic 7) com o campo opcional `centro_custo_id`; segue a mesma cópia por Empresa de Categorias/Templates (Epic 10).
- 12.4 depende do provisionamento do Ambiente de Treinamento (Story 9.2) e do upload de fotos (Story 3.5).
- A exclusão de Estoque (bloqueada se houver Lotes, Epic 11) não é alterada por este epic, mas Estoques agora sempre têm Filial.
