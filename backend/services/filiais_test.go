package services

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// Testes de Filiais — Story 12.1 (spec-12-1).

func TestCriarFilial_ValidacaoDuplicidadeEIsolamento(t *testing.T) {
	db := testDB(t)
	removerEmpresaDeTeste(t, db, "filial-a")
	removerEmpresaDeTeste(t, db, "filial-b")
	t.Cleanup(func() {
		removerEmpresaDeTeste(t, db, "filial-a")
		removerEmpresaDeTeste(t, db, "filial-b")
	})
	a := criarEmpresaDeTeste(t, db, "filial-a", "778889990101", "Filial A")
	b := criarEmpresaDeTeste(t, db, "filial-b", "778889990102", "Filial B")

	f, err := CriarFilial(db, a.ID, "  Recife  ")
	if err != nil {
		t.Fatalf("CriarFilial: %v", err)
	}
	if f.Nome != "Recife" || f.ID == "" {
		t.Errorf("filial = %+v", f)
	}
	if _, err := CriarFilial(db, a.ID, "recife"); !errors.Is(err, ErrNomeFilialDuplicado) {
		t.Errorf("duplicada: erro = %v, want ErrNomeFilialDuplicado", err)
	}
	// Mesmo nome em OUTRA Empresa é permitido.
	if _, err := CriarFilial(db, b.ID, "Recife"); err != nil {
		t.Errorf("mesmo nome em outra Empresa: %v", err)
	}
	for _, nome := range []string{"", "   ", strings.Repeat("x", 256), "a\x00b"} {
		if _, err := CriarFilial(db, a.ID, nome); !errors.Is(err, ErrFilialValidacao) {
			t.Errorf("CriarFilial(%.10q): erro = %v, want ErrFilialValidacao", nome, err)
		}
	}
	if _, err := CriarFilial(db, a.ID, strings.Repeat("é", 255)); err != nil {
		t.Errorf("255 runes deve ser aceito: %v", err)
	}

	// Listagem: só a Empresa do contexto, ordenada por nome normalizado. A
	// Filial padrão (Nome Fantasia) veio do provisionamento.
	filiais, err := ListarFiliais(db, a.ID)
	if err != nil {
		t.Fatalf("ListarFiliais: %v", err)
	}
	var nomes []string
	for _, x := range filiais {
		nomes = append(nomes, x.Nome)
	}
	if got := strings.Join(nomes, "|"); got != "Filial A|Recife|"+strings.Repeat("é", 255) {
		t.Errorf("filiais = %q", got)
	}
}

func TestProvisionarEmpresa_CriaFilialPadraoAtomicamente(t *testing.T) {
	db := testDB(t)
	removerEmpresaDeTeste(t, db, "provisionamento-filial")
	t.Cleanup(func() { removerEmpresaDeTeste(t, db, "provisionamento-filial") })

	e := criarEmpresaDeTeste(t, db, "provisionamento-filial", "778889990103", "Provisionamento Filial")
	if n := contar(t, db, `SELECT count(*) FROM filiais WHERE empresa_id = $1 AND nome = $2`, e.ID, "Provisionamento Filial"); n != 1 {
		t.Errorf("filiais com o Nome Fantasia = %d, want 1", n)
	}

	// Falha no meio desfaz tudo: rollback não deixa Filial órfã.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	e2, err := ProvisionarEmpresa(tx, DadosEmpresa{
		NomeFantasia: "Provisionamento Filial Desfeito",
		RazaoSocial:  "Desfeito LTDA",
		CNPJ:         cnpjDeTeste("778889990104"),
		Slug:         "provisionamento-filial-desfeito",
		Endereco:     enderecoDeTeste(),
	})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("ProvisionarEmpresa: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if n := contar(t, db, `SELECT count(*) FROM filiais WHERE empresa_id = $1`, e2.ID); n != 0 {
		t.Errorf("filiais após rollback = %d, want 0", n)
	}
}

func TestCriarEstoque_Filial(t *testing.T) {
	db := testDB(t)
	limparEstoques(t, db)
	removerEmpresaComDados(t, db, "estoque-filial-outra")
	t.Cleanup(func() { removerEmpresaComDados(t, db, "estoque-filial-outra") })
	outra := criarEmpresaDeTeste(t, db, "estoque-filial-outra", "778889990105", "Estoque Filial Outra")

	filialA := filialTeste(t, db, empresaTeste)
	fb, err := CriarFilial(db, empresaTeste, "Filial B Estoque 12.1")
	if err != nil {
		if !errors.Is(err, ErrNomeFilialDuplicado) {
			t.Fatalf("CriarFilial: %v", err)
		}
		if err := db.QueryRow(`SELECT id, nome FROM filiais WHERE empresa_id = $1 AND nome = 'Filial B Estoque 12.1'`, empresaTeste).Scan(&fb.ID, &fb.Nome); err != nil {
			t.Fatalf("reler filial B: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM estoques WHERE filial_id = $1`, fb.ID)
		_, _ = db.Exec(`DELETE FROM filiais WHERE id = $1`, fb.ID)
	})

	e, err := CriarEstoque(db, empresaTeste, filialA, "Central")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}
	var nomeFilialA string
	if err := db.QueryRow(`SELECT nome FROM filiais WHERE id = $1`, filialA).Scan(&nomeFilialA); err != nil {
		t.Fatalf("nome da filial A: %v", err)
	}
	if e.FilialID == nil || *e.FilialID != filialA || e.FilialNome == nil || *e.FilialNome != nomeFilialA {
		t.Errorf("estoque = %+v, want filial_id=%q e filial_nome=%q", e, filialA, nomeFilialA)
	}
	// Mesmo nome em Filial diferente: permitido; na mesma: duplicado.
	eb, err := CriarEstoque(db, empresaTeste, fb.ID, "Central")
	if err != nil {
		t.Fatalf("mesmo nome em outra Filial: %v", err)
	}
	if eb.FilialID == nil || *eb.FilialID != fb.ID || eb.FilialNome == nil || *eb.FilialNome != fb.Nome {
		t.Errorf("estoque em B = %+v, want filial_id=%q e filial_nome=%q", eb, fb.ID, fb.Nome)
	}
	if _, err := CriarEstoque(db, empresaTeste, filialA, " central "); !errors.Is(err, ErrNomeEstoqueDuplicado) {
		t.Errorf("mesma Filial: erro = %v, want ErrNomeEstoqueDuplicado", err)
	}

	// Filial ausente, malformada, inexistente ou de outra Empresa: nada gravado.
	filialAlheia := filialTeste(t, db, outra.ID)
	for nome, id := range map[string]string{
		"vazia":       "",
		"malformada":  "nao-e-uuid",
		"inexistente": "00000000-0000-4000-8000-000000000000",
		"alheia":      filialAlheia,
	} {
		if _, err := CriarEstoque(db, empresaTeste, id, "Invalido "+nome); !errors.Is(err, ErrFilialInvalida) {
			t.Errorf("filial %s: erro = %v, want ErrFilialInvalida", nome, err)
		}
	}
	if n := contar(t, db, `SELECT count(*) FROM estoques WHERE nome LIKE 'Invalido %'`); n != 0 {
		t.Errorf("estoques inválidos gravados = %d, want 0", n)
	}

	// Estoque legado (sem Filial) continua listado, com null nos dois campos.
	if _, err := db.Exec(`INSERT INTO estoques (nome, empresa_id) VALUES ('Legado', $1)`, empresaTeste); err != nil {
		t.Fatalf("seed legado: %v", err)
	}
	lista, err := ListarEstoques(db, empresaTeste)
	if err != nil {
		t.Fatalf("ListarEstoques: %v", err)
	}
	if len(lista) != 3 {
		t.Fatalf("estoques = %d, want 3", len(lista))
	}
	for _, x := range lista {
		if x.Nome == "Central" && x.FilialNome != nil {
			want := nomeFilialA
			if x.FilialID != nil && *x.FilialID == fb.ID {
				want = fb.Nome
			}
			if *x.FilialNome != want {
				t.Errorf("listagem: filial_nome = %q, want %q", *x.FilialNome, want)
			}
		}
		if x.Nome == "Legado" {
			if x.FilialID != nil || x.FilialNome != nil {
				t.Errorf("legado: %+v, want filial null", x)
			}
		} else if x.FilialID == nil || x.FilialNome == nil {
			t.Errorf("%s sem filial na listagem", x.Nome)
		}
	}
}

func TestEncontrarOuCriarEstoque_FilialPadrao(t *testing.T) {
	db := testDB(t)
	limparEstoques(t, db)
	removerEmpresaComDados(t, db, "importacao-filial")
	t.Cleanup(func() { removerEmpresaComDados(t, db, "importacao-filial") })
	emp := criarEmpresaDeTeste(t, db, "importacao-filial", "778889990106", "Importacao Filial")
	padrao := filialTeste(t, db, emp.ID)

	// Filial mais nova para forçar homônimos.
	var outraFilial string
	if err := db.QueryRow(`INSERT INTO filiais (empresa_id, nome, criado_em) VALUES ($1, 'Mais Nova', now() + interval '1 hour') RETURNING id`, emp.ID).Scan(&outraFilial); err != nil {
		t.Fatalf("filial: %v", err)
	}
	if got, err := filialPadraoDaEmpresa(db, emp.ID); err != nil || got != padrao {
		t.Fatalf("filialPadraoDaEmpresa = %q (err=%v), want a mais antiga %q", got, err, padrao)
	}

	run := func(nome string) Estoque {
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		e, err := encontrarOuCriarEstoque(tx, emp.ID, nome)
		if err != nil {
			t.Fatalf("encontrarOuCriarEstoque(%q): %v", nome, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
		return e
	}
	novo := run("Depósito Novo")
	var filialDoNovo string
	if err := db.QueryRow(`SELECT filial_id FROM estoques WHERE id = $1`, novo.ID).Scan(&filialDoNovo); err != nil || filialDoNovo != padrao {
		t.Errorf("Estoque novo na filial %q (err=%v), want a padrão %q", filialDoNovo, err, padrao)
	}
	if again := run("depósito  novo"); again.ID != novo.ID {
		t.Errorf("segunda chamada criou outro Estoque: %s vs %s", again.ID, novo.ID)
	}

	// Homônimos em Filiais distintas: prefere o da Filial padrão.
	var noutra string
	if err := db.QueryRow(`INSERT INTO estoques (nome, empresa_id, filial_id) VALUES ('Homonimo', $1, $2) RETURNING id`, emp.ID, outraFilial).Scan(&noutra); err != nil {
		t.Fatalf("seed homônimo: %v", err)
	}
	var nopadrao string
	if err := db.QueryRow(`INSERT INTO estoques (nome, empresa_id, filial_id) VALUES ('Homonimo', $1, $2) RETURNING id`, emp.ID, padrao).Scan(&nopadrao); err != nil {
		t.Fatalf("seed homônimo: %v", err)
	}
	if got := run("Homonimo"); got.ID != nopadrao {
		t.Errorf("homônimo escolhido = %s, want o da Filial padrão %s (outro = %s)", got.ID, nopadrao, noutra)
	}
}

func TestEncontrarOuCriarEstoque_EmpresaSemFilial(t *testing.T) {
	db := testDB(t)
	removerEmpresaComDados(t, db, "importacao-sem-filial")
	t.Cleanup(func() { removerEmpresaComDados(t, db, "importacao-sem-filial") })

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	// InserirEmpresa é a metade de ProvisionarEmpresa que NÃO cria Filial:
	// simula a Empresa legada (antes da Story 12.2).
	pelada, err := InserirEmpresa(tx, DadosEmpresa{
		NomeFantasia: "Sem Filial",
		RazaoSocial:  "Sem Filial LTDA",
		CNPJ:         cnpjDeTeste("778889990107"),
		Slug:         "importacao-sem-filial",
		Endereco:     enderecoDeTeste(),
	})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("InserirEmpresa: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	tx2, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx2.Rollback() }()
	if _, err := encontrarOuCriarEstoque(tx2, pelada.ID, "Qualquer"); !errors.Is(err, ErrEmpresaSemFilial) {
		t.Errorf("erro = %v, want ErrEmpresaSemFilial", err)
	}
	if n := contar(t, db, `SELECT count(*) FROM estoques WHERE empresa_id = $1`, pelada.ID); n != 0 {
		t.Errorf("estoques órfãos = %d, want 0", n)
	}
}

// empresaSemFilialDeTeste cria uma Empresa completa (listas padrão) e remove a
// Filial padrão, simulando a Empresa legada anterior à Story 12.2. A limpeza
// também apaga importações, que o helper genérico não cobre.
func empresaSemFilialDeTeste(t *testing.T, db *sql.DB, slug, cnpjBase12, nome string) Empresa {
	t.Helper()
	limpar := func() {
		var id string
		if err := db.QueryRow(`SELECT id FROM empresas WHERE slug = $1`, slug).Scan(&id); err == nil {
			_, _ = db.Exec(`DELETE FROM importacao_linhas WHERE importacao_id IN (SELECT id FROM importacoes WHERE empresa_id = $1)`, id)
			_, _ = db.Exec(`DELETE FROM importacoes WHERE empresa_id = $1`, id)
		}
		removerEmpresaComDados(t, db, slug)
	}
	limpar()
	t.Cleanup(limpar)
	e := criarEmpresaDeTeste(t, db, slug, cnpjBase12, nome)
	if _, err := db.Exec(`DELETE FROM filiais WHERE empresa_id = $1`, e.ID); err != nil {
		t.Fatalf("remover filial padrão: %v", err)
	}
	return e
}

// Estoque legado (filial NULL) é encontrado por nome sem exigir Filial e sem
// criar nada — com e sem Filial na Empresa.
func TestEncontrarOuCriarEstoque_LegadoSemFilialEncontradoPorNome(t *testing.T) {
	db := testDB(t)
	emp := empresaSemFilialDeTeste(t, db, "importacao-legado-nome", "778889990108", "Importacao Legado Nome")

	var legadoID string
	if err := db.QueryRow(`INSERT INTO estoques (nome, empresa_id) VALUES ('Legado X', $1) RETURNING id`, emp.ID).Scan(&legadoID); err != nil {
		t.Fatalf("seed legado: %v", err)
	}

	buscar := func() {
		t.Helper()
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		e, err := encontrarOuCriarEstoque(tx, emp.ID, "  legado   x ")
		if err != nil {
			t.Fatalf("encontrarOuCriarEstoque: %v", err)
		}
		if e.ID != legadoID {
			t.Errorf("id = %s, want o do Estoque legado %s", e.ID, legadoID)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
		if n := contar(t, db, `SELECT count(*) FROM estoques WHERE empresa_id = $1`, emp.ID); n != 1 {
			t.Errorf("estoques da Empresa = %d, want 1 (nada criado)", n)
		}
	}

	buscar() // sem Filial na Empresa

	if _, err := CriarFilial(db, emp.ID, "Filial Tardia"); err != nil {
		t.Fatalf("CriarFilial: %v", err)
	}
	buscar() // com Filial na Empresa
}
