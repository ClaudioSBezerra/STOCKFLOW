---
name: 'review-adversarial-spine-v2'
type: adversarial-review
target: 'ARCHITECTURE-SPINE.md (rodada com AD-24..AD-30 — Lote/FEFO, reserva de saldo, contador sequencial, Filial, Centro de Custo)'
created: '2026-09-19'
method: 'pares de "duas unidades um nível abaixo" (Stories/builders/handlers) que obedecem toda AD ao pé da letra e ainda assim constroem de forma incompatível'
---

# Revisão Adversarial — Architecture Spine stockflow (rodada AD-24 a AD-30)

## Como esta revisão foi feita

Para cada achado, construí um par concreto de implementações (Builder A / Builder B) que leem literalmente as mesmas ADs, discordam em um ponto que o spine não decide, e produzem sistemas incompatíveis entre si (dado divergente, dono duplicado da mesma entidade, ou caminho de mutação de estado conflitante). Um achado só entra nesta lista se eu consigo escrever concretamente os dois builders discordando — não é uma preferência de estilo.

Severidade: **CRÍTICA** (quebra invariante de negócio ou isolamento de dado — saldo, tenant), **ALTA** (abre uma corrida ou vazamento real sob condição plausível), **MÉDIA** (inconsistência textual/documental que induz erro, mas com blast radius menor).

---

## Achados CRÍTICOS

### C1 — AD-11 (mesclagem) não decide o destino dos LOTES do Produto removido

**ADs em tensão:** AD-11 (mesclagem de duplicatas) x AD-24 (Lote/FEFO, nova rodada).

AD-11 diz, ao pé da letra: *"Mesclagem de duplicatas (FR-20) reescreve o `produto_id` em todas as linhas históricas de `MOVIMENTACOES` e `PEDIDO_ITENS` do produto removido para o produto sobrevivente, antes do soft-delete."* Essa frase enumera exatamente duas tabelas. `LOTES` não existia quando AD-11 foi escrita (ela nasceu para o mundo de `produto_estoque`, uma linha por par Produto/Estoque). AD-24 substitui `produto_estoque` por `lotes` mas não volta a tocar AD-11.

- **Builder A (Story "Mesclagem de duplicatas FR-20")** lê AD-11 literalmente: reescreve `produto_id` em `MOVIMENTACOES` e `PEDIDO_ITENS`, soft-deleta o produto removido. Não toca `LOTES`, porque a AD não manda.
- **Builder B (Story "Consumo FEFO / saldo do Produto")** lê AD-24 e AD-29: saldo do Produto = soma de `lotes.quantidade` onde `produto_id` = o produto. Depois de uma mesclagem feita pelo Builder A, os Lotes físicos do produto removido continuam existindo com `produto_id` apontando para uma linha soft-deleted — nunca aparecem no saldo do produto sobrevivente, e ficam inacessíveis para consumo FEFO (nenhuma tela referencia produto soft-deleted).

Resultado: estoque físico real "some" silenciosamente numa mesclagem — quebra exatamente a garantia que AD-11 diz proteger ("soma de MOVIMENTACOES == quantidade atual", agora reformulada em termos de LOTES) e que FR-30 (relatórios) e FR-16 dependem. Dois builders, cada um 100% aderente à AD que leu, produzem um buraco de estoque perdido.

**Fechar com:** estender a Rule de AD-11 para reescrever `produto_id` também em `LOTES` (e, por construção do FEFO, isso simplesmente amplia o conjunto de Lotes candidatos do produto sobrevivente — nenhuma soma manual necessária) — na mesma transação, antes do soft-delete, com o mesmo lock ordering de AD-10 estendido por AD-24 (`produto_id, estoque_id, lote_id`) para evitar corrida com uma baixa/aprovação em andamento sobre os Lotes que estão trocando de dono no meio da mesclagem.

---

### C2 — AD-11 (mesclagem) não decide o destino das RESERVAS_PEDIDO_ITEM do Produto removido

**ADs em tensão:** AD-11 x AD-25 (reserva de saldo, nova rodada).

Mesmo problema do C1, num ponto ainda mais sensível: se o Produto removido numa mesclagem tem um Pedido pendente com saldo reservado (`RESERVAS_PEDIDO_ITEM.produto_id` = produto removido), AD-11 manda reescrever `PEDIDO_ITENS.produto_id` para o produto sobrevivente, mas não menciona `RESERVAS_PEDIDO_ITEM` (tabela que nem existia quando AD-11 foi escrita).

- **Builder A** (mesclagem) reescreve `PEDIDO_ITENS.produto_id`, não toca `RESERVAS_PEDIDO_ITEM` — a reserva continua apontando para o `produto_id` antigo (agora soft-deleted).
- **Builder B** (aprovação/baixa de Pedido, ou o cálculo de saldo disponível de AD-25: *"soma de `lotes.quantidade` menos soma de `reservas_pedido_item.quantidade` ativas"*) calcula o saldo disponível do produto sobrevivente filtrando `reservas_pedido_item.produto_id = <produto sobrevivente>`. A reserva órfã (ainda no id antigo) nunca entra nesse cálculo.

Resultado: o produto sobrevivente mostra saldo disponível cheio, ignorando uma reserva que — depois do merge de `PEDIDO_ITENS` — nominalmente pertence a ele. Um segundo Pedido pode vender o mesmo saldo já comprometido: exatamente a race condition que FR-50/AD-25 foram desenhadas para fechar, reaberta pela mesclagem. Este é provavelmente o achado de maior risco de negócio do lote todo (venda dupla de estoque físico).

**Fechar com:** AD-11 precisa reescrever `produto_id` em `RESERVAS_PEDIDO_ITEM` no mesmo passo que reescreve `PEDIDO_ITENS`, sempre na mesma transação atômica da mesclagem — e a mesclagem em si deveria ser proibida (ou exigir resolução manual) se o produto removido tiver reservas ativas cujo `estoque_id` não existe no produto sobrevivente, para não criar uma reserva "flutuante" sem Lote correspondente.

---

### C3 — AD-25 define a fórmula do saldo disponível mas não estende o lock de AD-10 para a CRIAÇÃO da reserva

AD-10 (concorrência) manda: *"Toda escrita em `produto_estoque.quantidade`... usa `SELECT ... FOR UPDATE`."* Note o escopo exato: escrita em **quantidade**. AD-25 desenha a reserva deliberadamente para **não** tocar `lotes.quantidade` (*"saldo disponível é sempre calculado..., nunca uma coluna materializada"*) — ou seja, criar uma reserva (FR-22, no envio do Pedido) não é uma escrita em `quantidade` e, lida ao pé da letra, a regra de lock de AD-10 **não se aplica** a ela.

- **Builder A (Story "Aprovação/baixa de Pedido")** segue AD-10 à risca: só toma `FOR UPDATE` quando de fato debita `lotes.quantidade` (na aprovação). Não vê motivo para lockar nada na criação da reserva, porque a reserva não escreve em `quantidade`.
- **Builder B (Story "Envio de Pedido / criação de reserva", FR-22)** lê a mesma AD-10 e a mesma AD-25 e chega à mesma conclusão: calcula saldo disponível com um `SELECT` simples (soma de Lotes menos soma de reservas ativas) e, se positivo, faz `INSERT` em `reservas_pedido_item` — sem `FOR UPDATE` em lugar nenhum, porque nenhuma AD manda lockar para esse caminho.

Dois Pedidos enviados concorrentemente para o mesmo par produto/estoque, cada um lendo "saldo disponível = 5" antes do outro commitar seu `INSERT`, reservam 5 cada — 10 reservados contra 5 físicos. Isso é a mesma classe de corrida que FR-50 fechou para baixa/transferência, só que reaberta no ponto de entrada (reserva), porque nem AD-10 nem AD-25 dizem explicitamente "o cálculo de saldo disponível + INSERT da reserva também precisa lockar as linhas de `lotes` do par produto/estoque envolvidas, na mesma transação, antes de decidir se reserva."

**Fechar com:** adicionar a AD-25 (ou estender AD-10) uma frase explícita: *criar uma reserva também exige `SELECT lotes ... FOR UPDATE` no par `(produto_id, estoque_id)` antes de calcular o saldo disponível e inserir a linha em `reservas_pedido_item`, na mesma transação* — do contrário o "nunca coluna materializada" de AD-25 é uma leitura sem isolamento transacional garantido.

---

### C4 — FR-13 (bloqueio de exclusão por saldo residual) não é resolvida pela combinação AD-24/AD-29

O spine nunca decide o ciclo de vida de uma linha de `LOTES` quando `quantidade` chega a zero por consumo FEFO — AD-24 não diz se a linha é apagada, zerada-e-mantida, ou soft-deleted. AD-29 só cobre o caso "Produto sem **nenhuma** linha em `lotes`" (zero linhas), não o caso "Produto/Estoque com linhas em `lotes`, todas zeradas".

- **Builder A (Story "Exclusão de Estoque, FR-13")** implementa o bloqueio como *existência de linha*: `EXISTS (SELECT 1 FROM lotes WHERE estoque_id = $1)` → bloqueia a exclusão mesmo quando todo o saldo já foi consumido a zero, porque as linhas (histórico de Lote) continuam lá.
- **Builder B (Story "Consumo FEFO / baixa", FR-14)**, ao implementar o débito, nunca apaga a linha de Lote ao chegar a zero — trata isso como estado normal (mantém para rastreabilidade de validade/auditoria), coerente com o espírito de AD-29 ("saldo zero é um estado válido, não um estado de ocultação").

O resultado direto: um Estoque genuinamente vazio (zero saldo físico, coerente com AD-29 tratando zero como normal) fica permanentemente impossível de excluir via FR-13 no Builder A, porque a regra de bloqueio foi implementada por "existe linha" em vez de "soma de quantidade > 0" — nenhuma AD resolve qual das duas é a correta. Um segundo eixo do mesmo buraco: nem AD-25 nem FR-13 dizem se uma **reserva ativa** sem saldo físico correspondente (Pedido pendente sobre um Estoque com Lotes zerados) deveria por si só bloquear a exclusão do Estoque — hoje nada impede excluir o Estoque debaixo de uma reserva em aberto.

**Fechar com:** uma frase explícita definindo (a) que "saldo residual" para fins de FR-13 é `SUM(lotes.quantidade) > 0` (nunca existência de linha) e (b) que Estoque com reserva ativa (`EXISTS reservas_pedido_item ativa` para esse `estoque_id`) também bloqueia a exclusão, mesmo com saldo físico zerado — e, já que se decide isso, aproveitar para declarar o ciclo de vida da linha de Lote a zero (mantida vs. apagada) numa única frase em AD-24, hoje ausente.

---

## Achados de severidade ALTA

### A1 — A lista de tabelas de AD-20 está desatualizada; a nota do diagrama afirma cobertura que a Rule não escreve

AD-20 Rule enumera explicitamente as tabelas que exigem `WHERE empresa_id = $1`: `produtos, estoques, movimentacoes, pedidos, pedido_itens, categorias, logs_acesso, solicitacoes_promocao, mesclagens_duplicatas, mesclagem_produtos_removidos, importacoes, nomenclatura_templates` e `usuarios`. Nenhuma das tabelas novas desta rodada — `LOTES`, `RESERVAS_PEDIDO_ITEM`, `FILIAIS`, `CENTROS_CUSTO`, `CONVITES_EMPRESA`, `CONTADORES_PRODUTO` — está nessa lista. A única afirmação de que elas também são escopadas por Empresa vive numa **nota de rodapé do diagrama ER** (linha 251), não na Rule normativa que um builder segue ao implementar uma query nova.

- **Builder A (Story "Relatório de Lotes a vencer", plausível dado FEFO)** implementa uma query direta sobre `LOTES` (`SELECT * FROM lotes WHERE data_validade BETWEEN NOW() AND NOW() + interval '30 days'`) sem join a `produtos`/`estoques` — porque para esse relatório específico não precisa. Segue AD-20 "ao pé da letra": `LOTES` não está na lista de tabelas obrigadas, então não sente necessidade de adicionar `AND empresa_id = $1`.
- **Builder B (Story "Catálogo/saldo por produto")** só toca `LOTES` sempre via join com `produtos`/`estoques` (que já são filtrados por `empresa_id`), então nunca precisou pensar em filtrar `LOTES` diretamente — e concordaria que "não precisa", reforçando o mesmo ponto cego.

Como `LOTES` não está na lista, um relatório direto sobre ela (cenário natural para uma feature de FEFO) vaza dado de vencimento de estoque entre Empresas — quebra FR-40/AD-20 exatamente no ponto que a nota de rodapé jurava estar coberto.

**Fechar com:** atualizar a Rule (não só a nota do diagrama) de AD-20 com a lista completa de tabelas, incluindo as seis novas desta rodada.

### A2 — Nenhuma AD exige validar que uma FK cliente-fornecida (Filial, Centro de Custo) pertence à Empresa do contexto

AD-27 (`estoques.filial_id`) e AD-28 (`pedidos.centro_custo_id`) introduzem, pela primeira vez nesta spine, colunas de FK **opcionalmente escolhidas pelo cliente** que apontam para tabelas escopadas por Empresa — diferente do padrão anterior (`empresa_id` sempre vem do contexto de middleware, nunca do cliente, AD-19). Nenhuma AD diz explicitamente "todo `filial_id`/`centro_custo_id` recebido do cliente deve ser validado contra `empresa_id` do contexto antes do uso", e a FK do Postgres por si só não impõe isso (a FK só garante que a linha existe em algum lugar, não que pertence à Empresa certa).

- **Builder A (Story "Cadastro de Estoque com Filial", FR-51)** valida explicitamente `SELECT empresa_id FROM filiais WHERE id = $1` e compara com o contexto antes de aceitar.
- **Builder B (Story "Cadastro de Pedido com Centro de Custo", FR-52)** confia na FK do banco (`REFERENCES centros_custo(id)`) e não faz a checagem cruzada — afinal nenhuma AD pede isso, e a FK "já garante que existe".

Um usuário autenticado na Empresa B consegue associar um Pedido a um `centro_custo_id` pertencente à Empresa A (não vaza linhas em massa, mas é uma quebra pontual e real do isolamento de AD-20/FR-40, e pode vazar o *nome* do Centro de Custo de outra Empresa via join em relatório). O mesmo vetor se aplica a `filial_id` se algum endpoint aceitar esse campo do corpo da requisição em vez de sempre derivá-lo do Estoque já resolvido.

**Fechar com:** uma frase em AD-19 ou AD-20 generalizando a regra: *toda FK opcional apontando para tabela de domínio escopada por Empresa, quando fornecida pelo cliente, deve ser validada contra o `empresa_id` do contexto antes do uso — nunca apenas a existência via FK do banco.*

### A3 — AD-26 não diz quando/onde nasce a primeira linha de `contadores_produto` de uma Empresa nova

Ao contrário de AD-27, que é explícito (*"Toda Empresa nova ganha uma Filial padrão automática... na MESMA transação de `ProvisionarEmpresa`"*), AD-26 não diz quando a linha `contadores_produto(empresa_id, proximo_valor=1)` é criada para uma Empresa recém-provisionada.

- **Builder A (Story "Provisionamento de Empresa", FR-41)**, seguindo o padrão explícito de AD-27, também insere `contadores_produto` com `proximo_valor = 1` na mesma transação de `ProvisionarEmpresa` — a linha sempre existe antes do primeiro Produto.
- **Builder B (Story "Cadastro de Produto", FR-8)** não pressupõe que a linha já exista (porque AD-26 nunca disse isso) e implementa um bootstrap lazy: `INSERT INTO contadores_produto (empresa_id, proximo_valor) VALUES ($1, 1) ON CONFLICT DO NOTHING` seguido do `UPDATE ... RETURNING`. Isoladamente correto — mas só se **todo** caminho de escrita usar exatamente esse padrão atômico.

Se qualquer terceiro caminho (ex. script de seed do Ambiente de Treinamento, FR-43, ou o `cmd/migrate-empresa` de FR-44) fizer um `SELECT` para checar se a linha existe antes de decidir entre `INSERT` e `UPDATE` (check-then-act, não atômico), dois primeiros-cadastros de Produto concorrentes na mesma Empresa nova podem ambos ver "não existe linha", ambos inserir `proximo_valor = 1` e ambos tentar `RETURNING 1` — dois Produtos com o mesmo código `000001` na mesma Empresa. Isso responde diretamente a Pergunta 3 do brief: o `UPDATE ... RETURNING` dentro da mesma transação **é** seguro contra corrida — mas só depois que a linha já existe; a criação da linha em si é o ponto não coberto.

**Fechar com:** uma frase em AD-26 espelhando AD-27: a linha de `contadores_produto` nasce na mesma transação de `ProvisionarEmpresa`, nunca via bootstrap lazy no primeiro cadastro de Produto.

### A4 — AD-3 não foi estendida para os novos domínios com handler próprio (Filial no mínimo)

AD-3 fixa "quatro canais" (`produtos`, `estoques`, `movimentacoes`, `pedidos`) mas também enuncia a própria regra geradora: *"todo domínio com handler próprio tem canal — nenhum fica implícito em outro."* Esta rodada introduz `handlers/filiais.go` (AD-27, Structural Seed e Capability Map) como um domínio novo com handler próprio — a mesma justificativa que originou os quatro canais existentes.

- **Builder A (Story "SSE de Filiais")**, seguindo a regra geradora de AD-3 ao pé da letra, cria um quinto canal `filiais` porque Filial tem handler próprio.
- **Builder B (Story "Front-end realtime")**, seguindo a lista literal e explícita ("quatro canais"), assume que mudanças de Filial se propagam via o canal `estoques` já existente (afinal Filial afeta a exibição de Estoque) e nunca escuta um canal `filiais` que não está documentado.

O evento de criação/edição de Filial nunca chega ao front-end no Builder B, ou o Builder A emite eventos num canal que ninguém assina — o mesmo tipo de canal "implícito" que a própria AD-3 diz prevenir. O mesmo vale, com menor força, para `Centro de Custo` (FR-52).

**Fechar com:** atualizar a lista fixa de canais de AD-3 para incluir `filiais` (e decidir explicitamente se Centro de Custo tem canal próprio ou se muda tão pouco que não precisa de realtime).

---

## Achados de severidade MÉDIA

### M1 — Título e texto de "Prevents" de AD-10 ainda nomeiam `produto_estoque.quantidade`, tabela que AD-24 diz não existir mais

O título de AD-10 é *"Concorrência e propriedade de escrita de `produto_estoque.quantidade`"* e o "Prevents" também cita a tabela antiga. AD-24 diz que `lotes` **substitui** `produto_estoque` ("deixa de existir uma linha única por par Produto/Estoque"). AD-10 tem, sim, um bullet final corrigindo isso ("Estendido nesta versão (AD-24)..."), mas um builder que lê só o título e o corpo principal do AD-10 (comum quando se navega por busca de texto/índice) pode legitimamente pensar que ainda existe uma tabela `produto_estoque` viva em paralelo a `lotes`, ou ficar em dúvida sobre qual delas é a fonte de verdade. Baixo custo de correção (renomear o título), mas real potencial de confundir um builder que não lê a AD inteira.

**Fechar com:** renomear o título/Prevents de AD-10 para `lotes.quantidade` (ou "saldo por Lote"), deixando o bullet de extensão como está.

### M2 — Ordem entre a migração de Lote-legado (AD-24) e o backfill de `empresa_id` (AD-20/FR-44) não está sequenciada

AD-24 diz que a migração cria "Lote legado" a partir de `produto_estoque`, com `empresa_id` como coluna não-nullable na definição de `lotes` (só `data_validade` é explicitamente `NULL`able na lista de colunas). AD-20 diz que o backfill de `empresa_id` da Ferreira Costa é feito em duas fases (nullable → backfill → `NOT NULL`) sobre as tabelas de domínio já existentes. Se a migração de `produto_estoque → lotes` (AD-24) rodar **antes** da Fase 1 do backfill de empresa_id terminar, de onde vem o `empresa_id` de cada linha de Lote legado — do `produtos`/`estoques` pai (que nesse momento pode ainda estar `NULL`)? O spine não ordena as duas migrações uma em relação à outra nem diz que `lotes.empresa_id` também nasce nullable temporariamente.

**Fechar com:** uma frase explícita de ordem: a migração de Lote-legado só roda depois que o backfill de `empresa_id` (AD-20) já tiver `SET NOT NULL` nas tabelas-pai, herdando o valor diretamente delas.

---

## Resumo por severidade

- **Crítico (4):** C1 mesclagem não move Lotes (estoque físico perdido); C2 mesclagem não move reservas (venda duplicada de saldo); C3 criação de reserva sem lock (mesma classe de corrida que FR-50 fechou, reaberta); C4 FR-13 "saldo residual" indefinido para Lotes zerados/reservas ativas.
- **Alto (4):** A1 lista de tabelas de AD-20 desatualizada (LOTES sem filtro de empresa_id em query direta); A2 FK cliente-fornecida (Filial/Centro de Custo) sem validação cruzada de Empresa; A3 bootstrap de `contadores_produto` não especificado (corrida no primeiro Produto de uma Empresa nova); A4 AD-3 com canais SSE fixos desatualizados para Filial/Centro de Custo.
- **Médio (2):** M1 título de AD-10 referencia tabela extinta; M2 ordem não especificada entre migração de Lote-legado e backfill de empresa_id.

Arquivo: `/home/claudio/projetos/stockflow/_bmad-output/planning-artifacts/architecture/architecture-stockflow-2026-08-29/reviews/review-adversarial.md`
