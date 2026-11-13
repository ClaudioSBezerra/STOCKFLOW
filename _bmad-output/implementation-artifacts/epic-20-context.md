# Epic 20 Context: Pedido de Transferência e Pedido de Baixa, com Aprovação em Lote e Pré-Nota

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

A Ferreira Costa emite Nota Fiscal de transferência de saldos de obras entre filiais — o botão "Transferir" atual move saldo na hora, sem aprovação, o que é arriscado demais para um processo que gera documento fiscal. Da mesma forma, "Registrar Baixa" é hoje uma ação solta sem aprovação. Este épico cria dois fluxos novos e paralelos, baseados em solicitação→aprovação (como o Pedido de retirada já existente): Pedido de Transferência entre Filiais e Pedido de Baixa de Estoque, ambos exigindo aprovação de um Gestor. A aprovação pode cobrir vários pedidos de uma vez (lote) e gera uma Pré-Nota com número de documento sequencial (`docnum`, equivalente ao MIRO do SAP), imprimível com todos os dados fiscais. Os botões diretos atuais continuam existindo sem mudança nenhuma — os dois fluxos coexistem até a equipe da Ferreira Costa validar qual deve ficar definitivo.

## Stories

- Story 20.1: Pedido de Transferência entre Filiais (solicitar)
- Story 20.2: Pedido de Baixa de Estoque (solicitar)
- Story 20.3: Aprovação em lote por Gestor
- Story 20.4: Geração de Pré-Nota (docnum) ao aprovar lote
- Story 20.5: Documento impresso da Pré-Nota

## Requirements & Constraints

- Solicitar um Pedido de Transferência ou de Baixa nunca mexe em saldo físico — só reserva, exatamente como o Pedido de retirada já faz hoje (reserva dura por item, nunca "solta" até decisão).
- Quantidade acima do disponível, ou (na Transferência) Estoque de origem igual ao de destino, rejeita a solicitação com a mesma mensagem/mecanismo já usado hoje — nunca grava nada parcial.
- Só um Gestor (papel `gestor` ou `adm`) decide pedidos destes dois tipos novos, individualmente ou em lote (podendo misturar Transferência e Baixa no mesmo lote); o servidor é a autoridade real (403 mesmo que a UI esconda o botão).
- Aprovar reexecuta a MESMA mecânica física já existente para cada tipo (débito por FEFO na origem + crédito preservando validade no destino, para Transferência; débito por FEFO, para Baixa) — revalidando a reserva contra o saldo físico no momento da aprovação (pode resultar em aprovação parcial de um item específico, sem travar os demais do lote).
- Toda aprovação em lote (de qualquer tamanho, inclusive 1 pedido só) gera exatamente uma Pré-Nota com `docnum` sequencial único por Empresa, nunca gerado para um lote rejeitado. Numeração nunca colide nem repete sob aprovações concorrentes, e cada Empresa numera a partir do próprio maior `docnum` (nunca mistura contagem entre Empresas).
- O documento impresso da Pré-Nota é rotulado "Pré-Nota" e mostra: `docnum`, Empresa, Data, Solicitante(s), Aprovador, e por item: Produto, Descrição, Quantidade, Unidade de Medida, Valor (preço unitário do produto); itens de Transferência também mostram Filial origem e destino.
- Os botões diretos "Transferir" e "Registrar Baixa" continuam funcionando exatamente como hoje, sem nenhum gate de aprovação — nunca remover, desativar ou unificar com os fluxos novos nesta entrega.
- Isolamento por Empresa em toda consulta/escrita nova (nenhum papel cruza Empresas), mesma garantia que já vale para todo o restante do sistema.

## Technical Decisions

- Estender o modelo existente de `pedidos`/`pedido_itens`/`reservas_pedido_item` com uma coluna `tipo` (`retirada` | `transferencia` | `baixa`) em vez de criar tabelas paralelas — reaproveita toda a reserva dura, snapshot imutável e autorização já existentes (Epic 7/11). Transferência precisa de um `estoque_destino_id` por item (nullable, só preenchido quando `tipo='transferencia'`); Filial de origem/destino são derivadas de `estoques.filial_id` de cada lado (já existe desde a Story 12.1) — não precisa de campo próprio para Filial.
- A mecânica física de débito/crédito (FEFO, preservação de validade no destino) já existe em `RegistrarTransferencia`/`RegistrarBaixa` (movimentacoes.go) — a decisão/aprovação do pedido deve reaproveitar essa lógica (ou a mesma função interna que ela usa), nunca duplicá-la.
- A geração de `docnum` sequencial por Empresa deve seguir o mesmo padrão já usado para o código interno de produto (contador atômico, avançado dentro da mesma transação da aprovação — nunca um `SELECT MAX()+1` solto).
- Aprovação em lote processa vários Pedidos numa operação do usuário; tratar como uma única decisão de produto mesmo que a implementação itere pedido a pedido — a Pré-Nota é o registro que amarra o lote.

## Cross-Story Dependencies

- Story 20.3 (aprovação em lote) depende dos tipos "transferencia"/"baixa" existirem no modelo (Stories 20.1/20.2) para ter o que decidir, mas é código novo e autocontido além disso.
- Story 20.4 (Pré-Nota/docnum) depende do fluxo de aprovação (20.3) já commitando decisões para ter o que vincular.
- Story 20.5 (documento impresso) depende da Pré-Nota (20.4) já existir para ter o que exibir/baixar.
- Nenhuma story depende de uma story FUTURA — cada uma é testável e demonstrável sozinha no momento em que é construída (ex.: 20.1 sozinha já cria o Pedido pendente e pode ser verificada, mesmo sem 20.3 ainda decidir nada).
