// Package services, arquivo pedidos.go: Envio de Pedido — Story 7.2 (Epic
// 7, Pedidos de Retirada), spec-7-2. Formaliza o conteúdo do Carrinho
// (Story 7.1) como um Pedido `pendente` que o Almoxarife poderá enfileirar
// e decidir (Stories 7.3+, fora do escopo desta story).
package services

import (
	"database/sql"
	"errors"
	"fmt"
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
	// QuantidadeAprovada (Story 7.5, spec-7-5): nil enquanto o Pedido
	// permanece `pendente`; a partir da decisão, um valor concreto de 0 até
	// `Quantidade` — o quanto de fato foi debitado/aprovado deste item.
	// `Quantidade - *QuantidadeAprovada` é a pendência não atendida, sempre
	// visível (nunca escondida, nunca descartada).
	QuantidadeAprovada *float64 `json:"quantidadeAprovada"`
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
// dos itens são ordenados ascendentemente (molde de RegistrarTransferencia,
// movimentacoes.go — AD-10 do epic-7-context.md) e travados um a um via
// `SELECT ... FOR UPDATE`, revalidando `quantidade <= disponível` no
// momento do envio — NUNCA confia no snapshot da montagem do carrinho
// (Always, spec-7-2). Ausência de linha em produto_estoque colapsa em
// "0 disponível", mesmo colapso de RegistrarBaixa. Qualquer item
// insuficiente -> &ErroPedidoIndisponivel{Itens: [...]} com TODOS os itens
// insuficientes (não só o primeiro), rollback da transação inteira — nada é
// debitado, nada é gravado (Never, spec-7-2: o débito real é da Story 7.5).
//
// Caso contrário: insere `pedidos` (status default 'pendente'), insere uma
// linha de `pedido_itens` por item com o SNAPSHOT de nome/estoque (já
// resolvidos por ListarCarrinho) e `categoria_nome` via join com
// `categorias` (Code Map de spec-7-2), esvazia `carrinho_itens` do usuário
// e commita — tudo na MESMA transação (Always, spec-7-2).
func SubmeterPedido(db *sql.DB, usuarioID, solicitante, obraCentroCusto, observacao string) (Pedido, error) {
	solicitanteTrim := strings.TrimSpace(solicitante)
	if solicitanteTrim == "" {
		return Pedido{}, &ErroPedidoValidacao{Mensagem: "solicitante é obrigatório"}
	}
	obraTrim := strings.TrimSpace(obraCentroCusto)
	if obraTrim == "" {
		return Pedido{}, &ErroPedidoValidacao{Mensagem: "obra/centro de custo é obrigatório"}
	}

	itens, _, err := ListarCarrinho(db, usuarioID)
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

	const selectDisponivel = `
		SELECT quantidade FROM produto_estoque
		WHERE produto_id = $1 AND estoque_id = $2
		FOR UPDATE`
	var indisponiveis []string
	for _, item := range itensOrdenados {
		var disponivel float64
		if err := tx.QueryRow(selectDisponivel, item.ProdutoID, item.EstoqueID).Scan(&disponivel); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				disponivel = 0
			} else {
				return Pedido{}, fmt.Errorf("falha ao travar linha de produto_estoque no envio do pedido: %w", err)
			}
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
		INSERT INTO pedidos (usuario_id, solicitante, obra_centro_custo, observacao)
		VALUES ($1, $2, $3, $4)
		RETURNING id, usuario_id, solicitante, obra_centro_custo, observacao, status, criado_em`
	if err := tx.QueryRow(insertPedido, usuarioID, solicitanteTrim, obraTrim, observacaoNull).Scan(
		&pedido.ID, &pedido.UsuarioID, &pedido.Solicitante, &pedido.ObraCentroCusto,
		&observacaoGravada, &pedido.Status, &pedido.CriadoEm,
	); err != nil {
		return Pedido{}, fmt.Errorf("falha ao inserir pedido: %w", err)
	}
	if observacaoGravada.Valid {
		pedido.Observacao = &observacaoGravada.String
	}

	const selectCategoriaNome = `SELECT c.nome FROM produtos p JOIN categorias c ON c.id = p.categoria_id WHERE p.id = $1`
	const insertItem = `
		INSERT INTO pedido_itens (pedido_id, produto_id, produto_nome, categoria_nome, estoque_id, estoque_nome, quantidade)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	for _, item := range itensOrdenados {
		var categoriaNome string
		if err := tx.QueryRow(selectCategoriaNome, item.ProdutoID).Scan(&categoriaNome); err != nil {
			return Pedido{}, fmt.Errorf("falha ao resolver categoria do item de pedido: %w", err)
		}
		if _, err := tx.Exec(
			insertItem,
			pedido.ID, item.ProdutoID, item.ProdutoNome, categoriaNome, item.EstoqueID, item.EstoqueNome, item.Quantidade,
		); err != nil {
			return Pedido{}, fmt.Errorf("falha ao inserir item de pedido: %w", err)
		}
	}

	if _, err := tx.Exec(`DELETE FROM carrinho_itens WHERE usuario_id = $1`, usuarioID); err != nil {
		return Pedido{}, fmt.Errorf("falha ao esvaziar carrinho após envio do pedido: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Pedido{}, fmt.Errorf("falha ao commitar envio de pedido: %w", err)
	}

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
func ListarPedidosProprios(db *sql.DB, usuarioID, filtroStatus string) ([]PedidoResumo, error) {
	if filtroStatus != "" && !statusPedidoValido[filtroStatus] {
		return nil, &ErroPedidoValidacao{Mensagem: "status inválido"}
	}

	q := `
		SELECT p.id, p.usuario_id, p.solicitante, p.obra_centro_custo, p.observacao, p.status, p.criado_em,
		       COALESCE(i.qtd, 0)
		FROM pedidos p
		LEFT JOIN (SELECT pedido_id, count(*) AS qtd FROM pedido_itens GROUP BY pedido_id) i ON i.pedido_id = p.id
		WHERE p.usuario_id = $1`
	args := []any{usuarioID}
	if filtroStatus != "" {
		q += " AND p.status = $2"
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
		if err := rows.Scan(
			&r.ID, &r.UsuarioID, &r.Solicitante, &r.ObraCentroCusto,
			&observacao, &r.Status, &r.CriadoEm, &r.QtdItens,
		); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de pedido próprio: %w", err)
		}
		if observacao.Valid {
			r.Observacao = &observacao.String
		}
		resumos = append(resumos, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar pedidos próprios: %w", err)
	}
	return resumos, nil
}

// ListarPedidosFila devolve TODOS os Pedidos da organização (sem filtro por
// `usuario_id`), do mais recente ao mais antigo, cada um com a contagem de
// itens — a Fila do Almoxarife (Story 7.4, spec-7-4). Mesma
// validação/projeção/scan/ordenação de ListarPedidosProprios (Story 7.3), só
// sem a cláusula `WHERE p.usuario_id = $1`. `filtroStatus` vazio -> sem
// filtro; não-vazio e fora de statusPedidoValido -> &ErroPedidoValidacao
// (400 VALIDATION_ERROR) ANTES de qualquer query, mesmo contrato de
// ListarPedidosProprios. Quem pode chamar esta função (autorização por
// papel) é decidido por ListarPedidosParaSessao, nunca aqui.
func ListarPedidosFila(db *sql.DB, filtroStatus string) ([]PedidoResumo, error) {
	if filtroStatus != "" && !statusPedidoValido[filtroStatus] {
		return nil, &ErroPedidoValidacao{Mensagem: "status inválido"}
	}

	q := `
		SELECT p.id, p.usuario_id, p.solicitante, p.obra_centro_custo, p.observacao, p.status, p.criado_em,
		       COALESCE(i.qtd, 0)
		FROM pedidos p
		LEFT JOIN (SELECT pedido_id, count(*) AS qtd FROM pedido_itens GROUP BY pedido_id) i ON i.pedido_id = p.id`
	args := []any{}
	if filtroStatus != "" {
		q += " WHERE p.status = $1"
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
		if err := rows.Scan(
			&r.ID, &r.UsuarioID, &r.Solicitante, &r.ObraCentroCusto,
			&observacao, &r.Status, &r.CriadoEm, &r.QtdItens,
		); err != nil {
			return nil, fmt.Errorf("falha ao ler linha da fila de pedidos: %w", err)
		}
		if observacao.Valid {
			r.Observacao = &observacao.String
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
func ListarPedidosParaSessao(db *sql.DB, usuarioID, papel string, escopoTodos bool, filtroStatus string) ([]PedidoResumo, error) {
	if escopoTodos && RankPapel(papel) >= RankPapel(PapelAlmoxarife) {
		return ListarPedidosFila(db, filtroStatus)
	}
	return ListarPedidosProprios(db, usuarioID, filtroStatus)
}

// BuscarPedidoProprio devolve o cabeçalho + os itens em snapshot do Pedido
// `pedidoID`, liberado ao DONO (`pedido.usuario_id == usuarioID`) OU a um
// papel que alcança `almoxarife` na hierarquia (AD-8: a Story 7.5/7.6
// precisa que um Almoxarife carregue um Pedido que não é dele — negar isso
// agora obrigaria a reescrever a checagem depois).
//
// `sql.ErrNoRows` (id inexistente), SQLSTATE 22P02 (id malformado, não-UUID
// — mesmo colapso de ObterProdutoDetalhe) e "Pedido de outro Usuário sem
// papel suficiente" colapsam TODOS em ErrPedidoNaoEncontrado: a fronteira
// HTTP traduz para um único 404 NOT_FOUND, nunca revelando a existência de
// um Pedido alheio, nunca respondendo 403.
//
// Os itens vêm sempre do SNAPSHOT em `pedido_itens` (AD-17) — nunca um join
// ao vivo com `produtos`/`estoques`.
func BuscarPedidoProprio(db *sql.DB, pedidoID, usuarioID, papel string) (PedidoDetalhe, error) {
	var det PedidoDetalhe
	var observacao sql.NullString
	const selectPedido = `
		SELECT id, usuario_id, solicitante, obra_centro_custo, observacao, status, criado_em
		FROM pedidos WHERE id = $1`
	if err := db.QueryRow(selectPedido, pedidoID).Scan(
		&det.ID, &det.UsuarioID, &det.Solicitante, &det.ObraCentroCusto,
		&observacao, &det.Status, &det.CriadoEm,
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

	if det.UsuarioID != usuarioID && RankPapel(papel) < RankPapel(PapelAlmoxarife) {
		return PedidoDetalhe{}, ErrPedidoNaoEncontrado
	}

	const selectItens = `
		SELECT produto_id, produto_nome, categoria_nome, estoque_id, estoque_nome, quantidade, quantidade_aprovada
		FROM pedido_itens WHERE pedido_id = $1 ORDER BY produto_nome`
	rows, err := db.Query(selectItens, pedidoID)
	if err != nil {
		return PedidoDetalhe{}, fmt.Errorf("falha ao listar itens do pedido: %w", err)
	}
	defer rows.Close()

	det.Itens = make([]PedidoItem, 0)
	for rows.Next() {
		var it PedidoItem
		var quantidadeAprovada sql.NullFloat64
		if err := rows.Scan(
			&it.ProdutoID, &it.ProdutoNome, &it.CategoriaNome,
			&it.EstoqueID, &it.EstoqueNome, &it.Quantidade, &quantidadeAprovada,
		); err != nil {
			return PedidoDetalhe{}, fmt.Errorf("falha ao ler item do pedido: %w", err)
		}
		if quantidadeAprovada.Valid {
			it.QuantidadeAprovada = &quantidadeAprovada.Float64
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
// `aprovar=true`: os pares (produto_id, estoque_id) de TODOS os itens do
// Pedido vêm de `SELECT ... ORDER BY produto_id, estoque_id` — ordem
// ascendente sobre o LOTE INTEIRO (AD-10, mesmo molde de SubmeterPedido,
// pedidos.go) — travados um a um nessa ordem via `SELECT ... FOR UPDATE`.
// Ausência de linha em `produto_estoque` colapsa em "0 disponível" (mesmo
// colapso de SubmeterPedido/RegistrarBaixa). Por item,
// `quantidadeAprovada := min(quantidade solicitada, disponível)` — nunca
// mais que o solicitado; se `> 0`, debita `produto_estoque` e insere uma
// Movimentação (`tipo='baixa'`, `estoque_origem_id`=o do item,
// `usuario_id`=o DECISOR — mesma convenção de RegistrarBaixa) na MESMA
// transação; `quantidadeAprovada` é sempre gravada em `pedido_itens`, mesmo
// quando 0. Status final do cabeçalho: `'aprovado'` se TODOS os itens
// tiveram `quantidadeAprovada == quantidade`; senão `'parcialmente_aprovado'`
// (inclui `quantidadeAprovada == 0` em TODOS os itens — o Almoxarife
// escolheu aprovar, não rejeitar; nunca reclassificado como `'rejeitado'`
// por baixo do capô).
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
func DecidirPedido(db *sql.DB, pedidoID, decisorID, papelDecisor string, aprovar bool) (PedidoDetalhe, error) {
	var status string
	const selectStatus = `SELECT status FROM pedidos WHERE id = $1`
	if err := db.QueryRow(selectStatus, pedidoID).Scan(&status); err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return PedidoDetalhe{}, ErrPedidoNaoEncontrado
		}
		return PedidoDetalhe{}, fmt.Errorf("falha ao consultar pedido para decisão: %w", err)
	}
	if status != "pendente" {
		return PedidoDetalhe{}, ErrPedidoNaoPendente
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
		FROM pedido_itens WHERE pedido_id = $1 ORDER BY produto_id, estoque_id`
	rows, err := tx.Query(selectItens, pedidoID)
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
		const selectDisponivel = `
			SELECT quantidade FROM produto_estoque
			WHERE produto_id = $1 AND estoque_id = $2
			FOR UPDATE`
		const updateEstoque = `
			UPDATE produto_estoque SET quantidade = quantidade - $1
			WHERE produto_id = $2 AND estoque_id = $3`
		const insertMovimentacao = `
			INSERT INTO movimentacoes (produto_id, tipo, estoque_origem_id, quantidade, usuario_id)
			VALUES ($1, 'baixa', $2, $3, $4)`
		const updateItem = `
			UPDATE pedido_itens SET quantidade_aprovada = $1
			WHERE pedido_id = $2 AND produto_id = $3 AND estoque_id = $4`

		totalmenteAprovado := true
		for _, it := range itens {
			var disponivel float64
			if err := tx.QueryRow(selectDisponivel, it.ProdutoID, it.EstoqueID).Scan(&disponivel); err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					return PedidoDetalhe{}, fmt.Errorf("falha ao travar linha de produto_estoque na decisão: %w", err)
				}
				disponivel = 0
			}

			quantidadeAprovada := it.Quantidade
			if disponivel < quantidadeAprovada {
				quantidadeAprovada = disponivel
			}
			if quantidadeAprovada < it.Quantidade {
				totalmenteAprovado = false
			}

			if quantidadeAprovada > 0 {
				if _, err := tx.Exec(updateEstoque, quantidadeAprovada, it.ProdutoID, it.EstoqueID); err != nil {
					return PedidoDetalhe{}, fmt.Errorf("falha ao debitar produto_estoque na decisão: %w", err)
				}
				if _, err := tx.Exec(insertMovimentacao, it.ProdutoID, it.EstoqueID, quantidadeAprovada, decisorID); err != nil {
					return PedidoDetalhe{}, fmt.Errorf("falha ao inserir movimentação de baixa na decisão: %w", err)
				}
			}
			if _, err := tx.Exec(updateItem, quantidadeAprovada, pedidoID, it.ProdutoID, it.EstoqueID); err != nil {
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
		const zerarItens = `UPDATE pedido_itens SET quantidade_aprovada = 0 WHERE pedido_id = $1`
		if _, err := tx.Exec(zerarItens, pedidoID); err != nil {
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
	const registrarDecisao = `
		UPDATE pedidos SET status = $2, decidido_por = $3, decidido_em = now()
		WHERE id = $1 AND status = 'pendente'
		RETURNING id, usuario_id, solicitante, obra_centro_custo, observacao, status, criado_em, decidido_por, decidido_em`
	if err := tx.QueryRow(registrarDecisao, pedidoID, novoStatus, decisorID).Scan(
		&det.ID, &det.UsuarioID, &det.Solicitante, &det.ObraCentroCusto,
		&observacao, &det.Status, &det.CriadoEm, &decididoPor, &decididoEm,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Outra requisição decidiu este Pedido entre o SELECT inicial e
			// este UPDATE (corrida entre duas decisões concorrentes) — o
			// rollback do defer desfaz qualquer débito já feito acima.
			return PedidoDetalhe{}, ErrPedidoNaoPendente
		}
		return PedidoDetalhe{}, fmt.Errorf("falha ao registrar decisão do pedido: %w", err)
	}
	if observacao.Valid {
		det.Observacao = &observacao.String
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
}

// ReciboPedidoConteudo é o conteúdo já resolvido/formatado do recibo — um
// struct de dados simples, SEM nenhuma dependência de gopdf (Design Notes de
// spec-7-6): RenderizarReciboPedidoPDF só desenha a partir daqui, o que torna
// MontarReciboPedidoConteudo testável sem precisar ler bytes de PDF de volta.
type ReciboPedidoConteudo struct {
	PedidoID        string
	Solicitante     string
	ObraCentroCusto string
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
func MontarReciboPedidoConteudo(db *sql.DB, pedidoID, usuarioID, papel string) (ReciboPedidoConteudo, error) {
	det, err := BuscarPedidoProprio(db, pedidoID, usuarioID, papel)
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
	const selectAprovadorEData = `SELECT u.nome, p.decidido_em FROM pedidos p JOIN usuarios u ON u.id = p.decidido_por WHERE p.id = $1`
	if err := db.QueryRow(selectAprovadorEData, pedidoID).Scan(&aprovador, &decididoEm); err != nil {
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
		})
	}

	return ReciboPedidoConteudo{
		PedidoID:        det.ID,
		Solicitante:     det.Solicitante,
		ObraCentroCusto: det.ObraCentroCusto,
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
func GerarReciboPedidoPDF(db *sql.DB, pedidoID, usuarioID, papel string) ([]byte, error) {
	conteudo, err := MontarReciboPedidoConteudo(db, pedidoID, usuarioID, papel)
	if err != nil {
		return nil, err
	}
	return RenderizarReciboPedidoPDF(conteudo)
}
