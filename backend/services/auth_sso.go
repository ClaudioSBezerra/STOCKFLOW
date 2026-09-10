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

// BuscarUsuarioPorEmailSSO resolve a conta local DA EMPRESA `empresaID` a
// partir do e-mail do token federado, comparando por `lower(email)` E
// `empresa_id` (a mesma normalização com que `usuarios.email` é gravado —
// molde de Login, auth.go; o recorte por Empresa é da Story 9.1: o mesmo
// e-mail corporativo pode existir em Empresas diferentes e o SSO nunca
// pergunta qual — ela vem do slug da URL). Devolve a conta
// mesmo com ativo=false: quem decide o 401 de conta desativada é o handler.
func BuscarUsuarioPorEmailSSO(db *sql.DB, empresaID string, email string) (UsuarioSessao, error) {
	var u UsuarioSessao
	var empresaDaConta sql.NullString
	const q = `
		SELECT id, nome, email, papel, ativo, mfa_habilitado, empresa_id
		FROM usuarios
		WHERE lower(email) = lower($1) AND empresa_id = $2`
	err := db.QueryRow(q, email, empresaID).Scan(&u.ID, &u.Nome, &u.Email, &u.Papel, &u.Ativo, &u.MFAHabilitado, &empresaDaConta)
	u.EmpresaID = empresaDaConta.String
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
func RevogarSessaoPorRefreshToken(db *sql.DB, empresaID string, token string) error {
	if token == "" {
		return nil
	}
	// Guard de Empresa por `EXISTS` sobre o dono da sessão (Story 9.1), mesmo
	// molde de RenovarSessao: um refresh token de outra Empresa não é revogado
	// sob este slug — e o logout continua idempotente (0 linhas não é erro).
	const q = `
		UPDATE sessoes
		SET revogado_em = now()
		WHERE refresh_token = $1 AND revogado_em IS NULL
		  AND EXISTS (SELECT 1 FROM usuarios u WHERE u.id = sessoes.usuario_id AND u.empresa_id = $2)`
	if _, err := db.Exec(q, token, empresaID); err != nil {
		return fmt.Errorf("falha ao revogar sessão por refresh token: %w", err)
	}
	return nil
}
