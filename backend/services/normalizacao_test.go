package services

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

// seedProdutoNormalizacao cadastra um Produto com o `nome` e as dimensões
// estruturadas dadas (`nil` fica NULL nas duas colunas do campo) — sem
// quantidade inicial (irrelevante para a análise). Devolve o id.
func seedProdutoNormalizacao(t *testing.T, db *sql.DB, nome string, dims CriarProdutoInput) string {
	t.Helper()
	estoque, err := CriarEstoque(db, "Estoque "+nome)
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	dims.Nome = nome
	dims.CategoriaID = categoriaIDPorCodigo(t, db, "04.001")
	dims.EstoqueID = estoque.ID
	produto, err := CriarProduto(db, dims)
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}
	return produto.ID
}

// setDimensoesPendentesRevisao grava `produtos.dimensoes_pendentes_revisao`
// diretamente via SQL — nenhum service de escrita cobre essa coluna (só a
// migração legada, Story 3.7, a preenche em produção); os testes desta story
// simulam o estado que a migração deixaria.
func setDimensoesPendentesRevisao(t *testing.T, db *sql.DB, produtoID, jsonBruto string) {
	t.Helper()
	if _, err := db.Exec(
		`UPDATE produtos SET dimensoes_pendentes_revisao = $1::jsonb WHERE id = $2`,
		jsonBruto, produtoID,
	); err != nil {
		t.Fatalf("falha ao gravar dimensoes_pendentes_revisao: %v", err)
	}
}

func sugestaoDe(sugestoes []Sugestao, produtoID, campo string) (Sugestao, bool) {
	for _, s := range sugestoes {
		if s.ProdutoID == produtoID && s.Campo == campo {
			return s, true
		}
	}
	return Sugestao{}, false
}

// --- parseDimensaoTexto (parser tolerante, origem "migracao") -------------

func TestParseDimensaoTexto(t *testing.T) {
	casos := []struct {
		texto       string
		querValor   float64
		querUnidade string
		querOk      bool
	}{
		{"cerca de 3 metros", 3, "m", true},
		{"6 m", 6, "m", true},
		{"100mm", 100, "mm", true},
		{"10,5 centimetros", 10.5, "cm", true},
		{"aprox. 2 centímetros", 2, "cm", true},
		{"5 milimetros", 5, "mm", true},
		{"ver etiqueta", 0, "", false},
		{"verificar depois", 0, "", false},
		{"", 0, "", false},
		// `\b` no início do grupo numérico: "123mm" no MEIO de "LOTE123mm"
		// não é um token número+unidade válido (não há fronteira de palavra
		// entre "E" e "1") — nenhum valor é inventado a partir de um código
		// de lote que por acaso termina em algo que parece unidade.
		{"LOTE123mm", 0, "", false},
	}
	for _, c := range casos {
		t.Run(c.texto, func(t *testing.T) {
			valor, unidade, ok := parseDimensaoTexto(c.texto)
			if ok != c.querOk {
				t.Fatalf("ok = %v, want %v", ok, c.querOk)
			}
			if !ok {
				return
			}
			if valor != c.querValor || unidade != c.querUnidade {
				t.Errorf("= (%v,%q), want (%v,%q)", valor, unidade, c.querValor, c.querUnidade)
			}
		})
	}
}

// --- extrairValorDoNome (origem "nome") ------------------------------------

func TestExtrairValorDoNome(t *testing.T) {
	casos := []struct {
		nome           string
		jaEstruturados []valorUnidade
		querValor      float64
		querUnidade    string
		querOk         bool
	}{
		{"TUBO PVC 6M", nil, 6, "m", true},
		{"CABO FLEXIVEL 4MM", nil, 4, "mm", true},
		{"CHAPA 10 CM", nil, 10, "cm", true},
		{"TUBO PVC 6M DN25", nil, 6, "m", true}, // extrai o primeiro token; a ambiguidade é decidida pelo chamador
		{"PARAFUSO SEXTAVADO", nil, 0, "", false},
		// jaEstruturados filtra o primeiro token quando ele já pertence a um
		// campo preenchido — o segundo token, ainda livre, é o candidato.
		{"TUBO 25MM 6M", []valorUnidade{{valor: 25, unidade: "mm"}}, 6, "m", true},
		// Todos os tokens do nome já pertencem a campos preenchidos -> ok=false.
		{"TUBO 25MM 6M", []valorUnidade{{valor: 25, unidade: "mm"}, {valor: 6, unidade: "m"}}, 0, "", false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			valor, unidade, ok := extrairValorDoNome(c.nome, c.jaEstruturados)
			if ok != c.querOk {
				t.Fatalf("ok = %v, want %v", ok, c.querOk)
			}
			if !ok {
				return
			}
			if valor != c.querValor || unidade != c.querUnidade {
				t.Errorf("= (%v,%q), want (%v,%q)", valor, unidade, c.querValor, c.querUnidade)
			}
		})
	}
}

// --- AnalisarInconsistencias: I/O Matrix de spec-6-1 -----------------------

// TestAnalisarInconsistencias_CampoEstruturadoValidoNuncaSugere prova a linha
// "Dimensão já estruturada e válida": um campo com valor+unidade preenchidos
// nunca gera sugestão, não importa a origem.
func TestAnalisarInconsistencias_CampoEstruturadoValidoNuncaSugere(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "Tubo Estruturado Valido", CriarProdutoInput{
		Comprimento: &DimensaoInput{Valor: ptrFloat(6), Unidade: ptrStr("m")},
		Largura:     &DimensaoInput{Valor: ptrFloat(100), Unidade: ptrStr("mm")},
		Diametro:    &DimensaoInput{Valor: ptrFloat(10), Unidade: ptrStr("cm")},
		Altura:      &DimensaoInput{Valor: ptrFloat(2), Unidade: ptrStr("m")},
		Espessura:   &DimensaoInput{Valor: ptrFloat(5), Unidade: ptrStr("mm")},
	})
	// Mesmo com uma entrada em dimensoes_pendentes_revisao para um campo já
	// estruturado, nenhuma sugestão deve nascer dela — a condição de campo
	// vazio é testada ANTES de qualquer parsing.
	setDimensoesPendentesRevisao(t, db, produtoID, `{"comprimento": "cerca de 3 metros"}`)

	sugestoes, err := AnalisarInconsistencias(db)
	if err != nil {
		t.Fatalf("AnalisarInconsistencias: %v", err)
	}
	for _, campo := range ordemCamposDimensao {
		if s, ok := sugestaoDe(sugestoes, produtoID, campo); ok {
			t.Errorf("campo %q não deveria ter sugestão, got %+v", campo, s)
		}
	}
}

// TestAnalisarInconsistencias_MigracaoTextoReparseavel prova a linha
// "Migração com texto reparseável".
func TestAnalisarInconsistencias_MigracaoTextoReparseavel(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "Tubo Migracao Reparseavel", CriarProdutoInput{})
	setDimensoesPendentesRevisao(t, db, produtoID, `{"comprimento": "cerca de 3 metros"}`)

	sugestoes, err := AnalisarInconsistencias(db)
	if err != nil {
		t.Fatalf("AnalisarInconsistencias: %v", err)
	}
	s, ok := sugestaoDe(sugestoes, produtoID, "comprimento")
	if !ok {
		t.Fatalf("nenhuma sugestão para comprimento do produto %s", produtoID)
	}
	if s.Origem != "migracao" || s.Valor != 3 || s.Unidade != "m" {
		t.Errorf("sugestão = %+v, want {Origem:migracao Valor:3 Unidade:m}", s)
	}
}

// TestAnalisarInconsistencias_MigracaoTextoNaoParseavel prova a linha
// "Migração com texto não-parseável": nenhuma sugestão nasce, nenhum valor é
// inventado.
func TestAnalisarInconsistencias_MigracaoTextoNaoParseavel(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "Tubo Migracao Nao Parseavel", CriarProdutoInput{})
	setDimensoesPendentesRevisao(t, db, produtoID, `{"largura": "ver etiqueta"}`)

	sugestoes, err := AnalisarInconsistencias(db)
	if err != nil {
		t.Fatalf("AnalisarInconsistencias: %v", err)
	}
	if s, ok := sugestaoDe(sugestoes, produtoID, "largura"); ok {
		t.Errorf("largura não deveria ter sugestão, got %+v", s)
	}
}

// TestAnalisarInconsistencias_NomeComValorImplicitoUnicoCampoVazio prova a
// linha "Nome com valor implícito, único campo vazio".
func TestAnalisarInconsistencias_NomeComValorImplicitoUnicoCampoVazio(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "TUBO PVC 6M", CriarProdutoInput{
		// Comprimento fica NULL (o único campo vazio); os outros 4 preenchidos.
		Largura:   &DimensaoInput{Valor: ptrFloat(100), Unidade: ptrStr("mm")},
		Diametro:  &DimensaoInput{Valor: ptrFloat(10), Unidade: ptrStr("cm")},
		Altura:    &DimensaoInput{Valor: ptrFloat(2), Unidade: ptrStr("m")},
		Espessura: &DimensaoInput{Valor: ptrFloat(5), Unidade: ptrStr("mm")},
	})

	sugestoes, err := AnalisarInconsistencias(db)
	if err != nil {
		t.Fatalf("AnalisarInconsistencias: %v", err)
	}
	s, ok := sugestaoDe(sugestoes, produtoID, "comprimento")
	if !ok {
		t.Fatalf("nenhuma sugestão para comprimento do produto %s", produtoID)
	}
	if s.Origem != "nome" || s.Valor != 6 || s.Unidade != "m" {
		t.Errorf("sugestão = %+v, want {Origem:nome Valor:6 Unidade:m}", s)
	}
}

// TestAnalisarInconsistencias_NomeComNumeroDoisCamposVaziosNuncaSugereDeNome
// prova a linha "Nome com número, 2+ campos vazios": ambíguo demais, nenhuma
// sugestão de origem "nome" nasce para esse Produto.
func TestAnalisarInconsistencias_NomeComNumeroDoisCamposVaziosNuncaSugereDeNome(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "TUBO PVC 6M DN25", CriarProdutoInput{
		// Comprimento E diametro ficam NULL — dois campos vazios.
		Largura:   &DimensaoInput{Valor: ptrFloat(100), Unidade: ptrStr("mm")},
		Altura:    &DimensaoInput{Valor: ptrFloat(2), Unidade: ptrStr("m")},
		Espessura: &DimensaoInput{Valor: ptrFloat(5), Unidade: ptrStr("mm")},
	})

	sugestoes, err := AnalisarInconsistencias(db)
	if err != nil {
		t.Fatalf("AnalisarInconsistencias: %v", err)
	}
	for _, campo := range []string{"comprimento", "diametro"} {
		if s, ok := sugestaoDe(sugestoes, produtoID, campo); ok {
			t.Errorf("campo %q não deveria ter sugestão de origem nome, got %+v", campo, s)
		}
	}
}

// TestAnalisarInconsistencias_MigracaoTemPrioridadeSobreNome prova a ordem de
// decisão do Code Map: quando o único campo vazio tem uma entrada
// reparseável em dimensoes_pendentes_revisao, a sugestão nasce com origem
// "migracao" — extrairValorDoNome nem chega a ser tentado (mesmo quando o
// nome também contém um valor implícito diferente).
func TestAnalisarInconsistencias_MigracaoTemPrioridadeSobreNome(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "TUBO PVC 6M", CriarProdutoInput{
		Largura:   &DimensaoInput{Valor: ptrFloat(100), Unidade: ptrStr("mm")},
		Diametro:  &DimensaoInput{Valor: ptrFloat(10), Unidade: ptrStr("cm")},
		Altura:    &DimensaoInput{Valor: ptrFloat(2), Unidade: ptrStr("m")},
		Espessura: &DimensaoInput{Valor: ptrFloat(5), Unidade: ptrStr("mm")},
	})
	setDimensoesPendentesRevisao(t, db, produtoID, `{"comprimento": "cerca de 3 metros"}`)

	sugestoes, err := AnalisarInconsistencias(db)
	if err != nil {
		t.Fatalf("AnalisarInconsistencias: %v", err)
	}
	s, ok := sugestaoDe(sugestoes, produtoID, "comprimento")
	if !ok {
		t.Fatalf("nenhuma sugestão para comprimento do produto %s", produtoID)
	}
	if s.Origem != "migracao" || s.Valor != 3 || s.Unidade != "m" {
		t.Errorf("sugestão = %+v, want {Origem:migracao Valor:3 Unidade:m} (migração tem prioridade)", s)
	}
}

// TestAnalisarInconsistencias_CatalogoSemProdutoPendente prova a linha
// "Catálogo sem nenhum Produto pendente": lista vazia, sem erro.
func TestAnalisarInconsistencias_CatalogoSemProdutoPendente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	sugestoes, err := AnalisarInconsistencias(db)
	if err != nil {
		t.Fatalf("AnalisarInconsistencias: %v", err)
	}
	if len(sugestoes) != 0 {
		t.Errorf("len(sugestoes) = %d, want 0", len(sugestoes))
	}
}

// TestAnalisarInconsistencias_NomeComDoisTokensNaoReatribuiCampoJaEstruturado
// prova o fix do code review: um nome com DOIS tokens número+unidade
// embutidos — "TUBO 25MM 6M", com `diametro` já estruturado como {25,mm} e
// `comprimento` como o ÚNICO campo vazio — não pode devolver o primeiro
// token em ordem de leitura ("25MM", que já pertence a `diametro`) para
// `comprimento`; o candidato certo é o segundo token, ainda livre ("6M").
func TestAnalisarInconsistencias_NomeComDoisTokensNaoReatribuiCampoJaEstruturado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "TUBO 25MM 6M", CriarProdutoInput{
		// Comprimento fica NULL (o único campo vazio); diametro já estruturado
		// com o MESMO valor+unidade do primeiro token do nome ("25MM").
		Diametro:  &DimensaoInput{Valor: ptrFloat(25), Unidade: ptrStr("mm")},
		Largura:   &DimensaoInput{Valor: ptrFloat(100), Unidade: ptrStr("mm")},
		Altura:    &DimensaoInput{Valor: ptrFloat(2), Unidade: ptrStr("m")},
		Espessura: &DimensaoInput{Valor: ptrFloat(5), Unidade: ptrStr("mm")},
	})

	sugestoes, err := AnalisarInconsistencias(db)
	if err != nil {
		t.Fatalf("AnalisarInconsistencias: %v", err)
	}
	s, ok := sugestaoDe(sugestoes, produtoID, "comprimento")
	if !ok {
		t.Fatalf("nenhuma sugestão para comprimento do produto %s", produtoID)
	}
	if s.Origem != "nome" || s.Valor != 6 || s.Unidade != "m" {
		t.Errorf("sugestão = %+v, want {Origem:nome Valor:6 Unidade:m} (o token 25mm já pertence a diametro)", s)
	}
}

// TestAnalisarInconsistencias_DimensoesPendentesMalformadoNaoAbortaAnalise
// prova o fix do code review: um Produto cujo dimensoes_pendentes_revisao é
// JSON válido para o Postgres (a coluna é JSONB — texto sintaticamente
// inválido nunca sobrevive a um INSERT/UPDATE) mas de FORMATO inesperado
// para o Go (`map[string]string` — ex. um valor numérico em vez de string,
// linha legada corrompida de outra forma) NUNCA aborta a análise inteira com
// erro; a origem "migracao" só fica suprimida PARA ESSE Produto, e todo o
// resto do catálogo (incluindo a origem "nome" do MESMO Produto, e a
// análise de outros Produtos) continua sendo processado normalmente.
func TestAnalisarInconsistencias_DimensoesPendentesMalformadoNaoAbortaAnalise(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	// Produto com dimensoes_pendentes_revisao de formato inesperado (valor
	// numérico em vez de string — json.Unmarshal em map[string]string falha)
	// e nome com valor implícito para o único campo vazio — a origem "nome"
	// deve continuar funcionando para ele.
	produtoComJSONRuim := seedProdutoNormalizacao(t, db, "TUBO PVC 6M", CriarProdutoInput{
		Largura:   &DimensaoInput{Valor: ptrFloat(100), Unidade: ptrStr("mm")},
		Diametro:  &DimensaoInput{Valor: ptrFloat(10), Unidade: ptrStr("cm")},
		Altura:    &DimensaoInput{Valor: ptrFloat(2), Unidade: ptrStr("m")},
		Espessura: &DimensaoInput{Valor: ptrFloat(5), Unidade: ptrStr("mm")},
	})
	setDimensoesPendentesRevisao(t, db, produtoComJSONRuim, `{"comprimento": 3}`)

	// Um segundo Produto, saudável, no mesmo catálogo — prova que a análise
	// não para no primeiro Produto ruim.
	outroProdutoID := seedProdutoNormalizacao(t, db, "Cano Migracao Sadio", CriarProdutoInput{})
	setDimensoesPendentesRevisao(t, db, outroProdutoID, `{"largura": "6 m"}`)

	sugestoes, err := AnalisarInconsistencias(db)
	if err != nil {
		t.Fatalf("AnalisarInconsistencias não deveria falhar com um dimensoes_pendentes_revisao malformado: %v", err)
	}

	s, ok := sugestaoDe(sugestoes, produtoComJSONRuim, "comprimento")
	if !ok {
		t.Fatalf("produto com JSON malformado deveria ainda receber sugestão de origem nome para comprimento")
	}
	// Origem "nome" (não "migracao"): o valor numérico malformado em
	// dimensoes_pendentes_revisao nunca chega a virar sugestão — só suprime
	// a origem migracao para este Produto, sem abortar a análise.
	if s.Origem != "nome" || s.Valor != 6 || s.Unidade != "m" {
		t.Errorf("sugestão = %+v, want {Origem:nome Valor:6 Unidade:m}", s)
	}

	outra, ok := sugestaoDe(sugestoes, outroProdutoID, "largura")
	if !ok {
		t.Fatalf("segundo produto (saudável) deveria ter sugestão de origem migracao para largura")
	}
	if outra.Origem != "migracao" || outra.Valor != 6 || outra.Unidade != "m" {
		t.Errorf("sugestão do segundo produto = %+v, want {Origem:migracao Valor:6 Unidade:m}", outra)
	}
}

// --- AplicarCorrecoes/IgnorarSugestao: I/O Matrix de spec-6-2 --------------

// campoDimensao devolve o valor/unidade estruturados atuais de `campo` para
// `produtoID` — usado pelos testes de AplicarCorrecoes para verificar o
// estado gravado (ou não-gravado) depois da chamada.
func campoDimensao(t *testing.T, db *sql.DB, produtoID, campo string) (valor sql.NullFloat64, unidade sql.NullString) {
	t.Helper()
	query := fmt.Sprintf(`SELECT %s_valor, %s_unidade FROM produtos WHERE id = $1`, campo, campo)
	if err := db.QueryRow(query, produtoID).Scan(&valor, &unidade); err != nil {
		t.Fatalf("falha ao ler %s do produto %s: %v", campo, produtoID, err)
	}
	return valor, unidade
}

func contarIgnoradas(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM normalizacao_ignoradas`).Scan(&n); err != nil {
		t.Fatalf("falha ao contar normalizacao_ignoradas: %v", err)
	}
	return n
}

// TestAplicarCorrecao_Individual prova a linha "Aplicar individual": 1
// correção, campo vazio -> campo gravado, `aplicadas` contém o item.
func TestAplicarCorrecao_Individual(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "Tubo Aplicar Individual", CriarProdutoInput{})

	aplicadas, err := AplicarCorrecoes(db, []CorrecaoInput{
		{ProdutoID: produtoID, Campo: "comprimento", Valor: 6, Unidade: "m"},
	})
	if err != nil {
		t.Fatalf("AplicarCorrecoes: %v", err)
	}
	if len(aplicadas) != 1 || aplicadas[0].ProdutoID != produtoID || aplicadas[0].Campo != "comprimento" {
		t.Fatalf("aplicadas = %+v, want [{%s comprimento}]", aplicadas, produtoID)
	}

	valor, unidade := campoDimensao(t, db, produtoID, "comprimento")
	if !valor.Valid || valor.Float64 != 6 || !unidade.Valid || unidade.String != "m" {
		t.Errorf("comprimento gravado = (%+v,%+v), want (6,m)", valor, unidade)
	}
}

// TestAplicarCorrecao_LoteComItemObsoleto prova a linha "Aplicar lote com
// item obsoleto": das 2 correções, uma já tem o campo preenchido (simulando
// concorrência) — a outra é gravada normalmente, a obsoleta some de
// `aplicadas` sem abortar o lote.
func TestAplicarCorrecao_LoteComItemObsoleto(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	// produtoJaPreenchido já tem `largura` estruturada -> o guard IS NULL
	// bloqueia a escrita, o item some de `aplicadas`.
	produtoJaPreenchido := seedProdutoNormalizacao(t, db, "Tubo Ja Preenchido", CriarProdutoInput{
		Largura: &DimensaoInput{Valor: ptrFloat(50), Unidade: ptrStr("mm")},
	})
	produtoVazio := seedProdutoNormalizacao(t, db, "Tubo Vazio", CriarProdutoInput{})

	aplicadas, err := AplicarCorrecoes(db, []CorrecaoInput{
		{ProdutoID: produtoJaPreenchido, Campo: "largura", Valor: 100, Unidade: "mm"},
		{ProdutoID: produtoVazio, Campo: "comprimento", Valor: 6, Unidade: "m"},
	})
	if err != nil {
		t.Fatalf("AplicarCorrecoes: %v", err)
	}
	if len(aplicadas) != 1 || aplicadas[0].ProdutoID != produtoVazio || aplicadas[0].Campo != "comprimento" {
		t.Fatalf("aplicadas = %+v, want só [{%s comprimento}]", aplicadas, produtoVazio)
	}

	// O valor pré-existente NUNCA é sobrescrito pelo item obsoleto.
	valor, unidade := campoDimensao(t, db, produtoJaPreenchido, "largura")
	if !valor.Valid || valor.Float64 != 50 || !unidade.Valid || unidade.String != "mm" {
		t.Errorf("largura pré-existente foi sobrescrita: (%+v,%+v), want (50,mm)", valor, unidade)
	}

	valorVazio, unidadeVazio := campoDimensao(t, db, produtoVazio, "comprimento")
	if !valorVazio.Valid || valorVazio.Float64 != 6 || !unidadeVazio.Valid || unidadeVazio.String != "m" {
		t.Errorf("comprimento do produto vazio = (%+v,%+v), want (6,m)", valorVazio, unidadeVazio)
	}
}

// TestAplicarCorrecao_CampoInvalido prova a linha "Correção com campo
// inválido": nenhuma escrita, erro de validação.
func TestAplicarCorrecao_CampoInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "Tubo Campo Invalido", CriarProdutoInput{})

	_, err := AplicarCorrecoes(db, []CorrecaoInput{
		{ProdutoID: produtoID, Campo: "peso", Valor: 6, Unidade: "m"},
	})
	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("err = %v, want *ErroProdutoValidacao", err)
	}

	valor, unidade := campoDimensao(t, db, produtoID, "comprimento")
	if valor.Valid || unidade.Valid {
		t.Errorf("nenhuma escrita deveria ter ocorrido, got (%+v,%+v)", valor, unidade)
	}
}

// TestAplicarCorrecao_ListaVazia prova a linha "Correção com correcoes:[]":
// lista vazia também é erro de validação.
func TestAplicarCorrecao_ListaVazia(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	_, err := AplicarCorrecoes(db, []CorrecaoInput{})
	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("err = %v, want *ErroProdutoValidacao", err)
	}
}

// TestAplicarCorrecao_ValorZeroOuUnidadeInvalidaSaoRejeitados cobre as
// outras duas bordas de validarCorrecao (mesma regra de validarDimensao,
// produtos.go): valor <= 0 e unidade fora de {mm,cm,m}.
func TestAplicarCorrecao_ValorZeroOuUnidadeInvalidaSaoRejeitados(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "Tubo Valor Invalido", CriarProdutoInput{})

	casos := []CorrecaoInput{
		{ProdutoID: produtoID, Campo: "comprimento", Valor: 0, Unidade: "m"},
		{ProdutoID: produtoID, Campo: "comprimento", Valor: 6, Unidade: "polegada"},
		{ProdutoID: produtoID, Campo: "comprimento", Valor: limiteNumeric103 + 1, Unidade: "m"},
	}
	for _, c := range casos {
		_, err := AplicarCorrecoes(db, []CorrecaoInput{c})
		var erroValidacao *ErroProdutoValidacao
		if !errors.As(err, &erroValidacao) {
			t.Errorf("caso %+v: err = %v, want *ErroProdutoValidacao", c, err)
		}
	}
}

// TestAplicarCorrecao_LoteTotalmenteObsoletoRetornaVazio prova que um lote em
// que TODOS os itens já estão preenchidos (não só um, como em
// TestAplicarCorrecaoHandler_200LoteComItemObsoleto) devolve sucesso com
// `aplicadas` vazio — nunca um erro, mesmo sem nenhum item realmente gravado.
func TestAplicarCorrecao_LoteTotalmenteObsoletoRetornaVazio(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "Tubo Totalmente Obsoleto", CriarProdutoInput{
		Comprimento: &DimensaoInput{Valor: ptrFloat(6), Unidade: ptrStr("m")},
	})

	aplicadas, err := AplicarCorrecoes(db, []CorrecaoInput{
		{ProdutoID: produtoID, Campo: "comprimento", Valor: 9, Unidade: "m"},
	})
	if err != nil {
		t.Fatalf("AplicarCorrecoes não deveria falhar quando todo o lote está obsoleto: %v", err)
	}
	if len(aplicadas) != 0 {
		t.Fatalf("aplicadas = %+v, want []", aplicadas)
	}
}

// TestSugestaoIgnorada_ValorAcimaDoLimiteRejeitado prova que a mesma borda de
// validarCorrecao (valor > limiteNumeric103) também é aplicada por
// IgnorarSugestao.
func TestSugestaoIgnorada_ValorAcimaDoLimiteRejeitado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "Tubo Ignorar Valor Acima Limite", CriarProdutoInput{})

	err := IgnorarSugestao(db, produtoID, "comprimento", limiteNumeric103+1, "m")
	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("err = %v, want *ErroProdutoValidacao", err)
	}
	if n := contarIgnoradas(t, db); n != 0 {
		t.Errorf("normalizacao_ignoradas tem %d linhas, want 0", n)
	}
}

// TestSugestaoIgnorada_ProdutoIdMalformado prova que um produtoId
// malformado (não-UUID) colapsa em ErrProdutoNaoEncontrado — mesmo
// tratamento do UUID bem-formado mas inexistente (TestSugestaoIgnorada_
// ProdutoInexistente), via pqInvalidTextRepresentation em vez de
// pqForeignKeyViolation.
func TestSugestaoIgnorada_ProdutoIdMalformado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	err := IgnorarSugestao(db, "id-nao-e-um-uuid", "comprimento", 6, "m")
	if !errors.Is(err, ErrProdutoNaoEncontrado) {
		t.Fatalf("err = %v, want ErrProdutoNaoEncontrado", err)
	}
}

// TestAplicarCorrecao_LoteComProdutoIdMalformadoNaoAbortaLote prova o fix do
// code review: um produtoId malformado (não-UUID) no MEIO de um lote não
// pode abortar a transação inteira — mesmo tratamento de "item obsoleto"
// (some do retorno, sem erro), e o item válido do MESMO lote continua sendo
// gravado normalmente (SAVEPOINT por item, ver comentário de
// AplicarCorrecoes).
func TestAplicarCorrecao_LoteComProdutoIdMalformadoNaoAbortaLote(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoValido := seedProdutoNormalizacao(t, db, "Tubo ProdutoId Malformado", CriarProdutoInput{})

	aplicadas, err := AplicarCorrecoes(db, []CorrecaoInput{
		{ProdutoID: "id-nao-e-um-uuid", Campo: "comprimento", Valor: 6, Unidade: "m"},
		{ProdutoID: produtoValido, Campo: "comprimento", Valor: 6, Unidade: "m"},
	})
	if err != nil {
		t.Fatalf("AplicarCorrecoes não deveria falhar por causa de um produtoId malformado no meio do lote: %v", err)
	}
	if len(aplicadas) != 1 || aplicadas[0].ProdutoID != produtoValido || aplicadas[0].Campo != "comprimento" {
		t.Fatalf("aplicadas = %+v, want só [{%s comprimento}]", aplicadas, produtoValido)
	}

	valor, unidade := campoDimensao(t, db, produtoValido, "comprimento")
	if !valor.Valid || valor.Float64 != 6 || !unidade.Valid || unidade.String != "m" {
		t.Errorf("comprimento do produto válido = (%+v,%+v), want (6,m)", valor, unidade)
	}
}

// TestSugestaoIgnorada_RemoveDaProximaAnalise prova a linha "Ignorar uma
// sugestão": a tupla é gravada em normalizacao_ignoradas e uma nova chamada
// a AnalisarInconsistencias não traz mais essa sugestão.
func TestSugestaoIgnorada_RemoveDaProximaAnalise(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "Tubo Ignorar", CriarProdutoInput{})
	setDimensoesPendentesRevisao(t, db, produtoID, `{"comprimento": "cerca de 3 metros"}`)

	// Confere que a sugestão existe ANTES de ignorar.
	antes, err := AnalisarInconsistencias(db)
	if err != nil {
		t.Fatalf("AnalisarInconsistencias (antes): %v", err)
	}
	if _, ok := sugestaoDe(antes, produtoID, "comprimento"); !ok {
		t.Fatalf("sugestão deveria existir antes de ignorar")
	}

	if err := IgnorarSugestao(db, produtoID, "comprimento", 3, "m"); err != nil {
		t.Fatalf("IgnorarSugestao: %v", err)
	}

	depois, err := AnalisarInconsistencias(db)
	if err != nil {
		t.Fatalf("AnalisarInconsistencias (depois): %v", err)
	}
	if s, ok := sugestaoDe(depois, produtoID, "comprimento"); ok {
		t.Errorf("sugestão ignorada reapareceu: %+v", s)
	}
}

// TestSugestaoIgnorada_MesmaTuplaDuasVezesEIdempotente prova a linha "Ignorar
// a mesma tupla duas vezes": sucesso idempotente, nenhuma linha duplicada
// (ON CONFLICT DO NOTHING sobre a própria PRIMARY KEY).
func TestSugestaoIgnorada_MesmaTuplaDuasVezesEIdempotente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "Tubo Ignorar Duas Vezes", CriarProdutoInput{})

	if err := IgnorarSugestao(db, produtoID, "comprimento", 3, "m"); err != nil {
		t.Fatalf("IgnorarSugestao (1a chamada): %v", err)
	}
	if err := IgnorarSugestao(db, produtoID, "comprimento", 3, "m"); err != nil {
		t.Fatalf("IgnorarSugestao (2a chamada, reenvio): %v", err)
	}

	if n := contarIgnoradas(t, db); n != 1 {
		t.Errorf("normalizacao_ignoradas tem %d linhas, want 1 (idempotente)", n)
	}
}

// TestSugestaoIgnorada_ValorMudaParaOutroInconsistenteReaparece prova a linha
// "Valor muda para outro inconsistente depois de ignorado": a tupla antiga
// fica ignorada, mas quando a origem produz um valor DIFERENTE, a nova
// análise traz a sugestão de novo (chave textual diferente).
func TestSugestaoIgnorada_ValorMudaParaOutroInconsistenteReaparece(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "Tubo Valor Muda", CriarProdutoInput{})
	setDimensoesPendentesRevisao(t, db, produtoID, `{"comprimento": "cerca de 3 metros"}`)

	if err := IgnorarSugestao(db, produtoID, "comprimento", 3, "m"); err != nil {
		t.Fatalf("IgnorarSugestao: %v", err)
	}

	// A origem "migracao" muda para um valor diferente do que foi ignorado.
	setDimensoesPendentesRevisao(t, db, produtoID, `{"comprimento": "5 metros"}`)

	sugestoes, err := AnalisarInconsistencias(db)
	if err != nil {
		t.Fatalf("AnalisarInconsistencias: %v", err)
	}
	s, ok := sugestaoDe(sugestoes, produtoID, "comprimento")
	if !ok {
		t.Fatalf("sugestão com o novo valor deveria reaparecer")
	}
	if s.Valor != 5 || s.Unidade != "m" {
		t.Errorf("sugestão = %+v, want {Valor:5 Unidade:m}", s)
	}
}

// TestSugestaoIgnorada_CampoInvalido prova que IgnorarSugestao aplica a MESMA
// validação de AplicarCorrecoes: nenhuma escrita para campo inválido.
func TestSugestaoIgnorada_CampoInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	produtoID := seedProdutoNormalizacao(t, db, "Tubo Ignorar Campo Invalido", CriarProdutoInput{})

	err := IgnorarSugestao(db, produtoID, "peso", 6, "m")
	var erroValidacao *ErroProdutoValidacao
	if !errors.As(err, &erroValidacao) {
		t.Fatalf("err = %v, want *ErroProdutoValidacao", err)
	}
	if n := contarIgnoradas(t, db); n != 0 {
		t.Errorf("normalizacao_ignoradas tem %d linhas, want 0", n)
	}
}

// TestSugestaoIgnorada_ProdutoInexistente prova que um produtoId inexistente
// (UUID bem-formado, mas sem linha correspondente) colapsa em
// ErrProdutoNaoEncontrado — violação da FK produto_id -> produtos(id).
func TestSugestaoIgnorada_ProdutoInexistente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	err := IgnorarSugestao(db, "00000000-0000-0000-0000-000000000000", "comprimento", 6, "m")
	if !errors.Is(err, ErrProdutoNaoEncontrado) {
		t.Fatalf("err = %v, want ErrProdutoNaoEncontrado", err)
	}
}

// --- DetectarDuplicatas: I/O Matrix de spec-6-3 ----------------------------

// seedProdutoComEstoque cadastra um Produto com `nome` e as dimensões dadas,
// vinculado a um `estoqueID` já existente (ao contrário de
// seedProdutoNormalizacao, que sempre cria um Estoque novo por Produto) —
// necessário para os testes de DetectarDuplicatas, onde "local em comum"
// entre dois Produtos é justamente a condição sob teste.
func seedProdutoComEstoque(t *testing.T, db *sql.DB, nome, estoqueID string, dims CriarProdutoInput) string {
	t.Helper()
	dims.Nome = nome
	dims.CategoriaID = categoriaIDPorCodigo(t, db, "04.001")
	dims.EstoqueID = estoqueID
	produto, err := CriarProduto(db, dims)
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}
	return produto.ID
}

// adicionarProdutoEstoque grava uma linha extra em `produto_estoque` — usada
// pelos testes que precisam de um Produto presente em MAIS de um Estoque
// (mesmo padrão de catalogo_test.go).
func adicionarProdutoEstoque(t *testing.T, db *sql.DB, produtoID, estoqueID string, quantidade float64) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO produto_estoque (produto_id, estoque_id, quantidade) VALUES ($1, $2, $3)`,
		produtoID, estoqueID, quantidade,
	); err != nil {
		t.Fatalf("seed produto_estoque: %v", err)
	}
}

// grupoContemProduto testa se algum grupo devolvido por DetectarDuplicatas
// contém `produtoID` — usado pelos testes "não deveria agrupar" para provar
// a AUSÊNCIA sem depender de len(grupos) (outros Produtos do mesmo teste
// podiam, em tese, formar grupo por engano).
func grupoContemProduto(grupos []GrupoDuplicata, produtoID string) bool {
	for _, g := range grupos {
		for _, p := range g.Produtos {
			if p.ID == produtoID {
				return true
			}
		}
	}
	return false
}

// TestNormalizarNomeProduto prova a regra do glossário do PRD (Always,
// spec-6-3): sem acento, case-insensitive, aparado só nas pontas (espaço
// interno duplicado NÃO é colapsado).
func TestNormalizarNomeProduto(t *testing.T) {
	casos := []struct{ nome, quer string }{
		{"Tubo PVC 25mm", "tubo pvc 25mm"},
		{"  Tubo PVC 25mm  ", "tubo pvc 25mm"},
		{"TUBO PVC 25MM", "tubo pvc 25mm"},
		{"Válvula Registro Água", "valvula registro agua"},
		{"Conexão em Nylon", "conexao em nylon"},
		{"Tubo  PVC", "tubo  pvc"}, // espaço duplo INTERNO preservado
	}
	for _, c := range casos {
		if got := normalizarNomeProduto(c.nome); got != c.quer {
			t.Errorf("normalizarNomeProduto(%q) = %q, want %q", c.nome, got, c.quer)
		}
	}
}

// TestDetectarDuplicatas_DuplicataClara prova a linha "Duplicata clara": 2
// Produtos com mesmo nome, dimensões equivalentes após conversão de unidade
// (25mm == 2,5cm) e uma linha em produto_estoque para o MESMO Estoque -> 1
// grupo com os 2 Produtos.
func TestDetectarDuplicatas_DuplicataClara(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Estoque Duplicata Clara")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}

	p1 := seedProdutoComEstoque(t, db, "Tubo PVC 25mm", estoque.ID, CriarProdutoInput{
		Diametro: &DimensaoInput{Valor: ptrFloat(25), Unidade: ptrStr("mm")},
	})
	p2 := seedProdutoComEstoque(t, db, "Tubo PVC 25mm", estoque.ID, CriarProdutoInput{
		Diametro: &DimensaoInput{Valor: ptrFloat(2.5), Unidade: ptrStr("cm")},
	})

	grupos, err := DetectarDuplicatas(db)
	if err != nil {
		t.Fatalf("DetectarDuplicatas: %v", err)
	}
	if len(grupos) != 1 {
		t.Fatalf("len(grupos) = %d, want 1: %+v", len(grupos), grupos)
	}
	if len(grupos[0].Produtos) != 2 {
		t.Fatalf("len(grupos[0].Produtos) = %d, want 2: %+v", len(grupos[0].Produtos), grupos[0].Produtos)
	}
	ids := map[string]bool{grupos[0].Produtos[0].ID: true, grupos[0].Produtos[1].ID: true}
	if !ids[p1] || !ids[p2] {
		t.Errorf("grupo = %+v, want produtos %s e %s", grupos[0].Produtos, p1, p2)
	}
	// Cada Produto conserva a UNIDADE ORIGINAL gravada — a conversão para mm
	// é só interna, para comparação, nunca é escrita nem devolvida no lugar
	// do valor original.
	for _, p := range grupos[0].Produtos {
		if p.ID == p1 && (p.Dimensoes.Diametro == nil || p.Dimensoes.Diametro.Valor != 25 || p.Dimensoes.Diametro.Unidade != "mm") {
			t.Errorf("diametro de %s = %+v, want {25 mm}", p1, p.Dimensoes.Diametro)
		}
		if p.ID == p2 && (p.Dimensoes.Diametro == nil || p.Dimensoes.Diametro.Valor != 2.5 || p.Dimensoes.Diametro.Unidade != "cm") {
			t.Errorf("diametro de %s = %+v, want {2.5 cm}", p2, p.Dimensoes.Diametro)
		}
	}
}

// TestDetectarDuplicatas_MesmoNomeDimensaoDiferente prova a linha "Mesmo
// nome, dimensão diferente": 2 Produtos com o mesmo nome mas comprimento
// diferente (20mm vs 30mm) -> não agrupados.
func TestDetectarDuplicatas_MesmoNomeDimensaoDiferente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Estoque Dimensao Diferente")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}
	p1 := seedProdutoComEstoque(t, db, "Parafuso M6", estoque.ID, CriarProdutoInput{
		Comprimento: &DimensaoInput{Valor: ptrFloat(20), Unidade: ptrStr("mm")},
	})
	p2 := seedProdutoComEstoque(t, db, "Parafuso M6", estoque.ID, CriarProdutoInput{
		Comprimento: &DimensaoInput{Valor: ptrFloat(30), Unidade: ptrStr("mm")},
	})

	grupos, err := DetectarDuplicatas(db)
	if err != nil {
		t.Fatalf("DetectarDuplicatas: %v", err)
	}
	if grupoContemProduto(grupos, p1) || grupoContemProduto(grupos, p2) {
		t.Errorf("Parafuso M6 20mm/30mm não deveriam ser agrupados, got %+v", grupos)
	}
}

// TestDetectarDuplicatas_SemLocalEmComum prova a linha "Mesmo nome+dimensão,
// sem local em comum": Produto A só no Estoque X, Produto B só no Estoque Y
// -> não agrupados.
func TestDetectarDuplicatas_SemLocalEmComum(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoqueX, err := CriarEstoque(db, "Estoque X Sem Local Comum")
	if err != nil {
		t.Fatalf("CriarEstoque X: %v", err)
	}
	estoqueY, err := CriarEstoque(db, "Estoque Y Sem Local Comum")
	if err != nil {
		t.Fatalf("CriarEstoque Y: %v", err)
	}

	p1 := seedProdutoComEstoque(t, db, "Cabo Flexivel 4mm", estoqueX.ID, CriarProdutoInput{
		Diametro: &DimensaoInput{Valor: ptrFloat(4), Unidade: ptrStr("mm")},
	})
	p2 := seedProdutoComEstoque(t, db, "Cabo Flexivel 4mm", estoqueY.ID, CriarProdutoInput{
		Diametro: &DimensaoInput{Valor: ptrFloat(4), Unidade: ptrStr("mm")},
	})

	grupos, err := DetectarDuplicatas(db)
	if err != nil {
		t.Fatalf("DetectarDuplicatas: %v", err)
	}
	if grupoContemProduto(grupos, p1) || grupoContemProduto(grupos, p2) {
		t.Errorf("Produtos sem local em comum não deveriam ser agrupados, got %+v", grupos)
	}
}

// TestDetectarDuplicatas_CampoParcialmentePreenchido prova a linha "Campo
// dimensional parcialmente preenchido": A com altura=10mm, B com altura NULL
// -> não agrupados, mesmo com nome igual e local em comum.
func TestDetectarDuplicatas_CampoParcialmentePreenchido(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Estoque Campo Parcial")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}

	p1 := seedProdutoComEstoque(t, db, "Chapa Metalica", estoque.ID, CriarProdutoInput{
		Altura: &DimensaoInput{Valor: ptrFloat(10), Unidade: ptrStr("mm")},
	})
	p2 := seedProdutoComEstoque(t, db, "Chapa Metalica", estoque.ID, CriarProdutoInput{})

	grupos, err := DetectarDuplicatas(db)
	if err != nil {
		t.Fatalf("DetectarDuplicatas: %v", err)
	}
	if grupoContemProduto(grupos, p1) || grupoContemProduto(grupos, p2) {
		t.Errorf("altura preenchida de um lado e NULL do outro não deveria agrupar, got %+v", grupos)
	}
}

// TestDetectarDuplicatas_NomeComAcentoECaseDiferentesAgrupa prova a regra de
// nome normalizado (sem acento, case-insensitive) participando do
// agrupamento de ponta a ponta, não só na função normalizarNomeProduto
// isolada.
func TestDetectarDuplicatas_NomeComAcentoECaseDiferentesAgrupa(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Estoque Acento")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}

	p1 := seedProdutoComEstoque(t, db, "Válvula Registro", estoque.ID, CriarProdutoInput{})
	p2 := seedProdutoComEstoque(t, db, "valvula registro", estoque.ID, CriarProdutoInput{})

	grupos, err := DetectarDuplicatas(db)
	if err != nil {
		t.Fatalf("DetectarDuplicatas: %v", err)
	}
	if len(grupos) != 1 || len(grupos[0].Produtos) != 2 {
		t.Fatalf("grupos = %+v, want 1 grupo com 2 produtos (%s,%s)", grupos, p1, p2)
	}
}

// TestDetectarDuplicatas_TresMembrosSemInterseccaoTotalNaoAgrupa prova o
// Design Notes de spec-6-3: A e B compartilham um Estoque, B e C compartilham
// OUTRO Estoque, mas A e C não compartilham NENHUM Estoque entre si — a
// interseção TOTAL entre os 3 membros é vazia, então NENHUM grupo nasce
// (nem "A+B" nem "B+C" nem os 3 juntos) — evita o grupo "encadeado" que
// confundiria a revisão humana da Story 6.4.
func TestDetectarDuplicatas_TresMembrosSemInterseccaoTotalNaoAgrupa(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoqueAB, err := CriarEstoque(db, "Estoque AB")
	if err != nil {
		t.Fatalf("CriarEstoque AB: %v", err)
	}
	estoqueBC, err := CriarEstoque(db, "Estoque BC")
	if err != nil {
		t.Fatalf("CriarEstoque BC: %v", err)
	}

	nome := "Anel Vedacao"
	produtoA := seedProdutoComEstoque(t, db, nome, estoqueAB.ID, CriarProdutoInput{})
	produtoB := seedProdutoComEstoque(t, db, nome, estoqueAB.ID, CriarProdutoInput{})
	adicionarProdutoEstoque(t, db, produtoB, estoqueBC.ID, 1) // B também está no Estoque BC
	produtoC := seedProdutoComEstoque(t, db, nome, estoqueBC.ID, CriarProdutoInput{})

	grupos, err := DetectarDuplicatas(db)
	if err != nil {
		t.Fatalf("DetectarDuplicatas: %v", err)
	}
	for _, id := range []string{produtoA, produtoB, produtoC} {
		if grupoContemProduto(grupos, id) {
			t.Fatalf("produto %s não deveria estar em nenhum grupo (interseção total vazia): %+v", id, grupos)
		}
	}
}

// TestDetectarDuplicatas_TresMembrosComInterseccaoTotalAgrupaEmUmGrupo prova
// o caso de sucesso simétrico a TestDetectarDuplicatas_
// TresMembrosSemInterseccaoTotalNaoAgrupa: 3 Produtos com o MESMO nome
// normalizado e dimensões equivalentes, cada um com um Estoque próprio ALÉM
// de um Estoque em comum aos 3 (não um único Estoque compartilhado simples) —
// a interseção TOTAL entre os 3 (não só par a par) é não-vazia, então os 3
// devem nascer num ÚNICO GrupoDuplicata, com todos os membros presentes.
func TestDetectarDuplicatas_TresMembrosComInterseccaoTotalAgrupaEmUmGrupo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoqueComum, err := CriarEstoque(db, "Estoque Comum Tres Membros")
	if err != nil {
		t.Fatalf("CriarEstoque comum: %v", err)
	}
	estoqueExtraA, err := CriarEstoque(db, "Estoque Extra A")
	if err != nil {
		t.Fatalf("CriarEstoque extra A: %v", err)
	}
	estoqueExtraB, err := CriarEstoque(db, "Estoque Extra B")
	if err != nil {
		t.Fatalf("CriarEstoque extra B: %v", err)
	}
	estoqueExtraC, err := CriarEstoque(db, "Estoque Extra C")
	if err != nil {
		t.Fatalf("CriarEstoque extra C: %v", err)
	}

	nome := "Joelho PVC 90"
	produtoA := seedProdutoComEstoque(t, db, nome, estoqueComum.ID, CriarProdutoInput{})
	adicionarProdutoEstoque(t, db, produtoA, estoqueExtraA.ID, 1)
	produtoB := seedProdutoComEstoque(t, db, nome, estoqueComum.ID, CriarProdutoInput{})
	adicionarProdutoEstoque(t, db, produtoB, estoqueExtraB.ID, 1)
	produtoC := seedProdutoComEstoque(t, db, nome, estoqueComum.ID, CriarProdutoInput{})
	adicionarProdutoEstoque(t, db, produtoC, estoqueExtraC.ID, 1)

	grupos, err := DetectarDuplicatas(db)
	if err != nil {
		t.Fatalf("DetectarDuplicatas: %v", err)
	}
	if len(grupos) != 1 {
		t.Fatalf("len(grupos) = %d, want 1: %+v", len(grupos), grupos)
	}
	if len(grupos[0].Produtos) != 3 {
		t.Fatalf("len(grupos[0].Produtos) = %d, want 3: %+v", len(grupos[0].Produtos), grupos[0].Produtos)
	}
	ids := map[string]bool{}
	for _, p := range grupos[0].Produtos {
		ids[p.ID] = true
	}
	for _, id := range []string{produtoA, produtoB, produtoC} {
		if !ids[id] {
			t.Errorf("grupo = %+v, esperava conter o produto %s", grupos[0].Produtos, id)
		}
	}
}

// TestDetectarDuplicatas_MultiplosGruposIndependentesNaoContaminam prova que
// duas duplicatas independentes (nomes diferentes, sem nenhum Produto em
// comum) devolvem DOIS GrupoDuplicata separados numa única chamada, sem
// contaminação cruzada entre os grupos — e em ordem determinística por
// (nome, id) do primeiro membro (Code Map, spec-6-3): "Parafuso M6" vem antes
// de "Tubo PVC 25mm" alfabeticamente.
func TestDetectarDuplicatas_MultiplosGruposIndependentesNaoContaminam(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoqueTubo, err := CriarEstoque(db, "Estoque Grupo Tubo")
	if err != nil {
		t.Fatalf("CriarEstoque tubo: %v", err)
	}
	estoqueParafuso, err := CriarEstoque(db, "Estoque Grupo Parafuso")
	if err != nil {
		t.Fatalf("CriarEstoque parafuso: %v", err)
	}

	tuboA := seedProdutoComEstoque(t, db, "Tubo PVC 25mm", estoqueTubo.ID, CriarProdutoInput{
		Diametro: &DimensaoInput{Valor: ptrFloat(25), Unidade: ptrStr("mm")},
	})
	tuboB := seedProdutoComEstoque(t, db, "Tubo PVC 25mm", estoqueTubo.ID, CriarProdutoInput{
		Diametro: &DimensaoInput{Valor: ptrFloat(25), Unidade: ptrStr("mm")},
	})
	parafusoC := seedProdutoComEstoque(t, db, "Parafuso M6", estoqueParafuso.ID, CriarProdutoInput{})
	parafusoD := seedProdutoComEstoque(t, db, "Parafuso M6", estoqueParafuso.ID, CriarProdutoInput{})

	grupos, err := DetectarDuplicatas(db)
	if err != nil {
		t.Fatalf("DetectarDuplicatas: %v", err)
	}
	if len(grupos) != 2 {
		t.Fatalf("len(grupos) = %d, want 2: %+v", len(grupos), grupos)
	}

	// Ordem determinística: "Parafuso M6" < "Tubo PVC 25mm".
	if len(grupos[0].Produtos) != 2 || grupos[0].Produtos[0].Nome != "Parafuso M6" {
		t.Fatalf("grupos[0] = %+v, want grupo Parafuso M6 primeiro", grupos[0])
	}
	if len(grupos[1].Produtos) != 2 || grupos[1].Produtos[0].Nome != "Tubo PVC 25mm" {
		t.Fatalf("grupos[1] = %+v, want grupo Tubo PVC 25mm segundo", grupos[1])
	}

	idsParafuso := map[string]bool{grupos[0].Produtos[0].ID: true, grupos[0].Produtos[1].ID: true}
	if !idsParafuso[parafusoC] || !idsParafuso[parafusoD] {
		t.Errorf("grupo Parafuso = %+v, want produtos %s e %s", grupos[0].Produtos, parafusoC, parafusoD)
	}

	idsTubo := map[string]bool{grupos[1].Produtos[0].ID: true, grupos[1].Produtos[1].ID: true}
	if !idsTubo[tuboA] || !idsTubo[tuboB] {
		t.Errorf("grupo Tubo = %+v, want produtos %s e %s", grupos[1].Produtos, tuboA, tuboB)
	}

	// Nenhuma contaminação cruzada: nenhum produto de um grupo aparece no
	// outro.
	for id := range idsParafuso {
		if idsTubo[id] {
			t.Errorf("produto %s aparece nos dois grupos", id)
		}
	}
}

// TestDetectarDuplicatas_CatalogoSemDuplicatas prova a linha equivalente a
// "Catálogo sem nenhum Produto pendente" (spec-6-1) para duplicatas: lista
// vazia, sem erro, quando não há Produtos repetidos.
func TestDetectarDuplicatas_CatalogoSemDuplicatas(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	seedProdutoNormalizacao(t, db, "Produto Unico A", CriarProdutoInput{})
	seedProdutoNormalizacao(t, db, "Produto Unico B", CriarProdutoInput{})

	grupos, err := DetectarDuplicatas(db)
	if err != nil {
		t.Fatalf("DetectarDuplicatas: %v", err)
	}
	if len(grupos) != 0 {
		t.Errorf("len(grupos) = %d, want 0: %+v", len(grupos), grupos)
	}
}

// TestDetectarDuplicatas_FalhaDeBanco prova a linha "Falha de banco durante a
// consulta": com a tabela `produtos` indisponível, DetectarDuplicatas devolve
// erro (o handler mapeia para 500 INTERNAL_ERROR).
func TestDetectarDuplicatas_FalhaDeBanco(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	if _, err := db.Exec(`ALTER TABLE produtos RENAME TO produtos_indisponivel_duplicatas`); err != nil {
		t.Fatalf("renomear produtos: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`ALTER TABLE produtos_indisponivel_duplicatas RENAME TO produtos`); err != nil {
			t.Fatalf("restaurar produtos: %v", err)
		}
	})

	if _, err := DetectarDuplicatas(db); err == nil {
		t.Fatal("DetectarDuplicatas deveria falhar com a tabela produtos indisponível")
	}
}

// TestDetectarDuplicatas_FalhaDeBancoAoCarregarLocais prova o fix do code
// review: ao contrário de TestDetectarDuplicatas_FalhaDeBanco (que derruba
// `produtos`, a PRIMEIRA consulta — carregarLocaisProduto, que consulta
// `produto_estoque`, nunca chega a ser exercitada nesse caminho), este teste
// derruba `produto_estoque` especificamente — a tabela que
// carregarLocaisProduto consulta. Prova que uma falha NESSA consulta também
// aborta DetectarDuplicatas com erro; sem este teste, uma regressão que
// engolisse silenciosamente o erro de carregarLocaisProduto (ex.
// `locais, _ := carregarLocaisProduto(db)`) passaria despercebida —
// DetectarDuplicatas seguiria com locais == nil, todo par falharia
// locaisEmComumPar, e a função devolveria erroneamente `200 {"grupos":[]}`
// em vez do `500 INTERNAL_ERROR` correto.
func TestDetectarDuplicatas_FalhaDeBancoAoCarregarLocais(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	if _, err := db.Exec(`ALTER TABLE produto_estoque RENAME TO produto_estoque_indisponivel_duplicatas`); err != nil {
		t.Fatalf("renomear produto_estoque: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`ALTER TABLE produto_estoque_indisponivel_duplicatas RENAME TO produto_estoque`); err != nil {
			t.Fatalf("restaurar produto_estoque: %v", err)
		}
	})

	if _, err := DetectarDuplicatas(db); err == nil {
		t.Fatal("DetectarDuplicatas deveria falhar com a tabela produto_estoque indisponível")
	}
}

// --- MesclarDuplicatas: I/O Matrix de spec-6-4 -----------------------------

// seedProdutoParaMesclagem cadastra um Produto com `nome`, as dimensões
// dadas, vinculado a `estoqueID` (já existente) com `quantidade` inicial —
// molde de seedProdutoComEstoque (Story 6.3), acrescido do controle de
// quantidade que os testes de mesclagem precisam.
func seedProdutoParaMesclagem(t *testing.T, db *sql.DB, nome, estoqueID string, quantidade float64, dims CriarProdutoInput) string {
	t.Helper()
	dims.Nome = nome
	dims.CategoriaID = categoriaIDPorCodigo(t, db, "04.001")
	dims.EstoqueID = estoqueID
	dims.QuantidadeInicial = quantidade
	produto, err := CriarProduto(db, dims)
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}
	return produto.ID
}

// quantidadeProdutoEstoque lê a quantidade de um par (produtoID,estoqueID) —
// 0 quando a linha não existe (mesmo colapso `COALESCE` usado em produção).
func quantidadeProdutoEstoque(t *testing.T, db *sql.DB, produtoID, estoqueID string) float64 {
	t.Helper()
	var quantidade float64
	err := db.QueryRow(
		`SELECT quantidade FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2`,
		produtoID, estoqueID,
	).Scan(&quantidade)
	if errors.Is(err, sql.ErrNoRows) {
		return 0
	}
	if err != nil {
		t.Fatalf("falha ao ler produto_estoque(%s,%s): %v", produtoID, estoqueID, err)
	}
	return quantidade
}

// produtoDeletedAt lê `deleted_at` de um Produto — nil quando ainda ativo.
func produtoDeletedAt(t *testing.T, db *sql.DB, produtoID string) *time.Time {
	t.Helper()
	var deletedAt sql.NullTime
	if err := db.QueryRow(`SELECT deleted_at FROM produtos WHERE id = $1`, produtoID).Scan(&deletedAt); err != nil {
		t.Fatalf("falha ao ler deleted_at de %s: %v", produtoID, err)
	}
	if !deletedAt.Valid {
		return nil
	}
	return &deletedAt.Time
}

func contarMesclagens(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM mesclagens_duplicatas`).Scan(&n); err != nil {
		t.Fatalf("falha ao contar mesclagens_duplicatas: %v", err)
	}
	return n
}

func contarMovimentacoesDoProduto(t *testing.T, db *sql.DB, produtoID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM movimentacoes WHERE produto_id = $1`, produtoID).Scan(&n); err != nil {
		t.Fatalf("falha ao contar movimentações do produto %s: %v", produtoID, err)
	}
	return n
}

// TestMesclarDuplicatas_MesclagemSimples prova a linha "Mesclagem simples" da
// I/O Matrix: A(qtd 5, Estoque X) + B(qtd 3, Estoque X), mantém A ->
// produto_estoque(A,X).quantidade=8; B com deleted_at setado; 1 linha em
// mesclagens_duplicatas + 1 em mesclagem_produtos_removidos.
func TestMesclarDuplicatas_MesclagemSimples(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Estoque Mesclagem Simples")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}
	usuarioID := semearConta(t, db, "Almox Mesclagem Simples", "mesclagem-simples-almox@empresa.com", PapelAlmoxarife, 0)

	produtoA := seedProdutoParaMesclagem(t, db, "Tubo PVC 25mm", estoque.ID, 5, CriarProdutoInput{
		Diametro: &DimensaoInput{Valor: ptrFloat(25), Unidade: ptrStr("mm")},
	})
	produtoB := seedProdutoParaMesclagem(t, db, "Tubo PVC 25mm", estoque.ID, 3, CriarProdutoInput{
		Diametro: &DimensaoInput{Valor: ptrFloat(25), Unidade: ptrStr("mm")},
	})

	resultado, err := MesclarDuplicatas(db, produtoA, []string{produtoB}, usuarioID)
	if err != nil {
		t.Fatalf("MesclarDuplicatas: %v", err)
	}
	if resultado.ProdutoMantidoID != produtoA {
		t.Errorf("ProdutoMantidoID = %q, want %q", resultado.ProdutoMantidoID, produtoA)
	}
	if len(resultado.ProdutosRemovidosIDs) != 1 || resultado.ProdutosRemovidosIDs[0] != produtoB {
		t.Errorf("ProdutosRemovidosIDs = %+v, want [%s]", resultado.ProdutosRemovidosIDs, produtoB)
	}
	if resultado.QuantidadeConsolidada != 3 {
		t.Errorf("QuantidadeConsolidada = %v, want 3", resultado.QuantidadeConsolidada)
	}

	if q := quantidadeProdutoEstoque(t, db, produtoA, estoque.ID); q != 8 {
		t.Errorf("quantidade(A,X) = %v, want 8", q)
	}
	if q := quantidadeProdutoEstoque(t, db, produtoB, estoque.ID); q != 0 {
		t.Errorf("quantidade(B,X) = %v, want 0 (linha deveria ter sido deletada)", q)
	}
	var existeLinhaB bool
	if err := db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM produto_estoque WHERE produto_id = $1)`, produtoB,
	).Scan(&existeLinhaB); err != nil {
		t.Fatalf("falha ao checar produto_estoque de B: %v", err)
	}
	if existeLinhaB {
		t.Errorf("produto_estoque de B deveria ter sido deletado, não zerado")
	}

	if produtoDeletedAt(t, db, produtoA) != nil {
		t.Errorf("A não deveria estar soft-deletado")
	}
	if produtoDeletedAt(t, db, produtoB) == nil {
		t.Errorf("B deveria estar soft-deletado")
	}

	if n := contarMesclagens(t, db); n != 1 {
		t.Errorf("mesclagens_duplicatas tem %d linhas, want 1", n)
	}
	var mantidoGravado, usuarioGravado string
	if err := db.QueryRow(
		`SELECT produto_mantido_id, usuario_id FROM mesclagens_duplicatas`,
	).Scan(&mantidoGravado, &usuarioGravado); err != nil {
		t.Fatalf("falha ao ler mesclagens_duplicatas: %v", err)
	}
	if mantidoGravado != produtoA || usuarioGravado != usuarioID {
		t.Errorf("mesclagens_duplicatas = (%s,%s), want (%s,%s)", mantidoGravado, usuarioGravado, produtoA, usuarioID)
	}

	var nRemovidos int
	if err := db.QueryRow(`SELECT count(*) FROM mesclagem_produtos_removidos`).Scan(&nRemovidos); err != nil {
		t.Fatalf("falha ao contar mesclagem_produtos_removidos: %v", err)
	}
	var removidoGravado string
	var quantidadeGravada float64
	if err := db.QueryRow(
		`SELECT produto_removido_id, quantidade_consolidada FROM mesclagem_produtos_removidos WHERE produto_removido_id = $1`,
		produtoB,
	).Scan(&removidoGravado, &quantidadeGravada); err != nil {
		t.Fatalf("falha ao ler mesclagem_produtos_removidos: %v", err)
	}
	if nRemovidos != 1 || removidoGravado != produtoB || quantidadeGravada != 3 {
		t.Errorf("mesclagem_produtos_removidos = (%d,%s,%v), want (1,%s,3)", nRemovidos, removidoGravado, quantidadeGravada, produtoB)
	}
}

// TestMesclarDuplicatas_RemovidoComHistoricoDeMovimentacoes prova a linha
// "Removido com histórico de Movimentações": as Movimentações de B passam a
// `produto_id=A`, e a invariante "soma de Movimentações == quantidade atual"
// se preserva sem nenhuma Movimentação nova.
func TestMesclarDuplicatas_RemovidoComHistoricoDeMovimentacoes(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Estoque Mesclagem Historico")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}
	usuarioID := semearConta(t, db, "Almox Mesclagem Historico", "mesclagem-historico-almox@empresa.com", PapelAlmoxarife, 0)

	produtoA := seedProdutoParaMesclagem(t, db, "Cabo Flexivel 4mm", estoque.ID, 10, CriarProdutoInput{
		Diametro: &DimensaoInput{Valor: ptrFloat(4), Unidade: ptrStr("mm")},
	})
	produtoB := seedProdutoParaMesclagem(t, db, "Cabo Flexivel 4mm", estoque.ID, 5, CriarProdutoInput{
		Diametro: &DimensaoInput{Valor: ptrFloat(4), Unidade: ptrStr("mm")},
	})

	if _, err := RegistrarBaixa(db, produtoB, estoque.ID, usuarioID, 1); err != nil {
		t.Fatalf("RegistrarBaixa 1: %v", err)
	}
	if _, err := RegistrarBaixa(db, produtoB, estoque.ID, usuarioID, 1); err != nil {
		t.Fatalf("RegistrarBaixa 2: %v", err)
	}
	// B agora tem 3 de saldo (5 - 1 - 1) e 2 linhas em movimentacoes.
	if q := quantidadeProdutoEstoque(t, db, produtoB, estoque.ID); q != 3 {
		t.Fatalf("saldo de B antes da mesclagem = %v, want 3", q)
	}
	if n := contarMovimentacoesDoProduto(t, db, produtoB); n != 2 {
		t.Fatalf("movimentações de B antes da mesclagem = %d, want 2", n)
	}

	resultado, err := MesclarDuplicatas(db, produtoA, []string{produtoB}, usuarioID)
	if err != nil {
		t.Fatalf("MesclarDuplicatas: %v", err)
	}
	if resultado.QuantidadeConsolidada != 3 {
		t.Errorf("QuantidadeConsolidada = %v, want 3", resultado.QuantidadeConsolidada)
	}

	if q := quantidadeProdutoEstoque(t, db, produtoA, estoque.ID); q != 13 {
		t.Errorf("quantidade(A,X) = %v, want 13 (10+3)", q)
	}
	if n := contarMovimentacoesDoProduto(t, db, produtoB); n != 0 {
		t.Errorf("movimentações de B depois da mesclagem = %d, want 0 (todas reescritas para A)", n)
	}
	if n := contarMovimentacoesDoProduto(t, db, produtoA); n != 2 {
		t.Errorf("movimentações de A depois da mesclagem = %d, want 2 (as 2 antigas de B)", n)
	}
}

// TestMesclarDuplicatas_GrupoDeTresLocaisEmIntersecaoTotal prova a linha
// "Grupo de 3+, locais em interseção total": A, B e C mesclados juntos, B e C
// somados em A e soft-deletados.
func TestMesclarDuplicatas_GrupoDeTresLocaisEmIntersecaoTotal(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Estoque Mesclagem Tres Membros")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}
	usuarioID := semearConta(t, db, "Almox Mesclagem Tres Membros", "mesclagem-tres-almox@empresa.com", PapelAlmoxarife, 0)

	nome := "Joelho PVC 90"
	produtoA := seedProdutoParaMesclagem(t, db, nome, estoque.ID, 2, CriarProdutoInput{})
	produtoB := seedProdutoParaMesclagem(t, db, nome, estoque.ID, 3, CriarProdutoInput{})
	produtoC := seedProdutoParaMesclagem(t, db, nome, estoque.ID, 4, CriarProdutoInput{})

	resultado, err := MesclarDuplicatas(db, produtoA, []string{produtoB, produtoC}, usuarioID)
	if err != nil {
		t.Fatalf("MesclarDuplicatas: %v", err)
	}
	if resultado.QuantidadeConsolidada != 7 {
		t.Errorf("QuantidadeConsolidada = %v, want 7 (3+4)", resultado.QuantidadeConsolidada)
	}
	if q := quantidadeProdutoEstoque(t, db, produtoA, estoque.ID); q != 9 {
		t.Errorf("quantidade(A,X) = %v, want 9 (2+3+4)", q)
	}
	if produtoDeletedAt(t, db, produtoB) == nil {
		t.Errorf("B deveria estar soft-deletado")
	}
	if produtoDeletedAt(t, db, produtoC) == nil {
		t.Errorf("C deveria estar soft-deletado")
	}

	var nRemovidos int
	if err := db.QueryRow(`SELECT count(*) FROM mesclagem_produtos_removidos`).Scan(&nRemovidos); err != nil {
		t.Fatalf("falha ao contar mesclagem_produtos_removidos: %v", err)
	}
	if nRemovidos != 2 {
		t.Errorf("mesclagem_produtos_removidos tem %d linhas, want 2", nRemovidos)
	}
}

// TestMesclarDuplicatas_ProdutoJaMescladoPorExecucaoConcorrente prova a linha
// "Produto já mesclado por execução concorrente": B já está com deleted_at
// setado quando a mesclagem tenta travar -> *ErroMesclagemInvalida, rollback
// completo, nada escrito.
func TestMesclarDuplicatas_ProdutoJaMescladoPorExecucaoConcorrente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Estoque Mesclagem Ja Mesclado")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}
	usuarioID := semearConta(t, db, "Almox Mesclagem Ja Mesclado", "mesclagem-ja-mesclado-almox@empresa.com", PapelAlmoxarife, 0)

	produtoA := seedProdutoParaMesclagem(t, db, "Anel Vedacao", estoque.ID, 2, CriarProdutoInput{})
	produtoB := seedProdutoParaMesclagem(t, db, "Anel Vedacao", estoque.ID, 3, CriarProdutoInput{})

	if _, err := db.Exec(`UPDATE produtos SET deleted_at = now() WHERE id = $1`, produtoB); err != nil {
		t.Fatalf("falha ao simular mesclagem concorrente de B: %v", err)
	}

	_, err = MesclarDuplicatas(db, produtoA, []string{produtoB}, usuarioID)
	var erroInvalida *ErroMesclagemInvalida
	if !errors.As(err, &erroInvalida) {
		t.Fatalf("err = %v, want *ErroMesclagemInvalida", err)
	}

	if q := quantidadeProdutoEstoque(t, db, produtoA, estoque.ID); q != 2 {
		t.Errorf("quantidade(A,X) não deveria ter mudado, got %v, want 2", q)
	}
	if n := contarMesclagens(t, db); n != 0 {
		t.Errorf("mesclagens_duplicatas deveria continuar vazia, tem %d linhas", n)
	}
}

// TestMesclarDuplicatas_GrupoNaoEhMaisValido prova a linha "Grupo não é mais
// válido": a dimensão de B foi corrigida entre a listagem e a confirmação,
// deixando de ser equivalente à de A -> *ErroMesclagemInvalida, nada escrito.
func TestMesclarDuplicatas_GrupoNaoEhMaisValido(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Estoque Mesclagem Grupo Invalido")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}
	usuarioID := semearConta(t, db, "Almox Mesclagem Grupo Invalido", "mesclagem-grupo-invalido-almox@empresa.com", PapelAlmoxarife, 0)

	produtoA := seedProdutoParaMesclagem(t, db, "Tubo PVC 25mm", estoque.ID, 2, CriarProdutoInput{
		Diametro: &DimensaoInput{Valor: ptrFloat(25), Unidade: ptrStr("mm")},
	})
	produtoB := seedProdutoParaMesclagem(t, db, "Tubo PVC 25mm", estoque.ID, 3, CriarProdutoInput{
		Diametro: &DimensaoInput{Valor: ptrFloat(25), Unidade: ptrStr("mm")},
	})

	// Story 6.2: a dimensão de B é corrigida para um valor diferente entre a
	// listagem (Story 6.3) e a confirmação desta mesclagem.
	if _, err := db.Exec(
		`UPDATE produtos SET diametro_valor = 30 WHERE id = $1`, produtoB,
	); err != nil {
		t.Fatalf("falha ao corrigir dimensão de B: %v", err)
	}

	_, err = MesclarDuplicatas(db, produtoA, []string{produtoB}, usuarioID)
	var erroInvalida *ErroMesclagemInvalida
	if !errors.As(err, &erroInvalida) {
		t.Fatalf("err = %v, want *ErroMesclagemInvalida", err)
	}

	if q := quantidadeProdutoEstoque(t, db, produtoA, estoque.ID); q != 2 {
		t.Errorf("quantidade(A,X) não deveria ter mudado, got %v, want 2", q)
	}
	if produtoDeletedAt(t, db, produtoB) != nil {
		t.Errorf("B não deveria ter sido soft-deletado")
	}
}

// TestMesclarDuplicatas_FormaInvalida prova as bordas de validarFormaMesclagem
// — todas colapsam em *ErroProdutoValidacao, sem abrir transação.
func TestMesclarDuplicatas_FormaInvalida(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Estoque Mesclagem Forma Invalida")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}
	usuarioID := semearConta(t, db, "Almox Mesclagem Forma Invalida", "mesclagem-forma-invalida-almox@empresa.com", PapelAlmoxarife, 0)
	produtoA := seedProdutoParaMesclagem(t, db, "Produto Forma Invalida A", estoque.ID, 1, CriarProdutoInput{})
	produtoB := seedProdutoParaMesclagem(t, db, "Produto Forma Invalida A", estoque.ID, 1, CriarProdutoInput{})

	casos := []struct {
		nome               string
		produtoMantidoID   string
		produtoRemovidoIDs []string
	}{
		{"mantido vazio", "", []string{produtoB}},
		{"sem removidos", produtoA, []string{}},
		{"mantido entre os removidos", produtoA, []string{produtoA, produtoB}},
		{"ids duplicados", produtoA, []string{produtoB, produtoB}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := MesclarDuplicatas(db, c.produtoMantidoID, c.produtoRemovidoIDs, usuarioID)
			var erroValidacao *ErroProdutoValidacao
			if !errors.As(err, &erroValidacao) {
				t.Fatalf("err = %v, want *ErroProdutoValidacao", err)
			}
		})
	}
}

// TestMesclarDuplicatas_ProdutoIdMalformadoOuInexistenteEhGrupoInvalido prova
// que um `produtoRemovidoIds` malformado (não-UUID, SQLSTATE 22P02) ou um
// UUID bem-formado mas sem linha correspondente colapsam em
// *ErroMesclagemInvalida (mesma família de erro de "grupo inválido") — nada
// é escrito em nenhum dos dois casos.
func TestMesclarDuplicatas_ProdutoIdMalformadoOuInexistenteEhGrupoInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Estoque Mesclagem Id Malformado")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}
	usuarioID := semearConta(t, db, "Almox Mesclagem Id Malformado", "mesclagem-id-malformado-almox@empresa.com", PapelAlmoxarife, 0)
	produtoA := seedProdutoParaMesclagem(t, db, "Produto Id Malformado A", estoque.ID, 1, CriarProdutoInput{})

	casos := map[string]string{
		"malformado":  "id-nao-e-um-uuid",
		"inexistente": "00000000-0000-0000-0000-000000000000",
	}
	for nome, produtoRemovidoID := range casos {
		t.Run(nome, func(t *testing.T) {
			_, err := MesclarDuplicatas(db, produtoA, []string{produtoRemovidoID}, usuarioID)
			var erroInvalida *ErroMesclagemInvalida
			if !errors.As(err, &erroInvalida) {
				t.Fatalf("err = %v, want *ErroMesclagemInvalida", err)
			}
			if q := quantidadeProdutoEstoque(t, db, produtoA, estoque.ID); q != 1 {
				t.Errorf("quantidade(A,X) não deveria ter mudado, got %v, want 1", q)
			}
		})
	}
}

// TestMesclarDuplicatas_FalhaDeBanco prova a linha "Falha de banco durante a
// transação": com a tabela `produtos` indisponível, MesclarDuplicatas
// devolve erro (o handler mapeia para 500 INTERNAL_ERROR).
func TestMesclarDuplicatas_FalhaDeBanco(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Estoque Mesclagem Falha Banco")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}
	usuarioID := semearConta(t, db, "Almox Mesclagem Falha Banco", "mesclagem-falha-banco-almox@empresa.com", PapelAlmoxarife, 0)
	produtoA := seedProdutoParaMesclagem(t, db, "Produto Falha Banco A", estoque.ID, 1, CriarProdutoInput{})
	produtoB := seedProdutoParaMesclagem(t, db, "Produto Falha Banco A", estoque.ID, 1, CriarProdutoInput{})

	if _, err := db.Exec(`ALTER TABLE mesclagens_duplicatas RENAME TO mesclagens_duplicatas_indisponivel`); err != nil {
		t.Fatalf("renomear mesclagens_duplicatas: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`ALTER TABLE mesclagens_duplicatas_indisponivel RENAME TO mesclagens_duplicatas`); err != nil {
			t.Fatalf("restaurar mesclagens_duplicatas: %v", err)
		}
	})

	_, err = MesclarDuplicatas(db, produtoA, []string{produtoB}, usuarioID)
	if err == nil {
		t.Fatal("MesclarDuplicatas deveria falhar com a tabela mesclagens_duplicatas indisponível")
	}
	var erroInvalida *ErroMesclagemInvalida
	var erroValidacao *ErroProdutoValidacao
	if errors.As(err, &erroInvalida) || errors.As(err, &erroValidacao) {
		t.Fatalf("err = %v, want um erro genérico de banco (não *ErroMesclagemInvalida/*ErroProdutoValidacao)", err)
	}
}

// --- MesclarDuplicatas: rewrite de pedido_itens.produto_id (Story 7.2, spec-7-2) ---

// TestMesclarDuplicatas_PedidoItensRewriteSemColisao prova o rewrite básico:
// uma linha de pedido_itens do produto REMOVIDO, SEM colisão de chave
// composta com o produto MANTIDO no mesmo (pedido_id, estoque_id), tem seu
// produto_id simplesmente reescrito para o mantido — o snapshot de
// nome/quantidade permanece intocado (o Pedido continua mostrando
// exatamente o que foi pedido no momento do envio).
func TestMesclarDuplicatas_PedidoItensRewriteSemColisao(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Estoque Pedido Rewrite Sem Colisao")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}
	almoxarifeID := semearConta(t, db, "Almox Pedido Rewrite", "pedido-rewrite-almox@empresa.com", PapelAlmoxarife, 0)
	usuarioID := semearConta(t, db, "Usuario Pedido Rewrite", "pedido-rewrite-usuario@empresa.com", PapelUsuario, 0)

	produtoA := seedProdutoParaMesclagem(t, db, "Cimento CP II", estoque.ID, 5, CriarProdutoInput{})
	produtoB := seedProdutoParaMesclagem(t, db, "Cimento CP II", estoque.ID, 3, CriarProdutoInput{})

	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoB, estoque.ID, 2); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho: %v", err)
	}
	pedido, err := SubmeterPedido(db, usuarioID, "Fulano", "Obra X", "")
	if err != nil {
		t.Fatalf("seed SubmeterPedido: %v", err)
	}
	itensAntes := listarPedidoItens(t, db, pedido.ID)
	if len(itensAntes) != 1 || itensAntes[0].ProdutoID != produtoB {
		t.Fatalf("seed pedido_itens = %+v, want 1 linha do produto B", itensAntes)
	}
	nomeSnapshot := itensAntes[0].ProdutoNome

	if _, err := MesclarDuplicatas(db, produtoA, []string{produtoB}, almoxarifeID); err != nil {
		t.Fatalf("MesclarDuplicatas: %v", err)
	}

	itensDepois := listarPedidoItens(t, db, pedido.ID)
	if len(itensDepois) != 1 {
		t.Fatalf("len(itensDepois) = %d, want 1", len(itensDepois))
	}
	if itensDepois[0].ProdutoID != produtoA {
		t.Errorf("ProdutoID = %q, want %q (reescrito para o mantido)", itensDepois[0].ProdutoID, produtoA)
	}
	if itensDepois[0].ProdutoNome != nomeSnapshot {
		t.Errorf("ProdutoNome = %q, want %q (snapshot intocado)", itensDepois[0].ProdutoNome, nomeSnapshot)
	}
	if itensDepois[0].Quantidade != 2 {
		t.Errorf("Quantidade = %v, want 2 (intocada)", itensDepois[0].Quantidade)
	}
}

// TestMesclarDuplicatas_PedidoItensColisaoDeChaveComposta prova a correção
// do achado de review documentado no Spec Change Log de spec-7-2: quando o
// MESMO Pedido já tem uma linha do produto MANTIDO e uma linha do produto
// REMOVIDO no mesmo (pedido_id, estoque_id) — cenário plausível, "pedir os
// dois duplicados no mesmo Estoque" é exatamente o tipo de entrada que a
// deduplicação existe para resolver — as duas linhas são consolidadas
// (quantidade somada numa só) SEM violar a chave primária composta
// `(pedido_id, produto_id, estoque_id)`; a mesclagem é bem-sucedida (nunca
// um erro de chave duplicada). O snapshot da linha MANTIDA nunca é
// reescrito.
func TestMesclarDuplicatas_PedidoItensColisaoDeChaveComposta(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	estoque, err := CriarEstoque(db, "Estoque Pedido Colisao")
	if err != nil {
		t.Fatalf("CriarEstoque: %v", err)
	}
	almoxarifeID := semearConta(t, db, "Almox Pedido Colisao", "pedido-colisao-almox@empresa.com", PapelAlmoxarife, 0)
	usuarioID := semearConta(t, db, "Usuario Pedido Colisao", "pedido-colisao-usuario@empresa.com", PapelUsuario, 0)

	produtoA := seedProdutoParaMesclagem(t, db, "Tijolo Ceramico 6 Furos", estoque.ID, 10, CriarProdutoInput{})
	produtoB := seedProdutoParaMesclagem(t, db, "Tijolo Ceramico 6 Furos", estoque.ID, 8, CriarProdutoInput{})

	// O MESMO usuário pede os dois "duplicados" (A e B) no MESMO Estoque, no
	// MESMO Pedido — cenário exato da colisão de chave composta.
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoA, estoque.ID, 2); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho A: %v", err)
	}
	if _, err := AdicionarItemCarrinho(db, usuarioID, produtoB, estoque.ID, 4); err != nil {
		t.Fatalf("seed AdicionarItemCarrinho B: %v", err)
	}
	pedido, err := SubmeterPedido(db, usuarioID, "Fulano", "Obra X", "")
	if err != nil {
		t.Fatalf("seed SubmeterPedido: %v", err)
	}
	itensAntes := listarPedidoItens(t, db, pedido.ID)
	if len(itensAntes) != 2 {
		t.Fatalf("seed pedido_itens = %+v, want 2 linhas (A e B)", itensAntes)
	}
	var nomeSnapshotA string
	for _, it := range itensAntes {
		if it.ProdutoID == produtoA {
			nomeSnapshotA = it.ProdutoNome
		}
	}

	resultado, err := MesclarDuplicatas(db, produtoA, []string{produtoB}, almoxarifeID)
	if err != nil {
		t.Fatalf("MesclarDuplicatas deveria suceder (soma-e-descarta), erro: %v", err)
	}
	if resultado.ProdutoMantidoID != produtoA {
		t.Errorf("ProdutoMantidoID = %q, want %q", resultado.ProdutoMantidoID, produtoA)
	}

	itensDepois := listarPedidoItens(t, db, pedido.ID)
	if len(itensDepois) != 1 {
		t.Fatalf("len(itensDepois) = %d, want 1 (consolidado)", len(itensDepois))
	}
	if itensDepois[0].ProdutoID != produtoA {
		t.Errorf("ProdutoID = %q, want %q", itensDepois[0].ProdutoID, produtoA)
	}
	if itensDepois[0].Quantidade != 6 {
		t.Errorf("Quantidade = %v, want 6 (2+4)", itensDepois[0].Quantidade)
	}
	if itensDepois[0].ProdutoNome != nomeSnapshotA {
		t.Errorf("ProdutoNome = %q, want %q (snapshot da linha MANTIDA nunca reescrito)", itensDepois[0].ProdutoNome, nomeSnapshotA)
	}
}
