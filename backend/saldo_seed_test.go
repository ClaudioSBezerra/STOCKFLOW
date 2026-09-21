package main

import (
	"database/sql"
	"testing"

	"stockflow/backend/services"
)

// semearSaldoLegado grava uma linha em `produto_estoque` (saldo legado, sem
// Lote) para um Produto já cadastrado. Desde a Story 11.6 CriarProduto não
// cria mais saldo (FR8, AD-29).
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

// criarProdutoComSaldo chama services.CriarProduto e, quando `estoqueID` !=
// "", semeia o saldo legado `quantidade` no Estoque.
func criarProdutoComSaldo(db *sql.DB, empresaID string, in services.CriarProdutoInput, estoqueID string, quantidade float64) (services.Produto, error) {
	p, err := services.CriarProduto(db, empresaID, in)
	if err != nil {
		return p, err
	}
	if estoqueID != "" {
		if err := inserirSaldoLegado(db, p.ID, estoqueID, quantidade); err != nil {
			return services.Produto{}, err
		}
	}
	return p, nil
}
