package services

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// realtimeTicketExpiracao é o prazo de validade do ticket de conexão SSE
// (`tokens_acao`, tipo `realtime_ticket`) emitido por EmitirTicketRealtime
// (Story 4.4, AD-3): 30s, fixado explicitamente pelo intent desta story —
// folga suficiente para o cliente abrir o EventSource logo em seguida à
// emissão, e janela mínima de uso caso o ticket vaze (ex. em log de acesso).
const realtimeTicketExpiracao = 30 * time.Second

// EmitirTicketRealtime emite o token opaco de uso único que
// EmitirTicketRealtimeHandler devolve para POST /api/realtime/ticket
// (Story 4.4): mesmo molde de gerarTokenAcao/tokens_acao já usado por
// verificação de e-mail, redefinição de senha e login por MFA, com
// `tipo='realtime_ticket'` e `expira_em = now() + realtimeTicketExpiracao`.
//
// AO CONTRÁRIO de IniciarLoginMFA/SolicitarRedefinicaoSenha, NÃO invalida
// tickets anteriores ainda não usados da mesma conta: cada aba do navegador
// abre sua própria conexão SSE e precisa do seu próprio ticket —
// invalidar o anterior quebraria múltiplas abas simultâneas, e nenhum
// requisito de segurança (AD-18) exige essa invalidação para este `tipo`
// especificamente (só para `mfa_login`, onde um código antigo válido é um
// risco real de conta).
func EmitirTicketRealtime(db *sql.DB, empresaID string, usuarioID string) (string, error) {
	token, err := gerarTokenAcao()
	if err != nil {
		return "", err
	}

	expiraEm := time.Now().UTC().Add(realtimeTicketExpiracao)
	// `tokens_acao` é tabela FILHA e não ganhou `empresa_id`; o guard de
	// Empresa (Story 9.1) é o `SELECT` no lugar do `VALUES` — um `usuarioID`
	// de outra Empresa não emite ticket nenhum.
	const insertToken = `
		INSERT INTO tokens_acao (usuario_id, token, tipo, expira_em)
		SELECT u.id, $2, 'realtime_ticket', $3 FROM usuarios u WHERE u.id = $1 AND u.empresa_id = $4`
	res, err := db.Exec(insertToken, usuarioID, token, expiraEm, empresaID)
	if err != nil {
		return "", fmt.Errorf("falha ao emitir ticket de conexão em tempo real: %w", err)
	}
	if linhas, errLinhas := res.RowsAffected(); errLinhas == nil && linhas == 0 {
		return "", ErrUsuarioSessaoNaoEncontrado
	}
	return token, nil
}

// ConsumirTicketRealtime consome atomicamente um ticket de conexão SSE
// (Story 4.4) para GET /api/realtime/stream — mesma corrida fechada
// SELECT+UPDATE condicional de VerificarEmail: a busca inicial distingue
// "token não existe" (ErrTokenNaoEncontrado) de "token existe mas
// expirado/já usado" (ErrTokenExpirado); o UPDATE que marca o token como
// usado repete as mesmas condições (não expirado, não usado) para fechar a
// janela de corrida entre as duas consultas — se `RowsAffected() == 0`,
// outra requisição (outra aba abrindo a MESMA conexão duas vezes, por
// exemplo) já consumiu ou o prazo expirou nesse meio-tempo, e o resultado
// também é ErrTokenExpirado.
func ConsumirTicketRealtime(db *sql.DB, empresaID string, token string) (usuarioID string, err error) {
	tx, err := db.Begin()
	if err != nil {
		return "", fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	var expiraEm time.Time
	var usadoEm sql.NullTime
	// Mesmo guard de Empresa por JOIN dos demais consumos de token (Story
	// 9.1): um ticket emitido sob o slug de outra Empresa colapsa em
	// ErrTokenNaoEncontrado aqui.
	const selectToken = `
		SELECT t.usuario_id, t.expira_em, t.usado_em
		FROM tokens_acao t
		JOIN usuarios u ON u.id = t.usuario_id AND u.empresa_id = $2
		WHERE t.token = $1 AND t.tipo = 'realtime_ticket'`
	if err := tx.QueryRow(selectToken, token, empresaID).Scan(&usuarioID, &expiraEm, &usadoEm); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrTokenNaoEncontrado
		}
		return "", fmt.Errorf("falha ao consultar ticket de conexão em tempo real: %w", err)
	}
	if usadoEm.Valid || !time.Now().Before(expiraEm) {
		return "", ErrTokenExpirado
	}

	const marcarUsado = `
		UPDATE tokens_acao
		SET usado_em = now()
		WHERE token = $1 AND tipo = 'realtime_ticket' AND usado_em IS NULL AND expira_em > now()`
	res, err := tx.Exec(marcarUsado, token)
	if err != nil {
		return "", fmt.Errorf("falha ao marcar ticket de conexão em tempo real como usado: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", ErrTokenExpirado
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("falha ao commitar consumo do ticket de conexão em tempo real: %w", err)
	}
	return usuarioID, nil
}
