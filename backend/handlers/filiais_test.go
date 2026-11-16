package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// Testes HTTP das Filiais — Story 12.1 (spec-12-1), despachados pela MESMA
// composição de newMux: POST/PUT -> RequireAuth -> RequireRole(gestor)
// (spec-filial-dados-fiscais; era adm); GET -> RequireAuth apenas.

func postFiliais(db *sql.DB, authHeader, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /e/{slug}/api/filiais",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(
				middleware.RequireRole(services.PapelGestor)(
					CriarFilialHandler(db)))))
	r := httptest.NewRequest(http.MethodPost, prefixoEmpresaTeste+"/api/filiais", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func getFiliais(db *sql.DB, authHeader string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /e/{slug}/api/filiais",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(
				ListarFiliaisHandler(db))))
	r := httptest.NewRequest(http.MethodGet, prefixoEmpresaTeste+"/api/filiais", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func limparFiliaisDeTeste(t *testing.T, db *sql.DB, prefixo string) {
	t.Helper()
	limpar := func() {
		_, _ = db.Exec(`DELETE FROM estoques WHERE filial_id IN (SELECT id FROM filiais WHERE nome LIKE $1)`, prefixo+"%")
		_, _ = db.Exec(`DELETE FROM filiais WHERE nome LIKE $1`, prefixo+"%")
	}
	limpar()
	t.Cleanup(limpar)
}

func TestCriarFilialHandler_201ParaAdmEListagem(t *testing.T) {
	db := testDB(t)
	limparFiliaisDeTeste(t, db, "HF12 ")
	criarContaComPapel(t, db, "Adm", "filial-adm@empresa.com", "senha-123456", "adm")
	tokenAdm := tokenDeLogin(t, db, "filial-adm@empresa.com", "senha-123456")

	w := postFiliais(db, "Bearer "+tokenAdm, `{"nome":"  HF12 Recife "}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Filial map[string]any `json:"filial"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Filial["nome"] != "HF12 Recife" || resp.Filial["id"] == "" || resp.Filial["cnpj"] != nil ||
		resp.Filial["inscricaoEstadual"] != nil || resp.Filial["endereco"] != nil {
		t.Errorf("filial = %v, want {id, nome=\"HF12 Recife\", cnpj/inscricaoEstadual/endereco null}", resp.Filial)
	}
	var empresaDaFilial string
	if err := db.QueryRow(`SELECT empresa_id FROM filiais WHERE id = $1`, resp.Filial["id"]).Scan(&empresaDaFilial); err != nil || empresaDaFilial != empresaTeste {
		t.Errorf("empresa_id da filial = %q (err=%v), want a Empresa do contexto", empresaDaFilial, err)
	}

	// Qualquer conta autenticada lista (almoxarife precisa escolher a Filial).
	criarContaComPapel(t, db, "Almox", "filial-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := tokenDeLogin(t, db, "filial-almox@empresa.com", "senha-123456")
	lw := getFiliais(db, "Bearer "+tokenAlmox)
	if lw.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body=%s)", lw.Code, lw.Body.String())
	}
	var lista struct {
		Filiais []map[string]any `json:"filiais"`
	}
	if err := json.Unmarshal(lw.Body.Bytes(), &lista); err != nil {
		t.Fatalf("decode lista: %v", err)
	}
	achou := false
	for _, f := range lista.Filiais {
		if f["nome"] == "HF12 Recife" {
			achou = true
		}
	}
	if !achou {
		t.Errorf("filial criada não aparece na listagem: %v", lista.Filiais)
	}
	if w := getFiliais(db, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("GET sem token: status = %d, want 401", w.Code)
	}
}

func TestCriarFilialHandler_403AbaixoDeGestor(t *testing.T) {
	db := testDB(t)
	limparFiliaisDeTeste(t, db, "HF12 ")
	for _, papel := range []string{"usuario", "almoxarife"} {
		t.Run(papel, func(t *testing.T) {
			email := "filial-403-" + papel + "@empresa.com"
			criarContaComPapel(t, db, "Conta "+papel, email, "senha-123456", papel)
			token := tokenDeLogin(t, db, email, "senha-123456")
			w := postFiliais(db, "Bearer "+token, `{"nome":"HF12 Proibida"}`)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM filiais WHERE nome = 'HF12 Proibida'`).Scan(&n); err != nil || n != 0 {
				t.Errorf("filiais gravadas = %d (err=%v), want 0", n, err)
			}
		})
	}
}

func TestCriarFilialHandler_400E409(t *testing.T) {
	db := testDB(t)
	limparFiliaisDeTeste(t, db, "HF12 ")
	criarContaComPapel(t, db, "Adm", "filial-val-adm@empresa.com", "senha-123456", "adm")
	token := tokenDeLogin(t, db, "filial-val-adm@empresa.com", "senha-123456")

	casos := map[string]string{
		"vazio":         `{"nome":""}`,
		"em branco":     `{"nome":"   "}`,
		"ausente":       `{}`,
		"json inválido": `{"nome":`,
		"> 255 runes":   `{"nome":"` + strings.Repeat("é", 256) + `"}`,
	}
	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			w := postFiliais(db, "Bearer "+token, corpo)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "VALIDATION_ERROR" {
				t.Errorf("code = %q, want VALIDATION_ERROR", env.Error.Code)
			}
		})
	}

	if w := postFiliais(db, "Bearer "+token, `{"nome":"HF12 Dup"}`); w.Code != http.StatusCreated {
		t.Fatalf("primeiro: status = %d, want 201", w.Code)
	}
	w := postFiliais(db, "Bearer "+token, `{"nome":"hf12   dup "}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicado: status = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}
	if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != "CONFLICT" {
		t.Errorf("code = %q, want CONFLICT", env.Error.Code)
	}
}

func putFilial(db *sql.DB, authHeader, id, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /e/{slug}/api/filiais/{id}",
		comEmpresa(db,
			middleware.RequireAuth(db, testJWTSecret)(
				middleware.RequireRole(services.PapelGestor)(
					AtualizarFilialHandler(db)))))
	r := httptest.NewRequest(http.MethodPut, prefixoEmpresaTeste+"/api/filiais/"+id, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// cnpjComDV completa 12 dígitos com os dois dígitos verificadores do CNPJ.
func cnpjComDV(base12 string) string {
	dv := func(b string) byte {
		pesos := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}[13-len(b):]
		soma := 0
		for i, r := range b {
			soma += int(r-'0') * pesos[i]
		}
		if soma%11 < 2 {
			return '0'
		}
		return byte('0' + 11 - soma%11)
	}
	c := base12 + string(dv(base12))
	return c + string(dv(c))
}

// cnpjDaFilialTeste: CNPJ válido com a raiz (8 dígitos) da Empresa da suíte
// e o estabelecimento `filial4` — a raiz tem que ser a da Empresa.
func cnpjDaFilialTeste(t *testing.T, db *sql.DB, filial4 string) string {
	t.Helper()
	var cnpj string
	if err := db.QueryRow(`SELECT cnpj FROM empresas WHERE id = $1`, empresaTeste).Scan(&cnpj); err != nil {
		t.Fatalf("ler CNPJ da empresa: %v", err)
	}
	return cnpjComDV(strings.TrimSpace(cnpj)[:8] + filial4)
}

func TestAtualizarFilialHandler_DadosFiscais(t *testing.T) {
	db := testDB(t)
	limparFiliaisDeTeste(t, db, "HFF ")
	criarContaComPapel(t, db, "Gestor", "filial-put-gestor@empresa.com", "senha-123456", "gestor")
	token := "Bearer " + tokenDeLogin(t, db, "filial-put-gestor@empresa.com", "senha-123456")

	criar := func(nome string) string {
		t.Helper()
		w := postFiliais(db, token, `{"nome":"`+nome+`"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("criar %s: status = %d (body=%s)", nome, w.Code, w.Body.String())
		}
		var resp struct {
			Filial struct{ ID string } `json:"filial"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp.Filial.ID
	}
	id := criar("HFF Recife")
	outraID := criar("HFF Natal")
	cnpjFilialHandlerTeste := cnpjDaFilialTeste(t, db, "0901")
	cnpjOutraRaiz := cnpjComDV("998887770001") // raiz diferente da Empresa da suíte
	cnpjDV := cnpjFilialHandlerTeste[:13] + string('0'+(cnpjFilialHandlerTeste[13]-'0'+1)%10)

	corpoValido := `{"nome":"HFF Recife","cnpj":"` + cnpjFilialHandlerTeste + `","inscricaoEstadual":"ISENTO",
		"endereco":{"logradouro":"Av. Recife","numero":"1000","complemento":"","bairro":"Areias",
		"cidade":"Recife","cep":"50780-000","uf":"pe"}}`
	w := putFilial(db, token, id, corpoValido)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT válido: status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Filial services.Filial `json:"filial"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	f := resp.Filial
	if f.CNPJ == nil || *f.CNPJ != cnpjFilialHandlerTeste || f.InscricaoEstadual == nil || *f.InscricaoEstadual != "ISENTO" ||
		f.Endereco == nil || f.Endereco.CEP != "50780000" || f.Endereco.UF != "PE" || f.Endereco.Cidade != "Recife" {
		t.Errorf("filial = %+v / endereco = %+v", f, f.Endereco)
	}

	casos := []struct {
		nome, id, corpo string
		status          int
		code            string
	}{
		{"CNPJ inválido", id, `{"nome":"HFF Recife","cnpj":"` + cnpjDV + `"}`, 400, "VALIDATION_ERROR"},
		{"CNPJ de outra raiz", id, `{"cnpj":"` + cnpjOutraRaiz + `"}`, 400, "VALIDATION_ERROR"},
		{"CEP com letras", id, `{"endereco":{"cep":"5O780-000"}}`, 400, "VALIDATION_ERROR"},
		{"IE com quebra de linha", id, `{"inscricaoEstadual":"12\n34"}`, 400, "VALIDATION_ERROR"},
		{"endereço incompleto", id, `{"endereco":{"bairro":""}}`, 400, "VALIDATION_ERROR"},
		{"nome vazio", id, `{"nome":"  "}`, 400, "VALIDATION_ERROR"},
		{"json inválido", id, `{"nome":`, 400, "VALIDATION_ERROR"},
		{"CNPJ repetido", outraID, `{"nome":"HFF Natal","cnpj":"` + cnpjFilialHandlerTeste + `"}`, 409, "CONFLICT"},
		{"nome repetido", outraID, `{"nome":"hff recife"}`, 409, "CONFLICT"},
		{"inexistente", "00000000-0000-4000-8000-000000000000", `{"nome":"HFF X"}`, 404, "NOT_FOUND"},
		{"malformado", "nao-e-uuid", `{"nome":"HFF X"}`, 404, "NOT_FOUND"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w := putFilial(db, token, c.id, c.corpo)
			if w.Code != c.status {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, c.status, w.Body.String())
			}
			if env := decodeErro(t, w.Body.Bytes()); env.Error.Code != c.code {
				t.Errorf("code = %q, want %q", env.Error.Code, c.code)
			}
		})
	}
	var cnpj, cidade sql.NullString
	if err := db.QueryRow(`SELECT cnpj, cidade FROM filiais WHERE id = $1`, id).Scan(&cnpj, &cidade); err != nil ||
		cnpj.String != cnpjFilialHandlerTeste || cidade.String != "Recife" {
		t.Errorf("edições recusadas alteraram a Filial: cnpj=%v cidade=%v err=%v", cnpj, cidade, err)
	}

	// PUT parcial: só {"nome"} mantém CNPJ/IE/endereço; "" limpa só o campo.
	if w := putFilial(db, token, id, `{"nome":"HFF Recife Centro"}`); w.Code != http.StatusOK {
		t.Fatalf("PUT só nome: status = %d (body=%s)", w.Code, w.Body.String())
	} else if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Filial.Nome != "HFF Recife Centro" ||
		resp.Filial.CNPJ == nil || *resp.Filial.CNPJ != cnpjFilialHandlerTeste || resp.Filial.InscricaoEstadual == nil ||
		resp.Filial.Endereco == nil || resp.Filial.Endereco.Cidade != "Recife" {
		t.Errorf("PUT só nome apagou dados fiscais: %+v (err=%v)", resp.Filial, err)
	}
	if w := putFilial(db, token, id, `{"inscricaoEstadual":"","cnpj":""}`); w.Code != http.StatusOK {
		t.Fatalf("PUT limpando IE/CNPJ: status = %d (body=%s)", w.Code, w.Body.String())
	} else {
		var r2 struct {
			Filial services.Filial `json:"filial"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &r2); err != nil || r2.Filial.CNPJ != nil || r2.Filial.InscricaoEstadual != nil ||
			r2.Filial.Endereco == nil || r2.Filial.Nome != "HFF Recife Centro" {
			t.Errorf("PUT com \"\" deveria limpar só CNPJ/IE: %+v (err=%v)", r2.Filial, err)
		}
	}

	// Filial de OUTRA Empresa -> 404, nada gravado.
	var outraEmpresa string
	if err := db.QueryRow(`SELECT id FROM empresas WHERE id <> $1 ORDER BY criado_em LIMIT 1`, empresaTeste).Scan(&outraEmpresa); err == nil {
		var filialAlheia string
		if err := db.QueryRow(`INSERT INTO filiais (empresa_id, nome) VALUES ($1, 'HFF Alheia') RETURNING id`, outraEmpresa).Scan(&filialAlheia); err != nil {
			t.Fatalf("seed filial alheia: %v", err)
		}
		if w := putFilial(db, token, filialAlheia, `{"nome":"HFF Invadida"}`); w.Code != http.StatusNotFound {
			t.Errorf("PUT em Filial de outra Empresa: status = %d, want 404", w.Code)
		}
		var nome string
		if err := db.QueryRow(`SELECT nome FROM filiais WHERE id = $1`, filialAlheia).Scan(&nome); err != nil || nome != "HFF Alheia" {
			t.Errorf("Filial alheia alterada: nome=%q err=%v", nome, err)
		}
	} else {
		t.Logf("sem segunda Empresa no banco de teste — caso 'outra Empresa' coberto em services")
	}

	// Almoxarife -> 403, nada gravado.
	criarContaComPapel(t, db, "Almox", "filial-put-almox@empresa.com", "senha-123456", "almoxarife")
	tokenAlmox := "Bearer " + tokenDeLogin(t, db, "filial-put-almox@empresa.com", "senha-123456")
	if w := putFilial(db, tokenAlmox, id, `{"nome":"HFF Almox"}`); w.Code != http.StatusForbidden {
		t.Errorf("PUT almoxarife: status = %d, want 403", w.Code)
	}
	if w := putFilial(db, "", id, `{"nome":"HFF Sem Token"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("PUT sem token: status = %d, want 401", w.Code)
	}
}
