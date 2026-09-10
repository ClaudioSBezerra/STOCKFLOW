package main

import (
	"database/sql"
	"errors"
	"testing"

	"stockflow/backend/services"
)

// Helpers de Empresa para a suíte do pacote `main` — Story 9.1
// (Multi-Empresa), spec-9-1. `newMux` registra as 63 rotas de negócio sob
// `/e/{slug}/api/...` (só `GET /api/health` fica fora), então todo teste de
// composição precisa de uma Empresa real para o slug do caminho resolver.
//
// A Empresa padrão é criada uma vez e nunca truncada: `TRUNCATE TABLE
// usuarios CASCADE` não alcança `empresas` (é a tabela referenciada, não a
// referenciadora), e as linhas de `categorias`/`nomenclatura_templates`
// copiadas para ela por ProvisionarEmpresa sobrevivem junto — exatamente
// como o seed fixo das migrações 000010/000013 já sobrevivia.

// slugEmpresaTeste é o slug da Empresa padrão — o mesmo das suítes de
// `services` e `handlers`, que compartilham o banco: a primeira a rodar cria
// a linha, as outras a reaproveitam pelo SELECT por slug.
const slugEmpresaTeste = "empresa-teste"

// prefixoEmpresaTeste é o prefixo de rota da Empresa padrão; todo caminho
// literal dos testes nasce dele, como o browser faria.
const prefixoEmpresaTeste = "/e/" + slugEmpresaTeste

// empresaTeste é o id da Empresa padrão, preenchido por testDB.
var empresaTeste string

// garantirEmpresaTeste devolve o id da Empresa padrão, provisionando-a na
// primeira chamada do processo.
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

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("garantirEmpresaTeste: begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	e, err := services.ProvisionarEmpresa(tx, services.DadosEmpresa{
		NomeFantasia: "Empresa Teste",
		RazaoSocial:  "Empresa Teste LTDA",
		CNPJ:         "11222333000181",
		Slug:         slugEmpresaTeste,
		Endereco: services.EnderecoEmpresa{
			Logradouro: "Rua de Teste",
			Numero:     "100",
			Bairro:     "Centro",
			Cidade:     "Recife",
			CEP:        "50000000",
			UF:         "PE",
		},
	})
	if err != nil {
		t.Fatalf("garantirEmpresaTeste: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("garantirEmpresaTeste: commit: %v", err)
	}
	return e.ID
}
