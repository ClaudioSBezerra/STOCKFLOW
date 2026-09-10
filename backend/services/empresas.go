// Package services, arquivo empresas.go: a Empresa como fronteira de
// isolamento — Story 9.1 (Epic 9, Multi-Empresa e Plataforma), spec-9-1.
//
// Toda requisição de negócio vive sob o prefixo `/e/{slug}/api/...`; o
// middleware (middleware.RequireEmpresa, AD-19) resolve o slug UMA vez por
// requisição via BuscarEmpresaPorSlug e repassa `empresa.ID` como argumento
// explícito a cada função de service — nenhum service re-deriva a Empresa,
// nenhum handler a aceita de body/query/header (AD-8 forma 3: esta camada não
// importa net/http nem context).
//
// ProvisionarEmpresa é o ÚNICO caminho de criação de uma Empresa: grava a
// linha e copia para ela as listas padrão de Categoria e de Nomenclatura
// Guiada (as linhas semeadas pelas migrações 000010/000013, que continuam com
// `empresa_id IS NULL` e servem de molde). A Story 9.2 (criação pela UI) e a
// Story 9.4 (Empresa "Ferreira Costa") reutilizam esta função em vez de
// duplicar o provisionamento. Esta story não cria nenhuma Empresa.
package services

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

// Status possíveis de uma Empresa — espelham o CHECK da migração 000031.
const (
	StatusEmpresaAtiva   = "ativa"
	StatusEmpresaInativa = "inativa"
)

// ErrEmpresaNaoEncontrada cobre slug inexistente, Empresa `inativa` e slug
// sintaticamente inválido — os três colapsam no MESMO erro (o middleware
// responde 404 NOT_FOUND nos três, nunca 403, nunca revelando que um slug
// inativo existe).
var ErrEmpresaNaoEncontrada = errors.New("empresa não encontrada")

var (
	// ErrCNPJDuplicado indica que o CNPJ já pertence a outra Empresa da
	// plataforma (constraint `empresas_cnpj_unico`).
	ErrCNPJDuplicado = errors.New("já existe uma empresa com este CNPJ")
	// ErrSlugDuplicado indica que o slug já está em uso por outra Empresa
	// (constraint `empresas_slug_unico`).
	ErrSlugDuplicado = errors.New("já existe uma empresa com este endereço (slug)")
)

// ErroEmpresaValidacao é o erro de validação de ProvisionarEmpresa (campo
// obrigatório ausente, CNPJ inválido, slug inválido, UF/CEP fora do formato).
// A mensagem já nomeia o campo e vem pronta para exibição.
type ErroEmpresaValidacao struct {
	Mensagem string
}

func (e *ErroEmpresaValidacao) Error() string { return e.Mensagem }

// EnderecoEmpresa é o endereço cadastral da Empresa. `Complemento` é o único
// campo opcional ("" quando ausente — gravado como NULL).
type EnderecoEmpresa struct {
	Logradouro  string `json:"logradouro"`
	Numero      string `json:"numero"`
	Complemento string `json:"complemento"`
	Bairro      string `json:"bairro"`
	Cidade      string `json:"cidade"`
	CEP         string `json:"cep"`
	UF          string `json:"uf"`
}

// Empresa é a projeção de uma linha de `empresas`. `EmpresaOrigemID` é nil
// para toda Empresa "raiz" (só um Ambiente de Treinamento, Story 9.3, aponta
// para a Empresa de onde foi copiado).
type Empresa struct {
	ID              string          `json:"id"`
	NomeFantasia    string          `json:"nomeFantasia"`
	RazaoSocial     string          `json:"razaoSocial"`
	CNPJ            string          `json:"cnpj"`
	Endereco        EnderecoEmpresa `json:"endereco"`
	Slug            string          `json:"slug"`
	Status          string          `json:"status"`
	EmpresaOrigemID *string         `json:"empresaOrigemId"`
}

// DadosEmpresa é o insumo de ProvisionarEmpresa. `CNPJ` e `Slug` podem chegar
// "sujos" (máscara de CNPJ, maiúsculas/acentos no slug) — são normalizados
// por NormalizarCNPJ/NormalizarSlug antes de validar e gravar.
type DadosEmpresa struct {
	NomeFantasia    string
	RazaoSocial     string
	CNPJ            string
	Endereco        EnderecoEmpresa
	Slug            string
	EmpresaOrigemID *string
}

// NormalizarCNPJ remove tudo que não é dígito ("12.345.678/0001-95" ->
// "12345678000195"). Não valida — isso é de ValidarCNPJ.
func NormalizarCNPJ(cnpj string) string {
	var b strings.Builder
	for _, r := range cnpj {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// pesosCNPJ são os pesos dos dois dígitos verificadores do CNPJ: o primeiro
// usa os 12 últimos pesos (5..2, 9..2) sobre os 12 primeiros dígitos; o
// segundo usa os 13 (6..2, 9..2) sobre os 13 primeiros.
var pesosCNPJ = []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}

// digitoVerificadorCNPJ calcula um dígito verificador do CNPJ sobre `base`
// (12 ou 13 dígitos): resto da soma ponderada por 11; resto < 2 -> 0, senão
// 11 - resto.
func digitoVerificadorCNPJ(base string) int {
	pesos := pesosCNPJ[len(pesosCNPJ)-len(base):]
	soma := 0
	for i, r := range base {
		soma += int(r-'0') * pesos[i]
	}
	resto := soma % 11
	if resto < 2 {
		return 0
	}
	return 11 - resto
}

// ValidarCNPJ normaliza `cnpj` (NormalizarCNPJ) e exige 14 dígitos, não
// todos iguais ("00000000000000" passa no cálculo mas não é um CNPJ real), com
// os dois dígitos verificadores corretos. Qualquer falha ->
// *ErroEmpresaValidacao.
func ValidarCNPJ(cnpj string) error {
	d := NormalizarCNPJ(cnpj)
	if len(d) != 14 {
		return &ErroEmpresaValidacao{Mensagem: "CNPJ deve ter 14 dígitos"}
	}
	if strings.Count(d, d[:1]) == 14 {
		return &ErroEmpresaValidacao{Mensagem: "CNPJ inválido"}
	}
	if digitoVerificadorCNPJ(d[:12]) != int(d[12]-'0') || digitoVerificadorCNPJ(d[:13]) != int(d[13]-'0') {
		return &ErroEmpresaValidacao{Mensagem: "CNPJ inválido: dígito verificador não confere"}
	}
	return nil
}

// Limites do slug: cabe em VARCHAR(63) (migração 000031, mesmo teto de um
// rótulo DNS) e tem ao menos 2 caracteres.
const (
	slugMinRunes = 2
	slugMaxRunes = 63
)

// reSlugValido é a forma canônica de um slug: minúsculas ASCII e dígitos,
// segmentos separados por UM hífen, sem hífen nas pontas.
var reSlugValido = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// reSlugSeparadores casa qualquer sequência de caracteres fora de [a-z0-9] —
// NormalizarSlug troca cada uma por um único hífen.
var reSlugSeparadores = regexp.MustCompile(`[^a-z0-9]+`)

// NormalizarSlug reduz `s` à forma canônica de slug: minúsculas, sem acento
// (mesma tabela de normalizarNomeProduto), qualquer sequência de caracteres
// fora de [a-z0-9] vira um hífen, sem hífen nas pontas ("Ferreira Costa" ->
// "ferreira-costa"). Resultado vazio ou fora de [2,63] caracteres ->
// *ErroEmpresaValidacao.
func NormalizarSlug(s string) (string, error) {
	base := normalizarAcentosReplacer.Replace(strings.ToLower(strings.TrimSpace(s)))
	slug := strings.Trim(reSlugSeparadores.ReplaceAllString(base, "-"), "-")
	if n := utf8.RuneCountInString(slug); n < slugMinRunes || n > slugMaxRunes {
		return "", &ErroEmpresaValidacao{
			Mensagem: fmt.Sprintf("slug deve ter entre %d e %d letras ou números", slugMinRunes, slugMaxRunes),
		}
	}
	return slug, nil
}

// slugCanonico diz se `s` já está exatamente na forma canônica — usado por
// BuscarEmpresaPorSlug para recusar sem consultar o banco um slug que nunca
// poderia ter sido gravado (maiúsculas, espaço, acento, hífen duplo...).
func slugCanonico(s string) bool {
	n := len(s)
	return n >= slugMinRunes && n <= slugMaxRunes && reSlugValido.MatchString(s)
}

// colunasEmpresa é a projeção comum de BuscarEmpresaPorSlug e
// ProvisionarEmpresa (RETURNING), na ordem de scanEmpresa.
const colunasEmpresa = `id, nome_fantasia, razao_social, cnpj,
	logradouro, numero, complemento, bairro, cidade, cep, uf,
	slug, status, empresa_origem_id`

// linhaEmpresa abstrai *sql.Row para scanEmpresa.
type linhaEmpresa interface {
	Scan(dest ...any) error
}

func scanEmpresa(l linhaEmpresa) (Empresa, error) {
	var e Empresa
	var complemento, origem sql.NullString
	if err := l.Scan(
		&e.ID, &e.NomeFantasia, &e.RazaoSocial, &e.CNPJ,
		&e.Endereco.Logradouro, &e.Endereco.Numero, &complemento, &e.Endereco.Bairro,
		&e.Endereco.Cidade, &e.Endereco.CEP, &e.Endereco.UF,
		&e.Slug, &e.Status, &origem,
	); err != nil {
		return Empresa{}, err
	}
	e.Endereco.Complemento = complemento.String
	if origem.Valid {
		e.EmpresaOrigemID = &origem.String
	}
	return e, nil
}

// BuscarEmpresaPorSlug resolve a Empresa ATIVA de `slug` — chamada só pelo
// middleware (AD-19), uma vez por requisição. Slug fora da forma canônica
// (recusado sem consultar o banco), inexistente ou de Empresa `inativa` ->
// ErrEmpresaNaoEncontrada, sempre o mesmo erro.
func BuscarEmpresaPorSlug(db *sql.DB, slug string) (Empresa, error) {
	if !slugCanonico(slug) {
		return Empresa{}, ErrEmpresaNaoEncontrada
	}
	const q = `SELECT ` + colunasEmpresa + ` FROM empresas WHERE slug = $1 AND status = 'ativa'`
	e, err := scanEmpresa(db.QueryRow(q, slug))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Empresa{}, ErrEmpresaNaoEncontrada
		}
		return Empresa{}, fmt.Errorf("falha ao buscar empresa por slug: %w", err)
	}
	return e, nil
}

// campoObrigatorio devolve o valor trimado ou *ErroEmpresaValidacao nomeando
// `campo` quando vazio ou maior que `max` runes.
func campoObrigatorio(campo, valor string, max int) (string, error) {
	v := strings.TrimSpace(valor)
	if v == "" || utf8.RuneCountInString(v) > max {
		return "", &ErroEmpresaValidacao{
			Mensagem: fmt.Sprintf("%s é obrigatório e deve ter no máximo %d caracteres", campo, max),
		}
	}
	return v, nil
}

// dadosEmpresaValidados é DadosEmpresa depois de normalizado e validado —
// pronto para o INSERT.
type dadosEmpresaValidados struct {
	nomeFantasia, razaoSocial, cnpj, slug       string
	logradouro, numero, bairro, cidade, cep, uf string
	complemento                                 sql.NullString
	empresaOrigemID                             sql.NullString
}

// validarDadosEmpresa normaliza e valida todos os campos de DadosEmpresa
// ANTES de qualquer escrita.
func validarDadosEmpresa(d DadosEmpresa) (dadosEmpresaValidados, error) {
	var v dadosEmpresaValidados
	var err error
	if v.nomeFantasia, err = campoObrigatorio("nome fantasia", d.NomeFantasia, 255); err != nil {
		return v, err
	}
	if v.razaoSocial, err = campoObrigatorio("razão social", d.RazaoSocial, 255); err != nil {
		return v, err
	}
	if err := ValidarCNPJ(d.CNPJ); err != nil {
		return v, err
	}
	v.cnpj = NormalizarCNPJ(d.CNPJ)
	if v.slug, err = NormalizarSlug(d.Slug); err != nil {
		return v, err
	}
	if v.logradouro, err = campoObrigatorio("logradouro", d.Endereco.Logradouro, 255); err != nil {
		return v, err
	}
	if v.numero, err = campoObrigatorio("número", d.Endereco.Numero, 20); err != nil {
		return v, err
	}
	if v.bairro, err = campoObrigatorio("bairro", d.Endereco.Bairro, 255); err != nil {
		return v, err
	}
	if v.cidade, err = campoObrigatorio("cidade", d.Endereco.Cidade, 255); err != nil {
		return v, err
	}
	if comp := strings.TrimSpace(d.Endereco.Complemento); comp != "" {
		if utf8.RuneCountInString(comp) > 255 {
			return v, &ErroEmpresaValidacao{Mensagem: "complemento deve ter no máximo 255 caracteres"}
		}
		v.complemento = sql.NullString{String: comp, Valid: true}
	}
	v.cep = NormalizarCNPJ(d.Endereco.CEP) // mesma extração de dígitos
	if len(v.cep) != 8 {
		return v, &ErroEmpresaValidacao{Mensagem: "CEP deve ter 8 dígitos"}
	}
	v.uf = strings.ToUpper(strings.TrimSpace(d.Endereco.UF))
	if len(v.uf) != 2 || strings.Trim(v.uf, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		return v, &ErroEmpresaValidacao{Mensagem: "UF deve ter 2 letras"}
	}
	if d.EmpresaOrigemID != nil {
		v.empresaOrigemID = sql.NullString{String: *d.EmpresaOrigemID, Valid: true}
	}
	return v, nil
}

// ProvisionarEmpresa cria uma Empresa `ativa` dentro da transação `tx` do
// chamador e copia para ela as listas padrão de Categoria (25) e de
// Nomenclatura Guiada (28) — as linhas com `empresa_id IS NULL` semeadas
// pelas migrações 000010/000013. Tudo na MESMA transação do chamador: um
// provisionamento que falhe no meio nunca deixa uma Empresa sem suas listas.
//
// Validação completa ANTES de qualquer escrita (*ErroEmpresaValidacao). CNPJ
// ou slug já usados -> ErrCNPJDuplicado / ErrSlugDuplicado (colisão dos
// índices únicos — sem SELECT-antes-de-INSERT). `EmpresaOrigemID` apontando
// para uma Empresa inexistente -> *ErroEmpresaValidacao.
//
// Uma violação de unicidade dentro de `tx` deixa a transação abortada no
// Postgres: o chamador deve desfazê-la (o padrão `defer tx.Rollback()` da
// casa já cobre isso).
func ProvisionarEmpresa(tx *sql.Tx, dados DadosEmpresa) (Empresa, error) {
	v, err := validarDadosEmpresa(dados)
	if err != nil {
		return Empresa{}, err
	}

	const insertEmpresa = `
		INSERT INTO empresas (
			nome_fantasia, razao_social, cnpj,
			logradouro, numero, complemento, bairro, cidade, cep, uf,
			slug, empresa_origem_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING ` + colunasEmpresa
	e, err := scanEmpresa(tx.QueryRow(insertEmpresa,
		v.nomeFantasia, v.razaoSocial, v.cnpj,
		v.logradouro, v.numero, v.complemento, v.bairro, v.cidade, v.cep, v.uf,
		v.slug, v.empresaOrigemID,
	))
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) {
			switch {
			case pqErr.Code == pqUniqueViolation && pqErr.Constraint == "empresas_cnpj_unico":
				return Empresa{}, ErrCNPJDuplicado
			case pqErr.Code == pqUniqueViolation && pqErr.Constraint == "empresas_slug_unico":
				return Empresa{}, ErrSlugDuplicado
			case pqErr.Code == pqForeignKeyViolation || pqErr.Code == pqInvalidTextRepresentation:
				return Empresa{}, &ErroEmpresaValidacao{Mensagem: "empresa de origem não existe"}
			}
		}
		return Empresa{}, fmt.Errorf("falha ao inserir empresa: %w", err)
	}

	const copiarCategorias = `
		INSERT INTO categorias (codigo, nome, empresa_id)
		SELECT codigo, nome, $1 FROM categorias WHERE empresa_id IS NULL`
	if _, err := tx.Exec(copiarCategorias, e.ID); err != nil {
		return Empresa{}, fmt.Errorf("falha ao copiar categorias padrão para a empresa: %w", err)
	}

	const copiarTemplates = `
		INSERT INTO nomenclatura_templates (subtipo, template, empresa_id)
		SELECT subtipo, template, $1 FROM nomenclatura_templates WHERE empresa_id IS NULL`
	if _, err := tx.Exec(copiarTemplates, e.ID); err != nil {
		return Empresa{}, fmt.Errorf("falha ao copiar templates de nomenclatura padrão para a empresa: %w", err)
	}

	return e, nil
}
