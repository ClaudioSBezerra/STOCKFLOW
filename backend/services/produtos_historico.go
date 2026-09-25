package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lib/pq"
)

// HistoricoProdutoItem é uma linha do histórico do Produto (Story 16.3).
type HistoricoProdutoItem struct {
	ID       string         `json:"id"`
	Acao     string         `json:"acao"`
	Autor    string         `json:"autor"`
	Detalhe  map[string]any `json:"detalhe"`
	CriadoEm string         `json:"criadoEm"`
}

// ListarHistoricoProduto devolve o histórico do Produto, do mais recente ao
// mais antigo. Produto inexistente/malformado/mesclado/de outra Empresa ->
// ErrProdutoNaoEncontrado (Produto inativo existe e é listado).
func ListarHistoricoProduto(db *sql.DB, empresaID, produtoID string) ([]HistoricoProdutoItem, error) {
	var existe bool
	err := db.QueryRow(
		`SELECT true FROM produtos WHERE id = $1 AND empresa_id = $2 AND deleted_at IS NULL`,
		produtoID, empresaID,
	).Scan(&existe)
	if err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return nil, ErrProdutoNaoEncontrado
		}
		return nil, fmt.Errorf("falha ao buscar produto: %w", err)
	}

	rows, err := db.Query(
		`SELECT h.id, h.acao, COALESCE(u.nome, ''), h.detalhe, to_char(h.criado_em AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
		 FROM produto_historico h
		 LEFT JOIN usuarios u ON u.id = h.ator_id
		 WHERE h.empresa_id = $1 AND h.produto_id = $2
		 ORDER BY h.criado_em DESC, h.id DESC`,
		empresaID, produtoID,
	)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar histórico do produto: %w", err)
	}
	defer rows.Close()

	itens := []HistoricoProdutoItem{}
	for rows.Next() {
		var it HistoricoProdutoItem
		var raw []byte
		if err := rows.Scan(&it.ID, &it.Acao, &it.Autor, &raw, &it.CriadoEm); err != nil {
			return nil, fmt.Errorf("falha ao ler histórico do produto: %w", err)
		}
		if err := json.Unmarshal(raw, &it.Detalhe); err != nil {
			return nil, fmt.Errorf("detalhe do histórico inválido: %w", err)
		}
		if it.Detalhe == nil { // JSON `null` desserializa para mapa nil
			it.Detalhe = map[string]any{}
		}
		itens = append(itens, it)
	}
	return itens, rows.Err()
}
