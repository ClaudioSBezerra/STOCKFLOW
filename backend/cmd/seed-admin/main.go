// Command seed-admin bootstraps the very first `adm` account for stockflow.
//
// This is deliberately a standalone CLI binary — never an HTTP handler nor a
// route registered on the API server (AD-12: a self-promotion HTTP endpoint
// is a privilege-escalation vector). It refuses to run again once an `adm`
// account already exists, and never touches an existing account when it does.
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

// pqUniqueViolation is the Postgres SQLSTATE for a unique-constraint
// violation (23505) — used to recognize a lost race against the DB-level
// partial unique index idx_usuarios_unico_adm (see migration 000001).
const pqUniqueViolation = "23505"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	nome := flag.String("nome", "", "Nome completo do primeiro administrador")
	email := flag.String("email", "", "E-mail do primeiro administrador")
	senha := flag.String("senha", "", "Senha do primeiro administrador")
	// --empresa-slug é OPCIONAL (Story 9.1, spec-9-1): informado, resolve a
	// Empresa daquele slug e cria o `adm` DENTRO dela; ausente, o
	// comportamento é EXATAMENTE o de antes desta story (`empresa_id` NULL,
	// unicidade global de `adm` preservada pelo NULLS NOT DISTINCT da
	// migração 000032). O deploy em CI
	// (.github/workflows/deploy-cliente-aws.yml) passa só
	// --nome/--email/--senha e não pode quebrar.
	empresaSlug := flag.String("empresa-slug", "", "Slug da Empresa em que criar o administrador (opcional; sem ele a conta nasce sem Empresa)")
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

	id, err := seedAdmin(db, strings.TrimSpace(*empresaSlug), *nome, *email, *senha)
	if err != nil {
		if errors.Is(err, errAdminAlreadyExists) {
			fmt.Fprintln(os.Stderr, "erro: já existe uma conta com papel 'adm' — seed-admin não altera contas existentes")
		} else if errors.Is(err, errEmpresaNaoEncontrada) {
			fmt.Fprintf(os.Stderr, "erro: nenhuma empresa ativa com o slug %q\n", strings.TrimSpace(*empresaSlug))
		} else {
			fmt.Fprintf(os.Stderr, "erro: falha ao criar conta administradora: %v\n", err)
		}
		os.Exit(1)
	}

	fmt.Printf("conta adm criada com sucesso: id=%s email=%s\n", id, normalizeEmail(*email))
}

var errAdminAlreadyExists = errors.New("já existe uma conta com papel adm")

// errEmpresaNaoEncontrada indica que o `--empresa-slug` informado não
// corresponde a nenhuma Empresa `ativa` — nenhuma conta é criada (Story 9.1).
var errEmpresaNaoEncontrada = errors.New("empresa não encontrada")

// validateFlags checa que --nome, --email e --senha foram informados com
// conteúdo não vazio após remover espaços nas bordas — evita, por exemplo,
// que uma senha apenas com espaços (" ") passe pela checagem de string vazia
// e seja hasheada como a senha real do primeiro admin.
func validateFlags(nome, email, senha string) error {
	if strings.TrimSpace(nome) == "" || strings.TrimSpace(email) == "" || strings.TrimSpace(senha) == "" {
		return errors.New("--nome, --email e --senha são obrigatórios")
	}
	return nil
}

// normalizeEmail aplica a mesma normalização exigida na gravação: minúsculas
// e sem espaços nas bordas. A unicidade real é garantida pelo índice único
// funcional sobre lower(email) (AD-14); esta função só evita gravar variações
// de caixa óbvias antes mesmo de chegar ao banco.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// seedAdmin cria a primeira conta `adm`. A checagem sequencial "nenhum adm
// existe" e o INSERT acontecem em uma única instrução (INSERT ... SELECT ...
// WHERE NOT EXISTS), o que cobre o caso comum sem round-trip extra. Isso por
// si só NÃO é suficiente sob READ COMMITTED: duas execuções concorrentes de
// seed-admin podem avaliar NOT EXISTS como verdadeiro antes de qualquer uma
// commitar. A garantia real vem do índice único parcial idx_usuarios_unico_adm
// (papel) WHERE papel='adm' (migration 000001) — se a corrida acontecer, o
// INSERT perdedor falha com violação de unicidade (SQLSTATE 23505), tratada
// abaixo como errAdminAlreadyExists. Em qualquer um dos dois caminhos, uma
// conta adm já existente nunca é alterada.
func seedAdmin(db *sql.DB, empresaSlug, nome, email, senha string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("falha ao gerar hash da senha: %w", err)
	}

	normalizedEmail := normalizeEmail(email)

	// Sem `--empresa-slug`: comportamento idêntico ao de antes da Story 9.1 —
	// `empresa_id` NULL e "nenhum adm existe" avaliado sobre as OUTRAS linhas
	// sem Empresa (`empresa_id IS NULL`), que é exatamente o conjunto de
	// contas legadas. Com o slug: a Empresa é resolvida primeiro e tanto o
	// NOT EXISTS quanto o INSERT ficam recortados a ela — dois `adm` em
	// Empresas distintas são aceitos, um segundo `adm` na MESMA Empresa é
	// recusado (pelo NOT EXISTS ou, sob corrida, pelo índice único
	// `idx_usuarios_unico_adm`, agora `(empresa_id, papel)`).
	var empresaID sql.NullString
	if empresaSlug != "" {
		var id string
		err := db.QueryRow(
			`SELECT id FROM empresas WHERE slug = $1 AND status = 'ativa'`, empresaSlug,
		).Scan(&id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", errEmpresaNaoEncontrada
			}
			return "", fmt.Errorf("falha ao resolver empresa por slug: %w", err)
		}
		empresaID = sql.NullString{String: id, Valid: true}
	}

	// `$4 IS NOT DISTINCT FROM empresa_id` casa NULL com NULL (sem slug) e
	// id com id (com slug) numa única forma — a mesma semântica do
	// `NULLS NOT DISTINCT` do índice único que serve de backstop.
	const query = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, empresa_id)
		SELECT $1, $2, $3, 'adm', true, true, $4
		WHERE NOT EXISTS (
			SELECT 1 FROM usuarios
			WHERE papel = 'adm' AND $4 IS NOT DISTINCT FROM empresa_id
		)
		RETURNING id`

	var id string
	err = db.QueryRow(query, strings.TrimSpace(nome), normalizedEmail, string(hash), empresaID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errAdminAlreadyExists
		}
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation && pqErr.Constraint == "idx_usuarios_unico_adm" {
			return "", errAdminAlreadyExists
		}
		return "", err
	}
	return id, nil
}
