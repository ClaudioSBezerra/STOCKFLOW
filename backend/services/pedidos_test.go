package services

import (
	"bytes"
	"database/sql"
	"errors"
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

	lista, err := ListarPedidosProprios(db, usuarioID, "parcialmente_aprovado")
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

	lista, err := ListarPedidosFila(db, "")
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

	lista, err := ListarPedidosFila(db, "")
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

	lista, err := ListarPedidosFila(db, "aprovado")
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

	lista, err := ListarPedidosFila(db, "parcialmente_aprovado")
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

	_, err := ListarPedidosFila(db, "banana")
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

	lista, err := ListarPedidosFila(db, "")
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

	lista, err := ListarPedidosParaSessao(db, almox, PapelAlmoxarife, true, "")
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

	lista, err := ListarPedidosParaSessao(db, almox, PapelAlmoxarife, false, "")
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

	lista, err := ListarPedidosParaSessao(db, usuarioA, PapelUsuario, true, "")
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

			lista, err := ListarPedidosParaSessao(db, ator, papel, true, "")
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
		if _, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, it.QtdSolicitada); err != nil {
			t.Fatalf("seed AdicionarItemCarrinho (%s): %v", it.NomeBase, err)
		}
	}
	pedido, err := SubmeterPedido(db, usuarioID, "Solicitante Decisao", "Obra Decisao", "")
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

	det, err := DecidirPedido(db, pedido.ID, almoxID, PapelAlmoxarife, true)
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

	det, err := DecidirPedido(db, pedido.ID, almoxID, PapelAlmoxarife, true)
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

	det, err := DecidirPedido(db, pedido.ID, almoxID, PapelAlmoxarife, true)
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

	det, err := DecidirPedido(db, pedido.ID, almoxID, PapelAlmoxarife, false)
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

	_, err := DecidirPedido(db, pedido.ID, almoxID, PapelAlmoxarife, true)
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

	if _, err := DecidirPedido(db, "nao-e-uuid", almoxID, PapelAlmoxarife, true); !errors.Is(err, ErrPedidoNaoEncontrado) {
		t.Errorf("id malformado: erro = %v, want ErrPedidoNaoEncontrado", err)
	}
	if _, err := DecidirPedido(db, "00000000-0000-0000-0000-000000000000", almoxID, PapelAlmoxarife, true); !errors.Is(err, ErrPedidoNaoEncontrado) {
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
		_, err1 = DecidirPedido(db, pedido.ID, almoxID, PapelAlmoxarife, true)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err2 = DecidirPedido(db, pedido.ID, almoxID, PapelAlmoxarife, true)
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
		det1, err1 = DecidirPedido(db, pedido.ID, almoxID, PapelAlmoxarife, true)
	}()
	go func() {
		defer wg.Done()
		<-start
		det2, err2 = DecidirPedido(db, pedido.ID, almoxID, PapelAlmoxarife, false)
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
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoB, estoqueB, 2); err != nil {
		t.Fatalf("seed Pedido1 item B: %v", err)
	}
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoA, estoqueA, 2); err != nil {
		t.Fatalf("seed Pedido1 item A: %v", err)
	}
	pedido1, err := SubmeterPedido(db, usuarioID, "Solicitante 1", "Obra 1", "")
	if err != nil {
		t.Fatalf("seed SubmeterPedido Pedido1: %v", err)
	}

	// Pedido2: mesmos dois produtos, item A adicionado ANTES de B.
	usuario2ID := semearConta(t, db, "Decisao OrdemLocks U2", "decisao-ordem-locks-u2@empresa.com", PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, usuario2ID, produtoA, estoqueA, 2); err != nil {
		t.Fatalf("seed Pedido2 item A: %v", err)
	}
	if _, err := AdicionarItemCarrinho(db, usuario2ID, produtoB, estoqueB, 2); err != nil {
		t.Fatalf("seed Pedido2 item B: %v", err)
	}
	pedido2, err := SubmeterPedido(db, usuario2ID, "Solicitante 2", "Obra 2", "")
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
			if _, err := AdicionarItemCarrinho(db, usuarioID, produtoB, estoqueB, 2); err != nil {
				t.Fatalf("iteração %d: seed Pedido1 item B: %v", i, err)
			}
			if _, err := AdicionarItemCarrinho(db, usuarioID, produtoA, estoqueA, 2); err != nil {
				t.Fatalf("iteração %d: seed Pedido1 item A: %v", i, err)
			}
			pedido1, err = SubmeterPedido(db, usuarioID, "Solicitante 1", "Obra 1", "")
			if err != nil {
				t.Fatalf("iteração %d: seed SubmeterPedido Pedido1: %v", i, err)
			}
			if _, err := AdicionarItemCarrinho(db, usuario2ID, produtoA, estoqueA, 2); err != nil {
				t.Fatalf("iteração %d: seed Pedido2 item A: %v", i, err)
			}
			if _, err := AdicionarItemCarrinho(db, usuario2ID, produtoB, estoqueB, 2); err != nil {
				t.Fatalf("iteração %d: seed Pedido2 item B: %v", i, err)
			}
			pedido2, err = SubmeterPedido(db, usuario2ID, "Solicitante 2", "Obra 2", "")
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
			_, err1 = DecidirPedido(db, pedido1.ID, almoxID, PapelAlmoxarife, true)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, err2 = DecidirPedido(db, pedido2.ID, almoxID, PapelAlmoxarife, true)
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

	det, err := DecidirPedido(db, pedido.ID, almoxID, PapelAlmoxarife, true)
	if err != nil {
		t.Fatalf("seed DecidirPedido: %v", err)
	}

	var nomeAprovador string
	if err := db.QueryRow(`SELECT nome FROM usuarios WHERE id = $1`, almoxID).Scan(&nomeAprovador); err != nil {
		t.Fatalf("seed: buscar nome do aprovador: %v", err)
	}

	conteudo, err := MontarReciboPedidoConteudo(db, pedido.ID, usuarioID, PapelUsuario)
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
	if _, err := DecidirPedido(db, pedido.ID, almoxID, PapelAlmoxarife, true); err != nil {
		t.Fatalf("seed DecidirPedido: %v", err)
	}

	conteudo, err := MontarReciboPedidoConteudo(db, pedido.ID, usuarioID, PapelUsuario)
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

	_, err := MontarReciboPedidoConteudo(db, pedido.ID, usuarioID, PapelUsuario)
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
	if _, err := DecidirPedido(db, pedido.ID, almoxID, PapelAlmoxarife, false); err != nil {
		t.Fatalf("seed DecidirPedido (rejeitar): %v", err)
	}

	_, err := MontarReciboPedidoConteudo(db, pedido.ID, usuarioID, PapelUsuario)
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
	if _, err := DecidirPedido(db, pedido.ID, almoxID, PapelAlmoxarife, true); err != nil {
		t.Fatalf("seed DecidirPedido: %v", err)
	}

	_, err := MontarReciboPedidoConteudo(db, pedido.ID, outro, PapelUsuario)
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
	if _, err := DecidirPedido(db, pedido.ID, almoxID, PapelAlmoxarife, true); err != nil {
		t.Fatalf("seed DecidirPedido: %v", err)
	}

	b1, err := GerarReciboPedidoPDF(db, pedido.ID, usuarioID, PapelUsuario)
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

	b2, err := GerarReciboPedidoPDF(db, pedido.ID, usuarioID, PapelUsuario)
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

	_, err := GerarReciboPedidoPDF(db, pedido.ID, usuarioID, PapelUsuario)
	if !errors.Is(err, ErrPedidoSemRecibo) {
		t.Fatalf("erro = %v, want ErrPedidoSemRecibo", err)
	}
}
