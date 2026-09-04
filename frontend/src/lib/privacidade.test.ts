import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { baixarMeusDadosBlob, MENSAGEM_ERRO_EXPORTAR } from './privacidade';

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

const fetchMock = vi.fn();

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock);
  fetchMock.mockReset();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('baixarMeusDadosBlob', () => {
  it('GET /api/usuarios/me/exportar-dados com Authorization e devolve o Blob do corpo', async () => {
    const blob = new Blob(['{"nome":"Ana"}'], { type: 'application/json' });
    fetchMock.mockResolvedValue({ ok: true, blob: async () => blob });

    const resultado = await baixarMeusDadosBlob();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/usuarios/me/exportar-dados');
    // A lib omite `method`, contando com o default GET do fetch; a rota é
    // registrada método-específica (`GET /api/usuarios/me/exportar-dados`),
    // então uma regressão para POST daria 405.
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init.headers).toMatchObject({ Authorization: 'Bearer token-de-teste' });
    expect(resultado).toBe(blob);
  });

  it('propaga a mensagem de erro do servidor numa resposta não-ok', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({
        error: { code: 'INTERNAL_ERROR', message: 'falha ao exportar dados pessoais' },
      }),
    });

    await expect(baixarMeusDadosBlob()).rejects.toThrow('falha ao exportar dados pessoais');
  });

  it('cai no fallback MENSAGEM_ERRO_EXPORTAR quando a resposta não-ok não traz error.message', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({}),
    });

    await expect(baixarMeusDadosBlob()).rejects.toThrow(MENSAGEM_ERRO_EXPORTAR);
  });

  it('cai no fallback MENSAGEM_ERRO_EXPORTAR quando o corpo da resposta não-ok não é JSON', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 502,
      json: async () => {
        throw new SyntaxError('Unexpected token < in JSON');
      },
    });

    await expect(baixarMeusDadosBlob()).rejects.toThrow(MENSAGEM_ERRO_EXPORTAR);
  });
});
