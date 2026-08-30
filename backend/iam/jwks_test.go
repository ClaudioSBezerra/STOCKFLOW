package iam

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// jwksDeChave serializa uma ou mais chaves públicas RSA no formato JWKS que o
// Keycloak devolve em /protocol/openid-connect/certs.
func jwksDeChave(chaves map[string]*rsa.PublicKey) string {
	type outJWK struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Use string `json:"use"`
		Alg string `json:"alg"`
		N   string `json:"n"`
		E   string `json:"e"`
	}
	doc := struct {
		Keys []outJWK `json:"keys"`
	}{}
	for kid, pub := range chaves {
		doc.Keys = append(doc.Keys, outJWK{
			Kty: "RSA",
			Kid: kid,
			Use: "sig",
			Alg: "RS256",
			N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		})
	}
	b, _ := json.Marshal(doc)
	return string(b)
}

// servidorJWKS sobe um httptest.Server que serve o corpo/status informados e
// conta quantos GETs recebeu.
func servidorJWKS(t *testing.T, status int, corpo string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, corpo)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func gerarChaveRSA(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gerar chave RSA: %v", err)
	}
	return k
}

func TestJWKSClient_CacheHit(t *testing.T) {
	priv := gerarChaveRSA(t)
	body := jwksDeChave(map[string]*rsa.PublicKey{"kid-1": &priv.PublicKey})
	srv, hits := servidorJWKS(t, http.StatusOK, body)

	c := NewJWKSClient(srv.URL, time.Hour)

	for i := 0; i < 5; i++ {
		got, err := c.GetKey("kid-1")
		if err != nil {
			t.Fatalf("GetKey #%d: %v", i, err)
		}
		if got.N.Cmp(priv.PublicKey.N) != 0 {
			t.Fatalf("GetKey #%d: chave divergente", i)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("fetches = %d, want 1 (cache deveria servir as chamadas seguintes)", n)
	}
}

func TestJWKSClient_RefetchAoExpirarTTL(t *testing.T) {
	priv := gerarChaveRSA(t)
	body := jwksDeChave(map[string]*rsa.PublicKey{"kid-1": &priv.PublicKey})
	srv, hits := servidorJWKS(t, http.StatusOK, body)

	// TTL minúsculo: a segunda chamada, após o sleep, deve refazer o fetch.
	c := NewJWKSClient(srv.URL, 20*time.Millisecond)

	if _, err := c.GetKey("kid-1"); err != nil {
		t.Fatalf("primeira GetKey: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := c.GetKey("kid-1"); err != nil {
		t.Fatalf("segunda GetKey: %v", err)
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("fetches = %d, want 2 (TTL expirado deveria forçar refetch)", n)
	}
}

func TestJWKSClient_KidDesconhecidoDisparaUmUnicoRefetch(t *testing.T) {
	priv := gerarChaveRSA(t)
	body := jwksDeChave(map[string]*rsa.PublicKey{"kid-1": &priv.PublicKey})
	srv, hits := servidorJWKS(t, http.StatusOK, body)

	c := NewJWKSClient(srv.URL, time.Hour)

	if _, err := c.GetKey("kid-1"); err != nil {
		t.Fatalf("GetKey(kid-1): %v", err)
	}
	// kid ausente: UM único refetch (nunca um laço de retry) e então ErrKeyNotFound.
	_, err := c.GetKey("kid-desconhecido")
	if err != ErrKeyNotFound {
		t.Fatalf("GetKey(kid-desconhecido): err = %v, want ErrKeyNotFound", err)
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("fetches = %d, want 2 (1 inicial + exatamente 1 refetch, sem laço)", n)
	}

	// A chave boa continua servível do cache — o miss não corrompeu o cache.
	if _, err := c.GetKey("kid-1"); err != nil {
		t.Fatalf("GetKey(kid-1) após o miss: %v", err)
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("fetches = %d, want 2 (cache-hit não refaz fetch)", n)
	}
}

func TestJWKSClient_StatusNao200EhErro(t *testing.T) {
	srv, _ := servidorJWKS(t, http.StatusServiceUnavailable, `{"keys":[]}`)
	c := NewJWKSClient(srv.URL, time.Hour)

	if _, err := c.GetKey("kid-1"); err == nil {
		t.Fatal("GetKey deveria falhar quando o JWKS responde 503 — nunca 'sucesso com cache vazio'")
	}
}

func TestJWKSClient_CorpoSemChavesEhErro(t *testing.T) {
	srv, _ := servidorJWKS(t, http.StatusOK, `{"keys":[]}`)
	c := NewJWKSClient(srv.URL, time.Hour)

	if _, err := c.GetKey("kid-1"); err == nil {
		t.Fatal("GetKey deveria falhar quando o JWKS não traz nenhuma chave (dívida do FB_APU02 não repetida)")
	}
}

func TestJWKSClient_JWKMalformadoEhIgnorado(t *testing.T) {
	priv := gerarChaveRSA(t)
	// Uma chave boa + uma malformada (n inválido). A boa deve sobreviver.
	corpo := `{"keys":[
		{"kty":"RSA","kid":"ruim","use":"sig","alg":"RS256","n":"!!!nao-base64!!!","e":"AQAB"},
		{"kty":"RSA","kid":"boa","use":"sig","alg":"RS256","n":"` +
		base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes()) + `","e":"` +
		base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.PublicKey.E)).Bytes()) + `"}
	]}`
	srv, _ := servidorJWKS(t, http.StatusOK, corpo)
	c := NewJWKSClient(srv.URL, time.Hour)

	if _, err := c.GetKey("boa"); err != nil {
		t.Fatalf("GetKey(boa): %v — a chave válida não deveria ser descartada junto com a malformada", err)
	}
	if _, err := c.GetKey("ruim"); err != ErrKeyNotFound {
		t.Fatalf("GetKey(ruim): err = %v, want ErrKeyNotFound (JWK malformado não entra no cache)", err)
	}
}
