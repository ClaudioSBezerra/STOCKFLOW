// Package services, arquivo privacidade.go: Exportação dos próprios dados
// pessoais — Story 8.1 (Epic 8, Privacidade/LGPD), spec-8-1. A LGPD exige que
// qualquer Usuário consiga baixar os próprios dados pessoais; esta story
// compõe identidade da sessão + as três fontes já-escopadas ao usuário
// chamador (log de acesso, Movimentações, Pedidos) num único payload,
// consumido por GET /api/usuarios/me/exportar-dados (handlers/privacidade.go).
package services

import "database/sql"

// DadosPessoaisExportados é o payload devolvido por ExportarDadosUsuario —
// serializado tal qual como o corpo de GET /api/usuarios/me/exportar-dados
// (handlers/privacidade.go). `Nome`/`Email` vêm da sessão do chamador, nunca
// de uma consulta própria (o handler já os tem via
// middleware.UsuarioDaSessao). As três listas são SEMPRE arrays — vazios
// quando o usuário não tem registro naquela fonte, nunca `null` (Always,
// spec-8-1): ListarLogsAcessoDoUsuario, ListarMovimentacoesDoUsuario e
// ListarPedidosProprios já devolvem slices vazios não-nil, nunca nil, então
// nenhum tratamento extra é necessário aqui.
type DadosPessoaisExportados struct {
	Nome          string                  `json:"nome"`
	Email         string                  `json:"email"`
	LogAcesso     []LogAcesso             `json:"logAcesso"`
	Movimentacoes []MovimentacaoHistorico `json:"movimentacoes"`
	Pedidos       []PedidoResumo          `json:"pedidos"`
}

// ExportarDadosUsuario compõe o export LGPD de `usuarioID` (Story 8.1,
// spec-8-1): log de acesso (ListarLogsAcessoDoUsuario, logs_acesso.go),
// Movimentações (ListarMovimentacoesDoUsuario, movimentacoes.go) e Pedidos
// (ListarPedidosProprios com `filtroStatus=""` — reaproveitada sem
// alteração, já sem teto e já-escopada ao usuário, Story 7.3). `nome`/
// `email` são só compostos no payload de saída, nunca usados para
// consultar nada (o escopo das três fontes é inteiramente por
// `usuarioID`). O primeiro erro não-nil de qualquer uma das três consultas
// interrompe e propaga — nenhum payload parcial é devolvido (I/O Matrix,
// spec-8-1).
func ExportarDadosUsuario(db *sql.DB, usuarioID, nome, email string) (DadosPessoaisExportados, error) {
	logAcesso, err := ListarLogsAcessoDoUsuario(db, usuarioID)
	if err != nil {
		return DadosPessoaisExportados{}, err
	}

	movimentacoes, err := ListarMovimentacoesDoUsuario(db, usuarioID)
	if err != nil {
		return DadosPessoaisExportados{}, err
	}

	pedidos, err := ListarPedidosProprios(db, usuarioID, "")
	if err != nil {
		return DadosPessoaisExportados{}, err
	}

	return DadosPessoaisExportados{
		Nome:          nome,
		Email:         email,
		LogAcesso:     logAcesso,
		Movimentacoes: movimentacoes,
		Pedidos:       pedidos,
	}, nil
}
