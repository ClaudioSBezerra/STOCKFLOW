package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/lib/pq"
)

// Migração de Pedidos de Retirada legados — Story 7.7 (spec-7-7). Estende
// backend/cmd/migrate-legado com migrarPedidos, chamada sequencialmente
// DEPOIS de migrarMovimentacoes no mesmo main() (o vínculo Movimentação↔
// Pedido precisa das Movimentações já em migracao_id_map).
//
// Lê legado.pedidos (addendum §F, coleção `pedidos`), recria cada Pedido
// com seus itens referenciando os novos produto_id (via migracao_id_map
// entidade='produto') e estoque_id (via estoques.nome_normalizado no alvo),
// preservando `status`, `criado_em` e — para Pedidos decididos — a data da
// decisão. Depois de recriar um Pedido, faz UPDATE movimentacoes SET
// pedido_id nas Movimentações já migradas cujas linhas de legado.historico
// referenciam aquele Pedido legado (campo `pedido`).
//
// legado.pedidos não tem autor confiável mapeável: `usuario_id` é resolvido
// por lower(email) contra usuarios; sem match usa o usuário sintético
// "Migração do sistema legado" (seed da migration 000022) — mesma
// pré-condição de seed de migrarMovimentacoes. `uid` (auth id do Firestore)
// não é lido: sem mapeamento no schema novo.
//
// Idempotência pelo PK (entidade, id_legado) de migracao_id_map
// (entidade='pedido'). UMA única transação para todo o lote (molde
// migrarEstoques/migrarMovimentacoes). Diferente de migrarMovimentacoes, a
// falha de resolução é POR PEDIDO, não por item: um Pedido gravado sem um de
// seus itens misrepresentaria o que foi solicitado (AC #1). Qualquer item
// irresolúvel (produto fora do mapa, estoque desconhecido, nome/categoria
// ausente, quantidade inválida) ou `status` fora de {pendente, aprovado,
// rejeitado} joga o Pedido INTEIRO para o relatório efêmero PendentesRevisao
// e ele é pulado, sem interromper o lote — só erros inesperados (conexão,
// legado.pedidos ilegível, seed ausente, backstop 23505) abortam.

// PendenteRevisaoPedido é um Pedido de legado.pedidos que NÃO pôde ser
// recriado por inteiro: dados-chave + motivo, para revisão manual do
// operador. Lista efêmera (stderr no fim da execução), recomputada a cada
// execução — nenhuma tabela nova de pendências.
type PendenteRevisaoPedido struct {
	IDLegado    string
	Solicitante string
	Status      string
	QtdItens    int
	Motivo      string
}

// ResultadoMigracaoPedidos é o relatório de uma execução de migrarPedidos.
// PendentesRevisao NUNCA acompanha um error não-nil — é falha branda por
// Pedido. AvisosData lista Pedidos migrados com ressalva de dado (autor não
// resolvido por e-mail, `criadoEm` ausente, movimentações do histórico não
// migradas e portanto não vinculadas).
type ResultadoMigracaoPedidos struct {
	Migrados         int
	JaMigrados       int
	PendentesRevisao []PendenteRevisaoPedido
	AvisosData       []string
}

// itemLegadoPedido é um elemento do array `itens` de legado.pedidos
// (addendum §F: `{prodId, nome, unidade, estoque, qtd, categoria}`).
// `unidade` NÃO migra — o modelo-alvo pedido_itens não tem essa coluna
// (precedente 5.4 com obs/unidade). `qtd` é lida como RawMessage e validada
// como texto (mesma guarda endurecida de migrarMovimentacoes): o legado
// pode gravar número ou string.
type itemLegadoPedido struct {
	ProdID    string          `json:"prodId"`
	Nome      string          `json:"nome"`
	Unidade   string          `json:"unidade"`
	Estoque   string          `json:"estoque"`
	Qtd       json.RawMessage `json:"qtd"`
	Categoria string          `json:"categoria"`
}

// pedidoLegado é uma linha de legado.pedidos já carregada, com o array
// `itens` já desserializado (itensErr guarda uma falha de desserialização,
// tratada como pendência do Pedido, não como erro que aborta o lote).
type pedidoLegado struct {
	id           string
	solicitante  sql.NullString
	obra         sql.NullString
	obs          sql.NullString
	email        sql.NullString
	status       sql.NullString
	criadoEm     sql.NullTime
	atualizadoEm sql.NullTime
	itensRaw     []byte
	itens        []itemLegadoPedido
	itensErr     error
}

// itemResolvido são os campos de uma linha pedido_itens prontos para o
// INSERT.
type itemResolvido struct {
	produtoID          string
	produtoNome        string
	categoriaNome      string
	estoqueID          string
	estoqueNome        string
	quantidade         float64
	quantidadeAprovada sql.NullFloat64
}

// pedidoResolvido são os campos de uma linha `pedidos` + seus itens prontos
// para o INSERT, depois que migrarPedidos resolveu autor, itens, status e
// data da decisão de um Pedido legado.
type pedidoResolvido struct {
	usuarioID      string
	autorResolvido bool
	solicitante    string
	obra           string
	observacao     sql.NullString
	status         string
	criadoEm       sql.NullTime
	decididoPor    sql.NullString
	decididoEm     sql.NullTime
	itens          []itemResolvido
}

// insertPedido grava `criado_em` explícito (COALESCE com now() de fallback)
// e resolve `decidido_em` no próprio SQL: NULL enquanto `pendente`,
// COALESCE(atualizadoEm|criadoEm, now()) depois de decidido — mantendo a
// preservação de timestamp fora do Go (molde migrarMovimentacoes).
const insertPedido = `
	INSERT INTO pedidos (
		usuario_id, solicitante, obra_centro_custo, observacao, status,
		criado_em, decidido_por, decidido_em
	) VALUES (
		$1, $2, $3, $4, $5,
		COALESCE($6, now()), $7,
		CASE WHEN $5 = 'pendente' THEN NULL ELSE COALESCE($8, now()) END
	)
	RETURNING id`

const insertPedidoItem = `
	INSERT INTO pedido_itens (
		pedido_id, produto_id, produto_nome, categoria_nome,
		estoque_id, estoque_nome, quantidade, quantidade_aprovada
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

// migrarPedidos é o ponto testável da migração de Pedidos legados. `alvo` é
// o pool para o schema novo (DATABASE_URL); `legado` é o pool para o espelho
// do Firestore (LEGADO_DATABASE_URL).
//
// Passos, na ordem (molde migrarMovimentacoes):
//
//   - Pré-condição de seed: resolve o id do usuário sintético da migration
//     000022 por lower(email). Ausente => errSeedUsuarioMigracaoAusente,
//     nada lido nem escrito.
//   - Carrega legado.pedidos + desserializa cada array `itens` (falha ao ler
//     a tabela = "legado.pedidos ilegível", aborta; falha ao desserializar
//     um `itens` = pendência daquele Pedido).
//   - Carga dos mapas de resolução, TODA fora de transação: migracao_id_map
//     entidade='produto' (id_legado -> produto_id novo); estoques do alvo
//     (nome_normalizado -> id); usuarios (lower(email) -> id);
//     migracao_id_map entidade='movimentacao' (id_legado -> id_novo);
//     legado.historico (pedido -> ids de historico); normalização
//     Postgres-side dos nomes de estoque citados nos itens.
//   - executar == false (dry-run): para cada Pedido, consulta o mapa
//     entidade='pedido' — no mapa => JaMigrados++; senão resolve — falha =>
//     PendentesRevisao, ok => Migrados++ (+ AvisosData). Sem transação, sem
//     escrita.
//   - executar == true: UMA transação no alvo. Para cada Pedido: no mapa =>
//     JaMigrados++, pula (inclusive o UPDATE de vínculo, já feito na 1ª
//     passada); não resolve => PendentesRevisao, pula; resolve => INSERT
//     INTO pedidos + INSERT INTO pedido_itens + INSERT na migracao_id_map +
//     UPDATE movimentacoes SET pedido_id, Migrados++. Commit no fim.
//
// O 23505 no INSERT na migracao_id_map é BACKSTOP (linha (pedido, id_legado)
// criada por outra sessão entre a checagem e a transação): a transação sofre
// rollback via defer e o erro identifica id_legado — nada parcial fica
// gravado.
func migrarPedidos(alvo, legado *sql.DB, executar bool) (ResultadoMigracaoPedidos, error) {
	var res ResultadoMigracaoPedidos

	// 1) Pré-condição de seed — ANTES de ler qualquer linha legada. É o
	//    fallback de autor e o `decidido_por` de todo Pedido migrado já
	//    decidido; sem ele nada pode ser escrito.
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

	// 2) Carrega legado.pedidos. Falha aqui (tabela ausente / estrutura
	//    divergente) aborta o corte — "legado.pedidos ilegível". `uid` não é
	//    lido: auth id do Firestore sem mapeamento no schema novo.
	rows, err := legado.Query(`
		SELECT id, solicitante, obra, obs, email, itens, status, criado_em, atualizado_em
		FROM pedidos
		ORDER BY id`)
	if err != nil {
		return res, fmt.Errorf("falha ao ler pedidos do banco legado: %w", err)
	}
	var pedidos []pedidoLegado
	for rows.Next() {
		var p pedidoLegado
		if err := rows.Scan(
			&p.id, &p.solicitante, &p.obra, &p.obs, &p.email,
			&p.itensRaw, &p.status, &p.criadoEm, &p.atualizadoEm,
		); err != nil {
			rows.Close()
			return res, fmt.Errorf("falha ao ler linha de pedido legado: %w", err)
		}
		if len(p.itensRaw) > 0 {
			if err := json.Unmarshal(p.itensRaw, &p.itens); err != nil {
				p.itensErr = err
			}
		}
		pedidos = append(pedidos, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return res, fmt.Errorf("falha ao iterar pedidos legados: %w", err)
	}
	rows.Close()

	// 3a) migracao_id_map (entidade='produto'): id_legado -> produto_id novo.
	produtoIDNovoPorLegado, err := carregarMapaMigracao(alvo, "produto")
	if err != nil {
		return res, err
	}

	// 3b) migracao_id_map (entidade='movimentacao'): id_legado -> id_novo,
	//     para o vínculo Movimentação↔Pedido.
	movIDNovoPorLegado, err := carregarMapaMigracao(alvo, "movimentacao")
	if err != nil {
		return res, err
	}

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

	// 3d) usuarios do alvo: lower(email) -> id (índice único
	//     idx_usuarios_email_lower — lower(email) é único).
	usuarioIDPorEmail := make(map[string]string)
	usrRows, err := alvo.Query(`SELECT lower(email), id FROM usuarios WHERE email IS NOT NULL`)
	if err != nil {
		return res, fmt.Errorf("falha ao carregar usuarios do banco alvo: %w", err)
	}
	for usrRows.Next() {
		var email, id string
		if err := usrRows.Scan(&email, &id); err != nil {
			usrRows.Close()
			return res, fmt.Errorf("falha ao ler usuario do banco alvo: %w", err)
		}
		usuarioIDPorEmail[email] = id
	}
	if err := usrRows.Err(); err != nil {
		usrRows.Close()
		return res, fmt.Errorf("falha ao iterar usuarios do banco alvo: %w", err)
	}
	usrRows.Close()

	// 3e) legado.historico: pedido -> ids de historico que o referenciam.
	//     A confirmação do nome real desse campo no espelho é operator_action
	//     (spec Design Notes) — a fixture de teste cria legado.historico.pedido.
	historicoPorPedidoLegado := make(map[string][]string)
	histRows, err := legado.Query(`SELECT id, pedido FROM historico WHERE pedido IS NOT NULL`)
	if err != nil {
		return res, fmt.Errorf("falha ao ler o vínculo pedido<-historico do banco legado: %w", err)
	}
	for histRows.Next() {
		var histID, pedidoLegadoID string
		if err := histRows.Scan(&histID, &pedidoLegadoID); err != nil {
			histRows.Close()
			return res, fmt.Errorf("falha ao ler linha de vínculo historico legado: %w", err)
		}
		historicoPorPedidoLegado[pedidoLegadoID] = append(historicoPorPedidoLegado[pedidoLegadoID], histID)
	}
	if err := histRows.Err(); err != nil {
		histRows.Close()
		return res, fmt.Errorf("falha ao iterar o vínculo pedido<-historico legado: %w", err)
	}
	histRows.Close()

	// 3f) normalização Postgres-side dos nomes de estoque citados nos itens —
	//     a MESMA expressão de estoques.nome_normalizado / normExpr (migration
	//     000008), NUNCA reimplementada em Go (o \s do Postgres != unicode.IsSpace).
	var nomesEstoque []string
	vistoNomeEstoque := make(map[string]bool)
	for _, p := range pedidos {
		for _, it := range p.itens {
			if !vistoNomeEstoque[it.Estoque] {
				vistoNomeEstoque[it.Estoque] = true
				nomesEstoque = append(nomesEstoque, it.Estoque)
			}
		}
	}
	normPorNomeEstoque := make(map[string]string)
	if len(nomesEstoque) > 0 {
		normRows, err := alvo.Query(
			`SELECT nome, `+normExpr+` AS norm FROM unnest($1::text[]) AS x(nome)`,
			pq.Array(nomesEstoque),
		)
		if err != nil {
			return res, fmt.Errorf("falha ao normalizar nomes de estoque dos itens: %w", err)
		}
		for normRows.Next() {
			var nome, norm string
			if err := normRows.Scan(&nome, &norm); err != nil {
				normRows.Close()
				return res, fmt.Errorf("falha ao ler normalização de nome de estoque: %w", err)
			}
			normPorNomeEstoque[nome] = norm
		}
		if err := normRows.Err(); err != nil {
			normRows.Close()
			return res, fmt.Errorf("falha ao iterar normalização de nomes de estoque: %w", err)
		}
		normRows.Close()
	}

	// resolverPedido traduz UM Pedido legado nos campos de `pedidos` +
	// `pedido_itens`, ou devolve um motivo de pendência (string não-vazia).
	// Falha é POR PEDIDO: o PRIMEIRO problema encontrado (status -> itens)
	// joga o Pedido inteiro para PendentesRevisao.
	resolverPedido := func(p pedidoLegado) (pedidoResolvido, string) {
		var r pedidoResolvido

		st := ""
		if p.status.Valid {
			st = p.status.String
		}
		if st != "pendente" && st != "aprovado" && st != "rejeitado" {
			return r, fmt.Sprintf("status inválido: '%s'", st)
		}
		r.status = st

		if p.itensErr != nil {
			return r, fmt.Sprintf("itens ilegível: %v", p.itensErr)
		}
		if len(p.itens) == 0 {
			return r, "pedido sem itens"
		}

		// Itens: resolução + soma-e-descarta na colisão do par
		// (produto_id, estoque_id) dentro do MESMO Pedido (PK-alvo
		// (pedido_id, produto_id, estoque_id) — molde MesclarDuplicatas).
		type chaveItem struct{ prod, est string }
		ordem := make([]chaveItem, 0, len(p.itens))
		acc := make(map[chaveItem]*itemResolvido, len(p.itens))
		for _, it := range p.itens {
			produtoID, ok := produtoIDNovoPorLegado[strings.TrimSpace(it.ProdID)]
			if !ok {
				return r, fmt.Sprintf("item com produto não migrado: prodId=%s", it.ProdID)
			}
			norm := normPorNomeEstoque[it.Estoque]
			estoqueID, ok := estoqueIDPorNorm[norm]
			if !ok {
				return r, fmt.Sprintf("item com estoque '%s' não encontrado", it.Estoque)
			}
			if strings.TrimSpace(it.Nome) == "" {
				return r, "item sem nome"
			}
			if strings.TrimSpace(it.Categoria) == "" {
				return r, "item sem categoria"
			}
			qtxt := textoQuantidade(it.Qtd)
			q, errq := strconv.ParseFloat(qtxt, 64)
			if errq != nil || math.IsNaN(q) || math.IsInf(q, 0) || q < 0.001 || q > 9999999.999 {
				return r, fmt.Sprintf("quantidade inválida: '%s'", qtxt)
			}

			k := chaveItem{produtoID, estoqueID}
			if existente, dup := acc[k]; dup {
				existente.quantidade += q
				continue
			}
			acc[k] = &itemResolvido{
				produtoID:     produtoID,
				produtoNome:   it.Nome,
				categoriaNome: it.Categoria,
				estoqueID:     estoqueID,
				estoqueNome:   it.Estoque,
				quantidade:    q,
			}
			ordem = append(ordem, k)
		}
		for _, k := range ordem {
			item := acc[k]
			// A quantidade SOMADA na colisão do par (produto_id, estoque_id)
			// tem de respeitar a MESMA guarda endurecida de cada item: sem
			// isto, dois itens válidos cuja soma estoura NUMERIC(10,3)
			// CHECK (quantidade > 0) fariam o INSERT falhar e abortar o LOTE
			// inteiro — mas o contrato é "só erros inesperados abortam";
			// dado legado ruim vira pendência do Pedido.
			if s := item.quantidade; math.IsNaN(s) || math.IsInf(s, 0) || s < 0.001 || s > 9999999.999 {
				return r, fmt.Sprintf("quantidade somada fora da faixa: '%s'", strconv.FormatFloat(s, 'f', -1, 64))
			}
			switch r.status {
			case "aprovado":
				item.quantidadeAprovada = sql.NullFloat64{Float64: item.quantidade, Valid: true}
			case "rejeitado":
				item.quantidadeAprovada = sql.NullFloat64{Float64: 0, Valid: true}
			default: // pendente
				item.quantidadeAprovada = sql.NullFloat64{}
			}
			r.itens = append(r.itens, *item)
		}

		if p.solicitante.Valid {
			r.solicitante = p.solicitante.String
		}
		if p.obra.Valid {
			r.obra = p.obra.String
		}
		r.observacao = p.obs
		r.criadoEm = p.criadoEm

		// Autor: lower(email) contra usuarios; sem match (ou e-mail
		// nulo/vazio) usa o usuário sintético e vira AvisosData. Nunca é
		// pendência — o Pedido continua rastreável pelo texto livre
		// `solicitante`.
		r.usuarioID = usuarioMigracaoID
		if p.email.Valid && strings.TrimSpace(p.email.String) != "" {
			if uid, ok := usuarioIDPorEmail[strings.ToLower(p.email.String)]; ok {
				r.usuarioID = uid
				r.autorResolvido = true
			}
		}

		// Decisão: para aprovado/rejeitado, decidido_por = usuário sintético
		// (MontarReciboPedidoConteudo faz JOIN INNER em decidido_por — Story
		// 7.6) e decidido_em = COALESCE(atualizadoEm, criadoEm, now()); para
		// pendente, ambos NULL.
		if r.status == "aprovado" || r.status == "rejeitado" {
			r.decididoPor = sql.NullString{String: usuarioMigracaoID, Valid: true}
			switch {
			case p.atualizadoEm.Valid:
				r.decididoEm = p.atualizadoEm
			case p.criadoEm.Valid:
				r.decididoEm = p.criadoEm
			}
		}

		return r, ""
	}

	// 4) Dry-run: nada escrito, só conta (checagem no mapa + resolução +
	//    AvisosData).
	if !executar {
		for _, p := range pedidos {
			var idNovo string
			errMap := alvo.QueryRow(
				`SELECT id_novo FROM migracao_id_map WHERE entidade = 'pedido' AND id_legado = $1`, p.id,
			).Scan(&idNovo)
			switch {
			case errMap == nil:
				res.JaMigrados++
				continue
			case !errors.Is(errMap, sql.ErrNoRows):
				return res, fmt.Errorf("falha ao consultar migracao_id_map (dry-run) para id_legado=%s: %w", p.id, errMap)
			}

			r, motivo := resolverPedido(p)
			if motivo != "" {
				res.PendentesRevisao = append(res.PendentesRevisao, pendenteDePedido(p, motivo))
				continue
			}
			registrarAvisosPedido(&res, p, r, historicoPorPedidoLegado, movIDNovoPorLegado)
			res.Migrados++
		}
		return res, nil
	}

	// 5) Corte real: UMA transação para todo o lote (molde migrarEstoques).
	//    O script roda fora do runtime da aplicação, então nenhum evento SSE
	//    no canal `pedidos` é publicado no corte.
	tx, err := alvo.Begin()
	if err != nil {
		return res, fmt.Errorf("falha ao abrir transação no banco alvo: %w", err)
	}
	// Rollback é no-op depois de um Commit bem-sucedido (sql.ErrTxDone,
	// ignorado). Em qualquer return de erro abaixo, o defer dispara e nada
	// parcial do lote fica gravado.
	defer tx.Rollback()

	for _, p := range pedidos {
		var idNovo string
		errMap := tx.QueryRow(
			`SELECT id_novo FROM migracao_id_map WHERE entidade = 'pedido' AND id_legado = $1`, p.id,
		).Scan(&idNovo)
		if errMap == nil {
			res.JaMigrados++
			continue
		}
		if !errors.Is(errMap, sql.ErrNoRows) {
			return res, fmt.Errorf("falha ao consultar migracao_id_map para id_legado=%s: %w", p.id, errMap)
		}

		r, motivo := resolverPedido(p)
		if motivo != "" {
			res.PendentesRevisao = append(res.PendentesRevisao, pendenteDePedido(p, motivo))
			continue
		}

		var pedidoNovoID string
		if err := tx.QueryRow(
			insertPedido,
			r.usuarioID, r.solicitante, r.obra, r.observacao, r.status,
			r.criadoEm, r.decididoPor, r.decididoEm,
		).Scan(&pedidoNovoID); err != nil {
			return res, fmt.Errorf("falha ao inserir pedido para id_legado=%s: %w", p.id, err)
		}

		for _, item := range r.itens {
			if _, err := tx.Exec(
				insertPedidoItem,
				pedidoNovoID, item.produtoID, item.produtoNome, item.categoriaNome,
				item.estoqueID, item.estoqueNome, item.quantidade, item.quantidadeAprovada,
			); err != nil {
				return res, fmt.Errorf("falha ao inserir item do pedido id_legado=%s: %w", p.id, err)
			}
		}

		if _, err := tx.Exec(
			`INSERT INTO migracao_id_map (entidade, id_legado, id_novo) VALUES ('pedido', $1, $2)`,
			p.id, pedidoNovoID,
		); err != nil {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation {
				return res, fmt.Errorf(
					"colisão em migracao_id_map (backstop 23505): id_legado=%s já mapeado para um pedido fora deste lote — corte abortado, nada foi escrito",
					p.id)
			}
			return res, fmt.Errorf("falha ao gravar migracao_id_map para id_legado=%s: %w", p.id, err)
		}

		// Vínculo Movimentação↔Pedido: as Movimentações já migradas (5.4)
		// cujas linhas de legado.historico referenciam este Pedido legado.
		// As não resolvidas viram AvisosData em registrarAvisosPedido abaixo.
		novoIDs, _ := vinculoMovimentacoesDoPedido(p, historicoPorPedidoLegado, movIDNovoPorLegado)
		if len(novoIDs) > 0 {
			if _, err := tx.Exec(
				`UPDATE movimentacoes SET pedido_id = $1 WHERE id = ANY($2::uuid[])`,
				pedidoNovoID, pq.Array(novoIDs),
			); err != nil {
				return res, fmt.Errorf("falha ao vincular movimentações ao pedido id_legado=%s: %w", p.id, err)
			}
		}

		registrarAvisosPedido(&res, p, r, historicoPorPedidoLegado, movIDNovoPorLegado)
		res.Migrados++
	}

	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("falha ao efetivar a transação do corte de Pedidos: %w", err)
	}
	return res, nil
}

// carregarMapaMigracao lê migracao_id_map para uma entidade e devolve
// id_legado -> id_novo. Fora de transação (molde migrarMovimentacoes).
func carregarMapaMigracao(alvo *sql.DB, entidade string) (map[string]string, error) {
	m := make(map[string]string)
	rows, err := alvo.Query(`SELECT id_legado, id_novo FROM migracao_id_map WHERE entidade = $1`, entidade)
	if err != nil {
		return nil, fmt.Errorf("falha ao carregar migracao_id_map (%s): %w", entidade, err)
	}
	defer rows.Close()
	for rows.Next() {
		var idLegado, idNovo string
		if err := rows.Scan(&idLegado, &idNovo); err != nil {
			return nil, fmt.Errorf("falha ao ler migracao_id_map (%s): %w", entidade, err)
		}
		m[idLegado] = idNovo
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar migracao_id_map (%s): %w", entidade, err)
	}
	return m, nil
}

// vinculoMovimentacoesDoPedido resolve as linhas de legado.historico que
// referenciam `p` contra o mapa entidade='movimentacao'. Devolve os id_novo
// resolvidos (para o UPDATE) e a contagem das que não estão no mapa (elas
// próprias foram PendenteRevisao na 5.4 — vínculo não estabelecido, nunca
// aborta).
func vinculoMovimentacoesDoPedido(p pedidoLegado, historicoPorPedido map[string][]string, movIDNovoPorLegado map[string]string) (novoIDs []string, naoMigradas int) {
	for _, histID := range historicoPorPedido[p.id] {
		if novo, ok := movIDNovoPorLegado[histID]; ok {
			novoIDs = append(novoIDs, novo)
		} else {
			naoMigradas++
		}
	}
	return novoIDs, naoMigradas
}

// registrarAvisosPedido acrescenta a res.AvisosData as ressalvas de dado de
// um Pedido migrado: autor não resolvido por e-mail, `criadoEm` ausente, e
// movimentações do histórico referenciadas mas não migradas. Recomputado
// igual no dry-run e no corte real.
func registrarAvisosPedido(res *ResultadoMigracaoPedidos, p pedidoLegado, r pedidoResolvido, historicoPorPedido map[string][]string, movIDNovoPorLegado map[string]string) {
	if !r.autorResolvido {
		res.AvisosData = append(res.AvisosData, avisoAutorPedido(p.id, p.email))
	}
	if !r.criadoEm.Valid {
		res.AvisosData = append(res.AvisosData, avisoCriadoEmPedido(p.id))
	}
	if _, naoMigradas := vinculoMovimentacoesDoPedido(p, historicoPorPedido, movIDNovoPorLegado); naoMigradas > 0 {
		res.AvisosData = append(res.AvisosData, avisoVinculoPedido(p.id, naoMigradas))
	}
}

// pendenteDePedido monta um item de PendentesRevisao a partir do Pedido
// legado bruto + o motivo.
func pendenteDePedido(p pedidoLegado, motivo string) PendenteRevisaoPedido {
	st := ""
	if p.status.Valid {
		st = p.status.String
	}
	sol := ""
	if p.solicitante.Valid {
		sol = p.solicitante.String
	}
	return PendenteRevisaoPedido{
		IDLegado:    p.id,
		Solicitante: sol,
		Status:      st,
		QtdItens:    len(p.itens),
		Motivo:      motivo,
	}
}

// textoQuantidade extrai o valor de `qtd` de um item legado como texto — o
// legado pode gravar número (`3`), string (`"3"`) ou nulo/ausente. Mesma
// guarda de faixa/finitude aplicada depois com strconv.ParseFloat (molde
// migrarMovimentacoes).
func textoQuantidade(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "<nulo>"
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		var str string
		if err := json.Unmarshal(raw, &str); err == nil {
			str = strings.TrimSpace(str)
			if str == "" {
				return "<nulo>"
			}
			return str
		}
	}
	return s
}

func avisoAutorPedido(idLegado string, email sql.NullString) string {
	e := "<nulo>"
	if email.Valid {
		e = email.String
	}
	return fmt.Sprintf(
		"pedido %s: autor não resolvido pelo e-mail %q — usuario_id definido como o usuário sintético de migração", idLegado, e)
}

func avisoCriadoEmPedido(idLegado string) string {
	return fmt.Sprintf(
		"pedido %s: criadoEm ausente no legado — criado_em definido como o momento do corte", idLegado)
}

func avisoVinculoPedido(idLegado string, n int) string {
	return fmt.Sprintf(
		"pedido %s: %d movimentação(ões) do histórico não migradas — vínculo não estabelecido", idLegado, n)
}
