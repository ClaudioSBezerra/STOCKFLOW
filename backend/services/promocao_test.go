package services

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

// inserirSolicitacao grava uma linha em solicitacoes_promocao com status
// controlado. `decididoPor` vazio -> NULL. Para status != 'pendente',
// `decidido_em` recebe now() (trilha de auditoria). Devolve o id gerado.
func inserirSolicitacao(t *testing.T, db *sql.DB, solicitanteID, papelAlvo, status, decididoPor string) string {
	t.Helper()
	var decisor sql.NullString
	if decididoPor != "" {
		decisor = sql.NullString{String: decididoPor, Valid: true}
	}
	// decidido_em: now() quando já decidida, NULL enquanto pendente.
	decididoEm := sql.NullTime{Time: time.Now().UTC(), Valid: status != "pendente"}
	const insert = `
		INSERT INTO solicitacoes_promocao (solicitante_id, papel_alvo, status, decidido_por, decidido_em)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`
	var id string
	if err := db.QueryRow(insert, solicitanteID, papelAlvo, status, decisor, decididoEm).Scan(&id); err != nil {
		t.Fatalf("falha ao inserir solicitação (%s): %v", status, err)
	}
	return id
}

func papelDaConta(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var papel string
	if err := db.QueryRow(`SELECT papel FROM usuarios WHERE id = $1`, id).Scan(&papel); err != nil {
		t.Fatalf("falha ao ler papel da conta %s: %v", id, err)
	}
	return papel
}

// --- proximoPapelPromocao / papelAbaixoDe ---------------------------------

func TestProximoPapelPromocao(t *testing.T) {
	casos := []struct {
		papel     string
		wantAlvo  string
		wantHasUp bool
	}{
		{PapelUsuario, PapelAlmoxarife, true},
		{PapelAlmoxarife, PapelGestor, true},
		{PapelGestor, "", false},
		{PapelAdm, "", false},
		{"", "", false},
		{"desconhecido", "", false},
	}
	for _, c := range casos {
		alvo, ok := proximoPapelPromocao(c.papel)
		if alvo != c.wantAlvo || ok != c.wantHasUp {
			t.Errorf("proximoPapelPromocao(%q) = (%q, %v), want (%q, %v)", c.papel, alvo, ok, c.wantAlvo, c.wantHasUp)
		}
	}
}

func TestPapelAbaixoDe(t *testing.T) {
	if got := papelAbaixoDe(PapelAlmoxarife); got != PapelUsuario {
		t.Errorf("papelAbaixoDe(almoxarife) = %q, want %q", got, PapelUsuario)
	}
	if got := papelAbaixoDe(PapelGestor); got != PapelAlmoxarife {
		t.Errorf("papelAbaixoDe(gestor) = %q, want %q", got, PapelAlmoxarife)
	}
	if got := papelAbaixoDe("qualquer"); got != "" {
		t.Errorf("papelAbaixoDe(qualquer) = %q, want \"\"", got)
	}
}

// --- SolicitarPromocao ---------------------------------------------------

// TestSolicitarPromocao_AlvoDerivadoDoPapel prova as duas primeiras linhas da
// I/O Matrix: `usuario` -> alvo `almoxarife`; `almoxarife` -> alvo `gestor`;
// sempre `status='pendente'` e uma única linha gravada.
func TestSolicitarPromocao_AlvoDerivadoDoPapel(t *testing.T) {
	db := testDB(t)

	casos := []struct {
		papelAtual string
		wantAlvo   string
	}{
		{PapelUsuario, PapelAlmoxarife},
		{PapelAlmoxarife, PapelGestor},
	}
	for _, c := range casos {
		t.Run(c.papelAtual, func(t *testing.T) {
			id := semearConta(t, db, "Conta "+c.papelAtual, c.papelAtual+"-solicita@empresa.com", c.papelAtual, 1)

			s, err := SolicitarPromocao(db, id, c.papelAtual)
			if err != nil {
				t.Fatalf("SolicitarPromocao erro inesperado: %v", err)
			}
			if s.PapelAlvo != c.wantAlvo {
				t.Errorf("PapelAlvo = %q, want %q", s.PapelAlvo, c.wantAlvo)
			}
			if s.Status != "pendente" {
				t.Errorf("Status = %q, want pendente", s.Status)
			}
			if s.ID == "" || s.CriadoEm.IsZero() {
				t.Errorf("ID/CriadoEm não preenchidos: %+v", s)
			}
			if s.DecididoEm != nil {
				t.Errorf("DecididoEm = %v, want nil em solicitação pendente", s.DecididoEm)
			}

			var n int
			if err := db.QueryRow(
				`SELECT count(*) FROM solicitacoes_promocao WHERE solicitante_id = $1 AND papel_alvo = $2 AND status = 'pendente'`,
				id, c.wantAlvo,
			).Scan(&n); err != nil {
				t.Fatalf("count: %v", err)
			}
			if n != 1 {
				t.Errorf("linhas pendentes = %d, want 1", n)
			}
		})
	}
}

// TestSolicitarPromocao_PapelSemPromocao prova a linha "Solicitar, gestor ou
// adm" da I/O Matrix: ErrPromocaoIndisponivel e nenhuma linha gravada.
func TestSolicitarPromocao_PapelSemPromocao(t *testing.T) {
	db := testDB(t)

	for _, papel := range []string{PapelGestor, PapelAdm} {
		t.Run(papel, func(t *testing.T) {
			id := semearConta(t, db, "Conta "+papel, papel+"-sem-promo@empresa.com", papel, 1)

			_, err := SolicitarPromocao(db, id, papel)
			if !errors.Is(err, ErrPromocaoIndisponivel) {
				t.Fatalf("erro = %v, want ErrPromocaoIndisponivel", err)
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM solicitacoes_promocao WHERE solicitante_id = $1`, id).Scan(&n); err != nil {
				t.Fatalf("count: %v", err)
			}
			if n != 0 {
				t.Errorf("linhas = %d, want 0", n)
			}
		})
	}
}

// TestSolicitarPromocao_JaHaPendente prova a linha "Solicitar, já há pendente":
// segunda chamada -> ErrSolicitacaoPendenteExiste, sem linha nova.
func TestSolicitarPromocao_JaHaPendente(t *testing.T) {
	db := testDB(t)
	id := semearConta(t, db, "Repetida", "repete-solicita@empresa.com", PapelUsuario, 1)

	if _, err := SolicitarPromocao(db, id, PapelUsuario); err != nil {
		t.Fatalf("primeira SolicitarPromocao falhou: %v", err)
	}
	_, err := SolicitarPromocao(db, id, PapelUsuario)
	if !errors.Is(err, ErrSolicitacaoPendenteExiste) {
		t.Fatalf("erro = %v, want ErrSolicitacaoPendenteExiste", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM solicitacoes_promocao WHERE solicitante_id = $1`, id).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("linhas = %d, want 1", n)
	}
}

// TestSolicitarPromocao_AposRejeicao prova a linha "Solicitar após rejeição":
// uma solicitação anterior `rejeitada` não bloqueia a nova (sem espera).
func TestSolicitarPromocao_AposRejeicao(t *testing.T) {
	db := testDB(t)
	solicitante := semearConta(t, db, "Recusada", "apos-rejeicao@empresa.com", PapelUsuario, 1)
	decisor := semearConta(t, db, "Gestor", "gestor-rejeitou@empresa.com", PapelGestor, 2)
	inserirSolicitacao(t, db, solicitante, PapelAlmoxarife, "rejeitada", decisor)

	s, err := SolicitarPromocao(db, solicitante, PapelUsuario)
	if err != nil {
		t.Fatalf("SolicitarPromocao após rejeição: %v", err)
	}
	if s.Status != "pendente" || s.PapelAlvo != PapelAlmoxarife {
		t.Errorf("nova solicitação = %+v, want pendente/almoxarife", s)
	}
}

// TestSolicitarPromocao_CorridaIndicePartial prova o backstop do índice único
// parcial: duas chamadas concorrentes só podem criar uma pendente; a perdedora
// recebe ErrSolicitacaoPendenteExiste (via `23505`). Mesmo padrão de
// TestVerificarEmail_Concorrente.
func TestSolicitarPromocao_CorridaIndicePartial(t *testing.T) {
	db := testDB(t)
	id := semearConta(t, db, "Corrida", "corrida-solicita@empresa.com", PapelUsuario, 1)

	const n = 2
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = SolicitarPromocao(db, id, PapelUsuario)
		}(i)
	}
	close(start)
	wg.Wait()

	var ok, conflito int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrSolicitacaoPendenteExiste):
			conflito++
		default:
			t.Fatalf("erro inesperado na corrida: %v", err)
		}
	}
	if ok != 1 || conflito != n-1 {
		t.Errorf("ok=%d conflito=%d, want ok=1 conflito=%d", ok, conflito, n-1)
	}
	var linhas int
	if err := db.QueryRow(`SELECT count(*) FROM solicitacoes_promocao WHERE solicitante_id = $1`, id).Scan(&linhas); err != nil {
		t.Fatalf("count: %v", err)
	}
	if linhas != 1 {
		t.Errorf("linhas = %d, want 1", linhas)
	}
}

// --- BuscarMinhaSolicitacao --------------------------------------------

// TestBuscarMinhaSolicitacao_MaisRecenteOuNil prova as linhas "Minha
// solicitação, existe" e "nunca pediu" da I/O Matrix.
func TestBuscarMinhaSolicitacao_MaisRecenteOuNil(t *testing.T) {
	db := testDB(t)

	semNada := semearConta(t, db, "Sem histórico", "sem-historico@empresa.com", PapelUsuario, 1)
	got, err := BuscarMinhaSolicitacao(db, semNada)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got != nil {
		t.Errorf("got = %+v, want nil", got)
	}

	comHist := semearConta(t, db, "Com histórico", "com-historico@empresa.com", PapelUsuario, 2)
	decisor := semearConta(t, db, "Gestor hist", "gestor-hist@empresa.com", PapelGestor, 3)
	inserirSolicitacao(t, db, comHist, PapelAlmoxarife, "rejeitada", decisor)
	// A mais recente: uma pendente inserida depois.
	pendenteID := inserirSolicitacao(t, db, comHist, PapelAlmoxarife, "pendente", "")

	got, err = BuscarMinhaSolicitacao(db, comHist)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got == nil {
		t.Fatal("got = nil, want a solicitação mais recente")
	}
	if got.ID != pendenteID {
		t.Errorf("ID = %q, want %q (a mais recente)", got.ID, pendenteID)
	}
	if got.Status != "pendente" {
		t.Errorf("Status = %q, want pendente", got.Status)
	}
	if got.DecididoEm != nil {
		t.Errorf("DecididoEm = %v, want nil enquanto pendente", got.DecididoEm)
	}
}

// TestBuscarMinhaSolicitacao_DecididoEmPreenchido prova que uma solicitação já
// decidida traz `decidido_em` não-nil.
func TestBuscarMinhaSolicitacao_DecididoEmPreenchido(t *testing.T) {
	db := testDB(t)
	solicitante := semearConta(t, db, "Decidida", "decidida-minha@empresa.com", PapelAlmoxarife, 1)
	decisor := semearConta(t, db, "Adm dec", "adm-dec@empresa.com", PapelAdm, 2)
	inserirSolicitacao(t, db, solicitante, PapelGestor, "aprovada", decisor)

	got, err := BuscarMinhaSolicitacao(db, solicitante)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got == nil || got.DecididoEm == nil {
		t.Fatalf("got = %+v, want DecididoEm preenchido", got)
	}
	if got.Status != "aprovada" {
		t.Errorf("Status = %q, want aprovada", got.Status)
	}
}

// --- ListarSolicitacoesPendentes -------------------------------------

// TestListarSolicitacoesPendentes_RecorteGestorVsAdm prova as linhas "Fila,
// decisor gestor" e "Fila, decisor adm": gestor só vê alvo `almoxarife`; adm
// vê tudo; só `pendente`; ordenado por `criado_em, id`.
func TestListarSolicitacoesPendentes_RecorteGestorVsAdm(t *testing.T) {
	db := testDB(t)

	u1 := semearConta(t, db, "U1", "u1-fila@empresa.com", PapelUsuario, 1)
	u2 := semearConta(t, db, "U2", "u2-fila@empresa.com", PapelUsuario, 2)
	a1 := semearConta(t, db, "A1", "a1-fila@empresa.com", PapelAlmoxarife, 3)
	decisor := semearConta(t, db, "Adm fila", "adm-fila@empresa.com", PapelAdm, 4)

	inserirSolicitacao(t, db, u1, PapelAlmoxarife, "pendente", "")
	inserirSolicitacao(t, db, u2, PapelAlmoxarife, "pendente", "")
	inserirSolicitacao(t, db, a1, PapelGestor, "pendente", "")
	// Ruído que nunca deve aparecer: uma já rejeitada.
	u3 := semearConta(t, db, "U3", "u3-fila@empresa.com", PapelUsuario, 5)
	inserirSolicitacao(t, db, u3, PapelAlmoxarife, "rejeitada", decisor)

	t.Run("gestor vê só alvo almoxarife", func(t *testing.T) {
		lista, err := ListarSolicitacoesPendentes(db, PapelGestor)
		if err != nil {
			t.Fatalf("erro: %v", err)
		}
		if len(lista) != 2 {
			t.Fatalf("len = %d, want 2 (%+v)", len(lista), lista)
		}
		for _, p := range lista {
			if p.PapelAlvo != PapelAlmoxarife {
				t.Errorf("gestor recebeu alvo %q — fora do escopo", p.PapelAlvo)
			}
			if p.SolicitanteNome == "" || p.SolicitanteEmail == "" || p.PapelAtual == "" {
				t.Errorf("dados do solicitante não resolvidos pelo JOIN: %+v", p)
			}
		}
	})

	t.Run("adm vê todas as pendentes", func(t *testing.T) {
		lista, err := ListarSolicitacoesPendentes(db, PapelAdm)
		if err != nil {
			t.Fatalf("erro: %v", err)
		}
		if len(lista) != 3 {
			t.Fatalf("len = %d, want 3 (%+v)", len(lista), lista)
		}
		alvos := map[string]int{}
		for _, p := range lista {
			alvos[p.PapelAlvo]++
		}
		if alvos[PapelAlmoxarife] != 2 || alvos[PapelGestor] != 1 {
			t.Errorf("distribuição de alvos = %v, want almoxarife:2 gestor:1", alvos)
		}
	})
}

// TestListarSolicitacoesPendentes_ListaVazia prova que ausência de pendentes
// devolve slice vazio, nunca nil/erro.
func TestListarSolicitacoesPendentes_ListaVazia(t *testing.T) {
	db := testDB(t)
	lista, err := ListarSolicitacoesPendentes(db, PapelAdm)
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

// --- DecidirSolicitacao ----------------------------------------------

// TestDecidirSolicitacao_AprovarTrocaPapelEAuditoria prova a linha "Aprovar
// promoção a almoxarife por gestor": papel do solicitante vira `almoxarife`,
// solicitação `aprovada` com decidido_por/decidido_em na mesma transação.
func TestDecidirSolicitacao_AprovarTrocaPapelEAuditoria(t *testing.T) {
	db := testDB(t)
	solicitante := semearConta(t, db, "Promovido", "promovido@empresa.com", PapelUsuario, 1)
	decisor := semearConta(t, db, "Gestor aprova", "gestor-aprova@empresa.com", PapelGestor, 2)
	solID := inserirSolicitacao(t, db, solicitante, PapelAlmoxarife, "pendente", "")

	s, err := DecidirSolicitacao(db, solID, decisor, PapelGestor, true)
	if err != nil {
		t.Fatalf("DecidirSolicitacao erro inesperado: %v", err)
	}
	if s.Status != "aprovada" || s.PapelAlvo != PapelAlmoxarife {
		t.Errorf("resposta = %+v, want aprovada/almoxarife", s)
	}
	if s.DecididoEm == nil {
		t.Error("DecididoEm = nil, want preenchido")
	}
	if got := papelDaConta(t, db, solicitante); got != PapelAlmoxarife {
		t.Errorf("papel do solicitante = %q, want almoxarife", got)
	}
	var decididoPor sql.NullString
	var decididoEm sql.NullTime
	if err := db.QueryRow(`SELECT decidido_por, decidido_em FROM solicitacoes_promocao WHERE id = $1`, solID).
		Scan(&decididoPor, &decididoEm); err != nil {
		t.Fatalf("reler solicitação: %v", err)
	}
	if !decididoPor.Valid || decididoPor.String != decisor {
		t.Errorf("decidido_por = %v, want %q", decididoPor, decisor)
	}
	if !decididoEm.Valid {
		t.Error("decidido_em nulo após aprovação")
	}
}

// TestDecidirSolicitacao_AprovarAlvoGestorPorAdm prova a linha "Aprovar
// promoção a gestor por adm".
func TestDecidirSolicitacao_AprovarAlvoGestorPorAdm(t *testing.T) {
	db := testDB(t)
	solicitante := semearConta(t, db, "Vai a gestor", "vai-gestor@empresa.com", PapelAlmoxarife, 1)
	decisor := semearConta(t, db, "Adm aprova", "adm-aprova@empresa.com", PapelAdm, 2)
	solID := inserirSolicitacao(t, db, solicitante, PapelGestor, "pendente", "")

	s, err := DecidirSolicitacao(db, solID, decisor, PapelAdm, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if s.Status != "aprovada" {
		t.Errorf("Status = %q, want aprovada", s.Status)
	}
	if got := papelDaConta(t, db, solicitante); got != PapelGestor {
		t.Errorf("papel do solicitante = %q, want gestor", got)
	}
}

// TestDecidirSolicitacao_AlvoGestorPorNaoAdm prova a linha "Decidir promoção a
// gestor por não-adm": ErrDecisaoNaoAutorizada, nada muda.
func TestDecidirSolicitacao_AlvoGestorPorNaoAdm(t *testing.T) {
	db := testDB(t)
	solicitante := semearConta(t, db, "Alvo gestor", "alvo-gestor@empresa.com", PapelAlmoxarife, 1)
	decisor := semearConta(t, db, "Gestor nao-adm", "gestor-naoadm@empresa.com", PapelGestor, 2)
	solID := inserirSolicitacao(t, db, solicitante, PapelGestor, "pendente", "")

	_, err := DecidirSolicitacao(db, solID, decisor, PapelGestor, true)
	if !errors.Is(err, ErrDecisaoNaoAutorizada) {
		t.Fatalf("erro = %v, want ErrDecisaoNaoAutorizada", err)
	}
	if got := papelDaConta(t, db, solicitante); got != PapelAlmoxarife {
		t.Errorf("papel do solicitante mudou para %q — não deveria", got)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM solicitacoes_promocao WHERE id = $1`, solID).Scan(&status); err != nil {
		t.Fatalf("reler status: %v", err)
	}
	if status != "pendente" {
		t.Errorf("status = %q, want pendente (intacto)", status)
	}
}

// TestDecidirSolicitacao_Rejeitar prova a linha "Rejeitar": solicitação
// `rejeitada` + auditoria; `usuarios` intacto.
func TestDecidirSolicitacao_Rejeitar(t *testing.T) {
	db := testDB(t)
	solicitante := semearConta(t, db, "Recusado", "recusado-dec@empresa.com", PapelUsuario, 1)
	decisor := semearConta(t, db, "Gestor recusa", "gestor-recusa@empresa.com", PapelGestor, 2)
	solID := inserirSolicitacao(t, db, solicitante, PapelAlmoxarife, "pendente", "")

	s, err := DecidirSolicitacao(db, solID, decisor, PapelGestor, false)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if s.Status != "rejeitada" {
		t.Errorf("Status = %q, want rejeitada", s.Status)
	}
	if s.DecididoEm == nil {
		t.Error("DecididoEm = nil, want preenchido")
	}
	if got := papelDaConta(t, db, solicitante); got != PapelUsuario {
		t.Errorf("papel do solicitante = %q, want usuario (intacto)", got)
	}
}

// TestDecidirSolicitacao_Inexistente prova a linha "Decidir solicitação
// inexistente": uuid válido sem linha -> ErrSolicitacaoNaoEncontrada.
func TestDecidirSolicitacao_Inexistente(t *testing.T) {
	db := testDB(t)
	decisor := semearConta(t, db, "Gestor 404", "gestor-404@empresa.com", PapelGestor, 1)

	_, err := DecidirSolicitacao(db, "00000000-0000-0000-0000-000000000000", decisor, PapelGestor, true)
	if !errors.Is(err, ErrSolicitacaoNaoEncontrada) {
		t.Fatalf("erro = %v, want ErrSolicitacaoNaoEncontrada", err)
	}
}

// TestDecidirSolicitacao_IDMalformado prova a linha "Decidir com id
// malformado": `pq` 22P02 tratado como ErrSolicitacaoNaoEncontrada.
func TestDecidirSolicitacao_IDMalformado(t *testing.T) {
	db := testDB(t)
	decisor := semearConta(t, db, "Gestor malformado", "gestor-malformado@empresa.com", PapelGestor, 1)

	_, err := DecidirSolicitacao(db, "nao-e-um-uuid", decisor, PapelGestor, true)
	if !errors.Is(err, ErrSolicitacaoNaoEncontrada) {
		t.Fatalf("erro = %v, want ErrSolicitacaoNaoEncontrada", err)
	}
}

// TestDecidirSolicitacao_JaDecidida prova a linha "Decidir solicitação já
// decidida": segunda decisão -> ErrSolicitacaoNaoPendente.
func TestDecidirSolicitacao_JaDecidida(t *testing.T) {
	db := testDB(t)
	solicitante := semearConta(t, db, "Duas vezes", "duas-vezes@empresa.com", PapelUsuario, 1)
	decisor := semearConta(t, db, "Gestor 2x", "gestor-2x@empresa.com", PapelGestor, 2)
	solID := inserirSolicitacao(t, db, solicitante, PapelAlmoxarife, "pendente", "")

	if _, err := DecidirSolicitacao(db, solID, decisor, PapelGestor, true); err != nil {
		t.Fatalf("primeira decisão falhou: %v", err)
	}
	_, err := DecidirSolicitacao(db, solID, decisor, PapelGestor, false)
	if !errors.Is(err, ErrSolicitacaoNaoPendente) {
		t.Fatalf("erro = %v, want ErrSolicitacaoNaoPendente", err)
	}
}

// TestDecidirSolicitacao_PapelDoSolicitanteMudou prova a linha "Decidir, papel
// do solicitante mudou": alvo `almoxarife` mas o solicitante já é `gestor` ->
// ErrEstadoContaMudou; solicitação continua `pendente`.
func TestDecidirSolicitacao_PapelDoSolicitanteMudou(t *testing.T) {
	db := testDB(t)
	solicitante := semearConta(t, db, "Já subiu", "ja-subiu@empresa.com", PapelUsuario, 1)
	decisor := semearConta(t, db, "Gestor mudou", "gestor-mudou@empresa.com", PapelGestor, 2)
	solID := inserirSolicitacao(t, db, solicitante, PapelAlmoxarife, "pendente", "")

	// O papel do solicitante mudou por outro caminho desde a solicitação.
	if _, err := db.Exec(`UPDATE usuarios SET papel = $1 WHERE id = $2`, PapelGestor, solicitante); err != nil {
		t.Fatalf("forçar mudança de papel: %v", err)
	}

	_, err := DecidirSolicitacao(db, solID, decisor, PapelGestor, true)
	if !errors.Is(err, ErrEstadoContaMudou) {
		t.Fatalf("erro = %v, want ErrEstadoContaMudou", err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM solicitacoes_promocao WHERE id = $1`, solID).Scan(&status); err != nil {
		t.Fatalf("reler status: %v", err)
	}
	if status != "pendente" {
		t.Errorf("status = %q, want pendente", status)
	}
}

// TestDecidirSolicitacao_CorridaEntreDoisDecisores prova o fecho da janela
// SELECT->UPDATE guardado (`WHERE id=$1 AND status='pendente'`, molde de
// VerificarEmail): duas decisões concorrentes de REJEIÇÃO sobre a MESMA linha
// (sem UPDATE em usuarios, para isolar exatamente esse guard) só têm uma
// vencedora; a perdedora recebe ErrSolicitacaoNaoPendente.
func TestDecidirSolicitacao_CorridaEntreDoisDecisores(t *testing.T) {
	db := testDB(t)
	solicitante := semearConta(t, db, "Disputado", "disputado@empresa.com", PapelUsuario, 1)
	d1 := semearConta(t, db, "Gestor A", "gestor-a@empresa.com", PapelGestor, 2)
	d2 := semearConta(t, db, "Gestor B", "gestor-b@empresa.com", PapelGestor, 3)
	solID := inserirSolicitacao(t, db, solicitante, PapelAlmoxarife, "pendente", "")

	const n = 2
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)
	decisores := []string{d1, d2}
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = DecidirSolicitacao(db, solID, decisores[i], PapelGestor, false)
		}(i)
	}
	close(start)
	wg.Wait()

	var ok, conflito int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrSolicitacaoNaoPendente):
			conflito++
		default:
			t.Fatalf("erro inesperado na corrida: %v", err)
		}
	}
	if ok != 1 || conflito != n-1 {
		t.Errorf("ok=%d conflito=%d, want ok=1 conflito=%d", ok, conflito, n-1)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM solicitacoes_promocao WHERE id = $1`, solID).Scan(&status); err != nil {
		t.Fatalf("reler status: %v", err)
	}
	if status != "rejeitada" {
		t.Errorf("status = %q, want rejeitada", status)
	}
}

// TestDecidirSolicitacao_CorridaAprovacaoConcorrente prova que, mesmo na
// aprovação concorrente (com o UPDATE em usuarios no meio), só uma decisão
// vence; a perdedora recebe um erro da classe 409 (ErrSolicitacaoNaoPendente
// OU ErrEstadoContaMudou — a serialização do lock de linha em `usuarios`
// determina qual), nunca sucesso duplo.
func TestDecidirSolicitacao_CorridaAprovacaoConcorrente(t *testing.T) {
	db := testDB(t)
	solicitante := semearConta(t, db, "Aprovado 2x", "aprovado-2x@empresa.com", PapelUsuario, 1)
	d1 := semearConta(t, db, "Gestor X", "gestor-x@empresa.com", PapelGestor, 2)
	d2 := semearConta(t, db, "Gestor Y", "gestor-y@empresa.com", PapelGestor, 3)
	solID := inserirSolicitacao(t, db, solicitante, PapelAlmoxarife, "pendente", "")

	const n = 2
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)
	decisores := []string{d1, d2}
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = DecidirSolicitacao(db, solID, decisores[i], PapelGestor, true)
		}(i)
	}
	close(start)
	wg.Wait()

	var ok, conflito int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrSolicitacaoNaoPendente), errors.Is(err, ErrEstadoContaMudou):
			conflito++
		default:
			t.Fatalf("erro inesperado na corrida: %v", err)
		}
	}
	if ok != 1 || conflito != n-1 {
		t.Errorf("ok=%d conflito=%d, want ok=1 conflito=%d", ok, conflito, n-1)
	}
	if got := papelDaConta(t, db, solicitante); got != PapelAlmoxarife {
		t.Errorf("papel do solicitante = %q, want almoxarife", got)
	}
}
