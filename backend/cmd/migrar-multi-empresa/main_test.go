package main

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
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

// opcoesDeTeste devolve um conjunto de flags válido para a etapa `empresa`.
func opcoesDeTeste(slug string) opcoes {
	return opcoes{
		etapa:        etapaEmpresa,
		executar:     true,
		nomeFantasia: "Fundadora CLI",
		razaoSocial:  "Fundadora CLI LTDA",
		cnpj:         "94740100000106",
		logradouro:   "Av. Principal",
		numero:       "1000",
		bairro:       "Centro",
		cidade:       "Recife",
		cep:          "50000-000",
		uf:           "PE",
		slug:         slug,
		admNome:      "Claudio",
		admEmail:     "claudio@fundadora-cli.test",
		lote:         1000,
	}
}

// limparEmpresa apaga a Empresa e o Treinamento criados por um teste.
func limparEmpresa(t *testing.T, db *sql.DB, slug string) {
	t.Helper()
	rows, err := db.Query(
		`SELECT id FROM empresas WHERE slug = $1 OR slug = $1 || '-treinamento'`, slug)
	if err != nil {
		t.Fatalf("limparEmpresa(%s): %v", slug, err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatalf("limparEmpresa(%s): %v", slug, err)
		}
		ids = append(ids, id)
	}
	rows.Close()

	for _, id := range ids {
		for _, stmt := range []string{
			`DELETE FROM produto_estoque WHERE produto_id IN (SELECT id FROM produtos WHERE empresa_id = $1)`,
			`DELETE FROM produtos WHERE empresa_id = $1`,
			`DELETE FROM estoques WHERE empresa_id = $1`,
			`DELETE FROM usuarios WHERE empresa_id = $1`,
			`DELETE FROM categorias WHERE empresa_id = $1`,
			`DELETE FROM nomenclatura_templates WHERE empresa_id = $1`,
		} {
			if _, err := db.Exec(stmt, id); err != nil {
				t.Fatalf("limparEmpresa(%s) [%s]: %v", slug, stmt, err)
			}
		}
	}
	if _, err := db.Exec(`DELETE FROM empresas WHERE empresa_origem_id IN (SELECT id FROM empresas WHERE slug = $1)`, slug); err != nil {
		t.Fatalf("limparEmpresa(%s): treinamento: %v", slug, err)
	}
	if _, err := db.Exec(`DELETE FROM empresas WHERE slug = $1`, slug); err != nil {
		t.Fatalf("limparEmpresa(%s): empresa: %v", slug, err)
	}
}

func TestValidarOpcoes(t *testing.T) {
	base := opcoesDeTeste("fundadora-cli")

	semCNPJ := base
	semCNPJ.cnpj = "  "

	backfillSemAlvo := base
	backfillSemAlvo.etapa = etapaBackfill
	backfillSemAlvo.slug = ""
	backfillSemAlvo.nomeFantasia = ""

	dryRunPelado := opcoes{etapa: etapaTudo, lote: 1000}

	etapaInvalida := base
	etapaInvalida.etapa = "quase"

	loteZero := base
	loteZero.lote = 0

	casos := []struct {
		nome    string
		o       opcoes
		wantErr bool
	}{
		{"etapa empresa completa", base, false},
		{"dry-run pelado não exige cadastro", dryRunPelado, false},
		{"etapa inválida", etapaInvalida, true},
		{"lote zero", loteZero, true},
		{"etapa empresa sem CNPJ", semCNPJ, true},
		{"backfill sem slug nem nome", backfillSemAlvo, true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if err := validarOpcoes(c.o); (err != nil) != c.wantErr {
				t.Errorf("validarOpcoes() err = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

// TestExecutarMigracao_DryRunNaoEscreve: o diagnóstico não altera o banco.
func TestExecutarMigracao_DryRunNaoEscreve(t *testing.T) {
	db := testDB(t)

	empresasAntes := contar(t, db, `SELECT count(*) FROM empresas`)
	usuariosAntes := contar(t, db, `SELECT count(*) FROM usuarios`)

	o := opcoesDeTeste("fundadora-cli-dry")
	o.executar = false
	o.etapa = etapaTudo

	var out bytes.Buffer
	if err := executarMigracao(db, &out, services.EmailConfig{AppURL: "http://test.local"}, o); err != nil {
		t.Fatalf("executarMigracao (dry-run): %v", err)
	}
	saida := out.String()
	for _, esperado := range []string{"dry-run (nada será escrito)", "linhas sem Empresa", "colunas empresa_id já NOT NULL"} {
		if !strings.Contains(saida, esperado) {
			t.Errorf("saída sem %q:\n%s", esperado, saida)
		}
	}
	if n := contar(t, db, `SELECT count(*) FROM empresas`); n != empresasAntes {
		t.Errorf("empresas = %d, want %d — dry-run escreveu", n, empresasAntes)
	}
	if n := contar(t, db, `SELECT count(*) FROM usuarios`); n != usuariosAntes {
		t.Errorf("usuarios = %d, want %d — dry-run escreveu", n, usuariosAntes)
	}
}

// TestExecutarMigracao_EtapaEmpresaCriaPar cobre a etapa `empresa` e a sua
// idempotência pela superfície do CLI.
func TestExecutarMigracao_EtapaEmpresaCriaPar(t *testing.T) {
	db := testDB(t)
	const slug = "fundadora-cli"
	t.Cleanup(func() { limparEmpresa(t, db, slug) })

	o := opcoesDeTeste(slug)
	var out bytes.Buffer
	if err := executarMigracao(db, &out, services.EmailConfig{AppURL: "http://test.local"}, o); err != nil {
		t.Fatalf("etapa empresa: %v", err)
	}
	if !strings.Contains(out.String(), "etapa empresa: criada.") {
		t.Errorf("saída sem a linha de criação:\n%s", out.String())
	}
	if n := contar(t, db, `SELECT count(*) FROM empresas WHERE slug = $1 OR slug = $1 || '-treinamento'`, slug); n != 2 {
		t.Fatalf("empresas criadas = %d, want 2 (real + treinamento)", n)
	}

	var out2 bytes.Buffer
	if err := executarMigracao(db, &out2, services.EmailConfig{AppURL: "http://test.local"}, o); err != nil {
		t.Fatalf("etapa empresa (2a vez): %v", err)
	}
	if !strings.Contains(out2.String(), "já existente") {
		t.Errorf("2a execução não reportou idempotência:\n%s", out2.String())
	}
	if n := contar(t, db, `SELECT count(*) FROM empresas WHERE slug = $1 OR slug = $1 || '-treinamento'`, slug); n != 2 {
		t.Errorf("empresas = %d, want 2 — a 2a execução recriou algo", n)
	}
}

// TestExecutarMigracao_EndurecimentoRecusaComOrfas: a etapa `endurecimento`
// sai com erro (o CLI traduz para exit 1) e não emite DDL.
func TestExecutarMigracao_EndurecimentoRecusaComOrfas(t *testing.T) {
	db := testDB(t)

	var orfao string
	if err := db.QueryRow(
		`INSERT INTO estoques (nome) VALUES ('Orfao CLI 94') RETURNING id`,
	).Scan(&orfao); err != nil {
		t.Fatalf("criar estoque órfão: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM estoques WHERE id = $1`, orfao) })

	o := opcoesDeTeste("fundadora-cli-endur")
	o.etapa = etapaEndurecimento

	var out bytes.Buffer
	err := executarMigracao(db, &out, services.EmailConfig{}, o)
	if !errors.Is(err, services.ErrOrfasPendentes) {
		t.Fatalf("erro = %v, want ErrOrfasPendentes", err)
	}
	if msg := mensagemDeErro(err); !strings.Contains(msg, "estoques") {
		t.Errorf("mensagem = %q, quer nomear a tabela pendente", msg)
	}
}

// TestMigracaoNuncaEmPipeline é a AC 4 como invariante verificável: nenhum
// workflow, compose ou instalador invoca o binário da migração. O Dockerfile
// pode citá-lo (build/copy), mas nunca no ENTRYPOINT.
func TestMigracaoNuncaEmPipeline(t *testing.T) {
	const binario = "migrar-multi-empresa"
	raiz := filepath.Join("..", "..", "..")

	var alvos []string
	for _, padrao := range []string{
		filepath.Join(raiz, ".github", "workflows", "*.yml"),
		filepath.Join(raiz, ".github", "workflows", "*.yaml"),
		filepath.Join(raiz, "docker-compose.yml"),
		filepath.Join(raiz, "installer", "*", "*.yml"),
	} {
		encontrados, err := filepath.Glob(padrao)
		if err != nil {
			t.Fatalf("glob %s: %v", padrao, err)
		}
		alvos = append(alvos, encontrados...)
	}
	if len(alvos) == 0 {
		t.Fatal("nenhum arquivo de pipeline encontrado — o teste perderia o sentido")
	}

	for _, caminho := range alvos {
		conteudo, err := os.ReadFile(caminho)
		if err != nil {
			t.Fatalf("ler %s: %v", caminho, err)
		}
		if strings.Contains(string(conteudo), binario) {
			t.Errorf("%s cita %q — a migração é disparada À MÃO por uma pessoa (AD-15, PRD §9)", caminho, binario)
		}
	}

	dockerfile, err := os.ReadFile(filepath.Join(raiz, "backend", "Dockerfile"))
	if err != nil {
		t.Fatalf("ler Dockerfile: %v", err)
	}
	for _, linha := range strings.Split(string(dockerfile), "\n") {
		if strings.HasPrefix(strings.TrimSpace(linha), "ENTRYPOINT") && strings.Contains(linha, binario) {
			t.Errorf("ENTRYPOINT invoca a migração: %q", linha)
		}
	}
}

func contar(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("contar (%s): %v", query, err)
	}
	return n
}
