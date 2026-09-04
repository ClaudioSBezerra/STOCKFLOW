package services

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

// inserirLogAcessoDireto insere uma linha de logs_acesso com criado_em
// controlado — para exercitar o filtro por período e a ordenação de
// ListarLogsAcesso sem depender do relógio real.
func inserirLogAcessoDireto(t *testing.T, db *sql.DB, usuarioID *string, email, metodo string, sucesso bool, ip string, criadoEm time.Time) string {
	t.Helper()
	var id string
	const q = `
		INSERT INTO logs_acesso (usuario_id, email_informado, metodo, sucesso, ip, criado_em)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`
	if err := db.QueryRow(q, usuarioID, email, metodo, sucesso, ip, criadoEm).Scan(&id); err != nil {
		t.Fatalf("falha ao inserir log_acesso de teste: %v", err)
	}
	return id
}

func TestRegistrarTentativaLogin_GravaCamposDeSucesso(t *testing.T) {
	db := testDB(t)
	id := criarUsuarioParaLogin(t, db, "log-sucesso@empresa.com", "senha-123456", true, true)

	if err := RegistrarTentativaLogin(db, RegistroTentativaLogin{
		UsuarioID:      &id,
		EmailInformado: "  Log-Sucesso@Empresa.com ",
		Metodo:         "senha",
		IP:             "203.0.113.7",
		Sucesso:        true,
	}); err != nil {
		t.Fatalf("RegistrarTentativaLogin: %v", err)
	}

	var (
		usuarioID sql.NullString
		email     string
		metodo    string
		sucesso   bool
		ip        string
	)
	if err := db.QueryRow(
		`SELECT usuario_id, email_informado, metodo, sucesso, ip FROM logs_acesso`,
	).Scan(&usuarioID, &email, &metodo, &sucesso, &ip); err != nil {
		t.Fatalf("reler log: %v", err)
	}
	if !usuarioID.Valid || usuarioID.String != id {
		t.Errorf("usuario_id = %v, want %q", usuarioID, id)
	}
	if email != "log-sucesso@empresa.com" {
		t.Errorf("email_informado = %q, want normalizado", email)
	}
	if metodo != "senha" || !sucesso || ip != "203.0.113.7" {
		t.Errorf("metodo=%q sucesso=%v ip=%q", metodo, sucesso, ip)
	}
}

func TestRegistrarTentativaLogin_FalhaGravaUsuarioNulo(t *testing.T) {
	db := testDB(t)

	if err := RegistrarTentativaLogin(db, RegistroTentativaLogin{
		UsuarioID:      nil,
		EmailInformado: "fantasma@empresa.com",
		Metodo:         "senha",
		IP:             "",
		Sucesso:        false,
	}); err != nil {
		t.Fatalf("RegistrarTentativaLogin: %v", err)
	}

	var usuarioID sql.NullString
	var ip string
	if err := db.QueryRow(`SELECT usuario_id, ip FROM logs_acesso`).Scan(&usuarioID, &ip); err != nil {
		t.Fatalf("reler log: %v", err)
	}
	if usuarioID.Valid {
		t.Errorf("usuario_id = %v, want NULL", usuarioID)
	}
	if ip != "desconhecido" {
		t.Errorf("ip = %q, want 'desconhecido' (default de IP vazio)", ip)
	}
}

func TestRegistrarTentativaLogin_TruncaEmailLongo(t *testing.T) {
	db := testDB(t)

	longo := strings.Repeat("a", 300) + "@x.com"
	if err := RegistrarTentativaLogin(db, RegistroTentativaLogin{
		EmailInformado: longo,
		Metodo:         "sso",
		IP:             "10.0.0.1",
		Sucesso:        false,
	}); err != nil {
		t.Fatalf("RegistrarTentativaLogin: %v", err)
	}

	var email string
	if err := db.QueryRow(`SELECT email_informado FROM logs_acesso`).Scan(&email); err != nil {
		t.Fatalf("reler log: %v", err)
	}
	if len([]rune(email)) != 255 {
		t.Errorf("len(email_informado) = %d runes, want 255 (truncado)", len([]rune(email)))
	}
}

func TestListarLogsAcesso_FiltroPorPeriodoOrdemELimite(t *testing.T) {
	db := testDB(t)
	id := criarUsuarioParaLogin(t, db, "listar-log@empresa.com", "senha-123456", true, true)

	base := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	// 3 linhas em dias distintos, inseridas fora de ordem cronológica.
	inserirLogAcessoDireto(t, db, &id, "listar-log@empresa.com", "senha", true, "1.1.1.1", base.AddDate(0, 0, 2)) // 12/08
	inserirLogAcessoDireto(t, db, nil, "fantasma@empresa.com", "senha", false, "2.2.2.2", base)                   // 10/08
	inserirLogAcessoDireto(t, db, &id, "listar-log@empresa.com", "sso", true, "3.3.3.3", base.AddDate(0, 0, 4))   // 14/08

	t.Run("sem filtro: todas, DESC, com nome do join", func(t *testing.T) {
		logs, err := ListarLogsAcesso(db, nil, nil)
		if err != nil {
			t.Fatalf("ListarLogsAcesso: %v", err)
		}
		if len(logs) != 3 {
			t.Fatalf("len = %d, want 3", len(logs))
		}
		if !logs[0].CriadoEm.After(logs[1].CriadoEm) || !logs[1].CriadoEm.After(logs[2].CriadoEm) {
			t.Errorf("ordem não é criado_em DESC: %v", []time.Time{logs[0].CriadoEm, logs[1].CriadoEm, logs[2].CriadoEm})
		}
		// A mais recente é a linha SSO da conta identificável -> nome preenchido.
		if logs[0].UsuarioNome == nil || *logs[0].UsuarioNome == "" {
			t.Errorf("usuarioNome nil na linha com usuario_id preenchido")
		}
		// A do meio (10/08) é a falha sem conta -> usuario_id/nome nil.
		if logs[2].UsuarioID != nil || logs[2].UsuarioNome != nil {
			t.Errorf("linha sem conta deveria ter usuarioId/usuarioNome nil, got %v/%v", logs[2].UsuarioID, logs[2].UsuarioNome)
		}
	})

	t.Run("limite inferior inclusivo", func(t *testing.T) {
		inicio := base.AddDate(0, 0, 1) // 11/08 -> exclui a de 10/08
		logs, err := ListarLogsAcesso(db, &inicio, nil)
		if err != nil {
			t.Fatalf("ListarLogsAcesso: %v", err)
		}
		if len(logs) != 2 {
			t.Fatalf("len = %d, want 2", len(logs))
		}
	})

	t.Run("limite superior", func(t *testing.T) {
		fim := base.AddDate(0, 0, 3) // 13/08 -> inclui 10 e 12, exclui 14
		logs, err := ListarLogsAcesso(db, nil, &fim)
		if err != nil {
			t.Fatalf("ListarLogsAcesso: %v", err)
		}
		if len(logs) != 2 {
			t.Fatalf("len = %d, want 2", len(logs))
		}
	})

	t.Run("ambos os limites", func(t *testing.T) {
		inicio := base.AddDate(0, 0, 1)
		fim := base.AddDate(0, 0, 3)
		logs, err := ListarLogsAcesso(db, &inicio, &fim)
		if err != nil {
			t.Fatalf("ListarLogsAcesso: %v", err)
		}
		if len(logs) != 1 {
			t.Fatalf("len = %d, want 1 (só a de 12/08)", len(logs))
		}
	})
}

func TestListarLogsAcesso_RespeitaLimiteMaximo(t *testing.T) {
	db := testDB(t)

	// Insere maxLogsAcessoPorConsulta + 5 linhas de uma vez.
	const q = `
		INSERT INTO logs_acesso (email_informado, metodo, sucesso, ip, criado_em)
		SELECT 'bulk@empresa.com', 'senha', false, '9.9.9.9', now() - (g || ' seconds')::interval
		FROM generate_series(1, $1) AS g`
	if _, err := db.Exec(q, maxLogsAcessoPorConsulta+5); err != nil {
		t.Fatalf("bulk insert: %v", err)
	}

	logs, err := ListarLogsAcesso(db, nil, nil)
	if err != nil {
		t.Fatalf("ListarLogsAcesso: %v", err)
	}
	if len(logs) != maxLogsAcessoPorConsulta {
		t.Fatalf("len = %d, want %d (LIMIT)", len(logs), maxLogsAcessoPorConsulta)
	}
}

func TestListarLogsAcesso_ListaVaziaNaoErro(t *testing.T) {
	db := testDB(t)
	logs, err := ListarLogsAcesso(db, nil, nil)
	if err != nil {
		t.Fatalf("ListarLogsAcesso: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("len = %d, want 0", len(logs))
	}
}

// --- ListarLogsAcessoDoUsuario — Story 8.1, spec-8-1 ------------------------

// TestListarLogsAcessoDoUsuario_EscopadoAoUsuarioSemLimite prova o Always da
// spec-8-1: só as linhas do usuário pedido voltam (nunca as de outra
// conta nem as sem conta), em ORDER BY criado_em DESC, e SEM o teto de
// maxLogsAcessoPorConsulta que ListarLogsAcesso aplica.
func TestListarLogsAcessoDoUsuario_EscopadoAoUsuarioSemLimite(t *testing.T) {
	db := testDB(t)
	id := criarUsuarioParaLogin(t, db, "export-log@empresa.com", "senha-123456", true, true)
	outroID := criarUsuarioParaLogin(t, db, "export-log-outro@empresa.com", "senha-123456", true, true)

	base := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	inserirLogAcessoDireto(t, db, &id, "export-log@empresa.com", "senha", true, "1.1.1.1", base)
	inserirLogAcessoDireto(t, db, &id, "export-log@empresa.com", "sso", true, "2.2.2.2", base.AddDate(0, 0, 1))
	inserirLogAcessoDireto(t, db, &outroID, "export-log-outro@empresa.com", "senha", true, "3.3.3.3", base)
	inserirLogAcessoDireto(t, db, nil, "fantasma@empresa.com", "senha", false, "4.4.4.4", base)

	logs, err := ListarLogsAcessoDoUsuario(db, id)
	if err != nil {
		t.Fatalf("ListarLogsAcessoDoUsuario: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("len = %d, want 2 (só as do usuário pedido)", len(logs))
	}
	for _, l := range logs {
		if l.UsuarioID == nil || *l.UsuarioID != id {
			t.Errorf("linha vazando de outro usuário: UsuarioID = %v, want %q", l.UsuarioID, id)
		}
	}
	if !logs[0].CriadoEm.After(logs[1].CriadoEm) {
		t.Errorf("ordem não é criado_em DESC: %v", []time.Time{logs[0].CriadoEm, logs[1].CriadoEm})
	}
}

// TestListarLogsAcessoDoUsuario_SemLogNaoEErro prova a linha "Usuário sem
// log próprio" da I/O Matrix de spec-8-1: slice vazio, não-nil, sem erro.
func TestListarLogsAcessoDoUsuario_SemLogNaoEErro(t *testing.T) {
	db := testDB(t)
	id := criarUsuarioParaLogin(t, db, "export-log-vazio@empresa.com", "senha-123456", true, true)

	logs, err := ListarLogsAcessoDoUsuario(db, id)
	if err != nil {
		t.Fatalf("ListarLogsAcessoDoUsuario: %v", err)
	}
	if logs == nil {
		t.Fatal("logs = nil, want slice vazio não-nil")
	}
	if len(logs) != 0 {
		t.Fatalf("len = %d, want 0", len(logs))
	}
}
