package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"stockflow/backend/services"
)

// empresaExigeMFA / empresaNaoExigeMFA são as Empresas injetadas no contexto
// pelos testes unitários de RequireRole (Story 14.1): o gate de MFA só
// dispara quando a Empresa da requisição exige.
var (
	empresaExigeMFA    = services.Empresa{ID: "empresa-exige", Slug: "empresa-exige", Status: "ativa", MFAObrigatorio: true}
	empresaNaoExigeMFA = services.Empresa{ID: "empresa-nao-exige", Slug: "empresa-nao-exige", Status: "ativa", MFAObrigatorio: false}
)

// chamarRequireRole compõe RequireRole(papelMinimo) sobre um handler que
// grava 200 e registra que executou. Quando usuario != nil, injeta o
// UsuarioSessao no contexto exatamente como RequireAuth faria — sem passar
// por RequireAuth, para exercitar RequireRole isoladamente. Injeta também
// uma Empresa que EXIGE MFA (como RequireEmpresa faria), de modo que os
// testes do gate da Story 1.11 continuem exercitando o caso "Empresa exige".
func chamarRequireRole(papelMinimo string, usuario *services.UsuarioSessao) (*httptest.ResponseRecorder, bool) {
	empresa := empresaExigeMFA
	return chamarRequireRoleComEmpresa(papelMinimo, usuario, &empresa)
}

// chamarRequireRoleComEmpresa é a variante de chamarRequireRole que escolhe a
// Empresa do contexto (empresaCtxKey); empresa == nil simula uma rota
// registrada fora de RequireEmpresa.
func chamarRequireRoleComEmpresa(papelMinimo string, usuario *services.UsuarioSessao, empresa *services.Empresa) (*httptest.ResponseRecorder, bool) {
	var executou bool
	next := func(w http.ResponseWriter, r *http.Request) {
		executou = true
		w.WriteHeader(http.StatusOK)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/usuarios", nil)
	ctx := req.Context()
	if usuario != nil {
		ctx = context.WithValue(ctx, usuarioSessaoCtxKey, *usuario)
	}
	if empresa != nil {
		ctx = context.WithValue(ctx, empresaCtxKey, *empresa)
	}
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	RequireRole(papelMinimo)(next)(w, req)
	return w, executou
}

// TestRequireRole_AbaixoDoMinimo403 prova a I/O Matrix: um papel abaixo do
// mínimo exigido resulta em 403 FORBIDDEN e o handler decorado nunca executa.
func TestRequireRole_AbaixoDoMinimo403(t *testing.T) {
	for _, papel := range []string{services.PapelUsuario, services.PapelAlmoxarife} {
		t.Run(papel, func(t *testing.T) {
			w, executou := chamarRequireRole(services.PapelGestor, &services.UsuarioSessao{Papel: papel, Ativo: true})
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
			}
			env := decodeErro(t, w.Body.Bytes())
			if env.Error.Code != "FORBIDDEN" {
				t.Errorf("code = %q, want %q", env.Error.Code, "FORBIDDEN")
			}
			if executou {
				t.Error("handler decorado executou apesar do 403 — a decisão allow/deny deve viver só no middleware")
			}
		})
	}
}

// TestRequireRole_NoMinimoOuAcimaPassaAdiante prova que um papel igual ou
// acima do mínimo passa direto para o handler seguinte, sem 403.
func TestRequireRole_NoMinimoOuAcimaPassaAdiante(t *testing.T) {
	for _, papel := range []string{services.PapelGestor, services.PapelAdm} {
		t.Run(papel, func(t *testing.T) {
			w, executou := chamarRequireRole(services.PapelGestor, &services.UsuarioSessao{Papel: papel, Ativo: true})
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
			}
			if !executou {
				t.Error("handler decorado não executou apesar do papel suficiente")
			}
		})
	}
}

// TestRequireRole_SemRequireAuth500 prova o cenário de composição da I/O
// Matrix: RequireRole aplicado sem RequireAuth antes (contexto sem
// UsuarioSessao) devolve 500 INTERNAL_ERROR — erro de programação, não de
// request.
func TestRequireRole_SemRequireAuth500(t *testing.T) {
	w, executou := chamarRequireRole(services.PapelGestor, nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusInternalServerError, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("code = %q, want %q", env.Error.Code, "INTERNAL_ERROR")
	}
	if executou {
		t.Error("handler decorado executou sem UsuarioSessao no contexto")
	}
}

// TestRequireRole_PapelMinimoDesconhecidoPanica prova o fail-fast: construir
// o decorator com um papel mínimo fora do enum (rank 0) é erro de
// programação e derruba o startup — nunca deixa a rota silenciosamente
// aberta a todos.
func TestRequireRole_PapelMinimoDesconhecidoPanica(t *testing.T) {
	for _, minimo := range []string{"naoexiste", "", "ADM", "Gestor"} {
		t.Run(minimo, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("RequireRole(%q) não entrou em panic — gate de papel inócuo passaria despercebido", minimo)
				}
			}()
			RequireRole(minimo)
		})
	}
}

// TestRequireRole_PapelDesconhecido403 prova o caso defensivo: um papel fora
// do enum conhecido tem rank 0 e fica abaixo de qualquer mínimo -> 403.
func TestRequireRole_PapelDesconhecido403(t *testing.T) {
	w, executou := chamarRequireRole(services.PapelUsuario, &services.UsuarioSessao{Papel: "", Ativo: true})
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
	if executou {
		t.Error("handler decorado executou para papel desconhecido")
	}
}

// TestRequireRole_ComposicaoRequireAuthAntes prova a ordem
// RequireAuth->RequireRole: sem token, RequireAuth responde 401 TOKEN_EXPIRED
// antes de RequireRole sequer rodar (nenhum 403, nenhum 500 de composição), e
// o handler decorado nunca executa — a mesma cadeia registrada em newMux.
func TestRequireRole_ComposicaoRequireAuthAntes(t *testing.T) {
	db := testDB(t)

	var executou bool
	next := func(w http.ResponseWriter, r *http.Request) {
		executou = true
		w.WriteHeader(http.StatusOK)
	}

	handler := RequireAuth(db, testJWTSecret)(RequireRole(services.PapelGestor)(next))

	req := httptest.NewRequest(http.MethodGet, "/api/usuarios", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "TOKEN_EXPIRED" {
		t.Errorf("code = %q, want %q (RequireAuth deve resolver a ausência de token antes de RequireRole)", env.Error.Code, "TOKEN_EXPIRED")
	}
	if executou {
		t.Error("handler decorado executou sem autenticação")
	}
}

// ===== Story 1.11: MFA obrigatório para papéis administrativos =====

// TestRequireRole_MFASetupRequired prova o novo gate: uma sessão
// `origem=senha` de papel gestor+ SEM MFA habilitado é recusada com 403
// MFA_SETUP_REQUIRED, mesmo com papel suficiente — o handler decorado nunca
// executa.
func TestRequireRole_MFASetupRequired(t *testing.T) {
	w, executou := chamarRequireRole(services.PapelGestor, &services.UsuarioSessao{
		Papel: services.PapelGestor, Ativo: true, Origem: "senha", MFAHabilitado: false,
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "MFA_SETUP_REQUIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "MFA_SETUP_REQUIRED")
	}
	if executou {
		t.Error("handler decorado executou apesar do 403 MFA_SETUP_REQUIRED")
	}
}

// TestRequireRole_MFAGate_OrigemSSONuncaDispara prova que uma sessão
// `origem=sso` NUNCA dispara o gate de MFA, mesmo sem MFA habilitado — o
// realm Keycloak já impõe MFA a esses papéis.
func TestRequireRole_MFAGate_OrigemSSONuncaDispara(t *testing.T) {
	w, executou := chamarRequireRole(services.PapelGestor, &services.UsuarioSessao{
		Papel: services.PapelGestor, Ativo: true, Origem: "sso", MFAHabilitado: false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	if !executou {
		t.Error("handler decorado não executou para sessão origem=sso")
	}
}

// TestRequireRole_MFAGate_OrigemVaziaNuncaDispara prova o fail-open
// deliberado para JWTs emitidos antes da migration desta story (sem o claim
// `origem`, decodificado como string vazia): tratados como "não senha" pelo
// gate de MFA até expirarem naturalmente.
func TestRequireRole_MFAGate_OrigemVaziaNuncaDispara(t *testing.T) {
	w, executou := chamarRequireRole(services.PapelGestor, &services.UsuarioSessao{
		Papel: services.PapelGestor, Ativo: true, Origem: "", MFAHabilitado: false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	if !executou {
		t.Error("handler decorado não executou para sessão sem claim origem")
	}
}

// TestRequireRole_MFAGate_MFAHabilitadoPassa prova que uma sessão
// `origem=senha` com MFA habilitado passa normalmente.
func TestRequireRole_MFAGate_MFAHabilitadoPassa(t *testing.T) {
	w, executou := chamarRequireRole(services.PapelGestor, &services.UsuarioSessao{
		Papel: services.PapelGestor, Ativo: true, Origem: "senha", MFAHabilitado: true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	if !executou {
		t.Error("handler decorado não executou para sessão com MFA habilitado")
	}
}

// TestRequireRole_MFAGate_PapelInsuficienteVenceAntes prova a ordem exigida
// pela spec: papel insuficiente SEMPRE vence primeiro (403 FORBIDDEN), nunca
// vazando MFA_SETUP_REQUIRED (que revelaria "esta rota existe e é
// restrita") para quem nem tem o papel mínimo — mesmo essa sessão também
// sendo origem=senha sem MFA.
func TestRequireRole_MFAGate_PapelInsuficienteVenceAntes(t *testing.T) {
	w, executou := chamarRequireRole(services.PapelGestor, &services.UsuarioSessao{
		Papel: services.PapelUsuario, Ativo: true, Origem: "senha", MFAHabilitado: false,
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
	env := decodeErro(t, w.Body.Bytes())
	if env.Error.Code != "FORBIDDEN" {
		t.Errorf("code = %q, want %q (papel insuficiente deve vencer antes do gate de MFA)", env.Error.Code, "FORBIDDEN")
	}
	if executou {
		t.Error("handler decorado executou apesar do papel insuficiente")
	}
}

// TestRequireRole_MFAGate_AbaixoDeGestorNuncaDispara prova que o gate de MFA
// nunca se aplica abaixo de gestor: `RequireRole(usuario)` nunca checa MFA,
// mesmo para uma sessão origem=senha sem MFA habilitado.
func TestRequireRole_MFAGate_AbaixoDeGestorNuncaDispara(t *testing.T) {
	w, executou := chamarRequireRole(services.PapelUsuario, &services.UsuarioSessao{
		Papel: services.PapelAlmoxarife, Ativo: true, Origem: "senha", MFAHabilitado: false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	if !executou {
		t.Error("handler decorado não executou — RequireRole(usuario) nunca deveria exigir MFA")
	}
}

// TestRequireRole_ComposicaoRequireAuthAntes_MFASetupRequired prova o gate de
// MFA (Story 1.11, condicional desde a Story 14.1) na composição real de
// newMux, RequireEmpresa->RequireAuth->RequireRole: um token com claim
// `origem=senha` de um gestor ativo SEM `mfa_habilitado`, numa Empresa com
// `mfa_obrigatorio=true`, recebe 403 MFA_SETUP_REQUIRED. Depois de um UPDATE
// do flag para false, o MESMO token passa na requisição seguinte (a Empresa é
// resolvida por slug a cada requisição, sem cache) — e volta a ser barrado
// quando o flag volta a true.
func TestRequireRole_ComposicaoRequireAuthAntes_MFASetupRequired(t *testing.T) {
	db := testDB(t)
	empresa := criarEmpresaMiddleware(t, db, "mw-mfa-exige", "55667788000186", "MW MFA Exige")
	definirMFAObrigatorio(t, db, empresa.ID, true)
	t.Cleanup(func() { _, _ = db.Exec(`UPDATE empresas SET mfa_obrigatorio = false WHERE id = $1`, empresa.ID) })

	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, mfa_habilitado, empresa_id)
		VALUES ('Gestor Sem MFA', $1, 'hash-qualquer', 'gestor', true, true, false, $2)
		RETURNING id`
	if err := db.QueryRow(insert, "requirerole-gestor-sem-mfa@empresa.com", empresa.ID).Scan(&id); err != nil {
		t.Fatalf("falha ao criar gestor de teste: %v", err)
	}
	token := gerarAccessTokenComOrigemTeste(t, testJWTSecret, id, "senha", time.Now().UTC().Add(30*time.Minute))

	var executou bool
	next := func(w http.ResponseWriter, r *http.Request) {
		executou = true
		w.WriteHeader(http.StatusOK)
	}
	handler := RequireAuth(db, testJWTSecret)(RequireRole(services.PapelGestor)(next))

	w := servirRotaDeEmpresa(db, empresa.Slug, handler, "Bearer "+token)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
	if code := codigoDoErro(t, w.Body.Bytes()); code != "MFA_SETUP_REQUIRED" {
		t.Errorf("code = %q, want %q", code, "MFA_SETUP_REQUIRED")
	}
	if executou {
		t.Error("handler decorado executou apesar do 403 MFA_SETUP_REQUIRED")
	}

	// Flag desligado: o mesmo token passa já na próxima requisição.
	definirMFAObrigatorio(t, db, empresa.ID, false)
	w = servirRotaDeEmpresa(db, empresa.Slug, handler, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("após mfa_obrigatorio=false: status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	if !executou {
		t.Error("handler decorado não executou com a Empresa sem exigência de MFA")
	}

	// Flag religado: barrado de novo, sem novo login.
	executou = false
	definirMFAObrigatorio(t, db, empresa.ID, true)
	w = servirRotaDeEmpresa(db, empresa.Slug, handler, "Bearer "+token)
	if w.Code != http.StatusForbidden {
		t.Fatalf("após mfa_obrigatorio=true: status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
	if executou {
		t.Error("handler decorado executou após a Empresa voltar a exigir MFA")
	}
}

// definirMFAObrigatorio grava `empresas.mfa_obrigatorio` direto no banco — a
// tela/rota de alteração só chega nas Stories 14.2/14.3.
func definirMFAObrigatorio(t *testing.T, db *sql.DB, empresaID string, valor bool) {
	t.Helper()
	if _, err := db.Exec(`UPDATE empresas SET mfa_obrigatorio = $1 WHERE id = $2`, valor, empresaID); err != nil {
		t.Fatalf("falha ao definir mfa_obrigatorio=%v: %v", valor, err)
	}
}

// TestRequireRole_ComposicaoRequireAuthAntes_OrigemSSOPassa prova o outro
// lado: a MESMA conta gestor sem MFA, mas com claim `origem=sso`, atravessa
// o gate normalmente — o realm Keycloak já impõe MFA a esses papéis.
func TestRequireRole_ComposicaoRequireAuthAntes_OrigemSSOPassa(t *testing.T) {
	db := testDB(t)
	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, mfa_habilitado, empresa_id)
		VALUES ('Gestor SSO Sem MFA', $1, 'hash-qualquer', 'gestor', true, true, false, $2)
		RETURNING id`
	if err := db.QueryRow(insert, "requirerole-gestor-sso@empresa.com", empresaContasMiddleware(t, db)).Scan(&id); err != nil {
		t.Fatalf("falha ao criar gestor de teste: %v", err)
	}
	token := gerarAccessTokenComOrigemTeste(t, testJWTSecret, id, "sso", time.Now().UTC().Add(30*time.Minute))

	var executou bool
	next := func(w http.ResponseWriter, r *http.Request) {
		executou = true
		w.WriteHeader(http.StatusOK)
	}
	handler := RequireAuth(db, testJWTSecret)(RequireRole(services.PapelGestor)(next))

	req := httptest.NewRequest(http.MethodGet, "/api/usuarios", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	if !executou {
		t.Error("handler decorado não executou para sessão origem=sso")
	}
}

// TestRequireRole_ComposicaoRequireAuthAntes_GestorPassa prova o outro lado
// da cadeia real: um token válido de um gestor ativo atravessa
// RequireAuth->RequireRole(gestor) e alcança o handler decorado.
func TestRequireRole_ComposicaoRequireAuthAntes_GestorPassa(t *testing.T) {
	db := testDB(t)
	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, empresa_id)
		VALUES ('Gestor Teste', $1, 'hash-qualquer', 'gestor', true, true, $2)
		RETURNING id`
	if err := db.QueryRow(insert, "requirerole-gestor@empresa.com", empresaContasMiddleware(t, db)).Scan(&id); err != nil {
		t.Fatalf("falha ao criar gestor de teste: %v", err)
	}
	token := gerarAccessTokenTeste(t, testJWTSecret, id, time.Now().UTC().Add(30*time.Minute))

	var executou bool
	next := func(w http.ResponseWriter, r *http.Request) {
		executou = true
		w.WriteHeader(http.StatusOK)
	}
	handler := RequireAuth(db, testJWTSecret)(RequireRole(services.PapelGestor)(next))

	req := httptest.NewRequest(http.MethodGet, "/api/usuarios", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	if !executou {
		t.Error("handler decorado não executou para gestor autenticado")
	}
}

// ===== Story 14.1: gate de MFA condicional à Empresa =====

// TestRequireRole_MFAGate_EmpresaNaoExigePassa prova o caso central da
// Story 14.1: gestor/adm por senha, sem MFA, numa Empresa que NÃO exige MFA,
// alcança o handler (200).
func TestRequireRole_MFAGate_EmpresaNaoExigePassa(t *testing.T) {
	for _, papel := range []string{services.PapelGestor, services.PapelAdm} {
		t.Run(papel, func(t *testing.T) {
			empresa := empresaNaoExigeMFA
			w, executou := chamarRequireRoleComEmpresa(services.PapelGestor, &services.UsuarioSessao{
				Papel: papel, Ativo: true, Origem: "senha", MFAHabilitado: false,
			}, &empresa)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
			}
			if !executou {
				t.Error("handler decorado não executou numa Empresa que não exige MFA")
			}
		})
	}
}

// TestRequireRole_MFAGate_AdmEmpresaExige403 prova que o gate também vale
// para adm (rota adm) quando a Empresa exige.
func TestRequireRole_MFAGate_AdmEmpresaExige403(t *testing.T) {
	w, executou := chamarRequireRole(services.PapelAdm, &services.UsuarioSessao{
		Papel: services.PapelAdm, Ativo: true, Origem: "senha", MFAHabilitado: false,
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "MFA_SETUP_REQUIRED" {
		t.Errorf("code = %q, want %q", env.Error.Code, "MFA_SETUP_REQUIRED")
	}
	if executou {
		t.Error("handler decorado executou apesar do 403 MFA_SETUP_REQUIRED")
	}
}

// TestRequireRole_MFAGate_SemEmpresa500 prova o fail-closed: com as três
// primeiras condições do gate verdadeiras e SEM Empresa no contexto (rota
// fora de RequireEmpresa), a resposta é 500 INTERNAL_ERROR — nunca abre a
// rota.
func TestRequireRole_MFAGate_SemEmpresa500(t *testing.T) {
	w, executou := chamarRequireRoleComEmpresa(services.PapelGestor, &services.UsuarioSessao{
		Papel: services.PapelGestor, Ativo: true, Origem: "senha", MFAHabilitado: false,
	}, nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusInternalServerError, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("code = %q, want %q", env.Error.Code, "INTERNAL_ERROR")
	}
	if executou {
		t.Error("handler decorado executou sem Empresa no contexto")
	}
}

// TestRequireRole_MFAGate_SemEmpresaForaDoGatePassa prova que a leitura da
// Empresa só acontece quando as três primeiras condições valem: uma sessão
// com MFA habilitado (ou SSO) passa mesmo sem Empresa no contexto.
func TestRequireRole_MFAGate_SemEmpresaForaDoGatePassa(t *testing.T) {
	casos := map[string]services.UsuarioSessao{
		"mfa-habilitado": {Papel: services.PapelGestor, Ativo: true, Origem: "senha", MFAHabilitado: true},
		"sso":            {Papel: services.PapelGestor, Ativo: true, Origem: "sso"},
	}
	for nome, usuario := range casos {
		t.Run(nome, func(t *testing.T) {
			u := usuario
			w, executou := chamarRequireRoleComEmpresa(services.PapelGestor, &u, nil)
			if w.Code != http.StatusOK || !executou {
				t.Fatalf("status = %d, executou = %v, want 200 e executou (body=%s)", w.Code, executou, w.Body.String())
			}
		})
	}
}

// TestRequireRole_MFAGate_PapelBaixoEmpresaExigePassa prova que
// usuario/almoxarife nunca são bloqueados pela exigência da Empresa, numa
// rota almoxarife ou abaixo.
func TestRequireRole_MFAGate_PapelBaixoEmpresaExigePassa(t *testing.T) {
	for _, papel := range []string{services.PapelUsuario, services.PapelAlmoxarife} {
		t.Run(papel, func(t *testing.T) {
			w, executou := chamarRequireRole(services.PapelUsuario, &services.UsuarioSessao{
				Papel: papel, Ativo: true, Origem: "senha", MFAHabilitado: false,
			})
			if w.Code != http.StatusOK || !executou {
				t.Fatalf("status = %d, executou = %v, want 200 e executou (body=%s)", w.Code, executou, w.Body.String())
			}
		})
	}
	// Um gestor numa rota almoxarife também não é barrado: o rank que decide
	// é o da ROTA, não o do usuário.
	w, executou := chamarRequireRole(services.PapelAlmoxarife, &services.UsuarioSessao{
		Papel: services.PapelGestor, Ativo: true, Origem: "senha", MFAHabilitado: false,
	})
	if w.Code != http.StatusOK || !executou {
		t.Fatalf("gestor em rota almoxarife: status = %d, executou = %v, want 200 (body=%s)", w.Code, executou, w.Body.String())
	}
}
