---
stepsCompleted: [1, 1-confirmed, 2, 2-approved, 3, 4-validated, r2-1, r2-1-confirmed, r2-2, r2-2-approved, r2-3, r2-4-validated]
inputDocuments:
  - _bmad-output/planning-artifacts/prds/prd-stockflow-2026-08-29/prd.md
  - _bmad-output/planning-artifacts/prds/prd-stockflow-2026-08-29/addendum.md
  - _bmad-output/planning-artifacts/architecture/architecture-stockflow-2026-08-29/ARCHITECTURE-SPINE.md
  - _bmad-output/planning-artifacts/ux-designs/ux-stockflow-2026-08-29/DESIGN.md
  - _bmad-output/planning-artifacts/ux-designs/ux-stockflow-2026-08-29/EXPERIENCE.md
---

# stockflow - Epic Breakdown

## Overview

This document provides the complete epic and story breakdown for stockflow, decomposing the requirements from the PRD, UX Design, and Architecture into implementable stories.

## Requirements Inventory

### Functional Requirements

FR1: Usuário autentica com e-mail/senha para acessar qualquer funcionalidade. Sessão expira após 2h de inatividade; nenhum endpoint responde sem token válido, exceto login e cadastro.
FR2: Backend valida o papel do Usuário (`usuario < almoxarife < gestor < adm`) em cada endpoint sensível, nunca só na interface. Ação de `almoxarife`+ chamada por `usuario` retorna 403 mesmo via chamada direta à API.
FR3: Qualquer pessoa cria conta pública (nome, e-mail, senha), sempre como `usuario`. E-mail único garantido atomicamente; confirmação de e-mail obrigatória antes do primeiro login; backend nunca aceita papel diferente vindo do formulário.
FR4: Busca de Produto por nome/código/categoria com autocomplete (até 7 sugestões, ordenado por relevância).
FR5: Filtros de Produto por categoria, estoque e disponibilidade, combináveis simultaneamente.
FR6: Visualização do catálogo em grade e em tabela agrupada (soma quantidades de produtos com mesmo nome/unidade/dimensões).
FR7: Detalhe do Produto mostrando quantidade discriminada por Estoque.
FR8: Cadastro manual de Produto (nome, código, categoria, dimensões estruturadas valor+unidade, estoque destino, quantidade inicial, observações, foto opcional), restrito a `almoxarife`+.
FR9: Nomenclatura Guiada por subtipo de material (templates de nome), validada no servidor; edição de um Produto com template aplicado revalida o nome contra o template.
FR10: Importação em massa de Produtos via planilha padronizada; cria Estoques ausentes automaticamente; cabeçalho fora do padrão rejeita a importação inteira antes de processar qualquer linha.
FR11: Reimportação de planilha atualiza Produto existente por código em vez de sempre criar um novo; relatório final discrimina criados/atualizados/rejeitados.
FR12: Criar e listar locais de Estoque, com nome único garantido atomicamente.
FR13: Exclusão de Estoque bloqueada se houver quantidade residual ou Pedido `pendente` referenciando o Estoque.
FR14: Registrar Baixa (consumo) de estoque; rejeita quantidade zero ou negativa; restrito a `almoxarife`+.
FR15: Registrar Transferência entre Estoques; rejeita origem=destino e quantidade maior que a disponível; checagem e débito atômicos.
FR16: Histórico de Movimentações consultável (produto, tipo, origem, destino, quantidade, autor, data).
FR17: Detecção de inconsistências dimensionais em Produtos (dimensão em texto livre residual, valor sem unidade, etc.).
FR18: Aplicação seletiva de correções de Normalização (individual, em lote por produto, ou em lote geral); "ignorar" é permanente por valor específico, reaparecendo se o campo mudar para um novo valor inconsistente.
FR19: Detecção de Produtos duplicados por nome normalizado + dimensões equivalentes + locais coincidentes.
FR20: Mesclagem de duplicatas com trilha de auditoria permanente (quem, quando, produtos removidos, valores); item em Carrinho/Pedido pendente é redirecionado ao produto mantido.
FR21: Carrinho de reserva de itens antes de enviar um Pedido; valida disponibilidade no momento da adição.
FR22: Envio de Pedido de Retirada (solicitante, obra/centro de custo, observação) → status `pendente`; rejeita carrinho vazio; revalida disponibilidade no envio.
FR23: Consulta de Pedidos próprios, filtrável por status.
FR24: Consulta de todos os Pedidos por `almoxarife`+; um Usuário sem esse papel vê só os próprios Pedidos (escopo, não erro).
FR25: Aprovação/rejeição de Pedido com revalidação de estoque item a item; nunca sucesso parcial silencioso; débito e Movimentação atômicos.
FR26: Recibo do Pedido gerado como PDF no servidor (itens, quantidades, estoques de origem, solicitante, aprovador, data), sob demanda via endpoint dedicado.
FR27: Upload de foto de Produto (JPG/PNG/WEBP) com regra única de resolução (500px maior lado) e compressão (JPEG q=0.82), independente do fluxo.
FR28: Armazenamento de fotos de Produto fora do banco relacional (volume/serviço de objetos dedicado).
FR29: Galeria de fotos do Produto com visualização ampliada (lightbox) a partir do card ou do detalhe.
FR30: Exportação da tabela do catálogo para Excel (.xlsx) com totais e subtotais dinâmicos por grupo/filtro ativo.
FR31: Desativação e rebaixamento de conta de Usuário — `adm` age sobre qualquer conta, `gestor` só sobre `almoxarife`/`usuario`; conta desativada não autentica mais (senha ou SSO); rebaixamento perde acesso já na próxima requisição.
FR32: Recuperação de senha por e-mail (link/código de uso único, expira em prazo curto); mensagem genérica para e-mail inexistente; conta que só usava SSO pode criar senha própria pela primeira vez por este fluxo.
FR33: Solicitação de promoção de papel para o nível imediatamente acima; decidida por `gestor`/`adm` (promoção a `gestor` só por `adm`); decisão sempre registra quem decidiu e quando.
FR34: Login federado via Keycloak (SSO Ferreira Costa) como alternativa ao login por senha; nunca cria conta nova (busca por e-mail, case-insensitive); exige `email_verified=true`; papel do Usuário continua definido dentro do stockflow, não pelo Keycloak; login por senha continua sendo o caminho padrão visível, sem redirecionamento automático; RP-initiated logout ao encerrar sessão SSO.
FR35: Identificação de Produto via leitura de QR Code/código de barras (câmera do celular), reaproveitando o Código de Identificação já cadastrado (FR8); leitura abre o detalhe do Produto ou adiciona ao Carrinho conforme o contexto.
FR36: Bloqueio temporário de conta após tentativas de login malsucedidas consecutivas; política de senha mínima no cadastro/redefinição; não afeta o caminho de login via SSO.
FR37 *(revisado 2026-09-24)*: MFA (segundo fator TOTP) exigido de contas `gestor`/`adm` autenticadas por senha **somente quando a Empresa exige** (FR53); sem MFA a conta fica sem acesso ao sistema (servidor recusa as rotas restritas, interface só libera a configuração de segurança e o logout) até configurar; quem já tem MFA ligado sempre digita o código; login via SSO herda o MFA do realm Keycloak corporativo.
FR38: Log de acesso e auditoria (todo login, sucesso ou falha, com usuário quando identificável, timestamp, IP, método), append-only, consultável por `adm`.
FR39: Exportação dos próprios dados pessoais por qualquer Usuário; `adm` pode processar exclusão/anonimização de dados pessoais de uma conta, preservando o vínculo de Histórico/Pedidos já registrado.
FR40: Empresa como unidade de isolamento total de dados — todo Produto, Estoque, Movimentação, Pedido, Categoria, Log de Acesso e conta de Usuário pertence a exatamente uma Empresa; nenhuma consulta, filtro ou exportação de nenhuma área (Catálogo, Estoques, Movimentações, Pedidos, Log de Acesso, Normalização/Duplicatas, Gestão de Contas/Promoção) cruza Empresas, para nenhum papel. `adm` passa a ser único por Empresa, não mais global.
FR41: Papel "Dono da Plataforma" (ortogonal à hierarquia `usuario`–`adm` intra-Empresa) cria Empresas via tela própria e provisiona o primeiro `adm` de cada uma; cadastro de Empresa inclui CNPJ (único, validado), Razão Social, Nome Fantasia e endereço completo. MFA obrigatório para esse papel. Identidade nunca é a mesma conta/credencial de um `adm` de Empresa.
FR42: Vínculo de Usuário a uma Empresa via convite nominal (vinculado a um e-mail específico, uso único, com expiração) — nunca por domínio de e-mail nem link genérico reutilizável; papel da conta criada é sempre `usuario`. Login resolve a Empresa automaticamente pelo contexto de acesso, sem seletor manual.
FR43: Ambiente de Treinamento auto-provisionado junto com toda Empresa criada — Empresa-irmã isolada até da Empresa real que a originou, com dados de exemplo próprios e a mesma hierarquia de papéis, para prática sem risco de afetar dado real.
FR44: Migração da Ferreira Costa (dados/contas já em produção) para a primeira Empresa real do novo modelo, sem perda de histórico — migração aditiva/resumível, sem downtime obrigatório, com plano de rollback (diferente da migração legada anterior, que rodava contra um espelho dormente).

**Feedback de Treinamento pós Multi-Empresa (2026-09-19) — FR revisados:**

FR6 *(revisado)*: Visualização do catálogo em grade e em tabela agrupada, soma quantidades de produtos com mesmo nome/unidade/dimensões; toda listagem mostra explicitamente código, nome, categoria, estoque total (soma across Estoques) e embalagem+unidade como colunas próprias.
FR8 *(revisado)*: Cadastro manual de Produto — nome (mínimo 10 caracteres), código gerado automaticamente e sequencial (deixa de ser digitado), categoria, template de Nomenclatura (agora obrigatório), código do fornecedor e EAN-13, unidade de medida e embalagem, dimensões estruturadas, observações, foto opcional; Estoque destino e quantidade inicial SAEM deste formulário (passam a ser lançados separadamente, FR47); regras novas (nome mínimo, template obrigatório) valem só para cadastro novo/próxima edição, nunca retroativas.
FR9 *(revisado)*: Nomenclatura Guiada — seleção de template passa a ser OBRIGATÓRIA no cadastro; ganha um template "Genérico" (`[NOME LIVRE]`) sempre disponível como fallback para categorias sem template específico (as ~16/25 categorias fora do domínio de material de construção).
FR12 *(revisado)*: Criar e listar locais de Estoque, com nome único garantido atomicamente dentro da mesma Filial (não mais globalmente na Empresa).
FR14 *(revisado)*: Registrar Baixa (consumo) — debita contra o saldo DISPONÍVEL (descontada a reserva de FR50), nunca o saldo total; debita de um ou mais Lotes do Produto no Estoque (FEFO, ver Architecture AD-24).
FR15 *(revisado)*: Registrar Transferência entre Estoques — rejeita quantidade maior que a DISPONÍVEL (mesma correção de FR14); move Lote(s) inteiros ou parte de um Lote preservando a Data de Validade original, nunca cria Lote novo com validade diferente da origem.
FR21 *(revisado)*: Carrinho de reserva de itens antes de enviar um Pedido; valida disponibilidade no momento da adição — nota de terminologia: "reservar" aqui soma só com o próprio carrinho do usuário, não trava saldo contra outros usuários (isso só passa a acontecer no envio do Pedido, FR50).
FR22 *(revisado)*: Envio de Pedido de Retirada (solicitante, obra/centro de custo — FR52, observação) → status `pendente`; rejeita carrinho vazio; revalida disponibilidade no envio; passa a RESERVAR o saldo dos itens (FR50), deixando de ser só uma revalidação pontual.
FR25 *(revisado)*: Aprovação/rejeição de Pedido com revalidação de estoque item a item contra a RESERVA (não mais o saldo livre); nunca sucesso parcial silencioso; item aprovado debita do(s) Lote(s) do Estoque de origem (FEFO); débito e Movimentação atômicos.
FR40 *(revisado)*: Empresa como unidade de isolamento total de dados — assunção de cópia editável por Empresa estendida de Categorias também para Templates de Nomenclatura (FR49).
FR43 *(revisado)*: Ambiente de Treinamento auto-provisionado — conjunto de exemplo passa a incluir também fotos de exemplo nos Produtos (antes só Produtos/Estoques sem foto), para dar sensação de catálogo real.

**Feedback de Treinamento pós Multi-Empresa (2026-09-19) — FR novos:**

FR45: Código do Fornecedor (texto livre) e EAN-13 (13 dígitos + dígito verificador) no cadastro de Produto — dois campos adicionais, independentes entre si e do código interno (FR8); ambos opcionais, sem garantia de unicidade.
FR46: Unidade de Medida (enum, obrigatória para Produto novo) e Embalagem (texto livre, opcional) no cadastro de Produto; aparecem como colunas no Catálogo (FR6) e no detalhe por Estoque (FR7); Produto legado sem Unidade de Medida recebe valor de migração único em lote.
FR47: Lançamento de saldo inicial com Lote e Data de Validade, tela dedicada ao Almoxarife+ — um Produto pode ter vários Lotes ativos simultaneamente no mesmo Estoque; quantidade total exibida é a soma de todos os Lotes ativos; consumo é FEFO automático (Architecture AD-24); saldo legado migra como "Lote legado" com Data de Validade nula.
FR48: CRUD de Categorias pelo `adm`+ — código até 8 caracteres, nome/descrição até 50; cópia editável independente por Empresa; exclusão bloqueada se referenciada por algum Produto.
FR49: CRUD de Templates de Nomenclatura pelo `adm`+ — mesma cópia editável por Empresa de Categorias; editar um template em uso não força reedição retroativa dos Produtos já cadastrados; exclusão bloqueada se referenciado por algum Produto.
FR50: Reserva de saldo ao enviar Pedido — saldo dos itens fica reservado (indisponível para qualquer outro Pedido/Carrinho) até a decisão (FR25); Catálogo/Estoque passam a mostrar saldo disponível separado do reservado, com detalhe de para qual Pedido/solicitante; reserva liberada automaticamente na rejeição, parcialmente na aprovação parcial; sem expiração automática nesta versão.
FR51: Cadastro de Filiais pelo `adm`+ — toda Filial pertence a exatamente uma Empresa; todo Estoque passa a pertencer a exatamente uma Filial (Depósito = Estoque existente + Filial como pai, não um terceiro nível); toda Empresa nova ganha uma Filial padrão automática; migração vincula todo Estoque legado a essa Filial padrão.
FR52: Cadastro de Centro de Custo e Destino de Obra pelo `adm`+ — entidades próprias referenciáveis no envio de Pedido (FR22), coexistindo com o campo texto livre já existente (não o substituem nesta versão); mesma cópia por Empresa de FR40/FR48/FR49.
FR53: A Empresa decide se exige dupla autenticação — pergunta no cadastro pelo Dono da Plataforma (padrão "Não", confirmação explícita), Empresas existentes iniciam "Não", Treinamento herda na criação; o `adm` altera depois em Configurações → Segurança com auditoria e aviso de quantos `gestor`/`adm` ainda não têm MFA; ligar obriga gestor/adm sem MFA a cadastrar no próximo acesso; desligar não desliga MFA de ninguém; recuperação: `adm` reseta o MFA de um membro e cada conta desliga o próprio (senha + código), ambos auditados. Dono da Plataforma continua com MFA obrigatório.
FR42 (revisado): E-mail único entre as Empresas reais — convite, autocadastro e o cadastro do primeiro `adm` recusam um e-mail que já tem conta em outra Empresa real; o Ambiente de Treinamento pode repetir o e-mail de uma conta da sua Empresa real, nunca de outra.
FR54: Login na raiz do domínio, sem a Empresa na URL — e-mail e senha descobrem a Empresa pela conta; conta na Empresa real e no Treinamento com a mesma senha → pergunta "Ambiente real ou Treinamento?" só depois da senha conferida; sem revelar quem tem conta; mesmas regras do login de hoje (bloqueio, e-mail não confirmado, Empresa inativa, MFA); "Esqueci a senha" na raiz; domínio de um cliente só abre direto a Empresa por configuração; endereços `/e/{empresa}`, convites e SSO não mudam.
FR45 (revisado): EAN-13 único entre os Produtos ativos da Empresa — cadastro, edição e reativação recusam EAN já usado por outro Produto ativo, dizendo qual; inativo libera o EAN; duplicatas antigas não são alteradas e bloqueiam o salvamento até correção.
FR55: Inativar e reativar Produto — gestor/adm, qualquer motivo (opcional), só sem saldo nem reserva (a recusa diz onde há saldo); o inativo some de Catálogo, busca, leitura por código, exportação, Carrinho, duplicatas/inconsistências, lançamento de saldo e importação, e continua no histórico com a marca "Inativo"; filtro "Inativos" e reativar para gestor/adm.
FR56: Histórico de alterações do Produto — troca de nome (antes/depois), inativação e reativação registradas com quem e quando, visíveis no detalhe do Produto para almoxarife+.

### NonFunctional Requirements

NFR1: Toda autorização por papel é validada no servidor; nenhuma credencial (própria ou do Keycloak) é exposta no cliente/repositório; toda entrada é validada no limite da API.
NFR2: Proteção contra força bruta (FR36) e MFA para papéis administrativos (FR37) fazem parte da postura de segurança padrão do sistema.
NFR3: Log de acesso auditável e append-only (FR38).
NFR4: Conformidade com LGPD para dados pessoais de conta de Usuário — exportação e exclusão/anonimização sob solicitação (FR39).
NFR5: Observabilidade — ações hoje silenciosas no protótipo (skip de item sem estoque, erros de fundo) passam a ser logadas estruturadamente e comunicadas na interface.
NFR6: Confiabilidade — operações em lote não travam a UI nem deixam o catálogo em estado parcial sem indicação de progresso; importação interrompida permite saber quais linhas já foram gravadas e retomar sem duplicar.
NFR7: Concorrência — toda escrita dependente de estado lido previamente é atômica no servidor (saldo de estoque, unicidade de código/e-mail/nome de estoque, aprovação concorrente de Pedido).
NFR8: Desempenho — busca/listagem do catálogo ≤300ms p95 sob carga típica (até 8.000 produtos, 30 Estoques).
NFR9: Usabilidade em campo — interface responsiva funcional em viewport a partir de 360px de largura, testada em navegadores móveis padrão (Chrome Android, Safari iOS); não é opcional, é requisito central dado o uso predominante em campo pelas personas primárias.
NFR10: Isolamento multi-Empresa — toda consulta a dado operacional (Catálogo, Estoques, Movimentações, Pedidos, Log de Acesso, Normalização/Duplicatas, Gestão de Contas/Promoção) é automaticamente restrita à Empresa do ator autenticado, em toda camada onde o dado é lido ou escrito; nenhum papel, incluindo Dono da Plataforma, cruza Empresas para conteúdo operacional (FR40, FR41).

### Additional Requirements

- Paradigma de arquitetura: Layered Go pragmático (handlers → services → acesso a dados via `database/sql`), sem framework web e sem ORM — ratificado do projeto de referência `FB_APU02`, não o Hexagonal/Clean Architecture do PRD-fonte original (Architecture AD-1).
- Sem Redis nem RabbitMQ em nenhuma decisão de infraestrutura deste projeto (Architecture AD-2) — diverge deliberadamente do `FB_APU02` (que mantém Redis não utilizado) e do PRD-fonte original.
- Atualização em tempo real via broadcaster in-process + SSE, 4 canais fixos (`produtos`, `estoques`, `movimentacoes`, `pedidos`), envelope de evento fixo (`resource`/`id`/`change`), autenticação da conexão via ticket de curta duração (30s, uso único) obtido por endpoint autenticado normalmente (Architecture AD-3).
- E-mail transacional assíncrono via tabela de outbox no Postgres (`emails_pendentes`, schema fixo com `tipo` enum) + worker por polling — nunca chamada SMTP síncrona no handler HTTP (Architecture AD-4).
- Papel do Usuário sempre lido diretamente do Postgres a cada requisição autenticada, sem cache (Architecture AD-5) — garante que revogação/rebaixamento (FR31) valha já na próxima requisição.
- Modelo de sessão: JWT de acesso curto (30min, `golang-jwt/jwt/v5`) + refresh token rotativo em cookie `HttpOnly` com TTL de 2h (Architecture AD-6) — mesmo formato de token emitido tanto pelo login por senha (FR1) quanto pela troca de token do SSO (FR34).
- Integração Keycloak SSO: pacote `iam/` dedicado, JWKS cache em memória (TTL 1h), validação RS256 via `kid`, `iss`=URL do realm, `azp` (não `aud`) contra allowlist, endpoint de troca de token, endpoint de config runtime (não build-time) — replica o padrão já em produção no `FB_APU02` com desvios deliberados documentados (Architecture AD-7).
- Autorização por papel: hierarquia como ordem total codificada (`adm=4 > gestor=3 > almoxarife=2 > usuario=1`), decisão (papel mínimo/comparação relativa) sempre em middleware, filtro de escopo em listagem sempre em service (Architecture AD-8).
- Dimensões de Produto sempre `{valor, unidade}` estruturado, nunca string livre, em todo o schema/validação/normalização (Architecture AD-9).
- Concorrência de estoque: `SELECT ... FOR UPDATE` em toda escrita de `produto_estoque.quantidade`, sem exceção — toda escrita gera uma Movimentação correspondente; ordem canônica de lock `(produto_id, estoque_id)` ascendente, aplicada ao lote inteiro antes de adquirir qualquer lock (Architecture AD-10).
- Fotos de Produto em volume Docker nomeado e persistente, nome de arquivo versionado (`<produto_id>-<timestamp_unix>.jpg`, nunca overwrite em path fixo); soft-delete via `deleted_at IS NULL`; mesclagem de duplicatas (FR20) reescreve `produto_id` em `MOVIMENTACOES`/`PEDIDO_ITENS` do produto removido para o sobrevivente antes do soft-delete (Architecture AD-11).
- Bootstrap do primeiro Adm via comando CLI dedicado (`cmd/seed-admin`), nunca endpoint HTTP (Architecture AD-12).
- Topologia de deployment: Docker Compose single-host, padrão `installer/cliente-aws` do `FB_APU02` — serviços `api` (Go), `web` (React+Nginx), `db` (`postgres:15-alpine`); sem Redis/RabbitMQ; volume nomeado persistente para fotos (Architecture AD-13).
- Convenções: tabelas/colunas do schema em português (nomes já estabelecidos pelo protótipo/PRD); UUID v4 em toda tabela nova; `timestamptz` UTC; e-mail sempre normalizado para minúsculas com índice único sobre o valor normalizado; envelope de erro HTTP `{"error":{"code","message"}}` com vocabulário fixo de `code` para autenticação/sessão; logging via `log/slog` estruturado (Architecture AD-14).
- Migração de dados legados: script one-off (`cmd/migrate-legado`, fora do runtime), lê diretamente do PostgreSQL espelho do Firestore mantido pela empresa; converte dimensões texto-livre; gera UUIDs novos com tabela de mapeamento id-antigo→id-novo; popula `NOMENCLATURA_TEMPLATES` (28 seeds) e `CATEGORIAS` (25 seeds); corte único, sempre disparado por humano, nunca por agente autônomo (Architecture AD-15).
- Envelope operacional: ambientes local (Compose) + produção (servidor dedicado Ferreira Costa); segredos via `.env` não versionado; backup `pg_dump` diário; observabilidade via Prometheus + Grafana; CI/CD via GitHub Actions no padrão do `FB_APU02` (Architecture AD-16).
- Recibo PDF (FR26) sempre renderizado a partir do snapshot já capturado em `PEDIDO_ITENS` no momento da decisão do Pedido — nunca um join ao vivo com `PRODUTOS` (Architecture AD-17).
- `TOKENS_ACAO` (verificação de e-mail FR3, redefinição de senha FR32) tipado por coluna `tipo` (enum) e de uso único — validação sempre filtra por token+usuario_id+tipo+não expirado+não usado (Architecture AD-18).
- Stack pinado nesta rodada (Architecture): Go 1.27, PostgreSQL 15, React 19.2.x, TypeScript 7.0.x, Vite 8.0.x, React Router 6.x, TanStack Query 5.x, shadcn/ui + Tailwind CSS, `golang-jwt/jwt` v5, `signintech/gopdf` (FR26), `qax-os/excelize` v2.11.0 (FR30) — Go/React/Vite/TypeScript atualizados deliberadamente além das versões do `FB_APU02` por estarem sem suporte de segurança ou muitas majors atrás.
- Biblioteca TOTP para MFA (FR37) e endereço/DNS de deploy em Ferreira Costa permanecem em aberto (Architecture Deferred) — a resolver durante a implementação das stories correspondentes, não bloqueiam a criação dos épicos/stories.
- Empresa resolvida no middleware por slug de URL (path prefix, ex. `/e/ferreira-costa/...`), injetada no contexto da requisição junto com o papel, nunca re-derivada em service nem aceita de body/query — divergência deliberada de subdomínio (evita DNS/TLS wildcard novo) (Architecture AD-19).
- Isolamento por `empresa_id` (UUID, FK) em toda tabela de domínio; toda query de service filtra por ele. Migração da Ferreira Costa é aditiva em duas fases (coluna nullable + backfill em lote resumível, só depois `NOT NULL`) — nunca `ALTER` direto sobre produção viva; `adm` único por `empresa_id`, não mais global (Architecture AD-20).
- "Dono da Plataforma" em tabela própria (`donos_plataforma`), disjunta de `usuarios` — nunca a mesma credencial de um `adm` de Empresa; rota de login própria; bootstrap do primeiro registro por CLI, mesmo padrão de AD-12; reaproveita o formato de token de AD-6 (Architecture AD-21).
- Convite de acesso em tabela própria (`convites_empresa`) — nominal (e-mail específico), uso único, expira; mesmo espírito de AD-18 mas tabela separada por não haver `usuario_id` ainda no momento da criação (Architecture AD-22).
- Ambiente de Treinamento é uma Empresa comum com `empresa_origem_id` nullable — reusa inteiramente o isolamento de AD-20, sem nenhuma condicional `if eh_treinamento` em service (Architecture AD-23).

**Feedback de Treinamento pós Multi-Empresa (2026-09-19):**

- Consumo de Lote é FEFO automático (`ORDER BY data_validade NULLS LAST, criado_em ASC`), nunca escolha manual — nova tabela `lotes` SUBSTITUI `produto_estoque` (uma linha por Lote, quantidade do par Produto/Estoque é a soma); Lote legado migra com `data_validade = NULL`, herdando `empresa_id` direto da linha de origem (Architecture AD-24).
- Saldo disponível é sempre calculado em query (soma `lotes.quantidade` menos reserva ativa), nunca coluna materializada; nova tabela `reservas_pedido_item`; reserva nasce no envio do Pedido, só é liberada por decisão explícita, sem expiração automática nesta versão; criar uma reserva participa do mesmo lock `SELECT ... FOR UPDATE` ordenado de AD-10, mesmo sem escrever em `lotes.quantidade` (Architecture AD-25, estende AD-10).
- Código sequencial de Produto via contador atômico por Empresa (`contadores_produto`, PK `empresa_id`, `UPDATE ... RETURNING` na mesma transação do INSERT), zero-padding 6 dígitos; primeira linha nasce na mesma transação de `ProvisionarEmpresa` que cria a Filial padrão, nunca lazy-init (Architecture AD-26).
- Nova tabela `filiais`; `estoques.filial_id` migração aditiva (nullable → backfill de Filial padrão → NOT NULL); índice único de nome de Estoque muda de `(empresa_id, nome)` para `(filial_id, nome)`; toda Empresa nova ganha Filial padrão automática na mesma transação de provisionamento (Architecture AD-27).
- Nova tabela `centros_custo`; `pedidos.centro_custo_id` opcional coexiste com `pedidos.obra_centro_custo` (texto livre, inalterado) — sem migração retroativa (Architecture AD-28).
- Produto sem nenhuma linha em `lotes` aparece no Catálogo com quantidade total 0 — nenhum estado de visibilidade novo (Architecture AD-29).
- Importação em massa (FR10) gera Lote com `data_validade = NULL` para toda linha que cria/atualiza saldo — mesmo tratamento do saldo legado migrado (Architecture AD-30).
- Exclusão de Estoque (FR13) bloqueada se existir qualquer linha em `lotes` para esse Estoque (mesmo com quantidade zero) OU reserva ativa referenciando um Lote dele; Lote a quantidade zero nunca é apagado automaticamente, mesmo espírito de preservação de histórico de AD-11 (Architecture AD-31, `[ASSUMPTION]` a confirmar).
- `produtos.codigo_fornecedor` (texto livre) e `produtos.ean13` (`CHAR(13)`, validado por dígito verificador) opcionais, sem unicidade; `produtos.unidade_medida` (enum) obrigatória só para Produto novo com migração aditiva (backfill `[ASSUMPTION]` "un" para dado legado); `produtos.embalagem` opcional (Architecture AD-32).
- CRUD de Categorias/Templates: exclusão bloqueada se referenciado por Produto (mesmo princípio de AD-31); `categorias.codigo` até 8 chars, nome/descrição até 50; editar Template em uso não revalida retroativamente Produtos já cadastrados (Architecture AD-33).
- Template "Genérico" (`[NOME LIVRE]`) é caso especial no motor de validação — aceita qualquer texto sem checagem de tokens, distinto da validação estrutural dos demais 27 templates; sempre disponível, nunca removível via CRUD (Architecture AD-34).
- Mesclagem de duplicatas (FR20/AD-11) estendida para reescrever `produto_id` também em `LOTES` e `RESERVAS_PEDIDO_ITEM` do produto removido, além de `MOVIMENTACOES`/`PEDIDO_ITENS` já cobertos — sem isso, saldo físico e reserva ativa do produto removido ficariam órfãos.
- Canais SSE (AD-3) seguem fixos em 4 (`produtos`, `estoques`, `movimentacoes`, `pedidos`); Categorias/Templates/Filiais/Centros de Custo são config administrativa de baixa frequência, explicitamente exemptas do gerador "todo domínio com handler tem canal" — sem canal SSE dedicado, cliente rebusca via GET normal após CRUD.
- Exigência de MFA é propriedade da Empresa: `empresas.mfa_obrigatorio` (default `false`, novas e existentes); gate único em `RequireRole` (rank >= gestor, origem senha, sem MFA **e** Empresa exige), Empresa lida por requisição (AD-19/AD-5, sem cache); `mfa_habilitado` da conta independe do flag; `/api/auth/me` devolve `empresa.mfaObrigatorio` para o frontend espelhar; alteração pelo `adm` e reset/desligamento de MFA gravam `auditoria_seguranca` (append-only); Treinamento herda só na criação; Dono da Plataforma não é afetado (Architecture AD-35).
- `filial_id`/`centro_custo_id` recebidos do cliente são sempre revalidados contra a Empresa do contexto antes de aceitar — FK do banco sozinha não impede um id de outra Empresa (extensão de AD-20).

### UX Design Requirements

UX-DR1: Herdar por completo os tokens de design shadcn/ui + Tailwind do `FB_APU02` — cores (`primary` #E62019, `accent`/`success` #16A249, `destructive` #EF4343, `warning` #F59F0A, `info` #0DA2E7, `background`/`foreground`/`card`/`secondary`/`border` inalterados), tipografia (Inter para interface geral, JetBrains Mono para todo código/SKU/UUID/valor de QR lido), escala de radius (sm 8px/md 10px/lg-DEFAULT 12px/full 9999px).
UX-DR2: Definir 4 tokens de cor `text-on-tint-*` mais escuros (success #166534, warning #92400E, info #075985, destructive #B91C1C) usados exclusivamente como texto de badge sobre fundo tintado a 10% da mesma cor, para atingir contraste WCAG AA (corrige falha medida de 1.97–3.19:1 herdada do `FB_APU02`).
UX-DR3: Definir tokens de espaçamento: rail 56px, bottom nav 56px, FAB 56px de diâmetro com margem de 16px e offset de 72px sobre a bottom nav em mobile, submenu vertical 224px, alvo de toque mínimo 48px.
UX-DR4: Componente `fab-scanner` — botão circular flutuante (cor `primary`, radius `full`, `shadow-lg`), presente só nas superfícies de Catálogo e Carrinho, nunca em telas administrativas.
UX-DR5: Componente `cart-badge` — contador circular sobre o ícone de Carrinho, preenchimento sólido `destructive`, desaparece por completo quando o Carrinho está vazio (nunca mostra "0").
UX-DR6: Componentes de badge de status (`status-pendente`, `status-aprovado`, `status-rejeitado`, `status-disponivel`) — formato pill, sempre ícone + texto (nunca só cor), texto sempre na variante `text-on-tint-*` correspondente.
UX-DR7: Componente `nav-item-active` — fundo `primary` a 10% de opacidade, texto/ícone `primary` sólido, idêntico ao padrão já em produção no `FB_APU02`.
UX-DR8: Adotar `AlertDialog` (shadcn) para toda ação destrutiva/irreversível (rejeitar Pedido, mesclar/excluir Produto, desativar conta) — correção deliberada do anti-padrão `window.confirm()` nativo identificado no `FB_APU02`.
UX-DR9: Padronizar 100% no vocabulário shadcn moderno (`Card`, `Table`, `Skeleton`, `Toast`) desde a primeira tela — não repetir a mistura de estilos ("shadcn moderno" vs. "Tailwind cru") encontrada em páginas mais antigas do `FB_APU02`.
UX-DR10: Todo badge de status/disponibilidade carrega sempre ícone + texto, nunca só cor.
UX-DR11: Toda confirmação de ação assíncrona (adicionar/remover do Carrinho, aprovar/rejeitar Pedido, importação concluída, foto enviada, não só atualização em tempo real) usa toast com `aria-live="polite"`.
UX-DR12: Alvo de toque mínimo de 48px em todo elemento interativo do fluxo de campo — acima do piso WCAG de 44px, calibrado para uso com luvas ou uma mão só.
UX-DR13: Busca por texto/Código de Identificação digitado ou colado manualmente sempre disponível como alternativa ao scanner de QR Code/código de barras — o scanner nunca é dependência obrigatória de nenhum fluxo (cobre falha de câmera, permissão negada, reflexo de sol).
UX-DR14: Viewport mínimo de 360px — nenhuma tela do fluxo principal (busca, carrinho, aprovação, leitura de QR) pode quebrar layout abaixo disso.
UX-DR15: Shell desktop/tablet (≥768px): rail de ícones fixo (56px) + header fino + barra de abas horizontal por módulo, ou submenu vertical (224px) para módulos densos (Estoques, Normalização) — espelha o `FB_APU02`.
UX-DR16: Shell mobile (<768px, a partir de 360px): rail vira bottom nav (56px) com Catálogo/Carrinho/Pedidos/Mais; Estoques/Normalização/Relatórios/Configurações completas ficam atrás do item "Mais" (`Sheet`).
UX-DR17: Indicador de atualização em tempo real — toast discreto (`aria-live="polite"`) na chegada de evento SSE em qualquer um dos 4 canais, nunca recarrega a tela sozinho.
UX-DR18: Indicador de reconexão SSE lenta — indicador discreto e persistente ("Reconectando...") se a reconexão levar mais que alguns segundos; reconexão rápida permanece silenciosa.
UX-DR19: Padrão de linha de aprovação item-a-item na Fila de Pedidos — mostra "Solicitado: X · Disponível: Y" em caso de divergência; nunca um botão único "Aprovar tudo" que esconde a divergência.
UX-DR20: Padrão de relatório de importação — tabela criados/atualizados/rejeitados + CTA "Verificar duplicatas agora" com deep-link para Normalização já com a análise disparada.
UX-DR21: Câmera do scanner exige contexto seguro (HTTPS) — funcionalidade indisponível em ambiente de desenvolvimento local sem HTTPS.
UX-DR22: Tela de configuração de MFA (TOTP) — forçada e não pulável para `gestor`/`adm` autenticados por senha; opcional para `usuario`/`almoxarife`; nunca exibida no caminho de login via SSO.
UX-DR23: Padrão de galeria/lightbox de fotos de Produto — toque abre lightbox em tela cheia, fechar retorna à posição exata da rolagem anterior; upload restrito a `almoxarife`+, visualização liberada a `usuario`.

### FR Coverage Map

FR1: Epic 1 - Login por e-mail e senha
FR2: Epic 1 - Autorização por papel aplicada no servidor
FR3: Epic 1 - Autocadastro público, sempre como usuario
FR4: Epic 4 - Busca por nome/código/categoria com sugestões
FR5: Epic 4 - Filtros por categoria, estoque e disponibilidade
FR6: Epic 4 - Visualização em grade e tabela agrupada
FR7: Epic 4 - Detalhe do produto por local de estoque
FR8: Epic 3 - Cadastro manual de Produto
FR9: Epic 3 - Nomenclatura Guiada por subtipo
FR10: Epic 3 - Importação em massa via planilha padronizada
FR11: Epic 3 - Importação atualiza por código, não só cria
FR12: Epic 2 - Criar e listar locais de Estoque
FR13: Epic 2 - Exclusão de Estoque trata resíduos e pedidos pendentes
FR14: Epic 5 - Registrar Baixa (consumo)
FR15: Epic 5 - Registrar Transferência entre Estoques
FR16: Epic 5 - Histórico de Movimentações consultável
FR17: Epic 6 - Detecção de inconsistências dimensionais
FR18: Epic 6 - Aplicação seletiva de correções
FR19: Epic 6 - Detecção de duplicatas
FR20: Epic 6 - Mesclagem de duplicatas com trilha de auditoria
FR21: Epic 7 - Carrinho de reserva
FR22: Epic 7 - Envio de Pedido
FR23: Epic 7 - Consulta de Pedidos próprios
FR24: Epic 7 - Consulta de todos os Pedidos (Almoxarife+)
FR25: Epic 7 - Aprovação/rejeição com revalidação de estoque item a item
FR26: Epic 7 - Recibo do Pedido em PDF gerado pelo servidor
FR27: Epic 3 - Upload de foto com regra única de tamanho/compressão
FR28: Epic 3 - Armazenamento de fotos fora do banco relacional
FR29: Epic 3 - Galeria e visualização ampliada (lightbox)
FR30: Epic 4 - Exportação da tabela do catálogo para Excel
FR31: Epic 1 - Desativação e rebaixamento de conta
FR32: Epic 1 - Recuperação de senha por e-mail
FR33: Epic 1 - Solicitação de promoção de papel
FR34: Epic 1 - Login federado via Keycloak (SSO Ferreira Costa)
FR35: Epic 4 - Identificação de Produto via QR Code / código de barras
FR36: Epic 1 - Bloqueio de conta e política de senha
FR37: Epic 1 - MFA obrigatório para papéis administrativos
FR38: Epic 1 - Log de acesso e auditoria
FR39: Epic 8 - Exportação e exclusão de dados pessoais (LGPD)
FR40: Epic 9 - Empresa como unidade de isolamento total de dados
FR41: Epic 9 - Papel "Dono da Plataforma" cria e gerencia Empresas
FR42: Epic 9 - Vínculo de Usuário a uma Empresa via convite nominal
FR43: Epic 9 - Ambiente de Treinamento automático por Empresa
FR44: Epic 9 - Migração da Ferreira Costa para o modelo multi-Empresa
FR6 (revisado): Epic 10 - Colunas explícitas (código, categoria, estoque total, embalagem+unidade) na listagem
FR8 (revisado): Epic 10 - Nome mínimo 10 chars, código automático, estoque/quantidade saem do cadastro
FR9 (revisado): Epic 10 - Template de Nomenclatura obrigatório + fallback Genérico
FR40 (revisado): Epic 10 - Cópia editável por Empresa estendida a Templates de Nomenclatura
FR45: Epic 10 - Código do Fornecedor e EAN-13 no cadastro de Produto
FR46: Epic 10 - Unidade de Medida e Embalagem no cadastro de Produto
FR48: Epic 10 - CRUD de Categorias
FR49: Epic 10 - CRUD de Templates de Nomenclatura
FR14 (revisado): Epic 11 - Baixa debita saldo disponível e consome Lote (FEFO)
FR15 (revisado): Epic 11 - Transferência debita saldo disponível e move Lote preservando validade
FR21 (revisado): Epic 11 - Terminologia de Carrinho/Reservar distinta da reserva dura
FR22 (revisado): Epic 11 - Envio de Pedido passa a reservar saldo
FR25 (revisado): Epic 11 - Aprovação revalida contra reserva e debita Lote (FEFO)
FR47: Epic 11 - Lançamento de saldo inicial com Lote e Data de Validade
FR50: Epic 11 - Reserva de saldo ao enviar Pedido
FR12 (revisado): Epic 12 - Nome de Estoque único dentro da mesma Filial
FR43 (revisado): Epic 12 - Ambiente de Treinamento ganha fotos de exemplo
FR51: Epic 12 - Cadastro de Filiais
FR52: Epic 12 - Cadastro de Centro de Custo e Destino de Obra
FR8 (edição): Epic 13 - Editar um Produto já cadastrado (o PRD prevê edição em FR8/FR9; nenhuma tela existia)
FR37 (revisado): Epic 14 - MFA exigido só quando a Empresa exige
FR53: Epic 14 - A Empresa decide se exige dupla autenticação
FR42 (revisado): Epic 15 - E-mail único entre as Empresas reais
FR54: Epic 15 - Login na raiz do domínio, sem a Empresa na URL
FR3 e FR40 (uma linha cada): Epic 15 - unicidade de e-mail e login pela conta
FR55: Epic 16 - Inativar e reativar Produto
FR56: Epic 16 - Histórico de alterações do Produto
FR45 (revisado): Epic 16 - EAN-13 único entre Produtos ativos
FR41 (linha nova): Epic 14 - Cadastro da Empresa pergunta se exige MFA
FR43 (linha nova): Epic 14 - Treinamento herda a escolha

## Epic List

### Epic 1: Autenticação e Gestão de Acesso
Qualquer pessoa cria conta, entra com segurança (senha ou SSO corporativo Ferreira Costa), a organização controla quem acessa o quê, e contas administrativas ficam protegidas por segundo fator.
**FRs covered:** FR1, FR2, FR3, FR31, FR32, FR33, FR34, FR36, FR37, FR38

### Epic 2: Gestão de Estoques
Almoxarife organiza os locais físicos de estoque com integridade referencial.
**FRs covered:** FR12, FR13

### Epic 3: Cadastro, Importação e Fotos de Produtos
Almoxarife popula e mantém o catálogo — manual, em lote via planilha, ou com fotos.
**FRs covered:** FR8, FR9, FR10, FR11, FR27, FR28, FR29

### Epic 4: Catálogo — Consulta, Descoberta e Exportação
Qualquer Usuário encontra material disponível, vê onde está (inclusive via QR Code/código de barras), e exporta o catálogo.
**FRs covered:** FR4, FR5, FR6, FR7, FR30, FR35

### Epic 5: Movimentação de Estoque
Almoxarife registra saída e transferência de estoque com histórico consultável.
**FRs covered:** FR14, FR15, FR16

### Epic 6: Normalização de Dados
Almoxarife mantém o catálogo limpo (inconsistências e duplicatas) sem trabalho manual item a item.
**FRs covered:** FR17, FR18, FR19, FR20

### Epic 7: Pedidos de Retirada
Usuário solicita, almoxarife aprova com estoque real, recibo em PDF — ciclo completo.
**FRs covered:** FR21, FR22, FR23, FR24, FR25, FR26

### Epic 8: Privacidade e Conformidade (LGPD)
Usuário exporta os próprios dados pessoais; Adm processa solicitações de exclusão/anonimização — cobrindo identidade, log de acesso, Movimentações e Pedidos já existentes nos épicos anteriores.
**FRs covered:** FR39

### Epic 9: Multi-Empresa e Plataforma
O stockflow deixa de ser uma instalação única (Ferreira Costa) para ser uma plataforma multi-cliente com isolamento total de dados entre Empresas; Dono da Plataforma cria Empresas (com Ambiente de Treinamento automático); Usuário se vincula a uma Empresa via convite nominal; a Ferreira Costa migra para a primeira Empresa real do novo modelo.
**FRs covered:** FR40, FR41, FR42, FR43, FR44

### Epic 10: Enriquecimento do Cadastro de Produto e Configuração de Catálogo
Almoxarife cadastra Produtos mais completos e consistentes (nome mínimo, template de Nomenclatura obrigatório com fallback Genérico, Código do Fornecedor e EAN-13, Unidade de Medida e Embalagem, colunas explícitas na listagem); Adm mantém Categorias e Templates de Nomenclatura próprios por Empresa, sem depender de seed fixo.
**FRs covered:** FR6, FR8, FR9, FR40, FR45, FR46, FR48, FR49

### Epic 11: Estoque Preciso — Lote, Validade e Reserva de Saldo
Almoxarife lança e consome saldo por Lote com Data de Validade (FEFO automático, sem escolha manual); enviar um Pedido passa a travar de verdade o saldo reservado até a decisão do almoxarife, fechando a corrida entre pedidos concorrentes que hoje não existe.
**FRs covered:** FR14, FR15, FR21, FR22, FR25, FR47, FR50

### Epic 12: Estrutura Organizacional — Filiais e Centro de Custo
Adm estrutura a organização em Filiais (com Depósitos/Estoques vinculados) e cadastra Centro de Custo/Destino de Obra, referenciável no envio de Pedido; Ambiente de Treinamento ganha fotos de exemplo nos Produtos.
**FRs covered:** FR12, FR43, FR51, FR52

### Epic 13: Edição de Produto
Almoxarife corrige um Produto já cadastrado sem precisar cadastrar outro. Surgiu dos testes reais de treinamento (2026-09-23): a única rota de edição (`/renomear`) nunca teve tela, e nenhum épico anterior previu uma.
**FRs covered:** FR8 (edição), FR9 (revalidação do nome na edição)

### Epic 14: Dupla autenticação por Empresa
Cada Empresa decide se exige dupla autenticação (MFA) de seus `gestor`/`adm`, em vez de a exigência ser fixa para todos; o Dono da Plataforma responde à pergunta no cadastro, o `adm` altera depois, e existe saída para quem perde o celular. Pedido dos sócios e de clientes (2026-09-24).
**FRs covered:** FR37 (revisado), FR53, FR41 e FR43 (uma linha cada)

### Epic 15: Acesso sem a Empresa na URL
Quem abre o stockflow sem `/e/{empresa}` entra só com e-mail e senha, e o sistema descobre a Empresa pela conta; o domínio de um cliente só abre direto a Empresa dele. Para isso, o e-mail passa a ser único entre as Empresas reais. Pedido do usuário (2026-09-24): `stockflow.fbtechia.com` é a plataforma comercial com várias Empresas, `suprimentos.fcxlabs.com` é o servidor de um cliente só.
**FRs covered:** FR54, FR42 (revisado), FR3 e FR40 (uma linha cada)

### Epic 16: Inativar Produto, EAN único e histórico do Produto
Gestor e adm tiram de uso um Produto sem apagá-lo (só sem saldo) e o reativam depois; o EAN-13 deixa de poder repetir entre Produtos ativos; e toda troca de nome, inativação e reativação fica registrada. Feedback do treinamento da Ferreira Costa (Karla, 2026-09-25).
**FRs covered:** FR55, FR56, FR45 (revisado)

## Epic 1: Autenticação e Gestão de Acesso

Qualquer pessoa cria conta, entra com segurança (senha ou SSO corporativo Ferreira Costa), a organização controla quem acessa o quê, e contas administrativas ficam protegidas por segundo fator.

### Story 1.1: Bootstrap do primeiro Adm e fundação do backend

As a operador de infraestrutura,
I want provisionar o primeiro usuário Adm via linha de comando,
So that o sistema tenha um ponto de entrada seguro sem depender de um endpoint HTTP de auto-promoção.

**Acceptance Criteria:**

**Given** a tabela `usuarios` inexistente
**When** as migrations SQL são aplicadas no startup da aplicação
**Then** a tabela `usuarios` é criada com colunas `id` (UUID v4), `nome`, `email` (único, normalizado para minúsculas — AD-14), `senha_hash` (nullable, para contas só-SSO), `papel` (enum `usuario`/`almoxarife`/`gestor`/`adm`), `email_verificado` (bool), `ativo` (bool), `criado_em`

**Given** o banco de dados com o schema migrado e nenhum Adm ainda cadastrado
**When** o operador executa `cmd/seed-admin` (AD-12) informando nome, e-mail e senha
**Then** uma conta é criada com papel `adm`, senha hasheada (bcrypt), e-mail normalizado
**And** não existe nenhum endpoint HTTP equivalente a este comando

**Given** já existe uma conta com papel `adm`
**When** o operador tenta rodar `cmd/seed-admin` novamente
**Then** o comando falha com uma mensagem clara, sem alterar a conta existente

### Story 1.2: Fundação do shell de navegação e design tokens

As a qualquer Usuário autenticado,
I want uma interface consistente com a identidade visual da Ferreira Costa, responsiva entre desktop e celular,
So that eu reconheça o produto como parte da mesma família de ferramentas internas e consiga usá-lo em campo.

**Acceptance Criteria:**

**Given** o frontend React inicializado com Vite + TypeScript (Architecture Stack)
**When** os tokens do `DESIGN.md` são aplicados via configuração Tailwind/shadcn
**Then** a paleta `primary`/`accent`/`destructive`/`warning`/`info` e as variantes `text-on-tint-*` ficam disponíveis (UX-DR1, UX-DR2), com Inter como fonte padrão, JetBrains Mono disponível para código/identificador, e os tokens de espaçamento (rail 56px, bottom nav 56px, FAB 56px, submenu 224px, alvo de toque 48px) definidos globalmente (UX-DR3)

**Given** um usuário autenticado em viewport ≥768px
**When** ele acessa qualquer tela do shell
**Then** vê o rail de ícones fixo (56px) + header fino + barra de abas horizontal ou submenu vertical de 224px conforme o módulo, com tooltip ao passar o mouse sobre um ícone (UX-DR15)
**And** o item de navegação correspondente à tela atual usa o estilo `nav-item-active` (fundo `primary` a 10% de opacidade, texto/ícone `primary` sólido — UX-DR7)

**Given** um usuário autenticado em viewport <768px (a partir de 360px)
**When** ele acessa qualquer tela do shell
**Then** vê a bottom nav (56px) com os itens visíveis, com itens administrativos atrás do item "Mais" (`Sheet`) (UX-DR16), e nenhuma tela do fluxo principal quebra o layout abaixo de 360px (UX-DR14)

**Given** a necessidade de confirmar qualquer ação destrutiva em stories futuras
**When** este shell é finalizado
**Then** um componente `ConfirmDialog` reutilizável (usando `AlertDialog` do shadcn, nunca `window.confirm()`) e o `Toaster` global (`sonner`, `aria-live="polite"`) já estão disponíveis para qualquer story consumir — padronizando 100% no vocabulário shadcn moderno (`Card`, `Table`, `Skeleton`, `Toast`, `AlertDialog`) desde a primeira tela, sem repetir a mistura de estilos do `FB_APU02` (UX-DR8, UX-DR9, UX-DR11)

**Given** qualquer elemento interativo do fluxo de campo (botões, itens de lista, controles de formulário)
**When** ele é implementado em qualquer story futura
**Then** o alvo de toque mínimo é 48px (`{spacing.touch-target-min}`), acima do piso WCAG de 44px, calibrado para uso com luvas ou uma mão só (UX-DR12) — regra global estabelecida por esta story, aplicada por toda story subsequente

### Story 1.3: Autocadastro com verificação de e-mail

As a colaborador sem conta no stockflow,
I want criar minha própria conta informando nome, e-mail e senha,
So that eu possa acessar o catálogo como `usuario` assim que confirmar meu e-mail.

**Acceptance Criteria:**

**Given** a tela pública de Autocadastro
**When** o usuário informa nome, e-mail e senha e envia o formulário
**Then** uma conta é criada com papel `usuario` (nunca aceito do formulário, mesmo se enviado), e-mail normalizado, `email_verificado=false`
**And** um registro é inserido em `emails_pendentes` (`tipo=verificacao_conta`, AD-4) na mesma transação
**And** um token é gerado em `TOKENS_ACAO` (`tipo=verificacao_email`, uso único — AD-18)

**Given** um e-mail já cadastrado
**When** o usuário tenta se cadastrar com o mesmo e-mail
**Then** o sistema responde 409 e a tela mostra "Este e-mail já está cadastrado."

**Given** uma conta com `email_verificado=false`
**When** o usuário tenta fazer login antes de confirmar
**Then** o login é recusado com mensagem orientando a verificar o e-mail

**Given** um link de verificação válido e não expirado
**When** o usuário clica no link
**Then** `email_verificado` passa a `true` e o token é marcado como usado (nunca reutilizável)

### Story 1.4: Login por e-mail e senha

As a Usuário com conta confirmada,
I want entrar com e-mail e senha,
So that eu acesse as funcionalidades do meu papel.

**Acceptance Criteria:**

**Given** uma conta ativa, com e-mail verificado e senha correta
**When** o usuário submete a tela de Login
**Then** um JWT de acesso (30min) é emitido e um refresh token rotativo é definido em cookie `HttpOnly` com TTL de 2h (AD-6)

**Given** uma sessão sem atividade por mais de 2h
**When** o usuário tenta usar o refresh token expirado
**Then** a sessão é encerrada e é necessário logar novamente

**Given** credenciais inválidas
**When** o usuário submete o formulário
**Then** o sistema responde com mensagem genérica (não revela se o e-mail existe)

**Given** qualquer endpoint autenticado
**When** uma requisição chega sem token válido
**Then** a resposta é 401, exceto nas rotas de login e cadastro

### Story 1.5: Autorização por papel aplicada no servidor

As a Adm/Gestor da organização,
I want que toda ação sensível seja checada no servidor, não só escondida na interface,
So that nenhum Usuário execute uma ação além do seu papel mesmo chamando a API diretamente.

**Acceptance Criteria:**

**Given** um Usuário autenticado com papel `usuario`
**When** ele chama diretamente um endpoint restrito a `almoxarife`+
**Then** a resposta é 403, independente do que a interface mostra

**Given** a hierarquia de papel como ordem total `adm=4 > gestor=3 > almoxarife=2 > usuario=1` (AD-8)
**When** o middleware avalia uma rota com papel mínimo exigido
**Then** a checagem usa essa fórmula, nunca uma allow-list de pares reimplementada por rota

**Given** uma rota que precisará de filtro de escopo em listagem (ex. FR-24, Epic 7)
**When** o middleware resolve o papel do Usuário autenticado
**Then** esse papel fica disponível no contexto da requisição para o service aplicar o filtro, sem re-consultar o banco

### Story 1.6: Recuperação de senha por e-mail

As a Usuário que esqueceu a senha,
I want solicitar redefinição por e-mail,
So that eu recupere o acesso sem depender de suporte.

**Acceptance Criteria:**

**Given** um e-mail informado na tela "Esqueci minha senha"
**When** o usuário envia o formulário
**Then** o sistema sempre responde "Se o e-mail existir, você receberá um link.", exista ou não a conta
**And**, se a conta existir, um token é gerado em `TOKENS_ACAO` (`tipo=redefinicao_senha`, expira em 30min, uso único) e um registro é inserido em `emails_pendentes` na mesma transação

**Given** um link de redefinição válido e não expirado
**When** o usuário define uma nova senha (mínimo 8 caracteres, com letra e número)
**Then** a senha é atualizada, o token é marcado como usado, e todas as sessões ativas da conta são revogadas

**Given** uma conta que hoje só tem login via SSO (`senha_hash` nulo)
**When** o usuário usa este fluxo pela primeira vez
**Then** uma senha própria é criada e a conta passa a ter os dois caminhos de login disponíveis

**Given** um link expirado ou já usado
**When** o usuário tenta acessá-lo
**Then** a tela explica o motivo e oferece gerar um novo link

### Story 1.7: Solicitação de promoção de papel

As a Usuário ou Almoxarife,
I want solicitar promoção para o papel imediatamente acima,
So that eu ganhe acesso às funcionalidades que preciso, mediante aprovação.

**Acceptance Criteria:**

**Given** um Usuário sem solicitação pendente
**When** ele clica "Solicitar promoção" em Meu Perfil
**Then** uma `SOLICITACOES_PROMOCAO` é criada com o papel imediatamente acima como alvo, status `pendente`

**Given** uma solicitação pendente para promoção a `almoxarife`
**When** um `gestor` ou `adm` a aprova
**Then** o papel do Usuário muda imediatamente (não espera a sessão expirar) e a decisão registra quem decidiu e quando

**Given** uma solicitação de promoção a `gestor`
**When** qualquer papel diferente de `adm` tenta decidir
**Then** a resposta é 403

**Given** uma solicitação já pendente para um Usuário
**When** o mesmo Usuário tenta criar outra
**Then** o sistema rejeita — só uma solicitação pendente por vez

**Given** uma solicitação rejeitada
**When** o Usuário tenta solicitar novamente
**Then** é permitido, sem período de espera

### Story 1.8: Gestão de contas — desativação e rebaixamento

As a Gestor ou Adm,
I want desativar ou rebaixar contas dentro do meu escopo de autoridade,
So that eu controle quem tem acesso ao sistema.

**Acceptance Criteria:**

**Given** um `gestor` na tela de Gestão de Usuários
**When** ele lista as contas
**Then** só vê contas com papel `almoxarife` ou `usuario` (nunca `gestor`/`adm`)

**Given** uma conta `almoxarife` sendo desativada por um `gestor`
**When** a ação é confirmada via `ConfirmDialog` (Story 1.2)
**Then** a conta não autentica mais (senha ou SSO) e qualquer sessão ativa é encerrada na próxima requisição

**Given** um `gestor` tentando agir sobre outro `gestor` ou um `adm`
**When** ele tenta desativar/rebaixar essa conta
**Then** a resposta é 403

**Given** um `adm` agindo sobre qualquer conta, inclusive `gestor`
**When** ele desativa ou rebaixa
**Then** a ação é permitida

### Story 1.9: Login federado via Keycloak — SSO Ferreira Costa

As a colaborador com conta corporativa Ferreira Costa,
I want entrar no stockflow com a mesma identidade usada em outros sistemas internos,
So that eu não precise lembrar de outra senha.

**Acceptance Criteria:**

**Given** a tela de Login
**When** ela carrega
**Then** e-mail/senha aparece como caminho padrão visível e o botão "Entrar com Ferreira Costa" aparece como opção adicional — nunca redirecionamento automático

**Given** um Usuário completando a autenticação no Keycloak (realm `ferreiracosta`) e retornando com um token válido
**When** o backend processa a troca
**Then** valida assinatura RS256 via JWKS, `iss`, e `azp` contra allowlist, exige `email_verified=true` (AD-7), busca o Usuário por e-mail normalizado (case-insensitive) sem criar conta nova, e emite os mesmos tokens de sessão da Story 1.4

**Given** um e-mail do Keycloak sem conta local correspondente
**When** o backend não encontra o Usuário
**Then** o login é recusado com mensagem orientando a se cadastrar primeiro (Story 1.3)

**Given** um e-mail encontrado mas `email_verified=false` no token
**When** o backend processa a troca
**Then** o login é recusado com mensagem distinta, orientando a confirmar o e-mail corporativo

**Given** uma sessão iniciada via SSO
**When** o Usuário faz logout
**Then** a sessão local é encerrada e o RP-initiated logout do Keycloak é disparado

### Story 1.10: Bloqueio de conta e política de senha

As a Adm responsável pela segurança do sistema,
I want que o login por senha resista a força bruta e exija senha minimamente robusta,
So that contas não sejam comprometidas por adivinhação de senha.

**Acceptance Criteria:**

**Given** 5 tentativas de login malsucedidas consecutivas para a mesma conta
**When** a 6ª tentativa chega
**Then** a conta fica bloqueada por 15 minutos, sem revelar o tempo exato restante na mensagem

**Given** uma conta bloqueada
**When** o Usuário tenta "Esqueci minha senha" (Story 1.6)
**Then** o fluxo continua disponível — bloqueio de tentativas não afeta redefinição

**Given** o cadastro (Story 1.3) ou a redefinição (Story 1.6) de uma senha
**When** a senha tem menos de 8 caracteres ou não contém letra e número
**Then** o sistema rejeita com mensagem explicando o critério

**Given** o login via SSO (Story 1.9)
**When** há tentativas malsucedidas de senha para a mesma conta
**Then** o caminho SSO não é afetado

### Story 1.11: MFA obrigatório para papéis administrativos

As a Gestor ou Adm autenticado por senha,
I want configurar um segundo fator (TOTP),
So that minha conta administrativa tenha uma camada extra de proteção.

**Acceptance Criteria:**

**Given** uma conta `gestor`/`adm` autenticada por senha sem MFA configurado
**When** ela tenta acessar qualquer ação restrita ao seu papel
**Then** é redirecionada para Configurações → Segurança antes de liberar a ação (UX-DR22)

**Given** a tela de configuração de MFA
**When** o Usuário escaneia o QR Code do autenticador e confirma um código válido
**Then** o segundo fator fica ativo e passa a ser exigido em logins futuros por senha

**Given** uma conta `usuario`/`almoxarife`
**When** ela acessa Configurações → Segurança
**Then** a configuração de MFA aparece como opcional, nunca forçada

**Given** um login via SSO (Story 1.9) para conta `gestor`/`adm`
**When** o Usuário autentica pelo Keycloak
**Then** a tela de MFA do stockflow nunca aparece — o realm corporativo já impõe o segundo fator

### Story 1.12: Log de acesso e auditoria

As a Adm,
I want consultar um log de todas as tentativas de login,
So that eu tenha visibilidade de acessos legítimos e suspeitos.

**Acceptance Criteria:**

**Given** qualquer tentativa de login (sucesso ou falha, por senha ou SSO)
**When** ela ocorre
**Then** um registro append-only é criado com usuário (quando identificável), timestamp, IP e método

**Given** uma tentativa de login com e-mail inexistente
**When** ela é registrada
**Then** o registro não revela ao solicitante se o e-mail existe, mas fica visível para o `adm` no log

**Given** a tela de Log de Acesso
**When** um `adm` a acessa e filtra por período
**Then** vê os registros, sem nenhuma ação de edição ou exclusão disponível na interface

**Given** um Usuário sem papel `adm`
**When** ele tenta acessar a rota de log de acesso
**Then** a resposta é 403 e o item de navegação nem aparece

## Epic 2: Gestão de Estoques

Almoxarife organiza os locais físicos de estoque com integridade referencial.

### Story 2.1: Criar e listar locais de Estoque

As a Almoxarife,
I want cadastrar e listar os locais físicos de Estoque,
So that eu possa organizar onde os Produtos ficam armazenados.

**Acceptance Criteria:**

**Given** um Almoxarife autenticado
**When** ele cadastra um novo Estoque com um nome
**Then** o Estoque é criado com `id` (UUID v4) e o nome é único de forma atômica (case/espaço-insensitive)

**Given** um nome de Estoque já existente (mesmo com capitalização/espaçamento diferente)
**When** outro Almoxarife tenta cadastrar o mesmo nome, inclusive sob requisições concorrentes
**Then** o sistema rejeita com 409

**Given** um Usuário com papel `usuario`
**When** ele tenta cadastrar ou excluir Estoques diretamente pela API
**Then** a resposta é 403 (Story 1.5)

**Given** a lista de Estoques cadastrados
**When** qualquer Usuário autenticado a consulta
**Then** a lista retorna nome e id de cada Estoque

### Story 2.2: Exclusão de Estoque trata resíduos e pedidos pendentes

As a Almoxarife,
I want que a exclusão de um Estoque seja bloqueada quando ainda há uso ativo,
So that eu não perca rastreabilidade de estoque residual ou de Pedidos em andamento.

**Acceptance Criteria:**

**Given** um Estoque sem nenhuma outra tabela referenciando-o ainda (estado do sistema logo após o Epic 2)
**When** um Almoxarife exclui o Estoque
**Then** a exclusão é permitida — não há o que bloquear nesse ponto do sistema

**Given** um Estoque com quantidade residual de algum Produto, uma vez que `PRODUTO_ESTOQUE` exista (Epic 3)
**When** um Almoxarife tenta excluir o Estoque
**Then** a exclusão é bloqueada e a resposta lista quais Produtos ainda têm quantidade ali

**Given** um Estoque referenciado por um Pedido com status `pendente`, uma vez que `PEDIDOS` exista (Epic 7)
**When** um Almoxarife tenta excluir o Estoque
**Then** a exclusão é bloqueada, mesmo com quantidade residual zerada

**Given** um Estoque sem quantidade residual e sem Pedido pendente referenciando
**When** um Almoxarife confirma a exclusão via `ConfirmDialog`
**Then** o Estoque é removido

**Nota de implementação:** os dois critérios de bloqueio (quantidade residual, Pedido pendente) dependem de tabelas criadas em épicos posteriores (`PRODUTO_ESTOQUE` no Epic 3, `PEDIDOS` no Epic 7). Esta story entrega a exclusão funcional agora; os guards concretos são adicionados como parte das stories que criam essas tabelas (3.1 e 7.2, respectivamente), sem reabrir esta story — Epic 2 não fica bloqueado esperando por Epic 3/7.

### Story 2.3: Migração dos Estoques legados

As a Adm/Almoxarife responsável pela migração,
I want que os Estoques do sistema legado sejam migrados automaticamente para o schema novo,
So that nenhum local de armazenamento existente se perca no corte de migração.

**Acceptance Criteria:**

**Given** a tabela `estoques` do sistema legado (espelho do Firestore mantido pela empresa)
**When** o script `cmd/migrate-legado` (AD-15) processa a migração de Estoques
**Then** cada Estoque legado é recriado com um novo UUID v4, preservando o nome, e uma entrada é criada na tabela de mapeamento id-antigo→id-novo

**Given** dois Estoques legados com nomes equivalentes por case/espaço
**When** a migração roda
**Then** o conflito de unicidade (Story 2.1) é detectado e reportado para revisão manual antes do corte — a migração nunca cria duas linhas para o "mesmo" Estoque

**Given** a migração já executada uma vez
**When** o script é executado novamente
**Then** Estoques já migrados não são duplicados (idempotência)

**Given** o corte de dados em produção
**When** o script é executado
**Then** a execução é sempre disparada manualmente por uma pessoa, nunca por um agente autônomo (AD-15, PRD §9)

## Epic 3: Cadastro, Importação e Fotos de Produtos

Almoxarife popula e mantém o catálogo — manual, em lote via planilha, ou com fotos.

### Story 3.1: Cadastro manual de Produto com dimensões estruturadas

As a Almoxarife,
I want cadastrar um Produto manualmente com suas dimensões e estoque inicial,
So that o catálogo reflita os materiais realmente disponíveis.

**Acceptance Criteria:**

**Given** um Almoxarife autenticado em Catálogo → Cadastrar
**When** ele informa nome, código (opcional), categoria, dimensões (cada uma como par valor+unidade — comprimento, largura, diâmetro, altura, espessura), estoque destino, quantidade inicial e observações
**Then** um Produto é criado e uma linha em `PRODUTO_ESTOQUE` vincula o Produto ao Estoque informado com a quantidade inicial (AD-9) — isto também completa o guard de exclusão da Story 2.2 (quantidade residual passa a ser verificável)

**Given** uma dimensão informada com valor mas sem unidade (ou vice-versa)
**When** o Almoxarife tenta salvar
**Then** o sistema rejeita o campo específico, sem salvar um Produto parcialmente preenchido

**Given** um Usuário com papel `usuario`
**When** ele tenta cadastrar um Produto diretamente pela API
**Then** a resposta é 403

**Given** a lista de 25 Categorias (addendum §H)
**When** o formulário de cadastro carrega
**Then** a Categoria é selecionada a partir dessa lista fixa, nunca digitada livremente

### Story 3.2: Nomenclatura Guiada por subtipo

As a Almoxarife,
I want usar um template de nome sugerido ao cadastrar um Produto de um subtipo conhecido,
So that o catálogo tenha nomes consistentes entre diferentes pessoas cadastrando.

**Acceptance Criteria:**

**Given** a lista de 28 templates de Nomenclatura Guiada (addendum §G)
**When** o Almoxarife seleciona um template ao cadastrar um Produto
**Then** o campo nome exige preencher todos os placeholders do template, na mesma ordem de tokens, validado no servidor

**Given** um Produto cadastrado sem template selecionado
**When** o Almoxarife informa o nome
**Then** o campo aceita texto livre, sem validação de estrutura

**Given** um Produto com template aplicado
**When** o Almoxarife edita o nome depois do cadastro
**Then** a edição revalida o nome contra o mesmo template — não é possível burlar a regra editando depois

### Story 3.3: Importação em massa via planilha padronizada

As a Almoxarife,
I want importar uma planilha com muitos Produtos de uma vez,
So that eu não precise cadastrar item a item ao encerrar uma obra ou consolidar um catálogo.

**Acceptance Criteria:**

**Given** o modelo de planilha padronizado (nome, código, categoria, dimensões com valor+unidade separados, quantidade, estoque, observações)
**When** o Almoxarife envia uma planilha com o cabeçalho correto
**Then** cada linha é processada, criando Estoques ausentes automaticamente (Story 2.1) e um Produto por linha válida

**Given** uma planilha com cabeçalho fora do padrão
**When** o Almoxarife tenta importar
**Then** a importação inteira é rejeitada antes de processar qualquer linha, indicando o problema no cabeçalho

**Given** uma linha com valor de dimensão sem a unidade correspondente
**When** a planilha é processada
**Then** essa linha específica é marcada como erro no relatório final, sem interromper as demais linhas

**Given** uma importação interrompida no meio (falha de rede, fechamento do navegador)
**When** o Almoxarife reabre a tela de Importar
**Then** um banner mostra até onde a importação chegou e oferece continuar, sem reprocessar linhas já salvas (`IMPORTACOES`/`IMPORTACAO_LINHAS`, addendum §A)

### Story 3.4: Importação atualiza por código, não só cria

As a Almoxarife,
I want que reimportar uma planilha atualize Produtos já existentes em vez de duplicá-los,
So that eu possa reprocessar uma planilha corrigida sem gerar lixo no catálogo.

**Acceptance Criteria:**

**Given** uma planilha com uma linha cujo código já existe no catálogo
**When** a importação processa essa linha
**Then** o Produto existente é atualizado com os novos valores, em vez de criar um Produto duplicado

**Given** uma planilha reimportada sem nenhuma mudança
**When** a importação processa
**Then** nenhum Produto novo é criado, e a linha nunca aparece como "criado" no relatório

**Given** o relatório final de importação
**When** ele é exibido
**Then** discrimina quantas linhas foram criadas, atualizadas e rejeitadas, com um CTA "Verificar duplicatas agora" apontando para a Normalização (Epic 6, UX-DR20)

**Given** uma linha sem código cujo nome é parecido com um Produto existente
**When** a importação processa
**Then** um novo Produto é criado — correspondência por nome sem código fica para a ferramenta de Duplicatas, não para o importador

### Story 3.5: Upload e armazenamento de foto do Produto

As a Almoxarife,
I want anexar fotos a um Produto,
So that outros Usuários reconheçam o material visualmente antes de reservar.

**Acceptance Criteria:**

**Given** um Produto existente
**When** um Almoxarife envia uma foto (JPG/PNG/WEBP, via câmera ou galeria do dispositivo)
**Then** a imagem é redimensionada para 500px no maior lado e comprimida em JPEG q=0.82, independente do fluxo (cadastro ou edição)
**And** o arquivo é salvo em volume Docker nomeado e persistente, com nome versionado (`<produto_id>-<timestamp_unix>.jpg`), nunca em base64 no banco (AD-11)

**Given** um arquivo fora do tamanho ou formato aceito
**When** o Almoxarife tenta enviar
**Then** o sistema rejeita com um erro específico indicando se o problema é tamanho ou formato

**Given** um Produto já com uma foto
**When** um Almoxarife envia uma nova foto
**Then** a foto anterior não é sobrescrita no mesmo caminho — o nome versionado evita servir uma imagem obsoleta de cache

**Given** um Usuário com papel `usuario`
**When** ele tenta enviar uma foto pela API
**Then** a resposta é 403 — upload é só para `almoxarife`+; visualização continua liberada a todos

### Story 3.6: Galeria e visualização ampliada de fotos (lightbox)

As a qualquer Usuário,
I want ver as fotos de um Produto em destaque,
So that eu confirme visualmente que é o material certo antes de reservar.

**Acceptance Criteria:**

**Given** um Produto com múltiplas fotos
**When** um Usuário abre o card ou o detalhe do Produto
**Then** vê uma galeria navegável de todas as fotos

**Given** uma foto na galeria
**When** o Usuário toca/clica nela
**Then** ela expande em lightbox de tela cheia (UX-DR23)

**Given** o lightbox aberto
**When** o Usuário fecha (toque fora, `Esc`, ou botão fechar)
**Then** retorna à posição exata de rolagem anterior, sem recarregar a página

### Story 3.7: Migração de Produtos, Categorias e fotos legadas

As a Adm/Almoxarife responsável pela migração,
I want que os Produtos, suas fotos e as Categorias do sistema legado sejam migrados para o schema novo,
So that o catálogo não comece vazio na virada para o novo sistema.

**Acceptance Criteria:**

**Given** a tabela `produtos` do sistema legado, com dimensões em texto livre
**When** o script `cmd/migrate-legado` processa a migração de Produtos
**Then** cada dimensão é convertida para o par estruturado `{valor, unidade}` usando um parser único de conversão; casos ambíguos ficam marcados para revisão manual via Normalização (Epic 6)

**Given** as fotos armazenadas inline em base64 no sistema legado
**When** a migração processa um Produto com foto
**Then** a foto é extraída, redimensionada/comprimida conforme a Story 3.5, e salva no volume de fotos — nunca migrada como base64

**Given** a lista de 25 Categorias e os 28 Templates de Nomenclatura Guiada (addendum §H, §G)
**When** a migração roda pela primeira vez
**Then** essas listas são inseridas como seed, como fonte única — mesmo estando hoje duplicadas em dois lugares do sistema legado

**Given** a migração já executada
**When** o script roda novamente
**Then** Produtos e seeds já migrados não são duplicados

**Given** o corte de dados em produção
**When** o script é executado
**Then** é sempre disparado manualmente por uma pessoa, nunca por um agente autônomo

## Epic 4: Catálogo — Consulta, Descoberta e Exportação

Qualquer Usuário encontra material disponível, vê onde está (inclusive via QR Code/código de barras), e exporta o catálogo.

### Story 4.1: Busca por nome/código/categoria com sugestões

As a qualquer Usuário,
I want buscar um Produto por nome, código ou categoria com sugestões automáticas,
So that eu encontre rapidamente o material que preciso.

**Acceptance Criteria:**

**Given** o campo de busca do Catálogo
**When** o Usuário digita alguns caracteres
**Then** até 7 sugestões aparecem, ordenadas por relevância, atualizando conforme ele digita

**Given** uma busca sem nenhum resultado
**When** o Usuário completa a digitação
**Then** a tela mostra "Nenhum produto encontrado para '{busca}'.", sem sugestão de comprar externamente

**Given** a NFR de desempenho (≤300ms p95, até 8.000 produtos/30 estoques)
**When** a busca é executada sob carga típica
**Then** o tempo de resposta cumpre esse limite

### Story 4.2: Filtros por categoria, estoque e disponibilidade

As a qualquer Usuário,
I want filtrar o catálogo por categoria, estoque e disponibilidade,
So that eu restrinja a lista ao que realmente me interessa.

**Acceptance Criteria:**

**Given** a lista de Categorias e Estoques cadastrados
**When** o Usuário aplica um ou mais filtros simultaneamente
**Then** a lista de Produtos reflete a combinação de todos os filtros ativos

**Given** o filtro "Com estoque"
**When** aplicado
**Then** só aparecem Produtos com quantidade maior que zero em pelo menos um Estoque

**Given** filtros aplicados junto com a busca por texto (Story 4.1)
**When** ambos estão ativos
**Then** busca e filtros combinam (E lógico), um não substitui o outro

### Story 4.3: Visualização em grade e tabela agrupada

As a qualquer Usuário,
I want alternar entre visualização em grade (cards) e tabela agrupada,
So that eu escolha o formato mais adequado ao meu dispositivo e tarefa.

**Acceptance Criteria:**

**Given** o Catálogo em viewport ≥768px
**When** o Usuário alterna para visualização em tabela
**Then** produtos com mesmo nome/unidade/dimensões aparecem agrupados numa linha, com a soma das quantidades

**Given** o Catálogo em viewport <768px (a partir de 360px)
**When** o Usuário o acessa
**Then** a visualização padrão é em grade (cards), consistente com o shell mobile (UX-DR16)
**And** cada card mostra o badge `status-disponivel` ("Disponível"/"Sem estoque") sempre com ícone + texto, nunca só cor (UX-DR10)

**Given** uma linha agrupada na tabela
**When** o Usuário clica/expande
**Then** vê a quantidade discriminada por Estoque (Story 4.4)

### Story 4.4: Detalhe do produto por Estoque com atualização em tempo real

As a qualquer Usuário,
I want ver o detalhe de um Produto com a quantidade exata por Estoque, sempre atualizada,
So that eu confie no que vejo antes de reservar.

**Acceptance Criteria:**

**Given** o detalhe de um Produto
**When** ele é aberto
**Then** mostra a quantidade discriminada por cada Estoque onde o Produto está presente

**Given** o canal SSE `produtos` (AD-3)
**When** um evento de mudança de quantidade chega enquanto a tela está aberta
**Then** um toast discreto (`aria-live="polite"`) avisa "Catálogo atualizado.", sem recarregar a tela sozinho (UX-DR17)

**Given** uma conexão SSE que demora mais que alguns segundos para reconectar
**When** isso acontece
**Then** um indicador "Reconectando..." aparece; reconexão rápida permanece silenciosa (UX-DR18)

**Given** a reconexão SSE concluída
**When** o cliente volta a ficar online
**Then** ele sempre faz um GET completo do estado atual, nunca espera replay de eventos perdidos (AD-3)

### Story 4.5: Identificação de Produto via QR Code / código de barras

As a qualquer Usuário,
I want apontar a câmera para um Código de Identificação e ir direto ao Produto,
So that eu não precise digitar a busca em campo.

**Acceptance Criteria:**

**Given** a tela de Catálogo ou Carrinho
**When** o Usuário toca no `fab-scanner` (UX-DR4) e aponta a câmera para um QR Code/código de barras
**Then** o Código de Identificação lido abre o detalhe do Produto (Story 4.4) ou o adiciona ao Carrinho, conforme o contexto

**Given** a câmera sem permissão concedida, sem hardware, ou incapaz de reconhecer o código
**When** a leitura falha
**Then** uma mensagem clara aparece e o campo de busca por texto (Story 4.1) continua disponível e em foco — o scanner nunca é a única forma de encontrar um Produto (UX-DR13)

**Given** um ambiente sem contexto seguro (HTTPS)
**When** o Usuário tenta acessar o scanner
**Then** a funcionalidade fica indisponível com uma mensagem explicando o motivo — a câmera do navegador exige HTTPS (UX-DR21)

**Given** um Produto sem Código de Identificação cadastrado
**When** alguém tenta escaneá-lo
**Then** ele continua acessível normalmente por busca textual, só sem esse atalho

### Story 4.6: Exportação da tabela do catálogo para Excel

As a Almoxarife,
I want exportar a tabela do catálogo, com os filtros aplicados, para Excel,
So that eu leve os dados para uma planilha ou relatório externo.

**Acceptance Criteria:**

**Given** a visualização em tabela do Catálogo com um ou mais filtros ativos (Story 4.2)
**When** o Almoxarife exporta
**Then** o `.xlsx` gerado reflete exatamente o filtro aplicado, com subtotais dinâmicos (fórmula `SUBTOTAL`, não soma estática) por grupo

**Given** um filtro que resulta em zero produtos
**When** o Almoxarife exporta mesmo assim
**Then** o `.xlsx` é gerado válido, contendo só o cabeçalho

**Given** um Usuário com papel `usuario`
**When** ele tenta exportar diretamente pela API
**Then** a resposta é 403 — exportação é restrita a `almoxarife`+

## Epic 5: Movimentação de Estoque

Almoxarife registra saída e transferência de estoque com histórico consultável.

### Story 5.1: Registrar Baixa (consumo)

As a Almoxarife,
I want registrar a baixa (consumo) de um Produto em um Estoque,
So that o saldo reflita o que realmente saiu, com rastro auditável.

**Acceptance Criteria:**

**Given** um Produto com quantidade disponível em um Estoque
**When** um Almoxarife registra uma Baixa de uma quantidade válida (maior que zero)
**Then** o sistema usa `SELECT ... FOR UPDATE` na linha de `PRODUTO_ESTOQUE` (AD-10), debita a quantidade e cria uma Movimentação do tipo `baixa` na mesma transação — nunca uma sem a outra

**Given** uma quantidade zero ou negativa informada
**When** o Almoxarife tenta registrar
**Then** o sistema rejeita antes de qualquer escrita

**Given** uma quantidade maior que a disponível
**When** o Almoxarife tenta registrar
**Then** o sistema rejeita, informando a quantidade real disponível no momento

**Given** um Usuário com papel `usuario`
**When** ele tenta registrar uma Baixa pela API
**Then** a resposta é 403

### Story 5.2: Registrar Transferência entre Estoques

As a Almoxarife,
I want transferir uma quantidade de um Produto entre dois Estoques,
So that o material fique corretamente localizado sem passar por uma Baixa/Entrada manual.

**Acceptance Criteria:**

**Given** dois Estoques diferentes com o Produto presente na origem
**When** um Almoxarife registra uma Transferência de origem para destino
**Then** a checagem de disponibilidade e o débito/crédito são atômicos na mesma transação, com locks adquiridos na ordem canônica `(produto_id, estoque_id)` ascendente (AD-10)
**And** uma Movimentação do tipo `transferencia` é criada com origem e destino registrados

**Given** origem igual ao destino
**When** o Almoxarife tenta transferir
**Then** o sistema rejeita

**Given** quantidade maior que a disponível na origem
**When** a transferência é tentada
**Then** o sistema rejeita, sem debitar nada

**Given** duas transferências concorrentes envolvendo os mesmos dois Estoques, montadas em ordens de inserção opostas
**When** ambas tentam rodar ao mesmo tempo
**Then** a ordem canônica de lock evita deadlock — uma espera a outra, nenhuma trava indefinidamente

### Story 5.3: Histórico de Movimentações consultável

As a Almoxarife,
I want consultar o histórico de todas as Movimentações,
So that eu tenha rastreabilidade completa de saídas e transferências.

**Acceptance Criteria:**

**Given** Movimentações registradas (Baixa ou Transferência)
**When** um Almoxarife acessa o Histórico
**Then** vê Produto, tipo, origem, destino, quantidade, autor e data de cada Movimentação, em ordem cronológica

**Given** o canal SSE `movimentacoes` (AD-3)
**When** uma nova Movimentação é criada
**Then** um evento é publicado nesse canal para qualquer tela assinante atualizar (toast discreto, UX-DR17)

**Given** um Usuário com papel `usuario`
**When** ele tenta consultar o Histórico pela API
**Then** a resposta é 403

### Story 5.4: Migração do Histórico de Movimentações legado

As a Adm/Almoxarife responsável pela migração,
I want que o histórico de movimentações do sistema legado seja migrado,
So that a rastreabilidade anterior ao corte não se perca.

**Acceptance Criteria:**

**Given** a tabela `historico` do sistema legado (produto desnormalizado por nome, tipo baixa/transferência, origem/destino, quantidade, timestamp)
**When** o script `cmd/migrate-legado` processa o Histórico
**Then** cada registro é recriado como uma Movimentação vinculada ao novo `produto_id` (via tabela de mapeamento id-antigo→id-novo), preservando data e autor originais quando disponíveis

**Given** um registro do histórico legado referenciando um Produto que não foi migrado
**When** a migração processa esse registro
**Then** ele é marcado para revisão manual, sem interromper a migração dos demais

**Given** a migração já executada
**When** o script roda novamente
**Then** registros já migrados não são duplicados

**Given** o corte de dados em produção
**When** o script é executado
**Then** é sempre disparado manualmente por uma pessoa, nunca por um agente autônomo

## Epic 6: Normalização de Dados

Almoxarife mantém o catálogo limpo (inconsistências e duplicatas) sem trabalho manual item a item.

### Story 6.1: Detecção de inconsistências dimensionais

As a Almoxarife,
I want que o sistema aponte inconsistências dimensionais em Produtos,
So that eu identifique dados incompletos ou malformados sem revisar item a item manualmente.

**Acceptance Criteria:**

**Given** um Produto com uma dimensão estruturada válida
**When** a análise de inconsistências roda
**Then** esse campo nunca gera sugestão

**Given** um Produto migrado com uma dimensão que não pôde ser convertida automaticamente na migração (Story 3.7), ou um valor de dimensão implícito no nome sem preenchimento no campo estruturado
**When** o Almoxarife clica "Analisar todos os produtos"
**Then** o sistema lista sugestões, cada uma identificando produto, campo, valor sugerido e origem (migração ou nome)

### Story 6.2: Aplicação seletiva de correções

As a Almoxarife,
I want aplicar ou ignorar cada sugestão de correção, individualmente ou em lote,
So that eu limpe o catálogo no meu ritmo, sem re-analisar o que já decidi.

**Acceptance Criteria:**

**Given** uma lista de sugestões de inconsistência (Story 6.1)
**When** o Almoxarife aplica uma correção individualmente, em lote por produto, ou em lote geral
**Then** os campos correspondentes são atualizados para o valor estruturado sugerido

**Given** uma sugestão marcada como "ignorar" para um valor específico
**When** a mesma inconsistência (mesmo produto, campo, valor) é reavaliada depois
**Then** ela não reaparece

**Given** um campo cujo valor muda depois para um novo valor inconsistente diferente do que foi ignorado
**When** a análise roda de novo
**Then** a sugestão reaparece — "ignorar" vale só para o valor específico já visto

### Story 6.3: Detecção de duplicatas

As a Almoxarife,
I want que o sistema identifique Produtos duplicados,
So that eu consolide o catálogo sem perder tempo comparando manualmente.

**Acceptance Criteria:**

**Given** dois ou mais Produtos com nome normalizado igual, dimensões equivalentes (considerando conversão de unidade) e locais coincidentes
**When** a detecção de duplicatas roda
**Then** esses Produtos aparecem agrupados como candidatos a mesclagem

**Given** Produtos com nome normalizado igual mas dimensões diferentes
**When** a detecção roda
**Then** eles não são agrupados como duplicatas

**Given** o relatório de importação (Stories 3.3/3.4)
**When** o Almoxarife clica "Verificar duplicatas agora"
**Then** é levado direto a esta tela com a análise já em andamento

### Story 6.4: Mesclagem de duplicatas com trilha de auditoria

As a Almoxarife,
I want mesclar Produtos duplicados mantendo um só registro,
So that o catálogo fique consolidado sem perder o histórico de nenhum dos itens mesclados.

**Acceptance Criteria:**

**Given** um grupo de Produtos duplicados (Story 6.3)
**When** o Almoxarife confirma a mesclagem (via `ConfirmDialog`) escolhendo qual Produto mantém
**Then** a quantidade dos demais é somada no Produto mantido, os demais são soft-deletados (`deleted_at`), e uma auditoria é registrada (quem, quando, produtos removidos, valores) em `MESCLAGEM_PRODUTOS_REMOVIDOS` — permanente, nunca expurgada

**Given** as linhas históricas de Movimentações e itens de Pedido dos Produtos removidos
**When** a mesclagem é confirmada
**Then** o `produto_id` dessas linhas é reescrito para o Produto sobrevivente antes do soft-delete, preservando "soma de Movimentações == quantidade atual" (AD-11)

**Given** um item em Carrinho ou Pedido pendente referenciando um Produto que acabou de ser mesclado
**When** a mesclagem é confirmada
**Then** esse item é redirecionado automaticamente para o Produto mantido

**Given** a quantidade somada calculada no momento da revisão
**When** o Almoxarife confirma a mesclagem depois de um tempo
**Then** a quantidade é revalidada na confirmação, nunca usa um snapshot antigo

**Given** um Produto já soft-deletado por uma mesclagem anterior
**When** ele aparece em uma nova análise de duplicatas
**Then** ele nunca reentra em uma nova mesclagem, mas sua foto permanece em disco para auditoria permanente

## Epic 7: Pedidos de Retirada

Usuário solicita, almoxarife aprova com estoque real, recibo em PDF — ciclo completo.

### Story 7.1: Carrinho de reserva

As a qualquer Usuário,
I want acumular itens num carrinho antes de enviar um Pedido,
So that eu monte a solicitação inteira antes de formalizar.

**Acceptance Criteria:**

**Given** um Produto disponível num Estoque
**When** o Usuário o adiciona ao Carrinho (via detalhe do Produto, tabela, ou leitura de QR Code — Story 4.5)
**Then** o item entra no Carrinho, a disponibilidade é validada no momento da adição (somando linhas já no carrinho para o mesmo Produto/Estoque), um toast confirma "Adicionado ao Carrinho." e o `cart-badge` atualiza o contador (UX-DR5, UX-DR11)

**Given** um item no Carrinho cujo Produto ou Estoque deixou de existir (ex. mesclado — Story 6.4, ou Estoque excluído — Story 2.2)
**When** o Usuário abre o Carrinho
**Then** esse item é removido automaticamente, com aviso explicando o motivo

**Given** o Carrinho vazio
**When** o Usuário o acessa
**Then** vê "Seu carrinho está vazio. Busque um produto ou aponte a câmera para um código."

**Given** um item removido do Carrinho
**When** a remoção é confirmada
**Then** um toast confirma e o `cart-badge` atualiza (some por completo se ficar vazio, nunca mostra "0")

### Story 7.2: Envio de Pedido

As a qualquer Usuário,
I want enviar o Carrinho como um Pedido de Retirada formal,
So that o almoxarife possa aprovar e separar o material.

**Acceptance Criteria:**

**Given** um Carrinho com ao menos um item
**When** o Usuário informa solicitante, obra/centro de custo e observação, e envia
**Then** um Pedido é criado com status `pendente`, revalidando a disponibilidade de cada item no momento do envio — isto também completa o segundo guard de exclusão pendente na Story 2.2 (Pedido `pendente` referenciando um Estoque)

**Given** um Carrinho vazio
**When** o Usuário tenta enviar
**Then** o sistema rejeita

**Given** "solicitante" preenchido como texto livre diferente do nome do Usuário autenticado
**When** o Pedido é registrado
**Then** a auditoria e "Meus Pedidos" sempre usam a identidade autenticada, independente do texto livre

### Story 7.3: Consulta de Pedidos próprios

As a qualquer Usuário,
I want acompanhar meus próprios Pedidos,
So that eu saiba o status de cada solicitação que fiz.

**Acceptance Criteria:**

**Given** Pedidos enviados por um Usuário
**When** ele acessa "Meus Pedidos"
**Then** vê a lista filtrável por status, com badge `status-pendente`/`status-aprovado`/`status-rejeitado` sempre em ícone + texto, nunca só cor (UX-DR6, UX-DR10)

**Given** um Pedido de outro Usuário
**When** este Usuário tenta acessá-lo diretamente por id
**Then** o acesso é negado, conforme o padrão de escopo do sistema

### Story 7.4: Consulta de todos os Pedidos (Fila, Almoxarife+)

As a Almoxarife,
I want ver todos os Pedidos pendentes da organização, não só os meus,
So that eu tenha uma fila única de trabalho para atender.

**Acceptance Criteria:**

**Given** um `almoxarife`+ acessando Pedidos → Fila
**When** ele filtra por status
**Then** vê todos os Pedidos da organização que casam o filtro, não só os próprios

**Given** um Usuário sem papel `almoxarife`+ chamando a mesma rota
**When** a requisição chega
**Then** ele recebe só os próprios Pedidos — escopo, não erro (AD-8, Story 1.5)

### Story 7.5: Aprovação/rejeição com revalidação de estoque item a item

As a Almoxarife,
I want aprovar ou rejeitar um Pedido com o estoque real revalidado no momento da decisão,
So that eu nunca aprove algo que já não existe mais.

**Acceptance Criteria:**

**Given** um Pedido pendente com múltiplos itens
**When** o Almoxarife abre para decidir
**Then** cada item é revalidado no servidor; itens com estoque insuficiente mostram "Solicitado: X · Disponível: Y" (UX-DR19)

**Given** um item com divergência
**When** o Almoxarife escolhe aprovar parcialmente esse item
**Then** o débito e a Movimentação são atômicos só para a quantidade aprovada, e o restante volta como pendência separada — nunca um sucesso parcial silencioso

**Given** um Pedido com múltiplos itens de Produtos/Estoques diferentes
**When** a aprovação processa o lote inteiro
**Then** os locks são adquiridos na ordem canônica `(produto_id, estoque_id)` ascendente sobre o conjunto ordenado do lote inteiro, não na ordem de inserção do carrinho (AD-10)

**Given** o papel do aprovador
**When** a aprovação é submetida
**Then** o papel é revalidado no momento exato da submissão, não no carregamento da tela

**Given** uma decisão de aprovação/rejeição concluída
**When** ela é salva
**Then** o badge do Pedido muda de status na hora (canal SSE `pedidos`, AD-3) sem recarregar a página

### Story 7.6: Recibo do Pedido em PDF gerado pelo servidor

As a Usuário ou Almoxarife,
I want baixar um recibo em PDF de um Pedido já decidido,
So that eu tenha um comprovante formal da retirada.

**Acceptance Criteria:**

**Given** um Pedido aprovado ou parcialmente aprovado
**When** alguém com acesso ao Pedido clica "Baixar recibo"
**Then** um PDF é gerado no servidor com itens, quantidades, estoques de origem, solicitante, aprovador e data — sempre a partir do snapshot já capturado em `PEDIDO_ITENS` no momento da decisão, nunca um join ao vivo com o catálogo atual (AD-17)

**Given** um Produto editado depois da decisão do Pedido
**When** o recibo é baixado novamente
**Then** o conteúdo do PDF não muda — reflete o que foi decidido, não o estado atual do catálogo

**Given** um Pedido ainda pendente
**When** alguém tenta baixar o recibo
**Then** a opção não está disponível — só Pedidos já decididos têm recibo

### Story 7.7: Migração de Pedidos e vínculo com Histórico

As a Adm/Almoxarife responsável pela migração,
I want que os Pedidos do sistema legado sejam migrados preservando seu vínculo com o Histórico,
So that solicitações e aprovações anteriores ao corte continuem rastreáveis.

**Acceptance Criteria:**

**Given** a coleção `pedidos` do sistema legado (itens, status, solicitante, e-mail/uid, timestamps)
**When** o script `cmd/migrate-legado` processa Pedidos
**Then** cada Pedido é recriado com seus itens, referenciando os novos `produto_id`/`estoque_id` via a tabela de mapeamento id-antigo→id-novo, preservando o status original

**Given** um Pedido legado aprovado que gerou uma Movimentação no histórico legado
**When** ambos são migrados
**Then** o vínculo entre a Movimentação migrada (Story 5.4) e o Pedido migrado é preservado

**Given** a migração já executada
**When** o script roda novamente
**Then** Pedidos já migrados não são duplicados

**Given** o corte de dados em produção
**When** o script é executado
**Then** é sempre disparado manualmente por uma pessoa, nunca por um agente autônomo

## Epic 8: Privacidade e Conformidade (LGPD)

Usuário exporta os próprios dados pessoais; Adm processa solicitações de exclusão/anonimização — cobrindo identidade, log de acesso, Movimentações e Pedidos já existentes nos épicos anteriores.

### Story 8.1: Exportação dos próprios dados pessoais

As a Usuário,
I want baixar meus dados pessoais,
So that eu tenha controle e transparência sobre o que o sistema guarda sobre mim, conforme a LGPD.

**Acceptance Criteria:**

**Given** um Usuário autenticado em Meu Perfil
**When** ele clica "Baixar meus dados"
**Then** recebe um arquivo (JSON ou PDF) com nome, e-mail, log de acesso (Story 1.12), Movimentações que registrou (Story 5.3) e Pedidos que criou (Story 7.3)

**Given** um Usuário sem nenhuma Movimentação ou Pedido registrado
**When** ele exporta seus dados
**Then** o arquivo é gerado normalmente, com essas seções vazias

### Story 8.2: Exclusão e anonimização de dados pessoais por Adm

As a Adm,
I want processar uma solicitação de exclusão de conta,
So that o stockflow atenda ao direito de exclusão da LGPD sem quebrar a integridade de registros históricos.

**Acceptance Criteria:**

**Given** um Usuário solicitando exclusão da própria conta (via "Solicitar exclusão de conta" em Meu Perfil)
**When** a solicitação chega
**Then** ela fica registrada para um `adm` processar — não é uma exclusão self-service imediata

**Given** um `adm` processando uma solicitação de exclusão
**When** ele confirma a anonimização via `ConfirmDialog`
**Then** nome e e-mail da conta são substituídos por valores anonimizados, mas o `usuario_id` permanece intacto em qualquer Movimentação, Pedido ou registro de log já existente — nenhuma referência histórica é quebrada ou removida

**Given** uma conta já anonimizada
**When** alguém tenta autenticar com o e-mail antigo (senha ou SSO)
**Then** o login falha como se a conta não existisse

**Given** o `adm` que processaria a exclusão é o único `adm` ativo do sistema
**When** ele tenta anonimizar a própria conta ou a de outro `adm`
**Then** o sistema bloqueia com mensagem explicando que ao menos um `adm` ativo deve sempre existir

## Epic 9: Multi-Empresa e Plataforma

O stockflow deixa de ser uma instalação única (Ferreira Costa) para ser uma plataforma multi-cliente com isolamento total de dados entre Empresas (Architecture AD-19, AD-20). Dono da Plataforma cria Empresas com Ambiente de Treinamento automático; Usuário se vincula a uma Empresa via convite nominal; a Ferreira Costa migra para a primeira Empresa real do novo modelo.

### Story 9.1: Fundação Multi-Empresa — schema e isolamento por Empresa

As a arquiteto do sistema,
I want toda tabela de domínio escopada por Empresa e a Empresa resolvida de forma única e consistente em cada requisição,
So that nenhum dado de uma Empresa jamais vaze para outra, em nenhuma área do sistema, desde a primeira linha de código que depende disso.

**Acceptance Criteria:**

**Given** o schema hoje sem conceito de Empresa
**When** esta story é aplicada
**Then** existe uma tabela `empresas` (id, nome_fantasia, razao_social, cnpj, endereço completo, slug, status ativo/inativo, `empresa_origem_id` nullable) e toda tabela de domínio (`produtos`, `estoques`, `movimentacoes`, `pedidos`, `pedido_itens`, `categorias`, `logs_acesso`, `solicitacoes_promocao`, `mesclagens_duplicatas`, `mesclagem_produtos_removidos`, `importacoes`, `nomenclatura_templates`, `usuarios`) ganha uma coluna `empresa_id`

**Given** uma requisição autenticada chegando sob o prefixo `/e/{slug}/...`
**When** o middleware processa a requisição
**Then** ele resolve `empresa_id` a partir do slug **uma única vez** e o injeta no contexto da requisição junto com o papel já resolvido (mesmo padrão de AD-8) — nenhum `service` re-deriva ou aceita `empresa_id` vindo de body/query

**Given** qualquer `service` que lê ou escreve Catálogo, Estoques, Movimentações, Pedidos, Log de Acesso, Normalização/Duplicatas ou Gestão de Contas/Promoção
**When** a query é executada
**Then** ela sempre filtra por `empresa_id` do contexto da requisição — verificável por teste automatizado que cria dados em duas Empresas e confirma que uma nunca aparece na consulta da outra (SM-7)

**Given** a mesclagem de Duplicatas (Story 6.4) especificamente
**When** o agrupamento por nome/dimensão é calculado
**Then** ele nunca considera Produto de outra Empresa — mesclar através de Empresas diferentes é impossível, não apenas desencorajado

**Given** o índice único que hoje garante um só `adm` ativo no sistema inteiro
**When** esta story é aplicada
**Then** o índice passa a ser único **por `empresa_id`** — cada Empresa tem exatamente um `adm` ativo por vez, e duas Empresas diferentes podem cada uma ter o seu

### Story 9.2: Dono da Plataforma cria Empresas com Ambiente de Treinamento automático

As a Dono da Plataforma,
I want criar uma Empresa nova pela interface, com seu Ambiente de Treinamento já provisionado junto,
So that eu consiga colocar um cliente novo no ar rapidamente, e o time dele já nasça com um lugar seguro pra praticar.

**Acceptance Criteria:**

**Given** que esta é a primeira vez que o sistema roda (nenhum Dono da Plataforma existe ainda)
**When** um operador roda o comando CLI dedicado (mesmo padrão de Story 1.1/`cmd/seed-admin`)
**Then** o primeiro registro em `donos_plataforma` é criado — nunca por uma rota HTTP

**Given** um Dono da Plataforma autenticado (MFA já configurado — obrigatório para este papel, sem exceção)
**When** ele acessa a área "Empresas" e preenche Nome Fantasia, Razão Social, CNPJ e endereço completo, mais nome/e-mail do primeiro `adm`
**Then** uma nova Empresa é criada com esses dados, o primeiro `adm` dela é provisionado, e uma segunda Empresa-irmã "{Nome Fantasia} - Treinamento" é criada automaticamente na mesma ação, com `empresa_origem_id` apontando para a Empresa real e um pequeno conjunto de Produtos/Estoques de exemplo (nunca copiados da Empresa real)

**Given** um CNPJ que já existe em outra Empresa
**When** o Dono da Plataforma tenta cadastrar
**Then** o sistema recusa com 409 — CNPJ é único em toda a plataforma

**Given** o Dono da Plataforma gerenciando a lista de Empresas
**When** ele visualiza a tela
**Then** vê apenas metadado (Nome Fantasia, Razão Social, CNPJ, endereço, status, `adm` responsável, data de criação) — nenhum Catálogo, Estoque, Pedido ou Log de Acesso de nenhuma Empresa é acessível a partir dessa tela

**Given** uma Empresa que o Dono da Plataforma desativa
**When** qualquer conta vinculada a ela tenta entrar
**Then** o login é recusado, sem nenhum dado ser apagado

**Given** a identidade "Dono da Plataforma" e a de um `adm` de qualquer Empresa (inclusive Ferreira Costa)
**When** o sistema é usado no dia a dia
**Then** nunca são a mesma conta/credencial — login do Dono da Plataforma é uma rota própria, fora do prefixo `/e/{slug}` das Empresas

### Story 9.3: Convite nominal de acesso a uma Empresa

As a `adm`/`gestor` de uma Empresa,
I want convidar uma pessoa específica por e-mail para entrar na minha Empresa,
So that só quem eu de fato convidei consiga criar conta vinculada aos meus dados — nunca um link genérico que vaza pra qualquer um.

**Acceptance Criteria:**

**Given** um `adm`/`gestor` autenticado numa Empresa
**When** ele gera um convite informando um e-mail específico
**Then** um registro é criado em `convites_empresa` (empresa_id, e-mail, token, expiração), e um link é produzido para ser compartilhado com essa pessoa

**Given** um convite válido e não usado
**When** alguém acessa o link e preenche o autocadastro (Story 1.3) com o e-mail exatamente igual ao do convite
**Then** a conta nasce vinculada a essa Empresa, sempre com papel `usuario`, e o convite é marcado como usado atomicamente

**Given** o mesmo convite
**When** alguém tenta se cadastrar com um e-mail diferente do informado no convite
**Then** o cadastro é recusado, mesmo com o token correto

**Given** um convite expirado, já usado, ou revogado pelo emissor antes do uso
**When** alguém tenta usá-lo
**Then** o cadastro é recusado com uma mensagem clara do motivo

**Given** o login de uma conta já vinculada a uma Empresa
**When** o Usuário informa e-mail e senha
**Then** a Empresa é resolvida automaticamente pelo contexto de acesso (Story 9.1) — nunca por um seletor manual na tela de login, mesmo que o mesmo e-mail exista em outra Empresa

### Story 9.4: Migração da Ferreira Costa para o modelo multi-Empresa

As a operador do sistema (Claudio),
I want migrar os dados e contas já reais da Ferreira Costa para o novo modelo multi-Empresa,
So that o sistema em produção passe a operar sob o mesmo isolamento de qualquer outro cliente, sem perder nenhum histórico já registrado.

**Acceptance Criteria:**

**Given** o banco de produção com Produtos, Estoques, Movimentações, Pedidos, Categorias e contas de Usuário já existentes, sistema em uso ativo
**When** a migração é aplicada
**Then** ela roda em duas fases aditivas — `empresa_id` nasce nullable, backfill em lote atribui o id da Empresa "Ferreira Costa" a toda linha existente, só depois a coluna vira `NOT NULL` — sem exigir parada do sistema entre as duas fases

**Given** a migração interrompida no meio do backfill
**When** ela é executada novamente
**Then** retoma de onde parou sem duplicar nem perder vínculo de nenhuma linha (mesmo rigor de resumabilidade já exigido da importação de planilha, Story 3.3)

**Given** que a Empresa "Ferreira Costa" ainda não existe como linha em `empresas` antes desta migração
**When** o operador fornece CNPJ, Razão Social e endereço completo da Ferreira Costa
**Then** a linha é criada com esses dados, a conta `adm` hoje única e global passa a ser o `adm` dessa Empresa especificamente, e uma Empresa "Ferreira Costa - Treinamento" é criada como parte desta mesma migração (reaproveitando o mecanismo de Story 9.2) — não fica pendente de ação manual futura

**Given** a regra já estabelecida para qualquer corte de dados em produção (Architecture AD-15, PRD §9)
**When** esta migração é executada
**Then** ela é sempre disparada manualmente por uma pessoa — nunca de forma autônoma por um agente de IA, mesmo que o código da migração tenha sido escrito sob o processo de agentes do `bmad-loop`

## Epic 10: Enriquecimento do Cadastro de Produto e Configuração de Catálogo

Almoxarife cadastra Produtos mais completos e consistentes (nome mínimo, template de Nomenclatura obrigatório com fallback Genérico, Código do Fornecedor e EAN-13, Unidade de Medida e Embalagem, colunas explícitas na listagem); Adm mantém Categorias e Templates de Nomenclatura próprios por Empresa, sem depender de seed fixo.

### Story 10.1: Nome mínimo e Template de Nomenclatura obrigatório com fallback Genérico

As a Almoxarife,
I want que o cadastro de Produto exija um nome minimamente descritivo e sempre um Template de Nomenclatura selecionado,
So that o catálogo pare de receber nomes curtos demais ou fora do padrão que atrapalham busca e consistência.

**Acceptance Criteria:**

**Given** o cadastro ou edição de Produto
**When** o nome informado tem menos de 10 caracteres
**Then** o sistema rejeita com mensagem clara (máximo de 255 continua valendo)

**Given** a lista de templates (28 estruturais + "Genérico" `[NOME LIVRE]`, Architecture AD-34)
**When** o Almoxarife tenta cadastrar sem selecionar nenhum template
**Then** o sistema rejeita — não existe mais caminho de cadastro sem template selecionado

**Given** uma Categoria sem template estrutural específico (addendum §H, ~16 das 25 categorias)
**When** o Almoxarife cadastra um Produto dessa categoria
**Then** o template "Genérico" está sempre disponível e aceita qualquer texto não vazio, sem checagem de ordem/presença de token (AD-34)

**Given** um Produto com template (estrutural ou Genérico) já aplicado
**When** o Almoxarife edita o nome depois do cadastro
**Then** a edição revalida o nome contra o mesmo template — não é possível burlar a regra editando depois

**Given** Produtos já cadastrados antes desta story, com nome curto ou sem template
**When** o sistema sobe com essas regras novas
**Then** nenhuma varredura retroativa força reedição — as regras valem só para cadastro novo e a próxima edição de cada Produto (FR8 `[NOTE FOR PM]`)

### Story 10.2: Código de Produto automático e sequencial por Empresa

As a Almoxarife,
I want que o código do Produto seja gerado automaticamente pelo sistema,
So that eu não precise inventar/digitar um código e não haja risco de colisão entre cadastros.

**Acceptance Criteria:**

**Given** uma Empresa sem nenhum Produto cadastrado ainda
**When** o primeiro Produto é cadastrado
**Then** o código gerado é "000001" (zero-padding de 6 dígitos), a partir de uma linha em `contadores_produto` criada na mesma transação de provisionamento da Empresa (Story 9.2), nunca via lazy-init (Architecture AD-26)

**Given** dois cadastros de Produto concorrentes na mesma Empresa
**When** ambos são submetidos ao mesmo tempo
**Then** cada um recebe um código distinto e sequencial via `UPDATE contadores_produto ... RETURNING` atômico, na mesma transação do `INSERT` em `produtos` — nunca uma colisão

**Given** o campo código no formulário de cadastro
**When** a tela carrega
**Then** o campo não é mais editável pelo Almoxarife — só exibido, já preenchido, depois da criação

**Given** Produtos já cadastrados manualmente antes desta story, com códigos livres
**When** a sequência nova passa a valer
**Then** os códigos antigos permanecem intactos, convivendo com a nova sequência, sem renumeração retroativa

**Given** a sequência de código de duas Empresas diferentes
**When** comparadas
**Then** cada Empresa tem sua própria sequência independente, nunca compartilhada (consistente com AD-20)

### Story 10.3: Código do Fornecedor, EAN-13, Unidade de Medida e Embalagem

As a Almoxarife,
I want registrar o código do fornecedor, o código de barras EAN-13, a unidade de medida e a embalagem de um Produto,
So that o cadastro reflita informações reais de compra/logística já usadas no dia a dia.

**Acceptance Criteria:**

**Given** o formulário de cadastro/edição de Produto
**When** o Almoxarife informa Código do Fornecedor (texto livre) e/ou EAN-13
**Then** ambos são salvos como campos opcionais, sem nenhuma checagem de unicidade entre Produtos, sem relação funcional com o código interno de Produto (Story 10.2) (FR45, Architecture AD-32)

**Given** um EAN-13 informado
**When** o Almoxarife salva
**Then** o sistema valida o formato (13 dígitos + dígito verificador) e rejeita valores inválidos — campo vazio nunca é rejeitado

**Given** o cadastro de um Produto NOVO
**When** o Almoxarife não informa Unidade de Medida
**Then** o cadastro é rejeitado — Unidade de Medida é obrigatória só para Produto novo (Embalagem continua opcional)

**Given** a migração aditiva desta story
**When** ela roda contra Produtos já em produção sem Unidade de Medida
**Then** a coluna nasce nullable, um valor único (`[ASSUMPTION]` "un") é atribuído em lote a todo Produto legado sem essa informação, e só depois a obrigatoriedade passa a valer no cadastro/edição — nunca bloqueia o sistema de subir por dado antigo incompleto (AD-32)

**Given** Unidade de Medida e Embalagem persistidas no Produto
**When** consultadas via API de detalhe de Produto (FR7)
**Then** os dois valores retornam como campos próprios, sem transformação — exibição explícita na listagem do Catálogo é entregue pela Story 10.4

### Story 10.4: Colunas explícitas na listagem do Catálogo

As a qualquer Usuário,
I want ver código, categoria, estoque total e embalagem+unidade diretamente na listagem do Catálogo,
So that eu não precise abrir o detalhe de cada Produto pra saber o essencial.

**Acceptance Criteria:**

**Given** o Catálogo em visualização de grade
**When** um Usuário a acessa
**Then** cada card mostra explicitamente código, nome, categoria, estoque total (soma across Estoques) e embalagem+unidade

**Given** o Catálogo em visualização de tabela agrupada
**When** um Usuário a acessa
**Then** as mesmas colunas aparecem como colunas próprias da tabela, sem precisar expandir uma linha

**Given** um Produto sem Embalagem preenchida (campo opcional)
**When** ele aparece na listagem
**Then** a coluna de embalagem mostra um traço/vazio, nunca quebra o layout

**Given** uma linha da tabela agrupada que cobre mais de um Produto distinto (mesmo nome+dimensões, `CatalogoGrupo`)
**When** os Produtos do grupo divergem em código, categoria ou embalagem+unidade
**Then** a coluna correspondente mostra "Múltiplos" em vez de um valor específico; quando todos os Produtos do grupo concordam, mostra o valor comum — decisão do usuário (2026-09-20), sem alterar a chave de agrupamento existente (Story 4.3)

**Nota de dependência:** esta story consome `unidade_medida`/`embalagem` persistidos pela Story 10.3 — só pode rodar depois dela (ordem numérica já garante isso, mas o scheduler do `bmad-loop` não a impõe sozinho se a 10.3 travar).

### Story 10.5: CRUD de Categorias

As a `adm`+,
I want cadastrar, editar e excluir Categorias,
So that eu não dependa mais de uma lista fixa definida por seed para organizar o catálogo.

**Acceptance Criteria:**

**Given** um `adm`+ autenticado
**When** ele cadastra uma Categoria com código (até 8 caracteres) e nome/descrição (até 50 caracteres)
**Then** a Categoria é criada, escopada à Empresa dele (AD-20), com os limites de tamanho aplicados no banco, não só na validação de handler (Architecture AD-33)

**Given** uma Categoria referenciada por pelo menos um Produto
**When** o `adm`+ tenta excluí-la
**Then** a exclusão é bloqueada — mesmo princípio de FR13/AD-31 aplicado agora a Categorias

**Given** uma Categoria sem nenhum Produto referenciando
**When** o `adm`+ confirma a exclusão via `ConfirmDialog`
**Then** ela é removida

**Given** um Usuário com papel abaixo de `adm`
**When** ele tenta cadastrar/editar/excluir Categoria pela API
**Then** a resposta é 403

**Given** as ~25 Categorias seed já existentes por Empresa (Story 9.x, cópia por Empresa)
**When** esta story é implementada
**Then** elas continuam intactas e passam a ser editáveis via este CRUD — nenhuma migração de dado necessária, só a capacidade de editar o que já existe

### Story 10.6: CRUD de Templates de Nomenclatura

As a `adm`+,
I want cadastrar, editar e excluir Templates de Nomenclatura,
So that a Empresa consiga adaptar os padrões de nome às suas próprias categorias, sem depender só do seed fixo.

**Acceptance Criteria:**

**Given** um `adm`+ autenticado
**When** ele cadastra um Template com sua estrutura de tokens
**Then** o Template é criado, escopado à Empresa dele, seguindo a mesma cópia editável independente por Empresa já estabelecida para Categorias — assunção estendida explicitamente a Templates nesta rodada (FR40 revisado)

**Given** um Template já em uso por Produtos existentes
**When** o `adm`+ edita sua estrutura
**Then** Produtos já cadastrados sob o padrão antigo NÃO são reeditados retroativamente — a mudança só passa a valer a partir do próximo cadastro/edição de nome desses Produtos (Architecture AD-33)

**Given** um Template referenciado por pelo menos um Produto
**When** o `adm`+ tenta excluí-lo
**Then** a exclusão é bloqueada, mesmo princípio de FR13/FR48/AD-31/AD-33

**Given** o template "Genérico" (`[NOME LIVRE]`, Story 10.1/AD-34)
**When** o `adm`+ visualiza a lista de Templates
**Then** ele aparece listado como qualquer outro, mas a interface nunca permite excluí-lo enquanto for o único fallback disponível para as categorias sem template específico

**Given** um Usuário com papel abaixo de `adm`
**When** ele tenta cadastrar/editar/excluir Template pela API
**Then** a resposta é 403

## Epic 11: Estoque Preciso — Lote, Validade e Reserva de Saldo

Almoxarife lança e consome saldo por Lote com Data de Validade (FEFO automático, sem escolha manual); enviar um Pedido passa a travar de verdade o saldo reservado até a decisão do almoxarife, fechando a corrida entre pedidos concorrentes que hoje não existe.

### Story 11.1: Lançamento de saldo inicial com Lote e Data de Validade

As a Almoxarife,
I want lançar saldo de um Produto num Estoque sempre informando Lote e Data de Validade,
So that eu tenha rastreabilidade real de recebimentos, em vez de uma quantidade única sem histórico.

**Acceptance Criteria:**

**Given** uma tela dedicada de Lançamento de Saldo
**When** o Almoxarife informa Produto, Estoque, quantidade e Data de Validade (opcional — pode ficar em branco se desconhecida)
**Then** uma nova linha é criada em `lotes` (produto_id, estoque_id, quantidade, data_validade nullable, empresa_id) — nunca sobrescrevendo um Lote existente do mesmo par (Architecture AD-24)

**Given** um Produto que já tem Lotes ativos no mesmo Estoque
**When** um novo lançamento é feito
**Then** ele cria um Lote adicional, e a quantidade total exibida (FR6/FR7) passa a ser a soma de todos os Lotes ativos ali

**Given** um Lote com Data de Validade no passado
**When** ele aparece na consulta de Estoque (FR7)
**Then** é sinalizado visualmente como vencido — o saldo continua existindo e contável, só sinalizado, nunca bloqueado ou oculto (Out of Scope: alerta proativo por e-mail)

**Given** um Usuário com papel `usuario`
**When** ele tenta lançar saldo pela API
**Then** a resposta é 403 — restrito a `almoxarife`+

**Given** a quantidade lançada
**When** ela é zero ou negativa
**Then** o lançamento é rejeitado

### Story 11.2: Migração do saldo existente para Lote legado

As a Adm/Almoxarife responsável pela migração,
I want que todo saldo hoje existente vire automaticamente um Lote legado,
So that o sistema nunca suba incapaz de mostrar o saldo real já registrado.

**Acceptance Criteria:**

**Given** a tabela `produto_estoque`, com uma linha única por par Produto/Estoque
**When** a migração roda
**Then** cada linha vira uma linha em `lotes` com a mesma quantidade preservada, `data_validade = NULL` (marcado como "validade desconhecida" na tela, nunca inventada), e `empresa_id` copiado diretamente da linha de origem (já backfilled pela Story 9.4) — nunca recalculado (Architecture AD-24)

**Given** a migração concluída com sucesso
**When** o sistema opera normalmente a partir daí
**Then** `lotes` passa a ser a única fonte de saldo — `produto_estoque` é descontinuada

**Given** a migração já executada uma vez
**When** ela roda novamente
**Then** linhas já migradas não são duplicadas (idempotência, mesmo padrão de Story 2.3/3.7)

**Given** o corte de dados em produção
**When** o script é executado
**Then** é sempre disparado manualmente por uma pessoa, nunca por um agente autônomo (AD-15, PRD §9)

### Story 11.3: Reserva de saldo ao enviar Pedido

As a Almoxarife,
I want que o saldo dos itens de um Pedido fique travado assim que ele é enviado,
So that dois Pedidos concorrentes nunca disputem o mesmo saldo físico até uma decisão ser tomada.

**Acceptance Criteria:**

**Given** um Pedido sendo enviado (carrinho não vazio)
**When** o envio é confirmado
**Then** uma linha é criada em `reservas_pedido_item` para cada item, na mesma transação, após adquirir `SELECT ... FOR UPDATE` ordenado sobre as linhas de `lotes` afetadas — fecha a corrida entre duas reservas concorrentes do mesmo saldo (Architecture AD-10 estendida, AD-25)

**Given** o saldo disponível de um Produto num Estoque
**When** ele é consultado (Catálogo/Estoque, FR6/FR7)
**Then** é sempre calculado como soma de `lotes.quantidade` menos soma de `reservas_pedido_item.quantidade` ativas — nunca uma coluna materializada

**Given** um item reservado
**When** um Usuário clica na quantidade reservada
**Then** vê para qual(is) Pedido(s)/solicitante(s) ela está associada

**Given** um Pedido rejeitado
**When** a decisão é registrada
**Then** a reserva inteira é liberada automaticamente

**Given** uma aprovação parcial de Pedido
**When** a decisão é registrada
**Then** só a parte não aprovada volta a ficar disponível

**Given** o rótulo "Reservar"/"Carrinho de reserva" já existente (Epic 7)
**When** esta story é implementada
**Then** a interface passa a distinguir claramente as duas coisas (ex. "Adicionar ao carrinho" vs. "Saldo reservado") para não sugerir que adicionar ao carrinho já trava saldo contra outros usuários (FR21 `[NOTE FOR PM]`)

**Given** um Pedido pendente há muito tempo sem decisão
**When** ele permanece nesse estado
**Then** o saldo continua reservado indefinidamente — sem expiração automática nesta versão (Deferred, AD-25)

### Story 11.4: Baixa e Transferência consomem Lote automaticamente e respeitam saldo reservado

As a Almoxarife,
I want que Baixa e Transferência debitem sempre o Lote mais próximo do vencimento e nunca toquem saldo já reservado por um Pedido,
So that o estoque físico e a reserva de outros Pedidos nunca fiquem inconsistentes entre si.

**Acceptance Criteria:**

**Given** uma Baixa ou Transferência de quantidade X
**When** ela é confirmada
**Then** o débito é feito automaticamente do(s) Lote(s) do par Produto/Estoque com `data_validade` mais próxima primeiro (`ORDER BY data_validade NULLS LAST, criado_em ASC`) — Lote sem validade conhecida só é consumido depois de esgotados os Lotes com validade real (FEFO, AD-24); nenhuma tela permite escolha manual de Lote

**Given** o saldo DISPONÍVEL (descontada a reserva ativa, Story 11.3)
**When** uma Baixa/Transferência é solicitada com quantidade maior que o disponível, mesmo com saldo físico total suficiente
**Then** é rejeitada — nunca debita saldo que está reservado por outro Pedido

**Given** uma Transferência entre Estoques
**When** ela move um Lote inteiro ou parte de um
**Then** a Data de Validade original é preservada no Estoque destino — nunca cria um Lote novo com validade diferente da origem

**Given** uma transação de débito que toca múltiplas linhas de Lote
**When** os locks são adquiridos
**Then** segue a ordem canônica `(produto_id, estoque_id, lote_id)` ascendente antes de qualquer escrita (Architecture AD-10)

**Given** toda escrita em `lotes.quantidade`
**When** ela ocorre
**Then** uma `MOVIMENTACOES` correspondente é sempre gerada na mesma transação, sem exceção (AD-10)

**Given** quantidade zero ou negativa, ou origem=destino numa Transferência
**When** solicitado
**Then** rejeitado (comportamento já existente, preservado)

### Story 11.5: Aprovação de Pedido revalida contra reserva e debita Lote

As a Almoxarife,
I want que aprovar um Pedido revalide contra o que está de fato reservado e debite o Lote certo,
So that a aprovação nunca falhe por concorrência normal — só por um bug real de reserva.

**Acceptance Criteria:**

**Given** um Pedido pendente com itens reservados (Story 11.3)
**When** o Almoxarife aprova
**Then** cada item é revalidado contra a própria reserva daquele Pedido (não mais contra o saldo livre) — falha aqui só deveria acontecer por bug de reserva, nunca por concorrência normal entre Pedidos

**Given** um item aprovado
**When** o débito acontece
**Then** ele consome do(s) Lote(s) do Estoque de origem seguindo o mesmo critério FEFO da Story 11.4, e a reserva desse item é liberada atomicamente na mesma transação

**Given** uma falha de revalidação em um item específico
**When** a aprovação é processada
**Then** nunca gera sucesso parcial silencioso — a lista exata de itens com problema é devolvida ao Almoxarife para decidir (aprovação parcial explícita ou rejeitar/ajustar), mesmo comportamento já existente (Epic 7), agora rodando contra a base de reserva

**Given** o papel de quem aprova
**When** a aprovação é submetida
**Then** é revalidado no momento exato da submissão (comportamento já existente, preservado)

**Given** débito e liberação de reserva
**When** a aprovação é confirmada
**Then** ambos e a Movimentação correspondente são atômicos na mesma transação

### Story 11.6: Estoque e quantidade inicial saem do Cadastro de Produto

As a Almoxarife,
I want que o cadastro de um Produto não exija mais informar Estoque/quantidade inicial,
So that eu possa criar o Produto primeiro e lançar o saldo separadamente, pela tela dedicada de Lote.

**Acceptance Criteria:**

**Given** a tela de Cadastro de Produto já existente (Story 3.1, com os campos adicionais de Story 10.1–10.3 se essa epic já tiver rodado)
**When** o Almoxarife cadastra um Produto novo
**Then** os campos de Estoque destino e quantidade inicial não aparecem mais no formulário — o Produto é criado sem nenhuma linha de saldo em nenhum Estoque

**Given** um Produto recém-cadastrado sem saldo em nenhum Estoque
**When** ele aparece no Catálogo (FR6)
**Then** aparece normalmente com quantidade total 0, sem nenhum estado de erro ou visibilidade especial (Architecture AD-29) — se Epic 10/Story 10.4 já tiver rodado, essa é a primeira vez que esse estado realmente ocorre; senão, vale para a listagem já existente

**Given** a tela de Lançamento de Saldo (Story 11.1) já existente
**When** o Almoxarife precisa dar entrada em estoque de um Produto recém-cadastrado
**Then** esse é o único caminho disponível — nenhum outro formulário lança saldo inicial

**Given** Produtos cadastrados antes desta story, com saldo já vinculado no cadastro
**When** esta mudança entra em vigor
**Then** nada muda para eles — a alteração afeta só o formulário de cadastro NOVO daqui pra frente

## Epic 12: Estrutura Organizacional — Filiais e Centro de Custo

Adm estrutura a organização em Filiais (com Depósitos/Estoques vinculados) e cadastra Centro de Custo/Destino de Obra, referenciável no envio de Pedido; Ambiente de Treinamento ganha fotos de exemplo nos Produtos.

### Story 12.1: Cadastro de Filiais e vínculo de Estoque

As a `adm`+,
I want cadastrar Filiais e vincular cada Estoque a uma delas,
So that a organização física da empresa fique refletida no sistema, com Depósitos (Estoques) agrupados por Filial.

**Acceptance Criteria:**

**Given** um `adm`+ autenticado
**When** ele cadastra uma Filial com um nome
**Then** ela é criada, escopada à Empresa dele (Architecture AD-27)

**Given** o cadastro/edição de um Estoque NOVO (Story 2.1) a partir desta story em diante
**When** ele é criado
**Then** exige uma Filial vinculada — Depósito continua sendo o mesmo conceito de Estoque já existente, só ganhando Filial como atributo/pai, não um terceiro nível de hierarquia

**Given** o nome de um Estoque
**When** dois Estoques de Filiais diferentes da mesma Empresa têm o mesmo nome
**Then** ambos são permitidos — a unicidade passa a ser `(filial_id, nome_normalizado)`, não mais só `(empresa_id, nome_normalizado)` (FR12 revisado)

**Given** uma Empresa nova sendo provisionada (Story 9.2)
**When** o provisionamento roda
**Then** uma Filial padrão (nome = Nome Fantasia da Empresa) é criada automaticamente na mesma transação — nenhum Estoque nasce sem Filial, nenhum passo extra de onboarding necessário

**Given** um Usuário com papel abaixo de `adm`
**When** ele tenta cadastrar Filial pela API
**Then** a resposta é 403

### Story 12.2: Migração dos Estoques legados para Filial padrão

As a Adm/Almoxarife responsável pela migração,
I want que todo Estoque já existente seja vinculado automaticamente a uma Filial,
So that o sistema nunca suba com Estoque órfão de Filial.

**Acceptance Criteria:**

**Given** todo Estoque hoje existente numa Empresa (ex. Ferreira Costa, ~11 estoques reais)
**When** a migração roda
**Then** uma Filial padrão única é criada para a Empresa (se ainda não existir) e todo Estoque atual é atribuído a ela automaticamente — migração aditiva (`filial_id` nasce nullable, backfill, só depois `NOT NULL`), mesmo molde de AD-20 (Architecture AD-27)

**Given** a migração concluída
**When** o `adm` acessa a lista de Estoques
**Then** todos aparecem vinculados a essa Filial padrão, prontos para reorganização manual futura (fora do escopo desta migração)

**Given** a migração já executada uma vez
**When** ela roda novamente
**Then** Estoques já vinculados não são reprocessados (idempotência)

**Given** o corte de dados em produção
**When** o script é executado
**Then** é sempre disparado manualmente por uma pessoa, nunca por um agente autônomo (AD-15, PRD §9)

### Story 12.3: Cadastro de Centro de Custo e Destino de Obra

As a `adm`+,
I want cadastrar Centros de Custo e Destinos de Obra como entidades próprias,
So that o envio de Pedido possa referenciar uma lista padronizada, em vez de depender só de texto livre.

**Acceptance Criteria:**

**Given** um `adm`+ autenticado
**When** ele cadastra um Centro de Custo/Destino de Obra com um nome (ex. "Estoque do Cabo de Santo Agostinho")
**Then** ele é criado, escopado à Empresa dele, seguindo a mesma cópia por Empresa já estabelecida para Categorias/Templates/Filiais (Architecture AD-28)

**Given** o envio de um Pedido (Epic 7)
**When** o solicitante preenche o campo obra/centro de custo
**Then** o campo texto livre já existente continua obrigatório e inalterado; um novo campo opcional `centro_custo_id` referencia a lista estruturada quando o solicitante escolhe um item cadastrado — os dois coexistem, sem migração retroativa de Pedido histórico

**Given** um `centro_custo_id` recebido do cliente
**When** o Pedido é enviado
**Then** é sempre revalidado contra a Empresa do contexto antes de aceitar — nunca aceito de outra Empresa (extensão de AD-20)

**Given** um Usuário com papel abaixo de `adm`
**When** ele tenta cadastrar Centro de Custo/Destino de Obra pela API
**Then** a resposta é 403

### Story 12.4: Fotos de exemplo no Ambiente de Treinamento

As a Adm provisionando uma Empresa nova,
I want que o Ambiente de Treinamento venha com fotos de exemplo nos Produtos semeados,
So that o catálogo de treinamento passe a sensação de um catálogo real, não uma lista vazia de nomes.

**Acceptance Criteria:**

**Given** o provisionamento de um Ambiente de Treinamento (Story 9.2)
**When** os Produtos de exemplo são semeados
**Then** cada um recebe pelo menos uma foto de exemplo, enviada pelas rotas normais de upload (Story 3.5), reaproveitando o armazenamento versionado em disco já existente — sem storage novo (Architecture AD-11)

**Given** o mecanismo de seed
**When** ele é executado
**Then** é um script one-off disparado manualmente por uma pessoa (mesmo princípio de AD-15), nunca automático nem por agente autônomo

**Given** fotos já semeadas num Ambiente de Treinamento existente antes desta story
**When** ela é implementada
**Then** não força reseed automático — passa a valer para Ambientes de Treinamento provisionados a partir de agora

**Given** a quantidade/conteúdo exato de Produtos e fotos semeadas
**When** esta story é implementada
**Then** usa um conjunto pequeno definido em conjunto com o Adm — decisão de conteúdo/UX, não parte fixa desta story

## Epic 13: Edição de Produto

Almoxarife corrige um Produto já cadastrado sem precisar cadastrar outro. Surgiu dos testes reais de treinamento (2026-09-23): a única rota de edição (`/renomear`, só nome) nunca teve tela, e o PRD (FR8/FR9) prevê edição com revalidação do nome contra o template.

### Story 13.1: Editar um Produto já cadastrado

As a Almoxarife,
I want editar os dados de um Produto que já cadastrei,
So that eu corrija erros de digitação e informações incompletas sem criar um Produto duplicado.

**Acceptance Criteria:**

**Given** o detalhe de um Produto
**When** um `almoxarife`+ aciona "Editar"
**Then** abre um formulário pré-preenchido com nome, template de Nomenclatura, categoria, unidade de medida, embalagem, código do fornecedor, EAN-13, dimensões e observações; o Código (gerado pelo sistema, Story 10.2) aparece somente-leitura

**Given** o nome alterado
**When** o Almoxarife salva
**Then** ele é revalidado contra o template escolhido (mínimo 10 caracteres, formato do template, "Genérico" aceita qualquer nome) e a mensagem de erro mostra o formato esperado

**Given** campos inválidos (EAN-13 com dígito verificador errado, dimensão com valor sem unidade, unidade fora da lista)
**When** o Almoxarife salva
**Then** o servidor rejeita apontando o campo e nada é gravado

**Given** um Produto legado sem template (cadastrado antes da Story 10.1)
**When** ele é editado
**Then** a edição não exige escolher template (regra já existente do renomear), mas passa a revalidar o nome quando um template é escolhido

**Given** um Usuário com papel `usuario`
**When** ele tenta editar pela API ou pela tela
**Then** a API responde 403 e o botão "Editar" nem aparece

**Given** um Produto de outra Empresa, ou inexistente
**When** a edição é tentada
**Then** a resposta é 404, nunca revelando a existência (AD-20)

**Given** uma edição bem-sucedida
**When** o servidor grava
**Then** o Código, o saldo (Lotes), as reservas e o histórico de Movimentações permanecem intactos, e um evento `produtos` (`updated`) é publicado para as outras sessões abertas

**Nota:** esta story não edita saldo (isso é Lançamento de Saldo, Story 11.1) nem o Código do Produto.

## Epic 14: Dupla autenticação por Empresa

Cada Empresa decide se exige dupla autenticação (MFA) de seus `gestor`/`adm`. Hoje a exigência é fixa e vale para toda Empresa; a pedido dos sócios e de clientes ela passa a ser uma escolha da Empresa (padrão "Não", inclusive para as já existentes), com a pergunta no cadastro, alteração posterior pelo `adm` e recuperação para celular perdido. A exigência continua valendo só para `gestor`/`adm` (decisão de 2026-09-24).

### Story 14.1: A Empresa passa a definir se exige MFA (gate condicional)

As a `adm` de uma Empresa,
I want que a obrigatoriedade de MFA para gestor e adm dependa de uma configuração da Empresa,
So that a minha Empresa só exija a dupla autenticação se quiser.

**Acceptance Criteria:**

**Given** a migration desta story
**When** ela roda contra o banco com Empresas já existentes (Ferreira Costa e o Treinamento)
**Then** `empresas.mfa_obrigatorio` nasce `NOT NULL DEFAULT false` e todas as Empresas existentes ficam `false` — ninguém é bloqueado no deploy (Architecture AD-35)

**Given** uma Empresa com `mfa_obrigatorio = false`
**When** um `gestor` ou `adm`, autenticado por senha e sem MFA, acessa uma rota restrita a esses papéis
**Then** a requisição é atendida normalmente — o `403 MFA_SETUP_REQUIRED` não é emitido

**Given** uma Empresa com `mfa_obrigatorio = true`
**When** um `gestor` ou `adm`, autenticado por senha e sem MFA, acessa uma rota restrita a esses papéis
**Then** a resposta é `403 MFA_SETUP_REQUIRED` (comportamento de hoje), e `usuario`/`almoxarife` nunca são bloqueados por esta regra

**Given** o flag da Empresa alterado no banco
**When** o mesmo usuário faz a próxima requisição
**Then** o novo valor já vale, sem novo login (a Empresa é lida por requisição, sem cache — AD-19/AD-5)

**Given** uma conta com `mfa_habilitado = true`
**When** ela faz login por senha, em Empresa que exige ou não
**Then** o código TOTP continua sendo pedido — o segundo fator ligado pela conta nunca é ignorado

**Given** uma sessão originada por SSO
**When** ela acessa rotas restritas
**Then** o gate de MFA nunca dispara, com ou sem a exigência da Empresa

**Given** `GET /api/auth/me`
**When** o frontend o consulta
**Then** a resposta traz `empresa.mfaObrigatorio`; `App.tsx` só bloqueia a navegação (liberando apenas Configurações → Segurança e o logout) quando papel >= `gestor`, origem senha, sem MFA **e** a Empresa exige; e Configurações → Segurança mostra "obrigatório" nesse caso e "opcional" nos demais

**Given** o Dono da Plataforma
**When** ele faz login
**Then** o MFA dele continua obrigatório, sem depender de nenhum flag de Empresa

### Story 14.2: Cadastro da Empresa pergunta se exige MFA; Treinamento herda

As a Dono da Plataforma,
I want responder, ao cadastrar uma Empresa, se ela exige dupla autenticação,
So that a Empresa já nasça com a política que o cliente escolheu.

**Acceptance Criteria:**

**Given** a tela de cadastro de Empresa (`EmpresasPage`)
**When** ela abre
**Then** exibe a pergunta "Esta Empresa exige dupla autenticação?" com o padrão **Não** pré-selecionado, e a escolha vai no corpo de `POST /api/plataforma/empresas` (`mfa_obrigatorio`)

**Given** o cadastro enviado sem o campo
**When** o servidor processa
**Then** assume `false` (mesmo padrão), nunca falha nem exige o campo

**Given** uma Empresa criada com "Sim"
**When** o provisionamento termina
**Then** `empresas.mfa_obrigatorio = true` na Empresa real **e** na Empresa de Treinamento criada junto (herda a escolha na criação; depois as duas são independentes, Architecture AD-35)

**Given** a listagem de Empresas do Dono da Plataforma
**When** ela é exibida
**Then** mostra a escolha de cada Empresa (só metadado administrativo, sem acesso a conteúdo operacional — FR-41)

**Given** um usuário que não é Dono da Plataforma
**When** tenta criar Empresa ou enviar `mfa_obrigatorio`
**Then** a resposta é 403, como já é hoje

### Story 14.3: O adm altera a exigência de MFA, com aviso e auditoria

As a `adm` de uma Empresa,
I want ligar ou desligar a exigência de MFA da minha Empresa,
So that a política acompanhe a decisão da empresa sem depender do Dono da Plataforma.

**Acceptance Criteria:**

**Given** Configurações → Segurança, aberta por um `adm`
**When** ele vê a seção "Dupla autenticação da Empresa"
**Then** encontra a escolha atual e o controle para alterá-la; papéis abaixo de `adm` não veem o controle e a API responde 403

**Given** o `adm` prestes a passar de "não" para "sim"
**When** ele aciona a mudança
**Then** a tela avisa quantos `gestor`/`adm` da Empresa ainda não têm MFA e que eles ficarão sem acesso até cadastrar, e só aplica depois da confirmação

**Given** a exigência ligada
**When** um `gestor`/`adm` sem MFA faz o próximo acesso
**Then** é obrigado a cadastrar o MFA e fica sem acesso ao sistema até concluir (comportamento da Story 14.1)

**Given** o `adm` passando de "sim" para "não"
**When** a mudança é aplicada
**Then** nenhum MFA já configurado é desligado — só a obrigação é removida

**Given** qualquer alteração da escolha
**When** ela é gravada
**Then** uma linha é inserida em `auditoria_seguranca` (`acao=exigencia_alterada`, ator, valor anterior e novo, momento), tabela append-only escopada por `empresa_id` (Architecture AD-35), sem rota de edição ou exclusão; o `adm` consulta o histórico na própria seção

**Given** a escolha já está no valor pedido (ex. reenvio da mesma requisição)
**When** o `adm` grava o mesmo valor de novo
**Then** nada muda e nenhuma linha de auditoria é criada — a operação é idempotente

### Story 14.4: Recuperação — reset de MFA por adm e desligamento pela própria conta

As a membro de uma Empresa que perdeu o celular,
I want recuperar o acesso sem depender de suporte da plataforma,
So that o MFA opcional não vire um bloqueio permanente.

**Acceptance Criteria:**

**Given** a Gestão de Contas, aberta por um `adm`
**When** ele aciona "Resetar MFA" numa conta de rank menor que o dele (AD-8)
**Then** `mfa_habilitado`, `mfa_secret` e `mfa_ultimo_passo_usado` são zerados, as sessões da conta são revogadas e uma linha `acao=mfa_resetado` (ator e alvo) é gravada em `auditoria_seguranca`

**Given** a conta resetada, em Empresa que exige e de papel `gestor`
**When** ela entra de novo
**Then** é obrigada a cadastrar um MFA novo (Story 14.1); em Empresa que não exige, entra normalmente

**Given** um `gestor` ou `usuario`
**When** tenta resetar o MFA de alguém, ou um `adm` tenta resetar o de outro `adm` ou de conta de outra Empresa
**Then** a resposta é 403 (ou 404 para outra Empresa, sem revelar existência — AD-20)

**Given** qualquer conta com MFA ligado
**When** ela aciona "Desligar meu MFA" informando a senha atual e o código TOTP vigente
**Then** o MFA é desligado e `acao=mfa_desligado` é gravada; senha ou código errado não desliga nada e conta para o bloqueio por tentativas (FR-36)

**Given** um `gestor`/`adm` numa Empresa que exige MFA
**When** ele tenta desligar o próprio MFA
**Then** a resposta é 409 explicando que a Empresa exige MFA para o papel dele — nada é alterado

**Given** o fluxo de redefinição de senha (FR-32)
**When** uma conta com MFA redefine a senha por e-mail
**Then** o MFA continua ligado e o código continua sendo pedido no login seguinte — a redefinição não desliga nem contorna o segundo fator

## Epic 15: Acesso sem a Empresa na URL

Hoje só se entra no stockflow pelo endereço da Empresa (`/e/{empresa}`); a raiz do domínio mostra apenas uma página explicativa. Como cada conta pertence a uma só Empresa real, o e-mail basta para descobrir a Empresa: a raiz passa a ter login por e-mail e senha, e o domínio de um cliente só abre direto a Empresa dele. A premissa é o e-mail único entre as Empresas reais, garantido no banco (Architecture AD-36). O Ambiente de Treinamento continua podendo repetir o e-mail da sua Empresa real; nesse caso o login pergunta "Ambiente real ou Treinamento?".

### Story 15.1: E-mail único entre as Empresas reais

As a Dono da Plataforma,
I want que um e-mail tenha no máximo uma conta somando todas as Empresas reais,
So that o e-mail identifique sozinho a Empresa de cada pessoa.

**Acceptance Criteria:**

**Given** a migration desta story
**When** ela roda contra um banco sem e-mail repetido entre Empresas reais
**Then** `usuarios.empresa_raiz_id` é criada e preenchida (a Empresa real da conta: a própria Empresa, ou a de origem para contas de Treinamento), fica `NOT NULL`, e passa a valer a restrição `EXCLUDE USING gist (lower(email) WITH =, empresa_raiz_id WITH <>)` (Architecture AD-36)

**Given** um banco com o mesmo e-mail em duas Empresas reais
**When** a migration roda
**Then** ela falha antes de criar a restrição, com mensagem que lista os e-mails repetidos — nada fica pela metade

**Given** qualquer `INSERT INTO usuarios` (cadastro por convite, primeiro `adm` de Empresa nova, `adm` do Treinamento, CLIs de seed e migração)
**When** a conta é criada
**Then** `empresa_raiz_id` é preenchida pelo trigger `BEFORE INSERT`, sem o código de inserção precisar informá-la

**Given** um convite ou autocadastro para um e-mail que já tem conta em outra Empresa real (ou no Treinamento de outra Empresa real)
**When** o convite é criado ou o cadastro é enviado
**Then** a resposta é 409 com a mesma mensagem de e-mail em uso de hoje, e nada é gravado

**Given** o Dono da Plataforma criando uma Empresa cujo primeiro `adm` tem e-mail já usado em outra Empresa real
**When** envia o cadastro
**Then** a resposta é 409 indicando que o e-mail do administrador já está em uso, e nem a Empresa nem o Treinamento são criados

**Given** o provisionamento de uma Empresa nova
**When** o `adm` do Treinamento é criado com o mesmo e-mail do `adm` real
**Then** a criação é aceita — real e Treinamento têm a mesma Empresa raiz

**Given** um convite no Ambiente de Treinamento para o e-mail de uma conta da sua Empresa real
**When** a pessoa se cadastra
**Then** a conta de Treinamento é criada normalmente

### Story 15.2: Login na raiz do domínio pela conta

As a pessoa com conta no stockflow,
I want entrar pela raiz do domínio só com e-mail e senha,
So that eu não precise saber o endereço da minha Empresa.

**Acceptance Criteria:**

**Given** a raiz do domínio (qualquer caminho fora de `/e/{empresa}` e de `/plataforma`)
**When** ela abre
**Then** mostra a tela de login com e-mail, senha e "Esqueci a senha", no lugar da página explicativa de hoje (`lib/entrada.ts`, app `sem-empresa`)

**Given** um e-mail com uma única conta ativa e a senha correta
**When** a pessoa envia o login
**Then** `POST /api/auth/entrar` devolve o slug da Empresa e grava o refresh token no cookie com `Path=/e/{slug}/api/auth`; o frontend recarrega em `/e/{slug}/` e a pessoa já está autenticada, sem digitar de novo (Architecture AD-36)

**Given** um e-mail com conta na Empresa real e no Treinamento, e a senha correta nas duas
**When** a pessoa envia o login
**Then** a tela pergunta "Ambiente real ou Treinamento?" com o nome de cada Empresa; escolhida uma, `POST /api/auth/entrar/escolha` conclui o login naquela Empresa; o token da escolha é de uso único, vence em 5 minutos e só aceita as Empresas das contas conferidas

**Given** um e-mail com conta na Empresa real e no Treinamento, e a senha correta só numa delas
**When** a pessoa envia o login
**Then** ela entra direto na Empresa cuja senha conferiu, sem pergunta

**Given** um e-mail sem conta, ou uma senha errada
**When** a pessoa envia o login
**Then** a resposta é o mesmo `401 INVALID_CREDENTIALS` do login de hoje nos dois casos, com o bcrypt rodando também quando não há conta — a raiz nunca revela se o e-mail existe nem em qual Empresa

**Given** senhas erradas repetidas
**When** o limite de FR-36 é atingido
**Then** a conta (cada conta daquele e-mail) fica bloqueada exatamente como no login pelo endereço da Empresa, e a raiz responde `429 ACCOUNT_LOCKED`

**Given** uma conta desativada, com e-mail não confirmado, ou de Empresa desativada
**When** ela tenta entrar pela raiz
**Then** recebe a mesma recusa que receberia no login pelo endereço da Empresa — a raiz só descobre a Empresa, não abre exceção (a checagem é a do `services.Login` de hoje, chamado por Empresa)

**Given** uma conta com MFA ligado
**When** a senha confere na raiz
**Then** a resposta traz o slug e o `mfaToken`, e a pessoa digita o código na etapa de MFA da Empresa, sob `/e/{slug}`, como hoje

**Given** os endereços `/e/{empresa}`, os convites e o login SSO
**When** esta story entra
**Then** continuam funcionando exatamente como antes

### Story 15.3: "Esqueci a senha" na raiz do domínio

As a pessoa que esqueceu a senha,
I want pedir a redefinição só com o e-mail, pela raiz do domínio,
So that eu recupere o acesso sem saber o endereço da minha Empresa.

**Acceptance Criteria:**

**Given** a tela de login da raiz
**When** a pessoa aciona "Esqueci a senha" e informa o e-mail
**Then** `POST /api/auth/esqueci-senha` responde sempre `202` com a mesma mensagem, exista ou não a conta

**Given** um e-mail com uma conta ativa
**When** a redefinição é pedida pela raiz
**Then** chega o e-mail de redefinição de hoje (FR-32), com o link já no endereço da Empresa da conta (`/e/{slug}/redefinir-senha?...`)

**Given** um e-mail com conta na Empresa real e no Treinamento
**When** a redefinição é pedida pela raiz
**Then** chega um e-mail por conta, cada um com o nome e o endereço da sua Empresa

**Given** o mesmo e-mail pedindo redefinição várias vezes
**When** os pedidos chegam pela raiz
**Then** valem os mesmos limites e prazos do pedido feito pelo endereço da Empresa (é o mesmo `SolicitarRedefinicaoSenha`, chamado por Empresa)

### Story 15.4: Domínio de um cliente só abre direto a Empresa

As a colaborador da Ferreira Costa,
I want que `suprimentos.fcxlabs.com` abra direto o login da minha Empresa,
So that eu não precise digitar nada além do meu acesso.

**Acceptance Criteria:**

**Given** o backend com `EMPRESA_PADRAO=ferreira-costa` e essa Empresa ativa
**When** alguém abre a raiz do domínio
**Then** `GET /api/entrada` devolve `{empresaPadrao: "ferreira-costa"}` e o frontend faz `location.replace('/e/ferreira-costa/')` — cai no login da Empresa, com SSO se configurado

**Given** o backend sem `EMPRESA_PADRAO`, ou com um slug que não existe ou está desativado
**When** alguém abre a raiz
**Then** `GET /api/entrada` devolve `{empresaPadrao: null}` e aparece o login pela conta da Story 15.2 — nunca um erro nem um redirecionamento para Empresa inexistente

**Given** `GET /api/entrada`
**When** é chamado sem sessão
**Then** responde só o slug padrão (ou `null`), sem nenhum outro dado da Empresa

**Given** o `installer/cliente-aws/docker-compose.yml` (a lista de variáveis do container `api` é explícita e o deploy copia esse arquivo para o servidor)
**When** esta story entra
**Then** a lista ganha `EMPRESA_PADRAO=${EMPRESA_PADRAO}` — vazia no `.env` equivale a não configurada

**Given** o `.env.example` e a documentação de implantação
**When** esta story entra
**Then** `EMPRESA_PADRAO` aparece documentada como opcional, só para servidores de um cliente só, e não é definida em `stockflow.fbtechia.com`

**Operator action (depois do deploy):** acrescentar `EMPRESA_PADRAO=ferreira-costa` ao `.env` do servidor de `suprimentos.fcxlabs.com` (`/opt/apps/stockflow/cliente-aws/.env`) e reiniciar o container da API. Enquanto isso não for feito, a raiz desse domínio mostra o login pela conta (Story 15.2), que também funciona.

## Epic 16: Inativar Produto, EAN único e histórico do Produto

Do treinamento da Ferreira Costa (Karla, 2026-09-25): não havia como tirar de uso um Produto sem apagá-lo; a edição aceitou o mesmo EAN-13 de outro Produto ativo (o mesmo item com dois códigos internos); e a troca da descrição inteira não deixava rastro. Inativar exige saldo zero — o material é transferido ou baixado antes. O inativo some das operações e continua no histórico (Architecture AD-37); o EAN passa a ser único entre ativos, garantido no service (AD-32 revisada).

### Story 16.1: Inativar e reativar um Produto

As a gestor ou adm,
I want inativar um Produto que não usamos mais e reativá-lo se precisar,
So that o Catálogo mostre só o que está em uso, sem perder o histórico.

**Acceptance Criteria:**

**Given** a migration desta story
**When** ela roda
**Then** `produtos` ganha `inativado_em` e `inativado_por` (nulos — todo Produto existente continua ativo) e existe a tabela `produto_historico` (`acao` em `nome_alterado | inativado | reativado`, escopada por Empresa, append-only) (Architecture AD-37)

**Given** um `gestor` ou `adm` no detalhe de um Produto ativo sem saldo e sem reserva
**When** aciona "Inativar produto", informa (ou não) o motivo e confirma
**Then** `POST /api/produtos/{id}/inativacao` grava a inativação e uma linha `inativado` no histórico com o motivo, e o detalhe passa a mostrar a marca "Inativo" e o botão "Reativar"

**Given** um Produto com saldo em algum Estoque ou com reserva de Pedido pendente
**When** o gestor tenta inativar
**Then** a resposta é 409 `PRODUTO_COM_SALDO` e a tela diz em quais Estoques há saldo (ou que há reserva), orientando a transferir ou dar baixa antes — nada é gravado

**Given** um `almoxarife` ou `usuario`
**When** tenta inativar ou reativar (tela ou API)
**Then** o botão não aparece e a API responde 403

**Given** um Produto que está no Carrinho de alguém
**When** é inativado
**Then** o item sai do Carrinho e a pessoa vê o aviso de item removido, como já acontece com Produto que some (FR-21)

**Given** uma inativação e, ao mesmo tempo, um lançamento de saldo ou envio de Pedido do mesmo Produto
**When** as duas chegam juntas
**Then** uma espera a outra (`FOR UPDATE` x `FOR SHARE`, AD-37): ou a inativação é recusada por saldo/reserva, ou o lançamento/pedido é recusado por Produto inativo — nunca fica um Produto inativo com saldo

**Given** um Produto inativo
**When** o gestor aciona "Reativar"
**Then** `POST /api/produtos/{id}/reativacao` o devolve às operações e grava `reativado` no histórico; se o EAN-13 dele já estiver em outro Produto ativo, a resposta é 409 `EAN_EM_USO` dizendo qual (a regra da Story 16.4)

### Story 16.2: Produto inativo some das operações; filtro "Inativos"

As a almoxarife,
I want que Produtos inativos não apareçam onde eu opero o estoque,
So that ninguém reserve, lance saldo ou importe algo que não usamos mais.

**Acceptance Criteria:**

**Given** um Produto inativo
**When** alguém usa Catálogo, busca, leitura por QR Code/código de barras ou a exportação do Catálogo
**Then** ele não aparece (a leitura por código responde como código não encontrado)

**Given** um Produto inativo
**When** alguém tenta lançar saldo inicial, adicionar ao Carrinho ou enviar Pedido com ele (inclusive por chamada direta à API)
**Then** a resposta é 409 dizendo que o Produto está inativo

**Given** uma planilha de importação com o código de um Produto inativo
**When** a linha é processada
**Then** ela é marcada como erro ("Produto inativo — reative antes de importar") sem interromper as demais linhas

**Given** a detecção de duplicatas e de inconsistências (Normalização)
**When** roda
**Then** Produtos inativos ficam de fora

**Given** Movimentações, Pedidos, recibos em PDF e o detalhe do Produto
**When** mostram um Produto inativo
**Then** ele continua aparecendo, com a marca "Inativo"

**Given** um `gestor` ou `adm` no Catálogo
**When** liga o filtro "Inativos"
**Then** vê só os Produtos inativos e abre o detalhe de cada um para reativar; para `almoxarife`/`usuario` o filtro não aparece e `?inativos=1` é ignorado

### Story 16.3: Histórico de alterações do Produto

As a gestor,
I want ver quem trocou o nome de um Produto e quem o inativou ou reativou,
So that uma alteração indevida possa ser identificada e corrigida.

**Acceptance Criteria:**

**Given** a edição de Produto (`PUT /api/produtos/{id}`) ou a rota de renomear
**When** o nome muda de fato
**Then** uma linha `nome_alterado` com `{antes, depois}`, o autor e o horário é gravada na mesma transação; salvar sem mudar o nome não grava nada

**Given** o detalhe de um Produto aberto por `almoxarife`+
**When** a seção "Histórico do produto" carrega (`GET /api/produtos/{id}/historico`)
**Then** mostra, do mais recente para o mais antigo, cada troca de nome (antes → depois), inativação (com motivo) e reativação, com nome de quem fez e data/hora

**Given** um `usuario`
**When** abre o detalhe
**Then** a seção não aparece e a rota responde 403

**Given** o histórico de um Produto de outra Empresa
**When** é pedido
**Then** a resposta é 404, sem revelar existência (AD-20)

### Story 16.4: EAN-13 único entre os Produtos ativos da Empresa

As a almoxarife,
I want que o sistema recuse um EAN-13 que já está em outro Produto ativo,
So that o mesmo item não seja cadastrado duas vezes com códigos internos diferentes.

**Acceptance Criteria:**

**Given** o cadastro ou a edição de um Produto com um EAN-13 já usado por outro Produto **ativo** da mesma Empresa
**When** a pessoa salva
**Then** a resposta é 409 `EAN_EM_USO` e a tela diz "Este EAN já está no produto {código} — {nome}"; nada é gravado

**Given** o mesmo EAN-13 num Produto **inativo** ou em outra Empresa
**When** a pessoa salva
**Then** o salvamento é aceito

**Given** duas gravações simultâneas do mesmo EAN-13 em Produtos diferentes da mesma Empresa
**When** chegam juntas
**Then** só uma é aceita (lock por Empresa + EAN na transação, AD-32 revisada)

**Given** dois Produtos ativos que já tinham o mesmo EAN antes desta story
**When** esta story entra
**Then** nenhum dado é alterado; editar um dos dois só salva depois que o EAN for corrigido ou o outro Produto for inativado, e a mensagem diz qual é o outro

**Given** a edição de um Produto sem mudar o EAN e sem conflito
**When** salva
**Then** nada muda no comportamento de hoje
