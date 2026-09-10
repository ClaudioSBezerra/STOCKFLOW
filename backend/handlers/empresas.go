// Package handlers, arquivo empresas.go: a área "Empresas" do Dono da
// Plataforma — Story 9.2 (Epic 9, FR-41/FR-43), spec-9-2.
//
// Todas as rotas ficam atrás de middleware.RequireDonoPlataforma, em
// `/api/plataforma/empresas...`. Nenhuma resposta traz dado operacional de
// uma Empresa, nem a senha ou o token de primeiro acesso do `adm`.
package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"stockflow/backend/services"
)

// novaEmpresaRequestMaxBytes limita o corpo de POST /api/plataforma/empresas.
const novaEmpresaRequestMaxBytes = 64 * 1024

// novaEmpresaRequest é o payload de POST /api/plataforma/empresas. `slug` é
// opcional (vazio -> derivado do nome fantasia). Não existe campo de senha:
// o `adm` define a própria senha pelo link do e-mail.
type novaEmpresaRequest struct {
	NomeFantasia string                   `json:"nomeFantasia"`
	RazaoSocial  string                   `json:"razaoSocial"`
	CNPJ         string                   `json:"cnpj"`
	Slug         string                   `json:"slug"`
	Endereco     services.EnderecoEmpresa `json:"endereco"`
	AdmNome      string                   `json:"admNome"`
	AdmEmail     string                   `json:"admEmail"`
}

// CriarEmpresaHandler expõe POST /api/plataforma/empresas: cria a Empresa
// real, o `adm` dela, o Ambiente de Treinamento (com dados de exemplo) e o
// `adm` do Treinamento numa única transação. 201 `{empresa, treinamento}`;
// validação -> 400 VALIDATION_ERROR com a mensagem do campo; CNPJ ou slug em
// uso -> 409 CONFLICT nomeando qual.
func CriarEmpresaHandler(db *sql.DB, emailCfg services.EmailConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dono, ok := donoDaRequisicao(w, r)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, novaEmpresaRequestMaxBytes)
		var req novaEmpresaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		empresa, treino, err := services.CriarEmpresaComTreinamento(db, emailCfg, services.NovaEmpresaInput{
			DadosEmpresa: services.DadosEmpresa{
				NomeFantasia: req.NomeFantasia,
				RazaoSocial:  req.RazaoSocial,
				CNPJ:         req.CNPJ,
				Endereco:     req.Endereco,
				Slug:         req.Slug,
			},
			AdmNome:  req.AdmNome,
			AdmEmail: req.AdmEmail,
		})
		var erroValidacao *services.ErroEmpresaValidacao
		switch {
		case err == nil:
			slog.Info("empresa criada pelo dono da plataforma",
				"dono_id", dono.ID, "empresa_id", empresa.ID, "slug", empresa.Slug,
				"treinamento_id", treino.ID, "treinamento_slug", treino.Slug)
			escreverJSON(w, http.StatusCreated, map[string]any{
				"empresa":     empresa,
				"treinamento": treino,
			})
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		case errors.Is(err, services.ErrCNPJDuplicado):
			escreverErro(w, http.StatusConflict, "CONFLICT", "Já existe uma empresa com este CNPJ.")
		case errors.Is(err, services.ErrSlugTreinamentoDuplicado):
			escreverErro(w, http.StatusConflict, "CONFLICT", "O endereço de acesso do Ambiente de Treinamento ({endereço}-treinamento) já está em uso. Escolha outro endereço de acesso.")
		case errors.Is(err, services.ErrSlugDuplicado):
			escreverErro(w, http.StatusConflict, "CONFLICT", "Já existe uma empresa com este endereço de acesso.")
		default:
			slog.Error("falha ao criar empresa", "dono_id", dono.ID, "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao criar empresa")
		}
	}
}

// ListarEmpresasHandler expõe GET /api/plataforma/empresas: 200
// `{empresas: [...]}` só com metadado, cada Empresa real com o seu
// Treinamento aninhado.
func ListarEmpresasHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := donoDaRequisicao(w, r); !ok {
			return
		}
		empresas, err := services.ListarEmpresasPlataforma(db)
		if err != nil {
			slog.Error("falha ao listar empresas", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar empresas")
			return
		}
		escreverJSON(w, http.StatusOK, map[string]any{"empresas": empresas})
	}
}

// DesativarEmpresaHandler expõe POST /api/plataforma/empresas/{id}/desativacao.
func DesativarEmpresaHandler(db *sql.DB) http.HandlerFunc {
	return alterarStatusEmpresaHandler(db, services.StatusEmpresaInativa, "empresa desativada pelo dono da plataforma")
}

// ReativarEmpresaHandler expõe POST /api/plataforma/empresas/{id}/reativacao.
func ReativarEmpresaHandler(db *sql.DB) http.HandlerFunc {
	return alterarStatusEmpresaHandler(db, services.StatusEmpresaAtiva, "empresa reativada pelo dono da plataforma")
}

// alterarStatusEmpresaHandler aplica `status` à Empresa real `{id}` e ao seu
// Treinamento. 200 `{empresa:{id,status}}`; id de Treinamento, inexistente ou
// malformado -> 404 NOT_FOUND.
func alterarStatusEmpresaHandler(db *sql.DB, status, mensagemAuditoria string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dono, ok := donoDaRequisicao(w, r)
		if !ok {
			return
		}
		id := r.PathValue("id")

		err := services.AlterarStatusEmpresa(db, id, status)
		switch {
		case err == nil:
			slog.Info(mensagemAuditoria, "dono_id", dono.ID, "empresa_id", id)
			escreverJSON(w, http.StatusOK, map[string]any{
				"empresa": map[string]string{"id": id, "status": status},
			})
		case errors.Is(err, services.ErrEmpresaNaoEncontrada):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "empresa não encontrada")
		default:
			slog.Error("falha ao alterar status da empresa", "dono_id", dono.ID, "empresa_id", id, "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao alterar status da empresa")
		}
	}
}
