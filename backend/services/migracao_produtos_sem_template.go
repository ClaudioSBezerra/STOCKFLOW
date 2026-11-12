// Package services, arquivo migracao_produtos_sem_template.go: correção
// retroativa ÚNICA dos Produtos que nasceram fora de `CriarProduto` e por
// isso nunca ganharam `template_id` — até 2026-11-12 isso cobria dois
// caminhos: a importação de planilha (services/importacoes.go, Story
// 3.3/3.4, corrigida nesta mesma data — ver o comentário no topo daquele
// arquivo) e os Produtos fixos de exemplo do Ambiente de Treinamento. Nos
// dois, `codigo` (quando presente) muitas vezes era literalmente o valor
// externo/legado da planilha, nunca o sequencial interno que todo Produto
// cadastrado manualmente recebe.
//
// Dá a cada Produto sem template, numa única passada:
//   - `template_id` = o template "Genérico" (`[NOME LIVRE]`) da PRÓPRIA
//     Empresa (buscarTemplateGenericoID, importacoes.go) — igual ao que
//     `CriarProduto` já exige desde a Story 10.1.
//   - Se `codigo` já existir e NÃO parecer um sequencial já gerado
//     (`^[0-9]{1,9}$`, mesmo padrão de proximoCodigoProduto) e
//     `codigo_fornecedor` ainda estiver vazio: move esse valor para
//     `codigo_fornecedor` — nunca sobrescreve um `codigo_fornecedor` que já
//     exista, defesa contra um Produto que por algum motivo já tenha os dois
//     preenchidos.
//   - Sempre que `codigo` ficar vazio (já era NULL, ou acabou de ser movido
//     para `codigo_fornecedor` no passo acima): gera um sequencial novo via
//     `proximoCodigoProduto` — MESMA função usada por `CriarProduto`.
//   - Um `codigo` que já pareça sequencial (ex. um Produto que por algum
//     motivo já tenha passado por uma correção anterior) NUNCA é tocado nem
//     movido — só o `template_id` é preenchido nesse caso.
//
// Disparada À MÃO por uma pessoa via `cmd/corrigir-produtos-sem-template`
// (AD-15, PRD §9), nunca por rota, cron, job ou entrypoint. Não altera nome,
// categoria, saldo, fotos nem nenhum outro campo do Produto.
package services

import (
	"database/sql"
	"fmt"
	"regexp"
)

// codigoPareceSequencial reconhece o formato que proximoCodigoProduto gera
// (zero-padding de 1 a 9 dígitos, mesmo limite usado lá para reconhecer um
// código legado puramente numérico) — um `codigo` nesse formato nunca é
// tratado como "valor externo a mover para codigo_fornecedor".
var codigoPareceSequencial = regexp.MustCompile(`^[0-9]{1,9}$`)

// DiagnosticoProdutosSemTemplate é o relatório do dry-run.
type DiagnosticoProdutosSemTemplate struct {
	// ProdutosSemTemplate conta Produtos ativos OU inativados (nunca
	// mesclados — `deleted_at` não interessa mais a template nenhum) sem
	// `template_id`.
	ProdutosSemTemplate int
}

// ResultadoMigracaoProdutosSemTemplate é o resultado de
// MigrarProdutosSemTemplate.
type ResultadoMigracaoProdutosSemTemplate struct {
	ProdutosAtualizados int
}

const consultaProdutosSemTemplateSQL = `SELECT count(*) FROM produtos WHERE template_id IS NULL AND deleted_at IS NULL`

// DiagnosticarProdutosSemTemplate só lê: nada é escrito.
func DiagnosticarProdutosSemTemplate(db *sql.DB) (DiagnosticoProdutosSemTemplate, error) {
	var d DiagnosticoProdutosSemTemplate
	if err := db.QueryRow(consultaProdutosSemTemplateSQL).Scan(&d.ProdutosSemTemplate); err != nil {
		return d, fmt.Errorf("falha ao contar produtos sem template: %w", err)
	}
	return d, nil
}

// MigrarProdutosSemTemplate aplica a correção descrita no comentário do
// arquivo a cada Produto sem `template_id` (ativo ou inativado; nunca um
// Produto mesclado/`deleted_at`), processados por Empresa, do mais antigo
// (`criado_em`) para o mais novo, numa ÚNICA transação. Qualquer falha ->
// rollback completo, nada gravado. Idempotente: reexecutar atualiza 0
// Produtos (todos já têm template).
func MigrarProdutosSemTemplate(db *sql.DB) (ResultadoMigracaoProdutosSemTemplate, error) {
	tx, err := db.Begin()
	if err != nil {
		return ResultadoMigracaoProdutosSemTemplate{}, fmt.Errorf("falha ao iniciar transação de correção de templates: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Impede um cadastro/importação concorrente de criar um Produto sem
	// template (ou de avançar o contador de código) entre o diagnóstico e os
	// UPDATEs abaixo.
	if _, err := tx.Exec(`LOCK TABLE produtos IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return ResultadoMigracaoProdutosSemTemplate{}, fmt.Errorf("falha ao travar produtos para correção de templates: %w", err)
	}

	rows, err := tx.Query(`
		SELECT id, empresa_id, codigo, codigo_fornecedor FROM produtos
		WHERE template_id IS NULL AND deleted_at IS NULL
		ORDER BY empresa_id, criado_em ASC, id ASC`)
	if err != nil {
		return ResultadoMigracaoProdutosSemTemplate{}, fmt.Errorf("falha ao listar produtos sem template: %w", err)
	}
	type alvoSemTemplate struct {
		id, empresaID    string
		codigo           sql.NullString
		codigoFornecedor sql.NullString
	}
	var alvos []alvoSemTemplate
	for rows.Next() {
		var a alvoSemTemplate
		if err := rows.Scan(&a.id, &a.empresaID, &a.codigo, &a.codigoFornecedor); err != nil {
			rows.Close()
			return ResultadoMigracaoProdutosSemTemplate{}, fmt.Errorf("falha ao ler produto sem template: %w", err)
		}
		alvos = append(alvos, a)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ResultadoMigracaoProdutosSemTemplate{}, fmt.Errorf("falha ao iterar produtos sem template: %w", err)
	}
	rows.Close()

	for _, a := range alvos {
		templateID, err := buscarTemplateGenericoID(tx, a.empresaID)
		if err != nil {
			return ResultadoMigracaoProdutosSemTemplate{}, err
		}

		novoCodigo := a.codigo
		novoCodigoFornecedor := a.codigoFornecedor
		if novoCodigo.Valid && !codigoPareceSequencial.MatchString(novoCodigo.String) {
			// Valor externo/legado, não um sequencial interno válido — nunca
			// fica como `codigo` final. Só move para `codigo_fornecedor`
			// quando esse campo ainda está vazio (defesa contra um Produto
			// que por algum motivo já tenha os dois preenchidos); do
			// contrário o valor antigo é descartado ao gerar o novo
			// sequencial abaixo — caso raro, nunca observado nos dados reais
			// que motivaram esta correção.
			if !novoCodigoFornecedor.Valid {
				novoCodigoFornecedor = novoCodigo
			}
			novoCodigo = sql.NullString{}
		}
		if !novoCodigo.Valid {
			gerado, err := proximoCodigoProduto(tx, a.empresaID)
			if err != nil {
				return ResultadoMigracaoProdutosSemTemplate{}, err
			}
			novoCodigo = sql.NullString{String: gerado, Valid: true}
		}

		const atualizar = `
			UPDATE produtos SET template_id = $1, codigo = $2, codigo_fornecedor = $3
			WHERE id = $4`
		if _, err := tx.Exec(atualizar, templateID, novoCodigo, novoCodigoFornecedor, a.id); err != nil {
			return ResultadoMigracaoProdutosSemTemplate{}, fmt.Errorf("falha ao corrigir produto %s: %w", a.id, err)
		}
	}

	var restantes int
	if err := tx.QueryRow(consultaProdutosSemTemplateSQL).Scan(&restantes); err != nil {
		return ResultadoMigracaoProdutosSemTemplate{}, fmt.Errorf("falha ao reconferir produtos sem template: %w", err)
	}
	if restantes != 0 {
		return ResultadoMigracaoProdutosSemTemplate{}, fmt.Errorf("conferência da correção de templates divergiu (%d produto(s) ainda sem template) — nada foi gravado", restantes)
	}

	if err := tx.Commit(); err != nil {
		return ResultadoMigracaoProdutosSemTemplate{}, fmt.Errorf("falha ao confirmar correção de templates: %w", err)
	}
	return ResultadoMigracaoProdutosSemTemplate{ProdutosAtualizados: len(alvos)}, nil
}
