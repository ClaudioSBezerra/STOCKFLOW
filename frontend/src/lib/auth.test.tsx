import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { StrictMode } from 'react';
import { render, screen, waitFor, act } from '@testing-library/react';
import { AuthProvider, useAuth } from './auth';
import { clearAccessToken, getAccessToken } from './session';

function Sonda() {
  const { estado, usuario, definirSessao } = useAuth();
  return (
    <div>
      <span data-testid="estado">{estado}</span>
      <span data-testid="papel">{usuario?.papel ?? '—'}</span>
      <span data-testid="nome">{usuario?.nome ?? '—'}</span>
      <button
        type="button"
        onClick={() =>
          definirSessao(
            { id: '9', nome: 'Logada', email: 'logada@empresa.com', papel: 'gestor' },
            'token-do-login',
          )
        }
      >
        definir
      </button>
    </div>
  );
}

function renderProvider() {
  return render(
    <AuthProvider>
      <Sonda />
    </AuthProvider>,
  );
}

const fetchMock = vi.fn();

describe('AuthProvider — bootstrap silencioso', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock);
    fetchMock.mockReset();
    clearAccessToken();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    clearAccessToken();
  });

  it('resolve a sessão via POST /api/auth/refresh + GET /api/auth/me e fica autenticado', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: true, json: async () => ({ token: 'access-abc' }) });
      }
      if (url === '/api/auth/me') {
        return Promise.resolve({
          ok: true,
          json: async () => ({ id: '1', nome: 'Fulano', email: 'f@empresa.com', papel: 'almoxarife' }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    renderProvider();

    await waitFor(() => expect(screen.getByTestId('estado')).toHaveTextContent('autenticado'));
    expect(screen.getByTestId('papel')).toHaveTextContent('almoxarife');
    expect(screen.getByTestId('nome')).toHaveTextContent('Fulano');
    expect(getAccessToken()).toBe('access-abc');

    // /me foi chamado com o Authorization: Bearer do token do refresh.
    const meCall = fetchMock.mock.calls.find(([u]) => u === '/api/auth/me');
    expect(meCall?.[1]?.headers?.Authorization).toBe('Bearer access-abc');
    // refresh foi um POST.
    const refreshCall = fetchMock.mock.calls.find(([u]) => u === '/api/auth/refresh');
    expect(refreshCall?.[1]?.method).toBe('POST');
  });

  it('cai para anonimo, sem erro visível, quando o refresh falha (sem cookie)', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: false, status: 401, json: async () => ({}) });
      }
      throw new Error(`não deveria chamar ${url}`);
    });

    renderProvider();

    await waitFor(() => expect(screen.getByTestId('estado')).toHaveTextContent('anonimo'));
    expect(screen.getByTestId('papel')).toHaveTextContent('—');
    expect(fetchMock.mock.calls.some(([u]) => u === '/api/auth/me')).toBe(false);
  });

  it('cai para anonimo quando o refresh dá 200 mas /me falha, e limpa o token rotacionado', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: true, json: async () => ({ token: 'access-abc' }) });
      }
      if (url === '/api/auth/me') {
        return Promise.resolve({ ok: false, status: 401, json: async () => ({}) });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    renderProvider();

    await waitFor(() => expect(screen.getByTestId('estado')).toHaveTextContent('anonimo'));
    expect(screen.getByTestId('papel')).toHaveTextContent('—');
    // O token que o refresh 200 gravou não pode sobrar em lib/session com o
    // app já se considerando anônimo.
    expect(getAccessToken()).toBeNull();
  });

  it('sob StrictMode dispara POST /api/auth/refresh exatamente uma vez', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: true, json: async () => ({ token: 'access-abc' }) });
      }
      if (url === '/api/auth/me') {
        return Promise.resolve({
          ok: true,
          json: async () => ({ id: '1', nome: 'Fulano', email: 'f@empresa.com', papel: 'gestor' }),
        });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    render(
      <StrictMode>
        <AuthProvider>
          <Sonda />
        </AuthProvider>
      </StrictMode>,
    );

    await waitFor(() => expect(screen.getByTestId('estado')).toHaveTextContent('autenticado'));

    const refreshCalls = fetchMock.mock.calls.filter(([u]) => u === '/api/auth/refresh');
    expect(refreshCalls).toHaveLength(1);
  });

  it('não reverte para anonimo se definirSessao rodar antes do refresh pendente rejeitar', async () => {
    let rejeitarRefresh!: (motivo: unknown) => void;
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return new Promise((_resolve, reject) => {
          rejeitarRefresh = reject;
        });
      }
      throw new Error(`não deveria chamar ${url}`);
    });

    renderProvider();
    // Bootstrap está pendente no refresh; login rápido estabelece a sessão.
    await act(async () => {
      screen.getByRole('button', { name: 'definir' }).click();
    });
    expect(screen.getByTestId('estado')).toHaveTextContent('autenticado');

    // Agora o refresh pendente falha — a sessão manual não pode ser revertida.
    await act(async () => {
      rejeitarRefresh(new Error('refresh 401 (rotacionado/revogado)'));
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(screen.getByTestId('estado')).toHaveTextContent('autenticado');
    expect(screen.getByTestId('papel')).toHaveTextContent('gestor');
    expect(getAccessToken()).toBe('token-do-login');
  });

  it('cai para anonimo quando o fetch de refresh rejeita (erro de rede)', async () => {
    fetchMock.mockRejectedValue(new Error('network down'));

    renderProvider();

    await waitFor(() => expect(screen.getByTestId('estado')).toHaveTextContent('anonimo'));
  });

  it('definirSessao promove para autenticado imediatamente e guarda o token', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: false, status: 401, json: async () => ({}) });
      }
      throw new Error(`não deveria chamar ${url}`);
    });

    renderProvider();
    await waitFor(() => expect(screen.getByTestId('estado')).toHaveTextContent('anonimo'));

    await act(async () => {
      screen.getByRole('button', { name: 'definir' }).click();
    });

    expect(screen.getByTestId('estado')).toHaveTextContent('autenticado');
    expect(screen.getByTestId('papel')).toHaveTextContent('gestor');
    expect(getAccessToken()).toBe('token-do-login');
  });

  it('useAuth lança fora de um AuthProvider', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => render(<Sonda />)).toThrow(/AuthProvider/);
    spy.mockRestore();
  });
});
