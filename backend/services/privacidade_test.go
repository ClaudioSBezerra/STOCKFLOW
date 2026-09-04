// Testes de ExportarDadosUsuario — Story 8.1 (Epic 8, Privacidade/LGPD),
// spec-8-1.
package services

import (
	"testing"
	"time"
)

// TestExportarDadosUsuario_ComHistoricoCompleto cobre "Happy path com
// histórico" da I/O Matrix: usuário com log de acesso, Movimentação e
// Pedido próprios recebe as 3 listas preenchidas e o nome/email exatamente
// como passados (a função nunca os relê do banco).
func TestExportarDadosUsuario_ComHistoricoCompleto(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Exportar Completo", "exportar-completo@empresa.com", PapelUsuario, 0)

	inserirLogAcessoDireto(t, db, &usuarioID, "exportar-completo@empresa.com", "senha", true, "10.0.0.1", time.Now())

	produtoID, estoqueID, _ := seedProdutoComSaldo(t, db, "Canteiro Exportar Completo", 20)
	if _, err := RegistrarBaixa(db, produtoID, estoqueID, usuarioID, 2); err != nil {
		t.Fatalf("seed RegistrarBaixa: %v", err)
	}

	pedido := seedPedidoComItem(t, db, usuarioID, "Exportar Completo", 3)

	dados, err := ExportarDadosUsuario(db, usuarioID, "Exportar Completo", "exportar-completo@empresa.com")
	if err != nil {
		t.Fatalf("ExportarDadosUsuario: %v", err)
	}
	if dados.Nome != "Exportar Completo" {
		t.Errorf("Nome = %q, want %q", dados.Nome, "Exportar Completo")
	}
	if dados.Email != "exportar-completo@empresa.com" {
		t.Errorf("Email = %q, want %q", dados.Email, "exportar-completo@empresa.com")
	}
	if len(dados.LogAcesso) != 1 {
		t.Fatalf("len(LogAcesso) = %d, want 1", len(dados.LogAcesso))
	}
	if len(dados.Movimentacoes) != 1 {
		t.Fatalf("len(Movimentacoes) = %d, want 1", len(dados.Movimentacoes))
	}
	if len(dados.Pedidos) != 1 || dados.Pedidos[0].ID != pedido.ID {
		t.Fatalf("Pedidos = %+v, want 1 pedido com ID %q", dados.Pedidos, pedido.ID)
	}
}

// TestExportarDadosUsuario_SemNenhumRegistro cobre "Usuário sem
// Movimentação/Pedido/log próprio" da I/O Matrix: mesmo formato, as 3
// listas vêm como slice vazio não-nil, nunca nil/omitido, sem erro.
func TestExportarDadosUsuario_SemNenhumRegistro(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	usuarioID := semearConta(t, db, "Exportar Vazio", "exportar-vazio@empresa.com", PapelUsuario, 0)

	dados, err := ExportarDadosUsuario(db, usuarioID, "Exportar Vazio", "exportar-vazio@empresa.com")
	if err != nil {
		t.Fatalf("ExportarDadosUsuario: %v", err)
	}
	if dados.LogAcesso == nil || len(dados.LogAcesso) != 0 {
		t.Errorf("LogAcesso = %v, want slice vazio não-nil", dados.LogAcesso)
	}
	if dados.Movimentacoes == nil || len(dados.Movimentacoes) != 0 {
		t.Errorf("Movimentacoes = %v, want slice vazio não-nil", dados.Movimentacoes)
	}
	if dados.Pedidos == nil || len(dados.Pedidos) != 0 {
		t.Errorf("Pedidos = %v, want slice vazio não-nil", dados.Pedidos)
	}
}

// TestExportarDadosUsuario_ErroDeQueryPropagado cobre "Falha ao consultar o
// banco em qualquer uma das 3 fontes" da I/O Matrix: com a conexão fechada,
// o erro da primeira consulta (log de acesso) é propagado tal qual, sem
// payload parcial.
func TestExportarDadosUsuario_ErroDeQueryPropagado(t *testing.T) {
	db := testDB(t)
	db.Close()

	dados, err := ExportarDadosUsuario(db, "qualquer-id", "Nome", "email@empresa.com")
	if err == nil {
		t.Fatal("ExportarDadosUsuario: erro esperado com a conexão fechada")
	}
	if dados.Nome != "" || len(dados.LogAcesso) != 0 || len(dados.Movimentacoes) != 0 || len(dados.Pedidos) != 0 {
		t.Errorf("payload parcial devolvido junto com o erro: %+v", dados)
	}
}
