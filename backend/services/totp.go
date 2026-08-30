package services

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // HMAC-SHA1 é o algoritmo exigido pelo RFC 6238/4226 (TOTP), não usado aqui para hashing de segredo.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// totpSegredoBytes é o tamanho do segredo TOTP gerado por GerarSegredoTOTP:
// 20 bytes (160 bits) é o tamanho recomendado pelo RFC 4226 §4 para uso com
// HMAC-SHA1.
const totpSegredoBytes = 20

// totpPasso é o intervalo de tempo (RFC 6238 §5.2) usado para derivar o
// contador HOTP a partir do relógio: 30s, o valor padrão do RFC e o único
// usado por qualquer app autenticador comum (Google Authenticator, Authy).
const totpPasso = 30 * time.Second

// totpDigitos é o número de dígitos do código TOTP exibido/validado — 6,
// padrão do RFC 6238 e de todo app autenticador comum.
const totpDigitos = 6

// totpJanelaTolerancia é a tolerância de deriva de relógio entre o
// autenticador do usuário e o servidor (RFC 6238 §5.2): aceita o passo
// atual e um passo antes/depois (±30s).
var totpJanelaTolerancia = []int64{-1, 0, 1}

// GerarSegredoTOTP gera um novo segredo TOTP aleatório: 20 bytes de
// crypto/rand codificados em base32 sem padding (o formato aceito por todo
// app autenticador na hora de digitar o segredo manualmente).
func GerarSegredoTOTP() (string, error) {
	buf := make([]byte, totpSegredoBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("falha ao gerar segredo TOTP: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

// URLProvisionamentoTOTP monta a URL `otpauth://` (formato padrão "Key URI",
// usado por todo app autenticador para preencher o QR Code de
// provisionamento): emissor "StockFlow", HMAC-SHA1, 6 dígitos, passo de 30s
// — mesmos parâmetros usados por ValidarCodigoTOTP.
func URLProvisionamentoTOTP(email, segredo string) string {
	label := "StockFlow:" + email
	v := url.Values{}
	v.Set("secret", segredo)
	v.Set("issuer", "StockFlow")
	v.Set("algorithm", "SHA1")
	v.Set("digits", strconv.Itoa(totpDigitos))
	v.Set("period", strconv.Itoa(int(totpPasso.Seconds())))
	return fmt.Sprintf("otpauth://totp/%s?%s", url.PathEscape(label), v.Encode())
}

// gerarCodigoHOTP implementa o núcleo HOTP (RFC 4226 §5.3) sobre
// HMAC-SHA1: o segredo (base32) é decodificado, o contador de 8 bytes
// big-endian é HMAC-assinado, e o "dynamic truncation" do RFC extrai
// totpDigitos dígitos decimais do resultado.
func gerarCodigoHOTP(segredo string, contador uint64) (string, error) {
	chave, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(segredo)))
	if err != nil {
		return "", fmt.Errorf("segredo TOTP inválido: %w", err)
	}

	var contadorBytes [8]byte
	binary.BigEndian.PutUint64(contadorBytes[:], contador)

	mac := hmac.New(sha1.New, chave)
	mac.Write(contadorBytes[:])
	soma := mac.Sum(nil)

	// Dynamic truncation (RFC 4226 §5.3): os 4 bits menos significativos do
	// último byte do HMAC apontam o offset de onde extrair 4 bytes, dos quais
	// só os 31 bits menos significativos (máscara 0x7fffffff, descarta o bit
	// de sinal) formam o inteiro truncado.
	offset := soma[len(soma)-1] & 0x0f
	truncado := (uint32(soma[offset])&0x7f)<<24 |
		uint32(soma[offset+1])<<16 |
		uint32(soma[offset+2])<<8 |
		uint32(soma[offset+3])

	modulo := uint32(1)
	for i := 0; i < totpDigitos; i++ {
		modulo *= 10
	}
	codigo := truncado % modulo

	return fmt.Sprintf("%0*d", totpDigitos, codigo), nil
}

// PassoAtualTOTP devolve o índice do passo TOTP (RFC 6238 §5.2, o contador
// HOTP derivado do relógio) correspondente ao instante atual — usado por
// ConcluirLoginMFA/ConfirmarConfiguracaoMFA (Story 1.11) para gravar
// `usuarios.mfa_ultimo_passo_usado` e recusar o reuso do mesmo código dentro
// da mesma janela de validade (~30s, sem contar a tolerância de relógio de
// ValidarCodigoTOTP).
func PassoAtualTOTP() int64 {
	return int64(time.Now().UTC().Unix()) / int64(totpPasso.Seconds())
}

// ValidarCodigoTOTP confere um código de 6 dígitos contra o segredo,
// aceitando o passo atual e ±1 passo de tolerância de relógio (RFC 6238
// §5.2). A comparação de cada candidato usa subtle.ConstantTimeCompare para
// não vazar por temporização quantos dígitos do prefixo bateram. Um segredo
// malformado (base32 inválido) ou um código que não seja exatamente
// totpDigitos dígitos numéricos nunca casam — devolve false, nunca erro:
// quem chama só precisa saber "válido ou não".
func ValidarCodigoTOTP(segredo, codigo string) bool {
	codigoNormalizado := strings.TrimSpace(codigo)
	if len(codigoNormalizado) != totpDigitos {
		return false
	}
	for _, r := range codigoNormalizado {
		if r < '0' || r > '9' {
			return false
		}
	}

	contadorAtual := uint64(time.Now().UTC().Unix()) / uint64(totpPasso.Seconds())
	for _, delta := range totpJanelaTolerancia {
		contador := contadorAtual
		if delta < 0 {
			decremento := uint64(-delta)
			if decremento > contador {
				continue
			}
			contador -= decremento
		} else {
			contador += uint64(delta)
		}

		candidato, err := gerarCodigoHOTP(segredo, contador)
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(candidato), []byte(codigoNormalizado)) == 1 {
			return true
		}
	}
	return false
}
