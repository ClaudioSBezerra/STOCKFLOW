package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

// CRUD de Categorias — Story 10.5 (Epic 10, AD-33). Escrita restrita a `adm`+
// (gate no roteamento, RequireRole); toda query filtra `empresa_id` (AD-20).
// A listagem continua em ListarCategorias (produtos.go).

const (
	// categoriaCodigoMax e categoriaNomeMax espelham VARCHAR(8)/VARCHAR(50) de
	// `categorias` (migration 000039).
	categoriaCodigoMax = 8
	categoriaNomeMax   = 50

	// pqStringDataRightTruncation é o SQLSTATE 22001 (valor longo demais para
	// a coluna) — backstop do limite imposto pelo banco.
	pqStringDataRightTruncation = "22001"
	// pqCheckViolation é o SQLSTATE 23514 — backstop do CHECK de não-vazio.
	pqCheckViolation = "23514"
	// pqInvalidByteSequence é o SQLSTATE 22021 — texto com byte inválido
	// (ex. NUL vindo de "\u0000" no JSON): entrada de cliente, não falha do servidor.
	pqInvalidByteSequence = "22021"

	idxCategoriasEmpresaCodigo = "idx_categorias_empresa_codigo"
	idxCategoriasEmpresaNome   = "idx_categorias_empresa_nome"
)

var (
	// ErrCategoriaValidacao: `codigo`/`nome` vazio após o trim, ou acima de
	// 8 / 50 runes. Mapeado para 400 VALIDATION_ERROR, sem tocar o banco.
	ErrCategoriaValidacao = errors.New("o código (até 8 caracteres) e o nome (até 50 caracteres) da categoria são obrigatórios")
	// ErrCategoriaNaoEncontrada: `id` inexistente, de outra Empresa ou
	// não-UUID (22P02) — todos colapsam aqui. Mapeado para 404.
	ErrCategoriaNaoEncontrada = errors.New("categoria não encontrada")
)

// ErrCategoriaDuplicada indica colisão de unicidade por Empresa (23505 em
// idx_categorias_empresa_codigo/_nome). `Campo` é "código" ou "nome".
// Mapeado para 409 CONFLICT.
type ErrCategoriaDuplicada struct {
	Campo string
}

func (e *ErrCategoriaDuplicada) Error() string {
	return fmt.Sprintf("já existe uma categoria com esse %s", e.Campo)
}

// ErroCategoriaEmUso indica que a exclusão foi bloqueada por Produtos que
// referenciam a Categoria. Mapeado para 409 CONFLICT.
type ErroCategoriaEmUso struct {
	Produtos int
}

func (e *ErroCategoriaEmUso) Error() string {
	if e.Produtos == 1 {
		return "a categoria não pode ser excluída: está em uso por 1 produto"
	}
	return fmt.Sprintf("a categoria não pode ser excluída: está em uso por %d produtos", e.Produtos)
}

// validarCategoria trima e valida `codigo`/`nome` (vazio ou acima do limite
// em runes -> ErrCategoriaValidacao).
func validarCategoria(codigo, nome string) (string, string, error) {
	codigo = strings.TrimSpace(codigo)
	nome = strings.TrimSpace(nome)
	if codigo == "" || nome == "" ||
		strings.ContainsRune(codigo, 0) || strings.ContainsRune(nome, 0) ||
		utf8.RuneCountInString(codigo) > categoriaCodigoMax ||
		utf8.RuneCountInString(nome) > categoriaNomeMax {
		return "", "", ErrCategoriaValidacao
	}
	return codigo, nome, nil
}

// traduzirErroEscritaCategoria converte erros do Postgres nos erros de
// domínio. Devolve nil quando `err` não é um erro conhecido.
func traduzirErroEscritaCategoria(err error) error {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return nil
	}
	switch pqErr.Code {
	case pqUniqueViolation:
		switch pqErr.Constraint {
		case idxCategoriasEmpresaCodigo:
			return &ErrCategoriaDuplicada{Campo: "código"}
		case idxCategoriasEmpresaNome:
			return &ErrCategoriaDuplicada{Campo: "nome"}
		}
		// Outra violação de unicidade não é "duplicado" de código/nome:
		// segue como erro inesperado (500), nunca rotulada como nome repetido.
		return nil
	case pqStringDataRightTruncation, pqCheckViolation, pqInvalidByteSequence:
		return ErrCategoriaValidacao
	case pqInvalidTextRepresentation:
		return ErrCategoriaNaoEncontrada
	}
	return nil
}

// CriarCategoria insere uma Categoria na Empresa `empresaID`. Unicidade por
// Empresa vem dos índices únicos (sem SELECT prévio, sem janela de corrida).
func CriarCategoria(db *sql.DB, empresaID, codigo, nome string) (Categoria, error) {
	codigo, nome, err := validarCategoria(codigo, nome)
	if err != nil {
		return Categoria{}, err
	}
	var c Categoria
	const insert = `INSERT INTO categorias (codigo, nome, empresa_id) VALUES ($1, $2, $3) RETURNING id, codigo, nome`
	if err := db.QueryRow(insert, codigo, nome, empresaID).Scan(&c.ID, &c.Codigo, &c.Nome); err != nil {
		if t := traduzirErroEscritaCategoria(err); t != nil {
			return Categoria{}, t
		}
		return Categoria{}, fmt.Errorf("falha ao inserir categoria: %w", err)
	}
	return c, nil
}

// AtualizarCategoria altera `codigo`/`nome` da Categoria `id` DENTRO da
// Empresa `empresaID`. `id` de outra Empresa, inexistente ou não-UUID ->
// ErrCategoriaNaoEncontrada. Editar Categoria em uso é permitido (Produtos
// apontam por `id`).
func AtualizarCategoria(db *sql.DB, empresaID, id, codigo, nome string) (Categoria, error) {
	codigo, nome, err := validarCategoria(codigo, nome)
	if err != nil {
		return Categoria{}, err
	}
	// Byte NUL no id (SQLSTATE 22021) é id malformado -> 404, não 400/500.
	if strings.ContainsRune(id, 0) {
		return Categoria{}, ErrCategoriaNaoEncontrada
	}
	var c Categoria
	const update = `UPDATE categorias SET codigo = $1, nome = $2
		WHERE id = $3 AND empresa_id = $4 RETURNING id, codigo, nome`
	if err := db.QueryRow(update, codigo, nome, id, empresaID).Scan(&c.ID, &c.Codigo, &c.Nome); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Categoria{}, ErrCategoriaNaoEncontrada
		}
		if t := traduzirErroEscritaCategoria(err); t != nil {
			return Categoria{}, t
		}
		return Categoria{}, fmt.Errorf("falha ao atualizar categoria: %w", err)
	}
	return c, nil
}

// ExcluirCategoria remove a Categoria `id` da Empresa `empresaID`, desde que
// nenhum Produto a referencie. Molde de ExcluirEstoque: numa transação, trava
// a linha com FOR UPDATE (um CriarProduto concorrente precisa de KEY SHARE
// sobre ela para a FK e fica bloqueado até o fim desta transação), conta os
// Produtos e só então faz o DELETE. Produto referenciando (inclusive
// soft-deleted: a FK é NOT NULL e sem CASCADE) -> *ErroCategoriaEmUso.
func ExcluirCategoria(db *sql.DB, empresaID, id string) error {
	if strings.ContainsRune(id, 0) {
		return ErrCategoriaNaoEncontrada
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var idTravado string
	if err := tx.QueryRow(
		`SELECT id FROM categorias WHERE id = $1 AND empresa_id = $2 FOR UPDATE`, id, empresaID,
	).Scan(&idTravado); err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) ||
			(errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return ErrCategoriaNaoEncontrada
		}
		return fmt.Errorf("falha ao travar categoria para exclusão: %w", err)
	}

	var emUso int
	if err := tx.QueryRow(`SELECT count(*) FROM produtos WHERE categoria_id = $1`, id).Scan(&emUso); err != nil {
		return fmt.Errorf("falha ao contar produtos da categoria: %w", err)
	}
	if emUso > 0 {
		return &ErroCategoriaEmUso{Produtos: emUso}
	}

	res, err := tx.Exec(`DELETE FROM categorias WHERE id = $1 AND empresa_id = $2`, id, empresaID)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqForeignKeyViolation {
			return &ErroCategoriaEmUso{Produtos: 1}
		}
		return fmt.Errorf("falha ao excluir categoria: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("falha ao confirmar linhas excluídas da categoria: %w", err)
	}
	if n == 0 {
		return ErrCategoriaNaoEncontrada
	}
	if err := tx.Commit(); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqForeignKeyViolation {
			return &ErroCategoriaEmUso{Produtos: 1}
		}
		return fmt.Errorf("falha ao confirmar exclusão da categoria: %w", err)
	}
	return nil
}
