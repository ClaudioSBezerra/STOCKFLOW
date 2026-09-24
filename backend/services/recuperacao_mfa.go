package services

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

// Recuperação de MFA — Story 14.4 (FR-53, AD-35). Com o MFA opcional por
// Empresa (Story 14.1), quem perde o autenticador não pode ficar preso para
// sempre. Duas saídas, ambas gravando `auditoria_seguranca` NA MESMA transação
// que zera as colunas de MFA:
//
//   - ResetarMFAUsuario: o `adm` zera o MFA de uma conta de rank ESTRITAMENTE
//     menor da própria Empresa e revoga as sessões dela. O que acontece no
//     próximo login é decidido só pelo gate de middleware.RequireRole.
//   - DesligarMFAPropria: a própria conta desliga o seu MFA com senha atual +
//     código TOTP vigente. As falhas contam no mesmo bloqueio por tentativas
//     do login (Story 1.10).
//
// Nenhuma das duas mexe em RedefinirSenha: a redefinição de senha por e-mail
// continua sem tocar em colunas de MFA.

// ResetarMFAUsuario zera `mfa_habilitado`, `mfa_secret` e
// `mfa_ultimo_passo_usado` da conta alvo (POST /api/usuarios/{id}/mfa-reset).
//
//   - Alvo inexistente, de outra Empresa ou id não-UUID -> ErrContaNaoEncontrada.
//   - `RankPapel(alvo) >= RankPapel(ator)` (outro `adm`, ou a própria conta) ->
//     ErrGestaoForaDeEscopo. Regra de rank ESTRITA: não usa
//     carregarAlvoParaGestao, que deixa o `adm` agir sobre outro `adm`.
//   - Alvo sem MFA -> ErrMFANaoConfigurado, nada gravado.
//
// Numa transação: UPDATE guardado por papel e `mfa_habilitado=true`
// (0 linhas -> ErrEstadoContaMudou), revogação das sessões vivas do alvo,
// invalidação dos tokens `mfa_login` pendentes dele e INSERT `mfa_resetado` em
// `auditoria_seguranca`. Devolve o UsuarioResumo já atualizado.
func ResetarMFAUsuario(db *sql.DB, empresaID, alvoID, atorID, papelAtor string) (UsuarioResumo, error) {
	var papelAlvo string
	var mfaHabilitado bool
	err := db.QueryRow(
		`SELECT papel, mfa_habilitado FROM usuarios WHERE id = $1 AND empresa_id = $2`, alvoID, empresaID,
	).Scan(&papelAlvo, &mfaHabilitado)
	if err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) {
			return UsuarioResumo{}, ErrContaNaoEncontrada
		}
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return UsuarioResumo{}, ErrContaNaoEncontrada
		}
		return UsuarioResumo{}, fmt.Errorf("falha ao consultar conta alvo do reset de MFA: %w", err)
	}

	if alvoID == atorID || RankPapel(papelAlvo) >= RankPapel(papelAtor) {
		return UsuarioResumo{}, ErrGestaoForaDeEscopo
	}
	if !mfaHabilitado {
		return UsuarioResumo{}, ErrMFANaoConfigurado
	}

	tx, err := db.Begin()
	if err != nil {
		return UsuarioResumo{}, fmt.Errorf("falha ao iniciar transação do reset de MFA: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	res, err := tx.Exec(`
		UPDATE usuarios
		SET mfa_habilitado = false, mfa_secret = NULL, mfa_ultimo_passo_usado = NULL
		WHERE id = $1 AND empresa_id = $2 AND papel = $3 AND mfa_habilitado = true`,
		alvoID, empresaID, papelAlvo)
	if err != nil {
		return UsuarioResumo{}, fmt.Errorf("falha ao resetar MFA da conta: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return UsuarioResumo{}, ErrEstadoContaMudou
	}

	if _, err := tx.Exec(
		`UPDATE sessoes SET revogado_em = now() WHERE usuario_id = $1 AND revogado_em IS NULL`, alvoID,
	); err != nil {
		return UsuarioResumo{}, fmt.Errorf("falha ao revogar sessões da conta no reset de MFA: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE tokens_acao SET usado_em = now() WHERE usuario_id = $1 AND tipo = 'mfa_login' AND usado_em IS NULL`, alvoID,
	); err != nil {
		return UsuarioResumo{}, fmt.Errorf("falha ao invalidar tokens de login por MFA no reset: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO auditoria_seguranca (empresa_id, ator_id, alvo_id, acao, detalhe)
		VALUES ($1, $2, $3, $4, '{}')`, empresaID, atorID, alvoID, AcaoMFAResetado,
	); err != nil {
		return UsuarioResumo{}, fmt.Errorf("falha ao registrar auditoria do reset de MFA: %w", err)
	}

	u, err := relerUsuarioResumoTx(tx, empresaID, alvoID)
	if err != nil {
		return UsuarioResumo{}, err
	}
	if err := tx.Commit(); err != nil {
		return UsuarioResumo{}, fmt.Errorf("falha ao commitar reset de MFA: %w", err)
	}
	slog.Info("MFA resetado por adm", "ator_id", atorID, "alvo_id", alvoID)
	return u, nil
}

// DesligarMFAPropria desliga o MFA da própria conta (POST /api/auth/mfa/desligar).
// Ordem das checagens (a checagem "sessão sem MFA" do handler vem antes):
//
//  1. `gestor`/`adm` numa Empresa que exige -> ErrMFAExigidoPelaEmpresa, sem
//     alterar nada nem contar tentativa.
//  2. Conta sem MFA no banco -> ErrMFANaoConfigurado.
//  3. bcrypt SEMPRE roda (dummyBcryptHash se `senha_hash` nulo), e só depois
//     se olha `bloqueado_ate` — molde de Login. Bloqueio vigente ->
//     ErrContaBloqueada; bloqueio expirado é destravado e o fluxo segue.
//  4. Senha errada -> registrarFalhaLogin + ErrCredenciaisInvalidas (conta
//     só-SSO, sem `senha_hash`, recebe o mesmo erro mas NÃO conta tentativa —
//     molde de Login); código TOTP inválido -> registrarFalhaLogin +
//     ErrMFACodigoInvalido. O handler responde as duas com o MESMO 401 (sem
//     oráculo da senha).
//  5. Sucesso: UPDATE guardado pelo segredo lido e pelo passo TOTP QUE CASOU
//     (passoDoCodigoTOTP), exigindo-o estritamente maior que
//     `mfa_ultimo_passo_usado` — mesma regra do login do Dono da Plataforma:
//     um código já usado no login (inclusive aceito pela tolerância de ±1
//     passo) não pode ser reapresentado no passo seguinte. 0 linhas ->
//     tratado como código inválido: rollback, registrarFalhaLogin. Junto, o
//     INSERT `mfa_desligado` na mesma transação. As sessões da conta NÃO são
//     revogadas.
func DesligarMFAPropria(db *sql.DB, usuarioID, papel string, empresaExige bool, senhaAtual, codigo string) error {
	if empresaExige && RankPapel(papel) >= RankPapel(PapelGestor) {
		return ErrMFAExigidoPelaEmpresa
	}

	var empresaID, senhaHash, segredo sql.NullString
	var mfaHabilitado bool
	var bloqueadoAte sql.NullTime
	err := db.QueryRow(`
		SELECT empresa_id, senha_hash, mfa_habilitado, mfa_secret, bloqueado_ate
		FROM usuarios WHERE id = $1`, usuarioID,
	).Scan(&empresaID, &senhaHash, &mfaHabilitado, &segredo, &bloqueadoAte)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUsuarioSessaoNaoEncontrado
		}
		return fmt.Errorf("falha ao consultar conta para desligar MFA: %w", err)
	}
	if !mfaHabilitado || !segredo.Valid {
		return ErrMFANaoConfigurado
	}

	hashParaComparar := dummyBcryptHash
	if senhaHash.Valid {
		hashParaComparar = []byte(senhaHash.String)
	}
	senhaCorreta := bcrypt.CompareHashAndPassword(hashParaComparar, []byte(senhaAtual)) == nil && senhaHash.Valid

	agora := time.Now().UTC()
	if bloqueadoAte.Valid && bloqueadoAte.Time.After(agora) {
		return ErrContaBloqueada
	}
	if bloqueadoAte.Valid {
		if _, err := db.Exec(`UPDATE usuarios SET tentativas_login_falhas = 0, bloqueado_ate = NULL
			WHERE id = $1 AND bloqueado_ate <= now()`, usuarioID); err != nil {
			slog.Warn("falha ao destravar conta com bloqueio expirado (desligar MFA)", "usuario_id", usuarioID, "error", err)
		}
	}

	falhar := func(e error) error {
		if err := registrarFalhaLogin(db, usuarioID); err != nil {
			slog.Warn("falha ao registrar tentativa malsucedida de desligar MFA", "usuario_id", usuarioID, "error", err)
		}
		return e
	}
	if !senhaCorreta {
		if !senhaHash.Valid {
			// Conta só-SSO: nunca teria como acertar a senha; não é sinal de
			// força bruta (mesmo critério de Login).
			return ErrCredenciaisInvalidas
		}
		return falhar(ErrCredenciaisInvalidas)
	}
	passo, ok := passoDoCodigoTOTP(segredo.String, codigo)
	if !ok {
		return falhar(ErrMFACodigoInvalido)
	}
	if !empresaID.Valid {
		return fmt.Errorf("conta %s sem Empresa: não é possível auditar o desligamento de MFA", usuarioID)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("falha ao iniciar transação para desligar MFA: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	res, err := tx.Exec(`
		UPDATE usuarios
		SET mfa_habilitado = false, mfa_secret = NULL, mfa_ultimo_passo_usado = NULL,
		    tentativas_login_falhas = 0, bloqueado_ate = NULL
		WHERE id = $1 AND mfa_habilitado = true AND mfa_secret = $2
		  AND (mfa_ultimo_passo_usado IS NULL OR mfa_ultimo_passo_usado < $3)`,
		usuarioID, segredo.String, passo)
	if err != nil {
		return fmt.Errorf("falha ao desligar MFA: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Passo do código não é posterior ao último usado (reuso) ou segredo
		// trocado em corrida:
		// mesmo vocabulário de código inválido, sem revelar qual.
		_ = tx.Rollback()
		return falhar(ErrMFACodigoInvalido)
	}
	if _, err := tx.Exec(`
		INSERT INTO auditoria_seguranca (empresa_id, ator_id, alvo_id, acao, detalhe)
		VALUES ($1, $2, $2, $3, '{}')`, empresaID.String, usuarioID, AcaoMFADesligado,
	); err != nil {
		return fmt.Errorf("falha ao registrar auditoria do desligamento de MFA: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("falha ao commitar desligamento de MFA: %w", err)
	}
	slog.Info("MFA desligado pela própria conta", "usuario_id", usuarioID)
	return nil
}
