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

	"github.com/golang-jwt/jwt/v5"
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

	_, err := Cadastrar(db, testEmailCfg, "Segundo", "DUPLICADO@Empresa.com", "outra-senha1")
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
// limites da coluna/do bcrypt mapeiam para um erro de validação (400) em vez
// de vazar um erro bruto do Postgres/bcrypt (500). Nome/e-mail longos ->
// ErrCadastroValidacao; a partir da Story 1.10 a senha acima de 72 bytes é
// rejeitada por ValidarForcaSenha como ErrSenhaFraca (o guard próprio de
// tamanho em Cadastrar foi removido por redundância) — ambos 400
// VALIDATION_ERROR na fronteira HTTP.
func TestCadastrar_ValidacaoDeTamanho(t *testing.T) {
	db := testDB(t)

	nomeGrande := strings.Repeat("a", 256)
	emailGrande := strings.Repeat("a", 250) + "@b.com" // > 255 caracteres
	senhaGrande := strings.Repeat("a", 73)             // bcrypt rejeita > 72 bytes

	casos := []struct {
		desc    string
		nome    string
		email   string
		senha   string
		wantErr error
	}{
		{desc: "nome maior que 255 caracteres", nome: nomeGrande, email: "nomegrande@empresa.com", senha: "senha-123456", wantErr: ErrCadastroValidacao},
		{desc: "e-mail maior que 255 caracteres", nome: "Nome", email: emailGrande, senha: "senha-123456", wantErr: ErrCadastroValidacao},
		{desc: "senha maior que 72 bytes", nome: "Nome", email: "senhagrande@empresa.com", senha: senhaGrande, wantErr: ErrSenhaFraca},
	}
	for _, c := range casos {
		t.Run(c.desc, func(t *testing.T) {
			antes := contarLinhas(t, db, "usuarios")
			_, err := Cadastrar(db, testEmailCfg, c.nome, c.email, c.senha)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("erro = %v, want %v", err, c.wantErr)
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

// testJWTSecret é o segredo usado para assinar/validar tokens de sessão
// nesta suíte — mesmo padrão de testEmailCfg acima.
var testJWTSecret = []byte("segredo-de-teste-nao-usar-em-producao")

// criarUsuarioParaLogin insere uma linha diretamente em usuarios (sem passar
// por Cadastrar, que sempre cria email_verificado=false) para poder exercitar
// livremente todas as combinações de ativo/email_verificado/senha_hash da I/O
// Matrix de login. senha vazia grava senha_hash NULL (conta só-SSO).
func criarUsuarioParaLogin(t *testing.T, db *sql.DB, email, senha string, ativo, emailVerificado bool) string {
	t.Helper()

	var senhaHash sql.NullString
	if senha != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
		if err != nil {
			t.Fatalf("falha ao gerar hash de senha de teste: %v", err)
		}
		senhaHash = sql.NullString{String: string(hash), Valid: true}
	}

	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
		VALUES ('Usuário Teste', $1, $2, 'usuario', $3, $4)
		RETURNING id`
	if err := db.QueryRow(insert, email, senhaHash, emailVerificado, ativo).Scan(&id); err != nil {
		t.Fatalf("falha ao criar usuario de teste: %v", err)
	}
	return id
}

// TestLogin_Sucesso prova o cenário "Login válido" da I/O Matrix: conta
// ativa, e-mail verificado e senha correta devolvem o id do usuário sem
// erro.
func TestLogin_Sucesso(t *testing.T) {
	db := testDB(t)
	id := criarUsuarioParaLogin(t, db, "login-ok@empresa.com", "senha-123456", true, true)

	got, err := Login(db, "Login-OK@Empresa.com", "senha-123456")
	if err != nil {
		t.Fatalf("Login retornou erro inesperado: %v", err)
	}
	if got != id {
		t.Errorf("usuarioID = %q, want %q", got, id)
	}
}

// TestLogin_CredenciaisInvalidas prova que TODOS os cenários malsucedidos da
// I/O Matrix (senha errada, e-mail inexistente, e-mail não verificado, conta
// desativada, conta só-SSO) devolvem exatamente ErrCredenciaisInvalidas —
// nunca um erro que permita distinguir qual condição falhou.
func TestLogin_CredenciaisInvalidas(t *testing.T) {
	db := testDB(t)

	criarUsuarioParaLogin(t, db, "senha-errada@empresa.com", "senha-correta", true, true)
	criarUsuarioParaLogin(t, db, "nao-verificado@empresa.com", "senha-123456", true, false)
	criarUsuarioParaLogin(t, db, "desativado@empresa.com", "senha-123456", false, true)
	criarUsuarioParaLogin(t, db, "so-sso@empresa.com", "", true, true)

	casos := []struct {
		nome, email, senha string
	}{
		{"senha incorreta", "senha-errada@empresa.com", "senha-incorreta"},
		{"e-mail inexistente", "nunca-existiu@empresa.com", "qualquer-senha"},
		{"e-mail não verificado", "nao-verificado@empresa.com", "senha-123456"},
		{"conta desativada", "desativado@empresa.com", "senha-123456"},
		{"conta só-SSO (senha_hash nulo)", "so-sso@empresa.com", "qualquer-senha"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := Login(db, c.email, c.senha)
			if !errors.Is(err, ErrCredenciaisInvalidas) {
				t.Fatalf("erro = %v, want ErrCredenciaisInvalidas", err)
			}
		})
	}
}

// TestLogin_CampoObrigatorioAusente prova o cenário "Campo obrigatório
// ausente" da I/O Matrix: e-mail ou senha em branco retornam
// ErrLoginValidacao (400 VALIDATION_ERROR na fronteira HTTP).
func TestLogin_CampoObrigatorioAusente(t *testing.T) {
	db := testDB(t)

	casos := []struct{ email, senha string }{
		{"", "senha-123456"},
		{"   ", "senha-123456"},
		{"alguem@empresa.com", ""},
		{"alguem@empresa.com", "   "},
	}
	for _, c := range casos {
		t.Run(fmt.Sprintf("email=%q senha=%q", c.email, c.senha), func(t *testing.T) {
			_, err := Login(db, c.email, c.senha)
			if !errors.Is(err, ErrLoginValidacao) {
				t.Fatalf("erro = %v, want ErrLoginValidacao", err)
			}
		})
	}
}

// TestEmitirSessao_EmiteAccessTokenEPersisteRefresh prova a AC principal
// desta story: EmitirSessao devolve um access JWT válido (sub=usuarioID,
// exp ~30min) e persiste uma linha em `sessoes` com o refresh token, TTL de
// ~2h e revogado_em nulo.
func TestEmitirSessao_EmiteAccessTokenEPersisteRefresh(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "sessao@empresa.com", "senha-123456", true, true)

	accessToken, refreshToken, expiraRefresh, err := EmitirSessao(db, testJWTSecret, usuarioID, "senha")
	if err != nil {
		t.Fatalf("EmitirSessao retornou erro inesperado: %v", err)
	}
	if accessToken == "" || refreshToken == "" {
		t.Fatal("accessToken/refreshToken vazios")
	}

	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(accessToken, claims, func(*jwt.Token) (any, error) { return testJWTSecret, nil })
	if err != nil || !parsed.Valid {
		t.Fatalf("access token inválido: %v", err)
	}
	if claims.Subject != usuarioID {
		t.Errorf("claim sub = %q, want %q", claims.Subject, usuarioID)
	}
	wantExpira := time.Now().UTC().Add(accessTokenExpiracao)
	if diff := wantExpira.Sub(claims.ExpiresAt.Time); diff < 0 || diff > time.Minute {
		t.Errorf("access token exp = %v, want ~30min a partir de agora (diff=%v)", claims.ExpiresAt.Time, diff)
	}

	var dbRefreshToken string
	var dbExpiraEm time.Time
	var dbRevogadoEm sql.NullTime
	var dbUsuarioID string
	err = db.QueryRow(`SELECT usuario_id, refresh_token, expira_em, revogado_em FROM sessoes WHERE refresh_token = $1`, refreshToken).
		Scan(&dbUsuarioID, &dbRefreshToken, &dbExpiraEm, &dbRevogadoEm)
	if err != nil {
		t.Fatalf("falha ao ler sessão persistida: %v", err)
	}
	if dbUsuarioID != usuarioID {
		t.Errorf("sessoes.usuario_id = %q, want %q", dbUsuarioID, usuarioID)
	}
	if dbRevogadoEm.Valid {
		t.Error("sessoes.revogado_em já preenchido em sessão recém-criada")
	}
	if diff := expiraRefresh.Sub(dbExpiraEm); diff < -time.Second || diff > time.Second {
		t.Errorf("expiraRefresh retornado (%v) difere do persistido (%v)", expiraRefresh, dbExpiraEm)
	}
	wantExpiraRefresh := time.Now().UTC().Add(RefreshTokenExpiracao)
	if diff := wantExpiraRefresh.Sub(dbExpiraEm); diff < 0 || diff > time.Minute {
		t.Errorf("sessoes.expira_em = %v, want ~2h a partir de agora (diff=%v)", dbExpiraEm, diff)
	}
}

// TestRenovarSessao_Sucesso prova o cenário "Refresh válido" da I/O Matrix:
// a linha antiga é marcada revogada e uma nova é inserida na mesma
// transação, com um novo access token válido para o mesmo usuário.
func TestRenovarSessao_Sucesso(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "renovar-ok@empresa.com", "senha-123456", true, true)
	_, refreshToken, _, err := EmitirSessao(db, testJWTSecret, usuarioID, "senha")
	if err != nil {
		t.Fatalf("EmitirSessao falhou: %v", err)
	}

	novoAccess, novoRefresh, novoExpiraRefresh, err := RenovarSessao(db, testJWTSecret, refreshToken)
	if err != nil {
		t.Fatalf("RenovarSessao retornou erro inesperado: %v", err)
	}
	if novoAccess == "" || novoRefresh == "" {
		t.Fatal("novoAccess/novoRefresh vazios")
	}
	if novoRefresh == refreshToken {
		t.Error("novoRefresh igual ao token antigo — rotação não aconteceu")
	}

	var revogadoEm sql.NullTime
	if err := db.QueryRow(`SELECT revogado_em FROM sessoes WHERE refresh_token = $1`, refreshToken).Scan(&revogadoEm); err != nil {
		t.Fatalf("falha ao reler sessão antiga: %v", err)
	}
	if !revogadoEm.Valid {
		t.Error("sessão antiga não marcada como revogada após rotação")
	}

	var novoUsuarioID string
	var novoRevogadoEm sql.NullTime
	var novoExpiraEmDB time.Time
	if err := db.QueryRow(`SELECT usuario_id, revogado_em, expira_em FROM sessoes WHERE refresh_token = $1`, novoRefresh).Scan(&novoUsuarioID, &novoRevogadoEm, &novoExpiraEmDB); err != nil {
		t.Fatalf("falha ao ler nova sessão: %v", err)
	}
	if novoUsuarioID != usuarioID {
		t.Errorf("nova sessão: usuario_id = %q, want %q", novoUsuarioID, usuarioID)
	}
	if novoRevogadoEm.Valid {
		t.Error("nova sessão já nasce revogada")
	}
	// novoExpiraRefresh (devolvido por RenovarSessao) precisa ser o MESMO
	// instante persistido em sessoes.expira_em — é exatamente esse contrato que
	// permite ao chamador HTTP (RefreshHandler) montar o cookie com o valor
	// realmente gravado, em vez de recalcular a partir de RefreshTokenExpiracao
	// e arriscar divergir pelo tempo do round-trip ao banco.
	if diff := novoExpiraRefresh.Sub(novoExpiraEmDB); diff < -time.Second || diff > time.Second {
		t.Errorf("novoExpiraRefresh retornado (%v) difere do persistido (%v)", novoExpiraRefresh, novoExpiraEmDB)
	}

	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(novoAccess, claims, func(*jwt.Token) (any, error) { return testJWTSecret, nil })
	if err != nil || !parsed.Valid || claims.Subject != usuarioID {
		t.Fatalf("novo access token inválido ou sub incorreto: err=%v sub=%q", err, claims.Subject)
	}
}

// TestRenovarSessao_TokenInexistenteOuVazio prova que um token que nunca
// existiu, ou uma string vazia (cookie ausente repassado como valor vazio
// pelo handler), retornam ErrSessaoInvalida.
func TestRenovarSessao_TokenInexistenteOuVazio(t *testing.T) {
	db := testDB(t)

	for _, token := range []string{"token-que-nunca-existiu", ""} {
		t.Run(fmt.Sprintf("token=%q", token), func(t *testing.T) {
			_, _, _, err := RenovarSessao(db, testJWTSecret, token)
			if !errors.Is(err, ErrSessaoInvalida) {
				t.Fatalf("erro = %v, want ErrSessaoInvalida", err)
			}
		})
	}
}

// TestRenovarSessao_TokenExpirado prova o cenário "Refresh ausente/expirado/
// revogado" da I/O Matrix para o caso de expiração: expira_em no passado
// retorna ErrSessaoInvalida e não revoga a linha (nada a rotacionar).
func TestRenovarSessao_TokenExpirado(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "renovar-expirado@empresa.com", "senha-123456", true, true)
	_, refreshToken, _, err := EmitirSessao(db, testJWTSecret, usuarioID, "senha")
	if err != nil {
		t.Fatalf("EmitirSessao falhou: %v", err)
	}
	if _, err := db.Exec(`UPDATE sessoes SET expira_em = now() - interval '1 hour' WHERE refresh_token = $1`, refreshToken); err != nil {
		t.Fatalf("falha ao forçar expiração: %v", err)
	}

	_, _, _, err = RenovarSessao(db, testJWTSecret, refreshToken)
	if !errors.Is(err, ErrSessaoInvalida) {
		t.Fatalf("erro = %v, want ErrSessaoInvalida", err)
	}
}

// TestRenovarSessao_TokenJaRevogado prova o mesmo cenário para uma sessão já
// rotacionada: reapresentar o refresh token antigo depois de uma rotação
// bem-sucedida deve falhar, nunca reexecutar o efeito (idempotência, mesmo
// espírito de TestVerificarEmail_LinkJaUsado).
func TestRenovarSessao_TokenJaRevogado(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "renovar-revogado@empresa.com", "senha-123456", true, true)
	_, refreshToken, _, err := EmitirSessao(db, testJWTSecret, usuarioID, "senha")
	if err != nil {
		t.Fatalf("EmitirSessao falhou: %v", err)
	}
	if _, _, _, err := RenovarSessao(db, testJWTSecret, refreshToken); err != nil {
		t.Fatalf("primeira renovação falhou: %v", err)
	}

	_, _, _, err = RenovarSessao(db, testJWTSecret, refreshToken)
	if !errors.Is(err, ErrSessaoInvalida) {
		t.Fatalf("segunda renovação: erro = %v, want ErrSessaoInvalida", err)
	}
}

// TestRenovarSessao_Concorrente prova o backstop de correção contra a
// corrida real fechada pela condição do UPDATE...RETURNING de RenovarSessao
// (`revogado_em IS NULL AND expira_em > now()`): duas chamadas concorrentes
// para o MESMO refresh token só podem ter exatamente uma vencedora. Mesmo
// padrão de TestVerificarEmail_Concorrente.
func TestRenovarSessao_Concorrente(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "renovar-concorrente@empresa.com", "senha-123456", true, true)
	_, refreshToken, _, err := EmitirSessao(db, testJWTSecret, usuarioID, "senha")
	if err != nil {
		t.Fatalf("EmitirSessao falhou: %v", err)
	}

	const n = 2
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			_, _, _, errs[i] = RenovarSessao(db, testJWTSecret, refreshToken)
		}(i)
	}
	close(start)
	wg.Wait()

	var successCount, invalidCount int
	for _, err := range errs {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, ErrSessaoInvalida):
			invalidCount++
		default:
			t.Fatalf("erro inesperado em execução concorrente: %v", err)
		}
	}
	if successCount != 1 {
		t.Errorf("successCount = %d, want 1", successCount)
	}
	if invalidCount != n-1 {
		t.Errorf("invalidCount = %d, want %d", invalidCount, n-1)
	}
}

// TestBuscarUsuarioSessao_Sucesso prova que o middleware resolve o usuário a
// partir do Postgres com todos os campos exigidos pelo contrato (AD-6).
func TestBuscarUsuarioSessao_Sucesso(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "buscar-sessao@empresa.com", "senha-123456", true, true)

	u, err := BuscarUsuarioSessao(db, usuarioID)
	if err != nil {
		t.Fatalf("BuscarUsuarioSessao retornou erro inesperado: %v", err)
	}
	if u.ID != usuarioID {
		t.Errorf("ID = %q, want %q", u.ID, usuarioID)
	}
	if u.Email != "buscar-sessao@empresa.com" {
		t.Errorf("Email = %q, want %q", u.Email, "buscar-sessao@empresa.com")
	}
	if u.Papel != "usuario" {
		t.Errorf("Papel = %q, want %q", u.Papel, "usuario")
	}
	if !u.Ativo {
		t.Error("Ativo = false, want true")
	}
}

// TestBuscarUsuarioSessao_NaoEncontrado prova o caso consumido pelo
// middleware para decidir SESSION_REVOKED: um id que não existe mais (ex.
// conta removida) retorna ErrUsuarioSessaoNaoEncontrado.
func TestBuscarUsuarioSessao_NaoEncontrado(t *testing.T) {
	db := testDB(t)

	_, err := BuscarUsuarioSessao(db, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrUsuarioSessaoNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrUsuarioSessaoNaoEncontrado", err)
	}
}

// TestBuscarUsuarioSessao_ContaDesativada prova que o campo Ativo reflete o
// estado atual do banco — a decisão de revogar acesso (SESSION_REVOKED) cabe
// ao middleware, que consulta este campo, nunca ao claim do JWT.
func TestBuscarUsuarioSessao_ContaDesativada(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "desativado-sessao@empresa.com", "senha-123456", false, true)

	u, err := BuscarUsuarioSessao(db, usuarioID)
	if err != nil {
		t.Fatalf("BuscarUsuarioSessao retornou erro inesperado: %v", err)
	}
	if u.Ativo {
		t.Error("Ativo = true, want false")
	}
}

// --- Story 1.6: recuperação de senha por e-mail ---------------------------

// TestValidarForcaSenha prova a política mínima de força de senha (Story 1.6,
// semente da Story 1.10): 8+ caracteres contados como runes, ao menos uma
// letra e um dígito, no máximo 72 bytes.
func TestValidarForcaSenha(t *testing.T) {
	casos := []struct {
		nome  string
		senha string
		ok    bool
	}{
		{"7 caracteres é curto demais", "abc1234", false},
		{"8 caracteres com letra e dígito passa", "abcd1234", true},
		{"sem dígito reprova", "abcdefgh", false},
		{"sem letra reprova", "12345678", false},
		{"73 bytes reprova (limite do bcrypt)", strings.Repeat("a", 72) + "1", false},
		{"72 bytes no limite passa", strings.Repeat("a", 71) + "1", true},
		{"acento conta como 1 rune: 8 runes / 13 bytes passa", "áéíóú123", true},
		{"string vazia reprova", "", false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			err := ValidarForcaSenha(c.senha)
			if c.ok && err != nil {
				t.Fatalf("ValidarForcaSenha(%q) = %v, want nil", c.senha, err)
			}
			if !c.ok && !errors.Is(err, ErrSenhaFraca) {
				t.Fatalf("ValidarForcaSenha(%q) = %v, want ErrSenhaFraca", c.senha, err)
			}
		})
	}
}

// lerTokenRedefinicao lê o token de redefinição (tipo='redefinicao_senha')
// gerado para um usuário; falha o teste se não houver exatamente um.
func lerTokenRedefinicao(t *testing.T, db *sql.DB, usuarioID string) string {
	t.Helper()
	var token string
	if err := db.QueryRow(
		`SELECT token FROM tokens_acao WHERE usuario_id = $1 AND tipo = 'redefinicao_senha'`,
		usuarioID,
	).Scan(&token); err != nil {
		t.Fatalf("falha ao ler token de redefinição: %v", err)
	}
	return token
}

// TestSolicitarRedefinicaoSenha_ContaExiste prova o cenário "Solicitação,
// conta existe" da I/O Matrix: um tokens_acao (redefinicao_senha, +30min,
// usado_em nulo) + um emails_pendentes (redefinicao_senha) gravados na mesma
// transação, com o link montado a partir de APP_URL.
func TestSolicitarRedefinicaoSenha_ContaExiste(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "reset-existe@empresa.com", "senha-123456", true, true)

	if err := SolicitarRedefinicaoSenha(db, testEmailCfg, "Reset-Existe@Empresa.com"); err != nil {
		t.Fatalf("SolicitarRedefinicaoSenha retornou erro inesperado: %v", err)
	}

	var token, tipo string
	var expiraEm time.Time
	var usadoEm sql.NullTime
	if err := db.QueryRow(
		`SELECT token, tipo, expira_em, usado_em FROM tokens_acao WHERE usuario_id = $1`, usuarioID,
	).Scan(&token, &tipo, &expiraEm, &usadoEm); err != nil {
		t.Fatalf("falha ao ler token gerado: %v", err)
	}
	if tipo != "redefinicao_senha" {
		t.Errorf("tipo do token = %q, want %q", tipo, "redefinicao_senha")
	}
	if usadoEm.Valid {
		t.Error("usado_em já preenchido em token recém-criado")
	}
	wantExpira := time.Now().UTC().Add(30 * time.Minute)
	if diff := wantExpira.Sub(expiraEm); diff < -time.Minute || diff > time.Minute {
		t.Errorf("expira_em = %v, want ~30min a partir de agora (diff=%v)", expiraEm, diff)
	}

	var destinatario, tipoEmail, variaveisRaw string
	if err := db.QueryRow(
		`SELECT destinatario, tipo, variaveis_json::text FROM emails_pendentes WHERE usuario_id = $1`, usuarioID,
	).Scan(&destinatario, &tipoEmail, &variaveisRaw); err != nil {
		t.Fatalf("falha ao ler linha de outbox: %v", err)
	}
	if destinatario != "reset-existe@empresa.com" {
		t.Errorf("destinatario = %q, want %q (normalizado)", destinatario, "reset-existe@empresa.com")
	}
	if tipoEmail != "redefinicao_senha" {
		t.Errorf("tipo do e-mail = %q, want %q", tipoEmail, "redefinicao_senha")
	}
	var variaveis map[string]any
	if err := json.Unmarshal([]byte(variaveisRaw), &variaveis); err != nil {
		t.Fatalf("variaveis_json inválido: %v", err)
	}
	wantLink := "http://test.local/redefinir-senha?token=" + token
	if got, _ := variaveis["link"].(string); got != wantLink {
		t.Errorf("link = %q, want %q", got, wantLink)
	}
}

// TestSolicitarRedefinicaoSenha_ContaNaoExiste prova o cenário "Solicitação,
// conta não existe": retorna nil e nenhuma linha é gravada.
func TestSolicitarRedefinicaoSenha_ContaNaoExiste(t *testing.T) {
	db := testDB(t)

	if err := SolicitarRedefinicaoSenha(db, testEmailCfg, "nunca-existiu@empresa.com"); err != nil {
		t.Fatalf("SolicitarRedefinicaoSenha retornou erro inesperado: %v", err)
	}
	if n := contarLinhas(t, db, "tokens_acao"); n != 0 {
		t.Errorf("count(tokens_acao) = %d, want 0", n)
	}
	if n := contarLinhas(t, db, "emails_pendentes"); n != 0 {
		t.Errorf("count(emails_pendentes) = %d, want 0", n)
	}
}

// TestSolicitarRedefinicaoSenha_ContaSoSSO prova que uma conta só-SSO
// (senha_hash nulo) NÃO é exceção — recebe token e e-mail normalmente.
func TestSolicitarRedefinicaoSenha_ContaSoSSO(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "reset-sso@empresa.com", "", true, true)

	if err := SolicitarRedefinicaoSenha(db, testEmailCfg, "reset-sso@empresa.com"); err != nil {
		t.Fatalf("SolicitarRedefinicaoSenha retornou erro inesperado: %v", err)
	}
	_ = lerTokenRedefinicao(t, db, usuarioID)
	if n := contarLinhas(t, db, "emails_pendentes"); n != 1 {
		t.Errorf("count(emails_pendentes) = %d, want 1", n)
	}
}

// TestSolicitarRedefinicaoSenha_EmailComMaiusculasEEspacos prova o match
// case-insensitive com normalização de bordas (normalizeEmail).
func TestSolicitarRedefinicaoSenha_EmailComMaiusculasEEspacos(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "reset-normaliza@empresa.com", "senha-123456", true, true)

	if err := SolicitarRedefinicaoSenha(db, testEmailCfg, "  Reset-Normaliza@Empresa.COM "); err != nil {
		t.Fatalf("SolicitarRedefinicaoSenha retornou erro inesperado: %v", err)
	}
	_ = lerTokenRedefinicao(t, db, usuarioID)
}

// TestSolicitarRedefinicaoSenha_InvalidaTokensAnteriores prova que emitir um
// novo link marca como usado qualquer token de redefinição anterior ainda não
// consumido — a conta nunca fica com vários links de redefinição válidos ao
// mesmo tempo (só o mais recente é aceito).
func TestSolicitarRedefinicaoSenha_InvalidaTokensAnteriores(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "reset-invalida-anterior@empresa.com", "senha-antiga1", true, true)

	if err := SolicitarRedefinicaoSenha(db, testEmailCfg, "reset-invalida-anterior@empresa.com"); err != nil {
		t.Fatalf("primeira solicitação falhou: %v", err)
	}
	var primeiroToken string
	if err := db.QueryRow(
		`SELECT token FROM tokens_acao WHERE usuario_id = $1 AND tipo = 'redefinicao_senha' ORDER BY criado_em`,
		usuarioID,
	).Scan(&primeiroToken); err != nil {
		t.Fatalf("falha ao ler primeiro token: %v", err)
	}

	if err := SolicitarRedefinicaoSenha(db, testEmailCfg, "reset-invalida-anterior@empresa.com"); err != nil {
		t.Fatalf("segunda solicitação falhou: %v", err)
	}

	var usadoPrimeiro sql.NullTime
	if err := db.QueryRow(`SELECT usado_em FROM tokens_acao WHERE token = $1`, primeiroToken).Scan(&usadoPrimeiro); err != nil {
		t.Fatalf("falha ao reler primeiro token: %v", err)
	}
	if !usadoPrimeiro.Valid {
		t.Error("primeiro token continua com usado_em nulo após a emissão de um novo link")
	}

	var segundoToken string
	if err := db.QueryRow(
		`SELECT token FROM tokens_acao WHERE usuario_id = $1 AND tipo = 'redefinicao_senha' AND usado_em IS NULL`,
		usuarioID,
	).Scan(&segundoToken); err != nil {
		t.Fatalf("falha ao ler segundo token (único não usado): %v", err)
	}
	if segundoToken == primeiroToken {
		t.Fatal("segundo token igual ao primeiro")
	}

	if err := ValidarTokenRedefinicao(db, primeiroToken); !errors.Is(err, ErrTokenExpirado) {
		t.Errorf("ValidarTokenRedefinicao(primeiro) = %v, want ErrTokenExpirado", err)
	}
	if err := ValidarTokenRedefinicao(db, segundoToken); err != nil {
		t.Errorf("ValidarTokenRedefinicao(segundo) = %v, want nil", err)
	}
	if err := RedefinirSenha(db, primeiroToken, "nova-senha1"); !errors.Is(err, ErrTokenExpirado) {
		t.Errorf("RedefinirSenha(primeiro) = %v, want ErrTokenExpirado", err)
	}
	if err := RedefinirSenha(db, segundoToken, "nova-senha1"); err != nil {
		t.Errorf("RedefinirSenha(segundo) = %v, want nil", err)
	}
}

// TestValidarTokenRedefinicao cobre os três desfechos: válido (sem consumir),
// inexistente e expirado/usado.
func TestValidarTokenRedefinicao(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "valida-token@empresa.com", "senha-123456", true, true)
	if err := SolicitarRedefinicaoSenha(db, testEmailCfg, "valida-token@empresa.com"); err != nil {
		t.Fatalf("SolicitarRedefinicaoSenha falhou: %v", err)
	}
	token := lerTokenRedefinicao(t, db, usuarioID)

	if err := ValidarTokenRedefinicao(db, token); err != nil {
		t.Fatalf("ValidarTokenRedefinicao(token válido) = %v, want nil", err)
	}
	var usadoEm sql.NullTime
	if err := db.QueryRow(`SELECT usado_em FROM tokens_acao WHERE token = $1`, token).Scan(&usadoEm); err != nil {
		t.Fatalf("falha ao reler token: %v", err)
	}
	if usadoEm.Valid {
		t.Error("usado_em preenchido — ValidarTokenRedefinicao não pode consumir o token")
	}

	if err := ValidarTokenRedefinicao(db, "token-que-nunca-existiu"); !errors.Is(err, ErrTokenNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrTokenNaoEncontrado", err)
	}

	// Token usado mas AINDA não expirado: fixa o sub-ramo `usadoEm.Valid` de
	// ValidarTokenRedefinicao isolado da expiração (linha "GET com token
	// vencido OU usado_em preenchido" da I/O Matrix).
	if _, err := db.Exec(`UPDATE tokens_acao SET usado_em = now() WHERE token = $1`, token); err != nil {
		t.Fatalf("falha ao marcar token como usado: %v", err)
	}
	if err := ValidarTokenRedefinicao(db, token); !errors.Is(err, ErrTokenExpirado) {
		t.Fatalf("token usado (não expirado): erro = %v, want ErrTokenExpirado", err)
	}

	if _, err := db.Exec(`UPDATE tokens_acao SET expira_em = now() - interval '1 minute' WHERE token = $1`, token); err != nil {
		t.Fatalf("falha ao forçar expiração: %v", err)
	}
	if err := ValidarTokenRedefinicao(db, token); !errors.Is(err, ErrTokenExpirado) {
		t.Fatalf("erro = %v, want ErrTokenExpirado", err)
	}
}

// TestValidarTokenRedefinicao_TokenDeVerificacaoEmailIsolado prova o
// isolamento entre fluxos (AD-18): um token válido de tipo='verificacao_email'
// é invisível para o fluxo de redefinição.
func TestValidarTokenRedefinicao_TokenDeVerificacaoEmailIsolado(t *testing.T) {
	db := testDB(t)
	_, tokenVerificacao := criarUsuarioComToken(t, db, "isola-verificacao@empresa.com")

	if err := ValidarTokenRedefinicao(db, tokenVerificacao); !errors.Is(err, ErrTokenNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrTokenNaoEncontrado — fluxos isolados", err)
	}
}

// prepararRedefinicao cria uma conta ativa/verificada com senha conhecida e
// devolve o id + um token de redefinição válido.
func prepararRedefinicao(t *testing.T, db *sql.DB, email string) (usuarioID, token string) {
	t.Helper()
	usuarioID = criarUsuarioParaLogin(t, db, email, "senha-antiga1", true, true)
	if err := SolicitarRedefinicaoSenha(db, testEmailCfg, email); err != nil {
		t.Fatalf("SolicitarRedefinicaoSenha falhou: %v", err)
	}
	return usuarioID, lerTokenRedefinicao(t, db, usuarioID)
}

// TestRedefinirSenha_Sucesso prova o caminho feliz: senha_hash passa a casar
// a nova senha (não a antiga), o token fica usado e TODAS as sessões da conta
// são revogadas — sessões de outras contas ficam intactas.
func TestRedefinirSenha_Sucesso(t *testing.T) {
	db := testDB(t)
	usuarioID, token := prepararRedefinicao(t, db, "redefine-ok@empresa.com")

	// Sessão da própria conta (deve ser revogada) e de outra conta (intacta).
	_, _, _, err := EmitirSessao(db, testJWTSecret, usuarioID, "senha")
	if err != nil {
		t.Fatalf("EmitirSessao falhou: %v", err)
	}
	outroID := criarUsuarioParaLogin(t, db, "outra-conta@empresa.com", "senha-123456", true, true)
	_, outroRefresh, _, err := EmitirSessao(db, testJWTSecret, outroID, "senha")
	if err != nil {
		t.Fatalf("EmitirSessao (outra conta) falhou: %v", err)
	}

	if err := RedefinirSenha(db, token, "nova-senha1"); err != nil {
		t.Fatalf("RedefinirSenha retornou erro inesperado: %v", err)
	}

	var senhaHash string
	if err := db.QueryRow(`SELECT senha_hash FROM usuarios WHERE id = $1`, usuarioID).Scan(&senhaHash); err != nil {
		t.Fatalf("falha ao reler senha_hash: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(senhaHash), []byte("nova-senha1")); err != nil {
		t.Errorf("senha_hash não casa a nova senha: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(senhaHash), []byte("senha-antiga1")) == nil {
		t.Error("senha_hash ainda casa a senha antiga")
	}

	var usadoEm sql.NullTime
	if err := db.QueryRow(`SELECT usado_em FROM tokens_acao WHERE token = $1`, token).Scan(&usadoEm); err != nil {
		t.Fatalf("falha ao reler token: %v", err)
	}
	if !usadoEm.Valid {
		t.Error("usado_em não preenchido após redefinição bem-sucedida")
	}

	var ativas int
	if err := db.QueryRow(`SELECT count(*) FROM sessoes WHERE usuario_id = $1 AND revogado_em IS NULL`, usuarioID).Scan(&ativas); err != nil {
		t.Fatalf("falha ao contar sessões ativas: %v", err)
	}
	if ativas != 0 {
		t.Errorf("sessões ativas da conta = %d, want 0", ativas)
	}

	// A sessão da outra conta continua utilizável.
	if _, _, _, err := RenovarSessao(db, testJWTSecret, outroRefresh); err != nil {
		t.Errorf("sessão de outra conta foi afetada: %v", err)
	}
}

// TestRedefinirSenha_SenhaFracaNaoConsomeToken prova o cenário "Redefinição,
// senha fraca" da I/O Matrix: ErrSenhaFraca, token permanece válido.
func TestRedefinirSenha_SenhaFracaNaoConsomeToken(t *testing.T) {
	db := testDB(t)
	usuarioID, token := prepararRedefinicao(t, db, "redefine-fraca@empresa.com")

	if err := RedefinirSenha(db, token, "curta1"); !errors.Is(err, ErrSenhaFraca) {
		t.Fatalf("erro = %v, want ErrSenhaFraca", err)
	}

	var usadoEm sql.NullTime
	var senhaHash string
	if err := db.QueryRow(`SELECT usado_em FROM tokens_acao WHERE token = $1`, token).Scan(&usadoEm); err != nil {
		t.Fatalf("falha ao reler token: %v", err)
	}
	if usadoEm.Valid {
		t.Error("usado_em preenchido — senha fraca não pode consumir o token")
	}
	if err := db.QueryRow(`SELECT senha_hash FROM usuarios WHERE id = $1`, usuarioID).Scan(&senhaHash); err != nil {
		t.Fatalf("falha ao reler senha_hash: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(senhaHash), []byte("senha-antiga1")) != nil {
		t.Error("senha_hash mudou apesar da senha fraca")
	}
	// O mesmo link continua válido para nova tentativa.
	if err := RedefinirSenha(db, token, "nova-senha1"); err != nil {
		t.Fatalf("segunda tentativa com senha forte falhou: %v", err)
	}
}

// TestRedefinirSenha_TokenInexistente prova o cenário "Redefinição, token
// inexistente" da I/O Matrix.
func TestRedefinirSenha_TokenInexistente(t *testing.T) {
	db := testDB(t)

	if err := RedefinirSenha(db, "token-que-nunca-existiu", "nova-senha1"); !errors.Is(err, ErrTokenNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrTokenNaoEncontrado", err)
	}
}

// TestRedefinirSenha_TokenExpirado prova o cenário "Redefinição, token
// expirado/usado" da I/O Matrix — e que o erro não deixa nenhum efeito
// colateral: senha_hash intacto (bcrypt ainda casa a senha antiga) e
// usado_em do token ainda nulo.
func TestRedefinirSenha_TokenExpirado(t *testing.T) {
	db := testDB(t)
	usuarioID, token := prepararRedefinicao(t, db, "redefine-expirado@empresa.com")
	if _, err := db.Exec(`UPDATE tokens_acao SET expira_em = now() - interval '1 minute' WHERE token = $1`, token); err != nil {
		t.Fatalf("falha ao forçar expiração: %v", err)
	}

	if err := RedefinirSenha(db, token, "nova-senha1"); !errors.Is(err, ErrTokenExpirado) {
		t.Fatalf("erro = %v, want ErrTokenExpirado", err)
	}

	var senhaHash string
	if err := db.QueryRow(`SELECT senha_hash FROM usuarios WHERE id = $1`, usuarioID).Scan(&senhaHash); err != nil {
		t.Fatalf("falha ao reler senha_hash: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(senhaHash), []byte("senha-antiga1")); err != nil {
		t.Errorf("senha_hash mudou apesar do token expirado: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(senhaHash), []byte("nova-senha1")) == nil {
		t.Error("senha_hash passou a casar a nova senha apesar do token expirado")
	}

	var usadoEm sql.NullTime
	if err := db.QueryRow(`SELECT usado_em FROM tokens_acao WHERE token = $1`, token).Scan(&usadoEm); err != nil {
		t.Fatalf("falha ao reler token: %v", err)
	}
	if usadoEm.Valid {
		t.Error("usado_em preenchido — token expirado não pode ser consumido")
	}
}

// TestRedefinirSenha_ReusoDoMesmoToken prova o cenário "Redefinição, reuso do
// mesmo token": o segundo POST com o token já consumido -> ErrTokenExpirado
// (o UPDATE guardado afeta 0 linhas).
func TestRedefinirSenha_ReusoDoMesmoToken(t *testing.T) {
	db := testDB(t)
	_, token := prepararRedefinicao(t, db, "redefine-reuso@empresa.com")

	if err := RedefinirSenha(db, token, "nova-senha1"); err != nil {
		t.Fatalf("primeira redefinição falhou: %v", err)
	}
	if err := RedefinirSenha(db, token, "outra-senha2"); !errors.Is(err, ErrTokenExpirado) {
		t.Fatalf("segunda redefinição: erro = %v, want ErrTokenExpirado", err)
	}
}

// TestRedefinirSenha_TokenDeVerificacaoEmailIsolado prova o isolamento entre
// fluxos (AD-18): um token válido de tipo='verificacao_email' -> NOT_FOUND.
func TestRedefinirSenha_TokenDeVerificacaoEmailIsolado(t *testing.T) {
	db := testDB(t)
	_, tokenVerificacao := criarUsuarioComToken(t, db, "redefine-isola@empresa.com")

	if err := RedefinirSenha(db, tokenVerificacao, "nova-senha1"); !errors.Is(err, ErrTokenNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrTokenNaoEncontrado — fluxos isolados", err)
	}
}

// TestRedefinirSenha_ContaSoSSOPassaAAutenticarPorSenha prova o cenário
// "Redefinição de conta só-SSO" da I/O Matrix: senha_hash era nulo; após o
// fluxo, Login com a nova senha autentica.
func TestRedefinirSenha_ContaSoSSOPassaAAutenticarPorSenha(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "sso-ganha-senha@empresa.com", "", true, true)
	if err := SolicitarRedefinicaoSenha(db, testEmailCfg, "sso-ganha-senha@empresa.com"); err != nil {
		t.Fatalf("SolicitarRedefinicaoSenha falhou: %v", err)
	}
	token := lerTokenRedefinicao(t, db, usuarioID)

	if err := RedefinirSenha(db, token, "nova-senha1"); err != nil {
		t.Fatalf("RedefinirSenha falhou: %v", err)
	}

	got, err := Login(db, "sso-ganha-senha@empresa.com", "nova-senha1")
	if err != nil {
		t.Fatalf("Login com a nova senha falhou: %v", err)
	}
	if got != usuarioID {
		t.Errorf("Login devolveu id %q, want %q", got, usuarioID)
	}

	// email_verificado e ativo intactos.
	var emailVerificado, ativo bool
	if err := db.QueryRow(`SELECT email_verificado, ativo FROM usuarios WHERE id = $1`, usuarioID).Scan(&emailVerificado, &ativo); err != nil {
		t.Fatalf("falha ao reler usuario: %v", err)
	}
	if !emailVerificado || !ativo {
		t.Errorf("email_verificado=%v ativo=%v, want ambos true (nenhum efeito colateral)", emailVerificado, ativo)
	}
}

// TestRedefinirSenha_Concorrente prova o backstop contra a corrida fechada
// pela condição repetida no UPDATE de marcarUsado
// (`usado_em IS NULL AND expira_em > now()`): duas chamadas concorrentes para
// o MESMO token só podem ter uma vencedora. Mesmo padrão de
// TestVerificarEmail_Concorrente.
func TestRedefinirSenha_Concorrente(t *testing.T) {
	db := testDB(t)
	_, token := prepararRedefinicao(t, db, "redefine-concorrente@empresa.com")

	const n = 2
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = RedefinirSenha(db, token, "nova-senha1")
		}(i)
	}
	close(start)
	wg.Wait()

	var ok, expirado int
	for _, err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrTokenExpirado):
			expirado++
		default:
			t.Fatalf("erro inesperado em execução concorrente: %v", err)
		}
	}
	if ok != 1 || expirado != n-1 {
		t.Errorf("ok=%d expirado=%d, want ok=1 expirado=%d", ok, expirado, n-1)
	}
}

// --- Story 1.10: bloqueio de conta por excesso de tentativas de login ---

// lerBloqueioLogin devolve o contador de falhas e o instante de bloqueio
// persistidos para uma conta.
func lerBloqueioLogin(t *testing.T, db *sql.DB, usuarioID string) (tentativas int, bloqueadoAte sql.NullTime) {
	t.Helper()
	if err := db.QueryRow(
		`SELECT tentativas_login_falhas, bloqueado_ate FROM usuarios WHERE id = $1`, usuarioID,
	).Scan(&tentativas, &bloqueadoAte); err != nil {
		t.Fatalf("falha ao ler estado de bloqueio: %v", err)
	}
	return tentativas, bloqueadoAte
}

// TestLogin_BloqueiaNaQuintaFalhaERecusaSexta prova a linha "5ª falha
// consecutiva" + "6ª tentativa (bloqueada), senha correta" da I/O Matrix: as 5
// primeiras falhas retornam ErrCredenciaisInvalidas (a 5ª grava
// bloqueado_ate ~15min à frente com tentativas=5); a 6ª tentativa, MESMO com a
// senha correta, retorna ErrContaBloqueada sem tocar contador/prazo.
func TestLogin_BloqueiaNaQuintaFalhaERecusaSexta(t *testing.T) {
	db := testDB(t)
	id := criarUsuarioParaLogin(t, db, "brute-force@empresa.com", "senha-correta1", true, true)

	for i := 1; i <= 5; i++ {
		_, err := Login(db, "brute-force@empresa.com", "senha-errada")
		if !errors.Is(err, ErrCredenciaisInvalidas) {
			t.Fatalf("falha %d: erro = %v, want ErrCredenciaisInvalidas", i, err)
		}
		tentativas, bloqueadoAte := lerBloqueioLogin(t, db, id)
		if tentativas != i {
			t.Fatalf("após falha %d: tentativas = %d, want %d", i, tentativas, i)
		}
		if i < 5 && bloqueadoAte.Valid {
			t.Fatalf("após falha %d: bloqueado_ate preenchido antes do limite", i)
		}
		if i == 5 {
			if !bloqueadoAte.Valid {
				t.Fatal("após a 5ª falha: bloqueado_ate deveria estar preenchido")
			}
			want := time.Now().UTC().Add(duracaoBloqueioLogin)
			if diff := want.Sub(bloqueadoAte.Time); diff < -time.Minute || diff > time.Minute {
				t.Errorf("bloqueado_ate = %v, want ~%v (diff=%v)", bloqueadoAte.Time, want, diff)
			}
			// AC1 fixa "~15 min à frente". A checagem acima só prova que
			// bloqueado_ate bate com duracaoBloqueioLogin seja qual for o valor
			// da constante; este limite literal pega uma alteração silenciosa da
			// janela de bloqueio (molde da checagem de ~30 min em
			// TestSolicitarRedefinicaoSenha_ContaExiste).
			if adiante := time.Until(bloqueadoAte.Time); adiante < 13*time.Minute || adiante > 17*time.Minute {
				t.Errorf("bloqueado_ate está %v à frente, want ~15 min (AC1)", adiante)
			}
		}
	}

	// 6ª tentativa com a senha CORRETA: recusada como bloqueada, sem alterar
	// contador nem prazo.
	tentativasAntes, bloqueioAntes := lerBloqueioLogin(t, db, id)
	_, err := Login(db, "brute-force@empresa.com", "senha-correta1")
	if !errors.Is(err, ErrContaBloqueada) {
		t.Fatalf("6ª tentativa: erro = %v, want ErrContaBloqueada", err)
	}
	tentativasDepois, bloqueioDepois := lerBloqueioLogin(t, db, id)
	if tentativasDepois != tentativasAntes {
		t.Errorf("tentativas mudou de %d para %d na tentativa bloqueada", tentativasAntes, tentativasDepois)
	}
	if !bloqueioDepois.Time.Equal(bloqueioAntes.Time) {
		t.Errorf("bloqueado_ate estendido de %v para %v na tentativa bloqueada", bloqueioAntes.Time, bloqueioDepois.Time)
	}
}

// TestLogin_SucessoAntesDoLimiteZeraContador prova a linha "Sucesso antes do
// limite" da I/O Matrix: com tentativas=3, um login válido devolve o id e
// zera tentativas/bloqueado_ate.
func TestLogin_SucessoAntesDoLimiteZeraContador(t *testing.T) {
	db := testDB(t)
	id := criarUsuarioParaLogin(t, db, "reset-sucesso@empresa.com", "senha-correta1", true, true)

	for i := 0; i < 3; i++ {
		if _, err := Login(db, "reset-sucesso@empresa.com", "errada"); !errors.Is(err, ErrCredenciaisInvalidas) {
			t.Fatalf("falha %d: erro = %v", i, err)
		}
	}
	if tentativas, _ := lerBloqueioLogin(t, db, id); tentativas != 3 {
		t.Fatalf("pré-condição: tentativas = %d, want 3", tentativas)
	}

	got, err := Login(db, "reset-sucesso@empresa.com", "senha-correta1")
	if err != nil {
		t.Fatalf("Login válido retornou erro: %v", err)
	}
	if got != id {
		t.Errorf("usuarioID = %q, want %q", got, id)
	}
	tentativas, bloqueadoAte := lerBloqueioLogin(t, db, id)
	if tentativas != 0 || bloqueadoAte.Valid {
		t.Errorf("após sucesso: tentativas=%d bloqueado_ate.Valid=%v, want 0 / false", tentativas, bloqueadoAte.Valid)
	}
}

// TestLogin_BloqueioExpiradoDestrava prova as linhas "Bloqueio expirado +
// senha correta" e "Bloqueio expirado + senha errada" da I/O Matrix. O
// bloqueio é forçado a expirar via UPDATE direto (evita time.Sleep).
func TestLogin_BloqueioExpiradoDestrava(t *testing.T) {
	t.Run("senha correta destrava e zera", func(t *testing.T) {
		db := testDB(t)
		id := criarUsuarioParaLogin(t, db, "expira-ok@empresa.com", "senha-correta1", true, true)
		for i := 0; i < 5; i++ {
			_, _ = Login(db, "expira-ok@empresa.com", "errada")
		}
		if _, err := db.Exec(`UPDATE usuarios SET bloqueado_ate = now() - interval '1 minute' WHERE id = $1`, id); err != nil {
			t.Fatalf("falha ao expirar bloqueio: %v", err)
		}

		got, err := Login(db, "expira-ok@empresa.com", "senha-correta1")
		if err != nil || got != id {
			t.Fatalf("Login após expiração: id=%q err=%v, want id=%q nil", got, err, id)
		}
		tentativas, bloqueadoAte := lerBloqueioLogin(t, db, id)
		if tentativas != 0 || bloqueadoAte.Valid {
			t.Errorf("tentativas=%d bloqueado_ate.Valid=%v, want 0 / false", tentativas, bloqueadoAte.Valid)
		}
	})

	t.Run("senha errada inicia novo streak", func(t *testing.T) {
		db := testDB(t)
		id := criarUsuarioParaLogin(t, db, "expira-erro@empresa.com", "senha-correta1", true, true)
		for i := 0; i < 5; i++ {
			_, _ = Login(db, "expira-erro@empresa.com", "errada")
		}
		if _, err := db.Exec(`UPDATE usuarios SET bloqueado_ate = now() - interval '1 minute' WHERE id = $1`, id); err != nil {
			t.Fatalf("falha ao expirar bloqueio: %v", err)
		}

		if _, err := Login(db, "expira-erro@empresa.com", "ainda-errada"); !errors.Is(err, ErrCredenciaisInvalidas) {
			t.Fatalf("erro = %v, want ErrCredenciaisInvalidas", err)
		}
		tentativas, bloqueadoAte := lerBloqueioLogin(t, db, id)
		if tentativas != 1 || bloqueadoAte.Valid {
			t.Errorf("tentativas=%d bloqueado_ate.Valid=%v, want 1 / false (streak novo)", tentativas, bloqueadoAte.Valid)
		}
	})
}

// TestLogin_FalhasNaoConsecutivasNuncaBloqueiam prova a linha "Falhas não
// consecutivas" da I/O Matrix: 3 falhas, 1 sucesso (zera), 2 falhas — nunca
// bloqueia; contador final = 2.
func TestLogin_FalhasNaoConsecutivasNuncaBloqueiam(t *testing.T) {
	db := testDB(t)
	id := criarUsuarioParaLogin(t, db, "nao-consecutivas@empresa.com", "senha-correta1", true, true)

	for i := 0; i < 3; i++ {
		if _, err := Login(db, "nao-consecutivas@empresa.com", "errada"); !errors.Is(err, ErrCredenciaisInvalidas) {
			t.Fatalf("falha %d: erro = %v", i, err)
		}
	}
	if _, err := Login(db, "nao-consecutivas@empresa.com", "senha-correta1"); err != nil {
		t.Fatalf("sucesso intermediário retornou erro: %v", err)
	}
	if tentativas, _ := lerBloqueioLogin(t, db, id); tentativas != 0 {
		t.Fatalf("após sucesso: tentativas = %d, want 0", tentativas)
	}
	for i := 0; i < 2; i++ {
		if _, err := Login(db, "nao-consecutivas@empresa.com", "errada"); !errors.Is(err, ErrCredenciaisInvalidas) {
			t.Fatalf("falha pós-sucesso %d: erro = %v", i, err)
		}
	}
	tentativas, bloqueadoAte := lerBloqueioLogin(t, db, id)
	if tentativas != 2 || bloqueadoAte.Valid {
		t.Errorf("tentativas=%d bloqueado_ate.Valid=%v, want 2 / false", tentativas, bloqueadoAte.Valid)
	}
}

// TestLogin_EmailInexistenteMarteladoNaoBloqueia prova a linha "E-mail
// inexistente martelado" da I/O Matrix: N tentativas contra um e-mail sem
// linha sempre devolvem ErrCredenciaisInvalidas, sem panic e sem criar linha.
func TestLogin_EmailInexistenteMarteladoNaoBloqueia(t *testing.T) {
	db := testDB(t)
	for i := 0; i < 8; i++ {
		if _, err := Login(db, "fantasma@empresa.com", "qualquer"); !errors.Is(err, ErrCredenciaisInvalidas) {
			t.Fatalf("tentativa %d: erro = %v, want ErrCredenciaisInvalidas", i, err)
		}
	}
	if n := contarLinhas(t, db, "usuarios"); n != 0 {
		t.Errorf("count(usuarios) = %d, want 0 — nenhuma linha criada por login de e-mail inexistente", n)
	}
}

// TestLogin_SenhaCorretaEmContaNaoElegivelNaoIncrementa prova a linha "Conta
// desativada/não verificada, senha correta" da I/O Matrix: a senha certa numa
// conta inativa / não verificada não é sinal de força bruta e não mexe no
// contador.
func TestLogin_SenhaCorretaEmContaNaoElegivelNaoIncrementa(t *testing.T) {
	casos := []struct {
		nome            string
		ativo           bool
		emailVerificado bool
	}{
		{"conta desativada", false, true},
		{"e-mail não verificado", true, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			db := testDB(t)
			email := "nao-elegivel@empresa.com"
			id := criarUsuarioParaLogin(t, db, email, "senha-correta1", c.ativo, c.emailVerificado)

			if _, err := Login(db, email, "senha-correta1"); !errors.Is(err, ErrCredenciaisInvalidas) {
				t.Fatalf("erro = %v, want ErrCredenciaisInvalidas", err)
			}
			tentativas, bloqueadoAte := lerBloqueioLogin(t, db, id)
			if tentativas != 0 || bloqueadoAte.Valid {
				t.Errorf("tentativas=%d bloqueado_ate.Valid=%v, want 0 / false", tentativas, bloqueadoAte.Valid)
			}
		})
	}
}

// TestLogin_SenhaErradaEmContaNaoElegivelIncrementa fixa o outro lado da regra
// de "Boundaries -> Always": o guard de incremento é `senhaHash.Valid &&
// !senhaCorreta`, SEM condição de `ativo`/`emailVerificado`. Uma senha
// REALMENTE errada contra uma conta com hash é sinal de força bruta mesmo que
// a conta esteja desativada ou não verificada — e cinco delas bloqueiam. Sem
// este teste, um refactor que apertasse o guard para exigir também
// `ativo && emailVerificado` passaria em toda a suíte existente.
func TestLogin_SenhaErradaEmContaNaoElegivelIncrementa(t *testing.T) {
	casos := []struct {
		nome            string
		ativo           bool
		emailVerificado bool
	}{
		{"conta desativada", false, true},
		{"e-mail não verificado", true, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			db := testDB(t)
			email := "nao-elegivel-brute@empresa.com"
			id := criarUsuarioParaLogin(t, db, email, "senha-correta1", c.ativo, c.emailVerificado)

			if _, err := Login(db, email, "senha-errada"); !errors.Is(err, ErrCredenciaisInvalidas) {
				t.Fatalf("erro = %v, want ErrCredenciaisInvalidas", err)
			}
			if tentativas, _ := lerBloqueioLogin(t, db, id); tentativas != 1 {
				t.Fatalf("após 1 senha errada: tentativas = %d, want 1", tentativas)
			}

			for i := 0; i < 4; i++ {
				_, _ = Login(db, email, "senha-errada")
			}
			tentativas, bloqueadoAte := lerBloqueioLogin(t, db, id)
			if tentativas != 5 || !bloqueadoAte.Valid {
				t.Errorf("após 5 senhas erradas: tentativas=%d bloqueado_ate.Valid=%v, want 5 / true", tentativas, bloqueadoAte.Valid)
			}
		})
	}
}

// TestCadastrar_SenhaFraca prova a linha "Cadastro com senha fraca" da I/O
// Matrix: senha curta ou sem dígito reprova com ErrSenhaFraca e ZERO linhas
// são escritas em usuarios/tokens_acao/emails_pendentes.
func TestCadastrar_SenhaFraca(t *testing.T) {
	for nome, senha := range map[string]string{"curta (<8)": "abc", "sem dígito": "abcdefgh"} {
		t.Run(nome, func(t *testing.T) {
			db := testDB(t)
			_, err := Cadastrar(db, testEmailCfg, "Fulano", "fraca@empresa.com", senha)
			if !errors.Is(err, ErrSenhaFraca) {
				t.Fatalf("erro = %v, want ErrSenhaFraca", err)
			}
			for _, tabela := range []string{"usuarios", "tokens_acao", "emails_pendentes"} {
				if n := contarLinhas(t, db, tabela); n != 0 {
					t.Errorf("count(%s) = %d, want 0 — senha fraca não pode escrever nada", tabela, n)
				}
			}
		})
	}
}

// TestCadastrar_SenhaForteCriaConta prova a linha "Cadastro com senha forte"
// da I/O Matrix: senha "abcd1234" cria a conta como usuario /
// email_verificado=false.
func TestCadastrar_SenhaForteCriaConta(t *testing.T) {
	db := testDB(t)
	id, err := Cadastrar(db, testEmailCfg, "Fulano", "forte@empresa.com", "abcd1234")
	if err != nil {
		t.Fatalf("Cadastrar retornou erro inesperado: %v", err)
	}
	var papel string
	var emailVerificado bool
	if err := db.QueryRow(`SELECT papel, email_verificado FROM usuarios WHERE id = $1`, id).Scan(&papel, &emailVerificado); err != nil {
		t.Fatalf("falha ao reler usuario: %v", err)
	}
	if papel != "usuario" || emailVerificado {
		t.Errorf("papel=%q email_verificado=%v, want usuario / false", papel, emailVerificado)
	}
}

// TestLogin_FalhasConcorrentes prova que o contador de força bruta é
// incrementado no banco de forma atômica (`tentativas_login_falhas =
// tentativas_login_falhas + 1`), não por um read-modify-write em Go: dezenas
// de tentativas simultâneas com a senha errada não podem "perder" incrementos
// e deixar a conta destravada. Molde de TestRedefinirSenha_Concorrente.
func TestLogin_FalhasConcorrentes(t *testing.T) {
	db := testDB(t)
	id := criarUsuarioParaLogin(t, db, "brute-concorrente@empresa.com", "senha-correta1", true, true)

	const n = 20
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			_, results[i] = Login(db, "brute-concorrente@empresa.com", "senha-errada")
		}(i)
	}
	close(start)
	wg.Wait()

	// Toda tentativa falhou como credencial inválida ou como conta já bloqueada
	// — nunca sucesso, nunca panic, nunca outro erro.
	for i, err := range results {
		if !errors.Is(err, ErrCredenciaisInvalidas) && !errors.Is(err, ErrContaBloqueada) {
			t.Fatalf("goroutine %d: erro = %v, want ErrCredenciaisInvalidas ou ErrContaBloqueada", i, err)
		}
	}

	tentativas, bloqueadoAte := lerBloqueioLogin(t, db, id)
	if tentativas < maxTentativasLogin {
		t.Errorf("tentativas_login_falhas = %d, want >= %d — incrementos concorrentes foram perdidos", tentativas, maxTentativasLogin)
	}
	if !bloqueadoAte.Valid || !bloqueadoAte.Time.After(time.Now()) {
		t.Errorf("bloqueado_ate = %v, want um instante no futuro — a conta deveria ter travado", bloqueadoAte)
	}
}

// ===== Story 1.11: MFA obrigatório para papéis administrativos =====

// criarUsuarioComMFA insere uma conta ativa/verificada com um segredo TOTP
// real já configurado (`mfa_habilitado=true`) — molde de criarUsuarioParaLogin
// com o segredo devolvido para o chamador computar códigos válidos via
// gerarCodigoHOTP.
func criarUsuarioComMFA(t *testing.T, db *sql.DB, email, senha string) (usuarioID, segredo string) {
	t.Helper()
	usuarioID = criarUsuarioParaLogin(t, db, email, senha, true, true)
	segredo, err := GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP: %v", err)
	}
	if _, err := db.Exec(`UPDATE usuarios SET mfa_habilitado = true, mfa_secret = $1 WHERE id = $2`, segredo, usuarioID); err != nil {
		t.Fatalf("falha ao habilitar MFA de teste: %v", err)
	}
	return usuarioID, segredo
}

// codigoTOTPValidoAgora gera um código TOTP correto para `segredo` no
// instante atual, usando o mesmo gerarCodigoHOTP não-exportado de totp.go —
// possível aqui porque este arquivo é do mesmo pacote `services`.
func codigoTOTPValidoAgora(t *testing.T, segredo string) string {
	t.Helper()
	contador := uint64(time.Now().UTC().Unix()) / 30
	codigo, err := gerarCodigoHOTP(segredo, contador)
	if err != nil {
		t.Fatalf("gerarCodigoHOTP: %v", err)
	}
	return codigo
}

// TestIniciarLoginMFA_GeraTokenTipoMfaLogin prova que IniciarLoginMFA grava
// um token opaco em tokens_acao com tipo='mfa_login' e expira_em ~5min à
// frente — o mesmo molde de token de uso único já usado por verificação de
// e-mail/redefinição de senha.
func TestIniciarLoginMFA_GeraTokenTipoMfaLogin(t *testing.T) {
	db := testDB(t)
	usuarioID, _ := criarUsuarioComMFA(t, db, "mfa-iniciar@empresa.com", "senha-123456")

	token, err := IniciarLoginMFA(db, usuarioID)
	if err != nil {
		t.Fatalf("IniciarLoginMFA retornou erro inesperado: %v", err)
	}
	if token == "" {
		t.Fatal("token vazio")
	}

	var tipo string
	var expiraEm time.Time
	var usadoEm sql.NullTime
	if err := db.QueryRow(`SELECT tipo, expira_em, usado_em FROM tokens_acao WHERE token = $1`, token).
		Scan(&tipo, &expiraEm, &usadoEm); err != nil {
		t.Fatalf("falha ao ler token persistido: %v", err)
	}
	if tipo != "mfa_login" {
		t.Errorf("tipo = %q, want %q", tipo, "mfa_login")
	}
	if usadoEm.Valid {
		t.Error("usado_em já preenchido em token recém-emitido")
	}
	wantExpira := time.Now().UTC().Add(mfaLoginTokenExpiracao)
	if diff := wantExpira.Sub(expiraEm); diff < -time.Minute || diff > time.Minute {
		t.Errorf("expira_em = %v, want ~5min a partir de agora (diff=%v)", expiraEm, diff)
	}
}

// TestIniciarLoginMFA_InvalidaTokenAnterior prova o precedente já usado por
// SolicitarRedefinicaoSenha (Story 1.6): um novo login por MFA da mesma conta
// invalida (usado_em) qualquer token de mfa_login anterior ainda não
// consumido — nunca mais de um token válido ao mesmo tempo para a mesma
// conta.
func TestIniciarLoginMFA_InvalidaTokenAnterior(t *testing.T) {
	db := testDB(t)
	usuarioID, _ := criarUsuarioComMFA(t, db, "mfa-iniciar-invalida-anterior@empresa.com", "senha-123456")

	token1, err := IniciarLoginMFA(db, usuarioID)
	if err != nil {
		t.Fatalf("IniciarLoginMFA (1) falhou: %v", err)
	}
	token2, err := IniciarLoginMFA(db, usuarioID)
	if err != nil {
		t.Fatalf("IniciarLoginMFA (2) falhou: %v", err)
	}
	if token1 == token2 {
		t.Fatal("tokens de logins consecutivos idênticos")
	}

	var usadoEm1 sql.NullTime
	if err := db.QueryRow(`SELECT usado_em FROM tokens_acao WHERE token = $1`, token1).Scan(&usadoEm1); err != nil {
		t.Fatalf("falha ao reler token1: %v", err)
	}
	if !usadoEm1.Valid {
		t.Error("token1 (anterior) não foi invalidado pelo novo login por MFA")
	}

	var usadoEm2 sql.NullTime
	if err := db.QueryRow(`SELECT usado_em FROM tokens_acao WHERE token = $1`, token2).Scan(&usadoEm2); err != nil {
		t.Fatalf("falha ao reler token2: %v", err)
	}
	if usadoEm2.Valid {
		t.Error("token2 (o mais recente) não deveria estar invalidado")
	}
}

// TestConcluirLoginMFA_CodigoCorreto prova o caminho feliz: um código TOTP
// válido troca o token de mfa_login por um usuarioID, marcando o token usado.
func TestConcluirLoginMFA_CodigoCorreto(t *testing.T) {
	db := testDB(t)
	usuarioID, segredo := criarUsuarioComMFA(t, db, "mfa-concluir-ok@empresa.com", "senha-123456")
	token, err := IniciarLoginMFA(db, usuarioID)
	if err != nil {
		t.Fatalf("IniciarLoginMFA falhou: %v", err)
	}

	got, err := ConcluirLoginMFA(db, token, codigoTOTPValidoAgora(t, segredo))
	if err != nil {
		t.Fatalf("ConcluirLoginMFA retornou erro inesperado: %v", err)
	}
	if got != usuarioID {
		t.Errorf("usuarioID = %q, want %q", got, usuarioID)
	}

	var usadoEm sql.NullTime
	if err := db.QueryRow(`SELECT usado_em FROM tokens_acao WHERE token = $1`, token).Scan(&usadoEm); err != nil {
		t.Fatalf("falha ao reler token: %v", err)
	}
	if !usadoEm.Valid {
		t.Error("usado_em não preenchido após ConcluirLoginMFA bem-sucedido")
	}
}

// TestConcluirLoginMFA_CodigoErradoNaoConsomeToken prova a I/O Matrix: um
// código incorreto devolve ErrMFACodigoInvalido e NÃO consome o token — nova
// tentativa continua possível até expirar ou a conta bloquear.
func TestConcluirLoginMFA_CodigoErradoNaoConsomeToken(t *testing.T) {
	db := testDB(t)
	usuarioID, _ := criarUsuarioComMFA(t, db, "mfa-codigo-errado@empresa.com", "senha-123456")
	token, err := IniciarLoginMFA(db, usuarioID)
	if err != nil {
		t.Fatalf("IniciarLoginMFA falhou: %v", err)
	}

	_, err = ConcluirLoginMFA(db, token, "000000")
	if !errors.Is(err, ErrMFACodigoInvalido) {
		t.Fatalf("erro = %v, want ErrMFACodigoInvalido", err)
	}

	var usadoEm sql.NullTime
	if err := db.QueryRow(`SELECT usado_em FROM tokens_acao WHERE token = $1`, token).Scan(&usadoEm); err != nil {
		t.Fatalf("falha ao reler token: %v", err)
	}
	if usadoEm.Valid {
		t.Error("usado_em preenchido após código errado — o token não deveria ter sido consumido")
	}

	tentativas, _ := lerBloqueioLogin(t, db, usuarioID)
	if tentativas != 1 {
		t.Errorf("tentativas_login_falhas = %d, want 1 — código errado deve contar como tentativa de força bruta", tentativas)
	}
}

// TestConcluirLoginMFA_QuintaFalhaBloqueiaConta prova que 5 códigos errados
// seguidos bloqueiam a conta por duracaoBloqueioLogin — o MESMO contador da
// Story 1.10, compartilhado com senha.
func TestConcluirLoginMFA_QuintaFalhaBloqueiaConta(t *testing.T) {
	db := testDB(t)
	usuarioID, segredo := criarUsuarioComMFA(t, db, "mfa-quinta-falha@empresa.com", "senha-123456")

	for i := 0; i < maxTentativasLogin; i++ {
		token, err := IniciarLoginMFA(db, usuarioID)
		if err != nil {
			t.Fatalf("IniciarLoginMFA (tentativa %d) falhou: %v", i+1, err)
		}
		_, err = ConcluirLoginMFA(db, token, "000000")
		if i < maxTentativasLogin-1 {
			if !errors.Is(err, ErrMFACodigoInvalido) {
				t.Fatalf("tentativa %d: erro = %v, want ErrMFACodigoInvalido", i+1, err)
			}
		} else if !errors.Is(err, ErrMFACodigoInvalido) {
			t.Fatalf("última tentativa: erro = %v, want ErrMFACodigoInvalido", err)
		}
	}

	_, bloqueadoAte := lerBloqueioLogin(t, db, usuarioID)
	if !bloqueadoAte.Valid || !bloqueadoAte.Time.After(time.Now()) {
		t.Fatalf("bloqueado_ate = %v, want um instante no futuro após %d falhas", bloqueadoAte, maxTentativasLogin)
	}

	// Mesmo com o código CERTO, a conta bloqueada recusa a tentativa.
	token, err := IniciarLoginMFA(db, usuarioID)
	if err != nil {
		t.Fatalf("IniciarLoginMFA (pós-bloqueio) falhou: %v", err)
	}
	if _, err := ConcluirLoginMFA(db, token, codigoTOTPValidoAgora(t, segredo)); !errors.Is(err, ErrContaBloqueada) {
		t.Fatalf("erro = %v, want ErrContaBloqueada mesmo com código correto", err)
	}
}

// TestConcluirLoginMFA_TokenExpirado prova que um token de mfa_login com
// expira_em no passado é recusado com ErrTokenExpirado, mesmo com o código
// certo.
func TestConcluirLoginMFA_TokenExpirado(t *testing.T) {
	db := testDB(t)
	usuarioID, segredo := criarUsuarioComMFA(t, db, "mfa-token-expirado@empresa.com", "senha-123456")
	token, err := IniciarLoginMFA(db, usuarioID)
	if err != nil {
		t.Fatalf("IniciarLoginMFA falhou: %v", err)
	}
	if _, err := db.Exec(`UPDATE tokens_acao SET expira_em = now() - interval '1 hour' WHERE token = $1`, token); err != nil {
		t.Fatalf("falha ao forçar expiração: %v", err)
	}

	if _, err := ConcluirLoginMFA(db, token, codigoTOTPValidoAgora(t, segredo)); !errors.Is(err, ErrTokenExpirado) {
		t.Fatalf("erro = %v, want ErrTokenExpirado", err)
	}
}

// TestConcluirLoginMFA_TokenReusado prova que um token já usado (login por
// MFA já concluído) não pode ser reaproveitado.
func TestConcluirLoginMFA_TokenReusado(t *testing.T) {
	db := testDB(t)
	usuarioID, segredo := criarUsuarioComMFA(t, db, "mfa-token-reusado@empresa.com", "senha-123456")
	token, err := IniciarLoginMFA(db, usuarioID)
	if err != nil {
		t.Fatalf("IniciarLoginMFA falhou: %v", err)
	}
	if _, err := ConcluirLoginMFA(db, token, codigoTOTPValidoAgora(t, segredo)); err != nil {
		t.Fatalf("primeira conclusão falhou: %v", err)
	}

	if _, err := ConcluirLoginMFA(db, token, codigoTOTPValidoAgora(t, segredo)); !errors.Is(err, ErrTokenExpirado) {
		t.Fatalf("segunda conclusão: erro = %v, want ErrTokenExpirado", err)
	}
}

// TestConcluirLoginMFA_CodigoReusadoNaoAceitoEmNovoToken prova a defesa
// contra reuso de código dentro da mesma janela de validade (~30s): o MESMO
// código TOTP, correto, não pode autenticar duas vezes — mesmo trocado de
// token de mfa_login (um código interceptado por quem já conhece a senha não
// pode ser reaproveitado para logar de novo).
func TestConcluirLoginMFA_CodigoReusadoNaoAceitoEmNovoToken(t *testing.T) {
	db := testDB(t)
	usuarioID, segredo := criarUsuarioComMFA(t, db, "mfa-codigo-reusado@empresa.com", "senha-123456")

	token1, err := IniciarLoginMFA(db, usuarioID)
	if err != nil {
		t.Fatalf("IniciarLoginMFA (1) falhou: %v", err)
	}
	codigo := codigoTOTPValidoAgora(t, segredo)
	if _, err := ConcluirLoginMFA(db, token1, codigo); err != nil {
		t.Fatalf("primeira conclusão falhou: %v", err)
	}

	token2, err := IniciarLoginMFA(db, usuarioID)
	if err != nil {
		t.Fatalf("IniciarLoginMFA (2) falhou: %v", err)
	}
	if _, err := ConcluirLoginMFA(db, token2, codigo); !errors.Is(err, ErrMFACodigoInvalido) {
		t.Fatalf("erro = %v, want ErrMFACodigoInvalido (reuso do mesmo código nesta janela)", err)
	}
}

// TestConcluirLoginMFA_TokenInexistente prova que um token que nunca existiu
// devolve ErrTokenNaoEncontrado.
func TestConcluirLoginMFA_TokenInexistente(t *testing.T) {
	db := testDB(t)
	if _, err := ConcluirLoginMFA(db, "token-nunca-existiu", "123456"); !errors.Is(err, ErrTokenNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrTokenNaoEncontrado", err)
	}
}

// TestConfirmarConfiguracaoMFA_Sucesso prova o caminho feliz do enrollment:
// código correto grava mfa_secret/mfa_habilitado=true.
func TestConfirmarConfiguracaoMFA_Sucesso(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "mfa-confirmar-ok@empresa.com", "senha-123456", true, true)
	segredo, err := GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP: %v", err)
	}

	if err := ConfirmarConfiguracaoMFA(db, usuarioID, "senha-123456", segredo, codigoTOTPValidoAgora(t, segredo)); err != nil {
		t.Fatalf("ConfirmarConfiguracaoMFA retornou erro inesperado: %v", err)
	}

	var mfaHabilitado bool
	var mfaSecret sql.NullString
	if err := db.QueryRow(`SELECT mfa_habilitado, mfa_secret FROM usuarios WHERE id = $1`, usuarioID).Scan(&mfaHabilitado, &mfaSecret); err != nil {
		t.Fatalf("falha ao reler usuario: %v", err)
	}
	if !mfaHabilitado {
		t.Error("mfa_habilitado = false, want true")
	}
	if !mfaSecret.Valid || mfaSecret.String != segredo {
		t.Errorf("mfa_secret = %+v, want %q", mfaSecret, segredo)
	}
}

// TestConfirmarConfiguracaoMFA_CodigoErrado prova que um código incorreto não
// grava nenhuma coluna.
func TestConfirmarConfiguracaoMFA_CodigoErrado(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "mfa-confirmar-errado@empresa.com", "senha-123456", true, true)
	segredo, err := GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP: %v", err)
	}

	if err := ConfirmarConfiguracaoMFA(db, usuarioID, "senha-123456", segredo, "000000"); !errors.Is(err, ErrMFACodigoInvalido) {
		t.Fatalf("erro = %v, want ErrMFACodigoInvalido", err)
	}

	var mfaHabilitado bool
	if err := db.QueryRow(`SELECT mfa_habilitado FROM usuarios WHERE id = $1`, usuarioID).Scan(&mfaHabilitado); err != nil {
		t.Fatalf("falha ao reler usuario: %v", err)
	}
	if mfaHabilitado {
		t.Error("mfa_habilitado = true após código errado, want false")
	}
}

// TestConfirmarConfiguracaoMFA_SenhaErrada prova a defesa contra sequestro de
// conta (Story 1.11): a senha atual errada devolve ErrCredenciaisInvalidas —
// mesmo erro/vocabulário de Login — e NENHUMA coluna é gravada, mesmo com o
// código TOTP correto.
func TestConfirmarConfiguracaoMFA_SenhaErrada(t *testing.T) {
	db := testDB(t)
	usuarioID := criarUsuarioParaLogin(t, db, "mfa-confirmar-senha-errada@empresa.com", "senha-123456", true, true)
	segredo, err := GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP: %v", err)
	}

	if err := ConfirmarConfiguracaoMFA(db, usuarioID, "senha-totalmente-errada", segredo, codigoTOTPValidoAgora(t, segredo)); !errors.Is(err, ErrCredenciaisInvalidas) {
		t.Fatalf("erro = %v, want ErrCredenciaisInvalidas", err)
	}

	var mfaHabilitado bool
	var mfaSecret sql.NullString
	if err := db.QueryRow(`SELECT mfa_habilitado, mfa_secret FROM usuarios WHERE id = $1`, usuarioID).Scan(&mfaHabilitado, &mfaSecret); err != nil {
		t.Fatalf("falha ao reler usuario: %v", err)
	}
	if mfaHabilitado || mfaSecret.Valid {
		t.Error("MFA habilitado/segredo gravado apesar da senha atual incorreta")
	}
}

// TestConfirmarConfiguracaoMFA_JaConfigurado prova o guard de corrida: uma
// conta que já tem mfa_habilitado=true recusa uma nova confirmação.
func TestConfirmarConfiguracaoMFA_JaConfigurado(t *testing.T) {
	db := testDB(t)
	usuarioID, segredoAntigo := criarUsuarioComMFA(t, db, "mfa-ja-configurado@empresa.com", "senha-123456")

	novoSegredo, err := GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP: %v", err)
	}
	if err := ConfirmarConfiguracaoMFA(db, usuarioID, "senha-123456", novoSegredo, codigoTOTPValidoAgora(t, novoSegredo)); !errors.Is(err, ErrMFAJaConfigurado) {
		t.Fatalf("erro = %v, want ErrMFAJaConfigurado", err)
	}

	var mfaSecret sql.NullString
	if err := db.QueryRow(`SELECT mfa_secret FROM usuarios WHERE id = $1`, usuarioID).Scan(&mfaSecret); err != nil {
		t.Fatalf("falha ao reler usuario: %v", err)
	}
	if !mfaSecret.Valid || mfaSecret.String != segredoAntigo {
		t.Error("mfa_secret foi sobrescrito por uma tentativa de reconfiguração — deveria permanecer intacto")
	}
}

// TestRenovarSessao_PreservaOrigem prova que RenovarSessao propaga a mesma
// `origem` da sessão que está rotacionando — tanto para o claim do novo
// access token quanto para a nova linha em `sessoes` (Story 1.11): uma
// sessão SSO nunca vira "senha" (nem vice-versa) por refresh.
func TestRenovarSessao_PreservaOrigem(t *testing.T) {
	db := testDB(t)
	for _, origem := range []string{"senha", "sso"} {
		t.Run(origem, func(t *testing.T) {
			usuarioID := criarUsuarioParaLogin(t, db, "renovar-origem-"+origem+"@empresa.com", "senha-123456", true, true)
			_, refreshToken, _, err := EmitirSessao(db, testJWTSecret, usuarioID, origem)
			if err != nil {
				t.Fatalf("EmitirSessao falhou: %v", err)
			}

			novoAccess, novoRefresh, _, err := RenovarSessao(db, testJWTSecret, refreshToken)
			if err != nil {
				t.Fatalf("RenovarSessao falhou: %v", err)
			}

			var origemPersistida string
			if err := db.QueryRow(`SELECT origem FROM sessoes WHERE refresh_token = $1`, novoRefresh).Scan(&origemPersistida); err != nil {
				t.Fatalf("falha ao ler origem da nova sessão: %v", err)
			}
			if origemPersistida != origem {
				t.Errorf("sessoes.origem = %q, want %q", origemPersistida, origem)
			}

			claims := &AcessoClaims{}
			parsed, err := jwt.ParseWithClaims(novoAccess, claims, func(*jwt.Token) (any, error) { return testJWTSecret, nil })
			if err != nil || !parsed.Valid {
				t.Fatalf("novo access token inválido: %v", err)
			}
			if claims.Origem != origem {
				t.Errorf("claim origem = %q, want %q", claims.Origem, origem)
			}
		})
	}
}
