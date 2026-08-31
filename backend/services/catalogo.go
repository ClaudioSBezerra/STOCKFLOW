package services

import (
	"database/sql"
	"fmt"
	"sort"

	"github.com/lib/pq"
)

// TamanhoPaginaCatalogo é o tamanho fixo de página do Catálogo (Story 4.3,
// spec-4-3) — decisão desta spec, NÃO é parâmetro de query. `agrupar=false`
// pagina sobre Produtos, `agrupar=true` sobre grupos; nos dois casos a
// contagem e o OFFSET/LIMIT operam na mesma unidade que as linhas
// retornadas, para que a soma de um grupo nunca fique partida entre páginas.
const TamanhoPaginaCatalogo = 24

// MaxPaginaCatalogo é o teto de `pagina` aceito pelo handler do Catálogo.
// Sem ele, um inteiro absurdo (ex. "400000000000000000") passa por
// strconv.Atoi e estoura `(pagina-1)*TamanhoPaginaCatalogo` no cálculo do
// OFFSET, virando um OFFSET negativo e um erro do Postgres (500) em cima de
// input de cliente. O teto é folgado demais para qualquer catálogo real
// (>=24 milhões de linhas/grupos) e mantém a mensagem "página inválida".
const MaxPaginaCatalogo = 1_000_000

// DimensaoValor é o par valor+unidade de uma dimensão física do Produto na
// projeção do Catálogo (AD-9: nunca texto livre). Um par com `valor` e
// `unidade` ambos NULL no banco vira `nil` (JSON `null`).
type DimensaoValor struct {
	Valor   float64 `json:"valor"`
	Unidade string  `json:"unidade"`
}

// DimensoesProduto agrupa as 5 dimensões estruturadas de um Produto/grupo do
// Catálogo. Cada ponteiro `nil` serializa como `null`.
type DimensoesProduto struct {
	Comprimento *DimensaoValor `json:"comprimento"`
	Largura     *DimensaoValor `json:"largura"`
	Diametro    *DimensaoValor `json:"diametro"`
	Altura      *DimensaoValor `json:"altura"`
	Espessura   *DimensaoValor `json:"espessura"`
}

// CatalogoItem é uma linha da grade (`agrupar=false`): um Produto com a
// quantidade total somada de todas as suas linhas `produto_estoque` e o
// indicador de disponibilidade derivado dela.
type CatalogoItem struct {
	ID              string           `json:"id"`
	Nome            string           `json:"nome"`
	Codigo          *string          `json:"codigo"`
	Categoria       Categoria        `json:"categoria"`
	Dimensoes       DimensoesProduto `json:"dimensoes"`
	QuantidadeTotal float64          `json:"quantidadeTotal"`
	Disponivel      bool             `json:"disponivel"`
}

// EstoqueQuantidade é a discriminação da quantidade de um grupo por Estoque
// (usada na expansão da linha da tabela agrupada).
type EstoqueQuantidade struct {
	EstoqueID   string  `json:"estoqueId"`
	EstoqueNome string  `json:"estoqueNome"`
	Quantidade  float64 `json:"quantidade"`
}

// CatalogoGrupo é uma linha da tabela agrupada (`agrupar=true`): todos os
// Produtos com o mesmo `nome` e as mesmas 5 dimensões estruturadas
// colapsados numa linha, com as quantidades somadas e a discriminação por
// Estoque embutida para a expansão.
type CatalogoGrupo struct {
	Chave           string              `json:"chave"`
	Nome            string              `json:"nome"`
	Dimensoes       DimensoesProduto    `json:"dimensoes"`
	QuantidadeTotal float64             `json:"quantidadeTotal"`
	Disponivel      bool                `json:"disponivel"`
	PorEstoque      []EstoqueQuantidade `json:"porEstoque"`
}

// Paginacao é o bloco de paginação numérica devolvido pelos dois modos do
// Catálogo. `Tamanho` é sempre TamanhoPaginaCatalogo; `Total` conta na
// unidade paginada (Produtos ou grupos). Página além da última devolve lista
// vazia mas `Total`/`TotalPaginas` continuam corretos.
type Paginacao struct {
	Pagina       int `json:"pagina"`
	Tamanho      int `json:"tamanho"`
	Total        int `json:"total"`
	TotalPaginas int `json:"totalPaginas"`
}

// novaPaginacao monta o bloco de paginação a partir da página pedida (já
// validada como >=1 pelo handler) e do total na unidade certa.
func novaPaginacao(pagina, total int) Paginacao {
	totalPaginas := 0
	if total > 0 {
		totalPaginas = (total + TamanhoPaginaCatalogo - 1) / TamanhoPaginaCatalogo
	}
	return Paginacao{
		Pagina:       pagina,
		Tamanho:      TamanhoPaginaCatalogo,
		Total:        total,
		TotalPaginas: totalPaginas,
	}
}

// parDimensao guarda o par cru (valor, unidade) lido do banco para uma
// dimensão antes de virar *DimensaoValor.
type parDimensao struct {
	valor   sql.NullFloat64
	unidade sql.NullString
}

// paraDimensao converte o par cru: os dois lados preenchidos -> *DimensaoValor;
// qualquer lado NULL -> nil (a constraint de CriarProduto/validarDimensao
// garante que na prática é sempre "os dois" ou "nenhum").
func (p parDimensao) paraDimensao() *DimensaoValor {
	if !p.valor.Valid || !p.unidade.Valid {
		return nil
	}
	return &DimensaoValor{Valor: p.valor.Float64, Unidade: p.unidade.String}
}

// catalogoGradeQuery devolve uma página de Produtos (Story 4.3, `agrupar=false`):
// um Produto por linha, `quantidadeTotal` = SUM(produto_estoque.quantidade)
// do Produto (0 quando não há nenhuma linha, via LEFT JOIN + COALESCE),
// ordenado por `nome ASC, id ASC`. Sem índice novo: ~8.000 linhas com um
// LEFT JOIN a um agregado é volume trivial para o Postgres (Design Notes).
const catalogoGradeQuery = `
	SELECT
		p.id, p.nome, p.codigo,
		c.id, c.codigo, c.nome,
		p.comprimento_valor, p.comprimento_unidade,
		p.largura_valor, p.largura_unidade,
		p.diametro_valor, p.diametro_unidade,
		p.altura_valor, p.altura_unidade,
		p.espessura_valor, p.espessura_unidade,
		COALESCE(pe.total, 0) AS quantidade_total
	FROM produtos p
	JOIN categorias c ON c.id = p.categoria_id
	LEFT JOIN (
		SELECT produto_id, SUM(quantidade) AS total
		FROM produto_estoque
		GROUP BY produto_id
	) pe ON pe.produto_id = p.id
	ORDER BY p.nome ASC, p.id ASC
	LIMIT $1 OFFSET $2`

// ListarCatalogoGrade devolve a página `pagina` da grade do Catálogo e o
// bloco de paginação (contagem sobre `produtos`). `pagina` é assumido >=1
// (validado pelo handler). Página além da última -> slice vazio (nunca
// `nil`), paginação ainda com `total`/`totalPaginas` corretos.
func ListarCatalogoGrade(db *sql.DB, pagina int) ([]CatalogoItem, Paginacao, error) {
	if pagina < 1 {
		pagina = 1
	}

	var total int
	if err := db.QueryRow(`SELECT count(*) FROM produtos`).Scan(&total); err != nil {
		return nil, Paginacao{}, fmt.Errorf("falha ao contar produtos do catálogo: %w", err)
	}
	paginacao := novaPaginacao(pagina, total)

	rows, err := db.Query(catalogoGradeQuery, TamanhoPaginaCatalogo, (pagina-1)*TamanhoPaginaCatalogo)
	if err != nil {
		return nil, Paginacao{}, fmt.Errorf("falha ao listar grade do catálogo: %w", err)
	}
	defer rows.Close()

	itens := make([]CatalogoItem, 0)
	for rows.Next() {
		var (
			it                         CatalogoItem
			codigo                     sql.NullString
			comp, larg, diam, alt, esp parDimensao
			quantidade                 float64
		)
		if err := rows.Scan(
			&it.ID, &it.Nome, &codigo,
			&it.Categoria.ID, &it.Categoria.Codigo, &it.Categoria.Nome,
			&comp.valor, &comp.unidade,
			&larg.valor, &larg.unidade,
			&diam.valor, &diam.unidade,
			&alt.valor, &alt.unidade,
			&esp.valor, &esp.unidade,
			&quantidade,
		); err != nil {
			return nil, Paginacao{}, fmt.Errorf("falha ao ler linha da grade do catálogo: %w", err)
		}
		if codigo.Valid {
			c := codigo.String
			it.Codigo = &c
		}
		it.Dimensoes = DimensoesProduto{
			Comprimento: comp.paraDimensao(),
			Largura:     larg.paraDimensao(),
			Diametro:    diam.paraDimensao(),
			Altura:      alt.paraDimensao(),
			Espessura:   esp.paraDimensao(),
		}
		it.QuantidadeTotal = quantidade
		it.Disponivel = quantidade > 0
		itens = append(itens, it)
	}
	if err := rows.Err(); err != nil {
		return nil, Paginacao{}, fmt.Errorf("falha ao iterar grade do catálogo: %w", err)
	}
	return itens, paginacao, nil
}

// colunasChaveGrupo são as 11 colunas que definem um grupo da tabela
// agrupada: `nome` + o par (valor, unidade) das 5 dimensões estruturadas.
// `NULL` agrupa com `NULL` (semântica de `GROUP BY` do Postgres).
const colunasChaveGrupo = `nome,
	comprimento_valor, comprimento_unidade,
	largura_valor, largura_unidade,
	diametro_valor, diametro_unidade,
	altura_valor, altura_unidade,
	espessura_valor, espessura_unidade`

// catalogoGrupoCountQuery conta GRUPOS (não Produtos) — a unidade sobre a
// qual `agrupar=true` pagina.
const catalogoGrupoCountQuery = `
	SELECT count(*) FROM (
		SELECT 1 FROM produtos GROUP BY ` + colunasChaveGrupo + `
	) t`

// catalogoGrupoQuery devolve uma página de grupos. `chave` = md5 estável da
// concatenação de `nome` + os 10 valores de dimensão (delimitados por chr(31)
// para que um valor contendo o delimitador não colida com outro grupo),
// serve de `key` no React. `quantidade_total` = soma de TODAS as linhas
// `produto_estoque` de TODOS os Produtos do grupo (LEFT JOIN + COALESCE).
// `produto_ids` = ids de Produto do grupo, para resolver o `porEstoque` numa
// segunda query com `WHERE pe.produto_id = ANY(...)`. Ordena por `nome`,
// depois pelas colunas de dimensão de forma determinística, depois `chave`.
const catalogoGrupoQuery = `
	SELECT
		md5(
			coalesce(p.nome, '') || chr(31) ||
			coalesce(p.comprimento_valor::text, '') || chr(31) || coalesce(p.comprimento_unidade::text, '') || chr(31) ||
			coalesce(p.largura_valor::text, '') || chr(31) || coalesce(p.largura_unidade::text, '') || chr(31) ||
			coalesce(p.diametro_valor::text, '') || chr(31) || coalesce(p.diametro_unidade::text, '') || chr(31) ||
			coalesce(p.altura_valor::text, '') || chr(31) || coalesce(p.altura_unidade::text, '') || chr(31) ||
			coalesce(p.espessura_valor::text, '') || chr(31) || coalesce(p.espessura_unidade::text, '')
		) AS chave,
		p.nome,
		p.comprimento_valor, p.comprimento_unidade,
		p.largura_valor, p.largura_unidade,
		p.diametro_valor, p.diametro_unidade,
		p.altura_valor, p.altura_unidade,
		p.espessura_valor, p.espessura_unidade,
		COALESCE(SUM(pe.quantidade), 0) AS quantidade_total,
		array_agg(DISTINCT p.id::text) AS produto_ids
	FROM produtos p
	LEFT JOIN produto_estoque pe ON pe.produto_id = p.id
	GROUP BY
		p.nome,
		p.comprimento_valor, p.comprimento_unidade,
		p.largura_valor, p.largura_unidade,
		p.diametro_valor, p.diametro_unidade,
		p.altura_valor, p.altura_unidade,
		p.espessura_valor, p.espessura_unidade
	ORDER BY
		p.nome ASC,
		p.comprimento_valor ASC NULLS FIRST, p.comprimento_unidade ASC NULLS FIRST,
		p.largura_valor ASC NULLS FIRST, p.largura_unidade ASC NULLS FIRST,
		p.diametro_valor ASC NULLS FIRST, p.diametro_unidade ASC NULLS FIRST,
		p.altura_valor ASC NULLS FIRST, p.altura_unidade ASC NULLS FIRST,
		p.espessura_valor ASC NULLS FIRST, p.espessura_unidade ASC NULLS FIRST,
		chave ASC
	LIMIT $1 OFFSET $2`

// catalogoPorEstoqueQuery devolve, para os Produtos da página de grupos, a
// quantidade por Estoque — só Estoques onde o Produto tem linha
// `produto_estoque`. A agregação por grupo (somar Produtos do mesmo grupo,
// ordenar por `estoqueNome`) é feita em Go.
const catalogoPorEstoqueQuery = `
	SELECT pe.produto_id, e.id, e.nome, pe.quantidade
	FROM produto_estoque pe
	JOIN estoques e ON e.id = pe.estoque_id
	WHERE pe.produto_id = ANY($1)`

// ListarCatalogoAgrupado devolve a página `pagina` da tabela agrupada do
// Catálogo e o bloco de paginação (contagem sobre GRUPOS). Um grupo =
// Produtos com o mesmo `nome` e as mesmas 5 dimensões estruturadas.
// `pagina` é assumido >=1 (validado pelo handler). Página além da última ->
// slice vazio (nunca `nil`). `porEstoque` de um grupo é sempre não-`nil`
// (pode ser `[]`).
func ListarCatalogoAgrupado(db *sql.DB, pagina int) ([]CatalogoGrupo, Paginacao, error) {
	if pagina < 1 {
		pagina = 1
	}

	var total int
	if err := db.QueryRow(catalogoGrupoCountQuery).Scan(&total); err != nil {
		return nil, Paginacao{}, fmt.Errorf("falha ao contar grupos do catálogo: %w", err)
	}
	paginacao := novaPaginacao(pagina, total)

	rows, err := db.Query(catalogoGrupoQuery, TamanhoPaginaCatalogo, (pagina-1)*TamanhoPaginaCatalogo)
	if err != nil {
		return nil, Paginacao{}, fmt.Errorf("falha ao listar tabela agrupada do catálogo: %w", err)
	}
	defer rows.Close()

	grupos := make([]CatalogoGrupo, 0)
	produtoParaGrupo := make(map[string]int)
	var todosIDs []string
	for rows.Next() {
		var (
			g                          CatalogoGrupo
			comp, larg, diam, alt, esp parDimensao
			quantidade                 float64
			ids                        []string
		)
		if err := rows.Scan(
			&g.Chave, &g.Nome,
			&comp.valor, &comp.unidade,
			&larg.valor, &larg.unidade,
			&diam.valor, &diam.unidade,
			&alt.valor, &alt.unidade,
			&esp.valor, &esp.unidade,
			&quantidade,
			pq.Array(&ids),
		); err != nil {
			return nil, Paginacao{}, fmt.Errorf("falha ao ler linha da tabela agrupada do catálogo: %w", err)
		}
		g.Dimensoes = DimensoesProduto{
			Comprimento: comp.paraDimensao(),
			Largura:     larg.paraDimensao(),
			Diametro:    diam.paraDimensao(),
			Altura:      alt.paraDimensao(),
			Espessura:   esp.paraDimensao(),
		}
		g.QuantidadeTotal = quantidade
		g.Disponivel = quantidade > 0
		g.PorEstoque = make([]EstoqueQuantidade, 0)

		idx := len(grupos)
		grupos = append(grupos, g)
		for _, id := range ids {
			produtoParaGrupo[id] = idx
			todosIDs = append(todosIDs, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, Paginacao{}, fmt.Errorf("falha ao iterar tabela agrupada do catálogo: %w", err)
	}

	if len(todosIDs) > 0 {
		if err := preencherPorEstoque(db, grupos, produtoParaGrupo, todosIDs); err != nil {
			return nil, Paginacao{}, err
		}
	}
	return grupos, paginacao, nil
}

// preencherPorEstoque resolve o `porEstoque` de cada grupo da página numa
// única query (`WHERE pe.produto_id = ANY(...)`), somando por Estoque os
// Produtos que caem no mesmo grupo e ordenando cada lista por `estoqueNome
// ASC` (desempate por `estoqueId` para determinismo).
func preencherPorEstoque(db *sql.DB, grupos []CatalogoGrupo, produtoParaGrupo map[string]int, todosIDs []string) error {
	rows, err := db.Query(catalogoPorEstoqueQuery, pq.Array(todosIDs))
	if err != nil {
		return fmt.Errorf("falha ao listar quantidade por estoque do catálogo: %w", err)
	}
	defer rows.Close()

	acumuladores := make([]map[string]*EstoqueQuantidade, len(grupos))
	for rows.Next() {
		var produtoID, estoqueID, estoqueNome string
		var quantidade float64
		if err := rows.Scan(&produtoID, &estoqueID, &estoqueNome, &quantidade); err != nil {
			return fmt.Errorf("falha ao ler quantidade por estoque do catálogo: %w", err)
		}
		idx, ok := produtoParaGrupo[produtoID]
		if !ok {
			continue
		}
		if acumuladores[idx] == nil {
			acumuladores[idx] = make(map[string]*EstoqueQuantidade)
		}
		if eq, existe := acumuladores[idx][estoqueID]; existe {
			eq.Quantidade += quantidade
		} else {
			acumuladores[idx][estoqueID] = &EstoqueQuantidade{
				EstoqueID:   estoqueID,
				EstoqueNome: estoqueNome,
				Quantidade:  quantidade,
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("falha ao iterar quantidade por estoque do catálogo: %w", err)
	}

	for idx := range grupos {
		if acumuladores[idx] == nil {
			continue
		}
		lista := make([]EstoqueQuantidade, 0, len(acumuladores[idx]))
		for _, eq := range acumuladores[idx] {
			lista = append(lista, *eq)
		}
		sort.Slice(lista, func(i, j int) bool {
			if lista[i].EstoqueNome != lista[j].EstoqueNome {
				return lista[i].EstoqueNome < lista[j].EstoqueNome
			}
			return lista[i].EstoqueID < lista[j].EstoqueID
		})
		grupos[idx].PorEstoque = lista
	}
	return nil
}
