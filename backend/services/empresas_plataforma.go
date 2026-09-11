// Package services, arquivo empresas_plataforma.go: a gestão de Empresas pelo
// Dono da Plataforma — Story 9.2 (Epic 9, FR-41/FR-43), spec-9-2.
//
// O Dono cria, lista e desativa/reativa CLIENTES, nunca conteúdo: nenhuma
// função deste arquivo lê `produtos`, `estoques`, `movimentacoes`,
// `pedidos`, `logs_acesso` nem as tabelas de normalização. A única exceção é
// a GRAVAÇÃO dos dados de exemplo fixos do Ambiente de Treinamento, dentro da
// transação de criação.
//
// O Ambiente de Treinamento é uma Empresa comum (AD-23): nasce por
// ProvisionarEmpresa, com `empresa_origem_id` apontando para a Empresa real,
// e nenhum service de domínio nem o isolamento sabem que ele existe. O único
// uso desse vínculo fora daqui é o flag de exibição `ambienteTreinamento` da
// resposta de sessão.
package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"
)

const (
	// sufixoSlugTreinamento/sufixoNomeTreinamento derivam o slug e o nome
	// fantasia do Ambiente de Treinamento a partir dos da Empresa real.
	sufixoSlugTreinamento = "-treinamento"
	sufixoNomeTreinamento = " - Treinamento"

	// slugRealMaxRunes/nomeFantasiaRealMaxRunes reservam espaço para os
	// sufixos: o slug do Treinamento precisa caber em VARCHAR(63) e o nome
	// fantasia em VARCHAR(255) (migração 000031). Os sufixos são ASCII, então
	// len() conta caracteres.
	slugRealMaxRunes         = slugMaxRunes - len(sufixoSlugTreinamento) // 51
	nomeFantasiaRealMaxRunes = 255 - len(sufixoNomeTreinamento)          // 241

	// tokenPrimeiroAcessoExpiracao é a validade do link de definição de senha
	// do `adm` provisionado: um convite de primeiro acesso, não uma
	// recuperação (os 30min de redefinição da Story 1.6). Expirado, o
	// "Esqueci minha senha" da tela de login cobre sem ação do Dono.
	tokenPrimeiroAcessoExpiracao = 7 * 24 * time.Hour
)

// ErrSlugTreinamentoDuplicado indica que o slug da Empresa real está livre,
// mas o do Treinamento (`{slug}-treinamento`) já pertence a outra Empresa.
// Embrulha ErrSlugDuplicado: errors.Is com qualquer um dos dois casa.
var ErrSlugTreinamentoDuplicado = fmt.Errorf("o endereço do ambiente de treinamento já está em uso: %w", ErrSlugDuplicado)

// NovaEmpresaInput é o insumo de CriarEmpresaComTreinamento: os dados
// cadastrais da Empresa real (`Slug` opcional — vazio usa
// NormalizarSlug(NomeFantasia)) e o nome/e-mail do primeiro `adm`. O Dono
// nunca informa a senha do `adm`.
type NovaEmpresaInput struct {
	DadosEmpresa
	AdmNome  string
	AdmEmail string
}

// AdmResponsavel é o `adm` mais antigo de uma Empresa, exibido na listagem.
type AdmResponsavel struct {
	Nome  string `json:"nome"`
	Email string `json:"email"`
}

// TreinamentoResumo é o Ambiente de Treinamento de uma Empresa real, aninhado
// a ela na listagem.
type TreinamentoResumo struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Status string `json:"status"`
}

// EmpresaResumo é uma linha da área "Empresas" do Dono: SÓ metadado
// cadastral, status, data de criação, `adm` responsável e o Treinamento.
type EmpresaResumo struct {
	ID           string             `json:"id"`
	NomeFantasia string             `json:"nomeFantasia"`
	RazaoSocial  string             `json:"razaoSocial"`
	CNPJ         string             `json:"cnpj"`
	Endereco     EnderecoEmpresa    `json:"endereco"`
	Slug         string             `json:"slug"`
	Status       string             `json:"status"`
	CriadoEm     time.Time          `json:"criadoEm"`
	Adm          *AdmResponsavel    `json:"adm"`
	Treinamento  *TreinamentoResumo `json:"treinamento"`
}

// produtoExemplo é um Produto dos dados de exemplo do Treinamento.
type produtoExemplo struct {
	nome            string
	categoriaCodigo string
	estoque         string
	quantidade      float64
}

// Dados de exemplo FIXOS do Ambiente de Treinamento — nunca lidos da Empresa
// real. As Categorias são resolvidas por `codigo` na cópia de `categorias`
// DO TREINAMENTO. A "Luva de raspa" nasce com estoque baixo de propósito,
// para exercitar o Pedido que ultrapassa o disponível. Cadastro não gera
// `movimentacoes` (AD-10).
var (
	estoquesExemploTreinamento = []string{"Almoxarifado Central", "Canteiro de Obras"}
	produtosExemploTreinamento = []produtoExemplo{
		{"Cimento CP II 50kg", "04.001", "Almoxarifado Central", 40},
		{"Cabo flexível 2,5mm 100m", "04.002", "Almoxarifado Central", 6},
		{"Capacete de segurança", "05.001", "Canteiro de Obras", 15},
		{"Luva de raspa (par)", "05.001", "Canteiro de Obras", 2},
		{"Trena 5m", "10.001", "Almoxarifado Central", 8},
	}
)

// ValidarDadosNovaEmpresa aplica, ANTES de qualquer escrita, as regras
// próprias da criação de uma Empresa com Ambiente de Treinamento (nome
// fantasia até 241, slug até 51, nome/e-mail do `adm`) e a validação completa
// de ProvisionarEmpresa. Devolve os dados da Empresa real já com nome
// fantasia trimado e slug normalizado.
//
// Exportada na Story 9.4: `cmd/migrar-multi-empresa` valida os dados do
// operador com EXATAMENTE os mesmos limites da criação pela UI (Story 9.2),
// nunca com uma cópia deles.
func ValidarDadosNovaEmpresa(input NovaEmpresaInput) (DadosEmpresa, string, string, error) {
	nomeFantasia, err := campoObrigatorio("nome fantasia", input.NomeFantasia, nomeFantasiaRealMaxRunes)
	if err != nil {
		return DadosEmpresa{}, "", "", err
	}

	slugBase := input.Slug
	if strings.TrimSpace(slugBase) == "" {
		slugBase = nomeFantasia
	}
	slug, err := NormalizarSlug(slugBase)
	if err != nil {
		return DadosEmpresa{}, "", "", err
	}
	if utf8.RuneCountInString(slug) > slugRealMaxRunes {
		return DadosEmpresa{}, "", "", &ErroEmpresaValidacao{
			Mensagem: fmt.Sprintf("endereço de acesso (slug) deve ter no máximo %d caracteres, para caber o sufixo %s", slugRealMaxRunes, sufixoSlugTreinamento),
		}
	}

	admNome, err := campoObrigatorio("nome do administrador", input.AdmNome, 255)
	if err != nil {
		return DadosEmpresa{}, "", "", err
	}
	admEmail := normalizeEmail(input.AdmEmail)
	if !emailPlausivel(admEmail) {
		return DadosEmpresa{}, "", "", &ErroEmpresaValidacao{
			Mensagem: "e-mail do administrador é obrigatório, deve conter @ e ter no máximo 255 caracteres",
		}
	}

	dados := input.DadosEmpresa
	dados.NomeFantasia = nomeFantasia
	dados.Slug = slug
	dados.EmpresaOrigemID = nil
	if _, err := validarDadosEmpresa(dados); err != nil {
		return DadosEmpresa{}, "", "", err
	}
	return dados, admNome, admEmail, nil
}

// CriarEmpresaComTreinamento cria, numa ÚNICA transação: a Empresa real
// (ProvisionarEmpresa), o primeiro `adm` dela, a Empresa-irmã
// "{Nome Fantasia} - Treinamento" (`{slug}-treinamento`, mesmos razão
// social/CNPJ/endereço, `empresa_origem_id` = real), os dados de exemplo fixos
// do Treinamento e o `adm` do Treinamento (conta separada, mesmo nome/e-mail).
// Cada `adm` nasce sem senha e recebe um e-mail `primeiro_acesso` com o link
// de definição de senha sob o slug da própria Empresa. Qualquer falha desfaz
// tudo.
//
// Validação -> *ErroEmpresaValidacao, antes de abrir a transação. CNPJ de
// outra Empresa real -> ErrCNPJDuplicado; slug em uso -> ErrSlugDuplicado;
// `{slug}-treinamento` em uso -> ErrSlugTreinamentoDuplicado.
func CriarEmpresaComTreinamento(db *sql.DB, emailCfg EmailConfig, input NovaEmpresaInput) (Empresa, Empresa, error) {
	dadosReal, admNome, admEmail, err := ValidarDadosNovaEmpresa(input)
	if err != nil {
		return Empresa{}, Empresa{}, err
	}

	tx, err := db.Begin()
	if err != nil {
		return Empresa{}, Empresa{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	empresa, err := ProvisionarEmpresa(tx, dadosReal)
	if err != nil {
		return Empresa{}, Empresa{}, err
	}
	if err := provisionarAdmPrimeiroAcesso(tx, emailCfg, empresa, admNome, admEmail); err != nil {
		return Empresa{}, Empresa{}, err
	}

	treino, err := ProvisionarTreinamento(tx, emailCfg, empresa, dadosReal, admNome, admEmail)
	if err != nil {
		return Empresa{}, Empresa{}, err
	}

	if err := tx.Commit(); err != nil {
		return Empresa{}, Empresa{}, fmt.Errorf("falha ao commitar criação de empresa: %w", err)
	}
	return empresa, treino, nil
}

// ProvisionarTreinamento cria a Empresa-irmã "{Nome Fantasia} - Treinamento"
// de `real`, dentro da transação `tx` do chamador: nome/slug derivados dos da
// Empresa real pelos sufixos, mesmos razão social/CNPJ/endereço,
// `empresa_origem_id` = `real.ID`, as listas padrão (ProvisionarEmpresa), os
// dados de exemplo fixos (semearDadosTreinamento) e o `adm` do Treinamento —
// conta SEPARADA da da Empresa real, mesmo nome/e-mail, sem senha, com e-mail
// `primeiro_acesso` (provisionarAdmPrimeiroAcesso).
//
// Extraída de CriarEmpresaComTreinamento na Story 9.4 para ser reusada por
// services.AdotarEmpresaFundadora (`cmd/migrar-multi-empresa`): a 9.4 depende
// do mecanismo da 9.2, nunca de uma cópia dele.
//
// `{slug}-treinamento` já em uso -> ErrSlugTreinamentoDuplicado (que embrulha
// ErrSlugDuplicado: errors.Is casa com qualquer um dos dois).
func ProvisionarTreinamento(tx *sql.Tx, emailCfg EmailConfig, real Empresa, dadosReal DadosEmpresa, admNome, admEmail string) (Empresa, error) {
	dadosTreino := dadosReal
	dadosTreino.NomeFantasia = dadosReal.NomeFantasia + sufixoNomeTreinamento
	dadosTreino.Slug = dadosReal.Slug + sufixoSlugTreinamento
	dadosTreino.EmpresaOrigemID = &real.ID

	treino, err := ProvisionarEmpresa(tx, dadosTreino)
	if err != nil {
		if errors.Is(err, ErrSlugDuplicado) {
			return Empresa{}, ErrSlugTreinamentoDuplicado
		}
		return Empresa{}, err
	}
	if err := semearDadosTreinamento(tx, treino.ID); err != nil {
		return Empresa{}, err
	}
	if err := provisionarAdmPrimeiroAcesso(tx, emailCfg, treino, admNome, admEmail); err != nil {
		return Empresa{}, err
	}
	return treino, nil
}

// provisionarAdmPrimeiroAcesso cria o `adm` de `empresa` sem senha
// (`senha_hash NULL`, `email_verificado=true` — a posse do e-mail é provada
// ao consumir o link), um token `redefinicao_senha` de 7 dias e o e-mail
// `primeiro_acesso` com o link `/e/{slug}/redefinir-senha?token=...` — o
// mesmo fluxo da Story 1.6, que seta a senha e mantém o e-mail verificado.
func provisionarAdmPrimeiroAcesso(tx *sql.Tx, emailCfg EmailConfig, empresa Empresa, nome, email string) error {
	var usuarioID string
	const insertAdm = `
		INSERT INTO usuarios (nome, email, senha_hash, papel, email_verificado, ativo, empresa_id)
		VALUES ($1, $2, NULL, 'adm', true, true, $3)
		RETURNING id`
	if err := tx.QueryRow(insertAdm, nome, email, empresa.ID).Scan(&usuarioID); err != nil {
		return fmt.Errorf("falha ao criar o administrador da empresa: %w", err)
	}

	token, err := gerarTokenAcao()
	if err != nil {
		return err
	}
	const insertToken = `
		INSERT INTO tokens_acao (usuario_id, token, tipo, expira_em)
		VALUES ($1, $2, 'redefinicao_senha', $3)`
	if _, err := tx.Exec(insertToken, usuarioID, token, time.Now().UTC().Add(tokenPrimeiroAcessoExpiracao)); err != nil {
		return fmt.Errorf("falha ao criar o token de primeiro acesso: %w", err)
	}

	variaveis := map[string]any{
		"nome":    nome,
		"empresa": empresa.NomeFantasia,
		"link":    LinkDaEmpresa(emailCfg.AppURL, empresa.Slug, "/redefinir-senha", token),
	}
	return EnfileirarEmail(tx, email, usuarioID, "primeiro_acesso", variaveis)
}

// semearDadosTreinamento grava os dados de exemplo fixos na Empresa de
// Treinamento `empresaID`, dentro da transação de criação. Toda linha leva o
// `empresa_id` do Treinamento; as Categorias vêm da cópia DELE (por código).
func semearDadosTreinamento(tx *sql.Tx, empresaID string) error {
	estoques := make(map[string]string, len(estoquesExemploTreinamento))
	for _, nome := range estoquesExemploTreinamento {
		var id string
		if err := tx.QueryRow(
			`INSERT INTO estoques (nome, empresa_id) VALUES ($1, $2) RETURNING id`, nome, empresaID,
		).Scan(&id); err != nil {
			return fmt.Errorf("falha ao criar estoque de exemplo %q: %w", nome, err)
		}
		estoques[nome] = id
	}

	const insertProduto = `
		INSERT INTO produtos (nome, categoria_id, empresa_id)
		SELECT $1, c.id, $3 FROM categorias c WHERE c.codigo = $2 AND c.empresa_id = $3
		RETURNING id`
	for _, p := range produtosExemploTreinamento {
		var produtoID string
		if err := tx.QueryRow(insertProduto, p.nome, p.categoriaCodigo, empresaID).Scan(&produtoID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("falha ao criar produto de exemplo %q: categoria %s ausente na empresa de treinamento", p.nome, p.categoriaCodigo)
			}
			return fmt.Errorf("falha ao criar produto de exemplo %q: %w", p.nome, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO produto_estoque (produto_id, estoque_id, quantidade) VALUES ($1, $2, $3)`,
			produtoID, estoques[p.estoque], p.quantidade,
		); err != nil {
			return fmt.Errorf("falha ao definir quantidade do produto de exemplo %q: %w", p.nome, err)
		}
	}
	return nil
}

// ListarEmpresasPlataforma devolve as Empresas REAIS, mais recentes
// primeiro, cada uma com o `adm` mais antigo e o seu Treinamento aninhado —
// só metadado.
func ListarEmpresasPlataforma(db *sql.DB) ([]EmpresaResumo, error) {
	const q = `
		SELECT e.id, e.nome_fantasia, e.razao_social, e.cnpj,
		       e.logradouro, e.numero, e.complemento, e.bairro, e.cidade, e.cep, e.uf,
		       e.slug, e.status, e.criado_em,
		       t.id, t.slug, t.status,
		       a.nome, a.email
		FROM empresas e
		LEFT JOIN empresas t ON t.empresa_origem_id = e.id
		LEFT JOIN LATERAL (
			SELECT u.nome, u.email
			FROM usuarios u
			WHERE u.empresa_id = e.id AND u.papel = 'adm'
			ORDER BY u.criado_em, u.id
			LIMIT 1
		) a ON true
		WHERE e.empresa_origem_id IS NULL
		ORDER BY e.criado_em DESC, e.id`
	rows, err := db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar empresas: %w", err)
	}
	defer rows.Close()

	empresas := make([]EmpresaResumo, 0)
	for rows.Next() {
		var e EmpresaResumo
		var complemento, treinoID, treinoSlug, treinoStatus, admNome, admEmail sql.NullString
		if err := rows.Scan(
			&e.ID, &e.NomeFantasia, &e.RazaoSocial, &e.CNPJ,
			&e.Endereco.Logradouro, &e.Endereco.Numero, &complemento, &e.Endereco.Bairro,
			&e.Endereco.Cidade, &e.Endereco.CEP, &e.Endereco.UF,
			&e.Slug, &e.Status, &e.CriadoEm,
			&treinoID, &treinoSlug, &treinoStatus,
			&admNome, &admEmail,
		); err != nil {
			return nil, fmt.Errorf("falha ao ler empresa da listagem: %w", err)
		}
		e.Endereco.Complemento = complemento.String
		if treinoID.Valid {
			e.Treinamento = &TreinamentoResumo{ID: treinoID.String, Slug: treinoSlug.String, Status: treinoStatus.String}
		}
		if admNome.Valid {
			e.Adm = &AdmResponsavel{Nome: admNome.String, Email: admEmail.String}
		}
		empresas = append(empresas, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar empresas da listagem: %w", err)
	}
	return empresas, nil
}

// AlterarStatusEmpresa desativa ou reativa uma Empresa REAL e o seu
// Treinamento na mesma instrução — o Dono gerencia clientes, não linhas.
// Nenhuma linha é apagada: `status='inativa'` já faz o slug parar de resolver
// (BuscarEmpresaPorSlug), o que derruba login e toda rota sob ele. Ao
// desativar, as `sessoes` ativas das contas das duas Empresas são revogadas
// na mesma transação, para cortar também as sessões em voo.
//
// Id de Treinamento, inexistente ou malformado -> ErrEmpresaNaoEncontrada.
func AlterarStatusEmpresa(db *sql.DB, empresaID, status string) error {
	if status != StatusEmpresaAtiva && status != StatusEmpresaInativa {
		return fmt.Errorf("status de empresa inválido: %q", status)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	const atualizar = `
		UPDATE empresas
		SET status = $2
		WHERE (id = $1 AND empresa_origem_id IS NULL) OR empresa_origem_id = $1
		RETURNING id`
	rows, err := tx.Query(atualizar, empresaID, status)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return ErrEmpresaNaoEncontrada
		}
		return fmt.Errorf("falha ao alterar status da empresa: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("falha ao ler empresa alterada: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqInvalidTextRepresentation {
			return ErrEmpresaNaoEncontrada
		}
		return fmt.Errorf("falha ao alterar status da empresa: %w", err)
	}
	if len(ids) == 0 {
		return ErrEmpresaNaoEncontrada
	}

	if status == StatusEmpresaInativa {
		const revogarSessoes = `
			UPDATE sessoes
			SET revogado_em = now()
			WHERE revogado_em IS NULL
			  AND usuario_id IN (SELECT id FROM usuarios WHERE empresa_id = ANY($1::uuid[]))`
		if _, err := tx.Exec(revogarSessoes, pq.Array(ids)); err != nil {
			return fmt.Errorf("falha ao revogar sessões das contas da empresa: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("falha ao commitar alteração de status da empresa: %w", err)
	}
	return nil
}
