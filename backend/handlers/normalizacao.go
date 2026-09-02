// Package handlers, arquivo normalizacao.go: fronteira HTTP da Detecção de
// inconsistências dimensionais (Story 6.1, spec-6-1), da Aplicação
// seletiva de correções (Story 6.2, spec-6-2) e da Detecção de duplicatas
// (Story 6.3, spec-6-3). Molde exato de handlers/movimentacoes.go
// (ListarMovimentacoesHandler): decodifica sessão do contexto, chama o
// service, serializa; nenhuma regra de negócio própria.
//
// Registro em newMux (main.go):
//   - GET /api/normalizacao/inconsistencias -> RequireAuth ->
//     RequireRole(almoxarife) (mesmo gate mínimo de GET /api/movimentacoes):
//     o 401/403 é decidido pelos middlewares; este handler só executa depois
//     de os dois passarem. Rota só-leitura, sem query params — a regra de
//     análise (parsing tolerante, heurística de campo único vazio) vive em
//     services.AnalisarInconsistencias. Resposta: 200 {"sugestoes":[...]}.
//   - POST /api/normalizacao/correcoes -> mesmo gate: aplica um lote de
//     correções (services.AplicarCorrecoes) e publica `{"resource":"produtos",
//     "id","change":"updated"}` uma vez por produtoId distinto tocado.
//   - POST /api/normalizacao/ignoradas -> mesmo gate: grava a tupla exata
//     (produto,campo,valor) ignorada (services.IgnorarSugestao) — sem
//     publicação em tempo real (não altera nenhum Produto).
//   - GET /api/normalizacao/duplicatas -> mesmo gate: varre o catálogo sob
//     demanda e devolve os grupos de Produtos candidatos a duplicata
//     (services.DetectarDuplicatas). Rota só-leitura, sem query params, sem
//     publicação em tempo real (mesmo padrão de GET /inconsistencias).
//   - POST /api/normalizacao/mesclar -> mesmo gate (Story 6.4, spec-6-4):
//     mescla um grupo de duplicatas num único Produto (services.
//     MesclarDuplicatas) e publica os 3 eventos em tempo real do Always
//     (produtos updated/deleted + movimentacoes updated).
package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"stockflow/backend/middleware"
	"stockflow/backend/realtime"
	"stockflow/backend/services"
)

// AnalisarInconsistenciasHandler expõe GET /api/normalizacao/inconsistencias
// (Story 6.1, spec-6-1): varre todos os Produtos sob demanda e devolve a
// lista de sugestões de correção dimensional — leitura pontual, nenhuma
// escrita (aplicar/ignorar é Story 6.2). Guard de contexto ausente -> 500,
// mesmo padrão de ListarMovimentacoesHandler. Erro do service -> 500
// INTERNAL_ERROR.
func AnalisarInconsistenciasHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("AnalisarInconsistenciasHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		sugestoes, err := services.AnalisarInconsistencias(db)
		if err != nil {
			slog.Error("falha ao analisar inconsistências", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao analisar inconsistências")
			return
		}

		escreverJSON(w, http.StatusOK, map[string]any{"sugestoes": sugestoes})
	}
}

// normalizacaoCorrecoesRequestMaxBytes é o teto de corpo aceito por POST
// /api/normalizacao/correcoes — maior que authRequestMaxBytes (64KB,
// dimensionado para payloads de login) porque um "lote geral" (todas as
// sugestões da tela selecionadas de uma vez, potencialmente centenas de
// itens) plausivelmente precisa de mais espaço do que um payload de
// autenticação. POST /api/normalizacao/ignoradas continua com
// authRequestMaxBytes — sempre 1 item, não precisa do teto maior.
const normalizacaoCorrecoesRequestMaxBytes = 1 << 20 // 1MB

// dimensaoValorRequestNormalizacao é o par valor+unidade de fio, molde
// exato de `valorSugerido` em `Sugestao.MarshalJSON` (services/
// normalizacao.go) — o mesmo shape que o cliente recebeu de GET
// /api/normalizacao/inconsistencias e reenvia sem alteração.
type dimensaoValorRequestNormalizacao struct {
	Valor   float64 `json:"valor"`
	Unidade string  `json:"unidade"`
}

// correcaoRequest é um item do corpo de POST /api/normalizacao/correcoes —
// molde exato de `{"produtoId","campo","valorSugerido":{"valor","unidade"}}`
// (Intent Contract de spec-6-2).
type correcaoRequest struct {
	ProdutoID     string                           `json:"produtoId"`
	Campo         string                           `json:"campo"`
	ValorSugerido dimensaoValorRequestNormalizacao `json:"valorSugerido"`
}

// aplicarCorrecoesRequest é o corpo aceito por POST
// /api/normalizacao/correcoes: `{"correcoes":[...]}`, 1..N itens — mesma
// lista cobre aceitar individual (1 item), lote por produto e lote geral (N
// itens); quem decide o agrupamento é o front-end, via seleção de
// checkboxes (Intent Contract de spec-6-2).
type aplicarCorrecoesRequest struct {
	Correcoes []correcaoRequest `json:"correcoes"`
}

// AplicarCorrecoesHandler expõe POST /api/normalizacao/correcoes (Story 6.2,
// spec-6-2): aplica um lote de correções dimensionais — services.
// AplicarCorrecoes valida tudo ANTES de qualquer escrita (campo/valor/
// unidade inválidos ou lista vazia -> 400 VALIDATION_ERROR, nenhuma escrita)
// e devolve só as que realmente afetaram uma linha (guard `IS NULL`, item
// obsoleto não aborta o lote).
//
// `200 {"aplicadas":[{"produtoId","campo"}]}` no sucesso — o front-end
// remove da tabela só as linhas confirmadas aqui. `registro` (AD-2/AD-3):
// publica `{"resource":"produtos","id":<produtoId>,"change":"updated"}` uma
// vez por `produtoId` DISTINTO tocado no lote, não por campo — services.
// AplicarCorrecoes NÃO ganha esse parâmetro, mesma razão de
// CriarProdutoHandler/AtualizarNomeProdutoHandler (produtos.go).
func AplicarCorrecoesHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("AplicarCorrecoesHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, normalizacaoCorrecoesRequestMaxBytes)
		var req aplicarCorrecoesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		correcoes := make([]services.CorrecaoInput, 0, len(req.Correcoes))
		for _, c := range req.Correcoes {
			correcoes = append(correcoes, services.CorrecaoInput{
				ProdutoID: c.ProdutoID,
				Campo:     c.Campo,
				Valor:     c.ValorSugerido.Valor,
				Unidade:   c.ValorSugerido.Unidade,
			})
		}

		aplicadas, err := services.AplicarCorrecoes(db, correcoes)
		var erroValidacao *services.ErroProdutoValidacao
		switch {
		case err == nil:
			// Uma publicação por produtoId DISTINTO — `tocados` deduplica
			// entre itens do mesmo produto (ex. lote por produto com 2+ campos
			// aplicados de uma vez).
			tocados := make(map[string]bool, len(aplicadas))
			for _, a := range aplicadas {
				if tocados[a.ProdutoID] {
					continue
				}
				tocados[a.ProdutoID] = true
				registro.Publish("produtos", realtime.Evento{ID: a.ProdutoID, Change: "updated"})
			}
			escreverJSON(w, http.StatusOK, map[string]any{"aplicadas": aplicadas})
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		default:
			slog.Error("falha ao aplicar correções", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao aplicar correções")
		}
	}
}

// IgnorarSugestaoHandler expõe POST /api/normalizacao/ignoradas (Story 6.2,
// spec-6-2): grava a tupla exata (produtoId,campo,valorSugerido) em
// `normalizacao_ignoradas` via services.IgnorarSugestao — idempotente
// (`ON CONFLICT DO NOTHING`), reenviar a mesma tupla nunca é erro. Sem
// publicação em tempo real: esta ação não altera nenhum Produto, só marca
// uma sugestão como revisada.
//
// `200 {"ignorada":true}` no sucesso; `400 VALIDATION_ERROR` para campo/
// valor/unidade inválidos (mesma validação de AplicarCorrecoesHandler);
// `404 NOT_FOUND` para `produtoId` inexistente ou malformado (não-UUID).
func IgnorarSugestaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("IgnorarSugestaoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		// Corpo é um item só, mas o shape é idêntico a correcaoRequest — reusa
		// o mesmo tipo em vez de redeclarar os 3 campos.
		var req correcaoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		err := services.IgnorarSugestao(db, req.ProdutoID, req.Campo, req.ValorSugerido.Valor, req.ValorSugerido.Unidade)
		var erroValidacao *services.ErroProdutoValidacao
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]any{"ignorada": true})
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		case errors.Is(err, services.ErrProdutoNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "produto não encontrado")
		default:
			slog.Error("falha ao ignorar sugestão", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao ignorar sugestão")
		}
	}
}

// DetectarDuplicatasHandler expõe GET /api/normalizacao/duplicatas (Story
// 6.3, spec-6-3): varre todos os Produtos sob demanda e devolve os grupos de
// Produtos candidatos a duplicata — leitura pontual, nenhuma escrita
// (mesclar é Story 6.4). Guard de contexto ausente -> 500, mesmo padrão de
// AnalisarInconsistenciasHandler. Erro do service -> 500 INTERNAL_ERROR.
func DetectarDuplicatasHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("DetectarDuplicatasHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		grupos, err := services.DetectarDuplicatas(db)
		if err != nil {
			slog.Error("falha ao detectar duplicatas", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao detectar duplicatas")
			return
		}

		escreverJSON(w, http.StatusOK, map[string]any{"grupos": grupos})
	}
}

// mesclarDuplicatasRequest é o corpo aceito por POST
// /api/normalizacao/mesclar (Story 6.4, spec-6-4): o Produto escolhido para
// sobreviver e os ids dos demais membros do grupo, exatamente como exibidos
// na última chamada a GET /api/normalizacao/duplicatas — services.
// MesclarDuplicatas NUNCA confia nesta lista, revalida tudo dentro da
// transação (Always, spec-6-4).
type mesclarDuplicatasRequest struct {
	ProdutoMantidoID   string   `json:"produtoMantidoId"`
	ProdutoRemovidoIDs []string `json:"produtoRemovidoIds"`
}

// MesclarDuplicatasHandler expõe POST /api/normalizacao/mesclar (Story 6.4,
// spec-6-4): mescla um grupo de Produtos duplicados (Story 6.3) num único
// Produto sobrevivente — soma de quantidade, reescrita de Movimentações,
// soft-delete dos removidos e trilha de auditoria permanente, tudo dentro de
// services.MesclarDuplicatas. Molde de AplicarCorrecoesHandler: guarda de
// sessão -> 500; payload inválido -> 400 VALIDATION_ERROR; erro de FORMA da
// requisição (*services.ErroProdutoValidacao) -> 400 VALIDATION_ERROR; grupo
// inválido ou membro já mesclado (*services.ErroMesclagemInvalida) ->
// 409 CONFLICT; qualquer outro erro -> 500 INTERNAL_ERROR.
//
// Sucesso: `200 {"produtoMantidoId","produtosRemovidosIds",
// "quantidadeConsolidada"}` (services.ResultadoMesclagem já carrega essas
// tags de JSON) + os 3 eventos em tempo real do Always (spec-6-4): um
// `{"resource":"produtos","change":"updated"}` para o mantido, um
// `{"resource":"produtos","change":"deleted"}` POR removido, e um ÚNICO
// `{"resource":"movimentacoes","change":"updated"}` (payload mínimo — o
// cliente rebusca via GET, mesmo padrão dos outros canais).
func MesclarDuplicatasHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("MesclarDuplicatasHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req mesclarDuplicatasRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		resultado, err := services.MesclarDuplicatas(db, req.ProdutoMantidoID, req.ProdutoRemovidoIDs, usuario.ID)
		var erroValidacao *services.ErroProdutoValidacao
		var erroInvalida *services.ErroMesclagemInvalida
		switch {
		case err == nil:
			registro.Publish("produtos", realtime.Evento{ID: resultado.ProdutoMantidoID, Change: "updated"})
			for _, removidoID := range resultado.ProdutosRemovidosIDs {
				registro.Publish("produtos", realtime.Evento{ID: removidoID, Change: "deleted"})
			}
			registro.Publish("movimentacoes", realtime.Evento{Change: "updated"})
			escreverJSON(w, http.StatusOK, resultado)
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		case errors.As(err, &erroInvalida):
			escreverErro(w, http.StatusConflict, "CONFLICT", erroInvalida.Motivo)
		default:
			slog.Error("falha ao mesclar duplicatas", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao mesclar duplicatas")
		}
	}
}
