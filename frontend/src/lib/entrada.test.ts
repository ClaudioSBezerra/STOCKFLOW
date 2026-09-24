import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  CHAVE_MFA_PENDENTE,
  concluirEscolha,
  entrarPelaConta,
  escolherApp,
  gravarMfaPendente,
  lerMfaPendente,
  removerMfaPendente,
  type AppDeEntrada,
} from './entrada';

describe('escolherApp (Story 9.2)', () => {
  const casos: Array<[string, AppDeEntrada]> = [
    ['/plataforma', 'plataforma'],
    ['/plataforma/', 'plataforma'],
    ['/plataforma/login', 'plataforma'],
    ['/plataformas', 'sem-empresa'],
    ['/e/acme', 'empresa'],
    ['/e/acme/pedidos', 'empresa'],
    ['/e/acme-treinamento/login', 'empresa'],
    ['/', 'sem-empresa'],
    ['/login', 'sem-empresa'],
    ['/e/', 'sem-empresa'],
    ['/e/Acme/login', 'sem-empresa'],
  ];

  it.each(casos)('escolherApp(%j) -> %j', (pathname, esperado) => {
    expect(escolherApp(pathname)).toBe(esperado);
  });
});

function resposta(status: number, body: unknown) {
  return Promise.resolve({ ok: status >= 200 && status < 300, status, json: async () => body });
}

describe('login pela conta (Story 15.2)', () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal('fetch', fetchMock);
    window.sessionStorage.clear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    window.sessionStorage.clear();
  });

  it('entrarPelaConta chama /api/auth/entrar sem prefixo de Empresa, mesmo sob /e/{slug}', async () => {
    window.history.pushState({}, '', '/e/outra/login');
    fetchMock.mockImplementation(() => resposta(200, { slug: 'acme' }));

    expect(await entrarPelaConta('a@b.c', 's')).toEqual({ tipo: 'sessao', slug: 'acme' });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/auth/entrar');
    expect(JSON.parse(init.body as string)).toEqual({ email: 'a@b.c', senha: 's' });
    window.history.pushState({}, '', '/');
  });

  it('interpreta MFA, escolha e erro', async () => {
    fetchMock.mockImplementationOnce(() => resposta(200, { slug: 'acme', mfaRequerido: true, mfaToken: 't' }));
    expect(await entrarPelaConta('a@b.c', 's')).toEqual({ tipo: 'mfa', slug: 'acme', mfaToken: 't' });

    const escolha = [{ slug: 'acme', nomeFantasia: 'Acme', treinamento: false }];
    fetchMock.mockImplementationOnce(() => resposta(200, { escolha, escolhaToken: 'e' }));
    expect(await entrarPelaConta('a@b.c', 's')).toEqual({ tipo: 'escolha', escolha, escolhaToken: 'e' });

    fetchMock.mockImplementationOnce(() => resposta(429, { error: { code: 'ACCOUNT_LOCKED' } }));
    expect(await entrarPelaConta('a@b.c', 's')).toEqual({ tipo: 'erro', codigo: 'ACCOUNT_LOCKED' });

    fetchMock.mockImplementationOnce(() => Promise.reject(new Error('rede')));
    expect(await entrarPelaConta('a@b.c', 's')).toEqual({ tipo: 'erro' });
  });

  it('concluirEscolha chama /api/auth/entrar/escolha', async () => {
    fetchMock.mockImplementation(() => resposta(401, { error: { code: 'ESCOLHA_INVALIDA' } }));

    expect(await concluirEscolha('tok', 'acme')).toEqual({ tipo: 'erro', codigo: 'ESCOLHA_INVALIDA' });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/auth/entrar/escolha');
    expect(JSON.parse(init.body as string)).toEqual({ escolhaToken: 'tok', slug: 'acme' });
  });

  it('repasse do MFA só vale para o mesmo slug', () => {
    gravarMfaPendente('acme', 'mfa-tok');
    expect(JSON.parse(window.sessionStorage.getItem(CHAVE_MFA_PENDENTE) ?? 'null')).toEqual({
      slug: 'acme',
      mfaToken: 'mfa-tok',
    });
    expect(lerMfaPendente('acme')).toBe('mfa-tok');
    expect(lerMfaPendente('outra')).toBe('');
    expect(lerMfaPendente('')).toBe('');

    removerMfaPendente();
    expect(lerMfaPendente('acme')).toBe('');

    window.sessionStorage.setItem(CHAVE_MFA_PENDENTE, '{corrompido');
    expect(lerMfaPendente('acme')).toBe('');
  });
});
