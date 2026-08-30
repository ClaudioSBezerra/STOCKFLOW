import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest';
import { fetchSSOConfig, resetSSOConfigCache } from './config';

describe('fetchSSOConfig', () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    resetSSOConfigCache();
    fetchMock.mockReset();
    vi.stubGlobal('fetch', fetchMock);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('devolve o corpo em caso de sucesso e cacheia (uma única chamada de rede)', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ enabled: true, base_url: 'https://kc.example/realms/x', client_id: 'w' }),
    });

    const a = await fetchSSOConfig();
    const b = await fetchSSOConfig();

    expect(a.enabled).toBe(true);
    expect(b).toEqual(a);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('!res.ok -> {enabled:false} e NÃO cacheia (próxima chamada tenta de novo)', async () => {
    fetchMock.mockResolvedValueOnce({ ok: false, status: 500, json: async () => ({}) });
    fetchMock.mockResolvedValueOnce({ ok: true, json: async () => ({ enabled: true }) });

    expect(await fetchSSOConfig()).toEqual({ enabled: false });
    expect(await fetchSSOConfig()).toEqual({ enabled: true });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('rejeição de rede -> {enabled:false} sem lançar', async () => {
    fetchMock.mockRejectedValue(new Error('offline'));
    await expect(fetchSSOConfig()).resolves.toEqual({ enabled: false });
  });
});
