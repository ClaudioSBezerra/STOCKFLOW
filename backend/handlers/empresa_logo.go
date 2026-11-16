package handlers

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

// Handlers da logo da Empresa (spec-logo-da-empresa). Os três ficam atrás de
// RequireAuth + RequireRole(gestor) no roteamento (main.go); `empresa_id`
// vem sempre do contexto, nunca do cliente.
//
//   - GET    /api/empresa/logo -> 200 com os bytes + Content-Type; 404 sem logo
//     (Cache-Control: private, no-cache + ETag)
//   - PUT    /api/empresa/logo -> multipart, campo `logo`; 200 {"atualizadoEm"}
//     400 VALIDATION_ERROR (formato, tamanho, dimensões)
//   - DELETE /api/empresa/logo -> 204 (idempotente)

// logoRequestMaxBytes: o arquivo pode ter até LogoEmpresaMaxBytes; o resto é
// folga para os cabeçalhos multipart. Corpo acima disso é recusado já na leitura.
const logoRequestMaxBytes = services.LogoEmpresaMaxBytes + 64*1024

func sessaoEEmpresaDaLogo(w http.ResponseWriter, r *http.Request, nome string) (services.Empresa, bool) {
	if _, ok := middleware.UsuarioDaSessao(r.Context()); !ok {
		slog.Error(nome + " chamado sem UsuarioSessao no contexto — RequireAuth não foi aplicado")
		escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao resolver usuário")
		return services.Empresa{}, false
	}
	return empresaDaRequisicao(w, r)
}

// etagCorresponde implementa a comparação fraca de If-None-Match (RFC 9110
// 13.1.2): lista separada por vírgula, validador fraco `W/` ignorado, `*`
// casa com qualquer representação existente.
func etagCorresponde(ifNoneMatch, etag string) bool {
	for _, candidato := range strings.Split(ifNoneMatch, ",") {
		candidato = strings.TrimSpace(candidato)
		if candidato == "*" {
			return true
		}
		if strings.TrimPrefix(candidato, "W/") == strings.TrimPrefix(etag, "W/") && candidato != "" {
			return true
		}
	}
	return false
}

// BuscarLogoEmpresaHandler expõe GET /api/empresa/logo.
func BuscarLogoEmpresaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		empresa, ok := sessaoEEmpresaDaLogo(w, r, "BuscarLogoEmpresaHandler")
		if !ok {
			return
		}
		logo, err := services.BuscarLogoEmpresa(db, empresa.ID)
		switch {
		case errors.Is(err, services.ErrLogoEmpresaNaoEncontrada):
			escreverErro(w, http.StatusNotFound, "NOT_FOUND", "a empresa não tem logo cadastrada")
			return
		case err != nil:
			slog.Error("falha ao buscar logo da empresa", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao buscar logo da empresa")
			return
		}
		etag := fmt.Sprintf(`"%x"`, sha256.Sum256(logo.Conteudo))
		w.Header().Set("Content-Type", logo.TipoConteudo)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("ETag", etag)
		if etagCorresponde(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(logo.Conteudo)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(logo.Conteudo)
	}
}

// EnviarLogoEmpresaHandler expõe PUT /api/empresa/logo.
func EnviarLogoEmpresaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		empresa, ok := sessaoEEmpresaDaLogo(w, r, "EnviarLogoEmpresaHandler")
		if !ok {
			return
		}
		if tipo, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || tipo != "multipart/form-data" {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "Envie o arquivo no campo 'logo' (multipart/form-data).")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, logoRequestMaxBytes)
		if err := r.ParseMultipartForm(logoRequestMaxBytes); err != nil {
			var tamanho *http.MaxBytesError
			if errors.As(err, &tamanho) {
				escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "A logo é grande demais: o limite é 512 KB.")
				return
			}
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "Formato inválido: use PNG ou JPEG.")
			return
		}
		arquivo, _, err := r.FormFile("logo")
		if err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "Formato inválido: use PNG ou JPEG.")
			return
		}
		defer arquivo.Close()
		dados, err := io.ReadAll(io.LimitReader(arquivo, services.LogoEmpresaMaxBytes+1))
		if err != nil {
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", "Formato inválido: use PNG ou JPEG.")
			return
		}
		logo, err := services.SalvarLogoEmpresa(db, empresa.ID, dados)
		var erroLogo *services.ErroLogoEmpresa
		switch {
		case err == nil:
			escreverJSON(w, http.StatusOK, map[string]any{"atualizadoEm": logo.AtualizadoEm})
		case errors.As(err, &erroLogo):
			escreverErro(w, http.StatusBadRequest, "VALIDATION_ERROR", erroLogo.Mensagem)
		default:
			slog.Error("falha ao salvar logo da empresa", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao salvar logo da empresa")
		}
	}
}

// RemoverLogoEmpresaHandler expõe DELETE /api/empresa/logo.
func RemoverLogoEmpresaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		empresa, ok := sessaoEEmpresaDaLogo(w, r, "RemoverLogoEmpresaHandler")
		if !ok {
			return
		}
		if err := services.RemoverLogoEmpresa(db, empresa.ID); err != nil {
			slog.Error("falha ao remover logo da empresa", "error", err)
			escreverErro(w, http.StatusInternalServerError, "INTERNAL_ERROR", "falha ao remover logo da empresa")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
