import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import {
  alterarExigenciaMFA,
  listarAuditoriaSeguranca,
  MENSAGEM_ERRO_ALTERAR_EXIGENCIA,
  MENSAGEM_ERRO_LISTAR_AUDITORIA,
  MENSAGEM_ERRO_OBTER_EXIGENCIA,
  obterExigenciaMFA,
} from './segurancaEmpresa';

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

function naoOk(body: unknown = {}) {
  return { ok: false, status: 500, json: async () => body };
}

describe('obterExigenciaMFA', () => {
  it('GET /api/seguranca/mfa-empresa com Authorization', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ mfaObrigatorio: true, contasSemMfa: 3 }) });

    await expect(obterExigenciaMFA()).resolves.toEqual({ mfaObrigatorio: true, contasSemMfa: 3 });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/seguranca/mfa-empresa');
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init.headers).toMatchObject({ Authorization: 'Bearer token-de-teste' });
  });

  it('lança o fallback numa resposta não-ok sem mensagem', async () => {
    fetchMock.mockResolvedValue(naoOk());
    await expect(obterExigenciaMFA()).rejects.toThrow(MENSAGEM_ERRO_OBTER_EXIGENCIA);
  });
});

describe('alterarExigenciaMFA', () => {
  it('PUT com {mfaObrigatorio} em JSON e devolve o resultado', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ mfaObrigatorio: true, alterado: true }) });

    await expect(alterarExigenciaMFA(true)).resolves.toEqual({ mfaObrigatorio: true, alterado: true });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/seguranca/mfa-empresa');
    expect(init.method).toBe('PUT');
    expect(init.headers).toMatchObject({
      Authorization: 'Bearer token-de-teste',
      'Content-Type': 'application/json',
    });
    expect(JSON.parse(init.body)).toEqual({ mfaObrigatorio: true });
  });

  it('propaga a mensagem do servidor', async () => {
    fetchMock.mockResolvedValue(naoOk({ error: { code: 'MFA_SETUP_REQUIRED', message: 'configure o MFA' } }));
    await expect(alterarExigenciaMFA(false)).rejects.toThrow('configure o MFA');
  });

  it('cai no fallback sem mensagem', async () => {
    fetchMock.mockResolvedValue(naoOk());
    await expect(alterarExigenciaMFA(false)).rejects.toThrow(MENSAGEM_ERRO_ALTERAR_EXIGENCIA);
  });
});

describe('listarAuditoriaSeguranca', () => {
  it('GET /api/seguranca/auditoria e devolve os eventos', async () => {
    const eventos = [
      {
        id: 'e1',
        acao: 'exigencia_alterada',
        atorId: 'a1',
        atorNome: 'Adm',
        alvoId: null,
        alvoNome: null,
        detalhe: { anterior: false, novo: true },
        criadoEm: '2026-09-24T10:00:00Z',
      },
    ];
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ eventos }) });

    await expect(listarAuditoriaSeguranca()).resolves.toEqual(eventos);
    expect(fetchMock.mock.calls[0][0]).toBe('/api/seguranca/auditoria');
  });

  it('lança o fallback numa resposta não-ok', async () => {
    fetchMock.mockResolvedValue(naoOk());
    await expect(listarAuditoriaSeguranca()).rejects.toThrow(MENSAGEM_ERRO_LISTAR_AUDITORIA);
  });
});
