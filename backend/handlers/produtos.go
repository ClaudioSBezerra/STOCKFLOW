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

// Handlers de Produtos e Categorias — cadastro manual com dimensões
// estruturadas (Story 3.1, spec-3-1). Fronteira HTTP pura: decodifica/
// serializa JSON, traduz os erros de services/produtos.go para o envelope de
// erro fixo (AD-14) e nunca contém regra de negócio própria — molde de
// handlers/estoques.go.
//
// Registro em newMux (main.go):
//   - POST /api/produtos -> RequireAuth -> RequireRole(almoxarife); corpo
//     ver criarProdutoRequest. O 403 para papel abaixo de `almoxarife` é
//     decidido inteiramente por RequireRole — este handler só executa quando
//     o papel já passou nesse gate.
//   - GET /api/categorias -> RequireAuth apenas: qualquer conta autenticada
//     lista as 25 categorias fixas (AC4).

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
