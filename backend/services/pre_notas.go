package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
	// Base de fusos embutida no binário: a imagem final é alpine, sem
	// /usr/share/zoneinfo — sem isto, America/Sao_Paulo não carregaria lá.
	_ "time/tzdata"

	"github.com/lib/pq"
	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// --- Documento impresso da Pré-Nota — Story 20.5 (Epic 20), spec-20-5 -------
//
// Molde do recibo de Pedido (Story 7.6, pedidos.go): MontarPreNotaConteudo
// (busca, sem gopdf) / layoutDaPreNota (texto do documento já formatado e
// agrupado por rota — função pura, testável sem ler bytes de PDF) /
// RenderizarPreNotaPDF (só desenha, no estilo DANFE, determinística) /
// GerarPreNotaPDF (tudo em sequência — o que BaixarPreNotaPDFHandler chama).
// Layout DANFE: spec-pre-nota-layout-danfe.
//
// O que é congelado (Spec Change Log de spec-20-5, decisão do usuário): SÓ
// preço e unidade, gravados em `pedido_itens` na aprovação em lote. Código do
// produto, nomes das Filiais, nome do Estoque de destino, razão social/CNPJ
// e nome do Aprovador vêm do cadastro atual no momento do download (mesmo
// critério do recibo de Pedido) — renomear algum deles muda o próximo
// download. O nome do Estoque de origem é o snapshot `pedido_itens.estoque_nome`. A renderização em
// si é determinística: mesma entrada -> mesmos bytes.

// ErrPreNotaNaoEncontrada colapsa id malformado, inexistente ou de OUTRA
// Empresa no MESMO erro — 404 NOT_FOUND na fronteira HTTP (mesmo colapso de
// ErrPedidoNaoEncontrado no recibo de Pedido).
var ErrPreNotaNaoEncontrada = errors.New("pré-nota não encontrada")

const (
	// textoAusentePreNota marca dado ausente (snapshot NULL de Pré-Nota
	// anterior à Story 20.5 ou Produto sem preço, Estoque sem Filial, etc.)
	// — nunca quebra a geração.
	textoAusentePreNota = "—"
	// RodapePreNota é impresso no pé de TODA página do documento.
	RodapePreNota         = "Documento interno de controle — não substitui a Nota Fiscal"
	textoSemItensPreNota  = "Nenhum item movimentado nesta Pré-Nota"
	avisoTotalSemValor    = " (há itens sem valor cadastrado)"
	fusoHorarioPreNota    = "America/Sao_Paulo"
	alturaRodapePreNotaPT = 20.0
)

// localPreNota é o fuso em que a Data é impressa — `criado_em` vem do banco
// no fuso da conexão (normalmente UTC); o usuário está no Brasil.
var localPreNota = func() *time.Location {
	loc, err := time.LoadLocation(fusoHorarioPreNota)
	if err != nil {
		// Inalcançável com time/tzdata embutido; fallback fixo por garantia.
		return time.FixedZone("BRT", -3*60*60)
	}
	return loc
}()

// PreNotaItem é uma linha do documento. Valores monetários e quantidades
// são inteiros exatos (centavos / milésimos), nunca float binário. Preço e
// unidade vêm SEMPRE do snapshot gravado em `pedido_itens` na aprovação em
// lote — nil quando o snapshot é NULL.
type PreNotaItem struct {
	Tipo                string // "transferencia" | "baixa" | outro (impresso como veio)
	ProdutoCodigo       string // código interno atual; "" quando o Produto não tem código
	Descricao           string // `pedido_itens.produto_nome` (snapshot do envio)
	QuantidadeMilesimos int64  // `quantidade_aprovada` × 1000 (NUMERIC(10,3), exato)
	UnidadeMedida       *string
	PrecoCentavos       *int64 // `preco_unitario` × 100 (NUMERIC(12,2), exato)
	FilialOrigem        string // "" quando o Estoque de origem não tem Filial
	FilialDestino       string // só Transferência; "" nos demais
	EstoqueOrigem       string // `pedido_itens.estoque_nome` (snapshot do envio)
	EstoqueDestino      string // só Transferência: `estoques.nome` atual do destino; "" nos demais
}

// PreNotaConteudo é o conteúdo já resolvido do documento — struct de dados
// simples, SEM dependência de gopdf (mesmo desenho de ReciboPedidoConteudo).
type PreNotaConteudo struct {
	PreNotaID          string
	Docnum             int
	EmpresaRazaoSocial string
	EmpresaCNPJ        string // 14 dígitos, sem máscara (formatado no texto)
	CriadoEm           time.Time
	Aprovador          string
	Solicitantes       []string // distintos, ordenados, só de Pedidos com item listado
	Itens              []PreNotaItem
}

// MontarPreNotaConteudo busca e monta o conteúdo da Pré-Nota `preNotaID` da
// Empresa `empresaID`. Filtro por `empresa_id` em TODA consulta: outra
// Empresa, id inexistente ou malformado -> ErrPreNotaNaoEncontrada. O gate de
// papel (`gestor`+) fica na rota (RequireRole), não aqui. As três leituras
// (cabeçalho, solicitantes, itens) rodam numa MESMA transação somente-leitura
// REPEATABLE READ — enxergam o mesmo instante do banco.
func MontarPreNotaConteudo(db *sql.DB, empresaID, preNotaID string) (PreNotaConteudo, error) {
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return PreNotaConteudo{}, fmt.Errorf("falha ao iniciar leitura da pré-nota: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // somente-leitura: nada a commitar

	var c PreNotaConteudo
	var aprovador sql.NullString
	const selectCabecalho = `
		SELECT pn.id, pn.docnum, pn.criado_em, e.razao_social, e.cnpj, u.nome
		FROM pre_notas pn
		JOIN empresas e ON e.id = pn.empresa_id
		LEFT JOIN usuarios u ON u.id = pn.decidido_por
		WHERE pn.id = $1 AND pn.empresa_id = $2`
	if err := tx.QueryRow(selectCabecalho, preNotaID, empresaID).Scan(
		&c.PreNotaID, &c.Docnum, &c.CriadoEm, &c.EmpresaRazaoSocial, &c.EmpresaCNPJ, &aprovador,
	); err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return PreNotaConteudo{}, ErrPreNotaNaoEncontrada
		}
		return PreNotaConteudo{}, fmt.Errorf("falha ao buscar pré-nota: %w", err)
	}
	c.EmpresaCNPJ = strings.TrimSpace(c.EmpresaCNPJ)
	if aprovador.Valid {
		c.Aprovador = aprovador.String
	}

	// Solicitantes: só dos Pedidos com ao menos 1 item listado no documento
	// (quantidade_aprovada > 0) — um Pedido todo zerado não põe nome no
	// cabeçalho sem nenhuma linha correspondente.
	const selectSolicitantes = `
		SELECT DISTINCT p.solicitante FROM pedidos p
		WHERE p.pre_nota_id = $1 AND p.empresa_id = $2
		  AND EXISTS (
		    SELECT 1 FROM pedido_itens pi
		    WHERE pi.pedido_id = p.id AND pi.empresa_id = p.empresa_id AND pi.quantidade_aprovada > 0)
		ORDER BY p.solicitante`
	rows, err := tx.Query(selectSolicitantes, preNotaID, empresaID)
	if err != nil {
		return PreNotaConteudo{}, fmt.Errorf("falha ao listar solicitantes da pré-nota: %w", err)
	}
	c.Solicitantes = make([]string, 0)
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return PreNotaConteudo{}, fmt.Errorf("falha ao ler solicitante da pré-nota: %w", err)
		}
		c.Solicitantes = append(c.Solicitantes, s)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return PreNotaConteudo{}, fmt.Errorf("falha ao iterar solicitantes da pré-nota: %w", err)
	}
	rows.Close()

	// Itens: só `quantidade_aprovada > 0`. Quantidade e preço lidos como
	// inteiros exatos (milésimos/centavos) — as escalas das colunas
	// (NUMERIC(10,3)/NUMERIC(12,2)) garantem que a multiplicação é exata.
	// Filial origem/destino via `estoques.filial_id`. Ordem total e estável
	// para a renderização ser determinística.
	const selectItens = `
		SELECT p.tipo, pr.codigo, pi.produto_nome, (pi.quantidade_aprovada * 1000)::bigint,
		       pi.unidade_medida::text, (pi.preco_unitario * 100)::bigint, fo.nome, fd.nome,
		       pi.estoque_nome, ed.nome
		FROM pedido_itens pi
		JOIN pedidos p ON p.id = pi.pedido_id AND p.empresa_id = pi.empresa_id
		LEFT JOIN produtos pr ON pr.id = pi.produto_id AND pr.empresa_id = pi.empresa_id
		LEFT JOIN estoques eo ON eo.id = pi.estoque_id AND eo.empresa_id = pi.empresa_id
		LEFT JOIN filiais fo ON fo.id = eo.filial_id AND fo.empresa_id = pi.empresa_id
		LEFT JOIN estoques ed ON ed.id = pi.estoque_destino_id AND ed.empresa_id = pi.empresa_id
		LEFT JOIN filiais fd ON fd.id = ed.filial_id AND fd.empresa_id = pi.empresa_id
		WHERE p.pre_nota_id = $1 AND pi.empresa_id = $2 AND pi.quantidade_aprovada > 0
		ORDER BY pi.produto_nome, p.id, pi.produto_id, pi.estoque_id`
	itensRows, err := tx.Query(selectItens, preNotaID, empresaID)
	if err != nil {
		return PreNotaConteudo{}, fmt.Errorf("falha ao listar itens da pré-nota: %w", err)
	}
	defer itensRows.Close()
	c.Itens = make([]PreNotaItem, 0)
	for itensRows.Next() {
		var it PreNotaItem
		var codigo, unidade, filialOrigem, filialDestino, estoqueOrigem, estoqueDestino sql.NullString
		var preco sql.NullInt64
		if err := itensRows.Scan(
			&it.Tipo, &codigo, &it.Descricao, &it.QuantidadeMilesimos, &unidade, &preco, &filialOrigem, &filialDestino,
			&estoqueOrigem, &estoqueDestino,
		); err != nil {
			return PreNotaConteudo{}, fmt.Errorf("falha ao ler item da pré-nota: %w", err)
		}
		if codigo.Valid {
			it.ProdutoCodigo = codigo.String
		}
		if unidade.Valid {
			u := unidade.String
			it.UnidadeMedida = &u
		}
		if preco.Valid {
			v := preco.Int64
			it.PrecoCentavos = &v
		}
		if filialOrigem.Valid {
			it.FilialOrigem = filialOrigem.String
		}
		if estoqueOrigem.Valid {
			it.EstoqueOrigem = estoqueOrigem.String
		}
		if it.Tipo == "transferencia" {
			if filialDestino.Valid {
				it.FilialDestino = filialDestino.String
			}
			if estoqueDestino.Valid {
				it.EstoqueDestino = estoqueDestino.String
			}
		}
		c.Itens = append(c.Itens, it)
	}
	if err := itensRows.Err(); err != nil {
		return PreNotaConteudo{}, fmt.Errorf("falha ao iterar itens da pré-nota: %w", err)
	}
	return c, nil
}

// --- Formatação (funções puras) -------------------------------------------

// formatarCNPJPreNota aplica a máscara 00.000.000/0000-00 a um CNPJ de 14
// dígitos; qualquer outro formato é devolvido como veio.
func formatarCNPJPreNota(cnpj string) string {
	if len(cnpj) != 14 {
		return cnpj
	}
	return cnpj[0:2] + "." + cnpj[2:5] + "." + cnpj[5:8] + "/" + cnpj[8:12] + "-" + cnpj[12:14]
}

// agruparMilharPreNota insere "." a cada 3 dígitos de uma sequência de
// dígitos sem sinal ("1234567" -> "1.234.567").
func agruparMilharPreNota(digitos string) string {
	var b strings.Builder
	for i, r := range digitos {
		if i > 0 && (len(digitos)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// formatarCentavosPreNota formata um valor em centavos como "R$ 1.234,56".
func formatarCentavosPreNota(centavos *big.Int) string {
	sinal := ""
	abs := new(big.Int).Set(centavos)
	if abs.Sign() < 0 {
		sinal = "-"
		abs.Neg(abs)
	}
	s := abs.String()
	for len(s) < 3 {
		s = "0" + s
	}
	return "R$ " + sinal + agruparMilharPreNota(s[:len(s)-2]) + "," + s[len(s)-2:]
}

// formatarPrecoPreNota formata o preço unitário do snapshot; nil -> "—".
func formatarPrecoPreNota(centavos *int64) string {
	if centavos == nil {
		return textoAusentePreNota
	}
	return formatarCentavosPreNota(big.NewInt(*centavos))
}

// formatarQuantidadePreNota formata milésimos sem zeros à direita, no padrão
// PT-BR do resto do documento: 6000 -> "6", 2500 -> "2,5", 1234500 ->
// "1.234,5".
func formatarQuantidadePreNota(milesimos int64) string {
	sinal := ""
	if milesimos < 0 {
		sinal = "-"
		milesimos = -milesimos
	}
	inteiro := agruparMilharPreNota(strconv.FormatInt(milesimos/1000, 10))
	frac := strings.TrimRight(fmt.Sprintf("%03d", milesimos%1000), "0")
	if frac == "" {
		return sinal + inteiro
	}
	return sinal + inteiro + "," + frac
}

// valorLinhaCentavosPreNota = quantidade × preço, em centavos, com
// arredondamento meio-para-cima (inteiros exatos via math/big, nunca float):
// (milésimos × centavos) / 1000.
func valorLinhaCentavosPreNota(milesimos, precoCentavos int64) *big.Int {
	produto := new(big.Int).Mul(big.NewInt(milesimos), big.NewInt(precoCentavos))
	negativo := produto.Sign() < 0
	produto.Abs(produto)
	produto.Add(produto, big.NewInt(500))
	produto.Quo(produto, big.NewInt(1000))
	if negativo {
		produto.Neg(produto)
	}
	return produto
}

// formatarDataPreNota imprime a Data sempre no fuso de São Paulo.
func formatarDataPreNota(t time.Time) string {
	return t.In(localPreNota).Format("02/01/2006 15:04")
}

func rotuloTipoPreNota(tipo string) string {
	switch tipo {
	case "baixa":
		return "Baixa"
	case "transferencia":
		return "Transferência"
	default:
		return tipo
	}
}

func textoOuAusentePreNota(s string) string {
	if strings.TrimSpace(s) == "" {
		return textoAusentePreNota
	}
	return s
}

// --- Modelo de layout (funções puras) — spec-pre-nota-layout-danfe --------
//
// layoutDaPreNota converte o conteúdo montado no que o documento imprime, já
// formatado e agrupado por rota — é o que os testes conferem sem ler bytes de
// PDF. RenderizarPreNotaPDF só posiciona estes textos nos quadros.

const (
	naturezaTransferenciaPreNota = "Transferência"
	naturezaBaixaPreNota         = "Baixa de estoque"
	// textoSemDestinatarioPreNota ocupa o quadro de Destinatário numa Baixa:
	// o material foi consumido, não foi para outro Estoque.
	textoSemDestinatarioPreNota = "BAIXA DE ESTOQUE — consumo, sem destinatário"
)

// partePreNota é um lado da rota (Remetente ou Destinatário), já formatado
// ("—" no que faltar). Empresa e CNPJ são os da própria Empresa — a Pré-Nota
// é sempre interna.
type partePreNota struct {
	Empresa string
	CNPJ    string
	Filial  string
	Estoque string
}

// produtoPreNota é uma linha da tabela DADOS DOS PRODUTOS, já formatada.
type produtoPreNota struct {
	Codigo        string
	Descricao     string
	Unidade       string
	Quantidade    string
	ValorUnitario string
	ValorTotal    string
}

// rotaPreNota agrupa os itens de um mesmo par Estoque origem -> Estoque
// destino (Baixa: mesmo Estoque origem). Destinatario nil = sem
// destinatário; o quadro mostra SemDestinatario no lugar.
type rotaPreNota struct {
	Natureza        string
	Remetente       partePreNota
	Destinatario    *partePreNota
	SemDestinatario string
	Produtos        []produtoPreNota
	Subtotal        string
}

// layoutPreNota é o documento inteiro, na ordem impressa (o rodapé
// RodapePreNota + "Página N" é desenhado em toda página). Rotas vazio = sem
// itens: o documento mostra textoSemItensPreNota e não tem CÁLCULO DO TOTAL.
type layoutPreNota struct {
	Empresa      string
	CNPJ         string
	Numero       string // "Nº 000.001"
	Emissao      string // "Emissão: dd/mm/aaaa hh:mm" (São Paulo)
	Rotas        []rotaPreNota
	ValorTotal   string // com avisoTotalSemValor quando há item sem preço
	Aprovador    string
	Solicitantes string
}

// formatarDocnumPreNota: docnum com 6 dígitos e ponto de milhar ("Nº 000.001").
func formatarDocnumPreNota(docnum int) string {
	if docnum < 0 {
		return fmt.Sprintf("Nº %d", docnum)
	}
	return "Nº " + agruparMilharPreNota(fmt.Sprintf("%06d", docnum))
}

func layoutDaPreNota(c PreNotaConteudo) layoutPreNota {
	empresa := textoOuAusentePreNota(c.EmpresaRazaoSocial)
	cnpj := textoOuAusentePreNota(formatarCNPJPreNota(c.EmpresaCNPJ))
	solicitantes := textoAusentePreNota
	if len(c.Solicitantes) > 0 {
		solicitantes = strings.Join(c.Solicitantes, ", ")
	}
	l := layoutPreNota{
		Empresa:      empresa,
		CNPJ:         cnpj,
		Numero:       formatarDocnumPreNota(c.Docnum),
		Emissao:      "Emissão: " + formatarDataPreNota(c.CriadoEm),
		Aprovador:    textoOuAusentePreNota(c.Aprovador),
		Solicitantes: solicitantes,
	}

	// Agrupamento por rota na ordem da 1ª aparição — a ordem dos itens já é
	// total e estável (ORDER BY de MontarPreNotaConteudo), então o documento
	// também é. A chave usa os nomes impressos (Filial + Estoque de cada
	// lado): duas rotas indistinguíveis no papel viram um bloco só.
	type chaveRota struct{ tipo, filialO, estoqueO, filialD, estoqueD string }
	indice := make(map[chaveRota]int)
	subtotais := make([]*big.Int, 0)
	total := new(big.Int)
	algumSemValor := false
	for _, it := range c.Itens {
		transf := it.Tipo == "transferencia"
		k := chaveRota{tipo: it.Tipo, filialO: it.FilialOrigem, estoqueO: it.EstoqueOrigem}
		if transf {
			k.filialD, k.estoqueD = it.FilialDestino, it.EstoqueDestino
		}
		i, ok := indice[k]
		if !ok {
			r := rotaPreNota{
				Natureza: rotuloTipoPreNota(it.Tipo),
				Remetente: partePreNota{Empresa: empresa, CNPJ: cnpj,
					Filial: textoOuAusentePreNota(it.FilialOrigem), Estoque: textoOuAusentePreNota(it.EstoqueOrigem)},
				SemDestinatario: textoAusentePreNota,
			}
			switch it.Tipo {
			case "transferencia":
				r.Natureza = naturezaTransferenciaPreNota
				r.Destinatario = &partePreNota{Empresa: empresa, CNPJ: cnpj,
					Filial: textoOuAusentePreNota(it.FilialDestino), Estoque: textoOuAusentePreNota(it.EstoqueDestino)}
				r.SemDestinatario = ""
			case "baixa":
				r.Natureza = naturezaBaixaPreNota
				r.SemDestinatario = textoSemDestinatarioPreNota
			}
			i = len(l.Rotas)
			indice[k] = i
			l.Rotas = append(l.Rotas, r)
			subtotais = append(subtotais, new(big.Int))
		}

		valorLinha := textoAusentePreNota
		if it.PrecoCentavos != nil {
			v := valorLinhaCentavosPreNota(it.QuantidadeMilesimos, *it.PrecoCentavos)
			subtotais[i].Add(subtotais[i], v)
			total.Add(total, v)
			valorLinha = formatarCentavosPreNota(v)
		} else {
			algumSemValor = true
		}
		unidade := textoAusentePreNota
		if it.UnidadeMedida != nil {
			unidade = textoOuAusentePreNota(*it.UnidadeMedida)
		}
		l.Rotas[i].Produtos = append(l.Rotas[i].Produtos, produtoPreNota{
			Codigo:        textoOuAusentePreNota(it.ProdutoCodigo),
			Descricao:     textoOuAusentePreNota(it.Descricao),
			Unidade:       unidade,
			Quantidade:    formatarQuantidadePreNota(it.QuantidadeMilesimos),
			ValorUnitario: formatarPrecoPreNota(it.PrecoCentavos),
			ValorTotal:    valorLinha,
		})
	}
	for i := range l.Rotas {
		l.Rotas[i].Subtotal = formatarCentavosPreNota(subtotais[i])
	}
	if len(l.Rotas) > 0 {
		l.ValorTotal = formatarCentavosPreNota(total)
		if algumSemValor {
			l.ValorTotal += avisoTotalSemValor
		}
	}
	return l
}

// --- Renderização (estilo DANFE) -------------------------------------------

const (
	margemPreNotaPT        = 28.0
	larguraPaginaPreNotaPT = 595.0
	larguraUtilPreNotaPT   = larguraPaginaPreNotaPT - 2*margemPreNotaPT
	alturaPaginaPreNotaPT  = 842.0
	// limiteYPreNotaPT: abaixo daqui fica só o rodapé.
	limiteYPreNotaPT = alturaPaginaPreNotaPT - margemPreNotaPT - alturaRodapePreNotaPT

	tamanhoRotuloPreNotaPT  = 6.0 // rótulo em maiúsculas no canto do quadro
	tamanhoValorPreNotaPT   = 8.5 // valor dentro do quadro
	entrelinhaPreNotaPT     = 10.5
	tamanhoItemPreNotaPT    = 8.0 // texto da tabela de produtos
	entrelinhaItemPreNotaPT = 10.0
	alturaTituloSecaoPT     = 11.0 // "REMETENTE (ORIGEM)", "DADOS DOS PRODUTOS"...
	alturaCabecalhoTabelaPT = 14.0
	espacoEntreQuadrosPT    = 4.0
	espacoEntreRotasPT      = 10.0
	paddingQuadroPT         = 3.0
)

// colunasProdutosPreNota: Código | Descrição | Un. | Qtd. | Valor unit. |
// Valor total — larguras somam larguraUtilPreNotaPT (539pt).
var colunasProdutosPreNota = []struct {
	Titulo  string
	Largura float64
	Align   int
}{
	{"CÓDIGO", 58, gopdf.Left},
	{"DESCRIÇÃO DO PRODUTO", 232, gopdf.Left},
	{"UN.", 34, gopdf.Center},
	{"QTD.", 55, gopdf.Right},
	{"VALOR UNIT.", 75, gopdf.Right},
	{"VALOR TOTAL", 85, gopdf.Right},
}

// RenderizarPreNotaPDF desenha a Pré-Nota a partir de um PreNotaConteudo JÁ
// MONTADO, no estilo DANFE (A4 retrato): quadros com borda fina e rótulo
// pequeno em maiúsculas, tabela de produtos com cabeçalho em caixa e uma
// linha entre cada item. Função pura e determinística (mesma regra de
// RenderizarReciboPedidoPDF: sem `time.Now()`, sem `pdf.SetInfo`).
//
// Quebra de página: os quadros Remetente/Destinatário de uma rota só são
// desenhados se couberem junto do cabeçalho da tabela e do 1º item; um item
// nunca é dividido; tabela que continua na página seguinte repete o
// cabeçalho. O rodapé (texto + "Página N") é desenhado em toda página.
func RenderizarPreNotaPDF(conteudo PreNotaConteudo) ([]byte, error) {
	doc := layoutDaPreNota(conteudo)

	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.SetMargins(margemPreNotaPT, margemPreNotaPT, margemPreNotaPT, margemPreNotaPT)
	if err := pdf.AddTTFFontData("gofont", goregular.TTF); err != nil {
		return nil, fmt.Errorf("falha ao carregar fonte da pré-nota: %w", err)
	}
	if err := pdf.AddTTFFontData("gofont-negrito", gobold.TTF); err != nil {
		return nil, fmt.Errorf("falha ao carregar fonte negrito da pré-nota: %w", err)
	}

	// Primeiro erro de desenho; as funções abaixo viram no-op depois dele.
	var erro error
	fonte := func(negrito bool, tamanho float64) {
		if erro != nil {
			return
		}
		familia := "gofont"
		if negrito {
			familia = "gofont-negrito"
		}
		if err := pdf.SetFont(familia, "", tamanho); err != nil {
			erro = fmt.Errorf("falha ao selecionar fonte da pré-nota: %w", err)
		}
	}
	// escrever põe `texto` na caixa (x, y, w, h), alinhado por `align`.
	escrever := func(x, y, w, h float64, texto string, align int) {
		if erro != nil || texto == "" {
			return
		}
		pdf.SetXY(x, y)
		if err := pdf.CellWithOption(&gopdf.Rect{W: w, H: h}, texto, gopdf.CellOption{Align: align}); err != nil {
			erro = fmt.Errorf("falha ao escrever texto da pré-nota: %w", err)
		}
	}
	// quebrar divide `texto` em linhas que cabem em `largura` com a fonte
	// indicada — texto longo quebra, nunca é cortado.
	quebrar := func(texto string, largura float64, negrito bool, tamanho float64) []string {
		fonte(negrito, tamanho)
		if erro != nil || texto == "" {
			return []string{""}
		}
		partes, err := pdf.SplitTextWithWordWrap(texto, largura)
		if err != nil {
			erro = fmt.Errorf("falha ao quebrar texto da pré-nota: %w", err)
			return []string{""}
		}
		if len(partes) == 0 {
			return []string{""}
		}
		return partes
	}
	traco := func(largura float64, cinza uint8) {
		pdf.SetLineWidth(largura)
		pdf.SetStrokeColor(cinza, cinza, cinza)
	}
	quadro := func(x, y, w, h float64) {
		traco(0.6, 0)
		pdf.RectFromUpperLeftWithStyle(x, y, w, h, "D")
	}

	// Campo = rótulo pequeno no canto superior esquerdo + valor (quebrado na
	// largura do campo). Altura medida antes de desenhar.
	alturaCampo := func(linhas int) float64 {
		return paddingQuadroPT + tamanhoRotuloPreNotaPT + 2 + float64(linhas)*entrelinhaPreNotaPT + paddingQuadroPT
	}
	linhasCampo := func(valor string, w float64, negrito bool) []string {
		return quebrar(valor, w-2*paddingQuadroPT, negrito, tamanhoValorPreNotaPT)
	}
	desenharCampo := func(x, y, w float64, rotulo string, linhas []string, negrito bool) {
		fonte(false, tamanhoRotuloPreNotaPT)
		pdf.SetTextColor(70, 70, 70)
		escrever(x+paddingQuadroPT, y+paddingQuadroPT, w-2*paddingQuadroPT, tamanhoRotuloPreNotaPT, rotulo, gopdf.Left|gopdf.Top)
		pdf.SetTextColor(0, 0, 0)
		fonte(negrito, tamanhoValorPreNotaPT)
		ly := y + paddingQuadroPT + tamanhoRotuloPreNotaPT + 2
		for _, l := range linhas {
			escrever(x+paddingQuadroPT, ly, w-2*paddingQuadroPT, entrelinhaPreNotaPT, l, gopdf.Left|gopdf.Middle)
			ly += entrelinhaPreNotaPT
		}
	}
	tituloSecao := func(x, y, w float64, titulo string) {
		fonte(true, 7)
		escrever(x, y, w, alturaTituloSecaoPT, titulo, gopdf.Left|gopdf.Middle)
	}

	pdf.AddFooter(func() {
		yLinha := alturaPaginaPreNotaPT - margemPreNotaPT - 14
		traco(0.4, 120)
		pdf.Line(margemPreNotaPT, yLinha, margemPreNotaPT+larguraUtilPreNotaPT, yLinha)
		pdf.SetTextColor(70, 70, 70)
		fonte(false, 7)
		escrever(margemPreNotaPT, yLinha+3, larguraUtilPreNotaPT-60, 10, RodapePreNota, gopdf.Left|gopdf.Middle)
		escrever(margemPreNotaPT+larguraUtilPreNotaPT-60, yLinha+3, 60, 10,
			fmt.Sprintf("Página %d", pdf.GetNumberOfPages()), gopdf.Right|gopdf.Middle)
		pdf.SetTextColor(0, 0, 0)
	})

	x := margemPreNotaPT
	W := larguraUtilPreNotaPT
	topoY := margemPreNotaPT
	y := topoY
	novaPagina := func() {
		pdf.AddPage()
		pdf.SetTextColor(0, 0, 0)
		y = topoY
	}
	// cabe: há espaço para `h` pontos a partir de y? No topo da página
	// sempre "cabe" (um bloco maior que a página é desenhado assim mesmo).
	garantir := func(h float64) {
		if y+h > limiteYPreNotaPT && y > topoY {
			novaPagina()
		}
	}
	novaPagina()

	// 1. Cabeçalho: Empresa | PRÉ-NOTA | Nº + Emissão.
	{
		wE, wC := 230.0, 170.0
		wN := W - wE - wC
		linhasEmpresa := linhasCampo(doc.Empresa, wE, true)
		linhasEmpresa = append(linhasEmpresa, linhasCampo("CNPJ: "+doc.CNPJ, wE, false)...)
		linhasAviso := quebrar(RodapePreNota, wC-2*paddingQuadroPT, false, 7)
		h := alturaCampo(len(linhasEmpresa))
		if hC := 6 + 18 + 4 + float64(len(linhasAviso))*9 + 6; hC > h {
			h = hC
		}
		if h < 56 {
			h = 56
		}
		quadro(x, y, W, h)
		traco(0.6, 0)
		pdf.Line(x+wE, y, x+wE, y+h)
		pdf.Line(x+wE+wC, y, x+wE+wC, y+h)

		// Empresa: razão social em negrito, CNPJ embaixo.
		fonte(false, tamanhoRotuloPreNotaPT)
		pdf.SetTextColor(70, 70, 70)
		escrever(x+paddingQuadroPT, y+paddingQuadroPT, wE, tamanhoRotuloPreNotaPT, "EMPRESA", gopdf.Left|gopdf.Top)
		pdf.SetTextColor(0, 0, 0)
		ly := y + paddingQuadroPT + tamanhoRotuloPreNotaPT + 2
		for i, l := range linhasEmpresa {
			fonte(i < len(linhasEmpresa)-1, tamanhoValorPreNotaPT)
			escrever(x+paddingQuadroPT, ly, wE-2*paddingQuadroPT, entrelinhaPreNotaPT, l, gopdf.Left|gopdf.Middle)
			ly += entrelinhaPreNotaPT
		}

		// Centro: título em destaque + aviso.
		fonte(true, 16)
		escrever(x+wE, y+6, wC, 18, "PRÉ-NOTA", gopdf.Center|gopdf.Middle)
		fonte(false, 7)
		ly = y + 6 + 18 + 4
		for _, l := range linhasAviso {
			escrever(x+wE+paddingQuadroPT, ly, wC-2*paddingQuadroPT, 9, l, gopdf.Center|gopdf.Middle)
			ly += 9
		}

		// Direita: número e emissão.
		xN := x + wE + wC
		fonte(false, tamanhoRotuloPreNotaPT)
		pdf.SetTextColor(70, 70, 70)
		escrever(xN+paddingQuadroPT, y+paddingQuadroPT, wN, tamanhoRotuloPreNotaPT, "NÚMERO", gopdf.Left|gopdf.Top)
		pdf.SetTextColor(0, 0, 0)
		fonte(true, 13)
		escrever(xN, y+12, wN, 18, doc.Numero, gopdf.Center|gopdf.Middle)
		fonte(false, tamanhoValorPreNotaPT)
		escrever(xN, y+34, wN, 12, doc.Emissao, gopdf.Center|gopdf.Middle)

		y += h + espacoEntreRotasPT
	}

	// Tabela de produtos.
	alturasLinhaProduto := func(p produtoPreNota) ([][]string, float64) {
		valores := []string{p.Codigo, p.Descricao, p.Unidade, p.Quantidade, p.ValorUnitario, p.ValorTotal}
		celulas := make([][]string, len(valores))
		maior := 1
		for i, v := range valores {
			celulas[i] = quebrar(v, colunasProdutosPreNota[i].Largura-2*paddingQuadroPT, false, tamanhoItemPreNotaPT)
			if len(celulas[i]) > maior {
				maior = len(celulas[i])
			}
		}
		return celulas, float64(maior)*entrelinhaItemPreNotaPT + 5
	}
	// Segmento = parte da tabela numa página: cabeçalho em caixa + itens.
	// A borda externa e as divisões de coluna são desenhadas ao fechar.
	var inicioSegmento float64
	abrirSegmento := func(titulo string) {
		tituloSecao(x, y, W, titulo)
		y += alturaTituloSecaoPT
		inicioSegmento = y
		pdf.SetFillColor(232, 232, 232)
		traco(0.6, 0)
		pdf.RectFromUpperLeftWithStyle(x, y, W, alturaCabecalhoTabelaPT, "FD")
		fonte(true, 6.5)
		cx := x
		for _, col := range colunasProdutosPreNota {
			escrever(cx+paddingQuadroPT, y, col.Largura-2*paddingQuadroPT, alturaCabecalhoTabelaPT, col.Titulo, col.Align|gopdf.Middle)
			cx += col.Largura
		}
		y += alturaCabecalhoTabelaPT
	}
	fecharSegmento := func() {
		quadro(x, inicioSegmento, W, y-inicioSegmento)
		traco(0.6, 0)
		cx := x
		for _, col := range colunasProdutosPreNota[:len(colunasProdutosPreNota)-1] {
			cx += col.Largura
			pdf.Line(cx, inicioSegmento, cx, y)
		}
	}

	if len(doc.Rotas) == 0 {
		h := 26.0
		quadro(x, y, W, h)
		fonte(false, 9)
		escrever(x, y, W, h, textoSemItensPreNota, gopdf.Center|gopdf.Middle)
		y += h + espacoEntreRotasPT
	}

	// 2. Uma seção por rota.
	wParte := (W - espacoEntreQuadrosPT) / 2
	larguraCNPJ := 100.0
	for ri, rota := range doc.Rotas {
		// Medidas dos quadros Remetente/Destinatário: 3 faixas (Empresa;
		// CNPJ | Filial; Estoque), cada uma com a altura do lado mais alto.
		partes := []*partePreNota{&rota.Remetente, rota.Destinatario}
		hFaixa := [3]float64{}
		type faixas struct{ empresa, cnpj, filial, estoque []string }
		medidas := make([]faixas, 2)
		for i, p := range partes {
			if p == nil {
				continue
			}
			m := faixas{
				empresa: linhasCampo(p.Empresa, wParte, false),
				cnpj:    linhasCampo(p.CNPJ, larguraCNPJ, false),
				filial:  linhasCampo(p.Filial, wParte-larguraCNPJ, false),
				estoque: linhasCampo(p.Estoque, wParte, true),
			}
			medidas[i] = m
			hFaixa[0] = max(hFaixa[0], alturaCampo(len(m.empresa)))
			hFaixa[1] = max(hFaixa[1], alturaCampo(len(m.cnpj)), alturaCampo(len(m.filial)))
			hFaixa[2] = max(hFaixa[2], alturaCampo(len(m.estoque)))
		}
		hPartes := hFaixa[0] + hFaixa[1] + hFaixa[2]
		linhasSemDest := quebrar(rota.SemDestinatario, wParte-4*paddingQuadroPT, true, tamanhoValorPreNotaPT)
		hNatureza := alturaCampo(1)
		_, hPrimeiro := alturasLinhaProduto(rota.Produtos[0])

		// Remetente/Destinatário nunca ficam longe do início da tabela.
		if ri > 0 {
			y += espacoEntreRotasPT
		}
		garantir(alturaTituloSecaoPT + hPartes + espacoEntreQuadrosPT + hNatureza + espacoEntreQuadrosPT +
			alturaTituloSecaoPT + alturaCabecalhoTabelaPT + hPrimeiro)

		titulos := []string{"REMETENTE (ORIGEM)", "DESTINATÁRIO (DESTINO)"}
		for i, p := range partes {
			px := x + float64(i)*(wParte+espacoEntreQuadrosPT)
			tituloSecao(px, y, wParte, titulos[i])
			by := y + alturaTituloSecaoPT
			quadro(px, by, wParte, hPartes)
			if p == nil {
				fonte(true, tamanhoValorPreNotaPT)
				ly := by + (hPartes-float64(len(linhasSemDest))*entrelinhaPreNotaPT)/2
				for _, l := range linhasSemDest {
					escrever(px+2*paddingQuadroPT, ly, wParte-4*paddingQuadroPT, entrelinhaPreNotaPT, l, gopdf.Center|gopdf.Middle)
					ly += entrelinhaPreNotaPT
				}
				continue
			}
			m := medidas[i]
			traco(0.6, 0)
			pdf.Line(px, by+hFaixa[0], px+wParte, by+hFaixa[0])
			pdf.Line(px, by+hFaixa[0]+hFaixa[1], px+wParte, by+hFaixa[0]+hFaixa[1])
			pdf.Line(px+larguraCNPJ, by+hFaixa[0], px+larguraCNPJ, by+hFaixa[0]+hFaixa[1])
			desenharCampo(px, by, wParte, "EMPRESA", m.empresa, false)
			desenharCampo(px, by+hFaixa[0], larguraCNPJ, "CNPJ", m.cnpj, false)
			desenharCampo(px+larguraCNPJ, by+hFaixa[0], wParte-larguraCNPJ, "FILIAL", m.filial, false)
			desenharCampo(px, by+hFaixa[0]+hFaixa[1], wParte, "ESTOQUE", m.estoque, true)
		}
		y += alturaTituloSecaoPT + hPartes + espacoEntreQuadrosPT

		quadro(x, y, W, hNatureza)
		desenharCampo(x, y, W, "NATUREZA DA OPERAÇÃO", []string{rota.Natureza}, false)
		y += hNatureza + espacoEntreQuadrosPT

		// Na página seguinte o título da tabela lembra de qual rota ela é.
		continuacao := "DADOS DOS PRODUTOS (continuação: " + rota.Remetente.Estoque
		if rota.Destinatario != nil {
			continuacao += " para " + rota.Destinatario.Estoque + ")"
		} else {
			continuacao += ", " + strings.ToLower(rota.Natureza) + ")"
		}
		abrirSegmento("DADOS DOS PRODUTOS")
		for _, p := range rota.Produtos {
			celulas, h := alturasLinhaProduto(p)
			if y+h > limiteYPreNotaPT {
				fecharSegmento()
				novaPagina()
				abrirSegmento(continuacao)
			}
			fonte(false, tamanhoItemPreNotaPT)
			cx := x
			for i, col := range colunasProdutosPreNota {
				ly := y + 2.5
				for _, l := range celulas[i] {
					escrever(cx+paddingQuadroPT, ly, col.Largura-2*paddingQuadroPT, entrelinhaItemPreNotaPT, l, col.Align|gopdf.Middle)
					ly += entrelinhaItemPreNotaPT
				}
				cx += col.Largura
			}
			y += h
			traco(0.3, 150)
			pdf.Line(x, y, x+W, y)
		}
		// Subtotal da rota, colado no fim da tabela.
		hSub := 15.0
		if y+hSub > limiteYPreNotaPT {
			fecharSegmento()
			novaPagina()
			abrirSegmento(continuacao)
		}
		fecharSegmento()
		wTotal := colunasProdutosPreNota[len(colunasProdutosPreNota)-1].Largura
		quadro(x, y, W, hSub)
		traco(0.6, 0)
		pdf.Line(x+W-wTotal, y, x+W-wTotal, y+hSub)
		fonte(true, 7)
		escrever(x, y, W-wTotal-paddingQuadroPT, hSub, "SUBTOTAL DA ROTA", gopdf.Right|gopdf.Middle)
		fonte(true, tamanhoItemPreNotaPT)
		escrever(x+W-wTotal+paddingQuadroPT, y, wTotal-2*paddingQuadroPT, hSub, rota.Subtotal, gopdf.Right|gopdf.Middle)
		y += hSub
	}

	// 3. CÁLCULO DO TOTAL.
	if len(doc.Rotas) > 0 {
		y += espacoEntreRotasPT
		h := alturaCampo(1)
		garantir(alturaTituloSecaoPT + h)
		tituloSecao(x, y, W, "CÁLCULO DO TOTAL")
		y += alturaTituloSecaoPT
		quadro(x, y, W, h)
		desenharCampo(x, y, W, "VALOR TOTAL DOS PRODUTOS", nil, false)
		fonte(true, 10)
		escrever(x+paddingQuadroPT, y+paddingQuadroPT+tamanhoRotuloPreNotaPT+2, W-2*paddingQuadroPT, entrelinhaPreNotaPT,
			doc.ValorTotal, gopdf.Right|gopdf.Middle)
		y += h + espacoEntreRotasPT
	}

	// 4. DADOS ADICIONAIS: Aprovador e Solicitante(s).
	{
		wAprov := 180.0
		linhasAprov := linhasCampo(doc.Aprovador, wAprov, false)
		linhasSolic := linhasCampo(doc.Solicitantes, W-wAprov, false)
		h := alturaCampo(max(len(linhasAprov), len(linhasSolic)))
		garantir(alturaTituloSecaoPT + h)
		tituloSecao(x, y, W, "DADOS ADICIONAIS")
		y += alturaTituloSecaoPT
		quadro(x, y, W, h)
		traco(0.6, 0)
		pdf.Line(x+wAprov, y, x+wAprov, y+h)
		desenharCampo(x, y, wAprov, "APROVADOR", linhasAprov, false)
		desenharCampo(x+wAprov, y, W-wAprov, "SOLICITANTE(S)", linhasSolic, false)
	}

	if erro != nil {
		return nil, erro
	}
	bytes, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		return nil, fmt.Errorf("falha ao gerar bytes do PDF da pré-nota: %w", err)
	}
	return bytes, nil
}

// GerarPreNotaPDF compõe MontarPreNotaConteudo + RenderizarPreNotaPDF — é o
// que BaixarPreNotaPDFHandler chama. Devolve também o `docnum` (nome do
// arquivo no Content-Disposition).
func GerarPreNotaPDF(db *sql.DB, empresaID, preNotaID string) ([]byte, int, error) {
	conteudo, err := MontarPreNotaConteudo(db, empresaID, preNotaID)
	if err != nil {
		return nil, 0, err
	}
	pdf, err := RenderizarPreNotaPDF(conteudo)
	if err != nil {
		return nil, 0, err
	}
	return pdf, conteudo.Docnum, nil
}
