package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// --- despacho pela MESMA composição de newMux (main.go) --------------------
//
// POST /api/produtos/{id}/fotos -> RequireAuth -> RequireRole(almoxarife) -> handler.
// GET  /api/produtos/{id}/fotos/{arquivo} -> RequireAuth -> handler.

func limparProdutosFotos(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("falha ao limpar produtos/estoques: %v", err)
	}
}

// criarProdutoParaFotoHandler cria um Produto mínimo (categoria/estoque de
// seed) via services.CriarProduto — só para servir de `{id}` válido nos
// testes de fronteira HTTP desta suíte.
func criarProdutoParaFotoHandler(t *testing.T, db *sql.DB, nome string) string {
	t.Helper()
	estoque, err := services.CriarEstoque(db, "Canteiro Foto HTTP "+nome)
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	var categoriaID string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE codigo = $1`, "04.001").Scan(&categoriaID); err != nil {
		t.Fatalf("seed categoria: %v", err)
	}
	p, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome:              nome,
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}
	return p.ID
}

// postFotoProduto despacha POST /api/produtos/{id}/fotos pela MESMA
// composição de newMux, com um corpo multipart real (campo `foto`).
// `conteudo == nil` omite o campo por completo (simula o cliente não anexar
// nenhum arquivo).
func postFotoProduto(db *sql.DB, fotosDir, authHeader, produtoID string, conteudo []byte, nomeArquivo string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/produtos/{id}/fotos",
		middleware.RequireAuth(db, testJWTSecret)(
			middleware.RequireRole(services.PapelAlmoxarife)(
				EnviarFotoProdutoHandler(db, fotosDir))))

	corpo := &bytes.Buffer{}
	writer := multipart.NewWriter(corpo)
	if conteudo != nil {
		part, _ := writer.CreateFormFile("foto", nomeArquivo)
		_, _ = part.Write(conteudo)
	}
	_ = writer.Close()

	r := httptest.NewRequest(http.MethodPost, "/api/produtos/"+produtoID+"/fotos", corpo)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// getFotoProduto despacha GET /api/produtos/{id}/fotos/{arquivo} pela MESMA
// composição de newMux (só RequireAuth, sem RequireRole).
func getFotoProduto(db *sql.DB, fotosDir, authHeader, produtoID, arquivo string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/produtos/{id}/fotos/{arquivo}",
		middleware.RequireAuth(db, testJWTSecret)(
			ServirFotoProdutoHandler(fotosDir)))

	r := httptest.NewRequest(http.MethodGet, "/api/produtos/"+produtoID+"/fotos/"+arquivo, nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// listarFotosProduto despacha GET /api/produtos/{id}/fotos pela MESMA
// composição de newMux (só RequireAuth, sem RequireRole — Story 3.6).
func listarFotosProduto(db *sql.DB, fotosDir, authHeader, produtoID string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/produtos/{id}/fotos",
		middleware.RequireAuth(db, testJWTSecret)(
			ListarFotosProdutoHandler(db, fotosDir)))

	r := httptest.NewRequest(http.MethodGet, "/api/produtos/"+produtoID+"/fotos", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// construirJPEG monta um JPEG real em memória, largura x altura, cor sólida —
// usado para exercitar EnviarFotoProdutoHandler com um arquivo de verdade,
// não um mock.
func construirJPEG(t *testing.T, largura, altura int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, largura, altura))
	for y := 0; y < altura; y++ {
		for x := 0; x < largura; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

// construirPNG monta um PNG real em memória, largura x altura, cor sólida.
func construirPNG(t *testing.T, largura, altura int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, largura, altura))
	for y := 0; y < altura; y++ {
		for x := 0; x < largura; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 200, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// lerWebpPequeno devolve os bytes de um WEBP real (75x100, abaixo do teto de
// 500px), fixture copiada de golang.org/x/image/testdata — usado para provar
// que o decoder WEBP registrado (import em branco) de fato decodifica esse
// formato pelo conteúdo, sem depender de extensão/Content-Type.
func lerWebpPequeno(t *testing.T) []byte {
	t.Helper()
	dados, err := os.ReadFile("testdata/foto-pequena.webp")
	if err != nil {
		t.Fatalf("falha ao ler fixture webp: %v", err)
	}
	return dados
}

// decodeDimensoes lê `Content-Type: image/jpeg` + corpo de uma resposta de
// GET /api/produtos/{id}/fotos/{arquivo} e devolve as dimensões decodificadas
// — usado para provar o resize (ou a ausência dele) na ponta a ponta.
func decodeDimensoes(t *testing.T, jpegBytes []byte) (largura, altura int) {
	t.Helper()
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(jpegBytes))
	if err != nil {
		t.Fatalf("jpeg.DecodeConfig: %v (a resposta deveria sempre ser um JPEG válido)", err)
	}
	return cfg.Width, cfg.Height
}

// TestEnviarFotoProdutoHandler_JPEGGrandeRedimensiona prova a AC1: um JPEG
// 2000x1000 é salvo redimensionado a 500px no maior lado (proporção 2:1
// preservada, 500x250), sempre recomprimido em JPEG.
func TestEnviarFotoProdutoHandler_JPEGGrandeRedimensiona(t *testing.T) {
	db := testDB(t)
	limparProdutosFotos(t, db)
	fotosDir := t.TempDir()
	criarContaComPapel(t, db, "Almox Foto Um", "foto-almox-1@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "foto-almox-1@empresa.com", "senha-123456")
	produtoID := criarProdutoParaFotoHandler(t, db, "Produto Foto JPEG")

	jpegGrande := construirJPEG(t, 2000, 1000)

	w := postFotoProduto(db, fotosDir, "Bearer "+token, produtoID, jpegGrande, "foto.jpg")
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}

	var resp struct {
		Foto struct {
			Nome string `json:"nome"`
			URL  string `json:"url"`
		} `json:"foto"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp.Foto.Nome == "" || resp.Foto.URL == "" {
		t.Fatalf("resposta incompleta: %+v", resp.Foto)
	}
	wantURL := "/api/produtos/" + produtoID + "/fotos/" + resp.Foto.Nome
	if resp.Foto.URL != wantURL {
		t.Errorf("url = %q, want %q", resp.Foto.URL, wantURL)
	}

	// Rebusca via GET (fronteira completa) e confirma o resize + recompressão.
	wGet := getFotoProduto(db, fotosDir, "Bearer "+token, produtoID, resp.Foto.Nome)
	if wGet.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body=%s)", wGet.Code, wGet.Body.String())
	}
	if ct := wGet.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
	largura, altura := decodeDimensoes(t, wGet.Body.Bytes())
	if largura != 500 || altura != 250 {
		t.Errorf("dimensões = %dx%d, want 500x250 (maior lado no teto de 500px, proporção 2:1 preservada)", largura, altura)
	}
}

// TestEnviarFotoProdutoHandler_PNGPequenoSemUpscale prova a AC2: um PNG
// 300x200 (abaixo do teto de 500px) é preservado nas dimensões originais —
// nenhum upscale — e recomprimido em JPEG.
func TestEnviarFotoProdutoHandler_PNGPequenoSemUpscale(t *testing.T) {
	db := testDB(t)
	limparProdutosFotos(t, db)
	fotosDir := t.TempDir()
	criarContaComPapel(t, db, "Almox Foto Dois", "foto-almox-2@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "foto-almox-2@empresa.com", "senha-123456")
	produtoID := criarProdutoParaFotoHandler(t, db, "Produto Foto PNG")

	pngPequeno := construirPNG(t, 300, 200)

	w := postFotoProduto(db, fotosDir, "Bearer "+token, produtoID, pngPequeno, "foto.png")
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Foto struct {
			Nome string `json:"nome"`
		} `json:"foto"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	wGet := getFotoProduto(db, fotosDir, "Bearer "+token, produtoID, resp.Foto.Nome)
	if wGet.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body=%s)", wGet.Code, wGet.Body.String())
	}
	largura, altura := decodeDimensoes(t, wGet.Body.Bytes())
	if largura != 300 || altura != 200 {
		t.Errorf("dimensões = %dx%d, want 300x200 (sem upscale)", largura, altura)
	}
}

// TestEnviarFotoProdutoHandler_WEBPDecodificaPeloConteudo prova que o
// decoder WEBP (golang.org/x/image/webp, import em branco) decodifica o
// conteúdo real do arquivo — extensão/Content-Type do multipart nunca
// entram na decisão de formato.
func TestEnviarFotoProdutoHandler_WEBPDecodificaPeloConteudo(t *testing.T) {
	db := testDB(t)
	limparProdutosFotos(t, db)
	fotosDir := t.TempDir()
	criarContaComPapel(t, db, "Almox Foto WEBP", "foto-almox-webp@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "foto-almox-webp@empresa.com", "senha-123456")
	produtoID := criarProdutoParaFotoHandler(t, db, "Produto Foto WEBP")

	w := postFotoProduto(db, fotosDir, "Bearer "+token, produtoID, lerWebpPequeno(t), "foto.webp")
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
}

// TestEnviarFotoProdutoHandler_400ArquivoMuitoGrande prova o limite de
// fotoRequestMaxBytes (15 MiB): corpo acima do limite -> 400 VALIDATION_ERROR
// com mensagemFotoTamanho, DISTINTA de mensagemFotoFormato, nenhum arquivo
// salvo.
func TestEnviarFotoProdutoHandler_400ArquivoMuitoGrande(t *testing.T) {
	db := testDB(t)
	limparProdutosFotos(t, db)
	fotosDir := t.TempDir()
	criarContaComPapel(t, db, "Almox Foto Grande", "foto-almox-grande@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "foto-almox-grande@empresa.com", "senha-123456")
	produtoID := criarProdutoParaFotoHandler(t, db, "Produto Foto Grande")

	// Conteúdo puro já maior que o limite — a codificação multipart (boundary
	// + cabeçalhos) só soma bytes, garantindo estourar fotoRequestMaxBytes.
	arquivoGrande := bytes.Repeat([]byte("a"), fotoRequestMaxBytes+1024)

	w := postFotoProduto(db, fotosDir, "Bearer "+token, produtoID, arquivoGrande, "foto-grande.jpg")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
	if env.Error.Message != mensagemFotoTamanho {
		t.Errorf("message = %q, want %q", env.Error.Message, mensagemFotoTamanho)
	}
	if env.Error.Message == mensagemFotoFormato {
		t.Error("message igual à de formato inválido — diagnóstico enganoso")
	}

	entradas, err := os.ReadDir(fotosDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entradas) != 0 {
		t.Errorf("fotosDir deveria estar vazio, tem %d entradas", len(entradas))
	}
}

// TestEnviarFotoProdutoHandler_400FormatoInvalido prova que um arquivo cujo
// conteúdo não decodifica como JPEG/PNG/WEBP (aqui, um PDF renomeado `.jpg`)
// -> 400 VALIDATION_ERROR com mensagemFotoFormato, DISTINTA de
// mensagemFotoTamanho, mesmo com extensão/Content-Type de imagem.
func TestEnviarFotoProdutoHandler_400FormatoInvalido(t *testing.T) {
	db := testDB(t)
	limparProdutosFotos(t, db)
	fotosDir := t.TempDir()
	criarContaComPapel(t, db, "Almox Foto Formato", "foto-almox-formato@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "foto-almox-formato@empresa.com", "senha-123456")
	produtoID := criarProdutoParaFotoHandler(t, db, "Produto Foto Formato")

	conteudoNaoImagem := []byte("%PDF-1.4 este arquivo não é uma imagem de verdade")

	w := postFotoProduto(db, fotosDir, "Bearer "+token, produtoID, conteudoNaoImagem, "documento.jpg")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Message != mensagemFotoFormato {
		t.Errorf("message = %q, want %q", env.Error.Message, mensagemFotoFormato)
	}
	if env.Error.Message == mensagemFotoTamanho {
		t.Error("message igual à de tamanho excedido — diagnóstico enganoso")
	}

	entradas, err := os.ReadDir(fotosDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entradas) != 0 {
		t.Errorf("fotosDir deveria estar vazio, tem %d entradas", len(entradas))
	}
}

// TestEnviarFotoProdutoHandler_SegundoUploadNaoSobrescreve prova a AC de
// "foto anterior permanece intacta": dois uploads para o MESMO Produto geram
// dois nomes de arquivo distintos, os dois recuperáveis via GET.
func TestEnviarFotoProdutoHandler_SegundoUploadNaoSobrescreve(t *testing.T) {
	db := testDB(t)
	limparProdutosFotos(t, db)
	fotosDir := t.TempDir()
	criarContaComPapel(t, db, "Almox Foto Reenvio", "foto-almox-reenvio@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "foto-almox-reenvio@empresa.com", "senha-123456")
	produtoID := criarProdutoParaFotoHandler(t, db, "Produto Foto Reenvio")

	w1 := postFotoProduto(db, fotosDir, "Bearer "+token, produtoID, construirJPEG(t, 100, 100), "foto1.jpg")
	if w1.Code != http.StatusCreated {
		t.Fatalf("primeiro upload: status = %d, want 201 (body=%s)", w1.Code, w1.Body.String())
	}
	w2 := postFotoProduto(db, fotosDir, "Bearer "+token, produtoID, construirJPEG(t, 100, 100), "foto2.jpg")
	if w2.Code != http.StatusCreated {
		t.Fatalf("segundo upload: status = %d, want 201 (body=%s)", w2.Code, w2.Body.String())
	}

	var r1, r2 struct {
		Foto struct {
			Nome string `json:"nome"`
		} `json:"foto"`
	}
	_ = json.Unmarshal(w1.Body.Bytes(), &r1)
	_ = json.Unmarshal(w2.Body.Bytes(), &r2)
	if r1.Foto.Nome == r2.Foto.Nome {
		t.Fatalf("os dois uploads geraram o MESMO nome de arquivo: %q", r1.Foto.Nome)
	}

	if wGet1 := getFotoProduto(db, fotosDir, "Bearer "+token, produtoID, r1.Foto.Nome); wGet1.Code != http.StatusOK {
		t.Errorf("GET da primeira foto: status = %d, want 200", wGet1.Code)
	}
	if wGet2 := getFotoProduto(db, fotosDir, "Bearer "+token, produtoID, r2.Foto.Nome); wGet2.Code != http.StatusOK {
		t.Errorf("GET da segunda foto: status = %d, want 200", wGet2.Code)
	}
}

// TestEnviarFotoProdutoHandler_403ParaUsuario prova a AC5: papel `usuario`
// chamando POST /api/produtos/{id}/fotos direto -> 403 FORBIDDEN, nada gravado
// (o handler nunca executa — decidido inteiramente por RequireRole).
func TestEnviarFotoProdutoHandler_403ParaUsuario(t *testing.T) {
	db := testDB(t)
	limparProdutosFotos(t, db)
	fotosDir := t.TempDir()
	criarContaComPapel(t, db, "Usuária Foto", "foto-usuario@empresa.com", "senha-123456", "usuario")
	token := tokenDeLogin(t, db, "foto-usuario@empresa.com", "senha-123456")
	produtoID := criarProdutoParaFotoHandler(t, db, "Produto Foto Proibido")

	w := postFotoProduto(db, fotosDir, "Bearer "+token, produtoID, construirJPEG(t, 100, 100), "foto.jpg")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}

	entradas, err := os.ReadDir(fotosDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entradas) != 0 {
		t.Errorf("fotosDir deveria estar vazio, tem %d entradas", len(entradas))
	}
}

// TestEnviarFotoProdutoHandler_404ProdutoInexistenteOuMalformado prova que
// tanto um UUID sintaticamente válido sem linha correspondente quanto um
// `id` malformado (não-UUID) -> 404 NOT_FOUND, nenhum arquivo salvo.
func TestEnviarFotoProdutoHandler_404ProdutoInexistenteOuMalformado(t *testing.T) {
	db := testDB(t)
	limparProdutosFotos(t, db)
	criarContaComPapel(t, db, "Almox Foto 404", "foto-almox-404@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "foto-almox-404@empresa.com", "senha-123456")

	casos := []struct {
		nome      string
		produtoID string
	}{
		{"UUID válido inexistente", "00000000-0000-0000-0000-000000000000"},
		{"id malformado", "id-nao-e-uuid"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			fotosDir := t.TempDir()
			w := postFotoProduto(db, fotosDir, "Bearer "+token, c.produtoID, construirJPEG(t, 100, 100), "foto.jpg")
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
			}
			entradas, err := os.ReadDir(fotosDir)
			if err != nil {
				t.Fatalf("ReadDir: %v", err)
			}
			if len(entradas) != 0 {
				t.Errorf("fotosDir deveria estar vazio, tem %d entradas", len(entradas))
			}
		})
	}
}

// TestServirFotoProdutoHandler_404NomeNaoBateNoPatraoDoID prova que
// `{arquivo}` fora do padrão `^<id>-\d+\.jpg$` do `{id}` da URL -> 404
// NOT_FOUND, sem nenhuma leitura de disco fora de fotosDir — mesmo com um
// arquivo de MESMO NOME existindo fisicamente dentro de fotosDir (só não sob
// o prefixo esperado para este produto).
func TestServirFotoProdutoHandler_404NomeNaoBateNoPatraoDoID(t *testing.T) {
	db := testDB(t)
	limparProdutosFotos(t, db)
	fotosDir := t.TempDir()
	criarContaComPapel(t, db, "Almox Foto Serve", "foto-almox-serve@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "foto-almox-serve@empresa.com", "senha-123456")
	produtoID := criarProdutoParaFotoHandler(t, db, "Produto Foto Serve")

	if err := os.WriteFile(fotosDir+"/outro-arquivo.jpg", []byte("nao-deveria-ser-lido"), 0o644); err != nil {
		t.Fatalf("falha ao criar arquivo de outro produto: %v", err)
	}

	w := getFotoProduto(db, fotosDir, "Bearer "+token, produtoID, "outro-arquivo.jpg")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
}

// TestServirFotoProdutoHandler_QualquerPapelAutenticado prova que a
// visualização é liberada a QUALQUER papel autenticado (`usuario` incluso) —
// sem RequireRole, ao contrário do upload.
func TestServirFotoProdutoHandler_QualquerPapelAutenticado(t *testing.T) {
	db := testDB(t)
	limparProdutosFotos(t, db)
	fotosDir := t.TempDir()
	criarContaComPapel(t, db, "Almox Foto View", "foto-almox-view@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "foto-almox-view@empresa.com", "senha-123456")
	produtoID := criarProdutoParaFotoHandler(t, db, "Produto Foto View")

	w := postFotoProduto(db, fotosDir, "Bearer "+tokenAlmox, produtoID, construirJPEG(t, 100, 100), "foto.jpg")
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Foto struct {
			Nome string `json:"nome"`
		} `json:"foto"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	criarContaComPapel(t, db, "Usuária Foto View", "foto-usuario-view@empresa.com", "senha-123456", "usuario")
	tokenUsuario := tokenDeLogin(t, db, "foto-usuario-view@empresa.com", "senha-123456")

	wGet := getFotoProduto(db, fotosDir, "Bearer "+tokenUsuario, produtoID, resp.Foto.Nome)
	if wGet.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body=%s) — visualização deveria ser liberada a qualquer papel", wGet.Code, wGet.Body.String())
	}
}

// TestListarFotosProdutoHandler_SucessoComNFotos prova a AC1 (Story 3.6,
// spec-3-6): 2 uploads bem-sucedidos para o mesmo Produto -> GET
// /api/produtos/{id}/fotos devolve `200 {"fotos":[...]}` com as 2 entradas.
func TestListarFotosProdutoHandler_SucessoComNFotos(t *testing.T) {
	db := testDB(t)
	limparProdutosFotos(t, db)
	fotosDir := t.TempDir()
	criarContaComPapel(t, db, "Almox Foto Galeria", "foto-almox-galeria@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "foto-almox-galeria@empresa.com", "senha-123456")
	produtoID := criarProdutoParaFotoHandler(t, db, "Produto Foto Galeria")

	w1 := postFotoProduto(db, fotosDir, "Bearer "+token, produtoID, construirJPEG(t, 100, 100), "foto1.jpg")
	if w1.Code != http.StatusCreated {
		t.Fatalf("primeiro upload: status = %d, want 201 (body=%s)", w1.Code, w1.Body.String())
	}
	w2 := postFotoProduto(db, fotosDir, "Bearer "+token, produtoID, construirJPEG(t, 100, 100), "foto2.jpg")
	if w2.Code != http.StatusCreated {
		t.Fatalf("segundo upload: status = %d, want 201 (body=%s)", w2.Code, w2.Body.String())
	}

	w := listarFotosProduto(db, fotosDir, "Bearer "+token, produtoID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Fotos []struct {
			Nome string `json:"nome"`
			URL  string `json:"url"`
		} `json:"fotos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Fotos) != 2 {
		t.Fatalf("len(fotos) = %d, want 2", len(resp.Fotos))
	}
}

// TestListarFotosProdutoHandler_GaleriaVazia prova a AC4: Produto existente
// sem nenhuma foto -> `200 {"fotos":[]}`, nunca erro.
func TestListarFotosProdutoHandler_GaleriaVazia(t *testing.T) {
	db := testDB(t)
	limparProdutosFotos(t, db)
	fotosDir := t.TempDir()
	criarContaComPapel(t, db, "Almox Foto Vazia", "foto-almox-vazia@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "foto-almox-vazia@empresa.com", "senha-123456")
	produtoID := criarProdutoParaFotoHandler(t, db, "Produto Foto Vazia")

	w := listarFotosProduto(db, fotosDir, "Bearer "+token, produtoID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"fotos":[]`)) {
		t.Errorf("body = %s, want array vazio explícito em \"fotos\"", w.Body.String())
	}
}

// TestListarFotosProdutoHandler_404ProdutoInexistenteOuMalformado prova que
// `id` inexistente OU malformado -> 404 NOT_FOUND, mesmo tratamento das
// demais rotas de Produto.
func TestListarFotosProdutoHandler_404ProdutoInexistenteOuMalformado(t *testing.T) {
	db := testDB(t)
	limparProdutosFotos(t, db)
	fotosDir := t.TempDir()
	criarContaComPapel(t, db, "Almox Foto 404 Galeria", "foto-almox-404-galeria@empresa.com", "senha-123456", "almoxarife")
	token := tokenDeLogin(t, db, "foto-almox-404-galeria@empresa.com", "senha-123456")

	casos := []struct {
		nome      string
		produtoID string
	}{
		{"UUID válido inexistente", "00000000-0000-0000-0000-000000000000"},
		{"id malformado", "id-nao-e-uuid"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w := listarFotosProduto(db, fotosDir, "Bearer "+token, c.produtoID)
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
			}
		})
	}
}

// TestListarFotosProdutoHandler_QualquerPapelAutenticado prova que a
// listagem é liberada a QUALQUER papel autenticado (`usuario` incluso) —
// sem RequireRole, mesmo padrão de ServirFotoProdutoHandler.
func TestListarFotosProdutoHandler_QualquerPapelAutenticado(t *testing.T) {
	db := testDB(t)
	limparProdutosFotos(t, db)
	fotosDir := t.TempDir()
	criarContaComPapel(t, db, "Almox Foto Galeria View", "foto-almox-galeria-view@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "foto-almox-galeria-view@empresa.com", "senha-123456")
	produtoID := criarProdutoParaFotoHandler(t, db, "Produto Foto Galeria View")

	w := postFotoProduto(db, fotosDir, "Bearer "+tokenAlmox, produtoID, construirJPEG(t, 100, 100), "foto.jpg")
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}

	criarContaComPapel(t, db, "Usuária Foto Galeria View", "foto-usuario-galeria-view@empresa.com", "senha-123456", "usuario")
	tokenUsuario := tokenDeLogin(t, db, "foto-usuario-galeria-view@empresa.com", "senha-123456")

	wLista := listarFotosProduto(db, fotosDir, "Bearer "+tokenUsuario, produtoID)
	if wLista.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s) — listagem deveria ser liberada a qualquer papel", wLista.Code, wLista.Body.String())
	}
}
