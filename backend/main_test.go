package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // HMAC-SHA1 é o algoritmo do TOTP (RFC 6238/4226), não hashing de segredo.
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"

	"stockflow/backend/iam"
	"stockflow/backend/services"
)

// totpCodigoTesteAtual gera um código TOTP válido para `segredo` no instante
// atual — mesmo algoritmo HOTP/RFC 6238 de services.ValidarCodigoTOTP,
// reimplementado aqui porque package main não acessa o gerador não-exportado
// de services (mesmo padrão de duplicação de middleware/auth_test.go, que
// assina seus próprios JWTs de teste em vez de chamar a função não-exportada
// equivalente de services).
func totpCodigoTesteAtual(t *testing.T, segredo string) string {
	t.Helper()
	chave, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(segredo)
	if err != nil {
		t.Fatalf("segredo TOTP de teste inválido: %v", err)
	}
	contador := uint64(time.Now().UTC().Unix()) / 30
	var contadorBytes [8]byte
	binary.BigEndian.PutUint64(contadorBytes[:], contador)
	mac := hmac.New(sha1.New, chave)
	mac.Write(contadorBytes[:])
	soma := mac.Sum(nil)
	offset := soma[len(soma)-1] & 0x0f
	truncado := (uint32(soma[offset])&0x7f)<<24 |
		uint32(soma[offset+1])<<16 |
		uint32(soma[offset+2])<<8 |
		uint32(soma[offset+3])
	return fmt.Sprintf("%06d", truncado%1000000)
}

// seedContaMux insere uma conta ativa/verificada com papel e senha
// controlados, para os testes de composição de newMux abaixo (Story 1.5/1.7/
// 1.8). Story 1.11: papel gestor+ nasce com `mfa_habilitado=true` e um
// segredo TOTP real, guardado em `segredos[email]` — necessário para
// tokenDeMux conseguir completar o segundo fator (estes testes provam o gate
// de PAPEL, não o de MFA, que tem cobertura dedicada em
// middleware/roles_test.go; sem MFA configurado, o login de uma conta
// gestor/adm nem chegaria à rota que estes testes querem exercitar).
func seedContaMux(t *testing.T, db *sql.DB, email, papel, senha string, segredos map[string]string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	mfaHabilitado := services.RankPapel(papel) >= services.RankPapel(services.PapelGestor)
	var mfaSecret sql.NullString
	if mfaHabilitado {
		segredo, err := services.GerarSegredoTOTP()
		if err != nil {
			t.Fatalf("GerarSegredoTOTP: %v", err)
		}
		segredos[email] = segredo
		mfaSecret = sql.NullString{String: segredo, Valid: true}
	}
	if _, err := db.Exec(
		`INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, mfa_habilitado, mfa_secret)
		 VALUES ('Conta Teste', $1, $2, $3, true, true, $4, $5)`,
		email, string(hash), papel, mfaHabilitado, mfaSecret,
	); err != nil {
		t.Fatalf("insert conta %q (%s): %v", email, papel, err)
	}
}

// tokenDeMux faz login real (POST /api/auth/login) através de `mux` e, se a
// conta exigir MFA (Story 1.11: resposta `mfaRequerido:true`), completa o
// segundo fator via POST /api/auth/mfa/verificar usando o segredo gravado por
// seedContaMux — sempre devolve um access token de sessão de verdade, o mesmo
// caminho que o frontend percorre.
func tokenDeMux(t *testing.T, mux *http.ServeMux, email, senha string, segredos map[string]string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"email":"`+email+`","senha":"`+senha+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login %q: status = %d, want 200 (body=%s)", email, w.Code, w.Body.String())
	}
	var body struct {
		Token        string `json:"token"`
		MfaRequerido bool   `json:"mfaRequerido"`
		MfaToken     string `json:"mfaToken"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("login %q: decode: %v", email, err)
	}
	if !body.MfaRequerido {
		return body.Token
	}

	segredo, ok := segredos[email]
	if !ok {
		t.Fatalf("login %q: mfaRequerido=true sem segredo TOTP conhecido para essa conta", email)
	}
	codigo := totpCodigoTesteAtual(t, segredo)
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/mfa/verificar",
		strings.NewReader(`{"mfaToken":"`+body.MfaToken+`","codigo":"`+codigo+`"}`))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("mfa/verificar %q: status = %d, want 200 (body=%s)", email, w2.Code, w2.Body.String())
	}
	var body2 struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &body2); err != nil {
		t.Fatalf("mfa/verificar %q: decode: %v", email, err)
	}
	return body2.Token
}

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
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

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
		{
			nome:         "logs-acesso sem token chega no RequireAuth antes de RequireRole(adm)",
			metodo:       http.MethodGet,
			caminho:      "/api/logs-acesso",
			statusQuerAo: http.StatusUnauthorized,
		},
		{
			nome:         "movimentacoes sem token chega no RequireAuth antes de RequireRole(almoxarife)",
			metodo:       http.MethodGet,
			caminho:      "/api/movimentacoes",
			statusQuerAo: http.StatusUnauthorized,
		},
		{
			nome:         "estoques POST sem token chega no RequireAuth antes de RequireRole",
			metodo:       http.MethodPost,
			caminho:      "/api/estoques",
			corpo:        `{"nome":"Canteiro A"}`,
			statusQuerAo: http.StatusUnauthorized,
		},
		{
			nome:         "estoques GET sem token chega no RequireAuth (rota sem RequireRole)",
			metodo:       http.MethodGet,
			caminho:      "/api/estoques",
			statusQuerAo: http.StatusUnauthorized,
		},
		{
			nome:         "estoques DELETE sem token chega no RequireAuth antes de RequireRole",
			metodo:       http.MethodDelete,
			caminho:      "/api/estoques/algum-id",
			statusQuerAo: http.StatusUnauthorized,
		},
		{
			nome:         "sso/config sempre registrada (sem IAM_* -> enabled:false)",
			metodo:       http.MethodGet,
			caminho:      "/api/auth/sso/config",
			statusQuerAo: http.StatusOK,
		},
		{
			nome:         "logout sempre registrado (sem cookie -> 204)",
			metodo:       http.MethodPost,
			caminho:      "/api/auth/logout",
			statusQuerAo: http.StatusNoContent,
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
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

	getUsuarios := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/usuarios", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	seedContaMux(t, db, "mux-usuario@empresa.com", "usuario", senha, segredos)
	seedContaMux(t, db, "mux-almox@empresa.com", "almoxarife", senha, segredos)
	seedContaMux(t, db, "mux-gestor@empresa.com", "gestor", senha, segredos)
	seedContaMux(t, db, "mux-adm@empresa.com", "adm", senha, segredos)

	t.Run("papel insuficiente -> 403 FORBIDDEN", func(t *testing.T) {
		for _, email := range []string{"mux-usuario@empresa.com", "mux-almox@empresa.com"} {
			w := getUsuarios(tokenDeMux(t, mux, email, senha, segredos))
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
			w := getUsuarios(tokenDeMux(t, mux, email, senha, segredos))
			if w.Code != http.StatusOK {
				t.Fatalf("%s: status = %d, want %d (body=%s)", email, w.Code, http.StatusOK, w.Body.String())
			}
		}
	})
}

// TestNewMux_EstoquesRotaCarregaRequireRole prova, despachando pela mesma
// instância de newMux usada por main() (Story 2.1), que:
//   - POST /api/estoques está atrás de RequireRole(almoxarife): token
//     `usuario` -> 403 FORBIDDEN; token `almoxarife`/`gestor`/`adm` passa do
//     gate (201, ou 400 num payload inválido — nunca 403).
//   - DELETE /api/estoques/{id} está atrás de RequireRole(almoxarife): token
//     `usuario` -> 403 FORBIDDEN; token `almoxarife`/`gestor`/`adm` passa do
//     gate (204 num Estoque criado, ou 404 num id aleatório — nunca 403).
//   - GET /api/estoques NÃO leva RequireRole: um token `usuario` -> 200.
//
// Sem estes casos, remover `middleware.RequireRole(services.PapelAlmoxarife)`
// do POST — ou adicioná-lo indevidamente ao GET — deixaria a suíte verde (o
// único caso pré-existente em main_test.go, sem token -> 401, é produzido só
// por RequireAuth).
func TestNewMux_EstoquesRotaCarregaRequireRole(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}
	// `produto_estoque`/`produtos` entram na mesma TRUNCATE que `estoques`:
	// Postgres recusa truncar uma tabela referenciada por FK de outra (mesmo
	// vazia) a menos que todas entrem na mesma instrução (Story 3.1).
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("truncate estoques: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

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

	seedContaMux(t, db, "estq-mux-usuario@empresa.com", "usuario", senha, segredos)
	seedContaMux(t, db, "estq-mux-almox@empresa.com", "almoxarife", senha, segredos)
	seedContaMux(t, db, "estq-mux-gestor@empresa.com", "gestor", senha, segredos)
	seedContaMux(t, db, "estq-mux-adm@empresa.com", "adm", senha, segredos)

	t.Run("POST: papel usuario -> 403 FORBIDDEN", func(t *testing.T) {
		token := tokenDeMux(t, mux, "estq-mux-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodPost, "/api/estoques", token, `{"nome":"Canteiro Vetado"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
		}
		var env struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Error.Code != "FORBIDDEN" {
			t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
		}
	})

	t.Run("POST e DELETE: almoxarife/gestor/adm passam do gate (nunca 403)", func(t *testing.T) {
		// Um único tokenDeMux por conta: gestor/adm exigem MFA e o segundo
		// fator TOTP não pode ser reapresentado dentro da mesma janela de 30s,
		// então POST e DELETE compartilham o mesmo token/subteste.
		casos := []struct{ email, nome string }{
			{"estq-mux-almox@empresa.com", "Canteiro Almox"},
			{"estq-mux-gestor@empresa.com", "Canteiro Gestor"},
			{"estq-mux-adm@empresa.com", "Canteiro Adm"},
		}
		for _, c := range casos {
			token := tokenDeMux(t, mux, c.email, senha, segredos)

			w := despachar(http.MethodPost, "/api/estoques", token, `{"nome":"`+c.nome+`"}`)
			if w.Code != http.StatusCreated {
				t.Errorf("%s: status = %d, want %d (body=%s)", c.email, w.Code, http.StatusCreated, w.Body.String())
			}
			var criado struct {
				Estoque struct {
					ID string `json:"id"`
				} `json:"estoque"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &criado); err != nil {
				t.Fatalf("%s: decode estoque criado: %v", c.email, err)
			}
			// Payload inválido pelo mesmo caminho: o handler executou (400), não parou no 403.
			wInvalido := despachar(http.MethodPost, "/api/estoques", token, `{"nome":"   "}`)
			if wInvalido.Code != http.StatusBadRequest {
				t.Errorf("%s (payload inválido): status = %d, want %d (body=%s)", c.email, wInvalido.Code, http.StatusBadRequest, wInvalido.Body.String())
			}

			// DELETE pelo mesmo caminho: o Estoque recém-criado -> 204 (nunca 403).
			wDel := despachar(http.MethodDelete, "/api/estoques/"+criado.Estoque.ID, token, "")
			if wDel.Code != http.StatusNoContent {
				t.Errorf("%s: DELETE status = %d, want %d (body=%s)", c.email, wDel.Code, http.StatusNoContent, wDel.Body.String())
			}
			// Id aleatório pelo mesmo caminho: o handler executou (404), não parou no 403.
			wAusente := despachar(http.MethodDelete, "/api/estoques/00000000-0000-4000-8000-000000000000", token, "")
			if wAusente.Code != http.StatusNotFound {
				t.Errorf("%s (id ausente): status = %d, want %d (body=%s)", c.email, wAusente.Code, http.StatusNotFound, wAusente.Body.String())
			}
		}
	})

	t.Run("GET: papel usuario -> 200 (rota sem RequireRole)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "estq-mux-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodGet, "/api/estoques", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("DELETE: papel usuario -> 403 FORBIDDEN", func(t *testing.T) {
		token := tokenDeMux(t, mux, "estq-mux-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodDelete, "/api/estoques/00000000-0000-4000-8000-000000000000", token, "")
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
		}
		var env struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Error.Code != "FORBIDDEN" {
			t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
		}
	})

}

// TestNewMux_ProdutosRotaCarregaRequireRole prova, despachando pela mesma
// instância de newMux usada por main() (Story 3.1; GET nomenclatura-templates
// acrescentado pela Story 3.2), que:
//   - POST /api/produtos está atrás de RequireRole(almoxarife): token
//     `usuario` -> 403 FORBIDDEN; token `almoxarife` passa do gate (201,
//     nunca 403).
//   - GET /api/categorias e GET /api/nomenclatura-templates NÃO levam
//     RequireRole: um token `usuario` -> 200 nos dois.
//
// Sem estes casos, remover `middleware.RequireRole(services.PapelAlmoxarife)`
// do POST — ou adicioná-lo indevidamente a qualquer um dos GETs — deixaria a
// suíte verde (o único caso pré-existente em main_test.go, sem token -> 401,
// é produzido só por RequireAuth).
func TestNewMux_ProdutosRotaCarregaRequireRole(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("truncate produtos: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

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

	seedContaMux(t, db, "prod-mux-usuario@empresa.com", "usuario", senha, segredos)
	seedContaMux(t, db, "prod-mux-almox@empresa.com", "almoxarife", senha, segredos)

	var categoriaID string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE codigo = '04.001'`).Scan(&categoriaID); err != nil {
		t.Fatalf("buscar categoria de seed: %v", err)
	}
	estoque, err := services.CriarEstoque(db, "Canteiro Mux Produtos")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	t.Run("POST: papel usuario -> 403 FORBIDDEN", func(t *testing.T) {
		token := tokenDeMux(t, mux, "prod-mux-usuario@empresa.com", senha, segredos)
		corpo := `{"nome":"Produto Vetado","categoria_id":"` + categoriaID + `","estoque_id":"` + estoque.ID + `","quantidade_inicial":1}`
		w := despachar(http.MethodPost, "/api/produtos", token, corpo)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
		}
		var env struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Error.Code != "FORBIDDEN" {
			t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
		}
	})

	t.Run("POST: almoxarife passa do gate (nunca 403)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "prod-mux-almox@empresa.com", senha, segredos)
		corpo := `{"nome":"Produto Almox","categoria_id":"` + categoriaID + `","estoque_id":"` + estoque.ID + `","quantidade_inicial":1}`
		w := despachar(http.MethodPost, "/api/produtos", token, corpo)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
		}
	})

	t.Run("GET categorias: papel usuario -> 200 (rota sem RequireRole)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "prod-mux-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodGet, "/api/categorias", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
		}
	})

	// Story 3.2: mesma prova que o caso de categorias acima, agora para
	// GET /api/nomenclatura-templates — sem isto, um RequireRole indevido
	// adicionado a esta rota (bloqueando `usuario`, que o formulário de
	// cadastro consulta) deixaria a suíte verde: os testes de
	// handlers/produtos_test.go montam seu próprio mux local e nunca
	// despacham pela composição real de main.go.
	t.Run("GET nomenclatura-templates: papel usuario -> 200 (rota sem RequireRole)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "prod-mux-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodGet, "/api/nomenclatura-templates", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
		}
	})
}

// TestNewMux_ProdutosRenomearRotaCarregaRequireRole prova, despachando pela
// mesma instância de newMux usada por main() (Story 3.2), que
// POST /api/produtos/{id}/renomear está atrás de RequireRole(almoxarife):
// token `usuario` -> 403 FORBIDDEN; token `almoxarife` passa do gate (nunca
// 403, ainda que o corpo devolva outro status por outro motivo de negócio).
//
// Sem este caso, remover `middleware.RequireRole(services.PapelAlmoxarife)`
// de POST /api/produtos/{id}/renomear deixaria a suíte verde (o único caso
// pré-existente em main_test.go, sem token -> 401, é produzido só por
// RequireAuth).
func TestNewMux_ProdutosRenomearRotaCarregaRequireRole(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("truncate produtos: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

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

	seedContaMux(t, db, "prod-renomear-usuario@empresa.com", "usuario", senha, segredos)
	seedContaMux(t, db, "prod-renomear-almox@empresa.com", "almoxarife", senha, segredos)

	var categoriaID string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE codigo = '04.001'`).Scan(&categoriaID); err != nil {
		t.Fatalf("buscar categoria de seed: %v", err)
	}
	estoque, err := services.CriarEstoque(db, "Canteiro Mux Renomear")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	produto, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome:              "Produto Mux Renomear",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	t.Run("papel usuario -> 403 FORBIDDEN", func(t *testing.T) {
		token := tokenDeMux(t, mux, "prod-renomear-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodPost, "/api/produtos/"+produto.ID+"/renomear", token, `{"nome":"Nome Vetado"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
		}
		var env struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Error.Code != "FORBIDDEN" {
			t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
		}
	})

	t.Run("almoxarife passa do gate (nunca 403)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "prod-renomear-almox@empresa.com", senha, segredos)
		w := despachar(http.MethodPost, "/api/produtos/"+produto.ID+"/renomear", token, `{"nome":"Nome Renomeado Mux"}`)
		if w.Code == http.StatusForbidden {
			t.Fatalf("status = %d, want != 403 (body=%s)", w.Code, w.Body.String())
		}
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
		}
	})
}

// TestNewMux_ProdutosBaixaRotaCarregaRequireRole prova, despachando pela
// mesma instância de newMux usada por main() (Story 5.1), que
// POST /api/produtos/{id}/estoques/{estoqueId}/baixa está atrás de
// RequireRole(almoxarife): token `usuario` -> 403 FORBIDDEN; token
// `almoxarife` passa do gate (201 de verdade, nunca 403).
//
// Sem este caso, remover `middleware.RequireRole(services.PapelAlmoxarife)`
// de POST /api/produtos/{id}/estoques/{estoqueId}/baixa deixaria a suíte
// verde (o único caso pré-existente em main_test.go, sem token -> 401, é
// produzido só por RequireAuth).
func TestNewMux_ProdutosBaixaRotaCarregaRequireRole(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("truncate produtos: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

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

	seedContaMux(t, db, "prod-baixa-usuario@empresa.com", "usuario", senha, segredos)
	seedContaMux(t, db, "prod-baixa-almox@empresa.com", "almoxarife", senha, segredos)

	var categoriaID string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE codigo = '04.001'`).Scan(&categoriaID); err != nil {
		t.Fatalf("buscar categoria de seed: %v", err)
	}
	estoque, err := services.CriarEstoque(db, "Canteiro Mux Baixa")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	produto, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome:              "Produto Mux Baixa",
		CategoriaID:       categoriaID,
		EstoqueID:         estoque.ID,
		QuantidadeInicial: 10,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}
	caminho := "/api/produtos/" + produto.ID + "/estoques/" + estoque.ID + "/baixa"

	t.Run("papel usuario -> 403 FORBIDDEN", func(t *testing.T) {
		token := tokenDeMux(t, mux, "prod-baixa-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodPost, caminho, token, `{"quantidade":1}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
		}
		var env struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Error.Code != "FORBIDDEN" {
			t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
		}
	})

	t.Run("almoxarife passa do gate (nunca 403)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "prod-baixa-almox@empresa.com", senha, segredos)
		w := despachar(http.MethodPost, caminho, token, `{"quantidade":1}`)
		if w.Code == http.StatusForbidden {
			t.Fatalf("status = %d, want != 403 (body=%s)", w.Code, w.Body.String())
		}
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
		}
	})
}

// TestNewMux_ProdutosTransferenciaRotaCarregaRequireRole prova, despachando
// pela mesma instância de newMux usada por main() (Story 5.2), que
// POST /api/produtos/{id}/estoques/{estoqueId}/transferencia está atrás de
// RequireRole(almoxarife): token `usuario` -> 403 FORBIDDEN; token
// `almoxarife` passa do gate (201 de verdade, nunca 403). Molde de
// TestNewMux_ProdutosBaixaRotaCarregaRequireRole.
//
// Sem este caso, remover `middleware.RequireRole(services.PapelAlmoxarife)`
// de POST /api/produtos/{id}/estoques/{estoqueId}/transferencia deixaria a
// suíte verde (o único caso pré-existente em main_test.go, sem token ->
// 401, é produzido só por RequireAuth).
func TestNewMux_ProdutosTransferenciaRotaCarregaRequireRole(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("truncate produtos: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

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

	seedContaMux(t, db, "prod-transf-usuario@empresa.com", "usuario", senha, segredos)
	seedContaMux(t, db, "prod-transf-almox@empresa.com", "almoxarife", senha, segredos)

	var categoriaID string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE codigo = '04.001'`).Scan(&categoriaID); err != nil {
		t.Fatalf("buscar categoria de seed: %v", err)
	}
	estoqueOrigem, err := services.CriarEstoque(db, "Canteiro Mux Transferencia Origem")
	if err != nil {
		t.Fatalf("seed CriarEstoque origem: %v", err)
	}
	estoqueDestino, err := services.CriarEstoque(db, "Canteiro Mux Transferencia Destino")
	if err != nil {
		t.Fatalf("seed CriarEstoque destino: %v", err)
	}
	produto, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome:              "Produto Mux Transferencia",
		CategoriaID:       categoriaID,
		EstoqueID:         estoqueOrigem.ID,
		QuantidadeInicial: 10,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}
	caminho := "/api/produtos/" + produto.ID + "/estoques/" + estoqueOrigem.ID + "/transferencia"
	corpo := `{"estoqueDestinoId":"` + estoqueDestino.ID + `","quantidade":1}`

	t.Run("papel usuario -> 403 FORBIDDEN", func(t *testing.T) {
		token := tokenDeMux(t, mux, "prod-transf-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodPost, caminho, token, corpo)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
		}
		var env struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Error.Code != "FORBIDDEN" {
			t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
		}
	})

	t.Run("almoxarife passa do gate (nunca 403)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "prod-transf-almox@empresa.com", senha, segredos)
		w := despachar(http.MethodPost, caminho, token, corpo)
		if w.Code == http.StatusForbidden {
			t.Fatalf("status = %d, want != 403 (body=%s)", w.Code, w.Body.String())
		}
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
		}
	})
}

// TestNewMux_MovimentacoesRotaCarregaRequireRole prova, despachando pela
// mesma instância de newMux usada por main() (Story 5.3), que
// GET /api/movimentacoes está atrás de RequireRole(almoxarife): token
// `usuario` -> 403 FORBIDDEN; token `almoxarife`/`gestor`/`adm` passa do
// gate (200 de verdade, nunca 403). Molde de
// TestNewMux_EstoquesRotaCarregaRequireRole.
//
// Sem este caso, remover `middleware.RequireRole(services.PapelAlmoxarife)`
// de GET /api/movimentacoes deixaria a suíte verde (o único caso
// pré-existente em main_test.go, sem token -> 401, é produzido só por
// RequireAuth).
func TestNewMux_MovimentacoesRotaCarregaRequireRole(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("truncate produtos: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

	despachar := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/movimentacoes", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	seedContaMux(t, db, "mov-mux-usuario@empresa.com", "usuario", senha, segredos)
	seedContaMux(t, db, "mov-mux-almox@empresa.com", "almoxarife", senha, segredos)
	seedContaMux(t, db, "mov-mux-gestor@empresa.com", "gestor", senha, segredos)
	seedContaMux(t, db, "mov-mux-adm@empresa.com", "adm", senha, segredos)

	t.Run("papel usuario -> 403 FORBIDDEN", func(t *testing.T) {
		token := tokenDeMux(t, mux, "mov-mux-usuario@empresa.com", senha, segredos)
		w := despachar(token)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
		}
		var env struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Error.Code != "FORBIDDEN" {
			t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
		}
	})

	t.Run("almoxarife/gestor/adm passam do gate (nunca 403)", func(t *testing.T) {
		for _, email := range []string{
			"mov-mux-almox@empresa.com",
			"mov-mux-gestor@empresa.com",
			"mov-mux-adm@empresa.com",
		} {
			token := tokenDeMux(t, mux, email, senha, segredos)
			w := despachar(token)
			if w.Code == http.StatusForbidden {
				t.Fatalf("%s: status = %d, want != 403 (body=%s)", email, w.Code, w.Body.String())
			}
			if w.Code != http.StatusOK {
				t.Fatalf("%s: status = %d, want %d (body=%s)", email, w.Code, http.StatusOK, w.Body.String())
			}
		}
	})
}

// construirXLSXMux monta um `.xlsx` real em memória (via excelize) a partir
// de uma matriz de células — mesmo helper de handlers/importacoes_test.go,
// reimplementado aqui porque package main não importa o pacote de testes de
// handlers.
func construirXLSXMux(t *testing.T, linhas [][]string) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	planilha := f.GetSheetList()[0]
	for i, linha := range linhas {
		for j, valor := range linha {
			célula, err := excelize.CoordinatesToCellName(j+1, i+1)
			if err != nil {
				t.Fatalf("CoordinatesToCellName(%d,%d): %v", j+1, i+1, err)
			}
			if err := f.SetCellStr(planilha, célula, valor); err != nil {
				t.Fatalf("SetCellStr(%s): %v", célula, err)
			}
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("WriteToBuffer: %v", err)
	}
	return buf.Bytes()
}

// TestNewMux_ImportacoesRotaCarregaRequireRole prova, despachando pela mesma
// instância de newMux usada por main() (Story 3.3), que os 3 endpoints de
// importação estão atrás de RequireRole(almoxarife):
//   - POST /api/importacoes: token `usuario` -> 403 FORBIDDEN; token
//     `almoxarife` passa do gate (201 de verdade, nunca 403).
//   - GET /api/importacoes/ultima: token `usuario` -> 403 FORBIDDEN; token
//     `almoxarife` passa do gate (200, nunca 403).
//   - POST /api/importacoes/{id}/continuar: token `usuario` -> 403 FORBIDDEN;
//     token `almoxarife` passa do gate (404 para id inexistente é um motivo
//     de negócio válido, mas nunca 403).
//
// Sem estes casos, remover `middleware.RequireRole(services.PapelAlmoxarife)`
// de qualquer um dos 3 — ou registrá-los sem RequireAuth — deixaria a suíte
// verde (o único caso pré-existente em main_test.go, sem token -> 401, é
// produzido só por RequireAuth).
func TestNewMux_ImportacoesRotaCarregaRequireRole(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("truncate produtos: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}
	seedContaMux(t, db, "importacao-mux-usuario@empresa.com", "usuario", senha, segredos)
	seedContaMux(t, db, "importacao-mux-almox@empresa.com", "almoxarife", senha, segredos)

	// A coluna "Categoria" da planilha casa por NOME, não por código
	// (services.CriarImportacao, Design Notes da spec-3-3).
	var categoria string
	if err := db.QueryRow(`SELECT nome FROM categorias WHERE codigo = '04.001'`).Scan(&categoria); err != nil {
		t.Fatalf("buscar categoria de seed: %v", err)
	}

	despacharMultipart := func(token string, arquivo []byte) *httptest.ResponseRecorder {
		corpo := &bytes.Buffer{}
		writer := multipart.NewWriter(corpo)
		part, _ := writer.CreateFormFile("planilha", "planilha.xlsx")
		_, _ = part.Write(arquivo)
		_ = writer.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/importacoes", corpo)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	despachar := func(metodo, caminho, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(metodo, caminho, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	xlsx := construirXLSXMux(t, [][]string{
		services.CabecalhoEsperado,
		{"Produto Mux Importação", "SKU-MUX-IMP", categoria, "", "", "", "", "", "", "", "", "", "", "1", "Canteiro Mux Importação", ""},
	})

	t.Run("POST importacoes: papel usuario -> 403 FORBIDDEN", func(t *testing.T) {
		token := tokenDeMux(t, mux, "importacao-mux-usuario@empresa.com", senha, segredos)
		w := despacharMultipart(token, xlsx)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	t.Run("POST importacoes: almoxarife passa do gate (201 de verdade, nunca 403)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "importacao-mux-almox@empresa.com", senha, segredos)
		w := despacharMultipart(token, xlsx)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
		}
	})

	t.Run("GET importacoes/ultima: papel usuario -> 403 FORBIDDEN", func(t *testing.T) {
		token := tokenDeMux(t, mux, "importacao-mux-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodGet, "/api/importacoes/ultima", token)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	t.Run("GET importacoes/ultima: almoxarife passa do gate (200, nunca 403)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "importacao-mux-almox@empresa.com", senha, segredos)
		w := despachar(http.MethodGet, "/api/importacoes/ultima", token)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("POST importacoes/{id}/continuar: papel usuario -> 403 FORBIDDEN", func(t *testing.T) {
		token := tokenDeMux(t, mux, "importacao-mux-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodPost, "/api/importacoes/00000000-0000-0000-0000-000000000000/continuar", token)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	t.Run("POST importacoes/{id}/continuar: almoxarife passa do gate (nunca 403)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "importacao-mux-almox@empresa.com", senha, segredos)
		w := despachar(http.MethodPost, "/api/importacoes/00000000-0000-0000-0000-000000000000/continuar", token)
		if w.Code == http.StatusForbidden {
			t.Fatalf("status = %d, want != 403 (body=%s)", w.Code, w.Body.String())
		}
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d (id inexistente, body=%s)", w.Code, http.StatusNotFound, w.Body.String())
		}
	})
}

// TestNewMux_LogsAcessoRotaCarregaRequireRole prova, despachando pela mesma
// instância de newMux usada por main(), que GET /api/logs-acesso está atrás
// de RequireRole(services.PapelAdm) — e não só de RequireAuth nem de um gate
// de papel mais baixo. Um token de `usuario`/`almoxarife`/`gestor` recebe 403
// FORBIDDEN; só um `adm` recebe 200. Sem estes casos, trocar o argumento de
// RequireRole (ex. PapelGestor) ou remover o middleware de newMux deixaria a
// suíte verde — o único caso pré-existente em main_test.go (sem token -> 401)
// é insensível ao argumento de papel.
func TestNewMux_LogsAcessoRotaCarregaRequireRole(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

	getLogs := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/logs-acesso", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	seedContaMux(t, db, "mux-logs-usuario@empresa.com", "usuario", senha, segredos)
	seedContaMux(t, db, "mux-logs-almox@empresa.com", "almoxarife", senha, segredos)
	seedContaMux(t, db, "mux-logs-gestor@empresa.com", "gestor", senha, segredos)
	seedContaMux(t, db, "mux-logs-adm@empresa.com", "adm", senha, segredos)

	t.Run("papel abaixo de adm -> 403 FORBIDDEN", func(t *testing.T) {
		for _, email := range []string{"mux-logs-usuario@empresa.com", "mux-logs-almox@empresa.com", "mux-logs-gestor@empresa.com"} {
			w := getLogs(tokenDeMux(t, mux, email, senha, segredos))
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

	t.Run("adm -> 200", func(t *testing.T) {
		w := getLogs(tokenDeMux(t, mux, "mux-logs-adm@empresa.com", senha, segredos))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
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
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

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

	seedContaMux(t, db, "promo-mux-usuario@empresa.com", "usuario", senha, segredos)
	seedContaMux(t, db, "promo-mux-almox@empresa.com", "almoxarife", senha, segredos)
	seedContaMux(t, db, "promo-mux-gestor@empresa.com", "gestor", senha, segredos)
	seedContaMux(t, db, "promo-mux-adm@empresa.com", "adm", senha, segredos)

	const uuidAleatorio = "11111111-1111-1111-1111-111111111111"

	t.Run("papel abaixo de gestor -> 403 nas duas rotas", func(t *testing.T) {
		for _, email := range []string{"promo-mux-usuario@empresa.com", "promo-mux-almox@empresa.com"} {
			token := tokenDeMux(t, mux, email, senha, segredos)

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
			token := tokenDeMux(t, mux, email, senha, segredos)

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
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

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

	seedContaMux(t, db, "gestao-mux-usuario@empresa.com", "usuario", senha, segredos)
	seedContaMux(t, db, "gestao-mux-almox@empresa.com", "almoxarife", senha, segredos)
	seedContaMux(t, db, "gestao-mux-gestor@empresa.com", "gestor", senha, segredos)
	seedContaMux(t, db, "gestao-mux-adm@empresa.com", "adm", senha, segredos)

	const uuidAleatorio = "11111111-1111-1111-1111-111111111111"

	t.Run("papel abaixo de gestor -> 403 nas duas rotas", func(t *testing.T) {
		for _, email := range []string{"gestao-mux-usuario@empresa.com", "gestao-mux-almox@empresa.com"} {
			token := tokenDeMux(t, mux, email, senha, segredos)

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
			token := tokenDeMux(t, mux, email, senha, segredos)

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
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

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
// TestNewMux_SSOConfigSempreRegistrada prova que GET /api/auth/sso/config é
// registrado mesmo com iam.Config vazia (Story 1.9) e responde
// {"enabled":false} — a tela de Login depende disso para simplesmente não
// mostrar o botão de SSO num servidor sem realm configurado.
func TestNewMux_SSOConfigSempreRegistrada(t *testing.T) {
	db := testDB(t)
	fotosDir := t.TempDir()
	mux := newMux(db, services.CarregarEmailConfig(), []byte("segredo-de-teste"), iam.Config{}, fotosDir)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/sso/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["enabled"] != false {
		t.Fatalf("body = %v, want {\"enabled\":false}", body)
	}
}

// TestNewMux_SSOKeycloakRegistradaSomenteComConfig prova o registro
// condicional de POST /api/auth/sso/keycloak: sem IAM_BASE_URL a rota não
// existe (404); com um RealmURL setado ela existe e, sem token, o middleware
// `iam` responde 401.
func TestNewMux_SSOKeycloakRegistradaSomenteComConfig(t *testing.T) {
	db := testDB(t)
	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste")
	fotosDir := t.TempDir()

	t.Run("sem config -> 404", func(t *testing.T) {
		mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/sso/keycloak", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (rota não deveria existir sem IAM_BASE_URL)", w.Code)
		}
	})

	t.Run("com config -> rota existe, sem token 401", func(t *testing.T) {
		mux := newMux(db, emailCfg, jwtSecret, iam.Config{
			RealmURL:         "https://kc.example/realms/ferreiracosta",
			AllowedClientIDs: []string{"stockflow-web"},
		}, fotosDir)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/sso/keycloak", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
		}
	})
}

// TestNewMux_LogoutSempreRegistrada prova que POST /api/auth/logout existe
// independentemente da config de SSO e é idempotente sem cookie (204).
func TestNewMux_LogoutSempreRegistrada(t *testing.T) {
	db := testDB(t)
	fotosDir := t.TempDir()
	mux := newMux(db, services.CarregarEmailConfig(), []byte("segredo-de-teste"), iam.Config{}, fotosDir)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", w.Code, w.Body.String())
	}
}

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

// TestNewMux_ProdutosBuscaRotaSoRequireAuth prova, despachando pela mesma
// instância de newMux usada por main() (Story 4.1, spec-4-1), que GET
// /api/produtos/busca NÃO leva RequireRole: sem token -> 401 (RequireAuth);
// token `usuario` -> 200, NUNCA 403 — mesmo molde de
// TestNewMux_EstoquesRotaCarregaRequireRole para o caso "rota sem
// RequireRole". Sem este caso, adicionar indevidamente
// `middleware.RequireRole(...)` a esta rota deixaria a suíte verde (os
// testes de handlers/produtos_test.go montam seu próprio mux local e nunca
// despacham pela composição real de main.go).
func TestNewMux_ProdutosBuscaRotaSoRequireAuth(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("truncate produtos: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

	despachar := func(metodo, caminho, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(metodo, caminho, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	seedContaMux(t, db, "busca-mux-usuario@empresa.com", "usuario", senha, segredos)

	t.Run("sem token -> 401", func(t *testing.T) {
		w := despachar(http.MethodGet, "/api/produtos/busca?q=parafuso", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("token usuario -> 200 (rota sem RequireRole)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "busca-mux-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodGet, "/api/produtos/busca?q=parafuso", token)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body=%s) — rota não deveria exigir RequireRole", w.Code, http.StatusOK, w.Body.String())
		}
	})
}

// TestNewMux_ProdutosCatalogoRotaSoRequireAuth prova, despachando pela mesma
// instância de newMux usada por main() (Story 4.3, spec-4-3), que GET
// /api/produtos/catalogo NÃO leva RequireRole: sem token -> 401
// (RequireAuth); token `usuario` -> 200, NUNCA 403 — mesmo molde de
// TestNewMux_ProdutosBuscaRotaSoRequireAuth. Sem este caso, adicionar
// indevidamente `middleware.RequireRole(...)` a esta rota deixaria a suíte
// verde (os testes de handlers/produtos_test.go montam seu próprio mux local
// e nunca despacham pela composição real de main.go).
func TestNewMux_ProdutosCatalogoRotaSoRequireAuth(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("truncate produtos: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

	despachar := func(metodo, caminho, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(metodo, caminho, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	seedContaMux(t, db, "catalogo-mux-usuario@empresa.com", "usuario", senha, segredos)

	t.Run("sem token -> 401", func(t *testing.T) {
		w := despachar(http.MethodGet, "/api/produtos/catalogo", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("token usuario -> 200 (rota sem RequireRole)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "catalogo-mux-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodGet, "/api/produtos/catalogo", token)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body=%s) — rota não deveria exigir RequireRole", w.Code, http.StatusOK, w.Body.String())
		}
	})
}

// TestNewMux_ProdutosDetalheRotaSoRequireAuth prova, despachando pela mesma
// instância de newMux usada por main() (Story 4.4, spec-4-4), que GET
// /api/produtos/{id} NÃO leva RequireRole: sem token -> 401 (RequireAuth);
// token `usuario` -> 200, NUNCA 403 — mesmo molde de
// TestNewMux_ProdutosCatalogoRotaSoRequireAuth.
func TestNewMux_ProdutosDetalheRotaSoRequireAuth(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("truncate produtos: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

	despachar := func(metodo, caminho, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(metodo, caminho, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	seedContaMux(t, db, "detalhe-mux-usuario@empresa.com", "usuario", senha, segredos)

	var categoriaID string
	if err := db.QueryRow(`SELECT id FROM categorias LIMIT 1`).Scan(&categoriaID); err != nil {
		t.Fatalf("seed categoria: %v", err)
	}
	estoque, err := services.CriarEstoque(db, "Canteiro Detalhe Mux")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	produto, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome: "Produto Detalhe Mux", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 1,
	})
	if err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	t.Run("sem token -> 401", func(t *testing.T) {
		w := despachar(http.MethodGet, "/api/produtos/"+produto.ID, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("token usuario -> 200 (rota sem RequireRole)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "detalhe-mux-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodGet, "/api/produtos/"+produto.ID, token)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body=%s) — rota não deveria exigir RequireRole", w.Code, http.StatusOK, w.Body.String())
		}
	})
}

// TestNewMux_ProdutosPorCodigoRotaSoRequireAuth prova, despachando pela mesma
// instância de newMux usada por main() (Story 4.5, spec-4-5), que GET
// /api/produtos/por-codigo NÃO leva RequireRole: sem token -> 401
// (RequireAuth); token `usuario` -> 200, NUNCA 403 — mesmo molde de
// TestNewMux_ProdutosDetalheRotaSoRequireAuth. Inclui também a asserção de
// PRECEDÊNCIA sobre o wildcard `{id}`: `GET /api/produtos/por-codigo?codigo=`
// responde `400 VALIDATION_ERROR` "código obrigatório" (do handler literal),
// nunca `404` de ObterProdutoHandler — no mux do Go 1.22 o segmento literal
// vence o wildcard na mesma posição.
func TestNewMux_ProdutosPorCodigoRotaSoRequireAuth(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("truncate produtos: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

	despachar := func(metodo, caminho, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(metodo, caminho, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	seedContaMux(t, db, "porcodigo-mux-usuario@empresa.com", "usuario", senha, segredos)

	var categoriaID string
	if err := db.QueryRow(`SELECT id FROM categorias LIMIT 1`).Scan(&categoriaID); err != nil {
		t.Fatalf("seed categoria: %v", err)
	}
	estoque, err := services.CriarEstoque(db, "Canteiro PorCodigo Mux")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}
	if _, err := services.CriarProduto(db, services.CriarProdutoInput{
		Nome: "Produto PorCodigo Mux", Codigo: "PCM-001", CategoriaID: categoriaID, EstoqueID: estoque.ID, QuantidadeInicial: 1,
	}); err != nil {
		t.Fatalf("seed CriarProduto: %v", err)
	}

	t.Run("sem token -> 401", func(t *testing.T) {
		w := despachar(http.MethodGet, "/api/produtos/por-codigo?codigo=PCM-001", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("token usuario -> 200 (rota sem RequireRole)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "porcodigo-mux-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodGet, "/api/produtos/por-codigo?codigo=PCM-001", token)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body=%s) — rota não deveria exigir RequireRole", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("codigo vazio -> 400 do handler literal, nunca 404 do wildcard {id}", func(t *testing.T) {
		token := tokenDeMux(t, mux, "porcodigo-mux-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodGet, "/api/produtos/por-codigo?codigo=", token)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d (body=%s) — rota literal deveria vencer o wildcard {id}", w.Code, http.StatusBadRequest, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "código obrigatório") {
			t.Errorf("body = %s, want mensagem 'código obrigatório' (handler por-codigo, não ObterProdutoHandler)", w.Body.String())
		}
	})
}

// TestNewMux_RealtimeTicketRotaSoRequireAuth prova, despachando pela mesma
// instância de newMux usada por main() (Story 4.4, spec-4-4), que POST
// /api/realtime/ticket NÃO leva RequireRole: sem token -> 401
// (RequireAuth); token `usuario` -> 201, NUNCA 403.
func TestNewMux_RealtimeTicketRotaSoRequireAuth(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}

	despachar := func(metodo, caminho, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(metodo, caminho, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	seedContaMux(t, db, "realtime-ticket-mux-usuario@empresa.com", "usuario", senha, segredos)

	t.Run("sem token -> 401", func(t *testing.T) {
		w := despachar(http.MethodPost, "/api/realtime/ticket", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("token usuario -> 201 (rota sem RequireRole)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "realtime-ticket-mux-usuario@empresa.com", senha, segredos)
		w := despachar(http.MethodPost, "/api/realtime/ticket", token)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d (body=%s) — rota não deveria exigir RequireRole", w.Code, http.StatusCreated, w.Body.String())
		}
	})
}

// TestRealtimeStream_FluxoCompletoTicketStreamEvento prova o fluxo inteiro
// da AD-3 (Story 4.4, spec-4-4) numa conexão HTTP de verdade — não só
// ResponseRecorder — para exercitar a promoção real a `text/event-stream`
// numa conexão de longa duração: emite um ticket (POST
// /api/realtime/ticket), abre o stream (GET /api/realtime/stream?ticket=),
// confirma que a conexão fica pendurada com os headers corretos, dispara
// POST /api/produtos (que publica no canal `produtos` via o MESMO
// `*realtime.Registry` da instância de `mux` usada por `httptest.NewServer`)
// e lê o evento correspondente do corpo da resposta antes de fechá-la.
func TestRealtimeStream_FluxoCompletoTicketStreamEvento(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("truncate produtos: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	server := httptest.NewServer(mux)
	defer server.Close()

	const senha = "senha-123456"
	segredos := map[string]string{}
	seedContaMux(t, db, "sse-fluxo-usuario@empresa.com", "usuario", senha, segredos)
	seedContaMux(t, db, "sse-fluxo-almoxarife@empresa.com", "almoxarife", senha, segredos)

	tokenUsuario := tokenDeMux(t, mux, "sse-fluxo-usuario@empresa.com", senha, segredos)

	// POST /api/realtime/ticket — via mux diretamente (mesma instância que o
	// servidor real serve; despachar por ResponseRecorder aqui é só
	// conveniência, o ticket em si é um dado do banco, não da conexão).
	reqTicket := httptest.NewRequest(http.MethodPost, "/api/realtime/ticket", nil)
	reqTicket.Header.Set("Authorization", "Bearer "+tokenUsuario)
	wTicket := httptest.NewRecorder()
	mux.ServeHTTP(wTicket, reqTicket)
	if wTicket.Code != http.StatusCreated {
		t.Fatalf("POST /api/realtime/ticket: status = %d, want %d (body=%s)", wTicket.Code, http.StatusCreated, wTicket.Body.String())
	}
	var ticketResp struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(wTicket.Body.Bytes(), &ticketResp); err != nil {
		t.Fatalf("decode ticket: %v", err)
	}

	// GET /api/realtime/stream?ticket=... — conexão HTTP real, cancelável via
	// context (fecha explicitamente após ler o 1º evento, mais um teto de
	// segurança de 10s para não pendurar a suíte se algo falhar).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reqStream, err := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"/api/realtime/stream?ticket="+ticketResp.Ticket, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := http.DefaultClient.Do(reqStream)
	if err != nil {
		t.Fatalf("GET /api/realtime/stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// Neste ponto o servidor já executou registro.Subscribe() (acontece ANTES
	// de w.WriteHeader/Flush no handler — StreamRealtimeHandler, Story 4.4) —
	// o cliente só recebeu os headers depois desse flush, então a assinatura
	// já está registrada no *realtime.Registry compartilhado.

	var categoriaID string
	if err := db.QueryRow(`SELECT id FROM categorias LIMIT 1`).Scan(&categoriaID); err != nil {
		t.Fatalf("seed categoria: %v", err)
	}
	estoque, err := services.CriarEstoque(db, "Canteiro SSE Fluxo")
	if err != nil {
		t.Fatalf("seed CriarEstoque: %v", err)
	}

	tokenAlmoxarife := tokenDeMux(t, mux, "sse-fluxo-almoxarife@empresa.com", senha, segredos)
	corpoProduto := fmt.Sprintf(
		`{"nome":"Produto SSE Fluxo","categoria_id":%q,"estoque_id":%q,"quantidade_inicial":1}`,
		categoriaID, estoque.ID,
	)
	reqCriar := httptest.NewRequest(http.MethodPost, "/api/produtos", strings.NewReader(corpoProduto))
	reqCriar.Header.Set("Content-Type", "application/json")
	reqCriar.Header.Set("Authorization", "Bearer "+tokenAlmoxarife)
	wCriar := httptest.NewRecorder()
	mux.ServeHTTP(wCriar, reqCriar)
	if wCriar.Code != http.StatusCreated {
		t.Fatalf("POST /api/produtos: status = %d, want %d (body=%s)", wCriar.Code, http.StatusCreated, wCriar.Body.String())
	}
	var produtoResp struct {
		Produto struct {
			ID string `json:"id"`
		} `json:"produto"`
	}
	if err := json.Unmarshal(wCriar.Body.Bytes(), &produtoResp); err != nil {
		t.Fatalf("decode produto criado: %v", err)
	}

	// Lê linhas do corpo até encontrar o evento SSE (`data: <json>`) — pula
	// eventuais linhas de `: keep-alive` que cheguem antes (improvável na
	// janela deste teste, mas inofensivo caso ocorra).
	leitor := bufio.NewReader(resp.Body)
	var evento struct {
		Resource string `json:"resource"`
		ID       string `json:"id"`
		Change   string `json:"change"`
	}
	encontrado := false
	for !encontrado {
		linha, err := leitor.ReadString('\n')
		if err != nil {
			t.Fatalf("falha ao ler evento SSE do corpo da resposta: %v", err)
		}
		linha = strings.TrimRight(linha, "\r\n")
		if !strings.HasPrefix(linha, "data: ") {
			continue
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(linha, "data: ")), &evento); err != nil {
			t.Fatalf("decode evento SSE (%q): %v", linha, err)
		}
		encontrado = true
	}

	if evento.Resource != "produtos" || evento.ID != produtoResp.Produto.ID || evento.Change != "created" {
		t.Fatalf("evento = %+v, want {produtos %s created}", evento, produtoResp.Produto.ID)
	}
}

// TestNewMux_ProdutosCatalogoExportarRotaRequireRoleAlmoxarife prova,
// despachando pela mesma instância de newMux usada por main() (Story 4.6,
// spec-4-6, FR-30), que GET /api/produtos/catalogo/exportar está atrás de
// RequireRole(almoxarife): token `usuario` -> 403 FORBIDDEN (o handler nunca
// roda); token `almoxarife` passa do gate — 200 de verdade, com o
// `Content-Type` do `.xlsx`, nunca 403. Mesmo molde de
// TestNewMux_ImportacoesRotaCarregaRequireRole acima.
func TestNewMux_ProdutosCatalogoExportarRotaRequireRoleAlmoxarife(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, normalizacao_ignoradas, mesclagem_produtos_removidos, mesclagens_duplicatas, produto_estoque, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("truncate produtos: %v", err)
	}

	emailCfg := services.CarregarEmailConfig()
	jwtSecret := []byte("segredo-de-teste-nao-usar-em-producao")
	fotosDir := t.TempDir()
	mux := newMux(db, emailCfg, jwtSecret, iam.Config{}, fotosDir)

	const senha = "senha-123456"
	segredos := map[string]string{}
	seedContaMux(t, db, "catalogo-exportar-mux-usuario@empresa.com", "usuario", senha, segredos)
	seedContaMux(t, db, "catalogo-exportar-mux-almox@empresa.com", "almoxarife", senha, segredos)

	despachar := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/produtos/catalogo/exportar", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	t.Run("papel usuario -> 403 FORBIDDEN", func(t *testing.T) {
		token := tokenDeMux(t, mux, "catalogo-exportar-mux-usuario@empresa.com", senha, segredos)
		w := despachar(token)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
		}
		var env struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Error.Code != "FORBIDDEN" {
			t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
		}
	})

	t.Run("almoxarife passa do gate (200 de verdade, nunca 403)", func(t *testing.T) {
		token := tokenDeMux(t, mux, "catalogo-exportar-mux-almox@empresa.com", senha, segredos)
		w := despachar(token)
		if w.Code == http.StatusForbidden {
			t.Fatalf("status = %d, want != 403 (body=%s)", w.Code, w.Body.String())
		}
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
		}
		wantContentType := "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		if ct := w.Header().Get("Content-Type"); ct != wantContentType {
			t.Errorf("Content-Type = %q, want %q", ct, wantContentType)
		}
	})
}
