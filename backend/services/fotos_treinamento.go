package services

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
)

// Fotos de exemplo do Ambiente de Treinamento — Story 12.4 (Epic 12, FR-51;
// AD-15), spec-12-4.
//
// Os Produtos semeados pela Story 9.2 (produtosExemploTreinamento) nascem sem
// foto. Este service, chamado SÓ pelo binário `cmd/seed-fotos-treinamento`
// (disparado à mão, UM Treinamento por vez), grava uma foto de exemplo por
// Produto com SalvarFotoProduto — o mesmo armazenamento versionado da Story
// 3.5. Não há tabela nova, reseed automático nem toque no provisionamento
// (semearDadosTreinamento segue transacional e sem disco).
//
// As fotos são ilustrações geradas (rótulo "EXEMPLO"), JPEG ≤500px q≈82 — já
// no formato final, dispensam o decode/resize do handler. Trocar por fotos
// reais é substituir o arquivo de mesmo nome em `fotos_treinamento/`.

//go:embed fotos_treinamento/*.jpg
var fotosTreinamentoFS embed.FS

// fotosExemploTreinamento mapeia o nome de cada Produto de exemplo ao arquivo
// embarcado. Toda entrada de produtosExemploTreinamento precisa estar aqui
// (garantido por teste).
var fotosExemploTreinamento = map[string]string{
	"Cimento CP II 50kg":       "fotos_treinamento/cimento.jpg",
	"Cabo flexível 2,5mm 100m": "fotos_treinamento/cabo.jpg",
	"Capacete de segurança":    "fotos_treinamento/capacete.jpg",
	"Luva de raspa (par)":      "fotos_treinamento/luva.jpg",
	"Trena 5m":                 "fotos_treinamento/trena.jpg",
}

// ErrEmpresaNaoTreinamento recusa SemearFotosTreinamento numa Empresa real
// (sem `empresa_origem_id`): a guarda vive só neste service de seed.
var ErrEmpresaNaoTreinamento = errors.New("a empresa informada não é um Ambiente de Treinamento")

// ErrTreinamentoSemProdutosExemplo recusa SemearFotosTreinamento quando NENHUM
// Produto de exemplo existe na Empresa (Treinamento nunca semeado ou com todos
// os Produtos renomeados): sair "com sucesso" sem gravar nada enganaria o
// operador.
var ErrTreinamentoSemProdutosExemplo = errors.New("nenhum Produto de exemplo encontrado no Ambiente de Treinamento")

// ResultadoFotosTreinamento é o relatório de SemearFotosTreinamento.
type ResultadoFotosTreinamento struct {
	// Executado é false em dry-run (nada foi gravado em disco).
	Executado bool
	// Semeadas conta as fotos gravadas (em dry-run, as que seriam gravadas).
	Semeadas int
	// JaComFoto conta os Produtos pulados por já terem alguma foto.
	JaComFoto int
	// Ausentes lista os Produtos de exemplo não encontrados na Empresa.
	Ausentes []string
}

// SemearFotosTreinamento grava a foto de exemplo de cada Produto de exemplo
// do Treinamento `slug`. Empresa real -> ErrEmpresaNaoTreinamento; slug
// desconhecido/inativo -> ErrEmpresaNaoEncontrada; nada é escrito nesses
// casos. Produto que já tem foto é pulado (preserva foto real do Adm) e
// Produto ausente só é reportado. Idempotente. Com `executar` false roda em
// dry-run: mesmas contagens, nada em disco.
func SemearFotosTreinamento(db *sql.DB, fotosDir, slug string, executar bool) (ResultadoFotosTreinamento, error) {
	res := ResultadoFotosTreinamento{Executado: executar}

	empresa, err := BuscarEmpresaPorSlug(db, slug)
	if err != nil {
		return res, err
	}
	if empresa.EmpresaOrigemID == nil {
		return res, ErrEmpresaNaoTreinamento
	}

	// Lê todas as fotos embarcadas ANTES de qualquer escrita.
	fotos := make(map[string][]byte, len(produtosExemploTreinamento))
	for _, p := range produtosExemploTreinamento {
		arquivo, ok := fotosExemploTreinamento[p.nome]
		if !ok {
			return res, fmt.Errorf("produto de exemplo %q sem foto embarcada", p.nome)
		}
		dados, err := fotosTreinamentoFS.ReadFile(arquivo)
		if err != nil {
			return res, fmt.Errorf("falha ao ler foto embarcada de %q: %w", p.nome, err)
		}
		fotos[p.nome] = dados
	}

	if executar {
		if err := os.MkdirAll(fotosDir, 0o755); err != nil {
			return res, fmt.Errorf("falha ao criar diretório de fotos: %w", err)
		}
	}

	for _, p := range produtosExemploTreinamento {
		var produtoID string
		err := db.QueryRow(
			`SELECT id FROM produtos WHERE empresa_id = $1 AND nome = $2 AND deleted_at IS NULL ORDER BY id LIMIT 1`,
			empresa.ID, p.nome,
		).Scan(&produtoID)
		if errors.Is(err, sql.ErrNoRows) {
			res.Ausentes = append(res.Ausentes, p.nome)
			continue
		}
		if err != nil {
			return res, fmt.Errorf("falha ao buscar produto de exemplo %q: %w", p.nome, err)
		}

		existentes, err := ListarFotosProduto(db, empresa.ID, fotosDir, produtoID)
		if err != nil {
			return res, err
		}
		if len(existentes) > 0 {
			res.JaComFoto++
			continue
		}

		if executar {
			if _, err := SalvarFotoProduto(db, empresa.ID, fotosDir, produtoID, fotos[p.nome]); err != nil {
				return res, fmt.Errorf("falha ao gravar foto de %q: %w", p.nome, err)
			}
		}
		res.Semeadas++
	}
	if len(res.Ausentes) == len(produtosExemploTreinamento) {
		return res, ErrTreinamentoSemProdutosExemplo
	}
	return res, nil
}
