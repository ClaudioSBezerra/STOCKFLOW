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
	"strings"
	"time"
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
