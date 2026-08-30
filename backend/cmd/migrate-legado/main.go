// Command migrate-legado executa o corte "big-bang" único que traz os Estoques
// do sistema legado (protótipo Firestore, hoje espelhado num PostgreSQL local
// mantido pela empresa — addendum §F) para o schema novo do stockflow.
//
// Isto é deliberadamente um binário standalone one-off — nunca uma rota HTTP,
// nunca um handler registrado no servidor da API, nunca um cron nem uma chamada
// a partir do runtime da aplicação. O corte é sempre disparado manualmente por
// uma pessoa (AD-15, PRD §9): o agente não tem — nem deve ter — acesso ao
// espelho Postgres real da empresa. Molde: cmd/seed-admin/main.go.
//
// Sem a flag --executar o binário roda em dry-run: conecta, carrega as linhas
// legadas, roda todas as pré-checagens (nomes inválidos, nomes equivalentes,
// colisão com o banco alvo) e apenas relata o que faria, sem escrever nada. Com
// --executar aplica a migração numa única transação no banco alvo; se qualquer
// pré-checagem falhar, nada é escrito mesmo assim.
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"unicode/utf8"

	"github.com/joho/godotenv"
	"github.com/lib/pq"
)

// pqUniqueViolation é o SQLSTATE do Postgres para violação de restrição de
// unicidade (23505) — usado como BACKSTOP para reconhecer uma colisão do nome
// legado com uma linha que já existe em estoques no banco alvo, fora do mapa
// (índice único idx_estoques_nome_normalizado, migration 000008), caso ela
// apareça só no INSERT (ex.: linha criada por outra sessão entre a
// pré-checagem e a transação). services.pqUniqueViolation não é exportada e
// não cruza limite de pacote, por isso a constante é redeclarada aqui.
const pqUniqueViolation = "23505"

// normExpr é a expressão de normalização de nome copiada VERBATIM da coluna
// gerada nome_normalizado / índice idx_estoques_nome_normalizado (migration
// 000008). Toda normalização de nome nesta ferramenta é feita pelo próprio
// Postgres com esta string — nunca reimplementada em Go (o \s do Postgres !=
// unicode.IsSpace).
const normExpr = `lower(regexp_replace(btrim(nome), '\s+', ' ', 'g'))`

// ConflitoNome descreve um grupo de Estoques legados cujos nomes, depois de
// normalizados (minúsculas + colapso de espaços), colidem entre si.
type ConflitoNome struct {
	Norm  string
	IDs   []string
	Nomes []string
}

// ColisaoAlvo descreve um Estoque legado (ainda não migrado) cujo nome
// normalizado já existe em estoques no banco alvo, fora do mapa de migração.
type ColisaoAlvo struct {
	IDLegado string
	Nome     string
	Norm     string
}

// NomeInvalido descreve um registro de Estoque legado com nome que nem sequer
// poderia ser inserido: nulo, vazio após normalização, ou acima de 255 runes.
type NomeInvalido struct {
	IDLegado string
	Motivo   string
}

// ResultadoMigracao é o relatório de uma execução de migrarEstoques. Qualquer
// uma das listas de problema não-vazia significa "corte abortado, nada
// escrito" e vem acompanhada de um error não-nil.
type ResultadoMigracao struct {
	Migrados       int
	JaMigrados     int
	Conflitos      []ConflitoNome
	ColisoesAlvo   []ColisaoAlvo
	NomesInvalidos []NomeInvalido
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	executar := flag.Bool("executar", false, "aplica a migração; sem a flag o binário roda em dry-run e não escreve nada")
	flag.Parse()

	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "erro: argumento inesperado %q — use a flag --executar (com os dois hifens)\n", flag.Arg(0))
		os.Exit(1)
	}

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("falha ao carregar .env", "error", err)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "erro: DATABASE_URL não definido")
		os.Exit(1)
	}
	legadoURL := os.Getenv("LEGADO_DATABASE_URL")
	if legadoURL == "" {
		fmt.Fprintln(os.Stderr, "erro: LEGADO_DATABASE_URL não definido — preenchido pelo operador só na hora do corte (ver .env.example)")
		os.Exit(1)
	}
	if databaseURL == legadoURL {
		fmt.Fprintln(os.Stderr, "erro: DATABASE_URL e LEGADO_DATABASE_URL apontam para o mesmo banco — o corte lê do legado e escreve no alvo; use bancos distintos")
		os.Exit(1)
	}

	alvo, err := sql.Open("postgres", databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: falha ao abrir conexão com o banco alvo: %v\n", err)
		os.Exit(1)
	}
	defer alvo.Close()

	legado, err := sql.Open("postgres", legadoURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: falha ao abrir conexão com o banco legado: %v\n", err)
		os.Exit(1)
	}
	defer legado.Close()

	if err := alvo.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "erro: banco alvo indisponível: %v\n", err)
		os.Exit(1)
	}
	if err := legado.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "erro: banco legado indisponível: %v\n", err)
		os.Exit(1)
	}

	res, err := migrarEstoques(alvo, legado, *executar)
	if err != nil {
		switch {
		case len(res.NomesInvalidos) > 0:
			fmt.Fprintln(os.Stderr, "erro: Estoques legados com nome inválido — corte abortado, nada foi escrito.")
			fmt.Fprintln(os.Stderr, "Corrija os registros abaixo no banco legado antes de rodar de novo:")
			for _, n := range res.NomesInvalidos {
				fmt.Fprintf(os.Stderr, "    id_legado=%s  motivo: %s\n", n.IDLegado, n.Motivo)
			}
		case len(res.Conflitos) > 0:
			fmt.Fprintln(os.Stderr, "erro: Estoques legados com nomes equivalentes por caixa/espaço — corte abortado, nada foi escrito.")
			fmt.Fprintln(os.Stderr, "Revise manualmente e unifique os registros abaixo no banco legado antes de rodar de novo:")
			for _, c := range res.Conflitos {
				fmt.Fprintf(os.Stderr, "  nome normalizado: %q\n", c.Norm)
				for i := range c.IDs {
					fmt.Fprintf(os.Stderr, "    id_legado=%s  nome=%q\n", c.IDs[i], c.Nomes[i])
				}
			}
		case len(res.ColisoesAlvo) > 0:
			fmt.Fprintln(os.Stderr, "erro: nomes de Estoques legados que já existem em estoques no banco alvo, fora do mapa (criados manualmente antes do corte?) — corte abortado, nada foi escrito.")
			fmt.Fprintln(os.Stderr, "Resolva cada caso (renomeie ou remova a linha no alvo, ou mapeie-a manualmente) antes de rodar de novo:")
			for _, c := range res.ColisoesAlvo {
				fmt.Fprintf(os.Stderr, "    id_legado=%s  nome=%q  (normalizado: %q)\n", c.IDLegado, c.Nome, c.Norm)
			}
		default:
			fmt.Fprintf(os.Stderr, "erro: %v (transação revertida, nada foi escrito)\n", err)
		}
		os.Exit(1)
	}

	if *executar {
		fmt.Fprintf(os.Stdout, "corte aplicado: Migrados=%d, JaMigrados=%d\n", res.Migrados, res.JaMigrados)
	} else {
		fmt.Fprintf(os.Stdout, "dry-run (nada escrito): migraria %d, já migrados %d, conflitos %d\n",
			res.Migrados, res.JaMigrados, len(res.Conflitos))
	}
}

// migrarEstoques é o ponto testável do corte. `alvo` é o pool para o schema
// novo (DATABASE_URL); `legado` é o pool para o espelho do Firestore
// (LEGADO_DATABASE_URL). Passos:
//
//  1. SELECT id, nome, <norm> FROM estoques no legado (colunas conforme
//     addendum §F: `id` textual = doc id do Firestore, `nome`). O nome
//     normalizado vem calculado pelo PRÓPRIO Postgres (normExpr).
//  2. Pré-validação de cada nome legado (nome nulo / vazio após normalização /
//     acima de 255 runes) -> NomesInvalidos; qualquer um aborta, nada escrito.
//  3. Pré-checagem de nomes equivalentes ENTRE SI no legado, com a mesma
//     expressão do índice 000008, executada pelo Postgres -> Conflitos;
//     qualquer grupo aborta, nada escrito.
//  4. Pré-checagem de colisão com o banco ALVO (nos dois modos, em bloco):
//     para as linhas legadas ainda não mapeadas, um nome cujo
//     nome_normalizado já existe em estoques -> ColisoesAlvo; qualquer uma
//     aborta ANTES de abrir a transação, nada escrito.
//  5. executar == false: calcula Migrados/JaMigrados só com o SELECT no mapa —
//     sem INSERT e sem transação de escrita.
//  6. executar == true: abre UMA transação no alvo; para cada linha legada,
//     consulta migracao_id_map (entidade='estoque', id_legado) — achou ->
//     JaMigrados++ e segue; não achou -> INSERT INTO estoques (nome)
//     RETURNING id (o UUID v4 vem do DEFAULT gen_random_uuid()), depois INSERT
//     na migracao_id_map, e Migrados++. Commit no fim.
//
// O 23505 no INSERT INTO estoques continua tratado como BACKSTOP: se uma linha
// colidente escapar da pré-checagem (criada por outra sessão no intervalo), a
// transação sofre rollback (via defer) e o erro identifica id_legado + nome —
// nada parcial do lote fica gravado.
func migrarEstoques(alvo, legado *sql.DB, executar bool) (ResultadoMigracao, error) {
	var res ResultadoMigracao

	// 1) Carrega as linhas legadas, com o nome normalizado calculado pelo
	//    próprio Postgres legado (normExpr — mesma string do índice 000008).
	type linhaLegada struct {
		id       string
		nome     string
		nomeNull bool
		norm     string
	}
	rows, err := legado.Query(`SELECT id, nome, ` + normExpr + ` AS norm FROM estoques ORDER BY id`)
	if err != nil {
		return res, fmt.Errorf("falha ao ler estoques do banco legado: %w", err)
	}
	var legados []linhaLegada
	for rows.Next() {
		var id string
		var nome, norm sql.NullString
		if err := rows.Scan(&id, &nome, &norm); err != nil {
			rows.Close()
			return res, fmt.Errorf("falha ao ler linha de estoque legado: %w", err)
		}
		legados = append(legados, linhaLegada{
			id:       id,
			nome:     nome.String,
			nomeNull: !nome.Valid,
			norm:     norm.String,
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return res, fmt.Errorf("falha ao iterar estoques legados: %w", err)
	}
	rows.Close()

	// 2) Pré-validação de cada nome legado, ANTES de qualquer escrita. Classe
	//    de erro à parte de Conflitos (nomes equivalentes) — um nome vazio ou
	//    nulo não entra no grupo de equivalência.
	for _, l := range legados {
		switch {
		case l.nomeNull:
			res.NomesInvalidos = append(res.NomesInvalidos, NomeInvalido{l.id, "nome nulo no registro legado"})
		case l.norm == "":
			res.NomesInvalidos = append(res.NomesInvalidos, NomeInvalido{l.id, "nome vazio após normalização (só espaços)"})
		case utf8.RuneCountInString(l.nome) > 255:
			res.NomesInvalidos = append(res.NomesInvalidos, NomeInvalido{
				l.id,
				fmt.Sprintf("nome com %d caracteres (máximo 255)", utf8.RuneCountInString(l.nome)),
			})
		}
	}
	if len(res.NomesInvalidos) > 0 {
		return res, fmt.Errorf("%d registro(s) de Estoque legado com nome inválido — revisão manual necessária antes do corte", len(res.NomesInvalidos))
	}

	// 3) Pré-checagem de nomes equivalentes entre si no legado. A expressão é
	//    a mesma de normExpr; o Postgres legado é Postgres (addendum §F), então
	//    o resultado é idêntico ao que o índice único idx_estoques_nome_normalizado
	//    imporia.
	preCheck := `
		SELECT ` + normExpr + ` AS norm,
		       array_agg(id ORDER BY id),
		       array_agg(nome ORDER BY id)
		FROM estoques
		GROUP BY 1
		HAVING count(*) > 1`
	confRows, err := legado.Query(preCheck)
	if err != nil {
		return res, fmt.Errorf("falha na pré-checagem de conflito de nomes no banco legado: %w", err)
	}
	for confRows.Next() {
		var c ConflitoNome
		if err := confRows.Scan(&c.Norm, pq.Array(&c.IDs), pq.Array(&c.Nomes)); err != nil {
			confRows.Close()
			return res, fmt.Errorf("falha ao ler grupo de conflito de nomes: %w", err)
		}
		res.Conflitos = append(res.Conflitos, c)
	}
	if err := confRows.Err(); err != nil {
		confRows.Close()
		return res, fmt.Errorf("falha ao iterar grupos de conflito de nomes: %w", err)
	}
	confRows.Close()

	if len(res.Conflitos) > 0 {
		return res, fmt.Errorf("%d grupo(s) de nomes legados equivalentes por caixa/espaço — revisão manual necessária antes do corte", len(res.Conflitos))
	}

	// 4) Pré-checagem de colisão com o banco ALVO — roda em !executar e em
	//    executar. Sem isto, uma linha legada cujo nome_normalizado já existe
	//    em estoques (fora do mapa — ex.: criada manualmente pela tela antes do
	//    corte) só apareceria uma-a-uma via 23505 no meio da transação. Aqui
	//    todas são detectadas de uma vez, no banco, antes de escrever qualquer
	//    coisa.
	mapeados := make(map[string]bool)
	mapRows, err := alvo.Query(`SELECT id_legado FROM migracao_id_map WHERE entidade = 'estoque'`)
	if err != nil {
		return res, fmt.Errorf("falha ao carregar migracao_id_map: %w", err)
	}
	for mapRows.Next() {
		var idLegado string
		if err := mapRows.Scan(&idLegado); err != nil {
			mapRows.Close()
			return res, fmt.Errorf("falha ao ler migracao_id_map: %w", err)
		}
		mapeados[idLegado] = true
	}
	if err := mapRows.Err(); err != nil {
		mapRows.Close()
		return res, fmt.Errorf("falha ao iterar migracao_id_map: %w", err)
	}
	mapRows.Close()

	var normsNaoMapeados []string
	vistos := make(map[string]bool)
	for _, l := range legados {
		if mapeados[l.id] || vistos[l.norm] {
			continue
		}
		vistos[l.norm] = true
		normsNaoMapeados = append(normsNaoMapeados, l.norm)
	}
	if len(normsNaoMapeados) > 0 {
		colideRows, err := alvo.Query(
			`SELECT nome_normalizado FROM estoques WHERE nome_normalizado = ANY($1)`,
			pq.Array(normsNaoMapeados),
		)
		if err != nil {
			return res, fmt.Errorf("falha na pré-checagem de colisão com o banco alvo: %w", err)
		}
		colididos := make(map[string]bool)
		for colideRows.Next() {
			var norm string
			if err := colideRows.Scan(&norm); err != nil {
				colideRows.Close()
				return res, fmt.Errorf("falha ao ler colisão com o banco alvo: %w", err)
			}
			colididos[norm] = true
		}
		if err := colideRows.Err(); err != nil {
			colideRows.Close()
			return res, fmt.Errorf("falha ao iterar colisões com o banco alvo: %w", err)
		}
		colideRows.Close()

		for _, l := range legados {
			if mapeados[l.id] {
				continue
			}
			if colididos[l.norm] {
				res.ColisoesAlvo = append(res.ColisoesAlvo, ColisaoAlvo{IDLegado: l.id, Nome: l.nome, Norm: l.norm})
			}
		}
	}
	if len(res.ColisoesAlvo) > 0 {
		return res, fmt.Errorf("%d nome(s) de Estoque legado já existem no banco alvo fora do mapa — revisão manual necessária antes do corte", len(res.ColisoesAlvo))
	}

	// 5) Dry-run: só o SELECT no mapa, sem transação de escrita.
	if !executar {
		for _, l := range legados {
			var idNovo string
			err := alvo.QueryRow(
				`SELECT id_novo FROM migracao_id_map WHERE entidade = 'estoque' AND id_legado = $1`, l.id,
			).Scan(&idNovo)
			switch {
			case err == nil:
				res.JaMigrados++
			case errors.Is(err, sql.ErrNoRows):
				res.Migrados++
			default:
				return res, fmt.Errorf("falha ao consultar migracao_id_map (dry-run) para id_legado=%s: %w", l.id, err)
			}
		}
		return res, nil
	}

	// 6) Corte real: uma única transação no alvo.
	//
	// O estoques legado não tem `criadoEm` (addendum §F): estoques.criado_em do
	// alvo assume o DEFAULT now() da migration 000008 — comportamento esperado,
	// não perda de dado.
	tx, err := alvo.Begin()
	if err != nil {
		return res, fmt.Errorf("falha ao abrir transação no banco alvo: %w", err)
	}
	// Rollback é no-op depois de um Commit bem-sucedido (devolve
	// sql.ErrTxDone, ignorado). Em qualquer caminho de erro abaixo, o return
	// dispara este defer e nada parcial fica gravado.
	defer tx.Rollback()

	for _, l := range legados {
		var idNovo string
		err := tx.QueryRow(
			`SELECT id_novo FROM migracao_id_map WHERE entidade = 'estoque' AND id_legado = $1`, l.id,
		).Scan(&idNovo)
		if err == nil {
			res.JaMigrados++
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return res, fmt.Errorf("falha ao consultar migracao_id_map para id_legado=%s: %w", l.id, err)
		}

		var novoID string
		err = tx.QueryRow(`INSERT INTO estoques (nome) VALUES ($1) RETURNING id`, l.nome).Scan(&novoID)
		if err != nil {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation {
				return res, fmt.Errorf(
					"colisão de nome com o banco alvo (backstop 23505): id_legado=%s nome=%q já existe em estoques fora do mapa — corte abortado, nada foi escrito",
					l.id, l.nome)
			}
			return res, fmt.Errorf("falha ao inserir estoque para id_legado=%s: %w", l.id, err)
		}

		if _, err := tx.Exec(
			`INSERT INTO migracao_id_map (entidade, id_legado, id_novo) VALUES ('estoque', $1, $2)`,
			l.id, novoID,
		); err != nil {
			return res, fmt.Errorf("falha ao gravar migracao_id_map para id_legado=%s: %w", l.id, err)
		}
		res.Migrados++
	}

	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("falha ao efetivar a transação do corte: %w", err)
	}
	return res, nil
}
