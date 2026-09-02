package services

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

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

// FiltrosCatalogo agrupa os 4 filtros opcionais combináveis por E lógico do
// Catálogo (Story 4.2, spec-4-2), aplicados ANTES do `GROUP BY` nas 3 queries
// (grade, contagem de grupos, grupos) — decidem QUAIS Produtos entram, nunca
// recortam o que é mostrado sobre quem entrou (Design Notes). Zero-value
// (`FiltrosCatalogo{}`) = nenhum filtro, comportamento idêntico à Story 4.3
// (sem regressão). `Q` já deve chegar trimado e validado (<=255 runes) pelo
// chamador (handler) — aqui só é usado, nunca revalidado. `ComEstoque == nil`
// significa "sem filtro"; `*ComEstoque` distingue `true`/`false`.
type FiltrosCatalogo struct {
	Q           string
	CategoriaID string
	EstoqueID   string
	ComEstoque  *bool
}

// filtroUUIDInvalido reconhece o SQLSTATE 22P02 (invalid_text_representation)
// do Postgres — dispara quando `categoriaId`/`estoqueId` malformado (não-UUID)
// tenta comparar com uma coluna `uuid`. Mesmo padrão de colapso de
// ObterProdutoDetalhe/ExcluirEstoque: NUNCA um erro 500, sempre "zero linhas"
// (Always, spec-4-2) — o chamador usa isto para devolver uma página vazia em
// vez de propagar o erro.
func filtroUUIDInvalido(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation
}

// montarFiltrosCatalogo monta o fragmento `WHERE ...` (com a palavra-chave
// incluída, ou "" quando nenhum filtro está presente) e os argumentos
// correspondentes a partir de FiltrosCatalogo, numerando os placeholders a
// partir de `primeiroPlaceholder`. Os filtros combinam sempre por E lógico
// (AND) entre si — nenhum substitui outro (Always, spec-4-2). Espera `p`
// (produtos) e `c` (categorias) como aliases já presentes na query alvo.
//
//   - `Q`: substring case-insensitive em nome/código/categoria, mesmo padrão
//     ILIKE...ESCAPE '\' de buscarProdutosQuery (Story 4.1), sem ranking.
//   - `CategoriaID`: igualdade exata com `p.categoria_id`.
//   - `EstoqueID`: `EXISTS` em `produto_estoque` para esse Estoque, qualquer
//     quantidade (inclusive 0) — presença de linha, não de saldo.
//   - `ComEstoque`: SEMPRE global (soma de TODOS os Estoques do Produto,
//     nunca escopado a `EstoqueID` — Design Notes); comparação com uma
//     constante SQL (">"/"=" 0), nunca com um placeholder de usuário, então
//     não consome um número de placeholder.
func montarFiltrosCatalogo(f FiltrosCatalogo, primeiroPlaceholder int) (string, []any) {
	// `p.deleted_at IS NULL` é SEMPRE a primeira condição (Story 6.4,
	// spec-6-4) — deixa de ser opcional: um Produto mesclado nunca aparece na
	// grade, no agrupado nem na exportação, sem consumir um número de
	// placeholder (comparação com uma constante SQL, não com input do
	// usuário).
	condicoes := []string{"p.deleted_at IS NULL"}
	var args []any
	n := primeiroPlaceholder

	if f.Q != "" {
		condicoes = append(condicoes, fmt.Sprintf(
			`(p.nome ILIKE $%d ESCAPE '\' OR p.codigo ILIKE $%d ESCAPE '\' OR c.nome ILIKE $%d ESCAPE '\')`,
			n, n, n,
		))
		args = append(args, "%"+escaparCoringasLike(f.Q)+"%")
		n++
	}
	if f.CategoriaID != "" {
		condicoes = append(condicoes, fmt.Sprintf("p.categoria_id = $%d", n))
		args = append(args, f.CategoriaID)
		n++
	}
	if f.EstoqueID != "" {
		condicoes = append(condicoes, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM produto_estoque fe WHERE fe.produto_id = p.id AND fe.estoque_id = $%d)", n,
		))
		args = append(args, f.EstoqueID)
		n++
	}
	if f.ComEstoque != nil {
		op := ">"
		if !*f.ComEstoque {
			op = "="
		}
		condicoes = append(condicoes, fmt.Sprintf(
			"COALESCE((SELECT SUM(fc.quantidade) FROM produto_estoque fc WHERE fc.produto_id = p.id), 0) %s 0", op,
		))
	}

	if len(condicoes) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(condicoes, " AND "), args
}

// catalogoGradeQueryBase é a parte fixa da grade (Story 4.3) — sem `WHERE`
// nem `LIMIT`/`OFFSET`, que ListarCatalogoGrade acrescenta dinamicamente
// conforme os filtros presentes (Story 4.2, spec-4-2): o `WHERE` (quando há
// algum filtro) entra ANTES do `ORDER BY`, com placeholders numerados a
// partir de $1; `LIMIT`/`OFFSET` sempre ficam com os dois últimos
// placeholders da query final. Sem índice novo: ~8.000 linhas com um LEFT
// JOIN a um agregado é volume trivial para o Postgres (Design Notes).
const catalogoGradeQueryBase = `
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
	) pe ON pe.produto_id = p.id`

// ListarCatalogoGrade devolve a página `pagina` da grade do Catálogo e o
// bloco de paginação (contagem sobre `produtos`, já com os `filtros`
// aplicados). `pagina` é assumido >=1 (validado pelo handler). Página além da
// última -> slice vazio (nunca `nil`), paginação ainda com
// `total`/`totalPaginas` corretos. `categoriaId`/`estoqueId` malformado
// (não-UUID) colapsa em página vazia, nunca erro (filtroUUIDInvalido).
func ListarCatalogoGrade(db *sql.DB, pagina int, filtros FiltrosCatalogo) ([]CatalogoItem, Paginacao, error) {
	if pagina < 1 {
		pagina = 1
	}

	where, args := montarFiltrosCatalogo(filtros, 1)

	var total int
	countQuery := "SELECT count(*) FROM produtos p JOIN categorias c ON c.id = p.categoria_id" + where
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		if filtroUUIDInvalido(err) {
			return make([]CatalogoItem, 0), novaPaginacao(pagina, 0), nil
		}
		return nil, Paginacao{}, fmt.Errorf("falha ao contar produtos do catálogo: %w", err)
	}
	paginacao := novaPaginacao(pagina, total)

	limitArgs := append(append([]any{}, args...), TamanhoPaginaCatalogo, (pagina-1)*TamanhoPaginaCatalogo)
	query := catalogoGradeQueryBase + where +
		fmt.Sprintf(" ORDER BY p.nome ASC, p.id ASC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	rows, err := db.Query(query, limitArgs...)
	if err != nil {
		if filtroUUIDInvalido(err) {
			return make([]CatalogoItem, 0), paginacao, nil
		}
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
// agrupada: `nome` + o par (valor, unidade) das 5 dimensões estruturadas,
// qualificadas com `p.` (Story 4.2: a query de grupo ganhou `JOIN categorias
// c`, que também tem uma coluna `nome` — sem qualificar, `nome` ficaria
// ambíguo). `NULL` agrupa com `NULL` (semântica de `GROUP BY` do Postgres).
const colunasChaveGrupo = `p.nome,
	p.comprimento_valor, p.comprimento_unidade,
	p.largura_valor, p.largura_unidade,
	p.diametro_valor, p.diametro_unidade,
	p.altura_valor, p.altura_unidade,
	p.espessura_valor, p.espessura_unidade`

// catalogoGrupoCountQueryBase é a parte fixa da contagem de GRUPOS (não
// Produtos) — a unidade sobre a qual `agrupar=true` pagina. Ganha o mesmo
// `JOIN categorias c` da query de grupo (Story 4.2, spec-4-2) para que o
// filtro `q` sobre `categorias.nome` também valha na contagem; sem `WHERE`,
// que ListarCatalogoAgrupado acrescenta dinamicamente entre esta base e
// catalogoGrupoCountQuerySuffix.
const catalogoGrupoCountQueryBase = `
	SELECT count(*) FROM (
		SELECT 1 FROM produtos p
		JOIN categorias c ON c.id = p.categoria_id`

// catalogoGrupoCountQuerySuffix fecha a subquery de contagem de grupos
// (GROUP BY pelas mesmas colunas da query de grupo).
const catalogoGrupoCountQuerySuffix = `
	GROUP BY ` + colunasChaveGrupo + `
	) t`

// catalogoGrupoQueryBase é a parte fixa da query de grupos — sem `WHERE` nem
// `GROUP BY`/`ORDER BY`/`LIMIT`/`OFFSET`, que ListarCatalogoAgrupado
// acrescenta dinamicamente (Story 4.2, spec-4-2). `chave` = md5 estável da
// concatenação de `nome` + os 10 valores de dimensão (delimitados por chr(31)
// para que um valor contendo o delimitador não colida com outro grupo),
// serve de `key` no React. `quantidade_total` = soma de TODAS as linhas
// `produto_estoque` de TODOS os Produtos do grupo (LEFT JOIN + COALESCE).
// `produto_ids` = ids de Produto do grupo, para resolver o `porEstoque` numa
// segunda query com `WHERE pe.produto_id = ANY(...)`. Ganha `JOIN categorias
// c` (ausente antes da Story 4.2) para suportar o filtro `q` sobre
// `categorias.nome` — join 1:1 pela FK `categoria_id`, nunca multiplica
// linhas (não afeta o `GROUP BY`/agregados abaixo).
const catalogoGrupoQueryBase = `
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
	JOIN categorias c ON c.id = p.categoria_id
	LEFT JOIN produto_estoque pe ON pe.produto_id = p.id`

// catalogoGrupoQuerySuffix fecha a query de grupo (GROUP BY/ORDER BY), sem
// `LIMIT`/`OFFSET` — ListarCatalogoAgrupado acrescenta os dois com
// placeholders numerados depois dos do `WHERE`.
const catalogoGrupoQuerySuffix = `
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
		chave ASC`

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
// Catálogo e o bloco de paginação (contagem sobre GRUPOS, já com os
// `filtros` aplicados ANTES do agrupamento — Design Notes, spec-4-2). Um
// grupo = Produtos com o mesmo `nome` e as mesmas 5 dimensões estruturadas.
// `pagina` é assumido >=1 (validado pelo handler). Página além da última ->
// slice vazio (nunca `nil`). `porEstoque` de um grupo é sempre não-`nil`
// (pode ser `[]`). `categoriaId`/`estoqueId` malformado (não-UUID) colapsa em
// página vazia, nunca erro (filtroUUIDInvalido).
func ListarCatalogoAgrupado(db *sql.DB, pagina int, filtros FiltrosCatalogo) ([]CatalogoGrupo, Paginacao, error) {
	if pagina < 1 {
		pagina = 1
	}

	where, args := montarFiltrosCatalogo(filtros, 1)

	var total int
	countQuery := catalogoGrupoCountQueryBase + where + catalogoGrupoCountQuerySuffix
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		if filtroUUIDInvalido(err) {
			return make([]CatalogoGrupo, 0), novaPaginacao(pagina, 0), nil
		}
		return nil, Paginacao{}, fmt.Errorf("falha ao contar grupos do catálogo: %w", err)
	}
	paginacao := novaPaginacao(pagina, total)

	limitArgs := append(append([]any{}, args...), TamanhoPaginaCatalogo, (pagina-1)*TamanhoPaginaCatalogo)
	query := catalogoGrupoQueryBase + where + catalogoGrupoQuerySuffix +
		fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	rows, err := db.Query(query, limitArgs...)
	if err != nil {
		if filtroUUIDInvalido(err) {
			return make([]CatalogoGrupo, 0), paginacao, nil
		}
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

// ListarTodosGruposCatalogo devolve TODOS os grupos da tabela agrupada do
// Catálogo que casam os `filtros` — SEM paginação (Story 4.6, spec-4-6): a
// exportação para Excel sempre reflete o filtro completo, nunca uma única
// página (`TamanhoPaginaCatalogo`, Story 4.3, é uma decisão de UI de tela).
// Reusa catalogoGrupoQueryBase/catalogoGrupoQuerySuffix/preencherPorEstoque
// de ListarCatalogoAgrupado — mesma query/filtros/ordem, só sem
// `LIMIT`/`OFFSET`; ListarCatalogoAgrupado em si NUNCA é alterada por esta
// story (Never, spec-4-6), por isso a duplicação do laço de scan abaixo em
// vez de um helper compartilhado entre as duas. `categoriaId`/`estoqueId`
// malformado (não-UUID) colapsa em slice vazio, nunca erro
// (filtroUUIDInvalido) — mesmo colapso de ListarCatalogoAgrupado.
func ListarTodosGruposCatalogo(db *sql.DB, filtros FiltrosCatalogo) ([]CatalogoGrupo, error) {
	where, args := montarFiltrosCatalogo(filtros, 1)

	query := catalogoGrupoQueryBase + where + catalogoGrupoQuerySuffix
	rows, err := db.Query(query, args...)
	if err != nil {
		if filtroUUIDInvalido(err) {
			return make([]CatalogoGrupo, 0), nil
		}
		return nil, fmt.Errorf("falha ao listar todos os grupos do catálogo: %w", err)
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
			return nil, fmt.Errorf("falha ao ler linha de todos os grupos do catálogo: %w", err)
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
		return nil, fmt.Errorf("falha ao iterar todos os grupos do catálogo: %w", err)
	}

	if len(todosIDs) > 0 {
		if err := preencherPorEstoque(db, grupos, produtoParaGrupo, todosIDs); err != nil {
			return nil, err
		}
	}
	return grupos, nil
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

// ProdutoDetalhe é a projeção de GET /api/produtos/{id} (Story 4.4,
// spec-4-4): os mesmos campos/tipos de CatalogoItem (Story 4.3) mais
// `PorEstoque` — a quantidade discriminada por Estoque, mesma projeção de
// CatalogoGrupo.PorEstoque. `PorEstoque` é sempre não-nil (pode ser `[]`),
// ordenado `estoqueNome ASC, estoqueId ASC` (mesmo critério de
// preencherPorEstoque).
type ProdutoDetalhe struct {
	ID              string              `json:"id"`
	Nome            string              `json:"nome"`
	Codigo          *string             `json:"codigo"`
	Categoria       Categoria           `json:"categoria"`
	Dimensoes       DimensoesProduto    `json:"dimensoes"`
	QuantidadeTotal float64             `json:"quantidadeTotal"`
	Disponivel      bool                `json:"disponivel"`
	PorEstoque      []EstoqueQuantidade `json:"porEstoque"`
}

// produtoDetalheQuery devolve um único Produto por `id` — mesmas colunas de
// catalogoGradeQueryBase, trocando LIMIT/OFFSET por WHERE p.id = $1 (sem
// paginação: só 1 linha, no máximo). Sem filtros da Story 4.2: Story 4.4 é
// sempre "um Produto específico por id", nunca uma listagem filtrável.
const produtoDetalheQuery = `
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
	WHERE p.id = $1 AND p.deleted_at IS NULL`

// ObterProdutoDetalhe devolve o detalhe de um único Produto por `id`
// (Story 4.4, spec-4-4): a mesma quantidade total/disponibilidade da grade
// (Story 4.3, ListarCatalogoGrade), mais a discriminação por Estoque via
// catalogoPorEstoqueQuery — agregação em Go trivial (só 1 Produto, nunca
// precisa do mapa multi-grupo de preencherPorEstoque, já que cada linha
// `produto_estoque` de um único Produto é, por definição, um Estoque
// distinto — a PK composta (produto_id, estoque_id) garante isso).
//
// `id` inexistente OU malformado (não-UUID, `pq` SQLSTATE 22P02) ->
// ErrProdutoNaoEncontrado — mesmo colapso de AtualizarNomeProduto.
func ObterProdutoDetalhe(db *sql.DB, id string) (ProdutoDetalhe, error) {
	var (
		det                        ProdutoDetalhe
		codigo                     sql.NullString
		comp, larg, diam, alt, esp parDimensao
		quantidade                 float64
	)
	err := db.QueryRow(produtoDetalheQuery, id).Scan(
		&det.ID, &det.Nome, &codigo,
		&det.Categoria.ID, &det.Categoria.Codigo, &det.Categoria.Nome,
		&comp.valor, &comp.unidade,
		&larg.valor, &larg.unidade,
		&diam.valor, &diam.unidade,
		&alt.valor, &alt.unidade,
		&esp.valor, &esp.unidade,
		&quantidade,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return ProdutoDetalhe{}, ErrProdutoNaoEncontrado
		}
		return ProdutoDetalhe{}, fmt.Errorf("falha ao buscar detalhe do produto: %w", err)
	}
	if codigo.Valid {
		c := codigo.String
		det.Codigo = &c
	}
	det.Dimensoes = DimensoesProduto{
		Comprimento: comp.paraDimensao(),
		Largura:     larg.paraDimensao(),
		Diametro:    diam.paraDimensao(),
		Altura:      alt.paraDimensao(),
		Espessura:   esp.paraDimensao(),
	}
	det.QuantidadeTotal = quantidade
	det.Disponivel = quantidade > 0

	rows, err := db.Query(catalogoPorEstoqueQuery, pq.Array([]string{id}))
	if err != nil {
		return ProdutoDetalhe{}, fmt.Errorf("falha ao listar quantidade por estoque do produto: %w", err)
	}
	defer rows.Close()

	lista := make([]EstoqueQuantidade, 0)
	for rows.Next() {
		var eq EstoqueQuantidade
		var produtoID string
		if err := rows.Scan(&produtoID, &eq.EstoqueID, &eq.EstoqueNome, &eq.Quantidade); err != nil {
			return ProdutoDetalhe{}, fmt.Errorf("falha ao ler quantidade por estoque do produto: %w", err)
		}
		lista = append(lista, eq)
	}
	if err := rows.Err(); err != nil {
		return ProdutoDetalhe{}, fmt.Errorf("falha ao iterar quantidade por estoque do produto: %w", err)
	}
	sort.Slice(lista, func(i, j int) bool {
		if lista[i].EstoqueNome != lista[j].EstoqueNome {
			return lista[i].EstoqueNome < lista[j].EstoqueNome
		}
		return lista[i].EstoqueID < lista[j].EstoqueID
	})
	det.PorEstoque = lista

	return det, nil
}
