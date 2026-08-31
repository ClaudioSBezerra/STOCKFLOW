package services

import "testing"

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

// TestListarNomenclaturaTemplates_Todas28OrdenadasPorSubtipo prova a AC dos
// 28 templates de seed (migração 000013), ordenados por `subtipo` ascendente.
func TestListarNomenclaturaTemplates_Todas28OrdenadasPorSubtipo(t *testing.T) {
	db := testDB(t)

	templates, err := ListarNomenclaturaTemplates(db)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(templates) != 28 {
		t.Fatalf("len = %d, want 28", len(templates))
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
}
