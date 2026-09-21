import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { CentrosCustoSection } from './CentrosCustoSection';

const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess } }));

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
  return Promise.resolve({ ok: true, status: 200, json: async () => body });
}

function jsonErro(status: number, message: string) {
  return Promise.resolve({
    ok: false,
    status,
    json: async () => ({ error: { code: 'X', message } }),
  });
}

const CENTROS = [
  { id: 'c-1', nome: 'Estoque do Cabo' },
  { id: 'c-2', nome: 'Obra Norte' },
];

const metodo = (init?: RequestInit) => init?.method ?? 'GET';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('CentrosCustoSection', () => {
  it('lista os centros de custo de GET /api/centros-custo', async () => {
    stubFetch((url) => {
      if (url === '/api/centros-custo') return jsonOk({ centrosCusto: CENTROS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<CentrosCustoSection />);

    expect(await screen.findByText('Estoque do Cabo')).toBeInTheDocument();
    expect(screen.getByText('Obra Norte')).toBeInTheDocument();
  });

  it('lista vazia mostra a mensagem', async () => {
    stubFetch(() => jsonOk({ centrosCusto: [] }));
    render(<CentrosCustoSection />);
    expect(await screen.findByText('Nenhum centro de custo cadastrado ainda.')).toBeInTheDocument();
  });

  it('GET !ok no mount mostra role="alert" genérico', async () => {
    stubFetch(() => jsonErro(500, 'x'));
    render(<CentrosCustoSection />);
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar os centros de custo.',
    );
  });

  it('cadastro 201: POST com {nome}, toast, input limpo e lista recarregada', async () => {
    let gets = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/centros-custo' && metodo(init) === 'GET') {
        gets += 1;
        return jsonOk({ centrosCusto: gets === 1 ? [CENTROS[0]] : CENTROS });
      }
      if (url === '/api/centros-custo' && init?.method === 'POST') {
        return Promise.resolve({
          ok: true,
          status: 201,
          json: async () => ({ centroCusto: CENTROS[1] }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<CentrosCustoSection />);
    await screen.findByText('Estoque do Cabo');

    const input = screen.getByLabelText('Nome do centro de custo');
    await user.type(input, 'Obra Norte');
    await user.click(screen.getByRole('button', { name: 'Adicionar centro de custo' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/centros-custo',
        expect.objectContaining({ method: 'POST', body: JSON.stringify({ nome: 'Obra Norte' }) }),
      ),
    );
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Centro de custo criado.'));
    expect(await screen.findByText('Obra Norte')).toBeInTheDocument();
    expect(input).toHaveValue('');
  });

  it('cadastro 409: mostra a mensagem do servidor e não limpa o input', async () => {
    stubFetch((url, init) => {
      if (url === '/api/centros-custo' && metodo(init) === 'GET') return jsonOk({ centrosCusto: CENTROS });
      if (url === '/api/centros-custo' && init?.method === 'POST') {
        return jsonErro(409, 'já existe um centro de custo com esse nome');
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<CentrosCustoSection />);
    await screen.findByText('Estoque do Cabo');

    const input = screen.getByLabelText('Nome do centro de custo');
    await user.type(input, 'estoque do cabo');
    await user.click(screen.getByRole('button', { name: 'Adicionar centro de custo' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('já existe um centro de custo com esse nome');
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(input).toHaveValue('estoque do cabo');
  });

  it('botão desabilitado com o nome em branco e maxLength 255', async () => {
    stubFetch(() => jsonOk({ centrosCusto: [] }));
    const user = userEvent.setup();
    render(<CentrosCustoSection />);
    await screen.findByText('Nenhum centro de custo cadastrado ainda.');

    const botao = screen.getByRole('button', { name: 'Adicionar centro de custo' });
    expect(botao).toBeDisabled();
    expect(screen.getByLabelText('Nome do centro de custo')).toHaveAttribute('maxlength', '255');
    await user.type(screen.getByLabelText('Nome do centro de custo'), 'Nova');
    expect(botao).toBeEnabled();
  });
});
