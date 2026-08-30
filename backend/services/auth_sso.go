package services

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrContaSSONaoEncontrada indica que nenhuma linha de `usuarios` casa
// (case-insensitive) com o e-mail vindo do token do Keycloak. O login
// federado NUNCA cria conta (Story 1.9, epic-context) — o handler mapeia isto
// para 401 SSO_SEM_CONTA orientando o cadastro.
var ErrContaSSONaoEncontrada = errors.New("nenhuma conta local para o e-mail do SSO")

// BuscarUsuarioPorEmailSSO resolve a conta local a partir do e-mail do token
// federado, comparando por lower(email) (a mesma normalização com que
// `usuarios.email` é gravado — molde de Login, auth.go). Devolve a conta
// mesmo com ativo=false: quem decide o 401 de conta desativada é o handler.
func BuscarUsuarioPorEmailSSO(db *sql.DB, email string) (UsuarioSessao, error) {
	var u UsuarioSessao
	const q = `
		SELECT id, nome, email, papel, ativo, mfa_habilitado
		FROM usuarios
		WHERE lower(email) = lower($1)`
	err := db.QueryRow(q, email).Scan(&u.ID, &u.Nome, &u.Email, &u.Papel, &u.Ativo, &u.MFAHabilitado)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UsuarioSessao{}, ErrContaSSONaoEncontrada
		}
		return UsuarioSessao{}, fmt.Errorf("falha ao consultar usuário por e-mail (SSO): %w", err)
	}
	return u, nil
}

// RevogarSessaoPorRefreshToken marca revogada a sessão viva do refresh token
// informado (molde do revoke em RedefinirSenha, auth.go). Tolerante por
// design: token vazio é no-op; zero linhas afetadas (cookie já revogado ou
// inexistente) NÃO é erro — o LogoutHandler é idempotente.
func RevogarSessaoPorRefreshToken(db *sql.DB, token string) error {
	if token == "" {
		return nil
	}
	const q = `
		UPDATE sessoes
		SET revogado_em = now()
		WHERE refresh_token = $1 AND revogado_em IS NULL`
	if _, err := db.Exec(q, token); err != nil {
		return fmt.Errorf("falha ao revogar sessão por refresh token: %w", err)
	}
	return nil
}
