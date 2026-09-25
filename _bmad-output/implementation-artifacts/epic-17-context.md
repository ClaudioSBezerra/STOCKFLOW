# Epic 17 Context: Novo visual — menu lateral agrupado e páginas de lista

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Adotar o padrão visual aprovado pelos sócios (referência `ux-designs/ux-stockflow-2026-08-29/referencias/modelo-tela.png`): menu lateral escuro com os nomes das telas agrupados, cada tela com rota própria, e páginas de lista com busca, filtros, indicadores e tabela limpa. No celular o menu abre pelo ☰. Nenhuma regra de negócio muda: é só apresentação e navegação, com os mesmos papéis e as mesmas rotas de API.

## Stories

- Story 17.1: Menu lateral agrupado e topo novo
- Story 17.2: Páginas próprias para Cadastros e Administração
- Story 17.3: Padrão de página de lista aplicado ao Catálogo
- Story 17.4: Pedidos no padrão de lista
- Story 17.5: Locais, Movimentações e Usuários no padrão de lista

## Requirements & Constraints

- Substitui o rail de ícones, as abas por módulo, o submenu vertical e a bottom nav; ao final nenhuma tela pode usá-los. Testes de navegação cobrem desktop, menu recolhido e celular.
- Cada item de menu é rota própria, abrível por link direto. Item sem permissão some (nunca desabilitado); grupo sem item visível some inteiro. O servidor continua sendo a autoridade de permissão (rota aberta sem papel mostra a recusa de hoje).
- Links antigos (ex. `/configuracoes`, e-mails, favoritos) continuam funcionando.
- Gate de MFA: com a navegação bloqueada, o menu segue visível e toda rota leva a `/configuracoes`.
- `/configuracoes` passa a ser "Meu perfil": só dados da conta, minha dupla autenticação, solicitar promoção e privacidade.
- Comportamentos existentes preservados: aprovação item a item, recibo PDF, `fab-scanner` (Catálogo e Carrinho), busca por código, grade do Catálogo como alternativa, `cart-badge` (vai para o item Carrinho do menu).

## Technical Decisions

- Indicadores das listas vêm de rota irmã `GET .../indicadores` (ex. `GET /e/{slug}/api/produtos/indicadores`) com o mesmo papel mínimo e exatamente os mesmos parâmetros de busca/filtro; o service monta o `WHERE` pela mesma função da listagem, escopado por `empresa_id`, só ativos (`inativado_em IS NULL`) salvo filtro explícito de inativos (gestor+).
- "Sem foto" vem de uma única listagem do diretório de fotos por requisição (vínculo `<produto_id>-*.jpg`), nunca uma chamada por Produto.
- Nunca contar indicador no navegador sobre a página visível. Se a rota de indicadores falhar, a tela funciona e a faixa mostra "—".
- Estado de grupos abertos/fechados e menu recolhido guardados em `localStorage` por pessoa, apenas como conveniência (try/catch; sem ele, padrão aberto).
- Componentes reutilizáveis do padrão de lista (título, barra de busca/filtros, faixa de indicadores, tabela, pílula de status), com testes próprios, criados na 17.3 e reusados em 17.4 e 17.5.

## UX & Interaction Patterns

- Menu lateral azul-marinho 240px (recolhido 64px só com ícones de grupo, tooltip e painel flutuante ao clicar). Marca do ambiente no topo ("stockflow" ou "Suprimentos"); "Meu perfil" no rodapé. Item ativo: negrito branco, fundo destacado, barra vermelha FC de 3px à esquerda, `aria-current="page"`; grupo do item ativo abre sozinho.
- Acessibilidade: `nav` com `aria-label="Menu principal"`; cada grupo é botão com `aria-expanded`.
- Grupos e rotas (papel mínimo): Catálogo (Produtos `/` usuario; Cadastrar produto `/produtos/novo` e Importar planilha `/produtos/importar` almoxarife; Carrinho `/carrinho` usuario); Pedidos (Meus pedidos `/pedidos` usuario; Fila `/pedidos/fila` almoxarife); Estoque (Locais `/estoques`, Lançar saldo `/estoques/lancar-saldo`, Movimentações `/estoques/movimentacoes`, almoxarife); Qualidade dos dados (Inconsistências `/normalizacao`, Duplicatas `/normalizacao/duplicatas`, almoxarife); Cadastros (Categorias, Templates, Filiais, Centros de custo em `/cadastros/*`, adm); Administração (Usuários `/admin/usuarios`, Convites `/admin/convites`, Promoções `/admin/promocoes` gestor; Segurança `/admin/seguranca`, Log de acesso `/admin/log-acesso`, LGPD `/admin/lgpd` adm).
- Topo branco 56px: à direita Treinamento (se aplicável), Ajuda e menu da conta (Meu perfil, Sair).
- Celular (<768px): sem bottom nav nem menu fixo; ☰ abre gaveta esquerda com os mesmos grupos, fecha ao escolher item, `Esc` ou toque fora; foco vai para a gaveta e volta ao ☰.
- Página de lista: título; busca + "Adicionar filtro" + "Limpar filtros", filtros ativos em pílulas removíveis; faixa de indicadores (rótulo cinza, valor grande; alerta em vermelho FC quando > 0) com ações (Exportar, Cadastrar) à direita.
  - Indicadores: Catálogo (Itens, Com saldo, Sem foto); Pedidos (Pendentes, Aprovados no mês, Rejeitados no mês); Movimentações (Baixas, Transferências); Locais (Locais, Itens em estoque); Usuários (Ativos, Sem MFA, alerta só se a Empresa exige MFA).
- Tabela: linhas de 60px, divisor fino, sem card nem zebra; 1ª coluna com ícone redondo 32px, nome e subtítulo cinza ("Categoria • código"); números à direita; status em pílula suave (inclui `status-inativo`); no celular vira lista de linhas. Estados de carregando, vazio e erro seguem os padrões existentes.

## Cross-Story Dependencies

- 17.1 cria as rotas próprias (`/produtos/novo`, `/produtos/importar`, `/pedidos/fila`, `/estoques/lancar-saldo`, `/estoques/movimentacoes`, `/normalizacao/duplicatas`) que 17.4 e 17.5 usam; 17.2 completa as rotas de Cadastros/Administração listadas no menu (17.5 depende de `/admin/usuarios`).
- 17.3 entrega os componentes do padrão de lista e a base de indicadores (AD-38) exigidos por 17.4 e 17.5.
- Depende do Epic 16 (Produto inativo, filtro de inativos e status `inativo`).
