/**
 * PKCE (RFC 7636) para o Authorization Code flow do Keycloak — Story 1.9.
 *
 * Desvio deliberado do FB_APU02 (que usa `@noble/hashes` por rodar em contexto
 * não-seguro): aqui o S256 vem de `crypto.subtle.digest`, disponível em
 * `localhost` (dev) e HTTPS (produção) — cobre os ambientes reais do stockflow
 * e evita uma dependência npm nova. Um dev acessando por IP de LAN sobre HTTP
 * puro não terá o botão funcional (aceitável para ferramenta interna).
 */
import type { SSOConfig } from './config';

export const SESSION_KEY_VERIFIER = 'sso_pkce_verifier';
export const SESSION_KEY_STATE = 'sso_oauth_state';

function base64url(bytes: Uint8Array): string {
  let s = '';
  for (const b of bytes) {
    s += String.fromCharCode(b);
  }
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

/** 48 bytes aleatórios em base64url (64 chars) — dentro da faixa 43–128 da RFC. */
export function gerarVerifier(): string {
  const raw = new Uint8Array(48);
  crypto.getRandomValues(raw);
  return base64url(raw);
}

/** `code_challenge` = base64url(SHA-256(verifier)) — método S256. */
export async function gerarChallenge(verifier: string): Promise<string> {
  const data = new TextEncoder().encode(verifier);
  const digest = await crypto.subtle.digest('SHA-256', data);
  return base64url(new Uint8Array(digest));
}

/** UUID v4 via getRandomValues (não depende de `crypto.randomUUID`, ausente em
 * contexto não-seguro). */
export function gerarState(): string {
  const b = new Uint8Array(16);
  crypto.getRandomValues(b);
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = Array.from(b, (x) => x.toString(16).padStart(2, '0'));
  return `${h.slice(0, 4).join('')}-${h.slice(4, 6).join('')}-${h.slice(6, 8).join('')}-${h.slice(8, 10).join('')}-${h.slice(10).join('')}`;
}

/**
 * Grava `verifier`/`state` em `sessionStorage` e devolve a URL de authorize do
 * realm. Lança se a config não está habilitada ou faltam campos essenciais — o
 * chamador só deve invocar depois de confirmar `cfg.enabled`.
 */
export async function buildLoginUrl(cfg: SSOConfig): Promise<string> {
  if (!cfg.enabled || !cfg.base_url || !cfg.client_id || !cfg.redirect_uri) {
    throw new Error('SSO não configurado neste servidor.');
  }

  const verifier = gerarVerifier();
  const challenge = await gerarChallenge(verifier);
  const state = gerarState();

  sessionStorage.setItem(SESSION_KEY_VERIFIER, verifier);
  sessionStorage.setItem(SESSION_KEY_STATE, state);

  const params = new URLSearchParams({
    response_type: 'code',
    client_id: cfg.client_id,
    redirect_uri: cfg.redirect_uri,
    scope: cfg.scopes || 'openid profile email',
    state,
    code_challenge: challenge,
    code_challenge_method: 'S256',
  });

  return `${cfg.base_url}/protocol/openid-connect/auth?${params.toString()}`;
}
