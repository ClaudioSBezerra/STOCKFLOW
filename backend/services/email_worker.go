package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

// maxTentativasEmail é o número de tentativas de envio antes de uma linha de
// `emails_pendentes` ser marcada como terminal (`status='falho'`). Enquanto
// `tentativas < maxTentativasEmail` a linha volta para `status='pendente'` e
// é reprocessada no próximo ciclo de polling.
const maxTentativasEmail = 5

// IntervaloPollingEmail é o intervalo fixo entre ciclos de polling do worker
// — sem backoff, decisão desta story para um volume baixo de e-mails
// (ferramenta interna).
const IntervaloPollingEmail = 10 * time.Second

// IniciarWorkerEmail sobe uma única goroutine que consome `emails_pendentes`
// por polling a cada `intervalo`, resolve o template pelo `tipo` e envia via
// SMTP. Retorna uma função `parar` que encerra a goroutine e só retorna
// depois que ela de fato terminou — segura para ser chamada durante o
// shutdown gracioso já existente em main.go.
func IniciarWorkerEmail(db *sql.DB, cfg EmailConfig, intervalo time.Duration) (parar func()) {
	pararCh := make(chan struct{})
	encerradoCh := make(chan struct{})

	go func() {
		defer close(encerradoCh)
		ticker := time.NewTicker(intervalo)
		defer ticker.Stop()
		for {
			select {
			case <-pararCh:
				return
			case <-ticker.C:
				processarProximoEmailPendente(db, cfg)
			}
		}
	}()

	return func() {
		close(pararCh)
		<-encerradoCh
	}
}

// processarProximoEmailPendente busca no máximo uma linha `pendente` (a mais
// antiga), tenta enviá-la e registra o resultado. Nenhum erro aqui derruba o
// processo — falha de envio (ex. SMTP não configurado) só incrementa
// `tentativas`/`ultimo_erro` na própria linha (AC3 desta story).
func processarProximoEmailPendente(db *sql.DB, cfg EmailConfig) {
	tx, err := db.Begin()
	if err != nil {
		slog.Error("worker de e-mail: falha ao iniciar transação", "error", err)
		return
	}

	var id, usuarioID, destinatario, tipo string
	var variaveisRaw []byte
	var tentativas int
	const selectPendente = `
		SELECT id, usuario_id, destinatario, tipo, variaveis_json, tentativas
		FROM emails_pendentes
		WHERE status = 'pendente'
		ORDER BY criado_em
		LIMIT 1
		FOR UPDATE SKIP LOCKED`
	err = tx.QueryRow(selectPendente).Scan(&id, &usuarioID, &destinatario, &tipo, &variaveisRaw, &tentativas)
	if err != nil {
		_ = tx.Rollback()
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Error("worker de e-mail: falha ao buscar linha pendente", "error", err)
		}
		return
	}

	var variaveis map[string]any
	if err := json.Unmarshal(variaveisRaw, &variaveis); err != nil {
		finalizarComFalha(tx, id, tentativas, "variaveis_json inválido: "+err.Error())
		return
	}

	tpl, err := renderizarTemplate(tipo, variaveis)
	if err != nil {
		finalizarComFalha(tx, id, tentativas, err.Error())
		return
	}

	if err := EnviarSMTP(cfg, destinatario, tpl.Assunto, tpl.CorpoHTML); err != nil {
		finalizarComFalha(tx, id, tentativas, err.Error())
		return
	}

	if _, err := tx.Exec(`UPDATE emails_pendentes SET status = 'enviado', enviado_em = now() WHERE id = $1`, id); err != nil {
		slog.Error("worker de e-mail: falha ao marcar e-mail como enviado", "error", err)
		_ = tx.Rollback()
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("worker de e-mail: falha ao commitar envio", "error", err)
	}
}

// finalizarComFalha incrementa `tentativas`, grava `ultimo_erro` e move a
// linha para `status='falho'` (terminal) assim que `tentativas` alcança
// maxTentativasEmail; enquanto isso, a linha volta para 'pendente' e é
// reprocessada no próximo ciclo.
func finalizarComFalha(tx *sql.Tx, id string, tentativasAtuais int, erro string) {
	novasTentativas := tentativasAtuais + 1
	status := "pendente"
	if novasTentativas >= maxTentativasEmail {
		status = "falho"
	}

	const marcarFalha = `
		UPDATE emails_pendentes
		SET status = $1, tentativas = $2, ultimo_erro = $3
		WHERE id = $4`
	if _, err := tx.Exec(marcarFalha, status, novasTentativas, erro, id); err != nil {
		slog.Error("worker de e-mail: falha ao registrar tentativa", "error", err)
		_ = tx.Rollback()
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("worker de e-mail: falha ao commitar tentativa", "error", err)
	}
}
