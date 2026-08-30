import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { GestaoUsuariosSection } from './GestaoUsuariosSection';

// useAuth() fornece o id/papel do ator — configuráveis por teste.
const authState = vi.hoisted(() => ({
  id: 'ator-1' as string,
  papel: 'gestor' as string,
}));

vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: { id: authState.id, nome: 'Ator', email: 'ator@empresa.com', papel: authState.papel },
    definirSessao: vi.fn(),
  }),
}));

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

const CONTAS = [
  { id: 'u-1', nome: 'Ana Usuária', email: 'ana@empresa.com', papel: 'usuario', ativo: true },
  { id: 'a-1', nome: 'Bruno Almox', email: 'bruno@empresa.com', papel: 'almoxarife', ativo: true },
];

beforeEach(() => {
  authState.id = 'ator-1';
  authState.papel = 'gestor';
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('GestaoUsuariosSection', () => {
  it('lista as contas recebidas de GET /api/usuarios', async () => {
    stubFetch((url) => {
      if (url === '/api/usuarios') return jsonOk({ usuarios: CONTAS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<GestaoUsuariosSection />);

    expect(await screen.findByText('Ana Usuária')).toBeInTheDocument();
    expect(screen.getByText('Bruno Almox')).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith('/api/usuarios', expect.anything());
  });

  it('não mostra botões de ação na linha do próprio ator', async () => {
    stubFetch((url) => {
      if (url === '/api/usuarios') {
        return jsonOk({
          usuarios: [
            { id: 'ator-1', nome: 'Eu Mesmo', email: 'ator@empresa.com', papel: 'gestor', ativo: true },
            ...CONTAS,
          ],
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<GestaoUsuariosSection />);

    expect(await screen.findByText('Eu Mesmo')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Desativar conta de Eu Mesmo/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Rebaixar Eu Mesmo/ })).not.toBeInTheDocument();
    // As demais linhas seguem com ação.
    expect(screen.getByRole('button', { name: 'Desativar conta de Ana Usuária' })).toBeInTheDocument();
  });

  it('não mostra "Rebaixar" numa linha de papel usuario (sem papel abaixo)', async () => {
    stubFetch((url) => {
      if (url === '/api/usuarios') return jsonOk({ usuarios: CONTAS });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<GestaoUsuariosSection />);

    await screen.findByText('Ana Usuária');
    expect(screen.queryByRole('button', { name: /Rebaixar Ana Usuária/ })).not.toBeInTheDocument();
    // O almoxarife pode ser rebaixado para Usuário.
    expect(
      screen.getByRole('button', { name: 'Rebaixar Bruno Almox para Usuário' }),
    ).toBeInTheDocument();
  });

  it('"Desativar" confirma no ConfirmDialog e chama POST .../desativacao {ativo:false} + refaz o GET', async () => {
    let listaChamadas = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/usuarios') {
        listaChamadas += 1;
        return jsonOk({ usuarios: CONTAS });
      }
      if (url === '/api/usuarios/u-1/desativacao' && init?.method === 'POST') {
        return jsonOk({ usuario: { ...CONTAS[0], ativo: false } });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<GestaoUsuariosSection />);

    await user.click(await screen.findByRole('button', { name: 'Desativar conta de Ana Usuária' }));
    // ConfirmDialog aberto — nunca window.confirm().
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Desativar' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/usuarios/u-1/desativacao',
        expect.objectContaining({ method: 'POST', body: JSON.stringify({ ativo: false }) }),
      ),
    );
    await waitFor(() => expect(listaChamadas).toBe(2));
  });

  it('"Rebaixar" confirma no ConfirmDialog e chama POST .../rebaixamento + refaz o GET', async () => {
    let listaChamadas = 0;
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/usuarios') {
        listaChamadas += 1;
        return jsonOk({ usuarios: CONTAS });
      }
      if (url === '/api/usuarios/a-1/rebaixamento' && init?.method === 'POST') {
        return jsonOk({ usuario: { ...CONTAS[1], papel: 'usuario' } });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<GestaoUsuariosSection />);

    await user.click(
      await screen.findByRole('button', { name: 'Rebaixar Bruno Almox para Usuário' }),
    );
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Rebaixar' }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/usuarios/a-1/rebaixamento',
        expect.objectContaining({ method: 'POST' }),
      ),
    );
    const rebaixarInit = fetchMock.mock.calls.find(
      ([u]) => u === '/api/usuarios/a-1/rebaixamento',
    )?.[1] as RequestInit | undefined;
    expect(rebaixarInit?.body).toBeUndefined();
    await waitFor(() => expect(listaChamadas).toBe(2));
  });

  it('"Reativar" numa conta inativa é direto (sem ConfirmDialog)', async () => {
    const fetchMock = stubFetch((url, init) => {
      if (url === '/api/usuarios') {
        return jsonOk({
          usuarios: [{ ...CONTAS[1], ativo: false }],
        });
      }
      if (url === '/api/usuarios/a-1/desativacao' && init?.method === 'POST') {
        return jsonOk({ usuario: { ...CONTAS[1], ativo: true } });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<GestaoUsuariosSection />);

    await user.click(await screen.findByRole('button', { name: 'Reativar conta de Bruno Almox' }));

    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/usuarios/a-1/desativacao',
        expect.objectContaining({ method: 'POST', body: JSON.stringify({ ativo: true }) }),
      ),
    );
  });

  it('falha ao carregar a lista mostra um role="alert"', async () => {
    stubFetch((url) => {
      if (url === '/api/usuarios') {
        return Promise.resolve({
          ok: false,
          status: 500,
          json: async () => ({ error: { code: 'INTERNAL_ERROR' } }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<GestaoUsuariosSection />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar a lista de contas. Recarregue a página.',
    );
  });

  it('falha de ação (409) mostra role="alert" e refaz o GET /api/usuarios', async () => {
    let listaChamadas = 0;
    stubFetch((url, init) => {
      if (url === '/api/usuarios') {
        listaChamadas += 1;
        return jsonOk({ usuarios: CONTAS });
      }
      if (url === '/api/usuarios/a-1/rebaixamento' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 409,
          json: async () => ({ error: { code: 'CONFLICT' } }),
        });
      }
      throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
    });

    const user = userEvent.setup();
    render(<GestaoUsuariosSection />);

    await user.click(
      await screen.findByRole('button', { name: 'Rebaixar Bruno Almox para Usuário' }),
    );
    const dialog = await screen.findByRole('alertdialog');
    await user.click(within(dialog).getByRole('button', { name: 'Rebaixar' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível concluir a ação na conta.',
    );
    await waitFor(() => expect(listaChamadas).toBe(2));
  });

  it('não monta nem chama GET /api/usuarios quando o papel é abaixo de gestor', async () => {
    authState.papel = 'almoxarife';
    const fetchMock = stubFetch((url) => {
      throw new Error(`URL inesperada: ${url}`);
    });

    const { container } = render(<GestaoUsuariosSection />);

    expect(container).toBeEmptyDOMElement();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
