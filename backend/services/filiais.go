package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lib/pq"
)

// Filiais — Story 12.1 (Epic 12, FR-51, AD-27). Filial é um nível de
// organização física dentro da Empresa (Empresa -> Filial -> Estoque); o
// isolamento continua só por Empresa (AD-20): toda query filtra `empresa_id`
// vindo do contexto, nunca do cliente. Escrita restrita a `gestor`+ (gate no
// roteamento, RequireRole — spec-filial-dados-fiscais; era `adm`+).

// Filial é a projeção devolvida por POST/PUT/GET /api/filiais. Dados fiscais
// (spec-filial-dados-fiscais) são todos opcionais: `cnpj` (14 caracteres [0-9A-Z],
// maiúsculos, sem máscara) e `inscricaoEstadual` são null quando ausentes; `endereco` é null
// quando a Filial não tem endereço (o endereço é um bloco — tudo ou nada).
type Filial struct {
	ID                string           `json:"id"`
	Nome              string           `json:"nome"`
	CNPJ              *string          `json:"cnpj"`
	InscricaoEstadual *string          `json:"inscricaoEstadual"`
	Endereco          *EnderecoEmpresa `json:"endereco"`
}

// DadosFilial é a entrada de CriarFilial (e o estado completo que
// AtualizarFilial valida). Só `Nome` é obrigatório; CNPJ/IE vazios e
// endereço todo vazio são gravados como NULL.
type DadosFilial struct {
	Nome              string          `json:"nome"`
	CNPJ              string          `json:"cnpj"`
	InscricaoEstadual string          `json:"inscricaoEstadual"`
	Endereco          EnderecoEmpresa `json:"endereco"`
}

// EnderecoFilialParcial é o endereço de um PUT parcial: campo nil (ausente
// no JSON, ou null) mantém o valor gravado; presente com "" limpa.
type EnderecoFilialParcial struct {
	Logradouro  *string `json:"logradouro"`
	Numero      *string `json:"numero"`
	Complemento *string `json:"complemento"`
	Bairro      *string `json:"bairro"`
	Cidade      *string `json:"cidade"`
	CEP         *string `json:"cep"`
	UF          *string `json:"uf"`
}

// AtualizacaoFilial é a entrada de AtualizarFilial (PUT /api/filiais/{id}):
// semântica de PATCH por campo — nil mantém o gravado, "" limpa. O resultado
// da mescla com o estado atual passa pela MESMA validação da criação (nome
// obrigatório, endereço como bloco...).
type AtualizacaoFilial struct {
	Nome              *string                `json:"nome"`
	CNPJ              *string                `json:"cnpj"`
	InscricaoEstadual *string                `json:"inscricaoEstadual"`
	Endereco          *EnderecoFilialParcial `json:"endereco"`
}

// AtualizacaoCompletaFilial devolve uma AtualizacaoFilial que regrava TODOS
// os campos com os valores de `d` (substituição completa).
func AtualizacaoCompletaFilial(d DadosFilial) AtualizacaoFilial {
	p := func(s string) *string { return &s }
	return AtualizacaoFilial{
		Nome: p(d.Nome), CNPJ: p(d.CNPJ), InscricaoEstadual: p(d.InscricaoEstadual),
		Endereco: &EnderecoFilialParcial{
			Logradouro: p(d.Endereco.Logradouro), Numero: p(d.Endereco.Numero), Complemento: p(d.Endereco.Complemento),
			Bairro: p(d.Endereco.Bairro), Cidade: p(d.Endereco.Cidade), CEP: p(d.Endereco.CEP), UF: p(d.Endereco.UF),
		},
	}
}

// aplicar mescla `a` sobre o estado atual `d`.
func (a AtualizacaoFilial) aplicar(d DadosFilial) DadosFilial {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&d.Nome, a.Nome)
	set(&d.CNPJ, a.CNPJ)
	set(&d.InscricaoEstadual, a.InscricaoEstadual)
	if e := a.Endereco; e != nil {
		set(&d.Endereco.Logradouro, e.Logradouro)
		set(&d.Endereco.Numero, e.Numero)
		set(&d.Endereco.Complemento, e.Complemento)
		set(&d.Endereco.Bairro, e.Bairro)
		set(&d.Endereco.Cidade, e.Cidade)
		set(&d.Endereco.CEP, e.CEP)
		set(&d.Endereco.UF, e.UF)
	}
	return d
}

// dadosDaFilial é o estado gravado de `f` no formato de entrada.
func dadosDaFilial(f Filial) DadosFilial {
	d := DadosFilial{Nome: f.Nome}
	if f.CNPJ != nil {
		d.CNPJ = *f.CNPJ
	}
	if f.InscricaoEstadual != nil {
		d.InscricaoEstadual = *f.InscricaoEstadual
	}
	if f.Endereco != nil {
		d.Endereco = *f.Endereco
	}
	return d
}

var (
	// ErrFilialValidacao: nome de Filial vazio após o trim, com mais de 255
	// runes ou com byte inválido. Mapeado para 400 VALIDATION_ERROR.
	ErrFilialValidacao = errors.New("o nome da filial é obrigatório e deve ter no máximo 255 caracteres")
	// ErrNomeFilialDuplicado: o nome normalizado já existe na Empresa
	// (colisão do índice único, 23505). Mapeado para 409 CONFLICT.
	ErrNomeFilialDuplicado = errors.New("já existe uma filial com esse nome")
	// ErrCNPJFilialDuplicado: outra Filial da mesma Empresa já tem esse CNPJ
	// (índice único parcial `idx_filiais_empresa_cnpj`). 409 CONFLICT.
	ErrCNPJFilialDuplicado = errors.New("já existe uma filial com esse CNPJ")
	// ErrFilialNaoEncontrada: id malformado, inexistente ou de OUTRA Empresa
	// na edição — colapsados no mesmo erro. 404 NOT_FOUND.
	ErrFilialNaoEncontrada = errors.New("filial não encontrada")
	// ErrFilialInvalida: `filial_id` ausente, malformado ou de outra Empresa
	// ao criar um Estoque. Mapeado para 400 VALIDATION_ERROR.
	ErrFilialInvalida = errors.New("a filial é obrigatória e deve pertencer à empresa")
	// ErrEmpresaSemFilial: a Empresa não tem nenhuma Filial (legada, antes da
	// Story 12.2) e um Estoque precisaria ser criado na Filial padrão.
	ErrEmpresaSemFilial = errors.New("a empresa não possui nenhuma filial cadastrada")
)

// ErroDadosFilial é o erro de validação dos dados fiscais/endereço da Filial
// (CNPJ inválido, endereço incompleto, CEP/UF fora do formato, IE longa). A
// mensagem já nomeia o(s) campo(s). Mapeado para 400 VALIDATION_ERROR.
type ErroDadosFilial struct {
	Mensagem string
}

func (e *ErroDadosFilial) Error() string { return e.Mensagem }

// idxFiliaisEmpresaCNPJ é o índice único parcial da migration 000061.
const idxFiliaisEmpresaCNPJ = "idx_filiais_empresa_cnpj"

// ufsBrasil são as 27 Unidades Federativas aceitas em `uf`.
var ufsBrasil = map[string]bool{
	"AC": true, "AL": true, "AP": true, "AM": true, "BA": true, "CE": true, "DF": true,
	"ES": true, "GO": true, "MA": true, "MT": true, "MS": true, "MG": true, "PA": true,
	"PB": true, "PR": true, "PE": true, "PI": true, "RJ": true, "RN": true, "RS": true,
	"RO": true, "RR": true, "SC": true, "SP": true, "SE": true, "TO": true,
}

// dadosFilialValidados é DadosFilial normalizado, pronto para a escrita
// (NullString inválido = NULL).
type dadosFilialValidados struct {
	nome                                                     string
	cnpj, ie                                                 sql.NullString
	logradouro, numero, complemento, bairro, cidade, cep, uf sql.NullString
}

func textoNulo(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// reCEP: 8 dígitos, com ou sem hífen depois do 5º ("50780-000", "50780000").
var reCEP = regexp.MustCompile(`^\d{5}-?\d{3}$`)

// normalizarCEP aceita só reCEP (após o trim) e devolve os 8 dígitos. Letras
// ou qualquer outro caractere -> erro (nunca descarta caracteres em silêncio).
func normalizarCEP(cep string) (string, error) {
	c := strings.TrimSpace(cep)
	if !reCEP.MatchString(c) {
		return "", &ErroDadosFilial{Mensagem: "CEP deve ter 8 dígitos (ex.: 50780-000)"}
	}
	return strings.ReplaceAll(c, "-", ""), nil
}

// temCaractereDeControle: quebra de linha, tabulação, NUL etc.
func temCaractereDeControle(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) >= 0
}

// raizCNPJ: os 8 primeiros caracteres alfanuméricos (identificam a empresa; os 4 seguintes, o
// estabelecimento).
func raizCNPJ(cnpj string) string {
	d := NormalizarCNPJ(cnpj)
	if len(d) < 8 {
		return d
	}
	return d[:8]
}

// validarDadosFilial normaliza e valida `d` ANTES de qualquer escrita. Nome:
// mesma regra de sempre (ErrFilialValidacao). CNPJ: ValidarCNPJ quando
// informado, gravado normalizado (maiúsculo, sem máscara), e com a MESMA raiz (8 primeiros caracteres)
// do CNPJ da Empresa `cnpjEmpresa`. IE: texto livre até 20 caracteres.
// Endereço: bloco — se qualquer campo vier preenchido, logradouro, número,
// bairro, cidade, CEP (8 dígitos) e UF (uma das 27) passam a ser obrigatórios;
// complemento é sempre opcional. Caracteres de controle (\n, \t...) em
// qualquer campo -> erro.
func validarDadosFilial(d DadosFilial, cnpjEmpresa string) (dadosFilialValidados, error) {
	var v dadosFilialValidados
	v.nome = strings.TrimSpace(d.Nome)
	if v.nome == "" || strings.ContainsRune(v.nome, 0) || utf8.RuneCountInString(v.nome) > 255 {
		return v, ErrFilialValidacao
	}
	campos := []struct{ rotulo, valor string }{
		{"nome", d.Nome}, {"CNPJ", d.CNPJ}, {"inscrição estadual", d.InscricaoEstadual},
		{"logradouro", d.Endereco.Logradouro}, {"número", d.Endereco.Numero},
		{"complemento", d.Endereco.Complemento}, {"bairro", d.Endereco.Bairro},
		{"cidade", d.Endereco.Cidade}, {"CEP", d.Endereco.CEP}, {"UF", d.Endereco.UF},
	}
	for _, c := range campos {
		if !utf8.ValidString(c.valor) {
			return v, &ErroDadosFilial{Mensagem: c.rotulo + " contém caracteres inválidos"}
		}
		if temCaractereDeControle(c.valor) {
			return v, &ErroDadosFilial{Mensagem: c.rotulo + " não pode conter quebras de linha, tabulações ou outros caracteres de controle"}
		}
	}

	if cnpj := strings.TrimSpace(d.CNPJ); cnpj != "" {
		if err := ValidarCNPJ(cnpj); err != nil {
			return v, &ErroDadosFilial{Mensagem: err.Error()}
		}
		n := NormalizarCNPJ(cnpj)
		if raiz := raizCNPJ(cnpjEmpresa); n[:8] != raiz {
			return v, &ErroDadosFilial{Mensagem: fmt.Sprintf(
				"o CNPJ da filial deve ser da mesma empresa: os 8 primeiros caracteres (%s) têm que ser iguais aos do CNPJ da empresa (%s)",
				n[:2]+"."+n[2:5]+"."+n[5:8], formatarRaizCNPJ(raiz))}
		}
		v.cnpj = textoNulo(n)
	}
	ie := strings.TrimSpace(d.InscricaoEstadual)
	if utf8.RuneCountInString(ie) > 20 {
		return v, &ErroDadosFilial{Mensagem: "inscrição estadual deve ter no máximo 20 caracteres"}
	}
	v.ie = textoNulo(ie)

	e := EnderecoEmpresa{
		Logradouro:  strings.TrimSpace(d.Endereco.Logradouro),
		Numero:      strings.TrimSpace(d.Endereco.Numero),
		Complemento: strings.TrimSpace(d.Endereco.Complemento),
		Bairro:      strings.TrimSpace(d.Endereco.Bairro),
		Cidade:      strings.TrimSpace(d.Endereco.Cidade),
		CEP:         strings.TrimSpace(d.Endereco.CEP),
		UF:          strings.ToUpper(strings.TrimSpace(d.Endereco.UF)),
	}
	if e == (EnderecoEmpresa{}) {
		return v, nil // sem endereço
	}
	obrigatorios := []struct{ campo, valor string }{
		{"logradouro", e.Logradouro}, {"número", e.Numero}, {"bairro", e.Bairro},
		{"cidade", e.Cidade}, {"CEP", e.CEP}, {"UF", e.UF},
	}
	var faltando []string
	for _, o := range obrigatorios {
		if o.valor == "" {
			faltando = append(faltando, o.campo)
		}
	}
	if len(faltando) > 0 {
		return v, &ErroDadosFilial{Mensagem: "endereço incompleto: preencha também " + strings.Join(faltando, ", ") +
			" (logradouro, número, bairro, cidade, CEP e UF são obrigatórios quando há endereço; complemento é opcional)"}
	}
	limites := []struct {
		campo, valor string
		max          int
	}{
		{"logradouro", e.Logradouro, 255}, {"número", e.Numero, 20}, {"complemento", e.Complemento, 255},
		{"bairro", e.Bairro, 255}, {"cidade", e.Cidade, 255},
	}
	for _, l := range limites {
		if utf8.RuneCountInString(l.valor) > l.max {
			return v, &ErroDadosFilial{Mensagem: fmt.Sprintf("%s deve ter no máximo %d caracteres", l.campo, l.max)}
		}
	}
	cep, err := normalizarCEP(e.CEP)
	if err != nil {
		return v, err
	}
	if !ufsBrasil[e.UF] {
		return v, &ErroDadosFilial{Mensagem: "UF deve ser a sigla de um estado brasileiro (ex.: PE)"}
	}
	v.logradouro, v.numero, v.complemento = textoNulo(e.Logradouro), textoNulo(e.Numero), textoNulo(e.Complemento)
	v.bairro, v.cidade, v.cep, v.uf = textoNulo(e.Bairro), textoNulo(e.Cidade), textoNulo(cep), textoNulo(e.UF)
	return v, nil
}

// formatarRaizCNPJ: "38149716" -> "38.149.716".
func formatarRaizCNPJ(raiz string) string {
	if len(raiz) != 8 {
		return raiz
	}
	return raiz[:2] + "." + raiz[2:5] + "." + raiz[5:8]
}

// cnpjDaEmpresa lê o CNPJ da Empresa (raiz exigida no CNPJ das Filiais).
func cnpjDaEmpresa(q queryRower, empresaID string) (string, error) {
	var cnpj string
	if err := q.QueryRow(`SELECT cnpj FROM empresas WHERE id = $1`, empresaID).Scan(&cnpj); err != nil {
		return "", fmt.Errorf("falha ao ler CNPJ da empresa: %w", err)
	}
	return strings.TrimSpace(cnpj), nil
}

// colunasFilial é a projeção lida por scanFilial (mesma ordem).
const colunasFilial = `id, nome, cnpj, inscricao_estadual, logradouro, numero, complemento, bairro, cidade, cep, uf`

type filialScanner interface {
	Scan(dest ...any) error
}

func scanFilial(s filialScanner) (Filial, error) {
	var f Filial
	var cnpj, ie, logradouro, numero, complemento, bairro, cidade, cep, uf sql.NullString
	if err := s.Scan(&f.ID, &f.Nome, &cnpj, &ie, &logradouro, &numero, &complemento, &bairro, &cidade, &cep, &uf); err != nil {
		return Filial{}, err
	}
	if cnpj.Valid {
		c := strings.TrimSpace(cnpj.String)
		f.CNPJ = &c
	}
	if ie.Valid {
		i := ie.String
		f.InscricaoEstadual = &i
	}
	if logradouro.Valid || numero.Valid || bairro.Valid || cidade.Valid || cep.Valid || uf.Valid {
		f.Endereco = &EnderecoEmpresa{
			Logradouro: logradouro.String, Numero: numero.String, Complemento: complemento.String,
			Bairro: bairro.String, Cidade: cidade.String, CEP: strings.TrimSpace(cep.String), UF: strings.TrimSpace(uf.String),
		}
	}
	return f, nil
}

// erroEscritaFilial traduz o erro do Postgres numa escrita de Filial.
func erroEscritaFilial(err error, operacao string) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch pqErr.Code {
		case pqUniqueViolation:
			if pqErr.Constraint == idxFiliaisEmpresaCNPJ {
				return ErrCNPJFilialDuplicado
			}
			return ErrNomeFilialDuplicado
		case pqStringDataRightTruncation, pqInvalidByteSequence:
			return ErrFilialValidacao
		}
	}
	return fmt.Errorf("falha ao %s filial: %w", operacao, err)
}

// CriarFilial insere uma Filial na Empresa `empresaID`. A unicidade de nome e
// de CNPJ por Empresa vem dos índices únicos (sem SELECT prévio, sem janela
// de corrida).
func CriarFilial(db *sql.DB, empresaID string, d DadosFilial) (Filial, error) {
	cnpjEmpresa, err := cnpjDaEmpresa(db, empresaID)
	if err != nil {
		return Filial{}, err
	}
	v, err := validarDadosFilial(d, cnpjEmpresa)
	if err != nil {
		return Filial{}, err
	}
	insert := `INSERT INTO filiais (empresa_id, nome, cnpj, inscricao_estadual, logradouro, numero, complemento, bairro, cidade, cep, uf)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING ` + colunasFilial
	f, err := scanFilial(db.QueryRow(insert, empresaID, v.nome, v.cnpj, v.ie,
		v.logradouro, v.numero, v.complemento, v.bairro, v.cidade, v.cep, v.uf))
	if err != nil {
		return Filial{}, erroEscritaFilial(err, "inserir")
	}
	return f, nil
}

// AtualizarFilial aplica `a` à Filial `id` da Empresa `empresaID` — campo
// nil mantém o valor gravado, "" limpa — e valida o resultado inteiro antes
// de gravar. Leitura (FOR UPDATE) e escrita na mesma transação. Id
// malformado, inexistente ou de outra Empresa -> ErrFilialNaoEncontrada.
func AtualizarFilial(db *sql.DB, empresaID, id string, a AtualizacaoFilial) (Filial, error) {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return Filial{}, fmt.Errorf("falha ao iniciar edição da filial: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	atual, err := scanFilial(tx.QueryRow(
		`SELECT `+colunasFilial+` FROM filiais WHERE id = $1 AND empresa_id = $2 FOR UPDATE`, id, empresaID))
	if err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return Filial{}, ErrFilialNaoEncontrada
		}
		return Filial{}, fmt.Errorf("falha ao ler filial: %w", err)
	}
	cnpjEmpresa, err := cnpjDaEmpresa(tx, empresaID)
	if err != nil {
		return Filial{}, err
	}
	v, err := validarDadosFilial(a.aplicar(dadosDaFilial(atual)), cnpjEmpresa)
	if err != nil {
		return Filial{}, err
	}
	update := `UPDATE filiais SET nome = $3, cnpj = $4, inscricao_estadual = $5, logradouro = $6, numero = $7,
		complemento = $8, bairro = $9, cidade = $10, cep = $11, uf = $12
		WHERE id = $1 AND empresa_id = $2 RETURNING ` + colunasFilial
	f, err := scanFilial(tx.QueryRow(update, id, empresaID, v.nome, v.cnpj, v.ie,
		v.logradouro, v.numero, v.complemento, v.bairro, v.cidade, v.cep, v.uf))
	if err != nil {
		return Filial{}, erroEscritaFilial(err, "atualizar")
	}
	if err := tx.Commit(); err != nil {
		return Filial{}, erroEscritaFilial(err, "atualizar")
	}
	return f, nil
}

// ListarFiliais devolve as Filiais da Empresa `empresaID` ordenadas por nome
// normalizado. Lista vazia não é erro — devolve um slice vazio, nunca nil.
func ListarFiliais(db *sql.DB, empresaID string) ([]Filial, error) {
	rows, err := db.Query(
		`SELECT `+colunasFilial+` FROM filiais WHERE empresa_id = $1 ORDER BY nome_normalizado ASC, id`,
		empresaID)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar filiais: %w", err)
	}
	defer rows.Close()

	filiais := make([]Filial, 0)
	for rows.Next() {
		f, err := scanFilial(rows)
		if err != nil {
			return nil, fmt.Errorf("falha ao ler linha de filial: %w", err)
		}
		filiais = append(filiais, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar filiais: %w", err)
	}
	return filiais, nil
}

// queryRower é satisfeito por *sql.DB e *sql.Tx.
type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

// filialPadraoDaEmpresa devolve o id da Filial padrão da Empresa: a mais
// antiga (`ORDER BY criado_em, id`). Empresa sem nenhuma Filial ->
// ErrEmpresaSemFilial (nunca cria Estoque órfão).
func filialPadraoDaEmpresa(q queryRower, empresaID string) (string, error) {
	var id string
	err := q.QueryRow(
		`SELECT id FROM filiais WHERE empresa_id = $1 ORDER BY criado_em, id LIMIT 1`,
		empresaID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrEmpresaSemFilial
	}
	if err != nil {
		return "", fmt.Errorf("falha ao buscar filial padrão da empresa: %w", err)
	}
	return id, nil
}
