/**
 * Config de SSO (Story 1.9) buscada em RUNTIME do backend
 * (`GET /api/auth/sso/config`), nunca de env var de build: a mesma imagem de
 * frontend serve ambientes com e sem SSO, e cravar a config no bundle vazaria
 * o botão para quem não devia tê-lo. Molde de `fetchIAMConfig` do FB_APU02.
 */
export interface SSOConfig {
  enabled: boolean;
  base_url?: string;
  client_id?: string;
  redirect_uri?: string;
  scopes?: string;
}

let cache: SSOConfig | null = null;

/**
 * Busca (e cacheia em memória, só em sucesso) a config de SSO deste servidor.
 * NUNCA lança: qualquer falha de rede/parse/`!res.ok` vira `{enabled:false}`
 * (fail-safe — esconde o botão em vez de quebrar a tela de Login). A falha não
 * é cacheada, então a próxima chamada tenta de novo.
 */
export async function fetchSSOConfig(): Promise<SSOConfig> {
  if (cache) {
    return cache;
  }
  try {
    const res = await fetch('/api/auth/sso/config');
    if (!res.ok) {
      return { enabled: false };
    }
    const data = (await res.json()) as SSOConfig;
    cache = data;
    return data;
  } catch {
    return { enabled: false };
  }
}

/** Limpa o cache em memória — uso exclusivo dos testes (cada `it` parte de um
 * estado limpo de config). */
export function resetSSOConfigCache(): void {
  cache = null;
}
