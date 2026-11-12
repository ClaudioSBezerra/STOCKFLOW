package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

// Handlers de Importação em massa via planilha padronizada (Story 3.3,
// spec-3-3) e atualização por código (Story 3.4, spec-3-4). Reaproveita, do
// mesmo pacote `services`, `validarDimensao`/`unidadesDimensaoValidas`/
// `limiteNumeric103`/`limiteNumeric103Texto`/`nomeValidoParaTemplate`
// (produtos.go/nomenclatura.go) e `pqInvalidTextRepresentation`/
// `ErrEstoqueValidacao` (estoques.go).
//
// CORREÇÃO DE BUG (2026-11-12, feedback real — Ferreira Costa importou um
// catálogo inteiro e nenhum Produto ganhou código interno): a célula
// "Código" da planilha NUNCA mais vira `produtos.codigo` diretamente — esse
// campo é, a partir de agora, SEMPRE gerado pelo servidor via
// `proximoCodigoProduto` (produtos.go), exatamente como o cadastro manual
// (`CriarProduto`). O valor que a planilha traz (código legado/do fornecedor,
// ex. "B.1.02.C009") vai para `codigo_fornecedor` — campo já existente
// (Story 10.3), nunca usado pela importação até aqui. O match de
// reimportação (Story 3.4, FR-11) passa a ser por `codigo_fornecedor`, não
// por `codigo`: o contrato do FR-11 sempre foi "bate exatamente com o valor
// armazenado", nunca exigiu que esse valor fosse sequencial. Todo Produto
// criado pela importação também ganha `template_id` do template "Genérico"
// da Empresa (`buscarTemplateGenericoID`) — mesma obrigatoriedade que
// `CriarProduto` já aplica ao cadastro manual (Story 10.1, AC2); antes desta
// correção a importação era o único caminho de cadastro sem template.
//
// `pqForeignKeyViolation` (estoques.go/produtos.go) NÃO é referenciada aqui,
// de propósito: `categoria_id` é resolvido por SELECT e `estoque_id` por
// encontrarOuCriarEstoque, os dois DENTRO da mesma transação e IMEDIATAMENTE
// antes dos INSERTs/UPDATEs em `produtos`/`produto_estoque` — quando essas
// escritas rodam, as duas FKs já foram comprovadas válidas na mesma
// transação, então uma violação de FK nesse ponto não é um caminho
// praticamente alcançável (viraria erro de infraestrutura genérico via
// `%w`, não um caso dedicado). O `UPDATE produtos` do ramo de atualização
// também não pode violar `idx_produtos_codigo` (migration 000017): o
// `codigo` interno do Produto encontrado nunca é tocado pela atualização
// (só `codigo_fornecedor` é sobrescrito).
//
// `pqUniqueViolation` (auth.go) JÁ É referenciada aqui, também de propósito:
// o caminho de CRIAÇÃO de processarProximaLinha pode perder uma corrida
// contra outra transação concorrente que gerou o MESMO código sequencial
// (duas linhas, cada uma reivindicada por uma chamada concorrente de
// continuar — `FOR UPDATE SKIP LOCKED` deixa as duas avançarem; o `UPDATE`
// em `contadores_produto` dentro de `proximoCodigoProduto` serializa a
// maioria dos casos via lock de linha, mas a janela entre esse UPDATE e o
// COMMIT do INSERT ainda permite uma colisão rara). Em vez de abortar a
// linha inteira como erro de infraestrutura, um `SAVEPOINT` antes de cada
// tentativa permite desfazer só o avanço do contador + o INSERT que falhou e
// tentar de novo (até um limite) — nunca delega para o caminho de
// atualização: uma colisão de código AUTO-GERADO nunca significa "mesmo
// Produto", só azar de concorrência (ver o comentário do SAVEPOINT em
// processarProximaLinha).
//
// O cabeçalho fixo da planilha É VALIDADO PELO HANDLER
// (handlers/importacoes.go), não aqui: `CriarImportacao` assume `linhas[0]`
// já validado — ver o comentário da função.

// Ordem das 17 colunas fixas da planilha padronizada — mesma ordem de
// CabecalhoEsperado, usada tanto para ler cada célula de `dados` quanto para
// montar o INSERT em `produtos`.
const (
	colNome = iota
	colCodigo
	colCategoria
	colComprimentoValor
	colComprimentoUnidade
	colLarguraValor
	colLarguraUnidade
	colDiametroValor
	colDiametroUnidade
	colAlturaValor
	colAlturaUnidade
	colEspessuraValor
	colEspessuraUnidade
	colQuantidade
	colEstoque
	colObservacoes
	// colPrecoUnitario (feedback Ferreira Costa, direção, 2026-11-11): 17ª
	// coluna, sempre opcional — célula vazia grava NULL, mesma regra de
	// validarPrecoUnitario (produtos.go). Acrescentada ao FINAL da planilha
	// padronizada para não reordenar nenhuma coluna já documentada.
	colPrecoUnitario
)

// numeroColunas é a contagem fixa de colunas da planilha padronizada — mesma
// contagem de CabecalhoEsperado, e o tamanho fixo do array `dados` gravado em
// cada `importacao_linhas` (17 strings brutas, índices colNome..colPrecoUnitario).
const numeroColunas = colPrecoUnitario + 1

// CabecalhoEsperado é o cabeçalho fixo (17 colunas, nesta ordem exata) que
// handlers.CriarImportacaoHandler compara (com trim, sem normalizar caixa)
// contra a primeira linha da planilha recebida — citado na mensagem de erro
// quando o cabeçalho está fora do padrão. "Preço Unitário" (feedback
// Ferreira Costa, direção, 2026-11-11) é sempre opcional.
var CabecalhoEsperado = []string{
	"Nome", "Código", "Categoria",
	"Comprimento (valor)", "Comprimento (unidade)",
	"Largura (valor)", "Largura (unidade)",
	"Diâmetro (valor)", "Diâmetro (unidade)",
	"Altura (valor)", "Altura (unidade)",
	"Espessura (valor)", "Espessura (unidade)",
	"Quantidade", "Estoque", "Observações", "Preço Unitário",
}

// Importacao é a projeção somente-leitura de uma linha de `importacoes`,
// devolvida pelos 3 endpoints de importação.
//
// `ProximaLinhaPendente` é o número REAL da linha (mesma convenção de
// `numero_linha` — cabeçalho = 1) mais antiga ainda `pendente`/`processando`
// desta importação, ou `nil` quando não sobra nenhuma (importação
// `concluida`, ou uma `em_andamento` cuja última linha pendente acabou de
// ser reivindicada por outra chamada concorrente). Existe especificamente
// para o banner de retomada do frontend ("Última importação parou na linha
// N de M") — `criados+rejeitados` NUNCA é um substituto válido para N:
// `numero_linha` começa em 2, não em 1 (cabeçalho = 1), e tem gaps (linhas
// 100% em branco são descartadas antes de gravar `importacao_linhas`, então
// nem toda linha real da planilha vira uma linha de `importacao_linhas`) —
// um simples "quantas linhas já processei" nunca aponta pra célula certa.
type Importacao struct {
	ID                   string `json:"id"`
	Status               string `json:"status"`
	TotalLinhas          int    `json:"total_linhas"`
	ProximaLinhaPendente *int   `json:"proxima_linha_pendente"`
}

// LinhaRejeitada é um item de `RelatorioImportacao.LinhasRejeitadas`: o
// número real da linha na planilha (cabeçalho = 1) e o motivo da rejeição.
type LinhaRejeitada struct {
	Linha int    `json:"linha"`
	Erro  string `json:"erro"`
}

// RelatorioImportacao agrega o resultado do processamento de uma importação
// no momento em que é consultado — pode refletir um processamento parcial
// quando devolvido no meio de uma importação `em_andamento`
// (GET /api/importacoes/ultima antes de continuar).
type RelatorioImportacao struct {
	Criados int `json:"criados"`
	// Atualizados conta as linhas cujo `código` casou com um Produto já
	// existente (Story 3.4, FR-11) — o Produto é sobrescrito com os valores
	// da linha, nunca duplicado. NUNCA soma em Criados, mesmo quando o
	// `UPDATE` não muda nenhum valor observável (roda de qualquer forma,
	// sem diff prévio contra o estado anterior).
	Atualizados      int              `json:"atualizados"`
	Rejeitados       int              `json:"rejeitados"`
	LinhasRejeitadas []LinhaRejeitada `json:"linhas_rejeitadas"`
}

// ErroImportacaoValidacao é o erro de validação de UMA linha da planilha
// (nome/código ausente ou longo demais, quantidade fora do intervalo, uma
// das 5 dimensões incompleta ou fora do intervalo aceito). Nunca propaga
// como erro de função — processarPendentes captura `.Error()` e grava em
// `importacao_linhas.erro`, marcando só aquela linha como `rejeitada`.
type ErroImportacaoValidacao struct {
	Mensagem string
}

func (e *ErroImportacaoValidacao) Error() string { return e.Mensagem }

// ErrImportacaoNaoEncontrada indica `id` de Importacao inexistente OU
// malformado (não-UUID, `pq` SQLSTATE 22P02) — mesmo padrão de
// ErrEstoqueNaoEncontrado/ErrProdutoNaoEncontrado. Mapeado para 404 NOT_FOUND
// por ContinuarImportacaoHandler.
var ErrImportacaoNaoEncontrada = errors.New("importação não encontrada")

// linhaBruta é uma linha de dado da planilha já normalizada para exatamente
// numeroColunas células (preenchidas com "" além do que a linha realmente
// tinha — excelize corta células vazias à direita) e associada ao seu número
// real na planilha (cabeçalho = 1, então a primeira linha de dado = 2).
type linhaBruta struct {
	numero int
	dados  [numeroColunas]string
}

// prepararLinhasDados normaliza cada linha de dado (já sem o cabeçalho) para
// numeroColunas células e descarta as 100% em branco (todas vazias após
// trim) — elas não contam em `total_linhas` nem geram gap perceptível ao
// usuário (spec-3-3). O número real da linha é calculado pela posição
// original em `linhasDeDados` (índice 0 = linha real 2), não pela posição
// depois do filtro.
func prepararLinhasDados(linhasDeDados [][]string) []linhaBruta {
	preparadas := make([]linhaBruta, 0, len(linhasDeDados))
	for i, linha := range linhasDeDados {
		var dados [numeroColunas]string
		for c := 0; c < numeroColunas && c < len(linha); c++ {
			dados[c] = linha[c]
		}
		todasVazias := true
		for _, v := range dados {
			if strings.TrimSpace(v) != "" {
				todasVazias = false
				break
			}
		}
		if todasVazias {
			continue
		}
		preparadas = append(preparadas, linhaBruta{numero: i + 2, dados: dados})
	}
	return preparadas
}

// CriarImportacao grava `importacoes` + uma `importacao_linhas` (status
// `pendente`) por linha de dado não-em-branco de `linhas[1:]`, numa única
// transação, e então processa tudo sequencialmente (processarPendentes).
//
// `linhas[0]` (o cabeçalho) já foi validado por
// handlers.CriarImportacaoHandler contra CabecalhoEsperado ANTES desta
// função ser chamada — nenhuma validação de cabeçalho acontece aqui, e
// `linhas[0]` nunca é lido por esta função (só `linhas[1:]`).
func CriarImportacao(db *sql.DB, empresaID string, criadoPor, nomeArquivo string, linhas [][]string) (Importacao, RelatorioImportacao, error) {
	var linhasDeDados [][]string
	if len(linhas) > 1 {
		linhasDeDados = linhas[1:]
	}
	preparadas := prepararLinhasDados(linhasDeDados)

	tx, err := db.Begin()
	if err != nil {
		return Importacao{}, RelatorioImportacao{}, fmt.Errorf("falha ao iniciar transação de importação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	var importacaoID string
	const insertImportacao = `
		INSERT INTO importacoes (nome_arquivo, total_linhas, criado_por, empresa_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id`
	if err := tx.QueryRow(insertImportacao, nomeArquivo, len(preparadas), criadoPor, empresaID).Scan(&importacaoID); err != nil {
		return Importacao{}, RelatorioImportacao{}, fmt.Errorf("falha ao inserir importação: %w", err)
	}

	const insertLinha = `
		INSERT INTO importacao_linhas (importacao_id, numero_linha, dados)
		VALUES ($1, $2, $3)`
	for _, l := range preparadas {
		dadosJSON, err := json.Marshal(l.dados)
		if err != nil {
			return Importacao{}, RelatorioImportacao{}, fmt.Errorf("falha ao serializar dados da linha %d: %w", l.numero, err)
		}
		if _, err := tx.Exec(insertLinha, importacaoID, l.numero, dadosJSON); err != nil {
			return Importacao{}, RelatorioImportacao{}, fmt.Errorf("falha ao inserir linha %d: %w", l.numero, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return Importacao{}, RelatorioImportacao{}, fmt.Errorf("falha ao commitar importação: %w", err)
	}

	return processarECarregar(db, empresaID, importacaoID)
}

// ContinuarImportacao retoma o processamento de uma importação existente —
// só as linhas ainda `pendente`/`processando`, nunca reprocessando uma linha
// já `criado`/`rejeitada` (a condição `status IN ('pendente','processando')`
// de processarPendentes já garante isso). `id` inexistente ou malformado
// (não-UUID) -> ErrImportacaoNaoEncontrada, verificado ANTES de processar —
// sem essa checagem prévia, um id inexistente processaria silenciosamente
// zero linhas e devolveria um relatório vazio "de sucesso", mascarando o 404.
func ContinuarImportacao(db *sql.DB, empresaID string, id string) (Importacao, RelatorioImportacao, error) {
	if _, err := buscarImportacao(db, empresaID, id); err != nil {
		return Importacao{}, RelatorioImportacao{}, err
	}
	return processarECarregar(db, empresaID, id)
}

// ObterUltimaImportacao devolve a importação mais recente DA EMPRESA
// `empresaID` (Story 9.1, AD-20 — nunca a de outro cliente) (`ORDER BY
// iniciado_em DESC LIMIT 1`) com o relatório agregado do seu estado atual —
// `em_andamento` (parou em algum ponto) ou `concluida`. Nenhuma importação
// registrada -> `(nil, RelatorioImportacao{}, nil)`, nunca erro.
func ObterUltimaImportacao(db *sql.DB, empresaID string) (*Importacao, RelatorioImportacao, error) {
	var imp Importacao
	const selectUltima = `
		SELECT id, status, total_linhas FROM importacoes
		WHERE empresa_id = $1
		ORDER BY iniciado_em DESC LIMIT 1`
	if err := db.QueryRow(selectUltima, empresaID).Scan(&imp.ID, &imp.Status, &imp.TotalLinhas); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, RelatorioImportacao{}, nil
		}
		return nil, RelatorioImportacao{}, fmt.Errorf("falha ao buscar última importação: %w", err)
	}
	proximaLinha, err := proximaLinhaPendente(db, imp.ID)
	if err != nil {
		return nil, RelatorioImportacao{}, err
	}
	imp.ProximaLinhaPendente = proximaLinha

	relatorio, err := montarRelatorio(db, imp.ID)
	if err != nil {
		return nil, RelatorioImportacao{}, err
	}
	return &imp, relatorio, nil
}

// buscarImportacao lê `id`/`status`/`total_linhas`/`proxima_linha_pendente`
// de uma importação existente. `id` inexistente OU malformado (não-UUID,
// `pq` SQLSTATE 22P02) colapsam em ErrImportacaoNaoEncontrada.
func buscarImportacao(db *sql.DB, empresaID string, id string) (Importacao, error) {
	var imp Importacao
	const selectImportacao = `SELECT id, status, total_linhas FROM importacoes WHERE id = $1 AND empresa_id = $2`
	if err := db.QueryRow(selectImportacao, id, empresaID).Scan(&imp.ID, &imp.Status, &imp.TotalLinhas); err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return Importacao{}, ErrImportacaoNaoEncontrada
		}
		return Importacao{}, fmt.Errorf("falha ao buscar importação: %w", err)
	}
	proximaLinha, err := proximaLinhaPendente(db, imp.ID)
	if err != nil {
		return Importacao{}, err
	}
	imp.ProximaLinhaPendente = proximaLinha
	return imp, nil
}

// proximaLinhaPendente devolve o menor `numero_linha` ainda `pendente`/
// `processando` de `importacaoID` — a linha real onde uma importação
// `em_andamento` parou (ver o comentário de Importacao.ProximaLinhaPendente
// para o porquê disto, e não `criados+rejeitados`, ser o valor certo).
// `MIN()` sobre um conjunto vazio devolve SQL NULL -> `nil` (importação
// `concluida`, ou toda linha não-terminal momentaneamente reivindicada por
// outra transação em andamento).
func proximaLinhaPendente(db *sql.DB, importacaoID string) (*int, error) {
	const selectProximaLinha = `
		SELECT MIN(numero_linha) FROM importacao_linhas
		WHERE importacao_id = $1 AND status IN ('pendente', 'processando')`
	var proxima sql.NullInt64
	if err := db.QueryRow(selectProximaLinha, importacaoID).Scan(&proxima); err != nil {
		return nil, fmt.Errorf("falha ao buscar próxima linha pendente: %w", err)
	}
	if !proxima.Valid {
		return nil, nil
	}
	n := int(proxima.Int64)
	return &n, nil
}

// processarECarregar chama processarPendentes e, em caso de sucesso, recarrega
// a Importacao (para refletir um eventual status='concluida' setado pelo
// próprio processarPendentes) — usado por CriarImportacao e
// ContinuarImportacao, os dois pontos de entrada que disparam processamento.
func processarECarregar(db *sql.DB, empresaID string, importacaoID string) (Importacao, RelatorioImportacao, error) {
	relatorio, err := processarPendentes(db, empresaID, importacaoID)
	if err != nil {
		return Importacao{}, RelatorioImportacao{}, err
	}
	importacao, err := buscarImportacao(db, empresaID, importacaoID)
	if err != nil {
		return Importacao{}, RelatorioImportacao{}, err
	}
	return importacao, relatorio, nil
}

// montarRelatorio agrega o estado atual de `importacao_linhas` de uma
// importação: contagem por status (`criado`/`rejeitada`) e a lista completa
// de linhas rejeitadas (número real + motivo), ordenada por número de linha.
func montarRelatorio(db *sql.DB, importacaoID string) (RelatorioImportacao, error) {
	relatorio := RelatorioImportacao{LinhasRejeitadas: make([]LinhaRejeitada, 0)}

	const selectContagens = `
		SELECT status, count(*) FROM importacao_linhas
		WHERE importacao_id = $1
		GROUP BY status`
	rows, err := db.Query(selectContagens, importacaoID)
	if err != nil {
		return RelatorioImportacao{}, fmt.Errorf("falha ao agregar relatório de importação: %w", err)
	}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			rows.Close()
			return RelatorioImportacao{}, fmt.Errorf("falha ao ler contagem de status: %w", err)
		}
		switch status {
		case "criado":
			relatorio.Criados = n
		case "atualizado":
			relatorio.Atualizados = n
		case "rejeitada":
			relatorio.Rejeitados = n
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return RelatorioImportacao{}, fmt.Errorf("falha ao iterar contagens de status: %w", err)
	}
	rows.Close()

	const selectRejeitadas = `
		SELECT numero_linha, erro FROM importacao_linhas
		WHERE importacao_id = $1 AND status = 'rejeitada'
		ORDER BY numero_linha`
	linhaRows, err := db.Query(selectRejeitadas, importacaoID)
	if err != nil {
		return RelatorioImportacao{}, fmt.Errorf("falha ao listar linhas rejeitadas: %w", err)
	}
	defer linhaRows.Close()
	for linhaRows.Next() {
		var lr LinhaRejeitada
		var erro sql.NullString
		if err := linhaRows.Scan(&lr.Linha, &erro); err != nil {
			return RelatorioImportacao{}, fmt.Errorf("falha ao ler linha rejeitada: %w", err)
		}
		lr.Erro = erro.String
		relatorio.LinhasRejeitadas = append(relatorio.LinhasRejeitadas, lr)
	}
	if err := linhaRows.Err(); err != nil {
		return RelatorioImportacao{}, fmt.Errorf("falha ao iterar linhas rejeitadas: %w", err)
	}

	return relatorio, nil
}

// processarPendentes processa, uma de cada vez, todas as linhas
// `pendente`/`processando` de uma importação (processarProximaLinha), até
// não sobrar nenhuma reivindicável. Ao esvaziar sem erro, se não sobra
// nenhuma linha pendente/processando (nem a de outra chamada concorrente
// ainda em andamento), marca a importação `concluida`. Um erro de
// infraestrutura em qualquer iteração interrompe o laço inteiro e é
// devolvido ao chamador — a importação permanece `em_andamento`, retomável
// por uma futura chamada de ContinuarImportacao.
func processarPendentes(db *sql.DB, empresaID string, importacaoID string) (RelatorioImportacao, error) {
	for {
		encontrou, err := processarProximaLinha(db, empresaID, importacaoID)
		if err != nil {
			return RelatorioImportacao{}, err
		}
		if !encontrou {
			break
		}
	}

	var restantes int
	const selectRestantes = `
		SELECT count(*) FROM importacao_linhas
		WHERE importacao_id = $1 AND status IN ('pendente', 'processando')`
	if err := db.QueryRow(selectRestantes, importacaoID).Scan(&restantes); err != nil {
		return RelatorioImportacao{}, fmt.Errorf("falha ao verificar linhas restantes: %w", err)
	}
	if restantes == 0 {
		const concluir = `
			UPDATE importacoes SET status = 'concluida', concluido_em = now()
			WHERE id = $1 AND status = 'em_andamento' AND empresa_id = $2`
		if _, err := db.Exec(concluir, importacaoID, empresaID); err != nil {
			return RelatorioImportacao{}, fmt.Errorf("falha ao concluir importação: %w", err)
		}
	}

	return montarRelatorio(db, importacaoID)
}

// processarProximaLinha reivindica e resolve UMA linha `pendente`/
// `processando` de `importacaoID`, numa única transação que cobre o
// reivindicar-até-resolver inteiro (nunca um UPDATE de reivindicação solto
// fora de tx, que abriria janela de corrida entre duas chamadas concorrentes
// de continuar). `SELECT ... FOR UPDATE SKIP LOCKED` pula qualquer linha
// ainda sendo processada de verdade por outra transação em andamento (seu
// `FOR UPDATE` mantém o lock até committar ou desfazer).
//
// O `UPDATE ... SET status = 'processando'` feito logo abaixo NUNCA fica
// visível para outra sessão nesse meio-tempo — sob READ COMMITTED (padrão do
// Postgres), um UPDATE não commitado é invisível a qualquer transação que
// não seja a que o executou; só a própria transação, na sua própria leitura,
// enxerga esse valor antes de commitar. Se o processo morrer/a conexão cair
// antes do commit final (`criado`/`rejeitada`), o Postgres desfaz a
// transação INTEIRA automaticamente — a linha volta a exibir seu último
// status COMMITADO (tipicamente `pendente`, já que este código só commita
// junto com um status terminal), nunca `processando`. A cláusula `status IN
// ('pendente', 'processando')` da consulta abaixo é o que uma chamada futura
// usa para reivindicar essa linha de novo; `processando` entra nela como
// defesa em profundidade (nenhum caminho deste código commita deixando uma
// linha em `processando` — o status só é observável por quem está DENTRO da
// mesma transação, antes dela commitar).
//
// Devolve `(false, nil)` quando não sobra nenhuma linha reivindicável — o
// sinal para processarPendentes sair do laço. Um erro de VALIDAÇÃO da linha
// (campo inválido, categoria não encontrada, nome de Estoque inválido) marca
// só aquela linha como `rejeitada` e commita, devolvendo `(true, nil)` para
// a próxima iteração seguir; um erro de INFRAESTRUTURA desfaz a transação e
// é devolvido ao chamador, interrompendo o laço inteiro.
func processarProximaLinha(db *sql.DB, empresaID string, importacaoID string) (bool, error) {
	tx, err := db.Begin()
	if err != nil {
		return false, fmt.Errorf("falha ao iniciar transação de processamento: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	var linhaID string
	var numeroLinha int
	var dadosRaw []byte
	const selectProximaLinha = `
		SELECT id, numero_linha, dados FROM importacao_linhas
		WHERE importacao_id = $1 AND status IN ('pendente', 'processando')
		ORDER BY numero_linha
		FOR UPDATE SKIP LOCKED
		LIMIT 1`
	err = tx.QueryRow(selectProximaLinha, importacaoID).Scan(&linhaID, &numeroLinha, &dadosRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("falha ao reivindicar próxima linha de importação: %w", err)
	}

	if _, err := tx.Exec(`UPDATE importacao_linhas SET status = 'processando' WHERE id = $1`, linhaID); err != nil {
		return false, fmt.Errorf("falha ao marcar linha %d como processando: %w", numeroLinha, err)
	}

	var dados []string
	if err := json.Unmarshal(dadosRaw, &dados); err != nil {
		return false, fmt.Errorf("falha ao decodificar dados da linha %d: %w", numeroLinha, err)
	}

	validada, erroValidacao := validarLinhaImportacao(dados)
	if erroValidacao != nil {
		return true, rejeitarECommitar(tx, linhaID, numeroLinha, erroValidacao.Error())
	}

	var categoriaID string
	const selectCategoria = `
		SELECT id FROM categorias
		WHERE lower(btrim(nome)) = lower(btrim($1)) AND empresa_id = $2`
	err = tx.QueryRow(selectCategoria, validada.categoriaTexto, empresaID).Scan(&categoriaID)
	if errors.Is(err, sql.ErrNoRows) {
		motivo := fmt.Sprintf("categoria %q não encontrada", validada.categoriaTexto)
		return true, rejeitarECommitar(tx, linhaID, numeroLinha, motivo)
	}
	if err != nil {
		return false, fmt.Errorf("falha ao resolver categoria da linha %d: %w", numeroLinha, err)
	}

	// Match por código do fornecedor (Story 3.4, FR-11; redesenhado nesta
	// correção de bug — o `codigo` interno agora é sempre gerado pelo
	// servidor, nunca mais vem da planilha): só quando a linha traz um
	// código do fornecedor não-vazio — vazio sempre segue para o caminho de
	// criação abaixo, sem nenhuma busca (nunca casa por `nome`).
	if validada.codigoFornecedor.Valid {
		// `codigo_fornecedor` não tem índice único (Story 10.3) — diferente
		// de `codigo` (que tinha `idx_produtos_codigo`, migration 000017),
		// nada no banco serializa duas transações concorrentes decidindo,
		// ao mesmo tempo, "nenhum Produto ainda com este código do
		// fornecedor" para o MESMO valor (ex. duas linhas da mesma leva
		// repetindo o mesmo "Código" da planilha, cada uma reivindicada por
		// uma chamada concorrente de continuar). Sem serialização, as duas
		// cairiam no caminho de criação e gerariam dois Produtos distintos
		// com o mesmo código do fornecedor — exatamente a duplicata que o
		// FR-11 existe para evitar. `pg_advisory_xact_lock` trava só por
		// (empresa, código do fornecedor), dura a transação inteira e libera
		// sozinho no commit/rollback: a segunda transação bloqueia aqui até
		// a primeira commitar, e então enxerga (READ COMMITTED) o Produto
		// que ela acabou de criar, seguindo para o ramo de atualização.
		const travarCodigoFornecedor = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`
		if _, err := tx.Exec(travarCodigoFornecedor, empresaID+":"+validada.codigoFornecedor.String); err != nil {
			return false, fmt.Errorf("falha ao travar código do fornecedor da linha %d: %w", numeroLinha, err)
		}

		produtoExistenteID, produtoExistenteTemplateID, produtoInativo, encontrado, err := buscarProdutoPorCodigoFornecedor(tx, empresaID, validada.codigoFornecedor.String)
		if err != nil {
			return false, fmt.Errorf("falha ao buscar produto existente por código do fornecedor da linha %d: %w", numeroLinha, err)
		}
		if encontrado {
			if produtoInativo {
				return true, rejeitarECommitar(tx, linhaID, numeroLinha, motivoProdutoInativoImportacao)
			}
			return processarLinhaDeAtualizacao(tx, empresaID, linhaID, numeroLinha, validada, categoriaID, produtoExistenteID, produtoExistenteTemplateID)
		}
	}

	estoque, err := encontrarOuCriarEstoque(tx, empresaID, validada.estoqueNome)
	if errors.Is(err, ErrEstoqueValidacao) || errors.Is(err, ErrEmpresaSemFilial) {
		return true, rejeitarECommitar(tx, linhaID, numeroLinha, err.Error())
	}
	if err != nil {
		return false, fmt.Errorf("falha ao encontrar/criar estoque da linha %d: %w", numeroLinha, err)
	}

	templateID, err := buscarTemplateGenericoID(tx, empresaID)
	if err != nil {
		return false, fmt.Errorf("falha ao resolver template genérico da linha %d: %w", numeroLinha, err)
	}

	const insertProduto = `
		INSERT INTO produtos (
			nome, codigo, codigo_fornecedor, categoria_id, template_id, observacoes,
			comprimento_valor, comprimento_unidade,
			largura_valor, largura_unidade,
			diametro_valor, diametro_unidade,
			altura_valor, altura_unidade,
			espessura_valor, espessura_unidade,
			preco_unitario,
			empresa_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		RETURNING id`
	// O código interno é SEMPRE gerado pelo servidor (proximoCodigoProduto),
	// nunca aceito da planilha (correção de bug 2026-11-12 — ver o comentário
	// no topo do arquivo). Colisão no índice único de `codigo` é rara (o
	// UPDATE em `contadores_produto` dentro de proximoCodigoProduto serializa
	// a maior parte da concorrência via lock de linha, mesma garantia de
	// CriarProduto), mas ainda possível na janela entre esse UPDATE e o
	// COMMIT do INSERT — até `maxTentativasCodigoImportacao` tentativas,
	// mesma margem de CriarProduto (produtos.go). `SAVEPOINT` antes de CADA
	// tentativa (não só antes do INSERT): um `ROLLBACK TO SAVEPOINT` precisa
	// desfazer tanto o avanço do contador quanto o INSERT que falhou, para
	// que a tentativa seguinte releia o contador já avançado pela transação
	// vencedora. Diferente da versão anterior desta função, uma colisão AQUI
	// nunca delega para processarLinhaDeAtualizacao — os dois Produtos não
	// têm nenhuma relação entre si, só azar de concorrência.
	const maxTentativasCodigoImportacao = 3
	var produtoID string
	for tentativa := 1; ; tentativa++ {
		if _, err := tx.Exec(`SAVEPOINT sp_insert_produto`); err != nil {
			return false, fmt.Errorf("falha ao criar savepoint antes de inserir produto da linha %d: %w", numeroLinha, err)
		}

		codigoInterno, err := proximoCodigoProduto(tx, empresaID)
		if err != nil {
			return false, fmt.Errorf("falha ao gerar código do produto da linha %d: %w", numeroLinha, err)
		}

		err = tx.QueryRow(insertProduto,
			validada.nome, codigoInterno, validada.codigoFornecedor, categoriaID, templateID, validada.observacoes,
			validada.comprimentoValor, validada.comprimentoUnidade,
			validada.larguraValor, validada.larguraUnidade,
			validada.diametroValor, validada.diametroUnidade,
			validada.alturaValor, validada.alturaUnidade,
			validada.espessuraValor, validada.espessuraUnidade,
			validada.precoUnitario,
			empresaID,
		).Scan(&produtoID)
		if err == nil {
			break
		}

		var pqErr *pq.Error
		if !errors.As(err, &pqErr) || pqErr.Code != pqUniqueViolation {
			return false, fmt.Errorf("falha ao inserir produto da linha %d: %w", numeroLinha, err)
		}
		if _, errRollback := tx.Exec(`ROLLBACK TO SAVEPOINT sp_insert_produto`); errRollback != nil {
			return false, fmt.Errorf("falha ao reverter savepoint da linha %d: %w", numeroLinha, errRollback)
		}
		if tentativa >= maxTentativasCodigoImportacao {
			return false, fmt.Errorf("não foi possível gerar um código de produto único para a linha %d após %d tentativas", numeroLinha, maxTentativasCodigoImportacao)
		}
	}

	const insertProdutoEstoque = `
		INSERT INTO produto_estoque (produto_id, estoque_id, quantidade)
		VALUES ($1, $2, $3)`
	if _, err := tx.Exec(insertProdutoEstoque, produtoID, estoque.ID, validada.quantidade); err != nil {
		return false, fmt.Errorf("falha ao inserir produto_estoque da linha %d: %w", numeroLinha, err)
	}

	const marcarCriada = `UPDATE importacao_linhas SET status = 'criado', produto_id = $1 WHERE id = $2`
	if _, err := tx.Exec(marcarCriada, produtoID, linhaID); err != nil {
		return false, fmt.Errorf("falha ao marcar linha %d como criada: %w", numeroLinha, err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("falha ao commitar linha %d: %w", numeroLinha, err)
	}
	return true, nil
}

// buscarProdutoPorCodigoFornecedor busca o Produto (id + template_id) cujo
// `codigo_fornecedor` bate exatamente com `codigoFornecedor` (já trimado,
// não-vazio) — usado pelo match de processarProximaLinha (Story 3.4, FR-11,
// redesenhado nesta correção de bug: antes casava por `codigo`, o código
// interno; agora casa pelo valor que a planilha realmente carrega).
// `encontrado=false` (sql.ErrNoRows) nunca é erro — significa "nenhum
// Produto com esse código do fornecedor ainda", que o chamador trata como
// sinal para seguir o caminho de criação.
//
// Diferente de `codigo` (único por Empresa via `idx_produtos_codigo`),
// `codigo_fornecedor` NÃO tem índice único (Story 10.3: texto livre, sem
// checagem de unicidade) — `ORDER BY criado_em, id LIMIT 1` torna o match
// determinístico mesmo que dois Produtos acabem com o mesmo valor (prioriza
// o mais antigo, mesmo critério de desempate de encontrarOuCriarEstoque).
//
// Story 16.2: `inativo` sinaliza Produto inativado (encontrado, mas a linha
// deve ser rejeitada).
func buscarProdutoPorCodigoFornecedor(tx *sql.Tx, empresaID string, codigoFornecedor string) (id string, templateID sql.NullString, inativo bool, encontrado bool, err error) {
	const selectProdutoPorCodigoFornecedor = `
		SELECT id, template_id, inativado_em IS NOT NULL FROM produtos
		WHERE codigo_fornecedor = $1 AND deleted_at IS NULL AND empresa_id = $2
		ORDER BY criado_em, id
		LIMIT 1`
	err = tx.QueryRow(selectProdutoPorCodigoFornecedor, codigoFornecedor, empresaID).Scan(&id, &templateID, &inativo)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.NullString{}, false, false, nil
	}
	if err != nil {
		return "", sql.NullString{}, false, false, err
	}
	return id, templateID, inativo, true, nil
}

// buscarTemplateGenericoID resolve o id do template "Genérico" (marcador
// `[NOME LIVRE]`, Story 10.1, AD-34) da Empresa — toda Empresa tem ao menos
// uma linha com esse marcador (ProvisionarEmpresa/CopiarListasPadrao,
// migration 000036). Usado pela importação para dar a MESMA garantia que
// `CriarProduto` já aplica ao cadastro manual (template sempre presente) —
// prioriza `subtipo = 'Genérico'` (o semeado por padrão) para o caso raro de
// a Empresa ter mais de um marcador `[NOME LIVRE]` sob outro subtipo (ex. um
// fallback customizado renomeado, nomenclatura_crud.go).
func buscarTemplateGenericoID(tx *sql.Tx, empresaID string) (string, error) {
	var id string
	const selectGenerico = `
		SELECT id FROM nomenclatura_templates
		WHERE empresa_id = $1 AND template = $2
		ORDER BY (subtipo = 'Genérico') DESC, subtipo ASC
		LIMIT 1`
	err := tx.QueryRow(selectGenerico, empresaID, TemplateGenericoMarcador).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("template genérico ausente para a empresa %s", empresaID)
	}
	if err != nil {
		return "", fmt.Errorf("falha ao buscar template genérico: %w", err)
	}
	return id, nil
}

// motivoProdutoInativoImportacao é o motivo da linha rejeitada por Produto inativo (Story 16.2).
const motivoProdutoInativoImportacao = "Produto inativo — reative antes de importar"

// processarLinhaDeAtualizacao é o ramo de UPDATE de processarProximaLinha
// (Story 3.4, FR-11): chamado quando o `código do fornecedor` da linha já
// casou com um Produto existente (`produtoExistenteID`), dentro da MESMA
// transação de reivindicação da linha. Nunca chama AtualizarNomeProduto
// (produtos.go) — escreve via UPDATE próprio, mas replica a MESMA regra de
// revalidação de template que aquela função aplica (Story 3.2): um
// `template_id` NÃO-nulo no Produto encontrado exige que `validada.nome`
// case o formato desse template, senão a linha é rejeitada e o Produto não
// é tocado.
//
// Sucesso: sobrescreve nome, código do fornecedor, categoria, observações e
// as 5 dimensões — `codigo` (o interno, sequencial) e `template_id` NUNCA
// são tocados, o que já estava no Produto sobrevive. Também faz upsert em
// `produto_estoque`
// (`ON CONFLICT (produto_id, estoque_id) DO UPDATE SET quantidade =
// EXCLUDED.quantidade` — substitui, nunca soma, e só no par Produto/Estoque
// desta linha; outros pares do mesmo Produto com outros Estoques ficam
// intactos). Marca a linha `atualizado` (nunca `criado`) e commita.
//
// ORDEM DELIBERADA: o Estoque da linha é resolvido (encontrarOuCriarEstoque)
// ANTES do `UPDATE produtos` — nunca depois. Um nome de Estoque inválido
// rejeita a linha via `rejeitarECommitar`, que COMMITA a transação; se o
// `UPDATE produtos` já tivesse rodado antes dessa checagem, esse commit
// gravaria a atualização do Produto mesmo com a linha terminando `rejeitada`
// (violando a garantia desta função: "o Produto não é alterado" numa
// rejeição). Mesma ordem do caminho de criação em processarProximaLinha, que
// resolve o Estoque antes do `INSERT INTO produtos` pelo mesmo motivo.
func processarLinhaDeAtualizacao(
	tx *sql.Tx, empresaID string, linhaID string, numeroLinha int, validada linhaValidada,
	categoriaID string, produtoExistenteID string, produtoExistenteTemplateID sql.NullString,
) (bool, error) {
	if produtoExistenteTemplateID.Valid {
		var templateTexto string
		const selectTemplate = `SELECT template FROM nomenclatura_templates WHERE id = $1 AND empresa_id = $2`
		if err := tx.QueryRow(selectTemplate, produtoExistenteTemplateID.String, empresaID).Scan(&templateTexto); err != nil {
			return false, fmt.Errorf("falha ao buscar template aplicado ao produto da linha %d: %w", numeroLinha, err)
		}
		if !nomeValidoParaTemplate(templateTexto, validada.nome) {
			return true, rejeitarECommitar(tx, linhaID, numeroLinha, "nome não corresponde ao formato do template aplicado a este produto")
		}
	}

	estoque, err := encontrarOuCriarEstoque(tx, empresaID, validada.estoqueNome)
	if errors.Is(err, ErrEstoqueValidacao) || errors.Is(err, ErrEmpresaSemFilial) {
		return true, rejeitarECommitar(tx, linhaID, numeroLinha, err.Error())
	}
	if err != nil {
		return false, fmt.Errorf("falha ao encontrar/criar estoque da linha %d: %w", numeroLinha, err)
	}

	const updateProduto = `
		UPDATE produtos SET
			nome = $1, codigo_fornecedor = $2, categoria_id = $3, observacoes = $4,
			comprimento_valor = $5, comprimento_unidade = $6,
			largura_valor = $7, largura_unidade = $8,
			diametro_valor = $9, diametro_unidade = $10,
			altura_valor = $11, altura_unidade = $12,
			espessura_valor = $13, espessura_unidade = $14,
			preco_unitario = $17
		WHERE id = $15 AND empresa_id = $16`
	if _, err := tx.Exec(updateProduto,
		validada.nome, validada.codigoFornecedor, categoriaID, validada.observacoes,
		validada.comprimentoValor, validada.comprimentoUnidade,
		validada.larguraValor, validada.larguraUnidade,
		validada.diametroValor, validada.diametroUnidade,
		validada.alturaValor, validada.alturaUnidade,
		validada.espessuraValor, validada.espessuraUnidade,
		produtoExistenteID, empresaID, validada.precoUnitario,
	); err != nil {
		return false, fmt.Errorf("falha ao atualizar produto da linha %d: %w", numeroLinha, err)
	}

	const upsertProdutoEstoque = `
		INSERT INTO produto_estoque (produto_id, estoque_id, quantidade)
		VALUES ($1, $2, $3)
		ON CONFLICT (produto_id, estoque_id) DO UPDATE SET quantidade = EXCLUDED.quantidade`
	if _, err := tx.Exec(upsertProdutoEstoque, produtoExistenteID, estoque.ID, validada.quantidade); err != nil {
		return false, fmt.Errorf("falha ao upsert produto_estoque da linha %d: %w", numeroLinha, err)
	}

	const marcarAtualizada = `UPDATE importacao_linhas SET status = 'atualizado', produto_id = $1 WHERE id = $2`
	if _, err := tx.Exec(marcarAtualizada, produtoExistenteID, linhaID); err != nil {
		return false, fmt.Errorf("falha ao marcar linha %d como atualizada: %w", numeroLinha, err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("falha ao commitar atualização da linha %d: %w", numeroLinha, err)
	}
	return true, nil
}

// rejeitarECommitar marca `linhaID` como `rejeitada` com `motivo` e commita a
// transação — usado pelos ramos de rejeição de processarProximaLinha (campo
// inválido, categoria não encontrada, nome de Estoque inválido) e de
// processarLinhaDeAtualizacao (Story 3.4: nome fora do formato do template
// aplicado, nome de Estoque inválido).
func rejeitarECommitar(tx *sql.Tx, linhaID string, numeroLinha int, motivo string) error {
	const rejeitar = `UPDATE importacao_linhas SET status = 'rejeitada', erro = $1 WHERE id = $2`
	if _, err := tx.Exec(rejeitar, motivo, linhaID); err != nil {
		return fmt.Errorf("falha ao rejeitar linha %d: %w", numeroLinha, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("falha ao commitar rejeição da linha %d: %w", numeroLinha, err)
	}
	return nil
}

// linhaValidada é o resultado, já pronto para INSERT, da validação dos 16
// campos brutos de uma linha (validarLinhaImportacao) — mesmos tipos usados
// por CriarProduto (sql.NullString/sql.NullFloat64 para os campos opcionais).
type linhaValidada struct {
	nome             string
	codigoFornecedor sql.NullString
	observacoes      sql.NullString
	quantidade       float64

	comprimentoValor, larguraValor, diametroValor, alturaValor, espessuraValor           sql.NullFloat64
	comprimentoUnidade, larguraUnidade, diametroUnidade, alturaUnidade, espessuraUnidade sql.NullString

	categoriaTexto string
	estoqueNome    string

	precoUnitario sql.NullFloat64
}

// validarLinhaImportacao valida os 17 campos brutos de uma linha da planilha
// contra as MESMAS regras de CriarProduto: nome (obrigatório, máx. 255
// runes), código do fornecedor (opcional, máx. 255 runes — a célula
// "Código" da planilha; o `codigo` interno nunca vem daqui, é sempre gerado
// pelo servidor em processarProximaLinha), quantidade (mesma checagem de
// QuantidadeInicial: >= 0 e <= limiteNumeric103) e as 5 dimensões via
// validarDimensao reaproveitada tal qual — a unidade é lowercased ANTES de
// checar contra unidadesDimensaoValidas (a planilha não impõe caixa). A
// resolução de categoria/Estoque acontece DEPOIS, em processarProximaLinha
// (dependem do banco) — aqui só os campos "de forma".
func validarLinhaImportacao(dados []string) (linhaValidada, error) {
	if len(dados) != numeroColunas {
		return linhaValidada{}, &ErroImportacaoValidacao{
			Mensagem: fmt.Sprintf("linha com %d colunas, esperado %d", len(dados), numeroColunas),
		}
	}

	nome := strings.TrimSpace(dados[colNome])
	if nome == "" || utf8.RuneCountInString(nome) > 255 {
		return linhaValidada{}, &ErroImportacaoValidacao{
			Mensagem: "nome é obrigatório e deve ter no máximo 255 caracteres",
		}
	}

	codigoFornecedorTrimado := strings.TrimSpace(dados[colCodigo])
	if utf8.RuneCountInString(codigoFornecedorTrimado) > 255 {
		return linhaValidada{}, &ErroImportacaoValidacao{
			Mensagem: "código deve ter no máximo 255 caracteres",
		}
	}
	var codigoFornecedor sql.NullString
	if codigoFornecedorTrimado != "" {
		codigoFornecedor = sql.NullString{String: codigoFornecedorTrimado, Valid: true}
	}

	var observacoes sql.NullString
	if observacoesTrimadas := strings.TrimSpace(dados[colObservacoes]); observacoesTrimadas != "" {
		observacoes = sql.NullString{String: observacoesTrimadas, Valid: true}
	}

	quantidadeTexto := strings.TrimSpace(dados[colQuantidade])
	if quantidadeTexto == "" {
		return linhaValidada{}, &ErroImportacaoValidacao{Mensagem: "quantidade é obrigatória"}
	}
	// Parsing só aceita ponto decimal — mesma convenção do resto do sistema
	// (a API JSON e os `<input type="number">` do formulário de cadastro
	// nunca fazem parsing de localidade). NÃO há substituição de vírgula por
	// ponto: essa heurística mascarava silenciosamente o separador de milhar
	// PT-BR ("1.500", com o dígito já usando ponto, nunca passava por ela;
	// mas uma vírgula em "1,500" virava "1.500" -> 1.5, um valor 1000x menor
	// que o pretendido, sem nenhum erro). `math.IsNaN`/`math.IsInf` cobrem o
	// caso de `strconv.ParseFloat` aceitar sem erro os literais "NaN"/"Inf"/
	// "+Inf"/"-Inf"/"Infinity".
	quantidade, err := strconv.ParseFloat(quantidadeTexto, 64)
	if err != nil || math.IsNaN(quantidade) || math.IsInf(quantidade, 0) {
		return linhaValidada{}, &ErroImportacaoValidacao{
			Mensagem: fmt.Sprintf("quantidade: valor inválido %q", quantidadeTexto),
		}
	}
	if quantidade < 0 {
		return linhaValidada{}, &ErroImportacaoValidacao{
			Mensagem: "quantidade deve ser maior ou igual a zero",
		}
	}
	if quantidade > limiteNumeric103 {
		return linhaValidada{}, &ErroImportacaoValidacao{
			Mensagem: fmt.Sprintf("quantidade deve ser no máximo %s", limiteNumeric103Texto),
		}
	}

	comprimentoValor, comprimentoUnidade, err := validarDimensaoLinha("comprimento", dados[colComprimentoValor], dados[colComprimentoUnidade])
	if err != nil {
		return linhaValidada{}, err
	}
	larguraValor, larguraUnidade, err := validarDimensaoLinha("largura", dados[colLarguraValor], dados[colLarguraUnidade])
	if err != nil {
		return linhaValidada{}, err
	}
	diametroValor, diametroUnidade, err := validarDimensaoLinha("diâmetro", dados[colDiametroValor], dados[colDiametroUnidade])
	if err != nil {
		return linhaValidada{}, err
	}
	alturaValor, alturaUnidade, err := validarDimensaoLinha("altura", dados[colAlturaValor], dados[colAlturaUnidade])
	if err != nil {
		return linhaValidada{}, err
	}
	espessuraValor, espessuraUnidade, err := validarDimensaoLinha("espessura", dados[colEspessuraValor], dados[colEspessuraUnidade])
	if err != nil {
		return linhaValidada{}, err
	}

	// Preço Unitário (feedback Ferreira Costa, direção, 2026-11-11): célula
	// vazia -> NULL, sem erro (sempre opcional). Mesma convenção de
	// quantidade: só ponto decimal, validado por validarPrecoUnitario
	// (produtos.go) — >= 0 e dentro do limite da coluna.
	var precoUnitario sql.NullFloat64
	if precoTexto := strings.TrimSpace(dados[colPrecoUnitario]); precoTexto != "" {
		preco, err := strconv.ParseFloat(precoTexto, 64)
		if err != nil || math.IsNaN(preco) || math.IsInf(preco, 0) {
			return linhaValidada{}, &ErroImportacaoValidacao{
				Mensagem: fmt.Sprintf("preço unitário: valor inválido %q", precoTexto),
			}
		}
		precoValidado, err := validarPrecoUnitario(&preco)
		if err != nil {
			var erroValidacaoProduto *ErroProdutoValidacao
			if errors.As(err, &erroValidacaoProduto) {
				return linhaValidada{}, &ErroImportacaoValidacao{Mensagem: erroValidacaoProduto.Mensagem}
			}
			return linhaValidada{}, err
		}
		precoUnitario = precoValidado
	}

	return linhaValidada{
		nome:             nome,
		codigoFornecedor: codigoFornecedor,
		observacoes:      observacoes,
		quantidade:       quantidade,

		comprimentoValor: comprimentoValor, comprimentoUnidade: comprimentoUnidade,
		larguraValor: larguraValor, larguraUnidade: larguraUnidade,
		diametroValor: diametroValor, diametroUnidade: diametroUnidade,
		alturaValor: alturaValor, alturaUnidade: alturaUnidade,
		espessuraValor: espessuraValor, espessuraUnidade: espessuraUnidade,

		categoriaTexto: strings.TrimSpace(dados[colCategoria]),
		estoqueNome:    dados[colEstoque],

		precoUnitario: precoUnitario,
	}, nil
}

// validarDimensaoLinha converte o par de células brutas (valor/unidade) de
// uma dimensão para *DimensaoInput e delega a validarDimensao (produtos.go,
// reaproveitada tal qual). A unidade é lowercased ANTES de checar contra
// unidadesDimensaoValidas — a planilha não impõe caixa ("MM"/"Mm"/"mm" são
// todos válidos), diferente do corpo JSON de POST /api/produtos.
func validarDimensaoLinha(campo, valorTexto, unidadeTexto string) (sql.NullFloat64, sql.NullString, error) {
	valorTrimado := strings.TrimSpace(valorTexto)
	unidadeTrimada := strings.ToLower(strings.TrimSpace(unidadeTexto))

	var valor *float64
	if valorTrimado != "" {
		// Mesma convenção de validarLinhaImportacao (quantidade): só ponto
		// decimal, sem heurística de vírgula, com guard contra
		// NaN/±Inf/Infinity — ver o comentário lá para o porquê.
		v, err := strconv.ParseFloat(valorTrimado, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return sql.NullFloat64{}, sql.NullString{}, &ErroImportacaoValidacao{
				Mensagem: fmt.Sprintf("%s: valor inválido %q", campo, valorTrimado),
			}
		}
		valor = &v
	}
	var unidade *string
	if unidadeTrimada != "" {
		unidade = &unidadeTrimada
	}

	nullValor, nullUnidade, err := validarDimensao(campo, &DimensaoInput{Valor: valor, Unidade: unidade})
	if err != nil {
		return sql.NullFloat64{}, sql.NullString{}, &ErroImportacaoValidacao{Mensagem: err.Error()}
	}
	return nullValor, nullUnidade, nil
}

// encontrarOuCriarEstoque resolve o Estoque de nome `nome` dentro da
// transação de processamento de uma linha: cria-o se ainda não existir,
// encontra-o se já existir. Reaproveita a MESMA expressão de
// `nome_normalizado` gerada pela migration 000008 (estoques.go, nunca
// alterado por esta story).
//
// `nome` vazio/>255 runes após o trim -> ErrEstoqueValidacao (mesma
// constante exportada de estoques.go) — processarProximaLinha trata como
// validação de linha (rejeitada), não como erro de infraestrutura.
//
// DUAS instruções separadas, de propósito — NÃO uma única query combinando
// `INSERT ... RETURNING` (CTE) e o `SELECT` de fallback num `UNION ALL`
// (correção pós-implementação: essa versão combinada tinha uma race
// condition real sob concorrência). Sob READ COMMITTED, uma instrução
// composta inteira (CTE + UNION ALL, mesmo com múltiplas cláusulas) enxerga
// UM ÚNICO snapshot tirado no início da instrução. Quando o INSERT bloqueia
// esperando outra transação que está inserindo o MESMO nome novo, e depois
// destrava porque essa outra transação commitou, a própria recheck de
// conflito do INSERT enxerga a linha recém-commitada (por isso `ON CONFLICT
// DO NOTHING` reconhece corretamente o conflito) — mas o ramo `SELECT ...
// FROM estoques WHERE nome_normalizado = ...` do `UNION ALL`, por fazer
// parte da MESMA instrução, ainda usa o snapshot de ANTES da espera, e
// portanto NÃO enxerga essa linha recém-commitada. Resultado: os dois ramos
// devolvem zero linhas, e `sql.ErrNoRows` vaza como erro de infraestrutura
// genérico ("no rows in result set") — não um erro de validação, então
// interrompe o laço de processarPendentes inteiro.
//
// Emitindo o SELECT de fallback como uma instrução PRÓPRIA (só quando o
// INSERT devolve sql.ErrNoRows, ou seja, houve conflito), ele ganha seu
// próprio snapshot fresco sob READ COMMITTED — tirado DEPOIS do INSERT já
// ter voltado do bloqueio, então sempre enxerga a linha que acabou de ser
// commitada pela transação concorrente que venceu a corrida. Duas
// instruções continuam sem janela de corrida real: entre o INSERT falhar
// por conflito e o SELECT seguinte rodar, a linha já está commitada e
// permanece (Estoques nunca são excluídos por nenhuma story implementada).
func encontrarOuCriarEstoque(tx *sql.Tx, empresaID string, nome string) (Estoque, error) {
	nomeTrimado := strings.TrimSpace(nome)
	if nomeTrimado == "" || utf8.RuneCountInString(nomeTrimado) > 255 {
		return Estoque{}, ErrEstoqueValidacao
	}

	var e Estoque
	// Story 12.1: a busca é por nome DENTRO da Empresa (o mesmo nome pode
	// existir em Filiais distintas); havendo homônimos, prefere o da Filial
	// padrão (a mais antiga da Empresa). O Estoque legado sem Filial também é
	// encontrado por nome, como antes.
	const filialPadraoSQL = `(SELECT id FROM filiais WHERE empresa_id = $2 ORDER BY criado_em, id LIMIT 1)`
	const buscarExistente = `
		SELECT id, nome FROM estoques
		WHERE nome_normalizado = lower(regexp_replace(btrim($1), '\s+', ' ', 'g'))
		  AND empresa_id = $2
		ORDER BY (filial_id IS NOT DISTINCT FROM ` + filialPadraoSQL + `) DESC, criado_em, id
		LIMIT 1`
	err := tx.QueryRow(buscarExistente, nomeTrimado, empresaID).Scan(&e.ID, &e.Nome)
	if err == nil {
		return e, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Estoque{}, fmt.Errorf("falha ao encontrar ou criar estoque: %w", err)
	}

	filialID, err := filialPadraoDaEmpresa(tx, empresaID)
	if err != nil {
		return Estoque{}, err
	}
	// O alvo de inferência do ON CONFLICT acompanha o índice
	// `idx_estoques_nome_normalizado`, reescopado para
	// `(filial_id, nome_normalizado)` pela migração 000044 (Story 12.1) — sem
	// as duas colunas aqui, o Postgres não acharia o índice.
	const inserir = `
		INSERT INTO estoques (nome, empresa_id, filial_id) VALUES ($1, $2, $3)
		ON CONFLICT (filial_id, nome_normalizado) DO NOTHING
		RETURNING id, nome`
	err = tx.QueryRow(inserir, nomeTrimado, empresaID, filialID).Scan(&e.ID, &e.Nome)
	if errors.Is(err, sql.ErrNoRows) {
		// Conflito: outra transação criou o mesmo nome na Filial padrão entre
		// a busca e o INSERT (própria instrução, próprio snapshot — ver o
		// comentário da função).
		const buscarNaFilial = `
			SELECT id, nome FROM estoques
			WHERE nome_normalizado = lower(regexp_replace(btrim($1), '\s+', ' ', 'g'))
			  AND filial_id = $2`
		err = tx.QueryRow(buscarNaFilial, nomeTrimado, filialID).Scan(&e.ID, &e.Nome)
	}
	if err != nil {
		return Estoque{}, fmt.Errorf("falha ao encontrar ou criar estoque: %w", err)
	}
	return e, nil
}
