package services

import (
	"database/sql"
	"errors"
	"fmt"
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
}

// ListarMovimentacoes devolve a trilha de Movimentações (Baixas da Story
// 5.1, Transferências da Story 5.2) do mais recente ao mais antigo,
// limitada a maxMovimentacoesPorConsulta. Lista vazia não é erro. Molde de
// ListarLogsAcesso (logs_acesso.go): `JOIN` simples para as colunas NOT
// NULL (`produto_id`, `usuario_id`), `LEFT JOIN` + `sql.NullString` para as
// anuláveis (`estoque_origem_id`, `estoque_destino_id`),
// `ORDER BY criado_em DESC, id DESC` (mesmo desempate determinístico —
// duas Movimentações no mesmo instante compartilham `criado_em` e sem o
// `id` a fronteira do LIMIT ordenaria de forma não-determinística), sem
// parâmetro runtime nem filtro. SQL explícito, sem ORM.
func ListarMovimentacoes(db *sql.DB) ([]MovimentacaoHistorico, error) {
	q := fmt.Sprintf(`
		SELECT m.id, m.produto_id, p.nome, m.tipo,
		       m.estoque_origem_id, eo.nome, m.estoque_destino_id, ed.nome,
		       m.quantidade, m.usuario_id, u.nome, m.criado_em
		FROM movimentacoes m
		JOIN produtos p ON p.id = m.produto_id
		JOIN usuarios u ON u.id = m.usuario_id
		LEFT JOIN estoques eo ON eo.id = m.estoque_origem_id
		LEFT JOIN estoques ed ON ed.id = m.estoque_destino_id
		ORDER BY m.criado_em DESC, m.id DESC
		LIMIT %d`, maxMovimentacoesPorConsulta)

	rows, err := db.Query(q)
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
			&m.Quantidade, &m.UsuarioID, &m.UsuarioNome, &m.CriadoEm,
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

// RegistrarBaixa registra o consumo de `quantidade` unidades do Produto
// `produtoID` no Estoque `estoqueID`, debitando `produto_estoque.quantidade`
// e inserindo a Movimentação `tipo='baixa'` correspondente numa ÚNICA
// transação (Story 5.1, spec-5-1) — nunca uma escrita sem a outra.
//
// Validação de `quantidade` (zero/negativa ou acima de limiteNumeric103)
// acontece ANTES de `tx.Begin()`: nenhuma escrita, nenhum lock adquirido
// para um pedido já inválido.
//
// Depois: `SELECT ... FOR UPDATE` trava a linha de `produto_estoque` (molde
// transacional de ExcluirEstoque, estoques.go). SQLSTATE 22P02
// (produtoID/estoqueID malformado, não-UUID) e sql.ErrNoRows (par válido mas
// sem linha — Produto nunca teve saldo nesse Estoque) colapsam ambos em
// &ErroQuantidadeIndisponivel{Disponivel: 0}: nenhuma AC desta story pede um
// 404 distinto aqui (ver Design Notes de spec-5-1). `quantidade` maior que o
// saldo travado -> &ErroQuantidadeIndisponivel{Disponivel: disponivel}, sem
// debitar nada.
//
// Caso contrário, debita a linha e insere a Movimentação na mesma transação,
// commit único.
func RegistrarBaixa(db *sql.DB, produtoID, estoqueID, usuarioID string, quantidade float64) (Movimentacao, error) {
	if quantidade <= 0 {
		return Movimentacao{}, &ErroMovimentacaoValidacao{Mensagem: "quantidade deve ser maior que zero"}
	}
	if quantidade > limiteNumeric103 {
		return Movimentacao{}, &ErroMovimentacaoValidacao{
			Mensagem: fmt.Sprintf("quantidade deve ser no máximo %s", limiteNumeric103Texto),
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	var disponivel float64
	const selectDisponivel = `
		SELECT quantidade FROM produto_estoque
		WHERE produto_id = $1 AND estoque_id = $2
		FOR UPDATE`
	if err := tx.QueryRow(selectDisponivel, produtoID, estoqueID).Scan(&disponivel); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return Movimentacao{}, &ErroQuantidadeIndisponivel{Disponivel: 0}
		}
		if errors.Is(err, sql.ErrNoRows) {
			return Movimentacao{}, &ErroQuantidadeIndisponivel{Disponivel: 0}
		}
		return Movimentacao{}, fmt.Errorf("falha ao travar linha de produto_estoque: %w", err)
	}

	if quantidade > disponivel {
		return Movimentacao{}, &ErroQuantidadeIndisponivel{Disponivel: disponivel}
	}

	const update = `
		UPDATE produto_estoque SET quantidade = quantidade - $1
		WHERE produto_id = $2 AND estoque_id = $3`
	if _, err := tx.Exec(update, quantidade, produtoID, estoqueID); err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao debitar produto_estoque: %w", err)
	}

	var mov Movimentacao
	const insert = `
		INSERT INTO movimentacoes (produto_id, tipo, estoque_origem_id, quantidade, usuario_id)
		VALUES ($1, 'baixa', $2, $3, $4)
		RETURNING id, produto_id, tipo, estoque_origem_id, quantidade, usuario_id, criado_em`
	if err := tx.QueryRow(insert, produtoID, estoqueID, quantidade, usuarioID).Scan(
		&mov.ID, &mov.ProdutoID, &mov.Tipo, &mov.EstoqueOrigemID, &mov.Quantidade, &mov.UsuarioID, &mov.CriadoEm,
	); err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao inserir movimentação de baixa: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao commitar baixa: %w", err)
	}
	return mov, nil
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
func travarLinhaProdutoEstoque(tx *sql.Tx, produtoID, estoqueID string) (float64, error) {
	var quantidade float64
	const upsertLock = `
		INSERT INTO produto_estoque (produto_id, estoque_id, quantidade)
		VALUES ($1, $2, 0)
		ON CONFLICT (produto_id, estoque_id) DO UPDATE SET quantidade = produto_estoque.quantidade
		RETURNING quantidade`
	if err := tx.QueryRow(upsertLock, produtoID, estoqueID).Scan(&quantidade); err != nil {
		return 0, err
	}
	return quantidade, nil
}

// erroTravarProdutoEstoque traduz o erro de travarLinhaProdutoEstoque:
// 22P02/23503 colapsam em &ErroQuantidadeIndisponivel{Disponivel: 0}
// (mesmo colapso da Story 5.1, agora também para o lado destino — nenhuma
// AC desta story pede um código de erro dedicado para Estoque destino
// inválido). Qualquer outro erro é devolvido envolto, sem colapsar.
func erroTravarProdutoEstoque(err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && (pqErr.Code == pqInvalidTextRepresentation || pqErr.Code == pqForeignKeyViolation) {
		return &ErroQuantidadeIndisponivel{Disponivel: 0}
	}
	return fmt.Errorf("falha ao travar linha de produto_estoque: %w", err)
}

// RegistrarTransferencia move `quantidade` unidades do Produto `produtoID`
// do Estoque `estoqueOrigemID` para o Estoque `estoqueDestinoID`, debitando
// a origem, creditando o destino e inserindo a Movimentação
// `tipo='transferencia'` correspondente (com os dois lados preenchidos)
// numa ÚNICA transação (Story 5.2, spec-5-2) — mesmo molde transacional de
// RegistrarBaixa (Story 5.1).
//
// Validação de `quantidade` (zero/negativa ou acima de limiteNumeric103,
// mesmo texto de RegistrarBaixa) e de `estoqueOrigemID`/`estoqueDestinoID`
// iguais (comparação case-insensitive via strings.EqualFold — dois UUIDs
// idênticos com capitalização diferente não devem escapar do guard) acontece
// ANTES de `tx.Begin()`: nenhuma escrita, nenhum lock adquirido para um
// pedido já inválido.
//
// AD-10 (epic-5-context.md): as duas linhas de produto_estoque tocadas
// (origem e destino) são travadas via travarLinhaProdutoEstoque na ORDEM
// CANÔNICA ascendente de estoque_id — nunca na ordem origem-depois-destino
// declarada pelo chamador. Como o par (produto_id, X) compartilha o mesmo
// produto_id nos dois lados, ordenar os pares reduz a ordenar por
// estoque_id (comparação de string simples). Isso garante que duas
// Transferências concorrentes entre os MESMOS dois Estoques, em direções
// opostas, travem sempre na mesma ordem física — uma espera a outra,
// nenhum deadlock do Postgres.
//
// Depois de travar as duas linhas: `quantidade` maior que o saldo da
// ORIGEM -> &ErroQuantidadeIndisponivel{Disponivel: disponivelOrigem}, sem
// debitar nem creditar nada (o `defer tx.Rollback()` desfaz qualquer linha
// de destino criada pelo upsert-lock). Senão, debita a origem, credita o
// destino e insere a Movimentação na mesma transação, commit único.
func RegistrarTransferencia(db *sql.DB, produtoID, estoqueOrigemID, estoqueDestinoID, usuarioID string, quantidade float64) (Movimentacao, error) {
	if quantidade <= 0 {
		return Movimentacao{}, &ErroMovimentacaoValidacao{Mensagem: "quantidade deve ser maior que zero"}
	}
	if quantidade > limiteNumeric103 {
		return Movimentacao{}, &ErroMovimentacaoValidacao{
			Mensagem: fmt.Sprintf("quantidade deve ser no máximo %s", limiteNumeric103Texto),
		}
	}
	if strings.EqualFold(estoqueOrigemID, estoqueDestinoID) {
		return Movimentacao{}, &ErroMovimentacaoValidacao{Mensagem: "estoque de origem e destino devem ser diferentes"}
	}

	tx, err := db.Begin()
	if err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	primeiro, segundo := estoqueOrigemID, estoqueDestinoID
	if segundo < primeiro {
		primeiro, segundo = segundo, primeiro
	}
	saldoPrimeiro, err := travarLinhaProdutoEstoque(tx, produtoID, primeiro)
	if err != nil {
		return Movimentacao{}, erroTravarProdutoEstoque(err)
	}
	saldoSegundo, err := travarLinhaProdutoEstoque(tx, produtoID, segundo)
	if err != nil {
		return Movimentacao{}, erroTravarProdutoEstoque(err)
	}

	disponivelOrigem := saldoSegundo
	if estoqueOrigemID == primeiro {
		disponivelOrigem = saldoPrimeiro
	}

	if quantidade > disponivelOrigem {
		return Movimentacao{}, &ErroQuantidadeIndisponivel{Disponivel: disponivelOrigem}
	}

	const updateOrigem = `
		UPDATE produto_estoque SET quantidade = quantidade - $1
		WHERE produto_id = $2 AND estoque_id = $3`
	if _, err := tx.Exec(updateOrigem, quantidade, produtoID, estoqueOrigemID); err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao debitar produto_estoque (origem): %w", err)
	}

	const updateDestino = `
		UPDATE produto_estoque SET quantidade = quantidade + $1
		WHERE produto_id = $2 AND estoque_id = $3`
	if _, err := tx.Exec(updateDestino, quantidade, produtoID, estoqueDestinoID); err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao creditar produto_estoque (destino): %w", err)
	}

	var mov Movimentacao
	const insert = `
		INSERT INTO movimentacoes (produto_id, tipo, estoque_origem_id, estoque_destino_id, quantidade, usuario_id)
		VALUES ($1, 'transferencia', $2, $3, $4, $5)
		RETURNING id, produto_id, tipo, estoque_origem_id, estoque_destino_id, quantidade, usuario_id, criado_em`
	if err := tx.QueryRow(insert, produtoID, estoqueOrigemID, estoqueDestinoID, quantidade, usuarioID).Scan(
		&mov.ID, &mov.ProdutoID, &mov.Tipo, &mov.EstoqueOrigemID, &mov.EstoqueDestinoID, &mov.Quantidade, &mov.UsuarioID, &mov.CriadoEm,
	); err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao inserir movimentação de transferência: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Movimentacao{}, fmt.Errorf("falha ao commitar transferência: %w", err)
	}
	return mov, nil
}
