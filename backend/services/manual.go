package services

import (
	"fmt"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/goregular"
)

// Manual Online do sistema — feedback Ferreira Costa (direção, 2026-10-22):
// "Ajuda" deixa de ser um diálogo estático e passa a carregar o manual de
// acordo com o papel de quem está logado; o `adm` vê o manual completo e
// pode gerar um PDF para encaminhar aos usuários.
//
// Conteúdo FIXO e hardcoded (mesmo espírito de cmd/seed-fotos-treinamento ou
// dos 28 templates de nomenclatura padrão): cada seção espelha um item de
// navegação de `frontend/src/components/shell/nav-items.ts` — mesmo `id` e
// mesmo `PapelMinimo` (duplicação deliberada entre Go e TS, mesmo caso já
// documentado para RankPapel/rankPapel) — mais uma seção "visao-geral" sem
// item de menu correspondente, sempre visível a qualquer papel.
//
// Três funções no mesmo molde do Recibo do Pedido em PDF (Story 7.6,
// spec-7-6, GerarReciboPedidoPDF): SecoesManualParaPapel (filtra por papel,
// sem gopdf, testável com asserções Go simples) / RenderizarManualPDF (só
// desenha a partir das seções já filtradas, pura e determinística) /
// GerarManualPDF (as duas em sequência, sempre o manual COMPLETO — é o que
// BaixarManualPDFHandler chama, atrás de RequireRole(adm)).

// SecaoManual é uma seção do manual — um bloco de texto simples (sem
// markdown, sem HTML: o frontend renderiza cada parágrafo como um `<p>`, o
// PDF escreve cada parágrafo como uma linha/bloco, mesmo conteúdo nos dois
// lugares).
type SecaoManual struct {
	ID          string   `json:"id"`
	Titulo      string   `json:"titulo"`
	PapelMinimo string   `json:"papelMinimo"`
	Paragrafos  []string `json:"paragrafos"`
}

// secoesManual é a lista COMPLETA e fixa do manual, na ordem em que aparece
// — mesmo agrupamento visual de nav-items.ts (Catálogo, Pedidos, Estoque,
// Qualidade dos dados, Cadastros, Administração), com "Visão geral" antes de
// tudo e "Meu perfil" ao final.
var secoesManual = []SecaoManual{
	{
		ID:          "visao-geral",
		Titulo:      "Visão geral e papéis de acesso",
		PapelMinimo: PapelUsuario,
		Paragrafos: []string{
			"O stockflow organiza o controle de materiais excedentes em quatro papéis, cada um enxergando mais telas que o anterior: Usuário, Almoxarife, Gestor e Administrador (ADM).",
			"Usuário: busca produtos no Catálogo, monta um Carrinho e envia Pedidos de Retirada, e acompanha os próprios pedidos.",
			"Almoxarife: tudo que o Usuário faz, mais cadastro de produtos, lançamento de saldo em estoque, aprovação da fila de pedidos e as ferramentas de Qualidade dos Dados.",
			"Gestor: tudo que o Almoxarife faz, mais a Gestão de Usuários, emissão de Convites e a fila de Promoções de papel.",
			"Administrador (ADM): acesso completo — inclui Cadastros (categorias, templates de nome, filiais, centros de custo), Segurança da Empresa (exigir MFA), Log de Acesso, Solicitações LGPD, e o cadastro direto de novos usuários.",
			"Uma tela sem o papel necessário simplesmente não aparece no menu — nunca aparece desabilitada. Este manual segue a mesma regra: você só vê as seções das telas às quais já tem acesso.",
		},
	},
	{
		ID:          "produtos",
		Titulo:      "Catálogo — Produtos",
		PapelMinimo: PapelUsuario,
		Paragrafos: []string{
			"Lista todos os produtos cadastrados na Empresa, com busca por nome e filtros por categoria, estoque, \"com saldo\" e \"com foto\".",
			"Cada produto mostra o código sequencial (gerado automaticamente no cadastro), o nome, a categoria e a miniatura da foto de capa, quando existir.",
			"A busca compara o texto digitado contra o nome do produto de forma contígua — se o nome tiver muitas palavras parecidas (ex.: variações de um mesmo tipo de cabo), vale digitar um trecho mais específico do nome em vez de palavras soltas fora de ordem.",
			"Clicar num produto abre o detalhe completo: dimensões, unidade de medida, embalagem, saldo por estoque e o histórico de movimentações daquele item.",
		},
	},
	{
		ID:          "cadastrar-produto",
		Titulo:      "Catálogo — Cadastrar produto",
		PapelMinimo: PapelAlmoxarife,
		Paragrafos: []string{
			"Cadastra um produto novo: nome, categoria, template de nomenclatura (o formato guiado do nome, como \"CABO [TIPO] [TENSÃO] ...\"), unidade de medida e, opcionalmente, código do fornecedor, EAN-13, embalagem e dimensões.",
			"O código do produto é gerado automaticamente pelo servidor assim que o cadastro é salvo — não existe campo para digitar um código. Ao salvar, aparece uma janela confirmando \"Código: NNNNNN cadastrado\"; pressione Enter ou clique em OK para confirmar antes de continuar.",
			"Depois de cadastrar, é possível enviar uma foto do produto na mesma tela — o produto nasce sem saldo em estoque; o saldo inicial é lançado depois, em Estoque → Lançar saldo.",
		},
	},
	{
		ID:          "importar-planilha",
		Titulo:      "Catálogo — Importar planilha",
		PapelMinimo: PapelAlmoxarife,
		Paragrafos: []string{
			"Cadastra vários produtos de uma vez a partir de uma planilha (.xlsx), em vez de um de cada vez pelo formulário.",
			"O sistema valida cada linha antes de gravar e mostra um relatório de quais produtos foram criados e quais linhas tiveram erro, com o motivo de cada erro.",
		},
	},
	{
		ID:          "carrinho",
		Titulo:      "Catálogo — Carrinho",
		PapelMinimo: PapelUsuario,
		Paragrafos: []string{
			"Acumula produto + estoque + quantidade antes de virar um Pedido de Retirada — nada é debitado do saldo enquanto o item está só no carrinho.",
			"Um produto pode ser adicionado buscando pelo nome ou apontando a câmera do celular para o código do produto.",
			"Se o produto escolhido for mesclado (duplicata) ou inativado, ou se o estoque escolhido for excluído enquanto o item ainda estava no carrinho, o item some automaticamente da lista com um aviso explicando o motivo.",
		},
	},
	{
		ID:          "meus-pedidos",
		Titulo:      "Pedidos — Meus pedidos",
		PapelMinimo: PapelUsuario,
		Paragrafos: []string{
			"Envia o carrinho atual como um Pedido de Retirada, informando solicitante, obra/centro de custo e uma observação opcional.",
			"Mostra todos os pedidos já enviados e o status de cada um: pendente, aprovado, parcialmente aprovado ou rejeitado.",
			"Um pedido já decidido (aprovado ou parcialmente aprovado) tem um recibo em PDF para baixar, com os itens efetivamente retirados.",
		},
	},
	{
		ID:          "fila-aprovacao",
		Titulo:      "Pedidos — Fila de aprovação",
		PapelMinimo: PapelAlmoxarife,
		Paragrafos: []string{
			"Lista os Pedidos de Retirada pendentes de toda a Empresa (ou só os seus, conforme o filtro) para aprovar ou rejeitar.",
			"Ao aprovar, o sistema confere o saldo disponível de cada item no momento da decisão — se o saldo físico for menor que o pedido, a aprovação sai parcial automaticamente, nunca falha silenciosamente.",
			"Itens cujo produto foi inativado depois do envio aparecem marcados como \"Inativo\", mas continuam aprováveis normalmente.",
		},
	},
	{
		ID:          "locais",
		Titulo:      "Estoque — Locais",
		PapelMinimo: PapelAlmoxarife,
		Paragrafos: []string{
			"Cadastra e mantém os Estoques (locais físicos de armazenamento) da Empresa — cada um pertence a uma Filial.",
			"Um Estoque só pode ser excluído se não tiver saldo nem pedido pendente vinculado a ele.",
		},
	},
	{
		ID:          "lancar-saldo",
		Titulo:      "Estoque — Lançar saldo",
		PapelMinimo: PapelAlmoxarife,
		Paragrafos: []string{
			"Registra entrada de saldo físico de um produto num estoque — busca o produto pelo nome ou código, escolhe o estoque e informa a quantidade na unidade de medida do próprio produto (mostrada ao lado do campo).",
			"Quando o produto tem \"Embalagem\" preenchida (ex.: \"rolo de 100m\"), isso é só uma informação de referência — a quantidade lançada é sempre na unidade de medida do produto, nunca multiplicada pela embalagem.",
			"Produtos com controle de lote/validade pedem também o número do lote e a data de validade; a saída de estoque (aprovação de Pedido) sempre consome primeiro o lote que vence mais cedo (FEFO).",
		},
	},
	{
		ID:          "movimentacoes",
		Titulo:      "Estoque — Movimentações",
		PapelMinimo: PapelAlmoxarife,
		Paragrafos: []string{
			"Histórico de toda entrada e saída de saldo de cada produto, com data, tipo de movimentação, quantidade e o código do produto ao lado do nome.",
			"Serve para conferir de onde veio ou para onde foi um saldo específico — cada baixa por aprovação de Pedido aparece vinculada ao Pedido que a originou.",
		},
	},
	{
		ID:          "inconsistencias",
		Titulo:      "Qualidade dos dados — Inconsistências",
		PapelMinimo: PapelAlmoxarife,
		Paragrafos: []string{
			"Detecta produtos com dados dimensionais incompletos ou inconsistentes (ex.: uma dimensão preenchida sem unidade, ou valores fora do esperado para o tipo de produto).",
		},
	},
	{
		ID:          "duplicatas",
		Titulo:      "Qualidade dos dados — Duplicatas",
		PapelMinimo: PapelAlmoxarife,
		Paragrafos: []string{
			"Sugere grupos de produtos que parecem ser o mesmo item cadastrado mais de uma vez (nomes muito parecidos) e permite mesclar os duplicados num só.",
			"A mesclagem transfere o saldo e o histórico dos produtos removidos para o produto escolhido como principal; produtos mesclados nunca podem ser restaurados, mas continuam visíveis no histórico de quem já os tinha no carrinho ou em pedidos antigos.",
		},
	},
	{
		ID:          "categorias",
		Titulo:      "Cadastros — Categorias",
		PapelMinimo: PapelAdm,
		Paragrafos: []string{
			"Lista fixa de categorias de produto usada no cadastro — todo produto precisa de uma categoria, escolhida sempre desta lista (nunca um texto livre).",
		},
	},
	{
		ID:          "templates",
		Titulo:      "Cadastros — Templates de nome",
		PapelMinimo: PapelAdm,
		Paragrafos: []string{
			"Define o formato esperado do nome de cada tipo de produto (Nomenclatura Guiada) — ex.: \"CABO [TIPO] [TENSÃO] Ø[SEÇÃO]MM² [COR]\". O cadastro de produto mostra esse formato como orientação ao digitar o nome.",
			"O template \"Genérico\" ([NOME LIVRE]) aceita qualquer nome, sem formato fixo — útil para produtos que não se encaixam nos templates técnicos. É possível criar outros templates com esse mesmo marcador [NOME LIVRE] sob um rótulo diferente (ex.: \"Migração\", para produtos vindos do cadastro antigo).",
			"Cada template pode ter um exemplo preenchido, mostrado junto do formato para deixar mais claro como usar.",
		},
	},
	{
		ID:          "filiais",
		Titulo:      "Cadastros — Filiais",
		PapelMinimo: PapelAdm,
		Paragrafos: []string{
			"Cadastra as Filiais da Empresa — toda Empresa nasce com uma Filial padrão, e cada Estoque pertence a uma Filial.",
		},
	},
	{
		ID:          "centros-custo",
		Titulo:      "Cadastros — Centros de custo",
		PapelMinimo: PapelAdm,
		Paragrafos: []string{
			"Lista fixa de obras/centros de custo que pode ser escolhida no envio de um Pedido de Retirada, em vez de digitar o nome da obra livremente.",
		},
	},
	{
		ID:          "usuarios",
		Titulo:      "Administração — Usuários",
		PapelMinimo: PapelGestor,
		Paragrafos: []string{
			"Lista as contas da Empresa — um Gestor vê só contas Usuário/Almoxarife; um ADM vê todas, inclusive Gestor e ADM.",
			"Permite desativar uma conta, rebaixar um papel (um degrau por vez) e resetar o MFA de uma conta travada.",
			"Só o ADM tem a seção \"Cadastrar usuário\": cria uma conta direto, sem convite, escolhendo nome, e-mail, senha inicial e QUALQUER papel (inclusive outro ADM), além de decidir se aquela conta específica é obrigada a configurar autenticação em duas etapas (2FA) — essa exigência vale independente do papel da conta e do que a Empresa pede no geral.",
		},
	},
	{
		ID:          "convites",
		Titulo:      "Administração — Convites",
		PapelMinimo: PapelGestor,
		Paragrafos: []string{
			"Gera um link de convite nominal (válido por 7 dias, uso único) para uma pessoa criar a própria conta — a conta sempre nasce com papel Usuário; para subir de papel depois, use Promoções.",
			"O link não é enviado por e-mail automaticamente: quem convida compartilha o link por fora (WhatsApp, por exemplo).",
		},
	},
	{
		ID:          "promocoes",
		Titulo:      "Administração — Promoções",
		PapelMinimo: PapelGestor,
		Paragrafos: []string{
			"Fila de solicitações de promoção de papel — qualquer conta pode solicitar subir um degrau (Usuário → Almoxarife → Gestor) em Meu Perfil.",
			"Um Gestor decide promoções até Almoxarife; só um ADM decide uma promoção para Gestor.",
		},
	},
	{
		ID:          "seguranca-empresa",
		Titulo:      "Administração — Segurança da empresa (MFA)",
		PapelMinimo: PapelAdm,
		Paragrafos: []string{
			"Liga ou desliga a exigência de autenticação em duas etapas (MFA/TOTP) para todas as contas Gestor e ADM da Empresa — enquanto estiver ligada, uma conta desse nível sem MFA configurado é bloqueada em qualquer ação restrita até configurar.",
			"Essa exigência é por Empresa inteira; para exigir MFA de uma conta específica, independente do papel dela, use \"Cadastrar usuário\" (Administração → Usuários).",
		},
	},
	{
		ID:          "log-acesso",
		Titulo:      "Administração — Log de acesso",
		PapelMinimo: PapelAdm,
		Paragrafos: []string{
			"Auditoria de eventos de segurança da conta: login, troca de senha, configuração/reset de MFA, entre outros.",
		},
	},
	{
		ID:          "lgpd",
		Titulo:      "Administração — Solicitações LGPD",
		PapelMinimo: PapelAdm,
		Paragrafos: []string{
			"Lista pedidos de exclusão de conta feitos pelos próprios usuários (direito de exclusão de dados pessoais) para o ADM decidir.",
		},
	},
	{
		ID:          "perfil",
		Titulo:      "Meu perfil",
		PapelMinimo: PapelUsuario,
		Paragrafos: []string{
			"Mostra seus próprios dados (nome, e-mail, papel) e o botão \"Solicitar promoção\", quando há um papel acima do seu disponível para pedir.",
			"Em Segurança, qualquer conta pode configurar a própria autenticação em duas etapas (2FA) de forma voluntária, mesmo que a Empresa não exija — e desligar depois, exceto quando a Empresa ou o ADM exigirem especificamente para aquela conta.",
			"O botão \"Sair\" da conta fica nesta página.",
		},
	},
}

// SecoesManualParaPapel devolve as seções do manual visíveis para `papel` —
// a MESMA regra de filtrarNavPorPapel (nav-items.ts): mantém a seção cujo
// PapelMinimo o papel alcança na ordem total (RankPapel), na ordem fixa de
// secoesManual. Um ADM recebe o manual COMPLETO (todas as seções); um papel
// desconhecido/vazio não recebe nenhuma (rank 0 nunca alcança nada).
func SecoesManualParaPapel(papel string) []SecaoManual {
	rank := RankPapel(papel)
	visiveis := make([]SecaoManual, 0, len(secoesManual))
	for _, s := range secoesManual {
		if rank >= RankPapel(s.PapelMinimo) {
			visiveis = append(visiveis, s)
		}
	}
	return visiveis
}

// alturaPaginaManualPT / margemManualPT — mesmos valores de
// alturaPaginaReciboPT/margemReciboPT (gopdf.PageSizeA4, 595x842pt).
const (
	alturaPaginaManualPT = 842.0
	margemManualPT       = 40.0
	larguraUtilManualPT  = 595.0 - 2*margemManualPT
)

// RenderizarManualPDF desenha o manual em PDF a partir de uma lista de
// seções JÁ FILTRADA — função pura e determinística, mesmo molde de
// RenderizarReciboPedidoPDF (nenhuma metadata dinâmica, nenhum time.Now():
// o PDF do manual completo é sempre byte-idêntico). O parágrafo é quebrado
// em várias linhas quando ultrapassa a largura útil da página, por contagem
// de caracteres (gopdf não mede largura de texto por si só sem abrir a
// fonte a cada chamada — aproximação suficiente para o texto corrido deste
// manual, nunca para números/tabelas).
func RenderizarManualPDF(secoes []SecaoManual) ([]byte, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.SetMargins(margemManualPT, margemManualPT, margemManualPT, margemManualPT)
	pdf.AddPage()

	if err := pdf.AddTTFFontData("gofont", goregular.TTF); err != nil {
		return nil, fmt.Errorf("falha ao carregar fonte do manual: %w", err)
	}

	escreverLinha := func(texto string, tamanho float64) error {
		if err := pdf.SetFont("gofont", "", tamanho); err != nil {
			return fmt.Errorf("falha ao selecionar fonte do manual: %w", err)
		}
		altura := tamanho + 6
		if pdf.GetY()+altura > alturaPaginaManualPT-margemManualPT {
			pdf.AddPage()
		}
		if err := pdf.Cell(&gopdf.Rect{W: larguraUtilManualPT, H: altura}, texto); err != nil {
			return fmt.Errorf("falha ao escrever linha do manual: %w", err)
		}
		pdf.Br(altura)
		return nil
	}

	// ~95 caracteres cabem na largura útil em corpo 10pt com a fonte
	// gofont — aproximação por contagem, não por medição real de glifo.
	const larguraMaximaCaracteres = 95
	escreverParagrafo := func(texto string) error {
		for _, linha := range quebrarEmLinhas(texto, larguraMaximaCaracteres) {
			if err := escreverLinha(linha, 10); err != nil {
				return err
			}
		}
		return nil
	}

	if err := escreverLinha("Manual do stockflow", 20); err != nil {
		return nil, err
	}
	pdf.Br(10)

	for _, secao := range secoes {
		if err := escreverLinha(secao.Titulo, 14); err != nil {
			return nil, err
		}
		pdf.Br(4)
		for _, p := range secao.Paragrafos {
			if err := escreverParagrafo(p); err != nil {
				return nil, err
			}
			pdf.Br(4)
		}
		pdf.Br(8)
	}

	bytes, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		return nil, fmt.Errorf("falha ao gerar bytes do PDF do manual: %w", err)
	}
	return bytes, nil
}

// quebrarEmLinhas quebra `texto` em linhas de até `max` caracteres,
// cortando em espaços (nunca no meio de uma palavra) — função pura, só
// contagem de runas.
func quebrarEmLinhas(texto string, max int) []string {
	var linhas []string
	var atual []rune
	var palavraAtual []rune
	flush := func() {
		if len(palavraAtual) > 0 {
			if len(atual) > 0 {
				atual = append(atual, ' ')
			}
			atual = append(atual, palavraAtual...)
			palavraAtual = nil
		}
	}
	for _, r := range texto {
		if r == ' ' {
			if len(atual)+len(palavraAtual)+1 > max && len(atual) > 0 {
				linhas = append(linhas, string(atual))
				atual = nil
			}
			flush()
			continue
		}
		palavraAtual = append(palavraAtual, r)
	}
	if len(atual)+len(palavraAtual)+1 > max && len(atual) > 0 {
		linhas = append(linhas, string(atual))
		atual = nil
	}
	flush()
	if len(atual) > 0 {
		linhas = append(linhas, string(atual))
	}
	return linhas
}

// GerarManualPDF compõe SecoesManualParaPapel(PapelAdm) + RenderizarManualPDF
// em sequência — SEMPRE o manual COMPLETO, independente de quem chamou: só
// existe atrás de RequireRole(adm) (BaixarManualPDFHandler), então "gerar o
// PDF" já pressupõe querer o manual inteiro para encaminhar a qualquer
// usuário.
func GerarManualPDF() ([]byte, error) {
	return RenderizarManualPDF(SecoesManualParaPapel(PapelAdm))
}
