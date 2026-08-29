---
stepsCompleted: [1, 1-confirmed, 2, 2-approved, 3, 4-validated]
inputDocuments:
  - _bmad-output/planning-artifacts/prds/prd-stockflow-2026-08-29/prd.md
  - _bmad-output/planning-artifacts/prds/prd-stockflow-2026-08-29/addendum.md
  - _bmad-output/planning-artifacts/architecture/architecture-stockflow-2026-08-29/ARCHITECTURE-SPINE.md
  - _bmad-output/planning-artifacts/ux-designs/ux-stockflow-2026-08-29/DESIGN.md
  - _bmad-output/planning-artifacts/ux-designs/ux-stockflow-2026-08-29/EXPERIENCE.md
---

# stockflow - Epic Breakdown

## Overview

This document provides the complete epic and story breakdown for stockflow, decomposing the requirements from the PRD, UX Design, and Architecture into implementable stories.

## Requirements Inventory

### Functional Requirements

FR1: Usuário autentica com e-mail/senha para acessar qualquer funcionalidade. Sessão expira após 2h de inatividade; nenhum endpoint responde sem token válido, exceto login e cadastro.
FR2: Backend valida o papel do Usuário (`usuario < almoxarife < gestor < adm`) em cada endpoint sensível, nunca só na interface. Ação de `almoxarife`+ chamada por `usuario` retorna 403 mesmo via chamada direta à API.
FR3: Qualquer pessoa cria conta pública (nome, e-mail, senha), sempre como `usuario`. E-mail único garantido atomicamente; confirmação de e-mail obrigatória antes do primeiro login; backend nunca aceita papel diferente vindo do formulário.
FR4: Busca de Produto por nome/código/categoria com autocomplete (até 7 sugestões, ordenado por relevância).
FR5: Filtros de Produto por categoria, estoque e disponibilidade, combináveis simultaneamente.
FR6: Visualização do catálogo em grade e em tabela agrupada (soma quantidades de produtos com mesmo nome/unidade/dimensões).
FR7: Detalhe do Produto mostrando quantidade discriminada por Estoque.
FR8: Cadastro manual de Produto (nome, código, categoria, dimensões estruturadas valor+unidade, estoque destino, quantidade inicial, observações, foto opcional), restrito a `almoxarife`+.
FR9: Nomenclatura Guiada por subtipo de material (templates de nome), validada no servidor; edição de um Produto com template aplicado revalida o nome contra o template.
FR10: Importação em massa de Produtos via planilha padronizada; cria Estoques ausentes automaticamente; cabeçalho fora do padrão rejeita a importação inteira antes de processar qualquer linha.
FR11: Reimportação de planilha atualiza Produto existente por código em vez de sempre criar um novo; relatório final discrimina criados/atualizados/rejeitados.
FR12: Criar e listar locais de Estoque, com nome único garantido atomicamente.
FR13: Exclusão de Estoque bloqueada se houver quantidade residual ou Pedido `pendente` referenciando o Estoque.
FR14: Registrar Baixa (consumo) de estoque; rejeita quantidade zero ou negativa; restrito a `almoxarife`+.
FR15: Registrar Transferência entre Estoques; rejeita origem=destino e quantidade maior que a disponível; checagem e débito atômicos.
FR16: Histórico de Movimentações consultável (produto, tipo, origem, destino, quantidade, autor, data).
FR17: Detecção de inconsistências dimensionais em Produtos (dimensão em texto livre residual, valor sem unidade, etc.).
FR18: Aplicação seletiva de correções de Normalização (individual, em lote por produto, ou em lote geral); "ignorar" é permanente por valor específico, reaparecendo se o campo mudar para um novo valor inconsistente.
FR19: Detecção de Produtos duplicados por nome normalizado + dimensões equivalentes + locais coincidentes.
FR20: Mesclagem de duplicatas com trilha de auditoria permanente (quem, quando, produtos removidos, valores); item em Carrinho/Pedido pendente é redirecionado ao produto mantido.
FR21: Carrinho de reserva de itens antes de enviar um Pedido; valida disponibilidade no momento da adição.
FR22: Envio de Pedido de Retirada (solicitante, obra/centro de custo, observação) → status `pendente`; rejeita carrinho vazio; revalida disponibilidade no envio.
FR23: Consulta de Pedidos próprios, filtrável por status.
FR24: Consulta de todos os Pedidos por `almoxarife`+; um Usuário sem esse papel vê só os próprios Pedidos (escopo, não erro).
FR25: Aprovação/rejeição de Pedido com revalidação de estoque item a item; nunca sucesso parcial silencioso; débito e Movimentação atômicos.
FR26: Recibo do Pedido gerado como PDF no servidor (itens, quantidades, estoques de origem, solicitante, aprovador, data), sob demanda via endpoint dedicado.
FR27: Upload de foto de Produto (JPG/PNG/WEBP) com regra única de resolução (500px maior lado) e compressão (JPEG q=0.82), independente do fluxo.
FR28: Armazenamento de fotos de Produto fora do banco relacional (volume/serviço de objetos dedicado).
FR29: Galeria de fotos do Produto com visualização ampliada (lightbox) a partir do card ou do detalhe.
FR30: Exportação da tabela do catálogo para Excel (.xlsx) com totais e subtotais dinâmicos por grupo/filtro ativo.
FR31: Desativação e rebaixamento de conta de Usuário — `adm` age sobre qualquer conta, `gestor` só sobre `almoxarife`/`usuario`; conta desativada não autentica mais (senha ou SSO); rebaixamento perde acesso já na próxima requisição.
FR32: Recuperação de senha por e-mail (link/código de uso único, expira em prazo curto); mensagem genérica para e-mail inexistente; conta que só usava SSO pode criar senha própria pela primeira vez por este fluxo.
FR33: Solicitação de promoção de papel para o nível imediatamente acima; decidida por `gestor`/`adm` (promoção a `gestor` só por `adm`); decisão sempre registra quem decidiu e quando.
FR34: Login federado via Keycloak (SSO Ferreira Costa) como alternativa ao login por senha; nunca cria conta nova (busca por e-mail, case-insensitive); exige `email_verified=true`; papel do Usuário continua definido dentro do stockflow, não pelo Keycloak; login por senha continua sendo o caminho padrão visível, sem redirecionamento automático; RP-initiated logout ao encerrar sessão SSO.
FR35: Identificação de Produto via leitura de QR Code/código de barras (câmera do celular), reaproveitando o Código de Identificação já cadastrado (FR8); leitura abre o detalhe do Produto ou adiciona ao Carrinho conforme o contexto.
FR36: Bloqueio temporário de conta após tentativas de login malsucedidas consecutivas; política de senha mínima no cadastro/redefinição; não afeta o caminho de login via SSO.
FR37: MFA (segundo fator TOTP) obrigatório para contas `gestor`/`adm` autenticadas por senha antes de liberar ações restritas a esses papéis; login via SSO herda o MFA já imposto pelo realm Keycloak corporativo.
FR38: Log de acesso e auditoria (todo login, sucesso ou falha, com usuário quando identificável, timestamp, IP, método), append-only, consultável por `adm`.
FR39: Exportação dos próprios dados pessoais por qualquer Usuário; `adm` pode processar exclusão/anonimização de dados pessoais de uma conta, preservando o vínculo de Histórico/Pedidos já registrado.

### NonFunctional Requirements

NFR1: Toda autorização por papel é validada no servidor; nenhuma credencial (própria ou do Keycloak) é exposta no cliente/repositório; toda entrada é validada no limite da API.
NFR2: Proteção contra força bruta (FR36) e MFA para papéis administrativos (FR37) fazem parte da postura de segurança padrão do sistema.
NFR3: Log de acesso auditável e append-only (FR38).
NFR4: Conformidade com LGPD para dados pessoais de conta de Usuário — exportação e exclusão/anonimização sob solicitação (FR39).
NFR5: Observabilidade — ações hoje silenciosas no protótipo (skip de item sem estoque, erros de fundo) passam a ser logadas estruturadamente e comunicadas na interface.
NFR6: Confiabilidade — operações em lote não travam a UI nem deixam o catálogo em estado parcial sem indicação de progresso; importação interrompida permite saber quais linhas já foram gravadas e retomar sem duplicar.
NFR7: Concorrência — toda escrita dependente de estado lido previamente é atômica no servidor (saldo de estoque, unicidade de código/e-mail/nome de estoque, aprovação concorrente de Pedido).
NFR8: Desempenho — busca/listagem do catálogo ≤300ms p95 sob carga típica (até 8.000 produtos, 30 Estoques).
NFR9: Usabilidade em campo — interface responsiva funcional em viewport a partir de 360px de largura, testada em navegadores móveis padrão (Chrome Android, Safari iOS); não é opcional, é requisito central dado o uso predominante em campo pelas personas primárias.

### Additional Requirements

- Paradigma de arquitetura: Layered Go pragmático (handlers → services → acesso a dados via `database/sql`), sem framework web e sem ORM — ratificado do projeto de referência `FB_APU02`, não o Hexagonal/Clean Architecture do PRD-fonte original (Architecture AD-1).
- Sem Redis nem RabbitMQ em nenhuma decisão de infraestrutura deste projeto (Architecture AD-2) — diverge deliberadamente do `FB_APU02` (que mantém Redis não utilizado) e do PRD-fonte original.
- Atualização em tempo real via broadcaster in-process + SSE, 4 canais fixos (`produtos`, `estoques`, `movimentacoes`, `pedidos`), envelope de evento fixo (`resource`/`id`/`change`), autenticação da conexão via ticket de curta duração (30s, uso único) obtido por endpoint autenticado normalmente (Architecture AD-3).
- E-mail transacional assíncrono via tabela de outbox no Postgres (`emails_pendentes`, schema fixo com `tipo` enum) + worker por polling — nunca chamada SMTP síncrona no handler HTTP (Architecture AD-4).
- Papel do Usuário sempre lido diretamente do Postgres a cada requisição autenticada, sem cache (Architecture AD-5) — garante que revogação/rebaixamento (FR31) valha já na próxima requisição.
- Modelo de sessão: JWT de acesso curto (30min, `golang-jwt/jwt/v5`) + refresh token rotativo em cookie `HttpOnly` com TTL de 2h (Architecture AD-6) — mesmo formato de token emitido tanto pelo login por senha (FR1) quanto pela troca de token do SSO (FR34).
- Integração Keycloak SSO: pacote `iam/` dedicado, JWKS cache em memória (TTL 1h), validação RS256 via `kid`, `iss`=URL do realm, `azp` (não `aud`) contra allowlist, endpoint de troca de token, endpoint de config runtime (não build-time) — replica o padrão já em produção no `FB_APU02` com desvios deliberados documentados (Architecture AD-7).
- Autorização por papel: hierarquia como ordem total codificada (`adm=4 > gestor=3 > almoxarife=2 > usuario=1`), decisão (papel mínimo/comparação relativa) sempre em middleware, filtro de escopo em listagem sempre em service (Architecture AD-8).
- Dimensões de Produto sempre `{valor, unidade}` estruturado, nunca string livre, em todo o schema/validação/normalização (Architecture AD-9).
- Concorrência de estoque: `SELECT ... FOR UPDATE` em toda escrita de `produto_estoque.quantidade`, sem exceção — toda escrita gera uma Movimentação correspondente; ordem canônica de lock `(produto_id, estoque_id)` ascendente, aplicada ao lote inteiro antes de adquirir qualquer lock (Architecture AD-10).
- Fotos de Produto em volume Docker nomeado e persistente, nome de arquivo versionado (`<produto_id>-<timestamp_unix>.jpg`, nunca overwrite em path fixo); soft-delete via `deleted_at IS NULL`; mesclagem de duplicatas (FR20) reescreve `produto_id` em `MOVIMENTACOES`/`PEDIDO_ITENS` do produto removido para o sobrevivente antes do soft-delete (Architecture AD-11).
- Bootstrap do primeiro Adm via comando CLI dedicado (`cmd/seed-admin`), nunca endpoint HTTP (Architecture AD-12).
- Topologia de deployment: Docker Compose single-host, padrão `installer/cliente-aws` do `FB_APU02` — serviços `api` (Go), `web` (React+Nginx), `db` (`postgres:15-alpine`); sem Redis/RabbitMQ; volume nomeado persistente para fotos (Architecture AD-13).
- Convenções: tabelas/colunas do schema em português (nomes já estabelecidos pelo protótipo/PRD); UUID v4 em toda tabela nova; `timestamptz` UTC; e-mail sempre normalizado para minúsculas com índice único sobre o valor normalizado; envelope de erro HTTP `{"error":{"code","message"}}` com vocabulário fixo de `code` para autenticação/sessão; logging via `log/slog` estruturado (Architecture AD-14).
- Migração de dados legados: script one-off (`cmd/migrate-legado`, fora do runtime), lê diretamente do PostgreSQL espelho do Firestore mantido pela empresa; converte dimensões texto-livre; gera UUIDs novos com tabela de mapeamento id-antigo→id-novo; popula `NOMENCLATURA_TEMPLATES` (28 seeds) e `CATEGORIAS` (25 seeds); corte único, sempre disparado por humano, nunca por agente autônomo (Architecture AD-15).
- Envelope operacional: ambientes local (Compose) + produção (servidor dedicado Ferreira Costa); segredos via `.env` não versionado; backup `pg_dump` diário; observabilidade via Prometheus + Grafana; CI/CD via GitHub Actions no padrão do `FB_APU02` (Architecture AD-16).
- Recibo PDF (FR26) sempre renderizado a partir do snapshot já capturado em `PEDIDO_ITENS` no momento da decisão do Pedido — nunca um join ao vivo com `PRODUTOS` (Architecture AD-17).
- `TOKENS_ACAO` (verificação de e-mail FR3, redefinição de senha FR32) tipado por coluna `tipo` (enum) e de uso único — validação sempre filtra por token+usuario_id+tipo+não expirado+não usado (Architecture AD-18).
- Stack pinado nesta rodada (Architecture): Go 1.27, PostgreSQL 15, React 19.2.x, TypeScript 7.0.x, Vite 8.0.x, React Router 6.x, TanStack Query 5.x, shadcn/ui + Tailwind CSS, `golang-jwt/jwt` v5, `signintech/gopdf` (FR26), `qax-os/excelize` v2.11.0 (FR30) — Go/React/Vite/TypeScript atualizados deliberadamente além das versões do `FB_APU02` por estarem sem suporte de segurança ou muitas majors atrás.
- Biblioteca TOTP para MFA (FR37) e endereço/DNS de deploy em Ferreira Costa permanecem em aberto (Architecture Deferred) — a resolver durante a implementação das stories correspondentes, não bloqueiam a criação dos épicos/stories.

### UX Design Requirements

UX-DR1: Herdar por completo os tokens de design shadcn/ui + Tailwind do `FB_APU02` — cores (`primary` #E62019, `accent`/`success` #16A249, `destructive` #EF4343, `warning` #F59F0A, `info` #0DA2E7, `background`/`foreground`/`card`/`secondary`/`border` inalterados), tipografia (Inter para interface geral, JetBrains Mono para todo código/SKU/UUID/valor de QR lido), escala de radius (sm 8px/md 10px/lg-DEFAULT 12px/full 9999px).
UX-DR2: Definir 4 tokens de cor `text-on-tint-*` mais escuros (success #166534, warning #92400E, info #075985, destructive #B91C1C) usados exclusivamente como texto de badge sobre fundo tintado a 10% da mesma cor, para atingir contraste WCAG AA (corrige falha medida de 1.97–3.19:1 herdada do `FB_APU02`).
UX-DR3: Definir tokens de espaçamento: rail 56px, bottom nav 56px, FAB 56px de diâmetro com margem de 16px e offset de 72px sobre a bottom nav em mobile, submenu vertical 224px, alvo de toque mínimo 48px.
UX-DR4: Componente `fab-scanner` — botão circular flutuante (cor `primary`, radius `full`, `shadow-lg`), presente só nas superfícies de Catálogo e Carrinho, nunca em telas administrativas.
UX-DR5: Componente `cart-badge` — contador circular sobre o ícone de Carrinho, preenchimento sólido `destructive`, desaparece por completo quando o Carrinho está vazio (nunca mostra "0").
UX-DR6: Componentes de badge de status (`status-pendente`, `status-aprovado`, `status-rejeitado`, `status-disponivel`) — formato pill, sempre ícone + texto (nunca só cor), texto sempre na variante `text-on-tint-*` correspondente.
UX-DR7: Componente `nav-item-active` — fundo `primary` a 10% de opacidade, texto/ícone `primary` sólido, idêntico ao padrão já em produção no `FB_APU02`.
UX-DR8: Adotar `AlertDialog` (shadcn) para toda ação destrutiva/irreversível (rejeitar Pedido, mesclar/excluir Produto, desativar conta) — correção deliberada do anti-padrão `window.confirm()` nativo identificado no `FB_APU02`.
UX-DR9: Padronizar 100% no vocabulário shadcn moderno (`Card`, `Table`, `Skeleton`, `Toast`) desde a primeira tela — não repetir a mistura de estilos ("shadcn moderno" vs. "Tailwind cru") encontrada em páginas mais antigas do `FB_APU02`.
UX-DR10: Todo badge de status/disponibilidade carrega sempre ícone + texto, nunca só cor.
UX-DR11: Toda confirmação de ação assíncrona (adicionar/remover do Carrinho, aprovar/rejeitar Pedido, importação concluída, foto enviada, não só atualização em tempo real) usa toast com `aria-live="polite"`.
UX-DR12: Alvo de toque mínimo de 48px em todo elemento interativo do fluxo de campo — acima do piso WCAG de 44px, calibrado para uso com luvas ou uma mão só.
UX-DR13: Busca por texto/Código de Identificação digitado ou colado manualmente sempre disponível como alternativa ao scanner de QR Code/código de barras — o scanner nunca é dependência obrigatória de nenhum fluxo (cobre falha de câmera, permissão negada, reflexo de sol).
UX-DR14: Viewport mínimo de 360px — nenhuma tela do fluxo principal (busca, carrinho, aprovação, leitura de QR) pode quebrar layout abaixo disso.
UX-DR15: Shell desktop/tablet (≥768px): rail de ícones fixo (56px) + header fino + barra de abas horizontal por módulo, ou submenu vertical (224px) para módulos densos (Estoques, Normalização) — espelha o `FB_APU02`.
UX-DR16: Shell mobile (<768px, a partir de 360px): rail vira bottom nav (56px) com Catálogo/Carrinho/Pedidos/Mais; Estoques/Normalização/Relatórios/Configurações completas ficam atrás do item "Mais" (`Sheet`).
UX-DR17: Indicador de atualização em tempo real — toast discreto (`aria-live="polite"`) na chegada de evento SSE em qualquer um dos 4 canais, nunca recarrega a tela sozinho.
UX-DR18: Indicador de reconexão SSE lenta — indicador discreto e persistente ("Reconectando...") se a reconexão levar mais que alguns segundos; reconexão rápida permanece silenciosa.
UX-DR19: Padrão de linha de aprovação item-a-item na Fila de Pedidos — mostra "Solicitado: X · Disponível: Y" em caso de divergência; nunca um botão único "Aprovar tudo" que esconde a divergência.
UX-DR20: Padrão de relatório de importação — tabela criados/atualizados/rejeitados + CTA "Verificar duplicatas agora" com deep-link para Normalização já com a análise disparada.
UX-DR21: Câmera do scanner exige contexto seguro (HTTPS) — funcionalidade indisponível em ambiente de desenvolvimento local sem HTTPS.
UX-DR22: Tela de configuração de MFA (TOTP) — forçada e não pulável para `gestor`/`adm` autenticados por senha; opcional para `usuario`/`almoxarife`; nunca exibida no caminho de login via SSO.
UX-DR23: Padrão de galeria/lightbox de fotos de Produto — toque abre lightbox em tela cheia, fechar retorna à posição exata da rolagem anterior; upload restrito a `almoxarife`+, visualização liberada a `usuario`.

### FR Coverage Map

FR1: Epic 1 - Login por e-mail e senha
FR2: Epic 1 - Autorização por papel aplicada no servidor
FR3: Epic 1 - Autocadastro público, sempre como usuario
FR4: Epic 4 - Busca por nome/código/categoria com sugestões
FR5: Epic 4 - Filtros por categoria, estoque e disponibilidade
FR6: Epic 4 - Visualização em grade e tabela agrupada
FR7: Epic 4 - Detalhe do produto por local de estoque
FR8: Epic 3 - Cadastro manual de Produto
FR9: Epic 3 - Nomenclatura Guiada por subtipo
FR10: Epic 3 - Importação em massa via planilha padronizada
FR11: Epic 3 - Importação atualiza por código, não só cria
FR12: Epic 2 - Criar e listar locais de Estoque
FR13: Epic 2 - Exclusão de Estoque trata resíduos e pedidos pendentes
FR14: Epic 5 - Registrar Baixa (consumo)
FR15: Epic 5 - Registrar Transferência entre Estoques
FR16: Epic 5 - Histórico de Movimentações consultável
FR17: Epic 6 - Detecção de inconsistências dimensionais
FR18: Epic 6 - Aplicação seletiva de correções
FR19: Epic 6 - Detecção de duplicatas
FR20: Epic 6 - Mesclagem de duplicatas com trilha de auditoria
FR21: Epic 7 - Carrinho de reserva
FR22: Epic 7 - Envio de Pedido
FR23: Epic 7 - Consulta de Pedidos próprios
FR24: Epic 7 - Consulta de todos os Pedidos (Almoxarife+)
FR25: Epic 7 - Aprovação/rejeição com revalidação de estoque item a item
FR26: Epic 7 - Recibo do Pedido em PDF gerado pelo servidor
FR27: Epic 3 - Upload de foto com regra única de tamanho/compressão
FR28: Epic 3 - Armazenamento de fotos fora do banco relacional
FR29: Epic 3 - Galeria e visualização ampliada (lightbox)
FR30: Epic 4 - Exportação da tabela do catálogo para Excel
FR31: Epic 1 - Desativação e rebaixamento de conta
FR32: Epic 1 - Recuperação de senha por e-mail
FR33: Epic 1 - Solicitação de promoção de papel
FR34: Epic 1 - Login federado via Keycloak (SSO Ferreira Costa)
FR35: Epic 4 - Identificação de Produto via QR Code / código de barras
FR36: Epic 1 - Bloqueio de conta e política de senha
FR37: Epic 1 - MFA obrigatório para papéis administrativos
FR38: Epic 1 - Log de acesso e auditoria
FR39: Epic 8 - Exportação e exclusão de dados pessoais (LGPD)

## Epic List

### Epic 1: Autenticação e Gestão de Acesso
Qualquer pessoa cria conta, entra com segurança (senha ou SSO corporativo Ferreira Costa), a organização controla quem acessa o quê, e contas administrativas ficam protegidas por segundo fator.
**FRs covered:** FR1, FR2, FR3, FR31, FR32, FR33, FR34, FR36, FR37, FR38

### Epic 2: Gestão de Estoques
Almoxarife organiza os locais físicos de estoque com integridade referencial.
**FRs covered:** FR12, FR13

### Epic 3: Cadastro, Importação e Fotos de Produtos
Almoxarife popula e mantém o catálogo — manual, em lote via planilha, ou com fotos.
**FRs covered:** FR8, FR9, FR10, FR11, FR27, FR28, FR29

### Epic 4: Catálogo — Consulta, Descoberta e Exportação
Qualquer Usuário encontra material disponível, vê onde está (inclusive via QR Code/código de barras), e exporta o catálogo.
**FRs covered:** FR4, FR5, FR6, FR7, FR30, FR35

### Epic 5: Movimentação de Estoque
Almoxarife registra saída e transferência de estoque com histórico consultável.
**FRs covered:** FR14, FR15, FR16

### Epic 6: Normalização de Dados
Almoxarife mantém o catálogo limpo (inconsistências e duplicatas) sem trabalho manual item a item.
**FRs covered:** FR17, FR18, FR19, FR20

### Epic 7: Pedidos de Retirada
Usuário solicita, almoxarife aprova com estoque real, recibo em PDF — ciclo completo.
**FRs covered:** FR21, FR22, FR23, FR24, FR25, FR26

### Epic 8: Privacidade e Conformidade (LGPD)
Usuário exporta os próprios dados pessoais; Adm processa solicitações de exclusão/anonimização — cobrindo identidade, log de acesso, Movimentações e Pedidos já existentes nos épicos anteriores.
**FRs covered:** FR39

## Epic 1: Autenticação e Gestão de Acesso

Qualquer pessoa cria conta, entra com segurança (senha ou SSO corporativo Ferreira Costa), a organização controla quem acessa o quê, e contas administrativas ficam protegidas por segundo fator.

### Story 1.1: Bootstrap do primeiro Adm e fundação do backend

As a operador de infraestrutura,
I want provisionar o primeiro usuário Adm via linha de comando,
So that o sistema tenha um ponto de entrada seguro sem depender de um endpoint HTTP de auto-promoção.

**Acceptance Criteria:**

**Given** a tabela `usuarios` inexistente
**When** as migrations SQL são aplicadas no startup da aplicação
**Then** a tabela `usuarios` é criada com colunas `id` (UUID v4), `nome`, `email` (único, normalizado para minúsculas — AD-14), `senha_hash` (nullable, para contas só-SSO), `papel` (enum `usuario`/`almoxarife`/`gestor`/`adm`), `email_verificado` (bool), `ativo` (bool), `criado_em`

**Given** o banco de dados com o schema migrado e nenhum Adm ainda cadastrado
**When** o operador executa `cmd/seed-admin` (AD-12) informando nome, e-mail e senha
**Then** uma conta é criada com papel `adm`, senha hasheada (bcrypt), e-mail normalizado
**And** não existe nenhum endpoint HTTP equivalente a este comando

**Given** já existe uma conta com papel `adm`
**When** o operador tenta rodar `cmd/seed-admin` novamente
**Then** o comando falha com uma mensagem clara, sem alterar a conta existente

### Story 1.2: Fundação do shell de navegação e design tokens

As a qualquer Usuário autenticado,
I want uma interface consistente com a identidade visual da Ferreira Costa, responsiva entre desktop e celular,
So that eu reconheça o produto como parte da mesma família de ferramentas internas e consiga usá-lo em campo.

**Acceptance Criteria:**

**Given** o frontend React inicializado com Vite + TypeScript (Architecture Stack)
**When** os tokens do `DESIGN.md` são aplicados via configuração Tailwind/shadcn
**Then** a paleta `primary`/`accent`/`destructive`/`warning`/`info` e as variantes `text-on-tint-*` ficam disponíveis (UX-DR1, UX-DR2), com Inter como fonte padrão, JetBrains Mono disponível para código/identificador, e os tokens de espaçamento (rail 56px, bottom nav 56px, FAB 56px, submenu 224px, alvo de toque 48px) definidos globalmente (UX-DR3)

**Given** um usuário autenticado em viewport ≥768px
**When** ele acessa qualquer tela do shell
**Then** vê o rail de ícones fixo (56px) + header fino + barra de abas horizontal ou submenu vertical de 224px conforme o módulo, com tooltip ao passar o mouse sobre um ícone (UX-DR15)
**And** o item de navegação correspondente à tela atual usa o estilo `nav-item-active` (fundo `primary` a 10% de opacidade, texto/ícone `primary` sólido — UX-DR7)

**Given** um usuário autenticado em viewport <768px (a partir de 360px)
**When** ele acessa qualquer tela do shell
**Then** vê a bottom nav (56px) com os itens visíveis, com itens administrativos atrás do item "Mais" (`Sheet`) (UX-DR16), e nenhuma tela do fluxo principal quebra o layout abaixo de 360px (UX-DR14)

**Given** a necessidade de confirmar qualquer ação destrutiva em stories futuras
**When** este shell é finalizado
**Then** um componente `ConfirmDialog` reutilizável (usando `AlertDialog` do shadcn, nunca `window.confirm()`) e o `Toaster` global (`sonner`, `aria-live="polite"`) já estão disponíveis para qualquer story consumir — padronizando 100% no vocabulário shadcn moderno (`Card`, `Table`, `Skeleton`, `Toast`, `AlertDialog`) desde a primeira tela, sem repetir a mistura de estilos do `FB_APU02` (UX-DR8, UX-DR9, UX-DR11)

**Given** qualquer elemento interativo do fluxo de campo (botões, itens de lista, controles de formulário)
**When** ele é implementado em qualquer story futura
**Then** o alvo de toque mínimo é 48px (`{spacing.touch-target-min}`), acima do piso WCAG de 44px, calibrado para uso com luvas ou uma mão só (UX-DR12) — regra global estabelecida por esta story, aplicada por toda story subsequente

### Story 1.3: Autocadastro com verificação de e-mail

As a colaborador sem conta no stockflow,
I want criar minha própria conta informando nome, e-mail e senha,
So that eu possa acessar o catálogo como `usuario` assim que confirmar meu e-mail.

**Acceptance Criteria:**

**Given** a tela pública de Autocadastro
**When** o usuário informa nome, e-mail e senha e envia o formulário
**Then** uma conta é criada com papel `usuario` (nunca aceito do formulário, mesmo se enviado), e-mail normalizado, `email_verificado=false`
**And** um registro é inserido em `emails_pendentes` (`tipo=verificacao_conta`, AD-4) na mesma transação
**And** um token é gerado em `TOKENS_ACAO` (`tipo=verificacao_email`, uso único — AD-18)

**Given** um e-mail já cadastrado
**When** o usuário tenta se cadastrar com o mesmo e-mail
**Then** o sistema responde 409 e a tela mostra "Este e-mail já está cadastrado."

**Given** uma conta com `email_verificado=false`
**When** o usuário tenta fazer login antes de confirmar
**Then** o login é recusado com mensagem orientando a verificar o e-mail

**Given** um link de verificação válido e não expirado
**When** o usuário clica no link
**Then** `email_verificado` passa a `true` e o token é marcado como usado (nunca reutilizável)

### Story 1.4: Login por e-mail e senha

As a Usuário com conta confirmada,
I want entrar com e-mail e senha,
So that eu acesse as funcionalidades do meu papel.

**Acceptance Criteria:**

**Given** uma conta ativa, com e-mail verificado e senha correta
**When** o usuário submete a tela de Login
**Then** um JWT de acesso (30min) é emitido e um refresh token rotativo é definido em cookie `HttpOnly` com TTL de 2h (AD-6)

**Given** uma sessão sem atividade por mais de 2h
**When** o usuário tenta usar o refresh token expirado
**Then** a sessão é encerrada e é necessário logar novamente

**Given** credenciais inválidas
**When** o usuário submete o formulário
**Then** o sistema responde com mensagem genérica (não revela se o e-mail existe)

**Given** qualquer endpoint autenticado
**When** uma requisição chega sem token válido
**Then** a resposta é 401, exceto nas rotas de login e cadastro

### Story 1.5: Autorização por papel aplicada no servidor

As a Adm/Gestor da organização,
I want que toda ação sensível seja checada no servidor, não só escondida na interface,
So that nenhum Usuário execute uma ação além do seu papel mesmo chamando a API diretamente.

**Acceptance Criteria:**

**Given** um Usuário autenticado com papel `usuario`
**When** ele chama diretamente um endpoint restrito a `almoxarife`+
**Then** a resposta é 403, independente do que a interface mostra

**Given** a hierarquia de papel como ordem total `adm=4 > gestor=3 > almoxarife=2 > usuario=1` (AD-8)
**When** o middleware avalia uma rota com papel mínimo exigido
**Then** a checagem usa essa fórmula, nunca uma allow-list de pares reimplementada por rota

**Given** uma rota que precisará de filtro de escopo em listagem (ex. FR-24, Epic 7)
**When** o middleware resolve o papel do Usuário autenticado
**Then** esse papel fica disponível no contexto da requisição para o service aplicar o filtro, sem re-consultar o banco

### Story 1.6: Recuperação de senha por e-mail

As a Usuário que esqueceu a senha,
I want solicitar redefinição por e-mail,
So that eu recupere o acesso sem depender de suporte.

**Acceptance Criteria:**

**Given** um e-mail informado na tela "Esqueci minha senha"
**When** o usuário envia o formulário
**Then** o sistema sempre responde "Se o e-mail existir, você receberá um link.", exista ou não a conta
**And**, se a conta existir, um token é gerado em `TOKENS_ACAO` (`tipo=redefinicao_senha`, expira em 30min, uso único) e um registro é inserido em `emails_pendentes` na mesma transação

**Given** um link de redefinição válido e não expirado
**When** o usuário define uma nova senha (mínimo 8 caracteres, com letra e número)
**Then** a senha é atualizada, o token é marcado como usado, e todas as sessões ativas da conta são revogadas

**Given** uma conta que hoje só tem login via SSO (`senha_hash` nulo)
**When** o usuário usa este fluxo pela primeira vez
**Then** uma senha própria é criada e a conta passa a ter os dois caminhos de login disponíveis

**Given** um link expirado ou já usado
**When** o usuário tenta acessá-lo
**Then** a tela explica o motivo e oferece gerar um novo link

### Story 1.7: Solicitação de promoção de papel

As a Usuário ou Almoxarife,
I want solicitar promoção para o papel imediatamente acima,
So that eu ganhe acesso às funcionalidades que preciso, mediante aprovação.

**Acceptance Criteria:**

**Given** um Usuário sem solicitação pendente
**When** ele clica "Solicitar promoção" em Meu Perfil
**Then** uma `SOLICITACOES_PROMOCAO` é criada com o papel imediatamente acima como alvo, status `pendente`

**Given** uma solicitação pendente para promoção a `almoxarife`
**When** um `gestor` ou `adm` a aprova
**Then** o papel do Usuário muda imediatamente (não espera a sessão expirar) e a decisão registra quem decidiu e quando

**Given** uma solicitação de promoção a `gestor`
**When** qualquer papel diferente de `adm` tenta decidir
**Then** a resposta é 403

**Given** uma solicitação já pendente para um Usuário
**When** o mesmo Usuário tenta criar outra
**Then** o sistema rejeita — só uma solicitação pendente por vez

**Given** uma solicitação rejeitada
**When** o Usuário tenta solicitar novamente
**Then** é permitido, sem período de espera

### Story 1.8: Gestão de contas — desativação e rebaixamento

As a Gestor ou Adm,
I want desativar ou rebaixar contas dentro do meu escopo de autoridade,
So that eu controle quem tem acesso ao sistema.

**Acceptance Criteria:**

**Given** um `gestor` na tela de Gestão de Usuários
**When** ele lista as contas
**Then** só vê contas com papel `almoxarife` ou `usuario` (nunca `gestor`/`adm`)

**Given** uma conta `almoxarife` sendo desativada por um `gestor`
**When** a ação é confirmada via `ConfirmDialog` (Story 1.2)
**Then** a conta não autentica mais (senha ou SSO) e qualquer sessão ativa é encerrada na próxima requisição

**Given** um `gestor` tentando agir sobre outro `gestor` ou um `adm`
**When** ele tenta desativar/rebaixar essa conta
**Then** a resposta é 403

**Given** um `adm` agindo sobre qualquer conta, inclusive `gestor`
**When** ele desativa ou rebaixa
**Then** a ação é permitida

### Story 1.9: Login federado via Keycloak — SSO Ferreira Costa

As a colaborador com conta corporativa Ferreira Costa,
I want entrar no stockflow com a mesma identidade usada em outros sistemas internos,
So that eu não precise lembrar de outra senha.

**Acceptance Criteria:**

**Given** a tela de Login
**When** ela carrega
**Then** e-mail/senha aparece como caminho padrão visível e o botão "Entrar com Ferreira Costa" aparece como opção adicional — nunca redirecionamento automático

**Given** um Usuário completando a autenticação no Keycloak (realm `ferreiracosta`) e retornando com um token válido
**When** o backend processa a troca
**Then** valida assinatura RS256 via JWKS, `iss`, e `azp` contra allowlist, exige `email_verified=true` (AD-7), busca o Usuário por e-mail normalizado (case-insensitive) sem criar conta nova, e emite os mesmos tokens de sessão da Story 1.4

**Given** um e-mail do Keycloak sem conta local correspondente
**When** o backend não encontra o Usuário
**Then** o login é recusado com mensagem orientando a se cadastrar primeiro (Story 1.3)

**Given** um e-mail encontrado mas `email_verified=false` no token
**When** o backend processa a troca
**Then** o login é recusado com mensagem distinta, orientando a confirmar o e-mail corporativo

**Given** uma sessão iniciada via SSO
**When** o Usuário faz logout
**Then** a sessão local é encerrada e o RP-initiated logout do Keycloak é disparado

### Story 1.10: Bloqueio de conta e política de senha

As a Adm responsável pela segurança do sistema,
I want que o login por senha resista a força bruta e exija senha minimamente robusta,
So that contas não sejam comprometidas por adivinhação de senha.

**Acceptance Criteria:**

**Given** 5 tentativas de login malsucedidas consecutivas para a mesma conta
**When** a 6ª tentativa chega
**Then** a conta fica bloqueada por 15 minutos, sem revelar o tempo exato restante na mensagem

**Given** uma conta bloqueada
**When** o Usuário tenta "Esqueci minha senha" (Story 1.6)
**Then** o fluxo continua disponível — bloqueio de tentativas não afeta redefinição

**Given** o cadastro (Story 1.3) ou a redefinição (Story 1.6) de uma senha
**When** a senha tem menos de 8 caracteres ou não contém letra e número
**Then** o sistema rejeita com mensagem explicando o critério

**Given** o login via SSO (Story 1.9)
**When** há tentativas malsucedidas de senha para a mesma conta
**Then** o caminho SSO não é afetado

### Story 1.11: MFA obrigatório para papéis administrativos

As a Gestor ou Adm autenticado por senha,
I want configurar um segundo fator (TOTP),
So that minha conta administrativa tenha uma camada extra de proteção.

**Acceptance Criteria:**

**Given** uma conta `gestor`/`adm` autenticada por senha sem MFA configurado
**When** ela tenta acessar qualquer ação restrita ao seu papel
**Then** é redirecionada para Configurações → Segurança antes de liberar a ação (UX-DR22)

**Given** a tela de configuração de MFA
**When** o Usuário escaneia o QR Code do autenticador e confirma um código válido
**Then** o segundo fator fica ativo e passa a ser exigido em logins futuros por senha

**Given** uma conta `usuario`/`almoxarife`
**When** ela acessa Configurações → Segurança
**Then** a configuração de MFA aparece como opcional, nunca forçada

**Given** um login via SSO (Story 1.9) para conta `gestor`/`adm`
**When** o Usuário autentica pelo Keycloak
**Then** a tela de MFA do stockflow nunca aparece — o realm corporativo já impõe o segundo fator

### Story 1.12: Log de acesso e auditoria

As a Adm,
I want consultar um log de todas as tentativas de login,
So that eu tenha visibilidade de acessos legítimos e suspeitos.

**Acceptance Criteria:**

**Given** qualquer tentativa de login (sucesso ou falha, por senha ou SSO)
**When** ela ocorre
**Then** um registro append-only é criado com usuário (quando identificável), timestamp, IP e método

**Given** uma tentativa de login com e-mail inexistente
**When** ela é registrada
**Then** o registro não revela ao solicitante se o e-mail existe, mas fica visível para o `adm` no log

**Given** a tela de Log de Acesso
**When** um `adm` a acessa e filtra por período
**Then** vê os registros, sem nenhuma ação de edição ou exclusão disponível na interface

**Given** um Usuário sem papel `adm`
**When** ele tenta acessar a rota de log de acesso
**Then** a resposta é 403 e o item de navegação nem aparece

## Epic 2: Gestão de Estoques

Almoxarife organiza os locais físicos de estoque com integridade referencial.

### Story 2.1: Criar e listar locais de Estoque

As a Almoxarife,
I want cadastrar e listar os locais físicos de Estoque,
So that eu possa organizar onde os Produtos ficam armazenados.

**Acceptance Criteria:**

**Given** um Almoxarife autenticado
**When** ele cadastra um novo Estoque com um nome
**Then** o Estoque é criado com `id` (UUID v4) e o nome é único de forma atômica (case/espaço-insensitive)

**Given** um nome de Estoque já existente (mesmo com capitalização/espaçamento diferente)
**When** outro Almoxarife tenta cadastrar o mesmo nome, inclusive sob requisições concorrentes
**Then** o sistema rejeita com 409

**Given** um Usuário com papel `usuario`
**When** ele tenta cadastrar ou excluir Estoques diretamente pela API
**Then** a resposta é 403 (Story 1.5)

**Given** a lista de Estoques cadastrados
**When** qualquer Usuário autenticado a consulta
**Then** a lista retorna nome e id de cada Estoque

### Story 2.2: Exclusão de Estoque trata resíduos e pedidos pendentes

As a Almoxarife,
I want que a exclusão de um Estoque seja bloqueada quando ainda há uso ativo,
So that eu não perca rastreabilidade de estoque residual ou de Pedidos em andamento.

**Acceptance Criteria:**

**Given** um Estoque sem nenhuma outra tabela referenciando-o ainda (estado do sistema logo após o Epic 2)
**When** um Almoxarife exclui o Estoque
**Then** a exclusão é permitida — não há o que bloquear nesse ponto do sistema

**Given** um Estoque com quantidade residual de algum Produto, uma vez que `PRODUTO_ESTOQUE` exista (Epic 3)
**When** um Almoxarife tenta excluir o Estoque
**Then** a exclusão é bloqueada e a resposta lista quais Produtos ainda têm quantidade ali

**Given** um Estoque referenciado por um Pedido com status `pendente`, uma vez que `PEDIDOS` exista (Epic 7)
**When** um Almoxarife tenta excluir o Estoque
**Then** a exclusão é bloqueada, mesmo com quantidade residual zerada

**Given** um Estoque sem quantidade residual e sem Pedido pendente referenciando
**When** um Almoxarife confirma a exclusão via `ConfirmDialog`
**Then** o Estoque é removido

**Nota de implementação:** os dois critérios de bloqueio (quantidade residual, Pedido pendente) dependem de tabelas criadas em épicos posteriores (`PRODUTO_ESTOQUE` no Epic 3, `PEDIDOS` no Epic 7). Esta story entrega a exclusão funcional agora; os guards concretos são adicionados como parte das stories que criam essas tabelas (3.1 e 7.2, respectivamente), sem reabrir esta story — Epic 2 não fica bloqueado esperando por Epic 3/7.

### Story 2.3: Migração dos Estoques legados

As a Adm/Almoxarife responsável pela migração,
I want que os Estoques do sistema legado sejam migrados automaticamente para o schema novo,
So that nenhum local de armazenamento existente se perca no corte de migração.

**Acceptance Criteria:**

**Given** a tabela `estoques` do sistema legado (espelho do Firestore mantido pela empresa)
**When** o script `cmd/migrate-legado` (AD-15) processa a migração de Estoques
**Then** cada Estoque legado é recriado com um novo UUID v4, preservando o nome, e uma entrada é criada na tabela de mapeamento id-antigo→id-novo

**Given** dois Estoques legados com nomes equivalentes por case/espaço
**When** a migração roda
**Then** o conflito de unicidade (Story 2.1) é detectado e reportado para revisão manual antes do corte — a migração nunca cria duas linhas para o "mesmo" Estoque

**Given** a migração já executada uma vez
**When** o script é executado novamente
**Then** Estoques já migrados não são duplicados (idempotência)

**Given** o corte de dados em produção
**When** o script é executado
**Then** a execução é sempre disparada manualmente por uma pessoa, nunca por um agente autônomo (AD-15, PRD §9)

## Epic 3: Cadastro, Importação e Fotos de Produtos

Almoxarife popula e mantém o catálogo — manual, em lote via planilha, ou com fotos.

### Story 3.1: Cadastro manual de Produto com dimensões estruturadas

As a Almoxarife,
I want cadastrar um Produto manualmente com suas dimensões e estoque inicial,
So that o catálogo reflita os materiais realmente disponíveis.

**Acceptance Criteria:**

**Given** um Almoxarife autenticado em Catálogo → Cadastrar
**When** ele informa nome, código (opcional), categoria, dimensões (cada uma como par valor+unidade — comprimento, largura, diâmetro, altura, espessura), estoque destino, quantidade inicial e observações
**Then** um Produto é criado e uma linha em `PRODUTO_ESTOQUE` vincula o Produto ao Estoque informado com a quantidade inicial (AD-9) — isto também completa o guard de exclusão da Story 2.2 (quantidade residual passa a ser verificável)

**Given** uma dimensão informada com valor mas sem unidade (ou vice-versa)
**When** o Almoxarife tenta salvar
**Then** o sistema rejeita o campo específico, sem salvar um Produto parcialmente preenchido

**Given** um Usuário com papel `usuario`
**When** ele tenta cadastrar um Produto diretamente pela API
**Then** a resposta é 403

**Given** a lista de 25 Categorias (addendum §H)
**When** o formulário de cadastro carrega
**Then** a Categoria é selecionada a partir dessa lista fixa, nunca digitada livremente

### Story 3.2: Nomenclatura Guiada por subtipo

As a Almoxarife,
I want usar um template de nome sugerido ao cadastrar um Produto de um subtipo conhecido,
So that o catálogo tenha nomes consistentes entre diferentes pessoas cadastrando.

**Acceptance Criteria:**

**Given** a lista de 28 templates de Nomenclatura Guiada (addendum §G)
**When** o Almoxarife seleciona um template ao cadastrar um Produto
**Then** o campo nome exige preencher todos os placeholders do template, na mesma ordem de tokens, validado no servidor

**Given** um Produto cadastrado sem template selecionado
**When** o Almoxarife informa o nome
**Then** o campo aceita texto livre, sem validação de estrutura

**Given** um Produto com template aplicado
**When** o Almoxarife edita o nome depois do cadastro
**Then** a edição revalida o nome contra o mesmo template — não é possível burlar a regra editando depois

### Story 3.3: Importação em massa via planilha padronizada

As a Almoxarife,
I want importar uma planilha com muitos Produtos de uma vez,
So that eu não precise cadastrar item a item ao encerrar uma obra ou consolidar um catálogo.

**Acceptance Criteria:**

**Given** o modelo de planilha padronizado (nome, código, categoria, dimensões com valor+unidade separados, quantidade, estoque, observações)
**When** o Almoxarife envia uma planilha com o cabeçalho correto
**Then** cada linha é processada, criando Estoques ausentes automaticamente (Story 2.1) e um Produto por linha válida

**Given** uma planilha com cabeçalho fora do padrão
**When** o Almoxarife tenta importar
**Then** a importação inteira é rejeitada antes de processar qualquer linha, indicando o problema no cabeçalho

**Given** uma linha com valor de dimensão sem a unidade correspondente
**When** a planilha é processada
**Then** essa linha específica é marcada como erro no relatório final, sem interromper as demais linhas

**Given** uma importação interrompida no meio (falha de rede, fechamento do navegador)
**When** o Almoxarife reabre a tela de Importar
**Then** um banner mostra até onde a importação chegou e oferece continuar, sem reprocessar linhas já salvas (`IMPORTACOES`/`IMPORTACAO_LINHAS`, addendum §A)

### Story 3.4: Importação atualiza por código, não só cria

As a Almoxarife,
I want que reimportar uma planilha atualize Produtos já existentes em vez de duplicá-los,
So that eu possa reprocessar uma planilha corrigida sem gerar lixo no catálogo.

**Acceptance Criteria:**

**Given** uma planilha com uma linha cujo código já existe no catálogo
**When** a importação processa essa linha
**Then** o Produto existente é atualizado com os novos valores, em vez de criar um Produto duplicado

**Given** uma planilha reimportada sem nenhuma mudança
**When** a importação processa
**Then** nenhum Produto novo é criado, e a linha nunca aparece como "criado" no relatório

**Given** o relatório final de importação
**When** ele é exibido
**Then** discrimina quantas linhas foram criadas, atualizadas e rejeitadas, com um CTA "Verificar duplicatas agora" apontando para a Normalização (Epic 6, UX-DR20)

**Given** uma linha sem código cujo nome é parecido com um Produto existente
**When** a importação processa
**Then** um novo Produto é criado — correspondência por nome sem código fica para a ferramenta de Duplicatas, não para o importador

### Story 3.5: Upload e armazenamento de foto do Produto

As a Almoxarife,
I want anexar fotos a um Produto,
So that outros Usuários reconheçam o material visualmente antes de reservar.

**Acceptance Criteria:**

**Given** um Produto existente
**When** um Almoxarife envia uma foto (JPG/PNG/WEBP, via câmera ou galeria do dispositivo)
**Then** a imagem é redimensionada para 500px no maior lado e comprimida em JPEG q=0.82, independente do fluxo (cadastro ou edição)
**And** o arquivo é salvo em volume Docker nomeado e persistente, com nome versionado (`<produto_id>-<timestamp_unix>.jpg`), nunca em base64 no banco (AD-11)

**Given** um arquivo fora do tamanho ou formato aceito
**When** o Almoxarife tenta enviar
**Then** o sistema rejeita com um erro específico indicando se o problema é tamanho ou formato

**Given** um Produto já com uma foto
**When** um Almoxarife envia uma nova foto
**Then** a foto anterior não é sobrescrita no mesmo caminho — o nome versionado evita servir uma imagem obsoleta de cache

**Given** um Usuário com papel `usuario`
**When** ele tenta enviar uma foto pela API
**Then** a resposta é 403 — upload é só para `almoxarife`+; visualização continua liberada a todos

### Story 3.6: Galeria e visualização ampliada de fotos (lightbox)

As a qualquer Usuário,
I want ver as fotos de um Produto em destaque,
So that eu confirme visualmente que é o material certo antes de reservar.

**Acceptance Criteria:**

**Given** um Produto com múltiplas fotos
**When** um Usuário abre o card ou o detalhe do Produto
**Then** vê uma galeria navegável de todas as fotos

**Given** uma foto na galeria
**When** o Usuário toca/clica nela
**Then** ela expande em lightbox de tela cheia (UX-DR23)

**Given** o lightbox aberto
**When** o Usuário fecha (toque fora, `Esc`, ou botão fechar)
**Then** retorna à posição exata de rolagem anterior, sem recarregar a página

### Story 3.7: Migração de Produtos, Categorias e fotos legadas

As a Adm/Almoxarife responsável pela migração,
I want que os Produtos, suas fotos e as Categorias do sistema legado sejam migrados para o schema novo,
So that o catálogo não comece vazio na virada para o novo sistema.

**Acceptance Criteria:**

**Given** a tabela `produtos` do sistema legado, com dimensões em texto livre
**When** o script `cmd/migrate-legado` processa a migração de Produtos
**Then** cada dimensão é convertida para o par estruturado `{valor, unidade}` usando um parser único de conversão; casos ambíguos ficam marcados para revisão manual via Normalização (Epic 6)

**Given** as fotos armazenadas inline em base64 no sistema legado
**When** a migração processa um Produto com foto
**Then** a foto é extraída, redimensionada/comprimida conforme a Story 3.5, e salva no volume de fotos — nunca migrada como base64

**Given** a lista de 25 Categorias e os 28 Templates de Nomenclatura Guiada (addendum §H, §G)
**When** a migração roda pela primeira vez
**Then** essas listas são inseridas como seed, como fonte única — mesmo estando hoje duplicadas em dois lugares do sistema legado

**Given** a migração já executada
**When** o script roda novamente
**Then** Produtos e seeds já migrados não são duplicados

**Given** o corte de dados em produção
**When** o script é executado
**Then** é sempre disparado manualmente por uma pessoa, nunca por um agente autônomo

## Epic 4: Catálogo — Consulta, Descoberta e Exportação

Qualquer Usuário encontra material disponível, vê onde está (inclusive via QR Code/código de barras), e exporta o catálogo.

### Story 4.1: Busca por nome/código/categoria com sugestões

As a qualquer Usuário,
I want buscar um Produto por nome, código ou categoria com sugestões automáticas,
So that eu encontre rapidamente o material que preciso.

**Acceptance Criteria:**

**Given** o campo de busca do Catálogo
**When** o Usuário digita alguns caracteres
**Then** até 7 sugestões aparecem, ordenadas por relevância, atualizando conforme ele digita

**Given** uma busca sem nenhum resultado
**When** o Usuário completa a digitação
**Then** a tela mostra "Nenhum produto encontrado para '{busca}'.", sem sugestão de comprar externamente

**Given** a NFR de desempenho (≤300ms p95, até 8.000 produtos/30 estoques)
**When** a busca é executada sob carga típica
**Then** o tempo de resposta cumpre esse limite

### Story 4.2: Filtros por categoria, estoque e disponibilidade

As a qualquer Usuário,
I want filtrar o catálogo por categoria, estoque e disponibilidade,
So that eu restrinja a lista ao que realmente me interessa.

**Acceptance Criteria:**

**Given** a lista de Categorias e Estoques cadastrados
**When** o Usuário aplica um ou mais filtros simultaneamente
**Then** a lista de Produtos reflete a combinação de todos os filtros ativos

**Given** o filtro "Com estoque"
**When** aplicado
**Then** só aparecem Produtos com quantidade maior que zero em pelo menos um Estoque

**Given** filtros aplicados junto com a busca por texto (Story 4.1)
**When** ambos estão ativos
**Then** busca e filtros combinam (E lógico), um não substitui o outro

### Story 4.3: Visualização em grade e tabela agrupada

As a qualquer Usuário,
I want alternar entre visualização em grade (cards) e tabela agrupada,
So that eu escolha o formato mais adequado ao meu dispositivo e tarefa.

**Acceptance Criteria:**

**Given** o Catálogo em viewport ≥768px
**When** o Usuário alterna para visualização em tabela
**Then** produtos com mesmo nome/unidade/dimensões aparecem agrupados numa linha, com a soma das quantidades

**Given** o Catálogo em viewport <768px (a partir de 360px)
**When** o Usuário o acessa
**Then** a visualização padrão é em grade (cards), consistente com o shell mobile (UX-DR16)
**And** cada card mostra o badge `status-disponivel` ("Disponível"/"Sem estoque") sempre com ícone + texto, nunca só cor (UX-DR10)

**Given** uma linha agrupada na tabela
**When** o Usuário clica/expande
**Then** vê a quantidade discriminada por Estoque (Story 4.4)

### Story 4.4: Detalhe do produto por Estoque com atualização em tempo real

As a qualquer Usuário,
I want ver o detalhe de um Produto com a quantidade exata por Estoque, sempre atualizada,
So that eu confie no que vejo antes de reservar.

**Acceptance Criteria:**

**Given** o detalhe de um Produto
**When** ele é aberto
**Then** mostra a quantidade discriminada por cada Estoque onde o Produto está presente

**Given** o canal SSE `produtos` (AD-3)
**When** um evento de mudança de quantidade chega enquanto a tela está aberta
**Then** um toast discreto (`aria-live="polite"`) avisa "Catálogo atualizado.", sem recarregar a tela sozinho (UX-DR17)

**Given** uma conexão SSE que demora mais que alguns segundos para reconectar
**When** isso acontece
**Then** um indicador "Reconectando..." aparece; reconexão rápida permanece silenciosa (UX-DR18)

**Given** a reconexão SSE concluída
**When** o cliente volta a ficar online
**Then** ele sempre faz um GET completo do estado atual, nunca espera replay de eventos perdidos (AD-3)

### Story 4.5: Identificação de Produto via QR Code / código de barras

As a qualquer Usuário,
I want apontar a câmera para um Código de Identificação e ir direto ao Produto,
So that eu não precise digitar a busca em campo.

**Acceptance Criteria:**

**Given** a tela de Catálogo ou Carrinho
**When** o Usuário toca no `fab-scanner` (UX-DR4) e aponta a câmera para um QR Code/código de barras
**Then** o Código de Identificação lido abre o detalhe do Produto (Story 4.4) ou o adiciona ao Carrinho, conforme o contexto

**Given** a câmera sem permissão concedida, sem hardware, ou incapaz de reconhecer o código
**When** a leitura falha
**Then** uma mensagem clara aparece e o campo de busca por texto (Story 4.1) continua disponível e em foco — o scanner nunca é a única forma de encontrar um Produto (UX-DR13)

**Given** um ambiente sem contexto seguro (HTTPS)
**When** o Usuário tenta acessar o scanner
**Then** a funcionalidade fica indisponível com uma mensagem explicando o motivo — a câmera do navegador exige HTTPS (UX-DR21)

**Given** um Produto sem Código de Identificação cadastrado
**When** alguém tenta escaneá-lo
**Then** ele continua acessível normalmente por busca textual, só sem esse atalho

### Story 4.6: Exportação da tabela do catálogo para Excel

As a Almoxarife,
I want exportar a tabela do catálogo, com os filtros aplicados, para Excel,
So that eu leve os dados para uma planilha ou relatório externo.

**Acceptance Criteria:**

**Given** a visualização em tabela do Catálogo com um ou mais filtros ativos (Story 4.2)
**When** o Almoxarife exporta
**Then** o `.xlsx` gerado reflete exatamente o filtro aplicado, com subtotais dinâmicos (fórmula `SUBTOTAL`, não soma estática) por grupo

**Given** um filtro que resulta em zero produtos
**When** o Almoxarife exporta mesmo assim
**Then** o `.xlsx` é gerado válido, contendo só o cabeçalho

**Given** um Usuário com papel `usuario`
**When** ele tenta exportar diretamente pela API
**Then** a resposta é 403 — exportação é restrita a `almoxarife`+

## Epic 5: Movimentação de Estoque

Almoxarife registra saída e transferência de estoque com histórico consultável.

### Story 5.1: Registrar Baixa (consumo)

As a Almoxarife,
I want registrar a baixa (consumo) de um Produto em um Estoque,
So that o saldo reflita o que realmente saiu, com rastro auditável.

**Acceptance Criteria:**

**Given** um Produto com quantidade disponível em um Estoque
**When** um Almoxarife registra uma Baixa de uma quantidade válida (maior que zero)
**Then** o sistema usa `SELECT ... FOR UPDATE` na linha de `PRODUTO_ESTOQUE` (AD-10), debita a quantidade e cria uma Movimentação do tipo `baixa` na mesma transação — nunca uma sem a outra

**Given** uma quantidade zero ou negativa informada
**When** o Almoxarife tenta registrar
**Then** o sistema rejeita antes de qualquer escrita

**Given** uma quantidade maior que a disponível
**When** o Almoxarife tenta registrar
**Then** o sistema rejeita, informando a quantidade real disponível no momento

**Given** um Usuário com papel `usuario`
**When** ele tenta registrar uma Baixa pela API
**Then** a resposta é 403

### Story 5.2: Registrar Transferência entre Estoques

As a Almoxarife,
I want transferir uma quantidade de um Produto entre dois Estoques,
So that o material fique corretamente localizado sem passar por uma Baixa/Entrada manual.

**Acceptance Criteria:**

**Given** dois Estoques diferentes com o Produto presente na origem
**When** um Almoxarife registra uma Transferência de origem para destino
**Then** a checagem de disponibilidade e o débito/crédito são atômicos na mesma transação, com locks adquiridos na ordem canônica `(produto_id, estoque_id)` ascendente (AD-10)
**And** uma Movimentação do tipo `transferencia` é criada com origem e destino registrados

**Given** origem igual ao destino
**When** o Almoxarife tenta transferir
**Then** o sistema rejeita

**Given** quantidade maior que a disponível na origem
**When** a transferência é tentada
**Then** o sistema rejeita, sem debitar nada

**Given** duas transferências concorrentes envolvendo os mesmos dois Estoques, montadas em ordens de inserção opostas
**When** ambas tentam rodar ao mesmo tempo
**Then** a ordem canônica de lock evita deadlock — uma espera a outra, nenhuma trava indefinidamente

### Story 5.3: Histórico de Movimentações consultável

As a Almoxarife,
I want consultar o histórico de todas as Movimentações,
So that eu tenha rastreabilidade completa de saídas e transferências.

**Acceptance Criteria:**

**Given** Movimentações registradas (Baixa ou Transferência)
**When** um Almoxarife acessa o Histórico
**Then** vê Produto, tipo, origem, destino, quantidade, autor e data de cada Movimentação, em ordem cronológica

**Given** o canal SSE `movimentacoes` (AD-3)
**When** uma nova Movimentação é criada
**Then** um evento é publicado nesse canal para qualquer tela assinante atualizar (toast discreto, UX-DR17)

**Given** um Usuário com papel `usuario`
**When** ele tenta consultar o Histórico pela API
**Then** a resposta é 403

### Story 5.4: Migração do Histórico de Movimentações legado

As a Adm/Almoxarife responsável pela migração,
I want que o histórico de movimentações do sistema legado seja migrado,
So that a rastreabilidade anterior ao corte não se perca.

**Acceptance Criteria:**

**Given** a tabela `historico` do sistema legado (produto desnormalizado por nome, tipo baixa/transferência, origem/destino, quantidade, timestamp)
**When** o script `cmd/migrate-legado` processa o Histórico
**Then** cada registro é recriado como uma Movimentação vinculada ao novo `produto_id` (via tabela de mapeamento id-antigo→id-novo), preservando data e autor originais quando disponíveis

**Given** um registro do histórico legado referenciando um Produto que não foi migrado
**When** a migração processa esse registro
**Then** ele é marcado para revisão manual, sem interromper a migração dos demais

**Given** a migração já executada
**When** o script roda novamente
**Then** registros já migrados não são duplicados

**Given** o corte de dados em produção
**When** o script é executado
**Then** é sempre disparado manualmente por uma pessoa, nunca por um agente autônomo

## Epic 6: Normalização de Dados

Almoxarife mantém o catálogo limpo (inconsistências e duplicatas) sem trabalho manual item a item.

### Story 6.1: Detecção de inconsistências dimensionais

As a Almoxarife,
I want que o sistema aponte inconsistências dimensionais em Produtos,
So that eu identifique dados incompletos ou malformados sem revisar item a item manualmente.

**Acceptance Criteria:**

**Given** um Produto com uma dimensão estruturada válida
**When** a análise de inconsistências roda
**Then** esse campo nunca gera sugestão

**Given** um Produto migrado com uma dimensão que não pôde ser convertida automaticamente na migração (Story 3.7), ou um valor de dimensão implícito no nome sem preenchimento no campo estruturado
**When** o Almoxarife clica "Analisar todos os produtos"
**Then** o sistema lista sugestões, cada uma identificando produto, campo, valor sugerido e origem (migração ou nome)

### Story 6.2: Aplicação seletiva de correções

As a Almoxarife,
I want aplicar ou ignorar cada sugestão de correção, individualmente ou em lote,
So that eu limpe o catálogo no meu ritmo, sem re-analisar o que já decidi.

**Acceptance Criteria:**

**Given** uma lista de sugestões de inconsistência (Story 6.1)
**When** o Almoxarife aplica uma correção individualmente, em lote por produto, ou em lote geral
**Then** os campos correspondentes são atualizados para o valor estruturado sugerido

**Given** uma sugestão marcada como "ignorar" para um valor específico
**When** a mesma inconsistência (mesmo produto, campo, valor) é reavaliada depois
**Then** ela não reaparece

**Given** um campo cujo valor muda depois para um novo valor inconsistente diferente do que foi ignorado
**When** a análise roda de novo
**Then** a sugestão reaparece — "ignorar" vale só para o valor específico já visto

### Story 6.3: Detecção de duplicatas

As a Almoxarife,
I want que o sistema identifique Produtos duplicados,
So that eu consolide o catálogo sem perder tempo comparando manualmente.

**Acceptance Criteria:**

**Given** dois ou mais Produtos com nome normalizado igual, dimensões equivalentes (considerando conversão de unidade) e locais coincidentes
**When** a detecção de duplicatas roda
**Then** esses Produtos aparecem agrupados como candidatos a mesclagem

**Given** Produtos com nome normalizado igual mas dimensões diferentes
**When** a detecção roda
**Then** eles não são agrupados como duplicatas

**Given** o relatório de importação (Stories 3.3/3.4)
**When** o Almoxarife clica "Verificar duplicatas agora"
**Then** é levado direto a esta tela com a análise já em andamento

### Story 6.4: Mesclagem de duplicatas com trilha de auditoria

As a Almoxarife,
I want mesclar Produtos duplicados mantendo um só registro,
So that o catálogo fique consolidado sem perder o histórico de nenhum dos itens mesclados.

**Acceptance Criteria:**

**Given** um grupo de Produtos duplicados (Story 6.3)
**When** o Almoxarife confirma a mesclagem (via `ConfirmDialog`) escolhendo qual Produto mantém
**Then** a quantidade dos demais é somada no Produto mantido, os demais são soft-deletados (`deleted_at`), e uma auditoria é registrada (quem, quando, produtos removidos, valores) em `MESCLAGEM_PRODUTOS_REMOVIDOS` — permanente, nunca expurgada

**Given** as linhas históricas de Movimentações e itens de Pedido dos Produtos removidos
**When** a mesclagem é confirmada
**Then** o `produto_id` dessas linhas é reescrito para o Produto sobrevivente antes do soft-delete, preservando "soma de Movimentações == quantidade atual" (AD-11)

**Given** um item em Carrinho ou Pedido pendente referenciando um Produto que acabou de ser mesclado
**When** a mesclagem é confirmada
**Then** esse item é redirecionado automaticamente para o Produto mantido

**Given** a quantidade somada calculada no momento da revisão
**When** o Almoxarife confirma a mesclagem depois de um tempo
**Then** a quantidade é revalidada na confirmação, nunca usa um snapshot antigo

**Given** um Produto já soft-deletado por uma mesclagem anterior
**When** ele aparece em uma nova análise de duplicatas
**Then** ele nunca reentra em uma nova mesclagem, mas sua foto permanece em disco para auditoria permanente

## Epic 7: Pedidos de Retirada

Usuário solicita, almoxarife aprova com estoque real, recibo em PDF — ciclo completo.

### Story 7.1: Carrinho de reserva

As a qualquer Usuário,
I want acumular itens num carrinho antes de enviar um Pedido,
So that eu monte a solicitação inteira antes de formalizar.

**Acceptance Criteria:**

**Given** um Produto disponível num Estoque
**When** o Usuário o adiciona ao Carrinho (via detalhe do Produto, tabela, ou leitura de QR Code — Story 4.5)
**Then** o item entra no Carrinho, a disponibilidade é validada no momento da adição (somando linhas já no carrinho para o mesmo Produto/Estoque), um toast confirma "Adicionado ao Carrinho." e o `cart-badge` atualiza o contador (UX-DR5, UX-DR11)

**Given** um item no Carrinho cujo Produto ou Estoque deixou de existir (ex. mesclado — Story 6.4, ou Estoque excluído — Story 2.2)
**When** o Usuário abre o Carrinho
**Then** esse item é removido automaticamente, com aviso explicando o motivo

**Given** o Carrinho vazio
**When** o Usuário o acessa
**Then** vê "Seu carrinho está vazio. Busque um produto ou aponte a câmera para um código."

**Given** um item removido do Carrinho
**When** a remoção é confirmada
**Then** um toast confirma e o `cart-badge` atualiza (some por completo se ficar vazio, nunca mostra "0")

### Story 7.2: Envio de Pedido

As a qualquer Usuário,
I want enviar o Carrinho como um Pedido de Retirada formal,
So that o almoxarife possa aprovar e separar o material.

**Acceptance Criteria:**

**Given** um Carrinho com ao menos um item
**When** o Usuário informa solicitante, obra/centro de custo e observação, e envia
**Then** um Pedido é criado com status `pendente`, revalidando a disponibilidade de cada item no momento do envio — isto também completa o segundo guard de exclusão pendente na Story 2.2 (Pedido `pendente` referenciando um Estoque)

**Given** um Carrinho vazio
**When** o Usuário tenta enviar
**Then** o sistema rejeita

**Given** "solicitante" preenchido como texto livre diferente do nome do Usuário autenticado
**When** o Pedido é registrado
**Then** a auditoria e "Meus Pedidos" sempre usam a identidade autenticada, independente do texto livre

### Story 7.3: Consulta de Pedidos próprios

As a qualquer Usuário,
I want acompanhar meus próprios Pedidos,
So that eu saiba o status de cada solicitação que fiz.

**Acceptance Criteria:**

**Given** Pedidos enviados por um Usuário
**When** ele acessa "Meus Pedidos"
**Then** vê a lista filtrável por status, com badge `status-pendente`/`status-aprovado`/`status-rejeitado` sempre em ícone + texto, nunca só cor (UX-DR6, UX-DR10)

**Given** um Pedido de outro Usuário
**When** este Usuário tenta acessá-lo diretamente por id
**Then** o acesso é negado, conforme o padrão de escopo do sistema

### Story 7.4: Consulta de todos os Pedidos (Fila, Almoxarife+)

As a Almoxarife,
I want ver todos os Pedidos pendentes da organização, não só os meus,
So that eu tenha uma fila única de trabalho para atender.

**Acceptance Criteria:**

**Given** um `almoxarife`+ acessando Pedidos → Fila
**When** ele filtra por status
**Then** vê todos os Pedidos da organização que casam o filtro, não só os próprios

**Given** um Usuário sem papel `almoxarife`+ chamando a mesma rota
**When** a requisição chega
**Then** ele recebe só os próprios Pedidos — escopo, não erro (AD-8, Story 1.5)

### Story 7.5: Aprovação/rejeição com revalidação de estoque item a item

As a Almoxarife,
I want aprovar ou rejeitar um Pedido com o estoque real revalidado no momento da decisão,
So that eu nunca aprove algo que já não existe mais.

**Acceptance Criteria:**

**Given** um Pedido pendente com múltiplos itens
**When** o Almoxarife abre para decidir
**Then** cada item é revalidado no servidor; itens com estoque insuficiente mostram "Solicitado: X · Disponível: Y" (UX-DR19)

**Given** um item com divergência
**When** o Almoxarife escolhe aprovar parcialmente esse item
**Then** o débito e a Movimentação são atômicos só para a quantidade aprovada, e o restante volta como pendência separada — nunca um sucesso parcial silencioso

**Given** um Pedido com múltiplos itens de Produtos/Estoques diferentes
**When** a aprovação processa o lote inteiro
**Then** os locks são adquiridos na ordem canônica `(produto_id, estoque_id)` ascendente sobre o conjunto ordenado do lote inteiro, não na ordem de inserção do carrinho (AD-10)

**Given** o papel do aprovador
**When** a aprovação é submetida
**Then** o papel é revalidado no momento exato da submissão, não no carregamento da tela

**Given** uma decisão de aprovação/rejeição concluída
**When** ela é salva
**Then** o badge do Pedido muda de status na hora (canal SSE `pedidos`, AD-3) sem recarregar a página

### Story 7.6: Recibo do Pedido em PDF gerado pelo servidor

As a Usuário ou Almoxarife,
I want baixar um recibo em PDF de um Pedido já decidido,
So that eu tenha um comprovante formal da retirada.

**Acceptance Criteria:**

**Given** um Pedido aprovado ou parcialmente aprovado
**When** alguém com acesso ao Pedido clica "Baixar recibo"
**Then** um PDF é gerado no servidor com itens, quantidades, estoques de origem, solicitante, aprovador e data — sempre a partir do snapshot já capturado em `PEDIDO_ITENS` no momento da decisão, nunca um join ao vivo com o catálogo atual (AD-17)

**Given** um Produto editado depois da decisão do Pedido
**When** o recibo é baixado novamente
**Then** o conteúdo do PDF não muda — reflete o que foi decidido, não o estado atual do catálogo

**Given** um Pedido ainda pendente
**When** alguém tenta baixar o recibo
**Then** a opção não está disponível — só Pedidos já decididos têm recibo

### Story 7.7: Migração de Pedidos e vínculo com Histórico

As a Adm/Almoxarife responsável pela migração,
I want que os Pedidos do sistema legado sejam migrados preservando seu vínculo com o Histórico,
So that solicitações e aprovações anteriores ao corte continuem rastreáveis.

**Acceptance Criteria:**

**Given** a coleção `pedidos` do sistema legado (itens, status, solicitante, e-mail/uid, timestamps)
**When** o script `cmd/migrate-legado` processa Pedidos
**Then** cada Pedido é recriado com seus itens, referenciando os novos `produto_id`/`estoque_id` via a tabela de mapeamento id-antigo→id-novo, preservando o status original

**Given** um Pedido legado aprovado que gerou uma Movimentação no histórico legado
**When** ambos são migrados
**Then** o vínculo entre a Movimentação migrada (Story 5.4) e o Pedido migrado é preservado

**Given** a migração já executada
**When** o script roda novamente
**Then** Pedidos já migrados não são duplicados

**Given** o corte de dados em produção
**When** o script é executado
**Then** é sempre disparado manualmente por uma pessoa, nunca por um agente autônomo

## Epic 8: Privacidade e Conformidade (LGPD)

Usuário exporta os próprios dados pessoais; Adm processa solicitações de exclusão/anonimização — cobrindo identidade, log de acesso, Movimentações e Pedidos já existentes nos épicos anteriores.

### Story 8.1: Exportação dos próprios dados pessoais

As a Usuário,
I want baixar meus dados pessoais,
So that eu tenha controle e transparência sobre o que o sistema guarda sobre mim, conforme a LGPD.

**Acceptance Criteria:**

**Given** um Usuário autenticado em Meu Perfil
**When** ele clica "Baixar meus dados"
**Then** recebe um arquivo (JSON ou PDF) com nome, e-mail, log de acesso (Story 1.12), Movimentações que registrou (Story 5.3) e Pedidos que criou (Story 7.3)

**Given** um Usuário sem nenhuma Movimentação ou Pedido registrado
**When** ele exporta seus dados
**Then** o arquivo é gerado normalmente, com essas seções vazias

### Story 8.2: Exclusão e anonimização de dados pessoais por Adm

As a Adm,
I want processar uma solicitação de exclusão de conta,
So that o stockflow atenda ao direito de exclusão da LGPD sem quebrar a integridade de registros históricos.

**Acceptance Criteria:**

**Given** um Usuário solicitando exclusão da própria conta (via "Solicitar exclusão de conta" em Meu Perfil)
**When** a solicitação chega
**Then** ela fica registrada para um `adm` processar — não é uma exclusão self-service imediata

**Given** um `adm` processando uma solicitação de exclusão
**When** ele confirma a anonimização via `ConfirmDialog`
**Then** nome e e-mail da conta são substituídos por valores anonimizados, mas o `usuario_id` permanece intacto em qualquer Movimentação, Pedido ou registro de log já existente — nenhuma referência histórica é quebrada ou removida

**Given** uma conta já anonimizada
**When** alguém tenta autenticar com o e-mail antigo (senha ou SSO)
**Then** o login falha como se a conta não existisse

**Given** o `adm` que processaria a exclusão é o único `adm` ativo do sistema
**When** ele tenta anonimizar a própria conta ou a de outro `adm`
**Then** o sistema bloqueia com mensagem explicando que ao menos um `adm` ativo deve sempre existir
