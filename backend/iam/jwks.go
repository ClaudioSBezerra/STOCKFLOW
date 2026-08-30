// Package iam isola tudo que toca o Keycloak (realm corporativo Ferreira
// Costa) do resto do backend (AD-7): o cliente JWKS com cache em memória e o
// middleware que valida um access token RS256 do realm e extrai apenas o
// e-mail/`email_verified` para o fluxo de login federado (Story 1.9). Este
// pacote NUNCA importa `handlers` (mesma razão da duplicação de `escreverErro`
// em `middleware/auth.go`: evitar ciclo de import) e NUNCA consome
// roles/grupos do realm.
package iam

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// ErrKeyNotFound é devolvido por GetKey quando o `kid` do token não está no
// JWKS do realm — mesmo depois do refetch disparado pela ausência. O
// middleware mapeia isso para 401 SSO_TOKEN_INVALIDO.
var ErrKeyNotFound = errors.New("iam: kid não encontrado no JWKS")

// jwk é uma chave pública no formato JWK (RFC 7517) como o Keycloak serializa
// em /protocol/openid-connect/certs.
type jwk struct {
	Kty string `json:"kty"` // "RSA"
	Kid string `json:"kid"`
	N   string `json:"n"` // módulo RSA, base64url sem padding
	E   string `json:"e"` // expoente RSA, base64url sem padding
}

// JWKSClient busca e mantém em cache (thread-safe) as chaves públicas RSA do
// realm usadas para verificar a assinatura RS256 dos access tokens.
type JWKSClient struct {
	jwksURL    string
	httpClient *http.Client
	cacheTTL   time.Duration

	mu       sync.RWMutex
	keys     map[string]*rsa.PublicKey
	cachedAt time.Time
}

// NewJWKSClient cria um cliente apontado para jwksURL (tipicamente
// "<realm>/protocol/openid-connect/certs"). ttl <= 0 assume 1h.
func NewJWKSClient(jwksURL string, ttl time.Duration) *JWKSClient {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &JWKSClient{
		jwksURL:    jwksURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		cacheTTL:   ttl,
		keys:       make(map[string]*rsa.PublicKey),
	}
}

// GetKey devolve a chave pública RSA do `kid` informado. Serve do cache
// enquanto ele é válido; refaz o fetch quando o TTL expira OU quando o `kid`
// não está no cache (janela de rotação de chaves do realm: um novo `kid`
// aparece antes do TTL vencer). Faz NO MÁXIMO um fetch por chamada — nunca
// entra em laço de retry por `kid` arbitrário (dívida do FB_APU02 não
// repetida, addendum §B).
func (c *JWKSClient) GetKey(kid string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	if !c.expiradoLocked() {
		if k, ok := c.keys[kid]; ok {
			c.mu.RUnlock()
			return k, nil
		}
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check: outra goroutine pode ter atualizado o cache enquanto
	// esperávamos o Lock exclusivo.
	if !c.expiradoLocked() {
		if k, ok := c.keys[kid]; ok {
			return k, nil
		}
	}

	// Cache expirado, nunca populado, ou `kid` ausente: um único refetch.
	if err := c.fetchLocked(); err != nil {
		return nil, err
	}

	if k, ok := c.keys[kid]; ok {
		return k, nil
	}
	return nil, ErrKeyNotFound
}

// expiradoLocked reporta se o cache precisa ser recarregado por idade. DEVE
// ser chamado com c.mu já retido (leitura ou escrita).
func (c *JWKSClient) expiradoLocked() bool {
	return c.cachedAt.IsZero() || time.Since(c.cachedAt) > c.cacheTTL
}

// fetchLocked busca o JWKS e substitui o cache. DEVE ser chamado com
// c.mu.Lock() retido. Status != 200 ou zero chaves RSA válidas é ERRO — nunca
// "sucesso com cache vazio" (dívida do FB_APU02 explicitamente não repetida,
// addendum §B): um JWKS vazio travaria todo login SSO por até um TTL inteiro.
func (c *JWKSClient) fetchLocked() error {
	resp, err := c.httpClient.Get(c.jwksURL)
	if err != nil {
		return fmt.Errorf("iam: falha ao buscar JWKS: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("iam: JWKS respondeu status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("iam: falha ao ler corpo do JWKS: %w", err)
	}

	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("iam: falha ao decodificar JWKS: %w", err)
	}

	novas := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pub, err := parseRSAPublicKey(k)
		if err != nil {
			slog.Warn("iam: chave JWK ignorada", "kid", k.Kid, "error", err)
			continue
		}
		novas[k.Kid] = pub
	}

	if len(novas) == 0 {
		return errors.New("iam: JWKS não trouxe nenhuma chave RSA válida")
	}

	c.keys = novas
	c.cachedAt = time.Now()
	slog.Info("iam: JWKS atualizado", "chaves", len(novas))
	return nil
}

// parseRSAPublicKey reconstrói uma *rsa.PublicKey a partir de um JWK kty=RSA,
// decodificando `n`/`e` de base64url (sem padding) para big.Int.
func parseRSAPublicKey(k jwk) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("módulo n inválido: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("expoente e inválido: %w", err)
	}
	if len(nBytes) == 0 || len(eBytes) == 0 {
		return nil, errors.New("módulo ou expoente vazio")
	}

	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() || e.Int64() > (1<<31-1) {
		return nil, errors.New("expoente fora do intervalo suportado")
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(e.Int64()),
	}, nil
}
