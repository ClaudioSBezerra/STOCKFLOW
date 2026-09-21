package services

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/lib/pq"
)

// Story 11.3 (Epic 11, AD-25, FR-50): Reserva de saldo ao enviar Pedido.
// `reservas_pedido_item` guarda 1 linha por item de Pedido `pendente`; ativa
// = a linha existe, liberar = apagar. O saldo DISPONÍVEL nunca é coluna
// materializada: é sempre calculado por saldoDisponivelParTx (saldo físico de
// `produto_estoque` + `lotes` menos as reservas ativas) — Stories 11.4/11.5
// reaproveitam a mesma função.

// ErrReservaAlvoNaoEncontrado indica Produto ou Estoque inexistente, de outra
// Empresa, mesclado (soft-delete) ou com id malformado — todos colapsam no
// mesmo sentinela (nunca 403, nunca revela existência). Mapeado para 404.
var ErrReservaAlvoNaoEncontrado = errors.New("produto ou estoque não encontrado")

// ParSaldo identifica o par (Produto, Estoque) cujo saldo é travado/lido.
type ParSaldo struct {
	ProdutoID string
	EstoqueID string
}

// consultadorSQL é o subconjunto de *sql.DB/*sql.Tx usado para ler saldo.
type consultadorSQL interface {
	QueryRow(query string, args ...any) *sql.Row
}

// travarSaldoParesTx trava, dentro de `tx`, o saldo físico dos pares (AD-10):
// o conjunto COMPLETO é ordenado por (produto_id, estoque_id) ANTES de
// qualquer lock e, por par, trava primeiro a linha de `produto_estoque` (se
// existir) e depois as linhas de `lotes` do par (`ORDER BY id`). Só depois
// disso o chamador deve ler o disponível e inserir a reserva: dois envios
// concorrentes que compartilhem um par se serializam nessas linhas e o
// segundo enxerga a reserva do primeiro (READ COMMITTED, statement novo).
// Par sem nenhuma linha física não trava nada — o disponível é 0 e nenhuma
// reserva é criada, então não há o que serializar.
func travarSaldoParesTx(tx *sql.Tx, empresaID string, pares []ParSaldo) error {
	ordenados := append([]ParSaldo(nil), pares...)
	sort.Slice(ordenados, func(i, j int) bool {
		if ordenados[i].ProdutoID != ordenados[j].ProdutoID {
			return ordenados[i].ProdutoID < ordenados[j].ProdutoID
		}
		return ordenados[i].EstoqueID < ordenados[j].EstoqueID
	})

	// Os JOINs escopam a linha travada à Empresa da requisição (AD-20); um
	// par alheio simplesmente não trava nada.
	const travarProdutoEstoque = `
		SELECT pe.quantidade FROM produto_estoque pe
		JOIN produtos p ON p.id = pe.produto_id AND p.empresa_id = $3
		JOIN estoques e ON e.id = pe.estoque_id AND e.empresa_id = $3
		WHERE pe.produto_id = $1 AND pe.estoque_id = $2
		FOR UPDATE OF pe`
	const travarLotes = `
		SELECT id FROM lotes
		WHERE produto_id = $1 AND estoque_id = $2 AND empresa_id = $3
		ORDER BY id
		FOR UPDATE`

	for i, par := range ordenados {
		if i > 0 && par == ordenados[i-1] {
			continue
		}
		var ignorada float64
		if err := tx.QueryRow(travarProdutoEstoque, par.ProdutoID, par.EstoqueID, empresaID).Scan(&ignorada); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("falha ao travar linha de produto_estoque: %w", err)
		}
		rows, err := tx.Query(travarLotes, par.ProdutoID, par.EstoqueID, empresaID)
		if err != nil {
			return fmt.Errorf("falha ao travar lotes do par: %w", err)
		}
		for rows.Next() {
		}
		errRows := rows.Err()
		rows.Close()
		if errRows != nil {
			return fmt.Errorf("falha ao iterar lotes travados do par: %w", errRows)
		}
	}
	return nil
}

// saldoDisponivelParTx devolve o saldo DISPONÍVEL do par: soma de
// `produto_estoque` e `lotes` menos a soma das reservas ativas, tudo escopado
// à Empresa. Ausência de linha em qualquer tabela conta como 0. O resultado
// pode ser negativo (reservas de backfill acima do saldo) — quem exibe trunca
// em 0. Para ser confiável contra corrida, chame-a DEPOIS de
// travarSaldoParesTx na mesma transação.
func saldoDisponivelParTx(q consultadorSQL, empresaID, produtoID, estoqueID string) (float64, error) {
	const consulta = `
		SELECT
			COALESCE((
				SELECT SUM(pe.quantidade) FROM produto_estoque pe
				JOIN produtos p ON p.id = pe.produto_id AND p.empresa_id = $3
				JOIN estoques e ON e.id = pe.estoque_id AND e.empresa_id = $3
				WHERE pe.produto_id = $1 AND pe.estoque_id = $2
			), 0)
			+ COALESCE((
				SELECT SUM(l.quantidade) FROM lotes l
				WHERE l.produto_id = $1 AND l.estoque_id = $2 AND l.empresa_id = $3
			), 0)
			- COALESCE((
				SELECT SUM(r.quantidade) FROM reservas_pedido_item r
				WHERE r.produto_id = $1 AND r.estoque_id = $2 AND r.empresa_id = $3
			), 0)`
	var disponivel float64
	if err := q.QueryRow(consulta, produtoID, estoqueID, empresaID).Scan(&disponivel); err != nil {
		return 0, fmt.Errorf("falha ao calcular saldo disponível: %w", err)
	}
	return disponivel, nil
}

// liberarReservasPedidoTx apaga todas as reservas do Pedido (liberar =
// apagar) — chamado por DecidirPedido, na mesma transação da decisão.
func liberarReservasPedidoTx(tx *sql.Tx, empresaID, pedidoID string) error {
	if _, err := tx.Exec(`DELETE FROM reservas_pedido_item WHERE pedido_id = $1 AND empresa_id = $2`, pedidoID, empresaID); err != nil {
		return fmt.Errorf("falha ao liberar reservas do pedido: %w", err)
	}
	return nil
}

// ReservaSaldo é um Pedido pendente que reserva saldo de um par (Produto,
// Estoque) — a resposta de GET /api/produtos/{id}/estoques/{estoqueId}/reservas.
type ReservaSaldo struct {
	PedidoID    string    `json:"pedidoId"`
	Solicitante string    `json:"solicitante"`
	Quantidade  float64   `json:"quantidade"`
	CriadoEm    time.Time `json:"criadoEm"`
}

// ListarReservasSaldo lista os Pedidos com reserva ativa do par, do mais
// antigo ao mais recente. Produto ativo/Estoque inexistente, alheio ou com id
// malformado -> ErrReservaAlvoNaoEncontrado.
func ListarReservasSaldo(db *sql.DB, empresaID, produtoID, estoqueID string) ([]ReservaSaldo, error) {
	var existe bool
	err := db.QueryRow(`
		SELECT EXISTS (SELECT 1 FROM produtos WHERE id = $1 AND deleted_at IS NULL AND empresa_id = $3)
		   AND EXISTS (SELECT 1 FROM estoques WHERE id = $2 AND empresa_id = $3)`,
		produtoID, estoqueID, empresaID).Scan(&existe)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return nil, ErrReservaAlvoNaoEncontrado
		}
		return nil, fmt.Errorf("falha ao verificar produto/estoque das reservas: %w", err)
	}
	if !existe {
		return nil, ErrReservaAlvoNaoEncontrado
	}

	rows, err := db.Query(`
		SELECT ped.id, ped.solicitante, r.quantidade, ped.criado_em
		FROM reservas_pedido_item r
		JOIN pedidos ped ON ped.id = r.pedido_id AND ped.empresa_id = $3
		WHERE r.produto_id = $1 AND r.estoque_id = $2 AND r.empresa_id = $3
		ORDER BY ped.criado_em, ped.id`, produtoID, estoqueID, empresaID)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar reservas do par: %w", err)
	}
	defer rows.Close()
	lista := make([]ReservaSaldo, 0)
	for rows.Next() {
		var r ReservaSaldo
		if err := rows.Scan(&r.PedidoID, &r.Solicitante, &r.Quantidade, &r.CriadoEm); err != nil {
			return nil, fmt.Errorf("falha ao ler reserva do par: %w", err)
		}
		lista = append(lista, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar reservas do par: %w", err)
	}
	return lista, nil
}
