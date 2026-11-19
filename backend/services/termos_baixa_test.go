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
			{Solicitante: "Ana", Texto: "Furto no galpão 3, BO 123", Obra: "Obra Norte"},
			{Solicitante: "Bruno", Texto: "Material molhado pela chuva"},
		},
		Itens: []PreNotaItem{
			{Tipo: "baixa", ProdutoCodigo: "000123", Descricao: "Cimento CP-II 50kg", QuantidadeMilesimos: 6000,
				UnidadeMedida: ptrStr("un"), PrecoCentavos: ptrI64(123450), FilialOrigem: "Recife",
				EstoqueOrigem: "Almoxarifado Central", EstoqueOrigemID: "e1", FiscalOrigem: fiscalRecifeTeste()},
			{Tipo: "baixa", ProdutoCodigo: "000456", Descricao: "Areia média", QuantidadeMilesimos: 2500,
				FilialOrigem: "Recife", EstoqueOrigem: "Almoxarifado Central", EstoqueOrigemID: "e1", FiscalOrigem: fiscalRecifeTeste()},
			{Tipo: "baixa", ProdutoCodigo: "000789", Descricao: "Brita", QuantidadeMilesimos: 1000,
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
	if len(l.Origens) != 2 {
		t.Fatalf("Origens = %d, want 2 (um por Estoque de origem)", len(l.Origens))
	}
	o := l.Origens[0]
	if o.Origem.Estoque != "Almoxarifado Central" || len(o.Produtos) != 2 {
		t.Errorf("origem 0 = %+v", o)
	}
	// 6 × 1.234,50 = 7.407,00; a Areia não tem preço -> aviso no subtotal e no total.
	if o.Subtotal != "R$ 7.407,00"+avisoTotalSemValor {
		t.Errorf("Subtotal = %q", o.Subtotal)
	}
	if o.Produtos[1].ValorTotal != textoAusentePreNota || o.Produtos[1].Unidade != textoAusentePreNota {
		t.Errorf("item sem preço/unidade = %+v", o.Produtos[1])
	}
	if l.Origens[1].Subtotal != "R$ 50,00" {
		t.Errorf("Subtotal 1 = %q", l.Origens[1].Subtotal)
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

func TestLayoutTermoBaixa_JustificativasEObra(t *testing.T) {
	l := layoutDoTermoBaixa(conteudoTermoTeste("utilizacao_obra"))
	want := []string{
		"Ana: Furto no galpão 3, BO 123 (Obra / centro de custo: Obra Norte)",
		"Bruno: Material molhado pela chuva",
	}
	if len(l.Justificativas) != 2 || l.Justificativas[0] != want[0] || l.Justificativas[1] != want[1] {
		t.Errorf("Justificativas = %q, want %q", l.Justificativas, want)
	}
	if l.Motivo != "SAÍDA POR UTILIZAÇÃO NA OBRA DA EMPRESA" {
		t.Errorf("Motivo = %q", l.Motivo)
	}
}

func TestLayoutTermoBaixa_JustificativaLegadaVazia(t *testing.T) {
	c := conteudoTermoTeste("")
	c.Justificativas = []JustificativaTermoBaixa{{Solicitante: "Ana"}, {Solicitante: "Bruno", Texto: "   "}}
	l := layoutDoTermoBaixa(c)
	want := "Sem justificativa informada (pedido anterior à exigência)"
	if len(l.Justificativas) != 2 || l.Justificativas[0] != "Ana: "+want || l.Justificativas[1] != "Bruno: "+want {
		t.Errorf("Justificativas = %q", l.Justificativas)
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
	if l.Motivo != "BAIXA DE ESTOQUE — MOTIVO NÃO INFORMADO" || len(l.Origens) != 0 || l.ValorTotal != "" {
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
		c.Itens = append(c.Itens, PreNotaItem{Tipo: "baixa", Descricao: strings.Repeat("Descrição longa ", 8), QuantidadeMilesimos: 1500,
			FilialOrigem: "Recife", EstoqueOrigem: "Almoxarifado Central", EstoqueOrigemID: "e1"})
	}
	for i := 0; i < 40; i++ {
		c.Justificativas = append(c.Justificativas, JustificativaTermoBaixa{Solicitante: "Fulano", Texto: strings.Repeat("texto da justificativa ", 12)})
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
