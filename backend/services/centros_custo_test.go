package services

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// Testes de Centros de Custo — Story 12.3 (spec-12-3).

func limparCentrosCustoDeTeste(t *testing.T, db *sql.DB, empresaIDs ...string) {
	t.Helper()
	limpar := func() {
		for _, id := range empresaIDs {
			// Pedidos que referenciam o Centro precisam sair antes (FK).
			_, _ = db.Exec(`DELETE FROM pedido_itens WHERE pedido_id IN (SELECT id FROM pedidos WHERE centro_custo_id IN (SELECT id FROM centros_custo WHERE empresa_id = $1))`, id)
			_, _ = db.Exec(`DELETE FROM reservas_pedido_item WHERE pedido_id IN (SELECT id FROM pedidos WHERE centro_custo_id IN (SELECT id FROM centros_custo WHERE empresa_id = $1))`, id)
			_, _ = db.Exec(`DELETE FROM pedidos WHERE centro_custo_id IN (SELECT id FROM centros_custo WHERE empresa_id = $1)`, id)
			_, _ = db.Exec(`DELETE FROM centros_custo WHERE empresa_id = $1`, id)
		}
	}
	limpar()
	t.Cleanup(limpar)
}

func TestCriarCentroCusto_ValidacaoDuplicidadeEIsolamento(t *testing.T) {
	db := testDB(t)
	removerEmpresaDeTeste(t, db, "cc-a")
	removerEmpresaDeTeste(t, db, "cc-b")
	a := criarEmpresaDeTeste(t, db, "cc-a", "778889990201", "CC A")
	b := criarEmpresaDeTeste(t, db, "cc-b", "778889990202", "CC B")
	t.Cleanup(func() {
		removerEmpresaDeTeste(t, db, "cc-a")
		removerEmpresaDeTeste(t, db, "cc-b")
	})

	c, err := CriarCentroCusto(db, a.ID, "  Estoque do Cabo  ")
	if err != nil {
		t.Fatalf("CriarCentroCusto: %v", err)
	}
	if c.Nome != "Estoque do Cabo" || c.ID == "" {
		t.Errorf("centro = %+v", c)
	}
	if _, err := CriarCentroCusto(db, a.ID, "estoque   do cabo "); !errors.Is(err, ErrNomeCentroCustoDuplicado) {
		t.Errorf("duplicado: erro = %v, want ErrNomeCentroCustoDuplicado", err)
	}
	if _, err := CriarCentroCusto(db, b.ID, "Estoque do Cabo"); err != nil {
		t.Errorf("mesmo nome em outra Empresa: %v", err)
	}
	for _, nome := range []string{"", "   ", strings.Repeat("x", 256), "a\x00b"} {
		if _, err := CriarCentroCusto(db, a.ID, nome); !errors.Is(err, ErrCentroCustoValidacao) {
			t.Errorf("CriarCentroCusto(%.10q): erro = %v, want ErrCentroCustoValidacao", nome, err)
		}
	}
	if _, err := CriarCentroCusto(db, a.ID, strings.Repeat("é", 255)); err != nil {
		t.Errorf("255 runes deve ser aceito: %v", err)
	}
}

func TestListarCentrosCusto_OrdenadaEEscopadaPorEmpresa(t *testing.T) {
	db := testDB(t)
	removerEmpresaDeTeste(t, db, "cc-lista-a")
	removerEmpresaDeTeste(t, db, "cc-lista-b")
	a := criarEmpresaDeTeste(t, db, "cc-lista-a", "778889990203", "CC Lista A")
	b := criarEmpresaDeTeste(t, db, "cc-lista-b", "778889990204", "CC Lista B")
	t.Cleanup(func() {
		removerEmpresaDeTeste(t, db, "cc-lista-a")
		removerEmpresaDeTeste(t, db, "cc-lista-b")
	})

	vazia, err := ListarCentrosCusto(db, a.ID)
	if err != nil || vazia == nil || len(vazia) != 0 {
		t.Fatalf("lista vazia = %v (err=%v), want slice vazio não-nil", vazia, err)
	}

	for _, nome := range []string{"Zebra", "obra Beta", "Alfa"} {
		if _, err := CriarCentroCusto(db, a.ID, nome); err != nil {
			t.Fatalf("seed %s: %v", nome, err)
		}
	}
	if _, err := CriarCentroCusto(db, b.ID, "Outra Empresa"); err != nil {
		t.Fatalf("seed B: %v", err)
	}
	lista, err := ListarCentrosCusto(db, a.ID)
	if err != nil {
		t.Fatalf("ListarCentrosCusto: %v", err)
	}
	var nomes []string
	for _, c := range lista {
		nomes = append(nomes, c.Nome)
	}
	if got := strings.Join(nomes, "|"); got != "Alfa|obra Beta|Zebra" {
		t.Errorf("centros = %q, want Alfa|obra Beta|Zebra", got)
	}
}
