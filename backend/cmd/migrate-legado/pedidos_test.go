package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Testes de migrarPedidos — Story 7.7 (spec-7-7). Cobre toda a I/O &
// Edge-Case Matrix do spec. testDB(t) (main_test.go) já cria/limpa
// legado.pedidos + a coluna legado.historico.pedido e garante o usuário
// sintético "Migração do sistema legado" (seed da migration 000022);
// limparTabelas limpa pedido_itens/pedidos/movimentacoes/legado.pedidos na
// ordem de FK, nunca `usuarios`.

// itemPedido monta um elemento do array `itens` de legado.pedidos
// (addendum §F: `{prodId, nome, unidade, estoque, qtd, categoria}`). `qtd`
// aceita número, string ou nil — o legado grava os três. `unidade` é
// incluída de propósito: o modelo-alvo não a tem e ela nunca deve migrar.
func itemPedido(prodID, nome, estoque, categoria string, qtd any) map[string]any {
	return map[string]any{
		"prodId":    prodID,
		"nome":      nome,
		"unidade":   "un",
		"estoque":   estoque,
		"qtd":       qtd,
		"categoria": categoria,
	}
}

// pedidoLegadoInput agrupa os campos de uma linha de legado.pedidos para os
// testes. EmailNull/ObsNull gravam NULL; CriadoEm/AtualizadoEm nil gravam
// NULL. ItensRaw, quando não vazio, substitui a serialização de Itens.
type pedidoLegadoInput struct {
	ID           string
	Solicitante  string
	Obra         string
	Obs          string
	ObsNull      bool
	Email        string
	EmailNull    bool
	UID          string
	Itens        []map[string]any
	ItensRaw     string
	Status       string
	CriadoEm     *time.Time
	AtualizadoEm *time.Time
}

func inserirLegadoPedido(t *testing.T, alvo *sql.DB, in pedidoLegadoInput) {
	t.Helper()
	var itens any
	if in.ItensRaw != "" {
		itens = in.ItensRaw
	} else {
		b, err := json.Marshal(in.Itens)
		if err != nil {
			t.Fatalf("falha ao montar itens jsonb (%s): %v", in.ID, err)
		}
		itens = string(b)
	}
	var email any
	if !in.EmailNull {
		email = in.Email
	}
	var obs any
	if !in.ObsNull {
		obs = in.Obs
	}
	var criadoEm, atualizadoEm any
	if in.CriadoEm != nil {
		criadoEm = *in.CriadoEm
	}
	if in.AtualizadoEm != nil {
		atualizadoEm = *in.AtualizadoEm
	}
	if _, err := alvo.Exec(`
		INSERT INTO legado.pedidos
			(id, solicitante, obra, obs, email, uid, itens, status, criado_em, atualizado_em)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10)`,
		in.ID, in.Solicitante, in.Obra, obs, email, in.UID, itens, in.Status, criadoEm, atualizadoEm,
	); err != nil {
		t.Fatalf("falha ao inserir legado.pedidos (%s): %v", in.ID, err)
	}
}

// inserirHistoricoComPedido cria uma linha de legado.historico que
// referencia um Pedido legado pelo campo `pedido` (o vínculo que o corte
// restabelece), SEM linha em migracao_id_map — o cenário "movimentação do
// vínculo não migrada".
func inserirHistoricoComPedido(t *testing.T, alvo *sql.DB, histID, pedidoLegadoID string) {
	t.Helper()
	if _, err := alvo.Exec(`
		INSERT INTO legado.historico (id, produto, tipo, origem, destino, qtd, pedido)
		VALUES ($1, 'Item', 'baixa', 'Almox', '—', '1', $2)`,
		histID, pedidoLegadoID,
	); err != nil {
		t.Fatalf("falha ao inserir legado.historico com pedido (%s): %v", histID, err)
	}
}

// seedMovimentacaoComVinculoPedido cria o estado de uma Movimentação já
// migrada pela 5.4 (linha em `movimentacoes` + linha em migracao_id_map
// entidade='movimentacao') cuja linha de legado.historico referencia
// `pedidoLegadoID` pelo campo `pedido`. Devolve o id novo da Movimentação.
func seedMovimentacaoComVinculoPedido(t *testing.T, alvo *sql.DB, histID, produtoID, estoqueID, pedidoLegadoID string) string {
	t.Helper()
	var movID string
	if err := alvo.QueryRow(`
		INSERT INTO movimentacoes (produto_id, tipo, estoque_origem_id, quantidade, usuario_id)
		VALUES ($1, 'baixa', $2, 1, $3) RETURNING id`,
		produtoID, estoqueID, usuarioMigracaoID(t, alvo),
	).Scan(&movID); err != nil {
		t.Fatalf("falha ao criar movimentacao migrada (%s): %v", histID, err)
	}
	if _, err := alvo.Exec(
		`INSERT INTO migracao_id_map (entidade, id_legado, id_novo) VALUES ('movimentacao', $1, $2)`,
		histID, movID,
	); err != nil {
		t.Fatalf("falha ao mapear movimentacao (%s): %v", histID, err)
	}
	inserirHistoricoComPedido(t, alvo, histID, pedidoLegadoID)
	return movID
}

type pedidoLido struct {
	id          string
	usuarioID   string
	solicitante string
	obra        string
	observacao  sql.NullString
	status      string
	criadoEm    time.Time
	decididoPor sql.NullString
	decididoEm  sql.NullTime
}

func lerPedidoMigrado(t *testing.T, alvo *sql.DB, idLegado string) pedidoLido {
	t.Helper()
	var p pedidoLido
	if err := alvo.QueryRow(`
		SELECT p.id, p.usuario_id, p.solicitante, p.obra_centro_custo, p.observacao,
		       p.status, p.criado_em, p.decidido_por, p.decidido_em
		FROM pedidos p
		JOIN migracao_id_map m ON m.id_novo = p.id AND m.entidade = 'pedido'
		WHERE m.id_legado = $1`, idLegado,
	).Scan(&p.id, &p.usuarioID, &p.solicitante, &p.obra, &p.observacao,
		&p.status, &p.criadoEm, &p.decididoPor, &p.decididoEm); err != nil {
		t.Fatalf("falha ao ler pedido migrado de id_legado=%s: %v", idLegado, err)
	}
	return p
}

type itemLido struct {
	produtoID          string
	produtoNome        string
	categoriaNome      string
	estoqueID          string
	estoqueNome        string
	quantidade         float64
	quantidadeAprovada sql.NullFloat64
}

func lerItensDoPedido(t *testing.T, alvo *sql.DB, pedidoID string) []itemLido {
	t.Helper()
	rows, err := alvo.Query(`
		SELECT produto_id, produto_nome, categoria_nome, estoque_id, estoque_nome,
		       quantidade, quantidade_aprovada
		FROM pedido_itens
		WHERE pedido_id = $1
		ORDER BY produto_nome, estoque_nome`, pedidoID)
	if err != nil {
		t.Fatalf("falha ao ler pedido_itens: %v", err)
	}
	defer rows.Close()
	var out []itemLido
	for rows.Next() {
		var it itemLido
		if err := rows.Scan(&it.produtoID, &it.produtoNome, &it.categoriaNome,
			&it.estoqueID, &it.estoqueNome, &it.quantidade, &it.quantidadeAprovada); err != nil {
			t.Fatalf("falha ao escanear pedido_item: %v", err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("falha ao iterar pedido_itens: %v", err)
	}
	return out
}

func pedidoIDDaMov(t *testing.T, alvo *sql.DB, movID string) sql.NullString {
	t.Helper()
	var pid sql.NullString
	if err := alvo.QueryRow(`SELECT pedido_id FROM movimentacoes WHERE id = $1`, movID).Scan(&pid); err != nil {
		t.Fatalf("falha ao ler movimentacoes.pedido_id (%s): %v", movID, err)
	}
	return pid
}

// --- Corte inicial ---------------------------------------------------------

// TestMigrarPedidos_CorteInicial — linha "Corte inicial" da I/O Matrix: N
// Pedidos resolvíveis viram linhas `pedidos` + `pedido_itens` com
// produto_id/estoque_id resolvidos, snapshot de nome/categoria/estoque,
// criado_em legado e status verbatim; N linhas entidade='pedido' no mapa.
func TestMigrarPedidos_CorteInicial(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	prodCim := seedProdutoMigrado(t, alvo, "prod-1", "Cimento CP-II")
	prodVerg := seedProdutoMigrado(t, alvo, "prod-2", "Vergalhão 10mm")
	estA := criarEstoqueAlvo(t, alvo, "Almox Central")
	estB := criarEstoqueAlvo(t, alvo, "Canteiro Norte")

	criado := time.Date(2025, 5, 1, 9, 0, 0, 0, time.UTC)
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-1", Solicitante: "João da Obra", Obra: "Obra 42", Obs: "urgente",
		Email: "naomapeado@x.com", Status: "pendente", CriadoEm: &criado,
		Itens: []map[string]any{
			itemPedido("prod-1", "Cimento CP-II", "Almox Central", cat, 10),
			itemPedido("prod-2", "Vergalhão 10mm", "Canteiro Norte", cat, 5),
		},
	})
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-2", Solicitante: "Maria", Obra: "Obra 7", Status: "pendente", CriadoEm: &criado,
		Itens: []map[string]any{itemPedido("prod-1", "Cimento CP-II", "Almox Central", cat, 2)},
	})

	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Migrados != 2 || res.JaMigrados != 0 || len(res.PendentesRevisao) != 0 {
		t.Fatalf("res = %+v, want Migrados=2 sem pendências", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedidos`); got != 2 {
		t.Errorf("count(pedidos) = %d, want 2", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedido_itens`); got != 3 {
		t.Errorf("count(pedido_itens) = %d, want 3", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map WHERE entidade = 'pedido'`); got != 2 {
		t.Errorf("count(migracao_id_map pedido) = %d, want 2", got)
	}

	p := lerPedidoMigrado(t, alvo, "ped-1")
	if !pareceUUIDv4(p.id) {
		t.Errorf("id do pedido %q não parece UUID v4", p.id)
	}
	if p.solicitante != "João da Obra" || p.obra != "Obra 42" || p.status != "pendente" {
		t.Errorf("pedido ped-1 = %+v", p)
	}
	if !p.observacao.Valid || p.observacao.String != "urgente" {
		t.Errorf("observacao = %v, want 'urgente'", p.observacao)
	}
	if !p.criadoEm.Equal(criado) {
		t.Errorf("criado_em = %v, want %v (legado preservado)", p.criadoEm, criado)
	}

	itens := lerItensDoPedido(t, alvo, p.id)
	if len(itens) != 2 {
		t.Fatalf("itens de ped-1 = %d, want 2", len(itens))
	}
	if itens[0].produtoID != prodCim || itens[0].estoqueID != estA ||
		itens[0].produtoNome != "Cimento CP-II" || itens[0].categoriaNome != cat ||
		itens[0].estoqueNome != "Almox Central" || itens[0].quantidade != 10 {
		t.Errorf("item[0] = %+v", itens[0])
	}
	if itens[1].produtoID != prodVerg || itens[1].estoqueID != estB ||
		itens[1].estoqueNome != "Canteiro Norte" || itens[1].quantidade != 5 {
		t.Errorf("item[1] = %+v", itens[1])
	}
}

// TestMigrarPedidos_Idempotente — linha "Reexecução (idempotência)": a 2ª
// execução não escreve nada, conta tudo como JaMigrados e não altera o
// pedido_id já gravado nas Movimentações.
func TestMigrarPedidos_Idempotente(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	prodID := seedProdutoMigrado(t, alvo, "p1", "Areia Média")
	estID := criarEstoqueAlvo(t, alvo, "Almox")
	movID := seedMovimentacaoComVinculoPedido(t, alvo, "h1", prodID, estID, "ped-1")

	atualizado := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-1", Solicitante: "S", Obra: "O", Status: "aprovado", AtualizadoEm: &atualizado,
		Itens: []map[string]any{itemPedido("p1", "Areia Média", "Almox", cat, 3)},
	})

	if _, err := migrarPedidos(alvo, legado, true); err != nil {
		t.Fatalf("1ª execução falhou: %v", err)
	}
	pidApos1 := pedidoIDDaMov(t, alvo, movID)
	if !pidApos1.Valid {
		t.Fatalf("pedido_id não foi vinculado na 1ª execução")
	}

	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("2ª execução retornou erro: %v", err)
	}
	if res.Migrados != 0 || res.JaMigrados != 1 {
		t.Fatalf("res 2ª execução = %+v, want Migrados=0 JaMigrados=1", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedidos`); got != 1 {
		t.Errorf("count(pedidos) = %d, want 1 — reexecução não duplica", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedido_itens`); got != 1 {
		t.Errorf("count(pedido_itens) = %d, want 1", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map WHERE entidade = 'pedido'`); got != 1 {
		t.Errorf("count(migracao_id_map pedido) = %d, want 1", got)
	}
	if pidApos2 := pedidoIDDaMov(t, alvo, movID); pidApos2.String != pidApos1.String {
		t.Errorf("pedido_id da movimentação mudou entre execuções: %v -> %v", pidApos1, pidApos2)
	}
}

// TestMigrarPedidos_DecisaoPorStatus — linhas "Pedido pendente/aprovado/
// rejeitado": decidido_por/decidido_em e quantidade_aprovada conforme o
// status legado.
func TestMigrarPedidos_DecisaoPorStatus(t *testing.T) {
	casos := []struct {
		status       string
		querDecidido bool
		querAprovada sql.NullFloat64
	}{
		{"pendente", false, sql.NullFloat64{}},
		{"aprovado", true, sql.NullFloat64{Float64: 4, Valid: true}},
		{"rejeitado", true, sql.NullFloat64{Float64: 0, Valid: true}},
	}
	for _, c := range casos {
		t.Run(c.status, func(t *testing.T) {
			alvo, legado := testDB(t)
			_, cat := categoriaExistente(t, alvo)
			seedProdutoMigrado(t, alvo, "p1", "Brita 1")
			criarEstoqueAlvo(t, alvo, "Almox")
			criado := time.Date(2025, 6, 1, 8, 0, 0, 0, time.UTC)
			atualizado := time.Date(2025, 6, 15, 8, 0, 0, 0, time.UTC)
			inserirLegadoPedido(t, alvo, pedidoLegadoInput{
				ID: "ped-1", Solicitante: "S", Obra: "O", Status: c.status,
				CriadoEm: &criado, AtualizadoEm: &atualizado,
				Itens: []map[string]any{itemPedido("p1", "Brita 1", "Almox", cat, 4)},
			})

			res, err := migrarPedidos(alvo, legado, true)
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if res.Migrados != 1 {
				t.Fatalf("Migrados = %d, want 1 (res=%+v)", res.Migrados, res)
			}

			p := lerPedidoMigrado(t, alvo, "ped-1")
			if p.status != c.status {
				t.Errorf("status = %q, want %q (verbatim)", p.status, c.status)
			}
			if c.querDecidido {
				if !p.decididoPor.Valid || p.decididoPor.String != usuarioMigracaoID(t, alvo) {
					t.Errorf("decidido_por = %v, want usuário sintético", p.decididoPor)
				}
				if !p.decididoEm.Valid || !p.decididoEm.Time.Equal(atualizado) {
					t.Errorf("decidido_em = %v, want %v (atualizadoEm legado)", p.decididoEm, atualizado)
				}
			} else if p.decididoPor.Valid || p.decididoEm.Valid {
				t.Errorf("pendente não pode ter decidido_por/decidido_em: %v / %v", p.decididoPor, p.decididoEm)
			}

			itens := lerItensDoPedido(t, alvo, p.id)
			if len(itens) != 1 {
				t.Fatalf("itens = %d, want 1", len(itens))
			}
			got := itens[0].quantidadeAprovada
			if got.Valid != c.querAprovada.Valid || (got.Valid && got.Float64 != c.querAprovada.Float64) {
				t.Errorf("quantidade_aprovada = %v, want %v", got, c.querAprovada)
			}
		})
	}
}

// TestMigrarPedidos_DecididoEmCaiParaCriadoEm — status decidido mas
// `atualizadoEm` ausente: decidido_em = COALESCE(atualizadoEm, criadoEm) =
// criadoEm legado.
func TestMigrarPedidos_DecididoEmCaiParaCriadoEm(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	seedProdutoMigrado(t, alvo, "p1", "Cal Hidratada")
	criarEstoqueAlvo(t, alvo, "Almox")
	criado := time.Date(2025, 4, 4, 12, 0, 0, 0, time.UTC)
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-1", Solicitante: "S", Obra: "O", Status: "aprovado", CriadoEm: &criado,
		Itens: []map[string]any{itemPedido("p1", "Cal Hidratada", "Almox", cat, 1)},
	})

	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Migrados != 1 {
		t.Fatalf("Migrados = %d, want 1", res.Migrados)
	}
	p := lerPedidoMigrado(t, alvo, "ped-1")
	if !p.decididoEm.Valid || !p.decididoEm.Time.Equal(criado) {
		t.Errorf("decidido_em = %v, want %v (fallback para criadoEm)", p.decididoEm, criado)
	}
}

// TestMigrarPedidos_AutorResolvidoPorEmail — linha "Autor resolvido por
// e-mail": lower(email) casa uma linha de `usuarios` (inclusive com caixa
// diferente); usuario_id = essa linha, sem AvisosData.
func TestMigrarPedidos_AutorResolvidoPorEmail(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	seedProdutoMigrado(t, alvo, "p1", "Cal")
	criarEstoqueAlvo(t, alvo, "Almox")

	var autorID string
	if err := alvo.QueryRow(`
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
		VALUES ('Fulano Solicitante', $1, NULL, 'usuario', true, true)
		RETURNING id`, "solicitante-7-7@teste.local",
	).Scan(&autorID); err != nil {
		t.Fatalf("falha ao criar usuário autor: %v", err)
	}
	t.Cleanup(func() {
		// Ordem de FK: pedido_itens -> pedidos -> usuarios (sem cascade em
		// pedidos.usuario_id). limparTabelas do próximo teste faz o resto.
		alvo.Exec(`DELETE FROM pedido_itens WHERE pedido_id IN (SELECT id FROM pedidos WHERE usuario_id = $1)`, autorID)
		alvo.Exec(`DELETE FROM pedidos WHERE usuario_id = $1`, autorID)
		alvo.Exec(`DELETE FROM usuarios WHERE id = $1`, autorID)
	})

	criado := time.Date(2025, 7, 7, 7, 0, 0, 0, time.UTC)
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-1", Solicitante: "Fulano", Obra: "O", Status: "pendente", CriadoEm: &criado,
		Email: "Solicitante-7-7@Teste.Local", // caixa diferente
		Itens: []map[string]any{itemPedido("p1", "Cal", "Almox", cat, 1)},
	})

	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(res.AvisosData) != 0 {
		t.Errorf("AvisosData = %+v, want vazio (autor resolvido por e-mail)", res.AvisosData)
	}
	if p := lerPedidoMigrado(t, alvo, "ped-1"); p.usuarioID != autorID {
		t.Errorf("usuario_id = %s, want %s (autor por e-mail)", p.usuarioID, autorID)
	}
}

// TestMigrarPedidos_AutorNaoResolvido — linha "Autor não resolvido": e-mail
// nulo/vazio ou sem match => usuario_id = usuário sintético + AvisosData;
// nunca é pendência.
func TestMigrarPedidos_AutorNaoResolvido(t *testing.T) {
	casos := []struct {
		nome string
		in   pedidoLegadoInput
	}{
		{"email nulo", pedidoLegadoInput{EmailNull: true}},
		{"email sem match", pedidoLegadoInput{Email: "ninguem@lugar.nenhum"}},
		{"email vazio", pedidoLegadoInput{Email: "   "}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			alvo, legado := testDB(t)
			_, cat := categoriaExistente(t, alvo)
			seedProdutoMigrado(t, alvo, "p1", "Tinta")
			criarEstoqueAlvo(t, alvo, "Almox")

			criado := time.Date(2025, 7, 7, 7, 0, 0, 0, time.UTC)
			in := c.in
			in.ID, in.Solicitante, in.Obra, in.Status = "ped-1", "S", "O", "pendente"
			in.CriadoEm = &criado
			in.Itens = []map[string]any{itemPedido("p1", "Tinta", "Almox", cat, 1)}
			inserirLegadoPedido(t, alvo, in)

			res, err := migrarPedidos(alvo, legado, true)
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if res.Migrados != 1 || len(res.PendentesRevisao) != 0 {
				t.Fatalf("res = %+v, want Migrados=1 sem pendências (autor não é pendência)", res)
			}
			if len(res.AvisosData) != 1 {
				t.Fatalf("AvisosData = %+v, want 1 (autor não resolvido)", res.AvisosData)
			}
			if p := lerPedidoMigrado(t, alvo, "ped-1"); p.usuarioID != usuarioMigracaoID(t, alvo) {
				t.Errorf("usuario_id = %s, want usuário sintético", p.usuarioID)
			}
		})
	}
}

// --- Pendências por Pedido (falha é POR PEDIDO, não por item) --------------

// TestMigrarPedidos_ItemComProdutoNaoMigrado — linha "Item com produto não
// migrado": o Pedido INTEIRO entra em PendentesRevisao; nada dele é
// inserido; os demais Pedidos do lote migram; o lote não aborta.
func TestMigrarPedidos_ItemComProdutoNaoMigrado(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	seedProdutoMigrado(t, alvo, "p-ok", "Cimento")
	criarEstoqueAlvo(t, alvo, "Almox")

	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-ok", Solicitante: "S", Obra: "O", Status: "pendente",
		Itens: []map[string]any{itemPedido("p-ok", "Cimento", "Almox", cat, 1)},
	})
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-bad", Solicitante: "S2", Obra: "O2", Status: "pendente",
		Itens: []map[string]any{
			itemPedido("p-ok", "Cimento", "Almox", cat, 1),
			itemPedido("p-fantasma", "Coisa", "Almox", cat, 1),
		},
	})

	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("o lote não pode abortar por Pedido irresolúvel: %v", err)
	}
	if res.Migrados != 1 {
		t.Errorf("Migrados = %d, want 1 (ped-ok migra apesar de ped-bad)", res.Migrados)
	}
	if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].IDLegado != "ped-bad" {
		t.Fatalf("PendentesRevisao = %+v, want 1 item ped-bad", res.PendentesRevisao)
	}
	pr := res.PendentesRevisao[0]
	if pr.Motivo != "item com produto não migrado: prodId=p-fantasma" {
		t.Errorf("Motivo = %q", pr.Motivo)
	}
	if pr.Solicitante != "S2" || pr.Status != "pendente" || pr.QtdItens != 2 {
		t.Errorf("pendência sem dados-chave: %+v", pr)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedidos`); got != 1 {
		t.Errorf("count(pedidos) = %d, want 1 — nada de ped-bad inserido", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedido_itens`); got != 1 {
		t.Errorf("count(pedido_itens) = %d, want 1", got)
	}
}

// TestMigrarPedidos_ItemComEstoqueDesconhecido — linha "Item com estoque
// desconhecido": `item.estoque` sem match em estoques.nome_normalizado.
func TestMigrarPedidos_ItemComEstoqueDesconhecido(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	seedProdutoMigrado(t, alvo, "p1", "Prego")
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-1", Solicitante: "S", Obra: "O", Status: "pendente",
		Itens: []map[string]any{itemPedido("p1", "Prego", "Estoque Fantasma", cat, 1)},
	})

	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(res.PendentesRevisao) != 1 ||
		res.PendentesRevisao[0].Motivo != "item com estoque 'Estoque Fantasma' não encontrado" {
		t.Fatalf("PendentesRevisao = %+v, want estoque não encontrado", res.PendentesRevisao)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedidos`); got != 0 {
		t.Errorf("count(pedidos) = %d, want 0", got)
	}
}

// TestMigrarPedidos_ItemSemNomeOuCategoria — linha "Item sem nome/categoria".
func TestMigrarPedidos_ItemSemNomeOuCategoria(t *testing.T) {
	casos := []struct {
		nome, item, motivo string
	}{
		{"sem nome", "nome", "item sem nome"},
		{"sem categoria", "categoria", "item sem categoria"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			alvo, legado := testDB(t)
			_, cat := categoriaExistente(t, alvo)
			seedProdutoMigrado(t, alvo, "p1", "Massa")
			criarEstoqueAlvo(t, alvo, "Almox")

			it := itemPedido("p1", "Massa", "Almox", cat, 1)
			it[c.item] = "" // zera nome OU categoria
			inserirLegadoPedido(t, alvo, pedidoLegadoInput{
				ID: "ped-1", Solicitante: "S", Obra: "O", Status: "pendente",
				Itens: []map[string]any{it},
			})

			res, err := migrarPedidos(alvo, legado, true)
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].Motivo != c.motivo {
				t.Fatalf("PendentesRevisao = %+v, want %q", res.PendentesRevisao, c.motivo)
			}
			if got := contar(t, alvo, `SELECT count(*) FROM pedidos`); got != 0 {
				t.Errorf("count(pedidos) = %d, want 0", got)
			}
		})
	}
}

// TestMigrarPedidos_QuantidadeInvalida — linha "Quantidade inválida": nula,
// não-numérica, NaN/Inf, abaixo de 0.001 ou acima de 9999999.999.
func TestMigrarPedidos_QuantidadeInvalida(t *testing.T) {
	casos := []struct {
		nome   string
		qtd    any
		motivo string
	}{
		{"nula", nil, "quantidade inválida: '<nulo>'"},
		{"nao-numerica", "dez", "quantidade inválida: 'dez'"},
		{"nan", "NaN", "quantidade inválida: 'NaN'"},
		{"inf", "Inf", "quantidade inválida: 'Inf'"},
		{"abaixo-da-faixa", 0.0004, "quantidade inválida: '0.0004'"},
		{"acima-da-faixa", 99999999999, "quantidade inválida: '99999999999'"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			alvo, legado := testDB(t)
			_, cat := categoriaExistente(t, alvo)
			seedProdutoMigrado(t, alvo, "p1", "Areia")
			criarEstoqueAlvo(t, alvo, "Almox")
			inserirLegadoPedido(t, alvo, pedidoLegadoInput{
				ID: "ped-1", Solicitante: "S", Obra: "O", Status: "pendente",
				Itens: []map[string]any{itemPedido("p1", "Areia", "Almox", cat, c.qtd)},
			})

			res, err := migrarPedidos(alvo, legado, true)
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].Motivo != c.motivo {
				t.Fatalf("PendentesRevisao = %+v, want %q", res.PendentesRevisao, c.motivo)
			}
			if got := contar(t, alvo, `SELECT count(*) FROM pedidos`); got != 0 {
				t.Errorf("count(pedidos) = %d, want 0", got)
			}
		})
	}
}

// TestMigrarPedidos_QuantidadeComoString — o protótipo legado grava `qtd`
// como número OU como string JSON; textoQuantidade tem um ramo dedicado à
// string entre aspas. Uma `qtd` `"3"` (string) migra com quantidade 3.
func TestMigrarPedidos_QuantidadeComoString(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	prodID := seedProdutoMigrado(t, alvo, "p1", "Cimento")
	estID := criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-1", Solicitante: "S", Obra: "O", Status: "pendente",
		Itens: []map[string]any{itemPedido("p1", "Cimento", "Almox", cat, "3")}, // string, não número
	})

	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Migrados != 1 || len(res.PendentesRevisao) != 0 {
		t.Fatalf("res = %+v, want Migrados=1 sem pendências", res)
	}
	p := lerPedidoMigrado(t, alvo, "ped-1")
	itens := lerItensDoPedido(t, alvo, p.id)
	if len(itens) != 1 {
		t.Fatalf("itens = %d, want 1", len(itens))
	}
	if itens[0].produtoID != prodID || itens[0].estoqueID != estID || itens[0].quantidade != 3 {
		t.Errorf("item = %+v, want quantidade 3", itens[0])
	}
}

// TestMigrarPedidos_QuantidadeSomadaForaDaFaixa — dois itens do mesmo Pedido
// com o mesmo prodId + estoque, cada `qtd` individualmente válida mas cuja
// SOMA estoura 9999999.999 (NUMERIC(10,3)): o Pedido inteiro vira pendência
// "quantidade somada fora da faixa" — o lote NÃO aborta.
func TestMigrarPedidos_QuantidadeSomadaForaDaFaixa(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	seedProdutoMigrado(t, alvo, "p1", "Cimento")
	seedProdutoMigrado(t, alvo, "p2", "Areia")
	criarEstoqueAlvo(t, alvo, "Almox")

	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-soma", Solicitante: "S", Obra: "O", Status: "pendente",
		Itens: []map[string]any{
			itemPedido("p1", "Cimento", "Almox", cat, 6000000),
			itemPedido("p1", "Cimento", "Almox", cat, 5000000),
		},
	})
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-ok", Solicitante: "S", Obra: "O", Status: "pendente",
		Itens: []map[string]any{itemPedido("p2", "Areia", "Almox", cat, 1)},
	})

	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("o lote não pode abortar: %v", err)
	}
	if res.Migrados != 1 {
		t.Errorf("Migrados = %d, want 1 (ped-ok migra)", res.Migrados)
	}
	if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].IDLegado != "ped-soma" ||
		res.PendentesRevisao[0].Motivo != "quantidade somada fora da faixa: '11000000'" {
		t.Fatalf("PendentesRevisao = %+v, want ped-soma 'quantidade somada fora da faixa'", res.PendentesRevisao)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedido_itens`); got != 1 {
		t.Errorf("count(pedido_itens) = %d, want 1 — nada de ped-soma inserido", got)
	}
}

// TestMigrarPedidos_StatusInvalido — linha "Status inválido": `status` fora
// de {pendente, aprovado, rejeitado} (aqui um valor legado inesperado e o
// vazio).
func TestMigrarPedidos_StatusInvalido(t *testing.T) {
	for _, st := range []string{"cancelado", ""} {
		t.Run("status="+st, func(t *testing.T) {
			alvo, legado := testDB(t)
			_, cat := categoriaExistente(t, alvo)
			seedProdutoMigrado(t, alvo, "p1", "Cimento")
			criarEstoqueAlvo(t, alvo, "Almox")
			inserirLegadoPedido(t, alvo, pedidoLegadoInput{
				ID: "ped-1", Solicitante: "S", Obra: "O", Status: st,
				Itens: []map[string]any{itemPedido("p1", "Cimento", "Almox", cat, 1)},
			})

			res, err := migrarPedidos(alvo, legado, true)
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			want := "status inválido: '" + st + "'"
			if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].Motivo != want {
				t.Fatalf("PendentesRevisao = %+v, want %q", res.PendentesRevisao, want)
			}
			if got := contar(t, alvo, `SELECT count(*) FROM pedidos`); got != 0 {
				t.Errorf("count(pedidos) = %d, want 0", got)
			}
		})
	}
}

// TestMigrarPedidos_PedidoSemItens — classe de pendência "pedido sem itens":
// `itens` é um array vazio. O Pedido inteiro vira pendência; um Pedido irmão
// resolvível no mesmo lote migra; o lote não aborta.
func TestMigrarPedidos_PedidoSemItens(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	seedProdutoMigrado(t, alvo, "p1", "Cimento")
	criarEstoqueAlvo(t, alvo, "Almox")

	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-vazio", Solicitante: "S", Obra: "O", Status: "pendente",
		Itens: []map[string]any{}, // array vazio
	})
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-ok", Solicitante: "S", Obra: "O", Status: "pendente",
		Itens: []map[string]any{itemPedido("p1", "Cimento", "Almox", cat, 1)},
	})

	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("o lote não pode abortar: %v", err)
	}
	if res.Migrados != 1 {
		t.Errorf("Migrados = %d, want 1 (ped-ok migra)", res.Migrados)
	}
	if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].IDLegado != "ped-vazio" ||
		res.PendentesRevisao[0].Motivo != "pedido sem itens" {
		t.Fatalf("PendentesRevisao = %+v, want ped-vazio 'pedido sem itens'", res.PendentesRevisao)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedidos`); got != 1 {
		t.Errorf("count(pedidos) = %d, want 1", got)
	}
}

// TestMigrarPedidos_ItensIlegivel — classe de pendência "itens ilegível": a
// coluna `itens` (jsonb) guarda JSON sintaticamente válido mas com forma
// errada (um objeto, não o array esperado) — json.Unmarshal no
// []itemLegadoPedido falha. (Um jsonb não aceita JSON malformado, então o
// caso é "válido mas forma errada".) O Pedido inteiro vira pendência; o
// lote não aborta.
func TestMigrarPedidos_ItensIlegivel(t *testing.T) {
	alvo, legado := testDB(t)
	seedProdutoMigrado(t, alvo, "p1", "Cimento")
	criarEstoqueAlvo(t, alvo, "Almox")

	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-bad", Solicitante: "S", Obra: "O", Status: "pendente",
		ItensRaw: `{}`, // objeto onde se espera um array
	})

	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("o lote não pode abortar: %v", err)
	}
	if res.Migrados != 0 {
		t.Errorf("Migrados = %d, want 0", res.Migrados)
	}
	if len(res.PendentesRevisao) != 1 || res.PendentesRevisao[0].IDLegado != "ped-bad" ||
		!strings.HasPrefix(res.PendentesRevisao[0].Motivo, "itens ilegível:") {
		t.Fatalf("PendentesRevisao = %+v, want ped-bad motivo começando com 'itens ilegível:'", res.PendentesRevisao)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedidos`); got != 0 {
		t.Errorf("count(pedidos) = %d, want 0", got)
	}
}

// TestMigrarPedidos_ItensColidindoNoPar — linha "Itens colidindo no par
// produto/estoque": dois itens do mesmo Pedido com o mesmo prodId + estoque
// (inclusive por normalização de caixa/espaço) => uma linha pedido_itens
// com quantidade = soma; Pedido migrado normalmente, sem pendência.
func TestMigrarPedidos_ItensColidindoNoPar(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	prodID := seedProdutoMigrado(t, alvo, "p1", "Cimento")
	estID := criarEstoqueAlvo(t, alvo, "Almox Central")
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-1", Solicitante: "S", Obra: "O", Status: "pendente",
		Itens: []map[string]any{
			itemPedido("p1", "Cimento", "Almox Central", cat, 2),
			itemPedido("p1", "Cimento", "  almox   central ", cat, 3),
		},
	})

	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Migrados != 1 || len(res.PendentesRevisao) != 0 {
		t.Fatalf("res = %+v, want Migrados=1 sem pendências", res)
	}
	p := lerPedidoMigrado(t, alvo, "ped-1")
	itens := lerItensDoPedido(t, alvo, p.id)
	if len(itens) != 1 {
		t.Fatalf("itens = %d, want 1 (colisão soma-e-descarta)", len(itens))
	}
	if itens[0].produtoID != prodID || itens[0].estoqueID != estID || itens[0].quantidade != 5 {
		t.Errorf("item = %+v, want quantidade 5 numa única linha", itens[0])
	}
}

// TestMigrarPedidos_CriadoEmAusente — linha "`criadoEm` ausente": Pedido
// resolvível migra com criado_em = now() e entra em AvisosData.
func TestMigrarPedidos_CriadoEmAusente(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	seedProdutoMigrado(t, alvo, "p1", "Lixa 220")
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-1", Solicitante: "S", Obra: "O", Status: "pendente",
		// Email resolvível (usuário sintético) isola o aviso ao `criadoEm`.
		Email: emailUsuarioMigracaoLegado,
		Itens: []map[string]any{itemPedido("p1", "Lixa 220", "Almox", cat, 1)},
		// CriadoEm nil
	})

	antes := time.Now().Add(-2 * time.Second)
	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Migrados != 1 || len(res.PendentesRevisao) != 0 {
		t.Fatalf("res = %+v, want Migrados=1 sem pendências", res)
	}
	if len(res.AvisosData) != 1 {
		t.Fatalf("AvisosData = %+v, want 1 (criadoEm ausente)", res.AvisosData)
	}
	p := lerPedidoMigrado(t, alvo, "ped-1")
	if p.criadoEm.Before(antes) || time.Since(p.criadoEm) > time.Minute {
		t.Errorf("criado_em = %v, want ~agora (momento do corte)", p.criadoEm)
	}
}

// --- Vínculo Movimentação↔Pedido -----------------------------------------

// TestMigrarPedidos_VinculoMovimentacaoPedido — linha "Vínculo Movimentação↔
// Pedido": as Movimentações já migradas (5.4) cujas linhas de
// legado.historico referenciam o Pedido legado recebem pedido_id = id novo;
// Movimentações de outro Pedido ficam intocadas.
func TestMigrarPedidos_VinculoMovimentacaoPedido(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	prodID := seedProdutoMigrado(t, alvo, "p1", "Cimento")
	estID := criarEstoqueAlvo(t, alvo, "Almox")
	mov1 := seedMovimentacaoComVinculoPedido(t, alvo, "h1", prodID, estID, "ped-1")
	mov2 := seedMovimentacaoComVinculoPedido(t, alvo, "h2", prodID, estID, "ped-1")
	movOutro := seedMovimentacaoComVinculoPedido(t, alvo, "h3", prodID, estID, "ped-2")

	criado := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	atualizado := time.Date(2025, 2, 2, 0, 0, 0, 0, time.UTC)
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-1", Solicitante: "S", Obra: "O", Status: "aprovado",
		Email: emailUsuarioMigracaoLegado, CriadoEm: &criado, AtualizadoEm: &atualizado,
		Itens: []map[string]any{itemPedido("p1", "Cimento", "Almox", cat, 1)},
	})

	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Migrados != 1 || len(res.AvisosData) != 0 {
		t.Fatalf("res = %+v, want Migrados=1 sem avisos", res)
	}
	p := lerPedidoMigrado(t, alvo, "ped-1")
	for _, mid := range []string{mov1, mov2} {
		if pid := pedidoIDDaMov(t, alvo, mid); !pid.Valid || pid.String != p.id {
			t.Errorf("movimentação %s pedido_id = %v, want %s", mid, pid, p.id)
		}
	}
	if pid := pedidoIDDaMov(t, alvo, movOutro); pid.Valid {
		t.Errorf("movimentação de ped-2 (não migrado) não deveria ser vinculada: %v", pid)
	}
}

// TestMigrarPedidos_MovimentacaoDoVinculoNaoMigrada — linha "Movimentação do
// vínculo não migrada": a linha de legado.historico referencia o Pedido mas
// não está em migracao_id_map (ficou PendenteRevisao na 5.4). O Pedido
// migra; AvisosData registra as não vinculadas; o lote não aborta.
func TestMigrarPedidos_MovimentacaoDoVinculoNaoMigrada(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	prodID := seedProdutoMigrado(t, alvo, "p1", "Cimento")
	estID := criarEstoqueAlvo(t, alvo, "Almox")
	movOK := seedMovimentacaoComVinculoPedido(t, alvo, "h-ok", prodID, estID, "ped-1")
	inserirHistoricoComPedido(t, alvo, "h-perdida", "ped-1") // sem migracao_id_map

	criado := time.Date(2025, 3, 2, 0, 0, 0, 0, time.UTC)
	atualizado := time.Date(2025, 3, 3, 0, 0, 0, 0, time.UTC)
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-1", Solicitante: "S", Obra: "O", Status: "aprovado",
		Email: emailUsuarioMigracaoLegado, CriadoEm: &criado, AtualizadoEm: &atualizado,
		Itens: []map[string]any{itemPedido("p1", "Cimento", "Almox", cat, 1)},
	})

	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("o lote não pode abortar: %v", err)
	}
	if res.Migrados != 1 {
		t.Fatalf("Migrados = %d, want 1", res.Migrados)
	}
	if len(res.AvisosData) != 1 || !strings.Contains(res.AvisosData[0], "não migradas") {
		t.Fatalf("AvisosData = %+v, want 1 aviso de vínculo não estabelecido", res.AvisosData)
	}
	p := lerPedidoMigrado(t, alvo, "ped-1")
	if pid := pedidoIDDaMov(t, alvo, movOK); !pid.Valid || pid.String != p.id {
		t.Errorf("a movimentação migrada deveria ser vinculada: %v", pid)
	}
}

// --- Dry-run -------------------------------------------------------------

// TestMigrarPedidos_DryRun — linha "Dry-run": conta migraria/pendências mas
// não escreve nada e não altera nenhum pedido_id.
func TestMigrarPedidos_DryRun(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	prodID := seedProdutoMigrado(t, alvo, "p1", "Cimento")
	estID := criarEstoqueAlvo(t, alvo, "Almox")
	movID := seedMovimentacaoComVinculoPedido(t, alvo, "h1", prodID, estID, "ped-1")

	criado := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-1", Solicitante: "S", Obra: "O", Status: "pendente", CriadoEm: &criado,
		Itens: []map[string]any{itemPedido("p1", "Cimento", "Almox", cat, 1)},
	})
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-2", Solicitante: "S", Obra: "O", Status: "pendente", CriadoEm: &criado,
		Itens: []map[string]any{itemPedido("inexistente", "X", "Almox", cat, 1)},
	})

	res, err := migrarPedidos(alvo, legado, false)
	if err != nil {
		t.Fatalf("dry-run retornou erro: %v", err)
	}
	if res.Migrados != 1 || res.JaMigrados != 0 || len(res.PendentesRevisao) != 1 {
		t.Fatalf("res = %+v, want Migrados=1 JaMigrados=0 Pendências=1", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedidos`); got != 0 {
		t.Errorf("count(pedidos) = %d, want 0 — dry-run não escreve", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedido_itens`); got != 0 {
		t.Errorf("count(pedido_itens) = %d, want 0", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map WHERE entidade = 'pedido'`); got != 0 {
		t.Errorf("count(migracao_id_map pedido) = %d, want 0", got)
	}
	if pid := pedidoIDDaMov(t, alvo, movID); pid.Valid {
		t.Errorf("dry-run não pode vincular movimentação: %v", pid)
	}
}

// TestMigrarPedidos_DryRunContabilizaJaMigrados — dry-run após um corte real
// reconhece o Pedido já mapeado (JaMigrados), sem escrever.
func TestMigrarPedidos_DryRunContabilizaJaMigrados(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	seedProdutoMigrado(t, alvo, "p1", "Cimento")
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-1", Solicitante: "S", Obra: "O", Status: "pendente",
		Itens: []map[string]any{itemPedido("p1", "Cimento", "Almox", cat, 1)},
	})
	if _, err := migrarPedidos(alvo, legado, true); err != nil {
		t.Fatalf("corte inicial falhou: %v", err)
	}

	seedProdutoMigrado(t, alvo, "p2", "Areia")
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-2", Solicitante: "S", Obra: "O", Status: "pendente",
		Itens: []map[string]any{itemPedido("p2", "Areia", "Almox", cat, 1)},
	})

	res, err := migrarPedidos(alvo, legado, false)
	if err != nil {
		t.Fatalf("dry-run retornou erro: %v", err)
	}
	if res.Migrados != 1 || res.JaMigrados != 1 {
		t.Fatalf("res = %+v, want Migrados=1 JaMigrados=1", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedidos`); got != 1 {
		t.Errorf("count(pedidos) = %d, want 1 — dry-run não escreve o novo", got)
	}
}

// --- Pré-condições que abortam -----------------------------------------

// TestMigrarPedidos_SeedUsuarioAusente — linha "Seed do usuário ausente":
// sem a linha `usuarios` sentinela, aborta antes de qualquer escrita.
func TestMigrarPedidos_SeedUsuarioAusente(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)

	if _, err := alvo.Exec(`DELETE FROM usuarios WHERE lower(email) = lower($1)`, emailUsuarioMigracaoLegado); err != nil {
		t.Fatalf("falha ao remover o usuário sintético: %v", err)
	}
	t.Cleanup(func() {
		alvo.Exec(`
			INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
			VALUES ('Migração do sistema legado', $1, NULL, 'almoxarife', false, false)
			ON CONFLICT (lower(email)) DO NOTHING`, emailUsuarioMigracaoLegado)
	})

	seedProdutoMigrado(t, alvo, "p1", "Cimento")
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-1", Solicitante: "S", Obra: "O", Status: "pendente",
		Itens: []map[string]any{itemPedido("p1", "Cimento", "Almox", cat, 1)},
	})

	res, err := migrarPedidos(alvo, legado, true)
	if !errors.Is(err, errSeedUsuarioMigracaoAusente) {
		t.Fatalf("erro = %v, want errSeedUsuarioMigracaoAusente", err)
	}
	if res.Migrados != 0 {
		t.Errorf("Migrados = %d, want 0", res.Migrados)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedidos`); got != 0 {
		t.Errorf("count(pedidos) = %d, want 0 — nada escrito", got)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map WHERE entidade = 'pedido'`); got != 0 {
		t.Errorf("count(migracao_id_map pedido) = %d, want 0", got)
	}
}

// TestMigrarPedidos_LegadoPedidosIlegivel — linha "`legado.pedidos`
// ilegível": o SELECT em legado.pedidos falha (tabela ausente).
func TestMigrarPedidos_LegadoPedidosIlegivel(t *testing.T) {
	alvo, legado := testDB(t)

	if _, err := alvo.Exec(`DROP TABLE legado.pedidos`); err != nil {
		t.Fatalf("falha ao dropar legado.pedidos: %v", err)
	}
	t.Cleanup(func() {
		alvo.Exec(`CREATE TABLE IF NOT EXISTS legado.pedidos (
			id text primary key,
			solicitante text,
			obra text,
			obs text,
			email text,
			uid text,
			itens jsonb,
			status text,
			criado_em timestamptz,
			atualizado_em timestamptz
		)`)
	})

	res, err := migrarPedidos(alvo, legado, true)
	if err == nil {
		t.Fatalf("migrarPedidos deveria falhar com legado.pedidos ausente; res=%+v", res)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM pedidos`); got != 0 {
		t.Errorf("count(pedidos) = %d, want 0", got)
	}
}

// TestMigrarPedidos_FalhaInesperadaNoInsert — linha "Falha inesperada no
// INSERT": um erro de banco no meio do lote (aqui simulado renomeando
// `pedidos`) faz a transação única sofrer rollback via defer; nada parcial
// é gravado e o erro cita o id_legado.
func TestMigrarPedidos_FalhaInesperadaNoInsert(t *testing.T) {
	alvo, legado := testDB(t)
	_, cat := categoriaExistente(t, alvo)
	seedProdutoMigrado(t, alvo, "p1", "Cimento")
	criarEstoqueAlvo(t, alvo, "Almox")
	inserirLegadoPedido(t, alvo, pedidoLegadoInput{
		ID: "ped-1", Solicitante: "S", Obra: "O", Status: "pendente",
		Itens: []map[string]any{itemPedido("p1", "Cimento", "Almox", cat, 1)},
	})

	if _, err := alvo.Exec(`ALTER TABLE pedidos RENAME TO pedidos_bkp_teste`); err != nil {
		t.Fatalf("falha ao renomear pedidos: %v", err)
	}
	t.Cleanup(func() {
		if _, err := alvo.Exec(`ALTER TABLE pedidos_bkp_teste RENAME TO pedidos`); err != nil {
			t.Fatalf("falha ao restaurar pedidos: %v", err)
		}
	})

	res, err := migrarPedidos(alvo, legado, true)
	if err == nil {
		t.Fatalf("esperava erro com pedidos ausente; res=%+v", res)
	}
	if !strings.Contains(err.Error(), "ped-1") {
		t.Errorf("erro não cita o id_legado: %v", err)
	}
	if got := contar(t, alvo, `SELECT count(*) FROM migracao_id_map WHERE entidade = 'pedido'`); got != 0 {
		t.Errorf("count(migracao_id_map pedido) = %d, want 0 — transação revertida", got)
	}
}

// TestMigrarPedidos_LegadoVazio — legado.pedidos sem linhas: tudo zero, sem
// erro.
func TestMigrarPedidos_LegadoVazio(t *testing.T) {
	alvo, legado := testDB(t)
	res, err := migrarPedidos(alvo, legado, true)
	if err != nil {
		t.Fatalf("erro inesperado para legado.pedidos vazio: %v", err)
	}
	if res.Migrados != 0 || res.JaMigrados != 0 || len(res.PendentesRevisao) != 0 || len(res.AvisosData) != 0 {
		t.Fatalf("res = %+v, want tudo zero", res)
	}
}
