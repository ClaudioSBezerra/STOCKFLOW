package main

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/lib/pq"
)

// Migração do Histórico de Movimentações legado — Story 5.4 (spec-5-4).
// Estende backend/cmd/migrate-legado com migrarMovimentacoes, chamada
// sequencialmente DEPOIS de migrarEstoques e migrarProdutos no mesmo main().
//
// Lê legado.historico (addendum §F, coleção `historico`), resolve o Produto
// pelo nome desnormalizado via legado.produtos -> migracao_id_map
// (entidade='produto'), resolve os Estoques pelo nome normalizado contra
// estoques.nome_normalizado no alvo (populada pela 2.3) e recria cada linha
// como uma linha `movimentacoes`, preservando `tipo`, a quantidade e o
// `timestamp` original (INSERT com `criado_em` explícito).
//
// O historico legado não tem campo de autor: `usuario_id` aponta SEMPRE para
// o usuário sintético "Migração do sistema legado" semeado pela migration
// 000022 — resolvido numa pré-condição de seed ANTES de qualquer leitura
// legada; ausente => error não-nil, nada escrito.
//
// Idempotência pelo PK (entidade, id_legado) de migracao_id_map
// (entidade='movimentacao'). Diferente de migrarProdutos (transação por
// linha), segue o molde de migrarEstoques: UMA única transação para todo o
// lote. Toda linha que não resolve (produto não migrado / não encontrado /
// ambíguo, estoque desconhecido, tipo/quantidade inválidos, origem = destino)
// vai para o relatório efêmero PendentesRevisao e é PULADA, sem interromper o
// lote — só erros inesperados (conexão, historico ilegível, seed ausente,
// backstop 23505) abortam.

// emailUsuarioMigracaoLegado é o e-mail sentinela do usuário sintético
// semeado pela migration 000022 — autor NOT NULL de toda Movimentação
// migrada. Resolvido por lower(email) (índice idx_usuarios_email_lower).
const emailUsuarioMigracaoLegado = "migracao-legado@sistema.stockflow.local"

// errSeedUsuarioMigracaoAusente sinaliza que a migration 000022 não foi
// aplicada no banco alvo (nenhuma linha `usuarios` com o e-mail sentinela).
// É uma pré-condição: retornada ANTES de ler qualquer linha legada e antes
// de qualquer escrita. main() a reconhece com errors.Is para um ramo de
// stderr dedicado.
var errSeedUsuarioMigracaoAusente = errors.New(
	"seed ausente: nenhum usuário com e-mail " + emailUsuarioMigracaoLegado +
		" — aplique a migration 000022 no banco alvo antes do corte de Movimentações")

// PendenteRevisao é uma linha de legado.historico que NÃO pôde ser recriada
// como Movimentação: dados brutos + motivo, para revisão manual do operador.
// Lista efêmera (stderr no fim da execução), recomputada a cada execução —
// nenhuma tabela nova de pendências. Molde de FotoFalha (produtos.go).
type PendenteRevisao struct {
	IDLegado  string
	Produto   string
	Tipo      string
	Origem    string
	Destino   string
	Qtd       string
	Timestamp string
	Motivo    string
}

// ResultadoMigracaoMovimentacoes é o relatório de uma execução de
// migrarMovimentacoes. Diferente de ResultadoMigracao (Estoques) /
// ResultadoMigracaoProdutos, PendentesRevisao NUNCA acompanha um error
// não-nil — é falha branda por linha (molde de FotosComFalha). AvisosData
// lista linhas migradas cujo `timestamp` legado estava ausente (nesse caso
// `criado_em` recebe o momento do corte).
type ResultadoMigracaoMovimentacoes struct {
	Migrados         int
	JaMigrados       int
	PendentesRevisao []PendenteRevisao
	AvisosData       []string
}

// linhaHistoricoLegado é uma linha de legado.historico já carregada, com
// `produto` e origem/destino trimados/normalizados pelo PRÓPRIO Postgres
// legado (btrim / normExpr) — nunca reimplementado em Go (o \s do Postgres
// != unicode.IsSpace).
type linhaHistoricoLegado struct {
	id          string
	produto     sql.NullString
	produtoTrim sql.NullString
	tipo        sql.NullString
	origem      sql.NullString
	destino     sql.NullString
	origemNorm  sql.NullString
	destinoNorm sql.NullString
	qtd         sql.NullString
	timestamp   sql.NullTime
}

// movimentacaoResolvida são os campos de uma linha `movimentacoes` prontos
// para o INSERT, depois que migrarMovimentacoes resolveu Produto, Estoques,
// tipo e quantidade de uma linha legada.
type movimentacaoResolvida struct {
	produtoID        string
	tipo             string
	estoqueOrigemID  sql.NullString
	estoqueDestinoID sql.NullString
	quantidade       float64
	criadoEm         sql.NullTime
}

// migrarMovimentacoes é o ponto testável da migração do Histórico legado.
// `alvo` é o pool para o schema novo (DATABASE_URL); `legado` é o pool para o
// espelho do Firestore (LEGADO_DATABASE_URL).
//
// Passos, na ordem:
//
//   - Pré-condição de seed: resolve o id do usuário sintético da migration
//     000022 por lower(email). Ausente => errSeedUsuarioMigracaoAusente, nada
//     lido nem escrito.
//   - Carrega legado.historico (falha aqui = "historico ilegível", aborta).
//   - Carga dos três mapas de resolução, TODA fora de transação: legado.produtos
//     (btrim(nome) -> ids legados, lista p/ detectar nome ambíguo);
//     migracao_id_map entidade='produto' (id_legado -> produto_id novo);
//     estoques do alvo (nome_normalizado -> id).
//   - executar == false (dry-run): para cada linha, consulta o mapa
//     entidade='movimentacao' — no mapa => JaMigrados++; senão resolve — falha
//     => PendentesRevisao, ok => Migrados++. Sem transação, sem INSERT.
//   - executar == true: UMA transação no alvo. Para cada linha: no mapa =>
//     JaMigrados++, pula; não resolve => PendentesRevisao, pula; resolve =>
//     INSERT INTO movimentacoes (com criado_em explícito) + INSERT na
//     migracao_id_map, Migrados++. Commit no fim.
//
// O 23505 no INSERT na migracao_id_map é BACKSTOP (linha (movimentacao,
// id_legado) criada por outra sessão entre a checagem e a transação): a
// transação sofre rollback via defer e o erro identifica id_legado — nada
// parcial do lote fica gravado.
func migrarMovimentacoes(alvo, legado *sql.DB, executar bool) (ResultadoMigracaoMovimentacoes, error) {
	var res ResultadoMigracaoMovimentacoes

	// 1) Pré-condição de seed — ANTES de ler qualquer linha legada. O usuário
	//    sintético é o autor NOT NULL de TODA Movimentação migrada; sem ele,
	//    nada pode ser escrito e nem faz sentido continuar.
	var usuarioMigracaoID string
	err := alvo.QueryRow(
		`SELECT id FROM usuarios WHERE lower(email) = lower($1)`, emailUsuarioMigracaoLegado,
	).Scan(&usuarioMigracaoID)
	if errors.Is(err, sql.ErrNoRows) {
		return res, errSeedUsuarioMigracaoAusente
	}
	if err != nil {
		return res, fmt.Errorf("falha ao resolver o usuário sintético de migração: %w", err)
	}

	// 2) Carrega legado.historico. Falha aqui (tabela ausente / estrutura
	//    divergente) aborta o corte — "historico ilegível". `produto` é
	//    trimado e origem/destino são normalizados pela MESMA expressão de
	//    estoques.nome_normalizado (migration 000008), tudo calculado pelo
	//    Postgres legado.
	rows, err := legado.Query(`
		SELECT id, produto, btrim(produto) AS produto_trim, tipo,
		       origem, destino,
		       lower(regexp_replace(btrim(origem), '\s+', ' ', 'g'))  AS origem_norm,
		       lower(regexp_replace(btrim(destino), '\s+', ' ', 'g')) AS destino_norm,
		       qtd, "timestamp"
		FROM historico
		ORDER BY id`)
	if err != nil {
		return res, fmt.Errorf("falha ao ler historico do banco legado: %w", err)
	}
	var historico []linhaHistoricoLegado
	for rows.Next() {
		var l linhaHistoricoLegado
		if err := rows.Scan(
			&l.id, &l.produto, &l.produtoTrim, &l.tipo,
			&l.origem, &l.destino, &l.origemNorm, &l.destinoNorm,
			&l.qtd, &l.timestamp,
		); err != nil {
			rows.Close()
			return res, fmt.Errorf("falha ao ler linha de historico legado: %w", err)
		}
		historico = append(historico, l)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return res, fmt.Errorf("falha ao iterar historico legado: %w", err)
	}
	rows.Close()

	// 3a) legado.produtos: btrim(nome) -> ids legados. Lista (não valor
	//     único) para distinguir "não encontrado" (0), "ambíguo" (>1) e
	//     "encontrado" (1). btrim nos dois lados, sem case-fold (mesma
	//     decisão de `codigo` na 3.7 — nomes podem diferir só por caixa).
	produtosPorNome := make(map[string][]string)
	prodRows, err := legado.Query(`SELECT id, btrim(nome) AS nome_trim FROM produtos WHERE nome IS NOT NULL`)
	if err != nil {
		return res, fmt.Errorf("falha ao ler produtos do banco legado: %w", err)
	}
	for prodRows.Next() {
		var id, nomeTrim string
		if err := prodRows.Scan(&id, &nomeTrim); err != nil {
			prodRows.Close()
			return res, fmt.Errorf("falha ao ler linha de produto legado: %w", err)
		}
		produtosPorNome[nomeTrim] = append(produtosPorNome[nomeTrim], id)
	}
	if err := prodRows.Err(); err != nil {
		prodRows.Close()
		return res, fmt.Errorf("falha ao iterar produtos legados: %w", err)
	}
	prodRows.Close()

	// 3b) migracao_id_map (entidade='produto'): id_legado -> produto_id novo.
	produtoIDNovoPorLegado := make(map[string]string)
	mapProdRows, err := alvo.Query(`SELECT id_legado, id_novo FROM migracao_id_map WHERE entidade = 'produto'`)
	if err != nil {
		return res, fmt.Errorf("falha ao carregar migracao_id_map (produto): %w", err)
	}
	for mapProdRows.Next() {
		var idLegado, idNovo string
		if err := mapProdRows.Scan(&idLegado, &idNovo); err != nil {
			mapProdRows.Close()
			return res, fmt.Errorf("falha ao ler migracao_id_map (produto): %w", err)
		}
		produtoIDNovoPorLegado[idLegado] = idNovo
	}
	if err := mapProdRows.Err(); err != nil {
		mapProdRows.Close()
		return res, fmt.Errorf("falha ao iterar migracao_id_map (produto): %w", err)
	}
	mapProdRows.Close()

	// 3c) estoques do alvo: nome_normalizado -> id (populado pela 2.3).
	estoqueIDPorNorm := make(map[string]string)
	estRows, err := alvo.Query(`SELECT nome_normalizado, id FROM estoques`)
	if err != nil {
		return res, fmt.Errorf("falha ao carregar estoques do banco alvo: %w", err)
	}
	for estRows.Next() {
		var norm, id string
		if err := estRows.Scan(&norm, &id); err != nil {
			estRows.Close()
			return res, fmt.Errorf("falha ao ler estoque do banco alvo: %w", err)
		}
		estoqueIDPorNorm[norm] = id
	}
	if err := estRows.Err(); err != nil {
		estRows.Close()
		return res, fmt.Errorf("falha ao iterar estoques do banco alvo: %w", err)
	}
	estRows.Close()

	// resolver traduz UMA linha legada nos campos de `movimentacoes`, ou
	// devolve um motivo de pendência (string não-vazia). Uma pendência por
	// linha — devolve o PRIMEIRO problema encontrado, na ordem Produto ->
	// tipo -> quantidade -> Estoques -> origem = destino.
	resolver := func(l linhaHistoricoLegado) (movimentacaoResolvida, string) {
		var r movimentacaoResolvida

		// Produto — caminho obrigatório pela tabela de mapeamento (AC).
		nomeTrim := ""
		if l.produtoTrim.Valid {
			nomeTrim = l.produtoTrim.String
		}
		ids := produtosPorNome[nomeTrim]
		switch {
		case len(ids) == 0:
			return r, "produto não encontrado no legado"
		case len(ids) > 1:
			return r, "nome de produto ambíguo no legado"
		}
		produtoID, ok := produtoIDNovoPorLegado[ids[0]]
		if !ok {
			return r, "produto não migrado"
		}
		r.produtoID = produtoID

		// tipo — verbatim, só `baixa` e `transferencia` do legado.
		tipo := l.tipo.String
		if !l.tipo.Valid || (tipo != "baixa" && tipo != "transferencia") {
			return r, fmt.Sprintf("tipo inválido: '%s'", tipo)
		}
		r.tipo = tipo

		// quantidade — texto -> ParseFloat; nulo / não-numérico / não-finito
		// / fora do que NUMERIC(10,3) CHECK (quantidade > 0) aceita é
		// pendência. ParseFloat aceita "NaN"/"Inf"/"Infinity" com erro nil, e
		// `NaN > 0` / `Inf > 0` passariam o CHECK do Postgres gravando lixo na
		// trilha; um valor que arredonda para <= 0 (ex. "0.0004") ou estoura a
		// precisão abortaria a transação única do lote por causa de UMA linha.
		// Tudo isso vira pendência, sem interromper os demais.
		if !l.qtd.Valid {
			return r, "quantidade inválida: '<nulo>'"
		}
		q, errq := strconv.ParseFloat(l.qtd.String, 64)
		switch {
		case errq != nil, math.IsNaN(q), math.IsInf(q, 0), q < 0.001, q > 9999999.999:
			return r, fmt.Sprintf("quantidade inválida: '%s'", l.qtd.String)
		}
		r.quantidade = q

		// Estoque de origem — obrigatório nos dois tipos. Distingue "a linha
		// legada não tem origem" de "a origem não casa nenhum Estoque".
		origemNorm := ""
		if l.origemNorm.Valid {
			origemNorm = l.origemNorm.String
		}
		if origemNorm == "" {
			return r, "origem ausente no historico"
		}
		origemID, origemOK := estoqueIDPorNorm[origemNorm]
		if !origemOK {
			return r, fmt.Sprintf("estoque '%s' não encontrado", l.origem.String)
		}
		r.estoqueOrigemID = sql.NullString{String: origemID, Valid: true}

		switch tipo {
		case "transferencia":
			// Origem E destino têm de resolver.
			destinoNorm := ""
			if l.destinoNorm.Valid {
				destinoNorm = l.destinoNorm.String
			}
			if destinoNorm == "" {
				return r, "destino ausente no historico"
			}
			destinoID, destinoOK := estoqueIDPorNorm[destinoNorm]
			if !destinoOK {
				return r, fmt.Sprintf("estoque '%s' não encontrado", l.destino.String)
			}
			if destinoID == origemID {
				return r, "origem igual ao destino"
			}
			r.estoqueDestinoID = sql.NullString{String: destinoID, Valid: true}
		default: // "baixa"
			// O modelo-alvo não tem destino para baixa (migration 000021:
			// "baixa, estoque_destino_id sempre NULL"). O `destino` legado
			// ('—' / vazio / nulo / qualquer texto) é descartado — nunca é
			// pendência.
		}

		// criado_em: `timestamp` legado quando presente; ausente => NULL
		// aqui (o INSERT usa COALESCE(..., now())) e a linha entra em
		// AvisosData no resumo.
		r.criadoEm = l.timestamp
		return r, ""
	}

	// 4) Dry-run: nada escrito, só conta (checagem no mapa + resolução).
	if !executar {
		for _, l := range historico {
			var idNovo string
			errMap := alvo.QueryRow(
				`SELECT id_novo FROM migracao_id_map WHERE entidade = 'movimentacao' AND id_legado = $1`, l.id,
			).Scan(&idNovo)
			switch {
			case errMap == nil:
				res.JaMigrados++
				continue
			case !errors.Is(errMap, sql.ErrNoRows):
				return res, fmt.Errorf("falha ao consultar migracao_id_map (dry-run) para id_legado=%s: %w", l.id, errMap)
			}
			r, motivo := resolver(l)
			if motivo != "" {
				res.PendentesRevisao = append(res.PendentesRevisao, pendenteDeHistorico(l, motivo))
				continue
			}
			if !r.criadoEm.Valid {
				res.AvisosData = append(res.AvisosData, avisoTimestampAusente(l.id))
			}
			res.Migrados++
		}
		return res, nil
	}

	// 5) Corte real: UMA transação para todo o lote (molde migrarEstoques).
	//    O script roda fora do runtime da aplicação, então nenhum evento SSE
	//    no canal `movimentacoes` é publicado no corte — o cliente rebusca
	//    via GET quando a app voltar.
	tx, err := alvo.Begin()
	if err != nil {
		return res, fmt.Errorf("falha ao abrir transação no banco alvo: %w", err)
	}
	// Rollback é no-op depois de um Commit bem-sucedido (sql.ErrTxDone,
	// ignorado). Em qualquer return de erro abaixo, o defer dispara e nada
	// parcial do lote fica gravado.
	defer tx.Rollback()

	for _, l := range historico {
		var idNovo string
		errMap := tx.QueryRow(
			`SELECT id_novo FROM migracao_id_map WHERE entidade = 'movimentacao' AND id_legado = $1`, l.id,
		).Scan(&idNovo)
		if errMap == nil {
			res.JaMigrados++
			continue
		}
		if !errors.Is(errMap, sql.ErrNoRows) {
			return res, fmt.Errorf("falha ao consultar migracao_id_map para id_legado=%s: %w", l.id, errMap)
		}

		r, motivo := resolver(l)
		if motivo != "" {
			res.PendentesRevisao = append(res.PendentesRevisao, pendenteDeHistorico(l, motivo))
			continue
		}

		var movID string
		err := tx.QueryRow(`
			INSERT INTO movimentacoes (
				produto_id, tipo, estoque_origem_id, estoque_destino_id,
				quantidade, usuario_id, criado_em
			) VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7, now()))
			RETURNING id`,
			r.produtoID, r.tipo, r.estoqueOrigemID, r.estoqueDestinoID,
			r.quantidade, usuarioMigracaoID, r.criadoEm,
		).Scan(&movID)
		if err != nil {
			return res, fmt.Errorf("falha ao inserir movimentacao para id_legado=%s: %w", l.id, err)
		}

		if _, err := tx.Exec(
			`INSERT INTO migracao_id_map (entidade, id_legado, id_novo) VALUES ('movimentacao', $1, $2)`,
			l.id, movID,
		); err != nil {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation {
				return res, fmt.Errorf(
					"colisão em migracao_id_map (backstop 23505): id_legado=%s já mapeado para uma movimentacao fora deste lote — corte abortado, nada foi escrito",
					l.id)
			}
			return res, fmt.Errorf("falha ao gravar migracao_id_map para id_legado=%s: %w", l.id, err)
		}

		if !r.criadoEm.Valid {
			res.AvisosData = append(res.AvisosData, avisoTimestampAusente(l.id))
		}
		res.Migrados++
	}

	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("falha ao efetivar a transação do corte de Movimentações: %w", err)
	}
	return res, nil
}

// pendenteDeHistorico monta um item de PendentesRevisao a partir da linha
// legada bruta + o motivo. `timestamp` nulo vira string vazia.
func pendenteDeHistorico(l linhaHistoricoLegado, motivo string) PendenteRevisao {
	ts := ""
	if l.timestamp.Valid {
		ts = l.timestamp.Time.Format(time.RFC3339)
	}
	return PendenteRevisao{
		IDLegado:  l.id,
		Produto:   l.produto.String,
		Tipo:      l.tipo.String,
		Origem:    l.origem.String,
		Destino:   l.destino.String,
		Qtd:       l.qtd.String,
		Timestamp: ts,
		Motivo:    motivo,
	}
}

// avisoTimestampAusente é a linha de AvisosData de uma Movimentação migrada
// cujo `timestamp` legado estava ausente — `criado_em` recebeu o momento do
// corte. Recomputada tanto no dry-run quanto no corte real (mesma string).
func avisoTimestampAusente(idLegado string) string {
	return fmt.Sprintf(
		"id_legado=%s: timestamp ausente no historico — criado_em definido como o momento do corte", idLegado)
}
