# Epic 19 Context: Catálogo 2.0 — Novo Sistema de Design

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Trocar a linguagem visual do stockflow (cores, tipografia, espaçamentos, raios, sombras) pela do "Catálogo 2.0 Design System" (Marlos, 2026-11-11), já adotado no projeto irmão FB_APU05, para que os sistemas internos da Ferreira Costa se leiam como uma única família. É um épico 100% visual: nenhuma regra de negócio, rota, permissão ou fluxo muda, e a arquitetura de navegação entregue no Epic 17 (menu lateral agrupado, páginas de lista com KPIs/pílulas/tabela) é preservada integralmente — só a pele visual é substituída.

## Stories

- Story 19.1: Tokens base do Catálogo 2.0 no Tailwind v4
- Story 19.2: Menu lateral e topo no novo padrão visual
- Story 19.3: Páginas de lista no novo padrão visual
- Story 19.4: Formulários e diálogos no novo padrão visual
- Story 19.5: Validação visual final e limpeza

## Requirements & Constraints

- As variáveis shadcn existentes (`--color-primary`, `--color-accent`, `--color-destructive`, `--color-sidebar`, `--color-sidebar-item-active`, `--color-secondary`, `--color-border`, etc.) mantêm o MESMO nome, só com valor novo — nenhum componente pode precisar trocar a classe Tailwind que consome.
- `npm run build` e `npm test` continuam limpos durante e depois da migração.
- Inter passa a ser servida localmente via `@fontsource-variable/inter` (dependência nova em `frontend/package.json`, import em `main.tsx`) — nenhuma chamada a `fonts.googleapis.com` em nenhum momento (hoje o stockflow já não carrega Google Fonts; a regra é não introduzir uma).
- Tokens semânticos de alerta (warn/err/suc/info) e de score (low/medium/good/excellent) entram como `@theme` reutilizáveis na 19.1 mesmo sem consumidor ainda; só passam a ser usados nas stories seguintes.
- `DESIGN.md` (29/08) recebe um addendum (origem Catálogo 2.0 + tabela de mapeamento tokens-antigos → tokens-novos) — não é reescrito do zero.
- Na 19.5, nenhum token do `DESIGN.md` antigo que tenha ficado sem nenhum componente consumindo pode permanecer em `index.css`.

## Technical Decisions

- **Diferença de arquitetura crucial:** stockflow usa Tailwind v4, sintaxe `@theme` CSS-first em `frontend/src/index.css`, sem `tailwind.config.js`. O FB_APU05 (origem dos tokens) usa Tailwind v3, com `tailwind.config.js` + mapeamento shadcn em HSL. Não há `tailwind.config.js` para criar nem HSL para converter aqui — os valores hex de `catalogo-tokens.css` são portados manualmente, um a um, para dentro do bloco `@theme` já existente em `index.css`, mantendo os nomes de variável atuais.
- O arquivo fonte de verdade (`catalogo-tokens.css`, FB_APU05) **não é importado** pelo stockflow — é só referência de leitura para extrair valores. Cópia local: `/home/claudio/projetos/FB_APU05/frontend/src/styles/catalogo-tokens.css`; mesma cópia no servidor `hostinger-fc` em `/opt/apps/fb_apu05/current/frontend/src/styles/catalogo-tokens.css`.
- Mapeamentos confirmados pelo próprio épico: `--color-primary` (CTA, hoje `#e62019`) passa a referenciar o verde `--green-150` (`#68a439`) como CTA principal; `--color-destructive` (hoje `#ef4343`) passa a referenciar `--brand-red` (`#e71c35`). Os demais nomes de variável (`--color-sidebar`, `--color-sidebar-item-active`, `--color-secondary`, `--color-border`, `--color-accent`) mantêm o nome mas o épico não fixa o valor exato — escolher dentre os neutros/semânticos do Catálogo 2.0 (`--fg-1` `#252728`, `--fg-secondary` `#4f5256`, `--br-soft` `#e5e6e6`, `--br-strong` `#d4d5d6`, `--bg-soft` `#f6f7f7`, `--bg-canvas` `#f4f5f6`) pelo propósito mais próximo, não pela cor "parecida".
- Paleta completa disponível para consulta: brand (`--brand-red #e71c35`, `--brand-red-strong #b52a40`, `--brand-red-tint #fef4f5`), verde primário (`--green-50/150/250/350`, `--green-l550/l700/l850`), azul secundário (`--blue`, `--blue-d50/150/250/350`, `--blue-l550/650/750/850/950`), alertas (`--warn-*`, `--err-*`, `--suc-*`, `--info-*`), score (`--score-low/medium/good/excellent/off`), badges prontos (`--badge-blue/gray/green/red/yellow-bg/br/tx`), neutros (`--fg-1` a `--fg-ghost`, `--br-strong/soft/faint`, `--bg-canvas/soft/card`). Tipografia: `--font-family` (Inter Variable), `--fs-9` a `--fs-64`, `--fw-light` a `--fw-bold`; classes utilitárias `.t-h1`..`.t-h4`, `.t-body`/`.t-body-secondary`, `.t-label`/`.t-helper`/`.t-error`, `.t-table-th`/`.t-table-td`, `.t-badge`. Raios: `--r-button 4px`, `--r-card 8px`, `--r-pill 24px`, `--r-avatar 100px` (escala mais angulosa que a atual, onde botão/input era 10px). Sombras: `--shadow-card`, `--shadow-modal`, `--shadow-popover`.
- Os badges antigos (`status-pendente/aprovado/rejeitado/disponivel/inativo`) dependiam de um fundo tintado a 10% + uma variante `text-on-tint-*` mais escura só para corrigir contraste AA (achado de revisão de 29/08). Os `--badge-*` do Catálogo 2.0 já vêm com `-bg`/`-br`/`-tx` prontos e AA-safe — a variante `text-on-tint-*` fica sem função e é candidata a remoção na 19.5.
- Tokens de layout específicos do stockflow/Epic 17 (`--spacing-sidebar-width` 240/64, `--spacing-topbar-height` 56, `table-row-height` 60, `fab-size`, `fab-margin`, `touch-target-min`) não têm equivalente no Catálogo 2.0 (que só define uma escala genérica `--sp-tiny`..`--sp-huge`) — permanecem como estão; só a escala genérica de espaçamento e a tipografia entram vindas do Catálogo 2.0.
- Conceito de "workspace" do FB_APU05 (`--ws-business`, `--ws-catalogo` etc.) não existe no stockflow e não deve ser portado.

## UX & Interaction Patterns

- A arquitetura de navegação do Epic 17 (grupos do menu, rotas próprias, permissões, menu recolhido, gaveta no celular, abrir/fechar por ☰/Esc/toque fora, retorno de foco) é 100% preservada — a 19.2 só troca cor/tipografia do mesmo shell.
- A barra de 3px do item ativo do menu lateral muda de vermelho antigo (`--color-primary` `#e62019`) para `--brand-red` (`#e71c35`) — mesmo tratamento visual (negrito branco, fundo destacado, `aria-current="page"`), só a cor da barra muda.
- Faixa de KPIs, pílulas de status e tabela de 60px das páginas de lista (Epic 17/17.3) mantêm números, cálculos e lógica de exibição — só os tokens visuais (`bg-card`/`shadow-card`, cores de badge, `t-table-th`/`t-table-td`, `--br-soft` como divisor) mudam.
- Diálogos de confirmação (`AlertDialog`, já em uso desde o DESIGN.md original) mantêm textos e ações; só raio (`--r-card`) e sombra (`--shadow-modal`) mudam.
- Relatório de importação (criados/atualizados/rejeitados) passa a usar as cores semânticas suc/err/warn do Catálogo 2.0 no lugar das cores antigas de success/warning/destructive.

## Cross-Story Dependencies

- A 19.1 é pré-requisito direto de 19.2, 19.3 e 19.4 — todas consomem variáveis `@theme` (incluindo as semânticas de alerta/score reservadas sem uso ainda na 19.1).
- A 19.5 só roda depois de 19.1–19.4 concluídas: validação visual comparativa (antes/depois) e limpeza de tokens do `DESIGN.md` antigo sem consumidor.
- Depende da arquitetura de navegação e dos componentes de página de lista já entregues no Epic 17 — este épico reskina, não reestrutura.
