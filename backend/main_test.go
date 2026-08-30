package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"

	"stockflow/backend/services"
)

var (
	migrateOnce sync.Once
	migrateErr  error
)

// testDB abre uma conexão contra DATABASE_URL e aplica as migrations reais do
// binário (runMigrations, a mesma função usada no startup de main()). Pula o
// teste — em vez de falhar — quando nenhum Postgres foi configurado: suba um
// com `docker compose up -d db` (ou um Postgres local) e exporte DATABASE_URL.
//
// Este pacote e backend/cmd/seed-admin compartilham a mesma tabela usuarios
// ao vivo no DATABASE_URL informado (cada um faz TRUNCATE/INSERT/SELECT nela).
// `go test ./...` roda pacotes diferentes como processos concorrentes por
// padrão, o que faria um pacote truncar linhas que o outro acabou de inserir
// e ainda vai verificar — por isso a suíte completa deve rodar com
// `go test -p 1 ./...` (serializa os pacotes; dentro de cada pacote os testes
// já rodam sequenciais por padrão).
func testDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL não definido — suba o banco (docker compose up -d db) para rodar os testes de integração")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("falha ao abrir conexão: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := db.Ping(); err != nil {
		t.Fatalf("banco indisponível em %s: %v", dsn, err)
	}

	migrateOnce.Do(func() {
		// go test ./... roda pacotes em paralelo por padrão: este pacote e o
		// pacote cmd/seed-admin (main_test.go) podem migrar a mesma base "do
		// zero" ao mesmo tempo. Retry curto absorve a corrida na primeira
		// criação da tabela de controle do golang-migrate sem exigir
		// `go test -p 1`.
		for attempt := 1; attempt <= 5; attempt++ {
			if migrateErr = runMigrations(db); migrateErr == nil {
				return
			}
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}
	})
	if migrateErr != nil {
		t.Fatalf("falha ao aplicar migrations: %v", migrateErr)
	}

	return db
}

// TestRunMigrations_CreateUsuariosSchema prova o AC1: a migration cria a
// tabela usuarios com todas as colunas exigidas, tipos corretos e o índice
// único funcional sobre lower(email) (AD-14).
func TestRunMigrations_CreateUsuariosSchema(t *testing.T) {
	db := testDB(t)

	cols := map[string]string{}
	rows, err := db.Query(`SELECT column_name, data_type FROM information_schema.columns WHERE table_name = 'usuarios'`)
	if err != nil {
		t.Fatalf("falha ao consultar colunas: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols[name] = typ
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iteração de colunas: %v", err)
	}

	want := map[string]string{
		"id":               "uuid",
		"nome":             "character varying",
		"email":            "character varying",
		"senha_hash":       "text",
		"papel":            "character varying",
		"email_verificado": "boolean",
		"ativo":            "boolean",
		"criado_em":        "timestamp with time zone",
	}
	for col, typ := range want {
		got, ok := cols[col]
		if !ok {
			t.Errorf("coluna %q ausente em usuarios", col)
			continue
		}
		if got != typ {
			t.Errorf("coluna %q: tipo = %q, want %q", col, got, typ)
		}
	}

	var indexDef string
	err = db.QueryRow(`SELECT indexdef FROM pg_indexes WHERE indexname = 'idx_usuarios_email_lower'`).Scan(&indexDef)
	if err != nil {
		t.Fatalf("índice único idx_usuarios_email_lower não encontrado: %v", err)
	}

	// CASCADE: desde a migration 000002 (Story 1.3), tokens_acao e
	// emails_pendentes referenciam usuarios(id) via FK — sem CASCADE, um
	// TRUNCATE isolado de usuarios falharia mesmo com as tabelas dependentes
	// vazias.
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO usuarios (nome, email, papel) VALUES ('x', 'papel-invalido@example.com', 'inexistente')`); err == nil {
		t.Error("esperava falha ao inserir papel fora do CHECK ('usuario','almoxarife','gestor','adm'), mas o insert teve sucesso")
	}
}

// TestRunMigrations_UniqueEmailLowerIndex prova que a unicidade é garantida
// por lower(email) e não pelo valor bruto — duas contas com o mesmo e-mail em
// caixas diferentes devem colidir.
func TestRunMigrations_UniqueEmailLowerIndex(t *testing.T) {
	db := testDB(t)
	// CASCADE: desde a migration 000002 (Story 1.3), tokens_acao e
	// emails_pendentes referenciam usuarios(id) via FK — sem CASCADE, um
	// TRUNCATE isolado de usuarios falharia mesmo com as tabelas dependentes
	// vazias.
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO usuarios (nome, email, papel) VALUES ('a', 'Dup@Example.com', 'usuario')`); err != nil {
		t.Fatalf("insert inicial falhou: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO usuarios (nome, email, papel) VALUES ('b', 'dup@example.com', 'usuario')`); err == nil {
		t.Error("esperava violação de unicidade por lower(email), insert teve sucesso")
	}
}

// TestRunMigrations_UniqueAdminIndex prova o backstop de correção do Patch 1:
// mesmo inserindo diretamente via SQL (sem passar por seedAdmin), o banco
// nunca aceita uma segunda linha com papel='adm' — a garantia é do índice
// único parcial idx_usuarios_unico_adm, não apenas da checagem sequencial na
// aplicação.
func TestRunMigrations_UniqueAdminIndex(t *testing.T) {
	db := testDB(t)
	// CASCADE: desde a migration 000002 (Story 1.3), tokens_acao e
	// emails_pendentes referenciam usuarios(id) via FK — sem CASCADE, um
	// TRUNCATE isolado de usuarios falharia mesmo com as tabelas dependentes
	// vazias.
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO usuarios (nome, email, papel) VALUES ('a', 'admin1@example.com', 'adm')`); err != nil {
		t.Fatalf("insert inicial de adm falhou: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO usuarios (nome, email, papel) VALUES ('b', 'admin2@example.com', 'adm')`); err == nil {
		t.Error("esperava violação do índice único parcial idx_usuarios_unico_adm, insert teve sucesso")
	}
}

// TestNewMux_RegistraRotasDeAutenticacao prova que newMux — a função
// realmente usada por main() — expõe as rotas de autenticação no
// método+caminho exato esperado. Sem este teste, um erro de digitação no
// padrão de rota registrado (ex. "POST /api/auth/cadastr") deixaria todos os
// testes de handlers/auth_test.go verdes (eles chamam CadastroHandler/
// VerificarEmailHandler diretamente, nunca através deste mux) enquanto o
// servidor real responderia 404 no caminho pretendido.
func TestNewMux_RegistraRotasDeAutenticacao(t *testing.T) {
	db := testDB(t)
	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	mux := newMux(db, emailCfg, jwtSecret)

	casos := []struct {
		nome         string
		metodo       string
		caminho      string
		corpo        string
		statusQuerAo int
	}{
		{
			nome:         "cadastro com payload invalido chega no CadastroHandler",
			metodo:       http.MethodPost,
			caminho:      "/api/auth/cadastro",
			corpo:        `{isto nao e json`,
			statusQuerAo: http.StatusBadRequest,
		},
		{
			nome:         "verificar-email sem token chega no VerificarEmailHandler",
			metodo:       http.MethodGet,
			caminho:      "/api/auth/verificar-email",
			statusQuerAo: http.StatusNotFound,
		},
		{
			nome:         "health chega no healthHandler",
			metodo:       http.MethodGet,
			caminho:      "/api/health",
			statusQuerAo: http.StatusOK,
		},
		{
			nome:         "login com payload invalido chega no LoginHandler",
			metodo:       http.MethodPost,
			caminho:      "/api/auth/login",
			corpo:        `{isto nao e json`,
			statusQuerAo: http.StatusBadRequest,
		},
		{
			nome:         "refresh sem cookie chega no RefreshHandler",
			metodo:       http.MethodPost,
			caminho:      "/api/auth/refresh",
			statusQuerAo: http.StatusUnauthorized,
		},
		{
			nome:         "me sem token chega no RequireAuth",
			metodo:       http.MethodGet,
			caminho:      "/api/auth/me",
			statusQuerAo: http.StatusUnauthorized,
		},
		{
			nome:         "esqueci-senha com payload invalido chega no EsqueciSenhaHandler",
			metodo:       http.MethodPost,
			caminho:      "/api/auth/esqueci-senha",
			corpo:        `{isto nao e json`,
			statusQuerAo: http.StatusBadRequest,
		},
		{
			nome:         "redefinir-senha GET sem token chega no ValidarRedefinicaoSenhaHandler",
			metodo:       http.MethodGet,
			caminho:      "/api/auth/redefinir-senha",
			statusQuerAo: http.StatusNotFound,
		},
		{
			nome:         "redefinir-senha POST com payload invalido chega no RedefinirSenhaHandler",
			metodo:       http.MethodPost,
			caminho:      "/api/auth/redefinir-senha",
			corpo:        `{isto nao e json`,
			statusQuerAo: http.StatusBadRequest,
		},
		{
			nome:         "usuarios sem token chega no RequireAuth antes de RequireRole",
			metodo:       http.MethodGet,
			caminho:      "/api/usuarios",
			statusQuerAo: http.StatusUnauthorized,
		},
		{
			nome:         "promocoes POST sem token chega no RequireAuth",
			metodo:       http.MethodPost,
			caminho:      "/api/promocoes",
			statusQuerAo: http.StatusUnauthorized,
		},
		{
			nome:         "promocoes/minha sem token chega no RequireAuth",
			metodo:       http.MethodGet,
			caminho:      "/api/promocoes/minha",
			statusQuerAo: http.StatusUnauthorized,
		},
		{
			nome:         "promocoes GET sem token chega no RequireAuth antes de RequireRole",
			metodo:       http.MethodGet,
			caminho:      "/api/promocoes",
			statusQuerAo: http.StatusUnauthorized,
		},
		{
			nome:         "promocoes/{id}/decisao sem token chega no RequireAuth antes de RequireRole",
			metodo:       http.MethodPost,
			caminho:      "/api/promocoes/qualquer-id/decisao",
			statusQuerAo: http.StatusUnauthorized,
		},
		{
			nome:         "usuarios/{id}/desativacao sem token chega no RequireAuth antes de RequireRole",
			metodo:       http.MethodPost,
			caminho:      "/api/usuarios/qualquer-id/desativacao",
			statusQuerAo: http.StatusUnauthorized,
		},
		{
			nome:         "usuarios/{id}/rebaixamento sem token chega no RequireAuth antes de RequireRole",
			metodo:       http.MethodPost,
			caminho:      "/api/usuarios/qualquer-id/rebaixamento",
			statusQuerAo: http.StatusUnauthorized,
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			var req *http.Request
			if c.corpo != "" {
				req = httptest.NewRequest(c.metodo, c.caminho, strings.NewReader(c.corpo))
			} else {
				req = httptest.NewRequest(c.metodo, c.caminho, nil)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != c.statusQuerAo {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, c.statusQuerAo, w.Body.String())
			}
		})
	}
}

// TestRunMigrations_IndicesDeTokensEEmailsPendentes prova que a migration
// 000002 (Story 1.3) cria os índices sobre usuario_id em tokens_acao e
// emails_pendentes — sem cobertura própria até este teste, ao contrário do
// precedente já estabelecido para idx_usuarios_email_lower acima.
func TestRunMigrations_IndicesDeTokensEEmailsPendentes(t *testing.T) {
	db := testDB(t)

	indices := []string{"idx_tokens_acao_usuario_id", "idx_emails_pendentes_usuario_id"}
	for _, nome := range indices {
		t.Run(nome, func(t *testing.T) {
			var indexDef string
			err := db.QueryRow(`SELECT indexdef FROM pg_indexes WHERE indexname = $1`, nome).Scan(&indexDef)
			if err != nil {
				t.Fatalf("índice %q não encontrado: %v", nome, err)
			}
		})
	}
}

// TestRunMigrations_CheckConstraintsDeTokensEEmailsPendentes prova que os
// CHECK constraints de tokens_acao.tipo e emails_pendentes.tipo/status
// (migration 000002) rejeitam valores fora do enum documentado — mesmo
// precedente já estabelecido para usuarios.papel em
// TestRunMigrations_CreateUsuariosSchema acima.
func TestRunMigrations_CheckConstraintsDeTokensEEmailsPendentes(t *testing.T) {
	db := testDB(t)

	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	var usuarioID string
	if err := db.QueryRow(`INSERT INTO usuarios (nome, email, papel) VALUES ('x', 'check-constraint@example.com', 'usuario') RETURNING id`).Scan(&usuarioID); err != nil {
		t.Fatalf("insert usuario: %v", err)
	}

	t.Run("tokens_acao.tipo rejeita valor fora do enum", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO tokens_acao (usuario_id, token, tipo, expira_em) VALUES ($1, 'token-invalido', 'tipo-invalido', now() + interval '1 hour')`, usuarioID)
		if err == nil {
			t.Error("esperava falha ao inserir tipo fora do CHECK ('verificacao_email','redefinicao_senha'), mas o insert teve sucesso")
		}
	})

	t.Run("emails_pendentes.tipo rejeita valor fora do enum", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO emails_pendentes (usuario_id, destinatario, tipo, variaveis_json) VALUES ($1, 'x@example.com', 'tipo-invalido', '{}')`, usuarioID)
		if err == nil {
			t.Error("esperava falha ao inserir tipo fora do CHECK ('verificacao_conta','redefinicao_senha'), mas o insert teve sucesso")
		}
	})

	t.Run("emails_pendentes.status rejeita valor fora do enum", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO emails_pendentes (usuario_id, destinatario, tipo, variaveis_json, status) VALUES ($1, 'x@example.com', 'verificacao_conta', '{}', 'status-invalido')`, usuarioID)
		if err == nil {
			t.Error("esperava falha ao inserir status fora do CHECK ('pendente','enviado','falho'), mas o insert teve sucesso")
		}
	})
}

func TestHealthHandler(t *testing.T) {
	db := testDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	healthHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// TestNewMux_UsuariosRotaCarregaRequireRole prova, despachando pela mesma
// instância de newMux usada por main(), que GET /api/usuarios está atrás de
// RequireRole(gestor) — e não só de RequireAuth. Um token de `usuario`/
// `almoxarife` recebe 403 FORBIDDEN; um de `gestor`/`adm` recebe 200. Sem
// estes casos, remover `middleware.RequireRole(services.PapelGestor)` de
// newMux deixaria toda a suíte verde (o único caso pré-existente — sem token
// -> 401 — é produzido só por RequireAuth).
func TestNewMux_UsuariosRotaCarregaRequireRole(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	mux := newMux(db, emailCfg, jwtSecret)

	const senha = "senha-123456"
	seedConta := func(email, papel string) {
		hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		if _, err := db.Exec(
			`INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
			 VALUES ('Conta Teste', $1, $2, $3, true, true)`,
			email, string(hash), papel,
		); err != nil {
			t.Fatalf("insert conta %q (%s): %v", email, papel, err)
		}
	}

	tokenDe := func(email string) string {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
			strings.NewReader(`{"email":"`+email+`","senha":"`+senha+`"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("login %q: status = %d, want 200 (body=%s)", email, w.Code, w.Body.String())
		}
		var body struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("login %q: decode: %v", email, err)
		}
		return body.Token
	}

	getUsuarios := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/usuarios", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	seedConta("mux-usuario@empresa.com", "usuario")
	seedConta("mux-almox@empresa.com", "almoxarife")
	seedConta("mux-gestor@empresa.com", "gestor")
	seedConta("mux-adm@empresa.com", "adm")

	t.Run("papel insuficiente -> 403 FORBIDDEN", func(t *testing.T) {
		for _, email := range []string{"mux-usuario@empresa.com", "mux-almox@empresa.com"} {
			w := getUsuarios(tokenDe(email))
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s: status = %d, want %d (body=%s)", email, w.Code, http.StatusForbidden, w.Body.String())
			}
			var env struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
				t.Fatalf("%s: decode envelope: %v", email, err)
			}
			if env.Error.Code != "FORBIDDEN" {
				t.Errorf("%s: code = %q, want %q", email, env.Error.Code, "FORBIDDEN")
			}
		}
	})

	t.Run("papel suficiente -> 200", func(t *testing.T) {
		for _, email := range []string{"mux-gestor@empresa.com", "mux-adm@empresa.com"} {
			w := getUsuarios(tokenDe(email))
			if w.Code != http.StatusOK {
				t.Fatalf("%s: status = %d, want %d (body=%s)", email, w.Code, http.StatusOK, w.Body.String())
			}
		}
	})
}

// TestNewMux_PromocoesRotasCarregamRequireRole prova, pela mesma instância de
// newMux usada por main(), que GET /api/promocoes e
// POST /api/promocoes/{id}/decisao estão atrás de RequireRole(gestor) — e não
// só de RequireAuth (Story 1.7). Um token de `usuario`/`almoxarife` recebe
// 403 FORBIDDEN; um de `gestor`/`adm` passa do gate (200 no GET; 404 no POST
// decisao com um uuid aleatório, provando que o handler executou). Sem estes
// casos, remover `middleware.RequireRole(services.PapelGestor)` dessas duas
// rotas em newMux deixaria a suíte verde.
func TestNewMux_PromocoesRotasCarregamRequireRole(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	mux := newMux(db, emailCfg, jwtSecret)

	const senha = "senha-123456"
	seedConta := func(email, papel string) {
		hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		if _, err := db.Exec(
			`INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
			 VALUES ('Conta Teste', $1, $2, $3, true, true)`,
			email, string(hash), papel,
		); err != nil {
			t.Fatalf("insert conta %q (%s): %v", email, papel, err)
		}
	}

	tokenDe := func(email string) string {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
			strings.NewReader(`{"email":"`+email+`","senha":"`+senha+`"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("login %q: status = %d, want 200 (body=%s)", email, w.Code, w.Body.String())
		}
		var body struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("login %q: decode: %v", email, err)
		}
		return body.Token
	}

	despachar := func(metodo, caminho, token, corpo string) *httptest.ResponseRecorder {
		var req *http.Request
		if corpo != "" {
			req = httptest.NewRequest(metodo, caminho, strings.NewReader(corpo))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req = httptest.NewRequest(metodo, caminho, nil)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	seedConta("promo-mux-usuario@empresa.com", "usuario")
	seedConta("promo-mux-almox@empresa.com", "almoxarife")
	seedConta("promo-mux-gestor@empresa.com", "gestor")
	seedConta("promo-mux-adm@empresa.com", "adm")

	const uuidAleatorio = "11111111-1111-1111-1111-111111111111"

	t.Run("papel abaixo de gestor -> 403 nas duas rotas", func(t *testing.T) {
		for _, email := range []string{"promo-mux-usuario@empresa.com", "promo-mux-almox@empresa.com"} {
			token := tokenDe(email)

			wGet := despachar(http.MethodGet, "/api/promocoes", token, "")
			if wGet.Code != http.StatusForbidden {
				t.Errorf("%s GET /api/promocoes: status = %d, want 403 (body=%s)", email, wGet.Code, wGet.Body.String())
			}

			wPost := despachar(http.MethodPost, "/api/promocoes/"+uuidAleatorio+"/decisao", token, `{"aprovar":true}`)
			if wPost.Code != http.StatusForbidden {
				t.Errorf("%s POST .../decisao: status = %d, want 403 (body=%s)", email, wPost.Code, wPost.Body.String())
			}
		}
	})

	t.Run("gestor/adm passam do gate", func(t *testing.T) {
		for _, email := range []string{"promo-mux-gestor@empresa.com", "promo-mux-adm@empresa.com"} {
			token := tokenDe(email)

			wGet := despachar(http.MethodGet, "/api/promocoes", token, "")
			if wGet.Code != http.StatusOK {
				t.Errorf("%s GET /api/promocoes: status = %d, want 200 (body=%s)", email, wGet.Code, wGet.Body.String())
			}

			// uuid válido porém inexistente: o handler executou (passou do
			// RequireRole) e devolveu 404, nunca 403.
			wPost := despachar(http.MethodPost, "/api/promocoes/"+uuidAleatorio+"/decisao", token, `{"aprovar":true}`)
			if wPost.Code != http.StatusNotFound {
				t.Errorf("%s POST .../decisao: status = %d, want 404 (body=%s)", email, wPost.Code, wPost.Body.String())
			}
		}
	})
}

// TestNewMux_GestaoUsuariosRotasCarregamRequireRole prova, pela mesma
// instância de newMux usada por main(), que POST /api/usuarios/{id}/desativacao
// e POST /api/usuarios/{id}/rebaixamento estão atrás de RequireRole(gestor)
// (Story 1.8). Um token de `usuario`/`almoxarife` recebe 403 FORBIDDEN; um de
// `gestor`/`adm` passa do gate (o handler executa e devolve 404 para um uuid
// aleatório, provando que não parou no 403). Sem estes casos, remover
// `middleware.RequireRole(services.PapelGestor)` dessas duas rotas em newMux
// deixaria a suíte verde.
func TestNewMux_GestaoUsuariosRotasCarregamRequireRole(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	mux := newMux(db, emailCfg, jwtSecret)

	const senha = "senha-123456"
	seedConta := func(email, papel string) {
		hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		if _, err := db.Exec(
			`INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
			 VALUES ('Conta Teste', $1, $2, $3, true, true)`,
			email, string(hash), papel,
		); err != nil {
			t.Fatalf("insert conta %q (%s): %v", email, papel, err)
		}
	}

	tokenDe := func(email string) string {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
			strings.NewReader(`{"email":"`+email+`","senha":"`+senha+`"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("login %q: status = %d, want 200 (body=%s)", email, w.Code, w.Body.String())
		}
		var body struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("login %q: decode: %v", email, err)
		}
		return body.Token
	}

	despachar := func(caminho, token, corpo string) *httptest.ResponseRecorder {
		var req *http.Request
		if corpo != "" {
			req = httptest.NewRequest(http.MethodPost, caminho, strings.NewReader(corpo))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req = httptest.NewRequest(http.MethodPost, caminho, nil)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	seedConta("gestao-mux-usuario@empresa.com", "usuario")
	seedConta("gestao-mux-almox@empresa.com", "almoxarife")
	seedConta("gestao-mux-gestor@empresa.com", "gestor")
	seedConta("gestao-mux-adm@empresa.com", "adm")

	const uuidAleatorio = "11111111-1111-1111-1111-111111111111"

	t.Run("papel abaixo de gestor -> 403 nas duas rotas", func(t *testing.T) {
		for _, email := range []string{"gestao-mux-usuario@empresa.com", "gestao-mux-almox@empresa.com"} {
			token := tokenDe(email)

			wDesat := despachar("/api/usuarios/"+uuidAleatorio+"/desativacao", token, `{"ativo":false}`)
			if wDesat.Code != http.StatusForbidden {
				t.Errorf("%s POST .../desativacao: status = %d, want 403 (body=%s)", email, wDesat.Code, wDesat.Body.String())
			}
			wReb := despachar("/api/usuarios/"+uuidAleatorio+"/rebaixamento", token, "")
			if wReb.Code != http.StatusForbidden {
				t.Errorf("%s POST .../rebaixamento: status = %d, want 403 (body=%s)", email, wReb.Code, wReb.Body.String())
			}
		}
	})

	t.Run("gestor/adm passam do gate", func(t *testing.T) {
		for _, email := range []string{"gestao-mux-gestor@empresa.com", "gestao-mux-adm@empresa.com"} {
			token := tokenDe(email)

			// uuid válido porém inexistente: o handler executou (passou do
			// RequireRole) e devolveu 404, nunca 403.
			wDesat := despachar("/api/usuarios/"+uuidAleatorio+"/desativacao", token, `{"ativo":false}`)
			if wDesat.Code != http.StatusNotFound {
				t.Errorf("%s POST .../desativacao: status = %d, want 404 (body=%s)", email, wDesat.Code, wDesat.Body.String())
			}
			wReb := despachar("/api/usuarios/"+uuidAleatorio+"/rebaixamento", token, "")
			if wReb.Code != http.StatusNotFound {
				t.Errorf("%s POST .../rebaixamento: status = %d, want 404 (body=%s)", email, wReb.Code, wReb.Body.String())
			}
		}
	})
}

// TestNewMux_PromocoesRotasAutenticadasAlcancamHandlers prova, pela mesma
// instância de newMux usada por main(), que as duas rotas só-RequireAuth
// (POST /api/promocoes e GET /api/promocoes/minha) chegam de fato aos seus
// handlers com um token válido — não só que devolvem 401 sem token (Story
// 1.7). Sem estes casos, trocar SolicitarPromocaoHandler por
// MinhaSolicitacaoHandler em newMux (ou pendurar o handler no verbo errado)
// compila e deixa a suíte inteira verde: TestNewMux_RegistraRotasDeAutenticacao
// só exercita o ramo sem-token e os testes de handler recompõem o middleware
// à mão, sem passar por newMux.
func TestNewMux_PromocoesRotasAutenticadasAlcancamHandlers(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	mux := newMux(db, emailCfg, jwtSecret)

	const senha = "senha-123456"
	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
		 VALUES ('Conta Teste', 'promo-mux-auth@empresa.com', $1, 'usuario', true, true)`,
		string(hash),
	); err != nil {
		t.Fatalf("insert conta: %v", err)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"email":"promo-mux-auth@empresa.com","senha":"`+senha+`"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	mux.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200 (body=%s)", loginRec.Code, loginRec.Body.String())
	}
	var loginBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginBody); err != nil {
		t.Fatalf("login: decode: %v", err)
	}

	despachar := func(metodo, caminho string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(metodo, caminho, nil)
		req.Header.Set("Authorization", "Bearer "+loginBody.Token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	// GET /api/promocoes/minha antes de qualquer solicitação: o handler
	// executou e devolveu {"solicitacao": null}, não 401/500 nem o corpo de
	// outra rota.
	wMinhaAntes := despachar(http.MethodGet, "/api/promocoes/minha")
	if wMinhaAntes.Code != http.StatusOK {
		t.Fatalf("GET /api/promocoes/minha (antes): status = %d, want 200 (body=%s)", wMinhaAntes.Code, wMinhaAntes.Body.String())
	}
	if got := strings.TrimSpace(wMinhaAntes.Body.String()); got != `{"solicitacao":null}` {
		t.Errorf("GET /api/promocoes/minha (antes): body = %s, want {\"solicitacao\":null}", got)
	}

	// POST /api/promocoes: o handler executou, derivou o alvo do papel da
	// sessão e persistiu uma linha.
	wSolicitar := despachar(http.MethodPost, "/api/promocoes")
	if wSolicitar.Code != http.StatusCreated {
		t.Fatalf("POST /api/promocoes: status = %d, want 201 (body=%s)", wSolicitar.Code, wSolicitar.Body.String())
	}
	var solicitarBody struct {
		Solicitacao struct {
			ID        string `json:"id"`
			PapelAlvo string `json:"papel_alvo"`
			Status    string `json:"status"`
		} `json:"solicitacao"`
	}
	if err := json.Unmarshal(wSolicitar.Body.Bytes(), &solicitarBody); err != nil {
		t.Fatalf("POST /api/promocoes: decode: %v (body=%s)", err, wSolicitar.Body.String())
	}
	if solicitarBody.Solicitacao.PapelAlvo != "almoxarife" || solicitarBody.Solicitacao.Status != "pendente" {
		t.Errorf("POST /api/promocoes: solicitacao = %+v, want papel_alvo=almoxarife status=pendente", solicitarBody.Solicitacao)
	}
	var linhas int
	if err := db.QueryRow(
		`SELECT count(*) FROM solicitacoes_promocao WHERE papel_alvo = 'almoxarife' AND status = 'pendente'`,
	).Scan(&linhas); err != nil {
		t.Fatalf("count solicitacoes_promocao: %v", err)
	}
	if linhas != 1 {
		t.Errorf("linhas pendentes gravadas = %d, want 1", linhas)
	}

	// GET /api/promocoes/minha agora reflete a solicitação recém-criada —
	// prova que essa rota chega ao MinhaSolicitacaoHandler, não a outro.
	wMinhaDepois := despachar(http.MethodGet, "/api/promocoes/minha")
	if wMinhaDepois.Code != http.StatusOK {
		t.Fatalf("GET /api/promocoes/minha (depois): status = %d, want 200 (body=%s)", wMinhaDepois.Code, wMinhaDepois.Body.String())
	}
	var minhaBody struct {
		Solicitacao *struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"solicitacao"`
	}
	if err := json.Unmarshal(wMinhaDepois.Body.Bytes(), &minhaBody); err != nil {
		t.Fatalf("GET /api/promocoes/minha (depois): decode: %v", err)
	}
	if minhaBody.Solicitacao == nil || minhaBody.Solicitacao.ID != solicitarBody.Solicitacao.ID {
		t.Errorf("GET /api/promocoes/minha (depois): solicitacao = %+v, want id = %q", minhaBody.Solicitacao, solicitarBody.Solicitacao.ID)
	}
}

// TestRunMigrations_SolicitacoesPromocaoSchema prova que a migration 000004
// (Story 1.7) cria os CHECK constraints e os índices de solicitacoes_promocao
// — mesmo precedente das asserções de schema das migrations anteriores neste
// arquivo.
func TestRunMigrations_SolicitacoesPromocaoSchema(t *testing.T) {
	db := testDB(t)

	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	var solicitanteID string
	if err := db.QueryRow(
		`INSERT INTO usuarios (nome, email, papel) VALUES ('x', 'promo-schema@example.com', 'usuario') RETURNING id`,
	).Scan(&solicitanteID); err != nil {
		t.Fatalf("insert usuario: %v", err)
	}

	t.Run("papel_alvo rejeita valor fora do enum", func(t *testing.T) {
		_, err := db.Exec(
			`INSERT INTO solicitacoes_promocao (solicitante_id, papel_alvo) VALUES ($1, 'adm')`, solicitanteID,
		)
		if err == nil {
			t.Error("esperava falha ao inserir papel_alvo fora do CHECK ('almoxarife','gestor'), mas o insert teve sucesso")
		}
	})

	t.Run("status rejeita valor fora do enum", func(t *testing.T) {
		_, err := db.Exec(
			`INSERT INTO solicitacoes_promocao (solicitante_id, papel_alvo, status) VALUES ($1, 'almoxarife', 'cancelada')`,
			solicitanteID,
		)
		if err == nil {
			t.Error("esperava falha ao inserir status fora do CHECK ('pendente','aprovada','rejeitada'), mas o insert teve sucesso")
		}
	})

	t.Run("CHECK de consistência: pendente com decidido_em preenchido é rejeitado", func(t *testing.T) {
		_, err := db.Exec(
			`INSERT INTO solicitacoes_promocao (solicitante_id, papel_alvo, status, decidido_em)
			 VALUES ($1, 'almoxarife', 'pendente', now())`,
			solicitanteID,
		)
		if err == nil {
			t.Error("esperava falha: status='pendente' exige decidido_em NULL")
		}
	})

	t.Run("CHECK de consistência: decidida sem decidido_em é rejeitada", func(t *testing.T) {
		_, err := db.Exec(
			`INSERT INTO solicitacoes_promocao (solicitante_id, papel_alvo, status) VALUES ($1, 'almoxarife', 'aprovada')`,
			solicitanteID,
		)
		if err == nil {
			t.Error("esperava falha: status != 'pendente' exige decidido_em preenchido")
		}
	})

	t.Run("índices esperados existem", func(t *testing.T) {
		indices := []string{
			"idx_solicitacoes_promocao_pendente_unica",
			"idx_solicitacoes_promocao_solicitante",
			"idx_solicitacoes_promocao_decidido_por",
			"idx_solicitacoes_promocao_status",
		}
		for _, nome := range indices {
			var indexDef string
			if err := db.QueryRow(`SELECT indexdef FROM pg_indexes WHERE indexname = $1`, nome).Scan(&indexDef); err != nil {
				t.Errorf("índice %q não encontrado: %v", nome, err)
				continue
			}
			if nome == "idx_solicitacoes_promocao_pendente_unica" {
				if !strings.Contains(indexDef, "UNIQUE") || !strings.Contains(indexDef, "WHERE") {
					t.Errorf("%s deveria ser um índice parcial UNIQUE, definição = %q", nome, indexDef)
				}
			}
		}
	})
}

// TestHealthHandler_Unhealthy prova o ramo de erro do handler: se o ping ao
// banco falhar, a resposta é 503 com status "unhealthy" — o healthcheck do
// docker-compose (wget --spider contra /api/health) depende desse contrato
// para não reportar o container saudável com o banco fora do ar.
func TestHealthHandler_Unhealthy(t *testing.T) {
	db := testDB(t)
	db.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	healthHandler(db)(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var resp healthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("falha ao decodificar corpo: %v", err)
	}
	if resp.Status != "unhealthy" {
		t.Errorf("status body = %q, want %q", resp.Status, "unhealthy")
	}
}
