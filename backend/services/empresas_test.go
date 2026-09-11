package services

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// Testes da fundação de Empresa — Story 9.1 (Epic 9, Multi-Empresa),
// spec-9-1: validação de CNPJ e slug (puras), resolução por slug (a única
// tradução slug -> Empresa do sistema, AD-19) e o provisionamento, que copia
// as listas padrão de Categoria e Nomenclatura para a Empresa nova.
//
// As Empresas criadas aqui têm slug e CNPJ próprios: `empresas` nunca é
// truncada entre testes (ver empresa_teste_test.go), então cada caso precisa
// de identificadores que não colidam com os das outras suítes.

func TestNormalizarCNPJ_RemoveMascara(t *testing.T) {
	casos := map[string]string{
		"11.222.333/0001-81":   "11222333000181",
		"11222333000181":       "11222333000181",
		" 11 222 333/0001 81 ": "11222333000181",
		"abc":                  "",
	}
	for entrada, want := range casos {
		if got := NormalizarCNPJ(entrada); got != want {
			t.Errorf("NormalizarCNPJ(%q) = %q, want %q", entrada, got, want)
		}
	}
}

// TestValidarCNPJ prova que os dois dígitos verificadores são realmente
// calculados: um CNPJ com o último dígito trocado é recusado, e a máscara não
// muda o veredito.
func TestValidarCNPJ(t *testing.T) {
	valido := cnpjDeTeste("112223330001")

	casos := []struct {
		nome    string
		cnpj    string
		querErr bool
	}{
		{"válido sem máscara", valido, false},
		{"válido com máscara", "11.222.333/0001-" + valido[12:], false},
		{"dígito verificador errado", valido[:13] + string(rune('0'+(int(valido[13]-'0')+1)%10)), true},
		{"curto demais", valido[:13], true},
		{"longo demais", valido + "0", true},
		{"todos os dígitos iguais", "11111111111111", true},
		{"vazio", "", true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			err := ValidarCNPJ(c.cnpj)
			if c.querErr {
				var valErr *ErroEmpresaValidacao
				if !errors.As(err, &valErr) {
					t.Fatalf("ValidarCNPJ(%q) = %v, want *ErroEmpresaValidacao", c.cnpj, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidarCNPJ(%q) erro inesperado: %v", c.cnpj, err)
			}
		})
	}
}

func TestNormalizarSlug(t *testing.T) {
	casos := map[string]string{
		"Ferreira Costa":   "ferreira-costa",
		"  ACME  ":         "acme",
		"Construção Élite": "construcao-elite",
		"a--b__c":          "a-b-c",
		"-Canteiro 7-":     "canteiro-7",
	}
	for entrada, want := range casos {
		got, err := NormalizarSlug(entrada)
		if err != nil {
			t.Fatalf("NormalizarSlug(%q) erro inesperado: %v", entrada, err)
		}
		if got != want {
			t.Errorf("NormalizarSlug(%q) = %q, want %q", entrada, got, want)
		}
	}

	invalidos := []string{"", "   ", "a", "!!", strings.Repeat("x", 64)}
	for _, entrada := range invalidos {
		if _, err := NormalizarSlug(entrada); err == nil {
			t.Errorf("NormalizarSlug(%q) = nil, want *ErroEmpresaValidacao", entrada)
		}
	}
}

// TestBuscarEmpresaPorSlug_Achada prova que a projeção devolvida ao
// middleware carrega os campos que ele injeta no contexto.
func TestBuscarEmpresaPorSlug_Achada(t *testing.T) {
	db := testDB(t)
	removerEmpresaDeTeste(t, db, "busca-slug-achada")
	criada := criarEmpresaDeTeste(t, db, "busca-slug-achada", "556667770001", "Busca Achada")

	e, err := BuscarEmpresaPorSlug(db, "busca-slug-achada")
	if err != nil {
		t.Fatalf("BuscarEmpresaPorSlug: %v", err)
	}
	if e.ID != criada.ID {
		t.Errorf("id = %q, want %q", e.ID, criada.ID)
	}
	if e.NomeFantasia != "Busca Achada" || e.Slug != "busca-slug-achada" || e.Status != "ativa" {
		t.Errorf("projeção = %+v, want nome/slug/status da Empresa criada", e)
	}
	if e.EmpresaOrigemID != nil {
		t.Errorf("empresaOrigemId = %v, want nil (Empresa raiz)", *e.EmpresaOrigemID)
	}
}

// TestBuscarEmpresaPorSlug_ColapsaTodasAsFalhas prova o Always da spec: slug
// inexistente, fora da forma canônica e de Empresa `inativa` devolvem
// EXATAMENTE o mesmo erro — é o que o middleware traduz em 404 NOT_FOUND,
// nunca revelando que um slug existe mas está desligado.
func TestBuscarEmpresaPorSlug_ColapsaTodasAsFalhas(t *testing.T) {
	db := testDB(t)
	removerEmpresaDeTeste(t, db, "busca-slug-inativa")
	inativa := criarEmpresaDeTeste(t, db, "busca-slug-inativa", "667778880001", "Busca Inativa")
	if _, err := db.Exec(`UPDATE empresas SET status = 'inativa' WHERE id = $1`, inativa.ID); err != nil {
		t.Fatalf("desativar empresa: %v", err)
	}

	casos := map[string]string{
		"inexistente":      "nao-existe-slug-nenhum",
		"inativa":          "busca-slug-inativa",
		"com maiúsculas":   "Busca-Slug-Inativa",
		"com espaço":       "busca slug inativa",
		"vazio":            "",
		"hífen nas pontas": "-busca-slug-inativa-",
	}
	for nome, slug := range casos {
		t.Run(nome, func(t *testing.T) {
			_, err := BuscarEmpresaPorSlug(db, slug)
			if !errors.Is(err, ErrEmpresaNaoEncontrada) {
				t.Fatalf("BuscarEmpresaPorSlug(%q) = %v, want ErrEmpresaNaoEncontrada", slug, err)
			}
		})
	}
}

// TestProvisionarEmpresa_CopiaListasPadrao prova o contrato que as Stories
// 9.2 e 9.4 reutilizam: a Empresa nova nasce com a MESMA lista de Categorias
// e de templates de Nomenclatura semeada pelas migrações 000010/000013 — sem
// isso, um cliente novo abriria o Cadastro de Produto sem categoria alguma.
func TestProvisionarEmpresa_CopiaListasPadrao(t *testing.T) {
	db := testDB(t)

	var categoriasPadrao, templatesPadrao int
	if err := db.QueryRow(`SELECT count(*) FROM categorias_padrao`).Scan(&categoriasPadrao); err != nil {
		t.Fatalf("contar categorias padrão: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM nomenclatura_templates_padrao`).Scan(&templatesPadrao); err != nil {
		t.Fatalf("contar templates padrão: %v", err)
	}
	if categoriasPadrao == 0 || templatesPadrao == 0 {
		t.Fatalf("pré-condição: seed padrão vazio (categorias=%d templates=%d)", categoriasPadrao, templatesPadrao)
	}

	removerEmpresaDeTeste(t, db, "provisionamento-listas")
	e := criarEmpresaDeTeste(t, db, "provisionamento-listas", "778889990001", "Provisionamento Listas")

	var categorias, templates int
	if err := db.QueryRow(`SELECT count(*) FROM categorias WHERE empresa_id = $1`, e.ID).Scan(&categorias); err != nil {
		t.Fatalf("contar categorias da empresa: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM nomenclatura_templates WHERE empresa_id = $1`, e.ID).Scan(&templates); err != nil {
		t.Fatalf("contar templates da empresa: %v", err)
	}
	if categorias != categoriasPadrao {
		t.Errorf("categorias copiadas = %d, want %d", categorias, categoriasPadrao)
	}
	if templates != templatesPadrao {
		t.Errorf("templates copiados = %d, want %d", templates, templatesPadrao)
	}
}

// TestProvisionarEmpresa_NormalizaEValida prova que os dados chegam
// normalizados ao banco (CNPJ sem máscara, slug canônico) e que uma validação
// que falha não escreve nada.
func TestProvisionarEmpresa_NormalizaEValida(t *testing.T) {
	db := testDB(t)

	removerEmpresaDeTeste(t, db, "normaliza-dados")
	cnpj := cnpjDeTeste("889990001001")
	e := provisionar(t, db, DadosEmpresa{
		NomeFantasia: "Normaliza Dados",
		RazaoSocial:  "Normaliza Dados LTDA",
		CNPJ:         cnpj[:2] + "." + cnpj[2:5] + "." + cnpj[5:8] + "/" + cnpj[8:12] + "-" + cnpj[12:],
		Slug:         "  Normaliza   Dados  ",
		Endereco:     enderecoDeTeste(),
	})
	if e.CNPJ != cnpj {
		t.Errorf("cnpj gravado = %q, want %q (sem máscara)", e.CNPJ, cnpj)
	}
	if e.Slug != "normaliza-dados" {
		t.Errorf("slug gravado = %q, want %q", e.Slug, "normaliza-dados")
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := ProvisionarEmpresa(tx, DadosEmpresa{
		NomeFantasia: "",
		RazaoSocial:  "Sem Nome LTDA",
		CNPJ:         cnpjDeTeste("990001112001"),
		Slug:         "sem-nome-fantasia",
		Endereco:     enderecoDeTeste(),
	}); err == nil {
		t.Fatal("ProvisionarEmpresa sem nome fantasia = nil, want *ErroEmpresaValidacao")
	}
}

// TestProvisionarEmpresa_CNPJESlugUnicosNaPlataforma prova que a unicidade de
// CNPJ e de slug vale para a plataforma inteira (não por Empresa): dois
// clientes nunca compartilham o mesmo CNPJ nem o mesmo endereço de acesso.
func TestProvisionarEmpresa_CNPJESlugUnicosNaPlataforma(t *testing.T) {
	db := testDB(t)

	removerEmpresaDeTeste(t, db, "primeira-dona")
	removerEmpresaDeTeste(t, db, "segunda-dona")
	cnpj := cnpjDeTeste("111222334001")
	provisionar(t, db, DadosEmpresa{
		NomeFantasia: "Primeira Dona",
		RazaoSocial:  "Primeira Dona LTDA",
		CNPJ:         cnpj,
		Slug:         "primeira-dona",
		Endereco:     enderecoDeTeste(),
	})

	casos := []struct {
		nome    string
		dados   DadosEmpresa
		wantErr error
	}{
		{"mesmo CNPJ", DadosEmpresa{
			NomeFantasia: "Segunda", RazaoSocial: "Segunda LTDA",
			CNPJ: cnpj, Slug: "segunda-dona", Endereco: enderecoDeTeste(),
		}, ErrCNPJDuplicado},
		{"mesmo slug", DadosEmpresa{
			NomeFantasia: "Terceira", RazaoSocial: "Terceira LTDA",
			CNPJ: cnpjDeTeste("222333445001"), Slug: "primeira-dona", Endereco: enderecoDeTeste(),
		}, ErrSlugDuplicado},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			tx, err := db.Begin()
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer func() { _ = tx.Rollback() }()
			if _, err := ProvisionarEmpresa(tx, c.dados); !errors.Is(err, c.wantErr) {
				t.Fatalf("ProvisionarEmpresa = %v, want %v", err, c.wantErr)
			}
		})
	}
}

// enderecoDeTeste é o endereço cadastral usado pelos casos que não estão
// exercitando o endereço em si.
func enderecoDeTeste() EnderecoEmpresa {
	return EnderecoEmpresa{
		Logradouro: "Rua de Teste",
		Numero:     "100",
		Bairro:     "Centro",
		Cidade:     "Recife",
		CEP:        "50000000",
		UF:         "PE",
	}
}

// provisionar roda ProvisionarEmpresa em uma transação própria e comita —
// atalho dos casos que precisam da Empresa gravada, não do erro.
func provisionar(t *testing.T, db *sql.DB, dados DadosEmpresa) Empresa {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	e, err := ProvisionarEmpresa(tx, dados)
	if err != nil {
		t.Fatalf("ProvisionarEmpresa(%q): %v", dados.Slug, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return e
}

// TestInserirEmpresa_NaoCopiaListas prova a metade "cadastral" extraída na
// Story 9.4: a Empresa fundadora nasce SEM nenhuma Categoria/Template,
// porque ela adota as linhas legadas no backfill — receber a cópia antes
// duplicaria cada código dentro dela.
func TestInserirEmpresa_NaoCopiaListas(t *testing.T) {
	db := testDB(t)
	const slug = "empresa-94-insere"
	t.Cleanup(func() { removerEmpresaDeTeste(t, db, slug) })

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	e, err := InserirEmpresa(tx, DadosEmpresa{
		NomeFantasia: "Empresa 94 Insere",
		RazaoSocial:  "Empresa 94 Insere LTDA",
		CNPJ:         cnpjDeTeste("947300010001"),
		Slug:         slug,
		Endereco: EnderecoEmpresa{
			Logradouro: "Rua de Teste", Numero: "100", Bairro: "Centro",
			Cidade: "Recife", CEP: "50000000", UF: "PE",
		},
	})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("InserirEmpresa: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var categorias, templates int
	if err := db.QueryRow(`SELECT count(*) FROM categorias WHERE empresa_id = $1`, e.ID).Scan(&categorias); err != nil {
		t.Fatalf("contar categorias: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM nomenclatura_templates WHERE empresa_id = $1`, e.ID).Scan(&templates); err != nil {
		t.Fatalf("contar templates: %v", err)
	}
	if categorias != 0 || templates != 0 {
		t.Errorf("InserirEmpresa copiou listas: categorias=%d templates=%d, want 0 e 0", categorias, templates)
	}
}

// TestCopiarListasPadrao_Idempotente prova o `WHERE NOT EXISTS` que permite
// ao backfill da Story 9.4 COMPLETAR a lista de uma Empresa que já adotou
// parte dela: a segunda chamada não insere nada.
func TestCopiarListasPadrao_Idempotente(t *testing.T) {
	db := testDB(t)
	const slug = "empresa-94-copia"
	t.Cleanup(func() { removerEmpresaDeTeste(t, db, slug) })

	e := criarEmpresaDeTeste(t, db, slug, "947300020001", "Empresa 94 Copia")

	var depoisDaPrimeira int
	if err := db.QueryRow(`SELECT count(*) FROM categorias WHERE empresa_id = $1`, e.ID).Scan(&depoisDaPrimeira); err != nil {
		t.Fatalf("contar categorias: %v", err)
	}
	if depoisDaPrimeira == 0 {
		t.Fatal("pré-condição: ProvisionarEmpresa não copiou nenhuma categoria")
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := CopiarListasPadrao(tx, e.ID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("CopiarListasPadrao (2a vez): %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var depoisDaSegunda int
	if err := db.QueryRow(`SELECT count(*) FROM categorias WHERE empresa_id = $1`, e.ID).Scan(&depoisDaSegunda); err != nil {
		t.Fatalf("contar categorias: %v", err)
	}
	if depoisDaSegunda != depoisDaPrimeira {
		t.Errorf("categorias depois da 2a cópia = %d, want %d (idempotente)", depoisDaSegunda, depoisDaPrimeira)
	}
}
