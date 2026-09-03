package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// --- despacho pela MESMA composição de newMux (main.go) --------------------
//
// POST /api/importacoes -> RequireAuth -> RequireRole(almoxarife) -> handler.
// POST /api/importacoes/{id}/continuar -> RequireAuth -> RequireRole(almoxarife) -> handler.
// GET /api/importacoes/ultima -> RequireAuth -> RequireRole(almoxarife) -> handler.

func limparImportacoesHandler(t *testing.T, db *sql.DB) {
	t.Helper()
	// `usuarios CASCADE` (testDB) já esvazia `importacoes`/`importacao_linhas`
	// (FK transitiva via criado_por) a cada teste — aqui só limpamos o que
	// `testDB` não alcança: produtos/estoques criados pela própria importação.
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, carrinho_itens, pedido_itens, pedidos, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("falha ao limpar produtos/estoques: %v", err)
	}
}

// construirXLSX monta um `.xlsx` real em memória (via excelize — mesma
// biblioteca de handlers/importacoes.go) a partir de uma matriz de células,
// linha a linha — usado para exercitar CriarImportacaoHandler com um arquivo
// de verdade, não um mock.
func construirXLSX(t *testing.T, linhas [][]string) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	planilha := f.GetSheetList()[0]
	for i, linha := range linhas {
		for j, valor := range linha {
			célula, err := excelize.CoordinatesToCellName(j+1, i+1)
			if err != nil {
				t.Fatalf("CoordinatesToCellName(%d,%d): %v", j+1, i+1, err)
			}
			if err := f.SetCellStr(planilha, célula, valor); err != nil {
				t.Fatalf("SetCellStr(%s): %v", célula, err)
			}
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("WriteToBuffer: %v", err)
	}
	return buf.Bytes()
}

// postImportacoes despacha POST /api/importacoes pela MESMA composição de
// newMux, com um corpo multipart real (campo `planilha`). `arquivo == nil`
// omite o campo por completo (simula o cliente não anexar nenhum arquivo).
func postImportacoes(db *sql.DB, authHeader string, arquivo []byte, nomeArquivo string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/importacoes",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				CriarImportacaoHandler(db))))

	corpo := &bytes.Buffer{}
	writer := multipart.NewWriter(corpo)
	if arquivo != nil {
		part, _ := writer.CreateFormFile("planilha", nomeArquivo)
		_, _ = part.Write(arquivo)
	}
	_ = writer.Close()

	r := httptest.NewRequest(http.MethodPost, "/api/importacoes", corpo)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func postContinuarImportacao(db *sql.DB, authHeader, id string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/importacoes/{id}/continuar",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				ContinuarImportacaoHandler(db))))
	r := httptest.NewRequest(http.MethodPost, "/api/importacoes/"+id+"/continuar", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func getUltimaImportacao(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/importacoes/ultima",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				UltimaImportacaoHandler(db))))
	r := httptest.NewRequest(http.MethodGet, "/api/importacoes/ultima", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func categoriaNomePorCodigoHandler(t *testing.T, db *sql.DB, codigo string) string {
	t.Helper()
	var nome string
	if err := db.QueryRow(`SELECT nome FROM categorias WHERE codigo = $1`, codigo).Scan(&nome); err != nil {
		t.Fatalf("falha ao buscar categoria %q: %v", codigo, err)
	}
	return nome
}

// linhaImportacao monta uma linha de dado (16 células, mesma ordem de
// services.CabecalhoEsperado) com dimensões/observações em branco.
func linhaImportacao(nome, codigo, categoria, quantidade, estoque string) []string {
	return []string{nome, codigo, categoria, "", "", "", "", "", "", "", "", "", "", quantidade, estoque, ""}
}

// TestCriarImportacaoHandler_201ParaAlmoxarife prova a AC1 na fronteira HTTP:
// uma sessão `almoxarife` enviando um `.xlsx` real com cabeçalho correto e
// uma linha válida recebe 201 com `{"importacao":...,"relatorio":...}`, e o
// Produto/Estoque são de fato criados no banco.
func TestCriarImportacaoHandler_201ParaAlmoxarife(t *testing.T) {
	db := testDB(t)
	limparImportacoesHandler(t, db)
	criarContaComPapel(t, db, "Almox Importação", "importacao-almox@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "importacao-almox@empresa.com", "senha-123456")
	categoria := categoriaNomePorCodigoHandler(t, db, "04.001")

	linhas := [][]string{
		services.CabecalhoEsperado,
		linhaImportacao("Produto HTTP Um", "SKU-HTTP-1", categoria, "2", "Canteiro Importação HTTP"),
	}
	xlsx := construirXLSX(t, linhas)

	w := postImportacoes(db, "Bearer "+token, xlsx, "planilha.xlsx")
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp struct {
		Importacao struct {
			ID                   string `json:"id"`
			Status               string `json:"status"`
			TotalLinhas          int    `json:"total_linhas"`
			ProximaLinhaPendente *int   `json:"proxima_linha_pendente"`
		} `json:"importacao"`
		Relatorio struct {
			Criados          int `json:"criados"`
			Rejeitados       int `json:"rejeitados"`
			LinhasRejeitadas []struct {
				Linha int    `json:"linha"`
				Erro  string `json:"erro"`
			} `json:"linhas_rejeitadas"`
		} `json:"relatorio"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Importacao.ID == "" {
		t.Error("importacao.id vazio")
	}
	if resp.Importacao.Status != "concluida" {
		t.Errorf("importacao.status = %q, want concluida", resp.Importacao.Status)
	}
	if resp.Importacao.TotalLinhas != 1 {
		t.Errorf("importacao.total_linhas = %d, want 1", resp.Importacao.TotalLinhas)
	}
	if resp.Importacao.ProximaLinhaPendente != nil {
		t.Errorf("importacao.proxima_linha_pendente = %v, want null (importação concluida)", *resp.Importacao.ProximaLinhaPendente)
	}
	if resp.Relatorio.Criados != 1 || resp.Relatorio.Rejeitados != 0 {
		t.Errorf("relatorio = %+v, want Criados=1 Rejeitados=0", resp.Relatorio)
	}

	var nProdutos int
	if err := db.QueryRow(`SELECT count(*) FROM produtos`).Scan(&nProdutos); err != nil {
		t.Fatalf("count produtos: %v", err)
	}
	if nProdutos != 1 {
		t.Errorf("linhas em produtos = %d, want 1", nProdutos)
	}
}

// TestCriarImportacaoHandler_403ParaUsuario prova a AC5: papel `usuario`
// chamando POST /api/importacoes direto -> 403 FORBIDDEN, nada gravado.
func TestCriarImportacaoHandler_403ParaUsuario(t *testing.T) {
	db := testDB(t)
	limparImportacoesHandler(t, db)
	criarContaComPapel(t, db, "Usuária Importação", "importacao-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "importacao-usuario@empresa.com", "senha-123456")
	categoria := categoriaNomePorCodigoHandler(t, db, "04.001")

	linhas := [][]string{
		services.CabecalhoEsperado,
		linhaImportacao("Produto Proibido", "SKU-PROIB", categoria, "1", "Canteiro Proibido"),
	}
	xlsx := construirXLSX(t, linhas)

	w := postImportacoes(db, "Bearer "+token, xlsx, "planilha.xlsx")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "FORBIDDEN" {
		t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM importacoes`).Scan(&n); err != nil {
		t.Fatalf("count importacoes: %v", err)
	}
	if n != 0 {
		t.Errorf("linhas em importacoes = %d, want 0 (handler não deve ter executado)", n)
	}
}

// TestCriarImportacaoHandler_400CabecalhoInvalido prova a AC2: cabeçalho fora
// do padrão (coluna faltando) -> 400 VALIDATION_ERROR citando as colunas
// esperadas, e NADA é gravado em importacoes/importacao_linhas.
func TestCriarImportacaoHandler_400CabecalhoInvalido(t *testing.T) {
	db := testDB(t)
	limparImportacoesHandler(t, db)
	criarContaComPapel(t, db, "Almox Cabeçalho", "importacao-cabecalho@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "importacao-cabecalho@empresa.com", "senha-123456")

	cabecalhoSemObservacoes := services.CabecalhoEsperado[:len(services.CabecalhoEsperado)-1]
	linhas := [][]string{
		cabecalhoSemObservacoes,
		{"Produto Sem Observacoes", "SKU-X", "Materiais Civis", "", "", "", "", "", "", "", "", "", "", "1", "Canteiro X"},
	}
	xlsx := construirXLSX(t, linhas)

	w := postImportacoes(db, "Bearer "+token, xlsx, "planilha.xlsx")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
	if !bytesContainsAll(env.Error.Message, "Nome", "Código", "Observações") {
		t.Errorf("message = %q, want citar as colunas esperadas", env.Error.Message)
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM importacoes`).Scan(&n); err != nil {
		t.Fatalf("count importacoes: %v", err)
	}
	if n != 0 {
		t.Errorf("linhas em importacoes = %d, want 0 (nada deveria ser gravado)", n)
	}
}

// TestCriarImportacaoHandler_400ArquivoNaoXLSX prova a I/O Matrix "arquivo
// que não abre no excelize": bytes que não são um `.xlsx` de verdade ->
// 400 VALIDATION_ERROR, nada gravado.
func TestCriarImportacaoHandler_400ArquivoNaoXLSX(t *testing.T) {
	db := testDB(t)
	limparImportacoesHandler(t, db)
	criarContaComPapel(t, db, "Almox Arquivo Ruim", "importacao-arquivoruim@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "importacao-arquivoruim@empresa.com", "senha-123456")

	w := postImportacoes(db, "Bearer "+token, []byte("isto não é uma planilha"), "planilha.xlsx")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

// TestCriarImportacaoHandler_400SemArquivo prova o caso "campo `planilha`
// ausente" -> 400 VALIDATION_ERROR (r.FormFile devolve erro).
func TestCriarImportacaoHandler_400SemArquivo(t *testing.T) {
	db := testDB(t)
	limparImportacoesHandler(t, db)
	criarContaComPapel(t, db, "Almox Sem Arquivo", "importacao-semarquivo@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "importacao-semarquivo@empresa.com", "senha-123456")

	w := postImportacoes(db, "Bearer "+token, nil, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// TestCriarImportacaoHandler_400NomeArquivoMuitoLongo prova a correção da
// review pass (2026-08-31): um nome de arquivo com mais de 255 caracteres
// estourava `importacoes.nome_arquivo VARCHAR(255)` no INSERT e virava um
// 500 genérico — agora é validado ANTES do handler chamar services, virando
// 400 VALIDATION_ERROR limpo, sem nada gravado.
func TestCriarImportacaoHandler_400NomeArquivoMuitoLongo(t *testing.T) {
	db := testDB(t)
	limparImportacoesHandler(t, db)
	criarContaComPapel(t, db, "Almox Nome Longo", "importacao-nomelongo@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "importacao-nomelongo@empresa.com", "senha-123456")
	categoria := categoriaNomePorCodigoHandler(t, db, "04.001")

	linhas := [][]string{
		services.CabecalhoEsperado,
		linhaImportacao("Produto Nome Longo", "SKU-NL", categoria, "1", "Canteiro Nome Longo"),
	}
	xlsx := construirXLSX(t, linhas)
	nomeArquivoMuitoLongo := strings.Repeat("a", 256) + ".xlsx" // 261 runes, > 255

	w := postImportacoes(db, "Bearer "+token, xlsx, nomeArquivoMuitoLongo)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM importacoes`).Scan(&n); err != nil {
		t.Fatalf("count importacoes: %v", err)
	}
	if n != 0 {
		t.Errorf("linhas em importacoes = %d, want 0 (nada deveria ser gravado)", n)
	}
}

// TestCriarImportacaoHandler_400ArquivoMuitoGrande prova a correção da review
// pass (2026-08-31): um corpo acima de importacaoRequestMaxBytes (que faz
// ParseMultipartForm falhar via http.MaxBytesReader/*http.MaxBytesError)
// recebe uma mensagem DISTINTA de "arquivo não é uma planilha .xlsx válida"
// — um arquivo grande demais não é a mesma coisa que um arquivo corrompido,
// e confundir os dois levaria quem enviou a tentar corrigir o problema
// errado.
func TestCriarImportacaoHandler_400ArquivoMuitoGrande(t *testing.T) {
	db := testDB(t)
	limparImportacoesHandler(t, db)
	criarContaComPapel(t, db, "Almox Arquivo Grande", "importacao-grande@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "importacao-grande@empresa.com", "senha-123456")

	// Conteúdo puro já maior que o limite — a codificação multipart (boundary
	// + cabeçalhos) só soma bytes, garantindo estourar importacaoRequestMaxBytes.
	arquivoGrande := bytes.Repeat([]byte("a"), importacaoRequestMaxBytes+1024)

	w := postImportacoes(db, "Bearer "+token, arquivoGrande, "planilha-grande.xlsx")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
	if env.Error.Message != mensagemArquivoMuitoGrande {
		t.Errorf("message = %q, want %q (distinta da mensagem de arquivo inválido genérico)",
			env.Error.Message, mensagemArquivoMuitoGrande)
	}
	if env.Error.Message == mensagemArquivoInvalido {
		t.Error("message igual à de arquivo corrompido/não-xlsx — diagnóstico enganoso")
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM importacoes`).Scan(&n); err != nil {
		t.Fatalf("count importacoes: %v", err)
	}
	if n != 0 {
		t.Errorf("linhas em importacoes = %d, want 0 (nada deveria ser gravado)", n)
	}
}

// TestCriarImportacaoHandler_401SemToken prova a linha "sem autenticação" da
// I/O Matrix: RequireAuth responde 401 antes de RequireRole rodar.
func TestCriarImportacaoHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	limparImportacoesHandler(t, db)
	w := postImportacoes(db, "", []byte("qualquer coisa"), "planilha.xlsx")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestContinuarImportacaoHandler_200Sucesso prova que uma sessão `almoxarife`
// retoma uma importação `em_andamento` existente com sucesso.
func TestContinuarImportacaoHandler_200Sucesso(t *testing.T) {
	db := testDB(t)
	limparImportacoesHandler(t, db)
	criarContaComPapel(t, db, "Almox Continuar", "importacao-continuar-http@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "importacao-continuar-http@empresa.com", "senha-123456")
	categoria := categoriaNomePorCodigoHandler(t, db, "04.002")

	usuarioID := usuarioIDPorEmail(t, db, "importacao-continuar-http@empresa.com")
	importacao, _, err := services.CriarImportacao(db, usuarioID, "planilha.xlsx", [][]string{
		services.CabecalhoEsperado,
		linhaImportacao("Produto Continuar HTTP", "SKU-CONT-HTTP", categoria, "1", "Canteiro Continuar HTTP"),
	})
	if err != nil {
		t.Fatalf("seed CriarImportacao: %v", err)
	}
	// A importação seed já terminou 'concluida' (processamento síncrono) —
	// forçamos de volta a em_andamento para exercitar o endpoint de retomada
	// isoladamente, sem depender de simular uma interrupção real.
	if _, err := db.Exec(`UPDATE importacoes SET status = 'em_andamento' WHERE id = $1`, importacao.ID); err != nil {
		t.Fatalf("forçar em_andamento: %v", err)
	}

	w := postContinuarImportacao(db, "Bearer "+token, importacao.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestContinuarImportacaoHandler_404IDInexistente prova a AC4: `id`
// inexistente (UUID válido, sem linha) -> 404 NOT_FOUND.
func TestContinuarImportacaoHandler_404IDInexistente(t *testing.T) {
	db := testDB(t)
	limparImportacoesHandler(t, db)
	criarContaComPapel(t, db, "Almox 404", "importacao-404@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "importacao-404@empresa.com", "senha-123456")

	w := postContinuarImportacao(db, "Bearer "+token, "00000000-0000-0000-0000-000000000000")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
	}
}

// TestContinuarImportacaoHandler_403ParaUsuario prova o gate de papel do
// endpoint de retomada.
func TestContinuarImportacaoHandler_403ParaUsuario(t *testing.T) {
	db := testDB(t)
	limparImportacoesHandler(t, db)
	criarContaComPapel(t, db, "Usuária Continuar", "importacao-continuar-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "importacao-continuar-usuario@empresa.com", "senha-123456")

	w := postContinuarImportacao(db, "Bearer "+token, "00000000-0000-0000-0000-000000000000")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
}

// TestUltimaImportacaoHandler_200SemImportacao prova que, sem nenhuma
// importação registrada, o endpoint devolve `200 {"importacao": null}`.
func TestUltimaImportacaoHandler_200SemImportacao(t *testing.T) {
	db := testDB(t)
	limparImportacoesHandler(t, db)
	criarContaComPapel(t, db, "Almox Última", "importacao-ultima-http@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "importacao-ultima-http@empresa.com", "senha-123456")

	w := getUltimaImportacao(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		Importacao *struct{} `json:"importacao"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Importacao != nil {
		t.Errorf("importacao = %+v, want null", resp.Importacao)
	}
}

// TestUltimaImportacaoHandler_ProximaLinhaPendente prova, na fronteira HTTP,
// que o campo `proxima_linha_pendente` (review pass 2026-08-31) reflete o
// número REAL da linha pendente — não `criados+rejeitados` — para o banner
// de retomada do frontend.
func TestUltimaImportacaoHandler_ProximaLinhaPendente(t *testing.T) {
	db := testDB(t)
	limparImportacoesHandler(t, db)
	criarContaComPapel(t, db, "Almox Proxima Linha", "importacao-proximalinha-http@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "importacao-proximalinha-http@empresa.com", "senha-123456")
	categoria := categoriaNomePorCodigoHandler(t, db, "04.003")

	usuarioID := usuarioIDPorEmail(t, db, "importacao-proximalinha-http@empresa.com")
	importacao, _, err := services.CriarImportacao(db, usuarioID, "planilha.xlsx", [][]string{
		services.CabecalhoEsperado,
		linhaImportacao("Produto Proxima Um", "SKU-PLH-1", categoria, "1", "Canteiro Proxima HTTP"),
		linhaImportacao("Produto Proxima Dois", "SKU-PLH-2", categoria, "1", "Canteiro Proxima HTTP"),
	})
	if err != nil {
		t.Fatalf("seed CriarImportacao: %v", err)
	}
	// Simula interrupção: a 2ª linha de dado (numero_linha 3) volta a
	// pendente; a importação volta a em_andamento.
	if _, err := db.Exec(
		`UPDATE importacao_linhas SET status = 'pendente' WHERE importacao_id = $1 AND numero_linha = 3`, importacao.ID,
	); err != nil {
		t.Fatalf("resetar linha 3 para pendente: %v", err)
	}
	if _, err := db.Exec(`UPDATE importacoes SET status = 'em_andamento' WHERE id = $1`, importacao.ID); err != nil {
		t.Fatalf("resetar importação para em_andamento: %v", err)
	}

	w := getUltimaImportacao(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		Importacao struct {
			Status               string `json:"status"`
			ProximaLinhaPendente *int   `json:"proxima_linha_pendente"`
		} `json:"importacao"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Importacao.Status != "em_andamento" {
		t.Fatalf("status = %q, want em_andamento", resp.Importacao.Status)
	}
	if resp.Importacao.ProximaLinhaPendente == nil {
		t.Fatal("proxima_linha_pendente = null, want 3")
	}
	if *resp.Importacao.ProximaLinhaPendente != 3 {
		t.Errorf("proxima_linha_pendente = %d, want 3", *resp.Importacao.ProximaLinhaPendente)
	}
}

// TestUltimaImportacaoHandler_403ParaUsuario prova o gate de papel do
// endpoint de última importação — mesma restrição do módulo (não é
// visualização de catálogo).
func TestUltimaImportacaoHandler_403ParaUsuario(t *testing.T) {
	db := testDB(t)
	limparImportacoesHandler(t, db)
	criarContaComPapel(t, db, "Usuária Última", "importacao-ultima-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "importacao-ultima-usuario@empresa.com", "senha-123456")

	w := getUltimaImportacao(db, "Bearer "+token)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func usuarioIDPorEmail(t *testing.T, db *sql.DB, email string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`SELECT id FROM usuarios WHERE lower(email) = lower($1)`, email).Scan(&id); err != nil {
		t.Fatalf("falha ao buscar usuario %q: %v", email, err)
	}
	return id
}

func bytesContainsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !bytes.Contains([]byte(s), []byte(sub)) {
			return false
		}
	}
	return true
}
