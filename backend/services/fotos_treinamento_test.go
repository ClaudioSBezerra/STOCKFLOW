package services

import (
	"bytes"
	"database/sql"
	"errors"

	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Testes da Story 12.4 (spec-12-4): fotos de exemplo no Ambiente de
// Treinamento. Cobrem a matriz de I/O do service; o CLI tem o próprio teste.

// treinamentoParaFotos cria a Empresa real + Treinamento de `slug` e devolve o
// slug e o id do Treinamento.
func treinamentoParaFotos(t *testing.T, db *sql.DB, slug, cnpjBase12 string) (slugTreino, treinoID string) {
	t.Helper()
	comParLimpo(t, db, slug)
	_, treino, err := CriarEmpresaComTreinamento(db, testEmailCfg, novaEmpresaTeste(slug, cnpjBase12, "Cliente Fotos "+slug))
	if err != nil {
		t.Fatalf("CriarEmpresaComTreinamento(%s): %v", slug, err)
	}
	return treino.Slug, treino.ID
}

func idProdutoExemplo(t *testing.T, db *sql.DB, empresaID, nome string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`SELECT id FROM produtos WHERE empresa_id = $1 AND nome = $2`, empresaID, nome).Scan(&id); err != nil {
		t.Fatalf("produto %q da empresa %s: %v", nome, empresaID, err)
	}
	return id
}

func arquivosDe(t *testing.T, dir string) int {
	t.Helper()
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	return len(entradas)
}

func TestFotosTreinamento_EmbarcadasValidasParaTodoProdutoDeExemplo(t *testing.T) {
	for _, p := range produtosExemploTreinamento {
		arquivo, ok := fotosExemploTreinamento[p.nome]
		if !ok {
			t.Errorf("produto de exemplo %q sem foto embarcada", p.nome)
			continue
		}
		dados, err := fotosTreinamentoFS.ReadFile(arquivo)
		if err != nil {
			t.Errorf("%q: %v", arquivo, err)
			continue
		}
		img, err := jpeg.Decode(bytes.NewReader(dados))
		if err != nil {
			t.Errorf("%q não é JPEG decodificável: %v", arquivo, err)
			continue
		}
		if b := img.Bounds(); b.Dx() > 500 || b.Dy() > 500 {
			t.Errorf("%q = %dx%d, want <= 500px", arquivo, b.Dx(), b.Dy())
		}
	}
}

func TestFotosTreinamento_SemeiaEReexecutaSemGravar(t *testing.T) {
	db := testDB(t)
	slug, treinoID := treinamentoParaFotos(t, db, "fotos-semear", "971234560001")
	dir := t.TempDir()

	res, err := SemearFotosTreinamento(db, dir, slug, true)
	if err != nil {
		t.Fatalf("SemearFotosTreinamento: %v", err)
	}
	if !res.Executado || res.Semeadas != 5 || res.JaComFoto != 0 || len(res.Ausentes) != 0 {
		t.Fatalf("resultado = %+v, want semeadas=5", res)
	}
	for _, p := range produtosExemploTreinamento {
		id := idProdutoExemplo(t, db, treinoID, p.nome)
		fotos, err := ListarFotosProduto(db, treinoID, dir, id)
		if err != nil || len(fotos) != 1 {
			t.Errorf("fotos de %q = %v (err %v), want 1", p.nome, fotos, err)
			continue
		}
		gravado, err := os.ReadFile(filepath.Join(dir, fotos[0].Nome))
		if err != nil {
			t.Fatalf("ler foto gravada de %q: %v", p.nome, err)
		}
		esperado, err := fotosTreinamentoFS.ReadFile(fotosExemploTreinamento[p.nome])
		if err != nil || !bytes.Equal(gravado, esperado) {
			t.Errorf("foto gravada de %q difere da embarcada (err %v)", p.nome, err)
		}
	}
	if n := arquivosDe(t, dir); n != 5 {
		t.Fatalf("arquivos = %d, want 5", n)
	}

	res, err = SemearFotosTreinamento(db, dir, slug, true)
	if err != nil {
		t.Fatalf("reexecução: %v", err)
	}
	if res.Semeadas != 0 || res.JaComFoto != 5 {
		t.Errorf("reexecução = %+v, want semeadas=0 jaComFoto=5", res)
	}
	if n := arquivosDe(t, dir); n != 5 {
		t.Errorf("arquivos após reexecução = %d, want 5", n)
	}
}

func TestFotosTreinamento_DryRunNaoGrava(t *testing.T) {
	db := testDB(t)
	slug, _ := treinamentoParaFotos(t, db, "fotos-dry", "972345670001")
	dir := filepath.Join(t.TempDir(), "naoexiste")

	res, err := SemearFotosTreinamento(db, dir, slug, false)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if res.Executado || res.Semeadas != 5 || res.JaComFoto != 0 {
		t.Errorf("dry-run = %+v, want a semear=5", res)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dry-run criou o diretório de fotos (err=%v)", err)
	}
}

func TestFotosTreinamento_PreservaFotoExistenteEReportaAusente(t *testing.T) {
	db := testDB(t)
	slug, treinoID := treinamentoParaFotos(t, db, "fotos-parcial", "973456780001")
	dir := t.TempDir()

	comFoto := idProdutoExemplo(t, db, treinoID, produtosExemploTreinamento[0].nome)
	real, err := SalvarFotoProduto(db, treinoID, dir, comFoto, []byte("foto-real-do-adm"))
	if err != nil {
		t.Fatalf("SalvarFotoProduto: %v", err)
	}
	removido := produtosExemploTreinamento[4].nome
	if _, err := db.Exec(`DELETE FROM produto_estoque WHERE produto_id IN (SELECT id FROM produtos WHERE empresa_id = $1 AND nome = $2)`, treinoID, removido); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM produtos WHERE empresa_id = $1 AND nome = $2`, treinoID, removido); err != nil {
		t.Fatal(err)
	}

	res, err := SemearFotosTreinamento(db, dir, slug, true)
	if err != nil {
		t.Fatalf("SemearFotosTreinamento: %v", err)
	}
	if res.Semeadas != 3 || res.JaComFoto != 1 || len(res.Ausentes) != 1 || res.Ausentes[0] != removido {
		t.Fatalf("resultado = %+v, want semeadas=3 jaComFoto=1 ausentes=[%s]", res, removido)
	}
	fotos, err := ListarFotosProduto(db, treinoID, dir, comFoto)
	if err != nil || len(fotos) != 1 || fotos[0].Nome != real.Nome {
		t.Errorf("foto real alterada: %v (err %v), want só %s", fotos, err, real.Nome)
	}
	conteudo, err := os.ReadFile(filepath.Join(dir, real.Nome))
	if err != nil || string(conteudo) != "foto-real-do-adm" {
		t.Errorf("conteúdo da foto real = %q (err %v)", conteudo, err)
	}
	if n := arquivosDe(t, dir); n != 4 {
		t.Errorf("arquivos = %d, want 4 (1 real + 3 semeadas)", n)
	}
}

func TestFotosTreinamento_RecusaEmpresaRealEInexistente(t *testing.T) {
	db := testDB(t)
	treinamentoParaFotos(t, db, "fotos-real", "974567890001")
	dir := t.TempDir()

	if _, err := SemearFotosTreinamento(db, dir, "fotos-real", true); !errors.Is(err, ErrEmpresaNaoTreinamento) {
		t.Errorf("Empresa real: erro = %v, want ErrEmpresaNaoTreinamento", err)
	}
	if _, err := SemearFotosTreinamento(db, dir, "fotos-nao-existe", true); !errors.Is(err, ErrEmpresaNaoEncontrada) {
		t.Errorf("slug inexistente: erro = %v, want ErrEmpresaNaoEncontrada", err)
	}
	if n := arquivosDe(t, dir); n != 0 {
		t.Errorf("arquivos = %d, want 0", n)
	}
}

func TestFotosTreinamento_IsolamentoEntreEmpresas(t *testing.T) {
	db := testDB(t)
	slugA, _ := treinamentoParaFotos(t, db, "fotos-iso-a", "975678900001")
	_, outroID := treinamentoParaFotos(t, db, "fotos-iso-b", "976789010001")
	dir := t.TempDir()

	if res, err := SemearFotosTreinamento(db, dir, slugA, true); err != nil || res.Semeadas != 5 {
		t.Fatalf("semear A: %+v, %v", res, err)
	}
	for _, p := range produtosExemploTreinamento {
		id := idProdutoExemplo(t, db, outroID, p.nome)
		fotos, err := ListarFotosProduto(db, outroID, dir, id)
		if err != nil || len(fotos) != 0 {
			t.Errorf("produto homônimo %q de outra Empresa recebeu foto: %v (err %v)", p.nome, fotos, err)
		}
	}
	if n := arquivosDe(t, dir); n != 5 {
		t.Errorf("arquivos = %d, want 5", n)
	}
}

func TestFotosTreinamento_SemProdutosDeExemploOuApagadosRecusa(t *testing.T) {
	db := testDB(t)
	slug, treinoID := treinamentoParaFotos(t, db, "fotos-vazio", "975678900001")
	dir := t.TempDir()

	// Produto de exemplo apagado (mesclagem, `deleted_at`) não recebe foto.
	if _, err := db.Exec(`UPDATE produtos SET deleted_at = now() WHERE empresa_id = $1`, treinoID); err != nil {
		t.Fatal(err)
	}
	res, err := SemearFotosTreinamento(db, dir, slug, true)
	if !errors.Is(err, ErrTreinamentoSemProdutosExemplo) {
		t.Fatalf("erro = %v, want ErrTreinamentoSemProdutosExemplo", err)
	}
	if res.Semeadas != 0 || len(res.Ausentes) != len(produtosExemploTreinamento) {
		t.Errorf("resultado = %+v", res)
	}
	if n := arquivosDe(t, dir); n != 0 {
		t.Errorf("arquivos = %d, want 0", n)
	}
}

// TestFotosTreinamento_DryRunDetectaDiretorioNaoGravavel: o dry-run sonda a
// escrita em `fotosDir` (antes só o `--executar` falhava, no meio do laço). Um
// caminho DENTRO de um arquivo comum é inalcançável em qualquer usuário.
func TestFotosTreinamento_DryRunDetectaDiretorioNaoGravavel(t *testing.T) {
	db := testDB(t)
	slug, _ := treinamentoParaFotos(t, db, "fotos-sonda", "972345670091")
	arquivo := filepath.Join(t.TempDir(), "arquivo-comum")
	if err := os.WriteFile(arquivo, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := SemearFotosTreinamento(db, filepath.Join(arquivo, "fotos"), slug, false)
	if err == nil || !strings.Contains(err.Error(), "sem permissão de escrita") {
		t.Fatalf("erro = %v, want falha de diretório sem permissão de escrita já no dry-run", err)
	}
}

// TestFotosTreinamento_ExecucoesSimultaneasNaoDuplicam: o advisory lock por
// slug serializa duas execuções reais; a 2ª encontra tudo já semeado (a
// sequência lista-fotos -> grava-foto sozinha não é atômica).
func TestFotosTreinamento_ExecucoesSimultaneasNaoDuplicam(t *testing.T) {
	db := testDB(t)
	slug, _ := treinamentoParaFotos(t, db, "fotos-simultaneo", "972345670092")
	dir := t.TempDir()

	type saida struct {
		res ResultadoFotosTreinamento
		err error
	}
	ch := make(chan saida, 2)
	for i := 0; i < 2; i++ {
		go func() {
			res, err := SemearFotosTreinamento(db, dir, slug, true)
			ch <- saida{res, err}
		}()
	}
	semeadas := 0
	for i := 0; i < 2; i++ {
		o := <-ch
		if o.err != nil {
			t.Fatalf("execução simultânea: %v", o.err)
		}
		semeadas += o.res.Semeadas
	}
	if semeadas != 5 {
		t.Errorf("Semeadas somadas = %d, want 5 (sem duplicar)", semeadas)
	}
	if n := arquivosDe(t, dir); n != 5 {
		t.Errorf("arquivos em disco = %d, want 5", n)
	}
}
