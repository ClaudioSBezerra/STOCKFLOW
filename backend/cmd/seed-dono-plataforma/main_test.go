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
	"golang.org/x/crypto/bcrypt"

	"stockflow/backend/services"
)

var (
	migrateOnce sync.Once
	migrateErr  error
)

// testDB abre uma conexão contra DATABASE_URL, aplica as migrations reais e
// limpa `donos_plataforma` (DELETE, com cascata para `sessoes_plataforma`)
// antes e depois de cada teste. Pula quando nenhum Postgres foi configurado.
// A suíte completa roda com `go test -p 1 ./...` (banco compartilhado).
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

	if _, err := db.Exec(`DELETE FROM donos_plataforma`); err != nil {
		t.Fatalf("falha ao limpar donos_plataforma: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM donos_plataforma`) })
	return db
}

// valorDaLinha extrai de `saida` o valor impresso depois de `rotulo`.
func valorDaLinha(t *testing.T, saida, rotulo string) string {
	t.Helper()
	for _, linha := range strings.Split(saida, "\n") {
		if i := strings.Index(linha, rotulo); i >= 0 {
			return strings.TrimSpace(linha[i+len(rotulo):])
		}
	}
	t.Fatalf("saída sem %q:\n%s", rotulo, saida)
	return ""
}

// TestSeedDono_PrimeiraExecucaoCria prova a AC 1: a primeira execução cria o
// Dono com MFA ativa e imprime o segredo e a URL otpauth:// uma vez.
func TestSeedDono_PrimeiraExecucaoCria(t *testing.T) {
	db := testDB(t)

	var out bytes.Buffer
	if err := seedDono(db, &out, "  Dona Inicial ", "Dona@Plataforma.COM", "senha-forte-1"); err != nil {
		t.Fatalf("seedDono: %v", err)
	}
	saida := out.String()
	segredo := valorDaLinha(t, saida, "segredo TOTP:")
	url := valorDaLinha(t, saida, "URL otpauth:")
	if !strings.HasPrefix(url, "otpauth://totp/") || !strings.Contains(url, "secret="+segredo) {
		t.Errorf("URL otpauth = %q", url)
	}
	if !strings.Contains(saida, "NÃO serão exibidos de novo") {
		t.Error("saída sem o aviso de que o segredo não será exibido de novo")
	}

	var email, senhaHash, segredoGravado string
	var mfa bool
	if err := db.QueryRow(`SELECT email, senha_hash, mfa_habilitado, mfa_secret FROM donos_plataforma`).
		Scan(&email, &senhaHash, &mfa, &segredoGravado); err != nil {
		t.Fatalf("ler dono: %v", err)
	}
	if email != "dona@plataforma.com" || !mfa || segredoGravado != segredo {
		t.Errorf("dono gravado: email=%q mfa=%v segredo confere=%v", email, mfa, segredoGravado == segredo)
	}
	if bcrypt.CompareHashAndPassword([]byte(senhaHash), []byte("senha-forte-1")) != nil {
		t.Error("senha_hash não confere via bcrypt")
	}
	if !strings.Contains(saida, "email=dona@plataforma.com") {
		t.Errorf("saída sem o e-mail normalizado:\n%s", saida)
	}
}

func TestSeedDono_SegundaExecucaoRecusaSemAlterar(t *testing.T) {
	db := testDB(t)

	if err := seedDono(db, &bytes.Buffer{}, "Primeira", "primeira@plataforma.com", "senha-forte-1"); err != nil {
		t.Fatalf("primeira execução: %v", err)
	}
	var out bytes.Buffer
	err := seedDono(db, &out, "Segunda", "segunda@plataforma.com", "senha-forte-2")
	if !errors.Is(err, services.ErrDonoJaExiste) {
		t.Fatalf("erro = %v, want ErrDonoJaExiste", err)
	}
	if !strings.Contains(mensagemDeErro(err), "já existe um Dono da Plataforma") {
		t.Errorf("mensagem = %q", mensagemDeErro(err))
	}
	if out.Len() != 0 {
		t.Errorf("a execução recusada imprimiu na saída: %q", out.String())
	}
	var total int
	var email string
	if err := db.QueryRow(`SELECT count(*), min(email) FROM donos_plataforma`).Scan(&total, &email); err != nil {
		t.Fatalf("contar donos: %v", err)
	}
	if total != 1 || email != "primeira@plataforma.com" {
		t.Errorf("donos = %d (%s), want só a primeira, intacta", total, email)
	}
}

func TestSeedDono_SenhaFracaNaoGrava(t *testing.T) {
	db := testDB(t)

	err := seedDono(db, &bytes.Buffer{}, "Dona", "dona@plataforma.com", "abc")
	if !errors.Is(err, services.ErrSenhaFraca) {
		t.Fatalf("erro = %v, want ErrSenhaFraca", err)
	}
	if !strings.Contains(mensagemDeErro(err), "8 caracteres") {
		t.Errorf("mensagem = %q", mensagemDeErro(err))
	}
	var total int
	if err := db.QueryRow(`SELECT count(*) FROM donos_plataforma`).Scan(&total); err != nil {
		t.Fatalf("contar donos: %v", err)
	}
	if total != 0 {
		t.Errorf("donos = %d, want 0", total)
	}
}

func TestValidateFlags(t *testing.T) {
	casos := []struct {
		nome, n, e, s string
		wantErr       bool
	}{
		{"tudo preenchido", "Nome", "e@x.com", "senha-123", false},
		{"nome vazio", "", "e@x.com", "senha-123", true},
		{"e-mail só espaços", "Nome", "   ", "senha-123", true},
		{"senha vazia", "Nome", "e@x.com", "", true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if err := validateFlags(c.n, c.e, c.s); (err != nil) != c.wantErr {
				t.Errorf("validateFlags(%q,%q,%q) err = %v, wantErr %v", c.n, c.e, c.s, err, c.wantErr)
			}
		})
	}
}
