---
title: 'Story 1.2 — Fundação do shell de navegação e design tokens'
type: 'feature'
created: '2026-08-29'
status: 'done'
baseline_revision: '943d78a47f3507625a8a9131a9e1c9ce96dc1d1a'
review_loop_iteration: 0
followup_review_recommended: true
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md']
warnings: ['oversized']
deferred:
  - summary: >-
      `next-themes` está instalado e `sonner.tsx` chama `useTheme()`, mas
      nenhum `ThemeProvider` é montado em `main.tsx`/`App.tsx` e `index.css`
      só define tokens de tema claro — a capacidade de dark mode existe no
      código gerado pelo `shadcn` CLI mas não faz nada.
    evidence: |-
      `DESIGN.md` não define nenhuma paleta escura (produto utilitário, sem
      requisito de dark mode); `frontend/src/main.tsx` e `App.tsx` não
      importam `ThemeProvider`. Decisão de produto sobre dark mode está fora
      do escopo desta story.
    location: 'frontend/src/components/ui/sonner.tsx'
    severity: low
  - summary: >-
      O serviço `web` do `docker-compose.yml` (Nginx) não tem headers de
      cache/segurança (`Cache-Control`, `X-Content-Type-Options` etc.) nem
      roda como usuário não-root, ao contrário do cuidado já aplicado ao
      `backend/Dockerfile` (usuário `appuser`) na Story 1.1.
    evidence: |-
      `frontend/nginx.conf` só define `try_files` para fallback de SPA;
      `frontend/Dockerfile` não define `USER` na etapa `nginx:alpine` final.
      Hardening de produção é um item de AD-16 (envelope operacional),
      não uma AC desta story.
    location: 'frontend/nginx.conf, frontend/Dockerfile'
    severity: low
  - summary: >-
      Os testes que verificam breakpoint responsivo (rail vs. bottom nav) e
      alvo de toque de 48px checam a presença das classes Tailwind
      (`md:flex`, `min-h-touch-target-min`) em vez do layout/tamanho
      computado real — o `jsdom` não tem motor de CSS/media query, então a
      AC "nenhuma tela quebra o layout abaixo de 360px" não tem cobertura
      automatizada real, só a checagem manual documentada em `## Verification`.
    evidence: |-
      `frontend/src/components/shell/AppShell.test.tsx` e
      `ConfirmDialog.test.tsx` usam `element.className.toContain(...)`:
      prova que a classe certa foi escrita, não que o navegador de fato
      esconde/mostra os elementos nos breakpoints certos. Resolver isso
      exigiria infraestrutura de teste em navegador real (Playwright), que
      não existe em nenhuma story deste projeto ainda.
    location: 'frontend/src/components/shell/AppShell.test.tsx'
    severity: medium
  - summary: >-
      `frontend/.npmrc` define `legacy-peer-deps=true` para todo o projeto,
      mascarando qualquer conflito real de peer dependency entre React
      19.2/TypeScript 7.0/Vite 8.0 (deliberadamente à frente do ecossistema,
      Architecture Stack) e pacotes que ainda não publicaram ranges de peer
      compatíveis.
    evidence: |-
      `npm install` sem essa flag falha por causa de `@vitejs/plugin-react`
      e outros pacotes com peer range desatualizado para as versões pinadas.
      Trade-off inerente à decisão de arquitetura de usar versões de ponta,
      não uma escolha desta story.
    location: 'frontend/.npmrc'
    severity: medium
  - summary: >-
      Não há pipeline de CI que rode `npm run build`/`lint`/`test` do
      frontend automaticamente — mesmo gap já registrado para o backend na
      Story 1.1 (DW-1), agora também presente no frontend.
    evidence: |-
      Não existe `.github/workflows` no repositório. Configurar CI/CD é
      AD-16 (envelope operacional de todo o projeto), não uma AC desta story.
    location: '.github/workflows (inexistente)'
    severity: low
  - summary: >-
      Não há README (ou doc equivalente) explicando como rodar o `frontend/`
      localmente (`npm run dev`) ou o novo serviço `web` do
      `docker-compose.yml` (porta `8081:80`) — mesmo gap já registrado para
      o backend na Story 1.1 (DW-3), agora também presente no frontend.
    evidence: |-
      Não existe `README.md` no repositório documentando nenhum dos dois
      stacks (backend ou frontend).
    location: 'README.md (inexistente)'
    severity: low
  - summary: >-
      Não há checagem automatizada de acessibilidade (ex. `axe-core`/
      `jest-axe`) apesar do `AppShell` compor ARIA não trivial (dois `<nav>`
      com o mesmo `aria-label`, `Tooltip`/`Sheet`/`DropdownMenu`) — os testes
      atuais checam papéis/nomes pontuais, não regressões amplas de a11y.
    evidence: |-
      `frontend/package.json` não inclui `jest-axe`/`@axe-core/react` nem
      equivalente para Vitest. Nenhuma AC desta story exige verificação
      automatizada de acessibilidade além do que já está coberto por
      `@testing-library` (papéis/nomes acessíveis).
    location: 'frontend/src/components/shell/AppShell.test.tsx'
    severity: low
  - summary: >-
      `frontend/nginx.conf`'s `try_files $uri $uri/ /index.html;` faz
      fallback para o SPA em qualquer requisição não encontrada, incluindo
      assets estáticos com hash (JS/CSS) — uma requisição para um asset
      obsoleto/inexistente (ex. após deploy com hash novo) recebe `200
      index.html` em vez de `404`, mascarando problemas de cache/deploy.
    evidence: |-
      `frontend/nginx.conf` não tem um `location` separado para assets
      estáticos com `try_files $uri =404;`. Mesmo tema do item já registrado
      sobre hardening de produção do Nginx (headers de cache/segurança,
      usuário não-root) — AD-16 (envelope operacional), não uma AC desta
      story.
    location: 'frontend/nginx.conf'
    severity: low
  - summary: >-
      `sonner.tsx` (Toaster global) só passa `--normal-bg`/`--normal-text`/
      `--normal-border`/`--border-radius` como CSS custom properties; nenhuma
      das variáveis por-tipo do `sonner` (`--success-bg`, `--error-bg`,
      `--warning-bg`, `--info-bg`) é definida, então toasts de
      success/error/warning/info do stockflow renderizam com a mesma cor de
      fundo, diferindo só pelo ícone — apesar de `index.css` definir tokens
      de tint distintos (`{colors.warning}`, `{colors.info}` etc.) para
      exatamente esse propósito.
    evidence: |-
      `frontend/src/components/ui/sonner.tsx`'s `style` prop no `<Sonner>`
      só popula as 4 variáveis genéricas do `sonner`. Nenhuma AC desta story
      exige toasts com cor por tipo — AC1 só exige que os tokens fiquem
      "disponíveis nas classes Tailwind geradas", não que todo consumidor
      gerado pelo `shadcn` já os utilize (mesmo padrão do item de dark mode
      acima: capacidade presente, não conectada).
    location: 'frontend/src/components/ui/sonner.tsx'
    severity: low
---

<intent-contract>

## Intent

**Problem:** Não existe frontend ainda. Toda story futura (Epic 1 em diante) precisa de um shell de navegação e tokens de design consistentes em vez de estilo ad hoc por tela.

**Approach:** Inicializar `frontend/` (Vite+React+TS, Stack da Architecture), configurar Tailwind CSS v4 (`@theme` CSS-first) + shadcn/ui com os tokens literais do `DESIGN.md`, e construir um `AppShell` responsivo (rail+header no desktop, bottom nav+"Mais" no mobile) mais `ConfirmDialog`/`Toaster` reutilizáveis para toda story seguinte consumir.

## Boundaries & Constraints

**Always:**
- Versões exatas da Architecture Stack: React `19.2.x`, TypeScript `7.0.x`, Vite `8.0.x`, `react-router-dom` `^6.30` (nunca 7.x), Tailwind CSS `^4` CSS-first (`@theme`, sem `tailwind.config.js`), shadcn/ui via CLI `shadcn` (não `shadcn-ui`, depreciado).
- Tokens de `DESIGN.md` (colors, typography, spacing, rounded) traduzidos literalmente para `@theme` em `frontend/src/index.css` — nenhum valor hardcoded fora do token.
- Itens de navegação seguem a IA de `EXPERIENCE.md`: primários (Catálogo, Carrinho, Pedidos) sempre visíveis; administrativos (Estoques, Normalização, Relatórios) atrás de "Mais" (`Sheet`) no mobile; Configurações/Meu Perfil em rodapé (avatar + `DropdownMenu`) no rail, e dentro do mesmo `Sheet` de "Mais" no mobile.
- Rail (`≥768px`, `md`): ícone + `Tooltip` no hover; item da rota atual usa `nav-item-active` (`{colors.primary}/10` de fundo, `{colors.primary}` sólido no ícone/texto).
- Todo controle interativo do shell (ícones de nav, botões do `ConfirmDialog`) atinge `{spacing.touch-target-min}` (48px).
- `ConfirmDialog` usa `AlertDialog` do shadcn; `Toaster` global via `sonner` com `aria-live="polite"`.
- `docker-compose.yml` ganha serviço `web` (build `frontend/Dockerfile`, multi-stage → Nginx, AD-13), espelhando o padrão já usado pelo serviço `api` (Story 1.1).

**Never:**
- Autorização/gating de item por papel — não há autenticação ainda (Story 1.3/1.4/1.5); todo item de navegação renderiza incondicionalmente nesta story.
- Construir telas reais de produto (Catálogo, Carrinho, Pedidos, Estoques, ...) — só uma página placeholder mínima para hospedar o shell.
- `window.confirm()`/`alert()` nativos.
- Nginx do serviço `web` fazendo proxy de `/api` — não há chamada a API nesta story.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Desktop | viewport ≥768px, rota atual = placeholder | Rail (56px) + header fino visíveis; item da rota atual com `nav-item-active`; hover no ícone mostra `Tooltip` | N/A |
| Mobile | viewport <768px, a partir de 360px | Bottom nav (56px) visível, rail ausente; nenhuma quebra de layout/scroll horizontal em 360px | N/A |
| "Mais" (mobile) | tap no item "Mais" | `Sheet` abre de baixo com itens administrativos + Meu Perfil | N/A |
| `ConfirmDialog` confirma | `onConfirm` fornecido, usuário clica em confirmar | `AlertDialog` fecha, `onConfirm` é chamado uma vez | N/A |
| `ConfirmDialog` cancela | usuário clica em cancelar/fecha | `AlertDialog` fecha, `onConfirm` nunca é chamado | N/A |
| Toast | chamada a `toast.success`/`toast.error` | Renderiza via `sonner`, região com `aria-live="polite"` | N/A |

</intent-contract>

## Code Map

- `frontend/package.json`, `vite.config.ts`, `tsconfig*.json` -- scaffold Vite+React+TS (React 19.2.x, TS 7.0.x, Vite 8.0.x); inclui `vitest`+`@testing-library/react`+`jsdom` como devDependencies (mesmo padrão de teste automatizado usado no `backend`, Story 1.1).
- `frontend/components.json` -- config do CLI `shadcn` (estilo, alias `@/components`, `@/lib`).
- `frontend/src/index.css` -- `@import "tailwindcss"`; bloco `@theme` com todos os tokens de `DESIGN.md` (colors/typography/spacing/rounded), incluindo as 4 variantes `text-on-tint-*`.
- `frontend/src/components/ui/*` -- primitivos gerados pelo `shadcn` CLI: `button`, `tooltip`, `sheet`, `alert-dialog`, `avatar`, `dropdown-menu`, `sonner`.
- `frontend/src/components/shell/nav-items.ts` -- config de navegação (id, label, ícone, rota, `area: 'primary' | 'admin' | 'profile'`) refletindo a IA de `EXPERIENCE.md`.
- `frontend/src/components/shell/AppShell.tsx` -- layout raiz: rail+header (desktop, `hidden md:flex`) / bottom nav+"Mais" (mobile, `flex md:hidden`); aceita `tabs?`/`sideNav?` opcionais para o padrão "abas horizontais ou submenu vertical de 224px conforme o módulo" (nenhuma story ainda os usa — capacidade provada por teste, não por página real).
- `frontend/src/components/ConfirmDialog.tsx` -- wrapper reutilizável sobre `AlertDialog` (`open`, `onConfirm`, `onCancel`, `title`, `description`).
- `frontend/src/App.tsx` -- `react-router-dom` `createBrowserRouter`, rota raiz usa `AppShell` como layout, uma rota placeholder como `index`.
- `frontend/src/pages/PlaceholderPage.tsx` -- página mínima ("Em construção") só para hospedar o shell até a Story 3.x/4.x existir.
- `frontend/src/main.tsx` -- monta `<App />` + `<Toaster />` global.
- `frontend/Dockerfile`, `frontend/.dockerignore` -- build multi-stage `node:22-alpine` → `nginx:alpine`, espelhando `backend/Dockerfile` (Story 1.1, AD-13).
- `docker-compose.yml` (raiz) -- novo serviço `web` (build `./frontend`, porta exposta, `depends_on: api` opcional -- sem chamada real ainda).
- Referência lida (read-only): `_bmad-output/planning-artifacts/ux-designs/ux-stockflow-2026-08-29/DESIGN.md` (tokens) e `EXPERIENCE.md` (IA de navegação, breakpoints).

## Tasks & Acceptance

**Execution:**
- `frontend/package.json`+config -- scaffold Vite+React+TS com deps pinadas -- base para todo o resto.
- `frontend/src/index.css` -- tokens `@theme` -- satisfaz AC1.
- `frontend/src/components/ui/*` via `shadcn` CLI -- primitivos necessários (`tooltip`, `sheet`, `alert-dialog`, `sonner`, `avatar`, `dropdown-menu`, `button`) -- base para Rail/BottomNav/ConfirmDialog/Toaster.
- `frontend/src/components/shell/nav-items.ts` + `AppShell.tsx` -- rail desktop / bottom nav mobile / "Mais" sheet / `nav-item-active` / tooltip -- satisfaz AC2 e AC3.
- `frontend/src/components/ConfirmDialog.tsx` + `Toaster` em `main.tsx` -- satisfaz AC4.
- Alvo de toque 48px aplicado a todo item de nav e botão do `ConfirmDialog` -- satisfaz AC5.
- `frontend/src/**/*.test.tsx` (Vitest+RTL) -- cobre a I/O Matrix (breakpoint classes do Rail/BottomNav, abertura do "Mais", confirmar/cancelar do `ConfirmDialog`, toast) -- prova as ACs sem inspeção manual.
- `frontend/Dockerfile` + serviço `web` em `docker-compose.yml` -- fundação executável localmente (AD-13), espelhando Story 1.1.

**Acceptance Criteria:**
- Given o frontend inicializado, when os tokens do `DESIGN.md` são aplicados via `@theme`, then a paleta `primary`/`accent`/`destructive`/`warning`/`info` + `text-on-tint-*`, Inter, JetBrains Mono, e os tokens de espaçamento (rail 56px, bottom nav 56px, FAB 56px, submenu 224px, toque mínimo 48px) ficam disponíveis nas classes Tailwind geradas.
- Given viewport ≥768px, when o usuário acessa a página placeholder, then vê o rail (56px) + header fino, tooltip no hover de cada ícone, e o item ativo com `nav-item-active`.
- Given viewport <768px (a partir de 360px), when o usuário acessa a mesma página, then vê a bottom nav (56px) com Catálogo/Carrinho/Pedidos/Mais, itens administrativos e Meu Perfil dentro do `Sheet` de "Mais", sem quebra de layout.
- Given qualquer story futura precisando confirmar uma ação destrutiva, when ela importa `ConfirmDialog`/`Toaster` desta fundação, then ambos funcionam sem nenhuma configuração adicional (nenhum `window.confirm()` no código).
- Given `docker compose up --build`, when o serviço `web` sobe, then serve a página placeholder na porta configurada.

## Spec Change Log

## Review Triage Log

### 2026-08-29 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 5: (high 2, medium 0, low 3)
- defer: 7: (high 0, medium 2, low 5)
- reject: 7: (high 0, medium 0, low 7)
- addressed_findings:
  - `[high]` `[patch]` `ConfirmDialog.tsx`'s `AlertDialog.onOpenChange` handler called `onCancel` on any close, but `AlertDialogAction` is a Radix `DialogPrimitive.Close` under the hood and also triggers `onOpenChange(false)` on confirm — reproduced directly (confirm path called both `onConfirm` and `onCancel`). Fixed with a `confirmedRef` set in `AlertDialogAction`'s `onClick` before `onConfirm`, checked/reset in `onOpenChange` so `onCancel` only fires on a real cancel/dismiss. Added regression test `nunca chama onCancel quando o usuário confirma`.
  - `[high]` `[patch]` `AppShell.tsx`'s "Mais" `Sheet` was uncontrolled and `SheetNavRow` used a plain `NavLink`, so clicking any admin/profile item inside it navigated the route but left the sheet's overlay open on top of the new page (Radix only closes via Escape/overlay-click/`DialogClose`). Fixed by making `Sheet` controlled (`moreOpen`/`setMoreOpen` in `AppShell`) and closing it from `SheetNavRow`'s `onNavigate` callback. Added regression test `fecha o Sheet de "Mais" ao clicar num item de navegação administrativo`.
  - `[low]` `[patch]` `ConfirmDialog.tsx`'s `onOpenChange` prop was optional despite the dialog being fully `open`-controlled — a consumer that forgot to wire it would see `onCancel` fire without the dialog actually closing. Made `onOpenChange` required.
  - `[low]` `[patch]` `frontend/index.html` referenced `/vite.svg` as favicon with no `public/vite.svg` asset in the diff. Added the standard Vite placeholder asset.
  - `[low]` `[patch]` `docker-compose.yml`'s new `web` service used short-form `depends_on: - api` unlike `api`'s health-gated `depends_on: db: condition: service_healthy`. Changed to `depends_on: api: condition: service_healthy` (an `api` healthcheck already existed from Story 1.1); added an equivalent healthcheck to `web` itself.
  - `[reject]` 7 findings routed to reject: no `.env`/API-base-URL wiring and no `frontend/.env.example` (both explicitly out of scope — the spec's own `Never` clause states no real API call happens in this story); "Meu Perfil" routing to `/configuracoes` flagged as a label/route mismatch (false positive — matches `EXPERIENCE.md`'s own "Configurações → Meu Perfil" naming literally); a worry that the pinned `typescript`/`vite` versions might be typos (verified real published versions via `npm view` before writing the spec); unused shadcn-generated exports (`AvatarBadge`, `DropdownMenuRadioItem`, etc. — standard CLI boilerplate, no functional issue); potential z-index conflict between `Sheet` and `AlertDialog` if both were open simultaneously (no current flow combines them — out of scope per the spec's own "Never: build real screens" boundary); `nav-items.ts`'s `profileNavItem = navItems.find(...) as NavItem` flagged as possibly `undefined` (statically unreachable — the array is a hardcoded literal in the same file, not user-modifiable data).

### 2026-08-29 — Review pass (follow-up, `done` → fresh review)
- intent_gap: 0
- bad_spec: 0
- patch: 4: (high 0, medium 3, low 1)
- defer: 0
- reject: 13: (high 0, medium 0, low 13)
- addressed_findings:
  - `[medium]` `[patch]` `AppShell.tsx`'s `RailNavIcon` passed `<NavLink to={item.to}>` with no `end` prop. React Router's own internal active-match (which drives `aria-current="page"`, independent of the custom `className`) defaults to `end: false`, so with the Catálogo item's `to="/"`, every other rail route also matched as "current" for assistive tech even though the visible `nav-item-active` class (computed separately via `useMatch({ end: item.to === '/' })`) only lit up on `/` — reproduced and confirmed via source read. Fixed by adding `end={item.to === '/'}` to the `NavLink`, mirroring `BottomNavIcon`, which already had it. Added regression test `não marca um item inativo do rail com aria-current`.
  - `[medium]` `[patch]` `ConfirmDialog.tsx`'s `AlertDialogAction onClick` had no guard against a second click firing before the dialog's close state propagated, risking `onConfirm` being invoked more than once on a rapid double-click — directly conflicting with the AC's "onConfirm é chamado uma vez". Fixed with an early return when `confirmedRef.current` is already `true`. Added regression test `nunca chama onConfirm mais de uma vez em duplo clique rápido antes do fechamento` (two synchronous `fireEvent.click` calls before the dialog unmounts).
  - `[low]` `[patch]` Same `onClick` handler left `confirmedRef.current` stuck at `true` if `onConfirm` threw synchronously, which would silently swallow the next real cancel/dismiss's `onCancel` call. Fixed by resetting the ref and rethrowing inside a `try/catch`.
  - `[medium]` `[patch]` No automated test covered the desktop rail's "Meu Perfil" `DropdownMenu` (`AppShell.tsx`), despite it being the AC2-required entry point to Configurações/Meu Perfil on desktop — a regression there (broken route, lost accessible name, menu not opening) would have passed `npm run test` silently. Added regression test `abre o DropdownMenu de perfil no rail com o link Meu Perfil` asserting the trigger opens the menu and the item links to `/configuracoes`.
  - `[reject]` 13 findings routed to reject: a reviewer flagged `frontend/package-lock.json` as missing from the reviewed diff and therefore `npm ci`/`docker build` as broken — false positive, an artifact of this review pass deliberately excluding the 5188-line lockfile from the reviewer-facing diff for readability; `git diff --stat` confirms the lockfile is committed (counted once, covering both the "missing lockfile" and "pinned versions unguaranteed without a lockfile" restatements of the same claim); a reviewer flagged `.oxlintrc.json`'s `jsx-a11y` plugin as inert because `categories` only enables `correctness`/`suspicious` — false positive, empirically verified by running `oxlint` against a deliberately broken `<img>` missing `alt`, which correctly reported `jsx-a11y(alt-text)`; `ConfirmDialog` lacking a `variant="destructive"` prop for the confirm button (no AC or `Boundaries & Constraints` line asks for a destructive visual variant — out of scope for this foundation story); `ConfirmDialog`'s `onConfirm` being synchronous-only with no pending/loading state (this story's own `Never` clause rules out any real API call, so there is no async action to show pending state for yet); `AppShell.test.tsx` using its own `MemoryRouter`/`Routes` instead of importing `App.tsx`'s real router (a deliberate, standard RTL component-test isolation pattern, not a defect); the stock Vite placeholder `public/vite.svg` (branding is out of scope for this story); `index.html` missing `<meta name="description">`/`theme-color`/manifest (no AC requires PWA/SEO metadata); no code formatter configured for `frontend/` (tooling choice, not requested by any AC); `AppShell` having no "skip to content" link (a11y enhancement beyond what any AC's Given/When/Then requires); `sonner.test.tsx` only exercising the `success` toast icon mapping, not `error`/`warning`/`info`/`loading` (cosmetic icon-mapping coverage, no functional risk); `lib/utils.ts` and `nav-items.ts` having no dedicated unit tests (both are exercised indirectly by every component test that imports them); `AppShell`'s unused `sideNav` prop having no mobile fallback (the spec's own Design Notes already state `sideNav` "fica sem consumidor real nesta story" — nothing to regress); a restatement of the already-recorded `deferred` item on breakpoint/touch-target tests checking Tailwind class names instead of computed layout (duplicate of an existing frontmatter `deferred` entry from the first review pass — not re-added).

### 2026-08-29 — Review pass (segundo follow-up, `done` → fresh review)
- intent_gap: 0
- bad_spec: 0
- patch: 3: (high 0, medium 3, low 0)
- defer: 2: (high 0, medium 0, low 2)
- reject: 19: (high 0, medium 0, low 19)
- addressed_findings:
  - `[medium]` `[patch]` `AppShell.tsx`'s "Meu Perfil" `DropdownMenuItem` (dentro do `DropdownMenu` do rail) não carregava nenhuma classe de alvo de toque — só o botão-gatilho (`Avatar`) tinha `min-h/min-w-touch-target-min`; o próprio item de menu clicável ficava com o padding padrão do shadcn (`py-1.5`), bem abaixo dos 48px exigidos pelo `Always` da spec ("Todo controle interativo do shell... atinge {spacing.touch-target-min}"). Corrigido adicionando `className={touchTarget}` ao `DropdownMenuItem`. Adicionado teste de regressão `aplica o alvo de toque mínimo (48px) ao item "Meu Perfil" dentro do DropdownMenu do rail`.
  - `[medium]` `[patch]` `ConfirmDialog.tsx`'s `AlertDialogCancel` também é um `DialogPrimitive.Close` por baixo (mesma classe do `AlertDialogAction`), então um duplo clique rápido em "Cancelar" antes do diálogo fechar podia disparar `onOpenChange(false)` mais de uma vez para o mesmo fechamento e chamar `onCancel` mais de uma vez — a guarda de duplo clique existente só cobria o caminho de confirmação. Corrigido generalizando a guarda: um novo `closeHandledRef` processa só a primeira transição para fechado por sessão aberta (rearmado quando o diálogo reabre), cobrindo confirmar e cancelar simetricamente. Adicionado teste de regressão `nunca chama onCancel mais de uma vez em duplo clique rápido em Cancelar antes do fechamento`.
  - `[medium]` `[patch]` Nenhum teste cobria o `catch` que reseta `confirmedRef` quando `onConfirm` lança de forma síncrona (lógica adicionada na passagem de revisão anterior) — um regression ali (ex.: alguém removendo o `try/catch` numa limpeza futura) passaria `npm run test` silenciosamente, deixando um cancelamento real subsequente sem chamar `onCancel`. Adicionado teste de regressão `reseta o estado de confirmação quando onConfirm lança, permitindo onCancel num cancelamento real subsequente` (captura o erro relançado via um listener temporário de `window 'error'`, já que o React re-lança de forma assíncrona em vez de propagar de volta pela chamada síncrona a `fireEvent.click`).
  - `[reject]` 19 achados roteados para reject, entre eles: 6 restatements de itens já registrados em `deferred` (dark mode instalado mas inerte; testes de breakpoint/alvo-de-toque via nome de classe em vez de layout computado; `legacy-peer-deps` mascarando conflitos de peer; ausência de headers de cache/segurança no Nginx; `tabs`/`sideNav` sem consumidor real; acessibilidade além do que as ACs exigem) — nenhum re-adicionado, mesmo padrão da passagem anterior; `SheetNavRow` não passar `end={item.to === '/'}` ao `NavLink` (mesmo padrão do `RailNavIcon`/`BottomNavIcon`) — falso positivo após inspeção: nenhum item `admin`/`profile` (os únicos que aparecem dentro do Sheet) tem `to === '/'`, então `end` sempre resolveria para `false` de qualquer forma — zero efeito comportamental, atual ou futuro, dado que a área `primary` (a única com `to: '/'`) nunca é renderizada dentro do Sheet; `onConfirm` podendo ser assíncrono sem `await`/tratamento de rejeição — fora de escopo, a assinatura do tipo é `() => void` e o `Never` da própria spec já exclui qualquer chamada real a API nesta story (mesmo tema já rejeitado na primeira passagem: "onConfirm sendo síncrono-only sem estado pending"); `nav-items.ts`'s `profileNavItem = navItems.find(...) as NavItem` possivelmente `undefined` — duplicata exata de achado já rejeitado na primeira passagem (estaticamente inalcançável, array literal fixo no mesmo arquivo); `.gitignore`/`.dockerignore`'s padrão `*.env` não cobrir `.env.local`/`.env.*.local` do Vite — sem efeito atual, nenhum arquivo `.env` existe ou é consumido nesta story (mesmo tema já rejeitado: sem wiring de API/`.env.example`, fora de escopo desta story); ausência de `ErrorBoundary`/`errorElement` em `App.tsx`/`main.tsx` — nenhuma AC ou `Boundaries & Constraints` exige tratamento de erro de render, fora do escopo desta fundação (mesma categoria de itens genéricos de hardening já rejeitados, ex. "skip to content", formatter); ausência de teste para dismissal via Esc/clique-fora do `Sheet`/`AlertDialog` — nenhuma AC exige cobertura desses caminhos especificamente, sem defeito demonstrado (comportamento padrão do Radix); ausência de checagem automatizada comparando `@theme` a `DESIGN.md` — pedido de tooling nova não requerida por nenhuma AC; `.oxlintrc.json`'s `categories` habilitando só `correctness`/`suspicious` (deixando a maior parte das regras de `unicorn`/`react-hooks`/resto de `jsx-a11y` desligadas) — escolha de configuração já examinada e aceita na passagem anterior, nenhuma AC exige categorias específicas; variantes `icon`/`icon-sm`/`icon-xs` do `Button` gerado pelo shadcn ficarem abaixo de 48px — sem violação atual (`AppShell`/`ConfirmDialog` não usam essas variantes, aplicam `touchTarget` manualmente), especulação sobre uso futuro; ausência de teste clicando "Meu Perfil" dentro do Sheet mobile (só "Estoques" é testado no `Sheet`) — mesmo `onNavigate`/padrão já coberto por `SheetNavRow`, gap de cobertura cosmético sem defeito demonstrado; ausência de teste para as classes `pb-bottom-nav-height md:pb-0` do `<main>` — mesma limitação de `jsdom` sem motor de CSS já registrada em `deferred`; `frontend/Dockerfile` supostamente empacotando arquivos de teste no bundle final — falso positivo, `tsconfig.app.json` usa `noEmit: true` (só type-check) e `vite build`/Rollup só inclui módulos alcançáveis a partir de `main.tsx`, nenhum arquivo de teste é importado pelo entrypoint de produção; `docker compose up --build` não executado nesta passagem — já autorrelatado no `## Auto Run Result` da passagem anterior, não é um achado novo.

## Design Notes

Tailwind v4 é CSS-first: não existe `tailwind.config.js` -- todo o mapeamento de token vive em `@theme` dentro de `frontend/src/index.css`, ex.:

```css
@import "tailwindcss";
@theme {
  --color-primary: #E62019;
  --color-primary-foreground: #FFFFFF;
  --spacing-rail-width: 56px;
  --spacing-touch-target-min: 48px;
  --radius-md: 10px;
  --font-sans: Inter, system-ui, sans-serif;
  --font-mono: "JetBrains Mono", monospace;
}
```

Item "Configurações/Meu Perfil" some do rodapé do rail (avatar) para dentro do `Sheet` de "Mais" no mobile -- decisão de layout desta story (não há espaço para um 5º ícone na bottom nav de 56px); não é uma ambiguidade de intenção, é um detalhe de implementação dentro do texto literal da AC ("itens administrativos atrás de Mais").

`tabs`/`sideNav` do `AppShell` ficam sem consumidor real nesta story (nenhum módulo com abas/submenu existe ainda) -- provados só por teste de componente, não por página de produto real.

## Verification

**Commands:**
- `cd frontend && npm install` -- expected: instala sem erro, versões pinadas conforme Architecture Stack.
- `cd frontend && npm run build` -- expected: `tsc` + `vite build` limpos, sem erro de tipo.
- `cd frontend && npm run lint` -- expected: sem warnings.
- `cd frontend && npm run test` -- expected: todos os testes de componente passam (Rail/BottomNav/ConfirmDialog/Toaster).
- `docker compose up --build web` -- expected: `web` sobe e serve a página placeholder.

**Manual checks (if no CLI):**
- Abrir `npm run dev` no navegador, redimensionar para 360px e para ≥768px, confirmar visualmente que não há scroll horizontal nem quebra de layout, e que o hover no rail mostra o `Tooltip`.

## Auto Run Result

**Resumo da mudança implementada:** `frontend/` inicializado do zero (Vite 8.0.16 + React 19.2.8 + TypeScript 7.0.2, `react-router-dom` 6.30.6 — nunca 7.x), Tailwind CSS v4 CSS-first com todos os tokens de `DESIGN.md` traduzidos literalmente em `@theme`, primitivos `shadcn/ui` (`button`, `tooltip`, `sheet`, `alert-dialog`, `avatar`, `dropdown-menu`, `sonner`), `AppShell` responsivo (rail+header desktop, bottom nav+"Mais" mobile, `nav-item-active`, `Tooltip` no rail), `ConfirmDialog` reutilizável sobre `AlertDialog`, `Toaster` global via `sonner`, e o serviço `web` (Nginx) adicionado ao `docker-compose.yml` espelhando o padrão da Story 1.1. Esta é uma passagem de revisão de acompanhamento (a spec estava `done` com `followup_review_recommended: true`); nenhuma funcionalidade nova foi adicionada, apenas correções encontradas pela revisão.

**Arquivos alterados nesta passagem:**
- `frontend/src/components/shell/AppShell.tsx` -- `RailNavIcon` ganhou `end={item.to === '/'}` no `NavLink` (corrige `aria-current="page"` incorreto em rotas não-raiz).
- `frontend/src/components/ConfirmDialog.tsx` -- guarda contra duplo clique disparando `onConfirm` mais de uma vez; `confirmedRef` é revertido se `onConfirm` lançar.
- `frontend/src/components/shell/AppShell.test.tsx` -- +2 testes: `aria-current` correto no rail, `DropdownMenu` de "Meu Perfil" abre e linka para `/configuracoes`.
- `frontend/src/components/ConfirmDialog.test.tsx` -- +1 teste: duplo clique rápido em "Confirmar" não chama `onConfirm` mais de uma vez.
- `_bmad-output/implementation-artifacts/spec-1-2-fundacao-do-shell-de-navegacao-e-design-tokens.md` -- este arquivo (`status`, `## Review Triage Log`, `## Auto Run Result`).

**Resultado da revisão (esta passagem):** intent_gap 0, bad_spec 0, patch 4 (medium 3, low 1) aplicados, defer 0, reject 13. Detalhe completo em `## Review Triage Log` — passagem de 2026-08-29 (follow-up).

**Recomendação de revisão de acompanhamento:** `true` — patches desta passagem: medium 3, low 1; score `3×3 + 1×1 = 10` (≥ 5).

**Verificação executada:**
- `cd frontend && npm run build` (`tsc -b && vite build`) -- OK, build limpo.
- `cd frontend && npm run lint` (`oxlint .`) -- OK, exit 0, sem warnings.
- `cd frontend && npm run test` (Vitest+RTL) -- OK, 19/19 testes passam (16 da passagem anterior + 3 novos desta passagem).
- `docker compose up --build web` e checagem manual de navegador (360px/≥768px) -- não executados neste sandbox, mesma limitação já registrada na passagem anterior (sem `docker` e sem automação de navegador disponíveis).

**Riscos residuais:** os 7 itens já registrados em `deferred` permanecem inalterados (dark mode instalado mas não usado; Nginx sem hardening de produção; testes de breakpoint/toque via nome de classe, não layout real; `legacy-peer-deps` mascarando conflitos de peer; ausência de CI; ausência de README; ausência de checagem automatizada de acessibilidade); nenhum item novo foi adicionado a `deferred` nesta passagem.

**Nota de finalização:** o commit `ec92a8a` cobre todos os arquivos revisados nesta passagem (4 arquivos de `frontend/` + este spec); `deferred-work.md` e `sprint-status.yaml` seguem modificados na working copy, mas são propriedade do orquestrador (fora do escopo desta invocação) e não foram tocados nem revertidos por este agente.

---

## Auto Run Result (segundo follow-up)

**Resumo da mudança implementada:** esta é uma segunda passagem de revisão de acompanhamento (a spec estava `done` com `followup_review_recommended: true`); nenhuma funcionalidade nova foi adicionada. Três correções pontuais encontradas pela revisão foram aplicadas: alvo de toque de 48px no item "Meu Perfil" do `DropdownMenu` do rail, guarda simétrica contra duplo clique em "Cancelar" no `ConfirmDialog` (mesma classe de bug já corrigida para "Confirmar"), e um teste de regressão cobrindo o reset de `confirmedRef` quando `onConfirm` lança.

**Arquivos alterados nesta passagem:**
- `frontend/src/components/shell/AppShell.tsx` -- `DropdownMenuItem` do "Meu Perfil" (rail) ganhou `className={touchTarget}` (corrige alvo de toque abaixo de 48px).
- `frontend/src/components/shell/AppShell.test.tsx` -- +1 teste: alvo de toque de 48px no item "Meu Perfil" do `DropdownMenu`.
- `frontend/src/components/ConfirmDialog.tsx` -- novo `closeHandledRef` generaliza a guarda de fechamento duplicado para cobrir "Cancelar" (antes só cobria "Confirmar" via `confirmedRef`).
- `frontend/src/components/ConfirmDialog.test.tsx` -- +2 testes: duplo clique rápido em "Cancelar" não chama `onCancel` mais de uma vez; `onConfirm` lançando reseta o estado e permite `onCancel` num cancelamento real subsequente.
- `_bmad-output/implementation-artifacts/spec-1-2-fundacao-do-shell-de-navegacao-e-design-tokens.md` -- este arquivo (`status`, `deferred`, `## Review Triage Log`, `## Auto Run Result`).

**Resultado da revisão (esta passagem):** intent_gap 0, bad_spec 0, patch 3 (medium 3) aplicados, defer 2 (low 2), reject 19. Detalhe completo em `## Review Triage Log` — passagem de 2026-08-29 (segundo follow-up).

**Recomendação de revisão de acompanhamento:** `true` — patches desta passagem: medium 3, low 0; score `3×3 + 1×0 = 9` (≥ 5).

**Verificação executada:**
- `cd frontend && npm run build` (`tsc -b && vite build`) -- OK, build limpo (`dist/` gerado, sem erro de tipo).
- `cd frontend && npm run lint` (`oxlint .`) -- OK, sem warnings.
- `cd frontend && npm run test` (Vitest+RTL) -- OK, 22/22 testes passam (19 das passagens anteriores + 3 novos desta passagem).
- `docker compose up --build web` e checagem manual de navegador (360px/≥768px) -- não executados neste sandbox, mesma limitação já registrada nas passagens anteriores (sem `docker` e sem automação de navegador disponíveis).

**Riscos residuais:** os 9 itens em `deferred` permanecem (7 anteriores inalterados + 2 novos desta passagem: `nginx.conf` sem `404` para asset estático obsoleto/ausente — mesmo tema de hardening de produção já registrado; `sonner.tsx` não conecta as variáveis CSS por-tipo do `sonner` aos tokens de tint de `DESIGN.md`, então toasts de tipos diferentes só variam pelo ícone). Nenhum item novo além desses dois foi adicionado a `deferred` nesta passagem.

