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
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"stockflow/backend/services"
)

// novaEmpresaRequestMaxBytes limita o corpo de POST /api/plataforma/empresas.
const novaEmpresaRequestMaxBytes = 64 * 1024

// novaEmpresaRequest é o payload de POST /api/plataforma/empresas. `slug` é
// opcional (vazio -> derivado do nome fantasia). Não existe campo de senha:
// o `adm` define a própria senha pelo link do e-mail.
//
// `mfa_obrigatorio` (Story 14.2) é opcional: ausente ou null -> false. O nome
// snake_case é o literal do AC; `mfaObrigatorio` (camelCase, padrão do resto
// do payload) é aceito como sinônimo. Os dois com valores diferentes -> 400.
type novaEmpresaRequest struct {
	NomeFantasia string                   `json:"nomeFantasia"`
	RazaoSocial  string                   `json:"razaoSocial"`
	CNPJ         string                   `json:"cnpj"`
	Slug         string                   `json:"slug"`
	Endereco     services.EnderecoEmpresa `json:"endereco"`
	AdmNome      string                   `json:"admNome"`
	AdmEmail     string                   `json:"admEmail"`

	MFAObrigatorio      *bool `json:"mfa_obrigatorio"`
	MFAObrigatorioCamel *bool `json:"mfaObrigatorio"`
}

// resolverMFAObrigatorio combina `mfa_obrigatorio` e o sinônimo
// `mfaObrigatorio`: ausentes -> false; divergentes -> ok=false.
func (req novaEmpresaRequest) resolverMFAObrigatorio() (mfa bool, ok bool) {
	if req.MFAObrigatorio != nil {
		mfa = *req.MFAObrigatorio
	}
	if req.MFAObrigatorioCamel != nil {
		if req.MFAObrigatorio != nil && *req.MFAObrigatorio != *req.MFAObrigatorioCamel {
			return false, false
		}
		mfa = *req.MFAObrigatorioCamel
	}
	return mfa, true
}

// CriarEmpresaHandler expõe POST /api/plataforma/empresas: cria a Empresa
// real, o `adm` dela, o Ambiente de Treinamento (com dados de exemplo) e o
// `adm` do Treinamento numa única transação. 201 `{empresa, treinamento}`;
// validação -> 400 VALIDATION_ERROR com a mensagem do campo; CNPJ ou slug em
// uso -> 409 CONFLICT nomeando qual. `mfa_obrigatorio` (Story 14.2) vai para
// a Empresa real e, por herança na criação, para o Treinamento; conflito com
// o sinônimo `mfaObrigatorio` -> 400 VALIDATION_ERROR.
//
// Depois do commit, grava as fotos de exemplo nos Produtos do Treinamento
// (services.SemearFotosTreinamento, a mesma rotina do CLI da Story 12.4), para
// o Treinamento já nascer com catálogo ilustrado. É melhor-esforço e fica FORA
// da transação (arquivo em disco): uma falha só é registrada em log — a
// Empresa já existe, o 201 não muda e o CLI continua servindo para completar.
func CriarEmpresaHandler(db *sql.DB, emailCfg services.EmailConfig, fotosDir string) http.HandlerFunc {
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
		mfaObrigatorio, ok := req.resolverMFAObrigatorio()
		if !ok {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR",
				"mfa_obrigatorio e mfaObrigatorio foram enviados com valores diferentes")
			return
		}

		empresa, treino, err := services.CriarEmpresaComTreinamento(db, emailCfg, services.NovaEmpresaInput{
			DadosEmpresa: services.DadosEmpresa{
				NomeFantasia:   req.NomeFantasia,
				RazaoSocial:    req.RazaoSocial,
				CNPJ:           req.CNPJ,
				Endereco:       req.Endereco,
				Slug:           req.Slug,
				MFAObrigatorio: mfaObrigatorio,
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
			if res, errFotos := services.SemearFotosTreinamento(db, fotosDir, treino.Slug, true); errFotos != nil {
				slog.Warn("empresa criada, mas as fotos de exemplo do Treinamento não foram gravadas; rode seed-fotos-treinamento",
					"treinamento_slug", treino.Slug, "error", errFotos)
			} else {
				slog.Info("fotos de exemplo gravadas no Treinamento", "treinamento_slug", treino.Slug, "semeadas", res.Semeadas)
			}
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
		case errors.Is(err, services.ErrEmailAdmEmUso):
			escreverErro(w, http.StatusConflict, "CONFLICT", "O e-mail do administrador já está em uso.")
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

// estenderTrialRequestMaxBytes limita o corpo de POST .../trial/extensao.
const estenderTrialRequestMaxBytes = 4 * 1024

// diasEstensaoTrialMax (100 anos) é o teto de `dias` aceito por
// EstenderTrialHandler: sem ele, `time.Duration(dias) * 24 * time.Hour`
// pode estourar o int64 de nanossegundos de time.Duration para um `dias`
// absurdamente grande, envolvendo (wrap) e gravando um `trial_termina_em`
// sem sentido (possivelmente no passado) em vez de recusar com 400.
const diasEstensaoTrialMax = 36500

// estenderTrialRequest é o payload de POST .../trial/extensao: `dias` deve
// ser um inteiro positivo, somado a `time.Now()` (nunca ao prazo antigo).
type estenderTrialRequest struct {
	Dias int `json:"dias"`
}

// EstenderTrialHandler expõe POST /api/plataforma/empresas/{id}/trial/extensao
// (Story 18.3): grava `trial_termina_em = time.Now() + dias` só na linha de
// `{id}` — a mesma rota serve tanto a Empresa real quanto o seu Treinamento,
// sem sincronizar os dois. `dias` ausente, <=0 ou não-inteiro -> 400
// VALIDATION_ERROR, nada é gravado; `{id}` inexistente ou malformado -> 404
// NOT_FOUND.
func EstenderTrialHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dono, ok := donoDaRequisicao(w, r)
		if !ok {
			return
		}
		id := r.PathValue("id")

		r.Body = http.MaxBytesReader(w, r.Body, estenderTrialRequestMaxBytes)
		var req estenderTrialRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}
		if req.Dias <= 0 {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "dias deve ser um número inteiro positivo")
			return
		}
		if req.Dias > diasEstensaoTrialMax {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR",
				fmt.Sprintf("dias deve ser no máximo %d", diasEstensaoTrialMax))
			return
		}

		novoPrazo := time.Now().UTC().Add(time.Duration(req.Dias) * 24 * time.Hour)
		err := services.AtualizarTrialEmpresa(db, id, &novoPrazo)
		switch {
		case err == nil:
			slog.Info("trial estendido pelo dono da plataforma", "dono_id", dono.ID, "empresa_id", id, "dias", req.Dias)
			escreverJSON(w, http.StatusOK, map[string]any{
				"empresa": map[string]any{"id": id, "trialTerminaEm": novoPrazo},
			})
		case errors.Is(err, services.ErrEmpresaNaoEncontrada):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "empresa não encontrada")
		default:
			slog.Error("falha ao estender o prazo de teste da empresa", "dono_id", dono.ID, "empresa_id", id, "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao estender o prazo de teste da empresa")
		}
	}
}

// IsentarTrialHandler expõe POST /api/plataforma/empresas/{id}/trial/isencao
// (Story 18.3): grava `trial_termina_em = NULL` só na linha de `{id}`, sem
// corpo e sem guardar o valor anterior. `{id}` inexistente ou malformado ->
// 404 NOT_FOUND.
func IsentarTrialHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dono, ok := donoDaRequisicao(w, r)
		if !ok {
			return
		}
		id := r.PathValue("id")

		err := services.AtualizarTrialEmpresa(db, id, nil)
		switch {
		case err == nil:
			slog.Info("trial isentado pelo dono da plataforma", "dono_id", dono.ID, "empresa_id", id)
			escreverJSON(w, http.StatusOK, map[string]any{
				"empresa": map[string]any{"id": id, "trialTerminaEm": nil},
			})
		case errors.Is(err, services.ErrEmpresaNaoEncontrada):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "empresa não encontrada")
		default:
			slog.Error("falha ao isentar o prazo de teste da empresa", "dono_id", dono.ID, "empresa_id", id, "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao isentar o prazo de teste da empresa")
		}
	}
}
