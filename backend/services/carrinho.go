// Package services, arquivo carrinho.go: Carrinho de reserva — Story 7.1
// (Epic 7, Pedidos de Retirada), spec-7-1. Acumula Produto + Estoque +
// quantidade por Usuário ANTES de um Pedido de Retirada existir (Stories
// 7.2+, fora do escopo desta story). Toda operação é escopada ao
// `usuarioID` da sessão — nunca um id vindo do cliente (Always, spec-7-1);
// quem garante isso é o handler, lendo `middleware.UsuarioDaSessao`.
package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/lib/pq"
)

// ItemCarrinho é a projeção de uma linha ATIVA de carrinho_itens —
// devolvida por AdicionarItemCarrinho e ListarCarrinho — com o nome do
// Produto e do Estoque já resolvidos por JOIN (o frontend nunca precisa de
// uma segunda chamada por linha).
type ItemCarrinho struct {
	ProdutoID   string  `json:"produtoId"`
	ProdutoNome string  `json:"produtoNome"`
	EstoqueID   string  `json:"estoqueId"`
	EstoqueNome string  `json:"estoqueNome"`
	Quantidade  float64 `json:"quantidade"`
}

// Motivos de remoção preguiçosa devolvidos por ListarCarrinho (Always,
// spec-7-1) — string fixa, consumida pelo frontend para escolher a
// mensagem de aviso exibida por item removido.
const (
	// MotivoCarrinhoProdutoRemovido: o Produto do item foi mesclado (Story
	// 6.4, Normalização de Dados) — `produtos.deleted_at` preenchido.
	MotivoCarrinhoProdutoRemovido = "produto_removido"
	// MotivoCarrinhoEstoqueExcluido: o Estoque do item foi excluído (Story
	// 2.2) — hard-delete, a linha em `estoques` não existe mais.
	MotivoCarrinhoEstoqueExcluido = "estoque_excluido"
)

// ItemCarrinhoRemovido é a projeção de uma linha removida preguiçosamente
// por ListarCarrinho: o Produto foi mesclado ou o Estoque foi excluído
// desde que o item entrou no carrinho. `EstoqueNome` é `nil` quando
// `Motivo == MotivoCarrinhoEstoqueExcluido` — a linha física de `estoques`
// não existe mais para resolver o nome.
type ItemCarrinhoRemovido struct {
	ProdutoID   string  `json:"produtoId"`
	ProdutoNome string  `json:"produtoNome"`
	EstoqueID   string  `json:"estoqueId"`
	EstoqueNome *string `json:"estoqueNome"`
	Motivo      string  `json:"motivo"`
}

// ErroCarrinhoValidacao é o erro de validação devolvido por
// AdicionarItemCarrinho quando `quantidade` é zero, negativa, ou maior que
// limiteNumeric103 — sempre verificado ANTES de abrir a transação, nenhuma
// escrita acontece quando este erro é devolvido. Mapeado para
// 400 VALIDATION_ERROR. Mesmo molde de ErroMovimentacaoValidacao
// (movimentacoes.go).
type ErroCarrinhoValidacao struct {
	Mensagem string
}

func (e *ErroCarrinhoValidacao) Error() string { return e.Mensagem }

// ErroCarrinhoIndisponivel indica que
// `quantidade_já_no_carrinho_para_o_par + quantidade_pedida` excede o saldo
// disponível na linha de `produto_estoque` travada por
// AdicionarItemCarrinho — ou que o Estoque existe mas não há linha nenhuma
// para o par (produto_id, estoque_id) (`Restante: 0`, par nunca teve saldo
// lançado). Quando o próprio Estoque não existe (id malformado ou
// hard-deletado, Story 2.2), o erro devolvido é ErrCarrinhoEstoqueNaoEncontrado
// (404), não este (409) — ver a checagem de existência de Estoque em
// AdicionarItemCarrinho. `Restante` é quanto AINDA pode ser adicionado a
// este par (disponível - já no carrinho), não o saldo bruto do Estoque — a
// mensagem do carrinho é sobre "quanto falta para caber no que já está
// reservado", não sobre "quanto existe no total". Mapeado para 409 CONFLICT.
type ErroCarrinhoIndisponivel struct {
	Restante float64
}

func (e *ErroCarrinhoIndisponivel) Error() string {
	return fmt.Sprintf(
		"quantidade indisponível: apenas %s unidade(s) disponível(is) para adicionar ao carrinho",
		strconv.FormatFloat(e.Restante, 'f', -1, 64),
	)
}

// ErrCarrinhoProdutoNaoEncontrado indica que `produtoId` não existe, está
// mesclado (soft-deletado, Story 6.4, `produtos.deleted_at` preenchido) ou é
// malformado (não-UUID) — os três colapsam no mesmo 404 NOT_FOUND. Dedicado
// ao carrinho (em vez de reaproveitar ErrProdutoNaoEncontrado, produtos.go):
// a checagem de "Produto ainda ativo" aqui é responsabilidade isolada desta
// story, nunca compartilhada com CriarProduto/AtualizarNomeProduto/
// ObterProdutoDetalhe.
var ErrCarrinhoProdutoNaoEncontrado = errors.New("produto não encontrado")

// ErrCarrinhoEstoqueNaoEncontrado indica que `estoqueId` (sintaticamente um
// UUID válido) não existe em `estoques` — hard-deletado (Story 2.2) entre o
// carregamento da página e a submissão do diálogo "Adicionar ao Carrinho",
// por exemplo. Distinto de ErroCarrinhoIndisponivel: aqui a causa real é um
// Estoque inexistente, não falta de saldo — devolver 409 "quantidade
// indisponível" nesse caso seria enganoso. Mapeado para 404 NOT_FOUND, mesmo
// padrão de ErrCarrinhoProdutoNaoEncontrado.
var ErrCarrinhoEstoqueNaoEncontrado = errors.New("estoque não encontrado")

// ErrCarrinhoItemNaoEncontrado indica que o par (produtoId, estoqueId) não
// tem linha em carrinho_itens para o usuário — id malformado (pq 22P02)
// colapsa no mesmo 404, mesmo padrão de ErrEstoqueNaoEncontrado
// (estoques.go). Mapeado para 404 NOT_FOUND.
var ErrCarrinhoItemNaoEncontrado = errors.New("item não encontrado no carrinho")

// AdicionarItemCarrinho adiciona `quantidade` unidades do Produto
// `produtoID` no Estoque `estoqueID` ao carrinho do Usuário `usuarioID`
// (Story 7.1, spec-7-1) — upsert incrementando quando o par já está no
// carrinho (`PRIMARY KEY (usuario_id, produto_id, estoque_id)`).
//
// Validação de `quantidade` (zero/negativa ou acima de limiteNumeric103,
// mesmo limite de RegistrarBaixa) acontece ANTES de `tx.Begin()`: nenhuma
// escrita, nenhum lock adquirido para um pedido já inválido.
//
// Dentro da transação: o Produto precisa existir e estar ativo
// (`deleted_at IS NULL`) -> senão ErrCarrinhoProdutoNaoEncontrado, ANTES de
// tocar produto_estoque (nenhum lock adquirido para um Produto que nem
// existe mais). Em seguida, o Estoque precisa existir em `estoques`
// (estoqueID malformado ou hard-deletado, Story 2.2, colapsam no mesmo
// caso) -> senão ErrCarrinhoEstoqueNaoEncontrado (404), distinto do 409 de
// "sem saldo": o carrinho nunca deve dizer "indisponível" quando a causa
// real é um Estoque que nem existe mais. Só então `SELECT ... FOR UPDATE`
// trava a linha de `produto_estoque` (molde de RegistrarBaixa,
// movimentacoes.go) — ausência de linha colapsa em "0 disponível", NUNCA
// cria linha nova (ao contrário de travarLinhaProdutoEstoque, usado só por
// Transferência — Design Notes de spec-7-1: o carrinho não é dono de saldo
// de Estoque). Essa mesma trava serializa duas adições concorrentes ao
// MESMO par (produto_id, estoque_id), inclusive do MESMO usuário (AC5): a
// segunda só prossegue depois que a primeira commita, e por isso enxerga a
// quantidade já somada ao carrinho pela vencedora antes de validar a sua
// própria.
//
// `quantidade_já_no_carrinho_para_o_par + quantidade` > disponível ->
// &ErroCarrinhoIndisponivel{Restante: disponivel - jaNoCarrinho}, carrinho
// inalterado. Caso contrário, upsert incrementando e commit único.
func AdicionarItemCarrinho(db *sql.DB, usuarioID, produtoID, estoqueID string, quantidade float64) (ItemCarrinho, error) {
	if quantidade <= 0 {
		return ItemCarrinho{}, &ErroCarrinhoValidacao{Mensagem: "quantidade deve ser maior que zero"}
	}
	if quantidade > limiteNumeric103 {
		return ItemCarrinho{}, &ErroCarrinhoValidacao{
			Mensagem: fmt.Sprintf("quantidade deve ser no máximo %s", limiteNumeric103Texto),
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return ItemCarrinho{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	var produtoNome string
	const selectProduto = `SELECT nome FROM produtos WHERE id = $1 AND deleted_at IS NULL`
	if err := tx.QueryRow(selectProduto, produtoID).Scan(&produtoNome); err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return ItemCarrinho{}, ErrCarrinhoProdutoNaoEncontrado
		}
		return ItemCarrinho{}, fmt.Errorf("falha ao verificar produto do carrinho: %w", err)
	}

	// O Estoque precisa existir de verdade ANTES de tratar a ausência de
	// linha em produto_estoque como "0 disponível" — sem esta checagem, um
	// Estoque hard-deletado (Story 2.2, ex.: excluído entre o carregamento da
	// página e a submissão do diálogo) colapsaria na mesma
	// ErroCarrinhoIndisponivel{Restante: 0} de "par nunca teve saldo
	// lançado" (409, mensagem enganosa sobre "quantidade indisponível").
	// estoqueID malformado (não-UUID) colapsa no mesmo 404, mesmo padrão de
	// ErrCarrinhoProdutoNaoEncontrado acima.
	var estoqueExiste bool
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM estoques WHERE id = $1)`, estoqueID).Scan(&estoqueExiste); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return ItemCarrinho{}, ErrCarrinhoEstoqueNaoEncontrado
		}
		return ItemCarrinho{}, fmt.Errorf("falha ao verificar estoque do carrinho: %w", err)
	}
	if !estoqueExiste {
		return ItemCarrinho{}, ErrCarrinhoEstoqueNaoEncontrado
	}

	var disponivel float64
	const selectDisponivel = `
		SELECT quantidade FROM produto_estoque
		WHERE produto_id = $1 AND estoque_id = $2
		FOR UPDATE`
	if err := tx.QueryRow(selectDisponivel, produtoID, estoqueID).Scan(&disponivel); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ItemCarrinho{}, &ErroCarrinhoIndisponivel{Restante: 0}
		}
		return ItemCarrinho{}, fmt.Errorf("falha ao travar linha de produto_estoque: %w", err)
	}

	var jaNoCarrinho float64
	const selectJaNoCarrinho = `
		SELECT quantidade FROM carrinho_itens
		WHERE usuario_id = $1 AND produto_id = $2 AND estoque_id = $3`
	if err := tx.QueryRow(selectJaNoCarrinho, usuarioID, produtoID, estoqueID).Scan(&jaNoCarrinho); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ItemCarrinho{}, fmt.Errorf("falha ao ler quantidade já no carrinho: %w", err)
	}

	if jaNoCarrinho+quantidade > disponivel {
		restante := disponivel - jaNoCarrinho
		if restante < 0 {
			restante = 0
		}
		return ItemCarrinho{}, &ErroCarrinhoIndisponivel{Restante: restante}
	}

	// A linha de produto_estoque travada acima referencia um estoque_id com
	// FK `ON DELETE CASCADE` para estoques(id) (estoques.go) — se chegamos
	// até aqui, a linha de estoques correspondente existe garantidamente.
	var estoqueNome string
	if err := tx.QueryRow(`SELECT nome FROM estoques WHERE id = $1`, estoqueID).Scan(&estoqueNome); err != nil {
		return ItemCarrinho{}, fmt.Errorf("falha ao resolver nome do estoque: %w", err)
	}

	var quantidadeFinal float64
	const upsert = `
		INSERT INTO carrinho_itens (usuario_id, produto_id, estoque_id, quantidade)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (usuario_id, produto_id, estoque_id)
		DO UPDATE SET quantidade = carrinho_itens.quantidade + EXCLUDED.quantidade, atualizado_em = now()
		RETURNING quantidade`
	if err := tx.QueryRow(upsert, usuarioID, produtoID, estoqueID, quantidade).Scan(&quantidadeFinal); err != nil {
		return ItemCarrinho{}, fmt.Errorf("falha ao gravar item do carrinho: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return ItemCarrinho{}, fmt.Errorf("falha ao commitar adição ao carrinho: %w", err)
	}

	return ItemCarrinho{
		ProdutoID:   produtoID,
		ProdutoNome: produtoNome,
		EstoqueID:   estoqueID,
		EstoqueNome: estoqueNome,
		Quantidade:  quantidadeFinal,
	}, nil
}

// linhaCarrinhoBruta é a leitura crua de uma linha de carrinho_itens JOIN
// produtos (JOIN, sempre presente — produto_id sem CASCADE) LEFT JOIN
// estoques (pode não existir mais) — usada só dentro de ListarCarrinho para
// separar itens ativos de itens obsoletos antes de decidir o que apagar.
type linhaCarrinhoBruta struct {
	produtoID, produtoNome, estoqueID string
	produtoDeletedEm                  sql.NullTime
	estoqueNome                       sql.NullString
	quantidade                        float64
}

// ListarCarrinho devolve os itens ATIVOS do carrinho do Usuário `usuarioID`
// (Story 7.1, spec-7-1) mais os itens removidos preguiçosamente NESTA MESMA
// chamada (Always): qualquer linha cujo Produto tenha `deleted_at`
// preenchido (mesclado, Story 6.4) ou cujo `estoque_id` não exista mais em
// `estoques` (excluído, Story 2.2) é apagada de `carrinho_itens` e
// devolvida em `removidos` com o motivo — nunca na lista `itens`. Leitura +
// limpeza acontecem na mesma transação; nem `itens` nem `removidos` são
// `nil` (carrinho vazio, ou sem nenhum item obsoleto, não é erro).
//
// Esta função NUNCA modifica MesclarDuplicatas (Story 6.4) nem a exclusão
// de Estoque (Story 2.2, Never de spec-7-1) — é só leitura-e-limpeza do lado
// do carrinho, na direção oposta.
func ListarCarrinho(db *sql.DB, usuarioID string) ([]ItemCarrinho, []ItemCarrinhoRemovido, error) {
	itens := make([]ItemCarrinho, 0)
	removidos := make([]ItemCarrinhoRemovido, 0)

	tx, err := db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	const q = `
		SELECT ci.produto_id, p.nome, p.deleted_at, ci.estoque_id, e.nome, ci.quantidade
		FROM carrinho_itens ci
		JOIN produtos p ON p.id = ci.produto_id
		LEFT JOIN estoques e ON e.id = ci.estoque_id
		WHERE ci.usuario_id = $1
		ORDER BY ci.criado_em ASC`
	rows, err := tx.Query(q, usuarioID)
	if err != nil {
		return nil, nil, fmt.Errorf("falha ao listar carrinho: %w", err)
	}

	var linhas []linhaCarrinhoBruta
	for rows.Next() {
		var l linhaCarrinhoBruta
		if err := rows.Scan(
			&l.produtoID, &l.produtoNome, &l.produtoDeletedEm,
			&l.estoqueID, &l.estoqueNome, &l.quantidade,
		); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("falha ao ler linha do carrinho: %w", err)
		}
		linhas = append(linhas, l)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, fmt.Errorf("falha ao iterar carrinho: %w", err)
	}
	rows.Close()

	const deleteObsoleto = `DELETE FROM carrinho_itens WHERE usuario_id = $1 AND produto_id = $2 AND estoque_id = $3`
	for _, l := range linhas {
		switch {
		case l.produtoDeletedEm.Valid:
			var estoqueNomePtr *string
			if l.estoqueNome.Valid {
				n := l.estoqueNome.String
				estoqueNomePtr = &n
			}
			removidos = append(removidos, ItemCarrinhoRemovido{
				ProdutoID: l.produtoID, ProdutoNome: l.produtoNome,
				EstoqueID: l.estoqueID, EstoqueNome: estoqueNomePtr,
				Motivo: MotivoCarrinhoProdutoRemovido,
			})
			if _, err := tx.Exec(deleteObsoleto, usuarioID, l.produtoID, l.estoqueID); err != nil {
				return nil, nil, fmt.Errorf("falha ao limpar item de produto mesclado do carrinho: %w", err)
			}
		case !l.estoqueNome.Valid:
			removidos = append(removidos, ItemCarrinhoRemovido{
				ProdutoID: l.produtoID, ProdutoNome: l.produtoNome,
				EstoqueID: l.estoqueID, EstoqueNome: nil,
				Motivo: MotivoCarrinhoEstoqueExcluido,
			})
			if _, err := tx.Exec(deleteObsoleto, usuarioID, l.produtoID, l.estoqueID); err != nil {
				return nil, nil, fmt.Errorf("falha ao limpar item de estoque excluído do carrinho: %w", err)
			}
		default:
			itens = append(itens, ItemCarrinho{
				ProdutoID: l.produtoID, ProdutoNome: l.produtoNome,
				EstoqueID: l.estoqueID, EstoqueNome: l.estoqueNome.String,
				Quantidade: l.quantidade,
			})
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("falha ao commitar limpeza do carrinho: %w", err)
	}
	return itens, removidos, nil
}

// RemoverItemCarrinho remove a linha do par (produtoID, estoqueID) do
// carrinho do Usuário `usuarioID` (Story 7.1, spec-7-1). Par sem linha (ou
// id malformado, pq 22P02) -> ErrCarrinhoItemNaoEncontrado, mesmo colapso de
// ErrEstoqueNaoEncontrado (estoques.go).
func RemoverItemCarrinho(db *sql.DB, usuarioID, produtoID, estoqueID string) error {
	res, err := db.Exec(
		`DELETE FROM carrinho_itens WHERE usuario_id = $1 AND produto_id = $2 AND estoque_id = $3`,
		usuarioID, produtoID, estoqueID,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return ErrCarrinhoItemNaoEncontrado
		}
		return fmt.Errorf("falha ao remover item do carrinho: %w", err)
	}
	linhas, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("falha ao ler linhas afetadas na remoção do carrinho: %w", err)
	}
	if linhas == 0 {
		return ErrCarrinhoItemNaoEncontrado
	}
	return nil
}
