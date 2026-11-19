package services

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// --- Peças de desenho compartilhadas pelos documentos DANFE internos --------
//
// Pré-Nota (pre_notas.go) e Termo de Baixa (termos_baixa.go) desenham com o
// MESMO pintor: cabeçalho em 3 caixas, quadros com rótulo, tabela de produtos
// com cabeçalho repetido, total, dados adicionais, assinaturas e rodapé
// "Página N". Extraído de RenderizarPreNotaPDF sem mudar um byte da Pré-Nota
// (mesmas medidas, mesma ordem de chamadas ao gopdf).

// larguraCNPJParteDocPT é a largura da coluna CNPJ nos quadros de parte.
const larguraCNPJParteDocPT = 110.0

// posicaoCampoParte: onde cada campo de rotulosPartePreNota vai no quadro —
// faixa (0..3) e coluna (0 = linha inteira/esquerda, 1 = direita). Razão
// social; CNPJ | IE; Endereço; Filial | Estoque.
type posicaoCampo struct{ faixa, col int }

var posicaoCampoParte = []posicaoCampo{{0, 0}, {1, 0}, {1, 1}, {2, 0}, {3, 0}, {3, 1}}

// larguraCampoParte devolve x relativo e largura do campo na faixa de um
// quadro de parte com largura wParte.
func larguraCampoParte(wParte float64, pos posicaoCampo) (float64, float64) {
	divisao := 0.0
	switch pos.faixa {
	case 1:
		divisao = larguraCNPJParteDocPT
	case 3:
		divisao = wParte / 2
	}
	if divisao == 0 {
		return 0, wParte
	}
	if pos.col == 0 {
		return 0, divisao
	}
	return divisao, wParte - divisao
}

// pintorPDF carrega o gopdf, o primeiro erro de desenho (as chamadas viram
// no-op depois dele) e o cursor vertical.
type pintorPDF struct {
	pdf  *gopdf.GoPdf
	erro error
	// rotulo entra nas mensagens de erro ("da Pré-Nota" / "do Termo de Baixa").
	rotulo string
	x, W   float64
	topo   float64
	y      float64
}

// novoPintorPDF abre um A4 retrato com as fontes e o rodapé (textoRodape +
// "Página N") de toda página.
func novoPintorPDF(textoRodape, rotulo string) (*pintorPDF, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.SetMargins(margemPreNotaPT, margemPreNotaPT, margemPreNotaPT, margemPreNotaPT)
	if err := pdf.AddTTFFontData("gofont", goregular.TTF); err != nil {
		return nil, fmt.Errorf("falha ao carregar fonte %s: %w", rotulo, err)
	}
	if err := pdf.AddTTFFontData("gofont-negrito", gobold.TTF); err != nil {
		return nil, fmt.Errorf("falha ao carregar fonte negrito %s: %w", rotulo, err)
	}
	p := &pintorPDF{pdf: pdf, rotulo: rotulo, x: margemPreNotaPT, W: larguraUtilPreNotaPT, topo: margemPreNotaPT, y: margemPreNotaPT}
	pdf.AddFooter(func() {
		yLinha := alturaPaginaPreNotaPT - margemPreNotaPT - 14
		p.traco(0.4, 120)
		pdf.Line(margemPreNotaPT, yLinha, margemPreNotaPT+larguraUtilPreNotaPT, yLinha)
		pdf.SetTextColor(70, 70, 70)
		p.fonte(false, 7)
		p.escrever(margemPreNotaPT, yLinha+3, larguraUtilPreNotaPT-60, 10, textoRodape, gopdf.Left|gopdf.Middle)
		p.escrever(margemPreNotaPT+larguraUtilPreNotaPT-60, yLinha+3, 60, 10,
			fmt.Sprintf("Página %d", pdf.GetNumberOfPages()), gopdf.Right|gopdf.Middle)
		pdf.SetTextColor(0, 0, 0)
	})
	p.novaPagina()
	return p, nil
}

func (p *pintorPDF) fonte(negrito bool, tamanho float64) {
	if p.erro != nil {
		return
	}
	familia := "gofont"
	if negrito {
		familia = "gofont-negrito"
	}
	if err := p.pdf.SetFont(familia, "", tamanho); err != nil {
		p.erro = fmt.Errorf("falha ao selecionar fonte %s: %w", p.rotulo, err)
	}
}

// escrever põe `texto` na caixa (x, y, w, h), alinhado por `align`.
func (p *pintorPDF) escrever(x, y, w, h float64, texto string, align int) {
	if p.erro != nil || texto == "" {
		return
	}
	p.pdf.SetXY(x, y)
	if err := p.pdf.CellWithOption(&gopdf.Rect{W: w, H: h}, texto, gopdf.CellOption{Align: align}); err != nil {
		p.erro = fmt.Errorf("falha ao escrever texto %s: %w", p.rotulo, err)
	}
}

// quebrar divide `texto` em linhas que cabem em `largura` com a fonte
// indicada — texto longo quebra, nunca é cortado.
func (p *pintorPDF) quebrar(texto string, largura float64, negrito bool, tamanho float64) []string {
	p.fonte(negrito, tamanho)
	if p.erro != nil || texto == "" {
		return []string{""}
	}
	partes, err := p.pdf.SplitTextWithWordWrap(texto, largura)
	if err != nil {
		p.erro = fmt.Errorf("falha ao quebrar texto %s: %w", p.rotulo, err)
		return []string{""}
	}
	if len(partes) == 0 {
		return []string{""}
	}
	return partes
}

func (p *pintorPDF) traco(largura float64, cinza uint8) {
	p.pdf.SetLineWidth(largura)
	p.pdf.SetStrokeColor(cinza, cinza, cinza)
}

func (p *pintorPDF) quadro(x, y, w, h float64) {
	p.traco(0.6, 0)
	p.pdf.RectFromUpperLeftWithStyle(x, y, w, h, "D")
}

// alturaCampo: rótulo pequeno + `linhas` linhas de valor.
func (p *pintorPDF) alturaCampo(linhas int) float64 {
	return paddingQuadroPT + tamanhoRotuloPreNotaPT + 2 + float64(linhas)*entrelinhaPreNotaPT + paddingQuadroPT
}

func (p *pintorPDF) linhasCampo(valor string, w float64, negrito bool) []string {
	return p.quebrar(valor, w-2*paddingQuadroPT, negrito, tamanhoValorPreNotaPT)
}

// desenharCampo: rótulo pequeno no canto superior esquerdo + valor.
func (p *pintorPDF) desenharCampo(x, y, w float64, rotulo string, linhas []string, negrito bool) {
	p.fonte(false, tamanhoRotuloPreNotaPT)
	p.pdf.SetTextColor(70, 70, 70)
	p.escrever(x+paddingQuadroPT, y+paddingQuadroPT, w-2*paddingQuadroPT, tamanhoRotuloPreNotaPT, rotulo, gopdf.Left|gopdf.Top)
	p.pdf.SetTextColor(0, 0, 0)
	p.fonte(negrito, tamanhoValorPreNotaPT)
	ly := y + paddingQuadroPT + tamanhoRotuloPreNotaPT + 2
	for _, l := range linhas {
		p.escrever(x+paddingQuadroPT, ly, w-2*paddingQuadroPT, entrelinhaPreNotaPT, l, gopdf.Left|gopdf.Middle)
		ly += entrelinhaPreNotaPT
	}
}

func (p *pintorPDF) tituloSecao(x, y, w float64, titulo string) {
	p.fonte(true, 7)
	p.escrever(x, y, w, alturaTituloSecaoPT, titulo, gopdf.Left|gopdf.Middle)
}

func (p *pintorPDF) novaPagina() {
	p.pdf.AddPage()
	p.pdf.SetTextColor(0, 0, 0)
	p.y = p.topo
}

// garantir: há espaço para `h` pontos a partir de y? No topo da página sempre
// "cabe" (um bloco maior que a página é desenhado assim mesmo).
func (p *pintorPDF) garantir(h float64) {
	if p.y+h > limiteYPreNotaPT && p.y > p.topo {
		p.novaPagina()
	}
}

// cabecalho desenha a caixa de 3 partes: Empresa (+ logo) | título + aviso |
// número + emissão.
func (p *pintorPDF) cabecalho(titulo, empresa, cnpj, numero, emissao string, logo *logoPreNota) {
	x, y, W := p.x, p.y, p.W
	pdf := p.pdf
	wE, wC := 230.0, 170.0
	wN := W - wE - wC
	linhasEmpresa := p.linhasCampo(empresa, wE, true)
	linhasEmpresa = append(linhasEmpresa, p.linhasCampo("CNPJ: "+cnpj, wE, false)...)
	linhasAviso := p.quebrar(RodapePreNota, wC-2*paddingQuadroPT, false, 7)
	// Com logo, o texto da Empresa desce abaixo dela.
	posEmpresa := posicoesEmpresaDaLogo(logo)
	h := p.alturaCampo(len(linhasEmpresa)) + (posEmpresa.Rotulo - paddingQuadroPT)
	if hC := 6 + 18 + 4 + float64(len(linhasAviso))*9 + 6; hC > h {
		h = hC
	}
	if h < 56 {
		h = 56
	}
	p.quadro(x, y, W, h)
	p.traco(0.6, 0)
	pdf.Line(x+wE, y, x+wE, y+h)
	pdf.Line(x+wE+wC, y, x+wE+wC, y+h)

	// Empresa: razão social em negrito, CNPJ embaixo.
	p.fonte(false, tamanhoRotuloPreNotaPT)
	pdf.SetTextColor(70, 70, 70)
	if logo != nil && p.erro == nil {
		if err := desenharLogoPreNota(pdf, logo, x+paddingQuadroPT, y+posEmpresa.LogoTopo); err != nil {
			slog.Warn("falha ao desenhar a logo da empresa no documento", "error", err)
		}
	}
	p.escrever(x+paddingQuadroPT, y+posEmpresa.Rotulo, wE, tamanhoRotuloPreNotaPT, "EMPRESA", gopdf.Left|gopdf.Top)
	pdf.SetTextColor(0, 0, 0)
	ly := y + posEmpresa.Texto
	for i, l := range linhasEmpresa {
		p.fonte(i < len(linhasEmpresa)-1, tamanhoValorPreNotaPT)
		p.escrever(x+paddingQuadroPT, ly, wE-2*paddingQuadroPT, entrelinhaPreNotaPT, l, gopdf.Left|gopdf.Middle)
		ly += entrelinhaPreNotaPT
	}

	// Centro: título em destaque + aviso.
	p.fonte(true, 16)
	p.escrever(x+wE, y+6, wC, 18, titulo, gopdf.Center|gopdf.Middle)
	p.fonte(false, 7)
	ly = y + 6 + 18 + 4
	for _, l := range linhasAviso {
		p.escrever(x+wE+paddingQuadroPT, ly, wC-2*paddingQuadroPT, 9, l, gopdf.Center|gopdf.Middle)
		ly += 9
	}

	// Direita: número e emissão.
	xN := x + wE + wC
	p.fonte(false, tamanhoRotuloPreNotaPT)
	pdf.SetTextColor(70, 70, 70)
	p.escrever(xN+paddingQuadroPT, y+paddingQuadroPT, wN, tamanhoRotuloPreNotaPT, "NÚMERO", gopdf.Left|gopdf.Top)
	pdf.SetTextColor(0, 0, 0)
	p.fonte(true, 13)
	p.escrever(xN, y+12, wN, 18, numero, gopdf.Center|gopdf.Middle)
	p.fonte(false, tamanhoValorPreNotaPT)
	p.escrever(xN, y+34, wN, 12, emissao, gopdf.Center|gopdf.Middle)

	p.y += h + espacoEntreRotasPT
}

// medirParte quebra as linhas de cada campo de um quadro de parte (6 campos
// de rotulosPartePreNota) e acumula em hFaixa a altura de cada faixa (o lado
// mais alto manda: dois quadros lado a lado ficam alinhados).
func (p *pintorPDF) medirParte(campos []campoDesenhado, wParte float64, hFaixa *[4]float64) [][]string {
	linhasQuadro := make([][]string, len(campos))
	for ci, c := range campos {
		pos := posicaoCampoParte[ci]
		_, w := larguraCampoParte(wParte, pos)
		var linhas []string
		for _, l := range c.Linhas {
			linhas = append(linhas, p.linhasCampo(l, w, c.Rotulo == "ESTOQUE")...)
		}
		linhasQuadro[ci] = linhas
		hFaixa[pos.faixa] = max(hFaixa[pos.faixa], p.alturaCampo(len(linhas)))
	}
	return linhasQuadro
}

// desenharParte desenha o quadro de parte (já medido) em (px, by).
func (p *pintorPDF) desenharParte(px, by, wParte float64, campos []campoDesenhado, linhas [][]string, hFaixa [4]float64) {
	hPartes := hFaixa[0] + hFaixa[1] + hFaixa[2] + hFaixa[3]
	p.quadro(px, by, wParte, hPartes)
	yFaixa := [4]float64{by, by + hFaixa[0], by + hFaixa[0] + hFaixa[1], by + hFaixa[0] + hFaixa[1] + hFaixa[2]}
	p.traco(0.6, 0)
	for f := 1; f < 4; f++ {
		p.pdf.Line(px, yFaixa[f], px+wParte, yFaixa[f])
	}
	p.pdf.Line(px+larguraCNPJParteDocPT, yFaixa[1], px+larguraCNPJParteDocPT, yFaixa[2])
	p.pdf.Line(px+wParte/2, yFaixa[3], px+wParte/2, by+hPartes)
	for ci, c := range campos {
		pos := posicaoCampoParte[ci]
		dx, w := larguraCampoParte(wParte, pos)
		p.desenharCampo(px+dx, yFaixa[pos.faixa], w, c.Rotulo, linhas[ci], c.Rotulo == "ESTOQUE")
	}
}

// alturasLinhaProduto quebra as células de um item e devolve a altura da linha.
func (p *pintorPDF) alturasLinhaProduto(prod produtoPreNota) ([][]string, float64) {
	valores := []string{prod.Codigo, prod.Descricao, prod.Unidade, prod.Quantidade, prod.ValorUnitario, prod.ValorTotal}
	celulas := make([][]string, len(valores))
	maior := 1
	for i, v := range valores {
		celulas[i] = p.quebrar(v, colunasProdutosPreNota[i].Largura-2*paddingQuadroPT, false, tamanhoItemPreNotaPT)
		if len(celulas[i]) > maior {
			maior = len(celulas[i])
		}
	}
	return celulas, float64(maior)*entrelinhaItemPreNotaPT + 5
}

const alturaSubtotalPT = 15.0

// tabelaProdutos desenha o quadro DADOS DOS PRODUTOS: cabeçalho em caixa, uma
// linha entre itens, cabeçalho repetido (com `continuacao` no título) ao
// quebrar página, e o subtotal colado no fim (reservado junto do último item).
func (p *pintorPDF) tabelaProdutos(produtos []produtoPreNota, continuacao, rotuloSubtotal, subtotal string) {
	x, W, pdf := p.x, p.W, p.pdf
	var inicioSegmento float64
	abrirSegmento := func(titulo string) {
		p.tituloSecao(x, p.y, W, titulo)
		p.y += alturaTituloSecaoPT
		inicioSegmento = p.y
		pdf.SetFillColor(232, 232, 232)
		p.traco(0.6, 0)
		pdf.RectFromUpperLeftWithStyle(x, p.y, W, alturaCabecalhoTabelaPT, "FD")
		p.fonte(true, 6.5)
		cx := x
		for _, col := range colunasProdutosPreNota {
			p.escrever(cx+paddingQuadroPT, p.y, col.Largura-2*paddingQuadroPT, alturaCabecalhoTabelaPT, col.Titulo, col.Align|gopdf.Middle)
			cx += col.Largura
		}
		p.y += alturaCabecalhoTabelaPT
	}
	fecharSegmento := func() {
		p.quadro(x, inicioSegmento, W, p.y-inicioSegmento)
		p.traco(0.6, 0)
		cx := x
		for _, col := range colunasProdutosPreNota[:len(colunasProdutosPreNota)-1] {
			cx += col.Largura
			pdf.Line(cx, inicioSegmento, cx, p.y)
		}
	}

	abrirSegmento("DADOS DOS PRODUTOS")
	for pi, prod := range produtos {
		celulas, h := p.alturasLinhaProduto(prod)
		// O ÚLTIMO item reserva também o subtotal: os dois vão juntos.
		reserva := h
		if pi == len(produtos)-1 {
			reserva += alturaSubtotalPT
		}
		if p.y+reserva > limiteYPreNotaPT && pi > 0 {
			fecharSegmento()
			p.novaPagina()
			abrirSegmento(continuacao)
		}
		p.fonte(false, tamanhoItemPreNotaPT)
		cx := x
		for i, col := range colunasProdutosPreNota {
			ly := p.y + 2.5
			for _, l := range celulas[i] {
				p.escrever(cx+paddingQuadroPT, ly, col.Largura-2*paddingQuadroPT, entrelinhaItemPreNotaPT, l, col.Align|gopdf.Middle)
				ly += entrelinhaItemPreNotaPT
			}
			cx += col.Largura
		}
		p.y += h
		p.traco(0.3, 150)
		pdf.Line(x, p.y, x+W, p.y)
	}
	// Subtotal, colado no fim da tabela. Com item sem preço, o aviso vai no rótulo.
	fecharSegmento()
	wTotal := colunasProdutosPreNota[len(colunasProdutosPreNota)-1].Largura
	valorSub := subtotal
	if strings.HasSuffix(valorSub, avisoTotalSemValor) {
		valorSub = strings.TrimSuffix(valorSub, avisoTotalSemValor)
		rotuloSubtotal += avisoTotalSemValor
	}
	p.quadro(x, p.y, W, alturaSubtotalPT)
	p.traco(0.6, 0)
	pdf.Line(x+W-wTotal, p.y, x+W-wTotal, p.y+alturaSubtotalPT)
	p.fonte(true, 7)
	p.escrever(x, p.y, W-wTotal-paddingQuadroPT, alturaSubtotalPT, rotuloSubtotal, gopdf.Right|gopdf.Middle)
	p.fonte(true, tamanhoItemPreNotaPT)
	p.escrever(x+W-wTotal+paddingQuadroPT, p.y, wTotal-2*paddingQuadroPT, alturaSubtotalPT, valorSub, gopdf.Right|gopdf.Middle)
	p.y += alturaSubtotalPT
}

// calculoTotal desenha o quadro CÁLCULO DO TOTAL.
func (p *pintorPDF) calculoTotal(valorTotal string) {
	p.y += espacoEntreRotasPT
	h := p.alturaCampo(1)
	p.garantir(alturaTituloSecaoPT + h)
	p.tituloSecao(p.x, p.y, p.W, "CÁLCULO DO TOTAL")
	p.y += alturaTituloSecaoPT
	p.quadro(p.x, p.y, p.W, h)
	p.desenharCampo(p.x, p.y, p.W, "VALOR TOTAL DOS PRODUTOS", nil, false)
	p.fonte(true, 10)
	p.escrever(p.x+paddingQuadroPT, p.y+paddingQuadroPT+tamanhoRotuloPreNotaPT+2, p.W-2*paddingQuadroPT, entrelinhaPreNotaPT,
		valorTotal, gopdf.Right|gopdf.Middle)
	p.y += h + espacoEntreRotasPT
}

const (
	larguraAprovadorDocPT = 180.0
	alturaDeclaracaoDocPT = 16.0
	alturaAssinarDocPT    = 34.0
)

// medirDadosAdicionais quebra Aprovador e Solicitante(s) e devolve a altura
// do quadro DADOS ADICIONAIS.
func (p *pintorPDF) medirDadosAdicionais(aprovador, solicitantes string) (linhasAprov, linhasSolic []string, h float64) {
	linhasAprov = p.linhasCampo(aprovador, larguraAprovadorDocPT, false)
	linhasSolic = p.linhasCampo(solicitantes, p.W-larguraAprovadorDocPT, false)
	return linhasAprov, linhasSolic, p.alturaCampo(max(len(linhasAprov), len(linhasSolic)))
}

// desenharDadosAdicionais desenha o quadro (já medido) em y.
func (p *pintorPDF) desenharDadosAdicionais(linhasAprov, linhasSolic []string, h float64) {
	x, y, W := p.x, p.y, p.W
	p.tituloSecao(x, y, W, "DADOS ADICIONAIS")
	p.y += alturaTituloSecaoPT
	y = p.y
	p.quadro(x, y, W, h)
	p.traco(0.6, 0)
	p.pdf.Line(x+larguraAprovadorDocPT, y, x+larguraAprovadorDocPT, y+h)
	p.desenharCampo(x, y, larguraAprovadorDocPT, "APROVADOR", linhasAprov, false)
	p.desenharCampo(x+larguraAprovadorDocPT, y, W-larguraAprovadorDocPT, "SOLICITANTE(S)", linhasSolic, false)
	p.y += h
}

// blocoAssinatura é uma linha de assinatura: um texto (declaração ou papel)
// sobre a linha de campos em branco (Data | Nome legível | Assinatura).
type blocoAssinatura struct {
	Texto  string
	Campos []campoDesenhado
}

// alturaAssinaturas: espaço, título e os blocos.
func alturaAssinaturas(n int) float64 {
	if n == 0 {
		return 0
	}
	return espacoEntreRotasPT + alturaTituloSecaoPT + float64(n)*(alturaDeclaracaoDocPT+alturaAssinarDocPT)
}

// desenharAssinaturas desenha o quadro `titulo` com os blocos (um quadro só,
// linhas horizontais entre eles).
func (p *pintorPDF) desenharAssinaturas(titulo string, blocos []blocoAssinatura) {
	if len(blocos) == 0 {
		return
	}
	x, W := p.x, p.W
	p.y += espacoEntreRotasPT
	p.tituloSecao(x, p.y, W, titulo)
	p.y += alturaTituloSecaoPT
	hBloco := alturaDeclaracaoDocPT + alturaAssinarDocPT
	p.quadro(x, p.y, W, float64(len(blocos))*hBloco)
	larguras := []float64{110, 190, W - 110 - 190}
	for bi, b := range blocos {
		if bi > 0 {
			p.traco(0.6, 0)
			p.pdf.Line(x, p.y, x+W, p.y)
		}
		p.fonte(false, tamanhoValorPreNotaPT)
		p.escrever(x+paddingQuadroPT, p.y, W-2*paddingQuadroPT, alturaDeclaracaoDocPT, b.Texto, gopdf.Left|gopdf.Middle)
		yc := p.y + alturaDeclaracaoDocPT
		p.traco(0.6, 0)
		p.pdf.Line(x, yc, x+W, yc)
		cx := x
		for i, c := range b.Campos {
			if i > 0 {
				p.traco(0.6, 0)
				p.pdf.Line(cx, yc, cx, yc+alturaAssinarDocPT)
			}
			p.desenharCampo(cx, yc, larguras[i], c.Rotulo, c.Linhas, false)
			cx += larguras[i]
		}
		p.y = yc + alturaAssinarDocPT
	}
}

// bytes fecha o documento.
func (p *pintorPDF) bytes(nome string) ([]byte, error) {
	if p.erro != nil {
		return nil, p.erro
	}
	b, err := p.pdf.GetBytesPdfReturnErr()
	if err != nil {
		return nil, fmt.Errorf("falha ao gerar bytes do PDF %s: %w", nome, err)
	}
	return b, nil
}
