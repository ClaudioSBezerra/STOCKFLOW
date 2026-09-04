package services

import (
	"database/sql"
	"fmt"
	"time"
	"unicode/utf8"
)

// maxLogsAcessoPorConsulta limita quantas linhas GET /api/logs-acesso devolve
// numa consulta — a rota é só-leitura e só-`adm`, mas ainda assim o resultado
// não pode crescer sem teto conforme a trilha de auditoria acumula. 500 é
// decisão desta story (spec-1-12); não há configuração runtime.
const maxLogsAcessoPorConsulta = 500

// RegistroTentativaLogin é o insumo de RegistrarTentativaLogin: uma tentativa
// de login já concluída, com o desfecho conhecido. UsuarioID é nil quando a
// conta não é identificável sem ferir a não-enumeração de e-mail (falha de
// senha, SSO sem conta / e-mail não verificado).
type RegistroTentativaLogin struct {
	UsuarioID      *string
	EmailInformado string
	Metodo         string
	IP             string
	Sucesso        bool
}

// RegistrarTentativaLogin grava UMA linha em `logs_acesso` por tentativa de
// login concluída (Story 1.12, FR-38/NFR-3). É um único db.Exec, sem
// transação e deliberadamente NÃO-FATAL para o chamador: os handlers de login
// só logam um slog.Warn quando esta função devolve erro e seguem o fluxo —
// registrar a auditoria nunca pode transformar um login em 500 nem alterar a
// resposta ao solicitante (mesmo precedente de registrarFalhaLogin,
// auth.go).
//
// EmailInformado é normalizado por normalizeEmail (minúsculas, sem espaços
// nas bordas — mesma normalização de `usuarios.email`) e truncado a 255
// runes para caber em `logs_acesso.email_informado VARCHAR(255)`. IP vazio
// vira 'desconhecido' (a coluna é NOT NULL).
func RegistrarTentativaLogin(db *sql.DB, r RegistroTentativaLogin) error {
	email := normalizeEmail(r.EmailInformado)
	if utf8.RuneCountInString(email) > 255 {
		email = string([]rune(email)[:255])
	}

	ip := r.IP
	if ip == "" {
		ip = "desconhecido"
	}
	// Cap defensivo em 64 chars (largura da coluna) — espelha o truncamento de
	// EmailInformado acima. O chamador (handlers.ipDaRequisicao) já só devolve
	// um IP válido ou o host de RemoteAddr, mas este guard garante que nenhuma
	// origem consiga derrubar o INSERT não-fatal por estouro de coluna.
	if len(ip) > 64 {
		ip = ip[:64]
	}

	const insert = `
		INSERT INTO logs_acesso (usuario_id, email_informado, metodo, sucesso, ip)
		VALUES ($1, $2, $3, $4, $5)`
	if _, err := db.Exec(insert, r.UsuarioID, email, r.Metodo, r.Sucesso, ip); err != nil {
		return fmt.Errorf("falha ao registrar tentativa de login em logs_acesso: %w", err)
	}
	return nil
}

// LogAcesso é a projeção de uma linha de `logs_acesso` devolvida por
// GET /api/logs-acesso. UsuarioNome vem de um LEFT JOIN em `usuarios` (o
// `adm` vê quem foi sem outra chamada); fica nil quando usuario_id é NULL.
// Uma conta anonimizada pela Story 8.2 exibe o nome anonimizado, o que é
// correto.
type LogAcesso struct {
	ID             string    `json:"id"`
	UsuarioID      *string   `json:"usuarioId"`
	UsuarioNome    *string   `json:"usuarioNome"`
	EmailInformado string    `json:"emailInformado"`
	Metodo         string    `json:"metodo"`
	Sucesso        bool      `json:"sucesso"`
	IP             string    `json:"ip"`
	CriadoEm       time.Time `json:"criadoEm"`
}

// ListarLogsAcesso devolve as linhas de `logs_acesso` no intervalo
// [inicio, fim] (ambos opcionais — nil = sem limite naquele extremo),
// ordenadas do mais recente ao mais antigo e limitadas a
// maxLogsAcessoPorConsulta. Lista vazia não é erro. Molde de ListarUsuarios
// (services/usuarios.go).
func ListarLogsAcesso(db *sql.DB, inicio, fim *time.Time) ([]LogAcesso, error) {
	// Dereferência explícita para interface{}: um *time.Time nil é passado
	// como NULL, e o predicado `$n::timestamptz IS NULL OR ...` desliga o
	// filtro daquele extremo.
	var inicioArg, fimArg any
	if inicio != nil {
		inicioArg = *inicio
	}
	if fim != nil {
		fimArg = *fim
	}

	// `l.id` é o desempate determinístico do ORDER BY: duas tentativas de login
	// no mesmo microssegundo (plausível sob credential stuffing — justo quando o
	// log importa) compartilham `criado_em`, e sem desempate a fronteira do
	// LIMIT ordenaria de forma não-determinística entre consultas.
	q := fmt.Sprintf(`
		SELECT l.id, l.usuario_id, u.nome, l.email_informado, l.metodo, l.sucesso, l.ip, l.criado_em
		FROM logs_acesso l
		LEFT JOIN usuarios u ON u.id = l.usuario_id
		WHERE ($1::timestamptz IS NULL OR l.criado_em >= $1)
		  AND ($2::timestamptz IS NULL OR l.criado_em <= $2)
		ORDER BY l.criado_em DESC, l.id DESC
		LIMIT %d`, maxLogsAcessoPorConsulta)

	rows, err := db.Query(q, inicioArg, fimArg)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar logs de acesso: %w", err)
	}
	defer rows.Close()

	logs := make([]LogAcesso, 0)
	for rows.Next() {
		var l LogAcesso
		var usuarioID, usuarioNome sql.NullString
		if err := rows.Scan(&l.ID, &usuarioID, &usuarioNome, &l.EmailInformado, &l.Metodo, &l.Sucesso, &l.IP, &l.CriadoEm); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de log de acesso: %w", err)
		}
		if usuarioID.Valid {
			l.UsuarioID = &usuarioID.String
		}
		if usuarioNome.Valid {
			l.UsuarioNome = &usuarioNome.String
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar logs de acesso: %w", err)
	}
	return logs, nil
}

// ListarLogsAcessoDoUsuario devolve TODAS as linhas de `logs_acesso` cujo
// `usuario_id` é `usuarioID`, do mais recente ao mais antigo — insumo de
// ExportarDadosUsuario (Story 8.1, spec-8-1: exportação dos próprios dados
// pessoais, LGPD). Molde de ListarLogsAcesso, mas escopada por usuário e
// deliberadamente SEM `LIMIT`/maxLogsAcessoPorConsulta: aquele teto existe
// para a consulta administrativa (`adm`, toda a organização); aqui o
// conjunto já está limitado a um único usuário e a LGPD pede o histórico
// completo, não uma amostra. Mesmo `ORDER BY l.criado_em DESC, l.id DESC`
// (desempate determinístico). Lista vazia não é erro.
func ListarLogsAcessoDoUsuario(db *sql.DB, usuarioID string) ([]LogAcesso, error) {
	const q = `
		SELECT l.id, l.usuario_id, u.nome, l.email_informado, l.metodo, l.sucesso, l.ip, l.criado_em
		FROM logs_acesso l
		LEFT JOIN usuarios u ON u.id = l.usuario_id
		WHERE l.usuario_id = $1
		ORDER BY l.criado_em DESC, l.id DESC`

	rows, err := db.Query(q, usuarioID)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar logs de acesso do usuário: %w", err)
	}
	defer rows.Close()

	logs := make([]LogAcesso, 0)
	for rows.Next() {
		var l LogAcesso
		var usuarioIDCol, usuarioNome sql.NullString
		if err := rows.Scan(&l.ID, &usuarioIDCol, &usuarioNome, &l.EmailInformado, &l.Metodo, &l.Sucesso, &l.IP, &l.CriadoEm); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de log de acesso do usuário: %w", err)
		}
		if usuarioIDCol.Valid {
			l.UsuarioID = &usuarioIDCol.String
		}
		if usuarioNome.Valid {
			l.UsuarioNome = &usuarioNome.String
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar logs de acesso do usuário: %w", err)
	}
	return logs, nil
}
