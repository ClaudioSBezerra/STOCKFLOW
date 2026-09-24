package middleware

import (
	"fmt"
	"log/slog"
	"net/http"

	"stockflow/backend/services"
)

// RequireRole (Story 1.5, AD-8) decora um handler exigindo um papel mínimo na
// hierarquia de acesso. Tem a MESMA forma de RequireAuth
// (`func(http.HandlerFunc) http.HandlerFunc`) e é SEMPRE composto DEPOIS dele
// em newMux — `RequireAuth(...)(RequireRole(...)(h))` — para que a ausência de
// token seja resolvida por RequireAuth (401) antes de RequireRole rodar.
//
// `papelMinimo` é resolvido para um rank UMA vez, quando o decorator é
// construído (em newMux / no startup): um papel mínimo desconhecido/mal
// digitado tem rank 0 e faria `RankPapel(usuario) < 0` — nunca verdadeiro,
// deixando a rota aberta a todos (fail-open). Em vez disso, `panic` na
// construção — mesma filosofia fail-fast de DATABASE_URL/JWT_SECRET em
// main.go: o servidor nunca sobe com um gate de papel inócuo.
//
// Comportamento em runtime:
//   - Contexto sem UsuarioSessao (RequireRole aplicado sem RequireAuth antes):
//     500 INTERNAL_ERROR + slog.Error — é erro de composição/programação, não
//     de request (mesmo guard de handlers.MeHandler).
//   - `RankPapel(papel resolvido) < rank do papel mínimo`: 403 FORBIDDEN,
//     código já no vocabulário fixo do AD-14. O handler decorado nunca executa
//     e o banco nunca é tocado.
//   - Papel suficiente: chama `next` diretamente.
//
// A decisão allow/deny vive só aqui — nenhum handler reimplementa a
// comparação de papel nem usa allow-list de pares (AD-8 forma 1).
//
// Story 1.11 (FR-37/SM-2, MFA obrigatório para papéis administrativos)
// acrescenta um segundo gate, DEPOIS do de papel: uma sessão autenticada por
// SENHA (`usuario.Origem == "senha"`), de papel `gestor`+ (mesma comparação
// de rank do papel mínimo desta rota), SEM MFA habilitado
// (`!usuario.MFAHabilitado`), é recusada com 403 MFA_SETUP_REQUIRED — mesmo
// tendo papel suficiente. A ordem importa: papel insuficiente SEMPRE vence
// primeiro (403 FORBIDDEN), para nunca vazar "esta rota existe e é
// restrita" a quem nem tem o papel mínimo. Sessão `origem=sso` (o realm
// Keycloak já impõe MFA a esses papéis) e sessão `origem=""` (JWT emitido
// antes desta migration, sem o claim — fail-open só para este gate, nunca
// para autenticidade) nunca disparam esta checagem.
//
// Story 14.1 (FR-37 revisado, AD-35) torna esse gate CONDICIONAL à Empresa:
// além das três condições acima, a Empresa da requisição
// (EmpresaDaRequisicao, resolvida por slug a cada requisição por
// RequireEmpresa, sem cache) precisa exigir MFA (`empresa.MFAObrigatorio`).
// Uma mudança do flag vale já na próxima requisição, sem novo login. Se as
// três primeiras condições valem e a Empresa NÃO está no contexto (rota
// registrada fora de RequireEmpresa), a resposta é 500 INTERNAL_ERROR +
// slog.Error — erro de composição, fail-closed: nunca abre a rota. As rotas
// do Dono da Plataforma (RequireDonoPlataforma) não passam por aqui e não
// leem o flag de Empresa nenhuma.
func RequireRole(papelMinimo string) func(http.HandlerFunc) http.HandlerFunc {
	rankMinimo := services.RankPapel(papelMinimo)
	if rankMinimo == 0 {
		panic(fmt.Sprintf("RequireRole: papel mínimo desconhecido: %q", papelMinimo))
	}

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			usuario, ok := UsuarioDaSessao(r.Context())
			if !ok {
				slog.Error("RequireRole aplicado sem RequireAuth antes — UsuarioSessao ausente do contexto", "papel_minimo", papelMinimo)
				escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário da sessão")
				return
			}

			if services.RankPapel(usuario.Papel) < rankMinimo {
				escreverErro(w, http.StatusForbidden, "FORBIDDEN", "papel insuficiente para acessar este recurso")
				return
			}

			if rankMinimo >= services.RankPapel(services.PapelGestor) && usuario.Origem == "senha" && !usuario.MFAHabilitado {
				empresa, ok := EmpresaDaRequisicao(r.Context())
				if !ok {
					slog.Error("RequireRole aplicado sem RequireEmpresa antes — Empresa ausente do contexto", "papel_minimo", papelMinimo, "rota", r.URL.Path)
					escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver empresa da requisição")
					return
				}
				if empresa.MFAObrigatorio {
					escreverErro(w, http.StatusForbidden, "MFA_SETUP_REQUIRED", "configure a autenticação em duas etapas em Configurações → Segurança para continuar.")
					return
				}
			}

			next(w, r)
		}
	}
}
