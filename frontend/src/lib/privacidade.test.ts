import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import {
  baixarMeusDadosBlob,
  listarSolicitacoesExclusao,
  MENSAGEM_ERRO_EXPORTAR,
  MENSAGEM_ERRO_LISTAR_EXCLUSAO,
  MENSAGEM_ERRO_PROCESSAR_EXCLUSAO,
  MENSAGEM_ERRO_SOLICITAR_EXCLUSAO,
  processarExclusaoConta,
  solicitarExclusaoConta,
} from './privacidade';

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

describe('solicitarExclusaoConta', () => {
  it('POST /api/usuarios/me/solicitacao-exclusao com Authorization, sem corpo', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({}) });

    await solicitarExclusaoConta();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/usuarios/me/solicitacao-exclusao');
    expect(init.method).toBe('POST');
    expect(init.headers).toMatchObject({ Authorization: 'Bearer token-de-teste' });
    expect(init.body).toBeUndefined();
  });

  it('propaga a mensagem do servidor num 409', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({
        error: { code: 'CONFLICT', message: 'já existe uma solicitação de exclusão pendente para a sua conta' },
      }),
    });

    await expect(solicitarExclusaoConta()).rejects.toThrow(
      'já existe uma solicitação de exclusão pendente para a sua conta',
    );
  });

  it('cai no fallback MENSAGEM_ERRO_SOLICITAR_EXCLUSAO quando o não-ok não traz error.message', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500, json: async () => ({}) });

    await expect(solicitarExclusaoConta()).rejects.toThrow(MENSAGEM_ERRO_SOLICITAR_EXCLUSAO);
  });
});

describe('listarSolicitacoesExclusao', () => {
  it('GET /api/solicitacoes-exclusao com Authorization e devolve o array de solicitacoes', async () => {
    const solicitacoes = [
      { id: 's1', nome: 'Ana', email: 'ana@empresa.com', papel: 'usuario', criadoEm: '2026-09-04T10:00:00Z' },
    ];
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ solicitacoes }) });

    const resultado = await listarSolicitacoesExclusao();

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/solicitacoes-exclusao');
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init.headers).toMatchObject({ Authorization: 'Bearer token-de-teste' });
    expect(resultado).toEqual(solicitacoes);
  });

  it('devolve [] quando o corpo não traz solicitacoes', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({}) });
    await expect(listarSolicitacoesExclusao()).resolves.toEqual([]);
  });

  it('propaga a mensagem do servidor / cai no fallback numa resposta não-ok', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 403,
      json: async () => ({ error: { message: 'papel insuficiente' } }),
    });
    await expect(listarSolicitacoesExclusao()).rejects.toThrow('papel insuficiente');

    fetchMock.mockResolvedValue({ ok: false, status: 500, json: async () => ({}) });
    await expect(listarSolicitacoesExclusao()).rejects.toThrow(MENSAGEM_ERRO_LISTAR_EXCLUSAO);
  });
});

describe('processarExclusaoConta', () => {
  it('POST /api/solicitacoes-exclusao/{id}/processamento com Authorization, sem corpo', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({}) });

    await processarExclusaoConta('sol-123');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/solicitacoes-exclusao/sol-123/processamento');
    expect(init.method).toBe('POST');
    expect(init.headers).toMatchObject({ Authorization: 'Bearer token-de-teste' });
    expect(init.body).toBeUndefined();
  });

  it('propaga a mensagem do servidor no 409 do guard do último administrador', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({
        error: { code: 'CONFLICT', message: 'ao menos um administrador ativo deve sempre existir' },
      }),
    });

    await expect(processarExclusaoConta('sol-1')).rejects.toThrow(
      'ao menos um administrador ativo deve sempre existir',
    );
  });

  it('cai no fallback MENSAGEM_ERRO_PROCESSAR_EXCLUSAO quando o não-ok não traz error.message', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500, json: async () => ({}) });

    await expect(processarExclusaoConta('sol-1')).rejects.toThrow(MENSAGEM_ERRO_PROCESSAR_EXCLUSAO);
  });
});
