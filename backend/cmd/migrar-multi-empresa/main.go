// Command migrar-multi-empresa executa a migração ÚNICA do banco da Ferreira
// Costa para o modelo multi-Empresa — Story 9.4 (Epic 9, FR-40/FR-41; AD-15,
// AD-23), spec-9-4.
//
// É deliberadamente um binário de CLI, disparado À MÃO por uma pessoa dentro
// do container `api`, e NUNCA por um workflow, job, entrypoint ou rota HTTP
// (AD-15, PRD §9). Duas razões:
//
//   - Os dados cadastrais da Empresa (CNPJ, razão social, endereço) são
//     entrada obrigatória do operador — não existe fonte automática deles.
//   - O endurecimento (`SET NOT NULL`) não pode ser uma migration embutida: o
//     `api` roda as migrations no boot e sai com erro se alguma falhar
//     (backend/main.go). Uma migration `SET NOT NULL` rodaria ANTES do
//     backfill, falharia contra as linhas órfãs e derrubaria o container em
//     laço — impedindo justamente o `docker compose exec` que rodaria o
//     backfill.
//
// Sem `--executar` o binário roda em DRY-RUN: imprime o diagnóstico completo
// (linhas órfãs por tabela, se a Empresa/Treinamento já existem, quais
// colunas já são NOT NULL) e não escreve absolutamente nada.
//
// Runbook do operador (a ordem é obrigatória):
//
//  1. ./migrar-multi-empresa
//     # dry-run: diagnóstico, nada escrito
//  2. ./migrar-multi-empresa --etapa empresa --executar \
//     --nome-fantasia "Ferreira Costa" --razao-social "..." --cnpj "..." \
//     --logradouro "..." --numero "..." --bairro "..." --cidade "..." \
//     --cep "..." --uf PE
//  3. ./migrar-multi-empresa --etapa backfill --executar --slug ferreira-costa
//  4. ./migrate-legado --empresa-slug ferreira-costa             # dry-run do corte legado
//     ./migrate-legado --empresa-slug ferreira-costa --executar  # se o corte ainda estiver pendente
//  5. ./migrar-multi-empresa --etapa endurecimento --executar
//
// O passo 3 vem ANTES do passo 4 de propósito: `migrate-legado` resolve o
// autor sintético das Movimentações e as Categorias por consulta DENTRO da
// Empresa, e essas linhas só entram na Empresa no backfill.
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"stockflow/backend/services"
)

// Etapas possíveis de --etapa.
const (
	etapaTudo          = "tudo"
	etapaEmpresa       = "empresa"
	etapaBackfill      = "backfill"
	etapaEndurecimento = "endurecimento"
)

// opcoes reúne tudo que o operador informou na linha de comando — um struct
// só, para que a função testável receba a configuração inteira em vez de 16
// parâmetros posicionais.
type opcoes struct {
	etapa    string
	executar bool

	nomeFantasia string
	razaoSocial  string
	cnpj         string
	logradouro   string
	numero       string
	complemento  string
	bairro       string
	cidade       string
	cep          string
	uf           string
	slug         string

	admNome  string
	admEmail string

	lote int
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	var o opcoes
	flag.StringVar(&o.etapa, "etapa", etapaTudo, "etapa da migração: tudo|empresa|backfill|endurecimento")
	flag.BoolVar(&o.executar, "executar", false, "aplica a migração; sem a flag o binário roda em dry-run e não escreve nada")
	flag.StringVar(&o.nomeFantasia, "nome-fantasia", "", "Nome Fantasia da Empresa (etapa empresa)")
	flag.StringVar(&o.razaoSocial, "razao-social", "", "Razão Social da Empresa (etapa empresa)")
	flag.StringVar(&o.cnpj, "cnpj", "", "CNPJ da Empresa, com ou sem máscara (etapa empresa)")
	flag.StringVar(&o.logradouro, "logradouro", "", "Logradouro do endereço da Empresa (etapa empresa)")
	flag.StringVar(&o.numero, "numero", "", "Número do endereço da Empresa (etapa empresa)")
	flag.StringVar(&o.complemento, "complemento", "", "Complemento do endereço da Empresa (opcional)")
	flag.StringVar(&o.bairro, "bairro", "", "Bairro do endereço da Empresa (etapa empresa)")
	flag.StringVar(&o.cidade, "cidade", "", "Cidade do endereço da Empresa (etapa empresa)")
	flag.StringVar(&o.cep, "cep", "", "CEP do endereço da Empresa, 8 dígitos (etapa empresa)")
	flag.StringVar(&o.uf, "uf", "", "UF do endereço da Empresa, 2 letras (etapa empresa)")
	flag.StringVar(&o.slug, "slug", "", "Endereço de acesso da Empresa; vazio deriva do Nome Fantasia")
	flag.StringVar(&o.admNome, "adm-nome", "", "Nome do adm do Ambiente de Treinamento; vazio reusa o do adm existente")
	flag.StringVar(&o.admEmail, "adm-email", "", "E-mail do adm do Ambiente de Treinamento; vazio reusa o do adm existente")
	flag.IntVar(&o.lote, "lote", 1000, "tamanho do lote do backfill (linhas por transação)")
	flag.Parse()

	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "erro: argumento inesperado %q — use as flags com os dois hifens\n", flag.Arg(0))
		os.Exit(1)
	}

	if err := validarOpcoes(o); err != nil {
		fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		flag.Usage()
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

	if err := executarMigracao(db, os.Stdout, services.CarregarEmailConfig(), o); err != nil {
		fmt.Fprintf(os.Stderr, "erro: %s\n", mensagemDeErro(err))
		os.Exit(1)
	}
}

// validarOpcoes checa a etapa e as flags que ela exige. As exigências de
// cadastro só valem em `--executar`: o dry-run é um diagnóstico do banco, não
// depende de nenhum dado da Empresa e tem de rodar com o binário "pelado"
// (`./migrar-multi-empresa`, passo 1 do runbook).
func validarOpcoes(o opcoes) error {
	switch o.etapa {
	case etapaTudo, etapaEmpresa, etapaBackfill, etapaEndurecimento:
	default:
		return fmt.Errorf("--etapa inválida: %q (use tudo|empresa|backfill|endurecimento)", o.etapa)
	}
	if o.lote <= 0 {
		return fmt.Errorf("--lote deve ser maior que zero (recebido %d)", o.lote)
	}
	if !o.executar {
		return nil
	}

	if o.etapa == etapaTudo || o.etapa == etapaEmpresa {
		obrigatorias := []struct{ flag, valor string }{
			{"--nome-fantasia", o.nomeFantasia},
			{"--razao-social", o.razaoSocial},
			{"--cnpj", o.cnpj},
			{"--logradouro", o.logradouro},
			{"--numero", o.numero},
			{"--bairro", o.bairro},
			{"--cidade", o.cidade},
			{"--cep", o.cep},
			{"--uf", o.uf},
		}
		for _, c := range obrigatorias {
			if strings.TrimSpace(c.valor) == "" {
				return fmt.Errorf("%s é obrigatório na etapa %q", c.flag, o.etapa)
			}
		}
	}

	// O backfill precisa saber QUAL Empresa adotar as linhas órfãs: o slug
	// explícito, ou o derivado do Nome Fantasia (o mesmo que a etapa
	// `empresa` gravou).
	if o.etapa == etapaBackfill && strings.TrimSpace(o.slug) == "" && strings.TrimSpace(o.nomeFantasia) == "" {
		return fmt.Errorf("--slug (ou --nome-fantasia, de onde o slug é derivado) é obrigatório na etapa %q", o.etapa)
	}
	return nil
}

// dadosDaEmpresa monta o insumo de services a partir das flags.
func dadosDaEmpresa(o opcoes) services.DadosEmpresa {
	return services.DadosEmpresa{
		NomeFantasia: o.nomeFantasia,
		RazaoSocial:  o.razaoSocial,
		CNPJ:         o.cnpj,
		Slug:         o.slug,
		Endereco: services.EnderecoEmpresa{
			Logradouro:  o.logradouro,
			Numero:      o.numero,
			Complemento: o.complemento,
			Bairro:      o.bairro,
			Cidade:      o.cidade,
			CEP:         o.cep,
			UF:          o.uf,
		},
	}
}

// slugAlvo devolve o slug da Empresa: o `--slug` informado, ou o derivado do
// Nome Fantasia (mesma NormalizarSlug que a etapa `empresa` usa ao gravar).
func slugAlvo(o opcoes) (string, error) {
	base := o.slug
	if strings.TrimSpace(base) == "" {
		base = o.nomeFantasia
	}
	return services.NormalizarSlug(base)
}

// executarMigracao é o ponto testável do binário: recebe `out` injetado e
// nunca chama os.Exit. Devolve o primeiro erro; o chamador traduz e sai com 1.
func executarMigracao(db *sql.DB, out io.Writer, emailCfg services.EmailConfig, o opcoes) error {
	if !o.executar {
		return imprimirDiagnostico(db, out, o)
	}

	switch o.etapa {
	case etapaEmpresa:
		return etapaCriarEmpresa(db, out, emailCfg, o)
	case etapaBackfill:
		return etapaRodarBackfill(db, out, o)
	case etapaEndurecimento:
		return etapaRodarEndurecimento(db, out)
	case etapaTudo:
		if err := etapaCriarEmpresa(db, out, emailCfg, o); err != nil {
			return err
		}
		if err := etapaRodarBackfill(db, out, o); err != nil {
			return err
		}
		return etapaRodarEndurecimento(db, out)
	}
	return fmt.Errorf("--etapa inválida: %q", o.etapa)
}

// imprimirDiagnostico é o dry-run: conta as órfãs de cada uma das 13 tabelas,
// diz se a Empresa/Treinamento já existem e lista as colunas já NOT NULL.
// NADA é escrito por este caminho.
func imprimirDiagnostico(db *sql.DB, out io.Writer, o opcoes) error {
	fmt.Fprintln(out, "dry-run (nada será escrito) — use --executar para aplicar.")

	contagens, err := services.ContarLinhasSemEmpresa(db)
	if err != nil {
		return err
	}
	totalOrfas := 0
	fmt.Fprintln(out, "\nlinhas sem Empresa (empresa_id IS NULL):")
	for _, t := range services.TabelasComEmpresaID {
		fmt.Fprintf(out, "  %-32s %d\n", t, contagens[t])
		totalOrfas += contagens[t]
	}
	fmt.Fprintf(out, "  %-32s %d\n", "TOTAL", totalOrfas)

	fmt.Fprintln(out, "\nEmpresa:")
	slug, errSlug := slugAlvo(o)
	switch {
	case strings.TrimSpace(o.slug) == "" && strings.TrimSpace(o.nomeFantasia) == "":
		fmt.Fprintln(out, "  nenhum --slug/--nome-fantasia informado — informe um para diagnosticar a Empresa")
	case errSlug != nil:
		fmt.Fprintf(out, "  slug inválido: %v\n", errSlug)
	default:
		relatarEmpresa(db, out, "real       ", slug)
		relatarEmpresa(db, out, "treinamento", slug+"-treinamento")
	}

	jaNotNull, err := services.ColunasJaNotNull(db)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "\ncolunas empresa_id já NOT NULL:")
	if len(jaNotNull) == 0 {
		fmt.Fprintln(out, "  nenhuma — o endurecimento ainda não rodou")
	} else {
		sort.Strings(jaNotNull)
		fmt.Fprintf(out, "  %s\n", strings.Join(jaNotNull, ", "))
	}

	// A conta `adm` global é a que o backfill ADOTA (nunca recria) e a que dá
	// nome/e-mail ao `adm` do Treinamento quando o operador não os informa.
	nome, email, err := services.BuscarAdmSemEmpresa(db)
	fmt.Fprintln(out, "\nconta adm sem Empresa (será adotada pelo backfill):")
	switch {
	case errors.Is(err, sql.ErrNoRows):
		fmt.Fprintln(out, "  nenhuma — informe --adm-nome/--adm-email para o adm do Treinamento")
	case err != nil:
		return err
	default:
		fmt.Fprintf(out, "  %s <%s>\n", nome, email)
	}
	return nil
}

// relatarEmpresa imprime uma linha de diagnóstico para um slug.
func relatarEmpresa(db *sql.DB, out io.Writer, rotulo, slug string) {
	e, err := services.BuscarEmpresaPorSlugQualquerStatus(db, slug)
	switch {
	case errors.Is(err, services.ErrEmpresaNaoEncontrada):
		fmt.Fprintf(out, "  %s  %-40s ainda não existe\n", rotulo, slug)
	case err != nil:
		fmt.Fprintf(out, "  %s  %-40s erro ao consultar: %v\n", rotulo, slug, err)
	default:
		fmt.Fprintf(out, "  %s  %-40s já existe (id=%s, status=%s)\n", rotulo, slug, e.ID, e.Status)
	}
}

// etapaCriarEmpresa cria a Empresa real (sem listas — ela adota as legadas no
// backfill) e a Empresa-irmã de Treinamento, numa transação só. Idempotente:
// com o par já criado, reporta "já existente" e devolve nil.
func etapaCriarEmpresa(db *sql.DB, out io.Writer, emailCfg services.EmailConfig, o opcoes) error {
	admNome, admEmail := strings.TrimSpace(o.admNome), strings.TrimSpace(o.admEmail)
	if admNome == "" || admEmail == "" {
		nome, email, err := services.BuscarAdmSemEmpresa(db)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("não há conta `adm` sem Empresa para reaproveitar — informe --adm-nome e --adm-email para o adm do Ambiente de Treinamento")
			}
			return err
		}
		if admNome == "" {
			admNome = nome
		}
		if admEmail == "" {
			admEmail = email
		}
	}

	slug, err := slugAlvo(o)
	if err != nil {
		return err
	}
	jaExistia := false
	if _, err := services.BuscarEmpresaPorSlugQualquerStatus(db, slug); err == nil {
		jaExistia = true
	} else if !errors.Is(err, services.ErrEmpresaNaoEncontrada) {
		return err
	}

	dados := dadosDaEmpresa(o)
	dados.Slug = slug
	empresa, treino, err := services.AdotarEmpresaFundadora(db, emailCfg, dados, admNome, admEmail)
	if err != nil {
		return err
	}

	if jaExistia {
		fmt.Fprintf(out, "etapa empresa: já existente — nada foi criado.\n")
	} else {
		fmt.Fprintf(out, "etapa empresa: criada.\n")
	}
	fmt.Fprintf(out, "  empresa      id=%s slug=%s nome=%q\n", empresa.ID, empresa.Slug, empresa.NomeFantasia)
	if treino.ID != "" {
		fmt.Fprintf(out, "  treinamento  id=%s slug=%s nome=%q\n", treino.ID, treino.Slug, treino.NomeFantasia)
	} else {
		fmt.Fprintln(out, "  treinamento  ausente — a Empresa existe sem Ambiente de Treinamento")
	}
	fmt.Fprintln(out, "  a conta `adm` existente NÃO foi alterada: ela é adotada pela etapa `backfill`.")
	return nil
}

// etapaRodarBackfill resolve a Empresa pelo slug e atribui a ela toda linha
// ainda sem Empresa nas 13 tabelas, em lotes. Resumível e idempotente: uma
// reexecução depois do fim atualiza 0 linhas.
func etapaRodarBackfill(db *sql.DB, out io.Writer, o opcoes) error {
	slug, err := slugAlvo(o)
	if err != nil {
		return err
	}
	empresa, err := services.BuscarEmpresaPorSlug(db, slug)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "etapa backfill: empresa=%s (id=%s), lote=%d\n", empresa.Slug, empresa.ID, o.lote)
	total := 0
	atualizadas, err := services.BackfillEmpresa(db, empresa.ID, o.lote, func(tabela string, n int) {
		fmt.Fprintf(out, "  %-32s %d linha(s)\n", tabela, n)
	})
	for _, t := range services.TabelasComEmpresaID {
		total += atualizadas[t]
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "  %-32s %d linha(s)\n", "TOTAL", total)
	fmt.Fprintln(out, "  listas padrão da Empresa completadas (o que ela não adotou do legado).")
	return nil
}

// etapaRodarEndurecimento aplica o NOT NULL nas 13 colunas — recusado
// enquanto restar UMA linha órfã, sem emitir DDL nenhum.
func etapaRodarEndurecimento(db *sql.DB, out io.Writer) error {
	endurecidas, err := services.EndurecerEmpresaID(db)
	if err != nil {
		return err
	}
	if len(endurecidas) == 0 {
		fmt.Fprintln(out, "etapa endurecimento: nada a fazer — as 13 colunas já são NOT NULL.")
		return nil
	}
	fmt.Fprintf(out, "etapa endurecimento: %d coluna(s) agora NOT NULL:\n", len(endurecidas))
	for _, t := range endurecidas {
		fmt.Fprintf(out, "  %s.empresa_id\n", t)
	}
	return nil
}

// mensagemDeErro traduz os sentinelas conhecidos para o operador.
func mensagemDeErro(err error) string {
	var validacao *services.ErroEmpresaValidacao
	switch {
	case errors.As(err, &validacao):
		return "dados da Empresa inválidos: " + validacao.Mensagem
	case errors.Is(err, services.ErrCNPJDuplicado):
		return "já existe uma Empresa com este CNPJ — nada foi gravado"
	case errors.Is(err, services.ErrSlugTreinamentoDuplicado):
		return "o endereço do Ambiente de Treinamento ({slug}-treinamento) já está em uso — nada foi gravado"
	case errors.Is(err, services.ErrSlugDuplicado):
		return "já existe uma Empresa com este endereço (slug) — nada foi gravado"
	case errors.Is(err, services.ErrEmpresaNaoEncontrada):
		return "nenhuma Empresa ativa com esse endereço (slug) — rode a etapa `empresa` antes, ou confira o --slug"
	case errors.Is(err, services.ErrOrfasPendentes):
		return err.Error()
	default:
		return err.Error()
	}
}
