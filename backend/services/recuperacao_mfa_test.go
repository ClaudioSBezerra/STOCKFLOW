package services

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Story 14.4: recuperação de MFA — reset por adm e desligamento pela própria conta.

// contaRecuperacaoMFA cria uma conta ativa/verificada com senha conhecida, no
// papel pedido e, se `mfa`, com um segredo TOTP real (devolvido).
func contaRecuperacaoMFA(t *testing.T, db *sql.DB, email, papel string, mfa bool) (id, segredo string) {
	t.Helper()
	id = criarUsuarioParaLogin(t, db, email, "senha-123456", true, true)
	if _, err := db.Exec(`UPDATE usuarios SET papel = $1 WHERE id = $2`, papel, id); err != nil {
		t.Fatalf("definir papel %s: %v", papel, err)
	}
	if mfa {
		s, err := GerarSegredoTOTP()
		if err != nil {
			t.Fatalf("GerarSegredoTOTP: %v", err)
		}
		if _, err := db.Exec(`UPDATE usuarios SET mfa_habilitado = true, mfa_secret = $1 WHERE id = $2`, s, id); err != nil {
			t.Fatalf("habilitar MFA: %v", err)
		}
		segredo = s
	}
	return id, segredo
}

// codigoTOTPInvalidoAgora devolve um código de 6 dígitos que NÃO é aceito
// para `segredo` agora (evita o raro acaso de "000000" ser válido).
func codigoTOTPInvalidoAgora(t *testing.T, segredo string) string {
	t.Helper()
	for i := 0; i < 1000; i++ {
		c := fmt.Sprintf("%06d", i*111%1000000)
		if !ValidarCodigoTOTP(segredo, c) {
			return c
		}
	}
	t.Fatal("não achou código inválido")
	return ""
}

type estadoMFA struct {
	habilitado bool
	segredo    sql.NullString
	passo      sql.NullInt64
	tentativas int
	bloqueado  sql.NullTime
}

func lerEstadoMFA(t *testing.T, db *sql.DB, id string) estadoMFA {
	t.Helper()
	var e estadoMFA
	if err := db.QueryRow(`
		SELECT mfa_habilitado, mfa_secret, mfa_ultimo_passo_usado, tentativas_login_falhas, bloqueado_ate
		FROM usuarios WHERE id = $1`, id,
	).Scan(&e.habilitado, &e.segredo, &e.passo, &e.tentativas, &e.bloqueado); err != nil {
		t.Fatalf("ler estado MFA: %v", err)
	}
	return e
}

func auditoriaDe(t *testing.T, db *sql.DB, empresaID, acao string) (n int, atorID, alvoID string) {
	t.Helper()
	if err := db.QueryRow(`SELECT count(*) FROM auditoria_seguranca WHERE empresa_id = $1 AND acao = $2`, empresaID, acao).Scan(&n); err != nil {
		t.Fatalf("contar auditoria %s: %v", acao, err)
	}
	if n > 0 {
		var alvo sql.NullString
		if err := db.QueryRow(`SELECT ator_id, alvo_id FROM auditoria_seguranca WHERE empresa_id = $1 AND acao = $2 ORDER BY criado_em DESC LIMIT 1`,
			empresaID, acao).Scan(&atorID, &alvo); err != nil {
			t.Fatalf("ler auditoria %s: %v", acao, err)
		}
		alvoID = alvo.String
	}
	return n, atorID, alvoID
}

// --- ResetarMFAUsuario --------------------------------------------------------

func TestResetarMFAUsuario_ZeraColunasRevogaSessoesEAudita(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	admID, _ := contaRecuperacaoMFA(t, db, "reset-adm@empresa.com", PapelAdm, true)
	alvoID, _ := contaRecuperacaoMFA(t, db, "reset-gestor@empresa.com", PapelGestor, true)
	if _, err := db.Exec(`UPDATE usuarios SET mfa_ultimo_passo_usado = 42 WHERE id = $1`, alvoID); err != nil {
		t.Fatal(err)
	}
	semearSessao(t, db, alvoID, "reset-refresh-1")
	tokenMFA, err := IniciarLoginMFA(db, alvoID)
	if err != nil {
		t.Fatalf("IniciarLoginMFA: %v", err)
	}

	u, err := ResetarMFAUsuario(db, empresaTeste, alvoID, admID, PapelAdm)
	if err != nil {
		t.Fatalf("ResetarMFAUsuario: %v", err)
	}
	if u.ID != alvoID || u.MFAHabilitado {
		t.Errorf("resumo = %+v, want id=%s mfaHabilitado=false", u, alvoID)
	}
	e := lerEstadoMFA(t, db, alvoID)
	if e.habilitado || e.segredo.Valid || e.passo.Valid {
		t.Errorf("colunas MFA não zeradas: %+v", e)
	}
	if n := sessoesVivas(t, db, alvoID); n != 0 {
		t.Errorf("sessões vivas = %d, want 0", n)
	}
	var usado sql.NullTime
	if err := db.QueryRow(`SELECT usado_em FROM tokens_acao WHERE token = $1`, tokenMFA).Scan(&usado); err != nil {
		t.Fatal(err)
	}
	if !usado.Valid {
		t.Error("token mfa_login pendente não foi invalidado")
	}
	n, ator, alvo := auditoriaDe(t, db, empresaTeste, AcaoMFAResetado)
	if n != 1 || ator != admID || alvo != alvoID {
		t.Errorf("auditoria = (%d, %s, %s), want (1, %s, %s)", n, ator, alvo, admID, alvoID)
	}
}

func TestResetarMFAUsuario_RankEstrito(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	// O índice idx_usuarios_unico_adm só admite um `adm` por Empresa, então o
	// caso "rank igual" é provado com dois `gestor` (a regra é a mesma:
	// RankPapel(alvo) >= RankPapel(ator)), além do adm sobre si mesmo.
	admID, _ := contaRecuperacaoMFA(t, db, "reset-rank-adm@empresa.com", PapelAdm, true)
	gestorID, _ := contaRecuperacaoMFA(t, db, "reset-rank-gestor@empresa.com", PapelGestor, true)
	gestor2ID, _ := contaRecuperacaoMFA(t, db, "reset-rank-gestor2@empresa.com", PapelGestor, true)

	casos := []struct{ nome, alvo, ator, papelAtor string }{
		{"adm a si mesmo", admID, admID, PapelAdm},
		{"rank igual", gestor2ID, gestorID, PapelGestor},
		{"rank maior", admID, gestorID, PapelGestor},
	}
	for _, c := range casos {
		if _, err := ResetarMFAUsuario(db, empresaTeste, c.alvo, c.ator, c.papelAtor); !errors.Is(err, ErrGestaoForaDeEscopo) {
			t.Errorf("%s: err = %v, want ErrGestaoForaDeEscopo", c.nome, err)
		}
		if !lerEstadoMFA(t, db, c.alvo).habilitado {
			t.Errorf("%s: MFA foi alterado", c.nome)
		}
	}
	if n := contarAuditoria(t, db, empresaTeste); n != 0 {
		t.Errorf("auditoria = %d, want 0", n)
	}
}

func TestResetarMFAUsuario_OutraEmpresaInexistenteMalformado(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	admID, _ := contaRecuperacaoMFA(t, db, "reset-escopo-adm@empresa.com", PapelAdm, true)
	outra := criarOutraEmpresaSeguranca(t, db, "reset-mfa-outra", "887766550014")
	var alvoOutra string
	if err := db.QueryRow(`
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, mfa_habilitado, mfa_secret, empresa_id)
		VALUES ('Outra', 'reset-escopo-outra@empresa.com', 'x', 'gestor', true, true, true, 'SEGREDO', $1) RETURNING id`,
		outra.ID).Scan(&alvoOutra); err != nil {
		t.Fatal(err)
	}

	for nome, id := range map[string]string{
		"outra Empresa": alvoOutra,
		"inexistente":   "00000000-0000-0000-0000-000000000000",
		"malformado":    "abc",
	} {
		if _, err := ResetarMFAUsuario(db, empresaTeste, id, admID, PapelAdm); !errors.Is(err, ErrContaNaoEncontrada) {
			t.Errorf("%s: err = %v, want ErrContaNaoEncontrada", nome, err)
		}
	}
	var habilitado bool
	_ = db.QueryRow(`SELECT mfa_habilitado FROM usuarios WHERE id = $1`, alvoOutra).Scan(&habilitado)
	if !habilitado {
		t.Error("MFA de conta de outra Empresa foi alterado")
	}
}

func TestResetarMFAUsuario_AlvoSemMFA(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	admID, _ := contaRecuperacaoMFA(t, db, "reset-semmfa-adm@empresa.com", PapelAdm, true)
	alvoID, _ := contaRecuperacaoMFA(t, db, "reset-semmfa-alvo@empresa.com", PapelUsuario, false)
	semearSessao(t, db, alvoID, "reset-semmfa-refresh")

	if _, err := ResetarMFAUsuario(db, empresaTeste, alvoID, admID, PapelAdm); !errors.Is(err, ErrMFANaoConfigurado) {
		t.Fatalf("err = %v, want ErrMFANaoConfigurado", err)
	}
	if n := sessoesVivas(t, db, alvoID); n != 1 {
		t.Errorf("sessões vivas = %d, want 1 (nada gravado)", n)
	}
	if n := contarAuditoria(t, db, empresaTeste); n != 0 {
		t.Errorf("auditoria = %d, want 0", n)
	}
}

// --- DesligarMFAPropria -------------------------------------------------------

func TestDesligarMFAPropria_Sucesso(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	id, segredo := contaRecuperacaoMFA(t, db, "desligar-ok@empresa.com", PapelUsuario, true)
	semearSessao(t, db, id, "desligar-ok-refresh")
	if _, err := db.Exec(`UPDATE usuarios SET tentativas_login_falhas = 2 WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}

	if err := DesligarMFAPropria(db, id, PapelUsuario, false, "senha-123456", codigoTOTPValidoAgora(t, segredo)); err != nil {
		t.Fatalf("DesligarMFAPropria: %v", err)
	}
	e := lerEstadoMFA(t, db, id)
	if e.habilitado || e.segredo.Valid || e.passo.Valid || e.tentativas != 0 || e.bloqueado.Valid {
		t.Errorf("estado após desligar = %+v", e)
	}
	if n := sessoesVivas(t, db, id); n != 1 {
		t.Errorf("sessões vivas = %d, want 1 (desligar não revoga)", n)
	}
	n, ator, alvo := auditoriaDe(t, db, empresaTeste, AcaoMFADesligado)
	if n != 1 || ator != id || alvo != id {
		t.Errorf("auditoria = (%d, %s, %s), want (1, %s, %s)", n, ator, alvo, id, id)
	}
}

func TestDesligarMFAPropria_GestorEmEmpresaQueNaoExige(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	id, segredo := contaRecuperacaoMFA(t, db, "desligar-gestor-ok@empresa.com", PapelGestor, true)
	if err := DesligarMFAPropria(db, id, PapelGestor, false, "senha-123456", codigoTOTPValidoAgora(t, segredo)); err != nil {
		t.Fatalf("DesligarMFAPropria: %v", err)
	}
	if lerEstadoMFA(t, db, id).habilitado {
		t.Error("MFA continua habilitado")
	}
}

func TestDesligarMFAPropria_FalhasIncrementamTentativas(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	id, segredo := contaRecuperacaoMFA(t, db, "desligar-falhas@empresa.com", PapelUsuario, true)

	err := DesligarMFAPropria(db, id, PapelUsuario, false, "senha-errada1", codigoTOTPValidoAgora(t, segredo))
	if !errors.Is(err, ErrCredenciaisInvalidas) {
		t.Fatalf("senha errada: err = %v, want ErrCredenciaisInvalidas", err)
	}
	if e := lerEstadoMFA(t, db, id); !e.habilitado || e.tentativas != 1 {
		t.Errorf("após senha errada: %+v, want MFA intacto e tentativas=1", e)
	}

	err = DesligarMFAPropria(db, id, PapelUsuario, false, "senha-123456", codigoTOTPInvalidoAgora(t, segredo))
	if !errors.Is(err, ErrMFACodigoInvalido) {
		t.Fatalf("código errado: err = %v, want ErrMFACodigoInvalido", err)
	}
	if e := lerEstadoMFA(t, db, id); !e.habilitado || e.tentativas != 2 {
		t.Errorf("após código errado: %+v, want MFA intacto e tentativas=2", e)
	}
}

func TestDesligarMFAPropria_CodigoJaUsadoNoPasso(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	id, segredo := contaRecuperacaoMFA(t, db, "desligar-reuso@empresa.com", PapelUsuario, true)
	// Último passo usado >= qualquer passo casável agora (atual±1): o código
	// válido do instante tem de ser recusado, sem depender do relógio.
	if _, err := db.Exec(`UPDATE usuarios SET mfa_ultimo_passo_usado = $1 WHERE id = $2`, PassoAtualTOTP()+1, id); err != nil {
		t.Fatal(err)
	}
	err := DesligarMFAPropria(db, id, PapelUsuario, false, "senha-123456", codigoTOTPValidoAgora(t, segredo))
	if !errors.Is(err, ErrMFACodigoInvalido) {
		t.Fatalf("err = %v, want ErrMFACodigoInvalido", err)
	}
	if e := lerEstadoMFA(t, db, id); !e.habilitado || e.tentativas != 1 {
		t.Errorf("estado = %+v, want MFA intacto e tentativas=1", e)
	}
	if n := contarAuditoria(t, db, empresaTeste); n != 0 {
		t.Errorf("auditoria = %d, want 0", n)
	}
}

// TestDesligarMFAPropria_PassoAnteriorAceito prova que um último passo usado
// ANTERIOR ao do código não bloqueia o desligamento.
func TestDesligarMFAPropria_PassoAnteriorAceito(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	id, segredo := contaRecuperacaoMFA(t, db, "desligar-passo-anterior@empresa.com", PapelUsuario, true)
	if _, err := db.Exec(`UPDATE usuarios SET mfa_ultimo_passo_usado = $1 WHERE id = $2`, PassoAtualTOTP()-5, id); err != nil {
		t.Fatal(err)
	}
	if err := DesligarMFAPropria(db, id, PapelUsuario, false, "senha-123456", codigoTOTPValidoAgora(t, segredo)); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestDesligarMFAPropria_ContaSoSSONaoContaTentativa(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	id, segredo := contaRecuperacaoMFA(t, db, "desligar-sso@empresa.com", PapelUsuario, true)
	if _, err := db.Exec(`UPDATE usuarios SET senha_hash = NULL WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	err := DesligarMFAPropria(db, id, PapelUsuario, false, "qualquer-1", codigoTOTPValidoAgora(t, segredo))
	if !errors.Is(err, ErrCredenciaisInvalidas) {
		t.Fatalf("err = %v, want ErrCredenciaisInvalidas", err)
	}
	if e := lerEstadoMFA(t, db, id); !e.habilitado || e.tentativas != 0 {
		t.Errorf("estado = %+v, want MFA intacto e tentativas=0", e)
	}
}

func TestDesligarMFAPropria_ExigidoPelaEmpresa(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	for _, papel := range []string{PapelGestor, PapelAdm} {
		id, segredo := contaRecuperacaoMFA(t, db, "desligar-exigido-"+papel+"@empresa.com", papel, true)
		err := DesligarMFAPropria(db, id, papel, true, "senha-123456", codigoTOTPValidoAgora(t, segredo))
		if !errors.Is(err, ErrMFAExigidoPelaEmpresa) {
			t.Fatalf("%s: err = %v, want ErrMFAExigidoPelaEmpresa", papel, err)
		}
		if e := lerEstadoMFA(t, db, id); !e.habilitado || e.tentativas != 0 {
			t.Errorf("%s: estado = %+v, want MFA intacto e tentativas=0", papel, e)
		}
	}
	// `usuario` numa Empresa que exige pode desligar: a exigência é só de gestor/adm.
	id, segredo := contaRecuperacaoMFA(t, db, "desligar-exigido-usuario@empresa.com", PapelUsuario, true)
	if err := DesligarMFAPropria(db, id, PapelUsuario, true, "senha-123456", codigoTOTPValidoAgora(t, segredo)); err != nil {
		t.Errorf("usuario em Empresa que exige: err = %v, want nil", err)
	}
}

func TestDesligarMFAPropria_SemMFA(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	id, _ := contaRecuperacaoMFA(t, db, "desligar-semmfa@empresa.com", PapelUsuario, false)
	if err := DesligarMFAPropria(db, id, PapelUsuario, false, "senha-123456", "123456"); !errors.Is(err, ErrMFANaoConfigurado) {
		t.Fatalf("err = %v, want ErrMFANaoConfigurado", err)
	}
}

func TestDesligarMFAPropria_BloqueioNaQuintaFalha(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	id, segredo := contaRecuperacaoMFA(t, db, "desligar-bloqueio@empresa.com", PapelUsuario, true)

	for i := 1; i <= maxTentativasLogin; i++ {
		err := DesligarMFAPropria(db, id, PapelUsuario, false, "senha-errada1", codigoTOTPValidoAgora(t, segredo))
		if !errors.Is(err, ErrCredenciaisInvalidas) {
			t.Fatalf("falha %d: err = %v, want ErrCredenciaisInvalidas", i, err)
		}
	}
	e := lerEstadoMFA(t, db, id)
	if !e.bloqueado.Valid || !e.bloqueado.Time.After(time.Now()) {
		t.Fatalf("bloqueado_ate = %v, want futuro após a 5ª falha", e.bloqueado)
	}
	err := DesligarMFAPropria(db, id, PapelUsuario, false, "senha-123456", codigoTOTPValidoAgora(t, segredo))
	if !errors.Is(err, ErrContaBloqueada) {
		t.Fatalf("6ª chamada: err = %v, want ErrContaBloqueada", err)
	}
	if !lerEstadoMFA(t, db, id).habilitado {
		t.Error("MFA desligado apesar do bloqueio")
	}
}

func TestDesligarMFAPropria_BloqueioExpiradoEDestravado(t *testing.T) {
	db := testDB(t)
	restaurarExigenciaEmpresaTeste(t, db)
	id, segredo := contaRecuperacaoMFA(t, db, "desligar-expirado@empresa.com", PapelUsuario, true)
	if _, err := db.Exec(`UPDATE usuarios SET tentativas_login_falhas = 5, bloqueado_ate = now() - interval '1 minute' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := DesligarMFAPropria(db, id, PapelUsuario, false, "senha-123456", codigoTOTPValidoAgora(t, segredo)); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if e := lerEstadoMFA(t, db, id); e.habilitado || e.tentativas != 0 || e.bloqueado.Valid {
		t.Errorf("estado = %+v", e)
	}
}

// --- RedefinirSenha preserva o MFA ------------------------------------------

func TestRedefinirSenha_PreservaMFA(t *testing.T) {
	db := testDB(t)
	id, segredo := contaRecuperacaoMFA(t, db, "redefinir-preserva-mfa@empresa.com", PapelGestor, true)
	if err := SolicitarRedefinicaoSenha(db, testEmailCfg, empresaTeste, slugEmpresaTeste, "redefinir-preserva-mfa@empresa.com"); err != nil {
		t.Fatalf("SolicitarRedefinicaoSenha: %v", err)
	}
	var token string
	if err := db.QueryRow(`SELECT token FROM tokens_acao WHERE usuario_id = $1 AND tipo = 'redefinicao_senha'`, id).Scan(&token); err != nil {
		t.Fatal(err)
	}
	if err := RedefinirSenha(db, empresaTeste, token, "nova-senha1"); err != nil {
		t.Fatalf("RedefinirSenha: %v", err)
	}
	e := lerEstadoMFA(t, db, id)
	if !e.habilitado || e.segredo.String != segredo {
		t.Errorf("MFA alterado pela redefinição de senha: %+v", e)
	}
}
