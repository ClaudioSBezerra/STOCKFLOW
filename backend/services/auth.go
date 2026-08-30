// Package services concentra as regras de negócio de autenticação: Story 1.3
// (autocadastro público sempre como `usuario`, verificação de e-mail via
// token de uso único, outbox/worker de e-mail transacional — AD-4/AD-18),
// Story 1.4 (login por e-mail/senha, emissão e rotação de sessão — AD-6) e
// Story 1.6 (recuperação de senha por e-mail: solicitação com resposta
// genérica + outbox, redefinição consumindo o token de uso único e revogando
// todas as sessões da conta, política mínima de força de senha).
package services

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

// pqUniqueViolation é o SQLSTATE do Postgres para violação de unicidade —
// mesmo padrão de backend/cmd/seed-admin/main.go.
const pqUniqueViolation = "23505"

// tokenVerificacaoExpiracao é o prazo de validade do token de verificação de
// e-mail: 24h é decisão desta story — nenhuma fonte do PRD/épico fixa prazo
// para este tipo especificamente (os 30min de `redefinicao_senha` são só da
// Story 1.6).
const tokenVerificacaoExpiracao = 24 * time.Hour

// tokenRedefinicaoExpiracao é o prazo de validade do token de redefinição de
// senha (Story 1.6): 30min, fixado explicitamente pelo intent desta story.
const tokenRedefinicaoExpiracao = 30 * time.Minute

// Prazos de sessão (Story 1.4, AD-6): access JWT curto de 30min; refresh
// token opaco rotativo com TTL de 2h, mesmo formato reaproveitado depois pelo
// login federado (Story 1.9). RefreshTokenExpiracao é exportado porque é
// usada nos testes desta suíte (e de handlers/) para validar o Max-Age do
// cookie contra o TTL configurado — tanto EmitirSessao quanto RenovarSessao
// devolvem o instante de expiração efetivamente persistido, então nenhum
// chamador HTTP precisa recalculá-lo a partir desta constante.
const (
	accessTokenExpiracao  = 30 * time.Minute
	RefreshTokenExpiracao = 2 * time.Hour
)

var (
	// ErrCadastroValidacao indica campo obrigatório ausente/vazio no payload
	// de cadastro (nome, e-mail ou senha).
	ErrCadastroValidacao = errors.New("nome, e-mail e senha são obrigatórios")
	// ErrEmailDuplicado indica que o e-mail normalizado já está cadastrado
	// (comparação por lower(email), índice idx_usuarios_email_lower).
	ErrEmailDuplicado = errors.New("este e-mail já está cadastrado")
	// ErrTokenNaoEncontrado indica que nenhum token de verificação com aquele
	// valor e tipo existe.
	ErrTokenNaoEncontrado = errors.New("token de verificação não encontrado")
	// ErrTokenExpirado indica que o token existe mas já expirou ou já foi
	// usado — tratados com o mesmo código de erro (TOKEN_EXPIRED) por decisão
	// da I/O Matrix desta story.
	ErrTokenExpirado = errors.New("token de verificação expirado ou já utilizado")

	// ErrSenhaFraca indica que a senha não cumpre a política mínima de força
	// aplicada por ValidarForcaSenha (Story 1.6): 8+ caracteres, ao menos uma
	// letra e um dígito, no máximo 72 bytes (limite do bcrypt). Semente que a
	// Story 1.10 (bloqueio de conta e política de senha) vai reusar/estender.
	ErrSenhaFraca = errors.New("a senha não cumpre a política mínima de força")

	// ErrLoginValidacao indica e-mail ou senha em branco no payload de login —
	// mapeado para 400 VALIDATION_ERROR, sem nenhuma consulta ao banco (I/O
	// Matrix, Story 1.4).
	ErrLoginValidacao = errors.New("e-mail e senha são obrigatórios")
	// ErrCredenciaisInvalidas cobre TODOS os cenários de login malsucedido
	// (senha errada, e-mail inexistente, e-mail não verificado, conta
	// desativada, conta só-SSO) com o mesmo erro — deliberado: o chamador
	// nunca pode distinguir qual condição falhou nem revelar se o e-mail
	// existe (regra explícita do contexto do épico).
	ErrCredenciaisInvalidas = errors.New("e-mail ou senha inválidos")
	// ErrSessaoInvalida cobre refresh token ausente, expirado, já revogado ou
	// inexistente — mapeado para 401 TOKEN_EXPIRED pelo handler.
	ErrSessaoInvalida = errors.New("sessão inválida ou expirada")
	// ErrUsuarioSessaoNaoEncontrado indica que BuscarUsuarioSessao não
	// encontrou nenhuma linha para o usuario_id do claim `sub` — o middleware
	// trata isso como sessão revogada (SESSION_REVOKED), nunca como erro
	// interno: uma conta pode ter sido removida entre a emissão do token e o
	// uso.
	ErrUsuarioSessaoNaoEncontrado = errors.New("usuário da sessão não encontrado")
)

// normalizeEmail aplica a mesma normalização usada em cmd/seed-admin:
// minúsculas e sem espaços nas bordas. A unicidade real é garantida pelo
// índice único funcional sobre lower(email) (AD-14).
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// gerarTokenAcao gera um token opaco url-safe de 32 bytes aleatórios
// (crypto/rand) codificados em base64.RawURLEncoding — string de 43
// caracteres colada direto na URL de verificação, sem necessidade de
// escaping adicional.
func gerarTokenAcao() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("falha ao gerar token aleatório: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Cadastrar cria uma conta de autocadastro público. O papel do usuário nunca
// é um parâmetro desta função — é sempre 'usuario' (FR-3), independente de
// qualquer valor que o chamador HTTP tenha recebido no payload.
//
// Todo o trabalho (INSERT em usuarios, tokens_acao e emails_pendentes)
// acontece em uma única transação (AD-4/AD-18): se qualquer passo falhar,
// nenhuma linha órfã fica gravada em nenhuma das três tabelas.
func Cadastrar(db *sql.DB, emailCfg EmailConfig, nome, email, senha string) (usuarioID string, err error) {
	nomeTrimado := strings.TrimSpace(nome)
	normalizedEmail := normalizeEmail(email)
	if nomeTrimado == "" || normalizedEmail == "" || strings.TrimSpace(senha) == "" {
		return "", ErrCadastroValidacao
	}
	// usuarios.nome e usuarios.email são VARCHAR(255) (migration 000001): sem
	// estes guards, um valor maior que a coluna vira um erro bruto do
	// Postgres (500) em vez do 400 VALIDATION_ERROR esperado para um input de
	// cliente inválido (mesma classe de bug já corrigida para email/senha).
	// VARCHAR(255) do Postgres conta caracteres, não bytes — usar
	// utf8.RuneCountInString em vez de len() evita rejeitar como inválido um
	// nome/e-mail com acentos (comuns em PT-BR) que caberia na coluna mas
	// ultrapassa 255 bytes em UTF-8.
	if utf8.RuneCountInString(nomeTrimado) > 255 || utf8.RuneCountInString(normalizedEmail) > 255 {
		return "", ErrCadastroValidacao
	}
	// bcrypt.GenerateFromPassword rejeita senhas com mais de 72 bytes — sem
	// este guard, esse erro cairia no branch genérico de Cadastrar e viraria
	// 500 INTERNAL_ERROR em vez de 400 VALIDATION_ERROR para um input de
	// cliente legítimo (ainda que incomum).
	if len(senha) > 72 {
		return "", ErrCadastroValidacao
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("falha ao gerar hash da senha: %w", err)
	}

	token, err := gerarTokenAcao()
	if err != nil {
		return "", err
	}

	tx, err := db.Begin()
	if err != nil {
		return "", fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	const insertUsuario = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo)
		VALUES ($1, $2, $3, 'usuario', false, true)
		RETURNING id`
	if err := tx.QueryRow(insertUsuario, nomeTrimado, normalizedEmail, string(hash)).Scan(&usuarioID); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation {
			return "", ErrEmailDuplicado
		}
		return "", fmt.Errorf("falha ao inserir usuario: %w", err)
	}

	expiraEm := time.Now().UTC().Add(tokenVerificacaoExpiracao)
	const insertToken = `
		INSERT INTO tokens_acao (usuario_id, token, tipo, expira_em)
		VALUES ($1, $2, 'verificacao_email', $3)`
	if _, err := tx.Exec(insertToken, usuarioID, token, expiraEm); err != nil {
		return "", fmt.Errorf("falha ao inserir token de verificação: %w", err)
	}

	link := fmt.Sprintf("%s/verificar-email?token=%s", emailCfg.AppURL, token)
	variaveis := map[string]any{
		"nome": nomeTrimado,
		"link": link,
	}
	if err := EnfileirarEmail(tx, normalizedEmail, usuarioID, "verificacao_conta", variaveis); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("falha ao commitar cadastro: %w", err)
	}

	return usuarioID, nil
}

// VerificarEmail consome um token de verificação de e-mail: se válido,
// marca-o como usado e libera `email_verificado=true` na mesma transação
// (AD-18) — nenhum dos dois efeitos acontece sem o outro.
//
// A busca inicial distingue "token não existe" (ErrTokenNaoEncontrado) de
// "token existe mas expirado/já usado" (ErrTokenExpirado). O UPDATE que
// marca o token como usado repete as mesmas condições (não expirado, não
// usado) para fechar a janela de corrida entre o SELECT e o UPDATE: se
// `RowsAffected() == 0` ali, outra requisição consumiu ou o prazo expirou
// entre as duas consultas, e o resultado também é ErrTokenExpirado.
func VerificarEmail(db *sql.DB, token string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var usuarioID string
	var expiraEm time.Time
	var usadoEm sql.NullTime
	const selectToken = `
		SELECT usuario_id, expira_em, usado_em
		FROM tokens_acao
		WHERE token = $1 AND tipo = 'verificacao_email'`
	err = tx.QueryRow(selectToken, token).Scan(&usuarioID, &expiraEm, &usadoEm)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTokenNaoEncontrado
		}
		return fmt.Errorf("falha ao consultar token: %w", err)
	}

	if usadoEm.Valid || !time.Now().Before(expiraEm) {
		return ErrTokenExpirado
	}

	const marcarUsado = `
		UPDATE tokens_acao
		SET usado_em = now()
		WHERE token = $1 AND tipo = 'verificacao_email' AND usado_em IS NULL AND expira_em > now()`
	res, err := tx.Exec(marcarUsado, token)
	if err != nil {
		return fmt.Errorf("falha ao marcar token como usado: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTokenExpirado
	}

	if _, err := tx.Exec(`UPDATE usuarios SET email_verificado = true WHERE id = $1`, usuarioID); err != nil {
		return fmt.Errorf("falha ao marcar e-mail verificado: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("falha ao commitar verificação: %w", err)
	}
	return nil
}

// dummyBcryptHash é um hash bcrypt de custo padrão (bcrypt.DefaultCost)
// gerado uma única vez no import, para uma senha fixa arbitrária — usado
// apenas para equalizar o tempo de resposta de Login quando o e-mail não é
// encontrado (ver abaixo). Sem uma comparação bcrypt nesse caminho, ele seria
// mensuravelmente mais rápido que o caminho "e-mail existe, senha errada" (que
// sempre executa bcrypt.CompareHashAndPassword, deliberadamente lento),
// permitindo a um atacante enumerar e-mails cadastrados pelo tempo de
// resposta — violação direta da regra desta story de nunca revelar se um
// e-mail existe.
var dummyBcryptHash = mustGerarDummyBcryptHash()

func mustGerarDummyBcryptHash() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("senha-dummy-para-defesa-contra-timing"), bcrypt.DefaultCost)
	if err != nil {
		panic(fmt.Sprintf("falha ao gerar hash dummy para defesa contra timing em Login: %v", err))
	}
	return hash
}

// UsuarioSessao é a projeção de `usuarios` resolvida a cada requisição
// autenticada (BuscarUsuarioSessao) e exposta pelo middleware — nunca o
// claim do JWT, que só carrega `sub` (Story 1.4: papel/ativo/nome/email são
// sempre lidos do Postgres, nunca cacheados/confiados no token).
type UsuarioSessao struct {
	ID    string
	Nome  string
	Email string
	Papel string
	Ativo bool
}

// Login valida e-mail/senha e devolve o id do usuário autenticado. Cobre a
// I/O Matrix da Story 1.4: senha errada, e-mail inexistente, e-mail não
// verificado, conta desativada e conta só-SSO (`senha_hash IS NULL`)
// resultam TODOS em ErrCredenciaisInvalidas — a mesma resposta, para nunca
// revelar qual condição falhou nem se o e-mail existe (regra explícita do
// contexto do épico).
func Login(db *sql.DB, email, senha string) (usuarioID string, err error) {
	normalizedEmail := normalizeEmail(email)
	if normalizedEmail == "" || strings.TrimSpace(senha) == "" {
		return "", ErrLoginValidacao
	}

	var id string
	var senhaHash sql.NullString
	var ativo, emailVerificado bool
	const selectUsuario = `
		SELECT id, senha_hash, ativo, email_verificado
		FROM usuarios
		WHERE lower(email) = $1`
	err = db.QueryRow(selectUsuario, normalizedEmail).Scan(&id, &senhaHash, &ativo, &emailVerificado)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Defesa contra side-channel de tempo: mesmo sem linha nenhuma para
			// comparar, ainda executamos uma comparação bcrypt completa contra
			// dummyBcryptHash, do mesmo custo usado para senhas reais — assim o
			// tempo de resposta deste caminho fica comparável ao dos demais
			// caminhos de falha abaixo, e a diferença de latência não revela se o
			// e-mail existe.
			_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(senha))
			return "", ErrCredenciaisInvalidas
		}
		return "", fmt.Errorf("falha ao consultar usuario para login: %w", err)
	}

	// Mesma defesa contra side-channel de tempo do caminho "e-mail
	// inexistente" acima: conta desativada, e-mail não verificado ou
	// senha_hash nulo (conta só-SSO, Story 1.9) também precisam comparar
	// contra um hash de custo equivalente antes de devolver o erro — senão o
	// tempo de resposta desses três casos fica mensuravelmente mais rápido do
	// que os caminhos "e-mail inexistente"/"senha incorreta" (que sempre
	// executam bcrypt), revelando por temporização que a conta existe e em
	// que estado ela está.
	hashParaComparar := dummyBcryptHash
	if senhaHash.Valid {
		hashParaComparar = []byte(senhaHash.String)
	}
	senhaCorreta := bcrypt.CompareHashAndPassword(hashParaComparar, []byte(senha)) == nil

	if !ativo || !emailVerificado || !senhaHash.Valid || !senhaCorreta {
		return "", ErrCredenciaisInvalidas
	}

	return id, nil
}

// gerarAccessToken emite o JWT de acesso (AD-6): HS256, claim mínimo (`sub`
// apenas) — deliberado, para que o middleware nunca tenha a tentação de
// confiar em papel/estado carimbado no token em vez de reconsultar
// `usuarios`.
func gerarAccessToken(jwtSecret []byte, usuarioID string) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   usuarioID,
		ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(accessTokenExpiracao)),
		IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(jwtSecret)
	if err != nil {
		return "", fmt.Errorf("falha ao assinar access token: %w", err)
	}
	return signed, nil
}

// EmitirSessao gera o par de tokens de sessão (AD-6) para um usuário já
// autenticado (por senha, Login acima, ou futuramente por SSO, Story 1.9):
// um access JWT de 30min e um refresh token opaco de 2h, persistido em
// `sessoes`. expiraRefresh é devolvido para o chamador HTTP montar o
// `Set-Cookie` com o mesmo prazo.
func EmitirSessao(db *sql.DB, jwtSecret []byte, usuarioID string) (accessToken, refreshToken string, expiraRefresh time.Time, err error) {
	accessToken, err = gerarAccessToken(jwtSecret, usuarioID)
	if err != nil {
		return "", "", time.Time{}, err
	}

	refreshToken, err = gerarTokenAcao()
	if err != nil {
		return "", "", time.Time{}, err
	}

	expiraRefresh = time.Now().UTC().Add(RefreshTokenExpiracao)
	const insertSessao = `
		INSERT INTO sessoes (usuario_id, refresh_token, expira_em)
		VALUES ($1, $2, $3)`
	if _, err := db.Exec(insertSessao, usuarioID, refreshToken, expiraRefresh); err != nil {
		return "", "", time.Time{}, fmt.Errorf("falha ao gravar sessão: %w", err)
	}

	return accessToken, refreshToken, expiraRefresh, nil
}

// RenovarSessao rotaciona um refresh token válido: a linha atual em
// `sessoes` é marcada revogada e uma nova é inserida na MESMA transação —
// mesmo padrão de fechamento de janela de corrida já usado em VerificarEmail
// (marcarUsado): se o UPDATE não afetar nenhuma linha, outra requisição já
// rotacionou esse token ou o prazo expirou entre a leitura e o update, e o
// resultado é ErrSessaoInvalida (mapeado para 401 TOKEN_EXPIRED pelo
// handler). expiraRefresh é devolvido (mesmo formato de EmitirSessao) para o
// chamador HTTP montar o Set-Cookie com o prazo EFETIVAMENTE persistido, em
// vez de recalcular `time.Now().Add(RefreshTokenExpiracao)` de novo e
// arriscar divergir do valor gravado pelo round-trip ao banco.
func RenovarSessao(db *sql.DB, jwtSecret []byte, refreshTokenAtual string) (novoAccess, novoRefresh string, expiraRefresh time.Time, err error) {
	if strings.TrimSpace(refreshTokenAtual) == "" {
		return "", "", time.Time{}, ErrSessaoInvalida
	}

	tx, err := db.Begin()
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	const marcarRevogada = `
		UPDATE sessoes
		SET revogado_em = now()
		WHERE refresh_token = $1 AND revogado_em IS NULL AND expira_em > now()
		RETURNING usuario_id`
	var usuarioID string
	err = tx.QueryRow(marcarRevogada, refreshTokenAtual).Scan(&usuarioID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", time.Time{}, ErrSessaoInvalida
		}
		return "", "", time.Time{}, fmt.Errorf("falha ao revogar sessão atual: %w", err)
	}

	novoAccess, err = gerarAccessToken(jwtSecret, usuarioID)
	if err != nil {
		return "", "", time.Time{}, err
	}

	novoRefresh, err = gerarTokenAcao()
	if err != nil {
		return "", "", time.Time{}, err
	}

	expiraRefresh = time.Now().UTC().Add(RefreshTokenExpiracao)
	const insertSessao = `
		INSERT INTO sessoes (usuario_id, refresh_token, expira_em)
		VALUES ($1, $2, $3)`
	if _, err := tx.Exec(insertSessao, usuarioID, novoRefresh, expiraRefresh); err != nil {
		return "", "", time.Time{}, fmt.Errorf("falha ao gravar sessão rotacionada: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", "", time.Time{}, fmt.Errorf("falha ao commitar rotação de sessão: %w", err)
	}

	return novoAccess, novoRefresh, expiraRefresh, nil
}

// ValidarForcaSenha aplica a política mínima de força de senha (Story 1.6,
// semente da Story 1.10): mínimo 8 caracteres — contados como runes
// (utf8.RuneCountInString), então um acento vale 1 —, ao menos uma letra
// (unicode.IsLetter) e ao menos um dígito (unicode.IsDigit), no máximo 72
// bytes (limite rígido de bcrypt.GenerateFromPassword). Falha em qualquer
// regra devolve ErrSenhaFraca. O backend é sempre a autoridade; o espelho em
// frontend/src/lib/senha.ts é só conveniência no cliente.
func ValidarForcaSenha(senha string) error {
	if utf8.RuneCountInString(senha) < 8 {
		return ErrSenhaFraca
	}
	if len(senha) > 72 {
		return ErrSenhaFraca
	}
	var temLetra, temDigito bool
	for _, r := range senha {
		switch {
		case unicode.IsLetter(r):
			temLetra = true
		case unicode.IsDigit(r):
			temDigito = true
		}
	}
	if !temLetra || !temDigito {
		return ErrSenhaFraca
	}
	return nil
}

// SolicitarRedefinicaoSenha trata a regra por trás de POST
// /api/auth/esqueci-senha. A resposta do handler é SEMPRE a mesma mensagem
// genérica — esta função devolve nil tanto no caso "e-mail sem match" quanto
// no caso "token + linha de outbox gravados": o chamador nunca pode
// distinguir se a conta existe. Só um erro real de infraestrutura sobe como
// não-nil (vira 500 no handler).
//
// Quando (e só quando) existe uma linha em `usuarios` com lower(email) igual
// ao informado, grava numa única transação (mesmo padrão de Cadastrar): um
// `tokens_acao` (tipo='redefinicao_senha', expira_em = now()+30min) + um
// `emails_pendentes` via EnfileirarEmail, com
// link = "{APP_URL}/redefinir-senha?token={token}". Conta só-SSO
// (senha_hash nulo) NÃO é exceção — recebe token e e-mail normalmente.
func SolicitarRedefinicaoSenha(db *sql.DB, emailCfg EmailConfig, email string) error {
	normalizedEmail := normalizeEmail(email)
	if normalizedEmail == "" {
		return nil
	}

	var usuarioID, nome string
	const selectUsuario = `
		SELECT id, nome
		FROM usuarios
		WHERE lower(email) = $1`
	if err := db.QueryRow(selectUsuario, normalizedEmail).Scan(&usuarioID, &nome); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("falha ao consultar usuario para redefinição: %w", err)
	}

	token, err := gerarTokenAcao()
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	// Invalida qualquer token de redefinição anterior ainda não usado ANTES de
	// emitir o novo: uma conta nunca deve ter vários links de redefinição
	// válidos ao mesmo tempo — o link recém-emitido passa a ser o único aceito,
	// reforçando a intenção de "trancar" o acesso ao pedir a redefinição.
	const invalidarAnteriores = `
		UPDATE tokens_acao
		SET usado_em = now()
		WHERE usuario_id = $1 AND tipo = 'redefinicao_senha' AND usado_em IS NULL`
	if _, err := tx.Exec(invalidarAnteriores, usuarioID); err != nil {
		return fmt.Errorf("falha ao invalidar tokens de redefinição anteriores: %w", err)
	}

	expiraEm := time.Now().UTC().Add(tokenRedefinicaoExpiracao)
	const insertToken = `
		INSERT INTO tokens_acao (usuario_id, token, tipo, expira_em)
		VALUES ($1, $2, 'redefinicao_senha', $3)`
	if _, err := tx.Exec(insertToken, usuarioID, token, expiraEm); err != nil {
		return fmt.Errorf("falha ao inserir token de redefinição: %w", err)
	}

	link := fmt.Sprintf("%s/redefinir-senha?token=%s", emailCfg.AppURL, token)
	variaveis := map[string]any{
		"nome": nome,
		"link": link,
	}
	if err := EnfileirarEmail(tx, normalizedEmail, usuarioID, "redefinicao_senha", variaveis); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("falha ao commitar solicitação de redefinição: %w", err)
	}
	return nil
}

// ValidarTokenRedefinicao checa a validade de um token de redefinição SEM
// consumi-lo (SELECT puro, nenhuma escrita) — usado por GET
// /api/auth/redefinir-senha para a tela explicar um link morto já na
// abertura, antes de o usuário digitar a nova senha. Molde de VerificarEmail:
// token inexistente -> ErrTokenNaoEncontrado; existente porém expirado ou já
// usado -> ErrTokenExpirado; válido -> nil. O POST continua sendo a
// autoridade (revalida e trata a corrida "expirou entre abrir e enviar").
func ValidarTokenRedefinicao(db *sql.DB, token string) error {
	var expiraEm time.Time
	var usadoEm sql.NullTime
	const selectToken = `
		SELECT expira_em, usado_em
		FROM tokens_acao
		WHERE token = $1 AND tipo = 'redefinicao_senha'`
	if err := db.QueryRow(selectToken, token).Scan(&expiraEm, &usadoEm); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTokenNaoEncontrado
		}
		return fmt.Errorf("falha ao consultar token de redefinição: %w", err)
	}
	if usadoEm.Valid || !time.Now().Before(expiraEm) {
		return ErrTokenExpirado
	}
	return nil
}

// RedefinirSenha consome um token de redefinição válido e troca a senha da
// conta (Story 1.6). A força da nova senha é validada ANTES de tocar no token
// (senha reprovada -> ErrSenhaFraca, o mesmo link continua válido para nova
// tentativa). O token é resolvido por token+tipo (o valor já é globalmente
// único, mesmo padrão de VerificarEmail): inexistente -> ErrTokenNaoEncontrado;
// expirado ou já usado -> ErrTokenExpirado.
//
// No sucesso, numa única transação: UPDATE usuarios.senha_hash (bcrypt,
// DefaultCost); UPDATE tokens_acao.usado_em guardado por
// `usado_em IS NULL AND expira_em > now()` (RowsAffected()==0 ->
// ErrTokenExpirado, fecha a corrida SELECT->UPDATE como em VerificarEmail);
// UPDATE sessoes.revogado_em de TODAS as sessões ativas da conta. O access
// JWT stateless de <=30min expira sozinho; o efeito observável da revogação é
// um POST /api/auth/refresh com um cookie pré-redefinição passar a devolver
// 401. Nenhum outro campo de `usuarios` muda (email_verificado/ativo
// intactos): uma conta só-SSO que passa por aqui ganha os dois caminhos de
// login.
func RedefinirSenha(db *sql.DB, token, senha string) error {
	if err := ValidarForcaSenha(senha); err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	var usuarioID string
	var expiraEm time.Time
	var usadoEm sql.NullTime
	const selectToken = `
		SELECT usuario_id, expira_em, usado_em
		FROM tokens_acao
		WHERE token = $1 AND tipo = 'redefinicao_senha'`
	if err := tx.QueryRow(selectToken, token).Scan(&usuarioID, &expiraEm, &usadoEm); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTokenNaoEncontrado
		}
		return fmt.Errorf("falha ao consultar token de redefinição: %w", err)
	}
	if usadoEm.Valid || !time.Now().Before(expiraEm) {
		return ErrTokenExpirado
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("falha ao gerar hash da nova senha: %w", err)
	}

	if _, err := tx.Exec(`UPDATE usuarios SET senha_hash = $1 WHERE id = $2`, string(hash), usuarioID); err != nil {
		return fmt.Errorf("falha ao atualizar senha_hash: %w", err)
	}

	const marcarUsado = `
		UPDATE tokens_acao
		SET usado_em = now()
		WHERE token = $1 AND tipo = 'redefinicao_senha' AND usado_em IS NULL AND expira_em > now()`
	res, err := tx.Exec(marcarUsado, token)
	if err != nil {
		return fmt.Errorf("falha ao marcar token de redefinição como usado: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTokenExpirado
	}

	if _, err := tx.Exec(
		`UPDATE sessoes SET revogado_em = now() WHERE usuario_id = $1 AND revogado_em IS NULL`,
		usuarioID,
	); err != nil {
		return fmt.Errorf("falha ao revogar sessões da conta: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("falha ao commitar redefinição de senha: %w", err)
	}
	return nil
}

// BuscarUsuarioSessao resolve o usuário por id — sempre a partir do
// Postgres, nunca do claim do JWT (AD-6). Usado pelo middleware
// (middleware/auth.go) em toda requisição autenticada: papel/ativo/nome/
// email refletem o estado atual da conta, garantindo que um rebaixamento ou
// desativação derruba acesso já na próxima requisição.
func BuscarUsuarioSessao(db *sql.DB, usuarioID string) (UsuarioSessao, error) {
	var u UsuarioSessao
	const selectUsuario = `
		SELECT id, nome, email, papel, ativo
		FROM usuarios
		WHERE id = $1`
	err := db.QueryRow(selectUsuario, usuarioID).Scan(&u.ID, &u.Nome, &u.Email, &u.Papel, &u.Ativo)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UsuarioSessao{}, ErrUsuarioSessaoNaoEncontrado
		}
		return UsuarioSessao{}, fmt.Errorf("falha ao consultar usuário da sessão: %w", err)
	}
	return u, nil
}
