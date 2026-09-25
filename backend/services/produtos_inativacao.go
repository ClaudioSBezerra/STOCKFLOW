// Package services, arquivo produtos_inativacao.go: inativar e reativar um
// Produto — Story 16.1 (Epic 16, FR-55, AD-37).
//
// "Ativo" = `inativado_em IS NULL AND deleted_at IS NULL`. `deleted_at` é
// exclusivo da mesclagem (Story 6.4) e nunca é reutilizado aqui: um Produto
// mesclado é 404 para as duas operações.
//
// Ordem de locks (sem deadlock): inativar só trava a linha de `produtos`
// (FOR UPDATE) e lê saldo/reserva sem lock; LancarSaldo e o envio de Pedido
// travam `produtos` (FOR SHARE) ANTES de `produto_estoque`/`lotes`. Assim
// uma inativação e um lançamento concorrentes se serializam na linha do
// Produto e nunca sobra um Produto inativo com saldo.
package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

// motivoInativacaoMaxRunas é o teto do motivo opcional da inativação.
const motivoInativacaoMaxRunas = 500

// Ações gravadas em `produto_historico` por esta story.
const (
	AcaoProdutoInativado    = "inativado"
	AcaoProdutoReativado    = "reativado"
	AcaoProdutoNomeAlterado = "nome_alterado"
)

// ErrProdutoJaInativo: inativar um Produto que já está inativo. 409.
var ErrProdutoJaInativo = errors.New("o produto já está inativo")

// ErrProdutoJaAtivo: reativar um Produto que já está ativo. 409.
var ErrProdutoJaAtivo = errors.New("o produto já está ativo")

// ErrProdutoInativo: operação que exige Produto ativo (lançar saldo) sobre um
// Produto inativo. Mapeado para 409 PRODUTO_INATIVO.
var ErrProdutoInativo = errors.New("o produto está inativo")

// ErroProdutoValidacaoInativacao: motivo acima do teto — devolvido ANTES de
// abrir a transação. Mapeado para 400 VALIDATION_ERROR.
type ErroProdutoValidacaoInativacao struct {
	Mensagem string
}

func (e *ErroProdutoValidacaoInativacao) Error() string { return e.Mensagem }

// ErroProdutoComSaldo: o Produto ainda tem saldo físico em algum Estoque e/ou
// reserva de Pedido pendente — não pode ser inativado. `Estoques` são os nomes
// dos Estoques com saldo (ordem alfabética); `TemReserva` indica reserva.
// Mapeado para 409 PRODUTO_COM_SALDO com a mensagem de Error().
type ErroProdutoComSaldo struct {
	Estoques   []string
	TemReserva bool
}

func (e *ErroProdutoComSaldo) Error() string {
	var partes []string
	if len(e.Estoques) > 0 {
		partes = append(partes, fmt.Sprintf("tem saldo em: %s", strings.Join(e.Estoques, ", ")))
	}
	if e.TemReserva {
		partes = append(partes, "tem reserva de Pedido pendente")
	}
	return fmt.Sprintf(
		"Não é possível inativar: o produto %s. Transfira ou dê baixa no saldo (e decida os Pedidos pendentes) antes de inativar.",
		strings.Join(partes, " e "),
	)
}

// ErroEANEmUso: outro Produto ativo da mesma Empresa já usa o EAN-13.
// Mapeado para 409 EAN_EM_USO com a mensagem de Error().
type ErroEANEmUso struct {
	Codigo string
	Nome   string
}

func (e *ErroEANEmUso) Error() string {
	if e.Codigo == "" {
		return fmt.Sprintf("Este EAN já está no produto %s", e.Nome)
	}
	return fmt.Sprintf("Este EAN já está no produto %s — %s", e.Codigo, e.Nome)
}

// ErroPedidoProdutoInativo: o envio de Pedido encontrou Produto(s) inativo(s)
// dentro da transação (inativado entre a leitura do carrinho e o envio).
// `Itens` são os nomes. Mapeado para 409 PRODUTO_INATIVO.
type ErroPedidoProdutoInativo struct {
	Itens []string
}

func (e *ErroPedidoProdutoInativo) Error() string {
	if len(e.Itens) == 1 {
		return fmt.Sprintf("Não é possível enviar o pedido: o produto %s foi inativado. Atualize o carrinho e tente novamente.", e.Itens[0])
	}
	return fmt.Sprintf("Não é possível enviar o pedido: os produtos %s foram inativados. Atualize o carrinho e tente novamente.", strings.Join(e.Itens, ", "))
}

// produtoTravado é a leitura da linha de `produtos` sob FOR UPDATE.
type produtoTravado struct {
	inativo bool
	ean13   sql.NullString
}

// travarProdutoParaAtualizacaoTx trava (FOR UPDATE) a linha do Produto da
// Empresa, não mesclado. Inexistente, malformado, mesclado ou alheio ->
// ErrProdutoNaoEncontrado.
func travarProdutoParaAtualizacaoTx(tx *sql.Tx, empresaID, produtoID string) (produtoTravado, error) {
	var p produtoTravado
	err := tx.QueryRow(
		`SELECT inativado_em IS NOT NULL, ean13 FROM produtos
		 WHERE id = $1 AND empresa_id = $2 AND deleted_at IS NULL
		 FOR UPDATE`,
		produtoID, empresaID,
	).Scan(&p.inativo, &p.ean13)
	if err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return produtoTravado{}, ErrProdutoNaoEncontrado
		}
		return produtoTravado{}, fmt.Errorf("falha ao travar produto: %w", err)
	}
	return p, nil
}

// normalizarMotivoInativacao faz o trim; vazio -> nil; acima do teto -> erro.
func normalizarMotivoInativacao(motivo string) (*string, error) {
	m := strings.TrimSpace(motivo)
	if m == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(m) > motivoInativacaoMaxRunas {
		return nil, &ErroProdutoValidacaoInativacao{
			Mensagem: fmt.Sprintf("motivo deve ter no máximo %d caracteres", motivoInativacaoMaxRunas),
		}
	}
	return &m, nil
}

// registrarHistoricoProdutoTx grava uma linha append-only em
// `produto_historico` na transação corrente.
func registrarHistoricoProdutoTx(tx *sql.Tx, empresaID, produtoID, atorID, acao string, detalhe map[string]any) error {
	if detalhe == nil {
		detalhe = map[string]any{}
	}
	b, err := json.Marshal(detalhe)
	if err != nil {
		return fmt.Errorf("falha ao serializar detalhe do histórico: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO produto_historico (empresa_id, produto_id, ator_id, acao, detalhe)
		 VALUES ($1, $2, $3, $4, $5::jsonb)`,
		empresaID, produtoID, atorID, acao, string(b),
	); err != nil {
		return fmt.Errorf("falha ao gravar histórico do produto: %w", err)
	}
	return nil
}

// InativarProduto inativa o Produto `produtoID` da Empresa `empresaID` em
// nome de `atorID`. Motivo opcional (trim; vazio = null; > 500 runas ->
// 400 antes de abrir transação). Numa transação: trava a linha (FOR UPDATE);
// já inativo -> ErrProdutoJaInativo; saldo físico > 0 em algum Estoque
// (view `saldo_produto_estoque`) ou qualquer reserva de Pedido ->
// &ErroProdutoComSaldo, nada gravado. Senão grava `inativado_em/por` e o
// histórico `inativado` com `{"motivo": ...}`.
func InativarProduto(db *sql.DB, empresaID, atorID, produtoID, motivo string) error {
	motivoNorm, err := normalizarMotivoInativacao(motivo)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	p, err := travarProdutoParaAtualizacaoTx(tx, empresaID, produtoID)
	if err != nil {
		return err
	}
	if p.inativo {
		return ErrProdutoJaInativo
	}

	rows, err := tx.Query(
		`SELECT e.nome FROM saldo_produto_estoque s
		 JOIN estoques e ON e.id = s.estoque_id AND e.empresa_id = $2
		 WHERE s.produto_id = $1 AND s.quantidade > 0
		 ORDER BY e.nome, e.id`,
		produtoID, empresaID,
	)
	if err != nil {
		return fmt.Errorf("falha ao ler saldo do produto: %w", err)
	}
	var estoques []string
	for rows.Next() {
		var nome string
		if err := rows.Scan(&nome); err != nil {
			rows.Close()
			return fmt.Errorf("falha ao ler estoque com saldo: %w", err)
		}
		estoques = append(estoques, nome)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("falha ao iterar estoques com saldo: %w", err)
	}
	rows.Close()

	var temReserva bool
	if err := tx.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM reservas_pedido_item WHERE produto_id = $1 AND empresa_id = $2)`,
		produtoID, empresaID,
	).Scan(&temReserva); err != nil {
		return fmt.Errorf("falha ao ler reservas do produto: %w", err)
	}
	if len(estoques) > 0 || temReserva {
		return &ErroProdutoComSaldo{Estoques: estoques, TemReserva: temReserva}
	}

	if _, err := tx.Exec(
		`UPDATE produtos SET inativado_em = now(), inativado_por = $3 WHERE id = $1 AND empresa_id = $2`,
		produtoID, empresaID, atorID,
	); err != nil {
		return fmt.Errorf("falha ao inativar produto: %w", err)
	}
	var motivoDetalhe any
	if motivoNorm != nil {
		motivoDetalhe = *motivoNorm
	}
	if err := registrarHistoricoProdutoTx(tx, empresaID, produtoID, atorID, AcaoProdutoInativado, map[string]any{"motivo": motivoDetalhe}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("falha ao commitar inativação do produto: %w", err)
	}
	return nil
}

// ReativarProduto reativa o Produto `produtoID`. Mesma trava FOR UPDATE; já
// ativo -> ErrProdutoJaAtivo; com EAN-13 em uso por outro Produto ativo da
// Empresa -> &ErroEANEmUso, nada gravado. Senão limpa `inativado_em/por` e
// grava o histórico `reativado` (`{}`).
func ReativarProduto(db *sql.DB, empresaID, atorID, produtoID string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	p, err := travarProdutoParaAtualizacaoTx(tx, empresaID, produtoID)
	if err != nil {
		return err
	}
	if !p.inativo {
		return ErrProdutoJaAtivo
	}
	if p.ean13.Valid {
		if err := garantirEANLivreTx(tx, empresaID, p.ean13.String, produtoID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(
		`UPDATE produtos SET inativado_em = NULL, inativado_por = NULL WHERE id = $1 AND empresa_id = $2`,
		produtoID, empresaID,
	); err != nil {
		return fmt.Errorf("falha ao reativar produto: %w", err)
	}
	if err := registrarHistoricoProdutoTx(tx, empresaID, produtoID, atorID, AcaoProdutoReativado, nil); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("falha ao commitar reativação do produto: %w", err)
	}
	return nil
}

// garantirEANLivreTx garante, dentro da transação `tx`, que nenhum OUTRO
// Produto ativo (`inativado_em IS NULL AND deleted_at IS NULL`) da Empresa
// usa o EAN-13 `ean`. Toma `pg_advisory_xact_lock` pela chave
// empresa+EAN para serializar gravações concorrentes do mesmo EAN (não há
// índice único). `excetoProdutoID` (pode ser "") é o próprio Produto.
// EAN vazio (após trim) não é checado. Em uso -> &ErroEANEmUso{Codigo, Nome}.
//
// Isolado para ser reaproveitado por cadastro/edição (Story 16.4).
func garantirEANLivreTx(tx *sql.Tx, empresaID, ean, excetoProdutoID string) error {
	ean = strings.TrimSpace(ean)
	if ean == "" {
		return nil
	}
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1::text || ':' || $2::text))`, empresaID, ean); err != nil {
		return fmt.Errorf("falha ao travar EAN: %w", err)
	}
	var excetoArg sql.NullString
	if excetoProdutoID != "" {
		excetoArg = sql.NullString{String: excetoProdutoID, Valid: true}
	}
	var codigo sql.NullString
	var nome string
	err := tx.QueryRow(
		`SELECT codigo, nome FROM produtos
		 WHERE empresa_id = $1 AND ean13 = $2
		   AND inativado_em IS NULL AND deleted_at IS NULL
		   AND ($3::uuid IS NULL OR id <> $3::uuid)
		 ORDER BY criado_em, id
		 LIMIT 1`,
		empresaID, ean, excetoArg,
	).Scan(&codigo, &nome)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("falha ao verificar EAN em uso: %w", err)
	}
	return &ErroEANEmUso{Codigo: codigo.String, Nome: nome}
}
