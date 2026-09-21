package services

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// Story 11.4 (Epic 11, AD-24/AD-25, FR-14/FR-15): consumo FEFO (First Expired,
// First Out) de saldo de um par (Produto, Estoque). Função única e reutilizável
// — Baixa, Transferência e (Story 11.5) a Aprovação de Pedido debitam por aqui.

// consumoFonte descreve o que foi debitado de UMA fonte de saldo do par.
// `LoteID` é nil quando a fonte é o saldo legado de `produto_estoque` (saldo
// sem Lote e sem validade). `CriadoEm` é o zero de time.Time para o legado
// (`produto_estoque` não tem data de criação).
type consumoFonte struct {
	LoteID       *string
	Quantidade   float64
	DataValidade sql.NullString
	CriadoEm     time.Time
}

// fonteSaldo é uma fonte de saldo com quantidade > 0, já na ordem FEFO.
type fonteSaldo struct {
	loteID       *string
	quantidade   float64
	dataValidade sql.NullString
	criadoEm     time.Time
}

// ordenarFontesFEFO devolve as fontes na ordem de consumo: Lotes com validade
// (`data_validade, criado_em, id`), depois o saldo legado (sem Lote e sem
// validade, o mais antigo), depois Lotes sem validade (`criado_em, id`).
// `lotes` já deve conter apenas Lotes com quantidade > 0; `legado` <= 0 é
// ignorado. Função pura — testada isoladamente.
func ordenarFontesFEFO(lotes []fonteSaldo, legado float64) []fonteSaldo {
	comValidade := make([]fonteSaldo, 0, len(lotes))
	semValidade := make([]fonteSaldo, 0, len(lotes))
	for _, l := range lotes {
		if l.quantidade <= 0 {
			continue
		}
		if l.dataValidade.Valid {
			comValidade = append(comValidade, l)
		} else {
			semValidade = append(semValidade, l)
		}
	}
	idDe := func(f fonteSaldo) string {
		if f.loteID == nil {
			return ""
		}
		return *f.loteID
	}
	sort.SliceStable(comValidade, func(i, j int) bool {
		a, b := comValidade[i], comValidade[j]
		if a.dataValidade.String != b.dataValidade.String {
			return a.dataValidade.String < b.dataValidade.String
		}
		if !a.criadoEm.Equal(b.criadoEm) {
			return a.criadoEm.Before(b.criadoEm)
		}
		return idDe(a) < idDe(b)
	})
	sort.SliceStable(semValidade, func(i, j int) bool {
		a, b := semValidade[i], semValidade[j]
		if !a.criadoEm.Equal(b.criadoEm) {
			return a.criadoEm.Before(b.criadoEm)
		}
		return idDe(a) < idDe(b)
	})

	fontes := make([]fonteSaldo, 0, len(lotes)+1)
	fontes = append(fontes, comValidade...)
	if legado > 0 {
		fontes = append(fontes, fonteSaldo{quantidade: legado})
	}
	fontes = append(fontes, semValidade...)
	return fontes
}

// consumirFEFOTx debita `quantidade` do par (produtoID, estoqueID) por FEFO,
// dentro de `tx`, e devolve o que saiu de cada fonte, na ordem de consumo.
// PRÉ-CONDIÇÃO: o chamador já travou o par com travarSaldoParesTx e já
// validou `quantidade` contra saldoDisponivelParTx — as fontes são lidas aqui
// sem novo lock. Nenhuma tela/endpoint escolhe Lote.
//
// Aritmética arredondada a 3 casas (arredondar3); o débito de cada fonte é
// no máximo o seu saldo, então `quantidade` nunca fica negativa. Se as fontes
// não cobrirem `quantidade` (saldo físico inferior ao validado — não deveria
// ocorrer sob a pré-condição), devolve erro interno (inconsistência de saldo,
// não falta de saldo do usuário) e o chamador desfaz a transação. Lote esgotado NÃO é apagado. A escrita de
// Movimentação correspondente é responsabilidade do chamador (mesma tx).
func consumirFEFOTx(tx *sql.Tx, empresaID, produtoID, estoqueID string, quantidade float64) ([]consumoFonte, error) {
	rows, err := tx.Query(`
		SELECT id, quantidade, to_char(data_validade, 'YYYY-MM-DD'), criado_em
		FROM lotes
		WHERE produto_id = $1 AND estoque_id = $2 AND empresa_id = $3 AND quantidade > 0`,
		produtoID, estoqueID, empresaID)
	if err != nil {
		return nil, fmt.Errorf("falha ao ler lotes do par: %w", err)
	}
	var lotes []fonteSaldo
	for rows.Next() {
		var f fonteSaldo
		var id string
		if err := rows.Scan(&id, &f.quantidade, &f.dataValidade, &f.criadoEm); err != nil {
			rows.Close()
			return nil, fmt.Errorf("falha ao ler lote do par: %w", err)
		}
		f.loteID = &id
		f.quantidade = arredondar3(f.quantidade)
		lotes = append(lotes, f)
	}
	errRows := rows.Err()
	rows.Close()
	if errRows != nil {
		return nil, fmt.Errorf("falha ao iterar lotes do par: %w", errRows)
	}

	var legado float64
	err = tx.QueryRow(`
		SELECT pe.quantidade FROM produto_estoque pe
		JOIN produtos p ON p.id = pe.produto_id AND p.empresa_id = $3
		JOIN estoques e ON e.id = pe.estoque_id AND e.empresa_id = $3
		WHERE pe.produto_id = $1 AND pe.estoque_id = $2`,
		produtoID, estoqueID, empresaID).Scan(&legado)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("falha ao ler saldo legado do par: %w", err)
	}
	legado = arredondar3(legado)

	restante := arredondar3(quantidade)
	var consumos []consumoFonte
	for _, f := range ordenarFontesFEFO(lotes, legado) {
		if restante <= 0 {
			break
		}
		retirar := f.quantidade
		if restante < retirar {
			retirar = restante
		}
		if f.loteID != nil {
			if _, err := tx.Exec(
				`UPDATE lotes SET quantidade = quantidade - $1 WHERE id = $2`,
				retirar, *f.loteID); err != nil {
				return nil, fmt.Errorf("falha ao debitar lote: %w", err)
			}
		} else {
			if _, err := tx.Exec(
				`UPDATE produto_estoque SET quantidade = quantidade - $1 WHERE produto_id = $2 AND estoque_id = $3`,
				retirar, produtoID, estoqueID); err != nil {
				return nil, fmt.Errorf("falha ao debitar produto_estoque: %w", err)
			}
		}
		consumos = append(consumos, consumoFonte{
			LoteID:       f.loteID,
			Quantidade:   retirar,
			DataValidade: f.dataValidade,
			CriadoEm:     f.criadoEm,
		})
		restante = arredondar3(restante - retirar)
	}
	if restante > 0 {
		// Inalcançável após validarQuantidadeDisponivelTx (fontes >= disponível):
		// é inconsistência de saldo, não falta de saldo do usuário.
		return nil, fmt.Errorf("fontes FEFO insuficientes: faltam %s unidade(s) após validar o disponível", strconv.FormatFloat(restante, 'f', -1, 64))
	}
	return consumos, nil
}
