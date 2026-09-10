---
title: 'market research: Multi-tenancy com isolamento total de dados e ambiente de treinamento/sandbox por cliente (B2B SaaS)'
type: 'market'
topic: 'Multi-tenancy com isolamento total de dados e ambiente de treinamento/sandbox por cliente (B2B SaaS)'
decision: 'Fundamentar PRD launch-grade para duas features do stockflow: multi-tenancy com isolamento total de dados; ambiente de treinamento/sandbox por cliente'
source: 'native run (quick preset)'
status: complete
preset: 'quick'
validation: 'normal'
created: '2026-09-10'
updated: '2026-09-10'
---

# market research: Multi-tenancy com isolamento total de dados e ambiente de treinamento/sandbox por cliente (B2B SaaS)

**Decision this research serves:** Fundamentar PRD launch-grade para duas features do stockflow: multi-tenancy com isolamento total de dados; ambiente de treinamento/sandbox por cliente

## Resumo executivo

Padrão dominante em SaaS B2B multi-tenant maduro: **1 usuário : N organizações** (associação por membership, com papel por org), **super admin de plataforma separado do admin de cada tenant**, **seletor de organização no canto superior esquerdo**, e onboarding que tende a ser **admin-provisioned/JIT via SSO** em produtos verticais/enterprise (self-service instantâneo é mais um padrão de PLG horizontal). Para ambiente de treinamento/sandbox, o padrão dominante em produtos B2B complexos (ERP/CRM) é **tenant espelho separado** (não um flag dentro do tenant real), com nomenclatura convergindo em "Sandbox", ativação **gated por plano/tier** (não default para todo cliente) e distinção visual mais forte via **subdomínio/URL**, reforçada por um banner (que na prática tende a ser insuficiente sozinho). Confiança geral: média-alta nos padrões de UX/estrutura (fontes primárias de produto); baixa em estatísticas de prevalência (nenhuma fonte quantificou % de adoção por padrão).

## Dimensão 1 — Multi-tenancy com isolamento total de dados

**1 usuário : N tenants.** O padrão dominante em ferramentas B2B/dev-tool é um usuário poder pertencer a múltiplas organizações/workspaces simultaneamente, cada uma com papel e billing independentes — GitHub, Linear e Vercel confirmam isso em documentação oficial; Slack permite o mesmo por e-mail, ainda que de forma mais solta [1]. Plataformas de auth multi-tenant (Frontegg) formalizam isso como "usuário → membership → tenant", com token escopado por tenant na troca de contexto.

**Onboarding: self-service vs. provisionamento manual.** Não há um corte limpo por vertical, mas a tendência é: PLG horizontal → self-service instantâneo; B2B vertical/enterprise → admin convida usuários, ou provisionamento manual (que não escala e empurra o vendor para portais de admin self-serve), ou JIT (just-in-time) provisioning no primeiro login via SSO [4]. Nenhuma fonte trouxe estatística dura de prevalência por vertical — é uma lacuna.

**Papel "platform owner"/super admin.** Existe como prática recomendada explícita: um papel interno restrito (poucas pessoas, ex. 3-5 citado pela Frontegg) com acesso cross-tenant — impersonation, billing, feature flags, analytics agregada — estruturalmente separado do admin de cada empresa cliente, que fica escopado ao próprio tenant [2]. Exemplos reais análogos: Atlassian Cloud tem "org admin"/"site admin" que administra múltiplos sites/orgs; Microsoft Power Platform tem "service admin role" acima dos admins por ambiente.

**UI de troca de organização.** Padrão quase universal: seletor no canto superior esquerdo, atrás do nome da org/workspace atual, abrindo dropdown com todas as orgs do usuário + opção de criar/entrar em outra — confirmado em Notion, Linear, Vercel (inclusive via CLI, `vercel switch`) e Atlassian [3]. Nenhuma fonte pesquisada usava subdomínio-por-org como mecanismo primário de troca (padrão comum em ERPs/WMS verticais, mas não verificado nesta rodada — ver lacunas).

**Lacunas identificadas:** exemplos primários de ERPs/WMS multi-empresa (NetSuite OneWorld, Odoo multi-company, SAP Business One) não foram levantados nesta rodada — são o análogo mais próximo do domínio de estoque/material de obra e vale aprofundar depois; padrão de subdomínio-por-tenant também não verificado.

## Dimensão 2 — Ambiente de treinamento/sandbox por cliente

**Arquitetura: tenant espelho vs. flag no mesmo tenant.** Em plataformas B2B complexas (ERP/CRM), o padrão dominante é um **tenant/org totalmente separado**, provisionado como cópia de metadados (e, em alguns tipos, dados) da produção, com login e ciclo de refresh próprios — não um flag dentro do tenant ao vivo [5]. O padrão "modo de treino no mesmo tenant" aparece mais em ferramentas simples/prosumer (empresa de exemplo do QuickBooks, dados fictícios de tour de produto), provavelmente porque isolamento total de modelo de dados é mais seguro para domínios sensíveis a integridade (financeiro, estoque).

**Exemplos concretos.** Salesforce é a referência mais documentada: 4 tipos de sandbox (Developer, Developer Pro, Partial Copy, Full Copy) que variam por cópia de dados e cadência de refresh (diário a cada 29 dias), criados via Setup > New Sandbox, cada um com URL própria injetando "sandbox" no domínio [6]. NetSuite usa uma "Sandbox Account" única, refresh manual/sob demanda a partir da produção. Workday distingue "Sandbox" e "Sandbox Preview" (produção + funcionalidades da próxima release). HubSpot tem "Standard Sandbox" (gated por tier Enterprise) e contas dev/test gratuitas com expiração de 90 dias. ServiceNow chama de "sub-production instances", com regras de nomenclatura rígidas (proíbe sufixos como "demo"/"poc"/"pov").

**Nomenclatura e ativação.** Convergência em "Sandbox" como termo principal, com variantes (UAT, Test Drive, Developer/Test account, Sandbox Preview). Ativação é tipicamente **gated por plano/tier**, não default para todo cliente: contagem/tipo de sandbox atrelado à edição de produção (Salesforce), tier Enterprise exigido (HubSpot), tiers superiores para recursos avançados de refresh (NetSuite) [6]. O refresh cadence funciona como o limite de "staleness" embutido — diário para sandboxes sem dados reais, até 29 dias para réplicas completas.

**UX de distinção visual.** O padrão mais forte e verificável é via **URL/subdomínio** — Salesforce injeta "sandbox" no hostname do My Domain quando Enhanced Domains está ativo [7]. Existe também banner interno, mas evidências indiretas (extensões de terceiros para banners customizados, feature request aberta para customizar a cor do banner padrão) sugerem que o indicador nativo sozinho é visto como insuficiente por admins que lidam com múltiplos sandboxes.

**Lacunas identificadas:** mecanismo específico da SAP (S/4HANA via SAP LaMa, ou BTP trial subaccounts) não foi levantado; exemplos de WMS puro (Manhattan Associates, Blue Yonder, Fishbowl) — o análogo mais próximo do domínio do stockflow — não foram pesquisados nesta rodada; cor/texto padrão exato do banner do Salesforce não confirmado; limites numéricos de sandbox por cliente (NetSuite/Workday/HubSpot) não confirmados.

## Fontes principais

| # | Fonte | Publisher | Classe |
|---|---|---|---|
| 1 | linear.app/docs/workspaces, docs.github.com, vercel.com/docs/cli/switch | Linear/GitHub/Vercel | product doc |
| 2 | frontegg.com/blog/saas-multitenancy | Frontegg | vendor blog |
| 3 | notion.com/help, linear.app/docs, vercel.com/docs | Notion/Linear/Vercel | product doc |
| 4 | workos.com/blog/b2b-saas-onboarding-organizations-users | WorkOS | vendor blog |
| 5 | salesforce.com/platform/sandboxes-environments, doc.workday.com | Salesforce/Workday | product doc |
| 6 | salesforce.com/platform/sandboxes-environments, developers.hubspot.com, gurussolutions.com | Salesforce/HubSpot/Gurus Solutions | product doc / partner doc |
| 7 | help.salesforce.com, ideas.salesforce.com | Salesforce | product doc / community |

_Validação: normal (spot-check por dimensão). Preset: quick (2 dimensões, ~10 fontes cada, 1 rodada). Digests completos em `digests/multitenancy-r1-1.md` e `digests/sandbox-r1-1.md`._
