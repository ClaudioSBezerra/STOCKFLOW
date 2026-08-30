package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

// Estoque é a projeção somente-leitura de um local de estoque devolvida por
// POST/GET /api/estoques (Story 2.1): `id` (UUID v4) + `nome`, nada mais — a
// tela "Locais" e as telas de catálogo (Epic 4) só precisam disso.
type Estoque struct {
	ID   string `json:"id"`
	Nome string `json:"nome"`
}

var (
	// ErrEstoqueValidacao indica nome de Estoque vazio após o trim ou com mais
	// de 255 runes — mapeado para 400 VALIDATION_ERROR, sem tocar o banco.
	ErrEstoqueValidacao = errors.New("o nome do estoque é obrigatório e deve ter no máximo 255 caracteres")
	// ErrNomeEstoqueDuplicado indica que o nome normalizado (minúsculas +
	// espaços colapsados) já existe: a segunda linha bateu no índice único
	// idx_estoques_nome_normalizado (SQLSTATE 23505). É backstop de corrida —
	// não há checagem prévia. Mapeado para 409 CONFLICT.
	ErrNomeEstoqueDuplicado = errors.New("já existe um estoque com esse nome")
)

// CriarEstoque valida e insere um novo local de estoque (Story 2.1, FR12).
//
// O `nome` recebido é trimado nas pontas; vazio após o trim ou com mais de
// 255 runes -> ErrEstoqueValidacao (nenhuma escrita). Caso válido, um único
// INSERT ... RETURNING grava a linha — a coluna gerada `nome_normalizado` e
// o índice único idx_estoques_nome_normalizado impõem a unicidade de nome no
// próprio banco. Uma violação de unicidade (`pq` SQLSTATE 23505) é o único
// sinal de nome duplicado: traduzida para ErrNomeEstoqueDuplicado, sem
// nenhum SELECT-antes-de-INSERT que teria janela de corrida sob requisições
// concorrentes.
func CriarEstoque(db *sql.DB, nome string) (Estoque, error) {
	nomeTrimado := strings.TrimSpace(nome)
	if nomeTrimado == "" || utf8.RuneCountInString(nomeTrimado) > 255 {
		return Estoque{}, ErrEstoqueValidacao
	}

	var e Estoque
	const insert = `INSERT INTO estoques (nome) VALUES ($1) RETURNING id, nome`
	if err := db.QueryRow(insert, nomeTrimado).Scan(&e.ID, &e.Nome); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation {
			return Estoque{}, ErrNomeEstoqueDuplicado
		}
		return Estoque{}, fmt.Errorf("falha ao inserir estoque: %w", err)
	}
	return e, nil
}

// ListarEstoques devolve todos os locais de estoque ordenados por
// `nome_normalizado` ascendente (Story 2.1, AC4). Sem filtro de escopo:
// qualquer conta autenticada vê todos os Estoques (o gate de leitura é só
// RequireAuth em newMux). Lista vazia não é erro — devolve um slice vazio,
// nunca nil.
func ListarEstoques(db *sql.DB) ([]Estoque, error) {
	rows, err := db.Query(`SELECT id, nome FROM estoques ORDER BY nome_normalizado ASC`)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar estoques: %w", err)
	}
	defer rows.Close()

	estoques := make([]Estoque, 0)
	for rows.Next() {
		var e Estoque
		if err := rows.Scan(&e.ID, &e.Nome); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de estoque: %w", err)
		}
		estoques = append(estoques, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar estoques: %w", err)
	}
	return estoques, nil
}
