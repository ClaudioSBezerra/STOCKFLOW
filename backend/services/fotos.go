package services

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/lib/pq"
)

// Persistência de foto de Produto — Story 3.5 (spec-3-5). Sem tabela nova: o
// nome do arquivo `<produto_id>-<timestamp_unix>.jpg` é o único vínculo com o
// Produto (o glob por prefixo resolve "todas as fotos de um Produto" quando a
// Story 3.6 precisar listar). Decode/resize/recompressão em JPEG q=0.82
// acontecem em handlers/fotos.go (fronteira HTTP) — este arquivo só recebe os
// bytes JPEG já prontos e resolve on-disk existência/nome/escrita.

// fotoMaxTentativasColisao é o teto de tentativas de gerar um nome de arquivo
// sem colisão incrementando o timestamp em 1s a cada tentativa (spec-3-5) —
// na prática inatingível (exigiria 1000 uploads para o MESMO Produto no MESMO
// segundo civil), mantido como defesa contra um laço infinito caso o disco
// esteja num estado inesperado.
const fotoMaxTentativasColisao = 1000

// FotoProduto é a projeção devolvida por SalvarFotoProduto: `Nome` é o nome
// de arquivo gravado em `fotosDir`; `URL` é o caminho absoluto de
// GET /api/produtos/{id}/fotos/{nome} que serve o arquivo.
type FotoProduto struct {
	Nome string `json:"nome"`
	URL  string `json:"url"`
}

// SalvarFotoProduto grava `jpegBytes` (já decodificado, redimensionado e
// recomprimido pelo chamador) em `fotosDir`, com nome versionado
// `<produtoID>-<timestamp_unix>.jpg`. Verifica a existência do Produto ANTES
// de tocar o disco — `produtoID` inexistente OU malformado (não-UUID, `pq`
// SQLSTATE 22P02) -> ErrProdutoNaoEncontrado, mesmo tratamento de
// AtualizarNomeProduto (services/produtos.go).
//
// A escrita usa `os.O_CREATE|os.O_EXCL`: nunca sobrescreve um arquivo
// existente. Uma colisão de nome (dois uploads no mesmo segundo civil para o
// mesmo Produto) incrementa o timestamp em 1s e tenta de novo, até
// fotoMaxTentativasColisao tentativas antes de devolver erro de
// infraestrutura.
func SalvarFotoProduto(db *sql.DB, fotosDir string, produtoID string, jpegBytes []byte) (FotoProduto, error) {
	var existe bool
	err := db.QueryRow(`SELECT true FROM produtos WHERE id = $1`, produtoID).Scan(&existe)
	if err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return FotoProduto{}, ErrProdutoNaoEncontrado
		}
		return FotoProduto{}, fmt.Errorf("falha ao verificar produto para foto: %w", err)
	}

	timestampBase := time.Now().Unix()
	for tentativa := 0; tentativa < fotoMaxTentativasColisao; tentativa++ {
		nome := fmt.Sprintf("%s-%d.jpg", produtoID, timestampBase+int64(tentativa))
		caminho := filepath.Join(fotosDir, nome)

		arquivo, err := os.OpenFile(caminho, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return FotoProduto{}, fmt.Errorf("falha ao criar arquivo de foto: %w", err)
		}

		_, errEscrita := arquivo.Write(jpegBytes)
		errFechar := arquivo.Close()
		if errEscrita != nil {
			return FotoProduto{}, fmt.Errorf("falha ao escrever arquivo de foto: %w", errEscrita)
		}
		if errFechar != nil {
			return FotoProduto{}, fmt.Errorf("falha ao fechar arquivo de foto: %w", errFechar)
		}

		return FotoProduto{
			Nome: nome,
			URL:  fmt.Sprintf("/api/produtos/%s/fotos/%s", produtoID, nome),
		}, nil
	}

	return FotoProduto{}, fmt.Errorf(
		"falha ao gerar nome de arquivo sem colisão para produto %s após %d tentativas",
		produtoID, fotoMaxTentativasColisao,
	)
}

// ListarFotosProduto lista todas as fotos de um Produto — Story 3.6
// (spec-3-6). Reaproveita a MESMA checagem de existência de Produto de
// SalvarFotoProduto (existência OU malformação de `produtoID` ->
// ErrProdutoNaoEncontrado, ANTES de tocar o disco), depois resolve todos os
// arquivos gravados por SalvarFotoProduto via `filepath.Glob` pelo prefixo
// `<produtoID>-` (o único vínculo Produto↔foto, sem tabela nova).
//
// Devolve sempre uma slice não-nil, ordenada por `Nome` — como o timestamp
// unix do nome do arquivo tem largura fixa (10 dígitos até o ano 2286) e o
// prefixo `<produtoID>-` é constante por Produto, ordenar a STRING do nome é
// equivalente a ordenar pelo timestamp numérico (== ordem de envio), sem
// parse extra. Produto sem nenhuma foto -> slice vazia, nunca erro.
func ListarFotosProduto(db *sql.DB, fotosDir string, produtoID string) ([]FotoProduto, error) {
	var existe bool
	err := db.QueryRow(`SELECT true FROM produtos WHERE id = $1`, produtoID).Scan(&existe)
	if err != nil {
		var pqErr *pq.Error
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation) {
			return nil, ErrProdutoNaoEncontrado
		}
		return nil, fmt.Errorf("falha ao verificar produto para listar fotos: %w", err)
	}

	padrao := filepath.Join(fotosDir, fmt.Sprintf("%s-*.jpg", produtoID))
	caminhos, err := filepath.Glob(padrao)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar fotos do produto: %w", err)
	}

	fotos := make([]FotoProduto, 0, len(caminhos))
	for _, caminho := range caminhos {
		nome := filepath.Base(caminho)
		fotos = append(fotos, FotoProduto{
			Nome: nome,
			URL:  fmt.Sprintf("/api/produtos/%s/fotos/%s", produtoID, nome),
		})
	}
	sort.Slice(fotos, func(i, j int) bool { return fotos[i].Nome < fotos[j].Nome })

	return fotos, nil
}
