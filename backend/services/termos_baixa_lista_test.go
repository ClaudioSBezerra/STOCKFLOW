package services

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Testes de acesso por Solicitante, listagem e conteúdo por Pedido do Termo de
// Baixa (spec-termo-baixa-pendencias).

// semearDoisTermos cria: termoAB (perda_roubo; pedidos de Ana e Bruno), termoB
// (vencido_avariado; só Bruno) e um pedido de Carla TOTALMENTE zerado (saldo 0
// na aprovação) dentro do termoAB — Carla não conta como solicitante dele.
func semearDoisTermos(t *testing.T) (gestorID, anaID, brunoID, carlaID, termoAB, termoB string) {
	t.Helper()
	db := testDB(t)
	limparProdutos(t, db)
	gestorID = semearConta(t, db, "Lista Termo Gestor", "lista-termo-gestor@empresa.com", PapelGestor, 0)
	anaID = semearConta(t, db, "Lista Termo Ana", "lista-termo-ana@empresa.com", PapelUsuario, 1)
	brunoID = semearConta(t, db, "Lista Termo Bruno", "lista-termo-bruno@empresa.com", PapelAlmoxarife, 2)

	carlaID = semearConta(t, db, "Lista Termo Carla", "lista-termo-carla@empresa.com", PapelUsuario, 3)

	pa := submeterSaidaTeste(t, db, anaID, "Lista Termo A", "perda_roubo", 2)
	pb := submeterSaidaTeste(t, db, brunoID, "Lista Termo B", "perda_roubo", 3)
	pc := submeterSaidaTeste(t, db, brunoID, "Lista Termo C", "vencido_avariado", 1)
	pz := submeterSaidaTeste(t, db, carlaID, "Lista Termo Z", "perda_roubo", 2)
	for id, nome := range map[string]string{pa.ID: "Ana Lista", pb.ID: "Bruno Lista", pc.ID: "Bruno Lista", pz.ID: "Carla Lista"} {
		if _, err := db.Exec(`UPDATE pedidos SET solicitante = $2 WHERE id = $1`, id, nome); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 0 WHERE produto_id IN (SELECT produto_id FROM pedido_itens WHERE pedido_id = $1)`, pz.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{pa.ID, pb.ID, pc.ID, pz.ID}, true); err != nil {
		t.Fatalf("DecidirLotePedidos: %v", err)
	}
	termoAB, _, _, _ = termoDoPedido(t, db, pa.ID)
	termoB, _, _, _ = termoDoPedido(t, db, pc.ID)
	return
}

func TestUsuarioSolicitouPedidoDoTermoBaixa(t *testing.T) {
	db := testDB(t)
	_, ana, bruno, carla, termoAB, termoB := semearDoisTermos(t)
	casos := []struct {
		nome           string
		empresa, termo string
		usuario        string
		want           bool
	}{
		{"ana no termo misto", empresaTeste, termoAB, ana, true},
		{"bruno no termo misto", empresaTeste, termoAB, bruno, true},
		{"ana no termo só do bruno", empresaTeste, termoB, ana, false},
		{"carla: pedido zerado não conta", empresaTeste, termoAB, carla, false},
		{"id malformado", empresaTeste, "malformado", ana, false},
		{"inexistente", empresaTeste, "00000000-0000-0000-0000-000000000000", ana, false},
	}
	for _, c := range casos {
		got, err := UsuarioSolicitouPedidoDoTermoBaixa(db, c.empresa, c.termo, c.usuario)
		if err != nil || got != c.want {
			t.Errorf("%s: got=%v err=%v, want %v", c.nome, got, err, c.want)
		}
	}
	alheia := criarEmpresaDeTeste(t, db, "termo-lista-alheia", "777888990077", "Alheia Lista Termo")
	t.Cleanup(func() { removerEmpresaDeTeste(t, db, "termo-lista-alheia") })
	if got, err := UsuarioSolicitouPedidoDoTermoBaixa(db, alheia.ID, termoAB, ana); err != nil || got {
		t.Errorf("outra empresa: got=%v err=%v, want false", got, err)
	}
}

func TestListarTermosBaixa_PorPapelEFiltros(t *testing.T) {
	db := testDB(t)
	_, ana, bruno, carla, termoAB, termoB := semearDoisTermos(t)

	todos, total, err := ListarTermosBaixa(db, empresaTeste, "", false, FiltroTermosBaixa{})
	if err != nil || len(todos) != 2 || total != 2 {
		t.Fatalf("gestor: n=%d total=%d err=%v, want 2/2", len(todos), total, err)
	}
	if todos[0].Numero < todos[1].Numero || todos[0].ID != termoB || todos[1].ID != termoAB {
		t.Errorf("ordem/ids = %+v", todos)
	}
	if todos[1].QuantidadeItens != 2 || todos[1].Aprovador != "Lista Termo Gestor" {
		t.Errorf("termo misto = %+v", todos[1])
	}
	if got := todos[1].Solicitantes; len(got) != 2 || got[0] != "Ana Lista" || got[1] != "Bruno Lista" {
		t.Errorf("solicitantes = %v, want Ana e Bruno (Carla zerada fica de fora)", got)
	}
	if todos[1].MotivoSaida == nil || *todos[1].MotivoSaida != "perda_roubo" {
		t.Errorf("motivo = %v", todos[1].MotivoSaida)
	}

	deAna, _, err := ListarTermosBaixa(db, empresaTeste, ana, true, FiltroTermosBaixa{})
	if err != nil || len(deAna) != 1 || deAna[0].ID != termoAB {
		t.Errorf("ana: %+v err=%v, want só o termo misto", deAna, err)
	}
	deBruno, _, _ := ListarTermosBaixa(db, empresaTeste, bruno, true, FiltroTermosBaixa{})
	if len(deBruno) != 2 {
		t.Errorf("bruno: n=%d, want 2", len(deBruno))
	}
	if deCarla, tot, _ := ListarTermosBaixa(db, empresaTeste, carla, true, FiltroTermosBaixa{}); len(deCarla) != 0 || tot != 0 {
		t.Errorf("carla (pedido zerado): n=%d total=%d, want 0", len(deCarla), tot)
	}

	n := todos[1].Numero
	hoje := time.Now().In(localPreNota).Format("2006-01-02")
	filtros := []struct {
		nome  string
		f     FiltroTermosBaixa
		ids   []string
		total int
	}{
		{"numero", FiltroTermosBaixa{Numero: &n}, []string{termoAB}, 1},
		{"motivo", FiltroTermosBaixa{Motivo: "vencido_avariado"}, []string{termoB}, 1},
		{"q Ana só o misto", FiltroTermosBaixa{Q: "Ana"}, []string{termoAB}, 1},
		{"q Bruno", FiltroTermosBaixa{Q: "bruno"}, []string{termoB, termoAB}, 2},
		{"q Carla (pedido zerado)", FiltroTermosBaixa{Q: "Carla"}, nil, 0},
		{"q aprovador", FiltroTermosBaixa{Q: "lista termo gest"}, []string{termoB, termoAB}, 2},
		{"q % literal", FiltroTermosBaixa{Q: "%"}, nil, 0},
		{"q _ literal", FiltroTermosBaixa{Q: "_"}, nil, 0},
		{"q barra", FiltroTermosBaixa{Q: `\`}, nil, 0},
		{"periodo hoje", FiltroTermosBaixa{De: hoje, Ate: hoje}, []string{termoB, termoAB}, 2},
		{"periodo passado", FiltroTermosBaixa{Ate: "2020-01-01"}, nil, 0},
		{"limite", FiltroTermosBaixa{Limite: 1}, []string{termoB}, 2},
		{"offset", FiltroTermosBaixa{Limite: 1, Offset: 1}, []string{termoAB}, 2},
		{"limite acima do máximo vira 200", FiltroTermosBaixa{Limite: 500}, []string{termoB, termoAB}, 2},
	}
	for _, c := range filtros {
		got, tot, err := ListarTermosBaixa(db, empresaTeste, "", false, c.f)
		if err != nil || len(got) != len(c.ids) || tot != c.total {
			t.Errorf("%s: n=%d total=%d err=%v, want %d/%d", c.nome, len(got), tot, err, len(c.ids), c.total)
			continue
		}
		for i := range got {
			if got[i].ID != c.ids[i] {
				t.Errorf("%s: [%d]=%s, want %s", c.nome, i, got[i].ID, c.ids[i])
			}
		}
	}

	zero, grande := 0, 2147483648
	invalidos := []FiltroTermosBaixa{
		{Numero: &zero}, {Numero: &grande}, {Motivo: "outro"}, {De: "19/11/2026"}, {Ate: "x"},
		{De: "2026-02-01", Ate: "2026-01-01"}, {De: "1999-12-31"}, {Ate: "2101-01-01"}, {De: "0001-01-01"},
		{Q: "a\x00b"}, {Q: "a\xffb"}, {Q: strings.Repeat("a", 101)}, {Limite: -1}, {Offset: -1},
	}
	for _, f := range invalidos {
		var ev *ErroPedidoValidacao
		if _, _, err := ListarTermosBaixa(db, empresaTeste, "", false, f); !errors.As(err, &ev) {
			t.Errorf("filtro %+v: err = %v, want ErroPedidoValidacao", f, err)
		}
	}

	alheia := criarEmpresaDeTeste(t, db, "termo-lista-alheia2", "777888990078", "Alheia Lista Termo 2")
	t.Cleanup(func() { removerEmpresaDeTeste(t, db, "termo-lista-alheia2") })
	if got, tot, err := ListarTermosBaixa(db, alheia.ID, "", false, FiltroTermosBaixa{}); err != nil || len(got) != 0 || tot != 0 {
		t.Errorf("outra empresa: n=%d err=%v, want 0", len(got), err)
	}
}

// Aprovação parcial: solicitado 3, aprovado 1 (saldo caiu). O item aparece sob
// o pedido certo, na ordem de criação, com Solicitada e Quantidade distintas.
func TestMontarTermoBaixaConteudo_AprovacaoParcialSolicitadoXAprovado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Parcial Gestor", "parcial-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "Parcial Usuario", "parcial-usuario@empresa.com", PapelUsuario, 1)
	p1 := submeterSaidaTeste(t, db, usuarioID, "Parcial Um", "perda_roubo", 3)
	p2 := submeterSaidaTeste(t, db, usuarioID, "Parcial Dois", "perda_roubo", 2)
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 1 WHERE produto_id IN (SELECT produto_id FROM pedido_itens WHERE pedido_id = $1)`, p1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{p1.ID, p2.ID}, true); err != nil {
		t.Fatal(err)
	}
	termoID, _, _, _ := termoDoPedido(t, db, p1.ID)
	c, err := MontarTermoBaixaConteudo(db, empresaTeste, termoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Justificativas) != 2 || c.Justificativas[0].PedidoID != p1.ID || c.Justificativas[1].PedidoID != p2.ID {
		t.Fatalf("ordem dos pedidos = %+v, want p1 depois p2", c.Justificativas)
	}
	l := layoutDoTermoBaixa(c)
	if len(l.Pedidos) != 2 {
		t.Fatalf("Pedidos = %d", len(l.Pedidos))
	}
	a := l.Pedidos[0].Origens[0].Produtos[0]
	b := l.Pedidos[1].Origens[0].Produtos[0]
	if a.Descricao != "Produto Parcial Um" || a.Solicitada != "3" || a.Quantidade != "1" {
		t.Errorf("item do pedido 1 = %+v, want solicitada 3 / aprovada 1", a)
	}
	if b.Descricao != "Produto Parcial Dois" || b.Solicitada != "2" || b.Quantidade != "2" {
		t.Errorf("item do pedido 2 = %+v, want 2 / 2", b)
	}
}

func TestMontarTermoBaixaConteudo_ItensLigadosAoPedido(t *testing.T) {
	db := testDB(t)
	_, _, _, _, termoAB, _ := semearDoisTermos(t)
	c, err := MontarTermoBaixaConteudo(db, empresaTeste, termoAB)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Justificativas) != 2 || len(c.Itens) != 2 {
		t.Fatalf("Justificativas=%d Itens=%d, want 2/2", len(c.Justificativas), len(c.Itens))
	}
	ids := map[string]bool{c.Justificativas[0].PedidoID: true, c.Justificativas[1].PedidoID: true}
	for _, it := range c.Itens {
		if !ids[it.PedidoID] || it.QuantidadeSolicitadaMilesimos <= 0 {
			t.Errorf("item sem ligação ao pedido: %+v", it)
		}
	}
	l := layoutDoTermoBaixa(c)
	if len(l.Pedidos) != 2 || len(l.Pedidos[0].Origens) != 1 || len(l.Pedidos[1].Origens) != 1 {
		t.Errorf("layout Pedidos = %+v", l.Pedidos)
	}
}
