package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
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

// testDB abre uma conexão contra DATABASE_URL, aplica as migrations reais do
// projeto (via golang-migrate, source file://../../migrations — o mesmo
// diretório embutido em main.go) e limpa a tabela usuarios antes de cada
// teste, para que os cenários da I/O Matrix fiquem isolados entre si. Pula o
// teste quando nenhum Postgres foi configurado: suba um com
// `docker compose up -d db` (ou um Postgres local) e exporte DATABASE_URL.
//
// Este pacote e o pacote backend (main_test.go) compartilham a mesma tabela
// usuarios ao vivo no DATABASE_URL informado (cada um faz
// TRUNCATE/INSERT/SELECT nela). `go test ./...` roda pacotes diferentes como
// processos concorrentes por padrão, o que faria um pacote truncar linhas que
// o outro acabou de inserir e ainda vai verificar — por isso a suíte completa
// deve rodar com `go test -p 1 ./...` (serializa os pacotes; dentro de cada
// pacote os testes já rodam sequenciais por padrão).
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
		// go test ./... roda pacotes em paralelo por padrão: este pacote e o
		// pacote backend (main_test.go) podem migrar a mesma base "do zero" ao
		// mesmo tempo. golang-migrate só serializa via advisory lock depois que
		// a tabela schema_migrations existe — a primeira criação concorrente
		// dela pode colidir (efeito colateral conhecido de "CREATE TABLE IF NOT
		// EXISTS" concorrente no Postgres). Retry curto absorve essa corrida
		// sem exigir `go test -p 1`.
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

	// CASCADE: desde a migration 000002 (Story 1.3), tokens_acao e
	// emails_pendentes referenciam usuarios(id) via FK — sem CASCADE, um
	// TRUNCATE isolado de usuarios falharia mesmo com as tabelas dependentes
	// vazias.
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("falha ao limpar tabela usuarios entre testes: %v", err)
	}

	// `empresas` NUNCA entra neste TRUNCATE (Story 9.1): ela é referenciada
	// por categorias/nomenclatura_templates e por toda tabela de domínio, e
	// um TRUNCATE ... CASCADE nela levaria embora o seed das migrations
	// 000010/000013 — que nenhuma migration recriaria, quebrando as outras
	// suítes que compartilham este banco. A Empresa desta suíte é criada uma
	// vez e reaproveitada (garantirEmpresaSeed).

	return db
}

// TestSeedAdmin_Inicial prova o AC2: banco migrado, nenhum adm cadastrado,
// seed-admin cria a conta com papel adm, senha hasheada (bcrypt), e-mail
// normalizado, email_verificado=true e ativo=true.
func TestSeedAdmin_Inicial(t *testing.T) {
	db := testDB(t)
	garantirEmpresaSeed(t, db)

	id, err := seedAdmin(db, slugEmpresaSeed, "  Primeira Adm  ", "Admin@Empresa.COM", "senha-super-secreta")
	if err != nil {
		t.Fatalf("seedAdmin retornou erro inesperado: %v", err)
	}
	if id == "" {
		t.Fatal("id retornado vazio")
	}

	var nome, email, senhaHash, papel string
	var emailVerificado, ativo bool
	err = db.QueryRow(`SELECT nome, email, senha_hash, papel, email_verificado, ativo FROM usuarios WHERE id = $1`, id).
		Scan(&nome, &email, &senhaHash, &papel, &emailVerificado, &ativo)
	if err != nil {
		t.Fatalf("falha ao ler conta criada: %v", err)
	}

	if nome != "Primeira Adm" {
		t.Errorf("nome = %q, want %q", nome, "Primeira Adm")
	}
	if email != "admin@empresa.com" {
		t.Errorf("email = %q, want %q (normalizado para minúsculas)", email, "admin@empresa.com")
	}
	if papel != "adm" {
		t.Errorf("papel = %q, want %q", papel, "adm")
	}
	if !emailVerificado {
		t.Error("email_verificado = false, want true")
	}
	if !ativo {
		t.Error("ativo = false, want true")
	}
	if senhaHash == "senha-super-secreta" {
		t.Fatal("senha_hash gravada em texto plano")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(senhaHash), []byte("senha-super-secreta")); err != nil {
		t.Errorf("senha_hash não confere com a senha original via bcrypt: %v", err)
	}
}

// TestSeedAdmin_Duplicado prova o AC3: já existe uma conta adm, uma segunda
// chamada a seed-admin falha com erro claro e não altera nem insere linha
// alguma.
func TestSeedAdmin_Duplicado(t *testing.T) {
	db := testDB(t)
	garantirEmpresaSeed(t, db)

	firstID, err := seedAdmin(db, slugEmpresaSeed, "Primeiro Adm", "primeiro@empresa.com", "senha-123456")
	if err != nil {
		t.Fatalf("primeiro seedAdmin falhou: %v", err)
	}

	_, err = seedAdmin(db, slugEmpresaSeed, "Segundo Adm", "segundo@empresa.com", "outra-senha")
	if !errors.Is(err, errAdminAlreadyExists) {
		t.Fatalf("erro = %v, want errAdminAlreadyExists", err)
	}

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM usuarios WHERE papel = 'adm'`).Scan(&count); err != nil {
		t.Fatalf("falha ao contar admins: %v", err)
	}
	if count != 1 {
		t.Errorf("count(adm) = %d, want 1 — segunda tentativa não pode inserir nada", count)
	}

	var totalRows int
	if err := db.QueryRow(`SELECT count(*) FROM usuarios`).Scan(&totalRows); err != nil {
		t.Fatalf("falha ao contar linhas: %v", err)
	}
	if totalRows != 1 {
		t.Errorf("count(*) usuarios = %d, want 1 — segunda tentativa não pode inserir nenhuma linha", totalRows)
	}

	var nome string
	if err := db.QueryRow(`SELECT nome FROM usuarios WHERE id = $1`, firstID).Scan(&nome); err != nil {
		t.Fatalf("falha ao reler admin original: %v", err)
	}
	if nome != "Primeiro Adm" {
		t.Errorf("nome do admin original = %q, want %q — não pode ter sido alterado", nome, "Primeiro Adm")
	}
}

// TestSeedAdmin_EmailMaiusculo prova o cenário "Seed com e-mail maiúsculo" da
// I/O Matrix: --email=Admin@Empresa.COM é gravado como admin@empresa.com.
func TestSeedAdmin_EmailMaiusculo(t *testing.T) {
	db := testDB(t)
	garantirEmpresaSeed(t, db)

	id, err := seedAdmin(db, slugEmpresaSeed, "Adm Maiusculo", "ADMIN@EMPRESA.COM", "senha-123456")
	if err != nil {
		t.Fatalf("seedAdmin falhou: %v", err)
	}

	var email string
	if err := db.QueryRow(`SELECT email FROM usuarios WHERE id = $1`, id).Scan(&email); err != nil {
		t.Fatalf("falha ao ler conta criada: %v", err)
	}
	if email != "admin@empresa.com" {
		t.Errorf("email = %q, want %q", email, "admin@empresa.com")
	}
}

// TestSeedAdmin_SenhaFraca prova o cenário "Seed com senha fraca" da I/O
// Matrix: esta story não valida força de senha (deferido à Story 1.10) —
// seedAdmin apenas hasheia o que recebeu, sem rejeitar.
func TestSeedAdmin_SenhaFraca(t *testing.T) {
	db := testDB(t)
	garantirEmpresaSeed(t, db)

	id, err := seedAdmin(db, slugEmpresaSeed, "Adm Senha Fraca", "fraca@empresa.com", "123")
	if err != nil {
		t.Fatalf("seedAdmin retornou erro para senha fraca (fora de escopo desta story): %v", err)
	}

	var senhaHash string
	if err := db.QueryRow(`SELECT senha_hash FROM usuarios WHERE id = $1`, id).Scan(&senhaHash); err != nil {
		t.Fatalf("falha ao ler conta criada: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(senhaHash), []byte("123")); err != nil {
		t.Errorf("senha_hash não confere com a senha fraca original via bcrypt: %v", err)
	}
}

// TestSeedAdmin_Concorrente prova o backstop de correção contra a corrida
// real descrita no comentário de seedAdmin: duas chamadas concorrentes (não
// sequenciais, como em TestSeedAdmin_Duplicado) podem ambas avaliar
// WHERE NOT EXISTS como verdadeiro antes de qualquer uma commitar. Exatamente
// uma deve conseguir inserir; a perdedora deve receber errAdminAlreadyExists
// — seja pelo caminho sql.ErrNoRows (WHERE NOT EXISTS falso na hora do
// commit), seja pelo caminho pq.Error mapeando a violação de unicidade
// (SQLSTATE 23505) do índice idx_usuarios_unico_adm. Este teste é o único que
// realmente força a corrida via seedAdmin(); TestRunMigrations_UniqueAdminIndex
// (em backend/main_test.go) só prova a garantia via INSERT bruto, sem passar
// pelo mapeamento de erro desta função.
func TestSeedAdmin_Concorrente(t *testing.T) {
	db := testDB(t)
	garantirEmpresaSeed(t, db)

	const n = 2
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, n)

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := seedAdmin(db, slugEmpresaSeed, fmt.Sprintf("Adm Concorrente %d", i), fmt.Sprintf("concorrente%d@empresa.com", i), "senha-123456")
			results[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	var successCount, alreadyExistsCount int
	for _, err := range results {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, errAdminAlreadyExists):
			alreadyExistsCount++
		default:
			t.Fatalf("erro inesperado em execução concorrente: %v", err)
		}
	}

	if successCount != 1 {
		t.Errorf("successCount = %d, want 1", successCount)
	}
	if alreadyExistsCount != n-1 {
		t.Errorf("alreadyExistsCount = %d, want %d", alreadyExistsCount, n-1)
	}

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM usuarios WHERE papel = 'adm'`).Scan(&count); err != nil {
		t.Fatalf("falha ao contar admins: %v", err)
	}
	if count != 1 {
		t.Errorf("count(adm) = %d, want 1 — corrida não pode criar mais de um adm", count)
	}
}

func TestNormalizeEmail(t *testing.T) {
	cases := map[string]string{
		"Admin@Empresa.COM": "admin@empresa.com",
		"  a@b.com  ":       "a@b.com",
		"already@lower.com": "already@lower.com",
	}
	for in, want := range cases {
		if got := normalizeEmail(in); got != want {
			t.Errorf("normalizeEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestValidateFlags cobre o caminho de validação exercitado por main() antes
// de tocar no banco — em particular o caso de --senha só com espaços, que uma
// checagem anterior sem TrimSpace deixava passar (corrigido na revisão desta
// story) e que nenhum teste, até então, cobria diretamente.
func TestValidateFlags(t *testing.T) {
	cases := []struct {
		name, nome, email, senha, slug string
		wantErr                        bool
	}{
		{"tudo preenchido", "Nome", "e@x.com", "senha123", "empresa", false},
		{"nome vazio", "", "e@x.com", "senha123", "empresa", true},
		{"nome só espaços", "   ", "e@x.com", "senha123", "empresa", true},
		{"email vazio", "Nome", "", "senha123", "empresa", true},
		{"senha vazia", "Nome", "e@x.com", "", "empresa", true},
		{"senha só espaços", "Nome", "e@x.com", "   ", "empresa", true},
		// Story 15.1: toda conta precisa de Empresa — sem slug nada chega ao banco.
		{"slug vazio", "Nome", "e@x.com", "senha123", "", true},
		{"slug só espaços", "Nome", "e@x.com", "senha123", "   ", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateFlags(c.nome, c.email, c.senha, c.slug)
			if (err != nil) != c.wantErr {
				t.Errorf("validateFlags(%q, %q, %q, %q) err = %v, wantErr %v", c.nome, c.email, c.senha, c.slug, err, c.wantErr)
			}
		})
	}
}

// slugEmpresaSeed é a Empresa em que os testes criam o `adm`: desde a Story
// 15.1 toda conta tem Empresa, então nenhum cenário usa mais o slug vazio
// (exceto o que prova a recusa).
const slugEmpresaSeed = "seed-admin-empresa"

// garantirEmpresaSeed devolve o id da Empresa desta suíte, provisionando-a na
// primeira chamada — Story 9.1 (Multi-Empresa), spec-9-1. `empresas` não é
// truncada entre testes (ver testDB), então o SELECT por slug reaproveita a
// linha já gravada.
func garantirEmpresaSeed(t *testing.T, db *sql.DB) string {
	t.Helper()

	const slug = slugEmpresaSeed
	var id string
	err := db.QueryRow(`SELECT id FROM empresas WHERE slug = $1`, slug).Scan(&id)
	if err == nil {
		return id
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("garantirEmpresaSeed: %v", err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("garantirEmpresaSeed: begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	e, err := services.ProvisionarEmpresa(tx, services.DadosEmpresa{
		NomeFantasia: "Empresa Seed Admin",
		RazaoSocial:  "Empresa Seed Admin LTDA",
		CNPJ:         "23456789000195",
		Slug:         slug,
		Endereco: services.EnderecoEmpresa{
			Logradouro: "Rua de Teste",
			Numero:     "100",
			Bairro:     "Centro",
			Cidade:     "Recife",
			CEP:        "50000000",
			UF:         "PE",
		},
	})
	if err != nil {
		t.Fatalf("garantirEmpresaSeed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("garantirEmpresaSeed: commit: %v", err)
	}
	return e.ID
}

// TestSeedAdmin_ComEmpresaSlug prova o contrato do flag `--empresa-slug`
// (Story 9.1; obrigatório desde a Story 15.1): com o slug, o `adm` nasce DENTRO da Empresa;
// um segundo `adm` na mesma Empresa é recusado; e o `adm` sem Empresa passa a
// ser recusado pelo banco (Story 15.1).
func TestSeedAdmin_ComEmpresaSlug(t *testing.T) {
	db := testDB(t)
	empresaID := garantirEmpresaSeed(t, db)

	id, err := seedAdmin(db, "seed-admin-empresa", "Adm da Empresa", "adm-empresa@empresa.com", "senha-123456")
	if err != nil {
		t.Fatalf("seedAdmin com --empresa-slug: %v", err)
	}
	var gravado sql.NullString
	var papel string
	if err := db.QueryRow(`SELECT empresa_id, papel FROM usuarios WHERE id = $1`, id).Scan(&gravado, &papel); err != nil {
		t.Fatalf("falha ao reler a conta criada: %v", err)
	}
	if !gravado.Valid || gravado.String != empresaID {
		t.Errorf("empresa_id = %v, want %q", gravado, empresaID)
	}
	if papel != "adm" {
		t.Errorf("papel = %q, want adm", papel)
	}

	// Segundo `adm` na MESMA Empresa: recusado.
	if _, err := seedAdmin(db, "seed-admin-empresa", "Segundo Adm", "segundo-empresa@empresa.com", "senha-123456"); !errors.Is(err, errAdminAlreadyExists) {
		t.Fatalf("segundo adm na mesma Empresa: erro = %v, want errAdminAlreadyExists", err)
	}

	// Slug vazio (main() já o recusa em validateFlags): não resolve nenhuma
	// Empresa e nenhuma linha é gravada — conta sem Empresa não existe desde
	// a Story 15.1.
	antes := 0
	if err := db.QueryRow(`SELECT count(*) FROM usuarios`).Scan(&antes); err != nil {
		t.Fatalf("falha ao contar linhas: %v", err)
	}
	if _, err := seedAdmin(db, "", "Adm Sem Empresa", "adm-sem-empresa@empresa.com", "senha-123456"); !errors.Is(err, errEmpresaNaoEncontrada) {
		t.Fatalf("seedAdmin sem slug: erro = %v, want errEmpresaNaoEncontrada", err)
	}
	depois := 0
	if err := db.QueryRow(`SELECT count(*) FROM usuarios`).Scan(&depois); err != nil {
		t.Fatalf("falha ao contar linhas: %v", err)
	}
	if depois != antes {
		t.Errorf("count(*) usuarios = %d, want %d — conta sem Empresa não pode ser gravada", depois, antes)
	}
}

// TestSeedAdmin_EmpresaSlugInexistente prova que um slug que não resolve
// aborta sem escrever nada — o operador recebe erro claro em vez de um `adm`
// criado fora de qualquer Empresa.
func TestSeedAdmin_EmpresaSlugInexistente(t *testing.T) {
	db := testDB(t)

	if _, err := seedAdmin(db, "empresa-que-nao-existe", "Adm Fantasma", "fantasma@empresa.com", "senha-123456"); !errors.Is(err, errEmpresaNaoEncontrada) {
		t.Fatalf("erro = %v, want errEmpresaNaoEncontrada", err)
	}
	var total int
	if err := db.QueryRow(`SELECT count(*) FROM usuarios`).Scan(&total); err != nil {
		t.Fatalf("falha ao contar linhas: %v", err)
	}
	if total != 0 {
		t.Errorf("count(*) usuarios = %d, want 0 — slug inválido não pode inserir nada", total)
	}
}

// garantirOutraEmpresaSeed provisiona (uma vez) uma segunda Empresa real para
// o cenário de e-mail já usado em outra Empresa (Story 15.1).
func garantirOutraEmpresaSeed(t *testing.T, db *sql.DB) string {
	t.Helper()
	const slug = "seed-admin-outra"
	var id string
	err := db.QueryRow(`SELECT id FROM empresas WHERE slug = $1`, slug).Scan(&id)
	if err == nil {
		return id
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("garantirOutraEmpresaSeed: %v", err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("garantirOutraEmpresaSeed: begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	e, err := services.ProvisionarEmpresa(tx, services.DadosEmpresa{
		NomeFantasia: "Empresa Seed Admin Outra",
		RazaoSocial:  "Empresa Seed Admin Outra LTDA",
		CNPJ:         "91510000002130",
		Slug:         slug,
		Endereco: services.EnderecoEmpresa{
			Logradouro: "Rua de Teste", Numero: "100", Bairro: "Centro",
			Cidade: "Recife", CEP: "50000000", UF: "PE",
		},
	})
	if err != nil {
		t.Fatalf("garantirOutraEmpresaSeed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("garantirOutraEmpresaSeed: commit: %v", err)
	}
	return e.ID
}

// TestSeedAdmin_EmailEmUsoEmOutraEmpresa prova que o e-mail já usado em outra
// Empresa real é recusado com errEmailEmUso (23P01 da restrição
// usuarios_email_unico_entre_empresas_reais) e nada é gravado.
func TestSeedAdmin_EmailEmUsoEmOutraEmpresa(t *testing.T) {
	db := testDB(t)
	garantirEmpresaSeed(t, db)
	outra := garantirOutraEmpresaSeed(t, db)
	if _, err := db.Exec(
		`INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, empresa_id)
		 VALUES ('Na Outra', 'ja.usado@empresa.com', 'h', 'usuario', true, true, $1)`, outra,
	); err != nil {
		t.Fatalf("conta na outra Empresa: %v", err)
	}

	if _, err := seedAdmin(db, slugEmpresaSeed, "Adm", "JA.USADO@empresa.com", "senha-123456"); !errors.Is(err, errEmailEmUso) {
		t.Fatalf("erro = %v, want errEmailEmUso", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM usuarios WHERE papel = 'adm'`).Scan(&n); err != nil {
		t.Fatalf("contar adm: %v", err)
	}
	if n != 0 {
		t.Errorf("count(adm) = %d, want 0", n)
	}
}
