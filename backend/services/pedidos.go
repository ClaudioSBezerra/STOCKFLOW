// Package services, arquivo pedidos.go: Envio de Pedido — Story 7.2 (Epic
// 7, Pedidos de Retirada), spec-7-2. Formaliza o conteúdo do Carrinho
// (Story 7.1) como um Pedido `pendente` que o Almoxarife poderá enfileirar
// e decidir (Stories 7.3+, fora do escopo desta story).
package services

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/goregular"
)

// Pedido é a projeção devolvida por SubmeterPedido — o cabeçalho do Pedido
// recém-criado, sem os itens (o chamador já tinha os itens no Carrinho
// antes do envio; SubmeterPedido não precisa devolvê-los de novo).
type Pedido struct {
	ID              string    `json:"id"`
	UsuarioID       string    `json:"usuarioId"`
	Solicitante     string    `json:"solicitante"`
	ObraCentroCusto string    `json:"obraCentroCusto"`
	Observacao      *string   `json:"observacao"`
	Status          string    `json:"status"`
	CriadoEm        time.Time `json:"criadoEm"`
	// DecididoPor/DecididoEm (Story 7.5, spec-7-5): auditoria da decisão —
	// nil enquanto `status == "pendente"`. Só DecidirPedido as popula
	// diretamente; BuscarPedidoProprio NUNCA as lê de volta do banco (Never,
	// spec-7-5: auditoria fica só no banco/API nesta story, consumida pela
	// Story 7.6 depois — o frontend desta story não exibe estes campos).
	DecididoPor *string    `json:"decididoPor"`
	DecididoEm  *time.Time `json:"decididoEm"`
	// ProdutoIDs (Story 11.3): ids dos Produtos dos itens, preenchido só por
	// SubmeterPedido para o handler publicar `produtos` (updated) — nunca
	// serializado.
	ProdutoIDs []string `json:"-"`
	// CentroCustoID (Story 12.3): Centro de Custo cadastrado escolhido no
	// envio, nil quando ausente. Nunca serializado aqui — só a resposta do
	// POST /api/pedidos o expõe (o handler, como `centro_custo_id`); as
	// listagens/detalhe/recibo não o leem nem o mostram.
	CentroCustoID *string `json:"-"`
	// CentroCustoNome (decisão do code review, 2026-09-21): nome do Centro de
	// Custo cadastrado escolhido no envio, nil quando o Pedido não escolheu
	// nenhum. Exibido no detalhe, na Fila e no recibo; o texto livre
	// `ObraCentroCusto` continua sendo o campo obrigatório.
	CentroCustoNome *string `json:"centroCusto"`
	// Tipo (Story 20.1, Epic 20): `retirada` (Epic 7, default — toda linha
	// gravada antes desta story) | `transferencia` (Story 20.1) | `baixa`
	// (Story 20.2, ainda sem código que o produza). Usado pelo frontend
	// (TipoPedidoBadge) para distinguir o Pedido de Transferência do Pedido
	// de retirada comum na Fila/Meus Pedidos — DecidirPedido recusa decidir
	// qualquer Pedido com `tipo != 'retirada'` (ver guard abaixo).
	Tipo string `json:"tipo"`
}

// PedidoItem é uma linha do SNAPSHOT imutável em `pedido_itens` (AD-17,
// epic-7-context.md): o que o Usuário pediu, congelado no momento do envio.
// A leitura de detalhe (BuscarPedidoProprio) NUNCA faz join ao vivo com
// `produtos`/`estoques` — o rótulo não muda se o Produto for editado/mesclado
// depois.
type PedidoItem struct {
	ProdutoID     string  `json:"produtoId"`
	ProdutoNome   string  `json:"produtoNome"`
	CategoriaNome string  `json:"categoriaNome"`
	EstoqueID     string  `json:"estoqueId"`
	EstoqueNome   string  `json:"estoqueNome"`
	Quantidade    float64 `json:"quantidade"`
	// ProdutoCodigo (feedback Ferreira Costa, direção, 2026-10-20): lido ao
	// vivo por LEFT JOIN, como Inativo logo abaixo — nunca parte do
	// SNAPSHOT (AD-17), já que um Produto legado pode ganhar código depois
	// do envio do Pedido (migração de códigos legados).
	ProdutoCodigo *string `json:"produtoCodigo"`
	// QuantidadeAprovada (Story 7.5, spec-7-5): nil enquanto o Pedido
	// permanece `pendente`; a partir da decisão, um valor concreto de 0 até
	// `Quantidade` — o quanto de fato foi debitado/aprovado deste item.
	// `Quantidade - *QuantidadeAprovada` é a pendência não atendida, sempre
	// visível (nunca escondida, nunca descartada).
	QuantidadeAprovada *float64 `json:"quantidadeAprovada"`
	// Inativo (Story 16.2): o Produto foi inativado depois do envio. Lido ao
	// vivo por LEFT JOIN; nome/quantidade continuam vindo do snapshot (AD-17).
	Inativo bool `json:"inativo"`
}

// PedidoDetalhe é o cabeçalho do Pedido (struct Pedido, reaproveitado) mais
// os itens em snapshot — a projeção devolvida por BuscarPedidoProprio para
// GET /api/pedidos/{id}.
type PedidoDetalhe struct {
	Pedido
	Itens []PedidoItem `json:"itens"`
}

// PedidoResumo é o cabeçalho do Pedido mais a contagem de itens — a projeção
// devolvida por ListarPedidosProprios para GET /api/pedidos (a lista "Meus
// Pedidos" não precisa dos itens de cada linha, só de quantos são).
type PedidoResumo struct {
	Pedido
	QtdItens int `json:"qtdItens"`
}

// ErrPedidoNaoEncontrado indica `id` de Pedido inexistente, malformado
// (não-UUID, `pq` SQLSTATE 22P02) OU de outro Usuário sem papel almoxarife+
// — os três colapsam no MESMO erro (mesma resposta 404 NOT_FOUND na
// fronteira HTTP): nunca revela a existência de um Pedido alheio, nunca
// responde 403. Mesmo colapso de ObterProdutoDetalhe (catalogo.go).
var ErrPedidoNaoEncontrado = errors.New("pedido não encontrado")

// ErrPedidoNaoPendente indica que o Pedido já foi decidido (`aprovado`,
// `parcialmente_aprovado` ou `rejeitado`) — inclui reuso do endpoint de
// decisão e a corrida entre duas decisões concorrentes para o MESMO
// Pedido (o UPDATE guardado de DecidirPedido devolve este mesmo erro
// quando a corrida é detectada). Mapeado para 409 CONFLICT. Molde de
// ErrSolicitacaoNaoPendente (promocao.go).
var ErrPedidoNaoPendente = errors.New("pedido não está mais pendente")

// ErrPedidoTipoNaoSuportado indica que DecidirPedido foi chamado para um
// Pedido cujo `tipo` não é `retirada` (Story 20.1, Epic 20 — guard
// acrescentado na revisão da Story, Spec Change Log: a Fila existente, SEM
// este guard, aprovaria um Pedido de Transferência pendente debitando a
// origem por FEFO como baixa física (mesma mecânica do Pedido de retirada)
// SEM creditar o destino — perda de estoque real na primeira vez que
// alguém clicasse "Aprovar"). Checado ANTES de qualquer trava/débito — só
// a Story 20.3 implementa a decisão de verdade para
// `transferencia`/`baixa`; até lá, nenhum Pedido desses tipos pode ser
// aprovado NEM rejeitado pela Fila atual. Mapeado para 409 CONFLICT.
var ErrPedidoTipoNaoSuportado = errors.New("este tipo de pedido ainda não tem aprovação implementada nesta versão")

// statusPedidoValido fecha o conjunto de valores aceitos no filtro opcional
// `?status=` de GET /api/pedidos — espelha o CHECK da migração 000027 (Story
// 7.5 acrescentou `parcialmente_aprovado` ao CHECK original da migração
// 000026). Um valor fora deste conjunto é rejeitado por ListarPedidosProprios
// ANTES de tocar o banco (&ErroPedidoValidacao, 400 VALIDATION_ERROR).
var statusPedidoValido = map[string]bool{
	"pendente":              true,
	"aprovado":              true,
	"parcialmente_aprovado": true,
	"rejeitado":             true,
}

// ErroPedidoValidacao é o erro de validação devolvido por SubmeterPedido
// quando `solicitante` ou `obraCentroCusto` estão ausentes (vazios após o
// trim) — sempre verificado ANTES de tocar o banco (Design Notes/I-O Matrix
// de spec-7-2), nenhuma leitura de carrinho nem escrita acontece quando
// este erro é devolvido. Mapeado para 400 VALIDATION_ERROR. Mesmo molde de
// ErroCarrinhoValidacao.
type ErroPedidoValidacao struct {
	Mensagem string
}

func (e *ErroPedidoValidacao) Error() string { return e.Mensagem }

// ErrPedidoCarrinhoVazio indica que o carrinho do Usuário (depois da
// limpeza preguiçosa de ListarCarrinho — item obsoleto removido no próprio
// envio conta como "vazio" também) não tem nenhum item ativo. Mapeado para
// 409 CONFLICT.
var ErrPedidoCarrinhoVazio = errors.New("carrinho vazio: adicione ao menos um item antes de enviar o pedido")

// ErroPedidoIndisponivel indica que a disponibilidade real de ao menos um
// item caiu abaixo da quantidade pedida entre a montagem do carrinho e o
// envio — o Pedido inteiro é rejeitado (Always, spec-7-2: "falha de
// QUALQUER item aborta a transação inteira"), nenhum Pedido parcial,
// carrinho inalterado. `Itens` lista os nomes de Produto insuficientes, na
// ordem em que foram revalidados (par ordenado ascendente). Mapeado para
// 409 CONFLICT. Mesmo molde de ErroEstoqueComResiduo.
type ErroPedidoIndisponivel struct {
	Itens []string
}

func (e *ErroPedidoIndisponivel) Error() string {
	return fmt.Sprintf("disponibilidade insuficiente para: %s", strings.Join(e.Itens, ", "))
}

// SubmeterPedido formaliza o carrinho ativo do Usuário `usuarioID` como um
// Pedido `pendente` (Story 7.2, spec-7-2).
//
// `solicitante`/`obraCentroCusto` são trimados e validados como
// obrigatórios ANTES de qualquer acesso ao banco (400 VALIDATION_ERROR,
// nenhuma leitura de carrinho). `observacao` é opcional — trimada; vazia
// vira NULL.
//
// Em seguida chama ListarCarrinho (carrinho.go) para obter os itens ATIVOS
// já limpos (lazy cleanup de Produto mesclado/Estoque excluído, Story 7.1)
// — reaproveitado tal qual, nunca duplicado (Design Notes de spec-7-2).
// Carrinho vazio depois da limpeza -> ErrPedidoCarrinhoVazio, nada é
// escrito.
//
// Só então abre a transação de escrita: os pares (produto_id, estoque_id)
// dos itens são ordenados ascendentemente (AD-10 do epic-7-context.md) e
// travados (produto_estoque e lotes, travarSaldoParesTx), revalidando
// `quantidade <= disponível` no momento do envio — NUNCA confia no snapshot
// da montagem do carrinho (Always, spec-7-2). O disponível (Story 11.3) é
// saldoDisponivelParTx: produto_estoque + lotes − reservas ativas; ausência
// de linha colapsa em "0 disponível". Qualquer item
// insuficiente -> &ErroPedidoIndisponivel{Itens: [...]} com TODOS os itens
// insuficientes (não só o primeiro), rollback da transação inteira — nada é
// debitado, nada é gravado (Never, spec-7-2: o débito real é da Story 7.5).
//
// Caso contrário: insere `pedidos` (status default 'pendente'), insere uma
// linha de `pedido_itens` por item com o SNAPSHOT de nome/estoque (já
// resolvidos por ListarCarrinho) e `categoria_nome` via join com
// `categorias` (Code Map de spec-7-2), insere UMA reserva por item em
// `reservas_pedido_item` (Story 11.3), esvazia `carrinho_itens` do usuário
// e commita — tudo na MESMA transação (Always, spec-7-2).
func SubmeterPedido(db *sql.DB, empresaID string, usuarioID, solicitante, obraCentroCusto, observacao string) (Pedido, error) {
	return SubmeterPedidoComCentroCusto(db, empresaID, usuarioID, solicitante, obraCentroCusto, observacao, "")
}

// SubmeterPedidoComCentroCusto é SubmeterPedido (Story 12.3, AD-28) com o
// Centro de Custo cadastrado opcional: `centroCustoID` vazio (ou só espaços)
// = sem Centro; informado, é revalidado contra a Empresa do contexto ANTES
// de ler o carrinho ou escrever — inexistente, malformado ou de outra
// Empresa -> &ErroPedidoValidacao, nada gravado. O texto livre
// `obraCentroCusto` segue obrigatório e inalterado (coexistem, sem cópia).
func SubmeterPedidoComCentroCusto(db *sql.DB, empresaID string, usuarioID, solicitante, obraCentroCusto, observacao, centroCustoID string) (Pedido, error) {
	solicitanteTrim := strings.TrimSpace(solicitante)
	if solicitanteTrim == "" {
		return Pedido{}, &ErroPedidoValidacao{Mensagem: "solicitante é obrigatório"}
	}
	obraTrim := strings.TrimSpace(obraCentroCusto)
	if obraTrim == "" {
		return Pedido{}, &ErroPedidoValidacao{Mensagem: "obra/centro de custo é obrigatório"}
	}
	var centroCustoNull sql.NullString
	if idTrim := strings.TrimSpace(centroCustoID); idTrim != "" {
		if err := validarCentroCustoDaEmpresa(db, empresaID, idTrim); err != nil {
			return Pedido{}, err
		}
		centroCustoNull = sql.NullString{String: idTrim, Valid: true}
	}

	itens, _, err := ListarCarrinho(db, empresaID, usuarioID)
	if err != nil {
		return Pedido{}, fmt.Errorf("falha ao listar carrinho para envio de pedido: %w", err)
	}
	if len(itens) == 0 {
		return Pedido{}, ErrPedidoCarrinhoVazio
	}

	// AD-10 (epic-7-context.md): o conjunto COMPLETO de pares
	// (produto_id, estoque_id) é ordenado ascendentemente ANTES de
	// adquirir qualquer lock — sobre o lote inteiro, nunca na ordem em que
	// os itens entraram no carrinho. Serializa dois envios concorrentes que
	// compartilhem algum par, sempre na mesma ordem física, sem deadlock.
	itensOrdenados := append([]ItemCarrinho(nil), itens...)
	sort.Slice(itensOrdenados, func(i, j int) bool {
		if itensOrdenados[i].ProdutoID != itensOrdenados[j].ProdutoID {
			return itensOrdenados[i].ProdutoID < itensOrdenados[j].ProdutoID
		}
		return itensOrdenados[i].EstoqueID < itensOrdenados[j].EstoqueID
	})

	tx, err := db.Begin()
	if err != nil {
		return Pedido{}, fmt.Errorf("falha ao iniciar transação de envio de pedido: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	// Story 16.1 (FR-55): trava (FOR SHARE) as linhas de `produtos` dos
	// Produtos distintos, em ordem de id, ANTES de produto_estoque/lotes —
	// serializa com InativarProduto (FOR UPDATE) na mesma ordem de locks.
	// Algum inativo (inativado depois da leitura do carrinho) -> recusa.
	if err := recusarProdutosInativosTx(tx, empresaID, itensOrdenados); err != nil {
		return Pedido{}, err
	}

	// Story 11.3 (AD-25): trava produto_estoque e lotes dos pares (ordem
	// canônica, AD-10) e SÓ ENTÃO lê o saldo DISPONÍVEL (físico − reservas
	// ativas) — a mesma função que o carrinho usa. Par sem saldo em nenhuma
	// tabela (ou de outra Empresa) = 0 disponível, sem revelar existência.
	pares := make([]ParSaldo, 0, len(itensOrdenados))
	for _, item := range itensOrdenados {
		pares = append(pares, ParSaldo{ProdutoID: item.ProdutoID, EstoqueID: item.EstoqueID})
	}
	if err := travarSaldoParesTx(tx, empresaID, pares); err != nil {
		return Pedido{}, fmt.Errorf("falha ao travar saldo no envio do pedido: %w", err)
	}
	var indisponiveis []string
	for _, item := range itensOrdenados {
		disponivel, err := saldoDisponivelParTx(tx, empresaID, item.ProdutoID, item.EstoqueID)
		if err != nil {
			return Pedido{}, fmt.Errorf("falha ao calcular disponível no envio do pedido: %w", err)
		}
		if item.Quantidade > disponivel {
			indisponiveis = append(indisponiveis, item.ProdutoNome)
		}
	}
	if len(indisponiveis) > 0 {
		return Pedido{}, &ErroPedidoIndisponivel{Itens: indisponiveis}
	}

	var observacaoNull sql.NullString
	if obsTrim := strings.TrimSpace(observacao); obsTrim != "" {
		observacaoNull = sql.NullString{String: obsTrim, Valid: true}
	}

	var pedido Pedido
	var observacaoGravada sql.NullString
	const insertPedido = `
		INSERT INTO pedidos (usuario_id, solicitante, obra_centro_custo, observacao, empresa_id, centro_custo_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, usuario_id, solicitante, obra_centro_custo, observacao, status, criado_em, centro_custo_id, tipo`
	var centroCustoGravado sql.NullString
	if err := tx.QueryRow(insertPedido, usuarioID, solicitanteTrim, obraTrim, observacaoNull, empresaID, centroCustoNull).Scan(
		&pedido.ID, &pedido.UsuarioID, &pedido.Solicitante, &pedido.ObraCentroCusto,
		&observacaoGravada, &pedido.Status, &pedido.CriadoEm, &centroCustoGravado, &pedido.Tipo,
	); err != nil {
		return Pedido{}, fmt.Errorf("falha ao inserir pedido: %w", err)
	}
	if centroCustoGravado.Valid {
		pedido.CentroCustoID = &centroCustoGravado.String
	}
	if observacaoGravada.Valid {
		pedido.Observacao = &observacaoGravada.String
	}

	const selectCategoriaNome = `
		SELECT c.nome FROM produtos p
		JOIN categorias c ON c.id = p.categoria_id
		WHERE p.id = $1 AND p.empresa_id = $2`
	const insertItem = `
		INSERT INTO pedido_itens (pedido_id, produto_id, produto_nome, categoria_nome, estoque_id, estoque_nome, quantidade, empresa_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	const insertReserva = `
		INSERT INTO reservas_pedido_item (pedido_id, produto_id, estoque_id, quantidade, empresa_id)
		VALUES ($1, $2, $3, $4, $5)`
	for _, item := range itensOrdenados {
		var categoriaNome string
		if err := tx.QueryRow(selectCategoriaNome, item.ProdutoID, empresaID).Scan(&categoriaNome); err != nil {
			return Pedido{}, fmt.Errorf("falha ao resolver categoria do item de pedido: %w", err)
		}
		if _, err := tx.Exec(
			insertItem,
			pedido.ID, item.ProdutoID, item.ProdutoNome, categoriaNome, item.EstoqueID, item.EstoqueNome, item.Quantidade, empresaID,
		); err != nil {
			return Pedido{}, fmt.Errorf("falha ao inserir item de pedido: %w", err)
		}
		if _, err := tx.Exec(insertReserva, pedido.ID, item.ProdutoID, item.EstoqueID, item.Quantidade, empresaID); err != nil {
			return Pedido{}, fmt.Errorf("falha ao reservar saldo do item de pedido: %w", err)
		}
	}

	if _, err := tx.Exec(`DELETE FROM carrinho_itens WHERE usuario_id = $1`, usuarioID); err != nil {
		return Pedido{}, fmt.Errorf("falha ao esvaziar carrinho após envio do pedido: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Pedido{}, fmt.Errorf("falha ao commitar envio de pedido: %w", err)
	}
	for _, item := range itensOrdenados {
		pedido.ProdutoIDs = append(pedido.ProdutoIDs, item.ProdutoID)
	}

	return pedido, nil
}

// recusarProdutosInativosTx trava FOR SHARE, em ordem ascendente de id, a
// linha de `produtos` de cada Produto distinto dos itens (já ordenados por
// produto_id) e devolve &ErroPedidoProdutoInativo com os nomes dos inativos.
func recusarProdutosInativosTx(tx *sql.Tx, empresaID string, itensOrdenados []ItemCarrinho) error {
	var inativos []string
	for i, item := range itensOrdenados {
		if i > 0 && item.ProdutoID == itensOrdenados[i-1].ProdutoID {
			continue
		}
		var inativo bool
		err := tx.QueryRow(
			`SELECT inativado_em IS NOT NULL FROM produtos WHERE id = $1 AND empresa_id = $2 FOR SHARE`,
			item.ProdutoID, empresaID,
		).Scan(&inativo)
		if err != nil {
			return fmt.Errorf("falha ao travar produto no envio do pedido: %w", err)
		}
		if inativo {
			inativos = append(inativos, item.ProdutoNome)
		}
	}
	if len(inativos) > 0 {
		return &ErroPedidoProdutoInativo{Itens: inativos}
	}
	return nil
}

// validarQuantidadePedidoTransferencia é a validação prévia (ANTES de abrir
// a transação) de `quantidade` para SubmeterPedidoTransferencia — mesmo
// molde de validarQuantidadeMovimentacao (movimentacoes.go): NaN/±Inf/<=0
// (inclusive depois de arredondar a 3 casas) e acima de limiteNumeric103
// (NUMERIC(10,3) de pedido_itens.quantidade) são rejeitados com o mesmo
// ErroPedidoValidacao (400) usado pelos demais campos desta função — nunca
// colapsam num 500 de violação de CHECK constraint.
func validarQuantidadePedidoTransferencia(quantidade float64) error {
	if math.IsNaN(quantidade) || math.IsInf(quantidade, 0) || quantidade <= 0 || arredondar3(quantidade) <= 0 {
		return &ErroPedidoValidacao{Mensagem: "quantidade deve ser maior que zero"}
	}
	if quantidade > limiteNumeric103 {
		return &ErroPedidoValidacao{Mensagem: fmt.Sprintf("quantidade deve ser no máximo %s", limiteNumeric103Texto)}
	}
	return nil
}

// SubmeterPedidoTransferencia envia um Pedido de Transferência entre
// Filiais (Story 20.1, Epic 20, spec-20-1) — um tipo novo ao lado do Pedido
// de retirada (Epic 7): exatamente 1 item, enviado DIRETO numa tela própria
// (sem passar pelo Carrinho — Intent/Approach de spec-20-1). Reaproveita a
// MESMA reserva dura do Pedido de retirada
// (travarSaldoParesTx/saldoDisponivelParTx, reservas.go), travada só do par
// (Produto, Estoque ORIGEM) — o saldo físico NUNCA é debitado nem creditado
// aqui (Never, spec-20-1: isso é a Story 20.3; esta função nunca chama
// RegistrarTransferencia nem duplica a lógica de débito/crédito FEFO dela).
//
// Validação, nesta ordem, ANTES de abrir a transação:
//   - solicitante trimado e obrigatório (mesmo molde de
//     SubmeterPedidoComCentroCusto). `obraCentroCusto` é OPCIONAL
//     (requisito do usuário, pós-revisão, mesmo molde de
//     SubmeterPedidoBaixa/spec-20-2 — diferente do Pedido de retirada, que
//     continua exigindo este campo) — vazio após o trim vira NULL, mesmo
//     molde de `observacao`; migrations/000058 soltou o CHECK que restringia
//     o NULL a `tipo = 'baixa'` (a coluna já era NULLable desde a 000057).
//   - produtoID/estoqueOrigemID/estoqueDestinoID trimados: qualquer um
//     vazio -> &ErroPedidoValidacao (400) — NUNCA colapsa em
//     ErroPedidoIndisponivel (409), Always de spec-20-1.
//   - quantidade (validarQuantidadePedidoTransferencia acima).
//   - estoqueOrigemID == estoqueDestinoID (strings.EqualFold, molde de
//     RegistrarTransferencia, movimentacoes.go:518-520) ->
//     &ErroPedidoValidacao.
//
// Dentro da transação: resolve produto_nome/categoria_nome (existência do
// Produto na Empresa, não-excluído) — inexistente/outra Empresa também vira
// &ErroPedidoValidacao; recusarProdutosInativosTx (produtos_inativacao.go,
// reaproveitada tal qual, Always de spec-20-1) recusa Produto inativo ANTES
// de qualquer trava/reserva de saldo; resolve o nome do Estoque ORIGEM
// (existência na Empresa); valida o destino com o MESMO molde do guard de
// RegistrarTransferencia (movimentacoes.go:538-550: Produto + Estoque
// destino na MESMA Empresa, `FOR SHARE OF e`) — mas aqui o destino inválido
// vira &ErroPedidoValidacao (400), nunca ErroQuantidadeIndisponivel/
// ErroPedidoIndisponivel (409): decisão explícita da I/O Matrix de
// spec-20-1, diferente do molde original. Só então trava e revalida o par
// da ORIGEM -> &ErroPedidoIndisponivel{Itens: [nome]} (409) se a
// quantidade exceder o disponível.
//
// Sucesso: insere 1 linha em `pedidos` (`tipo='transferencia'`), 1 em
// `pedido_itens` (`estoque_id`=ORIGEM, `estoque_destino_id`=DESTINO,
// snapshot de nome/categoria/estoque como o Pedido de retirada) e 1 reserva
// em `reservas_pedido_item` (só da ORIGEM) — tudo na MESMA transação.
func SubmeterPedidoTransferencia(
	db *sql.DB, empresaID string,
	usuarioID, solicitante, obraCentroCusto, observacao string,
	produtoID, estoqueOrigemID, estoqueDestinoID string,
	quantidade float64,
) (Pedido, error) {
	solicitanteTrim := strings.TrimSpace(solicitante)
	if solicitanteTrim == "" {
		return Pedido{}, &ErroPedidoValidacao{Mensagem: "solicitante é obrigatório"}
	}
	produtoIDTrim := strings.TrimSpace(produtoID)
	if produtoIDTrim == "" {
		return Pedido{}, &ErroPedidoValidacao{Mensagem: "produto é obrigatório"}
	}
	estoqueOrigemIDTrim := strings.TrimSpace(estoqueOrigemID)
	if estoqueOrigemIDTrim == "" {
		return Pedido{}, &ErroPedidoValidacao{Mensagem: "estoque de origem é obrigatório"}
	}
	estoqueDestinoIDTrim := strings.TrimSpace(estoqueDestinoID)
	if estoqueDestinoIDTrim == "" {
		return Pedido{}, &ErroPedidoValidacao{Mensagem: "estoque de destino é obrigatório"}
	}
	if err := validarQuantidadePedidoTransferencia(quantidade); err != nil {
		return Pedido{}, err
	}
	if strings.EqualFold(estoqueOrigemIDTrim, estoqueDestinoIDTrim) {
		return Pedido{}, &ErroPedidoValidacao{Mensagem: "estoque de origem e destino devem ser diferentes"}
	}

	// obraCentroCusto é OPCIONAL no Pedido de Transferência (requisito do
	// usuário, pós-revisão, mesmo molde de SubmeterPedidoBaixa) — vazio após
	// o trim vira NULL, mesmo molde de `observacao` logo abaixo;
	// migrations/000058 soltou o CHECK que restringia o NULL a `tipo =
	// 'baixa'`.
	var obraCentroCustoNull sql.NullString
	if obraTrim := strings.TrimSpace(obraCentroCusto); obraTrim != "" {
		obraCentroCustoNull = sql.NullString{String: obraTrim, Valid: true}
	}

	var observacaoNull sql.NullString
	if obsTrim := strings.TrimSpace(observacao); obsTrim != "" {
		observacaoNull = sql.NullString{String: obsTrim, Valid: true}
	}

	tx, err := db.Begin()
	if err != nil {
		return Pedido{}, fmt.Errorf("falha ao iniciar transação de envio de pedido de transferência: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	var produtoNome, categoriaNome string
	const selectProduto = `
		SELECT p.nome, c.nome FROM produtos p
		JOIN categorias c ON c.id = p.categoria_id
		WHERE p.id = $1 AND p.deleted_at IS NULL AND p.empresa_id = $2`
	if err := tx.QueryRow(selectProduto, produtoIDTrim, empresaID).Scan(&produtoNome, &categoriaNome); err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return Pedido{}, &ErroPedidoValidacao{Mensagem: "produto não encontrado"}
		}
		return Pedido{}, fmt.Errorf("falha ao resolver produto do pedido de transferência: %w", err)
	}

	// Story 16.1 (FR-55): recusa Produto inativo ANTES de qualquer trava de
	// saldo — reaproveita recusarProdutosInativosTx tal qual (Always,
	// spec-20-1), para o único Produto deste Pedido.
	if err := recusarProdutosInativosTx(tx, empresaID, []ItemCarrinho{{ProdutoID: produtoIDTrim, ProdutoNome: produtoNome}}); err != nil {
		return Pedido{}, err
	}

	var estoqueOrigemNome string
	const selectEstoqueOrigem = `SELECT nome FROM estoques WHERE id = $1 AND empresa_id = $2`
	if err := tx.QueryRow(selectEstoqueOrigem, estoqueOrigemIDTrim, empresaID).Scan(&estoqueOrigemNome); err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return Pedido{}, &ErroPedidoValidacao{Mensagem: "estoque de origem não encontrado"}
		}
		return Pedido{}, fmt.Errorf("falha ao resolver estoque de origem do pedido de transferência: %w", err)
	}

	// Guard do destino — molde do guard de RegistrarTransferencia
	// (movimentacoes.go:538-550): Produto E Estoque destino precisam
	// existir na MESMA Empresa. `FOR SHARE OF e` serializa com
	// ExcluirEstoque (FOR UPDATE), mesmo padrão. Diferente de
	// RegistrarTransferencia: aqui o destino inválido é &ErroPedidoValidacao
	// (400), nunca uma disponibilidade insuficiente (409) — "Destino
	// inexistente/outra Empresa" é uma linha própria da I/O Matrix de
	// spec-20-1.
	var destinoValido bool
	const selectDestinoValido = `
		SELECT EXISTS (
			SELECT 1 FROM produtos p, estoques e
			WHERE p.id = $1 AND p.empresa_id = $3 AND p.deleted_at IS NULL
			  AND e.id = $2 AND e.empresa_id = $3
			FOR SHARE OF e
		)`
	if err := tx.QueryRow(selectDestinoValido, produtoIDTrim, estoqueDestinoIDTrim, empresaID).Scan(&destinoValido); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return Pedido{}, &ErroPedidoValidacao{Mensagem: "estoque de destino não encontrado"}
		}
		return Pedido{}, fmt.Errorf("falha ao validar estoque de destino do pedido de transferência: %w", err)
	}
	if !destinoValido {
		return Pedido{}, &ErroPedidoValidacao{Mensagem: "estoque de destino não encontrado"}
	}

	// Reserva dura: trava e revalida só o par da ORIGEM (Always, spec-20-1
	// — o destino nunca recebe reserva nem débito nesta story).
	if err := travarSaldoParesTx(tx, empresaID, []ParSaldo{{ProdutoID: produtoIDTrim, EstoqueID: estoqueOrigemIDTrim}}); err != nil {
		return Pedido{}, fmt.Errorf("falha ao travar saldo no envio do pedido de transferência: %w", err)
	}
	disponivel, err := saldoDisponivelParTx(tx, empresaID, produtoIDTrim, estoqueOrigemIDTrim)
	if err != nil {
		return Pedido{}, fmt.Errorf("falha ao calcular disponível no envio do pedido de transferência: %w", err)
	}
	if quantidade > disponivel {
		return Pedido{}, &ErroPedidoIndisponivel{Itens: []string{produtoNome}}
	}

	var pedido Pedido
	var observacaoGravada sql.NullString
	// COALESCE no RETURNING: `pedido.ObraCentroCusto` é `string` (não
	// ponteiro, mesmo campo lido pelo Pedido de retirada, que nunca grava
	// NULL) — um Pedido de Transferência sem Obra/Centro de Custo vira NULL
	// na coluna (migrations/000058) mas "" aqui, nunca um erro de Scan. Mesmo
	// molde de SubmeterPedidoBaixa.
	const insertPedido = `
		INSERT INTO pedidos (usuario_id, solicitante, obra_centro_custo, observacao, empresa_id, tipo)
		VALUES ($1, $2, $3, $4, $5, 'transferencia')
		RETURNING id, usuario_id, solicitante, COALESCE(obra_centro_custo, ''), observacao, status, criado_em, tipo`
	if err := tx.QueryRow(insertPedido, usuarioID, solicitanteTrim, obraCentroCustoNull, observacaoNull, empresaID).Scan(
		&pedido.ID, &pedido.UsuarioID, &pedido.Solicitante, &pedido.ObraCentroCusto,
		&observacaoGravada, &pedido.Status, &pedido.CriadoEm, &pedido.Tipo,
	); err != nil {
		return Pedido{}, fmt.Errorf("falha ao inserir pedido de transferência: %w", err)
	}
	if observacaoGravada.Valid {
		pedido.Observacao = &observacaoGravada.String
	}

	const insertItem = `
		INSERT INTO pedido_itens (pedido_id, produto_id, produto_nome, categoria_nome, estoque_id, estoque_nome, quantidade, empresa_id, estoque_destino_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	if _, err := tx.Exec(
		insertItem,
		pedido.ID, produtoIDTrim, produtoNome, categoriaNome, estoqueOrigemIDTrim, estoqueOrigemNome, quantidade, empresaID, estoqueDestinoIDTrim,
	); err != nil {
		return Pedido{}, fmt.Errorf("falha ao inserir item do pedido de transferência: %w", err)
	}

	const insertReserva = `
		INSERT INTO reservas_pedido_item (pedido_id, produto_id, estoque_id, quantidade, empresa_id)
		VALUES ($1, $2, $3, $4, $5)`
	if _, err := tx.Exec(insertReserva, pedido.ID, produtoIDTrim, estoqueOrigemIDTrim, quantidade, empresaID); err != nil {
		return Pedido{}, fmt.Errorf("falha ao reservar saldo do pedido de transferência: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Pedido{}, fmt.Errorf("falha ao commitar envio de pedido de transferência: %w", err)
	}
	pedido.ProdutoIDs = []string{produtoIDTrim}
	return pedido, nil
}

// SubmeterPedidoBaixa envia um Pedido de Baixa de Estoque (Story 20.2, Epic
// 20, spec-20-2) — tipo irmão do Pedido de Transferência (Story 20.1) e do
// Pedido de retirada (Epic 7): exatamente 1 item, enviado DIRETO numa tela
// própria (sem Carrinho), SEM Estoque destino (molde de
// SubmeterPedidoTransferencia acima, menos o bloco de destino). Reaproveita a
// MESMA reserva dura do Pedido de retirada
// (travarSaldoParesTx/saldoDisponivelParTx) — o saldo físico NUNCA é debitado
// aqui (Never, spec-20-2: isso continua sendo RegistrarBaixa,
// movimentacoes.go, ação direta sem aprovação, intocada; a decisão de
// verdade é a Story 20.3; DecidirPedido já recusa qualquer `tipo !=
// 'retirada'`, cobrindo `'baixa'` sem nenhuma mudança nesta função).
//
// Validação, nesta ordem, ANTES de abrir a transação:
//   - solicitante trimado e obrigatório (mesmo molde de
//     SubmeterPedidoComCentroCusto). `obraCentroCusto` é OPCIONAL
//     (requisito do usuário, pós-revisão: diferente do Pedido de
//     retirada/Transferência, que continuam exigindo este campo) — vazio
//     após o trim vira NULL, mesmo molde de `observacao`.
//   - produtoID/estoqueID trimados: qualquer um vazio -> &ErroPedidoValidacao
//     (400) — NUNCA colapsa em ErroPedidoIndisponivel (409), Always de
//     spec-20-2.
//   - quantidade (validarQuantidadePedidoTransferencia, reaproveitada tal
//     qual).
//
// Dentro da transação: resolve produto_nome/categoria_nome (existência do
// Produto na Empresa, não-excluído) — inexistente/outra Empresa vira
// &ErroPedidoValidacao; recusarProdutosInativosTx (Always de spec-20-2)
// recusa Produto inativo ANTES de qualquer trava/reserva de saldo; resolve o
// nome do Estoque (existência na Empresa) — inexistente/outra Empresa vira
// &ErroPedidoValidacao. Só então trava e revalida o par (Produto, Estoque) ->
// &ErroPedidoIndisponivel{Itens: [nome]} (409) se a quantidade exceder o
// disponível.
//
// Sucesso: insere 1 linha em `pedidos` (`tipo='baixa'`), 1 em `pedido_itens`
// (SEM `estoque_destino_id` — permanece NULL) e 1 reserva em
// `reservas_pedido_item` — tudo na MESMA transação.
func SubmeterPedidoBaixa(
	db *sql.DB, empresaID string,
	usuarioID, solicitante, obraCentroCusto, observacao string,
	produtoID, estoqueID string,
	quantidade float64,
) (Pedido, error) {
	solicitanteTrim := strings.TrimSpace(solicitante)
	if solicitanteTrim == "" {
		return Pedido{}, &ErroPedidoValidacao{Mensagem: "solicitante é obrigatório"}
	}
	produtoIDTrim := strings.TrimSpace(produtoID)
	if produtoIDTrim == "" {
		return Pedido{}, &ErroPedidoValidacao{Mensagem: "produto é obrigatório"}
	}
	estoqueIDTrim := strings.TrimSpace(estoqueID)
	if estoqueIDTrim == "" {
		return Pedido{}, &ErroPedidoValidacao{Mensagem: "estoque é obrigatório"}
	}
	if err := validarQuantidadePedidoTransferencia(quantidade); err != nil {
		return Pedido{}, err
	}

	// obraCentroCusto é OPCIONAL no Pedido de Baixa (requisito do usuário,
	// pós-revisão) — vazio após o trim vira NULL, mesmo molde de
	// `observacao` logo abaixo; `migrations/000057` soltou o NOT NULL da
	// coluna (compartilhada pelos três tipos de Pedido).
	var obraCentroCustoNull sql.NullString
	if obraTrim := strings.TrimSpace(obraCentroCusto); obraTrim != "" {
		obraCentroCustoNull = sql.NullString{String: obraTrim, Valid: true}
	}

	var observacaoNull sql.NullString
	if obsTrim := strings.TrimSpace(observacao); obsTrim != "" {
		observacaoNull = sql.NullString{String: obsTrim, Valid: true}
	}

	tx, err := db.Begin()
	if err != nil {
		return Pedido{}, fmt.Errorf("falha ao iniciar transação de envio de pedido de baixa: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	var produtoNome, categoriaNome string
	const selectProduto = `
		SELECT p.nome, c.nome FROM produtos p
		JOIN categorias c ON c.id = p.categoria_id
		WHERE p.id = $1 AND p.deleted_at IS NULL AND p.empresa_id = $2`
	if err := tx.QueryRow(selectProduto, produtoIDTrim, empresaID).Scan(&produtoNome, &categoriaNome); err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return Pedido{}, &ErroPedidoValidacao{Mensagem: "produto não encontrado"}
		}
		return Pedido{}, fmt.Errorf("falha ao resolver produto do pedido de baixa: %w", err)
	}

	// Story 16.1 (FR-55): recusa Produto inativo ANTES de qualquer trava de
	// saldo — reaproveita recusarProdutosInativosTx tal qual (Always,
	// spec-20-2), para o único Produto deste Pedido.
	if err := recusarProdutosInativosTx(tx, empresaID, []ItemCarrinho{{ProdutoID: produtoIDTrim, ProdutoNome: produtoNome}}); err != nil {
		return Pedido{}, err
	}

	var estoqueNome string
	const selectEstoque = `SELECT nome FROM estoques WHERE id = $1 AND empresa_id = $2`
	if err := tx.QueryRow(selectEstoque, estoqueIDTrim, empresaID).Scan(&estoqueNome); err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return Pedido{}, &ErroPedidoValidacao{Mensagem: "estoque não encontrado"}
		}
		return Pedido{}, fmt.Errorf("falha ao resolver estoque do pedido de baixa: %w", err)
	}

	// Reserva dura: trava e revalida o par (Produto, Estoque) — mesma regra
	// do Pedido de retirada/Transferência (Always, spec-20-2).
	if err := travarSaldoParesTx(tx, empresaID, []ParSaldo{{ProdutoID: produtoIDTrim, EstoqueID: estoqueIDTrim}}); err != nil {
		return Pedido{}, fmt.Errorf("falha ao travar saldo no envio do pedido de baixa: %w", err)
	}
	disponivel, err := saldoDisponivelParTx(tx, empresaID, produtoIDTrim, estoqueIDTrim)
	if err != nil {
		return Pedido{}, fmt.Errorf("falha ao calcular disponível no envio do pedido de baixa: %w", err)
	}
	if quantidade > disponivel {
		return Pedido{}, &ErroPedidoIndisponivel{Itens: []string{produtoNome}}
	}

	var pedido Pedido
	var observacaoGravada sql.NullString
	// COALESCE no RETURNING: `pedido.ObraCentroCusto` é `string` (não
	// ponteiro, mesmo campo lido por Pedido de retirada/Transferência, que
	// nunca gravam NULL) — um Pedido de Baixa sem Obra/Centro de Custo vira
	// NULL na coluna (migrations/000057) mas "" aqui, nunca um erro de Scan.
	const insertPedido = `
		INSERT INTO pedidos (usuario_id, solicitante, obra_centro_custo, observacao, empresa_id, tipo)
		VALUES ($1, $2, $3, $4, $5, 'baixa')
		RETURNING id, usuario_id, solicitante, COALESCE(obra_centro_custo, ''), observacao, status, criado_em, tipo`
	if err := tx.QueryRow(insertPedido, usuarioID, solicitanteTrim, obraCentroCustoNull, observacaoNull, empresaID).Scan(
		&pedido.ID, &pedido.UsuarioID, &pedido.Solicitante, &pedido.ObraCentroCusto,
		&observacaoGravada, &pedido.Status, &pedido.CriadoEm, &pedido.Tipo,
	); err != nil {
		return Pedido{}, fmt.Errorf("falha ao inserir pedido de baixa: %w", err)
	}
	if observacaoGravada.Valid {
		pedido.Observacao = &observacaoGravada.String
	}

	const insertItem = `
		INSERT INTO pedido_itens (pedido_id, produto_id, produto_nome, categoria_nome, estoque_id, estoque_nome, quantidade, empresa_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	if _, err := tx.Exec(
		insertItem,
		pedido.ID, produtoIDTrim, produtoNome, categoriaNome, estoqueIDTrim, estoqueNome, quantidade, empresaID,
	); err != nil {
		return Pedido{}, fmt.Errorf("falha ao inserir item do pedido de baixa: %w", err)
	}

	const insertReserva = `
		INSERT INTO reservas_pedido_item (pedido_id, produto_id, estoque_id, quantidade, empresa_id)
		VALUES ($1, $2, $3, $4, $5)`
	if _, err := tx.Exec(insertReserva, pedido.ID, produtoIDTrim, estoqueIDTrim, quantidade, empresaID); err != nil {
		return Pedido{}, fmt.Errorf("falha ao reservar saldo do pedido de baixa: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Pedido{}, fmt.Errorf("falha ao commitar envio de pedido de baixa: %w", err)
	}
	pedido.ProdutoIDs = []string{produtoIDTrim}
	return pedido, nil
}

// ListarPedidosProprios devolve os Pedidos cujo `usuario_id` é `usuarioID`
// (o da sessão — SEMPRE, para QUALQUER papel; a Fila do Almoxarife com TODOS
// os Pedidos é a Story 7.4, uma função de leitura à parte), do mais recente
// ao mais antigo, cada um com a contagem de itens. Sem paginação/teto
// (volume por usuário é baixo — Never de spec-7-3).
//
// `filtroStatus` vazio -> sem filtro de status. Não-vazio e fora de
// statusPedidoValido -> &ErroPedidoValidacao (400 VALIDATION_ERROR) ANTES de
// qualquer query. Molde de leitura/scan: ListarMovimentacoes
// (movimentacoes.go) — `ORDER BY criado_em DESC` com desempate por `id` para
// duas linhas no mesmo instante, `sql.NullString` para a `observacao`
// anulável, SQL explícito sem ORM.
func ListarPedidosProprios(db *sql.DB, empresaID string, usuarioID, filtroStatus string) ([]PedidoResumo, error) {
	if filtroStatus != "" && !statusPedidoValido[filtroStatus] {
		return nil, &ErroPedidoValidacao{Mensagem: "status inválido"}
	}

	// A contagem de itens é uma subquery CORRELACIONADA por `p.id` (correção
	// 2026-11-12, investigação de lentidão em "Meus Pedidos"): a versão
	// anterior agregava `pedido_itens` de TODA a Empresa numa subquery
	// independente antes do JOIN — trabalho desperdiçado proporcional ao
	// volume de pedidos de TODOS os usuários, não só do usuário consultando
	// (nunca foi vazamento de dado, o JOIN por `pedido_id`, chave
	// globalmente única, sempre casava certo). A forma correlacionada só
	// conta os itens do PRÓPRIO pedido da linha, usa o mesmo índice da PK
	// composta de `pedido_itens` (pedido_id, produto_id, estoque_id;
	// migration 000026) e nunca precisa de `empresa_id` — `pedido_id` já
	// identifica a linha sozinho.
	// COALESCE(p.obra_centro_custo, ''): um Pedido de Baixa (Story 20.2,
	// migrations/000057) OU de Transferência (Story 20.1, migrations/000058)
	// pode gravar NULL aqui — r.ObraCentroCusto é `string`, não ponteiro,
	// mesmo campo lido pelo Pedido de retirada (que nunca grava NULL); sem o
	// COALESCE, escanear uma linha de Baixa/Transferência sem Obra quebraria
	// esta leitura com um erro de Scan.
	q := `
		SELECT p.id, p.usuario_id, p.solicitante, COALESCE(p.obra_centro_custo, ''), p.observacao, p.status, p.criado_em, p.tipo,
		       (SELECT count(*) FROM pedido_itens pi WHERE pi.pedido_id = p.id), cc.nome
		FROM pedidos p
		LEFT JOIN centros_custo cc ON cc.id = p.centro_custo_id AND cc.empresa_id = p.empresa_id
		WHERE p.usuario_id = $1 AND p.empresa_id = $2`
	args := []any{usuarioID, empresaID}
	if filtroStatus != "" {
		q += " AND p.status = $3"
		args = append(args, filtroStatus)
	}
	q += " ORDER BY p.criado_em DESC, p.id DESC"

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar pedidos próprios: %w", err)
	}
	defer rows.Close()

	resumos := make([]PedidoResumo, 0)
	for rows.Next() {
		var r PedidoResumo
		var observacao sql.NullString
		var centroCusto sql.NullString
		if err := rows.Scan(
			&r.ID, &r.UsuarioID, &r.Solicitante, &r.ObraCentroCusto,
			&observacao, &r.Status, &r.CriadoEm, &r.Tipo, &r.QtdItens, &centroCusto,
		); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de pedido próprio: %w", err)
		}
		if observacao.Valid {
			r.Observacao = &observacao.String
		}
		if centroCusto.Valid {
			r.CentroCustoNome = &centroCusto.String
		}
		resumos = append(resumos, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar pedidos próprios: %w", err)
	}
	return resumos, nil
}

// ListarPedidosFila devolve TODOS os Pedidos DA EMPRESA `empresaID` (sem
// filtro por `usuario_id`, mas SEMPRE com o de Empresa — Story 9.1, AD-20), do mais recente ao mais antigo, cada um com a contagem de
// itens — a Fila do Almoxarife (Story 7.4, spec-7-4). Mesma
// validação/projeção/scan/ordenação de ListarPedidosProprios (Story 7.3), só
// sem a cláusula `WHERE p.usuario_id = $1`. `filtroStatus` vazio -> sem
// filtro; não-vazio e fora de statusPedidoValido -> &ErroPedidoValidacao
// (400 VALIDATION_ERROR) ANTES de qualquer query, mesmo contrato de
// ListarPedidosProprios. Quem pode chamar esta função (autorização por
// papel) é decidido por ListarPedidosParaSessao, nunca aqui.
func ListarPedidosFila(db *sql.DB, empresaID string, filtroStatus string) ([]PedidoResumo, error) {
	if filtroStatus != "" && !statusPedidoValido[filtroStatus] {
		return nil, &ErroPedidoValidacao{Mensagem: "status inválido"}
	}

	// Mesmo escopo por Empresa na subquery de contagem — ver o comentário
	// equivalente em ListarPedidosProprios. Mesmo COALESCE(obra_centro_custo,
	// '') de ListarPedidosProprios (Story 20.2/20.1, migrations/000057/000058).
	q := `
		SELECT p.id, p.usuario_id, p.solicitante, COALESCE(p.obra_centro_custo, ''), p.observacao, p.status, p.criado_em, p.tipo,
		       COALESCE(i.qtd, 0), cc.nome
		FROM pedidos p
		LEFT JOIN centros_custo cc ON cc.id = p.centro_custo_id AND cc.empresa_id = p.empresa_id
		LEFT JOIN (
			SELECT pedido_id, count(*) AS qtd FROM pedido_itens WHERE empresa_id = $1 GROUP BY pedido_id
		) i ON i.pedido_id = p.id
		WHERE p.empresa_id = $1`
	args := []any{empresaID}
	if filtroStatus != "" {
		q += " AND p.status = $2"
		args = append(args, filtroStatus)
	}
	q += " ORDER BY p.criado_em DESC, p.id DESC"

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar fila de pedidos: %w", err)
	}
	defer rows.Close()

	resumos := make([]PedidoResumo, 0)
	for rows.Next() {
		var r PedidoResumo
		var observacao sql.NullString
		var centroCusto sql.NullString
		if err := rows.Scan(
			&r.ID, &r.UsuarioID, &r.Solicitante, &r.ObraCentroCusto,
			&observacao, &r.Status, &r.CriadoEm, &r.Tipo, &r.QtdItens, &centroCusto,
		); err != nil {
			return nil, fmt.Errorf("falha ao ler linha da fila de pedidos: %w", err)
		}
		if observacao.Valid {
			r.Observacao = &observacao.String
		}
		if centroCusto.Valid {
			r.CentroCustoNome = &centroCusto.String
		}
		resumos = append(resumos, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar fila de pedidos: %w", err)
	}
	return resumos, nil
}

// ListarPedidosParaSessao decide o escopo de GET /api/pedidos (Story 7.4,
// spec-7-4) — MESMA rota, parâmetro opcional `?escopo=todos` — a partir do
// papel JÁ resolvido pelo contexto da requisição, passado explicitamente
// (molde de services.ListarUsuarios, usuarios.go:33, AD-8 forma 3; o handler
// nunca chama RankPapel ele mesmo).
//
// `escopoTodos && RankPapel(papel) >= RankPapel(PapelAlmoxarife)` -> chama
// ListarPedidosFila (todos os Pedidos da organização). Qualquer outro caso —
// `escopoTodos` falso OU papel insuficiente — chama ListarPedidosProprios
// inalterado: um Usuário sem papel almoxarife+ que force `?escopo=todos`
// recebe só os próprios Pedidos, nunca um erro (epics.md Story 7.4 AC2).
func ListarPedidosParaSessao(db *sql.DB, empresaID string, usuarioID, papel string, escopoTodos bool, filtroStatus string) ([]PedidoResumo, error) {
	if escopoTodos && RankPapel(papel) >= RankPapel(PapelAlmoxarife) {
		return ListarPedidosFila(db, empresaID, filtroStatus)
	}
	return ListarPedidosProprios(db, empresaID, usuarioID, filtroStatus)
}

// BuscarPedidoProprio devolve o cabeçalho + os itens em snapshot do Pedido
// `pedidoID`, liberado ao DONO (`pedido.usuario_id == usuarioID`) OU a um
// papel que alcança `almoxarife` na hierarquia (AD-8: a Story 7.5/7.6
// precisa que um Almoxarife carregue um Pedido que não é dele — negar isso
// agora obrigaria a reescrever a checagem depois).
//
// `sql.ErrNoRows` (id inexistente OU de outra Empresa — Story 9.1),
// SQLSTATE 22P02 (id malformado, não-UUID — mesmo colapso de
// ObterProdutoDetalhe) e "Pedido de outro Usuário sem
// papel suficiente" colapsam TODOS em ErrPedidoNaoEncontrado: a fronteira
// HTTP traduz para um único 404 NOT_FOUND, nunca revelando a existência de
// um Pedido alheio, nunca respondendo 403.
//
// Os itens vêm sempre do SNAPSHOT em `pedido_itens` (AD-17) — nunca um join
// ao vivo com `produtos`/`estoques`.
func BuscarPedidoProprio(db *sql.DB, empresaID string, pedidoID, usuarioID, papel string) (PedidoDetalhe, error) {
	var det PedidoDetalhe
	var observacao sql.NullString
	var centroCusto sql.NullString
	// Mesmo COALESCE(obra_centro_custo, '') de ListarPedidosProprios/Fila
	// (Story 20.2/20.1, migrations/000057/000058).
	const selectPedido = `
		SELECT p.id, p.usuario_id, p.solicitante, COALESCE(p.obra_centro_custo, ''), p.observacao, p.status, p.criado_em, p.tipo, cc.nome
		FROM pedidos p
		LEFT JOIN centros_custo cc ON cc.id = p.centro_custo_id AND cc.empresa_id = p.empresa_id
		WHERE p.id = $1 AND p.empresa_id = $2`
	if err := db.QueryRow(selectPedido, pedidoID, empresaID).Scan(
		&det.ID, &det.UsuarioID, &det.Solicitante, &det.ObraCentroCusto,
		&observacao, &det.Status, &det.CriadoEm, &det.Tipo, &centroCusto,
	); err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return PedidoDetalhe{}, ErrPedidoNaoEncontrado
		}
		return PedidoDetalhe{}, fmt.Errorf("falha ao buscar pedido: %w", err)
	}
	if observacao.Valid {
		det.Observacao = &observacao.String
	}
	if centroCusto.Valid {
		det.CentroCustoNome = &centroCusto.String
	}

	if det.UsuarioID != usuarioID && RankPapel(papel) < RankPapel(PapelAlmoxarife) {
		return PedidoDetalhe{}, ErrPedidoNaoEncontrado
	}

	const selectItens = `
		SELECT pi.produto_id, pi.produto_nome, pi.categoria_nome, pi.estoque_id, pi.estoque_nome, pi.quantidade, pi.quantidade_aprovada,
		       COALESCE(pr.inativado_em IS NOT NULL, false), pr.codigo
		FROM pedido_itens pi
		LEFT JOIN produtos pr ON pr.id = pi.produto_id AND pr.empresa_id = pi.empresa_id
		WHERE pi.pedido_id = $1 AND pi.empresa_id = $2 ORDER BY pi.produto_nome`
	rows, err := db.Query(selectItens, pedidoID, empresaID)
	if err != nil {
		return PedidoDetalhe{}, fmt.Errorf("falha ao listar itens do pedido: %w", err)
	}
	defer rows.Close()

	det.Itens = make([]PedidoItem, 0)
	for rows.Next() {
		var it PedidoItem
		var quantidadeAprovada sql.NullFloat64
		var codigo sql.NullString
		if err := rows.Scan(
			&it.ProdutoID, &it.ProdutoNome, &it.CategoriaNome,
			&it.EstoqueID, &it.EstoqueNome, &it.Quantidade, &quantidadeAprovada, &it.Inativo, &codigo,
		); err != nil {
			return PedidoDetalhe{}, fmt.Errorf("falha ao ler item do pedido: %w", err)
		}
		if quantidadeAprovada.Valid {
			it.QuantidadeAprovada = &quantidadeAprovada.Float64
		}
		if codigo.Valid {
			it.ProdutoCodigo = &codigo.String
		}
		det.Itens = append(det.Itens, it)
	}
	if err := rows.Err(); err != nil {
		return PedidoDetalhe{}, fmt.Errorf("falha ao iterar itens do pedido: %w", err)
	}
	return det, nil
}

// itemPedidoParaDecisao é a leitura crua de uma linha de `pedido_itens`
// usada só dentro de DecidirPedido — inclui o snapshot completo (nome de
// produto/categoria/estoque) para que a resposta de DecidirPedido possa ser
// montada SEM reconsultar o banco depois do commit (ver comentário de
// DecidirPedido).
type itemPedidoParaDecisao struct {
	ProdutoID     string
	ProdutoNome   string
	CategoriaNome string
	EstoqueID     string
	EstoqueNome   string
	Quantidade    float64
}

// DecidirPedido aprova ou rejeita um Pedido `pendente`
// (POST /api/pedidos/{id}/decisao, Story 7.5, spec-7-5), revalidando a
// disponibilidade real de cada item na MESMA transação da decisão — nunca
// confia no snapshot gravado no envio (Story 7.2). `decisorID`/`papelDecisor`
// vêm do contexto da sessão (o papel já foi revalidado pelo
// `RequireRole(almoxarife)` da rota, a cada requisição — nenhuma checagem de
// papel adicional acontece aqui).
//
// Guards (antes de qualquer escrita):
//   - `id` inexistente ou não-UUID (`pq` 22P02) -> ErrPedidoNaoEncontrado.
//   - `status != "pendente"` -> ErrPedidoNaoPendente.
//
// Dentro da transação, o UPDATE guardado de `pedidos`
// (`WHERE id = $1 AND status = 'pendente'`) fecha a corrida entre duas
// decisões concorrentes para o MESMO Pedido: `sql.ErrNoRows` no RETURNING ->
// mesmo ErrPedidoNaoPendente, rollback desfaz qualquer débito já feito nesta
// transação (molde de DecidirSolicitacao, promocao.go).
//
// `aprovar=false`: nenhuma leitura/trava de `produto_estoque`, nenhuma
// Movimentação — todos os itens gravam `quantidade_aprovada=0` (nunca NULL
// depois de decidido), status vira `'rejeitado'`.
//
// `aprovar=true` (Story 11.5, AD-24/AD-25): os itens vêm ordenados por
// (produto_id, estoque_id) e o conjunto COMPLETO de pares é travado por
// travarSaldoParesTx (AD-10: produto_estoque + lotes, ordem canônica) ANTES de
// ler reserva/saldo e de qualquer escrita. Por item,
// `quantidadeAprovada := min(solicitada, reserva do PRÓPRIO Pedido, saldo
// físico do par)` (arredondada a 3 casas; reserva ausente = 0). NÃO compara com
// o saldo livre: reserva de outros Pedidos nunca reduz o aprovável. Em
// condição normal (reserva íntegra) a aprovada é igual à solicitada; só um bug
// de reserva reduz o item. Se `> 0`, debita por consumirFEFOTx (FEFO único) e
// grava 1 Movimentação `tipo='baixa'` por fonte consumida
// (inserirMovimentacaoConsumoTx; `lote_id` NULL para o legado,
// `usuario_id`=o DECISOR, origem=Estoque do item) na MESMA transação;
// `quantidadeAprovada` é sempre gravada em `pedido_itens`, mesmo quando 0.
// Status final do cabeçalho: `'aprovado'` se TODOS os itens tiveram
// `quantidadeAprovada == quantidade`; senão `'parcialmente_aprovado'` (inclui
// 0 em TODOS os itens — o Almoxarife escolheu aprovar, não rejeitar; nunca
// reclassificado como `'rejeitado'`). Débito, liberação das reservas
// (liberarReservasPedidoTx) e decisão são atômicos.
//
// Sucesso: devolve a MESMA projeção de PedidoDetalhe (cabeçalho + itens,
// cada um com `quantidadeAprovada` preenchido) — montada inteiramente com
// dados já lidos/computados DENTRO da transação, ANTES do commit. Nenhuma
// releitura do banco acontece depois de `tx.Commit()`: se um novo round-trip
// pós-commit falhasse (blip transitório de conexão, por exemplo), o chamador
// receberia um 500 mesmo com a decisão já durável (estoque já debitado,
// Movimentação já registrada, status já mudado) — um falso-negativo enganoso
// para uma mudança de estado que já aconteceu de verdade. Por isso o
// cabeçalho vem do próprio `RETURNING` do UPDATE de decisão (abaixo) e os
// itens vêm do snapshot lido no início da transação (`itemPedidoParaDecisao`)
// combinado com `quantidadeAprovada` computado no loop.
func DecidirPedido(db *sql.DB, empresaID string, pedidoID, decisorID, papelDecisor string, aprovar bool) (PedidoDetalhe, error) {
	var status, tipo string
	const selectStatus = `SELECT status, tipo FROM pedidos WHERE id = $1 AND empresa_id = $2`
	if err := db.QueryRow(selectStatus, pedidoID, empresaID).Scan(&status, &tipo); err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return PedidoDetalhe{}, ErrPedidoNaoEncontrado
		}
		return PedidoDetalhe{}, fmt.Errorf("falha ao consultar pedido para decisão: %w", err)
	}
	if status != "pendente" {
		return PedidoDetalhe{}, ErrPedidoNaoPendente
	}
	// Story 20.1 (guard crítico, Spec Change Log): nenhum Pedido de tipo
	// diferente de `retirada` pode ser aprovado NEM rejeitado por esta Fila
	// — checado ANTES de qualquer trava/débito (nenhuma transação sequer
	// aberta). Só a Story 20.3 implementa a decisão de verdade por tipo.
	if tipo != "retirada" {
		return PedidoDetalhe{}, ErrPedidoTipoNaoSuportado
	}

	tx, err := db.Begin()
	if err != nil {
		return PedidoDetalhe{}, fmt.Errorf("falha ao iniciar transação de decisão do pedido: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	// Snapshot completo dos itens (nome de produto/categoria/estoque
	// incluídos) lido uma única vez, ANTES de qualquer trava de
	// produto_estoque. A ordem ascendente por (produto_id, estoque_id) é o
	// AD-10 exigido só para `aprovar=true` (ordem dos locks) — inofensiva
	// para `aprovar=false`, que não trava produto_estoque nenhum. Servirá
	// tanto para computar a decisão quanto para montar a resposta final sem
	// releitura pós-commit.
	const selectItens = `
		SELECT produto_id, produto_nome, categoria_nome, estoque_id, estoque_nome, quantidade
		FROM pedido_itens WHERE pedido_id = $1 AND empresa_id = $2 ORDER BY produto_id, estoque_id`
	rows, err := tx.Query(selectItens, pedidoID, empresaID)
	if err != nil {
		return PedidoDetalhe{}, fmt.Errorf("falha ao listar itens do pedido para decisão: %w", err)
	}
	var itens []itemPedidoParaDecisao
	for rows.Next() {
		var it itemPedidoParaDecisao
		if err := rows.Scan(&it.ProdutoID, &it.ProdutoNome, &it.CategoriaNome, &it.EstoqueID, &it.EstoqueNome, &it.Quantidade); err != nil {
			rows.Close()
			return PedidoDetalhe{}, fmt.Errorf("falha ao ler item do pedido para decisão: %w", err)
		}
		itens = append(itens, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return PedidoDetalhe{}, fmt.Errorf("falha ao iterar itens do pedido para decisão: %w", err)
	}
	rows.Close()

	novoStatus := "rejeitado"
	itensResposta := make([]PedidoItem, 0, len(itens))
	if aprovar {
		const selectReserva = `
			SELECT quantidade FROM reservas_pedido_item
			WHERE pedido_id = $1 AND produto_id = $2 AND estoque_id = $3 AND empresa_id = $4`
		const updateItem = `
			UPDATE pedido_itens SET quantidade_aprovada = $1
			WHERE pedido_id = $2 AND produto_id = $3 AND estoque_id = $4 AND empresa_id = $5`

		// AD-10: trava o conjunto COMPLETO de pares ANTES de ler reserva/saldo
		// e de qualquer escrita (travarSaldoParesTx ordena internamente).
		pares := make([]ParSaldo, 0, len(itens))
		for _, it := range itens {
			pares = append(pares, ParSaldo{ProdutoID: it.ProdutoID, EstoqueID: it.EstoqueID})
		}
		if err := travarSaldoParesTx(tx, empresaID, pares); err != nil {
			return PedidoDetalhe{}, fmt.Errorf("falha ao travar saldo dos pares na decisão: %w", err)
		}

		totalmenteAprovado := true
		for _, it := range itens {
			var reserva float64
			if err := tx.QueryRow(selectReserva, pedidoID, it.ProdutoID, it.EstoqueID, empresaID).Scan(&reserva); err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					return PedidoDetalhe{}, fmt.Errorf("falha ao ler reserva do item na decisão: %w", err)
				}
				reserva = 0
			}
			fisico, err := saldoFisicoParTx(tx, empresaID, it.ProdutoID, it.EstoqueID)
			if err != nil {
				return PedidoDetalhe{}, err
			}

			// Revalida contra a reserva do PRÓPRIO Pedido (limitada ao saldo
			// físico), nunca contra o saldo livre.
			quantidadeAprovada := arredondar3(it.Quantidade)
			if r := arredondar3(reserva); r < quantidadeAprovada {
				quantidadeAprovada = r
			}
			if f := arredondar3(fisico); f < quantidadeAprovada {
				quantidadeAprovada = f
			}
			if quantidadeAprovada < 0 {
				quantidadeAprovada = 0
			}
			if quantidadeAprovada < arredondar3(it.Quantidade) {
				totalmenteAprovado = false
				// Com reserva dura (AD-25) isto só deveria ocorrer por bug de
				// reserva ou saldo físico inconsistente — registra para não
				// ficar invisível (o Almoxarife só vê "parcial").
				slog.Warn("aprovação de pedido reduziu a quantidade de um item",
					"pedido_id", pedidoID, "produto_id", it.ProdutoID, "estoque_id", it.EstoqueID,
					"solicitada", it.Quantidade, "reserva", reserva, "saldo_fisico", fisico,
					"aprovada", quantidadeAprovada)
			} else {
				quantidadeAprovada = it.Quantidade
			}

			if quantidadeAprovada > 0 {
				consumos, err := consumirFEFOTx(tx, empresaID, it.ProdutoID, it.EstoqueID, quantidadeAprovada)
				if err != nil {
					return PedidoDetalhe{}, fmt.Errorf("falha ao debitar saldo por FEFO na decisão: %w", err)
				}
				for _, c := range consumos {
					if _, err := inserirMovimentacaoConsumoTx(tx, empresaID, "baixa", it.ProdutoID, it.EstoqueID, nil, decisorID, c); err != nil {
						return PedidoDetalhe{}, err
					}
				}
			}
			if _, err := tx.Exec(updateItem, quantidadeAprovada, pedidoID, it.ProdutoID, it.EstoqueID, empresaID); err != nil {
				return PedidoDetalhe{}, fmt.Errorf("falha ao gravar quantidade aprovada do item na decisão: %w", err)
			}

			itensResposta = append(itensResposta, PedidoItem{
				ProdutoID:          it.ProdutoID,
				ProdutoNome:        it.ProdutoNome,
				CategoriaNome:      it.CategoriaNome,
				EstoqueID:          it.EstoqueID,
				EstoqueNome:        it.EstoqueNome,
				Quantidade:         it.Quantidade,
				QuantidadeAprovada: &quantidadeAprovada,
			})
		}

		if totalmenteAprovado {
			novoStatus = "aprovado"
		} else {
			novoStatus = "parcialmente_aprovado"
		}
	} else {
		const zerarItens = `UPDATE pedido_itens SET quantidade_aprovada = 0 WHERE pedido_id = $1 AND empresa_id = $2`
		if _, err := tx.Exec(zerarItens, pedidoID, empresaID); err != nil {
			return PedidoDetalhe{}, fmt.Errorf("falha ao zerar quantidade aprovada na rejeição: %w", err)
		}
		for _, it := range itens {
			zero := 0.0
			itensResposta = append(itensResposta, PedidoItem{
				ProdutoID:          it.ProdutoID,
				ProdutoNome:        it.ProdutoNome,
				CategoriaNome:      it.CategoriaNome,
				EstoqueID:          it.EstoqueID,
				EstoqueNome:        it.EstoqueNome,
				Quantidade:         it.Quantidade,
				QuantidadeAprovada: &zero,
			})
		}
	}

	// O cabeçalho da resposta vem inteiro deste RETURNING — inclusive
	// decidido_por/decidido_em (Story 7.5, P5: auditoria testável a partir
	// do próprio retorno) — nenhuma segunda consulta depois do commit.
	var det PedidoDetalhe
	var observacao sql.NullString
	var decididoPor sql.NullString
	var decididoEm sql.NullTime
	var centroCustoDecisao sql.NullString
	// COALESCE(obra_centro_custo, ''): hoje este RETURNING só é alcançado
	// para `tipo = 'retirada'` (o guard acima já recusou qualquer outro tipo
	// antes de abrir a transação) — nunca NULL na prática. Protegido por
	// precaução (revisão pós-Story 20.2): o dia em que a Story 20.3 remover
	// esse guard pra decidir Transferência/Baixa de verdade, este Scan não
	// quebra com um Pedido de Baixa sem Obra/Centro de Custo. Mesmo molde de
	// ListarPedidosProprios/ListarPedidosFila/BuscarPedidoProprio acima.
	const registrarDecisao = `
		UPDATE pedidos SET status = $2, decidido_por = $3, decidido_em = now()
		WHERE id = $1 AND status = 'pendente' AND empresa_id = $4
		RETURNING id, usuario_id, solicitante, COALESCE(obra_centro_custo, ''), observacao, status, criado_em, decidido_por, decidido_em, tipo,
		  (SELECT cc.nome FROM centros_custo cc WHERE cc.id = pedidos.centro_custo_id AND cc.empresa_id = pedidos.empresa_id)`
	if err := tx.QueryRow(registrarDecisao, pedidoID, novoStatus, decisorID, empresaID).Scan(
		&det.ID, &det.UsuarioID, &det.Solicitante, &det.ObraCentroCusto,
		&observacao, &det.Status, &det.CriadoEm, &decididoPor, &decididoEm, &det.Tipo, &centroCustoDecisao,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Outra requisição decidiu este Pedido entre o SELECT inicial e
			// este UPDATE (corrida entre duas decisões concorrentes) — o
			// rollback do defer desfaz qualquer débito já feito acima.
			return PedidoDetalhe{}, ErrPedidoNaoPendente
		}
		return PedidoDetalhe{}, fmt.Errorf("falha ao registrar decisão do pedido: %w", err)
	}
	// Story 11.3: liberar = apagar. Depois do UPDATE guardado (uma decisão
	// concorrente perdedora já saiu acima com ErrPedidoNaoPendente); a parte
	// aprovada já saiu do saldo físico pelo débito desta transação e a não
	// aprovada volta a ficar disponível por deixar de ser reservada.
	if err := liberarReservasPedidoTx(tx, empresaID, pedidoID); err != nil {
		return PedidoDetalhe{}, err
	}
	if observacao.Valid {
		det.Observacao = &observacao.String
	}
	if centroCustoDecisao.Valid {
		det.CentroCustoNome = &centroCustoDecisao.String
	}
	if decididoPor.Valid {
		det.DecididoPor = &decididoPor.String
	}
	if decididoEm.Valid {
		det.DecididoEm = &decididoEm.Time
	}

	if err := tx.Commit(); err != nil {
		return PedidoDetalhe{}, fmt.Errorf("falha ao commitar decisão do pedido: %w", err)
	}

	// Mesma ordem de BuscarPedidoProprio (alfabética por produto_nome) —
	// `itensResposta` foi montado na ordem de locks (produto_id, estoque_id),
	// não na ordem de exibição.
	sort.Slice(itensResposta, func(i, j int) bool {
		return itensResposta[i].ProdutoNome < itensResposta[j].ProdutoNome
	})
	det.Itens = itensResposta

	return det, nil
}

// --- Recibo do Pedido em PDF — Story 7.6 (Epic 7, Pedidos de Retirada), ----
// --- spec-7-6 ---------------------------------------------------------------
//
// Três funções deliberadamente separadas (Design Notes de spec-7-6):
// MontarReciboPedidoConteudo (busca+gate+formata, sem gopdf, testável com
// asserções Go simples) / RenderizarReciboPedidoPDF (só desenha a partir do
// struct já pronto, pura e determinística) / GerarReciboPedidoPDF (as duas em
// sequência — é o que BaixarReciboPedidoHandler chama).

// ErrPedidoSemRecibo indica que o Pedido `pedidoID` ainda não foi decidido
// (`status` fora de {aprovado, parcialmente_aprovado}) — `pendente` ainda não
// teve nenhuma retirada, `rejeitado` não teve retirada nenhuma. Mapeado para
// 409 CONFLICT.
var ErrPedidoSemRecibo = errors.New("pedido ainda não foi decidido: nenhum recibo disponível")

// ReciboPedidoItem é uma linha do recibo em PDF: Produto/Categoria/Estoque
// vêm do MESMO snapshot de PedidoItem (AD-17, nunca um join ao vivo com
// `produtos`) e as quantidades solicitada/retirada. `QuantidadeAprovada` é
// sempre um valor concreto (nunca ponteiro) porque MontarReciboPedidoConteudo
// só chega aqui depois do gate de status — `quantidade_aprovada` nunca é
// NULL para um item de Pedido já decidido (DecidirPedido, Story 7.5, sempre
// grava um valor, mesmo 0).
type ReciboPedidoItem struct {
	ProdutoNome        string
	CategoriaNome      string
	EstoqueNome        string
	Quantidade         float64
	QuantidadeAprovada float64
	Inativo            bool // Story 16.2
}

// ReciboPedidoConteudo é o conteúdo já resolvido/formatado do recibo — um
// struct de dados simples, SEM nenhuma dependência de gopdf (Design Notes de
// spec-7-6): RenderizarReciboPedidoPDF só desenha a partir daqui, o que torna
// MontarReciboPedidoConteudo testável sem precisar ler bytes de PDF de volta.
type ReciboPedidoConteudo struct {
	PedidoID        string
	Solicitante     string
	ObraCentroCusto string
	CentroCusto     string // nome do Centro de Custo cadastrado; "" quando não escolhido
	Status          string
	Aprovador       string
	DecididoEm      time.Time
	Itens           []ReciboPedidoItem
}

// MontarReciboPedidoConteudo busca e monta o conteúdo do recibo do Pedido
// `pedidoID` (Story 7.6, spec-7-6) para GET /api/pedidos/{id}/recibo.
//
// Chama BuscarPedidoProprio PRIMEIRO — reaproveita inteiramente a mesma
// autorização (dono OU `almoxarife`+, AD-8) e o MESMO colapso de
// ErrPedidoNaoEncontrado (id malformado/inexistente, Pedido alheio sem papel
// suficiente, tudo no mesmo erro -> 404 NOT_FOUND na fronteira HTTP).
//
// Só então checa `det.Status`: fora de {aprovado, parcialmente_aprovado} ->
// ErrPedidoSemRecibo (409 CONFLICT) ANTES de resolver o aprovador ou montar
// qualquer coisa.
//
// O nome do aprovador vem de uma query dedicada
// (`SELECT u.nome FROM pedidos p JOIN usuarios u ON u.id = p.decidido_por
// WHERE p.id = $1`) — NUNCA estende o SELECT de BuscarPedidoProprio/o JSON de
// GET /api/pedidos*, que continuam sem decididoPor/decididoEm (Never,
// spec-7-5). Resolver o nome atual de `usuarios` no momento do download não
// viola AD-17: essa invariante fixa só a imutabilidade do snapshot de
// PEDIDO_ITENS contra PRODUTOS, não um join com `usuarios` (Design Notes de
// spec-7-6).
func MontarReciboPedidoConteudo(db *sql.DB, empresaID string, pedidoID, usuarioID, papel string) (ReciboPedidoConteudo, error) {
	det, err := BuscarPedidoProprio(db, empresaID, pedidoID, usuarioID, papel)
	if err != nil {
		return ReciboPedidoConteudo{}, err
	}
	if det.Status != "aprovado" && det.Status != "parcialmente_aprovado" {
		return ReciboPedidoConteudo{}, ErrPedidoSemRecibo
	}

	// Aprovador + data da decisão vêm da MESMA query dedicada — nunca do
	// SELECT de BuscarPedidoProprio (que nunca lê decidido_por/decidido_em de
	// volta, Never de spec-7-5) nem de PedidoDetalhe.DecididoEm (que
	// BuscarPedidoProprio deixa sempre nil).
	var aprovador string
	var decididoEm time.Time
	const selectAprovadorEData = `
		SELECT u.nome, p.decidido_em FROM pedidos p
		JOIN usuarios u ON u.id = p.decidido_por AND u.empresa_id = $2
		WHERE p.id = $1 AND p.empresa_id = $2`
	if err := db.QueryRow(selectAprovadorEData, pedidoID, empresaID).Scan(&aprovador, &decididoEm); err != nil {
		return ReciboPedidoConteudo{}, fmt.Errorf("falha ao resolver aprovador/data da decisão do recibo: %w", err)
	}

	itens := make([]ReciboPedidoItem, 0, len(det.Itens))
	for _, it := range det.Itens {
		var aprovada float64
		if it.QuantidadeAprovada != nil {
			aprovada = *it.QuantidadeAprovada
		}
		itens = append(itens, ReciboPedidoItem{
			ProdutoNome:        it.ProdutoNome,
			CategoriaNome:      it.CategoriaNome,
			EstoqueNome:        it.EstoqueNome,
			Quantidade:         it.Quantidade,
			QuantidadeAprovada: aprovada,
			Inativo:            it.Inativo,
		})
	}

	return ReciboPedidoConteudo{
		PedidoID:        det.ID,
		Solicitante:     det.Solicitante,
		ObraCentroCusto: det.ObraCentroCusto,
		CentroCusto:     nomeCentroCusto(det.CentroCustoNome),
		Status:          det.Status,
		Aprovador:       aprovador,
		DecididoEm:      decididoEm,
		Itens:           itens,
	}, nil
}

// formatarQuantidadeRecibo formata uma quantidade do recibo sem notação
// científica e sem zeros à direita — mesmo molde de `strconv.FormatFloat(v,
// 'f', -1, 64)` já usado por `ExportarMovimentacoesXLSX`
// (movimentacoes.go:148).
func formatarQuantidadeRecibo(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// alturaPaginaReciboPT / margemReciboPT fecham as dimensões usadas pelo
// controle de quebra de página manual de RenderizarReciboPedidoPDF — mesmos
// valores de gopdf.PageSizeA4 (595x842pt) e da margem passada a
// pdf.SetMargins abaixo.
const (
	alturaPaginaReciboPT = 842.0
	margemReciboPT       = 40.0
	larguraUtilReciboPT  = 595.0 - 2*margemReciboPT
)

// RenderizarReciboPedidoPDF desenha o recibo em PDF (Story 7.6, spec-7-6) a
// partir de um ReciboPedidoConteudo JÁ MONTADO — função pura e
// determinística: a MESMA entrada produz SEMPRE os MESMOS bytes (Always,
// spec-7-6: nenhuma metadata dinâmica é embutida — `pdf.SetInfo` nunca é
// chamado, nenhum `time.Now()` em lugar nenhum — para o PDF continuar
// byte-idêntico entre dois downloads do MESMO Pedido mesmo que o Produto
// referenciado seja editado entre eles).
//
// Fonte: `pdf.AddTTFFontData("gofont", goregular.TTF)` de
// `golang.org/x/image/font/gofont/goregular` — dependência já pinada em
// go.mod, cobre acentuação PT-BR sem exigir um `.ttf` novo no repositório
// (Design Notes de spec-7-6).
//
// Cada item mostra "Retirado" (quantidadeAprovada) sempre; quando
// `QuantidadeAprovada != Quantidade`, também mostra "Solicitado" e
// "Pendente" — mesma regra condicional de `FilaPedidosSection` (Story 7.5),
// nunca escondida quando diverge.
func RenderizarReciboPedidoPDF(conteudo ReciboPedidoConteudo) ([]byte, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.SetMargins(margemReciboPT, margemReciboPT, margemReciboPT, margemReciboPT)
	pdf.AddPage()

	if err := pdf.AddTTFFontData("gofont", goregular.TTF); err != nil {
		return nil, fmt.Errorf("falha ao carregar fonte do recibo: %w", err)
	}

	escreverLinha := func(texto string, tamanho float64) error {
		if err := pdf.SetFont("gofont", "", tamanho); err != nil {
			return fmt.Errorf("falha ao selecionar fonte do recibo: %w", err)
		}
		altura := tamanho + 6
		if pdf.GetY()+altura > alturaPaginaReciboPT-margemReciboPT {
			pdf.AddPage()
		}
		if err := pdf.Cell(&gopdf.Rect{W: larguraUtilReciboPT, H: altura}, texto); err != nil {
			return fmt.Errorf("falha ao escrever linha do recibo: %w", err)
		}
		pdf.Br(altura)
		return nil
	}

	if err := escreverLinha("Recibo do Pedido", 18); err != nil {
		return nil, err
	}
	pdf.Br(6)

	if err := escreverLinha("Solicitante: "+conteudo.Solicitante, 11); err != nil {
		return nil, err
	}
	if err := escreverLinha("Obra/Centro de Custo: "+conteudo.ObraCentroCusto, 11); err != nil {
		return nil, err
	}
	if conteudo.CentroCusto != "" {
		if err := escreverLinha("Centro de Custo: "+conteudo.CentroCusto, 11); err != nil {
			return nil, err
		}
	}
	if err := escreverLinha("Aprovador: "+conteudo.Aprovador, 11); err != nil {
		return nil, err
	}
	if err := escreverLinha("Data da decisão: "+conteudo.DecididoEm.Format("02/01/2006 15:04"), 11); err != nil {
		return nil, err
	}
	pdf.Br(10)

	if err := escreverLinha("Itens retirados", 13); err != nil {
		return nil, err
	}
	pdf.Br(4)

	for _, item := range conteudo.Itens {
		linha := fmt.Sprintf("%s — %s · %s", item.ProdutoNome, item.CategoriaNome, item.EstoqueNome)
		if item.Inativo {
			linha += " (Inativo)"
		}
		if err := escreverLinha(linha, 10); err != nil {
			return nil, err
		}

		quantidades := "Retirado: " + formatarQuantidadeRecibo(item.QuantidadeAprovada)
		if item.QuantidadeAprovada != item.Quantidade {
			quantidades += "   Solicitado: " + formatarQuantidadeRecibo(item.Quantidade) +
				"   Pendente: " + formatarQuantidadeRecibo(item.Quantidade-item.QuantidadeAprovada)
		}
		if err := escreverLinha(quantidades, 10); err != nil {
			return nil, err
		}
		pdf.Br(4)
	}

	bytes, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		return nil, fmt.Errorf("falha ao gerar bytes do PDF do recibo: %w", err)
	}
	return bytes, nil
}

// GerarReciboPedidoPDF compõe MontarReciboPedidoConteudo +
// RenderizarReciboPedidoPDF em sequência — é a função que
// BaixarReciboPedidoHandler chama (Story 7.6, spec-7-6). Qualquer erro de
// autorização/gate de status vem de MontarReciboPedidoConteudo
// (ErrPedidoNaoEncontrado / ErrPedidoSemRecibo); RenderizarReciboPedidoPDF só
// pode falhar por erro de biblioteca (fonte/renderização), mapeado para 500
// INTERNAL_ERROR pelo handler.
func GerarReciboPedidoPDF(db *sql.DB, empresaID string, pedidoID, usuarioID, papel string) ([]byte, error) {
	conteudo, err := MontarReciboPedidoConteudo(db, empresaID, pedidoID, usuarioID, papel)
	if err != nil {
		return nil, err
	}
	return RenderizarReciboPedidoPDF(conteudo)
}

func nomeCentroCusto(n *string) string {
	if n == nil {
		return ""
	}
	return *n
}

// IndicadoresPedido são os contadores exibidos na faixa de indicadores das
// páginas de Pedidos (Story 17.4) — calculados de uma única query, escopados
// por papel e empresa.
type IndicadoresPedido struct {
	Pendentes     int `json:"pendentes"`
	AprovadosMes  int `json:"aprovadosMes"`
	RejeitadosMes int `json:"rejeitadosMes"`
}

// IndicadoresPedidos conta os indicadores da faixa de Pedidos (Story 17.4,
// spec-17-4). Se `escopoTodos && RankPapel(papel) >= RankPapel(PapelAlmoxarife)`:
// conta todos os Pedidos da Empresa; caso contrário, conta só os do próprio
// Usuário — nunca retorna erro de autorização (mesmo padrão de
// ListarPedidosParaSessao).
//
// $1 = empresaID; $2 = usuarioID (escopo próprio) ou ausente (escopo todos).
// Indicadores: Pendentes = status='pendente'; AprovadosMes = status IN
// ('aprovado','parcialmente_aprovado') AND decidido_em no mês corrente;
// RejeitadosMes = status='rejeitado' AND decidido_em no mês corrente.
func IndicadoresPedidos(db *sql.DB, empresaID, usuarioID, papel string, escopoTodos bool) (IndicadoresPedido, error) {
	var ind IndicadoresPedido
	if escopoTodos && RankPapel(papel) >= RankPapel(PapelAlmoxarife) {
		const q = `
			SELECT
			  COUNT(*) FILTER (WHERE status = 'pendente'),
			  COUNT(*) FILTER (WHERE status IN ('aprovado','parcialmente_aprovado')
			    AND decidido_em >= date_trunc('month', CURRENT_DATE)),
			  COUNT(*) FILTER (WHERE status = 'rejeitado'
			    AND decidido_em >= date_trunc('month', CURRENT_DATE))
			FROM pedidos
			WHERE empresa_id = $1`
		if err := db.QueryRow(q, empresaID).Scan(&ind.Pendentes, &ind.AprovadosMes, &ind.RejeitadosMes); err != nil {
			return IndicadoresPedido{}, fmt.Errorf("falha ao calcular indicadores de pedidos (todos): %w", err)
		}
	} else {
		const q = `
			SELECT
			  COUNT(*) FILTER (WHERE status = 'pendente'),
			  COUNT(*) FILTER (WHERE status IN ('aprovado','parcialmente_aprovado')
			    AND decidido_em >= date_trunc('month', CURRENT_DATE)),
			  COUNT(*) FILTER (WHERE status = 'rejeitado'
			    AND decidido_em >= date_trunc('month', CURRENT_DATE))
			FROM pedidos
			WHERE empresa_id = $1 AND usuario_id = $2`
		if err := db.QueryRow(q, empresaID, usuarioID).Scan(&ind.Pendentes, &ind.AprovadosMes, &ind.RejeitadosMes); err != nil {
			return IndicadoresPedido{}, fmt.Errorf("falha ao calcular indicadores de pedidos (próprios): %w", err)
		}
	}
	return ind, nil
}
