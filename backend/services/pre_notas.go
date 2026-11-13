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
	"golang.org/x/image/font/gofont/goregular"
)

// --- Documento impresso da Pré-Nota — Story 20.5 (Epic 20), spec-20-5 -------
//
// Molde do recibo de Pedido (Story 7.6, pedidos.go): MontarPreNotaConteudo
// (busca, sem gopdf) / blocosPreNota + linhasPreNota (texto do documento,
// funções puras, testáveis sem ler bytes de PDF) / RenderizarPreNotaPDF (só
// desenha, determinística) / GerarPreNotaPDF (tudo em sequência — o que
// BaixarPreNotaPDFHandler chama).
//
// O que é congelado (Spec Change Log de spec-20-5, decisão do usuário): SÓ
// preço e unidade, gravados em `pedido_itens` na aprovação em lote. Código do
// produto, nomes das Filiais, razão social/CNPJ e nome do Aprovador vêm do
// cadastro atual no momento do download (mesmo critério do recibo de
// Pedido) — renomear algum deles muda o próximo download. A renderização em
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
	RodapePreNota          = "Documento interno de controle — não substitui a Nota Fiscal"
	textoSemItensPreNota   = "Nenhum item movimentado nesta Pré-Nota"
	avisoTotalSemValor     = " (há itens sem valor cadastrado)"
	fusoHorarioPreNota     = "America/Sao_Paulo"
	alturaRodapePreNotaPT  = 20.0
	tamanhoRodapePreNotaPT = 8.0
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
		       pi.unidade_medida::text, (pi.preco_unitario * 100)::bigint, fo.nome, fd.nome
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
		var codigo, unidade, filialOrigem, filialDestino sql.NullString
		var preco sql.NullInt64
		if err := itensRows.Scan(
			&it.Tipo, &codigo, &it.Descricao, &it.QuantidadeMilesimos, &unidade, &preco, &filialOrigem, &filialDestino,
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
		if it.Tipo == "transferencia" && filialDestino.Valid {
			it.FilialDestino = filialDestino.String
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

// --- Texto do documento (funções puras) -----------------------------------

type linhaPreNota struct {
	Texto   string
	Tamanho float64
}

// blocoPreNota é um grupo de linhas que NUNCA é dividido entre duas páginas
// (a não ser que sozinho não caiba numa página inteira).
type blocoPreNota struct {
	Linhas       []linhaPreNota
	EspacoDepois float64
}

// blocosPreNota monta o texto do documento em blocos: cabeçalho; "Itens"
// junto do 1º item (o título nunca fica sozinho no fim da página); um bloco
// por item; total. Sem itens: só a mensagem de "nenhum item". O rodapé não
// entra aqui — é desenhado em toda página (ver RenderizarPreNotaPDF).
func blocosPreNota(c PreNotaConteudo) []blocoPreNota {
	solicitantes := textoAusentePreNota
	if len(c.Solicitantes) > 0 {
		solicitantes = strings.Join(c.Solicitantes, ", ")
	}
	blocos := []blocoPreNota{{
		Linhas: []linhaPreNota{
			{fmt.Sprintf("Pré-Nota nº %d", c.Docnum), 18},
			{"Empresa: " + textoOuAusentePreNota(c.EmpresaRazaoSocial), 11},
			{"CNPJ: " + textoOuAusentePreNota(formatarCNPJPreNota(c.EmpresaCNPJ)), 11},
			{"Data: " + formatarDataPreNota(c.CriadoEm), 11},
			{"Aprovador: " + textoOuAusentePreNota(c.Aprovador), 11},
			{"Solicitante(s): " + solicitantes, 11},
		},
		EspacoDepois: 12,
	}}

	if len(c.Itens) == 0 {
		return append(blocos, blocoPreNota{Linhas: []linhaPreNota{{textoSemItensPreNota, 11}}})
	}

	total := new(big.Int)
	algumSemValor := false
	for i, it := range c.Itens {
		valorLinha := textoAusentePreNota
		if it.PrecoCentavos != nil {
			v := valorLinhaCentavosPreNota(it.QuantidadeMilesimos, *it.PrecoCentavos)
			total.Add(total, v)
			valorLinha = formatarCentavosPreNota(v)
		} else {
			algumSemValor = true
		}
		unidade := textoAusentePreNota
		if it.UnidadeMedida != nil {
			unidade = textoOuAusentePreNota(*it.UnidadeMedida)
		}
		filiais := "Filial origem: " + textoOuAusentePreNota(it.FilialOrigem)
		if it.Tipo == "transferencia" {
			filiais += "   Filial destino: " + textoOuAusentePreNota(it.FilialDestino)
		}

		var linhas []linhaPreNota
		if i == 0 {
			linhas = append(linhas, linhaPreNota{"Itens", 13})
		}
		linhas = append(linhas,
			linhaPreNota{fmt.Sprintf("%d. Produto: %s   Descrição: %s   (%s)",
				i+1, textoOuAusentePreNota(it.ProdutoCodigo), it.Descricao, rotuloTipoPreNota(it.Tipo)), 10},
			linhaPreNota{"Quantidade: " + formatarQuantidadePreNota(it.QuantidadeMilesimos) +
				"   Unidade: " + unidade +
				"   Valor unitário: " + formatarPrecoPreNota(it.PrecoCentavos) +
				"   Valor da linha: " + valorLinha, 10},
			linhaPreNota{filiais, 10},
		)
		blocos = append(blocos, blocoPreNota{Linhas: linhas, EspacoDepois: 6})
	}

	textoTotal := "Total: " + formatarCentavosPreNota(total)
	if algumSemValor {
		textoTotal += avisoTotalSemValor
	}
	return append(blocos, blocoPreNota{Linhas: []linhaPreNota{{textoTotal, 12}}})
}

// linhasPreNota devolve o texto completo do documento, na ordem impressa,
// com o rodapé ao final — usado pelos testes para assertar o conteúdo sem
// ler bytes de PDF.
func linhasPreNota(c PreNotaConteudo) []string {
	var out []string
	for _, b := range blocosPreNota(c) {
		for _, l := range b.Linhas {
			out = append(out, l.Texto)
		}
	}
	return append(out, RodapePreNota)
}

// --- Renderização ----------------------------------------------------------

// RenderizarPreNotaPDF desenha a Pré-Nota a partir de um PreNotaConteudo JÁ
// MONTADO — função pura e determinística (mesma regra de
// RenderizarReciboPedidoPDF: sem `time.Now()`, sem `pdf.SetInfo`).
//
// Cada linha é quebrada na largura útil (SplitTextWithWordWrap) — textos
// longos nunca são cortados nem saem da página. Antes de escrever um bloco,
// mede a altura dele: se não cabe no resto da página, começa uma nova (um
// item nunca fica dividido entre duas páginas). O rodapé é desenhado em toda
// página via AddFooter.
func RenderizarPreNotaPDF(conteudo PreNotaConteudo) ([]byte, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.SetMargins(margemReciboPT, margemReciboPT, margemReciboPT, margemReciboPT)
	if err := pdf.AddTTFFontData("gofont", goregular.TTF); err != nil {
		return nil, fmt.Errorf("falha ao carregar fonte da pré-nota: %w", err)
	}

	var erroRodape error
	pdf.AddFooter(func() {
		if err := pdf.SetFont("gofont", "", tamanhoRodapePreNotaPT); err != nil {
			erroRodape = err
			return
		}
		pdf.SetXY(margemReciboPT, alturaPaginaReciboPT-margemReciboPT-tamanhoRodapePreNotaPT-4)
		if err := pdf.Cell(&gopdf.Rect{W: larguraUtilReciboPT, H: tamanhoRodapePreNotaPT + 4}, RodapePreNota); err != nil {
			erroRodape = err
		}
	})
	pdf.AddPage()

	limiteY := alturaPaginaReciboPT - margemReciboPT - alturaRodapePreNotaPT
	topoY := margemReciboPT

	quebrar := func(l linhaPreNota) ([]string, error) {
		if err := pdf.SetFont("gofont", "", l.Tamanho); err != nil {
			return nil, fmt.Errorf("falha ao selecionar fonte da pré-nota: %w", err)
		}
		if l.Texto == "" {
			return []string{""}, nil
		}
		partes, err := pdf.SplitTextWithWordWrap(l.Texto, larguraUtilReciboPT)
		if err != nil {
			return nil, fmt.Errorf("falha ao quebrar linha da pré-nota: %w", err)
		}
		if len(partes) == 0 {
			partes = []string{""}
		}
		return partes, nil
	}

	for _, bloco := range blocosPreNota(conteudo) {
		quebradas := make([][]string, len(bloco.Linhas))
		alturaBloco := bloco.EspacoDepois
		for i, l := range bloco.Linhas {
			partes, err := quebrar(l)
			if err != nil {
				return nil, err
			}
			quebradas[i] = partes
			alturaBloco += float64(len(partes)) * (l.Tamanho + 6)
		}
		if pdf.GetY()+alturaBloco > limiteY && pdf.GetY() > topoY {
			pdf.AddPage()
		}
		for i, l := range bloco.Linhas {
			if err := pdf.SetFont("gofont", "", l.Tamanho); err != nil {
				return nil, fmt.Errorf("falha ao selecionar fonte da pré-nota: %w", err)
			}
			altura := l.Tamanho + 6
			for _, parte := range quebradas[i] {
				// Checagem por linha: só dispara se um bloco sozinho for
				// maior que uma página inteira.
				if pdf.GetY()+altura > limiteY && pdf.GetY() > topoY {
					pdf.AddPage()
					if err := pdf.SetFont("gofont", "", l.Tamanho); err != nil {
						return nil, fmt.Errorf("falha ao selecionar fonte da pré-nota: %w", err)
					}
				}
				if err := pdf.Cell(&gopdf.Rect{W: larguraUtilReciboPT, H: altura}, parte); err != nil {
					return nil, fmt.Errorf("falha ao escrever linha da pré-nota: %w", err)
				}
				pdf.Br(altura)
			}
		}
		pdf.Br(bloco.EspacoDepois)
	}
	if erroRodape != nil {
		return nil, fmt.Errorf("falha ao escrever rodapé da pré-nota: %w", erroRodape)
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
