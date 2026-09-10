package services

import (
	"database/sql"
	"errors"
	"strconv"
	"testing"
)

// Helpers de Empresa para a suíte de `services` — Story 9.1 (Multi-Empresa),
// spec-9-1. Toda função de service passou a receber `empresaID` como
// argumento explícito, então cada teste precisa de uma Empresa real para
// operar dentro dela.
//
// `empresaTeste` é a Empresa PADRÃO da suíte: provisionada uma única vez
// (`testDB`, auth_test.go, chama garantirEmpresaTeste a cada teste, mas o
// `SELECT` prévio a reaproveita) e nunca truncada — nenhum TRUNCATE desta
// suíte alcança `empresas`, e as linhas de `categorias`/
// `nomenclatura_templates` copiadas para ela por ProvisionarEmpresa
// sobrevivem junto, exatamente como o seed fixo das migrações 000010/000013
// já sobrevivia.
//
// O teste de isolamento (isolamento_test.go) NÃO usa esta Empresa: ele cria
// as suas duas, justamente para provar que uma nunca enxerga a outra.

// slugEmpresaTeste é o slug da Empresa padrão da suíte.
const slugEmpresaTeste = "empresa-teste"

// empresaTeste é o id da Empresa padrão — preenchido por testDB antes de
// qualquer teste rodar. Ler esta variável fora de um teste que passou por
// testDB devolveria "" (e toda query filtraria por Empresa nenhuma, o que
// falha fechado, nunca vaza).
var empresaTeste string

// cnpjDeTeste completa um prefixo de 12 dígitos com os dois dígitos
// verificadores corretos (mesmo cálculo de ValidarCNPJ) — evita espalhar
// CNPJs mágicos pelos testes.
func cnpjDeTeste(base12 string) string {
	comD1 := base12 + strconv.Itoa(digitoVerificadorCNPJ(base12))
	return comD1 + strconv.Itoa(digitoVerificadorCNPJ(comD1))
}

// criarEmpresaDeTeste provisiona uma Empresa nova (com as cópias das listas
// padrão de Categoria/Nomenclatura) e devolve a projeção gravada. Falha o
// teste em qualquer erro — é setup, nunca o objeto sob teste.
func criarEmpresaDeTeste(t *testing.T, db *sql.DB, slug, cnpjBase12, nomeFantasia string) Empresa {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("criarEmpresaDeTeste(%s): begin: %v", slug, err)
	}
	defer func() { _ = tx.Rollback() }()

	e, err := ProvisionarEmpresa(tx, DadosEmpresa{
		NomeFantasia: nomeFantasia,
		RazaoSocial:  nomeFantasia + " LTDA",
		CNPJ:         cnpjDeTeste(cnpjBase12),
		Slug:         slug,
		Endereco: EnderecoEmpresa{
			Logradouro: "Rua de Teste",
			Numero:     "100",
			Bairro:     "Centro",
			Cidade:     "Recife",
			CEP:        "50000000",
			UF:         "PE",
		},
	})
	if err != nil {
		t.Fatalf("criarEmpresaDeTeste(%s): %v", slug, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("criarEmpresaDeTeste(%s): commit: %v", slug, err)
	}
	return e
}

// garantirEmpresaTeste devolve o id da Empresa padrão da suíte, criando-a na
// primeira chamada. Idempotente por natureza: o `SELECT` prévio reaproveita a
// linha já gravada por um teste anterior do mesmo processo.
func garantirEmpresaTeste(t *testing.T, db *sql.DB) string {
	t.Helper()
	var id string
	err := db.QueryRow(`SELECT id FROM empresas WHERE slug = $1`, slugEmpresaTeste).Scan(&id)
	if err == nil {
		return id
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("garantirEmpresaTeste: %v", err)
	}
	return criarEmpresaDeTeste(t, db, slugEmpresaTeste, "112223330001", "Empresa Teste").ID
}

// removerEmpresaDeTeste apaga uma Empresa criada por um teste e as linhas que
// só existem por causa dela (as cópias de `categorias`/
// `nomenclatura_templates` feitas por ProvisionarEmpresa). Necessário porque
// `empresas` sobrevive entre testes e entre execuções: sem isto, um segundo
// `go test` esbarraria no CNPJ/slug já gravado pela execução anterior.
//
// NUNCA apaga as linhas padrão (`empresa_id IS NULL`, seed das migrations
// 000010/000013), nem a Empresa padrão da suíte.
func removerEmpresaDeTeste(t *testing.T, db *sql.DB, slug string) {
	t.Helper()
	if slug == slugEmpresaTeste {
		t.Fatalf("removerEmpresaDeTeste(%q): a Empresa padrão da suíte nunca é removida", slug)
	}

	var id string
	err := db.QueryRow(`SELECT id FROM empresas WHERE slug = $1`, slug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return
	}
	if err != nil {
		t.Fatalf("removerEmpresaDeTeste(%s): %v", slug, err)
	}

	for _, stmt := range []string{
		// Story 9.3: `convites_empresa` tem FK para `empresas` — sem apagá-los
		// primeiro, o DELETE da Empresa falharia. (Entre testes eles já somem
		// pelo TRUNCATE usuarios CASCADE, mas este helper roda no fim do
		// teste, com as linhas ainda vivas.)
		`DELETE FROM convites_empresa WHERE empresa_id = $1`,
		`DELETE FROM categorias WHERE empresa_id = $1`,
		`DELETE FROM nomenclatura_templates WHERE empresa_id = $1`,
		`DELETE FROM empresas WHERE id = $1`,
	} {
		if _, err := db.Exec(stmt, id); err != nil {
			t.Fatalf("removerEmpresaDeTeste(%s) [%s]: %v", slug, stmt, err)
		}
	}
}
