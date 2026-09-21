// Command seed-fotos-treinamento grava as fotos de exemplo nos Produtos de
// exemplo de UM Ambiente de Treinamento — Story 12.4 (Epic 12, FR-51; AD-15),
// spec-12-4.
//
// Para cada Produto de exemplo (Story 9.2) do Treinamento indicado em
// `--empresa`, grava uma foto embarcada com o mesmo armazenamento versionado
// das fotos de Produto (`<produto_id>-<timestamp>.jpg` em FOTOS_DIR, sem
// overwrite). Produto que já tem foto é pulado (preserva foto real enviada
// pelo Adm) e Produto ausente é só reportado; reexecutar não grava nada.
// Empresa real é recusada sem escrever.
//
// É deliberadamente um binário de CLI, disparado À MÃO por uma pessoa dentro
// do container `api` (é ele que enxerga o volume das fotos), e NUNCA por
// workflow, job, entrypoint, cron ou rota HTTP (AD-15). Treinamentos criados
// antes desta story só mudam quando alguém roda o binário para eles.
//
// Sem `--executar` o binário roda em DRY-RUN: relata o que faria e não
// escreve nada em disco.
//
// Runbook do operador (dentro do container `api`):
//
//  1. ./seed-fotos-treinamento --empresa <slug>-treinamento
//     # dry-run: relatório (a semear / já com foto / ausentes), nada escrito
//  2. ./seed-fotos-treinamento --empresa <slug>-treinamento --executar
//     # grava as fotos; reexecutar é inócuo (0 semeadas)
//
// `--fotos-dir` assume o padrão de FOTOS_DIR (senão ./fotos).
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"stockflow/backend/services"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	slug := flag.String("empresa", "", "slug do Ambiente de Treinamento (obrigatório)")
	fotosDir := flag.String("fotos-dir", "", "diretório das fotos de Produto (padrão: FOTOS_DIR, senão ./fotos)")
	executar := flag.Bool("executar", false, "grava as fotos; sem a flag o binário roda em dry-run e não escreve nada")
	flag.Parse()

	if err := validarArgumentos(*slug, flag.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "erro: %s\n", err)
		os.Exit(1)
	}

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("falha ao carregar .env", "error", err)
	}

	*fotosDir = resolverFotosDir(*fotosDir, os.Getenv("FOTOS_DIR")) // FOTOS_DIR pode vir do .env carregado acima

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

	if err := executarSemeadura(db, os.Stdout, *fotosDir, strings.TrimSpace(*slug), *executar); err != nil {
		fmt.Fprintf(os.Stderr, "erro: %s\n", mensagemDeErro(err))
		os.Exit(1)
	}
}

// executarSemeadura é o ponto testável do binário: recebe `out` injetado e
// nunca chama os.Exit.
func executarSemeadura(db *sql.DB, out io.Writer, fotosDir, slug string, executar bool) error {
	if !executar {
		fmt.Fprintln(out, "dry-run (nada será escrito) — use --executar para gravar.")
	}

	fmt.Fprintf(out, "diretório de fotos: %s\n", fotosDir)
	res, err := services.SemearFotosTreinamento(db, fotosDir, slug, executar)
	if err != nil {
		return err
	}

	rotulo := "a semear"
	if res.Executado {
		rotulo = "semeadas"
	}
	fmt.Fprintf(out, "%s: %d\n", rotulo, res.Semeadas)
	fmt.Fprintf(out, "já com foto: %d\n", res.JaComFoto)
	fmt.Fprintf(out, "ausentes: %d\n", len(res.Ausentes))
	for _, nome := range res.Ausentes {
		fmt.Fprintf(out, "  produto de exemplo ausente: %q\n", nome)
	}
	return nil
}

// resolverFotosDir escolhe o diretório de fotos: `--fotos-dir`, senão
// `FOTOS_DIR` (o mesmo volume que a API serve), senão `./fotos`.
func resolverFotosDir(flagDir, envDir string) string {
	if flagDir != "" {
		return flagDir
	}
	if envDir != "" {
		return envDir
	}
	return "./fotos"
}

// validarArgumentos recusa argumento posicional (flag sem os dois hifens) e
// `--empresa` ausente ou em branco.
func validarArgumentos(slug string, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("argumento inesperado %q — use as flags com os dois hifens", args[0])
	}
	if strings.TrimSpace(slug) == "" {
		return errors.New("--empresa é obrigatório (slug do Ambiente de Treinamento)")
	}
	return nil
}

// mensagemDeErro traduz os erros conhecidos para o operador.
func mensagemDeErro(err error) string {
	switch {
	case errors.Is(err, services.ErrEmpresaNaoTreinamento):
		return "a empresa informada é uma Empresa real, não um Ambiente de Treinamento; nada foi escrito"
	case errors.Is(err, services.ErrTreinamentoSemProdutosExemplo):
		return "o Ambiente de Treinamento não tem nenhum dos Produtos de exemplo (nunca semeado ou renomeados); nada foi escrito"
	case errors.Is(err, services.ErrEmpresaNaoEncontrada):
		return "empresa não encontrada (slug inexistente ou inativo); nada foi escrito"
	}
	return strings.TrimSpace(err.Error())
}
