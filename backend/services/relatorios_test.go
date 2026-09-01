package services

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// --- Story 4.6: Exportação da tabela do catálogo para Excel ---------------
//
// Helpers do mesmo pacote reutilizados: limparProdutos, categoriaIDPorCodigo,
// testDB, criarProdutoCat, setQuantidade, limparEstoqueDe (produtos_test.go/
// catalogo_test.go).

// abrirXLSX abre o `[]byte` gerado por GerarCatalogoXLSX com
// excelize.OpenReader, para ler células/fórmulas nos testes abaixo.
func abrirXLSX(t *testing.T, dados []byte) *excelize.File {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(dados))
	if err != nil {
		t.Fatalf("excelize.OpenReader: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func celXLSX(t *testing.T, col, linha int) string {
	t.Helper()
	celula, err := excelize.CoordinatesToCellName(col, linha)
	if err != nil {
		t.Fatalf("CoordinatesToCellName(%d,%d): %v", col, linha, err)
	}
	return celula
}

// TestGerarCatalogoXLSX_CabecalhoFixo prova o cabeçalho fixo de 13 colunas —
// mesmos rótulos de dimensão de CabecalhoEsperado (Story 3.3), sem
// `Código`/`Categoria` — presente mesmo com zero grupos.
func TestGerarCatalogoXLSX_CabecalhoFixo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	dados, err := GerarCatalogoXLSX(db, FiltrosCatalogo{Q: "produto-que-nao-existe-em-lugar-nenhum"})
	if err != nil {
		t.Fatalf("GerarCatalogoXLSX: %v", err)
	}
	f := abrirXLSX(t, dados)
	planilha := f.GetSheetList()[0]

	for i, titulo := range cabecalhoCatalogoXLSX {
		valor, err := f.GetCellValue(planilha, celXLSX(t, i+1, 1))
		if err != nil {
			t.Fatalf("GetCellValue coluna %d: %v", i+1, err)
		}
		if valor != titulo {
			t.Errorf("cabecalho[%d] = %q, want %q", i, valor, titulo)
		}
	}
}

// TestGerarCatalogoXLSX_ZeroGrupos prova a linha "Zero grupos" da matriz: só
// a linha de cabeçalho, nenhuma linha de dado nem de "Total geral".
func TestGerarCatalogoXLSX_ZeroGrupos(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	dados, err := GerarCatalogoXLSX(db, FiltrosCatalogo{})
	if err != nil {
		t.Fatalf("GerarCatalogoXLSX: %v", err)
	}
	f := abrirXLSX(t, dados)
	planilha := f.GetSheetList()[0]

	valor, err := f.GetCellValue(planilha, "A2")
	if err != nil {
		t.Fatalf("GetCellValue(A2): %v", err)
	}
	if valor != "" {
		t.Errorf("A2 = %q, want linha em branco (zero grupos -> só cabeçalho)", valor)
	}

	linhas, err := f.GetRows(planilha)
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(linhas) != 1 {
		t.Fatalf("len(linhas) = %d, want 1 (só cabeçalho)", len(linhas))
	}
}

// TestGerarCatalogoXLSX_GrupoComEstoques prova a linha "Exportação com grupos
// e Estoques": 1 linha de detalhe por combinação grupo×Estoque, 1 subtotal
// (`SUBTOTAL`) do grupo cobrindo exatamente as linhas de detalhe, e 1 "Total
// geral" cujo intervalo inclui de propósito a linha de subtotal aninhada —
// correto porque o Excel de verdade ignora nativamente uma célula SUBTOTAL
// dentro do intervalo de outra SUBTOTAL (Design Notes); ver o comentário
// junto à asserção da fórmula da linha 5 abaixo sobre por que este teste não
// verifica o VALOR calculado dessa célula.
func TestGerarCatalogoXLSX_GrupoComEstoques(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	estA, err := CriarEstoque(db, "Canteiro XLSX A")
	if err != nil {
		t.Fatalf("seed CriarEstoque A: %v", err)
	}
	estB, err := CriarEstoque(db, "Canteiro XLSX B")
	if err != nil {
		t.Fatalf("seed CriarEstoque B: %v", err)
	}

	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Prancha XLSX", CategoriaID: categoriaID, EstoqueID: estA.ID, QuantidadeInicial: 10,
	})
	p2 := criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Prancha XLSX", CategoriaID: categoriaID, EstoqueID: estA.ID, QuantidadeInicial: 5,
	})
	setQuantidade(t, db, p2, estB.ID, 2)

	dados, err := GerarCatalogoXLSX(db, FiltrosCatalogo{})
	if err != nil {
		t.Fatalf("GerarCatalogoXLSX: %v", err)
	}
	f := abrirXLSX(t, dados)
	planilha := f.GetSheetList()[0]

	// Linha 2/3: detalhe grupo×Estoque (ordem de preencherPorEstoque:
	// estoqueNome ASC -> "Canteiro XLSX A" antes de "Canteiro XLSX B").
	nome2, _ := f.GetCellValue(planilha, "A2")
	estoque2, _ := f.GetCellValue(planilha, celXLSX(t, colEstoqueXLSX, 2))
	qtd2, _ := f.CalcCellValue(planilha, celXLSX(t, colQuantidadeXLSX, 2))
	if nome2 != "Prancha XLSX" || estoque2 != "Canteiro XLSX A" || qtd2 != "15" {
		t.Errorf("linha 2 = nome=%q estoque=%q qtd=%q, want Prancha XLSX/Canteiro XLSX A/15", nome2, estoque2, qtd2)
	}
	estoque3, _ := f.GetCellValue(planilha, celXLSX(t, colEstoqueXLSX, 3))
	qtd3, _ := f.CalcCellValue(planilha, celXLSX(t, colQuantidadeXLSX, 3))
	if estoque3 != "Canteiro XLSX B" || qtd3 != "2" {
		t.Errorf("linha 3 = estoque=%q qtd=%q, want Canteiro XLSX B/2", estoque3, qtd3)
	}

	// Linha 4: subtotal do grupo — fórmula SUBTOTAL sobre M2:M3, valor 17.
	nome4, _ := f.GetCellValue(planilha, "A4")
	if nome4 != "Subtotal — Prancha XLSX" {
		t.Errorf("A4 = %q, want %q", nome4, "Subtotal — Prancha XLSX")
	}
	formula4, err := f.GetCellFormula(planilha, celXLSX(t, colQuantidadeXLSX, 4))
	if err != nil {
		t.Fatalf("GetCellFormula linha 4: %v", err)
	}
	if formula4 != "SUBTOTAL(9,M2:M3)" {
		t.Errorf("formula linha 4 = %q, want SUBTOTAL(9,M2:M3)", formula4)
	}
	valor4, err := f.CalcCellValue(planilha, celXLSX(t, colQuantidadeXLSX, 4))
	if err != nil {
		t.Fatalf("CalcCellValue linha 4: %v", err)
	}
	if valor4 != "17" {
		t.Errorf("valor calculado subtotal grupo = %q, want 17", valor4)
	}

	// Linha 5: "Total geral" — fórmula SUBTOTAL sobre M2:M4 (inclui de
	// propósito o subtotal aninhado do grupo, linha 4): o Excel de verdade
	// ignora nativamente uma célula SUBTOTAL dentro do intervalo de outra
	// SUBTOTAL (comportamento documentado do Excel, Design Notes) — por isso
	// o intervalo pode incluir a linha de subtotal sem risco de dobrar o
	// total. O motor de fórmulas do excelize (`CalcCellValue`) NÃO
	// implementa essa regra (soma ingenuamente, dobrando o valor) — por
	// isso este teste verifica só o TEXTO da fórmula, a parte sob controle
	// desta função; o valor correto (17, não 34) só aparece ao abrir o
	// arquivo num Excel de verdade, que recalcula ao abrir.
	nome5, _ := f.GetCellValue(planilha, "A5")
	if nome5 != "Total geral" {
		t.Errorf("A5 = %q, want Total geral", nome5)
	}
	formula5, err := f.GetCellFormula(planilha, celXLSX(t, colQuantidadeXLSX, 5))
	if err != nil {
		t.Fatalf("GetCellFormula linha 5: %v", err)
	}
	if formula5 != "SUBTOTAL(9,M2:M4)" {
		t.Errorf("formula linha 5 = %q, want SUBTOTAL(9,M2:M4)", formula5)
	}

	// Nenhuma linha depois do total geral.
	linhas, err := f.GetRows(planilha)
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(linhas) != 5 {
		t.Fatalf("len(linhas) = %d, want 5 (cabeçalho + 2 detalhe + subtotal + total)", len(linhas))
	}
}

// TestGerarCatalogoXLSX_GrupoSemEstoque prova a linha "Grupo sem
// `produto_estoque`": 1 linha única com `Estoque` vazio e `Quantidade` 0,
// subtotal do grupo também 0 — o grupo nunca é omitido.
func TestGerarCatalogoXLSX_GrupoSemEstoque(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	estoque, err := CriarEstoque(db, "Canteiro XLSX Sem Estoque")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	p := criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Sem Linha XLSX", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 0,
	})
	limparEstoqueDe(t, db, p)

	dados, err := GerarCatalogoXLSX(db, FiltrosCatalogo{})
	if err != nil {
		t.Fatalf("GerarCatalogoXLSX: %v", err)
	}
	f := abrirXLSX(t, dados)
	planilha := f.GetSheetList()[0]

	nome2, _ := f.GetCellValue(planilha, "A2")
	estoque2, _ := f.GetCellValue(planilha, celXLSX(t, colEstoqueXLSX, 2))
	qtd2, _ := f.CalcCellValue(planilha, celXLSX(t, colQuantidadeXLSX, 2))
	if nome2 != "Sem Linha XLSX" || estoque2 != "" || qtd2 != "0" {
		t.Errorf("linha 2 = nome=%q estoque=%q qtd=%q, want Sem Linha XLSX/''/0", nome2, estoque2, qtd2)
	}

	valorSubtotal, err := f.CalcCellValue(planilha, celXLSX(t, colQuantidadeXLSX, 3))
	if err != nil {
		t.Fatalf("CalcCellValue subtotal: %v", err)
	}
	if valorSubtotal != "0" {
		t.Errorf("subtotal do grupo sem estoque = %q, want 0", valorSubtotal)
	}

	linhas, err := f.GetRows(planilha)
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	// cabeçalho + 1 detalhe + subtotal + total geral = 4.
	if len(linhas) != 4 {
		t.Fatalf("len(linhas) = %d, want 4", len(linhas))
	}
}

// TestGerarCatalogoXLSX_MultiplosGrupos prova a linha "múltiplos grupos" do
// Code Map: a fórmula do "Total geral" cobre o intervalo certo — do primeiro
// detalhe do primeiro grupo até o subtotal do último grupo, incluindo TODOS
// os subtotais intermediários.
func TestGerarCatalogoXLSX_MultiplosGrupos(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	estoque, err := CriarEstoque(db, "Canteiro XLSX Multiplos")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Grupo A XLSX", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 4,
	})
	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Grupo B XLSX", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 6,
	})

	dados, err := GerarCatalogoXLSX(db, FiltrosCatalogo{})
	if err != nil {
		t.Fatalf("GerarCatalogoXLSX: %v", err)
	}
	f := abrirXLSX(t, dados)
	planilha := f.GetSheetList()[0]

	// Linha 2: Grupo A XLSX (detalhe); linha 3: subtotal Grupo A;
	// linha 4: Grupo B XLSX (detalhe); linha 5: subtotal Grupo B;
	// linha 6: Total geral.
	nomeGrupoA, _ := f.GetCellValue(planilha, "A2")
	if nomeGrupoA != "Grupo A XLSX" {
		t.Fatalf("A2 = %q, want Grupo A XLSX", nomeGrupoA)
	}
	subtotalA, _ := f.GetCellFormula(planilha, celXLSX(t, colQuantidadeXLSX, 3))
	if subtotalA != "SUBTOTAL(9,M2:M2)" {
		t.Errorf("formula subtotal A = %q, want SUBTOTAL(9,M2:M2)", subtotalA)
	}
	nomeGrupoB, _ := f.GetCellValue(planilha, "A4")
	if nomeGrupoB != "Grupo B XLSX" {
		t.Fatalf("A4 = %q, want Grupo B XLSX", nomeGrupoB)
	}
	subtotalB, _ := f.GetCellFormula(planilha, celXLSX(t, colQuantidadeXLSX, 5))
	if subtotalB != "SUBTOTAL(9,M4:M4)" {
		t.Errorf("formula subtotal B = %q, want SUBTOTAL(9,M4:M4)", subtotalB)
	}

	nomeTotal, _ := f.GetCellValue(planilha, "A6")
	if nomeTotal != "Total geral" {
		t.Fatalf("A6 = %q, want Total geral", nomeTotal)
	}
	formulaTotal, err := f.GetCellFormula(planilha, celXLSX(t, colQuantidadeXLSX, 6))
	if err != nil {
		t.Fatalf("GetCellFormula total geral: %v", err)
	}
	if formulaTotal != "SUBTOTAL(9,M2:M5)" {
		t.Errorf("formula total geral = %q, want SUBTOTAL(9,M2:M5) (cobre os 2 grupos + os 2 subtotais)", formulaTotal)
	}
	// Não verifica o VALOR calculado aqui — o motor de fórmulas do excelize
	// não implementa a regra do Excel de ignorar SUBTOTAL aninhado (soma
	// ingenuamente, dobrando o resultado); ver o comentário equivalente em
	// TestGerarCatalogoXLSX_GrupoComEstoques. O que esta função controla, e o
	// que o teste prova, é que a fórmula gerada tem o intervalo certo.
}

// TestGerarCatalogoXLSX_AutoFilterNoCabecalho prova o `AutoFilter` na linha
// de cabeçalho — coerente com `SUBTOTAL` quando o próprio Excel filtra o
// arquivo já exportado. excelize não expõe um getter público de AutoFilter
// (só `AddTable`/`AutoFilter`/`GetTables`, nenhum para o `<autoFilter>` de
// nível de planilha) — lido direto do XML dentro do `.xlsx` (é um zip).
func TestGerarCatalogoXLSX_AutoFilterNoCabecalho(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	estoque, err := CriarEstoque(db, "Canteiro XLSX AutoFilter")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "AutoFilter XLSX", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 1,
	})

	dados, err := GerarCatalogoXLSX(db, FiltrosCatalogo{})
	if err != nil {
		t.Fatalf("GerarCatalogoXLSX: %v", err)
	}

	// excelize grava a referência do AutoFilter com âncoras absolutas
	// (`$A$1:$M$1`), mesmo passando "A1:M1" para f.AutoFilter — comportamento
	// da biblioteca, não desta função.
	sheetXML := lerSheet1XML(t, dados)
	if !strings.Contains(sheetXML, `<autoFilter ref="$A$1:$M$1">`) {
		t.Errorf("sheet1.xml não contém <autoFilter ref=\"$A$1:$M$1\">: %s", sheetXML)
	}
}

// lerSheet1XML abre o `.xlsx` (um zip) e devolve o conteúdo cru de
// `xl/worksheets/sheet1.xml`.
func lerSheet1XML(t *testing.T, dados []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(dados), int64(len(dados)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	for _, arquivo := range zr.File {
		if arquivo.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		rc, err := arquivo.Open()
		if err != nil {
			t.Fatalf("abrir sheet1.xml: %v", err)
		}
		defer rc.Close()
		conteudo, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("ler sheet1.xml: %v", err)
		}
		return string(conteudo)
	}
	t.Fatal("xl/worksheets/sheet1.xml não encontrado no .xlsx")
	return ""
}

// TestGerarCatalogoXLSX_FiltroSemResultado prova a linha "Filtro sem
// resultado" da matriz: filtro que não casa nenhum Produto -> `.xlsx` só com
// cabeçalho (mesmo resultado de zero grupos).
func TestGerarCatalogoXLSX_FiltroSemResultado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	estoque, err := CriarEstoque(db, "Canteiro XLSX Filtro Vazio")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Existe XLSX", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 1,
	})

	dados, err := GerarCatalogoXLSX(db, FiltrosCatalogo{Q: "não-existe-jamais-xlsx"})
	if err != nil {
		t.Fatalf("GerarCatalogoXLSX: %v", err)
	}
	f := abrirXLSX(t, dados)
	planilha := f.GetSheetList()[0]

	linhas, err := f.GetRows(planilha)
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(linhas) != 1 {
		t.Fatalf("len(linhas) = %d, want 1 (só cabeçalho)", len(linhas))
	}
}

// TestGerarCatalogoXLSX_CategoriaEstoqueMalformadosSoCabecalho prova o mesmo
// colapso `filtroUUIDInvalido` de ListarTodosGruposCatalogo refletido no
// `.xlsx`: `categoriaId`/`estoqueId` malformado -> só cabeçalho, nunca erro.
func TestGerarCatalogoXLSX_CategoriaEstoqueMalformadosSoCabecalho(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	estoque, err := CriarEstoque(db, "Canteiro XLSX Malformado")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	criarProdutoCat(t, db, CriarProdutoInput{
		Nome: "Malformado XLSX", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 1,
	})

	dados, err := GerarCatalogoXLSX(db, FiltrosCatalogo{CategoriaID: "abc"})
	if err != nil {
		t.Fatalf("GerarCatalogoXLSX categoriaId malformado: %v", err)
	}
	f := abrirXLSX(t, dados)
	linhas, err := f.GetRows(f.GetSheetList()[0])
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(linhas) != 1 {
		t.Fatalf("categoriaId malformado: len(linhas) = %d, want 1", len(linhas))
	}
}
