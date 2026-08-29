# Addendum — stockflow

Material de apoio ao PRD (`prd.md`) que não pertence ao corpo do documento: decisões de arquitetura herdadas, desenho técnico de referência do Keycloak, visão de produto de longo prazo, análise competitiva, o processo de execução planejado, e o detalhe técnico de referência (modelo de dados, achados de segurança, templates, categorias) herdado do projeto original. Serve como insumo direto para a fase de Arquitetura e para decisões de produto futuras — não é normativo como o PRD.

## A. Arquitetura herdada do PRD original (Catálogo de Materiais / Catalogo-Obras) e pontos de conflito com o stack-alvo

O PRD e a arquitetura originais (repositório `Catalogo-Obras`, `_bmad-output/planning-artifacts/architecture/architecture-Catalogo-Obras-2026-08-17/ARCHITECTURE-SPINE.md`) assumiam **Go + PostgreSQL + Redis + RabbitMQ**, com paradigma Hexagonal/Clean Architecture (4 bounded contexts: `catalogo`, `pedidos`, `identidade`, `notificacao`). Como o stack-alvo real do stockflow segue o padrão do `FB_APU02` (Go stdlib `net/http`, sem router/ORM externo, PostgreSQL, Redis presente no compose mas não consumido pelo código Go, **sem RabbitMQ**), os seguintes pontos exigem revisão na fase de Arquitetura:

1. **RabbitMQ (AD-2, AD-7, AD-12 da arquitetura original)** — usado para (a) outbox pattern de eventos de domínio que viram atualização em tempo real, (b) envio assíncrono de e-mail transacional, (c) comunicação inter-módulo via eventos (ex. Catálogo→Pedidos no evento de mesclagem de duplicatas). Sem RabbitMQ, alternativas: outbox lido diretamente do Postgres (polling ou `LISTEN/NOTIFY`) sem fila externa; e-mail via goroutine/worker interno em vez de fila durável.
2. **Redis Pub/Sub (AD-3, AD-7)** — base do mecanismo de atualização quase em tempo real (equivalente ao `onSnapshot` do Firestore no protótipo atual). Não era uma decisão fechada no PRD original ("recomendação do assistente, aceita, não fechada"). Alternativa mais simples: broadcaster in-process, viável se o deploy for single-host sem múltiplas réplicas da aplicação (como é o caso do `FB_APU02`).
3. **Cache de papel com TTL curto via Redis (AD-14)** — usado para que revogação/rebaixamento de papel valham já na próxima requisição, sem colocar o papel no JWT. Alternativa sem Redis confirmado: leitura direta no Postgres a cada requisição, ou cache in-memory local (aceitável em single-host).
4. **Topologia de deployment** — a arquitetura original assumia 4 containers (app, PostgreSQL, Redis, RabbitMQ) + volume nomeado para fotos. Se o stack-alvo não provisiona RabbitMQ, essa topologia precisa ser redesenhada — provavelmente alinhada ao `docker-compose.yml` / `installer/cliente-aws/` do `FB_APU02`.

Nenhuma dessas dependências era tratada como opcional no documento original — eram decisões `[ADOPTED]`/binding. Removê-las exige uma decisão de arquitetura nova, não apenas a ausência de menção.

**Outras decisões de arquitetura originais que seguem válidas independente do stack de mensageria:**
- `usuarios.papel` como única fonte do papel efetivo (enum); `solicitacoes_promocao` com máquina de estado própria.
- Autorização centralizada em middleware, três formas obrigatórias por rota (papel mínimo / comparação relativa ao alvo / filtro de escopo em listagem).
- Dimensões sempre `{valor, unidade}` estruturado, nunca string livre.
- Fotos em disco local, sempre em volume Docker nomeado e persistente.
- Soft-delete de Produto via `deleted_at IS NULL` em todo read; produto soft-deleted nunca reentra em mesclagem, mas mantém foto em disco para auditoria permanente.
- Revalidação de estoque sempre com `SELECT ... FOR UPDATE` (lock pessimista) na mesma transação; ordem canônica de lock `(produto_id, estoque_id)` ascendente para evitar deadlock.
- Exclusão de Estoque verifica quantidade residual E pedido pendente na mesma transação.
- Bootstrap do primeiro `adm` via CLI dedicado, nunca endpoint HTTP.

**Modelo de dados (ERD core) herdado, ainda válido:** `USUARIOS`, `SOLICITACOES_PROMOCAO`, `CATEGORIAS`, `PRODUTOS`, `PRODUTO_ESTOQUE` (M:N), `ESTOQUES`, `MOVIMENTACOES` (origem obrigatória + destino nullable + pedido nullable), `PEDIDOS`, `PEDIDO_ITENS`, `MESCLAGENS_DUPLICATAS` + `MESCLAGEM_PRODUTOS_REMOVIDOS`, `TOKENS_ACAO` (verificação e-mail + reset senha), `IMPORTACOES` + `IMPORTACAO_LINHAS` (suporte a retomada de importação interrompida), `NOMENCLATURA_TEMPLATES` (28 seed rows). Detalhe campo-a-campo em §F.

**Recomendação técnica original para tempo real (contexto para a decisão de arquitetura do item 2 acima):** o PRD de origem registra a seguinte justificativa para SSE + Redis Pub/Sub, útil mesmo que o mecanismo mude:
- *Por que não polling:* a atualização instantânea entre usuários via `onSnapshot` do Firestore já é uma expectativa estabelecida — regredir para polling seria uma perda de UX perceptível, com carga proporcional ao intervalo escolhido.
- *Por que não WebSocket:* os fluxos são unidirecionais na prática (servidor empurra, cliente só lê) — WebSocket adiciona complexidade de conexão (handshake, ping/pong, reconexão com estado) sem necessidade, e tende a ter mais atrito em proxies/firewalls corporativos.
- *Por que SSE:* HTTP puro, reconecta sozinho no cliente (`EventSource` nativo), resolve exatamente o padrão "servidor empurra evento, cliente atualiza a tela".
- *Por que Redis Pub/Sub (no desenho original):* desacopla "quem gerou o evento" de "quem tem uma conexão SSE aberta" — só necessário se o backend rodar em mais de uma instância. **Ponto-chave para a decisão de Arquitetura do stockflow:** se o deploy for single-host (como no `FB_APU02`), essa necessidade desaparece e um broadcaster in-process resolve sem introduzir Redis como dependência ativa.
- Pontos ainda em aberto no desenho original, independente do mecanismo escolhido: granularidade dos canais/tópicos (geral vs. por coleção vs. por estoque), payload mínimo (id + tipo de mudança, cliente rebusca) vs. payload completo, e comportamento de reconexão/resync após queda (sempre GET completo, nunca replay de eventos perdidos).

## B. Desenho técnico de referência — Keycloak SSO (replicar do `FB_APU02`, com desvios deliberados anotados)

Implementado no `FB_APU02` como **opção paralela** ao login por e-mail/senha — não substitui, não altera o sistema de sessão existente. Origem: portado de uma skill interna reaproveitável (`keycloak-identity-react-go`) que gera módulos de login OIDC/PKCE para stacks React/Vite + Go a partir de um realm Keycloak real.

**Backend (padrão a replicar):**
- Pacote dedicado (`iam/` no `FB_APU02`) com: cliente JWKS com cache em memória thread-safe (TTL ~1h), middleware de validação de token.
- Validação: assinatura RS256 via `kid` → chave pública JWKS; `exp` obrigatório; leeway de ~30s para desvio de relógio; `iss` deve bater com a URL do realm; **`azp`** (authorized party — não `aud`, pois Keycloak não garante `aud == client_id`) validado contra allowlist de client ids permitidos.
- Claims consumidas: `sub`, `azp`, `email`, `email_verified`. Roles/grupos do Keycloak (`realm_access`/`resource_access`) **não são consumidos** — mesma lacuna documentada como extensão futura no `FB_APU02`.
- Endpoint de troca de token (`POST /api/auth/sso/keycloak`): protegido só nessa rota (nunca globalmente); exige `email_verified=true`; busca usuário por e-mail **case-insensitive** (`LOWER(email)=LOWER($1)` no `FB_APU02`, para não duplicar identidade por capitalização diferente entre Keycloak e conta local — replicar no stockflow, já capturado como consequência de FR-34); nunca cria usuário novo; emite os mesmos tokens de sessão próprios usados pelo login por senha. Token do Keycloak nunca é persistido.
- Endpoint de config runtime (`GET /api/auth/sso/config`, público): expõe `base_url`, `client_id`, `redirect_uri`, `scopes` em runtime, não build-time — permite que a mesma imagem sirva ambientes com/sem SSO habilitado. Retorna `enabled:false` se qualquer uma das variáveis de ambiente estiver faltando.
- Rate limit dedicado igual ao do login por senha.
- Variáveis de ambiente (nomes, sem valores): `IAM_BASE_URL`, `IAM_CLIENT_ID`, `IAM_ALLOWED_CLIENT_IDS` (CSV), `IAM_REDIRECT_URI`, `IAM_SCOPES` (default `"openid profile email"`).

**Frontend (padrão a replicar, com uma divergência deliberada):** sem biblioteca `keycloak-js` — fluxo Authorization Code + PKCE implementado manualmente: gera `code_verifier`/`code_challenge` (S256, via `@noble/hashes` — dependência JS adicionada especificamente porque `crypto.subtle` exige contexto seguro/HTTPS, e nem todo ambiente de desenvolvimento roda em HTTPS) e `state` (CSRF) client-side; troca `code` por token direto no Keycloak; troca esse token pela sessão própria via endpoint do backend; token do Keycloak não é usado depois disso. Alimenta o mesmo hook/context de autenticação já usado pelo login por senha — zero duplicação de gerenciamento de sessão.

- **Logout:** no `FB_APU02`, encerrar uma sessão iniciada via SSO também redireciona para o endpoint de RP-initiated logout do Keycloak (`/protocol/openid-connect/logout`) com um `post_logout_redirect_uri` — esse URI precisa estar registrado no client Keycloak (ver PRD §11, Open Question 5). Replicar no stockflow (já capturado como consequência de FR-34).
- **⚠️ Divergência deliberada de comportamento de login:** no `FB_APU02`, a tela de login **redireciona automaticamente** para o Keycloak sempre que o SSO está habilitado — o login por senha vira um caminho secundário, acessível só via parâmetro de URL (`?password`), escondido da UI padrão. **O stockflow decide diferente**: login por senha continua sendo o caminho padrão e visível na tela; o SSO aparece como um botão adicional ("Entrar com Ferreira Costa"), nunca como redirecionamento automático — confirmado com o usuário nesta rodada de reconciliação. Quem for implementar a partir deste addendum não deve copiar o comportamento de auto-redirect do `FB_APU02` neste ponto específico.
- **Outra divergência menor, já refletida no FR-34 do PRD:** no `FB_APU02`, um e-mail do Keycloak sem conta local correspondente recebe a mensagem "contate o administrador". No stockflow, como o autocadastro público (FR-3) já existe e é aberto, a mensagem orienta a se cadastrar primeiro — mais coerente com o produto, mas é uma escolha deliberada, não uma cópia direta do padrão de origem.

**Realm de referência:** `https://iam.fcxlabs.com/realms/ferreiracosta` (mesmo realm corporativo Ferreira Costa usado pelo `FB_APU02`, client id `fb-apu02`). O stockflow deve registrar client id próprio nesse mesmo realm — provisionamento no Keycloak real exige confirmação humana explícita antes de ser executado.

**Dívida técnica conhecida no `FB_APU02` a não repetir sem decisão consciente:** amplificação de fetch JWKS via `kid` arbitrário; JWKS vazio tratado como sucesso (trava login até 1h); `iss`/URL do JWKS sem normalização de barra final; ausência de autorização por role/grupo do Keycloak.

## C. Processo de execução planejado (fonte: `documentacao-orquestracao-agentes.pdf`)

Este documento descreve o *método*, não requisitos de produto — mas contextualiza como a migração será conduzida:

- Plano operacional para migrar **3 sistemas legados** ("HTML simples", incluindo o stockflow) para o padrão Go + PostgreSQL + React, usando BMAD para especificação e agentes Claude Code para execução autônoma, sob uma camada de governança ("Paperclip").
- **Confirmado nesta rodada:** o repositório de referência que o documento chama de "a apuração assistida" (de onde os 3 projetos leem padrões de build/test/stack) **é o próprio `FB_APU02`** — mesmo projeto usado como referência de stack e Keycloak em todo este addendum.
- **Um único "Arquiteto de padrões" é compartilhado entre os 3 projetos de migração** (não um por projeto) — responsável por manter os 3 arquitetonicamente equivalentes entre si, além de um par Desenvolvedor/QA dedicado por projeto. Isso é uma restrição mais forte do que apenas "alinhar ao `FB_APU02`" pontualmente: decisões de arquitetura tomadas para o stockflow tendem a se propagar (ou ser cobradas) nos outros 2 projetos migrados pelo mesmo processo, e vice-versa.
- Infraestrutura em 3 máquinas: Notebook (planejamento interativo, humano no loop), KVM2 (desenvolvimento — agentes + builds), KVM4 (produção — deploy manual via Coolify). Regra de ouro: agentes vivem só no ambiente de desenvolvimento, nunca tocam produção diretamente.
- Fluxo: planejar via BMAD completo → confirmar definição → stories em fila → migração noturna por agentes (uma story por vez, branch dedicada, auto-validação com testes) → revisão humana pela manhã → merge → deploy manual controlado.
- Guardrails: agentes só trabalham em branch de migração, nunca em `main`; nunca deploy direto; **nunca migração destrutiva em banco compartilhado/produção** (o PRD principal, §9, torna isso explícito para o corte de dados desta migração especificamente: sempre disparado por humano); commit só com testes passando; ambiguidade gera bloqueio registrado, nunca improviso; budgets de custo por agente com auto-pausa.
- Isso é coerente com a infraestrutura de automação (`bmad-loop`) já configurada neste projeto — o stockflow é candidato natural a rodar sob esse processo assim que a Arquitetura e os Épicos/Stories estiverem prontos.

## D. Visão de produto de longo prazo (fora do escopo deste PRD)

Os documentos `stockflow_especificacao_funcional.docx` e `stockflow_analise_competitiva.docx` descrevem uma visão ampla — **não o estado atual, confirmado pelo usuário** — de uma plataforma SaaS multi-tenant vendável a terceiros (construtoras, facilities, hotelaria, indústria, varejo, saúde, condomínios), com módulo de locação de equipamentos, fluxos de aprovação avançados, relatórios/BI, integrações ERP, planos comerciais (Starter/Business/Enterprise) e um roadmap em 3 fases. Resumo condensado, preservado aqui como contexto para decisões de produto futuras, caso a empresa decida comercializar o stockflow:

- **Mercado:** US$2,4bi globais (2025) → projeção US$3,73bi em 2030; Brasil ~US$120M com CAGR 12,1% (2026-2036), acima da média global. Nenhum líder local claro no segmento de construção/facilities em português.
- **Concorrentes diretos:** Sortly (US$49/mês, sem locação/PT-BR), EZOfficeInventory (US$40-65/mês, sem contexto BR), CHEQROOM (US$184/admin/mês, foco AV/mídia), ToolWatch (líder EUA, sem presença BR). Concorrentes de contexto: TOTVS Protheus, SAP Business One, Odoo, Zoho Inventory, inFlow — ERPs genéricos, pesados ou sem locação.
- **Gap identificado:** nenhum concorrente une materiais consumíveis + locação de equipamentos em um só produto, em português, com contexto de obra/facilities brasileiro.
- **Precificação sugerida (aspiracional):** Starter R$297/mês, Business R$697/mês, Enterprise a partir de R$1.997/mês — posicionado entre ferramentas de nicho (R$150-500/mês) e ERPs corporativos (R$3.000+/mês).
- **Roadmap sugerido:** Fase 1/MVP (auth multi-tenant, catálogo, estoques, pedidos simples, relatório básico, PWA) → Fase 2 (locação, aprovação avançada, inventário com contagem dupla, dashboard, API/webhooks) → Fase 3 (white-label, SSO Azure AD/Google, integrações ERP, app nativo, BI).
- **Sinais qualitativos com possível relevância mesmo no curto prazo** (não viram FR nesta versão, mas ficam registrados para a Arquitetura/UX considerarem): uso **mobile-first** por equipes de campo é citado como o principal contexto de uso do setor (já refletido como NFR de usabilidade em campo no PRD §8); **QR Code/código de barras** aparece como funcionalidade padrão de mercado mesmo em soluções de entrada (já incorporado como FR-35 no PRD, não fica só na visão de longo prazo); **importação de planilha sem fricção** é citada como o principal fator de conversão de clientes que hoje usam Excel (já refletido como nota em FR-10/FR-11 no PRD).

Nada do restante (locação, multi-tenant, planos comerciais) é FR neste PRD. Se a empresa decidir seguir essa direção comercial no futuro, esta seção é o ponto de partida para um novo PRD de produto SaaS — não uma extensão deste PRD de migração interna.

## E. Achados de segurança e débito técnico do protótipo atual (herdados do PRD original, para priorização em Arquitetura/Épicos)

1. **Nenhuma autorização por papel no servidor** — toda checagem de `almoxarife` vs. `usuario` hoje é só visual (esconder botão/aba). Toda função de escrita, incluindo aprovação de pedido (que debita estoque), está exposta globalmente sem checagem de papel no código. Endereçado por FR-2.
2. **Chave de API do Google Custom Search exposta e funcional**, associada a uma funcionalidade de busca automática de fotos sem nenhum elemento de interface que a acione — chamável apenas via console do navegador. Ação recomendada: revogar/rotacionar imediatamente, independentemente do cronograma da migração (Non-Goal/Constraint no PRD principal). O `index.html` também expõe a chave do Firebase (achado adicional confirmado durante a extração desta rodada) — tratar ambas como sensíveis.
3. **Exclusão de local de estoque não é cascateada** — remove o documento de `estoques`, mas quantidades em `produtos.estoques[nomeDoLocal]` continuam existindo como chaves órfãs, ainda contadas no total. Endereçado por FR-13.
4. **Reimportação de planilha sempre cria produtos novos**, nunca atualiza por código — depende do usuário rodar manualmente a ferramenta de Duplicatas depois. Endereçado por FR-11.
5. **Compressão de foto inconsistente** — cadastro inicial armazena base64 bruto (até 1 MB, sem redimensionamento); edição posterior redimensiona para 500px e comprime em JPEG 0.82. Endereçado por FR-27.
6. **Fotos armazenadas inline no documento**, apesar de o projeto Firebase ter um bucket de Storage configurado e não utilizado — risco de aproximar o limite de 1 MiB por documento do Firestore à medida que o catálogo cresce. Endereçado por FR-28.
7. **Aprovação de pedido pula silenciosamente itens sem estoque suficiente** no momento da aprovação, sem avisar o almoxarife nem o solicitante. Endereçado por FR-25.
8. **Mesclagem de Duplicatas é destrutiva e sem trilha** — soma a quantidade no item mantido e exclui os demais via `deleteDoc`, sem qualquer registro de auditoria da operação. Endereçado por FR-20.
9. **Quatro implementações distintas de parsing "valor+unidade"** espalhadas pelo código (`chaveAgrupamento`, `splitDim`, `extrairNumUnid`, `parseValUnid`), sintoma direto de dimensões serem texto livre em vez de campos estruturados. Resolvido pela decisão de reestruturar os campos de dimensão (ver §F e FR-8, FR-10, FR-17 no `prd.md`) — a versão-alvo nasce com um único parser, usado uma vez no script de migração para converter o texto livre existente; a planilha de importação (FR-10) já exige colunas de valor e unidade separadas, então não precisa desse parser em regime.
10. **Lista de categorias duplicada em dois lugares** do HTML (form de cadastro e modal de edição) — ~25 valores idênticos, sem fonte única. Lista completa em §H.
11. **"Gerar PDF" não gera PDF de fato** — abre uma aba com HTML imprimível e depende do usuário usar "Imprimir/Salvar como PDF" do navegador. Endereçado nesta versão por FR-26 (revisado para gerar PDF real no servidor, a pedido do usuário nesta rodada).
12. **Fallback para proxies CORS de terceiros** (`corsproxy.io`, `api.allorigins.win`) dentro da funcionalidade morta de busca de fotos (achado 2) — dependência de confiabilidade/privacidade de terceiros não controlados, relevante apenas se essa funcionalidade fosse reaproveitada, o que este PRD explicitamente não recomenda.

## F. Modelo de dados — estado atual (Firestore) e schema-alvo (Postgres)

> **Nota herdada do PRD original:** este levantamento foi feito sobre a estrutura do Firestore. Antes do corte, a empresa move esses dados para um PostgreSQL local (Docker) que espelha essa mesma estrutura, sem redesenho — os achados abaixo continuam valendo como estão; só muda o mecanismo de acesso do script de migração (conexão Postgres direta, em vez de export/API do Firestore). Se a estrutura divergir na prática, este documento precisa de nova verificação antes da implementação das stories de migração.

**Decisão confirmada:** os campos de dimensão migram de texto livre para valor numérico + unidade estruturados (ver `prd.md` FR-8, FR-10, FR-17).

### Coleção `produtos` (Firestore, hoje) → tabela `produtos` (Postgres, alvo)
| Campo | Tipo hoje | Observação |
|---|---|---|
| `nome` | string | obrigatório |
| `codigo` | string | opcional — também é o valor reaproveitado por FR-35 (identificação via QR/código de barras) |
| `categoria` | string | ~25 valores fixos, hoje duplicados em dois lugares do HTML (achado E.10) — no schema-alvo deve ser tabela/enum única (lista completa em §H) |
| `comprimento`, `largura`, `diametro`, `altura`, `espessura`, `lateral` | string livre, ex. `"6m"`, `"100mm"` | **Alvo confirmado:** cada dimensão vira um par `{valor: numeric, unidade: enum}` estruturado. Script único de conversão na migração, reaproveitando a lógica de parsing hoje espalhada (achado E.9) — casos ambíguos ficam para revisão manual via Normalização (FR-17/FR-18). |
| `unidade` | string | `un, m, m², m³, kg, L, cx, rolo, barra, mm, cm, kg/m²` — unidade da *quantidade em estoque* (separada da unidade de cada dimensão) |
| `obs` | string | livre |
| `foto` | string base64 | hoje inline no documento; FR-28 move para storage de objetos |
| `estoques` | map<string,number> | nome do local → quantidade; total = soma dos valores |
| `criadoEm` | timestamp | |

### Coleção `estoques`
| Campo | Tipo |
|---|---|
| `nome` | string (único) |

### Coleção `historico` (append-only)
| Campo | Tipo |
|---|---|
| `produto` | string (nome, desnormalizado) |
| `tipo` | `baixa` \| `transferencia` |
| `origem`, `destino` | string (`destino:'—'` para baixa) |
| `qtd` | number |
| `unidade` | string |
| `obs` | string |
| `timestamp` | timestamp |

### Coleção `pedidos`
| Campo | Tipo |
|---|---|
| `solicitante` | string |
| `obra` | string |
| `obs` | string |
| `email`, `uid` | referência ao usuário |
| `itens` | array de `{prodId, nome, unidade, estoque, qtd, categoria}` |
| `status` | `pendente` \| `aprovado` \| `rejeitado` |
| `criadoEm`, `atualizadoEm` | timestamp |

### Coleção `usuarios` (hoje somente leitura pelo app)
| Campo | Tipo |
|---|---|
| `nome` | string |
| `papel` ou `funcao` | string — dois nomes de campo distintos coexistem hoje; consolidar em um só no schema-alvo (já capturado em `prd.md` §9) |

### Documento único `config/norm_ignorados`
| Campo | Tipo |
|---|---|
| `lista` | array de strings `"<prodId>|<campo>"` — decisões de "ignorar" persistidas da ferramenta de Inconsistências (FR-18) |

## G. Templates de Nomenclatura Guiada (28, por subtipo de material)

Referência completa para a implementação de FR-9 — hoje uma lista fixa de 28 strings-template no HTML, uma por subtipo de material, cada uma com placeholders `[ENTRE COLCHETES]` a preencher pelo usuário ao cadastrar:

- **Cabos — Elétrico:** `CABO [TIPO] [TENSÃO] Ø[SEÇÃO]MM² [COR] [COMPLEMENTO]`
- **Cabos — Rede:** `CABO REDE [BLINDA] [CAT] [NORMA] [COR]`
- **Cabos — Coaxial/Fibra/Especial:** `CABO [TIPO] [ESPECIF] [NORMA/HOMOL]`
- **Elétrica — Luminárias:** `LUMINÁRIA [TIPO FONTE] [APLICAÇÃO] [DIM] [POTÊNCIA] [TEMP.COR]`
- **Elétrica — Painéis/Quadros:** `PAINEL [OU QUADRO] ELÉTRICO [TENSÃO] [TIPO]`
- **Elétrica — Tomadas/Interruptores:** `TOMADA INDUSTRIAL [APLICAÇÃO] [POLOS] [CORRENTE]A [TENSÃO]V [IP] [COR]`
- **Elétrica — Refletores:** `REFLETOR LED [POTÊNCIA]W [TEMP.COR] [APLICAÇÃO]`
- **Elétrica — Abraçadeiras/Acess.:** `ABRAÇADEIRA [TIPO] [MATERIAL] [DIAM/BITOLA]`
- **Hidráulica — Conexões PVC:** `[PEÇA] PVC [CLASSE] DN[XX] [COR]`
- **Hidráulica — Válvulas/Registros:** `VÁLVULA [OU REGISTRO] [TIPO] DN[XX] [MATERIAL] [PRESSÃO]`
- **Hidráulica — Louças/Vasos:** `BACIA SANITÁRIA [TIPO] [MODELO/MARCA] [COR] [ESTADO]`
- **Hidráulica — Torneiras/Chuveiros:** `TORNEIRA [APLICAÇÃO] [DN/POL] [MATERIAL/ACAB]`
- **Hidráulica — Mangueiras/Incêndio:** `MANGUEIRA INCÊND. [DIAM] [COMP] [TIPO]`
- **Tubo — Aço Carbono:** `TUBO AÇO CARBONO [ACAB] [BITOLA] [COMP]`
- **Tubo — Aço Inox:** `TUBO INOX [NORMA/LIGA] Ø[XX]MM [COMP]`
- **Tubo — PVC Esgoto/Água:** `TUBO PVC [TIPO] DN[XX] [COR] NBR[XXXX]`
- **Tubo — PEAD/PPR:** `TUBO PEAD [PN] DN[XX]`
- **Perfil — Aço Estrutural:** `PERFIL [SEÇÃO W/I/U/Z/L] AÇO [MEDIDA H]X[BF]MM [COMP]`
- **Perfil — Alumínio:** `PERFIL ALUMÍNIO TIPO [H/U/T/Z/CART.] [MEDIDA] [APLICAÇÃO]`
- **Perfil — Cartola/Estrutural:** `PERFIL CARTOLA [MEDIDA H]X[LA]MM [COMP]`
- **Ferragem — Barras Roscadas:** `BARRA ROSCADA [MATERIAL/ACAB] [BITOLA] L=[XX]M`
- **Ferragem — Telas de Aço:** `TELA AÇO SOLDADA Q-[XXX] [NORMA] [LARG]X[COMP]M`
- **Ferragem — Chumbadores:** `CHUMBADOR [TIPO: J/CBA/EXP] [BITOLA] [COMP]`
- **Ferragem — Estruturas Metálicas:** `ESTRUTURA METÁLICA TUBULAR [APLICAÇÃO] [DIMENSÕES]`
- **Mat. Construção — Pisos/Porcel.:** `PORCELANATO [MARCA] [DIM]CM [TIPO/ACAB] [QDE PCJ/CX]`
- **Mat. Construção — Parafusos/Fix.:** `PARAFUSO [TIPO] [BITOLA]X[COMP]MM [MATERIAL/ACAB]`
- **Mat. Construção — Forro/Gesso:** `PLACA GESSO [TIPO: GRELHA/LISA/ST] [DIM]CM`
- **Telha/Calha/Rufo:** `TELHA [PERFIL: TRAP/ONDA] [MATERIAL: AC/AL] [COMP]X[LARG]M`

## H. Lista de categorias atual (fonte única a ser criada no schema-alvo)

`04.001` Materiais Civis · `04.002` Materiais Elétricos · `04.003` Materiais de Acabamentos/Cobertura · `04.004` Materiais de Instalações Especiais · `04.005` Materiais/Estruturas Metálicas · `04.006` Materiais Hidrossanitários · `04.007` Madeiramento · `05.001` EPI/EPC · `05.002` Medicina do Trabalho · `05.003` Fardamentos · `05.004` Programa de Segurança · `06.001` Materiais de Escritório · `06.002` Materiais de Limpeza · `07.001` Equipamentos/Máquinas Alugados · `07.002` Veículos Alugados · `08.001` Equipamentos/Máquinas Adquiridos · `08.002` Ferramentas Adquiridas (Imobilizado/Ativos) · `08.003` Veículos Adquiridos · `09.001` Peças/Materiais para Equipamentos/Veículos/Máquinas · `10.001` Ferramentas Adquiridas (Consumo) · `10.002` Ferramentas Alugadas · `11.001` Combustíveis e Lubrificantes · `12.001` Verbas, Licenças e Alvarás · `12.002` Impostos · `13.001` Equipamentos Esportivos e Recreativos
