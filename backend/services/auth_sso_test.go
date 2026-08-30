package services

import (
	"database/sql"
	"errors"
	"testing"
)

func inserirUsuario(t *testing.T, db *sql.DB, nome, email, papel string, ativo bool) string {
	t.Helper()
	var id string
	const q = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
		VALUES ($1, $2, NULL, $3, true, $4)
		RETURNING id`
	if err := db.QueryRow(q, nome, email, papel, ativo).Scan(&id); err != nil {
		t.Fatalf("inserir usuario %q: %v", email, err)
	}
	return id
}

func TestBuscarUsuarioPorEmailSSO_CaseInsensitive(t *testing.T) {
	db := testDB(t)
	id := inserirUsuario(t, db, "Carlos", "carlos@fc.com", "gestor", true)

	got, err := BuscarUsuarioPorEmailSSO(db, "Carlos@FC.com")
	if err != nil {
		t.Fatalf("BuscarUsuarioPorEmailSSO: %v", err)
	}
	if got.ID != id {
		t.Fatalf("ID = %q, want %q (match deveria ser case-insensitive)", got.ID, id)
	}
	if got.Papel != "gestor" {
		t.Fatalf("Papel = %q, want gestor (sempre do banco)", got.Papel)
	}

	if n := contarLinhas(t, db, "usuarios"); n != 1 {
		t.Fatalf("linhas em usuarios = %d, want 1 (SSO nunca cria conta)", n)
	}
}

func TestBuscarUsuarioPorEmailSSO_SemConta(t *testing.T) {
	db := testDB(t)

	_, err := BuscarUsuarioPorEmailSSO(db, "ninguem@fc.com")
	if !errors.Is(err, ErrContaSSONaoEncontrada) {
		t.Fatalf("err = %v, want ErrContaSSONaoEncontrada", err)
	}
	if n := contarLinhas(t, db, "usuarios"); n != 0 {
		t.Fatalf("linhas em usuarios = %d, want 0 (nada criado)", n)
	}
}

func TestBuscarUsuarioPorEmailSSO_DevolveContaDesativada(t *testing.T) {
	db := testDB(t)
	inserirUsuario(t, db, "Inativa", "inativa@fc.com", "usuario", false)

	got, err := BuscarUsuarioPorEmailSSO(db, "inativa@fc.com")
	if err != nil {
		t.Fatalf("BuscarUsuarioPorEmailSSO: %v — a conta desativada deve ser devolvida (o handler decide o 401)", err)
	}
	if got.Ativo {
		t.Fatal("Ativo = true, want false")
	}
}

func TestRevogarSessaoPorRefreshToken_RevogaSessaoViva(t *testing.T) {
	db := testDB(t)
	usuarioID := inserirUsuario(t, db, "Com Sessão", "sessao@fc.com", "usuario", true)

	_, refreshToken, _, err := EmitirSessao(db, []byte("segredo-de-teste"), usuarioID, "sso")
	if err != nil {
		t.Fatalf("EmitirSessao: %v", err)
	}

	if err := RevogarSessaoPorRefreshToken(db, refreshToken); err != nil {
		t.Fatalf("RevogarSessaoPorRefreshToken: %v", err)
	}

	var revogadoEm sql.NullTime
	if err := db.QueryRow(`SELECT revogado_em FROM sessoes WHERE refresh_token = $1`, refreshToken).Scan(&revogadoEm); err != nil {
		t.Fatalf("consulta sessão: %v", err)
	}
	if !revogadoEm.Valid {
		t.Fatal("revogado_em nulo — a sessão viva deveria ter sido revogada")
	}
}

func TestRevogarSessaoPorRefreshToken_NoOpToleranteNaoErra(t *testing.T) {
	db := testDB(t)

	if err := RevogarSessaoPorRefreshToken(db, ""); err != nil {
		t.Fatalf("token vazio deveria ser no-op, got %v", err)
	}
	if err := RevogarSessaoPorRefreshToken(db, "token-que-nao-existe"); err != nil {
		t.Fatalf("token inexistente (0 linhas) não é erro, got %v", err)
	}

	// Revogar duas vezes: a segunda afeta 0 linhas e ainda assim não erra.
	usuarioID := inserirUsuario(t, db, "Dupla", "dupla@fc.com", "usuario", true)
	_, refreshToken, _, err := EmitirSessao(db, []byte("segredo-de-teste"), usuarioID, "senha")
	if err != nil {
		t.Fatalf("EmitirSessao: %v", err)
	}
	if err := RevogarSessaoPorRefreshToken(db, refreshToken); err != nil {
		t.Fatalf("primeira revogação: %v", err)
	}
	if err := RevogarSessaoPorRefreshToken(db, refreshToken); err != nil {
		t.Fatalf("segunda revogação (idempotente): %v", err)
	}
}
