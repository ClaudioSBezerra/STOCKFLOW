package main

import (
	"database/sql"
	"testing"
)

// filialTeste devolve o id da Filial padrão (a mais antiga) da Empresa
// `empresaID`, criando-a se faltar — a Empresa de teste pode ter sido
// provisionada antes da migration 000044 (Story 12.1), e a suíte não trunca
// `filiais`.
func filialTeste(t testing.TB, db *sql.DB, empresaID string) string {
	t.Helper()
	var id string
	err := db.QueryRow(
		`SELECT id FROM filiais WHERE empresa_id = $1 ORDER BY criado_em, id LIMIT 1`, empresaID,
	).Scan(&id)
	if err == nil {
		return id
	}
	if err != sql.ErrNoRows {
		t.Fatalf("filialTeste: %v", err)
	}
	if err := db.QueryRow(
		`INSERT INTO filiais (empresa_id, nome) VALUES ($1, 'Filial Teste') RETURNING id`, empresaID,
	).Scan(&id); err != nil {
		t.Fatalf("filialTeste: criar filial: %v", err)
	}
	return id
}
