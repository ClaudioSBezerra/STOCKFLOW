package services

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Story 11.3 — reserva de saldo ao enviar Pedido.

func contarReservasPedido(t *testing.T, db *sql.DB, pedidoID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM reservas_pedido_item WHERE pedido_id = $1`, pedidoID).Scan(&n); err != nil {
		t.Fatalf("count reservas: %v", err)
	}
	return n
}

func disponivelDoPar(t *testing.T, db *sql.DB, produtoID, estoqueID string) float64 {
	t.Helper()
	d, err := saldoDisponivelParTx(db, empresaTeste, produtoID, estoqueID)
	if err != nil {
		t.Fatalf("saldoDisponivelParTx: %v", err)
	}
	return d
}

func TestSubmeterPedido_CriaUmaReservaPorItemSemMexerNoSaldoFisico(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "Reserva Feliz", "reserva-feliz@empresa.com", PapelUsuario, 0)
	pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Reserva Feliz A", SaldoInicial: 10, QtdSolicitada: 4},
		{NomeBase: "Reserva Feliz B", SaldoInicial: 5, QtdSolicitada: 5},
	})

	if n := contarReservasPedido(t, db, pedido.ID); n != 2 {
		t.Fatalf("reservas = %d, want 2", n)
	}
	if saldo := saldoProdutoEstoque(t, db, pares[0].ProdutoID, pares[0].EstoqueID); saldo != 10 {
		t.Errorf("saldo físico A = %v, want 10 (inalterado)", saldo)
	}
	if d := disponivelDoPar(t, db, pares[0].ProdutoID, pares[0].EstoqueID); d != 6 {
		t.Errorf("disponível A = %v, want 6", d)
	}
	if d := disponivelDoPar(t, db, pares[1].ProdutoID, pares[1].EstoqueID); d != 0 {
		t.Errorf("disponível B = %v, want 0", d)
	}
	if len(pedido.ProdutoIDs) != 2 {
		t.Errorf("ProdutoIDs = %v, want 2 ids", pedido.ProdutoIDs)
	}
}

func TestSubmeterPedido_ReservaContraSaldoSoEmLote(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, almoxID := seedProdutoComSaldo(t, db, "Reserva So Lote", 0)
	if _, err := LancarSaldo(db, empresaTeste, almoxID, produtoID, estoqueID, 5, "2999-01-01"); err != nil {
		t.Fatal(err)
	}
	usuarioID := semearConta(t, db, "Reserva So Lote U", "reserva-so-lote@empresa.com", PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoID, estoqueID, 3); err != nil {
		t.Fatalf("AdicionarItemCarrinho contra Lote: %v", err)
	}
	pedido, err := SubmeterPedido(db, empresaTeste, usuarioID, "Sol", "Obra", "")
	if err != nil {
		t.Fatalf("SubmeterPedido: %v", err)
	}
	if n := contarReservasPedido(t, db, pedido.ID); n != 1 {
		t.Errorf("reservas = %d, want 1", n)
	}
	if d := disponivelDoPar(t, db, produtoID, estoqueID); d != 2 {
		t.Errorf("disponível = %v, want 2", d)
	}
}

func TestSubmeterPedido_CorridaMesmoSaldoSoUmReserva(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Reserva Corrida", 5)
	u1 := semearConta(t, db, "Corrida 1", "reserva-corrida-1@empresa.com", PapelUsuario, 0)
	u2 := semearConta(t, db, "Corrida 2", "reserva-corrida-2@empresa.com", PapelUsuario, 1)
	for _, u := range []string{u1, u2} {
		if _, err := AdicionarItemCarrinho(db, empresaTeste, u, produtoID, estoqueID, 5); err != nil {
			t.Fatalf("seed carrinho: %v", err)
		}
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	erros := make([]error, 2)
	for i, u := range []string{u1, u2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, erros[i] = SubmeterPedido(db, empresaTeste, u, "Sol", "Obra", "")
		}()
	}
	close(start)
	wg.Wait()

	sucessos, indisponiveis := 0, 0
	for _, err := range erros {
		var indisp *ErroPedidoIndisponivel
		switch {
		case err == nil:
			sucessos++
		case errors.As(err, &indisp):
			indisponiveis++
		default:
			t.Fatalf("erro inesperado: %v", err)
		}
	}
	if sucessos != 1 || indisponiveis != 1 {
		t.Fatalf("sucessos=%d indisponiveis=%d, want 1 e 1", sucessos, indisponiveis)
	}
	var reservas int
	if err := db.QueryRow(`SELECT count(*) FROM reservas_pedido_item`).Scan(&reservas); err != nil || reservas != 1 {
		t.Errorf("reservas = %d (err %v), want 1", reservas, err)
	}
	if got := contarPedidos(t, db); got != 1 {
		t.Errorf("pedidos = %d, want 1 (nada gravado pelo perdedor)", got)
	}
}

func TestSubmeterPedido_CorridaSaldoSoEmLoteSoUmReserva(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, almoxID := seedProdutoComSaldo(t, db, "Reserva Corrida Lote", 0)
	if _, err := LancarSaldo(db, empresaTeste, almoxID, produtoID, estoqueID, 5, "2999-01-01"); err != nil {
		t.Fatal(err)
	}
	u1 := semearConta(t, db, "Corrida Lote 1", "reserva-corrida-lote-1@empresa.com", PapelUsuario, 0)
	u2 := semearConta(t, db, "Corrida Lote 2", "reserva-corrida-lote-2@empresa.com", PapelUsuario, 1)
	for _, u := range []string{u1, u2} {
		if _, err := AdicionarItemCarrinho(db, empresaTeste, u, produtoID, estoqueID, 5); err != nil {
			t.Fatalf("seed carrinho: %v", err)
		}
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	erros := make([]error, 2)
	for i, u := range []string{u1, u2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, erros[i] = SubmeterPedido(db, empresaTeste, u, "Sol", "Obra", "")
		}()
	}
	close(start)
	wg.Wait()

	sucessos, indisponiveis := 0, 0
	for _, err := range erros {
		var indisp *ErroPedidoIndisponivel
		switch {
		case err == nil:
			sucessos++
		case errors.As(err, &indisp):
			indisponiveis++
		default:
			t.Fatalf("erro inesperado: %v", err)
		}
	}
	if sucessos != 1 || indisponiveis != 1 {
		t.Fatalf("sucessos=%d indisponiveis=%d, want 1 e 1", sucessos, indisponiveis)
	}
	var reservas int
	if err := db.QueryRow(`SELECT count(*) FROM reservas_pedido_item`).Scan(&reservas); err != nil || reservas != 1 {
		t.Errorf("reservas = %d (err %v), want 1", reservas, err)
	}
}

// O envio precisa esperar o lock das linhas de `lotes` do par: uma transação
// externa segura a linha do Lote e o envio só pode concluir depois que ela
// commita. Sem `FOR UPDATE` em lotes, o envio terminaria durante a espera e
// dois envios contra saldo só em Lote poderiam reservar o mesmo saldo.
func TestSubmeterPedido_SaldoSoEmLoteEsperaLockDoLote(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, almoxID := seedProdutoComSaldo(t, db, "Reserva Espera Lote", 0)
	if _, err := LancarSaldo(db, empresaTeste, almoxID, produtoID, estoqueID, 5, "2999-01-01"); err != nil {
		t.Fatal(err)
	}
	usuarioID := semearConta(t, db, "Espera Lote U", "reserva-espera-lote@empresa.com", PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoID, estoqueID, 5); err != nil {
		t.Fatal(err)
	}

	holder, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.Exec(`SELECT id FROM lotes WHERE produto_id = $1 AND estoque_id = $2 FOR UPDATE`, produtoID, estoqueID); err != nil {
		t.Fatalf("segurar lock do lote: %v", err)
	}

	feito := make(chan error, 1)
	go func() {
		_, err := SubmeterPedido(db, empresaTeste, usuarioID, "Sol", "Obra", "")
		feito <- err
	}()

	select {
	case err := <-feito:
		t.Fatalf("envio concluiu (err=%v) com o lock do Lote seguro por outra transação", err)
	case <-time.After(400 * time.Millisecond):
	}
	if err := holder.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-feito:
		if err != nil {
			t.Fatalf("envio após liberar o lock: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("envio não concluiu após liberar o lock do Lote")
	}
}

// Dois envios concorrentes com carrinhos sobre os MESMOS dois pares, montados
// em ordem oposta: sem a ordem canônica de locks (AD-10) isso derrubaria um
// dos envios com deadlock (40P01). Saldo de sobra: ambos devem ter sucesso.
func TestSubmeterPedido_ConcorrenteVariosParesSemDeadlock(t *testing.T) {
	db := testDB(t)
	for rodada := 0; rodada < 5; rodada++ {
		limparProdutos(t, db)
		pA, eA, _ := seedProdutoComSaldo(t, db, fmt.Sprintf("Reserva Par A%d", rodada), 100)
		pB, eB, _ := seedProdutoComSaldo(t, db, fmt.Sprintf("Reserva Par B%d", rodada), 100)
		u1 := semearConta(t, db, "Par 1", fmt.Sprintf("reserva-par-1-%d@empresa.com", rodada), PapelUsuario, 0)
		u2 := semearConta(t, db, "Par 2", fmt.Sprintf("reserva-par-2-%d@empresa.com", rodada), PapelUsuario, 1)
		for _, it := range [][2]string{{pA, eA}, {pB, eB}} {
			if _, err := AdicionarItemCarrinho(db, empresaTeste, u1, it[0], it[1], 1); err != nil {
				t.Fatalf("seed carrinho u1: %v", err)
			}
		}
		for _, it := range [][2]string{{pB, eB}, {pA, eA}} {
			if _, err := AdicionarItemCarrinho(db, empresaTeste, u2, it[0], it[1], 1); err != nil {
				t.Fatalf("seed carrinho u2: %v", err)
			}
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		erros := make([]error, 2)
		for i, u := range []string{u1, u2} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, erros[i] = SubmeterPedido(db, empresaTeste, u, "Sol", "Obra", "")
			}()
		}
		close(start)
		wg.Wait()

		for i, err := range erros {
			if err != nil {
				t.Fatalf("rodada %d: envio %d falhou: %v", rodada, i, err)
			}
		}
		var reservas int
		if err := db.QueryRow(`SELECT count(*) FROM reservas_pedido_item`).Scan(&reservas); err != nil || reservas != 4 {
			t.Errorf("rodada %d: reservas = %d (err %v), want 4", rodada, reservas, err)
		}
	}
}

func TestSubmeterPedido_SaldoJaReservadoRejeitaSegundoEnvio(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Reserva Ja Reservado", 5)
	ua := semearConta(t, db, "Reserva A", "reserva-ja-a@empresa.com", PapelUsuario, 0)
	ub := semearConta(t, db, "Reserva B", "reserva-ja-b@empresa.com", PapelUsuario, 1)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, ua, produtoID, estoqueID, 4); err != nil {
		t.Fatal(err)
	}
	// B monta o carrinho ANTES de A enviar (o carrinho não trava saldo).
	if _, err := AdicionarItemCarrinho(db, empresaTeste, ub, produtoID, estoqueID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmeterPedido(db, empresaTeste, ua, "A", "Obra", ""); err != nil {
		t.Fatalf("envio de A: %v", err)
	}

	_, err := SubmeterPedido(db, empresaTeste, ub, "B", "Obra", "")
	var indisp *ErroPedidoIndisponivel
	if !errors.As(err, &indisp) {
		t.Fatalf("erro = %v, want *ErroPedidoIndisponivel", err)
	}
	if len(indisp.Itens) != 1 || !strings.Contains(indisp.Itens[0], "Reserva Ja Reservado") {
		t.Errorf("Itens = %v, want o nome do produto", indisp.Itens)
	}
	if n := contarItensCarrinho(t, db, ub); n != 1 {
		t.Errorf("carrinho de B = %d itens, want 1 (inalterado)", n)
	}
}

func TestAdicionarItemCarrinho_ValidaContraDisponivel(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Reserva Carrinho", 5)
	ua := semearConta(t, db, "Carrinho A", "reserva-carrinho-a@empresa.com", PapelUsuario, 0)
	ub := semearConta(t, db, "Carrinho B", "reserva-carrinho-b@empresa.com", PapelUsuario, 1)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, ua, produtoID, estoqueID, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmeterPedido(db, empresaTeste, ua, "A", "Obra", ""); err != nil {
		t.Fatal(err)
	}

	_, err := AdicionarItemCarrinho(db, empresaTeste, ub, produtoID, estoqueID, 2)
	var indisp *ErroCarrinhoIndisponivel
	if !errors.As(err, &indisp) || indisp.Restante != 1 {
		t.Fatalf("erro = %v, want ErroCarrinhoIndisponivel{Restante:1}", err)
	}
	if _, err := AdicionarItemCarrinho(db, empresaTeste, ub, produtoID, estoqueID, 1); err != nil {
		t.Errorf("adicionar o disponível (1) deveria passar: %v", err)
	}
}

func TestDecidirPedido_LiberaReservas(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "Libera U", "libera-u@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Libera Almox", "libera-almox@empresa.com", PapelAlmoxarife, 0)

	t.Run("rejeicao", func(t *testing.T) {
		pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
			{NomeBase: "Libera Rejeita", SaldoInicial: 5, QtdSolicitada: 5},
		})
		if d := disponivelDoPar(t, db, pares[0].ProdutoID, pares[0].EstoqueID); d != 0 {
			t.Fatalf("disponível antes = %v, want 0", d)
		}
		if _, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, false); err != nil {
			t.Fatal(err)
		}
		if n := contarReservasPedido(t, db, pedido.ID); n != 0 {
			t.Errorf("reservas = %d, want 0", n)
		}
		if d := disponivelDoPar(t, db, pares[0].ProdutoID, pares[0].EstoqueID); d != 5 {
			t.Errorf("disponível depois = %v, want 5", d)
		}
	})

	t.Run("aprovacao parcial", func(t *testing.T) {
		pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
			{NomeBase: "Libera Parcial", SaldoInicial: 5, QtdSolicitada: 5},
		})
		// Baixa concorrente entre envio e decisão: saldo físico cai para 3.
		if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 3 WHERE produto_id = $1 AND estoque_id = $2`,
			pares[0].ProdutoID, pares[0].EstoqueID); err != nil {
			t.Fatal(err)
		}
		det, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
		if err != nil {
			t.Fatal(err)
		}
		if det.Status != "parcialmente_aprovado" {
			t.Errorf("status = %q", det.Status)
		}
		if n := contarReservasPedido(t, db, pedido.ID); n != 0 {
			t.Errorf("reservas = %d, want 0", n)
		}
		// Disponível = físico pós-débito (3 − 3 = 0).
		if d := disponivelDoPar(t, db, pares[0].ProdutoID, pares[0].EstoqueID); d != 0 {
			t.Errorf("disponível = %v, want 0", d)
		}
	})

	t.Run("aprovacao total", func(t *testing.T) {
		pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
			{NomeBase: "Libera Total", SaldoInicial: 6, QtdSolicitada: 2},
		})
		if _, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true); err != nil {
			t.Fatal(err)
		}
		if n := contarReservasPedido(t, db, pedido.ID); n != 0 {
			t.Errorf("reservas = %d, want 0", n)
		}
		if d := disponivelDoPar(t, db, pares[0].ProdutoID, pares[0].EstoqueID); d != 4 {
			t.Errorf("disponível = %v, want 4 (6 − 2 debitado, sem reserva residual)", d)
		}
	})
}

func TestReserva_PedidoPendenteAntigoMantemReserva(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "Antigo U", "reserva-antigo@empresa.com", PapelUsuario, 0)
	pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Reserva Antigo", SaldoInicial: 5, QtdSolicitada: 3},
	})
	if _, err := db.Exec(`UPDATE pedidos SET criado_em = now() - interval '200 days' WHERE id = $1`, pedido.ID); err != nil {
		t.Fatal(err)
	}
	if n := contarReservasPedido(t, db, pedido.ID); n != 1 {
		t.Errorf("reservas = %d, want 1 (sem expiração)", n)
	}
	if d := disponivelDoPar(t, db, pares[0].ProdutoID, pares[0].EstoqueID); d != 2 {
		t.Errorf("disponível = %v, want 2", d)
	}
}

func TestObterProdutoDetalhe_ReservadoEDisponivel(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "Detalhe Reserva", "detalhe-reserva@empresa.com", PapelUsuario, 0)
	_, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Detalhe Reserva", SaldoInicial: 10, QtdSolicitada: 4},
	})

	det, err := ObterProdutoDetalhe(db, empresaTeste, pares[0].ProdutoID)
	if err != nil {
		t.Fatal(err)
	}
	if det.QuantidadeTotal != 10 || det.QuantidadeReservada != 4 || det.QuantidadeDisponivel != 6 {
		t.Errorf("totais = %v/%v/%v, want 10/4/6", det.QuantidadeTotal, det.QuantidadeReservada, det.QuantidadeDisponivel)
	}
	if len(det.PorEstoque) != 1 {
		t.Fatalf("porEstoque = %d", len(det.PorEstoque))
	}
	eq := det.PorEstoque[0]
	if eq.Quantidade != 10 || eq.Reservada == nil || *eq.Reservada != 4 || eq.Disponivel == nil || *eq.Disponivel != 6 {
		t.Errorf("estoque = %+v", eq)
	}

	if !det.Disponivel {
		t.Error("Disponivel = false com 6 não reservados, want true")
	}

	// Reserva acima do saldo (ex.: backfill): disponível truncado em 0.
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 2 WHERE produto_id = $1`, pares[0].ProdutoID); err != nil {
		t.Fatal(err)
	}
	det, err = ObterProdutoDetalhe(db, empresaTeste, pares[0].ProdutoID)
	if err != nil {
		t.Fatal(err)
	}
	if *det.PorEstoque[0].Disponivel != 0 || det.QuantidadeDisponivel != 0 {
		t.Errorf("disponível = %v/%v, want 0/0", *det.PorEstoque[0].Disponivel, det.QuantidadeDisponivel)
	}
	// Produto 100% reservado NÃO aparece como disponível no detalhe (o saldo
	// físico segue > 0).
	if det.QuantidadeTotal != 2 || det.Disponivel {
		t.Errorf("total = %v, Disponivel = %v, want 2 e false", det.QuantidadeTotal, det.Disponivel)
	}
}

// Quantidades fracionárias: 1.1 − 0.8 = 0.30000000000000004 em float64; o
// detalhe arredonda para 3 casas.
func TestObterProdutoDetalhe_ReservaFracionariaArredondada(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "Fracao Reserva", "fracao-reserva@empresa.com", PapelUsuario, 0)
	_, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Fracao Reserva", SaldoInicial: 1.1, QtdSolicitada: 0.8},
	})

	det, err := ObterProdutoDetalhe(db, empresaTeste, pares[0].ProdutoID)
	if err != nil {
		t.Fatal(err)
	}
	eq := det.PorEstoque[0]
	if *eq.Reservada != 0.8 || *eq.Disponivel != 0.3 {
		t.Errorf("estoque reservada/disponivel = %v/%v, want 0.8/0.3", *eq.Reservada, *eq.Disponivel)
	}
	if det.QuantidadeReservada != 0.8 || det.QuantidadeDisponivel != 0.3 {
		t.Errorf("totais = %v/%v, want 0.8/0.3", det.QuantidadeReservada, det.QuantidadeDisponivel)
	}
}

// Saldo DIVIDIDO entre produto_estoque (legado) e lotes: 3 + 4 = 7.
func TestSubmeterPedido_SaldoDivididoEntreLegadoELote(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, almoxID := seedProdutoComSaldo(t, db, "Reserva Dividido", 3)
	if _, err := LancarSaldo(db, empresaTeste, almoxID, produtoID, estoqueID, 4, ""); err != nil {
		t.Fatal(err)
	}
	if d := disponivelDoPar(t, db, produtoID, estoqueID); d != 7 {
		t.Fatalf("disponível inicial = %v, want 7", d)
	}
	u8 := semearConta(t, db, "Dividido 8", "reserva-dividido-8@empresa.com", PapelUsuario, 0)
	u7 := semearConta(t, db, "Dividido 7", "reserva-dividido-7@empresa.com", PapelUsuario, 1)

	if _, err := AdicionarItemCarrinho(db, empresaTeste, u8, produtoID, estoqueID, 8); err == nil {
		t.Fatal("adicionar 8 ao carrinho deveria falhar (7 disponíveis)")
	}
	// Carrinho de 8 montado direto no banco para exercitar a recusa no ENVIO.
	if _, err := db.Exec(`INSERT INTO carrinho_itens (usuario_id, produto_id, estoque_id, quantidade) VALUES ($1, $2, $3, 8)`,
		u8, produtoID, estoqueID); err != nil {
		t.Fatal(err)
	}
	_, err := SubmeterPedido(db, empresaTeste, u8, "S", "O", "")
	var indisp *ErroPedidoIndisponivel
	if !errors.As(err, &indisp) {
		t.Fatalf("envio de 8: erro = %v, want *ErroPedidoIndisponivel", err)
	}

	if _, err := AdicionarItemCarrinho(db, empresaTeste, u7, produtoID, estoqueID, 7); err != nil {
		t.Fatalf("carrinho 7: %v", err)
	}
	pedido, err := SubmeterPedido(db, empresaTeste, u7, "S", "O", "")
	if err != nil {
		t.Fatalf("envio de 7 deveria passar: %v", err)
	}
	if n := contarReservasPedido(t, db, pedido.ID); n != 1 {
		t.Errorf("reservas = %d, want 1", n)
	}
	if d := disponivelDoPar(t, db, produtoID, estoqueID); d != 0 {
		t.Errorf("disponível final = %v, want 0", d)
	}
}

func TestListarReservasSaldo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "Lista Reserva", "lista-reserva@empresa.com", PapelUsuario, 0)
	pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Lista Reserva", SaldoInicial: 10, QtdSolicitada: 4},
	})

	lista, err := ListarReservasSaldo(db, empresaTeste, pares[0].ProdutoID, pares[0].EstoqueID)
	if err != nil {
		t.Fatal(err)
	}
	if len(lista) != 1 || lista[0].PedidoID != pedido.ID || lista[0].Solicitante != "Solicitante Decisao" || lista[0].Quantidade != 4 {
		t.Errorf("lista = %+v", lista)
	}
	if time.Since(lista[0].CriadoEm) > time.Minute {
		t.Errorf("criadoEm = %v, want recente", lista[0].CriadoEm)
	}

	outra := empresaAlheiaLotes(t, db)
	for nome, chamada := range map[string]func() error{
		"empresa alheia": func() error {
			_, err := ListarReservasSaldo(db, outra.ID, pares[0].ProdutoID, pares[0].EstoqueID)
			return err
		},
		"produto malformado": func() error {
			_, err := ListarReservasSaldo(db, empresaTeste, "nao-e-uuid", pares[0].EstoqueID)
			return err
		},
		"estoque malformado": func() error {
			_, err := ListarReservasSaldo(db, empresaTeste, pares[0].ProdutoID, "nao-e-uuid")
			return err
		},
		"produto inexistente": func() error {
			_, err := ListarReservasSaldo(db, empresaTeste, "00000000-0000-0000-0000-000000000000", pares[0].EstoqueID)
			return err
		},
	} {
		if err := chamada(); !errors.Is(err, ErrReservaAlvoNaoEncontrado) {
			t.Errorf("%s: erro = %v, want ErrReservaAlvoNaoEncontrado", nome, err)
		}
	}

	if _, err := DecidirPedido(db, empresaTeste, pedido.ID, usuarioID, PapelAlmoxarife, false); err != nil {
		t.Fatal(err)
	}
	lista, err = ListarReservasSaldo(db, empresaTeste, pares[0].ProdutoID, pares[0].EstoqueID)
	if err != nil || len(lista) != 0 {
		t.Errorf("após decisão: lista = %+v, err = %v, want vazia", lista, err)
	}
}

func TestMesclarDuplicatas_SomaReservasColididas(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	estoque, err := CriarEstoque(db, empresaTeste, "Estoque Mesclagem Reservas")
	if err != nil {
		t.Fatal(err)
	}
	almoxID := semearConta(t, db, "Almox Mescla Reservas", "mescla-reservas-almox@empresa.com", PapelAlmoxarife, 0)
	usuarioID := semearConta(t, db, "U Mescla Reservas", "mescla-reservas-u@empresa.com", PapelUsuario, 0)
	dims := CriarProdutoInput{UnidadeMedida: "un", Diametro: &DimensaoInput{Valor: ptrFloat(40), Unidade: ptrStr("mm")}}
	a := seedProdutoParaMesclagem(t, db, "Tubo Reserva 40mm", estoque.ID, 10, dims)
	b := seedProdutoParaMesclagem(t, db, "Tubo Reserva 40mm", estoque.ID, 10, dims)
	// O MESMO Pedido reserva os dois "duplicados" no mesmo Estoque.
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, a, estoque.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, b, estoque.ID, 3); err != nil {
		t.Fatal(err)
	}
	pedido, err := SubmeterPedido(db, empresaTeste, usuarioID, "Sol", "Obra", "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := MesclarDuplicatas(db, empresaTeste, a, []string{b}, almoxID); err != nil {
		t.Fatalf("MesclarDuplicatas: %v", err)
	}
	rows, err := db.Query(`SELECT produto_id, quantidade FROM reservas_pedido_item WHERE pedido_id = $1`, pedido.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var pid string
		var q float64
		if err := rows.Scan(&pid, &q); err != nil {
			t.Fatal(err)
		}
		n++
		if pid != a || q != 5 {
			t.Errorf("reserva = (%s, %v), want (%s, 5)", pid, q, a)
		}
	}
	if n != 1 {
		t.Errorf("reservas = %d, want 1 (soma na colisão)", n)
	}
}

func TestMesclarDuplicatas_ReescreveReservaSemColisao(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	estoque, err := CriarEstoque(db, empresaTeste, "Estoque Mesclagem Reservas 2")
	if err != nil {
		t.Fatal(err)
	}
	almoxID := semearConta(t, db, "Almox Mescla Reservas 2", "mescla-reservas2-almox@empresa.com", PapelAlmoxarife, 0)
	usuarioID := semearConta(t, db, "U Mescla Reservas 2", "mescla-reservas2-u@empresa.com", PapelUsuario, 0)
	dims := CriarProdutoInput{UnidadeMedida: "un", Diametro: &DimensaoInput{Valor: ptrFloat(50), Unidade: ptrStr("mm")}}
	a := seedProdutoParaMesclagem(t, db, "Tubo Reserva 50mm", estoque.ID, 10, dims)
	b := seedProdutoParaMesclagem(t, db, "Tubo Reserva 50mm", estoque.ID, 10, dims)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, b, estoque.ID, 3); err != nil {
		t.Fatal(err)
	}
	pedido, err := SubmeterPedido(db, empresaTeste, usuarioID, "Sol", "Obra", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MesclarDuplicatas(db, empresaTeste, a, []string{b}, almoxID); err != nil {
		t.Fatalf("MesclarDuplicatas: %v", err)
	}
	var pid string
	var q float64
	if err := db.QueryRow(`SELECT produto_id, quantidade FROM reservas_pedido_item WHERE pedido_id = $1`, pedido.ID).Scan(&pid, &q); err != nil {
		t.Fatal(err)
	}
	if pid != a || q != 3 {
		t.Errorf("reserva = (%s, %v), want (%s, 3)", pid, q, a)
	}
}

// O backfill da migration 000043 cria uma reserva por item de Pedido JÁ
// pendente (e nenhuma para os decididos): reaplica o INSERT ... SELECT.
func TestMigration43_BackfillReservaDePedidosPendentes(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "Backfill U", "backfill-reserva@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Backfill Almox", "backfill-reserva-almox@empresa.com", PapelAlmoxarife, 0)
	pendente, _ := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Backfill Pendente", SaldoInicial: 5, QtdSolicitada: 2},
	})
	decidido, _ := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Backfill Decidido", SaldoInicial: 5, QtdSolicitada: 2},
	})
	if _, err := DecidirPedido(db, empresaTeste, decidido.ID, almoxID, PapelAlmoxarife, false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM reservas_pedido_item`); err != nil {
		t.Fatal(err)
	}

	sqlBytes, err := os.ReadFile("../migrations/000043_create_reservas_pedido_item.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	texto := string(sqlBytes)
	i := strings.LastIndex(texto, "INSERT INTO reservas_pedido_item")
	if i < 0 {
		t.Fatal("backfill não encontrado na migration")
	}
	if _, err := db.Exec(texto[i:]); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if n := contarReservasPedido(t, db, pendente.ID); n != 1 {
		t.Errorf("reservas do pendente = %d, want 1", n)
	}
	if n := contarReservasPedido(t, db, decidido.ID); n != 0 {
		t.Errorf("reservas do decidido = %d, want 0", n)
	}
}
