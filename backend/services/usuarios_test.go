package services

import (
	"database/sql"
	"testing"
)

// semearConta insere uma conta com papel e ordem de criação controlados.
// `ordem` vira um offset em `criado_em` para tornar o ORDER BY determinístico
// entre os testes.
func semearConta(t *testing.T, db *sql.DB, nome, email, papel string, ordem int) string {
	t.Helper()
	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, criado_em)
		VALUES ($1, $2, 'hash-qualquer', $3, true, true, now() + ($4 || ' seconds')::interval)
		RETURNING id`
	if err := db.QueryRow(insert, nome, email, papel, ordem).Scan(&id); err != nil {
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
		lista, err := ListarUsuarios(db, PapelGestor)
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
		lista, err := ListarUsuarios(db, PapelAdm)
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

	lista, err := ListarUsuarios(db, PapelAdm)
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

	lista, err := ListarUsuarios(db, PapelGestor)
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
