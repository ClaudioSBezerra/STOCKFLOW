import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  abrirEmpresaPadrao,
  buscarEmpresaPadrao,
  CHAVE_MFA_PENDENTE,
  concluirEscolha,
  decidirEntrada,
  entrarPelaConta,
  escolherApp,
  gravarMfaPendente,
  lerMfaPendente,
  pedirRedefinicaoPelaConta,
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

  it('pedirRedefinicaoPelaConta chama POST /api/auth/esqueci-senha sem prefixo (Story 15.3)', async () => {
    window.history.pushState({}, '', '/e/outra/login');
    fetchMock.mockImplementationOnce(() =>
      resposta(202, { mensagem: 'Se o e-mail existir, você receberá um link.' }),
    );
    expect(await pedirRedefinicaoPelaConta('a@b.c')).toBe(true);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/auth/esqueci-senha');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ email: 'a@b.c' });
    window.history.pushState({}, '', '/');

    fetchMock.mockImplementationOnce(() => resposta(500, { error: { code: 'INTERNAL_ERROR' } }));
    expect(await pedirRedefinicaoPelaConta('a@b.c')).toBe(false);

    fetchMock.mockImplementationOnce(() => Promise.reject(new Error('rede')));
    expect(await pedirRedefinicaoPelaConta('a@b.c')).toBe(false);
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

describe('Empresa padrão (Story 15.4)', () => {
  const fetchMock = vi.fn();
  const replaceMock = vi.fn();

  beforeEach(() => {
    fetchMock.mockReset();
    replaceMock.mockReset();
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('location', { ...window.location, replace: replaceMock });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('chama /api/entrada sem prefixo de Empresa e devolve o slug', async () => {
    fetchMock.mockImplementation(() => resposta(200, { empresaPadrao: 'acme' }));

    expect(await buscarEmpresaPadrao()).toBe('acme');
    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/entrada');
  });

  const casosNull: Array<[string, () => Promise<unknown>]> = [
    ['empresaPadrao null', () => resposta(200, { empresaPadrao: null })],
    ['JSON sem o campo', () => resposta(200, {})],
    ['corpo null', () => resposta(200, null)],
    ['500', () => resposta(500, { error: { code: 'INTERNAL_ERROR' } })],
    ['erro de rede', () => Promise.reject(new TypeError('Failed to fetch'))],
    [
      'JSON inválido',
      () =>
        Promise.resolve({
          ok: true,
          status: 200,
          json: async () => {
            throw new SyntaxError('Unexpected token');
          },
        }),
    ],
    ['slug não canônico', () => resposta(200, { empresaPadrao: 'ACME/../x' })],
    ['slug com barra', () => resposta(200, { empresaPadrao: 'acme/admin' })],
    ['slug de outro tipo', () => resposta(200, { empresaPadrao: 42 })],
    ['slug vazio', () => resposta(200, { empresaPadrao: '' })],
  ];

  it.each(casosNull)('%s -> null', async (_nome, impl) => {
    fetchMock.mockImplementation(impl);
    expect(await buscarEmpresaPadrao()).toBeNull();
  });

  it('timeout de 3 s -> null', async () => {
    vi.useFakeTimers();
    fetchMock.mockImplementation(
      (_url: string, init: RequestInit) =>
        new Promise((_resolve, reject) => {
          init.signal?.addEventListener('abort', () =>
            reject(new DOMException('aborted', 'AbortError')),
          );
        }),
    );

    const promessa = buscarEmpresaPadrao();
    await vi.advanceTimersByTimeAsync(3000);
    expect(await promessa).toBeNull();
  });

  it('abrirEmpresaPadrao faz replace para /e/{slug}/ só com slug', async () => {
    fetchMock.mockImplementation(() => resposta(200, { empresaPadrao: 'acme' }));
    expect(await abrirEmpresaPadrao()).toBe(true);
    expect(replaceMock).toHaveBeenCalledExactlyOnceWith('/e/acme/');

    replaceMock.mockReset();
    fetchMock.mockImplementation(() => resposta(200, { empresaPadrao: null }));
    expect(await abrirEmpresaPadrao()).toBe(false);
    expect(replaceMock).not.toHaveBeenCalled();

    fetchMock.mockImplementation(() => resposta(500, {}));
    expect(await abrirEmpresaPadrao()).toBe(false);
    expect(replaceMock).not.toHaveBeenCalled();
  });

  it('fora da raiz, leva o caminho, a query e o hash para dentro da Empresa', async () => {
    fetchMock.mockImplementation(() => resposta(200, { empresaPadrao: 'acme' }));
    vi.stubGlobal('location', {
      pathname: '/auth/callback',
      search: '?code=abc&state=xyz',
      hash: '#fim',
      replace: replaceMock,
    });
    expect(await abrirEmpresaPadrao()).toBe(true);
    expect(replaceMock).toHaveBeenCalledExactlyOnceWith('/e/acme/auth/callback?code=abc&state=xyz#fim');

    replaceMock.mockReset();
    vi.stubGlobal('location', { pathname: '/login', search: '', hash: '', replace: replaceMock });
    expect(await abrirEmpresaPadrao()).toBe(true);
    expect(replaceMock).toHaveBeenCalledExactlyOnceWith('/e/acme/login');
  });

  describe('decidirEntrada', () => {
    it('sem-empresa com Empresa padrão -> redirecionado (nada é montado)', async () => {
      fetchMock.mockImplementation(() => resposta(200, { empresaPadrao: 'acme' }));
      expect(await decidirEntrada('/')).toBe('redirecionado');
      expect(replaceMock).toHaveBeenCalledExactlyOnceWith('/e/acme/');
    });

    it('sem-empresa sem Empresa padrão, ou com falha -> sem-empresa, sem replace', async () => {
      fetchMock.mockImplementation(() => resposta(200, { empresaPadrao: null }));
      expect(await decidirEntrada('/')).toBe('sem-empresa');
      fetchMock.mockImplementation(() => Promise.reject(new TypeError('Failed to fetch')));
      expect(await decidirEntrada('/qualquer')).toBe('sem-empresa');
      expect(replaceMock).not.toHaveBeenCalled();
    });

    it('Empresa e Plataforma nunca consultam /api/entrada', async () => {
      fetchMock.mockImplementation(() => resposta(200, { empresaPadrao: 'acme' }));
      expect(await decidirEntrada('/e/outra/pedidos')).toBe('empresa');
      expect(await decidirEntrada('/plataforma/login')).toBe('plataforma');
      expect(fetchMock).not.toHaveBeenCalled();
      expect(replaceMock).not.toHaveBeenCalled();
    });
  });
});
