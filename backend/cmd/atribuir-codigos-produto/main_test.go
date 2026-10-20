package main

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"

	"stockflow/backend/services"
)

var (
	migrateOnce sync.Once
	migrateErr  error
)

// testDB abre uma conexão contra DATABASE_URL e aplica as migrations reais.
// Mesmo padrão de cmd/migrar-saldo-lotes/main_test.go.
func testDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL não definido — suba o banco (docker compose up -d db) para rodar os testes de integração")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("falha ao abrir conexão: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("banco indisponível em %s: %v", dsn, err)
	}

	migrateOnce.Do(func() {
		for attempt := 1; attempt <= 5; attempt++ {
			var m *migrate.Migrate
			m, migrateErr = migrate.New("file://../../migrations", dsn)
			if migrateErr == nil {
				migrateErr = m.Up()
				m.Close()
			}
			if migrateErr == nil || errors.Is(migrateErr, migrate.ErrNoChange) {
				migrateErr = nil
				return
			}
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}
	})
	if migrateErr != nil {
		t.Fatalf("falha ao aplicar migrations: %v", migrateErr)
	}
	return db
}

const slugTeste = "codigos-legado-cli"

// semearLegado cria uma Empresa própria com `n` Produtos sem código (estilo
// corte único legado) e devolve seus ids. Banco compartilhado entre pacotes —
// TRUNCATE de propósito restrito à Empresa própria (via DELETE no cleanup),
// nunca `limparProdutos` (isso pertence só ao pacote services).
func semearLegado(t *testing.T, db *sql.DB, nomes ...string) (empresaID string, produtos []string) {
	t.Helper()

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	e, err := services.ProvisionarEmpresa(tx, services.DadosEmpresa{
		NomeFantasia: "Codigos Legado CLI",
		RazaoSocial:  "Codigos Legado CLI LTDA",
		CNPJ:         "10600000000500",
		Slug:         slugTeste,
		Endereco: services.EnderecoEmpresa{
			Logradouro: "Rua de Teste", Numero: "1", Bairro: "Centro",
			Cidade: "Recife", CEP: "50000000", UF: "PE",
		},
	})
	if err != nil {
		t.Fatalf("ProvisionarEmpresa: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	empresaID = e.ID

	t.Cleanup(func() {
		for _, stmt := range []string{
			`DELETE FROM produtos WHERE empresa_id = $1`,
			`DELETE FROM categorias WHERE empresa_id = $1`,
			`DELETE FROM nomenclatura_templates WHERE empresa_id = $1`,
			`DELETE FROM filiais WHERE empresa_id = $1`,
			`DELETE FROM contadores_produto WHERE empresa_id = $1`,
			`DELETE FROM empresas WHERE id = $1`,
		} {
			if _, err := db.Exec(stmt, empresaID); err != nil {
				t.Errorf("cleanup [%s]: %v", stmt, err)
			}
		}
	})

	var categoriaID string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE empresa_id = $1 LIMIT 1`, empresaID).Scan(&categoriaID); err != nil {
		t.Fatalf("categoria: %v", err)
	}
	for i, nome := range nomes {
		var produtoID string
		criadoEm := time.Now().Add(time.Duration(i) * time.Hour).UTC()
		if err := db.QueryRow(
			`INSERT INTO produtos (nome, categoria_id, empresa_id, criado_em) VALUES ($1, $2, $3, $4) RETURNING id`,
			nome, categoriaID, empresaID, criadoEm,
		).Scan(&produtoID); err != nil {
			t.Fatalf("produto: %v", err)
		}
		produtos = append(produtos, produtoID)
	}
	return empresaID, produtos
}

func codigoDe(t *testing.T, db *sql.DB, produtoID string) sql.NullString {
	t.Helper()
	var codigo sql.NullString
	if err := db.QueryRow(`SELECT codigo FROM produtos WHERE id = $1`, produtoID).Scan(&codigo); err != nil {
		t.Fatalf("codigo de %s: %v", produtoID, err)
	}
	return codigo
}

func TestExecutarMigracao_DryRunNaoEscreve(t *testing.T) {
	db := testDB(t)
	_, produtos := semearLegado(t, db, "Legado Dry Run A", "Legado Dry Run B")

	var out bytes.Buffer
	if err := executarMigracao(db, &out, false); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "dry-run") {
		t.Errorf("saída sem \"dry-run\":\n%s", out.String())
	}
	for _, id := range produtos {
		if c := codigoDe(t, db, id); c.Valid {
			t.Errorf("produto %s ganhou código %q no dry-run, want continuar sem código", id, c.String)
		}
	}
}

func TestExecutarMigracao_ExecutarEReexecutar(t *testing.T) {
	db := testDB(t)
	_, produtos := semearLegado(t, db, "Legado Executar A", "Legado Executar B", "Legado Executar C")

	var out bytes.Buffer
	if err := executarMigracao(db, &out, true); err != nil {
		t.Fatalf("executar: %v", err)
	}
	if !strings.Contains(out.String(), "3 produto(s) atualizado(s)") {
		t.Errorf("saída inesperada:\n%s", out.String())
	}
	for _, id := range produtos {
		if c := codigoDe(t, db, id); !c.Valid {
			t.Errorf("produto %s continua sem código após --executar", id)
		}
	}

	out.Reset()
	if err := executarMigracao(db, &out, true); err != nil {
		t.Fatalf("reexecutar: %v", err)
	}
	if !strings.Contains(out.String(), "nada a fazer") {
		t.Errorf("reexecução deveria dizer \"nada a fazer\":\n%s", out.String())
	}
}

func TestExecutarMigracao_SemLegado(t *testing.T) {
	db := testDB(t)
	semearLegado(t, db) // Empresa própria, 0 Produtos — não interfere no diagnóstico global

	var out bytes.Buffer
	if err := executarMigracao(db, &out, true); err != nil {
		t.Fatalf("executar: %v", err)
	}
	// Não afirma "0 produtos no total" (banco compartilhado pode ter outros
	// Produtos legados de outros testes/Empresas) — só que o comando roda sem
	// erro e não quebra com uma Empresa vazia no meio.
	if strings.Contains(out.String(), "erro") {
		t.Errorf("saída com erro inesperado:\n%s", out.String())
	}
}
