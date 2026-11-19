package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"stockflow/backend/iam"
	"stockflow/backend/services"
)

// TestNewMux_TermosBaixaAcesso (spec-termo-baixa-pendencias, patches da
// revisão): adm baixa; solicitante com pedidos próprios não baixa o Termo de
// outro (404); Empresa B não lista nem baixa Termo da Empresa A; paginação
// inválida -> 400; gestor sem MFA em Empresa que exige MFA -> 403
// MFA_SETUP_REQUIRED no PDF e na lista.
func TestNewMux_TermosBaixaAcesso(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE usuarios CASCADE`); err != nil {
		t.Fatalf("truncate usuarios: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE importacao_linhas, importacoes, normalizacao_ignoradas, mesclagem_produtos_removidos, produto_historico, mesclagens_duplicatas, carrinho_itens, pedido_itens, reservas_pedido_item, pedidos, produto_estoque, lotes, produtos, estoques, movimentacoes`); err != nil {
		t.Fatalf("truncate produtos: %v", err)
	}
	mux := newMux(db, services.CarregarEmailConfig(), []byte("segredo-de-teste-nao-usar-em-producao"), iam.Config{}, t.TempDir())

	const senha = "senha-123456"
	segredos := map[string]string{}
	emails := map[string]string{
		"ana": "tb-acesso-ana@empresa.com", "bruno": "tb-acesso-bruno@empresa.com",
		"gestor": "tb-acesso-gestor@empresa.com", "adm": "tb-acesso-adm@empresa.com",
		"gestorsemmfa": "tb-acesso-semmfa@empresa.com",
	}
	papeis := map[string]string{"ana": "usuario", "bruno": "usuario", "gestor": "gestor", "adm": "adm", "gestorsemmfa": "gestor"}
	for k, email := range emails {
		seedContaMux(t, db, email, papeis[k], senha, segredos)
	}
	if _, err := db.Exec(`UPDATE usuarios SET mfa_habilitado = false, mfa_secret = NULL WHERE email = $1`, emails["gestorsemmfa"]); err != nil {
		t.Fatal(err)
	}

	despachar := func(slugPrefixo, metodo, caminho, token, corpo string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(metodo, slugPrefixo+caminho, strings.NewReader(corpo))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	a := func(metodo, caminho, token, corpo string) *httptest.ResponseRecorder {
		return despachar(prefixoEmpresaTeste, metodo, caminho, token, corpo)
	}
	tokens := map[string]string{}
	for k, email := range emails {
		tokens[k] = tokenDeMux(t, mux, email, senha, segredos)
	}

	var categoriaID string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE codigo = '04.001' AND empresa_id = $1`, empresaTeste).Scan(&categoriaID); err != nil {
		t.Fatal(err)
	}
	estoque, err := services.CriarEstoque(db, empresaTeste, filialTeste(t, db, empresaTeste), "Canteiro Acesso Termo")
	if err != nil {
		t.Fatal(err)
	}
	produto, err := criarProdutoComSaldo(db, empresaTeste, services.CriarProdutoInput{
		UnidadeMedida: "un", TemplateID: templateGenericoIDMux(t, db), Nome: "Produto Acesso Termo", CategoriaID: categoriaID,
	}, estoque.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	pedir := func(quem, motivo string) string {
		corpo := `{"produtoId":"` + produto.ID + `","estoqueId":"` + estoque.ID + `","quantidade":1,"motivo":"` + motivo + `","observacao":"Justificativa ` + quem + `"}`
		w := a(http.MethodPost, "/api/pedidos/baixa", tokens[quem], corpo)
		if w.Code != http.StatusCreated {
			t.Fatalf("pedido de %s: %d %s", quem, w.Code, w.Body.String())
		}
		var r struct {
			Pedido struct {
				ID string `json:"id"`
			} `json:"pedido"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &r)
		return r.Pedido.ID
	}
	pa, pb := pedir("ana", "perda_roubo"), pedir("bruno", "vencido_avariado")
	w := a(http.MethodPost, "/api/pedidos/decisao-lote", tokens["gestor"], `{"pedidoIds":["`+pa+`","`+pb+`"],"aprovar":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("decisao-lote: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Pedidos []struct {
			ID           string `json:"id"`
			TermoBaixaID string `json:"termoBaixaId"`
			NumeroTermo  int    `json:"numeroTermo"`
		} `json:"pedidos"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	termo := map[string]string{}
	numero := map[string]int{}
	for _, p := range resp.Pedidos {
		quem := map[string]string{pa: "ana", pb: "bruno"}[p.ID]
		termo[quem], numero[quem] = p.TermoBaixaID, p.NumeroTermo
	}
	if termo["ana"] == "" || termo["bruno"] == "" || termo["ana"] == termo["bruno"] {
		t.Fatalf("termos = %v, want dois Termos distintos", termo)
	}
	pdf := func(quem string) string { return "/api/termos-baixa/" + termo[quem] + "/pdf" }

	t.Run("adm baixa qualquer Termo", func(t *testing.T) {
		for _, quem := range []string{"ana", "bruno"} {
			if w := a(http.MethodGet, pdf(quem), tokens["adm"], ""); w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "%PDF-") {
				t.Errorf("adm/%s: %d", quem, w.Code)
			}
		}
	})

	t.Run("solicitante com pedido próprio não baixa o Termo de outro (404)", func(t *testing.T) {
		if w := a(http.MethodGet, pdf("ana"), tokens["ana"], ""); w.Code != http.StatusOK {
			t.Errorf("ana no próprio: %d", w.Code)
		}
		if w := a(http.MethodGet, pdf("bruno"), tokens["ana"], ""); w.Code != http.StatusNotFound {
			t.Errorf("ana no Termo do bruno: %d, want 404", w.Code)
		}
		if w := a(http.MethodGet, pdf("ana"), tokens["bruno"], ""); w.Code != http.StatusNotFound {
			t.Errorf("bruno no Termo da ana: %d, want 404", w.Code)
		}
	})

	t.Run("paginação e filtros inválidos -> 400; limite 0/500 valem", func(t *testing.T) {
		for _, q := range []string{"?limite=-1", "?offset=abc", "?offset=-5", "?numero=2147483648", "?q=%00", "?q=%FF", "?de=1999-01-01", "?ate=2101-01-01"} {
			if w := a(http.MethodGet, "/api/termos-baixa"+q, tokens["gestor"], ""); w.Code != http.StatusBadRequest {
				t.Errorf("%s: %d, want 400", q, w.Code)
			}
		}
		for _, q := range []string{"?limite=0", "?limite=500"} {
			w := a(http.MethodGet, "/api/termos-baixa"+q, tokens["gestor"], "")
			var r struct {
				Termos []json.RawMessage `json:"termos"`
				Total  int               `json:"total"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &r)
			if w.Code != http.StatusOK || len(r.Termos) < 2 || r.Total < 2 {
				t.Errorf("%s: %d termos=%d total=%d", q, w.Code, len(r.Termos), r.Total)
			}
		}
		w := a(http.MethodGet, "/api/termos-baixa?limite=1", tokens["gestor"], "")
		var r struct {
			Termos []json.RawMessage `json:"termos"`
			Total  int               `json:"total"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &r)
		if len(r.Termos) != 1 || r.Total < 2 {
			t.Errorf("limite=1: termos=%d total=%d, want 1 e total >= 2", len(r.Termos), r.Total)
		}
	})

	t.Run("Empresa B não lista nem baixa Termo da Empresa A", func(t *testing.T) {
		var sobra string
		if err := db.QueryRow(`SELECT id FROM empresas WHERE slug = 'termo-acesso-b'`).Scan(&sobra); err == nil {
			removerEmpresaMux(t, db, sobra) // sobra de execução interrompida
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		empresaB, err := services.ProvisionarEmpresa(tx, services.DadosEmpresa{
			NomeFantasia: "Empresa B Termo", RazaoSocial: "Empresa B Termo LTDA", CNPJ: "11444777000161", Slug: "termo-acesso-b",
			Endereco: services.EnderecoEmpresa{Logradouro: "Rua B", Numero: "1", Bairro: "Centro", Cidade: "Recife", CEP: "50000000", UF: "PE"},
		})
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("ProvisionarEmpresa: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { removerEmpresaMux(t, db, empresaB.ID) })
		hash, _ := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
		if _, err := db.Exec(`INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, empresa_id)
			VALUES ('Gestor B', 'tb-acesso-b@empresab.com', $1, 'gestor', true, true, $2)`, string(hash), empresaB.ID); err != nil {
			t.Fatal(err)
		}
		const prefixoB = "/e/termo-acesso-b"
		login := despachar(prefixoB, http.MethodPost, "/api/auth/login", "", `{"email":"tb-acesso-b@empresab.com","senha":"`+senha+`"}`)
		var lb struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(login.Body.Bytes(), &lb)
		if login.Code != http.StatusOK || lb.Token == "" {
			t.Fatalf("login B: %d %s", login.Code, login.Body.String())
		}
		lista := despachar(prefixoB, http.MethodGet, "/api/termos-baixa", lb.Token, "")
		var r struct {
			Termos []json.RawMessage `json:"termos"`
			Total  int               `json:"total"`
		}
		_ = json.Unmarshal(lista.Body.Bytes(), &r)
		if lista.Code != http.StatusOK || len(r.Termos) != 0 || r.Total != 0 {
			t.Errorf("lista B: %d termos=%d total=%d, want vazia", lista.Code, len(r.Termos), r.Total)
		}
		if w := despachar(prefixoB, http.MethodGet, pdf("ana"), lb.Token, ""); w.Code != http.StatusNotFound {
			t.Errorf("PDF do Termo de A sob a Empresa B: %d, want 404", w.Code)
		}
		// Token da Empresa A sob o slug da B nunca vale.
		if w := despachar(prefixoB, http.MethodGet, pdf("ana"), tokens["gestor"], ""); w.Code != http.StatusUnauthorized {
			t.Errorf("token de A sob slug de B: %d, want 401", w.Code)
		}
	})

	t.Run("gestor sem MFA em Empresa que exige MFA -> 403 MFA_SETUP_REQUIRED", func(t *testing.T) {
		if w := a(http.MethodGet, pdf("ana"), tokens["gestorsemmfa"], ""); w.Code != http.StatusOK {
			t.Fatalf("pré-condição (Empresa sem exigência): %d", w.Code)
		}
		if _, err := db.Exec(`UPDATE empresas SET mfa_obrigatorio = true WHERE id = $1`, empresaTeste); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = db.Exec(`UPDATE empresas SET mfa_obrigatorio = false WHERE id = $1`, empresaTeste) })
		for _, caminho := range []string{pdf("ana"), "/api/termos-baixa"} {
			w := a(http.MethodGet, caminho, tokens["gestorsemmfa"], "")
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "MFA_SETUP_REQUIRED") {
				t.Errorf("%s: %d %s, want 403 MFA_SETUP_REQUIRED", caminho, w.Code, w.Body.String())
			}
		}
		// Solicitante comum não é alcançado pela exigência da Empresa (só gestor+).
		if w := a(http.MethodGet, pdf("ana"), tokens["ana"], ""); w.Code != http.StatusOK {
			t.Errorf("ana com Empresa exigindo MFA: %d, want 200", w.Code)
		}
	})
}

// removerEmpresaMux apaga a Empresa de teste criada por ProvisionarEmpresa
// (mesma lista de tabelas de removerEmpresaDeTeste, do pacote services).
func removerEmpresaMux(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	for _, stmt := range []string{
		`DELETE FROM logs_acesso WHERE empresa_id = $1`,
		`DELETE FROM usuarios WHERE empresa_id = $1`,
		`DELETE FROM convites_empresa WHERE empresa_id = $1`,
		`DELETE FROM categorias WHERE empresa_id = $1`,
		`DELETE FROM nomenclatura_templates WHERE empresa_id = $1`,
		`DELETE FROM centros_custo WHERE empresa_id = $1`,
		`DELETE FROM filiais WHERE empresa_id = $1`,
		`DELETE FROM contadores_produto WHERE empresa_id = $1`,
		`DELETE FROM pre_notas WHERE empresa_id = $1`,
		`DELETE FROM contadores_pre_nota WHERE empresa_id = $1`,
		`DELETE FROM termos_baixa WHERE empresa_id = $1`,
		`DELETE FROM contadores_termo_baixa WHERE empresa_id = $1`,
		`DELETE FROM empresas WHERE id = $1`,
	} {
		if _, err := db.Exec(stmt, id); err != nil {
			t.Errorf("removerEmpresaMux [%s]: %v", stmt, err)
		}
	}
}
