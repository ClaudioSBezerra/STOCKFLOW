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
