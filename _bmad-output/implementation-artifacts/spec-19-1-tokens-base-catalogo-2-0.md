---
title: 'Tokens de cor e fonte do Catálogo 2.0 no Tailwind v4'
type: 'feature'
created: '2026-11-12'
status: 'done'
review_loop_iteration: 0
baseline_commit: '6f41e698ec35b5e7307e907effed842c779fc11b'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-19-context.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O stockflow usa cores próprias (DESIGN.md de 29/08), diferentes do "Catálogo 2.0 Design System" padronizado pela Ferreira Costa entre seus sistemas internos (já adotado no FB_APU05).

**Approach:** Portar as cores do Catálogo 2.0 para a sintaxe `@theme` do Tailwind v4 em `index.css`, mantendo os MESMOS nomes de variável shadcn já usados (só o valor muda), e servir a fonte Inter localmente. Tipografia/raios/sombras/badges/score ficam para uma story posterior (ver `deferred-work.md`). Nenhum componente é tocado nesta story.

## Boundaries & Constraints

**Always:**
- Manter os nomes de variável shadcn existentes (`--color-primary`, `--color-destructive`, `--color-secondary`, `--color-border`, `--color-muted`, `--color-popover`, `--color-card`, `--color-ring`, `--color-input` etc.) — só o valor muda.
- `--color-sidebar*` (sidebar, sidebar-foreground, sidebar-foreground-active, sidebar-item-active, sidebar-group-title) ficam INALTERADOS — o Catálogo 2.0 não define um token de sidebar escura equivalente; decisão da Story 19.2.
- Não tocar `--font-mono`, `--text-*`, `--radius-*`, `--spacing-*` nesta story (deferred).
- Fonte Inter servida via `@fontsource-variable/inter` (pacote npm `^5.3.0`), importado em `main.tsx`, nunca via Google Fonts/CDN externo.
- `npm run build` e `npm test` devem continuar limpos ao final.

**Ask First:** nenhuma — mapeamento já decidido abaixo (Design Notes).

**Never:** tocar em qualquer componente `.tsx`/página nesta story. Nunca adicionar `tailwind.config.js` (projeto é Tailwind v4 CSS-first). Não mexer em tipografia/raios/sombras/badges/score (deferred).

</frozen-after-approval>

## Code Map

- `frontend/src/index.css` -- bloco `@theme`, só as linhas de `--color-*` (linhas ~15-47 do arquivo atual); o resto (`--font-*`, `--text-*`, `--radius-*`, `--spacing-*`, `@layer base/components`) fica intacto.
- `frontend/src/main.tsx` -- importa `./index.css` na linha 5; adicionar `import '@fontsource-variable/inter';` logo antes.
- `frontend/package.json` -- adicionar `"@fontsource-variable/inter": "^5.3.0"` em `dependencies`.
- `_bmad-output/planning-artifacts/ux-designs/ux-stockflow-2026-08-29/DESIGN.md` -- addendum novo (seção ao final, ou arquivo `DESIGN-addendum-catalogo-2-0.md` na mesma pasta) com a tabela de mapeamento.
- Fonte dos valores reais: `/home/claudio/projetos/FB_APU05/frontend/src/styles/catalogo-tokens.css` (cópia local, idêntica à do servidor hostinger-fc) — ler para confirmar valores ao implementar.

## Tasks & Acceptance

**Execution:**
- [x] `frontend/package.json` -- adicionar `@fontsource-variable/inter@^5.3.0` -- fonte local, sem CDN externo
- [x] `frontend/src/main.tsx` -- `import '@fontsource-variable/inter';` antes de `import './index.css';` -- garante fonte carregada antes do CSS
- [x] `frontend/src/index.css` -- substituir só as variáveis `--color-*` pelos valores do Catálogo 2.0 (ver tabela em Design Notes) -- troca de cores mantendo os mesmos nomes
- [x] `_bmad-output/planning-artifacts/ux-designs/ux-stockflow-2026-08-29/DESIGN.md` -- acrescentar "Addendum — Catálogo 2.0 (2026-11-12)" com origem e tabela de mapeamento -- governança exige documentar antes do código

**Acceptance Criteria:**
- Given o `index.css` modificado, when `npm run build` roda, then termina sem erro
- Given a aplicação carregada, when a aba Network é inspecionada, then nenhuma requisição vai a `fonts.googleapis.com` ou CDN de fonte externo
- Given o `DESIGN.md`, when consultado, then contém a tabela de mapeamento completa de cores
- Given `--color-sidebar*`, `--font-mono`, `--text-*`, `--radius-*`, `--spacing-*`, when o diff do `index.css` é revisado, then nenhum desses mudou

## Design Notes

**Mapeamento exato (nome stockflow → novo valor → origem Catálogo 2.0):**

| Variável | Novo valor | Origem |
|---|---|---|
| `--color-primary` | `#68a439` | `--green-150` (CTA principal) |
| `--color-primary-foreground` | `#ffffff` | — |
| `--color-accent` | `#3d8bd4` | `--blue-d150` (acento secundário) |
| `--color-accent-foreground` | `#ffffff` | — |
| `--color-destructive` | `#e71c35` | `--brand-red` |
| `--color-destructive-foreground` | `#ffffff` | — |
| `--color-background` | `#f4f5f6` | `--bg-canvas` |
| `--color-foreground` | `#252728` | `--fg-1` |
| `--color-card` | `#ffffff` | `--bg-card` |
| `--color-secondary` | `#e0f0ff` | `--blue-l850` (CTA secundário soft) |
| `--color-secondary-foreground` | `#367bbb` | `--blue-d250` |
| `--color-border` | `#e5e6e6` | `--br-soft` |
| `--color-success` | `#17a74a` | `--suc-br` |
| `--color-warning` | `#f2bd00` | `--warn-br` |
| `--color-info` | `#48a4f9` | `--info-br` |
| `--color-text-on-tint-success` | `#0d5c28` | `--suc-tx` |
| `--color-text-on-tint-warning` | `#594600` | `--warn-tx` |
| `--color-text-on-tint-info` | `#1a4a6b` | `--info-tx` |
| `--color-text-on-tint-destructive` | `#960c23` | `--err-tx` |
| `--color-muted` | `#f6f7f7` | `--bg-soft` |
| `--color-muted-foreground` | `#5c5e62` | `--fg-helper` (valor direto, não mais `color-mix`) |
| `--color-input` | `#d4d5d6` | `--br-strong` |
| `--color-ring` | `#48a4f9` | `--blue` (foco) |
| `--color-popover` / `-foreground` | inalterados (= card/foreground) | — |

`--font-sans: 'Inter Variable', 'Inter', system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif` (única mudança de tipografia permitida nesta story, por ser parte da troca de fonte).

## Verification

**Commands:**
- `npm run build` -- expected: build limpo, sem erro
- `npm test` -- expected: suíte inteira passando
- `npm run lint` (oxlint) -- expected: sem novo erro

**Manual checks (if no CLI):**
- Abrir a tela de login localmente e confirmar via DevTools (aba Fontes/Network) que `Inter Variable` carrega de arquivo local, nunca de `fonts.googleapis.com`.

## Suggested Review Order

**Tokens de cor**

- Ponto de entrada: bloco `@theme` inteiro com as cores novas do Catálogo 2.0
  [`index.css:22`](../../frontend/src/index.css#L22)

- Variáveis derivadas (`muted`/`input`/`ring`) passam a usar hex direto em vez de `color-mix`/reaproveitamento
  [`index.css:51`](../../frontend/src/index.css#L51)

**Fonte local**

- Import do `@fontsource-variable/inter` (normal + itálico) antes do `index.css`
  [`main.tsx:5`](../../frontend/src/main.tsx#L5)

- Dependência nova em `package.json`
  [`package.json:14`](../../frontend/package.json#L14)

**Governança**

- Addendum documenta origem e tabela de mapeamento completa, exigido antes do código
  [`DESIGN.md:230`](../../_bmad-output/planning-artifacts/ux-designs/ux-stockflow-2026-08-29/DESIGN.md#L230)

**Periférico**

- Comentário desatualizado corrigido (a premissa "`accent` == `success`" não vale mais com as cores novas)
  [`StatusPedidoBadge.tsx:43`](../../frontend/src/components/pedidos/StatusPedidoBadge.tsx#L43)
