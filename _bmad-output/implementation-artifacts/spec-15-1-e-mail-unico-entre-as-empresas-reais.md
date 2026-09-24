---
title: 'Story 15.1: E-mail único entre as Empresas reais'
type: 'feature'
created: '2026-09-24'
status: 'done'
baseline_revision: 'dfb77f77eb7a9bce9f29030129aa2816ecce61bc'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-15-context.md'
warnings: [oversized]
deferred:
  - summary: >-
      O caminho de conta órfã do CLI migrar-multi-empresa (usuarios em TabelasComEmpresaID e BuscarAdmSemEmpresa) ficou inalcançável depois da migration 000049.
    evidence: |-
      usuarios.empresa_raiz_id NOT NULL, preenchida a partir de empresa_id, impede conta com empresa_id NULL; o backfill de usuarios sempre encontra 0 linhas e BuscarAdmSemEmpresa sempre devolve sql.ErrNoRows. A migração da 9.4 já rodou em produção, então o código é só legado morto.
    location: >-
      backend/services/migracao_multi_empresa.go
    severity: low
  - summary: >-
      Teste de handler de produtos descarta o erro de um QueryRow de contagem.
    evidence: |-
      Apontado durante a revisão da 15.1 (padrão `_ = db.QueryRow(...).Scan(...)`); já existia antes desta story.
    location: >-
      backend/handlers/produtos_test.go:2254
    severity: low
---

<intent-contract>

## Intent

**Problem:** Hoje o e-mail é único só dentro de cada Empresa (`idx_usuarios_email_lower (empresa_id, lower(email))`), então a mesma pessoa pode ter conta em duas Empresas reais e o e-mail sozinho não identifica a Empresa. Isso impede o login pela raiz (Stories 15.2/15.3).

**Approach:** Nova migration `000049` cria `usuarios.empresa_raiz_id` (a Empresa real da conta), preenchida por trigger `BEFORE INSERT`, com backfill, `NOT NULL` e a restrição `EXCLUDE USING gist (lower(email) WITH =, empresa_raiz_id WITH <>)` (AD-36). Os três pontos de criação de conta (convite, cadastro, primeiro `adm` de Empresa nova) passam a responder `409` quando a restrição (ou a checagem prévia do convite) recusa o e-mail.

## Boundaries & Constraints

**Always:**
- `empresa_raiz_id = COALESCE(empresas.empresa_origem_id, empresas.id)` da Empresa da conta, calculada SEMPRE pelo trigger (sobrescreve qualquer valor informado no INSERT). Nenhum código Go de inserção passa a informar a coluna.
- A migration é transacional: pré-checagem que faz `RAISE EXCEPTION` listando os e-mails repetidos entre Empresas raiz diferentes ANTES de criar a restrição; também `RAISE EXCEPTION` claro se existir conta com `empresa_id IS NULL` (o `SET NOT NULL` não pode falhar com erro genérico). Nada fica pela metade.
- O índice `idx_usuarios_email_lower` e `idx_usuarios_unico_adm` continuam como estão.
- Mensagens de 409 reaproveitam as de hoje: convite → `ErrConviteEmailJaCadastrado` ("Este e-mail já tem conta nesta empresa."); cadastro → `ErrEmailDuplicado` ("Este e-mail já está cadastrado."). Nunca revelar em qual Empresa o e-mail existe.
- Criação de Empresa com e-mail de `adm` em uso: 409 "O e-mail do administrador já está em uso." e nem a Empresa nem o Treinamento ficam gravados (a transação de `CriarEmpresaComTreinamento` já garante; só mapear o erro).
- Treinamento repete e-mail da SUA Empresa real livremente (convite, cadastro e `adm` do Treinamento no provisionamento).
- Testes de schema e de fluxo que hoje inserem `usuarios` sem `empresa_id` passam a usar a Empresa de teste do pacote; testes que criavam o mesmo e-mail em duas Empresas reais passam a usar e-mails distintos ou a provar a recusa. Nunca afrouxar a restrição para acomodar teste.

**Block If:**
- A restrição de exclusão ou `CREATE EXTENSION btree_gist` falhar por permissão no banco de testes (contradiz o verificado em AD-36).

**Never:**
- Coluna booleana de Treinamento em `usuarios` ou `if treinamento` em service (AD-23).
- Trigger de `UPDATE` (conta nunca troca de Empresa).
- Rotas da raiz, login pela conta, `EMPRESA_PADRAO` (Stories 15.2–15.4).
- Qualquer mudança de frontend (a tela da Plataforma e a de convites já exibem a mensagem de 409 do servidor).
- `git push` (todo push em `main` faz deploy em produção).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Migration em banco limpo | sem e-mail repetido entre raízes | coluna criada, preenchida, `NOT NULL`, restrição `usuarios_email_unico_entre_empresas_reais` ativa | — |
| Migration com duplicata | `a@x.com` na Empresa A e na Empresa B (reais) | migration falha; mensagem contém `a@x.com`; schema intacto | `RAISE EXCEPTION` antes da restrição |
| Convite para e-mail de outra real | conta `a@x.com` na Empresa B; gestor de A convida `A@X.com` | 409 CONFLICT, nenhum convite gravado | `ErrConviteEmailJaCadastrado` |
| Convite para e-mail do Treinamento de outra real | conta em `b-treinamento`; convite em A | 409 | idem |
| Convite no Treinamento para conta da real | conta `a@x.com` em A; convite em `a-treinamento` | 201; cadastro depois cria a conta de Treinamento | — |
| Cadastro com convite antigo | convite pendente em A; a conta em B nasceu depois | 409, convite continua pendente, nada gravado | 23P01 → `ErrEmailDuplicado` |
| Criar Empresa com adm em uso | `adm_email` já existe na Empresa B | 409, nenhuma linha em `empresas` para o slug nem para `{slug}-treinamento` | 23P01 → `ErrEmailAdmEmUso` |
| Criar Empresa nova | adm real e adm do Treinamento com o mesmo e-mail | 201, as duas contas com a mesma `empresa_raiz_id` | — |
| INSERT direto (CLI/seed) | `INSERT` sem `empresa_raiz_id` | trigger preenche | — |

</intent-contract>

## Code Map

- `backend/migrations/000048_create_auditoria_seguranca.up.sql` -- última migration; a nova é `000049_add_empresa_raiz_id_to_usuarios.{up,down}.sql`. Estilo: cabeçalho de comentário PT-BR citando story/FR/AD.
- `backend/migrations/000032_add_empresa_id_dominio.up.sql` -- origem de `idx_usuarios_email_lower` / `idx_usuarios_unico_adm` (NULLS NOT DISTINCT). `usuarios.empresa_id` é NULLABLE no schema de migrations (o `SET NOT NULL` da 9.4 foi feito pelo CLI `cmd/migrar-multi-empresa`, não por migration) — por isso a checagem explícita de órfãs na 000049.
- `backend/main.go:131,276-291` -- migrations embutidas (`migrationsFS`) e `m.Up()` no boot; um teste pode ler `migrations/000049_*.sql` do `migrationsFS` e executá-los dentro de uma `tx` para provar a falha com duplicata (DDL do Postgres é transacional; `ROLLBACK` no fim).
- `backend/services/auth.go:30-32,76-78,265-279` -- `pqUniqueViolation`, `ErrEmailDuplicado`, INSERT de `Cadastrar`; mapear também `pqErr.Code == "23P01"` (nova const `pqExclusionViolation`) para `ErrEmailDuplicado`. Atualizar o comentário de `Cadastrar` (linhas ~163-168) que diz que o mesmo e-mail pode existir em outra Empresa.
- `backend/services/convites.go:58-68,172-194` -- `ErrConviteEmailJaCadastrado` e a checagem prévia de `EmitirConvite`: trocar por "existe conta com este e-mail nesta Empresa OU com `empresa_raiz_id` diferente da raiz desta Empresa" (raiz via `SELECT COALESCE(empresa_origem_id, id) FROM empresas WHERE id = $1`). Atualizar comentários.
- `backend/services/empresas_plataforma.go:40-52,186-189,265-275` -- sentinelas de erro e `provisionarAdmPrimeiroAcesso`: mapear 23P01 para novo `ErrEmailAdmEmUso`; documentar no comentário de `CriarEmpresaComTreinamento`.
- `backend/handlers/empresas.go:106-116` -- switch de erros da criação de Empresa: novo `case errors.Is(err, services.ErrEmailAdmEmUso)` → 409 CONFLICT "O e-mail do administrador já está em uso."
- `backend/handlers/auth.go:120`, `backend/handlers/convites.go:67` -- já mapeiam os sentinelas para 409; sem mudança além de conferir.
- `backend/cmd/seed-admin/main.go:157` -- INSERT sem mudança (o trigger cobre).
- Testes que inserem `usuarios` sem `empresa_id` (quebram com o `NOT NULL`): `backend/main_test.go:272,290-293,313-316,867,2315`, `backend/middleware/auth_test.go:91`, `backend/middleware/roles_test.go:354,389`, `backend/services/migracao_multi_empresa_test.go:219,390` (conta órfã e `TestBuscarAdmSemEmpresa`: órfã passa a ser impossível — provar que o banco recusa, ou remover a parte de `usuarios` mantendo as demais tabelas). Helpers de Empresa de teste: `backend/empresa_teste_test.go` (`garantirEmpresaTeste`, `empresaTeste`) e equivalentes em `services`/`handlers`/`middleware`.
- Banco de testes compartilhado: `DATABASE_URL=postgres://stockflow:stockflow@127.0.0.1:5432/stockflow_t91?sslmode=disable`, `go test -p 1`. Nunca `TRUNCATE empresas`; testes de schema que mexem em estrutura devem restaurar a forma VIGENTE.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/000049_add_empresa_raiz_id_to_usuarios.up.sql` -- `CREATE EXTENSION IF NOT EXISTS btree_gist`; `ADD COLUMN empresa_raiz_id UUID NULL REFERENCES empresas(id)`; bloco `DO` que falha se houver `empresa_id IS NULL`; `UPDATE` de backfill via `empresas`; bloco `DO` que agrega (`string_agg`) os `lower(email)` presentes em mais de uma raiz e `RAISE EXCEPTION` com a lista; `SET NOT NULL`; função plpgsql + trigger `BEFORE INSERT`; `ADD CONSTRAINT usuarios_email_unico_entre_empresas_reais EXCLUDE USING gist (lower(email) WITH =, empresa_raiz_id WITH <>)`; índice em `empresa_raiz_id` se a FK precisar -- AD-36.
- `backend/migrations/000049_add_empresa_raiz_id_to_usuarios.down.sql` -- remove restrição, trigger, função e coluna (a extensão fica; comentar por quê) -- reversível.
- `backend/services/auth.go` -- const `pqExclusionViolation = "23P01"`; `Cadastrar` mapeia 23505 e 23P01 para `ErrEmailDuplicado`; comentários -- 409 do cadastro.
- `backend/services/convites.go` -- checagem prévia de `EmitirConvite` por Empresa raiz -- 409 na emissão, sem convite gravado.
- `backend/services/empresas_plataforma.go` + `backend/handlers/empresas.go` -- `ErrEmailAdmEmUso` e mapeamento 409 -- criação de Empresa.
- Testes de schema (`backend/main_test.go`): coluna `NOT NULL`, trigger preenche real/Treinamento, restrição recusa mesmo e-mail (caixa diferente) em duas reais, aceita real+Treinamento; migration `000049` executada em `tx` sobre um estado com duplicata falha com o e-mail na mensagem (desfazer: `down` → inserir duplicata → `up` → esperar erro → `ROLLBACK`).
- Testes de service/handler (`backend/services/convites_test.go`, `auth_test.go`, `empresas_plataforma_test.go`, `backend/handlers/convites_test.go`, `plataforma_test.go`): cada linha da I/O Matrix.
- Ajustar os testes existentes listados no Code Map para o `NOT NULL` e a nova unicidade.

**Acceptance Criteria:**
- Given a suíte Go completa, when roda com o banco de testes na versão 49, then fica verde sem nenhum teste pulado por causa desta story.
- Given o `adm` real e o `adm` do Treinamento criados por `CriarEmpresaComTreinamento`, when lidos do banco, then as duas contas têm `empresa_raiz_id` = id da Empresa real.
- Given o login e todos os fluxos sob `/e/{slug}`, when esta story entra, then nada muda no comportamento deles.

## Review Triage Log

### 2026-09-24 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 5 (high 0, medium 1, low 4)
- defer: 2 (high 0, medium 0, low 2)
- reject: 22 (high 0, medium 4, low 18)
- addressed_findings:
  - `[medium]` `[patch]` `seed-admin` sem `--empresa-slug` passava pela validação e morria num 23502 cru do banco; agora `validateFlags` exige o slug antes de qualquer hash/DB, o ramo `empresa_id NULL` foi removido e os comentários falsos corrigidos.
  - `[low]` `[patch]` `seed-admin` não mapeava 23P01; novo `errEmailEmUso` com mensagem clara e teste; o ramo `else` do deploy (`.github/workflows/deploy-cliente-aws.yml`) que rodava o seed sem slug (falha engolida por `|| true`) virou um `echo` de "pulando".
  - `[low]` `[patch]` `Cadastrar` e `provisionarAdmPrimeiroAcesso` passam a mapear 23P01 só para a restrição `usuarios_email_unico_entre_empresas_reais` (nome numa const única).
  - `[low]` `[patch]` `EmitirConvite` compara a raiz com `IS DISTINCT FROM`, para um `empresaID` desconhecido nunca pular a checagem entre Empresas.
  - `[low]` `[patch]` `handlers/convites_test.go` deixou de descartar erros das contagens.

### 2026-09-24 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 3 (high 0, medium 0, low 3)
- defer: 0
- reject: 22 (high 0, medium 3, low 19)
- addressed_findings:
  - `[low]` `[patch]` Pré-checagem da `000049` que aborta deixa `schema_migrations` em 49 `dirty` e a API não sobe; o cabeçalho da migration agora documenta a recuperação (`migrate force 48` / `UPDATE schema_migrations`).
  - `[low]` `[patch]` Deploy sem `ADMIN_EMPRESA_SLUG` pulava o seed só com `echo`, e o `installer/cliente-aws/.env.template` nem trazia a chave; agora o workflow emite `::warning::` e o template documenta `ADMIN_EMPRESA_SLUG` como obrigatório.
  - `[low]` `[patch]` `handlers/convites_test.go` e `handlers/plataforma_test.go` usavam o mesmo CNPJ (`91510000001240`); resto de um teste faria o outro falhar por CNPJ em vez de e-mail. O de convites passou a `91510000001320` (dígitos válidos).

## Design Notes

Trigger sugerido (sobrescreve sempre; conta sem Empresa cai no `NOT NULL`):

```sql
CREATE FUNCTION usuarios_preencher_empresa_raiz() RETURNS trigger AS $$
BEGIN
  SELECT COALESCE(e.empresa_origem_id, e.id) INTO NEW.empresa_raiz_id
  FROM empresas e WHERE e.id = NEW.empresa_id;
  RETURN NEW;
END $$ LANGUAGE plpgsql;
```

Conta órfã (`empresa_id IS NULL`) deixa de ser possível. O CLI `cmd/migrar-multi-empresa` (9.4, já executado em produção) continua compilando e servindo às outras tabelas; para `usuarios` o backfill passa a encontrar sempre 0 linhas — não precisa mexer no CLI, só nos testes que criavam contas órfãs.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros
- `cd backend && DATABASE_URL='postgres://stockflow:stockflow@127.0.0.1:5432/stockflow_t91?sslmode=disable' go test -p 1 -count=1 -timeout 25m ./...` -- expected: verde (a suíte `services` leva ~8 min)
- `psql "$DATABASE_URL" -c '\d usuarios'` -- expected: `empresa_raiz_id` not null, trigger e restrição `EXCLUDE` listados

## Auto Run Result

**Resumo:** revisão de acompanhamento da Story 15.1 (e-mail único entre as Empresas reais, AD-36). A implementação da migration `000049` (`usuarios.empresa_raiz_id`, trigger `BEFORE INSERT`, backfill, `NOT NULL`, restrição `EXCLUDE USING gist`) e o 409 em convite, cadastro e criação de Empresa foram mantidos. Esta passada aplicou 3 correções pequenas de operação e de teste, sem mudar comportamento da API.

**Arquivos alterados nesta passada:**
- `backend/migrations/000049_add_empresa_raiz_id_to_usuarios.up.sql` -- só comentário: como recuperar o estado `dirty` se uma pré-checagem abortar.
- `.github/workflows/deploy-cliente-aws.yml` -- `::warning::` quando falta `ADMIN_EMPRESA_SLUG` e o seed do admin é pulado.
- `installer/cliente-aws/.env.template` -- nova chave `ADMIN_EMPRESA_SLUG` documentada.
- `backend/handlers/convites_test.go` -- CNPJ próprio para a Empresa `hconv-u151-outra`.

**Revisão:** 4 camadas (blind, edge-case, verification-gap, intent-alignment). O verification-gap não achou lacunas. 3 patches (todos low), 0 adiados, 22 rejeitados. Entre os rejeitados: trigger de `UPDATE` (proibido pelo intent), mensagem "nesta empresa" do convite e revelação de que o e-mail existe (o intent exige o 409 com a mensagem de hoje), outros caminhos de criação de conta (só existem os três `INSERT INTO usuarios` já tratados), `SET NOT NULL` em `empresa_id`, 23505 de `idx_usuarios_email_lower` no seed-admin (já era assim antes), lock do teste da migration (a suíte roda com `-p 1`) e código morto de `migracao_multi_empresa` (já está no ledger).

**Recomendação de nova revisão:** `false`. Patches: high 0, medium 0, low 3, pontuação 3×0 + 3 = 3 (< 5).

**Verificação:**
- `go build ./... && go vet ./...` e `gofmt -l .`: limpos.
- `go test -p 1 -count=1 -timeout 25m . ./handlers/... ./cmd/...` contra `stockflow_t91`: todos `ok`. Inclui o teste que executa a `000049` com duplicata e com conta órfã. `services` e `middleware` não foram tocados nesta passada e não foram rodados de novo.
- `.github/workflows/deploy-cliente-aws.yml` continua sendo YAML válido.

**Riscos residuais:**
- Os mesmos da passada anterior. A migration roda sozinha no boot da API. Se `stockflow.fbtechia.com` ou `suprimentos.fcxlabs.com` tiver conta com `empresa_id IS NULL`, ou o mesmo e-mail em duas Empresas reais, ela aborta, a API não sobe e é preciso corrigir os dados e dar `force 48`. Antes do push, rodar as duas consultas nos dois bancos.
- `CREATE EXTENSION btree_gist` depende da permissão do usuário do banco em produção.
- O `.env` já existente no servidor AWS precisa ter `ADMIN_EMPRESA_SLUG`, senão o seed do admin é pulado (agora com aviso no log).
- Nada foi publicado: todo push em `main` faz deploy em produção.
