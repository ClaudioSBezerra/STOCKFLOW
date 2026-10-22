package services

import (
	"database/sql"
	"errors"
	"testing"
)

// semearConta insere uma conta com papel e ordem de criação controlados.
// `ordem` vira um offset em `criado_em` para tornar o ORDER BY determinístico
// entre os testes.
func semearConta(t *testing.T, db *sql.DB, nome, email, papel string, ordem int) string {
	t.Helper()
	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, criado_em, empresa_id)
		VALUES ($1, $2, 'hash-qualquer', $3, true, true, now() + ($4 || ' seconds')::interval, $5)
		RETURNING id`
	if err := db.QueryRow(insert, nome, email, papel, ordem, empresaTeste).Scan(&id); err != nil {
		t.Fatalf("falha ao semear conta %q: %v", email, err)
	}
	return id
}

// TestListarUsuarios_EscopoGestorVsAdm prova a AC3: com as quatro contas dos
// quatro papéis semeadas, um `gestor` recebe só as contas `usuario`/
// `almoxarife`, enquanto um `adm` recebe todas — o recorte é decidido pelo
// papel passado como argumento, sem reconsultar `usuarios`.
func TestListarUsuarios_EscopoGestorVsAdm(t *testing.T) {
	db := testDB(t)

	semearConta(t, db, "Ana Usuária", "ana.usuario@empresa.com", PapelUsuario, 1)
	semearConta(t, db, "Bruno Almoxarife", "bruno.almox@empresa.com", PapelAlmoxarife, 2)
	semearConta(t, db, "Carla Gestora", "carla.gestor@empresa.com", PapelGestor, 3)
	semearConta(t, db, "Diego Adm", "diego.adm@empresa.com", PapelAdm, 4)

	t.Run("gestor vê só usuario/almoxarife", func(t *testing.T) {
		lista, err := ListarUsuarios(db, empresaTeste, PapelGestor)
		if err != nil {
			t.Fatalf("ListarUsuarios(gestor) erro: %v", err)
		}
		if len(lista) != 2 {
			t.Fatalf("len = %d, want 2 (%+v)", len(lista), lista)
		}
		for _, u := range lista {
			if u.Papel != PapelUsuario && u.Papel != PapelAlmoxarife {
				t.Errorf("gestor recebeu conta de papel %q — fora do escopo", u.Papel)
			}
		}
	})

	t.Run("adm vê todas as contas", func(t *testing.T) {
		lista, err := ListarUsuarios(db, empresaTeste, PapelAdm)
		if err != nil {
			t.Fatalf("ListarUsuarios(adm) erro: %v", err)
		}
		if len(lista) != 4 {
			t.Fatalf("len = %d, want 4 (%+v)", len(lista), lista)
		}
		papeis := map[string]bool{}
		for _, u := range lista {
			papeis[u.Papel] = true
		}
		for _, p := range []string{PapelUsuario, PapelAlmoxarife, PapelGestor, PapelAdm} {
			if !papeis[p] {
				t.Errorf("adm não recebeu nenhuma conta de papel %q", p)
			}
		}
	})
}

// TestListarUsuarios_OrdenadoPorCriadoEm prova que a listagem sai ordenada
// por `criado_em` ascendente, independente da ordem de inserção.
func TestListarUsuarios_OrdenadoPorCriadoEm(t *testing.T) {
	db := testDB(t)

	semearConta(t, db, "Terceira", "terceira@empresa.com", PapelUsuario, 30)
	semearConta(t, db, "Primeira", "primeira@empresa.com", PapelUsuario, 10)
	semearConta(t, db, "Segunda", "segunda@empresa.com", PapelAlmoxarife, 20)

	lista, err := ListarUsuarios(db, empresaTeste, PapelAdm)
	if err != nil {
		t.Fatalf("ListarUsuarios erro: %v", err)
	}
	want := []string{"Primeira", "Segunda", "Terceira"}
	if len(lista) != len(want) {
		t.Fatalf("len = %d, want %d", len(lista), len(want))
	}
	for i, nome := range want {
		if lista[i].Nome != nome {
			t.Errorf("posição %d: nome = %q, want %q", i, lista[i].Nome, nome)
		}
	}
}

// TestListarUsuarios_ListaVaziaNaoEhErro prova que uma base sem nenhuma conta
// no escopo devolve slice vazio (nunca nil, nunca erro) — o handler serializa
// isso como `{"usuarios":[]}`.
func TestListarUsuarios_ListaVaziaNaoEhErro(t *testing.T) {
	db := testDB(t)

	lista, err := ListarUsuarios(db, empresaTeste, PapelGestor)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if lista == nil {
		t.Fatal("lista = nil, want slice vazio")
	}
	if len(lista) != 0 {
		t.Fatalf("len = %d, want 0", len(lista))
	}
}

// TestCriarUsuarioDireto_SucessoComPapelEMFAEscolhidos prova o caminho feliz
// (feedback Ferreira Costa, direção, 2026-10-21): o `adm` escolhe livremente
// o papel (aqui, gestor — não o padrão `usuario` do convite) e marca MFA
// obrigatório individual; a conta nasce pronta para logar (email_verificado
// = true, sem convite nenhum) e a própria senha escolhida já autentica.
func TestCriarUsuarioDireto_SucessoComPapelEMFAEscolhidos(t *testing.T) {
	db := testDB(t)

	u, err := CriarUsuarioDireto(db, empresaTeste, "Karla Tenório", "karla.direto@empresa.com", "SenhaForte123", PapelGestor, true)
	if err != nil {
		t.Fatalf("CriarUsuarioDireto erro: %v", err)
	}
	if u.Nome != "Karla Tenório" || u.Email != "karla.direto@empresa.com" {
		t.Errorf("nome/email = %q/%q, want os informados", u.Nome, u.Email)
	}
	if u.Papel != PapelGestor {
		t.Errorf("papel = %q, want %q", u.Papel, PapelGestor)
	}
	if !u.Ativo {
		t.Error("ativo = false, want true")
	}
	if u.MFAHabilitado {
		t.Error("mfaHabilitado = true logo na criação — só fica true depois de configurar o TOTP")
	}
	if !u.MFAObrigatorioIndividual {
		t.Error("mfaObrigatorioIndividual = false, want true (foi o que o adm escolheu)")
	}

	var emailVerificado bool
	if err := db.QueryRow(`SELECT email_verificado FROM usuarios WHERE id = $1`, u.ID).Scan(&emailVerificado); err != nil {
		t.Fatalf("falha ao reler email_verificado: %v", err)
	}
	if !emailVerificado {
		t.Error("email_verificado = false — conta criada direto pelo adm não deveria exigir verificação")
	}

	// A conta já loga com a senha escolhida na criação, sem nenhum passo
	// extra (sem convite, sem link de primeiro acesso).
	if _, err := Login(db, empresaTeste, "karla.direto@empresa.com", "SenhaForte123"); err != nil {
		t.Errorf("Login logo após CriarUsuarioDireto falhou: %v", err)
	}
}

// TestCriarUsuarioDireto_PadraoSemMFAIndividual prova o outro lado da mesma
// escolha: mfaObrigatorioIndividual=false não força nada.
func TestCriarUsuarioDireto_PadraoSemMFAIndividual(t *testing.T) {
	db := testDB(t)

	u, err := CriarUsuarioDireto(db, empresaTeste, "Usuário Comum", "usuario.direto@empresa.com", "SenhaForte123", PapelUsuario, false)
	if err != nil {
		t.Fatalf("CriarUsuarioDireto erro: %v", err)
	}
	if u.MFAObrigatorioIndividual {
		t.Error("mfaObrigatorioIndividual = true, want false")
	}
}

// TestCriarUsuarioDireto_Validacoes cobre os quatro erros de validação sem
// nenhuma escrita: nome/e-mail/senha vazios (ErrCadastroValidacao), papel
// fora da hierarquia (ErrPapelInvalido) e senha fraca (ErrSenhaFraca).
func TestCriarUsuarioDireto_Validacoes(t *testing.T) {
	db := testDB(t)

	casos := []struct {
		nome, email, senha, papel string
		wantErr                   error
	}{
		{"", "x@empresa.com", "SenhaForte123", PapelUsuario, ErrCadastroValidacao},
		{"Nome", "", "SenhaForte123", PapelUsuario, ErrCadastroValidacao},
		{"Nome", "x@empresa.com", "", PapelUsuario, ErrCadastroValidacao},
		{"Nome", "email-sem-arroba", "SenhaForte123", PapelUsuario, ErrCadastroValidacao},
		{"Nome", "x@empresa.com", "SenhaForte123", "inexistente", ErrPapelInvalido},
		{"Nome", "x@empresa.com", "curta1", PapelUsuario, ErrSenhaFraca},
	}
	for _, c := range casos {
		_, err := CriarUsuarioDireto(db, empresaTeste, c.nome, c.email, c.senha, c.papel, false)
		if !errors.Is(err, c.wantErr) {
			t.Errorf("CriarUsuarioDireto(%q,%q,%q,%q) erro = %v, want %v", c.nome, c.email, c.senha, c.papel, err, c.wantErr)
		}
	}
}

// TestCriarUsuarioDireto_EmailDuplicado prova que o mesmo e-mail nesta
// Empresa recusa com ErrEmailDuplicado — mesma restrição já usada por
// Cadastrar (convite), aqui disparada pelo próprio banco (índice único),
// sem pré-checagem separada.
func TestCriarUsuarioDireto_EmailDuplicado(t *testing.T) {
	db := testDB(t)

	if _, err := CriarUsuarioDireto(db, empresaTeste, "Primeira", "duplicado@empresa.com", "SenhaForte123", PapelUsuario, false); err != nil {
		t.Fatalf("primeira criação falhou: %v", err)
	}
	_, err := CriarUsuarioDireto(db, empresaTeste, "Segunda", "DUPLICADO@empresa.com", "SenhaForte123", PapelUsuario, false)
	if !errors.Is(err, ErrEmailDuplicado) {
		t.Errorf("erro = %v, want ErrEmailDuplicado", err)
	}
}
