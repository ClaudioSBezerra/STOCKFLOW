package services

import (
	"bytes"
	"database/sql"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
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
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoA, estoqueA, 4); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho A: %v", err)
	}
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoB, estoqueB, 2); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho B: %v", err)
	}

	pedido, err := SubmeterPedido(db, empresaTeste, usuarioID, "  Fulano de Tal  ", "  Obra Norte  ", "  urgente  ")
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
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoID, estoqueID, 1); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho: %v", err)
	}

	pedido, err := SubmeterPedido(db, empresaTeste, usuarioID, "Fulano", "Obra X", "   ")
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
	_, err := SubmeterPedido(db, empresaTeste, usuarioID, "Fulano", "Obra X", "")
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
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoID, estoqueID, 1); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho: %v", err)
	}
	if _, err := db.Exec(`UPDATE produtos SET deleted_at = now() WHERE id = $1`, produtoID); err != nil {
		t.Fatalf("seed soft-delete: %v", err)
	}

	_, err := SubmeterPedido(db, empresaTeste, usuarioID, "Fulano", "Obra X", "")
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
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoA, estoqueA, 4); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho A: %v", err)
	}
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoB, estoqueB, 4); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho B: %v", err)
	}

	// Saldo real de B cai para 1 (< 4 pedidos) DEPOIS da montagem do
	// carrinho — simula outra Baixa concorrente.
	if _, err := RegistrarBaixa(db, empresaTeste, produtoB, estoqueB, autorID, 4); err != nil {
		t.Fatalf("seed RegistrarBaixa: %v", err)
	}

	antes := contarPedidos(t, db)
	_, err := SubmeterPedido(db, empresaTeste, usuarioID, "Fulano", "Obra X", "")
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

	_, err := SubmeterPedido(db, empresaTeste, usuarioID, "   ", "Obra X", "")
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

	_, err := SubmeterPedido(db, empresaTeste, usuarioID, "Fulano", "", "")
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
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoID, estoqueID, 1); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho: %v", err)
	}

	pedido, err := SubmeterPedido(db, empresaTeste, usuarioID, "Nome Completamente Diferente", "Obra X", "")
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
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoID, estoqueID, qtd); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho (%s): %v", nomeBase, err)
	}
	pedido, err := SubmeterPedido(db, empresaTeste, usuarioID, "Solicitante "+nomeBase, "Obra "+nomeBase, "")
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

	lista, err := ListarPedidosProprios(db, empresaTeste, usuarioA, "")
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
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, pA, eA, 1); err != nil {
		t.Fatalf("seed item A: %v", err)
	}
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, pB, eB, 1); err != nil {
		t.Fatalf("seed item B: %v", err)
	}
	recente, err := SubmeterPedido(db, empresaTeste, usuarioID, "Solicitante", "Obra", "")
	if err != nil {
		t.Fatalf("SubmeterPedido recente: %v", err)
	}

	lista, err := ListarPedidosProprios(db, empresaTeste, usuarioID, "")
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

	lista, err := ListarPedidosProprios(db, empresaTeste, usuarioID, "aprovado")
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

// TestListarPedidosProprios_FiltroPorStatusParcialmenteAprovado cobre o
// valor novo de status introduzido pela Story 7.5 (spec-7-5) no mesmo filtro
// `?status=` — sem este teste, um erro de digitação ou remoção da chave
// `"parcialmente_aprovado"` em statusPedidoValido devolveria
// &ErroPedidoValidacao para todo Usuário tentando filtrar "Meus Pedidos"
// por esse status, sem que nenhum teste existente detectasse a regressão.
func TestListarPedidosProprios_FiltroPorStatusParcialmenteAprovado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Filtro 75 Parcial", "pedidos-75-filtro-parcial@empresa.com", PapelUsuario, 0)

	pendente := seedPedidoComItem(t, db, usuarioID, "75 Filtro Pendente", 1)
	parcial := seedPedidoComItem(t, db, usuarioID, "75 Filtro Parcial", 1)
	setStatusPedido(t, db, parcial.ID, "parcialmente_aprovado")

	lista, err := ListarPedidosProprios(db, empresaTeste, usuarioID, "parcialmente_aprovado")
	if err != nil {
		t.Fatalf("ListarPedidosProprios(parcialmente_aprovado) erro: %v", err)
	}
	if len(lista) != 1 || lista[0].ID != parcial.ID || lista[0].Status != "parcialmente_aprovado" {
		t.Errorf("lista = %+v, want só {ID:%s Status:parcialmente_aprovado}", lista, parcial.ID)
	}
	_ = pendente
}

// TestListarPedidosProprios_FiltroInvalido cobre "Filtro de status inválido":
// &ErroPedidoValidacao devolvido sem tocar o banco.
func TestListarPedidosProprios_FiltroInvalido(t *testing.T) {
	db := testDB(t)

	_, err := ListarPedidosProprios(db, empresaTeste, "qualquer-coisa", "banana")
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

	lista, err := ListarPedidosProprios(db, empresaTeste, usuarioID, "")
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

	det, err := BuscarPedidoProprio(db, empresaTeste, pedido.ID, usuarioID, PapelUsuario)
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

	_, err := BuscarPedidoProprio(db, empresaTeste, pedido.ID, outro, PapelUsuario)
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

	det, err := BuscarPedidoProprio(db, empresaTeste, pedido.ID, almox, PapelAlmoxarife)
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

	if _, err := BuscarPedidoProprio(db, empresaTeste, "nao-e-uuid", usuarioID, PapelUsuario); !errors.Is(err, ErrPedidoNaoEncontrado) {
		t.Errorf("id malformado: erro = %v, want ErrPedidoNaoEncontrado", err)
	}
	if _, err := BuscarPedidoProprio(db, empresaTeste, "00000000-0000-0000-0000-000000000000", usuarioID, PapelUsuario); !errors.Is(err, ErrPedidoNaoEncontrado) {
		t.Errorf("id inexistente: erro = %v, want ErrPedidoNaoEncontrado", err)
	}
}

// --- ListarPedidosFila / ListarPedidosParaSessao (Story 7.4, spec-7-4) -----

// TestListarPedidosFila_TodosOsUsuarios cobre a linha "Fila escopada a
// todos" da I/O Matrix: a fila devolve Pedidos de VÁRIOS usuários, não só de
// um.
func TestListarPedidosFila_TodosOsUsuarios(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioA := semearConta(t, db, "Fila Dono A", "pedidos-74-fila-dono-a@empresa.com", PapelUsuario, 0)
	usuarioB := semearConta(t, db, "Fila Dono B", "pedidos-74-fila-dono-b@empresa.com", PapelUsuario, 0)

	pA := seedPedidoComItem(t, db, usuarioA, "74 Fila A", 1)
	pB := seedPedidoComItem(t, db, usuarioB, "74 Fila B", 2)

	lista, err := ListarPedidosFila(db, empresaTeste, "")
	if err != nil {
		t.Fatalf("ListarPedidosFila erro: %v", err)
	}
	if len(lista) != 2 {
		t.Fatalf("len(lista) = %d, want 2 (de ambos os usuários)", len(lista))
	}
	ids := map[string]bool{lista[0].ID: true, lista[1].ID: true}
	if !ids[pA.ID] || !ids[pB.ID] {
		t.Errorf("lista = %+v, want conter %s e %s", lista, pA.ID, pB.ID)
	}
}

// TestListarPedidosFila_OrdemDesc cobre a ordenação `criado_em DESC` da
// fila, mesmo molde de TestListarPedidosProprios_OrdemDescEQtdItens.
func TestListarPedidosFila_OrdemDesc(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Fila Ordem", "pedidos-74-fila-ordem@empresa.com", PapelUsuario, 0)

	antigo := seedPedidoComItem(t, db, usuarioID, "74 Fila Ordem Antigo", 1)
	if _, err := db.Exec(`UPDATE pedidos SET criado_em = now() - interval '1 hour' WHERE id = $1`, antigo.ID); err != nil {
		t.Fatalf("envelhecer pedido antigo: %v", err)
	}
	recente := seedPedidoComItem(t, db, usuarioID, "74 Fila Ordem Recente", 1)

	lista, err := ListarPedidosFila(db, empresaTeste, "")
	if err != nil {
		t.Fatalf("ListarPedidosFila erro: %v", err)
	}
	if len(lista) != 2 {
		t.Fatalf("len(lista) = %d, want 2", len(lista))
	}
	if lista[0].ID != recente.ID || lista[1].ID != antigo.ID {
		t.Errorf("ordem = [%s, %s], want [%s, %s] (recente primeiro)", lista[0].ID, lista[1].ID, recente.ID, antigo.ID)
	}
}

// TestListarPedidosFila_FiltroPorStatus cobre a linha "Fila filtrada por
// status": só os Pedidos naquele status de QUALQUER usuário.
func TestListarPedidosFila_FiltroPorStatus(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioA := semearConta(t, db, "Fila Filtro A", "pedidos-74-fila-filtro-a@empresa.com", PapelUsuario, 0)
	usuarioB := semearConta(t, db, "Fila Filtro B", "pedidos-74-fila-filtro-b@empresa.com", PapelUsuario, 0)

	pendenteA := seedPedidoComItem(t, db, usuarioA, "74 Fila Filtro Pend A", 1)
	aprovadoB := seedPedidoComItem(t, db, usuarioB, "74 Fila Filtro Aprov B", 1)
	setStatusPedido(t, db, aprovadoB.ID, "aprovado")

	lista, err := ListarPedidosFila(db, empresaTeste, "aprovado")
	if err != nil {
		t.Fatalf("ListarPedidosFila(aprovado) erro: %v", err)
	}
	if len(lista) != 1 || lista[0].ID != aprovadoB.ID {
		t.Fatalf("lista = %+v, want só %s", lista, aprovadoB.ID)
	}
	_ = pendenteA
}

// TestListarPedidosFila_FiltroPorStatusParcialmenteAprovado cobre, na Fila,
// o mesmo valor novo de status coberto acima em
// TestListarPedidosProprios_FiltroPorStatusParcialmenteAprovado — a chave
// `"parcialmente_aprovado"` em statusPedidoValido é compartilhada pelos dois
// filtros (Meus Pedidos e Fila), mas nenhum teste exercitava esse valor na
// Fila antes deste.
func TestListarPedidosFila_FiltroPorStatusParcialmenteAprovado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioA := semearConta(t, db, "Fila Filtro A Parcial", "pedidos-75-fila-filtro-a-parcial@empresa.com", PapelUsuario, 0)
	usuarioB := semearConta(t, db, "Fila Filtro B Parcial", "pedidos-75-fila-filtro-b-parcial@empresa.com", PapelUsuario, 0)

	pendenteA := seedPedidoComItem(t, db, usuarioA, "75 Fila Filtro Pend A", 1)
	parcialB := seedPedidoComItem(t, db, usuarioB, "75 Fila Filtro Parcial B", 1)
	setStatusPedido(t, db, parcialB.ID, "parcialmente_aprovado")

	lista, err := ListarPedidosFila(db, empresaTeste, "parcialmente_aprovado")
	if err != nil {
		t.Fatalf("ListarPedidosFila(parcialmente_aprovado) erro: %v", err)
	}
	if len(lista) != 1 || lista[0].ID != parcialB.ID {
		t.Fatalf("lista = %+v, want só %s", lista, parcialB.ID)
	}
	_ = pendenteA
}

// TestListarPedidosFila_FiltroInvalido cobre "Filtro de status inválido no
// escopo todos": &ErroPedidoValidacao devolvido sem tocar o banco.
func TestListarPedidosFila_FiltroInvalido(t *testing.T) {
	db := testDB(t)

	_, err := ListarPedidosFila(db, empresaTeste, "banana")
	var erroValidacao *ErroPedidoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroPedidoValidacao", err)
	}
}

// TestListarPedidosFila_Vazia cobre "Fila vazia": slice vazio não-nil, sem
// erro.
func TestListarPedidosFila_Vazia(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	lista, err := ListarPedidosFila(db, empresaTeste, "")
	if err != nil {
		t.Fatalf("ListarPedidosFila erro: %v", err)
	}
	if lista == nil {
		t.Fatal("lista == nil, want slice vazio não-nil")
	}
	if len(lista) != 0 {
		t.Fatalf("len(lista) = %d, want 0", len(lista))
	}
}

// TestListarPedidosFila_PedidoDeBaixaSemObraCentroCusto prova a linha
// achada pela revisão da Story 20.2 (verification-gap): um Pedido de Baixa
// SEM Obra/Centro de Custo (coluna NULL desde a migration 000057) não
// quebra `ListarPedidosFila` — o `COALESCE(p.obra_centro_custo, '')` já
// aplicado na query devolve `ObraCentroCusto = ""`, nunca um erro de Scan.
func TestListarPedidosFila_PedidoDeBaixaSemObraCentroCusto(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Fila Baixa Sem Obra", 10)
	pedido, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "", "", produtoID, estoqueID, 5)
	if err != nil {
		t.Fatalf("SubmeterPedidoBaixa erro inesperado: %v", err)
	}

	lista, err := ListarPedidosFila(db, empresaTeste, "")
	if err != nil {
		t.Fatalf("ListarPedidosFila erro: %v", err)
	}
	var achado *PedidoResumo
	for i := range lista {
		if lista[i].ID == pedido.ID {
			achado = &lista[i]
			break
		}
	}
	if achado == nil {
		t.Fatalf("pedido de baixa %q não apareceu na Fila (lista=%+v)", pedido.ID, lista)
	}
	if achado.ObraCentroCusto != "" {
		t.Errorf("ObraCentroCusto = %q, want \"\" (NULL no banco, COALESCE na leitura)", achado.ObraCentroCusto)
	}
	if achado.Tipo != "baixa" {
		t.Errorf("Tipo = %q, want baixa", achado.Tipo)
	}
}

// TestListarPedidosParaSessao_AlmoxarifeEscopoTodos cobre almoxarife+
// `escopoTodos=true` -> todos os Pedidos da organização, não só os próprios
// — e, especificamente, a Fila INCLUI também os Pedidos que o próprio
// almoxarife (o ator da sessão) enviou, lado a lado com os de outro
// usuário: nenhuma exclusão por `usuario_id` existe em ListarPedidosFila (ao
// contrário de ListarPedidosProprios), então "todos" precisa mesmo dizer
// todos, inclusive os do próprio ator.
func TestListarPedidosParaSessao_AlmoxarifeEscopoTodos(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	almox := semearConta(t, db, "Sessao Almox Todos", "pedidos-74-sessao-almox-todos@empresa.com", PapelAlmoxarife, 0)
	outro := semearConta(t, db, "Sessao Dono Alheio", "pedidos-74-sessao-dono-alheio@empresa.com", PapelUsuario, 0)
	pedidoOutro := seedPedidoComItem(t, db, outro, "74 Sessao Almox Todos", 1)
	pedidoProprioAlmox := seedPedidoComItem(t, db, almox, "74 Sessao Almox Todos Proprio", 1)

	lista, err := ListarPedidosParaSessao(db, empresaTeste, almox, PapelAlmoxarife, true, "")
	if err != nil {
		t.Fatalf("ListarPedidosParaSessao erro: %v", err)
	}
	if len(lista) != 2 {
		t.Fatalf("len(lista) = %d, want 2 (do outro usuário E do próprio almoxarife)", len(lista))
	}
	ids := map[string]bool{lista[0].ID: true, lista[1].ID: true}
	if !ids[pedidoOutro.ID] {
		t.Errorf("lista = %+v, want conter o Pedido do outro usuário %s", lista, pedidoOutro.ID)
	}
	if !ids[pedidoProprioAlmox.ID] {
		t.Errorf("lista = %+v, want conter TAMBÉM o Pedido do próprio almoxarife %s (Fila não exclui o ator)", lista, pedidoProprioAlmox.ID)
	}
}

// TestListarPedidosParaSessao_AlmoxarifeEscopoProprio cobre almoxarife+
// `escopoTodos=false` -> só os próprios Pedidos, mesmo comportamento de
// ListarPedidosProprios.
func TestListarPedidosParaSessao_AlmoxarifeEscopoProprio(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	almox := semearConta(t, db, "Sessao Almox Proprio", "pedidos-74-sessao-almox-proprio@empresa.com", PapelAlmoxarife, 0)
	outro := semearConta(t, db, "Sessao Dono Alheio 2", "pedidos-74-sessao-dono-alheio-2@empresa.com", PapelUsuario, 0)
	seedPedidoComItem(t, db, outro, "74 Sessao Almox Proprio Alheio", 1)

	lista, err := ListarPedidosParaSessao(db, empresaTeste, almox, PapelAlmoxarife, false, "")
	if err != nil {
		t.Fatalf("ListarPedidosParaSessao erro: %v", err)
	}
	if len(lista) != 0 {
		t.Fatalf("lista = %+v, want [] (almoxarife sem Pedidos próprios, escopoTodos=false)", lista)
	}
}

// TestListarPedidosParaSessao_PapelInsuficienteEscopoTodos cobre "Escopo
// todos ignorado para papel insuficiente": usuário comum + escopoTodos=true
// -> só os próprios, NUNCA erro (epics.md Story 7.4 AC2).
func TestListarPedidosParaSessao_PapelInsuficienteEscopoTodos(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioA := semearConta(t, db, "Sessao Papel Insuf A", "pedidos-74-sessao-papel-insuf-a@empresa.com", PapelUsuario, 0)
	usuarioB := semearConta(t, db, "Sessao Papel Insuf B", "pedidos-74-sessao-papel-insuf-b@empresa.com", PapelUsuario, 0)

	proprio := seedPedidoComItem(t, db, usuarioA, "74 Sessao Papel Insuf Proprio", 1)
	seedPedidoComItem(t, db, usuarioB, "74 Sessao Papel Insuf Alheio", 1)

	lista, err := ListarPedidosParaSessao(db, empresaTeste, usuarioA, PapelUsuario, true, "")
	if err != nil {
		t.Fatalf("ListarPedidosParaSessao erro: %v", err)
	}
	if len(lista) != 1 || lista[0].ID != proprio.ID {
		t.Fatalf("lista = %+v, want só o próprio %s", lista, proprio.ID)
	}
}

// TestListarPedidosParaSessao_GestorEAdmEscopoTodos cobre que papéis acima
// de almoxarife (gestor, adm) também alcançam a fila — confirma a
// comparação `>=`, não `==`.
func TestListarPedidosParaSessao_GestorEAdmEscopoTodos(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	for _, papel := range []string{PapelGestor, PapelAdm} {
		t.Run(papel, func(t *testing.T) {
			limparProdutos(t, db)
			ator := semearConta(t, db, "Sessao "+papel, "pedidos-74-sessao-"+papel+"@empresa.com", papel, 0)
			outro := semearConta(t, db, "Sessao Dono "+papel, "pedidos-74-sessao-dono-"+papel+"@empresa.com", PapelUsuario, 0)
			pedido := seedPedidoComItem(t, db, outro, "74 Sessao "+papel, 1)

			lista, err := ListarPedidosParaSessao(db, empresaTeste, ator, papel, true, "")
			if err != nil {
				t.Fatalf("ListarPedidosParaSessao(%s) erro: %v", papel, err)
			}
			if len(lista) != 1 || lista[0].ID != pedido.ID {
				t.Fatalf("lista = %+v, want só %s", lista, pedido.ID)
			}
		})
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
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoZ, estoqueZ, 1); err != nil {
		t.Fatalf("seed item Zebra: %v", err)
	}
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoA, estoqueA, 1); err != nil {
		t.Fatalf("seed item Arame: %v", err)
	}
	pedido, err := SubmeterPedido(db, empresaTeste, usuarioID, "Solicitante", "Obra", "")
	if err != nil {
		t.Fatalf("SubmeterPedido: %v", err)
	}

	det, err := BuscarPedidoProprio(db, empresaTeste, pedido.ID, usuarioID, PapelUsuario)
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

// --- DecidirPedido (Story 7.5, spec-7-5) -----------------------------------

// itemPedidoSeedSpec descreve um item a semear via seedPedidoComItens: um
// Produto novo com `SaldoInicial` de saldo real em `produto_estoque`, pedido
// no Pedido com `QtdSolicitada`.
type itemPedidoSeedSpec struct {
	NomeBase      string
	SaldoInicial  float64
	QtdSolicitada float64
}

// parProdutoEstoque é o par (produto_id, estoque_id) resolvido para um item
// semeado por seedPedidoComItens — devolvido na MESMA ordem de `itens` para
// os testes inspecionarem saldo/movimentação por item após a decisão.
type parProdutoEstoque struct {
	ProdutoID string
	EstoqueID string
}

// seedPedidoComItens monta e envia (via carrinho -> SubmeterPedido) um
// Pedido `pendente` com múltiplos itens, cada um com seu próprio Produto
// (saldo controlado independentemente) — o cenário que TestSubmeterPedido_*
// não precisava mas os testes de DecidirPedido precisam (saldo real
// divergente do solicitado, item a item).
func seedPedidoComItens(t *testing.T, db *sql.DB, usuarioID string, itens []itemPedidoSeedSpec) (Pedido, []parProdutoEstoque) {
	t.Helper()
	pares := make([]parProdutoEstoque, len(itens))
	for i, it := range itens {
		produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, it.NomeBase, it.SaldoInicial)
		pares[i] = parProdutoEstoque{ProdutoID: produtoID, EstoqueID: estoqueID}
		if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoID, estoqueID, it.QtdSolicitada); err != nil {
			t.Fatalf("seed AdicionarItemCarrinho (%s): %v", it.NomeBase, err)
		}
	}
	pedido, err := SubmeterPedido(db, empresaTeste, usuarioID, "Solicitante Decisao", "Obra Decisao", "")
	if err != nil {
		t.Fatalf("seed SubmeterPedido: %v", err)
	}
	return pedido, pares
}

// TestDecidirPedido_AprovacaoTotal cobre a linha "Aprovação total" da I/O
// Matrix: todos os itens com disponível >= solicitado -> status `aprovado`,
// cada item com `QuantidadeAprovada == Quantidade`, estoque debitado +
// Movimentação por item, SSE implícito fica a cargo do handler (não
// testado aqui).
func TestDecidirPedido_AprovacaoTotal(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Decisao Total", "decisao-total@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Decisao Total Almox", "decisao-total-almox@empresa.com", PapelAlmoxarife, 0)
	pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Decisao Total A", SaldoInicial: 10, QtdSolicitada: 4},
		{NomeBase: "Decisao Total B", SaldoInicial: 5, QtdSolicitada: 5},
	})

	det, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
	if err != nil {
		t.Fatalf("DecidirPedido erro inesperado: %v", err)
	}
	if det.Status != "aprovado" {
		t.Errorf("Status = %q, want aprovado", det.Status)
	}
	if len(det.Itens) != 2 {
		t.Fatalf("len(Itens) = %d, want 2", len(det.Itens))
	}
	for _, it := range det.Itens {
		if it.QuantidadeAprovada == nil || *it.QuantidadeAprovada != it.Quantidade {
			t.Errorf("item %s: QuantidadeAprovada = %v, want %v", it.ProdutoNome, it.QuantidadeAprovada, it.Quantidade)
		}
	}

	if saldo := saldoProdutoEstoque(t, db, pares[0].ProdutoID, pares[0].EstoqueID); saldo != 6 {
		t.Errorf("saldo A = %v, want 6 (10 - 4)", saldo)
	}
	if saldo := saldoProdutoEstoque(t, db, pares[1].ProdutoID, pares[1].EstoqueID); saldo != 0 {
		t.Errorf("saldo B = %v, want 0 (5 - 5)", saldo)
	}
	if n := contarMovimentacoes(t, db, pares[0].ProdutoID); n != 1 {
		t.Errorf("movimentacoes de A = %d, want 1", n)
	}
	if n := contarMovimentacoes(t, db, pares[1].ProdutoID); n != 1 {
		t.Errorf("movimentacoes de B = %d, want 1", n)
	}

	// P5 (spec-7-5 review): auditoria da decisão ("toda decisão registra quem
	// decidiu e quando", PRD) — decidido_por/decidido_em vêm preenchidos na
	// própria resposta de DecidirPedido, sem precisar de uma releitura à
	// parte.
	if det.DecididoPor == nil || *det.DecididoPor != almoxID {
		t.Errorf("DecididoPor = %v, want %q", det.DecididoPor, almoxID)
	}
	if det.DecididoEm == nil {
		t.Fatal("DecididoEm = nil, want um timestamp preenchido")
	}
	if agora := time.Since(*det.DecididoEm); agora < 0 || agora > time.Minute {
		t.Errorf("DecididoEm = %v, want um timestamp recente (< 1 minuto atrás)", *det.DecididoEm)
	}
}

// TestDecidirPedido_AprovacaoParcial cobre a linha "Aprovação parcial" da
// I/O Matrix: 1 item com disponível(4) < solicitado(10), outro item ok ->
// status `parcialmente_aprovado`, item divergente com `QuantidadeAprovada=4`
// (débito só de 4), item ok com `QuantidadeAprovada == Quantidade`.
func TestDecidirPedido_AprovacaoParcial(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Decisao Parcial", "decisao-parcial@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Decisao Parcial Almox", "decisao-parcial-almox@empresa.com", PapelAlmoxarife, 0)
	pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Decisao Parcial Divergente", SaldoInicial: 10, QtdSolicitada: 10},
		{NomeBase: "Decisao Parcial Ok", SaldoInicial: 5, QtdSolicitada: 3},
	})
	// Saldo real do item divergente cai para 4 (< 10 pedidos) DEPOIS da
	// montagem/envio — simula uma Baixa concorrente entre o envio e a
	// decisão.
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 4 WHERE produto_id = $1 AND estoque_id = $2`,
		pares[0].ProdutoID, pares[0].EstoqueID); err != nil {
		t.Fatalf("seed reduzir saldo divergente: %v", err)
	}

	det, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
	if err != nil {
		t.Fatalf("DecidirPedido erro inesperado: %v", err)
	}
	if det.Status != "parcialmente_aprovado" {
		t.Errorf("Status = %q, want parcialmente_aprovado", det.Status)
	}

	var divergente, ok PedidoItem
	for _, it := range det.Itens {
		switch it.ProdutoID {
		case pares[0].ProdutoID:
			divergente = it
		case pares[1].ProdutoID:
			ok = it
		}
	}
	if divergente.QuantidadeAprovada == nil || *divergente.QuantidadeAprovada != 4 {
		t.Errorf("item divergente: QuantidadeAprovada = %v, want 4", divergente.QuantidadeAprovada)
	}
	if ok.QuantidadeAprovada == nil || *ok.QuantidadeAprovada != ok.Quantidade {
		t.Errorf("item ok: QuantidadeAprovada = %v, want %v", ok.QuantidadeAprovada, ok.Quantidade)
	}

	if saldo := saldoProdutoEstoque(t, db, pares[0].ProdutoID, pares[0].EstoqueID); saldo != 0 {
		t.Errorf("saldo divergente = %v, want 0 (4 - 4, débito só do disponível)", saldo)
	}
	if saldo := saldoProdutoEstoque(t, db, pares[1].ProdutoID, pares[1].EstoqueID); saldo != 2 {
		t.Errorf("saldo ok = %v, want 2 (5 - 3)", saldo)
	}
}

// TestDecidirPedido_ItemSemEstoqueAlgum cobre a linha "Item sem estoque
// algum" da I/O Matrix: disponível=0 para 1 item -> esse item
// `QuantidadeAprovada=0`, nenhuma Movimentação para ele, status
// `parcialmente_aprovado`.
func TestDecidirPedido_ItemSemEstoqueAlgum(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Decisao SemEstoque", "decisao-sem-estoque@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Decisao SemEstoque Almox", "decisao-sem-estoque-almox@empresa.com", PapelAlmoxarife, 0)
	pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Decisao SemEstoque Zerado", SaldoInicial: 10, QtdSolicitada: 5},
	})
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 0 WHERE produto_id = $1 AND estoque_id = $2`,
		pares[0].ProdutoID, pares[0].EstoqueID); err != nil {
		t.Fatalf("seed zerar saldo: %v", err)
	}

	det, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
	if err != nil {
		t.Fatalf("DecidirPedido erro inesperado: %v", err)
	}
	if det.Status != "parcialmente_aprovado" {
		t.Errorf("Status = %q, want parcialmente_aprovado", det.Status)
	}
	if len(det.Itens) != 1 || det.Itens[0].QuantidadeAprovada == nil || *det.Itens[0].QuantidadeAprovada != 0 {
		t.Errorf("itens = %+v, want 1 item com QuantidadeAprovada=0", det.Itens)
	}
	if n := contarMovimentacoes(t, db, pares[0].ProdutoID); n != 0 {
		t.Errorf("movimentacoes = %d, want 0 (nenhum débito de item zerado)", n)
	}
	if saldo := saldoProdutoEstoque(t, db, pares[0].ProdutoID, pares[0].EstoqueID); saldo != 0 {
		t.Errorf("saldo = %v, want 0 (inalterado)", saldo)
	}
}

// TestDecidirPedido_Rejeicao cobre a linha "Rejeição" da I/O Matrix: status
// `rejeitado`, todos os itens `QuantidadeAprovada=0`, nenhum débito, nenhuma
// Movimentação.
func TestDecidirPedido_Rejeicao(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Decisao Rejeicao", "decisao-rejeicao@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Decisao Rejeicao Almox", "decisao-rejeicao-almox@empresa.com", PapelAlmoxarife, 0)
	pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Decisao Rejeicao A", SaldoInicial: 10, QtdSolicitada: 4},
	})

	det, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, false)
	if err != nil {
		t.Fatalf("DecidirPedido erro inesperado: %v", err)
	}
	if det.Status != "rejeitado" {
		t.Errorf("Status = %q, want rejeitado", det.Status)
	}
	if len(det.Itens) != 1 || det.Itens[0].QuantidadeAprovada == nil || *det.Itens[0].QuantidadeAprovada != 0 {
		t.Errorf("itens = %+v, want 1 item com QuantidadeAprovada=0", det.Itens)
	}
	if n := contarMovimentacoes(t, db, pares[0].ProdutoID); n != 0 {
		t.Errorf("movimentacoes = %d, want 0", n)
	}
	if saldo := saldoProdutoEstoque(t, db, pares[0].ProdutoID, pares[0].EstoqueID); saldo != 10 {
		t.Errorf("saldo = %v, want 10 (inalterado — rejeição nunca lê/trava produto_estoque)", saldo)
	}
}

// TestDecidirPedido_PedidoJaDecidido cobre "Pedido já decidido" da I/O
// Matrix: status != 'pendente' -> ErrPedidoNaoPendente, nenhuma escrita
// nova (saldo/Movimentação inalterados a partir do estado já decidido).
func TestDecidirPedido_PedidoJaDecidido(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Decisao JaFeita", "decisao-ja-feita@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Decisao JaFeita Almox", "decisao-ja-feita-almox@empresa.com", PapelAlmoxarife, 0)
	pedido, _ := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Decisao JaFeita A", SaldoInicial: 10, QtdSolicitada: 4},
	})
	setStatusPedido(t, db, pedido.ID, "aprovado")

	_, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
	if !errors.Is(err, ErrPedidoNaoPendente) {
		t.Fatalf("erro = %v, want ErrPedidoNaoPendente", err)
	}
}

// TestDecidirPedido_IdInexistenteOuMalformado cobre "Id inexistente/
// malformado" da I/O Matrix: os dois colapsam em ErrPedidoNaoEncontrado.
func TestDecidirPedido_IdInexistenteOuMalformado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	almoxID := semearConta(t, db, "Decisao IdRuim Almox", "decisao-id-ruim-almox@empresa.com", PapelAlmoxarife, 0)

	if _, err := DecidirPedido(db, empresaTeste, "nao-e-uuid", almoxID, PapelAlmoxarife, true); !errors.Is(err, ErrPedidoNaoEncontrado) {
		t.Errorf("id malformado: erro = %v, want ErrPedidoNaoEncontrado", err)
	}
	if _, err := DecidirPedido(db, empresaTeste, "00000000-0000-0000-0000-000000000000", almoxID, PapelAlmoxarife, true); !errors.Is(err, ErrPedidoNaoEncontrado) {
		t.Errorf("id inexistente: erro = %v, want ErrPedidoNaoEncontrado", err)
	}
}

// TestDecidirPedido_DecisoesConcorrentesSoAPrimeiraGanha cobre a AC5: duas
// decisões concorrentes para o MESMO Pedido — só a primeira a commitar
// decide de fato, a segunda recebe ErrPedidoNaoPendente sem debitar nada
// (o UPDATE guardado `WHERE status = 'pendente'` de DecidirPedido fecha a
// corrida). Dimensionado com saldo de sobra (10 disponível, 4 pedidos) para
// que a decisão vencedora sempre aprove totalmente — a asserção final
// confirma o débito de EXATAMENTE 4 (nunca 8, que seria dupla aplicação).
func TestDecidirPedido_DecisoesConcorrentesSoAPrimeiraGanha(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Decisao Corrida", "decisao-corrida@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Decisao Corrida Almox", "decisao-corrida-almox@empresa.com", PapelAlmoxarife, 0)
	pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Decisao Corrida A", SaldoInicial: 10, QtdSolicitada: 4},
	})

	start := make(chan struct{})
	var wg sync.WaitGroup
	var err1, err2 error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err1 = DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err2 = DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
	}()
	close(start)
	wg.Wait()

	sucessos := 0
	conflitos := 0
	for _, err := range []error{err1, err2} {
		switch {
		case err == nil:
			sucessos++
		case errors.Is(err, ErrPedidoNaoPendente):
			conflitos++
		default:
			t.Fatalf("erro inesperado numa decisão concorrente: %v", err)
		}
	}
	if sucessos != 1 || conflitos != 1 {
		t.Fatalf("sucessos=%d conflitos=%d, want 1 e 1 (só a primeira decide)", sucessos, conflitos)
	}
	if saldo := saldoProdutoEstoque(t, db, pares[0].ProdutoID, pares[0].EstoqueID); saldo != 6 {
		t.Errorf("saldo = %v, want 6 (10 - 4, débito de UMA SÓ decisão)", saldo)
	}
	if n := contarMovimentacoes(t, db, pares[0].ProdutoID); n != 1 {
		t.Errorf("movimentacoes = %d, want 1 (nunca dupla aplicação)", n)
	}
}

// TestDecidirPedido_DecisoesConcorrentesMistasSoAPrimeiraGanha cobre a mesma
// AC5 que TestDecidirPedido_DecisoesConcorrentesSoAPrimeiraGanha, mas para
// uma corrida MISTA — uma goroutine aprova, a outra rejeita, para o MESMO
// Pedido pendente. Só a primeira a commitar decide de fato: a outra recebe
// ErrPedidoNaoPendente sem debitar nada, sem zerar nada, e sem mudar o
// status de novo (o UPDATE guardado `WHERE status = 'pendente'` de
// DecidirPedido fecha a corrida independente de QUAL das duas decisões
// vence). O saldo/Movimentação final refletem exatamente qual delas venceu
// — nunca uma mistura das duas nem uma dupla aplicação.
func TestDecidirPedido_DecisoesConcorrentesMistasSoAPrimeiraGanha(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Decisao Corrida Mista", "decisao-corrida-mista@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Decisao Corrida Mista Almox", "decisao-corrida-mista-almox@empresa.com", PapelAlmoxarife, 0)
	pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Decisao Corrida Mista A", SaldoInicial: 10, QtdSolicitada: 4},
	})

	start := make(chan struct{})
	var wg sync.WaitGroup
	var det1, det2 PedidoDetalhe
	var err1, err2 error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		det1, err1 = DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
	}()
	go func() {
		defer wg.Done()
		<-start
		det2, err2 = DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, false)
	}()
	close(start)
	wg.Wait()

	sucessos := 0
	conflitos := 0
	var vencedor *PedidoDetalhe
	if err1 == nil {
		sucessos++
		vencedor = &det1
	} else if errors.Is(err1, ErrPedidoNaoPendente) {
		conflitos++
	} else {
		t.Fatalf("erro inesperado na decisão de aprovação: %v", err1)
	}
	if err2 == nil {
		sucessos++
		vencedor = &det2
	} else if errors.Is(err2, ErrPedidoNaoPendente) {
		conflitos++
	} else {
		t.Fatalf("erro inesperado na decisão de rejeição: %v", err2)
	}
	if sucessos != 1 || conflitos != 1 {
		t.Fatalf("sucessos=%d conflitos=%d, want 1 e 1 (só a primeira decide, seja aprovação ou rejeição)", sucessos, conflitos)
	}

	switch vencedor.Status {
	case "aprovado":
		if saldo := saldoProdutoEstoque(t, db, pares[0].ProdutoID, pares[0].EstoqueID); saldo != 6 {
			t.Errorf("saldo = %v, want 6 (10 - 4, aprovação venceu a corrida)", saldo)
		}
		if n := contarMovimentacoes(t, db, pares[0].ProdutoID); n != 1 {
			t.Errorf("movimentacoes = %d, want 1 (aprovação venceu a corrida)", n)
		}
	case "rejeitado":
		if saldo := saldoProdutoEstoque(t, db, pares[0].ProdutoID, pares[0].EstoqueID); saldo != 10 {
			t.Errorf("saldo = %v, want 10 (inalterado — rejeição venceu a corrida)", saldo)
		}
		if n := contarMovimentacoes(t, db, pares[0].ProdutoID); n != 0 {
			t.Errorf("movimentacoes = %d, want 0 (rejeição venceu a corrida)", n)
		}
	default:
		t.Fatalf("status do vencedor = %q, want aprovado ou rejeitado", vencedor.Status)
	}
}

// TestDecidirPedido_OrdemLocksAscendenteSemDeadlock cobre a AC1: duas
// decisões concorrentes de Pedidos DIFERENTES que compartilham os MESMOS
// dois pares (produto_id, estoque_id), mas com os itens inseridos em
// `pedido_itens` em ordem OPOSTA entre os dois Pedidos (Pedido1: item B
// enviado antes de A; Pedido2: item A antes de B) — se DecidirPedido travasse
// na ordem de inserção do lote (em vez da ordem ascendente de
// produto_id/estoque_id exigida pela AC1/AD-10), as duas decisões
// tentariam adquirir os locks em ordens opostas e o Postgres devolveria
// 40P01 (deadlock). Molde de TestRegistrarTransferencia_ConcorrenciaSemDeadlock.
func TestDecidirPedido_OrdemLocksAscendenteSemDeadlock(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Decisao OrdemLocks", "decisao-ordem-locks@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Decisao OrdemLocks Almox", "decisao-ordem-locks-almox@empresa.com", PapelAlmoxarife, 0)

	produtoA, estoqueA, _ := seedProdutoComSaldo(t, db, "Decisao OrdemLocks A", 100)
	produtoB, estoqueB, _ := seedProdutoComSaldo(t, db, "Decisao OrdemLocks B", 100)

	// Pedido1: item B adicionado ao carrinho ANTES de A (ordem de inserção
	// inversa à ordem ascendente de produto_id/estoque_id).
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoB, estoqueB, 2); err != nil {
		t.Fatalf("seed Pedido1 item B: %v", err)
	}
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoA, estoqueA, 2); err != nil {
		t.Fatalf("seed Pedido1 item A: %v", err)
	}
	pedido1, err := SubmeterPedido(db, empresaTeste, usuarioID, "Solicitante 1", "Obra 1", "")
	if err != nil {
		t.Fatalf("seed SubmeterPedido Pedido1: %v", err)
	}

	// Pedido2: mesmos dois produtos, item A adicionado ANTES de B.
	usuario2ID := semearConta(t, db, "Decisao OrdemLocks U2", "decisao-ordem-locks-u2@empresa.com", PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuario2ID, produtoA, estoqueA, 2); err != nil {
		t.Fatalf("seed Pedido2 item A: %v", err)
	}
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuario2ID, produtoB, estoqueB, 2); err != nil {
		t.Fatalf("seed Pedido2 item B: %v", err)
	}
	pedido2, err := SubmeterPedido(db, empresaTeste, usuario2ID, "Solicitante 2", "Obra 2", "")
	if err != nil {
		t.Fatalf("seed SubmeterPedido Pedido2: %v", err)
	}

	const iteracoes = 10
	for i := 0; i < iteracoes; i++ {
		// Cada rodada precisa de saldo de sobra e de um Pedido `pendente`
		// para cada lado — reseta o saldo e cria um novo par de Pedidos
		// pendentes a cada iteração (o par da primeira rodada já foi
		// consumido pela decisão acima).
		if i > 0 {
			if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 100 WHERE produto_id IN ($1, $2)`, produtoA, produtoB); err != nil {
				t.Fatalf("iteração %d: reset saldo: %v", i, err)
			}
			if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoB, estoqueB, 2); err != nil {
				t.Fatalf("iteração %d: seed Pedido1 item B: %v", i, err)
			}
			if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoA, estoqueA, 2); err != nil {
				t.Fatalf("iteração %d: seed Pedido1 item A: %v", i, err)
			}
			pedido1, err = SubmeterPedido(db, empresaTeste, usuarioID, "Solicitante 1", "Obra 1", "")
			if err != nil {
				t.Fatalf("iteração %d: seed SubmeterPedido Pedido1: %v", i, err)
			}
			if _, err := AdicionarItemCarrinho(db, empresaTeste, usuario2ID, produtoA, estoqueA, 2); err != nil {
				t.Fatalf("iteração %d: seed Pedido2 item A: %v", i, err)
			}
			if _, err := AdicionarItemCarrinho(db, empresaTeste, usuario2ID, produtoB, estoqueB, 2); err != nil {
				t.Fatalf("iteração %d: seed Pedido2 item B: %v", i, err)
			}
			pedido2, err = SubmeterPedido(db, empresaTeste, usuario2ID, "Solicitante 2", "Obra 2", "")
			if err != nil {
				t.Fatalf("iteração %d: seed SubmeterPedido Pedido2: %v", i, err)
			}
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var err1, err2 error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, err1 = DecidirPedido(db, empresaTeste, pedido1.ID, almoxID, PapelAlmoxarife, true)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, err2 = DecidirPedido(db, empresaTeste, pedido2.ID, almoxID, PapelAlmoxarife, true)
		}()
		close(start)
		wg.Wait()

		for nome, err := range map[string]error{"Pedido1": err1, "Pedido2": err2} {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == "40P01" {
				t.Fatalf("iteração %d, %s: deadlock detectado pelo Postgres (40P01) — ordenação por produto_id/estoque_id falhou: %v", i, nome, err)
			}
			if err != nil {
				t.Fatalf("iteração %d, %s: erro inesperado (saldo sempre de sobra): %v", i, nome, err)
			}
		}
	}
}

// --- DecidirPedido contra reserva + FEFO (Story 11.5) ------------------------

// seedPedidoReservado semeia um Produto com `legado` em produto_estoque e
// envia um Pedido de `qtd` para ele (a reserva nasce no envio). Lotes são
// semeados ANTES do envio pelo `antes`, quando informado.
func seedPedidoReservado(t *testing.T, db *sql.DB, nome, email string, legado, qtd float64, antes func(produtoID, estoqueID string)) (Pedido, string, string, string) {
	t.Helper()
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, nome, legado)
	if antes != nil {
		antes(produtoID, estoqueID)
	}
	usuarioID := semearConta(t, db, nome+" U", email, PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoID, estoqueID, qtd); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho (%s): %v", nome, err)
	}
	pedido, err := SubmeterPedido(db, empresaTeste, usuarioID, "Sol 115", "Obra 115", "")
	if err != nil {
		t.Fatalf("seed SubmeterPedido (%s): %v", nome, err)
	}
	return pedido, produtoID, estoqueID, usuarioID
}

func TestDecidirPedido_AprovaSaldoSoEmLotesPorFEFO(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	almoxID := semearConta(t, db, "D115 Lotes Almox", "d115-lotes-decisor@empresa.com", PapelAlmoxarife, 0)
	agora := time.Now()
	var l1, l2 string
	pedido, produtoID, estoqueID, _ := seedPedidoReservado(t, db, "D115 Lotes", "d115-lotes@empresa.com", 0, 5, func(p, e string) {
		// criado fora da ordem de validade: FEFO deve ignorar a criação
		l2 = seedLote(t, db, p, e, 4, "2026-10-01", agora.Add(-2*time.Hour))
		l1 = seedLote(t, db, p, e, 3, "2026-08-01", agora.Add(-1*time.Hour))
	})

	det, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
	if err != nil {
		t.Fatalf("DecidirPedido: %v", err)
	}
	if det.Status != "aprovado" || det.Itens[0].QuantidadeAprovada == nil || *det.Itens[0].QuantidadeAprovada != 5 {
		t.Fatalf("det = %+v, want aprovado com 5", det)
	}
	if q := qtdLote(t, db, l1); q != 0 {
		t.Errorf("lote 08/2026 = %v, want 0", q)
	}
	if q := qtdLote(t, db, l2); q != 2 {
		t.Errorf("lote 10/2026 = %v, want 2", q)
	}
	movs := movsDoProduto(t, db, produtoID)
	if len(movs) != 2 {
		t.Fatalf("movimentações = %+v, want 2", movs)
	}
	for _, m := range movs {
		if m.Tipo != "baixa" || !m.LoteID.Valid || m.Destino.Valid {
			t.Errorf("mov = %+v, want baixa com lote_id e sem destino", m)
		}
	}
	if movs[0].Quantidade+movs[1].Quantidade != 5 {
		t.Errorf("soma das movimentações = %+v, want 5", movs)
	}
	if n := contarReservasPedido(t, db, pedido.ID); n != 0 {
		t.Errorf("reservas = %d, want 0", n)
	}
	var usuarioMov string
	if err := db.QueryRow(`SELECT DISTINCT usuario_id FROM movimentacoes WHERE produto_id = $1`, produtoID).Scan(&usuarioMov); err != nil || usuarioMov != almoxID {
		t.Errorf("usuario_id da movimentação = %q (%v), want decisor %q", usuarioMov, err, almoxID)
	}
	_ = estoqueID
}

func TestDecidirPedido_LegadoMaisLotesSegueFEFO(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	almoxID := semearConta(t, db, "D115 Mix Almox", "d115-mix-decisor@empresa.com", PapelAlmoxarife, 0)
	agora := time.Now()
	var datado, semVal string
	pedido, produtoID, estoqueID, _ := seedPedidoReservado(t, db, "D115 Mix", "d115-mix@empresa.com", 3, 6, func(p, e string) {
		datado = seedLote(t, db, p, e, 2, "2999-01-01", agora.Add(-2*time.Hour))
		semVal = seedLote(t, db, p, e, 4, "", agora.Add(-3*time.Hour))
	})
	det, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
	if err != nil {
		t.Fatalf("DecidirPedido: %v", err)
	}
	if det.Status != "aprovado" {
		t.Errorf("status = %q, want aprovado", det.Status)
	}
	if q := qtdLote(t, db, datado); q != 0 {
		t.Errorf("lote datado = %v, want 0", q)
	}
	if q := saldoProdutoEstoque(t, db, produtoID, estoqueID); q != 0 {
		t.Errorf("legado = %v, want 0", q)
	}
	if q := qtdLote(t, db, semVal); q != 3 {
		t.Errorf("lote sem validade = %v, want 3 (4 - 1)", q)
	}
	movs := movsDoProduto(t, db, produtoID)
	if len(movs) != 3 {
		t.Fatalf("movimentações = %+v, want 3 (datado, legado, sem validade)", movs)
	}
}

func TestDecidirPedido_ReservaDeOutroPedidoNaoBloqueia(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	almoxID := semearConta(t, db, "D115 Outro Almox", "d115-outro-decisor@empresa.com", PapelAlmoxarife, 0)
	pedidoA, produtoID, estoqueID, _ := seedPedidoReservado(t, db, "D115 Outro", "d115-outro-a@empresa.com", 10, 4, nil)
	uB := semearConta(t, db, "D115 Outro B", "d115-outro-b@empresa.com", PapelUsuario, 1)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, uB, produtoID, estoqueID, 6); err != nil {
		t.Fatal(err)
	}
	pedidoB, err := SubmeterPedido(db, empresaTeste, uB, "Sol B", "Obra B", "")
	if err != nil {
		t.Fatal(err)
	}
	if d := disponivelDoPar(t, db, produtoID, estoqueID); d != 0 {
		t.Fatalf("disponível = %v, want 0", d)
	}

	det, err := DecidirPedido(db, empresaTeste, pedidoA.ID, almoxID, PapelAlmoxarife, true)
	if err != nil {
		t.Fatal(err)
	}
	if det.Status != "aprovado" || *det.Itens[0].QuantidadeAprovada != 4 {
		t.Errorf("A: %+v, want aprovado com 4", det)
	}
	if q := saldoProdutoEstoque(t, db, produtoID, estoqueID); q != 6 {
		t.Errorf("físico = %v, want 6", q)
	}
	// A reserva de B permanece intacta e B também é aprovável.
	if n := contarReservasPedido(t, db, pedidoB.ID); n != 1 {
		t.Errorf("reservas de B = %d, want 1", n)
	}
	detB, err := DecidirPedido(db, empresaTeste, pedidoB.ID, almoxID, PapelAlmoxarife, true)
	if err != nil {
		t.Fatal(err)
	}
	if detB.Status != "aprovado" {
		t.Errorf("B status = %q, want aprovado", detB.Status)
	}
	if q := saldoProdutoEstoque(t, db, produtoID, estoqueID); q != 0 {
		t.Errorf("físico final = %v, want 0", q)
	}
}

func TestDecidirPedido_BugDeReserva(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	almoxID := semearConta(t, db, "D115 Bug Almox", "d115-bug-decisor@empresa.com", PapelAlmoxarife, 0)

	t.Run("sem reserva", func(t *testing.T) {
		pedido, produtoID, estoqueID, _ := seedPedidoReservado(t, db, "D115 Bug Sem", "d115-bug-sem@empresa.com", 10, 4, nil)
		if _, err := db.Exec(`DELETE FROM reservas_pedido_item WHERE pedido_id = $1`, pedido.ID); err != nil {
			t.Fatal(err)
		}
		det, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
		if err != nil {
			t.Fatal(err)
		}
		if det.Status != "parcialmente_aprovado" || *det.Itens[0].QuantidadeAprovada != 0 {
			t.Errorf("det = %+v, want parcialmente_aprovado com 0", det)
		}
		if n := contarMovimentacoes(t, db, produtoID); n != 0 {
			t.Errorf("movimentações = %d, want 0", n)
		}
		if q := saldoProdutoEstoque(t, db, produtoID, estoqueID); q != 10 {
			t.Errorf("físico = %v, want 10", q)
		}
		var gravada float64
		if err := db.QueryRow(`SELECT quantidade_aprovada FROM pedido_itens WHERE pedido_id = $1`, pedido.ID).Scan(&gravada); err != nil || gravada != 0 {
			t.Errorf("quantidade_aprovada gravada = %v (%v), want 0", gravada, err)
		}
	})

	t.Run("reserva menor que o solicitado", func(t *testing.T) {
		pedido, produtoID, estoqueID, _ := seedPedidoReservado(t, db, "D115 Bug Menor", "d115-bug-menor@empresa.com", 10, 4, nil)
		if _, err := db.Exec(`UPDATE reservas_pedido_item SET quantidade = 3 WHERE pedido_id = $1`, pedido.ID); err != nil {
			t.Fatal(err)
		}
		det, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
		if err != nil {
			t.Fatal(err)
		}
		if det.Status != "parcialmente_aprovado" || *det.Itens[0].QuantidadeAprovada != 3 {
			t.Errorf("det = %+v, want parcialmente_aprovado com 3", det)
		}
		if q := saldoProdutoEstoque(t, db, produtoID, estoqueID); q != 7 {
			t.Errorf("físico = %v, want 7", q)
		}
	})

	t.Run("fisico abaixo da reserva", func(t *testing.T) {
		pedido, produtoID, estoqueID, _ := seedPedidoReservado(t, db, "D115 Bug Fisico", "d115-bug-fisico@empresa.com", 10, 10, nil)
		if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 4 WHERE produto_id = $1 AND estoque_id = $2`, produtoID, estoqueID); err != nil {
			t.Fatal(err)
		}
		det, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
		if err != nil {
			t.Fatal(err)
		}
		if det.Status != "parcialmente_aprovado" || *det.Itens[0].QuantidadeAprovada != 4 {
			t.Errorf("det = %+v, want parcialmente_aprovado com 4", det)
		}
		if q := saldoProdutoEstoque(t, db, produtoID, estoqueID); q != 0 {
			t.Errorf("físico = %v, want 0", q)
		}
		if n := contarReservasPedido(t, db, pedido.ID); n != 0 {
			t.Errorf("reservas = %d, want 0", n)
		}
	})
}

// TestDecidirPedido_FalhaNaMovimentacaoDesfazTudo prova a atomicidade: se a
// Movimentação falha (decisor inexistente -> FK), o débito FEFO, a decisão e a
// liberação da reserva são desfeitos.
func TestDecidirPedido_FalhaNaMovimentacaoDesfazTudo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	pedido, produtoID, estoqueID, _ := seedPedidoReservado(t, db, "D115 Rollback", "d115-rollback@empresa.com", 10, 4, nil)

	_, err := DecidirPedido(db, empresaTeste, pedido.ID, "00000000-0000-0000-0000-000000000001", PapelAlmoxarife, true)
	if err == nil {
		t.Fatal("DecidirPedido com decisor inexistente deveria falhar")
	}
	if q := saldoProdutoEstoque(t, db, produtoID, estoqueID); q != 10 {
		t.Errorf("físico = %v, want 10 (rollback do débito)", q)
	}
	if n := contarReservasPedido(t, db, pedido.ID); n != 1 {
		t.Errorf("reservas = %d, want 1 (rollback da liberação)", n)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM pedidos WHERE id = $1`, pedido.ID).Scan(&status); err != nil || status != "pendente" {
		t.Errorf("status = %q (%v), want pendente", status, err)
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 0 {
		t.Errorf("movimentações = %d, want 0", n)
	}
}

// TestDecidirPedido_ConcorrentesSoEmLotesDebitaUmaVez: duas aprovações do
// MESMO Pedido com saldo só em Lotes — só a primeira vence e o débito é único.
func TestDecidirPedido_ConcorrentesSoEmLotesDebitaUmaVez(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	almoxID := semearConta(t, db, "D115 Conc Almox", "d115-conc-decisor@empresa.com", PapelAlmoxarife, 0)
	var lote string
	pedido, produtoID, _, _ := seedPedidoReservado(t, db, "D115 Conc", "d115-conc@empresa.com", 0, 4, func(p, e string) {
		lote = seedLote(t, db, p, e, 10, "2999-01-01", time.Now())
	})

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
		}(i)
	}
	close(start)
	wg.Wait()

	sucessos, conflitos := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			sucessos++
		case errors.Is(err, ErrPedidoNaoPendente):
			conflitos++
		default:
			t.Fatalf("erro inesperado: %v", err)
		}
	}
	if sucessos != 1 || conflitos != 1 {
		t.Fatalf("sucessos=%d conflitos=%d, want 1 e 1", sucessos, conflitos)
	}
	if q := qtdLote(t, db, lote); q != 6 {
		t.Errorf("lote = %v, want 6 (10 - 4, uma vez)", q)
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 1 {
		t.Errorf("movimentações = %d, want 1", n)
	}
}

// TestDecidirPedido_LocksOpostosComLotesSemDeadlock: dois Pedidos com os
// mesmos pares (saldo em Lotes) e itens em ordens opostas, decididos em
// paralelo, nunca dão deadlock (40P01).
func TestDecidirPedido_LocksOpostosComLotesSemDeadlock(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	almoxID := semearConta(t, db, "D115 Locks Almox", "d115-locks-decisor@empresa.com", PapelAlmoxarife, 0)
	u1 := semearConta(t, db, "D115 Locks U1", "d115-locks-u1@empresa.com", PapelUsuario, 0)
	u2 := semearConta(t, db, "D115 Locks U2", "d115-locks-u2@empresa.com", PapelUsuario, 0)
	pA, eA, _ := seedProdutoComSaldo(t, db, "D115 Locks A", 0)
	pB, eB, _ := seedProdutoComSaldo(t, db, "D115 Locks B", 0)

	for i := 0; i < 8; i++ {
		seedLote(t, db, pA, eA, 20, "2999-01-01", time.Now())
		seedLote(t, db, pB, eB, 20, "2999-01-01", time.Now())
		enviar := func(u string, primeiro, segundo [2]string) Pedido {
			if _, err := AdicionarItemCarrinho(db, empresaTeste, u, primeiro[0], primeiro[1], 1); err != nil {
				t.Fatal(err)
			}
			if _, err := AdicionarItemCarrinho(db, empresaTeste, u, segundo[0], segundo[1], 1); err != nil {
				t.Fatal(err)
			}
			p, err := SubmeterPedido(db, empresaTeste, u, "Sol", "Obra", "")
			if err != nil {
				t.Fatal(err)
			}
			return p
		}
		p1 := enviar(u1, [2]string{pB, eB}, [2]string{pA, eA})
		p2 := enviar(u2, [2]string{pA, eA}, [2]string{pB, eB})

		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for k, id := range []string{p1.ID, p2.ID} {
			wg.Add(1)
			go func(k int, id string) {
				defer wg.Done()
				<-start
				_, errs[k] = DecidirPedido(db, empresaTeste, id, almoxID, PapelAlmoxarife, true)
			}(k, id)
		}
		close(start)
		wg.Wait()
		for _, err := range errs {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == "40P01" {
				t.Fatalf("iteração %d: deadlock 40P01: %v", i, err)
			}
			if err != nil {
				t.Fatalf("iteração %d: erro inesperado: %v", i, err)
			}
		}
	}
}

// --- Recibo do Pedido em PDF — Story 7.6, spec-7-6 -------------------------

// TestMontarReciboPedidoConteudo_ConteudoCorreto cobre a AC "conteúdo do
// recibo": um Pedido aprovado totalmente (via DecidirPedido) tem, no
// conteúdo montado, os itens (nome/categoria/estoque/quantidade
// retirada) e o cabeçalho (solicitante, aprovador via join, data da
// decisão == pedidos.decidido_em).
func TestMontarReciboPedidoConteudo_ConteudoCorreto(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Recibo Conteudo", "recibo-conteudo@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Recibo Conteudo Almox", "recibo-conteudo-almox@empresa.com", PapelAlmoxarife, 0)
	pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Recibo Conteudo A", SaldoInicial: 10, QtdSolicitada: 4},
	})
	_ = pares

	det, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true)
	if err != nil {
		t.Fatalf("seed DecidirPedido: %v", err)
	}

	var nomeAprovador string
	if err := db.QueryRow(`SELECT nome FROM usuarios WHERE id = $1`, almoxID).Scan(&nomeAprovador); err != nil {
		t.Fatalf("seed: buscar nome do aprovador: %v", err)
	}

	conteudo, err := MontarReciboPedidoConteudo(db, empresaTeste, pedido.ID, usuarioID, PapelUsuario)
	if err != nil {
		t.Fatalf("MontarReciboPedidoConteudo erro inesperado: %v", err)
	}
	if conteudo.Solicitante != "Solicitante Decisao" {
		t.Errorf("Solicitante = %q, want %q", conteudo.Solicitante, "Solicitante Decisao")
	}
	if conteudo.ObraCentroCusto != "Obra Decisao" {
		t.Errorf("ObraCentroCusto = %q, want %q", conteudo.ObraCentroCusto, "Obra Decisao")
	}
	if conteudo.Status != "aprovado" {
		t.Errorf("Status = %q, want aprovado", conteudo.Status)
	}
	if conteudo.Aprovador != nomeAprovador {
		t.Errorf("Aprovador = %q, want %q (nome de usuarios via join)", conteudo.Aprovador, nomeAprovador)
	}
	if det.DecididoEm == nil || !conteudo.DecididoEm.Equal(*det.DecididoEm) {
		t.Errorf("DecididoEm = %v, want %v (pedidos.decidido_em)", conteudo.DecididoEm, det.DecididoEm)
	}
	if len(conteudo.Itens) != 1 {
		t.Fatalf("len(Itens) = %d, want 1", len(conteudo.Itens))
	}
	item := conteudo.Itens[0]
	if item.ProdutoNome != "Produto Recibo Conteudo A" || item.EstoqueNome != "Recibo Conteudo A" {
		t.Errorf("item = %+v", item)
	}
	if item.Quantidade != 4 || item.QuantidadeAprovada != 4 {
		t.Errorf("Quantidade/QuantidadeAprovada = %v/%v, want 4/4", item.Quantidade, item.QuantidadeAprovada)
	}
}

// TestMontarReciboPedidoConteudo_ItemDivergente cobre um item com
// `quantidadeAprovada != quantidade` (aprovação parcial) — as duas
// quantidades chegam lado a lado no conteúdo montado, nunca uma escondendo a
// outra.
func TestMontarReciboPedidoConteudo_ItemDivergente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Recibo Divergente", "recibo-divergente@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Recibo Divergente Almox", "recibo-divergente-almox@empresa.com", PapelAlmoxarife, 0)
	pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Recibo Divergente A", SaldoInicial: 10, QtdSolicitada: 10},
	})
	// Saldo real cai abaixo do solicitado ENTRE o envio e a decisão (mesmo
	// molde de TestDecidirPedidoHandler_200AprovacaoParcial) — DecidirPedido
	// revalida no momento exato da decisão, nunca confia no snapshot do
	// envio.
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 4 WHERE produto_id = $1 AND estoque_id = $2`, pares[0].ProdutoID, pares[0].EstoqueID); err != nil {
		t.Fatalf("seed: reduzir saldo real: %v", err)
	}
	if _, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true); err != nil {
		t.Fatalf("seed DecidirPedido: %v", err)
	}

	conteudo, err := MontarReciboPedidoConteudo(db, empresaTeste, pedido.ID, usuarioID, PapelUsuario)
	if err != nil {
		t.Fatalf("MontarReciboPedidoConteudo erro inesperado: %v", err)
	}
	if conteudo.Status != "parcialmente_aprovado" {
		t.Errorf("Status = %q, want parcialmente_aprovado", conteudo.Status)
	}
	if len(conteudo.Itens) != 1 {
		t.Fatalf("len(Itens) = %d, want 1", len(conteudo.Itens))
	}
	if item := conteudo.Itens[0]; item.Quantidade != 10 || item.QuantidadeAprovada != 4 {
		t.Errorf("item = %+v, want Quantidade=10 QuantidadeAprovada=4", item)
	}
}

// TestMontarReciboPedidoConteudo_GatePendente cobre a linha "Pedido pendente"
// da I/O Matrix: nenhum PDF é gerado, ErrPedidoSemRecibo devolvido ANTES de
// resolver o aprovador.
func TestMontarReciboPedidoConteudo_GatePendente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Recibo Pendente", "recibo-pendente@empresa.com", PapelUsuario, 0)
	pedido, _ := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Recibo Pendente A", SaldoInicial: 10, QtdSolicitada: 1},
	})

	_, err := MontarReciboPedidoConteudo(db, empresaTeste, pedido.ID, usuarioID, PapelUsuario)
	if !errors.Is(err, ErrPedidoSemRecibo) {
		t.Fatalf("erro = %v, want ErrPedidoSemRecibo", err)
	}
}

// TestMontarReciboPedidoConteudo_GateRejeitado cobre a linha "Pedido
// rejeitado" da I/O Matrix.
func TestMontarReciboPedidoConteudo_GateRejeitado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Recibo Rejeitado", "recibo-rejeitado@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Recibo Rejeitado Almox", "recibo-rejeitado-almox@empresa.com", PapelAlmoxarife, 0)
	pedido, _ := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Recibo Rejeitado A", SaldoInicial: 10, QtdSolicitada: 1},
	})
	if _, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, false); err != nil {
		t.Fatalf("seed DecidirPedido (rejeitar): %v", err)
	}

	_, err := MontarReciboPedidoConteudo(db, empresaTeste, pedido.ID, usuarioID, PapelUsuario)
	if !errors.Is(err, ErrPedidoSemRecibo) {
		t.Fatalf("erro = %v, want ErrPedidoSemRecibo", err)
	}
}

// TestMontarReciboPedidoConteudo_PedidoAlheio prova que MontarReciboPedidoConteudo
// reaproveita o MESMO colapso de ErrPedidoNaoEncontrado de BuscarPedidoProprio
// (Pedido de outro usuário sem papel almoxarife+).
func TestMontarReciboPedidoConteudo_PedidoAlheio(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	dono := semearConta(t, db, "Recibo Alheio Dono", "recibo-alheio-dono@empresa.com", PapelUsuario, 0)
	outro := semearConta(t, db, "Recibo Alheio Outro", "recibo-alheio-outro@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Recibo Alheio Almox", "recibo-alheio-almox@empresa.com", PapelAlmoxarife, 0)
	pedido, _ := seedPedidoComItens(t, db, dono, []itemPedidoSeedSpec{
		{NomeBase: "Recibo Alheio A", SaldoInicial: 10, QtdSolicitada: 1},
	})
	if _, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true); err != nil {
		t.Fatalf("seed DecidirPedido: %v", err)
	}

	_, err := MontarReciboPedidoConteudo(db, empresaTeste, pedido.ID, outro, PapelUsuario)
	if !errors.Is(err, ErrPedidoNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrPedidoNaoEncontrado", err)
	}
}

// TestRenderizarReciboPedidoPDF_BytesValidosEDeterministico cobre "bytes
// começam com %PDF-" e o determinismo em si — a MESMA entrada
// (ReciboPedidoConteudo) produz SEMPRE os MESMOS bytes.
func TestRenderizarReciboPedidoPDF_BytesValidosEDeterministico(t *testing.T) {
	quantidadeAprovada := 4.0
	conteudo := ReciboPedidoConteudo{
		PedidoID:        "id-fake",
		Solicitante:     "Fulano de Tal",
		ObraCentroCusto: "Obra Ção",
		Status:          "parcialmente_aprovado",
		Aprovador:       "Aprovador Ñ",
		DecididoEm:      time.Date(2026, 9, 3, 14, 30, 0, 0, time.UTC),
		Itens: []ReciboPedidoItem{
			{ProdutoNome: "Parafuso", CategoriaNome: "Fixação", EstoqueNome: "Canteiro A", Quantidade: 10, QuantidadeAprovada: quantidadeAprovada},
		},
	}

	b1, err := RenderizarReciboPedidoPDF(conteudo)
	if err != nil {
		t.Fatalf("RenderizarReciboPedidoPDF erro inesperado (1ª chamada): %v", err)
	}
	if !bytes.HasPrefix(b1, []byte("%PDF-")) {
		t.Fatalf("PDF não começa com %%PDF-: %q", b1[:min(20, len(b1))])
	}

	b2, err := RenderizarReciboPedidoPDF(conteudo)
	if err != nil {
		t.Fatalf("RenderizarReciboPedidoPDF erro inesperado (2ª chamada): %v", err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatalf("PDFs diferem entre duas chamadas com o MESMO conteúdo (len1=%d, len2=%d)", len(b1), len(b2))
	}
}

// TestGerarReciboPedidoPDF_DeterministicoAposEdicaoDoProduto cobre a AC
// central da story: dois downloads do MESMO Pedido decidido, com o Produto
// referenciado EDITADO entre eles, produzem bytes byte-a-byte IDÊNTICOS — o
// recibo nunca faz join ao vivo com `produtos` (AD-17), lê sempre o snapshot
// de pedido_itens.
func TestGerarReciboPedidoPDF_DeterministicoAposEdicaoDoProduto(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Recibo Determinismo", "recibo-determinismo@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Recibo Determinismo Almox", "recibo-determinismo-almox@empresa.com", PapelAlmoxarife, 0)
	pedido, pares := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Recibo Determinismo A", SaldoInicial: 10, QtdSolicitada: 4},
	})
	if _, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true); err != nil {
		t.Fatalf("seed DecidirPedido: %v", err)
	}

	b1, err := GerarReciboPedidoPDF(db, empresaTeste, pedido.ID, usuarioID, PapelUsuario)
	if err != nil {
		t.Fatalf("GerarReciboPedidoPDF erro inesperado (1º download): %v", err)
	}
	if !bytes.HasPrefix(b1, []byte("%PDF-")) {
		t.Fatalf("PDF não começa com %%PDF-: %q", b1[:min(20, len(b1))])
	}

	// Produto editado entre os dois downloads — o recibo não deve refletir a
	// mudança, porque nunca faz join ao vivo com `produtos`.
	if _, err := db.Exec(`UPDATE produtos SET nome = 'Nome Totalmente Diferente' WHERE id = $1`, pares[0].ProdutoID); err != nil {
		t.Fatalf("seed: editar produto entre downloads: %v", err)
	}

	b2, err := GerarReciboPedidoPDF(db, empresaTeste, pedido.ID, usuarioID, PapelUsuario)
	if err != nil {
		t.Fatalf("GerarReciboPedidoPDF erro inesperado (2º download): %v", err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatalf("PDFs diferem entre dois downloads do MESMO pedido após edição do produto (len1=%d, len2=%d)", len(b1), len(b2))
	}
}

// TestGerarReciboPedidoPDF_GatePendente prova que GerarReciboPedidoPDF
// propaga ErrPedidoSemRecibo (via MontarReciboPedidoConteudo) sem chegar a
// renderizar nada.
func TestGerarReciboPedidoPDF_GatePendente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Recibo Gerar Pendente", "recibo-gerar-pendente@empresa.com", PapelUsuario, 0)
	pedido, _ := seedPedidoComItens(t, db, usuarioID, []itemPedidoSeedSpec{
		{NomeBase: "Recibo Gerar Pendente A", SaldoInicial: 10, QtdSolicitada: 1},
	})

	_, err := GerarReciboPedidoPDF(db, empresaTeste, pedido.ID, usuarioID, PapelUsuario)
	if !errors.Is(err, ErrPedidoSemRecibo) {
		t.Fatalf("erro = %v, want ErrPedidoSemRecibo", err)
	}
}

// --- Centro de Custo no envio (Story 12.3, spec-12-3) -----------------------

func centroCustoGravado(t *testing.T, db *sql.DB, pedidoID string) sql.NullString {
	t.Helper()
	var v sql.NullString
	if err := db.QueryRow(`SELECT centro_custo_id FROM pedidos WHERE id = $1`, pedidoID).Scan(&v); err != nil {
		t.Fatalf("falha ao ler centro_custo_id: %v", err)
	}
	return v
}

func TestSubmeterPedidoComCentroCusto_SemCentroEComCentroValido(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	limparCentrosCustoDeTeste(t, db, empresaTeste)

	usuarioID := semearConta(t, db, "Usuario Pedido CC", "pedido-cc@empresa.com", PapelUsuario, 0)
	centro, err := CriarCentroCusto(db, empresaTeste, "Estoque do Cabo CC")
	if err != nil {
		t.Fatalf("seed centro: %v", err)
	}
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Pedido CC", 20)

	// Sem Centro (vazio e só espaços): comportamento de hoje, coluna NULL.
	for _, id := range []string{"", "   "} {
		if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoID, estoqueID, 1); err != nil {
			t.Fatalf("seed carrinho: %v", err)
		}
		p, err := SubmeterPedidoComCentroCusto(db, empresaTeste, usuarioID, "Fulano", "Obra Livre", "", id)
		if err != nil {
			t.Fatalf("sem centro (%q): %v", id, err)
		}
		if p.CentroCustoID != nil || centroCustoGravado(t, db, p.ID).Valid {
			t.Errorf("sem centro (%q): centro_custo_id deveria ser nulo", id)
		}
	}

	// Com Centro válido: gravado e devolvido; texto livre inalterado.
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoID, estoqueID, 1); err != nil {
		t.Fatalf("seed carrinho: %v", err)
	}
	p, err := SubmeterPedidoComCentroCusto(db, empresaTeste, usuarioID, "Fulano", "  Obra Norte ", "", centro.ID)
	if err != nil {
		t.Fatalf("com centro: %v", err)
	}
	if p.CentroCustoID == nil || *p.CentroCustoID != centro.ID {
		t.Errorf("CentroCustoID = %v, want %s", p.CentroCustoID, centro.ID)
	}
	if g := centroCustoGravado(t, db, p.ID); !g.Valid || g.String != centro.ID {
		t.Errorf("centro_custo_id gravado = %v, want %s", g, centro.ID)
	}
	if p.ObraCentroCusto != "Obra Norte" {
		t.Errorf("ObraCentroCusto = %q, want texto livre inalterado", p.ObraCentroCusto)
	}
}

func TestSubmeterPedidoComCentroCusto_InvalidoNaoGravaNemEsvaziaCarrinho(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	removerEmpresaDeTeste(t, db, "cc-pedido-outra")
	outra := criarEmpresaDeTeste(t, db, "cc-pedido-outra", "778889990205", "CC Pedido Outra")
	limparCentrosCustoDeTeste(t, db, empresaTeste, outra.ID)
	t.Cleanup(func() { removerEmpresaDeTeste(t, db, "cc-pedido-outra") })

	usuarioID := semearConta(t, db, "Usuario Pedido CC Inv", "pedido-cc-inv@empresa.com", PapelUsuario, 0)
	alheio, err := CriarCentroCusto(db, outra.ID, "Centro Alheio")
	if err != nil {
		t.Fatalf("seed centro alheio: %v", err)
	}
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Pedido CC Inv", 20)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoID, estoqueID, 2); err != nil {
		t.Fatalf("seed carrinho: %v", err)
	}

	for nome, id := range map[string]string{
		"outra empresa": alheio.ID,
		"inexistente":   "00000000-0000-0000-0000-000000000000",
		"malformado":    "abc",
	} {
		antes := contarPedidos(t, db)
		_, err := SubmeterPedidoComCentroCusto(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", id)
		var ev *ErroPedidoValidacao
		if !errors.As(err, &ev) || ev.Mensagem != "centro de custo inválido" {
			t.Errorf("%s: erro = %v, want ErroPedidoValidacao \"centro de custo inválido\"", nome, err)
		}
		if depois := contarPedidos(t, db); depois != antes {
			t.Errorf("%s: pedidos = %d, want %d", nome, depois, antes)
		}
		if n := contarItensCarrinho(t, db, usuarioID); n != 1 {
			t.Errorf("%s: carrinho = %d itens, want 1 (intacto)", nome, n)
		}
	}
}

func TestSubmeterPedidoComCentroCusto_TextoLivreSegueObrigatorio(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	limparCentrosCustoDeTeste(t, db, empresaTeste)

	usuarioID := semearConta(t, db, "Usuario Pedido CC Txt", "pedido-cc-txt@empresa.com", PapelUsuario, 0)
	centro, err := CriarCentroCusto(db, empresaTeste, "Centro Texto Obrigatorio")
	if err != nil {
		t.Fatalf("seed centro: %v", err)
	}
	_, err = SubmeterPedidoComCentroCusto(db, empresaTeste, usuarioID, "Fulano", "   ", "", centro.ID)
	var ev *ErroPedidoValidacao
	if !errors.As(err, &ev) {
		t.Fatalf("erro = %v, want *ErroPedidoValidacao", err)
	}
}

// TestPedido_CentroCustoAparecemEmDetalheFilaDecisaoERecibo prova a decisão do
// code review dos Épicos 10-12: o Centro de Custo cadastrado escolhido no envio
// deixa de ser write-only e aparece no detalhe, na lista/Fila, na resposta da
// decisão e no conteúdo do recibo; um Pedido sem Centro devolve nil/vazio.
func TestPedido_CentroCustoAparecemEmDetalheFilaDecisaoERecibo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	limparCentrosCustoDeTeste(t, db, empresaTeste)

	usuarioID := semearConta(t, db, "Usuario CC Exibe", "pedido-cc-exibe@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Almox CC Exibe", "cc-exibe-decisor@empresa.com", PapelAlmoxarife, 0)
	centro, err := CriarCentroCusto(db, empresaTeste, "Estoque do Cabo Exibe")
	if err != nil {
		t.Fatalf("seed centro: %v", err)
	}
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Pedido CC Exibe", 20)
	for _, id := range []string{centro.ID, ""} {
		if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioID, produtoID, estoqueID, 1); err != nil {
			t.Fatalf("seed carrinho: %v", err)
		}
		if _, err := SubmeterPedidoComCentroCusto(db, empresaTeste, usuarioID, "Fulano", "Obra "+id[:min(len(id), 1)], "", id); err != nil {
			t.Fatalf("submeter (%q): %v", id, err)
		}
	}

	fila, err := ListarPedidosFila(db, empresaTeste, "")
	if err != nil {
		t.Fatalf("ListarPedidosFila: %v", err)
	}
	var comCentro, semCentro *PedidoResumo
	for i := range fila {
		if fila[i].CentroCustoNome != nil {
			comCentro = &fila[i]
		} else {
			semCentro = &fila[i]
		}
	}
	if comCentro == nil || *comCentro.CentroCustoNome != "Estoque do Cabo Exibe" {
		t.Fatalf("Fila: nenhum pedido com o Centro de Custo esperado: %+v", fila)
	}
	if semCentro == nil {
		t.Fatalf("Fila: o pedido sem Centro deveria ter CentroCustoNome nil")
	}
	proprios, err := ListarPedidosProprios(db, empresaTeste, usuarioID, "")
	if err != nil {
		t.Fatalf("ListarPedidosProprios: %v", err)
	}
	achou := false
	for _, p := range proprios {
		if p.CentroCustoNome != nil && *p.CentroCustoNome == "Estoque do Cabo Exibe" {
			achou = true
		}
	}
	if !achou {
		t.Errorf("Meus Pedidos não trouxe o Centro de Custo")
	}

	det, err := BuscarPedidoProprio(db, empresaTeste, comCentro.ID, usuarioID, PapelUsuario)
	if err != nil {
		t.Fatalf("BuscarPedidoProprio: %v", err)
	}
	if det.CentroCustoNome == nil || *det.CentroCustoNome != "Estoque do Cabo Exibe" {
		t.Errorf("detalhe: CentroCustoNome = %v", det.CentroCustoNome)
	}
	semDet, err := BuscarPedidoProprio(db, empresaTeste, semCentro.ID, usuarioID, PapelUsuario)
	if err != nil || semDet.CentroCustoNome != nil {
		t.Errorf("detalhe sem centro: err=%v CentroCustoNome=%v, want nil", err, semDet.CentroCustoNome)
	}

	decidido, err := DecidirPedido(db, empresaTeste, comCentro.ID, almoxID, PapelAlmoxarife, true)
	if err != nil {
		t.Fatalf("DecidirPedido: %v", err)
	}
	if decidido.CentroCustoNome == nil || *decidido.CentroCustoNome != "Estoque do Cabo Exibe" {
		t.Errorf("resposta da decisão: CentroCustoNome = %v", decidido.CentroCustoNome)
	}
	recibo, err := MontarReciboPedidoConteudo(db, empresaTeste, comCentro.ID, almoxID, PapelAlmoxarife)
	if err != nil {
		t.Fatalf("MontarReciboPedidoConteudo: %v", err)
	}
	if recibo.CentroCusto != "Estoque do Cabo Exibe" {
		t.Errorf("recibo.CentroCusto = %q", recibo.CentroCusto)
	}
	if pdf, err := RenderizarReciboPedidoPDF(recibo); err != nil || len(pdf) == 0 {
		t.Errorf("RenderizarReciboPedidoPDF: len=%d err=%v", len(pdf), err)
	}
}

// --- IndicadoresPedidos — Story 17.4 (Epic 17, Novo visual) -----------------

// inserirPedidoDireto insere uma linha em `pedidos` com o status informado
// (sem itens nem reservas) — usado exclusivamente em testes de indicadores,
// onde não é necessário passar pelo fluxo completo de carrinho+envio.
func inserirPedidoDireto(t *testing.T, db *sql.DB, empresaID, usuarioID, status string) string {
	t.Helper()
	var id string
	const q = `
		INSERT INTO pedidos (usuario_id, solicitante, obra_centro_custo, empresa_id, status)
		VALUES ($1, 'Solicitante Ind', 'Obra Ind', $2, $3)
		RETURNING id`
	if err := db.QueryRow(q, usuarioID, empresaID, status).Scan(&id); err != nil {
		t.Fatalf("inserirPedidoDireto: %v", err)
	}
	return id
}

// marcarPedidoDecidido força `decidido_em = now()` em um pedido já inserido,
// simulando o que DecidirPedido faz dentro da transação — usado só em testes
// de indicadores de mês.
func marcarPedidoDecidido(t *testing.T, db *sql.DB, pedidoID, status string) {
	t.Helper()
	if _, err := db.Exec(
		`UPDATE pedidos SET status = $2, decidido_em = now(), decidido_por = usuario_id WHERE id = $1`,
		pedidoID, status,
	); err != nil {
		t.Fatalf("marcarPedidoDecidido: %v", err)
	}
}

func TestIndicadoresPedidos_EscopoProprio(t *testing.T) {
	db := testDB(t)
	usuarioA := semearConta(t, db, "Ind Proprio A", "ind-proprio-a-174@empresa.com", PapelUsuario, 0)
	usuarioB := semearConta(t, db, "Ind Proprio B", "ind-proprio-b-174@empresa.com", PapelUsuario, 0)

	// A tem 2 pedidos pendentes; B tem 1.
	inserirPedidoDireto(t, db, empresaTeste, usuarioA, "pendente")
	inserirPedidoDireto(t, db, empresaTeste, usuarioA, "pendente")
	inserirPedidoDireto(t, db, empresaTeste, usuarioB, "pendente")

	ind, err := IndicadoresPedidos(db, empresaTeste, usuarioA, PapelUsuario, false)
	if err != nil {
		t.Fatalf("IndicadoresPedidos: %v", err)
	}
	if ind.Pendentes != 2 {
		t.Errorf("Pendentes = %d, want 2 (só os de A)", ind.Pendentes)
	}
}

func TestIndicadoresPedidos_EscopoTodosAlmoxarife(t *testing.T) {
	db := testDB(t)
	usuarioID := semearConta(t, db, "Ind Todos Usua", "ind-todos-usua-174@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "Ind Todos Almox", "ind-todos-almox-174@empresa.com", PapelAlmoxarife, 0)

	inserirPedidoDireto(t, db, empresaTeste, usuarioID, "pendente")
	inserirPedidoDireto(t, db, empresaTeste, almoxID, "pendente")

	// Almoxarife com escopoTodos vê os dois.
	ind, err := IndicadoresPedidos(db, empresaTeste, almoxID, PapelAlmoxarife, true)
	if err != nil {
		t.Fatalf("IndicadoresPedidos todos: %v", err)
	}
	if ind.Pendentes < 2 {
		t.Errorf("Pendentes = %d, want >= 2 (todos da empresa)", ind.Pendentes)
	}
}

func TestIndicadoresPedidos_PapelInsuficienteIgnoraEscopoTodos(t *testing.T) {
	db := testDB(t)
	usuarioA := semearConta(t, db, "Ind Papel Insuf A", "ind-papel-insuf-a-174@empresa.com", PapelUsuario, 0)
	usuarioB := semearConta(t, db, "Ind Papel Insuf B", "ind-papel-insuf-b-174@empresa.com", PapelUsuario, 0)

	// A tem 1 pedido pendente, B tem 1.
	inserirPedidoDireto(t, db, empresaTeste, usuarioA, "pendente")
	inserirPedidoDireto(t, db, empresaTeste, usuarioB, "pendente")

	// A com papel usuario e escopoTodos=true continua vendo só o próprio.
	ind, err := IndicadoresPedidos(db, empresaTeste, usuarioA, PapelUsuario, true)
	if err != nil {
		t.Fatalf("IndicadoresPedidos: %v", err)
	}
	if ind.Pendentes != 1 {
		t.Errorf("Pendentes = %d, want 1 (só os de A, papel insuficiente)", ind.Pendentes)
	}
}

func TestIndicadoresPedidos_AprovadosERejeitadosNoMes(t *testing.T) {
	db := testDB(t)
	usuarioID := semearConta(t, db, "Ind Mes Usua", "ind-mes-usua-174@empresa.com", PapelUsuario, 0)

	// 2 pendentes, 1 aprovado no mês, 1 rejeitado no mês.
	inserirPedidoDireto(t, db, empresaTeste, usuarioID, "pendente")
	inserirPedidoDireto(t, db, empresaTeste, usuarioID, "pendente")
	aprov := inserirPedidoDireto(t, db, empresaTeste, usuarioID, "aprovado")
	marcarPedidoDecidido(t, db, aprov, "aprovado")
	rejeit := inserirPedidoDireto(t, db, empresaTeste, usuarioID, "rejeitado")
	marcarPedidoDecidido(t, db, rejeit, "rejeitado")

	ind, err := IndicadoresPedidos(db, empresaTeste, usuarioID, PapelUsuario, false)
	if err != nil {
		t.Fatalf("IndicadoresPedidos: %v", err)
	}
	if ind.Pendentes != 2 {
		t.Errorf("Pendentes = %d, want 2", ind.Pendentes)
	}
	if ind.AprovadosMes != 1 {
		t.Errorf("AprovadosMes = %d, want 1", ind.AprovadosMes)
	}
	if ind.RejeitadosMes != 1 {
		t.Errorf("RejeitadosMes = %d, want 1", ind.RejeitadosMes)
	}
}

func TestIndicadoresPedidos_EmpresaIsolada(t *testing.T) {
	db := testDB(t)
	// Cria uma segunda Empresa e insere um pedido nela. removerEmpresaDeTeste
	// não apaga pedidos nem contas, então este teste limpa os que ele mesmo
	// cria — antes (sobra de execução interrompida) e depois — senão a
	// Empresa não sai e a próxima execução no mesmo banco falha no slug.
	limparOutraEmpresa := func() {
		for _, stmt := range []string{
			`DELETE FROM pedido_itens WHERE pedido_id IN (SELECT p.id FROM pedidos p JOIN empresas e ON e.id = p.empresa_id WHERE e.slug = 'outra-empresa-ind-174')`,
			`DELETE FROM pedidos WHERE empresa_id IN (SELECT id FROM empresas WHERE slug = 'outra-empresa-ind-174')`,
			`DELETE FROM usuarios WHERE empresa_id IN (SELECT id FROM empresas WHERE slug = 'outra-empresa-ind-174')`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("limpar outra empresa: %v", err)
			}
		}
		removerEmpresaDeTeste(t, db, "outra-empresa-ind-174")
	}
	limparOutraEmpresa()
	outraEmpresa := criarEmpresaDeTeste(t, db, "outra-empresa-ind-174", "777888990001", "Outra Empresa Ind 174")
	t.Cleanup(limparOutraEmpresa)
	usuarioOutra := semearContaNaEmpresa(t, db, outraEmpresa.ID, "Ind Outra Empresa", "ind-outra-174@outra.com", PapelUsuario)
	inserirPedidoDireto(t, db, outraEmpresa.ID, usuarioOutra, "pendente")

	// IndicadoresPedidos para empresaTeste não deve contar o pedido da outra.
	usuarioTeste := semearConta(t, db, "Ind Iso Teste", "ind-iso-teste-174@empresa.com", PapelAlmoxarife, 0)
	antes, err := IndicadoresPedidos(db, empresaTeste, usuarioTeste, PapelAlmoxarife, true)
	if err != nil {
		t.Fatalf("IndicadoresPedidos antes: %v", err)
	}
	// Agora insere também em empresaTeste para ter baseline.
	inserirPedidoDireto(t, db, empresaTeste, usuarioTeste, "pendente")
	depois, err := IndicadoresPedidos(db, empresaTeste, usuarioTeste, PapelAlmoxarife, true)
	if err != nil {
		t.Fatalf("IndicadoresPedidos depois: %v", err)
	}
	if depois.Pendentes != antes.Pendentes+1 {
		t.Errorf("EmpresaIsolada: Pendentes cresceu %d (want 1) ao inserir em empresaTeste", depois.Pendentes-antes.Pendentes)
	}
}

// --- SubmeterPedidoTransferencia (Story 20.1, Epic 20, spec-20-1) ---------

// pedidoItemTransferenciaLinha é a leitura crua de uma linha de
// pedido_itens de um Pedido de Transferência — inclui estoque_destino_id,
// que listarPedidoItens (acima, molde do Pedido de retirada) não lê.
type pedidoItemTransferenciaLinha struct {
	ProdutoID        string
	ProdutoNome      string
	CategoriaNome    string
	EstoqueID        string
	EstoqueNome      string
	Quantidade       float64
	EstoqueDestinoID sql.NullString
}

func listarPedidoItensTransferencia(t *testing.T, db *sql.DB, pedidoID string) []pedidoItemTransferenciaLinha {
	t.Helper()
	rows, err := db.Query(
		`SELECT produto_id, produto_nome, categoria_nome, estoque_id, estoque_nome, quantidade, estoque_destino_id
		 FROM pedido_itens WHERE pedido_id = $1 ORDER BY produto_nome`,
		pedidoID,
	)
	if err != nil {
		t.Fatalf("falha ao listar pedido_itens (transferência): %v", err)
	}
	defer rows.Close()
	var itens []pedidoItemTransferenciaLinha
	for rows.Next() {
		var it pedidoItemTransferenciaLinha
		if err := rows.Scan(&it.ProdutoID, &it.ProdutoNome, &it.CategoriaNome, &it.EstoqueID, &it.EstoqueNome, &it.Quantidade, &it.EstoqueDestinoID); err != nil {
			t.Fatalf("falha ao ler linha de pedido_itens (transferência): %v", err)
		}
		itens = append(itens, it)
	}
	return itens
}

func contarReservas(t *testing.T, db *sql.DB, pedidoID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM reservas_pedido_item WHERE pedido_id = $1`, pedidoID).Scan(&n); err != nil {
		t.Fatalf("falha ao contar reservas_pedido_item: %v", err)
	}
	return n
}

// criarEstoqueDestinoTeste cria um segundo Estoque (mesma Filial de
// empresaTeste) para servir de destino de uma Transferência — nunca recebe
// saldo (a Transferência de verdade, Story 20.3, nunca é chamada aqui).
func criarEstoqueDestinoTeste(t *testing.T, db *sql.DB, nome string) string {
	t.Helper()
	estoque, err := CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), nome)
	if err != nil {
		t.Fatalf("seed CriarEstoque (destino): %v", err)
	}
	return estoque.ID
}

// TestSubmeterPedidoTransferencia_Sucesso prova a linha "Solicitação válida"
// da I/O Matrix de spec-20-1: Pedido `pendente` tipo=transferencia, reserva
// de 5 na ORIGEM, saldo físico inalterado nos dois Estoques.
func TestSubmeterPedidoTransferencia_Sucesso(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Transf Sucesso", "transf-sucesso@empresa.com", PapelUsuario, 0)
	produtoID, estoqueOrigemID, _ := seedProdutoComSaldo(t, db, "Transf Sucesso", 10)
	estoqueDestinoID := criarEstoqueDestinoTeste(t, db, "Transf Sucesso Destino")

	pedido, err := SubmeterPedidoTransferencia(
		db, empresaTeste, usuarioID, "  Fulano de Tal  ", "  Obra Norte  ", "  urgente  ",
		produtoID, estoqueOrigemID, estoqueDestinoID, 5,
	)
	if err != nil {
		t.Fatalf("SubmeterPedidoTransferencia erro inesperado: %v", err)
	}
	if pedido.ID == "" {
		t.Error("ID vazio no retorno")
	}
	if pedido.Status != "pendente" {
		t.Errorf("Status = %q, want pendente", pedido.Status)
	}
	if pedido.Tipo != "transferencia" {
		t.Errorf("Tipo = %q, want transferencia", pedido.Tipo)
	}
	if pedido.Solicitante != "Fulano de Tal" {
		t.Errorf("Solicitante = %q, want trimado", pedido.Solicitante)
	}
	if pedido.Observacao == nil || *pedido.Observacao != "urgente" {
		t.Errorf("Observacao = %v, want \"urgente\"", pedido.Observacao)
	}

	itens := listarPedidoItensTransferencia(t, db, pedido.ID)
	if len(itens) != 1 {
		t.Fatalf("len(itens) = %d, want 1", len(itens))
	}
	it := itens[0]
	if it.ProdutoID != produtoID || it.EstoqueID != estoqueOrigemID || it.Quantidade != 5 {
		t.Errorf("item = %+v, want produto=%s estoque(origem)=%s quantidade=5", it, produtoID, estoqueOrigemID)
	}
	if !it.EstoqueDestinoID.Valid || it.EstoqueDestinoID.String != estoqueDestinoID {
		t.Errorf("EstoqueDestinoID = %+v, want %q", it.EstoqueDestinoID, estoqueDestinoID)
	}
	if it.ProdutoNome == "" || it.CategoriaNome == "" || it.EstoqueNome == "" {
		t.Errorf("snapshot incompleto: %+v", it)
	}

	if n := contarReservas(t, db, pedido.ID); n != 1 {
		t.Errorf("reservas_pedido_item do pedido = %d, want 1 (só a origem)", n)
	}

	// Never (spec-20-1): nunca debita/credita saldo físico na solicitação.
	if q := saldoProdutoEstoque(t, db, produtoID, estoqueOrigemID); q != 10 {
		t.Errorf("saldo da origem não deveria mudar, got %v, want 10", q)
	}
}

// TestSubmeterPedidoTransferencia_ObraCentroCustoOpcional prova o requisito
// do usuário (pós-revisão, mesmo molde de
// TestSubmeterPedidoBaixa_ObraCentroCustoOpcional): "Obra/Centro de Custo"
// deixa de ser obrigatório também no Pedido de Transferência — vazio/só
// espaços NÃO é &ErroPedidoValidacao, grava NULL na coluna
// (migrations/000058) e `pedido.ObraCentroCusto` volta "" (nunca quebra o
// Scan).
func TestSubmeterPedidoTransferencia_ObraCentroCustoOpcional(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Transf SemObra", "transf-semobra@empresa.com", PapelUsuario, 0)
	produtoID, estoqueOrigemID, _ := seedProdutoComSaldo(t, db, "Transf SemObra", 10)
	estoqueDestinoID := criarEstoqueDestinoTeste(t, db, "Transf SemObra Destino")

	pedido, err := SubmeterPedidoTransferencia(
		db, empresaTeste, usuarioID, "Fulano", "   ", "",
		produtoID, estoqueOrigemID, estoqueDestinoID, 5,
	)
	if err != nil {
		t.Fatalf("SubmeterPedidoTransferencia erro inesperado: %v", err)
	}
	if pedido.ObraCentroCusto != "" {
		t.Errorf("ObraCentroCusto = %q, want \"\"", pedido.ObraCentroCusto)
	}

	var obraNoBanco sql.NullString
	if err := db.QueryRow(`SELECT obra_centro_custo FROM pedidos WHERE id = $1`, pedido.ID).Scan(&obraNoBanco); err != nil {
		t.Fatalf("ler obra_centro_custo: %v", err)
	}
	if obraNoBanco.Valid {
		t.Errorf("obra_centro_custo no banco = %+v, want NULL", obraNoBanco)
	}

	// GET /api/pedidos (ListarPedidosProprios) e GET /api/pedidos/{id}
	// (BuscarPedidoProprio) nunca quebram ao reler esta linha com
	// obra_centro_custo NULL (COALESCE nas duas queries).
	resumos, err := ListarPedidosProprios(db, empresaTeste, usuarioID, "")
	if err != nil {
		t.Fatalf("ListarPedidosProprios: %v", err)
	}
	achou := false
	for _, r := range resumos {
		if r.ID == pedido.ID {
			achou = true
			if r.ObraCentroCusto != "" {
				t.Errorf("ListarPedidosProprios: ObraCentroCusto = %q, want \"\"", r.ObraCentroCusto)
			}
		}
	}
	if !achou {
		t.Fatal("Pedido semeado não apareceu em ListarPedidosProprios")
	}

	detalhe, err := BuscarPedidoProprio(db, empresaTeste, pedido.ID, usuarioID, PapelUsuario)
	if err != nil {
		t.Fatalf("BuscarPedidoProprio: %v", err)
	}
	if detalhe.ObraCentroCusto != "" {
		t.Errorf("BuscarPedidoProprio: ObraCentroCusto = %q, want \"\"", detalhe.ObraCentroCusto)
	}

	fila, err := ListarPedidosFila(db, empresaTeste, "")
	if err != nil {
		t.Fatalf("ListarPedidosFila: %v", err)
	}
	achouNaFila := false
	for _, r := range fila {
		if r.ID == pedido.ID {
			achouNaFila = true
			if r.ObraCentroCusto != "" {
				t.Errorf("ListarPedidosFila: ObraCentroCusto = %q, want \"\"", r.ObraCentroCusto)
			}
		}
	}
	if !achouNaFila {
		t.Fatal("Pedido semeado não apareceu em ListarPedidosFila")
	}
}

// TestSubmeterPedidoTransferencia_DisponibilidadeInsuficiente prova a linha
// "Disponível insuficiente" da I/O Matrix: pedir mais que o disponível na
// origem -> nada gravado, ErroPedidoIndisponivel (409).
func TestSubmeterPedidoTransferencia_DisponibilidadeInsuficiente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Transf Insuf", "transf-insuf@empresa.com", PapelUsuario, 0)
	produtoID, estoqueOrigemID, _ := seedProdutoComSaldo(t, db, "Transf Insuf", 10)
	estoqueDestinoID := criarEstoqueDestinoTeste(t, db, "Transf Insuf Destino")

	antes := contarPedidos(t, db)
	_, err := SubmeterPedidoTransferencia(
		db, empresaTeste, usuarioID, "Fulano", "Obra X", "",
		produtoID, estoqueOrigemID, estoqueDestinoID, 11,
	)
	var erroIndisponivel *ErroPedidoIndisponivel
	if !errors.As(err, &erroIndisponivel) {
		t.Fatalf("erro = %v, want *ErroPedidoIndisponivel", err)
	}
	if depois := contarPedidos(t, db); depois != antes {
		t.Errorf("pedidos = %d, want %d (nada gravado)", depois, antes)
	}
	if q := saldoProdutoEstoque(t, db, produtoID, estoqueOrigemID); q != 10 {
		t.Errorf("saldo da origem não deveria mudar, got %v, want 10", q)
	}
}

// TestSubmeterPedidoTransferencia_OrigemIgualDestino prova a linha "Origem
// == destino" da I/O Matrix: mesmo Estoque nos dois campos ->
// ErroPedidoValidacao (400), nada gravado.
func TestSubmeterPedidoTransferencia_OrigemIgualDestino(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Transf OrigDest", "transf-origdest@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Transf OrigDest", 10)

	antes := contarPedidos(t, db)
	_, err := SubmeterPedidoTransferencia(
		db, empresaTeste, usuarioID, "Fulano", "Obra X", "",
		produtoID, estoqueID, estoqueID, 1,
	)
	var erroValidacao *ErroPedidoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroPedidoValidacao", err)
	}
	if depois := contarPedidos(t, db); depois != antes {
		t.Errorf("pedidos = %d, want %d (nada gravado)", depois, antes)
	}
}

// TestSubmeterPedidoTransferencia_DestinoInexistenteOuOutraEmpresa prova a
// linha "Destino inexistente/outra Empresa" da I/O Matrix:
// &ErroPedidoValidacao (400) — nunca ErroPedidoIndisponivel (409).
func TestSubmeterPedidoTransferencia_DestinoInexistenteOuOutraEmpresa(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Transf DestRuim", "transf-destruim@empresa.com", PapelUsuario, 0)
	produtoID, estoqueOrigemID, _ := seedProdutoComSaldo(t, db, "Transf DestRuim", 10)

	outraEmpresa := criarEmpresaDeTeste(t, db, "outra-empresa-transf-dest", "777888990002", "Outra Empresa Transf Dest")
	t.Cleanup(func() { removerEmpresaDeTeste(t, db, "outra-empresa-transf-dest") })
	estoqueAlheio, err := CriarEstoque(db, outraEmpresa.ID, filialTeste(t, db, outraEmpresa.ID), "Estoque Alheio Transf Dest")
	if err != nil {
		t.Fatalf("seed CriarEstoque (outra empresa): %v", err)
	}
	// removerEmpresaDeTeste (acima) não apaga `estoques` — sem isto, a FK de
	// `estoques.filial_id` bloquearia o DELETE de `filiais` dentro dele.
	// t.Cleanup roda em LIFO: este, registrado DEPOIS, executa ANTES.
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM estoques WHERE id = $1`, estoqueAlheio.ID); err != nil {
			t.Fatalf("limpar estoque alheio: %v", err)
		}
	})

	casos := []struct {
		nome           string
		estoqueDestino string
	}{
		{"inexistente", "00000000-0000-0000-0000-000000000000"},
		{"outra empresa", estoqueAlheio.ID},
		{"malformado", "nao-e-uuid"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			antes := contarPedidos(t, db)
			_, err := SubmeterPedidoTransferencia(
				db, empresaTeste, usuarioID, "Fulano", "Obra X", "",
				produtoID, estoqueOrigemID, c.estoqueDestino, 1,
			)
			var erroValidacao *ErroPedidoValidacao
			if !errors.As(err, &erroValidacao) {
				t.Fatalf("erro = %v, want *ErroPedidoValidacao", err)
			}
			if depois := contarPedidos(t, db); depois != antes {
				t.Errorf("pedidos = %d, want %d (nada gravado)", depois, antes)
			}
		})
	}
}

// TestSubmeterPedidoTransferencia_CamposVaziosSaoValidacao prova o Always de
// spec-20-1: produtoId/estoqueOrigemId/estoqueDestinoId vazios (string em
// branco) são &ErroPedidoValidacao (400), NUNCA colapsam em
// ErroPedidoIndisponivel (409).
func TestSubmeterPedidoTransferencia_CamposVaziosSaoValidacao(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Transf Vazios", "transf-vazios@empresa.com", PapelUsuario, 0)
	produtoID, estoqueOrigemID, _ := seedProdutoComSaldo(t, db, "Transf Vazios", 10)
	estoqueDestinoID := criarEstoqueDestinoTeste(t, db, "Transf Vazios Destino")

	casos := []struct {
		nome                     string
		produto, origem, destino string
	}{
		{"produto vazio", "   ", estoqueOrigemID, estoqueDestinoID},
		{"origem vazia", produtoID, "  ", estoqueDestinoID},
		{"destino vazio", produtoID, estoqueOrigemID, "   "},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := SubmeterPedidoTransferencia(
				db, empresaTeste, usuarioID, "Fulano", "Obra X", "",
				c.produto, c.origem, c.destino, 1,
			)
			var erroValidacao *ErroPedidoValidacao
			if !errors.As(err, &erroValidacao) {
				t.Fatalf("erro = %v, want *ErroPedidoValidacao", err)
			}
		})
	}
}

// TestSubmeterPedidoTransferencia_QuantidadeInvalida cobre quantidade <= 0
// e NaN — mesma cobertura de validarQuantidadeMovimentacao (molde).
func TestSubmeterPedidoTransferencia_QuantidadeInvalida(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Transf QtdRuim", "transf-qtdruim@empresa.com", PapelUsuario, 0)
	produtoID, estoqueOrigemID, _ := seedProdutoComSaldo(t, db, "Transf QtdRuim", 10)
	estoqueDestinoID := criarEstoqueDestinoTeste(t, db, "Transf QtdRuim Destino")

	for _, q := range []float64{0, -5, math.NaN()} {
		_, err := SubmeterPedidoTransferencia(
			db, empresaTeste, usuarioID, "Fulano", "Obra X", "",
			produtoID, estoqueOrigemID, estoqueDestinoID, q,
		)
		var erroValidacao *ErroPedidoValidacao
		if !errors.As(err, &erroValidacao) {
			t.Errorf("quantidade=%v: erro = %v, want *ErroPedidoValidacao", q, err)
		}
	}
}

// TestSubmeterPedidoTransferencia_ProdutoInativoRecusado prova o Always de
// spec-20-1: Produto inativo é recusado ANTES de travar/reservar saldo —
// mesma mensagem/tipo de erro do Pedido de retirada (recusarProdutosInativosTx
// reaproveitada tal qual).
func TestSubmeterPedidoTransferencia_ProdutoInativoRecusado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Transf Inativo", "transf-inativo@empresa.com", PapelUsuario, 0)
	produtoID, estoqueOrigemID, _ := seedProdutoComSaldo(t, db, "Transf Inativo", 10)
	estoqueDestinoID := criarEstoqueDestinoTeste(t, db, "Transf Inativo Destino")
	if _, err := db.Exec(`UPDATE produtos SET inativado_em = now() WHERE id = $1`, produtoID); err != nil {
		t.Fatalf("seed inativação: %v", err)
	}

	antes := contarPedidos(t, db)
	_, err := SubmeterPedidoTransferencia(
		db, empresaTeste, usuarioID, "Fulano", "Obra X", "",
		produtoID, estoqueOrigemID, estoqueDestinoID, 1,
	)
	var erroInativo *ErroPedidoProdutoInativo
	if !errors.As(err, &erroInativo) {
		t.Fatalf("erro = %v, want *ErroPedidoProdutoInativo", err)
	}
	if len(erroInativo.Itens) != 1 || erroInativo.Itens[0] != "Produto Transf Inativo" {
		t.Errorf("Itens = %v, want [%q]", erroInativo.Itens, "Produto Transf Inativo")
	}
	if depois := contarPedidos(t, db); depois != antes {
		t.Errorf("pedidos = %d, want %d (nada gravado)", depois, antes)
	}
}

// TestSubmeterPedidoTransferencia_ConcorrenciaMesmoPar prova a reserva dura
// sob concorrência (AC5, mesmo molde de
// TestAdicionarItemCarrinho_ConcorrenciaMesmoUsuarioMesmoPar): disponível
// 10, duas solicitações de 6 na MESMA origem não podem as duas suceder (12 >
// 10) — a perdedora vê *ErroPedidoIndisponivel, só a vencedora grava.
func TestSubmeterPedidoTransferencia_ConcorrenciaMesmoPar(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Transf Corrida", "transf-corrida@empresa.com", PapelUsuario, 0)
	produtoID, estoqueOrigemID, _ := seedProdutoComSaldo(t, db, "Transf Corrida", 10)
	estoqueDestinoID := criarEstoqueDestinoTeste(t, db, "Transf Corrida Destino")

	start := make(chan struct{})
	var wg sync.WaitGroup
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err1 = SubmeterPedidoTransferencia(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueOrigemID, estoqueDestinoID, 6)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err2 = SubmeterPedidoTransferencia(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueOrigemID, estoqueDestinoID, 6)
	}()
	close(start)
	wg.Wait()

	sucesso1, sucesso2 := err1 == nil, err2 == nil
	if sucesso1 && sucesso2 {
		t.Fatalf("as duas solicitações de 6 (disponível 10) tiveram sucesso simultâneo: err1=%v err2=%v", err1, err2)
	}
	if !sucesso1 && !sucesso2 {
		t.Fatalf("as duas solicitações falharam — pelo menos uma deveria suceder (10 >= 6): err1=%v err2=%v", err1, err2)
	}
	erroPerdedora := err1
	if sucesso1 {
		erroPerdedora = err2
	}
	var erroIndisponivel *ErroPedidoIndisponivel
	if !errors.As(erroPerdedora, &erroIndisponivel) {
		t.Fatalf("erro da perdedora = %v, want *ErroPedidoIndisponivel", erroPerdedora)
	}

	var totalReservado float64
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(quantidade), 0) FROM reservas_pedido_item WHERE produto_id = $1 AND estoque_id = $2`,
		produtoID, estoqueOrigemID,
	).Scan(&totalReservado); err != nil {
		t.Fatalf("somar reservas: %v", err)
	}
	if totalReservado != 6 {
		t.Errorf("total reservado = %v, want 6 (só a vencedora gravou)", totalReservado)
	}
}

// TestDecidirPedido_RecusaPedidoDeTransferencia prova o guard CRÍTICO
// acrescentado na revisão de spec-20-1 (Spec Change Log): DecidirPedido
// recusa decidir (aprovar OU rejeitar) um Pedido de Transferência pendente
// com ErrPedidoTipoNaoSuportado, ANTES de qualquer trava/débito — nenhum
// saldo físico é tocado em nenhum dos dois Estoques, a reserva da origem
// continua intacta, o status continua `pendente`.
func TestDecidirPedido_RecusaPedidoDeTransferencia(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Transf Decisao", "transf-decisao@empresa.com", PapelUsuario, 0)
	// seedProdutoComSaldo já semeia uma conta almoxarife própria
	// (derivada do nome do Estoque) — reaproveitada aqui como decisor, em
	// vez de semear uma segunda conta com e-mail coincidente.
	produtoID, estoqueOrigemID, almoxID := seedProdutoComSaldo(t, db, "Transf Decisao", 10)
	estoqueDestinoID := criarEstoqueDestinoTeste(t, db, "Transf Decisao Destino")

	pedido, err := SubmeterPedidoTransferencia(
		db, empresaTeste, usuarioID, "Fulano", "Obra X", "",
		produtoID, estoqueOrigemID, estoqueDestinoID, 6,
	)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoTransferencia: %v", err)
	}

	for _, aprovar := range []bool{true, false} {
		if _, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, aprovar); !errors.Is(err, ErrPedidoTipoNaoSuportado) {
			t.Errorf("aprovar=%v: erro = %v, want ErrPedidoTipoNaoSuportado", aprovar, err)
		}
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM pedidos WHERE id = $1`, pedido.ID).Scan(&status); err != nil {
		t.Fatalf("ler status: %v", err)
	}
	if status != "pendente" {
		t.Errorf("status = %q, want pendente (nenhuma decisão real aconteceu)", status)
	}
	if n := contarReservas(t, db, pedido.ID); n != 1 {
		t.Errorf("reservas do pedido = %d, want 1 (intacta)", n)
	}
	if q := saldoProdutoEstoque(t, db, produtoID, estoqueOrigemID); q != 10 {
		t.Errorf("saldo da origem não deveria mudar, got %v, want 10", q)
	}
}

// --- SubmeterPedidoBaixa (Story 20.2, Epic 20, spec-20-2) ------------------

// pedidoItemBaixaLinha é a leitura crua de uma linha de pedido_itens de um
// Pedido de Baixa — confirma que estoque_destino_id permanece NULL (Never,
// spec-20-2).
type pedidoItemBaixaLinha struct {
	ProdutoID        string
	ProdutoNome      string
	CategoriaNome    string
	EstoqueID        string
	EstoqueNome      string
	Quantidade       float64
	EstoqueDestinoID sql.NullString
}

func listarPedidoItensBaixa(t *testing.T, db *sql.DB, pedidoID string) []pedidoItemBaixaLinha {
	t.Helper()
	rows, err := db.Query(
		`SELECT produto_id, produto_nome, categoria_nome, estoque_id, estoque_nome, quantidade, estoque_destino_id
		 FROM pedido_itens WHERE pedido_id = $1 ORDER BY produto_nome`,
		pedidoID,
	)
	if err != nil {
		t.Fatalf("falha ao listar pedido_itens (baixa): %v", err)
	}
	defer rows.Close()
	var itens []pedidoItemBaixaLinha
	for rows.Next() {
		var it pedidoItemBaixaLinha
		if err := rows.Scan(&it.ProdutoID, &it.ProdutoNome, &it.CategoriaNome, &it.EstoqueID, &it.EstoqueNome, &it.Quantidade, &it.EstoqueDestinoID); err != nil {
			t.Fatalf("falha ao ler linha de pedido_itens (baixa): %v", err)
		}
		itens = append(itens, it)
	}
	return itens
}

// TestSubmeterPedidoBaixa_Sucesso prova a linha "Solicitação válida" da I/O
// Matrix de spec-20-2: Pedido `pendente` tipo=baixa, reserva de 5, saldo
// físico inalterado, SEM estoque_destino_id.
func TestSubmeterPedidoBaixa_Sucesso(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Baixa Sucesso", "baixa-sucesso@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Baixa Sucesso", 10)

	pedido, err := SubmeterPedidoBaixa(
		db, empresaTeste, usuarioID, "  Fulano de Tal  ", "  Obra Norte  ", "  urgente  ",
		produtoID, estoqueID, 5,
	)
	if err != nil {
		t.Fatalf("SubmeterPedidoBaixa erro inesperado: %v", err)
	}
	if pedido.ID == "" {
		t.Error("ID vazio no retorno")
	}
	if pedido.Status != "pendente" {
		t.Errorf("Status = %q, want pendente", pedido.Status)
	}
	if pedido.Tipo != "baixa" {
		t.Errorf("Tipo = %q, want baixa", pedido.Tipo)
	}
	if pedido.Solicitante != "Fulano de Tal" {
		t.Errorf("Solicitante = %q, want trimado", pedido.Solicitante)
	}
	if pedido.Observacao == nil || *pedido.Observacao != "urgente" {
		t.Errorf("Observacao = %v, want \"urgente\"", pedido.Observacao)
	}

	itens := listarPedidoItensBaixa(t, db, pedido.ID)
	if len(itens) != 1 {
		t.Fatalf("len(itens) = %d, want 1", len(itens))
	}
	it := itens[0]
	if it.ProdutoID != produtoID || it.EstoqueID != estoqueID || it.Quantidade != 5 {
		t.Errorf("item = %+v, want produto=%s estoque=%s quantidade=5", it, produtoID, estoqueID)
	}
	if it.EstoqueDestinoID.Valid {
		t.Errorf("EstoqueDestinoID = %+v, want NULL (Pedido de Baixa nunca tem destino)", it.EstoqueDestinoID)
	}
	if it.ProdutoNome == "" || it.CategoriaNome == "" || it.EstoqueNome == "" {
		t.Errorf("snapshot incompleto: %+v", it)
	}

	if n := contarReservas(t, db, pedido.ID); n != 1 {
		t.Errorf("reservas_pedido_item do pedido = %d, want 1", n)
	}

	// Never (spec-20-2): nunca debita saldo físico na solicitação.
	if q := saldoProdutoEstoque(t, db, produtoID, estoqueID); q != 10 {
		t.Errorf("saldo não deveria mudar, got %v, want 10", q)
	}
}

// TestSubmeterPedidoBaixa_ObraCentroCustoOpcional prova o requisito do
// usuário (pós-revisão): "Obra/Centro de Custo" deixa de ser obrigatório no
// Pedido de Baixa — vazio/só espaços NÃO é &ErroPedidoValidacao, grava NULL
// na coluna (migrations/000057) e `pedido.ObraCentroCusto` volta "" (nunca
// quebra o Scan).
func TestSubmeterPedidoBaixa_ObraCentroCustoOpcional(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Baixa SemObra", "baixa-semobra@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Baixa SemObra", 10)

	pedido, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "   ", "", produtoID, estoqueID, 5)
	if err != nil {
		t.Fatalf("SubmeterPedidoBaixa erro inesperado: %v", err)
	}
	if pedido.ObraCentroCusto != "" {
		t.Errorf("ObraCentroCusto = %q, want \"\"", pedido.ObraCentroCusto)
	}

	var obraNoBanco sql.NullString
	if err := db.QueryRow(`SELECT obra_centro_custo FROM pedidos WHERE id = $1`, pedido.ID).Scan(&obraNoBanco); err != nil {
		t.Fatalf("ler obra_centro_custo: %v", err)
	}
	if obraNoBanco.Valid {
		t.Errorf("obra_centro_custo no banco = %+v, want NULL", obraNoBanco)
	}

	// GET /api/pedidos (ListarPedidosProprios) e GET /api/pedidos/{id}
	// (BuscarPedidoProprio) nunca quebram ao reler esta linha com
	// obra_centro_custo NULL (COALESCE nas duas queries).
	resumos, err := ListarPedidosProprios(db, empresaTeste, usuarioID, "")
	if err != nil {
		t.Fatalf("ListarPedidosProprios: %v", err)
	}
	achou := false
	for _, r := range resumos {
		if r.ID == pedido.ID {
			achou = true
			if r.ObraCentroCusto != "" {
				t.Errorf("ListarPedidosProprios: ObraCentroCusto = %q, want \"\"", r.ObraCentroCusto)
			}
		}
	}
	if !achou {
		t.Fatal("Pedido semeado não apareceu em ListarPedidosProprios")
	}

	detalhe, err := BuscarPedidoProprio(db, empresaTeste, pedido.ID, usuarioID, PapelUsuario)
	if err != nil {
		t.Fatalf("BuscarPedidoProprio: %v", err)
	}
	if detalhe.ObraCentroCusto != "" {
		t.Errorf("BuscarPedidoProprio: ObraCentroCusto = %q, want \"\"", detalhe.ObraCentroCusto)
	}

	// ListarPedidosFila (GET /api/pedidos?escopo=todos, Story 7.4) — revisão
	// pós-Story 20.2: mesmo COALESCE(obra_centro_custo, '') de
	// ListarPedidosProprios/BuscarPedidoProprio acima, mas ainda sem teste
	// fixando que a Fila não quebra ao reler esta linha com NULL.
	fila, err := ListarPedidosFila(db, empresaTeste, "")
	if err != nil {
		t.Fatalf("ListarPedidosFila: %v", err)
	}
	achouNaFila := false
	for _, r := range fila {
		if r.ID == pedido.ID {
			achouNaFila = true
			if r.ObraCentroCusto != "" {
				t.Errorf("ListarPedidosFila: ObraCentroCusto = %q, want \"\"", r.ObraCentroCusto)
			}
		}
	}
	if !achouNaFila {
		t.Fatal("Pedido semeado não apareceu em ListarPedidosFila")
	}
}

// TestSubmeterPedidoBaixa_DisponibilidadeInsuficiente prova a linha
// "Disponível insuficiente" da I/O Matrix: pedir mais que o disponível ->
// nada gravado, ErroPedidoIndisponivel (409).
func TestSubmeterPedidoBaixa_DisponibilidadeInsuficiente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Baixa Insuf", "baixa-insuf@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Baixa Insuf", 10)

	antes := contarPedidos(t, db)
	_, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 11)
	var erroIndisponivel *ErroPedidoIndisponivel
	if !errors.As(err, &erroIndisponivel) {
		t.Fatalf("erro = %v, want *ErroPedidoIndisponivel", err)
	}
	if depois := contarPedidos(t, db); depois != antes {
		t.Errorf("pedidos = %d, want %d (nada gravado)", depois, antes)
	}
	if q := saldoProdutoEstoque(t, db, produtoID, estoqueID); q != 10 {
		t.Errorf("saldo não deveria mudar, got %v, want 10", q)
	}
}

// TestSubmeterPedidoBaixa_ProdutoInativoRecusado prova o Always de spec-20-2:
// Produto inativo é recusado ANTES de travar/reservar saldo — mesma
// mensagem/tipo de erro do Pedido de retirada/Transferência.
func TestSubmeterPedidoBaixa_ProdutoInativoRecusado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Baixa Inativo", "baixa-inativo@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Baixa Inativo", 10)
	if _, err := db.Exec(`UPDATE produtos SET inativado_em = now() WHERE id = $1`, produtoID); err != nil {
		t.Fatalf("seed inativação: %v", err)
	}

	antes := contarPedidos(t, db)
	_, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 1)
	var erroInativo *ErroPedidoProdutoInativo
	if !errors.As(err, &erroInativo) {
		t.Fatalf("erro = %v, want *ErroPedidoProdutoInativo", err)
	}
	if len(erroInativo.Itens) != 1 || erroInativo.Itens[0] != "Produto Baixa Inativo" {
		t.Errorf("Itens = %v, want [%q]", erroInativo.Itens, "Produto Baixa Inativo")
	}
	if depois := contarPedidos(t, db); depois != antes {
		t.Errorf("pedidos = %d, want %d (nada gravado)", depois, antes)
	}
}

// TestSubmeterPedidoBaixa_CamposVaziosSaoValidacao prova o Always de
// spec-20-2: produtoId/estoqueId vazios (string em branco) são
// &ErroPedidoValidacao (400), NUNCA colapsam em ErroPedidoIndisponivel (409).
func TestSubmeterPedidoBaixa_CamposVaziosSaoValidacao(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Baixa Vazios", "baixa-vazios@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Baixa Vazios", 10)

	casos := []struct {
		nome             string
		produto, estoque string
	}{
		{"produto vazio", "   ", estoqueID},
		{"estoque vazio", produtoID, "  "},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", c.produto, c.estoque, 1)
			var erroValidacao *ErroPedidoValidacao
			if !errors.As(err, &erroValidacao) {
				t.Fatalf("erro = %v, want *ErroPedidoValidacao", err)
			}
		})
	}
}

// TestSubmeterPedidoBaixa_ProdutoOuEstoqueDeOutraEmpresa prova, especificamente
// pro fluxo de Baixa (revisão pós-Story 20.2 — os demais casos de validação
// já tinham teste dedicado, este não), que um Produto OU um Estoque de OUTRA
// Empresa colapsam em &ErroPedidoValidacao (400) — nunca revelam a existência
// do recurso alheio, nunca um 409. Mesmo molde de
// TestSubmeterPedidoTransferencia_DestinoInexistenteOuOutraEmpresa.
func TestSubmeterPedidoBaixa_ProdutoOuEstoqueDeOutraEmpresa(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Baixa OutraEmpresa", "baixa-outraempresa@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Baixa OutraEmpresa", 10)

	outraEmpresa := criarEmpresaDeTeste(t, db, "outra-empresa-baixa", "777888990003", "Outra Empresa Baixa")
	t.Cleanup(func() { removerEmpresaDeTeste(t, db, "outra-empresa-baixa") })

	categoriaAlheia := categoriaIDPorCodigoEmpresa(t, db, outraEmpresa.ID, "04.001")
	estoqueAlheio, err := CriarEstoque(db, outraEmpresa.ID, filialTeste(t, db, outraEmpresa.ID), "Estoque Alheio Baixa")
	if err != nil {
		t.Fatalf("seed CriarEstoque (outra empresa): %v", err)
	}
	// removerEmpresaDeTeste (acima) não apaga `estoques`/`produtos` — sem
	// isto, as FKs bloqueariam o DELETE de `filiais`/`categorias` dentro
	// dele. t.Cleanup roda em LIFO: estes, registrados DEPOIS, executam
	// ANTES (mesmo padrão do molde de Transferência).
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM estoques WHERE id = $1`, estoqueAlheio.ID); err != nil {
			t.Fatalf("limpar estoque alheio: %v", err)
		}
	})
	produtoAlheio, err := criarProdutoComSaldo(db, outraEmpresa.ID, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Produto Alheio Baixa",
		CategoriaID:   categoriaAlheia,
		TemplateID:    templateGenericoID(t, db, outraEmpresa.ID),
	}, estoqueAlheio.ID, 10)
	if err != nil {
		t.Fatalf("seed criarProdutoComSaldo (outra empresa): %v", err)
	}
	t.Cleanup(func() {
		// produto_estoque referencia produto_id (FK) — sem apagar essa linha
		// primeiro, o DELETE de produtos abaixo violaria
		// produto_estoque_produto_id_fkey (estoque_id não tem FK própria,
		// mesmo comentário de pedido_itens: Estoques são hard-deletados).
		if _, err := db.Exec(`DELETE FROM produto_estoque WHERE produto_id = $1`, produtoAlheio.ID); err != nil {
			t.Fatalf("limpar produto_estoque alheio: %v", err)
		}
		if _, err := db.Exec(`DELETE FROM produtos WHERE id = $1`, produtoAlheio.ID); err != nil {
			t.Fatalf("limpar produto alheio: %v", err)
		}
	})

	t.Run("produto de outra empresa", func(t *testing.T) {
		antes := contarPedidos(t, db)
		_, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoAlheio.ID, estoqueID, 1)
		var erroValidacao *ErroPedidoValidacao
		if !errors.As(err, &erroValidacao) {
			t.Fatalf("erro = %v, want *ErroPedidoValidacao", err)
		}
		if depois := contarPedidos(t, db); depois != antes {
			t.Errorf("pedidos = %d, want %d (nada gravado)", depois, antes)
		}
	})

	t.Run("estoque de outra empresa", func(t *testing.T) {
		antes := contarPedidos(t, db)
		_, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueAlheio.ID, 1)
		var erroValidacao *ErroPedidoValidacao
		if !errors.As(err, &erroValidacao) {
			t.Fatalf("erro = %v, want *ErroPedidoValidacao", err)
		}
		if depois := contarPedidos(t, db); depois != antes {
			t.Errorf("pedidos = %d, want %d (nada gravado)", depois, antes)
		}
	})
}

// TestSubmeterPedidoBaixa_QuantidadeInvalida cobre quantidade <= 0 e NaN —
// mesma cobertura de validarQuantidadePedidoTransferencia (reaproveitada).
func TestSubmeterPedidoBaixa_QuantidadeInvalida(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Baixa QtdRuim", "baixa-qtdruim@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Baixa QtdRuim", 10)

	for _, q := range []float64{0, -5, math.NaN()} {
		_, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, q)
		var erroValidacao *ErroPedidoValidacao
		if !errors.As(err, &erroValidacao) {
			t.Errorf("quantidade=%v: erro = %v, want *ErroPedidoValidacao", q, err)
		}
	}
}

// TestSubmeterPedidoBaixa_ConcorrenciaMesmoPar prova a reserva dura sob
// concorrência (mesmo molde de
// TestSubmeterPedidoTransferencia_ConcorrenciaMesmoPar): disponível 10, duas
// solicitações de 6 no MESMO par não podem as duas suceder (12 > 10) — a
// perdedora vê *ErroPedidoIndisponivel, só a vencedora grava.
func TestSubmeterPedidoBaixa_ConcorrenciaMesmoPar(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Baixa Corrida", "baixa-corrida@empresa.com", PapelUsuario, 0)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Baixa Corrida", 10)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err1 = SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 6)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err2 = SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 6)
	}()
	close(start)
	wg.Wait()

	sucesso1, sucesso2 := err1 == nil, err2 == nil
	if sucesso1 && sucesso2 {
		t.Fatalf("as duas solicitações de 6 (disponível 10) tiveram sucesso simultâneo: err1=%v err2=%v", err1, err2)
	}
	if !sucesso1 && !sucesso2 {
		t.Fatalf("as duas solicitações falharam — pelo menos uma deveria suceder (10 >= 6): err1=%v err2=%v", err1, err2)
	}
	erroPerdedora := err1
	if sucesso1 {
		erroPerdedora = err2
	}
	var erroIndisponivel *ErroPedidoIndisponivel
	if !errors.As(erroPerdedora, &erroIndisponivel) {
		t.Fatalf("erro da perdedora = %v, want *ErroPedidoIndisponivel", erroPerdedora)
	}

	var totalReservado float64
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(quantidade), 0) FROM reservas_pedido_item WHERE produto_id = $1 AND estoque_id = $2`,
		produtoID, estoqueID,
	).Scan(&totalReservado); err != nil {
		t.Fatalf("somar reservas: %v", err)
	}
	if totalReservado != 6 {
		t.Errorf("total reservado = %v, want 6 (só a vencedora gravou)", totalReservado)
	}
}

// TestDecidirPedido_RecusaPedidoDeBaixa prova que o guard genérico de
// DecidirPedido (Story 20.1, Spec Change Log: `tipo != 'retirada'`) já cobre
// `'baixa'` sem nenhuma mudança adicional — nenhum saldo físico é tocado, a
// reserva continua intacta, o status continua `pendente`.
func TestDecidirPedido_RecusaPedidoDeBaixa(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Baixa Decisao", "baixa-decisao@empresa.com", PapelUsuario, 0)
	// seedProdutoComSaldo já semeia uma conta almoxarife própria (derivada do
	// nome do Estoque) — reaproveitada aqui como decisor.
	produtoID, estoqueID, almoxID := seedProdutoComSaldo(t, db, "Baixa Decisao", 10)

	pedido, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 6)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}

	for _, aprovar := range []bool{true, false} {
		if _, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, aprovar); !errors.Is(err, ErrPedidoTipoNaoSuportado) {
			t.Errorf("aprovar=%v: erro = %v, want ErrPedidoTipoNaoSuportado", aprovar, err)
		}
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM pedidos WHERE id = $1`, pedido.ID).Scan(&status); err != nil {
		t.Fatalf("ler status: %v", err)
	}
	if status != "pendente" {
		t.Errorf("status = %q, want pendente (nenhuma decisão real aconteceu)", status)
	}
	if n := contarReservas(t, db, pedido.ID); n != 1 {
		t.Errorf("reservas do pedido = %d, want 1 (intacta)", n)
	}
	if q := saldoProdutoEstoque(t, db, produtoID, estoqueID); q != 10 {
		t.Errorf("saldo não deveria mudar, got %v, want 10", q)
	}
}

// --- DecidirLotePedidos (Story 20.3, Epic 20, spec-20-3) ------------------

// TestDecidirLotePedidos_ListaVazia cobre a linha "Lote vazio" da I/O Matrix:
// `pedidoIds: []` -> &ErroPedidoValidacao (400), nada consultado.
func TestDecidirLotePedidos_ListaVazia(t *testing.T) {
	db := testDB(t)
	gestorID := semearConta(t, db, "Lote Vazio Gestor", "lote-vazio-gestor@empresa.com", PapelGestor, 0)

	_, err := DecidirLotePedidos(db, empresaTeste, gestorID, nil, true)
	var erroValidacao *ErroPedidoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroPedidoValidacao", err)
	}
}

// TestDecidirLotePedidos_IdInexistenteOuDeOutraEmpresa cobre o achado de
// revisão (findings do triplo-review pós-story): a pré-checagem de tipo
// (ANTES de tx.Begin()) não tinha nenhuma cobertura para um id inexistente
// ou de outra Empresa dentro de `pedidoIds` — mesmo colapso de
// ErrPedidoNaoEncontrado já usado por DecidirPedido/BuscarPedidoProprio
// (AD-20, nunca revela existência de Pedido alheio).
func TestDecidirLotePedidos_IdInexistenteOuDeOutraEmpresa(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Lote IdRuim Gestor", "lote-idruim-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "Lote IdRuim Usuario", "lote-idruim-usuario@empresa.com", PapelUsuario, 1)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Lote IdRuim Baixa", 10)
	pedidoValido, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 3)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}

	// Achado de revisão (Story 20.4): este teste criava a Empresa extra e
	// nunca a removia — toda segunda execução contra o mesmo banco de teste
	// quebrava com slug duplicado. Remove ANTES (recupera uma execução
	// anterior interrompida) e DEPOIS via t.Cleanup.
	removerEmpresaDeTeste(t, db, "outra-empresa-lote-idruim")
	outraEmpresa := criarEmpresaDeTeste(t, db, "outra-empresa-lote-idruim", "777888990099", "Outra Empresa Lote IdRuim")
	t.Cleanup(func() { removerEmpresaDeTeste(t, db, "outra-empresa-lote-idruim") })
	// LIFO: roda ANTES do removerEmpresaDeTeste acima — pedido e conta são
	// dados de negócio que o helper genérico não apaga (mesmo padrão de
	// TestSubmeterPedidoBaixa_ProdutoOuEstoqueDeOutraEmpresa).
	t.Cleanup(func() {
		for _, stmt := range []string{
			`DELETE FROM pedidos WHERE empresa_id = $1`,
			`DELETE FROM usuarios WHERE empresa_id = $1`,
		} {
			if _, err := db.Exec(stmt, outraEmpresa.ID); err != nil {
				t.Fatalf("limpeza da outra empresa [%s]: %v", stmt, err)
			}
		}
	})
	usuarioOutra := semearContaNaEmpresa(t, db, outraEmpresa.ID, "IdRuim Outra Empresa", "idruim-outra@outra.com", PapelUsuario)
	pedidoAlheio := inserirPedidoDireto(t, db, outraEmpresa.ID, usuarioOutra, "pendente")

	casos := map[string][]string{
		"id inexistente (UUID válido, nenhum Pedido)": {pedidoValido.ID, "00000000-0000-0000-0000-000000000000"},
		"id malformado (não-UUID)":                     {pedidoValido.ID, "nao-e-um-uuid"},
		"id de outra Empresa":                          {pedidoValido.ID, pedidoAlheio},
	}
	for nome, ids := range casos {
		t.Run(nome, func(t *testing.T) {
			_, err := DecidirLotePedidos(db, empresaTeste, gestorID, ids, true)
			if !errors.Is(err, ErrPedidoNaoEncontrado) {
				t.Fatalf("erro = %v, want ErrPedidoNaoEncontrado", err)
			}
		})
	}

	// Nenhum dos casos acima gravou nada, nem no Pedido válido da MESMA
	// lista (a pré-checagem de tipo roda ANTES de qualquer tx.Begin()).
	var status string
	if err := db.QueryRow(`SELECT status FROM pedidos WHERE id = $1`, pedidoValido.ID).Scan(&status); err != nil {
		t.Fatalf("reler status: %v", err)
	}
	if status != "pendente" {
		t.Errorf("status do pedido válido = %q, want pendente (nada gravado)", status)
	}
}

// TestDecidirLotePedidos_RetiradaMisturada_RejeitaListaInteira cobre a linha
// "Lote com retirada misturado" da I/O Matrix: um dos ids é `tipo=retirada`
// -> o LOTE INTEIRO é rejeitado com &ErroPedidoValidacao (400), nada
// gravado — nem o Pedido de Baixa que seria válido sozinho. Também prova o
// Never de spec-20-3: DecidirPedido (retirada) continua INTOCADO por esta
// story — o mesmo Pedido de retirada decide normalmente depois, por
// DecidirPedido, fora do lote.
func TestDecidirLotePedidos_RetiradaMisturada_RejeitaListaInteira(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Lote Misto Retirada Gestor", "lote-misto-retirada-gestor@empresa.com", PapelGestor, 0)

	// Pedido de retirada pendente (molde de seedPedidoViaServico, mas
	// direto pela camada de serviço sem a dança HTTP).
	usuarioRetirada := semearConta(t, db, "Lote Misto Retirada Usuario", "lote-misto-retirada-usuario@empresa.com", PapelUsuario, 1)
	// seedProdutoComSaldo já semeia uma conta almoxarife própria (derivada
	// do nome do Estoque) — reaproveitada abaixo como decisor de
	// DecidirPedido, em vez de semear uma segunda conta com e-mail
	// coincidente (mesmo padrão de TestDecidirPedido_RecusaPedidoDeTransferencia).
	produtoRetiradaID, estoqueRetiradaID, almoxID := seedProdutoComSaldo(t, db, "Lote Misto Retirada", 10)
	if _, err := AdicionarItemCarrinho(db, empresaTeste, usuarioRetirada, produtoRetiradaID, estoqueRetiradaID, 2); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho: %v", err)
	}
	pedidoRetirada, err := SubmeterPedido(db, empresaTeste, usuarioRetirada, "Fulano", "Obra X", "")
	if err != nil {
		t.Fatalf("seed SubmeterPedido: %v", err)
	}

	// Pedido de Baixa pendente, que seria válido sozinho.
	produtoBaixaID, estoqueBaixaID, _ := seedProdutoComSaldo(t, db, "Lote Misto Baixa", 10)
	pedidoBaixa, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioRetirada, "Fulano", "Obra X", "", produtoBaixaID, estoqueBaixaID, 3)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}

	_, err = DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedidoBaixa.ID, pedidoRetirada.ID}, true)
	var erroValidacao *ErroPedidoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroPedidoValidacao", err)
	}

	for _, id := range []string{pedidoBaixa.ID, pedidoRetirada.ID} {
		var status string
		if err := db.QueryRow(`SELECT status FROM pedidos WHERE id = $1`, id).Scan(&status); err != nil {
			t.Fatalf("ler status de %s: %v", id, err)
		}
		if status != "pendente" {
			t.Errorf("pedido %s: status = %q, want pendente (lote inteiro rejeitado, nada gravado)", id, status)
		}
	}
	if q := saldoProdutoEstoque(t, db, produtoBaixaID, estoqueBaixaID); q != 10 {
		t.Errorf("saldo da Baixa não deveria mudar, got %v, want 10", q)
	}
	if q := saldoProdutoEstoque(t, db, produtoRetiradaID, estoqueRetiradaID); q != 10 {
		t.Errorf("saldo da retirada não deveria mudar, got %v, want 10", q)
	}

	// Never de spec-20-3: DecidirPedido (retirada) continua intocado — o
	// Pedido de retirada decide normalmente fora do lote.
	det, err := DecidirPedido(db, empresaTeste, pedidoRetirada.ID, almoxID, PapelAlmoxarife, true)
	if err != nil {
		t.Fatalf("DecidirPedido (fora do lote) erro inesperado: %v", err)
	}
	if det.Status != "aprovado" {
		t.Errorf("status do pedido de retirada = %q, want aprovado", det.Status)
	}
}

// TestDecidirLotePedidos_MistoTransferenciaEBaixa_Aprovado cobre a linha
// "Lote válido misto" da I/O Matrix: 1 Transferência + 1 Baixa, saldo
// suficiente -> ambos `aprovado`, cada um executando a MESMA mecânica física
// do botão direto correspondente (FEFO + crédito no destino preservando
// validade para Transferência; só FEFO para Baixa) — nenhum debita/credita
// duas vezes.
func TestDecidirLotePedidos_MistoTransferenciaEBaixa_Aprovado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Lote Misto OK Gestor", "lote-misto-ok-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "Lote Misto OK Usuario", "lote-misto-ok-usuario@empresa.com", PapelUsuario, 1)

	produtoTransfID, estoqueOrigemID, _ := seedProdutoComSaldo(t, db, "Lote Misto OK Transf", 10)
	estoqueDestinoID := criarEstoqueDestinoTeste(t, db, "Lote Misto OK Transf Destino")
	pedidoTransf, err := SubmeterPedidoTransferencia(
		db, empresaTeste, usuarioID, "Fulano", "Obra X", "",
		produtoTransfID, estoqueOrigemID, estoqueDestinoID, 6,
	)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoTransferencia: %v", err)
	}

	produtoBaixaID, estoqueBaixaID, _ := seedProdutoComSaldo(t, db, "Lote Misto OK Baixa", 10)
	pedidoBaixa, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra Y", "", produtoBaixaID, estoqueBaixaID, 4)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}

	resultado, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedidoTransf.ID, pedidoBaixa.ID}, true)
	if err != nil {
		t.Fatalf("DecidirLotePedidos erro inesperado: %v", err)
	}
	if len(resultado) != 2 {
		t.Fatalf("len(resultado) = %d, want 2", len(resultado))
	}
	// Devolvido na mesma ordem de entrada.
	if resultado[0].ID != pedidoTransf.ID || resultado[1].ID != pedidoBaixa.ID {
		t.Fatalf("ordem do resultado não preservou a ordem de entrada: %+v", resultado)
	}
	for _, det := range resultado {
		if det.Status != "aprovado" {
			t.Errorf("pedido %s: status = %q, want aprovado", det.ID, det.Status)
		}
		if len(det.Itens) != 1 || det.Itens[0].QuantidadeAprovada == nil {
			t.Fatalf("pedido %s: itens = %+v, want 1 item decidido", det.ID, det.Itens)
		}
	}

	// Transferência: origem debitada, destino creditado, exatamente 1
	// Movimentação tipo=transferencia.
	if q := saldoProdutoEstoque(t, db, produtoTransfID, estoqueOrigemID); q != 4 {
		t.Errorf("saldo da origem = %v, want 4 (10 - 6)", q)
	}
	var qtdDestino float64
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantidade), 0) FROM lotes WHERE produto_id = $1 AND estoque_id = $2`, produtoTransfID, estoqueDestinoID).Scan(&qtdDestino); err != nil {
		t.Fatalf("ler lotes do destino: %v", err)
	}
	if qtdDestino != 6 {
		t.Errorf("saldo do destino = %v, want 6", qtdDestino)
	}
	movsTransf := movsDoProduto(t, db, produtoTransfID)
	if len(movsTransf) != 1 || movsTransf[0].Tipo != "transferencia" || movsTransf[0].Quantidade != 6 || !movsTransf[0].Destino.Valid {
		t.Errorf("movimentações da transferência = %+v, want 1 transferencia(6) com destino", movsTransf)
	}

	// Baixa: só debita, exatamente 1 Movimentação tipo=baixa, sem destino.
	if q := saldoProdutoEstoque(t, db, produtoBaixaID, estoqueBaixaID); q != 6 {
		t.Errorf("saldo da baixa = %v, want 6 (10 - 4)", q)
	}
	movsBaixa := movsDoProduto(t, db, produtoBaixaID)
	if len(movsBaixa) != 1 || movsBaixa[0].Tipo != "baixa" || movsBaixa[0].Quantidade != 4 || movsBaixa[0].Destino.Valid {
		t.Errorf("movimentações da baixa = %+v, want 1 baixa(4) sem destino", movsBaixa)
	}

	// Reservas liberadas dos dois.
	if n := contarReservas(t, db, pedidoTransf.ID); n != 0 {
		t.Errorf("reservas da transferência = %d, want 0", n)
	}
	if n := contarReservas(t, db, pedidoBaixa.ID); n != 0 {
		t.Errorf("reservas da baixa = %d, want 0", n)
	}
}

// TestDecidirLotePedidos_ItemParcial_RestoDoLoteSeguiNormal cobre a linha
// "Saldo caiu entre envio e aprovação" da I/O Matrix: o saldo físico de 1
// pedido do lote cai abaixo do reservado -> esse pedido (e só esse) sai
// `parcialmente_aprovado`; o resto do lote segue `aprovado` normalmente.
func TestDecidirLotePedidos_ItemParcial_RestoDoLoteSeguiNormal(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Lote Parcial Gestor", "lote-parcial-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "Lote Parcial Usuario", "lote-parcial-usuario@empresa.com", PapelUsuario, 1)

	produtoA, estoqueA, _ := seedProdutoComSaldo(t, db, "Lote Parcial A", 10)
	pedidoA, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoA, estoqueA, 8)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa A: %v", err)
	}
	produtoB, estoqueB, _ := seedProdutoComSaldo(t, db, "Lote Parcial B", 10)
	pedidoB, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra Y", "", produtoB, estoqueB, 5)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa B: %v", err)
	}

	// Saldo físico de A cai para 3 (abaixo dos 8 reservados) ANTES da
	// decisão — mesmo molde de TestDecidirPedidoHandler_200AprovacaoParcial.
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 3 WHERE produto_id = $1 AND estoque_id = $2`, produtoA, estoqueA); err != nil {
		t.Fatalf("seed: reduzir saldo real de A: %v", err)
	}

	resultado, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedidoA.ID, pedidoB.ID}, true)
	if err != nil {
		t.Fatalf("DecidirLotePedidos erro inesperado: %v", err)
	}
	if len(resultado) != 2 {
		t.Fatalf("len(resultado) = %d, want 2", len(resultado))
	}
	if resultado[0].Status != "parcialmente_aprovado" || resultado[0].Itens[0].QuantidadeAprovada == nil || *resultado[0].Itens[0].QuantidadeAprovada != 3 {
		t.Errorf("pedido A = %+v, want parcialmente_aprovado com 3", resultado[0])
	}
	if resultado[1].Status != "aprovado" || resultado[1].Itens[0].QuantidadeAprovada == nil || *resultado[1].Itens[0].QuantidadeAprovada != 5 {
		t.Errorf("pedido B = %+v, want aprovado com 5", resultado[1])
	}
}

// TestDecidirLotePedidos_PedidoJaDecidido_PuladoSemAbortarLote cobre a linha
// "Pedido já decidido por outra requisição" da I/O Matrix: 1 dos ids do lote
// já não está mais `pendente` (decidido por outra requisição concorrente,
// simulado aqui por um UPDATE direto) -> é PULADO (reportado com o status
// JÁ gravado), sem abortar os demais pedidos do lote (não é erro de lote).
func TestDecidirLotePedidos_PedidoJaDecidido_PuladoSemAbortarLote(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	// Dois Gestores distintos: gestor1 decide o primeiro pedido "de
	// verdade" (fora do lote que o teste quer provar); gestor2 é quem
	// chama o lote que encontra esse mesmo pedido já decidido — nomes
	// diferentes em `decidido_por` provam que o lote NUNCA sobrescreve a
	// decisão original (Revisão de código, achado #4: o pulado precisa
	// reportar EXATAMENTE o que foi gravado da primeira vez, não um
	// placeholder).
	gestor1ID := semearConta(t, db, "Lote Pulado Gestor1", "lote-pulado-gestor1@empresa.com", PapelGestor, 0)
	gestor2ID := semearConta(t, db, "Lote Pulado Gestor2", "lote-pulado-gestor2@empresa.com", PapelGestor, 1)
	usuarioID := semearConta(t, db, "Lote Pulado Usuario", "lote-pulado-usuario@empresa.com", PapelUsuario, 2)

	// Saldo de 4 (exatamente o solicitado) -> a decisão real de gestor1
	// abaixo aprova TOTALMENTE, com um `quantidadeAprovada` previsível para
	// comparar byte a byte com o que o lote de gestor2 relê depois.
	produtoJaDecidido, estoqueJaDecidido, _ := seedProdutoComSaldo(t, db, "Lote Pulado JaDecidido", 4)
	pedidoJaDecidido, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "obs-ja-decidido", produtoJaDecidido, estoqueJaDecidido, 4)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa (já decidido): %v", err)
	}
	// Decisão REAL (não um UPDATE sintético) — simula a "outra requisição
	// concorrente" que decidiu este Pedido ANTES do lote de gestor2 abaixo
	// existir: debita saldo de verdade, grava quantidade_aprovada e
	// decidido_por/decidido_em de verdade. O resultado desta chamada é o
	// GROUND TRUTH que a leitura "pulada" do próximo lote precisa igualar.
	decididoAntes, err := DecidirLotePedidos(db, empresaTeste, gestor1ID, []string{pedidoJaDecidido.ID}, true)
	if err != nil {
		t.Fatalf("decisão real prévia de pedidoJaDecidido: %v", err)
	}
	if len(decididoAntes) != 1 || decididoAntes[0].Status != "aprovado" {
		t.Fatalf("seed: decisão prévia = %+v, want 1 item aprovado", decididoAntes)
	}
	ground := decididoAntes[0]

	produtoNormal, estoqueNormal, _ := seedProdutoComSaldo(t, db, "Lote Pulado Normal", 10)
	pedidoNormal, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra Y", "", produtoNormal, estoqueNormal, 3)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa (normal): %v", err)
	}

	resultado, err := DecidirLotePedidos(db, empresaTeste, gestor2ID, []string{pedidoJaDecidido.ID, pedidoNormal.ID}, true)
	if err != nil {
		t.Fatalf("DecidirLotePedidos erro inesperado (pedido já decidido nunca aborta o lote): %v", err)
	}
	if len(resultado) != 2 {
		t.Fatalf("len(resultado) = %d, want 2", len(resultado))
	}
	pulado := resultado[0]
	if pulado.ID != pedidoJaDecidido.ID {
		t.Fatalf("resultado[0].ID = %q, want %q (pedido já decidido deve vir na posição de entrada)", pulado.ID, pedidoJaDecidido.ID)
	}
	if pulado.Status != "aprovado" {
		t.Errorf("pedido já decidido: Status = %q, want %q (ground truth da decisão real)", pulado.Status, ground.Status)
	}
	// O "pulado" nunca é decidido por gestor2 — continua com a auditoria da
	// decisão REAL de gestor1, nunca sobrescrita pelo lote que só o
	// reportou.
	if pulado.DecididoPor == nil || *pulado.DecididoPor != gestor1ID {
		t.Errorf("pedido já decidido: DecididoPor = %v, want %q (gestor1, nunca gestor2)", pulado.DecididoPor, gestor1ID)
	}
	if ground.DecididoPor == nil || pulado.DecididoPor == nil || *pulado.DecididoPor != *ground.DecididoPor {
		t.Errorf("pedido já decidido: DecididoPor = %v, want igual ao ground truth %v", pulado.DecididoPor, ground.DecididoPor)
	}
	if ground.DecididoEm == nil || pulado.DecididoEm == nil || !pulado.DecididoEm.Equal(*ground.DecididoEm) {
		t.Errorf("pedido já decidido: DecididoEm = %v, want igual ao ground truth %v", pulado.DecididoEm, ground.DecididoEm)
	}
	if pulado.CentroCustoNome != nil || ground.CentroCustoNome != nil {
		t.Errorf("pedido já decidido: CentroCustoNome = %v (ground %v), want nil nos dois (Baixa nunca escolhe Centro de Custo cadastrado)", pulado.CentroCustoNome, ground.CentroCustoNome)
	}
	if pulado.Observacao == nil || *pulado.Observacao != "obs-ja-decidido" {
		t.Errorf("pedido já decidido: Observacao = %v, want \"obs-ja-decidido\" (snapshot do envio, preservado)", pulado.Observacao)
	}
	if len(pulado.Itens) != 1 || len(ground.Itens) != 1 {
		t.Fatalf("pedido já decidido: Itens = %+v (ground %+v), want 1 item nos dois", pulado.Itens, ground.Itens)
	}
	itemPulado, itemGround := pulado.Itens[0], ground.Itens[0]
	if itemPulado.ProdutoID != itemGround.ProdutoID || itemPulado.EstoqueID != itemGround.EstoqueID ||
		itemPulado.Quantidade != itemGround.Quantidade {
		t.Errorf("pedido já decidido: item = %+v, want igual ao ground truth %+v", itemPulado, itemGround)
	}
	if itemPulado.QuantidadeAprovada == nil || itemGround.QuantidadeAprovada == nil ||
		*itemPulado.QuantidadeAprovada != *itemGround.QuantidadeAprovada {
		t.Errorf("pedido já decidido: QuantidadeAprovada = %v, want igual ao ground truth %v", itemPulado.QuantidadeAprovada, itemGround.QuantidadeAprovada)
	}
	if itemPulado.QuantidadeAprovada == nil || *itemPulado.QuantidadeAprovada != 4 {
		t.Errorf("pedido já decidido: QuantidadeAprovada = %v, want 4 (saldo = solicitado)", itemPulado.QuantidadeAprovada)
	}

	if resultado[1].ID != pedidoNormal.ID || resultado[1].Status != "aprovado" {
		t.Errorf("pedido normal = %+v, want status=aprovado (resto do lote segue normal)", resultado[1])
	}
	if resultado[1].DecididoPor == nil || *resultado[1].DecididoPor != gestor2ID {
		t.Errorf("pedido normal: DecididoPor = %v, want %q (gestor2, decidido AGORA por este lote)", resultado[1].DecididoPor, gestor2ID)
	}

	// O pedido já decidido não foi tocado de novo por este segundo lote:
	// saldo igual ao que a decisão real de gestor1 já havia deixado (4 - 4
	// = 0), nunca debitado uma segunda vez.
	if q := saldoProdutoEstoque(t, db, produtoJaDecidido, estoqueJaDecidido); q != 0 {
		t.Errorf("saldo do pedido já decidido = %v, want 0 (debitado uma única vez, pela decisão real)", q)
	}
	if n := contarMovimentacoes(t, db, produtoJaDecidido); n != 1 {
		t.Errorf("movimentações do produto já decidido = %d, want 1 (nunca debitado 2x)", n)
	}
}

// TestDecidirLotePedidos_DecisoesConcorrentesSoAPrimeiraDebita cobre a
// cobertura de concorrência pedida por spec-20-3 (Tasks & Acceptance): 2
// decisões em lote simultâneas sobre o MESMO pedido (cada uma num lote de 1)
// nunca debitam duas vezes — exatamente uma delas decide de verdade (1
// Movimentação, saldo debitado uma vez); a outra vê o mesmo pedido já
// decidido e o reporta sem erro (nunca aborta, nunca duplica o débito).
func TestDecidirLotePedidos_DecisoesConcorrentesSoAPrimeiraDebita(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "Lote Corrida Gestor", "lote-corrida-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "Lote Corrida Usuario", "lote-corrida-usuario@empresa.com", PapelUsuario, 1)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Lote Corrida", 10)
	pedido, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 4)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	var res1, res2 []PedidoDetalhe
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		res1, err1 = DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedido.ID}, true)
	}()
	go func() {
		defer wg.Done()
		<-start
		res2, err2 = DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedido.ID}, true)
	}()
	close(start)
	wg.Wait()

	if err1 != nil {
		t.Fatalf("lote 1 erro inesperado: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("lote 2 erro inesperado: %v", err2)
	}
	if len(res1) != 1 || len(res2) != 1 {
		t.Fatalf("res1=%+v res2=%+v, want 1 item cada", res1, res2)
	}
	if res1[0].Status != "aprovado" || res2[0].Status != "aprovado" {
		t.Fatalf("res1=%+v res2=%+v, want status=aprovado nas duas respostas (a perdedora relê o mesmo resultado final)", res1, res2)
	}

	if q := saldoProdutoEstoque(t, db, produtoID, estoqueID); q != 6 {
		t.Errorf("saldo = %v, want 6 (10 - 4, debitado uma única vez)", q)
	}
	movs := movsDoProduto(t, db, produtoID)
	if len(movs) != 1 {
		t.Fatalf("movimentações = %+v, want exatamente 1 (nunca debitado duas vezes)", movs)
	}
	if n := contarReservas(t, db, pedido.ID); n != 0 {
		t.Errorf("reservas = %d, want 0", n)
	}
}

// ---------------------------------------------------------------------------
// Story 20.4 (Epic 20, Pré-Nota/docnum ao aprovar lote), spec-20-4: cobre a
// I/O Matrix da spec inteira + concorrência (2 lotes da mesma Empresa em
// paralelo -> docnum sem colisão; 2 Empresas em paralelo -> contagem
// independente).

// preNotaDoPedido lê o estado da Pré-Nota vinculada a `pedidoID` (Story
// 20.4) direto do banco — `ok=false` quando `pedidos.pre_nota_id` é NULL
// (Pedido nunca vinculado a nenhuma Pré-Nota).
func preNotaDoPedido(t *testing.T, db *sql.DB, pedidoID string) (preNotaID string, docnum int, decididoPor string, ok bool) {
	t.Helper()
	var pn sql.NullString
	if err := db.QueryRow(`SELECT pre_nota_id FROM pedidos WHERE id = $1`, pedidoID).Scan(&pn); err != nil {
		t.Fatalf("ler pre_nota_id do pedido %s: %v", pedidoID, err)
	}
	if !pn.Valid {
		return "", 0, "", false
	}
	if err := db.QueryRow(`SELECT docnum, decidido_por FROM pre_notas WHERE id = $1`, pn.String).Scan(&docnum, &decididoPor); err != nil {
		t.Fatalf("ler pré-nota %s: %v", pn.String, err)
	}
	return pn.String, docnum, decididoPor, true
}

// contadorPreNota lê `contadores_pre_nota.ultimo_numero` da Empresa — nunca
// assumido como 0, já que a tabela nunca é truncada entre testes (mesmo
// padrão de contadores_produto, produtos_test.go).
func contadorPreNota(t *testing.T, db *sql.DB, empresaID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT ultimo_numero FROM contadores_pre_nota WHERE empresa_id = $1`, empresaID).Scan(&n); err != nil {
		t.Fatalf("ler contadores_pre_nota da empresa %s: %v", empresaID, err)
	}
	return n
}

func contarPreNotas(t *testing.T, db *sql.DB, empresaID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM pre_notas WHERE empresa_id = $1`, empresaID).Scan(&n); err != nil {
		t.Fatalf("contar pre_notas da empresa %s: %v", empresaID, err)
	}
	return n
}

// TestDecidirLotePedidos_GeraPreNota_TodosDecididosAgora cobre a linha "Lote
// aprovado, todos decididos agora" da I/O Matrix: 3 Pedidos pendentes,
// aprovar=true -> 1 Pré-Nota nova só, docnum sequencial, vinculada aos 3.
func TestDecidirLotePedidos_GeraPreNota_TodosDecididosAgora(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "PreNota Todos Gestor", "prenota-todos-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "PreNota Todos Usuario", "prenota-todos-usuario@empresa.com", PapelUsuario, 1)

	var pedidoIDs []string
	for _, nome := range []string{"PreNota Todos A", "PreNota Todos B", "PreNota Todos C"} {
		produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, nome, 10)
		pedido, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 2)
		if err != nil {
			t.Fatalf("seed SubmeterPedidoBaixa (%s): %v", nome, err)
		}
		pedidoIDs = append(pedidoIDs, pedido.ID)
	}

	contadorAntes := contadorPreNota(t, db, empresaTeste)

	resultado, err := DecidirLotePedidos(db, empresaTeste, gestorID, pedidoIDs, true)
	if err != nil {
		t.Fatalf("DecidirLotePedidos erro inesperado: %v", err)
	}
	if len(resultado) != 3 {
		t.Fatalf("len(resultado) = %d, want 3", len(resultado))
	}

	preNotaIDs := make(map[string]bool, 3)
	var docnum int
	for _, det := range resultado {
		id, dn, decididoPor, ok := preNotaDoPedido(t, db, det.ID)
		if !ok {
			t.Fatalf("pedido %s sem pré-nota vinculada, want vinculado", det.ID)
		}
		preNotaIDs[id] = true
		docnum = dn
		if decididoPor != gestorID {
			t.Errorf("decidido_por da pré-nota = %q, want %q", decididoPor, gestorID)
		}
	}
	if len(preNotaIDs) != 1 {
		t.Errorf("pré-notas distintas vinculadas ao lote = %d, want 1 (uma só, pro lote inteiro)", len(preNotaIDs))
	}
	if docnum != contadorAntes+1 {
		t.Errorf("docnum = %d, want %d (contador antes + 1)", docnum, contadorAntes+1)
	}
}

// TestDecidirLotePedidos_PreNotaSoComDecididosNestaChamada cobre a linha
// "Lote aprovado, misto decidido+pulado" da I/O Matrix: A é decidido de
// verdade ANTES (ganha sua própria Pré-Nota); um segundo lote mistura A
// (agora pulado) com B (decidido agora) -> a nova Pré-Nota só cobre B; a de A
// fica intocada.
func TestDecidirLotePedidos_PreNotaSoComDecididosNestaChamada(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestor1ID := semearConta(t, db, "PreNota Misto Gestor1", "prenota-misto-gestor1@empresa.com", PapelGestor, 0)
	gestor2ID := semearConta(t, db, "PreNota Misto Gestor2", "prenota-misto-gestor2@empresa.com", PapelGestor, 1)
	usuarioID := semearConta(t, db, "PreNota Misto Usuario", "prenota-misto-usuario@empresa.com", PapelUsuario, 2)

	produtoA, estoqueA, _ := seedProdutoComSaldo(t, db, "PreNota Misto A", 10)
	pedidoA, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoA, estoqueA, 3)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa A: %v", err)
	}
	// Decisão real prévia, isolada — simula "já decidido por outra
	// transação" antes do lote misto abaixo existir; gera a PRÓPRIA Pré-Nota
	// de A.
	if _, err := DecidirLotePedidos(db, empresaTeste, gestor1ID, []string{pedidoA.ID}, true); err != nil {
		t.Fatalf("decisão prévia de A: %v", err)
	}
	preNotaAntigaID, docnumAntigo, _, ok := preNotaDoPedido(t, db, pedidoA.ID)
	if !ok {
		t.Fatalf("pedido A deveria ter pré-nota da decisão prévia")
	}

	produtoB, estoqueB, _ := seedProdutoComSaldo(t, db, "PreNota Misto B", 10)
	pedidoB, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra Y", "", produtoB, estoqueB, 4)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa B: %v", err)
	}

	resultado, err := DecidirLotePedidos(db, empresaTeste, gestor2ID, []string{pedidoA.ID, pedidoB.ID}, true)
	if err != nil {
		t.Fatalf("DecidirLotePedidos erro inesperado: %v", err)
	}
	if len(resultado) != 2 {
		t.Fatalf("len(resultado) = %d, want 2", len(resultado))
	}

	// A (pulado nesta chamada) mantém a Pré-Nota ANTIGA, intocada.
	preNotaAID, docnumA, _, ok := preNotaDoPedido(t, db, pedidoA.ID)
	if !ok || preNotaAID != preNotaAntigaID || docnumA != docnumAntigo {
		t.Errorf("pedido A: pré-nota = %q (docnum %d), want a antiga %q (docnum %d) intocada", preNotaAID, docnumA, preNotaAntigaID, docnumAntigo)
	}

	// B (decidido agora) ganha uma Pré-Nota NOVA, diferente da de A.
	preNotaBID, docnumB, decididoPorB, ok := preNotaDoPedido(t, db, pedidoB.ID)
	if !ok {
		t.Fatalf("pedido B deveria ter uma pré-nota nova")
	}
	if preNotaBID == preNotaAID {
		t.Errorf("pedido B ganhou a MESMA pré-nota do pulado A (%q), want uma nova só com os decididos nesta chamada", preNotaBID)
	}
	if docnumB <= docnumA {
		t.Errorf("docnum de B = %d, want > %d (docnum de A, já emitido antes)", docnumB, docnumA)
	}
	if decididoPorB != gestor2ID {
		t.Errorf("decidido_por da pré-nota de B = %q, want %q (gestor2)", decididoPorB, gestor2ID)
	}
}

// TestDecidirLotePedidos_LoteTotalmentePulado_NenhumaPreNotaNova cobre a
// linha "Lote 100% pulado" da I/O Matrix: o único id do lote já tinha sido
// decidido antes -> nenhuma Pré-Nota nova, `docnum` não avança.
func TestDecidirLotePedidos_LoteTotalmentePulado_NenhumaPreNotaNova(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestor1ID := semearConta(t, db, "PreNota Pulado Gestor1", "prenota-pulado-gestor1@empresa.com", PapelGestor, 0)
	gestor2ID := semearConta(t, db, "PreNota Pulado Gestor2", "prenota-pulado-gestor2@empresa.com", PapelGestor, 1)
	usuarioID := semearConta(t, db, "PreNota Pulado Usuario", "prenota-pulado-usuario@empresa.com", PapelUsuario, 2)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "PreNota Pulado", 10)
	pedido, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 3)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}
	if _, err := DecidirLotePedidos(db, empresaTeste, gestor1ID, []string{pedido.ID}, true); err != nil {
		t.Fatalf("decisão prévia: %v", err)
	}

	contadorAntes := contadorPreNota(t, db, empresaTeste)
	preNotasAntes := contarPreNotas(t, db, empresaTeste)

	// Lote inteiro 100% pulado: o único id já foi decidido acima.
	resultado, err := DecidirLotePedidos(db, empresaTeste, gestor2ID, []string{pedido.ID}, true)
	if err != nil {
		t.Fatalf("DecidirLotePedidos erro inesperado: %v", err)
	}
	if len(resultado) != 1 {
		t.Fatalf("len(resultado) = %d, want 1", len(resultado))
	}

	if contadorDepois := contadorPreNota(t, db, empresaTeste); contadorDepois != contadorAntes {
		t.Errorf("contador de docnum avançou de %d para %d num lote 100%% pulado, want inalterado", contadorAntes, contadorDepois)
	}
	if preNotasDepois := contarPreNotas(t, db, empresaTeste); preNotasDepois != preNotasAntes {
		t.Errorf("pré-notas = %d, want %d (nenhuma nova num lote 100%% pulado)", preNotasDepois, preNotasAntes)
	}
}

// TestDecidirLotePedidos_Rejeitado_NenhumaPreNotaGerada cobre a linha "Lote
// rejeitado" da I/O Matrix: aprovar=false, mesmo com vários Pedidos no lote
// -> nenhuma Pré-Nota é gerada, docnum não avança.
func TestDecidirLotePedidos_Rejeitado_NenhumaPreNotaGerada(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "PreNota Rejeitado Gestor", "prenota-rejeitado-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "PreNota Rejeitado Usuario", "prenota-rejeitado-usuario@empresa.com", PapelUsuario, 1)

	var pedidoIDs []string
	for _, nome := range []string{"PreNota Rejeitado A", "PreNota Rejeitado B"} {
		produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, nome, 10)
		pedido, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 2)
		if err != nil {
			t.Fatalf("seed SubmeterPedidoBaixa (%s): %v", nome, err)
		}
		pedidoIDs = append(pedidoIDs, pedido.ID)
	}

	contadorAntes := contadorPreNota(t, db, empresaTeste)
	preNotasAntes := contarPreNotas(t, db, empresaTeste)

	resultado, err := DecidirLotePedidos(db, empresaTeste, gestorID, pedidoIDs, false)
	if err != nil {
		t.Fatalf("DecidirLotePedidos erro inesperado: %v", err)
	}
	if len(resultado) != 2 {
		t.Fatalf("len(resultado) = %d, want 2", len(resultado))
	}
	for _, det := range resultado {
		if det.Status != "rejeitado" {
			t.Errorf("pedido %s: status = %q, want rejeitado", det.ID, det.Status)
		}
		if _, _, _, ok := preNotaDoPedido(t, db, det.ID); ok {
			t.Errorf("pedido %s: tem pré-nota vinculada, want nenhuma (lote rejeitado nunca gera Pré-Nota)", det.ID)
		}
	}
	if contadorDepois := contadorPreNota(t, db, empresaTeste); contadorDepois != contadorAntes {
		t.Errorf("contador de docnum avançou de %d para %d num lote rejeitado, want inalterado", contadorAntes, contadorDepois)
	}
	if preNotasDepois := contarPreNotas(t, db, empresaTeste); preNotasDepois != preNotasAntes {
		t.Errorf("pré-notas = %d, want %d (nenhuma nova num lote rejeitado)", preNotasDepois, preNotasAntes)
	}
}

// TestDecidirLotePedidos_DoisLotesSequenciais_DocnumCrescente cobre a linha
// "Dois lotes em sequência, mesma Empresa" da I/O Matrix: duas aprovações
// consecutivas -> docnum estritamente crescente, sem gap nem repetição.
func TestDecidirLotePedidos_DoisLotesSequenciais_DocnumCrescente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "PreNota Sequencial Gestor", "prenota-sequencial-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "PreNota Sequencial Usuario", "prenota-sequencial-usuario@empresa.com", PapelUsuario, 1)

	produtoA, estoqueA, _ := seedProdutoComSaldo(t, db, "PreNota Sequencial A", 10)
	pedidoA, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoA, estoqueA, 2)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa A: %v", err)
	}
	produtoB, estoqueB, _ := seedProdutoComSaldo(t, db, "PreNota Sequencial B", 10)
	pedidoB, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra Y", "", produtoB, estoqueB, 2)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa B: %v", err)
	}

	if _, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedidoA.ID}, true); err != nil {
		t.Fatalf("lote 1: %v", err)
	}
	if _, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedidoB.ID}, true); err != nil {
		t.Fatalf("lote 2: %v", err)
	}

	_, docnumA, _, okA := preNotaDoPedido(t, db, pedidoA.ID)
	_, docnumB, _, okB := preNotaDoPedido(t, db, pedidoB.ID)
	if !okA || !okB {
		t.Fatalf("ambos deveriam ter pré-nota: okA=%v okB=%v", okA, okB)
	}
	if docnumB != docnumA+1 {
		t.Errorf("docnum do lote 2 = %d, want %d (docnum do lote 1 + 1, sem gap nem repetição)", docnumB, docnumA+1)
	}
}

// TestDecidirLotePedidos_PreNotaConcorrenciaMesmaEmpresa_DocnumSemColisao
// prova que dois lotes concorrentes da MESMA Empresa (sobre Pedidos
// distintos, nunca a mesma linha) nunca colidem nem repetem docnum — mesmo
// molde de concorrência de TestDecidirLotePedidos_DecisoesConcorrentesSoAPrimeiraDebita,
// mas sobre 2 Pedidos diferentes (prova o contador atômico, não a trava de
// Pedido único).
func TestDecidirLotePedidos_PreNotaConcorrenciaMesmaEmpresa_DocnumSemColisao(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "PreNota Corrida Gestor", "prenota-corrida-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "PreNota Corrida Usuario", "prenota-corrida-usuario@empresa.com", PapelUsuario, 1)

	produtoA, estoqueA, _ := seedProdutoComSaldo(t, db, "PreNota Corrida A", 10)
	pedidoA, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoA, estoqueA, 2)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa A: %v", err)
	}
	produtoB, estoqueB, _ := seedProdutoComSaldo(t, db, "PreNota Corrida B", 10)
	pedidoB, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra Y", "", produtoB, estoqueB, 2)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa B: %v", err)
	}

	contadorAntes := contadorPreNota(t, db, empresaTeste)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var err1, err2 error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err1 = DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedidoA.ID}, true)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err2 = DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedidoB.ID}, true)
	}()
	close(start)
	wg.Wait()

	if err1 != nil {
		t.Fatalf("lote 1: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("lote 2: %v", err2)
	}

	_, docnumA, _, okA := preNotaDoPedido(t, db, pedidoA.ID)
	_, docnumB, _, okB := preNotaDoPedido(t, db, pedidoB.ID)
	if !okA || !okB {
		t.Fatalf("ambos deveriam ter pré-nota: okA=%v okB=%v", okA, okB)
	}
	if docnumA == docnumB {
		t.Fatalf("docnumA=%d docnumB=%d: colidiram, want distintos", docnumA, docnumB)
	}
	esperados := map[int]bool{contadorAntes + 1: true, contadorAntes + 2: true}
	if !esperados[docnumA] || !esperados[docnumB] {
		t.Errorf("docnumA=%d docnumB=%d, want exatamente {%d, %d} (sem gap nem repetição)", docnumA, docnumB, contadorAntes+1, contadorAntes+2)
	}
}

// TestDecidirLotePedidos_DuasEmpresasConcorrentes_DocnumIndependente cobre a
// linha "Duas Empresas aprovando ao mesmo tempo" da I/O Matrix: lotes
// concorrentes de Empresas diferentes -> cada uma numera a partir do próprio
// maior docnum, nunca misturando contagem.
func TestDecidirLotePedidos_DuasEmpresasConcorrentes_DocnumIndependente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "PreNota DuasEmpresas Gestor", "prenota-duasempresas-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "PreNota DuasEmpresas Usuario", "prenota-duasempresas-usuario@empresa.com", PapelUsuario, 1)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "PreNota DuasEmpresas", 10)
	pedido, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 2)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa (empresa 1): %v", err)
	}

	outraEmpresa := criarEmpresaDeTeste(t, db, "outra-empresa-prenota", "777888990010", "Outra Empresa PreNota")
	t.Cleanup(func() { removerEmpresaDeTeste(t, db, "outra-empresa-prenota") })
	// LIFO: os DELETEs abaixo, registrados DEPOIS, rodam ANTES do
	// removerEmpresaDeTeste acima — pedidos/pre_notas/produtos são dados de
	// negócio (não seedados por ProvisionarEmpresa), então o próprio teste
	// os limpa antes de remover a Empresa (mesmo padrão de
	// TestSubmeterPedidoBaixa_ProdutoOuEstoqueDeOutraEmpresa).
	t.Cleanup(func() {
		for _, stmt := range []string{
			`UPDATE pedidos SET pre_nota_id = NULL WHERE empresa_id = $1`,
			`DELETE FROM pre_notas WHERE empresa_id = $1`,
			`DELETE FROM pedido_itens WHERE empresa_id = $1`,
			`DELETE FROM movimentacoes WHERE empresa_id = $1`,
			`DELETE FROM pedidos WHERE empresa_id = $1`,
			`DELETE FROM produto_estoque WHERE produto_id IN (SELECT id FROM produtos WHERE empresa_id = $1)`,
			`DELETE FROM produtos WHERE empresa_id = $1`,
			`DELETE FROM estoques WHERE empresa_id = $1`,
			`DELETE FROM usuarios WHERE empresa_id = $1`,
		} {
			if _, err := db.Exec(stmt, outraEmpresa.ID); err != nil {
				t.Fatalf("limpeza da outra empresa [%s]: %v", stmt, err)
			}
		}
	})

	gestorOutraID := semearContaNaEmpresa(t, db, outraEmpresa.ID, "PreNota DuasEmpresas Gestor Outra", "prenota-duasempresas-gestor-outra@outra.com", PapelGestor)
	usuarioOutraID := semearContaNaEmpresa(t, db, outraEmpresa.ID, "PreNota DuasEmpresas Usuario Outra", "prenota-duasempresas-usuario-outra@outra.com", PapelUsuario)
	categoriaOutra := categoriaIDPorCodigoEmpresa(t, db, outraEmpresa.ID, "04.001")
	estoqueOutra, err := CriarEstoque(db, outraEmpresa.ID, filialTeste(t, db, outraEmpresa.ID), "Estoque PreNota Outra")
	if err != nil {
		t.Fatalf("seed CriarEstoque (outra empresa): %v", err)
	}
	produtoOutra, err := criarProdutoComSaldo(db, outraEmpresa.ID, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Produto PreNota Outra",
		CategoriaID:   categoriaOutra,
		TemplateID:    templateGenericoID(t, db, outraEmpresa.ID),
	}, estoqueOutra.ID, 10)
	if err != nil {
		t.Fatalf("seed criarProdutoComSaldo (outra empresa): %v", err)
	}
	pedidoOutra, err := SubmeterPedidoBaixa(db, outraEmpresa.ID, usuarioOutraID, "Fulano", "Obra X", "", produtoOutra.ID, estoqueOutra.ID, 2)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa (outra empresa): %v", err)
	}

	contadorAntes1 := contadorPreNota(t, db, empresaTeste)
	contadorAntes2 := contadorPreNota(t, db, outraEmpresa.ID)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var err1, err2 error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err1 = DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedido.ID}, true)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err2 = DecidirLotePedidos(db, outraEmpresa.ID, gestorOutraID, []string{pedidoOutra.ID}, true)
	}()
	close(start)
	wg.Wait()

	if err1 != nil {
		t.Fatalf("lote empresa 1: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("lote empresa 2: %v", err2)
	}

	_, docnum1, _, ok1 := preNotaDoPedido(t, db, pedido.ID)
	_, docnum2, _, ok2 := preNotaDoPedido(t, db, pedidoOutra.ID)
	if !ok1 || !ok2 {
		t.Fatalf("ambos deveriam ter pré-nota: ok1=%v ok2=%v", ok1, ok2)
	}
	if docnum1 != contadorAntes1+1 {
		t.Errorf("docnum empresa 1 = %d, want %d (contador próprio + 1)", docnum1, contadorAntes1+1)
	}
	if docnum2 != contadorAntes2+1 {
		t.Errorf("docnum empresa 2 = %d, want %d (contador próprio + 1, nunca misturado com a empresa 1)", docnum2, contadorAntes2+1)
	}
}

// TestDecidirLotePedidos_ExpoePreNotaIdEDocnumNasLeituras cobre o último item
// das Tasks de spec-20-4: PreNotaID/Docnum aparecem em
// ListarPedidosProprios/ListarPedidosFila/BuscarPedidoProprio depois da
// aprovação em lote (JOIN ao vivo com pre_notas) — nil/ausente enquanto
// pendente.
func TestDecidirLotePedidos_ExpoePreNotaIdEDocnumNasLeituras(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "PreNota Exposicao Gestor", "prenota-exposicao-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "PreNota Exposicao Usuario", "prenota-exposicao-usuario@empresa.com", PapelUsuario, 1)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "PreNota Exposicao", 10)
	pedido, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 2)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}

	// Antes de decidido: nenhuma das 3 leituras expõe Pré-Nota.
	det, err := BuscarPedidoProprio(db, empresaTeste, pedido.ID, usuarioID, PapelUsuario)
	if err != nil {
		t.Fatalf("BuscarPedidoProprio (pendente): %v", err)
	}
	if det.PreNotaID != nil || det.Docnum != nil {
		t.Errorf("BuscarPedidoProprio (pendente): PreNotaID=%v Docnum=%v, want nil nos dois", det.PreNotaID, det.Docnum)
	}
	proprios, err := ListarPedidosProprios(db, empresaTeste, usuarioID, "")
	if err != nil {
		t.Fatalf("ListarPedidosProprios (pendente): %v", err)
	}
	if ach := acharResumo(proprios, pedido.ID); ach == nil || ach.PreNotaID != nil || ach.Docnum != nil {
		t.Errorf("ListarPedidosProprios (pendente): resumo = %+v, want PreNotaID/Docnum nil", ach)
	}
	fila, err := ListarPedidosFila(db, empresaTeste, "")
	if err != nil {
		t.Fatalf("ListarPedidosFila (pendente): %v", err)
	}
	if ach := acharResumo(fila, pedido.ID); ach == nil || ach.PreNotaID != nil || ach.Docnum != nil {
		t.Errorf("ListarPedidosFila (pendente): resumo = %+v, want PreNotaID/Docnum nil", ach)
	}

	if _, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedido.ID}, true); err != nil {
		t.Fatalf("DecidirLotePedidos: %v", err)
	}
	preNotaID, docnum, _, ok := preNotaDoPedido(t, db, pedido.ID)
	if !ok {
		t.Fatalf("pedido deveria ter pré-nota depois do lote aprovado")
	}

	det, err = BuscarPedidoProprio(db, empresaTeste, pedido.ID, usuarioID, PapelUsuario)
	if err != nil {
		t.Fatalf("BuscarPedidoProprio (decidido): %v", err)
	}
	if det.PreNotaID == nil || *det.PreNotaID != preNotaID || det.Docnum == nil || *det.Docnum != docnum {
		t.Errorf("BuscarPedidoProprio (decidido): PreNotaID=%v Docnum=%v, want %q/%d", det.PreNotaID, det.Docnum, preNotaID, docnum)
	}
	proprios, err = ListarPedidosProprios(db, empresaTeste, usuarioID, "")
	if err != nil {
		t.Fatalf("ListarPedidosProprios (decidido): %v", err)
	}
	if ach := acharResumo(proprios, pedido.ID); ach == nil || ach.PreNotaID == nil || *ach.PreNotaID != preNotaID || ach.Docnum == nil || *ach.Docnum != docnum {
		t.Errorf("ListarPedidosProprios (decidido): resumo = %+v, want PreNotaID=%q Docnum=%d", ach, preNotaID, docnum)
	}
	fila, err = ListarPedidosFila(db, empresaTeste, "")
	if err != nil {
		t.Fatalf("ListarPedidosFila (decidido): %v", err)
	}
	if ach := acharResumo(fila, pedido.ID); ach == nil || ach.PreNotaID == nil || *ach.PreNotaID != preNotaID || ach.Docnum == nil || *ach.Docnum != docnum {
		t.Errorf("ListarPedidosFila (decidido): resumo = %+v, want PreNotaID=%q Docnum=%d", ach, preNotaID, docnum)
	}
}

// acharResumo acha, por id, a linha de um []PedidoResumo — helper de
// asserção dos testes de exposição de PreNotaID/Docnum acima.
func acharResumo(resumos []PedidoResumo, pedidoID string) *PedidoResumo {
	for i := range resumos {
		if resumos[i].ID == pedidoID {
			return &resumos[i]
		}
	}
	return nil
}

// TestDecidirLotePedidos_RespostaJaTrazPreNotaIdEDocnum cobre o achado de
// revisão (3 revisores independentes, Story 20.4): a RESPOSTA DA PRÓPRIA
// chamada que CRIA a Pré-Nota já devolve PreNotaID/Docnum preenchidos pros
// Pedidos decididos agora — antes desta correção, `resultado`/`porID` eram
// montados ANTES do bloco que gera a Pré-Nota e faz o UPDATE de
// `pedidos.pre_nota_id`, então a resposta sempre vinha com os dois nil; só
// uma releitura posterior (ListarPedidosProprios/Fila, BuscarPedidoProprio)
// mostrava o valor certo.
func TestDecidirLotePedidos_RespostaJaTrazPreNotaIdEDocnum(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "PreNota Resposta Gestor", "prenota-resposta-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "PreNota Resposta Usuario", "prenota-resposta-usuario@empresa.com", PapelUsuario, 1)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "PreNota Resposta", 10)
	pedido, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 2)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}

	resultado, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedido.ID}, true)
	if err != nil {
		t.Fatalf("DecidirLotePedidos erro inesperado: %v", err)
	}
	if len(resultado) != 1 {
		t.Fatalf("len(resultado) = %d, want 1", len(resultado))
	}
	det := resultado[0]
	if det.PreNotaID == nil || det.Docnum == nil {
		t.Fatalf("resposta da MESMA chamada que gerou a Pré-Nota: PreNotaID=%v Docnum=%v, want ambos preenchidos (nunca nil até uma releitura)", det.PreNotaID, det.Docnum)
	}

	// Confere que bate com o que foi de fato gravado — nunca um ponteiro só
	// preenchido pra passar no teste, com valor incorreto por trás.
	preNotaIDGravado, docnumGravado, _, ok := preNotaDoPedido(t, db, pedido.ID)
	if !ok {
		t.Fatalf("pedido deveria ter pré-nota gravada")
	}
	if *det.PreNotaID != preNotaIDGravado {
		t.Errorf("PreNotaID da resposta = %q, want %q (igual ao gravado)", *det.PreNotaID, preNotaIDGravado)
	}
	if *det.Docnum != docnumGravado {
		t.Errorf("Docnum da resposta = %d, want %d (igual ao gravado)", *det.Docnum, docnumGravado)
	}
}

// TestDecidirLotePedidos_MistoAprovadoEParcial_MesmaPreNotaNova cobre o
// achado de revisão (Story 20.4): um lote que mistura, na MESMA chamada
// aprovar=true, um Pedido que sai `aprovado` (total) e outro que sai
// `parcialmente_aprovado` (saldo insuficiente) — os dois caem na MESMA
// Pré-Nota nova (docnum/PreNotaID iguais), não só lotes uniformes (até aqui
// só "tudo aprovado" ou "tudo pulado" tinham cobertura).
func TestDecidirLotePedidos_MistoAprovadoEParcial_MesmaPreNotaNova(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "PreNota MistoStatus Gestor", "prenota-mistostatus-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "PreNota MistoStatus Usuario", "prenota-mistostatus-usuario@empresa.com", PapelUsuario, 1)

	produtoTotal, estoqueTotal, _ := seedProdutoComSaldo(t, db, "PreNota MistoStatus Total", 10)
	pedidoTotal, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoTotal, estoqueTotal, 4)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa (total): %v", err)
	}

	produtoParcial, estoqueParcial, _ := seedProdutoComSaldo(t, db, "PreNota MistoStatus Parcial", 10)
	pedidoParcial, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra Y", "", produtoParcial, estoqueParcial, 8)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa (parcial): %v", err)
	}
	// Saldo físico de Parcial cai para 3 (abaixo dos 8 solicitados/reservados)
	// ANTES da decisão — mesmo molde de
	// TestDecidirLotePedidos_ItemParcial_RestoDoLoteSeguiNormal.
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 3 WHERE produto_id = $1 AND estoque_id = $2`, produtoParcial, estoqueParcial); err != nil {
		t.Fatalf("seed: reduzir saldo real de Parcial: %v", err)
	}

	resultado, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedidoTotal.ID, pedidoParcial.ID}, true)
	if err != nil {
		t.Fatalf("DecidirLotePedidos erro inesperado: %v", err)
	}
	if len(resultado) != 2 {
		t.Fatalf("len(resultado) = %d, want 2", len(resultado))
	}
	if resultado[0].Status != "aprovado" {
		t.Errorf("pedido total: status = %q, want aprovado", resultado[0].Status)
	}
	if resultado[1].Status != "parcialmente_aprovado" {
		t.Errorf("pedido parcial: status = %q, want parcialmente_aprovado", resultado[1].Status)
	}

	for i, det := range resultado {
		if det.PreNotaID == nil || det.Docnum == nil {
			t.Fatalf("pedido[%d] (%s): PreNotaID=%v Docnum=%v, want ambos preenchidos", i, det.ID, det.PreNotaID, det.Docnum)
		}
	}
	// Os DOIS, apesar de status diferentes, caem na MESMA Pré-Nota nova —
	// um lote é UMA decisão de produto (Technical Decisions, epic-20-context.md).
	if *resultado[0].PreNotaID != *resultado[1].PreNotaID {
		t.Errorf("PreNotaID difere entre aprovado (%q) e parcialmente_aprovado (%q), want igual (MESMA Pré-Nota pro lote inteiro)", *resultado[0].PreNotaID, *resultado[1].PreNotaID)
	}
	if *resultado[0].Docnum != *resultado[1].Docnum {
		t.Errorf("Docnum difere entre aprovado (%d) e parcialmente_aprovado (%d), want igual", *resultado[0].Docnum, *resultado[1].Docnum)
	}
}
