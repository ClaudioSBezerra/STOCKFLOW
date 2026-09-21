package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

// Centros de Custo — Story 12.3 (Epic 12, FR-51, AD-28). Lista padronizada de
// obras/centros de custo POR EMPRESA (molde de filiais.go); o isolamento
// continua só por Empresa (AD-20): toda query filtra `empresa_id` vindo do
// contexto, nunca do cliente. Escrita restrita a `adm`+ (gate no
// roteamento, RequireRole). Sem editar/excluir nesta story.

// CentroCusto é a projeção devolvida por POST/GET /api/centros-custo.
type CentroCusto struct {
	ID   string `json:"id"`
	Nome string `json:"nome"`
}

var (
	// ErrCentroCustoValidacao: nome vazio após o trim, com mais de 255 runes
	// ou com byte inválido. Mapeado para 400 VALIDATION_ERROR.
	ErrCentroCustoValidacao = errors.New("o nome do centro de custo é obrigatório e deve ter no máximo 255 caracteres")
	// ErrNomeCentroCustoDuplicado: o nome normalizado já existe na Empresa
	// (colisão do índice único, 23505). Mapeado para 409 CONFLICT.
	ErrNomeCentroCustoDuplicado = errors.New("já existe um centro de custo com esse nome")
)

// CriarCentroCusto insere um Centro de Custo na Empresa `empresaID`. A
// unicidade por Empresa vem do índice único (sem SELECT prévio).
func CriarCentroCusto(db *sql.DB, empresaID, nome string) (CentroCusto, error) {
	nome = strings.TrimSpace(nome)
	if nome == "" || strings.ContainsRune(nome, 0) || utf8.RuneCountInString(nome) > 255 {
		return CentroCusto{}, ErrCentroCustoValidacao
	}
	var c CentroCusto
	const insert = `INSERT INTO centros_custo (empresa_id, nome) VALUES ($1, $2) RETURNING id, nome`
	if err := db.QueryRow(insert, empresaID, nome).Scan(&c.ID, &c.Nome); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) {
			switch pqErr.Code {
			case pqUniqueViolation:
				return CentroCusto{}, ErrNomeCentroCustoDuplicado
			case pqStringDataRightTruncation, pqInvalidByteSequence:
				return CentroCusto{}, ErrCentroCustoValidacao
			}
		}
		return CentroCusto{}, fmt.Errorf("falha ao inserir centro de custo: %w", err)
	}
	return c, nil
}

// ListarCentrosCusto devolve os Centros de Custo da Empresa `empresaID`
// ordenados por nome normalizado. Lista vazia devolve slice vazio, nunca nil.
func ListarCentrosCusto(db *sql.DB, empresaID string) ([]CentroCusto, error) {
	rows, err := db.Query(
		`SELECT id, nome FROM centros_custo WHERE empresa_id = $1 ORDER BY nome_normalizado ASC, id`,
		empresaID)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar centros de custo: %w", err)
	}
	defer rows.Close()

	centros := make([]CentroCusto, 0)
	for rows.Next() {
		var c CentroCusto
		if err := rows.Scan(&c.ID, &c.Nome); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de centro de custo: %w", err)
		}
		centros = append(centros, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar centros de custo: %w", err)
	}
	return centros, nil
}

// validarCentroCustoDaEmpresa confirma que `centroCustoID` existe na Empresa
// `empresaID`. Inexistente, malformado (22P02) ou de outra Empresa ->
// &ErroPedidoValidacao (400), sem revelar a existência em outra Empresa.
func validarCentroCustoDaEmpresa(q queryRower, empresaID, centroCustoID string) error {
	var existe bool
	err := q.QueryRow(
		`SELECT true FROM centros_custo WHERE id = $1 AND empresa_id = $2`,
		centroCustoID, empresaID,
	).Scan(&existe)
	if err == nil {
		return nil
	}
	var pqErr *pq.Error
	if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
		return &ErroPedidoValidacao{Mensagem: "centro de custo inválido"}
	}
	return fmt.Errorf("falha ao validar centro de custo: %w", err)
}
