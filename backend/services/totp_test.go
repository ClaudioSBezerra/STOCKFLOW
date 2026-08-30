package services

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

// segredoRFC6238ASCII é o segredo de teste SHA1 do RFC 6238 Apêndice B
// ("12345678901234567890", 20 bytes ASCII) — usado literalmente como chave
// HMAC pelo RFC, então aqui ele precisa ser codificado em base32 antes de
// passar por gerarCodigoHOTP/ValidarCodigoTOTP (que sempre decodificam o
// segredo recebido como base32).
const segredoRFC6238ASCII = "12345678901234567890"

func segredoRFC6238Base32() string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte(segredoRFC6238ASCII))
}

// TestGerarCodigoHOTP_VetoresRFC6238 prova o núcleo HOTP/RFC4226 contra os
// vetores de teste canônicos do RFC 6238 Apêndice B (modo SHA1, 8 dígitos no
// RFC). Esta implementação usa totpDigitos=6, e truncar para 6 dígitos é
// matematicamente equivalente a pegar os 6 dígitos finais do valor de 8
// dígitos publicado (ambos são o mesmo inteiro truncado módulo 10^n) — por
// isso os "want" abaixo são os 6 dígitos finais de cada valor do RFC.
func TestGerarCodigoHOTP_VetoresRFC6238(t *testing.T) {
	segredo := segredoRFC6238Base32()

	casos := []struct {
		tempoUnix int64
		rfcTOTP8  string
		want6     string
	}{
		{59, "94287082", "287082"},
		{1111111109, "07081804", "081804"},
		{1111111111, "14050471", "050471"},
		{1234567890, "89005924", "005924"},
		{2000000000, "69279037", "279037"},
	}

	for _, c := range casos {
		contador := uint64(c.tempoUnix) / 30
		got, err := gerarCodigoHOTP(segredo, contador)
		if err != nil {
			t.Fatalf("gerarCodigoHOTP(t=%d) retornou erro: %v", c.tempoUnix, err)
		}
		if got != c.want6 {
			t.Errorf("gerarCodigoHOTP(t=%d) = %q, want %q (RFC 6238: %q)", c.tempoUnix, got, c.want6, c.rfcTOTP8)
		}
	}
}

// TestValidarCodigoTOTP_CodigoAtualValido prova o caminho feliz com o
// relógio real: um código gerado para o contador do instante atual é aceito.
func TestValidarCodigoTOTP_CodigoAtualValido(t *testing.T) {
	segredo, err := GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP falhou: %v", err)
	}

	contadorAtual := uint64(time.Now().UTC().Unix()) / 30
	codigo, err := gerarCodigoHOTP(segredo, contadorAtual)
	if err != nil {
		t.Fatalf("gerarCodigoHOTP falhou: %v", err)
	}

	if !ValidarCodigoTOTP(segredo, codigo) {
		t.Error("ValidarCodigoTOTP rejeitou um código válido para o passo atual")
	}
}

// TestValidarCodigoTOTP_JanelaDeTolerancia prova que um passo antes ou
// depois do atual (±30s) ainda é aceito — a tolerância de deriva de relógio
// exigida pelo RFC 6238 §5.2.
func TestValidarCodigoTOTP_JanelaDeTolerancia(t *testing.T) {
	segredo, err := GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP falhou: %v", err)
	}

	contadorAtual := uint64(time.Now().UTC().Unix()) / 30
	for _, delta := range []int64{-1, 1} {
		contador := contadorAtual
		if delta < 0 {
			contador--
		} else {
			contador++
		}
		codigo, err := gerarCodigoHOTP(segredo, contador)
		if err != nil {
			t.Fatalf("gerarCodigoHOTP falhou: %v", err)
		}
		if !ValidarCodigoTOTP(segredo, codigo) {
			t.Errorf("ValidarCodigoTOTP rejeitou um código do passo %+d, dentro da tolerância", delta)
		}
	}
}

// TestValidarCodigoTOTP_ForaDaJanela prova que um código de dois passos
// (±60s) fora do atual é rejeitado.
func TestValidarCodigoTOTP_ForaDaJanela(t *testing.T) {
	segredo, err := GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP falhou: %v", err)
	}

	contadorAtual := uint64(time.Now().UTC().Unix()) / 30
	codigo, err := gerarCodigoHOTP(segredo, contadorAtual+2)
	if err != nil {
		t.Fatalf("gerarCodigoHOTP falhou: %v", err)
	}
	if ValidarCodigoTOTP(segredo, codigo) {
		t.Error("ValidarCodigoTOTP aceitou um código dois passos fora da janela de tolerância")
	}
}

// TestValidarCodigoTOTP_CodigoInvalido prova rejeição de entradas obviamente
// inválidas: vazio, tamanho errado, não-numérico.
func TestValidarCodigoTOTP_CodigoInvalido(t *testing.T) {
	segredo, err := GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP falhou: %v", err)
	}

	for _, codigo := range []string{"", "12345", "1234567", "abcdef", "12345a"} {
		if ValidarCodigoTOTP(segredo, codigo) {
			t.Errorf("ValidarCodigoTOTP aceitou código inválido %q", codigo)
		}
	}
}

// TestGerarSegredoTOTP_TamanhoEAlfabeto prova que GerarSegredoTOTP produz
// segredos de 20 bytes (32 caracteres em base32 sem padding) usando só o
// alfabeto base32 padrão, e que duas chamadas nunca colidem.
func TestGerarSegredoTOTP_TamanhoEAlfabeto(t *testing.T) {
	segredo1, err := GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP falhou: %v", err)
	}
	segredo2, err := GerarSegredoTOTP()
	if err != nil {
		t.Fatalf("GerarSegredoTOTP falhou: %v", err)
	}

	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(segredo1)
	if err != nil {
		t.Fatalf("segredo gerado não é base32 válido: %v", err)
	}
	if len(decoded) != totpSegredoBytes {
		t.Errorf("segredo decodificado tem %d bytes, want %d", len(decoded), totpSegredoBytes)
	}

	if segredo1 == segredo2 {
		t.Error("duas chamadas a GerarSegredoTOTP produziram o mesmo segredo")
	}
}

// TestURLProvisionamentoTOTP_FormatoOtpauth prova que a URL gerada contém o
// segredo, o issuer e o e-mail do usuário — os campos mínimos que qualquer
// app autenticador precisa para provisionar a conta a partir do QR Code.
func TestURLProvisionamentoTOTP_FormatoOtpauth(t *testing.T) {
	url := URLProvisionamentoTOTP("gestor@empresa.com", "JBSWY3DPEHPK3PXP")

	const prefixo = "otpauth://totp/"
	if len(url) < len(prefixo) || url[:len(prefixo)] != prefixo {
		t.Fatalf("URL = %q, want prefixo %q", url, prefixo)
	}
	for _, esperado := range []string{"secret=JBSWY3DPEHPK3PXP", "issuer=StockFlow", "algorithm=SHA1", "digits=6", "period=30"} {
		if !strings.Contains(url, esperado) {
			t.Errorf("URL %q não contém %q", url, esperado)
		}
	}
}
