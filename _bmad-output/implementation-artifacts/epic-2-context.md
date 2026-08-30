# Epic 2 Context: Gestão de Estoques

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Este épico estabelece o domínio de Estoques — os locais físicos (canteiros, almoxarifados) onde os Produtos são armazenados — como fundação referencial para os épicos seguintes (Cadastro/Importação de Produtos, Movimentação, Pedidos). Entrega o ciclo mínimo de criação, listagem e exclusão de Estoques, com nome único garantido atomicamente e exclusão protegida contra perda de rastreabilidade (estoque residual ou Pedido pendente). Inclui a migração dos Estoques do sistema legado para o schema novo. Importa porque todo o catálogo e todo o fluxo de estoque referenciam Estoques: sem integridade referencial aqui, saldos órfãos ao excluir um local e nomes duplicados — bugs reais do protótipo atual — se propagam para o resto do sistema.

## Stories

- Story 2.1: Criar e listar locais de Estoque
- Story 2.2: Exclusão de Estoque trata resíduos e pedidos pendentes
- Story 2.3: Migração dos Estoques legados

## Requirements & Constraints

- Criar e excluir Estoque é restrito a `almoxarife` ou acima; a decisão de autorização é feita no servidor e retorna 403 para papéis abaixo mesmo em chamada direta à API. Listar Estoques é liberado a qualquer Usuário autenticado e retorna nome + id de cada Estoque.
- Nome de Estoque é único, com comparação insensível a maiúsculas/minúsculas e a espaçamento (valor normalizado). A unicidade é garantida atomicamente inclusive sob requisições concorrentes; a colisão retorna 409 (`CONFLICT`).
- A exclusão de Estoque é bloqueada quando: (a) há quantidade residual de algum Produto no Estoque — a resposta lista quais Produtos ainda têm quantidade ali; (b) há Pedido com status `pendente` referenciando o Estoque, mesmo com quantidade zerada. As duas verificações ocorrem na mesma transação da exclusão.
- Os dois guards concretos dependem de tabelas criadas em épicos posteriores (`PRODUTO_ESTOQUE` no Epic 3, `PEDIDOS` no Epic 7). O Epic 2 entrega a exclusão funcional agora; os guards são adicionados pelas stories que criam essas tabelas (3.1 e 7.2), sem reabrir a Story 2.2. O Epic 2 não fica bloqueado esperando os épicos seguintes.
- A exclusão é confirmada por diálogo de confirmação reutilizável (padrão `AlertDialog`, nunca `window.confirm`).
- Migração legada (Story 2.3): script one-off fora do runtime da aplicação, disparado sempre manualmente por uma pessoa, nunca por agente autônomo; corte único ("big-bang"). Cada Estoque legado é recriado com novo UUID v4 preservando o nome, com uma entrada em tabela de mapeamento id-antigo→id-novo para preservar referências. A execução é idempotente: reexecutar não duplica Estoques já migrados. Dois nomes legados equivalentes por maiúsculas/espaços disparam conflito de unicidade detectado e reportado para revisão manual antes do corte — a migração nunca cria duas linhas para o "mesmo" Estoque.
- Mudanças em Estoques são publicadas em tempo real no canal de eventos `estoques`; o cliente rebusca o estado via GET, o payload do evento é mínimo.

## Technical Decisions

- Backend em Go em camadas, pragmático: `handlers/estoques.go` (fronteira HTTP — valida input, serializa resposta, nunca acessa o banco) → `services/` (regra de negócio, transações, filtro de escopo de listagem) → acesso a dados via `database/sql` com SQL explícito. Sem framework web (net/http da stdlib), sem ORM, sem container de injeção de dependência.
- Autorização: a decisão allow/deny fica sempre em middleware, nunca no handler. A hierarquia de papel é ordem total codificada (`adm=4 > gestor=3 > almoxarife=2 > usuario=1`); a checagem de papel mínimo usa comparação de rank, nunca uma allow-list de pares por rota. O papel é lido do Postgres a cada requisição autenticada, sem cache.
- Convenções de schema/API: tabela e colunas em português (`estoques`); UUID v4 como id; `timestamptz` em UTC no banco e ISO 8601 na API; envelope de erro `{"error":{"code","message"}}` com vocabulário fixo de `code` (`VALIDATION_ERROR`, `NOT_FOUND`, `CONFLICT`, `FORBIDDEN`, ...). Logging estruturado via `log/slog`, nunca `fmt.Print`.
- Unicidade de nome deve ser imposta pelo banco — índice único sobre o valor normalizado (lowercase + colapso de espaços), via índice funcional ou coluna dedicada. A concorrência resolve na colisão do índice, não numa sequência "SELECT e depois INSERT" em nível de aplicação.
- Modelo de dados: `ESTOQUES` 1:N `PRODUTO_ESTOQUE` (relação M:N entre Produtos e Estoques carregando a quantidade); `ESTOQUES` 1:N `MOVIMENTACOES` (origem obrigatória, destino nullable); Estoque também aparece como campo em `PEDIDO_ITENS`. As migrations SQL são sequenciais e aplicadas no startup da aplicação.
- Migração: `cmd/migrate-legado` lê diretamente do PostgreSQL espelho do Firestore mantido pela empresa. A tabela de mapeamento id-antigo→id-novo é compartilhada com as demais migrações (Produtos, Movimentações, Pedidos, Usuários) para manter referências consistentes no corte.
- Tempo real: registry de conexões SSE in-process, um canal por domínio (`estoques` entre os quatro canais fixos), envelope de evento fixo `{"resource","id","change"}`, autenticação da conexão via ticket de curta duração obtido por endpoint autenticado.

## UX & Interaction Patterns

- A superfície de Estoques vive no módulo "Estoques" da navegação, com papel mínimo `almoxarife`. Em viewport ≥768px: rail de ícones fixo (56px) + submenu vertical (224px), com sub-abas Locais / Movimentações. Em viewport <768px (a partir de 360px): o módulo fica atrás do item "Mais" (abre um `Sheet`); nenhuma tela pode quebrar o layout abaixo de 360px.
- Excluir Estoque é ação destrutiva e usa `AlertDialog` do shadcn. Toda confirmação de ação assíncrona usa toast com `aria-live="polite"`.
- Quando um evento SSE chega no canal `estoques` com a tela aberta, mostra-se um toast discreto de atualização; a tela nunca recarrega sozinha. Se a reconexão SSE demorar mais que alguns segundos, aparece um indicador persistente "Reconectando...".
- Quando a exclusão é bloqueada por quantidade residual, a resposta enumera os Produtos que ainda têm quantidade no Estoque — mensagem acionável, não erro genérico.
- Vocabulário PT-BR do glossário usado literalmente ("Estoque", nunca sinônimo). Alvo de toque mínimo de 48px em todo elemento interativo.

## Cross-Story Dependencies

- Story 2.1 cria a tabela `estoques` e a regra de unicidade de nome que a Story 2.3 (migração) precisa respeitar e que a Story 3.3 (importação cria Estoques ausentes automaticamente) reutiliza.
- Story 2.2 entrega a exclusão funcional; os guards de "quantidade residual" e "Pedido pendente" são completados por Story 3.1 (cria `PRODUTO_ESTOQUE`) e Story 7.2 (cria `PEDIDOS`), sem reabrir a Story 2.2.
- Depende do Epic 1: middleware de resolução de papel e papel disponível no contexto da requisição (Story 1.5); shell de navegação, componente de confirmação reutilizável e Toaster global (Story 1.2); tabela `usuarios` e sessão (Stories 1.1, 1.4).
- Story 2.3 compartilha o script `cmd/migrate-legado` e a tabela de mapeamento de IDs com as migrações do Epic 3 (Produtos, Categorias, Templates) e demais; o corte é sempre disparado por um humano.
- O canal SSE `estoques` é consumido pelas telas de catálogo e detalhe de Produto (Epic 4) e de Movimentações (Epic 5).
