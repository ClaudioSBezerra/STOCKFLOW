import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { AuthCallbackPage } from './AuthCallbackPage';
import { ErroCallbackSSO } from '@/lib/keycloak/callback';

const navigateMock = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});

const definirSessaoMock = vi.fn();
vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'anonimo',
    usuario: null,
    definirSessao: definirSessaoMock,
    logout: vi.fn(),
  }),
}));

const trocarMock = vi.fn();
vi.mock('@/lib/keycloak/callback', async () => {
  const actual = await vi.importActual<typeof import('@/lib/keycloak/callback')>(
    '@/lib/keycloak/callback',
  );
  return { ...actual, trocarCodePorSessao: (sp: URLSearchParams) => trocarMock(sp) };
});

function renderPage() {
  return render(
    <MemoryRouter>
      <AuthCallbackPage />
    </MemoryRouter>,
  );
}

describe('AuthCallbackPage', () => {
  beforeEach(() => {
    navigateMock.mockClear();
    definirSessaoMock.mockClear();
    trocarMock.mockReset();
    sessionStorage.clear();
  });
  afterEach(() => {
    sessionStorage.clear();
  });

  it('sucesso: marca auth_via_sso, chama definirSessao e navega para / (replace)', async () => {
    const usuario = { id: '1', nome: 'C', email: 'c@fc.com', papel: 'gestor' };
    trocarMock.mockResolvedValue({ token: 'sf-token', usuario });

    renderPage();

    await waitFor(() => expect(definirSessaoMock).toHaveBeenCalledWith(usuario, 'sf-token'));
    expect(sessionStorage.getItem('auth_via_sso')).toBe('1');
    expect(navigateMock).toHaveBeenCalledWith('/', { replace: true });
  });

  it('enquanto pende: mostra o texto de transição', () => {
    trocarMock.mockReturnValue(new Promise(() => {}));
    renderPage();
    expect(screen.getByText(/Concluindo login via Ferreira Costa/i)).toBeInTheDocument();
  });

  it('SSO_SEM_CONTA: alerta com "cadastre-se" + link para /cadastro, sem sessão', async () => {
    trocarMock.mockRejectedValue(new ErroCallbackSSO('SSO_SEM_CONTA'));

    renderPage();

    const alerta = await screen.findByRole('alert');
    expect(alerta).toHaveTextContent(/cadastre-se/i);
    expect(screen.getByRole('link', { name: 'Criar conta' })).toHaveAttribute('href', '/cadastro');
    expect(screen.getByRole('link', { name: 'Voltar para o login' })).toHaveAttribute(
      'href',
      '/login',
    );
    expect(definirSessaoMock).not.toHaveBeenCalled();
    expect(navigateMock).not.toHaveBeenCalled();
  });

  it('CSRF: alerta de possível CSRF com link para /login, sem sessão', async () => {
    trocarMock.mockRejectedValue(new ErroCallbackSSO('CSRF'));

    renderPage();

    const alerta = await screen.findByRole('alert');
    expect(alerta).toHaveTextContent(/CSRF/i);
    expect(screen.queryByRole('link', { name: 'Criar conta' })).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Voltar para o login' })).toHaveAttribute(
      'href',
      '/login',
    );
    expect(definirSessaoMock).not.toHaveBeenCalled();
  });

  it('EMAIL_NOT_VERIFIED: mensagem distinta, sem link de cadastro', async () => {
    trocarMock.mockRejectedValue(new ErroCallbackSSO('EMAIL_NOT_VERIFIED'));

    renderPage();

    const alerta = await screen.findByRole('alert');
    expect(alerta).toHaveTextContent(/confirme/i);
    expect(screen.queryByRole('link', { name: 'Criar conta' })).not.toBeInTheDocument();
  });
});
