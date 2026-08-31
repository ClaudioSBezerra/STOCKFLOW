package services

import (
	"database/sql"
	"fmt"
	"testing"
)

// --- Story 4.3: Visualização em grade e tabela agrupada -------------------
//
// Helpers do mesmo pacote reutilizados: limparProdutos, categoriaIDPorCodigo,
// testDB, ptrFloat, ptrStr (produtos_test.go).

// criarProdutoCat cadastra um Produto via CriarProduto e devolve o id. A
// linha inicial em `produto_estoque` (EstoqueID + QuantidadeInicial) segue a
// regra de CriarProduto; os testes ajustam depois com setQuantidade /
// limparEstoqueDe.
func criarProdutoCat(t *testing.T, db *sql.DB, in CriarProdutoInput) string {
	t.Helper()
	p, err := CriarProduto(db, in)
	if err != nil {
		t.Fatalf("seed CriarProduto(%q): %v", in.Nome, err)
	}
	return p.ID
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

	estoque, err := CriarEstoque(db, "Canteiro Catalogo Grade")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	// Inserção fora de ordem alfabética de propósito — a query deve ordenar.
	for i := 29; i >= 0; i-- {
		criarProdutoCat(t, db, CriarProdutoInput{
			Nome:              fmt.Sprintf("Produto %02d", i),
			CategoriaID:       categoriaID,
			EstoqueID:         estoque.ID,
			QuantidadeInicial: 1,
		})
	}

	itens, pag, err := ListarCatalogoGrade(db, 1)
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

	itens2, pag2, err := ListarCatalogoGrade(db, 2)
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

	estoque, err := CriarEstoque(db, "Canteiro Catalogo SemEstoque")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	semEstoque := criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Sem Estoque", Codigo: "SEM-EST", CategoriaID: categoriaID,
		EstoqueID: estoque.ID, QuantidadeInicial: 0,
	})
	limparEstoqueDe(t, db, semEstoque)

	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Com Estoque", CategoriaID: categoriaID,
		EstoqueID: estoque.ID, QuantidadeInicial: 7,
	})

	itens, pag, err := ListarCatalogoGrade(db, 1)
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
	if sem.Codigo == nil || *sem.Codigo != "SEM-EST" {
		t.Errorf("codigo = %v, want SEM-EST", sem.Codigo)
	}
}

// TestListarCatalogoGrade_QuantidadeSomadaEDimensoes prova que quantidadeTotal
// soma TODAS as linhas `produto_estoque` do Produto e que as 5 dimensões
// pareadas voltam na projeção (par NULL -> nil).
func TestListarCatalogoGrade_QuantidadeSomadaEDimensoes(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoqueA, err := CriarEstoque(db, "Estoque A")
	if err != nil {
		t.Fatalf("seed CriarEstoque A: %v", err)
	}
	estoqueB, err := CriarEstoque(db, "Estoque B")
	if err != nil {
		t.Fatalf("seed CriarEstoque B: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	id := criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Tubo PVC", CategoriaID: categoriaID,
		EstoqueID: estoqueA.ID, QuantidadeInicial: 10,
		Comprimento: &DimensaoInput{Valor: ptrFloat(6), Unidade: ptrStr("m")},
	})
	setQuantidade(t, db, id, estoqueB.ID, 5)

	itens, _, err := ListarCatalogoGrade(db, 1)
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

	estoque, err := CriarEstoque(db, "Canteiro Catalogo AlemUltima")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Único", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 1,
	})

	itens, pag, err := ListarCatalogoGrade(db, 99)
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

	itens, pag, err := ListarCatalogoGrade(db, 1)
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

	estAlmox, err := CriarEstoque(db, "Almoxarifado Central")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	estObra, err := CriarEstoque(db, "Obra Norte")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	dim := func() *DimensaoInput { return &DimensaoInput{Valor: ptrFloat(8), Unidade: ptrStr("mm")} }

	// p1: 10 em Almoxarifado. p2: 5 em Almoxarifado + 2 em Obra. p3: sem estoque.
	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Parafuso", CategoriaID: categoriaID, EstoqueID: estAlmox.ID, QuantidadeInicial: 10,
		Diametro: dim(),
	})
	p2 := criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Parafuso", CategoriaID: categoriaID, EstoqueID: estAlmox.ID, QuantidadeInicial: 5,
		Diametro: dim(),
	})
	setQuantidade(t, db, p2, estObra.ID, 2)
	p3 := criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Parafuso", CategoriaID: categoriaID, EstoqueID: estAlmox.ID, QuantidadeInicial: 0,
		Diametro: dim(),
	})
	limparEstoqueDe(t, db, p3)

	grupos, pag, err := ListarCatalogoAgrupado(db, 1)
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado: %v", err)
	}
	if pag.Total != 1 || len(grupos) != 1 {
		t.Fatalf("total = %d, len(grupos) = %d, want 1/1", pag.Total, len(grupos))
	}
	g := grupos[0]
	if g.Nome != "Parafuso" {
		t.Errorf("nome = %q, want Parafuso", g.Nome)
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

	estoque, err := CriarEstoque(db, "Canteiro Dimensoes Distintas")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Parafuso", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 3,
		Comprimento: &DimensaoInput{Valor: ptrFloat(20), Unidade: ptrStr("mm")},
	})
	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Parafuso", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 4,
		Comprimento: &DimensaoInput{Valor: ptrFloat(30), Unidade: ptrStr("mm")},
	})

	grupos, pag, err := ListarCatalogoAgrupado(db, 1)
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

	estoque, err := CriarEstoque(db, "Canteiro Dimensoes Nulas")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Cimento", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 100,
	})
	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Cimento", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 50,
	})

	grupos, pag, err := ListarCatalogoAgrupado(db, 1)
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

	estoque, err := CriarEstoque(db, "Canteiro Grupo Sem Estoque")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	a := criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Prego", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 0,
	})
	b := criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Prego", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 0,
	})
	limparEstoqueDe(t, db, a)
	limparEstoqueDe(t, db, b)

	grupos, pag, err := ListarCatalogoAgrupado(db, 1)
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

	estoque, err := CriarEstoque(db, "Canteiro Paginacao Grupos")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	for i := 0; i < 26; i++ {
		criarProdutoCat(t, db, CriarProdutoInput{
			Nome:              fmt.Sprintf("Item %02d", i),
			CategoriaID:       categoriaID,
			EstoqueID:         estoque.ID,
			QuantidadeInicial: 1,
		})
	}

	g1, pag1, err := ListarCatalogoAgrupado(db, 1)
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado(1): %v", err)
	}
	if len(g1) != 24 || pag1.Total != 26 || pag1.TotalPaginas != 2 {
		t.Fatalf("página 1: len = %d, total = %d, totalPaginas = %d, want 24/26/2", len(g1), pag1.Total, pag1.TotalPaginas)
	}
	if g1[0].Nome != "Item 00" || g1[23].Nome != "Item 23" {
		t.Errorf("página 1 ordem = [%q .. %q]", g1[0].Nome, g1[23].Nome)
	}

	g2, pag2, err := ListarCatalogoAgrupado(db, 2)
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado(2): %v", err)
	}
	if len(g2) != 2 || pag2.Total != 26 {
		t.Fatalf("página 2: len = %d, total = %d, want 2/26", len(g2), pag2.Total)
	}
	if g2[0].Nome != "Item 24" || g2[1].Nome != "Item 25" {
		t.Errorf("página 2 ordem = [%q, %q], want [Item 24, Item 25]", g2[0].Nome, g2[1].Nome)
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

	estoque, err := CriarEstoque(db, "Canteiro Agrupado AlemUltima")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	for _, nome := range []string{"Arruela", "Bucha", "Cano"} {
		criarProdutoCat(t, db, CriarProdutoInput{
			Nome: nome, CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 1,
		})
	}

	grupos, pag, err := ListarCatalogoAgrupado(db, 99)
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

	estoque, err := CriarEstoque(db, "Canteiro Dim Parcial")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Cano", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 1,
		Comprimento: &DimensaoInput{Valor: ptrFloat(6), Unidade: ptrStr("m")},
	})
	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Cano", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 1,
	})

	grupos, pag, err := ListarCatalogoAgrupado(db, 1)
	if err != nil {
		t.Fatalf("ListarCatalogoAgrupado: %v", err)
	}
	if pag.Total != 2 || len(grupos) != 2 {
		t.Fatalf("total = %d, len(grupos) = %d, want 2/2", pag.Total, len(grupos))
	}
}
