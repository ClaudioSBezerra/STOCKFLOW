package main

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/lib/pq"

	"stockflow/backend/services"
)

var (
	migrateOnce sync.Once
	migrateErr  error
)

// testDB abre uma conexão contra DATABASE_URL e aplica as migrations reais.
// Pula quando nenhum Postgres foi configurado. A suíte completa roda com
// `go test -p 1 ./...` (banco compartilhado entre pacotes).
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

const slugTeste = "estoques-filial-cli"

const truncarEstoques = `TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, produto_historico, mesclagens_duplicatas, carrinho_itens, pedido_itens, reservas_pedido_item, pedidos, produto_estoque, lotes, produtos, estoques, movimentacoes`

// semearEstoquesLegados cria uma Empresa própria (com a Filial padrão do
// provisionamento) e um Estoque legado (`filial_id` NULL) por nome. O banco é
// compartilhado e a migração é global (todas as Empresas), então o cleanup
// remove os Estoques, apaga as Filiais que a migração criou para Empresas
// alheias e restaura a nulabilidade de `estoques.filial_id` — os demais testes
// inserem Estoque sem Filial.
func semearEstoquesLegados(t *testing.T, db *sql.DB, nomes ...string) (empresaID string) {
	t.Helper()

	if _, err := db.Exec(`ALTER TABLE estoques ALTER COLUMN filial_id DROP NOT NULL`); err != nil {
		t.Fatalf("restaurar nulabilidade: %v", err)
	}
	if _, err := db.Exec(truncarEstoques); err != nil {
		t.Fatalf("limpar estoques: %v", err)
	}

	rows, err := db.Query(`SELECT id::text FROM empresas e WHERE NOT EXISTS (SELECT 1 FROM filiais f WHERE f.empresa_id = e.id)`)
	if err != nil {
		t.Fatalf("empresas sem filial: %v", err)
	}
	var semFilial []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		semFilial = append(semFilial, id)
	}
	rows.Close()

	t.Cleanup(func() {
		if _, err := db.Exec(truncarEstoques); err != nil {
			t.Errorf("cleanup estoques: %v", err)
		}
		for _, id := range semFilial {
			if _, err := db.Exec(`DELETE FROM filiais WHERE empresa_id = $1`, id); err != nil {
				t.Errorf("cleanup filiais de %s: %v", id, err)
			}
		}
		if _, err := db.Exec(`ALTER TABLE estoques ALTER COLUMN filial_id DROP NOT NULL`); err != nil {
			t.Errorf("cleanup: restaurar nulabilidade de estoques.filial_id: %v", err)
		}
		if empresaID == "" {
			return
		}
		for _, stmt := range []string{
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

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	e, err := services.ProvisionarEmpresa(tx, services.DadosEmpresa{
		NomeFantasia: "Estoques Filial CLI",
		RazaoSocial:  "Estoques Filial CLI LTDA",
		CNPJ:         "10500000005920",
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

	for _, nome := range nomes {
		if _, err := db.Exec(`INSERT INTO estoques (nome, empresa_id) VALUES ($1, $2)`, nome, empresaID); err != nil {
			t.Fatalf("estoque legado %q: %v", nome, err)
		}
	}
	return empresaID
}

func contar(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func filialNotNull(t *testing.T, db *sql.DB) bool {
	t.Helper()
	var v string
	if err := db.QueryRow(
		`SELECT is_nullable FROM information_schema.columns
		 WHERE table_schema = current_schema() AND table_name = 'estoques' AND column_name = 'filial_id'`,
	).Scan(&v); err != nil {
		t.Fatalf("is_nullable: %v", err)
	}
	return v == "NO"
}

func TestExecutarMigracao_DryRunNaoEscreve(t *testing.T) {
	db := testDB(t)
	empresaID := semearEstoquesLegados(t, db, "Central", "Obra")
	filiaisAntes := contar(t, db, `SELECT count(*) FROM filiais`)

	var out bytes.Buffer
	if err := executarMigracao(db, &out, false); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	s := out.String()
	for _, want := range []string{"dry-run", "estoques a vincular (filial_id nulo): 2", "estoques já vinculados: 0", "ainda é NULLABLE", "problemas: nenhum"} {
		if !strings.Contains(s, want) {
			t.Errorf("saída sem %q:\n%s", want, s)
		}
	}
	if n := contar(t, db, `SELECT count(*) FROM estoques WHERE empresa_id = $1 AND filial_id IS NULL`, empresaID); n != 2 {
		t.Errorf("estoques sem filial = %d, want 2", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM filiais`); n != filiaisAntes {
		t.Errorf("filiais = %d, want %d", n, filiaisAntes)
	}
	if filialNotNull(t, db) {
		t.Error("dry-run não deveria aplicar NOT NULL")
	}
}

func TestExecutarMigracao_ExecutarEReexecutar(t *testing.T) {
	db := testDB(t)
	empresaID := semearEstoquesLegados(t, db, "Central", "Obra", "Deposito")

	var out bytes.Buffer
	if err := executarMigracao(db, &out, true); err != nil {
		t.Fatalf("executar: %v", err)
	}
	if !strings.Contains(out.String(), "3 Estoque(s) vinculado(s)") {
		t.Errorf("saída inesperada:\n%s", out.String())
	}
	if n := contar(t, db, `SELECT count(*) FROM estoques e JOIN filiais f ON f.id = e.filial_id WHERE e.empresa_id = $1 AND f.empresa_id = $1`, empresaID); n != 3 {
		t.Errorf("estoques vinculados = %d, want 3", n)
	}
	if !filialNotNull(t, db) {
		t.Error("estoques.filial_id deveria ser NOT NULL")
	}

	out.Reset()
	if err := executarMigracao(db, &out, true); err != nil {
		t.Fatalf("reexecutar: %v", err)
	}
	for _, want := range []string{"0 Filial(is) padrão criada(s), 0 Estoque(s) vinculado(s)", "já é NOT NULL"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("reexecução sem %q:\n%s", want, out.String())
		}
	}
}

func TestExecutarMigracao_EstoqueSemEmpresaAborta(t *testing.T) {
	db := testDB(t)
	empresaID := semearEstoquesLegados(t, db, "Central")
	if _, err := db.Exec(`INSERT INTO estoques (nome) VALUES ('Orfao')`); err != nil {
		t.Fatal(err)
	}

	for _, executar := range []bool{false, true} {
		var out bytes.Buffer
		err := executarMigracao(db, &out, executar)
		var naoMigravel *services.ErroEstoqueNaoMigravel
		if !errors.As(err, &naoMigravel) {
			t.Fatalf("executar=%v: err = %v, want ErroEstoqueNaoMigravel", executar, err)
		}
		if msg := mensagemDeErro(err); !strings.Contains(msg, "nada foi escrito") {
			t.Errorf("mensagem = %q", msg)
		}
		if !strings.Contains(out.String(), "sem empresa_id") {
			t.Errorf("relatório sem o problema:\n%s", out.String())
		}
		if n := contar(t, db, `SELECT count(*) FROM estoques WHERE empresa_id = $1 AND filial_id IS NULL`, empresaID); n != 1 {
			t.Errorf("executar=%v: legado vinculado (sem filial = %d, want 1)", executar, n)
		}
		if filialNotNull(t, db) {
			t.Errorf("executar=%v: coluna deveria seguir NULLABLE", executar)
		}
	}
}

func TestExecutarMigracao_SemEstoqueLegado(t *testing.T) {
	db := testDB(t)
	semearEstoquesLegados(t, db)
	var out bytes.Buffer
	if err := executarMigracao(db, &out, true); err != nil {
		t.Fatalf("executar: %v", err)
	}
	if !strings.Contains(out.String(), "0 Estoque(s) vinculado(s)") {
		t.Errorf("saída inesperada:\n%s", out.String())
	}
	if !filialNotNull(t, db) {
		t.Error("NOT NULL deveria ter sido aplicado")
	}
}

// descricaoBanco ecoa host:porta/nome sem usuário nem senha — o operador vê QUAL
// banco será alterado por um `--executar` irreversível.
func TestDescricaoBanco_NaoVazaCredenciais(t *testing.T) {
	got := descricaoBanco("postgres://stockflow:segredo@db.interno:5432/stockflow?sslmode=disable")
	if got != "db.interno:5432/stockflow" {
		t.Errorf("descricaoBanco = %q, want db.interno:5432/stockflow", got)
	}
	if strings.Contains(got, "segredo") || strings.Contains(got, "stockflow:") {
		t.Errorf("vazou credencial: %q", got)
	}
	if got := descricaoBanco("://quebrada"); !strings.Contains(got, "não pôde ser interpretada") {
		t.Errorf("URL ilegível = %q", got)
	}
}

// lock_timeout (SQLSTATE 55P03) chega ao operador em português, não como o erro
// cru do Postgres.
func TestMensagemDeErro_LockTimeoutETraducao(t *testing.T) {
	err := fmt.Errorf("falha ao travar estoques para a migração: %w", &pq.Error{Code: "55P03", Message: "canceling statement due to lock timeout"})
	got := mensagemDeErro(err)
	if !strings.Contains(got, "não foi possível travar a tabela estoques") || !strings.Contains(got, "nada foi escrito") {
		t.Errorf("mensagem = %q", got)
	}
	if got := mensagemDeErro(&services.ErroFilialPadraoSemNome{Empresas: 2}); !strings.Contains(got, "2 empresa(s)") {
		t.Errorf("mensagem ErroFilialPadraoSemNome = %q", got)
	}
}
