---
title: 'Menu lateral agrupado e topo novo'
type: 'feature'
created: '2026-09-25'
status: 'done'
baseline_revision: '4e306ca08566568fa3df11bbcf6e3aa86b37dfcc'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-17-context.md'
  - '{project-root}/_bmad-output/planning-artifacts/ux-designs/ux-stockflow-2026-08-29/EXPERIENCE.md'
  - '{project-root}/_bmad-output/planning-artifacts/ux-designs/ux-stockflow-2026-08-29/DESIGN.md'
warnings: [oversized]
deferred: []
---

<intent-contract>

## Intent

**Problem:** A navegação é um rail de ícones sem nome, abas internas por módulo e bottom nav no celular; a pessoa precisa adivinhar o que cada ícone e aba significa.

**Approach:** Só frontend. Trocar o `AppShell` por menu lateral azul-marinho com grupos escritos (gaveta por ☰ no celular) e topo branco de 56px; cada aba interna vira rota própria. Nenhuma regra de negócio nem rota de API muda.

## Boundaries & Constraints

**Always:** Item sem papel mínimo some (nunca desabilitado) e grupo sem item visível some inteiro, via `filtrarNavPorPapel`; item ativo com `aria-current="page"`, negrito branco, fundo `sidebar-item-active` e barra de 3px vermelho FC à esquerda; `nav` com `aria-label="Menu principal"`, grupo é botão com `aria-expanded`; o grupo do item ativo abre sozinho; `localStorage` (menu recolhido e estado dos grupos) só como conveniência, sempre em try/catch, padrão aberto sem ele; gate de MFA (`RotaProtegida`) intacto: menu visível, toda rota leva a `/configuracoes`; `cart-badge` no item Carrinho; `fab-scanner` continua só no Catálogo/Carrinho, no canto inferior direito com `fab-margin` (sem bottom nav, sem o deslocamento de 72px); links antigos (`/normalizacao?verificarDuplicatas=1`) continuam funcionando; cada página nova tem `h1` com o nome da tela; marca do ambiente no topo do menu: "Suprimentos" se `window.location.hostname` começa com `suprimentos.`, senão "stockflow" (função pura em `lib/marca.ts`); textos em PT BR.

**Block If:** —

**Never:** Alterar backend, rotas de API ou regras de negócio; mexer nos componentes de seção (`*Section`) além do link do CTA de importação; criar as páginas de Cadastros/Administração ou mexer em `/configuracoes` (Story 17.2); padrão de lista/tabela/KPIs (Stories 17.3–17.5); cor nova fora dos tokens do DESIGN.md.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected | Error |
|---|---|---|---|
| Papel `usuario` | menu | só Catálogo→Produtos/Carrinho e Pedidos→Meus pedidos; grupos Estoque e Qualidade dos dados somem | — |
| Link direto | `/estoques/movimentacoes` | abre Movimentações, item ativo, grupo Estoque aberto | — |
| Rota gated sem papel | `/pedidos/fila` como `usuario` | "Você não tem acesso…" (servidor continua autoridade) | — |
| CTA da importação | `/normalizacao?verificarDuplicatas=1` | redireciona para `/normalizacao/duplicatas?verificarDuplicatas=1` e analisa 1 vez | — |
| Recolher | botão no topo do menu | 64px, ícones de grupo com tooltip, clique abre painel flutuante com os itens; lembrado | `localStorage` lança: abre expandido |
| Celular (<768px) | ☰ | gaveta esquerda; fecha ao escolher item, `Esc` ou toque fora; foco vai à gaveta e volta ao ☰ | — |
| Gate de MFA | sessão pendente | menu visível; qualquer rota → `/configuracoes` | — |

</intent-contract>

## Code Map

- `frontend/src/components/shell/AppShell.tsx` -- hoje rail 56px + header + bottom nav + Sheet "Mais" + `FaixaTreinamento`; reescrever para sidebar/topo/gaveta (manter `FaixaTreinamento` e a moldura de Treinamento, o `CartBadge` e `useCarrinho`); remover props `tabs`/`sideNav` (sem consumidor).
- `frontend/src/components/shell/nav-items.ts` -- manter `Papel`, `rankPapel`, `filtrarNavPorPapel` (usados por várias páginas); trocar `navItems` por grupos `{id,label,icon,itens:[{id,label,to,papelMinimo}]}` e `perfil`. Grupos desta story: Catálogo (Produtos `/`, Cadastrar produto `/produtos/novo`, Importar planilha `/produtos/importar`, Carrinho `/carrinho`), Pedidos (Meus pedidos `/pedidos`, Fila de aprovação `/pedidos/fila`), Estoque (Locais `/estoques`, Lançar saldo, Movimentações), Qualidade dos dados (Inconsistências `/normalizacao`, Duplicatas). Papéis conforme a tabela "Menu agrupado" do EXPERIENCE.md. Remover o item `/relatorios` (não existe na tabela).
- `frontend/src/components/shell/MenuLateral.tsx` (novo) -- conteúdo do menu (grupos + Meu perfil no rodapé), reusado pela sidebar fixa e pela gaveta; `useMatch`/`NavLink` com `end` para `/`; estado lembrado via helper com try/catch.
- `frontend/src/lib/marca.ts` (novo) -- `nomeDaMarca(hostname)`.
- `frontend/src/index.css` -- tokens `sidebar*`, `sidebar-width` 240, `sidebar-width-collapsed` 64, `topbar-height` 56 (DESIGN.md linhas 23-27, 72-74); remover `rail-width`, `bottom-nav-height`, `sidenav-width`, `fab-offset-mobile` e `.nav-item-active` se ficarem sem uso.
- `frontend/src/components/catalogo/ScannerProdutoFab.tsx:213` -- `bottom-fab-offset-mobile md:bottom-fab-margin` vira `bottom-fab-margin`.
- `frontend/src/components/ui/sheet.tsx` -- `SheetContent side="left"` para a gaveta (Radix já devolve o foco ao gatilho e fecha com `Esc`/overlay).
- `frontend/src/App.tsx` -- registrar as rotas novas dentro de `RotaProtegida`; `RotaProtegida` inalterada.
- `frontend/src/pages/PedidosPage.tsx`, `EstoquesPage.tsx`, `NormalizacaoPage.tsx`, `CatalogoPage.tsx` -- tirar as `Tabs`; cada aba vira página/rota (`PedidosPage` = Meus; `PedidosFilaPage`, `EstoquesPage` = Locais; `LancarSaldoPage`, `MovimentacoesPage`, `NormalizacaoPage` = Inconsistências, `DuplicatasPage`, `CadastrarProdutoPage`, `ImportarProdutosPage`). Preservar gates de papel e o `autoAnalisar` de uma vez por visita (`onAutoAnalisado`). `CatalogoPage` fica com busca + listagem + scanner.
- `frontend/src/components/produtos/ImportacaoProdutosSection.tsx:256` -- CTA passa a apontar `/normalizacao/duplicatas?verificarDuplicatas=1` (atualizar teste em `.test.tsx:342`).
- Testes existentes a adaptar: `AppShell.test.tsx`, `nav-items.test.ts`, `App.test.tsx` (linhas 128, 287-349, 468 assumem 2 navs "Navegação principal"), `PedidosPage/EstoquesPage/NormalizacaoPage/CatalogoPage.test.tsx`.

## Tasks & Acceptance

**Execution:**
- [ ] `frontend/src/lib/marca.ts` + teste -- `nomeDaMarca`
- [ ] `frontend/src/components/shell/nav-items.ts` + `nav-items.test.ts` -- grupos, papéis, grupo vazio some
- [ ] `frontend/src/index.css` e `ScannerProdutoFab.tsx` -- tokens novos, fab sem deslocamento
- [ ] `frontend/src/components/shell/MenuLateral.tsx` e `AppShell.tsx` -- sidebar 240/64, tooltips e painel flutuante ao recolher, gaveta ☰, topo (Treinamento, Ajuda, menu da conta com Meu perfil e Sair), cart-badge, persistência
- [ ] páginas e `App.tsx` -- rotas próprias no lugar das abas, redirecionamento do link antigo, CTA da importação
- [ ] testes -- `AppShell.test.tsx` (papéis, ativo/`aria-current`, grupo ativo aberto, recolher e `localStorage` indisponível, gaveta: foco/`Esc`/fechar ao escolher, gate de MFA com menu visível, badge) e `App.test.tsx`/páginas (cada rota nova abre a tela certa, gates, redirecionamento)

**Acceptance Criteria:**
- Given desktop ≥768px, when carrega, then menu azul-marinho de 240px com marca do ambiente, grupos filtrados por papel e "Meu perfil" no rodapé.
- Given uma rota aberta, when o menu renderiza, then o item dela tem `aria-current="page"`, negrito branco, barra vermelha e o grupo está aberto.
- Given as seis rotas novas, when abertas por link, then cada uma cai direto na tela certa, sem abas.
- Given o botão de recolher, when acionado, then 64px com ícones, tooltip e painel flutuante, lembrado; sem `localStorage` abre expandido.
- Given celular <768px, when carrega, then sem bottom nav nem menu fixo; ☰ abre gaveta com os mesmos grupos, fechando ao escolher, `Esc` ou toque fora, com foco indo e voltando; `fab-scanner` no canto inferior direito.
- Given MFA pendente, when navega, then menu visível e toda rota vai a `/configuracoes`.
- Given o topo de 56px branco, when carrega, then Treinamento (se for o caso), Ajuda e menu da conta à direita; `cart-badge` no item Carrinho.

## Design Notes

- Os grupos Cadastros e Administração da tabela do EXPERIENCE.md **não entram nesta story**: suas rotas nascem na 17.2. Até lá, tudo continua em `/configuracoes` ("Meu perfil" no rodapé), então nada fica inacessível. A 17.2 acrescenta os dois grupos em `nav-items.ts`.
- Ajuda: botão do topo que abre um `Dialog` curto ("Dúvidas de uso? Fale com o administrador da sua empresa."); sem página de ajuda nova.
- Painel flutuante do menu recolhido: `DropdownMenu` do Radix por grupo (já instalado).

## Verification

**Commands:**
- `cd frontend && npx tsc -b && npm run lint && npm test` -- expected: limpo e verde

**Manual checks (if no CLI):**
- Abrir em 1280px e 390px: menu, gaveta, ativo em vermelho, fab no canto.

## Auto Run Result

Status: done.

**Mudança:** menu lateral azul-marinho agrupado (240px, recolhível a 64px com tooltip e painel flutuante), topo branco de 56px (Treinamento, Ajuda, menu da conta), gaveta por ☰ no celular, sem bottom nav; as seis abas viraram rotas próprias e o link antigo `/normalizacao?verificarDuplicatas=1` redireciona.

**Arquivos:** `frontend/src/components/shell/` (`AppShell.tsx`, `MenuLateral.tsx`, `nav-items.ts`, `useMenuEstado.ts`), `frontend/src/lib/marca.ts`, `frontend/src/index.css` (tokens), `ScannerProdutoFab.tsx` (sem deslocamento da bottom nav), `App.tsx` (rotas), seis páginas novas (`CadastrarProdutoPage`, `ImportarProdutosPage`, `PedidosFilaPage`, `LancarSaldoPage`, `MovimentacoesPage`, `DuplicatasPage`), páginas `Pedidos/Estoques/Normalizacao/Catalogo` sem abas, CTA da importação, testes.

**Revisão:** 3 patches (todos low), 0 adiados, 27 rejeitados. Follow-up recomendado: não (score 3 = 1×3 low).

**Verificação:** `tsc -b` limpo; `oxlint` limpo nos arquivos do shell; `vitest run` 922 testes verdes (64 arquivos).

**Riscos residuais:** visual (cores, barra vermelha, gaveta, fab no canto, rolagem interna do `<main>` no desktop e moldura de Treinamento) só verificado por classes em jsdom — conferir a olho em 1280px e 390px; alguns títulos ficam duplicados (`h1` da página + `h2` da seção) até as Stories 17.3–17.5; no menu recolhido o item ativo do painel flutuante só tem negrito e `aria-current`; grupos Cadastros/Administração entram na 17.2.
