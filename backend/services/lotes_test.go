package services

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
)

// Story 11.1 — testes de LancarSaldo e das leituras de saldo com Lotes.

func empresaAlheiaLotes(t *testing.T, db *sql.DB) Empresa {
	t.Helper()
	const slug = "lotes-alheia"
	if e, err := BuscarEmpresaPorSlug(db, slug); err == nil {
		return e
	}
	return criarEmpresaDeTeste(t, db, slug, "105000000031", "Lotes Alheia")
}

func contarLotes(t *testing.T, db *sql.DB, produtoID, estoqueID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM lotes WHERE produto_id = $1 AND estoque_id = $2`, produtoID, estoqueID).Scan(&n); err != nil {
		t.Fatalf("count lotes: %v", err)
	}
	return n
}

func contarLotesTotal(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM lotes`).Scan(&n); err != nil {
		t.Fatalf("count lotes: %v", err)
	}
	return n
}

func contarMovimentacoesTotal(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM movimentacoes`).Scan(&n); err != nil {
		t.Fatalf("count movimentacoes: %v", err)
	}
	return n
}

func TestLancarSaldo_ComValidade(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Lotes Com Validade", 5)

	lote, err := LancarSaldo(db, empresaTeste, usuarioID, produtoID, estoqueID, 10, "2999-03-01")
	if err != nil {
		t.Fatalf("LancarSaldo: %v", err)
	}
	if lote.ID == "" || lote.Quantidade != 10 || lote.Vencido {
		t.Errorf("lote = %+v", lote)
	}
	if lote.DataValidade == nil || *lote.DataValidade != "2999-03-01" {
		t.Errorf("DataValidade = %v, want 2999-03-01", lote.DataValidade)
	}
	if n := contarLotes(t, db, produtoID, estoqueID); n != 1 {
		t.Errorf("lotes = %d, want 1", n)
	}

	var tipo string
	var origem sql.NullString
	var destino, loteID string
	var qtd float64
	if err := db.QueryRow(
		`SELECT tipo, estoque_origem_id, estoque_destino_id, lote_id, quantidade FROM movimentacoes WHERE id = $1`, lote.MovimentacaoID,
	).Scan(&tipo, &origem, &destino, &loteID, &qtd); err != nil {
		t.Fatalf("ler movimentação: %v", err)
	}
	if tipo != "entrada" || origem.Valid || destino != estoqueID || loteID != lote.ID || qtd != 10 {
		t.Errorf("movimentação = (%s,%v,%s,%s,%v)", tipo, origem, destino, loteID, qtd)
	}
	// produto_estoque intocado.
	if s := saldoProdutoEstoque(t, db, produtoID, estoqueID); s != 5 {
		t.Errorf("produto_estoque = %v, want 5 (intocado)", s)
	}
}

func TestLancarSaldo_SemValidade(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Lotes Sem Validade", 0)

	for _, dv := range []string{""} {
		lote, err := LancarSaldo(db, empresaTeste, usuarioID, produtoID, estoqueID, 1.5, dv)
		if err != nil {
			t.Fatalf("LancarSaldo(%q): %v", dv, err)
		}
		if lote.DataValidade != nil || lote.Vencido {
			t.Errorf("lote = %+v, want dataValidade nula e não vencido", lote)
		}
	}
}

func TestLancarSaldo_ValidadePassadaNasceVencido(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Lotes Vencido", 0)

	lote, err := LancarSaldo(db, empresaTeste, usuarioID, produtoID, estoqueID, 3, "2020-01-01")
	if err != nil {
		t.Fatalf("LancarSaldo: %v", err)
	}
	if !lote.Vencido {
		t.Errorf("Vencido = false, want true")
	}
	// hoje não é vencido
	var hoje string
	if err := db.QueryRow(`SELECT to_char(CURRENT_DATE, 'YYYY-MM-DD')`).Scan(&hoje); err != nil {
		t.Fatal(err)
	}
	l2, err := LancarSaldo(db, empresaTeste, usuarioID, produtoID, estoqueID, 3, hoje)
	if err != nil {
		t.Fatalf("LancarSaldo hoje: %v", err)
	}
	if l2.Vencido {
		t.Errorf("lote com validade hoje não deveria ser vencido")
	}
}

func TestLancarSaldo_SegundoLancamentoCriaLoteAdicional(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Lotes Adicional", 4)

	a, err := LancarSaldo(db, empresaTeste, usuarioID, produtoID, estoqueID, 10, "2999-01-01")
	if err != nil {
		t.Fatal(err)
	}
	b, err := LancarSaldo(db, empresaTeste, usuarioID, produtoID, estoqueID, 6, "2999-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Errorf("mesmo id de lote nos dois lançamentos")
	}
	if n := contarLotes(t, db, produtoID, estoqueID); n != 2 {
		t.Errorf("lotes = %d, want 2", n)
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 2 {
		t.Errorf("movimentações = %d, want 2 entradas", n)
	}

	det, err := ObterProdutoDetalhe(db, empresaTeste, produtoID)
	if err != nil {
		t.Fatal(err)
	}
	if det.QuantidadeTotal != 20 || !det.Disponivel {
		t.Errorf("QuantidadeTotal = %v, want 20 (4 legado + 10 + 6)", det.QuantidadeTotal)
	}
	if len(det.PorEstoque) != 1 || det.PorEstoque[0].Quantidade != 20 {
		t.Fatalf("PorEstoque = %+v", det.PorEstoque)
	}
	lotes := det.PorEstoque[0].Lotes
	if len(lotes) != 3 {
		t.Fatalf("lotes = %+v, want 3 (2 reais + 1 legado)", lotes)
	}
	var legados int
	for _, l := range lotes {
		if l.Legado {
			legados++
			if l.ID != nil || l.DataValidade != nil || l.Quantidade != 4 {
				t.Errorf("legado = %+v", l)
			}
		} else if l.ID == nil {
			t.Errorf("lote real sem id: %+v", l)
		}
	}
	if legados != 1 {
		t.Errorf("legados = %d, want 1", legados)
	}
	if !lotes[len(lotes)-1].Legado {
		t.Errorf("a entrada legado deve vir por último, lotes = %+v", lotes)
	}
}

func TestLancarSaldo_QuantidadeInvalida(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Lotes Qtd Invalida", 0)

	for _, q := range []float64{0, -1, limiteNumeric103 + 1, 0.0001} {
		_, err := LancarSaldo(db, empresaTeste, usuarioID, produtoID, estoqueID, q, "")
		var ev *ErroLoteValidacao
		if !errors.As(err, &ev) {
			t.Errorf("quantidade %v: err = %v, want *ErroLoteValidacao", q, err)
		}
	}
	if n := contarLotesTotal(t, db); n != 0 {
		t.Errorf("lotes = %d, want 0", n)
	}
	if n := contarMovimentacoesTotal(t, db); n != 0 {
		t.Errorf("movimentações = %d, want 0", n)
	}
}

func TestLancarSaldo_DataInvalida(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Lotes Data Invalida", 0)

	for _, d := range []string{"01/03/2027", "2027-13-40", "2027-3-1", "2027-03-01T00:00:00Z", "abc", "0000-01-01", " 2027-03-01"} {
		_, err := LancarSaldo(db, empresaTeste, usuarioID, produtoID, estoqueID, 1, d)
		var ev *ErroLoteValidacao
		if !errors.As(err, &ev) {
			t.Errorf("data %q: err = %v, want *ErroLoteValidacao", d, err)
		}
	}
	if n := contarLotesTotal(t, db); n != 0 {
		t.Errorf("lotes = %d, want 0", n)
	}
}

func TestLancarSaldo_AlvoInvalidoColapsaEm404(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Lotes Alvo", 0)

	alheia := empresaAlheiaLotes(t, db)
	estoqueAlheio, err := CriarEstoque(db, alheia.ID, filialTeste(t, db, alheia.ID), "Estoque Alheio Lotes")
	if err != nil {
		t.Fatalf("CriarEstoque alheio: %v", err)
	}
	produtoMesclado := seedProdutoParaMesclagem(t, db, "Produto Mesclado Lotes", estoqueID, 0, CriarProdutoInput{UnidadeMedida: "un"})
	if _, err := db.Exec(`UPDATE produtos SET deleted_at = now() WHERE id = $1`, produtoMesclado); err != nil {
		t.Fatal(err)
	}
	const ausente = "00000000-0000-4000-8000-000000000000"

	casos := map[string][2]string{
		"estoque alheio":      {produtoID, estoqueAlheio.ID},
		"produto inexistente": {ausente, estoqueID},
		"estoque inexistente": {produtoID, ausente},
		"produto mesclado":    {produtoMesclado, estoqueID},
		"produto malformado":  {"nao-uuid", estoqueID},
		"estoque malformado":  {produtoID, "nao-uuid"},
	}
	for nome, ids := range casos {
		_, err := LancarSaldo(db, empresaTeste, usuarioID, ids[0], ids[1], 1, "")
		if !errors.Is(err, ErrLoteAlvoNaoEncontrado) {
			t.Errorf("%s: err = %v, want ErrLoteAlvoNaoEncontrado", nome, err)
		}
	}
	// produto de outra Empresa
	_, err = LancarSaldo(db, alheia.ID, usuarioID, produtoID, estoqueAlheio.ID, 1, "")
	if !errors.Is(err, ErrLoteAlvoNaoEncontrado) {
		t.Errorf("produto alheio: err = %v, want ErrLoteAlvoNaoEncontrado", err)
	}
	if n := contarLotesTotal(t, db); n != 0 {
		t.Errorf("lotes = %d, want 0", n)
	}
	if n := contarMovimentacoesTotal(t, db); n != 0 {
		t.Errorf("movimentações = %d, want 0", n)
	}
}

func TestCatalogo_SaldoSomaLegadoMaisLotes(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Lotes Catalogo", 2)
	// Produto só com Lote (sem saldo legado) num segundo Estoque.
	outro, err := CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Lotes Catalogo Outro")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LancarSaldo(db, empresaTeste, usuarioID, produtoID, estoqueID, 3, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := LancarSaldo(db, empresaTeste, usuarioID, produtoID, outro.ID, 7, "2020-01-01"); err != nil {
		t.Fatal(err)
	}

	filtros := FiltrosCatalogo{EmpresaID: empresaTeste}
	grade, _, err := ListarCatalogoGrade(db, 1, filtros)
	if err != nil {
		t.Fatal(err)
	}
	if len(grade) != 1 || grade[0].QuantidadeTotal != 12 {
		t.Fatalf("grade = %+v, want total 12", grade)
	}

	// Filtro por Estoque que só tem Lote (sem produto_estoque) inclui o Produto.
	grade, pag, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, EstoqueID: outro.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(grade) != 1 || pag.Total != 1 {
		t.Errorf("filtro EstoqueID: grade=%d total=%d, want 1/1", len(grade), pag.Total)
	}
	sim := true
	_, pag, err = ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, ComEstoque: &sim})
	if err != nil || pag.Total != 1 {
		t.Errorf("ComEstoque=true: total=%d err=%v, want 1", pag.Total, err)
	}

	grupos, _, err := ListarCatalogoAgrupado(db, 1, filtros)
	if err != nil {
		t.Fatal(err)
	}
	if len(grupos) != 1 || grupos[0].QuantidadeTotal != 12 || len(grupos[0].PorEstoque) != 2 {
		t.Fatalf("grupos = %+v", grupos)
	}
	for _, eq := range grupos[0].PorEstoque {
		if len(eq.Lotes) != 0 {
			t.Errorf("tabela agrupada não deve trazer lotes: %+v", eq)
		}
	}

	det, err := ObterProdutoDetalhe(db, empresaTeste, produtoID)
	if err != nil {
		t.Fatal(err)
	}
	if det.QuantidadeTotal != 12 {
		t.Errorf("detalhe total = %v, want 12", det.QuantidadeTotal)
	}
	var vencidos int
	for _, eq := range det.PorEstoque {
		for _, l := range eq.Lotes {
			if l.Vencido {
				vencidos++
				if l.Quantidade != 7 {
					t.Errorf("lote vencido = %+v", l)
				}
			}
		}
	}
	if vencidos != 1 {
		t.Errorf("vencidos = %d, want 1 (saldo vencido segue contado)", vencidos)
	}
}

func TestExcluirEstoque_BarradoPorLote(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Lotes Exclusao", 0)
	if _, err := LancarSaldo(db, empresaTeste, usuarioID, produtoID, estoqueID, 5, ""); err != nil {
		t.Fatal(err)
	}
	// Lote zerado também barra.
	if _, err := db.Exec(`UPDATE lotes SET quantidade = 0`); err != nil {
		t.Fatal(err)
	}

	err := ExcluirEstoque(db, empresaTeste, estoqueID)
	var res *ErroEstoqueComResiduo
	if !errors.As(err, &res) {
		t.Fatalf("err = %v, want *ErroEstoqueComResiduo", err)
	}
	if len(res.Produtos) != 1 || res.Produtos[0] != "Produto Lotes Exclusao" {
		t.Errorf("Produtos = %v", res.Produtos)
	}
	var existe bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM estoques WHERE id = $1)`, estoqueID).Scan(&existe); err != nil || !existe {
		t.Errorf("estoque deveria permanecer (existe=%v err=%v)", existe, err)
	}
}

func TestMesclarDuplicatas_ReescreveLotesDosRemovidos(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	estoque, err := CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Estoque Mesclagem Lotes")
	if err != nil {
		t.Fatal(err)
	}
	usuarioID := semearConta(t, db, "Almox Mesclagem Lotes", "mesclagem-lotes-almox@empresa.com", PapelAlmoxarife, 0)
	dims := CriarProdutoInput{UnidadeMedida: "un", Diametro: &DimensaoInput{Valor: ptrFloat(32), Unidade: ptrStr("mm")}}
	a := seedProdutoParaMesclagem(t, db, "Tubo Lotes 32mm", estoque.ID, 1, dims)
	b := seedProdutoParaMesclagem(t, db, "Tubo Lotes 32mm", estoque.ID, 1, dims)
	if _, err := LancarSaldo(db, empresaTeste, usuarioID, b, estoque.ID, 9, "2999-05-05"); err != nil {
		t.Fatal(err)
	}

	if _, err := MesclarDuplicatas(db, empresaTeste, a, []string{b}, usuarioID); err != nil {
		t.Fatalf("MesclarDuplicatas: %v", err)
	}
	if n := contarLotes(t, db, a, estoque.ID); n != 1 {
		t.Errorf("lotes do sobrevivente = %d, want 1", n)
	}
	if n := contarLotes(t, db, b, estoque.ID); n != 0 {
		t.Errorf("lotes do removido = %d, want 0", n)
	}
	det, err := ObterProdutoDetalhe(db, empresaTeste, a)
	if err != nil {
		t.Fatal(err)
	}
	if det.QuantidadeTotal != 11 {
		t.Errorf("total = %v, want 11", det.QuantidadeTotal)
	}
}

// Filtro `ComEstoque` sobre a view: Produto cujo saldo vem SÓ de Lote (saldo
// legado 0) conta como "com estoque" e nunca como "sem estoque".
func TestCatalogo_ComEstoqueConsideraSaldoSoDeLote(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Lotes ComEstoque", 0)
	if _, err := LancarSaldo(db, empresaTeste, usuarioID, produtoID, estoqueID, 4, ""); err != nil {
		t.Fatal(err)
	}

	sim, nao := true, false
	_, pag, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, ComEstoque: &sim})
	if err != nil || pag.Total != 1 {
		t.Errorf("ComEstoque=true: total=%d err=%v, want 1", pag.Total, err)
	}
	_, pag, err = ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: empresaTeste, ComEstoque: &nao})
	if err != nil || pag.Total != 0 {
		t.Errorf("ComEstoque=false: total=%d err=%v, want 0", pag.Total, err)
	}
}

// TestLancarSaldo_CorridaComExcluirEstoque crava o lock que LancarSaldo
// declara ter contra ExcluirEstoque: as duas operações NUNCA terminam com
// sucesso simultâneo (um Lote commitado apontando para um Estoque removido).
func TestLancarSaldo_CorridaComExcluirEstoque(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Lotes Corrida Excluir", 0)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var errExcluir, errLancar error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		errExcluir = ExcluirEstoque(db, empresaTeste, estoqueID)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, errLancar = LancarSaldo(db, empresaTeste, usuarioID, produtoID, estoqueID, 4, "")
	}()
	close(start)
	wg.Wait()

	if errExcluir == nil && errLancar == nil {
		t.Fatalf("as duas operações tiveram sucesso simultâneo — Lote órfão de Estoque removido")
	}
	if errExcluir == nil {
		if !errors.Is(errLancar, ErrLoteAlvoNaoEncontrado) {
			t.Errorf("LancarSaldo deveria falhar com ErrLoteAlvoNaoEncontrado quando a exclusão vence, got %v", errLancar)
		}
		if n := contarLotes(t, db, produtoID, estoqueID); n != 0 {
			t.Errorf("lotes = %d, want 0", n)
		}
		return
	}
	var residuo *ErroEstoqueComResiduo
	if !errors.As(errExcluir, &residuo) {
		t.Fatalf("ExcluirEstoque deveria falhar com *ErroEstoqueComResiduo quando o lançamento vence, got %v", errExcluir)
	}
	if n := contarLotes(t, db, produtoID, estoqueID); n != 1 {
		t.Errorf("lotes = %d, want 1", n)
	}
}

// TestObterProdutoDetalhe_LotesOrdemELegadoZerado prova a ordenação dos Lotes
// no detalhe (validade crescente, NULL por último, legado ao fim) e que
// `produto_estoque.quantidade = 0` não gera entrada `legado`.
func TestObterProdutoDetalhe_LotesOrdemELegadoZerado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Lotes Ordem", 0)
	for _, dv := range []string{"2999-06-01", "", "2999-01-01"} {
		if _, err := LancarSaldo(db, empresaTeste, usuarioID, produtoID, estoqueID, 1, dv); err != nil {
			t.Fatalf("LancarSaldo(%q): %v", dv, err)
		}
	}

	det, err := ObterProdutoDetalhe(db, empresaTeste, produtoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(det.PorEstoque) != 1 {
		t.Fatalf("porEstoque = %+v, want 1 item", det.PorEstoque)
	}
	lotes := det.PorEstoque[0].Lotes
	if len(lotes) != 3 {
		t.Fatalf("lotes = %+v, want 3 (sem entrada legado)", lotes)
	}
	want := []string{"2999-01-01", "2999-06-01", ""}
	for i, l := range lotes {
		if l.Legado {
			t.Errorf("lotes[%d] é legado, mas produto_estoque = 0", i)
		}
		got := ""
		if l.DataValidade != nil {
			got = *l.DataValidade
		}
		if got != want[i] {
			t.Errorf("lotes[%d].DataValidade = %q, want %q", i, got, want[i])
		}
	}
}

// TestListarMovimentacoes_EntradaSemOrigem prova que a Movimentação `entrada`
// (origem NULL) aparece na trilha e na exportação LGPD do usuário.
func TestListarMovimentacoes_EntradaSemOrigem(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Lotes Trilha", 0)
	lote, err := LancarSaldo(db, empresaTeste, usuarioID, produtoID, estoqueID, 2, "")
	if err != nil {
		t.Fatal(err)
	}

	confere := func(nome string, lista []MovimentacaoHistorico, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", nome, err)
		}
		for _, m := range lista {
			if m.ID != lote.MovimentacaoID {
				continue
			}
			if m.Tipo != "entrada" || m.EstoqueOrigemID != nil || m.EstoqueDestinoID == nil || *m.EstoqueDestinoID != estoqueID {
				t.Errorf("%s: movimentação = %+v", nome, m)
			}
			return
		}
		t.Errorf("%s: movimentação de entrada %s ausente em %+v", nome, lote.MovimentacaoID, lista)
	}
	trilha, err := ListarMovimentacoes(db, empresaTeste, FiltroMovimentacoes{})
	confere("ListarMovimentacoes", trilha, err)
	doUsuario, err := ListarMovimentacoesDoUsuario(db, empresaTeste, usuarioID)
	confere("ListarMovimentacoesDoUsuario", doUsuario, err)
}
