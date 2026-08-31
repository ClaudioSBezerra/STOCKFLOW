# Epic 3 Context: Cadastro, Importação e Fotos de Produtos

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Este épico entrega os três caminhos pelos quais o Almoxarife popula e mantém o catálogo de Produtos: cadastro manual item a item (com dimensões estruturadas e nomenclatura guiada por subtipo), importação em massa via planilha padronizada (que cria e atualiza em lote, sem duplicar), e anexação de fotos (upload, galeria, lightbox). Importa porque o catálogo nasce vazio na virada para o novo sistema — sem estes fluxos, ninguém consegue popular ou manter os dados que todos os demais épicos (Catálogo, Movimentação, Pedidos, Normalização) consultam e operam sobre. A Story 3.1 também completa o guard de exclusão de Estoque da Story 2.2, criando a primeira linha de `PRODUTO_ESTOQUE` que torna "quantidade residual" verificável. Inclui a migração de Produtos, Categorias, Templates e fotos do sistema legado.

## Stories

- Story 3.1: Cadastro manual de Produto com dimensões estruturadas
- Story 3.2: Nomenclatura Guiada por subtipo
- Story 3.3: Importação em massa via planilha padronizada
- Story 3.4: Importação atualiza por código, não só cria
- Story 3.5: Upload e armazenamento de foto do Produto
- Story 3.6: Galeria e visualização ampliada de fotos (lightbox)
- Story 3.7: Migração de Produtos, Categorias e fotos legadas

## Requirements & Constraints

- Cadastro, importação e upload de foto são restritos a `almoxarife`+; `usuario` chamando esses endpoints diretamente pela API recebe 403 — visualização de catálogo/fotos continua liberada a qualquer Usuário autenticado.
- Cada dimensão do Produto (comprimento, largura, diâmetro, altura, espessura) é um par valor+unidade, nunca texto livre; valor sem unidade (ou vice-versa) rejeita só aquele campo, nunca salva um Produto parcialmente preenchido.
- Categoria é selecionada de uma lista fixa de ~25 categorias (fonte única, não digitável livremente) — hoje duplicada em dois lugares do sistema legado, deve virar tabela/enum única no schema-alvo.
- Nomenclatura Guiada: com um dos 28 templates fixos selecionado, o nome exige preencher todos os placeholders na mesma ordem de tokens, validado no servidor; sem template, texto livre. Editar um Produto com template já aplicado revalida o nome contra esse mesmo template — não dá para burlar a regra editando depois.
- Planilha padronizada (nome, código, categoria, dimensões valor+unidade em colunas separadas, quantidade, estoque, observações). Cabeçalho fora do padrão rejeita a importação inteira antes de processar qualquer linha; linha com dimensão sem unidade é erro só daquela linha, sem travar as demais; Estoques ausentes referenciados são criados automaticamente.
- Reimportar atualiza Produto existente por código (nunca duplica); sem mudança, a linha não aparece como "criado". Linha sem código com nome parecido sempre cria Produto novo — correspondência por nome é escopo da ferramenta de Duplicatas (Epic 6), não do importador. Relatório final discrimina criados/atualizados/rejeitados com CTA para disparar a checagem de Duplicatas. Importação interrompida (rede, navegador fechado) é retomável: reabrir a tela mostra até onde chegou, sem reprocessar linhas já salvas.
- Foto: JPG/PNG/WEBP via câmera ou galeria; sempre redimensionada a 500px no maior lado e comprimida em JPEG q=0.82, mesma regra em cadastro ou edição. Arquivo fora de tamanho/formato é rejeitado com erro específico dizendo qual dos dois. Nova foto nunca sobrescreve a anterior no mesmo caminho.
- Produto com múltiplas fotos expõe galeria navegável (card/detalhe); qualquer foto expande em lightbox de tela cheia; fechar retorna à posição exata de rolagem anterior, sem recarregar a página.
- Migração legada (3.7): dimensões texto-livre convertidas para `{valor, unidade}` por um parser único; casos ambíguos ficam marcados para revisão manual via Normalização (Epic 6). Fotos base64 inline são extraídas, redimensionadas/comprimidas como em 3.5 e salvas em disco — nunca migradas como base64. As 25 Categorias e os 28 Templates são inseridos como seed na primeira execução (fonte única, mesmo duplicados hoje no legado). Execução idempotente; corte sempre disparado manualmente por uma pessoa, nunca por agente autônomo.

## Technical Decisions

- Backend em camadas: `handlers/produtos.go` (fronteira HTTP) → `services/` (regra de negócio, transações, filtro de escopo) → `database/sql` explícito, sem ORM. Autorização decidida em `middleware/` por rank de papel (`adm=4 > gestor=3 > almoxarife=2 > usuario=1`), papel sempre lido do Postgres sem cache.
- Modelo de dados: `PRODUTOS` (dimensões estruturadas, `categoria_id`, `template_id` opcional, `deleted_at` soft-delete), `PRODUTO_ESTOQUE` (M:N Produto↔Estoque com quantidade), `CATEGORIAS` e `NOMENCLATURA_TEMPLATES` (tabelas de seed, fonte única), `IMPORTACOES` + `IMPORTACAO_LINHAS` (suportam retomada de importação). UUID v4 como id; `timestamptz` UTC no banco.
- Fotos sempre em volume Docker nomeado e persistente, nunca base64 no banco; nome de arquivo versionado `<produto_id>-<timestamp_unix>.jpg`. Leitura de Produto sempre filtra `deleted_at IS NULL`.
- Convenções transversais: envelope de erro `{"error":{"code","message"}}` com vocabulário fixo (`VALIDATION_ERROR`, `CONFLICT`, `NOT_FOUND`, `FORBIDDEN`); logging via `log/slog`. Mudanças em Produtos publicam evento mínimo no canal SSE `produtos`, cliente sempre rebusca via GET.
- Migração (`cmd/migrate-legado`): script one-off fora do runtime, lê o espelho Postgres do Firestore legado, reaproveita a tabela de mapeamento id-antigo→id-novo compartilhada com as demais migrações. O parser de dimensão texto-livre→estruturado é usado só aqui, em regime (a planilha já exige colunas separadas).
- As listas literais das 25 Categorias e dos 28 Templates (com placeholders/tokens) já existem nos documentos de planejamento como referência única — consultá-los para popular o seed, não reconstruir a partir do legado.

## UX & Interaction Patterns

- Cadastro e Importação são abas dentro do módulo Catálogo (papel mínimo `almoxarife`, some da navegação para `usuario`, nunca aparece desabilitada). Fotos ficam no detalhe do Produto, visualização liberada a qualquer Usuário.
- Relatório de importação: tabela criados/atualizados/rejeitados com barra de progresso durante o processamento, mais CTA "Verificar duplicatas agora" levando à Normalização com análise já disparada. Importação interrompida mostra banner "Última importação parou na linha N. Continuar de onde parou?" ao reabrir.
- Upload de foto: botão "Adicionar foto" (câmera/galeria) só para `almoxarife`+; `usuario` só visualiza. Galeria navegável; toque abre lightbox em tela cheia; fechar retorna à posição de rolagem exata, sem reload.
- Código de Produto (SKU) e demais identificadores sempre em `JetBrains Mono`, nunca na fonte sans — diferencia "identificador" de texto legível.
- Toda ação assíncrona (foto enviada, importação concluída, produto cadastrado) confirma via toast `aria-live="polite"`; ações destrutivas usam `AlertDialog`, nunca `window.confirm()`. Alvo de toque mínimo 48px; nenhuma tela quebra abaixo de 360px de viewport.

## Cross-Story Dependencies

- Story 3.1 cria `PRODUTO_ESTOQUE`, completando o guard de "quantidade residual" da exclusão de Estoque (Epic 2, Story 2.2) — sem reabrir aquela story.
- Story 3.1 depende da tabela `CATEGORIAS` estar populada (seed geralmente entregue junto com a migração da Story 3.7, ou por seed equivalente antes dela) para o formulário de cadastro funcionar; Story 3.2 tem a mesma dependência em relação a `NOMENCLATURA_TEMPLATES`.
- Story 3.3 depende de Estoques (Epic 2, Story 2.1) para a criação automática de Estoques ausentes durante a importação.
- Story 3.4 estende o pipeline de importação entregue pela Story 3.3 (mesmas tabelas `IMPORTACOES`/`IMPORTACAO_LINHAS`); o CTA do relatório final aponta para a ferramenta de Duplicatas do Epic 6.
- Stories 3.5 e 3.6 dependem de um Produto já existente (Story 3.1 ou 3.3/3.4) para ter onde anexar/exibir fotos.
- Story 3.7 compartilha o script `cmd/migrate-legado` e a tabela de mapeamento de IDs com a migração de Estoques (Epic 2, Story 2.3); casos ambíguos de conversão de dimensão ficam marcados para revisão na Normalização (Epic 6).
- O Catálogo (Epic 4) consulta diretamente `PRODUTOS`/`PRODUTO_ESTOQUE`/fotos criados por este épico; Normalização (Epic 6) opera sobre inconsistências e duplicatas geradas pelo cadastro/importação/migração deste épico.
