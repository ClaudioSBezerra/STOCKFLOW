package services

import (
	"database/sql"
	"fmt"
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
}

// ListarUsuarios devolve as contas visíveis para quem tem o papel
// `papelSolicitante` (AD-8 forma 3 / AC3): o filtro de escopo é aplicado aqui,
// no service, a partir do papel JÁ resolvido pelo contexto da requisição e
// passado como argumento explícito — esta função NUNCA reconsulta `usuarios`
// para descobrir o papel de quem chama.
//
// Escopo (EXPERIENCE.md, Configurações -> Usuários):
//   - `adm`: todas as contas.
//   - qualquer outro papel (na prática só `gestor` chega aqui, atrás de
//     RequireRole("gestor")): apenas contas `usuario`/`almoxarife`.
//
// Ordenado por `criado_em`, com `id` como critério de desempate para uma
// ordem determinística quando duas contas compartilham o mesmo `criado_em`.
// Lista vazia não é erro.
func ListarUsuarios(db *sql.DB, papelSolicitante string) ([]UsuarioResumo, error) {
	var rows *sql.Rows
	var err error
	if papelSolicitante == PapelAdm {
		rows, err = db.Query(`
			SELECT id, nome, email, papel, ativo
			FROM usuarios
			ORDER BY criado_em, id`)
	} else {
		// O escopo `gestor` (e qualquer papel abaixo de `adm` que passe pelo
		// RequireRole da rota) enxerga só contas `usuario`/`almoxarife` — os
		// nomes de papel vêm das constantes do pacote, nunca de literais
		// soltos na query.
		rows, err = db.Query(`
			SELECT id, nome, email, papel, ativo
			FROM usuarios
			WHERE papel IN ($1, $2)
			ORDER BY criado_em, id`, PapelUsuario, PapelAlmoxarife)
	}
	if err != nil {
		return nil, fmt.Errorf("falha ao listar usuários: %w", err)
	}
	defer rows.Close()

	usuarios := make([]UsuarioResumo, 0)
	for rows.Next() {
		var u UsuarioResumo
		if err := rows.Scan(&u.ID, &u.Nome, &u.Email, &u.Papel, &u.Ativo); err != nil {
			return nil, fmt.Errorf("falha ao ler linha de usuário: %w", err)
		}
		usuarios = append(usuarios, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar usuários: %w", err)
	}
	return usuarios, nil
}
