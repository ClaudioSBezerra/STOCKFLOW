package services

import (
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"time"
)

// timeoutSMTP limita tanto a conexão TCP quanto toda a troca SMTP
// subsequente (STARTTLS/AUTH/MAIL/RCPT/DATA). Sem isso, um host que aceita a
// conexão TCP mas trava no meio do handshake bloquearia a única goroutine do
// worker indefinidamente — e, por consequência, `parar()` (chamado no
// shutdown gracioso de main.go), que só verifica o canal de parada entre
// ciclos de polling.
const timeoutSMTP = 10 * time.Second

// EmailConfig é a configuração de SMTP lida de variáveis de ambiente — mesmos
// nomes de env var usados em `/home/claudio/projetos/FB_APU02/backend/services/email.go`
// (lido como referência read-only): SMTP_HOST/SMTP_PORT/SMTP_USER/SMTP_PASSWORD/SMTP_FROM.
// APP_URL é usado para montar o link de verificação colado no e-mail.
type EmailConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string
	AppURL   string
}

// CarregarEmailConfig lê a configuração de SMTP do ambiente. Nenhum valor
// aqui é obrigatório para o processo subir: com SMTP_PASSWORD vazio (padrão
// em ambiente local/CI) o envio real simplesmente falha de forma
// determinística em EnviarSMTP, sem impedir o resto da aplicação de
// funcionar (AC3 desta story).
func CarregarEmailConfig() EmailConfig {
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "http://localhost:8081"
	}
	return EmailConfig{
		Host:     os.Getenv("SMTP_HOST"),
		Port:     port,
		User:     os.Getenv("SMTP_USER"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     os.Getenv("SMTP_FROM"),
		AppURL:   appURL,
	}
}

// EnfileirarEmail grava uma linha em `emails_pendentes` dentro da mesma
// transação do chamador (AD-4) — o produtor nunca grava HTML pré-renderizado,
// só o `tipo` e as variáveis brutas que o worker vai usar para resolver o
// template no momento do envio.
func EnfileirarEmail(tx *sql.Tx, destinatario, usuarioID, tipo string, variaveis map[string]any) error {
	payload, err := json.Marshal(variaveis)
	if err != nil {
		return fmt.Errorf("falha ao serializar variaveis_json: %w", err)
	}

	const insertEmailPendente = `
		INSERT INTO emails_pendentes (usuario_id, destinatario, tipo, variaveis_json)
		VALUES ($1, $2, $3, $4)`
	if _, err := tx.Exec(insertEmailPendente, usuarioID, destinatario, tipo, payload); err != nil {
		return fmt.Errorf("falha ao enfileirar e-mail: %w", err)
	}
	return nil
}

// templateRenderizado é o resultado de renderizarTemplate: assunto e corpo já
// prontos para serem enviados via SMTP.
type templateRenderizado struct {
	Assunto   string
	CorpoHTML string
}

// renderizarTemplate resolve o template pelo `tipo` no momento do envio —
// nunca no momento em que o produtor enfileira o e-mail (AD-4). 'verificacao_conta'
// (Story 1.3) e 'redefinicao_senha' (Story 1.6) têm template; qualquer outro
// tipo retorna erro, tratado pelo worker como falha de envio dessa linha.
func renderizarTemplate(tipo string, variaveis map[string]any) (templateRenderizado, error) {
	switch tipo {
	case "verificacao_conta":
		nome, _ := variaveis["nome"].(string)
		link, _ := variaveis["link"].(string)
		corpo := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: Arial, sans-serif; color: #333333;">
	<p>Olá, %s.</p>
	<p>Confirme seu cadastro no stockflow clicando no link abaixo:</p>
	<p><a href="%s">Confirmar meu e-mail</a></p>
	<p>Ou copie e cole no navegador: %s</p>
	<p style="font-size: 12px; color: #999999;">Este link expira em 24 horas.</p>
</body>
</html>`, html.EscapeString(nome), link, link)
		return templateRenderizado{
			Assunto:   "Confirme seu e-mail — stockflow",
			CorpoHTML: corpo,
		}, nil
	case "redefinicao_senha":
		nome, _ := variaveis["nome"].(string)
		link, _ := variaveis["link"].(string)
		corpo := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: Arial, sans-serif; color: #333333;">
	<p>Olá, %s.</p>
	<p>Recebemos um pedido para redefinir a sua senha no stockflow. Clique no link abaixo para escolher uma nova senha:</p>
	<p><a href="%s">Redefinir minha senha</a></p>
	<p>Ou copie e cole no navegador: %s</p>
	<p>Se você não fez esse pedido, ignore este e-mail — sua senha continua a mesma.</p>
	<p style="font-size: 12px; color: #999999;">Este link expira em 30 minutos.</p>
</body>
</html>`, html.EscapeString(nome), link, link)
		return templateRenderizado{
			Assunto:   "Redefinição de senha — stockflow",
			CorpoHTML: corpo,
		}, nil
	default:
		return templateRenderizado{}, fmt.Errorf("tipo de e-mail desconhecido: %q", tipo)
	}
}

// envelopeAddress extrai o endereço nu de SMTP_FROM para o comando MAIL FROM.
// `SMTP_FROM` (.env.example) documenta o formato "Nome <endereco>" para uso
// no header `From:` (RFC 5322, onde nome de exibição é válido), mas o
// comando MAIL FROM do protocolo SMTP (RFC 5321) espera só o endereço —
// passar a string inteira produziria "MAIL FROM:<Nome <endereco>>", que
// servidores reais rejeitam. Se `from` não estiver no formato "Nome <e>" (ex.
// já é um endereço nu), retorna o valor original sem alterações.
func envelopeAddress(from string) string {
	addr, err := mail.ParseAddress(from)
	if err != nil {
		return from
	}
	return addr.Address
}

// EnviarSMTP envia um e-mail via net/smtp usando a configuração informada.
// Com Password vazio (ambiente local/CI, mesmo comportamento do FB_APU02) o
// envio falha de forma determinística e imediata, sem tentar nenhuma conexão
// de rede — os testes do worker nunca dependem de infraestrutura externa.
//
// Ao contrário de um smtp.SendMail() direto (sem timeout algum), a conexão
// TCP usa net.DialTimeout e a troca SMTP inteira (STARTTLS/AUTH/MAIL/RCPT/
// DATA) roda sob um único conn.SetDeadline — um host que aceita a conexão e
// trava no meio do handshake falha após timeoutSMTP em vez de travar a
// goroutine do worker para sempre.
func EnviarSMTP(cfg EmailConfig, destinatario, assunto, corpoHTML string) error {
	if cfg.Password == "" {
		return fmt.Errorf("SMTP_PASSWORD não configurado — envio de e-mail desabilitado neste ambiente")
	}

	addr := net.JoinHostPort(cfg.Host, cfg.Port)

	conn, err := net.DialTimeout("tcp", addr, timeoutSMTP)
	if err != nil {
		return fmt.Errorf("falha ao conectar ao servidor SMTP: %w", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(timeoutSMTP)); err != nil {
		return fmt.Errorf("falha ao configurar timeout da conexão SMTP: %w", err)
	}

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("falha ao iniciar cliente SMTP: %w", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
			return fmt.Errorf("falha ao negociar STARTTLS: %w", err)
		}
	}

	if ok, _ := client.Extension("AUTH"); ok {
		auth := smtp.PlainAuth("", cfg.User, cfg.Password, cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("falha na autenticação SMTP: %w", err)
		}
	}

	if err := client.Mail(envelopeAddress(cfg.From)); err != nil {
		return fmt.Errorf("falha no comando MAIL FROM: %w", err)
	}
	if err := client.Rcpt(destinatario); err != nil {
		return fmt.Errorf("falha no comando RCPT TO: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("falha no comando DATA: %w", err)
	}
	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		cfg.From, destinatario, assunto, corpoHTML,
	)
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("falha ao escrever corpo do e-mail: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("falha ao finalizar corpo do e-mail: %w", err)
	}

	return client.Quit()
}
