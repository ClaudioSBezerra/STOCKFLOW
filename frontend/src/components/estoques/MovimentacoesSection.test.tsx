import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import { MovimentacoesSection } from './MovimentacoesSection';
import type { EventoRealtime, StatusRealtime } from '@/lib/realtime/client';

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

const toastInfo = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { info: toastInfo } }));

// conectarRealtime mockado (molde de ProdutoDetalhePage.test.tsx): captura os
// dois callbacks (aoReceberEvento/aoMudarStatus) para disparar cada cenário
// diretamente, sem reexercitar a mecânica de reconexão do EventSource.
const conectarRealtimeMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/realtime/client', () => ({
  conectarRealtime: conectarRealtimeMock,
}));

let aoReceberEvento: (evento: EventoRealtime) => void;
let aoMudarStatus: (status: StatusRealtime) => void;
const desconectarMock = vi.fn();

type FetchImpl = (
  url: string,
  init?: RequestInit,
) => Promise<{ ok: boolean; status?: number; json: () => Promise<unknown> }>;

function stubFetch(impl: FetchImpl) {
  const fn = vi.fn(impl);
  vi.stubGlobal('fetch', fn);
  return fn;
}

function jsonOk(body: unknown) {
  return Promise.resolve({ ok: true, json: async () => body });
}

const MOVIMENTACOES = [
  {
    id: 'm-1',
    produtoId: 'p-1',
    produtoNome: 'Cabo Flexível 4mm',
    tipo: 'transferencia',
    estoqueOrigemId: 'e-1',
    estoqueOrigemNome: 'Almoxarifado Central',
    estoqueDestinoId: 'e-2',
    estoqueDestinoNome: 'Obra Norte',
    quantidade: 4,
    usuarioId: 'u-1',
    usuarioNome: 'Ana Almoxarife',
    criadoEm: '2026-08-15T14:30:00Z',
  },
  {
    id: 'm-2',
    produtoId: 'p-1',
    produtoNome: 'Cabo Flexível 4mm',
    tipo: 'baixa',
    estoqueOrigemId: 'e-1',
    estoqueOrigemNome: 'Almoxarifado Central',
    estoqueDestinoId: null,
    estoqueDestinoNome: null,
    quantidade: 3,
    usuarioId: 'u-1',
    usuarioNome: 'Ana Almoxarife',
    criadoEm: '2026-08-14T09:00:00Z',
  },
];

beforeEach(() => {
  conectarRealtimeMock.mockImplementation(
    (receber: (evento: EventoRealtime) => void, mudar: (status: StatusRealtime) => void) => {
      aoReceberEvento = receber;
      aoMudarStatus = mudar;
      return desconectarMock;
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('MovimentacoesSection', () => {
  it('carrega a tabela SÓ quando conectarRealtime chama aoMudarStatus("conectado")', async () => {
    const fetchMock = stubFetch((url) => {
      if (url === '/api/movimentacoes') return jsonOk({ movimentacoes: MOVIMENTACOES });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<MovimentacoesSection />);

    // Antes de "conectado", nenhum GET ainda foi disparado — mas há um
    // indicador de carregamento (não uma tela em branco).
    expect(fetchMock).not.toHaveBeenCalled();
    expect(screen.getByText('Carregando movimentações...')).toBeInTheDocument();

    act(() => {
      aoMudarStatus('conectado');
    });

    expect(await screen.findByText('Obra Norte')).toBeInTheDocument();
    // O indicador de carregamento some depois que as linhas chegam.
    expect(screen.queryByText('Carregando movimentações...')).not.toBeInTheDocument();
    for (const coluna of ['Produto', 'Tipo', 'Origem', 'Destino', 'Quantidade', 'Autor', 'Data']) {
      expect(screen.getByRole('columnheader', { name: coluna })).toBeInTheDocument();
    }
    // Transferência: origem e destino preenchidos, tipo traduzido.
    expect(screen.getByText('Transferência')).toBeInTheDocument();
    expect(screen.getByText('Obra Norte')).toBeInTheDocument();
    // Baixa: tipo traduzido e destino como "—".
    expect(screen.getByText('Baixa')).toBeInTheDocument();
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('não tem nenhum botão de ação em nenhuma linha', async () => {
    stubFetch((url) => {
      if (url === '/api/movimentacoes') return jsonOk({ movimentacoes: MOVIMENTACOES });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<MovimentacoesSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    expect(screen.queryAllByRole('button')).toHaveLength(0);
  });

  it('um evento SSE resource="movimentacoes" dispara toast + refetch, sem recarregar a tela', async () => {
    let chamada = 0;
    const fetchMock = stubFetch((url) => {
      if (url !== '/api/movimentacoes') throw new Error(`URL inesperada: ${url}`);
      chamada += 1;
      if (chamada === 1) return jsonOk({ movimentacoes: MOVIMENTACOES });
      return jsonOk({
        movimentacoes: [
          { ...MOVIMENTACOES[0], id: 'm-nova', produtoNome: 'Produto Novo' },
          ...MOVIMENTACOES,
        ],
      });
    });

    render(<MovimentacoesSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');
    expect(fetchMock).toHaveBeenCalledTimes(1);

    act(() => {
      aoReceberEvento({ resource: 'movimentacoes', id: 'm-nova', change: 'created' });
    });

    expect(toastInfo).toHaveBeenCalledWith('Movimentações atualizada.');
    expect(await screen.findByText('Produto Novo')).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('um evento SSE de outro resource é ignorado (sem toast, sem refetch)', async () => {
    const fetchMock = stubFetch((url) => {
      if (url === '/api/movimentacoes') return jsonOk({ movimentacoes: MOVIMENTACOES });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<MovimentacoesSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    act(() => {
      aoReceberEvento({ resource: 'produtos', id: 'p-1', change: 'updated' });
    });

    expect(toastInfo).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('status "reconectando" mostra o indicador persistente aria-live', async () => {
    stubFetch((url) => {
      if (url === '/api/movimentacoes') return jsonOk({ movimentacoes: MOVIMENTACOES });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<MovimentacoesSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    expect(screen.queryByText('Reconectando...')).not.toBeInTheDocument();

    act(() => {
      aoMudarStatus('reconectando');
    });

    const indicador = screen.getByText('Reconectando...');
    expect(indicador).toHaveAttribute('aria-live', 'polite');
    // Dado antigo permanece visível durante a reconexão.
    expect(screen.getByText('Obra Norte')).toBeInTheDocument();
  });

  it('lista vazia mostra "Nenhuma movimentação registrada."', async () => {
    stubFetch((url) => {
      if (url === '/api/movimentacoes') return jsonOk({ movimentacoes: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<MovimentacoesSection />);
    act(() => {
      aoMudarStatus('conectado');
    });

    expect(
      await screen.findByText('Nenhuma movimentação registrada.'),
    ).toBeInTheDocument();
  });

  it('res.ok === false mostra um role="alert" e mantém o dado anterior', async () => {
    let chamada = 0;
    stubFetch((url) => {
      if (url !== '/api/movimentacoes') throw new Error(`URL inesperada: ${url}`);
      chamada += 1;
      if (chamada === 1) return jsonOk({ movimentacoes: MOVIMENTACOES });
      return Promise.resolve({ ok: false, status: 500, json: async () => ({}) });
    });

    render(<MovimentacoesSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    act(() => {
      aoReceberEvento({ resource: 'movimentacoes', id: 'x', change: 'created' });
    });

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar as movimentações. Tente novamente em instantes.',
    );
    // Dado anterior mantido.
    expect(screen.getByText('Obra Norte')).toBeInTheDocument();
  });

  it('rejeição de rede no fetch mostra um role="alert"', async () => {
    stubFetch((url) => {
      if (url !== '/api/movimentacoes') throw new Error(`URL inesperada: ${url}`);
      return Promise.reject(new Error('rede'));
    });

    render(<MovimentacoesSection />);
    act(() => {
      aoMudarStatus('conectado');
    });

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar as movimentações. Tente novamente em instantes.',
    );
    // Sem load bem-sucedido: nem tabela, nem "Nenhuma movimentação registrada.".
    expect(screen.queryByText('Nenhuma movimentação registrada.')).not.toBeInTheDocument();
  });

  it('resultado no teto de 500 mostra o aviso de resultado capado', async () => {
    const movs500 = Array.from({ length: 500 }, (_, i) => ({
      ...MOVIMENTACOES[1],
      id: `m-${i}`,
    }));
    stubFetch((url) => {
      if (url === '/api/movimentacoes') return jsonOk({ movimentacoes: movs500 });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<MovimentacoesSection />);
    act(() => {
      aoMudarStatus('conectado');
    });

    expect(
      await screen.findByText(/Cada consulta mostra no máximo 500 movimentações/),
    ).toBeInTheDocument();
  });

  it('resultado abaixo de 500 NÃO mostra o aviso de resultado capado', async () => {
    stubFetch((url) => {
      if (url === '/api/movimentacoes') return jsonOk({ movimentacoes: MOVIMENTACOES });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<MovimentacoesSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    expect(
      screen.queryByText(/Cada consulta mostra no máximo 500 movimentações/),
    ).not.toBeInTheDocument();
  });

  it('desconecta a SSE no unmount', async () => {
    stubFetch((url) => {
      if (url === '/api/movimentacoes') return jsonOk({ movimentacoes: MOVIMENTACOES });
      throw new Error(`URL inesperada: ${url}`);
    });

    const { unmount } = render(<MovimentacoesSection />);
    act(() => {
      aoMudarStatus('conectado');
    });
    await screen.findByText('Obra Norte');

    unmount();
    expect(desconectarMock).toHaveBeenCalled();
  });
});
