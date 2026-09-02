package services

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// limparProdutos zera produto_estoque/produtos/estoques entre os testes desta
// suíte, numa única TRUNCATE (Postgres resolve as FKs entre as três porque
// todas estão na mesma instrução). `categorias` NUNCA é truncada aqui — é
// seed fixo da migração 000010, compartilhado por toda a suíte.
//
// `importacao_linhas` entra na mesma TRUNCATE (Story 3.3):
// `importacao_linhas.produto_id` referencia `produtos(id)` SEM `ON DELETE
// CASCADE` — truncar só produtos/estoques falharia (0A000, "cannot truncate
// a table referenced in a foreign key constraint") assim que a suíte de
// importações tiver gravado alguma linha. `normalizacao_ignoradas` entra
// pelo mesmo motivo (Story 6.2): `normalizacao_ignoradas.produto_id`
// referencia `produtos(id)`, também sem CASCADE.
func limparProdutos(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("falha ao limpar produtos/produto_estoque/estoques: %v", err)
	}
}

// categoriaIDPorCodigo devolve o id de uma das 25 categorias fixas de seed,
// pelo código do addendum §H — usado como `categoria_id` válido nos testes.
func categoriaIDPorCodigo(t *testing.T, db *sql.DB, codigo string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE codigo = $1`, codigo).Scan(&id); err != nil {
		t.Fatalf("falha ao buscar categoria %q: %v", codigo, err)
	}
	return id
}

// templatePorSubtipo devolve o id e o texto de um dos 28 templates fixos de
// seed (migração 000013), pelo `subtipo` (addendum §G) — usado como
// `template_id`/texto esperado nos testes de Nomenclatura Guiada.
func templatePorSubtipo(t *testing.T, db *sql.DB, subtipo string) (id, texto string) {
	t.Helper()
	if err := db.QueryRow(
		`SELECT id, template FROM nomenclatura_templates WHERE subtipo = $1`, subtipo,
	).Scan(&id, &texto); err != nil {
		t.Fatalf("falha ao buscar template %q: %v", subtipo, err)
	}
	return id, texto
}

func contarProdutos(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM produtos`).Scan(&n); err != nil {
		t.Fatalf("count produtos: %v", err)
	}
	return n
}

func contarProdutoEstoque(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM produto_estoque`).Scan(&n); err != nil {
		t.Fatalf("count produto_estoque: %v", err)
	}
	return n
}

func ptrFloat(v float64) *float64 { return &v }
func ptrStr(v string) *string     { return &v }

// TestCriarProduto_SucessoCompleto prova a AC1: todos os campos + as 5
// dimensões pareadas -> 201 equivalente (Produto{ID,Nome}), uma linha em
// `produtos` com as dimensões gravadas e uma linha em `produto_estoque` com a
// quantidade inicial exata.
func TestCriarProduto_SucessoCompleto(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Produtos")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	input := CriarProdutoInput{
		Nome:              "  Tubo PVC 100mm  ",
		Codigo:            "  SKU-1  ",
		Observacoes:       "  observação de teste  ",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 12.5,
		Comprimento:       &DimensaoInput{Valor: ptrFloat(6), Unidade: ptrStr("m")},
		Largura:           &DimensaoInput{Valor: ptrFloat(100), Unidade: ptrStr("mm")},
		Diametro:          &DimensaoInput{Valor: ptrFloat(10), Unidade: ptrStr("cm")},
		Altura:            &DimensaoInput{Valor: ptrFloat(2), Unidade: ptrStr("m")},
		Espessura:         &DimensaoInput{Valor: ptrFloat(5), Unidade: ptrStr("mm")},
	}

	p, err := CriarProduto(db, input)
	if err != nil {
		t.Fatalf("CriarProduto erro inesperado: %v", err)
	}
	if p.ID == "" {
		t.Error("ID vazio no retorno")
	}
	if p.Nome != "Tubo PVC 100mm" {
		t.Errorf("Nome = %q, want %q (trim das pontas)", p.Nome, "Tubo PVC 100mm")
	}
	if n := contarProdutos(t, db); n != 1 {
		t.Errorf("linhas em produtos = %d, want 1", n)
	}

	var comprimentoValor, larguraValor float64
	var comprimentoUnidade, larguraUnidade, codigo, observacoes string
	if err := db.QueryRow(
		`SELECT comprimento_valor, comprimento_unidade, largura_valor, largura_unidade, codigo, observacoes
		 FROM produtos WHERE id = $1`, p.ID,
	).Scan(&comprimentoValor, &comprimentoUnidade, &larguraValor, &larguraUnidade, &codigo, &observacoes); err != nil {
		t.Fatalf("falha ao ler produto gravado: %v", err)
	}
	if comprimentoValor != 6 || comprimentoUnidade != "m" {
		t.Errorf("comprimento = %v %v, want 6 m", comprimentoValor, comprimentoUnidade)
	}
	if larguraValor != 100 || larguraUnidade != "mm" {
		t.Errorf("largura = %v %v, want 100 mm", larguraValor, larguraUnidade)
	}
	if codigo != "SKU-1" {
		t.Errorf("codigo = %q, want %q", codigo, "SKU-1")
	}
	if observacoes != "observação de teste" {
		t.Errorf("observacoes = %q, want %q", observacoes, "observação de teste")
	}

	if n := contarProdutoEstoque(t, db); n != 1 {
		t.Fatalf("linhas em produto_estoque = %d, want 1", n)
	}
	var quantidade float64
	if err := db.QueryRow(
		`SELECT quantidade FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2`,
		p.ID, estoque.ID,
	).Scan(&quantidade); err != nil {
		t.Fatalf("falha ao ler produto_estoque: %v", err)
	}
	if quantidade != 12.5 {
		t.Errorf("quantidade = %v, want 12.5", quantidade)
	}
}

// TestCriarProduto_SucessoSemDimensoes prova a AC1 no caso "sem dimensões":
// 201 equivalente, todas as 10 colunas de dimensão NULL.
func TestCriarProduto_SucessoSemDimensoes(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Sem Dimensao")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.002")

	p, err := CriarProduto(db, CriarProdutoInput{
		Nome:              "Produto Simples",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 0,
	})
	if err != nil {
		t.Fatalf("CriarProduto erro inesperado: %v", err)
	}

	var comprimentoValor sql.NullFloat64
	var comprimentoUnidade sql.NullString
	if err := db.QueryRow(
		`SELECT comprimento_valor, comprimento_unidade FROM produtos WHERE id = $1`, p.ID,
	).Scan(&comprimentoValor, &comprimentoUnidade); err != nil {
		t.Fatalf("falha ao ler produto: %v", err)
	}
	if comprimentoValor.Valid || comprimentoUnidade.Valid {
		t.Errorf("comprimento deveria ser NULL, got valor=%v unidade=%v", comprimentoValor, comprimentoUnidade)
	}
}

// TestCriarProduto_DimensaoParIncompleto prova a AC2: cada uma das 5
// dimensões, isoladamente, com só `valor` ou só `unidade` preenchido ->
// ErroProdutoValidacao citando o campo; nada gravado em produtos nem em
// produto_estoque.
func TestCriarProduto_DimensaoParIncompleto(t *testing.T) {
	db := testDB(t)

	casos := []struct {
		nome  string
		campo string
		apply func(*CriarProdutoInput)
	}{
		{"comprimento valor sem unidade", "comprimento", func(i *CriarProdutoInput) {
			i.Comprimento = &DimensaoInput{Valor: ptrFloat(6)}
		}},
		{"comprimento unidade sem valor", "comprimento", func(i *CriarProdutoInput) {
			i.Comprimento = &DimensaoInput{Unidade: ptrStr("m")}
		}},
		{"largura valor sem unidade", "largura", func(i *CriarProdutoInput) {
			i.Largura = &DimensaoInput{Valor: ptrFloat(10)}
		}},
		{"largura unidade sem valor", "largura", func(i *CriarProdutoInput) {
			i.Largura = &DimensaoInput{Unidade: ptrStr("cm")}
		}},
		{"diâmetro valor sem unidade", "diâmetro", func(i *CriarProdutoInput) {
			i.Diametro = &DimensaoInput{Valor: ptrFloat(10)}
		}},
		{"diâmetro unidade sem valor", "diâmetro", func(i *CriarProdutoInput) {
			i.Diametro = &DimensaoInput{Unidade: ptrStr("cm")}
		}},
		{"altura valor sem unidade", "altura", func(i *CriarProdutoInput) {
			i.Altura = &DimensaoInput{Valor: ptrFloat(2)}
		}},
		{"altura unidade sem valor", "altura", func(i *CriarProdutoInput) {
			i.Altura = &DimensaoInput{Unidade: ptrStr("cm")}
		}},
		{"espessura valor sem unidade", "espessura", func(i *CriarProdutoInput) {
			i.Espessura = &DimensaoInput{Valor: ptrFloat(5)}
		}},
		{"espessura unidade sem valor", "espessura", func(i *CriarProdutoInput) {
			i.Espessura = &DimensaoInput{Unidade: ptrStr("mm")}
		}},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			limparProdutos(t, db)
			estoque, err := CriarEstoque(db, "Canteiro "+c.nome)
			if err != nil {
				t.Fatalf("seed CriarEstoque: %v", err)
			}
			categoriaID := categoriaIDPorCodigo(t, db, "04.003")

			input := CriarProdutoInput{
				Nome:              "Produto " + c.nome,
				CategoriaID:       categoriaID,
				EstoqueID:         estoque.ID,
				QuantidadeInicial: 1,
			}
			c.apply(&input)

			_, err = CriarProduto(db, input)
			var erroValidacao *ErroProdutoValidacao
			if !errors.As(err, &erroValidacao) {
				t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
			}
			if !strings.Contains(strings.ToLower(erroValidacao.Mensagem), c.campo) {
				t.Errorf("mensagem = %q, want conter %q", erroValidacao.Mensagem, c.campo)
			}
			if n := contarProdutos(t, db); n != 0 {
				t.Errorf("linhas em produtos = %d, want 0 (nada deveria ser gravado)", n)
			}
			if n := contarProdutoEstoque(t, db); n != 0 {
				t.Errorf("linhas em produto_estoque = %d, want 0", n)
			}
		})
	}
}

// TestValidarDimensao_UnidadeInvalida prova que `validarDimensao` rejeita uma
// unidade fora de `{mm,cm,m}` mesmo com `valor` presente e válido — unidade
// como "km" nunca foi exercitada antes deste teste. Unitário e sem banco:
// `validarDimensao` é uma função pura (mesmo molde de pacote de
// TestRankPapel_OrdemTotal/TestProximoPapelPromocao, que também testam
// funções puras de services/ sem testDB).
func TestValidarDimensao_UnidadeInvalida(t *testing.T) {
	valor := 10.0
	unidade := "km"
	_, _, err := validarDimensao("largura", &DimensaoInput{Valor: &valor, Unidade: &unidade})

	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
	}
	if !strings.Contains(erroValidacao.Mensagem, "largura") || !strings.Contains(erroValidacao.Mensagem, "mm, cm ou m") {
		t.Errorf("mensagem = %q, want citar %q e o conjunto de unidades aceitas", erroValidacao.Mensagem, "largura")
	}
}

// TestValidarDimensao_ValorNaoPositivoComUnidadePresente prova que
// `valor <= 0` é rejeitado mesmo com `unidade` presente e válida — o caso
// `{"valor":0,"unidade":"m"}` nunca foi exercitado antes deste teste (só o
// par incompleto valor-sem-unidade/unidade-sem-valor tinha cobertura).
func TestValidarDimensao_ValorNaoPositivoComUnidadePresente(t *testing.T) {
	casos := map[string]float64{"zero": 0, "negativo": -5}
	for nome, valor := range casos {
		t.Run(nome, func(t *testing.T) {
			unidade := "m"
			_, _, err := validarDimensao("altura", &DimensaoInput{Valor: &valor, Unidade: &unidade})

			var erroValidacao *ErroProdutoValidacao
			if !errors.As(err, &erroValidacao) {
				t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
			}
			if !strings.Contains(erroValidacao.Mensagem, "altura") || !strings.Contains(erroValidacao.Mensagem, "maior que zero") {
				t.Errorf("mensagem = %q, want citar %q e \"maior que zero\"", erroValidacao.Mensagem, "altura")
			}
		})
	}
}

// TestValidarDimensao_ValorAcimaDoLimiteNumeric103 prova que um `valor` acima
// da magnitude representável por `NUMERIC(10,3)` (limiteNumeric103) é
// rejeitado ANTES de qualquer INSERT — sem este teste, um valor como `1e12`
// só seria detectado pelo Postgres como "numeric field overflow", um erro não
// mapeado que cairia no 500 genérico.
func TestValidarDimensao_ValorAcimaDoLimiteNumeric103(t *testing.T) {
	valor := limiteNumeric103 + 1
	unidade := "m"
	_, _, err := validarDimensao("comprimento", &DimensaoInput{Valor: &valor, Unidade: &unidade})

	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
	}
	if !strings.Contains(erroValidacao.Mensagem, "comprimento") {
		t.Errorf("mensagem = %q, want citar %q", erroValidacao.Mensagem, "comprimento")
	}

	// O limite exato é válido (só o que EXCEDE é rejeitado).
	valorLimite := limiteNumeric103
	_, _, err = validarDimensao("comprimento", &DimensaoInput{Valor: &valorLimite, Unidade: &unidade})
	if err != nil {
		t.Errorf("valor no limite exato deveria ser válido, got %v", err)
	}
}

// TestErroEstoqueComResiduo_ErrorComDoisOuMaisProdutos prova que
// ErroEstoqueComResiduo.Error() formata corretamente a mensagem com MAIS de
// um Produto residual, preservando a ordem recebida (a ordenação alfabética
// em si é responsabilidade do SELECT em ExcluirEstoque, já coberta por
// TestExcluirEstoque_ComResiduo — este teste cobre só a formatação da
// mensagem em si, nunca exercitada com mais de um nome).
func TestErroEstoqueComResiduo_ErrorComDoisOuMaisProdutos(t *testing.T) {
	err := &ErroEstoqueComResiduo{Produtos: []string{"Cabo Elétrico X", "Tubo PVC 100mm"}}
	want := "estoque possui quantidade residual de: Cabo Elétrico X, Tubo PVC 100mm"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	err3 := &ErroEstoqueComResiduo{Produtos: []string{"A", "B", "C"}}
	want3 := "estoque possui quantidade residual de: A, B, C"
	if got := err3.Error(); got != want3 {
		t.Errorf("Error() = %q, want %q", got, want3)
	}
}

// TestCriarProduto_CategoriaInexistente prova a AC2: um `categoria_id` que é
// UUID válido mas sem linha correspondente -> ErroProdutoValidacao (violação
// de FK, SQLSTATE 23503), nada gravado.
func TestCriarProduto_CategoriaInexistente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Categoria Ausente")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	_, err = CriarProduto(db, CriarProdutoInput{
		Nome:              "Produto Categoria Ausente",
		CategoriaID:       "00000000-0000-4000-8000-000000000000",
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
	}
	if n := contarProdutos(t, db); n != 0 {
		t.Errorf("linhas em produtos = %d, want 0", n)
	}
}

// TestCriarProduto_EstoqueInexistente prova a AC2: um `estoque_id` que é UUID
// válido mas sem linha correspondente -> ErroProdutoValidacao, e o INSERT em
// `produtos` (que já teria rodado antes de detectar o problema em
// produto_estoque) é desfeito pelo ROLLBACK da transação — nenhuma linha
// órfã em `produtos`.
func TestCriarProduto_EstoqueInexistente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	categoriaID := categoriaIDPorCodigo(t, db, "04.004")

	_, err := CriarProduto(db, CriarProdutoInput{
		Nome:              "Produto Estoque Ausente",
		CategoriaID:       categoriaID,
		EstoqueID:         "00000000-0000-4000-8000-000000000000",
		QuantidadeInicial: 1,
	})
	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
	}
	if n := contarProdutos(t, db); n != 0 {
		t.Errorf("linhas em produtos = %d, want 0 (rollback deveria ter desfeito o INSERT)", n)
	}
	if n := contarProdutoEstoque(t, db); n != 0 {
		t.Errorf("linhas em produto_estoque = %d, want 0", n)
	}
}

// TestCriarProduto_QuantidadeInicialNegativa prova a validação de
// `quantidade_inicial`: valor negativo -> ErroProdutoValidacao, nada gravado.
func TestCriarProduto_QuantidadeInicialNegativa(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Quantidade Negativa")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.005")

	_, err = CriarProduto(db, CriarProdutoInput{
		Nome:              "Produto Quantidade Negativa",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: -1,
	})
	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
	}
	if n := contarProdutos(t, db); n != 0 {
		t.Errorf("linhas em produtos = %d, want 0", n)
	}
}

// TestCriarProduto_QuantidadeInicialAcimaDoLimite prova a validação de
// `quantidade_inicial` contra a magnitude máxima de `NUMERIC(10,3)`
// (limiteNumeric103): acima do limite -> ErroProdutoValidacao, nada gravado
// — sem esta validação, o Postgres rejeitaria com "numeric field overflow"
// (não mapeado), caindo no 500 genérico em vez de 400.
func TestCriarProduto_QuantidadeInicialAcimaDoLimite(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Quantidade Acima Limite")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.007")

	_, err = CriarProduto(db, CriarProdutoInput{
		Nome:              "Produto Quantidade Acima Limite",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1e12,
	})
	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
	}
	if n := contarProdutos(t, db); n != 0 {
		t.Errorf("linhas em produtos = %d, want 0", n)
	}
	if n := contarProdutoEstoque(t, db); n != 0 {
		t.Errorf("linhas em produto_estoque = %d, want 0", n)
	}
}

// TestCriarProduto_CodigoAcimaDe255Caracteres prova a validação de `codigo`
// (também `VARCHAR(255)`, como `nome`, mas sem cobertura própria até este
// teste): acima de 255 runes -> ErroProdutoValidacao, nada gravado — sem esta
// validação, o INSERT falharia com "value too long for type character
// varying(255)" (não mapeado), caindo no 500 genérico em vez de 400.
func TestCriarProduto_CodigoAcimaDe255Caracteres(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Codigo Longo")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "05.002")

	_, err = CriarProduto(db, CriarProdutoInput{
		Nome:              "Produto Codigo Longo",
		Codigo:            strings.Repeat("x", 256),
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
	}
	if !strings.Contains(erroValidacao.Mensagem, "código") {
		t.Errorf("mensagem = %q, want citar %q", erroValidacao.Mensagem, "código")
	}
	if n := contarProdutos(t, db); n != 0 {
		t.Errorf("linhas em produtos = %d, want 0", n)
	}

	// 255 runes exatos é válido.
	_, err = CriarProduto(db, CriarProdutoInput{
		Nome:              "Produto Codigo No Limite",
		Codigo:            strings.Repeat("y", 255),
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("código de 255 runes deveria ser válido, got %v", err)
	}
}

// TestCriarProduto_CodigoJaCadastrado prova a Story 3.4 (spec-3-4): o índice
// único parcial `idx_produtos_codigo` (migration 000017) barra um segundo
// Produto com o mesmo `código` não-nulo — CriarProduto mapeia a violação de
// unicidade (SQLSTATE 23505) para ErroProdutoValidacao "código já
// cadastrado", nunca um 500 genérico; nenhum Produto novo é gravado.
func TestCriarProduto_CodigoJaCadastrado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Codigo Duplicado")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	_, err = CriarProduto(db, CriarProdutoInput{
		Nome:              "Produto Codigo Original",
		Codigo:            "SKU-DUP-MANUAL",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto (primeiro): %v", err)
	}

	_, err = CriarProduto(db, CriarProdutoInput{
		Nome:              "Produto Codigo Repetido",
		Codigo:            "SKU-DUP-MANUAL",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
	}
	if !strings.Contains(erroValidacao.Mensagem, "código já cadastrado") {
		t.Errorf("mensagem = %q, want citar %q", erroValidacao.Mensagem, "código já cadastrado")
	}
	if n := contarProdutos(t, db); n != 1 {
		t.Errorf("linhas em produtos = %d, want 1 (só o original, o segundo não foi gravado)", n)
	}
}

// TestCriarProduto_NomeInvalido prova a validação de `nome`: vazio após o
// trim -> ErroProdutoValidacao, nada gravado.
func TestCriarProduto_NomeInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Nome Invalido")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.006")

	_, err = CriarProduto(db, CriarProdutoInput{
		Nome:              "   ",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
	}
	if n := contarProdutos(t, db); n != 0 {
		t.Errorf("linhas em produtos = %d, want 0", n)
	}
}

// TestListarCategorias_Todas25OrdenadasPorCodigo prova a AC4: as 25
// categorias fixas de seed (migração 000010), ordenadas por `codigo`
// ascendente.
func TestListarCategorias_Todas25OrdenadasPorCodigo(t *testing.T) {
	db := testDB(t)

	categorias, err := ListarCategorias(db)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(categorias) != 25 {
		t.Fatalf("len = %d, want 25", len(categorias))
	}
	for i := 1; i < len(categorias); i++ {
		if categorias[i-1].Codigo >= categorias[i].Codigo {
			t.Errorf("ordem quebrada em %d: %q >= %q", i, categorias[i-1].Codigo, categorias[i].Codigo)
		}
	}
	primeira := categorias[0]
	if primeira.Codigo != "04.001" || primeira.Nome != "Materiais Civis" || primeira.ID == "" {
		t.Errorf("primeira categoria = %+v, want {codigo:04.001 nome:Materiais Civis}", primeira)
	}
}

// --- Story 3.2: Nomenclatura Guiada por subtipo -----------------------------

// TestCriarProduto_ComTemplateNomeCompleto prova a AC1: `template_id` válido
// + `nome` preenchendo todos os placeholders na mesma ordem -> sucesso, e
// `produtos.template_id` grava o template escolhido.
func TestCriarProduto_ComTemplateNomeCompleto(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Template Completo")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.002")
	templateID, _ := templatePorSubtipo(t, db, "Tubo — PEAD/PPR")

	p, err := CriarProduto(db, CriarProdutoInput{
		Nome:              "TUBO PEAD PN80 DN50",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		TemplateID:        templateID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("CriarProduto erro inesperado: %v", err)
	}

	var templateIDGravado sql.NullString
	if err := db.QueryRow(`SELECT template_id FROM produtos WHERE id = $1`, p.ID).Scan(&templateIDGravado); err != nil {
		t.Fatalf("falha ao ler produto gravado: %v", err)
	}
	if !templateIDGravado.Valid || templateIDGravado.String != templateID {
		t.Errorf("template_id gravado = %v, want %q", templateIDGravado, templateID)
	}
}

// TestCriarProduto_ComTemplatePlaceholderFaltando prova a AC2: `nome` que não
// preenche todos os placeholders do template selecionado -> ErroProdutoValidacao,
// nada gravado.
func TestCriarProduto_ComTemplatePlaceholderFaltando(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Template Incompleto")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.002")
	templateID, _ := templatePorSubtipo(t, db, "Tubo — PEAD/PPR")

	_, err = CriarProduto(db, CriarProdutoInput{
		Nome:              "TUBO PEAD PN80", // falta o segmento DN[XX]
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		TemplateID:        templateID,
		QuantidadeInicial: 1,
	})
	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
	}
	if !strings.Contains(erroValidacao.Mensagem, "template") {
		t.Errorf("mensagem = %q, want citar %q", erroValidacao.Mensagem, "template")
	}
	if n := contarProdutos(t, db); n != 0 {
		t.Errorf("linhas em produtos = %d, want 0", n)
	}
}

// TestCriarProduto_TemplateInexistente prova a AC2: `template_id` que é UUID
// válido sem linha correspondente, OU malformado, -> ErroProdutoValidacao
// "template selecionado não existe", nada gravado.
func TestCriarProduto_TemplateInexistente(t *testing.T) {
	db := testDB(t)

	casos := map[string]string{
		"uuid válido sem linha": "00000000-0000-4000-8000-000000000000",
		"id malformado":         "não-e-um-uuid",
	}
	for nome, templateID := range casos {
		t.Run(nome, func(t *testing.T) {
			limparProdutos(t, db)
			estoque, err := CriarEstoque(db, "Canteiro Template Ausente "+nome)
			if err != nil {
				t.Fatalf("seed CriarEstoque: %v", err)
			}
			categoriaID := categoriaIDPorCodigo(t, db, "04.003")

			_, err = CriarProduto(db, CriarProdutoInput{
				Nome:              "Produto Template Ausente",
				CategoriaID:       categoriaID,
				EstoqueID:         estoque.ID,
				TemplateID:        templateID,
				QuantidadeInicial: 1,
			})
			var erroValidacao *ErroProdutoValidacao
			if !errors.As(err, &erroValidacao) {
				t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
			}
			if !strings.Contains(erroValidacao.Mensagem, "template selecionado não existe") {
				t.Errorf("mensagem = %q, want %q", erroValidacao.Mensagem, "template selecionado não existe")
			}
			if n := contarProdutos(t, db); n != 0 {
				t.Errorf("linhas em produtos = %d, want 0", n)
			}
		})
	}
}

// TestCriarProduto_SemTemplateGravaTemplateIDNulo prova a regressão da Story
// 3.1: `template_id` ausente -> sucesso, sem exigência de estrutura, e a
// coluna `template_id` fica NULL.
func TestCriarProduto_SemTemplateGravaTemplateIDNulo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Sem Template")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.004")

	p, err := CriarProduto(db, CriarProdutoInput{
		Nome:              "qualquer texto livre, sem estrutura nenhuma",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("CriarProduto erro inesperado: %v", err)
	}

	var templateID sql.NullString
	if err := db.QueryRow(`SELECT template_id FROM produtos WHERE id = $1`, p.ID).Scan(&templateID); err != nil {
		t.Fatalf("falha ao ler produto gravado: %v", err)
	}
	if templateID.Valid {
		t.Errorf("template_id = %v, want NULL", templateID)
	}
}

// TestAtualizarNomeProduto_SemTemplateAceitaQualquerNome prova que renomear
// um Produto sem `template_id` aceita qualquer texto (validado só pela regra
// básica de nome).
func TestAtualizarNomeProduto_SemTemplateAceitaQualquerNome(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Renomear Sem Template")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.005")
	p, err := CriarProduto(db, CriarProdutoInput{
		Nome:              "Nome Original",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	atualizado, err := AtualizarNomeProduto(db, p.ID, "Qualquer Nome Novo Sem Estrutura")
	if err != nil {
		t.Fatalf("AtualizarNomeProduto erro inesperado: %v", err)
	}
	if atualizado.Nome != "Qualquer Nome Novo Sem Estrutura" {
		t.Errorf("Nome = %q, want %q", atualizado.Nome, "Qualquer Nome Novo Sem Estrutura")
	}

	var nomeGravado string
	if err := db.QueryRow(`SELECT nome FROM produtos WHERE id = $1`, p.ID).Scan(&nomeGravado); err != nil {
		t.Fatalf("falha ao ler produto: %v", err)
	}
	if nomeGravado != "Qualquer Nome Novo Sem Estrutura" {
		t.Errorf("nome gravado = %q, want %q", nomeGravado, "Qualquer Nome Novo Sem Estrutura")
	}
}

// TestAtualizarNomeProduto_ComTemplateRevalida prova a AC3/AC4: Produto com
// `template_id` aplicado — um novo nome que preenche o mesmo template ->
// sucesso e `produtos.nome` atualizado; um novo nome que NÃO preenche ->
// ErroProdutoValidacao e o `nome` gravado permanece o anterior.
func TestAtualizarNomeProduto_ComTemplateRevalida(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Renomear Com Template")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.006")
	templateID, _ := templatePorSubtipo(t, db, "Tubo — PEAD/PPR")

	p, err := CriarProduto(db, CriarProdutoInput{
		Nome:              "TUBO PEAD PN80 DN50",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		TemplateID:        templateID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	// Nome inválido contra o mesmo template -> erro, nome antigo preservado.
	_, err = AtualizarNomeProduto(db, p.ID, "TUBO PEAD PN80")
	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
	}
	if !strings.Contains(erroValidacao.Mensagem, "template aplicado a este produto") {
		t.Errorf("mensagem = %q, want citar %q", erroValidacao.Mensagem, "template aplicado a este produto")
	}
	var nomeAposFalha string
	if err := db.QueryRow(`SELECT nome FROM produtos WHERE id = $1`, p.ID).Scan(&nomeAposFalha); err != nil {
		t.Fatalf("falha ao ler produto: %v", err)
	}
	if nomeAposFalha != "TUBO PEAD PN80 DN50" {
		t.Errorf("nome após falha = %q, want o nome original preservado", nomeAposFalha)
	}

	// Nome válido contra o mesmo template -> sucesso.
	atualizado, err := AtualizarNomeProduto(db, p.ID, "TUBO PEAD PN100 DN75")
	if err != nil {
		t.Fatalf("AtualizarNomeProduto erro inesperado: %v", err)
	}
	if atualizado.Nome != "TUBO PEAD PN100 DN75" {
		t.Errorf("Nome = %q, want %q", atualizado.Nome, "TUBO PEAD PN100 DN75")
	}
}

// TestAtualizarNomeProduto_IDInexistente prova que um `id` sem linha
// correspondente (UUID válido ou malformado) -> ErrProdutoNaoEncontrado.
func TestAtualizarNomeProduto_IDInexistente(t *testing.T) {
	db := testDB(t)

	casos := map[string]string{
		"uuid válido sem linha": "00000000-0000-4000-8000-000000000000",
		"id malformado":         "não-e-um-uuid",
	}
	for nome, id := range casos {
		t.Run(nome, func(t *testing.T) {
			_, err := AtualizarNomeProduto(db, id, "Nome Qualquer")
			if !errors.Is(err, ErrProdutoNaoEncontrado) {
				t.Fatalf("erro = %v, want ErrProdutoNaoEncontrado", err)
			}
		})
	}
}

// TestAtualizarNomeProduto_NomeInvalido prova que a validação básica de nome
// (vazio após trim) roda ANTES de tocar o banco -> ErroProdutoValidacao.
func TestAtualizarNomeProduto_NomeInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Renomear Nome Invalido")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.007")
	p, err := CriarProduto(db, CriarProdutoInput{
		Nome:              "Nome Original",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	_, err = AtualizarNomeProduto(db, p.ID, "   ")
	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroProdutoValidacao", err)
	}
}

// --- Story 4.1: Busca por nome/código/categoria com sugestões --------------

// criarProdutoBusca cadastra um Produto mínimo (nome/código/categoria) para
// os testes de BuscarProdutos, reaproveitando um único Estoque para toda a
// suíte (a busca não depende de Estoque/quantidade).
func criarProdutoBusca(t *testing.T, db *sql.DB, estoqueID, nome, codigo, categoriaID string) ProdutoBusca {
	t.Helper()
	p, err := CriarProduto(db, CriarProdutoInput{
		Nome:              nome,
		Codigo:            codigo,
		CategoriaID:       categoriaID,
		EstoqueID:         estoqueID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto(%q): %v", nome, err)
	}
	var codigoPtr *string
	if codigo != "" {
		c := codigo
		codigoPtr = &c
	}
	return ProdutoBusca{ID: p.ID, Nome: p.Nome, Codigo: codigoPtr}
}

// TestBuscarProdutos_MatchExatoVemPrimeiro prova a linha "Match exato de
// nome ou código" da matriz de I/O: um Produto cujo `codigo` é IGUAL
// (case-insensitive) ao termo buscado aparece em 1º lugar, à frente de um
// Produto que só bate por prefixo.
func TestBuscarProdutos_MatchExatoVemPrimeiro(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Busca 1")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoBusca(t, db, estoque.ID, "Parafuso Sextavado M8", "PAR-0010", categoriaID)
	exato := criarProdutoBusca(t, db, estoque.ID, "Outro Nome Qualquer", "par-001", categoriaID)

	resultado, err := BuscarProdutos(db, "PAR-001")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(resultado) != 2 {
		t.Fatalf("len(resultado) = %d, want 2", len(resultado))
	}
	if resultado[0].ID != exato.ID {
		t.Errorf("resultado[0].ID = %q, want %q (match exato de código primeiro)", resultado[0].ID, exato.ID)
	}
}

// TestBuscarProdutos_MatchPorPrefixo prova a linha "Match por prefixo" da
// matriz: dois Produtos cujo nome começa com o termo aparecem, ordenados
// por nome (rank igual, ORDER BY nome ASC).
func TestBuscarProdutos_MatchPorPrefixo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Busca 2")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoBusca(t, db, estoque.ID, "Parafuso Sextavado", "", categoriaID)
	criarProdutoBusca(t, db, estoque.ID, "Parafina", "", categoriaID)
	criarProdutoBusca(t, db, estoque.ID, "Sem Relação Nenhuma", "", categoriaID)

	resultado, err := BuscarProdutos(db, "paraf")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(resultado) != 2 {
		t.Fatalf("len(resultado) = %d, want 2", len(resultado))
	}
	if resultado[0].Nome != "Parafina" || resultado[1].Nome != "Parafuso Sextavado" {
		t.Errorf("ordem = [%q, %q], want [Parafina, Parafuso Sextavado] (rank 1, ORDER BY nome)",
			resultado[0].Nome, resultado[1].Nome)
	}
}

// TestBuscarProdutos_MatchSoPorCategoria prova a linha "Match só por
// categoria" da matriz: um Produto sem nenhum match em nome/código, mas
// pertencente a uma categoria cujo nome bate o termo, ainda aparece
// (rank 2).
func TestBuscarProdutos_MatchSoPorCategoria(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Busca 3")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaEletrica := categoriaIDPorCodigo(t, db, "04.002") // "Materiais Elétricos"
	categoriaCivil := categoriaIDPorCodigo(t, db, "04.001")

	semRelacao := criarProdutoBusca(t, db, estoque.ID, "Disjuntor Bipolar", "DISJ-1", categoriaEletrica)
	criarProdutoBusca(t, db, estoque.ID, "Tubo PVC", "TUBO-1", categoriaCivil)

	resultado, err := BuscarProdutos(db, "elétric")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(resultado) != 1 {
		t.Fatalf("len(resultado) = %d, want 1", len(resultado))
	}
	if resultado[0].ID != semRelacao.ID {
		t.Errorf("resultado[0].ID = %q, want %q", resultado[0].ID, semRelacao.ID)
	}
	if resultado[0].Categoria.Nome != "Materiais Elétricos" {
		t.Errorf("Categoria.Nome = %q, want %q", resultado[0].Categoria.Nome, "Materiais Elétricos")
	}
}

// TestBuscarProdutos_MaisDe7MatchesLimitaA7 prova a linha "Mais de 7
// matches" da matriz: 10 Produtos batendo o termo -> só 7 voltam.
func TestBuscarProdutos_MaisDe7MatchesLimitaA7(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Busca 4")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	for i := 0; i < 10; i++ {
		criarProdutoBusca(t, db, estoque.ID, fmt.Sprintf("Parafuso Tipo %02d", i), "", categoriaID)
	}

	resultado, err := BuscarProdutos(db, "parafuso")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(resultado) != 7 {
		t.Fatalf("len(resultado) = %d, want 7 (LIMIT 7)", len(resultado))
	}
}

// TestBuscarProdutos_NenhumMatchDevolveSliceVazio prova a linha "Nenhum
// match" da matriz: termo sem nenhuma correspondência -> slice vazio, nunca
// nil (mesmo padrão de ListarCategorias) — importante porque o handler
// serializa isso como `[]`, nunca `null`.
func TestBuscarProdutos_NenhumMatchDevolveSliceVazio(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	resultado, err := BuscarProdutos(db, "xyzxyz-inexistente")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if resultado == nil {
		t.Fatal("resultado = nil, want slice vazio não-nil")
	}
	if len(resultado) != 0 {
		t.Fatalf("len(resultado) = %d, want 0", len(resultado))
	}
}

// TestBuscarProdutos_CoringasLiteraisNaoViramWildcard prova a linha "Termo
// com %/_ literais" da matriz: um código de Produto contendo `%`/`_`
// literalmente só casa quando o termo buscado contém o mesmo caractere
// literal — sem o escaping (ESCAPE '\”), `_` casaria qualquer caractere e
// `%` casaria qualquer sequência, trazendo falsos positivos.
func TestBuscarProdutos_CoringasLiteraisNaoViramWildcard(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Busca 5")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	comPorcentagem := criarProdutoBusca(t, db, estoque.ID, "Desconto Especial", "50%OFF", categoriaID)
	criarProdutoBusca(t, db, estoque.ID, "Outro Produto Qualquer", "5XOFF", categoriaID)

	resultado, err := BuscarProdutos(db, "50%")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(resultado) != 1 {
		t.Fatalf("len(resultado) = %d, want 1 (só o código com '%%' literal)", len(resultado))
	}
	if resultado[0].ID != comPorcentagem.ID {
		t.Errorf("resultado[0].ID = %q, want %q", resultado[0].ID, comPorcentagem.ID)
	}

	// `_` literal no termo não deveria casar "5XOFF" (onde `_` viraria
	// wildcard de 1 caractere sem escaping) — usa um código próprio com `_`
	// literal para provar o mesmo ponto do lado do `_`.
	comUnderscore := criarProdutoBusca(t, db, estoque.ID, "Produto Com Underscore", "SKU_123", categoriaID)
	resultadoUnderscore, err := BuscarProdutos(db, "SKU_1")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	achou := false
	for _, r := range resultadoUnderscore {
		if r.ID == comUnderscore.ID {
			achou = true
		}
	}
	if !achou {
		t.Fatalf("SKU_123 não encontrado buscando 'SKU_1'")
	}
	// "SKUX123" (sem underscore, com X no lugar) NÃO deveria casar "SKU_1" se
	// o `_` estivesse sendo tratado como caractere literal e não wildcard.
	semUnderscore := criarProdutoBusca(t, db, estoque.ID, "Produto Sem Underscore", "SKUX123", categoriaID)
	resultadoUnderscore2, err := BuscarProdutos(db, "SKU_1")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	for _, r := range resultadoUnderscore2 {
		if r.ID == semUnderscore.ID {
			t.Errorf("SKUX123 casou 'SKU_1' — '_' não foi tratado como caractere literal")
		}
	}
}

// TestBuscarProdutos_CodigoAusenteDevolveNilNoPonteiro prova que um Produto
// sem código cadastrado (coluna opcional) devolve `Codigo == nil` — o
// handler serializa isso como `"codigo": null` no envelope de resposta.
func TestBuscarProdutos_CodigoAusenteDevolveNilNoPonteiro(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Busca 6")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoBusca(t, db, estoque.ID, "Produto Sem Codigo Nenhum", "", categoriaID)

	resultado, err := BuscarProdutos(db, "Produto Sem Codigo")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(resultado) != 1 {
		t.Fatalf("len(resultado) = %d, want 1", len(resultado))
	}
	if resultado[0].Codigo != nil {
		t.Errorf("Codigo = %v, want nil", *resultado[0].Codigo)
	}
}

// TestBuscarProdutos_EmpateDeRankENomeDesempataPorID prova o desempate final
// `p.id ASC` (Review Triage Log de 2026-08-31): dois Produtos com MESMO rank
// (ambos batem só por prefixo em `nome`) e MESMO `nome` não têm ordem
// garantida por `ORDER BY rank, nome` sozinho — sem o desempate por `id`, a
// ordem entre execuções não é determinística, o que pode mudar QUAIS
// Produtos aparecem quando o empate cai na fronteira do `LIMIT 7`. Aqui o
// volume é pequeno (2 Produtos, bem abaixo do limite), então o que esta
// prova é a ORDEM relativa entre eles, sempre ascendente por `id`.
func TestBuscarProdutos_EmpateDeRankENomeDesempataPorID(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro Busca Empate")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	primeiro := criarProdutoBusca(t, db, estoque.ID, "Empate Duplicado", "", categoriaID)
	segundo := criarProdutoBusca(t, db, estoque.ID, "Empate Duplicado", "", categoriaID)

	idsEsperados := []string{primeiro.ID, segundo.ID}
	sort.Strings(idsEsperados)

	resultado, err := BuscarProdutos(db, "empate duplicado")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(resultado) != 2 {
		t.Fatalf("len(resultado) = %d, want 2", len(resultado))
	}
	if resultado[0].ID != idsEsperados[0] || resultado[1].ID != idsEsperados[1] {
		t.Errorf("ordem = [%q, %q], want [%q, %q] (desempate por id ASC)",
			resultado[0].ID, resultado[1].ID, idsEsperados[0], idsEsperados[1])
	}
}

// --- Story 4.5: Identificação de Produto via QR Code / código de barras ----

// TestBuscarProdutoPorCodigo_MatchExatoEncontrado prova a linha "Código
// existente resolvido" da matriz de spec-4-5: um Produto cujo `codigo` é
// EXATAMENTE igual ao valor lido é devolvido com a projeção ProdutoBusca
// completa (id/nome/codigo/categoria).
func TestBuscarProdutoPorCodigo_MatchExatoEncontrado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro PorCodigo 1")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	criarProdutoBusca(t, db, estoque.ID, "Outro Produto Qualquer", "CAB-999", categoriaID)
	alvo := criarProdutoBusca(t, db, estoque.ID, "Cabo Flexível 4mm", "CAB-004", categoriaID)

	resultado, err := BuscarProdutoPorCodigo(db, "CAB-004")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if resultado.ID != alvo.ID {
		t.Errorf("ID = %q, want %q", resultado.ID, alvo.ID)
	}
	if resultado.Nome != "Cabo Flexível 4mm" {
		t.Errorf("Nome = %q, want %q", resultado.Nome, "Cabo Flexível 4mm")
	}
	if resultado.Codigo == nil || *resultado.Codigo != "CAB-004" {
		t.Errorf("Codigo = %v, want CAB-004", resultado.Codigo)
	}
	if resultado.Categoria.ID != categoriaID || resultado.Categoria.Codigo != "04.001" {
		t.Errorf("Categoria = %+v, want id=%q codigo=04.001", resultado.Categoria, categoriaID)
	}
}

// TestBuscarProdutoPorCodigo_CodigoInexistente prova a linha "Código não
// cadastrado" da matriz: nenhum Produto com aquele `codigo` exato ->
// ErrProdutoNaoEncontrado (sql.ErrNoRows colapsado, mesmo padrão de
// ObterProdutoDetalhe).
func TestBuscarProdutoPorCodigo_CodigoInexistente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro PorCodigo 2")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	criarProdutoBusca(t, db, estoque.ID, "Produto Com Outro Codigo", "XYZ-001", categoriaID)

	_, err = BuscarProdutoPorCodigo(db, "NAO-EXISTE-999")
	if !errors.Is(err, ErrProdutoNaoEncontrado) {
		t.Fatalf("erro = %v, want ErrProdutoNaoEncontrado", err)
	}
}

// TestBuscarProdutoPorCodigo_CaseSensitive prova que o match é
// case-sensitive (`WHERE p.codigo = $1`, coerente com o índice único parcial
// `idx_produtos_codigo`): `cab-004` NÃO resolve um Produto cadastrado como
// `CAB-004`.
func TestBuscarProdutoPorCodigo_CaseSensitive(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Canteiro PorCodigo 3")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	alvo := criarProdutoBusca(t, db, estoque.ID, "Cabo Flexível 4mm", "CAB-004", categoriaID)

	resultado, err := BuscarProdutoPorCodigo(db, "CAB-004")
	if err != nil {
		t.Fatalf("match exato deveria funcionar: %v", err)
	}
	if resultado.ID != alvo.ID {
		t.Fatalf("ID = %q, want %q", resultado.ID, alvo.ID)
	}

	_, err = BuscarProdutoPorCodigo(db, "cab-004")
	if !errors.Is(err, ErrProdutoNaoEncontrado) {
		t.Errorf("erro = %v, want ErrProdutoNaoEncontrado ('cab-004' != 'CAB-004', case-sensitive)", err)
	}
}
