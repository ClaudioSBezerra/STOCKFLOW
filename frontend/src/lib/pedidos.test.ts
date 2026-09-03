import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { buscarPedido, listarPedidos } from './pedidos';

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

function respostaOk(body: unknown) {
  return Promise.resolve({ ok: true, json: async () => body });
}

const RESUMO = {
  id: 'p-1',
  usuarioId: 'u-1',
  solicitante: 'Fulano',
  obraCentroCusto: 'Obra Norte',
  observacao: null,
  status: 'pendente',
  criadoEm: '2026-09-01T10:00:00Z',
  qtdItens: 2,
};

describe('listarPedidos', () => {
  it('GET /api/pedidos sem ?status= quando nenhum filtro é passado, com Authorization', async () => {
    fetchMock.mockReturnValue(respostaOk({ pedidos: [RESUMO] }));

    const pedidos = await listarPedidos();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/pedidos');
    expect(init.headers).toMatchObject({ Authorization: 'Bearer token-de-teste' });
    expect(pedidos).toEqual([RESUMO]);
  });

  it('acrescenta ?status= quando um filtro é passado', async () => {
    fetchMock.mockReturnValue(respostaOk({ pedidos: [] }));

    await listarPedidos('aprovado');

    expect(fetchMock.mock.calls[0][0]).toBe('/api/pedidos?status=aprovado');
  });

  it('devolve [] quando o corpo não traz pedidos', async () => {
    fetchMock.mockReturnValue(respostaOk({}));
    expect(await listarPedidos()).toEqual([]);
  });

  it('propaga a mensagem de erro do servidor numa resposta não-ok', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({ error: { code: 'VALIDATION_ERROR', message: 'status inválido' } }),
    });

    await expect(listarPedidos('banana' as never)).rejects.toThrow('status inválido');
  });
});

describe('buscarPedido', () => {
  it('GET /api/pedidos/{id} com Authorization e devolve o pedido do corpo', async () => {
    const detalhe = { ...RESUMO, itens: [] };
    fetchMock.mockReturnValue(respostaOk({ pedido: detalhe }));

    const pedido = await buscarPedido('p-1');

    expect(fetchMock.mock.calls[0][0]).toBe('/api/pedidos/p-1');
    expect(fetchMock.mock.calls[0][1].headers).toMatchObject({
      Authorization: 'Bearer token-de-teste',
    });
    expect(pedido).toEqual(detalhe);
  });

  it('propaga a mensagem de erro do servidor num 404', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 404,
      json: async () => ({ error: { code: 'NOT_FOUND', message: 'pedido não encontrado' } }),
    });

    await expect(buscarPedido('p-x')).rejects.toThrow('pedido não encontrado');
  });
});
