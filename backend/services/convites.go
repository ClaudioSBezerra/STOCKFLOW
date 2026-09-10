package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"
)

// Convite nominal de acesso a uma Empresa — Story 9.3 (Epic 9, FR-42;
// AD-22), spec-9-3.
//
// A partir desta story o autocadastro deixa de ser aberto: `gestor`+ emite um
// convite para um e-mail específico e recebe um LINK para compartilhar (por
// WhatsApp de canteiro, tipicamente — o convite NUNCA é enviado por e-mail,
// ver Design Notes da spec). Sem um convite válido, `services.Cadastrar` não
// cria conta nenhuma.
//
// Invariantes (AD-22):
//
//   - NOMINAL: `Cadastrar` só aceita se normalizeEmail(form.email) ==
//     convites_empresa.email. Token certo com outro e-mail é recusado.
//   - USO ÚNICO: `usado_em` é marcado na MESMA transação do INSERT em
//     `usuarios`, com UPDATE condicional exigindo RowsAffected()==1 — dois
//     resgates simultâneos do mesmo token produzem exatamente uma conta.
//   - POR EMPRESA: TODA query filtra por `empresa_id`. Um token da Empresa A
//     usado sob o slug da Empresa B colapsa em ErrConviteNaoEncontrado —
//     nunca cria conta, nunca revela que o token existe.
//   - O convite NUNCA concede papel: a conta nasce sempre `usuario` (FR-42);
//     promoção continua sendo FR-33/Story 1.7.
//
// O gate de papel (gestor+) da emissão/listagem/revogação é do middleware
// (RequireRole), nunca deste pacote — as funções daqui recebem a Empresa já
// resolvida como argumento explícito (AD-8 forma 3, molde de ListarUsuarios).

// conviteExpiracao é a validade de um convite: 7 dias. Mesmo prazo do link de
// primeiro acesso do `adm` provisionado pelo Dono (tokenPrimeiroAcessoExpiracao,
// Story 9.2) — os dois são o mesmo tipo de evento (onboarding, não uma
// recuperação de 30min). O prazo é decisão de implementação (PRD FR-42 /
// Deferred da arquitetura).
const conviteExpiracao = 7 * 24 * time.Hour

// Situações derivadas de um convite (nunca uma coluna — ver o cabeçalho da
// migration 000034): a leitura resolve `revogado_em` -> revogado; `usado_em`
// -> usado; `expira_em <= now()` -> expirado; senão pendente. Assim um
// convite expira sozinho, sem job nenhum.
const (
	ConvitePendente = "pendente"
	ConviteUsado    = "usado"
	ConviteRevogado = "revogado"
	ConviteExpirado = "expirado"
)

var (
	// ErrConviteValidacao indica e-mail vazio, sem formato de e-mail ou maior
	// que a coluna VARCHAR(255) no payload de emissão. Handler -> 400
	// VALIDATION_ERROR, sem gravar nenhuma linha.
	ErrConviteValidacao = errors.New("informe um e-mail válido para o convite")
	// ErrConviteEmailJaCadastrado indica que o e-mail já tem conta NA MESMA
	// Empresa. Recusar na EMISSÃO é deliberado: sem este guard o convite
	// nasceria morto — o INSERT em `usuarios` bateria em
	// idx_usuarios_email_lower e a pessoa veria "e-mail já cadastrado" só
	// depois de abrir o link. Handler -> 409 CONFLICT.
	ErrConviteEmailJaCadastrado = errors.New("este e-mail já tem conta nesta empresa")
	// ErrConviteNaoEncontrado cobre token/id inexistente, malformado (não-UUID,
	// `pq` 22P02) e — deliberadamente — DE OUTRA EMPRESA: os três colapsam no
	// mesmo sentinela, para que a resposta nunca revele que um convite existe
	// em outra Empresa. Handler -> 404 NOT_FOUND.
	ErrConviteNaoEncontrado = errors.New("convite não encontrado")
	// ErrConviteExpirado indica `expira_em <= now()`. Handler -> 400
	// TOKEN_EXPIRED.
	ErrConviteExpirado = errors.New("este convite expirou")
	// ErrConviteJaUsado indica `usado_em` preenchido — o convite é de uso
	// único. Handler -> 409 CONFLICT.
	ErrConviteJaUsado = errors.New("este convite já foi utilizado")
	// ErrConviteRevogado indica `revogado_em` preenchido: a Empresa cancelou o
	// convite antes do resgate. Handler -> 403 FORBIDDEN, com mensagem
	// própria (o AC exige "mensagem clara do motivo" — por isso a linha é
	// preservada em vez de apagada).
	ErrConviteRevogado = errors.New("este convite foi cancelado pela empresa")
	// ErrConviteEmailDivergente indica token válido apresentado com um e-mail
	// diferente do que foi convidado — o convite é NOMINAL (AD-22). O convite
	// permanece intacto (nada é gravado). Handler -> 403 FORBIDDEN.
	ErrConviteEmailDivergente = errors.New("este convite foi emitido para outro e-mail")
)

// ConviteResumo é a projeção somente-leitura de um convite devolvida por
// EmitirConvite e ListarConvites. Sem `token`: ele só sai pelo `Link`, e só
// para convites `pendente` — um convite usado/revogado/expirado não tem link
// para compartilhar, então `Link` fica vazio e some do JSON (omitempty).
type ConviteResumo struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	Situacao      string    `json:"situacao"`
	ExpiraEm      time.Time `json:"expiraEm"`
	CriadoEm      time.Time `json:"criadoEm"`
	CriadoPorNome string    `json:"criadoPorNome"`
	Link          string    `json:"link,omitempty"`
}

// situacaoConvite deriva a situação de um convite a partir das três colunas
// que a determinam. Ordem deliberada: revogado e usado são estados FINAIS
// (uma vez neles, o convite nunca volta), enquanto "expirado" é só a
// passagem do tempo sobre um convite que continuava pendente.
func situacaoConvite(expiraEm time.Time, usadoEm, revogadoEm sql.NullTime, agora time.Time) string {
	switch {
	case revogadoEm.Valid:
		return ConviteRevogado
	case usadoEm.Valid:
		return ConviteUsado
	case !agora.Before(expiraEm):
		return ConviteExpirado
	default:
		return ConvitePendente
	}
}

// erroDaSituacaoConvite traduz a situação derivada no sentinela que os
// caminhos de RESGATE (Cadastrar) e de VALIDAÇÃO do link (ValidarTokenConvite)
// devolvem. Fonte ÚNICA dessa tradução: os dois caminhos precisam concordar,
// senão a tela explicaria um motivo e o POST recusaria por outro.
func erroDaSituacaoConvite(situacao string) error {
	switch situacao {
	case ConviteRevogado:
		return ErrConviteRevogado
	case ConviteUsado:
		return ErrConviteJaUsado
	case ConviteExpirado:
		return ErrConviteExpirado
	default:
		return nil
	}
}

// emailDeConviteValido aplica a validação de formato do e-mail convidado. Não
// pretende ser um validador de RFC: barra o que é obviamente inválido (vazio,
// sem `@`, com espaço interno, maior que a coluna VARCHAR(255)) para que a
// emissão devolva 400 em vez de gravar um convite impossível de resgatar. A
// entrada já chega normalizada (normalizeEmail).
func emailDeConviteValido(email string) bool {
	if email == "" || utf8.RuneCountInString(email) > 255 {
		return false
	}
	if strings.ContainsAny(email, " \t\r\n") || strings.Count(email, "@") != 1 {
		return false
	}
	at := strings.Index(email, "@")
	return at > 0 && at < len(email)-1
}

// linkDoConvite monta o link que o emissor compartilha:
// `{APP_URL}/e/{slug}/cadastro?token=...`. Reusa LinkDaEmpresa (Story 9.1) —
// sem o prefixo `/e/{slug}` nem o SPA nem a API resolveriam a Empresa.
func linkDoConvite(appURL, empresaSlug, token string) string {
	return LinkDaEmpresa(appURL, empresaSlug, "/cadastro", token)
}

// EmitirConvite grava um convite nominal para `email` DENTRO da Empresa
// `empresaID` e devolve a projeção já com o link para compartilhar.
//
// `emailCfg`/`empresaSlug` só montam o link (nunca consultam nada) e
// `criadoPor` é o id do `gestor`+ autenticado, resolvido pelo middleware e
// passado como argumento — esta camada nunca reconsulta quem chama (AD-8
// forma 3).
//
// Ordem: validar o e-mail PRIMEIRO, checar conta existente DEPOIS, gravar por
// último — um payload inválido nunca chega ao banco.
func EmitirConvite(db *sql.DB, emailCfg EmailConfig, empresaID, empresaSlug string, criadoPor, email string) (ConviteResumo, error) {
	normalizado := normalizeEmail(email)
	if !emailDeConviteValido(normalizado) {
		return ConviteResumo{}, ErrConviteValidacao
	}

	// Guard de conta já existente NA MESMA Empresa: a unicidade de e-mail é
	// `(empresa_id, lower(email))` desde a migration 000032, então a mesma
	// pessoa pode ter conta em outra Empresa e ainda assim ser convidada aqui.
	var existe bool
	err := db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM usuarios WHERE empresa_id = $1 AND lower(email) = $2)`,
		empresaID, normalizado,
	).Scan(&existe)
	if err != nil {
		return ConviteResumo{}, fmt.Errorf("falha ao verificar conta existente: %w", err)
	}
	if existe {
		return ConviteResumo{}, ErrConviteEmailJaCadastrado
	}

	token, err := gerarTokenAcao()
	if err != nil {
		return ConviteResumo{}, err
	}

	var c ConviteResumo
	const insert = `
		INSERT INTO convites_empresa (empresa_id, email, token, expira_em, criado_por)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, email, expira_em, criado_em`
	err = db.QueryRow(insert, empresaID, normalizado, token, time.Now().UTC().Add(conviteExpiracao), criadoPor).
		Scan(&c.ID, &c.Email, &c.ExpiraEm, &c.CriadoEm)
	if err != nil {
		return ConviteResumo{}, fmt.Errorf("falha ao gravar convite: %w", err)
	}

	c.Situacao = ConvitePendente
	c.Link = linkDoConvite(emailCfg.AppURL, empresaSlug, token)
	return c, nil
}

// ListarConvites devolve TODOS os convites da Empresa `empresaID` — pendentes,
// usados, revogados e expirados — com a `Situacao` derivada na leitura. O
// `Link` só acompanha os `pendente`: um convite em qualquer outro estado não
// tem link para compartilhar, e devolvê-lo só espalharia um segredo inútil.
//
// `emailCfg`/`empresaSlug` entram pelo mesmo motivo de EmitirConvite (montar
// o link) — a spec escreve a assinatura como `ListarConvites(db, empresaID)`,
// mas a forma golden da linha da listagem inclui `link`, e montá-lo no handler
// colocaria em `handlers/` uma decisão que já vive aqui (qual caminho, qual
// prefixo, e em quais situações o link existe).
//
// Ordenado do mais recente para o mais antigo, com `id` como desempate para
// uma ordem determinística. Lista vazia não é erro.
func ListarConvites(db *sql.DB, emailCfg EmailConfig, empresaID, empresaSlug string) ([]ConviteResumo, error) {
	const query = `
		SELECT c.id, c.email, c.token, c.expira_em, c.usado_em, c.revogado_em, c.criado_em,
		       COALESCE(u.nome, '')
		FROM convites_empresa c
		LEFT JOIN usuarios u ON u.id = c.criado_por
		WHERE c.empresa_id = $1
		ORDER BY c.criado_em DESC, c.id DESC`
	rows, err := db.Query(query, empresaID)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar convites: %w", err)
	}
	defer rows.Close()

	agora := time.Now()
	convites := make([]ConviteResumo, 0)
	for rows.Next() {
		var c ConviteResumo
		var token string
		var usadoEm, revogadoEm sql.NullTime
		if err := rows.Scan(&c.ID, &c.Email, &token, &c.ExpiraEm, &usadoEm, &revogadoEm, &c.CriadoEm, &c.CriadoPorNome); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de convite: %w", err)
		}
		c.Situacao = situacaoConvite(c.ExpiraEm, usadoEm, revogadoEm, agora)
		if c.Situacao == ConvitePendente {
			c.Link = linkDoConvite(emailCfg.AppURL, empresaSlug, token)
		}
		convites = append(convites, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar convites: %w", err)
	}
	return convites, nil
}

// RevogarConvite cancela um convite ainda não resgatado da Empresa
// `empresaID`, preenchendo `revogado_em`. A linha NUNCA é apagada: quem abrir
// o link depois precisa ver "cancelado pela empresa", não "inexistente".
//
//   - id inexistente, malformado (não-UUID) ou DE OUTRA EMPRESA ->
//     ErrConviteNaoEncontrado (404). O filtro por `empresa_id` está no próprio
//     SELECT: um `gestor` da Empresa B nunca revoga um convite da Empresa A,
//     nem descobre que ele existe.
//   - convite já resgatado -> ErrConviteJaUsado (409): a conta já nasceu,
//     revogar não a desfaz (para isso existe a desativação, Story 1.8).
//   - convite já revogado -> sucesso silencioso (idempotente): o estado final
//     pedido pelo chamador já é o estado da linha.
//   - convite expirado mas nunca usado -> revoga normalmente (o emissor pode
//     querer marcar explicitamente que aquele link está cancelado).
//
// A escrita é UMA única declaração condicional (nunca um SELECT seguido de um
// UPDATE): assim ela já é o guard contra um resgate concorrente — se o convite
// for consumido no meio do caminho, o UPDATE simplesmente não casa nenhuma
// linha. Só nesse caso (RowsAffected()==0) uma segunda consulta classifica o
// motivo, para a resposta distinguir 404 de 409.
func RevogarConvite(db *sql.DB, empresaID, conviteID string) error {
	res, err := db.Exec(
		`UPDATE convites_empresa SET revogado_em = now()
		 WHERE id = $1 AND empresa_id = $2 AND usado_em IS NULL AND revogado_em IS NULL`,
		conviteID, empresaID,
	)
	if err != nil {
		// `id` malformado (não-UUID) chega como `pq` 22P02 e colapsa em "não
		// encontrado", exatamente como um id inexistente (molde de
		// carregarAlvoParaGestao).
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return ErrConviteNaoEncontrado
		}
		return fmt.Errorf("falha ao revogar convite: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}

	var usadoEm, revogadoEm sql.NullTime
	err = db.QueryRow(
		`SELECT usado_em, revogado_em FROM convites_empresa WHERE id = $1 AND empresa_id = $2`,
		conviteID, empresaID,
	).Scan(&usadoEm, &revogadoEm)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConviteNaoEncontrado
	}
	if err != nil {
		return fmt.Errorf("falha ao consultar convite: %w", err)
	}
	if usadoEm.Valid {
		return ErrConviteJaUsado
	}
	// Já revogado: o estado final pedido pelo chamador já é o da linha.
	return nil
}
