package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

func getManual(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, prefixoEmpresaTeste+"/api/manual", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	comEmpresa(db, middleware.RequireAuth(db, testJWTSecret)(ManualHandler(db)))(w, req)
	return w
}

func getManualPDF(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, prefixoEmpresaTeste+"/api/manual/pdf", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	comEmpresa(db, middleware.RequireAuth(db, testJWTSecret)(
		middleware.RequireRole(services.PapelAdm)(BaixarManualPDFHandler())))(w, req)
	return w
}

type manualResposta struct {
	Secoes []services.SecaoManual `json:"secoes"`
}

// TestManualHandler_FiltraPorPapel prova que cada papel recebe só as seções
// visíveis a ele (services.SecoesManualParaPapel) e que o adm recebe o
// manual completo — mesma asserção de TestSecoesManualParaPapel_FiltraPorRank,
// agora na fronteira HTTP.
func TestManualHandler_FiltraPorPapel(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Usuária Manual", "manual-usuario@empresa.com", "senha-123456", "usuario")
	criarContaComPapel(t, db, "Adm Manual", "manual-adm@empresa.com", "senha-123456", "adm")

	tokenUsuario := tokenDeLogin(t, db, "manual-usuario@empresa.com", "senha-123456")
	tokenAdm := tokenDeLogin(t, db, "manual-adm@empresa.com", "senha-123456")

	wUsuario := getManual(db, "Bearer "+tokenUsuario)
	if wUsuario.Code != http.StatusOK {
		t.Fatalf("usuario: status = %d, want 200 (body=%s)", wUsuario.Code, wUsuario.Body.String())
	}
	respUsuario := decodeManual(t, wUsuario.Body.Bytes())
	for _, s := range respUsuario.Secoes {
		if s.PapelMinimo != "usuario" {
			t.Errorf("usuario recebeu seção %q com papelMinimo=%q", s.ID, s.PapelMinimo)
		}
	}

	wAdm := getManual(db, "Bearer "+tokenAdm)
	if wAdm.Code != http.StatusOK {
		t.Fatalf("adm: status = %d, want 200 (body=%s)", wAdm.Code, wAdm.Body.String())
	}
	respAdm := decodeManual(t, wAdm.Body.Bytes())
	if len(respAdm.Secoes) <= len(respUsuario.Secoes) {
		t.Errorf("adm (%d seções) deveria ver mais que usuario (%d)", len(respAdm.Secoes), len(respUsuario.Secoes))
	}
}

func decodeManual(t *testing.T, body []byte) manualResposta {
	t.Helper()
	var resp manualResposta
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("falha ao decodificar resposta do manual: %v (body=%s)", err, body)
	}
	return resp
}

func TestManualHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	w := getManual(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestBaixarManualPDFHandler_200Adm prova o download do manual completo em
// PDF — mesmas asserções de Content-Type/Content-Disposition/prefixo %PDF-
// já usadas para o recibo do Pedido (TestBaixarReciboPedidoHandler_200Dono).
func TestBaixarManualPDFHandler_200Adm(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Adm PDF", "manual-pdf-adm@empresa.com", "senha-123456", "adm")
	token := tokenDeLogin(t, db, "manual-pdf-adm@empresa.com", "senha-123456")

	w := getManualPDF(db, "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
	wantDisposition := `attachment; filename="manual-stockflow.pdf"`
	if cd := w.Header().Get("Content-Disposition"); cd != wantDisposition {
		t.Errorf("Content-Disposition = %q, want %q", cd, wantDisposition)
	}
	if !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF-")) {
		t.Errorf("corpo não começa com %%PDF-: %q", w.Body.Bytes()[:min(20, w.Body.Len())])
	}
}

// TestBaixarManualPDFHandler_403ParaGestor prova que o download em PDF é
// exclusivo de adm — gestor vê o manual online (GET /api/manual), mas não
// baixa o PDF.
func TestBaixarManualPDFHandler_403ParaGestor(t *testing.T) {
	db := testDB(t)
	criarContaComPapel(t, db, "Gestora PDF", "manual-pdf-gestor@empresa.com", "senha-123456", "gestor")
	token := tokenDeLogin(t, db, "manual-pdf-gestor@empresa.com", "senha-123456")

	w := getManualPDF(db, "Bearer "+token)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestBaixarManualPDFHandler_401SemToken(t *testing.T) {
	db := testDB(t)
	w := getManualPDF(db, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}
