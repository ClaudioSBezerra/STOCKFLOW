package services

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
)

// carrinhoQuantidade lê a quantidade de uma linha de carrinho_itens
// diretamente, para asserções de "carrinho inalterado" após um erro
// esperado.
func carrinhoQuantidade(t *testing.T, db *sql.DB, usuarioID, produtoID, estoqueID string) (float64, bool) {
	t.Helper()
	var q float64
	err := db.QueryRow(
		`SELECT quantidade FROM carrinho_itens WHERE usuario_id = $1 AND produto_id = $2 AND estoque_id = $3`,
		usuarioID, produtoID, estoqueID,
	).Scan(&q)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	}
	if err != nil {
		t.Fatalf("falha ao ler quantidade do carrinho: %v", err)
	}
	return q, true
}

// --- AdicionarItemCarrinho (Story 7.1, spec-7-1) --------------------------

// TestAdicionarItemCarrinho_Sucesso prova a linha "Adição feliz" da I/O
// Matrix: Produto com saldo disponível, nada no carrinho ainda -> item
// gravado com a quantidade pedida, nome de Produto/Estoque resolvidos.
func TestAdicionarItemCarrinho_Sucesso(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Carrinho Add Sucesso", 10)
	usuarioID := semearConta(t, db, "Usuario Carrinho Sucesso", "carrinho-sucesso@empresa.com", PapelUsuario, 0)

	item, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 4)
	if err != nil {
		t.Fatalf("AdicionarItemCarrinho erro inesperado: %v", err)
	}
	if item.ProdutoID != produtoID {
		t.Errorf("ProdutoID = %q, want %q", item.ProdutoID, produtoID)
	}
	if item.ProdutoNome != "Produto Carrinho Add Sucesso" {
		t.Errorf("ProdutoNome = %q, want %q", item.ProdutoNome, "Produto Carrinho Add Sucesso")
	}
	if item.EstoqueID != estoqueID {
		t.Errorf("EstoqueID = %q, want %q", item.EstoqueID, estoqueID)
	}
	if item.EstoqueNome != "Carrinho Add Sucesso" {
		t.Errorf("EstoqueNome = %q, want %q", item.EstoqueNome, "Carrinho Add Sucesso")
	}
	if item.Quantidade != 4 {
		t.Errorf("Quantidade = %v, want 4", item.Quantidade)
	}

	// produto_estoque NÃO é debitado pela adição ao carrinho — o carrinho é
	// uma reserva textual, não um débito real (Design Notes de spec-7-1).
	if saldo := saldoProdutoEstoque(t, db, produtoID, estoqueID); saldo != 10 {
		t.Errorf("saldo de produto_estoque não deveria mudar, got %v, want 10", saldo)
	}
}

// TestAdicionarItemCarrinho_IncrementoDoMesmoPar prova a linha "Incremento
// do mesmo par": 3 já no carrinho, disponível 10, pede +5 -> upsert soma
// para 8.
func TestAdicionarItemCarrinho_IncrementoDoMesmoPar(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Carrinho Incremento", 10)
	usuarioID := semearConta(t, db, "Usuario Carrinho Incremento", "carrinho-incremento@empresa.com", PapelUsuario, 0)

	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 3); err != nil {
		t.Fatalf("primeira adição: erro inesperado: %v", err)
	}
	item, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 5)
	if err != nil {
		t.Fatalf("segunda adição: erro inesperado: %v", err)
	}
	if item.Quantidade != 8 {
		t.Errorf("Quantidade final = %v, want 8", item.Quantidade)
	}

	quantidade, existe := carrinhoQuantidade(t, db, usuarioID, produtoID, estoqueID)
	if !existe {
		t.Fatal("linha do carrinho não encontrada após incremento")
	}
	if quantidade != 8 {
		t.Errorf("linha em carrinho_itens = %v, want 8 (uma única linha, upsert)", quantidade)
	}
}

// TestAdicionarItemCarrinho_DisponibilidadeInsuficiente prova a linha
// "Disponibilidade insuficiente": 8 já no carrinho, disponível 10, pede +5
// (total 13) -> rejeitado com *ErroCarrinhoIndisponivel, carrinho inalterado
// (continua em 8).
func TestAdicionarItemCarrinho_DisponibilidadeInsuficiente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Carrinho Insuficiente", 10)
	usuarioID := semearConta(t, db, "Usuario Carrinho Insuficiente", "carrinho-insuficiente@empresa.com", PapelUsuario, 0)

	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 8); err != nil {
		t.Fatalf("adição inicial: erro inesperado: %v", err)
	}

	_, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 5)
	var erroIndisponivel *ErroCarrinhoIndisponivel
	if !errors.As(err, &erroIndisponivel) {
		t.Fatalf("erro = %v, want *ErroCarrinhoIndisponivel", err)
	}
	if erroIndisponivel.Restante != 2 {
		t.Errorf("Restante = %v, want 2 (10 disponível - 8 já no carrinho)", erroIndisponivel.Restante)
	}

	quantidade, existe := carrinhoQuantidade(t, db, usuarioID, produtoID, estoqueID)
	if !existe || quantidade != 8 {
		t.Errorf("carrinho deveria continuar em 8 (existe=%v, quantidade=%v)", existe, quantidade)
	}
}

// TestAdicionarItemCarrinho_SemLinhaEmProdutoEstoque prova a linha
// "Produto/Estoque sem linha em produto_estoque": par nunca teve saldo
// lançado -> tratado como 0 disponível, qualquer quantidade > 0 rejeitada,
// SEM criar linha nova em produto_estoque (Code Map, spec-7-1).
func TestAdicionarItemCarrinho_SemLinhaEmProdutoEstoque(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, _, _ := seedProdutoComSaldo(t, db, "Carrinho A Sem Saldo", 5)
	outroEstoque, err := CriarEstoque(db, "Carrinho B Sem Saldo")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	usuarioID := semearConta(t, db, "Usuario Carrinho Sem Saldo", "carrinho-sem-saldo@empresa.com", PapelUsuario, 0)

	_, err = AdicionarItemCarrinho(db, usuarioID, produtoID, outroEstoque.ID, 1)
	var erroIndisponivel *ErroCarrinhoIndisponivel
	if !errors.As(err, &erroIndisponivel) {
		t.Fatalf("erro = %v, want *ErroCarrinhoIndisponivel", err)
	}
	if erroIndisponivel.Restante != 0 {
		t.Errorf("Restante = %v, want 0", erroIndisponivel.Restante)
	}

	var existeLinha bool
	if err := db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2)`,
		produtoID, outroEstoque.ID,
	).Scan(&existeLinha); err != nil {
		t.Fatalf("checar linha produto_estoque: %v", err)
	}
	if existeLinha {
		t.Error("AdicionarItemCarrinho não deveria criar linha em produto_estoque")
	}
	if _, existe := carrinhoQuantidade(t, db, usuarioID, produtoID, outroEstoque.ID); existe {
		t.Error("nenhuma linha deveria ter sido gravada em carrinho_itens")
	}
}

// TestAdicionarItemCarrinho_ProdutoInexistenteOuMesclado prova a linha
// "Produto inexistente/mesclado": produtoId que não existe, ou que existe
// mas tem deleted_at preenchido (mesclado, Story 6.4), é recusado com
// ErrCarrinhoProdutoNaoEncontrado, sem tocar produto_estoque/carrinho_itens.
func TestAdicionarItemCarrinho_ProdutoInexistenteOuMesclado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Carrinho Produto Ruim", "carrinho-produto-ruim@empresa.com", PapelUsuario, 0)

	t.Run("inexistente", func(t *testing.T) {
		_, err := AdicionarItemCarrinho(db, usuarioID, "00000000-0000-0000-0000-000000000000", "00000000-0000-0000-0000-000000000000", 1)
		if !errors.Is(err, ErrCarrinhoProdutoNaoEncontrado) {
			t.Fatalf("erro = %v, want ErrCarrinhoProdutoNaoEncontrado", err)
		}
	})

	t.Run("malformado", func(t *testing.T) {
		_, err := AdicionarItemCarrinho(db, usuarioID, "nao-e-um-uuid", "nao-e-um-uuid", 1)
		if !errors.Is(err, ErrCarrinhoProdutoNaoEncontrado) {
			t.Fatalf("erro = %v, want ErrCarrinhoProdutoNaoEncontrado", err)
		}
	})

	t.Run("mesclado (soft-deletado)", func(t *testing.T) {
		produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Carrinho Produto Mesclado", 10)
		if _, err := db.Exec(`UPDATE produtos SET deleted_at = now() WHERE id = $1`, produtoID); err != nil {
			t.Fatalf("seed soft-delete: %v", err)
		}
		_, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 1)
		if !errors.Is(err, ErrCarrinhoProdutoNaoEncontrado) {
			t.Fatalf("erro = %v, want ErrCarrinhoProdutoNaoEncontrado", err)
		}
	})
}

// TestAdicionarItemCarrinho_EstoqueInexistenteOuMalformado prova que um
// estoqueId sintaticamente válido mas sem linha em `estoques` (hard-deletado,
// Story 2.2, ou que nunca existiu) é distinguido de "sem saldo lançado" —
// devolve ErrCarrinhoEstoqueNaoEncontrado (404), NUNCA
// *ErroCarrinhoIndisponivel (409, mensagem enganosa de "quantidade
// indisponível" para um Estoque que nem existe). estoqueId malformado
// (não-UUID), pareado com um produtoId válido, colapsa no mesmo 404 — mesmo
// molde de TestAdicionarItemCarrinho_ProdutoInexistenteOuMesclado.
func TestAdicionarItemCarrinho_EstoqueInexistenteOuMalformado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, _, _ := seedProdutoComSaldo(t, db, "Carrinho Estoque Ruim", 10)
	usuarioID := semearConta(t, db, "Usuario Carrinho Estoque Ruim", "carrinho-estoque-ruim@empresa.com", PapelUsuario, 0)

	t.Run("inexistente (sintaticamente válido)", func(t *testing.T) {
		_, err := AdicionarItemCarrinho(db, usuarioID, produtoID, "00000000-0000-0000-0000-000000000000", 1)
		if !errors.Is(err, ErrCarrinhoEstoqueNaoEncontrado) {
			t.Fatalf("erro = %v, want ErrCarrinhoEstoqueNaoEncontrado", err)
		}
		var erroIndisponivel *ErroCarrinhoIndisponivel
		if errors.As(err, &erroIndisponivel) {
			t.Fatalf("erro não deveria colapsar em *ErroCarrinhoIndisponivel (409): %v", err)
		}
	})

	t.Run("malformado", func(t *testing.T) {
		_, err := AdicionarItemCarrinho(db, usuarioID, produtoID, "nao-e-um-uuid", 1)
		if !errors.Is(err, ErrCarrinhoEstoqueNaoEncontrado) {
			t.Fatalf("erro = %v, want ErrCarrinhoEstoqueNaoEncontrado", err)
		}
	})
}

// TestAdicionarItemCarrinho_QuantidadeInvalida prova que quantidade <= 0 OU
// acima de limiteNumeric103 é rejeitada com *ErroCarrinhoValidacao ANTES de
// qualquer escrita — mesmo molde de RegistrarBaixa.
func TestAdicionarItemCarrinho_QuantidadeInvalida(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Carrinho Qtd Invalida", 10)
	usuarioID := semearConta(t, db, "Usuario Carrinho Qtd Invalida", "carrinho-qtd-invalida@empresa.com", PapelUsuario, 0)

	for _, quantidade := range []float64{0, -5, limiteNumeric103 + 0.001} {
		_, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, quantidade)
		var erroValidacao *ErroCarrinhoValidacao
		if !errors.As(err, &erroValidacao) {
			t.Fatalf("quantidade=%v: erro = %v, want *ErroCarrinhoValidacao", quantidade, err)
		}
	}

	if _, existe := carrinhoQuantidade(t, db, usuarioID, produtoID, estoqueID); existe {
		t.Error("nenhuma linha deveria ter sido gravada em carrinho_itens")
	}
}

// TestAdicionarItemCarrinho_ConcorrenciaMesmoUsuarioMesmoPar prova a AC5: o
// SELECT ... FOR UPDATE na linha de produto_estoque serializa duas adições
// concorrentes do MESMO usuário ao MESMO par — com disponível 10, duas
// adições de 6 (soma 12 > 10) não podem as duas suceder; a perdedora vê a
// quantidade já somada pela vencedora e é rejeitada, sem sobrescrever nem
// somar por cima. Molde de TestRegistrarBaixa_ConcorrenciaDuasBaixasMesmaLinha.
func TestAdicionarItemCarrinho_ConcorrenciaMesmoUsuarioMesmoPar(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Carrinho Corrida", 10)
	usuarioID := semearConta(t, db, "Usuario Carrinho Corrida", "carrinho-corrida@empresa.com", PapelUsuario, 0)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err1 = AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 6)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err2 = AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 6)
	}()
	close(start)
	wg.Wait()

	sucesso1, sucesso2 := err1 == nil, err2 == nil
	if sucesso1 && sucesso2 {
		t.Fatalf("as duas adições de 6 (disponível 10) tiveram sucesso simultâneo — deveriam somar 12 > 10: err1=%v err2=%v", err1, err2)
	}
	if !sucesso1 && !sucesso2 {
		t.Fatalf("as duas adições falharam — pelo menos uma deveria suceder (10 >= 6): err1=%v err2=%v", err1, err2)
	}

	erroPerdedora := err1
	if sucesso1 {
		erroPerdedora = err2
	}
	var erroIndisponivel *ErroCarrinhoIndisponivel
	if !errors.As(erroPerdedora, &erroIndisponivel) {
		t.Fatalf("erro da perdedora = %v, want *ErroCarrinhoIndisponivel", erroPerdedora)
	}
	if erroIndisponivel.Restante != 4 {
		t.Errorf("Restante visto pela perdedora = %v, want 4 (10 - 6 da vencedora)", erroIndisponivel.Restante)
	}

	quantidade, existe := carrinhoQuantidade(t, db, usuarioID, produtoID, estoqueID)
	if !existe || quantidade != 6 {
		t.Errorf("carrinho final = (existe=%v, quantidade=%v), want (true, 6) — só a vencedora gravou", existe, quantidade)
	}
}

// --- ListarCarrinho (Story 7.1, spec-7-1) ---------------------------------

// TestListarCarrinho_CarrinhoVazio prova a linha "Carrinho vazio": nenhuma
// linha para o usuário -> itens/removidos vazios (nunca nil), sem erro.
func TestListarCarrinho_CarrinhoVazio(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Carrinho Vazio", "carrinho-vazio@empresa.com", PapelUsuario, 0)

	itens, removidos, err := ListarCarrinho(db, usuarioID)
	if err != nil {
		t.Fatalf("ListarCarrinho erro inesperado: %v", err)
	}
	if itens == nil || len(itens) != 0 {
		t.Errorf("itens = %v, want slice vazio não-nil", itens)
	}
	if removidos == nil || len(removidos) != 0 {
		t.Errorf("removidos = %v, want slice vazio não-nil", removidos)
	}
}

// TestListarCarrinho_ItensAtivos prova a listagem normal: itens gravados por
// AdicionarItemCarrinho voltam com nome de Produto/Estoque resolvidos, sem
// aparecer em removidos.
func TestListarCarrinho_ItensAtivos(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Carrinho Listar Ativo", 10)
	usuarioID := semearConta(t, db, "Usuario Carrinho Listar Ativo", "carrinho-listar-ativo@empresa.com", PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 3); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho: %v", err)
	}

	itens, removidos, err := ListarCarrinho(db, usuarioID)
	if err != nil {
		t.Fatalf("ListarCarrinho erro inesperado: %v", err)
	}
	if len(removidos) != 0 {
		t.Errorf("removidos = %v, want vazio", removidos)
	}
	if len(itens) != 1 {
		t.Fatalf("len(itens) = %d, want 1", len(itens))
	}
	if itens[0].ProdutoNome != "Produto Carrinho Listar Ativo" {
		t.Errorf("ProdutoNome = %q", itens[0].ProdutoNome)
	}
	if itens[0].EstoqueNome != "Carrinho Listar Ativo" {
		t.Errorf("EstoqueNome = %q", itens[0].EstoqueNome)
	}
	if itens[0].Quantidade != 3 {
		t.Errorf("Quantidade = %v, want 3", itens[0].Quantidade)
	}
}

// TestListarCarrinho_ProdutoMescladoRemovidoAoAbrir prova a linha "Abrir
// carrinho com item obsoleto" (Produto mesclado): o item some da lista ativa
// e é apagado de carrinho_itens, devolvido em removidos com motivo
// "produto_removido" — sem erro (200 normal).
func TestListarCarrinho_ProdutoMescladoRemovidoAoAbrir(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Carrinho Produto Mesclado Listar", 10)
	usuarioID := semearConta(t, db, "Usuario Carrinho Produto Mesclado", "carrinho-produto-mesclado@empresa.com", PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 2); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho: %v", err)
	}
	if _, err := db.Exec(`UPDATE produtos SET deleted_at = now() WHERE id = $1`, produtoID); err != nil {
		t.Fatalf("seed soft-delete: %v", err)
	}

	itens, removidos, err := ListarCarrinho(db, usuarioID)
	if err != nil {
		t.Fatalf("ListarCarrinho erro inesperado: %v", err)
	}
	if len(itens) != 0 {
		t.Errorf("itens = %v, want vazio (produto mesclado)", itens)
	}
	if len(removidos) != 1 {
		t.Fatalf("len(removidos) = %d, want 1", len(removidos))
	}
	if removidos[0].Motivo != MotivoCarrinhoProdutoRemovido {
		t.Errorf("Motivo = %q, want %q", removidos[0].Motivo, MotivoCarrinhoProdutoRemovido)
	}
	if removidos[0].ProdutoID != produtoID {
		t.Errorf("ProdutoID = %q, want %q", removidos[0].ProdutoID, produtoID)
	}

	if _, existe := carrinhoQuantidade(t, db, usuarioID, produtoID, estoqueID); existe {
		t.Error("linha deveria ter sido apagada de carrinho_itens")
	}

	// Segunda chamada: a linha já foi apagada, nada mais a limpar.
	itens2, removidos2, err := ListarCarrinho(db, usuarioID)
	if err != nil {
		t.Fatalf("segunda ListarCarrinho erro inesperado: %v", err)
	}
	if len(itens2) != 0 || len(removidos2) != 0 {
		t.Errorf("segunda chamada deveria devolver tudo vazio, got itens=%v removidos=%v", itens2, removidos2)
	}
}

// TestListarCarrinho_EstoqueExcluidoRemovidoAoAbrir prova a mesma linha da
// I/O Matrix para o caso "Estoque excluído" (Story 2.2, hard-delete): a
// linha de carrinho_itens não tem FK para estoques (Design Notes,
// spec-7-1) — sobrevive à exclusão física do Estoque, e é a leitura de
// ListarCarrinho que detecta e limpa, com EstoqueNome nil (a linha de
// estoques não existe mais para resolver o nome).
func TestListarCarrinho_EstoqueExcluidoRemovidoAoAbrir(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Carrinho Estoque Excluido Listar", 10)
	usuarioID := semearConta(t, db, "Usuario Carrinho Estoque Excluido", "carrinho-estoque-excluido@empresa.com", PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 2); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho: %v", err)
	}

	// Hard-delete direto do Estoque (molde de ExcluirEstoque sem o guard de
	// resíduo — aqui o resíduo já não importa para o teste de carrinho):
	// `produto_estoque.estoque_id` tem `ON DELETE CASCADE` para
	// `estoques(id)` (estoques.go), então a linha de saldo cai junto.
	if _, err := db.Exec(`DELETE FROM estoques WHERE id = $1`, estoqueID); err != nil {
		t.Fatalf("seed hard-delete do estoque: %v", err)
	}

	itens, removidos, err := ListarCarrinho(db, usuarioID)
	if err != nil {
		t.Fatalf("ListarCarrinho erro inesperado: %v", err)
	}
	if len(itens) != 0 {
		t.Errorf("itens = %v, want vazio (estoque excluído)", itens)
	}
	if len(removidos) != 1 {
		t.Fatalf("len(removidos) = %d, want 1", len(removidos))
	}
	if removidos[0].Motivo != MotivoCarrinhoEstoqueExcluido {
		t.Errorf("Motivo = %q, want %q", removidos[0].Motivo, MotivoCarrinhoEstoqueExcluido)
	}
	if removidos[0].EstoqueNome != nil {
		t.Errorf("EstoqueNome = %v, want nil (estoque não existe mais)", *removidos[0].EstoqueNome)
	}
	if removidos[0].ProdutoNome != "Produto Carrinho Estoque Excluido Listar" {
		t.Errorf("ProdutoNome = %q", removidos[0].ProdutoNome)
	}

	if _, existe := carrinhoQuantidade(t, db, usuarioID, produtoID, estoqueID); existe {
		t.Error("linha deveria ter sido apagada de carrinho_itens")
	}
}

// --- RemoverItemCarrinho (Story 7.1, spec-7-1) ----------------------------

// TestRemoverItemCarrinho_Sucesso prova a remoção normal de um item
// existente.
func TestRemoverItemCarrinho_Sucesso(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Carrinho Remover Sucesso", 10)
	usuarioID := semearConta(t, db, "Usuario Carrinho Remover", "carrinho-remover@empresa.com", PapelUsuario, 0)
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoID, estoqueID, 2); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho: %v", err)
	}

	if err := RemoverItemCarrinho(db, usuarioID, produtoID, estoqueID); err != nil {
		t.Fatalf("RemoverItemCarrinho erro inesperado: %v", err)
	}

	if _, existe := carrinhoQuantidade(t, db, usuarioID, produtoID, estoqueID); existe {
		t.Error("linha deveria ter sido removida")
	}
}

// TestRemoverItemCarrinho_ItemInexistente prova a linha "Remover item
// inexistente": par que não está no carrinho do usuário -> nada é alterado,
// ErrCarrinhoItemNaoEncontrado.
func TestRemoverItemCarrinho_ItemInexistente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Usuario Carrinho Item Inexistente", "carrinho-item-inexistente@empresa.com", PapelUsuario, 0)

	err := RemoverItemCarrinho(db, usuarioID, "00000000-0000-0000-0000-000000000000", "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrCarrinhoItemNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrCarrinhoItemNaoEncontrado", err)
	}
}

// TestRemoverItemCarrinho_EscopadoPorUsuario prova que RemoverItemCarrinho
// nunca remove o item de OUTRO usuário para o mesmo par — Always de
// spec-7-1 ("toda operação de carrinho é escopada ao usuario_id da
// sessão").
func TestRemoverItemCarrinho_EscopadoPorUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Carrinho Escopo Usuario", 10)
	usuarioDono := semearConta(t, db, "Usuario Carrinho Dono", "carrinho-dono@empresa.com", PapelUsuario, 0)
	usuarioOutro := semearConta(t, db, "Usuario Carrinho Outro", "carrinho-outro@empresa.com", PapelUsuario, 1)
	if _, err := AdicionarItemCarrinho(db, usuarioDono, produtoID, estoqueID, 2); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho: %v", err)
	}

	err := RemoverItemCarrinho(db, usuarioOutro, produtoID, estoqueID)
	if !errors.Is(err, ErrCarrinhoItemNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrCarrinhoItemNaoEncontrado (item pertence a outro usuário)", err)
	}

	quantidade, existe := carrinhoQuantidade(t, db, usuarioDono, produtoID, estoqueID)
	if !existe || quantidade != 2 {
		t.Errorf("item do dono deveria continuar intacto, got existe=%v quantidade=%v", existe, quantidade)
	}
}
