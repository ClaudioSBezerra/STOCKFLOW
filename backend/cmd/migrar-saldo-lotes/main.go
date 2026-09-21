// Command migrar-saldo-lotes executa o corte ÚNICO do saldo legado de
// `produto_estoque` para Lotes — Story 11.2 (Epic 11, FR-47; AD-24, AD-15),
// spec-11-2.
//
// Cada linha de `produto_estoque` com quantidade > 0 vira UM Lote legado
// (`data_validade` NULL — validade desconhecida, nunca inventada) com a mesma
// quantidade e o `empresa_id` do Produto de origem; a linha de origem é
// ZERADA na mesma instrução SQL. O saldo total do Catálogo (view
// `saldo_produto_estoque`) não muda. Não gera Movimentação.
//
// É deliberadamente um binário de CLI, disparado À MÃO por uma pessoa dentro
// do container `api`, e NUNCA por workflow, job, entrypoint, cron ou rota HTTP
// (AD-15, PRD §9): o corte no banco de produção é ato humano.
//
// Sem `--executar` o binário roda em DRY-RUN: relata o que faria e não
// escreve absolutamente nada.
//
// Runbook do operador:
//
//  1. Pré-requisito: a migração multi-Empresa (`migrar-multi-empresa`, Story
//     9.4) já rodou por completo — todo Produto e Estoque com saldo tem
//     `empresa_id` e as Empresas de ambos coincidem. Senão o binário aborta
//     com relatório, sem escrever nada.
//  2. Rodar SÓ com o Épico 11 completo no ar (Stories 11.4/11.5 em
//     particular): Baixa, Transferência e Pedidos ainda leem `produto_estoque`
//     até lá, e o corte a ZERA — antes disso eles não enxergariam o saldo
//     migrado.
//  3. ./migrar-saldo-lotes
//     # dry-run: relatório, nada escrito
//  4. ./migrar-saldo-lotes --executar
//     # aplica; reexecutar é inócuo (0 Lotes novos) e migra apenas saldo que
//     # tenha reaparecido em produto_estoque desde o corte anterior
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"stockflow/backend/services"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	executar := flag.Bool("executar", false, "aplica o corte; sem a flag o binário roda em dry-run e não escreve nada")
	flag.Parse()

	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "erro: argumento inesperado %q — use as flags com os dois hifens\n", flag.Arg(0))
		os.Exit(1)
	}

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("falha ao carregar .env", "error", err)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "erro: DATABASE_URL não definido")
		os.Exit(1)
	}

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: falha ao abrir conexão com o banco: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "erro: banco indisponível: %v\n", err)
		os.Exit(1)
	}

	if err := executarMigracao(db, os.Stdout, *executar); err != nil {
		fmt.Fprintf(os.Stderr, "erro: %s\n", mensagemDeErro(err))
		os.Exit(1)
	}
}

func formatarQuantidade(q float64) string {
	return strconv.FormatFloat(q, 'f', -1, 64)
}

// executarMigracao é o ponto testável do binário: recebe `out` injetado e
// nunca chama os.Exit. A pré-checagem de Empresa roda em dry-run e em
// `--executar` (esta, dentro de MigrarSaldoParaLotes, antes de escrever).
func executarMigracao(db *sql.DB, out io.Writer, executar bool) error {
	diag, err := services.DiagnosticarSaldoLotes(db)
	if err != nil {
		return err
	}

	if !executar {
		fmt.Fprintln(out, "dry-run (nada será escrito) — use --executar para aplicar.")
	}
	fmt.Fprintf(out, "produto_estoque com saldo (a migrar): %d linha(s), quantidade total %s\n", diag.LinhasAMigrar, formatarQuantidade(diag.QuantidadeTotal))
	fmt.Fprintf(out, "produto_estoque zerada (pulada, sem Lote): %d linha(s)\n", diag.LinhasZeradasPuladas)
	fmt.Fprintf(out, "lotes já existentes: %d\n", diag.LotesExistentes)

	if len(diag.Problemas) > 0 {
		fmt.Fprintf(out, "problemas: %d linha(s) sem Empresa migrável\n", len(diag.Problemas))
		for _, p := range diag.Problemas {
			fmt.Fprintf(out, "  produto=%s estoque=%s quantidade=%s: %s\n", p.ProdutoID, p.EstoqueID, formatarQuantidade(p.Quantidade), p.MotivoDivergente)
		}
		return &services.ErroSaldoNaoMigravel{Linhas: diag.Problemas}
	}
	fmt.Fprintln(out, "problemas: nenhum")

	if !executar {
		return nil
	}

	res, err := services.MigrarSaldoParaLotes(db)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "migração aplicada: %d Lote(s) legado(s) criado(s), quantidade %s; produto_estoque de origem zerada.\n", res.Migrados, formatarQuantidade(res.Quantidade))
	return nil
}

// mensagemDeErro traduz os erros conhecidos para o operador.
func mensagemDeErro(err error) string {
	var naoMigravel *services.ErroSaldoNaoMigravel
	if errors.As(err, &naoMigravel) {
		return fmt.Sprintf("%d linha(s) de produto_estoque com saldo não podem ser migradas — confira a migração multi-Empresa (9.4) e corrija o `empresa_id` de Produto/Estoque; nada foi escrito", len(naoMigravel.Linhas))
	}
	return strings.TrimSpace(err.Error())
}
