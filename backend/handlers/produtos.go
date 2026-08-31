package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// Handlers de Produtos, Categorias e Nomenclatura Guiada — cadastro manual
// com dimensões estruturadas (Story 3.1, spec-3-1) e nomenclatura guiada por
// subtipo (Story 3.2, spec-3-2). Fronteira HTTP pura: decodifica/serializa
// JSON, traduz os erros de services/produtos.go e services/nomenclatura.go
// para o envelope de erro fixo (AD-14) e nunca contém regra de negócio
// própria — molde de handlers/estoques.go.
//
// Registro em newMux (main.go):
//   - POST /api/produtos -> RequireAuth -> RequireRole(almoxarife); corpo
//     ver criarProdutoRequest. O 403 para papel abaixo de `almoxarife` é
//     decidido inteiramente por RequireRole — este handler só executa quando
//     o papel já passou nesse gate.
//   - GET /api/categorias -> RequireAuth apenas: qualquer conta autenticada
//     lista as 25 categorias fixas (AC4).
//   - GET /api/nomenclatura-templates -> RequireAuth apenas: qualquer conta
//     autenticada lista os 28 templates fixos (Story 3.2).
//   - POST /api/produtos/{id}/renomear -> RequireAuth -> RequireRole
//     (almoxarife); corpo `{"nome": string}` — único endpoint de edição de
//     Produto que existe hoje, escopo restrito a `nome` (Story 3.2, AC3).

// dimensaoRequest é o par valor+unidade de uma dimensão no corpo de
// POST /api/produtos. Os dois ponteiros ausentes (`null`/chave omitida) ->
// dimensão não informada; só um dos dois preenchido é o caso de erro que
// services.CriarProduto rejeita nomeando o campo (AD-9).
type dimensaoRequest struct {
	Valor   *float64 `json:"valor"`
	Unidade *string  `json:"unidade"`
}

func (d *dimensaoRequest) paraInput() *services.DimensaoInput {
	if d == nil {
		return nil
	}
	return &services.DimensaoInput{Valor: d.Valor, Unidade: d.Unidade}
}

// criarProdutoRequest é o corpo aceito por POST /api/produtos. Campos ausentes
// decodificam para o zero value de cada tipo (string vazia, `0`, `nil`) e são
// tratados pela validação de services.CriarProduto — nenhuma validação de
// formato acontece aqui, só decodificação.
type criarProdutoRequest struct {
	Nome              string          `json:"nome"`
	Codigo            string          `json:"codigo"`
	Observacoes       string          `json:"observacoes"`
	CategoriaID       string          `json:"categoria_id"`
	EstoqueID         string          `json:"estoque_id"`
	TemplateID        string          `json:"template_id"`
	QuantidadeInicial float64         `json:"quantidade_inicial"`
	Comprimento       dimensaoRequest `json:"comprimento"`
	Largura           dimensaoRequest `json:"largura"`
	Diametro          dimensaoRequest `json:"diametro"`
	Altura            dimensaoRequest `json:"altura"`
	Espessura         dimensaoRequest `json:"espessura"`
}

// CriarProdutoHandler expõe POST /api/produtos: cadastra um novo Produto e a
// linha inicial de `produto_estoque`. `201 {"produto":{"id","nome"}}` no
// sucesso; `400 VALIDATION_ERROR` com a mensagem específica de campo devolvida
// por services.ErroProdutoValidacao (nome ausente, dimensão incompleta,
// quantidade negativa, categoria/estoque inexistente).
func CriarProdutoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("CriarProdutoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req criarProdutoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		input := services.CriarProdutoInput{
			Nome:              req.Nome,
			Codigo:            req.Codigo,
			Observacoes:       req.Observacoes,
			CategoriaID:       req.CategoriaID,
			EstoqueID:         req.EstoqueID,
			TemplateID:        req.TemplateID,
			QuantidadeInicial: req.QuantidadeInicial,
			Comprimento:       req.Comprimento.paraInput(),
			Largura:           req.Largura.paraInput(),
			Diametro:          req.Diametro.paraInput(),
			Altura:            req.Altura.paraInput(),
			Espessura:         req.Espessura.paraInput(),
		}

		produto, err := services.CriarProduto(db, input)
		var erroValidacao *services.ErroProdutoValidacao
		switch {
		case err == nil:
			escreverJSON(w, http.StatusCreated, map[string]any{"produto": produto})
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		default:
			slog.Error("falha ao criar produto", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao criar produto")
		}
	}
}

// ListarCategoriasHandler expõe GET /api/categorias: devolve
// `200 {"categorias":[{"id","codigo","nome"}, ...]}` ordenadas por código,
// para qualquer conta autenticada. Erro de banco -> 500 INTERNAL_ERROR + slog.
func ListarCategoriasHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("ListarCategoriasHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		categorias, err := services.ListarCategorias(db)
		if err != nil {
			slog.Error("falha ao listar categorias", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar categorias")
			return
		}

		escreverJSON(w, http.StatusOK, map[string]any{"categorias": categorias})
	}
}

// renomearProdutoRequest é o corpo aceito por
// POST /api/produtos/{id}/renomear (Story 3.2, AC3).
type renomearProdutoRequest struct {
	Nome string `json:"nome"`
}

// AtualizarNomeProdutoHandler expõe POST /api/produtos/{id}/renomear: edita
// SÓ o `nome` do Produto de `id` (nomeada como ação, não `PUT`/`PATCH` —
// mesmo padrão de /desativacao, /rebaixamento, /decisao). `200
// {"produto":{"id","nome"}}` no sucesso; `400 VALIDATION_ERROR` quando o novo
// nome falha a validação básica ou não corresponde ao template aplicado ao
// Produto (revalidação da Story 3.2 — não dá para burlar a regra editando
// depois do cadastro); `404 NOT_FOUND` para `id` inexistente ou malformado.
func AtualizarNomeProdutoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("AtualizarNomeProdutoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, authRequestMaxBytes)
		var req renomearProdutoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "payload inválido")
			return
		}

		produto, err := services.AtualizarNomeProduto(db, r.PathValue("id"), req.Nome)
		var erroValidacao *services.ErroProdutoValidacao
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]any{"produto": produto})
		case errors.As(err, &erroValidacao):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroValidacao.Mensagem)
		case errors.Is(err, services.ErrProdutoNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "produto não encontrado")
		default:
			slog.Error("falha ao renomear produto", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao renomear produto")
		}
	}
}

// ListarNomenclaturaTemplatesHandler expõe GET /api/nomenclatura-templates:
// devolve `200 {"templates":[{"id","subtipo","template"}, ...]}` ordenados
// por subtipo, para qualquer conta autenticada — mesmo padrão de
// GET /api/categorias. Erro de banco -> 500 INTERNAL_ERROR + slog.
func ListarNomenclaturaTemplatesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("ListarNomenclaturaTemplatesHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		templates, err := services.ListarNomenclaturaTemplates(db)
		if err != nil {
			slog.Error("falha ao listar templates de nomenclatura", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar templates de nomenclatura")
			return
		}

		escreverJSON(w, http.StatusOK, map[string]any{"templates": templates})
	}
}
