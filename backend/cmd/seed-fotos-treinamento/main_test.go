package main

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
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

const slugTeste = "fotos-treino-cli"

// cnpjValido completa 12 dígitos com os dois verificadores.
func cnpjValido(base12 string) string {
	dv := func(s string) string {
		soma, peso := 0, len(s)-7
		for _, c := range s {
			soma += int(c-'0') * peso
			if peso--; peso < 2 {
				peso = 9
			}
		}
		if r := soma % 11; r >= 2 {
			return strconv.Itoa(11 - r)
		}
		return "0"
	}
	s := base12 + dv(base12)
	return s + dv(s)
}

func removerPar(t *testing.T, db *sql.DB, slug string) {
	t.Helper()
	for _, s := range []string{slug + "-treinamento", slug} {
		var id string
		err := db.QueryRow(`SELECT id FROM empresas WHERE slug = $1`, s).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			t.Fatalf("buscar empresa %s: %v", s, err)
		}
		for _, stmt := range []string{
			`DELETE FROM produto_estoque WHERE produto_id IN (SELECT id FROM produtos WHERE empresa_id = $1)`,
			`DELETE FROM produtos WHERE empresa_id = $1`,
			`DELETE FROM estoques WHERE empresa_id = $1`,
			`DELETE FROM logs_acesso WHERE empresa_id = $1`,
			`DELETE FROM usuarios WHERE empresa_id = $1`,
			`DELETE FROM categorias WHERE empresa_id = $1`,
			`DELETE FROM nomenclatura_templates WHERE empresa_id = $1`,
			`DELETE FROM filiais WHERE empresa_id = $1`,
			`DELETE FROM contadores_produto WHERE empresa_id = $1`,
			`DELETE FROM empresas WHERE id = $1`,
		} {
			if _, err := db.Exec(stmt, id); err != nil {
				t.Fatalf("limpar %s [%s]: %v", s, stmt, err)
			}
		}
	}
}

func criarPar(t *testing.T, db *sql.DB) {
	t.Helper()
	removerPar(t, db, slugTeste)
	t.Cleanup(func() { removerPar(t, db, slugTeste) })
	_, _, err := services.CriarEmpresaComTreinamento(db, services.EmailConfig{
		Host: "smtp.invalid", Port: "587", From: "stockflow <noreply@stockflow.local>", AppURL: "http://test.local",
	}, services.NovaEmpresaInput{
		DadosEmpresa: services.DadosEmpresa{
			NomeFantasia: "Fotos Treino CLI",
			RazaoSocial:  "Fotos Treino CLI LTDA",
			CNPJ:         cnpjValido("981234560001"),
			Slug:         slugTeste,
			Endereco: services.EnderecoEmpresa{
				Logradouro: "Rua de Teste", Numero: "1", Bairro: "Centro", Cidade: "Recife", CEP: "50000000", UF: "PE",
			},
		},
		AdmNome:  "Ana Adm",
		AdmEmail: "ana.fotos.cli@cliente.com",
	})
	if err != nil {
		t.Fatalf("CriarEmpresaComTreinamento: %v", err)
	}
}

func arquivos(t *testing.T, dir string) int {
	t.Helper()
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	return len(entradas)
}

func TestExecutarSemeadura_DryRunExecutarEReexecutar(t *testing.T) {
	db := testDB(t)
	criarPar(t, db)
	dir := t.TempDir()
	slug := slugTeste + "-treinamento"

	var out bytes.Buffer
	if err := executarSemeadura(db, &out, dir, slug, false); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "dry-run") || !strings.Contains(out.String(), "a semear: 5") {
		t.Errorf("saída do dry-run = %q", out.String())
	}
	if n := arquivos(t, dir); n != 0 {
		t.Fatalf("dry-run gravou %d arquivo(s)", n)
	}

	out.Reset()
	if err := executarSemeadura(db, &out, dir, slug, true); err != nil {
		t.Fatalf("--executar: %v", err)
	}
	if !strings.Contains(out.String(), "semeadas: 5") {
		t.Errorf("saída de --executar = %q", out.String())
	}
	if n := arquivos(t, dir); n != 5 {
		t.Fatalf("arquivos = %d, want 5", n)
	}

	out.Reset()
	if err := executarSemeadura(db, &out, dir, slug, true); err != nil {
		t.Fatalf("reexecução: %v", err)
	}
	if !strings.Contains(out.String(), "semeadas: 0") || !strings.Contains(out.String(), "já com foto: 5") {
		t.Errorf("saída da reexecução = %q", out.String())
	}
	if n := arquivos(t, dir); n != 5 {
		t.Errorf("reexecução alterou os arquivos: %d, want 5", n)
	}
}

func TestExecutarSemeadura_RecusaEmpresaRealEInexistente(t *testing.T) {
	db := testDB(t)
	criarPar(t, db)
	dir := filepath.Join(t.TempDir(), "fotos")

	err := executarSemeadura(db, &bytes.Buffer{}, dir, slugTeste, true)
	if !errors.Is(err, services.ErrEmpresaNaoTreinamento) {
		t.Fatalf("Empresa real: erro = %v, want ErrEmpresaNaoTreinamento", err)
	}
	if !strings.Contains(mensagemDeErro(err), "nada foi escrito") {
		t.Errorf("mensagem = %q", mensagemDeErro(err))
	}

	err = executarSemeadura(db, &bytes.Buffer{}, dir, "fotos-cli-nao-existe", true)
	if !errors.Is(err, services.ErrEmpresaNaoEncontrada) {
		t.Fatalf("slug inexistente: erro = %v, want ErrEmpresaNaoEncontrada", err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("diretório de fotos criado numa recusa (err=%v)", statErr)
	}
}

func TestValidarArgumentos(t *testing.T) {
	if err := validarArgumentos("", nil); err == nil || !strings.Contains(err.Error(), "--empresa") {
		t.Errorf("sem --empresa: erro = %v", err)
	}
	if err := validarArgumentos("   ", nil); err == nil {
		t.Error("--empresa em branco deveria ser recusado")
	}
	if err := validarArgumentos("x-treinamento", []string{"executar"}); err == nil {
		t.Error("argumento posicional deveria ser recusado")
	}
	if err := validarArgumentos("x-treinamento", nil); err != nil {
		t.Errorf("entrada válida: erro = %v", err)
	}
}

func TestResolverFotosDir(t *testing.T) {
	cases := []struct{ flag, env, want string }{
		{"/a", "/b", "/a"},
		{"", "/b", "/b"},
		{"", "", "./fotos"},
	}
	for _, c := range cases {
		if got := resolverFotosDir(c.flag, c.env); got != c.want {
			t.Errorf("resolverFotosDir(%q, %q) = %q, want %q", c.flag, c.env, got, c.want)
		}
	}
}
