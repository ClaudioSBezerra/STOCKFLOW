package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// contarLogsAcesso / ultimoLogAcesso são o molde de contarSessoes
// (auth_sso_test.go) para a suíte da Story 1.12 — compartilhados por
// auth_test.go / auth_sso_test.go / auth_mfa_test.go (mesmo pacote).
func contarLogsAcesso(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM logs_acesso`).Scan(&n); err != nil {
		t.Fatalf("contar logs_acesso: %v", err)
	}
	return n
}

type logAcessoLinha struct {
	usuarioID sql.NullString
	email     string
	metodo    string
	sucesso   bool
	ip        string
}

// ultimoLogAcesso devolve a linha mais recente de logs_acesso (por
// criado_em). Falha o teste se a tabela está vazia.
func ultimoLogAcesso(t *testing.T, db *sql.DB) logAcessoLinha {
	t.Helper()
	var l logAcessoLinha
	err := db.QueryRow(`
		SELECT usuario_id, email_informado, metodo, sucesso, ip
		FROM logs_acesso
		ORDER BY criado_em DESC
		LIMIT 1`).Scan(&l.usuarioID, &l.email, &l.metodo, &l.sucesso, &l.ip)
	if err != nil {
		t.Fatalf("ultimoLogAcesso: %v", err)
	}
	return l
}

// getLogsAcesso despacha através da MESMA composição registrada em newMux
// (main.go): RequireAuth -> RequireRole(PapelAdm) -> ListarLogsAcessoHandler.
func getLogsAcesso(db *sql.DB, authHeader, query string) *httptest.ResponseRecorder {
	caminho := "/api/logs-acesso"
	if query != "" {
		caminho += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, caminho, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	middleware.RequireAuth(db, testJWTSecret)(
		middleware.RequireRole(services.PapelAdm)(
			ListarLogsAcessoHandler(db)))(w, req)
	return w
}

type logsAcessoResposta struct {
	Logs []services.LogAcesso `json:"logs"`
}

func decodeLogsAcesso(t *testing.T, body []byte) logsAcessoResposta {
	t.Helper()
	var resp logsAcessoResposta
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("falha ao decodificar resposta de logs de acesso: %v (body=%s)", err, body)
	}
	return resp
}

// TestListarLogsAcessoHandler_AdmRecebe200ComLinhas prova a AC3: uma sessão
// `adm` recebe 200 e as linhas ordenadas por criado_em DESC.
func TestListarLogsAcessoHandler_AdmRecebe200ComLinhas(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Adm", "logs-adm@empresa.com", "senha-123456", "adm")
	token := tokenDeLogin(t, db, "logs-adm@empresa.com", "senha-123456")

	// Duas tentativas de login por senha (uma falha, uma sucesso) geram linhas.
	criarUsuarioLogin(t, db, "alvo-logs@empresa.com", "senha-123456")
	if w := postLogin(db, `{"email":"alvo-logs@empresa.com","senha":"errada"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("pré-condição login falho: status = %d", w.Code)
	}
	if w := postLogin(db, `{"email":"alvo-logs@empresa.com","senha":"senha-123456"}`); w.Code != http.StatusOK {
		t.Fatalf("pré-condição login ok: status = %d", w.Code)
	}

	w := getLogsAcesso(db, "Bearer "+token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	resp := decodeLogsAcesso(t, w.Body.Bytes())
	// >= 2: o login do próprio adm por senha também gerou uma linha.
	if len(resp.Logs) < 2 {
		t.Fatalf("len(logs) = %d, want >= 2", len(resp.Logs))
	}
	for i := 1; i < len(resp.Logs); i++ {
		if resp.Logs[i-1].CriadoEm.Before(resp.Logs[i].CriadoEm) {
			t.Fatalf("logs fora de ordem criado_em DESC no índice %d", i)
		}
	}

	// Contrato de formato de fio: decodifica cru (não via services.LogAcesso,
	// que round-tripa qualquer renomeação de tag) e trava o conjunto EXATO de
	// chaves de uma linha — o frontend (LogAcessoSection) depende delas.
	var cru struct {
		Logs []map[string]any `json:"logs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cru); err != nil {
		t.Fatalf("decode cru: %v", err)
	}
	if len(cru.Logs) == 0 {
		t.Fatal("logs cru vazio")
	}
	querChaves := map[string]bool{
		"id": true, "usuarioId": true, "usuarioNome": true, "emailInformado": true,
		"metodo": true, "sucesso": true, "ip": true, "criadoEm": true,
	}
	for k := range cru.Logs[0] {
		if !querChaves[k] {
			t.Errorf("chave inesperada no JSON de log: %q", k)
		}
	}
	for k := range querChaves {
		if _, ok := cru.Logs[0][k]; !ok {
			t.Errorf("chave ausente no JSON de log: %q", k)
		}
	}
}

// TestListarLogsAcessoHandler_403ParaPapelAbaixoDeAdm prova a AC4: qualquer
// papel abaixo de `adm` recebe 403 e o corpo é o envelope de erro, nunca
// {"logs":...}.
func TestListarLogsAcessoHandler_403ParaPapelAbaixoDeAdm(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Usuária", "logs-usuario@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Almoxarife", "logs-almox@empresa.com", "senha-123456", "almoxarife")
	criarContaComPapel(t, db, "Gestora", "logs-gestor@empresa.com", "senha-123456", "gestor")

	casos := []struct{ nome, email string }{
		{"papel usuario", "logs-usuario@empresa.com"},
		{"papel almoxarife", "logs-almox@empresa.com"},
		{"papel gestor", "logs-gestor@empresa.com"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			token := tokenDeLogin(t, db, c.email, "senha-123456")
			w := getLogsAcesso(db, "Bearer "+token, "")
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "FORBIDDEN" {
				t.Errorf("code = %q, want FORBIDDEN", env.Error.Code)
			}
			var comLogs map[string]json.RawMessage
			_ = json.Unmarshal(w.Body.Bytes(), &comLogs)
			if _, temLogs := comLogs["logs"]; temLogs {
				t.Errorf("corpo do 403 contém \"logs\" — o handler nunca deveria ter executado")
			}
		})
	}
}

// TestListarLogsAcessoHandler_SemToken401 prova o cenário "sem autenticação":
// RequireAuth responde 401 TOKEN_EXPIRED antes de RequireRole rodar.
func TestListarLogsAcessoHandler_SemToken401(t *testing.T) {
	db := testDB(t)
	w := getLogsAcesso(db, "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want TOKEN_EXPIRED", env.Error.Code)
	}
}

// TestListarLogsAcessoHandler_FiltroDePeriodoInvalido prova a linha "inicio/fim
// malformado -> 400 VALIDATION_ERROR" da I/O Matrix.
func TestListarLogsAcessoHandler_FiltroDePeriodoInvalido(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Adm", "logs-adm-inv@empresa.com", "senha-123456", "adm")
	token := tokenDeLogin(t, db, "logs-adm-inv@empresa.com", "senha-123456")

	for _, q := range []string{"inicio=ontem", "fim=2026-13-40", "inicio=2026/08/01"} {
		w := getLogsAcesso(db, "Bearer "+token, q)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("query %q: status = %d, want 400 (body=%s)", q, w.Code, w.Body.String())
		}
		if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
			t.Errorf("query %q: code = %q, want VALIDATION_ERROR", q, env.Error.Code)
		}
	}
}

// TestListarLogsAcessoHandler_FiltroDePeriodoRestringe prova a linha "adm
// filtra por período": só linhas no intervalo, com `fim` só-data inclusivo
// até o fim do dia.
func TestListarLogsAcessoHandler_FiltroDePeriodoRestringe(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Adm", "logs-adm-filtro@empresa.com", "senha-123456", "adm")
	token := tokenDeLogin(t, db, "logs-adm-filtro@empresa.com", "senha-123456")

	// Limpa o que o login do adm gerou para começar de uma base conhecida.
	if _, err := db.Exec(`TRUNCATE TABLE logs_acesso`); err != nil {
		t.Fatalf("truncate logs_acesso: %v", err)
	}
	const q = `
		INSERT INTO logs_acesso (email_informado, metodo, sucesso, ip, criado_em)
		VALUES
			('a@x.com', 'senha', false, '1.1.1.1', TIMESTAMPTZ '2026-07-31 23:00:00Z'),
			('b@x.com', 'senha', true,  '1.1.1.1', TIMESTAMPTZ '2026-08-05 10:00:00Z'),
			('c@x.com', 'sso',   true,  '1.1.1.1', TIMESTAMPTZ '2026-08-15 18:30:00Z'),
			('d@x.com', 'senha', false, '1.1.1.1', TIMESTAMPTZ '2026-08-16 01:00:00Z')`
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("seed logs: %v", err)
	}

	w := getLogsAcesso(db, "Bearer "+token, "inicio=2026-08-01&fim=2026-08-15")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	resp := decodeLogsAcesso(t, w.Body.Bytes())
	if len(resp.Logs) != 2 {
		t.Fatalf("len(logs) = %d, want 2 (só 05/08 e 15/08) — %+v", len(resp.Logs), resp.Logs)
	}
	for _, l := range resp.Logs {
		if l.EmailInformado == "a@x.com" || l.EmailInformado == "d@x.com" {
			t.Errorf("linha fora do intervalo veio na resposta: %q", l.EmailInformado)
		}
	}
}

// TestListarLogsAcessoHandler_FiltroRFC3339 exercita o ramo
// time.Parse(time.RFC3339, ...) de parsePeriodoLog (os demais testes de
// período só passam data pura ou string já inválida): com limites RFC3339
// completos, só as linhas cujo criado_em cai no instante [inicio, fim]
// aparecem.
func TestListarLogsAcessoHandler_FiltroRFC3339(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Adm", "logs-adm-rfc@empresa.com", "senha-123456", "adm")
	token := tokenDeLogin(t, db, "logs-adm-rfc@empresa.com", "senha-123456")

	if _, err := db.Exec(`TRUNCATE TABLE logs_acesso`); err != nil {
		t.Fatalf("truncate logs_acesso: %v", err)
	}
	const seed = `
		INSERT INTO logs_acesso (email_informado, metodo, sucesso, ip, criado_em)
		VALUES
			('antes@x.com',  'senha', false, '1.1.1.1', TIMESTAMPTZ '2026-08-04 23:59:59Z'),
			('dentro1@x.com','senha', true,  '1.1.1.1', TIMESTAMPTZ '2026-08-05 00:00:01Z'),
			('dentro2@x.com','sso',   true,  '1.1.1.1', TIMESTAMPTZ '2026-08-15 23:59:58Z'),
			('depois@x.com', 'senha', false, '1.1.1.1', TIMESTAMPTZ '2026-08-16 00:00:05Z')`
	if _, err := db.Exec(seed); err != nil {
		t.Fatalf("seed logs: %v", err)
	}

	w := getLogsAcesso(db, "Bearer "+token, "inicio=2026-08-05T00:00:00Z&fim=2026-08-15T23:59:59Z")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	resp := decodeLogsAcesso(t, w.Body.Bytes())
	if len(resp.Logs) != 2 {
		t.Fatalf("len(logs) = %d, want 2 (só dentro1 e dentro2) — %+v", len(resp.Logs), resp.Logs)
	}
	for _, l := range resp.Logs {
		if l.EmailInformado != "dentro1@x.com" && l.EmailInformado != "dentro2@x.com" {
			t.Errorf("linha fora do intervalo RFC3339 veio na resposta: %q", l.EmailInformado)
		}
	}
}

// TestLoginHandler_FalhaDoInsertDeLogNaoQuebraLogin prova a linha "Falha do
// INSERT de log" da I/O Matrix: com a tabela logs_acesso indisponível, um
// login por senha válido ainda responde 200 (com token + cookie de refresh) —
// o registro de auditoria é não-fatal e nunca vira 500. A tabela é renomeada
// para fora e restaurada por t.Cleanup, para não afetar os demais testes da
// suíte (execução serial, sem t.Parallel).
func TestLoginHandler_FalhaDoInsertDeLogNaoQuebraLogin(t *testing.T) {
	db := testDB(t)
	criarUsuarioLogin(t, db, "login-sem-log@empresa.com", "senha-123456")

	if _, err := db.Exec(`ALTER TABLE logs_acesso RENAME TO logs_acesso_indisponivel`); err != nil {
		t.Fatalf("renomear logs_acesso: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`ALTER TABLE logs_acesso_indisponivel RENAME TO logs_acesso`); err != nil {
			t.Fatalf("restaurar logs_acesso: %v", err)
		}
	})

	w := postLogin(db, `{"email":"login-sem-log@empresa.com","senha":"senha-123456"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s) — INSERT de log falho não pode virar 500", w.Code, w.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Token == "" {
		t.Fatalf("resposta de login sem token utilizável (body=%s, err=%v)", w.Body.String(), err)
	}
	if refreshCookieDoResultado(t, w) == nil {
		t.Fatal("cookie de refresh ausente — sessão não foi emitida apesar do login válido")
	}
}
