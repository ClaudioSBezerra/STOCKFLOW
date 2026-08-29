// Package services concentra as regras de negócio de autenticação (Story
// 1.3): autocadastro público sempre como `usuario`, verificação de e-mail via
// token de uso único, e o outbox/worker de e-mail transacional (AD-4/AD-18).
package services

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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
