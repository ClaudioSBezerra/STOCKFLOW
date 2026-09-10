# Revisão Adversarial — §4.10 Estrutura Multi-Empresa e Ambiente de Treinamento (FR-40 a FR-44)

**Alvo:** `prd.md` (§4.10, FR-40–FR-44, mais interseções com §2, §3, §7, §9, §11) e `addendum.md` (§A, §I) do PRD stockflow, versão 2026-09-10.
**Lente:** Adversarial (única lente solicitada).
**Contexto de risco:** esta seção é adicionada a um sistema **já em produção real** na Ferreira Costa, com 2745+ produtos, movimentações e pedidos já migrados de uma migração legada anterior (Firestore→Postgres) que já rodou. Isso muda o perfil de risco de "definir um modelo novo" para "reestruturar um modelo vivo sob uso ativo".

## Veredito

A seção 4.10 é bem escrita como *intenção de produto*, mas como especificação de segurança para um sistema já em produção ela tem lacunas sérias o suficiente para não ser considerada "launch-ready" no rigor que o próprio PRD reivindica (rigor calibrado como *launch*, máximo). Os quatro vetores de ataque pedidos pelo usuário se confirmam todos como reais, não hipotéticos: (1) há pelo menos três pontos de junção implícitos onde isolamento pode vazar sem revisão explícita de código (Duplicatas, Log de Acesso de tentativa de login com e-mail ambíguo, arquivos de foto servidos fora do filtro de banco); (2) o fluxo de bootstrap do "Dono da Plataforma" contradiz uma decisão de arquitetura que o próprio addendum diz que continua válida (bootstrap só via CLI, nunca endpoint HTTP) e não define nem MFA para o papel mais privilegiado do sistema; (3) o convite/link (FR-42) é, como especificado, um bearer token sem vínculo a destinatário, sem menção de rate limit no resgate, e com semântica de uso único vs. reuso não resolvida; (4) a migração FR-44 é tratada como se fosse estruturalmente igual à migração legada anterior (corte único, pré-produção), quando na verdade é uma segunda migração sobre um banco **já vivo e em uso diário**, sem plano de rollback próprio, sem resumabilidade (ao contrário do que FR-10 exige para importação de planilha) e sem definição de como escrita concorrente durante a migração é tratada.

Nenhum destes é um problema de redação — são decisões de produto/arquitetura que faltam ser tomadas antes que Épicos/Stories sejam gerados a partir deste PRD.

## Achados

### Bloco 1 — Vazamento de isolamento entre Empresas (FR-40)

**1. Sucessão do papel `adm` dentro de uma Empresa não tem caminho definido**
- **Localização:** §3 Glossário ("Adm... único que promove/rebaixa/desativa Gestor dentro da sua Empresa"); FR-31; FR-41.
- **Condição de disparo:** o único `adm` ativo de uma Empresa é desativado, perde acesso, ou sai da empresa cliente — e nenhum outro papel dentro daquela Empresa pode agir sobre um `adm` (FR-31: `gestor` recebe 403 ao tentar agir sobre `adm`/`gestor`), e o Dono da Plataforma está explicitamente proibido de tocar conteúdo operacional e só provisiona o **primeiro** `adm` (FR-41), não um substituto.
- **Correção concreta:** FR-41 (ou um FR novo) precisa definir explicitamente um caminho de recuperação — ex. o Dono da Plataforma pode provisionar um `adm` de substituição sem enxergar conteúdo operacional (ação estritamente de metadado, auditada), análogo ao bootstrap inicial.
- **Consequência se não corrigido:** uma Empresa cliente pode ficar permanentemente sem ninguém capaz de gerenciar usuários — um "tenant lockout" sem solução no produto, forçando um bypass manual fora do app (contradizendo o próprio objetivo de "fechar brechas do protótipo atual").

**2. Janela de inconsistência entre deploy do schema multi-Empresa (FR-40) e a migração de dados (FR-44)**
- **Localização:** §9 "Migração multi-Empresa" ("Roda depois que o schema multi-Empresa (FR-40) já está em produção, antes de qualquer segunda Empresa real ser criada").
- **Condição de disparo:** a frase descreve dois passos sequenciais e não atômicos: (a) schema com `empresa_id` entra em produção; (b) dados existentes são retroativamente vinculados à Empresa "Ferreira Costa". O PRD não diz o que acontece com o sistema **entre** (a) e (b) — se toda consulta passa a filtrar por `empresa_id` do ator autenticado e as linhas existentes ainda não têm `empresa_id`, elas ficam invisíveis (ou o app precisa ficar fora do ar) até (b) terminar.
- **Correção concreta:** declarar explicitamente que (a) e (b) rodam na mesma transação/janela de manutenção, ou que o app fica em modo leitura/fora do ar entre os dois passos — isso não pode ficar implícito.
- **Consequência se não corrigido:** produção "desaparece" para os usuários reais (Mariana, João) durante uma janela não dimensionada, ou pior, alguém interpreta a lacuna como "linhas sem empresa_id são visíveis a todos por padrão" (fail-open) em vez de fail-closed.

**3. Fotos de produto (FR-28) não têm isolamento por Empresa mencionado explicitamente**
- **Localização:** FR-28 ("armazenamento fora do banco relacional... volume/serviço de objetos dedicado"); FR-40 consequência (lista Catálogo/Estoques/Movimentações/Pedidos/Log de Acesso, FR-4 a FR-30, FR-38 — inclui FR-28 só por estar no intervalo numérico, não nomeado).
- **Condição de disparo:** se o endpoint que serve o arquivo de foto identifica o arquivo só pelo id do produto/foto (sem checar `empresa_id` do produto dono contra a Empresa do ator autenticado antes de servir o blob), um usuário autenticado da Empresa B pode acessar fotos da Empresa A por enumeração/adivinhação de id, contornando qualquer filtro de banco que exista só nas consultas de metadado do Catálogo.
- **Correção concreta:** FR-28 (ou FR-40) precisa declarar explicitamente que o serviço de arquivos verifica posse por Empresa antes de servir qualquer blob — não é suficiente que a consulta de metadado do produto seja filtrada, se a URL do arquivo em si é servida sem essa checagem.
- **Consequência se não corrigido:** vazamento direto de imagens de produto entre clientes distintos, o tipo exato de vazamento "por ponto de junção implícito" que uma auditoria de FR-40 focada só em consultas SQL do catálogo não pegaria.

**4. Log de Acesso (FR-38) não tem regra de atribuição de Empresa para tentativas de login que falham antes de resolver a conta**
- **Localização:** FR-38; FR-40 (consequência lista "Log de Acesso" entre o que nunca cruza Empresas); FR-42 (e-mail deixa de ser único globalmente, único por Empresa).
- **Condição de disparo:** como o mesmo e-mail agora pode existir em contas de Empresas diferentes (FR-42), uma tentativa de login falha com e-mail X — que existe em Empresa A e Empresa B simultaneamente — não tem uma Empresa clara a que pertence no momento do registro (a falha acontece **antes** de a Empresa ser resolvida pela conta). O mesmo vale, de forma mais grave, para e-mail **inexistente** em qualquer Empresa: esse registro não tem dono nenhum.
- **Correção concreta:** FR-38 precisa definir explicitamente a regra: (a) tentativa com e-mail sem conta em nenhuma Empresa vai para onde? (b) tentativa com e-mail que existe em N Empresas gera N registros (um por Empresa candidata) ou fica sem registro até a senha ser conferida?
- **Consequência se não corrigido:** exatamente o tipo de "consulta que agrega sem filtrar por dono" que o usuário pediu para caçar — sem regra explícita, a implementação mais provável é uma tabela de log sem `empresa_id` obrigatório para falhas pré-resolução de conta, que different `adm`s podem acabar consultando de forma não escopada, ou que fica invisível a todos (quebrando a garantia "todo login é registrado" de FR-38).

**5. Detecção de Duplicatas (FR-19) é o ponto de junção mais provável para um scan sem filtro de Empresa**
- **Localização:** FR-19 ("agrupa por nome normalizado + dimensões equivalentes + locais coincidentes"); FR-20 (mesclagem).
- **Condição de disparo:** a lógica de FR-19 existia antes da multi-Empresa e sua natureza é varrer **todo** o catálogo em busca de nomes semelhantes — é exatamente o tipo de query que um desenvolvedor esquece de escopar por `empresa_id`, porque conceitualmente "duplicata" soa como uma operação global de qualidade de dados, não uma operação por-cliente. Se isso acontecer, o FR-20 (mesclagem) transforma um falso positivo cross-Empresa em corrupção real de dado — soma quantidade de um produto da Empresa 1 num produto da Empresa 2, ou exclui o produto de um cliente a favor do de outro.
- **Correção concreta:** FR-19/FR-20 devem declarar **explicitamente** (não só por herança do intervalo numérico "FR-4 a FR-30" citado em FR-40) que a query de agrupamento de duplicatas é escopada por `empresa_id` do ator, e que isso precisa de teste automatizado dedicado (não coberto genericamente por SM-7 sem essa menção nominal).
- **Consequência se não corrigido:** é o único fluxo de todo o PRD em que um vazamento entre Empresas se torna uma **ação destrutiva e irreversível de negócio** (mesclagem apaga o registro perdedor), não apenas uma leitura indevida — o pior caso possível de vazamento de isolamento.

**6. Semântica de "cópia por Empresa" de Categorias não está definida (eager vs. lazy)**
- **Localização:** FR-40 `[ASSUMPTION]` ("Categorias... passam a ser uma cópia editável independente por Empresa"); Open Question 16.
- **Condição de disparo:** o PRD não diz se a cópia é materializada integralmente na criação da Empresa (FR-41/FR-43) ou se é copy-on-write (linhas "globais" compartilhadas até que uma Empresa edite uma categoria específica). Se for copy-on-write mal implementado, uma query que lista categorias de uma Empresa pode inadvertidamente incluir linhas "globais" não pertencentes a nenhuma Empresa específica, ou uma edição feita por Empresa A pode aparecer para Empresa B até o momento em que B também edita aquela categoria.
- **Correção concreta:** fechar a decisão como "cópia materializada no momento da criação da Empresa, sem fallback global depois disso" — eliminando qualquer estado híbrido.
- **Consequência se não corrigido:** vazamento sutil e difícil de detectar em teste (só aparece quando duas Empresas reais coexistem e uma customiza uma categoria).

### Bloco 2 — Papel "Dono da Plataforma" e bootstrap (FR-41)

**7. FR-41 contradiz uma decisão de arquitetura que o addendum diz que continua válida**
- **Localização:** FR-41 ("Claudio acessa a área de gestão de Empresas... cria a Empresa... provisiona, na mesma ação, o primeiro `adm`"); `addendum.md` §A ("Bootstrap do primeiro `adm` via CLI dedicado, nunca endpoint HTTP" — listado explicitamente entre as decisões que "seguem válidas independente do stack de mensageria").
- **Condição de disparo:** FR-41 descreve uma UI/área de gestão dentro do próprio app (logo, um endpoint HTTP autenticado) para criar Empresas e provisionar `adm`s — exatamente o que a decisão de arquitetura herdada proíbe. O PRD não reconcilia essa contradição; trata os dois documentos como se não se tocassem.
- **Correção concreta:** decidir explicitamente — ou (a) o bootstrap do primeiro `adm` por Empresa continua sendo CLI-only, e a "área de gestão de Empresas" do Dono da Plataforma só dispara esse processo indiretamente (nunca cria a credencial em si via HTTP), ou (b) a decisão de arquitetura original é revista conscientemente para permitir esse caso específico, com as mitigações correspondentes (rate limit, MFA obrigatório no Dono da Plataforma, auditoria reforçada).
- **Consequência se não corrigido:** a superfície de ataque mais crítica do sistema (criar contas administrativas com privilégio total sobre uma Empresa) fica exposta via HTTP sem que ninguém tenha decidido conscientemente que isso é aceitável.

**8. Nenhuma exigência de MFA para o próprio papel "Dono da Plataforma"**
- **Localização:** FR-37 (MFA obrigatório só para `gestor`/`adm` **dentro de uma Empresa**); FR-41 (papel ortogonal à hierarquia `usuario`–`adm`).
- **Condição de disparo:** o Dono da Plataforma é, por definição, ortogonal à hierarquia de papéis por Empresa — logo, a regra de FR-37 ("Contas com papel `gestor` ou `adm`... devem configurar MFA") não se aplica textualmente a ele. É a conta mais poderosa do sistema (cria/desativa Empresas inteiras, provisiona `adm`s) e é a única que o PRD não obriga a MFA.
- **Correção concreta:** adicionar uma consequência explícita a FR-41 exigindo MFA (sem exceção, sem depender de realm Keycloak de terceiro) para o papel Dono da Plataforma.
- **Consequência se não corrigido:** comprometer uma única credencial de e-mail/senha (sem segundo fator) dá a um atacante controle sobre a criação/desativação de qualquer Empresa cliente da plataforma inteira.

**9. Canal de entrega da credencial inicial do "primeiro `adm`" de cada Empresa não é especificado**
- **Localização:** FR-41 ("provisiona seu primeiro `adm` (nome + e-mail)"); FR-42 (convites são gerados por `adm`/`gestor` já existentes — não cobre o caso do primeiro `adm`, que ainda não existe).
- **Condição de disparo:** o fluxo normal de convite (FR-42) pressupõe que já existe um `adm`/`gestor` na Empresa para gerar o link — mas o primeiro `adm` de uma Empresa nova é exatamente o caso em que isso ainda não é verdade (problema do ovo e da galinha). O PRD não diz como esse primeiro `adm` recebe e ativa sua credencial (convite especial? senha temporária por e-mail? link de definição de senha análogo a FR-32?).
- **Correção concreta:** FR-41 precisa descrever esse fluxo com o mesmo nível de detalhe de segurança que FR-42 dá ao convite comum (expiração, uso único, revogável).
- **Consequência se não corrigido:** essa lacuna tende a ser preenchida ad-hoc na implementação (ex. senha temporária em texto claro por e-mail, ou token sem expiração) sem passar pelo mesmo escrutínio de segurança do resto do sistema de convites.

**10. Gargalo operacional: Dono da Plataforma como pessoa única, sem papel de backup/delegação**
- **Localização:** FR-41 ("Só o Dono da Plataforma cria uma Empresa nova — sem cadastro self-service em v1"); UJ-6 (papel encarnado literalmente por "Claudio").
- **Condição de disparo:** a visão de longo prazo (`addendum.md` §D) é uma plataforma SaaS vendável a N clientes; a criação de cada Empresa e o bootstrap do primeiro `adm` dependem de uma única pessoa física, sem processo de delegação, SLA ou papel de backup definido.
- **Correção concreta:** ao menos registrar como Open Question explícita (hoje não está listada em §11) a necessidade de um segundo operador com o papel Dono da Plataforma, ou de um processo formal de onboarding que não dependa de disponibilidade de uma pessoa específica.
- **Consequência se não corrigido:** cresce como um gargalo silencioso assim que houver mais de um ou dois clientes reais simultâneos — e é exatamente o tipo de risco operacional que "N Empresas" deveria ter forçado a superfície neste PRD e não forçou.

### Bloco 3 — Convite/link de vínculo a Empresa (FR-42)

**11. Convite não é vinculado a um destinatário específico — é um bearer token**
- **Localização:** FR-42 ("um `adm`/`gestor`... gera um link ou código de convite... funciona também para e-mail pessoal... não depende de correspondência de domínio").
- **Condição de disparo:** quem quer que possua o link/código pode se autocadastrar com **qualquer** e-mail de sua escolha. Dado o contexto real de uso (obra, WhatsApp de canteiro, QR impresso e afixado fisicamente — citado no próprio addendum §I como preocupação de UX para o Ambiente de Treinamento, mas não para o convite), um link vazado (encaminhado, postado em grupo público, fotografado de um mural) dá acesso de `usuario` (que já enxerga todo o Catálogo/Estoques da Empresa, §4.2) a qualquer pessoa que o encontre.
- **Correção concreta:** considerar vincular o convite a um e-mail específico (o formulário de cadastro só aceita aquele e-mail exato) como opção padrão, com o modo "link genérico reutilizável" como escolha deliberada e não a única opção descrita.
- **Consequência se não corrigido:** o FR-42 é vendido no PRD como uma correção de segurança ("cadastro deixa de ser aberto sem contexto") mas, como especificado, troca um autocadastro público por um autocadastro "público para quem tiver o link" — que na prática de canteiro de obra tende a circular tão livremente quanto um cadastro aberto.

**12. Nenhuma menção de rate limit no resgate do convite**
- **Localização:** FR-42; FR-36 (bloqueio de força bruta é declarado só para "login por senha").
- **Condição de disparo:** se o convite é um "código" (não necessariamente um token longo em URL — FR-42 diz "link **ou** código"), nada no PRD impede tentativas repetidas de adivinhação de código válido contra o endpoint de cadastro.
- **Correção concreta:** FR-42 deve herdar explicitamente uma proteção de força bruta equivalente à de FR-36, aplicada ao endpoint de resgate de convite, e/ou exigir entropia mínima se a modalidade "código curto" for mantida.
- **Consequência se não corrigido:** um atacante pode enumerar convites válidos e se autocadastrar em Empresas às quais não foi convidado — outra forma direta de furar o isolamento total que FR-40 promete.

**13. Semântica de uso único vs. reutilizável não está resolvida**
- **Localização:** FR-42 `[ASSUMPTION]` ("mesmo padrão de token de uso único já adotado em FR-32").
- **Condição de disparo:** FR-32 é single-use por natureza (uma pessoa redefine sua própria senha). Um convite de Empresa, na prática de onboarding em lote (contratar vários almoxarifes/usuários de uma vez), tipicamente precisa ser usado por várias pessoas diferentes até expirar. O PRD copia a semântica errada do FR-32 sem qualificar.
- **Correção concreta:** decidir explicitamente se o convite é de uso único (um cadastro por link) ou multi-uso até expiração/revogação — isso muda tanto o modelo de dados quanto o risco de vazamento (multi-uso amplia o dano de um link vazado).
- **Consequência se não corrigido:** ambiguidade que a Arquitetura provavelmente resolve "errado" por padrão (ex. single-use, que quebra o UX de onboarding em lote implícito nas jornadas do produto) ou "solta demais" (multi-uso sem expiração curta, ampliando o achado 11).

**14. Herança da garantia "backend nunca aceita papel diferente do formulário" não é reconfirmada para o novo endpoint de convite**
- **Localização:** FR-3 (consequência original, pré-multi-Empresa); FR-42 (reaproveita/modifica o fluxo de FR-3).
- **Condição de disparo:** FR-42 introduz um novo parâmetro no fluxo de cadastro (o token/código de convite, que agora determina a Empresa) sem re-declarar explicitamente que a checagem de papel de FR-3 continua valendo *e* que a Empresa vinda do convite não pode ser sobrescrita por um campo de formulário alternativo.
- **Correção concreta:** repetir explicitamente em FR-42 (não só herdar implicitamente de FR-3) que nem papel nem Empresa podem vir de texto livre do cliente — só do token de convite validado no servidor.
- **Consequência se não corrigido:** um desenvolvedor implementando FR-42 como "adicione um campo ao formulário existente de FR-3" pode reintroduzir exatamente a superfície de escalação que o usuário pediu para caçar (conta criada com papel ou Empresa diferente do pretendido pelo convite).

### Bloco 4 — Migração FR-44 e colisão com a migração legada já executada

**15. FR-44 é tratado como estruturalmente igual à migração legada anterior, mas roda sobre um banco já vivo em produção**
- **Localização:** §9 Restrições ("Migração multi-Empresa... segue a mesma regra acima — corte manual, disparado por pessoa"); commits recentes do repositório confirmam que a migração legada (Firestore→Postgres) **já foi concluída** ("corte real de dados legados concluído").
- **Condição de disparo:** a linguagem de §9 para a migração original ("corte único... a partir do espelho PostgreSQL local... sem operação paralela prolongada") descreve um cutover pré-produção clássico. FR-44, ao contrário, precisa rodar contra um Postgres **já em produção, recebendo escrita real todos os dias** (Movimentações, Pedidos, cadastros). O PRD não distingue os dois casos nem define o que acontece com uma escrita que chega **durante** a execução do script de FR-44 (ex. um `INSERT` em `produtos` no meio de um `UPDATE ... SET empresa_id = ...` em massa).
- **Correção concreta:** declarar explicitamente uma janela de manutenção/modo leitura (ou write-freeze) para a duração de FR-44, e não apenas "corte manual disparado por pessoa" — essa frase não diz nada sobre concorrência.
- **Consequência se não corrigido:** linhas escritas durante a migração podem ficar sem `empresa_id`, tornando-se invisíveis (fail-closed, na melhor hipótese) ou vazando entre Empresas (fail-open, na pior) assim que uma segunda Empresa existir.

**16. FR-44 não tem garantia de resumabilidade, ao contrário do que o PRD exige para importação de planilha (FR-10)**
- **Localização:** FR-44 (sem menção de comportamento em caso de interrupção); comparar com UJ-3/FR-10 ("se a importação for interrompida no meio, a próxima tentativa mostra quais linhas já foram salvas, sem reprocessar nem duplicar").
- **Condição de disparo:** FR-44 aplica uma operação em massa sobre **todo** o dado de produção (produtos, estoques, movimentações, pedidos, categorias, usuários) — uma operação de risco categoricamente maior do que uma importação de planilha — mas é o único fluxo de migração em massa do PRD sem exigência explícita de idempotência/retomada.
- **Correção concreta:** adicionar a FR-44 uma consequência equivalente à de FR-10: se interrompida, a reexecução detecta o que já foi migrado e não duplica/corrompe.
- **Consequência se não corrigido:** uma falha parcial de FR-44 deixa o sistema em estado misto (parte dos dados com `empresa_id`, parte sem) sem um caminho seguro e testado de retomada — na prática, provavelmente forçando um "improviso" manual sob pressão, exatamente o que o processo de execução (`addendum.md` §C) diz que nunca deveria acontecer ("ambiguidade gera bloqueio registrado, nunca improviso").

**17. Nenhum plano de rollback próprio para FR-44 (o da migração legada já era só uma nota, não fechado)**
- **Localização:** §9 (`[NOTE FOR PM]` "exige plano de rollback e janela de baixo uso" — anexado à migração original, não repetido para FR-44).
- **Condição de disparo:** mesmo o rollback da migração ORIGINAL nunca foi fechado como especificação (ficou como nota para o PM). FR-44 empilha uma segunda migração de alto risco sobre essa mesma lacuna, sem sequer repetir a nota.
- **Correção concreta:** FR-44 precisa de seu próprio `[NOTE FOR PM]` ou `[ASSUMPTION]` de plano de rollback (ex. backup ponto-a-ponto antes de disparar, script de reversão testado).
- **Consequência se não corrigido:** se a migração corromper dado de produção real (2745+ produtos, movimentações e pedidos reais), não há especificação de como voltar atrás.

**18. Natureza técnica de FR-44 é ambígua: "corte único" (import) vs. ALTER/backfill em banco vivo**
- **Localização:** §9 ("fonte direta é o espelho PostgreSQL local... corte único" — linguagem herdada da migração original) aplicada, sem qualificação, também a FR-44.
- **Condição de disparo:** a migração original parece ser um IMPORT de um espelho para um schema-alvo novo (baixo acoplamento com o sistema já rodando). FR-44, ao vincular dado **já residente na tabela de produção** a uma Empresa, é estruturalmente um `ALTER TABLE ADD COLUMN empresa_id NOT NULL` + backfill + (possivelmente) novas constraints/FKs — uma operação com características de lock/duração muito diferentes (pode bloquear leituras/escritas concorrentes dependendo da versão do Postgres e do padrão de constraint usado), e que a Arquitetura precisa dimensionar como tal, não como "mais um corte único" genérico.
- **Correção concreta:** nomear explicitamente essa distinção técnica em §9 ou remeter para a Arquitetura decidir e documentar o padrão de migração online-safe (ex. adicionar coluna nullable, backfill em lotes, só então tornar NOT NULL).
- **Consequência se não corrigido:** risco de indisponibilidade não planejada durante a execução de FR-44 em produção, subestimado por analogia errada com a migração anterior.

**19. Acoplamento entre tagging retroativo de dado (FR-44) e provisionamento de uma Empresa nova (Treinamento) na mesma migração**
- **Localização:** FR-44 ("Uma Empresa 'Ferreira Costa - Treinamento' (FR-43) é criada como parte desta mesma migração").
- **Condição de disparo:** duas operações de natureza e risco diferentes — (a) retag de todo dado real existente, (b) criação de uma Empresa nova com dados de exemplo — são declaradas como parte de "uma migração única", sem dizer se falha em (b) reverte (a) ou se são etapas independentes/retomáveis separadamente.
- **Correção concreta:** separar as duas etapas explicitamente (mesmo que disparadas na mesma operação humana), com falha de uma não bloqueando nem exigindo rollback da outra.
- **Consequência se não corrigido:** aumenta o raio de explosão de qualquer falha durante a execução de FR-44 — um problema na criação dos dados de exemplo do Ambiente de Treinamento não deveria colocar em risco o corte de dados reais da Ferreira Costa.

**20. A conta `adm` migrada (Ferreira Costa) e a conta "Dono da Plataforma" podem colapsar na mesma identidade/credencial**
- **Localização:** FR-44 ("a conta `adm` hoje única e global — `claudio.bezerra@ferreiracosta.com.br` — passa a ser o `adm` dessa Empresa especificamente"); Open Question 14 (onde vive o papel Dono da Plataforma no modelo de dados).
- **Condição de disparo:** se "Dono da Plataforma" acabar sendo modelado como um atributo/flag sobre a mesma linha de `Usuario` que também é o `adm` da Empresa Ferreira Costa (em vez de uma identidade/conta inteiramente separada), então comprometer a credencial usada no dia a dia como `adm` de uma Empresa específica (superfície de ataque igual à de qualquer outro cliente) também compromete o controle da plataforma inteira (criar/desativar qualquer Empresa).
- **Correção concreta:** FR-41 ou a resolução da Open Question 14 devem exigir explicitamente que a identidade "Dono da Plataforma" seja uma conta separada da conta `adm` de qualquer Empresa individual (inclusive a da própria Ferreira Costa pós-FR-44), com sua própria credencial e MFA (achado 8).
- **Consequência se não corrigido:** a garantia de "isolamento total entre Empresas" (FR-40), o pilar central desta versão do PRD, teria uma única credencial cujo comprometimento a anula por completo.

### Achados sistêmicos adicionais

**21. SM-7 depende de um mecanismo de imposição ainda não escolhido, e a opção mais fraca das três listadas é a mais provável de ser escolhida por padrão**
- **Localização:** §8 NFR "Isolamento multi-Empresa" (lista três mecanismos possíveis — coluna `empresa_id` + filtro centralizado, isolamento por schema, RLS — como decisão em aberto de Arquitetura); SM-7 ("100% das consultas... verificável por teste automatizado").
- **Condição de disparo:** das três opções, "coluna + filtro centralizado" é a única que depende inteiramente de disciplina do desenvolvedor em cada nova query (nenhuma proteção em nível de banco impede um `SELECT` sem `WHERE empresa_id = ...`); é também, historicamente, a opção mais barata de implementar primeiro — logo a mais provável de ser escolhida sob pressão de prazo, apesar de ser a mais frágil frente exatamente aos achados 3, 4 e 5 acima.
- **Correção concreta:** já que o PRD declara este NFR como não-negociável ("não é uma preferência de implementação"), ele deveria recomendar (não só permitir) um mecanismo fail-closed a nível de banco (RLS) como padrão mínimo, deixando "schema-per-tenant" como alternativa mais cara, e desqualificando "só filtro de aplicação" como suficiente sozinho para o rigor de *launch* declarado.
- **Consequência se não corrigido:** SM-7 vira uma métrica que mede a cobertura da suíte de testes, não a garantia real do sistema — um endpoint novo, uma query de relatório ad-hoc, ou uma correção de bug futura pode reintroduzir vazamento sem que nenhum teste existente pegue, até que uma segunda Empresa real exista e alguém note.

**22. Desativação de Empresa (FR-41) não garante corte de sessão já ativa, ao contrário do padrão já definido para desativação de conta individual (FR-31)**
- **Localização:** FR-41 (consequência: "Desativar uma Empresa... impede login de qualquer conta vinculada a ela, sem apagar dados"); comparar com FR-31 ("Rebaixamento perde acesso já na próxima requisição").
- **Condição de disparo:** FR-41 só menciona bloqueio de **login futuro** — não repete a garantia de FR-31 de que sessões já ativas são cortadas na próxima requisição. Se a implementação seguir a letra do FR, todos os usuários de uma Empresa desativada (ex. cliente que cancelou contrato, ou Empresa suspeita de violação) continuam operando normalmente até suas sessões expirarem naturalmente (até 2h, FR-1).
- **Correção concreta:** estender explicitamente a FR-41 a mesma garantia de corte imediato de sessão ativa já especificada em FR-31.
- **Consequência se não corrigido:** uma desativação de Empresa motivada por segurança (ex. suspeita de comprometimento) não tem efeito imediato — contradiz a urgência implícita de "desativar" como ação de contenção.

## Observação final

Os quatro vetores citados pelo usuário não são apenas plausíveis — cada um aponta para uma decisão que o PRD atual deixa implícita ou não resolve, e ao menos um (achado 5, Duplicatas) descreve um caminho onde o vazamento vira **corrupção de dado irreversível**, não só leitura indevida. Nenhum destes achados exige inventar cenário — todos decorrem diretamente do texto e das lacunas do próprio `prd.md`/`addendum.md` já escritos. Recomendo tratar os achados 1, 2, 5, 7, 8, 15, 16, 17 e 20 como bloqueantes antes de gerar Épicos/Stories a partir desta seção, dado o rigor de *launch* que o próprio PRD reivindica.
