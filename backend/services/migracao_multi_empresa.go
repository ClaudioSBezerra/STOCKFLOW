// Package services, arquivo migracao_multi_empresa.go: a migração ÚNICA e
// operacional do banco da Ferreira Costa para o modelo multi-Empresa —
// Story 9.4 (Epic 9, FR-40/FR-41; AD-15, AD-23), spec-9-4.
//
// Depois das Stories 9.1–9.3 o banco de produção está órfão: `empresa_id`
// existe em 13 tabelas mas é NULL em toda linha já gravada, não há nenhuma
// linha em `empresas`, e como toda query de service filtra por Empresa e todo
// endpoint vive sob `/e/{slug}`, nenhum dado legado aparece para ninguém.
//
// A migração é feita em três etapas idempotentes, disparadas À MÃO por uma
// pessoa via `cmd/migrar-multi-empresa` (AD-15, PRD §9 — nunca por workflow,
// job ou ENTRYPOINT):
//
//  1. AdotarEmpresaFundadora — cria a Empresa real e a Empresa-irmã de
//     Treinamento, numa transação só, reusando o mecanismo da Story 9.2.
//  2. BackfillEmpresa — atribui essa Empresa a TODA linha ainda sem Empresa
//     nas 13 tabelas, em lotes resumíveis, e completa as listas padrão dela.
//  3. EndurecerEmpresaID — só com zero linha órfã, endurece `empresa_id` para
//     NOT NULL sem lock longo (fase 2 de AD-23).
//
// Resumibilidade SEM tabela de checkpoint: o próprio predicado
// `empresa_id IS NULL` é a marca de progresso. Uma execução interrompida
// deixa os lotes já commitados no lugar; a reexecução continua de onde parou;
// depois do fim, atualiza 0 linhas.
//
// Nada aqui apaga, reescreve ou remapeia linha de domínio: a migração é
// ADITIVA sobre `empresa_id`, e só sobre ele. A conta `adm` global vira o
// `adm` da Empresa por ADOÇÃO (o backfill preenche o `empresa_id` da conta
// existente, preservando senha, MFA, id e histórico), nunca por criação.
package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// TabelasComEmpresaID é a lista canônica das 13 tabelas de domínio que
// ganharam `empresa_id` na migration 000032, NA MESMA ORDEM dela. É a fonte
// única de ContarLinhasSemEmpresa, BackfillEmpresa e EndurecerEmpresaID —
// uma tabela esquecida em qualquer um dos três deixaria linha órfã invisível
// (contagem), sem Empresa (backfill) ou nullable para sempre (endurecimento).
//
// As tabelas FILHAS (`produto_estoque`, `carrinho_itens`,
// `importacao_linhas`, `normalizacao_ignoradas`, `sessoes`, `tokens_acao`,
// `emails_pendentes`) não entram de propósito: elas não têm a coluna, porque
// a posse delas é sempre a do pai (migration 000032, cabeçalho).
//
// Os nomes são interpolados em DDL/DML (não dá para parametrizar identificador
// no Postgres) — por isso vivem NESTA constante e nunca vêm de entrada do
// usuário. `tabelaConhecida` é a checagem que garante isso.
var TabelasComEmpresaID = []string{
	"usuarios",
	"produtos",
	"estoques",
	"movimentacoes",
	"pedidos",
	"pedido_itens",
	"categorias",
	"logs_acesso",
	"solicitacoes_promocao",
	"mesclagens_duplicatas",
	"mesclagem_produtos_removidos",
	"importacoes",
	"nomenclatura_templates",
}

// loteBackfillPadrao é o tamanho de lote default do backfill: grande o
// bastante para não pagar um round-trip por linha, pequeno o bastante para
// que cada transação segure locks por pouco tempo (o `api` continua no ar
// durante a migração).
const loteBackfillPadrao = 1000

// ErrOrfasPendentes é o sentinela do endurecimento recusado por ainda existir
// linha sem Empresa. A mensagem concreta (que nomeia tabela e contagem) vem
// embrulhada nele.
var ErrOrfasPendentes = errors.New("ainda existem linhas sem empresa")

// tabelaConhecida confirma que `tabela` é uma das 13 de TabelasComEmpresaID —
// o guard que torna seguro interpolar o nome em SQL.
func tabelaConhecida(tabela string) bool {
	for _, t := range TabelasComEmpresaID {
		if t == tabela {
			return true
		}
	}
	return false
}

// ContarLinhasSemEmpresa devolve, por tabela, quantas linhas ainda estão com
// `empresa_id IS NULL`. Sempre as 13 chaves, inclusive as zeradas — o
// diagnóstico do dry-run mostra a tabela limpa tanto quanto a suja.
func ContarLinhasSemEmpresa(db *sql.DB) (map[string]int, error) {
	contagens := make(map[string]int, len(TabelasComEmpresaID))
	for _, t := range TabelasComEmpresaID {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM ` + t + ` WHERE empresa_id IS NULL`).Scan(&n); err != nil {
			return nil, fmt.Errorf("falha ao contar linhas sem empresa em %s: %w", t, err)
		}
		contagens[t] = n
	}
	return contagens, nil
}

// ColunasJaNotNull devolve, das 13, quais já têm `empresa_id NOT NULL` — o
// outro lado do diagnóstico do dry-run (e o que faz o endurecimento repetido
// ser inócuo).
func ColunasJaNotNull(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`
		SELECT table_name
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND column_name = 'empresa_id'
		  AND is_nullable = 'NO'
		ORDER BY table_name`)
	if err != nil {
		return nil, fmt.Errorf("falha ao consultar colunas já NOT NULL: %w", err)
	}
	defer rows.Close()

	var tabelas []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("falha ao ler coluna já NOT NULL: %w", err)
		}
		if tabelaConhecida(t) {
			tabelas = append(tabelas, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("falha ao iterar colunas já NOT NULL: %w", err)
	}
	return tabelas, nil
}

// BuscarEmpresaPorSlugQualquerStatus resolve a Empresa de `slug`
// independentemente do `status` — ao contrário de BuscarEmpresaPorSlug (que
// só enxerga `ativa`, porque é o middleware de requisição). A etapa `empresa`
// da migração precisa RECONHECER uma Empresa já criada mesmo se alguém a
// tiver desativado, para não tentar recriá-la e colidir no CNPJ.
//
// Slug fora da forma canônica, ou inexistente -> ErrEmpresaNaoEncontrada.
func BuscarEmpresaPorSlugQualquerStatus(db *sql.DB, slug string) (Empresa, error) {
	if !slugCanonico(slug) {
		return Empresa{}, ErrEmpresaNaoEncontrada
	}
	const q = `SELECT ` + colunasEmpresa + ` FROM empresas WHERE slug = $1`
	e, err := scanEmpresa(db.QueryRow(q, slug))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Empresa{}, ErrEmpresaNaoEncontrada
		}
		return Empresa{}, fmt.Errorf("falha ao buscar empresa por slug: %w", err)
	}
	return e, nil
}

// BuscarAdmSemEmpresa lê o nome e o e-mail da conta `adm` GLOBAL (a que
// existe hoje, `empresa_id IS NULL`) — usada pelo CLI para dar nome/e-mail ao
// `adm` do Ambiente de Treinamento quando o operador não os informa. A conta
// em si nunca é alterada aqui: ela é adotada pelo backfill, com senha, MFA e
// id preservados.
//
// Nenhuma conta `adm` sem Empresa -> sql.ErrNoRows (o chamador decide se isso
// é um erro ou só "informe --adm-nome/--adm-email").
func BuscarAdmSemEmpresa(db *sql.DB) (nome, email string, err error) {
	const q = `
		SELECT nome, email
		FROM usuarios
		WHERE papel = 'adm' AND empresa_id IS NULL
		ORDER BY criado_em, id
		LIMIT 1`
	if err := db.QueryRow(q).Scan(&nome, &email); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", sql.ErrNoRows
		}
		return "", "", fmt.Errorf("falha ao buscar o adm sem empresa: %w", err)
	}
	return nome, email, nil
}

// AdotarEmpresaFundadora cria, numa ÚNICA transação, a Empresa REAL da
// Ferreira Costa e a Empresa-irmã "… - Treinamento".
//
// A Empresa real nasce por InserirEmpresa — SEM cópia das listas padrão: ela
// vai ADOTAR as linhas legadas de `categorias`/`nomenclatura_templates` no
// backfill (os Produtos de produção apontam para elas) e só depois completar
// o que faltar via CopiarListasPadrao. O Treinamento nasce por
// ProvisionarTreinamento — o MESMO mecanismo da Story 9.2, com listas, dados
// de exemplo e um `adm` próprio sem senha + e-mail `primeiro_acesso`.
//
// Nenhum `adm` é criado para a Empresa real: a conta `adm` global existente é
// adotada pelo backfill (AC 3).
//
// IDEMPOTENTE: se o slug já existir, resolve a Empresa real e o Treinamento
// já gravados e devolve os dois SEM escrever nada.
//
// Validação (limites da Story 9.2 + validarDadosEmpresa) ANTES de abrir a
// transação -> *ErroEmpresaValidacao. CNPJ de outra Empresa real ->
// ErrCNPJDuplicado. `{slug}-treinamento` em uso por outra Empresa ->
// ErrSlugTreinamentoDuplicado.
func AdotarEmpresaFundadora(db *sql.DB, emailCfg EmailConfig, dados DadosEmpresa, admNome, admEmail string) (Empresa, Empresa, error) {
	dadosReal, admNome, admEmail, err := ValidarDadosNovaEmpresa(NovaEmpresaInput{
		DadosEmpresa: dados,
		AdmNome:      admNome,
		AdmEmail:     admEmail,
	})
	if err != nil {
		return Empresa{}, Empresa{}, err
	}

	// Idempotência: a Empresa real já existe? Então o par já foi criado numa
	// execução anterior — resolve os dois e sai sem escrever.
	if existente, err := BuscarEmpresaPorSlugQualquerStatus(db, dadosReal.Slug); err == nil {
		treino, err := BuscarEmpresaPorSlugQualquerStatus(db, dadosReal.Slug+sufixoSlugTreinamento)
		if err != nil {
			if errors.Is(err, ErrEmpresaNaoEncontrada) {
				return existente, Empresa{}, nil
			}
			return Empresa{}, Empresa{}, err
		}
		return existente, treino, nil
	} else if !errors.Is(err, ErrEmpresaNaoEncontrada) {
		return Empresa{}, Empresa{}, err
	}

	tx, err := db.Begin()
	if err != nil {
		return Empresa{}, Empresa{}, fmt.Errorf("falha ao iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	empresa, err := InserirEmpresa(tx, dadosReal)
	if err != nil {
		return Empresa{}, Empresa{}, err
	}

	treino, err := ProvisionarTreinamento(tx, emailCfg, empresa, dadosReal, admNome, admEmail)
	if err != nil {
		return Empresa{}, Empresa{}, err
	}

	if err := tx.Commit(); err != nil {
		return Empresa{}, Empresa{}, fmt.Errorf("falha ao commitar a adoção da empresa fundadora: %w", err)
	}
	return empresa, treino, nil
}

// BackfillEmpresa atribui `empresaID` a TODA linha ainda sem Empresa nas 13
// tabelas, em lotes de `lote` linhas (<=0 usa o default de 1000), e ao final
// COMPLETA as listas padrão da Empresa (CopiarListasPadrao) — nessa ordem,
// obrigatoriamente: a Empresa primeiro ADOTA as linhas molde de
// `categorias`/`nomenclatura_templates` que os Produtos legados já usam, e só
// então recebe o que ainda falta das tabelas `_padrao`. Inverter duplicaria
// código de Categoria dentro da Empresa e violaria
// `idx_categorias_empresa_codigo`.
//
// Cada lote roda na SUA transação (`UPDATE ... WHERE ctid IN (SELECT ctid ...
// LIMIT n)`), o que torna a operação resumível sem tabela de checkpoint: o
// predicado `empresa_id IS NULL` é a própria marca de progresso. `ctid` (e
// não `id`) porque `pedido_itens` e `mesclagem_produtos_removidos` têm PK
// composta e NENHUMA coluna `id` — `ctid` é o único identificador de linha
// comum às 13.
//
// `progresso`, quando não-nil, é chamado uma vez por tabela com o total
// atribuído nela (inclusive 0), para o CLI relatar tabela a tabela.
//
// Devolve o total atualizado por tabela. Empresa inexistente ou inativa ->
// ErrEmpresaNaoEncontrada, ANTES de qualquer UPDATE.
func BackfillEmpresa(db *sql.DB, empresaID string, lote int, progresso func(tabela string, atualizadas int)) (map[string]int, error) {
	if lote <= 0 {
		lote = loteBackfillPadrao
	}

	var existe bool
	err := db.QueryRow(
		`SELECT true FROM empresas WHERE id::text = $1 AND status = 'ativa'`, empresaID,
	).Scan(&existe)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEmpresaNaoEncontrada
		}
		return nil, fmt.Errorf("falha ao resolver a empresa do backfill: %w", err)
	}

	atualizadas := make(map[string]int, len(TabelasComEmpresaID))
	for _, t := range TabelasComEmpresaID {
		total, err := backfillTabela(db, t, empresaID, lote)
		if err != nil {
			return atualizadas, err
		}
		atualizadas[t] = total
		if progresso != nil {
			progresso(t, total)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return atualizadas, fmt.Errorf("falha ao iniciar transação das listas padrão: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido
	if err := CopiarListasPadrao(tx, empresaID); err != nil {
		return atualizadas, err
	}
	if err := tx.Commit(); err != nil {
		return atualizadas, fmt.Errorf("falha ao commitar as listas padrão da empresa: %w", err)
	}

	return atualizadas, nil
}

// backfillTabela roda o laço de lotes de UMA tabela até nenhum lote atualizar
// linha nenhuma. Cada iteração é uma transação própria: um lote commitado
// nunca é refeito, e uma interrupção no meio não perde o que já passou.
func backfillTabela(db *sql.DB, tabela, empresaID string, lote int) (int, error) {
	if !tabelaConhecida(tabela) {
		return 0, fmt.Errorf("tabela desconhecida no backfill: %q", tabela)
	}
	// `ctid IN (SELECT ctid ... LIMIT $2)`: `ctid` é o endereço físico da
	// linha, estável dentro do statement — serve às 13 tabelas, inclusive as
	// duas sem coluna `id`.
	stmt := `UPDATE ` + tabela + ` SET empresa_id = $1
		WHERE ctid IN (SELECT ctid FROM ` + tabela + ` WHERE empresa_id IS NULL LIMIT $2)`

	total := 0
	for {
		tx, err := db.Begin()
		if err != nil {
			return total, fmt.Errorf("falha ao iniciar transação de lote em %s: %w", tabela, err)
		}
		res, err := tx.Exec(stmt, empresaID, lote)
		if err != nil {
			_ = tx.Rollback()
			return total, fmt.Errorf("falha ao atribuir empresa às linhas de %s: %w", tabela, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			return total, fmt.Errorf("falha ao ler o tamanho do lote de %s: %w", tabela, err)
		}
		if err := tx.Commit(); err != nil {
			return total, fmt.Errorf("falha ao commitar lote de %s: %w", tabela, err)
		}
		if n == 0 {
			return total, nil
		}
		total += int(n)
	}
}

// EndurecerEmpresaID é a fase 2 de AD-23: torna `empresa_id` NOT NULL nas 13
// tabelas. Devolve as tabelas efetivamente endurecidas nesta execução (as que
// já estavam NOT NULL são puladas, o que faz uma segunda execução ser inócua
// e devolver a lista vazia).
//
// A checagem de órfãs roda para TODAS as 13 tabelas ANTES de qualquer DDL:
// uma única linha sem Empresa em qualquer uma delas recusa a operação inteira
// (ErrOrfasPendentes, com tabela e contagem na mensagem) e NENHUM `ALTER
// TABLE` é emitido — endurecer metade das tabelas e falhar na outra metade
// deixaria o schema num estado que ninguém pediu.
//
// Uma tabela por transação, pelo caminho que NÃO segura lock longo
// (endurecerColunaEmpresa).
func EndurecerEmpresaID(db *sql.DB) ([]string, error) {
	contagens, err := ContarLinhasSemEmpresa(db)
	if err != nil {
		return nil, err
	}
	var pendencias []string
	for _, t := range TabelasComEmpresaID {
		if contagens[t] > 0 {
			pendencias = append(pendencias, fmt.Sprintf("%s (%d)", t, contagens[t]))
		}
	}
	if len(pendencias) > 0 {
		return nil, fmt.Errorf(
			"%w — rode a etapa `backfill` antes: %s. Nenhum ALTER TABLE foi emitido",
			ErrOrfasPendentes, strings.Join(pendencias, ", "))
	}

	jaNotNull := make(map[string]bool)
	colunas, err := ColunasJaNotNull(db)
	if err != nil {
		return nil, err
	}
	for _, t := range colunas {
		jaNotNull[t] = true
	}

	endurecidas := make([]string, 0, len(TabelasComEmpresaID))
	for _, t := range TabelasComEmpresaID {
		if jaNotNull[t] {
			continue
		}
		if err := endurecerColunaEmpresa(db, t); err != nil {
			return endurecidas, err
		}
		endurecidas = append(endurecidas, t)
	}
	return endurecidas, nil
}

// endurecerColunaEmpresa torna `<tabela>.empresa_id` NOT NULL sem varredura
// sob lock exclusivo, numa transação só (Design Notes, spec-9-4):
//
//	ADD CONSTRAINT ... CHECK (empresa_id IS NOT NULL) NOT VALID  -- instantâneo
//	VALIDATE CONSTRAINT ...                                      -- SHARE UPDATE EXCLUSIVE
//	ALTER COLUMN empresa_id SET NOT NULL                          -- PG12+: aproveita a constraint validada
//	DROP CONSTRAINT ...                                           -- a CHECK vira redundante
//
// Sem a CHECK validada, o `SET NOT NULL` sozinho varreria a tabela inteira
// segurando ACCESS EXCLUSIVE — em `movimentacoes`/`pedido_itens` de produção
// isso é uma janela de indisponibilidade.
//
// Idempotente: uma coluna já NOT NULL é pulada por EndurecerEmpresaID antes
// de chegar aqui; chamada direta sobre ela reaplica os quatro passos sem
// efeito líquido (a CHECK é criada e derrubada na mesma transação).
//
// Não-exportada e sem guard de allow-list de propósito: o único chamador de
// produção é EndurecerEmpresaID, que só passa nomes de TabelasComEmpresaID —
// `tabela` NUNCA vem de entrada do operador. (O teste da story a chama com uma
// tabela temporária que ele mesmo cria, justamente para não aplicar DDL nas
// tabelas reais do banco compartilhado da suíte.)
func endurecerColunaEmpresa(db *sql.DB, tabela string) error {
	constraint := tabela + "_empresa_id_nao_nula"

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("falha ao iniciar transação de endurecimento de %s: %w", tabela, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido

	for _, stmt := range []string{
		`ALTER TABLE ` + tabela + ` ADD CONSTRAINT ` + constraint + ` CHECK (empresa_id IS NOT NULL) NOT VALID`,
		`ALTER TABLE ` + tabela + ` VALIDATE CONSTRAINT ` + constraint,
		`ALTER TABLE ` + tabela + ` ALTER COLUMN empresa_id SET NOT NULL`,
		`ALTER TABLE ` + tabela + ` DROP CONSTRAINT ` + constraint,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("falha ao endurecer empresa_id em %s (%s): %w", tabela, stmt, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("falha ao commitar endurecimento de %s: %w", tabela, err)
	}
	return nil
}
