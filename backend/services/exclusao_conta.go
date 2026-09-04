// Package services, arquivo exclusao_conta.go: exclusão e anonimização de
// dados pessoais por Adm — Story 8.2 (Epic 8, Privacidade/LGPD), spec-8-2.
//
// A LGPD dá ao Usuário o direito de exclusão da conta. Aqui isso é sempre
// baseado em solicitação, nunca self-service:
//
//   - SolicitarExclusaoConta: qualquer conta autenticada registra uma
//     solicitação `pendente` da PRÓPRIA conta (molde exato de
//     SolicitarPromocao). No máximo uma pendente por conta.
//   - ListarSolicitacoesExclusao: só um `adm` vê a fila de pendentes, com os
//     dados do solicitante resolvidos pelo JOIN (molde de ListarUsuarios).
//   - ProcessarExclusaoConta: só um `adm` anonimiza. Numa única transação:
//     reescreve `usuarios.nome`/`usuarios.email` para valores anonimizados,
//     zera as credenciais (`senha_hash=NULL`, `ativo=false`, MFA desligado),
//     revoga todas as sessões vivas, invalida os `tokens_acao` pendentes e
//     transiciona a solicitação para `processada`. NUNCA toca em nenhuma
//     linha de `movimentacoes`, `pedidos` ou `logs_acesso` — a integridade
//     histórica/auditoria dos épicos anteriores tem de sobreviver intacta.
//
// O alvo da anonimização é SEMPRE `solicitacoes_exclusao_conta.solicitante_id`
// — nunca um id vindo de path/query/body do request de processamento (o path
// só carrega o id da SOLICITAÇÃO).
package services

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
)

var (
	// ErrExclusaoPendenteExiste indica que a conta já tem uma solicitação de
	// exclusão `pendente` viva — no máximo uma por vez. Handler -> 409 CONFLICT.
	ErrExclusaoPendenteExiste = errors.New("já existe uma solicitação de exclusão pendente para esta conta")
	// ErrSolicitacaoExclusaoNaoEncontrada indica `id` inexistente OU malformado
	// (não-UUID, `pq` 22P02) — os dois caem no mesmo erro. Handler -> 404.
	ErrSolicitacaoExclusaoNaoEncontrada = errors.New("solicitação de exclusão não encontrada")
	// ErrSolicitacaoExclusaoNaoPendente indica que a solicitação já foi
	// processada — inclui reuso e corrida entre dois `adm`. Handler -> 409.
	ErrSolicitacaoExclusaoNaoPendente = errors.New("solicitação de exclusão não está mais pendente")
	// ErrUltimoAdmAtivo indica que processar a solicitação deixaria o sistema
	// sem nenhum `adm` ativo — bloqueado, nenhuma escrita acontece. Handler ->
	// 409 CONFLICT com a mensagem de que ao menos um administrador ativo deve
	// sempre existir.
	ErrUltimoAdmAtivo = errors.New("ao menos um administrador ativo deve sempre existir")
)

// SolicitacaoExclusao é a projeção de uma linha de
// `solicitacoes_exclusao_conta` na perspectiva do solicitante (resposta de
// POST /api/usuarios/me/solicitacao-exclusao). `ProcessadoEm` é nil enquanto
// `Status == "pendente"`.
type SolicitacaoExclusao struct {
	ID           string
	Status       string
	CriadoEm     time.Time
	ProcessadoEm *time.Time
}

// SolicitacaoExclusaoPendente é um item da fila do `adm`
// (GET /api/solicitacoes-exclusao) e também a projeção devolvida por
// ProcessarExclusaoConta — os dados do solicitante são resolvidos pelo JOIN e
// capturados ANTES da anonimização.
type SolicitacaoExclusaoPendente struct {
	ID               string
	SolicitanteNome  string
	SolicitanteEmail string
	SolicitantePapel string
	CriadoEm         time.Time
}

// SolicitarExclusaoConta cria uma solicitação `pendente` de exclusão para a
// própria conta (POST /api/usuarios/me/solicitacao-exclusao). `solicitanteID`
// vem sempre de middleware.UsuarioDaSessao — nunca do cliente. Molde exato de
// SolicitarPromocao (promocao.go): `SELECT EXISTS` de pendente ->
// ErrExclusaoPendenteExiste (checagem prévia amigável); `INSERT ... RETURNING`;
// violação `23505` do índice parcial `idx_solicitacoes_exclusao_pendente_unica`
// -> ErrExclusaoPendenteExiste (backstop de corrida). Uma solicitação anterior
// `processada` NÃO bloqueia — o gate olha apenas `status = 'pendente'`.
func SolicitarExclusaoConta(db *sql.DB, solicitanteID string) (SolicitacaoExclusao, error) {
	var existePendente bool
	const selectPendente = `
		SELECT EXISTS (
			SELECT 1 FROM solicitacoes_exclusao_conta
			WHERE solicitante_id = $1 AND status = 'pendente'
		)`
	if err := db.QueryRow(selectPendente, solicitanteID).Scan(&existePendente); err != nil {
		return SolicitacaoExclusao{}, fmt.Errorf("falha ao verificar solicitação de exclusão pendente: %w", err)
	}
	if existePendente {
		return SolicitacaoExclusao{}, ErrExclusaoPendenteExiste
	}

	var s SolicitacaoExclusao
	const insert = `
		INSERT INTO solicitacoes_exclusao_conta (solicitante_id, status)
		VALUES ($1, 'pendente')
		RETURNING id, status, criado_em`
	if err := db.QueryRow(insert, solicitanteID).Scan(&s.ID, &s.Status, &s.CriadoEm); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation {
			// Corrida: outra requisição inseriu a pendente entre o SELECT acima
			// e este INSERT. O índice parcial único é o backstop.
			return SolicitacaoExclusao{}, ErrExclusaoPendenteExiste
		}
		return SolicitacaoExclusao{}, fmt.Errorf("falha ao inserir solicitação de exclusão: %w", err)
	}
	return s, nil
}

// ListarSolicitacoesExclusao devolve a fila de solicitações `pendente`
// (GET /api/solicitacoes-exclusao), com nome/email/papel do solicitante já
// resolvidos pelo JOIN. Ordenado por `criado_em, id`. Lista vazia não-nil,
// nunca erro (molde de ListarUsuarios).
func ListarSolicitacoesExclusao(db *sql.DB) ([]SolicitacaoExclusaoPendente, error) {
	rows, err := db.Query(`
		SELECT s.id, u.nome, u.email, u.papel, s.criado_em
		FROM solicitacoes_exclusao_conta s
		JOIN usuarios u ON u.id = s.solicitante_id
		WHERE s.status = 'pendente'
		ORDER BY s.criado_em, s.id`)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar solicitações de exclusão: %w", err)
	}
	defer rows.Close()

	pendentes := make([]SolicitacaoExclusaoPendente, 0)
	for rows.Next() {
		var p SolicitacaoExclusaoPendente
		if err := rows.Scan(&p.ID, &p.SolicitanteNome, &p.SolicitanteEmail, &p.SolicitantePapel, &p.CriadoEm); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de solicitação de exclusão: %w", err)
		}
		pendentes = append(pendentes, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar solicitações de exclusão: %w", err)
	}
	return pendentes, nil
}

// ProcessarExclusaoConta anonimiza a conta alvo de uma solicitação `pendente`
// (POST /api/solicitacoes-exclusao/{id}/processamento). `atorID` é o `adm` da
// sessão. O alvo é SEMPRE `solicitacoes_exclusao_conta.solicitante_id` da
// linha `solicitacaoID` — nunca um id do request.
//
// Guards (antes de qualquer escrita):
//   - `solicitacaoID` inexistente ou não-UUID (`pq` 22P02) ->
//     ErrSolicitacaoExclusaoNaoEncontrada.
//   - solicitação não-`pendente` -> ErrSolicitacaoExclusaoNaoPendente.
//   - alvo `papel = 'adm'` e nenhum outro `adm` ativo
//     (`count(*) ... WHERE papel='adm' AND ativo=true AND id <> $alvo` == 0) ->
//     ErrUltimoAdmAtivo. NENHUMA escrita acontece.
//
// Caso válido, numa única transação (`SELECT ... FOR UPDATE OF s` serializa
// dois `adm` sobre a mesma solicitação):
//   - `UPDATE usuarios` — nome/email anonimizados, `senha_hash=NULL`,
//     `ativo=false`, `mfa_habilitado=false`, `mfa_secret=NULL`,
//     `email_verificado=false`, contador/prazo de bloqueio zerados. `papel`
//     NÃO muda. E-mail determinístico e único:
//     `lower('anonimizado+' || <id> || '@anonimizado.invalido')` (TLD
//     reservado RFC 2606, satisfaz `idx_usuarios_email_lower`).
//   - `UPDATE sessoes SET revogado_em = now()` de todas as sessões vivas do
//     alvo (molde de AlterarAtivacaoUsuario).
//   - `UPDATE tokens_acao SET usado_em = now()` dos tokens pendentes do alvo.
//   - `UPDATE solicitacoes_exclusao_conta SET status='processada', ...
//     WHERE id=$1 AND status='pendente'` (RowsAffected()==0 ->
//     ErrSolicitacaoExclusaoNaoPendente, fecha a corrida entre dois `adm`).
//
// NUNCA faz SELECT/UPDATE/DELETE em `movimentacoes`, `pedidos` ou
// `logs_acesso`.
func ProcessarExclusaoConta(db *sql.DB, solicitacaoID, atorID string) (SolicitacaoExclusaoPendente, error) {
	tx, err := db.Begin()
	if err != nil {
		return SolicitacaoExclusaoPendente{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	var (
		p         SolicitacaoExclusaoPendente
		status    string
		alvoID    string
		alvoPapel string
	)
	const selectSolic = `
		SELECT s.status, u.id, u.nome, u.email, u.papel, s.criado_em
		FROM solicitacoes_exclusao_conta s
		JOIN usuarios u ON u.id = s.solicitante_id
		WHERE s.id = $1
		FOR UPDATE OF s`
	err = tx.QueryRow(selectSolic, solicitacaoID).
		Scan(&status, &alvoID, &p.SolicitanteNome, &p.SolicitanteEmail, &alvoPapel, &p.CriadoEm)
	if err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) {
			return SolicitacaoExclusaoPendente{}, ErrSolicitacaoExclusaoNaoEncontrada
		}
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return SolicitacaoExclusaoPendente{}, ErrSolicitacaoExclusaoNaoEncontrada
		}
		return SolicitacaoExclusaoPendente{}, fmt.Errorf("falha ao consultar solicitação de exclusão: %w", err)
	}
	p.ID = solicitacaoID
	p.SolicitantePapel = alvoPapel

	if status != "pendente" {
		return SolicitacaoExclusaoPendente{}, ErrSolicitacaoExclusaoNaoPendente
	}

	// Guarda do último `adm`: se o alvo é `adm` e não sobra nenhum outro `adm`
	// ativo, o processamento é bloqueado — nenhuma escrita acontece. Escrita
	// como `count(*) == 0` (e não `if papelAlvo == 'adm'`) para expressar
	// literalmente o invariante do épico e continuar correta se a unicidade de
	// `adm` for relaxada no futuro.
	if alvoPapel == PapelAdm {
		var outrosAdmsAtivos int
		const contarAdms = `
			SELECT count(*) FROM usuarios
			WHERE papel = 'adm' AND ativo = true AND id <> $1`
		if err := tx.QueryRow(contarAdms, alvoID).Scan(&outrosAdmsAtivos); err != nil {
			return SolicitacaoExclusaoPendente{}, fmt.Errorf("falha ao contar administradores ativos: %w", err)
		}
		if outrosAdmsAtivos == 0 {
			return SolicitacaoExclusaoPendente{}, ErrUltimoAdmAtivo
		}
	}

	const anonimizarConta = `
		UPDATE usuarios SET
			nome = 'Usuário anonimizado',
			email = lower('anonimizado+' || id || '@anonimizado.invalido'),
			senha_hash = NULL,
			ativo = false,
			mfa_habilitado = false,
			mfa_secret = NULL,
			email_verificado = false,
			tentativas_login_falhas = 0,
			bloqueado_ate = NULL
		WHERE id = $1`
	if _, err := tx.Exec(anonimizarConta, alvoID); err != nil {
		return SolicitacaoExclusaoPendente{}, fmt.Errorf("falha ao anonimizar conta: %w", err)
	}

	if _, err := tx.Exec(
		`UPDATE sessoes SET revogado_em = now() WHERE usuario_id = $1 AND revogado_em IS NULL`,
		alvoID,
	); err != nil {
		return SolicitacaoExclusaoPendente{}, fmt.Errorf("falha ao revogar sessões da conta anonimizada: %w", err)
	}

	if _, err := tx.Exec(
		`UPDATE tokens_acao SET usado_em = now() WHERE usuario_id = $1 AND usado_em IS NULL`,
		alvoID,
	); err != nil {
		return SolicitacaoExclusaoPendente{}, fmt.Errorf("falha ao invalidar tokens de ação da conta anonimizada: %w", err)
	}

	res, err := tx.Exec(`
		UPDATE solicitacoes_exclusao_conta
		SET status = 'processada', processado_por = $2, processado_em = now()
		WHERE id = $1 AND status = 'pendente'`, solicitacaoID, atorID)
	if err != nil {
		return SolicitacaoExclusaoPendente{}, fmt.Errorf("falha ao registrar processamento da solicitação: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Outro `adm` processou esta solicitação entre o SELECT inicial e este
		// UPDATE (corrida).
		return SolicitacaoExclusaoPendente{}, ErrSolicitacaoExclusaoNaoPendente
	}

	if err := tx.Commit(); err != nil {
		return SolicitacaoExclusaoPendente{}, fmt.Errorf("falha ao commitar processamento da exclusão: %w", err)
	}
	return p, nil
}
