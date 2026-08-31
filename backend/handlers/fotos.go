package handlers

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png" // registra o decoder PNG em image.Decode (Story 3.5)
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registra o decoder WEBP em image.Decode (Story 3.5)

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// Handlers de upload, armazenamento e listagem de foto do Produto — Story
// 3.5 (spec-3-5) e Story 3.6 (spec-3-6, listagem). Fronteira HTTP pura:
// decodifica o multipart, decide o formato SÓ pelo conteúdo real do arquivo
// (nunca pela extensão do nome enviado ou pelo `Content-Type` do multipart),
// redimensiona/recomprime e delega a gravação em disco a
// services.SalvarFotoProduto — que também verifica a existência do Produto.
//
// Registro em newMux (main.go):
//   - POST /api/produtos/{id}/fotos -> RequireAuth -> RequireRole(almoxarife);
//     multipart, campo `foto`. Mesmo mínimo de papel de POST /api/produtos.
//   - GET /api/produtos/{id}/fotos/{arquivo} -> RequireAuth apenas: qualquer
//     conta autenticada visualiza — sem RequireRole.
//   - GET /api/produtos/{id}/fotos -> RequireAuth apenas (Story 3.6): lista
//     todas as fotos do Produto, mesmo padrão de visualização liberada a
//     qualquer papel.

// fotoRequestMaxBytes limita o corpo aceito por POST /api/produtos/{id}/fotos
// — 15 MiB, decisão desta spec (nenhum documento de planejamento fixa um
// número): folgado o bastante para uma foto de câmera de celular em
// JPEG/PNG/WEBP, mesmo precedente de importacaoRequestMaxBytes
// (handlers/importacoes.go).
const fotoRequestMaxBytes = 15 << 20

// fotoMaxLadoPx é o teto de redimensionamento — só reduz, nunca amplia
// (spec-3-5: ampliar uma foto pequena degradaria qualidade sem necessidade e
// nenhuma AC exige upscale). Um arquivo cujo maior lado decodificado já é
// <= fotoMaxLadoPx permanece com as dimensões originais.
const fotoMaxLadoPx = 500

// fotoQualidadeJPEG é a qualidade de recompressão JPEG fixa (spec-3-5) —
// SEMPRE aplicada, mesmo quando o arquivo original já era JPEG e não precisou
// de resize: não há bifurcação de fluxo entre cadastro e reenvio.
const fotoQualidadeJPEG = 82

// fotoMaxPixelsDecodificados é o teto de pixels (largura × altura,
// declarados no cabeçalho do arquivo) aceito antes de rodar o
// image.Decode completo — checado via image.DecodeConfig, que só lê o
// cabeçalho, ANTES de alocar qualquer buffer proporcional às dimensões.
// Sem esse teto, um arquivo de poucos bytes pode declarar dimensões
// absurdas (ex. 60000x60000) e forçar uma alocação de dezenas de GB ao
// chamar image.Decode — DoS de exaustão de memória a partir de um upload
// minúsculo. 40 megapixels é folgado acima de qualquer foto de câmera de
// celular real; nenhuma AC da spec-3-5 define um teto de resolução, então
// este valor é só uma defesa de infraestrutura, independente das ACs de
// negócio.
const fotoMaxPixelsDecodificados = 40_000_000

// exifOrientationTag é o número do tag TIFF/Exif "Orientation" (padrão Exif
// 2.3, seção 4.6.4) dentro do IFD0 de um segmento APP1.
const exifOrientationTag = 0x0112

// mensagemFotoTamanho é a mensagem de 400 VALIDATION_ERROR quando o corpo
// excede fotoRequestMaxBytes — deliberadamente distinta de
// mensagemFotoFormato (spec-3-5: os dois erros nunca compartilham a mesma
// mensagem, para quem enviou saber qual dos dois corrigir).
const mensagemFotoTamanho = "arquivo excede o tamanho máximo permitido"

// mensagemFotoFormato é a mensagem de 400 VALIDATION_ERROR quando o corpo não
// é um multipart válido, o campo `foto` está ausente, ou o conteúdo do
// arquivo não decodifica como JPEG/PNG/WEBP (arquivo corrompido ou de outro
// formato, independente de extensão/Content-Type declarados).
const mensagemFotoFormato = "arquivo não é uma imagem JPG, PNG ou WEBP válida"

// mensagemFotoFalhaProcessar é a mensagem de 500 INTERNAL_ERROR quando a
// recompressão JPEG falha depois de um decode bem-sucedido — inalcançável na
// prática (jpeg.Encode só falha por erro de escrita em io.Writer, e o
// destino aqui é um bytes.Buffer em memória), mantida como defesa em
// profundidade.
const mensagemFotoFalhaProcessar = "falha ao processar foto"

// redimensionarSeNecessario aplica o teto de fotoMaxLadoPx no maior lado da
// imagem decodificada, preservando a proporção (golang.org/x/image/draw,
// reamostragem Catmull-Rom) — nunca amplia: uma imagem cujo maior lado já é
// <= fotoMaxLadoPx é devolvida sem nenhuma cópia/redesenho.
func redimensionarSeNecessario(img image.Image) image.Image {
	origem := img.Bounds()
	largura := origem.Dx()
	altura := origem.Dy()

	maiorLado := largura
	if altura > maiorLado {
		maiorLado = altura
	}
	if maiorLado <= fotoMaxLadoPx {
		return img
	}

	escala := float64(fotoMaxLadoPx) / float64(maiorLado)
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

// lerOrientacaoEXIF procura o segmento APP1 "Exif\0\0" nos bytes brutos de um
// JPEG (Story 3.5: fotos de câmera de celular frequentemente gravam retratos
// como pixels na horizontal + um tag Orientation indicando a rotação real) e
// devolve o valor do tag Orientation (1-8) do IFD0. Devolve 1 (identidade —
// "já está correto") se o arquivo não tiver EXIF, o tag Orientation estiver
// ausente, ou qualquer parte do segmento estiver malformada — nunca falha o
// upload por causa de metadado EXIF ausente/corrompido, só deixa de corrigir
// a rotação.
func lerOrientacaoEXIF(dados []byte) int {
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
			// Start of Scan: dados de pixel seguem — EXIF só aparece antes.
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
				return orientacaoDoTIFF(payload[6:])
			}
		}
		pos = fimSegmento
	}
	return 1
}

// orientacaoDoTIFF lê o IFD0 de um bloco TIFF (o corpo de um segmento Exif,
// já sem o prefixo "Exif\0\0") e devolve o valor do tag Orientation, ou 1 se
// ausente/malformado.
func orientacaoDoTIFF(tiff []byte) int {
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
		if tag == exifOrientationTag {
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

// aplicarOrientacaoEXIF corrige a rotação/espelhamento de img segundo o tag
// Orientation lido do EXIF (chamador só passa valores 2-8; 1 é identidade e
// nunca deveria chegar aqui) — sempre devolve uma nova *image.RGBA, nunca
// modifica img. Mapeamento de valores conforme o padrão Exif 2.3, seção
// 4.6.4 (mesma tabela usada por libjpeg/PIL/ImageMagick).
func aplicarOrientacaoEXIF(img image.Image, orientacao int) image.Image {
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
			case 2: // espelho horizontal
				sx, sy = largura-1-dx, dy
			case 3: // rotação 180°
				sx, sy = largura-1-dx, altura-1-dy
			case 4: // espelho vertical
				sx, sy = dx, altura-1-dy
			case 5: // transposta (espelho pela diagonal principal)
				sx, sy = dy, dx
			case 6: // rotação 90° horária
				sx, sy = dy, altura-1-dx
			case 7: // transversa (espelho pela diagonal secundária)
				sx, sy = largura-1-dy, altura-1-dx
			case 8: // rotação 90° anti-horária
				sx, sy = largura-1-dy, dx
			default:
				sx, sy = dx, dy
			}
			destino.Set(dx, dy, img.At(origem.Min.X+sx, origem.Min.Y+sy))
		}
	}
	return destino
}

// EnviarFotoProdutoHandler expõe POST /api/produtos/{id}/fotos: lê o
// multipart (campo `foto`, limite fotoRequestMaxBytes), decodifica o
// conteúdo real do arquivo como JPEG/PNG/WEBP (extensão do nome enviado e
// `Content-Type` do multipart são ignorados), redimensiona (só reduz, nunca
// amplia) e recomprime sempre em JPEG q=82, e delega a gravação a
// services.SalvarFotoProduto.
//
// `201 {"foto":{"nome","url"}}` no sucesso. Corpo acima do limite -> `400
// VALIDATION_ERROR` (mensagemFotoTamanho); multipart inválido, campo `foto`
// ausente, ou conteúdo não decodificável -> `400 VALIDATION_ERROR`
// (mensagemFotoFormato) — as duas mensagens NUNCA se confundem. `id`
// inexistente ou malformado -> `404 NOT_FOUND` (services.ErrProdutoNaoEncontrado),
// verificado por SalvarFotoProduto antes de qualquer escrita em disco.
func EnviarFotoProdutoHandler(db *sql.DB, fotosDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("EnviarFotoProdutoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		produtoID := r.PathValue("id")

		r.Body = http.MaxBytesReader(w, r.Body, fotoRequestMaxBytes)
		if err := r.ParseMultipartForm(fotoRequestMaxBytes); err != nil {
			var erroTamanho *http.MaxBytesError
			if errors.As(err, &erroTamanho) {
				escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", mensagemFotoTamanho)
				return
			}
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", mensagemFotoFormato)
			return
		}

		arquivoEnviado, _, err := r.FormFile("foto")
		if err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", mensagemFotoFormato)
			return
		}
		defer arquivoEnviado.Close()

		// Lê o arquivo inteiro em memória UMA vez (já limitado a
		// fotoRequestMaxBytes pelo MaxBytesReader acima) — os mesmos bytes
		// alimentam primeiro image.DecodeConfig (só o cabeçalho, para o teto
		// de fotoMaxPixelsDecodificados) e depois image.Decode/lerOrientacaoEXIF,
		// sem precisar reler do multipart.File (que não dá seek).
		dados, err := io.ReadAll(arquivoEnviado)
		if err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", mensagemFotoFormato)
			return
		}

		// image.DecodeConfig só lê o cabeçalho — barato mesmo para um arquivo
		// que declare dimensões enormes. Rejeita ANTES de image.Decode alocar
		// um buffer proporcional a essas dimensões (defesa contra DoS de
		// exaustão de memória a partir de um upload minúsculo).
		cfg, _, err := image.DecodeConfig(bytes.NewReader(dados))
		if err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", mensagemFotoFormato)
			return
		}
		if int64(cfg.Width)*int64(cfg.Height) > fotoMaxPixelsDecodificados {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", mensagemFotoFormato)
			return
		}

		imagemDecodificada, formatoDecodificado, err := image.Decode(bytes.NewReader(dados))
		if err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", mensagemFotoFormato)
			return
		}

		// EXIF Orientation só existe (na prática) em JPEG — PNG/WEBP não
		// carregam esse metadado de câmera, então nada a corrigir.
		if formatoDecodificado == "jpeg" {
			if orientacao := lerOrientacaoEXIF(dados); orientacao != 1 {
				imagemDecodificada = aplicarOrientacaoEXIF(imagemDecodificada, orientacao)
			}
		}

		imagemFinal := redimensionarSeNecessario(imagemDecodificada)

		var jpegBuf bytes.Buffer
		if err := jpeg.Encode(&jpegBuf, imagemFinal, &jpeg.Options{Quality: fotoQualidadeJPEG}); err != nil {
			slog.Error("falha ao recomprimir foto em JPEG", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", mensagemFotoFalhaProcessar)
			return
		}

		foto, err := services.SalvarFotoProduto(db, fotosDir, produtoID, jpegBuf.Bytes())
		switch {
		case err == nil:
			escreverJSON(w, http.StatusCreated, map[string]any{"foto": foto})
		case errors.Is(err, services.ErrProdutoNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "produto não encontrado")
		default:
			slog.Error("falha ao salvar foto de produto", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao salvar foto")
		}
	}
}

// ServirFotoProdutoHandler expõe GET /api/produtos/{id}/fotos/{arquivo}: só
// RequireAuth (qualquer papel — visualização liberada a todos, sem
// RequireRole). `{arquivo}` é validado contra `^<id>-\d+\.jpg$`
// (`regexp.QuoteMeta(id)`) ANTES de qualquer acesso ao disco — não bate ->
// `404 NOT_FOUND`, nunca lê fora de `fotosDir`. Não depende de nenhum estado
// de `produtos` (a tabela não tem soft-delete ainda; mesmo que tivesse, a
// foto sobreviveria em disco).
func ServirFotoProdutoHandler(fotosDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("ServirFotoProdutoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		produtoID := r.PathValue("id")
		nomeArquivo := r.PathValue("arquivo")

		padrao, err := regexp.Compile(`^` + regexp.QuoteMeta(produtoID) + `-\d+\.jpg$`)
		if err != nil || !padrao.MatchString(nomeArquivo) {
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "foto não encontrada")
			return
		}

		caminho := filepath.Join(fotosDir, nomeArquivo)
		arquivo, err := os.Open(caminho)
		if err != nil {
			if os.IsNotExist(err) {
				escreverErro(w, http.StatusNotFound, "NOT_FOUND", "foto não encontrada")
				return
			}
			// Qualquer outro erro (permissão negada, volume desmontado, etc.)
			// é uma falha de infraestrutura de verdade — não deve se
			// disfarçar de "não encontrada" (Story 3.5, revisão de patch).
			slog.Error("falha ao abrir foto de produto", "caminho", caminho, "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao servir foto")
			return
		}
		defer arquivo.Close()

		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, arquivo); err != nil {
			slog.Error("falha ao servir foto de produto", "error", err)
		}
	}
}

// ListarFotosProdutoHandler expõe GET /api/produtos/{id}/fotos: só
// RequireAuth (qualquer papel — mesmo padrão de ServirFotoProdutoHandler,
// sem RequireRole), delega a services.ListarFotosProduto. `200
// {"fotos":[{"nome","url"}, ...]}`, sempre um array (nunca `null`) — vazio
// quando o Produto não tem foto. `id` inexistente ou malformado -> `404
// NOT_FOUND` (services.ErrProdutoNaoEncontrado), verificado ANTES de
// qualquer leitura de disco — Story 3.6 (spec-3-6).
func ListarFotosProdutoHandler(db *sql.DB, fotosDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("ListarFotosProdutoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		produtoID := r.PathValue("id")

		fotos, err := services.ListarFotosProduto(db, fotosDir, produtoID)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]any{"fotos": fotos})
		case errors.Is(err, services.ErrProdutoNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "produto não encontrado")
		default:
			slog.Error("falha ao listar fotos de produto", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar fotos")
		}
	}
}
