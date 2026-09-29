/**
 * Interceptor global de `window.fetch` para `TRIAL_EXPIRADO` (Story 18.2,
 * Epic 18 — período de teste de 14 dias). Não existe nenhum cliente HTTP
 * central no frontend hoje: ~101 call-sites fazem `fetch(apiUrl(...))`, cada
 * um com seu próprio tratamento de erro/toast (ver Design Notes da spec).
 * Retrofitar cada um deles está fora de escopo — a única forma de cobrir
 * "qualquer chamada" é interceptar o `window.fetch` global uma única vez.
 *
 * `instalarInterceptorDeTrialExpirado` é chamado pelo efeito de bootstrap de
 * `AuthProvider` (lib/auth.tsx) — o único lugar que já roda antes de
 * qualquer página filha montar (e, portanto, antes de qualquer outra
 * `fetch` do app poder acontecer).
 *
 * A instalação precisa ser idempotente contra o `window.fetch` ATUAL: a
 * flag vive na própria função (não numa variável de módulo), porque os
 * testes existentes de `lib/auth.tsx` fazem `vi.stubGlobal('fetch',
 * fetchMock)` a cada `beforeEach` — isso substitui `window.fetch` inteiro, e
 * uma flag de módulo ficaria `true` para sempre após o primeiro teste,
 * impedindo o wrapper de ser reinstalado sobre o novo mock.
 */

type ComFlagDeInterceptor = typeof fetch & { __trialInterceptor?: boolean };

type Ouvinte = () => void;

const ouvintes = new Set<Ouvinte>();

/**
 * Registra um ouvinte chamado sempre que o interceptor detecta
 * `TRIAL_EXPIRADO` em alguma resposta. Devolve a função de cancelamento
 * (padrão `useEffect`).
 */
export function ouvirTrialExpirado(fn: Ouvinte): () => void {
  ouvintes.add(fn);
  return () => {
    ouvintes.delete(fn);
  };
}

/**
 * Instala o wrapper de `window.fetch` que detecta `TRIAL_EXPIRADO`. Idempotente
 * contra o `window.fetch` ATUAL — chamar de novo depois que outra coisa (um
 * teste, um outro módulo) já substituiu `window.fetch` reinstala o wrapper
 * por cima do fetch novo, em vez de ficar preso ao antigo.
 */
export function instalarInterceptorDeTrialExpirado(): void {
  const atual = window.fetch as ComFlagDeInterceptor;
  if (atual.__trialInterceptor) {
    return;
  }

  const original = atual.bind(window);
  const wrapper: ComFlagDeInterceptor = async (...args: Parameters<typeof fetch>) => {
    const res = await original(...args);
    if (res.status === 402) {
      res
        .clone()
        .json()
        .then((corpo: { error?: { code?: string } }) => {
          if (corpo?.error?.code === 'TRIAL_EXPIRADO') {
            ouvintes.forEach((fn) => fn());
          }
        })
        .catch(() => {
          // Corpo não é JSON válido (ou já consumido) — detecção é best-effort,
          // silenciosa (mesma postura do resto do bootstrap de AuthProvider).
        });
    }
    return res;
  };
  wrapper.__trialInterceptor = true;
  window.fetch = wrapper;
}
