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
