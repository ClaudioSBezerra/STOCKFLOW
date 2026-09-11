package main

import (
	"database/sql"
	"errors"
	"testing"

	"stockflow/backend/services"
)

// Helpers de Empresa para a suíte de `cmd/migrate-legado` — Story 9.4
// (Migração da Ferreira Costa para o modelo multi-Empresa), spec-9-4.
//
// Desde a 9.4 o corte legado exige `--empresa-slug`: toda linha escrita nasce
// dentro de uma Empresa e toda resolução (Estoque por nome, Categoria, autor
// por e-mail, código de Produto) é recortada por ela. Os testes, portanto,
// precisam de uma Empresa real para passar às quatro fases.
//
// A Empresa padrão é a MESMA das suítes de `services`/`handlers`/`main`
// (slug `empresa-teste`): a primeira suíte a rodar cria a linha, as outras a
// reaproveitam pelo SELECT por slug. `empresas` nunca é truncada por
// ninguém, e as cópias de `categorias`/`nomenclatura_templates` que
// ProvisionarEmpresa faz para ela sobrevivem junto — é delas que
// `migrarProdutos` resolve `categoria_id` agora.
const slugEmpresaTeste = "empresa-teste"

// empresaTeste é o id da Empresa padrão da suíte, preenchido por testDB
// antes de qualquer teste rodar.
var empresaTeste string

// garantirEmpresaTeste devolve o id da Empresa padrão, provisionando-a na
// primeira chamada do processo. Idempotente: o SELECT prévio reaproveita a
// linha já gravada por outra suíte contra o mesmo banco.
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
