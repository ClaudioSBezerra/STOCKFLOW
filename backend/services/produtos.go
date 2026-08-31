package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

// pqForeignKeyViolation é o SQLSTATE do Postgres para violação de chave
// estrangeira — usado por CriarProduto quando `categoria_id` ou `estoque_id`
// não correspondem a nenhuma linha existente (Story 3.1).
const pqForeignKeyViolation = "23503"

// unidadesDimensaoValidas é o conjunto fechado de unidades aceitas para as 5
// dimensões físicas do Produto (comprimento, largura, diâmetro, altura,
// espessura) — AD-9. `{mm,cm,m}` replica os exemplos do sistema legado
// ("6m", "100mm"); nenhuma AC desta story testa um valor fora deste conjunto.
var unidadesDimensaoValidas = map[string]bool{"mm": true, "cm": true, "m": true}

// limiteNumeric103 é a magnitude máxima representável numa coluna
// `NUMERIC(10,3)` (10 dígitos totais, 3 depois da vírgula -> até 7 dígitos
// antes dela): `comprimento_valor`/`largura_valor`/.../`quantidade` em
// `produtos`/`produto_estoque` são todas desse tipo. Validado ANTES do
// INSERT para que um valor fora da faixa vire 400 VALIDATION_ERROR — sem
// este limite, o Postgres rejeitaria com "numeric field overflow", um erro
// não mapeado que cairia no 500 genérico como qualquer erro de banco
// inesperado.
const limiteNumeric103 = 9999999.999

// limiteNumeric103Texto é a representação textual exata de limiteNumeric103
// para as mensagens de validação — `%g`/`%v` formatam esse float em notação
// científica (`9.999999999e+06`), ilegível para quem lê o erro.
const limiteNumeric103Texto = "9999999.999"

// Produto é a projeção mínima devolvida por POST /api/produtos (Story 3.1):
// `id` + `nome`, nada mais — a resposta de cadastro não precisa ecoar o
// restante do payload.
type Produto struct {
	ID   string `json:"id"`
	Nome string `json:"nome"`
}

// Categoria é a projeção somente-leitura de uma linha de `categorias`,
// devolvida por GET /api/categorias e usada para popular o `<Select>` de
// categoria no formulário de cadastro.
type Categoria struct {
	ID     string `json:"id"`
	Codigo string `json:"codigo"`
	Nome   string `json:"nome"`
}

// DimensaoInput é o par valor+unidade de uma dimensão física do Produto
// (AD-9: nunca texto livre). `nil` (ou os dois ponteiros internos `nil`)
// significa "dimensão não informada" — válido, vira NULL nas duas colunas.
// Só um dos dois ponteiros preenchido é o caso de erro que CriarProduto
// rejeita nomeando o campo.
type DimensaoInput struct {
	Valor   *float64
	Unidade *string
}

// CriarProdutoInput agrupa os campos aceitos por CriarProduto (Story 3.1,
// FR-8; `TemplateID` acrescentado pela Story 3.2). `EstoqueID`/
// `QuantidadeInicial` alimentam o INSERT em `produto_estoque` feito na mesma
// transação do INSERT em `produtos`.
//
// `TemplateID` vazio (após trim) preserva o comportamento da Story 3.1: nome
// livre, sem validação de estrutura. Preenchido, `nome` deve casar o formato
// do template referenciado (Story 3.2, AC1/AC2) — ver nomeValidoParaTemplate.
type CriarProdutoInput struct {
	Nome              string
	Codigo            string
	Observacoes       string
	CategoriaID       string
	EstoqueID         string
	TemplateID        string
	QuantidadeInicial float64
	Comprimento       *DimensaoInput
	Largura           *DimensaoInput
	Diametro          *DimensaoInput
	Altura            *DimensaoInput
	Espessura         *DimensaoInput
}

// ErroProdutoValidacao é o erro de validação devolvido por CriarProduto:
// nome ausente/longo demais, categoria/estoque ausentes ou inexistentes,
// quantidade inicial negativa, ou uma das 5 dimensões com valor sem unidade
// (ou vice-versa) ou com valor/unidade fora do intervalo aceito. A mensagem
// já vem pronta para exibição — nomeia o campo específico quando aplicável
// (ex. "largura: valor e unidade devem ser informados juntos"). Sempre
// mapeado para 400 VALIDATION_ERROR; nenhuma escrita acontece quando este
// erro é devolvido (validado por completo antes de abrir a transação).
type ErroProdutoValidacao struct {
	Mensagem string
}

func (e *ErroProdutoValidacao) Error() string { return e.Mensagem }

// ErrProdutoNaoEncontrado indica `id` de Produto inexistente OU malformado
// (não-UUID, `pq` SQLSTATE 22P02) — os dois colapsam no mesmo erro, mesmo
// padrão de ErrEstoqueNaoEncontrado/ErrContaNaoEncontrada. Mapeado para
// 404 NOT_FOUND por AtualizarNomeProdutoHandler (Story 3.2).
var ErrProdutoNaoEncontrado = errors.New("produto não encontrado")

// ErroEstoqueComResiduo indica que o Estoque alvo de ExcluirEstoque
// (estoques.go, mesmo pacote) tem ao menos um Produto com quantidade
// residual (`produto_estoque.quantidade > 0`) — a exclusão é barrada, nada é
// removido. `Produtos` lista os nomes na mesma ordem do SELECT (alfabética),
// e `Error()` já produz a mensagem citando-os, pronta para o envelope de
// erro fixo `{"error":{"code","message"}}` (AD-14), que não tem campo extra
// para a lista. Completa o guard que a Story 2.2 deixou pendente até
// `produto_estoque` existir.
type ErroEstoqueComResiduo struct {
	Produtos []string
}

func (e *ErroEstoqueComResiduo) Error() string {
	return fmt.Sprintf("estoque possui quantidade residual de: %s", strings.Join(e.Produtos, ", "))
}

// validarDimensao aplica a regra de par da AD-9 para uma dimensão nomeada
// `campo` (usado na mensagem de erro): ausente por completo -> válido, NULL
// nas duas colunas; só um dos dois preenchido -> ErroProdutoValidacao citando
// `campo`; os dois preenchidos -> `valor` deve ser > 0 e <= limiteNumeric103
// (a coluna é `NUMERIC(10,3)`) e `unidade` deve estar em `{mm,cm,m}`, senão
// ErroProdutoValidacao citando `campo`.
func validarDimensao(campo string, d *DimensaoInput) (sql.NullFloat64, sql.NullString, error) {
	if d == nil || (d.Valor == nil && d.Unidade == nil) {
		return sql.NullFloat64{}, sql.NullString{}, nil
	}
	if d.Valor == nil || d.Unidade == nil {
		return sql.NullFloat64{}, sql.NullString{}, &ErroProdutoValidacao{
			Mensagem: fmt.Sprintf("%s: valor e unidade devem ser informados juntos", campo),
		}
	}
	if *d.Valor <= 0 {
		return sql.NullFloat64{}, sql.NullString{}, &ErroProdutoValidacao{
			Mensagem: fmt.Sprintf("%s: valor deve ser maior que zero", campo),
		}
	}
	if *d.Valor > limiteNumeric103 {
		return sql.NullFloat64{}, sql.NullString{}, &ErroProdutoValidacao{
			Mensagem: fmt.Sprintf("%s: valor deve ser no máximo %s", campo, limiteNumeric103Texto),
		}
	}
	if !unidadesDimensaoValidas[*d.Unidade] {
		return sql.NullFloat64{}, sql.NullString{}, &ErroProdutoValidacao{
			Mensagem: fmt.Sprintf("%s: unidade deve ser mm, cm ou m", campo),
		}
	}
	return sql.NullFloat64{Float64: *d.Valor, Valid: true}, sql.NullString{String: *d.Unidade, Valid: true}, nil
}

// CriarProduto valida e insere um novo Produto (Story 3.1, FR-8; Story 3.2
// acrescenta a Nomenclatura Guiada). Toda a validação acontece ANTES de
// qualquer escrita — nome, categoria/estoque (presença), quantidade inicial,
// as 5 dimensões pareadas e, quando `TemplateID` é informado, o formato do
// nome contra o template (ver o bloco de validação de Nomenclatura Guiada
// abaixo) — de modo que um erro de validação NUNCA deixa um Produto
// parcialmente gravado.
//
// Sucesso: uma única transação insere a linha em `produtos` (`RETURNING id,
// nome`, incluindo `template_id` quando informado) seguida da linha em
// `produto_estoque` vinculando o Produto recém-criado ao Estoque informado
// com a quantidade inicial, e comita as duas juntas. `categoria_id`/
// `estoque_id` que não correspondem a nenhuma linha (violação de FK,
// SQLSTATE 23503) ou que não são UUID válido (SQLSTATE 22P02, mesma
// constante pqInvalidTextRepresentation de promocao.go) colapsam em
// ErroProdutoValidacao — nunca um 500: input de cliente inválido não é erro
// de servidor. `template_id` já foi validado (existência + formato do nome)
// antes da transação abrir, então na prática o INSERT em `produtos` só pode
// falhar por causa de `categoria_id`; o INSERT em `produto_estoque` (rodando
// depois, já com um `produto_id` válido) só pode falhar por causa de
// `estoque_id` — por isso a mensagem de cada ramo já nomeia o campo certo
// sem precisar inspecionar o nome da constraint.
func CriarProduto(db *sql.DB, input CriarProdutoInput) (Produto, error) {
	nomeTrimado := strings.TrimSpace(input.Nome)
	if nomeTrimado == "" || utf8.RuneCountInString(nomeTrimado) > 255 {
		return Produto{}, &ErroProdutoValidacao{
			Mensagem: "nome é obrigatório e deve ter no máximo 255 caracteres",
		}
	}

	codigoTrimado := strings.TrimSpace(input.Codigo)
	if codigoTrimado != "" && utf8.RuneCountInString(codigoTrimado) > 255 {
		return Produto{}, &ErroProdutoValidacao{
			Mensagem: "código deve ter no máximo 255 caracteres",
		}
	}

	categoriaID := strings.TrimSpace(input.CategoriaID)
	if categoriaID == "" {
		return Produto{}, &ErroProdutoValidacao{Mensagem: "categoria é obrigatória"}
	}
	estoqueID := strings.TrimSpace(input.EstoqueID)
	if estoqueID == "" {
		return Produto{}, &ErroProdutoValidacao{Mensagem: "estoque é obrigatório"}
	}
	if input.QuantidadeInicial < 0 {
		return Produto{}, &ErroProdutoValidacao{
			Mensagem: "quantidade inicial deve ser maior ou igual a zero",
		}
	}
	if input.QuantidadeInicial > limiteNumeric103 {
		return Produto{}, &ErroProdutoValidacao{
			Mensagem: fmt.Sprintf("quantidade inicial deve ser no máximo %s", limiteNumeric103Texto),
		}
	}

	comprimentoValor, comprimentoUnidade, err := validarDimensao("comprimento", input.Comprimento)
	if err != nil {
		return Produto{}, err
	}
	larguraValor, larguraUnidade, err := validarDimensao("largura", input.Largura)
	if err != nil {
		return Produto{}, err
	}
	diametroValor, diametroUnidade, err := validarDimensao("diâmetro", input.Diametro)
	if err != nil {
		return Produto{}, err
	}
	alturaValor, alturaUnidade, err := validarDimensao("altura", input.Altura)
	if err != nil {
		return Produto{}, err
	}
	espessuraValor, espessuraUnidade, err := validarDimensao("espessura", input.Espessura)
	if err != nil {
		return Produto{}, err
	}

	// Validação de Nomenclatura Guiada (Story 3.2, AC1/AC2) — feita AQUI,
	// ainda antes de abrir a transação, junto às demais validações: um
	// `template_id` vazio (após trim) preserva o comportamento da Story 3.1
	// (nome livre); preenchido, exige que `nome` case o formato do template.
	var templateID sql.NullString
	templateIDTrimado := strings.TrimSpace(input.TemplateID)
	if templateIDTrimado != "" {
		var templateTexto string
		err := db.QueryRow(
			`SELECT template FROM nomenclatura_templates WHERE id = $1`, templateIDTrimado,
		).Scan(&templateTexto)
		if err != nil {
			var pqErr *pq.Error
			if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
				return Produto{}, &ErroProdutoValidacao{Mensagem: "template selecionado não existe"}
			}
			return Produto{}, fmt.Errorf("falha ao buscar template de nomenclatura: %w", err)
		}
		if !nomeValidoParaTemplate(templateTexto, nomeTrimado) {
			return Produto{}, &ErroProdutoValidacao{
				Mensagem: "nome não corresponde ao formato do template selecionado",
			}
		}
		templateID = sql.NullString{String: templateIDTrimado, Valid: true}
	}

	var codigo sql.NullString
	if codigoTrimado != "" {
		codigo = sql.NullString{String: codigoTrimado, Valid: true}
	}
	var observacoes sql.NullString
	if observacoesTrimadas := strings.TrimSpace(input.Observacoes); observacoesTrimadas != "" {
		observacoes = sql.NullString{String: observacoesTrimadas, Valid: true}
	}

	tx, err := db.Begin()
	if err != nil {
		return Produto{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	const insertProduto = `
		INSERT INTO produtos (
			nome, codigo, categoria_id, observacoes, template_id,
			comprimento_valor, comprimento_unidade,
			largura_valor, largura_unidade,
			diametro_valor, diametro_unidade,
			altura_valor, altura_unidade,
			espessura_valor, espessura_unidade
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id, nome`
	var p Produto
	err = tx.QueryRow(insertProduto,
		nomeTrimado, codigo, categoriaID, observacoes, templateID,
		comprimentoValor, comprimentoUnidade,
		larguraValor, larguraUnidade,
		diametroValor, diametroUnidade,
		alturaValor, alturaUnidade,
		espessuraValor, espessuraUnidade,
	).Scan(&p.ID, &p.Nome)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && (pqErr.Code == pqForeignKeyViolation || pqErr.Code == pqInvalidTextRepresentation) {
			return Produto{}, &ErroProdutoValidacao{Mensagem: "categoria informada não existe"}
		}
		// Violação do índice único parcial `idx_produtos_codigo` (migration
		// 000017, Story 3.4): `código` já não-nulo em outro Produto. A
		// importação (services/importacoes.go) depende dessa unicidade para
		// que o match por código de processarProximaLinha seja determinístico
		// — o cadastro manual passa a respeitar a mesma regra.
		if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation {
			return Produto{}, &ErroProdutoValidacao{Mensagem: "código já cadastrado"}
		}
		return Produto{}, fmt.Errorf("falha ao inserir produto: %w", err)
	}

	const insertProdutoEstoque = `
		INSERT INTO produto_estoque (produto_id, estoque_id, quantidade)
		VALUES ($1, $2, $3)`
	if _, err := tx.Exec(insertProdutoEstoque, p.ID, estoqueID, input.QuantidadeInicial); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && (pqErr.Code == pqForeignKeyViolation || pqErr.Code == pqInvalidTextRepresentation) {
			return Produto{}, &ErroProdutoValidacao{Mensagem: "estoque informado não existe"}
		}
		return Produto{}, fmt.Errorf("falha ao inserir produto_estoque: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Produto{}, fmt.Errorf("falha ao commitar cadastro de produto: %w", err)
	}
	return p, nil
}

// AtualizarNomeProduto edita SÓ o `nome` de um Produto existente (Story 3.2,
// AC3) — escopo deliberadamente estreito: dimensões/categoria/estoque/
// observações ficam fora, sem endpoint de edição para eles nesta story (não
// existe, em nenhum épico do roadmap, uma tela geral de edição de Produto).
//
// `novoNome` é validado com a MESMA regra de CriarProduto (trim, 1..255
// runes) -> ErroProdutoValidacao se falhar, nenhuma leitura/escrita
// acontece.
//
// `id` inexistente OU malformado (não-UUID, `pq` SQLSTATE 22P02) ->
// ErrProdutoNaoEncontrado. Quando o Produto tem `template_id` aplicado, o
// novo nome é revalidado contra esse MESMO template (nomeValidoParaTemplate)
// antes do UPDATE — a regra da Story 3.2 não pode ser burlada editando o
// nome depois do cadastro; inválido -> ErroProdutoValidacao, `nome` no banco
// permanece o anterior (nenhum UPDATE roda). Produto sem template
// (`template_id IS NULL`) aceita qualquer texto que passe na validação
// básica acima.
func AtualizarNomeProduto(db *sql.DB, id string, novoNome string) (Produto, error) {
	nomeTrimado := strings.TrimSpace(novoNome)
	if nomeTrimado == "" || utf8.RuneCountInString(nomeTrimado) > 255 {
		return Produto{}, &ErroProdutoValidacao{
			Mensagem: "nome é obrigatório e deve ter no máximo 255 caracteres",
		}
	}

	var templateID sql.NullString
	err := db.QueryRow(`SELECT template_id FROM produtos WHERE id = $1`, id).Scan(&templateID)
	if err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return Produto{}, ErrProdutoNaoEncontrado
		}
		return Produto{}, fmt.Errorf("falha ao buscar produto para renomear: %w", err)
	}

	if templateID.Valid {
		var templateTexto string
		if err := db.QueryRow(
			`SELECT template FROM nomenclatura_templates WHERE id = $1`, templateID.String,
		).Scan(&templateTexto); err != nil {
			return Produto{}, fmt.Errorf("falha ao buscar template aplicado ao produto: %w", err)
		}
		if !nomeValidoParaTemplate(templateTexto, nomeTrimado) {
			return Produto{}, &ErroProdutoValidacao{
				Mensagem: "nome não corresponde ao formato do template aplicado a este produto",
			}
		}
	}

	var p Produto
	if err := db.QueryRow(
		`UPDATE produtos SET nome = $1 WHERE id = $2 RETURNING id, nome`, nomeTrimado, id,
	).Scan(&p.ID, &p.Nome); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Inalcançável na prática: o SELECT acima já provou que a linha
			// existe. Mantido como defesa em profundidade contra uma
			// exclusão concorrente entre o SELECT e o UPDATE.
			return Produto{}, ErrProdutoNaoEncontrado
		}
		return Produto{}, fmt.Errorf("falha ao atualizar nome do produto: %w", err)
	}
	return p, nil
}

// ListarCategorias devolve as categorias fixas ordenadas por `codigo`
// ascendente (Story 3.1, AC4) — a lista da qual o formulário de cadastro
// seleciona, nunca digitável livremente. Lista vazia não é erro (embora não
// deva ocorrer em produção: a migração 000010 sempre semeia as 25 linhas).
func ListarCategorias(db *sql.DB) ([]Categoria, error) {
	rows, err := db.Query(`SELECT id, codigo, nome FROM categorias ORDER BY codigo ASC`)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar categorias: %w", err)
	}
	defer rows.Close()

	categorias := make([]Categoria, 0)
	for rows.Next() {
		var c Categoria
		if err := rows.Scan(&c.ID, &c.Codigo, &c.Nome); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de categoria: %w", err)
		}
		categorias = append(categorias, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar categorias: %w", err)
	}
	return categorias, nil
}
