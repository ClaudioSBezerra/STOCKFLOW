package middleware

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"stockflow/backend/services"
)

// Testes de RequireEmpresa — Story 9.1 (Epic 9, Multi-Empresa), spec-9-1.
//
// RequireEmpresa é o único ponto do sistema que traduz o `{slug}` da rota em
// uma Empresa (AD-19). O que estes testes travam: (a) o slug resolvido chega
// ao handler pelo contexto; (b) TODA falha de resolução — slug inexistente,
// fora da forma canônica, ou de Empresa `inativa` — colapsa no MESMO 404
// NOT_FOUND, sem revelar que um slug existe mas está desligado; (c) composto
// com RequireAuth, um token válido de OUTRA Empresa é recusado com 401
// SESSION_REVOKED.

// criarEmpresaMiddleware provisiona uma Empresa para os testes deste pacote.
// `empresas` não é truncada entre testes (é a tabela referenciada por
// usuarios, não a referenciadora), então cada caso usa slug e CNPJ próprios.
func criarEmpresaMiddleware(t *testing.T, db *sql.DB, slug, cnpj, nome string) services.Empresa {
	t.Helper()

	var existente services.Empresa
	err := db.QueryRow(`SELECT id, slug, status FROM empresas WHERE slug = $1`, slug).
		Scan(&existente.ID, &existente.Slug, &existente.Status)
	if err == nil {
		if _, err := db.Exec(`UPDATE empresas SET status = 'ativa' WHERE id = $1`, existente.ID); err != nil {
			t.Fatalf("reativar empresa %q: %v", slug, err)
		}
		existente.Status = "ativa"
		return existente
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("criarEmpresaMiddleware(%s): begin: %v", slug, err)
	}
	defer func() { _ = tx.Rollback() }()

	e, err := services.ProvisionarEmpresa(tx, services.DadosEmpresa{
		NomeFantasia: nome,
		RazaoSocial:  nome + " LTDA",
		CNPJ:         cnpj,
		Slug:         slug,
		Endereco: services.EnderecoEmpresa{
			Logradouro: "Rua de Teste",
			Numero:     "100",
			Bairro:     "Centro",
			Cidade:     "Recife",
			CEP:        "50000000",
			UF:         "PE",
		},
	})
	if err != nil {
		t.Fatalf("criarEmpresaMiddleware(%s): %v", slug, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("criarEmpresaMiddleware(%s): commit: %v", slug, err)
	}
	return e
}

// criarUsuarioNaEmpresa insere uma conta ativa DENTRO de `empresaID` — a
// variante de criarUsuario (auth_test.go) para os casos de fronteira de
// Empresa.
func criarUsuarioNaEmpresa(t *testing.T, db *sql.DB, email, empresaID string) string {
	t.Helper()
	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, empresa_id)
		VALUES ('Usuário Teste', $1, 'hash-qualquer', 'usuario', true, true, $2)
		RETURNING id`
	if err := db.QueryRow(insert, email, empresaID).Scan(&id); err != nil {
		t.Fatalf("falha ao criar usuario de teste na empresa: %v", err)
	}
	return id
}

// servirRotaDeEmpresa despacha `GET /e/{slug}/api/teste` pela MESMA
// composição de newMux: RequireEmpresa POR FORA de tudo.
func servirRotaDeEmpresa(db *sql.DB, slug string, h http.HandlerFunc, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /e/{slug}/api/teste", RequireEmpresa(db)(h))

	r := httptest.NewRequest(http.MethodGet, "/e/"+slug+"/api/teste", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func codigoDoErro(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("falha ao decodificar envelope de erro: %v (body=%s)", err, body)
	}
	return env.Error.Code
}

// TestRequireEmpresa_SlugValidoInjetaEmpresa prova o caminho feliz: a
// Empresa do slug chega ao handler pelo contexto, resolvida uma única vez.
func TestRequireEmpresa_SlugValidoInjetaEmpresa(t *testing.T) {
	db := testDB(t)
	empresa := criarEmpresaMiddleware(t, db, "mw-empresa-ativa", "22333444000181", "MW Ativa")

	var recebida services.Empresa
	var temEmpresa bool
	w := servirRotaDeEmpresa(db, "mw-empresa-ativa", func(w http.ResponseWriter, r *http.Request) {
		recebida, temEmpresa = EmpresaDaRequisicao(r.Context())
		w.WriteHeader(http.StatusOK)
	}, "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if !temEmpresa {
		t.Fatal("EmpresaDaRequisicao devolveu ok=false — a Empresa não chegou ao handler")
	}
	if recebida.ID != empresa.ID || recebida.Slug != "mw-empresa-ativa" {
		t.Errorf("empresa no contexto = %+v, want id=%s slug=%s", recebida, empresa.ID, "mw-empresa-ativa")
	}
}

// TestRequireEmpresa_FalhasColapsamEm404 prova o Always da spec: slug
// inexistente, fora da forma canônica e de Empresa inativa devolvem o MESMO
// 404 NOT_FOUND, e o handler protegido nunca roda.
func TestRequireEmpresa_FalhasColapsamEm404(t *testing.T) {
	db := testDB(t)
	inativa := criarEmpresaMiddleware(t, db, "mw-empresa-inativa", "33444555000181", "MW Inativa")
	if _, err := db.Exec(`UPDATE empresas SET status = 'inativa' WHERE id = $1`, inativa.ID); err != nil {
		t.Fatalf("desativar empresa: %v", err)
	}

	casos := map[string]string{
		"slug inexistente":        "mw-nao-existe",
		"empresa inativa":         "mw-empresa-inativa",
		"slug com maiúsculas":     "MW-Empresa-Inativa",
		"slug com hífen na ponta": "-mw-empresa-inativa",
	}
	for nome, slug := range casos {
		t.Run(nome, func(t *testing.T) {
			chamou := false
			w := servirRotaDeEmpresa(db, slug, func(w http.ResponseWriter, r *http.Request) {
				chamou = true
				w.WriteHeader(http.StatusOK)
			}, "")

			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
			}
			if code := codigoDoErro(t, w.Body.Bytes()); code != "NOT_FOUND" {
				t.Errorf("code = %q, want NOT_FOUND", code)
			}
			if chamou {
				t.Error("handler protegido rodou — RequireEmpresa deveria ter cortado antes")
			}
		})
	}
}

// TestRequireEmpresa_ComRequireAuth prova a fronteira que impede um token
// perfeitamente válido de operar sob o slug de outra Empresa: a sessão da
// Empresa A é aceita sob o slug da A e recusada com 401 SESSION_REVOKED sob o
// slug da B. Uma conta legada (`empresa_id IS NULL`, fase 1 de AD-23) também
// é recusada — falha fechada.
func TestRequireEmpresa_ComRequireAuth(t *testing.T) {
	db := testDB(t)
	empresaA := criarEmpresaMiddleware(t, db, "mw-empresa-a", "44555666000181", "MW Empresa A")
	criarEmpresaMiddleware(t, db, "mw-empresa-b", "12345678000195", "MW Empresa B")

	idA := criarUsuarioNaEmpresa(t, db, "conta-a@empresa.com", empresaA.ID)
	tokenA := gerarAccessTokenTeste(t, testJWTSecret, idA, time.Now().Add(30*time.Minute))

	idLegado := criarUsuario(t, db, "conta-legada@empresa.com", true)
	tokenLegado := gerarAccessTokenTeste(t, testJWTSecret, idLegado, time.Now().Add(30*time.Minute))

	protegido := RequireAuth(db, testJWTSecret)(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	casos := []struct {
		nome     string
		slug     string
		token    string
		wantCode int
		wantErro string
	}{
		{"sessão da própria Empresa", "mw-empresa-a", tokenA, http.StatusOK, ""},
		{"sessão de outra Empresa", "mw-empresa-b", tokenA, http.StatusUnauthorized, "SESSION_REVOKED"},
		{"conta legada sem Empresa", "mw-empresa-a", tokenLegado, http.StatusUnauthorized, "SESSION_REVOKED"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w := servirRotaDeEmpresa(db, c.slug, protegido, "Bearer "+c.token)
			if w.Code != c.wantCode {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, c.wantCode, w.Body.String())
			}
			if c.wantErro != "" {
				if code := codigoDoErro(t, w.Body.Bytes()); code != c.wantErro {
					t.Errorf("code = %q, want %q", code, c.wantErro)
				}
			}
		})
	}
}
