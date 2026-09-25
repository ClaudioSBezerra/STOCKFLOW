# Epic 16 Context: Inativar Produto, EAN único e histórico do Produto

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Permitir que `gestor` e `adm` tirem de uso um Produto sem apagá-lo (só quando não há saldo nem reserva) e o reativem depois; impedir que o mesmo EAN-13 fique em dois Produtos ativos da mesma Empresa (o mesmo item com dois códigos internos); e registrar toda troca de nome, inativação e reativação. Vem do treinamento da Ferreira Costa (2026-09-25): hoje não há como aposentar um Produto, a edição aceitou EAN repetido e a troca da descrição inteira não deixa rastro.

## Stories

- Story 16.1: Inativar e reativar um Produto
- Story 16.2: Produto inativo some das operações; filtro "Inativos"
- Story 16.3: Histórico de alterações do Produto
- Story 16.4: EAN-13 único entre os Produtos ativos da Empresa

## Requirements & Constraints

- **Inativar/reativar:** só `gestor`/`adm` (outros papéis: botão oculto, API 403). Motivo opcional. Recusado se houver saldo (`lotes.quantidade > 0`) em qualquer Estoque ou reserva de Pedido pendente; a recusa diz em quais Estoques há saldo (ou que há reserva) e orienta transferir/dar baixa. Já inativo → 409. Fora de escopo: exclusão definitiva e inativação em lote.
- **Some das operações:** Catálogo, busca, leitura por QR Code/código de barras (responde como código não encontrado), exportação do Catálogo, Carrinho (item removido com o mesmo aviso de "Produto que sumiu"), detecção de duplicatas e inconsistências, lançamento de saldo, envio de Pedido e importação (linha vira erro "Produto inativo — reative antes de importar", sem interromper as demais). Chamada direta à API com Produto inativo → 409.
- **Continua no histórico:** Movimentações, Pedidos, recibos em PDF e detalhe do Produto mostram o inativo com a marca "Inativo". O código interno continua reservado a ele.
- **Filtro "Inativos"** no Catálogo só para `gestor`+; para os demais o filtro não aparece e `?inativos=1` é ignorado.
- **Histórico:** registra troca de nome (antes/depois), inativação (com motivo) e reativação, com autor e data/hora; visível no detalhe para `almoxarife`+ (usuário: seção oculta e 403), do mais recente para o mais antigo; sem edição nem exclusão. Salvar sem mudar o nome não grava nada. Fora de escopo: histórico campo a campo das outras propriedades.
- **EAN-13 único entre ativos da Empresa:** cadastro, edição e reativação recusam EAN já usado por outro Produto ativo → 409 `EAN_EM_USO`, tela "Este EAN já está no produto {código} — {nome}". EAN em Produto inativo ou em outra Empresa é aceito. Duplicatas antigas não são alteradas; editar uma delas só salva depois de corrigir o EAN ou inativar o outro. Edição sem mudar o EAN e sem conflito se comporta como hoje.
- Histórico de Produto de outra Empresa → 404 sem revelar existência.

## Technical Decisions

- **Dado (migração aditiva):** `produtos.inativado_em TIMESTAMPTZ NULL` e `produtos.inativado_por UUID NULL REFERENCES usuarios(id)`; todo Produto existente nasce ativo. "Ativo" = `inativado_em IS NULL AND deleted_at IS NULL`. Não reaproveitar `deleted_at` — ele significa "removido pela mesclagem", irreversível.
- **Tabela `produto_historico`:** `id`, `empresa_id`, `produto_id`, `ator_id`, `acao` enum `nome_alterado | inativado | reativado`, `detalhe` jsonb (`{antes, depois}` para nome, `{motivo}` para inativar), `criado_em`. Escopada por `empresa_id`, append-only, sem rota de escrita direta. Mesmo molde de `auditoria_seguranca`.
- **Rotas:** `POST /e/{slug}/api/produtos/{id}/inativacao` (corpo `{motivo?}`) e `POST .../reativacao`, ambas `RequireRole(gestor)`; `GET .../produtos/{id}/historico` para `almoxarife`+. Lógica em `services/produtos_inativacao.go`.
- **Inativar:** numa transação, `SELECT ... FROM produtos WHERE id AND empresa_id FOR UPDATE`; checa lotes com saldo e `reservas_pedido_item`; 409 `PRODUTO_COM_SALDO` listando Estoques; senão grava `inativado_em/por`, remove itens de `carrinho_itens` do Produto e grava histórico `inativado`.
- **Reativar:** limpa `inativado_em/por`, passa pela regra de EAN único e grava `reativado`.
- **Trava contra corrida:** toda escrita que cria saldo ou reserva (lançamento de saldo, envio de Pedido/reserva, linha de importação que atualiza Produto) toma `SELECT ... FROM produtos WHERE id ... FOR SHARE` na mesma transação e recusa Produto inativo. `FOR SHARE` x `FOR UPDATE` serializa com a inativação sem bloquear escritas concorrentes entre si.
- **Filtro:** toda leitura de operação ganha `inativado_em IS NULL` ao lado do `deleted_at IS NULL`. Leituras de histórico não filtram e expõem `inativo: true`.
- **Nome alterado:** `AtualizarProduto` (`PUT /api/produtos/{id}`) e `AtualizarNomeProduto` (rota de renomear) gravam `nome_alterado` na mesma transação quando o nome muda de fato.
- **EAN único no service, não em índice:** `CriarProduto`, `AtualizarProduto` e a reativação tomam `pg_advisory_xact_lock(hashtext(empresa_id || ':' || ean13))` na transação e só então procuram outro Produto ativo com o mesmo EAN. Índice único parcial foi recusado: os bancos já têm EAN repetido e a migration abortaria o deploy (lição da migration 000049).

## UX & Interaction Patterns

- Detalhe do Produto: botão "Inativar produto" (com motivo opcional e confirmação) para gestor/adm; quando inativo, marca "Inativo" e botão "Reativar".
- Recusa por saldo mostra os Estoques com saldo (ou a reserva) e orienta transferir ou dar baixa.
- Seção "Histórico do produto" no detalhe: troca de nome como antes → depois, inativação com motivo, reativação, com nome do autor e data/hora.
- Marca "Inativo" em toda tela de histórico que exiba o Produto (Movimentações, Pedidos, recibos PDF).

## Cross-Story Dependencies

- 16.1 cria a migration (colunas + `produto_historico`) da qual 16.2 e 16.3 dependem.
- A reativação (16.1) aplica a regra de EAN único de 16.4: reutilize o mesmo helper de lock + checagem em cadastro, edição e reativação.
- 16.2 toca leituras e escritas de épicos anteriores: Catálogo/busca/leitura por código/exportação, Carrinho, Normalização, importação por planilha, lançamento de saldo com Lote e reserva no envio de Pedido (Epic 11), edição de Produto (Epic 13).
- 16.3 se apoia na edição/renomear do Epic 13.
