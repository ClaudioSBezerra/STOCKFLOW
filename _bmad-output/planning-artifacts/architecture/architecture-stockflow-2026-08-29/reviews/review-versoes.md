---
name: 'review-versoes'
type: review
target: 'architecture-stockflow-2026-08-29/ARCHITECTURE-SPINE.md'
purpose: 'Auditar se cada decisão comprometida (stack, versões, ADs novas AD-24..AD-30) foi de fato pesquisada na web/verificada contra a realidade, e não apenas afirmada a partir de dado de treinamento.'
created: '2026-09-19'
method: 'Leitura integral do spine + WebSearch para cada item de versão/manutenção não citado com fonte explícita + leitura do package.json real do FB_APU02 (referência de stack citada pelo spine) para conferir se o "ratificado do FB_APU02" bate com o código real.'
---

# Revisão de Versões — Architecture Spine stockflow (2026-09-19)

## Veredito

**Parcialmente verificado.** A leitura da seção "AD-24 a AD-30" está **correta**: nenhuma delas introduz biblioteca nova não verificada — são todas tabela SQL nova + lógica de aplicação (`ORDER BY` para FEFO, `UPDATE ... RETURNING` para contador, cálculo de saldo via query, tabelas `filiais`/`centros_custo` simples). Conferido linha a linha, não há nenhuma dependência de terceiro nova nesse bloco.

Mas a seção **Stack** (que o brief do usuário assume já fechada desde 2026-08-29) tem **dois problemas reais** encontrados por pesquisa web nesta rodada — um deles (React) é uma versão que já ficou desatualizada exatamente no intervalo entre a pesquisa original e o `updated: 2026-09-19` do próprio documento — e um padrão sistemático de itens "ratificados" sem citação de método/data de verificação (diferente do padrão exemplar usado para `gopdf`/`excelize`, que documentam "pesquisa web em 2026-08-29"). Nenhum desses itens tem uma nota `[NOT VERIFIED]`/Deferred cobrindo o problema — estão afirmados como fato encerrado.

## Achados por severidade

### ALTO

1. **React "19.2.x" já está desatualizado no momento em que o spine foi atualizado.** Pesquisa web confirma: React 19.2 saiu em out/2025, patch mais recente da linha é 19.2.8 (21/jul/2026), mas **React 19.3.0 saiu em 9/set/2026** — 10 dias antes do `updated: 2026-09-19` do próprio spine. O pin em "19.2.x" não é errado por natureza, mas não há nenhuma citação de pesquisa (diferente de `gopdf`/`excelize`, que dizem explicitamente "após pesquisa web" com data) — indício de que o número foi extrapolado do padrão de cadência de release conhecido em treinamento, não checado contra o real no dia do fechamento. Houve também CVEs críticas em React Server Components (CVE-2025-55182 e correlatas, dez/2025–jan/2026) — não há como saber sem verificar se afetam esta stack (provavelmente não, já que não é Next.js/RSC, mas o spine não registra a checagem).

2. **React Router DOM "6.x" foi ratificado sem re-verificação, apesar de React ter sido deliberadamente saltado um major (18→19.2).** Conferido no `package.json` real do `FB_APU02`: `react-router-dom: ^6.22.3` — o spine ratifica exatamente essa major. Só que a pesquisa web mostra que o ecossistema já avançou duas majors: **React Router v7** (nov/2024, suporte oficial a React 19, fusão com Remix) é o mainstream atual, e **React Router v8** já foi lançado (blog oficial "React Router v8" do time Remix) — com `react-router-dom` sendo **removido em v8** (era só um pacote-espelho de compatibilidade v6→v7). O spine aplica o tratamento "atualizado deliberadamente" a Go/React/TypeScript/Vite na mesma tabela, mas trata React Router como "ratificado" sem verificar se a v6 ainda é a escolha certa ao lado de um React já em 19.x — exatamente o tipo de inconsistência que o método de pesquisa dos outros itens deveria ter capturado.

### MÉDIO

3. **`shadcn/ui + Tailwind CSS` — "ratificado do FB_APU02, confirmado ativamente desenvolvido em 2026" esconde um gap de major version.** Conferido no `package.json` real do `FB_APU02`: `tailwindcss: ^3.4.3`, `tailwindcss-animate: ^1.0.7` (plugin legado). Pesquisa web mostra que **Tailwind CSS v4 é hoje o padrão do ecossistema 2026** (v4.3.2, jun/2026) — reescrita com breaking changes (config movida para CSS via `@theme`, cores em OKLCH em vez de HSL) — e que o próprio shadcn/ui já **depreciou `tailwindcss-animate` em favor de `tw-animate-css`** para v4. O spine confirma só que "Tailwind ainda é ativamente desenvolvido" (verdade trivial), não qual major deveria ser adotado — o mesmo tipo de decisão que motivou o salto deliberado de Go/React/TS/Vite não foi feita aqui, apesar do FB_APU02 estar preso à mesma v3 legada que os outros itens da tabela.

4. **TypeScript 7.0.x está corretamente identificado (GA confirmado em 8/jul/2026, versão 7.0.2), mas falta uma ressalva relevante encontrada na pesquisa:** a TS 7.0 GA **não tem API programática estável** (prevista só para 7.1) — `typescript-eslint` e integrações de framework (Vue/Svelte/Astro/Angular) ainda não conseguem rodar sobre o compilador nativo. Como o stockflow é React puro (não um desses frameworks), o impacto direto é provavelmente baixo, mas se a stack usa ESLint com regras de tipo (comum em projetos React/TS), vale confirmar na story se a toolchain de lint funciona sobre TS7 antes de trocar — o spine não registra essa checagem.

### BAIXO

5. **Padrão de citação inconsistente dentro da própria seção Stack.** `golang-jwt/jwt` ("confirmado ativamente mantido em 2026") e `TanStack Query` ("confirmado current") não trazem data/método de verificação, ao contrário de `gopdf`/`excelize` (que citam "pesquisa web" com data). Verifiquei ambos nesta rodada e as afirmações **se sustentam**: `golang-jwt/jwt v5` tem release em 28/jan/2026 e é mantido ativamente por um time dedicado; TanStack Query v5 tem patch em 5.102.8 (~6 dias antes de 19/set/2026), ainda a linha estável corrente (v6 já em RC, mas isso não invalida v5 como escolha atual). Não é um erro de fato, mas quebra a rastreabilidade que o resto do documento estabeleceu como padrão de qualidade.

6. **`golang-migrate` citado no parágrafo de `migrations/` ("golang-migrate ou equivalente já usado no FB_APU02") sem versão nem citação de pesquisa.** Verificado: o projeto segue ativo (release em 9/set/2026, atividade em 31/ago/2026), então a afirmação implícita se sustenta — mas é citado como fato ("já usado no FB_APU02") sem confirmar se de fato é essa a lib usada lá, e sem o mesmo nível de evidência dos outros itens de Stack.

7. **Go 1.27 está correto e é a versão mais atual encontrada** (1.27 lançado 19/ago/2026, patch 1.27.1 em 1/set/2026) — mas, de novo, sem citação de método/data de verificação no documento (mesmo padrão do item 5). PostgreSQL 15 (EOL ~nov/2027) também confere com a pesquisa (end-of-life.org: 11/nov/2027).

## Confirmação do escopo pedido: AD-24 a AD-30

Revisadas as sete ADs novas linha a linha:

- **AD-24** (Lote/FEFO): tabela `lotes` nova + `ORDER BY data_validade NULLS LAST, criado_em ASC` — SQL puro.
- **AD-25** (reserva de saldo): tabela `reservas_pedido_item` + saldo calculado por soma/subtração via query — SQL puro, sem lib de cache/fila.
- **AD-26** (contador sequencial): tabela `contadores_produto` + `UPDATE ... RETURNING` atômico — SQL puro, explicitamente evita `sequence` nativa do Postgres por razão de isolamento multi-Empresa, não por preferência de lib.
- **AD-27** (Filial > Estoque): tabela `filiais` + FK — SQL puro.
- **AD-28** (Centro de Custo): tabela `centros_custo` + FK opcional coexistindo com texto livre — SQL puro.
- **AD-29** (produto sem saldo): nenhuma tabela nova, só regra de leitura — lógica de aplicação pura.
- **AD-30** (importação em massa gera Lote sem validade): mapeamento de linha de planilha para `lotes` — lógica de aplicação pura.

**Confirmado: nenhuma AD nova introduz biblioteca de terceiros não verificada.** A única referência a uma lib candidata no raio dessas mudanças (`klassmann/cpfcnpj` para validação de CNPJ, FR-41) está corretamente no bloco **Deferred**, não comprometida como decisão — tratamento correto, não é um achado.

## Fontes consultadas (WebSearch, 2026-09-19)

- Go 1.27/1.27.1 — go.dev/doc/go1.26, endoflife.date/go
- React 19.2/19.3, CVEs RSC — github.com/react/react/releases, makerkit.dev, reactnative.dev/blog
- TypeScript 7.0 GA e limitação de API programática — devblogs.microsoft.com/typescript, infoq.com, visualstudiomagazine.com
- Vite 8.0/Rolldown — vite.dev/blog/announcing-vite8, infoq.com
- react-router-dom / v7 / v8 — npmjs.com/package/react-router-dom, remix.run/blog/react-router-v8
- golang-jwt/jwt v5 manutenção — github.com/golang-jwt/jwt, pkg.go.dev
- TanStack Query v5 — tanstack.com/blog, npmjs.com
- Tailwind CSS v4 / shadcn/ui — ui.shadcn.com/docs/tailwind-v4, en.wikipedia.org/wiki/Tailwind_CSS
- PostgreSQL 15 EOL — end-of-life.org/postgresql/15
- pquerna/otp — inconclusivo via busca (já corretamente marcado como Deferred/não confirmado pelo próprio spine)
- golang-migrate — github.com/golang-migrate/migrate, pkg.go.dev
- `/home/claudio/projetos/FB_APU02/frontend/package.json` (real, lido diretamente) — confirma `react-router-dom ^6.22.3`, `tailwindcss ^3.4.3`, `tailwindcss-animate ^1.0.7`, `@tanstack/react-query ^5.90.20`, `react ^18.3.1`, `typescript ^5.2.2`, `vite ^5.2.0`
