---
title: 'Páginas próprias para Cadastros e Administração'
type: 'feature'
created: '2026-09-25'
status: 'done'
baseline_revision: '5ea1d9af64dbc8b6d17dea438e19d2804c5cd6a4'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-17-context.md'
  - '{project-root}/_bmad-output/planning-artifacts/ux-designs/ux-stockflow-2026-08-29/EXPERIENCE.md'
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** `/configuracoes` empilha ~12 seções (perfil, promoções, usuários, convites, MFA da empresa, log, filiais, centros de custo, categorias, templates, LGPD); gestor/adm rolam uma tela enorme para achar uma.

**Approach:** Só frontend. Cada seção administrativa ganha rota e página próprias reaproveitando os componentes `*Section` sem mudar comportamento; `/configuracoes` fica só com o que é da própria pessoa; o menu ganha os grupos Cadastros e Administração.

## Boundaries & Constraints

**Always:** rotas e papéis mínimos da tabela "Menu agrupado" do EXPERIENCE.md (Categorias/Templates/Filiais/Centros de custo `adm`; Usuários/Convites/Promoções `gestor`; Segurança/Log de acesso/LGPD `adm`); página sem o papel mínimo mostra `Você não tem acesso a <tela>.` (mesmo padrão de `PedidosFilaPage`) e não monta a seção nem dispara fetch; cada página tem `h1` com o nome da tela; "Decidir promoções" é extraída de `ConfiguracoesPage` para componente próprio com o mesmo comportamento e textos; `/configuracoes` mantém: dados da conta, `SegurancaCard` (minha dupla autenticação), "Solicitar promoção" e `PrivacidadeSection`; grupos novos em `nav-items.ts` com os mesmos papéis (item sem papel some, grupo vazio some); gate de MFA e `/configuracoes` como destino inalterados; textos em PT BR.

**Never:** mudar backend, rotas de API ou comportamento das seções; padrão de lista/tabela/KPIs (17.3–17.5); cor nova fora dos tokens; remover a rota `/configuracoes`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected | Error |
|---|---|---|---|
| Link direto | `/admin/usuarios` como gestor | página com `h1` "Usuários" e a `GestaoUsuariosSection`; item ativo, grupo Administração aberto | — |
| Sem papel | `/cadastros/filiais` como gestor | "Você não tem acesso a Filiais.", sem fetch | — |
| Perfil | `/configuracoes` como adm | só conta, Segurança, solicitar promoção e privacidade; nenhuma seção administrativa | — |
| Link antigo | `/configuracoes` | abre normalmente | — |
| Gate MFA | sessão pendente, `/admin/usuarios` | vai a `/configuracoes` | — |

</intent-contract>

## Code Map

- `frontend/src/pages/ConfiguracoesPage.tsx` -- hoje monta tudo (linhas ~556-712); tirar "Decidir promoções" (estado `pendentes`/`decidir`, ~l.443-540 e 626-690) e as seções administrativas; manter `SegurancaCard`, conta, solicitar, `PrivacidadeSection`. Atualizar o docblock.
- `frontend/src/components/promocoes/DecidirPromocoesSection.tsx` (novo) -- "Decidir promoções" extraída (`GET /api/promocoes`, `POST /api/promocoes/{id}/decisao`).
- `frontend/src/pages/admin/` (novos) e `frontend/src/pages/cadastros/` -- uma página fina por rota (`CategoriasPage`, `TemplatesPage`, `FiliaisPage`, `CentrosCustoPage`, `UsuariosPage`, `ConvitesPage`, `PromocoesPage`, `SegurancaEmpresaPage`, `LogAcessoPage`, `LgpdPage`) via um helper `PaginaAdmin` (`h1` + gate `rankPapel` + refusal). `SolicitacoesExclusaoSection` já se auto-gateia; a página LGPD usa o mesmo gate `adm`.
- `frontend/src/components/shell/nav-items.ts` -- acrescentar grupos Cadastros (ícone `Database`/similar) e Administração (`Settings`/similar); atualizar comentário e `nav-items.test.ts`.
- `frontend/src/App.tsx` -- registrar as 10 rotas dentro de `RotaProtegida`.
- Seções `*Section` (`categorias`, `nomenclatura`, `filiais`, `centroscusto`, `usuarios/GestaoUsuariosSection`, `ConvitesSection`, `SolicitacoesExclusaoSection`, `seguranca/MfaEmpresaSection`, `logs/LogAcessoSection`) -- reusadas sem alteração de comportamento (podem trazer `h2` próprio; título duplicado aceito até 17.3–17.5).
- Testes: `ConfiguracoesPage.test.tsx` (mover casos administrativos para testes das páginas novas), `AppShell.test.tsx`, `App.test.tsx`.

## Tasks & Acceptance

**Execution:**
- [ ] `DecidirPromocoesSection.tsx` -- extrair sem mudar comportamento
- [ ] páginas + helper -- 10 páginas com `h1` e gate de papel
- [ ] `ConfiguracoesPage.tsx` -- reduzir a conta/MFA pessoal/solicitar/privacidade
- [ ] `nav-items.ts` e `App.tsx` -- grupos e rotas
- [ ] testes -- cada rota abre a seção certa, gate por papel sem fetch, `/configuracoes` sem seções admin, menu por papel (usuario/almoxarife/gestor/adm)

**Acceptance Criteria:**
- Given as 10 rotas, when abertas por quem tem o papel mínimo, then mostram título e a seção de hoje sem mudar comportamento.
- Given uma rota sem o papel mínimo, when carrega, then mostra a recusa e não chama a API.
- Given `/configuracoes`, when aberta, then só mostra conta, minha dupla autenticação, solicitar promoção e privacidade, e continua funcionando como link antigo.
- Given o menu, when o papel é gestor ou adm, then aparecem Cadastros/Administração só com os itens permitidos.

## Verification

**Commands:**
- `cd frontend && npx tsc -b && npm run lint && npm test` -- expected: limpo e verde

## Auto Run Result

Status: done (recuperado manualmente)

**O que aconteceu:** a sessão do `bmad-loop` (run `20260925-145537-04da`) bateu o limite de uso da conta durante a review, com a implementação completa e a spec em `in-review`. O run foi parado para retomar com outra conta. **Não houve review pass concluído.**

**Verificação na recuperação:**
- `tsc -b`: limpo.
- `vitest run` completo: 946 testes verdes.
- As 10 rotas novas estão em `App.tsx`: `cadastros/{categorias,templates,filiais,centros-custo}` e `admin/{usuarios,convites,promocoes,seguranca,log-acesso,lgpd}`.
- O papel mínimo de cada página confere com a tabela do `EXPERIENCE.md`: gestor em Usuários, Convites e Promoções; adm nas demais. O servidor continua sendo a autoridade.
- `/configuracoes` ficou só com o que é da própria pessoa. Story só de frontend, sem backend nem migration.
