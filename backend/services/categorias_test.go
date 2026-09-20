package services

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lib/pq"
)

// Testes do CRUD de Categorias — Story 10.5 (spec-10-5), DB real. As
// Categorias criadas aqui usam códigos "T…" (fora da faixa do seed) e são
// removidas ao fim de cada teste, para não vazar para as demais suítes que
// dependem das 25 linhas seed da Empresa padrão.

func limparCategoriasDeTeste(t *testing.T, db *sql.DB) {
	t.Helper()
	del := func() {
		limparProdutos(t, db) // Produtos de teste referenciam Categorias "T…" (FK)
		if _, err := db.Exec(`DELETE FROM categorias WHERE codigo LIKE 'T%'`); err != nil {
			t.Fatalf("limpar categorias de teste: %v", err)
		}
	}
	del()
	t.Cleanup(del)
}

func contarCategoriasDaEmpresa(t *testing.T, db *sql.DB, empresaID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM categorias WHERE empresa_id = $1`, empresaID).Scan(&n); err != nil {
		t.Fatalf("count categorias: %v", err)
	}
	return n
}

func empresaSegundaCategorias(t *testing.T, db *sql.DB) Empresa {
	t.Helper()
	const slug = "categorias-outra-empresa"
	if e, err := BuscarEmpresaPorSlug(db, slug); err == nil {
		return e
	}
	return criarEmpresaDeTeste(t, db, slug, "105000000001", "Categorias Outra Empresa")
}

func TestCriarCategoria_Sucesso(t *testing.T) {
	db := testDB(t)
	limparCategoriasDeTeste(t, db)

	c, err := CriarCategoria(db, empresaTeste, "  T14.001 ", "  Brindes  ")
	if err != nil {
		t.Fatalf("CriarCategoria: %v", err)
	}
	if c.ID == "" || c.Codigo != "T14.001" || c.Nome != "Brindes" {
		t.Errorf("categoria = %+v, want código/nome trimados e id preenchido", c)
	}
	var emp string
	if err := db.QueryRow(`SELECT empresa_id FROM categorias WHERE id = $1`, c.ID).Scan(&emp); err != nil || emp != empresaTeste {
		t.Errorf("empresa_id = %q (err=%v), want %q", emp, err, empresaTeste)
	}
}

func TestCriarCategoria_Validacao(t *testing.T) {
	db := testDB(t)
	limparCategoriasDeTeste(t, db)
	antes := contarCategoriasDaEmpresa(t, db, empresaTeste)

	casos := map[string][2]string{
		"codigo 9 runes": {"T12345678", "Nome"},
		"nome 51 runes":  {"T1", strings.Repeat("ç", 51)},
		"codigo vazio":   {"   ", "Nome"},
		"nome vazio":     {"T1", "   "},
		"ambos ausentes": {"", ""},
		"byte NUL":       {"T1", "No\x00me"},
	}
	for nome, c := range casos {
		t.Run(nome, func(t *testing.T) {
			if _, err := CriarCategoria(db, empresaTeste, c[0], c[1]); !errors.Is(err, ErrCategoriaValidacao) {
				t.Fatalf("erro = %v, want ErrCategoriaValidacao", err)
			}
		})
	}
	if n := contarCategoriasDaEmpresa(t, db, empresaTeste); n != antes {
		t.Errorf("categorias = %d, want %d (nada gravado)", n, antes)
	}

	// Limites em runes: 8 e 50 caracteres multibyte são aceitos.
	c, err := CriarCategoria(db, empresaTeste, "Tééééééé", strings.Repeat("ã", 50))
	if err != nil {
		t.Fatalf("limite exato deveria passar: %v", err)
	}
	if utf8.RuneCountInString(c.Nome) != 50 {
		t.Errorf("nome com %d runes, want 50", utf8.RuneCountInString(c.Nome))
	}
}

// TestCategorias_LimitesNoBanco prova que 8/50 e o não-vazio são impostos
// pelo próprio banco (migration 000039), sem passar pela service.
func TestCategorias_LimitesNoBanco(t *testing.T) {
	db := testDB(t)
	limparCategoriasDeTeste(t, db)

	casos := map[string]struct {
		codigo, nome, sqlstate string
	}{
		"codigo 9":     {"T12345678", "Nome", "22001"},
		"nome 51":      {"T1", strings.Repeat("x", 51), "22001"},
		"codigo vazio": {"  ", "Nome", "23514"},
		"nome vazio":   {"T2", "  ", "23514"},
	}
	for nome, c := range casos {
		t.Run(nome, func(t *testing.T) {
			_, err := db.Exec(`INSERT INTO categorias (codigo, nome, empresa_id) VALUES ($1, $2, $3)`, c.codigo, c.nome, empresaTeste)
			var pqErr *pq.Error
			if !errors.As(err, &pqErr) || string(pqErr.Code) != c.sqlstate {
				t.Fatalf("erro = %v, want SQLSTATE %s", err, c.sqlstate)
			}
		})
	}
	var codigoTipo, nomeTipo string
	if err := db.QueryRow(`SELECT
		(SELECT character_maximum_length FROM information_schema.columns WHERE table_name='categorias' AND column_name='codigo')::text,
		(SELECT character_maximum_length FROM information_schema.columns WHERE table_name='categorias' AND column_name='nome')::text`).Scan(&codigoTipo, &nomeTipo); err != nil {
		t.Fatalf("information_schema: %v", err)
	}
	if codigoTipo != "8" || nomeTipo != "50" {
		t.Errorf("tipos = VARCHAR(%s)/VARCHAR(%s), want 8/50", codigoTipo, nomeTipo)
	}

	// categorias_padrao (molde de ProvisionarEmpresa) recebe os mesmos limites.
	for nome, c := range casos {
		t.Run("padrao "+nome, func(t *testing.T) {
			_, err := db.Exec(`INSERT INTO categorias_padrao (codigo, nome) VALUES ($1, $2)`, c.codigo, c.nome)
			var pqErr *pq.Error
			if !errors.As(err, &pqErr) || string(pqErr.Code) != c.sqlstate {
				t.Fatalf("erro = %v, want SQLSTATE %s", err, c.sqlstate)
			}
		})
	}
	if err := db.QueryRow(`SELECT
		(SELECT character_maximum_length FROM information_schema.columns WHERE table_schema = current_schema() AND table_name='categorias_padrao' AND column_name='codigo')::text,
		(SELECT character_maximum_length FROM information_schema.columns WHERE table_schema = current_schema() AND table_name='categorias_padrao' AND column_name='nome')::text`).Scan(&codigoTipo, &nomeTipo); err != nil {
		t.Fatalf("information_schema categorias_padrao: %v", err)
	}
	if codigoTipo != "8" || nomeTipo != "50" {
		t.Errorf("categorias_padrao tipos = VARCHAR(%s)/VARCHAR(%s), want 8/50", codigoTipo, nomeTipo)
	}
}

func TestCriarCategoria_Duplicado(t *testing.T) {
	db := testDB(t)
	limparCategoriasDeTeste(t, db)
	if _, err := CriarCategoria(db, empresaTeste, "T1", "Original"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var dup *ErrCategoriaDuplicada
	_, err := CriarCategoria(db, empresaTeste, "T1", "Outro Nome")
	if !errors.As(err, &dup) || dup.Campo != "código" {
		t.Errorf("mesmo código: erro = %v, want ErrCategoriaDuplicada{código}", err)
	}
	_, err = CriarCategoria(db, empresaTeste, "T2", "Original")
	if !errors.As(err, &dup) || dup.Campo != "nome" {
		t.Errorf("mesmo nome: erro = %v, want ErrCategoriaDuplicada{nome}", err)
	}
}

func TestCriarCategoria_MesmoCodigoEmOutraEmpresa(t *testing.T) {
	db := testDB(t)
	limparCategoriasDeTeste(t, db)
	outra := empresaSegundaCategorias(t, db)

	if _, err := CriarCategoria(db, empresaTeste, "T3", "Brindes"); err != nil {
		t.Fatalf("empresa A: %v", err)
	}
	if _, err := CriarCategoria(db, outra.ID, "T3", "Brindes"); err != nil {
		t.Fatalf("empresa B deveria aceitar o mesmo código/nome: %v", err)
	}
}

func TestAtualizarCategoria(t *testing.T) {
	db := testDB(t)
	limparCategoriasDeTeste(t, db)
	outra := empresaSegundaCategorias(t, db)

	a, _ := CriarCategoria(db, empresaTeste, "T4", "Quatro")
	CriarCategoria(db, empresaTeste, "T5", "Cinco")
	alheia, err := CriarCategoria(db, outra.ID, "T4", "Quatro")
	if err != nil {
		t.Fatalf("seed alheia: %v", err)
	}

	got, err := AtualizarCategoria(db, empresaTeste, a.ID, " T4B ", " Quatro B ")
	if err != nil || got.ID != a.ID || got.Codigo != "T4B" || got.Nome != "Quatro B" {
		t.Fatalf("atualizar = %+v, %v", got, err)
	}

	var dup *ErrCategoriaDuplicada
	if _, err := AtualizarCategoria(db, empresaTeste, a.ID, "T5", "Qualquer"); !errors.As(err, &dup) || dup.Campo != "código" {
		t.Errorf("duplicado código: %v", err)
	}
	if _, err := AtualizarCategoria(db, empresaTeste, a.ID, "T9", "Cinco"); !errors.As(err, &dup) || dup.Campo != "nome" {
		t.Errorf("duplicado nome: %v", err)
	}
	if _, err := AtualizarCategoria(db, empresaTeste, a.ID, "", "x"); !errors.Is(err, ErrCategoriaValidacao) {
		t.Errorf("vazio: %v", err)
	}

	for nome, id := range map[string]string{
		"alheia":      alheia.ID,
		"inexistente": "00000000-0000-4000-8000-000000000000",
		"malformado":  "nao-e-uuid",
		"id com NUL":  "0000\x00000",
	} {
		if _, err := AtualizarCategoria(db, empresaTeste, id, "T7", "Sete"); !errors.Is(err, ErrCategoriaNaoEncontrada) {
			t.Errorf("%s: erro = %v, want ErrCategoriaNaoEncontrada", nome, err)
		}
	}
	var nomeAlheia string
	db.QueryRow(`SELECT nome FROM categorias WHERE id = $1`, alheia.ID).Scan(&nomeAlheia)
	if nomeAlheia != "Quatro" {
		t.Errorf("categoria alheia alterada: %q", nomeAlheia)
	}
}

func TestExcluirCategoria(t *testing.T) {
	db := testDB(t)
	limparCategoriasDeTeste(t, db)
	limparProdutos(t, db)
	outra := empresaSegundaCategorias(t, db)

	livre, _ := CriarCategoria(db, empresaTeste, "T6", "Livre")
	alheia, _ := CriarCategoria(db, outra.ID, "T6", "Livre")

	for nome, id := range map[string]string{
		"alheia":      alheia.ID,
		"inexistente": "00000000-0000-4000-8000-000000000000",
		"malformado":  "nao-e-uuid",
		"id com NUL":  "0000\x00000",
	} {
		if err := ExcluirCategoria(db, empresaTeste, id); !errors.Is(err, ErrCategoriaNaoEncontrada) {
			t.Errorf("%s: erro = %v, want ErrCategoriaNaoEncontrada", nome, err)
		}
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM categorias WHERE id = $1`, alheia.ID).Scan(&n)
	if n != 1 {
		t.Errorf("categoria alheia removida")
	}

	if err := ExcluirCategoria(db, empresaTeste, livre.ID); err != nil {
		t.Fatalf("excluir sem uso: %v", err)
	}
	db.QueryRow(`SELECT count(*) FROM categorias WHERE id = $1`, livre.ID).Scan(&n)
	if n != 0 {
		t.Errorf("linha não removida")
	}
}

func TestExcluirCategoria_EmUso(t *testing.T) {
	db := testDB(t)
	limparCategoriasDeTeste(t, db)
	limparProdutos(t, db)

	c, _ := CriarCategoria(db, empresaTeste, "T8", "Em Uso")
	input := criarProdutoInputValido(t, db, "Produto Categoria Uso", "04.001")
	input.CategoriaID = c.ID
	if _, err := CriarProduto(db, empresaTeste, input); err != nil {
		t.Fatalf("seed produto: %v", err)
	}

	err := ExcluirCategoria(db, empresaTeste, c.ID)
	var emUso *ErroCategoriaEmUso
	if !errors.As(err, &emUso) || emUso.Produtos != 1 {
		t.Fatalf("erro = %v, want ErroCategoriaEmUso{1}", err)
	}
	if !strings.Contains(err.Error(), "1 produto") {
		t.Errorf("mensagem sem contagem: %q", err.Error())
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM categorias WHERE id = $1`, c.ID).Scan(&n)
	if n != 1 {
		t.Errorf("categoria em uso foi removida")
	}

	// Editar em uso é permitido.
	if _, err := AtualizarCategoria(db, empresaTeste, c.ID, "T8B", "Em Uso Editada"); err != nil {
		t.Errorf("editar em uso: %v", err)
	}
}

// TestCategorias_SeedIntactoEProvisionamento prova as linhas "Seed pós-
// migration" e "Provisionar Empresa nova" da I/O Matrix.
func TestCategorias_SeedIntactoEProvisionamento(t *testing.T) {
	db := testDB(t)

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM categorias_padrao`).Scan(&n); err != nil || n != 25 {
		t.Fatalf("categorias_padrao = %d (err=%v), want 25", n, err)
	}
	var nome string
	if err := db.QueryRow(`SELECT nome FROM categorias_padrao WHERE codigo = '09.001'`).Scan(&nome); err != nil {
		t.Fatalf("09.001 padrão: %v", err)
	}
	if nome != "Peças/Materiais p/ Equipamentos/Veículos/Máquinas" || utf8.RuneCountInString(nome) != 49 {
		t.Errorf("09.001 = %q (%d runes)", nome, utf8.RuneCountInString(nome))
	}

	removerEmpresaDeTeste(t, db, "categorias-provisionamento")
	e := criarEmpresaDeTeste(t, db, "categorias-provisionamento", "105000000002", "Categorias Provisionamento")
	if got := contarCategoriasDaEmpresa(t, db, e.ID); got != 25 {
		t.Errorf("categorias copiadas = %d, want 25", got)
	}
}
