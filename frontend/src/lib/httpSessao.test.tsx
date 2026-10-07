import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { instalarInterceptorDeRenovacaoDeSessao } from './httpSessao';
import { clearAccessToken, getAccessToken, setAccessToken } from './session';

// Resposta mock simples — nenhum teste aqui precisa de `.clone()` (o
// interceptor só olha `res.status`, nunca lê o corpo do 401).
function resposta(status: number, corpo: unknown = {}) {
  return { ok: status >= 200 && status < 300, status, json: async () => corpo };
}

describe('httpSessao — interceptor global de renovação de sessão (feedback real, 2026-10-07)', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    clearAccessToken();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    clearAccessToken();
  });

  it('401 numa chamada autenticada: renova pelo cookie e repete com o token novo', async () => {
    setAccessToken('token-velho');
    fetchMock
      .mockResolvedValueOnce(resposta(401)) // GET /api/produtos — token de 30min vencido
      .mockResolvedValueOnce(resposta(200, { token: 'token-novo' })) // POST /api/auth/refresh
      .mockResolvedValueOnce(resposta(200, { produtos: [] })); // repetição com o token novo

    instalarInterceptorDeRenovacaoDeSessao();
    const res = await fetch('/e/empresa/api/produtos', { headers: { Authorization: 'Bearer token-velho' } });

    expect(res.status).toBe(200);
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(fetchMock.mock.calls[1][0]).toBe('/api/auth/refresh');
    expect(fetchMock.mock.calls[2]).toEqual([
      '/e/empresa/api/produtos',
      { headers: { Authorization: 'Bearer token-novo' } },
    ]);
    expect(getAccessToken()).toBe('token-novo');
  });

  it('renovação falha (sessão realmente vencida, ex. 2h de inatividade): limpa o token e devolve o 401 original', async () => {
    setAccessToken('token-velho');
    fetchMock
      .mockResolvedValueOnce(resposta(401))
      .mockResolvedValueOnce(resposta(401)); // refresh também recusa — cookie expirado de vez

    instalarInterceptorDeRenovacaoDeSessao();
    const res = await fetch('/e/empresa/api/produtos', { headers: { Authorization: 'Bearer token-velho' } });

    expect(res.status).toBe(401);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(getAccessToken()).toBeNull();
  });

  it('401 sem Authorization (login/cadastro público): nunca tenta renovar — é credencial errada, não sessão vencida', async () => {
    fetchMock.mockResolvedValueOnce(resposta(401, { error: { code: 'CREDENCIAIS_INVALIDAS' } }));

    instalarInterceptorDeRenovacaoDeSessao();
    const res = await fetch('/api/auth/login', { method: 'POST', body: '{}' });

    expect(res.status).toBe(401);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('401 em /api/plataforma/...: nunca intercepta — aquele domínio resolve o próprio refresh (lib/plataforma.ts)', async () => {
    fetchMock.mockResolvedValueOnce(resposta(401));

    instalarInterceptorDeRenovacaoDeSessao();
    const res = await fetch('/api/plataforma/empresas', { headers: { Authorization: 'Bearer token-do-dono' } });

    expect(res.status).toBe(401);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('duas chamadas vencidas ao mesmo tempo compartilham UMA renovação (nunca duas rodando em paralelo)', async () => {
    setAccessToken('token-velho');
    let resolverRefresh: (v: unknown) => void = () => {};
    const refreshPendente = new Promise((r) => {
      resolverRefresh = r;
    });
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') return refreshPendente;
      if (url === '/api/produtos') return Promise.resolve(resposta(401));
      return Promise.resolve(resposta(200, {}));
    });

    instalarInterceptorDeRenovacaoDeSessao();
    const p1 = fetch('/api/produtos', { headers: { Authorization: 'Bearer token-velho' } });
    const p2 = fetch('/api/produtos', { headers: { Authorization: 'Bearer token-velho' } });

    await new Promise((r) => setTimeout(r, 0)); // deixa as duas chegarem no 401 antes do refresh resolver
    resolverRefresh(resposta(200, { token: 'token-novo' }));
    await Promise.all([p1, p2]);

    const chamadasDeRefresh = fetchMock.mock.calls.filter(([url]) => url === '/api/auth/refresh');
    expect(chamadasDeRefresh).toHaveLength(1);
  });

  it('instalar duas vezes não empilha o wrapper (idempotente contra o fetch atual)', async () => {
    fetchMock.mockResolvedValueOnce(resposta(200, {}));
    instalarInterceptorDeRenovacaoDeSessao();
    instalarInterceptorDeRenovacaoDeSessao();
    await fetch('/api/produtos');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
