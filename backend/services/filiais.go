package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

// Filiais — Story 12.1 (Epic 12, FR-51, AD-27). Filial é um nível de
// organização física dentro da Empresa (Empresa -> Filial -> Estoque); o
// isolamento continua só por Empresa (AD-20): toda query filtra `empresa_id`
// vindo do contexto, nunca do cliente. Escrita restrita a `adm`+ (gate no
// roteamento, RequireRole).

// Filial é a projeção devolvida por POST/GET /api/filiais.
type Filial struct {
	ID   string `json:"id"`
	Nome string `json:"nome"`
}

var (
	// ErrFilialValidacao: nome de Filial vazio após o trim, com mais de 255
	// runes ou com byte inválido. Mapeado para 400 VALIDATION_ERROR.
	ErrFilialValidacao = errors.New("o nome da filial é obrigatório e deve ter no máximo 255 caracteres")
	// ErrNomeFilialDuplicado: o nome normalizado já existe na Empresa
	// (colisão do índice único, 23505). Mapeado para 409 CONFLICT.
	ErrNomeFilialDuplicado = errors.New("já existe uma filial com esse nome")
	// ErrFilialInvalida: `filial_id` ausente, malformado ou de outra Empresa
	// ao criar um Estoque. Mapeado para 400 VALIDATION_ERROR.
	ErrFilialInvalida = errors.New("a filial é obrigatória e deve pertencer à empresa")
	// ErrEmpresaSemFilial: a Empresa não tem nenhuma Filial (legada, antes da
	// Story 12.2) e um Estoque precisaria ser criado na Filial padrão.
	ErrEmpresaSemFilial = errors.New("a empresa não possui nenhuma filial cadastrada")
)

// CriarFilial insere uma Filial na Empresa `empresaID`. A unicidade por
// Empresa vem do índice único (sem SELECT prévio, sem janela de corrida).
func CriarFilial(db *sql.DB, empresaID, nome string) (Filial, error) {
	nome = strings.TrimSpace(nome)
	if nome == "" || strings.ContainsRune(nome, 0) || utf8.RuneCountInString(nome) > 255 {
		return Filial{}, ErrFilialValidacao
	}
	var f Filial
	const insert = `INSERT INTO filiais (empresa_id, nome) VALUES ($1, $2) RETURNING id, nome`
	if err := db.QueryRow(insert, empresaID, nome).Scan(&f.ID, &f.Nome); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) {
			switch pqErr.Code {
			case pqUniqueViolation:
				return Filial{}, ErrNomeFilialDuplicado
			case pqStringDataRightTruncation, pqInvalidByteSequence:
				return Filial{}, ErrFilialValidacao
			}
		}
		return Filial{}, fmt.Errorf("falha ao inserir filial: %w", err)
	}
	return f, nil
}

// ListarFiliais devolve as Filiais da Empresa `empresaID` ordenadas por nome
// normalizado. Lista vazia não é erro — devolve um slice vazio, nunca nil.
func ListarFiliais(db *sql.DB, empresaID string) ([]Filial, error) {
	rows, err := db.Query(
		`SELECT id, nome FROM filiais WHERE empresa_id = $1 ORDER BY nome_normalizado ASC, id`,
		empresaID)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar filiais: %w", err)
	}
	defer rows.Close()

	filiais := make([]Filial, 0)
	for rows.Next() {
		var f Filial
		if err := rows.Scan(&f.ID, &f.Nome); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de filial: %w", err)
		}
		filiais = append(filiais, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar filiais: %w", err)
	}
	return filiais, nil
}

// queryRower é satisfeito por *sql.DB e *sql.Tx.
type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

// filialPadraoDaEmpresa devolve o id da Filial padrão da Empresa: a mais
// antiga (`ORDER BY criado_em, id`). Empresa sem nenhuma Filial ->
// ErrEmpresaSemFilial (nunca cria Estoque órfão).
func filialPadraoDaEmpresa(q queryRower, empresaID string) (string, error) {
	var id string
	err := q.QueryRow(
		`SELECT id FROM filiais WHERE empresa_id = $1 ORDER BY criado_em, id LIMIT 1`,
		empresaID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrEmpresaSemFilial
	}
	if err != nil {
		return "", fmt.Errorf("falha ao buscar filial padrão da empresa: %w", err)
	}
	return id, nil
}
