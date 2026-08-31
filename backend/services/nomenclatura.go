package services

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// NomenclaturaTemplate é a projeção somente-leitura de uma linha de
// `nomenclatura_templates` (Story 3.2, FR-9), devolvida por
// GET /api/nomenclatura-templates e usada para popular o `<Select>` de
// template (opcional) no formulário de cadastro de Produto.
type NomenclaturaTemplate struct {
	ID       string `json:"id"`
	Subtipo  string `json:"subtipo"`
	Template string `json:"template"`
}

// tokenTemplate casa cada placeholder `[ENTRE COLCHETES]` de um template de
// Nomenclatura Guiada, ex. `[TIPO]`, `[SEÇÃO]` — usado por
// nomeValidoParaTemplate para localizar os pontos onde `nome` deve preencher
// texto livre.
var tokenTemplate = regexp.MustCompile(`\[[^\]]+\]`)

// nomeValidoParaTemplate valida `nome` contra o formato de `templateTexto`
// (Story 3.2, AC1/AC2): extrai os placeholders `[X]` do template mantendo a
// ordem, constrói um padrão âncorado (`^...$`) escapando (regexp.QuoteMeta)
// cada trecho literal entre placeholders e substituindo cada placeholder por
// um grupo de captura não-guloso `(.+?)`, e casa `nome` contra esse padrão
// inteiro.
//
// Comparação do esqueleto literal (o texto fixo do template, ex. "CABO",
// "PVC") é CASE-SENSITIVE — decisão de implementação registrada em Design
// Notes da spec-3-2; nenhuma AC testa variação de caixa. Um template sem
// nenhum placeholder exige que `nome` seja idêntico ao template (casamento
// exato, sem grupos a validar).
//
// Inválido quando: `nome` não casa com o padrão inteiro (placeholder
// faltando, estrutura fora de ordem, ou texto fixo divergente), OU algum
// grupo capturado (o texto que preencheu um placeholder) fica vazio/só
// espaço após trim — um placeholder "preenchido" só com espaços não conta
// como preenchido.
func nomeValidoParaTemplate(templateTexto, nome string) bool {
	locs := tokenTemplate.FindAllStringIndex(templateTexto, -1)

	var padrao strings.Builder
	// `(?s)` faz `.` casar também `\n` — sem essa flag, um `nome` com quebra de
	// linha embutida (aceito pela validação básica de CriarProduto, que não
	// proíbe isso) seria rejeitado como "não corresponde ao template" mesmo
	// preenchendo os placeholders corretamente, já que RE2 (Go regexp) por
	// padrão não casa `\n` com `.`.
	padrao.WriteString("(?s)^")
	ultimo := 0
	for _, loc := range locs {
		padrao.WriteString(regexp.QuoteMeta(templateTexto[ultimo:loc[0]]))
		padrao.WriteString("(.+?)")
		ultimo = loc[1]
	}
	padrao.WriteString(regexp.QuoteMeta(templateTexto[ultimo:]))
	padrao.WriteString("$")

	re, err := regexp.Compile(padrao.String())
	if err != nil {
		return false
	}
	grupos := re.FindStringSubmatch(nome)
	if grupos == nil {
		return false
	}
	for _, g := range grupos[1:] {
		if strings.TrimSpace(g) == "" {
			return false
		}
	}
	return true
}

// ListarNomenclaturaTemplates devolve os 28 templates fixos de Nomenclatura
// Guiada (addendum §G), ordenados por `subtipo` ascendente — a lista da qual
// o formulário de cadastro seleciona (opcional), molde direto de
// ListarCategorias (produtos.go).
func ListarNomenclaturaTemplates(db *sql.DB) ([]NomenclaturaTemplate, error) {
	rows, err := db.Query(`SELECT id, subtipo, template FROM nomenclatura_templates ORDER BY subtipo ASC`)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar templates de nomenclatura: %w", err)
	}
	defer rows.Close()

	templates := make([]NomenclaturaTemplate, 0)
	for rows.Next() {
		var t NomenclaturaTemplate
		if err := rows.Scan(&t.ID, &t.Subtipo, &t.Template); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de template de nomenclatura: %w", err)
		}
		templates = append(templates, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar templates de nomenclatura: %w", err)
	}
	return templates, nil
}
