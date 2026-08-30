package handlers

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// ipDaRequisicao extrai o IP de origem de uma requisição de login para a
// trilha de auditoria (Story 1.12). Prefere o primeiro salto de
// X-Forwarded-For quando presente — mesmo motivo de cookieEhSeguro já
// confiar em X-Forwarded-Proto (auth.go): em produção o servidor fica atrás
// de um proxy reverso e r.RemoteAddr seria sempre o IP do proxy. O primeiro
// salto só é aceito quando é um IP sintaticamente válido (net.ParseIP): a
// rota de login é pública e não autenticada, então um X-Forwarded-For lixo ou
// gigante não pode virar o valor gravado (nem, sendo maior que a coluna,
// derrubar o INSERT de auditoria não-fatal e apagar o próprio rastro). Sem um
// header utilizável, usa o host de r.RemoteAddr (net.SplitHostPort), com
// fallback ao valor bruto se ele não vier no formato host:porta.
func ipDaRequisicao(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		primeiro := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		if net.ParseIP(primeiro) != nil {
			return primeiro
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// registrarTentativaLogin é o helper fino que LoginHandler e
// KeycloakSSOHandler chamam no ponto em que o desfecho de uma tentativa de
// login já é conhecido: monta o RegistroTentativaLogin (resolvendo o IP com
// ipDaRequisicao) e delega a services.RegistrarTentativaLogin. Erro do INSERT
// é engolido com um slog.Warn — registrar a auditoria NUNCA altera a resposta
// ao solicitante nem transforma um login em 500 (FR-32/FR-38).
func registrarTentativaLogin(r *http.Request, db *sql.DB, metodo, emailInformado string, usuarioID *string, sucesso bool) {
	err := services.RegistrarTentativaLogin(db, services.RegistroTentativaLogin{
		UsuarioID:      usuarioID,
		EmailInformado: emailInformado,
		Metodo:         metodo,
		IP:             ipDaRequisicao(r),
		Sucesso:        sucesso,
	})
	if err != nil {
		slog.Warn("falha ao registrar tentativa de login em logs_acesso", "metodo", metodo, "sucesso", sucesso, "error", err)
	}
}

// parsePeriodoLog converte um parâmetro `inicio`/`fim` de GET /api/logs-acesso
// para *time.Time. Aceita RFC3339 (ex. 2026-08-01T00:00:00Z) OU data pura
// `2006-01-02`. Valor vazio -> nil (sem limite naquele extremo). Uma data
// pura em `fim` (fimDeDia=true) é tratada como o fim de dia inclusivo
// (23:59:59.999999 daquele dia), para que `?fim=2026-08-15` inclua tudo do
// dia 15. Formato irreconhecível -> erro (o handler devolve 400
// VALIDATION_ERROR).
func parsePeriodoLog(valor string, fimDeDia bool) (*time.Time, error) {
	valor = strings.TrimSpace(valor)
	if valor == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, valor); err == nil {
		return &t, nil
	}
	if t, err := time.Parse("2006-01-02", valor); err == nil {
		if fimDeDia {
			t = t.Add(24*time.Hour - time.Microsecond)
		}
		return &t, nil
	}
	return nil, fmt.Errorf("formato de data/hora inválido: %q", valor)
}

// ListarLogsAcessoHandler expõe GET /api/logs-acesso (Story 1.12, FR-38),
// SEMPRE registrado em newMux atrás de
// RequireAuth -> RequireRole(services.PapelAdm) — a composição de GET
// /api/usuarios, aqui com o gate de papel + MFA (Story 1.11) já resolvido no
// mínimo `adm`. O 401 (sem token) e o 403 (papel abaixo de `adm`) são
// decididos inteiramente pelos middlewares; este handler só executa depois de
// os dois passarem. Guard de contexto ausente -> 500, igual a MeHandler.
//
// Filtra por período via os query params `inicio` e `fim` (ambos opcionais,
// RFC3339 ou AAAA-MM-DD; `fim` só com data é fim de dia inclusivo). Formato
// inválido -> 400 VALIDATION_ERROR. Resposta: 200 {"logs":[...]} ordenada por
// criado_em DESC, no máximo services.maxLogsAcessoPorConsulta linhas.
func ListarLogsAcessoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("ListarLogsAcessoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		q := r.URL.Query()
		inicio, err := parsePeriodoLog(q.Get("inicio"), false)
		if err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "parâmetro 'inicio' inválido: use RFC3339 ou AAAA-MM-DD")
			return
		}
		fim, err := parsePeriodoLog(q.Get("fim"), true)
		if err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "parâmetro 'fim' inválido: use RFC3339 ou AAAA-MM-DD")
			return
		}

		logs, err := services.ListarLogsAcesso(db, inicio, fim)
		if err != nil {
			slog.Error("falha ao listar logs de acesso", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar logs de acesso")
			return
		}

		escreverJSON(w, http.StatusOK, map[string]any{"logs": logs})
	}
}
