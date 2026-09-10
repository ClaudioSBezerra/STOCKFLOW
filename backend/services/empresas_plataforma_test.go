package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"
)

// Testes da gestão de Empresas pelo Dono — Story 9.2, spec-9-2 (AC 2, 3, 4 e
// 5 no service).
//
// ARMADILHA (spec-9-2): o banco é compartilhado entre pacotes e `empresas`
// nunca é truncada. Toda Empresa criada aqui é removida com TODAS as linhas
// que existem por causa dela, via DELETE — Treinamento antes da real.

// novaEmpresaTeste monta um insumo válido de CriarEmpresaComTreinamento.
func novaEmpresaTeste(slug, cnpjBase12, nomeFantasia string) NovaEmpresaInput {
	return NovaEmpresaInput{
		DadosEmpresa: DadosEmpresa{
			NomeFantasia: nomeFantasia,
			RazaoSocial:  nomeFantasia + " LTDA",
			CNPJ:         cnpjDeTeste(cnpjBase12),
			Slug:         slug,
			Endereco: EnderecoEmpresa{
				Logradouro:  "Av. da Plataforma",
				Numero:      "9",
				Complemento: "Sala 2",
				Bairro:      "Boa Viagem",
				Cidade:      "Recife",
				CEP:         "51020-000",
				UF:          "pe",
			},
		},
		AdmNome:  "Ana Administradora",
		AdmEmail: "Ana.Adm@Cliente.com",
	}
}

// removerEmpresaComDados apaga a Empresa `slug` e todas as linhas criadas por
// causa dela (dados de exemplo, contas, logs, listas copiadas).
func removerEmpresaComDados(t *testing.T, db *sql.DB, slug string) {
	t.Helper()
	var id string
	err := db.QueryRow(`SELECT id FROM empresas WHERE slug = $1`, slug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return
	}
	if err != nil {
		t.Fatalf("removerEmpresaComDados(%s): %v", slug, err)
	}
	for _, stmt := range []string{
		`DELETE FROM produto_estoque WHERE produto_id IN (SELECT id FROM produtos WHERE empresa_id = $1)`,
		`DELETE FROM produtos WHERE empresa_id = $1`,
		`DELETE FROM estoques WHERE empresa_id = $1`,
		`DELETE FROM logs_acesso WHERE empresa_id = $1`,
		`DELETE FROM usuarios WHERE empresa_id = $1`,
		`DELETE FROM categorias WHERE empresa_id = $1`,
		`DELETE FROM nomenclatura_templates WHERE empresa_id = $1`,
		`DELETE FROM empresas WHERE id = $1`,
	} {
		if _, err := db.Exec(stmt, id); err != nil {
			t.Fatalf("removerEmpresaComDados(%s) [%s]: %v", slug, stmt, err)
		}
	}
}

// removerParDeTeste remove o Treinamento e depois a Empresa real de `slug`.
func removerParDeTeste(t *testing.T, db *sql.DB, slug string) {
	t.Helper()
	removerEmpresaComDados(t, db, slug+sufixoSlugTreinamento)
	removerEmpresaComDados(t, db, slug)
}

// comParLimpo garante que `slugs` não existem antes do teste e são removidos
// depois dele.
func comParLimpo(t *testing.T, db *sql.DB, slugs ...string) {
	t.Helper()
	for _, s := range slugs {
		removerParDeTeste(t, db, s)
	}
	t.Cleanup(func() {
		for _, s := range slugs {
			removerParDeTeste(t, db, s)
		}
	})
}

func contar(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("contar [%s]: %v", q, err)
	}
	return n
}

// admPrimeiroAcesso lê o `adm` de uma Empresa, o e-mail de primeiro acesso e
// o token de redefinição dele.
type admPrimeiroAcesso struct {
	id, nome, email, papel string
	semSenha, verificado   bool
	link, empresaNoEmail   string
	nomeNoEmail            string
	token                  string
	expiraEm               time.Time
}

func lerAdmPrimeiroAcesso(t *testing.T, db *sql.DB, empresaID string) admPrimeiroAcesso {
	t.Helper()
	if n := contar(t, db, `SELECT count(*) FROM usuarios WHERE empresa_id = $1`, empresaID); n != 1 {
		t.Fatalf("usuarios da empresa %s = %d, want 1 (só o adm)", empresaID, n)
	}
	var a admPrimeiroAcesso
	if err := db.QueryRow(
		`SELECT id, nome, email, papel, senha_hash IS NULL, email_verificado FROM usuarios WHERE empresa_id = $1`, empresaID,
	).Scan(&a.id, &a.nome, &a.email, &a.papel, &a.semSenha, &a.verificado); err != nil {
		t.Fatalf("ler adm: %v", err)
	}

	var tipo string
	var variaveis []byte
	if n := contar(t, db, `SELECT count(*) FROM emails_pendentes WHERE usuario_id = $1`, a.id); n != 1 {
		t.Fatalf("e-mails do adm = %d, want 1", n)
	}
	if err := db.QueryRow(`SELECT tipo, variaveis_json FROM emails_pendentes WHERE usuario_id = $1`, a.id).Scan(&tipo, &variaveis); err != nil {
		t.Fatalf("ler e-mail do adm: %v", err)
	}
	if tipo != "primeiro_acesso" {
		t.Errorf("tipo do e-mail = %q, want primeiro_acesso", tipo)
	}
	var v map[string]string
	if err := json.Unmarshal(variaveis, &v); err != nil {
		t.Fatalf("variaveis_json: %v", err)
	}
	a.link, a.empresaNoEmail, a.nomeNoEmail = v["link"], v["empresa"], v["nome"]

	if err := db.QueryRow(
		`SELECT token, expira_em FROM tokens_acao WHERE usuario_id = $1 AND tipo = 'redefinicao_senha'`, a.id,
	).Scan(&a.token, &a.expiraEm); err != nil {
		t.Fatalf("ler token de primeiro acesso: %v", err)
	}
	return a
}

// TestCriarEmpresaComTreinamento_CriacaoCompleta é a prova central da AC 2:
// duas Empresas (a real e o Treinamento, com origem e CNPJ iguais), dois
// `adm` sem senha, dois e-mails `primeiro_acesso` com o link sob o slug certo
// e dados de exemplo SÓ no Treinamento.
func TestCriarEmpresaComTreinamento_CriacaoCompleta(t *testing.T) {
	db := testDB(t)
	comParLimpo(t, db, "plat-completa")

	empresa, treino, err := CriarEmpresaComTreinamento(db, testEmailCfg, novaEmpresaTeste("plat-completa", "901234560001", "Cliente Completo"))
	if err != nil {
		t.Fatalf("CriarEmpresaComTreinamento: %v", err)
	}

	if empresa.Slug != "plat-completa" || empresa.EmpresaOrigemID != nil || empresa.Status != StatusEmpresaAtiva {
		t.Errorf("empresa real = %+v", empresa)
	}
	if treino.Slug != "plat-completa-treinamento" || treino.NomeFantasia != "Cliente Completo - Treinamento" {
		t.Errorf("treino slug/nome = %q/%q", treino.Slug, treino.NomeFantasia)
	}
	if treino.EmpresaOrigemID == nil || *treino.EmpresaOrigemID != empresa.ID {
		t.Errorf("treino.EmpresaOrigemID = %v, want %q", treino.EmpresaOrigemID, empresa.ID)
	}
	if treino.CNPJ != empresa.CNPJ || treino.RazaoSocial != empresa.RazaoSocial || treino.Endereco != empresa.Endereco {
		t.Errorf("treino não herdou CNPJ/razão social/endereço: %+v vs %+v", treino, empresa)
	}
	if empresa.Endereco.CEP != "51020000" || empresa.Endereco.UF != "PE" || empresa.Endereco.Complemento != "Sala 2" {
		t.Errorf("endereço normalizado = %+v", empresa.Endereco)
	}

	for _, e := range []Empresa{empresa, treino} {
		a := lerAdmPrimeiroAcesso(t, db, e.ID)
		if a.nome != "Ana Administradora" || a.email != "ana.adm@cliente.com" || a.papel != PapelAdm {
			t.Errorf("%s: adm = %+v", e.Slug, a)
		}
		if !a.semSenha || !a.verificado {
			t.Errorf("%s: senha_hash NULL=%v email_verificado=%v, want true/true", e.Slug, a.semSenha, a.verificado)
		}
		prefixo := testEmailCfg.AppURL + "/e/" + e.Slug + "/redefinir-senha?token="
		if a.link != prefixo+a.token {
			t.Errorf("%s: link = %q, want %q", e.Slug, a.link, prefixo+a.token)
		}
		if a.empresaNoEmail != e.NomeFantasia || a.nomeNoEmail != "Ana Administradora" {
			t.Errorf("%s: variáveis do e-mail = empresa %q, nome %q", e.Slug, a.empresaNoEmail, a.nomeNoEmail)
		}
		if d := time.Until(a.expiraEm); d < tokenPrimeiroAcessoExpiracao-time.Minute || d > tokenPrimeiroAcessoExpiracao {
			t.Errorf("%s: token expira em %v, want ~7 dias", e.Slug, d)
		}
	}

	// Dados de exemplo: só no Treinamento, com as Categorias DA CÓPIA dele.
	if n := contar(t, db, `SELECT count(*) FROM produtos WHERE empresa_id = $1`, treino.ID); n != 5 {
		t.Errorf("produtos do treino = %d, want 5", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM estoques WHERE empresa_id = $1`, treino.ID); n != 2 {
		t.Errorf("estoques do treino = %d, want 2", n)
	}
	if n := contar(t, db, `SELECT count(DISTINCT categoria_id) FROM produtos WHERE empresa_id = $1`, treino.ID); n < 3 {
		t.Errorf("categorias distintas = %d, want >= 3", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM produtos p JOIN categorias c ON c.id = p.categoria_id
		WHERE p.empresa_id = $1 AND c.empresa_id IS DISTINCT FROM $1`, treino.ID); n != 0 {
		t.Errorf("%d produtos do treino usam Categoria de outra Empresa", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM produto_estoque pe JOIN produtos p ON p.id = pe.produto_id
		JOIN estoques e ON e.id = pe.estoque_id
		WHERE p.empresa_id = $1 AND e.empresa_id = $1 AND pe.quantidade <= 2`, treino.ID); n < 1 {
		t.Error("nenhum produto de exemplo com quantidade baixa (<= 2)")
	}
	if n := contar(t, db, `SELECT count(*) FROM movimentacoes WHERE empresa_id = $1`, treino.ID); n != 0 {
		t.Errorf("movimentacoes do treino = %d, want 0 (cadastro não gera movimentação)", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM produtos WHERE empresa_id = $1`, empresa.ID) +
		contar(t, db, `SELECT count(*) FROM estoques WHERE empresa_id = $1`, empresa.ID); n != 0 {
		t.Errorf("a Empresa real nasceu com %d produtos/estoques, want 0", n)
	}

	// O link de primeiro acesso funciona pelo fluxo da Story 1.6, sob a
	// Empresa certa — e o token de uma Empresa não serve na outra.
	admReal := lerAdmPrimeiroAcesso(t, db, empresa.ID)
	admTreino := lerAdmPrimeiroAcesso(t, db, treino.ID)
	if err := RedefinirSenha(db, treino.ID, admReal.token, "Senha-nova-1"); !errors.Is(err, ErrTokenNaoEncontrado) {
		t.Errorf("token da real sob o treino: erro = %v, want ErrTokenNaoEncontrado", err)
	}
	if err := RedefinirSenha(db, treino.ID, admTreino.token, "Senha-nova-1"); err != nil {
		t.Fatalf("RedefinirSenha (primeiro acesso do treino): %v", err)
	}
	if _, err := Login(db, treino.ID, "ana.adm@cliente.com", "Senha-nova-1"); err != nil {
		t.Errorf("login do adm do treino após o primeiro acesso: %v", err)
	}
}

func TestCriarEmpresaComTreinamento_SlugPadraoDoNomeFantasia(t *testing.T) {
	db := testDB(t)
	comParLimpo(t, db, "construtora-avila-plataforma")

	input := novaEmpresaTeste("", "912345670001", "Construtora Ávila Plataforma")
	empresa, treino, err := CriarEmpresaComTreinamento(db, testEmailCfg, input)
	if err != nil {
		t.Fatalf("CriarEmpresaComTreinamento: %v", err)
	}
	if empresa.Slug != "construtora-avila-plataforma" || treino.Slug != "construtora-avila-plataforma-treinamento" {
		t.Errorf("slugs = %q / %q", empresa.Slug, treino.Slug)
	}
}

func TestCriarEmpresaComTreinamento_CNPJDuplicadoNaoGravaNada(t *testing.T) {
	db := testDB(t)
	comParLimpo(t, db, "plat-cnpj-a", "plat-cnpj-b")

	if _, _, err := CriarEmpresaComTreinamento(db, testEmailCfg, novaEmpresaTeste("plat-cnpj-a", "923456780001", "Cliente CNPJ A")); err != nil {
		t.Fatalf("primeira criação: %v", err)
	}
	_, _, err := CriarEmpresaComTreinamento(db, testEmailCfg, novaEmpresaTeste("plat-cnpj-b", "923456780001", "Cliente CNPJ B"))
	if !errors.Is(err, ErrCNPJDuplicado) {
		t.Fatalf("erro = %v, want ErrCNPJDuplicado", err)
	}
	if n := contar(t, db, `SELECT count(*) FROM empresas WHERE slug IN ('plat-cnpj-b', 'plat-cnpj-b-treinamento')`); n != 0 {
		t.Errorf("%d empresas gravadas para a criação recusada", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM empresas WHERE cnpj = $1`, cnpjDeTeste("923456780001")); n != 2 {
		t.Errorf("empresas com o CNPJ = %d, want 2 (a real e o Treinamento dela)", n)
	}
}

func TestCriarEmpresaComTreinamento_SlugDuplicadoNaoGravaNada(t *testing.T) {
	db := testDB(t)
	comParLimpo(t, db, "plat-slug", "plat-colide")
	t.Cleanup(func() { removerEmpresaComDados(t, db, "plat-colide-treinamento") })

	if _, _, err := CriarEmpresaComTreinamento(db, testEmailCfg, novaEmpresaTeste("plat-slug", "934567890001", "Cliente Slug")); err != nil {
		t.Fatalf("primeira criação: %v", err)
	}

	t.Run("slug da real em uso", func(t *testing.T) {
		_, _, err := CriarEmpresaComTreinamento(db, testEmailCfg, novaEmpresaTeste("plat-slug", "945678900001", "Outro Cliente"))
		if !errors.Is(err, ErrSlugDuplicado) || errors.Is(err, ErrSlugTreinamentoDuplicado) {
			t.Fatalf("erro = %v, want ErrSlugDuplicado (da real)", err)
		}
		if n := contar(t, db, `SELECT count(*) FROM empresas WHERE cnpj = $1`, cnpjDeTeste("945678900001")); n != 0 {
			t.Errorf("%d empresas gravadas para a criação recusada", n)
		}
	})

	t.Run("slug do treinamento em uso", func(t *testing.T) {
		criarEmpresaDeTeste(t, db, "plat-colide-treinamento", "956789010001", "Empresa Que Colide")
		_, _, err := CriarEmpresaComTreinamento(db, testEmailCfg, novaEmpresaTeste("plat-colide", "967890120001", "Cliente Colide"))
		if !errors.Is(err, ErrSlugTreinamentoDuplicado) || !errors.Is(err, ErrSlugDuplicado) {
			t.Fatalf("erro = %v, want ErrSlugTreinamentoDuplicado", err)
		}
		if n := contar(t, db, `SELECT count(*) FROM empresas WHERE cnpj = $1`, cnpjDeTeste("967890120001")); n != 0 {
			t.Errorf("%d empresas gravadas para a criação recusada (a real precisa ter sido desfeita)", n)
		}
	})
}

func TestCriarEmpresaComTreinamento_Validacoes(t *testing.T) {
	db := testDB(t)
	const base = "978901230001"
	cnpjValido := cnpjDeTeste(base)
	cnpjDigitoErrado := cnpjValido[:13] + string(rune((cnpjValido[13]-'0'+1)%10+'0'))

	casos := []struct {
		nome    string
		mudar   func(*NovaEmpresaInput)
		mensage string
	}{
		{"CNPJ com dígito errado", func(i *NovaEmpresaInput) { i.CNPJ = cnpjDigitoErrado }, "CNPJ"},
		{"UF inválida", func(i *NovaEmpresaInput) { i.Endereco.UF = "P1" }, "UF"},
		{"CEP inválido", func(i *NovaEmpresaInput) { i.Endereco.CEP = "123" }, "CEP"},
		{"e-mail do adm sem @", func(i *NovaEmpresaInput) { i.AdmEmail = "sem-arroba.com" }, "e-mail do administrador"},
		{"nome do adm vazio", func(i *NovaEmpresaInput) { i.AdmNome = "   " }, "nome do administrador"},
		{"slug com 52 caracteres", func(i *NovaEmpresaInput) { i.Slug = strings.Repeat("a", 52) }, "51"},
		{"nome fantasia com 242 caracteres", func(i *NovaEmpresaInput) { i.NomeFantasia = strings.Repeat("N", 242) }, "241"},
		{"razão social vazia", func(i *NovaEmpresaInput) { i.RazaoSocial = "" }, "razão social"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			input := novaEmpresaTeste("plat-validacao", base, "Cliente Validação")
			c.mudar(&input)
			_, _, err := CriarEmpresaComTreinamento(db, testEmailCfg, input)
			var ev *ErroEmpresaValidacao
			if !errors.As(err, &ev) {
				t.Fatalf("erro = %v, want *ErroEmpresaValidacao", err)
			}
			if !strings.Contains(ev.Mensagem, c.mensage) {
				t.Errorf("mensagem = %q, want conter %q", ev.Mensagem, c.mensage)
			}
		})
	}
	if n := contar(t, db, `SELECT count(*) FROM empresas WHERE cnpj = $1 OR slug LIKE 'plat-validacao%'`, cnpjValido); n != 0 {
		t.Errorf("%d empresas gravadas por criações reprovadas na validação", n)
	}
}

// TestCriarEmpresaComTreinamento_LimitesExatos prova que os tetos 51/241
// deixam o Treinamento caber nas colunas (63/255).
func TestCriarEmpresaComTreinamento_LimitesExatos(t *testing.T) {
	db := testDB(t)
	slug := strings.Repeat("b", 51)
	comParLimpo(t, db, slug)

	input := novaEmpresaTeste(slug, "989012340001", strings.Repeat("N", 241))
	_, treino, err := CriarEmpresaComTreinamento(db, testEmailCfg, input)
	if err != nil {
		t.Fatalf("CriarEmpresaComTreinamento nos limites: %v", err)
	}
	if len(treino.Slug) != 63 || len([]rune(treino.NomeFantasia)) != 255 {
		t.Errorf("treino slug=%d nome=%d caracteres, want 63/255", len(treino.Slug), len([]rune(treino.NomeFantasia)))
	}
}

func TestListarEmpresasPlataforma_SoMetadado(t *testing.T) {
	db := testDB(t)
	comParLimpo(t, db, "plat-lista")

	empresa, treino, err := CriarEmpresaComTreinamento(db, testEmailCfg, novaEmpresaTeste("plat-lista", "990123450001", "Cliente Lista"))
	if err != nil {
		t.Fatalf("CriarEmpresaComTreinamento: %v", err)
	}

	lista, err := ListarEmpresasPlataforma(db)
	if err != nil {
		t.Fatalf("ListarEmpresasPlataforma: %v", err)
	}
	var achada *EmpresaResumo
	for i := range lista {
		if lista[i].ID == treino.ID {
			t.Error("o Treinamento aparece como Empresa de primeiro nível na listagem")
		}
		if lista[i].ID == empresa.ID {
			achada = &lista[i]
		}
	}
	if achada == nil {
		t.Fatal("Empresa criada ausente da listagem")
	}
	if achada.NomeFantasia != "Cliente Lista" || achada.CNPJ != empresa.CNPJ || achada.Status != StatusEmpresaAtiva || achada.CriadoEm.IsZero() {
		t.Errorf("resumo = %+v", achada)
	}
	if achada.Adm == nil || achada.Adm.Nome != "Ana Administradora" || achada.Adm.Email != "ana.adm@cliente.com" {
		t.Errorf("adm = %+v", achada.Adm)
	}
	if achada.Treinamento == nil || achada.Treinamento.Slug != "plat-lista-treinamento" || achada.Treinamento.Status != StatusEmpresaAtiva {
		t.Errorf("treinamento = %+v", achada.Treinamento)
	}

	// A forma serializada carrega SÓ metadado — nenhuma chave de conteúdo.
	bruto, err := json.Marshal(achada)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var campos map[string]json.RawMessage
	if err := json.Unmarshal(bruto, &campos); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var chaves []string
	for k := range campos {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	want := "adm,cnpj,criadoEm,endereco,id,nomeFantasia,razaoSocial,slug,status,treinamento"
	if got := strings.Join(chaves, ","); got != want {
		t.Errorf("chaves do resumo = %s, want %s", got, want)
	}
}

// TestAlterarStatusEmpresa_DesativaEReativaOPar prova a AC 5 no service: o par
// muda junto, as sessões das duas Empresas caem, nada é apagado e o slug
// volta a resolver na reativação.
func TestAlterarStatusEmpresa_DesativaEReativaOPar(t *testing.T) {
	db := testDB(t)
	comParLimpo(t, db, "plat-status")

	empresa, treino, err := CriarEmpresaComTreinamento(db, testEmailCfg, novaEmpresaTeste("plat-status", "901234560002", "Cliente Status"))
	if err != nil {
		t.Fatalf("CriarEmpresaComTreinamento: %v", err)
	}

	// Uma sessão por conta das duas Empresas, e uma sessão de outra Empresa
	// que NÃO pode ser tocada.
	var refreshes []string
	for _, e := range []Empresa{empresa, treino} {
		a := lerAdmPrimeiroAcesso(t, db, e.ID)
		_, refresh, _, err := EmitirSessao(db, segredoJWTPlataformaTeste, a.id, "senha")
		if err != nil {
			t.Fatalf("EmitirSessao: %v", err)
		}
		refreshes = append(refreshes, refresh)
	}
	var outroID string
	if err := db.QueryRow(`INSERT INTO usuarios (nome, email, papel, email_verificado, empresa_id)
		VALUES ('Outra', 'outra@empresa-teste.com', 'usuario', true, $1) RETURNING id`, empresaTeste).Scan(&outroID); err != nil {
		t.Fatalf("criar conta de outra empresa: %v", err)
	}
	if _, _, _, err := EmitirSessao(db, segredoJWTPlataformaTeste, outroID, "senha"); err != nil {
		t.Fatalf("EmitirSessao (outra empresa): %v", err)
	}

	if err := AlterarStatusEmpresa(db, empresa.ID, StatusEmpresaInativa); err != nil {
		t.Fatalf("desativar: %v", err)
	}
	if n := contar(t, db, `SELECT count(*) FROM empresas WHERE id IN ($1, $2) AND status = 'inativa'`, empresa.ID, treino.ID); n != 2 {
		t.Errorf("empresas inativas do par = %d, want 2", n)
	}
	for _, slug := range []string{empresa.Slug, treino.Slug} {
		if _, err := BuscarEmpresaPorSlug(db, slug); !errors.Is(err, ErrEmpresaNaoEncontrada) {
			t.Errorf("BuscarEmpresaPorSlug(%s) com a Empresa desativada: erro = %v, want ErrEmpresaNaoEncontrada", slug, err)
		}
	}
	if n := contar(t, db, `SELECT count(*) FROM sessoes s JOIN usuarios u ON u.id = s.usuario_id
		WHERE u.empresa_id IN ($1, $2) AND s.revogado_em IS NULL`, empresa.ID, treino.ID); n != 0 {
		t.Errorf("%d sessões do par continuam ativas", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM sessoes WHERE usuario_id = $1 AND revogado_em IS NULL`, outroID); n != 1 {
		t.Errorf("sessões ativas da outra Empresa = %d, want 1 (intocada)", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM produtos WHERE empresa_id = $1`, treino.ID); n != 5 {
		t.Errorf("produtos do treino após desativar = %d, want 5 (nada é apagado)", n)
	}
	if n := contar(t, db, `SELECT count(*) FROM usuarios WHERE empresa_id IN ($1, $2)`, empresa.ID, treino.ID); n != 2 {
		t.Errorf("contas do par após desativar = %d, want 2", n)
	}

	if err := AlterarStatusEmpresa(db, empresa.ID, StatusEmpresaAtiva); err != nil {
		t.Fatalf("reativar: %v", err)
	}
	for _, slug := range []string{empresa.Slug, treino.Slug} {
		if _, err := BuscarEmpresaPorSlug(db, slug); err != nil {
			t.Errorf("BuscarEmpresaPorSlug(%s) após reativar: %v", slug, err)
		}
	}
}

func TestAlterarStatusEmpresa_IdNaoEncontrado(t *testing.T) {
	db := testDB(t)
	comParLimpo(t, db, "plat-status-404")

	_, treino, err := CriarEmpresaComTreinamento(db, testEmailCfg, novaEmpresaTeste("plat-status-404", "912345670002", "Cliente 404"))
	if err != nil {
		t.Fatalf("CriarEmpresaComTreinamento: %v", err)
	}
	for _, id := range []string{treino.ID, "00000000-0000-4000-8000-000000000000", "nao-e-uuid", ""} {
		if err := AlterarStatusEmpresa(db, id, StatusEmpresaInativa); !errors.Is(err, ErrEmpresaNaoEncontrada) {
			t.Errorf("AlterarStatusEmpresa(%q): erro = %v, want ErrEmpresaNaoEncontrada", id, err)
		}
	}
	if n := contar(t, db, `SELECT count(*) FROM empresas WHERE id = $1 AND status = 'ativa'`, treino.ID); n != 1 {
		t.Error("o Treinamento mudou de status por um id recusado")
	}
}

func TestRenderizarTemplate_PrimeiroAcesso(t *testing.T) {
	tpl, err := renderizarTemplate("primeiro_acesso", map[string]any{
		"nome":    "<Ana>",
		"empresa": "A&B Construções",
		"link":    "http://app.local/e/ab/redefinir-senha?token=xyz",
	})
	if err != nil {
		t.Fatalf("renderizarTemplate: %v", err)
	}
	if tpl.Assunto != "Seu acesso de administrador — stockflow" {
		t.Errorf("assunto = %q", tpl.Assunto)
	}
	for _, trecho := range []string{"&lt;Ana&gt;", "A&amp;B Construções", "http://app.local/e/ab/redefinir-senha?token=xyz", "7 dias", "Esqueci minha senha"} {
		if !strings.Contains(tpl.CorpoHTML, trecho) {
			t.Errorf("corpo sem %q", trecho)
		}
	}
}
