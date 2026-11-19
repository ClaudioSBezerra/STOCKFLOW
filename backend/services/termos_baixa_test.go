package services

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func conteudoTermoTeste(motivo string) TermoBaixaConteudo {
	return TermoBaixaConteudo{
		Numero:             1,
		Motivo:             motivo,
		EmpresaRazaoSocial: "Ferreira Costa & Cia LTDA",
		EmpresaCNPJ:        "12345678000199",
		CriadoEm:           time.Date(2026, 11, 19, 1, 30, 0, 0, time.UTC), // 22:30 do dia 18 em São Paulo
		Aprovador:          "Gestor Ávila",
		Solicitantes:       []string{"Ana", "Bruno"},
		Justificativas: []JustificativaTermoBaixa{
			{PedidoID: "p1", Solicitante: "Ana", Texto: "Furto no galpão 3, BO 123", Obra: "Obra Norte"},
			{PedidoID: "p2", Solicitante: "Bruno", Texto: "Material molhado pela chuva"},
		},
		Itens: []PreNotaItem{
			{Tipo: "baixa", ProdutoCodigo: "000123", Descricao: "Cimento CP-II 50kg", QuantidadeMilesimos: 6000, QuantidadeSolicitadaMilesimos: 8000, PedidoID: "p1",
				UnidadeMedida: ptrStr("un"), PrecoCentavos: ptrI64(123450), FilialOrigem: "Recife",
				EstoqueOrigem: "Almoxarifado Central", EstoqueOrigemID: "e1", FiscalOrigem: fiscalRecifeTeste()},
			{Tipo: "baixa", ProdutoCodigo: "000456", Descricao: "Areia média", QuantidadeMilesimos: 2500, QuantidadeSolicitadaMilesimos: 2500, PedidoID: "p1",
				FilialOrigem: "Recife", EstoqueOrigem: "Almoxarifado Central", EstoqueOrigemID: "e1", FiscalOrigem: fiscalRecifeTeste()},
			{Tipo: "baixa", ProdutoCodigo: "000789", Descricao: "Brita", QuantidadeMilesimos: 1000, QuantidadeSolicitadaMilesimos: 1000, PedidoID: "p2",
				UnidadeMedida: ptrStr("m3"), PrecoCentavos: ptrI64(5000), FilialOrigem: "Recife",
				EstoqueOrigem: "Canteiro", EstoqueOrigemID: "e2", FiscalOrigem: fiscalRecifeTeste()},
		},
	}
}

func TestFaixaMotivoTermoBaixa(t *testing.T) {
	casos := map[string]string{
		"perda_roubo":      "BAIXA DE ESTOQUE POR PERDA OU ROUBO",
		"vencido_avariado": "PRODUTO VENCIDO OU AVARIADO",
		"utilizacao_obra":  "SAÍDA POR UTILIZAÇÃO NA OBRA DA EMPRESA",
		"":                 "BAIXA DE ESTOQUE — MOTIVO NÃO INFORMADO",
	}
	for m, want := range casos {
		if got := FaixaMotivoTermoBaixa(m); got != want {
			t.Errorf("FaixaMotivoTermoBaixa(%q) = %q, want %q", m, got, want)
		}
	}
}

func TestLayoutTermoBaixa_CabecalhoRodapeEOrigens(t *testing.T) {
	l := layoutDoTermoBaixa(conteudoTermoTeste("perda_roubo"))
	if l.Numero != "Nº 000.001" || l.Emissao != "Emissão: 18/11/2026 22:30" {
		t.Errorf("Numero/Emissao = %q / %q", l.Numero, l.Emissao)
	}
	if l.Motivo != "BAIXA DE ESTOQUE POR PERDA OU ROUBO" {
		t.Errorf("Motivo = %q", l.Motivo)
	}
	if l.Rodape != "TERMO DE BAIXA Nº 000.001 — BAIXA DE ESTOQUE POR PERDA OU ROUBO" {
		t.Errorf("Rodape = %q", l.Rodape)
	}
	if len(l.Pedidos) != 2 {
		t.Fatalf("Pedidos = %d, want 2", len(l.Pedidos))
	}
	o := l.Pedidos[0].Origens[0]
	if len(l.Pedidos[0].Origens) != 1 || o.Origem.Estoque != "Almoxarifado Central" || len(o.Produtos) != 2 {
		t.Errorf("pedido 0 = %+v", l.Pedidos[0])
	}
	if o.Produtos[0].Solicitada != "8" || o.Produtos[0].Quantidade != "6" {
		t.Errorf("solicitada/aprovada = %q / %q, want 8 / 6", o.Produtos[0].Solicitada, o.Produtos[0].Quantidade)
	}
	// 6 × 1.234,50 = 7.407,00; a Areia não tem preço -> aviso no subtotal e no total.
	if o.Subtotal != "R$ 7.407,00"+avisoTotalSemValor {
		t.Errorf("Subtotal = %q", o.Subtotal)
	}
	if o.Produtos[1].ValorTotal != textoAusentePreNota || o.Produtos[1].Unidade != textoAusentePreNota {
		t.Errorf("item sem preço/unidade = %+v", o.Produtos[1])
	}
	if o2 := l.Pedidos[1].Origens[0]; o2.Origem.Estoque != "Canteiro" || o2.Subtotal != "R$ 50,00" {
		t.Errorf("pedido 1 = %+v", l.Pedidos[1])
	}
	if l.ValorTotal != "R$ 7.457,00"+avisoTotalSemValor {
		t.Errorf("ValorTotal = %q", l.ValorTotal)
	}
	if l.Solicitantes != "Ana, Bruno" || l.Aprovador != "Gestor Ávila" {
		t.Errorf("Solicitantes/Aprovador = %q / %q", l.Solicitantes, l.Aprovador)
	}
	if len(l.Assinaturas) != 2 || l.Assinaturas[0].Texto != "SOLICITANTE" || l.Assinaturas[1].Texto != "APROVADOR" {
		t.Errorf("Assinaturas = %+v", l.Assinaturas)
	}
}

func TestLayoutTermoBaixa_JustificativaPorPedido(t *testing.T) {
	l := layoutDoTermoBaixa(conteudoTermoTeste("utilizacao_obra"))
	if len(l.Pedidos) != 2 {
		t.Fatalf("Pedidos = %d", len(l.Pedidos))
	}
	a, b := l.Pedidos[0], l.Pedidos[1]
	if a.Solicitante != "Ana" || a.Justificativa != "Furto no galpão 3, BO 123" || a.Obra != "Obra Norte" {
		t.Errorf("pedido Ana = %+v", a)
	}
	if b.Solicitante != "Bruno" || b.Justificativa != "Material molhado pela chuva" || b.Obra != "" {
		t.Errorf("pedido Bruno = %+v", b)
	}
	if l.Motivo != "SAÍDA POR UTILIZAÇÃO NA OBRA DA EMPRESA" {
		t.Errorf("Motivo = %q", l.Motivo)
	}
}

func TestLayoutTermoBaixa_JustificativaLegadaVazia(t *testing.T) {
	c := conteudoTermoTeste("")
	c.Justificativas = []JustificativaTermoBaixa{{PedidoID: "p1", Solicitante: "Ana"}, {PedidoID: "p2", Solicitante: "Bruno", Texto: "   "}}
	l := layoutDoTermoBaixa(c)
	want := "Sem justificativa informada (pedido anterior à exigência)"
	if len(l.Pedidos) != 2 || l.Pedidos[0].Justificativa != want || l.Pedidos[1].Justificativa != want {
		t.Errorf("Pedidos = %+v", l.Pedidos)
	}
}

func TestLayoutTermoBaixa_ItemDePedidoDesconhecidoNaoSeperde(t *testing.T) {
	c := conteudoTermoTeste("perda_roubo")
	c.Itens[0].PedidoID = "fantasma"
	l := layoutDoTermoBaixa(c)
	if len(l.Pedidos) != 3 || len(l.Pedidos[2].Origens) != 1 || l.Pedidos[2].Justificativa != textoSemJustificativaTermoBaixa {
		t.Errorf("Pedidos = %+v", l.Pedidos)
	}
}

func TestPintorPDF_GuardaORotuloDoDocumentoParaAsMensagensDeErro(t *testing.T) {
	for _, rotulo := range []string{"da Pré-Nota", "do Termo de Baixa"} {
		p, err := novoPintorPDF("rodapé", rotulo)
		if err != nil {
			t.Fatal(err)
		}
		if p.rotulo != rotulo {
			t.Errorf("rotulo = %q, want %q", p.rotulo, rotulo)
		}
	}
}

func TestVinculosDeDocumento_RotulosEConsultasFechadas(t *testing.T) {
	if vinculoPreNota.rotulo != "da Pré-Nota" || vinculoTermoBaixa.rotulo != "do Termo de Baixa" {
		t.Errorf("rótulos = %q / %q", vinculoPreNota.rotulo, vinculoTermoBaixa.rotulo)
	}
	if !strings.Contains(vinculoPreNota.sql, "p.pre_nota_id = $1") || strings.Contains(vinculoPreNota.sql, "termo_baixa_id") {
		t.Error("consulta da Pré-Nota deveria filtrar só por pre_nota_id")
	}
	if !strings.Contains(vinculoTermoBaixa.sql, "p.termo_baixa_id = $1") || strings.Contains(vinculoTermoBaixa.sql, "pre_nota_id") {
		t.Error("consulta do Termo deveria filtrar só por termo_baixa_id")
	}
}

func TestLayoutTermoBaixa_SemMotivoESemItens(t *testing.T) {
	l := layoutDoTermoBaixa(TermoBaixaConteudo{Numero: 12})
	if l.Motivo != "BAIXA DE ESTOQUE — MOTIVO NÃO INFORMADO" || len(l.Pedidos) != 0 || l.ValorTotal != "" {
		t.Errorf("layout = %+v", l)
	}
	if !strings.HasSuffix(l.Rodape, "MOTIVO NÃO INFORMADO") || !strings.HasPrefix(l.Rodape, "TERMO DE BAIXA Nº 000.012") {
		t.Errorf("Rodape = %q", l.Rodape)
	}
	if _, err := RenderizarTermoBaixaPDF(TermoBaixaConteudo{Numero: 12}); err != nil {
		t.Errorf("RenderizarTermoBaixaPDF sem itens: %v", err)
	}
}

func TestRenderizarTermoBaixaPDF_DeterministicoEVariasPaginas(t *testing.T) {
	c := conteudoTermoTeste("vencido_avariado")
	c.EmpresaRazaoSocial = strings.Repeat("Razão Social Muito Comprida ", 8)
	for i := 0; i < 60; i++ {
		c.Itens = append(c.Itens, PreNotaItem{Tipo: "baixa", Descricao: strings.Repeat("Descrição longa ", 8), QuantidadeMilesimos: 1500, PedidoID: "p1",
			FilialOrigem: "Recife", EstoqueOrigem: "Almoxarifado Central", EstoqueOrigemID: "e1"})
	}
	for i := 0; i < 40; i++ {
		c.Justificativas = append(c.Justificativas, JustificativaTermoBaixa{PedidoID: "x", Solicitante: "Fulano", Texto: strings.Repeat("texto da justificativa ", 12)})
	}
	a, err := RenderizarTermoBaixaPDF(c)
	if err != nil {
		t.Fatalf("RenderizarTermoBaixaPDF: %v", err)
	}
	b, _ := RenderizarTermoBaixaPDF(c)
	if !bytes.HasPrefix(a, []byte("%PDF-")) || !bytes.Equal(a, b) {
		t.Error("PDF inválido ou não determinístico")
	}
	if n := paginasPDFTeste(a); n < 3 {
		t.Errorf("páginas = %d, want >= 3", n)
	}
}

func TestRenderizarTermoBaixaPDF_JustificativaLongaQuebraPagina(t *testing.T) {
	c := conteudoTermoTeste("perda_roubo")
	c.Justificativas[0].Texto = strings.Repeat("Furto no galpão, conforme boletim de ocorrência. ", 32) // ~1500 caracteres
	a, err := RenderizarTermoBaixaPDF(c)
	if err != nil {
		t.Fatalf("RenderizarTermoBaixaPDF: %v", err)
	}
	b, _ := RenderizarTermoBaixaPDF(c)
	if !bytes.Equal(a, b) {
		t.Error("PDF não determinístico")
	}
	c.Justificativas[0].Texto = strings.Repeat("x", 40000) // várias páginas só de texto
	if _, err := RenderizarTermoBaixaPDF(c); err != nil {
		t.Errorf("justificativa gigante: %v", err)
	}
}

func somaLarguras(cols []colunaProdutoPDF) float64 {
	var s float64
	for _, c := range cols {
		s += c.Largura
	}
	return s
}

// Guarda da Pré-Nota: a tabela dela NÃO ganha a coluna do Termo.
func TestColunasProdutos_PreNotaIntactaETermoSoma539(t *testing.T) {
	if len(colunasProdutosPreNota) != 6 || somaLarguras(colunasProdutosPreNota) != larguraUtilPreNotaPT {
		t.Errorf("Pré-Nota: %d colunas, soma %v, want 6 / %v", len(colunasProdutosPreNota), somaLarguras(colunasProdutosPreNota), larguraUtilPreNotaPT)
	}
	if len(colunasProdutosTermoBaixa) != 7 || somaLarguras(colunasProdutosTermoBaixa) != larguraUtilPreNotaPT {
		t.Errorf("Termo: %d colunas, soma %v", len(colunasProdutosTermoBaixa), somaLarguras(colunasProdutosTermoBaixa))
	}
	p, err := novoPintorPDF("r", "da Pré-Nota")
	if err != nil {
		t.Fatal(err)
	}
	if p.comSolicitada || len(p.colunas) != 6 {
		t.Errorf("pintor padrão deve usar as colunas da Pré-Nota")
	}
	celulas, _ := p.alturasLinhaProduto(produtoPreNota{Codigo: "1", Descricao: "d", Solicitada: "9"})
	if len(celulas) != 6 {
		t.Errorf("valores da Pré-Nota = %d, want 6", len(celulas))
	}
	p.usarColunasTermoBaixa()
	celulas, _ = p.alturasLinhaProduto(produtoPreNota{Codigo: "1", Descricao: "d", Solicitada: "9"})
	if len(celulas) != 7 || celulas[3][0] != "9" {
		t.Errorf("valores do Termo = %v, want 7 com solicitada na 4ª", celulas)
	}
}

func TestLinhasPedidoTermoBaixa_NaoTruncaTextoEMantemAcentosQuebrasETabs(t *testing.T) {
	p, err := novoPintorPDF("r", "do Termo de Baixa")
	if err != nil {
		t.Fatal(err)
	}
	palavra := strings.Repeat("Ç", 300) // sem espaço, acentuada, mais larga que a linha
	texto := "Açúcar não é ação.\n\tSegunda\tlinha com tab\r\n\n" + palavra + " fim"
	linhas, nCab := p.linhasPedidoTermoBaixa(pedidoTermoBaixa{Solicitante: "João", Obra: "Obra Á", Justificativa: texto})
	if nCab != 2 {
		t.Errorf("nCabecalho = %d, want 2", nCab)
	}
	junto := strings.Join(linhas[nCab:], "")
	semEspaco := func(s string) string { return strings.Join(strings.Fields(s), "") }
	want := semEspaco("Justificativa: " + strings.ReplaceAll(strings.ReplaceAll(textoLimpoPDF(texto), "\n", ""), "\t", ""))
	if semEspaco(junto) != want {
		t.Errorf("texto truncado ou alterado:\n got %q\nwant %q", semEspaco(junto), want)
	}
	if !strings.Contains(junto, "Ç") || !strings.Contains(junto, "Açúcar") {
		t.Error("acentos perdidos")
	}
	for _, l := range linhas {
		if strings.ContainsAny(l, "\n\t\r") {
			t.Errorf("linha com controle: %q", l)
		}
	}
	if textoLimpoPDF("a\x00b\xffc\x07") != "abc" {
		t.Errorf("textoLimpoPDF = %q", textoLimpoPDF("a\x00b\xffc\x07"))
	}
}

func TestRenderizarTermoBaixaPDF_JustificativaGigante_VariasPaginasComContinuacao(t *testing.T) {
	c := conteudoTermoTeste("perda_roubo")
	c.Justificativas[0].Texto = strings.Repeat("Furto no galpão com acentuação ", 800) // ~25 mil caracteres
	pdf, err := RenderizarTermoBaixaPDF(c)
	if err != nil {
		t.Fatal(err)
	}
	if n := paginasPDFTeste(pdf); n < 3 {
		t.Errorf("páginas = %d, want >= 3", n)
	}
}

// O cabeçalho do Pedido não fica órfão: perto do fim da página o bloco inteiro
// (título + justificativa + origem + 1ª linha) vai para a página seguinte.
func TestPedidoTermoBaixa_NaoDeixaCabecalhoOrfao(t *testing.T) {
	l := layoutDoTermoBaixa(conteudoTermoTeste("perda_roubo"))
	desenhar := func(y float64) int {
		p, err := novoPintorPDF("r", "do Termo de Baixa")
		if err != nil {
			t.Fatal(err)
		}
		p.usarColunasTermoBaixa()
		p.y = y
		p.pedidoTermoBaixa(0, 2, l.Pedidos[0])
		return p.pdf.GetNumberOfPages()
	}
	if n := desenhar(margemPreNotaPT); n != 1 {
		t.Errorf("no topo: páginas = %d, want 1", n)
	}
	if n := desenhar(limiteYPreNotaPT - 60); n != 2 {
		t.Errorf("perto do fim: páginas = %d, want 2 (bloco inteiro na página nova)", n)
	}
}
