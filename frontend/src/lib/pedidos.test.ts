import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { buscarPedido, decidirPedido, listarFilaPedidos, listarPedidos } from './pedidos';

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

describe('listarFilaPedidos', () => {
  it('GET /api/pedidos?escopo=todos sem status quando nenhum filtro é passado, com Authorization', async () => {
    fetchMock.mockReturnValue(respostaOk({ pedidos: [RESUMO] }));

    const pedidos = await listarFilaPedidos();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/pedidos?escopo=todos');
    expect(init.headers).toMatchObject({ Authorization: 'Bearer token-de-teste' });
    expect(pedidos).toEqual([RESUMO]);
  });

  it('acrescenta &status= quando um filtro é passado, sempre incluindo escopo=todos', async () => {
    fetchMock.mockReturnValue(respostaOk({ pedidos: [] }));

    await listarFilaPedidos('aprovado');

    expect(fetchMock.mock.calls[0][0]).toBe('/api/pedidos?escopo=todos&status=aprovado');
  });

  it('devolve [] quando o corpo não traz pedidos', async () => {
    fetchMock.mockReturnValue(respostaOk({}));
    expect(await listarFilaPedidos()).toEqual([]);
  });

  it('propaga a mensagem de erro do servidor numa resposta não-ok', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({ error: { code: 'VALIDATION_ERROR', message: 'status inválido' } }),
    });

    await expect(listarFilaPedidos('banana' as never)).rejects.toThrow('status inválido');
  });

  // Code review: listarFilaPedidos tinha herdado por engano o fallback de
  // "Meus Pedidos" (MENSAGEM_ERRO_LISTAR) quando o corpo de uma resposta
  // não-ok não traz `error.message` — este teste exercita EXATAMENTE esse
  // caminho (corpo sem `error.message`) para provar que a mensagem
  // devolvida é específica da Fila, não a de "Meus Pedidos".
  it('cai no fallback específico da Fila quando a resposta não-ok não traz error.message', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({}),
    });

    await expect(listarFilaPedidos()).rejects.toThrow(
      'Não foi possível carregar a fila de pedidos agora. Tente novamente em instantes.',
    );
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

describe('decidirPedido', () => {
  it('POST /api/pedidos/{id}/decisao com {"aprovar":true}, Authorization e Content-Type, devolve o pedido do corpo', async () => {
    const decidido = { ...RESUMO, status: 'aprovado', itens: [] };
    fetchMock.mockReturnValue(respostaOk({ pedido: decidido }));

    const pedido = await decidirPedido('p-1', true);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/pedidos/p-1/decisao');
    expect(init.method).toBe('POST');
    expect(init.headers).toMatchObject({
      'Content-Type': 'application/json',
      Authorization: 'Bearer token-de-teste',
    });
    expect(JSON.parse(init.body)).toEqual({ aprovar: true });
    expect(pedido).toEqual(decidido);
  });

  it('envia {"aprovar":false} para rejeição', async () => {
    fetchMock.mockReturnValue(respostaOk({ pedido: { ...RESUMO, status: 'rejeitado', itens: [] } }));

    await decidirPedido('p-1', false);

    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body)).toEqual({ aprovar: false });
  });

  it('propaga a mensagem de erro do servidor num 409 (pedido já decidido)', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({ error: { code: 'CONFLICT', message: 'este pedido não está mais pendente' } }),
    });

    await expect(decidirPedido('p-1', true)).rejects.toThrow('este pedido não está mais pendente');
  });

  it('cai no fallback específico da decisão quando a resposta não-ok não traz error.message', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({}),
    });

    await expect(decidirPedido('p-1', true)).rejects.toThrow(
      'Não foi possível registrar a decisão agora. Tente novamente em instantes.',
    );
  });
});
