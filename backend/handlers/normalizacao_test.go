package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stockflow/backend/middleware"
	"stockflow/backend/realtime"
	"stockflow/backend/services"
)

// --- despacho pela MESMA composição de newMux (main.go) --------------------
//
// GET /api/normalizacao/inconsistencias -> RequireAuth ->
// RequireRole(almoxarife) -> handler. Mesmo molde de getMovimentacoes.

func getInconsistencias(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/normalizacao/inconsistencias",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				AnalisarInconsistenciasHandler(db))))
	r := httptest.NewRequest(http.MethodGet, "/api/normalizacao/inconsistencias", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// seedProdutoComPendenciaHandler cadastra um Produto com `nome` e as
// dimensões estruturadas dadas (`CriarProdutoInput` parcial — CategoriaID/
// EstoqueID/Nome são preenchidos aqui) e devolve seu id.
func seedProdutoComPendenciaHandler(t *testing.T, db *sql.DB, nome string, dims services.CriarProdutoInput) string {
	t.Helper()
	estoque, err := services.CriarEstoque(db, "Estoque "+nome)
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	dims.Nome = nome
	dims.CategoriaID = categoriaIDPorCodigoHandler(t, db, "04.001")
	dims.EstoqueID = estoque.ID
	produto, err := services.CriarProduto(db, dims)
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}
	return produto.ID
}

// TestAnalisarInconsistenciasHandler_200ComSugestoes prova a linha "Nome com
// valor implícito, único campo vazio" da I/O Matrix na fronteira HTTP: 200
// com o formato de fio exato `{"sugestoes":[{"produtoId","produtoNome",
// "campo","valorSugerido":{"valor","unidade"},"origem"}]}`.
func TestAnalisarInconsistenciasHandler_200ComSugestoes(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Normalizacao 200", "normalizacao-200-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "normalizacao-200-almox@empresa.com", "senha-123456")

	produtoID := seedProdutoComPendenciaHandler(t, db, "TUBO PVC 6M", services.CriarProdutoInput{
		Largura:   &services.DimensaoInput{Valor: ptrFloatHandler(100), Unidade: ptrStrHandler("mm")},
		Diametro:  &services.DimensaoInput{Valor: ptrFloatHandler(10), Unidade: ptrStrHandler("cm")},
		Altura:    &services.DimensaoInput{Valor: ptrFloatHandler(2), Unidade: ptrStrHandler("m")},
		Espessura: &services.DimensaoInput{Valor: ptrFloatHandler(5), Unidade: ptrStrHandler("mm")},
	})

	w := getInconsistencias(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	var resp struct {
		Sugestoes []struct {
			ProdutoID     string `json:"produtoId"`
			ProdutoNome   string `json:"produtoNome"`
			Campo         string `json:"campo"`
			ValorSugerido struct {
				Valor   float64 `json:"valor"`
				Unidade string  `json:"unidade"`
			} `json:"valorSugerido"`
			Origem string `json:"origem"`
		} `json:"sugestoes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}

	var encontrada bool
	for _, s := range resp.Sugestoes {
		if s.ProdutoID != produtoID {
			continue
		}
		encontrada = true
		if s.ProdutoNome != "TUBO PVC 6M" {
			t.Errorf("produtoNome = %q, want %q", s.ProdutoNome, "TUBO PVC 6M")
		}
		if s.Campo != "comprimento" {
			t.Errorf("campo = %q, want comprimento", s.Campo)
		}
		if s.ValorSugerido.Valor != 6 || s.ValorSugerido.Unidade != "m" {
			t.Errorf("valorSugerido = %+v, want {6 m}", s.ValorSugerido)
		}
		if s.Origem != "nome" {
			t.Errorf("origem = %q, want nome", s.Origem)
		}
	}
	if !encontrada {
		t.Fatalf("nenhuma sugestão para o produto %s em %+v", produtoID, resp.Sugestoes)
	}
}

// TestAnalisarInconsistenciasHandler_200ListaVazia prova a linha "Catálogo
// sem nenhum Produto pendente": 200 {"sugestoes":[]} (array, nunca null).
func TestAnalisarInconsistenciasHandler_200ListaVazia(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Normalizacao Vazia", "normalizacao-vazia-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "normalizacao-vazia-almox@empresa.com", "senha-123456")

	w := getInconsistencias(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != `{"sugestoes":[]}`+"\n" && body != `{"sugestoes":[]}` {
		t.Errorf("body = %s, want {\"sugestoes\":[]}", body)
	}
}

// TestAnalisarInconsistenciasHandler_403PapelUsuario prova a linha "Chamada
// com papel usuario": 403 FORBIDDEN, decidido por RequireRole — o handler
// nunca executa.
func TestAnalisarInconsistenciasHandler_403PapelUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Usuario Normalizacao 403", "normalizacao-403-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "normalizacao-403-usuario@empresa.com", "senha-123456")

	w := getInconsistencias(db, "Bearer "+token)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "FORBIDDEN" {
		t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
	}
	var comLista map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &comLista)
	if _, tem := comLista["sugestoes"]; tem {
		t.Errorf("corpo do 403 contém \"sugestoes\" — o handler nunca deveria ter executado")
	}
}

// TestAnalisarInconsistenciasHandler_401SemToken prova que uma requisição
// sem Authorization -> 401, produzido só por RequireAuth.
func TestAnalisarInconsistenciasHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)

	w := getInconsistencias(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestAnalisarInconsistenciasHandler_500FalhaDeBanco prova que, com a tabela
// `produtos` indisponível, um `almoxarife` autenticado recebe 500
// INTERNAL_ERROR no envelope AD-14 (a autenticação usa `usuarios`, que
// continua de pé). A tabela é renomeada para fora e restaurada por
// t.Cleanup — mesmo molde de TestListarMovimentacoesHandler_500FalhaDeBanco
// (movimentacoes_test.go), execução serial, sem t.Parallel.
func TestAnalisarInconsistenciasHandler_500FalhaDeBanco(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Normalizacao 500", "normalizacao-500-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "normalizacao-500-almox@empresa.com", "senha-123456")

	if _, err := db.Exec(`ALTER TABLE produtos RENAME TO produtos_indisponivel`); err != nil {
		t.Fatalf("renomear produtos: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`ALTER TABLE produtos_indisponivel RENAME TO produtos`); err != nil {
			t.Fatalf("restaurar produtos: %v", err)
		}
	})

	w := getInconsistencias(db, "Bearer "+token)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("code = %q, want INTERNAL_ERROR", env.Error.Code)
	}
}

// TestAnalisarInconsistenciasHandler_500FalhaAoCarregarIgnoradas prova que,
// com a tabela `normalizacao_ignoradas` indisponível, um `almoxarife`
// autenticado recebe 500 INTERNAL_ERROR — AnalisarInconsistencias (Story
// 6.2) carrega `normalizacao_ignoradas` ANTES de varrer `produtos`
// (services/normalizacao.go), então uma falha nessa carga deve abortar a
// análise, não seguir adiante como se nada estivesse ignorado. Mesmo molde
// de TestAnalisarInconsistenciasHandler_500FalhaDeBanco, mas visando a
// tabela nova em vez de `produtos`.
func TestAnalisarInconsistenciasHandler_500FalhaAoCarregarIgnoradas(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Normalizacao Ignoradas 500", "normalizacao-ignoradas-500-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "normalizacao-ignoradas-500-almox@empresa.com", "senha-123456")

	if _, err := db.Exec(`ALTER TABLE normalizacao_ignoradas RENAME TO normalizacao_ignoradas_indisponivel`); err != nil {
		t.Fatalf("renomear normalizacao_ignoradas: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`ALTER TABLE normalizacao_ignoradas_indisponivel RENAME TO normalizacao_ignoradas`); err != nil {
			t.Fatalf("restaurar normalizacao_ignoradas: %v", err)
		}
	})

	w := getInconsistencias(db, "Bearer "+token)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("code = %q, want INTERNAL_ERROR", env.Error.Code)
	}
}

func ptrFloatHandler(v float64) *float64 { return &v }
func ptrStrHandler(v string) *string     { return &v }

// --- POST /api/normalizacao/correcoes: I/O Matrix de spec-6-2 -------------
//
// RequireAuth -> RequireRole(almoxarife) -> AplicarCorrecoesHandler, mesma
// composição de POST /api/produtos (main.go).

func postCorrecoes(db *sql.DB, registro *realtime.Registry, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/normalizacao/correcoes",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				AplicarCorrecoesHandler(db, registro))))
	r := httptest.NewRequest(http.MethodPost, "/api/normalizacao/correcoes", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// TestAplicarCorrecaoHandler_200Individual prova a linha "Aplicar
// individual" na fronteira HTTP: 200 {"aplicadas":[{"produtoId","campo"}]}.
func TestAplicarCorrecaoHandler_200Individual(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Correcao 200", "correcao-200-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "correcao-200-almox@empresa.com", "senha-123456")
	produtoID := seedProdutoComPendenciaHandler(t, db, "Tubo Correcao Individual", services.CriarProdutoInput{})

	body := `{"correcoes":[{"produtoId":"` + produtoID + `","campo":"comprimento","valorSugerido":{"valor":6,"unidade":"m"}}]}`
	w := postCorrecoes(db, realtime.NewRegistry(), "Bearer "+token, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	var resp struct {
		Aplicadas []struct {
			ProdutoID string `json:"produtoId"`
			Campo     string `json:"campo"`
		} `json:"aplicadas"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Aplicadas) != 1 || resp.Aplicadas[0].ProdutoID != produtoID || resp.Aplicadas[0].Campo != "comprimento" {
		t.Fatalf("aplicadas = %+v, want [{%s comprimento}]", resp.Aplicadas, produtoID)
	}

	var valor sql.NullFloat64
	var unidade sql.NullString
	if err := db.QueryRow(`SELECT comprimento_valor, comprimento_unidade FROM produtos WHERE id = $1`, produtoID).
		Scan(&valor, &unidade); err != nil {
		t.Fatalf("falha ao ler comprimento: %v", err)
	}
	if !valor.Valid || valor.Float64 != 6 || !unidade.Valid || unidade.String != "m" {
		t.Errorf("comprimento gravado = (%+v,%+v), want (6,m)", valor, unidade)
	}
}

// TestAplicarCorrecaoHandler_200LoteComItemObsoleto prova a linha "Aplicar
// lote com item obsoleto": a correção obsoleta some de `aplicadas`, o lote
// não aborta, status continua 200.
func TestAplicarCorrecaoHandler_200LoteComItemObsoleto(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Correcao Obsoleta", "correcao-obsoleta-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "correcao-obsoleta-almox@empresa.com", "senha-123456")

	produtoJaPreenchido := seedProdutoComPendenciaHandler(t, db, "Tubo Ja Preenchido Handler", services.CriarProdutoInput{
		Largura: &services.DimensaoInput{Valor: ptrFloatHandler(50), Unidade: ptrStrHandler("mm")},
	})
	produtoVazio := seedProdutoComPendenciaHandler(t, db, "Tubo Vazio Handler", services.CriarProdutoInput{})

	body := `{"correcoes":[` +
		`{"produtoId":"` + produtoJaPreenchido + `","campo":"largura","valorSugerido":{"valor":100,"unidade":"mm"}},` +
		`{"produtoId":"` + produtoVazio + `","campo":"comprimento","valorSugerido":{"valor":6,"unidade":"m"}}` +
		`]}`
	w := postCorrecoes(db, realtime.NewRegistry(), "Bearer "+token, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	var resp struct {
		Aplicadas []struct {
			ProdutoID string `json:"produtoId"`
			Campo     string `json:"campo"`
		} `json:"aplicadas"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Aplicadas) != 1 || resp.Aplicadas[0].ProdutoID != produtoVazio {
		t.Fatalf("aplicadas = %+v, want só [{%s comprimento}]", resp.Aplicadas, produtoVazio)
	}
}

// TestAplicarCorrecaoHandler_400CampoInvalido prova a linha "Correção com
// campo inválido": 400 VALIDATION_ERROR, nenhuma escrita.
func TestAplicarCorrecaoHandler_400CampoInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Correcao Campo Invalido", "correcao-campo-invalido-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "correcao-campo-invalido-almox@empresa.com", "senha-123456")
	produtoID := seedProdutoComPendenciaHandler(t, db, "Tubo Campo Invalido Handler", services.CriarProdutoInput{})

	body := `{"correcoes":[{"produtoId":"` + produtoID + `","campo":"peso","valorSugerido":{"valor":6,"unidade":"m"}}]}`
	w := postCorrecoes(db, realtime.NewRegistry(), "Bearer "+token, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// TestAplicarCorrecaoHandler_400ListaVazia prova a linha "Correção com
// correcoes:[]": 400 VALIDATION_ERROR.
func TestAplicarCorrecaoHandler_400ListaVazia(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Correcao Lista Vazia", "correcao-lista-vazia-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "correcao-lista-vazia-almox@empresa.com", "senha-123456")

	w := postCorrecoes(db, realtime.NewRegistry(), "Bearer "+token, `{"correcoes":[]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// TestAplicarCorrecaoHandler_403PapelUsuario prova a linha "Chamada com
// papel usuario": 403 FORBIDDEN, decidido por RequireRole.
func TestAplicarCorrecaoHandler_403PapelUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Usuario Correcao 403", "correcao-403-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "correcao-403-usuario@empresa.com", "senha-123456")

	w := postCorrecoes(db, realtime.NewRegistry(), "Bearer "+token, `{"correcoes":[]}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
}

// TestAplicarCorrecaoHandler_401SemToken prova que uma requisição sem
// Authorization -> 401, produzido só por RequireAuth.
func TestAplicarCorrecaoHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)

	w := postCorrecoes(db, realtime.NewRegistry(), "", `{"correcoes":[]}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
}

// TestAplicarCorrecaoHandler_PublicaUmEventoPorProdutoDistinto prova que um
// lote por produto (2 campos do MESMO produto aplicados de uma vez) publica
// `{"resource":"produtos","id":<id>,"change":"updated"}` uma única vez —
// nunca uma vez por campo.
func TestAplicarCorrecaoHandler_PublicaUmEventoPorProdutoDistinto(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Correcao Evento", "correcao-evento-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "correcao-evento-almox@empresa.com", "senha-123456")
	produtoID := seedProdutoComPendenciaHandler(t, db, "Tubo Correcao Evento", services.CriarProdutoInput{})

	registro := realtime.NewRegistry()
	eventos, cancelar := registro.Subscribe()
	defer cancelar()

	body := `{"correcoes":[` +
		`{"produtoId":"` + produtoID + `","campo":"comprimento","valorSugerido":{"valor":6,"unidade":"m"}},` +
		`{"produtoId":"` + produtoID + `","campo":"largura","valorSugerido":{"valor":100,"unidade":"mm"}}` +
		`]}`
	w := postCorrecoes(db, registro, "Bearer "+token, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	select {
	case ev := <-eventos:
		if ev.Resource != "produtos" || ev.ID != produtoID || ev.Change != "updated" {
			t.Fatalf("evento = %+v, want {produtos %s updated}", ev, produtoID)
		}
	case <-time.After(time.Second):
		t.Fatal("nenhum evento publicado em 1s após AplicarCorrecoesHandler bem-sucedido")
	}

	select {
	case ev := <-eventos:
		t.Fatalf("segundo evento inesperado para o mesmo produtoId: %+v (deveria publicar só uma vez por lote)", ev)
	case <-time.After(200 * time.Millisecond):
		// nenhum segundo evento — correto.
	}
}

// TestAplicarCorrecaoHandler_200LoteComProdutoIdMalformadoNaoAborta prova o
// fix do code review na fronteira HTTP: um produtoId malformado no MEIO do
// lote não pode virar 500 nem abortar as outras correções válidas do MESMO
// lote — 200, `aplicadas` só com o item válido.
func TestAplicarCorrecaoHandler_200LoteComProdutoIdMalformadoNaoAborta(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Correcao Malformado", "correcao-malformado-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "correcao-malformado-almox@empresa.com", "senha-123456")
	produtoValido := seedProdutoComPendenciaHandler(t, db, "Tubo Correcao Malformado", services.CriarProdutoInput{})

	body := `{"correcoes":[` +
		`{"produtoId":"id-nao-e-um-uuid","campo":"comprimento","valorSugerido":{"valor":6,"unidade":"m"}},` +
		`{"produtoId":"` + produtoValido + `","campo":"comprimento","valorSugerido":{"valor":6,"unidade":"m"}}` +
		`]}`
	w := postCorrecoes(db, realtime.NewRegistry(), "Bearer "+token, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	var resp struct {
		Aplicadas []struct {
			ProdutoID string `json:"produtoId"`
			Campo     string `json:"campo"`
		} `json:"aplicadas"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Aplicadas) != 1 || resp.Aplicadas[0].ProdutoID != produtoValido {
		t.Fatalf("aplicadas = %+v, want só [{%s comprimento}]", resp.Aplicadas, produtoValido)
	}
}

// TestAplicarCorrecaoHandler_500FalhaDeBanco prova que, com a tabela
// `produtos` indisponível, um `almoxarife` autenticado recebe 500
// INTERNAL_ERROR — mesmo molde de TestAnalisarInconsistenciasHandler_500FalhaDeBanco.
func TestAplicarCorrecaoHandler_500FalhaDeBanco(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Correcao 500", "correcao-500-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "correcao-500-almox@empresa.com", "senha-123456")
	produtoID := seedProdutoComPendenciaHandler(t, db, "Tubo Correcao 500", services.CriarProdutoInput{})

	if _, err := db.Exec(`ALTER TABLE produtos RENAME TO produtos_indisponivel`); err != nil {
		t.Fatalf("renomear produtos: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`ALTER TABLE produtos_indisponivel RENAME TO produtos`); err != nil {
			t.Fatalf("restaurar produtos: %v", err)
		}
	})

	body := `{"correcoes":[{"produtoId":"` + produtoID + `","campo":"comprimento","valorSugerido":{"valor":6,"unidade":"m"}}]}`
	w := postCorrecoes(db, realtime.NewRegistry(), "Bearer "+token, body)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("code = %q, want INTERNAL_ERROR", env.Error.Code)
	}
}

// TestAplicarCorrecaoHandler_400CorpoAcimaDoLimite prova que
// normalizacaoCorrecoesRequestMaxBytes (1MB) é realmente aplicado: um corpo
// maior que o teto é rejeitado (payload inválido), não decodificado
// parcialmente nem aceito.
func TestAplicarCorrecaoHandler_400CorpoAcimaDoLimite(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Correcao Corpo Grande", "correcao-corpo-grande-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "correcao-corpo-grande-almox@empresa.com", "senha-123456")

	corpoGrande := `{"correcoes":[{"produtoId":"` + strings.Repeat("x", normalizacaoCorrecoesRequestMaxBytes+1) + `"}]}`
	w := postCorrecoes(db, realtime.NewRegistry(), "Bearer "+token, corpoGrande)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// --- POST /api/normalizacao/ignoradas: I/O Matrix de spec-6-2 -------------

func postIgnoradas(db *sql.DB, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/normalizacao/ignoradas",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				IgnorarSugestaoHandler(db))))
	r := httptest.NewRequest(http.MethodPost, "/api/normalizacao/ignoradas", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// TestSugestaoIgnoradaHandler_200GravaTupla prova a linha "Ignorar uma
// sugestão": 200, linha gravada em normalizacao_ignoradas.
func TestSugestaoIgnoradaHandler_200GravaTupla(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Ignorada 200", "ignorada-200-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "ignorada-200-almox@empresa.com", "senha-123456")
	produtoID := seedProdutoComPendenciaHandler(t, db, "Tubo Ignorada 200", services.CriarProdutoInput{})

	body := `{"produtoId":"` + produtoID + `","campo":"comprimento","valorSugerido":{"valor":6,"unidade":"m"}}`
	w := postIgnoradas(db, "Bearer "+token, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	var n int
	if err := db.QueryRow(
		`SELECT count(*) FROM normalizacao_ignoradas WHERE produto_id = $1 AND campo = 'comprimento' AND valor = 6 AND unidade = 'm'`,
		produtoID,
	).Scan(&n); err != nil {
		t.Fatalf("falha ao contar normalizacao_ignoradas: %v", err)
	}
	if n != 1 {
		t.Errorf("linhas em normalizacao_ignoradas = %d, want 1", n)
	}
}

// TestSugestaoIgnoradaHandler_200Idempotente prova a linha "Ignorar a mesma
// tupla duas vezes": sucesso idempotente, nenhum erro no reenvio.
func TestSugestaoIgnoradaHandler_200Idempotente(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Ignorada Idempotente", "ignorada-idempotente-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "ignorada-idempotente-almox@empresa.com", "senha-123456")
	produtoID := seedProdutoComPendenciaHandler(t, db, "Tubo Ignorada Idempotente", services.CriarProdutoInput{})

	body := `{"produtoId":"` + produtoID + `","campo":"comprimento","valorSugerido":{"valor":6,"unidade":"m"}}`
	w1 := postIgnoradas(db, "Bearer "+token, body)
	if w1.Code != http.StatusOK {
		t.Fatalf("1a chamada: status = %d, want 200 (body=%s)", w1.Code, w1.Body.String())
	}
	w2 := postIgnoradas(db, "Bearer "+token, body)
	if w2.Code != http.StatusOK {
		t.Fatalf("2a chamada (reenvio): status = %d, want 200 (body=%s)", w2.Code, w2.Body.String())
	}
}

// TestSugestaoIgnoradaHandler_400CampoInvalido prova que a mesma validação
// de campo/valor/unidade de AplicarCorrecoesHandler roda aqui.
func TestSugestaoIgnoradaHandler_400CampoInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Ignorada Campo Invalido", "ignorada-campo-invalido-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "ignorada-campo-invalido-almox@empresa.com", "senha-123456")
	produtoID := seedProdutoComPendenciaHandler(t, db, "Tubo Ignorada Campo Invalido", services.CriarProdutoInput{})

	body := `{"produtoId":"` + produtoID + `","campo":"peso","valorSugerido":{"valor":6,"unidade":"m"}}`
	w := postIgnoradas(db, "Bearer "+token, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// TestSugestaoIgnoradaHandler_404ProdutoInexistente prova o mapeamento de
// ErrProdutoNaoEncontrado para 404 NOT_FOUND.
func TestSugestaoIgnoradaHandler_404ProdutoInexistente(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Ignorada 404", "ignorada-404-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "ignorada-404-almox@empresa.com", "senha-123456")

	body := `{"produtoId":"00000000-0000-0000-0000-000000000000","campo":"comprimento","valorSugerido":{"valor":6,"unidade":"m"}}`
	w := postIgnoradas(db, "Bearer "+token, body)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
	}
}

// TestSugestaoIgnoradaHandler_403PapelUsuario prova a linha "Chamada com
// papel usuario": 403 FORBIDDEN, decidido por RequireRole.
func TestSugestaoIgnoradaHandler_403PapelUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Usuario Ignorada 403", "ignorada-403-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "ignorada-403-usuario@empresa.com", "senha-123456")

	body := `{"produtoId":"00000000-0000-0000-0000-000000000000","campo":"comprimento","valorSugerido":{"valor":6,"unidade":"m"}}`
	w := postIgnoradas(db, "Bearer "+token, body)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
}

// TestSugestaoIgnoradaHandler_401SemToken prova que uma requisição sem
// Authorization -> 401, produzido só por RequireAuth.
func TestSugestaoIgnoradaHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)

	body := `{"produtoId":"00000000-0000-0000-0000-000000000000","campo":"comprimento","valorSugerido":{"valor":6,"unidade":"m"}}`
	w := postIgnoradas(db, "", body)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
}

// TestSugestaoIgnoradaHandler_500FalhaDeBanco prova que, com a tabela
// `normalizacao_ignoradas` indisponível, um `almoxarife` autenticado recebe
// 500 INTERNAL_ERROR — mesmo molde de
// TestAnalisarInconsistenciasHandler_500FalhaDeBanco/
// TestAplicarCorrecaoHandler_500FalhaDeBanco, mas visando a tabela que
// IgnorarSugestao grava diretamente (produtos continua de pé — renomeá-la
// não quebraria a FK, que segue a tabela pelo OID, não pelo nome).
func TestSugestaoIgnoradaHandler_500FalhaDeBanco(t *testing.T) {
	db := testDB(t)
	limparProdutosHandler(t, db)
	criarContaComPapel(t, db, "Almox Ignorada 500", "ignorada-500-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "ignorada-500-almox@empresa.com", "senha-123456")
	produtoID := seedProdutoComPendenciaHandler(t, db, "Tubo Ignorada 500", services.CriarProdutoInput{})

	if _, err := db.Exec(`ALTER TABLE normalizacao_ignoradas RENAME TO normalizacao_ignoradas_indisponivel`); err != nil {
		t.Fatalf("renomear normalizacao_ignoradas: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`ALTER TABLE normalizacao_ignoradas_indisponivel RENAME TO normalizacao_ignoradas`); err != nil {
			t.Fatalf("restaurar normalizacao_ignoradas: %v", err)
		}
	})

	body := `{"produtoId":"` + produtoID + `","campo":"comprimento","valorSugerido":{"valor":6,"unidade":"m"}}`
	w := postIgnoradas(db, "Bearer "+token, body)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("code = %q, want INTERNAL_ERROR", env.Error.Code)
	}
}
