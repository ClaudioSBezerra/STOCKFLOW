---
name: stockflow
status: final
sources:
  - _bmad-output/planning-artifacts/prds/prd-stockflow-2026-08-29/prd.md
  - _bmad-output/planning-artifacts/prds/prd-stockflow-2026-08-29/addendum.md
  - _bmad-output/planning-artifacts/architecture/architecture-stockflow-2026-08-29/ARCHITECTURE-SPINE.md
created: '2026-08-29'
updated: '2026-08-29'
---

# stockflow — Experience Spine

## Foundation

Web multi-superfície responsivo — mesma base de código atende desktop/tablet (rail de ícones + abas, espelhando o `FB_APU02`) e mobile (bottom nav + botão flutuante), sem app nativo. shadcn/ui sobre React 19 + Vite + TypeScript (conforme a Architecture Spine). `DESIGN.md` é a referência visual; esta spine governa o comportamento.

Navegação é **gated por papel** (`usuario < almoxarife < gestor < adm`, hierarquia estrita — Glossário do PRD): um item de navegação para o qual o Usuário não tem papel simplesmente **não aparece** — nunca uma tela de "acesso negado" (mesmo padrão do `FB_APU02`: itens `adminOnly` somem da navegação, não aparecem desabilitados).

Duas superfícies públicas, fora do shell autenticado: Login (FR-1, FR-34) e Autocadastro (FR-3).

## Information Architecture

| Superfície | Alcançada por | Propósito | Papel mínimo |
|---|---|---|---|
| Login | URL raiz sem sessão | E-mail/senha (FR-1) ou "Entrar com Ferreira Costa" (FR-34, SSO) | público |
| Autocadastro | Link "Criar conta" no Login | Cadastro público, sempre como `usuario` (FR-3) | público |
| Callback SSO | Redirect do Keycloak | Tela de transição (spinner) durante troca de token (FR-34) — nunca fica visível por mais que o tempo de rede | público |
| **Catálogo** | Rail/bottom nav (ícone padrão, item inicial pós-login) | Buscar/filtrar Produtos, ver disponibilidade por Estoque (FR-4–7), identificar Produto via Código de Identificação lido por QR Code/código de barras (FR-35) | `usuario` |
| Catálogo → Cadastrar | Aba dentro de Catálogo | Cadastro manual de Produto, Nomenclatura Guiada (FR-8–9) | `almoxarife` |
| Catálogo → Importar | Aba dentro de Catálogo | Importação em massa via planilha (FR-10–11) | `almoxarife` |
| Catálogo → detalhe do Produto → Fotos | Toque na foto do card/detalhe | Galeria, upload e lightbox de fotos do Produto (FR-27–29) | `usuario` (ver), `almoxarife` (upload) |
| **Carrinho** | Ícone persistente com badge (rail/bottom nav) | Reserva de itens antes de enviar Pedido (FR-21) | `usuario` |
| **Pedidos** → Meus Pedidos | Rail/bottom nav | Acompanhar Pedidos próprios (FR-22–23), baixar recibo em PDF de um Pedido decidido (FR-26) | `usuario` |
| Pedidos → Fila | Aba dentro de Pedidos | Aprovar/rejeitar Pedidos pendentes (FR-24–25), baixar recibo em PDF (FR-26) | `almoxarife` |
| **Estoques** | Rail (desktop) — recolhido para dentro de "Mais" no bottom nav mobile | Locais de Estoque (FR-12–13), Movimentações (FR-14–16) | `almoxarife` |
| **Normalização** | Rail (desktop) — dentro de "Mais" no mobile | Inconsistências e Duplicatas (FR-17–20) | `almoxarife` |
| **Relatórios** | Rail (desktop) — dentro de "Mais" no mobile | Exportação Excel do catálogo (FR-30) | `almoxarife` |
| **Configurações** → Meu Perfil | Rodapé do rail/bottom nav (avatar, `DropdownMenu`) | Dados da conta, trocar senha, solicitar promoção de papel (FR-33), ver método de login (senha/SSO) | `usuario` |
| Configurações → Segurança | Aba dentro de Configurações | Configurar segundo fator (TOTP) — obrigatório para `gestor`/`adm` autenticados por senha (FR-37) | `usuario` (visível a todos, obrigatório a partir de `gestor`) |
| Configurações → Promoções | Aba dentro de Configurações | Decidir Solicitações de Promoção de Papel (FR-33) | `gestor` |
| Configurações → Usuários | Aba dentro de Configurações | Gestão de contas, desativação/rebaixamento (FR-31) | `gestor` (escopo limitado — AD-8), `adm` (completo) |
| Configurações → Log de Acesso | Aba dentro de Configurações | Consulta do log de acesso (FR-38) | `adm` |
| Configurações → Privacidade (LGPD) | Aba dentro de Configurações | Exportar/excluir dados pessoais (FR-39) | `usuario` (exportar os próprios) / `adm` (processar exclusão de terceiros) |

**Desktop/tablet (`≥ md`, 768px+):** rail de ícones fixo (`{spacing.rail-width}`) à esquerda + header fino + barra de abas horizontal por módulo, ou submenu vertical (`{spacing.sidenav-width}`) para Estoques/Normalização — mesmo padrão do `FB_APU02`.
**Mobile (`< md`):** rail vira **bottom nav** (`{spacing.bottom-nav-height}`) com os mesmos ícones (Catálogo, Carrinho, Pedidos, Mais); Estoques/Normalização/Relatórios/Configurações completas (incluindo Segurança/MFA e Log de Acesso) ficam atrás do item "Mais" (abre um `Sheet` de baixo para cima).

Modal empilha no máximo um nível (abrir um `Dialog`/`AlertDialog` sobre uma tela, nunca sobre outro modal).

→ Referência de composição: nenhum mockup renderizado nesta rodada (ver Finalize/Deferred). Spine vence em caso de conflito com qualquer mockup futuro.

## Voice and Tone

Microcopy em PT-BR. Tom direto, sem exclamação, vocabulário do Glossário do PRD usado literalmente (Produto, Estoque, Movimentação, Pedido, Carrinho — nunca sinônimo).

| Do | Don't |
|---|---|
| "3 itens no Carrinho" | "Você tem 3 itens no seu carrinho! 🛒" |
| "Pedido aprovado." | "Pedido aprovado com sucesso! ✓" |
| "Não foi possível conectar. Suas alterações locais não foram salvas." | "Erro de rede" |
| "Nenhum produto encontrado para 'tubo pvc 100'." | "Ops! Nada por aqui 😅" |
| Mesmo tom para Mariana (campo) e João (administrativo) — o produto fala igual para todos os papéis | Tom mais "casual" no mobile e mais "formal" no desktop |

## Component Patterns

Comportamental. Especificação visual em `DESIGN.md.Components`.

| Componente | Uso | Regras comportamentais |
|---|---|---|
| Card de Produto (grade) | Catálogo, mobile | Toque no card abre detalhe (FR-7). Foto, nome, badge `status-disponivel` (ícone + texto "Disponível"/"Sem estoque", nunca só cor — ver DESIGN.md). |
| Linha de Produto (tabela agrupada) | Catálogo, desktop | FR-6: agrupa e soma quantidades de mesmo nome/unidade/dimensão; clique expande para ver por Estoque. |
| `fab-scanner` | Catálogo, Carrinho | Toque abre a câmera (`getUserMedia`), com um alvo de toque de `{spacing.touch-target-min}`; leitura reconhecida fecha a câmera e navega direto ao resultado (detalhe do Produto ou adiciona ao Carrinho, conforme FR-35). **Sempre existe um caminho alternativo:** o campo de busca do Catálogo aceita digitar/colar o Código de Identificação manualmente — a câmera nunca é a única forma de localizar um Produto por código (ver Accessibility Floor). |
| Galeria e upload de foto | Catálogo → detalhe do Produto → Fotos | `almoxarife`+: botão "Adicionar foto" (câmera ou galeria do dispositivo). `usuario`: só visualização. Toque numa foto abre lightbox em tela cheia; fechar retorna à posição exata da rolagem anterior (FR-27–29). |
| Linha de item no Carrinho | Carrinho | Swipe ou botão "Remover" (alvo `{spacing.touch-target-min}`) tira o item; quantidade editável inline; item cujo Produto/Estoque some é removido automaticamente com toast (`aria-live="polite"`) explicando por quê (FR-21). |
| `cart-badge` | Ícone de Carrinho (rail/bottom nav), qualquer superfície | Atualiza o número a cada adição/remoção; some por completo quando o Carrinho fica vazio (nunca mostra "0"). Sempre acompanhado do toast de confirmação da linha acima — o badge muda de número, mas não é o único sinal de que a ação aconteceu (ver Accessibility Floor). |
| Badge de status de Pedido | Meus Pedidos, Fila | `status-pendente`/`status-aprovado`/`status-rejeitado` — sempre ícone + texto (nunca só cor), texto na variante `text-on-tint-*` (ver DESIGN.md). |
| Linha de aprovação item-a-item | Fila de Pedidos | Cada item mostra "Solicitado: X · Disponível: Y" quando há divergência; almoxarife aprova parcial ou rejeita item a item — nunca um botão único "Aprovar tudo" que esconde a divergência (realiza FR-25/UJ-2). |
| Botão "Baixar recibo" | Meus Pedidos, Fila (Pedido já decidido) | Gera o PDF (FR-26) a partir dos dados **já registrados no Pedido no momento da decisão** — se o Produto foi editado depois, o recibo não muda (AD-17); não é um "preço ao vivo". |
| Relatório de importação | Catálogo → Importar | Tabela criados/atualizados/rejeitados + CTA "Verificar duplicatas agora" (link direto para Normalização com análise já disparada — UJ-3). |
| Linha de sugestão de Normalização | Normalização | Mostra produto, campo, valor sugerido; ações inline "Aceitar" / "Ignorar" — nunca abre modal para uma correção simples. |
| Configuração de MFA (TOTP) | Configurações → Segurança | QR Code do autenticador + campo de código de confirmação. Para `gestor`/`adm` entrando por senha, a tela é forçada (não pulável) até configurar; para `usuario`/`almoxarife`, é opcional (FR-37). Login via SSO nunca mostra esta tela — o MFA já é do realm Keycloak corporativo. |
| Indicador de atualização em tempo real | Catálogo, Estoques, Movimentações, Fila de Pedidos | Toast discreto (`aria-live="polite"`, ex. "Catálogo atualizado.") quando um evento SSE chega em qualquer um dos 4 canais (AD-3: produtos/estoques/movimentacoes/pedidos) — nunca recarrega a tela sozinho; usuário decide quando olhar. |

## State Patterns

| Estado | Superfície | Tratamento |
|---|---|---|
| Carregamento inicial | Qualquer lista | `Skeleton` (shadcn) no formato esperado do conteúdo (linhas de tabela ou cards). |
| Catálogo vazio (filtro sem resultado) | Catálogo | "Nenhum produto encontrado para '{busca}'." Sem ilustração, sem CTA de "criar produto" para `usuario` (papel não pode). |
| Carrinho vazio | Carrinho | "Seu carrinho está vazio. Busque um produto ou aponte a câmera para um código." |
| Item adicionado/removido do Carrinho | Catálogo, Carrinho | Toast (`aria-live="polite"`) confirma a ação em texto ("Adicionado ao Carrinho.") — o `cart-badge` mudando de número nunca é o único sinal; leitor de tela e uso sem olhar constantemente para o ícone precisam do toast (achado de revisão de acessibilidade). |
| Câmera do scanner não reconhece / sem permissão / sem hardware | Catálogo, Carrinho | Mensagem clara + o campo de busca por texto continua disponível e em foco — o scanner nunca é o único caminho para encontrar um Produto (ver Interaction Primitives). |
| Importação interrompida | Catálogo → Importar | Ao reabrir, banner: "Última importação parou na linha N. Continuar de onde parou?" — nunca reprocessa do zero (FR-10 edge case). |
| Aprovação parcial disponível | Fila de Pedidos | Nunca sucesso silencioso — item com estoque insuficiente sempre exige decisão explícita do almoxarife (FR-25). |
| Conta bloqueada por tentativas de login (FR-36) | Login | Mensagem explica o bloqueio temporário sem revelar o tempo exato restante em segundos (evita ataque de timing), com opção de "Esqueci minha senha" sempre visível. |
| MFA obrigatório não configurado (FR-37) | Qualquer tela, para `gestor`/`adm` por senha | Redireciona para Configurações → Segurança antes de liberar qualquer ação restrita ao papel — a navegação normal fica bloqueada até configurar, não escondida. |
| Atualização em tempo real chegou | Catálogo, Estoques, Movimentações, Fila | Toast informativo (`info`, texto sempre em `{colors.text-on-tint-info}` quando sobre fundo tintado), nunca bloqueia a tela; dado antigo continua visível até o usuário atualizar. |
| Reconexão SSE em andamento (lenta, não caiu de vez) | Qualquer tela com dado ao vivo | Um indicador discreto e persistente ("Reconectando...") aparece se a reconexão levar mais que alguns segundos — o usuário nunca fica olhando para um dado potencialmente obsoleto sem nenhum sinal (achado de revisão de acessibilidade: silêncio total durante reconexão lenta é uma falha). Reconexão rápida continua silenciosa. |
| Sem conexão | Global | Toast único: "Sem conexão. Tentando reconectar." Ações que dependem de escrita ficam desabilitadas até reconectar — o stockflow não é offline-first (fora do MVP). |
| Permissão negada (papel insuficiente) | Navegação | Item simplesmente não aparece (ver Foundation) — nunca uma tela "Acesso negado". |
| Ação destrutiva pendente de confirmação | Rejeitar Pedido, mesclar/excluir Produto, desativar conta | `AlertDialog` (shadcn) sempre — nunca `window.confirm()` nativo (ver DESIGN.md Do's and Don'ts). |

## Interaction Primitives

- **Toque/clique é a ação primária** — sem gestos exóticos. Sem drag-and-drop em v1.
- **Rail de navegação (desktop):** hover sobre um ícone mostra `Tooltip` com o rótulo do módulo — mesmo padrão do `FB_APU02` (`AppRail`), já que o rail é só ícones sem texto visível permanente.
- **Câmera do scanner (FR-35):** ao tocar no FAB, solicita permissão de câmera se ainda não concedida; se negada, mensagem clara com link para reativar nas configurações do navegador — nunca falha silenciosamente. **A busca por texto do Catálogo sempre aceita o Código de Identificação digitado ou colado manualmente** — o scanner é um atalho, nunca a única forma de localizar um Produto (cobre falha de câmera, reflexo de sol impedindo a leitura, ou usuário sem condição de usar a câmera).
- **Atalhos de teclado (desktop, uso administrativo de João):** `/` foca a busca do Catálogo; `Esc` fecha modal, diálogo ou a câmera do scanner aberta. Não são obrigatórios — mouse/toque sempre funciona igual.
- **Paginação, nunca scroll infinito** — cardápio de até 8.000 produtos (NFR do PRD) exige paginação/virtualização explícita, não carregamento incremental invisível.
- **Banido em qualquer superfície:** `window.confirm()`/`alert()` nativos; modal empilhado em mais de um nível; qualquer ação de escrita sem feedback visível de sucesso/erro.

## Accessibility Floor

Comportamental. Contraste visual em `DESIGN.md` (herda os defaults do shadcn, verificados AA).

- WCAG 2.2 AA em toda a superfície web — incluindo os badges de status, que usam as variantes `text-on-tint-*` do DESIGN.md especificamente porque as cores originais do `FB_APU02` (herdadas para ícone/fundo) não atingem AA como texto (achado de revisão de acessibilidade, contraste medido entre 1.97–3.19:1 antes da correção).
- **Viewport mínimo 360px** (NFR do PRD §8) — nenhuma tela do fluxo principal (busca, carrinho, aprovação, leitura de QR) pode quebrar layout abaixo disso.
- **Alvo de toque ≥ `{spacing.touch-target-min}` (48px)** em todo elemento interativo do fluxo de campo — acima do piso WCAG de 44px, calibrado para uso com luvas ou uma mão só (Mariana/João carregando material). O FAB do scanner fica no canto inferior direito por ser a posição de alcance de polegar mais comum para destros em uma mão; considerar espelhamento para canhoto fica registrado como melhoria futura (ver Deferred).
- Todo badge de status/disponibilidade carrega **ícone + texto**, nunca só cor (daltonismo — verde/vermelho lado a lado nos badges de Pedido e no badge de disponibilidade do Catálogo).
- **Toda confirmação de ação assíncrona usa toast com `aria-live="polite"`** — não só a atualização em tempo real: adicionar/remover do Carrinho, aprovar/rejeitar Pedido, importação concluída, foto enviada. Nenhuma mudança de estado depende só de um elemento visual (badge, contador) mudando silenciosamente.
- Leitor de tela anuncia a superfície ao navegar: "Catálogo, N produtos encontrados." / "Fila de Pedidos, N pendentes."
- Ordem de tab segue a ordem de leitura em toda superfície; `Esc` sempre fecha o modal/diálogo/câmera no topo.
- O scanner de QR Code nunca é uma dependência obrigatória de nenhum fluxo — todo caminho que ele acelera tem uma alternativa por texto (ver Interaction Primitives).

## Responsive & Platform

| Breakpoint | Comportamento |
|---|---|
| `≥ lg` (1024px+) | Rail + abas horizontais ou submenu vertical (conforme densidade do módulo — Estoques/Normalização usam submenu vertical de 224px, Catálogo/Pedidos usam abas horizontais). Tabelas mostram todas as colunas. |
| `md` (768–1023px) | Mesmo shell do `lg`, tabelas priorizam colunas essenciais (demais atrás de "Mostrar mais"). |
| `< md` (`sm`, a partir de 360px) | Rail vira bottom nav (Catálogo/Carrinho/Pedidos/Mais); tabelas viram cards empilhados; `fab-scanner` sempre visível nas superfícies aplicáveis. |

Câmera do scanner exige contexto seguro (HTTPS) — mesmo requisito técnico que motivou o uso de `@noble/hashes` no PKCE do FB_APU02 (Architecture AD-7); ambiente de desenvolvimento local precisa de HTTPS ou a feature fica indisponível ali.

## Inspiration & Anti-patterns

- **Herdado do `FB_APU02`:** o conceito de rail de ícones + abas por módulo, navegação com visibilidade condicionada a papel (itens somem, nunca aparecem desabilitados), toasts via `sonner` para toda ação assíncrona.
- **Rejeitado — `window.confirm()`/`alert()` nativos:** débito de UX identificado no próprio `FB_APU02` (usado ali em vez do `AlertDialog` já instalado). O stockflow corrige isso desde o início — toda ação destrutiva usa `AlertDialog`.
- **Rejeitado — mistura de estilo "shadcn moderno" vs. "Tailwind cru"** encontrada em páginas mais antigas do `FB_APU02` (tabela HTML crua, spinner customizado, banners inline de sucesso/erro). O stockflow padroniza 100% no vocabulário shadcn (`Card`, `Table`, `Skeleton`, `Toast`) desde a primeira tela — não é um débito a herdar.
- **Rejeitado — sucesso parcial silencioso na aprovação de Pedido:** o próprio protótipo Firebase atual do stockflow tinha esse defeito (pulava item sem estoque sem avisar); o design corrige isso explicitamente na Fila de Pedidos (ver Component Patterns).

## Key Flows

### UJ-1 — Mariana, engenheira de obra, encontra material sobrando de outra obra (mobile, campo)

1. Mariana abre o stockflow no celular (já autenticada, sessão de 2h ainda válida). Bottom nav mostra Catálogo como aba ativa.
2. Toca no campo de busca, digita "tubo pvc 100mm" — autocomplete aparece após 2-3 caracteres.
3. Toca em "Com estoque" no filtro rápido; a lista vira cards com badge verde "Disponível".
4. Abre o card do produto — detalhe mostra 40m divididos entre dois Estoques (canteiros).
5. **Climax:** toca "Reservar" — um toast confirma "Adicionado ao Carrinho." e o ícone de Carrinho na bottom nav ganha o badge "1" instantaneamente. Ela não precisou sair da tela de detalhe para confirmar que o item entrou no Carrinho.

**Edge case:** produto não existe → autocomplete não retorna nada; nenhuma sugestão de "comprar externamente" (fora do escopo do sistema).

### UJ-2 — João, almoxarife, aprova um pedido de retirada (tablet/desktop, almoxarifado)

1. João recebe um toast de atualização em tempo real ("Fila de Pedidos atualizada.") enquanto está em outra tela.
2. Navega para Pedidos → Fila (aba visível só para `almoxarife`+). Filtra "Pendentes".
3. Abre o pedido de Mariana — cada item numa linha própria.
4. Um item mostra "Solicitado: 10m · Disponível: 4m" (divergência revalidada no servidor no momento da abertura).
5. João toca "Aprovar os 4m disponíveis" — confirmação explícita, sem `AlertDialog` (não é destrutivo, é uma decisão de dados, feedback via toast basta).
6. **Climax:** o badge do pedido muda de `status-pendente` para `status-aprovado` na hora, sem recarregar a página; o item que faltou aparece marcado como pendência separada, nunca escondido.

**Edge case:** João cancela antes de confirmar → nada é debitado, pedido continua pendente.

### UJ-3 — João cadastra 200 itens de uma obra encerrada via planilha (desktop, escritório do almoxarifado)

1. Catálogo → Importar (aba só para `almoxarife`+). Seleciona a planilha exportada.
2. Barra de progresso mostra linhas processadas; João não perde a tela de espera sem indicação.
3. Relatório final: tabela com criados/atualizados/rejeitados, mais o CTA "Verificar duplicatas agora".
4. **Climax:** toca o CTA — vai direto para Normalização → Duplicatas, já com a análise em andamento (não precisa navegar manualmente nem lembrar de rodar a checagem depois).

**Edge case:** importação cai no meio → ao reabrir a tela de Importar, um banner mostra até onde chegou e oferece continuar, nunca reprocessa linhas já salvas.

### UJ-4 — João limpa inconsistências de nomenclatura acumuladas (desktop)

1. Normalização → Inconsistências. Toca "Analisar todos os produtos".
2. Lista de sugestões aparece linha a linha (produto, campo, valor sugerido).
3. Aceita em lote as óbvias (checkbox + "Aplicar selecionadas"), toca "Ignorar" nas que não se aplicam.
4. **Climax:** as sugestões óbvias somem da lista assim que aplicadas em lote — o catálogo já reflete a limpeza sem precisar recarregar a tela manualmente.

**Edge case:** um produto que João marcou "Ignorar" para um valor específico recebe depois uma edição que introduz um valor inconsistente diferente — a sugestão reaparece (FR-18: "ignorar" vale só para o valor já visto, não bloqueia detecções futuras naquele campo). João não fica surpreso, porque a mensagem da sugestão explica que é um valor novo, não a mesma de antes.

### UJ-5 — Carlos, colaborador Ferreira Costa, entra pelo login corporativo (SSO) (desktop ou mobile)

1. Carlos abre o stockflow deslogado — tela de Login mostra e-mail/senha como padrão visível, com o botão "Entrar com Ferreira Costa" abaixo (nunca redirecionamento automático — decisão deliberada, diferente do `FB_APU02`).
2. Toca no botão SSO — vai para o Keycloak corporativo, autentica normalmente (inclusive MFA, já imposto pelo realm para `gestor`/`adm`).
3. Retorna ao stockflow — tela de transição breve (spinner) enquanto o token é trocado.
4. **Climax:** Carlos cai direto na primeira tela pós-login (Catálogo), já com o papel que tinha antes (`usuario`/`almoxarife`/`gestor`/`adm`) — nenhuma tela extra pedindo para "completar o cadastro".

**Edge case:** o login via SSO é recusado em duas condições distintas, cada uma com mensagem própria — (1) e-mail do Keycloak não bate com nenhuma conta local → tela de erro orientando a se cadastrar primeiro (FR-3), nunca cria conta automaticamente; (2) e-mail encontrado, mas não marcado como verificado no token do Keycloak (`email_verified=false`) → tela de erro distinta, orientando a confirmar o e-mail corporativo antes de tentar de novo — nunca autentica mesmo achando a conta.

## Fluxos de apoio sem protagonista nomeado

Mais curtos que os UJs acima — sem narrativa completa, mas cada um precisa de uma superfície e um comportamento definidos (fecham a Information Architecture).

- **Autocadastro (FR-3):** tela pública, campos nome/e-mail/senha, sem campo de papel. Envio mostra "Verifique seu e-mail para confirmar a conta." — primeiro login só libera depois da confirmação por e-mail.
- **Esqueci minha senha (FR-32):** link na tela de Login → informa e-mail → mensagem genérica ("Se o e-mail existir, você receberá um link.") independente de a conta existir ou não. Uma conta que só usava SSO até então pode usar este fluxo para criar uma senha própria pela primeira vez.
- **Solicitar promoção de papel (FR-33):** dentro de Meu Perfil, botão "Solicitar promoção" (o sistema já sabe qual o próximo papel); botão fica desabilitado enquanto uma solicitação está pendente; decisão (aprovação/rejeição) chega por toast na próxima vez que o Usuário abrir o app.
- **Baixar recibo do Pedido (FR-26):** botão "Baixar recibo" em qualquer Pedido já decidido (aprovado/parcialmente aprovado) — gera e baixa o PDF diretamente, sem tela intermediária.
- **Consultar Log de Acesso (FR-38):** tela só para `adm`, tabela com usuário/timestamp/IP/método (senha ou SSO), filtrável por período — somente leitura, sem ação de edição/exclusão possível na interface (log é append-only).
- **Exportar/excluir dados pessoais — LGPD (FR-39):** dentro de Meu Perfil, "Baixar meus dados" (qualquer Usuário, gera arquivo com nome/e-mail/histórico de ações); "Solicitar exclusão de conta" aciona um pedido que só `adm` processa (fora da interface do próprio Usuário, por decisão de FR-39 de preservar vínculo de auditoria).

## Deferred

- **Espelhamento do `fab-scanner` para canhotos:** o FAB fica fixo no canto inferior direito (alcance de polegar padrão destro). Não há toggle de posição em v1 — revisitar se usuários canhotos relatarem dificuldade real de uso em campo.
- **Estados fora dos 5 UJs nomeados:** superfícies administrativas menos críticas (Estoques, Relatórios, Log de Acesso, LGPD) têm IA e Component/State Patterns definidos, mas não um fluxo narrado completo — comportamento suficiente para implementação, mockup visual fica para quando/se o produto passar por uma rodada de mockups de telas-chave.
