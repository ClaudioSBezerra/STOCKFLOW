package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// categoriaNomePorCodigo devolve o `nome` de uma das 25 categorias fixas de
// seed (migração 000010), pelo `codigo` — a importação casa a coluna
// "Categoria" da planilha por NOME (spec-3-3, Design Notes), não por código.
func categoriaNomePorCodigo(t *testing.T, db *sql.DB, codigo string) string {
	t.Helper()
	var nome string
	if err := db.QueryRow(`SELECT nome FROM categorias WHERE codigo = $1`, codigo).Scan(&nome); err != nil {
		t.Fatalf("falha ao buscar categoria %q: %v", codigo, err)
	}
	return nome
}

// linhaBase monta uma linha de dado da planilha (16 células, mesma ordem de
// CabecalhoEsperado) com as 5 dimensões e observações em branco — os testes
// que precisam de uma dimensão preenchida sobrescrevem os índices
// colComprimentoValor/colComprimentoUnidade (etc.) diretamente no slice
// devolvido. `codigoFornecedor` é a célula "Código" da planilha — grava em
// `produtos.codigo_fornecedor`, nunca no `codigo` interno (sempre gerado
// pelo servidor, correção de bug 2026-11-12).
func linhaBase(nome, codigoFornecedor, categoria, quantidade, estoque string) []string {
	linha := make([]string, numeroColunas)
	linha[colNome] = nome
	linha[colCodigo] = codigoFornecedor
	linha[colCategoria] = categoria
	linha[colQuantidade] = quantidade
	linha[colEstoque] = estoque
	return linha
}

func criarUsuarioImportacao(t *testing.T, db *sql.DB, email string) string {
	t.Helper()
	return criarUsuarioParaLogin(t, db, email, "senha-123456", true, true)
}

// TestCriarImportacao_SucessoCompleto prova a I/O Matrix "Importação válida
// completa": todas as linhas de dado válidas -> `importacoes.status` termina
// `concluida`, um Produto por linha, e o Estoque referenciado (ainda
// inexistente) é criado automaticamente e reaproveitado pela segunda linha
// que cita o mesmo nome.
func TestCriarImportacao_SucessoCompleto(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-sucesso@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Importado Um", "SKU-IMP-1", categoria, "5", "Canteiro Importação Sucesso"),
		linhaBase("Produto Importado Dois", "SKU-IMP-2", categoria, "3", "Canteiro Importação Sucesso"),
	}

	importacao, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if importacao.Status != "concluida" {
		t.Errorf("status = %q, want concluida", importacao.Status)
	}
	if importacao.TotalLinhas != 2 {
		t.Errorf("total_linhas = %d, want 2", importacao.TotalLinhas)
	}
	if importacao.ProximaLinhaPendente != nil {
		t.Errorf("ProximaLinhaPendente = %v, want nil (importação concluida, nenhuma linha pendente sobra)", *importacao.ProximaLinhaPendente)
	}
	if relatorio.Criados != 2 || relatorio.Rejeitados != 0 {
		t.Errorf("relatorio = %+v, want Criados=2 Rejeitados=0", relatorio)
	}
	if len(relatorio.LinhasRejeitadas) != 0 {
		t.Errorf("LinhasRejeitadas = %+v, want vazio", relatorio.LinhasRejeitadas)
	}
	if n := contarProdutos(t, db); n != 2 {
		t.Errorf("linhas em produtos = %d, want 2", n)
	}
	if n := contarEstoques(t, db); n != 1 {
		t.Errorf("linhas em estoques = %d, want 1 (Estoque novo reaproveitado pelas 2 linhas)", n)
	}
	if n := contarProdutoEstoque(t, db); n != 2 {
		t.Errorf("linhas em produto_estoque = %d, want 2", n)
	}
}

// TestCriarImportacao_PrecoUnitario prova a coluna nova "Preço Unitário"
// (feedback Ferreira Costa, direção, 2026-11-11): célula vazia grava NULL,
// célula preenchida grava o valor exato, e reimportar a MESMA linha por
// código (caminho de atualização) sobrescreve o preço, igual a qualquer
// outro campo editável da planilha.
func TestCriarImportacao_PrecoUnitario(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-preco@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	semPreco := linhaBase("Produto Importado Sem Preco", "SKU-PRECO-1", categoria, "1", "Canteiro Preço")
	comPreco := linhaBase("Produto Importado Com Preco", "SKU-PRECO-2", categoria, "1", "Canteiro Preço")
	comPreco[colPrecoUnitario] = "1234.56"

	linhas := [][]string{CabecalhoEsperado, semPreco, comPreco}
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if relatorio.Criados != 2 || relatorio.Rejeitados != 0 {
		t.Fatalf("relatorio = %+v, want Criados=2 Rejeitados=0", relatorio)
	}

	var precoSemID, precoComID sql.NullFloat64
	if err := db.QueryRow(`SELECT preco_unitario FROM produtos WHERE codigo_fornecedor = 'SKU-PRECO-1'`).Scan(&precoSemID); err != nil {
		t.Fatal(err)
	}
	if !precoSemID.Valid || precoSemID.Float64 != 0 {
		t.Errorf("SKU-PRECO-1: preco_unitario = %v, want 0 (vazio vira zero em produto novo)", precoSemID)
	}
	if err := db.QueryRow(`SELECT preco_unitario FROM produtos WHERE codigo_fornecedor = 'SKU-PRECO-2'`).Scan(&precoComID); err != nil {
		t.Fatal(err)
	}
	if !precoComID.Valid || precoComID.Float64 != 1234.56 {
		t.Errorf("SKU-PRECO-2: preco_unitario = %v, want 1234.56", precoComID)
	}

	// Reimportar o MESMO código com um preço diferente -> caminho de
	// atualização, sobrescreve.
	atualizacao := linhaBase("Produto Importado Com Preco", "SKU-PRECO-2", categoria, "1", "Canteiro Preço")
	atualizacao[colPrecoUnitario] = "9999.00"
	_, relatorioAtt, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha2.xlsx", depositoImp(t, db, empresaTeste), [][]string{CabecalhoEsperado, atualizacao})
	if err != nil {
		t.Fatalf("CriarImportacao (atualização) erro inesperado: %v", err)
	}
	if relatorioAtt.Rejeitados != 0 {
		t.Fatalf("relatorio atualização = %+v, want Rejeitados=0", relatorioAtt)
	}
	if err := db.QueryRow(`SELECT preco_unitario FROM produtos WHERE codigo_fornecedor = 'SKU-PRECO-2'`).Scan(&precoComID); err != nil {
		t.Fatal(err)
	}
	if !precoComID.Valid || precoComID.Float64 != 9999.00 {
		t.Errorf("SKU-PRECO-2 após atualização: preco_unitario = %v, want 9999.00", precoComID)
	}
	if n := contarProdutos(t, db); n != 2 {
		t.Errorf("linhas em produtos = %d, want 2 (atualização não deveria duplicar)", n)
	}
}

// TestCriarImportacao_PrecoUnitarioInvalidoRejeitaSoALinha prova que um
// preço negativo ou não-numérico rejeita SÓ a linha (as demais seguem
// normalmente) — mesmo padrão de TestCriarImportacao_LinhaComDimensaoIncompleta.
func TestCriarImportacao_PrecoUnitarioInvalidoRejeitaSoALinha(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-preco-invalido@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	linhaNegativa := linhaBase("Produto Preco Negativo Rejeitado", "SKU-PRECO-NEG", categoria, "1", "Canteiro Preço Inválido")
	linhaNegativa[colPrecoUnitario] = "-1"

	linhas := [][]string{
		CabecalhoEsperado,
		linhaNegativa,
		linhaBase("Produto Preco Valido Dois", "SKU-PRECO-OK", categoria, "1", "Canteiro Preço Inválido"),
	}
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if relatorio.Criados != 1 || relatorio.Rejeitados != 1 {
		t.Fatalf("relatorio = %+v, want Criados=1 Rejeitados=1", relatorio)
	}
	if len(relatorio.LinhasRejeitadas) != 1 || !strings.Contains(relatorio.LinhasRejeitadas[0].Erro, "maior ou igual a zero") {
		t.Errorf("LinhasRejeitadas = %+v, want citar 'maior ou igual a zero'", relatorio.LinhasRejeitadas)
	}
}

// TestCriarImportacao_LinhaComDimensaoIncompleta prova a I/O Matrix "Linha
// com dimensão sem unidade": só a linha com o par incompleto é marcada
// `rejeitada`; as demais N-1 são processadas normalmente.
func TestCriarImportacao_LinhaComDimensaoIncompleta(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-dimensao@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	linhaInvalida := linhaBase("Produto Dimensao Incompleta", "SKU-DIM", categoria, "1", "Canteiro Dimensao")
	linhaInvalida[colComprimentoValor] = "6"
	// colComprimentoUnidade deliberadamente em branco.

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Valido Um", "SKU-V1", categoria, "1", "Canteiro Dimensao"),
		linhaInvalida,
		linhaBase("Produto Valido Dois", "SKU-V2", categoria, "1", "Canteiro Dimensao"),
	}

	importacao, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if importacao.Status != "concluida" {
		t.Errorf("status = %q, want concluida", importacao.Status)
	}
	if relatorio.Criados != 2 {
		t.Errorf("Criados = %d, want 2", relatorio.Criados)
	}
	if relatorio.Rejeitados != 1 {
		t.Errorf("Rejeitados = %d, want 1", relatorio.Rejeitados)
	}
	if len(relatorio.LinhasRejeitadas) != 1 {
		t.Fatalf("LinhasRejeitadas = %+v, want 1 item", relatorio.LinhasRejeitadas)
	}
	// header=1, primeira linha de dado=2 -> a linha inválida é a 2ª linha de
	// dado -> numero_linha real = 3.
	if relatorio.LinhasRejeitadas[0].Linha != 3 {
		t.Errorf("Linha rejeitada = %d, want 3", relatorio.LinhasRejeitadas[0].Linha)
	}
	if !strings.Contains(relatorio.LinhasRejeitadas[0].Erro, "comprimento") {
		t.Errorf("Erro = %q, want citar 'comprimento'", relatorio.LinhasRejeitadas[0].Erro)
	}
	if n := contarProdutos(t, db); n != 2 {
		t.Errorf("linhas em produtos = %d, want 2", n)
	}
}

// TestCriarImportacao_CategoriaInexistente prova a I/O Matrix "Linha com
// categoria não encontrada": a linha é rejeitada citando o valor da
// planilha, e o Estoque referenciado por ELA não é criado.
func TestCriarImportacao_CategoriaInexistente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-categoria@empresa.com")

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Categoria Ruim", "SKU-CAT", "Categoria Totalmente Inexistente", "1", "Estoque Nao Deveria Existir"),
	}

	importacao, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if importacao.Status != "concluida" {
		t.Errorf("status = %q, want concluida", importacao.Status)
	}
	if relatorio.Criados != 0 || relatorio.Rejeitados != 1 {
		t.Errorf("relatorio = %+v, want Criados=0 Rejeitados=1", relatorio)
	}
	if !strings.Contains(relatorio.LinhasRejeitadas[0].Erro, "Categoria Totalmente Inexistente") {
		t.Errorf("Erro = %q, want citar o valor da planilha", relatorio.LinhasRejeitadas[0].Erro)
	}
	if n := contarProdutos(t, db); n != 0 {
		t.Errorf("linhas em produtos = %d, want 0", n)
	}
	var existeEstoque bool
	if err := db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM estoques WHERE nome = 'Estoque Nao Deveria Existir')`,
	).Scan(&existeEstoque); err != nil {
		t.Fatalf("falha ao checar estoque: %v", err)
	}
	if existeEstoque {
		t.Error("Estoque foi criado para uma linha rejeitada por categoria inexistente")
	}
}

// TestCriarImportacao_EstoqueNovoReaproveitadoEntreLinhas: a coluna "Estoque"
// é ignorada (nomes distintos/preenchidos) — nenhum depósito é criado e todas
// as linhas gravam no MESMO depósito escolhido.
func TestCriarImportacao_EstoqueNovoReaproveitadoEntreLinhas(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-estoque-dup@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.002")

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Deposito B Um", "SKU-DB1", categoria, "1", "  Depósito B  "),
		linhaBase("Produto Deposito B Dois", "SKU-DB2", categoria, "1", "Outro Nome Qualquer"),
	}

	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if relatorio.Criados != 2 {
		t.Fatalf("Criados = %d, want 2", relatorio.Criados)
	}
	if n := contarEstoques(t, db); n != 1 {
		t.Fatalf("linhas em estoques = %d, want 1", n)
	}

	var estoqueIDs []string
	rows, err := db.Query(`
		SELECT DISTINCT pe.estoque_id FROM produto_estoque pe
		JOIN produtos p ON p.id = pe.produto_id
		WHERE p.codigo_fornecedor IN ('SKU-DB1', 'SKU-DB2')`)
	if err != nil {
		t.Fatalf("query estoque_id: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan estoque_id: %v", err)
		}
		estoqueIDs = append(estoqueIDs, id)
	}
	if len(estoqueIDs) != 1 {
		t.Errorf("estoque_id distintos entre as 2 linhas = %v, want exatamente 1 valor", estoqueIDs)
	}
}

// TestContinuarImportacao_SoProcessaLinhasPendentes prova a I/O Matrix
// "Importação retomada": ContinuarImportacao processa só as linhas
// `pendente`/`processando`, sem reprocessar as já `criado`/`rejeitada`.
//
// Como CriarImportacao processa tudo sincronamente até o fim, esta suíte
// simula uma interrupção manipulando o estado diretamente: após uma
// importação completa (3 linhas, todas `criado`), uma das linhas é
// resetada para `pendente` (e a importação de volta para `em_andamento`) —
// só ESSA linha deve ser reprocessada. Desde a Story 3.4, reprocessar uma
// linha cujo `código` já existe (o próprio Produto que ELA MESMA criou da
// primeira vez, já que o código não muda entre as duas passadas) casa o
// match por código e vira uma ATUALIZAÇÃO do mesmo Produto — nunca um
// Produto novo (I/O Matrix "Duas linhas... mesmo código", spec-3-4); as
// outras duas linhas devem permanecer com o `produto_id` original, intocadas.
func TestContinuarImportacao_SoProcessaLinhasPendentes(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-continuar@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Continuar Um", "SKU-CONT-1", categoria, "1", "Canteiro Continuar"),
		linhaBase("Produto Continuar Dois", "SKU-CONT-2", categoria, "1", "Canteiro Continuar"),
		linhaBase("Produto Continuar Tres", "SKU-CONT-3", categoria, "1", "Canteiro Continuar"),
	}

	importacao, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if relatorio.Criados != 3 {
		t.Fatalf("Criados = %d, want 3", relatorio.Criados)
	}

	var linhaID4, produtoIDOriginal1, produtoIDOriginal2, produtoIDOriginal3 string
	if err := db.QueryRow(
		`SELECT id FROM importacao_linhas WHERE importacao_id = $1 AND numero_linha = 4`, importacao.ID,
	).Scan(&linhaID4); err != nil {
		t.Fatalf("buscar linha 4: %v", err)
	}
	if err := db.QueryRow(
		`SELECT produto_id FROM importacao_linhas WHERE importacao_id = $1 AND numero_linha = 2`, importacao.ID,
	).Scan(&produtoIDOriginal1); err != nil {
		t.Fatalf("buscar produto_id linha 2: %v", err)
	}
	if err := db.QueryRow(
		`SELECT produto_id FROM importacao_linhas WHERE importacao_id = $1 AND numero_linha = 3`, importacao.ID,
	).Scan(&produtoIDOriginal2); err != nil {
		t.Fatalf("buscar produto_id linha 3: %v", err)
	}
	if err := db.QueryRow(
		`SELECT produto_id FROM importacao_linhas WHERE importacao_id = $1 AND numero_linha = 4`, importacao.ID,
	).Scan(&produtoIDOriginal3); err != nil {
		t.Fatalf("buscar produto_id linha 4: %v", err)
	}

	// Simula interrupção: linha 4 volta a pendente, importação volta a
	// em_andamento.
	if _, err := db.Exec(`UPDATE importacao_linhas SET status = 'pendente' WHERE id = $1`, linhaID4); err != nil {
		t.Fatalf("resetar linha 4 para pendente: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE importacoes SET status = 'em_andamento', concluido_em = NULL WHERE id = $1`, importacao.ID,
	); err != nil {
		t.Fatalf("resetar importação para em_andamento: %v", err)
	}

	// Antes de continuar: a importação "parada" deve apontar exatamente para
	// a linha 4 (a única pendente) — prova o mecanismo por trás do banner de
	// retomada do frontend (spec-3-3, review pass), não um valor qualquer.
	ultimaAntesDeContinuar, _, err := ObterUltimaImportacao(db, empresaTeste)
	if err != nil {
		t.Fatalf("ObterUltimaImportacao antes de continuar: %v", err)
	}
	if ultimaAntesDeContinuar == nil || ultimaAntesDeContinuar.ProximaLinhaPendente == nil {
		t.Fatalf("ProximaLinhaPendente ausente antes de continuar: %+v", ultimaAntesDeContinuar)
	}
	if *ultimaAntesDeContinuar.ProximaLinhaPendente != 4 {
		t.Errorf("ProximaLinhaPendente = %d, want 4", *ultimaAntesDeContinuar.ProximaLinhaPendente)
	}

	produtosAntes := contarProdutos(t, db)

	importacaoContinuada, relatorioContinuado, err := ContinuarImportacao(db, empresaTeste, importacao.ID)
	if err != nil {
		t.Fatalf("ContinuarImportacao erro inesperado: %v", err)
	}
	if importacaoContinuada.Status != "concluida" {
		t.Errorf("status = %q, want concluida", importacaoContinuada.Status)
	}
	if importacaoContinuada.ProximaLinhaPendente != nil {
		t.Errorf("ProximaLinhaPendente = %v, want nil após concluir", *importacaoContinuada.ProximaLinhaPendente)
	}
	if relatorioContinuado.Criados != 2 {
		t.Errorf("Criados = %d, want 2 (as 2 linhas originais nunca reprocessadas)", relatorioContinuado.Criados)
	}
	if relatorioContinuado.Atualizados != 1 {
		t.Errorf("Atualizados = %d, want 1 (a linha 4 reprocessada casa por código com o Produto que ela mesma criou)", relatorioContinuado.Atualizados)
	}
	if n := contarProdutos(t, db); n != produtosAntes {
		t.Errorf("linhas em produtos após continuar = %d, want %d (reprocessar por código nunca cria Produto novo)", n, produtosAntes)
	}

	var produtoIDNovo1, produtoIDNovo2, produtoIDLinha4Depois string
	if err := db.QueryRow(
		`SELECT produto_id FROM importacao_linhas WHERE importacao_id = $1 AND numero_linha = 2`, importacao.ID,
	).Scan(&produtoIDNovo1); err != nil {
		t.Fatalf("buscar produto_id linha 2 após continuar: %v", err)
	}
	if err := db.QueryRow(
		`SELECT produto_id FROM importacao_linhas WHERE importacao_id = $1 AND numero_linha = 3`, importacao.ID,
	).Scan(&produtoIDNovo2); err != nil {
		t.Fatalf("buscar produto_id linha 3 após continuar: %v", err)
	}
	if err := db.QueryRow(
		`SELECT produto_id FROM importacao_linhas WHERE importacao_id = $1 AND numero_linha = 4`, importacao.ID,
	).Scan(&produtoIDLinha4Depois); err != nil {
		t.Fatalf("buscar produto_id linha 4 após continuar: %v", err)
	}
	if produtoIDNovo1 != produtoIDOriginal1 {
		t.Errorf("linha 2 foi reprocessada indevidamente: produto_id mudou de %q para %q", produtoIDOriginal1, produtoIDNovo1)
	}
	if produtoIDNovo2 != produtoIDOriginal2 {
		t.Errorf("linha 3 foi reprocessada indevidamente: produto_id mudou de %q para %q", produtoIDOriginal2, produtoIDNovo2)
	}
	if produtoIDLinha4Depois != produtoIDOriginal3 {
		t.Errorf("linha 4 (resetada para pendente) mudou de produto_id de %q para %q — deveria atualizar o MESMO Produto (match por código), não criar outro", produtoIDOriginal3, produtoIDLinha4Depois)
	}
}

// TestObterUltimaImportacao_SemNenhumaImportacao prova que, sem nenhuma
// importação registrada, ObterUltimaImportacao devolve (nil, zero, nil) —
// nunca um erro.
func TestObterUltimaImportacao_SemNenhumaImportacao(t *testing.T) {
	db := testDB(t)

	importacao, relatorio, err := ObterUltimaImportacao(db, empresaTeste)
	if err != nil {
		t.Fatalf("ObterUltimaImportacao erro inesperado: %v", err)
	}
	if importacao != nil {
		t.Errorf("importacao = %+v, want nil", importacao)
	}
	if relatorio.Criados != 0 || relatorio.Rejeitados != 0 || len(relatorio.LinhasRejeitadas) != 0 {
		t.Errorf("relatorio = %+v, want zero value", relatorio)
	}
}

// TestObterUltimaImportacao_DevolveAMaisRecente prova a ordenação
// `iniciado_em DESC` de ObterUltimaImportacao: entre duas importações, a mais
// recente é a devolvida.
func TestObterUltimaImportacao_DevolveAMaisRecente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-ultima@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	linhasA := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Ultima A", "SKU-ULT-A", categoria, "1", "Canteiro Ultima"),
	}
	importacaoA, _, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha-a.xlsx", depositoImp(t, db, empresaTeste), linhasA)
	if err != nil {
		t.Fatalf("CriarImportacao A: %v", err)
	}

	linhasB := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Ultima B", "SKU-ULT-B", categoria, "1", "Canteiro Ultima"),
	}
	importacaoB, _, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha-b.xlsx", depositoImp(t, db, empresaTeste), linhasB)
	if err != nil {
		t.Fatalf("CriarImportacao B: %v", err)
	}

	// Garante ordenação determinística mesmo se as duas importações
	// caírem no mesmo timestamp (relógio de baixa resolução).
	if _, err := db.Exec(
		`UPDATE importacoes SET iniciado_em = now() WHERE id = $1`, importacaoB.ID,
	); err != nil {
		t.Fatalf("forçar iniciado_em de B mais recente: %v", err)
	}

	ultima, _, err := ObterUltimaImportacao(db, empresaTeste)
	if err != nil {
		t.Fatalf("ObterUltimaImportacao erro inesperado: %v", err)
	}
	if ultima == nil {
		t.Fatal("ultima = nil, want a importação B")
	}
	if ultima.ID != importacaoB.ID {
		t.Errorf("ultima.ID = %q, want %q (B, mais recente); importacaoA.ID=%q", ultima.ID, importacaoB.ID, importacaoA.ID)
	}
	if ultima.ProximaLinhaPendente != nil {
		t.Errorf("ProximaLinhaPendente = %v, want nil (B foi totalmente processada)", *ultima.ProximaLinhaPendente)
	}
}

// TestObterUltimaImportacao_ProximaLinhaPendente prova o MIN() de
// proximaLinhaPendente: com múltiplas linhas ainda pendente/processando, o
// valor devolvido é a MENOR delas — o número real que leva a pessoa direto à
// primeira célula que precisa de atenção, não uma qualquer.
func TestObterUltimaImportacao_ProximaLinhaPendente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-proxima-linha@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Proxima Um", "SKU-PL-1", categoria, "1", "Canteiro Proxima Linha"),     // numero_linha 2
		linhaBase("Produto Proxima Dois", "SKU-PL-2", categoria, "1", "Canteiro Proxima Linha"),   // numero_linha 3
		linhaBase("Produto Proxima Tres", "SKU-PL-3", categoria, "1", "Canteiro Proxima Linha"),   // numero_linha 4
		linhaBase("Produto Proxima Quatro", "SKU-PL-4", categoria, "1", "Canteiro Proxima Linha"), // numero_linha 5
		linhaBase("Produto Proxima Cinco", "SKU-PL-5", categoria, "1", "Canteiro Proxima Linha"),  // numero_linha 6
	}
	importacao, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if relatorio.Criados != 5 {
		t.Fatalf("Criados = %d, want 5", relatorio.Criados)
	}

	// Reseta as linhas 4 e 6 para pendente (a 2 é deliberadamente OMITIDA —
	// prova que o MIN() ignora linhas já terminais mesmo quando não é a
	// primeira da tabela).
	if _, err := db.Exec(
		`UPDATE importacao_linhas SET status = 'pendente' WHERE importacao_id = $1 AND numero_linha IN (4, 6)`,
		importacao.ID,
	); err != nil {
		t.Fatalf("resetar linhas 4 e 6 para pendente: %v", err)
	}
	if _, err := db.Exec(`UPDATE importacoes SET status = 'em_andamento' WHERE id = $1`, importacao.ID); err != nil {
		t.Fatalf("resetar importação para em_andamento: %v", err)
	}

	ultima, _, err := ObterUltimaImportacao(db, empresaTeste)
	if err != nil {
		t.Fatalf("ObterUltimaImportacao erro inesperado: %v", err)
	}
	if ultima == nil {
		t.Fatal("ultima = nil")
	}
	if ultima.ProximaLinhaPendente == nil {
		t.Fatal("ProximaLinhaPendente = nil, want 4 (MIN entre 4 e 6)")
	}
	if *ultima.ProximaLinhaPendente != 4 {
		t.Errorf("ProximaLinhaPendente = %d, want 4 (MIN entre 4 e 6)", *ultima.ProximaLinhaPendente)
	}
}

// TestValidarDimensaoLinha_RejeitaNaoFinito prova que strconv.ParseFloat
// aceitar sem erro os literais "NaN"/"Inf"/"+Inf"/"-Inf"/"Infinity" NÃO
// significa que validarDimensaoLinha os aceita — o guard math.IsNaN/
// math.IsInf (review pass) rejeita todos eles.
func TestValidarDimensaoLinha_RejeitaNaoFinito(t *testing.T) {
	for _, valor := range []string{"NaN", "nan", "Inf", "+Inf", "-Inf", "Infinity", "-Infinity"} {
		t.Run(valor, func(t *testing.T) {
			_, _, err := validarDimensaoLinha("comprimento", valor, "mm")
			if err == nil {
				t.Fatalf("validarDimensaoLinha(%q) não retornou erro, want rejeição de valor não-finito", valor)
			}
		})
	}
}

// TestValidarDimensaoLinha_NaoAceitaVirgulaComoSeparadorDecimal prova a
// remoção da heurística ReplaceAll(",", ".") (review pass): antes, "1,5"
// virava "1.5" e era silenciosamente aceito; agora, sem heurística de
// localidade (mesma convenção do resto do sistema — API JSON/inputs HTML
// nunca fazem parsing de localidade), uma vírgula é só um caractere inválido
// para strconv.ParseFloat e a linha é corretamente rejeitada em vez de
// interpretada com o valor errado.
func TestValidarDimensaoLinha_NaoAceitaVirgulaComoSeparadorDecimal(t *testing.T) {
	_, _, err := validarDimensaoLinha("comprimento", "1,5", "mm")
	if err == nil {
		t.Fatal("validarDimensaoLinha(\"1,5\") não retornou erro, want rejeição (vírgula não é mais aceita)")
	}
}

// TestValidarDimensaoLinha_AceitaPontoDecimal prova que o caminho feliz
// (ponto decimal, a única notação aceita) continua funcionando após a
// remoção da heurística de vírgula.
func TestValidarDimensaoLinha_AceitaPontoDecimal(t *testing.T) {
	valor, unidade, err := validarDimensaoLinha("comprimento", "1.5", "MM")
	if err != nil {
		t.Fatalf("validarDimensaoLinha(\"1.5\") erro inesperado: %v", err)
	}
	if !valor.Valid || valor.Float64 != 1.5 {
		t.Errorf("valor = %+v, want {1.5 true}", valor)
	}
	if !unidade.Valid || unidade.String != "mm" {
		t.Errorf("unidade = %+v, want {mm true} (lowercased)", unidade)
	}
}

// TestValidarLinhaImportacao_QuantidadeRejeitaNaoFinitoEVirgula prova o mesmo
// guard (NaN/Inf, sem heurística de vírgula) para o parsing de `quantidade`
// em validarLinhaImportacao — código separado do de validarDimensaoLinha,
// precisa da própria cobertura.
func TestValidarLinhaImportacao_QuantidadeRejeitaNaoFinitoEVirgula(t *testing.T) {
	for _, quantidade := range []string{"NaN", "Inf", "1,5"} {
		t.Run(quantidade, func(t *testing.T) {
			linha := linhaBase("Produto Teste", "SKU-X", "Categoria Qualquer", quantidade, "Estoque X")
			_, err := validarLinhaImportacao(linha)
			if err == nil {
				t.Fatalf("validarLinhaImportacao com quantidade=%q não retornou erro", quantidade)
			}
		})
	}
}

// TestCriarImportacao_QuantidadeInvalidaOuNaoFinita prova o fim-a-fim (banco
// real) da correção: linhas com quantidade "NaN"/"1,5" são rejeitadas pelo
// pipeline completo, sem interromper a linha válida ao lado.
func TestCriarImportacao_QuantidadeInvalidaOuNaoFinita(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-quantidade-invalida@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Quantidade NaN", "SKU-QNAN", categoria, "NaN", "Canteiro Quantidade"),
		linhaBase("Produto Quantidade Virgula", "SKU-QVIRG", categoria, "1,5", "Canteiro Quantidade"),
		linhaBase("Produto Quantidade Valida", "SKU-QOK", categoria, "2.5", "Canteiro Quantidade"),
	}

	importacao, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if importacao.Status != "concluida" {
		t.Errorf("status = %q, want concluida", importacao.Status)
	}
	if relatorio.Criados != 1 {
		t.Errorf("Criados = %d, want 1 (só a linha com quantidade 2.5)", relatorio.Criados)
	}
	if relatorio.Rejeitados != 2 {
		t.Errorf("Rejeitados = %d, want 2 (NaN e vírgula)", relatorio.Rejeitados)
	}
	var quantidadeGravada float64
	if err := db.QueryRow(
		`SELECT pe.quantidade FROM produto_estoque pe JOIN produtos p ON p.id = pe.produto_id WHERE p.codigo_fornecedor = 'SKU-QOK'`,
	).Scan(&quantidadeGravada); err != nil {
		t.Fatalf("buscar quantidade gravada: %v", err)
	}
	if quantidadeGravada != 2.5 {
		t.Errorf("quantidade gravada = %v, want 2.5", quantidadeGravada)
	}
}

// TestContinuarImportacao_IDInexistente prova que um id de Importacao
// inexistente ou malformado devolve ErrImportacaoNaoEncontrada, sem
// processar nada.
func TestContinuarImportacao_IDInexistente(t *testing.T) {
	db := testDB(t)

	for _, id := range []string{
		"00000000-0000-0000-0000-000000000000", // UUID válido, sem linha
		"nao-e-um-uuid",                        // malformado
	} {
		t.Run(id, func(t *testing.T) {
			_, _, err := ContinuarImportacao(db, empresaTeste, id)
			if !errors.Is(err, ErrImportacaoNaoEncontrada) {
				t.Errorf("err = %v, want ErrImportacaoNaoEncontrada", err)
			}
		})
	}
}

// seedImportacaoBruta grava `importacoes` + `numeroDeLinhas` linhas de
// `importacao_linhas`, todas `pendente`, DIRETO via SQL — sem passar por
// CriarImportacao, que processaria tudo sincronamente sozinho e não deixaria
// nada pendente para um teste de concorrência disputar. Cada linha é válida
// (mesma categoria/estoque para todas) e tem um `codigo_fornecedor` único
// (`SKU-CONC-<i>`) para que o teste possa depois contar Produtos por padrão
// de código sem ambiguidade.
//
// TODAS as linhas citam o MESMO nome de Estoque, de propósito: além de
// provar a reivindicação de linha (`FOR UPDATE SKIP LOCKED`), isso também
// exercita `encontrarOuCriarEstoque` sob concorrência real — N transações
// concorrentes tentando criar o MESMO nome novo pela primeira vez ao mesmo
// tempo (corrigido em duas instruções separadas, ver o comentário da
// função; nenhum pré-cadastro é feito aqui).
func seedImportacaoBruta(t *testing.T, db *sql.DB, criadoPor string, numeroDeLinhas int, categoria string) string {
	t.Helper()
	var importacaoID string
	const insertImportacao = `
		INSERT INTO importacoes (nome_arquivo, total_linhas, criado_por, empresa_id)
		VALUES ($1, $2, $3, $4) RETURNING id`
	if err := db.QueryRow(insertImportacao, "planilha-concorrencia.xlsx", numeroDeLinhas, criadoPor, empresaTeste).Scan(&importacaoID); err != nil {
		t.Fatalf("seed importacoes: %v", err)
	}
	for i := 0; i < numeroDeLinhas; i++ {
		linha := linhaBase(
			fmt.Sprintf("Produto Concorrencia %d", i),
			fmt.Sprintf("SKU-CONC-%d", i),
			categoria, "1", "Canteiro Concorrencia",
		)
		dadosJSON, err := json.Marshal(linha)
		if err != nil {
			t.Fatalf("marshal linha %d: %v", i, err)
		}
		numeroLinha := i + 2
		const insertLinha = `
			INSERT INTO importacao_linhas (importacao_id, numero_linha, dados)
			VALUES ($1, $2, $3)`
		if _, err := db.Exec(insertLinha, importacaoID, numeroLinha, dadosJSON); err != nil {
			t.Fatalf("seed importacao_linhas (numero_linha=%d): %v", numeroLinha, err)
		}
	}
	return importacaoID
}

// TestContinuarImportacao_ConcorrenciaSemDuplicarProcessamento prova DUAS
// propriedades de segurança sob concorrência real, na mesma corrida:
//
//  1. Reivindicação de LINHA (`SELECT ... FOR UPDATE SKIP LOCKED`, lock
//     mantido pela transação inteira de cada linha): chamadas SIMULTÂNEAS de
//     ContinuarImportacao contra a MESMA importação nunca reivindicam/
//     processam a mesma linha em duplicidade.
//  2. encontrarOuCriarEstoque sob concorrência: TODAS as linhas seedadas
//     citam o MESMO nome de Estoque, ainda inexistente — N transações
//     concorrentes disputam criar esse nome pela primeira vez ao mesmo
//     tempo. Antes da correção pós-implementação (duas instruções
//     separadas, uma para o INSERT e outra para o SELECT de fallback — ver
//     o comentário de encontrarOuCriarEstoque), isso podia estourar
//     `sql.ErrNoRows` sob READ COMMITTED quando o INSERT perdia a corrida do
//     `ON CONFLICT` mas o SELECT de fallback da MESMA instrução ainda
//     enxergava o snapshot de antes da linha vencedora commitar.
//
// As goroutines abaixo compartilham o mesmo *sql.DB (mesmo pool de conexões)
// — não uma conexão dedicada cada — mas isso já basta para exercitar
// concorrência real: cada `db.Begin()` (dentro de processarProximaLinha) tira
// uma conexão física distinta do pool quando chamado de goroutines
// diferentes ao mesmo tempo (exatamente o que acontece em produção, com duas
// requisições HTTP concorrentes batendo no mesmo `*sql.DB` do servidor) — o
// teste não serializa nada por conta própria (nenhum mutex em torno das
// chamadas a ContinuarImportacao, nenhum pré-cadastro do Estoque).
func TestContinuarImportacao_ConcorrenciaSemDuplicarProcessamento(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-concorrencia@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	const totalLinhas = 20
	const numeroDeChamadasConcorrentes = 4
	importacaoID := seedImportacaoBruta(t, db, criadoPor, totalLinhas, categoria)

	var wg sync.WaitGroup
	erros := make([]error, numeroDeChamadasConcorrentes)
	for i := 0; i < numeroDeChamadasConcorrentes; i++ {
		wg.Add(1)
		go func(indice int) {
			defer wg.Done()
			_, _, err := ContinuarImportacao(db, empresaTeste, importacaoID)
			erros[indice] = err
		}(i)
	}
	wg.Wait()

	for i, err := range erros {
		if err != nil {
			t.Fatalf("chamada concorrente %d: ContinuarImportacao erro inesperado: %v", i, err)
		}
	}

	// Estado final no banco — a fonte de verdade, não o que cada goroutine
	// individualmente devolveu (uma delas pode ter processado 0 linhas se
	// outra já tiver reivindicado tudo primeiro; o que importa é o total).
	rows, err := db.Query(
		`SELECT status, count(*) FROM importacao_linhas WHERE importacao_id = $1 GROUP BY status`, importacaoID,
	)
	if err != nil {
		t.Fatalf("query final de status: %v", err)
	}
	defer rows.Close()
	var criados, rejeitados int
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			t.Fatalf("scan status final: %v", err)
		}
		switch status {
		case "criado":
			criados = n
		case "rejeitada":
			rejeitados = n
		default:
			t.Errorf("linha sobrou em status %q (%d linhas) após as chamadas concorrentes terminarem — "+
				"indica reivindicação incompleta/perdida", status, n)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar status final: %v", err)
	}

	if total := criados + rejeitados; total != totalLinhas {
		t.Errorf("criados+rejeitados = %d, want %d (nenhuma linha deveria sobrar sem processar)", total, totalLinhas)
	}
	if criados != totalLinhas {
		t.Errorf("criados = %d, want %d (todas as %d linhas seedadas são válidas)", criados, totalLinhas, totalLinhas)
	}

	// A prova mais direta da correção de encontrarOuCriarEstoque: as 20
	// linhas concorrentes citando o MESMO nome novo de Estoque devem ter
	// resultado em EXATAMENTE 1 linha em `estoques` — nem 0 (nenhuma
	// goroutine conseguiu, todas erraram com sql.ErrNoRows) nem >1 (a query
	// de fallback não pegou o conflito e cada uma criou a sua).
	if n := contarEstoques(t, db); n != 1 {
		t.Errorf("linhas em estoques = %d, want 1 (mesmo nome novo disputado por %d chamadas concorrentes)",
			n, numeroDeChamadasConcorrentes)
	}

	// Nenhum Produto duplicado: exatamente 1 por numero_linha. Duas
	// goroutines processando a MESMA linha produziriam 2 Produtos para o
	// mesmo `codigo_fornecedor` — o que esta contagem pegaria.
	var produtosCriados int
	if err := db.QueryRow(`SELECT count(*) FROM produtos WHERE codigo_fornecedor LIKE 'SKU-CONC-%'`).Scan(&produtosCriados); err != nil {
		t.Fatalf("count produtos: %v", err)
	}
	if produtosCriados != totalLinhas {
		t.Errorf("produtos criados = %d, want %d — uma diferença indicaria alguma linha processada em duplicidade "+
			"(ou nenhuma vez) por duas chamadas concorrentes", produtosCriados, totalLinhas)
	}

	// Reforço final e mais direto ainda: cada `codigo_fornecedor` semeado
	// corresponde a EXATAMENTE um produto_id em produto_estoque, nunca dois.
	var codigosComMaisDeUmProduto int
	if err := db.QueryRow(`
		SELECT count(*) FROM (
			SELECT codigo_fornecedor FROM produtos WHERE codigo_fornecedor LIKE 'SKU-CONC-%' GROUP BY codigo_fornecedor HAVING count(*) > 1
		) duplicados`,
	).Scan(&codigosComMaisDeUmProduto); err != nil {
		t.Fatalf("count códigos duplicados: %v", err)
	}
	if codigosComMaisDeUmProduto != 0 {
		t.Errorf("%d código(s) com mais de um Produto — linha processada em duplicidade", codigosComMaisDeUmProduto)
	}
}

// TestCriarImportacao_CodigoExistente_AtualizaEmVezDeCriar prova a I/O Matrix
// "Linha com código já existente, campos diferentes" (Story 3.4, FR-11): o
// Produto existente é sobrescrito com os novos valores da linha, a linha
// termina `atualizado` (nunca `criado`), e a quantidade do par
// (Produto, Estoque) é SUBSTITUÍDA, não somada.
func TestCriarImportacao_CodigoExistente_AtualizaEmVezDeCriar(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-atualiza@empresa.com")
	categoriaAntiga := categoriaIDPorCodigo(t, db, "04.001")
	categoriaNova := categoriaNomePorCodigo(t, db, "04.002")
	estoque, err := CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Canteiro Atualiza Existente")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	produtoExistente, err := criarProdutoComSaldo(db, empresaTeste, CriarProdutoInput{
		UnidadeMedida:    "un",
		Nome:             "Produto Nome Antigo",
		CategoriaID:      categoriaAntiga,
		TemplateID:       templateGenericoID(t, db, empresaTeste),
		CodigoFornecedor: "FORN-ATUALIZA-1",
	}, estoque.ID, 10)
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Nome Novo", "FORN-ATUALIZA-1", categoriaNova, "3", estoque.Nome),
	}
	importacao, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", estoque.ID, linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if importacao.Status != "concluida" {
		t.Errorf("status = %q, want concluida", importacao.Status)
	}
	if relatorio.Criados != 0 {
		t.Errorf("Criados = %d, want 0", relatorio.Criados)
	}
	if relatorio.Atualizados != 1 {
		t.Errorf("Atualizados = %d, want 1", relatorio.Atualizados)
	}
	if n := contarProdutos(t, db); n != 1 {
		t.Errorf("linhas em produtos = %d, want 1 (nenhum Produto novo)", n)
	}

	var nomeGravado, categoriaIDGravada string
	if err := db.QueryRow(
		`SELECT nome, categoria_id FROM produtos WHERE id = $1`, produtoExistente.ID,
	).Scan(&nomeGravado, &categoriaIDGravada); err != nil {
		t.Fatalf("buscar produto atualizado: %v", err)
	}
	if nomeGravado != "Produto Nome Novo" {
		t.Errorf("nome gravado = %q, want %q", nomeGravado, "Produto Nome Novo")
	}
	if categoriaIDGravada != categoriaIDPorCodigo(t, db, "04.002") {
		t.Errorf("categoria_id gravada = %q, want a categoria nova", categoriaIDGravada)
	}

	var quantidadeGravada float64
	if err := db.QueryRow(
		`SELECT quantidade FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2`,
		produtoExistente.ID, estoque.ID,
	).Scan(&quantidadeGravada); err != nil {
		t.Fatalf("buscar quantidade atualizada: %v", err)
	}
	if quantidadeGravada != 3 {
		t.Errorf("quantidade gravada = %v, want 3 (substituída, não somada aos 10 originais)", quantidadeGravada)
	}
}

// TestCriarImportacao_ReimportacaoIdentica_NuncaContaComoCriado prova a I/O
// Matrix "Reimportação idêntica" (Story 3.4): reimportar a MESMA planilha,
// sem nenhuma mudança de valor, roda o UPDATE de qualquer forma (idempotente,
// sem diff prévio) e a linha sempre termina `atualizado` — NUNCA `criado`,
// mesmo que nada tenha realmente mudado.
func TestCriarImportacao_ReimportacaoIdentica_NuncaContaComoCriado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-reimport@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Reimportado", "SKU-REIMPORT-1", categoria, "5", "Canteiro Reimportacao"),
	}

	_, relatorioPrimeiraVez, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha-1.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao (primeira vez) erro inesperado: %v", err)
	}
	if relatorioPrimeiraVez.Criados != 1 {
		t.Fatalf("Criados (primeira vez) = %d, want 1", relatorioPrimeiraVez.Criados)
	}
	produtosAntes := contarProdutos(t, db)

	_, relatorioSegundaVez, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha-2.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao (segunda vez) erro inesperado: %v", err)
	}
	if relatorioSegundaVez.Criados != 0 {
		t.Errorf("Criados (segunda vez) = %d, want 0", relatorioSegundaVez.Criados)
	}
	if relatorioSegundaVez.Atualizados != 1 {
		t.Errorf("Atualizados (segunda vez) = %d, want 1", relatorioSegundaVez.Atualizados)
	}
	if n := contarProdutos(t, db); n != produtosAntes {
		t.Errorf("linhas em produtos após reimportação idêntica = %d, want %d (sem Produto novo)", n, produtosAntes)
	}
}

// TestCriarImportacao_CodigoExistente_TemplateNomeInvalido_Rejeitada prova a
// I/O Matrix "Produto encontrado tem template_id aplicado, nome da linha não
// bate com o template" (Story 3.4): a linha é rejeitada citando o motivo, e o
// Produto NÃO é alterado (mesma regra de AtualizarNomeProduto, Story 3.2,
// aplicada aqui via UPDATE próprio).
func TestCriarImportacao_CodigoExistente_TemplateNomeInvalido_Rejeitada(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-template-invalido@empresa.com")
	categoriaID := categoriaIDPorCodigo(t, db, "04.002")
	categoriaNome := categoriaNomePorCodigo(t, db, "04.002")
	estoque, err := CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Canteiro Template Invalido Import")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	templateID, _ := templatePorSubtipo(t, db, "Tubo — PEAD/PPR")

	produtoExistente, err := criarProdutoComSaldo(db, empresaTeste, CriarProdutoInput{
		UnidadeMedida:    "un",
		Nome:             "TUBO PEAD PN80 DN50",
		CategoriaID:      categoriaID,
		TemplateID:       templateID,
		CodigoFornecedor: "FORN-TEMPLATE-INVALIDO-1",
	}, estoque.ID, 1)
	if err != nil {
		t.Fatalf("seed CriarProduto com template: %v", err)
	}

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Nome Fora Do Formato", "FORN-TEMPLATE-INVALIDO-1", categoriaNome, "1", estoque.Nome),
	}
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if relatorio.Criados != 0 || relatorio.Atualizados != 0 || relatorio.Rejeitados != 1 {
		t.Errorf("relatorio = %+v, want Criados=0 Atualizados=0 Rejeitados=1", relatorio)
	}
	if len(relatorio.LinhasRejeitadas) != 1 {
		t.Fatalf("LinhasRejeitadas = %+v, want 1 item", relatorio.LinhasRejeitadas)
	}
	if !strings.Contains(relatorio.LinhasRejeitadas[0].Erro, "template") {
		t.Errorf("Erro = %q, want citar 'template'", relatorio.LinhasRejeitadas[0].Erro)
	}

	var nomeGravado string
	if err := db.QueryRow(`SELECT nome FROM produtos WHERE id = $1`, produtoExistente.ID).Scan(&nomeGravado); err != nil {
		t.Fatalf("buscar produto após rejeição: %v", err)
	}
	if nomeGravado != "TUBO PEAD PN80 DN50" {
		t.Errorf("nome gravado = %q, want inalterado (%q)", nomeGravado, "TUBO PEAD PN80 DN50")
	}
}

// TestCriarImportacao_LinhaSemCodigo_NomeParecidoAindaAssimCria prova a I/O
// Matrix "Linha sem código, nome parecido com Produto existente" (Story 3.4):
// correspondência por nome nunca acontece — código vazio sempre cria Produto
// novo, mesmo com nome idêntico a um Produto já existente (Duplicatas é
// escopo do Epic 6, FR-19).
func TestCriarImportacao_LinhaSemCodigo_NomeParecidoAindaAssimCria(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-sem-codigo@empresa.com")
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	categoriaNome := categoriaNomePorCodigo(t, db, "04.001")
	estoque, err := CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Canteiro Sem Codigo")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	_, err = criarProdutoComSaldo(db, empresaTeste, CriarProdutoInput{
		UnidadeMedida: "un",
		Nome:          "Produto Nome Igualzinho",
		CategoriaID:   categoriaID,
		TemplateID:    templateGenericoID(t, db, empresaTeste),
	}, estoque.ID, 1)
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Nome Igualzinho", "", categoriaNome, "1", estoque.Nome),
	}
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if relatorio.Criados != 1 {
		t.Errorf("Criados = %d, want 1 (código vazio nunca casa por nome)", relatorio.Criados)
	}
	if relatorio.Atualizados != 0 {
		t.Errorf("Atualizados = %d, want 0", relatorio.Atualizados)
	}
	if n := contarProdutos(t, db); n != 2 {
		t.Errorf("linhas em produtos = %d, want 2 (Produto novo, sem duplicar o existente)", n)
	}
}

// TestCriarImportacao_DuasLinhasMesmoCodigoNovo_SegundaAtualizaAPrimeira
// prova a I/O Matrix "Duas linhas da mesma planilha com o mesmo código novo"
// (Story 3.4): a primeira linha cria o Produto; a segunda, processada na
// mesma leva sequencial de processarPendentes, já enxerga esse Produto
// (commitado pela transação da primeira linha) e o ATUALIZA — nunca duplica.
func TestCriarImportacao_DuasLinhasMesmoCodigoNovo_SegundaAtualizaAPrimeira(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-mesmo-codigo-novo@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Codigo Novo Primeira Vez", "SKU-DUPNOVO-1", categoria, "1", "Canteiro Codigo Novo"),
		linhaBase("Produto Codigo Novo Segunda Vez", "SKU-DUPNOVO-1", categoria, "9", "Canteiro Codigo Novo"),
	}
	importacao, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if importacao.Status != "concluida" {
		t.Errorf("status = %q, want concluida", importacao.Status)
	}
	if relatorio.Criados != 1 {
		t.Errorf("Criados = %d, want 1 (só a primeira linha)", relatorio.Criados)
	}
	if relatorio.Atualizados != 1 {
		t.Errorf("Atualizados = %d, want 1 (a segunda linha atualiza o Produto que a primeira criou)", relatorio.Atualizados)
	}
	if n := contarProdutos(t, db); n != 1 {
		t.Errorf("linhas em produtos = %d, want 1 (sem duplicar)", n)
	}

	var produtoIDLinha2, produtoIDLinha3 string
	if err := db.QueryRow(
		`SELECT produto_id FROM importacao_linhas WHERE importacao_id = $1 AND numero_linha = 2`, importacao.ID,
	).Scan(&produtoIDLinha2); err != nil {
		t.Fatalf("buscar produto_id linha 2: %v", err)
	}
	if err := db.QueryRow(
		`SELECT produto_id FROM importacao_linhas WHERE importacao_id = $1 AND numero_linha = 3`, importacao.ID,
	).Scan(&produtoIDLinha3); err != nil {
		t.Fatalf("buscar produto_id linha 3: %v", err)
	}
	if produtoIDLinha2 == "" || produtoIDLinha2 != produtoIDLinha3 {
		t.Errorf("produto_id linha 2 = %q, linha 3 = %q, want o mesmo id nas duas", produtoIDLinha2, produtoIDLinha3)
	}

	var quantidadeFinal float64
	if err := db.QueryRow(
		`SELECT quantidade FROM produto_estoque WHERE produto_id = $1`, produtoIDLinha2,
	).Scan(&quantidadeFinal); err != nil {
		t.Fatalf("buscar quantidade final: %v", err)
	}
	if quantidadeFinal != 9 {
		t.Errorf("quantidade final = %v, want 9 (valor da segunda linha, que atualizou por último)", quantidadeFinal)
	}
}

// TestCriarImportacao_CodigoExistente_NovoEstoque_ParExistenteIntacto prova a
// I/O Matrix "Linha com código existente referenciando Estoque diferente dos
// que o Produto já tem" (Story 3.4): um novo par `produto_estoque` é criado
// para o Estoque novo, e o par com o Estoque original permanece intacto — a
// atualização nunca apaga/zera pares de outros Estoques do mesmo Produto.
func TestCriarImportacao_CodigoExistente_NovoEstoque_ParExistenteIntacto(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-novo-estoque@empresa.com")
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	categoriaNome := categoriaNomePorCodigo(t, db, "04.001")
	estoqueOriginal, err := CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Canteiro Estoque Original")
	if err != nil {
		t.Fatalf("seed CriarEstoque original: %v", err)
	}

	produtoExistente, err := criarProdutoComSaldo(db, empresaTeste, CriarProdutoInput{
		UnidadeMedida:    "un",
		Nome:             "Produto Multi Estoque",
		CategoriaID:      categoriaID,
		TemplateID:       templateGenericoID(t, db, empresaTeste),
		CodigoFornecedor: "FORN-MULTI-ESTOQUE-1",
	}, estoqueOriginal.ID, 5)
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	estoqueNovo, err := CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Canteiro Estoque Novo")
	if err != nil {
		t.Fatalf("seed CriarEstoque novo: %v", err)
	}
	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Multi Estoque", "FORN-MULTI-ESTOQUE-1", categoriaNome, "7", "Canteiro Estoque Novo"),
	}
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", estoqueNovo.ID, linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if relatorio.Atualizados != 1 {
		t.Errorf("Atualizados = %d, want 1", relatorio.Atualizados)
	}
	if n := contarProdutos(t, db); n != 1 {
		t.Errorf("linhas em produtos = %d, want 1 (mesmo Produto, sem duplicar)", n)
	}

	rows, err := db.Query(
		`SELECT e.nome, pe.quantidade FROM produto_estoque pe
		 JOIN estoques e ON e.id = pe.estoque_id
		 WHERE pe.produto_id = $1 ORDER BY e.nome`,
		produtoExistente.ID,
	)
	if err != nil {
		t.Fatalf("query pares produto_estoque: %v", err)
	}
	defer rows.Close()
	pares := map[string]float64{}
	for rows.Next() {
		var nome string
		var quantidade float64
		if err := rows.Scan(&nome, &quantidade); err != nil {
			t.Fatalf("scan par produto_estoque: %v", err)
		}
		pares[nome] = quantidade
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar pares produto_estoque: %v", err)
	}
	if len(pares) != 2 {
		t.Fatalf("pares produto_estoque = %+v, want 2 (original + novo)", pares)
	}
	if q, ok := pares["Canteiro Estoque Original"]; !ok || q != 5 {
		t.Errorf("par com Estoque original = %v (presente=%v), want quantidade 5 intacta", q, ok)
	}
	if q, ok := pares["Canteiro Estoque Novo"]; !ok || q != 7 {
		t.Errorf("par com Estoque novo = %v (presente=%v), want quantidade 7", q, ok)
	}
}

// TestCriarImportacao_CodigoExistente_ColunaEstoqueIgnorada prova que a coluna
// "Estoque" da planilha (mesmo vazia/em branco) é IGNORADA: a linha atualiza o
// Produto existente no depósito escolhido, sem tocar o par do depósito original
// e sem criar depósito (spec-importacao-por-deposito).
func TestCriarImportacao_CodigoExistente_ColunaEstoqueIgnorada(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-estoque-invalido@empresa.com")
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	categoriaNome := categoriaNomePorCodigo(t, db, "04.001")
	estoqueOriginal, err := CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Canteiro Estoque Invalido Original")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	destino := depositoImp(t, db, empresaTeste)

	produtoExistente, err := criarProdutoComSaldo(db, empresaTeste, CriarProdutoInput{
		UnidadeMedida:    "un",
		Nome:             "Produto Nome Original",
		CategoriaID:      categoriaID,
		TemplateID:       templateGenericoID(t, db, empresaTeste),
		CodigoFornecedor: "FORN-ESTOQUE-INVALIDO-1",
	}, estoqueOriginal.ID, 4)
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Nome Diferente", "FORN-ESTOQUE-INVALIDO-1", categoriaNome, "9", "   "),
	}
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", destino, linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if relatorio.Atualizados != 1 || relatorio.Rejeitados != 0 {
		t.Fatalf("relatorio = %+v, want Atualizados=1 Rejeitados=0", relatorio)
	}
	var nome string
	if err := db.QueryRow(`SELECT nome FROM produtos WHERE id = $1`, produtoExistente.ID).Scan(&nome); err != nil || nome != "Produto Nome Diferente" {
		t.Errorf("nome = %q (err=%v), want atualizado", nome, err)
	}
	if q := saldoProdutoEstoque(t, db, produtoExistente.ID, estoqueOriginal.ID); q != 4 {
		t.Errorf("saldo no depósito original = %v, want 4 intacto", q)
	}
	if q := saldoProdutoEstoque(t, db, produtoExistente.ID, destino); q != 9 {
		t.Errorf("saldo no depósito escolhido = %v, want 9", q)
	}
	if n := contarEstoques(t, db); n != 2 {
		t.Errorf("estoques = %d, want 2 (nenhum criado pela importação)", n)
	}
}

// TestContinuarImportacao_CorridaMesmoCodigoNovo_SemErroNemDuplicar prova a
// correção do Bug 2 (review pass pós-implementação, Story 3.4): várias
// linhas da MESMA leva citando o MESMO código NOVO (ainda inexistente antes
// da importação), reivindicadas por chamadas concorrentes de
// ContinuarImportacao (`FOR UPDATE SKIP LOCKED` deixa cada uma pegar uma
// linha diferente), nunca produzem um erro de infraestrutura nem um Produto
// duplicado. A(s) transação(ões) que perdem a corrida do `INSERT INTO
// produtos` contra `idx_produtos_codigo` (migration 000017) se recuperam via
// SAVEPOINT e caem no ramo de atualização para o Produto que a vencedora
// criou — nunca abortam processarPendentes inteiro.
func TestContinuarImportacao_CorridaMesmoCodigoNovo_SemErroNemDuplicar(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-corrida-codigo@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	const totalLinhas = 20
	const codigoDisputado = "SKU-CORRIDA-CODIGO-NOVO"
	var importacaoID string
	const insertImportacao = `
		INSERT INTO importacoes (nome_arquivo, total_linhas, criado_por, empresa_id)
		VALUES ($1, $2, $3, $4) RETURNING id`
	if err := db.QueryRow(insertImportacao, "planilha-corrida-codigo.xlsx", totalLinhas, criadoPor, empresaTeste).Scan(&importacaoID); err != nil {
		t.Fatalf("seed importacoes: %v", err)
	}
	for i := 0; i < totalLinhas; i++ {
		// TODAS as linhas citam o MESMO código NOVO, de propósito — exercita
		// exatamente a corrida do Bug 2 (N transações concorrentes disputando
		// criar o mesmo código pela primeira vez, cada uma processando uma
		// linha diferente ao mesmo tempo).
		linha := linhaBase(
			fmt.Sprintf("Produto Corrida Codigo %d", i),
			codigoDisputado, categoria, "1", "Canteiro Corrida Codigo",
		)
		dadosJSON, err := json.Marshal(linha)
		if err != nil {
			t.Fatalf("marshal linha %d: %v", i, err)
		}
		numeroLinha := i + 2
		const insertLinha = `
			INSERT INTO importacao_linhas (importacao_id, numero_linha, dados)
			VALUES ($1, $2, $3)`
		if _, err := db.Exec(insertLinha, importacaoID, numeroLinha, dadosJSON); err != nil {
			t.Fatalf("seed importacao_linhas (numero_linha=%d): %v", numeroLinha, err)
		}
	}

	const numeroDeChamadasConcorrentes = 4
	var wg sync.WaitGroup
	erros := make([]error, numeroDeChamadasConcorrentes)
	for i := 0; i < numeroDeChamadasConcorrentes; i++ {
		wg.Add(1)
		go func(indice int) {
			defer wg.Done()
			_, _, err := ContinuarImportacao(db, empresaTeste, importacaoID)
			erros[indice] = err
		}(i)
	}
	wg.Wait()

	for i, err := range erros {
		if err != nil {
			t.Fatalf("chamada concorrente %d: ContinuarImportacao erro inesperado: %v", i, err)
		}
	}

	rows, err := db.Query(
		`SELECT status, count(*) FROM importacao_linhas WHERE importacao_id = $1 GROUP BY status`, importacaoID,
	)
	if err != nil {
		t.Fatalf("query final de status: %v", err)
	}
	defer rows.Close()
	var criados, atualizados int
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			t.Fatalf("scan status final: %v", err)
		}
		switch status {
		case "criado":
			criados = n
		case "atualizado":
			atualizados = n
		default:
			t.Errorf("linha sobrou em status %q (%d linhas) após as chamadas concorrentes terminarem — "+
				"indica reivindicação incompleta/perdida", status, n)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar status final: %v", err)
	}

	if criados != 1 {
		t.Errorf("criados = %d, want 1 (só a primeira linha a vencer a corrida cria o Produto)", criados)
	}
	if atualizados != totalLinhas-1 {
		t.Errorf("atualizados = %d, want %d (todas as demais atualizam o mesmo Produto, nunca duplicam)", atualizados, totalLinhas-1)
	}

	var nProdutos int
	if err := db.QueryRow(`SELECT count(*) FROM produtos WHERE codigo_fornecedor = $1`, codigoDisputado).Scan(&nProdutos); err != nil {
		t.Fatalf("count produtos: %v", err)
	}
	if nProdutos != 1 {
		t.Errorf("produtos com código %q = %d, want 1 (nenhuma duplicata mesmo sob corrida concorrente)", codigoDisputado, nProdutos)
	}
}

// Empresa SEM Filial (legado) com depósito escolhido: a importação grava no
// depósito legado escolhido (create e update de Produto existente) e nunca cria
// depósito — o antigo erro ErrEmpresaSemFilial não se aplica mais.
func TestCriarImportacao_EmpresaSemFilial_DepositoLegadoEscolhido(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	emp := empresaSemFilialDeTeste(t, db, "importacao-sem-filial-linha", "778889990109", "Importacao Sem Filial Linha")
	criadoPor := criarUsuarioImportacao(t, db, "importacao-sem-filial@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	var categoriaID, legadoID string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE codigo = '04.001' AND empresa_id = $1`, emp.ID).Scan(&categoriaID); err != nil {
		t.Fatalf("categoria da empresa: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO estoques (nome, empresa_id) VALUES ('Legado Sem Filial', $1) RETURNING id`, emp.ID).Scan(&legadoID); err != nil {
		t.Fatalf("seed estoque legado: %v", err)
	}
	_, err := criarProdutoComSaldo(db, emp.ID, CriarProdutoInput{
		UnidadeMedida:    "un",
		Nome:             "Produto Existente Sem Filial",
		CategoriaID:      categoriaID,
		TemplateID:       templateGenericoID(t, db, emp.ID),
		CodigoFornecedor: "FORN-SEM-FILIAL-1",
	}, legadoID, 4)
	if err != nil {
		t.Fatalf("seed produto: %v", err)
	}

	linhas := [][]string{
		CabecalhoEsperado,
		// create: código novo + Estoque novo.
		linhaBase("Produto Novo Sem Filial", "SKU-SEM-FILIAL", categoria, "2", "Estoque Novo Sem Filial"),
		// update: Produto existente por código do fornecedor + Estoque novo.
		linhaBase("Produto Existente Sem Filial", "FORN-SEM-FILIAL-1", categoria, "3", "Estoque Novo Sem Filial"),
	}
	importacao, relatorio, err := CriarImportacao(db, emp.ID, criadoPor, "planilha.xlsx", legadoID, linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if relatorio.Criados != 1 || relatorio.Atualizados != 1 || relatorio.Rejeitados != 0 {
		t.Fatalf("relatorio = %+v, want Criados=1 Atualizados=1 Rejeitados=0", relatorio)
	}
	if importacao.Status != "concluida" {
		t.Errorf("status da importação = %q, want concluida", importacao.Status)
	}
	if n := contar(t, db, `SELECT count(*) FROM estoques WHERE empresa_id = $1`, emp.ID); n != 1 {
		t.Errorf("estoques da Empresa = %d, want 1 (só o legado; nenhum criado pela importação)", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM produto_estoque WHERE estoque_id = $1 AND quantidade IN (2, 3)`, legadoID); n != 2 {
		t.Errorf("saldos no depósito legado = %d, want 2", n)
	}
}

// TestCriarImportacao_GeraCodigoInternoETemplateGenerico prova a correção do
// bug (feedback real, Ferreira Costa, 2026-11-12): todo Produto criado pela
// importação ganha um `codigo` interno sequencial (igual ao cadastro
// manual, nunca NULL) e o template "Genérico" da Empresa — o valor da célula
// "Código" da planilha vai para `codigo_fornecedor`, nunca mais para
// `codigo`.
func TestCriarImportacao_GeraCodigoInternoETemplateGenerico(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-codigo-interno@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Codigo Interno Legado", "B.1.02.C009", categoria, "1", "Canteiro Codigo Interno"),
	}
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if relatorio.Criados != 1 {
		t.Fatalf("Criados = %d, want 1", relatorio.Criados)
	}

	var codigo, codigoFornecedor sql.NullString
	var templateID sql.NullString
	if err := db.QueryRow(
		`SELECT codigo, codigo_fornecedor, template_id FROM produtos WHERE nome = $1 AND empresa_id = $2`,
		"Produto Codigo Interno Legado", empresaTeste,
	).Scan(&codigo, &codigoFornecedor, &templateID); err != nil {
		t.Fatalf("buscar produto criado: %v", err)
	}
	if !codigo.Valid || len(codigo.String) != 6 {
		t.Errorf("codigo = %+v, want sequencial de 6 dígitos gerado pelo servidor (bug: planilha com código gravava o valor da planilha direto em `codigo`)", codigo)
	}
	if codigo.String == "B.1.02.C009" {
		t.Errorf("codigo = %q, want o sequencial interno, nunca o valor literal da planilha", codigo.String)
	}
	if !codigoFornecedor.Valid || codigoFornecedor.String != "B.1.02.C009" {
		t.Errorf("codigo_fornecedor = %+v, want %q (valor da célula \"Código\" da planilha)", codigoFornecedor, "B.1.02.C009")
	}
	if !templateID.Valid {
		t.Error("template_id = NULL, want o template Genérico da Empresa (importação é um caminho de cadastro, mesma regra do manual)")
	} else if generico := templateGenericoID(t, db, empresaTeste); templateID.String != generico {
		t.Errorf("template_id = %q, want o Genérico (%q)", templateID.String, generico)
	}
}

// TestCriarImportacao_CelulaCodigoVazia_GeraCodigoInternoSemCodigoFornecedor
// prova o subcaso "célula vazia" da mesma correção: o Produto ainda ganha
// `codigo` sequencial (nunca NULL) e `codigo_fornecedor` fica NULL — nunca
// confundido com o literal vazio.
func TestCriarImportacao_CelulaCodigoVazia_GeraCodigoInternoSemCodigoFornecedor(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-sem-codigo-fornecedor@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Sem Codigo Planilha Nenhum", "", categoria, "1", "Canteiro Sem Codigo Fornecedor"),
	}
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", depositoImp(t, db, empresaTeste), linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if relatorio.Criados != 1 {
		t.Fatalf("Criados = %d, want 1", relatorio.Criados)
	}

	var codigo, codigoFornecedor sql.NullString
	if err := db.QueryRow(
		`SELECT codigo, codigo_fornecedor FROM produtos WHERE nome = $1 AND empresa_id = $2`,
		"Produto Sem Codigo Planilha Nenhum", empresaTeste,
	).Scan(&codigo, &codigoFornecedor); err != nil {
		t.Fatalf("buscar produto criado: %v", err)
	}
	if !codigo.Valid || codigo.String == "" {
		t.Fatalf("codigo = %+v, want sequencial não-nulo (bug: planilha sem código gravava codigo = NULL)", codigo)
	}
	if len(codigo.String) != 6 {
		t.Errorf("codigo = %q, want 6 dígitos (mesmo formato de zero-padding de proximoCodigoProduto)", codigo.String)
	}
	if codigoFornecedor.Valid {
		t.Errorf("codigo_fornecedor = %q, want NULL (célula vazia na planilha)", codigoFornecedor.String)
	}
}

// TestContinuarImportacao_CorridaCodigoFornecedorRepetido_UmaCriaDemaisAtualizam
// prova que o `pg_advisory_xact_lock` por (empresa, código do fornecedor)
// restaura, para a chave nova, a MESMA garantia que `idx_produtos_codigo`
// dava à chave antiga: N linhas da MESMA leva repetindo o MESMO código do
// fornecedor (ainda inexistente), reivindicadas por chamadas concorrentes de
// ContinuarImportacao, nunca produzem mais de um Produto — mesmo sem nenhum
// índice único em `codigo_fornecedor` (Story 10.3).
func TestContinuarImportacao_CorridaCodigoFornecedorRepetido_UmaCriaDemaisAtualizam(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-corrida-codigo-fornecedor@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")

	const totalLinhas = 20
	const codigoFornecedorDisputado = "FORN-CORRIDA-REPETIDO"

	var importacaoID string
	const insertImportacao = `
		INSERT INTO importacoes (nome_arquivo, total_linhas, criado_por, empresa_id)
		VALUES ($1, $2, $3, $4) RETURNING id`
	if err := db.QueryRow(insertImportacao, "planilha-corrida-fornecedor.xlsx", totalLinhas, criadoPor, empresaTeste).Scan(&importacaoID); err != nil {
		t.Fatalf("seed importacoes: %v", err)
	}
	for i := 0; i < totalLinhas; i++ {
		linha := linhaBase(
			fmt.Sprintf("Produto Corrida Fornecedor %d", i),
			codigoFornecedorDisputado, categoria, "1", "Canteiro Corrida Fornecedor",
		)
		dadosJSON, err := json.Marshal(linha)
		if err != nil {
			t.Fatalf("marshal linha %d: %v", i, err)
		}
		numeroLinha := i + 2
		const insertLinha = `
			INSERT INTO importacao_linhas (importacao_id, numero_linha, dados)
			VALUES ($1, $2, $3)`
		if _, err := db.Exec(insertLinha, importacaoID, numeroLinha, dadosJSON); err != nil {
			t.Fatalf("seed importacao_linhas (numero_linha=%d): %v", numeroLinha, err)
		}
	}

	const numeroDeChamadasConcorrentes = 4
	var wg sync.WaitGroup
	erros := make([]error, numeroDeChamadasConcorrentes)
	for i := 0; i < numeroDeChamadasConcorrentes; i++ {
		wg.Add(1)
		go func(indice int) {
			defer wg.Done()
			_, _, err := ContinuarImportacao(db, empresaTeste, importacaoID)
			erros[indice] = err
		}(i)
	}
	wg.Wait()

	for i, err := range erros {
		if err != nil {
			t.Fatalf("chamada concorrente %d: ContinuarImportacao erro inesperado: %v", i, err)
		}
	}

	var nProdutos int
	if err := db.QueryRow(`SELECT count(*) FROM produtos WHERE codigo_fornecedor = $1`, codigoFornecedorDisputado).Scan(&nProdutos); err != nil {
		t.Fatalf("count produtos: %v", err)
	}
	if nProdutos != 1 {
		t.Errorf("produtos com código do fornecedor %q = %d, want 1 (nenhuma duplicata mesmo sob corrida concorrente, campo sem índice único)", codigoFornecedorDisputado, nProdutos)
	}
}

// depositoImp devolve o id do depósito de destino usado pelos testes de
// importação: "Depósito Importação Teste", criado na Filial padrão da Empresa
// na primeira chamada do teste e reaproveitado nas seguintes.
func depositoImp(t *testing.T, db *sql.DB, empresaID string) string {
	t.Helper()
	var id string
	err := db.QueryRow(`SELECT id FROM estoques WHERE empresa_id = $1 AND nome = 'Depósito Importação Teste'`, empresaID).Scan(&id)
	if err == nil {
		return id
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("buscar depósito de importação: %v", err)
	}
	e, err := CriarEstoque(db, empresaID, filialTeste(t, db, empresaID), "Depósito Importação Teste")
	if err != nil {
		t.Fatalf("criar depósito de importação: %v", err)
	}
	return e.ID
}

// --- spec-importacao-por-deposito -----------------------------------------

func TestCriarImportacao_SemEstoqueID_Recusa(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-sem-estoque-id@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	linhas := [][]string{CabecalhoEsperado, linhaBase("Produto Sem Deposito", "SKU-SD", categoria, "1", "")}

	for _, vazio := range []string{"", "   "} {
		_, _, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", vazio, linhas)
		if !errors.Is(err, ErrImportacaoSemDeposito) {
			t.Fatalf("err = %v, want ErrImportacaoSemDeposito", err)
		}
	}
	if n := contarProdutos(t, db); n != 0 {
		t.Errorf("produtos = %d, want 0", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM importacoes WHERE criado_por = $1`, criadoPor); n != 0 {
		t.Errorf("importacoes = %d, want 0 (nada gravado)", n)
	}
}

func TestCriarImportacao_DepositoDeOutraEmpresaOuInexistente_NaoEncontrado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-outra-empresa@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	outra := empresaSemFilialDeTeste(t, db, "importacao-deposito-alheio", "778889990117", "Importacao Deposito Alheio")
	var alheioID string
	if err := db.QueryRow(`INSERT INTO estoques (nome, empresa_id) VALUES ('Deposito Alheio', $1) RETURNING id`, outra.ID).Scan(&alheioID); err != nil {
		t.Fatalf("seed estoque alheio: %v", err)
	}
	linhas := [][]string{CabecalhoEsperado, linhaBase("Produto Alheio", "SKU-ALH", categoria, "1", "")}

	for _, id := range []string{alheioID, "00000000-0000-0000-0000-000000000000", "nao-e-uuid"} {
		_, _, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", id, linhas)
		if !errors.Is(err, ErrEstoqueNaoEncontrado) {
			t.Fatalf("id %q: err = %v, want ErrEstoqueNaoEncontrado", id, err)
		}
	}
	if n := contarProdutos(t, db); n != 0 {
		t.Errorf("produtos = %d, want 0", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM importacoes WHERE criado_por = $1`, criadoPor); n != 0 {
		t.Errorf("importacoes = %d, want 0", n)
	}
}

func TestCriarImportacao_ColunaEstoquePreenchida_NaoCriaDeposito(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-ignora-estoque@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	destino := depositoImp(t, db, empresaTeste)

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Ignora Um", "SKU-IGN-1", categoria, "4", "Depósito Fantasma"),
		linhaBase("Produto Ignora Dois", "SKU-IGN-2", categoria, "6", ""),
	}
	imp, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", destino, linhas)
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if relatorio.Criados != 2 || relatorio.Rejeitados != 0 {
		t.Fatalf("relatorio = %+v", relatorio)
	}
	if n := contar(t, db, `SELECT count(*) FROM estoques WHERE nome = 'Depósito Fantasma'`); n != 0 {
		t.Errorf("depósito fantasma criado")
	}
	if n := contarEstoques(t, db); n != 1 {
		t.Errorf("estoques = %d, want 1", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM produto_estoque WHERE estoque_id = $1`, destino); n != 2 {
		t.Errorf("saldos no depósito escolhido = %d, want 2", n)
	}
	if imp.EstoqueID == nil || *imp.EstoqueID != destino || imp.EstoqueNome == nil || *imp.EstoqueNome != "Depósito Importação Teste" {
		t.Errorf("importacao não expõe o depósito: %+v", imp)
	}
	if imp.FilialNome == nil || !imp.PorDeposito {
		t.Errorf("filial/por_deposito ausentes: %+v", imp)
	}
}

func TestCriarImportacao_VazioViraZero_ProdutoNovo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-vazio-zero@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	destino := depositoImp(t, db, empresaTeste)

	vazio := linhaBase("Produto Novo Vazio", "SKU-NV-1", categoria, "", "")
	zero := linhaBase("Produto Novo Zero", "SKU-NV-2", categoria, "0", "")
	zero[colPrecoUnitario] = "0"
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", destino, [][]string{CabecalhoEsperado, vazio, zero})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if relatorio.Criados != 2 || relatorio.Rejeitados != 0 || relatorio.PreenchidasComZero != 2 || relatorio.MantiveramValorAtual != 0 {
		t.Fatalf("relatorio = %+v", relatorio)
	}
	for _, cod := range []string{"SKU-NV-1", "SKU-NV-2"} {
		var q, preco float64
		if err := db.QueryRow(`SELECT pe.quantidade, p.preco_unitario FROM produto_estoque pe JOIN produtos p ON p.id = pe.produto_id WHERE p.codigo_fornecedor = $1`, cod).Scan(&q, &preco); err != nil {
			t.Fatalf("%s: %v", cod, err)
		}
		if q != 0 || preco != 0 {
			t.Errorf("%s: quantidade=%v preco=%v, want 0/0", cod, q, preco)
		}
	}
}

func TestCriarImportacao_VazioOuZero_ProdutoExistente_MantemSaldoEPreco(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-mantem@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	destino := depositoImp(t, db, empresaTeste)
	preco := 12.5

	casos := []struct {
		cod, qtd, prc string
		mantemQtd     bool
		mantemPreco   bool
	}{
		{"EX-QVAZIO", "", "20", true, false},
		{"EX-QZERO", "0", "20", true, false},
		{"EX-PVAZIO", "8", "", false, true},
		{"EX-PZERO", "8", "0", false, true},
	}
	ids := map[string]string{}
	for _, c := range casos {
		p, err := criarProdutoComSaldo(db, empresaTeste, CriarProdutoInput{
			UnidadeMedida: "un", Nome: "Produto " + c.cod, CategoriaID: categoriaID,
			TemplateID: templateGenericoID(t, db, empresaTeste), CodigoFornecedor: c.cod, PrecoUnitario: &preco,
		}, destino, 5)
		if err != nil {
			t.Fatalf("seed %s: %v", c.cod, err)
		}
		ids[c.cod] = p.ID
	}
	linhas := [][]string{CabecalhoEsperado}
	for _, c := range casos {
		l := linhaBase("Produto "+c.cod, c.cod, categoria, c.qtd, "")
		l[colPrecoUnitario] = c.prc
		linhas = append(linhas, l)
	}
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", destino, linhas)
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if relatorio.Atualizados != 4 || relatorio.Rejeitados != 0 || relatorio.MantiveramValorAtual != 4 || relatorio.PreenchidasComZero != 0 {
		t.Fatalf("relatorio = %+v", relatorio)
	}
	for _, c := range casos {
		q := saldoProdutoEstoque(t, db, ids[c.cod], destino)
		var p float64
		if err := db.QueryRow(`SELECT preco_unitario FROM produtos WHERE id = $1`, ids[c.cod]).Scan(&p); err != nil {
			t.Fatal(err)
		}
		wantQ, wantP := 8.0, 20.0
		if c.mantemQtd {
			wantQ = 5
		}
		if c.mantemPreco {
			wantP = 12.5
		}
		if q != wantQ || p != wantP {
			t.Errorf("%s: saldo=%v preco=%v, want %v/%v", c.cod, q, p, wantQ, wantP)
		}
	}
}

func TestCriarImportacao_QuantidadeEPrecoInvalidos_Rejeitados(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-invalidos@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	destino := depositoImp(t, db, empresaTeste)

	l1 := linhaBase("Produto Qtd Texto", "SKU-INV-1", categoria, "abc", "")
	l2 := linhaBase("Produto Qtd Negativa", "SKU-INV-2", categoria, "-3", "")
	l3 := linhaBase("Produto Preco Texto", "SKU-INV-3", categoria, "1", "")
	l3[colPrecoUnitario] = "xyz"
	l4 := linhaBase("Produto Preco Negativo", "SKU-INV-4", categoria, "1", "")
	l4[colPrecoUnitario] = "-2"
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", destino, [][]string{CabecalhoEsperado, l1, l2, l3, l4})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if relatorio.Criados != 0 || relatorio.Rejeitados != 4 {
		t.Fatalf("relatorio = %+v, want 4 rejeitadas", relatorio)
	}
}

func TestContinuarImportacao_MesmoDepositoEscolhido(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-retomada-deposito@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	destino := depositoImp(t, db, empresaTeste)

	imp, _, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", destino, [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Retoma Um", "SKU-RET-1", categoria, "1", "Outro"),
		linhaBase("Produto Retoma Dois", "SKU-RET-2", categoria, "2", "Outro"),
	})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	// Simula interrupção: a linha 3 volta a pendente e seu produto some.
	if _, err := db.Exec(`UPDATE importacao_linhas SET status = 'pendente', produto_id = NULL WHERE importacao_id = $1 AND numero_linha = 3`, imp.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM produto_estoque WHERE produto_id IN (SELECT id FROM produtos WHERE codigo_fornecedor = 'SKU-RET-2')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE importacoes SET status = 'em_andamento' WHERE id = $1`, imp.ID); err != nil {
		t.Fatal(err)
	}
	cont, _, err := ContinuarImportacao(db, empresaTeste, imp.ID)
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if cont.EstoqueID == nil || *cont.EstoqueID != destino {
		t.Errorf("estoque_id na retomada = %v, want %s", cont.EstoqueID, destino)
	}
	if n := contar(t, db, `SELECT count(*) FROM produto_estoque WHERE estoque_id = $1`, destino); n != 2 {
		t.Errorf("saldos no depósito = %d, want 2", n)
	}
	if n := contarEstoques(t, db); n != 1 {
		t.Errorf("estoques = %d, want 1", n)
	}
}

func TestContinuarImportacao_DepositoExcluido_RejeitaRestantes(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-deposito-excluido@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	destino := depositoImp(t, db, empresaTeste)

	imp, _, err := CriarImportacao(db, empresaTeste, criadoPor, "planilha.xlsx", destino, [][]string{
		CabecalhoEsperado, linhaBase("Produto Exc Um", "SKU-EXC-1", categoria, "1", ""),
	})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM produto_estoque WHERE estoque_id = $1`, destino); err != nil {
		t.Fatal(err)
	}
	if err := ExcluirEstoque(db, empresaTeste, destino); err != nil {
		t.Fatalf("ExcluirEstoque: %v", err)
	}
	if _, err := db.Exec(`UPDATE importacao_linhas SET status = 'pendente', produto_id = NULL WHERE importacao_id = $1`, imp.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE importacoes SET status = 'em_andamento' WHERE id = $1`, imp.ID); err != nil {
		t.Fatal(err)
	}
	// Mesmo produto com outro código não existe: a linha casa por código com o
	// produto criado antes; o depósito sumiu, então deve ser rejeitada.
	_, relatorio, err := ContinuarImportacao(db, empresaTeste, imp.ID)
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if relatorio.Rejeitados != 1 || !strings.Contains(relatorio.LinhasRejeitadas[0].Erro, "depósito de destino") {
		t.Fatalf("relatorio = %+v", relatorio)
	}
	if n := contarEstoques(t, db); n != 0 {
		t.Errorf("estoques = %d, want 0 (nada recriado)", n)
	}
}

func TestContinuarImportacao_LegadaSemEstoqueID_SegueCaminhoLegado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-legada@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	// Garante Filial padrão para o caminho legado criar o depósito.
	filialTeste(t, db, empresaTeste)

	var impID string
	if err := db.QueryRow(`INSERT INTO importacoes (nome_arquivo, total_linhas, criado_por, empresa_id) VALUES ('legada.xlsx', 1, $1, $2) RETURNING id`, criadoPor, empresaTeste).Scan(&impID); err != nil {
		t.Fatal(err)
	}
	dados, _ := json.Marshal(linhaBase("Produto Legado", "SKU-LEG-1", categoria, "3", "Depósito Legado Da Planilha"))
	if _, err := db.Exec(`INSERT INTO importacao_linhas (importacao_id, numero_linha, dados) VALUES ($1, 2, $2)`, impID, dados); err != nil {
		t.Fatal(err)
	}
	imp, relatorio, err := ContinuarImportacao(db, empresaTeste, impID)
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if relatorio.Criados != 1 || imp.PorDeposito || imp.EstoqueID != nil {
		t.Fatalf("imp=%+v relatorio=%+v", imp, relatorio)
	}
	if n := contar(t, db, `SELECT count(*) FROM estoques WHERE nome = 'Depósito Legado Da Planilha'`); n != 1 {
		t.Errorf("caminho legado deveria criar o depósito da planilha")
	}
}

// --- reforços da revisão (spec-importacao-por-deposito) ---------------------

func TestEhZeroNaColuna(t *testing.T) {
	casos := []struct {
		v     float64
		casas int
		want  bool
	}{
		{0, 3, true}, {0.0004, 3, true}, {0.0005, 3, false}, {0.001, 3, false},
		{0.004, 2, true}, {0.005, 2, false}, {1, 3, false},
	}
	for _, c := range casos {
		if got := ehZeroNaColuna(c.v, c.casas); got != c.want {
			t.Errorf("ehZeroNaColuna(%v, %d) = %v, want %v", c.v, c.casas, got, c.want)
		}
	}
	for _, txt := range []string{"-0", "0e0", "1e-400", "0.0", "00"} {
		linha := linhaBase("X", "", "Cat", txt, "")
		v, err := validarLinhaImportacao(linha)
		if err != nil || !v.quantidadeZero || v.quantidade != 0 {
			t.Errorf("quantidade %q: %+v err=%v, want zero", txt, v, err)
		}
	}
}

func TestMigration000064_ColunasESetNullAoExcluirDeposito(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	for _, col := range []string{"estoque_id", "por_deposito"} {
		if n := contar(t, db, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'importacoes' AND column_name = $1`, col); n != 1 {
			t.Errorf("coluna importacoes.%s ausente", col)
		}
	}
	criadoPor := criarUsuarioImportacao(t, db, "importacao-set-null@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	destino := depositoImp(t, db, empresaTeste)
	imp, _, err := CriarImportacao(db, empresaTeste, criadoPor, "p.xlsx", destino, [][]string{CabecalhoEsperado, linhaBase("Produto Null", "SKU-NULL", categoria, "", "")})
	if err != nil {
		t.Fatal(err)
	}
	if err := ExcluirEstoque(db, empresaTeste, destino); err != nil {
		t.Fatalf("excluir depósito usado por importação: %v", err)
	}
	var estoqueID sql.NullString
	var por bool
	if err := db.QueryRow(`SELECT estoque_id, por_deposito FROM importacoes WHERE id = $1`, imp.ID).Scan(&estoqueID, &por); err != nil {
		t.Fatal(err)
	}
	if estoqueID.Valid || !por {
		t.Errorf("estoque_id=%v por_deposito=%v, want NULL/true", estoqueID, por)
	}
}

func TestCriarImportacao_CelulaEstoqueLongaOuEstranha_Ignorada(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-estoque-longo@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	destino := depositoImp(t, db, empresaTeste)
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "p.xlsx", destino, [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Estoque Longo", "SKU-LONGO", categoria, "1", strings.Repeat("x", 400)),
		linhaBase("Produto Estoque Estranho", "SKU-ESTR", categoria, "1", "<script>\x01 ; DROP"),
	})
	if err != nil || relatorio.Criados != 2 || relatorio.Rejeitados != 0 {
		t.Fatalf("relatorio=%+v err=%v", relatorio, err)
	}
	if n := contarEstoques(t, db); n != 1 {
		t.Errorf("estoques = %d, want 1", n)
	}
}

func TestContinuarImportacao_SegundoDepositoDaEmpresa(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-segundo-deposito@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	primeiro := depositoImp(t, db, empresaTeste)
	segundo, err := CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Segundo Depósito Importação")
	if err != nil {
		t.Fatal(err)
	}
	imp, _, err := CriarImportacao(db, empresaTeste, criadoPor, "p.xlsx", segundo.ID, [][]string{
		CabecalhoEsperado, linhaBase("Produto Segundo", "SKU-SEG-1", categoria, "3", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE importacao_linhas SET status = 'pendente', produto_id = NULL WHERE importacao_id = $1`, imp.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM produto_estoque`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE importacoes SET status = 'em_andamento' WHERE id = $1`, imp.ID); err != nil {
		t.Fatal(err)
	}
	cont, _, err := ContinuarImportacao(db, empresaTeste, imp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cont.EstoqueID == nil || *cont.EstoqueID != segundo.ID {
		t.Errorf("estoque_id = %v, want o segundo depósito", cont.EstoqueID)
	}
	if n := contar(t, db, `SELECT count(*) FROM produto_estoque WHERE estoque_id = $1`, segundo.ID); n != 1 {
		t.Errorf("saldo no segundo depósito = %d, want 1", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM produto_estoque WHERE estoque_id = $1`, primeiro); n != 0 {
		t.Errorf("saldo no primeiro depósito = %d, want 0", n)
	}
}

func TestContinuarImportacao_PorDepositoSemEstoque_ProdutoNovoRejeitado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-deposito-excluido-novo@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	filialTeste(t, db, empresaTeste)

	var impID string
	if err := db.QueryRow(`INSERT INTO importacoes (nome_arquivo, total_linhas, criado_por, empresa_id, estoque_id, por_deposito) VALUES ('x.xlsx', 1, $1, $2, NULL, TRUE) RETURNING id`, criadoPor, empresaTeste).Scan(&impID); err != nil {
		t.Fatal(err)
	}
	dados, _ := json.Marshal(linhaBase("Produto Novo Sem Deposito", "SKU-NSD", categoria, "2", "Nome Planilha"))
	if _, err := db.Exec(`INSERT INTO importacao_linhas (importacao_id, numero_linha, dados) VALUES ($1, 2, $2)`, impID, dados); err != nil {
		t.Fatal(err)
	}
	imp, relatorio, err := ContinuarImportacao(db, empresaTeste, impID)
	if err != nil {
		t.Fatal(err)
	}
	if relatorio.Rejeitados != 1 || !strings.Contains(relatorio.LinhasRejeitadas[0].Erro, "depósito de destino") {
		t.Fatalf("relatorio = %+v", relatorio)
	}
	if !imp.PorDeposito || imp.EstoqueID != nil {
		t.Errorf("imp = %+v", imp)
	}
	if n := contarProdutos(t, db); n != 0 {
		t.Errorf("produtos = %d, want 0 (produto novo não é criado sem depósito)", n)
	}
	if n := contarEstoques(t, db); n != 0 {
		t.Errorf("estoques = %d, want 0 (nada recriado)", n)
	}
}

func TestContinuarImportacao_LegadaComProdutoExistente_AtualizaPeloNomeDaPlanilha(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-legada-existente@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	filialTeste(t, db, empresaTeste)
	legado, err := CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Depósito Legado Existente")
	if err != nil {
		t.Fatal(err)
	}
	p, err := criarProdutoComSaldo(db, empresaTeste, CriarProdutoInput{
		UnidadeMedida: "un", Nome: "Produto Legado Existente", CategoriaID: categoriaID,
		TemplateID: templateGenericoID(t, db, empresaTeste), CodigoFornecedor: "FORN-LEG-EX",
	}, legado.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var impID string
	if err := db.QueryRow(`INSERT INTO importacoes (nome_arquivo, total_linhas, criado_por, empresa_id) VALUES ('legada.xlsx', 1, $1, $2) RETURNING id`, criadoPor, empresaTeste).Scan(&impID); err != nil {
		t.Fatal(err)
	}
	dados, _ := json.Marshal(linhaBase("Produto Legado Existente", "FORN-LEG-EX", categoria, "7", "Depósito Legado Existente"))
	if _, err := db.Exec(`INSERT INTO importacao_linhas (importacao_id, numero_linha, dados) VALUES ($1, 2, $2)`, impID, dados); err != nil {
		t.Fatal(err)
	}
	_, relatorio, err := ContinuarImportacao(db, empresaTeste, impID)
	if err != nil || relatorio.Atualizados != 1 {
		t.Fatalf("relatorio=%+v err=%v", relatorio, err)
	}
	if q := saldoProdutoEstoque(t, db, p.ID, legado.ID); q != 7 {
		t.Errorf("saldo = %v, want 7 (depósito pelo nome da planilha, caminho legado)", q)
	}
}

func TestCriarImportacao_Invalidos_MotivoPorLinhaENadaGravado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-invalidos-motivo@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	destino := depositoImp(t, db, empresaTeste)

	l1 := linhaBase("P Qtd Texto", "SKU-M-1", categoria, "abc", "")
	l2 := linhaBase("P Qtd Negativa", "SKU-M-2", categoria, "-3", "")
	l3 := linhaBase("P Preco Texto", "SKU-M-3", categoria, "1", "")
	l3[colPrecoUnitario] = "xyz"
	l4 := linhaBase("P Preco Negativo", "SKU-M-4", categoria, "1", "")
	l4[colPrecoUnitario] = "-2"
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "p.xlsx", destino, [][]string{CabecalhoEsperado, l1, l2, l3, l4})
	if err != nil {
		t.Fatal(err)
	}
	motivos := map[int]string{}
	for _, l := range relatorio.LinhasRejeitadas {
		motivos[l.Linha] = l.Erro
	}
	esperado := map[int]string{2: "quantidade: valor inválido", 3: "quantidade deve ser maior ou igual a zero", 4: "preço unitário: valor inválido", 5: "preço unitário deve ser maior ou igual a zero"}
	for linha, trecho := range esperado {
		if !strings.Contains(motivos[linha], trecho) {
			t.Errorf("linha %d: erro = %q, want conter %q", linha, motivos[linha], trecho)
		}
	}
	if n := contarProdutos(t, db); n != 0 {
		t.Errorf("produtos = %d, want 0", n)
	}
	if n := contarProdutoEstoque(t, db); n != 0 {
		t.Errorf("produto_estoque = %d, want 0", n)
	}
}

func TestRelatorio_MantiveramValorAtual_CasosDeBorda(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-relatorio-borda@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	destino := depositoImp(t, db, empresaTeste)
	preco := 12.5

	casos := []struct {
		cod, qtd, prc string
		mantem        bool
	}{
		{"BD-OK", "8", "20", false},
		{"BD-0.0", "0.0", "20", true},
		{"BD-00", "00", "20", true},
		{"BD-NEG0", "-0", "20", true},
		{"BD-ARRED", "0.0004", "20", true},
		{"BD-P-ARRED", "8", "0.004", true},
	}
	ids := map[string]string{}
	linhas := [][]string{CabecalhoEsperado}
	for _, c := range casos {
		p, err := criarProdutoComSaldo(db, empresaTeste, CriarProdutoInput{
			UnidadeMedida: "un", Nome: "Produto " + c.cod, CategoriaID: categoriaID,
			TemplateID: templateGenericoID(t, db, empresaTeste), CodigoFornecedor: c.cod, PrecoUnitario: &preco,
		}, destino, 5)
		if err != nil {
			t.Fatal(err)
		}
		ids[c.cod] = p.ID
		l := linhaBase("Produto "+c.cod, c.cod, categoria, c.qtd, "")
		l[colPrecoUnitario] = c.prc
		linhas = append(linhas, l)
	}
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "p.xlsx", destino, linhas)
	if err != nil {
		t.Fatal(err)
	}
	if relatorio.Atualizados != 6 || relatorio.MantiveramValorAtual != 5 || relatorio.PreenchidasComZero != 0 {
		t.Fatalf("relatorio = %+v, want 6 atualizados, 5 mantiveram", relatorio)
	}
	for _, c := range casos {
		q := saldoProdutoEstoque(t, db, ids[c.cod], destino)
		var p float64
		_ = db.QueryRow(`SELECT preco_unitario FROM produtos WHERE id = $1`, ids[c.cod]).Scan(&p)
		wantQ, wantP := 8.0, 20.0
		if c.cod == "BD-0.0" || c.cod == "BD-00" || c.cod == "BD-NEG0" || c.cod == "BD-ARRED" {
			wantQ = 5
		}
		if c.cod == "BD-P-ARRED" {
			wantP = 12.5
		}
		if q != wantQ || p != wantP {
			t.Errorf("%s: saldo=%v preco=%v, want %v/%v", c.cod, q, p, wantQ, wantP)
		}
	}
	ultima, rel2, err := ObterUltimaImportacao(db, empresaTeste)
	if err != nil || ultima == nil {
		t.Fatalf("ultima=%v err=%v", ultima, err)
	}
	if rel2.MantiveramValorAtual != 5 || ultima.EstoqueNome == nil || *ultima.EstoqueNome != "Depósito Importação Teste" || !ultima.PorDeposito {
		t.Errorf("ObterUltimaImportacao: rel=%+v ultima=%+v", rel2, ultima)
	}
}

func TestCriarImportacao_ProdutoExistente_SemPar_QuantidadeZero_NaoCriaPar(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	criadoPor := criarUsuarioImportacao(t, db, "importacao-sem-par@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.001")
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	outro, err := CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Outro Depósito Sem Par")
	if err != nil {
		t.Fatal(err)
	}
	destino := depositoImp(t, db, empresaTeste)
	if _, err := criarProdutoComSaldo(db, empresaTeste, CriarProdutoInput{
		UnidadeMedida: "un", Nome: "Produto Sem Par", CategoriaID: categoriaID,
		TemplateID: templateGenericoID(t, db, empresaTeste), CodigoFornecedor: "FORN-SEM-PAR",
	}, outro.ID, 3); err != nil {
		t.Fatal(err)
	}
	_, relatorio, err := CriarImportacao(db, empresaTeste, criadoPor, "p.xlsx", destino, [][]string{
		CabecalhoEsperado, linhaBase("Produto Sem Par", "FORN-SEM-PAR", categoria, "", ""),
	})
	if err != nil || relatorio.Atualizados != 1 {
		t.Fatalf("relatorio=%+v err=%v", relatorio, err)
	}
	if n := contar(t, db, `SELECT count(*) FROM produto_estoque WHERE estoque_id = $1`, destino); n != 0 {
		t.Errorf("par criado no depósito escolhido: %d, want 0", n)
	}
}
