package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"strings"
	"testing"
	"time"
)

// Testes de migrarProdutos — Story 3.7 (spec-3-7). Cobre toda a I/O &
// Edge-Case Matrix do spec. testDB(t) (main_test.go) já cria/limpa
// legado.produtos e nunca toca categorias/nomenclatura_templates (seed
// compartilhado — migrations 000010/000013).

// legadoProdutoInput agrupa os campos opcionais de uma linha de
// legado.produtos para os testes — ponteiros nil viram SQL NULL.
type legadoProdutoInput struct {
	ID          string
	Nome        string
	NomeNulo    bool // true grava NULL em `nome`, ignorando o campo Nome acima
	Codigo      *string
	Categoria   *string
	Comprimento *string
	Largura     *string
	Diametro    *string
	Altura      *string
	Espessura   *string
	Lateral     *string
	Obs         *string
	Foto        *string
	Estoques    map[string]float64
	// EstoquesTexto grava o mapa `estoques` com VALORES DE TEXTO em vez de
	// número — simula um valor não-numérico no JSON legado (cenário
	// "quantidade não é um número válido"). Mutuamente exclusivo com
	// Estoques; nenhum teste precisa dos dois ao mesmo tempo.
	EstoquesTexto map[string]string
	CriadoEm      *time.Time
}

func strPtr(s string) *string { return &s }

func inserirLegadoProduto(t *testing.T, alvo *sql.DB, in legadoProdutoInput) {
	t.Helper()
	var estoquesJSON []byte
	switch {
	case in.EstoquesTexto != nil:
		b, err := json.Marshal(in.EstoquesTexto)
		if err != nil {
			t.Fatalf("falha ao serializar estoques (texto) do fixture: %v", err)
		}
		estoquesJSON = b
	case in.Estoques != nil:
		b, err := json.Marshal(in.Estoques)
		if err != nil {
			t.Fatalf("falha ao serializar estoques do fixture: %v", err)
		}
		estoquesJSON = b
	}
	var nomeParam any
	if !in.NomeNulo {
		nomeParam = in.Nome
	}
	_, err := alvo.Exec(`
		INSERT INTO legado.produtos (
			id, nome, codigo, categoria, comprimento, largura, diametro, altura,
			espessura, "lateral", obs, foto, estoques, criado_em
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		in.ID, nomeParam, in.Codigo, in.Categoria, in.Comprimento, in.Largura,
		in.Diametro, in.Altura, in.Espessura, in.Lateral, in.Obs, in.Foto,
		estoquesJSON, in.CriadoEm,
	)
	if err != nil {
		t.Fatalf("falha ao inserir produto legado (%s): %v", in.ID, err)
	}
}

// categoriaExistente devolve id+nome da primeira categoria do seed
// (migration 000010) — usado como categoria válida nos fixtures, sem
// hardcodar um dos 25 nomes.
func categoriaExistente(t *testing.T, alvo *sql.DB) (id, nome string) {
	t.Helper()
	if err := alvo.QueryRow(`SELECT id, nome FROM categorias ORDER BY codigo LIMIT 1`).Scan(&id, &nome); err != nil {
		t.Fatalf("falha ao buscar categoria de seed: %v", err)
	}
	return id, nome
}

// criarEstoqueAlvo insere um Estoque diretamente no alvo (sem passar por
// migrarEstoques) — suficiente para migrarProdutos casar contra
// estoques.nome_normalizado.
func criarEstoqueAlvo(t *testing.T, alvo *sql.DB, nome string) string {
	t.Helper()
	var id string
	if err := alvo.QueryRow(`INSERT INTO estoques (nome) VALUES ($1) RETURNING id`, nome).Scan(&id); err != nil {
		t.Fatalf("falha ao pré-criar estoque no alvo (%s): %v", nome, err)
	}
	return id
}

// fotoJPEGBase64Valida gera uma imagem JPEG minúscula válida e devolve sua
// representação base64 — simula o campo `foto` de um documento legado com
// foto válida.
func fotoJPEGBase64Valida(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 10), G: uint8(y * 10), B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("falha ao codificar JPEG de teste: %v", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// arquivosComPrefixo lista os nomes de arquivo em dir cujo nome começa com
// prefixo — usado para confirmar que services.SalvarFotoProduto gravou (ou
// não) a foto de um Produto migrado.
func arquivosComPrefixo(t *testing.T, dir, prefixo string) []string {
	t.Helper()
	entradas, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("falha ao listar %s: %v", dir, err)
	}
	var achados []string
	for _, e := range entradas {
		if strings.HasPrefix(e.Name(), prefixo) {
			achados = append(achados, e.Name())
		}
	}
	return achados
}

// TestMigrarProdutos_CorteInicial — cenário "Corte inicial completo": N
// linhas válidas (categoria/estoques conhecidos, dimensões parseáveis, foto
// base64 válida), --executar.
func TestMigrarProdutos_CorteInicial(t *testing.T) {
	alvo, legado := testDB(t)
	fotosDir := t.TempDir()

	categoriaID, categoriaNome := categoriaExistente(t, alvo)
	estoqueAID := criarEstoqueAlvo(t, alvo, "Canteiro A")
	estoqueBID := criarEstoqueAlvo(t, alvo, "Depósito 7")

	criadoEm := time.Date(2020, 1, 15, 10, 0, 0, 0, time.UTC)
	fotoB64 := fotoJPEGBase64Valida(t)

	inserirLegadoProduto(t, alvo, legadoProdutoInput{
		ID:          "prod-1",
		Nome:        "Cabo Flexível",
		Codigo:      strPtr("COD-001"),
		Categoria:   strPtr(categoriaNome),
		Comprimento: strPtr("6m"),
		Largura:     strPtr("100mm"),
		Foto:        strPtr(fotoB64),
		// nomes com variação de caixa/espaço — prova que o matching usa a
		// MESMA normExpr de estoques.nome_normalizado, não igualdade exata.
		Estoques: map[string]float64{"canteiro   a": 10, " Depósito 7 ": 3.5},
		CriadoEm: &criadoEm,
	})
	inserirLegadoProduto(t, alvo, legadoProdutoInput{
		ID:        "prod-2",
		Nome:      "Parafuso",
		Categoria: strPtr(categoriaNome),
		Estoques:  map[string]float64{"Canteiro A": 1},
	})

	res, err := migrarProdutos(alvo, legado, fotosDir, true)
	if err != nil {
		t.Fatalf("migrarProdutos retornou erro inesperado: %v", err)
	}
	if res.Migrados != 2 || res.JaMigrados != 0 {
		t.Fatalf("resultado = %+v, want Migrados=2 JaMigrados=0", res)
	}
	if len(res.FotosComFalha) != 0 {
		t.Errorf("FotosComFalha = %+v, want vazio", res.FotosComFalha)
	}

	if got := contar(t, alvo, `SELECT count(*) FROM produtos`); got != 2 {
		t.Errorf("count(produtos) = %d, want 2", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map WHERE entidade = 'produto'`); got != 2 {
		t.Errorf("count(migracao_id_map produto) = %d, want 2", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM produto_estoque`); got != 3 {
		t.Errorf("count(produto_estoque) = %d, want 3 (2 + 1)", got)
	}

	var produtoID string
	var catID string
	var comprimentoValor sql.NullFloat64
	var comprimentoUnidade sql.NullString
	var larguraValor sql.NullFloat64
	var dimPendentes sql.NullString
	var criadoEmLido time.Time
	if err := alvo.QueryRow(
		`SELECT p.id, p.categoria_id, p.comprimento_valor, p.comprimento_unidade, p.largura_valor, p.dimensoes_pendentes_revisao, p.criado_em
		 FROM produtos p WHERE p.nome = 'Cabo Flexível'`,
	).Scan(&produtoID, &catID, &comprimentoValor, &comprimentoUnidade, &larguraValor, &dimPendentes, &criadoEmLido); err != nil {
		t.Fatalf("falha ao ler produto migrado: %v", err)
	}
	if catID != categoriaID {
		t.Errorf("categoria_id = %q, want %q", catID, categoriaID)
	}
	if !comprimentoValor.Valid || comprimentoValor.Float64 != 6 || comprimentoUnidade.String != "m" {
		t.Errorf("comprimento = %+v/%+v, want 6/m", comprimentoValor, comprimentoUnidade)
	}
	if !larguraValor.Valid || larguraValor.Float64 != 100 {
		t.Errorf("largura_valor = %+v, want 100", larguraValor)
	}
	if dimPendentes.Valid {
		t.Errorf("dimensoes_pendentes_revisao = %q, want NULL (tudo parseável)", dimPendentes.String)
	}
	if !criadoEmLido.Equal(criadoEm) {
		t.Errorf("criado_em = %v, want %v (preservado do legado)", criadoEmLido, criadoEm)
	}

	// produto_estoque referencia os Estoques certos com as quantidades
	// certas, apesar da variação de caixa/espaço no nome legado.
	var qtdA, qtdB float64
	if err := alvo.QueryRow(`SELECT quantidade FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2`, produtoID, estoqueAID).Scan(&qtdA); err != nil {
		t.Fatalf("sem produto_estoque para Canteiro A: %v", err)
	}
	if qtdA != 10 {
		t.Errorf("quantidade Canteiro A = %v, want 10", qtdA)
	}
	if err := alvo.QueryRow(`SELECT quantidade FROM produto_estoque WHERE produto_id = $1 AND estoque_id = $2`, produtoID, estoqueBID).Scan(&qtdB); err != nil {
		t.Fatalf("sem produto_estoque para Depósito 7: %v", err)
	}
	if qtdB != 3.5 {
		t.Errorf("quantidade Depósito 7 = %v, want 3.5", qtdB)
	}

	// Foto salva via services.SalvarFotoProduto (nunca base64 em coluna).
	achados := arquivosComPrefixo(t, fotosDir, produtoID+"-")
	if len(achados) != 1 {
		t.Fatalf("arquivos de foto para %s = %v, want exatamente 1", produtoID, achados)
	}
}

// TestMigrarProdutos_Idempotente — cenário "Reexecução (idempotência)":
// nenhuma linha nova em produtos/produto_estoque/migracao_id_map, nenhuma
// foto reprocessada.
func TestMigrarProdutos_Idempotente(t *testing.T) {
	alvo, legado := testDB(t)
	fotosDir := t.TempDir()

	_, categoriaNome := categoriaExistente(t, alvo)
	criarEstoqueAlvo(t, alvo, "Canteiro A")
	fotoB64 := fotoJPEGBase64Valida(t)

	inserirLegadoProduto(t, alvo, legadoProdutoInput{
		ID:        "prod-1",
		Nome:      "Cabo Flexível",
		Categoria: strPtr(categoriaNome),
		Foto:      strPtr(fotoB64),
		Estoques:  map[string]float64{"Canteiro A": 5},
	})

	if _, err := migrarProdutos(alvo, legado, fotosDir, true); err != nil {
		t.Fatalf("primeira execução falhou: %v", err)
	}
	achadosAntes := arquivosComPrefixo(t, fotosDir, "")

	res, err := migrarProdutos(alvo, legado, fotosDir, true)
	if err != nil {
		t.Fatalf("segunda execução retornou erro: %v", err)
	}
	if res.Migrados != 0 || res.JaMigrados != 1 {
		t.Fatalf("resultado 2ª execução = %+v, want Migrados=0 JaMigrados=1", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM produtos`); got != 1 {
		t.Errorf("count(produtos) = %d, want 1 — reexecução não pode duplicar", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM produto_estoque`); got != 1 {
		t.Errorf("count(produto_estoque) = %d, want 1 — reexecução não pode duplicar", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map WHERE entidade = 'produto'`); got != 1 {
		t.Errorf("count(migracao_id_map produto) = %d, want 1", got)
	}
	achadosDepois := arquivosComPrefixo(t, fotosDir, "")
	if len(achadosDepois) != len(achadosAntes) {
		t.Errorf("arquivos de foto mudaram entre execuções: antes=%v depois=%v — foto não pode ser reprocessada", achadosAntes, achadosDepois)
	}
}

// TestMigrarProdutos_FotoReprocessadaEmLinhaJaMigrada simula uma execução
// anterior interrompida ENTRE o commit do Produto e o processamento da foto
// (ex. o processo morreu no meio): o Produto e a linha em migracao_id_map já
// existem, mas nenhuma foto foi salva em disco. Uma reexecução (ramo
// JaMigrados) precisa reprocessar a foto pendente, não pulá-la para sempre.
func TestMigrarProdutos_FotoReprocessadaEmLinhaJaMigrada(t *testing.T) {
	alvo, legado := testDB(t)
	fotosDir := t.TempDir()

	categoriaID, categoriaNome := categoriaExistente(t, alvo)
	fotoB64 := fotoJPEGBase64Valida(t)

	inserirLegadoProduto(t, alvo, legadoProdutoInput{
		ID:        "prod-1",
		Nome:      "Item Interrompido",
		Categoria: strPtr(categoriaNome),
		Foto:      strPtr(fotoB64),
	})

	// Simula o estado deixado por uma execução anterior interrompida: o
	// Produto e o mapa já existem, mas a foto nunca foi processada.
	var produtoID string
	if err := alvo.QueryRow(
		`INSERT INTO produtos (nome, categoria_id) VALUES ($1, $2) RETURNING id`,
		"Item Interrompido", categoriaID,
	).Scan(&produtoID); err != nil {
		t.Fatalf("falha ao simular produto pré-migrado: %v", err)
	}
	if _, err := alvo.Exec(
		`INSERT INTO migracao_id_map (entidade, id_legado, id_novo) VALUES ('produto', 'prod-1', $1)`, produtoID,
	); err != nil {
		t.Fatalf("falha ao simular migracao_id_map: %v", err)
	}
	if achados := arquivosComPrefixo(t, fotosDir, produtoID+"-"); len(achados) != 0 {
		t.Fatalf("pré-condição inválida: já existe foto para %s", produtoID)
	}

	res, err := migrarProdutos(alvo, legado, fotosDir, true)
	if err != nil {
		t.Fatalf("migrarProdutos retornou erro inesperado: %v", err)
	}
	if res.Migrados != 0 || res.JaMigrados != 1 {
		t.Fatalf("resultado = %+v, want Migrados=0 JaMigrados=1", res)
	}
	if len(res.FotosComFalha) != 0 {
		t.Errorf("FotosComFalha = %+v, want vazio", res.FotosComFalha)
	}
	achados := arquivosComPrefixo(t, fotosDir, produtoID+"-")
	if len(achados) != 1 {
		t.Fatalf("arquivos de foto para %s = %v, want exatamente 1 (reprocessada na linha já migrada)", produtoID, achados)
	}

	// Reexecutar de novo NÃO pode reprocessar de novo — a foto já existe.
	res2, err := migrarProdutos(alvo, legado, fotosDir, true)
	if err != nil {
		t.Fatalf("terceira execução retornou erro: %v", err)
	}
	if len(res2.FotosComFalha) != 0 {
		t.Errorf("FotosComFalha na 3ª execução = %+v, want vazio", res2.FotosComFalha)
	}
	if achadosFinal := arquivosComPrefixo(t, fotosDir, produtoID+"-"); len(achadosFinal) != 1 {
		t.Errorf("arquivos de foto após 3ª execução = %v, want continuar exatamente 1 (não reprocessa de novo)", achadosFinal)
	}
}

// TestMigrarProdutos_DimensaoAmbigua — cenário "Dimensão ambígua": produto
// migrado normalmente, dimensoes_pendentes_revisao grava o texto original,
// colunas estruturadas ficam NULL.
func TestMigrarProdutos_DimensaoAmbigua(t *testing.T) {
	alvo, legado := testDB(t)

	_, categoriaNome := categoriaExistente(t, alvo)
	inserirLegadoProduto(t, alvo, legadoProdutoInput{
		ID:          "prod-1",
		Nome:        "Tubo",
		Categoria:   strPtr(categoriaNome),
		Comprimento: strPtr("cerca de 3 metros"),
		Largura:     strPtr("50mm"),
	})

	res, err := migrarProdutos(alvo, legado, t.TempDir(), true)
	if err != nil {
		t.Fatalf("migrarProdutos retornou erro inesperado: %v", err)
	}
	if res.Migrados != 1 {
		t.Fatalf("resultado = %+v, want Migrados=1", res)
	}

	var comprimentoValor sql.NullFloat64
	var comprimentoUnidade sql.NullString
	var dimPendentes sql.NullString
	if err := alvo.QueryRow(
		`SELECT comprimento_valor, comprimento_unidade, dimensoes_pendentes_revisao FROM produtos WHERE nome = 'Tubo'`,
	).Scan(&comprimentoValor, &comprimentoUnidade, &dimPendentes); err != nil {
		t.Fatalf("falha ao ler produto migrado: %v", err)
	}
	if comprimentoValor.Valid || comprimentoUnidade.Valid {
		t.Errorf("comprimento = %+v/%+v, want NULL/NULL (ambíguo)", comprimentoValor, comprimentoUnidade)
	}
	if !dimPendentes.Valid {
		t.Fatalf("dimensoes_pendentes_revisao é NULL, want conter 'comprimento'")
	}
	var pendentes map[string]string
	if err := json.Unmarshal([]byte(dimPendentes.String), &pendentes); err != nil {
		t.Fatalf("dimensoes_pendentes_revisao não é JSON válido: %v (%q)", err, dimPendentes.String)
	}
	if pendentes["comprimento"] != "cerca de 3 metros" {
		t.Errorf("pendentes[comprimento] = %q, want %q", pendentes["comprimento"], "cerca de 3 metros")
	}
	if _, ok := pendentes["largura"]; ok {
		t.Errorf("largura não deveria estar em dimensoes_pendentes_revisao (parseável): %+v", pendentes)
	}
}

// TestMigrarProdutos_CampoLateral — cenário "Campo lateral presente": nota
// anexada em observacoes, nenhuma coluna de dimensão para lateral, não entra
// em dimensoes_pendentes_revisao.
func TestMigrarProdutos_CampoLateral(t *testing.T) {
	alvo, legado := testDB(t)

	_, categoriaNome := categoriaExistente(t, alvo)
	inserirLegadoProduto(t, alvo, legadoProdutoInput{
		ID:        "prod-1",
		Nome:      "Chapa",
		Categoria: strPtr(categoriaNome),
		Lateral:   strPtr("6m"),
		Obs:       strPtr("observação original"),
	})

	res, err := migrarProdutos(alvo, legado, t.TempDir(), true)
	if err != nil {
		t.Fatalf("migrarProdutos retornou erro inesperado: %v", err)
	}
	if res.Migrados != 1 {
		t.Fatalf("resultado = %+v, want Migrados=1", res)
	}

	var observacoes sql.NullString
	var dimPendentes sql.NullString
	if err := alvo.QueryRow(`SELECT observacoes, dimensoes_pendentes_revisao FROM produtos WHERE nome = 'Chapa'`).Scan(&observacoes, &dimPendentes); err != nil {
		t.Fatalf("falha ao ler produto migrado: %v", err)
	}
	if !observacoes.Valid || !strings.Contains(observacoes.String, `Lateral (legado): "6m"`) {
		t.Errorf("observacoes = %q, want conter nota de lateral", observacoes.String)
	}
	if !strings.Contains(observacoes.String, "observação original") {
		t.Errorf("observacoes = %q, want preservar obs original também", observacoes.String)
	}
	if dimPendentes.Valid {
		t.Errorf("dimensoes_pendentes_revisao = %q, want NULL — lateral nunca é dimensão pendente", dimPendentes.String)
	}
}

// TestMigrarProdutos_NomeInvalido — pré-validação em bloco: nome nulo, vazio
// após trim e acima de 255 runes. Mesmo padrão de
// TestMigrarEstoques_NomesInvalidos, agora para Produtos: aborta nos dois
// modos, nada escrito.
func TestMigrarProdutos_NomeInvalido(t *testing.T) {
	alvo, legado := testDB(t)

	_, categoriaNome := categoriaExistente(t, alvo)
	inserirLegadoProduto(t, alvo, legadoProdutoInput{ID: "prod-nulo", NomeNulo: true, Categoria: strPtr(categoriaNome)})
	inserirLegadoProduto(t, alvo, legadoProdutoInput{ID: "prod-vazio", Nome: "   ", Categoria: strPtr(categoriaNome)})
	inserirLegadoProduto(t, alvo, legadoProdutoInput{ID: "prod-longo", Nome: strings.Repeat("x", 256), Categoria: strPtr(categoriaNome)})
	inserirLegadoProduto(t, alvo, legadoProdutoInput{ID: "prod-ok", Nome: "Produto Válido", Categoria: strPtr(categoriaNome)})

	res, err := migrarProdutos(alvo, legado, t.TempDir(), true)
	if err == nil {
		t.Fatalf("migrarProdutos deveria abortar; res=%+v", res)
	}
	if len(res.NomesInvalidos) != 3 {
		t.Fatalf("len(NomesInvalidos) = %d, want 3 (%+v)", len(res.NomesInvalidos), res.NomesInvalidos)
	}
	motivos := map[string]string{}
	for _, n := range res.NomesInvalidos {
		motivos[n.IDLegado] = n.Motivo
	}
	for _, id := range []string{"prod-nulo", "prod-vazio", "prod-longo"} {
		if _, ok := motivos[id]; !ok {
			t.Errorf("NomesInvalidos não menciona %s: %+v", id, res.NomesInvalidos)
		}
	}
	if _, ok := motivos["prod-ok"]; ok {
		t.Errorf("prod-ok não deveria estar em NomesInvalidos")
	}
	if got := contar(t, alvo, `SELECT count(*) FROM produtos`); got != 0 {
		t.Errorf("count(produtos) = %d, want 0 — nada escrito", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map`); got != 0 {
		t.Errorf("count(migracao_id_map) = %d, want 0 — nada escrito", got)
	}
}

// TestMigrarProdutos_CategoriaDesconhecida — cenário "Categoria
// desconhecida": aborta, nada escrito.
func TestMigrarProdutos_CategoriaDesconhecida(t *testing.T) {
	alvo, legado := testDB(t)

	inserirLegadoProduto(t, alvo, legadoProdutoInput{
		ID:        "prod-1",
		Nome:      "Item Órfão",
		Categoria: strPtr("Categoria Que Não Existe"),
	})

	res, err := migrarProdutos(alvo, legado, t.TempDir(), true)
	if err == nil {
		t.Fatalf("migrarProdutos deveria abortar; res=%+v", res)
	}
	if len(res.CategoriasDesconhecidas) != 1 {
		t.Fatalf("len(CategoriasDesconhecidas) = %d, want 1 (%+v)", len(res.CategoriasDesconhecidas), res.CategoriasDesconhecidas)
	}
	c := res.CategoriasDesconhecidas[0]
	if c.IDLegado != "prod-1" || c.Categoria != "Categoria Que Não Existe" {
		t.Errorf("CategoriaDesconhecida = %+v, want id=prod-1 categoria=%q", c, "Categoria Que Não Existe")
	}
	if got := contar(t, alvo, `SELECT count(*) FROM produtos`); got != 0 {
		t.Errorf("count(produtos) = %d, want 0 — nada escrito", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map`); got != 0 {
		t.Errorf("count(migracao_id_map) = %d, want 0 — nada escrito", got)
	}
}

// TestMigrarProdutos_EstoqueDesconhecido — cenário "Estoque referenciado
// inexistente": aborta, nada escrito.
func TestMigrarProdutos_EstoqueDesconhecido(t *testing.T) {
	alvo, legado := testDB(t)

	_, categoriaNome := categoriaExistente(t, alvo)
	inserirLegadoProduto(t, alvo, legadoProdutoInput{
		ID:        "prod-1",
		Nome:      "Item",
		Categoria: strPtr(categoriaNome),
		Estoques:  map[string]float64{"Estoque Inexistente": 1},
	})

	res, err := migrarProdutos(alvo, legado, t.TempDir(), true)
	if err == nil {
		t.Fatalf("migrarProdutos deveria abortar; res=%+v", res)
	}
	if len(res.EstoquesDesconhecidos) != 1 {
		t.Fatalf("len(EstoquesDesconhecidos) = %d, want 1 (%+v)", len(res.EstoquesDesconhecidos), res.EstoquesDesconhecidos)
	}
	e := res.EstoquesDesconhecidos[0]
	if e.IDLegado != "prod-1" || e.NomeEstoque != "Estoque Inexistente" {
		t.Errorf("EstoqueDesconhecido = %+v, want id=prod-1 nome=%q", e, "Estoque Inexistente")
	}
	if got := contar(t, alvo, `SELECT count(*) FROM produtos`); got != 0 {
		t.Errorf("count(produtos) = %d, want 0 — nada escrito", got)
	}
}

// TestMigrarProdutos_CodigoDuplicadoLegado — cenário "Código duplicado
// (legado)": duas linhas com o mesmo codigo não-nulo, aborta.
func TestMigrarProdutos_CodigoDuplicadoLegado(t *testing.T) {
	alvo, legado := testDB(t)

	_, categoriaNome := categoriaExistente(t, alvo)
	inserirLegadoProduto(t, alvo, legadoProdutoInput{ID: "prod-1", Nome: "A", Codigo: strPtr("DUP"), Categoria: strPtr(categoriaNome)})
	inserirLegadoProduto(t, alvo, legadoProdutoInput{ID: "prod-2", Nome: "B", Codigo: strPtr("DUP"), Categoria: strPtr(categoriaNome)})

	res, err := migrarProdutos(alvo, legado, t.TempDir(), true)
	if err == nil {
		t.Fatalf("migrarProdutos deveria abortar; res=%+v", res)
	}
	if len(res.CodigosDuplicadosLegado) != 1 {
		t.Fatalf("len(CodigosDuplicadosLegado) = %d, want 1 (%+v)", len(res.CodigosDuplicadosLegado), res.CodigosDuplicadosLegado)
	}
	d := res.CodigosDuplicadosLegado[0]
	if d.Codigo != "DUP" || len(d.IDs) != 2 {
		t.Errorf("CodigoDuplicadoLegado = %+v, want codigo=DUP com 2 ids", d)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM produtos`); got != 0 {
		t.Errorf("count(produtos) = %d, want 0 — nada escrito", got)
	}
}

// TestMigrarProdutos_CodigoColisaoAlvo — cenário "Colisão de código com o
// alvo": produtos.codigo já existe fora do mapa, aborta.
func TestMigrarProdutos_CodigoColisaoAlvo(t *testing.T) {
	alvo, legado := testDB(t)

	categoriaID, categoriaNome := categoriaExistente(t, alvo)
	if _, err := alvo.Exec(
		`INSERT INTO produtos (nome, codigo, categoria_id) VALUES ($1, $2, $3)`,
		"Já Existente", "COD-X", categoriaID,
	); err != nil {
		t.Fatalf("falha ao pré-criar produto no alvo: %v", err)
	}

	inserirLegadoProduto(t, alvo, legadoProdutoInput{ID: "prod-1", Nome: "Novo", Codigo: strPtr("COD-X"), Categoria: strPtr(categoriaNome)})

	res, err := migrarProdutos(alvo, legado, t.TempDir(), true)
	if err == nil {
		t.Fatalf("migrarProdutos deveria abortar; res=%+v", res)
	}
	if len(res.CodigosColisaoAlvo) != 1 {
		t.Fatalf("len(CodigosColisaoAlvo) = %d, want 1 (%+v)", len(res.CodigosColisaoAlvo), res.CodigosColisaoAlvo)
	}
	c := res.CodigosColisaoAlvo[0]
	if c.IDLegado != "prod-1" || c.Codigo != "COD-X" {
		t.Errorf("CodigoColisaoAlvo = %+v, want id=prod-1 codigo=COD-X", c)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM produtos`); got != 1 {
		t.Errorf("count(produtos) = %d, want 1 — só o pré-existente", got)
	}
}

// TestMigrarProdutos_QuantidadeNegativa — cenário "Quantidade negativa":
// aborta, nada escrito.
func TestMigrarProdutos_QuantidadeNegativa(t *testing.T) {
	alvo, legado := testDB(t)

	_, categoriaNome := categoriaExistente(t, alvo)
	criarEstoqueAlvo(t, alvo, "Canteiro A")
	inserirLegadoProduto(t, alvo, legadoProdutoInput{
		ID:        "prod-1",
		Nome:      "Item",
		Categoria: strPtr(categoriaNome),
		Estoques:  map[string]float64{"Canteiro A": -5},
	})

	res, err := migrarProdutos(alvo, legado, t.TempDir(), true)
	if err == nil {
		t.Fatalf("migrarProdutos deveria abortar; res=%+v", res)
	}
	if len(res.QuantidadesInvalidas) != 1 {
		t.Fatalf("len(QuantidadesInvalidas) = %d, want 1 (%+v)", len(res.QuantidadesInvalidas), res.QuantidadesInvalidas)
	}
	q := res.QuantidadesInvalidas[0]
	if q.IDLegado != "prod-1" || q.NomeEstoque != "Canteiro A" || q.Quantidade != -5 || q.Motivo == "" {
		t.Errorf("QuantidadeInvalida = %+v, want id=prod-1 nome=Canteiro A qtd=-5 com motivo não-vazio", q)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM produtos`); got != 0 {
		t.Errorf("count(produtos) = %d, want 0 — nada escrito", got)
	}
}

// TestMigrarProdutos_QuantidadeNaoNumerica — uma entrada do mapa `estoques`
// legado com valor NÃO NUMÉRICO (ex. `"dez"` em vez de `10`) entra na mesma
// pré-checagem de quantidade inválida, aborta limpo — em vez de quebrar a
// CARGA inicial com um erro cru de `::numeric` do Postgres.
func TestMigrarProdutos_QuantidadeNaoNumerica(t *testing.T) {
	alvo, legado := testDB(t)

	_, categoriaNome := categoriaExistente(t, alvo)
	criarEstoqueAlvo(t, alvo, "Canteiro A")
	inserirLegadoProduto(t, alvo, legadoProdutoInput{
		ID:            "prod-1",
		Nome:          "Item",
		Categoria:     strPtr(categoriaNome),
		EstoquesTexto: map[string]string{"Canteiro A": "dez"},
	})

	res, err := migrarProdutos(alvo, legado, t.TempDir(), true)
	if err == nil {
		t.Fatalf("migrarProdutos deveria abortar; res=%+v", res)
	}
	if len(res.QuantidadesInvalidas) != 1 {
		t.Fatalf("len(QuantidadesInvalidas) = %d, want 1 (%+v)", len(res.QuantidadesInvalidas), res.QuantidadesInvalidas)
	}
	q := res.QuantidadesInvalidas[0]
	if q.IDLegado != "prod-1" || q.NomeEstoque != "Canteiro A" || q.Motivo == "" {
		t.Errorf("QuantidadeInvalida = %+v, want id=prod-1 nome=Canteiro A com motivo não-vazio", q)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM produtos`); got != 0 {
		t.Errorf("count(produtos) = %d, want 0 — nada escrito", got)
	}
}

// TestMigrarProdutos_EstoqueColisaoNoProduto — duas entradas do mapa
// `estoques` de UM MESMO Produto legado cujos nomes normalizam para o MESMO
// Estoque no alvo (variação de caixa/espaço) — abortaria a PK
// (produto_id, estoque_id) no meio do laço de execução sem esta
// pré-checagem; aqui detecta em bloco, antes de qualquer escrita.
func TestMigrarProdutos_EstoqueColisaoNoProduto(t *testing.T) {
	alvo, legado := testDB(t)

	_, categoriaNome := categoriaExistente(t, alvo)
	criarEstoqueAlvo(t, alvo, "Canteiro A")
	inserirLegadoProduto(t, alvo, legadoProdutoInput{
		ID:        "prod-1",
		Nome:      "Item",
		Categoria: strPtr(categoriaNome),
		Estoques:  map[string]float64{"Canteiro A": 1, "canteiro   a": 2},
	})
	// Uma segunda linha legada válida, sem colisão — prova que o laço
	// inteiro é abortado em bloco (nada escrito, nem esta linha OK).
	inserirLegadoProduto(t, alvo, legadoProdutoInput{
		ID:        "prod-2",
		Nome:      "Item OK",
		Categoria: strPtr(categoriaNome),
	})

	res, err := migrarProdutos(alvo, legado, t.TempDir(), true)
	if err == nil {
		t.Fatalf("migrarProdutos deveria abortar; res=%+v", res)
	}
	if len(res.EstoquesColisaoNoProduto) != 1 {
		t.Fatalf("len(EstoquesColisaoNoProduto) = %d, want 1 (%+v)", len(res.EstoquesColisaoNoProduto), res.EstoquesColisaoNoProduto)
	}
	c := res.EstoquesColisaoNoProduto[0]
	if c.IDLegado != "prod-1" || len(c.Nomes) != 2 {
		t.Errorf("EstoqueColisaoNoProduto = %+v, want id=prod-1 com 2 nomes", c)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM produtos`); got != 0 {
		t.Errorf("count(produtos) = %d, want 0 — nada escrito (nem prod-2, que sozinho seria válido)", got)
	}
}

// TestMigrarProdutos_FotoCorrompida — cenário "Foto corrompida": produto
// criado SEM foto, FotosComFalha lista o motivo, Migrados ainda soma.
func TestMigrarProdutos_FotoCorrompida(t *testing.T) {
	alvo, legado := testDB(t)
	fotosDir := t.TempDir()

	_, categoriaNome := categoriaExistente(t, alvo)
	fotoCorrompida := base64.StdEncoding.EncodeToString([]byte("isto não é uma imagem"))
	inserirLegadoProduto(t, alvo, legadoProdutoInput{
		ID:        "prod-1",
		Nome:      "Item Com Foto Ruim",
		Categoria: strPtr(categoriaNome),
		Foto:      strPtr(fotoCorrompida),
	})

	res, err := migrarProdutos(alvo, legado, fotosDir, true)
	if err != nil {
		t.Fatalf("migrarProdutos retornou erro inesperado: %v", err)
	}
	if res.Migrados != 1 {
		t.Fatalf("resultado = %+v, want Migrados=1 (foto ruim não aborta)", res)
	}
	if len(res.FotosComFalha) != 1 {
		t.Fatalf("len(FotosComFalha) = %d, want 1 (%+v)", len(res.FotosComFalha), res.FotosComFalha)
	}
	if res.FotosComFalha[0].IDLegado != "prod-1" || res.FotosComFalha[0].Motivo == "" {
		t.Errorf("FotoFalha = %+v, want id=prod-1 com motivo não-vazio", res.FotosComFalha[0])
	}
	if got := contar(t, alvo, `SELECT count(*) FROM produtos`); got != 1 {
		t.Errorf("count(produtos) = %d, want 1 — produto migrado mesmo com foto ruim", got)
	}

	var produtoID string
	if err := alvo.QueryRow(`SELECT id FROM produtos WHERE nome = 'Item Com Foto Ruim'`).Scan(&produtoID); err != nil {
		t.Fatalf("falha ao ler produto migrado: %v", err)
	}
	if achados := arquivosComPrefixo(t, fotosDir, produtoID+"-"); len(achados) != 0 {
		t.Errorf("arquivos de foto = %v, want nenhum (foto corrompida)", achados)
	}
}

// TestMigrarProdutos_ProdutoSemFoto — cenário "Produto sem foto": criado sem
// foto, sem entrada em FotosComFalha.
func TestMigrarProdutos_ProdutoSemFoto(t *testing.T) {
	alvo, legado := testDB(t)
	fotosDir := t.TempDir()

	_, categoriaNome := categoriaExistente(t, alvo)
	inserirLegadoProduto(t, alvo, legadoProdutoInput{ID: "prod-1", Nome: "Sem Foto", Categoria: strPtr(categoriaNome)})

	res, err := migrarProdutos(alvo, legado, fotosDir, true)
	if err != nil {
		t.Fatalf("migrarProdutos retornou erro inesperado: %v", err)
	}
	if res.Migrados != 1 {
		t.Fatalf("resultado = %+v, want Migrados=1", res)
	}
	if len(res.FotosComFalha) != 0 {
		t.Errorf("FotosComFalha = %+v, want vazio — foto ausente não é falha", res.FotosComFalha)
	}
	if achados := arquivosComPrefixo(t, fotosDir, ""); len(achados) != 0 {
		t.Errorf("arquivos de foto = %v, want nenhum", achados)
	}
}

// TestMigrarProdutos_SeedAusente — cenário "Seed ausente": categorias (ou
// nomenclatura_templates) vazia aborta ANTES de ler qualquer linha legada.
// Restaura o seed compartilhado ao final via t.Cleanup, mesmo se o teste
// falhar — Boundaries desta story proíbem re-semear categorias/templates
// como parte do CÓDIGO de produção, mas este teste precisa do estado vazio
// temporariamente, sempre devolvido ao normal.
func TestMigrarProdutos_SeedAusente(t *testing.T) {
	alvo, legado := testDB(t)

	type categoriaRow struct{ codigo, nome string }
	var originais []categoriaRow
	rows, err := alvo.Query(`SELECT codigo, nome FROM categorias`)
	if err != nil {
		t.Fatalf("falha ao capturar snapshot de categorias: %v", err)
	}
	for rows.Next() {
		var c categoriaRow
		if err := rows.Scan(&c.codigo, &c.nome); err != nil {
			rows.Close()
			t.Fatalf("falha ao ler snapshot de categorias: %v", err)
		}
		originais = append(originais, c)
	}
	rows.Close()
	if len(originais) == 0 {
		t.Fatal("seed de categorias já está vazio antes do teste — não deveria acontecer (migration 000010)")
	}

	t.Cleanup(func() {
		for _, c := range originais {
			if _, err := alvo.Exec(
				`INSERT INTO categorias (codigo, nome) VALUES ($1, $2) ON CONFLICT DO NOTHING`, c.codigo, c.nome,
			); err != nil {
				t.Errorf("falha ao restaurar categoria %q no cleanup: %v", c.codigo, err)
			}
		}
	})

	if _, err := alvo.Exec(`DELETE FROM categorias`); err != nil {
		t.Fatalf("falha ao esvaziar categorias para o teste: %v", err)
	}

	res, err := migrarProdutos(alvo, legado, t.TempDir(), true)
	if err == nil {
		t.Fatalf("migrarProdutos deveria abortar com seed ausente; res=%+v", res)
	}
	if res.Migrados != 0 || res.JaMigrados != 0 {
		t.Errorf("resultado = %+v, want tudo zero (abortou antes de ler legado)", res)
	}
}

// TestMigrarProdutos_DryRun — cenário "Dry-run": relata migraria N sem
// escrever nada e sem processar nenhuma foto.
func TestMigrarProdutos_DryRun(t *testing.T) {
	alvo, legado := testDB(t)
	fotosDir := t.TempDir()

	_, categoriaNome := categoriaExistente(t, alvo)
	criarEstoqueAlvo(t, alvo, "Canteiro A")
	fotoB64 := fotoJPEGBase64Valida(t)
	inserirLegadoProduto(t, alvo, legadoProdutoInput{
		ID:        "prod-1",
		Nome:      "Item",
		Categoria: strPtr(categoriaNome),
		Foto:      strPtr(fotoB64),
		Estoques:  map[string]float64{"Canteiro A": 1},
	})
	inserirLegadoProduto(t, alvo, legadoProdutoInput{ID: "prod-2", Nome: "Item 2", Categoria: strPtr(categoriaNome)})

	res, err := migrarProdutos(alvo, legado, fotosDir, false)
	if err != nil {
		t.Fatalf("dry-run retornou erro: %v", err)
	}
	if res.Migrados != 2 || res.JaMigrados != 0 {
		t.Fatalf("resultado = %+v, want Migrados=2 JaMigrados=0", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM produtos`); got != 0 {
		t.Errorf("count(produtos) = %d, want 0 — dry-run não escreve", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map`); got != 0 {
		t.Errorf("count(migracao_id_map) = %d, want 0 — dry-run não escreve", got)
	}
	if achados := arquivosComPrefixo(t, fotosDir, ""); len(achados) != 0 {
		t.Errorf("arquivos de foto = %v, want nenhum — dry-run não processa foto", achados)
	}
}

// TestParseDimensaoLegado cobre o parser único de dimensão texto-livre ->
// estruturado (unitário, sem banco).
func TestParseDimensaoLegado(t *testing.T) {
	casos := []struct {
		texto       string
		wantValor   float64
		wantUnidade string
		wantOK      bool
	}{
		{"6m", 6, "m", true},
		{"100mm", 100, "mm", true},
		{"3,5cm", 3.5, "cm", true},
		{"3.5cm", 3.5, "cm", true},
		{"cerca de 3 metros", 0, "", false},
		{"", 0, "", false},
		{"6 m", 0, "", false}, // espaço interno não é o formato dos exemplos do addendum
	}
	for _, c := range casos {
		valor, unidade, ok := parseDimensaoLegado(c.texto)
		if ok != c.wantOK {
			t.Errorf("parseDimensaoLegado(%q) ok = %v, want %v", c.texto, ok, c.wantOK)
			continue
		}
		if ok && (valor != c.wantValor || unidade != c.wantUnidade) {
			t.Errorf("parseDimensaoLegado(%q) = (%v, %q), want (%v, %q)", c.texto, valor, unidade, c.wantValor, c.wantUnidade)
		}
	}
}
