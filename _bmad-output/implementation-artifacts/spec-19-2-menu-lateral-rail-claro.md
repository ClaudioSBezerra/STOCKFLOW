---
title: 'Menu lateral: converter para rail claro do Catálogo 2.0'
type: 'feature'
created: '2026-11-12'
status: 'done'
review_loop_iteration: 0
baseline_commit: '5bc5a321f6fd3782bb53da3c00462aa0b4f27976'
context: ['{project-root}/_bmad-output/implementation-artifacts/epic-19-context.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O menu lateral do stockflow (Epic 17) é azul-marinho escuro, mas o Catálogo 2.0 não tem token de "sidebar escura" — o FB_APU05 usa um rail CLARO. Claudio decidiu (2026-11-12) converter para esse padrão.

**Approach:** Trocar os 5 tokens `--color-sidebar*` em `index.css` para valores claros, e separar em `MenuLateral.tsx` o estado de HOVER (neutro) do estado ATIVO (chip verde) — hoje os dois reaproveitam a mesma classe. Ajustes de borda em `AppShell.tsx` ficam para depois (ver `deferred-work.md`).

## Boundaries & Constraints

**Always:**
- Preservar 100% da lógica de grupos/rotas/permissões/aria do Epic 17 — só classes CSS mudam.
- Texto do item REALMENTE ativo deve ter contraste AA (4.5:1) contra seu fundo — diferente da Story 19.1, aqui não há precedente aceito de abrir mão de contraste.
- Não tocar `AppShell.tsx` nesta story (deferred).

**Ask First:** nenhuma — decisões já fechadas (Design Notes).

**Never:** mudar `AppRail.tsx` do FB_APU05 (só referência). Nunca `tailwind.config.js`. Não tocar páginas de lista/formulários (19.3/19.4).

</frozen-after-approval>

## Code Map

- `frontend/src/index.css` -- 5 tokens `--color-sidebar*` (bloco `@theme`, ~linhas 29-33).
- `frontend/src/components/shell/MenuLateral.tsx` -- usa só classes semânticas. 4 ocorrências de `hover:bg-sidebar-item-active` (linhas 36, 108, 159, 231; as de 36/231 também têm `hover:text-sidebar-foreground-active`) e 1 de `border-sidebar-item-active` (linha 269, divisor "Meu Perfil") precisam virar tokens neutros. `ativo && 'sidebar-item-active'` / `temAtivo && 'sidebar-item-active'` (linhas 38, 161) ficam intocadas — é o estado ativo de verdade.
- Fonte dos valores: `/home/claudio/projetos/FB_APU05/frontend/src/styles/catalogo-tokens.css` e `frontend/src/components/AppRail.tsx` — já lidos, valores confirmados abaixo.
- `frontend/src/components/shell/AppShell.test.tsx:77-78` -- regex `ATIVO` já distingue a classe ativa do hover por desenho; não deve quebrar com esta mudança.

## Tasks & Acceptance

**Execution:**
- [x] `frontend/src/index.css` -- trocar os 5 tokens `--color-sidebar*` (ver tabela em Design Notes) -- base da conversão
- [x] `frontend/src/components/shell/MenuLateral.tsx` -- trocar as 4 ocorrências de `hover:bg-sidebar-item-active` (e as 2 de `hover:text-sidebar-foreground-active` junto) por `hover:bg-border`/`hover:text-foreground` -- separa hover (neutro) de ativo (chip verde)
- [x] `frontend/src/components/shell/MenuLateral.tsx` -- trocar `border-sidebar-item-active` (linha 269) por `border-border` -- divisor visível contra fundo branco

**Acceptance Criteria:**
- Given o menu sem nenhum item em hover, then o fundo é `#ffffff` e o texto dos itens não-ativos é `#4f5256`
- Given o item ativo (rota atual), then o fundo é `#ebf6e3`, o texto é `#252728` (não o verde do FB_APU05 — ver nota) e a barra de 3px continua verde (`var(--color-primary)`, já correto desde a 19.1)
- Given um item NÃO ativo em hover, then o fundo vira `#e5e6e6` e o texto `#252728` — nunca o chip verde do ativo
- Given `npm run build` e `npm test`, then terminam sem erro e a suíte passa

## Design Notes

**Tokens (stockflow → valor → origem):**

| Variável | Valor | Origem |
|---|---|---|
| `--color-sidebar` | `#ffffff` | `bg-card` (FB_APU05) |
| `--color-sidebar-foreground` | `#4f5256` | `--fg-secondary` (item inativo no FB_APU05) |
| `--color-sidebar-item-active` | `#ebf6e3` | `--green-l850` ("soft default · plain hover bg") |
| `--color-sidebar-foreground-active` | `#252728` | `--fg-1` — **desvio do FB_APU05**, ver nota |
| `--color-sidebar-group-title` | `#252728` | `--fg-1` (nome do workspace no FB_APU05) |

**Por que não copiar o texto verde do FB_APU05:** eles usam `--green-250` (`#5a8f19`) como texto do item ativo sobre `--green-l850`. Contraste calculado (WCAG): ~3,5:1 — não passa AA. Sem precedente aceito aqui (diferente da 19.1), uso `#252728` (quase-preto) no lugar — contraste ~15:1, estado ativo ainda claro via fundo verde + barra lateral.

**Hover vs. ativo:** hoje a mesma classe cobre as duas coisas (fazia sentido no tema escuro). Com fundo virando chip verde, todo hover pareceria "selecionado". Uso `--border`/`--foreground` (cinza neutro, já portados na 19.1) para hover-não-ativo, igual ao `hover:bg-border hover:text-foreground` do FB_APU05.

## Verification

**Commands:**
- `npm run build` -- expected: build limpo
- `npm test` -- expected: suíte inteira passando

**Manual checks (if no CLI):**
- Abrir o app localmente: menu claro, item ativo com chip verde + texto escuro + barra verde, hover em item não-ativo com cinza neutro.

## Suggested Review Order

**Tokens de cor**

- Ponto de entrada: os 5 tokens `--color-sidebar*` com os novos valores claros
  [`index.css:42`](../../frontend/src/index.css#L42)

**Hover vs. ativo (correção da revisão)**

- Guard `!ativo` evita que o hover "apague" o chip verde do item já ativo (camada `utilities` do Tailwind sempre vence `.sidebar-item-active`)
  [`MenuLateral.tsx:41`](../../frontend/src/components/shell/MenuLateral.tsx#L41)

- Mesmo guard no botão do grupo recolhido (`!temAtivo`)
  [`MenuLateral.tsx:165`](../../frontend/src/components/shell/MenuLateral.tsx#L165)

- Divisor do "Meu Perfil" trocado para um cinza visível (`border-border`)
  [`MenuLateral.tsx:269`](../../frontend/src/components/shell/MenuLateral.tsx#L269)

**Periférico**

- Scrollbar do menu recalibrada (o tom antigo ficava invisível no fundo branco)
  [`index.css:135`](../../frontend/src/index.css#L135)

- Comentários desatualizados corrigidos (texto branco/fundo azul-marinho que não existem mais)
  [`index.css:118`](../../frontend/src/index.css#L118)
