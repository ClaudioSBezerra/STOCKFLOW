package main

import (
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
)

var (
	migrateOnce sync.Once
	migrateErr  error
)

// testDB abre duas conexões contra o MESMO servidor Postgres (DATABASE_URL):
//
//   - `alvo`: o schema novo do stockflow, com as migrations reais aplicadas
//     (via golang-migrate, source file://../../migrations — o mesmo diretório
//     embutido em main.go). A migration 000009 cria migracao_id_map.
//   - `legado`: simula o espelho do Firestore. Vive no schema `legado` do
//     mesmo servidor, alcançado por um DSN com o parâmetro search_path=legado
//     (repassado pelo lib/pq ao backend). A tabela legado.estoques replica a
//     estrutura documentada no addendum §F: `id` textual (doc id) + `nome`.
//
// Cada teste começa com legado.estoques, estoques e migracao_id_map vazias.
// Pula quando nenhum Postgres foi configurado: suba um com
// `docker compose up -d db` e exporte DATABASE_URL. Rode a suíte completa com
// `go test -p 1 ./...`.
func testDB(t *testing.T) (alvo, legado *sql.DB) {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL não definido — suba o banco (docker compose up -d db) para rodar os testes de integração")
	}

	alvo, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("falha ao abrir conexão alvo: %v", err)
	}
	t.Cleanup(func() { alvo.Close() })
	if err := alvo.Ping(); err != nil {
		t.Fatalf("banco indisponível em %s: %v", dsn, err)
	}

	migrateOnce.Do(func() {
		// go test ./... roda pacotes em paralelo por padrão: este pacote e
		// outros (cmd/seed-admin, backend) podem migrar a mesma base "do zero"
		// ao mesmo tempo. golang-migrate só serializa via advisory lock depois
		// que schema_migrations existe — a primeira criação concorrente dela
		// pode colidir. Retry curto absorve a corrida sem exigir `-p 1`.
		for attempt := 1; attempt <= 5; attempt++ {
			var m *migrate.Migrate
			m, migrateErr = migrate.New("file://../../migrations", dsn)
			if migrateErr == nil {
				migrateErr = m.Up()
				m.Close()
			}
			if migrateErr == nil || errors.Is(migrateErr, migrate.ErrNoChange) {
				migrateErr = nil
				return
			}
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}
	})
	if migrateErr != nil {
		t.Fatalf("falha ao aplicar migrations: %v", migrateErr)
	}

	if _, err := alvo.Exec(`CREATE SCHEMA IF NOT EXISTS legado`); err != nil {
		t.Fatalf("falha ao criar schema legado: %v", err)
	}
	if _, err := alvo.Exec(`CREATE TABLE IF NOT EXISTS legado.estoques (id text primary key, nome text)`); err != nil {
		t.Fatalf("falha ao criar legado.estoques: %v", err)
	}
	// O espelho legado tem `nome` como string única, mas não garante NOT NULL
	// (addendum §F não afirma isso) — e o cenário "nome nulo" da pré-validação
	// precisa poder inserir NULL aqui. Idempotente: no-op se já for nullable,
	// inclusive contra uma tabela criada por uma versão anterior deste teste.
	if _, err := alvo.Exec(`ALTER TABLE legado.estoques ALTER COLUMN nome DROP NOT NULL`); err != nil {
		t.Fatalf("falha ao relaxar NOT NULL em legado.estoques.nome: %v", err)
	}

	// legado.produtos — Story 3.7 (spec-3-7). Fixture com todas as colunas
	// documentadas no addendum §F: `id` textual (doc id do Firestore),
	// `estoques` como map nome->quantidade (jsonb), e as 5 dimensões reais
	// mais `lateral` como texto livre (o schema-alvo não tem par
	// lateral_valor/lateral_unidade — precedente da Story 3.1).
	if _, err := alvo.Exec(`CREATE TABLE IF NOT EXISTS legado.produtos (
		id text primary key,
		nome text,
		codigo text,
		categoria text,
		comprimento text,
		largura text,
		diametro text,
		altura text,
		espessura text,
		"lateral" text,
		obs text,
		foto text,
		estoques jsonb,
		criado_em timestamptz
	)`); err != nil {
		t.Fatalf("falha ao criar legado.produtos: %v", err)
	}

	// legado.historico — Story 5.4 (spec-5-4). Fixture com as colunas
	// documentadas no addendum §F, coleção `historico`: `id` textual (doc id
	// do Firestore), `produto` (nome desnormalizado), `tipo` (baixa|
	// transferencia), `origem`/`destino` (string; `destino:'—'` para baixa),
	// `qtd` como TEXTO (validado com ParseFloat, molde de migrarProdutos),
	// `unidade`/`obs` (não migram) e `timestamp` (palavra reservada — precisa
	// de aspas, igual a `"lateral"` em legado.produtos).
	if _, err := alvo.Exec(`CREATE TABLE IF NOT EXISTS legado.historico (
		id text primary key,
		produto text,
		tipo text,
		origem text,
		destino text,
		qtd text,
		unidade text,
		obs text,
		"timestamp" timestamptz
	)`); err != nil {
		t.Fatalf("falha ao criar legado.historico: %v", err)
	}

	// Garante o usuário sintético "Migração do sistema legado" (seed da
	// migration 000022) — autor NOT NULL de toda Movimentação migrada. Outras
	// suítes (services/handlers/middleware) fazem `TRUNCATE usuarios CASCADE`,
	// então a linha semeada por m.Up() pode não sobreviver entre invocações
	// separadas de `go test`. Este INSERT é idempotente (ON CONFLICT DO
	// NOTHING) e NUNCA apaga nada — `usuarios` continua sem `DELETE`/`TRUNCATE`
	// nesta suíte.
	if _, err := alvo.Exec(`
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
		VALUES ('Migração do sistema legado', $1, NULL, 'almoxarife', false, false)
		ON CONFLICT (lower(email)) DO NOTHING`, emailUsuarioMigracaoLegado); err != nil {
		t.Fatalf("falha ao garantir o usuário sintético de migração: %v", err)
	}

	legado, err = sql.Open("postgres", comSearchPath(dsn, "legado"))
	if err != nil {
		t.Fatalf("falha ao abrir conexão legado: %v", err)
	}
	t.Cleanup(func() { legado.Close() })
	if err := legado.Ping(); err != nil {
		t.Fatalf("conexão legado indisponível: %v", err)
	}

	limparTabelas(t, alvo)

	return alvo, legado
}

func limparTabelas(t *testing.T, alvo *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		// legado.produtos e produto_estoque/produtos ANTES de
		// legado.estoques/migracao_id_map/estoques: produto_estoque.produto_id
		// referencia produtos(id) SEM CASCADE (migration 000012) — precisa ser
		// esvaziada antes de um DELETE FROM produtos não esbarrar na FK.
		// categorias/nomenclatura_templates/usuarios NUNCA são limpas aqui —
		// seed compartilhado com outras suítes (spec-3-7 / spec-5-4,
		// Boundaries). O usuário sintético "Migração do sistema legado" é seed
		// da migration 000022.
		//
		// movimentacoes ANTES de produtos/estoques: FK sem ON DELETE CASCADE
		// (migration 000021). legado.historico junto das demais tabelas
		// legadas.
		`DELETE FROM legado.produtos`,
		`DELETE FROM legado.estoques`,
		`DELETE FROM legado.historico`,
		`DELETE FROM movimentacoes`,
		`DELETE FROM produto_estoque`,
		`DELETE FROM migracao_id_map`,
		`DELETE FROM produtos`,
		`DELETE FROM estoques`,
	} {
		if _, err := alvo.Exec(stmt); err != nil {
			t.Fatalf("falha ao limpar estado entre testes (%s): %v", stmt, err)
		}
	}
}

// comSearchPath acrescenta o parâmetro search_path ao DSN. Cobre tanto o
// formato URL (postgres://...) quanto o formato keyword/value (host=... ).
func comSearchPath(dsn, schema string) string {
	if strings.Contains(dsn, "://") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		return dsn + sep + "search_path=" + schema
	}
	return dsn + " search_path=" + schema
}

func inserirLegado(t *testing.T, alvo *sql.DB, id, nome string) {
	t.Helper()
	if _, err := alvo.Exec(`INSERT INTO legado.estoques (id, nome) VALUES ($1, $2)`, id, nome); err != nil {
		t.Fatalf("falha ao inserir linha legada (%s): %v", id, err)
	}
}

func inserirLegadoNulo(t *testing.T, alvo *sql.DB, id string) {
	t.Helper()
	if _, err := alvo.Exec(`INSERT INTO legado.estoques (id, nome) VALUES ($1, NULL)`, id); err != nil {
		t.Fatalf("falha ao inserir linha legada nula (%s): %v", id, err)
	}
}

func contar(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("falha ao contar (%s): %v", query, err)
	}
	return n
}

// pareceUUIDv4 checa comprimento, hifens nas posições canônicas, o dígito de
// versão 4 (s[14]) e o nibble de variante RFC 4122 (s[19] ∈ {8,9,a,b}) —
// suficiente para provar que o id novo veio de gen_random_uuid().
func pareceUUIDv4(s string) bool {
	if len(s) != 36 {
		return false
	}
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	if s[14] != '4' {
		return false
	}
	switch s[19] {
	case '8', '9', 'a', 'b', 'A', 'B':
		return true
	default:
		return false
	}
}

// TestMigrarEstoques_CorteInicial — cenário "Corte inicial" da I/O Matrix.
func TestMigrarEstoques_CorteInicial(t *testing.T) {
	alvo, legado := testDB(t)

	nomes := map[string]string{
		"doc-1": "Canteiro Central",
		"doc-2": "Almoxarifado Norte",
		"doc-3": "Depósito 7",
	}
	for id, nome := range nomes {
		inserirLegado(t, alvo, id, nome)
	}

	res, err := migrarEstoques(alvo, legado, true)
	if err != nil {
		t.Fatalf("migrarEstoques retornou erro inesperado: %v", err)
	}
	if res.Migrados != 3 || res.JaMigrados != 0 || len(res.Conflitos) != 0 || len(res.ColisoesAlvo) != 0 || len(res.NomesInvalidos) != 0 {
		t.Fatalf("resultado = %+v, want Migrados=3 e todas as listas vazias", res)
	}

	if got := contar(t, alvo, `SELECT count(*) FROM estoques`); got != 3 {
		t.Errorf("count(estoques) = %d, want 3", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map WHERE entidade = 'estoque'`); got != 3 {
		t.Errorf("count(migracao_id_map estoque) = %d, want 3", got)
	}

	for idLegado, nome := range nomes {
		var idNovo string
		if err := alvo.QueryRow(
			`SELECT id_novo FROM migracao_id_map WHERE entidade = 'estoque' AND id_legado = $1`, idLegado,
		).Scan(&idNovo); err != nil {
			t.Fatalf("sem linha no mapa para id_legado=%s: %v", idLegado, err)
		}
		if !pareceUUIDv4(idNovo) {
			t.Errorf("id_novo %q não parece UUID v4", idNovo)
		}
		var nomeAlvo string
		if err := alvo.QueryRow(`SELECT nome FROM estoques WHERE id = $1`, idNovo).Scan(&nomeAlvo); err != nil {
			t.Fatalf("id_novo %s não corresponde a nenhuma linha em estoques: %v", idNovo, err)
		}
		if nomeAlvo != nome {
			t.Errorf("nome migrado = %q, want %q (preservado)", nomeAlvo, nome)
		}
	}
}

// TestMigrarEstoques_Idempotente — cenário "Reexecução (idempotência)": todas
// as linhas já mapeadas na 2ª execução.
func TestMigrarEstoques_Idempotente(t *testing.T) {
	alvo, legado := testDB(t)

	inserirLegado(t, alvo, "doc-1", "Canteiro A")
	inserirLegado(t, alvo, "doc-2", "Canteiro B")

	if _, err := migrarEstoques(alvo, legado, true); err != nil {
		t.Fatalf("primeira execução falhou: %v", err)
	}

	res, err := migrarEstoques(alvo, legado, true)
	if err != nil {
		t.Fatalf("segunda execução retornou erro: %v", err)
	}
	if res.Migrados != 0 || res.JaMigrados != 2 {
		t.Fatalf("resultado 2ª execução = %+v, want Migrados=0 JaMigrados=2", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM estoques`); got != 2 {
		t.Errorf("count(estoques) = %d, want 2 — reexecução não pode duplicar", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map`); got != 2 {
		t.Errorf("count(migracao_id_map) = %d, want 2 — reexecução não pode duplicar", got)
	}
}

// TestMigrarEstoques_ExecutarMapaMisto — uma única execução --executar em que
// parte das linhas legadas já está mapeada e parte é nova. Distinto de
// _Idempotente (tudo mapeado) e de _DryRunContabilizaJaMigrados (dry-run).
func TestMigrarEstoques_ExecutarMapaMisto(t *testing.T) {
	alvo, legado := testDB(t)

	inserirLegado(t, alvo, "doc-1", "Canteiro A")
	if _, err := migrarEstoques(alvo, legado, true); err != nil {
		t.Fatalf("corte inicial falhou: %v", err)
	}

	inserirLegado(t, alvo, "doc-2", "Canteiro B")
	res, err := migrarEstoques(alvo, legado, true)
	if err != nil {
		t.Fatalf("segunda execução retornou erro: %v", err)
	}
	if res.Migrados != 1 || res.JaMigrados != 1 {
		t.Fatalf("resultado = %+v, want Migrados=1 JaMigrados=1", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM estoques`); got != 2 {
		t.Errorf("count(estoques) = %d, want 2", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map`); got != 2 {
		t.Errorf("count(migracao_id_map) = %d, want 2", got)
	}
}

// TestMigrarEstoques_NomesEquivalentes — cenário "Nomes legados equivalentes":
// aborta com erro, nada escrito, tanto em --executar quanto em dry-run.
func TestMigrarEstoques_NomesEquivalentes(t *testing.T) {
	for _, executar := range []bool{true, false} {
		t.Run(map[bool]string{true: "executar", false: "dry-run"}[executar], func(t *testing.T) {
			alvo, legado := testDB(t)

			inserirLegado(t, alvo, "doc-1", "Canteiro A")
			inserirLegado(t, alvo, "doc-2", "canteiro  a")
			inserirLegado(t, alvo, "doc-3", "Depósito Único")

			res, err := migrarEstoques(alvo, legado, executar)
			if err == nil {
				t.Fatalf("migrarEstoques deveria abortar com erro; res=%+v", res)
			}
			if len(res.Conflitos) != 1 {
				t.Fatalf("len(Conflitos) = %d, want 1", len(res.Conflitos))
			}
			c := res.Conflitos[0]
			if c.Norm != "canteiro a" {
				t.Errorf("Norm = %q, want %q", c.Norm, "canteiro a")
			}
			if len(c.IDs) != 2 || len(c.Nomes) != 2 {
				t.Fatalf("IDs=%v Nomes=%v, want 2 de cada", c.IDs, c.Nomes)
			}
			if c.IDs[0] != "doc-1" || c.IDs[1] != "doc-2" {
				t.Errorf("IDs = %v, want [doc-1 doc-2]", c.IDs)
			}
			if c.Nomes[0] != "Canteiro A" || c.Nomes[1] != "canteiro  a" {
				t.Errorf("Nomes = %v, want [\"Canteiro A\" \"canteiro  a\"]", c.Nomes)
			}

			if got := contar(t, alvo, `SELECT count(*) FROM estoques`); got != 0 {
				t.Errorf("count(estoques) = %d, want 0 — nada pode ser escrito em conflito", got)
			}
			if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map`); got != 0 {
				t.Errorf("count(migracao_id_map) = %d, want 0 — nada pode ser escrito em conflito", got)
			}
		})
	}
}

// TestMigrarEstoques_NomesInvalidos — pré-validação em bloco: nome nulo, vazio
// após normalização e acima de 255 runes. Classe de erro à parte de Conflitos;
// aborta nos dois modos, nada escrito.
func TestMigrarEstoques_NomesInvalidos(t *testing.T) {
	for _, executar := range []bool{true, false} {
		t.Run(map[bool]string{true: "executar", false: "dry-run"}[executar], func(t *testing.T) {
			alvo, legado := testDB(t)

			inserirLegadoNulo(t, alvo, "doc-nulo")
			inserirLegado(t, alvo, "doc-vazio", "   ")
			inserirLegado(t, alvo, "doc-longo", strings.Repeat("x", 256))
			inserirLegado(t, alvo, "doc-ok", "Canteiro Válido")

			res, err := migrarEstoques(alvo, legado, executar)
			if err == nil {
				t.Fatalf("migrarEstoques deveria abortar; res=%+v", res)
			}
			if len(res.Conflitos) != 0 || len(res.ColisoesAlvo) != 0 {
				t.Errorf("nome inválido não pode virar Conflito/ColisaoAlvo: %+v", res)
			}
			motivos := map[string]string{}
			for _, n := range res.NomesInvalidos {
				motivos[n.IDLegado] = n.Motivo
			}
			for _, id := range []string{"doc-nulo", "doc-vazio", "doc-longo"} {
				if _, ok := motivos[id]; !ok {
					t.Errorf("NomesInvalidos não menciona %s: %+v", id, res.NomesInvalidos)
				}
			}
			if _, ok := motivos["doc-ok"]; ok {
				t.Errorf("doc-ok não deveria estar em NomesInvalidos")
			}
			if got := contar(t, alvo, `SELECT count(*) FROM estoques`); got != 0 {
				t.Errorf("count(estoques) = %d, want 0 — nada escrito", got)
			}
			if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map`); got != 0 {
				t.Errorf("count(migracao_id_map) = %d, want 0 — nada escrito", got)
			}
		})
	}
}

// TestMigrarEstoques_DryRun — cenário "Dry-run": relata mas não escreve.
func TestMigrarEstoques_DryRun(t *testing.T) {
	alvo, legado := testDB(t)

	inserirLegado(t, alvo, "doc-1", "Canteiro A")
	inserirLegado(t, alvo, "doc-2", "Canteiro B")
	inserirLegado(t, alvo, "doc-3", "Canteiro C")

	res, err := migrarEstoques(alvo, legado, false)
	if err != nil {
		t.Fatalf("dry-run retornou erro: %v", err)
	}
	if res.Migrados != 3 || res.JaMigrados != 0 || len(res.Conflitos) != 0 {
		t.Fatalf("resultado = %+v, want Migrados=3 JaMigrados=0 Conflitos=0", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM estoques`); got != 0 {
		t.Errorf("count(estoques) = %d, want 0 — dry-run não escreve", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map`); got != 0 {
		t.Errorf("count(migracao_id_map) = %d, want 0 — dry-run não escreve", got)
	}
}

// TestMigrarEstoques_DryRunContabilizaJaMigrados — dry-run após um corte real
// reconhece as linhas já mapeadas (JaMigrados), sem escrever, e sem acusar
// falsa colisão com o alvo para a linha já migrada.
func TestMigrarEstoques_DryRunContabilizaJaMigrados(t *testing.T) {
	alvo, legado := testDB(t)

	inserirLegado(t, alvo, "doc-1", "Canteiro A")
	if _, err := migrarEstoques(alvo, legado, true); err != nil {
		t.Fatalf("corte inicial falhou: %v", err)
	}
	inserirLegado(t, alvo, "doc-2", "Canteiro B")

	res, err := migrarEstoques(alvo, legado, false)
	if err != nil {
		t.Fatalf("dry-run retornou erro: %v", err)
	}
	if res.Migrados != 1 || res.JaMigrados != 1 {
		t.Fatalf("resultado = %+v, want Migrados=1 JaMigrados=1", res)
	}
	if len(res.ColisoesAlvo) != 0 {
		t.Errorf("linha já migrada não pode ser acusada de colisão com o alvo: %+v", res.ColisoesAlvo)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM estoques`); got != 1 {
		t.Errorf("count(estoques) = %d, want 1 — dry-run não escreve a linha nova", got)
	}
}

// TestMigrarEstoques_ColisaoComAlvo — cenário "Colisão com o alvo": uma linha
// legada colide com um Estoque criado manualmente no alvo (fora do mapa). A
// pré-checagem em bloco detecta a colisão ANTES da transação, nos dois modos,
// lista todas e não escreve nada; o 23505 no INSERT fica só como backstop.
func TestMigrarEstoques_ColisaoComAlvo(t *testing.T) {
	for _, executar := range []bool{true, false} {
		t.Run(map[bool]string{true: "executar", false: "dry-run"}[executar], func(t *testing.T) {
			alvo, legado := testDB(t)

			// Estoque pré-existente no alvo, criado "pela tela", sem linha no mapa.
			if _, err := alvo.Exec(`INSERT INTO estoques (nome) VALUES ($1)`, "Canteiro A"); err != nil {
				t.Fatalf("falha ao pré-criar estoque no alvo: %v", err)
			}

			// doc-a (nome novo, ordenado antes) e doc-b (colide com "Canteiro A").
			inserirLegado(t, alvo, "doc-a", "Canteiro B")
			inserirLegado(t, alvo, "doc-b", "Canteiro A")

			res, err := migrarEstoques(alvo, legado, executar)
			if err == nil {
				t.Fatalf("migrarEstoques deveria falhar por colisão; res=%+v", res)
			}
			if len(res.ColisoesAlvo) != 1 {
				t.Fatalf("len(ColisoesAlvo) = %d, want 1 (%+v)", len(res.ColisoesAlvo), res.ColisoesAlvo)
			}
			c := res.ColisoesAlvo[0]
			if c.IDLegado != "doc-b" || c.Nome != "Canteiro A" || c.Norm != "canteiro a" {
				t.Errorf("ColisaoAlvo = %+v, want {doc-b, \"Canteiro A\", \"canteiro a\"}", c)
			}

			if got := contar(t, alvo, `SELECT count(*) FROM estoques`); got != 1 {
				t.Errorf("count(estoques) = %d, want 1 — só o pré-existente; doc-a não pode ser escrito", got)
			}
			if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map`); got != 0 {
				t.Errorf("count(migracao_id_map) = %d, want 0 — nenhuma linha parcial", got)
			}
		})
	}
}

// TestMigrarEstoques_LegadoVazio — cenário "Legado vazio".
func TestMigrarEstoques_LegadoVazio(t *testing.T) {
	alvo, legado := testDB(t)

	res, err := migrarEstoques(alvo, legado, true)
	if err != nil {
		t.Fatalf("migrarEstoques retornou erro para legado vazio: %v", err)
	}
	if res.Migrados != 0 || res.JaMigrados != 0 || len(res.Conflitos) != 0 {
		t.Fatalf("resultado = %+v, want tudo zero", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM estoques`); got != 0 {
		t.Errorf("count(estoques) = %d, want 0", got)
	}
}

// TestMain_Processo exercita main() (que chama os.Exit) num subprocesso — o
// idioma padrão do Go: a própria função é reinvocada com
// MIGRATE_LEGADO_SUBPROC=1, reconstrói os.Args a partir de SUBPROC_ARGS e
// chama main(); o processo pai verifica exit code e saída. Cobre: env var
// obrigatória ausente, alvo == legado, argumento posicional, conflito de nomes
// equivalentes (aborta) e os dois caminhos de sucesso (--executar e dry-run).
func TestMain_Processo(t *testing.T) {
	if os.Getenv("MIGRATE_LEGADO_SUBPROC") == "1" {
		os.Args = append([]string{"migrate-legado"}, strings.Fields(os.Getenv("SUBPROC_ARGS"))...)
		main()
		return
	}

	runChild := func(t *testing.T, extraEnv map[string]string) (string, int) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestMain_Processo$")
		env := append(os.Environ(), "MIGRATE_LEGADO_SUBPROC=1")
		for k, v := range extraEnv {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err == nil {
			return string(out), 0
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("falha ao executar subprocesso: %v\nsaída=%s", err, out)
		}
		return string(out), exitErr.ExitCode()
	}

	// --- casos que não precisam de banco ---

	t.Run("DATABASE_URL ausente", func(t *testing.T) {
		out, code := runChild(t, map[string]string{"DATABASE_URL": "", "LEGADO_DATABASE_URL": "postgres://x"})
		if code != 1 {
			t.Fatalf("exit code = %d, want 1; saída=%s", code, out)
		}
		if !strings.Contains(out, "DATABASE_URL não definido") {
			t.Errorf("saída = %q, quer conter %q", out, "DATABASE_URL não definido")
		}
	})

	t.Run("LEGADO_DATABASE_URL ausente", func(t *testing.T) {
		out, code := runChild(t, map[string]string{"DATABASE_URL": "postgres://x", "LEGADO_DATABASE_URL": ""})
		if code != 1 {
			t.Fatalf("exit code = %d, want 1; saída=%s", code, out)
		}
		if !strings.Contains(out, "LEGADO_DATABASE_URL não definido") {
			t.Errorf("saída = %q, quer conter %q", out, "LEGADO_DATABASE_URL não definido")
		}
	})

	t.Run("alvo igual a legado", func(t *testing.T) {
		out, code := runChild(t, map[string]string{
			"DATABASE_URL":        "postgres://mesmo",
			"LEGADO_DATABASE_URL": "postgres://mesmo",
		})
		if code != 1 {
			t.Fatalf("exit code = %d, want 1; saída=%s", code, out)
		}
		if !strings.Contains(out, "mesmo banco") {
			t.Errorf("saída = %q, quer conter %q", out, "mesmo banco")
		}
	})

	t.Run("argumento posicional", func(t *testing.T) {
		out, code := runChild(t, map[string]string{"SUBPROC_ARGS": "executar"})
		if code != 1 {
			t.Fatalf("exit code = %d, want 1; saída=%s", code, out)
		}
		if !strings.Contains(out, "argumento inesperado") {
			t.Errorf("saída = %q, quer conter %q", out, "argumento inesperado")
		}
	})

	// --- casos que exercem main() ponta-a-ponta contra o banco ---

	t.Run("conflito de nomes equivalentes aborta", func(t *testing.T) {
		alvo, _ := testDB(t)
		dsn := os.Getenv("DATABASE_URL")
		inserirLegado(t, alvo, "doc-1", "Canteiro A")
		inserirLegado(t, alvo, "doc-2", "canteiro  a")

		out, code := runChild(t, map[string]string{
			"DATABASE_URL":        dsn,
			"LEGADO_DATABASE_URL": comSearchPath(dsn, "legado"),
			"SUBPROC_ARGS":        "--executar",
		})
		if code != 1 {
			t.Fatalf("exit code = %d, want 1; saída=%s", code, out)
		}
		if !strings.Contains(out, "canteiro a") || !strings.Contains(out, "doc-1") || !strings.Contains(out, "doc-2") {
			t.Errorf("saída não lista o conflito completo (norm + os dois ids): %q", out)
		}
		if n := contar(t, alvo, `SELECT count(*) FROM estoques`); n != 0 {
			t.Errorf("count(estoques) = %d, want 0", n)
		}
		if n := contar(t, alvo, `SELECT count(*) FROM migracao_id_map`); n != 0 {
			t.Errorf("count(migracao_id_map) = %d, want 0", n)
		}
	})

	t.Run("sucesso --executar", func(t *testing.T) {
		alvo, _ := testDB(t)
		dsn := os.Getenv("DATABASE_URL")
		inserirLegado(t, alvo, "doc-1", "Canteiro A")
		inserirLegado(t, alvo, "doc-2", "Canteiro B")

		out, code := runChild(t, map[string]string{
			"DATABASE_URL":        dsn,
			"LEGADO_DATABASE_URL": comSearchPath(dsn, "legado"),
			"SUBPROC_ARGS":        "--executar",
		})
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; saída=%s", code, out)
		}
		if !strings.Contains(out, "corte aplicado: Migrados=") {
			t.Errorf("saída sem a linha de sucesso: %q", out)
		}
		if n := contar(t, alvo, `SELECT count(*) FROM estoques`); n != 2 {
			t.Errorf("count(estoques) = %d, want 2", n)
		}
		if n := contar(t, alvo, `SELECT count(*) FROM migracao_id_map WHERE entidade='estoque'`); n != 2 {
			t.Errorf("count(migracao_id_map estoque) = %d, want 2", n)
		}
	})

	t.Run("sucesso dry-run", func(t *testing.T) {
		alvo, _ := testDB(t)
		dsn := os.Getenv("DATABASE_URL")
		inserirLegado(t, alvo, "doc-1", "Canteiro A")
		inserirLegado(t, alvo, "doc-2", "Canteiro B")

		out, code := runChild(t, map[string]string{
			"DATABASE_URL":        dsn,
			"LEGADO_DATABASE_URL": comSearchPath(dsn, "legado"),
			"SUBPROC_ARGS":        "",
		})
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; saída=%s", code, out)
		}
		if !strings.Contains(out, "dry-run (nada escrito): migraria ") {
			t.Errorf("saída sem a linha de dry-run: %q", out)
		}
		if n := contar(t, alvo, `SELECT count(*) FROM estoques`); n != 0 {
			t.Errorf("count(estoques) = %d, want 0 — dry-run não escreve", n)
		}
	})

	// Story 5.4: main() chama migrarMovimentacoes após migrarProdutos. Uma
	// linha de historico irresolúvel vira pendência no stderr e NÃO aborta o
	// corte (exit 0).
	t.Run("movimentações: pendência no stderr sem abortar", func(t *testing.T) {
		alvo, _ := testDB(t)
		dsn := os.Getenv("DATABASE_URL")
		inserirLegado(t, alvo, "e1", "Almox Central")
		// historico referencia um produto que não existe em legado.produtos.
		if _, err := alvo.Exec(`INSERT INTO legado.historico (id, produto, tipo, origem, destino, qtd)
			VALUES ('h1', 'Produto Fantasma', 'baixa', 'Almox Central', '—', '3')`); err != nil {
			t.Fatalf("falha ao inserir historico: %v", err)
		}

		out, code := runChild(t, map[string]string{
			"DATABASE_URL":        dsn,
			"LEGADO_DATABASE_URL": comSearchPath(dsn, "legado"),
			"SUBPROC_ARGS":        "--executar",
		})
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; saída=%s", code, out)
		}
		if !strings.Contains(out, "corte aplicado (movimentações): Migrados=0") {
			t.Errorf("saída sem a linha de sucesso de movimentações: %q", out)
		}
		if !strings.Contains(out, "produto não encontrado no legado") || !strings.Contains(out, "h1") {
			t.Errorf("saída não lista a pendência de movimentação: %q", out)
		}
		if n := contar(t, alvo, `SELECT count(*) FROM movimentacoes`); n != 0 {
			t.Errorf("count(movimentacoes) = %d, want 0", n)
		}
	})

	// Story 5.4: seed da migration 000022 ausente => main() aborta ANTES de
	// migrarEstoques/migrarProdutos (que commitam), com mensagem acurada e
	// nada escrito.
	t.Run("seed 000022 ausente aborta antes de qualquer escrita", func(t *testing.T) {
		alvo, _ := testDB(t)
		dsn := os.Getenv("DATABASE_URL")

		if _, err := alvo.Exec(`DELETE FROM usuarios WHERE lower(email) = lower($1)`, emailUsuarioMigracaoLegado); err != nil {
			t.Fatalf("falha ao remover o usuário sintético: %v", err)
		}
		t.Cleanup(func() {
			alvo.Exec(`
				INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
				VALUES ('Migração do sistema legado', $1, NULL, 'almoxarife', false, false)
				ON CONFLICT (lower(email)) DO NOTHING`, emailUsuarioMigracaoLegado)
		})

		inserirLegado(t, alvo, "e1", "Almox Central")

		out, code := runChild(t, map[string]string{
			"DATABASE_URL":        dsn,
			"LEGADO_DATABASE_URL": comSearchPath(dsn, "legado"),
			"SUBPROC_ARGS":        "--executar",
		})
		if code == 0 {
			t.Fatalf("exit code = 0, want != 0; saída=%s", out)
		}
		if !strings.Contains(out, "migration 000022") {
			t.Errorf("saída não cita a migration 000022: %q", out)
		}
		if n := contar(t, alvo, `SELECT count(*) FROM estoques`); n != 0 {
			t.Errorf("count(estoques) = %d, want 0 — abortou antes de migrarEstoques", n)
		}
		if n := contar(t, alvo, `SELECT count(*) FROM movimentacoes`); n != 0 {
			t.Errorf("count(movimentacoes) = %d, want 0", n)
		}
	})
}
