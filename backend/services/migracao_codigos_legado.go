// Package services, arquivo migracao_codigos_legado.go: atribuição ÚNICA de
// código sequencial aos Produtos legados sem código — feedback real (Karla,
// Ferreira Costa, 2026-10-20): o catálogo real inteiro (2.745 Produtos, corte
// único de FR-44/Story 9.4) nunca passou pelo cadastro novo (Story 10.2,
// AD-26) e por isso nunca ganhou `codigo` — dificulta achar um Produto
// específico em meio a milhares de nomes parecidos.
//
// Reusa a MESMA numeração sequencial por Empresa do cadastro normal
// (proximoCodigoProduto, produtos.go) — nunca inventa um esquema à parte —
// então um código atribuído aqui nunca colide com um cadastro novo
// concorrente nem com um código legado que já exista (planilha importada,
// Story 3.3/3.4).
//
// Disparada À MÃO por uma pessoa via `cmd/atribuir-codigos-produto` (AD-15,
// PRD §9), nunca por rota, cron, job ou entrypoint. Não altera nenhum outro
// dado do Produto (nome, categoria, saldo, fotos) — só `produtos.codigo`.
package services

import (
	"database/sql"
	"fmt"
)

// DiagnosticoCodigosLegado é o relatório do dry-run.
type DiagnosticoCodigosLegado struct {
	// ProdutosSemCodigo conta Produtos ativos OU inativados (nunca mesclados
	// — `deleted_at` não interessa mais a código nenhum) sem `codigo`.
	ProdutosSemCodigo int
}

// ResultadoAtribuicaoCodigosLegado é o resultado de AtribuirCodigosLegado.
type ResultadoAtribuicaoCodigosLegado struct {
	ProdutosAtualizados int
}

const consultaProdutosSemCodigoSQL = `SELECT count(*) FROM produtos WHERE codigo IS NULL AND deleted_at IS NULL`

// DiagnosticarCodigosLegado só lê: nada é escrito.
func DiagnosticarCodigosLegado(db *sql.DB) (DiagnosticoCodigosLegado, error) {
	var d DiagnosticoCodigosLegado
	if err := db.QueryRow(consultaProdutosSemCodigoSQL).Scan(&d.ProdutosSemCodigo); err != nil {
		return d, fmt.Errorf("falha ao contar produtos sem código: %w", err)
	}
	return d, nil
}

// AtribuirCodigosLegado dá, a cada Produto sem `codigo` (ativo ou inativado;
// nunca um Produto mesclado/`deleted_at`), o próximo código sequencial da
// PRÓPRIA Empresa (proximoCodigoProduto) — processados por Empresa, do mais
// antigo (`criado_em`) para o mais novo, numa ÚNICA transação. Qualquer falha
// -> rollback completo, nada gravado. Idempotente: reexecutar atualiza 0
// Produtos (todos já têm código).
func AtribuirCodigosLegado(db *sql.DB) (ResultadoAtribuicaoCodigosLegado, error) {
	tx, err := db.Begin()
	if err != nil {
		return ResultadoAtribuicaoCodigosLegado{}, fmt.Errorf("falha ao iniciar transação de atribuição de códigos: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Impede um cadastro novo concorrente de gravar um Produto sem código
	// (ou de avançar o contador) entre o diagnóstico e os UPDATEs abaixo.
	if _, err := tx.Exec(`LOCK TABLE produtos IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return ResultadoAtribuicaoCodigosLegado{}, fmt.Errorf("falha ao travar produtos para atribuição de códigos: %w", err)
	}

	rows, err := tx.Query(`
		SELECT id, empresa_id FROM produtos
		WHERE codigo IS NULL AND deleted_at IS NULL
		ORDER BY empresa_id, criado_em ASC, id ASC`)
	if err != nil {
		return ResultadoAtribuicaoCodigosLegado{}, fmt.Errorf("falha ao listar produtos sem código: %w", err)
	}
	type alvoCodigoLegado struct{ id, empresaID string }
	var alvos []alvoCodigoLegado
	for rows.Next() {
		var a alvoCodigoLegado
		if err := rows.Scan(&a.id, &a.empresaID); err != nil {
			rows.Close()
			return ResultadoAtribuicaoCodigosLegado{}, fmt.Errorf("falha ao ler produto sem código: %w", err)
		}
		alvos = append(alvos, a)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ResultadoAtribuicaoCodigosLegado{}, fmt.Errorf("falha ao iterar produtos sem código: %w", err)
	}
	rows.Close()

	for _, a := range alvos {
		codigo, err := proximoCodigoProduto(tx, a.empresaID)
		if err != nil {
			return ResultadoAtribuicaoCodigosLegado{}, err
		}
		if _, err := tx.Exec(`UPDATE produtos SET codigo = $1 WHERE id = $2`, codigo, a.id); err != nil {
			return ResultadoAtribuicaoCodigosLegado{}, fmt.Errorf("falha ao gravar código do produto %s: %w", a.id, err)
		}
	}

	var restantes int
	if err := tx.QueryRow(consultaProdutosSemCodigoSQL).Scan(&restantes); err != nil {
		return ResultadoAtribuicaoCodigosLegado{}, fmt.Errorf("falha ao reconferir produtos sem código: %w", err)
	}
	if restantes != 0 {
		return ResultadoAtribuicaoCodigosLegado{}, fmt.Errorf("conferência da atribuição de códigos divergiu (%d produto(s) ainda sem código) — nada foi gravado", restantes)
	}

	if err := tx.Commit(); err != nil {
		return ResultadoAtribuicaoCodigosLegado{}, fmt.Errorf("falha ao confirmar atribuição de códigos: %w", err)
	}
	return ResultadoAtribuicaoCodigosLegado{ProdutosAtualizados: len(alvos)}, nil
}
