// Package services, arquivo normalizacao.go: núcleo da Detecção de
// inconsistências dimensionais (Story 6.1, spec-6-1, Epic 6 — Normalização de
// Dados). AnalisarInconsistencias varre `produtos` sob demanda e devolve uma
// lista de sugestões (produto, campo, valor sugerido, origem) — NENHUMA
// escrita, aqui ou em qualquer chamador: aplicar/ignorar é Story 6.2.
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
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
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
	return sugestoes, nil
}
