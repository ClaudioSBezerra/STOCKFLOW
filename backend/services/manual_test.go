package services

import (
	"strings"
	"testing"
)

// TestSecoesManualParaPapel_FiltraPorRank prova que cada papel recebe
// exatamente as seções cujo PapelMinimo ele alcança (RankPapel) — a MESMA
// regra de filtrarNavPorPapel no frontend.
func TestSecoesManualParaPapel_FiltraPorRank(t *testing.T) {
	usuario := SecoesManualParaPapel(PapelUsuario)
	almoxarife := SecoesManualParaPapel(PapelAlmoxarife)
	gestor := SecoesManualParaPapel(PapelGestor)
	adm := SecoesManualParaPapel(PapelAdm)

	if len(usuario) == 0 {
		t.Fatal("usuario não recebeu nenhuma seção")
	}
	for _, s := range usuario {
		if s.PapelMinimo != PapelUsuario {
			t.Errorf("usuario recebeu seção %q com papelMinimo=%q", s.ID, s.PapelMinimo)
		}
	}

	if len(almoxarife) <= len(usuario) {
		t.Errorf("almoxarife (%d) deveria ver mais seções que usuario (%d)", len(almoxarife), len(usuario))
	}
	if len(gestor) <= len(almoxarife) {
		t.Errorf("gestor (%d) deveria ver mais seções que almoxarife (%d)", len(gestor), len(almoxarife))
	}
	if len(adm) <= len(gestor) {
		t.Errorf("adm (%d) deveria ver mais seções que gestor (%d)", len(adm), len(gestor))
	}
	if len(adm) != len(secoesManual) {
		t.Errorf("adm recebeu %d seções, want o manual completo (%d)", len(adm), len(secoesManual))
	}

	// Toda seção visível para almoxarife também precisa estar visível para
	// gestor e adm (ordem total: nenhum papel "perde" uma seção ao subir).
	idsAlmoxarife := map[string]bool{}
	for _, s := range almoxarife {
		idsAlmoxarife[s.ID] = true
	}
	idsGestor := map[string]bool{}
	for _, s := range gestor {
		idsGestor[s.ID] = true
	}
	for id := range idsAlmoxarife {
		if !idsGestor[id] {
			t.Errorf("seção %q visível para almoxarife mas não para gestor", id)
		}
	}
}

// TestSecoesManualParaPapel_PapelDesconhecidoNaoVeNada prova o fail-closed:
// um papel fora da hierarquia (rank 0) não recebe nenhuma seção, mesmo as
// de PapelMinimo=usuario.
func TestSecoesManualParaPapel_PapelDesconhecidoNaoVeNada(t *testing.T) {
	if s := SecoesManualParaPapel("inexistente"); len(s) != 0 {
		t.Errorf("len = %d, want 0", len(s))
	}
	if s := SecoesManualParaPapel(""); len(s) != 0 {
		t.Errorf("len = %d, want 0", len(s))
	}
}

// TestSecoesManual_SemIDDuplicado prova que cada seção tem um ID único —
// o frontend usa o ID como key de lista/âncora de navegação.
func TestSecoesManual_SemIDDuplicado(t *testing.T) {
	vistos := map[string]bool{}
	for _, s := range secoesManual {
		if vistos[s.ID] {
			t.Errorf("ID duplicado: %q", s.ID)
		}
		vistos[s.ID] = true
		if s.Titulo == "" {
			t.Errorf("seção %q sem título", s.ID)
		}
		if len(s.Paragrafos) == 0 {
			t.Errorf("seção %q sem parágrafo nenhum", s.ID)
		}
	}
}

func TestQuebrarEmLinhas(t *testing.T) {
	casos := []struct {
		nome   string
		texto  string
		max    int
		quants int // número de linhas esperado
	}{
		{"texto curto cabe numa linha", "Uma frase curta.", 95, 1},
		{"vazio produz zero linhas", "", 95, 0},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			linhas := quebrarEmLinhas(c.texto, c.max)
			if len(linhas) != c.quants {
				t.Errorf("len = %d, want %d (linhas=%v)", len(linhas), c.quants, linhas)
			}
		})
	}

	// Texto longo precisa ser quebrado em mais de uma linha, nenhuma linha
	// excede o máximo, e nenhuma palavra é cortada no meio (reconstruído com
	// espaço, o resultado bate com o texto original normalizado).
	texto := strings.Repeat("palavra ", 50) + "final"
	linhas := quebrarEmLinhas(texto, 40)
	if len(linhas) < 2 {
		t.Fatalf("texto longo gerou só %d linha(s), want >= 2", len(linhas))
	}
	for _, l := range linhas {
		if len([]rune(l)) > 40 {
			t.Errorf("linha excede o máximo: %q (%d runas)", l, len([]rune(l)))
		}
	}
	reconstruido := strings.Join(linhas, " ")
	if reconstruido != strings.TrimSpace(texto) {
		t.Errorf("reconstruído = %q, want %q", reconstruido, strings.TrimSpace(texto))
	}
}

func TestRenderizarManualPDF_GeraBytesValidosEDeterministico(t *testing.T) {
	secoes := SecoesManualParaPapel(PapelAdm)
	pdf1, err := RenderizarManualPDF(secoes)
	if err != nil {
		t.Fatalf("RenderizarManualPDF erro: %v", err)
	}
	if len(pdf1) == 0 {
		t.Fatal("PDF vazio")
	}
	if !strings.HasPrefix(string(pdf1[:5]), "%PDF-") {
		t.Errorf("início do arquivo = %q, want prefixo %%PDF-", pdf1[:5])
	}

	pdf2, err := RenderizarManualPDF(secoes)
	if err != nil {
		t.Fatalf("segunda chamada: erro: %v", err)
	}
	if string(pdf1) != string(pdf2) {
		t.Error("RenderizarManualPDF não é determinístico: duas chamadas com a mesma entrada produziram bytes diferentes")
	}
}

func TestGerarManualPDF_SempreCompleto(t *testing.T) {
	pdf, err := GerarManualPDF()
	if err != nil {
		t.Fatalf("GerarManualPDF erro: %v", err)
	}
	pdfDoAdm, err := RenderizarManualPDF(SecoesManualParaPapel(PapelAdm))
	if err != nil {
		t.Fatalf("RenderizarManualPDF(adm) erro: %v", err)
	}
	if string(pdf) != string(pdfDoAdm) {
		t.Error("GerarManualPDF deveria ser sempre o manual completo (mesmo conteúdo do papel adm)")
	}
}
