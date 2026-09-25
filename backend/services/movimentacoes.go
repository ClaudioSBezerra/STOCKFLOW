package services

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Movimentacao é a projeção devolvida por RegistrarBaixa (Story 5.1,
// spec-5-1) — trilha de auditoria de toda escrita em
// `produto_estoque.quantidade`. `EstoqueDestinoID` fica sempre `nil` para
// `tipo="baixa"` (a coluna existe desde já, mas só a Story 5.2, Transferência,
// a lê/escreve).
type Movimentacao struct {
	ID               string    `json:"id"`
	ProdutoID        string    `json:"produtoId"`
	Tipo             string    `json:"tipo"`
	EstoqueOrigemID  string    `json:"estoqueOrigemId"`
	EstoqueDestinoID *string   `json:"estoqueDestinoId"`
	Quantidade       float64   `json:"quantidade"`
	UsuarioID        string    `json:"usuarioId"`
	CriadoEm         time.Time `json:"criadoEm"`
}

// maxMovimentacoesPorConsulta limita quantas linhas GET /api/movimentacoes
// devolve numa consulta — a rota é só-leitura e só-`almoxarife`+, mas o
// resultado não pode crescer sem teto conforme a trilha de Movimentações
// acumula. 500 é decisão desta story (spec-5-3), espelhando
// maxLogsAcessoPorConsulta; não há configuração runtime.
const maxMovimentacoesPorConsulta = 500

// MovimentacaoHistorico é a projeção de uma linha de `movimentacoes`
// devolvida por GET /api/movimentacoes (Story 5.3, spec-5-3) — a trilha de
// auditoria consultável, com os nomes de Produto, Estoques (origem/destino)
// e autor já resolvidos por JOIN (o Almoxarife lê a linha inteira sem outra
// chamada). Molde de LogAcesso (logs_acesso.go).
//
// `EstoqueOrigemID`/`EstoqueOrigemNome` são anuláveis pelo schema (a coluna
// é NULLABLE), embora hoje sempre preenchidos. `EstoqueDestinoID`/
// `EstoqueDestinoNome` são `nil` para `tipo="baixa"` (a Baixa não tem
// destino) e preenchidos para `tipo="transferencia"`.
type MovimentacaoHistorico struct {
	ID                 string    `json:"id"`
	ProdutoID          string    `json:"produtoId"`
	ProdutoNome        string    `json:"produtoNome"`
	Tipo               string    `json:"tipo"`
	EstoqueOrigemID    *string   `json:"estoqueOrigemId"`
	EstoqueOrigemNome  *string   `json:"estoqueOrigemNome"`
	EstoqueDestinoID   *string   `json:"estoqueDestinoId"`
	EstoqueDestinoNome *string   `json:"estoqueDestinoNome"`
	Quantidade         float64   `json:"quantidade"`
	UsuarioID          string    `json:"usuarioId"`
	UsuarioNome        string    `json:"usuarioNome"`
	CriadoEm           time.Time `json:"criadoEm"`
	// Inativo (Story 16.2): o Produto foi inativado depois; a linha continua listada.
	Inativo bool `json:"inativo"`
}

// FiltroMovimentacoes reúne os filtros opcionais (Story 17.5) de
// ListarMovimentacoes/IndicadoresMovimentacoes. Strings vazias = sem filtro.
// `De`/`Ate` são `YYYY-MM-DD` (Ate inclusivo); `Tipo` é baixa|transferencia|
// ajuste|entrada; `EstoqueID` casa origem OU destino.
type FiltroMovimentacoes struct {
	De        string
	Ate       string
	Tipo      string
	EstoqueID string
}

var uuidMovimentacaoRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// whereMovimentacoes valida o filtro e monta o WHERE (compartilhado pela
// listagem e pelos indicadores). `$1` é sempre a Empresa.
func whereMovimentacoes(empresaID string, f FiltroMovimentacoes) (string, []any, error) {
	where := "m.empresa_id = $1"
	args := []any{empresaID}
	if f.De != "" {
		if _, err := time.Parse("2006-01-02", f.De); err != nil {
			return "", nil, &ErroMovimentacaoValidacao{Mensagem: "data inicial inválida (use AAAA-MM-DD)"}
		}
		args = append(args, f.De)
		where += fmt.Sprintf(" AND m.criado_em >= $%d::date", len(args))
	}
	if f.Ate != "" {
		if _, err := time.Parse("2006-01-02", f.Ate); err != nil {
			return "", nil, &ErroMovimentacaoValidacao{Mensagem: "data final inválida (use AAAA-MM-DD)"}
		}
		args = append(args, f.Ate)
		where += fmt.Sprintf(" AND m.criado_em < ($%d::date + 1)", len(args))
	}
	if f.Tipo != "" {
		switch f.Tipo {
		case "baixa", "transferencia", "ajuste", "entrada":
		default:
			return "", nil, &ErroMovimentacaoValidacao{Mensagem: "tipo de movimentação inválido"}
		}
		args = append(args, f.Tipo)
		where += fmt.Sprintf(" AND m.tipo = $%d", len(args))
	}
	if f.EstoqueID != "" {
		if !uuidMovimentacaoRe.MatchString(f.EstoqueID) {
			return "", nil, &ErroMovimentacaoValidacao{Mensagem: "estoque inválido"}
		}
		args = append(args, f.EstoqueID)
		where += fmt.Sprintf(" AND (m.estoque_origem_id = $%d OR m.estoque_destino_id = $%d)", len(args), len(args))
	}
	return where, args, nil
}

// IndicadoresMovimentacoesResultado alimenta a faixa de Movimentações (Story 17.5).
type IndicadoresMovimentacoesResultado struct {
	Baixas         int `json:"baixas"`
	Transferencias int `json:"transferencias"`
}

// IndicadoresMovimentacoes conta Baixas e Transferências sob o MESMO filtro
// da listagem (um `Tipo` filtrado a outro valor zera o outro contador).
func IndicadoresMovimentacoes(db *sql.DB, empresaID string, f FiltroMovimentacoes) (IndicadoresMovimentacoesResultado, error) {
	where, args, err := whereMovimentacoes(empresaID, f)
	if err != nil {
		return IndicadoresMovimentacoesResultado{}, err
	}
	var r IndicadoresMovimentacoesResultado
	q := `SELECT COUNT(*) FILTER (WHERE m.tipo = 'baixa'),
	             COUNT(*) FILTER (WHERE m.tipo = 'transferencia')
	      FROM movimentacoes m WHERE ` + where
	if err := db.QueryRow(q, args...).Scan(&r.Baixas, &r.Transferencias); err != nil {
		return IndicadoresMovimentacoesResultado{}, fmt.Errorf("falha ao calcular indicadores de movimentações: %w", err)
	}
	return r, nil
}

// ListarMovimentacoes devolve a trilha de Movimentações DA EMPRESA
// `empresaID` (Story 9.1, AD-20) — Baixas da Story 5.1, Transferências da
// Story 5.2 — do mais recente ao mais antigo, limitada a
// maxMovimentacoesPorConsulta, com filtros opcionais (Story 17.5). Lista vazia
// não é erro. `JOIN` simples para as colunas NOT NULL, `LEFT JOIN` para as
// anuláveis, `ORDER BY criado_em DESC, id DESC` (desempate determinístico).
func ListarMovimentacoes(db *sql.DB, empresaID string, filtro FiltroMovimentacoes) ([]MovimentacaoHistorico, error) {
	where, args, err := whereMovimentacoes(empresaID, filtro)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`
		SELECT m.id, m.produto_id, p.nome, m.tipo,
		       m.estoque_origem_id, eo.nome, m.estoque_destino_id, ed.nome,
		       m.quantidade, m.usuario_id, u.nome, m.criado_em, p.inativado_em IS NOT NULL
		FROM movimentacoes m
		JOIN produtos p ON p.id = m.produto_id
		JOIN usuarios u ON u.id = m.usuario_id
		LEFT JOIN estoques eo ON eo.id = m.estoque_origem_id
		LEFT JOIN estoques ed ON ed.id = m.estoque_destino_id
		WHERE %s
		ORDER BY m.criado_em DESC, m.id DESC
		LIMIT %d`, where, maxMovimentacoesPorConsulta)

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar movimentações: %w", err)
	}
	defer rows.Close()

	movimentacoes := make([]MovimentacaoHistorico, 0)
	for rows.Next() {
		var m MovimentacaoHistorico
		var origemID, origemNome, destinoID, destinoNome sql.NullString
		if err := rows.Scan(
			&m.ID, &m.ProdutoID, &m.ProdutoNome, &m.Tipo,
			&origemID, &origemNome, &destinoID, &destinoNome,
			&m.Quantidade, &m.UsuarioID, &m.UsuarioNome, &m.CriadoEm, &m.Inativo,
		); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de movimentação: %w", err)
		}
		if origemID.Valid {
			m.EstoqueOrigemID = &origemID.String
		}
		if origemNome.Valid {
			m.EstoqueOrigemNome = &origemNome.String
		}
		if destinoID.Valid {
			m.EstoqueDestinoID = &destinoID.String
		}
		if destinoNome.Valid {
			m.EstoqueDestinoNome = &destinoNome.String
		}
		movimentacoes = append(movimentacoes, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar movimentações: %w", err)
	}
	return movimentacoes, nil
}

// ListarMovimentacoesDoUsuario devolve TODAS as Movimentações cujo
// `usuario_id` é `usuarioID`, do mais recente ao mais antigo — insumo de
// ExportarDadosUsuario (Story 8.1, spec-8-1: exportação dos próprios dados
// pessoais, LGPD). Molde de ListarMovimentacoes, mas escopada por usuário e
// deliberadamente SEM `LIMIT`/maxMovimentacoesPorConsulta: aquele teto
// existe para a consulta administrativa (`almoxarife`+, toda a
// organização); aqui o conjunto já está limitado a um único usuário e a
// LGPD pede o histórico completo, não uma amostra. Mesmos JOINs e mesmo
// `ORDER BY m.criado_em DESC, m.id DESC` (desempate determinístico). Lista
// vazia não é erro.
func ListarMovimentacoesDoUsuario(db *sql.DB, empresaID string, usuarioID string) ([]MovimentacaoHistorico, error) {
	const q = `
		SELECT m.id, m.produto_id, p.nome, m.tipo,
		       m.estoque_origem_id, eo.nome, m.estoque_destino_id, ed.nome,
		       m.quantidade, m.usuario_id, u.nome, m.criado_em, p.inativado_em IS NOT NULL
		FROM movimentacoes m
		JOIN produtos p ON p.id = m.produto_id
		JOIN usuarios u ON u.id = m.usuario_id
		LEFT JOIN estoques eo ON eo.id = m.estoque_origem_id
		LEFT JOIN estoques ed ON ed.id = m.estoque_destino_id
		WHERE m.usuario_id = $1 AND m.empresa_id = $2
		ORDER BY m.criado_em DESC, m.id DESC`

	rows, err := db.Query(q, usuarioID, empresaID)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar movimentações do usuário: %w", err)
	}
	defer rows.Close()

	movimentacoes := make([]MovimentacaoHistorico, 0)
	for rows.Next() {
		var m MovimentacaoHistorico
		var origemID, origemNome, destinoID, destinoNome sql.NullString
		if err := rows.Scan(
			&m.ID, &m.ProdutoID, &m.ProdutoNome, &m.Tipo,
			&origemID, &origemNome, &destinoID, &destinoNome,
			&m.Quantidade, &m.UsuarioID, &m.UsuarioNome, &m.CriadoEm, &m.Inativo,
		); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de movimentação do usuário: %w", err)
		}
		if origemID.Valid {
			m.EstoqueOrigemID = &origemID.String
		}
		if origemNome.Valid {
			m.EstoqueOrigemNome = &origemNome.String
		}
		if destinoID.Valid {
			m.EstoqueDestinoID = &destinoID.String
		}
		if destinoNome.Valid {
			m.EstoqueDestinoNome = &destinoNome.String
		}
		movimentacoes = append(movimentacoes, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar movimentações do usuário: %w", err)
	}
	return movimentacoes, nil
}

// ErroMovimentacaoValidacao é o erro de validação devolvido por
// RegistrarBaixa quando `quantidade` é zero, negativa, ou maior que
// limiteNumeric103 — sempre verificado ANTES de abrir a transação, nenhuma
// escrita acontece quando este erro é devolvido. Mapeado para
// 400 VALIDATION_ERROR. Mesmo molde de ErroProdutoValidacao.
type ErroMovimentacaoValidacao struct {
	Mensagem string
}

func (e *ErroMovimentacaoValidacao) Error() string { return e.Mensagem }

// ErroQuantidadeIndisponivel indica que a quantidade pedida excede o saldo
// disponível na linha de `produto_estoque` travada por RegistrarBaixa — ou
// que não existe linha nenhuma para o par (produto_id, estoque_id) (`
// Disponivel: 0`, mesmo colapso usado para um `id`/`estoqueId` malformado,
// ver Design Notes de spec-5-1). Mapeado para 409 CONFLICT.
//
// `strconv.FormatFloat(..., 'f', -1, 64)` — nunca `%v`/`%g` — evita notação
// científica em valores pequenos, mesmo cuidado de limiteNumeric103Texto.
type ErroQuantidadeIndisponivel struct {
	Disponivel float64
}

func (e *ErroQuantidadeIndisponivel) Error() string {
	return fmt.Sprintf(
		"quantidade indisponível: apenas %s unidade(s) disponível(is)",
		strconv.FormatFloat(e.Disponivel, 'f', -1, 64),
	)
}

// erroSaldoAlvo traduz erros SQL de travar/ler o saldo de um par: SQLSTATE
// 22P02 (id malformado) e 23503 (FK — Produto/Estoque inexistente) colapsam em
// &ErroQuantidadeIndisponivel{Disponivel: 0} (colapso da Story 5.1, nunca
// revela existência). Qualquer outro erro é devolvido envolto em `contexto`.
func erroSaldoAlvo(err error, contexto string) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && (pqErr.Code == pqInvalidTextRepresentation || pqErr.Code == pqForeignKeyViolation) {
		return &ErroQuantidadeIndisponivel{Disponivel: 0}
	}
	return fmt.Errorf("%s: %w", contexto, err)
}

// validarQuantidadeMovimentacao é a validação prévia (antes de abrir a
// transação) compartilhada por Baixa e Transferência.
func validarQuantidadeMovimentacao(quantidade float64) error {
	if math.IsNaN(quantidade) || math.IsInf(quantidade, 0) || quantidade <= 0 || arredondar3(quantidade) <= 0 {
		return &ErroMovimentacaoValidacao{Mensagem: "quantidade deve ser maior que zero"}
	}
	if quantidade > limiteNumeric103 {
		return &ErroMovimentacaoValidacao{
			Mensagem: fmt.Sprintf("quantidade deve ser no máximo %s", limiteNumeric103Texto),
		}
	}
	return nil
}

// validarQuantidadeDisponivelTx lê o saldo DISPONÍVEL (físico menos reservas
// de Pedidos pendentes) do par e rejeita `quantidade` acima dele com
// &ErroQuantidadeIndisponivel{Disponivel: max(disponível, 0)}. Chame DEPOIS de
// travarSaldoParesTx. Par inexistente/alheio tem disponível 0.
func validarQuantidadeDisponivelTx(tx *sql.Tx, empresaID, produtoID, estoqueID string, quantidade float64) error {
	disponivel, err := saldoDisponivelParTx(tx, empresaID, produtoID, estoqueID)
	if err != nil {
		return erroSaldoAlvo(err, "falha ao ler saldo disponível")
	}
	disponivel = arredondar3(disponivel)
	if disponivel < 0 {
		disponivel = 0
	}
	if arredondar3(quantidade) > disponivel {
		return &ErroQuantidadeIndisponivel{Disponivel: disponivel}
	}
	return nil
}

// inserirMovimentacaoConsumoTx grava a Movimentação de UMA fonte consumida
// (`lote_id` NULL quando a fonte é o saldo legado de `produto_estoque`).
// `estoqueDestinoID` nil para Baixa.
func inserirMovimentacaoConsumoTx(tx *sql.Tx, empresaID, tipo, produtoID, estoqueOrigemID string, estoqueDestinoID *string, usuarioID string, c consumoFonte) (Movimentacao, error) {
	var mov Movimentacao
	var loteID any
	if c.LoteID != nil {
		loteID = *c.LoteID
	}
	var destino any
	if estoqueDestinoID != nil {
		destino = *estoqueDestinoID
	}
	const insert = `
		INSERT INTO movimentacoes (produto_id, tipo, estoque_origem_id, estoque_destino_id, quantidade, usuario_id, empresa_id, lote_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, produto_id, tipo, estoque_origem_id, estoque_destino_id, quantidade, usuario_id, criado_em`
	if err := tx.QueryRow(insert, produtoID, tipo, estoqueOrigemID, destino, c.Quantidade, usuarioID, empresaID, loteID).Scan(
		&mov.ID, &mov.ProdutoID, &mov.Tipo, &mov.EstoqueOrigemID, &mov.EstoqueDestinoID, &mov.Quantidade, &mov.UsuarioID, &mov.CriadoEm,
	); err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao inserir movimentação de %s: %w", tipo, err)
	}
	return mov, nil
}

// RegistrarBaixa registra o consumo de `quantidade` unidades do Produto
// `produtoID` no Estoque `estoqueID` (Story 5.1; Story 11.4, FR-14): valida
// contra o saldo DISPONÍVEL (físico menos reservas de Pedidos pendentes),
// debita por FEFO (consumirFEFOTx) e insere 1 Movimentação `tipo='baixa'` por
// fonte consumida (com `lote_id`, NULL para o saldo legado) numa ÚNICA
// transação. Devolve a Movimentação da PRIMEIRA fonte consumida, com
// `Quantidade` = total pedido (contrato JSON inalterado).
//
// Validação de `quantidade` acontece ANTES de `tx.Begin()`. Depois o par é
// travado com travarSaldoParesTx (AD-10) ANTES de ler o disponível.
// `quantidade` acima do disponível, ou Produto/Estoque malformado, inexistente
// ou de outra Empresa -> &ErroQuantidadeIndisponivel, sem gravar nada.
func RegistrarBaixa(db *sql.DB, empresaID string, produtoID, estoqueID, usuarioID string, quantidade float64) (Movimentacao, error) {
	if err := validarQuantidadeMovimentacao(quantidade); err != nil {
		return Movimentacao{}, err
	}

	tx, err := db.Begin()
	if err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	if err := travarSaldoParesTx(tx, empresaID, []ParSaldo{{ProdutoID: produtoID, EstoqueID: estoqueID}}); err != nil {
		return Movimentacao{}, erroSaldoAlvo(err, "falha ao travar saldo")
	}
	if err := validarQuantidadeDisponivelTx(tx, empresaID, produtoID, estoqueID, quantidade); err != nil {
		return Movimentacao{}, err
	}

	consumos, err := consumirFEFOTx(tx, empresaID, produtoID, estoqueID, quantidade)
	if err != nil {
		return Movimentacao{}, err
	}

	var primeira Movimentacao
	for i, c := range consumos {
		mov, err := inserirMovimentacaoConsumoTx(tx, empresaID, "baixa", produtoID, estoqueID, nil, usuarioID, c)
		if err != nil {
			return Movimentacao{}, err
		}
		if i == 0 {
			primeira = mov
		}
	}
	primeira.Quantidade = arredondar3(quantidade)

	if err := tx.Commit(); err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao commitar baixa: %w", err)
	}
	return primeira, nil
}

// travarLinhaProdutoEstoque adquire o lock de escrita da linha
// (produtoID, estoqueID) em produto_estoque para uso dentro de uma
// transação de Transferência (Story 5.2, spec-5-2) — NUNCA
// `SELECT ... FOR UPDATE` puro (ver Design Notes de spec-5-2): a linha do
// Estoque DESTINO de uma Transferência pode nunca ter existido (o Produto
// nunca esteve lá), e `FOR UPDATE` não trava nada quando não há linha
// nenhuma para travar.
//
// `INSERT ... ON CONFLICT (produto_id, estoque_id) DO UPDATE` resolve isso
// numa única instrução atômica: cria a linha ausente com `quantidade=0`
// (mantida sob o lock implícito da própria criação) OU adquire, via a
// cláusula `DO UPDATE` (um no-op lógico), o MESMO lock de escrita que
// `FOR UPDATE` adquiriria se a linha já existisse. Devolve o saldo travado.
//
// Erro SQLSTATE 22P02 (produtoID/estoqueID malformado) ou 23503 (violação
// de chave estrangeira — estoqueID referenciando um Estoque inexistente,
// ou produtoID um Produto inexistente) é devolvido tal qual para o
// chamador traduzir em &ErroQuantidadeIndisponivel{Disponivel: 0} — mesmo
// colapso "malformado/inexistente -> 0 disponível" da Story 5.1.
func travarLinhaProdutoEstoque(tx *sql.Tx, empresaID string, produtoID, estoqueID string) (float64, error) {
	var quantidade float64
	// O `SELECT` no lugar de `VALUES` é o guard de Empresa (Story 9.1,
	// AD-20): a linha só nasce quando o Produto E o Estoque são os dois da
	// Empresa da requisição. Um lado de outra Empresa produz zero linhas de
	// entrada, o `RETURNING` não devolve nada e o chamador recebe
	// `sql.ErrNoRows` — traduzido por erroTravarProdutoEstoque no MESMO
	// `Disponivel: 0` de um id malformado ou inexistente.
	const upsertLock = `
		INSERT INTO produto_estoque (produto_id, estoque_id, quantidade)
		SELECT p.id, e.id, 0
		FROM produtos p, estoques e
		WHERE p.id = $1 AND e.id = $2 AND p.empresa_id = $3 AND e.empresa_id = $3
		ON CONFLICT (produto_id, estoque_id) DO UPDATE SET quantidade = produto_estoque.quantidade
		RETURNING quantidade`
	if err := tx.QueryRow(upsertLock, produtoID, estoqueID, empresaID).Scan(&quantidade); err != nil {
		return 0, err
	}
	return quantidade, nil
}

// creditarLoteDestinoTx credita `c.Quantidade` no Estoque destino preservando a
// Data de Validade da fonte: soma no Lote de destino MAIS ANTIGO com a mesma
// `data_validade` (`IS NOT DISTINCT FROM`, cobre NULL) ou cria um Lote novo
// (com o `criado_em` da fonte; o legado, sem data, usa now()). O saldo do
// destino nunca é escrito em `produto_estoque` (AD-24).
func creditarLoteDestinoTx(tx *sql.Tx, empresaID, produtoID, estoqueDestinoID string, c consumoFonte) error {
	var loteID string
	err := tx.QueryRow(`
		SELECT id FROM lotes
		WHERE produto_id = $1 AND estoque_id = $2 AND empresa_id = $3
		  AND data_validade IS NOT DISTINCT FROM $4::date
		ORDER BY criado_em, id
		LIMIT 1`,
		produtoID, estoqueDestinoID, empresaID, c.DataValidade).Scan(&loteID)
	if err == nil {
		if _, err := tx.Exec(`UPDATE lotes SET quantidade = quantidade + $1 WHERE id = $2`, c.Quantidade, loteID); err != nil {
			return fmt.Errorf("falha ao creditar lote de destino: %w", err)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("falha ao procurar lote de destino: %w", err)
	}

	var criadoEm sql.NullTime
	if !c.CriadoEm.IsZero() {
		criadoEm = sql.NullTime{Time: c.CriadoEm, Valid: true}
	}
	if _, err := tx.Exec(`
		INSERT INTO lotes (produto_id, estoque_id, quantidade, data_validade, empresa_id, criado_em)
		VALUES ($1, $2, $3, $4::date, $5, COALESCE($6::timestamptz, now()))`,
		produtoID, estoqueDestinoID, c.Quantidade, c.DataValidade, empresaID, criadoEm); err != nil {
		return fmt.Errorf("falha ao criar lote de destino: %w", err)
	}
	return nil
}

// RegistrarTransferencia move `quantidade` unidades do Produto `produtoID` do
// Estoque `estoqueOrigemID` para `estoqueDestinoID` (Story 5.2; Story 11.4,
// FR-15) numa ÚNICA transação: valida contra o saldo DISPONÍVEL da origem,
// debita por FEFO (consumirFEFOTx), credita o destino em Lote(s) com a MESMA
// `data_validade` de cada fonte consumida e insere 1 Movimentação
// `tipo='transferencia'` por fonte (`lote_id` = Lote de origem, NULL para o
// legado). Devolve a Movimentação da primeira fonte, com `Quantidade` = total
// pedido (contrato JSON inalterado).
//
// Validação de `quantidade` e de origem == destino (strings.EqualFold) ocorre
// ANTES de `tx.Begin()`. AD-10: origem e destino entram no MESMO conjunto de
// pares de travarSaldoParesTx, ordenado por (produto_id, estoque_id) — nunca
// origem-depois-destino —, então Transferências opostas (A->B, B->A) não geram
// deadlock. Produto/Estoque (origem ou destino) malformado, inexistente ou de
// outra Empresa colapsa em &ErroQuantidadeIndisponivel{Disponivel: 0}.
func RegistrarTransferencia(db *sql.DB, empresaID string, produtoID, estoqueOrigemID, estoqueDestinoID, usuarioID string, quantidade float64) (Movimentacao, error) {
	if err := validarQuantidadeMovimentacao(quantidade); err != nil {
		return Movimentacao{}, err
	}
	if strings.EqualFold(estoqueOrigemID, estoqueDestinoID) {
		return Movimentacao{}, &ErroMovimentacaoValidacao{Mensagem: "estoque de origem e destino devem ser diferentes"}
	}

	tx, err := db.Begin()
	if err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	pares := []ParSaldo{
		{ProdutoID: produtoID, EstoqueID: estoqueOrigemID},
		{ProdutoID: produtoID, EstoqueID: estoqueDestinoID},
	}
	if err := travarSaldoParesTx(tx, empresaID, pares); err != nil {
		return Movimentacao{}, erroSaldoAlvo(err, "falha ao travar saldo")
	}

	// Guard do destino: Produto e Estoque destino da Empresa da requisição.
	// `FOR SHARE OF e` serializa com ExcluirEstoque (FOR UPDATE).
	var destinoValido bool
	err = tx.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM produtos p, estoques e
			WHERE p.id = $1 AND p.empresa_id = $3 AND e.id = $2 AND e.empresa_id = $3
			FOR SHARE OF e
		)`, produtoID, estoqueDestinoID, empresaID).Scan(&destinoValido)
	if err != nil {
		return Movimentacao{}, erroSaldoAlvo(err, "falha ao validar estoque de destino")
	}
	if !destinoValido {
		return Movimentacao{}, &ErroQuantidadeIndisponivel{Disponivel: 0}
	}

	if err := validarQuantidadeDisponivelTx(tx, empresaID, produtoID, estoqueOrigemID, quantidade); err != nil {
		return Movimentacao{}, err
	}

	consumos, err := consumirFEFOTx(tx, empresaID, produtoID, estoqueOrigemID, quantidade)
	if err != nil {
		return Movimentacao{}, err
	}

	destino := estoqueDestinoID
	var primeira Movimentacao
	for i, c := range consumos {
		if err := creditarLoteDestinoTx(tx, empresaID, produtoID, estoqueDestinoID, c); err != nil {
			return Movimentacao{}, err
		}
		mov, err := inserirMovimentacaoConsumoTx(tx, empresaID, "transferencia", produtoID, estoqueOrigemID, &destino, usuarioID, c)
		if err != nil {
			return Movimentacao{}, err
		}
		if i == 0 {
			primeira = mov
		}
	}
	primeira.Quantidade = arredondar3(quantidade)

	if err := tx.Commit(); err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao commitar transferência: %w", err)
	}
	return primeira, nil
}
