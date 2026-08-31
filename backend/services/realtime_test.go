package services

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

// --- Story 4.4: Detalhe do produto por Estoque com atualização em tempo
// real -----------------------------------------------------------------
//
// seedUsuarioRealtime/Cadastrar (auth_test.go) seedam a conta cujo id
// satisfaz a FK tokens_acao.usuario_id — os testes abaixo não dependem de
// email_verificado/ativo (essas regras são de RequireAuth, exercitadas em
// middleware/handlers, não aqui).

func seedUsuarioRealtime(t *testing.T, db *sql.DB, email string) string {
	t.Helper()
	usuarioID, err := Cadastrar(db, testEmailCfg, "Usuário Realtime", email, "senha-123456")
	if err != nil {
		t.Fatalf("seed Cadastrar: %v", err)
	}
	return usuarioID
}

// TestEmitirTicketRealtime_GeraTokenValido prova a emissão: um token
// url-safe de 43 caracteres (mesmo formato de gerarTokenAcao), gravado como
// `tipo='realtime_ticket'`, não usado, expirando ~30s à frente.
func TestEmitirTicketRealtime_GeraTokenValido(t *testing.T) {
	db := testDB(t)
	usuarioID := seedUsuarioRealtime(t, db, "realtime-emitir@empresa.com")

	token, err := EmitirTicketRealtime(db, usuarioID)
	if err != nil {
		t.Fatalf("EmitirTicketRealtime: %v", err)
	}
	if len(token) != 43 {
		t.Errorf("len(token) = %d, want 43", len(token))
	}

	var tipo string
	var usadoEm []byte
	var expiraEm time.Time
	if err := db.QueryRow(
		`SELECT tipo, usado_em, expira_em FROM tokens_acao WHERE token = $1`, token,
	).Scan(&tipo, &usadoEm, &expiraEm); err != nil {
		t.Fatalf("falha ao reler ticket: %v", err)
	}
	if tipo != "realtime_ticket" {
		t.Errorf("tipo = %q, want realtime_ticket", tipo)
	}
	if usadoEm != nil {
		t.Errorf("usado_em = %v, want NULL (ticket recém-emitido)", usadoEm)
	}
	if margem := expiraEm.Sub(time.Now().UTC()); margem <= 0 || margem > realtimeTicketExpiracao+time.Second {
		t.Errorf("expira_em fora da janela esperada (~%s à frente): margem = %s", realtimeTicketExpiracao, margem)
	}
}

// TestEmitirTicketRealtime_NaoInvalidaTicketsAnteriores prova a diferença
// deliberada em relação a IniciarLoginMFA/SolicitarRedefinicaoSenha: emitir
// um segundo ticket para a MESMA conta não invalida o primeiro — cada aba
// precisa do seu próprio ticket simultâneo (Boundaries da spec-4-4).
func TestEmitirTicketRealtime_NaoInvalidaTicketsAnteriores(t *testing.T) {
	db := testDB(t)
	usuarioID := seedUsuarioRealtime(t, db, "realtime-multiplas-abas@empresa.com")

	primeiro, err := EmitirTicketRealtime(db, usuarioID)
	if err != nil {
		t.Fatalf("EmitirTicketRealtime (1º): %v", err)
	}
	if _, err := EmitirTicketRealtime(db, usuarioID); err != nil {
		t.Fatalf("EmitirTicketRealtime (2º): %v", err)
	}

	var usadoEm []byte
	if err := db.QueryRow(`SELECT usado_em FROM tokens_acao WHERE token = $1`, primeiro).Scan(&usadoEm); err != nil {
		t.Fatalf("falha ao reler primeiro ticket: %v", err)
	}
	if usadoEm != nil {
		t.Error("primeiro ticket foi invalidado pela emissão do segundo — não deveria (múltiplas abas)")
	}

	// O primeiro ticket ainda deve ser consumível normalmente.
	consumidor, err := ConsumirTicketRealtime(db, primeiro)
	if err != nil {
		t.Fatalf("ConsumirTicketRealtime(primeiro): %v", err)
	}
	if consumidor != usuarioID {
		t.Errorf("usuarioID = %q, want %q", consumidor, usuarioID)
	}
}

// TestConsumirTicketRealtime_UsoUnico prova o consumo atômico: a primeira
// chamada resolve o usuarioID e marca o ticket usado; a segunda tentativa
// com o MESMO token falha com ErrTokenExpirado (mesmo vocabulário de
// "expirado ou já usado" de VerificarEmail).
func TestConsumirTicketRealtime_UsoUnico(t *testing.T) {
	db := testDB(t)
	usuarioID := seedUsuarioRealtime(t, db, "realtime-uso-unico@empresa.com")
	token, err := EmitirTicketRealtime(db, usuarioID)
	if err != nil {
		t.Fatalf("EmitirTicketRealtime: %v", err)
	}

	got, err := ConsumirTicketRealtime(db, token)
	if err != nil {
		t.Fatalf("1ª chamada: %v", err)
	}
	if got != usuarioID {
		t.Errorf("usuarioID = %q, want %q", got, usuarioID)
	}

	_, err = ConsumirTicketRealtime(db, token)
	if !errors.Is(err, ErrTokenExpirado) {
		t.Fatalf("2ª chamada: erro = %v, want ErrTokenExpirado", err)
	}
}

// TestConsumirTicketRealtime_Expirado prova que um ticket com `expira_em`
// no passado (>30s) é recusado com ErrTokenExpirado, mesmo nunca usado.
func TestConsumirTicketRealtime_Expirado(t *testing.T) {
	db := testDB(t)
	usuarioID := seedUsuarioRealtime(t, db, "realtime-expirado@empresa.com")
	token, err := EmitirTicketRealtime(db, usuarioID)
	if err != nil {
		t.Fatalf("EmitirTicketRealtime: %v", err)
	}
	if _, err := db.Exec(`UPDATE tokens_acao SET expira_em = now() - interval '1 second' WHERE token = $1`, token); err != nil {
		t.Fatalf("falha ao forçar expiração: %v", err)
	}

	_, err = ConsumirTicketRealtime(db, token)
	if !errors.Is(err, ErrTokenExpirado) {
		t.Fatalf("erro = %v, want ErrTokenExpirado", err)
	}
}

// TestConsumirTicketRealtime_Inexistente prova que um token que nunca foi
// emitido devolve ErrTokenNaoEncontrado — distinto de ErrTokenExpirado,
// mesma distinção de VerificarEmail.
func TestConsumirTicketRealtime_Inexistente(t *testing.T) {
	db := testDB(t)
	_, err := ConsumirTicketRealtime(db, "token-que-nunca-existiu")
	if !errors.Is(err, ErrTokenNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrTokenNaoEncontrado", err)
	}
}
