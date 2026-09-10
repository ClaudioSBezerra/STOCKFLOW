package services

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

// Suíte do convite nominal de acesso a uma Empresa — Story 9.3, spec-9-3.
// Cobre cada linha da I/O Matrix no nível de service, inclusive a corrida de
// duplo-resgate do mesmo token.

// conviteDeTeste grava um convite pendente direto na tabela e devolve o token.
// Grava direto (em vez de chamar EmitirConvite) DE PROPÓSITO: é setup de
// dezenas de testes que só precisam de "um convite válido para este e-mail",
// e passar por EmitirConvite os acoplaria aos guards dela (por exemplo o 409
// de e-mail já cadastrado, que impediria montar o cenário de e-mail
// duplicado). `criado_por` fica NULL: nenhum teste desta suíte precisa de um
// emissor real, e a coluna é nullable.
func conviteDeTeste(t *testing.T, db *sql.DB, empresaID, email string) string {
	t.Helper()
	token, err := gerarTokenAcao()
	if err != nil {
		t.Fatalf("conviteDeTeste: gerar token: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO convites_empresa (empresa_id, email, token, expira_em)
		 VALUES ($1, $2, $3, $4)`,
		empresaID, normalizeEmail(email), token, time.Now().UTC().Add(conviteExpiracao),
	)
	if err != nil {
		t.Fatalf("conviteDeTeste(%s): %v", email, err)
	}
	return token
}

// gestorDeTeste insere um `gestor` ativo na Empresa e devolve o id — o
// emissor do convite nos testes que exercitam EmitirConvite de verdade.
func gestorDeTeste(t *testing.T, db *sql.DB, empresaID, nome, email string) string {
	t.Helper()
	var id string
	err := db.QueryRow(
		`INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, empresa_id)
		 VALUES ($1, $2, 'hash-qualquer', 'gestor', true, true, $3) RETURNING id`,
		nome, normalizeEmail(email), empresaID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("gestorDeTeste(%s): %v", email, err)
	}
	return id
}

// lerConvite devolve as três colunas de estado de um convite pelo token.
func lerConvite(t *testing.T, db *sql.DB, token string) (expiraEm time.Time, usadoEm, revogadoEm sql.NullTime) {
	t.Helper()
	err := db.QueryRow(
		`SELECT expira_em, usado_em, revogado_em FROM convites_empresa WHERE token = $1`, token,
	).Scan(&expiraEm, &usadoEm, &revogadoEm)
	if err != nil {
		t.Fatalf("lerConvite: %v", err)
	}
	return
}

// TestEmitirConvite_Sucesso prova a linha "Emitir convite" da I/O Matrix: o
// e-mail é normalizado na gravação, o prazo é conviteExpiracao, a situação
// nasce pendente e o link aponta para o /cadastro SOB o prefixo da Empresa.
func TestEmitirConvite_Sucesso(t *testing.T) {
	db := testDB(t)
	emissor := gestorDeTeste(t, db, empresaTeste, "Maria Gestora", "maria.emite@empresa.com")

	c, err := EmitirConvite(db, testEmailCfg, empresaTeste, slugEmpresaTeste, emissor, "  Fulano@X.COM ")
	if err != nil {
		t.Fatalf("EmitirConvite: %v", err)
	}
	if c.Email != "fulano@x.com" {
		t.Errorf("email = %q, want %q (normalizado)", c.Email, "fulano@x.com")
	}
	if c.Situacao != ConvitePendente {
		t.Errorf("situacao = %q, want %q", c.Situacao, ConvitePendente)
	}
	if c.ID == "" {
		t.Error("id vazio")
	}
	if diff := time.Now().UTC().Add(conviteExpiracao).Sub(c.ExpiraEm); diff < 0 || diff > time.Minute {
		t.Errorf("expiraEm = %v, want ~7 dias a partir de agora (diff=%v)", c.ExpiraEm, diff)
	}

	var token string
	if err := db.QueryRow(`SELECT token FROM convites_empresa WHERE id = $1`, c.ID).Scan(&token); err != nil {
		t.Fatalf("falha ao reler convite: %v", err)
	}
	want := "http://test.local/e/" + slugEmpresaTeste + "/cadastro?token=" + token
	if c.Link != want {
		t.Errorf("link = %q, want %q", c.Link, want)
	}
	// O token é o segredo de 32 bytes de gerarTokenAcao (43 chars base64url),
	// nunca um código curto adivinhável.
	if len(token) != 43 {
		t.Errorf("len(token) = %d, want 43 (32 bytes em base64url)", len(token))
	}
}

// TestEmitirConvite_EmailInvalido prova a linha "E-mail vazio/inválido":
// 400 VALIDATION_ERROR no service (ErrConviteValidacao) e NENHUMA linha
// gravada.
func TestEmitirConvite_EmailInvalido(t *testing.T) {
	db := testDB(t)
	emissor := gestorDeTeste(t, db, empresaTeste, "Maria", "maria.invalido@empresa.com")

	for _, email := range []string{"", "   ", "sem-arroba", "@dominio.com", "local@", "com espaco@x.com", "a@b@c.com"} {
		t.Run(email, func(t *testing.T) {
			antes := contarLinhas(t, db, "convites_empresa")
			_, err := EmitirConvite(db, testEmailCfg, empresaTeste, slugEmpresaTeste, emissor, email)
			if !errors.Is(err, ErrConviteValidacao) {
				t.Fatalf("erro = %v, want ErrConviteValidacao", err)
			}
			if depois := contarLinhas(t, db, "convites_empresa"); depois != antes {
				t.Errorf("count(convites_empresa) = %d, want %d — nenhuma escrita esperada", depois, antes)
			}
		})
	}
}

// TestEmitirConvite_EmailJaTemContaNaEmpresa prova a linha "E-mail já tem
// conta na Empresa": 409 CONFLICT, nenhuma linha gravada — falhar cedo, no
// emissor, é o único momento em que alguém pode agir.
func TestEmitirConvite_EmailJaTemContaNaEmpresa(t *testing.T) {
	db := testDB(t)
	emissor := gestorDeTeste(t, db, empresaTeste, "Maria", "maria.jatem@empresa.com")
	gestorDeTeste(t, db, empresaTeste, "Já Existe", "jaexiste@empresa.com")

	antes := contarLinhas(t, db, "convites_empresa")
	// Caixa diferente de propósito: a colisão é por lower(email).
	_, err := EmitirConvite(db, testEmailCfg, empresaTeste, slugEmpresaTeste, emissor, "JaExiste@Empresa.com")
	if !errors.Is(err, ErrConviteEmailJaCadastrado) {
		t.Fatalf("erro = %v, want ErrConviteEmailJaCadastrado", err)
	}
	if depois := contarLinhas(t, db, "convites_empresa"); depois != antes {
		t.Errorf("count(convites_empresa) = %d, want %d", depois, antes)
	}
}

// TestListarConvites_SituacaoDerivadaELink prova a linha "Listar convites":
// cada convite traz a situação derivada na leitura, e o `link` só aparece nos
// pendentes.
func TestListarConvites_SituacaoDerivadaELink(t *testing.T) {
	db := testDB(t)
	emissor := gestorDeTeste(t, db, empresaTeste, "Maria Gestora", "maria.lista@empresa.com")

	pendente, err := EmitirConvite(db, testEmailCfg, empresaTeste, slugEmpresaTeste, emissor, "pendente@x.com")
	if err != nil {
		t.Fatalf("EmitirConvite(pendente): %v", err)
	}
	usado := conviteDeTeste(t, db, empresaTeste, "usado@x.com")
	if _, err := db.Exec(`UPDATE convites_empresa SET usado_em = now() WHERE token = $1`, usado); err != nil {
		t.Fatalf("marcar usado: %v", err)
	}
	revogado := conviteDeTeste(t, db, empresaTeste, "revogado@x.com")
	if _, err := db.Exec(`UPDATE convites_empresa SET revogado_em = now() WHERE token = $1`, revogado); err != nil {
		t.Fatalf("marcar revogado: %v", err)
	}
	expirado := conviteDeTeste(t, db, empresaTeste, "expirado@x.com")
	if _, err := db.Exec(`UPDATE convites_empresa SET expira_em = now() - interval '1 hour' WHERE token = $1`, expirado); err != nil {
		t.Fatalf("forçar expiração: %v", err)
	}

	convites, err := ListarConvites(db, testEmailCfg, empresaTeste, slugEmpresaTeste)
	if err != nil {
		t.Fatalf("ListarConvites: %v", err)
	}
	if len(convites) != 4 {
		t.Fatalf("len(convites) = %d, want 4", len(convites))
	}

	porEmail := map[string]ConviteResumo{}
	for _, c := range convites {
		porEmail[c.Email] = c
	}
	casos := map[string]string{
		"pendente@x.com": ConvitePendente,
		"usado@x.com":    ConviteUsado,
		"revogado@x.com": ConviteRevogado,
		"expirado@x.com": ConviteExpirado,
	}
	for email, wantSituacao := range casos {
		c, ok := porEmail[email]
		if !ok {
			t.Fatalf("convite de %s ausente da listagem", email)
		}
		if c.Situacao != wantSituacao {
			t.Errorf("%s: situacao = %q, want %q", email, c.Situacao, wantSituacao)
		}
		if wantSituacao == ConvitePendente {
			if c.Link == "" {
				t.Errorf("%s: link vazio em convite pendente", email)
			}
		} else if c.Link != "" {
			t.Errorf("%s: link = %q, want vazio (só pendentes têm link)", email, c.Link)
		}
	}
	if got := porEmail["pendente@x.com"].Link; got != pendente.Link {
		t.Errorf("link da listagem = %q, want %q (o mesmo devolvido na emissão)", got, pendente.Link)
	}
	if got := porEmail["pendente@x.com"].CriadoPorNome; got != "Maria Gestora" {
		t.Errorf("criadoPorNome = %q, want %q", got, "Maria Gestora")
	}
}

// TestRevogarConvite_Pendente prova a linha "Revogar pendente": 200 e
// `revogado_em` preenchido.
func TestRevogarConvite_Pendente(t *testing.T) {
	db := testDB(t)
	emissor := gestorDeTeste(t, db, empresaTeste, "Maria", "maria.revoga@empresa.com")

	c, err := EmitirConvite(db, testEmailCfg, empresaTeste, slugEmpresaTeste, emissor, "revogar@x.com")
	if err != nil {
		t.Fatalf("EmitirConvite: %v", err)
	}
	if err := RevogarConvite(db, empresaTeste, c.ID); err != nil {
		t.Fatalf("RevogarConvite: %v", err)
	}

	var revogadoEm sql.NullTime
	if err := db.QueryRow(`SELECT revogado_em FROM convites_empresa WHERE id = $1`, c.ID).Scan(&revogadoEm); err != nil {
		t.Fatalf("falha ao reler convite: %v", err)
	}
	if !revogadoEm.Valid {
		t.Error("revogado_em vazio após RevogarConvite")
	}

	// Revogar de novo é idempotente — o estado final pedido já é o da linha.
	if err := RevogarConvite(db, empresaTeste, c.ID); err != nil {
		t.Errorf("segunda revogação: %v, want nil (idempotente)", err)
	}
}

// TestRevogarConvite_UsadoEInexistente prova a linha "Revogar
// usado/inexistente": 409 CONFLICT para um convite já resgatado; 404
// NOT_FOUND para id aleatório e para id malformado (não-UUID).
func TestRevogarConvite_UsadoEInexistente(t *testing.T) {
	db := testDB(t)

	usado := conviteDeTeste(t, db, empresaTeste, "usado-revoga@x.com")
	var usadoID string
	if err := db.QueryRow(`SELECT id FROM convites_empresa WHERE token = $1`, usado).Scan(&usadoID); err != nil {
		t.Fatalf("ler id: %v", err)
	}
	if _, err := db.Exec(`UPDATE convites_empresa SET usado_em = now() WHERE id = $1`, usadoID); err != nil {
		t.Fatalf("marcar usado: %v", err)
	}

	if err := RevogarConvite(db, empresaTeste, usadoID); !errors.Is(err, ErrConviteJaUsado) {
		t.Errorf("usado: erro = %v, want ErrConviteJaUsado", err)
	}
	if err := RevogarConvite(db, empresaTeste, "11111111-1111-1111-1111-111111111111"); !errors.Is(err, ErrConviteNaoEncontrado) {
		t.Errorf("inexistente: erro = %v, want ErrConviteNaoEncontrado", err)
	}
	if err := RevogarConvite(db, empresaTeste, "nao-e-uuid"); !errors.Is(err, ErrConviteNaoEncontrado) {
		t.Errorf("malformado: erro = %v, want ErrConviteNaoEncontrado", err)
	}
}

// TestValidarTokenConvite prova a linha "Validar link ao abrir" e cada motivo
// de recusa do GET — o mesmo vocabulário de erro do POST.
func TestValidarTokenConvite(t *testing.T) {
	db := testDB(t)

	valido := conviteDeTeste(t, db, empresaTeste, "Valido@X.com")
	email, err := ValidarTokenConvite(db, empresaTeste, valido)
	if err != nil {
		t.Fatalf("ValidarTokenConvite(válido): %v", err)
	}
	if email != "valido@x.com" {
		t.Errorf("email = %q, want %q (normalizado)", email, "valido@x.com")
	}
	// Validar NÃO consome: o convite continua pendente para o POST.
	if _, usadoEm, _ := lerConvite(t, db, valido); usadoEm.Valid {
		t.Error("usado_em preenchido após uma validação que não deveria consumir")
	}

	if _, err := ValidarTokenConvite(db, empresaTeste, "token-que-nunca-existiu"); !errors.Is(err, ErrConviteNaoEncontrado) {
		t.Errorf("inexistente: erro = %v, want ErrConviteNaoEncontrado", err)
	}
	if _, err := ValidarTokenConvite(db, empresaTeste, ""); !errors.Is(err, ErrConviteNaoEncontrado) {
		t.Errorf("vazio: erro = %v, want ErrConviteNaoEncontrado", err)
	}

	expirado := conviteDeTeste(t, db, empresaTeste, "exp@x.com")
	if _, err := db.Exec(`UPDATE convites_empresa SET expira_em = now() - interval '1 hour' WHERE token = $1`, expirado); err != nil {
		t.Fatalf("forçar expiração: %v", err)
	}
	if _, err := ValidarTokenConvite(db, empresaTeste, expirado); !errors.Is(err, ErrConviteExpirado) {
		t.Errorf("expirado: erro = %v, want ErrConviteExpirado", err)
	}

	usado := conviteDeTeste(t, db, empresaTeste, "usd@x.com")
	if _, err := db.Exec(`UPDATE convites_empresa SET usado_em = now() WHERE token = $1`, usado); err != nil {
		t.Fatalf("marcar usado: %v", err)
	}
	if _, err := ValidarTokenConvite(db, empresaTeste, usado); !errors.Is(err, ErrConviteJaUsado) {
		t.Errorf("usado: erro = %v, want ErrConviteJaUsado", err)
	}

	revogado := conviteDeTeste(t, db, empresaTeste, "rev@x.com")
	if _, err := db.Exec(`UPDATE convites_empresa SET revogado_em = now() WHERE token = $1`, revogado); err != nil {
		t.Fatalf("marcar revogado: %v", err)
	}
	if _, err := ValidarTokenConvite(db, empresaTeste, revogado); !errors.Is(err, ErrConviteRevogado) {
		t.Errorf("revogado: erro = %v, want ErrConviteRevogado", err)
	}
}

// TestCadastrar_ComConviteValido prova a linha "Cadastro com convite válido"
// e o AC principal: uma conta `usuario` na Empresa do convite, com `usado_em`
// preenchido e o e-mail de verificação enfileirado. O e-mail do formulário vai
// em OUTRA caixa de propósito — a comparação é sobre o valor normalizado.
func TestCadastrar_ComConviteValido(t *testing.T) {
	db := testDB(t)
	token := conviteDeTeste(t, db, empresaTeste, "fulano@x.com")

	id, err := Cadastrar(db, testEmailCfg, empresaTeste, slugEmpresaTeste, "Fulano", "FULANO@x.com", "senha-123456", token)
	if err != nil {
		t.Fatalf("Cadastrar: %v", err)
	}

	var papel, empresaID string
	if err := db.QueryRow(`SELECT papel, empresa_id FROM usuarios WHERE id = $1`, id).Scan(&papel, &empresaID); err != nil {
		t.Fatalf("falha ao ler conta: %v", err)
	}
	if papel != PapelUsuario {
		t.Errorf("papel = %q, want %q — convite nunca concede papel", papel, PapelUsuario)
	}
	if empresaID != empresaTeste {
		t.Errorf("empresa_id = %q, want %q (a Empresa do convite)", empresaID, empresaTeste)
	}
	if _, usadoEm, _ := lerConvite(t, db, token); !usadoEm.Valid {
		t.Error("usado_em vazio após o cadastro — o convite é de uso único")
	}
	if n := contarLinhas(t, db, "emails_pendentes"); n != 1 {
		t.Errorf("count(emails_pendentes) = %d, want 1 (verificação de e-mail continua acontecendo)", n)
	}
}

// TestCadastrar_ConviteRecusado percorre cada linha de recusa da I/O Matrix e
// afirma, em todas, que NENHUMA conta é criada.
func TestCadastrar_ConviteRecusado(t *testing.T) {
	db := testDB(t)

	preparar := func(t *testing.T, email, sql string) string {
		t.Helper()
		token := conviteDeTeste(t, db, empresaTeste, email)
		if sql != "" {
			if _, err := db.Exec(sql, token); err != nil {
				t.Fatalf("preparar convite: %v", err)
			}
		}
		return token
	}

	casos := []struct {
		desc     string
		token    func(t *testing.T) string
		email    string
		wantErr  error
		queimado bool // o convite deixa de ser resgatável depois deste caso?
	}{
		{
			desc:    "sem token",
			token:   func(*testing.T) string { return "" },
			email:   "semtoken@x.com",
			wantErr: ErrConviteNaoEncontrado,
		},
		{
			desc:    "token inexistente",
			token:   func(*testing.T) string { return "token-aleatorio-que-nao-existe" },
			email:   "inexistente@x.com",
			wantErr: ErrConviteNaoEncontrado,
		},
		{
			desc:    "e-mail diferente do convidado",
			token:   func(t *testing.T) string { return preparar(t, "convidado@x.com", "") },
			email:   "outro@x.com",
			wantErr: ErrConviteEmailDivergente,
		},
		{
			desc: "convite expirado",
			token: func(t *testing.T) string {
				return preparar(t, "expirou@x.com", `UPDATE convites_empresa SET expira_em = now() - interval '1 hour' WHERE token = $1`)
			},
			email:   "expirou@x.com",
			wantErr: ErrConviteExpirado,
		},
		{
			desc: "convite já usado",
			token: func(t *testing.T) string {
				return preparar(t, "jausado@x.com", `UPDATE convites_empresa SET usado_em = now() WHERE token = $1`)
			},
			email:   "jausado@x.com",
			wantErr: ErrConviteJaUsado,
		},
		{
			desc: "convite revogado",
			token: func(t *testing.T) string {
				return preparar(t, "revogou@x.com", `UPDATE convites_empresa SET revogado_em = now() WHERE token = $1`)
			},
			email:   "revogou@x.com",
			wantErr: ErrConviteRevogado,
		},
	}

	for _, c := range casos {
		t.Run(c.desc, func(t *testing.T) {
			token := c.token(t)
			antes := contarLinhas(t, db, "usuarios")
			_, err := Cadastrar(db, testEmailCfg, empresaTeste, slugEmpresaTeste, "Alguém", c.email, "senha-123456", token)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("erro = %v, want %v", err, c.wantErr)
			}
			if depois := contarLinhas(t, db, "usuarios"); depois != antes {
				t.Errorf("count(usuarios) = %d, want %d — nenhuma conta pode nascer", depois, antes)
			}
		})
	}
}

// TestCadastrar_PayloadInvalidoNaoQueimaConvite prova a última linha da I/O
// Matrix: uma senha fraca com token válido devolve VALIDATION_ERROR e o
// convite CONTINUA pendente — a validação de campos vence antes da resolução
// do convite.
func TestCadastrar_PayloadInvalidoNaoQueimaConvite(t *testing.T) {
	db := testDB(t)
	token := conviteDeTeste(t, db, empresaTeste, "naoqueima@x.com")

	if _, err := Cadastrar(db, testEmailCfg, empresaTeste, slugEmpresaTeste, "Alguém", "naoqueima@x.com", "abc", token); !errors.Is(err, ErrSenhaFraca) {
		t.Fatalf("senha fraca: erro = %v, want ErrSenhaFraca", err)
	}
	if _, err := Cadastrar(db, testEmailCfg, empresaTeste, slugEmpresaTeste, "", "naoqueima@x.com", "senha-123456", token); !errors.Is(err, ErrCadastroValidacao) {
		t.Fatalf("nome vazio: erro = %v, want ErrCadastroValidacao", err)
	}

	if _, usadoEm, _ := lerConvite(t, db, token); usadoEm.Valid {
		t.Fatal("usado_em preenchido — um payload inválido nunca pode queimar o convite")
	}
	// E o convite continua servindo para o cadastro correto.
	if _, err := Cadastrar(db, testEmailCfg, empresaTeste, slugEmpresaTeste, "Alguém", "naoqueima@x.com", "senha-123456", token); err != nil {
		t.Fatalf("cadastro válido depois das tentativas inválidas: %v", err)
	}
}

// TestCadastrar_DuploResgateConcorrente prova a invariante de uso único sob
// corrida (AD-22): duas goroutines resgatando o MESMO token ao mesmo tempo
// produzem exatamente UMA conta. Sem o `FOR UPDATE` + o UPDATE condicional
// com RowsAffected()==1, as duas transações passariam pelo SELECT e a segunda
// só falharia (ou não) por acidente do índice de e-mail.
func TestCadastrar_DuploResgateConcorrente(t *testing.T) {
	db := testDB(t)
	token := conviteDeTeste(t, db, empresaTeste, "corrida@x.com")

	const n = 2
	var wg sync.WaitGroup
	erros := make([]error, n)
	inicio := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-inicio
			_, erros[i] = Cadastrar(db, testEmailCfg, empresaTeste, slugEmpresaTeste, "Corrida", "corrida@x.com", "senha-123456", token)
		}(i)
	}
	close(inicio)
	wg.Wait()

	sucessos := 0
	for i, err := range erros {
		if err == nil {
			sucessos++
			continue
		}
		// A perdedora sai por "convite já usado" (o caminho normal) ou, se o
		// INSERT em usuarios chegou primeiro ao índice, por e-mail duplicado.
		// Qualquer um dos dois é uma recusa legítima; o que não pode existir é
		// uma segunda CONTA.
		if !errors.Is(err, ErrConviteJaUsado) && !errors.Is(err, ErrEmailDuplicado) {
			t.Errorf("goroutine %d: erro = %v, want ErrConviteJaUsado ou ErrEmailDuplicado", i, err)
		}
	}
	if sucessos != 1 {
		t.Errorf("cadastros bem-sucedidos = %d, want exatamente 1", sucessos)
	}

	var contas int
	if err := db.QueryRow(
		`SELECT count(*) FROM usuarios WHERE empresa_id = $1 AND email = 'corrida@x.com'`, empresaTeste,
	).Scan(&contas); err != nil {
		t.Fatalf("contar contas: %v", err)
	}
	if contas != 1 {
		t.Errorf("contas criadas = %d, want 1", contas)
	}
}
