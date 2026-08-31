package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// Testes de processarFotoLegado (foto.go) — Story 3.7 (spec-3-7), finding de
// revisão adversarial: redimensionarSeNecessarioLegado, aplicarOrientacaoEXIFLegado
// e lerOrientacaoEXIFLegado não tinham nenhum teste que exercitasse o
// caminho de resize acima de 500px nem um JPEG com EXIF Orientation
// não-padrão. Os fixtures deste arquivo constroem esses casos byte a byte,
// sem depender de arquivos externos.

// construirJPEGLegado monta um JPEG real em memória, largura x altura,
// dividido em 4 quadrantes de cor sólida (TL=vermelho, TR=verde, BL=azul,
// BR=amarelo) — usado para provar geometricamente que o resize e a correção
// de orientação movem os pixels certos, não só que "algo" foi decodificado.
func construirJPEGLegado(t *testing.T, largura, altura int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, largura, altura))
	meioX, meioY := largura/2, altura/2
	for y := 0; y < altura; y++ {
		for x := 0; x < largura; x++ {
			var c color.RGBA
			switch {
			case x < meioX && y < meioY:
				c = color.RGBA{R: 255, A: 255} // TL vermelho
			case x >= meioX && y < meioY:
				c = color.RGBA{G: 255, A: 255} // TR verde
			case x < meioX && y >= meioY:
				c = color.RGBA{B: 255, A: 255} // BL azul
			default:
				c = color.RGBA{R: 255, G: 255, A: 255} // BR amarelo
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

// segmentoEXIFOrientacao monta um segmento APP1/EXIF mínimo (TIFF
// little-endian, IFD0 com uma única entrada Orientation) declarando
// `orientacao` — bytes conforme o padrão Exif 2.3 §4.6.4 (mesma tabela que
// orientacaoDoTIFFLegado lê).
func segmentoEXIFOrientacao(orientacao uint16) []byte {
	tiff := []byte{
		'I', 'I', 0x2A, 0x00, // byte order little-endian + magic number 42
		0x08, 0x00, 0x00, 0x00, // offset do IFD0 = 8
		0x01, 0x00, // 1 entrada no IFD0
		0x12, 0x01, // tag 0x0112 (Orientation), little-endian
		0x03, 0x00, // tipo SHORT
		0x01, 0x00, 0x00, 0x00, // count = 1
		byte(orientacao), byte(orientacao >> 8), 0x00, 0x00, // valor (SHORT, alinhado à esquerda)
		0x00, 0x00, 0x00, 0x00, // offset do próximo IFD = nenhum
	}
	payload := append([]byte("Exif\x00\x00"), tiff...)
	tamanho := len(payload) + 2
	seg := []byte{0xFF, 0xE1, byte(tamanho >> 8), byte(tamanho)}
	return append(seg, payload...)
}

// comEXIFOrientacao insere um segmento APP1/EXIF logo após o SOI (FFD8) de
// um JPEG já codificado — simula uma foto de câmera com o metadado
// Orientation, sem depender de nenhuma lib externa de escrita de EXIF.
func comEXIFOrientacao(t *testing.T, jpegBytes []byte, orientacao uint16) []byte {
	t.Helper()
	if len(jpegBytes) < 2 || jpegBytes[0] != 0xFF || jpegBytes[1] != 0xD8 {
		t.Fatalf("fixture não começa com SOI (FFD8)")
	}
	seg := segmentoEXIFOrientacao(orientacao)
	out := make([]byte, 0, len(jpegBytes)+len(seg))
	out = append(out, jpegBytes[:2]...)
	out = append(out, seg...)
	out = append(out, jpegBytes[2:]...)
	return out
}

// TestProcessarFotoLegado_RedimensionaMaiorLado prova o resize: uma imagem
// 1000x2000 (retrato, maior lado = altura) sai com o maior lado em 500px,
// proporção preservada — mesma regra da Story 3.5, agora exercitada no
// pipeline duplicado local de cmd/migrate-legado.
func TestProcessarFotoLegado_RedimensionaMaiorLado(t *testing.T) {
	entrada := construirJPEGLegado(t, 1000, 2000)
	fotoB64 := base64.StdEncoding.EncodeToString(entrada)

	jpegBytes, motivo := processarFotoLegado(fotoB64)
	if motivo != "" {
		t.Fatalf("processarFotoLegado retornou motivo de falha inesperado: %s", motivo)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(jpegBytes))
	if err != nil {
		t.Fatalf("jpeg.DecodeConfig: %v", err)
	}
	if cfg.Width != 250 || cfg.Height != 500 {
		t.Errorf("dimensões = %dx%d, want 250x500 (maior lado 500px, proporção 1:2 preservada)", cfg.Width, cfg.Height)
	}
}

// TestProcessarFotoLegado_SemUpscaleAbaixoDoTeto prova que uma imagem cujo
// maior lado já é <= 500px sai com as MESMAS dimensões (nenhum upscale) —
// mesma regra do handler HTTP (Story 3.5).
func TestProcessarFotoLegado_SemUpscaleAbaixoDoTeto(t *testing.T) {
	entrada := construirJPEGLegado(t, 300, 200)
	fotoB64 := base64.StdEncoding.EncodeToString(entrada)

	jpegBytes, motivo := processarFotoLegado(fotoB64)
	if motivo != "" {
		t.Fatalf("processarFotoLegado retornou motivo de falha inesperado: %s", motivo)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(jpegBytes))
	if err != nil {
		t.Fatalf("jpeg.DecodeConfig: %v", err)
	}
	if cfg.Width != 300 || cfg.Height != 200 {
		t.Errorf("dimensões = %dx%d, want 300x200 (sem upscale)", cfg.Width, cfg.Height)
	}
}

// canalDominante lê a cor de img em (x,y) e devolve qual dos 4 quadrantes de
// construirJPEGLegado ela representa — tolerante a ruído de recompressão
// JPEG (limiares, não igualdade exata).
func canalDominante(img image.Image, x, y int) string {
	r, g, b, _ := img.At(x, y).RGBA()
	r, g, b = r>>8, g>>8, b>>8
	switch {
	case r > 150 && g > 150 && b < 100:
		return "amarelo"
	case r > 150 && g < 100 && b < 100:
		return "vermelho"
	case g > 150 && r < 100 && b < 100:
		return "verde"
	case b > 150 && r < 100 && g < 100:
		return "azul"
	default:
		return "?"
	}
}

// TestProcessarFotoLegado_CorrigeOrientacaoEXIF prova que um JPEG com EXIF
// Orientation=6 (padrão Exif 2.3 §4.6.4 — fotos de câmera em retrato
// gravadas como pixels na horizontal) sai rotacionado 90° horário:
// dimensões trocadas e os quadrantes de cor movidos para os cantos
// esperados (out TL = in BL, out TR = in TL, out BL = in BR, out BR = in TR
// — a mesma matemática de rotação 90° horária padrão, verificada por
// derivação independente do código em foto.go, não só espelhando-o).
func TestProcessarFotoLegado_CorrigeOrientacaoEXIF(t *testing.T) {
	const largura, altura = 40, 80 // TL=vermelho TR=verde BL=azul BR=amarelo
	base := construirJPEGLegado(t, largura, altura)
	comExif := comEXIFOrientacao(t, base, 6)
	fotoB64 := base64.StdEncoding.EncodeToString(comExif)

	jpegBytes, motivo := processarFotoLegado(fotoB64)
	if motivo != "" {
		t.Fatalf("processarFotoLegado retornou motivo de falha inesperado: %s", motivo)
	}

	imagemFinal, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		t.Fatalf("jpeg.Decode: %v", err)
	}
	b := imagemFinal.Bounds()
	if b.Dx() != altura || b.Dy() != largura {
		t.Fatalf("dimensões pós-correção = %dx%d, want %dx%d (90° horário: largura/altura trocadas)", b.Dx(), b.Dy(), altura, largura)
	}

	const margem = 5
	casos := []struct {
		nome    string
		x, y    int
		wantCor string
	}{
		{"TL", margem, margem, "azul"},                            // out TL = in BL
		{"TR", b.Dx() - 1 - margem, margem, "vermelho"},           // out TR = in TL
		{"BL", margem, b.Dy() - 1 - margem, "amarelo"},            // out BL = in BR
		{"BR", b.Dx() - 1 - margem, b.Dy() - 1 - margem, "verde"}, // out BR = in TR
	}
	for _, c := range casos {
		if got := canalDominante(imagemFinal, c.x, c.y); got != c.wantCor {
			t.Errorf("canto %s em (%d,%d) = %q, want %q — orientação não foi corrigida corretamente", c.nome, c.x, c.y, got, c.wantCor)
		}
	}
}

// TestLerOrientacaoEXIFLegado_SemEXIF prova que um JPEG sem segmento
// APP1/Exif devolve orientação 1 (identidade) — nunca falha por ausência de
// metadado.
func TestLerOrientacaoEXIFLegado_SemEXIF(t *testing.T) {
	base := construirJPEGLegado(t, 20, 20)
	if got := lerOrientacaoEXIFLegado(base); got != 1 {
		t.Errorf("lerOrientacaoEXIFLegado(sem EXIF) = %d, want 1", got)
	}
}

// TestLerOrientacaoEXIFLegado_ComEXIF prova a leitura direta do valor
// Orientation (sem passar pelo pipeline completo de resize/recompressão).
func TestLerOrientacaoEXIFLegado_ComEXIF(t *testing.T) {
	base := construirJPEGLegado(t, 20, 20)
	comExif := comEXIFOrientacao(t, base, 6)
	if got := lerOrientacaoEXIFLegado(comExif); got != 6 {
		t.Errorf("lerOrientacaoEXIFLegado(Orientation=6) = %d, want 6", got)
	}
}
