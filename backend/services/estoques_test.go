package services

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
)

// limparEstoques zera a tabela `estoques` entre os testes desta suíte —
// `testDB` só trunca `usuarios CASCADE`, e `estoques` não tem FK para
// `usuarios`, então não é alcançada pelo CASCADE.
func limparEstoques(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`TRUNCATE TABLE estoques`); err != nil {
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
