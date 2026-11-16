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

	f, err := CriarFilial(db, a.ID, DadosFilial{Nome: "  Recife  "})
	if err != nil {
		t.Fatalf("CriarFilial: %v", err)
	}
	if f.Nome != "Recife" || f.ID == "" {
		t.Errorf("filial = %+v", f)
	}
	if _, err := CriarFilial(db, a.ID, DadosFilial{Nome: "recife"}); !errors.Is(err, ErrNomeFilialDuplicado) {
		t.Errorf("duplicada: erro = %v, want ErrNomeFilialDuplicado", err)
	}
	// Mesmo nome em OUTRA Empresa é permitido.
	if _, err := CriarFilial(db, b.ID, DadosFilial{Nome: "Recife"}); err != nil {
		t.Errorf("mesmo nome em outra Empresa: %v", err)
	}
	for _, nome := range []string{"", "   ", strings.Repeat("x", 256), "a\x00b"} {
		if _, err := CriarFilial(db, a.ID, DadosFilial{Nome: nome}); !errors.Is(err, ErrFilialValidacao) {
			t.Errorf("CriarFilial(%.10q): erro = %v, want ErrFilialValidacao", nome, err)
		}
	}
	if _, err := CriarFilial(db, a.ID, DadosFilial{Nome: strings.Repeat("é", 255)}); err != nil {
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
	fb, err := CriarFilial(db, empresaTeste, DadosFilial{Nome: "Filial B Estoque 12.1"})
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

	if _, err := CriarFilial(db, emp.ID, DadosFilial{Nome: "Filial Tardia"}); err != nil {
		t.Fatalf("CriarFilial: %v", err)
	}
	buscar() // com Filial na Empresa
}

// --- Dados fiscais da Filial — spec-filial-dados-fiscais --------------------

func enderecoFilialTeste() EnderecoEmpresa {
	return EnderecoEmpresa{
		Logradouro: " Av. Recife ", Numero: "1000", Complemento: "Galpão 3",
		Bairro: "Areias", Cidade: "Recife", CEP: " 50780-000 ", UF: " pe ",
	}
}

func TestFilialDadosFiscais_CriarEditarEValidar(t *testing.T) {
	db := testDB(t)
	removerEmpresaDeTeste(t, db, "filial-fiscal-a")
	removerEmpresaDeTeste(t, db, "filial-fiscal-b")
	t.Cleanup(func() {
		removerEmpresaDeTeste(t, db, "filial-fiscal-a")
		removerEmpresaDeTeste(t, db, "filial-fiscal-b")
	})
	a := criarEmpresaDeTeste(t, db, "filial-fiscal-a", "778889990201", "Filial Fiscal A")
	b := criarEmpresaDeTeste(t, db, "filial-fiscal-b", "778889990202", "Filial Fiscal B")
	cnpj := cnpjDeTeste("778889990002") // mesma raiz (77888999) das Empresas a e b
	cnpjMascarado := cnpj[0:2] + "." + cnpj[2:5] + "." + cnpj[5:8] + "/" + cnpj[8:12] + "-" + cnpj[12:]

	// Criar só com nome: campos NULL.
	f, err := CriarFilial(db, a.ID, DadosFilial{Nome: "Só Nome"})
	if err != nil {
		t.Fatalf("CriarFilial só nome: %v", err)
	}
	if f.CNPJ != nil || f.InscricaoEstadual != nil || f.Endereco != nil {
		t.Errorf("filial só nome = %+v, want dados fiscais nil", f)
	}
	if n := contar(t, db, `SELECT count(*) FROM filiais WHERE id = $1 AND cnpj IS NULL AND inscricao_estadual IS NULL
		AND logradouro IS NULL AND numero IS NULL AND complemento IS NULL AND bairro IS NULL AND cidade IS NULL
		AND cep IS NULL AND uf IS NULL`, f.ID); n != 1 {
		t.Errorf("colunas fiscais não ficaram NULL")
	}

	// Editar com tudo válido: normalizado (CNPJ/CEP só dígitos, UF maiúscula).
	e, err := AtualizarFilial(db, a.ID, f.ID, AtualizacaoCompletaFilial(DadosFilial{
		Nome: " Só Nome Editada ", CNPJ: cnpjMascarado, InscricaoEstadual: " ISENTO ", Endereco: enderecoFilialTeste(),
	}))
	if err != nil {
		t.Fatalf("AtualizarFilial: %v", err)
	}
	want := EnderecoEmpresa{Logradouro: "Av. Recife", Numero: "1000", Complemento: "Galpão 3",
		Bairro: "Areias", Cidade: "Recife", CEP: "50780000", UF: "PE"}
	if e.Nome != "Só Nome Editada" || e.CNPJ == nil || *e.CNPJ != cnpj || e.InscricaoEstadual == nil ||
		*e.InscricaoEstadual != "ISENTO" || e.Endereco == nil || *e.Endereco != want {
		t.Errorf("editada = %+v / endereco %+v", e, e.Endereco)
	}
	lista, err := ListarFiliais(db, a.ID)
	if err != nil {
		t.Fatalf("ListarFiliais: %v", err)
	}
	achou := false
	for _, x := range lista {
		if x.ID == f.ID {
			achou = x.CNPJ != nil && *x.CNPJ == cnpj && x.Endereco != nil && *x.Endereco == want
		}
	}
	if !achou {
		t.Errorf("listagem não traz os dados fiscais gravados: %+v", lista)
	}

	// Editar de volta sem nada limpa tudo (substituição completa).
	if limpa, err := AtualizarFilial(db, a.ID, f.ID, AtualizacaoCompletaFilial(DadosFilial{Nome: "Só Nome Editada"})); err != nil ||
		limpa.CNPJ != nil || limpa.Endereco != nil || limpa.InscricaoEstadual != nil {
		t.Errorf("limpar: %+v err=%v", limpa, err)
	}
	if _, err := AtualizarFilial(db, a.ID, f.ID, AtualizacaoCompletaFilial(DadosFilial{Nome: "Só Nome Editada", CNPJ: cnpj, Endereco: enderecoFilialTeste()})); err != nil {
		t.Fatalf("regravar: %v", err)
	}

	semAlteracao := func(t *testing.T) {
		t.Helper()
		if n := contar(t, db, `SELECT count(*) FROM filiais WHERE id = $1 AND nome = 'Só Nome Editada' AND cnpj = $2 AND cidade = 'Recife'`, f.ID, cnpj); n != 1 {
			t.Errorf("a Filial foi alterada por uma edição recusada")
		}
	}

	// CNPJ inválido (dígito verificador) -> ErroDadosFilial, nada gravado.
	cnpjErrado := cnpj[:13] + string('0'+(cnpj[13]-'0'+1)%10)
	var erroDados *ErroDadosFilial
	if _, err := AtualizarFilial(db, a.ID, f.ID, AtualizacaoCompletaFilial(DadosFilial{Nome: "Só Nome Editada", CNPJ: cnpjErrado})); !errors.As(err, &erroDados) {
		t.Errorf("CNPJ inválido: erro = %v, want ErroDadosFilial", err)
	}
	if _, err := CriarFilial(db, a.ID, DadosFilial{Nome: "CNPJ Ruim", CNPJ: cnpjErrado}); !errors.As(err, &erroDados) {
		t.Errorf("CNPJ inválido na criação: erro = %v", err)
	}
	semAlteracao(t)

	// CNPJ repetido na mesma Empresa -> 409; em outra Empresa é permitido.
	if _, err := CriarFilial(db, a.ID, DadosFilial{Nome: "Outra", CNPJ: cnpj}); !errors.Is(err, ErrCNPJFilialDuplicado) {
		t.Errorf("CNPJ repetido (criar): erro = %v, want ErrCNPJFilialDuplicado", err)
	}
	outra, err := CriarFilial(db, a.ID, DadosFilial{Nome: "Outra"})
	if err != nil {
		t.Fatalf("criar outra: %v", err)
	}
	if _, err := AtualizarFilial(db, a.ID, outra.ID, AtualizacaoCompletaFilial(DadosFilial{Nome: "Outra", CNPJ: cnpj})); !errors.Is(err, ErrCNPJFilialDuplicado) {
		t.Errorf("CNPJ repetido (editar): erro = %v, want ErrCNPJFilialDuplicado", err)
	}
	if _, err := AtualizarFilial(db, a.ID, outra.ID, AtualizacaoCompletaFilial(DadosFilial{Nome: "só nome editada"})); !errors.Is(err, ErrNomeFilialDuplicado) {
		t.Errorf("nome repetido (editar): erro = %v, want ErrNomeFilialDuplicado", err)
	}
	if _, err := CriarFilial(db, b.ID, DadosFilial{Nome: "Mesma CNPJ outra Empresa", CNPJ: cnpj}); err != nil {
		t.Errorf("mesmo CNPJ em outra Empresa: %v", err)
	}

	// Endereço incompleto: só cidade -> lista os obrigatórios.
	_, err = AtualizarFilial(db, a.ID, f.ID, AtualizacaoCompletaFilial(DadosFilial{Nome: "Só Nome Editada", CNPJ: cnpj,
		Endereco: EnderecoEmpresa{Cidade: "Recife"}}))
	if !errors.As(err, &erroDados) {
		t.Fatalf("endereço incompleto: erro = %v, want ErroDadosFilial", err)
	}
	for _, campo := range []string{"logradouro", "número", "bairro", "CEP", "UF"} {
		if !strings.Contains(erroDados.Mensagem, campo) {
			t.Errorf("mensagem %q não cita %q", erroDados.Mensagem, campo)
		}
	}
	casosEndereco := map[string]func(*EnderecoEmpresa){
		"CEP com 7 dígitos": func(e *EnderecoEmpresa) { e.CEP = "5078000" },
		"UF inexistente":    func(e *EnderecoEmpresa) { e.UF = "XX" },
		"UF com 3 letras":   func(e *EnderecoEmpresa) { e.UF = "PEE" },
		"número > 20":       func(e *EnderecoEmpresa) { e.Numero = strings.Repeat("1", 21) },
	}
	for nome, mut := range casosEndereco {
		end := enderecoFilialTeste()
		mut(&end)
		if _, err := AtualizarFilial(db, a.ID, f.ID, AtualizacaoCompletaFilial(DadosFilial{Nome: "Só Nome Editada", CNPJ: cnpj, Endereco: end})); !errors.As(err, &erroDados) {
			t.Errorf("%s: erro = %v, want ErroDadosFilial", nome, err)
		}
	}
	// Complemento é opcional.
	semComp := enderecoFilialTeste()
	semComp.Complemento = ""
	if _, err := CriarFilial(db, a.ID, DadosFilial{Nome: "Sem Complemento", Endereco: semComp}); err != nil {
		t.Errorf("sem complemento: %v", err)
	}
	if _, err := AtualizarFilial(db, a.ID, f.ID, AtualizacaoCompletaFilial(DadosFilial{Nome: "Só Nome Editada", CNPJ: cnpj,
		InscricaoEstadual: strings.Repeat("1", 21)})); !errors.As(err, &erroDados) {
		t.Errorf("IE > 20: erro = %v, want ErroDadosFilial", err)
	}
	semAlteracao(t)

	// CNPJ válido de OUTRA raiz (outra empresa) -> 400, nada gravado.
	cnpjOutraRaiz := cnpjDeTeste("112223330001")
	if _, err := AtualizarFilial(db, a.ID, f.ID, AtualizacaoFilial{CNPJ: &cnpjOutraRaiz}); !errors.As(err, &erroDados) ||
		!strings.Contains(erroDados.Mensagem, "8 primeiros dígitos") {
		t.Errorf("CNPJ de outra raiz (editar): erro = %v, want ErroDadosFilial sobre a raiz", err)
	}
	if _, err := CriarFilial(db, a.ID, DadosFilial{Nome: "Outra Raiz", CNPJ: cnpjOutraRaiz}); !errors.As(err, &erroDados) {
		t.Errorf("CNPJ de outra raiz (criar): erro = %v, want ErroDadosFilial", err)
	}

	// CEP com letras/lixo -> 400 (nunca descarta caracteres em silêncio);
	// caracteres de controle em nome/IE/endereço -> 400.
	for nome, mut := range map[string]func(*DadosFilial){
		"CEP com letras":     func(d *DadosFilial) { d.Endereco.CEP = "50780-00a" },
		"CEP com lixo":       func(d *DadosFilial) { d.Endereco.CEP = "CEP 50780000" },
		"CEP com 9 dígitos":  func(d *DadosFilial) { d.Endereco.CEP = "507800001" },
		"nome com quebra":    func(d *DadosFilial) { d.Nome = "Só Nome\nEditada" },
		"IE com tab":         func(d *DadosFilial) { d.InscricaoEstadual = "123\t456" },
		"logradouro com \\r": func(d *DadosFilial) { d.Endereco.Logradouro = "Av.\rRecife" },
		"cidade com \\x07":   func(d *DadosFilial) { d.Endereco.Cidade = "Rec\x07ife" },
	} {
		d := DadosFilial{Nome: "Só Nome Editada", CNPJ: cnpj, Endereco: enderecoFilialTeste()}
		mut(&d)
		if _, err := AtualizarFilial(db, a.ID, f.ID, AtualizacaoCompletaFilial(d)); !errors.As(err, &erroDados) {
			t.Errorf("%s: erro = %v, want ErroDadosFilial", nome, err)
		}
	}
	if cep, err := normalizarCEP(" 50780000 "); err != nil || cep != "50780000" {
		t.Errorf("normalizarCEP sem hífen = %q, %v", cep, err)
	}
	semAlteracao(t)

	// PUT parcial: campo ausente mantém; "" limpa.
	ie := "123456"
	if p, err := AtualizarFilial(db, a.ID, f.ID, AtualizacaoFilial{InscricaoEstadual: &ie}); err != nil ||
		p.InscricaoEstadual == nil || *p.InscricaoEstadual != ie || p.CNPJ == nil || *p.CNPJ != cnpj ||
		p.Endereco == nil || p.Endereco.Cidade != "Recife" || p.Nome != "Só Nome Editada" {
		t.Errorf("parcial só IE: %+v err=%v (CNPJ/endereço/nome deveriam ficar)", p, err)
	}
	vazio := "Natal"
	if p, err := AtualizarFilial(db, a.ID, f.ID, AtualizacaoFilial{Endereco: &EnderecoFilialParcial{Cidade: &vazio}}); err != nil ||
		p.Endereco == nil || p.Endereco.Cidade != "Natal" || p.Endereco.Logradouro != "Av. Recife" {
		t.Errorf("parcial só cidade: %+v err=%v", p.Endereco, err)
	}
	limpa := ""
	if p, err := AtualizarFilial(db, a.ID, f.ID, AtualizacaoFilial{InscricaoEstadual: &limpa}); err != nil ||
		p.InscricaoEstadual != nil || p.CNPJ == nil {
		t.Errorf("IE \"\" deveria limpar só a IE: %+v err=%v", p, err)
	}
	// Limpar só um campo obrigatório do endereço -> endereço incompleto.
	if _, err := AtualizarFilial(db, a.ID, f.ID, AtualizacaoFilial{Endereco: &EnderecoFilialParcial{Bairro: &limpa}}); !errors.As(err, &erroDados) {
		t.Errorf("limpar só o bairro: erro = %v, want endereço incompleto", err)
	}
	cidadeRecife := "Recife"
	if _, err := AtualizarFilial(db, a.ID, f.ID, AtualizacaoFilial{Endereco: &EnderecoFilialParcial{Cidade: &cidadeRecife}}); err != nil {
		t.Fatalf("restaurar cidade: %v", err)
	}
	semAlteracao(t)

	// Editar Filial de outra Empresa / inexistente / malformada -> não encontrada.
	for _, id := range []string{f.ID, "00000000-0000-4000-8000-000000000000", "nao-e-uuid"} {
		if _, err := AtualizarFilial(db, b.ID, id, AtualizacaoCompletaFilial(DadosFilial{Nome: "Invasora"})); !errors.Is(err, ErrFilialNaoEncontrada) {
			t.Errorf("AtualizarFilial(outra Empresa, %s): erro = %v, want ErrFilialNaoEncontrada", id, err)
		}
	}
	semAlteracao(t)
}
