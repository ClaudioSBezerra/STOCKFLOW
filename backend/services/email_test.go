package services

import (
	"strings"
	"testing"
	"time"
)

// TestEnfileirarEmail_GravaLinhaNaTransacao prova que EnfileirarEmail grava
// a linha esperada em emails_pendentes, dentro da transação recebida.
func TestEnfileirarEmail_GravaLinhaNaTransacao(t *testing.T) {
	db := testDB(t)
	usuarioID := inserirUsuarioDeTeste(t, db, "outbox@empresa.com")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("falha ao iniciar transação: %v", err)
	}
	defer tx.Rollback()

	err = EnfileirarEmail(tx, "outbox@empresa.com", usuarioID, "verificacao_conta", map[string]any{"nome": "Fulano", "link": "http://x"})
	if err != nil {
		t.Fatalf("EnfileirarEmail retornou erro: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("falha ao commitar: %v", err)
	}

	var destinatario, tipo, status string
	if err := db.QueryRow(`SELECT destinatario, tipo, status FROM emails_pendentes WHERE usuario_id = $1`, usuarioID).
		Scan(&destinatario, &tipo, &status); err != nil {
		t.Fatalf("falha ao reler linha: %v", err)
	}
	if destinatario != "outbox@empresa.com" || tipo != "verificacao_conta" || status != "pendente" {
		t.Errorf("linha gravada inesperada: destinatario=%q tipo=%q status=%q", destinatario, tipo, status)
	}
}

func TestRenderizarTemplate_VerificacaoConta(t *testing.T) {
	tpl, err := renderizarTemplate("verificacao_conta", map[string]any{
		"nome": "Fulano <script>",
		"link": "http://test.local/verificar-email?token=abc123",
	})
	if err != nil {
		t.Fatalf("renderizarTemplate retornou erro: %v", err)
	}
	if tpl.Assunto == "" {
		t.Error("assunto vazio")
	}
	if !strings.Contains(tpl.CorpoHTML, "http://test.local/verificar-email?token=abc123") {
		t.Error("corpo não contém o link de verificação")
	}
	if strings.Contains(tpl.CorpoHTML, "<script>") {
		t.Error("corpo não escapou o nome do usuário — risco de HTML injection")
	}
}

func TestRenderizarTemplate_RedefinicaoSenha(t *testing.T) {
	tpl, err := renderizarTemplate("redefinicao_senha", map[string]any{
		"nome": "Fulano <script>",
		"link": "http://test.local/redefinir-senha?token=abc123",
	})
	if err != nil {
		t.Fatalf("renderizarTemplate retornou erro: %v", err)
	}
	if tpl.Assunto != "Redefinição de senha — stockflow" {
		t.Errorf("assunto = %q, want %q", tpl.Assunto, "Redefinição de senha — stockflow")
	}
	if !strings.Contains(tpl.CorpoHTML, "http://test.local/redefinir-senha?token=abc123") {
		t.Error("corpo não contém o link de redefinição")
	}
	if !strings.Contains(tpl.CorpoHTML, "30 minutos") {
		t.Error("corpo não menciona a expiração de 30 minutos")
	}
	if strings.Contains(tpl.CorpoHTML, "<script>") {
		t.Error("corpo não escapou o nome do usuário — risco de HTML injection")
	}
}

func TestRenderizarTemplate_TipoDesconhecidoRetornaErro(t *testing.T) {
	_, err := renderizarTemplate("tipo-nao-implementado", map[string]any{})
	if err == nil {
		t.Fatal("esperava erro para tipo sem template implementado")
	}
}

// TestCarregarEmailConfig prova os dois caminhos de fallback de
// CarregarEmailConfig: SMTP_PORT (default "587") e APP_URL (default
// "http://localhost:8081") quando as variáveis não estão definidas, e o
// valor explícito quando estão. As demais variáveis (SMTP_HOST/USER/
// PASSWORD/FROM) não têm default — são sempre repassadas cruas de
// os.Getenv — e são conferidas junto para garantir que o fallback não
// vaza para elas.
func TestCarregarEmailConfig(t *testing.T) {
	cases := []struct {
		nome       string
		smtpPort   string // "" simula a variável não definida
		appURL     string
		wantPort   string
		wantAppURL string
	}{
		{
			nome:       "SMTP_PORT/APP_URL não definidas: usa os defaults",
			smtpPort:   "",
			appURL:     "",
			wantPort:   "587",
			wantAppURL: "http://localhost:8081",
		},
		{
			nome:       "SMTP_PORT/APP_URL definidas: usa os valores explícitos",
			smtpPort:   "2525",
			appURL:     "https://stockflow.empresa.com",
			wantPort:   "2525",
			wantAppURL: "https://stockflow.empresa.com",
		},
	}

	for _, c := range cases {
		t.Run(c.nome, func(t *testing.T) {
			t.Setenv("SMTP_PORT", c.smtpPort)
			t.Setenv("APP_URL", c.appURL)
			t.Setenv("SMTP_HOST", "smtp.empresa.com")
			t.Setenv("SMTP_USER", "usuario")
			t.Setenv("SMTP_PASSWORD", "segredo")
			t.Setenv("SMTP_FROM", "stockflow <noreply@stockflow.local>")

			cfg := CarregarEmailConfig()

			if cfg.Port != c.wantPort {
				t.Errorf("Port = %q, want %q", cfg.Port, c.wantPort)
			}
			if cfg.AppURL != c.wantAppURL {
				t.Errorf("AppURL = %q, want %q", cfg.AppURL, c.wantAppURL)
			}
			if cfg.Host != "smtp.empresa.com" {
				t.Errorf("Host = %q, want %q", cfg.Host, "smtp.empresa.com")
			}
			if cfg.User != "usuario" {
				t.Errorf("User = %q, want %q", cfg.User, "usuario")
			}
			if cfg.Password != "segredo" {
				t.Errorf("Password = %q, want %q", cfg.Password, "segredo")
			}
			if cfg.From != "stockflow <noreply@stockflow.local>" {
				t.Errorf("From = %q, want %q", cfg.From, "stockflow <noreply@stockflow.local>")
			}
		})
	}
}

// TestEnviarSMTP_SemPasswordFalhaImediatamente prova a garantia central do
// AC3/AC4 desta story: com SMTP_PASSWORD vazio, EnviarSMTP falha de forma
// determinística e rápida, sem tentar nenhuma conexão de rede real (o que
// deixaria o teste lento ou dependente de rede/DNS).
func TestEnviarSMTP_SemPasswordFalhaImediatamente(t *testing.T) {
	inicio := time.Now()
	err := EnviarSMTP(testEmailCfg, "alguem@empresa.com", "assunto", "<p>corpo</p>")
	duracao := time.Since(inicio)

	if err == nil {
		t.Fatal("esperava erro com SMTP_PASSWORD vazio")
	}
	if duracao > 100*time.Millisecond {
		t.Errorf("EnviarSMTP demorou %v — esperava falha imediata sem tentativa de rede", duracao)
	}
}

// TestEnvelopeAddress prova que envelopeAddress extrai o endereço nu de um
// SMTP_FROM no formato "Nome <endereco>" (.env.example) para uso no comando
// MAIL FROM — sem isso, o comando vira "MAIL FROM:<Nome <endereco>>", que um
// servidor SMTP real rejeita (bug corrigido nesta passagem de revisão, sem
// cobertura própria até este teste; TestEnviarSMTP_SemPasswordFalhaImediatamente
// nunca alcança esse trecho do código, pois falha antes, no guard de senha
// vazia).
func TestEnvelopeAddress(t *testing.T) {
	casos := []struct {
		nome string
		from string
		want string
	}{
		{"nome de exibicao com endereco entre colchetes", "stockflow <noreply@stockflow.local>", "noreply@stockflow.local"},
		{"apenas endereco, sem nome de exibicao", "noreply@stockflow.local", "noreply@stockflow.local"},
		{"formato invalido retorna valor original", "isto nao e um endereco", "isto nao e um endereco"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := envelopeAddress(c.from); got != c.want {
				t.Errorf("envelopeAddress(%q) = %q, want %q", c.from, got, c.want)
			}
		})
	}
}
