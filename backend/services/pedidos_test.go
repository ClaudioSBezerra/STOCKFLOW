package services

import (
	"database/sql"
	"errors"
	"testing"
)

// pedidoItemLinha é a leitura crua de uma linha de pedido_itens usada pelos
// testes desta suíte — mesmas colunas do snapshot gravado por
// SubmeterPedido.
type pedidoItemLinha struct {
	ProdutoID     string
	ProdutoNome   string
	CategoriaNome string
	EstoqueID     string
	EstoqueNome   string
	Quantidade    float64
}

func listarPedidoItens(t *testing.T, db *sql.DB, pedidoID string) []pedidoItemLinha {
	t.Helper()
	rows, err := db.Query(
		`SELECT produto_id, produto_nome, categoria_nome, estoque_id, estoque_nome, quantidade
		 FROM pedido_itens WHERE pedido_id = $1 ORDER BY produto_nome`,
		pedidoID,
	)
	if err != nil {
		t.Fatalf("falha ao listar pedido_itens: %v", err)
	}
	defer rows.Close()
	var itens []pedidoItemLinha
	for rows.Next() {
		var it pedidoItemLinha
		if err := rows.Scan(&it.ProdutoID, &it.ProdutoNome, &it.CategoriaNome, &it.EstoqueID, &it.EstoqueNome, &it.Quantidade); err != nil {
			t.Fatalf("falha ao ler linha de pedido_itens: %v", err)
		}
		itens = append(itens, it)
	}
	return itens
}

func contarPedidos(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM pedidos`).Scan(&n); err != nil {
		t.Fatalf("falha ao contar pedidos: %v", err)
	}
	return n
}

func contarItensCarrinho(t *testing.T, db *sql.DB, usuarioID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM carrinho_itens WHERE usuario_id = $1`, usuarioID).Scan(&n); err != nil {
		t.Fatalf("falha ao contar carrinho_itens: %v", err)
	}
	return n
}

// --- SubmeterPedido (Story 7.2, spec-7-2) ----------------------------------

// TestSubmeterPedido_Sucesso prova a linha "Envio feliz" da I/O Matrix:
// carrinho com 2 itens, ambos com saldo suficiente -> Pedido `pendente`
// criado com pedido_itens em snapshot (nome/categoria/estoque/quantidade),
// carrinho esvaziado.
func TestSubmeterPedido_Sucesso(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Pedido Sucesso", "pedido-sucesso@empresa.com", PapelUsuario, 0)
	produtoA, estoqueA, _ := seedProdutoComSaldo(t, db, "Pedido Sucesso A", 10)
	produtoB, estoqueB, _ := seedProdutoComSaldo(t, db, "Pedido Sucesso B", 5)
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoA, estoqueA, 4); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho A: %v", err)
	}
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoB, estoqueB, 2); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho B: %v", err)
	}

	pedido, err := SubmeterPedido(db, usuarioID, "  Fulano de Tal  ", "  Obra Norte  ", "  urgente  ")
	if err != nil {
		t.Fatalf("SubmeterPedido erro inesperado: %v", err)
	}
	if pedido.ID == "" {
		t.Error("ID vazio no retorno")
	}
	if pedido.UsuarioID != usuarioID {
		t.Errorf("UsuarioID = %q, want %q", pedido.UsuarioID, usuarioID)
	}
	if pedido.Solicitante != "Fulano de Tal" {
		t.Errorf("Solicitante = %q, want %q (trimado)", pedido.Solicitante, "Fulano de Tal")
	}
	if pedido.ObraCentroCusto != "Obra Norte" {
		t.Errorf("ObraCentroCusto = %q, want %q (trimado)", pedido.ObraCentroCusto, "Obra Norte")
	}
	if pedido.Observacao == nil || *pedido.Observacao != "urgente" {
		t.Errorf("Observacao = %v, want \"urgente\"", pedido.Observacao)
	}
	if pedido.Status != "pendente" {
		t.Errorf("Status = %q, want pendente", pedido.Status)
	}

	itens := listarPedidoItens(t, db, pedido.ID)
	if len(itens) != 2 {
		t.Fatalf("len(itens) = %d, want 2", len(itens))
	}
	if itens[0].ProdutoNome != "Produto Pedido Sucesso A" || itens[0].CategoriaNome == "" ||
		itens[0].EstoqueNome != "Pedido Sucesso A" || itens[0].Quantidade != 4 {
		t.Errorf("item A = %+v", itens[0])
	}
	if itens[1].ProdutoNome != "Produto Pedido Sucesso B" || itens[1].CategoriaNome == "" ||
		itens[1].EstoqueNome != "Pedido Sucesso B" || itens[1].Quantidade != 2 {
		t.Errorf("item B = %+v", itens[1])
	}

	if n := contarItensCarrinho(t, db, usuarioID); n != 0 {
		t.Errorf("carrinho_itens do usuário = %d linhas, want 0 (esvaziado)", n)
	}

	// Never (spec-7-2): o envio não debita produto_estoque — o débito real é
	// da Story 7.5 (aprovação).
	if q := saldoProdutoEstoque(t, db, produtoA, estoqueA); q != 10 {
		t.Errorf("saldo de A não deveria mudar, got %v, want 10", q)
	}
	if q := saldoProdutoEstoque(t, db, produtoB, estoqueB); q != 5 {
		t.Errorf("saldo de B não deveria mudar, got %v, want 5", q)
	}
}

// TestSubmeterPedido_ObservacaoAusenteFicaNula prova que uma observação
// vazia/só-espaços é gravada como NULL, não como string vazia.
func TestSubmeterPedido_ObservacaoAusenteFicaNula(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Pedido Sem Obs", "pedido-sem-obs@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Pedido Sem Obs", 10)
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 1); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho: %v", err)
	}

	pedido, err := SubmeterPedido(db, usuarioID, "Fulano", "Obra X", "   ")
	if err != nil {
		t.Fatalf("SubmeterPedido erro inesperado: %v", err)
	}
	if pedido.Observacao != nil {
		t.Errorf("Observacao = %v, want nil", *pedido.Observacao)
	}
}

// TestSubmeterPedido_CarrinhoVazio prova a linha "Carrinho vazio" da I/O
// Matrix: requisição rejeitada, nenhum Pedido criado.
func TestSubmeterPedido_CarrinhoVazio(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Pedido Vazio", "pedido-vazio@empresa.com", PapelUsuario, 0)

	antes := contarPedidos(t, db)
	_, err := SubmeterPedido(db, usuarioID, "Fulano", "Obra X", "")
	if !errors.Is(err, ErrPedidoCarrinhoVazio) {
		t.Fatalf("erro = %v, want ErrPedidoCarrinhoVazio", err)
	}
	if depois := contarPedidos(t, db); depois != antes {
		t.Errorf("pedidos = %d, want %d (nenhum criado)", depois, antes)
	}
}

// TestSubmeterPedido_CarrinhoSoComItemObsoleto prova que um carrinho com só
// itens obsoletos (Produto mesclado) — limpos pela própria ListarCarrinho
// reaproveitada — também colapsa em ErrPedidoCarrinhoVazio.
func TestSubmeterPedido_CarrinhoSoComItemObsoleto(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Pedido Obsoleto", "pedido-obsoleto@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Pedido Obsoleto", 10)
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 1); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho: %v", err)
	}
	if _, err := db.Exec(`UPDATE produtos SET deleted_at = now() WHERE id = $1`, produtoID); err != nil {
		t.Fatalf("seed soft-delete: %v", err)
	}

	_, err := SubmeterPedido(db, usuarioID, "Fulano", "Obra X", "")
	if !errors.Is(err, ErrPedidoCarrinhoVazio) {
		t.Fatalf("erro = %v, want ErrPedidoCarrinhoVazio", err)
	}
}

// TestSubmeterPedido_DisponibilidadeInsuficiente prova a linha
// "Disponibilidade insuficiente num item" da I/O Matrix: o saldo real caiu
// abaixo do pedido depois da montagem do carrinho -> Pedido inteiro
// rejeitado, carrinho inalterado, nenhum item debitado.
func TestSubmeterPedido_DisponibilidadeInsuficiente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Pedido Insuf", "pedido-insuf@empresa.com", PapelUsuario, 0)
	produtoA, estoqueA, autorID := seedProdutoComSaldo(t, db, "Pedido Insuf A", 10)
	produtoB, estoqueB, _ := seedProdutoComSaldo(t, db, "Pedido Insuf B", 5)
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoA, estoqueA, 4); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho A: %v", err)
	}
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoB, estoqueB, 4); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho B: %v", err)
	}

	// Saldo real de B cai para 1 (< 4 pedidos) DEPOIS da montagem do
	// carrinho — simula outra Baixa concorrente.
	if _, err := RegistrarBaixa(db, produtoB, estoqueB, autorID, 4); err != nil {
		t.Fatalf("seed RegistrarBaixa: %v", err)
	}

	antes := contarPedidos(t, db)
	_, err := SubmeterPedido(db, usuarioID, "Fulano", "Obra X", "")
	var erroIndisponivel *ErroPedidoIndisponivel
	if !errors.As(err, &erroIndisponivel) {
		t.Fatalf("erro = %v, want *ErroPedidoIndisponivel", err)
	}
	if len(erroIndisponivel.Itens) != 1 || erroIndisponivel.Itens[0] != "Produto Pedido Insuf B" {
		t.Errorf("Itens = %v, want [%q]", erroIndisponivel.Itens, "Produto Pedido Insuf B")
	}

	if depois := contarPedidos(t, db); depois != antes {
		t.Errorf("pedidos = %d, want %d (nenhum criado — falha de item aborta tudo)", depois, antes)
	}
	if n := contarItensCarrinho(t, db, usuarioID); n != 2 {
		t.Errorf("carrinho_itens do usuário = %d, want 2 (inalterado)", n)
	}
	if q := saldoProdutoEstoque(t, db, produtoA, estoqueA); q != 10 {
		t.Errorf("saldo de A não deveria mudar, got %v, want 10 (nenhum item debitado)", q)
	}
}

// TestSubmeterPedido_SolicitanteAusente prova a linha "Solicitante/obra
// ausente" da I/O Matrix: solicitante vazio/whitespace -> rejeitado ANTES de
// tocar o banco (nenhum Pedido, nenhuma leitura de carrinho necessária).
func TestSubmeterPedido_SolicitanteAusente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Pedido Sem Solic", "pedido-sem-solic@empresa.com", PapelUsuario, 0)

	_, err := SubmeterPedido(db, usuarioID, "   ", "Obra X", "")
	var erroValidacao *ErroPedidoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroPedidoValidacao", err)
	}
}

// TestSubmeterPedido_ObraAusente prova o mesmo caso para obraCentroCusto
// ausente.
func TestSubmeterPedido_ObraAusente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Pedido Sem Obra", "pedido-sem-obra@empresa.com", PapelUsuario, 0)

	_, err := SubmeterPedido(db, usuarioID, "Fulano", "", "")
	var erroValidacao *ErroPedidoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroPedidoValidacao", err)
	}
}

// TestSubmeterPedido_SolicitanteDiferenteDoUsuarioAutenticado prova o Always
// de spec-7-2: `solicitante` é sempre texto livre; `pedidos.usuario_id` é
// SEMPRE o id passado como usuarioID (a sessão), nunca inferido/derivado do
// texto de `solicitante`.
func TestSubmeterPedido_SolicitanteDiferenteDoUsuarioAutenticado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Pedido Dono", "pedido-dono@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Pedido Solicitante Livre", 10)
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 1); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho: %v", err)
	}

	pedido, err := SubmeterPedido(db, usuarioID, "Nome Completamente Diferente", "Obra X", "")
	if err != nil {
		t.Fatalf("SubmeterPedido erro inesperado: %v", err)
	}
	if pedido.UsuarioID != usuarioID {
		t.Errorf("UsuarioID = %q, want %q (sempre a sessão)", pedido.UsuarioID, usuarioID)
	}
	if pedido.Solicitante != "Nome Completamente Diferente" {
		t.Errorf("Solicitante = %q, want texto livre preservado", pedido.Solicitante)
	}

	var usuarioIDGravado string
	if err := db.QueryRow(`SELECT usuario_id FROM pedidos WHERE id = $1`, pedido.ID).Scan(&usuarioIDGravado); err != nil {
		t.Fatalf("falha ao reler pedido: %v", err)
	}
	if usuarioIDGravado != usuarioID {
		t.Errorf("usuario_id gravado = %q, want %q", usuarioIDGravado, usuarioID)
	}
}

// --- ListarPedidosProprios / BuscarPedidoProprio (Story 7.3, spec-7-3) -----

// seedPedidoComItem monta um carrinho de 1 item para `usuarioID` e o envia,
// devolvendo o Pedido `pendente` recém-criado. `nomeBase` precisa ser único
// dentro do teste (nomeia o Estoque e, via seedProdutoComSaldo, o Produto).
func seedPedidoComItem(t *testing.T, db *sql.DB, usuarioID, nomeBase string, qtd float64) Pedido {
	t.Helper()
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, nomeBase, qtd+10)
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, qtd); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho (%s): %v", nomeBase, err)
	}
	pedido, err := SubmeterPedido(db, usuarioID, "Solicitante "+nomeBase, "Obra "+nomeBase, "")
	if err != nil {
		t.Fatalf("seed SubmeterPedido (%s): %v", nomeBase, err)
	}
	return pedido
}

func setStatusPedido(t *testing.T, db *sql.DB, pedidoID, status string) {
	t.Helper()
	if _, err := db.Exec(`UPDATE pedidos SET status = $1 WHERE id = $2`, status, pedidoID); err != nil {
		t.Fatalf("setStatusPedido: %v", err)
	}
}

// TestListarPedidosProprios_EscopadoAoDono cobre a linha "Lista escopada ao
// dono" da I/O Matrix: o Usuário A vê só os próprios Pedidos, nunca os de B.
func TestListarPedidosProprios_EscopadoAoDono(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioA := semearConta(t, db, "Dono A", "pedidos-73-dono-a@empresa.com", PapelUsuario, 0)
	usuarioB := semearConta(t, db, "Dono B", "pedidos-73-dono-b@empresa.com", PapelUsuario, 0)

	seedPedidoComItem(t, db, usuarioA, "73 Escopo A1", 1)
	seedPedidoComItem(t, db, usuarioA, "73 Escopo A2", 2)
	seedPedidoComItem(t, db, usuarioB, "73 Escopo B1", 3)

	lista, err := ListarPedidosProprios(db, usuarioA, "")
	if err != nil {
		t.Fatalf("ListarPedidosProprios(A) erro: %v", err)
	}
	if len(lista) != 2 {
		t.Fatalf("len(lista) = %d, want 2 (só os de A)", len(lista))
	}
	for _, p := range lista {
		if p.UsuarioID != usuarioA {
			t.Errorf("Pedido %s tem UsuarioID = %q, want %q", p.ID, p.UsuarioID, usuarioA)
		}
	}
}

// TestListarPedidosProprios_OrdemDescEQtdItens cobre "ordem por criado_em
// DESC" e o campo QtdItens da projeção de resumo.
func TestListarPedidosProprios_OrdemDescEQtdItens(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Ordem 73", "pedidos-73-ordem@empresa.com", PapelUsuario, 0)

	// Pedido antigo: 1 item.
	antigo := seedPedidoComItem(t, db, usuarioID, "73 Ordem Antigo", 1)
	if _, err := db.Exec(`UPDATE pedidos SET criado_em = now() - interval '1 hour' WHERE id = $1`, antigo.ID); err != nil {
		t.Fatalf("envelhecer pedido antigo: %v", err)
	}

	// Pedido recente: 2 itens (dois produtos/estoques distintos).
	pA, eA, _ := seedProdutoComSaldo(t, db, "73 Ordem Recente A", 10)
	pB, eB, _ := seedProdutoComSaldo(t, db, "73 Ordem Recente B", 10)
	if _, err := AdicionarItemCarrinho(db, usuarioID, pA, eA, 1); err != nil {
		t.Fatalf("seed item A: %v", err)
	}
	if _, err := AdicionarItemCarrinho(db, usuarioID, pB, eB, 1); err != nil {
		t.Fatalf("seed item B: %v", err)
	}
	recente, err := SubmeterPedido(db, usuarioID, "Solicitante", "Obra", "")
	if err != nil {
		t.Fatalf("SubmeterPedido recente: %v", err)
	}

	lista, err := ListarPedidosProprios(db, usuarioID, "")
	if err != nil {
		t.Fatalf("ListarPedidosProprios erro: %v", err)
	}
	if len(lista) != 2 {
		t.Fatalf("len(lista) = %d, want 2", len(lista))
	}
	if lista[0].ID != recente.ID {
		t.Errorf("lista[0].ID = %s, want o mais recente %s", lista[0].ID, recente.ID)
	}
	if lista[0].QtdItens != 2 {
		t.Errorf("lista[0].QtdItens = %d, want 2", lista[0].QtdItens)
	}
	if lista[1].ID != antigo.ID {
		t.Errorf("lista[1].ID = %s, want o mais antigo %s", lista[1].ID, antigo.ID)
	}
	if lista[1].QtdItens != 1 {
		t.Errorf("lista[1].QtdItens = %d, want 1", lista[1].QtdItens)
	}
}

// TestListarPedidosProprios_FiltroPorStatus cobre a linha "Filtro por
// status" da I/O Matrix.
func TestListarPedidosProprios_FiltroPorStatus(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Filtro 73", "pedidos-73-filtro@empresa.com", PapelUsuario, 0)

	pendente := seedPedidoComItem(t, db, usuarioID, "73 Filtro Pendente", 1)
	aprovado := seedPedidoComItem(t, db, usuarioID, "73 Filtro Aprovado", 1)
	setStatusPedido(t, db, aprovado.ID, "aprovado")

	lista, err := ListarPedidosProprios(db, usuarioID, "aprovado")
	if err != nil {
		t.Fatalf("ListarPedidosProprios(aprovado) erro: %v", err)
	}
	if len(lista) != 1 {
		t.Fatalf("len(lista) = %d, want 1", len(lista))
	}
	if lista[0].ID != aprovado.ID || lista[0].Status != "aprovado" {
		t.Errorf("lista[0] = {ID:%s Status:%s}, want {ID:%s Status:aprovado}", lista[0].ID, lista[0].Status, aprovado.ID)
	}
	_ = pendente
}

// TestListarPedidosProprios_FiltroInvalido cobre "Filtro de status inválido":
// &ErroPedidoValidacao devolvido sem tocar o banco.
func TestListarPedidosProprios_FiltroInvalido(t *testing.T) {
	db := testDB(t)

	_, err := ListarPedidosProprios(db, "qualquer-coisa", "banana")
	var erroValidacao *ErroPedidoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroPedidoValidacao", err)
	}
}

// TestListarPedidosProprios_SemPedidos cobre "Sem Pedidos": slice vazio
// não-nil, sem erro.
func TestListarPedidosProprios_SemPedidos(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Sem Pedidos 73", "pedidos-73-vazio@empresa.com", PapelUsuario, 0)

	lista, err := ListarPedidosProprios(db, usuarioID, "")
	if err != nil {
		t.Fatalf("ListarPedidosProprios erro: %v", err)
	}
	if lista == nil {
		t.Fatal("lista == nil, want slice vazio não-nil")
	}
	if len(lista) != 0 {
		t.Fatalf("len(lista) = %d, want 0", len(lista))
	}
}

// TestBuscarPedidoProprio_DonoComItens cobre "Detalhe pelo dono": cabeçalho
// + itens em snapshot, ordenados por produto_nome.
func TestBuscarPedidoProprio_DonoComItens(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Detalhe Dono 73", "pedidos-73-detalhe-dono@empresa.com", PapelUsuario, 0)
	pedido := seedPedidoComItem(t, db, usuarioID, "73 Detalhe Dono", 4)

	det, err := BuscarPedidoProprio(db, pedido.ID, usuarioID, PapelUsuario)
	if err != nil {
		t.Fatalf("BuscarPedidoProprio erro: %v", err)
	}
	if det.ID != pedido.ID || det.UsuarioID != usuarioID {
		t.Errorf("cabeçalho = {ID:%s UsuarioID:%s}, want {ID:%s UsuarioID:%s}", det.ID, det.UsuarioID, pedido.ID, usuarioID)
	}
	if len(det.Itens) != 1 {
		t.Fatalf("len(Itens) = %d, want 1", len(det.Itens))
	}
	it := det.Itens[0]
	if it.ProdutoNome != "Produto 73 Detalhe Dono" || it.EstoqueNome != "73 Detalhe Dono" ||
		it.CategoriaNome == "" || it.Quantidade != 4 {
		t.Errorf("item = %+v", it)
	}
}

// TestBuscarPedidoProprio_OutroUsuarioPapelUsuario cobre "Detalhe de Pedido
// alheio, papel usuario": ErrPedidoNaoEncontrado, sem revelar existência.
func TestBuscarPedidoProprio_OutroUsuarioPapelUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	dono := semearConta(t, db, "Dono Alheio 73", "pedidos-73-dono-alheio@empresa.com", PapelUsuario, 0)
	outro := semearConta(t, db, "Outro 73", "pedidos-73-outro@empresa.com", PapelUsuario, 0)
	pedido := seedPedidoComItem(t, db, dono, "73 Alheio", 1)

	_, err := BuscarPedidoProprio(db, pedido.ID, outro, PapelUsuario)
	if !errors.Is(err, ErrPedidoNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrPedidoNaoEncontrado", err)
	}
}

// TestBuscarPedidoProprio_AlmoxarifeVeDeQualquerUm cobre "Detalhe de Pedido
// alheio, papel almoxarife+": 200 com cabeçalho + itens (padrão de escopo
// AD-8).
func TestBuscarPedidoProprio_AlmoxarifeVeDeQualquerUm(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	dono := semearConta(t, db, "Dono p/ Almox 73", "pedidos-73-dono-almox@empresa.com", PapelUsuario, 0)
	almox := semearConta(t, db, "Almox Consulta 73", "pedidos-73-almox-consulta@empresa.com", PapelAlmoxarife, 0)
	pedido := seedPedidoComItem(t, db, dono, "73 Almox Ve", 2)

	det, err := BuscarPedidoProprio(db, pedido.ID, almox, PapelAlmoxarife)
	if err != nil {
		t.Fatalf("BuscarPedidoProprio(almox) erro: %v", err)
	}
	if det.ID != pedido.ID {
		t.Errorf("det.ID = %s, want %s", det.ID, pedido.ID)
	}
	if len(det.Itens) != 1 {
		t.Errorf("len(Itens) = %d, want 1", len(det.Itens))
	}
}

// TestBuscarPedidoProprio_IdMalformadoOuInexistente cobre "Detalhe com id
// inexistente/malformado": os dois colapsam em ErrPedidoNaoEncontrado.
func TestBuscarPedidoProprio_IdMalformadoOuInexistente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Id Ruim 73", "pedidos-73-id-ruim@empresa.com", PapelUsuario, 0)

	if _, err := BuscarPedidoProprio(db, "nao-e-uuid", usuarioID, PapelUsuario); !errors.Is(err, ErrPedidoNaoEncontrado) {
		t.Errorf("id malformado: erro = %v, want ErrPedidoNaoEncontrado", err)
	}
	if _, err := BuscarPedidoProprio(db, "00000000-0000-0000-0000-000000000000", usuarioID, PapelUsuario); !errors.Is(err, ErrPedidoNaoEncontrado) {
		t.Errorf("id inexistente: erro = %v, want ErrPedidoNaoEncontrado", err)
	}
}

// TestBuscarPedidoProprio_ItensOrdenadosPorNome cobre um Pedido com 2+ itens:
// a ordenação declarada em BuscarPedidoProprio (`ORDER BY produto_nome`) vem
// alfabética na resposta, mesmo quando os itens foram inseridos em ordem
// reversa — os testes de item único não provam nada sobre essa ordenação.
func TestBuscarPedidoProprio_ItensOrdenadosPorNome(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Ordem Itens 73", "pedidos-73-ordem-itens@empresa.com", PapelUsuario, 0)

	// Seed em ordem reversa de nome: "Zebra" é inserido/adicionado ao
	// carrinho ANTES de "Arame" — se a query não ordenasse explicitamente,
	// a ordem de inserção (Zebra, Arame) vazaria na resposta.
	produtoZ, estoqueZ, _ := seedProdutoComSaldo(t, db, "73 Ordem Itens Zebra", 10)
	produtoA, estoqueA, _ := seedProdutoComSaldo(t, db, "73 Ordem Itens Arame", 10)
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoZ, estoqueZ, 1); err != nil {
		t.Fatalf("seed item Zebra: %v", err)
	}
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoA, estoqueA, 1); err != nil {
		t.Fatalf("seed item Arame: %v", err)
	}
	pedido, err := SubmeterPedido(db, usuarioID, "Solicitante", "Obra", "")
	if err != nil {
		t.Fatalf("SubmeterPedido: %v", err)
	}

	det, err := BuscarPedidoProprio(db, pedido.ID, usuarioID, PapelUsuario)
	if err != nil {
		t.Fatalf("BuscarPedidoProprio erro: %v", err)
	}
	if len(det.Itens) != 2 {
		t.Fatalf("len(Itens) = %d, want 2", len(det.Itens))
	}
	if det.Itens[0].ProdutoNome != "Produto 73 Ordem Itens Arame" || det.Itens[1].ProdutoNome != "Produto 73 Ordem Itens Zebra" {
		t.Errorf("ordem = [%s, %s], want [Arame, Zebra] (alfabética por produto_nome)",
			det.Itens[0].ProdutoNome, det.Itens[1].ProdutoNome)
	}
}
