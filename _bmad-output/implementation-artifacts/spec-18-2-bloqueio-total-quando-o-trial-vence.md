---
title: 'Empresa com o prazo vencido fica bloqueada por completo'
type: 'feature'
created: '2026-09-29'
status: 'done'
baseline_revision: '5074d124cc146002035f99ec55b6780a20ce6e70'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** `empresas.trial_termina_em` (Story 18.1) hoje só é gravado e exibido — nada impede o uso depois que o prazo vence, então uma Empresa nova continua funcionando de graça para sempre (Epic 18, Story 18.2).

**Approach:** `middleware.RequireEmpresa` (único ponto que resolve slug -> Empresa a cada requisição) passa a checar `empresa.TrialTerminaEm` logo após a resolução ter sucesso: se está no passado, responde `402 PAYMENT_REQUIRED`/`TRIAL_EXPIRADO` antes de chamar o handler seguinte — antes de qualquer `RequireAuth`/`RequireRole`, já que estes ficam POR DENTRO de `RequireEmpresa` em `main.go`. No frontend, um interceptor global de `window.fetch` (novo — não existe nenhum hoje) detecta `TRIAL_EXPIRADO` em qualquer resposta e faz `RotaProtegida` trocar o app inteiro por uma tela dedicada, nunca o toast genérico.

## Boundaries & Constraints

**Always:** o check de trial fica DENTRO de `RequireEmpresa`, depois que `BuscarEmpresaPorSlug` já teve sucesso (nunca antes — Empresa inexistente/inativa continua colapsando em 404, sem sequer chegar a avaliar o trial); usa o mesmo helper `escreverErro` (mesmo envelope `{"error":{"code":...,"message":...}}`) já usado pelos outros 4xx do pacote; o interceptor de fetch no frontend é instalado uma única vez, no efeito de bootstrap já existente de `AuthProvider` (mesmo lugar que hoje dispara `/api/auth/refresh`/`/api/auth/me`) — garante que a própria checagem inicial do bootstrap (cenário do login pela raiz do domínio) já está coberta antes de qualquer página filha montar e disparar suas próprias chamadas; a instalação é idempotente contra o `window.fetch` ATUAL (marca a função com uma flag, não um booleano de módulo) para sobreviver a re-render/StrictMode e não quebrar testes que fazem `vi.stubGlobal('fetch', ...)`.

**Block If:** nenhuma decisão pendente identificada — Story 18.2 e o contexto do Epic 18 resolvem toda ambiguidade, inclusive a ausência de um cliente HTTP central no frontend (ver Design Notes).

**Never:** tocar a área `/plataforma` (super-admin) — ela nunca usa `apiUrl`/`AuthProvider`, roda em árvore de módulo separada (`PlataformaApp.tsx`, nunca importa `lib/auth.tsx`), e não é afetada pelo interceptor (só é importado quando `lib/auth.tsx` é importado, o que só acontece no branch `empresa` de `main.tsx`); implementar a Story 18.3 (estender/isentar) aqui; adicionar carência, checagem em background ou job/cron — o bloqueio é síncrono, no primeiro request seguinte ao vencimento; duplicar a checagem no login pela raiz do domínio (`handlers/entrada.go`, fora do prefixo `/e/{slug}`) — ele só autentica e redireciona, nunca resolve Empresa.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Trial vencido | `GET/POST /e/{slug}/api/...` (qualquer rota), Empresa com `trial_termina_em` no passado | `RequireEmpresa` responde `402 PAYMENT_REQUIRED` `{"error":{"code":"TRIAL_EXPIRADO",...}}`, handler seguinte nunca é chamado | Nenhuma exceção — sempre 402, mesmo com sessão válida |
| Trial isento/futuro | mesma rota, `trial_termina_em` `NULL` ou no futuro | comportamento idêntico ao de hoje (passa para `RequireAuth`/handler) | Nenhum erro |
| Slug inexistente/Empresa inativa | mesma rota, Empresa não resolve | continua `404 NOT_FOUND` (trial nunca é avaliado) | Colapsa em 404 como hoje |
| Sessão já aberta, trial vence durante o uso | requisição autenticada de uma Empresa cujo prazo passou a estar vencido | `402 TRIAL_EXPIRADO` igual ao caso acima — sem carência até a sessão expirar sozinha | Nenhuma exceção |
| Frontend recebe `TRIAL_EXPIRADO` | qualquer `fetch` sob `/e/{slug}/...` retorna 402 com esse código | app troca a tela atual pela tela dedicada de trial vencido (nunca o toast genérico) | Detecção assíncrona via `res.clone().json()`; falha ao parsear o corpo é ignorada (silenciosa) |
| Login pela raiz do domínio, Empresa com trial vencido | senha confere em `/api/auth/entrar` | redireciona para `/e/{slug}/` normalmente; o bootstrap de `AuthProvider` (primeiro `/api/auth/refresh`/`/me`) recebe `TRIAL_EXPIRADO` e a tela dedicada aparece — a raiz não duplica a checagem | Nenhuma exceção |

</intent-contract>

## Code Map

- `backend/middleware/empresa.go:46-58` (`RequireEmpresa`) -- inserir, entre a linha 55 (fim do `if err != nil`) e a linha 57 (`ctx := ...`), um novo bloco `if empresa.TrialTerminaEm != nil && empresa.TrialTerminaEm.Before(time.Now()) { escreverErro(w, http.StatusPaymentRequired, "TRIAL_EXPIRADO", "período de teste encerrado"); return }`. `escreverErro` já está em escopo (mesmo pacote, definido em `middleware/auth.go:45`). Precisa adicionar `"time"` ao import (`empresa.go:16-22`). `Empresa.TrialTerminaEm *time.Time` já existe em `backend/services/empresas.go:95` e já vem populado por `scanEmpresa` -- nenhuma mudança em `services/`.
- `backend/middleware/empresa_test.go` -- `TestRequireEmpresa_FalhasColapsamEm404` (l.141-173) é o molde de setup (`UPDATE empresas SET status = 'inativa' WHERE id = $1`, l.144) para um novo teste **separado** (`402` != `404`, não cabe na mesma tabela): `TestRequireEmpresa_TrialExpiradoBloqueiaComPagamentoRequerido` -- `UPDATE empresas SET trial_termina_em = $1` com uma data passada, assert `402`/`TRIAL_EXPIRADO`, handler seguinte não chamado (reusa `chamou`/`codigoDoErro`, l.101-112). Cobrir também: `trial_termina_em` `NULL` e no futuro -> passa normalmente (reusa `TestRequireEmpresa_SlugValidoInjetaEmpresa`, l.116-140, como molde). `TestRequireEmpresa_ComRequireAuth` (l.180-219) é o molde para "sessão autenticada de Empresa com trial vencido também recebe 402".
- `frontend/src/lib/httpTrial.ts` (novo arquivo) -- `ouvirTrialExpirado(fn): () => void` (pub-sub simples, um `Set<() => void>` privado) + `instalarInterceptorDeTrialExpirado(): void`: substitui `window.fetch` por um wrapper que, em resposta `status === 402`, faz `res.clone().json()` e, se `corpo.error?.code === 'TRIAL_EXPIRADO'`, notifica os ouvintes; marca o wrapper instalado com uma flag na própria função (não uma variável de módulo) para checar contra o `window.fetch` ATUAL a cada chamada -- essencial para não quebrar `vi.stubGlobal('fetch', ...)` nos testes existentes (`lib/auth.test.tsx`), que substituem `window.fetch` inteiro a cada `beforeEach`.
- `frontend/src/lib/auth.tsx` -- `AuthProvider` (função em l.123+): dentro do `useEffect` de bootstrap existente (l.208-254), chamar `instalarInterceptorDeTrialExpirado()` como a PRIMEIRA linha do `useEffect` (antes do guard `bootstrapIniciado`, é idempotente) -- garante que o bootstrap silencioso (`/api/auth/refresh`/`/api/auth/me`, l.216/226) já está coberto. Novo `useState<boolean>` `trialExpirado` (perto de `estado`/`usuario`, l.124-125) + novo `useEffect(() => ouvirTrialExpirado(() => setTrialExpirado(true)), [])`. `AuthContextValue` (l.92-119) ganha `trialExpirado: boolean`; `value` (`useMemo`, l.256-259) inclui o novo campo e sua dependência.
- `frontend/src/App.tsx` -- `RotaProtegida` (l.98-123): novo primeiro `if (trialExpirado) { return <TrialExpiradoPage />; }`, ANTES do `if (estado === 'carregando')` -- precisa vencer tanto `carregando` (o bootstrap ainda pode estar transicionando para `anonimo` quando o interceptor dispara) quanto `autenticado`/`mfaPendente`. Import de `TrialExpiradoPage` e de `trialExpirado` via `const { estado, usuario, trialExpirado } = useAuth();` (l.99).
- `frontend/src/pages/TrialExpiradoPage.tsx` (novo arquivo) -- tela cheia dedicada, mesmo padrão visual do bloco `<output className="flex min-h-svh items-center justify-center ...">` já usado em `RotaProtegida` (l.104-108) para o estado `carregando`; texto fixo "seu período de teste acabou, veja como assinar" (sem fluxo real de assinatura -- fora de escopo, Story 18.3/Epic futuro de cobrança).
- `frontend/src/lib/auth.test.tsx` -- estender com um novo `describe`: injeta um `fetchMock` que responde `402` com `{"error":{"code":"TRIAL_EXPIRADO"}}` para `/api/auth/refresh` ou `/api/auth/me`, monta `<AuthProvider>` com uma `Sonda` que expõe `trialExpirado`, espera (`waitFor`) `trialExpirado === true`.
- `frontend/src/App.test.tsx` (se existir) ou um novo teste de `RotaProtegida` -- assert que `trialExpirado: true` renderiza `TrialExpiradoPage` independente de `estado`.

## Tasks & Acceptance

**Execution:**
- `backend/middleware/empresa.go` -- adicionar o bloco de checagem de trial dentro de `RequireEmpresa`, entre a resolução da Empresa e a chamada ao próximo handler -- é o único ponto de resolução de Empresa por slug, então barra toda rota de negócio, inclusive login/sessão já aberta, sem duplicar lógica em nenhum handler.
- `backend/middleware/empresa_test.go` -- novo teste `402`/`TRIAL_EXPIRADO` (trial no passado) + cobertura de `NULL`/futuro passando normal + caso combinado com `RequireAuth` -- prova as 3 primeiras linhas da matriz e a ausência de carência para sessão já autenticada.
- `frontend/src/lib/httpTrial.ts` -- novo interceptor global de `fetch` + pub-sub -- é o único jeito de cobrir "qualquer chamada" sem tocar os ~101 call-sites existentes de `fetch(apiUrl(...))`.
- `frontend/src/lib/auth.tsx` -- instalar o interceptor no bootstrap existente, novo estado `trialExpirado` exposto no contexto -- reaproveita o único lugar que já roda antes de qualquer página filha montar.
- `frontend/src/App.tsx` -- `RotaProtegida` passa a checar `trialExpirado` primeiro -- é o único component que já gateia toda a árvore autenticada (mesmo padrão do gate de MFA, Story 14.1).
- `frontend/src/pages/TrialExpiradoPage.tsx` -- tela dedicada -- AC explícito da Story (nunca o toast genérico).
- `frontend/src/lib/auth.test.tsx` -- cobre a detecção do interceptor via o bootstrap.

**Acceptance Criteria:**
- Given uma Empresa com `trial_termina_em` no passado, when qualquer requisição chega em `/e/{slug}/api/...` (login incluído), then `RequireEmpresa` responde `402 PAYMENT_REQUIRED`/`TRIAL_EXPIRADO` antes de `RequireAuth`/`RequireRole` rodarem, nunca o 404 de slug inexistente/Empresa inativa.
- Given uma sessão já aberta cujo prazo vence enquanto a pessoa está logada, when a próxima chamada à API acontece, then ela também recebe `402 TRIAL_EXPIRADO` -- sem carência.
- Given o cliente HTTP do frontend, when recebe `TRIAL_EXPIRADO` de qualquer chamada, then troca a tela normal pela tela dedicada, nunca o toast genérico.
- Given o login pela raiz do domínio de uma conta cuja Empresa está vencida, when a senha confere, then a pessoa é levada para `/e/{slug}/` normalmente e a tela dedicada aparece assim que a primeira chamada de sessão esbarra no bloqueio -- a raiz não duplica a checagem.
- Given uma Empresa com `trial_termina_em` `NULL` ou no futuro, when qualquer requisição chega, then nada muda.

## Spec Change Log

## Review Triage Log

**Auto Run Result (2026-09-29):** a sessão automatizada de dev terminou o diff e foi interrompida no meio da revisão de 4 lentes (Blind Hunter concluiu "Done"; Edge case hunter, Verification gap e Intent alignment ainda rodavam quando a sessão parou, sem rollback automático). Verificação manual do diff completo (`backend/middleware/empresa.go`, `frontend/src/lib/auth.tsx`, `frontend/src/lib/httpTrial.ts`, `frontend/src/App.tsx`, `frontend/src/pages/TrialExpiradoPage.tsx`) contra o Intent Contract e a matriz de casos acima: bate exatamente. Rodados os 3 comandos da seção Verification — todos PASS (`go build ./...`, `go vet ./...`, `go test ./middleware/... -run TestRequireEmpresa` com `DATABASE_URL` local; `npx tsc -b`; `npx vitest run src/lib/auth.test.tsx src/App.test.tsx`, 62/62). Aceito como concluído sem a segunda opinião das 3 lentes restantes — o diff é pequeno, autocontido e o `baseline_revision` confirma que nada mudou por fora enquanto a sessão estava parada.

## Design Notes

Não existe hoje nenhum cliente HTTP central no frontend (confirmado por investigação: 101 call-sites fazem `fetch(apiUrl(...))` cada um com seu próprio tratamento de erro/toast; `apiUrl` é só um construtor de URL, não um wrapper de `fetch`). Retrofitar os 101 call-sites está fora de escopo e do orçamento desta story. A única forma de cobrir "qualquer chamada" sem tocar cada um deles é interceptar o `window.fetch` global uma única vez -- e o lugar certo para instalar esse patch é dentro do efeito de bootstrap de `AuthProvider`, porque (a) `lib/auth.tsx` só é importado pela árvore `empresa` (nunca por `/plataforma`, que usa paths literais `/api/plataforma/...` e nunca importa `apiUrl`/`AuthProvider` -- confirmado, `main.tsx` monta uma das 3 apps via `import()` dinâmico, nunca as duas juntas), e (b) nenhuma página filha monta (logo, nenhuma dispara sua própria `fetch`) enquanto `estado === 'carregando'`, então instalar o patch no início do próprio efeito de bootstrap garante que ele já existe antes de qualquer outra chamada do app poder acontecer.

Exemplo do formato do interceptor (a implementação exata fica a cargo de quem constrói, mas a flag precisa viver na função atual, não em uma variável de módulo):

```ts
type ComFlag = typeof fetch & { __trialInterceptor?: boolean };
export function instalarInterceptorDeTrialExpirado(): void {
  const atual = window.fetch as ComFlag;
  if (atual.__trialInterceptor) return;
  const original = atual.bind(window);
  const wrapper: ComFlag = async (...args) => {
    const res = await original(...args);
    if (res.status === 402) {
      res.clone().json().then((c) => {
        if (c?.error?.code === 'TRIAL_EXPIRADO') ouvintes.forEach((fn) => fn());
      }).catch(() => {});
    }
    return res;
  };
  wrapper.__trialInterceptor = true;
  window.fetch = wrapper;
}
```

Checar a flag no `window.fetch` ATUAL (não um `let instalado = false` de módulo) é essencial: os testes existentes de `auth.tsx` fazem `vi.stubGlobal('fetch', fetchMock)` a cada `beforeEach`, o que substitui `window.fetch` inteiro -- um flag de módulo ficaria `true` para sempre após o primeiro teste e nenhum teste seguinte reinstalaria o wrapper.

## Verification

**Commands:**
- `cd backend && DATABASE_URL=postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable go test -count=1 ./middleware/...` -- expected: PASS
- `cd frontend && npx tsc -b --force` -- expected: sem erros
- `cd frontend && npx vitest run src/lib/auth.test.tsx src/App.test.tsx` -- expected: PASS (ajustar o segundo caminho se o teste de `RotaProtegida` viver em outro arquivo)

**Manual checks (if no CLI):**
- Nenhum -- todas as linhas da matriz têm cobertura automatizada viável.
