package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

// Estoque é a projeção somente-leitura de um local de estoque devolvida por
// POST/GET /api/estoques (Story 2.1): `id` (UUID v4) + `nome`, nada mais — a
// tela "Locais" e as telas de catálogo (Epic 4) só precisam disso.
type Estoque struct {
	ID   string `json:"id"`
	Nome string `json:"nome"`
}

var (
	// ErrEstoqueValidacao indica nome de Estoque vazio após o trim ou com mais
	// de 255 runes — mapeado para 400 VALIDATION_ERROR, sem tocar o banco.
	ErrEstoqueValidacao = errors.New("o nome do estoque é obrigatório e deve ter no máximo 255 caracteres")
	// ErrNomeEstoqueDuplicado indica que o nome normalizado (minúsculas +
	// espaços colapsados) já existe: a segunda linha bateu no índice único
	// idx_estoques_nome_normalizado (SQLSTATE 23505). É backstop de corrida —
	// não há checagem prévia. Mapeado para 409 CONFLICT.
	ErrNomeEstoqueDuplicado = errors.New("já existe um estoque com esse nome")
	// ErrEstoqueNaoEncontrado indica `id` inexistente OU malformado (não-UUID,
	// `pq` SQLSTATE 22P02) — os dois caem no mesmo erro. Mapeado para
	// 404 NOT_FOUND.
	ErrEstoqueNaoEncontrado = errors.New("estoque não encontrado")
)

// CriarEstoque valida e insere um novo local de estoque (Story 2.1, FR12).
//
// O `nome` recebido é trimado nas pontas; vazio após o trim ou com mais de
// 255 runes -> ErrEstoqueValidacao (nenhuma escrita). Caso válido, um único
// INSERT ... RETURNING grava a linha — a coluna gerada `nome_normalizado` e
// o índice único idx_estoques_nome_normalizado impõem a unicidade de nome no
// próprio banco. Uma violação de unicidade (`pq` SQLSTATE 23505) é o único
// sinal de nome duplicado: traduzida para ErrNomeEstoqueDuplicado, sem
// nenhum SELECT-antes-de-INSERT que teria janela de corrida sob requisições
// concorrentes.
func CriarEstoque(db *sql.DB, nome string) (Estoque, error) {
	nomeTrimado := strings.TrimSpace(nome)
	if nomeTrimado == "" || utf8.RuneCountInString(nomeTrimado) > 255 {
		return Estoque{}, ErrEstoqueValidacao
	}

	var e Estoque
	const insert = `INSERT INTO estoques (nome) VALUES ($1) RETURNING id, nome`
	if err := db.QueryRow(insert, nomeTrimado).Scan(&e.ID, &e.Nome); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation {
			return Estoque{}, ErrNomeEstoqueDuplicado
		}
		return Estoque{}, fmt.Errorf("falha ao inserir estoque: %w", err)
	}
	return e, nil
}

// ListarEstoques devolve todos os locais de estoque ordenados por
// `nome_normalizado` ascendente (Story 2.1, AC4). Sem filtro de escopo:
// qualquer conta autenticada vê todos os Estoques (o gate de leitura é só
// RequireAuth em newMux). Lista vazia não é erro — devolve um slice vazio,
// nunca nil.
func ListarEstoques(db *sql.DB) ([]Estoque, error) {
	rows, err := db.Query(`SELECT id, nome FROM estoques ORDER BY nome_normalizado ASC`)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar estoques: %w", err)
	}
	defer rows.Close()

	estoques := make([]Estoque, 0)
	for rows.Next() {
		var e Estoque
		if err := rows.Scan(&e.ID, &e.Nome); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de estoque: %w", err)
		}
		estoques = append(estoques, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar estoques: %w", err)
	}
	return estoques, nil
}

// ExcluirEstoque remove o local de estoque de `id` (DELETE /api/estoques/{id},
// Story 2.2, FR12). Um `id` não-UUID (`pq` SQLSTATE 22P02, reusa a constante
// de pacote pqInvalidTextRepresentation) ou um `id` UUID válido sem linha
// correspondente (`RowsAffected() == 0`) colapsam em ErrEstoqueNaoEncontrado —
// a mesma decisão de carregarAlvoParaGestao (gestao_usuarios.go) e
// DecidirSolicitacao (promocao.go): input de cliente inválido não vira 500 e
// não se vaza "este id existe mas está errado".
//
// Guard de quantidade residual (Story 3.1, completando o guard que a Story
// 2.2 deixou pendente até `produto_estoque` existir): numa única transação, um
// SELECT junta `produto_estoque`/`produtos` para os Produtos com quantidade
// positiva nesse Estoque, ordenados por nome. Se houver alguma linha, a
// transação é desfeita (ROLLBACK, via o `defer tx.Rollback()` — o `DELETE`
// nunca chega a rodar) e o erro devolvido é *ErroEstoqueComResiduo, com os
// nomes; lista vazia segue para o `DELETE` já existente, mesma transação. O
// guard de Pedido `pendente` (PEDIDOS, Epic 7) é acrescentado pela Story 7.2
// quando essa tabela existir.
//
// Corrida com services.CriarProduto (revisão pós-implementação, Story 3.1):
// sem travar a linha de `estoques` ANTES do SELECT de resíduo, um
// CriarProduto concorrente podia commitar um INSERT em `produto_estoque`
// (quantidade > 0) para este `estoque_id` bem depois do SELECT abaixo já ter
// visto "sem resíduo" mas antes do `DELETE` final commitar — e como
// `produto_estoque.estoque_id` é `ON DELETE CASCADE`, essa linha de resíduo
// recém-criada seria apagada silenciosamente junto do Estoque, furando o
// guard por completo sem qualquer erro. Por isso a primeira coisa que esta
// função faz, ainda antes do SELECT de resíduo, é `SELECT ... FOR UPDATE` na
// própria linha do Estoque: Postgres exige um lock em modo KEY SHARE sobre a
// linha referenciada para qualquer INSERT que a referencie via FK (o INSERT
// em `produto_estoque` feito por CriarProduto), e KEY SHARE conflita com
// FOR UPDATE — então um CriarProduto concorrente fica bloqueado até esta
// transação commitar (e aí sua própria FK falha, porque o Estoque já não
// existe mais -> CriarProduto devolve erro de validação) ou desfazer (e aí
// CriarProduto segue normalmente, e o SELECT de resíduo desta chamada —
// que já teria sido refeito, pois só roda depois do lock ser adquirido —
// enxerga a linha nova). As duas ordens de chegada são seguras; a janela que
// perdia dado silenciosamente deixa de existir.
func ExcluirEstoque(db *sql.DB, id string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	var idTravado string
	if err := tx.QueryRow(`SELECT id FROM estoques WHERE id = $1 FOR UPDATE`, id).Scan(&idTravado); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return ErrEstoqueNaoEncontrado
		}
		if errors.Is(err, sql.ErrNoRows) {
			return ErrEstoqueNaoEncontrado
		}
		return fmt.Errorf("falha ao travar estoque para exclusão: %w", err)
	}

	// Daqui em diante `id` já foi provado UUID válido e existente pelo lock
	// acima (mantido sob a mesma transação) — os ramos de
	// pqInvalidTextRepresentation abaixo, e o `RowsAffected() == 0` do
	// DELETE, ficam como defesa em profundidade (nunca deveriam disparar na
	// prática, mas custam nada e blindam contra o formato do lock acima mudar
	// no futuro sem que este comentário seja atualizado junto).
	const selectResiduo = `
		SELECT p.nome
		FROM produto_estoque pe
		JOIN produtos p ON p.id = pe.produto_id
		WHERE pe.estoque_id = $1 AND pe.quantidade > 0 AND p.deleted_at IS NULL
		ORDER BY p.nome`
	rows, err := tx.Query(selectResiduo, id)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return ErrEstoqueNaoEncontrado
		}
		return fmt.Errorf("falha ao verificar quantidade residual do estoque: %w", err)
	}
	produtos := make([]string, 0)
	for rows.Next() {
		var nome string
		if err := rows.Scan(&nome); err != nil {
			rows.Close()
			return fmt.Errorf("falha ao ler produto com resíduo: %w", err)
		}
		produtos = append(produtos, nome)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("falha ao iterar produtos com resíduo: %w", err)
	}
	rows.Close()
	if len(produtos) > 0 {
		return &ErroEstoqueComResiduo{Produtos: produtos}
	}

	res, err := tx.Exec(`DELETE FROM estoques WHERE id = $1`, id)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			// Inalcançável na prática: o SELECT ... FOR UPDATE acima já
			// validou que `id` é um UUID sintaticamente válido antes de
			// chegarmos aqui. Mantido como defesa em profundidade.
			return ErrEstoqueNaoEncontrado
		}
		return fmt.Errorf("falha ao excluir estoque: %w", err)
	}
	linhas, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("falha ao ler linhas afetadas na exclusão de estoque: %w", err)
	}
	if linhas == 0 {
		// Também inalcançável na prática pelo mesmo motivo: o lock acima
		// garante que a linha existe e permanece sob esta transação até o
		// commit/rollback. Mantido como defesa em profundidade.
		return ErrEstoqueNaoEncontrado
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("falha ao commitar exclusão de estoque: %w", err)
	}
	return nil
}
