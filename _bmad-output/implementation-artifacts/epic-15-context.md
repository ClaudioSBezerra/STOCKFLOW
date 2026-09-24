# Epic 15 Context: Acesso sem a Empresa na URL

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Hoje só se entra no stockflow pelo endereço da Empresa (`/e/{empresa}`); a raiz do domínio mostra apenas uma página explicativa. Este épico permite entrar pela raiz só com e-mail e senha: o sistema descobre a Empresa pela conta e leva a pessoa para dentro dela. Isso é possível porque o e-mail passa a ser único entre as Empresas reais, garantido no banco. O Ambiente de Treinamento pode repetir o e-mail da sua Empresa real; nesse caso o login pergunta "Ambiente real ou Treinamento?". Num servidor de um cliente só (`suprimentos.fcxlabs.com`), a raiz abre direto o login daquela Empresa.

## Stories

- Story 15.1: E-mail único entre as Empresas reais
- Story 15.2: Login na raiz do domínio pela conta
- Story 15.3: "Esqueci a senha" na raiz do domínio
- Story 15.4: Domínio de um cliente só abre direto a Empresa

## Requirements & Constraints

- **Unicidade de e-mail:** um e-mail tem no máximo uma conta somando todas as Empresas reais. Convite, autocadastro e cadastro do primeiro `adm` de Empresa nova recusam e-mail já usado em outra Empresa real (ou no Treinamento dela) com `409` e a mesma mensagem de "e-mail em uso" de hoje; nada é gravado (na criação de Empresa, nem Empresa nem Treinamento). O Treinamento pode repetir o e-mail de uma conta da **sua** Empresa real (inclusive o `adm` do Treinamento criado no provisionamento).
- **Não revelar quem tem conta:** e-mail inexistente e senha errada dão a mesma resposta (`401 INVALID_CREDENTIALS`) e levam o mesmo tempo (bcrypt roda também sem conta). A pergunta real/Treinamento só aparece depois de a senha conferir. "Esqueci a senha" na raiz responde sempre `202` com a mesma mensagem.
- **Mesmas regras do login por Empresa:** bloqueio por tentativas (aplica-se a cada conta daquele e-mail; `429 ACCOUNT_LOCKED`), conta desativada, e-mail não confirmado, Empresa desativada e MFA valem exatamente como sob `/e/{slug}`. A raiz só descobre a Empresa, nunca abre exceção.
- **Real + Treinamento:** senha confere nas duas → pergunta com o nome de cada Empresa; confere só numa → entra direto nela.
- **Esqueci a senha na raiz:** um e-mail de redefinição por conta ativa, cada um com o nome e o link `/e/{slug}/redefinir-senha?...` da sua Empresa; mesmos limites e prazos do pedido pelo endereço da Empresa.
- **Nada muda:** endereços `/e/{empresa}`, convites e login SSO continuam como antes. SSO continua só dentro do endereço da Empresa.
- **Fora de escopo:** trocar de Empresa sem sair, SSO na raiz, subdomínio por Empresa.

## Technical Decisions

- **Dado:** `usuarios.empresa_raiz_id UUID NOT NULL REFERENCES empresas(id)` = `COALESCE(empresas.empresa_origem_id, empresas.id)` da Empresa da conta. Preenchida por trigger `BEFORE INSERT` (conta nunca troca de Empresa, então sem tratamento de `UPDATE`), cobrindo todo `INSERT INTO usuarios` (cadastro, convite, primeiro `adm`, `adm` do Treinamento, CLIs de seed/migração) sem o código informá-la. Backfill na mesma migration.
- **Restrição no banco:** `CREATE EXTENSION IF NOT EXISTS btree_gist` + `EXCLUDE USING gist (lower(email) WITH =, empresa_raiz_id WITH <>)`. O índice único `(empresa_id, lower(email))` atual continua. Violação `23P01` → `409`. A migration falha antes de criar a restrição, listando os e-mails duplicados, se já houver duplicata entre Empresas reais (nada fica pela metade). Já foi verificado que o usuário da aplicação (sem superusuário) consegue criar a extensão e a restrição no Postgres 15/16.
- **Sem condicional de Treinamento:** nada de coluna booleana de Treinamento em `usuarios` nem `if treinamento` em service. A restrição de exclusão expressa a regra.
- **Rotas novas fora do prefixo `/e/{slug}` e sem `RequireEmpresa`** (as únicas exceções novas à resolução de Empresa pelo slug da URL): `POST /api/auth/entrar`, `POST /api/auth/entrar/escolha`, `POST /api/auth/esqueci-senha`, e o público `GET /api/entrada`. Nenhuma rota de domínio passa a aceitar `empresa_id` de body/query.
- **Entrar:** busca as contas do e-mail (normalizado) em Empresas **ativas** e chama o `services.Login(db, empresaID, email, senha)` atual para cada uma. Regras de bloqueio e contagem de falhas ficam num lugar só. Sem conta → `dummyBcryptHash`. Uma conta conferida → resposta com `slug`; o refresh token vai no cookie com `Path=/e/{slug}/api/auth` (o mesmo de `refreshTokenCookiePath`). Com MFA → `{slug, mfaRequerido, mfaToken}` e o código é verificado no `POST /e/{slug}/api/auth/mfa/verificar` atual. Duas contas → `200 {escolha: [{slug, nomeFantasia, treinamento}], escolhaToken}`.
- **escolhaToken:** em `tokens_acao`, novo tipo `escolha_empresa`, uso único, 5 min, preso aos ids das contas conferidas; `entrar/escolha {escolhaToken, slug}` só aceita slug de uma dessas contas. Consumo atômico, como os demais tokens tipados.
- **Esqueci a senha:** para cada conta ativa do e-mail, chama o `SolicitarRedefinicaoSenha` atual com a Empresa e o slug dela.
- **Domínio de cliente único:** variável de backend `EMPRESA_PADRAO` (slug). `GET /api/entrada` → `{empresaPadrao: slug | null}`, sem nenhum outro dado; `null` se ausente, vazia, inexistente ou Empresa desativada. Nunca definida em `stockflow.fbtechia.com`. Tem de entrar explicitamente na lista de variáveis do container `api` em `installer/cliente-aws/docker-compose.yml` (`EMPRESA_PADRAO=${EMPRESA_PADRAO}`) e ser documentada como opcional no `.env.example` e na documentação de implantação.
- Sem sessão "global" nova: cookie e sessão continuam por Empresa.

## UX & Interaction Patterns

- A app `sem-empresa` (`lib/entrada.ts`) deixa de ser a página explicativa e vira a tela de login pela conta: e-mail, senha, "Esqueci a senha" e, quando houver, a pergunta "Ambiente real ou Treinamento?" com o nome de cada Empresa. Vale para qualquer caminho fora de `/e/{empresa}` e `/plataforma`.
- Essa app não monta `AuthProvider`: só conversa com as rotas da raiz e redireciona. Depois do login, recarrega em `/e/{slug}/` e o `AuthProvider` restaura a sessão pelo refresh silencioso de sempre, sem a pessoa digitar de novo. Com MFA, a etapa do código acontece sob `/e/{slug}`.
- Com `EMPRESA_PADRAO` válida, a raiz faz `location.replace('/e/{slug}/')` e cai no login da Empresa (com SSO, se configurado). Caso contrário, mostra o login pela conta, nunca um erro.

## Cross-Story Dependencies

- 15.1 é pré-requisito de 15.2 e 15.3: o login e a redefinição pela raiz dependem de o e-mail identificar a Empresa real.
- 15.3 e o fallback de 15.4 usam a tela de login da raiz criada em 15.2.
- Reaproveita o que já existe: `services.Login`, bloqueio por tentativas, MFA condicional por Empresa (Epic 14), `SolicitarRedefinicaoSenha`, `tokens_acao` tipado e o provisionamento Empresa + Treinamento.
- Ação do operador depois do deploy de 15.4: acrescentar `EMPRESA_PADRAO=ferreira-costa` a `/opt/apps/stockflow/cliente-aws/.env` em `suprimentos.fcxlabs.com` e reiniciar o container da API. Até isso ser feito, a raiz mostra o login pela conta, que também funciona.
