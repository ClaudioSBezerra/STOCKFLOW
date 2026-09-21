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

// unidadesMedidaValidas é o conjunto fechado de Unidade de Medida aceito
// pelo cadastro de Produto (Story 10.3, spec-10-3, FR45/FR46; addendum.md
// §F) — mesma grafia, incluindo os caracteres não-ASCII de "m²"/"m³"/
// "kg/m²". Fechado por design: um valor fora deste conjunto é sempre
// ErroProdutoValidacao "unidade de medida inválida", nunca gravado.
var unidadesMedidaValidas = map[string]bool{
	"un": true, "m": true, "m²": true, "m³": true, "kg": true, "L": true,
	"cx": true, "rolo": true, "barra": true, "mm": true, "cm": true, "kg/m²": true,
}

// limiteTextoLivre255 é o teto de 255 runas aplicado a `codigo_fornecedor`/
// `embalagem` (Story 10.3, spec-10-3) — mesma razão de limiteNumeric103:
// evita que o Postgres rejeite com "value too long for type character
// varying(255)" (colunas `VARCHAR(255)`), um erro não mapeado que cairia no
// 500 genérico em cima de input de cliente inválido.
const limiteTextoLivre255 = 255

// validarTextoLivreOpcional trima `valor` e aplica o teto de 255 runas
// (Story 10.3) para um campo de texto livre opcional (`codigo_fornecedor`/
// `embalagem`): vazio após trim -> NULL, válido; acima do limite ->
// ErroProdutoValidacao citando `campo`. Nunca valida formato/conteúdo — só
// tamanho, mesmo espírito de limiteNumeric103.
func validarTextoLivreOpcional(campo, valor string) (sql.NullString, error) {
	trimado := strings.TrimSpace(valor)
	if trimado == "" {
		return sql.NullString{}, nil
	}
	if utf8.RuneCountInString(trimado) > limiteTextoLivre255 {
		return sql.NullString{}, &ErroProdutoValidacao{
			Mensagem: fmt.Sprintf("%s deve ter no máximo %d caracteres", campo, limiteTextoLivre255),
		}
	}
	return sql.NullString{String: trimado, Valid: true}, nil
}

// validarUnidadeMedida trima `valor` e exige que esteja no conjunto fechado
// unidadesMedidaValidas (Story 10.3, spec-10-3) — SEMPRE obrigatório no
// cadastro (`CriarProduto`), ainda que a coluna `unidade_medida` continue
// NULLable no banco (Never, spec-10-3: a importação em massa não preenche
// esta coluna). Vazio após trim -> "unidade de medida é obrigatória"; fora
// do conjunto -> "unidade de medida inválida".
func validarUnidadeMedida(valor string) (string, error) {
	trimado := strings.TrimSpace(valor)
	if trimado == "" {
		return "", &ErroProdutoValidacao{Mensagem: "unidade de medida é obrigatória"}
	}
	if !unidadesMedidaValidas[trimado] {
		return "", &ErroProdutoValidacao{Mensagem: "unidade de medida inválida"}
	}
	return trimado, nil
}

// pesosEAN13 são os pesos alternados 1/3 do dígito verificador do EAN-13,
// aplicados às 12 primeiras posições (algoritmo padrão: soma ponderada,
// dígito = (10 - soma%10) % 10).
var pesosEAN13 = [12]int{1, 3, 1, 3, 1, 3, 1, 3, 1, 3, 1, 3}

// validarEAN13 trima `valor` e valida o Código EAN-13 (Story 10.3,
// spec-10-3): vazio após trim é SEMPRE válido (NULL, campo opcional, nunca
// rejeitado); não-vazio deve ter exatamente 13 caracteres ASCII `0-9`,
// senão "EAN-13 deve ter 13 dígitos"; com 13 dígitos, o dígito verificador
// (13ª posição) deve conferir com a soma ponderada 1/3 alternada das 12
// primeiras, senão "EAN-13 inválido: dígito verificador não confere" (mesmo
// estilo de ValidarCNPJ/digitoVerificadorCNPJ, empresas.go).
func validarEAN13(valor string) (sql.NullString, error) {
	trimado := strings.TrimSpace(valor)
	if trimado == "" {
		return sql.NullString{}, nil
	}
	if len(trimado) != 13 {
		return sql.NullString{}, &ErroProdutoValidacao{Mensagem: "EAN-13 deve ter 13 dígitos"}
	}
	for _, r := range trimado {
		if r < '0' || r > '9' {
			return sql.NullString{}, &ErroProdutoValidacao{Mensagem: "EAN-13 deve ter 13 dígitos"}
		}
	}
	soma := 0
	for i := 0; i < 12; i++ {
		soma += int(trimado[i]-'0') * pesosEAN13[i]
	}
	digitoEsperado := (10 - soma%10) % 10
	if digitoEsperado != int(trimado[12]-'0') {
		return sql.NullString{}, &ErroProdutoValidacao{Mensagem: "EAN-13 inválido: dígito verificador não confere"}
	}
	return sql.NullString{String: trimado, Valid: true}, nil
}

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

// Produto é a projeção mínima devolvida por POST /api/produtos (Story 3.1;
// Story 10.2, spec-10-2, acrescenta `Codigo` — o código sequencial gerado
// pelo servidor, para que o cliente saiba o valor atribuído).
type Produto struct {
	ID     string `json:"id"`
	Nome   string `json:"nome"`
	Codigo string `json:"codigo"`
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
// FR-8; `TemplateID` acrescentado pela Story 3.2). Desde a Story 11.6
// (FR8, AD-29) o cadastro NÃO recebe Estoque nem quantidade inicial: o Produto
// nasce sem nenhuma linha de saldo — o Lançamento de Saldo (Story 11.1) é o
// único caminho de entrada de saldo.
//
// `TemplateID` é sempre obrigatório desde a Story 10.1 (AC2) — vazio (após
// trim) rejeita com ErroProdutoValidacao, não existe mais caminho de nome
// livre sem template no cadastro. `nome` deve casar o formato do template
// referenciado (Story 3.2, AC1/AC2) — ver nomeValidoParaTemplate; o template
// Genérico (`[NOME LIVRE]`, Story 10.1, AD-34) aceita qualquer nome não
// vazio, sem checar estrutura.
type CriarProdutoInput struct {
	Nome        string
	Observacoes string
	CategoriaID string
	TemplateID  string
	Comprimento *DimensaoInput
	Largura     *DimensaoInput
	Diametro    *DimensaoInput
	Altura      *DimensaoInput
	Espessura   *DimensaoInput
	// CodigoFornecedor/EAN13/UnidadeMedida/Embalagem (Story 10.3, spec-10-3,
	// FR45/FR46): CodigoFornecedor/Embalagem são texto livre opcional, sem
	// checagem de unicidade; EAN13 é opcional mas validado (formato + dígito
	// verificador) quando não-vazio; UnidadeMedida é SEMPRE obrigatório
	// (conjunto fechado, addendum.md §F) — ver validarUnidadeMedida/
	// validarEAN13/validarTextoLivreOpcional.
	CodigoFornecedor string
	EAN13            string
	UnidadeMedida    string
	Embalagem        string
}

// ErroProdutoValidacao é o erro de validação devolvido por CriarProduto:
// nome ausente/longo demais, categoria ausente ou inexistente,
// ou uma das 5 dimensões com valor sem unidade
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

// ErroEstoqueComPedidoPendente indica que o Estoque alvo de ExcluirEstoque
// (estoques.go, mesmo pacote) é referenciado por `pedido_itens` de ao menos
// um Pedido `status='pendente'` — a exclusão é barrada, nada é removido
// (Story 7.2, spec-7-2, segundo guard de exclusão de Estoque, completando o
// que a Story 2.2 deixou pendente até `pedidos` existir). `Produtos` lista
// os nomes (do SNAPSHOT em `pedido_itens.produto_nome`, nunca um join ao
// vivo com `produtos`) na mesma ordem do SELECT (alfabética), e `Error()` já
// produz a mensagem citando-os. Mesmo molde de ErroEstoqueComResiduo.
type ErroEstoqueComPedidoPendente struct {
	Produtos []string
}

func (e *ErroEstoqueComPedidoPendente) Error() string {
	return fmt.Sprintf("estoque possui pedido pendente referenciando: %s", strings.Join(e.Produtos, ", "))
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

// proximoCodigoProduto gera o próximo código sequencial de Produto para
// `empresaID` (Story 10.2, spec-10-2, FR-45): incrementa atomicamente
// `contadores_produto.ultimo_numero` via `UPDATE ... RETURNING`, DENTRO da
// transação `tx` do chamador, e formata o resultado com zero-padding de 6
// dígitos (ex. "000001"). Nunca faz lazy-init da linha do contador — a
// ausência de linha (`sql.ErrNoRows`) é um estado impossível em produção
// (toda Empresa nasce com sua linha via ProvisionarEmpresa, AD-26) e vira
// erro interno (`fmt.Errorf`, não ErroProdutoValidacao — não é input de
// cliente inválido).
//
// O próximo número é `GREATEST(contador, maior código puramente numérico da
// Empresa) + 1`: um código legado ou importado por planilha (que continua
// trazendo o `codigo` da planilha, Story 3.3/3.4) nunca colide com a
// sequência. O `UPDATE` trava a linha do contador, então dois cadastros
// concorrentes se serializam nela — o segundo relê o contador já avançado
// pelo primeiro, sem a corrida de um `SELECT MAX()+1` solto. O `CASE`
// impede o cast de códigos não numéricos (o Postgres não garante a ordem de
// avaliação de um `WHERE ... AND`); 9 dígitos no máximo cabem num INTEGER.
func proximoCodigoProduto(tx *sql.Tx, empresaID string) (string, error) {
	var numero int
	err := tx.QueryRow(
		`UPDATE contadores_produto c
		    SET ultimo_numero = GREATEST(
		          c.ultimo_numero,
		          COALESCE((SELECT MAX(CASE WHEN p.codigo ~ '^[0-9]{1,9}$' THEN p.codigo::integer END)
		                      FROM produtos p WHERE p.empresa_id = c.empresa_id), 0)
		        ) + 1
		  WHERE c.empresa_id = $1
		RETURNING c.ultimo_numero`,
		empresaID,
	).Scan(&numero)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("contador de código de produto ausente para a empresa %s", empresaID)
		}
		return "", fmt.Errorf("falha ao gerar próximo código de produto: %w", err)
	}
	return fmt.Sprintf("%06d", numero), nil
}

// CriarProduto valida e insere um novo Produto (Story 3.1, FR-8; Story 3.2
// acrescenta a Nomenclatura Guiada; Story 10.1 torna `nome` com mínimo de 10
// runas e `TemplateID` sempre obrigatórios, FR8; Story 10.2, spec-10-2,
// FR-45, tira `codigo` da entrada — o servidor gera o próximo número
// sequencial da Empresa via proximoCodigoProduto). Toda a validação acontece
// ANTES de qualquer escrita — nome (10..255 runas), categoria
// (presença), as 5 dimensões pareadas e o formato do
// nome contra o template selecionado, sempre presente (ver o bloco de
// validação de Nomenclatura Guiada abaixo, incluindo o fallback Genérico
// `[NOME LIVRE]`, AD-34) — de modo que um erro de validação NUNCA deixa um
// Produto parcialmente gravado.
//
// Sucesso: uma única transação gera o próximo código sequencial da Empresa
// (proximoCodigoProduto), insere a linha em `produtos` (`RETURNING id, nome,
// codigo`, incluindo `template_id` quando informado) e comita as duas juntas
// — nenhuma linha em `produto_estoque`/`lotes` é criada (Story 11.6).
// `categoria_id` que não corresponde a nenhuma linha (violação de FK,
// SQLSTATE 23503) ou que não são UUID válido (SQLSTATE 22P02, mesma
// constante pqInvalidTextRepresentation de promocao.go) colapsam em
// ErroProdutoValidacao — nunca um 500: input de cliente inválido não é erro
// de servidor. `template_id` já foi validado (existência + formato do nome)
// antes da transação abrir, então na prática o INSERT em `produtos` só pode
// falhar por causa de `categoria_id`.
func CriarProduto(db *sql.DB, empresaID string, input CriarProdutoInput) (Produto, error) {
	nomeTrimado := strings.TrimSpace(input.Nome)
	if n := utf8.RuneCountInString(nomeTrimado); n < 10 || n > 255 {
		return Produto{}, &ErroProdutoValidacao{
			Mensagem: "nome é obrigatório e deve ter entre 10 e 255 caracteres",
		}
	}

	categoriaID := strings.TrimSpace(input.CategoriaID)
	if categoriaID == "" {
		return Produto{}, &ErroProdutoValidacao{Mensagem: "categoria é obrigatória"}
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

	// Validação de Nomenclatura Guiada (Story 3.2, AC1/AC2; Story 10.1, AC2)
	// — feita AQUI, ainda antes de abrir a transação, junto às demais
	// validações. Desde a Story 10.1 `template_id` é SEMPRE obrigatório: o
	// template Genérico (`[NOME LIVRE]`, AD-34, migration 000036) é o
	// fallback universal para Categorias sem template estrutural, então não
	// existe mais caminho de cadastro sem template selecionado.
	var templateID sql.NullString
	templateIDTrimado := strings.TrimSpace(input.TemplateID)
	if templateIDTrimado == "" {
		return Produto{}, &ErroProdutoValidacao{
			Mensagem: "template de nomenclatura é obrigatório",
		}
	}
	{
		var templateTexto string
		err := db.QueryRow(
			`SELECT template FROM nomenclatura_templates WHERE id = $1 AND empresa_id = $2`,
			templateIDTrimado, empresaID,
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

	var observacoes sql.NullString
	if observacoesTrimadas := strings.TrimSpace(input.Observacoes); observacoesTrimadas != "" {
		observacoes = sql.NullString{String: observacoesTrimadas, Valid: true}
	}

	// Código do Fornecedor, EAN-13, Unidade de Medida e Embalagem (Story
	// 10.3, spec-10-3, FR45/FR46) — ainda antes de abrir a transação, junto
	// às demais validações: um erro aqui NUNCA deixa um Produto parcialmente
	// gravado.
	codigoFornecedor, err := validarTextoLivreOpcional("código do fornecedor", input.CodigoFornecedor)
	if err != nil {
		return Produto{}, err
	}
	ean13, err := validarEAN13(input.EAN13)
	if err != nil {
		return Produto{}, err
	}
	unidadeMedida, err := validarUnidadeMedida(input.UnidadeMedida)
	if err != nil {
		return Produto{}, err
	}
	embalagem, err := validarTextoLivreOpcional("embalagem", input.Embalagem)
	if err != nil {
		return Produto{}, err
	}

	tx, err := db.Begin()
	if err != nil {
		return Produto{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	// Código sequencial da Empresa (Story 10.2, spec-10-2, FR-45) — gerado
	// DENTRO da mesma transação do INSERT em `produtos`, antes dele, nunca a
	// partir de entrada do cliente.
	codigo, err := proximoCodigoProduto(tx, empresaID)
	if err != nil {
		return Produto{}, err
	}

	// A Categoria informada precisa pertencer À MESMA Empresa (Story 9.1,
	// AD-20): em vez de um SELECT-antes-de-INSERT (que teria janela de
	// corrida), o próprio INSERT lê `categorias` com o filtro de Empresa —
	// uma Categoria de outra Empresa simplesmente não produz linha, e o
	// `sql.ErrNoRows` do RETURNING colapsa na MESMA mensagem de "categoria
	// informada não existe" de um id inexistente (nunca revela existência).
	const insertProduto = `
		INSERT INTO produtos (
			nome, codigo, categoria_id, observacoes, template_id,
			comprimento_valor, comprimento_unidade,
			largura_valor, largura_unidade,
			diametro_valor, diametro_unidade,
			altura_valor, altura_unidade,
			espessura_valor, espessura_unidade,
			codigo_fornecedor, ean13, unidade_medida, embalagem,
			empresa_id
		)
		SELECT $1, $2, c.id, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20
		FROM categorias c
		WHERE c.id = $3 AND c.empresa_id = $20
		RETURNING id, nome, codigo`
	var p Produto
	err = tx.QueryRow(insertProduto,
		nomeTrimado, codigo, categoriaID, observacoes, templateID,
		comprimentoValor, comprimentoUnidade,
		larguraValor, larguraUnidade,
		diametroValor, diametroUnidade,
		alturaValor, alturaUnidade,
		espessuraValor, espessuraUnidade,
		codigoFornecedor, ean13, unidadeMedida, embalagem,
		empresaID,
	).Scan(&p.ID, &p.Nome, &p.Codigo)
	if err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && (pqErr.Code == pqForeignKeyViolation || pqErr.Code == pqInvalidTextRepresentation)) {
			return Produto{}, &ErroProdutoValidacao{Mensagem: "categoria informada não existe"}
		}
		// Violação do índice único `idx_produtos_codigo` (empresa_id, codigo;
		// migration 000032): na prática, hoje, só uma colisão eventual entre
		// o código sequencial recém-gerado e um código legado/manual já
		// gravado antes da Story 10.2 (spec-10-2, Design Notes) — não há
		// lógica nova de desvio/realocação, o Produto simplesmente não é
		// gravado. A importação (services/importacoes.go) depende da mesma
		// unicidade para que o match por código de processarProximaLinha seja
		// determinístico.
		if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation {
			return Produto{}, &ErroProdutoValidacao{Mensagem: "código já cadastrado"}
		}
		return Produto{}, fmt.Errorf("falha ao inserir produto: %w", err)
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
// `novoNome` é validado com a MESMA regra de CriarProduto (trim, 10..255
// runes desde a Story 10.1) -> ErroProdutoValidacao se falhar, nenhuma
// leitura/escrita acontece.
//
// `id` inexistente OU malformado (não-UUID, `pq` SQLSTATE 22P02) ->
// ErrProdutoNaoEncontrado. Quando o Produto tem `template_id` aplicado, o
// novo nome é revalidado contra esse MESMO template (nomeValidoParaTemplate)
// antes do UPDATE — a regra da Story 3.2 não pode ser burlada editando o
// nome depois do cadastro; inválido -> ErroProdutoValidacao, `nome` no banco
// permanece o anterior (nenhum UPDATE roda). Produto sem template
// (`template_id IS NULL`) aceita qualquer texto que passe na validação
// básica acima.
func AtualizarNomeProduto(db *sql.DB, empresaID string, id string, novoNome string) (Produto, error) {
	nomeTrimado := strings.TrimSpace(novoNome)
	if n := utf8.RuneCountInString(nomeTrimado); n < 10 || n > 255 {
		return Produto{}, &ErroProdutoValidacao{
			Mensagem: "nome é obrigatório e deve ter entre 10 e 255 caracteres",
		}
	}

	var templateID sql.NullString
	err := db.QueryRow(
		`SELECT template_id FROM produtos WHERE id = $1 AND empresa_id = $2`, id, empresaID,
	).Scan(&templateID)
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
		`UPDATE produtos SET nome = $1 WHERE id = $2 AND empresa_id = $3 RETURNING id, nome`,
		nomeTrimado, id, empresaID,
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

// ProdutoBusca é a projeção devolvida por BuscarProdutos (Story 4.1,
// spec-4-1) para GET /api/produtos/busca: `id`/`nome` do Produto, `codigo`
// (ponteiro — `nil`/`null` quando o Produto não tem código cadastrado,
// coluna opcional) e a Categoria completa (mesma projeção de
// ListarCategorias), usada pela UI para mostrar nome da categoria ao lado do
// resultado.
type ProdutoBusca struct {
	ID        string    `json:"id"`
	Nome      string    `json:"nome"`
	Codigo    *string   `json:"codigo"`
	Categoria Categoria `json:"categoria"`
}

// escaparCoringasLike escapa os 3 caracteres com significado especial em um
// padrão `LIKE`/`ILIKE` do Postgres (`\`, `%`, `_`) — nessa ordem: `\`
// primeiro, para não escapar duas vezes as barras inseridas pelos dois
// replace seguintes. Usado por BuscarProdutos ao montar os padrões de prefixo
// e substring a partir do termo digitado pelo usuário, para que um `%`/`_`
// literal no termo (ex. um código de Produto contendo `_`, comum em SKUs, ou
// um desconto "50%") não vire wildcard não intencional — a query sempre casa
// esses padrões com `ESCAPE '\'`.
func escaparCoringasLike(s string) string {
	substituidor := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return substituidor.Replace(s)
}

// buscarProdutosQuery ranqueia Produtos por relevância contra `termo`
// (Story 4.1, spec-4-1): rank 0 = `nome`/`codigo` == termo, case-insensitive
// (comparação direta com `lower()`, não `ILIKE` — termo aqui NUNCA é tratado
// como padrão, então não precisa de escaping); rank 1 = `nome`/`codigo`
// começa com o termo; rank 2 = `categorias.nome` == ou começa com o termo;
// rank 3 = qualquer outro match por substring em `nome`/`codigo`/
// `categorias.nome`. O `WHERE` usa só o padrão de substring (`%termo%`) —
// ele já é um superconjunto dos padrões de prefixo/igualdade usados no
// `CASE`, então uma linha que cai em qualquer rank sempre passa pelo `WHERE`.
// Sem `pg_trgm`/índice novo (Design Notes da spec): volume de ~8.000 linhas
// é trivial para um `ILIKE` sequencial com `JOIN` em `categorias` (25
// linhas).
const buscarProdutosQuery = `
	SELECT p.id, p.nome, p.codigo, c.id, c.codigo, c.nome,
		CASE
			WHEN lower(p.nome) = lower($1) OR lower(p.codigo) = lower($1) THEN 0
			WHEN p.nome ILIKE $2 ESCAPE '\' OR p.codigo ILIKE $2 ESCAPE '\' THEN 1
			WHEN lower(c.nome) = lower($1) OR c.nome ILIKE $2 ESCAPE '\' THEN 2
			ELSE 3
		END AS rank
	FROM produtos p
	JOIN categorias c ON c.id = p.categoria_id
	WHERE p.deleted_at IS NULL
	  AND p.empresa_id = $4
	  AND (p.nome ILIKE $3 ESCAPE '\' OR p.codigo ILIKE $3 ESCAPE '\' OR c.nome ILIKE $3 ESCAPE '\')
	ORDER BY rank ASC, p.nome ASC, p.id ASC
	LIMIT 7`

// BuscarProdutos devolve até 7 Produtos ranqueados por relevância contra
// `termo` (Story 4.1, spec-4-1, FR-4) — ver buscarProdutosQuery para o
// critério de ranking exato. `termo` é assumido não-vazio e já trimado: essa
// validação é responsabilidade do chamador (BuscarProdutosHandler); esta
// função nunca devolve erro de validação, só erro de banco. Nenhum match em
// nenhum dos três campos -> slice vazio (nunca `nil`), mesmo padrão de
// ListarCategorias.
func BuscarProdutos(db *sql.DB, empresaID string, termo string) ([]ProdutoBusca, error) {
	termoEscapado := escaparCoringasLike(termo)
	padraoPrefixo := termoEscapado + "%"
	padraoSubstring := "%" + termoEscapado + "%"

	rows, err := db.Query(buscarProdutosQuery, termo, padraoPrefixo, padraoSubstring, empresaID)
	if err != nil {
		return nil, fmt.Errorf("falha ao buscar produtos: %w", err)
	}
	defer rows.Close()

	resultado := make([]ProdutoBusca, 0)
	for rows.Next() {
		var pb ProdutoBusca
		var codigo sql.NullString
		var rank int
		if err := rows.Scan(
			&pb.ID, &pb.Nome, &codigo,
			&pb.Categoria.ID, &pb.Categoria.Codigo, &pb.Categoria.Nome,
			&rank,
		); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de busca de produtos: %w", err)
		}
		if codigo.Valid {
			c := codigo.String
			pb.Codigo = &c
		}
		resultado = append(resultado, pb)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar busca de produtos: %w", err)
	}
	return resultado, nil
}

// buscarProdutoPorCodigoQuery resolve um Produto pelo Código de Identificação
// EXATO (Story 4.5, spec-4-5, FR-35): `WHERE p.codigo = $1` — igualdade
// case-sensitive, coerente com o índice único parcial `idx_produtos_codigo`,
// NUNCA `LIKE`/`ILIKE` nem `escaparCoringasLike` (o valor é uma chave lida de
// um QR Code / código de barras físico, não um padrão de busca ranqueada).
// Mesma projeção e `JOIN categorias` de buscarProdutosQuery.
const buscarProdutoPorCodigoQuery = `
	SELECT p.id, p.nome, p.codigo, c.id, c.codigo, c.nome
	FROM produtos p
	JOIN categorias c ON c.id = p.categoria_id
	WHERE p.codigo = $1 AND p.deleted_at IS NULL AND p.empresa_id = $2`

// BuscarProdutoPorCodigo devolve o Produto cujo `codigo` é EXATAMENTE igual a
// `codigo` (Story 4.5, spec-4-5, FR-35) — a resolução do valor lido de um
// QR Code / código de barras físico para o detalhe do Produto (Story 4.4).
// `codigo` é assumido não-vazio e já trimado: essa validação é
// responsabilidade do chamador (BuscarProdutoPorCodigoHandler); esta função
// nunca devolve erro de validação. `sql.ErrNoRows` (nenhum Produto com aquele
// código exato) -> ErrProdutoNaoEncontrado, mesmo colapso de
// ObterProdutoHandler; qualquer outro erro -> erro de banco cru para o 500
// genérico do handler.
func BuscarProdutoPorCodigo(db *sql.DB, empresaID string, codigo string) (ProdutoBusca, error) {
	var pb ProdutoBusca
	var codigoLido sql.NullString
	err := db.QueryRow(buscarProdutoPorCodigoQuery, codigo, empresaID).Scan(
		&pb.ID, &pb.Nome, &codigoLido,
		&pb.Categoria.ID, &pb.Categoria.Codigo, &pb.Categoria.Nome,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProdutoBusca{}, ErrProdutoNaoEncontrado
		}
		return ProdutoBusca{}, fmt.Errorf("falha ao buscar produto por código: %w", err)
	}
	if codigoLido.Valid {
		c := codigoLido.String
		pb.Codigo = &c
	}
	return pb, nil
}

// ListarCategorias devolve as categorias DA EMPRESA `empresaID` ordenadas
// por `codigo` ascendente (Story 3.1, AC4) — a lista da qual o formulário de
// cadastro seleciona, nunca digitável livremente. Cada Empresa recebe a
// própria cópia das 25 linhas padrão em services.ProvisionarEmpresa (Story
// 9.1); o molde dessa cópia vive em `categorias_padrao`, tabela própria desde
// a Story 9.4 (migration 000035) — antes eram linhas `empresa_id IS NULL`
// dentro desta mesma tabela, o que impedia `empresa_id` de virar NOT NULL.
// Molde nenhum aparece aqui. Lista vazia não é erro.
func ListarCategorias(db *sql.DB, empresaID string) ([]Categoria, error) {
	rows, err := db.Query(
		`SELECT id, codigo, nome FROM categorias WHERE empresa_id = $1 ORDER BY codigo ASC`,
		empresaID)
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
