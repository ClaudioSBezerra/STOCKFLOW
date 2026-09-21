package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

// CRUD de Templates de Nomenclatura — Story 10.6 (Epic 10, AD-33). Escrita
// restrita a `adm`+ (gate no roteamento, RequireRole); toda query filtra
// `empresa_id` (AD-20). A listagem continua em ListarNomenclaturaTemplates
// (nomenclatura.go). Molde: categorias.go (Story 10.5).

const (
	// templateSubtipoMax e templateTextoMax espelham VARCHAR(255) de
	// `nomenclatura_templates` (migration 000013).
	templateSubtipoMax = 255
	templateTextoMax   = 255

	idxNomenclaturaTemplatesEmpresaSubtipo = "idx_nomenclatura_templates_empresa_subtipo"
)

var (
	// ErrTemplateValidacao: `subtipo`/`template` vazio, acima de 255 runes, com
	// byte NUL, ou `template` sem estrutura válida de tokens. Mapeado para 400.
	ErrTemplateValidacao = errors.New("o subtipo e o template são obrigatórios (até 255 caracteres); o template deve ser [NOME LIVRE] ou conter ao menos um campo entre colchetes, como [TIPO], sem colchetes soltos")
	// ErrTemplateNaoEncontrado: `id` inexistente, de outra Empresa, não-UUID
	// (22P02) ou com byte NUL — todos colapsam aqui. Mapeado para 404.
	ErrTemplateNaoEncontrado = errors.New("template não encontrado")
	// ErrTemplateDuplicado: `subtipo` já existe na Empresa (23505 em
	// idx_nomenclatura_templates_empresa_subtipo). Mapeado para 409.
	ErrTemplateDuplicado = errors.New("já existe um template com esse subtipo")
	// ErrTemplateFallbackObrigatorio: a operação removeria o ÚNICO template
	// `[NOME LIVRE]` (fallback universal, AD-34) da Empresa. Mapeado para 409.
	ErrTemplateFallbackObrigatorio = errors.New("o template Genérico ([NOME LIVRE]) é o fallback obrigatório da empresa: não pode ser excluído nem ter a estrutura alterada enquanto for o único")
)

// ErroTemplateEmUso indica que a exclusão foi bloqueada por Produtos que
// referenciam o Template. Mapeado para 409 CONFLICT.
type ErroTemplateEmUso struct {
	Produtos int
}

func (e *ErroTemplateEmUso) Error() string {
	if e.Produtos == 1 {
		return "o template não pode ser excluído: está em uso por 1 produto"
	}
	return fmt.Sprintf("o template não pode ser excluído: está em uso por %d produtos", e.Produtos)
}

// validarTemplateNomenclatura trima e valida `subtipo`/`template`. O template
// deve ser o marcador `[NOME LIVRE]` OU conter ao menos um token `[...]`, cada
// token com texto não-branco e sem `[` interno, sem `[`/`]` soltos fora deles.
func validarTemplateNomenclatura(subtipo, template string) (string, string, error) {
	subtipo = strings.TrimSpace(subtipo)
	template = strings.TrimSpace(template)
	if subtipo == "" || template == "" ||
		strings.ContainsRune(subtipo, 0) || strings.ContainsRune(template, 0) ||
		utf8.RuneCountInString(subtipo) > templateSubtipoMax ||
		utf8.RuneCountInString(template) > templateTextoMax {
		return "", "", ErrTemplateValidacao
	}
	if template == TemplateGenericoMarcador {
		return subtipo, template, nil
	}
	tokens := tokenTemplate.FindAllString(template, -1)
	if len(tokens) == 0 {
		return "", "", ErrTemplateValidacao
	}
	for _, tok := range tokens {
		interno := tok[1 : len(tok)-1]
		if strings.TrimSpace(interno) == "" || strings.Contains(interno, "[") {
			return "", "", ErrTemplateValidacao
		}
	}
	if resto := tokenTemplate.ReplaceAllString(template, ""); strings.ContainsAny(resto, "[]") {
		return "", "", ErrTemplateValidacao
	}
	return subtipo, template, nil
}

// traduzirErroEscritaTemplate converte erros do Postgres nos erros de
// domínio. Devolve nil quando `err` não é um erro conhecido.
func traduzirErroEscritaTemplate(err error) error {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return nil
	}
	switch pqErr.Code {
	case pqUniqueViolation:
		if pqErr.Constraint == idxNomenclaturaTemplatesEmpresaSubtipo {
			return ErrTemplateDuplicado
		}
		// Outra violação de unicidade segue como erro inesperado (500).
		return nil
	case pqStringDataRightTruncation, pqCheckViolation, pqInvalidByteSequence:
		return ErrTemplateValidacao
	case pqInvalidTextRepresentation:
		return ErrTemplateNaoEncontrado
	}
	return nil
}

// CriarNomenclaturaTemplate insere um Template na Empresa `empresaID`.
// Unicidade de `subtipo` por Empresa vem do índice único (sem SELECT prévio).
func CriarNomenclaturaTemplate(db *sql.DB, empresaID, subtipo, template string) (NomenclaturaTemplate, error) {
	subtipo, template, err := validarTemplateNomenclatura(subtipo, template)
	if err != nil {
		return NomenclaturaTemplate{}, err
	}
	var t NomenclaturaTemplate
	const insert = `INSERT INTO nomenclatura_templates (subtipo, template, empresa_id) VALUES ($1, $2, $3)
		RETURNING id, subtipo, template`
	if err := db.QueryRow(insert, subtipo, template, empresaID).Scan(&t.ID, &t.Subtipo, &t.Template); err != nil {
		if te := traduzirErroEscritaTemplate(err); te != nil {
			return NomenclaturaTemplate{}, te
		}
		return NomenclaturaTemplate{}, fmt.Errorf("falha ao inserir template de nomenclatura: %w", err)
	}
	return t, nil
}

// travarTemplateEMarcadores trava, numa única instrução e em ordem de `id`
// (evita deadlock entre duas operações concorrentes sobre marcadores
// distintos), a linha-alvo `id` e todas as linhas-marcador `[NOME LIVRE]` da
// Empresa. Devolve o texto do alvo e quantos templates-marcador a Empresa tem.
// Alvo ausente/alheio/malformado -> ErrTemplateNaoEncontrado.
func travarTemplateEMarcadores(tx *sql.Tx, empresaID, id string) (textoAlvo string, marcadores int, err error) {
	rows, err := tx.Query(
		`SELECT template, (id = $2) AS alvo FROM nomenclatura_templates
		 WHERE empresa_id = $1 AND (id = $2 OR template = $3)
		 ORDER BY id FOR UPDATE`,
		empresaID, id, TemplateGenericoMarcador)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return "", 0, ErrTemplateNaoEncontrado
		}
		return "", 0, fmt.Errorf("falha ao travar template de nomenclatura: %w", err)
	}
	defer rows.Close()

	achou := false
	for rows.Next() {
		var texto string
		var alvo bool
		if err := rows.Scan(&texto, &alvo); err != nil {
			return "", 0, fmt.Errorf("falha ao ler template de nomenclatura: %w", err)
		}
		if texto == TemplateGenericoMarcador {
			marcadores++
		}
		if alvo {
			achou = true
			textoAlvo = texto
		}
	}
	if err := rows.Err(); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return "", 0, ErrTemplateNaoEncontrado
		}
		return "", 0, fmt.Errorf("falha ao iterar templates de nomenclatura: %w", err)
	}
	if !achou {
		return "", 0, ErrTemplateNaoEncontrado
	}
	return textoAlvo, marcadores, nil
}

// AtualizarNomenclaturaTemplate altera `subtipo`/`template` do Template `id`
// DENTRO da Empresa `empresaID`. NUNCA toca `produtos`: a regra nova vale só
// no próximo cadastro/renomeação (ambos leem `template` na hora da ação).
// Trocar o texto do ÚNICO template-marcador por outro texto ->
// ErrTemplateFallbackObrigatorio (renomear só o `subtipo` é permitido).
func AtualizarNomenclaturaTemplate(db *sql.DB, empresaID, id, subtipo, template string) (NomenclaturaTemplate, error) {
	subtipo, template, err := validarTemplateNomenclatura(subtipo, template)
	if err != nil {
		return NomenclaturaTemplate{}, err
	}
	if strings.ContainsRune(id, 0) {
		return NomenclaturaTemplate{}, ErrTemplateNaoEncontrado
	}
	tx, err := db.Begin()
	if err != nil {
		return NomenclaturaTemplate{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	textoAtual, marcadores, err := travarTemplateEMarcadores(tx, empresaID, id)
	if err != nil {
		return NomenclaturaTemplate{}, err
	}
	if textoAtual == TemplateGenericoMarcador && template != TemplateGenericoMarcador && marcadores <= 1 {
		return NomenclaturaTemplate{}, ErrTemplateFallbackObrigatorio
	}

	var t NomenclaturaTemplate
	const update = `UPDATE nomenclatura_templates SET subtipo = $1, template = $2
		WHERE id = $3 AND empresa_id = $4 RETURNING id, subtipo, template`
	if err := tx.QueryRow(update, subtipo, template, id, empresaID).Scan(&t.ID, &t.Subtipo, &t.Template); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NomenclaturaTemplate{}, ErrTemplateNaoEncontrado
		}
		if te := traduzirErroEscritaTemplate(err); te != nil {
			return NomenclaturaTemplate{}, te
		}
		return NomenclaturaTemplate{}, fmt.Errorf("falha ao atualizar template de nomenclatura: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return NomenclaturaTemplate{}, fmt.Errorf("falha ao confirmar atualização do template: %w", err)
	}
	return t, nil
}

// ExcluirNomenclaturaTemplate remove o Template `id` da Empresa `empresaID`,
// desde que (1) não seja o único fallback `[NOME LIVRE]` e (2) nenhum Produto
// o referencie. Molde de ExcluirCategoria: transação com FOR UPDATE (um
// CriarProduto concorrente precisa de KEY SHARE sobre a linha para a FK e
// fica bloqueado até o fim desta transação), contagem e só então DELETE. A
// checagem do fallback vem ANTES da de "em uso".
func ExcluirNomenclaturaTemplate(db *sql.DB, empresaID, id string) error {
	if strings.ContainsRune(id, 0) {
		return ErrTemplateNaoEncontrado
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	textoAtual, marcadores, err := travarTemplateEMarcadores(tx, empresaID, id)
	if err != nil {
		return err
	}
	if textoAtual == TemplateGenericoMarcador && marcadores <= 1 {
		return ErrTemplateFallbackObrigatorio
	}

	var emUso int
	if err := tx.QueryRow(`SELECT count(*) FROM produtos WHERE template_id = $1`, id).Scan(&emUso); err != nil {
		return fmt.Errorf("falha ao contar produtos do template: %w", err)
	}
	if emUso > 0 {
		return &ErroTemplateEmUso{Produtos: emUso}
	}

	res, err := tx.Exec(`DELETE FROM nomenclatura_templates WHERE id = $1 AND empresa_id = $2`, id, empresaID)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqForeignKeyViolation {
			return &ErroTemplateEmUso{Produtos: 1}
		}
		return fmt.Errorf("falha ao excluir template de nomenclatura: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("falha ao confirmar linhas excluídas do template: %w", err)
	}
	if n == 0 {
		return ErrTemplateNaoEncontrado
	}
	if err := tx.Commit(); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqForeignKeyViolation {
			return &ErroTemplateEmUso{Produtos: 1}
		}
		return fmt.Errorf("falha ao confirmar exclusão do template: %w", err)
	}
	return nil
}
