// Command seed-dono-plataforma cria o PRIMEIRO Dono da Plataforma do
// stockflow — Story 9.2 (Epic 9, AD-21/AD-12), spec-9-2.
//
// É deliberadamente um binário de CLI, nunca uma rota HTTP (AD-12: nenhuma
// rota cria linha em `donos_plataforma`). Recusa rodar de novo quando um Dono
// já existe e nunca altera uma conta existente.
//
// O Dono nasce com a MFA já ativa: o segredo TOTP e a URL `otpauth://` são
// impressos UMA única vez, para o operador cadastrá-los no aplicativo
// autenticador naquele momento. Por isso este CLI NÃO roda no pipeline de
// deploy — o segredo precisa ser capturado por uma pessoa. O operador o
// executa dentro do container `api`:
//
//	./seed-dono-plataforma --nome "Nome" --email dono@exemplo.com --senha 'Senha-forte-1'
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"stockflow/backend/services"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	nome := flag.String("nome", "", "Nome do Dono da Plataforma")
	email := flag.String("email", "", "E-mail do Dono da Plataforma")
	senha := flag.String("senha", "", "Senha do Dono da Plataforma (8+ caracteres, com letra e número)")
	flag.Parse()

	if err := validateFlags(*nome, *email, *senha); err != nil {
		fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		flag.Usage()
		os.Exit(1)
	}

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("falha ao carregar .env", "error", err)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "erro: DATABASE_URL não definido")
		os.Exit(1)
	}

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: falha ao abrir conexão com o banco: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "erro: banco indisponível: %v\n", err)
		os.Exit(1)
	}

	if err := seedDono(db, os.Stdout, *nome, *email, *senha); err != nil {
		fmt.Fprintf(os.Stderr, "erro: %s\n", mensagemDeErro(err))
		os.Exit(1)
	}
}

// validateFlags exige --nome, --email e --senha com conteúdo além de espaços.
func validateFlags(nome, email, senha string) error {
	if strings.TrimSpace(nome) == "" || strings.TrimSpace(email) == "" || strings.TrimSpace(senha) == "" {
		return errors.New("--nome, --email e --senha são obrigatórios")
	}
	return nil
}

// seedDono cria o primeiro Dono (services.CriarPrimeiroDonoPlataforma — a
// mesma regra do service, nunca uma cópia) e imprime em `out` o id, o e-mail,
// o segredo TOTP e a URL `otpauth://`.
func seedDono(db *sql.DB, out io.Writer, nome, email, senha string) error {
	id, segredo, err := services.CriarPrimeiroDonoPlataforma(db, nome, email, senha)
	if err != nil {
		return err
	}
	emailNormalizado := strings.ToLower(strings.TrimSpace(email))
	fmt.Fprintf(out, "Dono da Plataforma criado com sucesso: id=%s email=%s\n\n", id, emailNormalizado)
	fmt.Fprintln(out, "A autenticação em duas etapas já está ATIVA. Cadastre AGORA no seu aplicativo autenticador:")
	fmt.Fprintf(out, "  segredo TOTP: %s\n", segredo)
	fmt.Fprintf(out, "  URL otpauth:  %s\n\n", services.URLProvisionamentoTOTP(emailNormalizado, segredo))
	fmt.Fprintln(out, "Estes dados NÃO serão exibidos de novo. Sem eles não é possível entrar em /plataforma.")
	return nil
}

// mensagemDeErro traduz os erros conhecidos do bootstrap para o operador.
func mensagemDeErro(err error) string {
	switch {
	case errors.Is(err, services.ErrDonoJaExiste):
		return "já existe um Dono da Plataforma — seed-dono-plataforma não altera contas existentes"
	case errors.Is(err, services.ErrSenhaFraca):
		return "a senha deve ter ao menos 8 caracteres, incluindo uma letra e um número"
	case errors.Is(err, services.ErrDonoValidacao):
		return "nome e e-mail são obrigatórios (e-mail com @, até 255 caracteres)"
	default:
		return fmt.Sprintf("falha ao criar o Dono da Plataforma: %v", err)
	}
}
