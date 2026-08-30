package services

// Papéis do sistema (AD-8): a hierarquia de acesso é uma ordem total estrita
// `usuario < almoxarife < gestor < adm`, definida UMA única vez aqui, no
// backend. Nenhuma rota, handler ou middleware reimplementa a comparação de
// papel nem mantém uma allow-list de pares — todos consomem RankPapel.
const (
	PapelUsuario    = "usuario"
	PapelAlmoxarife = "almoxarife"
	PapelGestor     = "gestor"
	PapelAdm        = "adm"
)

// rankPapel é a tabela de ordem total (AD-8 forma 1): quanto maior o número,
// mais alto o papel na hierarquia. Um papel fora deste conjunto (string vazia
// ou valor inesperado) simplesmente não está no mapa e RankPapel devolve 0 —
// sempre abaixo de qualquer mínimo exigível.
var rankPapel = map[string]int{
	PapelUsuario:    1,
	PapelAlmoxarife: 2,
	PapelGestor:     3,
	PapelAdm:        4,
}

// RankPapel devolve a posição do papel na ordem total da hierarquia de acesso
// (AD-8). Papel desconhecido/vazio -> 0, garantidamente abaixo de qualquer
// mínimo. É a fonte única de verdade da comparação de papel no backend:
// `RankPapel(papel) >= RankPapel(minimo)` é a única forma de decidir se um
// papel alcança outro.
func RankPapel(papel string) int {
	return rankPapel[papel]
}
