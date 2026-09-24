---
title: 'Story 15.3: "Esqueci a senha" na raiz do domínio'
type: 'feature'
created: '2026-09-24'
status: 'done'
baseline_revision: '4254a58f3ecb4c917f8c72811462ef4fa35e7e25'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-15-context.md'
warnings: [oversized]
deferred: []
---

<intent-contract>

## Intent

**Problem:** Na tela de login da raiz (Story 15.2), "Esqueci a senha" só orienta a pessoa a usar o endereço da Empresa. Quem não sabe o endereço `/e/{slug}` não consegue pedir a redefinição.

**Approach:** Criar a rota `POST /api/auth/esqueci-senha`, fora do prefixo `/e/{slug}` e sem `RequireEmpresa` (AD-36). Ela acha as contas ativas do e-mail em Empresas ativas e chama o `services.SolicitarRedefinicaoSenha` atual uma vez por conta, com a Empresa e o slug de cada uma. A resposta é sempre `202` com a mensagem genérica de hoje. O e-mail de redefinição passa a mostrar o nome da Empresa, para a pessoa distinguir o e-mail da real e o do Treinamento. Na raiz, "Esqueci a senha" vira o formulário do pedido.

## Boundaries & Constraints

**Always:**
- A regra de token/e-mail é SEMPRE a de `SolicitarRedefinicaoSenha(db, emailCfg, empresaID, empresaSlug, email)`, chamada uma vez por conta: invalida os tokens anteriores, 30 min, link `LinkDaEmpresa(AppURL, slug, "/redefinir-senha", token)`, outbox. Nenhuma regra reimplementada.
- Contas elegíveis: `lower(u.email) = normalizeEmail(email)`, `e.status = 'ativa'` e `u.ativo = true`, em ordem estável (real antes do Treinamento, depois por slug).
- Resposta: `202 {"mensagem": mensagemEsqueciSenha}`, byte-idêntica exista ou não a conta (e com e-mail em branco). Só JSON malformado ou grande demais (`authRequestMaxBytes`) → `400 VALIDATION_ERROR`. Erro de infraestrutura → `500 INTERNAL_ERROR`.
- O template `redefinicao_senha` passa a citar a Empresa ("na empresa **{nome}**") quando `variaveis.empresa` existe. `SolicitarRedefinicaoSenha` grava `empresa` = `empresas.nome_fantasia` da conta, nos dois caminhos (raiz e `/e/{slug}`). Linha antiga do outbox sem `empresa` renderiza como hoje, sem a frase. O nome passa por `html.EscapeString`.
- O frontend da raiz chama `fetch('/api/auth/esqueci-senha')` (nunca `apiUrl`) e trata qualquer `2xx` como sucesso.

**Block If:**
- Nenhum previsto.

**Never:**
- `GET /api/entrada`/`EMPRESA_PADRAO` (Story 15.4), rate limit novo (não existe no pedido por Empresa), mudar status/corpo de `POST /e/{slug}/api/auth/esqueci-senha` (continua `200`).
- Aceitar `empresa_id`/slug do body; revelar por status, corpo ou ramo de resposta se o e-mail tem conta ou quantas.
- `git push` (todo push em `main` faz deploy em produção).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Uma conta ativa | conta em `acme` | 202; 1 `tokens_acao` `redefinicao_senha` + 1 `emails_pendentes` com `link` contendo `/e/acme/redefinir-senha?token=` e `empresa` = nome de `acme` | — |
| Real + Treinamento | contas em `acme` e `acme-treinamento` | 202; um token + um e-mail por conta, cada um com o slug e o nome da sua Empresa | — |
| E-mail sem conta | — | 202, corpo idêntico; nada gravado | — |
| Conta desativada / Empresa inativa | `u.ativo=false` ou `e.status='inativa'` | 202, corpo idêntico; nada gravado para ela | — |
| Pedido repetido | dois pedidos seguidos | só o último token de cada conta fica válido (mesma regra por Empresa) | — |
| E-mail em branco / maiúsculas e espaços | `"  "` / `" Ana@X.com "` | 202; branco não grava nada; maiúsculas acham a conta | — |
| Payload inválido | `{` | 400 `VALIDATION_ERROR` | — |

</intent-contract>

## Code Map

- `backend/services/auth.go:1003-1079` -- `SolicitarRedefinicaoSenha`: trocar o `SELECT id, nome FROM usuarios` por um JOIN com `empresas` que traga também `e.nome_fantasia`, e acrescentar `"empresa"` em `variaveis`. A assinatura não muda.
- `backend/services/email.go:126-143` -- template `redefinicao_senha`: frase opcional com a Empresa. O molde é `primeiro_acesso` (:144), que já usa `variaveis["empresa"]`.
- `backend/services/entrada.go` -- novo `SolicitarRedefinicaoSenhaPelaConta(db, emailCfg, email) error`. Faz um SELECT das contas elegíveis (`u.empresa_id, e.slug`, ordem de `selectContasEntrada`) e chama `SolicitarRedefinicaoSenha` para cada uma. Retorna no primeiro erro.
- `backend/handlers/auth.go:477-518` -- `mensagemEsqueciSenha`, `esqueciSenhaRequest` e `EsqueciSenhaHandler`, a referência. Reusar a const e o struct.
- `backend/handlers/entrada.go` -- novo `EsqueciSenhaPelaContaHandler(db, emailCfg)`. Nunca chama `empresaDaRequisicao`.
- `backend/main.go:321-346` -- registrar `POST /api/auth/esqueci-senha` direto no `mux`, junto das rotas `/api/auth/entrar*`. Atualizar os comentários de exceção ao prefixo. O `emailCfg` já está no escopo (usado em `CriarEmpresaHandler`).
- Testes: `backend/handlers/entrada_test.go` (`prepararEntradaH`, `criarContaEntradaH(t, db, empresaID, email, senha, ativo, emailVerificado, mfa)`, `muxEntrada`, `postEntrada`) e `backend/services/entrada_test.go` (helpers equivalentes). `services/auth_test.go:934-1060` já testa `SolicitarRedefinicaoSenha`. `services/email_test.go:58` testa o template. Banco: `stockflow_t91`, `go test -p 1`.
- `frontend/src/lib/entrada.ts` -- nova `pedirRedefinicaoPelaConta(email): Promise<boolean>`: `true` em `2xx`, `false` em rede ou não-2xx.
- `frontend/src/pages/SemEmpresaPage.tsx` -- trocar `avisoEsqueci` por uma etapa `esqueci`. Visual e textos seguem `frontend/src/pages/EsqueciSenhaPage.tsx`: título "Esqueci minha senha", campo E-mail pré-preenchido com o e-mail já digitado, botão "Enviar link de redefinição", sucesso `<output>` "Se o e-mail existir, você receberá um link.", erro "Não foi possível enviar o link agora. Tente novamente em instantes." e botão "Voltar para o login".
- `frontend/src/pages/SemEmpresaPage.test.tsx:47-56` -- o teste da orientação inline é substituído.

## Tasks & Acceptance

**Execution:**
- `backend/services/auth.go`, `backend/services/email.go` -- nome da Empresa na variável e no template -- AC "cada um com o nome da sua Empresa".
- `backend/services/entrada.go` -- `SolicitarRedefinicaoSenhaPelaConta` -- descobre as contas e delega.
- `backend/handlers/entrada.go`, `backend/main.go` -- handler `202` e rota fora de `RequireEmpresa`.
- `backend/services/entrada_test.go`, `backend/handlers/entrada_test.go`, `backend/services/email_test.go` -- cada linha da I/O Matrix (conferir `tokens_acao` e `emails_pendentes.variaveis_json`: `link` e `empresa`). Handler pelo `muxEntrada`, com os corpos das respostas de "sem conta" e "com conta" byte-idênticos. Template com e sem `empresa`, com escape de HTML.
- `frontend/src/lib/entrada.ts` + `entrada.test.ts` -- `pedirRedefinicaoPelaConta` (URL, método, corpo; 202 → true; 500/rede → false).
- `frontend/src/pages/SemEmpresaPage.tsx` + `.test.tsx` -- etapa "Esqueci a senha": pré-preenchimento, sucesso, erro, voltar.

**Acceptance Criteria:**
- Given a tela de login da raiz, when a pessoa aciona "Esqueci a senha", informa o e-mail e envia, then o frontend chama `POST /api/auth/esqueci-senha` e mostra "Se o e-mail existir, você receberá um link." para qualquer resposta `2xx`.
- Given a rota `POST /e/{slug}/api/auth/esqueci-senha`, when esta story entra, then ela continua respondendo `200` com o mesmo corpo, e as suítes existentes seguem verdes. A única mudança é o `empresa` a mais nas variáveis do e-mail.

## Review Triage Log

### 2026-09-24 — Review pass
- intent_gap: 0
- bad_spec: 0
- patch: 4 (high 0, medium 0, low 4)
- defer: 0
- reject: 24 (high 0, medium 3, low 21)
- addressed_findings:
  - `[low]` `[patch]` Em `SolicitarRedefinicaoSenhaPelaConta`, um erro numa conta parava o laço e a outra conta ficava sem link. Agora tenta todas as contas e devolve os erros juntos (`errors.Join`). A leitura das contas foi para `contasRedefinicao`, com `defer rows.Close()`.
  - `[low]` `[patch]` Faltava teste do caminho de erro: `TestEsqueciSenhaRaiz_ErroDeInfraestrutura` (banco fechado → `500 INTERNAL_ERROR`, nunca o `202`).
  - `[low]` `[patch]` Na etapa "Esqueci", o e-mail corrigido se perdia ao voltar ao login. Agora volta para o formulário de login, e o campo fica desabilitado durante o envio. O teste foi atualizado.
  - `[low]` `[patch]` Comentário de cabeçalho de `handlers/entrada.go` refeito, com linha longa quebrada.

## Design Notes

"Conta ativa" (AD-36, contexto do épico) é lida ao pé da letra: `u.ativo = true` e Empresa `ativa`. O pedido por `/e/{slug}` numa Empresa inativa já dá 404 sem e-mail, e uma conta desativada não entra mesmo com senha nova (`Login` recusa). O filtro não abre exceção nenhuma e não aparece na resposta.

Os dois e-mails de real + Treinamento teriam texto idêntico com o template de hoje; só o link mudaria. Por isso o nome da Empresa entra no e-mail, que é o AC "cada um com o nome e o endereço da sua Empresa".

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros, sem arquivos listados
- `cd backend && DATABASE_URL='postgres://stockflow:stockflow@127.0.0.1:5432/stockflow_t91?sslmode=disable' go test -p 1 -count=1 -timeout 25m ./...` -- expected: verde (`services` leva ~8 min; não rodar junto com o vitest)
- `cd frontend && npx tsc --noEmit -p tsconfig.app.json && npx tsc -b && npx vitest run` -- expected: verde

## Auto Run Result

**Resumo:** a tela de login da raiz agora pede a redefinição de senha direto. `POST /api/auth/esqueci-senha` fica fora de `/e/{slug}` e de `RequireEmpresa`. Ele acha as contas ativas do e-mail em Empresas ativas (real antes do Treinamento) e chama o `SolicitarRedefinicaoSenha` de hoje uma vez por conta. Responde sempre `202` com a mensagem genérica. O e-mail de redefinição passa a citar o nome da Empresa ("na empresa **X**"), o que distingue o e-mail da real do e-mail do Treinamento. Essa mudança vale também para o pedido feito por `/e/{slug}`, que continua em `200`.

**Arquivos:**
- `backend/services/auth.go`: `SolicitarRedefinicaoSenha` busca `nome_fantasia` e grava `empresa` nas variáveis do e-mail.
- `backend/services/email.go`: o template `redefinicao_senha` cita a Empresa quando ela vem nas variáveis, com escape de HTML.
- `backend/services/entrada.go`: `SolicitarRedefinicaoSenhaPelaConta` e `contasRedefinicao`.
- `backend/handlers/entrada.go`: `EsqueciSenhaPelaContaHandler` (`202`, `400`, `500`).
- `backend/main.go`: rota registrada fora de `RequireEmpresa`, com os comentários de exceção atualizados.
- `backend/services/entrada_test.go`, `backend/handlers/entrada_test.go`, `backend/main_test.go`, `backend/services/auth_test.go`, `backend/services/email_test.go`: I/O Matrix, corpo byte-idêntico, erro 500, router real (raiz `202` e `/e/{slug}` `200`) e template com e sem Empresa.
- `frontend/src/lib/entrada.ts` (+ test): `pedirRedefinicaoPelaConta`.
- `frontend/src/pages/SemEmpresaPage.tsx` (+ test): etapa "Esqueci minha senha" com e-mail pré-preenchido, sucesso, erro e voltar.

**Review:** 4 camadas (blind, edge-case, verification-gap, intent-alignment). 4 patches (todos low), 0 adiados, 24 rejeitados. Entre os rejeitados:
- oráculo de tempo (0 contra N transações; o pedido por Empresa tem a mesma característica);
- rate limit (o AC exige os mesmos limites do pedido por Empresa, que não tem);
- `202` na raiz contra `200` em `/e/{slug}` (o `202` é o que o épico pede, e a rota por Empresa não deve mudar);
- conta desativada ainda recebe e-mail por `/e/{slug}` (comportamento que já existia; a spec decidiu não mexer);
- nome da Empresa no assunto (o AC é atendido no corpo);
- validação de e-mail vazio no cliente e foco ao trocar de etapa (mesmo padrão de `EsqueciSenhaPage`);
- índice global de `lower(email)` (a restrição gist da 15.1 cobre).

**Recomendação de nova revisão:** `false`. Patches: high 0, medium 0, low 4. Pontuação 3×0 + 4 = 4 (< 5).

**Verificação:**
- `go build ./...`, `go vet ./...` e `gofmt -l .`: limpos.
- `go test -p 1 -count=1` contra `stockflow_t91`: `handlers`, raiz, `services`, `middleware` e `cmd/...` todos `ok`, já com os patches aplicados.
- `npx tsc --noEmit -p tsconfig.app.json`, `npx tsc -b` e `npx vitest run`: 61 arquivos, 832 testes, verdes.

**Riscos residuais:**
- O tempo de resposta cresce com o número de contas elegíveis (0, 1 ou 2 transações curtas). O corpo e o status não revelam nada.
- Se o banco falhar no meio, uma conta pode receber o link e a outra não. A resposta é `500` e um novo pedido resolve.
- Nenhuma ação de operador é devida. Nada foi publicado: todo push em `main` faz deploy em produção.

