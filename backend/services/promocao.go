package services

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// pqInvalidTextRepresentation é o SQLSTATE do Postgres para "invalid input
// syntax" — devolvido, entre outros, quando um `WHERE id = $1` recebe um
// valor que não é um UUID válido. Tratado como "não encontrado" pela mesma
// classe de decisão já aplicada em Cadastrar (input de cliente inválido não
// deve virar 500).
const pqInvalidTextRepresentation = "22P02"

var (
	// ErrPromocaoIndisponivel indica que o papel atual do solicitante não tem
	// promoção disponível (`gestor`/`adm`, ou um valor fora da hierarquia).
	ErrPromocaoIndisponivel = errors.New("não há promoção de papel disponível para este papel")
	// ErrSolicitacaoPendenteExiste indica que a conta já tem uma solicitação
	// `pendente` viva — no máximo uma por vez (AC4).
	ErrSolicitacaoPendenteExiste = errors.New("já existe uma solicitação de promoção pendente para esta conta")
	// ErrSolicitacaoNaoEncontrada indica `id` inexistente OU malformado
	// (não-UUID, `pq` 22P02) — os dois caem no mesmo erro.
	ErrSolicitacaoNaoEncontrada = errors.New("solicitação de promoção não encontrada")
	// ErrSolicitacaoNaoPendente indica que a solicitação já foi decidida
	// (aprovada/rejeitada) — inclui reuso e corrida entre dois decisores.
	ErrSolicitacaoNaoPendente = errors.New("solicitação de promoção não está mais pendente")
	// ErrDecisaoNaoAutorizada indica que o decisor não pode decidir esta
	// promoção — na prática, alvo `gestor` decidido por não-`adm` (AC3).
	ErrDecisaoNaoAutorizada = errors.New("papel insuficiente para decidir esta promoção")
	// ErrEstadoContaMudou indica que o papel do solicitante mudou entre a
	// solicitação e a aprovação (o guard do UPDATE usuarios afetou 0 linhas).
	ErrEstadoContaMudou = errors.New("o papel do solicitante mudou desde a solicitação")
)

// SolicitacaoPromocao é a projeção de uma linha de `solicitacoes_promocao` na
// perspectiva do solicitante (POST /api/promocoes, GET /api/promocoes/minha,
// resposta de decisão). `DecididoEm` é nil enquanto `Status == "pendente"`.
type SolicitacaoPromocao struct {
	ID         string
	PapelAlvo  string
	Status     string
	CriadoEm   time.Time
	DecididoEm *time.Time
}

// SolicitacaoPendente é um item da fila de decisão (GET /api/promocoes), com
// os dados do solicitante já resolvidos pelo JOIN.
type SolicitacaoPendente struct {
	ID               string
	SolicitanteNome  string
	SolicitanteEmail string
	PapelAtual       string
	PapelAlvo        string
	CriadoEm         time.Time
}

// proximoPapelPromocao devolve o papel imediatamente acima na hierarquia
// (AD-8): `usuario -> almoxarife`, `almoxarife -> gestor`. `gestor` e `adm`
// (e qualquer valor inesperado) não têm promoção disponível — devolvem
// ("", false). É a ÚNICA fonte do papel-alvo: nenhum corpo de requisição o
// informa.
func proximoPapelPromocao(papel string) (string, bool) {
	switch papel {
	case PapelUsuario:
		return PapelAlmoxarife, true
	case PapelAlmoxarife:
		return PapelGestor, true
	default:
		return "", false
	}
}

// papelAbaixoDe é o inverso de proximoPapelPromocao — o papel que o
// solicitante PRECISA ter para a promoção ao alvo ser coerente. Usado como
// guard do `UPDATE usuarios SET papel = <alvo> WHERE ... AND papel = <abaixo>`
// na aprovação: se o solicitante já não estiver nesse papel, o UPDATE afeta 0
// linhas e a decisão vira ErrEstadoContaMudou.
func papelAbaixoDe(alvo string) string {
	switch alvo {
	case PapelAlmoxarife:
		return PapelUsuario
	case PapelGestor:
		return PapelAlmoxarife
	default:
		return ""
	}
}

// SolicitarPromocao cria uma solicitação `pendente` para a própria conta
// (POST /api/promocoes). O papel-alvo é derivado de `papelAtual` (contexto da
// sessão), nunca do cliente. Papel sem promoção disponível ->
// ErrPromocaoIndisponivel. Já existe uma pendente -> ErrSolicitacaoPendenteExiste
// (checagem prévia amigável + backstop de corrida via violação `23505` do
// índice parcial `idx_solicitacoes_promocao_pendente_unica`). Uma solicitação
// anterior `rejeitada`/`aprovada` NÃO bloqueia (AC5, sem período de espera): o
// gate olha apenas `status = 'pendente'`.
func SolicitarPromocao(db *sql.DB, solicitanteID, papelAtual string) (SolicitacaoPromocao, error) {
	alvo, ok := proximoPapelPromocao(papelAtual)
	if !ok {
		return SolicitacaoPromocao{}, ErrPromocaoIndisponivel
	}

	var existePendente bool
	const selectPendente = `
		SELECT EXISTS (
			SELECT 1 FROM solicitacoes_promocao
			WHERE solicitante_id = $1 AND status = 'pendente'
		)`
	if err := db.QueryRow(selectPendente, solicitanteID).Scan(&existePendente); err != nil {
		return SolicitacaoPromocao{}, fmt.Errorf("falha ao verificar solicitação pendente: %w", err)
	}
	if existePendente {
		return SolicitacaoPromocao{}, ErrSolicitacaoPendenteExiste
	}

	var s SolicitacaoPromocao
	const insert = `
		INSERT INTO solicitacoes_promocao (solicitante_id, papel_alvo, status)
		VALUES ($1, $2, 'pendente')
		RETURNING id, papel_alvo, status, criado_em`
	if err := db.QueryRow(insert, solicitanteID, alvo).Scan(&s.ID, &s.PapelAlvo, &s.Status, &s.CriadoEm); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation {
			// Corrida: outra requisição inseriu a pendente entre o SELECT acima
			// e este INSERT. O índice parcial único é o backstop.
			return SolicitacaoPromocao{}, ErrSolicitacaoPendenteExiste
		}
		return SolicitacaoPromocao{}, fmt.Errorf("falha ao inserir solicitação de promoção: %w", err)
	}
	return s, nil
}

// BuscarMinhaSolicitacao devolve a solicitação MAIS RECENTE da conta
// (GET /api/promocoes/minha), ou (nil, nil) se ela nunca solicitou. Nenhuma
// escrita.
func BuscarMinhaSolicitacao(db *sql.DB, solicitanteID string) (*SolicitacaoPromocao, error) {
	var s SolicitacaoPromocao
	var decididoEm sql.NullTime
	const query = `
		SELECT id, papel_alvo, status, criado_em, decidido_em
		FROM solicitacoes_promocao
		WHERE solicitante_id = $1
		ORDER BY criado_em DESC, id DESC
		LIMIT 1`
	err := db.QueryRow(query, solicitanteID).Scan(&s.ID, &s.PapelAlvo, &s.Status, &s.CriadoEm, &decididoEm)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("falha ao buscar solicitação de promoção: %w", err)
	}
	if decididoEm.Valid {
		s.DecididoEm = &decididoEm.Time
	}
	return &s, nil
}

// ListarSolicitacoesPendentes devolve a fila de solicitações `pendente`
// visíveis para `papelDecisor` (GET /api/promocoes). Recorte de escopo — mesmo
// padrão de ListarUsuarios, papel resolvido pelo contexto e passado como
// argumento, NUNCA reconsultado:
//   - `adm`: todas as pendentes.
//   - qualquer outro papel que passe pelo RequireRole(gestor) (na prática só
//     `gestor`): apenas `papel_alvo = 'almoxarife'` — não pode decidir
//     promoção a `gestor`.
//
// Ordenado por `criado_em, id`. Lista vazia não é erro.
func ListarSolicitacoesPendentes(db *sql.DB, papelDecisor string) ([]SolicitacaoPendente, error) {
	var rows *sql.Rows
	var err error
	if papelDecisor == PapelAdm {
		rows, err = db.Query(`
			SELECT s.id, u.nome, u.email, u.papel, s.papel_alvo, s.criado_em
			FROM solicitacoes_promocao s
			JOIN usuarios u ON u.id = s.solicitante_id
			WHERE s.status = 'pendente'
			ORDER BY s.criado_em, s.id`)
	} else {
		rows, err = db.Query(`
			SELECT s.id, u.nome, u.email, u.papel, s.papel_alvo, s.criado_em
			FROM solicitacoes_promocao s
			JOIN usuarios u ON u.id = s.solicitante_id
			WHERE s.status = 'pendente' AND s.papel_alvo = $1
			ORDER BY s.criado_em, s.id`, PapelAlmoxarife)
	}
	if err != nil {
		return nil, fmt.Errorf("falha ao listar solicitações pendentes: %w", err)
	}
	defer rows.Close()

	pendentes := make([]SolicitacaoPendente, 0)
	for rows.Next() {
		var p SolicitacaoPendente
		if err := rows.Scan(&p.ID, &p.SolicitanteNome, &p.SolicitanteEmail, &p.PapelAtual, &p.PapelAlvo, &p.CriadoEm); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de solicitação pendente: %w", err)
		}
		pendentes = append(pendentes, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar solicitações pendentes: %w", err)
	}
	return pendentes, nil
}

// DecidirSolicitacao aprova ou rejeita uma solicitação `pendente`
// (POST /api/promocoes/{id}/decisao). `papelDecisor` vem do contexto da
// sessão — nunca reconsultado.
//
// Guards (antes de qualquer escrita):
//   - `id` inexistente ou não-UUID (`pq` 22P02) -> ErrSolicitacaoNaoEncontrada.
//   - solicitação não-`pendente` -> ErrSolicitacaoNaoPendente.
//   - `papel_alvo = 'gestor'` e `papelDecisor != adm` -> ErrDecisaoNaoAutorizada.
//
// Decisão válida, numa única transação:
//   - aprovar=true: `UPDATE usuarios SET papel = <alvo> WHERE id = <solicitante>
//     AND papel = <papelAbaixoDe(alvo)>` — RowsAffected()==0 -> ErrEstadoContaMudou;
//     depois o UPDATE guardado da solicitação (molde de VerificarEmail:
//     `WHERE id = $1 AND status = 'pendente'`, sql.ErrNoRows no RETURNING ->
//     ErrSolicitacaoNaoPendente, fecha a corrida entre dois decisores).
//   - aprovar=false: só o UPDATE guardado da solicitação para `rejeitada`.
func DecidirSolicitacao(db *sql.DB, solicitacaoID, decisorID, papelDecisor string, aprovar bool) (SolicitacaoPromocao, error) {
	var solicitanteID, papelAlvo, status string
	const selectSolic = `
		SELECT solicitante_id, papel_alvo, status
		FROM solicitacoes_promocao
		WHERE id = $1`
	err := db.QueryRow(selectSolic, solicitacaoID).Scan(&solicitanteID, &papelAlvo, &status)
	if err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) {
			return SolicitacaoPromocao{}, ErrSolicitacaoNaoEncontrada
		}
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return SolicitacaoPromocao{}, ErrSolicitacaoNaoEncontrada
		}
		return SolicitacaoPromocao{}, fmt.Errorf("falha ao consultar solicitação de promoção: %w", err)
	}

	if status != "pendente" {
		return SolicitacaoPromocao{}, ErrSolicitacaoNaoPendente
	}
	if papelAlvo == PapelGestor && papelDecisor != PapelAdm {
		return SolicitacaoPromocao{}, ErrDecisaoNaoAutorizada
	}

	tx, err := db.Begin()
	if err != nil {
		return SolicitacaoPromocao{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	novoStatus := "rejeitada"
	if aprovar {
		novoStatus = "aprovada"
		res, err := tx.Exec(
			`UPDATE usuarios SET papel = $1 WHERE id = $2 AND papel = $3`,
			papelAlvo, solicitanteID, papelAbaixoDe(papelAlvo),
		)
		if err != nil {
			return SolicitacaoPromocao{}, fmt.Errorf("falha ao promover solicitante: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return SolicitacaoPromocao{}, ErrEstadoContaMudou
		}
	}

	var s SolicitacaoPromocao
	var decididoEm sql.NullTime
	const registrarDecisao = `
		UPDATE solicitacoes_promocao
		SET status = $2, decidido_por = $3, decidido_em = now()
		WHERE id = $1 AND status = 'pendente'
		RETURNING id, status, papel_alvo, decidido_em`
	err = tx.QueryRow(registrarDecisao, solicitacaoID, novoStatus, decisorID).
		Scan(&s.ID, &s.Status, &s.PapelAlvo, &decididoEm)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Outra requisição decidiu esta solicitação entre o SELECT inicial e
			// este UPDATE (corrida entre dois decisores).
			return SolicitacaoPromocao{}, ErrSolicitacaoNaoPendente
		}
		return SolicitacaoPromocao{}, fmt.Errorf("falha ao registrar decisão: %w", err)
	}
	if decididoEm.Valid {
		s.DecididoEm = &decididoEm.Time
	}

	if err := tx.Commit(); err != nil {
		return SolicitacaoPromocao{}, fmt.Errorf("falha ao commitar decisão: %w", err)
	}
	return s, nil
}
