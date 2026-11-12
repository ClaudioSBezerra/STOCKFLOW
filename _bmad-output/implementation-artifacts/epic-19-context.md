# Epic 19 Context: Catálogo 2.0 — Novo Sistema de Design

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Adotar o "Catálogo 2.0 Design System" (Ferreira Costa, criado por Marlos em 2026-11-11, já em produção no projeto irmão FB_APU05) em todo o stockflow, substituindo o sistema de design próprio do Epic 17. Mudança 100% visual/presentation — nenhuma regra de negócio, rota ou permissão muda.

## Stories

- Story 19.1: Tokens de cor e fonte do Catálogo 2.0 (concluída, commit 5bc5a32)
- Story 19.2: Menu lateral e topo no novo padrão visual
- Story 19.3: Páginas de lista no novo padrão visual
- Story 19.4: Formulários e diálogos no novo padrão visual
- Story 19.5: Validação visual final e limpeza

## Requirements & Constraints

- Preservar integralmente a estrutura de navegação/layout do Epic 17 (grupos de menu, rotas próprias, breakpoints mobile/desktop) — só a linguagem visual muda.
- Qualquer token de cor usado deve vir do Catálogo 2.0 (fonte real: `/home/claudio/projetos/FB_APU05/frontend/src/styles/catalogo-tokens.css`, cópia local idêntica ao servidor hostinger-fc), nunca um valor inventado.
- Mudanças de cor/tipografia/raio exigem documentação no `DESIGN.md` (addendum "Catálogo 2.0", seção já criada na Story 19.1) antes/junto do código.
- Contraste de texto normal deve atingir AA (4.5:1) em qualquer elemento novo, EXCETO onde já exista um precedente documentado de trade-off aceito pela própria Ferreira Costa em produção (caso do botão primário verde, ver Technical Decisions).

## Technical Decisions

- Story 19.1 já portou `--color-primary/accent/destructive/background/foreground/secondary/border/success/warning/info/muted/input/ring` em `frontend/src/index.css` para os valores do Catálogo 2.0, mantendo os nomes de variável shadcn. NÃO tocou em `--color-sidebar*` nem em tipografia/raios/espaçamentos/sombras/badges/score (deferred, ver `deferred-work.md`).
- **Lacuna de design confirmada:** o Catálogo 2.0 não define nenhum token de "sidebar escura". O FB_APU05 (`AppRail.tsx`) usa um rail CLARO (`bg-card`, item ativo com `bg-cta-soft`/`text-cta-pressed`, verde claro) — não uma barra azul-marinho. Decisão tomada (2026-11-12): o menu do stockflow converte para esse mesmo padrão claro, abandonando o azul-marinho do Epic 17, em vez de manter o escuro só com ajustes pontuais.
- A barra de 3px do item ativo do menu (classe `.sidebar-item-active` em `index.css`) já usa `var(--color-primary)` diretamente — a Story 19.1 já a tornou verde automaticamente (efeito colateral correto: no Catálogo 2.0, verde = estado ativo/CTA; vermelho é reservado para marca/ações destrutivas).
- Trade-off de contraste JÁ ACEITO (Story 19.1, não repetir a pergunta): botão primário verde (#68a439) + texto branco não atinge AA, mas é a mesma combinação usada em produção no FB_APU05 — mantido de propósito. Esse mesmo relaxamento NÃO se aplica ao menu claro desta story, que não tem precedente equivalente de "a empresa já aceita esse contraste" — aqui o contraste deve passar em AA de verdade.
- Projeto usa Tailwind v4 (`@theme` CSS-first em `index.css`, sem `tailwind.config.js`) — qualquer variável nova precisa entrar nessa sintaxe, não copiar a abordagem Tailwind v3/JS do FB_APU05.

## UX & Interaction Patterns

- Menu lateral agrupado (Epic 17): grupos com `aria-expanded`, item ativo com `aria-current="page"`, grupo do item ativo abre sozinho. Recolhido (desktop, 64px) mostra só ícones + tooltip. Celular (<768px): gaveta via ☰, fecha com Esc/toque fora, foco retorna ao ☰ ao fechar.
- Componente principal: `frontend/src/components/shell/MenuLateral.tsx` (usa classes `bg-sidebar`, `text-sidebar-foreground`, `sidebar-item-active`, `sidebar-group-title`, `sidebar-scroll`, todas definidas em `index.css`).

## Cross-Story Dependencies

- Todas as stories dependem da 19.1 (tokens base) já estar concluída — está.
- 19.3/19.4 vão reusar qualquer padrão de "card claro com borda sutil" que a 19.2 estabelecer para o menu, para manter consistência visual entre navegação e conteúdo.
