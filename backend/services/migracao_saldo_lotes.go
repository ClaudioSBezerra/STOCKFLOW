// Package services, arquivo migracao_saldo_lotes.go: o corte ÚNICO e
// operacional do saldo legado de `produto_estoque` para Lotes — Story 11.2
// (Epic 11, FR-47; AD-24, AD-15), spec-11-2.
//
// Até a Story 11.1 todo saldo vivia em `produto_estoque` (quantidade única por
// par Produto/Estoque, sem Lote). A view `saldo_produto_estoque` soma
// `produto_estoque` + `lotes`, mas o saldo antigo nunca virava Lote. Este
// corte converte cada linha com saldo em UM Lote legado (`data_validade`
// NULL — validade desconhecida, nunca inventada) e ZERA a linha de origem na
// MESMA instrução SQL: o zero é a marca de progresso (idempotência) e evita a
// dupla contagem na view. O saldo total do Catálogo é idêntico antes e depois.
//
// Disparado À MÃO por uma pessoa via `cmd/migrar-saldo-lotes` (AD-15, PRD §9),
// nunca por rota, cron, job ou entrypoint.
//
// Não gera Movimentação (o saldo total não muda) e não usa
// `migracao_id_map`.
package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// consultaSaldoLotes é o subconjunto comum de *sql.DB e *sql.Tx usado pelo
// diagnóstico, que roda tanto fora quanto dentro da transação do corte.
type consultaSaldoLotes interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// LinhaSaldoNaoMigravel descreve uma linha de `produto_estoque` com saldo
// que não pode virar Lote por falta/divergência de Empresa.
type LinhaSaldoNaoMigravel struct {
	ProdutoID        string
	EstoqueID        string
	Quantidade       float64
	EmpresaProduto   string // "" quando NULL
	EmpresaEstoque   string // "" quando NULL
	MotivoDivergente string
}

// ErroSaldoNaoMigravel indica que há linha de `produto_estoque` com saldo
// `> 0` cujo Produto ou Estoque não tem `empresa_id` (a migração 9.4 não
// rodou ou está incompleta), ou cujas Empresas de Produto e Estoque diferem.
// NADA é escrito quando este erro é devolvido.
type ErroSaldoNaoMigravel struct {
	Linhas []LinhaSaldoNaoMigravel
}

func (e *ErroSaldoNaoMigravel) Error() string {
	const maxExibidas = 10
	partes := make([]string, 0, maxExibidas)
	for i, l := range e.Linhas {
		if i == maxExibidas {
			break
		}
		partes = append(partes, fmt.Sprintf("produto=%s estoque=%s: %s", l.ProdutoID, l.EstoqueID, l.MotivoDivergente))
	}
	msg := fmt.Sprintf("%d linha(s) de produto_estoque com saldo não podem ser migradas (Produto/Estoque sem `empresa_id` ou de Empresas diferentes — confira a migração multi-Empresa 9.4 e corrija os dados): %s",
		len(e.Linhas), strings.Join(partes, "; "))
	if len(e.Linhas) > maxExibidas {
		msg += fmt.Sprintf("; ... e mais %d", len(e.Linhas)-maxExibidas)
	}
	return msg
}

// DiagnosticoSaldoLotes é o relatório do dry-run (e da pré-checagem).
type DiagnosticoSaldoLotes struct {
	LinhasAMigrar        int     // produto_estoque com quantidade > 0
	LinhasZeradasPuladas int     // produto_estoque com quantidade = 0 (não geram Lote)
	QuantidadeTotal      float64 // soma da quantidade das linhas a migrar
	LotesExistentes      int     // linhas já em `lotes`
	Problemas            []LinhaSaldoNaoMigravel
}

// ResultadoMigracaoSaldoLotes é o resultado de MigrarSaldoParaLotes.
type ResultadoMigracaoSaldoLotes struct {
	Migrados   int     // Lotes criados (= linhas de origem zeradas)
	Quantidade float64 // soma da quantidade migrada
}

// DiagnosticarSaldoLotes só lê: nada é escrito.
func DiagnosticarSaldoLotes(db *sql.DB) (DiagnosticoSaldoLotes, error) {
	return diagnosticarSaldoLotes(db)
}

func diagnosticarSaldoLotes(q consultaSaldoLotes) (DiagnosticoSaldoLotes, error) {
	var d DiagnosticoSaldoLotes
	if err := q.QueryRow(`
		SELECT
			count(*) FILTER (WHERE quantidade > 0),
			count(*) FILTER (WHERE quantidade = 0),
			COALESCE(SUM(quantidade) FILTER (WHERE quantidade > 0), 0)::float8
		FROM produto_estoque`).Scan(&d.LinhasAMigrar, &d.LinhasZeradasPuladas, &d.QuantidadeTotal); err != nil {
		return d, fmt.Errorf("falha ao contar saldo de produto_estoque: %w", err)
	}
	if err := q.QueryRow(`SELECT count(*) FROM lotes`).Scan(&d.LotesExistentes); err != nil {
		return d, fmt.Errorf("falha ao contar lotes: %w", err)
	}

	rows, err := q.Query(`
		SELECT pe.produto_id::text, pe.estoque_id::text, pe.quantidade::float8,
		       COALESCE(p.empresa_id::text, ''), COALESCE(e.empresa_id::text, '')
		FROM produto_estoque pe
		JOIN produtos p ON p.id = pe.produto_id
		JOIN estoques e ON e.id = pe.estoque_id
		WHERE pe.quantidade > 0
		  AND (p.empresa_id IS NULL OR e.empresa_id IS NULL OR p.empresa_id <> e.empresa_id)
		ORDER BY pe.produto_id, pe.estoque_id`)
	if err != nil {
		return d, fmt.Errorf("falha ao checar empresa do saldo: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var l LinhaSaldoNaoMigravel
		if err := rows.Scan(&l.ProdutoID, &l.EstoqueID, &l.Quantidade, &l.EmpresaProduto, &l.EmpresaEstoque); err != nil {
			return d, fmt.Errorf("falha ao ler linha não migrável: %w", err)
		}
		switch {
		case l.EmpresaProduto == "" && l.EmpresaEstoque == "":
			l.MotivoDivergente = "Produto e Estoque sem empresa_id"
		case l.EmpresaProduto == "":
			l.MotivoDivergente = "Produto sem empresa_id"
		case l.EmpresaEstoque == "":
			l.MotivoDivergente = "Estoque sem empresa_id"
		default:
			l.MotivoDivergente = "Produto e Estoque de Empresas diferentes"
		}
		d.Problemas = append(d.Problemas, l)
	}
	if err := rows.Err(); err != nil {
		return d, fmt.Errorf("falha ao iterar linhas não migráveis: %w", err)
	}
	return d, nil
}

// migrarSaldoLotesSQL trava as linhas com saldo (`FOR UPDATE OF pe`, o mesmo
// lock de Baixa/Transferência/Pedido), zera a origem e cria o Lote a partir
// do MESMO conjunto travado, numa instrução só. O filtro de Empresa repete o da
// pré-checagem (que roda sem lock): uma linha que ficou inválida entre as duas
// simplesmente não entra em `alvo` e é reportada pela re-checagem pós-escrita.
// `a.quantidade` no RETURNING de
// `zerados` é o valor ANTIGO (lido de `alvo`), não o novo zero.
const migrarSaldoLotesSQL = `
WITH alvo AS (
	SELECT pe.produto_id, pe.estoque_id, pe.quantidade, p.empresa_id
	FROM produto_estoque pe
	JOIN produtos p ON p.id = pe.produto_id
	JOIN estoques e ON e.id = pe.estoque_id
	WHERE pe.quantidade > 0
	  AND p.empresa_id IS NOT NULL AND p.empresa_id = e.empresa_id
	FOR UPDATE OF pe
), zerados AS (
	UPDATE produto_estoque pe SET quantidade = 0
	FROM alvo a
	WHERE pe.produto_id = a.produto_id AND pe.estoque_id = a.estoque_id
	RETURNING a.quantidade AS quantidade
), inseridos AS (
	INSERT INTO lotes (produto_id, estoque_id, quantidade, data_validade, empresa_id)
	SELECT produto_id, estoque_id, quantidade, NULL, empresa_id FROM alvo
	RETURNING quantidade
)
SELECT
	(SELECT count(*) FROM zerados),
	(SELECT count(*) FROM inseridos),
	(SELECT COALESCE(SUM(quantidade), 0) FROM zerados) = (SELECT COALESCE(SUM(quantidade), 0) FROM inseridos),
	(SELECT COALESCE(SUM(quantidade), 0) FROM inseridos)::float8`

// MigrarSaldoParaLotes converte, numa única transação, cada linha de
// `produto_estoque` com `quantidade > 0` em um Lote legado e zera a origem.
// Idempotente: sem linha `> 0` não faz nada (Migrados = 0). Linha zerada não
// gera Lote. A pré-checagem de Empresa roda ANTES de escrever
// (ErroSaldoNaoMigravel); a conferência de soma/contagem roda ANTES do
// commit (divergência -> rollback + erro).
func MigrarSaldoParaLotes(db *sql.DB) (ResultadoMigracaoSaldoLotes, error) {
	var res ResultadoMigracaoSaldoLotes

	tx, err := db.Begin()
	if err != nil {
		return res, fmt.Errorf("falha ao iniciar transação da migração de saldo: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	diag, err := diagnosticarSaldoLotes(tx)
	if err != nil {
		return res, err
	}
	if len(diag.Problemas) > 0 {
		return res, &ErroSaldoNaoMigravel{Linhas: diag.Problemas}
	}
	if diag.LinhasAMigrar == 0 {
		return res, nil
	}

	// O corte trava linhas que Baixa/Transferência/Pedido também travam: sem
	// teto de espera, um lock preso deixaria o operador pendurado segurando os
	// locks já tomados.
	if _, err := tx.Exec(`SET LOCAL lock_timeout = '30s'`); err != nil {
		return res, fmt.Errorf("falha ao definir lock_timeout da migração de saldo: %w", err)
	}

	var zerados, inseridos int
	var somasIguais bool
	if err := tx.QueryRow(migrarSaldoLotesSQL).Scan(&zerados, &inseridos, &somasIguais, &res.Quantidade); err != nil {
		return ResultadoMigracaoSaldoLotes{}, fmt.Errorf("falha ao migrar saldo para lotes: %w", err)
	}
	if zerados != inseridos || !somasIguais {
		return ResultadoMigracaoSaldoLotes{}, errors.New("conferência da migração de saldo divergiu (linhas zeradas x lotes criados) — nada foi gravado")
	}
	// Re-checagem pós-escrita: linha com saldo que ficou de fora de `alvo`
	// (Empresa ausente/divergente surgida depois da pré-checagem) aborta o corte
	// inteiro — nunca sucesso parcial silencioso.
	pos, err := diagnosticarSaldoLotes(tx)
	if err != nil {
		return ResultadoMigracaoSaldoLotes{}, err
	}
	if len(pos.Problemas) > 0 {
		return ResultadoMigracaoSaldoLotes{}, &ErroSaldoNaoMigravel{Linhas: pos.Problemas}
	}
	if err := tx.Commit(); err != nil {
		return ResultadoMigracaoSaldoLotes{}, fmt.Errorf("falha ao confirmar a migração de saldo: %w", err)
	}
	res.Migrados = inseridos
	return res, nil
}
