package services

import (
	"database/sql"
	"encoding/json"
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
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

var (
	migrateOnce sync.Once
	migrateErr  error
)

// testDB abre uma conexão contra DATABASE_URL, aplica as migrations reais do
// projeto (mesmo mecanismo de backend/cmd/seed-admin/main_test.go: source
// file://../migrations) e limpa usuarios/tokens_acao/emails_pendentes antes
// de cada teste. Pula o teste quando nenhum Postgres foi configurado — suba
// um com `docker compose up -d db` (ou um Postgres local) e exporte
// DATABASE_URL. Ver o comentário equivalente em backend/main_test.go sobre
// por que a suíte completa deve rodar com `go test -p 1 ./...`.
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
			m, migrateErr = migrate.New("file://../migrations", dsn)
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

	// CASCADE: tokens_acao e emails_pendentes referenciam usuarios(id) via FK
	// ON DELETE CASCADE (migration 000002) — truncar usuarios já limpa as
	// duas tabelas dependentes, mantendo os testes desta suíte isolados entre
	// si.
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("falha ao limpar tabelas entre testes: %v", err)
	}

	return db
}

// testEmailCfg é a EmailConfig usada nos testes desta suíte: AppURL fixo
// (para montar um link previsível) e Password sempre vazio — os testes desta
// story nunca dependem de credenciais reais de SMTP corporativo (AC4).
var testEmailCfg = EmailConfig{
	Host:     "smtp.invalid",
	Port:     "587",
	User:     "",
	Password: "",
	From:     "stockflow <noreply@stockflow.local>",
	AppURL:   "http://test.local",
}

func contarLinhas(t *testing.T, db *sql.DB, tabela string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s`, tabela)).Scan(&n); err != nil {
		t.Fatalf("falha ao contar linhas de %s: %v", tabela, err)
	}
	return n
}

// TestCadastrar_Sucesso prova o cenário "Cadastro válido" da I/O Matrix: a
// conta é criada como usuario/email_verificado=false, e o token +  a linha
// de outbox são inseridos na mesma transação.
func TestCadastrar_Sucesso(t *testing.T) {
	db := testDB(t)

	id, err := Cadastrar(db, testEmailCfg, "  Fulano de Tal  ", "Fulano@Empresa.COM", "senha-123456")
	if err != nil {
		t.Fatalf("Cadastrar retornou erro inesperado: %v", err)
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
	if nome != "Fulano de Tal" {
		t.Errorf("nome = %q, want %q", nome, "Fulano de Tal")
	}
	if email != "fulano@empresa.com" {
		t.Errorf("email = %q, want %q (normalizado)", email, "fulano@empresa.com")
	}
	if papel != "usuario" {
		t.Errorf("papel = %q, want %q", papel, "usuario")
	}
	if emailVerificado {
		t.Error("email_verificado = true, want false")
	}
	if !ativo {
		t.Error("ativo = false, want true")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(senhaHash), []byte("senha-123456")); err != nil {
		t.Errorf("senha_hash não confere com a senha original via bcrypt: %v", err)
	}

	var token, tipo string
	var usadoEm sql.NullTime
	var expiraEm time.Time
	err = db.QueryRow(`SELECT token, tipo, expira_em, usado_em FROM tokens_acao WHERE usuario_id = $1`, id).
		Scan(&token, &tipo, &expiraEm, &usadoEm)
	if err != nil {
		t.Fatalf("falha ao ler token de verificação: %v", err)
	}
	if tipo != "verificacao_email" {
		t.Errorf("tipo do token = %q, want %q", tipo, "verificacao_email")
	}
	if usadoEm.Valid {
		t.Error("usado_em já preenchido em token recém-criado")
	}
	if token == "" {
		t.Error("token vazio")
	}
	wantExpira := time.Now().UTC().Add(24 * time.Hour)
	if diff := wantExpira.Sub(expiraEm); diff < 0 || diff > time.Minute {
		t.Errorf("expira_em = %v, want ~24h a partir de agora (diff=%v)", expiraEm, diff)
	}

	var destinatario, tipoEmail, status, variaveisRaw string
	err = db.QueryRow(`SELECT destinatario, tipo, status, variaveis_json::text FROM emails_pendentes WHERE usuario_id = $1`, id).
		Scan(&destinatario, &tipoEmail, &status, &variaveisRaw)
	if err != nil {
		t.Fatalf("falha ao ler linha de outbox: %v", err)
	}
	if destinatario != "fulano@empresa.com" {
		t.Errorf("destinatario = %q, want %q", destinatario, "fulano@empresa.com")
	}
	if tipoEmail != "verificacao_conta" {
		t.Errorf("tipo do e-mail = %q, want %q", tipoEmail, "verificacao_conta")
	}
	if status != "pendente" {
		t.Errorf("status = %q, want %q", status, "pendente")
	}
	var variaveis map[string]any
	if err := json.Unmarshal([]byte(variaveisRaw), &variaveis); err != nil {
		t.Fatalf("variaveis_json inválido: %v", err)
	}
	link, _ := variaveis["link"].(string)
	if link == "" {
		t.Error("variaveis_json.link vazio")
	}
	wantLinkPrefix := "http://test.local/verificar-email?token=" + token
	if link != wantLinkPrefix {
		t.Errorf("link = %q, want %q", link, wantLinkPrefix)
	}
}

// TestCadastrar_EmailDuplicado prova o cenário "E-mail duplicado" da I/O
// Matrix: mesmo e-mail em outra capitalização deve colidir com
// ErrEmailDuplicado, sem inserir nenhuma linha nova em nenhuma tabela.
func TestCadastrar_EmailDuplicado(t *testing.T) {
	db := testDB(t)

	if _, err := Cadastrar(db, testEmailCfg, "Primeiro", "duplicado@empresa.com", "senha-123456"); err != nil {
		t.Fatalf("primeiro Cadastrar falhou: %v", err)
	}

	usuariosAntes := contarLinhas(t, db, "usuarios")
	tokensAntes := contarLinhas(t, db, "tokens_acao")
	emailsAntes := contarLinhas(t, db, "emails_pendentes")

	_, err := Cadastrar(db, testEmailCfg, "Segundo", "DUPLICADO@Empresa.com", "outra-senha")
	if !errors.Is(err, ErrEmailDuplicado) {
		t.Fatalf("erro = %v, want ErrEmailDuplicado", err)
	}

	if got := contarLinhas(t, db, "usuarios"); got != usuariosAntes {
		t.Errorf("count(usuarios) = %d, want %d — segunda tentativa não pode inserir nada", got, usuariosAntes)
	}
	if got := contarLinhas(t, db, "tokens_acao"); got != tokensAntes {
		t.Errorf("count(tokens_acao) = %d, want %d", got, tokensAntes)
	}
	if got := contarLinhas(t, db, "emails_pendentes"); got != emailsAntes {
		t.Errorf("count(emails_pendentes) = %d, want %d", got, emailsAntes)
	}
}

// TestCadastrar_CampoObrigatorioAusente prova o cenário "Campo obrigatório
// ausente/vazio" da I/O Matrix: nenhuma escrita acontece em nenhuma tabela.
func TestCadastrar_CampoObrigatorioAusente(t *testing.T) {
	db := testDB(t)

	casos := []struct {
		nome, email, senha string
	}{
		{"", "a@b.com", "senha123"},
		{"   ", "a@b.com", "senha123"},
		{"Nome", "", "senha123"},
		{"Nome", "   ", "senha123"},
		{"Nome", "a@b.com", ""},
		{"Nome", "a@b.com", "   "},
	}
	for _, c := range casos {
		t.Run(fmt.Sprintf("nome=%q email=%q senha=%q", c.nome, c.email, c.senha), func(t *testing.T) {
			antes := contarLinhas(t, db, "usuarios")
			_, err := Cadastrar(db, testEmailCfg, c.nome, c.email, c.senha)
			if !errors.Is(err, ErrCadastroValidacao) {
				t.Fatalf("erro = %v, want ErrCadastroValidacao", err)
			}
			if depois := contarLinhas(t, db, "usuarios"); depois != antes {
				t.Errorf("count(usuarios) = %d, want %d — nenhuma escrita esperada", depois, antes)
			}
		})
	}
}

// TestCadastrar_ValidacaoDeTamanho prova que nome/e-mail/senha acima dos
// limites da coluna/do bcrypt mapeiam para ErrCadastroValidacao (400) em vez
// de vazar um erro bruto do Postgres/bcrypt (500) — guards adicionados na
// passagem de revisão desta story, sem cobertura própria até este teste.
func TestCadastrar_ValidacaoDeTamanho(t *testing.T) {
	db := testDB(t)

	nomeGrande := strings.Repeat("a", 256)
	emailGrande := strings.Repeat("a", 250) + "@b.com" // > 255 caracteres
	senhaGrande := strings.Repeat("a", 73)             // bcrypt rejeita > 72 bytes

	casos := []struct {
		nome  string
		desc  string
		email string
		senha string
	}{
		{desc: "nome maior que 255 caracteres", nome: nomeGrande, email: "nomegrande@empresa.com", senha: "senha-123456"},
		{desc: "e-mail maior que 255 caracteres", nome: "Nome", email: emailGrande, senha: "senha-123456"},
		{desc: "senha maior que 72 bytes", nome: "Nome", email: "senhagrande@empresa.com", senha: senhaGrande},
	}
	for _, c := range casos {
		t.Run(c.desc, func(t *testing.T) {
			antes := contarLinhas(t, db, "usuarios")
			_, err := Cadastrar(db, testEmailCfg, c.nome, c.email, c.senha)
			if !errors.Is(err, ErrCadastroValidacao) {
				t.Fatalf("erro = %v, want ErrCadastroValidacao", err)
			}
			if depois := contarLinhas(t, db, "usuarios"); depois != antes {
				t.Errorf("count(usuarios) = %d, want %d — nenhuma escrita esperada", depois, antes)
			}
		})
	}
}

// TestCadastrar_NomeComAcentosDentroDoLimiteDeCaracteres prova que o guard de
// tamanho de TestCadastrar_ValidacaoDeTamanho compara caracteres, não bytes:
// VARCHAR(255) do Postgres conta caracteres, então um nome com acentos (comum
// em PT-BR) que tenha até 255 caracteres deve ser aceito mesmo passando de
// 255 bytes em UTF-8 (cada "á" ocupa 2 bytes).
func TestCadastrar_NomeComAcentosDentroDoLimiteDeCaracteres(t *testing.T) {
	db := testDB(t)

	nome := strings.Repeat("á", 255) // 255 caracteres, 510 bytes em UTF-8
	if len(nome) <= 255 {
		t.Fatalf("nome de teste deveria ter mais de 255 bytes, tem %d", len(nome))
	}

	usuarioID, err := Cadastrar(db, testEmailCfg, nome, "nomeacentuado@empresa.com", "senha-123456")
	if err != nil {
		t.Fatalf("Cadastrar retornou erro inesperado para nome de 255 caracteres: %v", err)
	}
	if usuarioID == "" {
		t.Error("usuarioID vazio, want id gerado")
	}
}

// criarUsuarioComToken é um helper de teste que cria uma conta via Cadastrar
// e retorna o id do usuário e o token de verificação gerado.
func criarUsuarioComToken(t *testing.T, db *sql.DB, email string) (usuarioID, token string) {
	t.Helper()
	usuarioID, err := Cadastrar(db, testEmailCfg, "Usuário Teste", email, "senha-123456")
	if err != nil {
		t.Fatalf("Cadastrar falhou: %v", err)
	}
	if err := db.QueryRow(`SELECT token FROM tokens_acao WHERE usuario_id = $1`, usuarioID).Scan(&token); err != nil {
		t.Fatalf("falha ao ler token gerado: %v", err)
	}
	return usuarioID, token
}

// TestVerificarEmail_LinkValido prova o cenário "Link de verificação válido"
// da I/O Matrix: email_verificado vira true e o token é marcado usado.
func TestVerificarEmail_LinkValido(t *testing.T) {
	db := testDB(t)
	usuarioID, token := criarUsuarioComToken(t, db, "verificar-ok@empresa.com")

	if err := VerificarEmail(db, token); err != nil {
		t.Fatalf("VerificarEmail retornou erro inesperado: %v", err)
	}

	var emailVerificado bool
	if err := db.QueryRow(`SELECT email_verificado FROM usuarios WHERE id = $1`, usuarioID).Scan(&emailVerificado); err != nil {
		t.Fatalf("falha ao reler usuario: %v", err)
	}
	if !emailVerificado {
		t.Error("email_verificado = false, want true")
	}

	var usadoEm sql.NullTime
	if err := db.QueryRow(`SELECT usado_em FROM tokens_acao WHERE token = $1`, token).Scan(&usadoEm); err != nil {
		t.Fatalf("falha ao reler token: %v", err)
	}
	if !usadoEm.Valid {
		t.Error("usado_em não preenchido após verificação bem-sucedida")
	}
}

// TestVerificarEmail_LinkExpirado prova o cenário "Link expirado" da I/O
// Matrix: expira_em no passado retorna ErrTokenExpirado e não altera
// email_verificado.
func TestVerificarEmail_LinkExpirado(t *testing.T) {
	db := testDB(t)
	usuarioID, token := criarUsuarioComToken(t, db, "verificar-expirado@empresa.com")

	if _, err := db.Exec(`UPDATE tokens_acao SET expira_em = now() - interval '1 hour' WHERE token = $1`, token); err != nil {
		t.Fatalf("falha ao forçar expiração: %v", err)
	}

	err := VerificarEmail(db, token)
	if !errors.Is(err, ErrTokenExpirado) {
		t.Fatalf("erro = %v, want ErrTokenExpirado", err)
	}

	var emailVerificado bool
	if err := db.QueryRow(`SELECT email_verificado FROM usuarios WHERE id = $1`, usuarioID).Scan(&emailVerificado); err != nil {
		t.Fatalf("falha ao reler usuario: %v", err)
	}
	if emailVerificado {
		t.Error("email_verificado = true, want false (token expirado não pode liberar a conta)")
	}
}

// TestVerificarEmail_LinkJaUsado prova o cenário "Link já usado" da I/O
// Matrix: usado_em preenchido retorna ErrTokenExpirado de forma idempotente
// — reaplicar o link não reexecuta o efeito.
func TestVerificarEmail_LinkJaUsado(t *testing.T) {
	db := testDB(t)
	_, token := criarUsuarioComToken(t, db, "verificar-usado@empresa.com")

	if err := VerificarEmail(db, token); err != nil {
		t.Fatalf("primeira verificação falhou: %v", err)
	}

	err := VerificarEmail(db, token)
	if !errors.Is(err, ErrTokenExpirado) {
		t.Fatalf("segunda verificação: erro = %v, want ErrTokenExpirado", err)
	}
}

// TestVerificarEmail_Concorrente prova o backstop de correção contra a
// corrida real fechada pela condição repetida no UPDATE de marcarUsado
// (`usado_em IS NULL AND expira_em > now()`): duas chamadas concorrentes de
// VerificarEmail para o MESMO token (não sequenciais, como em
// TestVerificarEmail_LinkJaUsado) podem ambas passar pelo SELECT inicial
// antes de qualquer uma commitar. Exatamente uma deve conseguir marcar o
// token como usado e liberar email_verificado; a perdedora deve receber
// ErrTokenExpirado. Mesmo padrão de TestSeedAdmin_Concorrente em
// backend/cmd/seed-admin/main_test.go.
func TestVerificarEmail_Concorrente(t *testing.T) {
	db := testDB(t)
	usuarioID, token := criarUsuarioComToken(t, db, "verificar-concorrente@empresa.com")

	const n = 2
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, n)

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = VerificarEmail(db, token)
		}(i)
	}
	close(start)
	wg.Wait()

	var successCount, expiradoCount int
	for _, err := range results {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, ErrTokenExpirado):
			expiradoCount++
		default:
			t.Fatalf("erro inesperado em execução concorrente: %v", err)
		}
	}

	if successCount != 1 {
		t.Errorf("successCount = %d, want 1", successCount)
	}
	if expiradoCount != n-1 {
		t.Errorf("expiradoCount = %d, want %d", expiradoCount, n-1)
	}

	var emailVerificado bool
	if err := db.QueryRow(`SELECT email_verificado FROM usuarios WHERE id = $1`, usuarioID).Scan(&emailVerificado); err != nil {
		t.Fatalf("falha ao reler usuario: %v", err)
	}
	if !emailVerificado {
		t.Error("email_verificado = false, want true — a chamada vencedora deve ter liberado a conta")
	}
}

// TestVerificarEmail_TokenInexistente prova o cenário "Token
// inexistente/malformado" da I/O Matrix: string arbitrária retorna
// ErrTokenNaoEncontrado.
func TestVerificarEmail_TokenInexistente(t *testing.T) {
	db := testDB(t)

	err := VerificarEmail(db, "token-que-nunca-existiu")
	if !errors.Is(err, ErrTokenNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrTokenNaoEncontrado", err)
	}
}

func TestGerarTokenAcao_GeraValoresUnicosENaoVazios(t *testing.T) {
	vistos := map[string]bool{}
	for i := 0; i < 50; i++ {
		token, err := gerarTokenAcao()
		if err != nil {
			t.Fatalf("gerarTokenAcao retornou erro: %v", err)
		}
		if token == "" {
			t.Fatal("token vazio")
		}
		if vistos[token] {
			t.Fatalf("token repetido: %q", token)
		}
		vistos[token] = true
	}
}

func TestNormalizeEmail(t *testing.T) {
	cases := map[string]string{
		"Fulano@Empresa.COM": "fulano@empresa.com",
		"  a@b.com  ":        "a@b.com",
		"already@lower.com":  "already@lower.com",
	}
	for in, want := range cases {
		if got := normalizeEmail(in); got != want {
			t.Errorf("normalizeEmail(%q) = %q, want %q", in, got, want)
		}
	}
}
