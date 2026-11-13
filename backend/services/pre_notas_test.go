package services

import (
	"bytes"
	"database/sql"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"
)

// --- Documento impresso da Pré-Nota — Story 20.5, spec-20-5 ----------------

// filialNomeadaTeste devolve (criando se preciso) uma Filial de empresaTeste
// com o nome dado — `filiais` não é truncada entre testes.
func filialNomeadaTeste(t *testing.T, db *sql.DB, nome string) string {
	t.Helper()
	var id string
	err := db.QueryRow(`SELECT id FROM filiais WHERE empresa_id = $1 AND nome = $2`, empresaTeste, nome).Scan(&id)
	if err == nil {
		return id
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("filialNomeadaTeste: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO filiais (empresa_id, nome) VALUES ($1, $2) RETURNING id`, empresaTeste, nome).Scan(&id); err != nil {
		t.Fatalf("filialNomeadaTeste: criar: %v", err)
	}
	return id
}

func definirPrecoProduto(t *testing.T, db *sql.DB, produtoID string, preco any) {
	t.Helper()
	if _, err := db.Exec(`UPDATE produtos SET preco_unitario = $2 WHERE id = $1 AND empresa_id = $3`, produtoID, preco, empresaTeste); err != nil {
		t.Fatalf("definir preco_unitario do produto %s: %v", produtoID, err)
	}
}

// snapshotItem lê o snapshot preço/unidade gravado em `pedido_itens` para o
// item (pedido, produto) de empresaTeste.
func snapshotItem(t *testing.T, db *sql.DB, pedidoID, produtoID string) (sql.NullString, sql.NullString) {
	t.Helper()
	var preco, unidade sql.NullString
	if err := db.QueryRow(
		`SELECT preco_unitario::text, unidade_medida::text FROM pedido_itens
		 WHERE pedido_id = $1 AND produto_id = $2 AND empresa_id = $3`, pedidoID, produtoID, empresaTeste,
	).Scan(&preco, &unidade); err != nil {
		t.Fatalf("ler snapshot de pedido_itens: %v", err)
	}
	return preco, unidade
}

func preNotaDoResultadoTeste(t *testing.T, resultado []PedidoDetalhe, pedidoID string) string {
	t.Helper()
	for _, det := range resultado {
		if det.ID == pedidoID {
			if det.PreNotaID == nil {
				t.Fatalf("pedido %s sem Pré-Nota na resposta do lote: %+v", pedidoID, det)
			}
			return *det.PreNotaID
		}
	}
	t.Fatalf("pedido %s ausente da resposta do lote", pedidoID)
	return ""
}

type cenarioPreNotaMista struct {
	preNotaID       string
	docnum          int
	pedidoTransfID  string
	produtoTransfID string
	produtoBaixaID  string
	gestorNome      string
}

// semearPreNotaMista monta a linha "Pré-Nota mista" da I/O Matrix: 1
// Transferência (6 un de um Produto a R$ 1.234,50, destino em OUTRA Filial)
// + 2 Baixas (Produto SEM preço) aprovadas no MESMO lote, com dois
// solicitantes distintos (um deles repetido).
func semearPreNotaMista(t *testing.T, db *sql.DB, prefixo string) cenarioPreNotaMista {
	t.Helper()
	c := cenarioPreNotaMista{gestorNome: prefixo + " Gestor"}
	gestorID := semearConta(t, db, c.gestorNome, slugTeste(prefixo)+"-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, prefixo+" Usuario", slugTeste(prefixo)+"-usuario@empresa.com", PapelUsuario, 1)

	produtoTransfID, estoqueOrigemID, _ := seedProdutoComSaldo(t, db, prefixo+" Transf", 10)
	definirPrecoProduto(t, db, produtoTransfID, "1234.50")
	estoqueDestino, err := CriarEstoque(db, empresaTeste, filialNomeadaTeste(t, db, "Filial Destino PreNota"), prefixo+" Destino")
	if err != nil {
		t.Fatalf("seed CriarEstoque destino: %v", err)
	}
	pedidoTransf, err := SubmeterPedidoTransferencia(db, empresaTeste, usuarioID, "Bruno", "Obra X", "", produtoTransfID, estoqueOrigemID, estoqueDestino.ID, 6)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoTransferencia: %v", err)
	}

	produtoBaixaID, estoqueBaixaID, _ := seedProdutoComSaldo(t, db, prefixo+" Baixa", 10)
	pedidoBaixa, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Ana", "Obra Y", "", produtoBaixaID, estoqueBaixaID, 4)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}
	pedidoBaixa2, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Ana", "Obra Y", "", produtoBaixaID, estoqueBaixaID, 1)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa 2: %v", err)
	}

	resultado, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedidoTransf.ID, pedidoBaixa.ID, pedidoBaixa2.ID}, true)
	if err != nil {
		t.Fatalf("DecidirLotePedidos: %v", err)
	}
	c.preNotaID = preNotaDoResultadoTeste(t, resultado, pedidoTransf.ID)
	for _, det := range resultado {
		if det.ID == pedidoTransf.ID && det.Docnum != nil {
			c.docnum = *det.Docnum
		}
	}
	c.pedidoTransfID = pedidoTransf.ID
	c.produtoTransfID = produtoTransfID
	c.produtoBaixaID = produtoBaixaID
	return c
}

func slugTeste(s string) string {
	b := []byte(s)
	for i, ch := range b {
		switch {
		case ch >= 'A' && ch <= 'Z':
			b[i] = ch + ('a' - 'A')
		case ch == ' ':
			b[i] = '-'
		}
	}
	return string(b)
}

// TestDecidirLotePedidos_GravaSnapshotPrecoEUnidadePreNota: a aprovação em
// lote grava em `pedido_itens` o preço/unidade do Produto do momento da
// decisão.
func TestDecidirLotePedidos_GravaSnapshotPrecoEUnidadePreNota(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	c := semearPreNotaMista(t, db, "PreNota Snap")

	preco, unidade := snapshotItem(t, db, c.pedidoTransfID, c.produtoTransfID)
	if !preco.Valid || preco.String != "1234.50" {
		t.Errorf("preco_unitario snapshot = %+v, want 1234.50", preco)
	}
	if !unidade.Valid || unidade.String != "un" {
		t.Errorf("unidade_medida snapshot = %+v, want un", unidade)
	}
}

// TestDecidirPedido_RetiradaNaoGravaSnapshotPreNota: o caminho de retirada
// (DecidirPedido) continua intocado — snapshot fica NULL.
func TestDecidirPedido_RetiradaNaoGravaSnapshotPreNota(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	usuarioID := semearConta(t, db, "PreNota Retirada Usuario", "prenota-retirada-usuario@empresa.com", PapelUsuario, 0)
	almoxID := semearConta(t, db, "PreNota Retirada Almox", "prenota-retirada-decisor@empresa.com", PapelAlmoxarife, 1)
	pedido := seedPedidoComItem(t, db, usuarioID, "PreNota Retirada", 2)

	var produtoID string
	if err := db.QueryRow(`SELECT produto_id FROM pedido_itens WHERE pedido_id = $1 AND empresa_id = $2`, pedido.ID, empresaTeste).Scan(&produtoID); err != nil {
		t.Fatalf("ler produto do pedido: %v", err)
	}
	definirPrecoProduto(t, db, produtoID, "10.00")

	if _, err := DecidirPedido(db, empresaTeste, pedido.ID, almoxID, PapelAlmoxarife, true); err != nil {
		t.Fatalf("DecidirPedido: %v", err)
	}
	preco, unidade := snapshotItem(t, db, pedido.ID, produtoID)
	if preco.Valid || unidade.Valid {
		t.Errorf("snapshot gravado em retirada: preco=%+v unidade=%+v, want NULL", preco, unidade)
	}
}

// TestDecidirLotePedidos_RejeitadoNaoGravaSnapshotPreNota: lote rejeitado não
// movimenta nada nem grava snapshot.
func TestDecidirLotePedidos_RejeitadoNaoGravaSnapshotPreNota(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "PreNota Rej Gestor", "prenota-rej-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "PreNota Rej Usuario", "prenota-rej-usuario@empresa.com", PapelUsuario, 1)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "PreNota Rej", 10)
	definirPrecoProduto(t, db, produtoID, "10.00")
	pedido, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 2)
	if err != nil {
		t.Fatalf("seed SubmeterPedidoBaixa: %v", err)
	}

	if _, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedido.ID}, false); err != nil {
		t.Fatalf("DecidirLotePedidos (rejeitar): %v", err)
	}
	preco, unidade := snapshotItem(t, db, pedido.ID, produtoID)
	if preco.Valid || unidade.Valid {
		t.Errorf("snapshot gravado em rejeição: preco=%+v unidade=%+v, want NULL", preco, unidade)
	}
}

// TestMontarPreNotaConteudo_MistaComPrecoAlteradoDepois cobre as linhas "Pré-
// Nota mista", "Preço alterado depois" e "Sem preço cadastrado" da I/O Matrix.
func TestMontarPreNotaConteudo_MistaComPrecoAlteradoDepois(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	c := semearPreNotaMista(t, db, "PreNota Mista")

	// Preço editado DEPOIS da aprovação (e um preço aparece no Produto que
	// não tinha) — o documento continua com o snapshot.
	definirPrecoProduto(t, db, c.produtoTransfID, "99.99")
	definirPrecoProduto(t, db, c.produtoBaixaID, "50.00")

	conteudo, err := MontarPreNotaConteudo(db, empresaTeste, c.preNotaID)
	if err != nil {
		t.Fatalf("MontarPreNotaConteudo: %v", err)
	}

	var razao, cnpj string
	if err := db.QueryRow(`SELECT razao_social, cnpj FROM empresas WHERE id = $1`, empresaTeste).Scan(&razao, &cnpj); err != nil {
		t.Fatalf("ler empresa: %v", err)
	}
	if conteudo.Docnum != c.docnum || c.docnum == 0 {
		t.Errorf("Docnum = %d, want %d", conteudo.Docnum, c.docnum)
	}
	if conteudo.EmpresaRazaoSocial != razao || conteudo.EmpresaCNPJ != strings.TrimSpace(cnpj) {
		t.Errorf("Empresa = %q/%q, want %q/%q", conteudo.EmpresaRazaoSocial, conteudo.EmpresaCNPJ, razao, cnpj)
	}
	if conteudo.Aprovador != c.gestorNome {
		t.Errorf("Aprovador = %q, want %q", conteudo.Aprovador, c.gestorNome)
	}
	if conteudo.CriadoEm.IsZero() {
		t.Error("CriadoEm zerado")
	}
	if !reflect.DeepEqual(conteudo.Solicitantes, []string{"Ana", "Bruno"}) {
		t.Errorf("Solicitantes = %v, want [Ana Bruno] (distintos, ordenados)", conteudo.Solicitantes)
	}

	if len(conteudo.Itens) != 3 {
		t.Fatalf("Itens = %+v, want 3", conteudo.Itens)
	}
	var transf *PreNotaItem
	baixas := 0
	for i := range conteudo.Itens {
		it := &conteudo.Itens[i]
		if it.ProdutoCodigo == "" {
			t.Errorf("item %q sem código interno", it.Descricao)
		}
		if it.UnidadeMedida == nil || *it.UnidadeMedida != "un" {
			t.Errorf("item %q: UnidadeMedida = %v, want un", it.Descricao, it.UnidadeMedida)
		}
		switch it.Tipo {
		case "transferencia":
			transf = it
		case "baixa":
			baixas++
			if it.PrecoCentavos != nil {
				t.Errorf("baixa: PrecoCentavos = %v, want nil (sem preço na aprovação)", *it.PrecoCentavos)
			}
			if it.FilialDestino != "" {
				t.Errorf("baixa: FilialDestino = %q, want vazio", it.FilialDestino)
			}
			if it.FilialOrigem == "" {
				t.Error("baixa: FilialOrigem vazia")
			}
		}
	}
	if transf == nil || baixas != 2 {
		t.Fatalf("itens = %+v, want 1 transferencia + 2 baixas", conteudo.Itens)
	}
	if transf.QuantidadeMilesimos != 6000 {
		t.Errorf("transf: QuantidadeMilesimos = %d, want 6000", transf.QuantidadeMilesimos)
	}
	if transf.PrecoCentavos == nil || *transf.PrecoCentavos != 123450 {
		t.Errorf("transf: PrecoCentavos = %v, want 123450 (snapshot, não 9999)", transf.PrecoCentavos)
	}
	if transf.FilialDestino != "Filial Destino PreNota" {
		t.Errorf("transf: FilialDestino = %q, want Filial Destino PreNota", transf.FilialDestino)
	}
	if transf.FilialOrigem == "" || transf.FilialOrigem == transf.FilialDestino {
		t.Errorf("transf: FilialOrigem = %q, want filial de origem distinta", transf.FilialOrigem)
	}

	// Texto impresso a partir do conteúdo montado do banco.
	texto := strings.Join(linhasPreNota(conteudo), "\n")
	for _, want := range []string{
		"Valor unitário: R$ 1.234,50   Valor da linha: R$ 7.407,00",
		"Total: R$ 7.407,00 (há itens sem valor cadastrado)",
		"Data: " + conteudo.CriadoEm.In(localPreNota).Format("02/01/2006 15:04"),
	} {
		if !strings.Contains(texto, want) {
			t.Errorf("texto da Pré-Nota sem %q:\n%s", want, texto)
		}
	}
}

// TestMontarPreNotaConteudo_ItemZeradoNaoAparece cobre "Item zerado": item
// aprovado com quantidade_aprovada = 0 não entra no documento, e o
// solicitante de um Pedido todo zerado não entra no cabeçalho.
func TestMontarPreNotaConteudo_ItemZeradoNaoAparece(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "PreNota Zero Gestor", "prenota-zero-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "PreNota Zero Usuario", "prenota-zero-usuario@empresa.com", PapelUsuario, 1)

	produtoA, estoqueA, _ := seedProdutoComSaldo(t, db, "PreNota Zero A", 10)
	pedidoA, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Zerado", "Obra X", "", produtoA, estoqueA, 5)
	if err != nil {
		t.Fatalf("seed A: %v", err)
	}
	produtoB, estoqueB, _ := seedProdutoComSaldo(t, db, "PreNota Zero B", 10)
	pedidoB, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra Y", "", produtoB, estoqueB, 2)
	if err != nil {
		t.Fatalf("seed B: %v", err)
	}
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 0 WHERE produto_id = $1 AND estoque_id = $2`, produtoA, estoqueA); err != nil {
		t.Fatalf("zerar saldo de A: %v", err)
	}

	resultado, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedidoA.ID, pedidoB.ID}, true)
	if err != nil {
		t.Fatalf("DecidirLotePedidos: %v", err)
	}
	for _, det := range resultado {
		if det.ID == pedidoA.ID && (len(det.Itens) != 1 || det.Itens[0].QuantidadeAprovada == nil || *det.Itens[0].QuantidadeAprovada != 0) {
			t.Fatalf("pré-condição: item de A deveria sair com 0, got %+v", det.Itens)
		}
	}

	conteudo, err := MontarPreNotaConteudo(db, empresaTeste, preNotaDoResultadoTeste(t, resultado, pedidoB.ID))
	if err != nil {
		t.Fatalf("MontarPreNotaConteudo: %v", err)
	}
	if len(conteudo.Itens) != 1 || conteudo.Itens[0].Descricao != "Produto PreNota Zero B" || conteudo.Itens[0].QuantidadeMilesimos != 2000 {
		t.Errorf("Itens = %+v, want só o item de B (2)", conteudo.Itens)
	}
	if !reflect.DeepEqual(conteudo.Solicitantes, []string{"Fulano"}) {
		t.Errorf("Solicitantes = %v, want [Fulano] (sem o do Pedido todo zerado)", conteudo.Solicitantes)
	}
}

// TestMontarPreNotaConteudo_NenhumItemMovimentado: lote cujo único item saiu
// zerado ainda gera Pré-Nota — o documento diz que nada foi movimentado.
func TestMontarPreNotaConteudo_NenhumItemMovimentado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	gestorID := semearConta(t, db, "PreNota Vazia Gestor", "prenota-vazia-gestor@empresa.com", PapelGestor, 0)
	usuarioID := semearConta(t, db, "PreNota Vazia Usuario", "prenota-vazia-usuario@empresa.com", PapelUsuario, 1)
	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "PreNota Vazia", 10)
	pedido, err := SubmeterPedidoBaixa(db, empresaTeste, usuarioID, "Fulano", "Obra X", "", produtoID, estoqueID, 3)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.Exec(`UPDATE produto_estoque SET quantidade = 0 WHERE produto_id = $1 AND estoque_id = $2`, produtoID, estoqueID); err != nil {
		t.Fatalf("zerar saldo: %v", err)
	}
	resultado, err := DecidirLotePedidos(db, empresaTeste, gestorID, []string{pedido.ID}, true)
	if err != nil {
		t.Fatalf("DecidirLotePedidos: %v", err)
	}

	conteudo, err := MontarPreNotaConteudo(db, empresaTeste, preNotaDoResultadoTeste(t, resultado, pedido.ID))
	if err != nil {
		t.Fatalf("MontarPreNotaConteudo: %v", err)
	}
	if len(conteudo.Itens) != 0 || len(conteudo.Solicitantes) != 0 {
		t.Errorf("conteúdo = %+v, want sem itens e sem solicitantes", conteudo)
	}
	linhas := linhasPreNota(conteudo)
	if !contemLinha(linhas, "Nenhum item movimentado nesta Pré-Nota") || contemLinha(linhas, "Itens") {
		t.Errorf("linhas = %q, want mensagem de nenhum item e sem seção Itens", linhas)
	}
	if _, err := RenderizarPreNotaPDF(conteudo); err != nil {
		t.Errorf("RenderizarPreNotaPDF sem itens: %v", err)
	}
}

// TestMontarPreNotaConteudo_NaoEncontrada cobre "Outra Empresa / inexistente":
// id malformado, inexistente ou de outra Empresa -> ErrPreNotaNaoEncontrada.
func TestMontarPreNotaConteudo_NaoEncontrada(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	c := semearPreNotaMista(t, db, "PreNota Isol")

	removerEmpresaDeTeste(t, db, "outra-empresa-prenota-pdf")
	outra := criarEmpresaDeTeste(t, db, "outra-empresa-prenota-pdf", "777888990155", "Outra Empresa PreNota PDF")
	t.Cleanup(func() { removerEmpresaDeTeste(t, db, "outra-empresa-prenota-pdf") })

	casos := map[string][2]string{
		"malformado":    {empresaTeste, "nao-e-uuid"},
		"inexistente":   {empresaTeste, "00000000-0000-0000-0000-000000000000"},
		"outra Empresa": {outra.ID, c.preNotaID},
	}
	for nome, caso := range casos {
		t.Run(nome, func(t *testing.T) {
			if _, err := MontarPreNotaConteudo(db, caso[0], caso[1]); !errors.Is(err, ErrPreNotaNaoEncontrada) {
				t.Fatalf("erro = %v, want ErrPreNotaNaoEncontrada", err)
			}
			if _, _, err := GerarPreNotaPDF(db, caso[0], caso[1]); !errors.Is(err, ErrPreNotaNaoEncontrada) {
				t.Fatalf("GerarPreNotaPDF erro = %v, want ErrPreNotaNaoEncontrada", err)
			}
		})
	}
}

// TestGerarPreNotaPDF_PrecoEditadoNaoMudaDocumento: duas gerações da MESMA
// Pré-Nota produzem os mesmos bytes, mesmo com o PREÇO do Produto editado
// entre elas (preço/unidade são snapshot). Nomes — código, Filiais, Empresa,
// Aprovador — seguem o cadastro atual por decisão do usuário (Spec Change
// Log de spec-20-5) e por isso não são editados aqui.
func TestGerarPreNotaPDF_PrecoEditadoNaoMudaDocumento(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	c := semearPreNotaMista(t, db, "PreNota Det")

	pdf1, docnum, err := GerarPreNotaPDF(db, empresaTeste, c.preNotaID)
	if err != nil {
		t.Fatalf("GerarPreNotaPDF (1): %v", err)
	}
	if docnum != c.docnum {
		t.Errorf("docnum = %d, want %d", docnum, c.docnum)
	}
	if !bytes.HasPrefix(pdf1, []byte("%PDF-")) {
		t.Fatalf("saída não começa com %%PDF-: %q", pdf1[:min(len(pdf1), 16)])
	}

	definirPrecoProduto(t, db, c.produtoTransfID, "1.23")
	definirPrecoProduto(t, db, c.produtoBaixaID, "4.56")

	pdf2, _, err := GerarPreNotaPDF(db, empresaTeste, c.preNotaID)
	if err != nil {
		t.Fatalf("GerarPreNotaPDF (2): %v", err)
	}
	if !bytes.Equal(pdf1, pdf2) {
		t.Error("PDF da Pré-Nota mudou após editar só o preço do Produto")
	}
}

// --- Texto impresso (funções puras, sem banco) -----------------------------

func ptrI64(v int64) *int64 { return &v }

func contemLinha(linhas []string, want string) bool {
	for _, l := range linhas {
		if l == want {
			return true
		}
	}
	return false
}

// conteudoMistoTeste: 18:04 UTC = 15:04 em São Paulo (UTC-3, sem horário de
// verão desde 2019).
func conteudoMistoTeste() PreNotaConteudo {
	return PreNotaConteudo{
		Docnum:             42,
		EmpresaRazaoSocial: "Ferreira Costa & Cia LTDA",
		EmpresaCNPJ:        "12345678000199",
		CriadoEm:           time.Date(2026, 11, 13, 18, 4, 0, 0, time.UTC),
		Aprovador:          "Gestor Ávila",
		Solicitantes:       []string{"Ana", "Bruno"},
		Itens: []PreNotaItem{
			{Tipo: "transferencia", ProdutoCodigo: "000123", Descricao: "Cimento CP-II 50kg", QuantidadeMilesimos: 6000,
				UnidadeMedida: ptrStr("un"), PrecoCentavos: ptrI64(123450), FilialOrigem: "Recife", FilialDestino: "Natal"},
			{Tipo: "baixa", ProdutoCodigo: "000456", Descricao: "Areia média", QuantidadeMilesimos: 2500,
				UnidadeMedida: ptrStr("m3"), PrecoCentavos: ptrI64(1001), FilialOrigem: "Recife"},
		},
	}
}

func TestLinhasPreNota_Mista(t *testing.T) {
	got := linhasPreNota(conteudoMistoTeste())
	want := []string{
		"Pré-Nota nº 42",
		"Empresa: Ferreira Costa & Cia LTDA",
		"CNPJ: 12.345.678/0001-99",
		"Data: 13/11/2026 15:04",
		"Aprovador: Gestor Ávila",
		"Solicitante(s): Ana, Bruno",
		"Itens",
		"1. Produto: 000123   Descrição: Cimento CP-II 50kg   (Transferência)",
		"Quantidade: 6   Unidade: un   Valor unitário: R$ 1.234,50   Valor da linha: R$ 7.407,00",
		"Filial origem: Recife   Filial destino: Natal",
		"2. Produto: 000456   Descrição: Areia média   (Baixa)",
		// 2,5 × 10,01 = 25,025 -> 25,03 (meio para cima, em inteiros)
		"Quantidade: 2,5   Unidade: m3   Valor unitário: R$ 10,01   Valor da linha: R$ 25,03",
		"Filial origem: Recife",
		"Total: R$ 7.432,03",
		"Documento interno de controle — não substitui a Nota Fiscal",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("linhas =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestLinhasPreNota_TotaisComItemSemPreco cobre a linha "Totais com item sem
// preço" da I/O Matrix: R$ 10,00 × 3 + 1 item sem preço.
func TestLinhasPreNota_TotaisComItemSemPreco(t *testing.T) {
	c := PreNotaConteudo{Docnum: 1, Itens: []PreNotaItem{
		{Tipo: "baixa", ProdutoCodigo: "1", Descricao: "Com preço", QuantidadeMilesimos: 3000, UnidadeMedida: ptrStr("un"), PrecoCentavos: ptrI64(1000)},
		{Tipo: "baixa", ProdutoCodigo: "2", Descricao: "Sem preço", QuantidadeMilesimos: 1000},
	}}
	linhas := linhasPreNota(c)
	for _, want := range []string{
		"Quantidade: 3   Unidade: un   Valor unitário: R$ 10,00   Valor da linha: R$ 30,00",
		"Quantidade: 1   Unidade: —   Valor unitário: —   Valor da linha: —",
		"Total: R$ 30,00 (há itens sem valor cadastrado)",
		RodapePreNota,
	} {
		if !contemLinha(linhas, want) {
			t.Errorf("linhas sem %q:\n%s", want, strings.Join(linhas, "\n"))
		}
	}
}

func TestLinhasPreNota_SemItensEDadosAusentes(t *testing.T) {
	linhas := linhasPreNota(PreNotaConteudo{Docnum: 3, CriadoEm: time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)})
	want := []string{
		"Pré-Nota nº 3",
		"Empresa: —",
		"CNPJ: —",
		"Data: 02/01/2026 00:00",
		"Aprovador: —",
		"Solicitante(s): —",
		"Nenhum item movimentado nesta Pré-Nota",
		RodapePreNota,
	}
	if !reflect.DeepEqual(linhas, want) {
		t.Errorf("linhas =\n%s\nwant\n%s", strings.Join(linhas, "\n"), strings.Join(want, "\n"))
	}
}

func TestLinhasPreNota_TipoDesconhecidoImpressoComoVeio(t *testing.T) {
	linhas := linhasPreNota(PreNotaConteudo{Itens: []PreNotaItem{{Tipo: "retirada", Descricao: "X", QuantidadeMilesimos: 1000}}})
	if !contemLinha(linhas, "1. Produto: —   Descrição: X   (retirada)") {
		t.Errorf("linhas = %q, want tipo desconhecido impresso como veio", linhas)
	}
	if !contemLinha(linhas, "Filial origem: —") {
		t.Errorf("linhas = %q, want sem Filial destino para tipo desconhecido", linhas)
	}
}

// TestRenderizarPreNotaPDF_TextoLongoEMuitosItens: textos longos (quebrados
// em várias linhas) e muitos itens (várias páginas) ainda geram um PDF
// válido e determinístico.
func TestRenderizarPreNotaPDF_TextoLongoEMuitosItens(t *testing.T) {
	c := conteudoMistoTeste()
	c.EmpresaRazaoSocial = strings.Repeat("Razão Social Muito Comprida ", 12)
	for i := 0; i < 40; i++ {
		c.Solicitantes = append(c.Solicitantes, "Solicitante Número "+strings.Repeat("X", i%7))
	}
	for i := 0; i < 80; i++ {
		c.Itens = append(c.Itens, PreNotaItem{Tipo: "baixa", Descricao: strings.Repeat("Descrição longa ", 15), QuantidadeMilesimos: 1500})
	}
	a, err := RenderizarPreNotaPDF(c)
	if err != nil {
		t.Fatalf("RenderizarPreNotaPDF: %v", err)
	}
	b, err := RenderizarPreNotaPDF(c)
	if err != nil {
		t.Fatalf("RenderizarPreNotaPDF (2): %v", err)
	}
	if !bytes.HasPrefix(a, []byte("%PDF-")) || !bytes.Equal(a, b) {
		t.Error("PDF inválido ou não determinístico")
	}
}

func TestFormatacoesPreNota(t *testing.T) {
	if got := formatarCNPJPreNota("12345678000199"); got != "12.345.678/0001-99" {
		t.Errorf("CNPJ = %q", got)
	}
	valores := map[int64]string{0: "R$ 0,00", 5: "R$ 0,05", 1250: "R$ 12,50", 123450: "R$ 1.234,50", 123456789: "R$ 1.234.567,89"}
	for v, want := range valores {
		if got := formatarCentavosPreNota(big.NewInt(v)); got != want {
			t.Errorf("centavos %d = %q, want %q", v, got, want)
		}
	}
	if got := formatarPrecoPreNota(nil); got != "—" {
		t.Errorf("preço nil = %q, want —", got)
	}
	quantidades := map[int64]string{6000: "6", 2500: "2,5", 1: "0,001", 1234500: "1.234,5"}
	for v, want := range quantidades {
		if got := formatarQuantidadePreNota(v); got != want {
			t.Errorf("quantidade %d = %q, want %q", v, got, want)
		}
	}
	linhas := []struct {
		milesimos, centavos int64
		want                string
	}{
		{1, 1, "0"},          // 0,00001 -> 0
		{500, 1, "1"},        // 0,005 -> 0,01 (meio para cima)
		{2500, 1001, "2503"}, // 25,025 -> 25,03
		{9999999999, 999999999999, "9999999998990000000"}, // sem overflow
	}
	for _, c := range linhas {
		if got := valorLinhaCentavosPreNota(c.milesimos, c.centavos).String(); got != c.want {
			t.Errorf("valorLinha(%d, %d) = %s, want %s", c.milesimos, c.centavos, got, c.want)
		}
	}
}
