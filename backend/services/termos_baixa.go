package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"
	"github.com/signintech/gopdf"
)

// --- Termo de Baixa — spec-pedido-outras-saidas ------------------------------
//
// Documento interno gerado na aprovação em lote de Pedidos de Outras Saídas
// (um por motivo, numeração própria). Mesmo desenho da Pré-Nota: busca
// (MontarTermoBaixaConteudo) / layout puro (layoutDoTermoBaixa) / desenho
// (RenderizarTermoBaixaPDF, sobre o pintorPDF compartilhado) / GerarTermoBaixaPDF.

// ErrTermoBaixaNaoEncontrado colapsa id malformado, inexistente ou de OUTRA
// Empresa — 404 NOT_FOUND na fronteira HTTP.
var ErrTermoBaixaNaoEncontrado = errors.New("termo de baixa não encontrado")

// JustificativaTermoBaixa é a justificativa de UM Pedido do Termo.
type JustificativaTermoBaixa struct {
	PedidoID    string
	Solicitante string
	Texto       string // `pedidos.observacao`; "" quando ausente (pedido legado)
	Obra        string // `pedidos.obra_centro_custo`; "" quando não informada
}

// TermoBaixaConteudo é o conteúdo já resolvido do documento (sem gopdf).
type TermoBaixaConteudo struct {
	TermoID            string
	Numero             int
	Motivo             string // "perda_roubo" | "vencido_avariado" | "utilizacao_obra" | "" (não informado)
	EmpresaRazaoSocial string
	EmpresaCNPJ        string
	CriadoEm           time.Time
	Aprovador          string
	Solicitantes       []string
	Justificativas     []JustificativaTermoBaixa
	Itens              []PreNotaItem
	LogoEmpresa        []byte
}

// MontarTermoBaixaConteudo busca o Termo `termoID` da Empresa `empresaID`
// (filtro por empresa em toda consulta; outra Empresa/inexistente/malformado
// -> ErrTermoBaixaNaoEncontrado), numa transação somente-leitura REPEATABLE READ.
func MontarTermoBaixaConteudo(db *sql.DB, empresaID, termoID string) (TermoBaixaConteudo, error) {
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return TermoBaixaConteudo{}, fmt.Errorf("falha ao iniciar leitura do termo de baixa: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var c TermoBaixaConteudo
	var aprovador, motivo sql.NullString
	const selectCabecalho = `
		SELECT tb.id, tb.numero, tb.motivo_saida, tb.criado_em, e.razao_social, e.cnpj, u.nome
		FROM termos_baixa tb
		JOIN empresas e ON e.id = tb.empresa_id
		LEFT JOIN usuarios u ON u.id = tb.decidido_por
		WHERE tb.id = $1 AND tb.empresa_id = $2`
	if err := tx.QueryRow(selectCabecalho, termoID, empresaID).Scan(
		&c.TermoID, &c.Numero, &motivo, &c.CriadoEm, &c.EmpresaRazaoSocial, &c.EmpresaCNPJ, &aprovador,
	); err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return TermoBaixaConteudo{}, ErrTermoBaixaNaoEncontrado
		}
		return TermoBaixaConteudo{}, fmt.Errorf("falha ao buscar termo de baixa: %w", err)
	}
	c.EmpresaCNPJ = strings.TrimSpace(c.EmpresaCNPJ)
	c.Motivo = motivo.String
	c.Aprovador = aprovador.String

	if err := tx.QueryRow(`SELECT conteudo FROM empresa_logos WHERE empresa_id = $1`, empresaID).Scan(&c.LogoEmpresa); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TermoBaixaConteudo{}, fmt.Errorf("falha ao buscar logo da empresa: %w", err)
	}

	// Pedidos do Termo com ao menos 1 item listado (quantidade_aprovada > 0),
	// na ordem de criação — mesma regra de solicitantes da Pré-Nota.
	const selectPedidos = `
		SELECT p.id::text, p.solicitante, p.observacao, p.obra_centro_custo FROM pedidos p
		WHERE p.termo_baixa_id = $1 AND p.empresa_id = $2
		  AND EXISTS (
		    SELECT 1 FROM pedido_itens pi
		    WHERE pi.pedido_id = p.id AND pi.empresa_id = p.empresa_id AND pi.quantidade_aprovada > 0)
		ORDER BY p.criado_em, p.id`
	rows, err := tx.Query(selectPedidos, termoID, empresaID)
	if err != nil {
		return TermoBaixaConteudo{}, fmt.Errorf("falha ao listar pedidos do termo de baixa: %w", err)
	}
	vistos := map[string]bool{}
	for rows.Next() {
		var j JustificativaTermoBaixa
		var obs, obra sql.NullString
		if err := rows.Scan(&j.PedidoID, &j.Solicitante, &obs, &obra); err != nil {
			rows.Close()
			return TermoBaixaConteudo{}, fmt.Errorf("falha ao ler pedido do termo de baixa: %w", err)
		}
		j.Texto, j.Obra = obs.String, obra.String
		c.Justificativas = append(c.Justificativas, j)
		vistos[j.Solicitante] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return TermoBaixaConteudo{}, fmt.Errorf("falha ao iterar pedidos do termo de baixa: %w", err)
	}
	rows.Close()
	c.Solicitantes = make([]string, 0, len(vistos))
	for s := range vistos {
		c.Solicitantes = append(c.Solicitantes, s)
	}
	sort.Strings(c.Solicitantes)

	itens, err := lerItensDocumentoTx(tx, vinculoTermoBaixa, termoID, empresaID)
	if err != nil {
		return TermoBaixaConteudo{}, err
	}
	c.Itens = itens
	return c, nil
}

// --- Layout puro ------------------------------------------------------------

const (
	textoSemItensTermoBaixa = "Nenhum item movimentado neste Termo de Baixa"
	// textoSemJustificativaTermoBaixa: pedido legado (anterior à exigência de
	// justificativa) com observação vazia/NULL.
	textoSemJustificativaTermoBaixa = "Sem justificativa informada (pedido anterior à exigência)"
	quadroOrigemTermoBaixa          = "ORIGEM DA SAÍDA"
	quadroAssinaturasTB             = "ASSINATURAS"
	alturaFaixaMotivoPT             = 24.0
)

// FaixaMotivoTermoBaixa é o texto (caixa alta) da faixa de destaque do motivo.
func FaixaMotivoTermoBaixa(motivo string) string {
	switch motivo {
	case MotivoSaidaPerdaRoubo:
		return "BAIXA DE ESTOQUE POR PERDA OU ROUBO"
	case MotivoSaidaVencidoAvariado:
		return "PRODUTO VENCIDO OU AVARIADO"
	case MotivoSaidaUtilizacaoObra:
		return "SAÍDA POR UTILIZAÇÃO NA OBRA DA EMPRESA"
	default:
		return "BAIXA DE ESTOQUE — MOTIVO NÃO INFORMADO"
	}
}

// origemTermoBaixa agrupa os itens de um mesmo Estoque de origem.
type origemTermoBaixa struct {
	Origem   partePreNota
	Produtos []produtoPreNota
	Subtotal string
}

// pedidoTermoBaixa é um Pedido do Termo: quem pediu, a justificativa (uma
// vez só) e os itens dele, agrupados por Estoque de origem.
type pedidoTermoBaixa struct {
	Solicitante   string
	Obra          string // "" quando não informada
	Justificativa string
	Origens       []origemTermoBaixa
}

type layoutTermoBaixa struct {
	Empresa      string
	CNPJ         string
	Numero       string
	Emissao      string
	Motivo       string // faixa de destaque
	Rodape       string // "TERMO DE BAIXA Nº 000.001 — <MOTIVO>", em toda página
	Pedidos      []pedidoTermoBaixa
	ValorTotal   string
	Aprovador    string
	Solicitantes string
	Assinaturas  []blocoAssinatura
	Logo         *logoPreNota
}

func camposAssinaturaTermoBaixa(papel string) blocoAssinatura {
	return blocoAssinatura{
		Texto: papel,
		Campos: []campoDesenhado{
			{Quadro: quadroAssinaturasTB, Rotulo: "DATA"},
			{Quadro: quadroAssinaturasTB, Rotulo: "NOME LEGÍVEL"},
			{Quadro: quadroAssinaturasTB, Rotulo: "ASSINATURA"},
		},
	}
}

func layoutDoTermoBaixa(c TermoBaixaConteudo) layoutTermoBaixa {
	empresa := textoOuAusentePreNota(c.EmpresaRazaoSocial)
	numero := formatarDocnumPreNota(c.Numero)
	motivo := FaixaMotivoTermoBaixa(c.Motivo)
	solicitantes := textoAusentePreNota
	if len(c.Solicitantes) > 0 {
		solicitantes = strings.Join(c.Solicitantes, ", ")
	}
	l := layoutTermoBaixa{
		Empresa:      empresa,
		CNPJ:         textoOuAusentePreNota(formatarCNPJPreNota(c.EmpresaCNPJ)),
		Numero:       numero,
		Emissao:      "Emissão: " + formatarDataPreNota(c.CriadoEm),
		Motivo:       motivo,
		Rodape:       "TERMO DE BAIXA " + numero + " — " + motivo,
		Aprovador:    textoOuAusentePreNota(c.Aprovador),
		Solicitantes: solicitantes,
		Assinaturas: []blocoAssinatura{
			camposAssinaturaTermoBaixa("SOLICITANTE"), camposAssinaturaTermoBaixa("APROVADOR"),
		},
		Logo: logoDoCabecalhoPreNota(c.LogoEmpresa),
	}
	chaveEstoque := func(id, nome string) string {
		if id != "" {
			return "id:" + id
		}
		return "nome:" + nome
	}
	// Um bloco por Pedido, na ordem do conteúdo; item de Pedido desconhecido
	// (dado inconsistente) ganha um bloco próprio ao final, sem perder o item.
	pedidoIdx := map[string]int{}
	novoPedido := func(id, solicitante, justificativa, obra string) int {
		justificativa = strings.TrimSpace(justificativa)
		if justificativa == "" {
			justificativa = textoSemJustificativaTermoBaixa
		}
		l.Pedidos = append(l.Pedidos, pedidoTermoBaixa{
			Solicitante:   textoOuAusentePreNota(solicitante),
			Obra:          strings.TrimSpace(obra),
			Justificativa: justificativa,
		})
		pedidoIdx[id] = len(l.Pedidos) - 1
		return len(l.Pedidos) - 1
	}
	for _, j := range c.Justificativas {
		novoPedido(j.PedidoID, j.Solicitante, j.Texto, j.Obra)
	}
	type chave struct {
		pedido          int
		filial, estoque string
	}
	indice := map[chave]int{}
	subtotais := map[[2]int]*big.Int{}
	semValor := map[[2]int]bool{}
	total := new(big.Int)
	algumSemValor := false
	for _, it := range c.Itens {
		pi, ok := pedidoIdx[it.PedidoID]
		if !ok {
			pi = novoPedido(it.PedidoID, "", "", "")
		}
		k := chave{pi, it.FilialOrigem, chaveEstoque(it.EstoqueOrigemID, it.EstoqueOrigem)}
		i, ok := indice[k]
		if !ok {
			i = len(l.Pedidos[pi].Origens)
			indice[k] = i
			l.Pedidos[pi].Origens = append(l.Pedidos[pi].Origens, origemTermoBaixa{
				Origem: parteDaFilialPreNota(empresa, it.FilialOrigem, it.EstoqueOrigem, it.FiscalOrigem),
			})
			subtotais[[2]int{pi, i}] = new(big.Int)
		}
		sk := [2]int{pi, i}
		valorLinha := textoAusentePreNota
		if it.PrecoCentavos != nil {
			v := valorLinhaCentavosPreNota(it.QuantidadeMilesimos, *it.PrecoCentavos)
			subtotais[sk].Add(subtotais[sk], v)
			total.Add(total, v)
			valorLinha = formatarCentavosPreNota(v)
		} else {
			algumSemValor = true
			semValor[sk] = true
		}
		unidade := textoAusentePreNota
		if it.UnidadeMedida != nil {
			unidade = textoOuAusentePreNota(*it.UnidadeMedida)
		}
		l.Pedidos[pi].Origens[i].Produtos = append(l.Pedidos[pi].Origens[i].Produtos, produtoPreNota{
			Codigo:        textoOuAusentePreNota(it.ProdutoCodigo),
			Descricao:     textoOuAusentePreNota(it.Descricao),
			Unidade:       unidade,
			Solicitada:    formatarQuantidadePreNota(it.QuantidadeSolicitadaMilesimos),
			Quantidade:    formatarQuantidadePreNota(it.QuantidadeMilesimos),
			ValorUnitario: formatarPrecoPreNota(it.PrecoCentavos),
			ValorTotal:    valorLinha,
		})
	}
	temItens := false
	for pi := range l.Pedidos {
		for i := range l.Pedidos[pi].Origens {
			temItens = true
			o := &l.Pedidos[pi].Origens[i]
			o.Subtotal = formatarCentavosPreNota(subtotais[[2]int{pi, i}])
			if semValor[[2]int{pi, i}] {
				o.Subtotal += avisoTotalSemValor
			}
		}
	}
	if temItens {
		l.ValorTotal = formatarCentavosPreNota(total)
		if algumSemValor {
			l.ValorTotal += avisoTotalSemValor
		}
	}
	return l
}

// tituloContinuacaoTermoBaixa lembra, na página seguinte, de qual Estoque é a tabela.
func tituloContinuacaoTermoBaixa(o origemTermoBaixa) string {
	return "DADOS DOS PRODUTOS (continuação: " + o.Origem.Estoque + " (" + o.Origem.Filial + "))"
}

// --- Renderização -----------------------------------------------------------

// RenderizarTermoBaixaPDF desenha o Termo de Baixa (A4, estilo DANFE da
// Pré-Nota). Pura e determinística: mesma entrada, mesmos bytes.
func RenderizarTermoBaixaPDF(conteudo TermoBaixaConteudo) ([]byte, error) {
	doc := layoutDoTermoBaixa(conteudo)
	p, err := novoPintorPDF(doc.Rodape, "do Termo de Baixa")
	if err != nil {
		return nil, err
	}
	x, W := p.x, p.W

	p.cabecalho("TERMO DE BAIXA", doc.Empresa, doc.CNPJ, doc.Numero, doc.Emissao, doc.Logo)

	// Faixa de destaque do MOTIVO: largura total, fundo escuro, texto branco.
	{
		linhas := p.quebrar(doc.Motivo, W-4*paddingQuadroPT, true, 13)
		h := max(alturaFaixaMotivoPT, float64(len(linhas))*16+8)
		p.garantir(h + espacoEntreRotasPT)
		p.pdf.SetFillColor(30, 30, 30)
		p.pdf.SetStrokeColor(30, 30, 30)
		p.pdf.RectFromUpperLeftWithStyle(x, p.y, W, h, "FD")
		p.pdf.SetTextColor(255, 255, 255)
		p.fonte(true, 13)
		ly := p.y + (h-float64(len(linhas))*16)/2
		for _, l := range linhas {
			p.escrever(x+2*paddingQuadroPT, ly, W-4*paddingQuadroPT, 16, l, gopdf.Center|gopdf.Middle)
			ly += 16
		}
		p.pdf.SetTextColor(0, 0, 0)
		p.y += h + espacoEntreRotasPT
	}

	if len(doc.Pedidos) == 0 {
		h := 26.0
		p.quadro(x, p.y, W, h)
		p.fonte(false, 9)
		p.escrever(x, p.y, W, h, textoSemItensTermoBaixa, gopdf.Center|gopdf.Middle)
		p.y += h + espacoEntreRotasPT
	}

	p.usarColunasTermoBaixa()
	for pi, ped := range doc.Pedidos {
		p.pedidoTermoBaixa(pi, len(doc.Pedidos), ped)
	}

	if doc.ValorTotal != "" {
		p.calculoTotal(doc.ValorTotal)
	}

	// DADOS ADICIONAIS + ASSINATURAS formam um bloco só.
	linhasAprov, linhasSolic, hDados := p.medirDadosAdicionais(doc.Aprovador, doc.Solicitantes)
	p.garantir(alturaTituloSecaoPT + hDados + alturaAssinaturas(len(doc.Assinaturas)))
	p.desenharDadosAdicionais(linhasAprov, linhasSolic, hDados)
	p.desenharAssinaturas(quadroAssinaturasTB, doc.Assinaturas)

	return p.bytes(p.rotulo)
}

// textoLimpoPDF normaliza texto livre para o PDF: quebras de linha viram "\n",
// tabulação vira espaço, demais controles somem e UTF-8 inválido é descartado.
func textoLimpoPDF(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, s)
}

// linhasParagrafosPDF quebra `texto` respeitando "\n" (linha vazia preservada).
func (p *pintorPDF) linhasParagrafosPDF(texto string, largura float64) []string {
	var out []string
	for _, par := range strings.Split(textoLimpoPDF(texto), "\n") {
		out = append(out, p.quebrar(par, largura, false, tamanhoValorPreNotaPT)...)
	}
	return out
}

// linhasPedidoTermoBaixa devolve as linhas do quadro do Pedido (solicitante,
// obra e justificativa) e quantas delas são o cabeçalho (antes da justificativa).
func (p *pintorPDF) linhasPedidoTermoBaixa(ped pedidoTermoBaixa) (linhas []string, nCabecalho int) {
	largura := p.W - 2*paddingQuadroPT
	linhas = append(linhas, p.linhasParagrafosPDF("Solicitante: "+ped.Solicitante, largura)...)
	if ped.Obra != "" {
		linhas = append(linhas, p.linhasParagrafosPDF("Obra / centro de custo: "+ped.Obra, largura)...)
	}
	nCabecalho = len(linhas)
	linhas = append(linhas, p.linhasParagrafosPDF("Justificativa: "+ped.Justificativa, largura)...)
	return linhas, nCabecalho
}

// origemMedida é uma origem (Estoque) já medida para o desenho.
type origemMedida struct {
	campos     []campoDesenhado
	linhas     [][]string
	hFaixa     [4]float64
	hPartes    float64
	hPrimeiro  float64 // primeira linha da tabela (+ subtotal se for a única)
	hCabecalho float64 // título + quadro da origem + título + cabeçalho da tabela + 1ª linha
}

func (p *pintorPDF) medirOrigemTermoBaixa(o origemTermoBaixa) origemMedida {
	var m origemMedida
	m.campos = camposPartePreNota(quadroOrigemTermoBaixa, o.Origem)
	m.linhas = p.medirParte(m.campos, p.W, &m.hFaixa)
	m.hPartes = m.hFaixa[0] + m.hFaixa[1] + m.hFaixa[2] + m.hFaixa[3]
	_, m.hPrimeiro = p.alturasLinhaProduto(o.Produtos[0])
	if len(o.Produtos) == 1 {
		m.hPrimeiro += alturaSubtotalPT
	}
	m.hCabecalho = alturaTituloSecaoPT + m.hPartes + espacoEntreQuadrosPT + alturaTituloSecaoPT + alturaCabecalhoTabelaPT + m.hPrimeiro
	return m
}

// pedidoTermoBaixa desenha o bloco de UM Pedido: título com o solicitante, o
// quadro com obra e justificativa (uma vez só, quebrando linha e página) e,
// abaixo, os itens do Pedido agrupados por Estoque de origem. O cabeçalho do
// Pedido nunca fica órfão no fim da página: reserva título + até 3 linhas da
// justificativa + ORIGEM DA SAÍDA + cabeçalho e 1ª linha da tabela. Quando a
// justificativa atravessa páginas, a continuação repete solicitante e nº.
func (p *pintorPDF) pedidoTermoBaixa(indice, total int, ped pedidoTermoBaixa) {
	x, W := p.x, p.W
	largura := W - 2*paddingQuadroPT
	titulo := fmt.Sprintf("PEDIDO %d DE %d — %s", indice+1, total, ped.Solicitante)
	texto, nCab := p.linhasPedidoTermoBaixa(ped)

	medidas := make([]origemMedida, len(ped.Origens))
	for i, o := range ped.Origens {
		medidas[i] = p.medirOrigemTermoBaixa(o)
	}
	reserva := alturaTituloSecaoPT + 2*paddingQuadroPT + float64(min(len(texto), nCab+3))*entrelinhaPreNotaPT
	if len(medidas) > 0 {
		reserva += espacoEntreQuadrosPT + medidas[0].hCabecalho
	}
	p.garantir(reserva)
	p.tituloSecao(x, p.y, W, titulo)
	p.y += alturaTituloSecaoPT
	// O quadro pode ser maior que uma página: desenha linha a linha,
	// partindo em novas páginas (sem repartir uma linha).
	inicio := 0
	for inicio < len(texto) {
		disponivel := int((limiteYPreNotaPT - p.y - 2*paddingQuadroPT) / entrelinhaPreNotaPT)
		if disponivel < 1 {
			p.novaPagina()
			p.tituloSecao(x, p.y, W, titulo+" (continuação)")
			p.y += alturaTituloSecaoPT
			continue
		}
		fim := min(len(texto), inicio+disponivel)
		h := 2*paddingQuadroPT + float64(fim-inicio)*entrelinhaPreNotaPT
		p.quadro(x, p.y, W, h)
		p.fonte(false, tamanhoValorPreNotaPT)
		ly := p.y + paddingQuadroPT
		for _, l := range texto[inicio:fim] {
			p.escrever(x+paddingQuadroPT, ly, largura, entrelinhaPreNotaPT, l, gopdf.Left|gopdf.Middle)
			ly += entrelinhaPreNotaPT
		}
		p.y += h
		inicio = fim
		if inicio < len(texto) {
			p.novaPagina()
			p.tituloSecao(x, p.y, W, titulo+" (continuação)")
			p.y += alturaTituloSecaoPT
		}
	}
	p.y += espacoEntreQuadrosPT

	for oi, o := range ped.Origens {
		m := medidas[oi]
		if oi > 0 {
			p.y += espacoEntreRotasPT
		}
		p.garantir(m.hCabecalho)
		p.tituloSecao(x, p.y, W, quadroOrigemTermoBaixa)
		p.desenharParte(x, p.y+alturaTituloSecaoPT, W, m.campos, m.linhas, m.hFaixa)
		p.y += alturaTituloSecaoPT + m.hPartes + espacoEntreQuadrosPT
		p.tabelaProdutos(o.Produtos, tituloContinuacaoTermoBaixa(o), "SUBTOTAL DO ESTOQUE", o.Subtotal)
	}
	p.y += espacoEntreRotasPT
}

// GerarTermoBaixaPDF compõe MontarTermoBaixaConteudo + RenderizarTermoBaixaPDF
// e devolve também o número (nome do arquivo no Content-Disposition).
func GerarTermoBaixaPDF(db *sql.DB, empresaID, termoID string) ([]byte, int, error) {
	conteudo, err := MontarTermoBaixaConteudo(db, empresaID, termoID)
	if err != nil {
		return nil, 0, err
	}
	pdf, err := RenderizarTermoBaixaPDF(conteudo)
	if err != nil {
		return nil, 0, err
	}
	return pdf, conteudo.Numero, nil
}

// --- Acesso por solicitante e listagem (spec-termo-baixa-pendencias) --------

// sqlPedidoComItemAprovado: o Pedido `alias` tem ao menos 1 item listado no
// Termo (quantidade_aprovada > 0) — a mesma regra dos solicitantes e da
// contagem de itens do documento.
func sqlPedidoComItemAprovado(alias string) string {
	return "EXISTS (SELECT 1 FROM pedido_itens pi WHERE pi.pedido_id = " + alias + ".id AND pi.empresa_id = " + alias +
		".empresa_id AND pi.quantidade_aprovada > 0)"
}

// UsuarioSolicitouPedidoDoTermoBaixa diz se `usuarioID` é o Solicitante
// (`pedidos.usuario_id`, a identidade da sessão que enviou o Pedido) de ao
// menos um Pedido do Termo `termoID` da Empresa `empresaID` que tenha item
// listado no Termo (quantidade_aprovada > 0). Id malformado, inexistente ou de
// outra Empresa -> false.
func UsuarioSolicitouPedidoDoTermoBaixa(db *sql.DB, empresaID, termoID, usuarioID string) (bool, error) {
	var ok bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pedidos p
			WHERE p.termo_baixa_id = $1 AND p.empresa_id = $2 AND p.usuario_id = $3
			  AND `+sqlPedidoComItemAprovado("p")+`)`,
		termoID, empresaID, usuarioID).Scan(&ok)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return false, nil
		}
		return false, fmt.Errorf("falha ao verificar solicitante do termo de baixa: %w", err)
	}
	return ok, nil
}

// TermoBaixaResumo é uma linha da listagem de Termos de Baixa.
type TermoBaixaResumo struct {
	ID              string    `json:"id"`
	Numero          int       `json:"numero"`
	MotivoSaida     *string   `json:"motivoSaida"`
	CriadoEm        time.Time `json:"criadoEm"`
	Aprovador       string    `json:"aprovador"`
	Solicitantes    []string  `json:"solicitantes"`
	QuantidadeItens int       `json:"quantidadeItens"`
}

// FiltroTermosBaixa são os filtros opcionais da listagem. Zero value = sem filtro.
type FiltroTermosBaixa struct {
	Numero *int
	Motivo string // "" = qualquer
	De     string // AAAA-MM-DD (fuso de São Paulo), inclusivo
	Ate    string // AAAA-MM-DD (fuso de São Paulo), inclusivo
	Q      string // texto: solicitante ou aprovador
	Limite int    // 0 -> 50; máximo 200
	Offset int
}

const (
	limitePadraoTermosBaixa = 50
	limiteMaxTermosBaixa    = 200
	maxInt32TermosBaixa     = 1<<31 - 1
	maxRunasBuscaTermos     = 100
	anoMinFiltroTermos      = 2000
	anoMaxFiltroTermos      = 2100
)

// ListarTermosBaixa lista os Termos da Empresa, mais novos primeiro, e o total
// que casa com os filtros (sem limite/offset). Quando `soDoUsuario` é true
// (papéis abaixo de gestor) só entram os Termos com ao menos um Pedido de
// `usuarioID` com item listado. Filtro inválido -> *ErroPedidoValidacao.
func ListarTermosBaixa(db *sql.DB, empresaID, usuarioID string, soDoUsuario bool, f FiltroTermosBaixa) ([]TermoBaixaResumo, int, error) {
	invalido := func(msg string) error { return &ErroPedidoValidacao{Mensagem: msg} }
	where := []string{"tb.empresa_id = $1"}
	args := []any{empresaID}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if soDoUsuario {
		where = append(where, "EXISTS (SELECT 1 FROM pedidos pu WHERE pu.termo_baixa_id = tb.id AND pu.empresa_id = tb.empresa_id AND pu.usuario_id = "+
			arg(usuarioID)+" AND "+sqlPedidoComItemAprovado("pu")+")")
	}
	if f.Numero != nil {
		if *f.Numero <= 0 || *f.Numero > maxInt32TermosBaixa {
			return nil, 0, invalido("número do termo inválido")
		}
		where = append(where, "tb.numero = "+arg(*f.Numero))
	}
	if f.Motivo != "" {
		if !motivoSaidaValido(f.Motivo) {
			return nil, 0, invalido("motivo inválido")
		}
		where = append(where, "tb.motivo_saida = "+arg(f.Motivo))
	}
	dia := func(v, nome string) (time.Time, error) {
		t, err := time.ParseInLocation("2006-01-02", v, localPreNota)
		if err != nil || t.Year() < anoMinFiltroTermos || t.Year() > anoMaxFiltroTermos {
			return time.Time{}, invalido("data inválida em \"" + nome + "\" (use AAAA-MM-DD, anos 2000 a 2100)")
		}
		return t, nil
	}
	var de, ate time.Time
	if f.De != "" {
		t, err := dia(f.De, "de")
		if err != nil {
			return nil, 0, err
		}
		de = t
		where = append(where, "tb.criado_em >= "+arg(t))
	}
	if f.Ate != "" {
		t, err := dia(f.Ate, "ate")
		if err != nil {
			return nil, 0, err
		}
		ate = t
		where = append(where, "tb.criado_em < "+arg(t.AddDate(0, 0, 1)))
	}
	if !de.IsZero() && !ate.IsZero() && ate.Before(de) {
		return nil, 0, invalido("período inválido: \"ate\" anterior a \"de\"")
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		if !utf8.ValidString(q) || strings.ContainsRune(q, 0) || utf8.RuneCountInString(q) > maxRunasBuscaTermos {
			return nil, 0, invalido("texto de busca inválido")
		}
		p := arg("%" + escaparCoringasLike(q) + "%")
		where = append(where, `(u.nome ILIKE `+p+` ESCAPE '\' OR EXISTS (
			SELECT 1 FROM pedidos pq WHERE pq.termo_baixa_id = tb.id AND pq.empresa_id = tb.empresa_id
			  AND pq.solicitante ILIKE `+p+` ESCAPE '\' AND `+sqlPedidoComItemAprovado("pq")+`))`)
	}
	if f.Limite < 0 || f.Offset < 0 || f.Offset > maxInt32TermosBaixa {
		return nil, 0, invalido("paginação inválida")
	}
	limite := f.Limite
	if limite == 0 {
		limite = limitePadraoTermosBaixa
	}
	limite = min(limite, limiteMaxTermosBaixa)

	filtro := ` FROM termos_baixa tb LEFT JOIN usuarios u ON u.id = tb.decidido_por WHERE ` + strings.Join(where, " AND ")
	var total int
	if err := db.QueryRow(`SELECT count(*)`+filtro, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("falha ao contar termos de baixa: %w", err)
	}

	query := `
		SELECT tb.id::text, tb.numero, tb.motivo_saida, tb.criado_em, COALESCE(u.nome, ''),
		       COALESCE((SELECT array_agg(DISTINCT ps.solicitante ORDER BY ps.solicitante)
		                 FROM pedidos ps
		                 WHERE ps.termo_baixa_id = tb.id AND ps.empresa_id = tb.empresa_id
		                   AND ` + sqlPedidoComItemAprovado("ps") + `), '{}'),
		       (SELECT count(*) FROM pedido_itens pi JOIN pedidos pc ON pc.id = pi.pedido_id AND pc.empresa_id = pi.empresa_id
		        WHERE pc.termo_baixa_id = tb.id AND pi.empresa_id = tb.empresa_id AND pi.quantidade_aprovada > 0)` +
		filtro + `
		ORDER BY tb.numero DESC
		LIMIT ` + arg(limite) + ` OFFSET ` + arg(f.Offset)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("falha ao listar termos de baixa: %w", err)
	}
	defer rows.Close()
	out := make([]TermoBaixaResumo, 0)
	for rows.Next() {
		var r TermoBaixaResumo
		var motivo sql.NullString
		var solic pq.StringArray
		if err := rows.Scan(&r.ID, &r.Numero, &motivo, &r.CriadoEm, &r.Aprovador, &solic, &r.QuantidadeItens); err != nil {
			return nil, 0, fmt.Errorf("falha ao ler termo de baixa: %w", err)
		}
		if motivo.Valid {
			m := motivo.String
			r.MotivoSaida = &m
		}
		r.Solicitantes = []string(solic)
		if r.Solicitantes == nil {
			r.Solicitantes = []string{}
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("falha ao iterar termos de baixa: %w", err)
	}
	return out, total, nil
}
