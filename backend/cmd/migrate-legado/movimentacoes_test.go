package main

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

// Testes de migrarMovimentacoes — Story 5.4 (spec-5-4). Cobre toda a I/O &
// Edge-Case Matrix do spec. testDB(t) (main_test.go) já cria/limpa
// legado.historico e garante o usuário sintético "Migração do sistema legado"
// (seed da migration 000022); limparTabelas limpa `movimentacoes` e
// `legado.historico`, nunca `usuarios`.

// legadoHistoricoInput agrupa os campos de uma linha de legado.historico para
// os testes. Um campo string vazio vira string vazia no banco; QtdNulo grava
// NULL em `qtd`; Timestamp nil grava NULL em `"timestamp"`.
type legadoHistoricoInput struct {
	ID        string
	Produto   string
	Tipo      string
	Origem    string
	Destino   string
	Qtd       string
	QtdNulo   bool
	Unidade   string
	Obs       string
	Timestamp *time.Time
}

func inserirLegadoHistorico(t *testing.T, alvo *sql.DB, in legadoHistoricoInput) {
	t.Helper()
	var qtd any
	if !in.QtdNulo {
		qtd = in.Qtd
	}
	var ts any
	if in.Timestamp != nil {
		ts = *in.Timestamp
	}
	if _, err := alvo.Exec(`
		INSERT INTO legado.historico (id, produto, tipo, origem, destino, qtd, unidade, obs, "timestamp")
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		in.ID, in.Produto, in.Tipo, in.Origem, in.Destino, qtd, in.Unidade, in.Obs, ts,
	); err != nil {
		t.Fatalf("falha ao inserir historico legado (%s): %v", in.ID, err)
	}
}

// seedProdutoLegado insere só uma linha em legado.produtos (nome
// desnormalizado). Sem linha em migracao_id_map — o cenário "produto não
// migrado".
func seedProdutoLegado(t *testing.T, alvo *sql.DB, idLegado, nome string) {
	t.Helper()
	if _, err := alvo.Exec(`INSERT INTO legado.produtos (id, nome) VALUES ($1, $2)`, idLegado, nome); err != nil {
		t.Fatalf("falha ao inserir legado.produtos (%s): %v", idLegado, err)
	}
}

// seedProdutoMigrado cria o estado de um Produto legado JÁ migrado: linha em
// legado.produtos + linha em `produtos` no alvo + linha em migracao_id_map
// (entidade='produto'). Devolve o produto_id novo.
func seedProdutoMigrado(t *testing.T, alvo *sql.DB, idLegado, nome string) string {
	t.Helper()
	categoriaID, _ := categoriaExistente(t, alvo)
	var produtoID string
	if err := alvo.QueryRow(
		`INSERT INTO produtos (nome, categoria_id) VALUES ($1, $2) RETURNING id`, nome, categoriaID,
	).Scan(&produtoID); err != nil {
		t.Fatalf("falha ao criar produto no alvo (%s): %v", nome, err)
	}
	if _, err := alvo.Exec(
		`INSERT INTO migracao_id_map (entidade, id_legado, id_novo) VALUES ('produto', $1, $2)`, idLegado, produtoID,
	); err != nil {
		t.Fatalf("falha ao criar migracao_id_map produto (%s): %v", idLegado, err)
	}
	seedProdutoLegado(t, alvo, idLegado, nome)
	return produtoID
}

func usuarioMigracaoID(t *testing.T, alvo *sql.DB) string {
	t.Helper()
	var id string
	if err := alvo.QueryRow(
		`SELECT id FROM usuarios WHERE lower(email) = lower($1)`, emailUsuarioMigracaoLegado,
	).Scan(&id); err != nil {
		t.Fatalf("falha ao resolver o usuário sintético: %v", err)
	}
	return id
}

// movLidaPorLegado devolve a linha `movimentacoes` ligada a `idLegado` pela
// migracao_id_map (entidade='movimentacao').
func movLidaPorLegado(t *testing.T, alvo *sql.DB, idLegado string) (produtoID, tipo string, origem, destino sql.NullString, qtd float64, usuarioID string, criadoEm time.Time) {
	t.Helper()
	if err := alvo.QueryRow(`
		SELECT m.produto_id, m.tipo, m.estoque_origem_id, m.estoque_destino_id,
		       m.quantidade, m.usuario_id, m.criado_em
		FROM movimentacoes m
		JOIN migracao_id_map mm ON mm.id_novo = m.id AND mm.entidade = 'movimentacao'
		WHERE mm.id_legado = $1`, idLegado,
	).Scan(&produtoID, &tipo, &origem, &destino, &qtd, &usuarioID, &criadoEm); err != nil {
		t.Fatalf("falha ao ler movimentacao de id_legado=%s: %v", idLegado, err)
	}
	return
}

// --- Corte inicial -----------------------------------------------------------

// TestMigrarMovimentacoes_CorteInicial — linha "Corte inicial" da I/O Matrix:
// baixa e transferência resolvíveis viram linhas `movimentacoes` com
// produto/origem/destino resolvidos, criado_em = timestamp legado e
// usuario_id = usuário sintético.
func TestMigrarMovimentacoes_CorteInicial(t *testing.T) {
	alvo, legado := testDB(t)

	prodCimento := seedProdutoMigrado(t, alvo, "p-cim", "Cimento CP-II")
	prodAco := seedProdutoMigrado(t, alvo, "p-aco", "Vergalhão 10mm")
	origemID := criarEstoqueAlvo(t, alvo, "Almox Central")
	destinoID := criarEstoqueAlvo(t, alvo, "Canteiro Norte")

	ts := time.Date(2025, 3, 10, 14, 30, 0, 0, time.UTC)
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Cimento CP-II", Tipo: "baixa",
		Origem: "Almox Central", Destino: "—", Qtd: "12.5", Timestamp: &ts,
	})
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h2", Produto: "Vergalhão 10mm", Tipo: "transferencia",
		Origem: "Almox Central", Destino: "Canteiro Norte", Qtd: "3", Timestamp: &ts,
	})

	res, err := migrarMovimentacoes(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Migrados != 2 || res.JaMigrados != 0 || len(res.PendentesRevisao) != 0 || len(res.AvisosData) != 0 {
		t.Fatalf("res = %+v, want Migrados=2 e listas vazias", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM movimentacoes`); got != 2 {
		t.Errorf("count(movimentacoes) = %d, want 2", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map WHERE entidade = 'movimentacao'`); got != 2 {
		t.Errorf("count(migracao_id_map movimentacao) = %d, want 2", got)
	}

	usrID := usuarioMigracaoID(t, alvo)

	prodGot, tipo, origem, destino, qtd, usr, criadoEm := movLidaPorLegado(t, alvo, "h1")
	if prodGot != prodCimento || tipo != "baixa" || !origem.Valid || origem.String != origemID ||
		destino.Valid || qtd != 12.5 || usr != usrID {
		t.Errorf("h1 = prod=%s tipo=%s origem=%v destino=%v qtd=%v usr=%s", prodGot, tipo, origem, destino, qtd, usr)
	}
	if !criadoEm.Equal(ts) {
		t.Errorf("h1 criado_em = %v, want %v (timestamp legado preservado)", criadoEm, ts)
	}

	prodGot, tipo, origem, destino, qtd, _, criadoEm = movLidaPorLegado(t, alvo, "h2")
	if prodGot != prodAco || tipo != "transferencia" || origem.String != origemID ||
		!destino.Valid || destino.String != destinoID || qtd != 3 {
		t.Errorf("h2 = prod=%s tipo=%s origem=%v destino=%v qtd=%v", prodGot, tipo, origem, destino, qtd)
	}
	if !criadoEm.Equal(ts) {
		t.Errorf("h2 criado_em = %v, want %v", criadoEm, ts)
	}
}

// TestMigrarMovimentacoes_Idempotente — linha "Reexecução (idempotência)":
// segunda execução não escreve nada, conta tudo como JaMigrados.
func TestMigrarMovimentacoes_Idempotente(t *testing.T) {
	alvo, legado := testDB(t)

	seedProdutoMigrado(t, alvo, "p1", "Areia Média")
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Areia Média", Tipo: "baixa", Origem: "Almox", Destino: "—", Qtd: "2",
	})

	if _, err := migrarMovimentacoes(alvo, legado, true); err != nil {
		t.Fatalf("primeira execução falhou: %v", err)
	}

	res, err := migrarMovimentacoes(alvo, legado, true)
	if err != nil {
		t.Fatalf("segunda execução retornou erro: %v", err)
	}
	if res.Migrados != 0 || res.JaMigrados != 1 {
		t.Fatalf("res 2ª execução = %+v, want Migrados=0 JaMigrados=1", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM movimentacoes`); got != 1 {
		t.Errorf("count(movimentacoes) = %d, want 1 — reexecução não duplica", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map WHERE entidade = 'movimentacao'`); got != 1 {
		t.Errorf("count(migracao_id_map movimentacao) = %d, want 1", got)
	}
}

// TestMigrarMovimentacoes_BaixaIgnoraDestino — linha "Baixa": qualquer
// `destino` legado ('—', vazio, nulo ou texto qualquer) resulta em
// estoque_destino_id NULL, sem virar pendência.
func TestMigrarMovimentacoes_BaixaIgnoraDestino(t *testing.T) {
	for _, destino := range []string{"—", "", "Destino Textual Qualquer"} {
		t.Run("destino="+destino, func(t *testing.T) {
			alvo, legado := testDB(t)
			seedProdutoMigrado(t, alvo, "p1", "Prego 17x27")
			origemID := criarEstoqueAlvo(t, alvo, "Almox")
			inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
				ID: "h1", Produto: "Prego 17x27", Tipo: "baixa", Origem: "Almox", Destino: destino, Qtd: "5",
			})

			res, err := migrarMovimentacoes(alvo, legado, true)
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if res.Migrados != 1 || len(res.PendentesRevisao) != 0 {
				t.Fatalf("res = %+v, want Migrados=1 sem pendências", res)
			}
			_, tipo, origem, dst, _, _, _ := movLidaPorLegado(t, alvo, "h1")
			if tipo != "baixa" || origem.String != origemID || dst.Valid {
				t.Errorf("h1 = tipo=%s origem=%v destino=%v, want baixa/origem resolvida/destino NULL", tipo, origem, dst)
			}
		})
	}
}

// --- Pendências de Produto --------------------------------------------------

// TestMigrarMovimentacoes_ProdutoNaoMigrado — linha "Produto não migrado":
// casa 1 linha em legado.produtos mas o id não está no mapa. Não inserido,
// entra em PendentesRevisao; os demais registros do lote migram.
func TestMigrarMovimentacoes_ProdutoNaoMigrado(t *testing.T) {
	alvo, legado := testDB(t)

	seedProdutoMigrado(t, alvo, "p-ok", "Areia Grossa")
	seedProdutoLegado(t, alvo, "p-bad", "Brita 1") // sem linha no mapa
	criarEstoqueAlvo(t, alvo, "Almox")

	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h-ok", Produto: "Areia Grossa", Tipo: "baixa", Origem: "Almox", Destino: "—", Qtd: "1",
	})
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h-bad", Produto: "Brita 1", Tipo: "baixa", Origem: "Almox", Destino: "—", Qtd: "1",
	})

	res, err := migrarMovimentacoes(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado (o lote não pode abortar): %v", err)
	}
	if res.Migrados != 1 {
		t.Errorf("Migrados = %d, want 1 (h-ok migra apesar da pendência de h-bad)", res.Migrados)
	}
	if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].IDLegado != "h-bad" ||
		res.PendentesRevisao[0].Motivo != "produto não migrado" {
		t.Fatalf("PendentesRevisao = %+v, want 1 item h-bad 'produto não migrado'", res.PendentesRevisao)
	}
	if p := res.PendentesRevisao[0]; p.Produto != "Brita 1" || p.Tipo != "baixa" || p.Qtd != "1" {
		t.Errorf("pendência sem dados brutos: %+v", p)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM movimentacoes`); got != 1 {
		t.Errorf("count(movimentacoes) = %d, want 1", got)
	}
}

// TestMigrarMovimentacoes_ProdutoNaoEncontrado — linha "Produto não
// encontrado no legado": `historico.produto` não casa nenhuma linha de
// legado.produtos.
func TestMigrarMovimentacoes_ProdutoNaoEncontrado(t *testing.T) {
	alvo, legado := testDB(t)
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Coisa Inexistente", Tipo: "baixa", Origem: "Almox", Destino: "—", Qtd: "1",
	})

	res, err := migrarMovimentacoes(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].Motivo != "produto não encontrado no legado" {
		t.Fatalf("PendentesRevisao = %+v, want 'produto não encontrado no legado'", res.PendentesRevisao)
	}
	if res.Migrados != 0 {
		t.Errorf("Migrados = %d, want 0", res.Migrados)
	}
}

// TestMigrarMovimentacoes_ProdutoAmbiguo — linha "Nome de produto ambíguo":
// `historico.produto` casa >1 linha em legado.produtos (após btrim).
func TestMigrarMovimentacoes_ProdutoAmbiguo(t *testing.T) {
	alvo, legado := testDB(t)
	seedProdutoLegado(t, alvo, "p1", "Tinta Branca")
	seedProdutoLegado(t, alvo, "p2", "Tinta Branca ") // btrim iguala
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Tinta Branca", Tipo: "baixa", Origem: "Almox", Destino: "—", Qtd: "1",
	})

	res, err := migrarMovimentacoes(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].Motivo != "nome de produto ambíguo no legado" {
		t.Fatalf("PendentesRevisao = %+v, want 'nome de produto ambíguo no legado'", res.PendentesRevisao)
	}
}

// --- Pendências de Estoque / tipo / quantidade ----------------------------

// TestMigrarMovimentacoes_EstoqueNaoEncontrado — linha "Estoque não
// encontrado": origem sem match em estoques.nome_normalizado.
func TestMigrarMovimentacoes_EstoqueNaoEncontrado(t *testing.T) {
	alvo, legado := testDB(t)
	seedProdutoMigrado(t, alvo, "p1", "Cabo 2.5mm")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Cabo 2.5mm", Tipo: "baixa", Origem: "Almox Fantasma", Destino: "—", Qtd: "1",
	})

	res, err := migrarMovimentacoes(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].Motivo != "estoque 'Almox Fantasma' não encontrado" {
		t.Fatalf("PendentesRevisao = %+v, want \"estoque 'Almox Fantasma' não encontrado\"", res.PendentesRevisao)
	}
}

// TestMigrarMovimentacoes_OrigemAusente — `baixa` cuja linha legada não tem
// `origem` (vazio/nulo): pendência "origem ausente no historico", distinta de
// "estoque não encontrado".
func TestMigrarMovimentacoes_OrigemAusente(t *testing.T) {
	alvo, legado := testDB(t)
	seedProdutoMigrado(t, alvo, "p1", "Cimento CP-II")
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Cimento CP-II", Tipo: "baixa", Origem: "", Destino: "—", Qtd: "1",
	})

	res, err := migrarMovimentacoes(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].Motivo != "origem ausente no historico" {
		t.Fatalf("PendentesRevisao = %+v, want 'origem ausente no historico'", res.PendentesRevisao)
	}
}

// TestMigrarMovimentacoes_DestinoAusenteEmTransferencia — `transferencia` com
// origem válida mas `destino` vazio/nulo: pendência "destino ausente no
// historico", distinta de "estoque não encontrado".
func TestMigrarMovimentacoes_DestinoAusenteEmTransferencia(t *testing.T) {
	alvo, legado := testDB(t)
	seedProdutoMigrado(t, alvo, "p1", "Vergalhão 10mm")
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Vergalhão 10mm", Tipo: "transferencia", Origem: "Almox", Destino: "", Qtd: "1",
	})

	res, err := migrarMovimentacoes(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].Motivo != "destino ausente no historico" {
		t.Fatalf("PendentesRevisao = %+v, want 'destino ausente no historico'", res.PendentesRevisao)
	}
}

// TestMigrarMovimentacoes_TransferenciaDestinoDesconhecido — transferência
// com origem conhecida mas destino sem match.
func TestMigrarMovimentacoes_TransferenciaDestinoDesconhecido(t *testing.T) {
	alvo, legado := testDB(t)
	seedProdutoMigrado(t, alvo, "p1", "Viga W")
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Viga W", Tipo: "transferencia", Origem: "Almox", Destino: "Canteiro X", Qtd: "1",
	})

	res, err := migrarMovimentacoes(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].Motivo != "estoque 'Canteiro X' não encontrado" {
		t.Fatalf("PendentesRevisao = %+v, want destino não encontrado", res.PendentesRevisao)
	}
}

// TestMigrarMovimentacoes_TipoInvalido — linha "Tipo inválido".
func TestMigrarMovimentacoes_TipoInvalido(t *testing.T) {
	alvo, legado := testDB(t)
	seedProdutoMigrado(t, alvo, "p1", "Parafuso")
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Parafuso", Tipo: "ajuste", Origem: "Almox", Destino: "—", Qtd: "1",
	})

	res, err := migrarMovimentacoes(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].Motivo != "tipo inválido: 'ajuste'" {
		t.Fatalf("PendentesRevisao = %+v, want \"tipo inválido: 'ajuste'\"", res.PendentesRevisao)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM movimentacoes`); got != 0 {
		t.Errorf("count(movimentacoes) = %d, want 0", got)
	}
}

// TestMigrarMovimentacoes_QuantidadeInvalida — linha "Quantidade inválida":
// nula, não-numérica, zero ou negativa.
func TestMigrarMovimentacoes_QuantidadeInvalida(t *testing.T) {
	casos := []struct {
		nome   string
		qtd    string
		nulo   bool
		motivo string
	}{
		{"nula", "", true, "quantidade inválida: '<nulo>'"},
		{"nao-numerica", "dez", false, "quantidade inválida: 'dez'"},
		{"zero", "0", false, "quantidade inválida: '0'"},
		{"negativa", "-4", false, "quantidade inválida: '-4'"},
		{"nan", "NaN", false, "quantidade inválida: 'NaN'"},
		{"inf", "Inf", false, "quantidade inválida: 'Inf'"},
		{"infinity", "Infinity", false, "quantidade inválida: 'Infinity'"},
		{"arredonda-para-zero", "0.0004", false, "quantidade inválida: '0.0004'"},
		{"fora-de-faixa", "99999999999", false, "quantidade inválida: '99999999999'"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			alvo, legado := testDB(t)
			seedProdutoMigrado(t, alvo, "p1", "Massa Corrida")
			criarEstoqueAlvo(t, alvo, "Almox")
			inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
				ID: "h1", Produto: "Massa Corrida", Tipo: "baixa", Origem: "Almox", Destino: "—",
				Qtd: c.qtd, QtdNulo: c.nulo,
			})

			res, err := migrarMovimentacoes(alvo, legado, true)
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].Motivo != c.motivo {
				t.Fatalf("PendentesRevisao = %+v, want %q", res.PendentesRevisao, c.motivo)
			}
			if got := contar(t, alvo, `SELECT count(*) FROM movimentacoes`); got != 0 {
				t.Errorf("count(movimentacoes) = %d, want 0", got)
			}
		})
	}
}

// TestMigrarMovimentacoes_TransferenciaOrigemIgualDestino — linha
// "Transferência origem = destino": os dois lados resolvem para o mesmo
// estoque_id (inclusive por normalização de caixa/espaço).
func TestMigrarMovimentacoes_TransferenciaOrigemIgualDestino(t *testing.T) {
	alvo, legado := testDB(t)
	seedProdutoMigrado(t, alvo, "p1", "Disco de Corte")
	criarEstoqueAlvo(t, alvo, "Almox Central")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Disco de Corte", Tipo: "transferencia",
		Origem: "Almox Central", Destino: "  almox   central ", Qtd: "1",
	})

	res, err := migrarMovimentacoes(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].Motivo != "origem igual ao destino" {
		t.Fatalf("PendentesRevisao = %+v, want 'origem igual ao destino'", res.PendentesRevisao)
	}
}

// TestMigrarMovimentacoes_TimestampAusente — linha "Timestamp ausente": linha
// resolvível migra com criado_em = now() e entra em AvisosData.
func TestMigrarMovimentacoes_TimestampAusente(t *testing.T) {
	alvo, legado := testDB(t)
	seedProdutoMigrado(t, alvo, "p1", "Lixa 220")
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Lixa 220", Tipo: "baixa", Origem: "Almox", Destino: "—", Qtd: "1",
		// Timestamp nil
	})

	antes := time.Now().Add(-2 * time.Second)
	res, err := migrarMovimentacoes(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Migrados != 1 || len(res.PendentesRevisao) != 0 {
		t.Fatalf("res = %+v, want Migrados=1 sem pendências", res)
	}
	if len(res.AvisosData) != 1 {
		t.Fatalf("AvisosData = %+v, want 1 aviso de timestamp ausente", res.AvisosData)
	}
	_, _, _, _, _, _, criadoEm := movLidaPorLegado(t, alvo, "h1")
	if criadoEm.Before(antes) || time.Since(criadoEm) > time.Minute {
		t.Errorf("criado_em = %v, want ~agora (momento do corte)", criadoEm)
	}
}

// --- Pré-condições que abortam --------------------------------------------

// TestMigrarMovimentacoes_SeedUsuarioAusente — linha "Seed do usuário
// ausente": sem a linha `usuarios` sentinela, aborta antes de qualquer
// escrita.
func TestMigrarMovimentacoes_SeedUsuarioAusente(t *testing.T) {
	alvo, legado := testDB(t)

	if _, err := alvo.Exec(`DELETE FROM usuarios WHERE lower(email) = lower($1)`, emailUsuarioMigracaoLegado); err != nil {
		t.Fatalf("falha ao remover o usuário sintético para o teste: %v", err)
	}
	t.Cleanup(func() {
		// Restaura o seed (limparTabelas nunca toca usuarios; outras suítes
		// contam com a linha presente na 1ª execução da migration).
		alvo.Exec(`
			INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
			VALUES ('Migração do sistema legado', $1, NULL, 'almoxarife', false, false)
			ON CONFLICT (lower(email)) DO NOTHING`, emailUsuarioMigracaoLegado)
	})

	seedProdutoMigrado(t, alvo, "p1", "Cimento CP-II")
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Cimento CP-II", Tipo: "baixa", Origem: "Almox", Destino: "—", Qtd: "5",
	})

	res, err := migrarMovimentacoes(alvo, legado, true)
	if !errors.Is(err, errSeedUsuarioMigracaoAusente) {
		t.Fatalf("erro = %v, want errSeedUsuarioMigracaoAusente", err)
	}
	if res.Migrados != 0 {
		t.Errorf("Migrados = %d, want 0", res.Migrados)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM movimentacoes`); got != 0 {
		t.Errorf("count(movimentacoes) = %d, want 0 — nada escrito", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map WHERE entidade = 'movimentacao'`); got != 0 {
		t.Errorf("count(migracao_id_map movimentacao) = %d, want 0", got)
	}
}

// TestMigrarMovimentacoes_HistoricoIlegivel — linha "historico ilegível": o
// SELECT em legado.historico falha (tabela ausente).
func TestMigrarMovimentacoes_HistoricoIlegivel(t *testing.T) {
	alvo, legado := testDB(t)

	if _, err := alvo.Exec(`DROP TABLE legado.historico`); err != nil {
		t.Fatalf("falha ao dropar legado.historico: %v", err)
	}
	t.Cleanup(func() {
		alvo.Exec(`CREATE TABLE IF NOT EXISTS legado.historico (
			id text primary key, produto text, tipo text, origem text, destino text,
			qtd text, unidade text, obs text, "timestamp" timestamptz)`)
	})

	res, err := migrarMovimentacoes(alvo, legado, true)
	if err == nil {
		t.Fatalf("migrarMovimentacoes deveria falhar com historico ausente; res=%+v", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM movimentacoes`); got != 0 {
		t.Errorf("count(movimentacoes) = %d, want 0", got)
	}
}

// --- Dry-run ---------------------------------------------------------------

// TestMigrarMovimentacoes_DryRun — linha "Dry-run": conta mas não escreve.
func TestMigrarMovimentacoes_DryRun(t *testing.T) {
	alvo, legado := testDB(t)
	seedProdutoMigrado(t, alvo, "p1", "Areia Fina")
	seedProdutoLegado(t, alvo, "p2", "Sem Mapa") // pendência
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Areia Fina", Tipo: "baixa", Origem: "Almox", Destino: "—", Qtd: "1",
	})
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h2", Produto: "Sem Mapa", Tipo: "baixa", Origem: "Almox", Destino: "—", Qtd: "1",
	})

	res, err := migrarMovimentacoes(alvo, legado, false)
	if err != nil {
		t.Fatalf("dry-run retornou erro: %v", err)
	}
	if res.Migrados != 1 || res.JaMigrados != 0 || len(res.PendentesRevisao) != 1 {
		t.Fatalf("res = %+v, want Migrados=1 JaMigrados=0 Pendências=1", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM movimentacoes`); got != 0 {
		t.Errorf("count(movimentacoes) = %d, want 0 — dry-run não escreve", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map WHERE entidade = 'movimentacao'`); got != 0 {
		t.Errorf("count(migracao_id_map movimentacao) = %d, want 0", got)
	}
}

// TestMigrarMovimentacoes_DryRunContabilizaJaMigrados — dry-run após um corte
// real reconhece as linhas já mapeadas (JaMigrados) sem escrever.
func TestMigrarMovimentacoes_DryRunContabilizaJaMigrados(t *testing.T) {
	alvo, legado := testDB(t)
	seedProdutoMigrado(t, alvo, "p1", "Bloco Cerâmico")
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Bloco Cerâmico", Tipo: "baixa", Origem: "Almox", Destino: "—", Qtd: "1",
	})
	if _, err := migrarMovimentacoes(alvo, legado, true); err != nil {
		t.Fatalf("corte inicial falhou: %v", err)
	}

	seedProdutoMigrado(t, alvo, "p2", "Telha Colonial")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h2", Produto: "Telha Colonial", Tipo: "baixa", Origem: "Almox", Destino: "—", Qtd: "1",
	})

	res, err := migrarMovimentacoes(alvo, legado, false)
	if err != nil {
		t.Fatalf("dry-run retornou erro: %v", err)
	}
	if res.Migrados != 1 || res.JaMigrados != 1 {
		t.Fatalf("res = %+v, want Migrados=1 JaMigrados=1", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM movimentacoes`); got != 1 {
		t.Errorf("count(movimentacoes) = %d, want 1 — dry-run não escreve a linha nova", got)
	}
}

// TestMigrarMovimentacoes_DryRunAvisaTimestampAusente — o dry-run mandatório
// pré-corte precisa mostrar quais linhas perderão a data original (criado_em
// = momento do corte), não só o corte real.
func TestMigrarMovimentacoes_DryRunAvisaTimestampAusente(t *testing.T) {
	alvo, legado := testDB(t)
	seedProdutoMigrado(t, alvo, "p1", "Lixa 120")
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Lixa 120", Tipo: "baixa", Origem: "Almox", Destino: "—", Qtd: "1",
		// Timestamp nil
	})

	res, err := migrarMovimentacoes(alvo, legado, false)
	if err != nil {
		t.Fatalf("dry-run retornou erro: %v", err)
	}
	if res.Migrados != 1 || len(res.PendentesRevisao) != 0 {
		t.Fatalf("res = %+v, want Migrados=1 sem pendências", res)
	}
	if len(res.AvisosData) != 1 {
		t.Fatalf("AvisosData = %+v, want 1 aviso mesmo em dry-run", res.AvisosData)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM movimentacoes`); got != 0 {
		t.Errorf("count(movimentacoes) = %d, want 0 — dry-run não escreve", got)
	}
}

// TestMigrarMovimentacoes_LegadoVazio — historico sem linhas: tudo zero, sem
// erro.
func TestMigrarMovimentacoes_LegadoVazio(t *testing.T) {
	alvo, legado := testDB(t)
	res, err := migrarMovimentacoes(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado para historico vazio: %v", err)
	}
	if res.Migrados != 0 || res.JaMigrados != 0 || len(res.PendentesRevisao) != 0 {
		t.Fatalf("res = %+v, want tudo zero", res)
	}
}

// TestMigrarMovimentacoes_FalhaInesperadaNoInsert — linha "Falha inesperada no
// INSERT" da I/O Matrix: um erro de banco no meio do lote (aqui simulado
// renomeando `movimentacoes` — molde de "falha de banco" da spec-5-3) faz a
// transação única sofrer rollback via defer; nada parcial é gravado e o erro
// cita o id_legado.
func TestMigrarMovimentacoes_FalhaInesperadaNoInsert(t *testing.T) {
	alvo, legado := testDB(t)

	seedProdutoMigrado(t, alvo, "p-1", "Cimento CP-II")
	criarEstoqueAlvo(t, alvo, "Almox Central")
	inserirLegadoHistorico(t, alvo, legadoHistoricoInput{
		ID: "h1", Produto: "Cimento CP-II", Tipo: "baixa",
		Origem: "Almox Central", Destino: "—", Qtd: "5",
	})

	if _, err := alvo.Exec(`ALTER TABLE movimentacoes RENAME TO movimentacoes_bkp_teste`); err != nil {
		t.Fatalf("falha ao renomear movimentacoes: %v", err)
	}
	t.Cleanup(func() {
		if _, err := alvo.Exec(`ALTER TABLE movimentacoes_bkp_teste RENAME TO movimentacoes`); err != nil {
			t.Fatalf("falha ao restaurar movimentacoes: %v", err)
		}
	})

	res, err := migrarMovimentacoes(alvo, legado, true)
	if err == nil {
		t.Fatalf("esperava erro com movimentacoes ausente; res=%+v", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map WHERE entidade = 'movimentacao'`); got != 0 {
		t.Errorf("count(migracao_id_map movimentacao) = %d, want 0 — transação revertida", got)
	}
}
