package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"stockflow/backend/middleware"
	"stockflow/backend/realtime"
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
//   - GET /api/produtos/busca -> RequireAuth apenas: qualquer conta
//     autenticada busca até 7 Produtos ranqueados por relevância em
//     nome/código/categoria (Story 4.1, spec-4-1).
//   - GET /api/produtos/{id} -> RequireAuth apenas: qualquer conta
//     autenticada abre o detalhe de um Produto com a quantidade
//     discriminada por Estoque (Story 4.4, spec-4-4). CriarProdutoHandler/
//     AtualizarNomeProdutoHandler ganham um `*realtime.Registry` e publicam
//     no canal `produtos` a cada escrita bem-sucedida — o consumidor deste
//     canal é a tela de detalhe, via SSE (handlers/realtime.go).

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
//
// `registro` (Story 4.4, spec-4-4): no sucesso, publica
// `{"resource":"produtos","id":<novo>,"change":"created"}` no canal
// `produtos` — services.CriarProduto NÃO ganha esse parâmetro (ver Design
// Notes da spec: só este handler, o único chamador em produção, muda de
// assinatura).
func CriarProdutoHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc {
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
			registro.Publish("produtos", realtime.Evento{ID: produto.ID, Change: "created"})
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
//
// `registro` (Story 4.4, spec-4-4): no sucesso, publica
// `{"resource":"produtos","id":<id>,"change":"updated"}` no canal
// `produtos` — services.AtualizarNomeProduto NÃO ganha esse parâmetro (mesma
// razão de CriarProdutoHandler acima).
func AtualizarNomeProdutoHandler(db *sql.DB, registro *realtime.Registry) http.HandlerFunc {
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
			registro.Publish("produtos", realtime.Evento{ID: produto.ID, Change: "updated"})
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

// BuscarProdutosHandler expõe GET /api/produtos/busca?q=<termo> (Story 4.1,
// spec-4-1, FR-4): só RequireAuth, qualquer papel (`usuario`+) — sem
// RequireRole, mesmo padrão de GET /api/categorias/GET /api/estoques. `q`
// é lido com `strings.TrimSpace`; vazio (ausente ou só espaços) -> `400
// VALIDATION_ERROR` "termo de busca obrigatório", nenhuma consulta ao banco
// acontece. `q` (trimado) com mais de 255 runes (mesmo teto aplicado a
// `nome`/`codigo` por services.CriarProduto) -> `400 VALIDATION_ERROR`
// "termo de busca muito longo", também sem consulta ao banco. Sucesso: `200
// {"produtos":[...]}`, até 7 itens, `[]` (nunca `null`) quando nenhum
// Produto casa o termo. Erro de banco -> 500 INTERNAL_ERROR + slog.
func BuscarProdutosHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("BuscarProdutosHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		termo := strings.TrimSpace(r.URL.Query().Get("q"))
		if termo == "" {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "termo de busca obrigatório")
			return
		}
		if utf8.RuneCountInString(termo) > 255 {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "termo de busca muito longo")
			return
		}

		produtos, err := services.BuscarProdutos(db, termo)
		if err != nil {
			slog.Error("falha ao buscar produtos", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao buscar produtos")
			return
		}

		escreverJSON(w, http.StatusOK, map[string]any{"produtos": produtos})
	}
}

// ListarCatalogoHandler expõe GET
// /api/produtos/catalogo?agrupar=<>&pagina=<>&q=<>&categoriaId=<>&estoqueId=<>&comEstoque=<>
// (Story 4.3, spec-4-3, FR-6; filtros da Story 4.2, spec-4-2): só
// RequireAuth, qualquer papel (`usuario`+) — sem RequireRole, mesmo padrão
// de GET /api/produtos/busca / GET /api/categorias.
//
// `pagina`: inteiro >=1; ausente/vazio -> `1`; `0`, negativo ou não-numérico
// -> `400 VALIDATION_ERROR` "página inválida", NENHUMA consulta ao banco.
// Página além da última -> `200` com lista vazia (a paginação ainda reporta
// `total`/`totalPaginas`).
//
// `agrupar`: só `true`, `false` ou ausente (-> `false`); qualquer outro
// valor -> `400 VALIDATION_ERROR` "parâmetro agrupar inválido", sem consulta.
//
// `q` (Story 4.2): `strings.TrimSpace`; ausente/vazio -> sem filtro de texto.
// Trimado com mais de 255 runes (mesmo teto de BuscarProdutosHandler) ->
// `400 VALIDATION_ERROR` "termo de busca muito longo", sem consulta.
// `categoriaId`/`estoqueId` (Story 4.2): repassados como estão para
// services.FiltrosCatalogo, sem validação de formato — um valor malformado
// (não-UUID) colapsa em página vazia no service/banco, nunca um erro aqui.
// `comEstoque` (Story 4.2): só `true`, `false` ou ausente (-> sem filtro);
// qualquer outro valor -> `400 VALIDATION_ERROR` "parâmetro comEstoque
// inválido" (mesmo padrão de `agrupar`), sem consulta. Todos os filtros
// combinam por E lógico entre si e com `agrupar`/`pagina`.
//
// Sucesso `agrupar=false`: `200 {"produtos":[...],"paginacao":{...}}` (grade,
// um Produto por linha). Sucesso `agrupar=true`: `200
// {"grupos":[...],"paginacao":{...}}` (Produtos com mesmo nome + dimensões
// colapsados). Erro de banco -> `500 INTERNAL_ERROR` + slog.
func ListarCatalogoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("ListarCatalogoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		pagina := 1
		if bruto := r.URL.Query().Get("pagina"); bruto != "" {
			n, err := strconv.Atoi(bruto)
			if err != nil || n < 1 || n > services.MaxPaginaCatalogo {
				escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "página inválida")
				return
			}
			pagina = n
		}

		var agrupar bool
		switch r.URL.Query().Get("agrupar") {
		case "", "false":
			agrupar = false
		case "true":
			agrupar = true
		default:
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "parâmetro agrupar inválido")
			return
		}

		termo := strings.TrimSpace(r.URL.Query().Get("q"))
		if utf8.RuneCountInString(termo) > 255 {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "termo de busca muito longo")
			return
		}

		filtros := services.FiltrosCatalogo{
			Q:           termo,
			CategoriaID: r.URL.Query().Get("categoriaId"),
			EstoqueID:   r.URL.Query().Get("estoqueId"),
		}
		switch r.URL.Query().Get("comEstoque") {
		case "":
			// ausente -> sem filtro (ComEstoque permanece nil).
		case "true":
			v := true
			filtros.ComEstoque = &v
		case "false":
			v := false
			filtros.ComEstoque = &v
		default:
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "parâmetro comEstoque inválido")
			return
		}

		if agrupar {
			grupos, paginacao, err := services.ListarCatalogoAgrupado(db, pagina, filtros)
			if err != nil {
				slog.Error("falha ao listar catálogo agrupado", "error", err)
				escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar catálogo")
				return
			}
			escreverJSON(w, http.StatusOK, map[string]any{"grupos": grupos, "paginacao": paginacao})
			return
		}

		produtos, paginacao, err := services.ListarCatalogoGrade(db, pagina, filtros)
		if err != nil {
			slog.Error("falha ao listar catálogo", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao listar catálogo")
			return
		}
		escreverJSON(w, http.StatusOK, map[string]any{"produtos": produtos, "paginacao": paginacao})
	}
}

// ObterProdutoHandler expõe GET /api/produtos/{id} (Story 4.4, spec-4-4):
// só RequireAuth, qualquer papel (`usuario`+) — sem RequireRole, mesmo
// padrão de GET /api/produtos/catalogo. `200 {"produto":<ProdutoDetalhe>}`
// no sucesso — mesmos tipos/formatação de CatalogoItem/EstoqueQuantidade
// (Story 4.3), mais `porEstoque` discriminado. `id` inexistente OU
// malformado (não-UUID) -> `404 NOT_FOUND` "produto não encontrado" (mesmo
// colapso de AtualizarNomeProdutoHandler). Erro de banco -> 500
// INTERNAL_ERROR + slog.
func ObterProdutoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("ObterProdutoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		produto, err := services.ObterProdutoDetalhe(db, r.PathValue("id"))
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]any{"produto": produto})
		case errors.Is(err, services.ErrProdutoNaoEncontrado):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "produto não encontrado")
		default:
			slog.Error("falha ao obter detalhe do produto", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao obter produto")
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
