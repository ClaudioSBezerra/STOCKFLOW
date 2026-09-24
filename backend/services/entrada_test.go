package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Story 15.2 (AD-36): login na raiz do domínio pela conta — EntrarPelaConta,
// IniciarEscolhaEmpresa e ConcluirEscolhaEmpresa.

const (
	slugEntradaReal        = "entrada-real-s152"
	slugEntradaTreinamento = "entrada-real-s152-treinamento"
	slugEntradaOutra       = "entrada-outra-s152"
)

// limparEmpresasEntrada apaga as Empresas desta suíte (Treinamento antes da
// real, por causa da FK `empresa_origem_id`) com as contas delas.
func limparEmpresasEntrada(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, slug := range []string{slugEntradaTreinamento, slugEntradaReal, slugEntradaOutra} {
		if _, err := db.Exec(`DELETE FROM usuarios WHERE empresa_id = (SELECT id FROM empresas WHERE slug = $1)`, slug); err != nil {
			t.Fatalf("limpar contas de %s: %v", slug, err)
		}
		removerEmpresaDeTeste(t, db, slug)
	}
}

// prepararEmpresasEntrada cria a Empresa real e o Treinamento dela (o
// `empresa_origem_id` é gravado ANTES de qualquer conta, para o trigger
// preencher `empresa_raiz_id` já com a Empresa real).
func prepararEmpresasEntrada(t *testing.T, db *sql.DB) (real, treinamento Empresa) {
	t.Helper()
	limparEmpresasEntrada(t, db)
	t.Cleanup(func() { limparEmpresasEntrada(t, db) })

	real = criarEmpresaDeTeste(t, db, slugEntradaReal, "152152150001", "Entrada Real")
	treinamento = criarEmpresaDeTeste(t, db, slugEntradaTreinamento, "152152150002", "Entrada Real")
	if _, err := db.Exec(`UPDATE empresas SET empresa_origem_id = $1 WHERE id = $2`, real.ID, treinamento.ID); err != nil {
		t.Fatalf("marcar Treinamento: %v", err)
	}
	return real, treinamento
}

func criarContaEntrada(t *testing.T, db *sql.DB, empresaID, email, senha string, ativo, emailVerificado bool) string {
	t.Helper()
	var senhaHash sql.NullString
	if senha != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		senhaHash = sql.NullString{String: string(hash), Valid: true}
	}
	var id string
	if err := db.QueryRow(`
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, empresa_id)
		VALUES ('Conta Entrada', $1, $2, 'usuario', $3, $4, $5) RETURNING id`,
		email, senhaHash, emailVerificado, ativo, empresaID).Scan(&id); err != nil {
		t.Fatalf("criar conta: %v", err)
	}
	return id
}

func TestEntrarPelaConta_UmaConta(t *testing.T) {
	db := testDB(t)
	real, _ := prepararEmpresasEntrada(t, db)
	id := criarContaEntrada(t, db, real.ID, "ana@entrada.test", "senha-certa-1", true, true)

	avaliadas, aceitas, err := EntrarPelaConta(db, "  ANA@entrada.test ", "senha-certa-1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(avaliadas) != 1 || len(aceitas) != 1 {
		t.Fatalf("avaliadas=%d aceitas=%d, want 1/1", len(avaliadas), len(aceitas))
	}
	c := aceitas[0]
	if c.UsuarioID != id || c.Slug != slugEntradaReal || c.EmpresaID != real.ID || c.NomeFantasia != "Entrada Real" || c.Treinamento {
		t.Errorf("conta = %+v", c)
	}
}

func TestEntrarPelaConta_RealETreinamento(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEmpresasEntrada(t, db)
	idReal := criarContaEntrada(t, db, real.ID, "bia@entrada.test", "senha-certa-1", true, true)
	idTrein := criarContaEntrada(t, db, trein.ID, "bia@entrada.test", "senha-certa-1", true, true)

	_, aceitas, err := EntrarPelaConta(db, "bia@entrada.test", "senha-certa-1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(aceitas) != 2 || aceitas[0].UsuarioID != idReal || aceitas[0].Treinamento || aceitas[1].UsuarioID != idTrein || !aceitas[1].Treinamento {
		t.Fatalf("aceitas = %+v (real primeiro)", aceitas)
	}

	token, err := IniciarEscolhaEmpresa(db, aceitas)
	if err != nil {
		t.Fatalf("IniciarEscolhaEmpresa: %v", err)
	}
	var tipo, usuarioID string
	if err := db.QueryRow(`SELECT tipo, usuario_id FROM tokens_acao WHERE token = $1`, token).Scan(&tipo, &usuarioID); err != nil {
		t.Fatalf("ler token: %v", err)
	}
	if tipo != "escolha_empresa" || usuarioID != idReal {
		t.Errorf("tipo=%q usuario_id=%q", tipo, usuarioID)
	}

	c, err := ConcluirEscolhaEmpresa(db, token, slugEntradaTreinamento)
	if err != nil {
		t.Fatalf("ConcluirEscolhaEmpresa: %v", err)
	}
	if c.UsuarioID != idTrein || !c.Treinamento || c.Slug != slugEntradaTreinamento {
		t.Errorf("conta escolhida = %+v", c)
	}

	// Uso único.
	if _, err := ConcluirEscolhaEmpresa(db, token, slugEntradaReal); !errors.Is(err, ErrEscolhaInvalida) {
		t.Errorf("reuso: err = %v, want ErrEscolhaInvalida", err)
	}
}

func TestConcluirEscolhaEmpresa_Invalida(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEmpresasEntrada(t, db)
	outra := criarEmpresaDeTeste(t, db, slugEntradaOutra, "152152150003", "Outra")
	criarContaEntrada(t, db, real.ID, "caio@entrada.test", "senha-certa-1", true, true)
	idTrein := criarContaEntrada(t, db, trein.ID, "caio@entrada.test", "senha-certa-1", true, true)
	criarContaEntrada(t, db, outra.ID, "outro@entrada.test", "senha-certa-1", true, true)

	novoToken := func() string {
		t.Helper()
		_, aceitas, err := EntrarPelaConta(db, "caio@entrada.test", "senha-certa-1")
		if err != nil || len(aceitas) != 2 {
			t.Fatalf("EntrarPelaConta: aceitas=%d err=%v", len(aceitas), err)
		}
		token, err := IniciarEscolhaEmpresa(db, aceitas)
		if err != nil {
			t.Fatalf("IniciarEscolhaEmpresa: %v", err)
		}
		return token
	}

	t.Run("slug fora das contas consome o token", func(t *testing.T) {
		token := novoToken()
		if _, err := ConcluirEscolhaEmpresa(db, token, slugEntradaOutra); !errors.Is(err, ErrEscolhaInvalida) {
			t.Fatalf("err = %v", err)
		}
		if _, err := ConcluirEscolhaEmpresa(db, token, slugEntradaReal); !errors.Is(err, ErrEscolhaInvalida) {
			t.Fatalf("token deveria ter sido consumido: err = %v", err)
		}
	})

	t.Run("vencido", func(t *testing.T) {
		token := novoToken()
		if _, err := db.Exec(`UPDATE tokens_acao SET expira_em = now() - interval '1 second' WHERE token = $1`, token); err != nil {
			t.Fatal(err)
		}
		if _, err := ConcluirEscolhaEmpresa(db, token, slugEntradaReal); !errors.Is(err, ErrEscolhaInvalida) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("inexistente ou vazio", func(t *testing.T) {
		if _, err := ConcluirEscolhaEmpresa(db, "nao-existe", slugEntradaReal); !errors.Is(err, ErrEscolhaInvalida) {
			t.Fatalf("err = %v", err)
		}
		if _, err := ConcluirEscolhaEmpresa(db, "", slugEntradaReal); !errors.Is(err, ErrEscolhaInvalida) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("conta desativada depois da senha", func(t *testing.T) {
		token := novoToken()
		if _, err := db.Exec(`UPDATE usuarios SET ativo = false WHERE id = $1`, idTrein); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = db.Exec(`UPDATE usuarios SET ativo = true WHERE id = $1`, idTrein) }()
		if _, err := ConcluirEscolhaEmpresa(db, token, slugEntradaTreinamento); !errors.Is(err, ErrEscolhaInvalida) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("Empresa desativada depois da senha", func(t *testing.T) {
		token := novoToken()
		if _, err := db.Exec(`UPDATE empresas SET status = 'inativa' WHERE id = $1`, trein.ID); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = db.Exec(`UPDATE empresas SET status = 'ativa' WHERE id = $1`, trein.ID) }()
		if _, err := ConcluirEscolhaEmpresa(db, token, slugEntradaTreinamento); !errors.Is(err, ErrEscolhaInvalida) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestEntrarPelaConta_SenhaCertaSoNuma(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEmpresasEntrada(t, db)
	criarContaEntrada(t, db, real.ID, "dani@entrada.test", "senha-real-1", true, true)
	idTrein := criarContaEntrada(t, db, trein.ID, "dani@entrada.test", "senha-trein-1", true, true)

	avaliadas, aceitas, err := EntrarPelaConta(db, "dani@entrada.test", "senha-trein-1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// Só a conta em que a senha conferiu é avaliada: a outra não ganha falha
	// (mesmo efeito do login pelo endereço da Empresa).
	if len(avaliadas) != 1 || avaliadas[0].UsuarioID != idTrein || len(aceitas) != 1 || aceitas[0].UsuarioID != idTrein {
		t.Fatalf("avaliadas=%+v aceitas=%+v", avaliadas, aceitas)
	}
	var falhasReal int
	if err := db.QueryRow(`SELECT tentativas_login_falhas FROM usuarios WHERE empresa_id = $1`, real.ID).Scan(&falhasReal); err != nil {
		t.Fatal(err)
	}
	if falhasReal != 0 {
		t.Errorf("falhas na conta real = %d, want 0", falhasReal)
	}
}

// Caso misto: a conta real está bloqueada, o Treinamento livre, e a senha
// confere nas duas -> entra direto no Treinamento, sem ErrContaBloqueada.
func TestEntrarPelaConta_RealBloqueadaTreinamentoLivre(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEmpresasEntrada(t, db)
	idReal := criarContaEntrada(t, db, real.ID, "iris@entrada.test", "senha-certa-1", true, true)
	idTrein := criarContaEntrada(t, db, trein.ID, "iris@entrada.test", "senha-certa-1", true, true)
	if _, err := db.Exec(`UPDATE usuarios SET tentativas_login_falhas = 5, bloqueado_ate = now() + interval '15 minutes' WHERE id = $1`, idReal); err != nil {
		t.Fatal(err)
	}

	avaliadas, aceitas, err := EntrarPelaConta(db, "iris@entrada.test", "senha-certa-1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(avaliadas) != 2 || len(aceitas) != 1 || aceitas[0].UsuarioID != idTrein {
		t.Fatalf("avaliadas=%d aceitas=%+v", len(avaliadas), aceitas)
	}
}

// Senha errada em todas as contas conta falha em todas.
func TestEntrarPelaConta_SenhaErradaNasDuasContaFalhaNasDuas(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEmpresasEntrada(t, db)
	criarContaEntrada(t, db, real.ID, "joao@entrada.test", "senha-certa-1", true, true)
	criarContaEntrada(t, db, trein.ID, "joao@entrada.test", "senha-certa-2", true, true)

	avaliadas, _, err := EntrarPelaConta(db, "joao@entrada.test", "senha-errada-1")
	if !errors.Is(err, ErrCredenciaisInvalidas) || len(avaliadas) != 2 {
		t.Fatalf("err=%v avaliadas=%d", err, len(avaliadas))
	}
	var comFalha int
	if err := db.QueryRow(`SELECT count(*) FROM usuarios WHERE lower(email) = 'joao@entrada.test' AND tentativas_login_falhas = 1`).Scan(&comFalha); err != nil {
		t.Fatal(err)
	}
	if comFalha != 2 {
		t.Errorf("contas com 1 falha = %d, want 2", comFalha)
	}
}

func TestEntrarPelaConta_Falhas(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEmpresasEntrada(t, db)
	criarContaEntrada(t, db, real.ID, "desativada@entrada.test", "senha-certa-1", false, true)
	criarContaEntrada(t, db, real.ID, "naoconfirmada@entrada.test", "senha-certa-1", true, false)
	criarContaEntrada(t, db, real.ID, "sso@entrada.test", "", true, true)
	criarContaEntrada(t, db, trein.ID, "inativa@entrada.test", "senha-certa-1", true, true)
	if _, err := db.Exec(`UPDATE empresas SET status = 'inativa' WHERE id = $1`, trein.ID); err != nil {
		t.Fatal(err)
	}

	casos := []struct {
		nome, email, senha string
		want               error
		avaliadas          int
	}{
		{"sem conta", "ninguem@entrada.test", "senha-certa-1", ErrCredenciaisInvalidas, 0},
		{"desativada", "desativada@entrada.test", "senha-certa-1", ErrCredenciaisInvalidas, 1},
		{"não confirmada", "naoconfirmada@entrada.test", "senha-certa-1", ErrCredenciaisInvalidas, 1},
		{"só-SSO", "sso@entrada.test", "senha-certa-1", ErrCredenciaisInvalidas, 1},
		{"Empresa inativa", "inativa@entrada.test", "senha-certa-1", ErrCredenciaisInvalidas, 0},
		{"e-mail em branco", "  ", "senha-certa-1", ErrLoginValidacao, 0},
		{"senha em branco", "desativada@entrada.test", "  ", ErrLoginValidacao, 0},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			avaliadas, aceitas, err := EntrarPelaConta(db, c.email, c.senha)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if len(aceitas) != 0 || len(avaliadas) != c.avaliadas {
				t.Errorf("avaliadas=%d aceitas=%d, want %d/0", len(avaliadas), len(aceitas), c.avaliadas)
			}
		})
	}
}

func TestEntrarPelaConta_BloqueioEmCadaConta(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEmpresasEntrada(t, db)
	criarContaEntrada(t, db, real.ID, "edu@entrada.test", "senha-certa-1", true, true)
	criarContaEntrada(t, db, trein.ID, "edu@entrada.test", "senha-certa-1", true, true)

	for i := 1; i <= maxTentativasLogin; i++ {
		if _, _, err := EntrarPelaConta(db, "edu@entrada.test", "senha-errada-1"); !errors.Is(err, ErrCredenciaisInvalidas) {
			t.Fatalf("tentativa %d: err = %v", i, err)
		}
	}
	var bloqueadas int
	if err := db.QueryRow(`SELECT count(*) FROM usuarios WHERE lower(email) = 'edu@entrada.test' AND bloqueado_ate > now()`).Scan(&bloqueadas); err != nil {
		t.Fatal(err)
	}
	if bloqueadas != 2 {
		t.Fatalf("contas bloqueadas = %d, want 2", bloqueadas)
	}
	if _, _, err := EntrarPelaConta(db, "edu@entrada.test", "senha-certa-1"); !errors.Is(err, ErrContaBloqueada) {
		t.Fatalf("depois do bloqueio: err = %v, want ErrContaBloqueada", err)
	}
	if _, err := Login(db, real.ID, "edu@entrada.test", "senha-certa-1"); !errors.Is(err, ErrContaBloqueada) {
		t.Fatalf("Login por Empresa depois do bloqueio: err = %v", err)
	}
}

// --- Story 15.3: "Esqueci a senha" na raiz — SolicitarRedefinicaoSenhaPelaConta.

// redefinicaoDaConta devolve os tokens `redefinicao_senha` válidos e as
// variáveis dos e-mails `redefinicao_senha` da conta.
func redefinicaoDaConta(t *testing.T, db *sql.DB, usuarioID string) (tokensValidos []string, emails []map[string]any) {
	t.Helper()
	rows, err := db.Query(`SELECT token FROM tokens_acao
		WHERE usuario_id = $1 AND tipo = 'redefinicao_senha' AND usado_em IS NULL AND expira_em > now()`, usuarioID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var tk string
		if err := rows.Scan(&tk); err != nil {
			t.Fatal(err)
		}
		tokensValidos = append(tokensValidos, tk)
	}
	rows.Close()

	rows, err = db.Query(`SELECT variaveis_json::text FROM emails_pendentes
		WHERE usuario_id = $1 AND tipo = 'redefinicao_senha' ORDER BY criado_em`, usuarioID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			t.Fatal(err)
		}
		emails = append(emails, v)
	}
	return tokensValidos, emails
}

func conferirEmailRedefinicao(t *testing.T, v map[string]any, slug, empresa, token string) {
	t.Helper()
	wantLink := "http://test.local/e/" + slug + "/redefinir-senha?token=" + token
	if got, _ := v["link"].(string); got != wantLink {
		t.Errorf("link = %q, want %q", got, wantLink)
	}
	if got, _ := v["empresa"].(string); got != empresa {
		t.Errorf("empresa = %q, want %q", got, empresa)
	}
}

func TestSolicitarRedefinicaoSenhaPelaConta_UmaConta(t *testing.T) {
	db := testDB(t)
	real, _ := prepararEmpresasEntrada(t, db)
	id := criarContaEntrada(t, db, real.ID, "ana@entrada.test", "senha-certa-1", true, true)

	if err := SolicitarRedefinicaoSenhaPelaConta(db, testEmailCfg, " ANA@Entrada.test "); err != nil {
		t.Fatalf("err = %v", err)
	}
	tokens, emails := redefinicaoDaConta(t, db, id)
	if len(tokens) != 1 || len(emails) != 1 {
		t.Fatalf("tokens=%d emails=%d, want 1/1", len(tokens), len(emails))
	}
	conferirEmailRedefinicao(t, emails[0], slugEntradaReal, "Entrada Real", tokens[0])
}

func TestSolicitarRedefinicaoSenhaPelaConta_RealETreinamento(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEmpresasEntrada(t, db)
	if _, err := db.Exec(`UPDATE empresas SET nome_fantasia = 'Entrada Real (Treinamento)' WHERE id = $1`, trein.ID); err != nil {
		t.Fatal(err)
	}
	idReal := criarContaEntrada(t, db, real.ID, "bia@entrada.test", "senha-real-1", true, true)
	idTrein := criarContaEntrada(t, db, trein.ID, "bia@entrada.test", "senha-trein-1", true, true)

	if err := SolicitarRedefinicaoSenhaPelaConta(db, testEmailCfg, "bia@entrada.test"); err != nil {
		t.Fatalf("err = %v", err)
	}
	tokens, emails := redefinicaoDaConta(t, db, idReal)
	if len(tokens) != 1 || len(emails) != 1 {
		t.Fatalf("real: tokens=%d emails=%d, want 1/1", len(tokens), len(emails))
	}
	conferirEmailRedefinicao(t, emails[0], slugEntradaReal, "Entrada Real", tokens[0])

	tokens, emails = redefinicaoDaConta(t, db, idTrein)
	if len(tokens) != 1 || len(emails) != 1 {
		t.Fatalf("treinamento: tokens=%d emails=%d, want 1/1", len(tokens), len(emails))
	}
	conferirEmailRedefinicao(t, emails[0], slugEntradaTreinamento, "Entrada Real (Treinamento)", tokens[0])
}

func TestSolicitarRedefinicaoSenhaPelaConta_NadaGravado(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEmpresasEntrada(t, db)
	desativada := criarContaEntrada(t, db, real.ID, "desativada@entrada.test", "senha-certa-1", false, true)
	emInativa := criarContaEntrada(t, db, trein.ID, "inativa@entrada.test", "senha-certa-1", true, true)
	if _, err := db.Exec(`UPDATE empresas SET status = 'inativa' WHERE id = $1`, trein.ID); err != nil {
		t.Fatal(err)
	}

	for _, email := range []string{"ninguem@entrada.test", "desativada@entrada.test", "inativa@entrada.test", "", "   "} {
		if err := SolicitarRedefinicaoSenhaPelaConta(db, testEmailCfg, email); err != nil {
			t.Fatalf("%q: err = %v", email, err)
		}
	}
	for _, id := range []string{desativada, emInativa} {
		if tokens, emails := redefinicaoDaConta(t, db, id); len(tokens) != 0 || len(emails) != 0 {
			t.Errorf("conta %s: tokens=%d emails=%d, want 0/0", id, len(tokens), len(emails))
		}
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM emails_pendentes WHERE tipo = 'redefinicao_senha'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("e-mails de redefinição = %d, want 0", n)
	}
}

func TestSolicitarRedefinicaoSenhaPelaConta_PedidoRepetido(t *testing.T) {
	db := testDB(t)
	real, trein := prepararEmpresasEntrada(t, db)
	idReal := criarContaEntrada(t, db, real.ID, "caio@entrada.test", "senha-certa-1", true, true)
	idTrein := criarContaEntrada(t, db, trein.ID, "caio@entrada.test", "senha-certa-1", true, true)

	for i := 0; i < 2; i++ {
		if err := SolicitarRedefinicaoSenhaPelaConta(db, testEmailCfg, "caio@entrada.test"); err != nil {
			t.Fatalf("pedido %d: err = %v", i+1, err)
		}
	}
	for _, c := range []struct{ id, slug string }{{idReal, slugEntradaReal}, {idTrein, slugEntradaTreinamento}} {
		tokens, emails := redefinicaoDaConta(t, db, c.id)
		if len(tokens) != 1 || len(emails) != 2 {
			t.Fatalf("%s: tokens válidos=%d emails=%d, want 1/2", c.slug, len(tokens), len(emails))
		}
		// o token válido é o do último e-mail
		if link, _ := emails[1]["link"].(string); !strings.HasSuffix(link, "token="+tokens[0]) {
			t.Errorf("%s: último link %q não leva o token válido", c.slug, link)
		}
	}
}
