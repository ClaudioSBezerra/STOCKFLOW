package services

import (
	"database/sql"
	"errors"
	"testing"
)

// Story 16.2 — Produto inativo some das operações e permanece nos históricos.

func inativarDireto(t *testing.T, db *sql.DB, produtoID string) {
	t.Helper()
	if _, err := db.Exec(`UPDATE produtos SET inativado_em = now() WHERE id = $1`, produtoID); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogoEBusca_ProdutoInativoSome(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	ativo := criarProdutoInativacao(t, db, empresaTeste, "Alfa Ativo Cat162", "")
	inativo := criarProdutoInativacao(t, db, empresaTeste, "Alfa Inativo Cat162", "")
	inativarDireto(t, db, inativo)
	var codInativo string
	if err := db.QueryRow(`SELECT codigo FROM produtos WHERE id = $1`, inativo).Scan(&codInativo); err != nil {
		t.Fatal(err)
	}

	itens, _, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil || len(itens) != 1 || itens[0].ID != ativo {
		t.Errorf("grade padrão = %+v err=%v, want só o ativo", itens, err)
	}
	itens, _, err = ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, SomenteInativos: true})
	if err != nil || len(itens) != 1 || itens[0].ID != inativo {
		t.Errorf("grade inativos = %+v err=%v, want só o inativo", itens, err)
	}
	grupos, _, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil || len(grupos) != 1 {
		t.Errorf("agrupado padrão = %+v err=%v, want 1 grupo", grupos, err)
	}
	todos, err := ListarTodosGruposCatalogo(db, FiltrosCatalogo{EmpresaID: empresaTeste})
	if err != nil || len(todos) != 1 {
		t.Errorf("todos os grupos = %+v err=%v, want 1", todos, err)
	}

	res, err := BuscarProdutos(db, empresaTeste, "Alfa")
	if err != nil || len(res) != 1 || res[0].ID != ativo {
		t.Errorf("busca = %+v err=%v, want só o ativo", res, err)
	}
	if _, err := BuscarProdutoPorCodigo(db, empresaTeste, codInativo); err == nil {
		t.Error("BuscarProdutoPorCodigo(inativo) deveria falhar com não encontrado")
	}
}

func TestAdicionarItemCarrinho_ProdutoInativoRecusado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Carrinho Recusa 162", 5)
	usuario := semearConta(t, db, "Usuário Carrinho 162", "usuario.carrinho162@empresa.com", PapelUsuario, 0)
	inativarDireto(t, db, produtoID)

	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuario, produtoID, estoqueID, 1); !errors.Is(err, ErrProdutoInativo) {
		t.Fatalf("err = %v, want ErrProdutoInativo", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM carrinho_itens WHERE usuario_id = $1`, usuario).Scan(&n); err != nil || n != 0 {
		t.Errorf("carrinho_itens = %d err=%v, want 0", n, err)
	}
}

func TestImportacao_LinhaDeProdutoInativoRejeitada(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-inativo162@empresa.com")
	estoque, err := CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Canteiro Import Inativo 162")
	if err != nil {
		t.Fatal(err)
	}
	cat := categoriaIDPorCodigo(t, db, "04.001")
	mk := func(nome string) Produto {
		p, err := criarProdutoComSaldo(db, empresaTeste, CriarProdutoInput{
			UnidadeMedida: "un", Nome: nome, CategoriaID: cat, TemplateID: templateGenericoID(t, db, empresaTeste),
		}, estoque.ID, 10)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	pInativo := mk("Produto Import Inativo")
	pAtivo := mk("Produto Import Ativo")
	inativarDireto(t, db, pInativo.ID)

	catNome := categoriaNomePorCodigo(t, db, "04.001")
	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Import Inativo", pInativo.Codigo, catNome, "3", estoque.Nome),
		linhaBase("Produto Import Ativo", pAtivo.Codigo, catNome, "4", estoque.Nome),
	}
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", linhas)
	if err != nil {
		t.Fatal(err)
	}
	if relatorio.Atualizados != 1 || len(relatorio.LinhasRejeitadas) != 1 {
		t.Fatalf("relatorio = %+v, want 1 atualizado e 1 rejeitada", relatorio)
	}
	if got := relatorio.LinhasRejeitadas[0]; got.Linha != 2 || got.Erro != "Produto inativo — reative antes de importar" {
		t.Errorf("rejeitada = %+v", got)
	}
	if q := saldoProdutoEstoque(t, db, pInativo.ID, estoque.ID); q != 10 {
		t.Errorf("saldo do inativo = %v, want 10 (intocado)", q)
	}
}

func TestNormalizacao_IgnoraProdutoInativo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	estoque, err := CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Estoque Normaliza Inativo 162")
	if err != nil {
		t.Fatal(err)
	}
	dim := func() CriarProdutoInput {
		return CriarProdutoInput{UnidadeMedida: "un", Diametro: &DimensaoInput{Valor: ptrFloat(25), Unidade: ptrStr("mm")}}
	}
	p1 := seedProdutoComEstoque(t, db, "Tubo Inativo Dup", estoque.ID, dim())
	p2 := seedProdutoComEstoque(t, db, "Tubo Inativo Dup", estoque.ID, dim())
	inativarDireto(t, db, p2)

	grupos, err := DetectarDuplicatas(db, empresaTeste)
	if err != nil {
		t.Fatal(err)
	}
	if grupoContemProduto(grupos, p1) || grupoContemProduto(grupos, p2) {
		t.Errorf("duplicata com inativo não deveria aparecer: %+v", grupos)
	}
	sugestoes, err := AnalisarInconsistencias(db, empresaTeste)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sugestoes {
		if s.ProdutoID == p2 {
			t.Errorf("sugestão para produto inativo: %+v", s)
		}
	}
}

func TestHistoricos_ProdutoInativoContinuaComMarca(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, almox := seedProdutoComSaldo(t, db, "Historico Inativo 162", 10)
	if _, err := RegistrarBaixa(db, empresaTeste, produtoID, estoqueID, almox, 1); err != nil {
		t.Fatal(err)
	}
	usuario := semearConta(t, db, "Usuário Hist 162", "usuario.hist162@empresa.com", PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuario, produtoID, estoqueID, 2); err != nil {
		t.Fatal(err)
	}
	pedido, err := SubmeterPedido(db, empresaTeste, usuario, "Usuário Hist 162", "Obra X", "")
	if err != nil {
		t.Fatal(err)
	}

	det, err := BuscarPedidoProprio(db, empresaTeste, pedido.ID, usuario, PapelUsuario)
	if err != nil || len(det.Itens) != 1 || det.Itens[0].Inativo {
		t.Fatalf("antes: %+v err=%v", det.Itens, err)
	}
	inativarDireto(t, db, produtoID)

	movs, err := ListarMovimentacoes(db, empresaTeste)
	if err != nil || len(movs) != 1 || !movs[0].Inativo {
		t.Errorf("movimentações = %+v err=%v, want 1 com Inativo", movs, err)
	}
	movsU, err := ListarMovimentacoesDoUsuario(db, empresaTeste, almox)
	if err != nil || len(movsU) != 1 || !movsU[0].Inativo {
		t.Errorf("movimentações do usuário = %+v err=%v", movsU, err)
	}
	det, err = BuscarPedidoProprio(db, empresaTeste, pedido.ID, usuario, PapelUsuario)
	if err != nil || len(det.Itens) != 1 || !det.Itens[0].Inativo {
		t.Errorf("pedido depois = %+v err=%v, want item Inativo", det.Itens, err)
	}
}

func TestMontarReciboPedidoConteudo_ProdutoInativoMarcado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "Recibo Inativo", "recibo-inativo162@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Recibo Inativo Almox", "recibo-inativo162-almox@empresa.com", PapelAlmoxarife, 0)
	pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Recibo Inativo A", SaldoInicial: 10, QtdSolicitada: 4},
	})
	if _, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true); err != nil {
		t.Fatal(err)
	}
	var produtoID string
	if err := db.QueryRow(`SELECT produto_id FROM pedido_itens WHERE pedido_id = $1`, pedido.ID).Scan(&produtoID); err != nil {
		t.Fatal(err)
	}
	_ = pares
	inativarDireto(t, db, produtoID)

	conteudo, err := MontarReciboPedidoConteudo(db, empresaTeste, pedido.ID, usuarioID, PapelUsuario)
	if err != nil {
		t.Fatal(err)
	}
	if len(conteudo.Itens) != 1 || !conteudo.Itens[0].Inativo {
		t.Fatalf("Itens = %+v, want 1 com Inativo", conteudo.Itens)
	}
	if b, err := RenderizarReciboPedidoPDF(conteudo); err != nil || len(b) == 0 {
		t.Errorf("PDF: len=%d err=%v", len(b), err)
	}
}
