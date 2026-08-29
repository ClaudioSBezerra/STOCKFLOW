---
title: stockflow
status: final
created: 2026-08-29
updated: 2026-08-29
---

# PRD: stockflow
*Working title — nome comercial "stockflow"; nome interno de origem "Catálogo de Materiais". Confirmar qual nome usar daqui para frente nos artefatos (repositório, comunicação interna).*

## 0. Objetivo deste documento

Este PRD é para Claudio (PM/dono da migração) e para os fluxos downstream do BMAD (UX, Arquitetura, Épicos/Stories, execução via `bmad-loop`). O stockflow **não é um produto novo**: é a mesma ferramenta interna da Ferreira Costa hoje conhecida como "Catálogo de Materiais", que já possui um PRD final, arquitetura e épicos/stories formalizados em um repositório irmão (`Catalogo-Obras`), com a Epic 1/Story 1.1 já marcada como pronta para desenvolvimento — mas nenhuma linha de código do backend-alvo foi escrita ainda. Este documento **consolida e substitui** aquele PRD como especificação de trabalho para o repositório `stockflow`, com duas mudanças de escopo:

1. **Stack-alvo alinhado ao projeto `FB_APU02`** (outro sistema já em produção na Ferreira Costa) em vez do stack originalmente especificado (Go + PostgreSQL + Redis + RabbitMQ + Hexagonal). Isso afeta principalmente decisões de arquitetura (ver §10 e `addendum.md`), não a lista de capacidades.
2. **Login federado via Keycloak** como opção adicional de autenticação, replicando o padrão já validado e em produção no `FB_APU02`.

Toda a numeração de FRs (FR-1 a FR-33) é herdada do PRD original para preservar rastreabilidade com os épicos/stories já escritos. Capacidades novas começam em FR-34. Vocabulário do Glossário (§3) é usado literalmente em todo o documento — nenhum sinônimo. Detalhes técnicos, decisões de arquitetura herdadas, a visão de produto SaaS de longo prazo e a análise competitiva estão em `addendum.md`, não aqui.

*Nota de organização:* os FRs em §4 estão agrupados por área funcional, não em ordem numérica estrita — por isso FR-31 a FR-39 (Autenticação/Acesso) aparecem antes de FR-4 (Catálogo) no corpo do documento. A numeração reflete a ordem histórica de introdução de cada capacidade (preservada do PRD original), não a ordem de leitura.

## 1. Visão

O stockflow é a ferramenta interna da Ferreira Costa para controlar **sobras de material de obra** compartilhadas entre múltiplos canteiros e almoxarifados. Hoje ele já existe como protótipo funcional (`index.html`, client-side puro, falando direto com Firebase/Firestore) e já é usado no dia a dia: cadastro de produtos, busca, movimentação (baixa/transferência) e pedidos de retirada com aprovação do almoxarife, mais ferramentas de normalização de dados (correção de inconsistências e deduplicação de produtos).

Este PRD não reinventa o produto — ele formaliza o que já foi validado por quem usa a ferramenta e planeja a reconstrução do backend em uma stack própria e sustentável, no lugar de depender do Firebase como sistema de registro. O ganho central da migração é fechar uma brecha de segurança crítica do protótipo atual: hoje toda a autorização por papel acontece só na interface — qualquer função de escrita, incluindo aprovar uma retirada (que debita estoque automaticamente), está exposta sem checagem no servidor. A migração também resolve estoque órfão ao excluir um local, compressão de foto inconsistente, duplicação de produtos ao reimportar planilha, e remove uma chave de API do Google exposta e sem uso.

Ao seguir o mesmo stack e o mesmo padrão de autenticação federada (Keycloak) já validados em produção no `FB_APU02` — outro sistema da Ferreira Costa — a migração reduz risco de arquitetura (padrão já testado em produção) e prepara o terreno para esta ser a primeira de várias migrações executadas pelo mesmo processo (ver `addendum.md`, seção de processo de execução).

## 2. Usuário-Alvo

### 2.1 Jobs To Be Done
- **Usuário de obra**: encontrar rapidamente material sobrando de outra obra antes de comprar de novo; solicitar retirada de material reservado sem precisar ligar para o almoxarifado.
- **Almoxarife**: aprovar/rejeitar pedidos com confiança de que o estoque mostrado é real no momento da aprovação; manter o catálogo organizado (sem nomes inconsistentes, duplicatas ou estoque fantasma); cadastrar materiais novos (manual ou via planilha) sem retrabalho.
- **Colaborador com conta corporativa Ferreira Costa**: entrar no stockflow com a mesma identidade corporativa usada em outros sistemas internos (ex. `FB_APU02`), sem precisar lembrar de outra senha.
- **Claudio (dono do produto/migração)**: um sistema que não depende da boa vontade do navegador do usuário para proteger dados e decisões de estoque, e que segue um padrão de stack já validado internamente.

### 2.2 Non-Users (v1)
- Fornecedores ou clientes externos — uso é 100% interno, sem portal externo, mesmo com autocadastro público aberto.
- Ninguém se autopromove de papel sem aprovação — toda promoção depende de decisão de um Gestor ou Adm (FR-33).
- Login via Keycloak não é *obrigatório* para ninguém em v1 — é uma opção adicional, o login por e-mail/senha (FR-1) continua existindo e sendo o caminho padrão para quem não tem conta corporativa Ferreira Costa mapeada no Keycloak.

### 2.3 Jornadas de Usuário Principais

- **UJ-1. Mariana, engenheira de obra, encontra material sobrando de outra obra.**
  Está orçando uma nova frente de serviço e quer evitar comprar tubo PVC que já existe em sobra. Busca "tubo pvc 100mm" (autocomplete), filtra "Com estoque", abre o card do produto e confere a quantidade por Estoque. Vê 40m disponíveis em dois canteiros diferentes e clica "Reservar" — o item entra no Carrinho. **Edge case:** se o produto não existe no catálogo, o autocomplete não retorna nada; comprar externamente fica fora do escopo do sistema.

- **UJ-2. João, almoxarife, aprova um pedido de retirada.**
  Recebe uma notificação in-app de pedido pendente, filtra "Pendentes", abre o pedido de Mariana e confere os itens. Clica "Aprovar" — o sistema revalida no servidor o estoque de cada item; se algum item não tem mais estoque suficiente, João é avisado explicitamente sobre qual item falhou (nunca uma baixa parcial silenciosa). O item mostra um selo "Solicitado: 10m · Disponível: 4m"; João escolhe "Aprovar os 4m disponíveis" (confirmação explícita) e resolve o restante depois. Se cancelar, nada é debitado. Estoque debitado e Movimentação registrada são atômicos.

- **UJ-3. João cadastra 200 itens de uma obra encerrada via planilha.**
  Seleciona a planilha Excel exportada; o sistema lê as colunas esperadas, cria Estoques ausentes automaticamente e decide, linha a linha, se atualiza (por código) ou cria um novo Produto. O relatório final mostra criados vs. atualizados, sem duplicar nada, e traz um CTA "Verificar duplicatas agora" que leva direto à ferramenta de Duplicatas (FR-19) já com a análise disparada — a importação só é considerada concluída depois dessa checagem. João roda a ferramenta como checagem final. **Edge case:** se a importação for interrompida no meio, a próxima tentativa mostra quais linhas já foram salvas, sem reprocessar nem duplicar.

- **UJ-4. João limpa inconsistências de nomenclatura acumuladas.**
  Produtos cadastrados por pessoas diferentes têm nomes e dimensões inconsistentes. Clica "Analisar todos os produtos", revisa as sugestões (ex. unidade ausente, valor embutido no nome), aceita em lote as óbvias e ignora as que não se aplicam. Decisões de "ignorar" ficam salvas e não reaparecem.

- **UJ-5. Carlos, colaborador Ferreira Costa, entra pelo login corporativo (SSO). *(novo nesta versão — realiza FR-34)***
  Carlos já tem conta no stockflow (criada via autocadastro ou provisionada por um Adm) e usa credenciais corporativas Ferreira Costa no dia a dia em outros sistemas internos. Na tela de login, clica em "Entrar com Ferreira Costa" e é redirecionado ao Keycloak corporativo, onde autentica com a credencial que já possui — sem precisar lembrar de outra senha. É redirecionado de volta ao stockflow, que troca o token do Keycloak por uma sessão própria (mesmo formato de sessão do login por senha), mantendo o papel que Carlos já tinha (`usuario`/`almoxarife`/`gestor`/`adm`). **Edge case:** se o e-mail retornado pelo Keycloak não corresponder a nenhuma conta existente no stockflow, ou não estiver marcado como verificado no Keycloak, o login via SSO é recusado com uma mensagem orientando a criar conta primeiro (FR-3) — o SSO nunca cria conta nova.

**Fluxos de apoio sem protagonista nomeado** (herdados do PRD original, ligados a FR-3/FR-31/FR-32/FR-33):
- **Novo colaborador cria a própria conta (FR-3):** cadastro público sem campo de papel; confirmação por e-mail obrigatória antes de acessar o catálogo como `usuario`.
- **Usuário pede para virar Almoxarife (FR-33):** menu do usuário → "Solicitar promoção"; justificativa opcional; botão desabilitado enquanto pendente; rejeição permite nova tentativa sem espera.
- **Gestor desativa um almoxarife que saiu (FR-31):** a tela de Gestão de Usuários só mostra contas que o ator pode gerenciar; a sessão ativa é encerrada na próxima requisição.
- **Mariana esqueceu a senha (FR-32):** reset por e-mail com mensagem genérica (não revela se a conta existe); link expirado ou já usado explica o motivo com opção de gerar um novo.

## 3. Glossário

- **Produto** — item do catálogo (ex. "Tubo PVC 100mm"): nome, código opcional, categoria, dimensões, foto, quantidade por Estoque.
- **Estoque** — local físico (canteiro/almoxarifado) onde Produtos são armazenados; um Produto pode estar em múltiplos Estoques.
- **Movimentação** — evento de saída (**Baixa**) ou **Transferência** entre Estoques; gera registro no Histórico.
- **Histórico** — log append-only de todas as Movimentações, incluindo as geradas por aprovação de Pedido.
- **Pedido de Retirada (Pedido)** — solicitação de retirada de quantidades de Produtos de Estoques específicos; status `pendente` / `aprovado` / `rejeitado`.
- **Carrinho** — seleção temporária de itens antes de enviar um Pedido.
- **Usuário** — pessoa autenticada; papel `usuario` < `almoxarife` < `gestor` < `adm`, hierarquia estrita (papel superior tem tudo do inferior + funções próprias).
- **Almoxarife** — decide todos os Pedidos, gerencia Estoques e Normalização.
- **Gestor** — herda Almoxarife; decide Solicitações de Promoção de Papel (inclusive para Almoxarife).
- **Adm** — herda Gestor; configuração geral do sistema; único que promove/rebaixa/desativa Gestor. Primeiro Adm é provisionado fora do app (bootstrap manual — ver FR-3).
- **Normalização** — ferramentas de Inconsistências (dimensões/unidades) e Duplicatas (detecção/mesclagem).
- **Nome normalizado** — nome sem acentuação, case-insensitive, sem espaços nas pontas — chave de comparação de Duplicatas.
- **Categoria** — classificação fixa (dado de configuração, ~25 categorias).
- **Nomenclatura Guiada** — templates de nome sugeridos por subtipo de material (28 templates na versão original, ver `addendum.md` §G).
- **Login Federado (SSO)** — autenticação alternativa via identidade corporativa Ferreira Costa no Keycloak, associada a um Usuário já existente no stockflow pelo e-mail; nunca cria conta nova nem altera o papel do Usuário.
- **Código de Identificação** — código interno/SKU já cadastrado em um Produto (FR-8), reaproveitado como valor codificado em QR Code ou código de barras físico para identificação rápida (FR-35).

## 4. Funcionalidades

### 4.1 Autenticação e Controle de Acesso
**Descrição:** todo acesso à API exige autenticação, exceto cadastro (FR-3) e login (FR-1, FR-34). O papel do Usuário determina o que ele pode fazer, e essa checagem existe no servidor — nunca só na interface.

**Login e controle de papel:**

#### FR-1: Login por e-mail e senha
Usuário autentica com e-mail/senha para acessar qualquer funcionalidade.
**Consequences:**
- Nenhum endpoint responde sem token válido, exceto login e cadastro.
- Sessão expira após 2h de inatividade.

#### FR-2: Autorização por papel aplicada no servidor
Backend valida o papel em cada endpoint sensível, não só a interface. Realiza UJ-2.
**Consequences:**
- Uma ação de `almoxarife`+ chamada por `usuario` retorna 403 mesmo via chamada direta à API.
- Endpoints de Estoques/Normalização exigem `almoxarife`+ (herança de papel).

#### FR-3: Autocadastro público, sempre como `usuario`
Qualquer pessoa cria conta pública (nome, e-mail, senha), sempre como `usuario` — promoção só via FR-33.
**Consequences:**
- Tela acessível sem login.
- Backend nunca aceita um papel diferente vindo do formulário.
- E-mail único garantido atomicamente (409 em duplicata).
- Confirmação de e-mail obrigatória antes do primeiro login.
- `[ASSUMPTION]` Primeiro Adm é provisionado fora do app via procedimento de bootstrap seguro — item obrigatório da fase de Arquitetura.

#### FR-31: Desativação e rebaixamento de conta
`adm` desativa/rebaixa qualquer conta (inclusive `gestor`); `gestor` só desativa/rebaixa `almoxarife`/`usuario`.
**Consequences:**
- Conta desativada não autentica mais (login por senha ou SSO).
- Rebaixamento perde acesso já na próxima requisição.
- `gestor` tentando agir sobre `gestor`/`adm` recebe 403.

#### FR-32: Recuperação de senha por e-mail
Usuário solicita redefinição por e-mail e recebe link/código de uso único.
**Consequences:**
- Link expira em prazo curto (`[ASSUMPTION: 30 min]`) e é de uso único.
- E-mail inexistente não é revelado (mensagem genérica).
- Uma conta que hoje só usa login via SSO (FR-34, nunca definiu senha) pode usar este fluxo para criar uma senha própria pela primeira vez — a partir daí passa a ter os dois caminhos de login disponíveis (SSO e senha).

#### FR-33: Solicitação de promoção de papel
Usuário solicita promoção para o papel imediatamente acima; fica pendente até decisão.
**Consequences:**
- Promoção a `almoxarife` decidida por `gestor`/`adm`; promoção a `gestor` só por `adm` `[ASSUMPTION]`.
- Só uma solicitação pendente por vez.
- Rejeição não impede nova tentativa.
- Aprovação vale imediatamente (não espera a sessão de 2h expirar).
- Toda decisão (aprovação ou rejeição) registra quem decidiu e quando — trilha de auditoria da solicitação.

#### FR-34: Login federado via Keycloak (SSO Ferreira Costa) *(novo)*
Usuário com conta corporativa Ferreira Costa (realm Keycloak `ferreiracosta`) autentica via SSO como alternativa ao login por e-mail/senha (FR-1), replicando o padrão já em produção no projeto `FB_APU02`. Realiza UJ-5.
**Consequences:**
- O SSO **nunca cria conta nova** — busca um Usuário existente pelo e-mail retornado pelo Keycloak; se não encontrar, recusa o login com orientação para se cadastrar primeiro (FR-3).
- Exige que o e-mail esteja marcado como verificado (`email_verified=true`) no token do Keycloak antes de aceitar o login — sem essa checagem, uma edição de e-mail no Keycloak sem reconfirmação permitiria logar como outra pessoa.
- O token do Keycloak nunca é persistido; ao concluir a troca, o stockflow emite os mesmos tokens de sessão próprios usados pelo login por senha (FR-1) — nenhuma duplicação de gerenciamento de sessão.
- O papel (`usuario`/`almoxarife`/`gestor`/`adm`) do Usuário **não é definido pelo Keycloak** — continua sendo o papel já atribuído dentro do stockflow (FR-2, FR-33). O SSO troca apenas *quem autentica*, nunca *quem autoriza*.
- Login por e-mail/senha (FR-1) continua disponível e é o caminho padrão exibido na tela de login; o SSO aparece como uma opção adicional (botão), **não** como redirecionamento automático — decisão deliberada, diferente do comportamento do `FB_APU02` (ver `addendum.md` §B).
- A busca do Usuário pelo e-mail retornado pelo Keycloak é case-insensitive (mesmo e-mail com capitalização diferente identifica a mesma conta).
- Logout de uma sessão iniciada via SSO também encerra a sessão no Keycloak (RP-initiated logout) — exige que a URL de retorno pós-logout esteja registrada no client Keycloak (ver Open Question 5).
**Out of Scope:**
- Provisionamento automático de conta nova a partir do primeiro login SSO (fica para versão futura — ver Open Questions).
- Sincronização de papel a partir de grupos/roles do realm Keycloak (mesma lacuna hoje existente no `FB_APU02` — ver `addendum.md`).

**Segurança de conta e conformidade:**

#### FR-36: Bloqueio de conta e política de senha *(novo)*
Login por senha (FR-1) tem proteção contra tentativa de força bruta e exige senha minimamente robusta no cadastro (FR-3) e redefinição (FR-32).
**Consequences:**
- `[ASSUMPTION]` Conta bloqueada temporariamente após 5 tentativas de login malsucedidas consecutivas; bloqueio de 15 minutos.
- `[ASSUMPTION]` Senha exige no mínimo 8 caracteres, com letra e número.
- Login via SSO (FR-34) não é afetado pelo bloqueio de senha — são caminhos independentes.

#### FR-37: MFA obrigatório para papéis administrativos *(novo)*
Contas com papel `gestor` ou `adm` autenticadas por senha (FR-1) devem configurar um segundo fator (TOTP) antes de acessar funcionalidades restritas a esses papéis.
**Consequences:**
- Conta `gestor`/`adm` sem MFA configurado é bloqueada de ações restritas até configurar, mesmo já autenticada.
- Login via SSO (FR-34) herda o MFA já imposto pelo realm Keycloak corporativo `ferreiracosta` a contas `gestor`/`adm` — **confirmado com o usuário nesta rodada**: o realm já impõe MFA para essas contas, então não é necessário um segundo MFA próprio no caminho SSO.
**Out of Scope:** MFA para papéis `usuario`/`almoxarife` (não exigido em v1).

#### FR-38: Log de acesso e auditoria *(novo)*
Todo login (sucesso ou falha) é registrado com usuário (quando identificável), timestamp, IP e método (senha ou SSO), consultável por `adm`.
**Consequences:**
- Falha de login por e-mail inexistente registra a tentativa sem revelar ao solicitante se o e-mail existe (consistente com FR-32).
- Log é append-only — não pode ser editado ou apagado por nenhum papel, apenas consultado.
- Retenção do log de acesso segue a mesma política de 12 meses do Histórico de Movimentações/Pedidos (§9), depois arquivado.

#### FR-39: Exportação e exclusão de dados pessoais (LGPD) *(novo)*
Usuário pode solicitar exportação dos próprios dados pessoais; `adm` pode processar solicitação de exclusão/anonimização de dados pessoais de uma conta.
**Consequences:**
- Exportação inclui nome, e-mail e histórico de ações do próprio Usuário, em formato legível (ex. JSON ou PDF).
- Exclusão anonimiza os dados pessoais da conta (nome, e-mail) mas preserva o vínculo do Histórico de Movimentações e Pedidos (FR-16, FR-23) já registrado — não quebra auditoria/rastreabilidade existente.
- `[NOTE FOR PM]` prazo de resposta e processo formal de atendimento a solicitações LGPD a definir com jurídico/compliance da empresa — fora do escopo deste PRD definir esse SLA.

### 4.2 Catálogo de Produtos
**Descrição:** qualquer Usuário autenticado pode buscar e visualizar o catálogo, com a quantidade discriminada por Estoque. Realiza UJ-1.

#### FR-4: Busca por nome/código/categoria com sugestões
Autocomplete com até 7 itens, ordenado por relevância.

#### FR-5: Filtros por categoria, estoque e disponibilidade
Filtros combináveis simultaneamente.

#### FR-6: Visualização em grade e tabela agrupada
Tabela agrupa e soma quantidades de produtos com mesmo nome/unidade/dimensões.

#### FR-7: Detalhe do produto por local de estoque
Mostra a quantidade discriminada por Estoque.

#### FR-35: Identificação de Produto via QR Code / código de barras *(novo)*
Usuário identifica um Produto apontando a câmera do celular para um QR Code ou código de barras, como alternativa à busca por texto (FR-4). Realiza UJ-1, UJ-2, UJ-3.
**Consequences:**
- O código lido é o mesmo Código de Identificação já cadastrado no Produto (FR-8) — nenhum campo novo de identificação é criado.
- Leitura reconhecida abre diretamente o detalhe do Produto (FR-7) ou adiciona ao Carrinho (FR-21), dependendo do contexto de uso (consulta vs. montagem de pedido).
- Produto sem código cadastrado continua funcionando normalmente, apenas sem esse atalho — segue disponível por busca textual (FR-4).
**Out of Scope:** impressão de etiquetas de QR Code/código de barras pelo próprio sistema — a identificação usa o código físico que a empresa já imprime/afixa hoje.

**Feature-specific NFRs:**
- Busca/listagem ≤300ms p95 sob carga típica (até 8.000 produtos, 30 Estoques).

### 4.3 Cadastro e Importação
**Descrição:** restrito a `almoxarife`+ (FR-2). Realiza UJ-3.

#### FR-8: Cadastro manual de Produto
Nome, código, categoria, dimensões (comprimento, largura, diâmetro, altura, espessura — cada uma como par estruturado valor+unidade, nunca texto livre), estoque destino, quantidade inicial, observações, foto opcional.
**Consequences:**
- `usuario` chamando este endpoint recebe 403 (vale também para FR-10).
- Dimensão sem unidade (ou vice-versa) é rejeitada.

#### FR-9: Nomenclatura Guiada por subtipo
Templates de nome sugeridos por subtipo (28 na versão original), obrigatória quando um template é selecionado.
**Consequences:**
- Nome deve seguir a estrutura do template (mesma ordem de tokens, sem placeholder vazio), validado no servidor.
- Sem template, texto livre.
- Editar um Produto com template aplicado revalida o nome contra esse template — não é possível burlar a regra editando depois do cadastro.

#### FR-10: Importação em massa via planilha padronizada
Cria Estoques ausentes automaticamente.
**Consequences:**
- Modelo de planilha com coluna valor+unidade separada por dimensão.
- Cabeçalho fora do padrão rejeita a importação inteira antes de processar qualquer linha.
- Linha com valor sem unidade é erro de linha, não produto incompleto.

#### FR-11: Importação atualiza por código, não só cria
**Consequences:**
- Reimportar a mesma planilha não duplica produtos.
- Relatório final discrimina criados/atualizados/rejeitados.
**Out of Scope:** correspondência por nome sem código fica para a ferramenta de Duplicatas (FR-19), não para o importador.

**Notes:** `[NOTE FOR PM]` a pesquisa de mercado (`addendum.md` §D) aponta a importação de planilha como o principal fator de adoção/conversão no segmento — friccionar esse fluxo custa desproporcionalmente mais caro em percepção de qualidade da migração do que qualquer outra feature. Priorizar UX de FR-10/FR-11 (mensagens de erro claras, feedback de progresso) acima do mínimo funcional.

### 4.4 Gestão de Estoques
**Descrição:** restrito a `almoxarife`+.

#### FR-12: Criar e listar locais de Estoque
Nome único (case/espaço-insensitive), garantido atomicamente.

#### FR-13: Exclusão de Estoque trata resíduos e pedidos pendentes
**Consequences:**
- Bloqueada se houver quantidade residual (lista quais Produtos) ou Pedido `pendente` referenciando o Estoque, mesmo com quantidade zerada.

### 4.5 Movimentação de Estoque
**Descrição:** restrito a `almoxarife`+ (mesma restrição de FR-8) — para não abrir caminho de debitar estoque fora do fluxo de aprovação de Pedido.

#### FR-14: Registrar Baixa (consumo)
**Consequences:** `usuario` → 403; rejeita quantidade zero ou negativa.

#### FR-15: Registrar Transferência entre Estoques
**Consequences:** `usuario` → 403; rejeita origem = destino; rejeita quantidade maior que a disponível — checagem e débito atômicos, para evitar saldo negativo por concorrência.

#### FR-16: Histórico de Movimentações consultável
Produto, tipo, origem, destino, quantidade, autor, data.

### 4.6 Normalização de Dados
**Descrição:** ferramentas de limpeza para dados migrados (texto livre) e detecção contínua de inconsistência. Realiza UJ-4.

#### FR-17: Detecção de inconsistências dimensionais
**Consequences:** sugestão identifica produto, campo, valor sugerido e origem; campo já estruturado válido nunca gera sugestão.

#### FR-18: Aplicação seletiva de correções
Individual, em lote por produto, ou em lote geral; "ignorar" é permanente por valor específico (não por campo em geral).
**Consequences:**
- Se o campo for alterado depois para um novo valor inconsistente, a sugestão reaparece — "ignorar" vale só para o valor específico já visto, não bloqueia detecções futuras naquele campo.

#### FR-19: Detecção de duplicatas
Agrupa por nome normalizado + dimensões equivalentes (com conversão de unidade) + locais coincidentes.

#### FR-20: Mesclagem de duplicatas com trilha de auditoria
**Consequences:**
- Soma quantidades no registro mantido, remove os demais, gera auditoria (quem, quando, produtos removidos, valores) — reconstituível mesmo após exclusão.
- Item em Carrinho ou Pedido pendente é redirecionado ao produto mantido.
- Quantidade somada é revalidada na confirmação (nunca usa um snapshot antigo).

### 4.7 Pedidos de Retirada
**Descrição:** todo consumo passa por este fluxo. Realiza UJ-1, UJ-2.

#### FR-21: Carrinho de reserva
**Consequences:** valida no momento da adição, somando linhas já no carrinho; item cujo Produto/Estoque some é removido automaticamente com aviso.

#### FR-22: Envio de Pedido
Finaliza informando solicitante, obra/centro de custo, observação → status `pendente`.
**Consequences:**
- Rejeita carrinho vazio.
- Revalida disponibilidade no envio (não só na adição).
- Auditoria e "Meus Pedidos" sempre usam a identidade autenticada, mesmo que "solicitante" seja texto livre.

#### FR-23: Consulta de Pedidos próprios
Filtrável por status, com texto explicativo por status.

#### FR-24: Consulta de todos os Pedidos (Almoxarife+)
**Consequences:** um Usuário sem papel `almoxarife`+ chamando esta rota recebe só os próprios pedidos (escopo, não erro).

#### FR-25: Aprovação/rejeição com revalidação de estoque item a item
Realiza UJ-2.
**Consequences:**
- Falha em um item nunca gera sucesso parcial silencioso — o almoxarife recebe a lista exata e decide (aprovação parcial explícita ou rejeitar/ajustar).
- Itens que falham voltam como pendentes.
- Papel do aprovador é revalidado no momento exato da submissão (não no carregamento da tela).
- Débito e Movimentação são atômicos.

#### FR-26: Recibo do Pedido em PDF gerado pelo servidor *(revisado nesta versão — substitui a decisão original de "impressão via navegador")*
Após decisão do Pedido (aprovado, ou parcialmente aprovado), o sistema gera um recibo/comprovante em **PDF no servidor**, com itens, quantidades, estoques de origem, solicitante, aprovador e data.
**Consequences:**
- Layout do PDF é consistente independente do navegador ou dispositivo — não depende mais de CSS de impressão do cliente.
- `[ASSUMPTION]` Geração sob demanda via endpoint dedicado (ex. "Baixar recibo"), não anexado automaticamente a e-mail.
**Out of Scope:** assinatura digital do PDF.

### 4.8 Fotos de Produtos

#### FR-27: Upload de foto com regra única de tamanho/compressão
JPG/PNG/WEBP.
**Consequences:** mesma resolução (maior lado 500px) e compressão (JPEG q=0.82) independente do fluxo (cadastro ou edição); erro específico por tamanho vs. formato.

#### FR-28: Armazenamento de fotos fora do banco relacional
Serviço/volume de objetos dedicado, não base64 inline.

#### FR-29: Galeria e visualização ampliada (lightbox)
Produto com múltiplas fotos exibe uma galeria navegável; qualquer foto pode ser expandida em tela cheia (lightbox) a partir do card ou do detalhe do Produto (FR-7).
**Consequences:** expandir uma foto não recarrega a página nem perde o contexto de navegação (filtros/posição na lista) ao fechar.

### 4.9 Exportação de Relatórios

#### FR-30: Exportação da tabela do catálogo para Excel
`.xlsx` formatado com totais e metragem calculada.
**Consequences:**
- Filtro com zero produtos gera `.xlsx` válido só com cabeçalho.
- Subtotais são dinâmicos por grupo/filtro ativo (fórmula tipo `SUBTOTAL`, não soma estática) — permanecem corretos mesmo com filtro aplicado na planilha exportada. Diferencial competitivo citado explicitamente na análise de mercado (`addendum.md` §D) — nenhum concorrente direto oferece isso.

## 5. Non-Goals (Explícito)

- Não migrar a busca automática de fotos via Google Custom Search — código morto e chave de API exposta; será revogada independente do cronograma de migração.
- Não há autopromoção de papel no autocadastro (FR-3).
- Não é sistema de compras nem integra fornecedores externos.
- SSO (FR-34) não substitui o login por e-mail/senha (FR-1) nem se torna obrigatório em v1.
- SSO (FR-34) não cria conta nova nem sincroniza papel a partir de grupos do Keycloak em v1.
- A visão de produto SaaS multi-tenant vendável a terceiros (módulo de locação de equipamentos, planos Starter/Business/Enterprise, white-label) **não faz parte deste PRD** — é uma visão de longo prazo documentada como contexto em `addendum.md`, sem FRs correspondentes nesta versão.

## 6. Escopo do MVP

### 6.1 Em Escopo
- Autenticação/autorização/ciclo de vida de conta/recuperação de senha (FR-1, FR-2, FR-3, FR-31, FR-32).
- Login federado via Keycloak (FR-34).
- Bloqueio de conta/política de senha (FR-36), MFA para papéis administrativos (FR-37), log de acesso (FR-38), exportação/exclusão de dados pessoais LGPD (FR-39).
- Todas as features de Catálogo, Cadastro/Importação, Estoques, Movimentação, Normalização, Pedidos e Fotos (FR-4 a FR-29), incluindo geração de PDF no servidor para o recibo do Pedido (FR-26) e identificação por QR Code/código de barras (FR-35).
- Solicitação de Promoção de papel (FR-33).
- Exportação Excel (FR-30).
- Migração de dados em corte único, a partir do espelho PostgreSQL local do Firestore mantido pela empresa (ver §9 Constraints).
- Revogação da chave do Google Custom Search exposta.
- Reconstrução do frontend seguindo os padrões visuais/de componentes já usados no `FB_APU02` (shadcn/ui + Tailwind).
- Atualização quase em tempo real do catálogo (equivalente ao comportamento atual do Firestore) — mecanismo concreto é decisão de Arquitetura, não fechada neste PRD (ver §11 Open Questions).

### 6.2 Fora de Escopo do MVP
- Assinatura digital do PDF de recibo (FR-26).
- Provisionamento automático de conta a partir do primeiro login SSO.
- Sincronização de papel via grupos/roles do Keycloak.
- Módulo de locação de equipamentos, multi-tenant, planos comerciais (visão de longo prazo — `[NOTE FOR PM]` revisitar só se a empresa decidir comercializar o stockflow para terceiros).

## 7. Métricas de Sucesso

**Primárias**
- **SM-1**: 100% das ações de escrita sensíveis recusadas no servidor quando executadas por um papel sem permissão, verificável por teste automatizado. Valida FR-2, FR-3, FR-8, FR-10, FR-13, FR-14, FR-15, FR-18, FR-20, FR-25, FR-31, FR-33, FR-34.
- **SM-2**: taxa de itens que falham na revalidação de estoque durante a aprovação (FR-25), acompanhada nos primeiros 90 dias — proxy de contenção/timing entre pedidos concorrentes.

**Secundárias**
- **SM-3**: reimportação de planilha não gera duplicatas novas. Valida FR-11.
- **SM-4**: tempo de resposta do catálogo ≤300ms p95.
- **SM-5**: adoção do login via SSO entre contas corporativas Ferreira Costa nos primeiros 90 dias após disponibilização — indicador de adoção, sem meta rígida. Valida FR-34.
- **SM-6**: 100% das tentativas de força bruta contra login por senha são bloqueadas após o limite configurado, verificável por teste automatizado. Valida FR-36.

**Contra-métricas (não otimizar)**
- **SM-C1**: a migração não deve aumentar o tempo percebido para cadastrar um produto ou registrar uma movimentação em relação ao protótipo atual — contrabalança SM-1 (não sacrificar velocidade em nome de segurança).

## 8. NFRs Transversais

- **Segurança:** toda autorização por papel é validada no servidor; nenhuma credencial (Keycloak ou própria) é exposta no cliente/repositório; toda entrada é validada no limite da API; proteção contra força bruta e MFA para papéis administrativos (FR-36, FR-37); log de acesso auditável (FR-38).
- **Privacidade:** conformidade com LGPD para dados pessoais de contas de Usuário — exportação e exclusão/anonimização sob solicitação (FR-39).
- **Observabilidade:** ações hoje silenciosas no protótipo (skip de item sem estoque, erros de fundo) passam a ser logadas estruturadamente e comunicadas na interface.
- **Confiabilidade:** operações em lote não travam a UI nem deixam o catálogo em estado parcial sem indicação de progresso; importação interrompida permite saber quais linhas já foram gravadas.
- **Concorrência:** toda escrita dependente de estado lido previamente é atômica no servidor (saldo de estoque, unicidade de código/e-mail/nome de estoque, aprovação concorrente de pedido).
- **Desempenho:** catálogo/busca ≤300ms p95 (8.000 produtos/30 estoques); demais telas sem SLA formal (`[NOTE FOR PM]` item para Arquitetura, sem baseline do protótipo atual).
- **Usabilidade em campo:** interface responsiva para uso no celular é requisito, não opcional — as personas primárias (Mariana, João) operam majoritariamente em campo/obra, com frequência só com o celular em mãos (sinal explícito nos documentos de origem, ver `addendum.md` §D). `[ASSUMPTION]` Critério concreto: todas as telas do fluxo principal (busca, carrinho, aprovação de pedido, leitura de QR Code/código de barras — FR-35) funcionais em viewport a partir de 360px de largura, testadas nos navegadores móveis padrão (Chrome Android, Safari iOS).

## 9. Restrições e Guardrails

- **Segurança:** a chave do Google Custom Search deve ser revogada/rotacionada independente do cronograma de migração; nenhuma nova credencial (própria ou do Keycloak) pode ser exposta no cliente.
- **Privacidade:** dados de solicitante são de uso interno; retenção segue política corporativa a definir.
- **Migração:** migrar Produtos/Estoques/Histórico/Pedidos/Usuários sem perda de histórico; fonte direta é o espelho PostgreSQL local (Docker) mantido pela empresa (a empresa decomissiona o Firestore antes do corte); corte único ("big-bang"), sem operação paralela prolongada — `[NOTE FOR PM]` exige plano de rollback e janela de baixo uso; consolidar a inconsistência `papel` vs. `funcao` hoje existente na coleção `usuarios` do protótipo (schema-alvo usa um único campo). **O corte de banco em produção é sempre disparado manualmente por uma pessoa — nunca executado de forma autônoma por um agente de IA**, mesmo que o restante da migração (código) seja desenvolvido sob o processo de agentes descrito em `addendum.md` §C.
- **Retenção:** histórico de Movimentações e Pedidos mantido por 12 meses, depois expurgado/arquivado (política a detalhar na Arquitetura); exceção: o registro de auditoria de mesclagem de duplicatas (FR-20) nunca é expurgado — permanente `[ASSUMPTION]`.
- **Autenticação federada:** o realm e client Keycloak usados devem seguir o mesmo padrão de segurança já em produção no `FB_APU02` (validação de assinatura via JWKS, checagem de `azp` contra allowlist, exigência de `email_verified`) — ver `addendum.md` para o desenho técnico de referência.

## 10. Integrações e Dependências

- **Keycloak (Ferreira Costa)** — realm corporativo já em produção (usado pelo `FB_APU02`); FR-34 depende de um client dedicado ao stockflow registrado nesse mesmo realm. `[ASSUMPTION]` reaproveitar o mesmo realm, com client id próprio do stockflow (não o mesmo client id do `FB_APU02`).
- **PostgreSQL local (Docker) da empresa** — origem direta da migração de dados, espelho estrutural do Firestore atual.
- **SMTP corporativo** — envio de e-mail transacional (verificação de conta, recuperação de senha); mecanismo de envio assíncrono é decisão de Arquitetura (ver §11).
- **Excel/CSV** — modelo único padronizado de colunas (nome, código, categoria, dimensões+unidade, quantidade, estoque, observações) para importação/exportação.
- **Google Custom Search API** — sendo descomissionada; não é dependência do sistema-alvo.
- Não há integração com ERP nesta versão (fora do escopo do MVP).

## 11. Perguntas em Aberto

1. **Mecanismo de atualização quase em tempo real do catálogo.** A arquitetura original (herdada do PRD de origem) previa SSE via Redis Pub/Sub + outbox pattern via RabbitMQ. O stack-alvo de referência (`FB_APU02`) não usa RabbitMQ, e o Redis está no `docker-compose` mas não é consumido pelo código Go. Precisa de decisão de Arquitetura: broadcaster in-process (viável se o deploy for single-host sem múltiplas réplicas da aplicação) ou outro mecanismo.
2. **Envio assíncrono de e-mail transacional** (verificação de conta FR-3, recuperação de senha FR-32) — a arquitetura original usava fila RabbitMQ para não bloquear a resposta HTTP. Sem essa dependência confirmada no stack-alvo, precisa de mecanismo alternativo (ex. tabela de outbox no Postgres + worker/goroutine).
3. **Revogação imediata de acesso (FR-31).** A arquitetura original usava cache de papel em Redis com TTL curto e invalidação ativa para garantir que o rebaixamento valha "já na próxima requisição". Precisa de mecanismo alternativo se Redis não estiver de fato em uso no runtime.
4. ~~Recuperação de senha para contas que só usam SSO~~ — **resolvida nesta rodada:** o fluxo de redefinição (FR-32) passa a permitir criar uma senha própria pela primeira vez para essas contas.
5. **Client Keycloak dedicado.** Confirmar se o stockflow deve registrar um client id próprio no realm `ferreiracosta`, ou reaproveitar allowlist existente do `FB_APU02` — decisão de Arquitetura/infraestrutura, requer aprovação humana explícita antes do provisionamento no Keycloak real (mesma regra já seguida no `FB_APU02`). O provisionamento também precisa registrar a URL de retorno pós-logout (`post_logout_redirect_uri`) usada pelo RP-initiated logout (FR-34).
6. **Ambiente de deploy em Ferreira Costa.** Servidor dedicado no padrão já usado pelo `FB_APU02` para clientes com SSO habilitado, ou outro ambiente — a confirmar na fase de Arquitetura.
7. **Escopo exato de "configurar tudo" do papel `adm`** (herdado do PRD original, nunca detalhado em FR concreto) — tratado como capacidade genérica até haver necessidade específica.
8. **Relação formal com o PRD/épicos/stories já existentes no repositório `Catalogo-Obras`** — este PRD consolida esse trabalho como especificação viva do repositório `stockflow`; a decidir se a Epic 1/Story 1.1 já marcada como `ready-for-dev` naquele repositório é reaproveitada como ponto de partida ou reescrita do zero na Arquitetura/Épicos deste projeto.
9. **Repositório/código do sistema atual** — ainda não recebido (o usuário indicou que enviará depois); pode revelar detalhes de schema/comportamento não cobertos pelos documentos já analisados, exigindo atualização deste PRD.
10. **Biblioteca de geração de PDF no servidor (FR-26).** Nenhum stack de referência analisado (`FB_APU02`, PRD original) já usa geração de PDF no servidor em Go — é uma dependência nova, a escolher na fase de Arquitetura.
11. **Numeração de FR do `epics.md` original não bate 1:1 com a do `prd.md` original para FR-4 a FR-30** (o bloco FR-31/32/33 foi realocado no PRD, mas `epics.md` mantém numeração própria) — quem for gerar os Épicos/Stories deste PRD a partir do `epics.md` existente precisa conferir o mapeamento FR a FR antes de reaproveitar, não assumir correspondência direta.
12. ~~O realm Keycloak `ferreiracosta` de fato impõe MFA a contas `gestor`/`adm`?~~ — **resolvida nesta rodada:** confirmado com o usuário que sim, o realm já impõe MFA a essas contas.

## 12. Índice de Assunções

- §4.1 FR-3 — primeiro Adm provisionado fora do app via bootstrap seguro (procedimento a definir na Arquitetura).
- §4.1 FR-32 — link de redefinição de senha expira em 30 minutos.
- §4.1 FR-33 — promoção a `gestor` restrita a decisão de `adm`.
- §4.7 FR-26 — PDF do recibo gerado sob demanda via endpoint dedicado, não anexado automaticamente a e-mail.
- §4.1 FR-36 — bloqueio após 5 tentativas malsucedidas, 15 minutos; senha mínima de 8 caracteres com letra e número.
- §4.1 FR-37 — login via SSO herda o MFA já aplicado pelo Keycloak corporativo (confirmado com o usuário: o realm já impõe MFA a `gestor`/`adm`).
- §8 — usabilidade em campo: viewport mínimo de 360px, testado em Chrome Android e Safari iOS.
- §9 — registro de auditoria de mesclagem de duplicatas (FR-20) nunca é expurgado, mesmo após 12 meses.
- §10 — client Keycloak do stockflow é registrado com client id próprio, distinto do `FB_APU02`, no mesmo realm `ferreiracosta`.
