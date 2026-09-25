package services

import (
	"database/sql"
	"errors"
	"testing"
)

// atorHistorico devolve (criando se preciso) um usuário para servir de autor.
func atorHistorico(t *testing.T, db *sql.DB) string {
	t.Helper()
	const email = "ator.historico@empresa.com"
	var id string
	err := db.QueryRow(`SELECT id FROM usuarios WHERE email = $1`, email).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return semearConta(t, db, "Ator Historico", email, PapelAlmoxarife, 0)
	}
	if err != nil {
		t.Fatalf("buscar ator: %v", err)
	}
	return id
}

func inputEdicao(t *testing.T, db *sql.DB, nome, obs string) CriarProdutoInput {
	return CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          nome,
		Observacoes:   obs,
		CategoriaID:   categoriaIDPorCodigoEmpresa(t, db, empresaTeste, "04.001"),
		TemplateID:    templateGenericoID(t, db, empresaTeste),
	}
}

func TestHistorico_EdicaoMudaNomeGravaLinha(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	ator := atorHistorico(t, db)
	id := criarProdutoInativacao(t, db, empresaTeste, "Nome Antigo Valido", "")

	if _, err := AtualizarProduto(db, empresaTeste, ator, id, inputEdicao(t, db, "  Nome Novo Valido  ", "")); err != nil {
		t.Fatal(err)
	}
	h := historicoProduto(t, db, id)
	if len(h) != 1 || h[0].acao != AcaoProdutoNomeAlterado || h[0].atorID != ator ||
		h[0].detalhe["antes"] != "Nome Antigo Valido" || h[0].detalhe["depois"] != "Nome Novo Valido" {
		t.Fatalf("histórico = %+v", h)
	}
}

func TestHistorico_EdicaoSemMudarNomeNaoGrava(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	ator := atorHistorico(t, db)
	id := criarProdutoInativacao(t, db, empresaTeste, "Nome Igual Valido", "")
	if _, err := AtualizarProduto(db, empresaTeste, ator, id, inputEdicao(t, db, "Nome Igual Valido", "obs nova")); err != nil {
		t.Fatal(err)
	}
	if h := historicoProduto(t, db, id); len(h) != 0 {
		t.Fatalf("histórico = %+v, want vazio", h)
	}
}

func TestHistorico_RenomearGravaEIgualNao(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	ator := atorHistorico(t, db)
	id := criarProdutoInativacao(t, db, empresaTeste, "Nome Renomear Antigo", "")
	if _, err := AtualizarNomeProduto(db, empresaTeste, ator, id, "Nome Renomear Antigo"); err != nil {
		t.Fatal(err)
	}
	if h := historicoProduto(t, db, id); len(h) != 0 {
		t.Fatalf("igual gravou: %+v", h)
	}
	if _, err := AtualizarNomeProduto(db, empresaTeste, ator, id, "Nome Renomear Novo"); err != nil {
		t.Fatal(err)
	}
	h := historicoProduto(t, db, id)
	if len(h) != 1 || h[0].acao != AcaoProdutoNomeAlterado || h[0].detalhe["depois"] != "Nome Renomear Novo" {
		t.Fatalf("histórico = %+v", h)
	}
}

func TestHistorico_ValidacaoFalhaNaoGrava(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	ator := atorHistorico(t, db)
	id := criarProdutoInativacao(t, db, empresaTeste, "Nome Estavel Valido", "")
	// nome fora do template exige template estrito; aqui basta uma categoria inexistente
	in := inputEdicao(t, db, "Nome Que Nao Persiste", "")
	in.CategoriaID = "00000000-0000-4000-8000-000000000099"
	if _, err := AtualizarProduto(db, empresaTeste, ator, id, in); err == nil {
		t.Fatal("esperava erro")
	}
	if h := historicoProduto(t, db, id); len(h) != 0 {
		t.Fatalf("histórico = %+v", h)
	}
	var nome string
	_ = db.QueryRow(`SELECT nome FROM produtos WHERE id = $1`, id).Scan(&nome)
	if nome != "Nome Estavel Valido" {
		t.Fatalf("nome = %q", nome)
	}
}

func TestListarHistoricoProduto(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	ator := atorHistorico(t, db)
	id := criarProdutoInativacao(t, db, empresaTeste, "Nome Lista Antigo", "")

	vazio, err := ListarHistoricoProduto(db, empresaTeste, id)
	if err != nil || vazio == nil || len(vazio) != 0 {
		t.Fatalf("vazio = %v, %v", vazio, err)
	}
	if _, err := AtualizarNomeProduto(db, empresaTeste, ator, id, "Nome Lista Novo"); err != nil {
		t.Fatal(err)
	}
	if err := InativarProduto(db, empresaTeste, ator, id, "motivo x"); err != nil {
		t.Fatal(err)
	}
	itens, err := ListarHistoricoProduto(db, empresaTeste, id)
	if err != nil || len(itens) != 2 {
		t.Fatalf("itens = %+v, %v", itens, err)
	}
	if itens[0].Acao != AcaoProdutoInativado || itens[1].Acao != AcaoProdutoNomeAlterado {
		t.Errorf("ordem = %s, %s", itens[0].Acao, itens[1].Acao)
	}
	if itens[0].Autor != "Ator Historico" || itens[0].Detalhe["motivo"] != "motivo x" || itens[0].CriadoEm == "" {
		t.Errorf("item = %+v", itens[0])
	}

	for _, pid := range []string{"nao-uuid", "00000000-0000-4000-8000-000000000001"} {
		if _, err := ListarHistoricoProduto(db, empresaTeste, pid); !errors.Is(err, ErrProdutoNaoEncontrado) {
			t.Errorf("id %q err = %v", pid, err)
		}
	}
	if _, err := ListarHistoricoProduto(db, "00000000-0000-4000-8000-000000000002", id); !errors.Is(err, ErrProdutoNaoEncontrado) {
		t.Errorf("outra empresa err = %v", err)
	}
}
