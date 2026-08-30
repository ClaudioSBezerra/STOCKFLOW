package services

import "testing"

// TestRankPapel_OrdemTotal prova a AC2: a hierarquia
// `adm=4 > gestor=3 > almoxarife=2 > usuario=1` é uma ordem total codificada
// uma única vez aqui — tabela pura, sem banco.
func TestRankPapel_OrdemTotal(t *testing.T) {
	casos := []struct {
		papel string
		want  int
	}{
		{PapelUsuario, 1},
		{PapelAlmoxarife, 2},
		{PapelGestor, 3},
		{PapelAdm, 4},
	}
	for _, c := range casos {
		if got := RankPapel(c.papel); got != c.want {
			t.Errorf("RankPapel(%q) = %d, want %d", c.papel, got, c.want)
		}
	}

	// A ordem total precisa ser estritamente crescente na sequência
	// documentada — nenhum par empatado, nenhuma inversão.
	if !(RankPapel(PapelUsuario) < RankPapel(PapelAlmoxarife) &&
		RankPapel(PapelAlmoxarife) < RankPapel(PapelGestor) &&
		RankPapel(PapelGestor) < RankPapel(PapelAdm)) {
		t.Errorf("hierarquia não é estritamente crescente: usuario=%d almoxarife=%d gestor=%d adm=%d",
			RankPapel(PapelUsuario), RankPapel(PapelAlmoxarife), RankPapel(PapelGestor), RankPapel(PapelAdm))
	}
}

// TestRankPapel_DesconhecidoEhZero prova o caso defensivo da I/O Matrix:
// string vazia ou valor fora do enum -> rank 0, sempre abaixo de qualquer
// mínimo exigível.
func TestRankPapel_DesconhecidoEhZero(t *testing.T) {
	for _, papel := range []string{"", "root", "superadmin", "USUARIO", "Adm", "  adm  "} {
		if got := RankPapel(papel); got != 0 {
			t.Errorf("RankPapel(%q) = %d, want 0", papel, got)
		}
	}

	// Um papel desconhecido nunca pode alcançar o menor papel real.
	if RankPapel("desconhecido") >= RankPapel(PapelUsuario) {
		t.Error("papel desconhecido alcançou o mínimo 'usuario' — deveria ficar abaixo")
	}
}
