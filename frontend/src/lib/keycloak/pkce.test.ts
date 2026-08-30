import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest';
import {
  buildLoginUrl,
  gerarChallenge,
  gerarState,
  gerarVerifier,
  SESSION_KEY_STATE,
  SESSION_KEY_VERIFIER,
} from './pkce';
import type { SSOConfig } from './config';

describe('pkce', () => {
  beforeEach(() => {
    sessionStorage.clear();
  });
  afterEach(() => {
    sessionStorage.clear();
    vi.restoreAllMocks();
  });

  it('gerarChallenge produz o S256 determinístico de um verifier fixo', async () => {
    // Vetor conhecido da RFC 7636 (Appendix B).
    const verifier = 'dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk';
    const challenge = await gerarChallenge(verifier);
    expect(challenge).toBe('E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM');
  });

  it('gerarVerifier devolve base64url sem padding, ~64 chars', () => {
    const v = gerarVerifier();
    expect(v).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(v.length).toBe(64);
  });

  it('gerarState devolve um UUID v4', () => {
    expect(gerarState()).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
  });

  it('buildLoginUrl monta a URL de authorize e persiste verifier+state', async () => {
    const cfg: SSOConfig = {
      enabled: true,
      base_url: 'https://kc.example/realms/ferreiracosta',
      client_id: 'stockflow-web',
      redirect_uri: 'http://localhost/auth/callback',
      scopes: 'openid profile email',
    };
    const url = await buildLoginUrl(cfg);

    expect(url.startsWith('https://kc.example/realms/ferreiracosta/protocol/openid-connect/auth?')).toBe(
      true,
    );
    const params = new URL(url).searchParams;
    expect(params.get('response_type')).toBe('code');
    expect(params.get('client_id')).toBe('stockflow-web');
    expect(params.get('redirect_uri')).toBe('http://localhost/auth/callback');
    expect(params.get('scope')).toBe('openid profile email');
    expect(params.get('code_challenge_method')).toBe('S256');
    expect(params.get('code_challenge')).toBeTruthy();
    expect(params.get('state')).toBe(sessionStorage.getItem(SESSION_KEY_STATE));

    const verifier = sessionStorage.getItem(SESSION_KEY_VERIFIER);
    expect(verifier).toBeTruthy();
    // o challenge guardado bate com o SHA-256 do verifier persistido
    expect(params.get('code_challenge')).toBe(await gerarChallenge(verifier as string));
  });

  it('buildLoginUrl lança quando a config não está habilitada', async () => {
    await expect(buildLoginUrl({ enabled: false })).rejects.toThrow();
    await expect(
      buildLoginUrl({ enabled: true, base_url: 'x' } as SSOConfig),
    ).rejects.toThrow();
  });
});
