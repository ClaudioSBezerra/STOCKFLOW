package services

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

// Story 11.4 — consumo FEFO em Baixa/Transferência.

func idPtr(s string) *string { return &s }

func TestOrdenarFontesFEFO(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	val := func(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
	lotes := []fonteSaldo{
		{loteID: idPtr("sv-novo"), quantidade: 1, criadoEm: base.Add(2 * time.Hour)},
		{loteID: idPtr("v-out-b"), quantidade: 1, dataValidade: val("2026-10-01"), criadoEm: base.Add(time.Hour)},
		{loteID: idPtr("sv-velho"), quantidade: 1, criadoEm: base},
		{loteID: idPtr("v-ago"), quantidade: 1, dataValidade: val("2026-08-01"), criadoEm: base.Add(3 * time.Hour)},
		{loteID: idPtr("v-out-a"), quantidade: 1, dataValidade: val("2026-10-01"), criadoEm: base},
		{loteID: idPtr("zerado"), quantidade: 0, dataValidade: val("2026-01-01"), criadoEm: base},
	}
	got := ordenarFontesFEFO(lotes, 3)
	want := []string{"v-ago", "v-out-a", "v-out-b", "", "sv-velho", "sv-novo"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i, f := range got {
		id := ""
		if f.loteID != nil {
			id = *f.loteID
		}
		if id != want[i] {
			t.Errorf("posição %d = %q, want %q", i, id, want[i])
		}
	}
	if sem := ordenarFontesFEFO(nil, 0); len(sem) != 0 {
		t.Errorf("sem fontes = %d, want 0", len(sem))
	}
}

// seedLote insere um Lote direto (com criado_em controlado) para os cenários.
func seedLote(t *testing.T, db *sql.DB, produtoID, estoqueID string, quantidade float64, validade string, criadoEm time.Time) string {
	t.Helper()
	var dv any
	if validade != "" {
		dv = validade
	}
	var id string
	if err := db.QueryRow(`
		INSERT INTO lotes (produto_id, estoque_id, quantidade, data_validade, empresa_id, criado_em)
		VALUES ($1, $2, $3, $4::date, $5, $6) RETURNING id`,
		produtoID, estoqueID, quantidade, dv, empresaTeste, criadoEm).Scan(&id); err != nil {
		t.Fatalf("seed lote: %v", err)
	}
	return id
}

func qtdLote(t *testing.T, db *sql.DB, id string) float64 {
	t.Helper()
	var q float64
	if err := db.QueryRow(`SELECT quantidade FROM lotes WHERE id = $1`, id).Scan(&q); err != nil {
		t.Fatalf("ler lote: %v", err)
	}
	return q
}

// zerarLegado zera o produto_estoque semeado, deixando só Lotes.
func zerarLegado(t *testing.T, db *sql.DB, produtoID, estoqueID string) {
	t.Helper()
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 0 WHERE produto_id = $1 AND estoque_id = $2`, produtoID, estoqueID); err != nil {
		t.Fatal(err)
	}
}

func saldoVisao(t *testing.T, db *sql.DB, produtoID, estoqueID string) float64 {
	t.Helper()
	var q sql.NullFloat64
	if err := db.QueryRow(`SELECT quantidade FROM saldo_produto_estoque WHERE produto_id = $1 AND estoque_id = $2`, produtoID, estoqueID).Scan(&q); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	return q.Float64
}

type movLinha struct {
	Tipo       string
	Quantidade float64
	LoteID     sql.NullString
	Destino    sql.NullString
}

func movsDoProduto(t *testing.T, db *sql.DB, produtoID string) []movLinha {
	t.Helper()
	rows, err := db.Query(`SELECT tipo, quantidade, lote_id, estoque_destino_id FROM movimentacoes WHERE produto_id = $1 ORDER BY criado_em, id`, produtoID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []movLinha
	for rows.Next() {
		var m movLinha
		if err := rows.Scan(&m.Tipo, &m.Quantidade, &m.LoteID, &m.Destino); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func TestRegistrarBaixa_FEFOSimples(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "FEFO Simples", 0)
	agora := time.Now().Add(-time.Hour)
	a := seedLote(t, db, produtoID, estoqueID, 3, "2026-10-01", agora)
	b := seedLote(t, db, produtoID, estoqueID, 4, "2026-08-01", agora.Add(time.Minute))

	mov, err := RegistrarBaixa(db, empresaTeste, produtoID, estoqueID, usuarioID, 5)
	if err != nil {
		t.Fatalf("RegistrarBaixa: %v", err)
	}
	if mov.Quantidade != 5 || mov.Tipo != "baixa" || mov.EstoqueDestinoID != nil {
		t.Errorf("mov = %+v, want baixa de 5 sem destino", mov)
	}
	if q := qtdLote(t, db, b); q != 0 {
		t.Errorf("lote B = %v, want 0", q)
	}
	if q := qtdLote(t, db, a); q != 2 {
		t.Errorf("lote A = %v, want 2", q)
	}
	movs := movsDoProduto(t, db, produtoID)
	if len(movs) != 2 {
		t.Fatalf("movimentações = %d, want 2", len(movs))
	}
	// Mesma transação => mesmo criado_em: compara por Lote, não por posição.
	porLote := map[string]float64{}
	for _, m := range movs {
		porLote[m.LoteID.String] = m.Quantidade
	}
	if porLote[b] != 4 || porLote[a] != 1 {
		t.Errorf("movs = %+v, want B:4 e A:1", movs)
	}
	var existe bool
	_ = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM lotes WHERE id = $1)`, b).Scan(&existe)
	if !existe {
		t.Error("lote esgotado não deve ser apagado")
	}
}

func TestRegistrarBaixa_SemValidadePorUltimo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "FEFO SemVal", 0)
	agora := time.Now().Add(-time.Hour)
	n := seedLote(t, db, produtoID, estoqueID, 5, "", agora)
	d := seedLote(t, db, produtoID, estoqueID, 2, "2027-01-01", agora.Add(time.Minute))

	if _, err := RegistrarBaixa(db, empresaTeste, produtoID, estoqueID, usuarioID, 4); err != nil {
		t.Fatalf("RegistrarBaixa: %v", err)
	}
	if q := qtdLote(t, db, d); q != 0 {
		t.Errorf("lote datado = %v, want 0", q)
	}
	if q := qtdLote(t, db, n); q != 3 {
		t.Errorf("lote sem validade = %v, want 3", q)
	}
}

func TestRegistrarBaixa_LegadoEntreDatadoESemValidade(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "FEFO Legado", 3)
	agora := time.Now().Add(-time.Hour)
	datado := seedLote(t, db, produtoID, estoqueID, 2, "2026-12-01", agora)
	sem := seedLote(t, db, produtoID, estoqueID, 4, "", agora)

	if _, err := RegistrarBaixa(db, empresaTeste, produtoID, estoqueID, usuarioID, 6); err != nil {
		t.Fatalf("RegistrarBaixa: %v", err)
	}
	if q := qtdLote(t, db, datado); q != 0 {
		t.Errorf("datado = %v, want 0", q)
	}
	var legado float64
	if err := db.QueryRow(`SELECT quantidade FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2`, produtoID, estoqueID).Scan(&legado); err != nil {
		t.Fatal(err)
	}
	if legado != 0 {
		t.Errorf("legado = %v, want 0", legado)
	}
	if q := qtdLote(t, db, sem); q != 3 {
		t.Errorf("sem validade = %v, want 3", q)
	}
	movs := movsDoProduto(t, db, produtoID)
	if len(movs) != 3 {
		t.Fatalf("movimentações = %d, want 3", len(movs))
	}
	porLote := map[string]float64{}
	for _, m := range movs {
		porLote[m.LoteID.String] += m.Quantidade // "" = legado (lote_id NULL)
	}
	if porLote[datado] != 2 || porLote[""] != 3 || porLote[sem] != 1 {
		t.Errorf("movs = %+v, want datado:2, legado(NULL):3, sem:1", movs)
	}
}

func TestRegistrarBaixa_RespeitaSaldoReservado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "Baixa Reservado", "baixa-reservado@empresa.com", PapelUsuario, 0)
	_, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Baixa Reservado", SaldoInicial: 10, QtdSolicitada: 8},
	})
	produtoID, estoqueID := pares[0].ProdutoID, pares[0].EstoqueID
	almox := semearConta(t, db, "Baixa Reservado Almox", "baixa-reservado-fefo-almox@empresa.com", PapelAlmoxarife, 0)

	_, err := RegistrarBaixa(db, empresaTeste, produtoID, estoqueID, almox, 3)
	var indisp *ErroQuantidadeIndisponivel
	if !errors.As(err, &indisp) || indisp.Disponivel != 2 {
		t.Fatalf("erro = %v, want Indisponivel{2}", err)
	}
	if q := saldoProdutoEstoque(t, db, produtoID, estoqueID); q != 10 {
		t.Errorf("saldo = %v, want 10 (inalterado)", q)
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 0 {
		t.Errorf("movimentações = %d, want 0", n)
	}

	if _, err := RegistrarBaixa(db, empresaTeste, produtoID, estoqueID, almox, 2); err != nil {
		t.Fatalf("baixa dentro do disponível: %v", err)
	}
	if q := saldoProdutoEstoque(t, db, produtoID, estoqueID); q != 8 {
		t.Errorf("físico = %v, want 8", q)
	}
	if d := disponivelDoPar(t, db, produtoID, estoqueID); d != 0 {
		t.Errorf("disponível = %v, want 0", d)
	}
}

func TestRegistrarBaixa_EstoqueAlheioColapsaEmZero(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, _, usuarioID := seedProdutoComSaldo(t, db, "Baixa Alheio", 5)
	alheia := empresaAlheiaLotes(t, db)
	estoqueAlheio, err := CriarEstoque(db, alheia.ID, "Estoque Alheio Baixa")
	if err != nil {
		t.Fatal(err)
	}
	_, err = RegistrarBaixa(db, empresaTeste, produtoID, estoqueAlheio.ID, usuarioID, 1)
	var indisp *ErroQuantidadeIndisponivel
	if !errors.As(err, &indisp) || indisp.Disponivel != 0 {
		t.Fatalf("erro = %v, want Indisponivel{0}", err)
	}
}

func TestRegistrarTransferencia_PreservaValidadeParcial(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, origem, usuarioID := seedProdutoComSaldo(t, db, "Transf Val Origem", 0)
	destino, err := CriarEstoque(db, empresaTeste, "Transf Val Destino")
	if err != nil {
		t.Fatal(err)
	}
	criado := time.Now().Add(-48 * time.Hour).Truncate(time.Microsecond)
	lote := seedLote(t, db, produtoID, origem, 5, "2026-11-15", criado)

	mov, err := RegistrarTransferencia(db, empresaTeste, produtoID, origem, destino.ID, usuarioID, 2)
	if err != nil {
		t.Fatalf("RegistrarTransferencia: %v", err)
	}
	if mov.Quantidade != 2 || mov.Tipo != "transferencia" || mov.EstoqueDestinoID == nil || *mov.EstoqueDestinoID != destino.ID {
		t.Errorf("mov = %+v", mov)
	}
	if q := qtdLote(t, db, lote); q != 3 {
		t.Errorf("lote origem = %v, want 3", q)
	}
	var q float64
	var dv string
	var ce time.Time
	if err := db.QueryRow(`SELECT quantidade, to_char(data_validade,'YYYY-MM-DD'), criado_em FROM lotes WHERE produto_id=$1 AND estoque_id=$2`, produtoID, destino.ID).Scan(&q, &dv, &ce); err != nil {
		t.Fatalf("lote destino: %v", err)
	}
	if q != 2 || dv != "2026-11-15" || !ce.Equal(criado) {
		t.Errorf("destino = (%v, %s, %v), want (2, 2026-11-15, %v)", q, dv, ce, criado)
	}
	var linhas int
	_ = db.QueryRow(`SELECT count(*) FROM produto_estoque WHERE produto_id=$1 AND estoque_id=$2`, produtoID, destino.ID).Scan(&linhas)
	if linhas != 0 {
		t.Errorf("produto_estoque do destino = %d linhas, want 0 (saldo só em lotes)", linhas)
	}
	movs := movsDoProduto(t, db, produtoID)
	if len(movs) != 1 || movs[0].LoteID.String != lote || !movs[0].Destino.Valid {
		t.Errorf("movs = %+v, want 1 com lote de origem e destino", movs)
	}
}

func TestRegistrarTransferencia_SomaNoLoteDeDestinoComMesmaValidade(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, origem, usuarioID := seedProdutoComSaldo(t, db, "Transf Merge Origem", 0)
	destino, err := CriarEstoque(db, empresaTeste, "Transf Merge Destino")
	if err != nil {
		t.Fatal(err)
	}
	agora := time.Now().Add(-time.Hour)
	seedLote(t, db, produtoID, origem, 5, "2026-09-30", agora)
	existente := seedLote(t, db, produtoID, destino.ID, 1, "2026-09-30", agora)
	outro := seedLote(t, db, produtoID, destino.ID, 1, "2026-12-31", agora)

	if _, err := RegistrarTransferencia(db, empresaTeste, produtoID, origem, destino.ID, usuarioID, 4); err != nil {
		t.Fatalf("RegistrarTransferencia: %v", err)
	}
	if q := qtdLote(t, db, existente); q != 5 {
		t.Errorf("lote destino mesma validade = %v, want 5", q)
	}
	if q := qtdLote(t, db, outro); q != 1 {
		t.Errorf("lote destino outra validade = %v, want 1", q)
	}
	if n := contarLotes(t, db, produtoID, destino.ID); n != 2 {
		t.Errorf("lotes no destino = %d, want 2 (sem lote novo)", n)
	}
}

func TestRegistrarTransferencia_MultiLoteELegado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, origem, usuarioID := seedProdutoComSaldo(t, db, "Transf Multi Origem", 3)
	destino, err := CriarEstoque(db, empresaTeste, "Transf Multi Destino")
	if err != nil {
		t.Fatal(err)
	}
	agora := time.Now().Add(-time.Hour)
	seedLote(t, db, produtoID, origem, 2, "2026-08-01", agora)
	seedLote(t, db, produtoID, origem, 4, "2026-10-01", agora)
	seedLote(t, db, produtoID, origem, 1, "", agora)

	if _, err := RegistrarTransferencia(db, empresaTeste, produtoID, origem, destino.ID, usuarioID, 10); err != nil {
		t.Fatalf("RegistrarTransferencia: %v", err)
	}
	if s := saldoVisao(t, db, produtoID, origem); s != 0 {
		t.Errorf("saldo origem = %v, want 0", s)
	}
	rows, err := db.Query(`SELECT COALESCE(to_char(data_validade,'YYYY-MM-DD'),''), quantidade FROM lotes WHERE produto_id=$1 AND estoque_id=$2 ORDER BY data_validade NULLS LAST`, produtoID, destino.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type par struct {
		v string
		q float64
	}
	var got []par
	for rows.Next() {
		var p par
		if err := rows.Scan(&p.v, &p.q); err != nil {
			t.Fatal(err)
		}
		got = append(got, p)
	}
	// legado (3) e Lote sem validade (1) caem no MESMO lote sem validade do destino.
	want := []par{{"2026-08-01", 2}, {"2026-10-01", 4}, {"", 4}}
	if len(got) != len(want) {
		t.Fatalf("lotes destino = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("lote destino %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if n := len(movsDoProduto(t, db, produtoID)); n != 4 {
		t.Errorf("movimentações = %d, want 4 (uma por fonte)", n)
	}
}

func TestRegistrarTransferencia_RespeitaReservaEDestinoAlheio(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "Transf Reservado", "transf-reservado@empresa.com", PapelUsuario, 0)
	_, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Transf Reservado", SaldoInicial: 10, QtdSolicitada: 8},
	})
	produtoID, origem := pares[0].ProdutoID, pares[0].EstoqueID
	destino, err := CriarEstoque(db, empresaTeste, "Transf Reservado Destino")
	if err != nil {
		t.Fatal(err)
	}
	almox := semearConta(t, db, "Transf Reservado Almox", "transf-reservado-fefo-almox@empresa.com", PapelAlmoxarife, 0)

	_, err = RegistrarTransferencia(db, empresaTeste, produtoID, origem, destino.ID, almox, 3)
	var indisp *ErroQuantidadeIndisponivel
	if !errors.As(err, &indisp) || indisp.Disponivel != 2 {
		t.Fatalf("erro = %v, want Indisponivel{2}", err)
	}
	if n := contarLotes(t, db, produtoID, destino.ID); n != 0 {
		t.Errorf("lotes no destino = %d, want 0", n)
	}

	alheia := empresaAlheiaLotes(t, db)
	estoqueAlheio, err := CriarEstoque(db, alheia.ID, "Estoque Alheio Transf")
	if err != nil {
		t.Fatal(err)
	}
	_, err = RegistrarTransferencia(db, empresaTeste, produtoID, origem, estoqueAlheio.ID, almox, 1)
	if !errors.As(err, &indisp) || indisp.Disponivel != 0 {
		t.Fatalf("destino alheio: erro = %v, want Indisponivel{0}", err)
	}
	if q := saldoProdutoEstoque(t, db, produtoID, origem); q != 10 {
		t.Errorf("saldo origem = %v, want 10", q)
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 0 {
		t.Errorf("movimentações = %d, want 0", n)
	}
}

func TestRegistrarBaixa_CorridaSoEmLotes(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "FEFO Corrida", 0)
	agora := time.Now().Add(-time.Hour)
	seedLote(t, db, produtoID, estoqueID, 3, "2026-08-01", agora)
	seedLote(t, db, produtoID, estoqueID, 2, "2026-09-01", agora)

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = RegistrarBaixa(db, empresaTeste, produtoID, estoqueID, usuarioID, 5)
		}(i)
	}
	close(start)
	wg.Wait()

	sucessos := 0
	for _, e := range errs {
		if e == nil {
			sucessos++
			continue
		}
		var indisp *ErroQuantidadeIndisponivel
		if !errors.As(e, &indisp) {
			t.Errorf("erro inesperado: %v", e)
		}
	}
	if sucessos != 1 {
		t.Fatalf("sucessos = %d, want 1 (errs=%v)", sucessos, errs)
	}
	if s := saldoVisao(t, db, produtoID, estoqueID); s != 0 {
		t.Errorf("saldo final = %v, want 0", s)
	}
}

func TestRegistrarTransferencia_LocksOpostosComLotesSemDeadlock(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, a, usuarioID := seedProdutoComSaldo(t, db, "FEFO Opostos A", 0)
	bRow, err := CriarEstoque(db, empresaTeste, "FEFO Opostos B")
	if err != nil {
		t.Fatal(err)
	}
	b := bRow.ID
	agora := time.Now().Add(-time.Hour)
	seedLote(t, db, produtoID, a, 20, "2026-08-01", agora)
	seedLote(t, db, produtoID, b, 20, "2026-09-01", agora)

	for i := 0; i < 20; i++ {
		start := make(chan struct{})
		var wg sync.WaitGroup
		var e1, e2 error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, e1 = RegistrarTransferencia(db, empresaTeste, produtoID, a, b, usuarioID, 3)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, e2 = RegistrarTransferencia(db, empresaTeste, produtoID, b, a, usuarioID, 3)
		}()
		close(start)
		wg.Wait()
		if e1 != nil || e2 != nil {
			t.Fatalf("rodada %d: e1=%v e2=%v", i, e1, e2)
		}
	}
	if s := saldoVisao(t, db, produtoID, a) + saldoVisao(t, db, produtoID, b); s != 40 {
		t.Errorf("saldo total = %v, want 40", s)
	}
}
