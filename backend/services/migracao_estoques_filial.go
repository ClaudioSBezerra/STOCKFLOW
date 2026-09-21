// Package services, arquivo migracao_estoques_filial.go: a migração ÚNICA e
// operacional dos Estoques legados para a Filial padrão da própria Empresa —
// Story 12.2 (Epic 12, FR-51; AD-27, AD-15), spec-12-2.
//
// A Story 12.1 criou `filiais` e `estoques.filial_id` NULLABLE, mas todo
// Estoque anterior continua sem Filial e Empresas anteriores à 12.1 podem nem
// ter uma. Esta migração, numa única transação: (1) cria a Filial padrão (nome
// = Nome Fantasia) de toda Empresa sem nenhuma Filial; (2) vincula todo
// Estoque com `filial_id IS NULL` à Filial padrão da PRÓPRIA Empresa (a mais
// antiga — a mesma regra de `filialPadraoDaEmpresa`); (3) só então aplica
// `ALTER COLUMN filial_id SET NOT NULL`.
//
// O `SET NOT NULL` vive aqui e não numa migration SQL de propósito: o deploy
// da 12.1 já aplicou as migrations com Estoques NULL em produção, e uma
// migration `NOT NULL` derrubaria o `api` no boot antes do backfill.
//
// Disparada À MÃO por uma pessoa via `cmd/migrar-estoques-filial` (AD-15, PRD
// §9), nunca por rota, cron, job ou entrypoint. Não gera Movimentação, não
// altera dados fora de `estoques.filial_id`/`filiais` e não usa
// `migracao_id_map`.
package services

import (
	"database/sql"
	"fmt"
	"strings"
)

// consultaEstoquesFilial é o subconjunto comum de *sql.DB e *sql.Tx usado pelo
// diagnóstico, que roda tanto fora quanto dentro da transação da migração.
type consultaEstoquesFilial interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

const (
	motivoEstoqueSemEmpresa    = "sem empresa_id"
	motivoEstoqueNomeDuplicado = "nome duplicado na Filial padrão"
)

// EstoqueNaoMigravel descreve um Estoque legado que não pode ser vinculado à
// Filial padrão da sua Empresa.
type EstoqueNaoMigravel struct {
	EstoqueID string
	Nome      string
	EmpresaID string // "" quando NULL
	Motivo    string
}

// ErroEstoqueNaoMigravel indica Estoque legado (`filial_id` NULL) sem
// `empresa_id` (a migração 9.4 não rodou) ou cujo `nome_normalizado` colide,
// na Filial padrão da Empresa, com outro Estoque. NADA é escrito quando este
// erro é devolvido.
type ErroEstoqueNaoMigravel struct {
	Estoques []EstoqueNaoMigravel
}

func (e *ErroEstoqueNaoMigravel) Error() string {
	const maxExibidos = 10
	partes := make([]string, 0, maxExibidos)
	for i, s := range e.Estoques {
		if i == maxExibidos {
			break
		}
		partes = append(partes, fmt.Sprintf("estoque=%s (%q): %s", s.EstoqueID, s.Nome, s.Motivo))
	}
	msg := fmt.Sprintf("%d estoque(s) legado(s) não podem ser vinculados à Filial padrão (sem `empresa_id` ou nome duplicado na Filial padrão — confira a migração multi-Empresa 9.4 e corrija os dados): %s",
		len(e.Estoques), strings.Join(partes, "; "))
	if len(e.Estoques) > maxExibidos {
		msg += fmt.Sprintf("; ... e mais %d", len(e.Estoques)-maxExibidos)
	}
	return msg
}

// DiagnosticoEstoquesFilial é o relatório do dry-run (e da pré-checagem).
type DiagnosticoEstoquesFilial struct {
	EmpresasSemFilial    int  // Empresas sem nenhuma Filial (Filial padrão a criar)
	EstoquesAVincular    int  // Estoques com `filial_id IS NULL`
	EstoquesJaVinculados int  // Estoques com `filial_id` preenchido
	FilialIDNotNull      bool // `estoques.filial_id` já é NOT NULL
	Problemas            []EstoqueNaoMigravel
}

// ResultadoMigracaoEstoquesFilial é o resultado de MigrarEstoquesParaFilial.
type ResultadoMigracaoEstoquesFilial struct {
	FiliaisCriadas     int // Filiais padrão criadas
	EstoquesVinculados int // Estoques legados vinculados
}

// DiagnosticarEstoquesFilial só lê: nada é escrito.
func DiagnosticarEstoquesFilial(db *sql.DB) (DiagnosticoEstoquesFilial, error) {
	return diagnosticarEstoquesFilial(db)
}

// problemasEstoquesFilialSQL lista os Estoques legados não migráveis: sem
// `empresa_id`, ou cujo nome normalizado colidiria na Filial padrão da Empresa
// com outro Estoque (já vinculado a ela ou outro legado da mesma Empresa).
// Empresa ainda sem Filial não tem Estoque vinculado: só colide entre legados.
const problemasEstoquesFilialSQL = `
SELECT id::text, nome, COALESCE(empresa_id::text, ''), motivo FROM (
	SELECT e.id, e.nome, e.empresa_id, $1::text AS motivo, 0 AS ordem
	FROM estoques e
	WHERE e.filial_id IS NULL AND e.empresa_id IS NULL
	UNION ALL
	SELECT l.id, l.nome, l.empresa_id, $2::text AS motivo, 1 AS ordem
	FROM (
		SELECT e.id, e.nome, e.nome_normalizado, e.empresa_id,
		       (SELECT f.id FROM filiais f WHERE f.empresa_id = e.empresa_id ORDER BY f.criado_em, f.id LIMIT 1) AS filial_padrao
		FROM estoques e
		WHERE e.filial_id IS NULL AND e.empresa_id IS NOT NULL
	) l
	WHERE EXISTS (
		SELECT 1 FROM estoques o
		WHERE o.id <> l.id AND o.nome_normalizado = l.nome_normalizado
		  AND ((l.filial_padrao IS NOT NULL AND o.filial_id = l.filial_padrao)
		    OR (o.filial_id IS NULL AND o.empresa_id = l.empresa_id))
	)
) p
ORDER BY ordem, nome, id`

func diagnosticarEstoquesFilial(q consultaEstoquesFilial) (DiagnosticoEstoquesFilial, error) {
	var d DiagnosticoEstoquesFilial
	if err := q.QueryRow(`SELECT count(*) FROM empresas e WHERE NOT EXISTS (SELECT 1 FROM filiais f WHERE f.empresa_id = e.id)`).Scan(&d.EmpresasSemFilial); err != nil {
		return d, fmt.Errorf("falha ao contar empresas sem filial: %w", err)
	}
	if err := q.QueryRow(`
		SELECT count(*) FILTER (WHERE filial_id IS NULL), count(*) FILTER (WHERE filial_id IS NOT NULL)
		FROM estoques`).Scan(&d.EstoquesAVincular, &d.EstoquesJaVinculados); err != nil {
		return d, fmt.Errorf("falha ao contar estoques: %w", err)
	}
	var nullable string
	if err := q.QueryRow(`
		SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'estoques' AND column_name = 'filial_id'`).Scan(&nullable); err != nil {
		return d, fmt.Errorf("falha ao ler a nulabilidade de estoques.filial_id: %w", err)
	}
	d.FilialIDNotNull = nullable == "NO"

	rows, err := q.Query(problemasEstoquesFilialSQL, motivoEstoqueSemEmpresa, motivoEstoqueNomeDuplicado)
	if err != nil {
		return d, fmt.Errorf("falha ao checar estoques não migráveis: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p EstoqueNaoMigravel
		if err := rows.Scan(&p.EstoqueID, &p.Nome, &p.EmpresaID, &p.Motivo); err != nil {
			return d, fmt.Errorf("falha ao ler estoque não migrável: %w", err)
		}
		d.Problemas = append(d.Problemas, p)
	}
	if err := rows.Err(); err != nil {
		return d, fmt.Errorf("falha ao iterar estoques não migráveis: %w", err)
	}
	return d, nil
}

// MigrarEstoquesParaFilial cria a Filial padrão das Empresas sem Filial,
// vincula todo Estoque legado à Filial padrão da própria Empresa e aplica
// `SET NOT NULL` em `estoques.filial_id`, tudo numa única transação. Qualquer
// divergência -> rollback + erro, nada gravado. Idempotente: Estoque já
// vinculado nunca é tocado; a 2ª execução cria 0 Filiais, vincula 0 Estoques e
// o `SET NOT NULL` é inócuo.
func MigrarEstoquesParaFilial(db *sql.DB) (ResultadoMigracaoEstoquesFilial, error) {
	var res ResultadoMigracaoEstoquesFilial

	tx, err := db.Begin()
	if err != nil {
		return res, fmt.Errorf("falha ao iniciar transação da migração de estoques: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// O ALTER TABLE pede lock exclusivo em `estoques`: sem teto de espera, uma
	// transação longa deixaria o operador pendurado segurando os locks já
	// tomados.
	if _, err := tx.Exec(`SET LOCAL lock_timeout = '30s'`); err != nil {
		return res, fmt.Errorf("falha ao definir lock_timeout da migração de estoques: %w", err)
	}

	// SHARE ROW EXCLUSIVE bloqueia escrita concorrente em `estoques` (um Estoque
	// novo/legado surgindo entre o diagnóstico e o UPDATE) e serializa duas
	// execuções simultâneas do binário; leituras seguem liberadas.
	if _, err := tx.Exec(`LOCK TABLE estoques IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return res, fmt.Errorf("falha ao travar estoques para a migração: %w", err)
	}

	diag, err := diagnosticarEstoquesFilial(tx)
	if err != nil {
		return res, err
	}
	if len(diag.Problemas) > 0 {
		return res, &ErroEstoqueNaoMigravel{Estoques: diag.Problemas}
	}

	criadas, err := tx.Exec(`
		INSERT INTO filiais (empresa_id, nome)
		SELECT e.id, e.nome_fantasia FROM empresas e
		WHERE NOT EXISTS (SELECT 1 FROM filiais f WHERE f.empresa_id = e.id)`)
	if err != nil {
		return ResultadoMigracaoEstoquesFilial{}, fmt.Errorf("falha ao criar filiais padrão: %w", err)
	}
	nCriadas, err := criadas.RowsAffected()
	if err != nil {
		return ResultadoMigracaoEstoquesFilial{}, fmt.Errorf("falha ao contar filiais criadas: %w", err)
	}

	vinculados, err := tx.Exec(`
		UPDATE estoques e
		SET filial_id = (SELECT f.id FROM filiais f WHERE f.empresa_id = e.empresa_id ORDER BY f.criado_em, f.id LIMIT 1)
		WHERE e.filial_id IS NULL AND e.empresa_id IS NOT NULL`)
	if err != nil {
		return ResultadoMigracaoEstoquesFilial{}, fmt.Errorf("falha ao vincular estoques à filial padrão: %w", err)
	}
	nVinculados, err := vinculados.RowsAffected()
	if err != nil {
		return ResultadoMigracaoEstoquesFilial{}, fmt.Errorf("falha ao contar estoques vinculados: %w", err)
	}
	if int(nVinculados) != diag.EstoquesAVincular {
		return ResultadoMigracaoEstoquesFilial{}, fmt.Errorf("conferência da migração de estoques divergiu (%d vinculados x %d a vincular) — nada foi gravado", nVinculados, diag.EstoquesAVincular)
	}

	var restantes int
	if err := tx.QueryRow(`SELECT count(*) FROM estoques WHERE filial_id IS NULL`).Scan(&restantes); err != nil {
		return ResultadoMigracaoEstoquesFilial{}, fmt.Errorf("falha ao reconferir estoques sem filial: %w", err)
	}
	if restantes != 0 {
		return ResultadoMigracaoEstoquesFilial{}, fmt.Errorf("conferência da migração de estoques divergiu (%d estoque(s) ainda sem filial) — nada foi gravado", restantes)
	}

	// Reexecução: a coluna já é NOT NULL — evita o lock exclusivo e o scan.
	if !diag.FilialIDNotNull {
		if _, err := tx.Exec(`ALTER TABLE estoques ALTER COLUMN filial_id SET NOT NULL`); err != nil {
			return ResultadoMigracaoEstoquesFilial{}, fmt.Errorf("falha ao aplicar NOT NULL em estoques.filial_id: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return ResultadoMigracaoEstoquesFilial{}, fmt.Errorf("falha ao confirmar a migração de estoques: %w", err)
	}
	res.FiliaisCriadas = int(nCriadas)
	res.EstoquesVinculados = int(nVinculados)
	return res, nil
}
