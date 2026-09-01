package services

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/lib/pq"
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

// --- RegistrarTransferencia (Story 5.2, spec-5-2) --------------------------

// TestRegistrarTransferencia_Sucesso prova a AC1: transferência válida entre
// dois Estoques onde o destino já tem linha debita a origem, credita o
// destino e insere a Movimentação tipo='transferencia' com os dois lados
// preenchidos, tudo na mesma transação.
func TestRegistrarTransferencia_Sucesso(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueOrigemID, usuarioID := seedProdutoComSaldo(t, db, "Canteiro Transf Sucesso Origem", 10)
	estoqueDestino, err := CriarEstoque(db, "Canteiro Transf Sucesso Destino")
	if err != nil {
		t.Fatalf("seed CriarEstoque destino: %v", err)
	}
	// Destino já com linha (quantidade 2) — cenário "destino já tem linha".
	if _, err := db.Exec(
		`INSERT INTO produto_estoque (produto_id, estoque_id, quantidade) VALUES ($1, $2, 2)`,
		produtoID, estoqueDestino.ID,
	); err != nil {
		t.Fatalf("seed linha destino: %v", err)
	}

	mov, err := RegistrarTransferencia(db, produtoID, estoqueOrigemID, estoqueDestino.ID, usuarioID, 3)
	if err != nil {
		t.Fatalf("RegistrarTransferencia erro inesperado: %v", err)
	}
	if mov.ID == "" {
		t.Error("ID vazio no retorno")
	}
	if mov.Tipo != "transferencia" {
		t.Errorf("Tipo = %q, want transferencia", mov.Tipo)
	}
	if mov.EstoqueOrigemID != estoqueOrigemID {
		t.Errorf("EstoqueOrigemID = %q, want %q", mov.EstoqueOrigemID, estoqueOrigemID)
	}
	if mov.EstoqueDestinoID == nil || *mov.EstoqueDestinoID != estoqueDestino.ID {
		t.Errorf("EstoqueDestinoID = %v, want %q", mov.EstoqueDestinoID, estoqueDestino.ID)
	}
	if mov.Quantidade != 3 {
		t.Errorf("Quantidade = %v, want 3", mov.Quantidade)
	}
	if mov.UsuarioID != usuarioID {
		t.Errorf("UsuarioID = %q, want %q", mov.UsuarioID, usuarioID)
	}

	if saldo := saldoProdutoEstoque(t, db, produtoID, estoqueOrigemID); saldo != 7 {
		t.Errorf("saldo origem pós-transferência = %v, want 7", saldo)
	}
	if saldo := saldoProdutoEstoque(t, db, produtoID, estoqueDestino.ID); saldo != 5 {
		t.Errorf("saldo destino pós-transferência = %v, want 5", saldo)
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 1 {
		t.Errorf("Movimentacoes criadas = %d, want 1", n)
	}
}

// TestRegistrarTransferencia_DestinoSemLinhaAinda prova a linha "destino
// NUNCA teve linha nesse Estoque" da I/O Matrix: o upsert-lock
// (travarLinhaProdutoEstoque) cria a linha do destino com a quantidade
// transferida quando o Produto nunca esteve lá.
func TestRegistrarTransferencia_DestinoSemLinhaAinda(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueOrigemID, usuarioID := seedProdutoComSaldo(t, db, "Canteiro Transf SemLinha Origem", 10)
	estoqueDestino, err := CriarEstoque(db, "Canteiro Transf SemLinha Destino")
	if err != nil {
		t.Fatalf("seed CriarEstoque destino: %v", err)
	}

	mov, err := RegistrarTransferencia(db, produtoID, estoqueOrigemID, estoqueDestino.ID, usuarioID, 4)
	if err != nil {
		t.Fatalf("RegistrarTransferencia erro inesperado: %v", err)
	}
	if mov.EstoqueDestinoID == nil || *mov.EstoqueDestinoID != estoqueDestino.ID {
		t.Errorf("EstoqueDestinoID = %v, want %q", mov.EstoqueDestinoID, estoqueDestino.ID)
	}

	if saldo := saldoProdutoEstoque(t, db, produtoID, estoqueOrigemID); saldo != 6 {
		t.Errorf("saldo origem pós-transferência = %v, want 6", saldo)
	}
	if saldo := saldoProdutoEstoque(t, db, produtoID, estoqueDestino.ID); saldo != 4 {
		t.Errorf("saldo destino pós-transferência (linha nova) = %v, want 4", saldo)
	}
}

// TestRegistrarTransferencia_OrigemIgualDestino prova a AC2: origem ==
// destino é rejeitada com *ErroMovimentacaoValidacao ANTES de qualquer
// escrita, sem tocar o banco.
func TestRegistrarTransferencia_OrigemIgualDestino(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Canteiro Transf OrigemDestino", 10)

	_, err := RegistrarTransferencia(db, produtoID, estoqueID, estoqueID, usuarioID, 1)
	var erroValidacao *ErroMovimentacaoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroMovimentacaoValidacao", err)
	}
	if erroValidacao.Mensagem != "estoque de origem e destino devem ser diferentes" {
		t.Errorf("mensagem = %q, want %q", erroValidacao.Mensagem, "estoque de origem e destino devem ser diferentes")
	}

	if saldo := saldoProdutoEstoque(t, db, produtoID, estoqueID); saldo != 10 {
		t.Errorf("saldo não deveria ter mudado, got %v, want 10", saldo)
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 0 {
		t.Errorf("nenhuma Movimentação deveria ter sido criada, count = %d", n)
	}
}

// TestRegistrarTransferencia_OrigemIgualDestinoCaseInsensitive prova que o
// guard de "origem == destino" não escapa quando os dois ids referem-se ao
// MESMO Estoque com capitalização diferente (ex.: um chamador direto da API
// enviando um UUID em maiúsculas) — a comparação usa strings.EqualFold, não
// `==` puro.
func TestRegistrarTransferencia_OrigemIgualDestinoCaseInsensitive(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueID, usuarioID := seedProdutoComSaldo(t, db, "Canteiro Transf OrigemDestinoCase", 10)

	_, err := RegistrarTransferencia(db, produtoID, estoqueID, strings.ToUpper(estoqueID), usuarioID, 1)
	var erroValidacao *ErroMovimentacaoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("erro = %v, want *ErroMovimentacaoValidacao", err)
	}
	if erroValidacao.Mensagem != "estoque de origem e destino devem ser diferentes" {
		t.Errorf("mensagem = %q, want %q", erroValidacao.Mensagem, "estoque de origem e destino devem ser diferentes")
	}

	if saldo := saldoProdutoEstoque(t, db, produtoID, estoqueID); saldo != 10 {
		t.Errorf("saldo não deveria ter mudado, got %v, want 10", saldo)
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 0 {
		t.Errorf("nenhuma Movimentação deveria ter sido criada, count = %d", n)
	}
}

// TestRegistrarTransferencia_QuantidadeZeroOuNegativa prova que quantidade
// <= 0 OU acima de limiteNumeric103 é rejeitada com *ErroMovimentacaoValidacao
// ANTES de qualquer escrita (mesmo molde/constante de RegistrarBaixa) — sem
// a guarda de limite superior, um valor além de NUMERIC(10,3) vazaria como
// 500 de overflow na origem.
func TestRegistrarTransferencia_QuantidadeZeroOuNegativa(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueOrigemID, usuarioID := seedProdutoComSaldo(t, db, "Canteiro Transf QtdInvalida Origem", 10)
	estoqueDestino, err := CriarEstoque(db, "Canteiro Transf QtdInvalida Destino")
	if err != nil {
		t.Fatalf("seed CriarEstoque destino: %v", err)
	}

	for _, quantidade := range []float64{0, -5, limiteNumeric103 + 0.001, limiteNumeric103 * 10} {
		_, err := RegistrarTransferencia(db, produtoID, estoqueOrigemID, estoqueDestino.ID, usuarioID, quantidade)
		var erroValidacao *ErroMovimentacaoValidacao
		if !errors.As(err, &erroValidacao) {
			t.Fatalf("quantidade=%v: erro = %v, want *ErroMovimentacaoValidacao", quantidade, err)
		}
	}

	if saldo := saldoProdutoEstoque(t, db, produtoID, estoqueOrigemID); saldo != 10 {
		t.Errorf("saldo origem não deveria ter mudado, got %v, want 10", saldo)
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 0 {
		t.Errorf("nenhuma Movimentação deveria ter sido criada, count = %d", n)
	}
}

// TestRegistrarTransferencia_QuantidadeMaiorQueDisponivel prova a AC3:
// quantidade acima do saldo disponível na ORIGEM -> *ErroQuantidadeIndisponivel
// citando o saldo real da origem, nada debitado nem creditado.
func TestRegistrarTransferencia_QuantidadeMaiorQueDisponivel(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueOrigemID, usuarioID := seedProdutoComSaldo(t, db, "Canteiro Transf Excede Origem", 4.5)
	estoqueDestino, err := CriarEstoque(db, "Canteiro Transf Excede Destino")
	if err != nil {
		t.Fatalf("seed CriarEstoque destino: %v", err)
	}

	_, err = RegistrarTransferencia(db, produtoID, estoqueOrigemID, estoqueDestino.ID, usuarioID, 10)
	var erroIndisponivel *ErroQuantidadeIndisponivel
	if !errors.As(err, &erroIndisponivel) {
		t.Fatalf("erro = %v, want *ErroQuantidadeIndisponivel", err)
	}
	if erroIndisponivel.Disponivel != 4.5 {
		t.Errorf("Disponivel = %v, want 4.5 (saldo real da ORIGEM)", erroIndisponivel.Disponivel)
	}

	if saldo := saldoProdutoEstoque(t, db, produtoID, estoqueOrigemID); saldo != 4.5 {
		t.Errorf("saldo origem não deveria ter mudado, got %v, want 4.5", saldo)
	}
	var existeLinhaDestino bool
	if err := db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2)`,
		produtoID, estoqueDestino.ID,
	).Scan(&existeLinhaDestino); err != nil {
		t.Fatalf("checar linha destino: %v", err)
	}
	if existeLinhaDestino {
		t.Error("linha fantasma de destino sobreviveu ao rollback — upsert-lock deveria ter sido desfeito")
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 0 {
		t.Errorf("nenhuma Movimentação deveria ter sido criada, count = %d", n)
	}
}

// TestRegistrarTransferencia_EstoqueDestinoMalformadoOuInexistente prova o
// colapso "malformado/inexistente -> 0 disponível" (Design Notes de
// spec-5-2) também para o lado destino.
func TestRegistrarTransferencia_EstoqueDestinoMalformadoOuInexistente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueOrigemID, usuarioID := seedProdutoComSaldo(t, db, "Canteiro Transf DestinoRuim", 10)

	casos := map[string]string{
		"malformado":  "nao-e-um-uuid",
		"inexistente": "00000000-0000-0000-0000-000000000000",
	}
	for nome, destino := range casos {
		t.Run(nome, func(t *testing.T) {
			_, err := RegistrarTransferencia(db, produtoID, estoqueOrigemID, destino, usuarioID, 1)
			var erroIndisponivel *ErroQuantidadeIndisponivel
			if !errors.As(err, &erroIndisponivel) {
				t.Fatalf("erro = %v, want *ErroQuantidadeIndisponivel", err)
			}
			if erroIndisponivel.Disponivel != 0 {
				t.Errorf("Disponivel = %v, want 0", erroIndisponivel.Disponivel)
			}
		})
	}

	if saldo := saldoProdutoEstoque(t, db, produtoID, estoqueOrigemID); saldo != 10 {
		t.Errorf("saldo origem não deveria ter mudado, got %v, want 10", saldo)
	}
}

// TestRegistrarTransferencia_ConcorrenciaSemDeadlock prova a AC4/AD-10: duas
// transferências concorrentes entre os MESMOS dois Estoques, em direções
// OPOSTAS (T1: A->B, T2: B->A), montadas simultaneamente, nunca produzem um
// erro de deadlock do Postgres (SQLSTATE 40P01) — a ordenação canônica por
// estoque_id serializa as duas travas na mesma ordem física, uma espera a
// outra.
//
// O cenário é dimensionado para que AMBAS as transferências SEMPRE tenham
// saldo de sobra (saldo inicial 20/20, cada rodada move 3 de A->B e 3 de
// B->A — líquido zero, os saldos nunca descem): as duas DEVEM devolver nil
// e os saldos finais voltam exatos a 20/20. Aceitar
// `ErroQuantidadeIndisponivel` aqui, ou checar só a soma, deixaria passar
// uma transferência silenciosamente descartada.
//
// O par concorrente roda em `iteracoes` rodadas: uma regressão que troque a
// ordem de trava para origem-depois-destino só gera 40P01 sob um
// entrelaçamento específico — repetir torna o deadlock observável de forma
// confiável.
func TestRegistrarTransferencia_ConcorrenciaSemDeadlock(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueA, usuarioID := seedProdutoComSaldo(t, db, "Canteiro Transf Corrida A", 20)
	estoqueBRow, err := CriarEstoque(db, "Canteiro Transf Corrida B")
	if err != nil {
		t.Fatalf("seed CriarEstoque B: %v", err)
	}
	estoqueB := estoqueBRow.ID
	if _, err := db.Exec(
		`INSERT INTO produto_estoque (produto_id, estoque_id, quantidade) VALUES ($1, $2, 20)`,
		produtoID, estoqueB,
	); err != nil {
		t.Fatalf("seed saldo B: %v", err)
	}

	const iteracoes = 20
	for i := 0; i < iteracoes; i++ {
		start := make(chan struct{})
		var wg sync.WaitGroup
		var err1, err2 error

		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, err1 = RegistrarTransferencia(db, produtoID, estoqueA, estoqueB, usuarioID, 3) // A -> B
		}()
		go func() {
			defer wg.Done()
			<-start
			_, err2 = RegistrarTransferencia(db, produtoID, estoqueB, estoqueA, usuarioID, 3) // B -> A
		}()
		close(start)
		wg.Wait()

		for nome, err := range map[string]error{"T1 (A->B)": err1, "T2 (B->A)": err2} {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == "40P01" {
				t.Fatalf("iteração %d, %s: deadlock detectado pelo Postgres (40P01) — ordenação canônica falhou: %v", i, nome, err)
			}
			if err != nil {
				t.Fatalf("iteração %d, %s: erro inesperado (as duas transferências têm saldo de sobra): %v", i, nome, err)
			}
		}
	}

	// Cada rodada move 3 de A->B e 3 de B->A — líquido zero, os saldos
	// voltam exatos ao inicial.
	saldoA := saldoProdutoEstoque(t, db, produtoID, estoqueA)
	saldoB := saldoProdutoEstoque(t, db, produtoID, estoqueB)
	if saldoA < 0 || saldoB < 0 {
		t.Fatalf("saldo negativo: A=%v B=%v", saldoA, saldoB)
	}
	if saldoA != 20.0 || saldoB != 20.0 {
		t.Errorf("saldos finais = A:%v B:%v, want A:20 B:20", saldoA, saldoB)
	}
	if saldoA+saldoB != 40.0 {
		t.Errorf("soma dos saldos finais = %v, want 40 (nenhuma unidade perdida/criada)", saldoA+saldoB)
	}
}

// TestRegistrarTransferencia_ConcorrenciaMesmaOrigemNuncaFicaNegativo prova o
// invariante do epic-5-context ("nenhuma corrida entre transações
// concorrentes pode deixar o saldo negativo"): duas transferências
// simultâneas DRENANDO a mesma origem (saldo 10) para destinos diferentes,
// cada uma pedindo 6, não podem suceder as duas — o lock pessimista via
// travarLinhaProdutoEstoque serializa; a perdedora vê o saldo já debitado e
// devolve *ErroQuantidadeIndisponivel. Molde de
// TestRegistrarBaixa_ConcorrenciaDuasBaixasMesmaLinha.
func TestRegistrarTransferencia_ConcorrenciaMesmaOrigemNuncaFicaNegativo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID, estoqueOrigem, usuarioID := seedProdutoComSaldo(t, db, "Canteiro Transf DrenaOrigem", 10)
	destino1, err := CriarEstoque(db, "Canteiro Transf Drena Destino 1")
	if err != nil {
		t.Fatalf("seed CriarEstoque destino1: %v", err)
	}
	destino2, err := CriarEstoque(db, "Canteiro Transf Drena Destino 2")
	if err != nil {
		t.Fatalf("seed CriarEstoque destino2: %v", err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err1 = RegistrarTransferencia(db, produtoID, estoqueOrigem, destino1.ID, usuarioID, 6)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err2 = RegistrarTransferencia(db, produtoID, estoqueOrigem, destino2.ID, usuarioID, 6)
	}()
	close(start)
	wg.Wait()

	sucesso1, sucesso2 := err1 == nil, err2 == nil
	if sucesso1 && sucesso2 {
		t.Fatalf("as duas transferências de 6 (origem tinha 10) tiveram sucesso — origem ficaria negativa: err1=%v err2=%v", err1, err2)
	}
	if !sucesso1 && !sucesso2 {
		t.Fatalf("as duas falharam — uma deveria suceder (10 >= 6): err1=%v err2=%v", err1, err2)
	}

	erroPerdedora := err1
	if sucesso1 {
		erroPerdedora = err2
	}
	var erroIndisponivel *ErroQuantidadeIndisponivel
	if !errors.As(erroPerdedora, &erroIndisponivel) {
		t.Fatalf("erro da perdedora = %v, want *ErroQuantidadeIndisponivel", erroPerdedora)
	}
	if erroIndisponivel.Disponivel != 4 {
		t.Errorf("Disponivel visto pela perdedora = %v, want 4 (10 - 6 da vencedora)", erroIndisponivel.Disponivel)
	}

	if saldo := saldoProdutoEstoque(t, db, produtoID, estoqueOrigem); saldo != 4 {
		t.Errorf("saldo origem final = %v, want 4 (só a vencedora debitou)", saldo)
	}
	if n := contarMovimentacoes(t, db, produtoID); n != 1 {
		t.Errorf("Movimentacoes criadas = %d, want 1 (só a transferência vencedora)", n)
	}
}
