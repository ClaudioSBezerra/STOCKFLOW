// Package services, arquivo plataforma.go: a identidade "Dono da Plataforma"
// — Story 9.2 (Epic 9, Multi-Empresa e Plataforma), spec-9-2, AD-21.
//
// O Dono é uma identidade DISJUNTA de `usuarios`: tabela
// (`donos_plataforma`), sessões (`sessoes_plataforma`), cookie, rotas
// (`/api/plataforma/...`) e middleware próprios. O papel não entra na escala
// de RankPapel e nunca pertence a uma Empresa.
//
// A sessão reaproveita o formato AD-6 (access JWT HS256 de 30min com o mesmo
// JWT_SECRET + refresh opaco rotativo de 2h), mas o access token carrega
// `aud = "plataforma"`: middleware.RequireDonoPlataforma exige esse `aud`, e
// middleware.RequireAuth recusa qualquer token que o traga. Nenhuma rota
// aceita a sessão de um tipo no lugar da do outro.
//
// A MFA do Dono não é opcional (CHECK no banco): o login pede e-mail, senha
// e código TOTP numa única chamada, e qualquer falha colapsa no MESMO
// ErrCredenciaisInvalidas. Bloqueio de força bruta e rate limit ficam com o
// item diferido de FR-36/AD-21 — até lá, a MFA obrigatória e o bcrypt são as
// defesas.
package services

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

// AudienciaPlataforma é o claim `aud` do access token do Dono. É o que
// separa, na validação do JWT, uma sessão da Plataforma de uma sessão de
// `usuarios` (que não carrega `aud` nenhum).
const AudienciaPlataforma = "plataforma"

// PlataformaClaims é o claim do access token do Dono: só `sub` (id em
// `donos_plataforma`), `aud`, `exp` e `iat`. Estado de conta (nome, ativo) é
// sempre relido do Postgres a cada requisição (BuscarDonoSessao).
type PlataformaClaims struct {
	jwt.RegisteredClaims
}

// DonoSessao é a projeção de `donos_plataforma` resolvida a cada requisição
// autenticada da Plataforma (middleware.RequireDonoPlataforma).
type DonoSessao struct {
	ID    string
	Nome  string
	Email string
	Ativo bool
}

var (
	// ErrDonoSessaoNaoEncontrado indica que o `sub` do token não corresponde a
	// nenhum Dono — o middleware trata como sessão revogada.
	ErrDonoSessaoNaoEncontrado = errors.New("dono da plataforma da sessão não encontrado")
	// ErrDonoJaExiste indica que já existe um Dono: o bootstrap por CLI só
	// cria a PRIMEIRA linha e nunca altera uma existente.
	ErrDonoJaExiste = errors.New("já existe um dono da plataforma")
	// ErrDonoValidacao indica nome/e-mail ausentes, longos demais ou e-mail
	// sem @ no bootstrap do Dono.
	ErrDonoValidacao = errors.New("nome e e-mail são obrigatórios (e-mail com @, até 255 caracteres)")
)

// LoginDonoPlataforma autentica o Dono por e-mail + senha + código TOTP,
// numa única etapa (a MFA é estrutural — não existe login do Dono sem
// código). Campo em branco -> ErrLoginValidacao, sem consulta ao banco.
// E-mail inexistente, senha errada, conta inativa, código errado ou código já
// usado -> o MESMO ErrCredenciaisInvalidas, nunca revelando qual fator falhou.
//
// O caminho "e-mail inexistente" compara contra dummyBcryptHash (mesma defesa
// de timing de Login). No sucesso, o passo TOTP que casou é gravado
// atomicamente em `mfa_ultimo_passo_usado`, guardado por "estritamente maior
// que o último": um código já consumido (ou um mais antigo) não entra de novo.
func LoginDonoPlataforma(db *sql.DB, email, senha, codigo string) (string, error) {
	emailNormalizado := normalizeEmail(email)
	if emailNormalizado == "" || strings.TrimSpace(senha) == "" || strings.TrimSpace(codigo) == "" {
		return "", ErrLoginValidacao
	}

	var id, senhaHash, segredo string
	var ativo bool
	const selectDono = `
		SELECT id, senha_hash, mfa_secret, ativo
		FROM donos_plataforma
		WHERE lower(email) = $1`
	if err := db.QueryRow(selectDono, emailNormalizado).Scan(&id, &senhaHash, &segredo, &ativo); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(senha))
			return "", ErrCredenciaisInvalidas
		}
		return "", fmt.Errorf("falha ao consultar dono da plataforma para login: %w", err)
	}

	senhaCorreta := bcrypt.CompareHashAndPassword([]byte(senhaHash), []byte(senha)) == nil
	if !ativo || !senhaCorreta {
		return "", ErrCredenciaisInvalidas
	}

	passo, ok := passoDoCodigoTOTP(segredo, codigo)
	if !ok {
		return "", ErrCredenciaisInvalidas
	}

	const marcarPassoUsado = `
		UPDATE donos_plataforma
		SET mfa_ultimo_passo_usado = $2
		WHERE id = $1 AND ativo
		  AND (mfa_ultimo_passo_usado IS NULL OR mfa_ultimo_passo_usado < $2)`
	res, err := db.Exec(marcarPassoUsado, id, passo)
	if err != nil {
		return "", fmt.Errorf("falha ao registrar passo TOTP usado (login do dono): %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", ErrCredenciaisInvalidas
	}
	return id, nil
}

// gerarAccessTokenPlataforma emite o access JWT do Dono: mesmo formato e
// prazo de gerarAccessToken (AD-6), mas com `aud = "plataforma"`.
func gerarAccessTokenPlataforma(jwtSecret []byte, donoID string) (string, error) {
	agora := time.Now().UTC()
	claims := PlataformaClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   donoID,
			Audience:  jwt.ClaimStrings{AudienciaPlataforma},
			ExpiresAt: jwt.NewNumericDate(agora.Add(accessTokenExpiracao)),
			IssuedAt:  jwt.NewNumericDate(agora),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
	if err != nil {
		return "", fmt.Errorf("falha ao assinar access token da plataforma: %w", err)
	}
	return signed, nil
}

// EmitirSessaoPlataforma gera o par de tokens de sessão do Dono já
// autenticado: access JWT de 30min (`aud=plataforma`) e refresh opaco de 2h
// persistido em `sessoes_plataforma`. expiraRefresh é devolvido para o
// chamador HTTP montar o Set-Cookie com o mesmo prazo gravado.
func EmitirSessaoPlataforma(db *sql.DB, jwtSecret []byte, donoID string) (accessToken, refreshToken string, expiraRefresh time.Time, err error) {
	accessToken, err = gerarAccessTokenPlataforma(jwtSecret, donoID)
	if err != nil {
		return "", "", time.Time{}, err
	}
	refreshToken, err = gerarTokenAcao()
	if err != nil {
		return "", "", time.Time{}, err
	}
	expiraRefresh = time.Now().UTC().Add(RefreshTokenExpiracao)
	const insertSessao = `
		INSERT INTO sessoes_plataforma (dono_id, refresh_token, expira_em)
		VALUES ($1, $2, $3)`
	if _, err := db.Exec(insertSessao, donoID, refreshToken, expiraRefresh); err != nil {
		return "", "", time.Time{}, fmt.Errorf("falha ao gravar sessão da plataforma: %w", err)
	}
	return accessToken, refreshToken, expiraRefresh, nil
}

// RenovarSessaoPlataforma rotaciona um refresh token válido do Dono — molde
// de RenovarSessao: a linha atual é revogada e a nova é inserida na MESMA
// transação. Token ausente, expirado, já revogado, inexistente ou de um Dono
// inativo -> ErrSessaoInvalida.
func RenovarSessaoPlataforma(db *sql.DB, jwtSecret []byte, refreshTokenAtual string) (novoAccess, novoRefresh string, expiraRefresh time.Time, err error) {
	if strings.TrimSpace(refreshTokenAtual) == "" {
		return "", "", time.Time{}, ErrSessaoInvalida
	}

	tx, err := db.Begin()
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	const marcarRevogada = `
		UPDATE sessoes_plataforma
		SET revogado_em = now()
		WHERE refresh_token = $1 AND revogado_em IS NULL AND expira_em > now()
		  AND EXISTS (SELECT 1 FROM donos_plataforma d WHERE d.id = sessoes_plataforma.dono_id AND d.ativo)
		RETURNING dono_id`
	var donoID string
	if err := tx.QueryRow(marcarRevogada, refreshTokenAtual).Scan(&donoID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", time.Time{}, ErrSessaoInvalida
		}
		return "", "", time.Time{}, fmt.Errorf("falha ao revogar sessão atual da plataforma: %w", err)
	}

	novoAccess, err = gerarAccessTokenPlataforma(jwtSecret, donoID)
	if err != nil {
		return "", "", time.Time{}, err
	}
	novoRefresh, err = gerarTokenAcao()
	if err != nil {
		return "", "", time.Time{}, err
	}

	expiraRefresh = time.Now().UTC().Add(RefreshTokenExpiracao)
	const insertSessao = `
		INSERT INTO sessoes_plataforma (dono_id, refresh_token, expira_em)
		VALUES ($1, $2, $3)`
	if _, err := tx.Exec(insertSessao, donoID, novoRefresh, expiraRefresh); err != nil {
		return "", "", time.Time{}, fmt.Errorf("falha ao gravar sessão rotacionada da plataforma: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", "", time.Time{}, fmt.Errorf("falha ao commitar rotação de sessão da plataforma: %w", err)
	}
	return novoAccess, novoRefresh, expiraRefresh, nil
}

// RevogarSessaoPlataforma revoga a sessão do refresh token informado (logout
// do Dono). Idempotente: token vazio, inexistente ou já revogado -> nil.
func RevogarSessaoPlataforma(db *sql.DB, refreshToken string) error {
	if strings.TrimSpace(refreshToken) == "" {
		return nil
	}
	const revogar = `
		UPDATE sessoes_plataforma
		SET revogado_em = now()
		WHERE refresh_token = $1 AND revogado_em IS NULL`
	if _, err := db.Exec(revogar, refreshToken); err != nil {
		return fmt.Errorf("falha ao revogar sessão da plataforma: %w", err)
	}
	return nil
}

// BuscarDonoSessao resolve o Dono por id — sempre do Postgres, nunca do
// claim (AD-6). Id inexistente ou malformado -> ErrDonoSessaoNaoEncontrado.
func BuscarDonoSessao(db *sql.DB, donoID string) (DonoSessao, error) {
	var d DonoSessao
	const selectDono = `SELECT id, nome, email, ativo FROM donos_plataforma WHERE id = $1`
	if err := db.QueryRow(selectDono, donoID).Scan(&d.ID, &d.Nome, &d.Email, &d.Ativo); err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return DonoSessao{}, ErrDonoSessaoNaoEncontrado
		}
		return DonoSessao{}, fmt.Errorf("falha ao consultar dono da plataforma da sessão: %w", err)
	}
	return d, nil
}

// CriarPrimeiroDonoPlataforma é o bootstrap do Dono, chamado SÓ pelo CLI
// `cmd/seed-dono-plataforma` (AD-12 — nenhuma rota HTTP cria Dono). Valida
// nome/e-mail (ErrDonoValidacao) e a força da senha (ErrSenhaFraca) antes de
// qualquer escrita, gera o segredo TOTP e grava a linha com a MFA já ativa.
//
// "Nenhum Dono existe" e o INSERT rodam numa transação sob
// `LOCK TABLE ... IN EXCLUSIVE MODE`: duas execuções concorrentes do CLI são
// serializadas, e a segunda vê a linha da primeira (ErrDonoJaExiste). Uma
// linha existente nunca é alterada. O segredo é devolvido UMA vez, para o
// operador cadastrar no autenticador — não é exibido de novo por nada.
func CriarPrimeiroDonoPlataforma(db *sql.DB, nome, email, senha string) (id, segredo string, err error) {
	nomeTrimado := strings.TrimSpace(nome)
	emailNormalizado := normalizeEmail(email)
	if nomeTrimado == "" || utf8.RuneCountInString(nomeTrimado) > 255 || !emailPlausivel(emailNormalizado) {
		return "", "", ErrDonoValidacao
	}
	if err := ValidarForcaSenha(senha); err != nil {
		return "", "", err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("falha ao gerar hash da senha: %w", err)
	}
	segredo, err = GerarSegredoTOTP()
	if err != nil {
		return "", "", err
	}

	tx, err := db.Begin()
	if err != nil {
		return "", "", fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	if _, err := tx.Exec(`LOCK TABLE donos_plataforma IN EXCLUSIVE MODE`); err != nil {
		return "", "", fmt.Errorf("falha ao travar a tabela de donos da plataforma: %w", err)
	}

	const insertDono = `
		INSERT INTO donos_plataforma (nome, email, senha_hash, mfa_habilitado, mfa_secret)
		SELECT $1, $2, $3, true, $4
		WHERE NOT EXISTS (SELECT 1 FROM donos_plataforma)
		RETURNING id`
	if err := tx.QueryRow(insertDono, nomeTrimado, emailNormalizado, string(hash), segredo).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrDonoJaExiste
		}
		return "", "", fmt.Errorf("falha ao inserir dono da plataforma: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", "", fmt.Errorf("falha ao commitar criação do dono da plataforma: %w", err)
	}
	slog.Info("primeiro dono da plataforma criado", "dono_id", id)
	return id, segredo, nil
}

// emailPlausivel é a checagem mínima de e-mail usada nas contas criadas pela
// Plataforma (o Dono e o `adm` provisionado): não vazio, até 255 caracteres e
// com um `@` que não esteja numa das pontas.
func emailPlausivel(email string) bool {
	if email == "" || utf8.RuneCountInString(email) > 255 {
		return false
	}
	i := strings.Index(email, "@")
	return i > 0 && i < len(email)-1
}
