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
// Pula quando nenhum Postgres foi configurado. A suíte completa roda com
// `go test -p 1 ./...` (banco compartilhado entre pacotes).
//
// Nada aqui trunca tabela alguma: os testes desta suíte criam a própria
// Empresa e a removem no cleanup — o banco é compartilhado e `empresas`
// nunca é truncada por ninguém (precedente da spec-9-1).
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

const slugTeste = "saldo-lotes-cli"

// semearSaldo cria uma Empresa própria com `n` pares Produto/Estoque, cada um
// com `quantidade` em produto_estoque. A tabela de saldo é limpa antes (banco
// compartilhado; a suíte de services faz o mesmo TRUNCATE a cada teste).
func semearSaldo(t *testing.T, db *sql.DB, quantidades ...float64) (empresaID string, produtos []string) {
	t.Helper()
	limpar := func() {
		if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, carrinho_itens, pedido_itens, reservas_pedido_item, pedidos, produto_estoque, lotes, produtos, estoques, movimentacoes`); err != nil {
			t.Fatalf("limpar saldo: %v", err)
		}
	}
	limpar()
	t.Cleanup(func() {
		limpar()
		if empresaID == "" {
			return
		}
		for _, stmt := range []string{
			`DELETE FROM categorias WHERE empresa_id = $1`,
			`DELETE FROM nomenclatura_templates WHERE empresa_id = $1`,
			`DELETE FROM contadores_produto WHERE empresa_id = $1`,
			`DELETE FROM empresas WHERE id = $1`,
		} {
			if _, err := db.Exec(stmt, empresaID); err != nil {
				t.Errorf("cleanup [%s]: %v", stmt, err)
			}
		}
	})

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	e, err := services.ProvisionarEmpresa(tx, services.DadosEmpresa{
		NomeFantasia: "Saldo Lotes CLI",
		RazaoSocial:  "Saldo Lotes CLI LTDA",
		CNPJ:         "10500000004100",
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

	var categoriaID string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE empresa_id = $1 LIMIT 1`, empresaID).Scan(&categoriaID); err != nil {
		t.Fatalf("categoria: %v", err)
	}
	for i, q := range quantidades {
		var estoqueID, produtoID string
		nome := "Estoque CLI " + string(rune('A'+i))
		if err := db.QueryRow(`INSERT INTO estoques (nome, empresa_id) VALUES ($1, $2) RETURNING id`, nome, empresaID).Scan(&estoqueID); err != nil {
			t.Fatalf("estoque: %v", err)
		}
		if err := db.QueryRow(`INSERT INTO produtos (nome, categoria_id, empresa_id) VALUES ($1, $2, $3) RETURNING id`,
			"Produto CLI "+nome, categoriaID, empresaID).Scan(&produtoID); err != nil {
			t.Fatalf("produto: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO produto_estoque (produto_id, estoque_id, quantidade) VALUES ($1, $2, $3)`, produtoID, estoqueID, q); err != nil {
			t.Fatalf("produto_estoque: %v", err)
		}
		produtos = append(produtos, produtoID)
	}
	return empresaID, produtos
}

func contar(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestExecutarMigracao_DryRunNaoEscreve(t *testing.T) {
	db := testDB(t)
	semearSaldo(t, db, 5, 0)

	var out bytes.Buffer
	if err := executarMigracao(db, &out, false); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	s := out.String()
	for _, want := range []string{"dry-run", "1 linha(s), quantidade total 5", "zerada (pulada, sem Lote): 1", "problemas: nenhum"} {
		if !strings.Contains(s, want) {
			t.Errorf("saída sem %q:\n%s", want, s)
		}
	}
	if n := contar(t, db, `SELECT count(*) FROM lotes`); n != 0 {
		t.Errorf("lotes = %d, want 0", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM produto_estoque WHERE quantidade > 0`); n != 1 {
		t.Errorf("produto_estoque com saldo = %d, want 1", n)
	}
}

func TestExecutarMigracao_ExecutarEReexecutar(t *testing.T) {
	db := testDB(t)
	empresaID, _ := semearSaldo(t, db, 5, 2.5, 0)

	var out bytes.Buffer
	if err := executarMigracao(db, &out, true); err != nil {
		t.Fatalf("executar: %v", err)
	}
	if !strings.Contains(out.String(), "2 Lote(s) legado(s) criado(s), quantidade 7.5") {
		t.Errorf("saída inesperada:\n%s", out.String())
	}
	if n := contar(t, db, `SELECT count(*) FROM lotes WHERE empresa_id = $1 AND data_validade IS NULL`, empresaID); n != 2 {
		t.Errorf("lotes = %d, want 2", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM produto_estoque WHERE quantidade > 0`); n != 0 {
		t.Errorf("produto_estoque com saldo = %d, want 0", n)
	}

	out.Reset()
	if err := executarMigracao(db, &out, true); err != nil {
		t.Fatalf("reexecutar: %v", err)
	}
	if !strings.Contains(out.String(), "0 Lote(s) legado(s) criado(s)") {
		t.Errorf("reexecução deveria migrar 0:\n%s", out.String())
	}
	if n := contar(t, db, `SELECT count(*) FROM lotes`); n != 2 {
		t.Errorf("lotes = %d, want 2 (sem duplicar)", n)
	}
}

func TestExecutarMigracao_EmpresaNaoBackfilledAborta(t *testing.T) {
	db := testDB(t)
	_, produtos := semearSaldo(t, db, 5)
	if _, err := db.Exec(`UPDATE produtos SET empresa_id = NULL WHERE id = $1`, produtos[0]); err != nil {
		t.Fatal(err)
	}

	for _, executar := range []bool{false, true} {
		var out bytes.Buffer
		err := executarMigracao(db, &out, executar)
		var naoMigravel *services.ErroSaldoNaoMigravel
		if !errors.As(err, &naoMigravel) {
			t.Fatalf("executar=%v: err = %v, want ErroSaldoNaoMigravel", executar, err)
		}
		if msg := mensagemDeErro(err); !strings.Contains(msg, "nada foi escrito") {
			t.Errorf("mensagem = %q", msg)
		}
		if !strings.Contains(out.String(), "Produto sem empresa_id") {
			t.Errorf("relatório sem o problema:\n%s", out.String())
		}
		if n := contar(t, db, `SELECT count(*) FROM lotes`); n != 0 {
			t.Errorf("executar=%v: lotes = %d, want 0", executar, n)
		}
	}
}

func TestExecutarMigracao_SemSaldo(t *testing.T) {
	db := testDB(t)
	semearSaldo(t, db)
	var out bytes.Buffer
	if err := executarMigracao(db, &out, true); err != nil {
		t.Fatalf("executar: %v", err)
	}
	if !strings.Contains(out.String(), "0 Lote(s) legado(s) criado(s)") {
		t.Errorf("saída inesperada:\n%s", out.String())
	}
}
