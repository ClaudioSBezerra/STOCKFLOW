import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { LocaisEstoqueSection } from './LocaisEstoqueSection';

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

const ESTOQUES = [
  { id: 'e-1', nome: 'Almoxarifado Central' },
  { id: 'e-2', nome: 'Canteiro A' },
];

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('LocaisEstoqueSection', () => {
  it('lista os estoques recebidos de GET /api/estoques', async () => {
    stubFetch((url) => {
      if (url === '/api/estoques') return jsonOk({ estoques: ESTOQUES });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LocaisEstoqueSection />);

    expect(await screen.findByText('Almoxarifado Central')).toBeInTheDocument();
    expect(screen.getByText('Canteiro A')).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith('/api/estoques', expect.anything());
  });

  it('lista vazia mostra a mensagem "Nenhum estoque cadastrado ainda."', async () => {
    stubFetch((url) => {
      if (url === '/api/estoques') return jsonOk({ estoques: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LocaisEstoqueSection />);

    expect(await screen.findByText('Nenhum estoque cadastrado ainda.')).toBeInTheDocument();
  });

  it('cadastro 201: POST com {nome}, toast.success, GET refeito e input limpo', async () => {
    let gets = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/estoques' && (init?.method ?? 'GET') === 'GET') {
        gets += 1;
        return jsonOk({ estoques: gets === 1 ? [] : ESTOQUES });
      }
      if (url === '/api/estoques' && init?.method === 'POST') {
        return Promise.resolve({
          ok: true,
          status: 201,
          json: async () => ({ estoque: { id: 'e-9', nome: 'Depósito Novo' } }),
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<LocaisEstoqueSection />);
    await screen.findByText('Nenhum estoque cadastrado ainda.');

    const input = screen.getByLabelText('Nome do estoque');
    await user.type(input, 'Depósito Novo');
    await user.click(screen.getByRole('button', { name: 'Adicionar estoque' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/estoques',
        expect.objectContaining({ method: 'POST', body: JSON.stringify({ nome: 'Depósito Novo' }) }),
      ),
    );
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Estoque criado.'));
    await waitFor(() => expect(gets).toBe(2));
    expect(input).toHaveValue('');
  });

  it('cadastro 409: mostra o role="alert" específico e não limpa o input', async () => {
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/estoques' && (init?.method ?? 'GET') === 'GET') {
        return jsonOk({ estoques: [] });
      }
      if (url === '/api/estoques' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 409,
          json: async () => ({ error: { code: 'CONFLICT' } }),
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<LocaisEstoqueSection />);
    await screen.findByText('Nenhum estoque cadastrado ainda.');

    const input = screen.getByLabelText('Nome do estoque');
    await user.type(input, 'Canteiro A');
    await user.click(screen.getByRole('button', { name: 'Adicionar estoque' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Já existe um estoque com esse nome.',
    );
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(input).toHaveValue('Canteiro A');
    // Só o GET de mount + o POST — nenhum GET extra após o 409.
    expect(fetchMock.mock.calls.filter(([, init]) => (init?.method ?? 'GET') === 'GET')).toHaveLength(1);
  });

  it('GET !ok no mount mostra um role="alert" genérico', async () => {
    stubFetch((url) => {
      if (url === '/api/estoques') {
        return Promise.resolve({
          ok: false,
          status: 500,
          json: async () => ({ error: { code: 'INTERNAL_ERROR' } }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<LocaisEstoqueSection />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar a lista de estoques. Recarregue a página.',
    );
  });

  it('botão desabilitado enquanto o nome está em branco', async () => {
    stubFetch((url) => {
      if (url === '/api/estoques') return jsonOk({ estoques: [] });
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<LocaisEstoqueSection />);
    await screen.findByText('Nenhum estoque cadastrado ainda.');

    const botao = screen.getByRole('button', { name: 'Adicionar estoque' });
    expect(botao).toBeDisabled();

    await user.type(screen.getByLabelText('Nome do estoque'), 'Canteiro B');
    expect(botao).toBeEnabled();
  });
});
