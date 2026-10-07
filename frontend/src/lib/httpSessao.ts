import { apiUrl } from '@/lib/api';
import { clearAccessToken, setAccessToken } from '@/lib/session';

/**
 * Interceptor global de `window.fetch` que renova a sessão da Empresa
 * sozinho quando o access token (30min, `backend/services/auth.go`) vence no
 * meio do uso — feedback real (Karla, Ferreira Costa, 2026-10-07): o cadastro
 * de Produto "abortava" porque preencher o formulário não gera nenhuma
 * chamada de API, então nem o access token (30min) nem a janela deslizante
 * de 2h do refresh token (AD-6, FR1) eram renovados enquanto ela digitava —
 * ao salvar, a primeira chamada em muito tempo já caía com o token vencido.
 *
 * Mesmo diagnóstico de `lib/httpTrial.ts`: não existe um cliente HTTP central
 * (~101 call-sites fazem `fetch(apiUrl(...), { headers: authHeaders() })`
 * direto) — retrofitar cada um está fora de escopo; a única forma de cobrir
 * "qualquer chamada autenticada" é interceptar o `window.fetch` global uma
 * única vez. Instalado por `AuthProvider` (lib/auth.tsx), junto do
 * interceptor de trial.
 *
 * Regra: numa resposta `401` de uma chamada que JÁ mandava
 * `Authorization: Bearer <token>` (nunca login/cadastro público, que não
 * manda esse header — um 401 ali é credencial errada, não sessão vencida),
 * tenta UMA renovação via `POST /api/auth/refresh` (cookie HttpOnly) e repete
 * a chamada original com o token novo — mesmo padrão já usado só para o Dono
 * da Plataforma em `lib/plataforma.ts`. Chamadas de `/api/plataforma/...`
 * NUNCA entram aqui: aquele domínio já resolve o próprio 401 com o cookie
 * próprio do Dono: tentar renovar pela rota da Empresa aqui quebraria os
 * dois. Falha na renovação -> limpa o access token (sessão realmente
 * morta, ex.: 2h de inatividade de verdade) e devolve o 401 original, que
 * cada tela já trata do jeito que trata hoje.
 *
 * Requisições concorrentes compartilham UMA renovação (a renovação rotaciona
 * e revoga o cookie — uma segunda em paralelo derrubaria a sessão recém
 * renovada), mesmo padrão de `lib/plataforma.ts`.
 */

type ComFlagDeInterceptor = typeof fetch & { __sessaoInterceptor?: boolean };

let renovacaoEmCurso: Promise<string | null> | null = null;

function renovarAccessToken(): Promise<string | null> {
  renovacaoEmCurso ??= (async () => {
    try {
      const res = await fetch(apiUrl('/api/auth/refresh'), { method: 'POST' });
      if (!res.ok) return null;
      const corpo = (await res.json()) as { token?: string };
      if (!corpo.token) return null;
      setAccessToken(corpo.token);
      return corpo.token;
    } catch {
      return null;
    }
  })().finally(() => {
    renovacaoEmCurso = null;
  });
  return renovacaoEmCurso;
}

function ehChamadaDaPlataforma(input: RequestInfo | URL): boolean {
  const url = typeof input === 'string' ? input : input instanceof URL ? input.pathname : input.url;
  return url.includes('/api/plataforma/');
}

function headerAutorizacao(headers: RequestInit['headers']): string | undefined {
  if (!headers || Array.isArray(headers) || headers instanceof Headers) {
    // Todo call-site deste app passa um objeto literal (authHeaders()) —
    // Headers/array nunca ocorrem na prática; tratados como "sem token" por
    // segurança (nunca tenta renovar/repetir o que não reconhece).
    return undefined;
  }
  const chave = Object.keys(headers).find((k) => k.toLowerCase() === 'authorization');
  return chave ? (headers as Record<string, string>)[chave] : undefined;
}

/**
 * Instala o wrapper. Idempotente contra o `window.fetch` ATUAL (mesmo motivo
 * de `httpTrial.ts`: sobrevive a `vi.stubGlobal('fetch', ...)` nos testes).
 */
export function instalarInterceptorDeRenovacaoDeSessao(): void {
  const atual = window.fetch as ComFlagDeInterceptor;
  if (atual.__sessaoInterceptor) {
    return;
  }

  const original = atual.bind(window);
  const wrapper: ComFlagDeInterceptor = async (...args: Parameters<typeof fetch>) => {
    const [input, init] = args;
    const res = await original(input, init);
    if (res.status !== 401 || ehChamadaDaPlataforma(input)) {
      return res;
    }
    const tokenAtual = headerAutorizacao(init?.headers);
    if (!tokenAtual) {
      return res;
    }

    const novoToken = await renovarAccessToken();
    if (!novoToken) {
      clearAccessToken();
      return res;
    }
    return original(input, {
      ...init,
      headers: { ...(init?.headers as Record<string, string>), Authorization: `Bearer ${novoToken}` },
    });
  };
  wrapper.__sessaoInterceptor = true;
  window.fetch = wrapper;
}
