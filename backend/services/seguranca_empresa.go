package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// maxEventosAuditoriaSeguranca limita quantas linhas GET /api/seguranca/auditoria
// devolve numa consulta (Story 14.3). Sem configuração runtime.
const maxEventosAuditoriaSeguranca = 200

// AcaoExigenciaAlterada é o valor de `auditoria_seguranca.acao` gravado quando o
// `adm` liga ou desliga a exigência de MFA da Empresa.
const AcaoExigenciaAlterada = "exigencia_alterada"

// AcaoMFAResetado é gravada quando o `adm` zera o MFA de uma conta de rank
// menor (Story 14.4). `ator_id` = adm, `alvo_id` = conta resetada.
const AcaoMFAResetado = "mfa_resetado"

// AcaoMFADesligado é gravada quando a própria conta desliga o seu MFA com
// senha atual + código TOTP (Story 14.4). `ator_id` = `alvo_id` = a conta.
const AcaoMFADesligado = "mfa_desligado"

// ContarContasSemMFA conta as contas da Empresa que passariam a ser bloqueadas
// pelo gate de middleware.RequireRole se a Empresa exigir MFA: ativas,
// `gestor`/`adm`, sem MFA e COM senha. Conta sem `senha_hash` só entra por SSO,
// e o gate nunca dispara para sessão SSO (Story 14.1), por isso fica de fora.
func ContarContasSemMFA(db *sql.DB, empresaID string) (int, error) {
	const q = `
		SELECT count(*)
		FROM usuarios
		WHERE empresa_id = $1
		  AND ativo = true
		  AND papel IN ('gestor', 'adm')
		  AND mfa_habilitado = false
		  AND senha_hash IS NOT NULL`
	var n int
	if err := db.QueryRow(q, empresaID).Scan(&n); err != nil {
		return 0, fmt.Errorf("falha ao contar contas sem MFA: %w", err)
	}
	return n, nil
}

// AlterarExigenciaMFA grava `empresas.mfa_obrigatorio = novo` e, só quando o
// valor de fato muda, registra uma linha `exigencia_alterada` em
// `auditoria_seguranca` NA MESMA transação (Story 14.3, AD-35).
//
// O UPDATE condicional (`mfa_obrigatorio <> $novo`) decide sozinho se houve
// mudança, sem SELECT prévio: duas requisições concorrentes com o mesmo valor
// produzem exatamente uma linha de auditoria, e o valor anterior é sempre
// `!novo`. Nenhuma coluna de `usuarios` é tocada — desligar a exigência nunca
// desliga o MFA de ninguém.
func AlterarExigenciaMFA(db *sql.DB, empresaID, atorID string, novo bool) (alterado bool, err error) {
	tx, err := db.Begin()
	if err != nil {
		return false, fmt.Errorf("falha ao iniciar transação da exigência de MFA: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(`UPDATE empresas SET mfa_obrigatorio = $2 WHERE id = $1 AND mfa_obrigatorio <> $2`, empresaID, novo)
	if err != nil {
		return false, fmt.Errorf("falha ao alterar exigência de MFA: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("falha ao ler linhas afetadas da exigência de MFA: %w", err)
	}
	if n == 0 {
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("falha ao confirmar transação da exigência de MFA: %w", err)
		}
		return false, nil
	}

	detalhe, err := json.Marshal(map[string]bool{"anterior": !novo, "novo": novo})
	if err != nil {
		return false, fmt.Errorf("falha ao montar detalhe da auditoria: %w", err)
	}
	const insert = `
		INSERT INTO auditoria_seguranca (empresa_id, ator_id, acao, detalhe)
		VALUES ($1, $2, $3, $4)`
	if _, err := tx.Exec(insert, empresaID, atorID, AcaoExigenciaAlterada, detalhe); err != nil {
		return false, fmt.Errorf("falha ao registrar auditoria da exigência de MFA: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("falha ao confirmar transação da exigência de MFA: %w", err)
	}
	return true, nil
}

// EventoAuditoriaSeguranca é a projeção de uma linha de `auditoria_seguranca`
// devolvida por GET /api/seguranca/auditoria. AtorNome/AlvoNome vêm de LEFT
// JOIN em `usuarios` escopado pela mesma Empresa. Detalhe é o JSONB cru.
type EventoAuditoriaSeguranca struct {
	ID       string          `json:"id"`
	Acao     string          `json:"acao"`
	AtorID   string          `json:"atorId"`
	AtorNome *string         `json:"atorNome"`
	AlvoID   *string         `json:"alvoId"`
	AlvoNome *string         `json:"alvoNome"`
	Detalhe  json.RawMessage `json:"detalhe"`
	CriadoEm time.Time       `json:"criadoEm"`
}

// ListarAuditoriaSeguranca devolve os eventos de `auditoria_seguranca` DA
// EMPRESA `empresaID`, do mais recente ao mais antigo (`id DESC` como
// desempate determinístico), limitados a maxEventosAuditoriaSeguranca. Molde de
// ListarLogsAcesso. Lista vazia não é erro.
func ListarAuditoriaSeguranca(db *sql.DB, empresaID string) ([]EventoAuditoriaSeguranca, error) {
	q := fmt.Sprintf(`
		SELECT a.id, a.acao, a.ator_id, ator.nome, a.alvo_id, alvo.nome, a.detalhe, a.criado_em
		FROM auditoria_seguranca a
		LEFT JOIN usuarios ator ON ator.id = a.ator_id AND ator.empresa_id = $1
		LEFT JOIN usuarios alvo ON alvo.id = a.alvo_id AND alvo.empresa_id = $1
		WHERE a.empresa_id = $1
		ORDER BY a.criado_em DESC, a.id DESC
		LIMIT %d`, maxEventosAuditoriaSeguranca)

	rows, err := db.Query(q, empresaID)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar auditoria de segurança: %w", err)
	}
	defer rows.Close()

	eventos := make([]EventoAuditoriaSeguranca, 0)
	for rows.Next() {
		var e EventoAuditoriaSeguranca
		var atorNome, alvoID, alvoNome sql.NullString
		var detalhe []byte
		if err := rows.Scan(&e.ID, &e.Acao, &e.AtorID, &atorNome, &alvoID, &alvoNome, &detalhe, &e.CriadoEm); err != nil {
			return nil, fmt.Errorf("falha ao ler evento de auditoria de segurança: %w", err)
		}
		if atorNome.Valid {
			e.AtorNome = &atorNome.String
		}
		if alvoID.Valid {
			e.AlvoID = &alvoID.String
		}
		if alvoNome.Valid {
			e.AlvoNome = &alvoNome.String
		}
		e.Detalhe = json.RawMessage(detalhe)
		eventos = append(eventos, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar auditoria de segurança: %w", err)
	}
	return eventos, nil
}
