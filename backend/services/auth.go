// Package services concentra as regras de negócio de autenticação: Story 1.3
// (autocadastro público sempre como `usuario`, verificação de e-mail via
// token de uso único, outbox/worker de e-mail transacional — AD-4/AD-18),
// Story 1.4 (login por e-mail/senha, emissão e rotação de sessão — AD-6),
// Story 1.6 (recuperação de senha por e-mail: solicitação com resposta
// genérica + outbox, redefinição consumindo o token de uso único e revogando
// todas as sessões da conta, política mínima de força de senha) e Story 1.10
// (bloqueio temporal da conta após N falhas consecutivas de senha no Login —
// contador `usuarios.tentativas_login_falhas`/`usuarios.bloqueado_ate` — e
// aplicação da política mínima de força de senha também no autocadastro).
package services

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
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

// mfaLoginTokenExpiracao é o prazo de validade do token opaco de uso único
// (`tokens_acao`, tipo `mfa_login`) emitido por IniciarLoginMFA quando o
// login por senha de uma conta com `mfa_habilitado=true` passa a exigir o
// segundo fator: 5min, fixado explicitamente pelo intent da Story 1.11.
const mfaLoginTokenExpiracao = 5 * time.Minute

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

// Bloqueio de força bruta por conta (Story 1.10, FR-36/SM-6): após
// maxTentativasLogin falhas de senha CONSECUTIVAS contra a mesma conta, o
// login fica recusado por duracaoBloqueioLogin — mesmo com a senha correta e
// sem revelar o tempo restante. Os valores 5 e 15min são o `[ASSUMPTION]` do
// PRD §4.1, fixado aqui como constante (não há configuração runtime).
const (
	maxTentativasLogin   = 5
	duracaoBloqueioLogin = 15 * time.Minute
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
	// ErrContaBloqueada indica que a conta tem `bloqueado_ate` no futuro por
	// ter acumulado maxTentativasLogin falhas de senha consecutivas (Story
	// 1.10). É a ÚNICA exceção deliberada e restrita à regra de não-enumeração
	// do épico: senha errada / e-mail inexistente / conta desativada continuam
	// todas em ErrCredenciaisInvalidas, mas o usuário legítimo que já fez 5
	// tentativas falhas precisa entender por que não entra nem com a senha
	// certa (ver Design Notes da spec-1-10). A mensagem nunca revela o tempo
	// restante nem promete que redefinir a senha destrava a conta — só a
	// expiração do prazo de 15 min faz isso.
	ErrContaBloqueada = errors.New("conta temporariamente bloqueada por excesso de tentativas de login")
	// ErrSessaoInvalida cobre refresh token ausente, expirado, já revogado ou
	// inexistente — mapeado para 401 TOKEN_EXPIRED pelo handler.
	ErrSessaoInvalida = errors.New("sessão inválida ou expirada")
	// ErrUsuarioSessaoNaoEncontrado indica que BuscarUsuarioSessao não
	// encontrou nenhuma linha para o usuario_id do claim `sub` — o middleware
	// trata isso como sessão revogada (SESSION_REVOKED), nunca como erro
	// interno: uma conta pode ter sido removida entre a emissão do token e o
	// uso.
	ErrUsuarioSessaoNaoEncontrado = errors.New("usuário da sessão não encontrado")

	// ErrMFACodigoInvalido cobre tanto "código TOTP errado" (ConcluirLoginMFA)
	// quanto "código de confirmação errado" (ConfirmarConfiguracaoMFA) — Story
	// 1.11. No caminho de login, é o mesmo sinal de força bruta já tratado pelo
	// contador da Story 1.10 (registrarFalhaLogin é chamado pelo próprio
	// handler/serviço no ponto correto, não por este erro em si).
	ErrMFACodigoInvalido = errors.New("código TOTP inválido")
	// ErrMFAJaConfigurado indica que a conta já tem `mfa_habilitado=true` —
	// tanto IniciarConfiguracaoMFA quanto ConfirmarConfiguracaoMFA recusam
	// reconfigurar um MFA já ativo (Story 1.11, sem opção de
	// desativar/reconfigurar nesta story).
	ErrMFAJaConfigurado = errors.New("autenticação em duas etapas já configurada para esta conta")
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
	// Política mínima de força de senha (Story 1.10): o autocadastro passa a
	// exigir 8+ caracteres com letra e dígito, igual à redefinição (Story 1.6).
	// Feito ANTES de qualquer escrita — senha reprovada não deixa linha órfã em
	// usuarios/tokens_acao/emails_pendentes. Também cobre o limite de 72 bytes
	// do bcrypt, tornando redundante um guard próprio de tamanho.
	if err := ValidarForcaSenha(senha); err != nil {
		return "", err
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
//
// MFAHabilitado É lido do Postgres (mfa_habilitado), como qualquer outro
// campo desta struct: estado de CONTA, sempre atual.
//
// Origem é a ÚNICA exceção deliberada: NUNCA é preenchida por
// BuscarUsuarioSessao/BuscarUsuarioPorEmailSSO (fica sempre "" quando lida
// direto daqui) — é metadado de UMA SESSÃO ("por qual caminho ela entrou:
// senha ou sso"), não estado de conta, e por isso é preenchida só pelo
// middleware.RequireAuth a partir do claim `origem` do JWT (Story 1.11).
type UsuarioSessao struct {
	ID            string
	Nome          string
	Email         string
	Papel         string
	Ativo         bool
	MFAHabilitado bool
	Origem        string
}

// Login valida e-mail/senha e devolve o id do usuário autenticado. Cobre a
// I/O Matrix da Story 1.4: senha errada, e-mail inexistente, e-mail não
// verificado, conta desativada e conta só-SSO (`senha_hash IS NULL`)
// resultam TODOS em ErrCredenciaisInvalidas — a mesma resposta, para nunca
// revelar qual condição falhou nem se o e-mail existe (regra explícita do
// contexto do épico).
//
// Story 1.10 acrescenta o bloqueio de força bruta por conta: cada falha de
// senha CONSECUTIVA incrementa `usuarios.tentativas_login_falhas` (UPDATE
// atômico, nunca contador recalculado em Go); ao alcançar maxTentativasLogin
// grava `bloqueado_ate = now() + duracaoBloqueioLogin`. Enquanto
// `bloqueado_ate` está no futuro, toda tentativa é recusada com
// ErrContaBloqueada — inclusive com a senha correta e sem revelar o tempo
// restante. Um bloqueio já expirado é destravado e o fluxo segue normal; um
// login bem-sucedido zera contador e prazo. A comparação bcrypt (real ou
// dummy) SEMPRE roda para uma linha encontrada, ANTES de qualquer return
// (inclusive no caminho "conta bloqueada"), para não regredir a defesa contra
// side-channel de tempo da Story 1.4.
func Login(db *sql.DB, email, senha string) (usuarioID string, err error) {
	normalizedEmail := normalizeEmail(email)
	if normalizedEmail == "" || strings.TrimSpace(senha) == "" {
		return "", ErrLoginValidacao
	}

	var id string
	var senhaHash sql.NullString
	var ativo, emailVerificado bool
	var tentativas int
	var bloqueadoAte sql.NullTime
	const selectUsuario = `
		SELECT id, senha_hash, ativo, email_verificado, tentativas_login_falhas, bloqueado_ate
		FROM usuarios
		WHERE lower(email) = $1`
	err = db.QueryRow(selectUsuario, normalizedEmail).Scan(&id, &senhaHash, &ativo, &emailVerificado, &tentativas, &bloqueadoAte)
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

	// Bloqueio de força bruta (Story 1.10). Esta checagem fica DEPOIS do
	// bcrypt.CompareHashAndPassword acima (real ou dummy): senão o caminho
	// "conta bloqueada" responderia sem o custo do bcrypt e viraria um oráculo
	// de tempo para "esta conta existe e está bloqueada".
	agora := time.Now().UTC()
	if bloqueadoAte.Valid && bloqueadoAte.Time.After(agora) {
		// Conta bloqueada: recusa toda tentativa — mesmo com a senha correta —
		// sem incrementar o contador nem estender o prazo.
		return "", ErrContaBloqueada
	}
	if bloqueadoAte.Valid {
		// Bloqueio expirado: destrava (zera contador/prazo) e segue o fluxo
		// normal como conta destravada. O predicado `bloqueado_ate <= now()`
		// fecha a corrida com uma tentativa concorrente que já tenha destravado
		// ou re-bloqueado a linha nesse meio-tempo. Falha aqui é deliberadamente
		// não-fatal — só registra para o operador, sem transformar o fluxo em
		// 500.
		if _, err := db.Exec(`UPDATE usuarios SET tentativas_login_falhas = 0, bloqueado_ate = NULL
			WHERE id = $1 AND bloqueado_ate <= now()`, id); err != nil {
			slog.Warn("falha ao destravar conta com bloqueio de login expirado", "usuario_id", id, "error", err)
		}
		// Zera os locais para o ramo de sucesso abaixo não re-disparar um UPDATE
		// idêntico (e evita o footgun de local defasado).
		tentativas = 0
		bloqueadoAte = sql.NullTime{}
	}

	if !ativo || !emailVerificado || !senhaHash.Valid || !senhaCorreta {
		// Só uma senha REALMENTE errada numa conta com senha conta como sinal de
		// força bruta. Conta desativada / e-mail não verificado / conta só-SSO
		// COM a senha correta não incrementam — puniria só o usuário legítimo.
		// A escrita de bookkeeping é não-fatal: uma falha nela não pode
		// transformar o 401 em 500, então só registramos.
		if senhaHash.Valid && !senhaCorreta {
			if err := registrarFalhaLogin(db, id); err != nil {
				slog.Warn("falha ao registrar tentativa de login malsucedida", "usuario_id", id, "error", err)
			}
		}
		return "", ErrCredenciaisInvalidas
	}

	if tentativas != 0 || bloqueadoAte.Valid {
		// Idem: não-fatal. O login já teve sucesso; no pior caso o contador fica
		// com um valor obsoleto até a próxima tentativa.
		if _, err := db.Exec(`UPDATE usuarios SET tentativas_login_falhas = 0, bloqueado_ate = NULL WHERE id = $1`, id); err != nil {
			slog.Warn("falha ao zerar contador de tentativas após login bem-sucedido", "usuario_id", id, "error", err)
		}
	}

	return id, nil
}

// registrarFalhaLogin incrementa o contador de falhas consecutivas de senha da
// conta e, quando o novo valor alcança maxTentativasLogin, grava
// `bloqueado_ate = now() + duracaoBloqueioLogin` — tudo num único UPDATE
// atômico no banco (nunca um `contador+1` calculado em Go e regravado), para
// que duas tentativas concorrentes não percam incrementos nem sobrescrevam o
// prazo uma da outra. O `CASE` só grava `bloqueado_ate` quando ele ainda é
// NULL: assim uma rajada de falhas concorrentes exatamente na 5ª não empurra o
// instante de desbloqueio um pouco além dos 15 min a cada escrita. `secs` (não
// `mins`) preserva durações abaixo de um minuto, caso a constante seja
// ajustada no futuro.
func registrarFalhaLogin(db *sql.DB, usuarioID string) error {
	const upd = `
		UPDATE usuarios
		SET tentativas_login_falhas = tentativas_login_falhas + 1,
		    bloqueado_ate = CASE
		        WHEN bloqueado_ate IS NULL AND tentativas_login_falhas + 1 >= $2 THEN now() + make_interval(secs => $3)
		        ELSE bloqueado_ate
		    END
		WHERE id = $1`
	_, err := db.Exec(upd, usuarioID, maxTentativasLogin, int(duracaoBloqueioLogin.Seconds()))
	return err
}

// AcessoClaims é o claim custom do access JWT (Story 1.11): embute
// jwt.RegisteredClaims (só `sub`/`exp`/`iat`, como antes) e acrescenta
// `origem` — a proveniência da sessão ("senha" ou "sso"), um dado IMUTÁVEL
// para a vida da sessão (mesma classe de decisão que já existe para `sub`),
// nunca um estado de conta que possa ficar defasado. middleware.RequireAuth
// parseia este struct (em vez de jwt.RegisteredClaims puro) para poder ler
// `claims.Origem` e repassá-lo a UsuarioSessao.Origem.
type AcessoClaims struct {
	jwt.RegisteredClaims
	Origem string `json:"origem"`
}

// gerarAccessToken emite o JWT de acesso (AD-6): HS256, claim mínimo
// (`sub`+`origem`) — deliberado, para que o middleware nunca tenha a
// tentação de confiar em papel/estado DE CONTA carimbado no token em vez de
// reconsultar `usuarios`. `origem` é a única exceção (Story 1.11, ver
// AcessoClaims): metadado de sessão, não de conta.
func gerarAccessToken(jwtSecret []byte, usuarioID, origem string) (string, error) {
	claims := AcessoClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   usuarioID,
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(accessTokenExpiracao)),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		},
		Origem: origem,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(jwtSecret)
	if err != nil {
		return "", fmt.Errorf("falha ao assinar access token: %w", err)
	}
	return signed, nil
}

// EmitirSessao gera o par de tokens de sessão (AD-6) para um usuário já
// autenticado (por senha, Login acima, ou por SSO, Story 1.9): um access JWT
// de 30min e um refresh token opaco de 2h, persistido em `sessoes`.
// `origem` ("senha" ou "sso", Story 1.11) é gravado tanto no claim do JWT
// quanto na coluna `sessoes.origem` — uma sessão SSO nunca vira "senha" (nem
// vice-versa) por refresh (ver RenovarSessao). expiraRefresh é devolvido
// para o chamador HTTP montar o `Set-Cookie` com o mesmo prazo.
func EmitirSessao(db *sql.DB, jwtSecret []byte, usuarioID, origem string) (accessToken, refreshToken string, expiraRefresh time.Time, err error) {
	accessToken, err = gerarAccessToken(jwtSecret, usuarioID, origem)
	if err != nil {
		return "", "", time.Time{}, err
	}

	refreshToken, err = gerarTokenAcao()
	if err != nil {
		return "", "", time.Time{}, err
	}

	expiraRefresh = time.Now().UTC().Add(RefreshTokenExpiracao)
	const insertSessao = `
		INSERT INTO sessoes (usuario_id, refresh_token, expira_em, origem)
		VALUES ($1, $2, $3, $4)`
	if _, err := db.Exec(insertSessao, usuarioID, refreshToken, expiraRefresh, origem); err != nil {
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
		RETURNING usuario_id, origem`
	var usuarioID, origem string
	err = tx.QueryRow(marcarRevogada, refreshTokenAtual).Scan(&usuarioID, &origem)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", time.Time{}, ErrSessaoInvalida
		}
		return "", "", time.Time{}, fmt.Errorf("falha ao revogar sessão atual: %w", err)
	}

	// origem é sempre a da sessão que está sendo rotacionada — nunca
	// recalculada nem aceita de fora: uma sessão SSO nunca vira "senha" (nem
	// vice-versa) por refresh (Story 1.11).
	novoAccess, err = gerarAccessToken(jwtSecret, usuarioID, origem)
	if err != nil {
		return "", "", time.Time{}, err
	}

	novoRefresh, err = gerarTokenAcao()
	if err != nil {
		return "", "", time.Time{}, err
	}

	expiraRefresh = time.Now().UTC().Add(RefreshTokenExpiracao)
	const insertSessao = `
		INSERT INTO sessoes (usuario_id, refresh_token, expira_em, origem)
		VALUES ($1, $2, $3, $4)`
	if _, err := tx.Exec(insertSessao, usuarioID, novoRefresh, expiraRefresh, origem); err != nil {
		return "", "", time.Time{}, fmt.Errorf("falha ao gravar sessão rotacionada: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", "", time.Time{}, fmt.Errorf("falha ao commitar rotação de sessão: %w", err)
	}

	return novoAccess, novoRefresh, expiraRefresh, nil
}

// IniciarLoginMFA emite o token opaco de uso único (Story 1.11) que
// LoginHandler devolve no lugar de uma sessão quando `usuario.MFAHabilitado`
// é true: mesmo molde de gerarTokenAcao/tokens_acao já usado por
// verificação de e-mail (Story 1.3) e redefinição de senha (Story 1.6), com
// `tipo='mfa_login'` e `expira_em = now() + mfaLoginTokenExpiracao` (5min).
func IniciarLoginMFA(db *sql.DB, usuarioID string) (string, error) {
	// Invalida qualquer token de mfa_login anterior ainda não usado desta
	// conta ANTES de emitir um novo — mesmo precedente de
	// SolicitarRedefinicaoSenha (Story 1.6): uma conta nunca deve ter mais de
	// um token de login por MFA válido ao mesmo tempo.
	const invalidarAnteriores = `
		UPDATE tokens_acao
		SET usado_em = now()
		WHERE usuario_id = $1 AND tipo = 'mfa_login' AND usado_em IS NULL`
	if _, err := db.Exec(invalidarAnteriores, usuarioID); err != nil {
		return "", fmt.Errorf("falha ao invalidar tokens de login por MFA anteriores: %w", err)
	}

	token, err := gerarTokenAcao()
	if err != nil {
		return "", err
	}

	expiraEm := time.Now().UTC().Add(mfaLoginTokenExpiracao)
	const insertToken = `
		INSERT INTO tokens_acao (usuario_id, token, tipo, expira_em)
		VALUES ($1, $2, 'mfa_login', $3)`
	if _, err := db.Exec(insertToken, usuarioID, token, expiraEm); err != nil {
		return "", fmt.Errorf("falha ao iniciar login por MFA: %w", err)
	}
	return token, nil
}

// ConcluirLoginMFA troca um token de `mfa_login` pendente por um
// `usuarioID` autenticado, mediante um código TOTP correto (Story 1.11):
// token inexistente -> ErrTokenNaoEncontrado; expirado ou já usado ->
// ErrTokenExpirado (molde de RedefinirSenha/VerificarEmail). Reaproveita o
// MESMO contador de força bruta da Story 1.10
// (tentativas_login_falhas/bloqueado_ate): uma conta já bloqueada recusa
// TODA tentativa — mesmo com o código certo — com ErrContaBloqueada, sem
// consumir o token; um código errado incrementa o contador (podendo
// bloquear a conta na 5ª falha, de código OU de senha, indistintamente) e
// NÃO consome o token, permitindo nova tentativa até expirar. No sucesso,
// marca `usado_em` guardado por `usado_em IS NULL AND expira_em > now()`
// (fecha a mesma corrida de VerificarEmail/RedefinirSenha) e zera o
// contador de falhas, se sujo.
func ConcluirLoginMFA(db *sql.DB, mfaToken, codigo string) (usuarioID string, err error) {
	var expiraEm time.Time
	var usadoEm sql.NullTime
	const selectToken = `
		SELECT usuario_id, expira_em, usado_em
		FROM tokens_acao
		WHERE token = $1 AND tipo = 'mfa_login'`
	if err := db.QueryRow(selectToken, mfaToken).Scan(&usuarioID, &expiraEm, &usadoEm); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrTokenNaoEncontrado
		}
		return "", fmt.Errorf("falha ao consultar token de login por MFA: %w", err)
	}
	if usadoEm.Valid || !time.Now().Before(expiraEm) {
		return "", ErrTokenExpirado
	}

	var segredo sql.NullString
	var tentativas int
	var bloqueadoAte sql.NullTime
	const selectUsuario = `
		SELECT mfa_secret, tentativas_login_falhas, bloqueado_ate
		FROM usuarios
		WHERE id = $1`
	if err := db.QueryRow(selectUsuario, usuarioID).Scan(&segredo, &tentativas, &bloqueadoAte); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrUsuarioSessaoNaoEncontrado
		}
		return "", fmt.Errorf("falha ao consultar usuário para login por MFA: %w", err)
	}

	agora := time.Now().UTC()
	if bloqueadoAte.Valid && bloqueadoAte.Time.After(agora) {
		// Conta bloqueada nesse meio-tempo (entre IniciarLoginMFA e a chegada do
		// código): recusa mesmo com o código correto, sem estender o prazo.
		return "", ErrContaBloqueada
	}
	if bloqueadoAte.Valid {
		// Bloqueio já expirado: destrava e segue, mesmo padrão de Login acima.
		if _, err := db.Exec(`UPDATE usuarios SET tentativas_login_falhas = 0, bloqueado_ate = NULL
			WHERE id = $1 AND bloqueado_ate <= now()`, usuarioID); err != nil {
			slog.Warn("falha ao destravar conta com bloqueio de login expirado (MFA)", "usuario_id", usuarioID, "error", err)
		}
		tentativas = 0
		bloqueadoAte = sql.NullTime{}
	}

	if !segredo.Valid || !ValidarCodigoTOTP(segredo.String, codigo) {
		// Código errado conta como tentativa de força bruta, exatamente como
		// senha errada em Login — o token de mfa_login NÃO é consumido, para
		// permitir nova tentativa até expirar ou a conta bloquear.
		if err := registrarFalhaLogin(db, usuarioID); err != nil {
			slog.Warn("falha ao registrar tentativa de código MFA malsucedida", "usuario_id", usuarioID, "error", err)
		}
		return "", ErrMFACodigoInvalido
	}

	// Defesa contra reuso do mesmo código dentro da mesma janela de validade
	// (~30s): grava atomicamente o passo TOTP usado, guardado por "ainda não
	// é este passo" — se outra requisição (ou quem quer que tenha
	// interceptado o mesmo código) já consumiu este exato passo para esta
	// conta, RowsAffected()==0 e o resultado é o mesmo ErrMFACodigoInvalido
	// (mesmo vocabulário, sem revelar que o código em si estava correto).
	passoAtual := PassoAtualTOTP()
	const marcarPassoUsado = `
		UPDATE usuarios
		SET mfa_ultimo_passo_usado = $2
		WHERE id = $1 AND (mfa_ultimo_passo_usado IS NULL OR mfa_ultimo_passo_usado <> $2)`
	resPasso, err := db.Exec(marcarPassoUsado, usuarioID, passoAtual)
	if err != nil {
		return "", fmt.Errorf("falha ao registrar passo TOTP usado (login por MFA): %w", err)
	}
	if n, _ := resPasso.RowsAffected(); n == 0 {
		if err := registrarFalhaLogin(db, usuarioID); err != nil {
			slog.Warn("falha ao registrar tentativa de reuso de código MFA", "usuario_id", usuarioID, "error", err)
		}
		return "", ErrMFACodigoInvalido
	}

	const marcarUsado = `
		UPDATE tokens_acao
		SET usado_em = now()
		WHERE token = $1 AND tipo = 'mfa_login' AND usado_em IS NULL AND expira_em > now()`
	res, err := db.Exec(marcarUsado, mfaToken)
	if err != nil {
		return "", fmt.Errorf("falha ao marcar token de login por MFA como usado: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", ErrTokenExpirado
	}

	if tentativas != 0 || bloqueadoAte.Valid {
		if _, err := db.Exec(`UPDATE usuarios SET tentativas_login_falhas = 0, bloqueado_ate = NULL WHERE id = $1`, usuarioID); err != nil {
			slog.Warn("falha ao zerar contador de tentativas após login por MFA bem-sucedido", "usuario_id", usuarioID, "error", err)
		}
	}

	slog.Info("login concluído via segundo fator (MFA)", "usuario_id", usuarioID)
	return usuarioID, nil
}

// IniciarConfiguracaoMFA gera um novo segredo TOTP e a URL `otpauth://`
// correspondente PARA A TELA renderizar o QR Code — não grava nada no banco
// (Story 1.11): o cliente devolve o segredo em POST /mfa/confirmar, e só
// ConfirmarConfiguracaoMFA persiste algo. Não há ganho de segurança em
// manter um "rascunho" pendente no servidor entre os dois passos, já que a
// operação é sempre sobre a própria conta de quem chama (RequireAuth), só
// complexidade extra (expiração/limpeza de rascunho).
func IniciarConfiguracaoMFA(email string) (segredo, otpauthURL string, err error) {
	segredo, err = GerarSegredoTOTP()
	if err != nil {
		return "", "", err
	}
	otpauthURL = URLProvisionamentoTOTP(email, segredo)
	return segredo, otpauthURL, nil
}

// ConfirmarConfiguracaoMFA confere a SENHA ATUAL da conta (bcrypt, mesmo
// vocabulário de erro do Login: ErrCredenciaisInvalidas) ANTES de validar o
// código TOTP e gravar qualquer coluna — sem isto, um access token roubado
// (válido até 30min) bastaria para um atacante habilitar MFA com o PRÓPRIO
// autenticador na conta da vítima, sequestrando os logins futuros dela mesmo
// depois do token expirar. Só então o código TOTP é validado contra o
// segredo recém-gerado e, se correto, `mfa_secret`/`mfa_habilitado=true` são
// gravados (Story 1.11). Senha errada -> ErrCredenciaisInvalidas, nenhuma
// escrita. Código errado -> ErrMFACodigoInvalido, NENHUMA coluna gravada — o
// mesmo segredo/QR continua válido para nova tentativa. O UPDATE final é
// guardado por `mfa_habilitado = false`: se `RowsAffected() == 0`, a conta já
// tinha MFA configurado (corrida entre duas abas, ou reenvio duplicado do
// mesmo POST) -> ErrMFAJaConfigurado. `mfa_ultimo_passo_usado` já nasce
// gravado com o passo TOTP deste sucesso, no mesmo UPDATE — mesma defesa
// contra reuso de código usada por ConcluirLoginMFA.
func ConfirmarConfiguracaoMFA(db *sql.DB, usuarioID, senhaAtual, segredo, codigo string) error {
	var senhaHash sql.NullString
	const selectUsuario = `SELECT senha_hash FROM usuarios WHERE id = $1`
	if err := db.QueryRow(selectUsuario, usuarioID).Scan(&senhaHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUsuarioSessaoNaoEncontrado
		}
		return fmt.Errorf("falha ao consultar usuário para confirmar configuração de MFA: %w", err)
	}
	// Mesma defesa contra side-channel de tempo do Login: uma conta só-SSO
	// (senha_hash nulo) ainda compara contra um hash de custo equivalente, em
	// vez de recusar de imediato — nunca teria como acertar `senhaAtual`
	// mesmo assim, já que não existe senha nenhuma para essa conta.
	hashParaComparar := dummyBcryptHash
	if senhaHash.Valid {
		hashParaComparar = []byte(senhaHash.String)
	}
	if bcrypt.CompareHashAndPassword(hashParaComparar, []byte(senhaAtual)) != nil {
		return ErrCredenciaisInvalidas
	}

	if !ValidarCodigoTOTP(segredo, codigo) {
		return ErrMFACodigoInvalido
	}

	const upd = `
		UPDATE usuarios
		SET mfa_secret = $1, mfa_habilitado = true, mfa_ultimo_passo_usado = $2
		WHERE id = $3 AND mfa_habilitado = false`
	res, err := db.Exec(upd, segredo, PassoAtualTOTP(), usuarioID)
	if err != nil {
		return fmt.Errorf("falha ao confirmar configuração de MFA: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrMFAJaConfigurado
	}
	slog.Info("configuração de MFA concluída", "usuario_id", usuarioID)
	return nil
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
		SELECT id, nome, email, papel, ativo, mfa_habilitado
		FROM usuarios
		WHERE id = $1`
	err := db.QueryRow(selectUsuario, usuarioID).Scan(&u.ID, &u.Nome, &u.Email, &u.Papel, &u.Ativo, &u.MFAHabilitado)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UsuarioSessao{}, ErrUsuarioSessaoNaoEncontrado
		}
		return UsuarioSessao{}, fmt.Errorf("falha ao consultar usuário da sessão: %w", err)
	}
	return u, nil
}
