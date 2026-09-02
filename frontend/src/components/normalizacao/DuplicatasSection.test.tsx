import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DuplicatasSection } from './DuplicatasSection';

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

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

const GRUPOS = [
  {
    produtos: [
      {
        id: 'p-1',
        nome: 'Tubo PVC 25mm',
        dimensoes: {
          comprimento: null,
          largura: null,
          diametro: { valor: 25, unidade: 'mm' },
          altura: null,
          espessura: null,
        },
      },
      {
        id: 'p-2',
        nome: 'Tubo PVC 25mm',
        dimensoes: {
          comprimento: null,
          largura: null,
          diametro: { valor: 2.5, unidade: 'cm' },
          altura: null,
          espessura: null,
        },
      },
    ],
  },
];

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('DuplicatasSection', () => {
  it('não busca nada no mount por padrão — só ao clicar em "Analisar duplicatas"', () => {
    const fetchMock = stubFetch(() => jsonOk({ grupos: [] }));
    render(<DuplicatasSection />);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('clique dispara o fetch e renderiza os grupos', async () => {
    const user = userEvent.setup();
    const fetchMock = stubFetch((url) => {
      if (url === '/api/normalizacao/duplicatas') return jsonOk({ grupos: GRUPOS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<DuplicatasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar duplicatas' }));

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/normalizacao/duplicatas',
      expect.objectContaining({ headers: { Authorization: 'Bearer token-de-teste' } }),
    );

    const nomes = await screen.findAllByText('Tubo PVC 25mm');
    expect(nomes).toHaveLength(2);
    expect(screen.getByRole('columnheader', { name: 'Produto' })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: 'Dimensões' })).toBeInTheDocument();
  });

  it('autoAnalisar dispara a análise uma vez ao montar, sem exigir clique', async () => {
    const fetchMock = stubFetch((url) => {
      if (url === '/api/normalizacao/duplicatas') return jsonOk({ grupos: GRUPOS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<DuplicatasSection autoAnalisar />);

    const nomes = await screen.findAllByText('Tubo PVC 25mm');
    expect(nomes).toHaveLength(2);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('lista vazia mostra "Nenhuma duplicata encontrada."', async () => {
    const user = userEvent.setup();
    stubFetch((url) => {
      if (url === '/api/normalizacao/duplicatas') return jsonOk({ grupos: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<DuplicatasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar duplicatas' }));

    expect(await screen.findByText('Nenhuma duplicata encontrada.')).toBeInTheDocument();
  });

  it('falha de rede mostra o alerta e nenhuma tabela', async () => {
    const user = userEvent.setup();
    stubFetch(() => Promise.reject(new Error('network down')));

    render(<DuplicatasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar duplicatas' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível analisar os produtos. Tente novamente em instantes.',
    );
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
  });

  it('resposta não-ok (ex. 500) mostra o mesmo alerta', async () => {
    const user = userEvent.setup();
    stubFetch(() => Promise.resolve({ ok: false, status: 500, json: async () => ({}) }));

    render(<DuplicatasSection />);
    await user.click(screen.getByRole('button', { name: 'Analisar duplicatas' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível analisar os produtos. Tente novamente em instantes.',
    );
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
  });
});
