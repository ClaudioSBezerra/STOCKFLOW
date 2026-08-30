package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"stockflow/backend/services"
)

// chamarRequireRole compõe RequireRole(papelMinimo) sobre um handler que
// grava 200 e registra que executou. Quando usuario != nil, injeta o
// UsuarioSessao no contexto exatamente como RequireAuth faria — sem passar
// por RequireAuth, para exercitar RequireRole isoladamente.
func chamarRequireRole(papelMinimo string, usuario *services.UsuarioSessao) (*httptest.ResponseRecorder, bool) {
	var executou bool
	next := func(w http.ResponseWriter, r *http.Request) {
		executou = true
		w.WriteHeader(http.StatusOK)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/usuarios", nil)
	if usuario != nil {
		ctx := context.WithValue(req.Context(), usuarioSessaoCtxKey, *usuario)
		req = req.WithContext(ctx)
	}
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

// TestRequireRole_ComposicaoRequireAuthAntes_GestorPassa prova o outro lado
// da cadeia real: um token válido de um gestor ativo atravessa
// RequireAuth->RequireRole(gestor) e alcança o handler decorado.
func TestRequireRole_ComposicaoRequireAuthAntes_GestorPassa(t *testing.T) {
	db := testDB(t)
	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
		VALUES ('Gestor Teste', $1, 'hash-qualquer', 'gestor', true, true)
		RETURNING id`
	if err := db.QueryRow(insert, "requirerole-gestor@empresa.com").Scan(&id); err != nil {
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
