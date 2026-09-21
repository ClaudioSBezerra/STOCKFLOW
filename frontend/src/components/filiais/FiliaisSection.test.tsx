import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { FiliaisSection } from './FiliaisSection';

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

const FILIAIS = [
  { id: 'f-1', nome: 'Matriz' },
  { id: 'f-2', nome: 'Recife' },
];

const metodo = (init?: RequestInit) => init?.method ?? 'GET';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('FiliaisSection', () => {
  it('lista as filiais de GET /api/filiais', async () => {
    stubFetch((url) => {
      if (url === '/api/filiais') return jsonOk({ filiais: FILIAIS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<FiliaisSection />);

    expect(await screen.findByText('Matriz')).toBeInTheDocument();
    expect(screen.getByText('Recife')).toBeInTheDocument();
  });

  it('lista vazia mostra a mensagem', async () => {
    stubFetch(() => jsonOk({ filiais: [] }));
    render(<FiliaisSection />);
    expect(await screen.findByText('Nenhuma filial cadastrada ainda.')).toBeInTheDocument();
  });

  it('GET !ok no mount mostra role="alert" genérico', async () => {
    stubFetch(() => jsonErro(500, 'x'));
    render(<FiliaisSection />);
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar as filiais.',
    );
  });

  it('cadastro 201: POST com {nome}, toast, input limpo e lista recarregada', async () => {
    let gets = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/filiais' && metodo(init) === 'GET') {
        gets += 1;
        return jsonOk({ filiais: gets === 1 ? [FILIAIS[0]] : FILIAIS });
      }
      if (url === '/api/filiais' && init?.method === 'POST') {
        return Promise.resolve({
          ok: true,
          status: 201,
          json: async () => ({ filial: FILIAIS[1] }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<FiliaisSection />);
    await screen.findByText('Matriz');

    const input = screen.getByLabelText('Nome da filial');
    await user.type(input, 'Recife');
    await user.click(screen.getByRole('button', { name: 'Adicionar filial' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/filiais',
        expect.objectContaining({ method: 'POST', body: JSON.stringify({ nome: 'Recife' }) }),
      ),
    );
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Filial criada.'));
    expect(await screen.findByText('Recife')).toBeInTheDocument();
    expect(input).toHaveValue('');
  });

  it('cadastro 409: mostra a mensagem do servidor e não limpa o input', async () => {
    stubFetch((url, init) => {
      if (url === '/api/filiais' && metodo(init) === 'GET') return jsonOk({ filiais: FILIAIS });
      if (url === '/api/filiais' && init?.method === 'POST') {
        return jsonErro(409, 'já existe uma filial com esse nome');
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<FiliaisSection />);
    await screen.findByText('Matriz');

    const input = screen.getByLabelText('Nome da filial');
    await user.type(input, 'matriz');
    await user.click(screen.getByRole('button', { name: 'Adicionar filial' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('já existe uma filial com esse nome');
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(input).toHaveValue('matriz');
  });

  it('botão desabilitado com o nome em branco e maxLength 255', async () => {
    stubFetch(() => jsonOk({ filiais: [] }));
    const user = userEvent.setup();
    render(<FiliaisSection />);
    await screen.findByText('Nenhuma filial cadastrada ainda.');

    const botao = screen.getByRole('button', { name: 'Adicionar filial' });
    expect(botao).toBeDisabled();
    expect(screen.getByLabelText('Nome da filial')).toHaveAttribute('maxlength', '255');
    await user.type(screen.getByLabelText('Nome da filial'), 'Nova');
    expect(botao).toBeEnabled();
  });
});
