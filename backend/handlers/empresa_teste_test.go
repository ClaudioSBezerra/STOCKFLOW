package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// Helpers de Empresa para a suíte de `handlers` — Story 9.1 (Multi-Empresa),
// spec-9-1. Toda rota de negócio vive sob `/e/{slug}/api/...` e resolve a
// Empresa uma única vez em `middleware.RequireEmpresa` (AD-19), então cada
// teste de handler precisa despachar pela MESMA composição: RequireEmpresa
// por fora, RequireAuth/RequireRole por dentro, como em `newMux`.
//
// A Empresa padrão da suíte é criada uma vez e nunca truncada — `TRUNCATE
// TABLE usuarios CASCADE` (testDB) não alcança `empresas`, que é a tabela
// referenciada, não a referenciadora. As linhas de `categorias`/
// `nomenclatura_templates` copiadas para ela por ProvisionarEmpresa
// sobrevivem junto.

// slugEmpresaTeste é o slug da Empresa padrão da suíte — o mesmo usado pela
// suíte de `services`, que compartilha o banco: a primeira suíte a rodar
// cria a linha, a outra a reaproveita pelo SELECT por slug.
const slugEmpresaTeste = "empresa-teste"

// prefixoEmpresaTeste é o prefixo de rota da Empresa padrão. Todo caminho
// literal dos testes é escrito a partir dele, como o browser faria.
const prefixoEmpresaTeste = "/e/" + slugEmpresaTeste

// empresaTeste é o id da Empresa padrão — preenchido por testDB antes de
// qualquer teste rodar.
var empresaTeste string

// garantirEmpresaTeste devolve o id da Empresa padrão, provisionando-a na
// primeira chamada do processo (ou reaproveitando a linha já gravada por
// outra suíte contra o mesmo banco).
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

// comEmpresa compõe RequireEmpresa por fora de `h`, como newMux faz com as
// 63 rotas. Quando o teste despacha o handler direto (sem mux), o `{slug}`
// do padrão de rota não existe para ser lido, então ele é injetado aqui —
// é o único ponto de teste que simula o casamento do padrão de rota, nunca
// uma via de entrada da Empresa pelo cliente.
func comEmpresa(db *sql.DB, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("slug") == "" {
			r.SetPathValue("slug", slugEmpresaTeste)
		}
		middleware.RequireEmpresa(db)(h)(w, r)
	}
}
