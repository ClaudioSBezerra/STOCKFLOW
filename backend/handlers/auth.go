// Package handlers implementa a fronteira HTTP de autenticação (Story 1.3):
// decodifica/serializa JSON, mapeia erros de `services/` para o envelope de
// erro fixo (AD-14), e nunca contém regra de negócio própria.
package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"stockflow/backend/services"
)

// erroEnvelope é o formato fixo de erro (AD-14): {"error":{"code","message"}}.
type erroEnvelope struct {
	Error erroDetalhe `json:"error"`
}

type erroDetalhe struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func escreverErro(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(erroEnvelope{Error: erroDetalhe{Code: code, Message: message}})
}

func escreverJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// cadastroRequestMaxBytes limita o corpo aceito por POST /api/auth/cadastro
// — rota pública e não autenticada, então json.Decode nunca deve ler um
// corpo arbitrariamente grande antes de rejeitá-lo.
const cadastroRequestMaxBytes = 64 * 1024

// cadastroRequest é o payload aceito por POST /api/auth/cadastro. O campo
// Papel existe só para ser explicitamente ignorado abaixo: nenhum valor
// vindo do formulário jamais decide o papel da conta criada (FR-3) — mesmo
// que o payload envie, por exemplo, `"papel":"adm"`.
type cadastroRequest struct {
	Nome  string `json:"nome"`
	Email string `json:"email"`
	Senha string `json:"senha"`
	Papel string `json:"papel"`
}

// CadastroHandler expõe POST /api/auth/cadastro: cria a conta sempre como
// `usuario`, enfileira o e-mail de verificação e nunca usa `req.Papel`.
func CadastroHandler(db *sql.DB, emailCfg services.EmailConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, cadastroRequestMaxBytes)

		var req cadastroRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		// req.Papel é lido acima apenas para existir no struct de decodificação
		// — deliberadamente nunca repassado a services.Cadastrar, que não tem
		// sequer um parâmetro de papel.
		_, err := services.Cadastrar(db, emailCfg, req.Nome, req.Email, req.Senha)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusCreated, map[string]string{
				"mensagem": "Cadastro realizado. Verifique seu e-mail para confirmar a conta.",
			})
		case errors.Is(err, services.ErrCadastroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "nome, e-mail e senha são obrigatórios")
		case errors.Is(err, services.ErrEmailDuplicado):
			escreverErro(w, http.StatusConflict, "CONFLICT", "Este e-mail já está cadastrado.")
		default:
			slog.Error("falha ao processar cadastro", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar cadastro")
		}
	}
}

// VerificarEmailHandler expõe GET /api/auth/verificar-email?token=...: libera
// `email_verificado` quando o token é válido, ou mapeia o erro correspondente
// da I/O Matrix.
func VerificarEmailHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")

		err := services.VerificarEmail(db, token)
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]string{
				"mensagem": "E-mail verificado com sucesso.",
			})
		case errors.Is(err, services.ErrTokenNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "token de verificação não encontrado")
		case errors.Is(err, services.ErrTokenExpirado):
			escreverErro(w, http.StatusBadRequest, "TOKEN_EXPIRED", "token de verificação expirado ou já utilizado")
		default:
			slog.Error("falha ao verificar e-mail", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao verificar e-mail")
		}
	}
}
