# Epic 5 Context: Movimentação de Estoque

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Almoxarife registra saída (baixa) e transferência de estoque com histórico consultável, garantindo que o saldo de cada Produto por Estoque sempre reflita a realidade com trilha de auditoria completa. Isso importa porque saldo e histórico alimentam decisões operacionais (o que sobrou, o que falta) e relatórios — qualquer divergência entre saldo e histórico quebra a confiança no sistema. A epic também cobre a migração do histórico de movimentações do sistema legado, preservando a rastreabilidade anterior ao corte de dados.

## Stories

- Story 5.1: Registrar Baixa (consumo)
- Story 5.2: Registrar Transferência entre Estoques
- Story 5.3: Histórico de Movimentações consultável
- Story 5.4: Migração do Histórico de Movimentações legado

## Requirements & Constraints

- Registrar Baixa/Transferência e consultar o Histórico é restrito ao papel `almoxarife` ou superior (mesma restrição do cadastro de Produto) — `usuario` recebe 403 em ambos.
- Baixa: rejeita quantidade zero ou negativa antes de qualquer escrita; rejeita quantidade maior que a disponível, informando a quantidade real disponível no momento da tentativa.
- Transferência: rejeita origem igual a destino; rejeita quantidade maior que a disponível na origem sem debitar nada; checagem de disponibilidade e débito/crédito são atômicos.
- Toda escrita de saldo dependente de estado lido previamente é atômica no servidor — nenhuma corrida entre transações concorrentes pode deixar o saldo negativo.
- Histórico exibe Produto, tipo, origem, destino, quantidade, autor e data, em ordem cronológica.
- Migração do histórico legado: cada registro (produto identificado por nome desnormalizado, tipo baixa/transferência, origem/destino, quantidade, timestamp) é recriado como Movimentação vinculada ao `produto_id` novo, preservando data e autor originais quando disponíveis. Registro referenciando um Produto não migrado é marcado para revisão manual, sem interromper a migração dos demais. Reexecução do script não duplica registros já migrados. Disparo sempre manual por uma pessoa, nunca por um agente autônomo.

## Technical Decisions

- Toda escrita em `produto_estoque.quantidade`, sem exceção, insere uma Movimentação na mesma transação (tipo `ajuste` reservado para futuras correções manuais de saldo) — não existe caminho de escrita em quantidade sem a Movimentação correspondente.
- Toda escrita de saldo usa `SELECT ... FOR UPDATE` (lock pessimista) na mesma transação.
- Operações que tocam múltiplas linhas `(produto_id, estoque_id)` — como a Transferência, que toca origem e destino — ordenam o conjunto completo de pares ascendentemente antes de adquirir qualquer lock, nunca na ordem de inserção/exibição. Garante que duas transferências concorrentes entre os mesmos dois Estoques, montadas em ordens opostas, nunca gerem deadlock.
- Decisão de autorização por papel mínimo é sempre resolvida na camada de middleware (allow/deny), nunca checada ad-hoc no handler.
- Toda criação de Movimentação publica um evento no canal SSE dedicado `movimentacoes`, usando o envelope fixo `{resource, id, change}` com payload mínimo — o cliente sempre rebusca via GET, nunca há replay de eventos perdidos.
- A migração do histórico legado roda dentro do script único de migração (fora do runtime da aplicação), reaproveitando a tabela de mapeamento id-antigo→id-novo já populada pela migração de Produtos e Estoques.
- Camadas: fronteira HTTP recebe/valida/serializa; regra de negócio, transação e lock vivem na camada de serviço; acesso a dados via SQL explícito, sem ORM.
- Datas em UTC no banco, ISO 8601 na API; erros HTTP seguem envelope fixo `{"error": {"code", "message"}}`.

## UX & Interaction Patterns

- Telas de Movimentações/Histórico mostram um toast discreto (`aria-live="polite"`, ex. "Movimentações atualizada.") quando chega um evento SSE no canal `movimentacoes` — nunca recarrega a tela sozinha; o usuário decide quando atualizar, e o dado antigo permanece visível até lá.
- Se a reconexão SSE demorar mais que alguns segundos, um indicador discreto e persistente ("Reconectando...") aparece — silêncio total durante reconexão lenta é considerado falha de acessibilidade.

## Cross-Story Dependencies

- Story 5.4 depende das migrações de Produtos e Estoques (outra epic) já terem rodado, pois consome a tabela de mapeamento id-antigo→id-novo gerada por elas.
- Story 5.3 (Histórico consultável) deve funcionar de forma independente com Movimentações novas; o histórico só fica completo com dados anteriores ao corte depois que a Story 5.4 rodar.
