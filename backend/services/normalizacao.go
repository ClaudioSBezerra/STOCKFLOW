// Package services, arquivo normalizacao.go: núcleo da Detecção de
// inconsistências dimensionais (Story 6.1, spec-6-1, Epic 6 — Normalização de
// Dados) e da Aplicação seletiva de correções (Story 6.2, spec-6-2).
// AnalisarInconsistencias varre `produtos` sob demanda e devolve uma lista de
// sugestões (produto, campo, valor sugerido, origem) — nenhuma escrita.
// AplicarCorrecoes/IgnorarSugestao (Story 6.2) são as duas únicas escritas
// deste arquivo: a primeira grava o valor sugerido nos campos ainda vazios
// (guard `IS NULL`, nunca sobrescreve); a segunda grava a tupla exata
// (produto, campo, valor, unidade) em `normalizacao_ignoradas`, e
// AnalisarInconsistencias passa a excluir da lista qualquer sugestão cuja
// tupla já esteja lá (ver carregarIgnoradas/chaveIgnorada abaixo).
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
