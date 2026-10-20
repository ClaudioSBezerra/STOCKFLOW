// Command atribuir-codigos-produto executa a atribuição ÚNICA de código
// sequencial aos Produtos legados sem código — feedback real (Karla,
// Ferreira Costa, 2026-10-20): o catálogo migrado de uma vez (FR-44, Story
// 9.4) nunca passou pelo cadastro novo (Story 10.2) e por isso nunca ganhou
// `codigo`, dificultando achar um Produto específico em meio a milhares de
// nomes parecidos.
//
// Dá a cada Produto sem código o próximo número sequencial da PRÓPRIA
// Empresa — a MESMA numeração usada pelo cadastro novo (nunca um esquema à
// parte, nunca colide com um cadastro concorrente nem com um código legado
// já existente). Não altera nome, categoria, saldo nem foto — só
// `produtos.codigo`.
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
//  1. Rodar numa janela de uso baixo — a migração trava a tabela `produtos`
//     (SHARE ROW EXCLUSIVE) pela duração inteira, bloqueando cadastro/edição
//     concorrente de Produto até terminar.
//  2. ./atribuir-codigos-produto
//     # dry-run: relatório, nada escrito
//  3. ./atribuir-codigos-produto --executar
//     # aplica; reexecutar é inócuo (0 Produtos — todos já têm código)
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"stockflow/backend/services"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	executar := flag.Bool("executar", false, "aplica a atribuição; sem a flag o binário roda em dry-run e não escreve nada")
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

	// Ecoa QUAL banco será alterado: `--executar` grava em produto de verdade,
	// e um DATABASE_URL errado apontaria pro banco errado.
	fmt.Fprintf(os.Stdout, "banco: %s\n", descricaoBanco(databaseURL))

	if err := executarMigracao(db, os.Stdout, *executar); err != nil {
		fmt.Fprintf(os.Stderr, "erro: %s\n", strings.TrimSpace(err.Error()))
		os.Exit(1)
	}
}

// executarMigracao é o ponto testável do binário: recebe `out` injetado e
// nunca chama os.Exit.
func executarMigracao(db *sql.DB, out io.Writer, executar bool) error {
	diag, err := services.DiagnosticarCodigosLegado(db)
	if err != nil {
		return err
	}

	if !executar {
		fmt.Fprintln(out, "dry-run (nada será escrito) — use --executar para aplicar.")
	}
	fmt.Fprintf(out, "produtos sem código (a atribuir): %d\n", diag.ProdutosSemCodigo)

	if diag.ProdutosSemCodigo == 0 {
		fmt.Fprintln(out, "nada a fazer.")
		return nil
	}
	if !executar {
		return nil
	}

	res, err := services.AtribuirCodigosLegado(db)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "atribuição aplicada: %d produto(s) atualizado(s) com código novo.\n", res.ProdutosAtualizados)
	return nil
}

// descricaoBanco devolve "host:porta/nome" de uma DATABASE_URL, sem usuário
// nem senha.
func descricaoBanco(databaseURL string) string {
	u, err := url.Parse(databaseURL)
	if err != nil || u.Host == "" {
		return "(DATABASE_URL não pôde ser interpretada)"
	}
	return u.Host + u.Path
}
