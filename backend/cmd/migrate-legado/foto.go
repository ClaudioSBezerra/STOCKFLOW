package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // registra o decoder PNG em image.Decode (Story 3.5)

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registra o decoder WEBP em image.Decode (Story 3.5)
)

// Pipeline de foto legada (Story 3.7): decodifica o `foto` base64 do
// documento legado (addendum §F), aplica a MESMA regra de redimensionamento/
// recompressão/orientação EXIF da Story 3.5 (handlers/fotos.go) e devolve os
// bytes JPEG prontos para services.SalvarFotoProduto — nunca a base64
// original é gravada em nenhuma coluna.
//
// A lógica abaixo é uma DUPLICATA deliberada de handlers/fotos.go: aquele
// pacote é HTTP-acoplado e suas constantes/funções não são exportadas, então
// não cruzam limite de pacote (mesmo padrão já usado neste binário para
// pqUniqueViolation/normExpr em main.go). Reabrir handlers/fotos.go para
// exportar algo reabriria código já revisado nas Stories 3.5/3.6, fora de
// escopo desta story.

// fotoLegadoMaxLadoPx é o teto de redimensionamento — só reduz, nunca amplia
// (mesma regra de fotoMaxLadoPx em handlers/fotos.go).
const fotoLegadoMaxLadoPx = 500

// fotoLegadoQualidadeJPEG é a qualidade de recompressão JPEG fixa (mesma
// regra de fotoQualidadeJPEG em handlers/fotos.go) — sempre aplicada, mesmo
// quando o arquivo original já era JPEG.
const fotoLegadoQualidadeJPEG = 82

// fotoLegadoMaxPixelsDecodificados é o mesmo teto de defesa contra DoS de
// exaustão de memória de handlers/fotos.go (fotoMaxPixelsDecodificados):
// checado via image.DecodeConfig, que só lê o cabeçalho, antes de alocar
// qualquer buffer proporcional às dimensões declaradas.
const fotoLegadoMaxPixelsDecodificados = 40_000_000

// exifOrientationTagLegado é o número do tag TIFF/Exif "Orientation" (padrão
// Exif 2.3, seção 4.6.4) — mesmo valor de exifOrientationTag em
// handlers/fotos.go.
const exifOrientationTagLegado = 0x0112

// processarFotoLegado decodifica `fotoBase64` (campo `foto` do documento
// legado, addendum §F), decodifica o formato real da imagem (JPEG/PNG/WEBP),
// corrige orientação EXIF se JPEG, redimensiona a fotoLegadoMaxLadoPx no
// maior lado e recomprime em JPEG q=fotoLegadoQualidadeJPEG.
//
// Base64/imagem corrompida ou formato não suportado -> `motivo` não-vazio,
// `jpegBytes` nil: o Produto é migrado sem foto (não aborta o corte, spec-3-7
// I/O Matrix "Foto corrompida").
func processarFotoLegado(fotoBase64 string) (jpegBytes []byte, motivo string) {
	dados, err := base64.StdEncoding.DecodeString(fotoBase64)
	if err != nil {
		return nil, fmt.Sprintf("falha ao decodificar base64: %v", err)
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(dados))
	if err != nil {
		return nil, fmt.Sprintf("conteúdo não é uma imagem JPEG/PNG/WEBP válida: %v", err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > fotoLegadoMaxPixelsDecodificados {
		return nil, fmt.Sprintf("imagem excede o teto de %d pixels decodificados", fotoLegadoMaxPixelsDecodificados)
	}

	imagemDecodificada, formato, err := image.Decode(bytes.NewReader(dados))
	if err != nil {
		return nil, fmt.Sprintf("falha ao decodificar imagem: %v", err)
	}

	if formato == "jpeg" {
		if orientacao := lerOrientacaoEXIFLegado(dados); orientacao != 1 {
			imagemDecodificada = aplicarOrientacaoEXIFLegado(imagemDecodificada, orientacao)
		}
	}

	imagemFinal := redimensionarSeNecessarioLegado(imagemDecodificada)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, imagemFinal, &jpeg.Options{Quality: fotoLegadoQualidadeJPEG}); err != nil {
		return nil, fmt.Sprintf("falha ao recomprimir imagem em JPEG: %v", err)
	}
	return buf.Bytes(), ""
}

// redimensionarSeNecessarioLegado — duplicata de
// handlers.redimensionarSeNecessario.
func redimensionarSeNecessarioLegado(img image.Image) image.Image {
	origem := img.Bounds()
	largura := origem.Dx()
	altura := origem.Dy()

	maiorLado := largura
	if altura > maiorLado {
		maiorLado = altura
	}
	if maiorLado <= fotoLegadoMaxLadoPx {
		return img
	}

	escala := float64(fotoLegadoMaxLadoPx) / float64(maiorLado)
	novaLargura := int(float64(largura)*escala + 0.5)
	novaAltura := int(float64(altura)*escala + 0.5)
	if novaLargura < 1 {
		novaLargura = 1
	}
	if novaAltura < 1 {
		novaAltura = 1
	}

	destino := image.NewRGBA(image.Rect(0, 0, novaLargura, novaAltura))
	draw.CatmullRom.Scale(destino, destino.Bounds(), img, origem, draw.Over, nil)
	return destino
}

// lerOrientacaoEXIFLegado — duplicata de handlers.lerOrientacaoEXIF.
func lerOrientacaoEXIFLegado(dados []byte) int {
	if len(dados) < 4 || dados[0] != 0xFF || dados[1] != 0xD8 {
		return 1
	}
	pos := 2
	for pos+4 <= len(dados) {
		if dados[pos] != 0xFF {
			return 1
		}
		marcador := dados[pos+1]
		if marcador == 0xD8 || marcador == 0xD9 || marcador == 0x01 || (marcador >= 0xD0 && marcador <= 0xD7) {
			pos += 2
			continue
		}
		if marcador == 0xDA {
			return 1
		}
		tamanhoSegmento := int(dados[pos+2])<<8 | int(dados[pos+3])
		if tamanhoSegmento < 2 {
			return 1
		}
		inicioPayload := pos + 4
		fimSegmento := pos + 2 + tamanhoSegmento
		if fimSegmento > len(dados) || inicioPayload > fimSegmento {
			return 1
		}
		if marcador == 0xE1 {
			payload := dados[inicioPayload:fimSegmento]
			if len(payload) >= 6 && string(payload[:6]) == "Exif\x00\x00" {
				return orientacaoDoTIFFLegado(payload[6:])
			}
		}
		pos = fimSegmento
	}
	return 1
}

// orientacaoDoTIFFLegado — duplicata de handlers.orientacaoDoTIFF.
func orientacaoDoTIFFLegado(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var ordem binary.ByteOrder
	switch {
	case tiff[0] == 'I' && tiff[1] == 'I':
		ordem = binary.LittleEndian
	case tiff[0] == 'M' && tiff[1] == 'M':
		ordem = binary.BigEndian
	default:
		return 1
	}
	deslocamentoIFD := int64(ordem.Uint32(tiff[4:8]))
	if deslocamentoIFD+2 > int64(len(tiff)) || deslocamentoIFD < 0 {
		return 1
	}
	numEntradas := int64(ordem.Uint16(tiff[deslocamentoIFD : deslocamentoIFD+2]))
	inicioEntradas := deslocamentoIFD + 2
	for i := int64(0); i < numEntradas; i++ {
		offsetEntrada := inicioEntradas + i*12
		if offsetEntrada+12 > int64(len(tiff)) {
			break
		}
		tag := ordem.Uint16(tiff[offsetEntrada : offsetEntrada+2])
		if tag == exifOrientationTagLegado {
			offsetValor := offsetEntrada + 8
			if offsetValor+2 > int64(len(tiff)) {
				return 1
			}
			valor := ordem.Uint16(tiff[offsetValor : offsetValor+2])
			if valor >= 1 && valor <= 8 {
				return int(valor)
			}
			return 1
		}
	}
	return 1
}

// aplicarOrientacaoEXIFLegado — duplicata de handlers.aplicarOrientacaoEXIF.
func aplicarOrientacaoEXIFLegado(img image.Image, orientacao int) image.Image {
	origem := img.Bounds()
	largura := origem.Dx()
	altura := origem.Dy()

	larguraDestino, alturaDestino := largura, altura
	if orientacao >= 5 && orientacao <= 8 {
		larguraDestino, alturaDestino = altura, largura
	}

	destino := image.NewRGBA(image.Rect(0, 0, larguraDestino, alturaDestino))
	for dy := 0; dy < alturaDestino; dy++ {
		for dx := 0; dx < larguraDestino; dx++ {
			var sx, sy int
			switch orientacao {
			case 2:
				sx, sy = largura-1-dx, dy
			case 3:
				sx, sy = largura-1-dx, altura-1-dy
			case 4:
				sx, sy = dx, altura-1-dy
			case 5:
				sx, sy = dy, dx
			case 6:
				sx, sy = dy, altura-1-dx
			case 7:
				sx, sy = largura-1-dy, altura-1-dx
			case 8:
				sx, sy = largura-1-dy, dx
			default:
				sx, sy = dx, dy
			}
			destino.Set(dx, dy, img.At(origem.Min.X+sx, origem.Min.Y+sy))
		}
	}
	return destino
}
