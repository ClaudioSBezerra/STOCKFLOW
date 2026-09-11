package services

import (
	"bytes"
	"database/sql"
	"errors"
	"testing"
)

// Teste-critério do isolamento por Empresa — Story 9.1 (Epic 9,
// Multi-Empresa), spec-9-1, AC 3 e SM-7.
//
// Duas Empresas (`iso-alfa` e `iso-beta`) recebem dados DELIBERADAMENTE
// equivalentes: Estoque de mesmo nome, Produto de mesmo nome/código/dimensão,
// contas, Movimentação, Pedido, Log de Acesso, Solicitação de Promoção e
// Importação. Para CADA área do AC 3 este arquivo afirma as duas metades da
// fronteira:
//
//  1. leitura — a consulta feita sob uma Empresa nunca devolve linha da outra;
//  2. escrita — uma operação endereçada a um id da outra Empresa falha no
//     sentinela de "não encontrado" já existente (nunca 403, nunca revelando
//     a existência) e NÃO altera nenhuma linha.
//
// Nada aqui usa a Empresa padrão da suíte (`empresaTeste`): o ponto é provar
// que uma Empresa não enxerga a outra, então cada lado tem a sua.

// ambienteIsolamento é o conjunto de dados de UMA das duas Empresas.
type ambienteIsolamento struct {
	empresa     Empresa
	estoque     Estoque
	produto     Produto
	categoriaID string
	almoxarife  string
	adm         string
	comum       string
	pedido      Pedido
	importacao  string
	rotulo      string
}

// semearContaNaEmpresa insere uma conta ativa/verificada DENTRO de
// `empresaID`. Variante de semearConta (usuarios_test.go), que grava sempre
// na Empresa padrão da suíte.
func semearContaNaEmpresa(t *testing.T, db *sql.DB, empresaID, nome, email, papel string) string {
	t.Helper()
	var id string
	const insert = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, empresa_id)
		VALUES ($1, $2, 'hash-qualquer', $3, true, true, $4)
		RETURNING id`
	if err := db.QueryRow(insert, nome, email, papel, empresaID).Scan(&id); err != nil {
		t.Fatalf("semear conta %q: %v", email, err)
	}
	return id
}

// montarAmbienteIsolamento provisiona uma Empresa e a povoa com uma linha de
// cada área do AC 3. Os nomes/códigos são os MESMOS nas duas Empresas de
// propósito: se alguma unicidade tivesse continuado global, o segundo
// ambiente falharia aqui, no setup.
func montarAmbienteIsolamento(t *testing.T, db *sql.DB, slug, cnpjBase12, rotulo string) ambienteIsolamento {
	t.Helper()

	a := ambienteIsolamento{rotulo: rotulo}
	// `empresas` não é truncada entre testes: o segundo teste deste arquivo
	// reaproveita as Empresas criadas pelo primeiro (a resolução por slug é
	// a mesma que o middleware usa em produção).
	if existente, err := BuscarEmpresaPorSlug(db, slug); err == nil {
		a.empresa = existente
	} else {
		a.empresa = criarEmpresaDeTeste(t, db, slug, cnpjBase12, "Isolamento "+rotulo)
	}

	if err := db.QueryRow(
		`SELECT id FROM categorias WHERE codigo = $1 AND empresa_id = $2`, "04.001", a.empresa.ID,
	).Scan(&a.categoriaID); err != nil {
		t.Fatalf("%s: categoria da empresa: %v", rotulo, err)
	}

	estoque, err := CriarEstoque(db, a.empresa.ID, "Canteiro Isolamento")
	if err != nil {
		t.Fatalf("%s: CriarEstoque: %v", rotulo, err)
	}
	a.estoque = estoque

	produto, err := CriarProduto(db, a.empresa.ID, CriarProdutoInput{
		Nome:              "Prancha Isolamento",
		Codigo:            "SKU-ISOLAMENTO",
		CategoriaID:       a.categoriaID,
		EstoqueID:         a.estoque.ID,
		QuantidadeInicial: 20,
	})
	if err != nil {
		t.Fatalf("%s: CriarProduto: %v", rotulo, err)
	}
	a.produto = produto

	a.almoxarife = semearContaNaEmpresa(t, db, a.empresa.ID, "Almox "+rotulo, "almox-"+slug+"@empresa.com", PapelAlmoxarife)
	a.adm = semearContaNaEmpresa(t, db, a.empresa.ID, "Adm "+rotulo, "adm-"+slug+"@empresa.com", PapelAdm)
	a.comum = semearContaNaEmpresa(t, db, a.empresa.ID, "Comum "+rotulo, "comum-"+slug+"@empresa.com", PapelUsuario)

	if _, err := RegistrarBaixa(db, a.empresa.ID, a.produto.ID, a.estoque.ID, a.almoxarife, 1); err != nil {
		t.Fatalf("%s: RegistrarBaixa: %v", rotulo, err)
	}

	if err := RegistrarTentativaLogin(db, RegistroTentativaLogin{
		EmpresaID:      a.empresa.ID,
		UsuarioID:      &a.comum,
		EmailInformado: "comum-" + slug + "@empresa.com",
		Metodo:         "senha",
		IP:             "203.0.113.9",
		Sucesso:        true,
	}); err != nil {
		t.Fatalf("%s: RegistrarTentativaLogin: %v", rotulo, err)
	}

	if _, err := AdicionarItemCarrinho(db, a.empresa.ID, a.comum, a.produto.ID, a.estoque.ID, 2); err != nil {
		t.Fatalf("%s: AdicionarItemCarrinho: %v", rotulo, err)
	}
	pedido, err := SubmeterPedido(db, a.empresa.ID, a.comum, "Solicitante "+rotulo, "Obra "+rotulo, "")
	if err != nil {
		t.Fatalf("%s: SubmeterPedido: %v", rotulo, err)
	}
	a.pedido = pedido

	if _, err := SolicitarPromocao(db, a.empresa.ID, a.comum, PapelUsuario); err != nil {
		t.Fatalf("%s: SolicitarPromocao: %v", rotulo, err)
	}

	if err := db.QueryRow(
		`INSERT INTO importacoes (nome_arquivo, total_linhas, criado_por, empresa_id)
		 VALUES ($1, 1, $2, $3) RETURNING id`,
		"planilha-"+slug+".xlsx", a.almoxarife, a.empresa.ID,
	).Scan(&a.importacao); err != nil {
		t.Fatalf("%s: seed importacoes: %v", rotulo, err)
	}

	return a
}

// TestIsolamentoPorEmpresa_LeituraNuncaCruza percorre TODAS as áreas do AC 3
// e afirma, área por área, que a consulta de uma Empresa não devolve nenhuma
// linha da outra.
func TestIsolamentoPorEmpresa_LeituraNuncaCruza(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	alfa := montarAmbienteIsolamento(t, db, "iso-alfa", "334455660001", "Alfa")
	beta := montarAmbienteIsolamento(t, db, "iso-beta", "445566770001", "Beta")

	t.Run("catálogo em grade", func(t *testing.T) {
		itens, pag, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: alfa.empresa.ID})
		if err != nil {
			t.Fatalf("ListarCatalogoGrade: %v", err)
		}
		if pag.Total != 1 || len(itens) != 1 {
			t.Fatalf("total = %d, len = %d, want 1/1 (só o Produto da Alfa)", pag.Total, len(itens))
		}
		if itens[0].ID != alfa.produto.ID {
			t.Errorf("item = %q, want %q", itens[0].ID, alfa.produto.ID)
		}
	})

	t.Run("catálogo agrupado", func(t *testing.T) {
		grupos, pag, err := ListarCatalogoAgrupado(db, 1, FiltrosCatalogo{EmpresaID: beta.empresa.ID})
		if err != nil {
			t.Fatalf("ListarCatalogoAgrupado: %v", err)
		}
		if pag.Total != 1 || len(grupos) != 1 {
			t.Fatalf("total = %d, len = %d, want 1/1 — grupo nunca mistura Empresas", pag.Total, len(grupos))
		}
	})

	t.Run("busca por texto", func(t *testing.T) {
		itens, _, err := ListarCatalogoGrade(db, 1, FiltrosCatalogo{EmpresaID: alfa.empresa.ID, Q: "Prancha"})
		if err != nil {
			t.Fatalf("ListarCatalogoGrade(q): %v", err)
		}
		if len(itens) != 1 || itens[0].ID != alfa.produto.ID {
			t.Fatalf("itens = %v, want só o Produto da Alfa", itens)
		}
	})

	t.Run("exportação XLSX", func(t *testing.T) {
		dados, err := GerarCatalogoXLSX(db, FiltrosCatalogo{EmpresaID: alfa.empresa.ID})
		if err != nil {
			t.Fatalf("GerarCatalogoXLSX: %v", err)
		}
		// A planilha é um zip: o id do Produto da Beta não pode aparecer em
		// lugar nenhum dos bytes gerados para a Alfa.
		if bytes.Contains(dados, []byte(beta.produto.ID)) {
			t.Error("a exportação da Alfa carrega o id do Produto da Beta")
		}
		// ListarTodosGruposCatalogo (a fonte da exportação, nunca paginada)
		// agrupa por nome+dimensão: com o mesmo nome nas duas Empresas, um
		// vazamento apareceria como a quantidade das duas somada no MESMO
		// grupo, então o total do grupo é comparado ao saldo só da Alfa.
		grupos, err := ListarTodosGruposCatalogo(db, FiltrosCatalogo{EmpresaID: alfa.empresa.ID})
		if err != nil {
			t.Fatalf("ListarTodosGruposCatalogo: %v", err)
		}
		if len(grupos) != 1 {
			t.Fatalf("len(grupos) = %d, want 1", len(grupos))
		}
		saldoAlfa := saldoProdutoEstoque(t, db, alfa.produto.ID, alfa.estoque.ID)
		if grupos[0].QuantidadeTotal != saldoAlfa {
			t.Errorf("quantidadeTotal = %v, want %v (só o saldo da Alfa)", grupos[0].QuantidadeTotal, saldoAlfa)
		}
	})

	t.Run("estoques", func(t *testing.T) {
		lista, err := ListarEstoques(db, alfa.empresa.ID)
		if err != nil {
			t.Fatalf("ListarEstoques: %v", err)
		}
		if len(lista) != 1 || lista[0].ID != alfa.estoque.ID {
			t.Fatalf("estoques = %v, want só o da Alfa (mesmo nome existe nas duas)", lista)
		}
	})

	t.Run("movimentações", func(t *testing.T) {
		lista, err := ListarMovimentacoes(db, alfa.empresa.ID)
		if err != nil {
			t.Fatalf("ListarMovimentacoes: %v", err)
		}
		if len(lista) != 1 {
			t.Fatalf("len = %d, want 1 (só a baixa da Alfa)", len(lista))
		}
		if lista[0].ProdutoID != alfa.produto.ID {
			t.Errorf("movimentação de produto %q, want %q", lista[0].ProdutoID, alfa.produto.ID)
		}
	})

	t.Run("pedidos próprios", func(t *testing.T) {
		// O solicitante da Beta pedido tem id próprio: consultado sob a Alfa,
		// não existe nenhum Pedido dele.
		lista, err := ListarPedidosProprios(db, alfa.empresa.ID, beta.comum, "")
		if err != nil {
			t.Fatalf("ListarPedidosProprios: %v", err)
		}
		if len(lista) != 0 {
			t.Fatalf("len = %d, want 0 — Pedido da Beta apareceu sob a Alfa", len(lista))
		}
		propria, err := ListarPedidosProprios(db, alfa.empresa.ID, alfa.comum, "")
		if err != nil {
			t.Fatalf("ListarPedidosProprios(alfa): %v", err)
		}
		if len(propria) != 1 || propria[0].ID != alfa.pedido.ID {
			t.Fatalf("pedidos próprios da Alfa = %v, want só %q", propria, alfa.pedido.ID)
		}
	})

	t.Run("fila de pedidos", func(t *testing.T) {
		fila, err := ListarPedidosFila(db, alfa.empresa.ID, "")
		if err != nil {
			t.Fatalf("ListarPedidosFila: %v", err)
		}
		if len(fila) != 1 || fila[0].ID != alfa.pedido.ID {
			t.Fatalf("fila da Alfa = %v, want só %q", fila, alfa.pedido.ID)
		}
	})

	t.Run("log de acesso", func(t *testing.T) {
		logs, err := ListarLogsAcesso(db, alfa.empresa.ID, nil, nil)
		if err != nil {
			t.Fatalf("ListarLogsAcesso: %v", err)
		}
		if len(logs) != 1 {
			t.Fatalf("len = %d, want 1 (só a tentativa registrada na Alfa)", len(logs))
		}
		doUsuario, err := ListarLogsAcessoDoUsuario(db, alfa.empresa.ID, beta.comum)
		if err != nil {
			t.Fatalf("ListarLogsAcessoDoUsuario: %v", err)
		}
		if len(doUsuario) != 0 {
			t.Errorf("len = %d, want 0 — log da conta da Beta visível sob a Alfa", len(doUsuario))
		}
	})

	t.Run("inconsistências", func(t *testing.T) {
		sugestoes, err := AnalisarInconsistencias(db, alfa.empresa.ID)
		if err != nil {
			t.Fatalf("AnalisarInconsistencias: %v", err)
		}
		for _, s := range sugestoes {
			if s.ProdutoID == beta.produto.ID {
				t.Fatal("sugestão da Alfa aponta para Produto da Beta")
			}
		}
	})

	t.Run("duplicatas", func(t *testing.T) {
		// As duas Empresas têm Produto de MESMO nome, sem dimensão, em
		// Estoque de mesmo nome — antes do escopo por Empresa isto formaria
		// um grupo com um Produto de cada.
		grupos, err := DetectarDuplicatas(db, alfa.empresa.ID)
		if err != nil {
			t.Fatalf("DetectarDuplicatas: %v", err)
		}
		for _, g := range grupos {
			for _, p := range g.Produtos {
				if p.ID == beta.produto.ID {
					t.Fatal("grupo de duplicatas da Alfa contém Produto da Beta")
				}
			}
		}
	})

	t.Run("gestão de contas", func(t *testing.T) {
		contas, err := ListarUsuarios(db, alfa.empresa.ID, PapelAdm)
		if err != nil {
			t.Fatalf("ListarUsuarios: %v", err)
		}
		if len(contas) != 3 {
			t.Fatalf("len = %d, want 3 (as três contas da Alfa)", len(contas))
		}
		for _, c := range contas {
			if c.ID == beta.comum || c.ID == beta.almoxarife || c.ID == beta.adm {
				t.Fatalf("conta da Beta (%s) apareceu na Gestão de Contas da Alfa", c.Email)
			}
		}
	})

	// Story 9.3 (convite nominal, AD-22): o convite é a porta de entrada de
	// contas novas, então cruzar a fronteira aqui criaria uma conta na Empresa
	// errada. As duas metades: a listagem de B não vê o convite de A, e o
	// token de A não resolve — nem no GET de validação, nem no cadastro — sob
	// a Empresa B.
	t.Run("convites", func(t *testing.T) {
		tokenAlfa := conviteDeTeste(t, db, alfa.empresa.ID, "convidado-iso@empresa.com")

		convitesAlfa, err := ListarConvites(db, testEmailCfg, alfa.empresa.ID, alfa.empresa.Slug)
		if err != nil {
			t.Fatalf("ListarConvites(alfa): %v", err)
		}
		if len(convitesAlfa) != 1 {
			t.Fatalf("len(convites da Alfa) = %d, want 1", len(convitesAlfa))
		}
		convitesBeta, err := ListarConvites(db, testEmailCfg, beta.empresa.ID, beta.empresa.Slug)
		if err != nil {
			t.Fatalf("ListarConvites(beta): %v", err)
		}
		if len(convitesBeta) != 0 {
			t.Fatalf("len(convites da Beta) = %d, want 0 — o convite da Alfa não pode aparecer aqui", len(convitesBeta))
		}

		if _, err := ValidarTokenConvite(db, beta.empresa.ID, tokenAlfa); !errors.Is(err, ErrConviteNaoEncontrado) {
			t.Errorf("ValidarTokenConvite(beta, token da Alfa) = %v, want ErrConviteNaoEncontrado", err)
		}

		// Revogar também é escrita: um `gestor` da Beta não pode cancelar o
		// convite da Alfa nem descobrir que ele existe.
		var idAlfa string
		if err := db.QueryRow(`SELECT id FROM convites_empresa WHERE token = $1`, tokenAlfa).Scan(&idAlfa); err != nil {
			t.Fatalf("ler id do convite da Alfa: %v", err)
		}
		if err := RevogarConvite(db, beta.empresa.ID, idAlfa); !errors.Is(err, ErrConviteNaoEncontrado) {
			t.Errorf("RevogarConvite(beta, convite da Alfa) = %v, want ErrConviteNaoEncontrado", err)
		}

		antes := contarLinhas(t, db, "usuarios")
		if _, err := Cadastrar(db, testEmailCfg, beta.empresa.ID, beta.empresa.Slug,
			"Invasor", "convidado-iso@empresa.com", "senha-123456", tokenAlfa); !errors.Is(err, ErrConviteNaoEncontrado) {
			t.Fatalf("Cadastrar(beta, token da Alfa) = %v, want ErrConviteNaoEncontrado", err)
		}
		if depois := contarLinhas(t, db, "usuarios"); depois != antes {
			t.Errorf("count(usuarios) = %d, want %d — nenhuma conta pode nascer de um convite de outra Empresa", depois, antes)
		}
		// E o convite da Alfa continua intacto, pendente para o dono legítimo.
		if _, usadoEm, _ := lerConvite(t, db, tokenAlfa); usadoEm.Valid {
			t.Error("o convite da Alfa foi consumido por uma tentativa sob a Empresa Beta")
		}
	})

	t.Run("promoção", func(t *testing.T) {
		pendentes, err := ListarSolicitacoesPendentes(db, alfa.empresa.ID, PapelAdm)
		if err != nil {
			t.Fatalf("ListarSolicitacoesPendentes: %v", err)
		}
		if len(pendentes) != 1 {
			t.Fatalf("len = %d, want 1 (só a solicitação da Alfa)", len(pendentes))
		}
		if pendentes[0].SolicitanteEmail != "comum-iso-alfa@empresa.com" {
			t.Errorf("solicitante = %q, want a conta da Alfa", pendentes[0].SolicitanteEmail)
		}
	})

	t.Run("importações", func(t *testing.T) {
		imp, _, err := ObterUltimaImportacao(db, alfa.empresa.ID)
		if err != nil {
			t.Fatalf("ObterUltimaImportacao: %v", err)
		}
		if imp == nil {
			t.Fatal("ObterUltimaImportacao devolveu nil para a Alfa")
		}
		if imp.ID != alfa.importacao {
			t.Errorf("importação = %q, want %q — a mais recente da BASE era a da Beta", imp.ID, alfa.importacao)
		}
	})

	t.Run("categorias", func(t *testing.T) {
		categorias, err := ListarCategorias(db, alfa.empresa.ID)
		if err != nil {
			t.Fatalf("ListarCategorias: %v", err)
		}
		if len(categorias) == 0 {
			t.Fatal("nenhuma categoria para a Alfa — ProvisionarEmpresa deveria ter copiado a lista padrão")
		}
		// `categorias_padrao` (Story 9.4, migration 000035): a lista molde
		// mudou de casa — deixou de ser um punhado de linhas
		// `empresa_id IS NULL` DENTRO de `categorias` (o que impedia
		// `SET NOT NULL`) e passou a ter tabela própria.
		var padrao int
		if err := db.QueryRow(`SELECT count(*) FROM categorias_padrao`).Scan(&padrao); err != nil {
			t.Fatalf("contar categorias padrão: %v", err)
		}
		if len(categorias) != padrao {
			t.Errorf("len = %d, want %d (a cópia da lista padrão, nunca a das outras Empresas)", len(categorias), padrao)
		}
	})
}

// TestIsolamentoPorEmpresa_EscritaPorIdAlheioFalhaSemEfeito prova a segunda
// metade do AC 3: uma operação de escrita endereçada a um id da OUTRA Empresa
// falha no sentinela de "não encontrado" já existente e não deixa rastro.
func TestIsolamentoPorEmpresa_EscritaPorIdAlheioFalhaSemEfeito(t *testing.T) {
	db := testDB(t)
	limparProdutos(t, db)

	alfa := montarAmbienteIsolamento(t, db, "iso-alfa", "334455660001", "Alfa")
	beta := montarAmbienteIsolamento(t, db, "iso-beta", "445566770001", "Beta")

	t.Run("detalhe de Produto alheio", func(t *testing.T) {
		if _, err := ObterProdutoDetalhe(db, alfa.empresa.ID, beta.produto.ID); !errors.Is(err, ErrProdutoNaoEncontrado) {
			t.Fatalf("ObterProdutoDetalhe = %v, want ErrProdutoNaoEncontrado", err)
		}
	})

	t.Run("renomear Produto alheio", func(t *testing.T) {
		if _, err := AtualizarNomeProduto(db, alfa.empresa.ID, beta.produto.ID, "Nome Invadido"); !errors.Is(err, ErrProdutoNaoEncontrado) {
			t.Fatalf("AtualizarNomeProduto = %v, want ErrProdutoNaoEncontrado", err)
		}
		var nome string
		if err := db.QueryRow(`SELECT nome FROM produtos WHERE id = $1`, beta.produto.ID).Scan(&nome); err != nil {
			t.Fatalf("reler produto da Beta: %v", err)
		}
		if nome != "Prancha Isolamento" {
			t.Errorf("nome do Produto da Beta = %q, want intocado", nome)
		}
	})

	t.Run("baixa em Produto alheio", func(t *testing.T) {
		saldoAntes := saldoProdutoEstoque(t, db, beta.produto.ID, beta.estoque.ID)
		_, err := RegistrarBaixa(db, alfa.empresa.ID, beta.produto.ID, beta.estoque.ID, alfa.almoxarife, 1)
		if err == nil {
			t.Fatal("RegistrarBaixa cruzando Empresas = nil, want erro")
		}
		if saldoDepois := saldoProdutoEstoque(t, db, beta.produto.ID, beta.estoque.ID); saldoDepois != saldoAntes {
			t.Errorf("saldo da Beta = %v, want %v (nenhuma escrita)", saldoDepois, saldoAntes)
		}
	})

	t.Run("decidir Pedido alheio", func(t *testing.T) {
		_, err := DecidirPedido(db, alfa.empresa.ID, beta.pedido.ID, alfa.almoxarife, PapelAlmoxarife, true)
		if !errors.Is(err, ErrPedidoNaoEncontrado) {
			t.Fatalf("DecidirPedido = %v, want ErrPedidoNaoEncontrado", err)
		}
		var status string
		if err := db.QueryRow(`SELECT status FROM pedidos WHERE id = $1`, beta.pedido.ID).Scan(&status); err != nil {
			t.Fatalf("reler pedido da Beta: %v", err)
		}
		if status != "pendente" {
			t.Errorf("status do Pedido da Beta = %q, want pendente (nenhuma decisão)", status)
		}
	})

	t.Run("mesclar com id alheio", func(t *testing.T) {
		var erroMesclagem *ErroMesclagemInvalida
		_, err := MesclarDuplicatas(db, alfa.empresa.ID, alfa.produto.ID, []string{beta.produto.ID}, alfa.almoxarife)
		if !errors.As(err, &erroMesclagem) {
			t.Fatalf("MesclarDuplicatas = %v, want *ErroMesclagemInvalida", err)
		}
		var mesclagens int
		if err := db.QueryRow(`SELECT count(*) FROM mesclagens_duplicatas`).Scan(&mesclagens); err != nil {
			t.Fatalf("contar mesclagens: %v", err)
		}
		if mesclagens != 0 {
			t.Errorf("mesclagens gravadas = %d, want 0", mesclagens)
		}
		var ativo bool
		if err := db.QueryRow(`SELECT deleted_at IS NULL FROM produtos WHERE id = $1`, beta.produto.ID).Scan(&ativo); err != nil {
			t.Fatalf("reler produto da Beta: %v", err)
		}
		if !ativo {
			t.Error("Produto da Beta foi removido por uma mesclagem da Alfa")
		}
	})

	t.Run("decidir promoção alheia", func(t *testing.T) {
		pendentesBeta, err := ListarSolicitacoesPendentes(db, beta.empresa.ID, PapelAdm)
		if err != nil {
			t.Fatalf("ListarSolicitacoesPendentes(beta): %v", err)
		}
		if len(pendentesBeta) != 1 {
			t.Fatalf("pré-condição: len = %d, want 1", len(pendentesBeta))
		}
		if _, err := DecidirSolicitacao(db, alfa.empresa.ID, pendentesBeta[0].ID, alfa.adm, PapelAdm, true); err == nil {
			t.Fatal("DecidirSolicitacao cruzando Empresas = nil, want erro")
		}
		var papel string
		if err := db.QueryRow(`SELECT papel FROM usuarios WHERE id = $1`, beta.comum).Scan(&papel); err != nil {
			t.Fatalf("reler conta da Beta: %v", err)
		}
		if papel != PapelUsuario {
			t.Errorf("papel da conta da Beta = %q, want %q (promoção não podia ser aprovada de fora)", papel, PapelUsuario)
		}
	})
}
