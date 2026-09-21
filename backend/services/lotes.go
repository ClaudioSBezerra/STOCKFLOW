package services

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/lib/pq"
)

// Story 11.1 (Epic 11, AD-24/AD-10, FR-47): Lançamento de saldo inicial com
// Lote e Data de Validade. Cada lançamento cria SEMPRE um Lote novo em
// `lotes` e a Movimentação `entrada` correspondente numa única transação —
// nunca atualiza um Lote existente nem `produto_estoque`.

// ErrLoteAlvoNaoEncontrado indica que o Produto ou o Estoque do lançamento
// não existe, é de outra Empresa, foi mesclado (soft-delete) ou tem id
// malformado — os quatro casos colapsam no mesmo sentinela (nunca 403, nunca
// revela existência). Mapeado para 404 NOT_FOUND.
var ErrLoteAlvoNaoEncontrado = errors.New("produto ou estoque não encontrado")

// ErroLoteValidacao é o erro de validação de LancarSaldo (quantidade fora de
// (0, limiteNumeric103] ou data de validade inválida) — sempre devolvido
// ANTES de tocar o banco. Mapeado para 400 VALIDATION_ERROR.
type ErroLoteValidacao struct {
	Mensagem string
}

func (e *ErroLoteValidacao) Error() string { return e.Mensagem }

// LoteLancado é a projeção do Lote recém-criado devolvida por LancarSaldo.
// `DataValidade` é `nil` quando a validade é desconhecida; `Vencido` é
// `data_validade < CURRENT_DATE` (o dia da validade ainda não é vencido).
// `MovimentacaoID` não entra no JSON do Lote — o handler o usa para publicar
// o evento SSE.
type LoteLancado struct {
	ID             string    `json:"id"`
	ProdutoID      string    `json:"produtoId"`
	EstoqueID      string    `json:"estoqueId"`
	Quantidade     float64   `json:"quantidade"`
	DataValidade   *string   `json:"dataValidade"`
	Vencido        bool      `json:"vencido"`
	MovimentacaoID string    `json:"-"`
	CriadoEm       time.Time `json:"-"`
}

// formatoDataValidade é o único formato aceito para `dataValidade`.
const formatoDataValidade = "2006-01-02"

// validarDataValidade devolve a data normalizada ("" = validade desconhecida)
// ou erro de validação. Formato estrito YYYY-MM-DD; datas passadas são aceitas.
func validarDataValidade(valor string) (string, error) {
	if valor == "" {
		return "", nil
	}
	t, err := time.Parse(formatoDataValidade, valor)
	if err != nil || len(valor) != len(formatoDataValidade) || t.Year() < 1 {
		return "", &ErroLoteValidacao{Mensagem: "dataValidade deve estar no formato YYYY-MM-DD"}
	}
	return t.Format(formatoDataValidade), nil
}

// LancarSaldo cria um Lote novo com `quantidade` unidades do Produto
// `produtoID` no Estoque `estoqueID` da Empresa `empresaID` e registra a
// Movimentação `tipo='entrada'` (com `lote_id`) na mesma transação.
// `dataValidade` vazia = desconhecida; senão YYYY-MM-DD estrito (passado é
// aceito — o Lote nasce vencido, só sinalizado).
//
// Validação (quantidade > 0 e <= limiteNumeric103, data parseável) acontece
// ANTES de abrir a transação. O INSERT em `lotes` é um `INSERT ... SELECT`
// que filtra `produtos`/`estoques` por `empresa_id` (e o Produto por
// `deleted_at IS NULL`): zero linhas ou SQLSTATE 22P02 -> ErrLoteAlvoNaoEncontrado.
// A FK `lotes.estoque_id` toma KEY SHARE na linha de `estoques`, o que
// serializa com ExcluirEstoque (FOR UPDATE).
func LancarSaldo(db *sql.DB, empresaID, usuarioID, produtoID, estoqueID string, quantidade float64, dataValidade string) (LoteLancado, error) {
	if quantidade <= 0 || math.Round(quantidade*1000) <= 0 {
		return LoteLancado{}, &ErroLoteValidacao{Mensagem: "quantidade deve ser maior que zero"}
	}
	if quantidade > limiteNumeric103 {
		return LoteLancado{}, &ErroLoteValidacao{
			Mensagem: fmt.Sprintf("quantidade deve ser no máximo %s", limiteNumeric103Texto),
		}
	}
	data, err := validarDataValidade(dataValidade)
	if err != nil {
		return LoteLancado{}, err
	}
	var dataArg sql.NullString
	if data != "" {
		dataArg = sql.NullString{String: data, Valid: true}
	}

	tx, err := db.Begin()
	if err != nil {
		return LoteLancado{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	var lote LoteLancado
	var validade sql.NullString
	const insertLote = `
		INSERT INTO lotes (produto_id, estoque_id, quantidade, data_validade, empresa_id)
		SELECT p.id, e.id, $3::numeric, $4::date, p.empresa_id
		FROM produtos p, estoques e
		WHERE p.id = $1 AND p.empresa_id = $5 AND p.deleted_at IS NULL
		  AND e.id = $2 AND e.empresa_id = $5
		FOR SHARE OF p, e
		RETURNING id, produto_id, estoque_id, quantidade, to_char(data_validade, 'YYYY-MM-DD'),
		          COALESCE(data_validade < CURRENT_DATE, false), criado_em`
	if err := tx.QueryRow(insertLote, produtoID, estoqueID, quantidade, dataArg, empresaID).Scan(
		&lote.ID, &lote.ProdutoID, &lote.EstoqueID, &lote.Quantidade, &validade, &lote.Vencido, &lote.CriadoEm,
	); err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && (pqErr.Code == pqInvalidTextRepresentation || pqErr.Code == pqForeignKeyViolation)) {
			return LoteLancado{}, ErrLoteAlvoNaoEncontrado
		}
		return LoteLancado{}, fmt.Errorf("falha ao inserir lote: %w", err)
	}
	if validade.Valid {
		v := validade.String
		lote.DataValidade = &v
	}

	const insertMov = `
		INSERT INTO movimentacoes (produto_id, tipo, estoque_origem_id, estoque_destino_id, quantidade, usuario_id, empresa_id, lote_id)
		VALUES ($1, 'entrada', NULL, $2, $3, $4, $5, $6)
		RETURNING id`
	if err := tx.QueryRow(insertMov, lote.ProdutoID, lote.EstoqueID, lote.Quantidade, usuarioID, empresaID, lote.ID).Scan(&lote.MovimentacaoID); err != nil {
		return LoteLancado{}, fmt.Errorf("falha ao inserir movimentação de entrada: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return LoteLancado{}, fmt.Errorf("falha ao commitar lançamento de saldo: %w", err)
	}
	return lote, nil
}
