package services

import (
	"database/sql"
	"testing"
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
