package services

// Login na raiz do domínio pela conta — Story 15.2 (AD-36). A raiz NÃO tem
// Empresa na URL: o e-mail (único entre as Empresas reais desde a Story 15.1)
// é que a identifica. Este arquivo só DESCOBRE as contas do e-mail e delega a
// decisão de credencial a Login, uma vez por conta — bloqueio/contagem de
// falhas, conta desativada, e-mail não confirmado e conta só-SSO continuam
// num lugar só. Nada aqui abre exceção a regra de conta.

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

// escolhaEmpresaTokenExpiracao é o prazo do `escolhaToken` (tokens_acao,
// tipo `escolha_empresa`): 5 min, mesmo prazo do token de MFA.
const escolhaEmpresaTokenExpiracao = 5 * time.Minute

// ErrEscolhaInvalida: `escolhaToken` inexistente, vencido, já usado, ou slug
// que não é de uma das contas conferidas (ou cuja Empresa/conta deixou de
// poder entrar). Handler -> 401 ESCOLHA_INVALIDA.
var ErrEscolhaInvalida = errors.New("a escolha expirou; faça login novamente")

// ContaEntrada é uma conta do e-mail numa Empresa ativa. `Treinamento` vem só
// de `empresas.empresa_origem_id IS NOT NULL` (mesma fonte de
// usuarioRespostaDe) — nunca de um dado da conta.
type ContaEntrada struct {
	UsuarioID    string
	EmpresaID    string
	Slug         string
	NomeFantasia string
	Treinamento  bool
}

// selectContasEntrada: a Empresa real vem antes do Treinamento (false < true),
// depois por slug — a ordem da pergunta "Ambiente real ou Treinamento?".
// `senha_hash` só alimenta a fase 1 de EntrarPelaConta; nunca sai deste
// arquivo (fica em contaComHash, não em ContaEntrada).
const selectContasEntrada = `
	SELECT u.id, e.id, e.slug, e.nome_fantasia, e.empresa_origem_id IS NOT NULL, u.senha_hash
	FROM usuarios u
	JOIN empresas e ON e.id = u.empresa_id
	WHERE lower(u.email) = $1 AND e.status = 'ativa'
	ORDER BY (e.empresa_origem_id IS NOT NULL), e.slug`

// contaComHash é a linha de selectContasEntrada com o hash da senha — uso
// interno da fase 1 de EntrarPelaConta.
type contaComHash struct {
	ContaEntrada
	senhaHash sql.NullString
}

func scanContasEntrada(rows *sql.Rows) ([]contaComHash, error) {
	defer rows.Close()
	var contas []contaComHash
	for rows.Next() {
		var c contaComHash
		if err := rows.Scan(&c.UsuarioID, &c.EmpresaID, &c.Slug, &c.NomeFantasia, &c.Treinamento, &c.senhaHash); err != nil {
			return nil, err
		}
		contas = append(contas, c)
	}
	return contas, rows.Err()
}

// EntrarPelaConta acha as contas do e-mail em Empresas ATIVAS e decide em
// duas fases, para que a tentativa pela raiz tenha em cada conta exatamente
// o efeito que teria pelo endereço da Empresa dela:
//
//   - Fase 1 (sem efeito colateral): compara a senha com o `senha_hash` de
//     cada conta (dummyBcryptHash se NULL). Nada é gravado.
//   - Fase 2: candidatas = as contas cuja senha conferiu na fase 1; se
//     nenhuma conferiu, candidatas = todas. Login roda SÓ nas candidatas —
//     ele continua sendo a única regra de conta (bloqueio/contagem de falhas,
//     desativada, e-mail não confirmado, só-SSO). Assim, entrar na Empresa
//     real não conta falha no Treinamento de senha diferente (nem vice-versa);
//     senha errada em todas conta falha em todas.
//
// `avaliadas` são as candidatas — as contas em que a tentativa contou (o
// handler grava uma linha de `logs_acesso` por avaliada, na Empresa dela);
// `aceitas` são as que Login aceitou, na mesma ordem.
//
// Agregação: ≥1 aceita -> err nil; nenhuma aceita e alguma bloqueada ->
// ErrContaBloqueada; senão ErrCredenciaisInvalidas. E-mail/senha em branco ->
// ErrLoginValidacao (sem consulta). Sem conta nenhuma, o bcrypt roda contra
// dummyBcryptHash para o tempo não revelar se o e-mail existe.
func EntrarPelaConta(db *sql.DB, email, senha string) (avaliadas, aceitas []ContaEntrada, err error) {
	normalizedEmail := normalizeEmail(email)
	if normalizedEmail == "" || strings.TrimSpace(senha) == "" {
		return nil, nil, ErrLoginValidacao
	}

	rows, err := db.Query(selectContasEntrada, normalizedEmail)
	if err != nil {
		return nil, nil, fmt.Errorf("falha ao buscar contas do e-mail: %w", err)
	}
	encontradas, err := scanContasEntrada(rows)
	if err != nil {
		return nil, nil, fmt.Errorf("falha ao ler contas do e-mail: %w", err)
	}

	if len(encontradas) == 0 {
		// Duas comparações: o menor caminho com conta (uma conta, senha errada)
		// faz a da fase 1 e a de dentro de Login — sem conta tem de custar o
		// mesmo, senão o tempo revela que o e-mail existe.
		_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(senha))
		_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(senha))
		return nil, nil, ErrCredenciaisInvalidas
	}

	// Fase 1: só compara, não grava nada.
	var conferiram []ContaEntrada
	todas := make([]ContaEntrada, len(encontradas))
	for i, c := range encontradas {
		todas[i] = c.ContaEntrada
		hash := dummyBcryptHash
		if c.senhaHash.Valid {
			hash = []byte(c.senhaHash.String)
		}
		if bcrypt.CompareHashAndPassword(hash, []byte(senha)) == nil && c.senhaHash.Valid {
			conferiram = append(conferiram, c.ContaEntrada)
		}
	}

	// Fase 2: Login só nas candidatas.
	avaliadas = conferiram
	if len(avaliadas) == 0 {
		avaliadas = todas
	}

	algumaBloqueada := false
	for _, c := range avaliadas {
		_, errLogin := Login(db, c.EmpresaID, email, senha)
		switch {
		case errLogin == nil:
			aceitas = append(aceitas, c)
		case errors.Is(errLogin, ErrContaBloqueada):
			algumaBloqueada = true
		case errors.Is(errLogin, ErrCredenciaisInvalidas), errors.Is(errLogin, ErrLoginValidacao):
			// recusada nesta conta
		default:
			return avaliadas, aceitas, errLogin
		}
	}

	switch {
	case len(aceitas) > 0:
		return avaliadas, aceitas, nil
	case algumaBloqueada:
		return avaliadas, nil, ErrContaBloqueada
	default:
		return avaliadas, nil, ErrCredenciaisInvalidas
	}
}

// IniciarEscolhaEmpresa grava o `escolhaToken` de uso único (5 min) preso aos
// ids das contas conferidas. `usuario_id` recebe a primeira (NOT NULL + FK).
func IniciarEscolhaEmpresa(db *sql.DB, aceitas []ContaEntrada) (string, error) {
	if len(aceitas) == 0 {
		return "", errors.New("IniciarEscolhaEmpresa sem contas aceitas")
	}
	ids := make([]string, len(aceitas))
	for i, c := range aceitas {
		ids[i] = c.UsuarioID
	}

	token, err := gerarTokenAcao()
	if err != nil {
		return "", err
	}
	expiraEm := time.Now().UTC().Add(escolhaEmpresaTokenExpiracao)
	const insertToken = `
		INSERT INTO tokens_acao (usuario_id, token, tipo, expira_em, contas_escolha)
		VALUES ($1, $2, 'escolha_empresa', $3, $4::uuid[])`
	if _, err := db.Exec(insertToken, ids[0], token, expiraEm, pq.Array(ids)); err != nil {
		return "", fmt.Errorf("falha ao gravar token de escolha de Empresa: %w", err)
	}
	return token, nil
}

// ConcluirEscolhaEmpresa consome o `escolhaToken` atomicamente (o primeiro
// uso o queima, mesmo com um slug inválido) e devolve a conta cuja Empresa
// tem `slug` — só entre as contas conferidas, em Empresa ainda ativa, conta
// ainda ativa, não bloqueada, com e-mail confirmado e com senha (não só-SSO):
// as mesmas condições que Login exigiu quando o token foi emitido, reconferidas
// porque podem ter mudado nesses 5 min. Qualquer outro caso ->
// ErrEscolhaInvalida.
func ConcluirEscolhaEmpresa(db *sql.DB, token, slug string) (ContaEntrada, error) {
	if token == "" || slug == "" {
		return ContaEntrada{}, ErrEscolhaInvalida
	}

	const consumir = `
		UPDATE tokens_acao SET usado_em = now()
		WHERE token = $1 AND tipo = 'escolha_empresa' AND usado_em IS NULL AND expira_em > now()
		RETURNING contas_escolha`
	var ids []string
	if err := db.QueryRow(consumir, token).Scan(pq.Array(&ids)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ContaEntrada{}, ErrEscolhaInvalida
		}
		return ContaEntrada{}, fmt.Errorf("falha ao consumir token de escolha de Empresa: %w", err)
	}
	if len(ids) == 0 {
		return ContaEntrada{}, ErrEscolhaInvalida
	}

	const selectConta = `
		SELECT u.id, e.id, e.slug, e.nome_fantasia, e.empresa_origem_id IS NOT NULL
		FROM usuarios u
		JOIN empresas e ON e.id = u.empresa_id
		WHERE u.id = ANY($1::uuid[]) AND e.slug = $2 AND e.status = 'ativa'
		  AND u.ativo AND u.email_verificado AND u.senha_hash IS NOT NULL
		  AND (u.bloqueado_ate IS NULL OR u.bloqueado_ate <= now())`
	var c ContaEntrada
	err := db.QueryRow(selectConta, pq.Array(ids), slug).Scan(&c.UsuarioID, &c.EmpresaID, &c.Slug, &c.NomeFantasia, &c.Treinamento)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ContaEntrada{}, ErrEscolhaInvalida
		}
		return ContaEntrada{}, fmt.Errorf("falha ao resolver a conta escolhida: %w", err)
	}
	return c, nil
}
