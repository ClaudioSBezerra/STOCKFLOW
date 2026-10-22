package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"

	"golang.org/x/crypto/bcrypt"
)

// UsuarioResumo é a projeção somente-leitura de uma conta devolvida por
// GET /api/usuarios (Story 1.5). Sem senha_hash, sem timestamps — só o
// necessário para a listagem de Configurações -> Usuários.
type UsuarioResumo struct {
	ID    string `json:"id"`
	Nome  string `json:"nome"`
	Email string `json:"email"`
	Papel string `json:"papel"`
	Ativo bool   `json:"ativo"`
	// MFAHabilitado (Story 14.4) diz à Gestão de Usuários quando oferecer
	// "Resetar MFA". Só o booleano: o segredo nunca sai do banco.
	MFAHabilitado bool `json:"mfaHabilitado"`
	// MFAObrigatorioIndividual (feedback Ferreira Costa, direção, 2026-10-21):
	// esta conta especificamente é obrigada a configurar MFA, independente do
	// papel e de `empresas.mfa_obrigatorio`. Decidido pelo `adm` no cadastro
	// direto (CriarUsuarioDireto) — ver o gate em middleware.RequireRole.
	MFAObrigatorioIndividual bool `json:"mfaObrigatorioIndividual"`
}

// ErrPapelInvalido indica que CriarUsuarioDireto recebeu um valor de papel
// fora da hierarquia (AD-8) — nenhum dos quatro papéis válidos.
var ErrPapelInvalido = errors.New("papel inválido")

// papelValido confere `papel` contra o mesmo CHECK do Postgres (migration
// 000001) — checado em Go ANTES do INSERT para devolver um 400
// VALIDATION_ERROR limpo, em vez de deixar o banco recusar com um erro bruto.
func papelValido(papel string) bool {
	switch papel {
	case PapelUsuario, PapelAlmoxarife, PapelGestor, PapelAdm:
		return true
	default:
		return false
	}
}

// CriarUsuarioDireto cadastra uma conta na Empresa `empresaID` SEM convite —
// feedback Ferreira Costa (direção, 2026-10-21): "o ADM pode incluir novos
// usuários também... podendo incluir nos níveis que o adm quiser e poder
// usar ou não 2FA". Molde técnico mais próximo: provisionarAdmPrimeiroAcesso
// (empresas_plataforma.go), mas aqui é o `adm` da PRÓPRIA Empresa que digita
// nome/e-mail/senha/papel na hora — simplicidade deliberada, sem link nem
// e-mail de verificação (a conta já nasce com `email_verificado = true`,
// mesmo padrão de cmd/seed-admin: quem cria já está vouching pela conta).
//
// O gate de papel (só `adm`, AD-8) é do middleware (RequireRole), nunca
// deste service — aqui o único papel aceito como PARÂMETRO é o da conta
// NOVA, escolhido livremente pelo chamador dentre os quatro válidos
// (diferente da promoção/rebaixamento, Story 1.7, que só sobe/desce um
// degrau de cada vez a partir do papel ATUAL de uma conta já existente).
func CriarUsuarioDireto(db *sql.DB, empresaID, nome, email, senha, papel string, mfaObrigatorioIndividual bool) (UsuarioResumo, error) {
	nomeTrimado := strings.TrimSpace(nome)
	normalizedEmail := normalizeEmail(email)
	if nomeTrimado == "" || !emailDeConviteValido(normalizedEmail) || strings.TrimSpace(senha) == "" {
		return UsuarioResumo{}, ErrCadastroValidacao
	}
	if utf8.RuneCountInString(nomeTrimado) > 255 {
		return UsuarioResumo{}, ErrCadastroValidacao
	}
	if !papelValido(papel) {
		return UsuarioResumo{}, ErrPapelInvalido
	}
	if err := ValidarForcaSenha(senha); err != nil {
		return UsuarioResumo{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		return UsuarioResumo{}, fmt.Errorf("falha ao gerar hash da senha: %w", err)
	}

	var u UsuarioResumo
	const insert = `
		INSERT INTO usuarios (
			nome, email, senha_hash, papel, email_verificado, ativo, empresa_id, mfa_obrigatorio_individual
		)
		VALUES ($1, $2, $3, $4, true, true, $5, $6)
		RETURNING id, nome, email, papel, ativo, mfa_habilitado, mfa_obrigatorio_individual`
	err = db.QueryRow(insert, nomeTrimado, normalizedEmail, string(hash), papel, empresaID, mfaObrigatorioIndividual).
		Scan(&u.ID, &u.Nome, &u.Email, &u.Papel, &u.Ativo, &u.MFAHabilitado, &u.MFAObrigatorioIndividual)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && (pqErr.Code == pqUniqueViolation ||
			(pqErr.Code == pqExclusionViolation && pqErr.Constraint == restricaoEmailUnicoEntreEmpresasReais)) {
			return UsuarioResumo{}, ErrEmailDuplicado
		}
		return UsuarioResumo{}, fmt.Errorf("falha ao inserir usuário: %w", err)
	}
	return u, nil
}

// ListarUsuarios devolve as contas da Empresa `empresaID` visíveis para quem
// tem o papel `papelSolicitante` (AD-8 forma 3 / AC3): o filtro de escopo é
// aplicado aqui,
// no service, a partir do papel JÁ resolvido pelo contexto da requisição e
// passado como argumento explícito — esta função NUNCA reconsulta `usuarios`
// para descobrir o papel de quem chama.
//
// Escopo (EXPERIENCE.md, Configurações -> Usuários):
//   - `adm`: todas as contas.
//   - qualquer outro papel (na prática só `gestor` chega aqui, atrás de
//     RequireRole("gestor")): apenas contas `usuario`/`almoxarife`.
//
// `empresaID` é a Empresa resolvida do slug da URL pelo middleware (Story
// 9.1, AD-19/AD-20): chega como argumento explícito e recorta a listagem —
// nenhuma conta de outra Empresa aparece, em nenhum papel.
//
// Ordenado por `criado_em`, com `id` como critério de desempate para uma
// ordem determinística quando duas contas compartilham o mesmo `criado_em`.
// Lista vazia não é erro.
func ListarUsuarios(db *sql.DB, empresaID string, papelSolicitante string) ([]UsuarioResumo, error) {
	var rows *sql.Rows
	var err error
	if papelSolicitante == PapelAdm {
		rows, err = db.Query(`
			SELECT id, nome, email, papel, ativo, mfa_habilitado, mfa_obrigatorio_individual
			FROM usuarios
			WHERE empresa_id = $1
			ORDER BY criado_em, id`, empresaID)
	} else {
		// O escopo `gestor` (e qualquer papel abaixo de `adm` que passe pelo
		// RequireRole da rota) enxerga só contas `usuario`/`almoxarife` — os
		// nomes de papel vêm das constantes do pacote, nunca de literais
		// soltos na query.
		rows, err = db.Query(`
			SELECT id, nome, email, papel, ativo, mfa_habilitado, mfa_obrigatorio_individual
			FROM usuarios
			WHERE empresa_id = $1 AND papel IN ($2, $3)
			ORDER BY criado_em, id`, empresaID, PapelUsuario, PapelAlmoxarife)
	}
	if err != nil {
		return nil, fmt.Errorf("falha ao listar usuários: %w", err)
	}
	defer rows.Close()

	usuarios := make([]UsuarioResumo, 0)
	for rows.Next() {
		var u UsuarioResumo
		if err := rows.Scan(&u.ID, &u.Nome, &u.Email, &u.Papel, &u.Ativo, &u.MFAHabilitado, &u.MFAObrigatorioIndividual); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de usuário: %w", err)
		}
		usuarios = append(usuarios, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar usuários: %w", err)
	}
	return usuarios, nil
}
