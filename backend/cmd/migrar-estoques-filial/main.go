// Command migrar-estoques-filial executa a migração ÚNICA dos Estoques legados
// para a Filial padrão da própria Empresa — Story 12.2 (Epic 12, FR-51;
// AD-27, AD-15), spec-12-2.
//
// Numa única transação: cria a Filial padrão (nome = Nome Fantasia) de toda
// Empresa sem nenhuma Filial, vincula todo Estoque com `filial_id IS NULL` à
// Filial padrão (a mais antiga) da própria Empresa e, só então, aplica
// `ALTER TABLE estoques ALTER COLUMN filial_id SET NOT NULL`. O `SET NOT NULL`
// fica aqui, e não numa migration SQL, para não derrubar o deploy antes do
// backfill. Não gera Movimentação e não altera dados fora de
// `estoques.filial_id`/`filiais`.
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
//  1. Pré-requisitos: a migração multi-Empresa (`migrar-multi-empresa`, Story
//     9.4) já rodou por completo — todo Estoque tem `empresa_id` — e o deploy
//     da Story 12.1 (tabela `filiais` e `estoques.filial_id`) já está no ar.
//     Senão o binário aborta com relatório, sem escrever nada.
//  2. Fazer BACKUP do banco antes de aplicar.
//  3. ./migrar-estoques-filial
//     # dry-run: relatório, nada escrito
//  4. ./migrar-estoques-filial --executar
//     # aplica; reexecutar é inócuo (0 Filiais criadas, 0 Estoques vinculados)
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/lib/pq"

	"stockflow/backend/services"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	executar := flag.Bool("executar", false, "aplica a migração; sem a flag o binário roda em dry-run e não escreve nada")
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

	// Ecoa QUAL banco será alterado (sem a senha): `--executar` é irreversível
	// (SET NOT NULL) e um DATABASE_URL errado apontaria para o banco errado.
	fmt.Fprintf(os.Stdout, "banco: %s\n", descricaoBanco(databaseURL))

	if err := executarMigracao(db, os.Stdout, *executar); err != nil {
		fmt.Fprintf(os.Stderr, "erro: %s\n", mensagemDeErro(err))
		os.Exit(1)
	}
}

// executarMigracao é o ponto testável do binário: recebe `out` injetado e
// nunca chama os.Exit. A pré-checagem roda em dry-run e em `--executar` (esta,
// dentro de MigrarEstoquesParaFilial, antes de escrever).
func executarMigracao(db *sql.DB, out io.Writer, executar bool) error {
	diag, err := services.DiagnosticarEstoquesFilial(db)
	if err != nil {
		return err
	}

	if !executar {
		fmt.Fprintln(out, "dry-run (nada será escrito) — use --executar para aplicar.")
	}
	fmt.Fprintf(out, "empresas sem filial (Filial padrão a criar): %d\n", diag.EmpresasSemFilial)
	fmt.Fprintf(out, "estoques a vincular (filial_id nulo): %d\n", diag.EstoquesAVincular)
	fmt.Fprintf(out, "estoques já vinculados: %d\n", diag.EstoquesJaVinculados)
	if diag.FilialIDNotNull {
		fmt.Fprintln(out, "estoques.filial_id: já é NOT NULL")
	} else {
		fmt.Fprintln(out, "estoques.filial_id: ainda é NULLABLE (NOT NULL será aplicado)")
	}

	if diag.EmpresasNomeInvalido > 0 {
		fmt.Fprintf(out, "problemas: %d empresa(s) sem Filial com Nome Fantasia vazio ou acima de 255 caracteres\n", diag.EmpresasNomeInvalido)
		return &services.ErroFilialPadraoSemNome{Empresas: diag.EmpresasNomeInvalido}
	}
	if len(diag.Problemas) > 0 {
		fmt.Fprintf(out, "problemas: %d estoque(s) não migrável(is)\n", len(diag.Problemas))
		for _, p := range diag.Problemas {
			empresa := p.EmpresaID
			if empresa == "" {
				empresa = "-"
			}
			fmt.Fprintf(out, "  estoque=%s nome=%q empresa=%s: %s\n", p.EstoqueID, p.Nome, empresa, p.Motivo)
		}
		return &services.ErroEstoqueNaoMigravel{Estoques: diag.Problemas}
	}
	fmt.Fprintln(out, "problemas: nenhum")

	if !executar {
		return nil
	}

	res, err := services.MigrarEstoquesParaFilial(db)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "migração aplicada: %d Filial(is) padrão criada(s), %d Estoque(s) vinculado(s); estoques.filial_id agora é NOT NULL.\n", res.FiliaisCriadas, res.EstoquesVinculados)
	return nil
}

// descricaoBanco devolve "host:porta/nome" de uma DATABASE_URL, sem usuário
// nem senha; URL ilegível vira um aviso em vez de vazar o conteúdo.
func descricaoBanco(databaseURL string) string {
	u, err := url.Parse(databaseURL)
	if err != nil || u.Host == "" {
		return "(DATABASE_URL não pôde ser interpretada)"
	}
	return u.Host + u.Path
}

// mensagemDeErro traduz os erros conhecidos para o operador.
func mensagemDeErro(err error) string {
	var semNome *services.ErroFilialPadraoSemNome
	if errors.As(err, &semNome) {
		return semNome.Error()
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "55P03" {
		return "não foi possível travar a tabela estoques em 30s (há transações abertas usando-a) — nada foi escrito; rode de novo numa janela de menos uso"
	}
	var naoMigravel *services.ErroEstoqueNaoMigravel
	if errors.As(err, &naoMigravel) {
		return fmt.Sprintf("%d estoque(s) legado(s) não podem ser vinculados à Filial padrão — confira a migração multi-Empresa (9.4) e corrija `empresa_id`/nomes duplicados; nada foi escrito", len(naoMigravel.Estoques))
	}
	return strings.TrimSpace(err.Error())
}
