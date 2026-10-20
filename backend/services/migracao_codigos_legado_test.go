package services

import (
	"database/sql"
	"testing"
	"time"
)

// criarProdutoLegadoSemCodigo grava uma linha de `produtos` direto via SQL,
// simulando um Produto do corte único legado (FR-44/Story 9.4): sem
// `codigo`, sem `template_id` — exatamente como os 2.745 Produtos reais da
// Ferreira Costa que motivaram esta migração. `criadoEm` controla a ordem
// esperada de atribuição (mais antigo primeiro).
func criarProdutoLegadoSemCodigo(t *testing.T, db *sql.DB, empresaID, categoriaID, nome string, criadoEm time.Time) string {
	t.Helper()
	var id string
	if err := db.QueryRow(
		`INSERT INTO produtos (nome, categoria_id, empresa_id, criado_em) VALUES ($1, $2, $3, $4) RETURNING id`,
		nome, categoriaID, empresaID, criadoEm,
	).Scan(&id); err != nil {
		t.Fatalf("criarProdutoLegadoSemCodigo(%q): %v", nome, err)
	}
	return id
}

func lerCodigoProduto(t *testing.T, db *sql.DB, produtoID string) *string {
	t.Helper()
	var codigo sql.NullString
	if err := db.QueryRow(`SELECT codigo FROM produtos WHERE id = $1`, produtoID).Scan(&codigo); err != nil {
		t.Fatalf("lerCodigoProduto(%s): %v", produtoID, err)
	}
	if !codigo.Valid {
		return nil
	}
	c := codigo.String
	return &c
}

// TestAtribuirCodigosLegado_SequencialPorCriacaoIgnoraMescladoEDaCodigoAoInativado
// prova o caso central do feedback (Karla, Ferreira Costa, 2026-10-20):
//   - 3 Produtos legados sem código, criados em ordem conhecida -> ganham
//     código sequencial crescente NESSA ordem (mais antigo primeiro).
//   - Um Produto já com código -> nunca é tocado.
//   - Um Produto MESCLADO (`deleted_at`) sem código -> continua sem código
//     (não entra mais em nenhuma operação).
//   - Um Produto INATIVADO (mas não mesclado) sem código -> TAMBÉM ganha
//     código (histórico/consulta continuam precisando dele).
func TestAtribuirCodigosLegado_SequencialPorCriacaoIgnoraMescladoEDaCodigoAoInativado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	base := time.Now().Add(-10 * time.Hour).UTC()
	maisAntigo := criarProdutoLegadoSemCodigo(t, db, empresaTeste, categoriaID, "Legado Mais Antigo", base)
	meio := criarProdutoLegadoSemCodigo(t, db, empresaTeste, categoriaID, "Legado Do Meio", base.Add(time.Hour))
	maisNovo := criarProdutoLegadoSemCodigo(t, db, empresaTeste, categoriaID, "Legado Mais Novo", base.Add(2*time.Hour))

	jaTinhaCodigo, codigoOriginal := criarProdutoCatComSaldo(t, db, CriarProdutoInput{
		UnidadeMedida: "un", Nome: "Produto Com Codigo Previo", CategoriaID: categoriaID,
	}, "", 0)

	mesclado := criarProdutoLegadoSemCodigo(t, db, empresaTeste, categoriaID, "Legado Mesclado Sem Codigo", base)
	if _, err := db.Exec(`UPDATE produtos SET deleted_at = now() WHERE id = $1`, mesclado); err != nil {
		t.Fatalf("marcar mesclado: %v", err)
	}

	inativado := criarProdutoLegadoSemCodigo(t, db, empresaTeste, categoriaID, "Legado Inativado Sem Codigo", base.Add(30*time.Minute))
	if _, err := db.Exec(`UPDATE produtos SET inativado_em = now() WHERE id = $1`, inativado); err != nil {
		t.Fatalf("marcar inativado: %v", err)
	}

	diagAntes, err := DiagnosticarCodigosLegado(db)
	if err != nil {
		t.Fatalf("DiagnosticarCodigosLegado: %v", err)
	}
	if diagAntes.ProdutosSemCodigo != 4 { // 3 ativos + 1 inativado; mesclado não conta
		t.Fatalf("ProdutosSemCodigo (antes) = %d, want 4", diagAntes.ProdutosSemCodigo)
	}

	res, err := AtribuirCodigosLegado(db)
	if err != nil {
		t.Fatalf("AtribuirCodigosLegado: %v", err)
	}
	if res.ProdutosAtualizados != 4 {
		t.Fatalf("ProdutosAtualizados = %d, want 4", res.ProdutosAtualizados)
	}

	codAntigo, codMeio, codNovo := lerCodigoProduto(t, db, maisAntigo), lerCodigoProduto(t, db, meio), lerCodigoProduto(t, db, maisNovo)
	if codAntigo == nil || codMeio == nil || codNovo == nil {
		t.Fatalf("esperava código nos 3 legados ativos: antigo=%v meio=%v novo=%v", codAntigo, codMeio, codNovo)
	}
	if !(*codAntigo < *codMeio && *codMeio < *codNovo) {
		t.Errorf("ordem de atribuição errada: antigo=%s meio=%s novo=%s, want crescente por criado_em", *codAntigo, *codMeio, *codNovo)
	}

	if got := lerCodigoProduto(t, db, inativado); got == nil {
		t.Error("produto inativado continua sem código, want código atribuído")
	}

	if got := lerCodigoProduto(t, db, mesclado); got != nil {
		t.Errorf("produto mesclado ganhou código %q, want continuar nil (nunca mexer em mesclado)", *got)
	}

	if got := lerCodigoProduto(t, db, jaTinhaCodigo); got == nil || *got != codigoOriginal {
		t.Errorf("código do produto que já tinha = %v, want %q intocado", got, codigoOriginal)
	}

	diagDepois, err := DiagnosticarCodigosLegado(db)
	if err != nil {
		t.Fatalf("DiagnosticarCodigosLegado (depois): %v", err)
	}
	if diagDepois.ProdutosSemCodigo != 0 {
		t.Errorf("ProdutosSemCodigo (depois) = %d, want 0", diagDepois.ProdutosSemCodigo)
	}
}

// TestAtribuirCodigosLegado_NaoMisturaNumeracaoEntreEmpresas prova que a
// atribuição respeita a MESMA regra de proximoCodigoProduto: cada Empresa
// numera a partir do PRÓPRIO maior código existente, nunca a partir da
// contagem de outra Empresa.
func TestAtribuirCodigosLegado_NaoMisturaNumeracaoEntreEmpresas(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	outra := empresaDeTemplates(t, db, "codigos-legado-outra", "141400000001")
	categoriaOutra := categoriaIDPorCodigo(t, db, "04.001")
	_ = categoriaOutra // mesma categoria código existe em toda Empresa (lista padrão)
	var categoriaIDOutra string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE codigo = '04.001' AND empresa_id = $1`, outra.ID).Scan(&categoriaIDOutra); err != nil {
		t.Fatalf("categoria da outra empresa: %v", err)
	}

	// `empresaTeste` já tem um produto cadastrado com código "000001" (de
	// chamadas anteriores de CriarProduto neste processo de teste) — não
	// assume nenhum número específico, só que os novos vêm DEPOIS do maior
	// já existente, e que a Empresa "outra" (sem nenhum produto antes) começa
	// do zero, independente do que aconteceu em empresaTeste.
	legadoA := criarProdutoLegadoSemCodigo(t, db, empresaTeste, categoriaID, "Legado Empresa A", time.Now().UTC())
	legadoB := criarProdutoLegadoSemCodigo(t, db, outra.ID, categoriaIDOutra, "Legado Empresa B", time.Now().UTC())
	// Registrado DEPOIS do cleanup de empresaDeTemplates (que apaga
	// categorias/filiais/etc. da "outra" empresa) — cleanups rodam em ordem
	// LIFO, então este remove o Produto PRIMEIRO, liberando a FK de
	// categoria_id para o cleanup da Empresa conseguir apagar as categorias.
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM produtos WHERE id = $1`, legadoB) })

	if _, err := AtribuirCodigosLegado(db); err != nil {
		t.Fatalf("AtribuirCodigosLegado: %v", err)
	}

	codA, codB := lerCodigoProduto(t, db, legadoA), lerCodigoProduto(t, db, legadoB)
	if codA == nil || codB == nil {
		t.Fatalf("esperava código nos dois: A=%v B=%v", codA, codB)
	}
	if *codB != "000001" {
		t.Errorf("código da Empresa nova (sem nenhum produto antes) = %q, want \"000001\"", *codB)
	}
}

// TestAtribuirCodigosLegado_Idempotente prova que reexecutar não altera nada:
// 0 Produtos atualizados e nenhum código muda de valor.
func TestAtribuirCodigosLegado_Idempotente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	id := criarProdutoLegadoSemCodigo(t, db, empresaTeste, categoriaID, "Legado Idempotente", time.Now().UTC())

	if _, err := AtribuirCodigosLegado(db); err != nil {
		t.Fatalf("1ª execução: %v", err)
	}
	codigoApos1 := lerCodigoProduto(t, db, id)
	if codigoApos1 == nil {
		t.Fatal("esperava código após a 1ª execução")
	}

	res2, err := AtribuirCodigosLegado(db)
	if err != nil {
		t.Fatalf("2ª execução: %v", err)
	}
	if res2.ProdutosAtualizados != 0 {
		t.Errorf("2ª execução: ProdutosAtualizados = %d, want 0", res2.ProdutosAtualizados)
	}
	if got := lerCodigoProduto(t, db, id); got == nil || *got != *codigoApos1 {
		t.Errorf("código mudou entre execuções: antes=%s depois=%v", *codigoApos1, got)
	}
}
