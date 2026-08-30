import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest';
import { ErroCallbackSSO, trocarCodePorSessao } from './callback';
import { resetSSOConfigCache, type SSOConfig } from './config';
import { SESSION_KEY_STATE, SESSION_KEY_VERIFIER } from './pkce';

const cfgOk: SSOConfig = {
  enabled: true,
  base_url: 'https://kc.example/realms/ferreiracosta',
  client_id: 'stockflow-web',
  redirect_uri: 'http://localhost/auth/callback',
};

describe('trocarCodePorSessao', () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    resetSSOConfigCache();
    sessionStorage.clear();
    fetchMock.mockReset();
    vi.stubGlobal('fetch', fetchMock);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    sessionStorage.clear();
  });

  it('fluxo feliz: troca code->token->sessão e devolve {token, usuario}', async () => {
    sessionStorage.setItem(SESSION_KEY_STATE, 'st-123');
    sessionStorage.setItem(SESSION_KEY_VERIFIER, 'ver-abc');

    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/sso/config') {
        return Promise.resolve({ ok: true, json: async () => cfgOk });
      }
      if (url === 'https://kc.example/realms/ferreiracosta/protocol/openid-connect/token') {
        return Promise.resolve({ ok: true, json: async () => ({ access_token: 'kc-access' }) });
      }
      if (url === '/api/auth/sso/keycloak') {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            token: 'sf-token',
            usuario: { id: '1', nome: 'C', email: 'c@fc.com', papel: 'gestor' },
          }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    const out = await trocarCodePorSessao(new URLSearchParams({ code: 'c1', state: 'st-123' }));
    expect(out.token).toBe('sf-token');
    expect(out.usuario.papel).toBe('gestor');

    // o backend foi chamado com o Bearer do access token do Keycloak
    const call = fetchMock.mock.calls.find(([u]) => u === '/api/auth/sso/keycloak');
    expect(call?.[1]?.headers?.Authorization).toBe('Bearer kc-access');
    // verifier/state foram consumidos
    expect(sessionStorage.getItem(SESSION_KEY_STATE)).toBeNull();
    expect(sessionStorage.getItem(SESSION_KEY_VERIFIER)).toBeNull();
  });

  it('state divergente -> ErroCallbackSSO("CSRF"), sem nenhuma chamada de rede', async () => {
    sessionStorage.setItem(SESSION_KEY_STATE, 'outro');
    sessionStorage.setItem(SESSION_KEY_VERIFIER, 'ver-abc');

    await expect(trocarCodePorSessao(new URLSearchParams({ code: 'c1', state: 'st-123' }))).rejects.toMatchObject({
      codigo: 'CSRF',
    });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('state ausente no sessionStorage -> CSRF', async () => {
    await expect(trocarCodePorSessao(new URLSearchParams({ code: 'c1', state: 'st-123' }))).rejects.toBeInstanceOf(
      ErroCallbackSSO,
    );
  });

  it('sem code/state nos parâmetros -> PARAMS_AUSENTES', async () => {
    await expect(trocarCodePorSessao(new URLSearchParams({}))).rejects.toMatchObject({ codigo: 'PARAMS_AUSENTES' });
  });

  it('erro do Keycloak na query -> KEYCLOAK_ERRO', async () => {
    await expect(
      trocarCodePorSessao(new URLSearchParams({ error: 'access_denied' })),
    ).rejects.toMatchObject({ codigo: 'KEYCLOAK_ERRO' });
  });

  it('backend 401 SSO_SEM_CONTA -> propaga codigo === "SSO_SEM_CONTA"', async () => {
    sessionStorage.setItem(SESSION_KEY_STATE, 'st-123');
    sessionStorage.setItem(SESSION_KEY_VERIFIER, 'ver-abc');
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/sso/config') {
        return Promise.resolve({ ok: true, json: async () => cfgOk });
      }
      if (url.endsWith('/protocol/openid-connect/token')) {
        return Promise.resolve({ ok: true, json: async () => ({ access_token: 'kc-access' }) });
      }
      if (url === '/api/auth/sso/keycloak') {
        return Promise.resolve({
          ok: false,
          status: 401,
          json: async () => ({ error: { code: 'SSO_SEM_CONTA', message: 'x' } }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    await expect(
      trocarCodePorSessao(new URLSearchParams({ code: 'c1', state: 'st-123' })),
    ).rejects.toMatchObject({ codigo: 'SSO_SEM_CONTA' });
  });

  it('token endpoint do Keycloak falha -> TROCA_TOKEN', async () => {
    sessionStorage.setItem(SESSION_KEY_STATE, 'st-123');
    sessionStorage.setItem(SESSION_KEY_VERIFIER, 'ver-abc');
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/sso/config') {
        return Promise.resolve({ ok: true, json: async () => cfgOk });
      }
      if (url.endsWith('/protocol/openid-connect/token')) {
        return Promise.resolve({ ok: false, status: 400, json: async () => ({}) });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    await expect(
      trocarCodePorSessao(new URLSearchParams({ code: 'c1', state: 'st-123' })),
    ).rejects.toMatchObject({ codigo: 'TROCA_TOKEN' });
  });
});
