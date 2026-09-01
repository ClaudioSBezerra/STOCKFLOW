package services

import (
	"database/sql"
	"fmt"

	"github.com/xuri/excelize/v2"
)

// Exportação da tabela do Catálogo para Excel — Story 4.6, spec-4-6, FR-30.
// GerarCatalogoXLSX é o único ponto de entrada deste arquivo: monta um
// `.xlsx` real (via qax-os/excelize, `github.com/xuri/excelize/v2`) refletindo
// exatamente o filtro ativo, com subtotais dinâmicos por grupo (fórmula
// `SUBTOTAL`, nunca soma estática) para permanecer correto mesmo quando o
// próprio Excel filtra o arquivo já exportado.

// cabecalhoCatalogoXLSX é o cabeçalho fixo de 13 colunas do `.xlsx`
// exportado, nesta ordem exata — mesmos rótulos de dimensão de
// CabecalhoEsperado (Story 3.3, services/importacoes.go, `Comprimento
// (valor)`, `Comprimento (unidade)`, etc.), vocabulário único de colunas
// entre import/export (Design Notes). Sem `Código`/`Categoria`: a tabela
// agrupada (Story 4.3) já omite os dois por design (um grupo pode juntar
// Produtos de código/categoria diferentes) — exportar teria que inventar um
// valor arbitrário. `Estoque`/`Quantidade` entram porque são a discriminação
// que a tabela expande em tela.
var cabecalhoCatalogoXLSX = []string{
	"Nome",
	"Comprimento (valor)", "Comprimento (unidade)",
	"Largura (valor)", "Largura (unidade)",
	"Diâmetro (valor)", "Diâmetro (unidade)",
	"Altura (valor)", "Altura (unidade)",
	"Espessura (valor)", "Espessura (unidade)",
	"Estoque", "Quantidade",
}

// colEstoqueXLSX/colQuantidadeXLSX são os números de coluna (1-based, ordem
// de cabecalhoCatalogoXLSX) usados fora do laço de dimensões abaixo.
const (
	colEstoqueXLSX    = 12
	colQuantidadeXLSX = 13
)

// GerarCatalogoXLSX gera o `.xlsx` de exportação da tabela agrupada do
// Catálogo com os `filtros` ativos (Story 4.6, spec-4-6, FR-30):
// ListarTodosGruposCatalogo (SEM paginação — sempre o conjunto filtrado
// completo, nunca uma página) devolve os grupos; para cada grupo, 1 linha por
// item de `PorEstoque` (Nome+dimensões repetidos, `Estoque`=nome,
// `Quantidade`=valor; `PorEstoque` vazio -> 1 linha com `Estoque` vazio e
// `Quantidade` 0, nunca omite o grupo) seguida de 1 linha "Subtotal — {nome}"
// com fórmula `SUBTOTAL(9,<intervalo Quantidade do grupo>)`. Ao final, só
// quando há >=1 grupo, 1 linha "Total geral" com `SUBTOTAL(9,<intervalo
// Quantidade completo, incluindo os subtotais>)` — o `SUBTOTAL` do Excel
// ignora nativamente subtotais aninhados no mesmo intervalo, então o total
// geral nunca soma em dobro. Zero grupos -> só a linha de cabeçalho, nenhuma
// linha de dado nem de total. `AutoFilter` na linha de cabeçalho, para que o
// filtro em Excel permaneça coerente com o `SUBTOTAL`.
func GerarCatalogoXLSX(db *sql.DB, filtros FiltrosCatalogo) ([]byte, error) {
	grupos, err := ListarTodosGruposCatalogo(db, filtros)
	if err != nil {
		return nil, err
	}

	f := excelize.NewFile()
	defer f.Close()
	planilha := f.GetSheetList()[0]

	for col, titulo := range cabecalhoCatalogoXLSX {
		if err := setStrXLSX(f, planilha, col+1, 1, titulo); err != nil {
			return nil, err
		}
	}

	linha := 2
	primeiraLinhaDados := linha
	for _, grupo := range grupos {
		inicioGrupo := linha

		porEstoque := grupo.PorEstoque
		if len(porEstoque) == 0 {
			porEstoque = []EstoqueQuantidade{{}}
		}
		for _, eq := range porEstoque {
			if err := escreverLinhaCatalogoXLSX(f, planilha, linha, grupo, eq); err != nil {
				return nil, err
			}
			linha++
		}
		fimGrupo := linha - 1

		if err := setStrXLSX(f, planilha, 1, linha, "Subtotal — "+grupo.Nome); err != nil {
			return nil, err
		}
		if err := escreverFormulaSubtotalXLSX(f, planilha, linha, inicioGrupo, fimGrupo); err != nil {
			return nil, err
		}
		linha++
	}

	if len(grupos) > 0 {
		if err := setStrXLSX(f, planilha, 1, linha, "Total geral"); err != nil {
			return nil, err
		}
		if err := escreverFormulaSubtotalXLSX(f, planilha, linha, primeiraLinhaDados, linha-1); err != nil {
			return nil, err
		}
		linha++
	}

	ultimaCelulaCabecalho, err := excelize.CoordinatesToCellName(len(cabecalhoCatalogoXLSX), 1)
	if err != nil {
		return nil, fmt.Errorf("falha ao montar intervalo do AutoFilter do catálogo: %w", err)
	}
	if err := f.AutoFilter(planilha, "A1:"+ultimaCelulaCabecalho, nil); err != nil {
		return nil, fmt.Errorf("falha ao aplicar AutoFilter ao catálogo: %w", err)
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("falha ao gerar bytes do .xlsx do catálogo: %w", err)
	}
	return buf.Bytes(), nil
}

// escreverLinhaCatalogoXLSX escreve uma linha de detalhe (Nome + 5 dimensões
// + Estoque + Quantidade) na `linha` informada, para a combinação grupo×`eq`.
// Uma dimensão `nil` deixa as 2 células (valor/unidade) em branco (mesma
// semântica de `null` na projeção JSON do Catálogo); `eq.EstoqueNome` vazio
// (grupo sem `produto_estoque`) deixa a célula `Estoque` em branco;
// `eq.Quantidade` é sempre escrita, mesmo `0`.
func escreverLinhaCatalogoXLSX(f *excelize.File, planilha string, linha int, grupo CatalogoGrupo, eq EstoqueQuantidade) error {
	if err := setStrXLSX(f, planilha, 1, linha, grupo.Nome); err != nil {
		return err
	}

	dimensoes := []*DimensaoValor{
		grupo.Dimensoes.Comprimento,
		grupo.Dimensoes.Largura,
		grupo.Dimensoes.Diametro,
		grupo.Dimensoes.Altura,
		grupo.Dimensoes.Espessura,
	}
	col := 2
	for _, d := range dimensoes {
		if d != nil {
			if err := setFloatXLSX(f, planilha, col, linha, d.Valor); err != nil {
				return err
			}
			if err := setStrXLSX(f, planilha, col+1, linha, d.Unidade); err != nil {
				return err
			}
		}
		col += 2
	}

	if err := setStrXLSX(f, planilha, colEstoqueXLSX, linha, eq.EstoqueNome); err != nil {
		return err
	}
	return setFloatXLSX(f, planilha, colQuantidadeXLSX, linha, eq.Quantidade)
}

// escreverFormulaSubtotalXLSX escreve, na coluna Quantidade da `linha`
// informada, a fórmula `SUBTOTAL(9,<coluna Quantidade de inicio até fim>)`
// (função 9 = SOMA, ignorando outros SUBTOTAL aninhados no mesmo intervalo —
// comportamento nativo do Excel, Design Notes).
func escreverFormulaSubtotalXLSX(f *excelize.File, planilha string, linha, inicio, fim int) error {
	celInicio, err := excelize.CoordinatesToCellName(colQuantidadeXLSX, inicio)
	if err != nil {
		return fmt.Errorf("falha ao montar intervalo SUBTOTAL do catálogo: %w", err)
	}
	celFim, err := excelize.CoordinatesToCellName(colQuantidadeXLSX, fim)
	if err != nil {
		return fmt.Errorf("falha ao montar intervalo SUBTOTAL do catálogo: %w", err)
	}
	celula, err := excelize.CoordinatesToCellName(colQuantidadeXLSX, linha)
	if err != nil {
		return fmt.Errorf("falha ao montar célula da fórmula SUBTOTAL do catálogo: %w", err)
	}
	formula := fmt.Sprintf("SUBTOTAL(9,%s:%s)", celInicio, celFim)
	if err := f.SetCellFormula(planilha, celula, formula); err != nil {
		return fmt.Errorf("falha ao escrever fórmula SUBTOTAL %s do catálogo: %w", celula, err)
	}
	return nil
}

// setStrXLSX/setFloatXLSX resolvem a célula (1-based col/linha) via
// excelize.CoordinatesToCellName antes de escrever — evitam repetir a
// tradução coordenada->célula e a checagem de erro em cada chamada acima.
// setStrXLSX não escreve nada para `valor == ""` (célula em branco de
// propósito, ex. dimensão ausente/Estoque vazio), mesmo padrão de omissão
// usado pela serialização JSON do Catálogo (`nil` -> `null`, nunca `""`).
func setStrXLSX(f *excelize.File, planilha string, col, linha int, valor string) error {
	if valor == "" {
		return nil
	}
	celula, err := excelize.CoordinatesToCellName(col, linha)
	if err != nil {
		return fmt.Errorf("falha ao montar célula (%d,%d) do catálogo: %w", col, linha, err)
	}
	if err := f.SetCellStr(planilha, celula, valor); err != nil {
		return fmt.Errorf("falha ao escrever célula %s do catálogo: %w", celula, err)
	}
	return nil
}

func setFloatXLSX(f *excelize.File, planilha string, col, linha int, valor float64) error {
	celula, err := excelize.CoordinatesToCellName(col, linha)
	if err != nil {
		return fmt.Errorf("falha ao montar célula (%d,%d) do catálogo: %w", col, linha, err)
	}
	if err := f.SetCellFloat(planilha, celula, valor, -1, 64); err != nil {
		return fmt.Errorf("falha ao escrever célula %s do catálogo: %w", celula, err)
	}
	return nil
}
