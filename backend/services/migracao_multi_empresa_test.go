package services

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

// Testes da Story 9.4 (spec-9-4): a migração operacional do banco da Ferreira
// Costa para o modelo multi-Empresa.
//
// ARMADILHA desta suíte (precedente registrado na spec-9-1): o banco de teste
// é ÚNICO e compartilhado entre pacotes (`go test -p 1 ./...`), e `empresas`
// nunca é truncada. Nenhum teste daqui pode aplicar DDL nas 13 tabelas reais
// — o endurecimento se prova (a) pelo caminho de RECUSA, que não emite ALTER
// nenhum, e (b) numa tabela temporária criada pelo próprio teste. Cada teste
// remove as Empresas que criou e as linhas que existem por causa delas.

// limparEmpresaDaMigracao apaga a Empresa `slug` (e o Ambiente de Treinamento
// dela, se existir) com TODAS as linhas que existem por causa das duas, na
// ordem das FKs. `removerEmpresaDeTeste` não serve aqui: estas Empresas têm
// Estoques, Produtos e contas, não só as cópias das listas padrão.
func limparEmpresaDaMigracao(t *testing.T, db *sql.DB, slug string) {
	t.Helper()

	var ids []string
	rows, err := db.Query(
		`SELECT id FROM empresas WHERE slug = $1 OR slug = $1 || '-treinamento' ORDER BY empresa_origem_id NULLS LAST`,
		slug)
	if err != nil {
		t.Fatalf("limparEmpresaDaMigracao(%s): %v", slug, err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatalf("limparEmpresaDaMigracao(%s): scan: %v", slug, err)
		}
		ids = append(ids, id)
	}
	rows.Close()

	for _, id := range ids {
		for _, stmt := range []string{
			`DELETE FROM movimentacoes WHERE empresa_id = $1`,
			`DELETE FROM pedido_itens WHERE empresa_id = $1`,
			`DELETE FROM pedidos WHERE empresa_id = $1`,
			`DELETE FROM produto_estoque WHERE produto_id IN (SELECT id FROM produtos WHERE empresa_id = $1)`,
			`DELETE FROM produtos WHERE empresa_id = $1`,
			`DELETE FROM estoques WHERE empresa_id = $1`,
			`DELETE FROM logs_acesso WHERE empresa_id = $1`,
			`DELETE FROM solicitacoes_promocao WHERE empresa_id = $1`,
			`DELETE FROM importacoes WHERE empresa_id = $1`,
			`DELETE FROM convites_empresa WHERE empresa_id = $1`,
			// tokens_acao e emails_pendentes saem por CASCADE de usuarios.
			`DELETE FROM usuarios WHERE empresa_id = $1`,
			`DELETE FROM categorias WHERE empresa_id = $1`,
			`DELETE FROM nomenclatura_templates WHERE empresa_id = $1`,
		} {
			if _, err := db.Exec(stmt, id); err != nil {
				t.Fatalf("limparEmpresaDaMigracao(%s) [%s]: %v", slug, stmt, err)
			}
		}
	}
	// O Treinamento aponta para a real (empresa_origem_id): apaga primeiro.
	if _, err := db.Exec(`DELETE FROM empresas WHERE empresa_origem_id IN (SELECT id FROM empresas WHERE slug = $1)`, slug); err != nil {
		t.Fatalf("limparEmpresaDaMigracao(%s): treinamento: %v", slug, err)
	}
	if _, err := db.Exec(`DELETE FROM empresas WHERE slug = $1`, slug); err != nil {
		t.Fatalf("limparEmpresaDaMigracao(%s): empresa: %v", slug, err)
	}
}

// dadosDaFundadora monta um insumo válido de Empresa fundadora.
func dadosDaFundadora(slug, cnpjBase12, nome string) DadosEmpresa {
	return DadosEmpresa{
		NomeFantasia: nome,
		RazaoSocial:  nome + " LTDA",
		CNPJ:         cnpjDeTeste(cnpjBase12),
		Slug:         slug,
		Endereco: EnderecoEmpresa{
			Logradouro: "Av. Principal", Numero: "1000", Bairro: "Centro",
			Cidade: "Recife", CEP: "50000000", UF: "PE",
		},
	}
}

// TestAdotarEmpresaFundadora_CriaParSemAdmNaReal prova a AC 3 no service: a
// Empresa real nasce SEM listas e SEM `adm` (a conta global é adotada depois
// pelo backfill), e o Treinamento nasce completo pelo mecanismo da 9.2.
func TestAdotarEmpresaFundadora_CriaParSemAdmNaReal(t *testing.T) {
	db := testDB(t)
	const slug = "fundadora-94"
	t.Cleanup(func() { limparEmpresaDaMigracao(t, db, slug) })

	real, treino, err := AdotarEmpresaFundadora(db, testEmailCfg,
		dadosDaFundadora(slug, "947301000001", "Fundadora 94"), "Claudio", "claudio@fundadora.test")
	if err != nil {
		t.Fatalf("AdotarEmpresaFundadora: %v", err)
	}

	if real.Slug != slug || real.EmpresaOrigemID != nil {
		t.Errorf("empresa real = slug %q origem %v, want %q e nil", real.Slug, real.EmpresaOrigemID, slug)
	}
	if treino.Slug != slug+"-treinamento" || treino.EmpresaOrigemID == nil || *treino.EmpresaOrigemID != real.ID {
		t.Errorf("treinamento = slug %q origem %v, want %q apontando para a real", treino.Slug, treino.EmpresaOrigemID, slug+"-treinamento")
	}

	// A Empresa real: nenhuma lista copiada e nenhum `adm` criado.
	if n := contarLinhas94(t, db, `SELECT count(*) FROM categorias WHERE empresa_id = $1`, real.ID); n != 0 {
		t.Errorf("categorias da Empresa real = %d, want 0 (ela adota as legadas no backfill)", n)
	}
	if n := contarLinhas94(t, db, `SELECT count(*) FROM usuarios WHERE empresa_id = $1`, real.ID); n != 0 {
		t.Errorf("contas na Empresa real = %d, want 0 (a conta `adm` global é adotada, nunca recriada)", n)
	}

	// O Treinamento: listas, dados de exemplo e `adm` sem senha.
	if n := contarLinhas94(t, db, `SELECT count(*) FROM categorias WHERE empresa_id = $1`, treino.ID); n == 0 {
		t.Error("treinamento sem nenhuma categoria copiada")
	}
	if n := contarLinhas94(t, db, `SELECT count(*) FROM estoques WHERE empresa_id = $1`, treino.ID); n != 2 {
		t.Errorf("estoques de exemplo do treinamento = %d, want 2", n)
	}
	if n := contarLinhas94(t, db, `SELECT count(*) FROM produtos WHERE empresa_id = $1`, treino.ID); n != 5 {
		t.Errorf("produtos de exemplo do treinamento = %d, want 5", n)
	}

	var senhaHash sql.NullString
	var papel string
	var verificado bool
	if err := db.QueryRow(
		`SELECT senha_hash, papel, email_verificado FROM usuarios WHERE empresa_id = $1`, treino.ID,
	).Scan(&senhaHash, &papel, &verificado); err != nil {
		t.Fatalf("ler adm do treinamento: %v", err)
	}
	if senhaHash.Valid || papel != "adm" || !verificado {
		t.Errorf("adm do treinamento: senha_hash válida=%v papel=%q verificado=%v; want sem senha, adm, verificado",
			senhaHash.Valid, papel, verificado)
	}
	if n := contarLinhas94(t, db,
		`SELECT count(*) FROM emails_pendentes e JOIN usuarios u ON u.id = e.usuario_id
		 WHERE u.empresa_id = $1 AND e.tipo = 'primeiro_acesso'`, treino.ID); n != 1 {
		t.Errorf("e-mails primeiro_acesso do treinamento = %d, want 1", n)
	}
}

// TestAdotarEmpresaFundadora_Idempotente: a segunda execução da etapa
// `empresa` reconhece o par já criado e não escreve nada.
func TestAdotarEmpresaFundadora_Idempotente(t *testing.T) {
	db := testDB(t)
	const slug = "fundadora-94-idem"
	t.Cleanup(func() { limparEmpresaDaMigracao(t, db, slug) })

	dados := dadosDaFundadora(slug, "947302000001", "Fundadora 94 Idem")
	real1, treino1, err := AdotarEmpresaFundadora(db, testEmailCfg, dados, "Claudio", "claudio@idem.test")
	if err != nil {
		t.Fatalf("1a execução: %v", err)
	}
	empresasAntes := contarLinhas94(t, db, `SELECT count(*) FROM empresas`)
	contasAntes := contarLinhas94(t, db, `SELECT count(*) FROM usuarios WHERE empresa_id = $1`, treino1.ID)

	real2, treino2, err := AdotarEmpresaFundadora(db, testEmailCfg, dados, "Claudio", "claudio@idem.test")
	if err != nil {
		t.Fatalf("2a execução: %v", err)
	}
	if real2.ID != real1.ID || treino2.ID != treino1.ID {
		t.Errorf("2a execução devolveu ids diferentes: real %s->%s, treino %s->%s", real1.ID, real2.ID, treino1.ID, treino2.ID)
	}
	if n := contarLinhas94(t, db, `SELECT count(*) FROM empresas`); n != empresasAntes {
		t.Errorf("empresas = %d, want %d (nada recriado)", n, empresasAntes)
	}
	if n := contarLinhas94(t, db, `SELECT count(*) FROM usuarios WHERE empresa_id = $1`, treino1.ID); n != contasAntes {
		t.Errorf("contas do treinamento = %d, want %d (nenhum adm duplicado)", n, contasAntes)
	}
}

// TestBackfillEmpresa_LotesResumiveis prova as ACs 1 e 2: linhas órfãs são
// adotadas em lotes, a reexecução é inócua e as listas padrão da Empresa
// ficam completas.
func TestBackfillEmpresa_LotesResumiveis(t *testing.T) {
	db := testDB(t)
	const slug = "fundadora-94-backfill"
	t.Cleanup(func() { limparEmpresaDaMigracao(t, db, slug) })

	empresa := criarEmpresaDeTeste(t, db, slug, "947303000001", "Fundadora 94 Backfill")
	// A Empresa nasceu com as listas (criarEmpresaDeTeste usa
	// ProvisionarEmpresa); o que importa aqui é a adoção das linhas órfãs.

	var categoriaID string
	if err := db.QueryRow(`SELECT id FROM categorias WHERE empresa_id = $1 LIMIT 1`, empresa.ID).Scan(&categoriaID); err != nil {
		t.Fatalf("categoria da empresa: %v", err)
	}

	// Órfãs: 3 Estoques, 1 Produto e 1 conta, todos com empresa_id IS NULL.
	var orfaos []string
	for i := 1; i <= 3; i++ {
		var id string
		if err := db.QueryRow(
			`INSERT INTO estoques (nome) VALUES ($1) RETURNING id`, fmt.Sprintf("Orfao 94 Estoque %d", i),
		).Scan(&id); err != nil {
			t.Fatalf("criar estoque órfão: %v", err)
		}
		orfaos = append(orfaos, id)
	}
	var produtoOrfao string
	if err := db.QueryRow(
		`INSERT INTO produtos (nome, categoria_id) VALUES ('Orfao 94 Produto', $1) RETURNING id`, categoriaID,
	).Scan(&produtoOrfao); err != nil {
		t.Fatalf("criar produto órfão: %v", err)
	}
	var usuarioOrfao string
	if err := db.QueryRow(
		`INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
		 VALUES ('Orfao 94', 'orfao-94@teste.local', NULL, 'usuario', false, true) RETURNING id`,
	).Scan(&usuarioOrfao); err != nil {
		t.Fatalf("criar usuário órfão: %v", err)
	}

	// Lote 1 força várias transações por tabela — é o caminho resumível.
	visitadas := map[string]int{}
	atualizadas, err := BackfillEmpresa(db, empresa.ID, 1, func(tabela string, n int) { visitadas[tabela] = n })
	if err != nil {
		t.Fatalf("BackfillEmpresa: %v", err)
	}
	if len(visitadas) != len(TabelasComEmpresaID) {
		t.Errorf("tabelas visitadas = %d, want %d", len(visitadas), len(TabelasComEmpresaID))
	}
	if atualizadas["estoques"] < 3 || atualizadas["produtos"] < 1 || atualizadas["usuarios"] < 1 {
		t.Errorf("atualizadas = %v, want >= 3 estoques, 1 produto e 1 usuário", atualizadas)
	}

	for _, id := range append(orfaos, produtoOrfao, usuarioOrfao) {
		_ = id
	}
	for _, c := range []struct{ tabela, id string }{
		{"estoques", orfaos[0]}, {"estoques", orfaos[1]}, {"estoques", orfaos[2]},
		{"produtos", produtoOrfao}, {"usuarios", usuarioOrfao},
	} {
		var dono sql.NullString
		if err := db.QueryRow(`SELECT empresa_id FROM `+c.tabela+` WHERE id = $1`, c.id).Scan(&dono); err != nil {
			t.Fatalf("ler %s %s: %v", c.tabela, c.id, err)
		}
		if !dono.Valid || dono.String != empresa.ID {
			t.Errorf("%s %s: empresa_id = %v, want %s", c.tabela, c.id, dono, empresa.ID)
		}
	}

	// Reexecução: nenhuma linha atualizada, nenhuma lista duplicada.
	categoriasAntes := contarLinhas94(t, db, `SELECT count(*) FROM categorias WHERE empresa_id = $1`, empresa.ID)
	atualizadas2, err := BackfillEmpresa(db, empresa.ID, 0, nil)
	if err != nil {
		t.Fatalf("BackfillEmpresa (2a vez): %v", err)
	}
	for tabela, n := range atualizadas2 {
		if n != 0 {
			t.Errorf("2a execução atualizou %d linha(s) em %s, want 0", n, tabela)
		}
	}
	if n := contarLinhas94(t, db, `SELECT count(*) FROM categorias WHERE empresa_id = $1`, empresa.ID); n != categoriasAntes {
		t.Errorf("categorias da Empresa = %d, want %d (sem duplicar)", n, categoriasAntes)
	}

	// As listas padrão ficam completas (o que ela não adotou do legado).
	padrao := contarLinhas94(t, db, `SELECT count(*) FROM categorias_padrao`)
	if n := contarLinhas94(t, db, `SELECT count(*) FROM categorias WHERE empresa_id = $1`, empresa.ID); n < padrao {
		t.Errorf("categorias da Empresa = %d, want >= %d (lista padrão completada)", n, padrao)
	}
}

// TestBackfillEmpresa_EmpresaInexistente: recusa antes de qualquer UPDATE.
func TestBackfillEmpresa_EmpresaInexistente(t *testing.T) {
	db := testDB(t)

	_, err := BackfillEmpresa(db, "00000000-0000-0000-0000-000000000000", 10, nil)
	if !errors.Is(err, ErrEmpresaNaoEncontrada) {
		t.Fatalf("erro = %v, want ErrEmpresaNaoEncontrada", err)
	}
}

// TestEndurecerEmpresaID_RecusaComOrfas prova a AC 4: uma única linha órfã
// recusa a operação inteira e NENHUM ALTER TABLE é emitido.
func TestEndurecerEmpresaID_RecusaComOrfas(t *testing.T) {
	db := testDB(t)

	var orfao string
	if err := db.QueryRow(
		`INSERT INTO estoques (nome) VALUES ('Orfao 94 Endurecimento') RETURNING id`,
	).Scan(&orfao); err != nil {
		t.Fatalf("criar estoque órfão: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM estoques WHERE id = $1`, orfao) })

	antes, err := ColunasJaNotNull(db)
	if err != nil {
		t.Fatalf("ColunasJaNotNull: %v", err)
	}

	endurecidas, err := EndurecerEmpresaID(db)
	if !errors.Is(err, ErrOrfasPendentes) {
		t.Fatalf("erro = %v, want ErrOrfasPendentes", err)
	}
	if len(endurecidas) != 0 {
		t.Errorf("endurecidas = %v, want nenhuma", endurecidas)
	}
	if msg := err.Error(); !contemTodas(msg, "estoques", "Nenhum ALTER TABLE foi emitido") {
		t.Errorf("mensagem = %q, quer nomear a tabela e afirmar que nada foi emitido", msg)
	}

	depois, err := ColunasJaNotNull(db)
	if err != nil {
		t.Fatalf("ColunasJaNotNull (depois): %v", err)
	}
	if len(depois) != len(antes) {
		t.Errorf("colunas NOT NULL mudaram de %d para %d — a recusa emitiu DDL", len(antes), len(depois))
	}
}

// TestEndurecerColunaEmpresa_TabelaTemporaria exercita o caminho de DDL numa
// tabela criada pelo próprio teste — NUNCA nas 13 reais, que são
// compartilhadas com todas as outras suítes.
func TestEndurecerColunaEmpresa_TabelaTemporaria(t *testing.T) {
	db := testDB(t)
	const tabela = "teste_endurecimento_94"

	if _, err := db.Exec(`CREATE TABLE ` + tabela + ` (id serial PRIMARY KEY, empresa_id uuid)`); err != nil {
		t.Fatalf("criar tabela temporária: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DROP TABLE IF EXISTS ` + tabela) })

	if _, err := db.Exec(`INSERT INTO `+tabela+` (empresa_id) VALUES ($1)`, empresaTeste); err != nil {
		t.Fatalf("inserir linha com Empresa: %v", err)
	}

	if err := endurecerColunaEmpresa(db, tabela); err != nil {
		t.Fatalf("endurecerColunaEmpresa: %v", err)
	}
	if nulavel := nulabilidade94(t, db, tabela); nulavel != "NO" {
		t.Errorf("is_nullable = %q, want NO", nulavel)
	}

	// Idempotente: reaplicar não quebra e não deixa a CHECK para trás.
	if err := endurecerColunaEmpresa(db, tabela); err != nil {
		t.Fatalf("endurecerColunaEmpresa (2a vez): %v", err)
	}
	if n := contarLinhas94(t, db,
		`SELECT count(*) FROM pg_constraint WHERE conname = $1`, tabela+"_empresa_id_nao_nula"); n != 0 {
		t.Errorf("constraint auxiliar sobrou (%d) — ela é criada e derrubada na mesma transação", n)
	}

	// A coluna agora recusa NULL de verdade.
	if _, err := db.Exec(`INSERT INTO ` + tabela + ` (empresa_id) VALUES (NULL)`); err == nil {
		t.Error("INSERT com empresa_id NULL foi aceito depois do endurecimento")
	}
}

// TestContarLinhasSemEmpresa_CobreAsTreze: o diagnóstico sempre devolve as 13
// chaves, inclusive as zeradas.
func TestContarLinhasSemEmpresa_CobreAsTreze(t *testing.T) {
	db := testDB(t)

	contagens, err := ContarLinhasSemEmpresa(db)
	if err != nil {
		t.Fatalf("ContarLinhasSemEmpresa: %v", err)
	}
	if len(contagens) != len(TabelasComEmpresaID) {
		t.Fatalf("chaves = %d, want %d", len(contagens), len(TabelasComEmpresaID))
	}
	for _, tabela := range TabelasComEmpresaID {
		if _, ok := contagens[tabela]; !ok {
			t.Errorf("tabela %q ausente do diagnóstico", tabela)
		}
	}
}

// TestBuscarAdmSemEmpresa lê a conta `adm` global — a que o backfill adota.
func TestBuscarAdmSemEmpresa(t *testing.T) {
	db := testDB(t)

	if _, _, err := BuscarAdmSemEmpresa(db); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("sem adm órfão: err = %v, want sql.ErrNoRows", err)
	}

	if _, err := db.Exec(
		`INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
		 VALUES ('Adm Global 94', 'adm-global-94@teste.local', NULL, 'adm', true, true)`,
	); err != nil {
		t.Fatalf("criar adm órfão: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM usuarios WHERE email = 'adm-global-94@teste.local'`) })

	nome, email, err := BuscarAdmSemEmpresa(db)
	if err != nil {
		t.Fatalf("BuscarAdmSemEmpresa: %v", err)
	}
	if nome != "Adm Global 94" || email != "adm-global-94@teste.local" {
		t.Errorf("adm = %q <%s>, want a conta órfã", nome, email)
	}
}

// --- helpers locais ---

func contarLinhas94(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("contar (%s): %v", query, err)
	}
	return n
}

func nulabilidade94(t *testing.T, db *sql.DB, tabela string) string {
	t.Helper()
	var v string
	if err := db.QueryRow(
		`SELECT is_nullable FROM information_schema.columns
		 WHERE table_schema = current_schema() AND table_name = $1 AND column_name = 'empresa_id'`, tabela,
	).Scan(&v); err != nil {
		t.Fatalf("is_nullable de %s: %v", tabela, err)
	}
	return v
}

func contemTodas(s string, partes ...string) bool {
	for _, p := range partes {
		encontrou := false
		for i := 0; i+len(p) <= len(s); i++ {
			if s[i:i+len(p)] == p {
				encontrou = true
				break
			}
		}
		if !encontrou {
			return false
		}
	}
	return true
}

// TestAdotarEmpresaFundadora_CNPJInvalidoNaoEscreve: validação ANTES da
// transação — o CLI recusa nomeando o campo e nenhuma linha é gravada.
func TestAdotarEmpresaFundadora_CNPJInvalidoNaoEscreve(t *testing.T) {
	db := testDB(t)
	const slug = "fundadora-94-cnpj-ruim"
	t.Cleanup(func() { limparEmpresaDaMigracao(t, db, slug) })

	dados := dadosDaFundadora(slug, "947305000001", "Fundadora 94 CNPJ Ruim")
	dados.CNPJ = "123"

	_, _, err := AdotarEmpresaFundadora(db, testEmailCfg, dados, "Claudio", "claudio@cnpj.test")
	var validacao *ErroEmpresaValidacao
	if !errors.As(err, &validacao) {
		t.Fatalf("erro = %v, want *ErroEmpresaValidacao", err)
	}
	if n := contarLinhas94(t, db, `SELECT count(*) FROM empresas WHERE slug = $1`, slug); n != 0 {
		t.Errorf("empresas com o slug = %d, want 0 (recusa antes de qualquer escrita)", n)
	}
}

// TestAdotarEmpresaFundadora_CNPJDuplicadoNaoEscreve: o CNPJ é único entre
// Empresas reais na plataforma inteira — a colisão recusa sem gravar nada.
func TestAdotarEmpresaFundadora_CNPJDuplicadoNaoEscreve(t *testing.T) {
	db := testDB(t)
	const slugPrimeira = "fundadora-94-cnpj-a"
	const slugSegunda = "fundadora-94-cnpj-b"
	t.Cleanup(func() {
		limparEmpresaDaMigracao(t, db, slugSegunda)
		limparEmpresaDaMigracao(t, db, slugPrimeira)
	})

	criarEmpresaDeTeste(t, db, slugPrimeira, "947306000001", "Fundadora 94 CNPJ A")

	dados := dadosDaFundadora(slugSegunda, "947306000001", "Fundadora 94 CNPJ B")
	_, _, err := AdotarEmpresaFundadora(db, testEmailCfg, dados, "Claudio", "claudio@cnpj-b.test")
	if !errors.Is(err, ErrCNPJDuplicado) {
		t.Fatalf("erro = %v, want ErrCNPJDuplicado", err)
	}
	if n := contarLinhas94(t, db, `SELECT count(*) FROM empresas WHERE slug = $1`, slugSegunda); n != 0 {
		t.Errorf("empresas com o slug da segunda = %d, want 0 (nada gravado)", n)
	}
}

// TestBackfillEmpresa_RetomaDeEstadoParcial prova a AC 2 no caso que importa:
// uma execução ANTERIOR interrompida já commitou parte dos lotes. A
// reexecução completa o que falta, não re-toca o que já tinha Empresa, e
// nenhuma linha é duplicada ou perde o vínculo.
func TestBackfillEmpresa_RetomaDeEstadoParcial(t *testing.T) {
	db := testDB(t)
	const slug = "fundadora-94-retomada"
	t.Cleanup(func() { limparEmpresaDaMigracao(t, db, slug) })

	empresa := criarEmpresaDeTeste(t, db, slug, "947307000001", "Fundadora 94 Retomada")

	var ids []string
	for i := 1; i <= 4; i++ {
		var id string
		if err := db.QueryRow(
			`INSERT INTO estoques (nome) VALUES ($1) RETURNING id`, fmt.Sprintf("Retomada 94 Estoque %d", i),
		).Scan(&id); err != nil {
			t.Fatalf("criar estoque órfão: %v", err)
		}
		ids = append(ids, id)
	}

	// Estado deixado por uma execução interrompida: os dois primeiros lotes
	// já commitaram (essas linhas JÁ têm Empresa), os outros dois não.
	for _, id := range ids[:2] {
		if _, err := db.Exec(`UPDATE estoques SET empresa_id = $1 WHERE id = $2`, empresa.ID, id); err != nil {
			t.Fatalf("simular lote já commitado: %v", err)
		}
	}

	orfasAntes, err := ContarLinhasSemEmpresa(db)
	if err != nil {
		t.Fatalf("ContarLinhasSemEmpresa: %v", err)
	}

	atualizadas, err := BackfillEmpresa(db, empresa.ID, 1, nil)
	if err != nil {
		t.Fatalf("BackfillEmpresa (retomada): %v", err)
	}

	// Exatamente as linhas que estavam órfãs no início desta chamada — nem
	// uma a mais: as já commitadas não são re-tocadas.
	if atualizadas["estoques"] != orfasAntes["estoques"] {
		t.Errorf("estoques atualizados = %d, want %d (só os que ainda estavam órfãos)",
			atualizadas["estoques"], orfasAntes["estoques"])
	}

	// Nenhuma linha perdida, nenhuma duplicada: as 4 continuam existindo,
	// todas na Empresa, uma vez cada.
	for _, id := range ids {
		var dono sql.NullString
		if err := db.QueryRow(`SELECT empresa_id FROM estoques WHERE id = $1`, id).Scan(&dono); err != nil {
			t.Fatalf("ler estoque %s: %v", id, err)
		}
		if !dono.Valid || dono.String != empresa.ID {
			t.Errorf("estoque %s: empresa_id = %v, want %s", id, dono, empresa.ID)
		}
	}
	if n := contarLinhas94(t, db,
		`SELECT count(*) FROM estoques WHERE nome LIKE 'Retomada 94 Estoque %'`); n != len(ids) {
		t.Errorf("estoques da retomada = %d, want %d (nenhuma linha duplicada nem perdida)", n, len(ids))
	}
}

// TestBackfillEmpresa_EmpresaInativa: uma Empresa desativada não recebe dado
// nenhum — mesmo recorte de BuscarEmpresaPorSlug.
func TestBackfillEmpresa_EmpresaInativa(t *testing.T) {
	db := testDB(t)
	const slug = "fundadora-94-inativa"
	t.Cleanup(func() { limparEmpresaDaMigracao(t, db, slug) })

	empresa := criarEmpresaDeTeste(t, db, slug, "947308000001", "Fundadora 94 Inativa")
	if _, err := db.Exec(`UPDATE empresas SET status = 'inativa' WHERE id = $1`, empresa.ID); err != nil {
		t.Fatalf("desativar empresa: %v", err)
	}

	var orfao string
	if err := db.QueryRow(
		`INSERT INTO estoques (nome) VALUES ('Orfao 94 Inativa') RETURNING id`,
	).Scan(&orfao); err != nil {
		t.Fatalf("criar estoque órfão: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM estoques WHERE id = $1`, orfao) })

	if _, err := BackfillEmpresa(db, empresa.ID, 10, nil); !errors.Is(err, ErrEmpresaNaoEncontrada) {
		t.Fatalf("erro = %v, want ErrEmpresaNaoEncontrada", err)
	}

	var dono sql.NullString
	if err := db.QueryRow(`SELECT empresa_id FROM estoques WHERE id = $1`, orfao).Scan(&dono); err != nil {
		t.Fatalf("ler estoque órfão: %v", err)
	}
	if dono.Valid {
		t.Errorf("estoque órfão recebeu empresa_id = %v — o backfill escreveu para uma Empresa inativa", dono)
	}
}
