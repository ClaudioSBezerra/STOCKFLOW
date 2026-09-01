package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/lib/pq"
)

// Movimentacao é a projeção devolvida por RegistrarBaixa (Story 5.1,
// spec-5-1) — trilha de auditoria de toda escrita em
// `produto_estoque.quantidade`. `EstoqueDestinoID` fica sempre `nil` para
// `tipo="baixa"` (a coluna existe desde já, mas só a Story 5.2, Transferência,
// a lê/escreve).
type Movimentacao struct {
	ID               string    `json:"id"`
	ProdutoID        string    `json:"produtoId"`
	Tipo             string    `json:"tipo"`
	EstoqueOrigemID  string    `json:"estoqueOrigemId"`
	EstoqueDestinoID *string   `json:"estoqueDestinoId"`
	Quantidade       float64   `json:"quantidade"`
	UsuarioID        string    `json:"usuarioId"`
	CriadoEm         time.Time `json:"criadoEm"`
}

// ErroMovimentacaoValidacao é o erro de validação devolvido por
// RegistrarBaixa quando `quantidade` é zero, negativa, ou maior que
// limiteNumeric103 — sempre verificado ANTES de abrir a transação, nenhuma
// escrita acontece quando este erro é devolvido. Mapeado para
// 400 VALIDATION_ERROR. Mesmo molde de ErroProdutoValidacao.
type ErroMovimentacaoValidacao struct {
	Mensagem string
}

func (e *ErroMovimentacaoValidacao) Error() string { return e.Mensagem }

// ErroQuantidadeIndisponivel indica que a quantidade pedida excede o saldo
// disponível na linha de `produto_estoque` travada por RegistrarBaixa — ou
// que não existe linha nenhuma para o par (produto_id, estoque_id) (`
// Disponivel: 0`, mesmo colapso usado para um `id`/`estoqueId` malformado,
// ver Design Notes de spec-5-1). Mapeado para 409 CONFLICT.
//
// `strconv.FormatFloat(..., 'f', -1, 64)` — nunca `%v`/`%g` — evita notação
// científica em valores pequenos, mesmo cuidado de limiteNumeric103Texto.
type ErroQuantidadeIndisponivel struct {
	Disponivel float64
}

func (e *ErroQuantidadeIndisponivel) Error() string {
	return fmt.Sprintf(
		"quantidade indisponível: apenas %s unidade(s) disponível(is)",
		strconv.FormatFloat(e.Disponivel, 'f', -1, 64),
	)
}

// RegistrarBaixa registra o consumo de `quantidade` unidades do Produto
// `produtoID` no Estoque `estoqueID`, debitando `produto_estoque.quantidade`
// e inserindo a Movimentação `tipo='baixa'` correspondente numa ÚNICA
// transação (Story 5.1, spec-5-1) — nunca uma escrita sem a outra.
//
// Validação de `quantidade` (zero/negativa ou acima de limiteNumeric103)
// acontece ANTES de `tx.Begin()`: nenhuma escrita, nenhum lock adquirido
// para um pedido já inválido.
//
// Depois: `SELECT ... FOR UPDATE` trava a linha de `produto_estoque` (molde
// transacional de ExcluirEstoque, estoques.go). SQLSTATE 22P02
// (produtoID/estoqueID malformado, não-UUID) e sql.ErrNoRows (par válido mas
// sem linha — Produto nunca teve saldo nesse Estoque) colapsam ambos em
// &ErroQuantidadeIndisponivel{Disponivel: 0}: nenhuma AC desta story pede um
// 404 distinto aqui (ver Design Notes de spec-5-1). `quantidade` maior que o
// saldo travado -> &ErroQuantidadeIndisponivel{Disponivel: disponivel}, sem
// debitar nada.
//
// Caso contrário, debita a linha e insere a Movimentação na mesma transação,
// commit único.
func RegistrarBaixa(db *sql.DB, produtoID, estoqueID, usuarioID string, quantidade float64) (Movimentacao, error) {
	if quantidade <= 0 {
		return Movimentacao{}, &ErroMovimentacaoValidacao{Mensagem: "quantidade deve ser maior que zero"}
	}
	if quantidade > limiteNumeric103 {
		return Movimentacao{}, &ErroMovimentacaoValidacao{
			Mensagem: fmt.Sprintf("quantidade deve ser no máximo %s", limiteNumeric103Texto),
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	var disponivel float64
	const selectDisponivel = `
		SELECT quantidade FROM produto_estoque
		WHERE produto_id = $1 AND estoque_id = $2
		FOR UPDATE`
	if err := tx.QueryRow(selectDisponivel, produtoID, estoqueID).Scan(&disponivel); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return Movimentacao{}, &ErroQuantidadeIndisponivel{Disponivel: 0}
		}
		if errors.Is(err, sql.ErrNoRows) {
			return Movimentacao{}, &ErroQuantidadeIndisponivel{Disponivel: 0}
		}
		return Movimentacao{}, fmt.Errorf("falha ao travar linha de produto_estoque: %w", err)
	}

	if quantidade > disponivel {
		return Movimentacao{}, &ErroQuantidadeIndisponivel{Disponivel: disponivel}
	}

	const update = `
		UPDATE produto_estoque SET quantidade = quantidade - $1
		WHERE produto_id = $2 AND estoque_id = $3`
	if _, err := tx.Exec(update, quantidade, produtoID, estoqueID); err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao debitar produto_estoque: %w", err)
	}

	var mov Movimentacao
	const insert = `
		INSERT INTO movimentacoes (produto_id, tipo, estoque_origem_id, quantidade, usuario_id)
		VALUES ($1, 'baixa', $2, $3, $4)
		RETURNING id, produto_id, tipo, estoque_origem_id, quantidade, usuario_id, criado_em`
	if err := tx.QueryRow(insert, produtoID, estoqueID, quantidade, usuarioID).Scan(
		&mov.ID, &mov.ProdutoID, &mov.Tipo, &mov.EstoqueOrigemID, &mov.Quantidade, &mov.UsuarioID, &mov.CriadoEm,
	); err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao inserir movimentação de baixa: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao commitar baixa: %w", err)
	}
	return mov, nil
}
