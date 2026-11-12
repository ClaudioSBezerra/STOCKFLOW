// Command corrigir-produtos-sem-template executa a correção ÚNICA dos
// Produtos que nasceram fora de `CriarProduto` e por isso nunca ganharam
// `template_id` — feedback real (Claudio, Ferreira Costa, 2026-11-12): a
// importação de planilha gravava o código do fornecedor direto em
// `produtos.codigo` (nunca o sequencial interno que o cadastro manual
// sempre gera) e nunca aplicava template nenhum.
//
// Dá a cada Produto sem template o template "Genérico" da PRÓPRIA Empresa;
// quando `codigo` já existir e não parecer um sequencial já gerado, move
// esse valor para `codigo_fornecedor` e gera um `codigo` novo via
// proximoCodigoProduto — a MESMA numeração do cadastro novo. Ver o
// comentário de services/migracao_produtos_sem_template.go para o
// detalhamento completo.
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
//     (SHARE ROW EXCLUSIVE) pela duração inteira, bloqueando cadastro/edição/
//     importação concorrente de Produto até terminar.
//  2. ./corrigir-produtos-sem-template
//     # dry-run: relatório, nada escrito
//  3. ./corrigir-produtos-sem-template --executar
//     # aplica; reexecutar é inócuo (0 Produtos — todos já têm template)
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

	executar := flag.Bool("executar", false, "aplica a correção; sem a flag o binário roda em dry-run e não escreve nada")
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
	diag, err := services.DiagnosticarProdutosSemTemplate(db)
	if err != nil {
		return err
	}

	if !executar {
		fmt.Fprintln(out, "dry-run (nada será escrito) — use --executar para aplicar.")
	}
	fmt.Fprintf(out, "produtos sem template (a corrigir): %d\n", diag.ProdutosSemTemplate)

	if diag.ProdutosSemTemplate == 0 {
		fmt.Fprintln(out, "nada a fazer.")
		return nil
	}
	if !executar {
		return nil
	}

	res, err := services.MigrarProdutosSemTemplate(db)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "correção aplicada: %d produto(s) atualizado(s) (template genérico + código interno, código do fornecedor preservado quando aplicável).\n", res.ProdutosAtualizados)
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
