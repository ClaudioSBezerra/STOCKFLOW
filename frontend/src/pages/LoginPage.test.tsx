import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { LoginPage } from './LoginPage';
import { clearAccessToken, getAccessToken, setAccessToken } from '@/lib/session';

const navigateMock = vi.fn();

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return {
    ...actual,
    useNavigate: () => navigateMock,
  };
});

// `useAuth` é mockado para isolar LoginPage do bootstrap do AuthProvider (que
// dispararia um fetch('/api/auth/refresh') na montagem e poluiria a contagem
// de chamadas de fetch destes testes). `definirSessao` mantém o efeito real
// de guardar o token via lib/session, para as asserções de getAccessToken
// continuarem válidas.
const { definirSessaoMock } = vi.hoisted(() => ({ definirSessaoMock: vi.fn() }));

vi.mock('@/lib/auth', () => ({
  useAuth: () => ({ estado: 'anonimo', usuario: null, definirSessao: definirSessaoMock }),
  AuthProvider: ({ children }: { children: ReactNode }) => children,
}));

function renderPage() {
  return render(
    <MemoryRouter>
      <LoginPage />
    </MemoryRouter>,
  );
}

async function preencherEEnviar(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('E-mail'), 'fulano@empresa.com');
  await user.type(screen.getByLabelText('Senha'), 'senha-123456');
  await user.click(screen.getByRole('button', { name: 'Entrar' }));
}

describe('LoginPage', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn());
    navigateMock.mockClear();
    definirSessaoMock.mockReset();
    // Comportamento padrão: definirSessao guarda o token como o AuthProvider real faz.
    definirSessaoMock.mockImplementation((_usuario: unknown, token: string) => {
      setAccessToken(token);
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    clearAccessToken();
  });

  it('renderiza os campos e-mail/senha e o botão de enviar', () => {
    renderPage();

    expect(screen.getByLabelText('E-mail')).toBeInTheDocument();
    expect(screen.getByLabelText('Senha')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Entrar' })).toBeInTheDocument();
  });

  it('envia POST /api/auth/login com o payload preenchido', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      json: async () => ({ token: 'access-token-123', usuario: { id: '1', nome: 'x', email: 'x', papel: 'usuario' } }),
    });
    renderPage();

    await preencherEEnviar(user);

    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    const [url, init] = (fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(url).toBe('/api/auth/login');
    expect(init.method).toBe('POST');
    const body = JSON.parse(init.body as string);
    expect(body).toEqual({ email: 'fulano@empresa.com', senha: 'senha-123456' });
  });

  it('chama definirSessao(usuario, token) e navega para / no sucesso', async () => {
    const user = userEvent.setup();
    const usuario = { id: '1', nome: 'Fulano', email: 'fulano@empresa.com', papel: 'usuario' };
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      json: async () => ({ token: 'access-token-123', usuario }),
    });
    renderPage();

    await preencherEEnviar(user);

    await waitFor(() => expect(definirSessaoMock).toHaveBeenCalledWith(usuario, 'access-token-123'));
    await waitFor(() => expect(getAccessToken()).toBe('access-token-123'));
    await waitFor(() => expect(navigateMock).toHaveBeenCalledWith('/'));
  });

  it('mostra a mesma mensagem genérica no 401 INVALID_CREDENTIALS', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      json: async () => ({ error: { code: 'INVALID_CREDENTIALS', message: 'qualquer coisa' } }),
    });
    renderPage();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent('E-mail ou senha inválidos.');
    expect(getAccessToken()).toBeNull();
    expect(definirSessaoMock).not.toHaveBeenCalled();
    expect(navigateMock).not.toHaveBeenCalled();
  });

  it('mostra erro inline de validação no 400 VALIDATION_ERROR', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      json: async () => ({ error: { code: 'VALIDATION_ERROR', message: 'obrigatorio' } }),
    });
    renderPage();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent('Preencha e-mail e senha para continuar.');
  });

  it('mostra a mensagem genérica em uma resposta HTTP de erro com código desconhecido/ausente', async () => {
    // Caminho distinto do erro de rede abaixo: aqui res.ok é false e o corpo
    // chega normalmente, só que com um `code` que mensagemDeErro não reconhece
    // (ex.: INTERNAL_ERROR de um 500) — cai no mesmo branch genérico, mas por
    // um caminho de código diferente (o `if (!res.ok)` de handleSubmit, nunca
    // o `catch`).
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      json: async () => ({ error: { code: 'INTERNAL_ERROR', message: 'falha ao processar login' } }),
    });
    renderPage();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível entrar. Tente novamente em instantes.',
    );
    expect(getAccessToken()).toBeNull();
    expect(navigateMock).not.toHaveBeenCalled();
  });

  it('mostra mensagem genérica quando a requisição falha por erro de rede', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error('network down'));
    renderPage();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível entrar. Tente novamente em instantes.',
    );
  });

  it('nunca envia um segundo POST quando dois submits chegam antes do repaint do botão desabilitado', async () => {
    const user = userEvent.setup();
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: true,
      json: async () => ({ token: 'access-token-123', usuario: { id: '1', nome: 'x', email: 'x', papel: 'usuario' } }),
    });
    renderPage();

    await user.type(screen.getByLabelText('E-mail'), 'fulano@empresa.com');
    await user.type(screen.getByLabelText('Senha'), 'senha-123456');

    const form = screen.getByRole('button', { name: 'Entrar' }).closest('form');
    if (!form) {
      throw new Error('formulário não encontrado');
    }
    fireEvent.submit(form);
    fireEvent.submit(form);

    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
  });
});
