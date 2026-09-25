package services

import (
	"errors"
	"sync"
	"testing"
)

// Story 16.4 — EAN-13 único entre os Produtos ativos da Empresa.

const eanUnico = "7891000000014"

func TestCriarProduto_EANEmUsoPorAtivo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criarProdutoInativacao(t, db, empresaTeste, "Produto EAN Primeiro", eanUnico)

	_, err := CriarProduto(db, empresaTeste, CriarProdutoInput{
		UnidadeMedida: "un", Nome: "Produto EAN Segundo",
		CategoriaID: categoriaIDPorCodigoEmpresa(t, db, empresaTeste, "04.001"),
		TemplateID:  templateGenericoID(t, db, empresaTeste),
		EAN13:       eanUnico,
	})
	var ee *ErroEANEmUso
	if !errors.As(err, &ee) || ee.Nome != "Produto EAN Primeiro" {
		t.Fatalf("err = %v, want ErroEANEmUso do primeiro", err)
	}
	var n int
	_ = db.QueryRow(`SELECT count(*) FROM produtos WHERE empresa_id = $1`, empresaTeste).Scan(&n)
	if n != 1 {
		t.Errorf("produtos = %d, want 1 (nada gravado)", n)
	}
}

func TestCriarProduto_EANEmInativoOuOutraEmpresaAceito(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestor := semearConta(t, db, "Gestora EAN16", "gestora.ean16@empresa.com", PapelGestor, 0)
	antigo := criarProdutoInativacao(t, db, empresaTeste, "Produto EAN Inativo", eanUnico)
	if err := InativarProduto(db, empresaTeste, gestor, antigo, ""); err != nil {
		t.Fatal(err)
	}
	criarProdutoInativacao(t, db, empresaTeste, "Produto EAN Ativo Novo", eanUnico)
	alheia := empresaAlheiaLotes(t, db)
	criarProdutoInativacao(t, db, alheia.ID, "Produto EAN Alheio", eanUnico)
}

func TestAtualizarProduto_EANEmUso(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	ator := atorHistorico(t, db)
	a := criarProdutoInativacao(t, db, empresaTeste, "Produto EAN Alfa", eanUnico)
	b := criarProdutoInativacao(t, db, empresaTeste, "Produto EAN Beta", "")

	in := criarProdutoInputValido(t, db, "Produto EAN Beta", "04.001")
	in.EAN13 = eanUnico
	_, err := AtualizarProduto(db, empresaTeste, ator, b, in)
	var ee *ErroEANEmUso
	if !errors.As(err, &ee) || ee.Nome != "Produto EAN Alfa" {
		t.Fatalf("err = %v, want ErroEANEmUso", err)
	}
	var ean *string
	_ = db.QueryRow(`SELECT ean13 FROM produtos WHERE id = $1`, b).Scan(&ean)
	if ean != nil {
		t.Errorf("ean gravado = %v, want nil", *ean)
	}

	// Editar o próprio Produto mantendo o EAN é aceito.
	inA := criarProdutoInputValido(t, db, "Produto EAN Alfa", "04.001")
	inA.EAN13 = eanUnico
	if _, err := AtualizarProduto(db, empresaTeste, ator, a, inA); err != nil {
		t.Fatalf("editar sem mudar EAN: %v", err)
	}
}

func TestAtualizarProduto_DuplicataAntigaSoSalvaAposCorrigir(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	ator := atorHistorico(t, db)
	a := criarProdutoInativacao(t, db, empresaTeste, "Produto Dup Alfa", eanUnico)
	b := criarProdutoInativacao(t, db, empresaTeste, "Produto Dup Beta", "")
	// Duplicata pré-existente (dado legado), sem passar pelo service.
	if _, err := db.Exec(`UPDATE produtos SET ean13 = $1 WHERE id = $2`, eanUnico, b); err != nil {
		t.Fatal(err)
	}
	in := criarProdutoInputValido(t, db, "Produto Dup Beta", "04.001")
	in.EAN13 = eanUnico
	var ee *ErroEANEmUso
	if _, err := AtualizarProduto(db, empresaTeste, ator, b, in); !errors.As(err, &ee) {
		t.Fatalf("err = %v, want ErroEANEmUso", err)
	}
	// Inativar o outro libera a edição.
	gestor := semearConta(t, db, "Gestora Dup", "gestora.dup@empresa.com", PapelGestor, 0)
	if err := InativarProduto(db, empresaTeste, gestor, a, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := AtualizarProduto(db, empresaTeste, ator, b, in); err != nil {
		t.Fatalf("após inativar o outro: %v", err)
	}
}

func TestCriarProduto_EANConcorrenteSoUmAceito(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	var wg sync.WaitGroup
	erros := make([]error, 6)
	for i := range erros {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, erros[i] = CriarProduto(db, empresaTeste, CriarProdutoInput{
				UnidadeMedida: "un", Nome: "Produto EAN Corrida " + string(rune('A'+i)),
				CategoriaID: categoriaIDPorCodigoEmpresa(t, db, empresaTeste, "04.001"),
				TemplateID:  templateGenericoID(t, db, empresaTeste),
				EAN13:       eanUnico,
			})
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range erros {
		var ee *ErroEANEmUso
		switch {
		case err == nil:
			ok++
		case !errors.As(err, &ee):
			t.Errorf("erro inesperado: %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("aceitos = %d, want 1", ok)
	}
}
