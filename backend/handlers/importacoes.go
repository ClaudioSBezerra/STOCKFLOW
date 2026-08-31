package handlers

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// Handlers de Importação em massa via planilha padronizada (Story 3.3,
// spec-3-3). Fronteira HTTP pura: decodifica o multipart/`xlsx`, valida o
// cabeçalho fixo (services.CabecalhoEsperado) e traduz os erros de
// services/importacoes.go para o envelope de erro fixo (AD-14) — molde de
// handlers/produtos.go/estoques.go.
//
// Registro em newMux (main.go), os 3 atrás de RequireRole(almoxarife):
//   - POST /api/importacoes -> multipart, campo `planilha`.
//   - POST /api/importacoes/{id}/continuar -> sem corpo.
//   - GET /api/importacoes/ultima -> sem corpo.

// importacaoRequestMaxBytes limita o corpo aceito por POST /api/importacoes
// (multipart, arquivo `.xlsx`) — bem acima de authRequestMaxBytes (payloads
// JSON pequenos): uma planilha real de catálogo, mesmo com centenas de
// linhas, cabe folgadamente em 10 MiB.
const importacaoRequestMaxBytes = 10 << 20

// mensagemArquivoInvalido cobre tanto "não é um multipart válido" quanto
// "excelize não conseguiu abrir o arquivo" — do ponto de vista de quem
// enviou, os dois casos são o mesmo problema: o arquivo enviado não é uma
// planilha .xlsx válida.
const mensagemArquivoInvalido = "arquivo não é uma planilha .xlsx válida"

// mensagemArquivoMuitoGrande é usada especificamente quando ParseMultipartForm
// falha por o corpo exceder importacaoRequestMaxBytes (http.MaxBytesReader) —
// um diagnóstico distinto de mensagemArquivoInvalido: um arquivo grande
// demais não é a mesma coisa que um arquivo corrompido/não-xlsx, e confundir
// os dois levaria quem enviou a tentar corrigir o arquivo errado.
const mensagemArquivoMuitoGrande = "arquivo excede o tamanho máximo permitido"

// nomeArquivoMaxRunes é o limite de `importacoes.nome_arquivo VARCHAR(255)`
// (migration 000015) — validado aqui, ANTES de chamar services.CriarImportacao,
// para que um nome de arquivo longo demais vire 400 VALIDATION_ERROR em vez
// de estourar a coluna no INSERT e cair no 500 genérico (erro de banco não
// mapeado).
const nomeArquivoMaxRunes = 255

// mensagemCabecalhoInvalido é a mensagem de 400 VALIDATION_ERROR quando a
// primeira linha da planilha não bate com services.CabecalhoEsperado —
// sempre cita as colunas esperadas, na ordem exata, para quem for corrigir a
// planilha.
var mensagemCabecalhoInvalido = "cabeçalho da planilha inválido — colunas esperadas, nesta ordem: " +
	strings.Join(services.CabecalhoEsperado, ", ")

// cabecalhoValido compara a primeira linha da planilha (`linhas[0]`) contra
// services.CabecalhoEsperado: mesma contagem de colunas, mesmo texto (trim
// nas pontas, SEM normalizar caixa) na mesma ordem exata. `linhas` vazio
// (planilha sem nenhuma linha, nem cabeçalho) -> inválido.
func cabecalhoValido(linhas [][]string) bool {
	if len(linhas) == 0 {
		return false
	}
	cabecalho := linhas[0]
	if len(cabecalho) != len(services.CabecalhoEsperado) {
		return false
	}
	for i, esperado := range services.CabecalhoEsperado {
		if strings.TrimSpace(cabecalho[i]) != esperado {
			return false
		}
	}
	return true
}

// CriarImportacaoHandler expõe POST /api/importacoes: recebe o `.xlsx` no
// campo `planilha`, valida o cabeçalho fixo e, se válido, entrega as linhas
// a services.CriarImportacao — que grava `importacoes`/`importacao_linhas` e
// processa tudo sequencialmente na mesma requisição (sem SSE, sem barra de
// progresso incremental — mesmo precedente das Stories 3.1/3.2).
//
// `201 {"importacao":{"id","status","total_linhas","proxima_linha_pendente"},
// "relatorio":{"criados","rejeitados","linhas_rejeitadas":[{"linha","erro"}]}}`
// no sucesso — mesmo no caso em que toda linha foi rejeitada (a importação em
// si sempre "teve sucesso" em processar o arquivo; o relatório é que
// discrimina). Cabeçalho fora do padrão, arquivo que não abre no excelize,
// campo `planilha` ausente, nome de arquivo maior que 255 caracteres, ou
// corpo acima de `importacaoRequestMaxBytes` (mensagem distinta desse último
// caso — ver mensagemArquivoMuitoGrande) -> `400 VALIDATION_ERROR`, e NENHUMA
// linha é gravada (toda essa validação acontece inteiramente antes de
// qualquer chamada a services).
func CriarImportacaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario, ok := middleware.UsuarioDaSessao(r.Context())
		if !ok {
			slog.Error("CriarImportacaoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, importacaoRequestMaxBytes)
		if err := r.ParseMultipartForm(importacaoRequestMaxBytes); err != nil {
			var erroTamanho *http.MaxBytesError
			if errors.As(err, &erroTamanho) {
				escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", mensagemArquivoMuitoGrande)
				return
			}
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", mensagemArquivoInvalido)
			return
		}

		arquivo, fileHeader, err := r.FormFile("planilha")
		if err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", mensagemArquivoInvalido)
			return
		}
		defer arquivo.Close()

		if utf8.RuneCountInString(fileHeader.Filename) > nomeArquivoMaxRunes {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR",
				"nome do arquivo deve ter no máximo 255 caracteres")
			return
		}

		f, err := excelize.OpenReader(arquivo)
		if err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", mensagemArquivoInvalido)
			return
		}
		defer f.Close()

		planilhas := f.GetSheetList()
		if len(planilhas) == 0 {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", mensagemCabecalhoInvalido)
			return
		}
		linhas, err := f.GetRows(planilhas[0])
		if err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", mensagemArquivoInvalido)
			return
		}

		if !cabecalhoValido(linhas) {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", mensagemCabecalhoInvalido)
			return
		}

		importacao, relatorio, err := services.CriarImportacao(db, usuario.ID, fileHeader.Filename, linhas)
		if err != nil {
			slog.Error("falha ao criar importação", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao processar importação")
			return
		}
		escreverJSON(w, http.StatusCreated, map[string]any{"importacao": importacao, "relatorio": relatorio})
	}
}

// ContinuarImportacaoHandler expõe POST /api/importacoes/{id}/continuar: sem
// corpo, retoma o processamento de uma importação `em_andamento` — só as
// linhas ainda `pendente`/`processando`. `200` com o mesmo formato de
// resposta de CriarImportacaoHandler no sucesso; `404 NOT_FOUND` para `id`
// inexistente ou malformado (não-UUID).
func ContinuarImportacaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("ContinuarImportacaoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		importacao, relatorio, err := services.ContinuarImportacao(db, r.PathValue("id"))
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]any{"importacao": importacao, "relatorio": relatorio})
		case errors.Is(err, services.ErrImportacaoNaoEncontrada):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "importação não encontrada")
		default:
			slog.Error("falha ao continuar importação", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao continuar importação")
		}
	}
}

// UltimaImportacaoHandler expõe GET /api/importacoes/ultima: devolve a
// importação mais recente (qualquer usuário que a tenha criado — escopo
// global, ver services.ObterUltimaImportacao) e o relatório do seu estado
// atual, para a tela de Importação decidir se mostra o banner de retomada.
// Nenhuma importação registrada ainda -> `200 {"importacao": null}` (nunca
// 404 — "nenhuma importação ainda" é um estado válido, não um erro).
func UltimaImportacaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
			slog.Error("UltimaImportacaoHandler chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
			return
		}

		importacao, relatorio, err := services.ObterUltimaImportacao(db)
		if err != nil {
			slog.Error("falha ao buscar última importação", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao buscar última importação")
			return
		}
		if importacao == nil {
			escreverJSON(w, http.StatusOK, map[string]any{"importacao": nil})
			return
		}
		escreverJSON(w, http.StatusOK, map[string]any{"importacao": importacao, "relatorio": relatorio})
	}
}
