package services

import (
	"database/sql"
	"testing"
)

// semearSaldoLegado grava uma linha em `produto_estoque` (saldo legado, sem
// Lote) para um Produto já cadastrado. Desde a Story 11.6 CriarProduto não
// cria mais saldo (FR8, AD-29); os testes que precisam de saldo o semeiam por
// aqui, diretamente via SQL.
func semearSaldoLegado(t *testing.T, db *sql.DB, produtoID, estoqueID string, quantidade float64) {
	t.Helper()
	if err := inserirSaldoLegado(db, produtoID, estoqueID, quantidade); err != nil {
		t.Fatalf("semearSaldoLegado(%s,%s,%v): %v", produtoID, estoqueID, quantidade, err)
	}
}

func inserirSaldoLegado(db *sql.DB, produtoID, estoqueID string, quantidade float64) error {
	_, err := db.Exec(
		`INSERT INTO produto_estoque (produto_id, estoque_id, quantidade) VALUES ($1, $2, $3)`,
		produtoID, estoqueID, quantidade,
	)
	return err
}

// criarProdutoComSaldo chama CriarProduto e, quando `estoqueID` != "", semeia
// o saldo legado `quantidade` no Estoque (o que o cadastro fazia antes da
// Story 11.6). Mesma forma de retorno de CriarProduto.
func criarProdutoComSaldo(db *sql.DB, empresaID string, in CriarProdutoInput, estoqueID string, quantidade float64) (Produto, error) {
	p, err := CriarProduto(db, empresaID, in)
	if err != nil {
		return p, err
	}
	if estoqueID != "" {
		if err := inserirSaldoLegado(db, p.ID, estoqueID, quantidade); err != nil {
			return Produto{}, err
		}
	}
	return p, nil
}

// criarProdutoCatComSaldo é criarProdutoCat + semeadura de saldo legado.
func criarProdutoCatComSaldo(t *testing.T, db *sql.DB, in CriarProdutoInput, estoqueID string, quantidade float64) (id string, codigo string) {
	t.Helper()
	id, codigo = criarProdutoCat(t, db, in)
	if estoqueID != "" {
		semearSaldoLegado(t, db, id, estoqueID, quantidade)
	}
	return id, codigo
}
