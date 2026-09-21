package services

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

// --- Story 4.3: Visualização em grade e tabela agrupada -------------------
//
// Helpers do mesmo pacote reutilizados: limparProdutos, categoriaIDPorCodigo,
// testDB, ptrFloat, ptrStr (produtos_test.go).

// criarProdutoCat cadastra um Produto via CriarProduto e devolve o id e o
// código gerado pelo servidor (Story 10.2, spec-10-2 — `codigo` não é mais
// entrada do chamador). A linha inicial em `produto_estoque` (EstoqueID +
// QuantidadeInicial) segue a regra de CriarProduto; os testes ajustam depois
// com setQuantidade / limparEstoqueDe.
func criarProdutoCat(t *testing.T, db *sql.DB, in CriarProdutoInput) (id string, codigo string) {
	t.Helper()
	// Story 10.1: `TemplateID` passou a ser obrigatório em CriarProduto; os
	// testes desta suíte (catálogo) não exercitam Nomenclatura Guiada, então
	// caem no fallback Genérico ([NOME LIVRE], AD-34) quando o chamador não
	// informa um explicitamente.
	if in.TemplateID == "" {
		in.TemplateID = templateGenericoID(t, db, empresaTeste)
	}
	p, err := CriarProduto(db, empresaTeste, in)
	if err != nil {
		t.Fatalf("seed CriarProduto(%q): %v", in.Nome, err)
	}
	return p.ID, p.Codigo
}

// setQuantidade fixa a quantidade de um Produto num Estoque (upsert na PK
// composta de `produto_estoque`).
func setQuantidade(t *testing.T, db *sql.DB, produtoID, estoqueID string, qtd float64) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO produto_estoque (produto_id, estoque_id, quantidade) VALUES ($1, $2, $3)
		 ON CONFLICT (produto_id, estoque_id) DO UPDATE SET quantidade = EXCLUDED.quantidade`,
		produtoID, estoqueID, qtd,
	); err != nil {
		t.Fatalf("setQuantidade(%s,%s,%v): %v", produtoID, estoqueID, qtd, err)
	}
}

// limparEstoqueDe remove todas as linhas `produto_estoque` de um Produto —
// para o caso "Produto sem nenhuma linha de estoque" (CriarProduto sempre
// cria a linha inicial).
func limparEstoqueDe(t *testing.T, db *sql.DB, produtoID string) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM produto_estoque WHERE produto_id = $1`, produtoID); err != nil {
		t.Fatalf("limparEstoqueDe(%s): %v", produtoID, err)
	}
}

// TestListarCatalogoGrade_PaginacaoEOrdem prova a linha "Grade, página 1" da
// matriz: 30 Produtos -> página 1 traz 24 ordenados por nome, paginacao
// {1,24,30,2}; página 2 traz os 6 restantes.
func TestListarCatalogoGrade_PaginacaoEOrdem(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Catalogo Grade")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	// Inserção fora de ordem alfabética de propósito — a query deve ordenar.
	for i := 29; i >= 0; i-- {
		criarProdutoCatComSaldo(t, db, CriarProdutoInput{
			UnidadeMedida: "un",
			Nome:          fmt.Sprintf("Produto %02d", i),
			CategoriaID:   categoriaID,
		}, estoque.ID, 1)
	}

	itens, pag, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoGrade(1): %v", err)
	}
	if len(itens) != 24 {
		t.Fatalf("página 1: len = %d, want 24", len(itens))
	}
	if pag != (Paginacao{Pagina: 1, Tamanho: 24, Total: 30, TotalPaginas: 2}) {
		t.Errorf("paginacao = %+v, want {1 24 30 2}", pag)
	}
	if itens[0].Nome != "Produto 00" || itens[23].Nome != "Produto 23" {
		t.Errorf("ordem = [%q .. %q], want [Produto 00 .. Produto 23]", itens[0].Nome, itens[23].Nome)
	}

	itens2, pag2, err := ListarCatalogoGrade(db, 2, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoGrade(2): %v", err)
	}
	if len(itens2) != 6 {
		t.Fatalf("página 2: len = %d, want 6", len(itens2))
	}
	if itens2[0].Nome != "Produto 24" || itens2[5].Nome != "Produto 29" {
		t.Errorf("página 2 ordem = [%q .. %q], want [Produto 24 .. Produto 29]", itens2[0].Nome, itens2[5].Nome)
	}
	if pag2.Pagina != 2 || pag2.Total != 30 || pag2.TotalPaginas != 2 {
		t.Errorf("paginacao pág2 = %+v", pag2)
	}
}

// TestListarCatalogoGrade_ProdutoSemEstoque prova a linha "Grade, Produto sem
// estoque": Produto sem nenhuma linha `produto_estoque` -> quantidadeTotal 0,
// disponivel false, ainda aparece.
func TestListarCatalogoGrade_ProdutoSemEstoque(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Catalogo SemEstoque")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	semEstoque, codigoSemEstoque := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Sem Estoque", CategoriaID: categoriaID,
	}, estoque.ID, 0)
	limparEstoqueDe(t, db, semEstoque)

	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Com Estoque", CategoriaID: categoriaID,
	}, estoque.ID, 7)

	itens, pag, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoGrade: %v", err)
	}
	if pag.Total != 2 || len(itens) != 2 {
		t.Fatalf("total = %d, len = %d, want 2/2", pag.Total, len(itens))
	}
	// Ordenado por nome: "Com Estoque" < "Sem Estoque".
	com, sem := itens[0], itens[1]
	if com.Nome != "Com Estoque" || com.QuantidadeTotal != 7 || !com.Disponivel {
		t.Errorf("com estoque = %+v, want qtd 7 disponivel true", com)
	}
	if sem.Nome != "Sem Estoque" || sem.QuantidadeTotal != 0 || sem.Disponivel {
		t.Errorf("sem estoque = %+v, want qtd 0 disponivel false", sem)
	}
	if sem.Codigo == nil || *sem.Codigo != codigoSemEstoque {
		t.Errorf("codigo = %v, want %q", sem.Codigo, codigoSemEstoque)
	}
}

// TestListarCatalogoGrade_QuantidadeSomadaEDimensoes prova que quantidadeTotal
// soma TODAS as linhas `produto_estoque` do Produto e que as 5 dimensões
// pareadas voltam na projeção (par NULL -> nil).
func TestListarCatalogoGrade_QuantidadeSomadaEDimensoes(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoqueA, err := CriarEstoque(db, empresaTeste, "Estoque A")
	if err != nil {
		t.Fatalf("seed CriarEstoque A: %v", err)
	}
	estoqueB, err := CriarEstoque(db, empresaTeste, "Estoque B")
	if err != nil {
		t.Fatalf("seed CriarEstoque B: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	id, _ := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Tubo PVC 100mm", CategoriaID: categoriaID,

		Comprimento: &DimensaoInput{Valor: ptrFloat(6), Unidade: ptrStr("m")},
	}, estoqueA.ID, 10)
	setQuantidade(t, db, id, estoqueB.ID, 5)

	itens, _, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoGrade: %v", err)
	}
	if len(itens) != 1 {
		t.Fatalf("len = %d, want 1", len(itens))
	}
	it := itens[0]
	if it.QuantidadeTotal != 15 || !it.Disponivel {
		t.Errorf("quantidadeTotal = %v (disponivel %v), want 15 / true", it.QuantidadeTotal, it.Disponivel)
	}
	if it.Dimensoes.Comprimento == nil || it.Dimensoes.Comprimento.Valor != 6 || it.Dimensoes.Comprimento.Unidade != "m" {
		t.Errorf("comprimento = %+v, want {6 m}", it.Dimensoes.Comprimento)
	}
	if it.Dimensoes.Largura != nil || it.Dimensoes.Diametro != nil || it.Dimensoes.Altura != nil || it.Dimensoes.Espessura != nil {
		t.Errorf("dimensões não informadas deveriam ser nil, got %+v", it.Dimensoes)
	}
}

// TestListarCatalogoGrade_PaginaAlemDaUltima prova a linha "Página além da
// última": pagina 99 -> lista vazia, paginacao.total ainda correto.
func TestListarCatalogoGrade_PaginaAlemDaUltima(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Catalogo AlemUltima")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Único no Catálogo", CategoriaID: categoriaID,
	}, estoque.ID, 1)

	itens, pag, err := ListarCatalogoGrade(db, 99, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoGrade(99): %v", err)
	}
	if itens == nil {
		t.Error("itens = nil, want slice vazio não-nil")
	}
	if len(itens) != 0 {
		t.Errorf("len = %d, want 0", len(itens))
	}
	if pag.Total != 1 || pag.TotalPaginas != 1 || pag.Pagina != 99 {
		t.Errorf("paginacao = %+v, want total 1 / totalPaginas 1 / pagina 99", pag)
	}
}

// TestListarCatalogoGrade_CatalogoVazio prova a linha "Catálogo vazio":
// 0 Produtos -> lista vazia (não-nil), paginacao zerada.
func TestListarCatalogoGrade_CatalogoVazio(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	itens, pag, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoGrade: %v", err)
	}
	if itens == nil || len(itens) != 0 {
		t.Errorf("itens = %v, want slice vazio não-nil", itens)
	}
	if pag != (Paginacao{Pagina: 1, Tamanho: 24, Total: 0, TotalPaginas: 0}) {
		t.Errorf("paginacao = %+v, want {1 24 0 0}", pag)
	}
}

// TestListarCatalogoAgrupado_AgrupaPorNomeEDimensoes prova a linha "Tabela
// agrupa por nome+dimensões": 3 Produtos "Parafuso" mesmas dimensões,
// quantidades espalhadas por 2 Estoques -> 1 grupo, quantidadeTotal = soma,
// porEstoque com a soma por Estoque, ordenado por estoqueNome ASC.
func TestListarCatalogoAgrupado_AgrupaPorNomeEDimensoes(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estAlmox, err := CriarEstoque(db, empresaTeste, "Almoxarifado Central")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	estObra, err := CriarEstoque(db, empresaTeste, "Obra Norte")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	dim := func() *DimensaoInput { return &DimensaoInput{Valor: ptrFloat(8), Unidade: ptrStr("mm")} }

	// p1: 10 em Almoxarifado. p2: 5 em Almoxarifado + 2 em Obra. p3: sem estoque.
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Parafuso Grupo", CategoriaID: categoriaID,
		Diametro: dim(),
	}, estAlmox.ID, 10)
	p2, _ := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Parafuso Grupo", CategoriaID: categoriaID,
		Diametro: dim(),
	}, estAlmox.ID, 5)
	setQuantidade(t, db, p2, estObra.ID, 2)
	p3, _ := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Parafuso Grupo", CategoriaID: categoriaID,
		Diametro: dim(),
	}, estAlmox.ID, 0)
	limparEstoqueDe(t, db, p3)

	grupos, pag, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado: %v", err)
	}
	if pag.Total != 1 || len(grupos) != 1 {
		t.Fatalf("total = %d, len(grupos) = %d, want 1/1", pag.Total, len(grupos))
	}
	g := grupos[0]
	if g.Nome != "Parafuso Grupo" {
		t.Errorf("nome = %q, want Parafuso Grupo", g.Nome)
	}
	if g.Chave == "" {
		t.Error("chave vazia")
	}
	if g.QuantidadeTotal != 17 || !g.Disponivel {
		t.Errorf("quantidadeTotal = %v (disponivel %v), want 17 / true", g.QuantidadeTotal, g.Disponivel)
	}
	if g.Dimensoes.Diametro == nil || g.Dimensoes.Diametro.Valor != 8 || g.Dimensoes.Diametro.Unidade != "mm" {
		t.Errorf("diametro = %+v, want {8 mm}", g.Dimensoes.Diametro)
	}
	if len(g.PorEstoque) != 2 {
		t.Fatalf("porEstoque len = %d, want 2 (%+v)", len(g.PorEstoque), g.PorEstoque)
	}
	// Ordenado por estoqueNome ASC: "Almoxarifado Central" (15) antes de "Obra Norte" (2).
	if g.PorEstoque[0].EstoqueNome != "Almoxarifado Central" || g.PorEstoque[0].Quantidade != 15 {
		t.Errorf("porEstoque[0] = %+v, want {Almoxarifado Central 15}", g.PorEstoque[0])
	}
	if g.PorEstoque[1].EstoqueNome != "Obra Norte" || g.PorEstoque[1].Quantidade != 2 {
		t.Errorf("porEstoque[1] = %+v, want {Obra Norte 2}", g.PorEstoque[1])
	}
}

// TestListarCatalogoAgrupado_DimensoesDistintas prova a linha "Tabela,
// dimensões distintas": 2 Produtos "Parafuso" com comprimentos diferentes ->
// 2 grupos separados.
func TestListarCatalogoAgrupado_DimensoesDistintas(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Dimensoes Distintas")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Parafuso Longo", CategoriaID: categoriaID,
		Comprimento: &DimensaoInput{Valor: ptrFloat(20), Unidade: ptrStr("mm")},
	}, estoque.ID, 3)
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Parafuso Longo", CategoriaID: categoriaID,
		Comprimento: &DimensaoInput{Valor: ptrFloat(30), Unidade: ptrStr("mm")},
	}, estoque.ID, 4)

	grupos, pag, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado: %v", err)
	}
	if pag.Total != 2 || len(grupos) != 2 {
		t.Fatalf("total = %d, len(grupos) = %d, want 2/2", pag.Total, len(grupos))
	}
	if grupos[0].Chave == grupos[1].Chave {
		t.Error("os 2 grupos têm a mesma chave — dimensões distintas deveriam separar")
	}
	// ORDER BY comprimento_valor ASC: 20 antes de 30.
	if grupos[0].Dimensoes.Comprimento.Valor != 20 || grupos[1].Dimensoes.Comprimento.Valor != 30 {
		t.Errorf("ordem por dimensão = [%v, %v], want [20, 30]",
			grupos[0].Dimensoes.Comprimento.Valor, grupos[1].Dimensoes.Comprimento.Valor)
	}
}

// TestListarCatalogoAgrupado_DimensoesTodasNulas prova a linha "Tabela,
// dimensões todas nulas": 2 Produtos "Cimento" sem nenhuma dimensão agrupam
// num único grupo (NULL = NULL).
func TestListarCatalogoAgrupado_DimensoesTodasNulas(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Dimensoes Nulas")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Cimento Padrão", CategoriaID: categoriaID,
	}, estoque.ID, 100)
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Cimento Padrão", CategoriaID: categoriaID,
	}, estoque.ID, 50)

	grupos, pag, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado: %v", err)
	}
	if pag.Total != 1 || len(grupos) != 1 {
		t.Fatalf("total = %d, len(grupos) = %d, want 1/1 (NULL agrupa com NULL)", pag.Total, len(grupos))
	}
	g := grupos[0]
	if g.QuantidadeTotal != 150 {
		t.Errorf("quantidadeTotal = %v, want 150", g.QuantidadeTotal)
	}
	if g.Dimensoes.Comprimento != nil || g.Dimensoes.Largura != nil || g.Dimensoes.Diametro != nil ||
		g.Dimensoes.Altura != nil || g.Dimensoes.Espessura != nil {
		t.Errorf("todas as dimensões deveriam ser nil, got %+v", g.Dimensoes)
	}
}

// TestListarCatalogoAgrupado_GrupoSemLinhasDeEstoque prova a linha "Grupo sem
// linhas de estoque": grupo cujos Produtos não têm `produto_estoque` ->
// quantidadeTotal 0, disponivel false, porEstoque = [] (não-nil).
func TestListarCatalogoAgrupado_GrupoSemLinhasDeEstoque(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Grupo Sem Estoque")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	a, _ := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Prego Comum", CategoriaID: categoriaID,
	}, estoque.ID, 0)
	b, _ := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Prego Comum", CategoriaID: categoriaID,
	}, estoque.ID, 0)
	limparEstoqueDe(t, db, a)
	limparEstoqueDe(t, db, b)

	grupos, pag, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado: %v", err)
	}
	if pag.Total != 1 || len(grupos) != 1 {
		t.Fatalf("total = %d, len(grupos) = %d, want 1/1", pag.Total, len(grupos))
	}
	g := grupos[0]
	if g.QuantidadeTotal != 0 || g.Disponivel {
		t.Errorf("quantidadeTotal = %v (disponivel %v), want 0 / false", g.QuantidadeTotal, g.Disponivel)
	}
	if g.PorEstoque == nil {
		t.Error("porEstoque = nil, want [] não-nil")
	}
	if len(g.PorEstoque) != 0 {
		t.Errorf("porEstoque = %+v, want []", g.PorEstoque)
	}
}

// TestListarCatalogoAgrupado_PaginacaoSobreGrupos prova que a paginação de
// `agrupar=true` conta e recorta GRUPOS (não Produtos): 26 grupos de 1
// Produto -> página 1 traz 24, página 2 traz 2, paginacao.total = 26.
func TestListarCatalogoAgrupado_PaginacaoSobreGrupos(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Paginacao Grupos")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	for i := 0; i < 26; i++ {
		criarProdutoCatComSaldo(t, db, CriarProdutoInput{
			UnidadeMedida: "un",
			Nome:          fmt.Sprintf("Item Catalogo %02d", i),
			CategoriaID:   categoriaID,
		}, estoque.ID, 1)
	}

	g1, pag1, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado(1): %v", err)
	}
	if len(g1) != 24 || pag1.Total != 26 || pag1.TotalPaginas != 2 {
		t.Fatalf("página 1: len = %d, total = %d, totalPaginas = %d, want 24/26/2", len(g1), pag1.Total, pag1.TotalPaginas)
	}
	if g1[0].Nome != "Item Catalogo 00" || g1[23].Nome != "Item Catalogo 23" {
		t.Errorf("página 1 ordem = [%q .. %q]", g1[0].Nome, g1[23].Nome)
	}

	g2, pag2, err := ListarCatalogoAgrupado(db, 2, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado(2): %v", err)
	}
	if len(g2) != 2 || pag2.Total != 26 {
		t.Fatalf("página 2: len = %d, total = %d, want 2/26", len(g2), pag2.Total)
	}
	if g2[0].Nome != "Item Catalogo 24" || g2[1].Nome != "Item Catalogo 25" {
		t.Errorf("página 2 ordem = [%q, %q], want [Item Catalogo 24, Item Catalogo 25]", g2[0].Nome, g2[1].Nome)
	}
}

// TestListarCatalogoAgrupado_PaginaAlemDaUltima prova a linha "Página além da
// última" da matriz para o modo agrupado: pagina 99 -> `grupos` vazio (nunca
// nil, para serializar como `[]`), paginacao.Total/TotalPaginas ainda
// corretos. Também exercita o ramo em que `preencherPorEstoque` é pulado
// (`len(todosIDs) == 0`).
func TestListarCatalogoAgrupado_PaginaAlemDaUltima(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Agrupado AlemUltima")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	for _, nome := range []string{"Arruela Grande", "Bucha Grande", "Cano Comprido"} {
		criarProdutoCatComSaldo(t, db, CriarProdutoInput{
			UnidadeMedida: "un",
			Nome:          nome, CategoriaID: categoriaID,
		}, estoque.ID, 1)
	}

	grupos, pag, err := ListarCatalogoAgrupado(db, 99, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado(99): %v", err)
	}
	if grupos == nil {
		t.Error("grupos = nil, want slice vazio não-nil")
	}
	if len(grupos) != 0 {
		t.Errorf("len(grupos) = %d, want 0", len(grupos))
	}
	if pag.Total != 3 || pag.TotalPaginas != 1 || pag.Pagina != 99 {
		t.Errorf("paginacao = %+v, want total 3 / totalPaginas 1 / pagina 99", pag)
	}
}

// TestListarCatalogoAgrupado_NomeIgualDimensaoParcialSepara é uma checagem de
// sanidade extra: mesmo nome, mas com uma dimensão preenchida vs. totalmente
// nula -> grupos distintos (NULL não agrupa com um valor).
func TestListarCatalogoAgrupado_NomeIgualDimensaoParcialSepara(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Dim Parcial")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Cano Comprido", CategoriaID: categoriaID,
		Comprimento: &DimensaoInput{Valor: ptrFloat(6), Unidade: ptrStr("m")},
	}, estoque.ID, 1)
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Cano Comprido", CategoriaID: categoriaID,
	}, estoque.ID, 1)

	grupos, pag, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado: %v", err)
	}
	if pag.Total != 2 || len(grupos) != 2 {
		t.Fatalf("total = %d, len(grupos) = %d, want 2/2", pag.Total, len(grupos))
	}
}

// --- Story 4.4: Detalhe do produto por Estoque com atualização em tempo
// real -----------------------------------------------------------------

// TestObterProdutoDetalhe_ComEstoqueDiscriminado prova a linha "Detalhe de
// Produto existente" da matriz: `porEstoque` discrimina a quantidade por
// Estoque, ordenado por `estoqueNome ASC, estoqueId ASC`.
func TestObterProdutoDetalhe_ComEstoqueDiscriminado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoqueA, err := CriarEstoque(db, empresaTeste, "Zebra Detalhe")
	if err != nil {
		t.Fatalf("seed CriarEstoque A: %v", err)
	}
	estoqueB, err := CriarEstoque(db, empresaTeste, "Alfa Detalhe")
	if err != nil {
		t.Fatalf("seed CriarEstoque B: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	produtoID, codigoGerado := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Embalagem:     "Caixa com 10",
		Nome:          "Produto Detalhe",
		CategoriaID:   categoriaID,
		Comprimento:   &DimensaoInput{Valor: ptrFloat(6), Unidade: ptrStr("m")},
	}, estoqueA.ID, 5)
	setQuantidade(t, db, produtoID, estoqueB.ID, 3)

	det, err := ObterProdutoDetalhe(db, empresaTeste, produtoID)
	if err != nil {
		t.Fatalf("ObterProdutoDetalhe: %v", err)
	}
	if det.ID != produtoID || det.Nome != "Produto Detalhe" {
		t.Fatalf("produto = %+v", det)
	}
	if det.Codigo == nil || *det.Codigo != codigoGerado {
		t.Errorf("codigo = %v, want %q", det.Codigo, codigoGerado)
	}
	// Story 10.3, spec-10-3: `unidadeMedida`/`embalagem` como campos próprios
	// do detalhe (AC5).
	if det.UnidadeMedida == nil || *det.UnidadeMedida != "un" {
		t.Errorf("unidadeMedida = %v, want %q", det.UnidadeMedida, "un")
	}
	if det.Embalagem == nil || *det.Embalagem != "Caixa com 10" {
		t.Errorf("embalagem = %v, want %q", det.Embalagem, "Caixa com 10")
	}
	if det.Dimensoes.Comprimento == nil || det.Dimensoes.Comprimento.Valor != 6 {
		t.Errorf("dimensoes.comprimento = %+v", det.Dimensoes.Comprimento)
	}
	if det.QuantidadeTotal != 8 || !det.Disponivel {
		t.Errorf("quantidadeTotal = %v, disponivel = %v, want 8/true", det.QuantidadeTotal, det.Disponivel)
	}
	if len(det.PorEstoque) != 2 {
		t.Fatalf("len(porEstoque) = %d, want 2", len(det.PorEstoque))
	}
	// "Alfa Detalhe" < "Zebra Detalhe" — ordem alfabética por estoqueNome,
	// mesmo critério de preencherPorEstoque.
	if det.PorEstoque[0].EstoqueNome != "Alfa Detalhe" || det.PorEstoque[0].Quantidade != 3 {
		t.Errorf("porEstoque[0] = %+v, want Alfa Detalhe/3", det.PorEstoque[0])
	}
	if det.PorEstoque[1].EstoqueNome != "Zebra Detalhe" || det.PorEstoque[1].Quantidade != 5 {
		t.Errorf("porEstoque[1] = %+v, want Zebra Detalhe/5", det.PorEstoque[1])
	}
}

// TestObterProdutoDetalhe_SemEstoque prova a linha "Produto sem nenhum
// produto_estoque" da matriz: `quantidadeTotal:0`, `disponivel:false`,
// `porEstoque:[]` (nunca nil).
func TestObterProdutoDetalhe_SemEstoque(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Detalhe Sem Estoque")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	produtoID, codigoGerado := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Produto Sem Estoque",
		CategoriaID:   categoriaID,
	}, estoque.ID, 1)
	limparEstoqueDe(t, db, produtoID)

	det, err := ObterProdutoDetalhe(db, empresaTeste, produtoID)
	if err != nil {
		t.Fatalf("ObterProdutoDetalhe: %v", err)
	}
	if det.QuantidadeTotal != 0 || det.Disponivel {
		t.Errorf("quantidadeTotal = %v, disponivel = %v, want 0/false", det.QuantidadeTotal, det.Disponivel)
	}
	if det.PorEstoque == nil {
		t.Error("porEstoque = nil, want slice vazio não-nil")
	}
	if len(det.PorEstoque) != 0 {
		t.Errorf("len(porEstoque) = %d, want 0", len(det.PorEstoque))
	}
	// Story 10.2 (spec-10-2): `codigo` é sempre gerado pelo servidor agora —
	// não existe mais "Produto sem código" entre os criados por CriarProduto.
	if det.Codigo == nil || *det.Codigo != codigoGerado {
		t.Errorf("codigo = %v, want %q", det.Codigo, codigoGerado)
	}
}

// TestObterProdutoDetalhe_SemUnidadeMedidaNemEmbalagem prova a linha
// "Detalhe de Produto criado via importação (sem unidade/embalagem)" da
// matriz da spec-10-3: um Produto com `unidade_medida`/`embalagem` NULL no
// banco (simulando o caminho de importação, Never desta spec — a importação
// em massa nunca preenche estas colunas) devolve os dois ponteiros `nil`
// (`null` no JSON) — nunca "un" forçado.
func TestObterProdutoDetalhe_SemUnidadeMedidaNemEmbalagem(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Detalhe Sem Unidade")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	produtoID, _ := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Produto Importado Sem Unidade",
		CategoriaID:   categoriaID,
	}, estoque.ID, 1)
	// Simula o INSERT de importação em massa (services/importacoes.go), que
	// nunca preenche `unidade_medida`/`embalagem` (Never, spec-10-3) — via SQL
	// direto, já que CriarProduto sempre exige `unidade_medida`.
	if _, err := db.Exec(
		`UPDATE produtos SET unidade_medida = NULL, embalagem = NULL WHERE id = $1`, produtoID,
	); err != nil {
		t.Fatalf("falha ao simular produto importado: %v", err)
	}

	det, err := ObterProdutoDetalhe(db, empresaTeste, produtoID)
	if err != nil {
		t.Fatalf("ObterProdutoDetalhe: %v", err)
	}
	if det.UnidadeMedida != nil {
		t.Errorf("unidadeMedida = %v, want nil", *det.UnidadeMedida)
	}
	if det.Embalagem != nil {
		t.Errorf("embalagem = %v, want nil", *det.Embalagem)
	}
}

// TestObterProdutoDetalhe_IDInexistente prova a linha "id inexistente" da
// matriz: um UUID sintaticamente válido mas sem linha correspondente ->
// ErrProdutoNaoEncontrado.
func TestObterProdutoDetalhe_IDInexistente(t *testing.T) {
	db := testDB(t)
	_, err := ObterProdutoDetalhe(db, empresaTeste, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrProdutoNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrProdutoNaoEncontrado", err)
	}
}

// TestObterProdutoDetalhe_IDMalformado prova a linha "id malformado" da
// matriz: uma string que não é UUID (SQLSTATE 22P02) colapsa no MESMO
// ErrProdutoNaoEncontrado do id inexistente.
func TestObterProdutoDetalhe_IDMalformado(t *testing.T) {
	db := testDB(t)
	_, err := ObterProdutoDetalhe(db, empresaTeste, "isto-nao-e-um-uuid")
	if !errors.Is(err, ErrProdutoNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrProdutoNaoEncontrado", err)
	}
}

// --- Story 4.2: Filtros por categoria, estoque e disponibilidade ----------

// TestListarCatalogoGrade_FiltroCategoria prova a linha "Filtro por categoria
// isolado" da matriz: só Produtos da categoria filtrada aparecem.
func TestListarCatalogoGrade_FiltroCategoria(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Filtro Categoria")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	civil := categoriaIDPorCodigo(t, db, "04.001")
	eletrico := categoriaIDPorCodigo(t, db, "04.002")

	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Cimento Portland", CategoriaID: civil,
	}, estoque.ID, 1)
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Cabo Flexível", CategoriaID: eletrico,
	}, estoque.ID, 1)

	itens, pag, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, CategoriaID: civil})
	if err != nil {
		t.Fatalf("ListarCatalogoGrade: %v", err)
	}
	if pag.Total != 1 || len(itens) != 1 {
		t.Fatalf("total = %d, len = %d, want 1/1", pag.Total, len(itens))
	}
	if itens[0].Nome != "Cimento Portland" {
		t.Errorf("nome = %q, want Cimento", itens[0].Nome)
	}
}

// TestListarCatalogoGrade_FiltroEstoque_LinhaComQuantidadeZero prova a linha
// "Filtro por Estoque isolado" da matriz: uma linha `produto_estoque` com
// quantidade 0 ainda casa o filtro — presença de linha, não de saldo.
func TestListarCatalogoGrade_FiltroEstoque_LinhaComQuantidadeZero(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estA, err := CriarEstoque(db, empresaTeste, "Estoque Filtro A")
	if err != nil {
		t.Fatalf("seed CriarEstoque A: %v", err)
	}
	estB, err := CriarEstoque(db, empresaTeste, "Estoque Filtro B")
	if err != nil {
		t.Fatalf("seed CriarEstoque B: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	p1, _ := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Zerado em A", CategoriaID: categoriaID,
	}, estA.ID, 0)
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Só em Estoque B", CategoriaID: categoriaID,
	}, estB.ID, 5)

	itens, pag, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, EstoqueID: estA.ID})
	if err != nil {
		t.Fatalf("ListarCatalogoGrade: %v", err)
	}
	if pag.Total != 1 || len(itens) != 1 {
		t.Fatalf("total = %d, len = %d, want 1/1", pag.Total, len(itens))
	}
	if itens[0].ID != p1 || itens[0].QuantidadeTotal != 0 {
		t.Errorf("item = %+v, want id %s / quantidadeTotal 0", itens[0], p1)
	}
}

// TestListarCatalogoGrade_FiltroComEstoque prova a linha "Filtro 'Com
// estoque' isolado" da matriz para `true`, e a simetria de teste para
// `false` (Always: backend suporta `comEstoque=false`, mesmo a UI só
// expondo a versão "on" do checkbox).
func TestListarCatalogoGrade_FiltroComEstoque(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Com Estoque")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Disponível", CategoriaID: categoriaID,
	}, estoque.ID, 3)
	semEstoque, _ := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Zerado Estoque", CategoriaID: categoriaID,
	}, estoque.ID, 0)

	comEstoqueTrue := true
	itens, pag, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, ComEstoque: &comEstoqueTrue})
	if err != nil {
		t.Fatalf("ListarCatalogoGrade(true): %v", err)
	}
	if pag.Total != 1 || len(itens) != 1 || itens[0].Nome != "Disponível" {
		t.Fatalf("comEstoque=true -> itens = %+v, total = %d, want só 'Disponível'", itens, pag.Total)
	}

	comEstoqueFalse := false
	itens2, pag2, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, ComEstoque: &comEstoqueFalse})
	if err != nil {
		t.Fatalf("ListarCatalogoGrade(false): %v", err)
	}
	if pag2.Total != 1 || len(itens2) != 1 || itens2[0].ID != semEstoque {
		t.Fatalf("comEstoque=false -> itens = %+v, total = %d, want só 'Zerado'", itens2, pag2.Total)
	}
}

// TestListarCatalogoGrade_TodosOsFiltrosComQCombinados prova a linha "Todos
// os filtros + q combinados" da matriz: só o Produto que satisfaz as 4
// condições simultaneamente (E lógico) sobrevive, mesmo com outros Produtos
// quase-correspondentes (cada um falhando em exatamente 1 dos 4 critérios).
func TestListarCatalogoGrade_TodosOsFiltrosComQCombinados(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estAlvo, err := CriarEstoque(db, empresaTeste, "Estoque Alvo Combinado")
	if err != nil {
		t.Fatalf("seed CriarEstoque alvo: %v", err)
	}
	estOutro, err := CriarEstoque(db, empresaTeste, "Estoque Outro Combinado")
	if err != nil {
		t.Fatalf("seed CriarEstoque outro: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	outraCategoria := categoriaIDPorCodigo(t, db, "04.002")

	// Único produto que casa TODOS os 4 filtros.
	alvo, _ := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Parafuso Sextavado", CategoriaID: categoriaID,
	}, estAlvo.ID, 10)
	// Casa q/estoque/comEstoque, falha na categoria.
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Parafuso Allen", CategoriaID: outraCategoria,
	}, estAlvo.ID, 10)
	// Casa q/categoria/comEstoque, falha no estoque.
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Parafuso Philips", CategoriaID: categoriaID,
	}, estOutro.ID, 10)
	// Casa q/categoria/estoque, falha em comEstoque (zerado em todo lugar).
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Parafuso Fenda", CategoriaID: categoriaID,
	}, estAlvo.ID, 0)
	// Casa categoria/estoque/comEstoque, falha no termo de busca.
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Arruela Lisa", CategoriaID: categoriaID,
	}, estAlvo.ID, 10)

	comEstoque := true
	itens, pag, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste,
		Q: "paraf", CategoriaID: categoriaID, EstoqueID: estAlvo.ID, ComEstoque: &comEstoque,
	})
	if err != nil {
		t.Fatalf("ListarCatalogoGrade: %v", err)
	}
	if pag.Total != 1 || len(itens) != 1 || itens[0].ID != alvo {
		t.Fatalf("itens = %+v, total = %d, want só %q (Parafuso Sextavado)", itens, pag.Total, alvo)
	}
}

// TestListarCatalogoGrade_EstoqueEComEstoqueSemSobreposicao prova a linha
// "Estoque + 'Com estoque' sem sobreposição" da matriz: `estoqueId` casa uma
// linha zerada NESSE Estoque; `comEstoque=true` casa a soma GLOBAL (> 0 em
// outro Estoque) — os dois são independentes, o Produto aparece.
func TestListarCatalogoGrade_EstoqueEComEstoqueSemSobreposicao(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estFiltrado, err := CriarEstoque(db, empresaTeste, "Estoque Filtrado SemSobrep")
	if err != nil {
		t.Fatalf("seed CriarEstoque filtrado: %v", err)
	}
	estOutro, err := CriarEstoque(db, empresaTeste, "Estoque Outro SemSobrep")
	if err != nil {
		t.Fatalf("seed CriarEstoque outro: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	produtoID, _ := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Disperso Multi", CategoriaID: categoriaID,
	}, estFiltrado.ID, 0)
	setQuantidade(t, db, produtoID, estOutro.ID, 5)

	comEstoque := true
	itens, pag, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, EstoqueID: estFiltrado.ID, ComEstoque: &comEstoque})
	if err != nil {
		t.Fatalf("ListarCatalogoGrade: %v", err)
	}
	if pag.Total != 1 || len(itens) != 1 || itens[0].ID != produtoID {
		t.Fatalf("itens = %+v, total = %d, want só %q", itens, pag.Total, produtoID)
	}
	if itens[0].QuantidadeTotal != 5 {
		t.Errorf("quantidadeTotal = %v, want 5 (soma global, comEstoque NUNCA escopado ao estoqueId filtrado)", itens[0].QuantidadeTotal)
	}
}

// TestListarCatalogoGrade_CategoriaEstoqueMalformadosColapsamEmZero prova a
// linha "categoriaId/estoqueId malformado (não-UUID)" da matriz: nunca um
// erro (nunca 500), sempre página vazia.
func TestListarCatalogoGrade_CategoriaEstoqueMalformadosColapsamEmZero(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Malformado Grade")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Qualquer Nome", CategoriaID: categoriaID,
	}, estoque.ID, 1)

	itens, pag, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, CategoriaID: "abc"})
	if err != nil {
		t.Fatalf("categoriaId malformado: err = %v, want nil (colapso em zero linhas)", err)
	}
	if itens == nil || len(itens) != 0 || pag.Total != 0 {
		t.Errorf("categoriaId malformado: itens = %v, total = %d, want [] / 0", itens, pag.Total)
	}

	itens2, pag2, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, EstoqueID: "xyz"})
	if err != nil {
		t.Fatalf("estoqueId malformado: err = %v, want nil (colapso em zero linhas)", err)
	}
	if itens2 == nil || len(itens2) != 0 || pag2.Total != 0 {
		t.Errorf("estoqueId malformado: itens = %v, total = %d, want [] / 0", itens2, pag2.Total)
	}
}

// TestListarCatalogoGrade_FiltroQBuscaPorCategoria prova que `q` também casa
// por `categorias.nome` na grade, mesmos 3 campos de BuscarProdutos (Story
// 4.1).
func TestListarCatalogoGrade_FiltroQBuscaPorCategoria(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Q Categoria Grade")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	eletrico := categoriaIDPorCodigo(t, db, "04.002") // "Materiais Elétricos"
	civil := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Disjuntor Bipolar", CategoriaID: eletrico,
	}, estoque.ID, 1)
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Cimento Comum", CategoriaID: civil,
	}, estoque.ID, 1)

	itens, pag, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, Q: "Elétric"})
	if err != nil {
		t.Fatalf("ListarCatalogoGrade: %v", err)
	}
	if pag.Total != 1 || len(itens) != 1 || itens[0].Nome != "Disjuntor Bipolar" {
		t.Fatalf("itens = %+v, total = %d, want só 'Disjuntor' (match por categorias.nome)", itens, pag.Total)
	}
}

// TestListarCatalogoGrade_PaginacaoSobreConjuntoFiltrado prova que a
// paginação pagina sobre o CONJUNTO JÁ FILTRADO, não sobre o total sem
// filtro: 30 Produtos casam o filtro de categoria + 5 não casam;
// `paginacao.total`/`totalPaginas` refletem só os 30, e a página 2 traz o
// recorte 25-30 do conjunto filtrado — nenhum Produto da categoria
// excluída aparece em nenhuma das duas páginas. Achado pelo Blind Hunter na
// revisão desta story (a numeração dinâmica de placeholders de
// `LIMIT`/`OFFSET` nunca era exercitada numa página >1 com filtro ativo).
func TestListarCatalogoGrade_PaginacaoSobreConjuntoFiltrado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Paginacao Filtrada")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	civil := categoriaIDPorCodigo(t, db, "04.001")
	eletrico := categoriaIDPorCodigo(t, db, "04.002")

	for i := 29; i >= 0; i-- {
		criarProdutoCatComSaldo(t, db, CriarProdutoInput{
			UnidadeMedida: "un",
			Nome:          fmt.Sprintf("Civil Produto %02d", i),
			CategoriaID:   civil,
		}, estoque.ID, 1)
	}
	for i := 0; i < 5; i++ {
		criarProdutoCatComSaldo(t, db, CriarProdutoInput{
			UnidadeMedida: "un",
			Nome:          fmt.Sprintf("Eletrico %02d", i),
			CategoriaID:   eletrico,
		}, estoque.ID, 1)
	}

	filtros := FiltrosCatalogo{EmpresaID: empresaTeste, CategoriaID: civil}

	pagina1, pag1, err := ListarCatalogoGrade(db, 1, filtros)
	if err != nil {
		t.Fatalf("ListarCatalogoGrade(1): %v", err)
	}
	if pag1 != (Paginacao{Pagina: 1, Tamanho: 24, Total: 30, TotalPaginas: 2}) {
		t.Fatalf("paginacao página 1 = %+v, want {1 24 30 2} (só os 30 'Civil', nunca os 5 'Eletrico')", pag1)
	}
	if len(pagina1) != 24 || pagina1[0].Nome != "Civil Produto 00" || pagina1[23].Nome != "Civil Produto 23" {
		t.Fatalf("página 1 = [%q .. %q] (len %d), want [Civil Produto 00 .. Civil Produto 23] (len 24)", pagina1[0].Nome, pagina1[23].Nome, len(pagina1))
	}

	pagina2, pag2, err := ListarCatalogoGrade(db, 2, filtros)
	if err != nil {
		t.Fatalf("ListarCatalogoGrade(2): %v", err)
	}
	if pag2 != (Paginacao{Pagina: 2, Tamanho: 24, Total: 30, TotalPaginas: 2}) {
		t.Fatalf("paginacao página 2 = %+v, want {2 24 30 2}", pag2)
	}
	if len(pagina2) != 6 || pagina2[0].Nome != "Civil Produto 24" || pagina2[5].Nome != "Civil Produto 29" {
		t.Fatalf("página 2 = [%q .. %q] (len %d), want [Civil Produto 24 .. Civil Produto 29] (len 6) — nunca um 'Eletrico'", pagina2[0].Nome, pagina2[5].Nome, len(pagina2))
	}
	for _, item := range append(append([]CatalogoItem{}, pagina1...), pagina2...) {
		if item.Categoria.ID != civil {
			t.Errorf("item %q com categoria %q vazou o filtro (want só %q)", item.Nome, item.Categoria.ID, civil)
		}
	}
}

// TestListarCatalogoAgrupado_FiltroParcialMostraSoQuemCasou prova a linha
// "agrupar=true com filtro que esvazia um grupo" da matriz: o grupo aparece
// só com a soma dos Produtos que casaram o filtro.
func TestListarCatalogoAgrupado_FiltroParcialMostraSoQuemCasou(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Grupo Filtro Parcial")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	civil := categoriaIDPorCodigo(t, db, "04.001")
	eletrico := categoriaIDPorCodigo(t, db, "04.002")

	// 2 Produtos "Parafuso", mesmo nome + dimensões nulas (agrupam juntos),
	// categorias diferentes.
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Parafuso Sextavado", CategoriaID: civil,
	}, estoque.ID, 10)
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Parafuso Sextavado", CategoriaID: eletrico,
	}, estoque.ID, 5)

	grupos, pag, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, CategoriaID: civil})
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado: %v", err)
	}
	if pag.Total != 1 || len(grupos) != 1 {
		t.Fatalf("total = %d, len = %d, want 1/1", pag.Total, len(grupos))
	}
	if grupos[0].QuantidadeTotal != 10 {
		t.Errorf("quantidadeTotal = %v, want 10 (só o produto que casou a categoria civil)", grupos[0].QuantidadeTotal)
	}
}

// TestListarCatalogoAgrupado_FiltroRemoveGrupoInteiro prova a linha
// "agrupar=true com filtro que remove o grupo inteiro" da matriz: grupo
// não aparece, `paginacao.total` não o conta.
func TestListarCatalogoAgrupado_FiltroRemoveGrupoInteiro(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Grupo Removido")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	civil := categoriaIDPorCodigo(t, db, "04.001")
	eletrico := categoriaIDPorCodigo(t, db, "04.002")

	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Cabo Flexível", CategoriaID: eletrico,
	}, estoque.ID, 10)

	grupos, pag, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, CategoriaID: civil})
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado: %v", err)
	}
	if grupos == nil || len(grupos) != 0 || pag.Total != 0 {
		t.Errorf("grupos = %v, total = %d, want [] / 0 (grupo inteiro removido pelo filtro)", grupos, pag.Total)
	}
}

// TestListarCatalogoAgrupado_FiltroQBuscaPorCategoria prova que `q` também
// casa por `categorias.nome` na tabela agrupada — exige o `JOIN categorias`
// que catalogoGrupoQuery/catalogoGrupoCountQuery ganharam nesta story (Code
// Map, spec-4-2).
func TestListarCatalogoAgrupado_FiltroQBuscaPorCategoria(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Grupo Q Categoria")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	eletrico := categoriaIDPorCodigo(t, db, "04.002")
	civil := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Disjuntor Bipolar", CategoriaID: eletrico,
	}, estoque.ID, 1)
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Cimento Comum", CategoriaID: civil,
	}, estoque.ID, 1)

	grupos, pag, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, Q: "Elétric"})
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado: %v", err)
	}
	if pag.Total != 1 || len(grupos) != 1 || grupos[0].Nome != "Disjuntor Bipolar" {
		t.Fatalf("grupos = %+v, total = %d, want só 'Disjuntor'", grupos, pag.Total)
	}
}

// TestListarCatalogoAgrupado_CategoriaEstoqueMalformadosColapsamEmZero prova
// a mesma linha da matriz que TestListarCatalogoGrade_..., para o modo
// agrupado.
func TestListarCatalogoAgrupado_CategoriaEstoqueMalformadosColapsamEmZero(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Malformado Agrupado")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Qualquer Nome", CategoriaID: categoriaID,
	}, estoque.ID, 1)

	grupos, pag, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, CategoriaID: "abc"})
	if err != nil {
		t.Fatalf("categoriaId malformado: err = %v, want nil", err)
	}
	if grupos == nil || len(grupos) != 0 || pag.Total != 0 {
		t.Errorf("categoriaId malformado: grupos = %v, total = %d, want [] / 0", grupos, pag.Total)
	}

	grupos2, pag2, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, EstoqueID: "xyz"})
	if err != nil {
		t.Fatalf("estoqueId malformado: err = %v, want nil", err)
	}
	if grupos2 == nil || len(grupos2) != 0 || pag2.Total != 0 {
		t.Errorf("estoqueId malformado: grupos = %v, total = %d, want [] / 0", grupos2, pag2.Total)
	}
}

// --- Story 4.6: Exportação da tabela do catálogo para Excel ---------------

// TestListarTodosGruposCatalogo_SemPaginacao prova a linha "sem paginação" da
// spec-4-6: mais grupos que TamanhoPaginaCatalogo (24) -> TODOS voltam numa
// única chamada, na mesma ordem (`nome ASC`) de ListarCatalogoAgrupado.
func TestListarTodosGruposCatalogo_SemPaginacao(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Exportar Sem Paginacao")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	const totalGrupos = TamanhoPaginaCatalogo + 5
	for i := totalGrupos - 1; i >= 0; i-- {
		criarProdutoCatComSaldo(t, db, CriarProdutoInput{
			UnidadeMedida: "un",
			Nome:          fmt.Sprintf("Exportar %02d", i),
			CategoriaID:   categoriaID,
		}, estoque.ID, 1)
	}

	grupos, err := ListarTodosGruposCatalogo(db, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarTodosGruposCatalogo: %v", err)
	}
	if len(grupos) != totalGrupos {
		t.Fatalf("len(grupos) = %d, want %d (nunca paginado)", len(grupos), totalGrupos)
	}
	if grupos[0].Nome != "Exportar 00" || grupos[totalGrupos-1].Nome != fmt.Sprintf("Exportar %02d", totalGrupos-1) {
		t.Errorf("ordem = [%q .. %q], want ordenado por nome ASC", grupos[0].Nome, grupos[totalGrupos-1].Nome)
	}
}

// TestListarTodosGruposCatalogo_FiltrosAplicados prova que os 4 filtros da
// Story 4.2 combinam por E lógico também nesta função — mesmo comportamento
// de ListarCatalogoAgrupado, sem a paginação.
func TestListarTodosGruposCatalogo_FiltrosAplicados(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoqueA, err := CriarEstoque(db, empresaTeste, "Canteiro Exportar Filtro A")
	if err != nil {
		t.Fatalf("seed CriarEstoque A: %v", err)
	}
	estoqueB, err := CriarEstoque(db, empresaTeste, "Canteiro Exportar Filtro B")
	if err != nil {
		t.Fatalf("seed CriarEstoque B: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Casa Filtro", CategoriaID: categoriaID,
	}, estoqueA.ID, 3)
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Fora Filtro", CategoriaID: categoriaID,
	}, estoqueB.ID, 3)

	grupos, err := ListarTodosGruposCatalogo(db, FiltrosCatalogo{EmpresaID: empresaTeste, Q: "Casa", EstoqueID: estoqueA.ID})
	if err != nil {
		t.Fatalf("ListarTodosGruposCatalogo com filtros: %v", err)
	}
	if len(grupos) != 1 || grupos[0].Nome != "Casa Filtro" {
		t.Fatalf("grupos = %+v, want só [Casa Filtro]", grupos)
	}
}

// TestListarTodosGruposCatalogo_IDMalformadoColapsaEmVazio prova o mesmo
// colapso `filtroUUIDInvalido` de ListarCatalogoAgrupado: `categoriaId`/
// `estoqueId` não-UUID -> slice vazio, nunca erro.
func TestListarTodosGruposCatalogo_IDMalformadoColapsaEmVazio(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Exportar Malformado")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Qualquer Nome", CategoriaID: categoriaID,
	}, estoque.ID, 1)

	grupos, err := ListarTodosGruposCatalogo(db, FiltrosCatalogo{EmpresaID: empresaTeste, CategoriaID: "abc"})
	if err != nil {
		t.Fatalf("categoriaId malformado: err = %v, want nil", err)
	}
	if grupos == nil || len(grupos) != 0 {
		t.Errorf("categoriaId malformado: grupos = %v, want [] não-nil", grupos)
	}

	grupos2, err := ListarTodosGruposCatalogo(db, FiltrosCatalogo{EmpresaID: empresaTeste, EstoqueID: "xyz"})
	if err != nil {
		t.Fatalf("estoqueId malformado: err = %v, want nil", err)
	}
	if grupos2 == nil || len(grupos2) != 0 {
		t.Errorf("estoqueId malformado: grupos = %v, want [] não-nil", grupos2)
	}
}

// --- Story 10.4: Colunas explícitas na listagem do Catálogo ---------------

// strVal desreferencia um *string para mensagens de erro ("<nil>" quando nil).
func strVal(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// TestListarCatalogoGrade_UnidadeEEmbalagem prova as linhas "Grade" da matriz
// da spec-10-4: unidadeMedida/embalagem preenchidos, e `nil` quando o Produto
// não tem (ex. importado).
func TestListarCatalogoGrade_UnidadeEEmbalagem(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Grade Unidade")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		Nome: "Produto Completo Grade", CategoriaID: categoriaID,
		UnidadeMedida: "un", Embalagem: "Cx c/ 12",
	}, estoque.ID, 15)
	semID, _ := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		Nome: "Produto Sem Unidade Grade", CategoriaID: categoriaID,
		UnidadeMedida: "un",
	}, estoque.ID, 1)
	if _, err := db.Exec(`UPDATE produtos SET unidade_medida = NULL, embalagem = NULL WHERE id = $1`, semID); err != nil {
		t.Fatalf("seed UPDATE: %v", err)
	}

	itens, _, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoGrade: %v", err)
	}
	if len(itens) != 2 {
		t.Fatalf("len = %d, want 2", len(itens))
	}
	for _, it := range itens {
		switch it.Nome {
		case "Produto Completo Grade":
			if it.UnidadeMedida == nil || *it.UnidadeMedida != "un" {
				t.Errorf("unidadeMedida = %s, want un", strVal(it.UnidadeMedida))
			}
			if it.Embalagem == nil || *it.Embalagem != "Cx c/ 12" {
				t.Errorf("embalagem = %s, want Cx c/ 12", strVal(it.Embalagem))
			}
			if it.QuantidadeTotal != 15 {
				t.Errorf("quantidadeTotal = %v, want 15", it.QuantidadeTotal)
			}
		case "Produto Sem Unidade Grade":
			if it.UnidadeMedida != nil || it.Embalagem != nil {
				t.Errorf("unidadeMedida/embalagem = %s/%s, want nil/nil", strVal(it.UnidadeMedida), strVal(it.Embalagem))
			}
		default:
			t.Errorf("nome inesperado %q", it.Nome)
		}
	}
}

// TestListarCatalogoAgrupado_ColunasComunsEMultiplos cobre a matriz da
// spec-10-4 para a tabela agrupada: grupo homogêneo, unitário e divergências
// de código (incluindo um NULL), categoria e par embalagem+unidade.
func TestListarCatalogoAgrupado_ColunasComunsEMultiplos(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Agrupado Colunas")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	cat1 := categoriaIDPorCodigo(t, db, "04.001")
	cat2 := categoriaIDPorCodigo(t, db, "04.002")

	novo := func(nome, categoriaID, unidade, embalagem string) string {
		id, _ := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
			Nome: nome, CategoriaID: categoriaID,
			UnidadeMedida: unidade, Embalagem: embalagem,
		}, estoque.ID, 1)
		return id
	}
	// Códigos são gerados sequencialmente pelo servidor: forçamos valores
	// iguais/diferentes via UPDATE para exercitar os casos de concordância.
	setCodigo := func(id string, codigo *string) {
		if _, err := db.Exec(`UPDATE produtos SET codigo = $1 WHERE id = $2`, codigo, id); err != nil {
			t.Fatalf("setCodigo: %v", err)
		}
	}
	cod := func(s string) *string { return &s }

	// Homogêneo: mesmo código/categoria/embalagem+unidade.
	h1 := novo("Grupo Homogeneo Um", cat1, "cx", "Caixa c/ 12")
	h2 := novo("Grupo Homogeneo Um", cat1, "cx", "Caixa c/ 12")
	// `codigo` é único por Empresa: dois Produtos do mesmo grupo só
	// "concordam" em código quando ambos são NULL (ex. importados).
	setCodigo(h1, nil)
	setCodigo(h2, nil)

	// Unitário.
	u1 := novo("Grupo Unitario Um", cat2, "un", "")
	setCodigo(u1, cod("UNI-1"))

	// Código divergente (um NULL).
	c1 := novo("Grupo Codigo Divergente", cat1, "un", "")
	c2 := novo("Grupo Codigo Divergente", cat1, "un", "")
	setCodigo(c1, cod("COD-1"))
	setCodigo(c2, nil)

	// Categoria divergente.
	k1 := novo("Grupo Categoria Divergente", cat1, "un", "")
	k2 := novo("Grupo Categoria Divergente", cat2, "un", "")
	setCodigo(k1, nil)
	setCodigo(k2, nil)

	// Par embalagem+unidade divergente (só a embalagem difere).
	e1 := novo("Grupo Embalagem Divergente", cat1, "un", "Caixa c/ 12")
	e2 := novo("Grupo Embalagem Divergente", cat1, "un", "Caixa c/ 24")
	setCodigo(e1, nil)
	setCodigo(e2, nil)

	// Só a unidade difere.
	m1 := novo("Grupo Unidade Divergente", cat1, "un", "Caixa")
	m2 := novo("Grupo Unidade Divergente", cat1, "cx", "Caixa")
	setCodigo(m1, nil)
	setCodigo(m2, nil)

	verificar := func(t *testing.T, grupos []CatalogoGrupo) map[string]CatalogoGrupo {
		t.Helper()
		por := make(map[string]CatalogoGrupo)
		for _, g := range grupos {
			por[g.Nome] = g
		}
		if len(por) != 6 {
			t.Fatalf("grupos = %d, want 6", len(por))
		}
		return por
	}

	checar := func(t *testing.T, por map[string]CatalogoGrupo) {
		t.Helper()

		g := por["Grupo Homogeneo Um"]
		if g.Multiplos != (MultiplosGrupo{}) {
			t.Errorf("homogêneo: multiplos = %+v, want todos false", g.Multiplos)
		}
		if g.Codigo != nil {
			t.Errorf("homogêneo: codigo = %s, want <nil> (ambos NULL)", strVal(g.Codigo))
		}
		if g.Categoria == nil || g.Categoria.ID != cat1 || g.Categoria.Codigo != "04.001" || g.Categoria.Nome == "" {
			t.Errorf("homogêneo: categoria = %+v", g.Categoria)
		}
		if g.Embalagem == nil || *g.Embalagem != "Caixa c/ 12" || g.UnidadeMedida == nil || *g.UnidadeMedida != "cx" {
			t.Errorf("homogêneo: embalagem/unidade = %s/%s", strVal(g.Embalagem), strVal(g.UnidadeMedida))
		}
		if g.QuantidadeTotal != 2 {
			t.Errorf("homogêneo: quantidadeTotal = %v, want 2", g.QuantidadeTotal)
		}

		g = por["Grupo Unitario Um"]
		if g.Multiplos != (MultiplosGrupo{}) {
			t.Errorf("unitário: multiplos = %+v, want todos false", g.Multiplos)
		}
		if g.Codigo == nil || *g.Codigo != "UNI-1" || g.Categoria == nil || g.Categoria.ID != cat2 {
			t.Errorf("unitário: codigo/categoria = %s/%+v", strVal(g.Codigo), g.Categoria)
		}
		if g.UnidadeMedida == nil || *g.UnidadeMedida != "un" {
			t.Errorf("unitário: unidadeMedida = %s, want un", strVal(g.UnidadeMedida))
		}

		g = por["Grupo Codigo Divergente"]
		if !g.Multiplos.Codigo || g.Codigo != nil {
			t.Errorf("código divergente: multiplos.codigo = %v, codigo = %s, want true/<nil>", g.Multiplos.Codigo, strVal(g.Codigo))
		}
		if g.Multiplos.Categoria || g.Multiplos.EmbalagemUnidade || g.Categoria == nil {
			t.Errorf("código divergente: demais colunas deveriam concordar (%+v, %+v)", g.Multiplos, g.Categoria)
		}

		g = por["Grupo Categoria Divergente"]
		if !g.Multiplos.Categoria || g.Categoria != nil {
			t.Errorf("categoria divergente: multiplos.categoria = %v, categoria = %+v, want true/nil", g.Multiplos.Categoria, g.Categoria)
		}
		if g.Multiplos.Codigo || g.Codigo != nil {
			t.Errorf("categoria divergente: codigo deveria ser comum nil (%v, %s)", g.Multiplos.Codigo, strVal(g.Codigo))
		}

		for _, nome := range []string{"Grupo Embalagem Divergente", "Grupo Unidade Divergente"} {
			g = por[nome]
			if !g.Multiplos.EmbalagemUnidade || g.Embalagem != nil || g.UnidadeMedida != nil {
				t.Errorf("%s: multiplos.embalagemUnidade = %v, embalagem/unidade = %s/%s, want true/<nil>/<nil>",
					nome, g.Multiplos.EmbalagemUnidade, strVal(g.Embalagem), strVal(g.UnidadeMedida))
			}
			if g.Multiplos.Codigo || g.Multiplos.Categoria {
				t.Errorf("%s: código/categoria deveriam concordar (%+v)", nome, g.Multiplos)
			}
		}
	}

	t.Run("ListarCatalogoAgrupado", func(t *testing.T) {
		grupos, _, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
		if err != nil {
			t.Fatalf("ListarCatalogoAgrupado: %v", err)
		}
		checar(t, verificar(t, grupos))
	})
	t.Run("ListarTodosGruposCatalogo", func(t *testing.T) {
		grupos, err := ListarTodosGruposCatalogo(db, FiltrosCatalogo{EmpresaID: empresaTeste})
		if err != nil {
			t.Fatalf("ListarTodosGruposCatalogo: %v", err)
		}
		checar(t, verificar(t, grupos))
	})
}

// TestListarCatalogoAgrupado_GrupoSemEmbalagemNemUnidade: grupo unitário/
// homogêneo com embalagem e unidade NULL (importado) -> nil/nil e flag false
// (o "valor comum" é ausente, nunca "Múltiplos").
func TestListarCatalogoAgrupado_GrupoSemEmbalagemNemUnidade(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, empresaTeste, "Canteiro Agrupado Sem Unidade")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	id, _ := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		Nome: "Grupo Importado Sem Unidade", CategoriaID: categoriaID,
		UnidadeMedida: "un",
	}, estoque.ID, 1)
	if _, err := db.Exec(`UPDATE produtos SET unidade_medida = NULL, embalagem = NULL WHERE id = $1`, id); err != nil {
		t.Fatalf("seed UPDATE: %v", err)
	}

	grupos, _, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado: %v", err)
	}
	if len(grupos) != 1 {
		t.Fatalf("len = %d, want 1", len(grupos))
	}
	g := grupos[0]
	if g.Embalagem != nil || g.UnidadeMedida != nil || g.Multiplos.EmbalagemUnidade {
		t.Errorf("embalagem/unidade = %s/%s (multiplos %v), want nil/nil/false", strVal(g.Embalagem), strVal(g.UnidadeMedida), g.Multiplos.EmbalagemUnidade)
	}
}
