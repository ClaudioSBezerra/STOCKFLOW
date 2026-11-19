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
		SELECT p.solicitante, p.observacao, p.obra_centro_custo FROM pedidos p
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
		if err := rows.Scan(&j.Solicitante, &obs, &obra); err != nil {
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
	quadroJustificativaTB           = "JUSTIFICATIVA"
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

type layoutTermoBaixa struct {
	Empresa        string
	CNPJ           string
	Numero         string
	Emissao        string
	Motivo         string // faixa de destaque
	Rodape         string // "TERMO DE BAIXA Nº 000.001 — <MOTIVO>", em toda página
	Origens        []origemTermoBaixa
	ValorTotal     string
	Justificativas []string // uma entrada por Pedido
	Aprovador      string
	Solicitantes   string
	Assinaturas    []blocoAssinatura
	Logo           *logoPreNota
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
	for _, j := range c.Justificativas {
		justificativa := strings.TrimSpace(j.Texto)
		if justificativa == "" {
			justificativa = textoSemJustificativaTermoBaixa
		}
		texto := textoOuAusentePreNota(j.Solicitante) + ": " + justificativa
		if strings.TrimSpace(j.Obra) != "" {
			texto += " (Obra / centro de custo: " + strings.TrimSpace(j.Obra) + ")"
		}
		l.Justificativas = append(l.Justificativas, texto)
	}

	chaveEstoque := func(id, nome string) string {
		if id != "" {
			return "id:" + id
		}
		return "nome:" + nome
	}
	type chave struct{ filial, estoque string }
	indice := map[chave]int{}
	subtotais := []*big.Int{}
	semValor := []bool{}
	total := new(big.Int)
	algumSemValor := false
	for _, it := range c.Itens {
		k := chave{it.FilialOrigem, chaveEstoque(it.EstoqueOrigemID, it.EstoqueOrigem)}
		i, ok := indice[k]
		if !ok {
			i = len(l.Origens)
			indice[k] = i
			l.Origens = append(l.Origens, origemTermoBaixa{
				Origem: parteDaFilialPreNota(empresa, it.FilialOrigem, it.EstoqueOrigem, it.FiscalOrigem),
			})
			subtotais = append(subtotais, new(big.Int))
			semValor = append(semValor, false)
		}
		valorLinha := textoAusentePreNota
		if it.PrecoCentavos != nil {
			v := valorLinhaCentavosPreNota(it.QuantidadeMilesimos, *it.PrecoCentavos)
			subtotais[i].Add(subtotais[i], v)
			total.Add(total, v)
			valorLinha = formatarCentavosPreNota(v)
		} else {
			algumSemValor = true
			semValor[i] = true
		}
		unidade := textoAusentePreNota
		if it.UnidadeMedida != nil {
			unidade = textoOuAusentePreNota(*it.UnidadeMedida)
		}
		l.Origens[i].Produtos = append(l.Origens[i].Produtos, produtoPreNota{
			Codigo:        textoOuAusentePreNota(it.ProdutoCodigo),
			Descricao:     textoOuAusentePreNota(it.Descricao),
			Unidade:       unidade,
			Quantidade:    formatarQuantidadePreNota(it.QuantidadeMilesimos),
			ValorUnitario: formatarPrecoPreNota(it.PrecoCentavos),
			ValorTotal:    valorLinha,
		})
	}
	for i := range l.Origens {
		l.Origens[i].Subtotal = formatarCentavosPreNota(subtotais[i])
		if semValor[i] {
			l.Origens[i].Subtotal += avisoTotalSemValor
		}
	}
	if len(l.Origens) > 0 {
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

	if len(doc.Origens) == 0 {
		h := 26.0
		p.quadro(x, p.y, W, h)
		p.fonte(false, 9)
		p.escrever(x, p.y, W, h, textoSemItensTermoBaixa, gopdf.Center|gopdf.Middle)
		p.y += h + espacoEntreRotasPT
	}

	for oi, o := range doc.Origens {
		campos := camposPartePreNota(quadroOrigemTermoBaixa, o.Origem)
		var hFaixa [4]float64
		linhas := p.medirParte(campos, W, &hFaixa)
		hPartes := hFaixa[0] + hFaixa[1] + hFaixa[2] + hFaixa[3]
		_, hPrimeiro := p.alturasLinhaProduto(o.Produtos[0])
		if len(o.Produtos) == 1 {
			hPrimeiro += alturaSubtotalPT
		}
		if oi > 0 {
			p.y += espacoEntreRotasPT
		}
		p.garantir(alturaTituloSecaoPT + hPartes + espacoEntreQuadrosPT + alturaTituloSecaoPT + alturaCabecalhoTabelaPT + hPrimeiro)
		p.tituloSecao(x, p.y, W, quadroOrigemTermoBaixa)
		p.desenharParte(x, p.y+alturaTituloSecaoPT, W, campos, linhas, hFaixa)
		p.y += alturaTituloSecaoPT + hPartes + espacoEntreQuadrosPT
		p.tabelaProdutos(o.Produtos, tituloContinuacaoTermoBaixa(o), "SUBTOTAL DO ESTOQUE", o.Subtotal)
	}

	if len(doc.Origens) > 0 {
		p.calculoTotal(doc.ValorTotal)
	}

	// JUSTIFICATIVA: uma linha por Pedido (solicitante + texto).
	{
		var texto []string
		for _, j := range doc.Justificativas {
			texto = append(texto, p.linhasCampo(j, W-2*paddingQuadroPT, false)...)
		}
		if len(texto) == 0 {
			texto = []string{textoAusentePreNota}
		}
		// O quadro pode ser maior que uma página: desenha linha a linha,
		// partindo em novas páginas (sem repartir uma linha).
		p.garantir(alturaTituloSecaoPT + p.alturaCampo(min(len(texto), 3)))
		p.tituloSecao(x, p.y, W, quadroJustificativaTB)
		p.y += alturaTituloSecaoPT
		inicio := 0
		for inicio < len(texto) {
			disponivel := int((limiteYPreNotaPT - p.y - 2*paddingQuadroPT) / entrelinhaPreNotaPT)
			if disponivel < 1 {
				p.novaPagina()
				continue
			}
			fim := min(len(texto), inicio+disponivel)
			h := 2*paddingQuadroPT + float64(fim-inicio)*entrelinhaPreNotaPT
			p.quadro(x, p.y, W, h)
			p.fonte(false, tamanhoValorPreNotaPT)
			ly := p.y + paddingQuadroPT
			for _, l := range texto[inicio:fim] {
				p.escrever(x+paddingQuadroPT, ly, W-2*paddingQuadroPT, entrelinhaPreNotaPT, l, gopdf.Left|gopdf.Middle)
				ly += entrelinhaPreNotaPT
			}
			p.y += h
			inicio = fim
			if inicio < len(texto) {
				p.novaPagina()
			}
		}
		p.y += espacoEntreRotasPT
	}

	// DADOS ADICIONAIS + ASSINATURAS formam um bloco só.
	linhasAprov, linhasSolic, hDados := p.medirDadosAdicionais(doc.Aprovador, doc.Solicitantes)
	p.garantir(alturaTituloSecaoPT + hDados + alturaAssinaturas(len(doc.Assinaturas)))
	p.desenharDadosAdicionais(linhasAprov, linhasSolic, hDados)
	p.desenharAssinaturas(quadroAssinaturasTB, doc.Assinaturas)

	return p.bytes(p.rotulo)
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
