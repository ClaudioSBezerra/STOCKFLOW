package services

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Testes do Dono da Plataforma — Story 9.2, spec-9-2 (linhas 1-5 da I/O
// Matrix). `donos_plataforma` é limpa com DELETE (cascata para
// `sessoes_plataforma`), nunca com TRUNCATE ... CASCADE.

const senhaDonoTeste = "senha-dono-123"

var segredoJWTPlataformaTeste = []byte("segredo-plataforma-de-teste")

// limparDonosPlataforma apaga todos os Donos (e, em cascata, as sessões deles).
func limparDonosPlataforma(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM donos_plataforma`); err != nil {
		t.Fatalf("limpar donos_plataforma: %v", err)
	}
}

// criarDonoTeste cria o único Dono do teste pelo MESMO caminho do CLI e o
// remove ao fim do teste.
func criarDonoTeste(t *testing.T, db *sql.DB, email string) (id, segredo string) {
	t.Helper()
	limparDonosPlataforma(t, db)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM donos_plataforma`) })
	id, segredo, err := CriarPrimeiroDonoPlataforma(db, "Dona da Plataforma", email, senhaDonoTeste)
	if err != nil {
		t.Fatalf("CriarPrimeiroDonoPlataforma: %v", err)
	}
	return id, segredo
}

// codigoTOTPAgora gera o código TOTP válido de `segredo` no passo atual.
func codigoTOTPAgora(t *testing.T, segredo string) string {
	t.Helper()
	codigo, err := gerarCodigoHOTP(segredo, uint64(PassoAtualTOTP()))
	if err != nil {
		t.Fatalf("gerarCodigoHOTP: %v", err)
	}
	return codigo
}

// codigoErrado devolve um código de 6 dígitos garantidamente diferente do
// correto (troca o último dígito).
func codigoErrado(correto string) string {
	ultimo := (correto[5]-'0'+1)%10 + '0'
	return correto[:5] + string(rune(ultimo))
}

func TestLoginDonoPlataforma_Sucesso(t *testing.T) {
	db := testDB(t)
	id, segredo := criarDonoTeste(t, db, "dona@plataforma.com")

	got, err := LoginDonoPlataforma(db, "  DONA@Plataforma.com ", senhaDonoTeste, codigoTOTPAgora(t, segredo))
	if err != nil {
		t.Fatalf("LoginDonoPlataforma: %v", err)
	}
	if got != id {
		t.Errorf("id = %q, want %q", got, id)
	}
	var passo sql.NullInt64
	if err := db.QueryRow(`SELECT mfa_ultimo_passo_usado FROM donos_plataforma WHERE id = $1`, id).Scan(&passo); err != nil {
		t.Fatalf("ler passo: %v", err)
	}
	if !passo.Valid {
		t.Error("mfa_ultimo_passo_usado continua NULL após login bem-sucedido")
	}
}

// TestLoginDonoPlataforma_FalhasColapsamNoMesmoErro prova que senha errada,
// código errado e e-mail inexistente devolvem o MESMO erro, e que nenhuma
// dessas falhas consome o passo TOTP.
func TestLoginDonoPlataforma_FalhasColapsamNoMesmoErro(t *testing.T) {
	db := testDB(t)
	id, segredo := criarDonoTeste(t, db, "dona@plataforma.com")
	codigo := codigoTOTPAgora(t, segredo)

	casos := []struct{ nome, email, senha, codigo string }{
		{"senha errada", "dona@plataforma.com", "senha-errada-9", codigo},
		{"código errado", "dona@plataforma.com", senhaDonoTeste, codigoErrado(codigo)},
		{"código não numérico", "dona@plataforma.com", senhaDonoTeste, "abcdef"},
		{"e-mail inexistente", "ninguem@plataforma.com", senhaDonoTeste, codigo},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if _, err := LoginDonoPlataforma(db, c.email, c.senha, c.codigo); !errors.Is(err, ErrCredenciaisInvalidas) {
				t.Fatalf("erro = %v, want ErrCredenciaisInvalidas", err)
			}
		})
	}

	var passo sql.NullInt64
	if err := db.QueryRow(`SELECT mfa_ultimo_passo_usado FROM donos_plataforma WHERE id = $1`, id).Scan(&passo); err != nil {
		t.Fatalf("ler passo: %v", err)
	}
	if passo.Valid {
		t.Errorf("mfa_ultimo_passo_usado = %d, want NULL — nenhuma tentativa falha pode consumir o passo", passo.Int64)
	}
}

func TestLoginDonoPlataforma_CodigoReusado(t *testing.T) {
	db := testDB(t)
	_, segredo := criarDonoTeste(t, db, "dona@plataforma.com")
	codigo := codigoTOTPAgora(t, segredo)

	if _, err := LoginDonoPlataforma(db, "dona@plataforma.com", senhaDonoTeste, codigo); err != nil {
		t.Fatalf("primeiro login: %v", err)
	}
	if _, err := LoginDonoPlataforma(db, "dona@plataforma.com", senhaDonoTeste, codigo); !errors.Is(err, ErrCredenciaisInvalidas) {
		t.Fatalf("reuso do código: erro = %v, want ErrCredenciaisInvalidas", err)
	}
}

func TestLoginDonoPlataforma_Inativo(t *testing.T) {
	db := testDB(t)
	id, segredo := criarDonoTeste(t, db, "dona@plataforma.com")
	if _, err := db.Exec(`UPDATE donos_plataforma SET ativo = false WHERE id = $1`, id); err != nil {
		t.Fatalf("desativar dono: %v", err)
	}
	if _, err := LoginDonoPlataforma(db, "dona@plataforma.com", senhaDonoTeste, codigoTOTPAgora(t, segredo)); !errors.Is(err, ErrCredenciaisInvalidas) {
		t.Fatalf("erro = %v, want ErrCredenciaisInvalidas", err)
	}
}

func TestLoginDonoPlataforma_CampoEmBranco(t *testing.T) {
	db := testDB(t)
	casos := []struct{ nome, email, senha, codigo string }{
		{"e-mail", "  ", senhaDonoTeste, "123456"},
		{"senha", "dona@plataforma.com", " ", "123456"},
		{"código", "dona@plataforma.com", senhaDonoTeste, ""},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if _, err := LoginDonoPlataforma(db, c.email, c.senha, c.codigo); !errors.Is(err, ErrLoginValidacao) {
				t.Fatalf("erro = %v, want ErrLoginValidacao", err)
			}
		})
	}
}

// TestSessaoPlataforma_EmitirRenovarRevogar prova o formato AD-6 com `aud`
// próprio, a rotação (o token antigo morre) e a revogação.
func TestSessaoPlataforma_EmitirRenovarRevogar(t *testing.T) {
	db := testDB(t)
	id, _ := criarDonoTeste(t, db, "dona@plataforma.com")

	access, refresh, expira, err := EmitirSessaoPlataforma(db, segredoJWTPlataformaTeste, id)
	if err != nil {
		t.Fatalf("EmitirSessaoPlataforma: %v", err)
	}
	if d := time.Until(expira); d < RefreshTokenExpiracao-time.Minute || d > RefreshTokenExpiracao {
		t.Errorf("expiração do refresh = %v, want ~%v", d, RefreshTokenExpiracao)
	}

	claims := &PlataformaClaims{}
	if _, err := jwt.ParseWithClaims(access, claims, func(*jwt.Token) (any, error) {
		return segredoJWTPlataformaTeste, nil
	}, jwt.WithAudience(AudienciaPlataforma)); err != nil {
		t.Fatalf("access token não valida com aud=plataforma: %v", err)
	}
	if claims.Subject != id {
		t.Errorf("sub = %q, want %q", claims.Subject, id)
	}

	_, novoRefresh, _, err := RenovarSessaoPlataforma(db, segredoJWTPlataformaTeste, refresh)
	if err != nil {
		t.Fatalf("RenovarSessaoPlataforma: %v", err)
	}
	if novoRefresh == refresh {
		t.Error("refresh não foi rotacionado")
	}
	if _, _, _, err := RenovarSessaoPlataforma(db, segredoJWTPlataformaTeste, refresh); !errors.Is(err, ErrSessaoInvalida) {
		t.Errorf("refresh antigo: erro = %v, want ErrSessaoInvalida", err)
	}

	if err := RevogarSessaoPlataforma(db, novoRefresh); err != nil {
		t.Fatalf("RevogarSessaoPlataforma: %v", err)
	}
	if _, _, _, err := RenovarSessaoPlataforma(db, segredoJWTPlataformaTeste, novoRefresh); !errors.Is(err, ErrSessaoInvalida) {
		t.Errorf("refresh revogado: erro = %v, want ErrSessaoInvalida", err)
	}
	if _, _, _, err := RenovarSessaoPlataforma(db, segredoJWTPlataformaTeste, ""); !errors.Is(err, ErrSessaoInvalida) {
		t.Errorf("refresh vazio: erro = %v, want ErrSessaoInvalida", err)
	}
	if err := RevogarSessaoPlataforma(db, "nao-existe"); err != nil {
		t.Errorf("revogar token inexistente deveria ser idempotente: %v", err)
	}

	// Dono desativado: a sessão dele não rotaciona mais.
	_, refreshInativo, _, err := EmitirSessaoPlataforma(db, segredoJWTPlataformaTeste, id)
	if err != nil {
		t.Fatalf("EmitirSessaoPlataforma: %v", err)
	}
	if _, err := db.Exec(`UPDATE donos_plataforma SET ativo = false WHERE id = $1`, id); err != nil {
		t.Fatalf("desativar dono: %v", err)
	}
	if _, _, _, err := RenovarSessaoPlataforma(db, segredoJWTPlataformaTeste, refreshInativo); !errors.Is(err, ErrSessaoInvalida) {
		t.Errorf("refresh de dono inativo: erro = %v, want ErrSessaoInvalida", err)
	}
}

func TestBuscarDonoSessao(t *testing.T) {
	db := testDB(t)
	id, _ := criarDonoTeste(t, db, "dona@plataforma.com")

	d, err := BuscarDonoSessao(db, id)
	if err != nil {
		t.Fatalf("BuscarDonoSessao: %v", err)
	}
	if d.ID != id || d.Email != "dona@plataforma.com" || !d.Ativo {
		t.Errorf("dono = %+v", d)
	}
	for _, idInvalido := range []string{"00000000-0000-4000-8000-000000000000", "nao-e-uuid"} {
		if _, err := BuscarDonoSessao(db, idInvalido); !errors.Is(err, ErrDonoSessaoNaoEncontrado) {
			t.Errorf("BuscarDonoSessao(%q): erro = %v, want ErrDonoSessaoNaoEncontrado", idInvalido, err)
		}
	}
}

func TestCriarPrimeiroDonoPlataforma_Cria(t *testing.T) {
	db := testDB(t)
	limparDonosPlataforma(t, db)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM donos_plataforma`) })

	id, segredo, err := CriarPrimeiroDonoPlataforma(db, "  Dona Inicial  ", "Dona@Plataforma.COM", senhaDonoTeste)
	if err != nil {
		t.Fatalf("CriarPrimeiroDonoPlataforma: %v", err)
	}

	var nome, email, senhaHash, segredoGravado string
	var mfaHabilitado, ativo bool
	if err := db.QueryRow(
		`SELECT nome, email, senha_hash, mfa_habilitado, mfa_secret, ativo FROM donos_plataforma WHERE id = $1`, id,
	).Scan(&nome, &email, &senhaHash, &mfaHabilitado, &segredoGravado, &ativo); err != nil {
		t.Fatalf("ler dono: %v", err)
	}
	if nome != "Dona Inicial" || email != "dona@plataforma.com" {
		t.Errorf("nome/email = %q/%q", nome, email)
	}
	if !mfaHabilitado || !ativo {
		t.Errorf("mfa_habilitado=%v ativo=%v, want true/true", mfaHabilitado, ativo)
	}
	if bcrypt.CompareHashAndPassword([]byte(senhaHash), []byte(senhaDonoTeste)) != nil {
		t.Error("senha_hash não confere com a senha via bcrypt")
	}
	if segredoGravado != segredo {
		t.Errorf("mfa_secret gravado = %q, want o segredo devolvido %q", segredoGravado, segredo)
	}
	if !ValidarCodigoTOTP(segredo, codigoTOTPAgora(t, segredo)) {
		t.Error("o segredo devolvido não gera um código TOTP válido")
	}
}

func TestCriarPrimeiroDonoPlataforma_RecusaSegundo(t *testing.T) {
	db := testDB(t)
	criarDonoTeste(t, db, "primeira@plataforma.com")

	if _, _, err := CriarPrimeiroDonoPlataforma(db, "Segunda", "segunda@plataforma.com", senhaDonoTeste); !errors.Is(err, ErrDonoJaExiste) {
		t.Fatalf("erro = %v, want ErrDonoJaExiste", err)
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

func TestCriarPrimeiroDonoPlataforma_Validacoes(t *testing.T) {
	db := testDB(t)
	limparDonosPlataforma(t, db)

	if _, _, err := CriarPrimeiroDonoPlataforma(db, "Dona", "dona@plataforma.com", "abc"); !errors.Is(err, ErrSenhaFraca) {
		t.Errorf("senha fraca: erro = %v, want ErrSenhaFraca", err)
	}
	if _, _, err := CriarPrimeiroDonoPlataforma(db, "Dona", "sem-arroba", senhaDonoTeste); !errors.Is(err, ErrDonoValidacao) {
		t.Errorf("e-mail sem @: erro = %v, want ErrDonoValidacao", err)
	}
	if _, _, err := CriarPrimeiroDonoPlataforma(db, "   ", "dona@plataforma.com", senhaDonoTeste); !errors.Is(err, ErrDonoValidacao) {
		t.Errorf("nome vazio: erro = %v, want ErrDonoValidacao", err)
	}
	var total int
	if err := db.QueryRow(`SELECT count(*) FROM donos_plataforma`).Scan(&total); err != nil {
		t.Fatalf("contar donos: %v", err)
	}
	if total != 0 {
		t.Errorf("donos = %d, want 0 — validação reprovada não grava nada", total)
	}
}

// TestDonosPlataforma_MFAEstruturalNoBanco prova que o próprio schema recusa
// um Dono sem MFA — mesmo por INSERT direto, sem passar pelo service.
func TestDonosPlataforma_MFAEstruturalNoBanco(t *testing.T) {
	db := testDB(t)
	limparDonosPlataforma(t, db)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM donos_plataforma`) })

	if _, err := db.Exec(`INSERT INTO donos_plataforma (nome, email, senha_hash, mfa_habilitado, mfa_secret)
		VALUES ('x', 'x@plataforma.com', 'h', false, 'S')`); err == nil {
		t.Error("INSERT com mfa_habilitado=false foi aceito")
	}
	if _, err := db.Exec(`INSERT INTO donos_plataforma (nome, email, senha_hash, mfa_secret)
		VALUES ('x', 'x@plataforma.com', 'h', NULL)`); err == nil {
		t.Error("INSERT com mfa_secret NULL foi aceito")
	}
}
