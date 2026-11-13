import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import {
  baixarPreNotaPDF,
  buscarPedido,
  buscarReciboPedidoBlob,
  decidirLotePedidos,
  decidirPedido,
  listarFilaPedidos,
  listarPedidos,
  submeterPedidoBaixa,
} from './pedidos';

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

describe('decidirLotePedidos', () => {
  it('POST /api/pedidos/decisao-lote com {pedidoIds, aprovar}, Authorization e Content-Type, devolve os pedidos do corpo', async () => {
    const decididos = [
      { ...RESUMO, id: 'p-1', status: 'aprovado', itens: [] },
      { ...RESUMO, id: 'p-2', status: 'aprovado', itens: [] },
    ];
    fetchMock.mockReturnValue(respostaOk({ pedidos: decididos }));

    const pedidos = await decidirLotePedidos(['p-1', 'p-2'], true);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/pedidos/decisao-lote');
    expect(init.method).toBe('POST');
    expect(init.headers).toMatchObject({
      'Content-Type': 'application/json',
      Authorization: 'Bearer token-de-teste',
    });
    expect(JSON.parse(init.body)).toEqual({ pedidoIds: ['p-1', 'p-2'], aprovar: true });
    expect(pedidos).toEqual(decididos);
  });

  it('envia {"aprovar":false} para rejeição em lote', async () => {
    fetchMock.mockReturnValue(respostaOk({ pedidos: [] }));

    await decidirLotePedidos(['p-1'], false);

    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body)).toEqual({ pedidoIds: ['p-1'], aprovar: false });
  });

  it('devolve [] quando o corpo não traz pedidos', async () => {
    fetchMock.mockReturnValue(respostaOk({}));
    expect(await decidirLotePedidos(['p-1'], true)).toEqual([]);
  });

  it('propaga a mensagem de erro do servidor num 400 (lote com retirada misturado)', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({
        error: { code: 'VALIDATION_ERROR', message: 'o lote só pode conter Pedidos de Transferência ou de Baixa' },
      }),
    });

    await expect(decidirLotePedidos(['p-1'], true)).rejects.toThrow(
      'o lote só pode conter Pedidos de Transferência ou de Baixa',
    );
  });

  it('cai no fallback específico do lote quando a resposta não-ok não traz error.message', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({}),
    });

    await expect(decidirLotePedidos(['p-1'], true)).rejects.toThrow(
      'Não foi possível registrar a decisão do lote agora. Tente novamente em instantes.',
    );
  });
});

describe('buscarReciboPedidoBlob', () => {
  it('GET /api/pedidos/{id}/recibo com Authorization e devolve o Blob do corpo', async () => {
    const blob = new Blob(['%PDF-fake'], { type: 'application/pdf' });
    fetchMock.mockReturnValue(Promise.resolve({ ok: true, blob: async () => blob }));

    const resultado = await buscarReciboPedidoBlob('p-1');

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/pedidos/p-1/recibo');
    expect(init.headers).toMatchObject({ Authorization: 'Bearer token-de-teste' });
    expect(resultado).toBe(blob);
  });

  it('propaga a mensagem de erro do servidor num 409 (pedido ainda não decidido)', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({
        error: { code: 'CONFLICT', message: 'pedido ainda não foi decidido: nenhum recibo disponível' },
      }),
    });

    await expect(buscarReciboPedidoBlob('p-1')).rejects.toThrow(
      'pedido ainda não foi decidido: nenhum recibo disponível',
    );
  });

  it('propaga a mensagem de erro do servidor num 404 (pedido alheio/inexistente)', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 404,
      json: async () => ({ error: { code: 'NOT_FOUND', message: 'pedido não encontrado' } }),
    });

    await expect(buscarReciboPedidoBlob('p-x')).rejects.toThrow('pedido não encontrado');
  });

  it('cai no fallback específico do recibo quando a resposta não-ok não traz error.message', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({}),
    });

    await expect(buscarReciboPedidoBlob('p-1')).rejects.toThrow(
      'Não foi possível baixar o recibo agora. Tente novamente em instantes.',
    );
  });
});

// Story 20.5 (spec-20-5): download do PDF da Pré-Nota — molde de
// buscarReciboPedidoBlob.
describe('baixarPreNotaPDF', () => {
  it('GET /api/pre-notas/{id}/pdf com Authorization e devolve o Blob do corpo', async () => {
    const blob = new Blob(['%PDF-fake'], { type: 'application/pdf' });
    fetchMock.mockReturnValue(Promise.resolve({ ok: true, blob: async () => blob }));

    const resultado = await baixarPreNotaPDF('pn-1');

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/pre-notas/pn-1/pdf');
    expect(init.headers).toMatchObject({ Authorization: 'Bearer token-de-teste' });
    expect(resultado).toBe(blob);
  });

  it('propaga a mensagem de erro do servidor (403 papel insuficiente)', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 403,
      json: async () => ({ error: { code: 'FORBIDDEN', message: 'permissão insuficiente' } }),
    });

    await expect(baixarPreNotaPDF('pn-1')).rejects.toThrow('permissão insuficiente');
  });

  it('erro de rede (fetch rejeitando) vira a mensagem amigável, nunca "Failed to fetch"', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'));

    await expect(baixarPreNotaPDF('pn-1')).rejects.toThrow(
      'Não foi possível baixar a Pré-Nota agora. Tente novamente em instantes.',
    );
  });

  it('falha ao ler o corpo (res.blob rejeitando) vira a mensagem amigável', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      blob: async () => {
        throw new TypeError('network error');
      },
    });

    await expect(baixarPreNotaPDF('pn-1')).rejects.toThrow(
      'Não foi possível baixar a Pré-Nota agora. Tente novamente em instantes.',
    );
  });

  it('cai no fallback específico da Pré-Nota quando a resposta não-ok não traz error.message', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500, json: async () => ({}) });

    await expect(baixarPreNotaPDF('pn-1')).rejects.toThrow(
      'Não foi possível baixar a Pré-Nota agora. Tente novamente em instantes.',
    );
  });
});

// Revisão pós-Story 20.2: submeterPedidoBaixa precisa nunca vazar um erro
// cru (TypeError de rede, SyntaxError de JSON inválido) — sempre a mensagem
// amigável (ou a do servidor, quando houver).
describe('submeterPedidoBaixa', () => {
  const INPUT = {
    obraCentroCusto: 'Obra Norte',
    observacao: '',
    produtoId: 'prod-1',
    estoqueId: 'est-1',
    quantidade: 5,
  };
  const MENSAGEM_FALLBACK =
    'Não foi possível enviar o pedido de baixa agora. Tente novamente em instantes.';

  it('POST /api/pedidos/baixa com Authorization e devolve o pedido no sucesso', async () => {
    const pedido = { id: 'ped-1', status: 'pendente', tipo: 'baixa' };
    fetchMock.mockReturnValue(respostaOk({ pedido }));

    const resultado = await submeterPedidoBaixa(INPUT);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/pedidos/baixa');
    expect(init.method).toBe('POST');
    expect(init.headers).toMatchObject({ Authorization: 'Bearer token-de-teste' });
    expect(JSON.parse(init.body)).toEqual(INPUT);
    expect(resultado).toEqual(pedido);
  });

  it('propaga a mensagem de erro do servidor numa resposta não-ok', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({ error: { code: 'CONFLICT', message: 'disponibilidade insuficiente para: X' } }),
    });

    await expect(submeterPedidoBaixa(INPUT)).rejects.toThrow('disponibilidade insuficiente para: X');
  });

  it('cai no fallback quando fetch() rejeita (rede offline) — nunca um erro cru', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'));

    await expect(submeterPedidoBaixa(INPUT)).rejects.toThrow(MENSAGEM_FALLBACK);
  });

  it('cai no fallback quando a resposta é ok mas o corpo não é JSON válido — nunca um erro cru', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => {
        throw new SyntaxError('Unexpected token');
      },
    });

    await expect(submeterPedidoBaixa(INPUT)).rejects.toThrow(MENSAGEM_FALLBACK);
  });
});
