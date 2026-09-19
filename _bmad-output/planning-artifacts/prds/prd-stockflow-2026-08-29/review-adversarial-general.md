# Revisão Adversarial — Rodada "Feedback de Treinamento pós Multi-Empresa" (FR-45 a FR-52)

**Alvo:** `prd.md` (área nova/revisada: FR-45–FR-52; revisões de FR-6, FR-8, FR-9, FR-12, FR-21, FR-22, FR-25, FR-40, FR-43; §7 SM-9/SM-2; §11 itens 17–25) e `addendum.md` (§F, §G, §H, §I), versão atualizada em 2026-09-19.
**Lente:** Adversarial (única lente solicitada).
**Contexto de risco:** esta rodada não é mais "definir um modelo novo" — é a **segunda camada de mudança estrutural sobre um sistema que, pelo histórico do próprio repositório (`git log`), já concluiu a migração multi-Empresa e já opera em produção real na Ferreira Costa** (commits "story 9-4: migração da Ferreira Costa para o modelo multi-Empresa", "fix(pedidos): escopa por empresa a subquery de contagem de itens"). Isso muda o risco de "FR mal escrito" para "FR que introduz uma invariante obrigatória nova (Filial, Lote, template, nome mínimo) sobre dados que já existem e não a satisfazem".

## Veredito

O lote de mudanças é bem-intencionado e claramente rastreável ao feedback real de treinamento (Karla, Ricardo), mas tem um padrão sistemático de risco que se repete em quase todos os FRs novos: **cada mudança é especificada como se o sistema fosse greenfield**, sem tratar o caso de dados já existentes em produção que não satisfazem a nova regra. Isso já havia sido tratado com rigor "launch" na rodada anterior para `empresa_id` (FR-44 detalha backfill, rollback, resumabilidade) — nesta rodada, o mesmo cuidado não se repete para Filial (FR-51), Lote (FR-47), template obrigatório (FR-9) nem nome mínimo de 10 caracteres (FR-8). Além disso, a reversão de regra de negócio mais sensível da rodada — reserva de saldo (FR-50) — não foi propagada para dois FRs pré-existentes que continuam podendo furá-la por completo (FR-14/FR-15, Baixa/Transferência direta), o que torna a garantia central de FR-50 falsa como especificada. E a obrigatoriedade de template (FR-9) esbarra numa limitação concreta e verificável nos próprios anexos do PRD: nem toda Categoria tem um template aplicável.

Nenhum destes é um problema de redação — são decisões de produto/migração que faltam ser tomadas antes que Épicos/Stories sejam gerados a partir deste PRD.

## Achados

### Bloco 1 — FR-50 (reserva de saldo) contradiz FRs que não foram revisados

**1. [Crítico] FR-14/FR-15 (Baixa/Transferência direta) não foram revisados para respeitar a reserva de saldo**
- **Localização:** §4.5 FR-14, FR-15 (texto idêntico ao pré-FR-50, nenhuma marca de revisão); FR-50 ("saldo... indisponível para qualquer outro Pedido/Carrinho").
- **Condição de disparo:** FR-50 promete que o saldo reservado por um Pedido pendente fica "indisponível para qualquer outro Pedido/Carrinho" — mas nunca menciona Baixa (FR-14) e Transferência (FR-15), que são um caminho de escrita de estoque totalmente diferente, acionado diretamente pelo Almoxarife fora do fluxo de Pedido. Como FR-14/FR-15 não foram tocados nesta rodada, eles continuam operando sobre "quantidade disponível" no sentido antigo (saldo total, não saldo total menos reservado) — nada no texto impede um Almoxarife de dar baixa ou transferir exatamente a porção já reservada por um Pedido pendente de outra pessoa.
- **Correção concreta:** FR-14 e FR-15 precisam de uma nova consequence explícita: rejeitam quantidade que excede o saldo **disponível** (não o saldo total), tratando a fração reservada como indisponível também para esses dois fluxos.
- **Consequência se não corrigido:** a garantia central de FR-50 é falsa como especificada — um Pedido pode ser aprovado, revalidar "sua própria reserva" (FR-25) e ainda assim falhar, porque a reserva nunca foi de fato oponível a Baixa/Transferência. Isso também invalida a afirmação de FR-25 de que, com reserva em vigor, "falha só deveria acontecer por bug de reserva, não por concorrência normal" — falha por ação direta de Almoxarife continua sendo um caminho normal e não coberto.

**2. [Alto] UJ-2 não foi marcada como revisada e ainda narra o cenário do modelo ANTIGO como fluxo esperado**
- **Localização:** §2.3 UJ-2 ("o sistema revalida no servidor o estoque de cada item; se algum item não tem mais estoque suficiente... selo Solicitado: 10m · Disponível: 4m").
- **Condição de disparo:** essa narrativa é exatamente o comportamento esperado sob o modelo ANTIGO (saldo livre até aprovação, podendo ter sido consumido por concorrência normal) — é o cenário que SM-2 revisado diz que "passa a indicar bug de reserva, não concorrência normal" sob FR-50. UJ-2 não tem nenhuma nota `(revisado nesta versão)` nem qualquer ressalva, então um leitor que só lê as Jornadas (não os FRs) sai com o modelo mental errado sobre quando esse selo aparece.
- **Correção concreta:** anotar UJ-2 explicitamente: sob FR-50, esse selo só deveria aparecer por (a) bug de reserva ou (b) uma Baixa/Transferência direta ter consumido saldo reservado (achado 1) — nunca por concorrência normal entre Pedidos.
- **Consequência se não corrigido:** Épicos/Stories gerados a partir de UJ-2 tendem a implementar o "aviso de item insuficiente" como um caso de negócio normal e recorrente, quando FR-25/FR-50 dizem que ele deveria ser raro/excepcional — divergência de expectativa entre UX e a regra de negócio real.

**3. [Médio] Ambiguidade de terminologia "Reservar" (FR-21) ficou reconhecida mas não virou Pergunta em Aberto formal**
- **Localização:** FR-21 NOTE FOR PM ("o rótulo 'Reservar' são anteriores à reserva de saldo de verdade... pode precisar de ajuste").
- **Condição de disparo:** o próprio PRD identifica a confusão (botão "Reservar" no Carrinho não trava nada; só o envio do Pedido trava, via FR-50) mas resolve isso com uma nota solta dentro de FR-21, não com uma entrada em §11 Perguntas em Aberto — ao contrário do padrão que o próprio documento usa para outras ambiguidades desta rodada (itens 17–25).
- **Correção concreta:** promover essa nota a uma Pergunta em Aberto numerada, para garantir que UX trate o rótulo antes do lançamento.
- **Consequência se não corrigido:** risco de a inconsistência de terminologia ser esquecida por estar "enterrada" dentro de um FR já implementado, em vez de aparecer na lista de pendências que orienta a próxima fase.

**4. [Médio] NFR "Concorrência" (§8) não foi atualizado para o novo invariante de reserva**
- **Localização:** §8 NFR Concorrência ("toda escrita dependente de estado lido previamente é atômica no servidor (saldo de estoque, unicidade de código/e-mail/nome de estoque, aprovação concorrente de pedido)").
- **Condição de disparo:** o texto normativo do NFR continua falando de "saldo de estoque" genérico, sem diferenciar saldo disponível vs. reservado — a garantia real introduzida por FR-50 (reserva atômica que nunca deixa disponível ir a negativo, mesmo com Carrinhos/Pedidos concorrentes) só está capturada em SM-9 (métrica), não no NFR que a SM deveria validar.
- **Correção concreta:** adicionar "saldo reservado vs. disponível (FR-50)" explicitamente à lista de estados que exigem atomicidade em §8.
- **Consequência se não corrigido:** um NFR desatualizado é fonte comum de gap entre o que a Arquitetura testa e o que o produto realmente promete.

**5. [Baixo] `addendum.md` §A descreve revalidação via `SELECT...FOR UPDATE` sobre um modelo de saldo único, não reconciliado com saldo disponível/reservado**
- **Localização:** `addendum.md` §A ("Revalidação de estoque sempre com `SELECT ... FOR UPDATE`... na mesma transação").
- **Condição de disparo:** essa é uma decisão de arquitetura herdada listada como "ainda válida independente do stack de mensageria" — mas foi escrita para um modelo de saldo único (sem reserva). Sob FR-50, a solução provavelmente precisa de uma coluna/estado de reserva separado, não apenas lock pessimista sobre a quantidade total.
- **Correção concreta:** marcar esse trecho do addendum como "requer revisão à luz de FR-50" para não ser copiado cegamente pela Arquitetura.
- **Consequência se não corrigido:** Arquitetura implementa lock pessimista sobre saldo total (suficiente para o modelo antigo) sem perceber que o modelo de reserva exige um design de dado diferente.

**6. [Médio] FR-50/SM-9 não tratam da interação de reserva com Importação em massa (FR-10/11) e Mesclagem de Duplicatas (FR-20)**
- **Localização:** FR-50; FR-11 (reimportação atualiza saldo por código); FR-20 (mesclagem soma quantidades).
- **Condição de disparo:** se uma reimportação de planilha ou uma mesclagem de duplicatas alterar a quantidade de um Produto/Estoque que tem saldo reservado por Pedidos pendentes, o PRD não diz o que acontece com a reserva (ela é preservada? recalculada? pode ficar inconsistente com o novo total?). É a mesma classe de furo do achado 1, em outros caminhos de escrita de estoque que já existiam antes de FR-50.
- **Correção concreta:** FR-50 (ou FR-11/FR-20) precisa declarar que toda alteração de saldo por qualquer via preserva/recalcula a reserva de forma consistente, e não apenas os dois fluxos citados explicitamente (Pedido/Carrinho).
- **Consequência se não corrigido:** saldo disponível/reservado pode divergir do real após uma importação ou mesclagem, sem que nenhum FR trate esse caso.

### Bloco 2 — FR-47 (Lote/Validade): decisão adiada além do razoável

**7. [Crítico] FR-14 e FR-15 não mencionam Lote nenhuma vez, apesar do modelo de saldo ter mudado estruturalmente**
- **Localização:** §4.5 FR-14, FR-15 (texto não tocado nesta rodada); Glossário §3 ("saldo é rastreado por Lote... não mais uma quantidade única por par Produto/Estoque").
- **Condição de disparo:** o Glossário afirma que a mudança de modelo de saldo (para Lote) é geral — mas só FR-25 (aprovação de Pedido) menciona explicitamente "debita do(s) Lote(s)". FR-14/FR-15, que são os dois FRs mais diretamente afetados (são literalmente "operações de saída/movimento de saldo"), não dizem se uma Baixa/Transferência exige escolher um Lote, distribui automaticamente entre Lotes, ou ignora Lote e decrementa um "saldo agregado" — o que contradiria o próprio Glossário.
- **Correção concreta:** revisar FR-14/FR-15 explicitamente para declarar como cada operação interage com Lote (seleção manual, FEFO, ou agregado com rateio automático).
- **Consequência se não corrigido:** dois dos FRs mais usados no dia a dia (toda baixa e toda transferência) ficam sem definição de comportamento sob o novo modelo de dado — o tipo de lacuna que gera ambiguidade de implementação bloqueante em Story, não uma nuance de Arquitetura.

**8. [Alto] Deferir FEFO para Arquitetura descarta uma decisão de negócio, não só de mecanismo**
- **Localização:** FR-47 NOTE FOR PM ("FEFO automático... vs. escolha manual pelo Almoxarife... não está decidido... O PRD fixa só a capacidade de rastrear, não o algoritmo de consumo").
- **Condição de disparo:** rastrear validade só tem valor de negócio se o sistema (ou o processo que ele impõe) evitar que lotes mais antigos vençam parados enquanto lotes novos são consumidos primeiro — esse é precisamente o motivo original de pedir rastreamento de Lote/Validade (evitar perda por vencimento). Adiar a escolha entre FEFO automático e escolha manual para "Arquitetura" trata uma política de negócio (quem decide o que sai primeiro, e se o sistema força isso) como um detalhe técnico — Arquitetura não é o fórum certo para essa escolha, é decisão de produto.
- **Correção concreta:** o PM deveria fechar ao menos a direção geral nesta rodada (ex. "sugestão de FEFO com override manual pelo Almoxarife, nunca bloqueio duro") e deixar só o *como implementar* para Arquitetura.
- **Consequência se não corrigido:** há risco real de a funcionalidade ser entregue (rastreamento existe) sem entregar o benefício que a motivou (redução de perda por vencimento), porque ninguém foi forçado a decidir se o sistema de fato prioriza consumo de lotes vencendo.

**9. [Médio] Não está definido se Lote com validade vencida pode ser consumido**
- **Localização:** FR-47 ("Lote com Data de Validade vencida não impede a existência do saldo, mas precisa ser sinalizado... regra exata de alerta/bloqueio é decisão de Arquitetura").
- **Condição de disparo:** "sinalizado" na consulta é uma coisa; permitir ou não que uma Baixa/Transferência/aprovação de Pedido efetivamente retire material de um lote vencido é outra, com implicação de compliance/segurança de obra (dependendo do material) que não é meramente técnica.
- **Correção concreta:** decidir explicitamente se consumo de lote vencido é permitido com confirmação, bloqueado, ou permitido silenciosamente — hoje nenhuma das três está descartada.
- **Consequência se não corrigido:** comportamento por padrão fica a critério de quem implementa, podendo permitir uso de material vencido sem qualquer fricção.

**10. [Alto] Ambiente de Treinamento (FR-43) e Migração real (FR-44) não foram revisados para contemplar Lote/Filial como estruturas agora obrigatórias**
- Ver Bloco 6, achado 16 — tratado em conjunto por ser o mesmo padrão de risco (estrutura nova obrigatória vs. dado/ambiente já existente).

### Bloco 3 — FR-8 revisado vs. UJ-3/FR-10/FR-11

**11. [Médio] §10 Integrações e Dependências ainda descreve a planilha de importação sem colunas de Lote/Validade**
- **Localização:** §10 ("Excel/CSV — modelo único padronizado de colunas (nome, código, categoria, dimensões+unidade, **quantidade, estoque**, observações)").
- **Condição de disparo:** essa lista de colunas não foi atualizada nesta rodada — continua descrevendo o modelo antigo (quantidade + estoque direto), incompatível com o novo modelo de saldo por Lote (FR-47). O PRD já registra essa lacuna como Pergunta em Aberto 25, mas o texto normativo de §10 segue afirmando o modelo antigo como se estivesse decidido, em vez de sinalizar a pendência no próprio corpo do documento.
- **Correção concreta:** marcar a linha de §10 com a mesma ressalva de OQ-25 (colunas de Lote/Validade a definir), para que quem ler só §10 não assuma o modelo antigo como fechado.
- **Consequência se não corrigido:** um leitor de §10 isoladamente (comum em fases de Arquitetura, que costuma revisitar Integrações/Dependências separadamente de Perguntas em Aberto) implementa o formato antigo de planilha sem saber que está pendente de decisão.

**12. [Baixo] Nenhuma contradição direta entre FR-8 revisado e UJ-3 — mas falta reforço cruzado**
- **Localização:** UJ-3 (importação em massa) descreve corretamente o fluxo de FR-10/FR-11, que continua distinto do cadastro manual (FR-8) — coerente. Falta apenas uma nota em UJ-3 ou FR-10 remetendo à pendência de Lote na importação (achado 11), hoje só visível em §11.

### Bloco 4 — Glossário "Estoque (Depósito)"

**13. [Médio] A unificação de termos não define se "Depósito" é rótulo de UI ou só sinônimo de documento**
- **Localização:** §3 Glossário ("Estoque (Depósito) — local físico..."); FR-51 ("Confirmado com o usuário: Depósito é o mesmo conceito de Estoque já existente, ganhando Filial como atributo/pai").
- **Condição de disparo:** o parêntese "(Depósito)" aparece só no cabeçalho do Glossário; a palavra "Depósito" nunca é usada como rótulo de tela, campo ou endpoint em nenhum outro lugar do PRD. Isso deixa ambíguo se a unificação é (a) puramente terminológica para desambiguar quem escreve o PRD/Stories, sem nunca aparecer para o usuário final, ou (b) um sinal de que a UI deveria efetivamente chamar essa entidade de "Depósito" (termo que passa a fazer mais sentido intuitivo justamente por causa da nova hierarquia de Filial, que soa mais como "Filial → Depósito" do que "Filial → Estoque"). Nenhuma Pergunta em Aberto cobre essa decisão de nomenclatura de interface.
- **Correção concreta:** adicionar uma nota explícita — "Depósito é só sinônimo de documentação, o rótulo de UI/API continua sendo 'Estoque'" (ou o inverso, se for essa a intenção) — e, se relevante, uma entrada em §11.
- **Consequência se não corrigido:** risco concreto e barato de virar uma decisão implícita e inconsistente entre UX e Arquitetura (um usa "Estoque" na tela, outro usa "Depósito" no banco/API), o tipo de mismatch que gera retrabalho de Story.

### Bloco 5 — FR-9 (template obrigatório) sem contrapeso reconhecido

**14. [Crítico] Nem toda Categoria tem um template de Nomenclatura aplicável — tornar a escolha obrigatória pode bloquear cadastro de categorias inteiras**
- **Localização:** FR-9 ("seleção de um template passa a ser obrigatória... não existe mais caminho de cadastro sem template selecionado"); `addendum.md` §G (28 templates, cobrindo só ~9 famílias: cabos, elétrica, hidráulica, tubo, perfil, ferragem, mat. construção/fixação, telha/calha); `addendum.md` §H (25 Categorias, incluindo `06.001 Materiais de Escritório`, `06.002 Materiais de Limpeza`, `11.001 Combustíveis e Lubrificantes`, `12.001 Verbas, Licenças e Alvarás`, `12.002 Impostos`, `13.001 Equipamentos Esportivos e Recreativos` — nenhuma delas coberta por qualquer um dos 28 templates listados).
- **Condição de disparo:** um Almoxarife tenta cadastrar um Produto de uma dessas categorias sem template correspondente. Com FR-9 tornando a escolha obrigatória e sem nenhum template "genérico"/catch-all definido, o cadastro fica literalmente bloqueado até alguém (via FR-49, CRUD de Templates) criar um template ad-hoc para aquela categoria — o que nem FR-9 nem FR-49 preveem como passo necessário de rollout.
- **Correção concreta:** ou (a) FR-9 define um template genérico obrigatório de fallback para categorias sem subtipo específico, ou (b) FR-9 permite explicitamente cadastro sem template quando a Categoria não tem nenhum template associado (reabrindo uma via de texto livre condicional, contrariando a decisão "sem exceção" atual). Qualquer uma das duas precisa ser uma decisão consciente, hoje ausente.
- **Consequência se não corrigido:** a mudança de UX mais arriscada da rodada (a própria pergunta do usuário já antecipa isso) não tem contrapeso nenhum registrado — nem em FR-9, nem em Non-Goals, nem em Perguntas em Aberto — apesar de ser demonstrável, com os próprios anexos do PRD, que ela quebra fluxos reais de cadastro.

**15. [Alto] Produtos já cadastrados sem template (comportamento válido até esta rodada) ficam num limbo não definido**
- **Localização:** FR-9 ("Editar um Produto com template aplicado revalida o nome contra esse template").
- **Condição de disparo:** a consequência só cobre o caso de um Produto que JÁ tem template aplicado. Produtos cadastrados antes desta mudança (a maioria do catálogo real da Ferreira Costa, dado que template era opcional) não têm template algum — o PRD não diz se editá-los agora exige escolher um template retroativamente, se ficam permanentemente isentos da regra, ou se a edição é bloqueada até essa escolha.
- **Correção concreta:** FR-9 precisa de uma consequence explícita sobre Produtos legados sem template.
- **Consequência se não corrigido:** mesmo padrão do achado 16 — regra nova tratada como se o catálogo fosse vazio.

### Bloco 6 — Outras contradições e riscos (padrão sistêmico da rodada)

**16. [Crítico] Estruturas novas obrigatórias (Filial, Lote) não têm plano de migração para a Ferreira Costa já em produção**
- **Localização:** FR-51 ("todo Estoque... passa a pertencer a exatamente uma Filial"); FR-47 (saldo passa a ser somado a partir de Lotes); FR-44 (migração da Ferreira Costa para multi-Empresa, já com plano detalhado de backfill/rollback/resumabilidade); histórico de commits do repositório confirmando que a migração de FR-44 já rodou em produção.
- **Condição de disparo:** ao contrário de FR-44 (que exige explicitamente "coluna `empresa_id` anulável primeiro, backfill em lote... plano de rollback... segura contra interrupção"), FR-51 e FR-47 são escritos como se estivessem definindo um schema para uma instalação nova — nenhum dos dois diz o que acontece com os Estoques e saldos **já existentes** na Ferreira Costa (que, pelo histórico do projeto, é uma instalação real e ativa): qual Filial "default" cada Estoque legado recebe, e qual Lote "default"/sem validade cada saldo legado recebe. `[NOTE FOR PM]` de FR-51 pergunta se "toda Empresa **nova**" precisa de Filial padrão — mas não cobre a Empresa que já existe e já tem Estoques sem Filial.
- **Correção concreta:** estender a FR-51/FR-47 o mesmo rigor de migração já exigido de FR-44 — plano de backfill explícito, decisão sobre Filial/Lote "default" para dado legado, e confirmação de que a mudança de schema não deriva em Estoques temporariamente "órfãos" de Filial (o que, sob NFR de isolamento e sob as regras já existentes de unicidade de nome por Filial, pode gerar comportamento indefinido).
- **Consequência se não corrigido:** o mesmo tipo de risco que motivou o rigor "launch" da rodada anterior (mudança estrutural sob banco vivo em produção) se repete aqui sem o mesmo cuidado — e, diferente da rodada anterior, não há sequer um `[NOTE FOR PM]` reconhecendo o risco.

**17. [Médio] Mesmo padrão de risco de retrocompatibilidade em FR-8 revisado (nome mínimo 10 caracteres)**
- **Localização:** FR-8 ("Nome deve ter no mínimo 10 caracteres (e no máximo 255, como já era)").
- **Condição de disparo:** produtos já cadastrados com nomes menores que 10 caracteres (permitido antes desta regra) não têm tratamento definido — a regra é escrita como validação de cadastro/edição, mas nada diz se um Produto legado nessas condições fica bloqueado para qualquer edição futura até alguém alongar o nome, ou se a regra vale só para nomes novos.
- **Correção concreta:** adicionar uma consequence explícita sobre Produtos legados abaixo do novo mínimo.
- **Consequência se não corrigido:** mesmo tipo de ambiguidade de implementação do achado 16/15, em escala potencialmente maior (nome curto é mais comum que "sem template").

**18. [Médio] SM-1/SM-7/FR-40 não foram atualizados para citar nominalmente as entidades novas desta rodada**
- **Localização:** §7 SM-1 ("Valida FR-2, FR-3, FR-8, FR-10, FR-13, FR-14, FR-15, FR-18, FR-20, FR-25, FR-31, FR-33, FR-34"); SM-7 ("100% das consultas de Catálogo, Estoques, Movimentações, Pedidos, Log de Acesso, Normalização... e Gestão de Contas... excluem dado de qualquer outra Empresa"); FR-40 (lista de áreas sob isolamento, escrita antes desta rodada).
- **Condição de disparo:** FR-45 a FR-52 introduzem endpoints novos restritos a papel (`almoxarife+` para FR-47, `adm+` para FR-48/49/51/52) e entidades novas sujeitas a isolamento por Empresa (Filial, Centro de Custo, Lote, Categoria, Template) — cada FR novo reafirma a obrigação de isolamento individualmente (e §0 afirma isso de forma geral), mas as métricas que dão rastreabilidade concreta de teste automatizado (SM-1, SM-7) não foram estendidas para citar essas entidades nominalmente.
- **Correção concreta:** atualizar as listas de FRs validados em SM-1 e a lista de domínios em SM-7 para incluir FR-45 a FR-52 explicitamente.
- **Consequência se não corrigido:** uma suíte de testes construída literalmente a partir do texto das métricas (prática comum ao gerar Stories de QA a partir de SM) pode não cobrir isolamento/autorização das entidades novas, mesmo que a intenção declarada em prosa seja de cobertura total.

**19. [Baixo] SM-9 tem redação ambígua sobre o invariante que promete testar**
- **Localização:** SM-9 ("nenhum segundo Pedido ou item de Carrinho consegue levar o saldo reservado abaixo de zero").
- **Condição de disparo:** o invariante real de um sistema de reserva é o saldo **disponível** nunca ficar negativo (reservas não podem exceder o saldo físico existente) — "saldo reservado abaixo de zero" não corresponde a nenhuma falha reconhecível nesse modelo. Como é uma métrica com promessa de "verificável por teste automatizado", a imprecisão pode levar a um teste que verifica a condição errada.
- **Correção concreta:** reescrever para "nenhuma combinação de reservas ativas excede o saldo físico existente (saldo disponível nunca é negativo)".
- **Consequência se não corrigido:** métrica "verificada por teste automatizado" que não testa, na prática, o cenário de concorrência que motivou FR-50.

**20. [Médio] FR-6 (revisado) e FR-50 descrevem a mesma tela sem requisitos reconciliados**
- **Localização:** FR-6 consequence ("toda listagem... mostra... estoque total (soma across Estoques)... como colunas próprias"); FR-50 consequence ("Consulta de Catálogo/Estoque (FR-6/FR-7) passa a mostrar saldo disponível separado do saldo reservado").
- **Condição de disparo:** as duas revisões, feitas na MESMA rodada, descrevem a mesma superfície de UI (listagem/detalhe de Catálogo) com vocabulário diferente — "estoque total" (FR-6) vs. "disponível separado de reservado" (FR-50) — sem dizer se "total" em FR-6 é a soma bruta (disponível+reservado) exibida ao lado da quebra, ou se FR-6 ficou desatualizado em relação a FR-50 (escrito depois, mais abaixo no documento).
- **Correção concreta:** unificar a redação — ex. "estoque total = disponível + reservado; ambos exibidos separadamente, com o total como soma dos dois".
- **Consequência se não corrigido:** ambiguidade de layout de tela gerada por dois FRs da mesma rodada não terem sido cruzados um contra o outro antes de fechar o documento.

**21. [Baixo] Pergunta em Aberto 24 (código de Categoria de 8 caracteres) parece resolvível sem esperar Arquitetura**
- **Localização:** §11 item 24; `addendum.md` §H (todos os 25 códigos existentes seguem o formato `XX.XXX`, ou seja, exatamente 6 caracteres).
- **Condição de disparo:** o PRD registra como pendente uma verificação (todos os códigos cabem em 8 caracteres?) que já é respondível por inspeção direta do próprio Anexo H incluído no mesmo documento — 6 ≤ 8 para todos os 25 casos listados.
- **Correção concreta:** fechar a Pergunta 24 agora ("confirmado: todos os códigos atuais cabem no limite de 8 caracteres") em vez de empurrá-la para a Arquitetura.
- **Consequência se não corrigido:** nenhuma prática — é só uma oportunidade barata de reduzir o número de pendências antes de Épicos/Stories.

## Resumo por severidade

- **Crítico (4):** #1 (FR-14/15 furam a reserva de FR-50), #7 (FR-14/15 ignoram Lote), #14 (template obrigatório sem fallback para categorias sem template), #16 (Filial/Lote sem plano de migração para produção já viva).
- **Alto (5):** #2 (UJ-2 desatualizada), #8 (FEFO é decisão de negócio disfarçada de decisão técnica), #10 (Treinamento/Migração não revisados para Lote/Filial — mesmo tema do #16), #15 (Produtos legados sem template), #6 (reserva vs. importação/mesclagem).
- **Médio (7):** #3, #4, #9, #11, #13, #17, #18, #20 (nota: são 8 itens, contagem ajustada — ver lista completa acima).
- **Baixo (3):** #5, #12, #19, #21.

## Top 5 para ação imediata antes de gerar Épicos/Stories

1. Revisar FR-14/FR-15 para respeitar saldo reservado (FR-50) e para declarar interação com Lote (FR-47) — hoje são os dois FRs mais usados no dia a dia e os mais desatualizados da rodada (#1, #7).
2. Decidir e registrar no PRD (não na Arquitetura) a política de consumo de Lote — ao menos a direção (FEFO com override manual, por exemplo) — porque é regra de negócio, não mecanismo (#8).
3. Resolver o conflito entre FR-9 (template obrigatório) e a cobertura incompleta dos 28 templates frente às 25 Categorias — decidir fallback genérico ou exceção condicional (#14).
4. Definir plano de backfill para Filial (FR-51) e Lote (FR-47) sobre os dados já em produção na Ferreira Costa, com o mesmo rigor já usado em FR-44 (#16, #10).
5. Definir o que acontece com Produtos legados que não satisfazem as novas regras (sem template, nome < 10 caracteres) — mesma classe de risco do item 4, em escopo menor (#15, #17).
