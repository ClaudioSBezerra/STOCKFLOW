package services

import (
	"database/sql"
	"testing"
)

// criarProdutoSemTemplate grava uma linha de `produtos` direto via SQL,
// simulando um Produto que nasceu fora de `CriarProduto` (importação de
// planilha antes da correção de 2026-11-12, ou os fixos de exemplo do
// Ambiente de Treinamento): sem `template_id`, com `codigo`/
// `codigo_fornecedor` como o chamador quiser simular.
func criarProdutoSemTemplate(t *testing.T, db *sql.DB, empresaID, categoriaID, nome string, codigo, codigoFornecedor *string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(
		`INSERT INTO produtos (nome, categoria_id, empresa_id, codigo, codigo_fornecedor)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		nome, categoriaID, empresaID, codigo, codigoFornecedor,
	).Scan(&id); err != nil {
		t.Fatalf("criarProdutoSemTemplate(%q): %v", nome, err)
	}
	return id
}

func lerProdutoTemplateCodigos(t *testing.T, db *sql.DB, produtoID string) (templateID, codigo, codigoFornecedor sql.NullString) {
	t.Helper()
	if err := db.QueryRow(
		`SELECT template_id, codigo, codigo_fornecedor FROM produtos WHERE id = $1`, produtoID,
	).Scan(&templateID, &codigo, &codigoFornecedor); err != nil {
		t.Fatalf("lerProdutoTemplateCodigos(%s): %v", produtoID, err)
	}
	return
}

func ptr(s string) *string { return &s }

// TestMigrarProdutosSemTemplate_MoveCodigoLegadoParaFornecedorEGeraSequencial
// prova o caso central do bug real (Ferreira Costa, 2026-11-12): um Produto
// sem template e com `codigo` = valor legado/do fornecedor (não-numérico,
// ex. "B.1.02.C009") ganha o template Genérico, esse valor é preservado em
// `codigo_fornecedor`, e `codigo` passa a ser um sequencial novo gerado por
// proximoCodigoProduto.
func TestMigrarProdutosSemTemplate_MoveCodigoLegadoParaFornecedorEGeraSequencial(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	id := criarProdutoSemTemplate(t, db, empresaTeste, categoriaID, "Produto Codigo Legado", ptr("B.1.02.C009"), nil)

	diagAntes, err := DiagnosticarProdutosSemTemplate(db)
	if err != nil {
		t.Fatalf("DiagnosticarProdutosSemTemplate: %v", err)
	}
	if diagAntes.ProdutosSemTemplate != 1 {
		t.Fatalf("ProdutosSemTemplate (antes) = %d, want 1", diagAntes.ProdutosSemTemplate)
	}

	res, err := MigrarProdutosSemTemplate(db)
	if err != nil {
		t.Fatalf("MigrarProdutosSemTemplate: %v", err)
	}
	if res.ProdutosAtualizados != 1 {
		t.Fatalf("ProdutosAtualizados = %d, want 1", res.ProdutosAtualizados)
	}

	templateID, codigo, codigoFornecedor := lerProdutoTemplateCodigos(t, db, id)
	generico := templateGenericoID(t, db, empresaTeste)
	if !templateID.Valid || templateID.String != generico {
		t.Errorf("template_id = %+v, want o Genérico (%q)", templateID, generico)
	}
	if !codigoFornecedor.Valid || codigoFornecedor.String != "B.1.02.C009" {
		t.Errorf("codigo_fornecedor = %+v, want %q (valor legado preservado)", codigoFornecedor, "B.1.02.C009")
	}
	if !codigo.Valid || codigo.String == "B.1.02.C009" || len(codigo.String) != 6 {
		t.Errorf("codigo = %+v, want sequencial novo de 6 dígitos, nunca o valor legado", codigo)
	}

	diagDepois, err := DiagnosticarProdutosSemTemplate(db)
	if err != nil {
		t.Fatalf("DiagnosticarProdutosSemTemplate (depois): %v", err)
	}
	if diagDepois.ProdutosSemTemplate != 0 {
		t.Errorf("ProdutosSemTemplate (depois) = %d, want 0", diagDepois.ProdutosSemTemplate)
	}
}

// TestMigrarProdutosSemTemplate_CodigoNuloGanhaSequencial prova o subcaso
// "célula vazia" (ex. os 5 fixos do Ambiente de Treinamento): `codigo` NULL
// -> ganha sequencial novo; `codigo_fornecedor` continua NULL (nada a mover).
func TestMigrarProdutosSemTemplate_CodigoNuloGanhaSequencial(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	id := criarProdutoSemTemplate(t, db, empresaTeste, categoriaID, "Produto Sem Codigo Nenhum", nil, nil)

	if _, err := MigrarProdutosSemTemplate(db); err != nil {
		t.Fatalf("MigrarProdutosSemTemplate: %v", err)
	}

	templateID, codigo, codigoFornecedor := lerProdutoTemplateCodigos(t, db, id)
	generico := templateGenericoID(t, db, empresaTeste)
	if !templateID.Valid || templateID.String != generico {
		t.Errorf("template_id = %+v, want o Genérico (%q)", templateID, generico)
	}
	if !codigo.Valid || len(codigo.String) != 6 {
		t.Errorf("codigo = %+v, want sequencial novo de 6 dígitos", codigo)
	}
	if codigoFornecedor.Valid {
		t.Errorf("codigo_fornecedor = %q, want NULL (nada a mover)", codigoFornecedor.String)
	}
}

// TestMigrarProdutosSemTemplate_CodigoJaSequencialNuncaEMovido prova que um
// `codigo` que já pareça um sequencial válido (`^[0-9]{1,9}$`) nunca é
// tocado nem movido para `codigo_fornecedor` — só o template é preenchido.
func TestMigrarProdutosSemTemplate_CodigoJaSequencialNuncaEMovido(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	id := criarProdutoSemTemplate(t, db, empresaTeste, categoriaID, "Produto Codigo Ja Sequencial", ptr("000777"), nil)

	if _, err := MigrarProdutosSemTemplate(db); err != nil {
		t.Fatalf("MigrarProdutosSemTemplate: %v", err)
	}

	templateID, codigo, codigoFornecedor := lerProdutoTemplateCodigos(t, db, id)
	generico := templateGenericoID(t, db, empresaTeste)
	if !templateID.Valid || templateID.String != generico {
		t.Errorf("template_id = %+v, want o Genérico (%q)", templateID, generico)
	}
	if !codigo.Valid || codigo.String != "000777" {
		t.Errorf("codigo = %+v, want intocado (%q)", codigo, "000777")
	}
	if codigoFornecedor.Valid {
		t.Errorf("codigo_fornecedor = %q, want NULL (nunca mover um código já sequencial)", codigoFornecedor.String)
	}
}

// TestMigrarProdutosSemTemplate_NuncaSobrescreveCodigoFornecedorJaPreenchido
// prova a defesa: um Produto sem template cujo `codigo_fornecedor` já esteja
// preenchido (estado inesperado) nunca tem esse valor sobrescrito — o
// `codigo` legado não-numérico é descartado ao gerar o sequencial novo.
func TestMigrarProdutosSemTemplate_NuncaSobrescreveCodigoFornecedorJaPreenchido(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	id := criarProdutoSemTemplate(t, db, empresaTeste, categoriaID, "Produto Fornecedor Ja Preenchido", ptr("CRZ.0605"), ptr("JA-TINHA-FORNECEDOR"))

	if _, err := MigrarProdutosSemTemplate(db); err != nil {
		t.Fatalf("MigrarProdutosSemTemplate: %v", err)
	}

	templateID, codigo, codigoFornecedor := lerProdutoTemplateCodigos(t, db, id)
	generico := templateGenericoID(t, db, empresaTeste)
	if !templateID.Valid || templateID.String != generico {
		t.Errorf("template_id = %+v, want o Genérico (%q)", templateID, generico)
	}
	if !codigoFornecedor.Valid || codigoFornecedor.String != "JA-TINHA-FORNECEDOR" {
		t.Errorf("codigo_fornecedor = %+v, want inalterado (%q)", codigoFornecedor, "JA-TINHA-FORNECEDOR")
	}
	if !codigo.Valid || codigo.String == "CRZ.0605" || len(codigo.String) != 6 {
		t.Errorf("codigo = %+v, want sequencial novo, nunca o valor legado (não havia onde preservá-lo)", codigo)
	}
}

// TestMigrarProdutosSemTemplate_Idempotente prova que reexecutar não altera
// nada: 0 Produtos atualizados e nenhum campo muda de valor.
func TestMigrarProdutosSemTemplate_Idempotente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")

	id := criarProdutoSemTemplate(t, db, empresaTeste, categoriaID, "Produto Idempotente", ptr("B.9.99.X999"), nil)

	if _, err := MigrarProdutosSemTemplate(db); err != nil {
		t.Fatalf("1ª execução: %v", err)
	}
	templateApos1, codigoApos1, fornecedorApos1 := lerProdutoTemplateCodigos(t, db, id)

	res2, err := MigrarProdutosSemTemplate(db)
	if err != nil {
		t.Fatalf("2ª execução: %v", err)
	}
	if res2.ProdutosAtualizados != 0 {
		t.Errorf("2ª execução: ProdutosAtualizados = %d, want 0", res2.ProdutosAtualizados)
	}

	templateApos2, codigoApos2, fornecedorApos2 := lerProdutoTemplateCodigos(t, db, id)
	if templateApos1 != templateApos2 || codigoApos1 != codigoApos2 || fornecedorApos1 != fornecedorApos2 {
		t.Errorf("valores mudaram entre execuções: antes=(%v,%v,%v) depois=(%v,%v,%v)",
			templateApos1, codigoApos1, fornecedorApos1, templateApos2, codigoApos2, fornecedorApos2)
	}
}

// TestMigrarProdutosSemTemplate_NaoMisturaNumeracaoEntreEmpresas prova que a
// geração de código novo respeita a MESMA regra de proximoCodigoProduto:
// cada Empresa numera a partir do PRÓPRIO maior código existente.
func TestMigrarProdutosSemTemplate_NaoMisturaNumeracaoEntreEmpresas(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	outra := empresaDeTemplates(t, db, "sem-template-outra", "141400000002")
	var categoriaIDOutra string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE codigo = '04.001' AND empresa_id = $1`, outra.ID).Scan(&categoriaIDOutra); err != nil {
		t.Fatalf("categoria da outra empresa: %v", err)
	}

	idA := criarProdutoSemTemplate(t, db, empresaTeste, categoriaID, "Produto A", ptr("LEGADO-A"), nil)
	idB := criarProdutoSemTemplate(t, db, outra.ID, categoriaIDOutra, "Produto B", ptr("LEGADO-B"), nil)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM produtos WHERE id = $1`, idB) })

	if _, err := MigrarProdutosSemTemplate(db); err != nil {
		t.Fatalf("MigrarProdutosSemTemplate: %v", err)
	}

	_, codigoA, _ := lerProdutoTemplateCodigos(t, db, idA)
	_, codigoB, _ := lerProdutoTemplateCodigos(t, db, idB)
	if !codigoB.Valid || codigoB.String != "000001" {
		t.Errorf("código da Empresa nova (sem nenhum produto antes) = %v, want \"000001\"", codigoB)
	}
	if !codigoA.Valid {
		t.Error("esperava código gerado para o Produto da empresaTeste")
	}
}
