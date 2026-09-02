package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lib/pq"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"stockflow/backend/services"
)

// removerAcentos descarta marcas diacríticas (NFD -> remove runas Mn -> NFC)
// — encontrado ao testar contra um export real do Firestore (Ricardo,
// 2026-09-02): ~4% dos Produtos legados usam "Maquinas" sem acento onde a
// categoria fixa (migration 000010, addendum §H) é "Máquinas". Padrão
// sistemático de digitação do legado, não erro aleatório; nenhuma das 25
// categorias fixas colide com outra ao remover acentos, então isto nunca
// funde duas categorias distintas. golang.org/x/text já é dependência
// indireta (via excelize) — nenhuma dependência nova.
func removerAcentos(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return out
}

// Migração de Produtos legados — Story 3.7 (spec-3-7). Estende
// backend/cmd/migrate-legado (Story 2.3) com migrarProdutos, chamada
// sequencialmente após migrarEstoques no mesmo main(). Reusa
// migracao_id_map (entidade='produto') para idempotência, e o parser único
// de dimensão texto-livre->estruturado (parseDimensaoLegado, local a este
// binário) e a lógica de foto (foto.go, também local).
//
// Diferente de migrarEstoques (uma única transação para TODO o lote), aqui
// cada linha legada não mapeada usa a SUA PRÓPRIA transação: fotos são
// processadas e gravadas via services.SalvarFotoProduto(alvo, ...) DEPOIS do
// commit de cada Produto — SalvarFotoProduto verifica a existência do
// Produto com uma consulta própria em `alvo *sql.DB` (nova conexão), que só
// enxerga linhas já commitadas. Uma falha de foto NUNCA desfaz o Produto já
// commitado (spec-3-7: "foto corrompida... não aborta o corte").

// reDimensaoLegado casa o formato dos exemplos do addendum §F ("6m",
// "100mm"): um número (ponto ou vírgula decimal) seguido, sem espaço, de
// mm/cm/m.
var reDimensaoLegado = regexp.MustCompile(`^([0-9]+(?:[.,][0-9]+)?)(mm|cm|m)$`)

// parseDimensaoLegado reconhece o formato texto-livre das 5 dimensões reais
// do legado (comprimento, largura, diametro, altura, espessura — nunca
// `lateral`, tratado à parte). `ok == false` significa "ambíguo": o
// chamador não aborta o corte, só marca o campo em
// produtos.dimensoes_pendentes_revisao com o texto original.
func parseDimensaoLegado(texto string) (valor float64, unidade string, ok bool) {
	m := reDimensaoLegado.FindStringSubmatch(strings.TrimSpace(texto))
	if m == nil {
		return 0, "", false
	}
	numStr := strings.ReplaceAll(m[1], ",", ".")
	v, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, "", false
	}
	return v, m[2], true
}

// ResultadoMigracaoProdutos é o relatório de uma execução de migrarProdutos.
// Qualquer uma das listas de pré-checagem (Categorias.../Estoques.../
// Quantidades.../Codigos...) não-vazia significa "corte abortado, nada
// escrito" e vem acompanhada de um error não-nil — mesmo contrato de
// ResultadoMigracao (Estoques). FotosComFalha é diferente: NUNCA aborta,
// só relata (spec-3-7, "Foto corrompida" na I/O Matrix).
type ResultadoMigracaoProdutos struct {
	Migrados   int
	JaMigrados int
	// NomesInvalidos reusa o tipo NomeInvalido{IDLegado, Motivo} já declarado
	// em main.go para Estoques (mesma forma exata) — um Produto legado com
	// `nome` nulo, vazio após trim, ou acima de 255 runes (limite de
	// `produtos.nome VARCHAR(255) NOT NULL`, migration 000011).
	NomesInvalidos           []NomeInvalido
	CategoriasDesconhecidas  []CategoriaDesconhecida
	EstoquesDesconhecidos    []EstoqueDesconhecido
	QuantidadesInvalidas     []QuantidadeInvalida
	EstoquesColisaoNoProduto []EstoqueColisaoNoProduto
	CodigosDuplicadosLegado  []CodigoDuplicadoLegado
	CodigosColisaoAlvo       []CodigoColisaoAlvo
	FotosComFalha            []FotoFalha
}

// CategoriaDesconhecida descreve um Produto legado cujo `categoria` não casa
// (lower(btrim(...))) nenhuma linha de categorias.nome no alvo.
type CategoriaDesconhecida struct {
	IDLegado  string
	Categoria string
}

// EstoqueDesconhecido descreve uma entrada do mapa `estoques` de um Produto
// legado cujo nome, normalizado pela MESMA normExpr de estoques
// (nome_normalizado), não casa nenhum Estoque no alvo.
type EstoqueDesconhecido struct {
	IDLegado    string
	NomeEstoque string
}

// QuantidadeInvalida descreve uma entrada do mapa `estoques` de um Produto
// legado com quantidade < 0 (viola o CHECK de produto_estoque.quantidade,
// migration 000012) OU cujo valor bruto não é um número válido (o JSON do
// legado tem um valor não-numérico onde addendum §F documenta
// `map<string,number>`). `Quantidade` só é significativo quando `Motivo`
// descreve o caso "negativa" — no caso "não numérico" fica no zero-value.
type QuantidadeInvalida struct {
	IDLegado    string
	NomeEstoque string
	Quantidade  float64
	Motivo      string
}

// EstoqueColisaoNoProduto descreve duas ou mais entradas do mapa `estoques`
// de UM MESMO Produto legado cujos nomes, depois de normalizados, resolvem
// para o MESMO Estoque no banco alvo (ex. `"Canteiro A"` e `"canteiro  a"`
// como chaves distintas do mesmo JSON) — sem esta pré-checagem, o segundo
// INSERT em produto_estoque violaria a PK (produto_id, estoque_id) e
// abortaria o restante do laço de migrarProdutos no meio da execução, fora
// do padrão "aborta, lista tudo, nada escrito" das demais pré-checagens.
type EstoqueColisaoNoProduto struct {
	IDLegado string
	Nomes    []string
}

// CodigoDuplicadoLegado descreve um `codigo` não-nulo repetido entre duas ou
// mais linhas DENTRO do próprio lote legado.
type CodigoDuplicadoLegado struct {
	Codigo string
	IDs    []string
}

// CodigoColisaoAlvo descreve um Produto legado (ainda não migrado) cujo
// `codigo` já existe em produtos.codigo no alvo, fora do mapa.
type CodigoColisaoAlvo struct {
	IDLegado string
	Codigo   string
}

// FotoFalha descreve um Produto cuja foto legada não pôde ser processada
// (base64/imagem corrompida) — o Produto é criado SEM foto; não é motivo de
// abortar o corte.
type FotoFalha struct {
	IDLegado string
	Motivo   string
}

// linhaLegadaProduto é uma linha de legado.produtos já carregada, com o
// `categoria` normalizado (lower(btrim(...))) calculado pelo PRÓPRIO
// Postgres legado.
type linhaLegadaProduto struct {
	id            string
	nome          sql.NullString
	codigo        sql.NullString
	categoria     sql.NullString
	categoriaNorm sql.NullString
	comprimento   sql.NullString
	largura       sql.NullString
	diametro      sql.NullString
	altura        sql.NullString
	espessura     sql.NullString
	lateral       sql.NullString
	obs           sql.NullString
	foto          sql.NullString
	criadoEm      sql.NullTime
}

// estoqueEntradaLegada é uma entrada do mapa `estoques` (nome -> quantidade)
// de um Produto legado, já com o nome normalizado pela MESMA expressão SQL
// de nome_normalizado (migration 000008) — nunca reimplementada em Go.
// `valorBruto` é o texto exato do valor no JSON legado; `quantidade` só é
// significativo quando `quantidadeOK` — um valor não-numérico (ex. `"dez"`
// em vez de `10`) nunca chega a ser convertido pelo Postgres (a query lê
// `e.value` como texto puro, sem `::numeric`), evitando que um valor
// malformado quebre a CARGA inicial com um erro cru de banco antes de
// qualquer pré-checagem estruturada.
type estoqueEntradaLegada struct {
	idLegadoProduto string
	nome            string
	norm            string
	valorBruto      string
	quantidade      float64
	quantidadeOK    bool
}

// migrarProdutos é o ponto testável da migração de Produtos legados (Story
// 3.7). `alvo` é o pool para o schema novo (DATABASE_URL); `legado` é o pool
// para o espelho do Firestore (LEGADO_DATABASE_URL); `fotosDir` é o mesmo
// diretório lido de FOTOS_DIR no processo `api` (Story 3.5), agora também
// lido por este binário one-off. Passos, na ordem:
//
//  1. Pré-condição de seed: categorias e nomenclatura_templates não podem
//     estar vazias (migrations 000010/000013 já as semeiam) — vazio aborta
//     ANTES de ler qualquer linha legada.
//  2. Carrega legado.produtos (com `categoria` já normalizado pelo Postgres
//     legado) e as entradas do mapa `estoques` de cada linha (com o nome já
//     normalizado pela mesma expressão de estoques.nome_normalizado).
//  3. Pré-checagem de categoria desconhecida (aborta, lista, nada escrito).
//  4. Pré-checagem de Estoque desconhecido e de quantidade negativa (duas
//     classes separadas, cada uma aborta independentemente).
//  5. Pré-checagem de código duplicado dentro do lote e de colisão de código
//     com o alvo fora do mapa (duas classes separadas).
//  6. `executar == false`: só conta Migrados/JaMigrados via SELECT no mapa,
//     sem escrever nada e sem processar foto.
//  7. `executar == true`: para cada linha não mapeada, abre UMA transação
//     própria (INSERT produtos + migracao_id_map + produto_estoque, commit);
//     DEPOIS do commit, processa a foto (se houver) via
//     services.SalvarFotoProduto — falha de foto vai para FotosComFalha,
//     nunca desfaz o Produto.
func migrarProdutos(alvo, legado *sql.DB, fotosDir string, executar bool) (ResultadoMigracaoProdutos, error) {
	var res ResultadoMigracaoProdutos

	// 1) Pré-condição de seed — checada ANTES de ler qualquer linha legada.
	var nCategorias, nTemplates int
	if err := alvo.QueryRow(`SELECT count(*) FROM categorias`).Scan(&nCategorias); err != nil {
		return res, fmt.Errorf("falha ao checar seed de categorias: %w", err)
	}
	if err := alvo.QueryRow(`SELECT count(*) FROM nomenclatura_templates`).Scan(&nTemplates); err != nil {
		return res, fmt.Errorf("falha ao checar seed de nomenclatura_templates: %w", err)
	}
	if nCategorias == 0 || nTemplates == 0 {
		return res, fmt.Errorf(
			"seed ausente: categorias=%d, nomenclatura_templates=%d — as migrations 000010/000013 devem semear essas tabelas antes da migração de Produtos",
			nCategorias, nTemplates)
	}

	// 2) Carrega legado.produtos, com `categoria` normalizado pelo próprio
	//    Postgres legado (lower(btrim(...))). `nome` é lido SEM coalesce —
	//    nulo/vazio/longo demais é uma classe de pré-checagem própria
	//    (NomesInvalidos), não um valor silenciosamente substituído.
	//
	//    `categoria` no Firestore real vem como "<código> - <nome>" (ex.
	//    "04.002 - Materiais Elétricos"), não só o nome — achado ao testar
	//    contra um export real do Ricardo (addendum §F previu só o nome
	//    solto; a estrutura real diverge, exatamente o caso que o próprio
	//    addendum já sinalizava como possível). `categorias.nome` no alvo
	//    (migration 000010) é só o nome, sem o código. O regexp_replace
	//    abaixo descarta um prefixo "<dígitos/pontos> - " se existir, antes
	//    do lower/btrim — nunca falha em registros que já vierem só com o
	//    nome (o padrão simplesmente não casa e a string passa intacta).
	rows, err := legado.Query(`
		SELECT id, nome, codigo, categoria,
		       lower(btrim(regexp_replace(categoria, '^[0-9][0-9.]*\s*-\s*', ''))) AS categoria_norm,
		       comprimento, largura, diametro, altura, espessura, "lateral", obs, foto, criado_em
		FROM produtos
		ORDER BY id`)
	if err != nil {
		return res, fmt.Errorf("falha ao ler produtos do banco legado: %w", err)
	}
	var legados []linhaLegadaProduto
	for rows.Next() {
		var l linhaLegadaProduto
		if err := rows.Scan(
			&l.id, &l.nome, &l.codigo, &l.categoria, &l.categoriaNorm,
			&l.comprimento, &l.largura, &l.diametro, &l.altura, &l.espessura,
			&l.lateral, &l.obs, &l.foto, &l.criadoEm,
		); err != nil {
			rows.Close()
			return res, fmt.Errorf("falha ao ler linha de produto legado: %w", err)
		}
		legados = append(legados, l)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return res, fmt.Errorf("falha ao iterar produtos legados: %w", err)
	}
	rows.Close()

	// Pré-checagem de nome inválido — ANTES de qualquer outra pré-checagem
	// relacional (mesma ordem de migrarEstoques: valida o "formato" da linha
	// antes de casar contra outras tabelas). `nome` é NOT NULL em
	// produtos (migration 000011); nulo, vazio após trim, ou acima de 255
	// runes nunca poderia ser inserido.
	for _, l := range legados {
		switch {
		case !l.nome.Valid:
			res.NomesInvalidos = append(res.NomesInvalidos, NomeInvalido{IDLegado: l.id, Motivo: "nome nulo no registro legado"})
		case strings.TrimSpace(l.nome.String) == "":
			res.NomesInvalidos = append(res.NomesInvalidos, NomeInvalido{IDLegado: l.id, Motivo: "nome vazio após normalização (só espaços)"})
		case utf8.RuneCountInString(strings.TrimSpace(l.nome.String)) > 255:
			res.NomesInvalidos = append(res.NomesInvalidos, NomeInvalido{
				IDLegado: l.id,
				Motivo:   fmt.Sprintf("nome com %d caracteres (máximo 255)", utf8.RuneCountInString(strings.TrimSpace(l.nome.String))),
			})
		}
	}
	if len(res.NomesInvalidos) > 0 {
		return res, fmt.Errorf(
			"%d produto(s) legado(s) com nome inválido — revisão manual necessária antes do corte",
			len(res.NomesInvalidos))
	}

	// Entradas do mapa `estoques` (nome -> quantidade) de cada Produto
	// legado, com o nome já normalizado pela MESMA expressão de
	// estoques.nome_normalizado (migration 000008) — computada pelo Postgres
	// legado, nunca reimplementada em Go. `e.value` é lido como TEXTO puro
	// (jsonb_each_text), sem `::numeric`: um valor não-numérico no JSON
	// legado não pode quebrar esta CARGA com um erro cru de banco — vira uma
	// entrada de QuantidadesInvalidas na pré-checagem estruturada abaixo.
	estRows, err := legado.Query(`
		SELECT p.id, e.key, lower(regexp_replace(btrim(e.key), '\s+', ' ', 'g')) AS norm, e.value
		FROM produtos p
		CROSS JOIN LATERAL jsonb_each_text(coalesce(p.estoques, '{}'::jsonb)) AS e(key, value)
		ORDER BY p.id, e.key`)
	if err != nil {
		return res, fmt.Errorf("falha ao ler entradas de estoques dos produtos legados: %w", err)
	}
	estoquesPorProduto := make(map[string][]estoqueEntradaLegada)
	for estRows.Next() {
		var e estoqueEntradaLegada
		if err := estRows.Scan(&e.idLegadoProduto, &e.nome, &e.norm, &e.valorBruto); err != nil {
			estRows.Close()
			return res, fmt.Errorf("falha ao ler entrada de estoque de produto legado: %w", err)
		}
		if v, errParse := strconv.ParseFloat(e.valorBruto, 64); errParse == nil {
			e.quantidade = v
			e.quantidadeOK = true
		}
		estoquesPorProduto[e.idLegadoProduto] = append(estoquesPorProduto[e.idLegadoProduto], e)
	}
	if err := estRows.Err(); err != nil {
		estRows.Close()
		return res, fmt.Errorf("falha ao iterar entradas de estoques dos produtos legados: %w", err)
	}
	estRows.Close()

	// 3) Pré-checagem de categoria desconhecida — categoria_id é NOT NULL
	//    (migration 000011), não há como migrar parcialmente.
	categoriaPorNorm := make(map[string]string)
	catRows, err := alvo.Query(`SELECT lower(btrim(nome)), id FROM categorias`)
	if err != nil {
		return res, fmt.Errorf("falha ao carregar categorias do banco alvo: %w", err)
	}
	for catRows.Next() {
		var norm, id string
		if err := catRows.Scan(&norm, &id); err != nil {
			catRows.Close()
			return res, fmt.Errorf("falha ao ler categoria do banco alvo: %w", err)
		}
		categoriaPorNorm[removerAcentos(norm)] = id
	}
	if err := catRows.Err(); err != nil {
		catRows.Close()
		return res, fmt.Errorf("falha ao iterar categorias do banco alvo: %w", err)
	}
	catRows.Close()

	for _, l := range legados {
		norm := ""
		if l.categoriaNorm.Valid {
			norm = removerAcentos(l.categoriaNorm.String)
		}
		if norm == "" || categoriaPorNorm[norm] == "" {
			res.CategoriasDesconhecidas = append(res.CategoriasDesconhecidas, CategoriaDesconhecida{
				IDLegado: l.id, Categoria: l.categoria.String,
			})
		}
	}
	if len(res.CategoriasDesconhecidas) > 0 {
		return res, fmt.Errorf(
			"%d produto(s) legado(s) com categoria sem correspondência em categorias.nome — revisão manual necessária antes do corte",
			len(res.CategoriasDesconhecidas))
	}

	// 4) Pré-checagem de Estoque desconhecido e de quantidade negativa —
	//    classes separadas, cada uma aborta independentemente.
	estoqueIDPorNorm := make(map[string]string)
	estAlvoRows, err := alvo.Query(`SELECT nome_normalizado, id FROM estoques`)
	if err != nil {
		return res, fmt.Errorf("falha ao carregar estoques do banco alvo: %w", err)
	}
	for estAlvoRows.Next() {
		var norm, id string
		if err := estAlvoRows.Scan(&norm, &id); err != nil {
			estAlvoRows.Close()
			return res, fmt.Errorf("falha ao ler estoque do banco alvo: %w", err)
		}
		estoqueIDPorNorm[norm] = id
	}
	if err := estAlvoRows.Err(); err != nil {
		estAlvoRows.Close()
		return res, fmt.Errorf("falha ao iterar estoques do banco alvo: %w", err)
	}
	estAlvoRows.Close()

	for _, l := range legados {
		for _, e := range estoquesPorProduto[l.id] {
			if _, ok := estoqueIDPorNorm[e.norm]; !ok {
				res.EstoquesDesconhecidos = append(res.EstoquesDesconhecidos, EstoqueDesconhecido{
					IDLegado: l.id, NomeEstoque: e.nome,
				})
			}
		}
	}
	if len(res.EstoquesDesconhecidos) > 0 {
		return res, fmt.Errorf(
			"%d referência(s) a Estoque sem correspondência em estoques.nome_normalizado — revisão manual necessária antes do corte",
			len(res.EstoquesDesconhecidos))
	}

	for _, l := range legados {
		for _, e := range estoquesPorProduto[l.id] {
			switch {
			case !e.quantidadeOK:
				res.QuantidadesInvalidas = append(res.QuantidadesInvalidas, QuantidadeInvalida{
					IDLegado: l.id, NomeEstoque: e.nome,
					Motivo: fmt.Sprintf("valor de quantidade não é um número válido: %q", e.valorBruto),
				})
			case e.quantidade < 0:
				res.QuantidadesInvalidas = append(res.QuantidadesInvalidas, QuantidadeInvalida{
					IDLegado: l.id, NomeEstoque: e.nome, Quantidade: e.quantidade,
					Motivo: "quantidade negativa",
				})
			}
		}
	}
	if len(res.QuantidadesInvalidas) > 0 {
		return res, fmt.Errorf(
			"%d entrada(s) de estoques legado com quantidade inválida (negativa ou não-numérica) — revisão manual necessária antes do corte",
			len(res.QuantidadesInvalidas))
	}

	// 4.5) Pré-checagem de colisão de nomes de Estoque DENTRO do mesmo
	//      Produto: duas entradas do mapa `estoques` de UMA MESMA linha
	//      legada cujos nomes normalizados resolvem para o MESMO Estoque no
	//      alvo violariam a PK (produto_id, estoque_id) no segundo INSERT em
	//      produto_estoque — detectada aqui, em bloco, ANTES de qualquer
	//      escrita, em vez de abortar o restante do laço de execução no meio
	//      de uma transação.
	for _, l := range legados {
		entradas := estoquesPorProduto[l.id]
		contagemPorEstoqueID := make(map[string]int, len(entradas))
		for _, e := range entradas {
			contagemPorEstoqueID[estoqueIDPorNorm[e.norm]]++
		}
		vistos := make(map[string]bool, len(entradas))
		for _, e := range entradas {
			estoqueID := estoqueIDPorNorm[e.norm]
			if contagemPorEstoqueID[estoqueID] <= 1 || vistos[estoqueID] {
				continue
			}
			vistos[estoqueID] = true
			var nomes []string
			for _, e2 := range entradas {
				if estoqueIDPorNorm[e2.norm] == estoqueID {
					nomes = append(nomes, e2.nome)
				}
			}
			res.EstoquesColisaoNoProduto = append(res.EstoquesColisaoNoProduto, EstoqueColisaoNoProduto{IDLegado: l.id, Nomes: nomes})
		}
	}
	if len(res.EstoquesColisaoNoProduto) > 0 {
		return res, fmt.Errorf(
			"%d produto(s) legado(s) com nomes de Estoque que colidem no mesmo Estoque do banco alvo dentro da própria linha — revisão manual necessária antes do corte",
			len(res.EstoquesColisaoNoProduto))
	}

	// 5) Pré-checagem de código: duplicado dentro do lote, depois colisão
	//    com o alvo (fora do mapa).
	codigoParaIDs := make(map[string][]string)
	for _, l := range legados {
		if !l.codigo.Valid {
			continue
		}
		c := strings.TrimSpace(l.codigo.String)
		if c != "" {
			codigoParaIDs[c] = append(codigoParaIDs[c], l.id)
		}
	}
	codigosDuplicadosVistos := make(map[string]bool)
	for _, l := range legados {
		if !l.codigo.Valid {
			continue
		}
		c := strings.TrimSpace(l.codigo.String)
		if c == "" || codigosDuplicadosVistos[c] {
			continue
		}
		if ids := codigoParaIDs[c]; len(ids) > 1 {
			codigosDuplicadosVistos[c] = true
			res.CodigosDuplicadosLegado = append(res.CodigosDuplicadosLegado, CodigoDuplicadoLegado{Codigo: c, IDs: ids})
		}
	}
	if len(res.CodigosDuplicadosLegado) > 0 {
		return res, fmt.Errorf(
			"%d código(s) repetido(s) dentro do próprio lote legado — revisão manual necessária antes do corte",
			len(res.CodigosDuplicadosLegado))
	}

	mapeados := make(map[string]bool)
	mapRows, err := alvo.Query(`SELECT id_legado FROM migracao_id_map WHERE entidade = 'produto'`)
	if err != nil {
		return res, fmt.Errorf("falha ao carregar migracao_id_map: %w", err)
	}
	for mapRows.Next() {
		var idLegado string
		if err := mapRows.Scan(&idLegado); err != nil {
			mapRows.Close()
			return res, fmt.Errorf("falha ao ler migracao_id_map: %w", err)
		}
		mapeados[idLegado] = true
	}
	if err := mapRows.Err(); err != nil {
		mapRows.Close()
		return res, fmt.Errorf("falha ao iterar migracao_id_map: %w", err)
	}
	mapRows.Close()

	var codigosNaoMapeados []string
	for _, l := range legados {
		if mapeados[l.id] || !l.codigo.Valid {
			continue
		}
		c := strings.TrimSpace(l.codigo.String)
		if c != "" {
			codigosNaoMapeados = append(codigosNaoMapeados, c)
		}
	}
	if len(codigosNaoMapeados) > 0 {
		colideRows, err := alvo.Query(`SELECT codigo FROM produtos WHERE codigo = ANY($1)`, pq.Array(codigosNaoMapeados))
		if err != nil {
			return res, fmt.Errorf("falha na pré-checagem de colisão de código com o banco alvo: %w", err)
		}
		colididos := make(map[string]bool)
		for colideRows.Next() {
			var c string
			if err := colideRows.Scan(&c); err != nil {
				colideRows.Close()
				return res, fmt.Errorf("falha ao ler colisão de código com o banco alvo: %w", err)
			}
			colididos[c] = true
		}
		if err := colideRows.Err(); err != nil {
			colideRows.Close()
			return res, fmt.Errorf("falha ao iterar colisões de código com o banco alvo: %w", err)
		}
		colideRows.Close()

		for _, l := range legados {
			if mapeados[l.id] || !l.codigo.Valid {
				continue
			}
			c := strings.TrimSpace(l.codigo.String)
			if c != "" && colididos[c] {
				res.CodigosColisaoAlvo = append(res.CodigosColisaoAlvo, CodigoColisaoAlvo{IDLegado: l.id, Codigo: c})
			}
		}
	}
	if len(res.CodigosColisaoAlvo) > 0 {
		return res, fmt.Errorf(
			"%d código(s) de produto legado já existem no banco alvo fora do mapa — revisão manual necessária antes do corte",
			len(res.CodigosColisaoAlvo))
	}

	// 6) Dry-run: só o SELECT no mapa, sem transação de escrita e sem
	//    processar foto.
	if !executar {
		for _, l := range legados {
			var idNovo string
			err := alvo.QueryRow(
				`SELECT id_novo FROM migracao_id_map WHERE entidade = 'produto' AND id_legado = $1`, l.id,
			).Scan(&idNovo)
			switch {
			case err == nil:
				res.JaMigrados++
			case errors.Is(err, sql.ErrNoRows):
				res.Migrados++
			default:
				return res, fmt.Errorf("falha ao consultar migracao_id_map (dry-run) para id_legado=%s: %w", l.id, err)
			}
		}
		return res, nil
	}

	// 7) Corte real: UMA transação POR LINHA não-mapeada (não uma única
	//    transação para o lote inteiro, ao contrário de migrarEstoques) —
	//    services.SalvarFotoProduto precisa que o Produto já esteja
	//    commitado e visível por uma consulta própria em `alvo *sql.DB`.
	for _, l := range legados {
		var idNovo string
		err := alvo.QueryRow(
			`SELECT id_novo FROM migracao_id_map WHERE entidade = 'produto' AND id_legado = $1`, l.id,
		).Scan(&idNovo)
		if err == nil {
			res.JaMigrados++
			// A linha já foi migrada numa execução anterior — mas se essa
			// execução tiver sido interrompida ENTRE o commit do Produto e o
			// processamento da foto (ex. o processo morreu no meio), o
			// Produto existe e está no mapa, porém nunca ganhou foto. Sem
			// isto, uma reexecução pularia a foto PARA SEMPRE (o ramo
			// JaMigrados nunca tentava foto). Só reprocessa quando o
			// Produto no alvo ainda não tem NENHUMA foto salva — uma vez
			// que exista ao menos uma, o comportamento continua idempotente
			// (nunca reprocessa de novo).
			if l.foto.Valid && strings.TrimSpace(l.foto.String) != "" {
				fotosExistentes, errListar := services.ListarFotosProduto(alvo, fotosDir, idNovo)
				switch {
				case errListar != nil:
					res.FotosComFalha = append(res.FotosComFalha, FotoFalha{IDLegado: l.id, Motivo: errListar.Error()})
				case len(fotosExistentes) == 0:
					if motivo := processarESalvarFotoLegado(alvo, fotosDir, idNovo, l.foto.String); motivo != "" {
						res.FotosComFalha = append(res.FotosComFalha, FotoFalha{IDLegado: l.id, Motivo: motivo})
					}
				}
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return res, fmt.Errorf("falha ao consultar migracao_id_map para id_legado=%s: %w", l.id, err)
		}

		produtoID, err := migrarUmProduto(alvo, l, categoriaPorNorm[removerAcentos(l.categoriaNorm.String)], estoquesPorProduto[l.id], estoqueIDPorNorm)
		if err != nil {
			return res, err
		}
		res.Migrados++

		// Foto — DEPOIS do commit da linha, nunca dentro da transação: o
		// campo `foto` vazio/ausente não é erro, nunca entra em
		// FotosComFalha.
		if l.foto.Valid && strings.TrimSpace(l.foto.String) != "" {
			if motivo := processarESalvarFotoLegado(alvo, fotosDir, produtoID, l.foto.String); motivo != "" {
				res.FotosComFalha = append(res.FotosComFalha, FotoFalha{IDLegado: l.id, Motivo: motivo})
			}
		}
	}

	return res, nil
}

// processarESalvarFotoLegado roda o pipeline de foto (processarFotoLegado,
// foto.go) e, no sucesso, grava via services.SalvarFotoProduto — usada tanto
// para um Produto recém-migrado quanto para um já mapeado que ainda não tem
// foto em disco (reprocessamento após uma execução interrompida). Devolve
// string vazia no sucesso, ou o motivo da falha (para FotosComFalha) —
// NUNCA um error que aborte o corte.
func processarESalvarFotoLegado(alvo *sql.DB, fotosDir, produtoID, fotoBase64 string) string {
	jpegBytes, motivo := processarFotoLegado(fotoBase64)
	if motivo != "" {
		return motivo
	}
	if _, err := services.SalvarFotoProduto(alvo, fotosDir, produtoID, jpegBytes); err != nil {
		return err.Error()
	}
	return ""
}

// migrarUmProduto insere UM Produto legado (já validado pelas pré-checagens
// de migrarProdutos) numa transação própria: INSERT produtos + INSERT
// migracao_id_map + INSERT produto_estoque (um por entrada de `estoques`),
// commit. Devolve o id novo do Produto no sucesso.
func migrarUmProduto(alvo *sql.DB, l linhaLegadaProduto, categoriaID string, entradasEstoque []estoqueEntradaLegada, estoqueIDPorNorm map[string]string) (string, error) {
	comprimentoValor, comprimentoUnidade, comprimentoPendente := resolverDimensao(l.comprimento)
	larguraValor, larguraUnidade, larguraPendente := resolverDimensao(l.largura)
	diametroValor, diametroUnidade, diametroPendente := resolverDimensao(l.diametro)
	alturaValor, alturaUnidade, alturaPendente := resolverDimensao(l.altura)
	espessuraValor, espessuraUnidade, espessuraPendente := resolverDimensao(l.espessura)

	pendentes := map[string]string{}
	if comprimentoPendente != "" {
		pendentes["comprimento"] = comprimentoPendente
	}
	if larguraPendente != "" {
		pendentes["largura"] = larguraPendente
	}
	if diametroPendente != "" {
		pendentes["diametro"] = diametroPendente
	}
	if alturaPendente != "" {
		pendentes["altura"] = alturaPendente
	}
	if espessuraPendente != "" {
		pendentes["espessura"] = espessuraPendente
	}
	var dimPendentesJSON sql.NullString
	if len(pendentes) > 0 {
		b, err := json.Marshal(pendentes)
		if err != nil {
			return "", fmt.Errorf("falha ao serializar dimensoes_pendentes_revisao para id_legado=%s: %w", l.id, err)
		}
		dimPendentesJSON = sql.NullString{String: string(b), Valid: true}
	}

	observacoes := construirObservacoes(l.obs, l.lateral)

	var codigo sql.NullString
	if l.codigo.Valid {
		c := strings.TrimSpace(l.codigo.String)
		if c != "" {
			codigo = sql.NullString{String: c, Valid: true}
		}
	}

	tx, err := alvo.Begin()
	if err != nil {
		return "", fmt.Errorf("falha ao abrir transação para id_legado=%s: %w", l.id, err)
	}

	var produtoID string
	err = tx.QueryRow(`
		INSERT INTO produtos (
			nome, codigo, categoria_id, observacoes, template_id,
			comprimento_valor, comprimento_unidade,
			largura_valor, largura_unidade,
			diametro_valor, diametro_unidade,
			altura_valor, altura_unidade,
			espessura_valor, espessura_unidade,
			dimensoes_pendentes_revisao,
			criado_em
		) VALUES ($1, $2, $3, $4, NULL, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, COALESCE($16, now()))
		RETURNING id`,
		l.nome.String, codigo, categoriaID, observacoes,
		comprimentoValor, comprimentoUnidade,
		larguraValor, larguraUnidade,
		diametroValor, diametroUnidade,
		alturaValor, alturaUnidade,
		espessuraValor, espessuraUnidade,
		dimPendentesJSON, l.criadoEm,
	).Scan(&produtoID)
	if err != nil {
		_ = tx.Rollback()
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation {
			return "", fmt.Errorf(
				"colisão de código com o banco alvo (backstop 23505): id_legado=%s codigo=%q já existe em produtos fora do mapa — corte abortado, nada foi escrito para esta linha",
				l.id, codigo.String)
		}
		return "", fmt.Errorf("falha ao inserir produto para id_legado=%s: %w", l.id, err)
	}

	if _, err := tx.Exec(
		`INSERT INTO migracao_id_map (entidade, id_legado, id_novo) VALUES ('produto', $1, $2)`,
		l.id, produtoID,
	); err != nil {
		_ = tx.Rollback()
		return "", fmt.Errorf("falha ao gravar migracao_id_map para id_legado=%s: %w", l.id, err)
	}

	for _, e := range entradasEstoque {
		estoqueID := estoqueIDPorNorm[e.norm]
		if _, err := tx.Exec(
			`INSERT INTO produto_estoque (produto_id, estoque_id, quantidade) VALUES ($1, $2, $3)`,
			produtoID, estoqueID, e.quantidade,
		); err != nil {
			_ = tx.Rollback()
			return "", fmt.Errorf("falha ao inserir produto_estoque para id_legado=%s estoque=%q: %w", l.id, e.nome, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("falha ao commitar produto para id_legado=%s: %w", l.id, err)
	}
	return produtoID, nil
}

// resolverDimensao aplica a regra desta story a um campo de dimensão em
// texto livre: vazio/ausente -> não informado (valor/unidade NULL,
// `pendente` vazio); casa o formato -> valor/unidade estruturados; não casa
// -> `pendente` devolve o texto original (o chamador grava em
// dimensoes_pendentes_revisao), valor/unidade ficam NULL.
func resolverDimensao(campo sql.NullString) (valor sql.NullFloat64, unidade sql.NullString, pendente string) {
	if !campo.Valid {
		return sql.NullFloat64{}, sql.NullString{}, ""
	}
	texto := strings.TrimSpace(campo.String)
	if texto == "" {
		return sql.NullFloat64{}, sql.NullString{}, ""
	}
	v, u, ok := parseDimensaoLegado(texto)
	if !ok {
		return sql.NullFloat64{}, sql.NullString{}, texto
	}
	return sql.NullFloat64{Float64: v, Valid: true}, sql.NullString{String: u, Valid: true}, ""
}

// construirObservacoes preserva `obs` do legado e, quando `lateral` está
// presente (não-vazio), anexa uma nota — o schema-alvo não tem par
// lateral_valor/lateral_unidade (precedente da Story 3.1, spec-3-1:72) e
// `lateral` nunca vira uma "dimensão pendente" nem bloqueia o corte.
func construirObservacoes(obs, lateral sql.NullString) sql.NullString {
	partes := make([]string, 0, 2)
	if obs.Valid {
		if t := strings.TrimSpace(obs.String); t != "" {
			partes = append(partes, t)
		}
	}
	if lateral.Valid {
		if t := strings.TrimSpace(lateral.String); t != "" {
			partes = append(partes, fmt.Sprintf("Lateral (legado): %q", t))
		}
	}
	if len(partes) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: strings.Join(partes, "\n"), Valid: true}
}
