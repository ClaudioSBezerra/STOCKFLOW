import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import * as authModule from '@/lib/auth';
import { clearAccessToken, getAccessToken } from '@/lib/session';
import App, { RotaProtegida, router } from './App';

// `useAuth` vira um `vi.fn` que, por padrão, roda a implementação REAL
// (usada pelos testes de wiring de <App /> abaixo — AuthProvider + router de
// verdade). Os testes unitários de RotaProtegida sobrescrevem o retorno para
// dirigir cada estado sem rede.
const authReal = await vi.importActual<typeof import('@/lib/auth')>('@/lib/auth');

vi.mock('@/lib/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/auth')>();
  return { ...actual, useAuth: vi.fn(actual.useAuth) };
});

const useAuthMock = vi.mocked(authModule.useAuth);

function LocationDisplay() {
  const { pathname } = useLocation();
  return <span data-testid="pathname">{pathname}</span>;
}

function renderRota() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <LocationDisplay />
      <Routes>
        <Route path="/" element={<RotaProtegida />}>
          <Route index element={<div>árvore protegida</div>} />
        </Route>
        <Route path="/login" element={<div>tela de login</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('RotaProtegida (unidade)', () => {
  afterEach(() => {
    useAuthMock.mockReset();
  });

  it('estado carregando: mostra "Carregando..." e não redireciona', () => {
    useAuthMock.mockReturnValue({ estado: 'carregando', usuario: null, definirSessao: vi.fn() });
    renderRota();

    expect(screen.getByText('Carregando...')).toBeInTheDocument();
    expect(screen.getByTestId('pathname')).toHaveTextContent('/');
    expect(screen.queryByText('tela de login')).not.toBeInTheDocument();
    expect(screen.queryByText('árvore protegida')).not.toBeInTheDocument();
  });

  it('estado anonimo: redireciona para /login', () => {
    useAuthMock.mockReturnValue({ estado: 'anonimo', usuario: null, definirSessao: vi.fn() });
    renderRota();

    expect(screen.getByText('tela de login')).toBeInTheDocument();
    expect(screen.getByTestId('pathname')).toHaveTextContent('/login');
    expect(screen.queryByText('árvore protegida')).not.toBeInTheDocument();
    expect(screen.queryByText('Carregando...')).not.toBeInTheDocument();
  });

  it('estado autenticado: renderiza o AppShell e permanece em /', () => {
    useAuthMock.mockReturnValue({
      estado: 'autenticado',
      usuario: { id: '1', nome: 'Teste', email: 'teste@empresa.com', papel: 'usuario' },
      definirSessao: vi.fn(),
    });
    renderRota();

    expect(screen.getByText('árvore protegida')).toBeInTheDocument();
    expect(screen.getByTestId('pathname')).toHaveTextContent('/');
    expect(screen.getAllByRole('navigation', { name: 'Navegação principal' })).toHaveLength(2);
    expect(screen.queryByText('tela de login')).not.toBeInTheDocument();
  });

  it('estado inesperado (fail-closed): redireciona para /login', () => {
    useAuthMock.mockReturnValue({
      estado: 'estado-novo-nao-previsto' as never,
      usuario: null,
      definirSessao: vi.fn(),
    });
    renderRota();

    expect(screen.getByText('tela de login')).toBeInTheDocument();
    expect(screen.queryByText('árvore protegida')).not.toBeInTheDocument();
  });
});

describe('<App /> — wiring real de AuthProvider + RotaProtegida', () => {
  const fetchMock = vi.fn();

  beforeEach(async () => {
    useAuthMock.mockImplementation(authReal.useAuth);
    vi.stubGlobal('fetch', fetchMock);
    fetchMock.mockReset();
    clearAccessToken();
    // Reseta a localização do router (compartilhado, criado em App.tsx) entre
    // os casos — um caso anterior pode tê-lo deixado em /login.
    await router.navigate('/');
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    clearAccessToken();
    useAuthMock.mockReset();
  });

  it('sem cookie de refresh: bootstrap falha e a rota protegida cai em /login', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: false, status: 401, json: async () => ({}) });
      }
      throw new Error(`não deveria chamar ${url}`);
    });

    render(<App />);

    // Bootstrap resolve como anônimo -> RotaProtegida redireciona para a rota
    // pública /login (LoginPage real).
    expect(await screen.findByText('Acesse sua conta do stockflow.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Entrar' })).toBeInTheDocument();
    expect(window.location.pathname).toBe('/login');
    expect(screen.queryByRole('navigation', { name: 'Navegação principal' })).not.toBeInTheDocument();
  });

  it('/esqueci-senha renderiza EsqueciSenhaPage sem passar pelo RotaProtegida', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: false, status: 401, json: async () => ({}) });
      }
      throw new Error(`não deveria chamar ${url}`);
    });
    await router.navigate('/esqueci-senha');

    render(<App />);

    expect(
      await screen.findByText('Informe seu e-mail e enviaremos um link para redefinir a senha.'),
    ).toBeInTheDocument();
    expect(window.location.pathname).toBe('/esqueci-senha');
    expect(screen.queryByRole('navigation', { name: 'Navegação principal' })).not.toBeInTheDocument();
  });

  it('/redefinir-senha renderiza RedefinirSenhaPage sem passar pelo RotaProtegida', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: false, status: 401, json: async () => ({}) });
      }
      throw new Error(`não deveria chamar ${url}`);
    });
    await router.navigate('/redefinir-senha');

    render(<App />);

    // Sem `?token=` a página já resolve para o estado de link inválido, sem
    // tocar a API — prova que a rota pública renderiza a página, não o shell.
    expect(await screen.findByText('Este link de redefinição é inválido.')).toBeInTheDocument();
    expect(window.location.pathname).toBe('/redefinir-senha');
    expect(screen.queryByRole('navigation', { name: 'Navegação principal' })).not.toBeInTheDocument();
  });

  it('/configuracoes renderiza ConfiguracoesPage dentro do shell, não a PlaceholderPage', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/refresh') {
        return Promise.resolve({ ok: true, json: async () => ({ token: 'access-abc' }) });
      }
      if (url === '/api/auth/me') {
        return Promise.resolve({
          ok: true,
          json: async () => ({ id: '1', nome: 'Fulano', email: 'f@empresa.com', papel: 'usuario' }),
        });
      }
      if (url === '/api/promocoes/minha') {
        return Promise.resolve({ ok: true, json: async () => ({ solicitacao: null }) });
      }
      throw new Error(`URL inesperada: ${url}`);
    });
    await router.navigate('/configuracoes');

    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Meu Perfil' })).toBeInTheDocument();
    expect(window.location.pathname).toBe('/configuracoes');
    expect(screen.getByRole('button', { name: 'Solicitar promoção para Almoxarife' })).toBeInTheDocument();
    expect(screen.getAllByRole('navigation', { name: 'Navegação principal' }).length).toBeGreaterThan(0);
    expect(screen.queryByText('Em construção')).not.toBeInTheDocument();
  });

  it('cookie válido: refresh + /me resolvem e o AppShell é renderizado em /', async () => {
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

    render(<App />);

    await waitFor(() =>
      expect(screen.getAllByRole('navigation', { name: 'Navegação principal' })).toHaveLength(2),
    );
    expect(window.location.pathname).toBe('/');
    expect(getAccessToken()).toBe('access-abc');
    expect(screen.queryByText('Acesse sua conta do stockflow.')).not.toBeInTheDocument();
  });
});
