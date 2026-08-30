package services

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"
)

// Gestão de contas — desativação e rebaixamento (Story 1.8, spec-1-8). Duas
// operações de escrita sobre `usuarios` para `gestor`/`adm`:
//
//   - AlterarAtivacaoUsuario: liga/desliga `usuarios.ativo`; ao desligar,
//     revoga TODAS as sessões vivas do alvo na mesma transação (molde de
//     RedefinirSenha, auth.go) — necessário porque POST /api/auth/refresh não
//     passa por RequireAuth e RenovarSessao não checa `ativo`.
//   - RebaixarUsuario: desce o papel do alvo UM degrau na hierarquia (inverso
//     exato de proximoPapelPromocao, Story 1.7). Não revoga sessões: a conta
//     segue autenticando, só com menos privilégio (RequireAuth relê o papel a
//     cada requisição).
//
// O alvo do rebaixamento é SEMPRE derivado no servidor de
// papelImediatamenteAbaixo(papelAtual) — nenhum corpo de requisição informa
// papel. O recorte de autoridade e a guarda de auto-ação são decididos aqui, a
// partir do papel do ator JÁ resolvido pelo contexto (middleware.UsuarioDaSessao)
// e passado como argumento — esta camada NUNCA reconsulta `usuarios` para
// descobrir o papel de quem chama (AD-8 forma 3, molde de ListarUsuarios).
var (
	// ErrGestaoForaDeEscopo indica que o ator não pode agir sobre o alvo: um
	// `gestor` tentando desativar/rebaixar `gestor`/`adm`, ou qualquer conta
	// agindo sobre si mesma (guarda de auto-ação — evita um `adm` se trancar
	// para fora). Handler -> 403 FORBIDDEN.
	ErrGestaoForaDeEscopo = errors.New("papel insuficiente para agir sobre esta conta")
	// ErrContaNaoEncontrada indica `id` inexistente OU malformado (não-UUID,
	// `pq` 22P02) — os dois caem no mesmo erro. Handler -> 404 NOT_FOUND.
	ErrContaNaoEncontrada = errors.New("conta não encontrada")
	// ErrRebaixamentoIndisponivel indica que o papel atual do alvo não tem
	// papel abaixo na hierarquia (`usuario`). Handler -> 409 CONFLICT.
	ErrRebaixamentoIndisponivel = errors.New("não há papel abaixo para rebaixar esta conta")
)

// papelImediatamenteAbaixo devolve o papel um degrau abaixo na hierarquia
// (AD-8): `gestor -> almoxarife`, `almoxarife -> usuario`. `usuario` e `adm`
// (e qualquer valor inesperado) não têm papel abaixo aplicável a rebaixamento
// — devolvem ("", false). É o inverso exato de proximoPapelPromocao
// (promocao.go) e a ÚNICA fonte do papel-alvo do rebaixamento: nenhum corpo
// de requisição o informa.
func papelImediatamenteAbaixo(papel string) (string, bool) {
	switch papel {
	case PapelGestor:
		return PapelAlmoxarife, true
	case PapelAlmoxarife:
		return PapelUsuario, true
	default:
		return "", false
	}
}

// carregarAlvoParaGestao resolve o papel atual do alvo e aplica, antes de
// qualquer escrita, os guards comuns às duas operações:
//
//   - `id` inexistente ou não-UUID (`pq` 22P02) -> ErrContaNaoEncontrada.
//   - `alvoID == atorID` -> ErrGestaoForaDeEscopo (guarda de auto-ação).
//   - ator abaixo de `adm` (na prática `gestor`) agindo sobre alvo `gestor`/
//     `adm` -> ErrGestaoForaDeEscopo. `adm` age sobre qualquer conta.
func carregarAlvoParaGestao(db *sql.DB, alvoID, atorID, papelAtor string) (string, error) {
	var papelAlvo string
	err := db.QueryRow(`SELECT papel FROM usuarios WHERE id = $1`, alvoID).Scan(&papelAlvo)
	if err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrContaNaoEncontrada
		}
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return "", ErrContaNaoEncontrada
		}
		return "", fmt.Errorf("falha ao consultar conta alvo: %w", err)
	}

	if alvoID == atorID {
		return "", ErrGestaoForaDeEscopo
	}
	if RankPapel(papelAtor) < RankPapel(PapelAdm) && (papelAlvo == PapelGestor || papelAlvo == PapelAdm) {
		return "", ErrGestaoForaDeEscopo
	}
	return papelAlvo, nil
}

// AlterarAtivacaoUsuario liga (`ativo=true`) ou desliga (`ativo=false`) a
// conta alvo (POST /api/usuarios/{id}/desativacao). Guards de
// id/escopo/auto-ação via carregarAlvoParaGestao. Caso válido, numa única
// transação: `UPDATE usuarios SET ativo = $1 WHERE id = $2 AND papel = $3`
// (o guard do papel atual fecha a corrida com uma promoção concorrente —
// RowsAffected()==0 -> ErrEstadoContaMudou, reusado de promocao.go, antes de
// qualquer revogação) e, se `ativo=false`, também
// `UPDATE sessoes SET revogado_em = now() WHERE usuario_id = $2 AND
// revogado_em IS NULL` (molde de RedefinirSenha). Devolve o UsuarioResumo já
// atualizado.
func AlterarAtivacaoUsuario(db *sql.DB, alvoID, atorID, papelAtor string, ativo bool) (UsuarioResumo, error) {
	papelAlvo, err := carregarAlvoParaGestao(db, alvoID, atorID, papelAtor)
	if err != nil {
		return UsuarioResumo{}, err
	}

	tx, err := db.Begin()
	if err != nil {
		return UsuarioResumo{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	// Guard do papel atual (mesmo padrão de RebaixarUsuario): fecha a janela
	// entre o SELECT sem lock de carregarAlvoParaGestao e esta escrita. Um
	// alvo que passou no recorte de escopo como `almoxarife` mas foi promovido
	// a `gestor`/`adm` numa corrida NÃO pode ser desativado por um `gestor`
	// (AC3) — RowsAffected()==0 -> ErrEstadoContaMudou, antes de qualquer
	// revogação de sessão.
	res, err := tx.Exec(`UPDATE usuarios SET ativo = $1 WHERE id = $2 AND papel = $3`, ativo, alvoID, papelAlvo)
	if err != nil {
		return UsuarioResumo{}, fmt.Errorf("falha ao alterar ativação da conta: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return UsuarioResumo{}, ErrEstadoContaMudou
	}
	if !ativo {
		if _, err := tx.Exec(
			`UPDATE sessoes SET revogado_em = now() WHERE usuario_id = $1 AND revogado_em IS NULL`,
			alvoID,
		); err != nil {
			return UsuarioResumo{}, fmt.Errorf("falha ao revogar sessões da conta: %w", err)
		}
	}

	u, err := relerUsuarioResumoTx(tx, alvoID)
	if err != nil {
		return UsuarioResumo{}, err
	}

	if err := tx.Commit(); err != nil {
		return UsuarioResumo{}, fmt.Errorf("falha ao commitar alteração de ativação: %w", err)
	}
	return u, nil
}

// RebaixarUsuario desce o papel do alvo um degrau na hierarquia
// (POST /api/usuarios/{id}/rebaixamento). Guards de id/escopo/auto-ação via
// carregarAlvoParaGestao. Alvo já `usuario` (sem papel abaixo) ->
// ErrRebaixamentoIndisponivel. Caso válido:
// `UPDATE usuarios SET papel = <abaixo> WHERE id = <alvo> AND papel = <atual>`
// — o guard do papel atual fecha a corrida com uma promoção/rebaixamento
// concorrente (RowsAffected()==0 -> ErrEstadoContaMudou, reusado de
// promocao.go). NÃO revoga sessões. Devolve o UsuarioResumo já atualizado.
func RebaixarUsuario(db *sql.DB, alvoID, atorID, papelAtor string) (UsuarioResumo, error) {
	papelAlvo, err := carregarAlvoParaGestao(db, alvoID, atorID, papelAtor)
	if err != nil {
		return UsuarioResumo{}, err
	}

	abaixo, ok := papelImediatamenteAbaixo(papelAlvo)
	if !ok {
		return UsuarioResumo{}, ErrRebaixamentoIndisponivel
	}

	tx, err := db.Begin()
	if err != nil {
		return UsuarioResumo{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	res, err := tx.Exec(
		`UPDATE usuarios SET papel = $1 WHERE id = $2 AND papel = $3`,
		abaixo, alvoID, papelAlvo,
	)
	if err != nil {
		return UsuarioResumo{}, fmt.Errorf("falha ao rebaixar conta: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return UsuarioResumo{}, ErrEstadoContaMudou
	}

	u, err := relerUsuarioResumoTx(tx, alvoID)
	if err != nil {
		return UsuarioResumo{}, err
	}

	if err := tx.Commit(); err != nil {
		return UsuarioResumo{}, fmt.Errorf("falha ao commitar rebaixamento: %w", err)
	}
	return u, nil
}

// relerUsuarioResumoTx relê a projeção somente-leitura da conta dentro da
// transação em curso — a resposta 200 das duas rotas devolve o estado já
// aplicado, nunca o de antes da escrita.
func relerUsuarioResumoTx(tx *sql.Tx, id string) (UsuarioResumo, error) {
	var u UsuarioResumo
	err := tx.QueryRow(
		`SELECT id, nome, email, papel, ativo FROM usuarios WHERE id = $1`, id,
	).Scan(&u.ID, &u.Nome, &u.Email, &u.Papel, &u.Ativo)
	if err != nil {
		return UsuarioResumo{}, fmt.Errorf("falha ao reler conta após a escrita: %w", err)
	}
	return u, nil
}
