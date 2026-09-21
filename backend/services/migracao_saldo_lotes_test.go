package services

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

// Story 11.2 — testes da migração do saldo de produto_estoque para Lotes.

func saldoTotalView(t *testing.T, db *sql.DB) float64 {
	t.Helper()
	var s float64
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantidade), 0)::float8 FROM saldo_produto_estoque`).Scan(&s); err != nil {
		t.Fatalf("saldo total da view: %v", err)
	}
	return s
}

func somaProdutoEstoque(t *testing.T, db *sql.DB) float64 {
	t.Helper()
	var s float64
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantidade), 0)::float8 FROM produto_estoque`).Scan(&s); err != nil {
		t.Fatalf("soma de produto_estoque: %v", err)
	}
	return s
}

func TestMigrarSaldoParaLotes_CorteInicial(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	p1, e1, _ := seedProdutoComSaldo(t, db, "Migra Saldo A", 10)
	p2, e2, _ := seedProdutoComSaldo(t, db, "Migra Saldo B", 2.5)
	movsAntes := contarMovimentacoesTotal(t, db)
	totalAntes := saldoTotalView(t, db)

	res, err := MigrarSaldoParaLotes(db)
	if err != nil {
		t.Fatalf("MigrarSaldoParaLotes: %v", err)
	}
	if res.Migrados != 2 || res.Quantidade != 12.5 {
		t.Errorf("resultado = %+v, want Migrados=2 Quantidade=12.5", res)
	}
	for _, c := range []struct {
		produto, estoque string
		qtd              float64
	}{{p1, e1, 10}, {p2, e2, 2.5}} {
		var qtd float64
		var validade sql.NullString
		var empresa string
		if err := db.QueryRow(
			`SELECT quantidade::float8, data_validade::text, empresa_id::text FROM lotes WHERE produto_id = $1 AND estoque_id = $2`,
			c.produto, c.estoque,
		).Scan(&qtd, &validade, &empresa); err != nil {
			t.Fatalf("ler lote: %v", err)
		}
		if qtd != c.qtd || validade.Valid || empresa != empresaTeste {
			t.Errorf("lote = (%v,%v,%s), want (%v,NULL,%s)", qtd, validade, empresa, c.qtd, empresaTeste)
		}
		if s := saldoProdutoEstoque(t, db, c.produto, c.estoque); s != 0 {
			t.Errorf("produto_estoque = %v, want 0", s)
		}
	}
	if totalDepois := saldoTotalView(t, db); totalDepois != totalAntes {
		t.Errorf("saldo total = %v, want %v", totalDepois, totalAntes)
	}
	if n := contarMovimentacoesTotal(t, db); n != movsAntes {
		t.Errorf("movimentações = %d, want %d (a migração não gera Movimentação)", n, movsAntes)
	}
}

func TestMigrarSaldoParaLotes_Idempotente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	p, e, _ := seedProdutoComSaldo(t, db, "Migra Idempotente", 4)

	if res, err := MigrarSaldoParaLotes(db); err != nil || res.Migrados != 1 {
		t.Fatalf("1ª execução = (%+v, %v), want Migrados=1", res, err)
	}
	res, err := MigrarSaldoParaLotes(db)
	if err != nil || res.Migrados != 0 {
		t.Fatalf("2ª execução = (%+v, %v), want Migrados=0", res, err)
	}
	if n := contarLotes(t, db, p, e); n != 1 {
		t.Errorf("lotes = %d, want 1", n)
	}
}

func TestMigrarSaldoParaLotes_LinhaZeradaNaoGeraLote(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	p, e, _ := seedProdutoComSaldo(t, db, "Migra Zerada", 0)

	res, err := MigrarSaldoParaLotes(db)
	if err != nil || res.Migrados != 0 {
		t.Fatalf("resultado = (%+v, %v), want Migrados=0", res, err)
	}
	if n := contarLotes(t, db, p, e); n != 0 {
		t.Errorf("lotes = %d, want 0", n)
	}
	var linhas int
	if err := db.QueryRow(`SELECT count(*) FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2`, p, e).Scan(&linhas); err != nil || linhas != 1 {
		t.Errorf("linha zerada deve permanecer: linhas=%d err=%v", linhas, err)
	}
}

func TestMigrarSaldoParaLotes_SemSaldo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	res, err := MigrarSaldoParaLotes(db)
	if err != nil || res.Migrados != 0 || res.Quantidade != 0 {
		t.Fatalf("resultado = (%+v, %v), want vazio", res, err)
	}
}

func TestMigrarSaldoParaLotes_EmpresaNaoBackfilledAbortaSemEscrever(t *testing.T) {
	db := testDB(t)
	casos := map[string]func(t *testing.T, produtoID, estoqueID string){
		"produto sem empresa": func(t *testing.T, produtoID, _ string) {
			if _, err := db.Exec(`UPDATE produtos SET empresa_id = NULL WHERE id = $1`, produtoID); err != nil {
				t.Fatal(err)
			}
		},
		"estoque sem empresa": func(t *testing.T, _, estoqueID string) {
			if _, err := db.Exec(`UPDATE estoques SET empresa_id = NULL WHERE id = $1`, estoqueID); err != nil {
				t.Fatal(err)
			}
		},
		"empresas distintas": func(t *testing.T, _, estoqueID string) {
			alheia := empresaAlheiaLotes(t, db)
			if _, err := db.Exec(`UPDATE estoques SET empresa_id = $2 WHERE id = $1`, estoqueID, alheia.ID); err != nil {
				t.Fatal(err)
			}
		},
	}
	sufixos := map[string]string{"produto sem empresa": "P", "estoque sem empresa": "E", "empresas distintas": "D"}
	for nome, quebrar := range casos {
		sufixo := sufixos[nome]
		t.Run(nome, func(t *testing.T) {
			limparProdutos(t, db)
			t.Cleanup(func() { limparProdutos(t, db) })
			// Um par saudável junto: o abort é em bloco, nada é escrito.
			pOk, eOk, _ := seedProdutoComSaldo(t, db, "Migra Saudavel "+sufixo, 3)
			p, e, _ := seedProdutoComSaldo(t, db, "Migra Quebrada "+sufixo, 5)
			quebrar(t, p, e)

			res, err := MigrarSaldoParaLotes(db)
			var erroSaldo *ErroSaldoNaoMigravel
			if !errors.As(err, &erroSaldo) {
				t.Fatalf("err = %v, want ErroSaldoNaoMigravel", err)
			}
			if len(erroSaldo.Linhas) != 1 || erroSaldo.Linhas[0].ProdutoID != p {
				t.Errorf("linhas = %+v, want só o par quebrado", erroSaldo.Linhas)
			}
			if res.Migrados != 0 {
				t.Errorf("Migrados = %d, want 0", res.Migrados)
			}
			if n := contarLotesTotal(t, db); n != 0 {
				t.Errorf("lotes = %d, want 0 (nada escrito)", n)
			}
			if s := saldoProdutoEstoque(t, db, pOk, eOk); s != 3 {
				t.Errorf("produto_estoque saudável = %v, want 3 (intocado)", s)
			}
			if s := saldoProdutoEstoque(t, db, p, e); s != 5 {
				t.Errorf("produto_estoque quebrado = %v, want 5 (intocado)", s)
			}
		})
	}
}

func TestMigrarSaldoParaLotes_SaldoNovoPosCorte(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	p, e, _ := seedProdutoComSaldo(t, db, "Migra Pos Corte", 6)
	if _, err := MigrarSaldoParaLotes(db); err != nil {
		t.Fatal(err)
	}
	// Cadastro/importação ainda escrevem em produto_estoque até a 11.6.
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 7 WHERE produto_id = $1 AND estoque_id = $2`, p, e); err != nil {
		t.Fatal(err)
	}
	totalAntes := saldoTotalView(t, db)

	res, err := MigrarSaldoParaLotes(db)
	if err != nil || res.Migrados != 1 || res.Quantidade != 7 {
		t.Fatalf("resultado = (%+v, %v), want Migrados=1 Quantidade=7", res, err)
	}
	if n := contarLotes(t, db, p, e); n != 2 {
		t.Errorf("lotes = %d, want 2 (sem duplicar o já migrado)", n)
	}
	if got := saldoTotalView(t, db); got != totalAntes || got != 13 {
		t.Errorf("saldo total = %v, want 13 (antes %v)", got, totalAntes)
	}
}

func TestMigrarSaldoParaLotes_DuasEmpresas(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	alheia := empresaAlheiaLotes(t, db)
	pA, eA, _ := seedProdutoComSaldo(t, db, "Migra Empresa Teste", 3)
	pB, eB, _ := seedProdutoComSaldo(t, db, "Migra Empresa Alheia", 8)
	if _, err := db.Exec(`UPDATE produtos SET empresa_id = $2 WHERE id = $1`, pB, alheia.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE estoques SET empresa_id = $2 WHERE id = $1`, eB, alheia.ID); err != nil {
		t.Fatal(err)
	}

	res, err := MigrarSaldoParaLotes(db)
	if err != nil || res.Migrados != 2 {
		t.Fatalf("resultado = (%+v, %v), want Migrados=2", res, err)
	}
	for _, c := range []struct{ produto, estoque, empresa string }{{pA, eA, empresaTeste}, {pB, eB, alheia.ID}} {
		var empresa string
		if err := db.QueryRow(`SELECT empresa_id::text FROM lotes WHERE produto_id = $1 AND estoque_id = $2`, c.produto, c.estoque).Scan(&empresa); err != nil {
			t.Fatal(err)
		}
		if empresa != c.empresa {
			t.Errorf("empresa do lote = %s, want %s", empresa, c.empresa)
		}
	}
}

func TestMigrarSaldoParaLotes_LoteExistenteNaoAlterado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	p, e, u := seedProdutoComSaldo(t, db, "Migra Lote Previo", 5)
	previo, err := LancarSaldo(db, empresaTeste, u, p, e, 9, "2999-01-01")
	if err != nil {
		t.Fatal(err)
	}

	res, err := MigrarSaldoParaLotes(db)
	if err != nil || res.Migrados != 1 {
		t.Fatalf("resultado = (%+v, %v), want Migrados=1", res, err)
	}
	if n := contarLotes(t, db, p, e); n != 2 {
		t.Errorf("lotes = %d, want 2", n)
	}
	var qtd float64
	var validade string
	if err := db.QueryRow(`SELECT quantidade::float8, data_validade::text FROM lotes WHERE id = $1`, previo.ID).Scan(&qtd, &validade); err != nil {
		t.Fatal(err)
	}
	if qtd != 9 || validade != "2999-01-01" {
		t.Errorf("lote prévio alterado: (%v,%s)", qtd, validade)
	}
	if got := saldoTotalView(t, db); got != 14 {
		t.Errorf("saldo total = %v, want 14", got)
	}
}

func TestDiagnosticarSaldoLotes_NaoEscreve(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	seedProdutoComSaldo(t, db, "Diag A", 10)
	seedProdutoComSaldo(t, db, "Diag B", 0)
	somaAntes := somaProdutoEstoque(t, db)

	d, err := DiagnosticarSaldoLotes(db)
	if err != nil {
		t.Fatal(err)
	}
	if d.LinhasAMigrar != 1 || d.LinhasZeradasPuladas != 1 || d.QuantidadeTotal != 10 || d.LotesExistentes != 0 || len(d.Problemas) != 0 {
		t.Errorf("diagnóstico = %+v", d)
	}
	if n := contarLotesTotal(t, db); n != 0 {
		t.Errorf("lotes = %d, want 0", n)
	}
	if got := somaProdutoEstoque(t, db); got != somaAntes {
		t.Errorf("produto_estoque mudou: %v -> %v", somaAntes, got)
	}
}

// O corte trava a linha de produto_estoque (FOR UPDATE OF pe), o mesmo lock de
// Baixa/Transferência/Pedido: com uma escrita concorrente em andamento, ele
// espera o commit e migra a quantidade JÁ atualizada — nunca um valor velho.
func TestMigrarSaldoParaLotes_SerializaComEscritaConcorrente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	p, e, _ := seedProdutoComSaldo(t, db, "Migra Concorrente", 10)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var q float64
	if err := tx.QueryRow(`SELECT quantidade::float8 FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2 FOR UPDATE`, p, e).Scan(&q); err != nil {
		t.Fatal(err)
	}

	type saida struct {
		res ResultadoMigracaoSaldoLotes
		err error
	}
	feito := make(chan saida, 1)
	go func() {
		res, err := MigrarSaldoParaLotes(db)
		feito <- saida{res, err}
	}()

	select {
	case s := <-feito:
		t.Fatalf("o corte não esperou o lock da linha: %+v %v", s.res, s.err)
	case <-time.After(500 * time.Millisecond):
	}

	// "Baixa" concorrente: debita 6 e libera o lock.
	if _, err := tx.Exec(`UPDATE produto_estoque SET quantidade = quantidade - 6 WHERE produto_id = $1 AND estoque_id = $2`, p, e); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	select {
	case s := <-feito:
		if s.err != nil || s.res.Migrados != 1 || s.res.Quantidade != 4 {
			t.Fatalf("resultado = (%+v, %v), want Migrados=1 Quantidade=4", s.res, s.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("o corte não terminou após o commit da escrita concorrente")
	}

	var qtd float64
	if err := db.QueryRow(`SELECT quantidade::float8 FROM lotes WHERE produto_id = $1 AND estoque_id = $2`, p, e).Scan(&qtd); err != nil || qtd != 4 {
		t.Errorf("lote = %v (err %v), want 4 (quantidade pós-Baixa)", qtd, err)
	}
	if s := saldoProdutoEstoque(t, db, p, e); s != 0 {
		t.Errorf("produto_estoque = %v, want 0", s)
	}
	if got := saldoTotalView(t, db); got != 4 {
		t.Errorf("saldo total = %v, want 4", got)
	}
}
