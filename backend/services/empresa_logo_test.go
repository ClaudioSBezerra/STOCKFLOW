package services

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/signintech/gopdf"
)

// pngTeste gera um PNG largura x altura de uma faixa colorida com um bloco
// mais escuro à esquerda (dá algo para ver no PDF de amostra).
func pngTeste(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{0x0b, 0x4f, 0xa8, 0xff}
			if x < w/6 {
				c = color.RGBA{0xe3, 0x1b, 0x23, 0xff}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pngIndexadoTransparenteTeste gera um PNG de PALETA (8 bits, colormap) com
// chunk tRNS: o fundo é a cor 0, totalmente transparente; um bloco vermelho
// opaco à direita. É o formato típico de logos exportadas de ferramentas
// de design.
func pngIndexadoTransparenteTeste(t testing.TB, w, h int) []byte {
	t.Helper()
	paleta := color.Palette{color.NRGBA{0, 0, 0, 0}, color.NRGBA{0xe3, 0x1b, 0x23, 0xff}, color.NRGBA{0x33, 0x33, 0x33, 0x80}}
	img := image.NewPaletted(image.Rect(0, 0, w, h), paleta)
	for y := h / 4; y < 3*h/4; y++ {
		for x := w / 4; x < 3*w/4; x++ {
			img.SetColorIndex(x, y, 1)
		}
		for x := 3 * w / 4; x < w; x++ {
			img.SetColorIndex(x, y, 2) // semitransparente
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// png16Teste: PNG RGBA de 16 bits por canal — formato que o gopdf não desenha.
func png16Teste(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA64(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pngRGBAParcialTeste: PNG NRGBA (não indexado) 8 bits com fundo totalmente
// transparente, um bloco vermelho opaco e um bloco cinza semitransparente.
func pngRGBAParcialTeste(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := h / 4; y < 3*h/4; y++ {
		for x := w / 4; x < 3*w/4; x++ {
			img.SetNRGBA(x, y, color.NRGBA{0xe3, 0x1b, 0x23, 0xff})
		}
		for x := 3 * w / 4; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{0x33, 0x33, 0x33, 0x80})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegTeste(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{0xe3, uint8(x * 255 / w), 0x23, 0xff})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestValidarLogoEmpresa(t *testing.T) {
	grande := append(pngTeste(t, 100, 100), bytes.Repeat([]byte{0}, LogoEmpresaMaxBytes)...)
	casos := []struct {
		nome  string
		dados []byte
		tipo  string
		erro  string
	}{
		{"png válido", pngTeste(t, 600, 200), "image/png", ""},
		{"jpeg válido", jpegTeste(t, 800, 300), "image/jpeg", ""},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"/>`), "", "use PNG ou JPEG"},
		{"texto renomeado", []byte("isto não é uma imagem"), "", "use PNG ou JPEG"},
		{"vazio", nil, "", "vazio"},
		{"gif", []byte("GIF89a\x40\x00\x40\x00\x00\x00\x00;"), "", "use PNG ou JPEG"},
		{"grande demais", grande, "", "512 KB"},
		{"pequena demais", pngTeste(t, 10, 10), "", "Dimensões"},
		{"larga demais", pngTeste(t, 4001, 40), "", "Dimensões"},
		{"pixels demais", pngTeste(t, 2001, 2000), "", "pixels demais"},
		{"no teto de pixels", pngTeste(t, 2000, 2000), "image/png", ""},
		{"png truncado", pngTeste(t, 600, 200)[:200], "", "corrompida ou incompleta"},
		{"jpeg truncado", jpegTeste(t, 800, 300)[:300], "", "corrompida ou incompleta"},
		{"proporção extrema", pngTeste(t, 4000, 32), "", "proporção"},
		{"png 16 bits (gopdf recusa)", png16Teste(t, 200, 100), "", "não pode ser usada"},
	}
	for _, c := range casos {
		tipo, err := ValidarLogoEmpresa(c.dados)
		if c.erro == "" {
			if err != nil || tipo != c.tipo {
				t.Errorf("%s: tipo=%q err=%v, want %q", c.nome, tipo, err, c.tipo)
			}
			continue
		}
		var e *ErroLogoEmpresa
		if !errors.As(err, &e) || !strings.Contains(e.Mensagem, c.erro) {
			t.Errorf("%s: err=%v, want mensagem com %q", c.nome, err, c.erro)
		}
	}
}

func TestLogoEmpresa_SalvarBuscarSubstituirRemoverEIsolamento(t *testing.T) {
	db := testDB(t)
	removerEmpresaDeTeste(t, db, "logo-a")
	removerEmpresaDeTeste(t, db, "logo-b")
	a := criarEmpresaDeTeste(t, db, "logo-a", "555000010001", "Logo A")
	b := criarEmpresaDeTeste(t, db, "logo-b", "555000020001", "Logo B")
	t.Cleanup(func() {
		removerEmpresaDeTeste(t, db, "logo-a")
		removerEmpresaDeTeste(t, db, "logo-b")
	})

	if _, err := BuscarLogoEmpresa(db, a.ID); !errors.Is(err, ErrLogoEmpresaNaoEncontrada) {
		t.Fatalf("sem logo: err = %v", err)
	}
	p1 := pngTeste(t, 600, 200)
	l, err := SalvarLogoEmpresa(db, a.ID, p1)
	if err != nil || l.TipoConteudo != "image/png" {
		t.Fatalf("salvar png: %v %v", l.TipoConteudo, err)
	}
	got, err := BuscarLogoEmpresa(db, a.ID)
	if err != nil || !bytes.Equal(got.Conteudo, p1) {
		t.Fatalf("bytes devem ser os originais: err=%v", err)
	}
	// Substitui por JPEG.
	j := jpegTeste(t, 800, 300)
	if _, err := SalvarLogoEmpresa(db, a.ID, j); err != nil {
		t.Fatal(err)
	}
	got, _ = BuscarLogoEmpresa(db, a.ID)
	if got.TipoConteudo != "image/jpeg" || !bytes.Equal(got.Conteudo, j) {
		t.Error("a nova logo deveria substituir a antiga")
	}
	// Inválido não grava nem apaga.
	if _, err := SalvarLogoEmpresa(db, a.ID, []byte("lixo")); err == nil {
		t.Fatal("lixo deveria ser recusado")
	}
	if got, _ = BuscarLogoEmpresa(db, a.ID); !bytes.Equal(got.Conteudo, j) {
		t.Error("envio inválido não pode alterar a logo gravada")
	}
	// Outra Empresa não enxerga nem é afetada.
	if _, err := BuscarLogoEmpresa(db, b.ID); !errors.Is(err, ErrLogoEmpresaNaoEncontrada) {
		t.Errorf("empresa B enxergou a logo da A: %v", err)
	}
	if err := RemoverLogoEmpresa(db, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := BuscarLogoEmpresa(db, a.ID); err != nil {
		t.Errorf("remover na B apagou a logo da A: %v", err)
	}
	// Remover é idempotente.
	for i := 0; i < 2; i++ {
		if err := RemoverLogoEmpresa(db, a.ID); err != nil {
			t.Fatalf("remover #%d: %v", i, err)
		}
	}
	if _, err := BuscarLogoEmpresa(db, a.ID); !errors.Is(err, ErrLogoEmpresaNaoEncontrada) {
		t.Errorf("após remover: %v", err)
	}
}

// --- Logo no cabeçalho da Pré-Nota --------------------------------------------

func TestLayoutPreNota_Logo(t *testing.T) {
	c := conteudoMistoTeste()
	if l := layoutDaPreNota(c); l.Logo != nil || l.posicoesEmpresa().Rotulo != paddingQuadroPT {
		t.Error("sem logo: layout não pode ter logo nem deslocamento")
	}
	for _, d := range [][2]int{{600, 200}, {200, 600}, {40, 40}} {
		c.LogoEmpresa = pngTeste(t, d[0], d[1])
		l := layoutDaPreNota(c)
		if l.Logo == nil {
			t.Fatalf("%v: logo ausente", d)
		}
		if l.Logo.Largura > logoMaxLarguraPreNotaPT+0.001 || l.Logo.Altura > logoMaxAlturaPreNotaPT+0.001 {
			t.Errorf("%v: %.1fx%.1f estoura o limite", d, l.Logo.Largura, l.Logo.Altura)
		}
		if r := l.Logo.Largura / l.Logo.Altura; r < float64(d[0])/float64(d[1])-0.01 || r > float64(d[0])/float64(d[1])+0.01 {
			t.Errorf("%v: proporção perdida (%.1fx%.1f)", d, l.Logo.Largura, l.Logo.Altura)
		}
		if l.posicoesEmpresa().Rotulo <= l.Logo.Altura {
			t.Errorf("%v: texto deve descer além da logo", d)
		}
	}
}

func TestRenderizarPreNotaPDF_Logo(t *testing.T) {
	c := conteudoMistoTeste()
	sem, err := RenderizarPreNotaPDF(c)
	if err != nil {
		t.Fatal(err)
	}
	semDeNovo, _ := RenderizarPreNotaPDF(c)
	if !bytes.Equal(sem, semDeNovo) {
		t.Error("sem logo: não determinístico")
	}
	// Logo nil/vazia = idêntico.
	c.LogoEmpresa = []byte{}
	if vazio, _ := RenderizarPreNotaPDF(c); !bytes.Equal(sem, vazio) {
		t.Error("logo vazia deve gerar o mesmo PDF de sempre")
	}

	dir := os.Getenv("PRE_NOTA_AMOSTRAS")
	if dir == "" {
		dir = t.TempDir()
	}
	for nome, logo := range map[string][]byte{"png": pngTeste(t, 600, 200), "jpeg": jpegTeste(t, 800, 300)} {
		c.LogoEmpresa = logo
		a, err := RenderizarPreNotaPDF(c)
		if err != nil {
			t.Fatalf("%s: %v", nome, err)
		}
		b, _ := RenderizarPreNotaPDF(c)
		if !bytes.Equal(a, b) {
			t.Errorf("%s: mesmos bytes de logo devem dar mesmos bytes de PDF", nome)
		}
		if bytes.Equal(a, sem) || len(a) <= len(sem) {
			t.Errorf("%s: PDF com logo deveria diferir do sem logo", nome)
		}
		_ = os.WriteFile(filepath.Join(dir, "pre-nota-logo-"+nome+".pdf"), a, 0o644)
	}
	_ = os.WriteFile(filepath.Join(dir, "pre-nota-sem-logo.pdf"), sem, 0o644)
}

func TestRenderizarPreNotaPDF_LogoCorrompidaNaoDerrubaOPDF(t *testing.T) {
	c := conteudoMistoTeste()
	sem, _ := RenderizarPreNotaPDF(c)
	ruins := map[string][]byte{
		"lixo":      []byte("não sou imagem"),
		"truncada":  pngTeste(t, 300, 100)[:40],
		"cabecalho": append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 64)...),
	}
	for nome, ruim := range ruins {
		c.LogoEmpresa = ruim
		pdf, err := RenderizarPreNotaPDF(c)
		if err != nil {
			t.Fatalf("%s: logo ruim derrubou o PDF: %v", nome, err)
		}
		if !bytes.Equal(pdf, sem) {
			t.Errorf("%s: sem a logo o PDF deve ser idêntico ao de sempre", nome)
		}
	}
}

// TestGerarPreNotaPDF_LogoDoCadastroAtual: a logo é lida do cadastro no
// momento do download (Pré-Nota já emitida sai com a logo cadastrada depois,
// e volta ao cabeçalho sem logo ao remover); a logo de OUTRA Empresa nunca vaza.
func TestGerarPreNotaPDF_LogoDoCadastroAtual(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	c := semearPreNotaMista(t, db, "PreNota Logo")
	removerEmpresaDeTeste(t, db, "logo-outra")
	outra := criarEmpresaDeTeste(t, db, "logo-outra", "555000030001", "Logo Outra")
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM empresa_logos WHERE empresa_id = $1`, empresaTeste)
		removerEmpresaDeTeste(t, db, "logo-outra")
	})
	_ = RemoverLogoEmpresa(db, empresaTeste)

	semLogo, _, err := GerarPreNotaPDF(db, empresaTeste, c.preNotaID)
	if err != nil {
		t.Fatalf("sem logo: %v", err)
	}
	// Logo só da outra Empresa: este PDF segue sem logo.
	if _, err := SalvarLogoEmpresa(db, outra.ID, pngTeste(t, 600, 200)); err != nil {
		t.Fatal(err)
	}
	if pdf, _, _ := GerarPreNotaPDF(db, empresaTeste, c.preNotaID); !bytes.Equal(pdf, semLogo) {
		t.Error("logo de outra Empresa vazou para a Pré-Nota")
	}

	png600 := pngTeste(t, 600, 200)
	if _, err := SalvarLogoEmpresa(db, empresaTeste, png600); err != nil {
		t.Fatal(err)
	}
	conteudo, err := MontarPreNotaConteudo(db, empresaTeste, c.preNotaID)
	if err != nil || !bytes.Equal(conteudo.LogoEmpresa, png600) {
		t.Fatalf("conteúdo deveria trazer a logo gravada (err=%v)", err)
	}
	comLogo, _, err := GerarPreNotaPDF(db, empresaTeste, c.preNotaID)
	if err != nil || bytes.Equal(comLogo, semLogo) {
		t.Fatalf("PDF com logo deveria diferir (err=%v)", err)
	}
	if err := RemoverLogoEmpresa(db, empresaTeste); err != nil {
		t.Fatal(err)
	}
	if pdf, _, _ := GerarPreNotaPDF(db, empresaTeste, c.preNotaID); !bytes.Equal(pdf, semLogo) {
		t.Error("após remover, o PDF deve voltar a ser idêntico ao sem logo")
	}
}

// TestRenderizarPreNotaPDF_LogoPNGIndexadoComTransparencia: PNG de paleta com
// tRNS (logos exportadas de ferramentas de design) é desenhado sem falha e
// SEM fundo opaco — a máscara de transparência vai para o PDF (SMask) e a
// logo guardada segue com os bytes originais.
func TestRenderizarPreNotaPDF_LogoPNGIndexadoComTransparencia(t *testing.T) {
	logo := pngIndexadoTransparenteTeste(t, 377, 134)
	if !bytes.Contains(logo, []byte("tRNS")) || !bytes.Contains(logo, []byte("PLTE")) {
		t.Fatal("pré-condição: o PNG de teste deveria ser indexado com tRNS")
	}
	c := conteudoMistoTeste()
	if l := layoutDaPreNota(PreNotaConteudo{LogoEmpresa: logo}); l.Logo == nil {
		t.Fatal("o gopdf deveria conseguir desenhar PNG indexado com tRNS")
	}
	c.LogoEmpresa = logo
	pdf, err := RenderizarPreNotaPDF(c)
	if err != nil {
		t.Fatal(err)
	}
	// Transparência PARCIAL (alpha 0x80 na paleta) vira imagem RGBA com
	// máscara suave (/SMask); sem isso as bordas sairiam opacas.
	if !bytes.Contains(pdf, []byte("/SMask")) {
		t.Error("PNG indexado com tRNS deveria sair com máscara (/SMask) no PDF: fundo opaco")
	}
	// Só fundo totalmente transparente (caso comum): o gopdf usa /Mask por
	// cor-chave com os bytes originais — também sem fundo opaco.
	soFundo := color.Palette{color.NRGBA{0, 0, 0, 0}, color.NRGBA{0xe3, 0x1b, 0x23, 0xff}}
	img := image.NewPaletted(image.Rect(0, 0, 100, 60), soFundo)
	for y := 15; y < 45; y++ {
		for x := 25; x < 75; x++ {
			img.SetColorIndex(x, y, 1)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	c2 := conteudoMistoTeste()
	c2.LogoEmpresa = buf.Bytes()
	if l := layoutDaPreNota(c2); l.Logo == nil || l.Logo.Imagem != nil {
		t.Error("fundo totalmente transparente deve usar os bytes originais")
	}
	pdf2, err := RenderizarPreNotaPDF(c2)
	if err != nil || !bytes.Contains(pdf2, []byte("/Mask [")) {
		t.Errorf("PNG indexado com fundo transparente deveria sair com /Mask (err=%v)", err)
	}
	outro, _ := RenderizarPreNotaPDF(c)
	if !bytes.Equal(pdf, outro) {
		t.Error("não determinístico")
	}
	if dir := os.Getenv("PRE_NOTA_AMOSTRAS"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "pre-nota-logo-indexado-trns.pdf"), pdf, 0o644)
	}
}

// pngRGBAParcial: PNG NÃO indexado com transparência parcial sai com máscara
// suave e sem falha; o PDF de amostra vai para PRE_NOTA_AMOSTRAS (conferência visual).
func TestRenderizarPreNotaPDF_LogoPNGRGBAComTransparenciaParcial(t *testing.T) {
	logo := pngRGBAParcialTeste(t, 377, 134)
	if _, err := ValidarLogoEmpresa(logo); err != nil {
		t.Fatalf("validar: %v", err)
	}
	c := conteudoMistoTeste()
	c.LogoEmpresa = logo
	pdf, err := RenderizarPreNotaPDF(c)
	if err != nil {
		t.Fatal(err)
	}
	if layoutDaPreNota(c).Logo == nil || !bytes.Contains(pdf, []byte("/SMask")) {
		t.Error("PNG NRGBA com alpha parcial deveria sair com /SMask")
	}
	if dir := os.Getenv("PRE_NOTA_AMOSTRAS"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "pre-nota-logo-rgba-parcial.pdf"), pdf, 0o644)
	}
}

// posicoesEmpresa: com logo, rótulo e texto ficam ABAIXO do fundo da logo
// (remover o deslocamento no renderizador faria este teste falhar); sem logo,
// as posições são as de sempre.
func TestLayoutPreNota_PosicoesDaEmpresaComELogo(t *testing.T) {
	c := conteudoMistoTeste()
	sem := layoutDaPreNota(c).posicoesEmpresa()
	if sem.LogoFundo != sem.LogoTopo || sem.Rotulo != paddingQuadroPT || sem.Texto != paddingQuadroPT+tamanhoRotuloPreNotaPT+2 {
		t.Errorf("sem logo = %+v, want as posições de sempre", sem)
	}
	c.LogoEmpresa = pngTeste(t, 600, 200)
	l := layoutDaPreNota(c)
	com := l.posicoesEmpresa()
	if l.Logo == nil || com.LogoFundo <= com.LogoTopo {
		t.Fatalf("logo ausente: %+v", com)
	}
	if com.Rotulo < com.LogoFundo || com.Texto < com.LogoFundo+tamanhoRotuloPreNotaPT {
		t.Errorf("com logo = %+v: rótulo/texto devem ficar abaixo do fundo da logo", com)
	}
	if com.Texto-com.Rotulo != sem.Texto-sem.Rotulo {
		t.Error("o espaço rótulo->texto não deveria mudar")
	}
}

func TestLayoutPreNota_LogoComProporcaoExtremaNaoEDesenhada(t *testing.T) {
	// Fora do envio (conteúdo montado à mão / dado antigo): sem logo, sem quebrar.
	c := conteudoMistoTeste()
	sem, _ := RenderizarPreNotaPDF(c)
	c.LogoEmpresa = pngTeste(t, 4000, 32)
	if l := layoutDaPreNota(c); l.Logo != nil {
		t.Error("4000x32 escalaria para menos de 6 pt de altura: não desenhar")
	}
	if pdf, err := RenderizarPreNotaPDF(c); err != nil || !bytes.Equal(pdf, sem) {
		t.Errorf("PDF deveria sair como o sem logo (err=%v)", err)
	}
}

func TestSemPanicoPreNota_PanicoViraErro(t *testing.T) {
	if err := semPanicoPreNota(func() error { panic("pânico de teste") }); err == nil {
		t.Error("o pânico deveria virar erro")
	}
	if err := semPanicoPreNota(func() error { return nil }); err != nil {
		t.Errorf("sem pânico: %v", err)
	}
	// Bytes que o gopdf recusa (ex.: aleatórios com cara de PNG) nunca panicam para fora.
	lixo := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x7f}, 128)...)
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.AddPage()
	if err := desenharLogoPreNota(pdf, &logoPreNota{Dados: lixo, Largura: 50, Altura: 50}, 0, 0); err == nil {
		t.Error("lixo com cabeçalho de PNG deveria dar erro, não sucesso")
	}
}

// Bytes que passam em DecodeConfig mas não decodificam, gravados DIRETO no
// banco (contornando a validação do envio): o PDF sai sem logo e sem pânico.
func TestGerarPreNotaPDF_LogoRuimNoBancoSaiSemLogo(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)
	c := semearPreNotaMista(t, db, "PreNota Logo Ruim")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM empresa_logos WHERE empresa_id = $1`, empresaTeste) })
	_ = RemoverLogoEmpresa(db, empresaTeste)
	semLogo, _, err := GerarPreNotaPDF(db, empresaTeste, c.preNotaID)
	if err != nil {
		t.Fatal(err)
	}
	bom := pngTeste(t, 300, 100)
	for nome, ruim := range map[string][]byte{
		"truncada":  bom[:len(bom)/2],
		"png16bits": png16Teste(t, 200, 100),
		"achatada":  pngTeste(t, 4000, 32),
	} {
		if _, err := db.Exec(`INSERT INTO empresa_logos (empresa_id, tipo_conteudo, conteudo) VALUES ($1, 'image/png', $2)
			ON CONFLICT (empresa_id) DO UPDATE SET conteudo = EXCLUDED.conteudo`, empresaTeste, ruim); err != nil {
			t.Fatalf("%s: gravar direto: %v", nome, err)
		}
		pdf, _, err := GerarPreNotaPDF(db, empresaTeste, c.preNotaID)
		if err != nil || !bytes.Equal(pdf, semLogo) {
			t.Errorf("%s: PDF deveria sair idêntico ao sem logo (err=%v)", nome, err)
		}
	}
	// A constraint do banco: vazio e acima de 512 KB não entram nem direto.
	for nome, bytesRuins := range map[string][]byte{"vazio": {}, "grande": make([]byte, LogoEmpresaMaxBytes+1)} {
		if _, err := db.Exec(`UPDATE empresa_logos SET conteudo = $2 WHERE empresa_id = $1`, empresaTeste, bytesRuins); err == nil {
			t.Errorf("%s: o CHECK de tamanho deveria recusar", nome)
		}
	}
}
