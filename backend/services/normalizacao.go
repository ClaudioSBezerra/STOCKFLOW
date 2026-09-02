// Package services, arquivo normalizacao.go: núcleo da Detecção de
// inconsistências dimensionais (Story 6.1, spec-6-1, Epic 6 — Normalização de
// Dados), da Aplicação seletiva de correções (Story 6.2, spec-6-2) e da
// Detecção de duplicatas (Story 6.3, spec-6-3).
// AnalisarInconsistencias varre `produtos` sob demanda e devolve uma lista de
// sugestões (produto, campo, valor sugerido, origem) — nenhuma escrita.
// AplicarCorrecoes/IgnorarSugestao (Story 6.2) são as duas únicas escritas
// deste arquivo: a primeira grava o valor sugerido nos campos ainda vazios
// (guard `IS NULL`, nunca sobrescreve); a segunda grava a tupla exata
// (produto, campo, valor, unidade) em `normalizacao_ignoradas`, e
// AnalisarInconsistencias passa a excluir da lista qualquer sugestão cuja
// tupla já esteja lá (ver carregarIgnoradas/chaveIgnorada abaixo).
// DetectarDuplicatas (Story 6.3) varre `produtos` + `produto_estoque` sob
// demanda e devolve grupos de Produtos candidatos a duplicata — também
// nenhuma escrita (mesclar é Story 6.4).
//
// Duas fontes de sugestão, por campo dimensional vazio (`{campo}_valor`/
// `{campo}_unidade` ambos NULL):
//   - origem "migracao": `produtos.dimensoes_pendentes_revisao[campo]`
//     (migration 000019, Story 3.7) reparseado por parseDimensaoTexto — um
//     parser TOLERANTE, mais permissivo que o parseDimensaoLegado estrito
//     usado durante a migração original (cmd/migrate-legado/produtos.go).
//   - origem "nome": só quando EXATAMENTE um dos 5 campos está vazio no
//     Produto — extrairValorDoNome tenta achar um valor com unidade
//     reconhecida (abreviada) no `nome`. Migração tem prioridade: só se
//     tenta a origem "nome" para um campo quando a origem "migracao" não
//     produziu sugestão para ELE (sem entrada em dimensoes_pendentes_revisao,
//     ou entrada presente mas não-parseável).
package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/lib/pq"
)

// Sugestao é uma sugestão de correção dimensional devolvida por
// AnalisarInconsistencias. Valor/Unidade ficam como campos Go planos (mais
// simples de montar e comparar nos testes), mas nunca aparecem soltos no
// JSON — MarshalJSON os aninha em `valorSugerido:{valor,unidade}`, o molde
// exato de DimensaoValor (catalogo.go), a forma exigida pela Intent Contract
// desta story.
type Sugestao struct {
	ProdutoID   string
	ProdutoNome string
	Campo       string
	Valor       float64
	Unidade     string
	Origem      string
}

// MarshalJSON serializa Sugestao no formato de fio `{"produtoId","produtoNome",
// "campo","valorSugerido":{"valor","unidade"},"origem"}` — ver comentário do
// tipo.
func (s Sugestao) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ProdutoID     string        `json:"produtoId"`
		ProdutoNome   string        `json:"produtoNome"`
		Campo         string        `json:"campo"`
		ValorSugerido DimensaoValor `json:"valorSugerido"`
		Origem        string        `json:"origem"`
	}{
		ProdutoID:     s.ProdutoID,
		ProdutoNome:   s.ProdutoNome,
		Campo:         s.Campo,
		ValorSugerido: DimensaoValor{Valor: s.Valor, Unidade: s.Unidade},
		Origem:        s.Origem,
	})
}

// ordemCamposDimensao é a ordem fixa em que os 5 campos dimensionais do
// Produto são avaliados — mesma ordem de DimensoesProduto (catalogo.go) e da
// Intent Contract desta story. Determinismo, não uma exigência funcional (o
// conjunto de sugestões não depende da ordem).
var ordemCamposDimensao = []string{"comprimento", "largura", "diametro", "altura", "espessura"}

// camposDimensaoValidos é ordemCamposDimensao como conjunto — usado pela
// validação de Story 6.2 (validarCorrecao), onde `campo` chega como string
// solta do cliente (ao contrário de produtos.go, que sempre valida um dos 5
// campos nomeados explicitamente).
var camposDimensaoValidos = map[string]bool{
	"comprimento": true,
	"largura":     true,
	"diametro":    true,
	"altura":      true,
	"espessura":   true,
}

// reDimensaoTolerante casa a PRIMEIRA ocorrência número+unidade dentro de um
// texto livre — número com separador decimal `.`/`,`, espaço opcional antes
// da unidade, unidade abreviada (mm/cm/m) OU por extenso
// (milímetro(s)/centímetro(s)/metro(s), com ou sem acento). As formas por
// extenso vêm primeiro na alternância para que "metros" não perca para o "m"
// abreviado na mesma posição (embora o `\b` final já resolvesse isso na
// prática — ver Design Notes de spec-6-1). Mais tolerante de propósito que
// reDimensaoLegado (cmd/migrate-legado/produtos.go), que é ancorado e sem
// espaço: esta é a "revisão manual" que o addendum do PRD previu para os
// casos que o parser da migração não conseguiu converter.
//
// `\b` no início do grupo numérico (simétrico ao `\b` final e ao `reValorNome`
// abaixo) evita casar o rabo de um token maior — sem ele, um texto como
// "LOTE123mm" casaria "123mm" no meio da palavra "LOTE123mm"; com o `\b`,
// não há fronteira entre "E" e "1" (os dois são caracteres de palavra) e o
// match inteiro é descartado, como deveria.
var reDimensaoTolerante = regexp.MustCompile(
	`(?i)\b([0-9]+(?:[.,][0-9]+)?)\s*(mil[ií]metros?|cent[ií]metros?|metros?|mm|cm|m)\b`,
)

// normalizarUnidadeTexto reduz a unidade casada por reDimensaoTolerante
// (abreviada ou por extenso, com ou sem acento) para uma das 3 abreviações do
// enum `dimensao_unidade` (migration 000011): mm/cm/m.
func normalizarUnidadeTexto(bruta string) string {
	b := strings.ToLower(bruta)
	switch {
	case strings.HasPrefix(b, "mil"):
		return "mm"
	case strings.HasPrefix(b, "cent"):
		return "cm"
	case strings.HasPrefix(b, "metro") || b == "m":
		return "m"
	default:
		return b // "mm"/"cm" já vêm na forma final
	}
}

// parseDimensaoTexto é o parser tolerante da origem "migracao" (ver
// comentário do arquivo e Design Notes de spec-6-1). `ok == false` significa
// "ainda ambíguo" (ex. "ver etiqueta", "verificar depois") — nenhum valor é
// inventado, o chamador simplesmente não gera sugestão para o campo.
func parseDimensaoTexto(texto string) (valor float64, unidade string, ok bool) {
	m := reDimensaoTolerante.FindStringSubmatch(texto)
	if m == nil {
		return 0, "", false
	}
	numStr := strings.ReplaceAll(m[1], ",", ".")
	v, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, "", false
	}
	return v, normalizarUnidadeTexto(m[2]), true
}

// reValorNome casa um número seguido (espaço opcional) de uma unidade
// ABREVIADA (mm/cm/m — nunca por extenso, ao contrário de
// reDimensaoTolerante) dentro do `nome` do Produto, com `\b` nas duas pontas.
// Alimenta a origem "nome": só chamada pelo chamador quando já se sabe que
// exatamente um dos 5 campos estruturados está vazio (ver AnalisarInconsistencias).
var reValorNome = regexp.MustCompile(`(?i)\b([0-9]+(?:[.,][0-9]+)?)\s*(mm|cm|m)\b`)

// valorUnidade é um par (valor, unidade) já normalizado — usado internamente
// por extrairValorDoNome para comparar um candidato do nome contra as
// dimensões do Produto que já estão estruturadas.
type valorUnidade struct {
	valor   float64
	unidade string
}

// extrairValorDoNome tenta achar, dentro do `nome` do Produto, a PRIMEIRA
// ocorrência número+unidade abreviada reconhecida cujo par (valor, unidade)
// NÃO coincide com nenhum dos pares em `jaEstruturados` (as dimensões do
// mesmo Produto que já estão preenchidas). Sem esse filtro, um nome com dois
// tokens dimensionais embutidos — ex. "TUBO Ø25MM 6M" com `diametro` já
// estruturado como {25,mm} e `comprimento` como o único campo vazio —
// erraria o alvo: o primeiro token em ordem de leitura ("25MM") pertence a
// um campo que já tem valor, não ao campo vazio; o candidato certo é o
// PRÓXIMO token que sobra depois de descartar os que já batem com um campo
// preenchido ("6M"). `ok == false` quando nenhum token número+unidade
// reconhecível e ainda não-atribuído aparece no nome.
func extrairValorDoNome(nome string, jaEstruturados []valorUnidade) (valor float64, unidade string, ok bool) {
	for _, m := range reValorNome.FindAllStringSubmatch(nome, -1) {
		numStr := strings.ReplaceAll(m[1], ",", ".")
		v, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			continue
		}
		u := strings.ToLower(m[2])

		candidato := valorUnidade{valor: v, unidade: u}
		jaAtribuido := false
		for _, e := range jaEstruturados {
			if candidato == e {
				jaAtribuido = true
				break
			}
		}
		if jaAtribuido {
			continue
		}
		return v, u, true
	}
	return 0, "", false
}

// AnalisarInconsistencias varre TODOS os Produtos (sem paginação/teto — o
// volume é por Produto, não por evento de trilha, ver Code Map de spec-6-1) e
// devolve a lista de sugestões de correção dimensional. Rota só-leitura:
// nenhuma linha é escrita, aqui ou em qualquer chamador. Molde de
// ListarMovimentacoes (movimentacoes.go): SELECT simples, sem transação.
//
// Por Produto, por campo (na ordem ordemCamposDimensao): campo já
// estruturado (`{campo}_valor`/`{campo}_unidade` ambos preenchidos) -> pula,
// nunca gera sugestão, condição testada ANTES de qualquer parsing. Campo
// vazio -> tenta origem "migracao" via
// `dimensoes_pendentes_revisao[campo]` + parseDimensaoTexto. Depois de
// avaliar os 5 campos: se exatamente 1 ficou vazio E a origem "migracao" não
// gerou sugestão para ele, tenta a origem "nome" via extrairValorDoNome —
// zero ou 2+ campos vazios nunca geram sugestão de origem "nome" (ambíguo
// demais: não há como saber qual campo o nome preencheria).
func AnalisarInconsistencias(db *sql.DB) ([]Sugestao, error) {
	// Story 6.2: carrega normalizacao_ignoradas INTEIRA antes de varrer
	// produtos (Code Map, spec-6-2) — o filtro final compara cada Sugestao
	// candidata contra este mapa por chave textual (chaveIgnorada), nunca por
	// igualdade de float64 (Design Notes de spec-6-2).
	ignoradas, err := carregarIgnoradas(db)
	if err != nil {
		return nil, err
	}

	const q = `
		SELECT id, nome,
		       comprimento_valor, comprimento_unidade,
		       largura_valor, largura_unidade,
		       diametro_valor, diametro_unidade,
		       altura_valor, altura_unidade,
		       espessura_valor, espessura_unidade,
		       dimensoes_pendentes_revisao
		FROM produtos
		ORDER BY nome, id`

	rows, err := db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("falha ao consultar produtos para análise de inconsistências: %w", err)
	}
	defer rows.Close()

	sugestoes := make([]Sugestao, 0)
	for rows.Next() {
		var id, nome string
		var compValor, largValor, diamValor, altValor, espValor sql.NullFloat64
		var compUnid, largUnid, diamUnid, altUnid, espUnid sql.NullString
		var pendentesRaw []byte

		if err := rows.Scan(
			&id, &nome,
			&compValor, &compUnid,
			&largValor, &largUnid,
			&diamValor, &diamUnid,
			&altValor, &altUnid,
			&espValor, &espUnid,
			&pendentesRaw,
		); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de produto: %w", err)
		}

		// Uma entrada malformada em dimensoes_pendentes_revisao (linha legada
		// corrompida) NUNCA aborta a análise inteira — isso negaria o
		// relatório de inconsistências para TODO almoxarife por causa de UM
		// Produto ruim. `pendentes` fica `nil` (mapa vazio: nenhuma chave
		// casa em pendentes[c.nome] abaixo), o que só suprime a origem
		// "migracao" para ESTE Produto; a origem "nome" continua tentada
		// normalmente mais abaixo.
		var pendentes map[string]string
		if len(pendentesRaw) > 0 {
			if err := json.Unmarshal(pendentesRaw, &pendentes); err != nil {
				slog.Error("dimensoes_pendentes_revisao malformado — pulando origem migracao para este produto",
					"produtoId", id, "error", err)
				pendentes = nil
			}
		}

		campos := []struct {
			nome    string
			valor   sql.NullFloat64
			unidade sql.NullString
		}{
			{"comprimento", compValor, compUnid},
			{"largura", largValor, largUnid},
			{"diametro", diamValor, diamUnid},
			{"altura", altValor, altUnid},
			{"espessura", espValor, espUnid},
		}

		var camposVazios []string
		var jaEstruturados []valorUnidade
		sugeridoPorMigracao := make(map[string]bool)

		for _, c := range campos {
			if c.valor.Valid && c.unidade.Valid {
				// Estruturado e válido: nunca gera sugestão, de nenhuma
				// origem — mas o par entra em jaEstruturados, para que a
				// origem "nome" (mais abaixo) nunca reatribua o valor de UM
				// campo já preenchido a outro campo vazio do mesmo Produto.
				jaEstruturados = append(jaEstruturados, valorUnidade{valor: c.valor.Float64, unidade: c.unidade.String})
				continue
			}
			camposVazios = append(camposVazios, c.nome)

			texto, temPendente := pendentes[c.nome]
			if !temPendente {
				continue
			}
			if valor, unidade, ok := parseDimensaoTexto(texto); ok {
				sugestoes = append(sugestoes, Sugestao{
					ProdutoID:   id,
					ProdutoNome: nome,
					Campo:       c.nome,
					Valor:       valor,
					Unidade:     unidade,
					Origem:      "migracao",
				})
				sugeridoPorMigracao[c.nome] = true
			}
		}

		if len(camposVazios) == 1 {
			campo := camposVazios[0]
			if !sugeridoPorMigracao[campo] {
				if valor, unidade, ok := extrairValorDoNome(nome, jaEstruturados); ok {
					sugestoes = append(sugestoes, Sugestao{
						ProdutoID:   id,
						ProdutoNome: nome,
						Campo:       campo,
						Valor:       valor,
						Unidade:     unidade,
						Origem:      "nome",
					})
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar produtos: %w", err)
	}

	// Story 6.2: filtro final — descarta qualquer sugestão cuja tupla exata
	// (produtoId,campo,valor,unidade) já esteja em normalizacao_ignoradas.
	// Filtro pós-geração (não embutido no laço acima) de propósito: preserva
	// intacta a lógica de prioridade migração/nome da Story 6.1 — o "ignorar"
	// nunca reabre a heurística para tentar outra origem no lugar da
	// descartada, só remove a sugestão da lista final.
	sugestoesFiltradas := make([]Sugestao, 0, len(sugestoes))
	for _, s := range sugestoes {
		if !ignoradas[chaveIgnorada(s.ProdutoID, s.Campo, s.Valor, s.Unidade)] {
			sugestoesFiltradas = append(sugestoesFiltradas, s)
		}
	}
	return sugestoesFiltradas, nil
}

// chaveIgnorada monta a chave textual usada tanto por carregarIgnoradas
// quanto pelo filtro final de AnalisarInconsistencias para testar uma
// Sugestao candidata contra `normalizacao_ignoradas` — `%.3f` replica a
// escala de NUMERIC(10,3) (mesma coluna de `produtos.{campo}_valor` e de
// `normalizacao_ignoradas.valor`), para não depender de igualdade exata de
// float64 entre um valor nascido em Go (parseDimensaoTexto/
// extrairValorDoNome) e um valor ida-e-volta pelo Postgres (Design Notes de
// spec-6-2).
func chaveIgnorada(produtoID, campo string, valor float64, unidade string) string {
	return fmt.Sprintf("%s|%s|%.3f|%s", produtoID, campo, valor, unidade)
}

// carregarIgnoradas lê `normalizacao_ignoradas` inteira — usada por
// AnalisarInconsistencias (Story 6.2, spec-6-2) para excluir da lista final
// qualquer sugestão já ignorada. Mapa vazio (nunca nil) quando a tabela está
// vazia — nenhuma sugestão é filtrada.
func carregarIgnoradas(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT produto_id, campo, valor, unidade FROM normalizacao_ignoradas`)
	if err != nil {
		return nil, fmt.Errorf("falha ao carregar sugestões ignoradas: %w", err)
	}
	defer rows.Close()

	ignoradas := make(map[string]bool)
	for rows.Next() {
		var produtoID, campo, unidade string
		var valor float64
		if err := rows.Scan(&produtoID, &campo, &valor, &unidade); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de sugestão ignorada: %w", err)
		}
		ignoradas[chaveIgnorada(produtoID, campo, valor, unidade)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar sugestões ignoradas: %w", err)
	}
	return ignoradas, nil
}

// validarCorrecao aplica a MESMA regra de validação de campo/valor/unidade
// de validarDimensao (produtos.go) — campo>0 e <=limiteNumeric103, unidade em
// {mm,cm,m} — acrescida do teste de pertencimento de `campo` ao conjunto
// fechado das 5 dimensões (aqui `campo` chega como string solta do cliente,
// ao contrário de produtos.go, que sempre valida um campo nomeado
// explicitamente). Usada por AplicarCorrecoes e IgnorarSugestao (Story 6.2).
func validarCorrecao(campo string, valor float64, unidade string) error {
	if !camposDimensaoValidos[campo] {
		return &ErroProdutoValidacao{
			Mensagem: fmt.Sprintf("campo inválido: %s", campo),
		}
	}
	if valor <= 0 {
		return &ErroProdutoValidacao{
			Mensagem: fmt.Sprintf("%s: valor deve ser maior que zero", campo),
		}
	}
	if valor > limiteNumeric103 {
		return &ErroProdutoValidacao{
			Mensagem: fmt.Sprintf("%s: valor deve ser no máximo %s", campo, limiteNumeric103Texto),
		}
	}
	if !unidadesDimensaoValidas[unidade] {
		return &ErroProdutoValidacao{
			Mensagem: fmt.Sprintf("%s: unidade deve ser mm, cm ou m", campo),
		}
	}
	return nil
}

// CorrecaoInput é uma correção a aplicar — o par (produto,campo) e o valor
// estruturado a gravar, molde de `Sugestao.MarshalJSON`/DimensaoValor
// (Story 6.2, spec-6-2). O cliente sempre manda o `valorSugerido` exato de
// uma chamada recente a GET /api/normalizacao/inconsistencias — AplicarCorrecoes
// NUNCA reavalia a sugestão contra o estado atual do Produto (Never da
// intent contract); só o guard `IS NULL` protege contra sobrescrita.
type CorrecaoInput struct {
	ProdutoID string
	Campo     string
	Valor     float64
	Unidade   string
}

// CorrecaoAplicada identifica uma correção que REALMENTE afetou uma linha —
// o subconjunto de CorrecaoInput devolvido por AplicarCorrecoes, para que o
// front-end remova da tabela só as linhas confirmadas pelo servidor.
type CorrecaoAplicada struct {
	ProdutoID string `json:"produtoId"`
	Campo     string `json:"campo"`
}

// AplicarCorrecoes grava o valor sugerido nos campos vazios indicados (Story
// 6.2, spec-6-2) — individualmente, em lote por produto ou em lote geral: os
// 3 modos são só tamanhos diferentes da mesma lista `correcoes`, decisão de
// agrupamento inteiramente do front-end (seleção de checkboxes).
//
// Validação de TODAS as correções (campo, valor, unidade — validarCorrecao)
// roda ANTES de qualquer escrita; lista vazia também é erro de validação —
// nenhum item chega a abrir a transação nos dois casos.
//
// Sucesso: UMA transação, um `UPDATE produtos SET {campo}_valor=$,
// {campo}_unidade=$ ... WHERE id=$ AND {campo}_valor IS NULL AND
// {campo}_unidade IS NULL` por item, cada um sob seu próprio SAVEPOINT — o
// guard `IS NULL` garante que uma correção nunca sobrescreve um valor que
// outra ação já preencheu enquanto a lista estava aberta na tela (Design
// Notes de spec-6-2); um item "obsoleto" simplesmente não afeta nenhuma
// linha e não entra no retorno — NUNCA aborta o lote. Dois caminhos
// distintos levam a isso: campo já preenchido ou `produtoId` bem-formado mas
// sem linha correspondente terminam o `UPDATE` sem erro (0 linhas afetadas,
// o SAVEPOINT nem chega a ser usado); só `produtoId` malformado/não-UUID
// dispara um erro do Postgres, tratado abaixo via `ROLLBACK TO SAVEPOINT`
// para isolar o dano. `campo` já passou pelo conjunto fechado
// camposDimensaoValidos acima, então interpolá-lo no texto do UPDATE é
// seguro (nunca vem direto do cliente sem essa validação).
func AplicarCorrecoes(db *sql.DB, correcoes []CorrecaoInput) ([]CorrecaoAplicada, error) {
	if len(correcoes) == 0 {
		return nil, &ErroProdutoValidacao{Mensagem: "correções: informe ao menos uma correção"}
	}
	for _, c := range correcoes {
		if err := validarCorrecao(c.Campo, c.Valor, c.Unidade); err != nil {
			return nil, err
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("falha ao iniciar transação de correções: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	aplicadas := make([]CorrecaoAplicada, 0, len(correcoes))
	for i, c := range correcoes {
		// SAVEPOINT por item: um `produtoId` malformado (não-UUID, SQLSTATE
		// 22P02) faz o UPDATE falhar, e QUALQUER erro dentro de uma transação
		// Postgres marca a transação inteira como abortada — todo `Exec`
		// seguinte falharia com "current transaction is aborted" mesmo para
		// itens válidos, se não houvesse um SAVEPOINT para isolar o dano.
		// `ROLLBACK TO SAVEPOINT` desfaz só o efeito do item ruim e devolve a
		// transação a um estado utilizável para os próximos itens — sem isso,
		// um único produtoId malformado abortaria o lote inteiro, o que o
		// Never/Code Map desta story proíbe explicitamente ("um item
		// obsoleto... NUNCA aborta o lote").
		savepoint := fmt.Sprintf("correcao_%d", i)
		if _, err := tx.Exec("SAVEPOINT " + savepoint); err != nil {
			return nil, fmt.Errorf("falha ao criar savepoint da correção %s/%s: %w", c.ProdutoID, c.Campo, err)
		}

		query := fmt.Sprintf(
			`UPDATE produtos SET %s_valor = $1, %s_unidade = $2
			 WHERE id = $3 AND %s_valor IS NULL AND %s_unidade IS NULL`,
			c.Campo, c.Campo, c.Campo, c.Campo,
		)
		res, err := tx.Exec(query, c.Valor, c.Unidade, c.ProdutoID)
		if err != nil {
			// `produtoId` malformado colapsa no MESMO tratamento de "item
			// obsoleto" — mesma classe de erro que IgnorarSugestao mapeia
			// para ErrProdutoNaoEncontrado. O item some do retorno sem
			// abortar o restante do lote; só um erro de banco genuinamente
			// inesperado (nenhuma das duas SQLSTATEs) ainda falha o lote
			// inteiro.
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && (pqErr.Code == pqForeignKeyViolation || pqErr.Code == pqInvalidTextRepresentation) {
				if _, rerr := tx.Exec("ROLLBACK TO SAVEPOINT " + savepoint); rerr != nil {
					return nil, fmt.Errorf("falha ao reverter savepoint da correção %s/%s: %w", c.ProdutoID, c.Campo, rerr)
				}
				continue
			}
			return nil, fmt.Errorf("falha ao aplicar correção %s/%s: %w", c.ProdutoID, c.Campo, err)
		}
		linhas, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("falha ao ler linhas afetadas da correção %s/%s: %w", c.ProdutoID, c.Campo, err)
		}
		if linhas > 0 {
			aplicadas = append(aplicadas, CorrecaoAplicada{ProdutoID: c.ProdutoID, Campo: c.Campo})
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("falha ao commitar correções: %w", err)
	}
	return aplicadas, nil
}

// IgnorarSugestao grava a tupla EXATA (produtoID,campo,valor,unidade) em
// `normalizacao_ignoradas` (Story 6.2, spec-6-2) — ação inline "Ignorar" por
// linha, um item por chamada. Mesma validação de campo/valor/unidade de
// AplicarCorrecoes (validarCorrecao); `produtoId`/`campo`/`valorSugerido`
// vêm da MESMA Sugestao recente que o front-end já tem na tela.
//
// `ON CONFLICT (produto_id,campo,valor,unidade) DO NOTHING` (a própria
// PRIMARY KEY da tabela, migration 000023): reenviar a mesma tupla nunca é
// erro, idempotente por natureza.
//
// `produtoID` inexistente ou malformado (não-UUID) colapsa em
// ErrProdutoNaoEncontrado — mesmo padrão de CriarProduto/AtualizarNomeProduto
// (produtos.go) para violação de FK/UUID inválido.
func IgnorarSugestao(db *sql.DB, produtoID, campo string, valor float64, unidade string) error {
	if err := validarCorrecao(campo, valor, unidade); err != nil {
		return err
	}

	_, err := db.Exec(
		`INSERT INTO normalizacao_ignoradas (produto_id, campo, valor, unidade)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (produto_id, campo, valor, unidade) DO NOTHING`,
		produtoID, campo, valor, unidade,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && (pqErr.Code == pqForeignKeyViolation || pqErr.Code == pqInvalidTextRepresentation) {
			return ErrProdutoNaoEncontrado
		}
		return fmt.Errorf("falha ao gravar sugestão ignorada: %w", err)
	}
	return nil
}

// --- Detecção de duplicatas (Story 6.3, spec-6-3) --------------------------

// ProdutoDuplicata é um Produto membro de um GrupoDuplicata — id, nome e as 5
// dimensões estruturadas (mesmo shape DimensoesProduto de catalogo.go, para
// que o front-end reuse a mesma formatação do Catálogo).
type ProdutoDuplicata struct {
	ID        string           `json:"id"`
	Nome      string           `json:"nome"`
	Dimensoes DimensoesProduto `json:"dimensoes"`
}

// GrupoDuplicata é um componente conectado de 2+ Produtos candidatos a
// duplicata — nome normalizado igual, dimensões equivalentes par a par e ao
// menos 1 `estoque_id` em comum entre TODOS os membros (Intent Contract,
// spec-6-3). Rota só-leitura: nunca persistido, recalculado a cada chamada de
// DetectarDuplicatas.
type GrupoDuplicata struct {
	Produtos []ProdutoDuplicata `json:"produtos"`
}

// normalizarAcentosReplacer reduz os acentos/cedilha/til pt-BR mais comuns
// para o caractere ASCII equivalente — usado só por normalizarNomeProduto,
// sobre texto já em minúsculas (strings.NewReplacer é case-sensitive, então
// só as formas minúsculas precisam constar aqui). Sem dependência nova
// (`golang.org/x/text/unicode/norm` ou similar) de propósito — o alfabeto
// fechado do glossário do PRD (nome de Produto, pt-BR) cabe inteiro numa
// tabela fixa pequena.
var normalizarAcentosReplacer = strings.NewReplacer(
	"á", "a", "à", "a", "ã", "a", "â", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "õ", "o", "ô", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// normalizarNomeProduto normaliza `nome` para a comparação de agrupamento de
// DetectarDuplicatas (Always, spec-6-3): sem acento, case-insensitive,
// aparado nas pontas — glossário do PRD. Note que só os espaços das PONTAS
// são removidos; espaço duplo no meio do nome (ex. "Tubo  PVC") permanece,
// então dois nomes que só diferem por espaçamento interno NÃO normalizam
// igual — o glossário do PRD não pede colapso de espaço interno.
func normalizarNomeProduto(nome string) string {
	return strings.TrimSpace(normalizarAcentosReplacer.Replace(strings.ToLower(nome)))
}

// converterParaMM converte `valor` (na unidade `unidade`, uma das 3 do enum
// `dimensao_unidade`: mm/cm/m) para milímetros — a unidade comum usada por
// dimensaoEquivalente para comparar duas dimensões de origens diferentes sem
// depender da unidade escolhida em cada Produto (Code Map, spec-6-3).
// `unidade` já chega validada (uma das 3 do enum, gravada por
// CriarProduto/AplicarCorrecoes) — o `default` cobre "mm" (fator 1) sem um
// `case` redundante.
func converterParaMM(valor float64, unidade string) float64 {
	switch unidade {
	case "cm":
		return valor * 10
	case "m":
		return valor * 1000
	default: // "mm"
		return valor
	}
}

// dimensaoEquivalente decide se duas dimensões (mesmo campo, dois Produtos
// diferentes) estão no MESMO estado (Always, spec-6-3): as duas `NULL`, ou as
// duas preenchidas com o MESMO valor após conversão para milímetros. Um lado
// preenchido e o outro vazio nunca é equivalente — evita falso positivo entre
// um Produto já corrigido (Story 6.1/6.2) e um ainda pendente de revisão.
// Comparação via string `%.3f` (não igualdade direta de float64), mesmo
// truque de chaveIgnorada (Story 6.2, Design Notes de spec-6-3): evita
// diferenças de arredondamento de ponto flutuante na conversão de unidade.
func dimensaoEquivalente(a, b parDimensao) bool {
	aPreenchida := a.valor.Valid && a.unidade.Valid
	bPreenchida := b.valor.Valid && b.unidade.Valid
	if aPreenchida != bPreenchida {
		return false
	}
	if !aPreenchida {
		return true // as duas NULL
	}
	return fmt.Sprintf("%.3f", converterParaMM(a.valor.Float64, a.unidade.String)) ==
		fmt.Sprintf("%.3f", converterParaMM(b.valor.Float64, b.unidade.String))
}

// produtoCandidatoDuplicata é a projeção de 1 linha de `produtos` usada por
// DetectarDuplicatas — os pares crus (parDimensao, catalogo.go) das 5
// dimensões ficam retidos (em vez de já convertidos para *DimensaoValor) para
// alimentar dimensaoEquivalente diretamente.
type produtoCandidatoDuplicata struct {
	id              string
	nome            string
	nomeNormalizado string
	comprimento     parDimensao
	largura         parDimensao
	diametro        parDimensao
	altura          parDimensao
	espessura       parDimensao
}

// dimensaoPorCampo devolve o parDimensao de `campo` (um dos 5 nomes de
// ordemCamposDimensao) — usado por dimensoesEquivalentes para iterar os 5
// campos na mesma ordem fixa do resto do pacote.
func (c produtoCandidatoDuplicata) dimensaoPorCampo(campo string) parDimensao {
	switch campo {
	case "comprimento":
		return c.comprimento
	case "largura":
		return c.largura
	case "diametro":
		return c.diametro
	case "altura":
		return c.altura
	case "espessura":
		return c.espessura
	default:
		return parDimensao{}
	}
}

// dimensoesEquivalentes decide se DOIS Produtos têm as 5 dimensões
// estruturadas equivalentes par a par (Always, spec-6-3) — reusa
// ordemCamposDimensao para a ordem de iteração (Code Map, spec-6-3).
func dimensoesEquivalentes(a, b produtoCandidatoDuplicata) bool {
	for _, campo := range ordemCamposDimensao {
		if !dimensaoEquivalente(a.dimensaoPorCampo(campo), b.dimensaoPorCampo(campo)) {
			return false
		}
	}
	return true
}

// carregarLocaisProduto lê `produto_estoque` inteira e devolve, por
// `produto_id`, o conjunto de `estoque_id` onde o Produto tem linha — usado
// por DetectarDuplicatas tanto para o pré-filtro par a par (locaisEmComumPar)
// quanto para a validação de interseção total do componente inteiro
// (interseccaoTotalNaoVazia). Um Produto sem nenhuma linha em
// `produto_estoque` simplesmente não aparece como chave (equivalente a um
// conjunto vazio para quem consulta o mapa).
func carregarLocaisProduto(db *sql.DB) (map[string]map[string]bool, error) {
	rows, err := db.Query(`SELECT produto_id, estoque_id FROM produto_estoque`)
	if err != nil {
		return nil, fmt.Errorf("falha ao consultar locais de produto: %w", err)
	}
	defer rows.Close()

	locais := make(map[string]map[string]bool)
	for rows.Next() {
		var produtoID, estoqueID string
		if err := rows.Scan(&produtoID, &estoqueID); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de local de produto: %w", err)
		}
		if locais[produtoID] == nil {
			locais[produtoID] = make(map[string]bool)
		}
		locais[produtoID][estoqueID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar locais de produto: %w", err)
	}
	return locais, nil
}

// locaisEmComumPar decide se dois conjuntos de `estoque_id` têm ao menos 1
// elemento em comum — pré-filtro PAR A PAR usado só para decidir se dois
// Produtos podem ser unidos no mesmo componente (uma condição NECESSÁRIA:
// se dois Produtos não compartilham NENHUM local entre si, eles nunca podem
// acabar no mesmo grupo final). A condição SUFICIENTE (todos os membros do
// componente compartilham >=1 local em comum) é verificada separadamente por
// interseccaoTotalNaoVazia sobre o componente inteiro, depois de formado —
// ver Design Notes de spec-6-3 sobre por que a interseção PAR A PAR sozinha
// não basta (risco de grupo "encadeado").
func locaisEmComumPar(a, b map[string]bool) bool {
	for estoqueID := range a {
		if b[estoqueID] {
			return true
		}
	}
	return false
}

// interseccaoTotalNaoVazia decide se TODOS os `membros` de um componente
// compartilham ao menos 1 `estoque_id` em comum entre si (Always, spec-6-3:
// "ao menos 1 estoque_id em comum entre TODOS os Produtos do grupo") — a
// interseção de N conjuntos, não só de pares. Design Notes de spec-6-3: esta
// é a validação FINAL que decide se um componente formado pelo pré-filtro par
// a par (locaisEmComumPar, dentro do union-find) realmente vira um grupo —
// um componente onde A∩B e B∩C não são vazios mas A∩C é (nenhum local comum
// entre TODOS os 3) falha aqui e NENHUM grupo nasce dele (mais simples que
// tentar sub-agrupar, e evita confundir a revisão humana da Story 6.4 com um
// grupo "encadeado").
func interseccaoTotalNaoVazia(membros []produtoCandidatoDuplicata, locais map[string]map[string]bool) bool {
	if len(membros) == 0 {
		return false
	}
	comuns := make(map[string]bool, len(locais[membros[0].id]))
	for estoqueID := range locais[membros[0].id] {
		comuns[estoqueID] = true
	}
	for _, m := range membros[1:] {
		atual := locais[m.id]
		for estoqueID := range comuns {
			if !atual[estoqueID] {
				delete(comuns, estoqueID)
			}
		}
		if len(comuns) == 0 {
			return false
		}
	}
	return len(comuns) > 0
}

// duplicatasUnionFind é um union-find simples (Code Map, spec-6-3) sobre
// `produtoID` — usado por DetectarDuplicatas para conectar pares de Produtos
// que passam no pré-filtro (dimensão equivalente + locaisEmComumPar) dentro
// de um mesmo balde de nome normalizado. Só entram no mapa `pai` os Produtos
// que participaram de ao menos uma união — um Produto ausente do mapa nunca
// fez par com ninguém, então nunca pode estar num grupo de tamanho >= 2.
type duplicatasUnionFind struct {
	pai map[string]string
}

func novoDuplicatasUnionFind() *duplicatasUnionFind {
	return &duplicatasUnionFind{pai: make(map[string]string)}
}

func (u *duplicatasUnionFind) raiz(x string) string {
	if _, ok := u.pai[x]; !ok {
		u.pai[x] = x
		return x
	}
	if u.pai[x] != x {
		u.pai[x] = u.raiz(u.pai[x])
	}
	return u.pai[x]
}

func (u *duplicatasUnionFind) unir(a, b string) {
	ra, rb := u.raiz(a), u.raiz(b)
	if ra != rb {
		u.pai[ra] = rb
	}
}

// DetectarDuplicatas varre TODOS os Produtos (sem paginação/teto, mesmo
// padrão de AnalisarInconsistencias) e devolve os grupos de Produtos
// candidatos a duplicata (Story 6.3, spec-6-3): nome normalizado igual +
// dimensões estruturadas equivalentes par a par + ao menos 1 `estoque_id` em
// comum entre TODOS os membros do grupo. Rota só-leitura: nenhuma escrita,
// cada chamada recalcula do zero (mesclar é Story 6.4).
//
// Algoritmo: agrupa os Produtos por nomeNormalizado; dentro de cada balde,
// testa cada PAR por dimensão equivalente + interseção de locais PAR A PAR
// não-vazia (condição necessária) e une os pares que batem via union-find;
// para cada componente conectado de tamanho >= 2, valida a interseção TOTAL
// de locais entre TODOS os membros (interseccaoTotalNaoVazia) — só então o
// componente vira um GrupoDuplicata (Design Notes, spec-6-3). Grupos
// devolvidos ordenados por (nome, id) do primeiro membro; membros de cada
// grupo também ordenados por (nome, id).
func DetectarDuplicatas(db *sql.DB) ([]GrupoDuplicata, error) {
	const q = `
		SELECT id, nome,
		       comprimento_valor, comprimento_unidade,
		       largura_valor, largura_unidade,
		       diametro_valor, diametro_unidade,
		       altura_valor, altura_unidade,
		       espessura_valor, espessura_unidade
		FROM produtos
		ORDER BY nome, id`

	rows, err := db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("falha ao consultar produtos para detecção de duplicatas: %w", err)
	}
	defer rows.Close()

	var candidatos []produtoCandidatoDuplicata
	for rows.Next() {
		var id, nome string
		var comp, larg, diam, alt, esp parDimensao
		if err := rows.Scan(
			&id, &nome,
			&comp.valor, &comp.unidade,
			&larg.valor, &larg.unidade,
			&diam.valor, &diam.unidade,
			&alt.valor, &alt.unidade,
			&esp.valor, &esp.unidade,
		); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de produto: %w", err)
		}
		candidatos = append(candidatos, produtoCandidatoDuplicata{
			id:              id,
			nome:            nome,
			nomeNormalizado: normalizarNomeProduto(nome),
			comprimento:     comp,
			largura:         larg,
			diametro:        diam,
			altura:          alt,
			espessura:       esp,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar produtos: %w", err)
	}

	locais, err := carregarLocaisProduto(db)
	if err != nil {
		return nil, err
	}

	baldes := make(map[string][]int)
	for i, c := range candidatos {
		baldes[c.nomeNormalizado] = append(baldes[c.nomeNormalizado], i)
	}

	uf := novoDuplicatasUnionFind()
	for _, indices := range baldes {
		if len(indices) < 2 {
			continue
		}
		for i := 0; i < len(indices); i++ {
			for j := i + 1; j < len(indices); j++ {
				a, b := candidatos[indices[i]], candidatos[indices[j]]
				if !dimensoesEquivalentes(a, b) {
					continue
				}
				if !locaisEmComumPar(locais[a.id], locais[b.id]) {
					continue
				}
				uf.unir(a.id, b.id)
			}
		}
	}

	componentes := make(map[string][]produtoCandidatoDuplicata)
	for _, c := range candidatos {
		if _, participou := uf.pai[c.id]; !participou {
			continue
		}
		raiz := uf.raiz(c.id)
		componentes[raiz] = append(componentes[raiz], c)
	}

	grupos := make([]GrupoDuplicata, 0)
	for _, membros := range componentes {
		if len(membros) < 2 {
			continue
		}
		if !interseccaoTotalNaoVazia(membros, locais) {
			continue
		}
		sort.Slice(membros, func(i, j int) bool {
			if membros[i].nome != membros[j].nome {
				return membros[i].nome < membros[j].nome
			}
			return membros[i].id < membros[j].id
		})
		produtos := make([]ProdutoDuplicata, 0, len(membros))
		for _, m := range membros {
			produtos = append(produtos, ProdutoDuplicata{
				ID:   m.id,
				Nome: m.nome,
				Dimensoes: DimensoesProduto{
					Comprimento: m.comprimento.paraDimensao(),
					Largura:     m.largura.paraDimensao(),
					Diametro:    m.diametro.paraDimensao(),
					Altura:      m.altura.paraDimensao(),
					Espessura:   m.espessura.paraDimensao(),
				},
			})
		}
		grupos = append(grupos, GrupoDuplicata{Produtos: produtos})
	}

	sort.Slice(grupos, func(i, j int) bool {
		pi, pj := grupos[i].Produtos[0], grupos[j].Produtos[0]
		if pi.Nome != pj.Nome {
			return pi.Nome < pj.Nome
		}
		return pi.ID < pj.ID
	})

	return grupos, nil
}
