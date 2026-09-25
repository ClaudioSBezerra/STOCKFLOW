package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// Story 16.1 — testes de InativarProduto/ReativarProduto e das travas em
// LancarSaldo, carrinho e envio de Pedido.

const eanTesteInativacao = "7891000000014"

func criarProdutoInativacao(t *testing.T, db *sql.DB, empresaID, nome, ean string) string {
	t.Helper()
	p, err := CriarProduto(db, empresaID, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          nome,
		CategoriaID:   categoriaIDPorCodigoEmpresa(t, db, empresaID, "04.001"),
		TemplateID:    templateGenericoID(t, db, empresaID),
		EAN13:         ean,
	})
	if err != nil {
		t.Fatalf("CriarProduto %q: %v", nome, err)
	}
	return p.ID
}

func categoriaIDPorCodigoEmpresa(t *testing.T, db *sql.DB, empresaID, codigo string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE codigo = $1 AND empresa_id = $2`, codigo, empresaID).Scan(&id); err != nil {
		t.Fatalf("categoria %q: %v", codigo, err)
	}
	return id
}

type linhaHistorico struct {
	acao, atorID string
	detalhe      map[string]any
}

func historicoProduto(t *testing.T, db *sql.DB, produtoID string) []linhaHistorico {
	t.Helper()
	rows, err := db.Query(`SELECT acao, ator_id, detalhe, empresa_id FROM produto_historico WHERE produto_id = $1 ORDER BY criado_em, id`, produtoID)
	if err != nil {
		t.Fatalf("ler histórico: %v", err)
	}
	defer rows.Close()
	var out []linhaHistorico
	for rows.Next() {
		var l linhaHistorico
		var raw []byte
		var empresa string
		if err := rows.Scan(&l.acao, &l.atorID, &raw, &empresa); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &l.detalhe); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

func estadoInativacao(t *testing.T, db *sql.DB, produtoID string) (inativo bool, por sql.NullString) {
	t.Helper()
	if err := db.QueryRow(`SELECT inativado_em IS NOT NULL, inativado_por FROM produtos WHERE id = $1`, produtoID).Scan(&inativo, &por); err != nil {
		t.Fatalf("ler produto: %v", err)
	}
	return
}

func TestInativarProduto_OkComMotivo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestor := semearConta(t, db, "Gestora Inat", "gestora.inat@empresa.com", PapelGestor, 0)
	produtoID := criarProdutoInativacao(t, db, empresaTeste, "Produto Inat Ok", "")

	if err := InativarProduto(db, empresaTeste, gestor, produtoID, "  fora de linha  "); err != nil {
		t.Fatalf("InativarProduto: %v", err)
	}
	inativo, por := estadoInativacao(t, db, produtoID)
	if !inativo || por.String != gestor {
		t.Errorf("estado = (%v,%v), want (true,%s)", inativo, por, gestor)
	}
	h := historicoProduto(t, db, produtoID)
	if len(h) != 1 || h[0].acao != AcaoProdutoInativado || h[0].atorID != gestor || h[0].detalhe["motivo"] != "fora de linha" {
		t.Errorf("histórico = %+v", h)
	}
	det, err := ObterProdutoDetalhe(db, empresaTeste, produtoID)
	if err != nil {
		t.Fatalf("ObterProdutoDetalhe de inativo: %v", err)
	}
	if !det.Inativo || det.InativadoEm == nil {
		t.Errorf("detalhe inativo=%v inativadoEm=%v", det.Inativo, det.InativadoEm)
	}
}

func TestInativarProduto_SemMotivoGravaNull(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestor := semearConta(t, db, "Gestora Sem Motivo", "gestora.semmotivo@empresa.com", PapelGestor, 0)
	for _, motivo := range []string{"", "   "} {
		produtoID := criarProdutoInativacao(t, db, empresaTeste, "Produto Sem Motivo"+motivo, "")
		if err := InativarProduto(db, empresaTeste, gestor, produtoID, motivo); err != nil {
			t.Fatalf("InativarProduto(%q): %v", motivo, err)
		}
		h := historicoProduto(t, db, produtoID)
		if len(h) != 1 {
			t.Fatalf("histórico = %+v", h)
		}
		v, existe := h[0].detalhe["motivo"]
		if !existe || v != nil {
			t.Errorf("detalhe = %v, want motivo:null", h[0].detalhe)
		}
	}
}

func TestInativarProduto_MotivoLongoRecusado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestor := semearConta(t, db, "Gestora Longo", "gestora.longo@empresa.com", PapelGestor, 0)
	produtoID := criarProdutoInativacao(t, db, empresaTeste, "Produto Motivo Longo", "")

	var ev *ErroProdutoValidacaoInativacao
	if err := InativarProduto(db, empresaTeste, gestor, produtoID, strings.Repeat("é", 501)); !errors.As(err, &ev) {
		t.Fatalf("err = %v, want ErroProdutoValidacaoInativacao", err)
	}
	if err := InativarProduto(db, empresaTeste, gestor, produtoID, strings.Repeat("é", 500)); err != nil {
		t.Fatalf("500 runas deveria passar: %v", err)
	}
}

func TestInativarProduto_ComSaldoCitaEstoque(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, almox := seedProdutoComSaldo(t, db, "Almox A", 0)
	gestor := semearConta(t, db, "Gestora Saldo", "gestora.saldo@empresa.com", PapelGestor, 0)
	if _, err := LancarSaldo(db, empresaTeste, almox, produtoID, estoqueID, 5, ""); err != nil {
		t.Fatal(err)
	}

	err := InativarProduto(db, empresaTeste, gestor, produtoID, "x")
	var es *ErroProdutoComSaldo
	if !errors.As(err, &es) {
		t.Fatalf("err = %v, want ErroProdutoComSaldo", err)
	}
	if len(es.Estoques) != 1 || es.Estoques[0] != "Almox A" || es.TemReserva || !strings.Contains(es.Error(), "Almox A") {
		t.Errorf("erro = %+v / %q", es, es.Error())
	}
	if inativo, _ := estadoInativacao(t, db, produtoID); inativo {
		t.Error("produto ficou inativo")
	}
	if h := historicoProduto(t, db, produtoID); len(h) != 0 {
		t.Errorf("histórico gravado: %+v", h)
	}
}

func TestInativarProduto_ComReservaPendente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Almox Reserva", 3)
	gestor := semearConta(t, db, "Gestora Reserva", "gestora.reserva@empresa.com", PapelGestor, 0)
	usuario := semearConta(t, db, "Usuário Reserva", "usuario.reserva@empresa.com", PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuario, produtoID, estoqueID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmeterPedido(db, empresaTeste, usuario, "Fulano", "Obra X", ""); err != nil {
		t.Fatal(err)
	}
	// Zera o saldo físico: resta só a reserva.
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 0 WHERE produto_id = $1`, produtoID); err != nil {
		t.Fatal(err)
	}

	err := InativarProduto(db, empresaTeste, gestor, produtoID, "")
	var es *ErroProdutoComSaldo
	if !errors.As(err, &es) {
		t.Fatalf("err = %v, want ErroProdutoComSaldo", err)
	}
	if !es.TemReserva || len(es.Estoques) != 0 || !strings.Contains(es.Error(), "reserva de Pedido") {
		t.Errorf("erro = %+v / %q", es, es.Error())
	}
	if inativo, _ := estadoInativacao(t, db, produtoID); inativo {
		t.Error("produto ficou inativo")
	}
}

func TestInativarProduto_JaInativo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestor := semearConta(t, db, "Gestora Ja", "gestora.ja@empresa.com", PapelGestor, 0)
	produtoID := criarProdutoInativacao(t, db, empresaTeste, "Produto Já Inativo", "")
	if err := InativarProduto(db, empresaTeste, gestor, produtoID, ""); err != nil {
		t.Fatal(err)
	}
	if err := InativarProduto(db, empresaTeste, gestor, produtoID, ""); !errors.Is(err, ErrProdutoJaInativo) {
		t.Errorf("err = %v, want ErrProdutoJaInativo", err)
	}
}

func TestInativarReativar_NaoEncontrado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestor := semearConta(t, db, "Gestora 404", "gestora.404@empresa.com", PapelGestor, 0)
	alheia := empresaAlheiaLotes(t, db)
	produtoAlheio := criarProdutoInativacao(t, db, alheia.ID, "Produto Alheio Inat", "")
	mesclado := criarProdutoInativacao(t, db, empresaTeste, "Produto Mesclado Inat", "")
	if _, err := db.Exec(`UPDATE produtos SET deleted_at = now() WHERE id = $1`, mesclado); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{produtoAlheio, mesclado, "nao-e-uuid", "00000000-0000-0000-0000-000000000000"} {
		if err := InativarProduto(db, empresaTeste, gestor, id, ""); !errors.Is(err, ErrProdutoNaoEncontrado) {
			t.Errorf("Inativar(%s) = %v, want ErrProdutoNaoEncontrado", id, err)
		}
		if err := ReativarProduto(db, empresaTeste, gestor, id); !errors.Is(err, ErrProdutoNaoEncontrado) {
			t.Errorf("Reativar(%s) = %v, want ErrProdutoNaoEncontrado", id, err)
		}
	}
}

func TestReativarProduto_Ok(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestor := semearConta(t, db, "Gestora Reat", "gestora.reat@empresa.com", PapelGestor, 0)
	semEAN := criarProdutoInativacao(t, db, empresaTeste, "Produto Reat Sem EAN", "")
	comEAN := criarProdutoInativacao(t, db, empresaTeste, "Produto Reat Com EAN", eanTesteInativacao)
	for _, id := range []string{semEAN, comEAN} {
		if err := InativarProduto(db, empresaTeste, gestor, id, "motivo"); err != nil {
			t.Fatal(err)
		}
		if err := ReativarProduto(db, empresaTeste, gestor, id); err != nil {
			t.Fatalf("ReativarProduto: %v", err)
		}
		inativo, por := estadoInativacao(t, db, id)
		if inativo || por.Valid {
			t.Errorf("estado = (%v,%v), want ativo", inativo, por)
		}
		h := historicoProduto(t, db, id)
		if len(h) != 2 || h[1].acao != AcaoProdutoReativado || len(h[1].detalhe) != 0 {
			t.Errorf("histórico = %+v", h)
		}
		det, err := ObterProdutoDetalhe(db, empresaTeste, id)
		if err != nil || det.Inativo || det.InativadoEm != nil {
			t.Errorf("detalhe = %+v err=%v", det, err)
		}
	}
}

func TestReativarProduto_JaAtivo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestor := semearConta(t, db, "Gestora Ativo", "gestora.ativo@empresa.com", PapelGestor, 0)
	produtoID := criarProdutoInativacao(t, db, empresaTeste, "Produto Ativo", "")
	if err := ReativarProduto(db, empresaTeste, gestor, produtoID); !errors.Is(err, ErrProdutoJaAtivo) {
		t.Errorf("err = %v, want ErrProdutoJaAtivo", err)
	}
}

func TestReativarProduto_EANEmUso(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestor := semearConta(t, db, "Gestora EAN", "gestora.ean@empresa.com", PapelGestor, 0)
	antigo := criarProdutoInativacao(t, db, empresaTeste, "Produto EAN Antigo", eanTesteInativacao)
	if err := InativarProduto(db, empresaTeste, gestor, antigo, ""); err != nil {
		t.Fatal(err)
	}
	novo := criarProdutoInativacao(t, db, empresaTeste, "Produto EAN Novo", eanTesteInativacao)
	var codigoNovo string
	if err := db.QueryRow(`SELECT codigo FROM produtos WHERE id = $1`, novo).Scan(&codigoNovo); err != nil {
		t.Fatal(err)
	}
	// O mesmo EAN noutra Empresa não conta.
	alheia := empresaAlheiaLotes(t, db)
	criarProdutoInativacao(t, db, alheia.ID, "Produto EAN Alheio", eanTesteInativacao)

	err := ReativarProduto(db, empresaTeste, gestor, antigo)
	var ee *ErroEANEmUso
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v, want ErroEANEmUso", err)
	}
	want := "Este EAN já está no produto " + codigoNovo + " — Produto EAN Novo"
	if ee.Error() != want {
		t.Errorf("mensagem = %q, want %q", ee.Error(), want)
	}
	if inativo, _ := estadoInativacao(t, db, antigo); !inativo {
		t.Error("produto foi reativado apesar do EAN em uso")
	}
	if h := historicoProduto(t, db, antigo); len(h) != 1 {
		t.Errorf("histórico = %+v, want só a inativação", h)
	}

	// Com o outro inativado, o EAN fica livre.
	if err := InativarProduto(db, empresaTeste, gestor, novo, ""); err != nil {
		t.Fatal(err)
	}
	if err := ReativarProduto(db, empresaTeste, gestor, antigo); err != nil {
		t.Errorf("Reativar com EAN livre: %v", err)
	}
}

func TestLancarSaldo_ProdutoInativoRecusado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, almox := seedProdutoComSaldo(t, db, "Lotes Inativo", 0)
	gestor := semearConta(t, db, "Gestora Lote Inat", "gestora.loteinat@empresa.com", PapelGestor, 0)
	if err := InativarProduto(db, empresaTeste, gestor, produtoID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := LancarSaldo(db, empresaTeste, almox, produtoID, estoqueID, 1, ""); !errors.Is(err, ErrProdutoInativo) {
		t.Errorf("err = %v, want ErrProdutoInativo", err)
	}
	if n := contarLotes(t, db, produtoID, estoqueID); n != 0 {
		t.Errorf("lotes = %d, want 0", n)
	}
	// Estoque inexistente continua 404, mesmo com Produto inativo.
	if _, err := LancarSaldo(db, empresaTeste, almox, produtoID, "00000000-0000-0000-0000-000000000000", 1, ""); !errors.Is(err, ErrLoteAlvoNaoEncontrado) {
		t.Errorf("estoque inexistente: err = %v, want ErrLoteAlvoNaoEncontrado", err)
	}
}

// TestInativarProduto_CorridaComLancarSaldo: inativar e lançar saldo em
// paralelo nunca terminam com um Produto inativo com saldo.
func TestInativarProduto_CorridaComLancarSaldo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestor := semearConta(t, db, "Gestora Corrida", "gestora.corrida@empresa.com", PapelGestor, 0)
	for i := 0; i < 10; i++ {
		produtoID, estoqueID, almox := seedProdutoComSaldo(t, db, "Corrida Inat "+string(rune('A'+i)), 0)

		start := make(chan struct{})
		var wg sync.WaitGroup
		var errInativar, errLancar error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			errInativar = InativarProduto(db, empresaTeste, gestor, produtoID, "")
		}()
		go func() {
			defer wg.Done()
			<-start
			_, errLancar = LancarSaldo(db, empresaTeste, almox, produtoID, estoqueID, 4, "")
		}()
		close(start)
		wg.Wait()

		if errInativar == nil && errLancar == nil {
			t.Fatalf("iteração %d: as duas operações tiveram sucesso — produto inativo com saldo", i)
		}
		inativo, _ := estadoInativacao(t, db, produtoID)
		lotes := contarLotes(t, db, produtoID, estoqueID)
		if inativo && lotes > 0 {
			t.Fatalf("iteração %d: inativo com %d lote(s)", i, lotes)
		}
		if errInativar == nil {
			if !errors.Is(errLancar, ErrProdutoInativo) {
				t.Errorf("iteração %d: LancarSaldo = %v, want ErrProdutoInativo", i, errLancar)
			}
		} else {
			var es *ErroProdutoComSaldo
			if !errors.As(errInativar, &es) || errLancar != nil {
				t.Errorf("iteração %d: Inativar = %v, Lancar = %v", i, errInativar, errLancar)
			}
		}
	}
}

func TestListarCarrinho_RemoveProdutoInativo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Carrinho Inativo", 3)
	usuario := semearConta(t, db, "Usuário Carrinho Inat", "usuario.carrinhoinat@empresa.com", PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuario, produtoID, estoqueID, 1); err != nil {
		t.Fatal(err)
	}
	// Inativação direta (com saldo a service recusaria; aqui só importa o
	// estado inativo visto pelo carrinho).
	if _, err := db.Exec(`UPDATE produtos SET inativado_em = now() WHERE id = $1`, produtoID); err != nil {
		t.Fatal(err)
	}

	itens, removidos, err := ListarCarrinho(db, empresaTeste, usuario)
	if err != nil {
		t.Fatal(err)
	}
	if len(itens) != 0 || len(removidos) != 1 || removidos[0].Motivo != MotivoCarrinhoProdutoInativo || removidos[0].ProdutoNome != "Produto Carrinho Inativo" {
		t.Errorf("itens=%+v removidos=%+v", itens, removidos)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM carrinho_itens WHERE usuario_id = $1`, usuario).Scan(&n); err != nil || n != 0 {
		t.Errorf("carrinho_itens = %d (err %v), want 0", n, err)
	}
	// Segunda leitura: nada mais a remover.
	_, removidos, _ = ListarCarrinho(db, empresaTeste, usuario)
	if len(removidos) != 0 {
		t.Errorf("removidos na 2ª leitura = %+v", removidos)
	}
}

func TestSubmeterPedido_ProdutoInativoNaTransacao(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Pedido Inativo", 3)
	usuario := semearConta(t, db, "Usuário Pedido Inat", "usuario.pedidoinat@empresa.com", PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuario, produtoID, estoqueID, 1); err != nil {
		t.Fatal(err)
	}
	itens, _, err := ListarCarrinho(db, empresaTeste, usuario)
	if err != nil || len(itens) != 1 {
		t.Fatalf("itens=%v err=%v", itens, err)
	}

	// Ativo: a trava passa.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := recusarProdutosInativosTx(tx, empresaTeste, itens); err != nil {
		t.Errorf("ativo: %v", err)
	}
	_ = tx.Rollback()

	// Inativado depois da leitura do carrinho: recusa com o nome.
	if _, err := db.Exec(`UPDATE produtos SET inativado_em = now() WHERE id = $1`, produtoID); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	err = recusarProdutosInativosTx(tx, empresaTeste, itens)
	var ei *ErroPedidoProdutoInativo
	if !errors.As(err, &ei) || len(ei.Itens) != 1 || ei.Itens[0] != "Produto Pedido Inativo" {
		t.Errorf("err = %v, want ErroPedidoProdutoInativo com o nome", err)
	}
}

func TestSubmeterPedido_CarrinhoSoComInativoFicaVazio(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Pedido Só Inativo", 3)
	usuario := semearConta(t, db, "Usuário Só Inat", "usuario.soinat@empresa.com", PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuario, produtoID, estoqueID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE produtos SET inativado_em = now() WHERE id = $1`, produtoID); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmeterPedido(db, empresaTeste, usuario, "Fulano", "Obra", ""); !errors.Is(err, ErrPedidoCarrinhoVazio) {
		t.Errorf("err = %v, want ErrPedidoCarrinhoVazio", err)
	}
}

// TestSubmeterPedido_CorridaComInativacao exercita a corrida real pelo
// SubmeterPedido: uma transação trava o Produto (FOR UPDATE) e o marca
// inativo SEM commitar; o envio lê o carrinho (sem lock, vê o Produto ativo)
// e bloqueia no FOR SHARE do Produto. Só depois que o envio está de fato
// esperando o lock a inativação commita — o envio tem de recusar com
// *ErroPedidoProdutoInativo e não gravar Pedido nem reserva.
func TestSubmeterPedido_CorridaComInativacao(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Corrida Pedido Inat", 3)
	usuario := semearConta(t, db, "Usuário Corrida Pedido", "usuario.corridapedido@empresa.com", PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuario, produtoID, estoqueID, 1); err != nil {
		t.Fatal(err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var ignorado string
	if err := tx.QueryRow(`SELECT id FROM produtos WHERE id = $1 FOR UPDATE`, produtoID).Scan(&ignorado); err != nil {
		t.Fatalf("travar produto: %v", err)
	}
	if _, err := tx.Exec(`UPDATE produtos SET inativado_em = now() WHERE id = $1`, produtoID); err != nil {
		t.Fatalf("inativar na transação: %v", err)
	}

	resultado := make(chan error, 1)
	go func() {
		_, err := SubmeterPedido(db, empresaTeste, usuario, "Fulano", "Obra Corrida", "")
		resultado <- err
	}()

	// Espera o envio estar bloqueado no lock do Produto (FOR SHARE).
	limite := time.Now().Add(10 * time.Second)
	for {
		var esperando int
		if err := db.QueryRow(`
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND wait_event_type = 'Lock'
			  AND query ILIKE '%FROM produtos%FOR SHARE%'`).Scan(&esperando); err != nil {
			t.Fatalf("consultar pg_stat_activity: %v", err)
		}
		if esperando > 0 {
			break
		}
		select {
		case err := <-resultado:
			t.Fatalf("SubmeterPedido terminou sem esperar o lock: %v", err)
		default:
		}
		if time.Now().After(limite) {
			t.Fatal("timeout: SubmeterPedido nunca ficou esperando o lock do Produto")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit da inativação: %v", err)
	}

	var errEnvio error
	select {
	case errEnvio = <-resultado:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout esperando SubmeterPedido")
	}
	var ei *ErroPedidoProdutoInativo
	if !errors.As(errEnvio, &ei) || len(ei.Itens) != 1 || ei.Itens[0] != "Produto Corrida Pedido Inat" {
		t.Fatalf("err = %v, want *ErroPedidoProdutoInativo com o nome do Produto", errEnvio)
	}
	var pedidos, reservas int
	if err := db.QueryRow(`SELECT count(*) FROM pedidos WHERE usuario_id = $1`, usuario).Scan(&pedidos); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM reservas_pedido_item WHERE produto_id = $1`, produtoID).Scan(&reservas); err != nil {
		t.Fatal(err)
	}
	if pedidos != 0 || reservas != 0 {
		t.Errorf("pedidos=%d reservas=%d, want 0 e 0", pedidos, reservas)
	}
}
