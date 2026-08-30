package services

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

// semearSessao insere uma sessão viva (revogado_em NULL) para a conta —
// usado para provar que a desativação revoga as sessões e o rebaixamento não.
func semearSessao(t *testing.T, db *sql.DB, usuarioID, refreshToken string) string {
	t.Helper()
	var id string
	const insert = `
		INSERT INTO sessoes (usuario_id, refresh_token, expira_em)
		VALUES ($1, $2, now() + interval '2 hours')
		RETURNING id`
	if err := db.QueryRow(insert, usuarioID, refreshToken).Scan(&id); err != nil {
		t.Fatalf("falha ao semear sessão para %s: %v", usuarioID, err)
	}
	return id
}

func contaAtiva(t *testing.T, db *sql.DB, id string) bool {
	t.Helper()
	var ativo bool
	if err := db.QueryRow(`SELECT ativo FROM usuarios WHERE id = $1`, id).Scan(&ativo); err != nil {
		t.Fatalf("falha ao ler ativo da conta %s: %v", id, err)
	}
	return ativo
}

func sessoesVivas(t *testing.T, db *sql.DB, usuarioID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT count(*) FROM sessoes WHERE usuario_id = $1 AND revogado_em IS NULL`, usuarioID,
	).Scan(&n); err != nil {
		t.Fatalf("falha ao contar sessões vivas de %s: %v", usuarioID, err)
	}
	return n
}

// --- papelImediatamenteAbaixo ------------------------------------------------

func TestPapelImediatamenteAbaixo(t *testing.T) {
	casos := []struct {
		papel      string
		wantAbaixo string
		wantOk     bool
	}{
		{PapelGestor, PapelAlmoxarife, true},
		{PapelAlmoxarife, PapelUsuario, true},
		{PapelUsuario, "", false},
		{PapelAdm, "", false},
		{"", "", false},
		{"desconhecido", "", false},
	}
	for _, c := range casos {
		abaixo, ok := papelImediatamenteAbaixo(c.papel)
		if abaixo != c.wantAbaixo || ok != c.wantOk {
			t.Errorf("papelImediatamenteAbaixo(%q) = (%q, %v), want (%q, %v)", c.papel, abaixo, ok, c.wantAbaixo, c.wantOk)
		}
	}
}

// --- AlterarAtivacaoUsuario ------------------------------------------------

// TestAlterarAtivacaoUsuario_DesativaERevogaSessoes prova a linha "Desativar
// almoxarife por gestor": ativo=false e TODAS as sessões vivas do alvo ficam
// com revogado_em preenchido, na mesma transação.
func TestAlterarAtivacaoUsuario_DesativaERevogaSessoes(t *testing.T) {
	db := testDB(t)
	ator := semearConta(t, db, "Gestora", "gestor-desativa@empresa.com", PapelGestor, 1)
	alvo := semearConta(t, db, "Almox", "almox-alvo@empresa.com", PapelAlmoxarife, 2)
	semearSessao(t, db, alvo, "refresh-vivo-1")
	semearSessao(t, db, alvo, "refresh-vivo-2")
	// Sessão de outra conta não deve ser tocada.
	outra := semearConta(t, db, "Outra", "outra-conta@empresa.com", PapelUsuario, 3)
	semearSessao(t, db, outra, "refresh-outra")

	u, err := AlterarAtivacaoUsuario(db, alvo, ator, PapelGestor, false)
	if err != nil {
		t.Fatalf("AlterarAtivacaoUsuario erro inesperado: %v", err)
	}
	if u.Ativo {
		t.Errorf("resposta.Ativo = true, want false")
	}
	if u.ID != alvo {
		t.Errorf("resposta.ID = %q, want %q", u.ID, alvo)
	}
	if contaAtiva(t, db, alvo) {
		t.Errorf("conta alvo continua ativa após desativação")
	}
	if n := sessoesVivas(t, db, alvo); n != 0 {
		t.Errorf("sessões vivas do alvo = %d, want 0 (todas revogadas)", n)
	}
	if n := sessoesVivas(t, db, outra); n != 1 {
		t.Errorf("sessões vivas de outra conta = %d, want 1 (intacta)", n)
	}
}

// TestAlterarAtivacaoUsuario_Reativa prova a linha "Reativar conta por gestor":
// ativo=true numa conta inativa; não mexe em sessões.
func TestAlterarAtivacaoUsuario_Reativa(t *testing.T) {
	db := testDB(t)
	ator := semearConta(t, db, "Gestora", "gestor-reativa@empresa.com", PapelGestor, 1)
	alvo := semearConta(t, db, "Inativo", "inativo-alvo@empresa.com", PapelAlmoxarife, 2)
	if _, err := db.Exec(`UPDATE usuarios SET ativo = false WHERE id = $1`, alvo); err != nil {
		t.Fatalf("forçar conta inativa: %v", err)
	}

	u, err := AlterarAtivacaoUsuario(db, alvo, ator, PapelGestor, true)
	if err != nil {
		t.Fatalf("AlterarAtivacaoUsuario erro inesperado: %v", err)
	}
	if !u.Ativo {
		t.Errorf("resposta.Ativo = false, want true")
	}
	if !contaAtiva(t, db, alvo) {
		t.Errorf("conta alvo continua inativa após reativação")
	}
}

// TestAlterarAtivacaoUsuario_GestorSobreGestorOuAdm prova as linhas
// "Desativar/rebaixar por gestor sobre gestor" e "por gestor sobre adm":
// ErrGestaoForaDeEscopo, nada muda (usuarios e sessoes intactos).
func TestAlterarAtivacaoUsuario_GestorSobreGestorOuAdm(t *testing.T) {
	db := testDB(t)
	ator := semearConta(t, db, "Gestora", "gestor-escopo@empresa.com", PapelGestor, 1)

	for _, papelAlvo := range []string{PapelGestor, PapelAdm} {
		t.Run(papelAlvo, func(t *testing.T) {
			alvo := semearConta(t, db, "Alvo "+papelAlvo, papelAlvo+"-fora-escopo@empresa.com", papelAlvo, 2)
			semearSessao(t, db, alvo, "refresh-"+papelAlvo)

			_, err := AlterarAtivacaoUsuario(db, alvo, ator, PapelGestor, false)
			if !errors.Is(err, ErrGestaoForaDeEscopo) {
				t.Fatalf("erro = %v, want ErrGestaoForaDeEscopo", err)
			}
			if !contaAtiva(t, db, alvo) {
				t.Errorf("conta alvo foi desativada — não deveria")
			}
			if n := sessoesVivas(t, db, alvo); n != 1 {
				t.Errorf("sessões vivas do alvo = %d, want 1 (intacta)", n)
			}
		})
	}
}

// TestAlterarAtivacaoUsuario_AdmSobreGestor prova a linha "Desativar/rebaixar
// por adm sobre gestor": 200, ação aplicada.
func TestAlterarAtivacaoUsuario_AdmSobreGestor(t *testing.T) {
	db := testDB(t)
	ator := semearConta(t, db, "Adm", "adm-sobre-gestor@empresa.com", PapelAdm, 1)
	alvo := semearConta(t, db, "Gestor alvo", "gestor-alvo-adm@empresa.com", PapelGestor, 2)
	semearSessao(t, db, alvo, "refresh-gestor-adm")

	u, err := AlterarAtivacaoUsuario(db, alvo, ator, PapelAdm, false)
	if err != nil {
		t.Fatalf("AlterarAtivacaoUsuario erro inesperado: %v", err)
	}
	if u.Ativo {
		t.Errorf("resposta.Ativo = true, want false")
	}
	if n := sessoesVivas(t, db, alvo); n != 0 {
		t.Errorf("sessões vivas do alvo = %d, want 0", n)
	}
}

// TestAlterarAtivacaoUsuario_AutoAcao prova a linha "Ação sobre a própria
// conta": alvoID == atorID -> ErrGestaoForaDeEscopo, nada muda (mesmo para adm).
func TestAlterarAtivacaoUsuario_AutoAcao(t *testing.T) {
	db := testDB(t)
	for _, papel := range []string{PapelGestor, PapelAdm} {
		t.Run(papel, func(t *testing.T) {
			id := semearConta(t, db, "Eu "+papel, papel+"-auto-acao@empresa.com", papel, 1)
			semearSessao(t, db, id, "refresh-auto-"+papel)

			_, err := AlterarAtivacaoUsuario(db, id, id, papel, false)
			if !errors.Is(err, ErrGestaoForaDeEscopo) {
				t.Fatalf("erro = %v, want ErrGestaoForaDeEscopo", err)
			}
			if !contaAtiva(t, db, id) {
				t.Errorf("a própria conta foi desativada — não deveria")
			}
			if n := sessoesVivas(t, db, id); n != 1 {
				t.Errorf("sessões vivas = %d, want 1 (intacta)", n)
			}
		})
	}
}

// TestAlterarAtivacaoUsuario_Inexistente prova a linha "id inexistente":
// uuid válido sem linha -> ErrContaNaoEncontrada.
func TestAlterarAtivacaoUsuario_Inexistente(t *testing.T) {
	db := testDB(t)
	ator := semearConta(t, db, "Gestora", "gestor-404@empresa.com", PapelGestor, 1)

	_, err := AlterarAtivacaoUsuario(db, "00000000-0000-0000-0000-000000000000", ator, PapelGestor, false)
	if !errors.Is(err, ErrContaNaoEncontrada) {
		t.Fatalf("erro = %v, want ErrContaNaoEncontrada", err)
	}
}

// TestAlterarAtivacaoUsuario_IDMalformado prova a linha "id malformado":
// path não-uuid (`pq` 22P02) tratado como ErrContaNaoEncontrada.
func TestAlterarAtivacaoUsuario_IDMalformado(t *testing.T) {
	db := testDB(t)
	ator := semearConta(t, db, "Gestora", "gestor-malformado@empresa.com", PapelGestor, 1)

	_, err := AlterarAtivacaoUsuario(db, "nao-e-uuid", ator, PapelGestor, false)
	if !errors.Is(err, ErrContaNaoEncontrada) {
		t.Fatalf("erro = %v, want ErrContaNaoEncontrada", err)
	}
}

// TestAlterarAtivacaoUsuario_CorridaGuardaPapelAtual prova que a desativação é
// simétrica ao rebaixamento (AC3): o guard `AND papel = $3` fecha a janela
// entre o SELECT sem lock de carregarAlvoParaGestao e a escrita. Uma transação
// externa trava a linha do alvo e o promove de `almoxarife` a `gestor` DEPOIS
// que o serviço já leu `almoxarife` — o UPDATE guardado afeta 0 linhas ->
// ErrEstadoContaMudou, a conta segue `ativo=true` e nenhuma sessão é revogada
// (o guard vem antes do `UPDATE sessoes`).
func TestAlterarAtivacaoUsuario_CorridaGuardaPapelAtual(t *testing.T) {
	db := testDB(t)
	ator := semearConta(t, db, "Gestora", "gestor-desat-corrida@empresa.com", PapelGestor, 1)
	alvo := semearConta(t, db, "Almox volátil", "almox-volatil@empresa.com", PapelAlmoxarife, 2)
	semearSessao(t, db, alvo, "refresh-desat-corrida")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin externo: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	var travado string
	if err := tx.QueryRow(`SELECT papel FROM usuarios WHERE id = $1 FOR UPDATE`, alvo).Scan(&travado); err != nil {
		t.Fatalf("lock da linha do alvo: %v", err)
	}

	resultado := make(chan error, 1)
	go func() {
		// AlterarAtivacaoUsuario lê `almoxarife` (SELECT sem lock), abre a
		// própria transação e bloqueia no UPDATE guardado esperando o lock.
		_, e := AlterarAtivacaoUsuario(db, alvo, ator, PapelGestor, false)
		resultado <- e
	}()

	time.Sleep(150 * time.Millisecond)
	if _, err := tx.Exec(`UPDATE usuarios SET papel = $1 WHERE id = $2`, PapelGestor, alvo); err != nil {
		t.Fatalf("promoção na transação externa: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit externo: %v", err)
	}

	err = <-resultado
	if !errors.Is(err, ErrEstadoContaMudou) {
		t.Fatalf("erro = %v, want ErrEstadoContaMudou", err)
	}
	if !contaAtiva(t, db, alvo) {
		t.Errorf("a conta foi desativada apesar da corrida perdida")
	}
	if n := sessoesVivas(t, db, alvo); n != 1 {
		t.Errorf("sessões vivas do alvo = %d, want 1 (nada revogado numa corrida perdida)", n)
	}
}

// --- RebaixarUsuario ----------------------------------------------------------

// TestRebaixarUsuario_GestorParaAlmoxarifePorAdm prova a linha "Rebaixar gestor
// por adm": papel vira `almoxarife`; sessões NÃO são revogadas.
func TestRebaixarUsuario_GestorParaAlmoxarifePorAdm(t *testing.T) {
	db := testDB(t)
	ator := semearConta(t, db, "Adm", "adm-rebaixa-g@empresa.com", PapelAdm, 1)
	alvo := semearConta(t, db, "Gestor alvo", "gestor-rebaixado@empresa.com", PapelGestor, 2)
	semearSessao(t, db, alvo, "refresh-rebaixa-g")

	u, err := RebaixarUsuario(db, alvo, ator, PapelAdm)
	if err != nil {
		t.Fatalf("RebaixarUsuario erro inesperado: %v", err)
	}
	if u.Papel != PapelAlmoxarife {
		t.Errorf("resposta.Papel = %q, want almoxarife", u.Papel)
	}
	if papelDaConta(t, db, alvo) != PapelAlmoxarife {
		t.Errorf("papel persistido = %q, want almoxarife", papelDaConta(t, db, alvo))
	}
	if n := sessoesVivas(t, db, alvo); n != 1 {
		t.Errorf("sessões vivas do alvo = %d, want 1 (rebaixamento não revoga)", n)
	}
}

// TestRebaixarUsuario_AlmoxarifeParaUsuarioPorGestor prova a linha "Rebaixar
// almoxarife por gestor": papel vira `usuario`.
func TestRebaixarUsuario_AlmoxarifeParaUsuarioPorGestor(t *testing.T) {
	db := testDB(t)
	ator := semearConta(t, db, "Gestora", "gestor-rebaixa-a@empresa.com", PapelGestor, 1)
	alvo := semearConta(t, db, "Almox alvo", "almox-rebaixado@empresa.com", PapelAlmoxarife, 2)

	u, err := RebaixarUsuario(db, alvo, ator, PapelGestor)
	if err != nil {
		t.Fatalf("RebaixarUsuario erro inesperado: %v", err)
	}
	if u.Papel != PapelUsuario {
		t.Errorf("resposta.Papel = %q, want usuario", u.Papel)
	}
	if papelDaConta(t, db, alvo) != PapelUsuario {
		t.Errorf("papel persistido = %q, want usuario", papelDaConta(t, db, alvo))
	}
}

// TestRebaixarUsuario_JaUsuario prova a linha "Rebaixar conta já usuario":
// sem papel abaixo -> ErrRebaixamentoIndisponivel, nada muda.
func TestRebaixarUsuario_JaUsuario(t *testing.T) {
	db := testDB(t)
	ator := semearConta(t, db, "Gestora", "gestor-rebaixa-u@empresa.com", PapelGestor, 1)
	alvo := semearConta(t, db, "Usuário alvo", "usuario-piso@empresa.com", PapelUsuario, 2)

	_, err := RebaixarUsuario(db, alvo, ator, PapelGestor)
	if !errors.Is(err, ErrRebaixamentoIndisponivel) {
		t.Fatalf("erro = %v, want ErrRebaixamentoIndisponivel", err)
	}
	if papelDaConta(t, db, alvo) != PapelUsuario {
		t.Errorf("papel mudou para %q — não deveria", papelDaConta(t, db, alvo))
	}
}

// TestRebaixarUsuario_GestorSobreGestor prova que o recorte de escopo também
// vale no rebaixamento: gestor sobre gestor -> ErrGestaoForaDeEscopo.
func TestRebaixarUsuario_GestorSobreGestor(t *testing.T) {
	db := testDB(t)
	ator := semearConta(t, db, "Gestora", "gestor-reb-escopo@empresa.com", PapelGestor, 1)
	alvo := semearConta(t, db, "Gestor alvo", "gestor-reb-alvo@empresa.com", PapelGestor, 2)

	_, err := RebaixarUsuario(db, alvo, ator, PapelGestor)
	if !errors.Is(err, ErrGestaoForaDeEscopo) {
		t.Fatalf("erro = %v, want ErrGestaoForaDeEscopo", err)
	}
	if papelDaConta(t, db, alvo) != PapelGestor {
		t.Errorf("papel do alvo mudou — não deveria")
	}
}

// TestRebaixarUsuario_AutoAcao prova a guarda de auto-ação no rebaixamento.
func TestRebaixarUsuario_AutoAcao(t *testing.T) {
	db := testDB(t)
	id := semearConta(t, db, "Adm", "adm-reb-auto@empresa.com", PapelAdm, 1)

	_, err := RebaixarUsuario(db, id, id, PapelAdm)
	if !errors.Is(err, ErrGestaoForaDeEscopo) {
		t.Fatalf("erro = %v, want ErrGestaoForaDeEscopo", err)
	}
	if papelDaConta(t, db, id) != PapelAdm {
		t.Errorf("a própria conta foi rebaixada — não deveria")
	}
}

// TestRebaixarUsuario_Inexistente prova a linha "id inexistente" no
// rebaixamento.
func TestRebaixarUsuario_Inexistente(t *testing.T) {
	db := testDB(t)
	ator := semearConta(t, db, "Gestora", "gestor-reb-404@empresa.com", PapelGestor, 1)

	_, err := RebaixarUsuario(db, "00000000-0000-0000-0000-000000000000", ator, PapelGestor)
	if !errors.Is(err, ErrContaNaoEncontrada) {
		t.Fatalf("erro = %v, want ErrContaNaoEncontrada", err)
	}
}

// TestRebaixarUsuario_CorridaGuardaPapelAtual prova a linha "Rebaixar, papel do
// alvo mudou na corrida": o guard `AND papel = $3` do UPDATE fecha a janela
// entre o SELECT inicial do serviço e a sua escrita. Uma transação externa
// trava a linha do alvo e troca o papel DEPOIS que o serviço já leu `gestor`,
// forçando o UPDATE guardado a afetar 0 linhas -> ErrEstadoContaMudou, com a
// conta intacta (o papel final é o que a transação externa deixou).
func TestRebaixarUsuario_CorridaGuardaPapelAtual(t *testing.T) {
	db := testDB(t)
	ator := semearConta(t, db, "Adm", "adm-reb-corrida@empresa.com", PapelAdm, 1)
	alvo := semearConta(t, db, "Gestor disputado", "gestor-disputado@empresa.com", PapelGestor, 2)

	// Transação externa segura o lock da linha do alvo.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin externo: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	var travado string
	if err := tx.QueryRow(`SELECT papel FROM usuarios WHERE id = $1 FOR UPDATE`, alvo).Scan(&travado); err != nil {
		t.Fatalf("lock da linha do alvo: %v", err)
	}

	resultado := make(chan error, 1)
	go func() {
		// RebaixarUsuario faz um SELECT sem lock (lê `gestor`), abre a própria
		// transação e bloqueia no UPDATE guardado esperando o lock externo.
		_, e := RebaixarUsuario(db, alvo, ator, PapelAdm)
		resultado <- e
	}()

	// Dá tempo do SELECT inicial do serviço rodar; então muda o papel e
	// solta o lock — o UPDATE guardado do serviço destrava e vê `almoxarife`.
	time.Sleep(150 * time.Millisecond)
	if _, err := tx.Exec(`UPDATE usuarios SET papel = $1 WHERE id = $2`, PapelAlmoxarife, alvo); err != nil {
		t.Fatalf("mudança de papel na transação externa: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit externo: %v", err)
	}

	err = <-resultado
	if !errors.Is(err, ErrEstadoContaMudou) {
		t.Fatalf("erro = %v, want ErrEstadoContaMudou", err)
	}
	if got := papelDaConta(t, db, alvo); got != PapelAlmoxarife {
		t.Errorf("papel do alvo = %q, want almoxarife (a escrita do serviço não pegou)", got)
	}
}
