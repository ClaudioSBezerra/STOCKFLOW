package services

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registra o decoder JPEG para image.DecodeConfig
	_ "image/png"  // registra o decoder PNG para image.DecodeConfig
	"time"
)

// Logo da Empresa (spec-logo-da-empresa): uma por Empresa, guardada em
// `empresa_logos` (bytes originais, sem redimensionar nem reencodar) e
// desenhada no cabeçalho da Pré-Nota. Isolamento por Empresa (AD-20): toda
// query filtra `empresa_id` vindo do contexto, nunca do cliente.

const (
	// LogoEmpresaMaxBytes é o tamanho máximo do arquivo enviado (512 KB).
	LogoEmpresaMaxBytes = 512 * 1024
	// Limites de largura e altura, em pixels, aceitos no envio.
	LogoEmpresaMinLado = 32
	LogoEmpresaMaxLado = 4000

	tipoLogoPNG  = "image/png"
	tipoLogoJPEG = "image/jpeg"
)

// ErroLogoEmpresa é uma recusa de validação do arquivo enviado; Mensagem é
// segura para devolver ao cliente (HTTP 400).
type ErroLogoEmpresa struct{ Mensagem string }

func (e *ErroLogoEmpresa) Error() string { return e.Mensagem }

// ErrLogoEmpresaNaoEncontrada: a Empresa não tem logo cadastrada.
var ErrLogoEmpresaNaoEncontrada = errors.New("logo da empresa não encontrada")

// LogoEmpresa é a logo gravada.
type LogoEmpresa struct {
	TipoConteudo string
	Conteudo     []byte
	AtualizadoEm time.Time
}

const mensagemLogoCorrompida = "Imagem corrompida ou incompleta. Salve de novo como PNG ou JPEG e envie outra vez."

// LogoEmpresaMaxPixels é o teto de pixels totais (largura x altura): limita a
// memória de decodificar/converter a imagem ao gerar o PDF (além dos limites
// por lado). 4.000.000 = por exemplo 2000x2000.
const LogoEmpresaMaxPixels = 4_000_000

// ValidarLogoEmpresa confere o arquivo DE VERDADE (nunca confia em extensão
// nem em Content-Type do cliente): formato PNG/JPEG pelo cabeçalho, limites
// de lado e de pixels, decodificação COMPLETA (imagem truncada/corrompida é
// recusada) e a mesma checagem de "dá para desenhar na Pré-Nota" usada na
// geração do PDF — o gestor descobre na hora, não depois. Devolve o tipo de
// conteúdo canônico ("image/png" | "image/jpeg").
func ValidarLogoEmpresa(dados []byte) (string, error) {
	if len(dados) == 0 {
		return "", &ErroLogoEmpresa{"Arquivo vazio. Envie uma imagem PNG ou JPEG."}
	}
	if len(dados) > LogoEmpresaMaxBytes {
		return "", &ErroLogoEmpresa{"A logo é grande demais: o limite é 512 KB."}
	}
	cfg, formato, err := image.DecodeConfig(bytes.NewReader(dados))
	if err != nil {
		// Começa como PNG/JPEG mas o cabeçalho não fecha: arquivo cortado.
		if bytes.HasPrefix(dados, []byte("\x89PNG")) || bytes.HasPrefix(dados, []byte("\xff\xd8")) {
			return "", &ErroLogoEmpresa{mensagemLogoCorrompida}
		}
		return "", &ErroLogoEmpresa{"Formato inválido: use PNG ou JPEG."}
	}
	if formato != "png" && formato != "jpeg" {
		return "", &ErroLogoEmpresa{"Formato inválido: use PNG ou JPEG."}
	}
	if cfg.Width < LogoEmpresaMinLado || cfg.Height < LogoEmpresaMinLado ||
		cfg.Width > LogoEmpresaMaxLado || cfg.Height > LogoEmpresaMaxLado {
		return "", &ErroLogoEmpresa{fmt.Sprintf(
			"Dimensões inválidas (%dx%d px): largura e altura devem ficar entre %d e %d px.",
			cfg.Width, cfg.Height, LogoEmpresaMinLado, LogoEmpresaMaxLado)}
	}
	if int64(cfg.Width)*int64(cfg.Height) > LogoEmpresaMaxPixels {
		return "", &ErroLogoEmpresa{"A imagem tem pixels demais (máx. 4 milhões, ex. 2000×2000). Reduza e envie de novo."}
	}
	if err := decodificarLogoCompleta(dados); err != nil {
		return "", &ErroLogoEmpresa{mensagemLogoCorrompida}
	}
	if _, err := prepararLogoPreNota(dados); err != nil {
		if errors.Is(err, errLogoProporcaoPreNota) {
			return "", &ErroLogoEmpresa{"A proporção da imagem é extrema demais: impressa no cabeçalho, a largura ou a altura ficaria menor que 6 pt. Use uma imagem menos achatada."}
		}
		return "", &ErroLogoEmpresa{"Esta imagem não pode ser usada na Pré-Nota. Salve como PNG ou JPEG comum (sem entrelaçamento) e envie de novo."}
	}
	if formato == "png" {
		return tipoLogoPNG, nil
	}
	return tipoLogoJPEG, nil
}

// decodificarLogoCompleta decodifica todos os pixels (com recover: um decoder
// que entre em pânico com dado hostil vira erro, nunca derruba o servidor).
func decodificarLogoCompleta(dados []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pânico ao decodificar: %v", r)
		}
	}()
	_, _, err = image.Decode(bytes.NewReader(dados))
	return err
}

// SalvarLogoEmpresa valida e grava (ou substitui) a logo da Empresa. Arquivo
// inválido -> *ErroLogoEmpresa e nada é gravado.
func SalvarLogoEmpresa(db *sql.DB, empresaID string, dados []byte) (LogoEmpresa, error) {
	tipo, err := ValidarLogoEmpresa(dados)
	if err != nil {
		return LogoEmpresa{}, err
	}
	var l LogoEmpresa
	err = db.QueryRowContext(context.Background(), `
		INSERT INTO empresa_logos (empresa_id, tipo_conteudo, conteudo, atualizado_em)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (empresa_id) DO UPDATE
		   SET tipo_conteudo = EXCLUDED.tipo_conteudo, conteudo = EXCLUDED.conteudo, atualizado_em = now()
		RETURNING tipo_conteudo, conteudo, atualizado_em`,
		empresaID, tipo, dados).Scan(&l.TipoConteudo, &l.Conteudo, &l.AtualizadoEm)
	if err != nil {
		return LogoEmpresa{}, fmt.Errorf("falha ao salvar logo da empresa: %w", err)
	}
	return l, nil
}

// BuscarLogoEmpresa devolve a logo da Empresa ou ErrLogoEmpresaNaoEncontrada.
func BuscarLogoEmpresa(db *sql.DB, empresaID string) (LogoEmpresa, error) {
	var l LogoEmpresa
	err := db.QueryRowContext(context.Background(),
		`SELECT tipo_conteudo, conteudo, atualizado_em FROM empresa_logos WHERE empresa_id = $1`,
		empresaID).Scan(&l.TipoConteudo, &l.Conteudo, &l.AtualizadoEm)
	if errors.Is(err, sql.ErrNoRows) {
		return LogoEmpresa{}, ErrLogoEmpresaNaoEncontrada
	}
	if err != nil {
		return LogoEmpresa{}, fmt.Errorf("falha ao buscar logo da empresa: %w", err)
	}
	return l, nil
}

// RemoverLogoEmpresa apaga a logo da Empresa; idempotente (sem logo = nada).
func RemoverLogoEmpresa(db *sql.DB, empresaID string) error {
	if _, err := db.ExecContext(context.Background(),
		`DELETE FROM empresa_logos WHERE empresa_id = $1`, empresaID); err != nil {
		return fmt.Errorf("falha ao remover logo da empresa: %w", err)
	}
	return nil
}
