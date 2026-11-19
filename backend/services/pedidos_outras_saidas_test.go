package services

import (
	"database/sql"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Testes do Pedido de Outras Saídas e do Termo de Baixa
// (spec-pedido-outras-saidas) — uma função por linha da I/O Matrix.

func contadorTermoBaixa(t *testing.T, db *sql.DB, empresaID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT ultimo_numero FROM contadores_termo_baixa WHERE empresa_id = $1`, empresaID).Scan(&n); err != nil {
		t.Fatalf("ler contadores_termo_baixa: %v", err)
	}
	return n
}

func contarTermosBaixa(t *testing.T, db *sql.DB, empresaID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM termos_baixa WHERE empresa_id = $1`, empresaID).Scan(&n); err != nil {
		t.Fatalf("contar termos_baixa: %v", err)
	}
	return n
}

func submeterSaidaTeste(t *testing.T, db *sql.DB, usuarioID, nome, motivo string, qtd float64) Pedido {
	t.Helper()
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, nome, 20)
	p, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "", "Justificativa de "+nome, produtoID, estoqueID, qtd, motivo)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa (%s): %v", nome, err)
	}
	return p
}

func termoDoPedido(t *testing.T, db *sql.DB, pedidoID string) (id string, numero int, motivo sql.NullString, ok bool) {
	t.Helper()
	var tid sql.NullString
	if err := db.QueryRow(`SELECT termo_baixa_id FROM pedidos WHERE id = $1`, pedidoID).Scan(&tid); err != nil {
		t.Fatalf("ler termo_baixa_id: %v", err)
	}
	if !tid.Valid {
		return "", 0, sql.NullString{}, false
	}
	if err := db.QueryRow(`SELECT numero, motivo_saida FROM termos_baixa WHERE id = $1`, tid.String).Scan(&numero, &motivo); err != nil {
		t.Fatalf("ler termo: %v", err)
	}
	return tid.String, numero, motivo, true
}

func TestSubmeterPedidoBaixa_ComMotivoGravaEReserva(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "Saida Motivo Usuario", "saida-motivo-usuario@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Saida Motivo", 30)

	p, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "", "Furto no galpão 3, BO 123", produtoID, estoqueID, 5, "perda_roubo")
	if err != nil {
		t.Fatalf("SubmeterPedidoBaixa: %v", err)
	}
	if p.Status != "pendente" || p.MotivoSaida == nil || *p.MotivoSaida != "perda_roubo" {
		t.Errorf("pedido = %+v", p)
	}
	var motivo string
	var saldo, reservado float64
	if err := db.QueryRow(`SELECT motivo_saida FROM pedidos WHERE id = $1`, p.ID).Scan(&motivo); err != nil || motivo != "perda_roubo" {
		t.Errorf("motivo_saida = %q (err=%v)", motivo, err)
	}
	_ = db.QueryRow(`SELECT quantidade FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2`, produtoID, estoqueID).Scan(&saldo)
	_ = db.QueryRow(`SELECT quantidade FROM reservas_pedido_item WHERE pedido_id = $1`, p.ID).Scan(&reservado)
	if saldo != 30 || reservado != 5 {
		t.Errorf("saldo = %v reservado = %v, want 30 e 5", saldo, reservado)
	}

	// Os três motivos são aceitos, com ou sem Obra / centro de custo.
	for _, m := range []string{"perda_roubo", "vencido_avariado", "utilizacao_obra"} {
		for _, obra := range []string{"", "Obra Norte"} {
			if _, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", obra, "just", produtoID, estoqueID, 1, m); err != nil {
				t.Errorf("motivo %s obra %q: %v", m, obra, err)
			}
		}
	}
}

func TestSubmeterPedidoBaixa_SemMotivoOuSemJustificativaNaoGrava(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "Saida Invalida Usuario", "saida-invalida-usuario@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Saida Invalida", 10)

	casos := []struct{ nome, motivo, just string }{
		{"motivo ausente", "", "just"},
		{"motivo inválido", "outro", "just"},
		{"justificativa vazia", "perda_roubo", ""},
		{"justificativa só espaços", "perda_roubo", "   \t "},
	}
	for _, c := range casos {
		_, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "", c.just, produtoID, estoqueID, 2, c.motivo)
		var ev *ErroPedidoValidacao
		if !errors.As(err, &ev) {
			t.Errorf("%s: err = %v, want ErroPedidoValidacao", c.nome, err)
		}
	}
	var n int
	_ = db.QueryRow(`SELECT count(*) FROM pedidos WHERE usuario_id = $1`, usuarioID).Scan(&n)
	if n != 0 {
		t.Errorf("pedidos gravados = %d, want 0", n)
	}
}

func TestSubmeterPedidoBaixa_JustificativaLimite1000(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "Saida Limite Usuario", "saida-limite-usuario@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Saida Limite", 10)

	// 1000 runes (acentuadas: contagem em caracteres, não em bytes) + espaços nas pontas: aceita.
	ok := "  " + strings.Repeat("é", 1000) + "  "
	if _, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "", ok, produtoID, estoqueID, 1, "perda_roubo"); err != nil {
		t.Fatalf("1000 caracteres deveria ser aceito: %v", err)
	}
	_, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "", strings.Repeat("é", 1001), produtoID, estoqueID, 1, "perda_roubo")
	var ev *ErroPedidoValidacao
	if !errors.As(err, &ev) || ev.Mensagem != "A justificativa deve ter no máximo 1000 caracteres." {
		t.Fatalf("1001 caracteres: err = %v, want ErroPedidoValidacao com a mensagem do limite", err)
	}
	var n int
	_ = db.QueryRow(`SELECT count(*) FROM pedidos WHERE usuario_id = $1`, usuarioID).Scan(&n)
	if n != 1 {
		t.Errorf("pedidos gravados = %d, want 1 (o de 1001 não grava)", n)
	}
}

func TestPedidos_MotivoSaidaSoParaBaixa_CheckDoBanco(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "Saida Check Usuario", "saida-check-usuario@empresa.com", PapelUsuario, 0)
	_, err := db.Exec(`INSERT INTO pedidos (usuario_id, solicitante, obra_centro_custo, empresa_id, tipo, motivo_saida)
		VALUES ($1, 'x', 'o', $2, 'retirada', 'perda_roubo')`, usuarioID, empresaTeste)
	if err == nil {
		t.Fatal("motivo_saida em pedido de retirada deveria violar o CHECK")
	}
	_, err = db.Exec(`INSERT INTO pedidos (usuario_id, solicitante, empresa_id, tipo, motivo_saida)
		VALUES ($1, 'x', $2, 'baixa', 'invalido')`, usuarioID, empresaTeste)
	if err == nil {
		t.Fatal("motivo_saida inválido deveria violar o CHECK")
	}
}

// Lote só de Outras Saídas, 3 motivos: um Termo por motivo, números
// consecutivos, nenhuma Pré-Nota; baixa legada (sem motivo) vira o 4º Termo.
func TestDecidirLotePedidos_OutrasSaidas_UmTermoPorMotivo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Termo Motivos Gestor", "termo-motivos-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "Termo Motivos Usuario", "termo-motivos-usuario@empresa.com", PapelUsuario, 1)

	pv1 := submeterSaidaTeste(t, db, usuarioID, "Termo Vencido 1", "vencido_avariado", 1)
	pu := submeterSaidaTeste(t, db, usuarioID, "Termo Utilizacao", "utilizacao_obra", 2)
	pp1 := submeterSaidaTeste(t, db, usuarioID, "Termo Perda 1", "perda_roubo", 3)
	pp2 := submeterSaidaTeste(t, db, usuarioID, "Termo Perda 2", "perda_roubo", 4)
	pl := submeterSaidaTeste(t, db, usuarioID, "Termo Legado", "perda_roubo", 1)
	// Baixa legada: criada antes de existir motivo.
	if _, err := db.Exec(`UPDATE pedidos SET motivo_saida = NULL WHERE id = $1`, pl.ID); err != nil {
		t.Fatalf("zerar motivo legado: %v", err)
	}

	antesTermo := contadorTermoBaixa(t, db, empresaTeste)
	antesPre := contadorPreNota(t, db, empresaTeste)
	preNotasAntes := contarPreNotas(t, db, empresaTeste)

	res, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{pv1.ID, pu.ID, pp1.ID, pp2.ID, pl.ID}, true)
	if err != nil {
		t.Fatalf("DecidirLotePedidos: %v", err)
	}
	// Ordem fixa dos grupos: perda_roubo, vencido_avariado, utilizacao_obra, sem motivo.
	esperado := map[string]struct {
		numero int
		motivo string
	}{
		pp1.ID: {antesTermo + 1, "perda_roubo"}, pp2.ID: {antesTermo + 1, "perda_roubo"},
		pv1.ID: {antesTermo + 2, "vencido_avariado"}, pu.ID: {antesTermo + 3, "utilizacao_obra"}, pl.ID: {antesTermo + 4, ""},
	}
	ids := map[string]string{}
	for _, det := range res {
		want := esperado[det.ID]
		if det.PreNotaID != nil {
			t.Errorf("pedido %s com Pré-Nota", det.ID)
		}
		if det.TermoBaixaID == nil || det.NumeroTermo == nil || *det.NumeroTermo != want.numero {
			t.Errorf("pedido %s: termo/número = %v/%v, want número %d", det.ID, det.TermoBaixaID, det.NumeroTermo, want.numero)
			continue
		}
		_, _, motivo, ok := termoDoPedido(t, db, det.ID)
		if !ok || motivo.String != want.motivo || motivo.Valid != (want.motivo != "") {
			t.Errorf("pedido %s: motivo do termo = %+v, want %q", det.ID, motivo, want.motivo)
		}
		ids[det.ID] = *det.TermoBaixaID
	}
	if ids[pp1.ID] != ids[pp2.ID] || ids[pp1.ID] == ids[pv1.ID] {
		t.Errorf("agrupamento por motivo errado: %v", ids)
	}
	if got := contadorTermoBaixa(t, db, empresaTeste); got != antesTermo+4 {
		t.Errorf("contador do termo = %d, want %d", got, antesTermo+4)
	}
	if contadorPreNota(t, db, empresaTeste) != antesPre || contarPreNotas(t, db, empresaTeste) != preNotasAntes {
		t.Error("lote só de Outras Saídas não pode gerar Pré-Nota nem avançar o docnum")
	}
}

func TestDecidirLotePedidos_SoTransferencias_UmaPreNotaNenhumTermo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Termo SoTransf Gestor", "termo-sotransf-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "Termo SoTransf Usuario", "termo-sotransf-usuario@empresa.com", PapelUsuario, 1)
	var ids []string
	for _, n := range []string{"A", "B", "C"} {
		produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Termo SoTransf "+n, 10)
		p, err := submeterTransfParaLoteTeste(t, db, empresaTeste, usuarioID, "Fulano", "Obra", produtoID, estoqueID, 1)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	termosAntes, contAntes := contarTermosBaixa(t, db, empresaTeste), contadorTermoBaixa(t, db, empresaTeste)
	preAntes := contarPreNotas(t, db, empresaTeste)
	if _, err := DecidirLotePedidos(db, empresaTeste, gestorID, ids, true); err != nil {
		t.Fatal(err)
	}
	if contarPreNotas(t, db, empresaTeste) != preAntes+1 {
		t.Error("deveria gerar exatamente 1 Pré-Nota")
	}
	if contarTermosBaixa(t, db, empresaTeste) != termosAntes || contadorTermoBaixa(t, db, empresaTeste) != contAntes {
		t.Error("lote só de Transferências não pode gerar Termo")
	}
}

func TestDecidirLotePedidos_LoteMisto_PreNotaETermosEmNumeracaoIndependente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Termo Misto Gestor", "termo-misto-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "Termo Misto Usuario", "termo-misto-usuario@empresa.com", PapelUsuario, 1)

	// Pré-Nota nº 7 já existe (contador em 7), contador de termos em 0.
	if _, err := db.Exec(`UPDATE contadores_pre_nota SET ultimo_numero = 7 WHERE empresa_id = $1`, empresaTeste); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE contadores_termo_baixa SET ultimo_numero = 0 WHERE empresa_id = $1`, empresaTeste); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, n := range []string{"A", "B"} {
		produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Termo Misto T"+n, 10)
		p, err := submeterTransfParaLoteTeste(t, db, empresaTeste, usuarioID, "Fulano", "Obra", produtoID, estoqueID, 1)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	b1 := submeterSaidaTeste(t, db, usuarioID, "Termo Misto B1", "perda_roubo", 1)
	b2 := submeterSaidaTeste(t, db, usuarioID, "Termo Misto B2", "vencido_avariado", 1)
	ids = append(ids, b1.ID, b2.ID)

	res, err := DecidirLotePedidos(db, empresaTeste, gestorID, ids, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, det := range res {
		if det.Tipo == "transferencia" {
			if det.Docnum == nil || *det.Docnum != 8 || det.TermoBaixaID != nil {
				t.Errorf("transferência: docnum=%v termo=%v, want docnum 8 e sem termo", det.Docnum, det.TermoBaixaID)
			}
		} else if det.NumeroTermo == nil || det.PreNotaID != nil || (*det.NumeroTermo != 1 && *det.NumeroTermo != 2) {
			t.Errorf("baixa: termo=%v preNota=%v, want termo 1 ou 2 e sem Pré-Nota", det.NumeroTermo, det.PreNotaID)
		}
	}
}

func TestDecidirLotePedidos_Rejeicao_NenhumTermoContadorIntacto(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Termo Rej Gestor", "termo-rej-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "Termo Rej Usuario", "termo-rej-usuario@empresa.com", PapelUsuario, 1)
	p := submeterSaidaTeste(t, db, usuarioID, "Termo Rej", "perda_roubo", 2)
	antes, termos := contadorTermoBaixa(t, db, empresaTeste), contarTermosBaixa(t, db, empresaTeste)
	res, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{p.ID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Status != "rejeitado" || res[0].TermoBaixaID != nil {
		t.Errorf("resultado = %+v", res[0])
	}
	if contadorTermoBaixa(t, db, empresaTeste) != antes || contarTermosBaixa(t, db, empresaTeste) != termos {
		t.Error("rejeição não pode gerar Termo nem avançar o contador")
	}
}

func TestDecidirLotePedidos_PuladoNaoEntraEmTermoNovo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Termo Pulado Gestor", "termo-pulado-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "Termo Pulado Usuario", "termo-pulado-usuario@empresa.com", PapelUsuario, 1)
	a := submeterSaidaTeste(t, db, usuarioID, "Termo Pulado A", "perda_roubo", 1)
	if _, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{a.ID}, true); err != nil {
		t.Fatal(err)
	}
	termoA, numeroA, _, _ := termoDoPedido(t, db, a.ID)
	contAntes := contadorTermoBaixa(t, db, empresaTeste)

	// Lote só com o já decidido: nenhum termo novo, contador intacto.
	if _, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{a.ID}, true); err != nil {
		t.Fatal(err)
	}
	if contadorTermoBaixa(t, db, empresaTeste) != contAntes {
		t.Error("lote 100% pulado avançou o contador")
	}
	// Lote com pulado + novo: o novo ganha o próximo número, o antigo fica intacto.
	b := submeterSaidaTeste(t, db, usuarioID, "Termo Pulado B", "perda_roubo", 1)
	if _, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{a.ID, b.ID}, true); err != nil {
		t.Fatal(err)
	}
	if t2, n2, _, _ := termoDoPedido(t, db, a.ID); t2 != termoA || n2 != numeroA {
		t.Error("pedido pulado teve o termo alterado")
	}
	if _, nb, _, ok := termoDoPedido(t, db, b.ID); !ok || nb != contAntes+1 {
		t.Errorf("termo de B = %d, want %d", nb, contAntes+1)
	}
}

func TestDecidirLotePedidos_TermosConcorrentes_SemColisaoNemBuraco(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Termo Conc Gestor", "termo-conc-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "Termo Conc Usuario", "termo-conc-usuario@empresa.com", PapelUsuario, 1)
	const lotes = 6
	var pedidos []Pedido
	for i := 0; i < lotes; i++ {
		pedidos = append(pedidos, submeterSaidaTeste(t, db, usuarioID, "Termo Conc "+string(rune('A'+i)), "perda_roubo", 1))
	}
	antes := contadorTermoBaixa(t, db, empresaTeste)
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, lotes)
	for i := range pedidos {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedidos[i].ID}, true)
		}(i)
	}
	close(start)
	wg.Wait()
	var numeros []int
	for i, e := range errs {
		if e != nil {
			t.Fatalf("lote %d: %v", i, e)
		}
		_, n, _, ok := termoDoPedido(t, db, pedidos[i].ID)
		if !ok {
			t.Fatalf("pedido %d sem termo", i)
		}
		numeros = append(numeros, n)
	}
	sort.Ints(numeros)
	for i, n := range numeros {
		if n != antes+1+i {
			t.Fatalf("números = %v, want %d..%d sem colisão nem buraco", numeros, antes+1, antes+lotes)
		}
	}
}

func TestDecidirLotePedidos_TermoDuasEmpresas_NumeracaoPropria(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Termo 2E Gestor", "termo-2e-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "Termo 2E Usuario", "termo-2e-usuario@empresa.com", PapelUsuario, 1)
	p1 := submeterSaidaTeste(t, db, usuarioID, "Termo 2E", "perda_roubo", 1)

	outra := criarEmpresaDeTeste(t, db, "outra-empresa-termo", "777888990011", "Outra Empresa Termo")
	t.Cleanup(func() { removerEmpresaDeTeste(t, db, "outra-empresa-termo") })
	t.Cleanup(func() {
		for _, stmt := range []string{
			`UPDATE pedidos SET termo_baixa_id = NULL WHERE empresa_id = $1`,
			`DELETE FROM termos_baixa WHERE empresa_id = $1`,
			`DELETE FROM pedido_itens WHERE empresa_id = $1`,
			`DELETE FROM movimentacoes WHERE empresa_id = $1`,
			`DELETE FROM reservas_pedido_item WHERE empresa_id = $1`,
			`DELETE FROM pedidos WHERE empresa_id = $1`,
			`DELETE FROM produto_estoque WHERE produto_id IN (SELECT id FROM produtos WHERE empresa_id = $1)`,
			`DELETE FROM produtos WHERE empresa_id = $1`,
			`DELETE FROM estoques WHERE empresa_id = $1`,
			`DELETE FROM usuarios WHERE empresa_id = $1`,
		} {
			if _, err := db.Exec(stmt, outra.ID); err != nil {
				t.Fatalf("limpeza da outra empresa [%s]: %v", stmt, err)
			}
		}
	})
	gestorOutra := semearContaNaEmpresa(t, db, outra.ID, "Termo 2E Gestor Outra", "termo-2e-gestor-outra@outra.com", PapelGestor)
	usuarioOutra := semearContaNaEmpresa(t, db, outra.ID, "Termo 2E Usuario Outra", "termo-2e-usuario-outra@outra.com", PapelUsuario)
	estoqueOutra, err := CriarEstoque(db, outra.ID, filialTeste(t, db, outra.ID), "Estoque Termo Outra")
	if err != nil {
		t.Fatal(err)
	}
	produtoOutra, err := criarProdutoComSaldo(db, outra.ID, CriarProdutoInput{
		UnidadeMedida: "un", Nome: "Produto Termo Outra", CategoriaID: categoriaIDPorCodigoEmpresa(t, db, outra.ID, "04.001"),
		TemplateID: templateGenericoID(t, db, outra.ID),
	}, estoqueOutra.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := SubmeterPedidoBaixa(db, outra.ID, usuarioOutra, "Fulano", "", "just", produtoOutra.ID, estoqueOutra.ID, 1, "perda_roubo")
	if err != nil {
		t.Fatal(err)
	}
	a1, a2 := contadorTermoBaixa(t, db, empresaTeste), contadorTermoBaixa(t, db, outra.ID)

	var wg sync.WaitGroup
	var e1, e2 error
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, e1 = DecidirLotePedidos(db, empresaTeste, gestorID, []string{p1.ID}, true)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, e2 = DecidirLotePedidos(db, outra.ID, gestorOutra, []string{p2.ID}, true)
	}()
	close(start)
	wg.Wait()
	if e1 != nil || e2 != nil {
		t.Fatalf("lotes: %v / %v", e1, e2)
	}
	if _, n, _, _ := termoDoPedido(t, db, p1.ID); n != a1+1 {
		t.Errorf("termo empresa 1 = %d, want %d", n, a1+1)
	}
	if _, n, _, _ := termoDoPedido(t, db, p2.ID); n != a2+1 {
		t.Errorf("termo empresa 2 = %d, want %d (nunca misturado)", n, a2+1)
	}
}

func TestLeiturasDePedido_ExpoemMotivoETermo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Termo Leit Gestor", "termo-leit-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "Termo Leit Usuario", "termo-leit-usuario@empresa.com", PapelUsuario, 1)
	p := submeterSaidaTeste(t, db, usuarioID, "Termo Leit", "vencido_avariado", 1)

	det, err := BuscarPedidoProprio(db, empresaTeste, p.ID, usuarioID, PapelUsuario)
	if err != nil || det.MotivoSaida == nil || *det.MotivoSaida != "vencido_avariado" || det.TermoBaixaID != nil {
		t.Fatalf("pendente: %+v (err=%v)", det.Pedido, err)
	}
	if _, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{p.ID}, true); err != nil {
		t.Fatal(err)
	}
	_, numero, _, _ := termoDoPedido(t, db, p.ID)
	det, _ = BuscarPedidoProprio(db, empresaTeste, p.ID, usuarioID, PapelUsuario)
	if det.TermoBaixaID == nil || det.NumeroTermo == nil || *det.NumeroTermo != numero {
		t.Errorf("detalhe = %+v", det.Pedido)
	}
	proprios, _ := ListarPedidosProprios(db, empresaTeste, usuarioID, "")
	fila, _ := ListarPedidosFila(db, empresaTeste, "")
	for nome, lista := range map[string][]PedidoResumo{"próprios": proprios, "fila": fila} {
		achou := false
		for _, r := range lista {
			if r.ID == p.ID {
				achou = true
				if r.MotivoSaida == nil || *r.MotivoSaida != "vencido_avariado" || r.NumeroTermo == nil || *r.NumeroTermo != numero {
					t.Errorf("%s: %+v", nome, r.Pedido)
				}
			}
		}
		if !achou {
			t.Errorf("%s: pedido ausente", nome)
		}
	}
}

func TestMontarTermoBaixaConteudo_EIsolamentoPorEmpresa(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Termo Cont Gestor", "termo-cont-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "Termo Cont Usuario", "termo-cont-usuario@empresa.com", PapelUsuario, 1)
	p := submeterSaidaTeste(t, db, usuarioID, "Termo Cont", "perda_roubo", 3)
	zero := submeterSaidaTeste(t, db, usuarioID, "Termo Cont Zero", "perda_roubo", 2)
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 0 WHERE produto_id IN (SELECT produto_id FROM pedido_itens WHERE pedido_id = $1)`, zero.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{p.ID, zero.ID}, true); err != nil {
		t.Fatal(err)
	}
	termoID, numero, _, _ := termoDoPedido(t, db, p.ID)

	c, err := MontarTermoBaixaConteudo(db, empresaTeste, termoID)
	if err != nil {
		t.Fatalf("MontarTermoBaixaConteudo: %v", err)
	}
	if c.Numero != numero || c.Motivo != "perda_roubo" || c.Aprovador != "Termo Cont Gestor" {
		t.Errorf("cabeçalho = %+v", c)
	}
	if len(c.Itens) != 1 || c.Itens[0].QuantidadeMilesimos != 3000 || c.Itens[0].Tipo != "baixa" {
		t.Errorf("Itens = %+v, want só o item de 3 (o zerado fica de fora)", c.Itens)
	}
	if len(c.Justificativas) != 1 || c.Justificativas[0].Texto != "Justificativa de Termo Cont" {
		t.Errorf("Justificativas = %+v", c.Justificativas)
	}
	if len(c.Solicitantes) != 1 || c.Solicitantes[0] != "Fulano" {
		t.Errorf("Solicitantes = %v", c.Solicitantes)
	}
	pdf, n, err := GerarTermoBaixaPDF(db, empresaTeste, termoID)
	if err != nil || n != numero || len(pdf) == 0 {
		t.Errorf("GerarTermoBaixaPDF: n=%d err=%v", n, err)
	}

	// Outra Empresa, inexistente, malformado -> não encontrado.
	for _, id := range []string{"00000000-0000-0000-0000-000000000000", "malformado"} {
		if _, err := MontarTermoBaixaConteudo(db, empresaTeste, id); !errors.Is(err, ErrTermoBaixaNaoEncontrado) {
			t.Errorf("id %q: err = %v", id, err)
		}
	}
	alheia := criarEmpresaDeTeste(t, db, "termo-alheia-x", "777888990012", "Alheia Termo")
	t.Cleanup(func() { removerEmpresaDeTeste(t, db, "termo-alheia-x") })
	outraEmpresaID := alheia.ID
	if _, err := MontarTermoBaixaConteudo(db, outraEmpresaID, termoID); !errors.Is(err, ErrTermoBaixaNaoEncontrado) {
		t.Errorf("empresa alheia: err = %v, want ErrTermoBaixaNaoEncontrado", err)
	}
}

func TestInserirEmpresa_CriaContadorDoTermoDeBaixa(t *testing.T) {
	db := testDB(t)
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM contadores_termo_baixa WHERE empresa_id = $1`, empresaTeste).Scan(&n); err != nil || n != 1 {
		t.Errorf("contadores_termo_baixa da empresa de teste: n=%d err=%v", n, err)
	}
}
