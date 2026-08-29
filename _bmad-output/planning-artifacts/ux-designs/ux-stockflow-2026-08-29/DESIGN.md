---
name: stockflow
description: Ferramenta interna Ferreira Costa para controle de sobras de material de obra. shadcn/ui sobre React + Tailwind, herdando a identidade visual já em produção no FB_APU02; este DESIGN.md registra os tokens herdados e as adições próprias do produto (FAB do scanner, badges de status de Pedido e de disponibilidade do Catálogo).
colors:
  primary: '#E62019'
  primary-foreground: '#FFFFFF'
  accent: '#16A249'
  accent-foreground: '#FFFFFF'
  destructive: '#EF4343'
  destructive-foreground: '#FFFFFF'
  background: '#F9FAFB'
  foreground: '#0F1729'
  card: '#FFFFFF'
  secondary: '#F1F5F9'
  border: '#E1E7EF'
  success: '#16A249'
  warning: '#F59F0A'
  info: '#0DA2E7'
  text-on-tint-success: '#166534'
  text-on-tint-warning: '#92400E'
  text-on-tint-info: '#075985'
  text-on-tint-destructive: '#B91C1C'
typography:
  sans:
    fontFamily: 'Inter, system-ui, sans-serif'
  mono:
    fontFamily: 'JetBrains Mono, monospace'
  heading-lg:
    fontFamily: '{typography.sans.fontFamily}'
    fontSize: 30px
    fontWeight: '700'
    lineHeight: '1.2'
    letterSpacing: -0.01em
  heading-md:
    fontFamily: '{typography.sans.fontFamily}'
    fontSize: 24px
    fontWeight: '700'
    lineHeight: '1.25'
  body:
    fontFamily: '{typography.sans.fontFamily}'
    fontSize: 14px
    fontWeight: '400'
    lineHeight: '1.5'
  label:
    fontFamily: '{typography.sans.fontFamily}'
    fontSize: 12px
    fontWeight: '500'
    lineHeight: '1.4'
  code:
    fontFamily: '{typography.mono.fontFamily}'
    fontSize: 13px
    fontWeight: '400'
rounded:
  sm: 8px
  md: 10px
  lg: 12px
  DEFAULT: 12px
  full: 9999px
spacing:
  rail-width: 56px
  bottom-nav-height: 56px
  fab-size: 56px
  fab-offset-mobile: 72px
  fab-margin: 16px
  sidenav-width: 224px
  touch-target-min: 48px
components:
  button-primary:
    background: '{colors.primary}'
    foreground: '{colors.primary-foreground}'
    radius: '{rounded.md}'
  fab-scanner:
    background: '{colors.primary}'
    foreground: '{colors.primary-foreground}'
    radius: '{rounded.full}'
    elevation: 'shadow-lg'
  cart-badge:
    background: '{colors.destructive}'
    foreground: '{colors.destructive-foreground}'
    radius: '{rounded.full}'
  nav-item-active:
    background: '{colors.primary}/10'
    foreground: '{colors.primary}'
  status-pendente:
    background: '{colors.warning}/10'
    foreground: '{colors.text-on-tint-warning}'
    radius: '{rounded.full}'
  status-aprovado:
    background: '{colors.success}/10'
    foreground: '{colors.text-on-tint-success}'
    radius: '{rounded.full}'
  status-rejeitado:
    background: '{colors.destructive}/10'
    foreground: '{colors.text-on-tint-destructive}'
    radius: '{rounded.full}'
  status-disponivel:
    background: '{colors.accent}/10'
    foreground: '{colors.text-on-tint-success}'
    radius: '{rounded.full}'
status: final
created: '2026-08-29'
updated: '2026-08-29'
---

# stockflow — Design Spine

## Brand & Style

O stockflow é uma ferramenta de trabalho, não um produto de consumo — usada por engenheiros de obra e almoxarifes para resolver uma tarefa concreta (achar material, aprovar retirada, organizar catálogo), muitas vezes com o celular na mão, no canteiro, sob sol ou pressa. A postura visual é **utilitária e confiável**, não decorativa: densidade de informação é aceitável e esperada (tabelas agrupadas, listas de pedidos, histórico de movimentação), e nenhum elemento decorativo compete com a tarefa.

O stockflow herda **por completo** o sistema shadcn/ui + Tailwind já em produção no `FB_APU02` — outro sistema interno da Ferreira Costa — porque os dois devem se ler como parte da mesma família de ferramentas internas, não como produtos de marcas diferentes. Este DESIGN.md registra o que foi herdado literalmente (cores, tipografia, radius) e as únicas duas adições próprias do stockflow: o botão flutuante do scanner de QR Code (padrão novo, o `FB_APU02` não tem scanner) e os badges de status de Pedido (pendente/aprovado/rejeitado — vocabulário de estado que o `FB_APU02` não precisa expressar).

## Colors

- **Primary — vermelho Ferreira Costa (`#E62019`, `primary-foreground` `#FFFFFF`)** — herdado do `FB_APU02`. Usado em botões primários, item de navegação ativo, e o FAB do scanner (sempre com texto/ícone branco sobre o vermelho sólido — essa combinação passa AA com folga). Não usado para indicar erro ou rejeição — ver Do's and Don'ts, risco real de confusão com `destructive`.
- **Accent / Success — verde (`#16A249`, `accent-foreground` `#FFFFFF`)** — mesma cor serve dois papéis herdados do `FB_APU02`: indicador de sucesso (badge "aprovado") e "disponível em estoque" no catálogo (`status-disponivel`). Como fundo sólido (ex. um selo cheio), usa `accent-foreground` branco; como fundo tintado a 10% (o caso mais comum, badges pill), o texto usa `{colors.text-on-tint-success}` — ver nota de contraste abaixo.
- **Destructive — vermelho-alaranjado (`#EF4343`, `destructive-foreground` `#FFFFFF`)** — reservado para erro, rejeição de Pedido, e a ação de exclusão/mesclagem. **Deliberadamente distinto do Primary** apesar de ambos serem "vermelhos" — nunca usar `primary` onde o significado é "algo deu errado ou foi rejeitado". Em badge tintado, o texto usa `{colors.text-on-tint-destructive}`, não a cor sólida.
- **Warning — âmbar (`#F59F0A`)** — badge "pendente" de Pedido, alertas de estoque baixo/validade. Texto em badge sempre `{colors.text-on-tint-warning}` — ver nota de contraste abaixo.
- **Info — azul (`#0DA2E7`)** — notificações neutras (ex. toast de atualização em tempo real via SSE), nunca usado em botão de ação. Texto sobre fundo tintado usa `{colors.text-on-tint-info}`.
- **Background/Foreground/Card/Secondary/Border** — herdados sem alteração do `FB_APU02` (`#F9FAFB` / `#0F1729` / `#FFFFFF` / `#F1F5F9` / `#E1E7EF`).

**Nota de contraste (achado de revisão de acessibilidade, 2026-08-29):** `warning`, `info`, `success` e `destructive`, do jeito que existem no `FB_APU02`, foram desenhados para uso em ícone/fundo sólido — como texto direto sobre um fundo tintado a 10% (o padrão dos badges pill deste produto), o contraste cai para 1.97–3.19:1, abaixo do mínimo AA (4.5:1) e inaceitável para leitura em campo sob sol forte. Por isso este DESIGN.md define quatro tokens `text-on-tint-*` — versões mais escuras (`#166534`, `#92400E`, `#075985`, `#B91C1C`) usadas **exclusivamente como texto sobre o próprio fundo tintado da mesma família de cor**. A cor de marca original nunca muda; só o texto do badge fica mais escuro que o ícone/indicador que o acompanha.

Evitar: introduzir qualquer cor de marca nova além destas; usar `primary` para estado de erro (usar `destructive`); texto de badge na cor sólida `warning`/`info`/`success`/`destructive` diretamente sobre fundo tintado (usar sempre a variante `text-on-tint-*`); gradientes ou cores decorativas sem significado semântico.

## Typography

**Inter** (sans, herdado do `FB_APU02`) para toda a interface. **JetBrains Mono** (herdado do `FB_APU02`, lá usado esparsamente) ganha um papel mais consistente aqui: todo código/identificador — código de Produto (FR-8), UUID exibido em tela de auditoria, valor lido do QR Code/código de barras (FR-35) — usa `{typography.code}`, nunca a fonte sans, para diferenciar visualmente "dado que é um identificador" de "texto legível".

- `{typography.heading-lg}` — título de página (ex. "Catálogo", "Pedidos de Retirada").
- `{typography.heading-md}` — título de seção/card dentro de uma página.
- `{typography.body}` — texto padrão de interface, tabelas, formulários.
- `{typography.label}` — rótulos de campo, legendas, texto secundário (`text-muted-foreground` no shadcn).
- `{typography.code}` — códigos, SKUs, UUIDs, valor de QR Code lido.

## Layout & Spacing

Escala de espaçamento Tailwind padrão (4, 8, 12, 16, 20, 24, 32...) herdada sem alteração. Sem `max-width` de conteúdo fixo — ao contrário de um produto de leitura, o stockflow é um produto de tabela/dado denso (catálogo com dezenas de colunas configuráveis, histórico de movimentação); forçar coluna única desperdiçaria a tela em desktop, onde João frequentemente trabalha (cadastro em lote, importação, normalização).

- Rail de navegação desktop: `{spacing.rail-width}` (56px, idêntico ao `FB_APU02`). Submenu vertical por módulo (Estoques, Normalização): `{spacing.sidenav-width}` (224px, idêntico ao `FB_APU02`).
- Bottom nav mobile: `{spacing.bottom-nav-height}` (56px), mesmo conjunto de ícones do rail, mesmo destaque de item ativo.
- FAB do scanner: `{spacing.fab-size}` (56px) de diâmetro, ancorado no canto inferior direito, `{spacing.fab-margin}` (16px) de margem da borda da tela — nunca sobrepõe a bottom nav (fica acima dela com `{spacing.fab-offset-mobile}`, 72px, em mobile).
- Alvo de toque mínimo: `{spacing.touch-target-min}` (48px) em todo elemento interativo do fluxo de campo — acima do piso WCAG de 44px, calibrado para uso com luvas (ver EXPERIENCE.md Accessibility Floor).

## Elevation & Depth

Herdado do shadcn — sombra sutil em hover/active, sem uso de elevação como hierarquia visual. Única adição: o FAB do scanner usa `shadow-lg` (mais elevado que qualquer outro elemento da interface) — precisa "flutuar" visualmente sobre o conteúdo, já que é a única ação verdadeiramente flutuante do produto.

## Shapes

Radius herdado do `FB_APU02`: `{rounded.DEFAULT}`/`{rounded.lg}` (12px, `--radius` original, os dois nomes apontam para o mesmo valor) em cards, diálogos e contêineres de nível superior em repouso; `{rounded.md}` (10px) em botões e inputs; `{rounded.sm}` (8px) em elementos menores. `{rounded.full}` (pill) reservado para os badges de status de Pedido/Catálogo e o FAB — nunca usado em botões retangulares comuns.

## Components

Herdados do `FB_APU02` sem alteração: `Button`, `Card`, `Table`, `Tabs`, `Dialog`, `Sheet`, `DropdownMenu`, `Tooltip`, `Skeleton`, `Toast` (sonner), `Select`, `Input`, `Checkbox`, `Switch`, `Avatar`.

**Adotado do `FB_APU02`, mas com correção deliberada:** `AlertDialog` — instalado mas nunca usado no `FB_APU02` (que usa `window.confirm()` nativo para confirmações destrutivas). O stockflow **usa `AlertDialog`** para toda ação destrutiva/irreversível (rejeitar Pedido, mesclar/excluir Produto, desativar conta) — ver Do's and Don'ts.

**Componentes próprios do stockflow:**
- **`fab-scanner`** — botão circular flutuante, `{colors.primary}` de fundo, ícone de câmera/QR branco, `{rounded.full}`, `shadow-lg`. Presente apenas nas telas onde faz sentido escanear (Catálogo, Carrinho) — nunca em telas administrativas (Configurações, Relatórios).
- **`cart-badge`** — círculo pequeno `{colors.destructive}` sobre o ícone de Carrinho na navegação, com o número de itens. Some quando o carrinho está vazio (não mostra "0").
- **`status-pendente` / `status-aprovado` / `status-rejeitado`** — badges pill (`{rounded.full}`) para o status de um Pedido, sempre com **ícone + texto**, nunca só cor. Texto usa a variante `text-on-tint-*` correspondente (ver Colors), nunca a cor sólida da marca diretamente sobre o fundo tintado.
- **`status-disponivel`** — mesmo padrão pill+ícone+texto, aplicado ao badge de disponibilidade do Catálogo ("Disponível" / "Sem estoque") — antes só tinha texto diferenciado sem ícone; corrigido para ficar consistente com os badges de Pedido e mais rápido de escanear visualmente sob sol forte.
- **`nav-item-active`** — mesmo tratamento do `FB_APU02`: fundo `{colors.primary}` a 10% de opacidade, texto/ícone em `{colors.primary}` sólido (essa combinação, testada no `FB_APU02` em produção, não teve falha de contraste reportada — mantida como está).

## Do's and Don'ts

| Do | Don't |
|---|---|
| Usar `{colors.destructive}` para toda rejeição/erro/exclusão | Usar `{colors.primary}` (vermelho) para indicar erro — os dois são vermelhos próximos e a confusão é um risco real herdado do `FB_APU02` |
| Badge de status sempre com ícone + texto | Badge de status só com cor (falha de acessibilidade para daltonismo — o produto usa verde/vermelho lado a lado) |
| Texto de badge sempre na variante `text-on-tint-*` | Texto de badge na cor sólida (`warning`/`info`/`success`/`destructive`) direto sobre o fundo tintado — falha de contraste AA confirmada em revisão (1.97–3.19:1) |
| `AlertDialog` (shadcn) para toda confirmação destrutiva | `window.confirm()` nativo — débito de UX já identificado no `FB_APU02`, não repetir aqui |
| `JetBrains Mono` para todo código/SKU/UUID exibido | Misturar a fonte sans em identificadores — dificulta escanear visualmente um código |
| FAB do scanner só em telas de consulta/carrinho | FAB em toda tela (polui a interface administrativa de João) |
| Alvo de toque de `{spacing.touch-target-min}` (48px) no fluxo de campo | Piso WCAG de 44px sem margem — insuficiente para uso com luvas |
| Uma única cor de marca (`primary`) + acentos semânticos | Introduzir uma segunda cor "de marca" além do vermelho Ferreira Costa |
