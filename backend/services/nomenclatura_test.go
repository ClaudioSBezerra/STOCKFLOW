package services

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/lib/pq"
)

// TestNomeValidoParaTemplate_SemToken prova que um template sem nenhum
// placeholder exige casamento exato de `nome` — não é "qualquer texto que
// contenha o template", é igualdade string a string.
func TestNomeValidoParaTemplate_SemToken(t *testing.T) {
	template := "TUBO PEAD"
	if !nomeValidoParaTemplate(template, "TUBO PEAD") {
		t.Error("nome idêntico ao template deveria ser válido")
	}
	if nomeValidoParaTemplate(template, "TUBO PEAD EXTRA") {
		t.Error("nome com sufixo extra não deveria casar template sem token")
	}
	if nomeValidoParaTemplate(template, "TUBO") {
		t.Error("nome parcial não deveria casar template sem token")
	}
}

// TestNomeValidoParaTemplate_UmToken prova o caso de um único placeholder
// preenchido corretamente.
func TestNomeValidoParaTemplate_UmToken(t *testing.T) {
	template := "TUBO PEAD [PN] DN[XX]"
	if !nomeValidoParaTemplate(template, "TUBO PEAD PN80 DN50") {
		t.Error("nome preenchendo os dois placeholders deveria ser válido")
	}
}

// TestNomeValidoParaTemplate_VariosTokensPreenchidos prova o exemplo da spec
// (Design Notes): template `CABO [TIPO] [TENSÃO]`, nome com os dois
// segmentos preenchidos -> válido.
func TestNomeValidoParaTemplate_VariosTokensPreenchidos(t *testing.T) {
	template := "CABO [TIPO] [TENSÃO]"
	if !nomeValidoParaTemplate(template, "CABO PP 220V") {
		t.Error("nome = \"CABO PP 220V\" deveria casar template com dois tokens")
	}
}

// TestNomeValidoParaTemplate_PlaceholderFaltando prova o exemplo da spec:
// `nome = "CABO 220V"` não casa o template `CABO [TIPO] [TENSÃO]` — falta um
// segmento (só um espaço/token preenchido em vez de dois).
func TestNomeValidoParaTemplate_PlaceholderFaltando(t *testing.T) {
	template := "CABO [TIPO] [TENSÃO]"
	if nomeValidoParaTemplate(template, "CABO 220V") {
		t.Error("nome com placeholder faltando não deveria ser válido")
	}
}

// TestNomeValidoParaTemplate_PlaceholderVazio prova que um placeholder
// "preenchido" só com espaços (ou string vazia entre dois literais
// adjacentes) é inválido — o grupo capturado, após TrimSpace, fica vazio.
func TestNomeValidoParaTemplate_PlaceholderVazio(t *testing.T) {
	template := "CABO [TIPO] [TENSÃO]"
	if nomeValidoParaTemplate(template, "CABO   220V") {
		t.Error("placeholder preenchido só com espaço deveria ser inválido")
	}
}

// TestNomeValidoParaTemplate_ForaDeOrdem prova que trocar a ordem estrutural
// dos segmentos literais do template invalida o nome — o template
// `[PEÇA] PVC [CLASSE] DN[XX] [COR]` exige "PVC" logo após o primeiro
// placeholder; um nome que não respeita essa estrutura não casa.
func TestNomeValidoParaTemplate_ForaDeOrdem(t *testing.T) {
	template := "[PEÇA] PVC [CLASSE] DN[XX] [COR]"
	if !nomeValidoParaTemplate(template, "JOELHO PVC SOLDÁVEL DN50 BRANCO") {
		t.Fatal("nome bem formado deveria ser válido (checagem de sanidade)")
	}
	// "PVC" movido para o fim: a estrutura literal fixa não bate mais.
	if nomeValidoParaTemplate(template, "JOELHO SOLDÁVEL DN50 BRANCO PVC") {
		t.Error("nome com estrutura fora de ordem não deveria casar o template")
	}
}

// TestNomeValidoParaTemplate_CaracteresEspeciaisEscapados prova que
// caracteres com significado especial em regex presentes no texto fixo do
// template (Ø, ², =) são tratados como literais (via regexp.QuoteMeta), não
// como metacaracteres — sem QuoteMeta, "²" ou "=" não quebrariam a regex (não
// são metacaracteres de regex), mas o teste cobre a combinação completa do
// template real do addendum §G que usa esses símbolos.
func TestNomeValidoParaTemplate_CaracteresEspeciaisEscapados(t *testing.T) {
	template := "CABO [TIPO] [TENSÃO] Ø[SEÇÃO]MM² [COR] [COMPLEMENTO]"
	if !nomeValidoParaTemplate(template, "CABO PP 220V Ø2,5MM² PRETO FLEXÍVEL") {
		t.Error("nome preenchendo todos os placeholders do template com Ø/² deveria ser válido")
	}
	if nomeValidoParaTemplate(template, "CABO PP 220V 2,5MM² PRETO FLEXÍVEL") {
		t.Error("nome sem o símbolo Ø literal não deveria casar o template")
	}

	templateComIgual := "BARRA ROSCADA [MATERIAL/ACAB] [BITOLA] L=[XX]M"
	if !nomeValidoParaTemplate(templateComIgual, "BARRA ROSCADA GALVANIZADA 3/8 L=1M") {
		t.Error("nome preenchendo o template com '=' literal deveria ser válido")
	}
}

// TestNomeValidoParaTemplate_PlaceholderComQuebraDeLinha prova que um
// placeholder preenchido com texto contendo uma quebra de linha ainda casa o
// template (RE2/Go regexp não casa `\n` com `.` por padrão — sem a flag
// `(?s)`, um nome assim seria rejeitado mesmo preenchendo o placeholder
// corretamente; CriarProduto não proíbe `\n` em `nome`, só limita o tamanho).
func TestNomeValidoParaTemplate_PlaceholderComQuebraDeLinha(t *testing.T) {
	template := "CABO [TIPO] [TENSÃO]"
	if !nomeValidoParaTemplate(template, "CABO P\nP 220V") {
		t.Error("placeholder preenchido com quebra de linha deveria ser válido")
	}
}

// TestListarNomenclaturaTemplates_Todas29OrdenadasPorSubtipo prova a AC dos
// 28 templates de seed (migração 000013) + o template Genérico acrescentado
// pela Story 10.1 (migration 000036, AD-34) = 29, ordenados por `subtipo`
// ascendente.
func TestListarNomenclaturaTemplates_Todas29OrdenadasPorSubtipo(t *testing.T) {
	db := testDB(t)

	templates, err := ListarNomenclaturaTemplates(db, empresaTeste)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(templates) != 29 {
		t.Fatalf("len = %d, want 29", len(templates))
	}
	for i := 1; i < len(templates); i++ {
		if templates[i-1].Subtipo >= templates[i].Subtipo {
			t.Errorf("ordem quebrada em %d: %q >= %q", i, templates[i-1].Subtipo, templates[i].Subtipo)
		}
	}
	for _, tpl := range templates {
		if tpl.ID == "" || tpl.Subtipo == "" || tpl.Template == "" {
			t.Errorf("template com campo vazio: %+v", tpl)
		}
	}
	var achouGenerico bool
	for _, tpl := range templates {
		if tpl.Subtipo == "Genérico" {
			achouGenerico = true
			if tpl.Template != TemplateGenericoMarcador {
				t.Errorf("template do subtipo Genérico = %q, want %q", tpl.Template, TemplateGenericoMarcador)
			}
		}
	}
	if !achouGenerico {
		t.Error("subtipo Genérico não encontrado na lista")
	}
}

// --- Story 10.1: template Genérico ([NOME LIVRE], AD-34) --------------------

// TestNomeValidoParaTemplate_MarcadorGenericoAceitaQualquerNomeNaoVazio prova
// o branch do marcador `[NOME LIVRE]` em nomeValidoParaTemplate: qualquer
// nome não vazio (após trim) é aceito, independente de formato/estrutura —
// nunca casado contra tokens/regex.
func TestNomeValidoParaTemplate_MarcadorGenericoAceitaQualquerNomeNaoVazio(t *testing.T) {
	casos := []string{
		"qualquer nome livre",
		"TUBO PEAD PN80 DN50", // até um nome que casaria outro template estrutural
		"1234567890",
		"nome com\nquebra de linha",
	}
	for _, nome := range casos {
		if !nomeValidoParaTemplate(TemplateGenericoMarcador, nome) {
			t.Errorf("nomeValidoParaTemplate(%q, %q) = false, want true (marcador Genérico aceita qualquer nome não vazio)", TemplateGenericoMarcador, nome)
		}
	}
}

// TestNomeValidoParaTemplate_MarcadorGenericoRejeitaVazioOuSoEspaco prova que
// o branch do marcador Genérico ainda rejeita nome vazio/só espaço — "aceita
// qualquer nome" não é "aceita string vazia".
func TestNomeValidoParaTemplate_MarcadorGenericoRejeitaVazioOuSoEspaco(t *testing.T) {
	casos := []string{"", "   ", "\t\n"}
	for _, nome := range casos {
		if nomeValidoParaTemplate(TemplateGenericoMarcador, nome) {
			t.Errorf("nomeValidoParaTemplate(%q, %q) = true, want false", TemplateGenericoMarcador, nome)
		}
	}
}

// --- Story 10.6: CRUD de Templates de Nomenclatura (spec-10-6), DB real -----
//
// Templates criados aqui usam subtipos "T10.6 …" e são removidos ao fim de
// cada teste. Os testes do fallback [NOME LIVRE] usam uma Empresa própria,
// para poder mutar/excluir o "Genérico" sem afetar as demais suítes.

func limparTemplatesDeTeste(t *testing.T, db *sql.DB) {
	t.Helper()
	del := func() {
		limparProdutos(t, db) // Produtos de teste referenciam Templates "T10.6…" (FK)
		if _, err := db.Exec(`DELETE FROM nomenclatura_templates WHERE subtipo LIKE 'T10.6%'`); err != nil {
			t.Fatalf("limpar templates de teste: %v", err)
		}
	}
	del()
	t.Cleanup(del)
}

func empresaDeTemplates(t *testing.T, db *sql.DB, slug, cnpjBase12 string) Empresa {
	t.Helper()
	removerEmpresaDeTeste(t, db, slug)
	t.Cleanup(func() { removerEmpresaDeTeste(t, db, slug) })
	return criarEmpresaDeTeste(t, db, slug, cnpjBase12, "Templates "+slug)
}

func contarTemplatesDaEmpresa(t *testing.T, db *sql.DB, empresaID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM nomenclatura_templates WHERE empresa_id = $1`, empresaID).Scan(&n); err != nil {
		t.Fatalf("count templates: %v", err)
	}
	return n
}

func idTemplateGenerico(t *testing.T, db *sql.DB, empresaID string) string {
	t.Helper()
	return templateGenericoID(t, db, empresaID)
}

const idAusenteTemplate = "00000000-0000-4000-8000-000000000000"

func TestValidarTemplateNomenclatura(t *testing.T) {
	validos := []string{
		"CABO [TIPO] [BITOLA]",
		"[NOME LIVRE]",
		"[PEÇA] PVC",
		"TUBO INOX [NORMA/LIGA] Ø[XX]MM [COMP]",
		"CHUMBADOR [TIPO: J/CBA/EXP] [BITOLA]",
		strings.Repeat("a", 244) + " [TIPO]",
	}
	for _, tpl := range validos {
		if _, got, err := validarTemplateNomenclatura("Sub", "  "+tpl+"  "); err != nil || got != tpl {
			t.Errorf("template %q: got=%q err=%v, want válido e trimado", tpl, got, err)
		}
	}
	invalidos := map[string][2]string{
		"subtipo vazio":     {"   ", "CABO [TIPO]"},
		"template vazio":    {"Sub", "   "},
		"ambos ausentes":    {"", ""},
		"sem token":         {"Sub", "CABO SEM TOKEN"},
		"colchete aberto":   {"Sub", "CABO [TIPO"},
		"colchete fechado":  {"Sub", "CABO TIPO]"},
		"colchete solto":    {"Sub", "CABO [TIPO] ]"},
		"token só espaços":  {"Sub", "CABO [   ]"},
		"token vazio":       {"Sub", "CABO []"},
		"colchete interno":  {"Sub", "CABO [[TIPO]"},
		"marcador + texto":  {"Sub", "[NOME LIVRE] ]"},
		"NUL no subtipo":    {"Su\x00b", "CABO [TIPO]"},
		"NUL no template":   {"Sub", "CABO [TI\x00PO]"},
		"subtipo 256 runes": {strings.Repeat("ç", 256), "CABO [TIPO]"},
		"template 256":      {"Sub", strings.Repeat("ç", 250) + " [TIPO]"},
	}
	for nome, c := range invalidos {
		if _, _, err := validarTemplateNomenclatura(c[0], c[1]); !errors.Is(err, ErrTemplateValidacao) {
			t.Errorf("%s: erro = %v, want ErrTemplateValidacao", nome, err)
		}
	}
	// Limite exato de 255 runes multibyte é aceito.
	if _, _, err := validarTemplateNomenclatura(strings.Repeat("ç", 255), strings.Repeat("ç", 248)+" [TIPO]"); err != nil {
		t.Errorf("255 runes deveria passar: %v", err)
	}
}

func TestCriarNomenclaturaTemplate_Sucesso(t *testing.T) {
	db := testDB(t)
	limparTemplatesDeTeste(t, db)

	tpl, err := CriarNomenclaturaTemplate(db, empresaTeste, "  T10.6 Cabos — Especial ", "  CABO [TIPO] [BITOLA]  ")
	if err != nil {
		t.Fatalf("CriarNomenclaturaTemplate: %v", err)
	}
	if tpl.ID == "" || tpl.Subtipo != "T10.6 Cabos — Especial" || tpl.Template != "CABO [TIPO] [BITOLA]" {
		t.Errorf("template = %+v, want trimado e id preenchido", tpl)
	}
	var emp string
	if err := db.QueryRow(`SELECT empresa_id FROM nomenclatura_templates WHERE id = $1`, tpl.ID).Scan(&emp); err != nil || emp != empresaTeste {
		t.Errorf("empresa_id = %q (err=%v), want %q", emp, err, empresaTeste)
	}
}

func TestCriarNomenclaturaTemplate_Validacao(t *testing.T) {
	db := testDB(t)
	limparTemplatesDeTeste(t, db)
	antes := contarTemplatesDaEmpresa(t, db, empresaTeste)

	casos := map[string][2]string{
		"subtipo vazio":   {"   ", "CABO [TIPO]"},
		"template vazio":  {"T10.6 a", "   "},
		"sem token":       {"T10.6 b", "CABO"},
		"colchete aberto": {"T10.6 c", "CABO [TIPO"},
		"token em branco": {"T10.6 d", "CABO [ ]"},
		"NUL":             {"T10.6 e", "CABO [TI\x00PO]"},
		"subtipo longo":   {"T10.6 " + strings.Repeat("x", 250), "CABO [TIPO]"},
	}
	for nome, c := range casos {
		if _, err := CriarNomenclaturaTemplate(db, empresaTeste, c[0], c[1]); !errors.Is(err, ErrTemplateValidacao) {
			t.Errorf("%s: erro = %v, want ErrTemplateValidacao", nome, err)
		}
	}
	if n := contarTemplatesDaEmpresa(t, db, empresaTeste); n != antes {
		t.Errorf("templates = %d, want %d (nada gravado)", n, antes)
	}
}

// TestNomenclaturaTemplates_ChecksNoBanco prova o CHECK de não-vazio (migration
// 000040) direto no SQL, nas duas tabelas.
func TestNomenclaturaTemplates_ChecksNoBanco(t *testing.T) {
	db := testDB(t)
	limparTemplatesDeTeste(t, db)

	casos := map[string][2]string{
		"subtipo vazio":  {"  ", "CABO [TIPO]"},
		"template vazio": {"T10.6 chk", "  "},
	}
	for nome, c := range casos {
		_, err := db.Exec(`INSERT INTO nomenclatura_templates (subtipo, template, empresa_id) VALUES ($1, $2, $3)`, c[0], c[1], empresaTeste)
		var pqErr *pq.Error
		if !errors.As(err, &pqErr) || pqErr.Code != "23514" {
			t.Errorf("%s: erro = %v, want SQLSTATE 23514", nome, err)
		}
		_, err = db.Exec(`INSERT INTO nomenclatura_templates_padrao (subtipo, template) VALUES ($1, $2)`, c[0], c[1])
		if !errors.As(err, &pqErr) || pqErr.Code != "23514" {
			t.Errorf("padrao %s: erro = %v, want SQLSTATE 23514", nome, err)
		}
	}
}

func TestCriarNomenclaturaTemplate_MarcadorEDuplicado(t *testing.T) {
	db := testDB(t)
	limparTemplatesDeTeste(t, db)
	outra := empresaDeTemplates(t, db, "templates-outra-empresa", "106000000001")

	if _, err := CriarNomenclaturaTemplate(db, empresaTeste, "T10.6 Livre 2", TemplateGenericoMarcador); err != nil {
		t.Fatalf("segundo marcador deveria ser permitido: %v", err)
	}
	if _, err := CriarNomenclaturaTemplate(db, empresaTeste, "T10.6 Dup", "CABO [TIPO]"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := CriarNomenclaturaTemplate(db, empresaTeste, "T10.6 Dup", "OUTRO [X]"); !errors.Is(err, ErrTemplateDuplicado) {
		t.Errorf("duplicado: erro = %v, want ErrTemplateDuplicado", err)
	}
	// O subtipo seedado (Genérico) também colide.
	if _, err := CriarNomenclaturaTemplate(db, empresaTeste, "Genérico", "OUTRO [X]"); !errors.Is(err, ErrTemplateDuplicado) {
		t.Errorf("duplicado do seed: erro = %v, want ErrTemplateDuplicado", err)
	}
	// Mesmo subtipo em outra Empresa é permitido (isolamento).
	if _, err := CriarNomenclaturaTemplate(db, outra.ID, "T10.6 Dup", "CABO [TIPO]"); err != nil {
		t.Errorf("outra empresa deveria aceitar o mesmo subtipo: %v", err)
	}
}

func TestAtualizarNomenclaturaTemplate(t *testing.T) {
	db := testDB(t)
	limparTemplatesDeTeste(t, db)
	outra := empresaDeTemplates(t, db, "templates-outra-empresa", "106000000001")

	a, _ := CriarNomenclaturaTemplate(db, empresaTeste, "T10.6 A", "A [X]")
	CriarNomenclaturaTemplate(db, empresaTeste, "T10.6 B", "B [X]")
	alheio, err := CriarNomenclaturaTemplate(db, outra.ID, "T10.6 A", "A [X]")
	if err != nil {
		t.Fatalf("seed alheio: %v", err)
	}

	got, err := AtualizarNomenclaturaTemplate(db, empresaTeste, a.ID, " T10.6 A2 ", " A2 [X] [Y] ")
	if err != nil || got.ID != a.ID || got.Subtipo != "T10.6 A2" || got.Template != "A2 [X] [Y]" {
		t.Fatalf("atualizar = %+v, %v", got, err)
	}
	if _, err := AtualizarNomenclaturaTemplate(db, empresaTeste, a.ID, "T10.6 B", "A2 [X]"); !errors.Is(err, ErrTemplateDuplicado) {
		t.Errorf("duplicado: %v", err)
	}
	if _, err := AtualizarNomenclaturaTemplate(db, empresaTeste, a.ID, "T10.6 A2", "sem token"); !errors.Is(err, ErrTemplateValidacao) {
		t.Errorf("inválido: %v", err)
	}

	for nome, id := range map[string]string{
		"alheio":      alheio.ID,
		"inexistente": idAusenteTemplate,
		"malformado":  "nao-e-uuid",
		"id com NUL":  "0000\x00000",
	} {
		if _, err := AtualizarNomenclaturaTemplate(db, empresaTeste, id, "T10.6 Z", "Z [X]"); !errors.Is(err, ErrTemplateNaoEncontrado) {
			t.Errorf("%s: erro = %v, want ErrTemplateNaoEncontrado", nome, err)
		}
	}
	var sub string
	db.QueryRow(`SELECT subtipo FROM nomenclatura_templates WHERE id = $1`, alheio.ID).Scan(&sub)
	if sub != "T10.6 A" {
		t.Errorf("template alheio alterado: %q", sub)
	}
}

// TestAtualizarNomenclaturaTemplate_EmUsoNaoRetroativo prova que editar um
// Template em uso não toca `produtos` e que a regra nova vale na próxima
// renomeação.
func TestAtualizarNomenclaturaTemplate_EmUsoNaoRetroativo(t *testing.T) {
	db := testDB(t)
	limparTemplatesDeTeste(t, db)

	tpl, err := CriarNomenclaturaTemplate(db, empresaTeste, "T10.6 Uso", "TUBO [TIPO] [MEDIDA]")
	if err != nil {
		t.Fatalf("seed template: %v", err)
	}
	input := criarProdutoInputValido(t, db, "TUBO PVC 50MM", "04.001")
	input.TemplateID = tpl.ID
	p, err := CriarProduto(db, empresaTeste, input)
	if err != nil {
		t.Fatalf("seed produto: %v", err)
	}

	if _, err := AtualizarNomenclaturaTemplate(db, empresaTeste, tpl.ID, "T10.6 Uso", "FITA [COR] [LARGURA]"); err != nil {
		t.Fatalf("editar em uso: %v", err)
	}

	var nome, templateID string
	if err := db.QueryRow(`SELECT nome, template_id FROM produtos WHERE id = $1`, p.ID).Scan(&nome, &templateID); err != nil {
		t.Fatalf("ler produto: %v", err)
	}
	if nome != "TUBO PVC 50MM" || templateID != tpl.ID {
		t.Errorf("produto alterado: nome=%q template_id=%q", nome, templateID)
	}

	// A próxima renomeação valida contra o texto NOVO.
	var ev *ErroProdutoValidacao
	if _, err := AtualizarNomeProduto(db, empresaTeste, p.ID, "TUBO PVC 75MM"); !errors.As(err, &ev) {
		t.Errorf("renomear pelo padrão antigo: erro = %v, want ErroProdutoValidacao", err)
	}
	if _, err := AtualizarNomeProduto(db, empresaTeste, p.ID, "FITA AZUL 50MM"); err != nil {
		t.Errorf("renomear pelo padrão novo: %v", err)
	}
}

func TestExcluirNomenclaturaTemplate(t *testing.T) {
	db := testDB(t)
	limparTemplatesDeTeste(t, db)
	outra := empresaDeTemplates(t, db, "templates-outra-empresa", "106000000001")

	livre, _ := CriarNomenclaturaTemplate(db, empresaTeste, "T10.6 Livre", "L [X]")
	alheio, _ := CriarNomenclaturaTemplate(db, outra.ID, "T10.6 Livre", "L [X]")

	for nome, id := range map[string]string{
		"alheio":      alheio.ID,
		"inexistente": idAusenteTemplate,
		"malformado":  "nao-e-uuid",
		"id com NUL":  "0000\x00000",
	} {
		if err := ExcluirNomenclaturaTemplate(db, empresaTeste, id); !errors.Is(err, ErrTemplateNaoEncontrado) {
			t.Errorf("%s: erro = %v, want ErrTemplateNaoEncontrado", nome, err)
		}
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM nomenclatura_templates WHERE id = $1`, alheio.ID).Scan(&n)
	if n != 1 {
		t.Errorf("template alheio removido")
	}

	if err := ExcluirNomenclaturaTemplate(db, empresaTeste, livre.ID); err != nil {
		t.Fatalf("excluir sem uso: %v", err)
	}
	db.QueryRow(`SELECT count(*) FROM nomenclatura_templates WHERE id = $1`, livre.ID).Scan(&n)
	if n != 0 {
		t.Errorf("linha não removida")
	}
}

func TestExcluirNomenclaturaTemplate_EmUso(t *testing.T) {
	db := testDB(t)
	limparTemplatesDeTeste(t, db)

	tpl, _ := CriarNomenclaturaTemplate(db, empresaTeste, "T10.6 Em Uso", "TUBO [TIPO]")
	input := criarProdutoInputValido(t, db, "TUBO PVC 50MM", "04.001")
	input.TemplateID = tpl.ID
	if _, err := CriarProduto(db, empresaTeste, input); err != nil {
		t.Fatalf("seed produto: %v", err)
	}

	err := ExcluirNomenclaturaTemplate(db, empresaTeste, tpl.ID)
	var emUso *ErroTemplateEmUso
	if !errors.As(err, &emUso) || emUso.Produtos != 1 {
		t.Fatalf("erro = %v, want ErroTemplateEmUso{1}", err)
	}
	if !strings.Contains(err.Error(), "1 produto") {
		t.Errorf("mensagem sem contagem: %q", err.Error())
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM nomenclatura_templates WHERE id = $1`, tpl.ID).Scan(&n)
	if n != 1 {
		t.Errorf("template em uso foi removido")
	}
}

// TestNomenclaturaTemplates_FallbackGenerico cobre a proteção do único
// template-marcador [NOME LIVRE] da Empresa (AD-34).
func TestNomenclaturaTemplates_FallbackGenerico(t *testing.T) {
	db := testDB(t)
	e := empresaDeTemplates(t, db, "templates-fallback", "106000000002")
	genId := idTemplateGenerico(t, db, e.ID)

	// Único marcador: excluir e trocar o texto são bloqueados.
	if err := ExcluirNomenclaturaTemplate(db, e.ID, genId); !errors.Is(err, ErrTemplateFallbackObrigatorio) {
		t.Errorf("excluir único: erro = %v, want ErrTemplateFallbackObrigatorio", err)
	}
	if _, err := AtualizarNomenclaturaTemplate(db, e.ID, genId, "Genérico", "CABO [TIPO]"); !errors.Is(err, ErrTemplateFallbackObrigatorio) {
		t.Errorf("editar texto do único: erro = %v, want ErrTemplateFallbackObrigatorio", err)
	}
	var texto string
	db.QueryRow(`SELECT template FROM nomenclatura_templates WHERE id = $1`, genId).Scan(&texto)
	if texto != TemplateGenericoMarcador {
		t.Errorf("template do fallback alterado: %q", texto)
	}

	// Renomear só o subtipo é permitido (identificado pelo texto, não pelo subtipo).
	got, err := AtualizarNomenclaturaTemplate(db, e.ID, genId, "Livre", TemplateGenericoMarcador)
	if err != nil || got.Subtipo != "Livre" {
		t.Fatalf("renomear subtipo: %+v, %v", got, err)
	}
	if err := ExcluirNomenclaturaTemplate(db, e.ID, genId); !errors.Is(err, ErrTemplateFallbackObrigatorio) {
		t.Errorf("renomeado continua único: erro = %v", err)
	}

	// Com um segundo marcador, o primeiro é editável/excluível.
	segundo, err := CriarNomenclaturaTemplate(db, e.ID, "Livre 2", TemplateGenericoMarcador)
	if err != nil {
		t.Fatalf("segundo marcador: %v", err)
	}
	if _, err := AtualizarNomenclaturaTemplate(db, e.ID, genId, "Livre", "CABO [TIPO]"); err != nil {
		t.Errorf("editar com 2º marcador: %v", err)
	}
	// Agora só resta o segundo: passa a ser o fallback protegido.
	if err := ExcluirNomenclaturaTemplate(db, e.ID, segundo.ID); !errors.Is(err, ErrTemplateFallbackObrigatorio) {
		t.Errorf("excluir o novo único: erro = %v, want ErrTemplateFallbackObrigatorio", err)
	}
	// Restaura o marcador no primeiro: excluir o segundo passa.
	if _, err := AtualizarNomenclaturaTemplate(db, e.ID, genId, "Livre", TemplateGenericoMarcador); err != nil {
		t.Fatalf("restaurar marcador: %v", err)
	}
	if err := ExcluirNomenclaturaTemplate(db, e.ID, segundo.ID); err != nil {
		t.Errorf("excluir 2º marcador com 1º presente: %v", err)
	}
}

// TestExcluirNomenclaturaTemplate_FallbackAntesDeEmUso prova a ordem das
// checagens: o único marcador em uso devolve fallback obrigatório (não "em uso").
func TestExcluirNomenclaturaTemplate_FallbackAntesDeEmUso(t *testing.T) {
	db := testDB(t)
	limparTemplatesDeTeste(t, db)
	input := criarProdutoInputValido(t, db, "Produto Genérico Uso", "04.001")
	if _, err := CriarProduto(db, empresaTeste, input); err != nil {
		t.Fatalf("seed produto: %v", err)
	}
	err := ExcluirNomenclaturaTemplate(db, empresaTeste, input.TemplateID)
	if !errors.Is(err, ErrTemplateFallbackObrigatorio) {
		t.Errorf("erro = %v, want ErrTemplateFallbackObrigatorio", err)
	}
}

// TestNomenclaturaTemplates_ProvisionarCopiaPadrao prova que os CHECKs não
// quebram a cópia do molde para uma Empresa nova (29 templates).
func TestNomenclaturaTemplates_ProvisionarCopiaPadrao(t *testing.T) {
	db := testDB(t)
	e := empresaDeTemplates(t, db, "templates-provisionamento", "106000000003")
	if n := contarTemplatesDaEmpresa(t, db, e.ID); n != 29 {
		t.Errorf("templates copiados = %d, want 29", n)
	}
}
