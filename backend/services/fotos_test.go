package services

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// criarProdutoParaFoto cria um Produto mínimo (categoria/estoque de seed) só
// para servir de `produtoID` válido nos testes de SalvarFotoProduto — o
// conteúdo do Produto em si é irrelevante para esta suíte.
func criarProdutoParaFoto(t *testing.T, db *sql.DB, nome string) Produto {
	t.Helper()
	estoque, err := CriarEstoque(db, "Canteiro Foto "+nome)
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	categoriaID := categoriaIDPorCodigo(t, db, "04.001")
	p, err := CriarProduto(db, CriarProdutoInput{
		Nome:              nome,
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}
	return p
}

// TestSalvarFotoProduto_Sucesso prova o caminho feliz: Produto existente,
// diretório vazio -> arquivo `<produto_id>-<timestamp>.jpg` gravado com os
// bytes exatos recebidos, `Nome`/`URL` corretos no retorno.
func TestSalvarFotoProduto_Sucesso(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	fotosDir := t.TempDir()

	produto := criarProdutoParaFoto(t, db, "Produto Com Foto")
	jpegBytes := []byte("bytes-jpeg-fake-para-teste")

	foto, err := SalvarFotoProduto(db, fotosDir, produto.ID, jpegBytes)
	if err != nil {
		t.Fatalf("SalvarFotoProduto erro inesperado: %v", err)
	}
	if foto.Nome == "" {
		t.Fatal("Nome vazio no retorno")
	}
	wantURL := "/api/produtos/" + produto.ID + "/fotos/" + foto.Nome
	if foto.URL != wantURL {
		t.Errorf("URL = %q, want %q", foto.URL, wantURL)
	}

	gravado, err := os.ReadFile(filepath.Join(fotosDir, foto.Nome))
	if err != nil {
		t.Fatalf("falha ao ler arquivo gravado: %v", err)
	}
	if string(gravado) != string(jpegBytes) {
		t.Errorf("conteúdo gravado = %q, want %q", gravado, jpegBytes)
	}
}

// TestSalvarFotoProduto_ProdutoInexistente prova que um UUID sintaticamente
// válido mas sem linha correspondente -> ErrProdutoNaoEncontrado, nenhum
// arquivo gravado.
func TestSalvarFotoProduto_ProdutoInexistente(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	fotosDir := t.TempDir()

	_, err := SalvarFotoProduto(db, fotosDir, "00000000-0000-0000-0000-000000000000", []byte("x"))
	if err != ErrProdutoNaoEncontrado {
		t.Fatalf("err = %v, want ErrProdutoNaoEncontrado", err)
	}

	entradas, err := os.ReadDir(fotosDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entradas) != 0 {
		t.Errorf("fotosDir deveria estar vazio, tem %d entradas", len(entradas))
	}
}

// TestSalvarFotoProduto_IDMalformado prova que um `id` não-UUID (SQLSTATE
// 22P02) colapsa no mesmo ErrProdutoNaoEncontrado — mesmo tratamento de
// AtualizarNomeProduto.
func TestSalvarFotoProduto_IDMalformado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	fotosDir := t.TempDir()

	_, err := SalvarFotoProduto(db, fotosDir, "id-nao-e-uuid", []byte("x"))
	if err != ErrProdutoNaoEncontrado {
		t.Fatalf("err = %v, want ErrProdutoNaoEncontrado", err)
	}
}

// TestSalvarFotoProduto_SegundoUploadNaoSobrescreve prova a AC de
// "foto anterior permanece intacta": dois uploads seguidos para o MESMO
// Produto gravam dois arquivos distintos, o primeiro com o conteúdo original
// preservado.
func TestSalvarFotoProduto_SegundoUploadNaoSobrescreve(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	fotosDir := t.TempDir()

	produto := criarProdutoParaFoto(t, db, "Produto Reenvio")

	primeira, err := SalvarFotoProduto(db, fotosDir, produto.ID, []byte("conteudo-1"))
	if err != nil {
		t.Fatalf("primeiro SalvarFotoProduto: %v", err)
	}
	segunda, err := SalvarFotoProduto(db, fotosDir, produto.ID, []byte("conteudo-2"))
	if err != nil {
		t.Fatalf("segundo SalvarFotoProduto: %v", err)
	}

	if primeira.Nome == segunda.Nome {
		t.Fatalf("os dois uploads geraram o MESMO nome de arquivo: %q", primeira.Nome)
	}

	conteudoPrimeira, err := os.ReadFile(filepath.Join(fotosDir, primeira.Nome))
	if err != nil {
		t.Fatalf("falha ao reler primeiro arquivo: %v", err)
	}
	if string(conteudoPrimeira) != "conteudo-1" {
		t.Errorf("primeiro arquivo foi sobrescrito: conteúdo = %q, want %q", conteudoPrimeira, "conteudo-1")
	}

	conteudoSegunda, err := os.ReadFile(filepath.Join(fotosDir, segunda.Nome))
	if err != nil {
		t.Fatalf("falha ao ler segundo arquivo: %v", err)
	}
	if string(conteudoSegunda) != "conteudo-2" {
		t.Errorf("segundo arquivo = %q, want %q", conteudoSegunda, "conteudo-2")
	}
}

// TestSalvarFotoProduto_ColisaoDeNomeIncrementaTimestamp prova o retry
// anti-colisão: um arquivo já presente no timestamp "atual" não é
// sobrescrito — SalvarFotoProduto tenta o próximo segundo e grava com sucesso
// num nome diferente.
func TestSalvarFotoProduto_ColisaoDeNomeIncrementaTimestamp(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	fotosDir := t.TempDir()

	produto := criarProdutoParaFoto(t, db, "Produto Colisao")

	// Pré-ocupa o nome de arquivo que corresponderia ao timestamp "agora" —
	// forçando SalvarFotoProduto a colidir na primeira tentativa e avançar.
	tsAgora := strconv.FormatInt(time.Now().Unix(), 10)
	nomePreOcupado := produto.ID + "-" + tsAgora + ".jpg"
	if err := os.WriteFile(filepath.Join(fotosDir, nomePreOcupado), []byte("ja-existe"), 0o644); err != nil {
		t.Fatalf("falha ao pré-ocupar nome de arquivo: %v", err)
	}

	foto, err := SalvarFotoProduto(db, fotosDir, produto.ID, []byte("conteudo-novo"))
	if err != nil {
		t.Fatalf("SalvarFotoProduto erro inesperado: %v", err)
	}
	if foto.Nome == nomePreOcupado {
		t.Fatalf("SalvarFotoProduto sobrescreveu o arquivo pré-existente %q", nomePreOcupado)
	}

	// O arquivo pré-ocupado permanece intacto.
	conteudoOriginal, err := os.ReadFile(filepath.Join(fotosDir, nomePreOcupado))
	if err != nil {
		t.Fatalf("falha ao reler arquivo pré-ocupado: %v", err)
	}
	if string(conteudoOriginal) != "ja-existe" {
		t.Errorf("arquivo pré-ocupado foi alterado: conteúdo = %q", conteudoOriginal)
	}
}

// TestListarFotosProduto_ListaOrdenadaPorNome prova a AC1 (Story 3.6,
// spec-3-6): 3 fotos salvas via SalvarFotoProduto (timestamps crescentes,
// forçados via nomes pré-ocupados de segundos anteriores) -> ListarFotosProduto
// devolve as 3 na mesma ordem de envio (== ordem de `Nome`, já que o
// timestamp unix tem largura fixa).
func TestListarFotosProduto_ListaOrdenadaPorNome(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	fotosDir := t.TempDir()

	produto := criarProdutoParaFoto(t, db, "Produto Galeria")

	// Grava 3 arquivos diretamente no formato esperado (<id>-<timestamp>.jpg)
	// com timestamps crescentes explícitos — evita depender de granularidade
	// de segundo civil real entre chamadas de SalvarFotoProduto no teste.
	base := time.Now().Unix() - 100
	var nomesEsperados []string
	for i := 0; i < 3; i++ {
		nome := produto.ID + "-" + strconv.FormatInt(base+int64(i), 10) + ".jpg"
		if err := os.WriteFile(filepath.Join(fotosDir, nome), []byte("foto-"+strconv.Itoa(i)), 0o644); err != nil {
			t.Fatalf("falha ao gravar fixture de foto %d: %v", i, err)
		}
		nomesEsperados = append(nomesEsperados, nome)
	}

	fotos, err := ListarFotosProduto(db, fotosDir, produto.ID)
	if err != nil {
		t.Fatalf("ListarFotosProduto erro inesperado: %v", err)
	}
	if len(fotos) != 3 {
		t.Fatalf("len(fotos) = %d, want 3", len(fotos))
	}
	for i, nome := range nomesEsperados {
		if fotos[i].Nome != nome {
			t.Errorf("fotos[%d].Nome = %q, want %q (ordem de envio)", i, fotos[i].Nome, nome)
		}
		wantURL := "/api/produtos/" + produto.ID + "/fotos/" + nome
		if fotos[i].URL != wantURL {
			t.Errorf("fotos[%d].URL = %q, want %q", i, fotos[i].URL, wantURL)
		}
	}
}

// TestListarFotosProduto_ListaVaziaSemFoto prova a AC4: Produto existente sem
// nenhuma foto -> slice vazia (nunca nil, nunca erro).
func TestListarFotosProduto_ListaVaziaSemFoto(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	fotosDir := t.TempDir()

	produto := criarProdutoParaFoto(t, db, "Produto Sem Foto")

	fotos, err := ListarFotosProduto(db, fotosDir, produto.ID)
	if err != nil {
		t.Fatalf("ListarFotosProduto erro inesperado: %v", err)
	}
	if fotos == nil {
		t.Fatal("fotos == nil, want slice vazia não-nil")
	}
	if len(fotos) != 0 {
		t.Errorf("len(fotos) = %d, want 0", len(fotos))
	}
}

// TestListarFotosProduto_ProdutoInexistenteOuMalformado prova que a mesma
// checagem de existência de SalvarFotoProduto se aplica à listagem: UUID
// válido sem linha correspondente OU `id` malformado -> ErrProdutoNaoEncontrado,
// nenhuma leitura de disco.
func TestListarFotosProduto_ProdutoInexistenteOuMalformado(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	fotosDir := t.TempDir()

	casos := []struct {
		nome      string
		produtoID string
	}{
		{"UUID válido inexistente", "00000000-0000-0000-0000-000000000000"},
		{"id malformado", "id-nao-e-uuid"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := ListarFotosProduto(db, fotosDir, c.produtoID)
			if err != ErrProdutoNaoEncontrado {
				t.Fatalf("err = %v, want ErrProdutoNaoEncontrado", err)
			}
		})
	}
}
