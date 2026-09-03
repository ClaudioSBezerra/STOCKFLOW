package services

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
)

// limparEstoques zera `estoques` entre os testes desta suíte — `testDB` só
// trunca `usuarios CASCADE`, e nenhuma das tabelas abaixo tem FK para
// `usuarios`, então não são alcançadas por aquele CASCADE. `produto_estoque`
// e `produtos` entram na mesma TRUNCATE (Story 3.1): `produto_estoque` tem FK
// para `estoques`, então truncar só `estoques` falharia (0A000) sempre que
// algum teste desta suíte tiver deixado uma linha de resíduo para trás (ex.
// TestExcluirEstoque_ComResiduo, que prova exatamente que a linha NÃO é
// removida). `importacao_linhas` entra pelo mesmo motivo (Story 3.3):
// `importacao_linhas.produto_id` referencia `produtos(id)` sem CASCADE.
func limparEstoques(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, carrinho_itens, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("falha ao limpar estoques: %v", err)
	}
}

func nomeNormalizado(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var norm string
	if err := db.QueryRow(`SELECT nome_normalizado FROM estoques WHERE id = $1`, id).Scan(&norm); err != nil {
		t.Fatalf("falha ao ler nome_normalizado de %s: %v", id, err)
	}
	return norm
}

func contarEstoques(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM estoques`).Scan(&n); err != nil {
		t.Fatalf("count estoques: %v", err)
	}
	return n
}

// TestCriarEstoque_Sucesso prova a AC1: cadastro válido grava uma linha com
// `id` e `nome`, e `nome_normalizado` é o nome em minúsculas com espaços
// colapsados.
func TestCriarEstoque_Sucesso(t *testing.T) {
	db := testDB(t)
	limparEstoques(t, db)

	e, err := CriarEstoque(db, "  Canteiro  A ")
	if err != nil {
		t.Fatalf("CriarEstoque erro inesperado: %v", err)
	}
	if e.ID == "" {
		t.Error("ID vazio no retorno")
	}
	if e.Nome != "Canteiro  A" {
		t.Errorf("Nome = %q, want %q (trim das pontas, espaço interno preservado)", e.Nome, "Canteiro  A")
	}
	if got := nomeNormalizado(t, db, e.ID); got != "canteiro a" {
		t.Errorf("nome_normalizado = %q, want %q", got, "canteiro a")
	}
	if n := contarEstoques(t, db); n != 1 {
		t.Errorf("linhas = %d, want 1", n)
	}
}

// TestCriarEstoque_DuplicataExata prova a AC2: um segundo cadastro com o
// mesmo nome -> ErrNomeEstoqueDuplicado, sem segunda linha.
func TestCriarEstoque_DuplicataExata(t *testing.T) {
	db := testDB(t)
	limparEstoques(t, db)

	if _, err := CriarEstoque(db, "Canteiro A"); err != nil {
		t.Fatalf("primeiro CriarEstoque: %v", err)
	}
	_, err := CriarEstoque(db, "Canteiro A")
	if !errors.Is(err, ErrNomeEstoqueDuplicado) {
		t.Fatalf("erro = %v, want ErrNomeEstoqueDuplicado", err)
	}
	if n := contarEstoques(t, db); n != 1 {
		t.Errorf("linhas = %d, want 1", n)
	}
}

// TestCriarEstoque_DuplicataPorCaixaEEspaco prova a AC2: capitalização e
// espaçamento diferentes colidem no mesmo `nome_normalizado`.
func TestCriarEstoque_DuplicataPorCaixaEEspaco(t *testing.T) {
	db := testDB(t)
	limparEstoques(t, db)

	if _, err := CriarEstoque(db, "Canteiro A"); err != nil {
		t.Fatalf("primeiro CriarEstoque: %v", err)
	}
	for _, variante := range []string{"  canteiro   a ", "CANTEIRO A", "Canteiro  A"} {
		_, err := CriarEstoque(db, variante)
		if !errors.Is(err, ErrNomeEstoqueDuplicado) {
			t.Fatalf("CriarEstoque(%q): erro = %v, want ErrNomeEstoqueDuplicado", variante, err)
		}
	}
	if n := contarEstoques(t, db); n != 1 {
		t.Errorf("linhas = %d, want 1", n)
	}
}

// TestCriarEstoque_NomeInvalido prova a validação: nome em branco ou acima de
// 255 runes -> ErrEstoqueValidacao, nada gravado.
func TestCriarEstoque_NomeInvalido(t *testing.T) {
	db := testDB(t)
	limparEstoques(t, db)

	casos := map[string]string{
		"vazio":        "",
		"só espaços":   "   ",
		"acima de 255": strings.Repeat("x", 256),
	}
	for nome, entrada := range casos {
		t.Run(nome, func(t *testing.T) {
			_, err := CriarEstoque(db, entrada)
			if !errors.Is(err, ErrEstoqueValidacao) {
				t.Fatalf("erro = %v, want ErrEstoqueValidacao", err)
			}
		})
	}
	// 255 runes exatos é válido.
	if _, err := CriarEstoque(db, strings.Repeat("y", 255)); err != nil {
		t.Fatalf("255 runes deveria ser válido, got %v", err)
	}
	if n := contarEstoques(t, db); n != 1 {
		t.Errorf("linhas = %d, want 1 (só o de 255 runes)", n)
	}
}

// TestCriarEstoque_Concorrencia prova a AC2 sob corrida: duas goroutines com
// nomes equivalentes só conseguem criar uma linha; a perdedora recebe
// ErrNomeEstoqueDuplicado (via SQLSTATE 23505 do índice único). Mesmo padrão
// de TestSolicitarPromocao_CorridaIndicePartial.
func TestCriarEstoque_Concorrencia(t *testing.T) {
	db := testDB(t)
	limparEstoques(t, db)

	entradas := []string{"Depósito Central", "  depósito   central "}
	const n = 2
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = CriarEstoque(db, entradas[i])
		}(i)
	}
	close(start)
	wg.Wait()

	var ok, conflito int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrNomeEstoqueDuplicado):
			conflito++
		default:
			t.Fatalf("erro inesperado na corrida: %v", err)
		}
	}
	if ok != 1 || conflito != n-1 {
		t.Errorf("ok=%d conflito=%d, want ok=1 conflito=%d", ok, conflito, n-1)
	}
	if got := contarEstoques(t, db); got != 1 {
		t.Errorf("linhas = %d, want 1", got)
	}
}

// TestExcluirEstoque_Sucesso prova a AC1: um Estoque existente é removido e a
// tabela volta a ficar vazia (contarEstoques == 0).
func TestExcluirEstoque_Sucesso(t *testing.T) {
	db := testDB(t)
	limparEstoques(t, db)

	e, err := CriarEstoque(db, "Canteiro Temp")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}
	if err := ExcluirEstoque(db, e.ID); err != nil {
		t.Fatalf("ExcluirEstoque erro inesperado: %v", err)
	}
	if n := contarEstoques(t, db); n != 0 {
		t.Errorf("linhas = %d, want 0", n)
	}
}

// TestExcluirEstoque_IdInexistente prova a AC2: um UUID válido sem linha
// correspondente -> ErrEstoqueNaoEncontrado (RowsAffected() == 0), nada removido.
func TestExcluirEstoque_IdInexistente(t *testing.T) {
	db := testDB(t)
	limparEstoques(t, db)
	if _, err := CriarEstoque(db, "Canteiro Vivo"); err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	err := ExcluirEstoque(db, "00000000-0000-4000-8000-000000000000")
	if !errors.Is(err, ErrEstoqueNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrEstoqueNaoEncontrado", err)
	}
	if n := contarEstoques(t, db); n != 1 {
		t.Errorf("linhas = %d, want 1 (nada removido)", n)
	}
}

// TestExcluirEstoque_IdMalformado prova a AC2: um id que não é UUID (`pq`
// SQLSTATE 22P02) colapsa no mesmo ErrEstoqueNaoEncontrado.
func TestExcluirEstoque_IdMalformado(t *testing.T) {
	db := testDB(t)
	limparEstoques(t, db)

	err := ExcluirEstoque(db, "nao-e-uuid")
	if !errors.Is(err, ErrEstoqueNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrEstoqueNaoEncontrado", err)
	}
}

// TestExcluirEstoque_ComResiduo prova a AC5 (Story 3.1, completando o guard
// pendente da Story 2.2): um Estoque com uma linha de `produto_estoque`
// vinculada e `quantidade > 0` -> *ErroEstoqueComResiduo citando o nome do
// Produto; nenhuma linha é removida (nem de `estoques` nem de
// `produto_estoque`).
func TestExcluirEstoque_ComResiduo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Com Residuo")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.007")
	produto, err := CriarProduto(db, CriarProdutoInput{
		Nome:              "Tubo PVC 100mm",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 5,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	err = ExcluirEstoque(db, estoque.ID)
	var residuo *ErroEstoqueComResiduo
	if !errors.As(err, &residuo) {
		t.Fatalf("erro = %v, want *ErroEstoqueComResiduo", err)
	}
	if len(residuo.Produtos) != 1 || residuo.Produtos[0] != produto.Nome {
		t.Errorf("Produtos = %v, want [%q]", residuo.Produtos, produto.Nome)
	}

	var nEstoques, nProdutoEstoque int
	if err := db.QueryRow(`SELECT count(*) FROM estoques WHERE id = $1`, estoque.ID).Scan(&nEstoques); err != nil {
		t.Fatalf("count estoques: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM produto_estoque WHERE estoque_id = $1`, estoque.ID).Scan(&nProdutoEstoque); err != nil {
		t.Fatalf("count produto_estoque: %v", err)
	}
	if nEstoques != 1 {
		t.Errorf("linhas em estoques = %d, want 1 (nada removido)", nEstoques)
	}
	if nProdutoEstoque != 1 {
		t.Errorf("linhas em produto_estoque = %d, want 1 (nada removido)", nProdutoEstoque)
	}
}

// TestExcluirEstoque_SemResiduoAposProdutoEstoqueZerado prova que uma linha de
// `produto_estoque` com `quantidade = 0` para o Estoque NÃO bloqueia a
// exclusão (Story 2.2 continua funcionando normalmente, agora exercitada de
// verdade com a tabela existindo).
func TestExcluirEstoque_SemResiduoAposProdutoEstoqueZerado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Sem Residuo")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "05.001")
	if _, err := CriarProduto(db, CriarProdutoInput{
		Nome:              "Capacete",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 0,
	}); err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	if err := ExcluirEstoque(db, estoque.ID); err != nil {
		t.Fatalf("ExcluirEstoque erro inesperado: %v", err)
	}
	if n := contarEstoques(t, db); n != 0 {
		t.Errorf("linhas em estoques = %d, want 0", n)
	}
}

// TestExcluirEstoque_CorridaComCriarProdutoResidual prova que o
// `SELECT ... FOR UPDATE` no início de ExcluirEstoque fecha a janela de
// corrida com um CriarProduto concorrente: as duas goroutines abaixo disparam
// ao mesmo tempo — uma exclui o Estoque, a outra cadastra um Produto nesse
// mesmo Estoque com quantidade inicial > 0 (isto é, cria uma linha de
// resíduo). Sem o lock, era possível o SELECT de resíduo de ExcluirEstoque
// rodar ANTES do INSERT de CriarProduto committar, e o DELETE (com
// ON DELETE CASCADE em produto_estoque.estoque_id) apagar silenciosamente a
// linha de resíduo recém-criada junto do Estoque — as duas operações
// "tendo sucesso" ao mesmo tempo, com o dado de resíduo perdido sem erro
// nenhum. Mesmo padrão de orquestração por canal de
// TestSolicitarPromocao_CorridaIndicePartial (promocao_test.go).
//
// Com o lock, só duas ordens de chegada são possíveis, e ambas são seguras:
//   - ExcluirEstoque trava a linha primeiro: se ele chega a commitar (sem
//     resíduo visto), o INSERT de CriarProduto (que ficou bloqueado
//     esperando o lock) roda depois contra um `estoque_id` que já não existe
//     mais -> falha com erro de validação (referência inválida). O Estoque
//     nunca teve uma linha de resíduo perdida — ela nunca chegou a existir.
//   - CriarProduto commita primeiro (sua própria escrita em produto_estoque
//     também exige, implicitamente, um lock em modo KEY SHARE sobre a linha
//     de `estoques` referenciada, que conflita com o FOR UPDATE seguinte):
//     ExcluirEstoque só adquire o lock depois, e o SELECT de resíduo dentro
//     da mesma transação já enxerga a linha nova -> ErroEstoqueComResiduo,
//     sem DELETE.
//
// A asserção central: as duas operações NUNCA terminam com sucesso
// simultâneo — o que seria a assinatura da corrida com dado perdido.
func TestExcluirEstoque_CorridaComCriarProdutoResidual(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Corrida Residuo")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	start := make(chan struct{})
	var wg sync.WaitGroup
	var errExcluir, errCriar error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		errExcluir = ExcluirEstoque(db, estoque.ID)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, errCriar = CriarProduto(db, CriarProdutoInput{
			Nome:              "Produto Corrida Residuo",
			CategoriaID:       categoriaID,
			EstoqueID:         estoque.ID,
			QuantidadeInicial: 5,
		})
	}()
	close(start)
	wg.Wait()

	excluiuComSucesso := errExcluir == nil
	criouComSucesso := errCriar == nil

	if excluiuComSucesso && criouComSucesso {
		t.Fatalf(
			"as duas operações tiveram sucesso simultâneo — sinal de dado de resíduo perdido silenciosamente (errExcluir=%v errCriar=%v)",
			errExcluir, errCriar,
		)
	}

	if excluiuComSucesso {
		// A exclusão vem primeiro: o cadastro concorrente deve ter falhado
		// referenciando um Estoque que já não existe mais.
		var erroValidacao *ErroProdutoValidacao
		if !errors.As(errCriar, &erroValidacao) {
			t.Fatalf("CriarProduto deveria falhar com *ErroProdutoValidacao quando a exclusão vence a corrida, got %v", errCriar)
		}
		if n := contarEstoques(t, db); n != 0 {
			t.Errorf("estoque deveria ter sido removido, linhas = %d, want 0", n)
		}
	} else {
		// O cadastro vem primeiro: a exclusão deve ter sido barrada pelo
		// guard de resíduo, e a linha residual deve continuar íntegra.
		var residuo *ErroEstoqueComResiduo
		if !errors.As(errExcluir, &residuo) {
			t.Fatalf("ExcluirEstoque deveria falhar com *ErroEstoqueComResiduo quando o cadastro vence a corrida, got %v", errExcluir)
		}
		if !criouComSucesso {
			t.Fatalf("CriarProduto deveria ter tido sucesso quando vence a corrida, got %v", errCriar)
		}
		if n := contarEstoques(t, db); n != 1 {
			t.Errorf("estoque deveria continuar existindo, linhas = %d, want 1", n)
		}
		var nResiduo int
		if err := db.QueryRow(
			`SELECT count(*) FROM produto_estoque WHERE estoque_id = $1 AND quantidade > 0`, estoque.ID,
		).Scan(&nResiduo); err != nil {
			t.Fatalf("count produto_estoque residual: %v", err)
		}
		if nResiduo != 1 {
			t.Errorf("linha residual deveria continuar presente, count = %d, want 1", nResiduo)
		}
	}
}

// TestListarEstoques_Vazio prova que ausência de linhas devolve slice vazio,
// nunca nil/erro.
func TestListarEstoques_Vazio(t *testing.T) {
	db := testDB(t)
	limparEstoques(t, db)

	lista, err := ListarEstoques(db)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if lista == nil {
		t.Fatal("lista = nil, want slice vazio")
	}
	if len(lista) != 0 {
		t.Fatalf("len = %d, want 0", len(lista))
	}
}

// TestListarEstoques_OrdenadoPorNomeNormalizado prova a ordenação por
// `nome_normalizado ASC` — independente da caixa do `nome` gravado e da ordem
// de inserção.
func TestListarEstoques_OrdenadoPorNomeNormalizado(t *testing.T) {
	db := testDB(t)
	limparEstoques(t, db)

	for _, nome := range []string{"Zinco", "  abc ", "Manga"} {
		if _, err := CriarEstoque(db, nome); err != nil {
			t.Fatalf("CriarEstoque(%q): %v", nome, err)
		}
	}

	lista, err := ListarEstoques(db)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	got := make([]string, len(lista))
	for i, e := range lista {
		got[i] = e.Nome
		if e.ID == "" {
			t.Errorf("linha %d sem ID: %+v", i, e)
		}
	}
	want := []string{"abc", "Manga", "Zinco"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("ordem = %v, want %v", got, want)
	}
}
