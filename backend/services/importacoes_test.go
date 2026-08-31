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
// devolvido.
func linhaBase(nome, codigo, categoria, quantidade, estoque string) []string {
	linha := make([]string, numeroColunas)
	linha[colNome] = nome
	linha[colCodigo] = codigo
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

	importacao, relatorio, err := CriarImportacao(db, criadoPor, "planilha.xlsx", linhas)
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

	importacao, relatorio, err := CriarImportacao(db, criadoPor, "planilha.xlsx", linhas)
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

	importacao, relatorio, err := CriarImportacao(db, criadoPor, "planilha.xlsx", linhas)
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

// TestCriarImportacao_EstoqueNovoReaproveitadoEntreLinhas prova a I/O Matrix
// "Duas linhas com o mesmo Estoque novo": só 1 linha é gravada em
// `estoques`, e a segunda linha reaproveita o mesmo `estoque_id`.
func TestCriarImportacao_EstoqueNovoReaproveitadoEntreLinhas(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	criadoPor := criarUsuarioImportacao(t, db, "importacao-estoque-dup@empresa.com")
	categoria := categoriaNomePorCodigo(t, db, "04.002")

	linhas := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Deposito B Um", "SKU-DB1", categoria, "1", "  Depósito B  "),
		linhaBase("Produto Deposito B Dois", "SKU-DB2", categoria, "1", "Depósito B"),
	}

	_, relatorio, err := CriarImportacao(db, criadoPor, "planilha.xlsx", linhas)
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
		WHERE p.codigo IN ('SKU-DB1', 'SKU-DB2')`)
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
// só ESSA linha deve ser reprocessada (ganhando um Produto NOVO, já que
// produtos não têm chave de deduplicação nesta story — Story 3.4); as
// outras duas devem permanecer com o `produto_id` original, intocadas.
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

	importacao, relatorio, err := CriarImportacao(db, criadoPor, "planilha.xlsx", linhas)
	if err != nil {
		t.Fatalf("CriarImportacao erro inesperado: %v", err)
	}
	if relatorio.Criados != 3 {
		t.Fatalf("Criados = %d, want 3", relatorio.Criados)
	}

	var linhaID4, produtoIDOriginal1, produtoIDOriginal2 string
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
	ultimaAntesDeContinuar, _, err := ObterUltimaImportacao(db)
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

	importacaoContinuada, relatorioContinuado, err := ContinuarImportacao(db, importacao.ID)
	if err != nil {
		t.Fatalf("ContinuarImportacao erro inesperado: %v", err)
	}
	if importacaoContinuada.Status != "concluida" {
		t.Errorf("status = %q, want concluida", importacaoContinuada.Status)
	}
	if importacaoContinuada.ProximaLinhaPendente != nil {
		t.Errorf("ProximaLinhaPendente = %v, want nil após concluir", *importacaoContinuada.ProximaLinhaPendente)
	}
	if relatorioContinuado.Criados != 3 {
		t.Errorf("Criados = %d, want 3 (as 2 originais + a linha reprocessada)", relatorioContinuado.Criados)
	}
	if n := contarProdutos(t, db); n != produtosAntes+1 {
		t.Errorf("linhas em produtos após continuar = %d, want %d (só a linha 4 gera Produto novo)", n, produtosAntes+1)
	}

	var produtoIDNovo1, produtoIDNovo2, produtoIDLinha3Depois string
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
	).Scan(&produtoIDLinha3Depois); err != nil {
		t.Fatalf("buscar produto_id linha 4 após continuar: %v", err)
	}
	if produtoIDNovo1 != produtoIDOriginal1 {
		t.Errorf("linha 2 foi reprocessada indevidamente: produto_id mudou de %q para %q", produtoIDOriginal1, produtoIDNovo1)
	}
	if produtoIDNovo2 != produtoIDOriginal2 {
		t.Errorf("linha 3 foi reprocessada indevidamente: produto_id mudou de %q para %q", produtoIDOriginal2, produtoIDNovo2)
	}
	if produtoIDLinha3Depois == "" {
		t.Error("linha 4 (resetada para pendente) não foi reprocessada — produto_id continua vazio")
	}
}

// TestObterUltimaImportacao_SemNenhumaImportacao prova que, sem nenhuma
// importação registrada, ObterUltimaImportacao devolve (nil, zero, nil) —
// nunca um erro.
func TestObterUltimaImportacao_SemNenhumaImportacao(t *testing.T) {
	db := testDB(t)

	importacao, relatorio, err := ObterUltimaImportacao(db)
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
	importacaoA, _, err := CriarImportacao(db, criadoPor, "planilha-a.xlsx", linhasA)
	if err != nil {
		t.Fatalf("CriarImportacao A: %v", err)
	}

	linhasB := [][]string{
		CabecalhoEsperado,
		linhaBase("Produto Ultima B", "SKU-ULT-B", categoria, "1", "Canteiro Ultima"),
	}
	importacaoB, _, err := CriarImportacao(db, criadoPor, "planilha-b.xlsx", linhasB)
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

	ultima, _, err := ObterUltimaImportacao(db)
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
	importacao, relatorio, err := CriarImportacao(db, criadoPor, "planilha.xlsx", linhas)
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

	ultima, _, err := ObterUltimaImportacao(db)
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

	importacao, relatorio, err := CriarImportacao(db, criadoPor, "planilha.xlsx", linhas)
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
		`SELECT pe.quantidade FROM produto_estoque pe JOIN produtos p ON p.id = pe.produto_id WHERE p.codigo = 'SKU-QOK'`,
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
			_, _, err := ContinuarImportacao(db, id)
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
// (mesma categoria/estoque para todas) e tem um `codigo` único
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
		INSERT INTO importacoes (nome_arquivo, total_linhas, criado_por)
		VALUES ($1, $2, $3) RETURNING id`
	if err := db.QueryRow(insertImportacao, "planilha-concorrencia.xlsx", numeroDeLinhas, criadoPor).Scan(&importacaoID); err != nil {
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
			_, _, err := ContinuarImportacao(db, importacaoID)
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
	// mesmo `codigo` — o que esta contagem pegaria.
	var produtosCriados int
	if err := db.QueryRow(`SELECT count(*) FROM produtos WHERE codigo LIKE 'SKU-CONC-%'`).Scan(&produtosCriados); err != nil {
		t.Fatalf("count produtos: %v", err)
	}
	if produtosCriados != totalLinhas {
		t.Errorf("produtos criados = %d, want %d — uma diferença indicaria alguma linha processada em duplicidade "+
			"(ou nenhuma vez) por duas chamadas concorrentes", produtosCriados, totalLinhas)
	}

	// Reforço final e mais direto ainda: cada `codigo` semeado corresponde a
	// EXATAMENTE um produto_id em produto_estoque, nunca dois.
	var codigosComMaisDeUmProduto int
	if err := db.QueryRow(`
		SELECT count(*) FROM (
			SELECT codigo FROM produtos WHERE codigo LIKE 'SKU-CONC-%' GROUP BY codigo HAVING count(*) > 1
		) duplicados`,
	).Scan(&codigosComMaisDeUmProduto); err != nil {
		t.Fatalf("count códigos duplicados: %v", err)
	}
	if codigosComMaisDeUmProduto != 0 {
		t.Errorf("%d código(s) com mais de um Produto — linha processada em duplicidade", codigosComMaisDeUmProduto)
	}
}
