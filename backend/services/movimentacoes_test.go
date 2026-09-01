package services

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// seedProdutoComSaldo cadastra um Produto com `quantidadeInicial` no Estoque
// `estoque` (criado com o nome dado) e devolve (produtoID, estoqueID,
// usuarioID) — trio pronto para RegistrarBaixa. `usuarioID` vem de uma conta
// `almoxarife` semeada (mesmo padrão de semearConta, usuarios_test.go).
func seedProdutoComSaldo(t *testing.T, db *sql.DB, nomeEstoque string, quantidadeInicial float64) (produtoID, estoqueID, usuarioID string) {
	t.Helper()
	estoque, err := CriarEstoque(db, nomeEstoque)
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	produto, err := CriarProduto(db, CriarProdutoInput{
		Nome:              "Produto " + nomeEstoque,
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: quantidadeInicial,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}
	slug := strings.ToLower(strings.ReplaceAll(nomeEstoque, " ", "-"))
	usuarioID = semearConta(t, db, "Almox "+nomeEstoque, slug+"-almox@empresa.com", PapelAlmoxarife, 0)
	return produto.ID, estoque.ID, usuarioID
}

func saldoProdutoEstoque(t *testing.T, db *sql.DB, produtoID, estoqueID string) float64 {
	t.Helper()
	var saldo float64
	if err := db.QueryRow(
		`SELECT quantidade FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2`,
		produtoID, estoqueID,
	).Scan(&saldo); err != nil {
		t.Fatalf("falha ao ler saldo: %v", err)
	}
	return saldo
}

func contarMovimentacoes(t *testing.T, db *sql.DB, produtoID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM movimentacoes WHERE produto_id = $1`, produtoID).Scan(&n); err != nil {
		t.Fatalf("count movimentacoes: %v", err)
	}
	return n
}

// TestRegistrarBaixa_Sucesso prova a AC1: quantidade válida <= disponível
// debita produto_estoque e insere a Movimentação tipo='baixa' na mesma
// transação, com estoque_destino_id NULL.
func TestRegistrarBaixa_Sucesso(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Canteiro Baixa Sucesso", 10)

	mov, err := RegistrarBaixa(db, produtoID, estoqueID, usuarioID, 3)
	if err != nil {
		t.Fatalf("RegistrarBaixa erro inesperado: %v", err)
	}
	if mov.ID == "" {
		t.Error("ID vazio no retorno")
	}
	if mov.ProdutoID != produtoID {
		t.Errorf("ProdutoID = %q, want %q", mov.ProdutoID, produtoID)
	}
	if mov.Tipo != "baixa" {
		t.Errorf("Tipo = %q, want baixa", mov.Tipo)
	}
	if mov.EstoqueOrigemID != estoqueID {
		t.Errorf("EstoqueOrigemID = %q, want %q", mov.EstoqueOrigemID, estoqueID)
	}
	if mov.EstoqueDestinoID != nil {
		t.Errorf("EstoqueDestinoID = %v, want nil", *mov.EstoqueDestinoID)
	}
	if mov.Quantidade != 3 {
		t.Errorf("Quantidade = %v, want 3", mov.Quantidade)
	}
	if mov.UsuarioID != usuarioID {
		t.Errorf("UsuarioID = %q, want %q", mov.UsuarioID, usuarioID)
	}
	if mov.CriadoEm.IsZero() {
		t.Error("CriadoEm zero")
	}

	if saldo := saldoProdutoEstoque(t, db, produtoID, estoqueID); saldo != 7 {
		t.Errorf("saldo pós-baixa = %v, want 7", saldo)
	}

	var estoqueDestinoNulo sql.NullString
	if err := db.QueryRow(
		`SELECT estoque_destino_id FROM movimentacoes WHERE id = $1`, mov.ID,
	).Scan(&estoqueDestinoNulo); err != nil {
		t.Fatalf("falha ao ler estoque_destino_id: %v", err)
	}
	if estoqueDestinoNulo.Valid {
		t.Errorf("estoque_destino_id = %q, want NULL", estoqueDestinoNulo.String)
	}
}

// TestRegistrarBaixa_QuantidadeZeroOuNegativa prova a AC2: quantidade <= 0
// é rejeitada com *ErroMovimentacaoValidacao ANTES de qualquer escrita —
// nem produto_estoque nem movimentacoes mudam.
func TestRegistrarBaixa_QuantidadeZeroOuNegativa(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Canteiro Baixa Invalida", 10)

	for _, quantidade := range []float64{0, -5} {
		_, err := RegistrarBaixa(db, produtoID, estoqueID, usuarioID, quantidade)
		var erroValidacao *ErroMovimentacaoValidacao
		if !errors.As(err, &erroValidacao) {
			t.Fatalf("quantidade=%v: erro = %v, want *ErroMovimentacaoValidacao", quantidade, err)
		}
	}

	if saldo := saldoProdutoEstoque(t, db, produtoID, estoqueID); saldo != 10 {
		t.Errorf("saldo não deveria ter mudado, got %v, want 10", saldo)
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 0 {
		t.Errorf("nenhuma Movimentação deveria ter sido criada, count = %d", n)
	}
}

// TestRegistrarBaixa_QuantidadeMaiorQueDisponivel prova a AC3: quantidade
// acima do saldo disponível -> *ErroQuantidadeIndisponivel citando o valor
// REAL disponível no momento, nada debitado.
func TestRegistrarBaixa_QuantidadeMaiorQueDisponivel(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Canteiro Baixa Excede", 4.5)

	_, err := RegistrarBaixa(db, produtoID, estoqueID, usuarioID, 10)
	var erroIndisponivel *ErroQuantidadeIndisponivel
	if !errors.As(err, &erroIndisponivel) {
		t.Fatalf("erro = %v, want *ErroQuantidadeIndisponivel", err)
	}
	if erroIndisponivel.Disponivel != 4.5 {
		t.Errorf("Disponivel = %v, want 4.5", erroIndisponivel.Disponivel)
	}
	mensagemEsperada := "quantidade indisponível: apenas " + strconv.FormatFloat(4.5, 'f', -1, 64) + " unidade(s) disponível(is)"
	if erroIndisponivel.Error() != mensagemEsperada {
		t.Errorf("mensagem = %q, want %q", erroIndisponivel.Error(), mensagemEsperada)
	}

	if saldo := saldoProdutoEstoque(t, db, produtoID, estoqueID); saldo != 4.5 {
		t.Errorf("saldo não deveria ter mudado, got %v, want 4.5", saldo)
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 0 {
		t.Errorf("nenhuma Movimentação deveria ter sido criada, count = %d", n)
	}
}

// TestRegistrarBaixa_ProdutoSemSaldoNesteEstoque prova a AC3 no caso "sem
// linha nenhuma" (par produto/estoque nunca teve saldo): sql.ErrNoRows no
// SELECT ... FOR UPDATE colapsa em *ErroQuantidadeIndisponivel{Disponivel: 0}
// — mesmo tratamento de um id malformado (ver Design Notes de spec-5-1).
func TestRegistrarBaixa_ProdutoSemSaldoNesteEstoque(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	// Produto cadastrado num Estoque A; tenta baixa num Estoque B onde nunca
	// teve saldo — nenhuma linha em produto_estoque para esse par.
	produtoID, _, usuarioID := seedProdutoComSaldo(t, db, "Canteiro A Sem Saldo", 5)
	outroEstoque, err := CriarEstoque(db, "Canteiro B Sem Saldo")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	_, err = RegistrarBaixa(db, produtoID, outroEstoque.ID, usuarioID, 1)
	var erroIndisponivel *ErroQuantidadeIndisponivel
	if !errors.As(err, &erroIndisponivel) {
		t.Fatalf("erro = %v, want *ErroQuantidadeIndisponivel", err)
	}
	if erroIndisponivel.Disponivel != 0 {
		t.Errorf("Disponivel = %v, want 0", erroIndisponivel.Disponivel)
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 0 {
		t.Errorf("nenhuma Movimentação deveria ter sido criada, count = %d", n)
	}
}

// TestRegistrarBaixa_IDMalformadoColapsaEmIndisponivel prova que um
// produtoID/estoqueID não-UUID (`pq` 22P02) colapsa no MESMO
// *ErroQuantidadeIndisponivel{Disponivel: 0} — nenhum 404 dedicado, ver
// Design Notes de spec-5-1.
func TestRegistrarBaixa_IDMalformadoColapsaEmIndisponivel(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Almox ID Malformado", "id-malformado-almox@empresa.com", PapelAlmoxarife, 0)

	_, err := RegistrarBaixa(db, "abc", "xyz", usuarioID, 1)
	var erroIndisponivel *ErroQuantidadeIndisponivel
	if !errors.As(err, &erroIndisponivel) {
		t.Fatalf("erro = %v, want *ErroQuantidadeIndisponivel", err)
	}
	if erroIndisponivel.Disponivel != 0 {
		t.Errorf("Disponivel = %v, want 0", erroIndisponivel.Disponivel)
	}
}

// TestRegistrarBaixa_ConcorrenciaDuasBaixasMesmaLinha prova que o
// SELECT ... FOR UPDATE serializa duas baixas concorrentes na mesma linha de
// produto_estoque (molde de TestExcluirEstoque_CorridaComCriarProdutoResidual,
// estoques_test.go): duas baixas de 6 cada, disparadas ao mesmo tempo, contra
// um saldo inicial de 10 — juntas somam mais que o disponível. Uma delas deve
// suceder e a outra ser rejeitada por *ErroQuantidadeIndisponivel (o lock
// impede que a segunda veja o saldo "pré-débito" da primeira); o saldo final
// nunca fica negativo, e o total debitado bate exatamente com o que foi
// aceito.
func TestRegistrarBaixa_ConcorrenciaDuasBaixasMesmaLinha(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Canteiro Corrida Baixa", 10)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err1 = RegistrarBaixa(db, produtoID, estoqueID, usuarioID, 6)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err2 = RegistrarBaixa(db, produtoID, estoqueID, usuarioID, 6)
	}()
	close(start)
	wg.Wait()

	sucesso1 := err1 == nil
	sucesso2 := err2 == nil

	if sucesso1 && sucesso2 {
		t.Fatalf("as duas baixas de 6 (saldo inicial 10) tiveram sucesso simultâneo — saldo deveria ter ficado negativo: err1=%v err2=%v", err1, err2)
	}
	if !sucesso1 && !sucesso2 {
		t.Fatalf("as duas baixas falharam — pelo menos uma deveria suceder (10 >= 6): err1=%v err2=%v", err1, err2)
	}

	// A que falhou deve ter falhado por indisponibilidade (saldo já debitado
	// pela vencedora), nunca por outro motivo.
	erroFalha := err1
	if sucesso1 {
		erroFalha = err2
	}
	var erroIndisponivel *ErroQuantidadeIndisponivel
	if !errors.As(erroFalha, &erroIndisponivel) {
		t.Fatalf("erro da baixa perdedora = %v, want *ErroQuantidadeIndisponivel", erroFalha)
	}
	if erroIndisponivel.Disponivel != 4 {
		t.Errorf("Disponivel visto pela baixa perdedora = %v, want 4 (10 - 6 da vencedora)", erroIndisponivel.Disponivel)
	}

	saldoFinal := saldoProdutoEstoque(t, db, produtoID, estoqueID)
	if saldoFinal < 0 {
		t.Fatalf("saldo final negativo: %v", saldoFinal)
	}
	if saldoFinal != 4 {
		t.Errorf("saldo final = %v, want 4 (10 - 6 da baixa vencedora)", saldoFinal)
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 1 {
		t.Errorf("Movimentacoes criadas = %d, want 1 (só a baixa vencedora)", n)
	}
}
