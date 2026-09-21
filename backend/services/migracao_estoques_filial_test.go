package services

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/lib/pq"
)

// Story 12.2 — testes da migração dos Estoques legados para a Filial padrão.
//
// A migração roda `SET NOT NULL` em `estoques.filial_id` no banco COMPARTILHADO
// e cria Filial para toda Empresa sem uma; os demais testes inserem Estoque
// legado sem Filial. Por isso o cleanup restaura a nulabilidade e apaga as
// Filiais que a migração criou para Empresas alheias a este teste.

func filialIDNotNull(t *testing.T, db *sql.DB) bool {
	t.Helper()
	var v string
	if err := db.QueryRow(
		`SELECT is_nullable FROM information_schema.columns
		 WHERE table_schema = current_schema() AND table_name = 'estoques' AND column_name = 'filial_id'`,
	).Scan(&v); err != nil {
		t.Fatalf("is_nullable de estoques.filial_id: %v", err)
	}
	return v == "NO"
}

// prepararMigracaoEstoquesFilial limpa os Estoques e registra o cleanup que
// devolve o banco ao estado de antes da migração.
func prepararMigracaoEstoquesFilial(t *testing.T, db *sql.DB) {
	t.Helper()
	limparProdutos(t, db)

	rows, err := db.Query(`SELECT id::text FROM empresas e WHERE NOT EXISTS (SELECT 1 FROM filiais f WHERE f.empresa_id = e.id)`)
	if err != nil {
		t.Fatalf("empresas sem filial: %v", err)
	}
	var semFilial []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		semFilial = append(semFilial, id)
	}
	rows.Close()

	t.Cleanup(func() {
		limparProdutos(t, db)
		for _, id := range semFilial {
			if _, err := db.Exec(`DELETE FROM filiais WHERE empresa_id = $1`, id); err != nil {
				t.Errorf("cleanup filiais de %s: %v", id, err)
			}
		}
		if _, err := db.Exec(`ALTER TABLE estoques ALTER COLUMN filial_id DROP NOT NULL`); err != nil {
			t.Errorf("cleanup: restaurar nulabilidade de estoques.filial_id: %v", err)
		}
	})
	// Garante o ponto de partida mesmo se um teste anterior deixou NOT NULL.
	if _, err := db.Exec(`ALTER TABLE estoques ALTER COLUMN filial_id DROP NOT NULL`); err != nil {
		t.Fatalf("restaurar nulabilidade: %v", err)
	}
}

func semearEstoqueLegado(t *testing.T, db *sql.DB, empresaID, nome string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`INSERT INTO estoques (nome, empresa_id) VALUES ($1, $2) RETURNING id`, nome, empresaID).Scan(&id); err != nil {
		t.Fatalf("seed estoque legado %q: %v", nome, err)
	}
	return id
}

func filialDoEstoque(t *testing.T, db *sql.DB, estoqueID string) sql.NullString {
	t.Helper()
	var f sql.NullString
	if err := db.QueryRow(`SELECT filial_id::text FROM estoques WHERE id = $1`, estoqueID).Scan(&f); err != nil {
		t.Fatalf("filial do estoque: %v", err)
	}
	return f
}

func TestMigrarEstoquesParaFilial_CorteInicial(t *testing.T) {
	db := testDB(t)
	prepararMigracaoEstoquesFilial(t, db)
	emp := empresaSemFilialDeTeste(t, db, "migra-estoques-inicial", "778889990201", "Migra Estoques Inicial")
	ids := []string{
		semearEstoqueLegado(t, db, emp.ID, "Central"),
		semearEstoqueLegado(t, db, emp.ID, "Obra 1"),
		semearEstoqueLegado(t, db, emp.ID, "Obra 2"),
	}

	diag, err := DiagnosticarEstoquesFilial(db)
	if err != nil {
		t.Fatalf("DiagnosticarEstoquesFilial: %v", err)
	}
	if diag.EstoquesAVincular != 3 || diag.EmpresasSemFilial < 1 || diag.FilialIDNotNull || len(diag.Problemas) != 0 {
		t.Errorf("diagnóstico = %+v", diag)
	}

	res, err := MigrarEstoquesParaFilial(db)
	if err != nil {
		t.Fatalf("MigrarEstoquesParaFilial: %v", err)
	}
	if res.EstoquesVinculados != 3 || res.FiliaisCriadas < 1 {
		t.Errorf("resultado = %+v, want EstoquesVinculados=3 FiliaisCriadas>=1", res)
	}

	var filialID, filialNome string
	if err := db.QueryRow(`SELECT id::text, nome FROM filiais WHERE empresa_id = $1`, emp.ID).Scan(&filialID, &filialNome); err != nil {
		t.Fatalf("filial padrão criada: %v", err)
	}
	if filialNome != "Migra Estoques Inicial" {
		t.Errorf("nome da filial = %q, want o Nome Fantasia", filialNome)
	}
	for _, id := range ids {
		if f := filialDoEstoque(t, db, id); !f.Valid || f.String != filialID {
			t.Errorf("estoque %s filial = %v, want %s", id, f, filialID)
		}
	}
	if !filialIDNotNull(t, db) {
		t.Error("estoques.filial_id deveria ser NOT NULL")
	}
	if _, err := db.Exec(`INSERT INTO estoques (nome, empresa_id) VALUES ('Sem Filial', $1)`, emp.ID); err == nil {
		t.Error("INSERT de Estoque sem filial_id deveria ser rejeitado")
	} else {
		var pqErr *pq.Error
		if !errors.As(err, &pqErr) || pqErr.Code != "23502" {
			t.Errorf("erro = %v, want not_null_violation (23502)", err)
		}
	}

	lista, err := ListarEstoques(db, emp.ID)
	if err != nil {
		t.Fatalf("ListarEstoques: %v", err)
	}
	if len(lista) != 3 {
		t.Fatalf("estoques listados = %d, want 3", len(lista))
	}
	for _, x := range lista {
		if x.FilialID == nil || *x.FilialID != filialID || x.FilialNome == nil || *x.FilialNome != "Migra Estoques Inicial" {
			t.Errorf("listagem: %+v, want filial preenchida", x)
		}
	}
}

func TestMigrarEstoquesParaFilial_EmpresaJaComFilialUsaAMaisAntiga(t *testing.T) {
	db := testDB(t)
	prepararMigracaoEstoquesFilial(t, db)
	emp := criarEmpresaDeTeste(t, db, "migra-estoques-com-filial", "778889990202", "Migra Estoques Com Filial")
	t.Cleanup(func() { removerEmpresaComDados(t, db, "migra-estoques-com-filial") })
	if _, err := db.Exec(`DELETE FROM filiais WHERE empresa_id = $1`, emp.ID); err != nil {
		t.Fatalf("limpar filiais: %v", err)
	}
	var antiga string
	if err := db.QueryRow(`INSERT INTO filiais (empresa_id, nome, criado_em) VALUES ($1, 'Antiga', now() - interval '2 hours') RETURNING id::text`, emp.ID).Scan(&antiga); err != nil {
		t.Fatalf("filial antiga: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO filiais (empresa_id, nome, criado_em) VALUES ($1, 'Nova', now())`, emp.ID); err != nil {
		t.Fatalf("filial nova: %v", err)
	}
	id := semearEstoqueLegado(t, db, emp.ID, "Legado")

	if _, err := MigrarEstoquesParaFilial(db); err != nil {
		t.Fatalf("MigrarEstoquesParaFilial: %v", err)
	}
	if f := filialDoEstoque(t, db, id); !f.Valid || f.String != antiga {
		t.Errorf("filial = %v, want a mais antiga %s", f, antiga)
	}
	if n := contar(t, db, `SELECT count(*) FROM filiais WHERE empresa_id = $1`, emp.ID); n != 2 {
		t.Errorf("filiais = %d, want 2 (nenhuma criada)", n)
	}
}

// Homônimo já vinculado a uma Filial que NÃO é a padrão não colide (o índice é
// por Filial); homônimo na Filial padrão (a mais antiga) aborta.
func TestMigrarEstoquesParaFilial_HomonimoEmFilialNaoPadrao(t *testing.T) {
	db := testDB(t)
	prepararMigracaoEstoquesFilial(t, db)
	emp := criarEmpresaDeTeste(t, db, "migra-estoques-homonimo", "778889990210", "Migra Estoques Homonimo")
	t.Cleanup(func() { removerEmpresaComDados(t, db, "migra-estoques-homonimo") })
	if _, err := db.Exec(`DELETE FROM filiais WHERE empresa_id = $1`, emp.ID); err != nil {
		t.Fatalf("limpar filiais: %v", err)
	}
	var antiga, nova string
	if err := db.QueryRow(`INSERT INTO filiais (empresa_id, nome, criado_em) VALUES ($1, 'Antiga', now() - interval '2 hours') RETURNING id::text`, emp.ID).Scan(&antiga); err != nil {
		t.Fatalf("filial antiga: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO filiais (empresa_id, nome, criado_em) VALUES ($1, 'Nova', now()) RETURNING id::text`, emp.ID).Scan(&nova); err != nil {
		t.Fatalf("filial nova: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO estoques (nome, empresa_id, filial_id) VALUES ('Central', $1, $2)`, emp.ID, nova); err != nil {
		t.Fatalf("seed vinculado à filial nova: %v", err)
	}
	id := semearEstoqueLegado(t, db, emp.ID, "central")

	res, err := MigrarEstoquesParaFilial(db)
	if err != nil {
		t.Fatalf("homônimo em Filial não padrão não deveria abortar: %v", err)
	}
	if res.EstoquesVinculados != 1 {
		t.Errorf("vinculados = %d, want 1", res.EstoquesVinculados)
	}
	if f := filialDoEstoque(t, db, id); !f.Valid || f.String != antiga {
		t.Errorf("filial = %v, want a padrão %s", f, antiga)
	}
}

func TestMigrarEstoquesParaFilial_IdempotenteENaoTocaVinculados(t *testing.T) {
	db := testDB(t)
	prepararMigracaoEstoquesFilial(t, db)
	emp := empresaSemFilialDeTeste(t, db, "migra-estoques-idem", "778889990203", "Migra Estoques Idem")
	var outra string
	if err := db.QueryRow(`INSERT INTO filiais (empresa_id, nome, criado_em) VALUES ($1, 'Vinculada', now() + interval '1 hour') RETURNING id::text`, emp.ID).Scan(&outra); err != nil {
		t.Fatalf("filial: %v", err)
	}
	var vinculado string
	if err := db.QueryRow(`INSERT INTO estoques (nome, empresa_id, filial_id) VALUES ('Ja Vinculado', $1, $2) RETURNING id::text`, emp.ID, outra).Scan(&vinculado); err != nil {
		t.Fatalf("estoque vinculado: %v", err)
	}
	legado := semearEstoqueLegado(t, db, emp.ID, "Legado")

	// Empresa com uma única Filial (a "Vinculada", não a padrão por nome): o
	// legado vai para ela; o já vinculado continua onde está.
	res, err := MigrarEstoquesParaFilial(db)
	if err != nil {
		t.Fatalf("1ª execução: %v", err)
	}
	if res.EstoquesVinculados != 1 {
		t.Errorf("1ª execução vinculou %d, want 1", res.EstoquesVinculados)
	}
	if f := filialDoEstoque(t, db, vinculado); f.String != outra {
		t.Errorf("estoque já vinculado mudou de filial: %v", f)
	}
	if f := filialDoEstoque(t, db, legado); f.String != outra {
		t.Errorf("legado filial = %v, want %s", f, outra)
	}

	res, err = MigrarEstoquesParaFilial(db)
	if err != nil {
		t.Fatalf("2ª execução: %v", err)
	}
	if res.FiliaisCriadas != 0 || res.EstoquesVinculados != 0 {
		t.Errorf("2ª execução = %+v, want 0/0", res)
	}
	if !filialIDNotNull(t, db) {
		t.Error("estoques.filial_id deveria seguir NOT NULL")
	}
	diag, err := DiagnosticarEstoquesFilial(db)
	if err != nil {
		t.Fatalf("diagnóstico: %v", err)
	}
	if !diag.FilialIDNotNull || diag.EstoquesAVincular != 0 || diag.EstoquesJaVinculados != 2 || diag.EmpresasSemFilial != 0 {
		t.Errorf("diagnóstico pós-migração = %+v", diag)
	}
}

func TestMigrarEstoquesParaFilial_DuasEmpresas(t *testing.T) {
	db := testDB(t)
	prepararMigracaoEstoquesFilial(t, db)
	a := empresaSemFilialDeTeste(t, db, "migra-estoques-a", "778889990204", "Migra Estoques A")
	b := empresaSemFilialDeTeste(t, db, "migra-estoques-b", "778889990205", "Migra Estoques B")
	ea := semearEstoqueLegado(t, db, a.ID, "Central")
	eb := semearEstoqueLegado(t, db, b.ID, "Central") // mesmo nome, Empresas distintas: não colide

	if _, err := MigrarEstoquesParaFilial(db); err != nil {
		t.Fatalf("MigrarEstoquesParaFilial: %v", err)
	}
	for _, c := range []struct{ estoque, empresa, nome string }{{ea, a.ID, "Migra Estoques A"}, {eb, b.ID, "Migra Estoques B"}} {
		var filialEmpresa, nome string
		if err := db.QueryRow(`SELECT f.empresa_id::text, f.nome FROM estoques e JOIN filiais f ON f.id = e.filial_id WHERE e.id = $1`, c.estoque).Scan(&filialEmpresa, &nome); err != nil {
			t.Fatalf("filial do estoque: %v", err)
		}
		if filialEmpresa != c.empresa || nome != c.nome {
			t.Errorf("estoque %s na filial (%s,%q), want (%s,%q)", c.estoque, filialEmpresa, nome, c.empresa, c.nome)
		}
	}
}

func TestMigrarEstoquesParaFilial_EmpresaSemEstoqueGanhaFilial(t *testing.T) {
	db := testDB(t)
	prepararMigracaoEstoquesFilial(t, db)
	emp := empresaSemFilialDeTeste(t, db, "migra-estoques-vazia", "778889990206", "Migra Estoques Vazia")

	res, err := MigrarEstoquesParaFilial(db)
	if err != nil {
		t.Fatalf("MigrarEstoquesParaFilial: %v", err)
	}
	if res.EstoquesVinculados != 0 {
		t.Errorf("Vinculados = %d, want 0", res.EstoquesVinculados)
	}
	if n := contar(t, db, `SELECT count(*) FROM filiais WHERE empresa_id = $1 AND nome = 'Migra Estoques Vazia'`, emp.ID); n != 1 {
		t.Errorf("filiais da empresa = %d, want 1", n)
	}
	if !filialIDNotNull(t, db) {
		t.Error("NOT NULL deveria ter sido aplicado mesmo sem Estoque legado")
	}
}

func TestMigrarEstoquesParaFilial_EstoqueSemEmpresaAborta(t *testing.T) {
	db := testDB(t)
	prepararMigracaoEstoquesFilial(t, db)
	emp := empresaSemFilialDeTeste(t, db, "migra-estoques-sem-emp", "778889990207", "Migra Estoques Sem Emp")
	bom := semearEstoqueLegado(t, db, emp.ID, "Bom")
	var orfao string
	if err := db.QueryRow(`INSERT INTO estoques (nome) VALUES ('Orfao') RETURNING id::text`).Scan(&orfao); err != nil {
		t.Fatalf("estoque sem empresa: %v", err)
	}

	assertNadaEscrito := func() {
		t.Helper()
		if f := filialDoEstoque(t, db, bom); f.Valid {
			t.Errorf("estoque bom vinculado (%v) apesar do aborto", f)
		}
		if n := contar(t, db, `SELECT count(*) FROM filiais WHERE empresa_id = $1`, emp.ID); n != 0 {
			t.Errorf("filiais criadas = %d, want 0", n)
		}
		if filialIDNotNull(t, db) {
			t.Error("estoques.filial_id deveria seguir NULLABLE")
		}
	}

	diag, err := DiagnosticarEstoquesFilial(db)
	if err != nil {
		t.Fatalf("diagnóstico: %v", err)
	}
	if len(diag.Problemas) != 1 || diag.Problemas[0].EstoqueID != orfao || diag.Problemas[0].Motivo != "sem empresa_id" {
		t.Errorf("problemas = %+v", diag.Problemas)
	}

	_, err = MigrarEstoquesParaFilial(db)
	var naoMigravel *ErroEstoqueNaoMigravel
	if !errors.As(err, &naoMigravel) {
		t.Fatalf("erro = %v, want ErroEstoqueNaoMigravel", err)
	}
	assertNadaEscrito()
}

func TestMigrarEstoquesParaFilial_NomeDuplicadoAborta(t *testing.T) {
	db := testDB(t)

	casos := map[string]func(t *testing.T, empresaID, filialPadrao string){
		"legado x ja vinculado a filial padrao": func(t *testing.T, empresaID, filialPadrao string) {
			semearEstoqueLegado(t, db, empresaID, "Central")
			if _, err := db.Exec(`INSERT INTO estoques (nome, empresa_id, filial_id) VALUES ('central', $1, $2)`, empresaID, filialPadrao); err != nil {
				t.Fatalf("seed vinculado: %v", err)
			}
		},
		"legado x legado da mesma empresa": func(t *testing.T, empresaID, _ string) {
			semearEstoqueLegado(t, db, empresaID, "Central")
			semearEstoqueLegado(t, db, empresaID, "  CENTRAL ")
		},
	}
	for nome, semear := range casos {
		t.Run(nome, func(t *testing.T) {
			prepararMigracaoEstoquesFilial(t, db)
			emp := criarEmpresaDeTeste(t, db, "migra-estoques-dup", "778889990208", "Migra Estoques Dup")
			t.Cleanup(func() { removerEmpresaComDados(t, db, "migra-estoques-dup") })
			padrao := filialTeste(t, db, emp.ID)
			semear(t, emp.ID, padrao)
			outra := empresaSemFilialDeTeste(t, db, "migra-estoques-dup-outra", "778889990211", "Migra Estoques Dup Outra")

			diag, err := DiagnosticarEstoquesFilial(db)
			if err != nil {
				t.Fatalf("diagnóstico: %v", err)
			}
			if len(diag.Problemas) == 0 || diag.Problemas[0].Motivo != "nome duplicado na Filial padrão" {
				t.Errorf("problemas = %+v", diag.Problemas)
			}

			_, err = MigrarEstoquesParaFilial(db)
			var naoMigravel *ErroEstoqueNaoMigravel
			if !errors.As(err, &naoMigravel) {
				t.Fatalf("erro = %v, want ErroEstoqueNaoMigravel", err)
			}
			if n := contar(t, db, `SELECT count(*) FROM estoques WHERE empresa_id = $1 AND filial_id IS NULL`, emp.ID); n == 0 {
				t.Error("nenhum legado deveria ter sido vinculado")
			}
			if filialIDNotNull(t, db) {
				t.Error("estoques.filial_id deveria seguir NULLABLE")
			}
			// Nada escrito: a Empresa sem Filial não ganha Filial no abort.
			if n := contar(t, db, `SELECT count(*) FROM filiais WHERE empresa_id = $1`, outra.ID); n != 0 {
				t.Errorf("filiais da empresa sem filial = %d, want 0 (abort não escreve)", n)
			}
		})
	}
}

func TestDiagnosticarEstoquesFilial_NaoEscreve(t *testing.T) {
	db := testDB(t)
	prepararMigracaoEstoquesFilial(t, db)
	emp := empresaSemFilialDeTeste(t, db, "migra-estoques-diag", "778889990209", "Migra Estoques Diag")
	id := semearEstoqueLegado(t, db, emp.ID, "Legado")

	if _, err := DiagnosticarEstoquesFilial(db); err != nil {
		t.Fatalf("diagnóstico: %v", err)
	}
	if n := contar(t, db, `SELECT count(*) FROM filiais WHERE empresa_id = $1`, emp.ID); n != 0 {
		t.Errorf("filiais = %d, want 0", n)
	}
	if f := filialDoEstoque(t, db, id); f.Valid {
		t.Errorf("estoque vinculado no dry-run: %v", f)
	}
	if filialIDNotNull(t, db) {
		t.Error("estoques.filial_id deveria seguir NULLABLE")
	}
}
