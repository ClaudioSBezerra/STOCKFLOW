import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { LoginPage } from './LoginPage';
import { clearAccessToken, getAccessToken, setAccessToken } from '@/lib/session';
import { resetSSOConfigCache, type SSOConfig } from '@/lib/keycloak/config';

const navigateMock = vi.fn();

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return {
    ...actual,
    useNavigate: () => navigateMock,
  };
});

// `useAuth` é mockado para isolar LoginPage do bootstrap do AuthProvider.
// `definirSessao` mantém o efeito real de guardar o token via lib/session.
const { definirSessaoMock } = vi.hoisted(() => ({ definirSessaoMock: vi.fn() }));

vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'anonimo',
    usuario: null,
    definirSessao: definirSessaoMock,
    logout: vi.fn(),
  }),
  AuthProvider: ({ children }: { children: ReactNode }) => children,
}));

// Roteador de fetch por URL: /api/auth/sso/config responde `ssoConfigResp`
// (default {enabled:false}); /api/auth/login responde `loginResp()`.
let ssoConfigResp: SSOConfig;
let loginResp: () => Promise<unknown>;
const fetchMock = vi.fn();

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
    resetSSOConfigCache();
    navigateMock.mockClear();
    definirSessaoMock.mockReset();
    definirSessaoMock.mockImplementation((_usuario: unknown, token: string) => {
      setAccessToken(token);
    });

    ssoConfigResp = { enabled: false };
    loginResp = () => Promise.reject(new Error('loginResp não configurado neste teste'));
    fetchMock.mockReset();
    fetchMock.mockImplementation((url: string) => {
      if (url === '/api/auth/sso/config') {
        return Promise.resolve({ ok: true, json: async () => ssoConfigResp });
      }
      if (url === '/api/auth/login') {
        return loginResp();
      }
      return Promise.reject(new Error(`fetch não stubado para ${url}`));
    });
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    clearAccessToken();
  });

  it('renderiza os campos e-mail/senha e o botão de enviar', () => {
    renderPage();

    expect(screen.getByLabelText('E-mail')).toBeInTheDocument();
    expect(screen.getByLabelText('Senha')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Entrar' })).toBeInTheDocument();
  });

  it('mostra o link "Esqueci minha senha" apontando para /esqueci-senha', () => {
    renderPage();

    expect(screen.getByRole('link', { name: 'Esqueci minha senha' })).toHaveAttribute(
      'href',
      '/esqueci-senha',
    );
  });

  it('envia POST /api/auth/login com o payload preenchido', async () => {
    const user = userEvent.setup();
    loginResp = () =>
      Promise.resolve({
        ok: true,
        json: async () => ({
          token: 'access-token-123',
          usuario: { id: '1', nome: 'x', email: 'x', papel: 'usuario' },
        }),
      });
    renderPage();

    await preencherEEnviar(user);

    await waitFor(() =>
      expect(fetchMock.mock.calls.some(([u]) => u === '/api/auth/login')).toBe(true),
    );
    const loginCall = fetchMock.mock.calls.find(([u]) => u === '/api/auth/login');
    expect(loginCall?.[1]?.method).toBe('POST');
    expect(JSON.parse(loginCall?.[1]?.body as string)).toEqual({
      email: 'fulano@empresa.com',
      senha: 'senha-123456',
    });
  });

  it('chama definirSessao(usuario, token) e navega para / no sucesso', async () => {
    const user = userEvent.setup();
    const usuario = { id: '1', nome: 'Fulano', email: 'fulano@empresa.com', papel: 'usuario' };
    loginResp = () =>
      Promise.resolve({ ok: true, json: async () => ({ token: 'access-token-123', usuario }) });
    renderPage();

    await preencherEEnviar(user);

    await waitFor(() => expect(definirSessaoMock).toHaveBeenCalledWith(usuario, 'access-token-123'));
    await waitFor(() => expect(getAccessToken()).toBe('access-token-123'));
    await waitFor(() => expect(navigateMock).toHaveBeenCalledWith('/'));
  });

  it('mostra a mesma mensagem genérica no 401 INVALID_CREDENTIALS', async () => {
    const user = userEvent.setup();
    loginResp = () =>
      Promise.resolve({
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

  it('mostra a mensagem de bloqueio (sem tempo restante) no 429 ACCOUNT_LOCKED', async () => {
    const user = userEvent.setup();
    loginResp = () =>
      Promise.resolve({
        ok: false,
        json: async () => ({ error: { code: 'ACCOUNT_LOCKED', message: 'qualquer coisa do backend' } }),
      });
    renderPage();

    await preencherEEnviar(user);

    const alerta = await screen.findByRole('alert');
    expect(alerta).toHaveTextContent(
      'Muitas tentativas de login sem sucesso. Por segurança, novas tentativas ficam bloqueadas temporariamente. Tente novamente mais tarde.',
    );
    // A mensagem nunca revela o tempo restante e não promete recuperação via
    // redefinição de senha.
    expect(alerta.textContent ?? '').not.toMatch(/\d/);
    expect(alerta.textContent ?? '').not.toMatch(/senha/i);
    // O link "Esqueci minha senha" continua visível como saída.
    expect(screen.getByRole('link', { name: 'Esqueci minha senha' })).toHaveAttribute(
      'href',
      '/esqueci-senha',
    );
    expect(getAccessToken()).toBeNull();
    expect(navigateMock).not.toHaveBeenCalled();
  });

  it('mostra erro inline de validação no 400 VALIDATION_ERROR', async () => {
    const user = userEvent.setup();
    loginResp = () =>
      Promise.resolve({
        ok: false,
        json: async () => ({ error: { code: 'VALIDATION_ERROR', message: 'obrigatorio' } }),
      });
    renderPage();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Preencha e-mail e senha para continuar.',
    );
  });

  it('mostra a mensagem genérica em erro HTTP com código desconhecido/ausente', async () => {
    const user = userEvent.setup();
    loginResp = () =>
      Promise.resolve({
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
    loginResp = () => Promise.reject(new Error('network down'));
    renderPage();

    await preencherEEnviar(user);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível entrar. Tente novamente em instantes.',
    );
  });

  it('nunca envia um segundo POST quando dois submits chegam antes do repaint', async () => {
    const user = userEvent.setup();
    loginResp = () =>
      Promise.resolve({
        ok: true,
        json: async () => ({
          token: 'access-token-123',
          usuario: { id: '1', nome: 'x', email: 'x', papel: 'usuario' },
        }),
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

    await waitFor(() =>
      expect(fetchMock.mock.calls.filter(([u]) => u === '/api/auth/login')).toHaveLength(1),
    );
  });

  describe('botão "Entrar com Ferreira Costa" (SSO, Story 1.9)', () => {
    it('não aparece quando /api/auth/sso/config responde enabled:false', async () => {
      renderPage();
      // dá tempo do useEffect resolver
      await waitFor(() =>
        expect(fetchMock.mock.calls.some(([u]) => u === '/api/auth/sso/config')).toBe(true),
      );
      expect(screen.queryByRole('button', { name: 'Entrar com Ferreira Costa' })).not.toBeInTheDocument();
      // o fluxo de senha continua intacto
      expect(screen.getByRole('button', { name: 'Entrar' })).toBeInTheDocument();
    });

    it('não aparece quando a config falha (erro de rede)', async () => {
      fetchMock.mockImplementation((url: string) => {
        if (url === '/api/auth/sso/config') return Promise.reject(new Error('offline'));
        return Promise.reject(new Error(`fetch não stubado para ${url}`));
      });
      renderPage();
      await new Promise((r) => setTimeout(r, 0));
      expect(screen.queryByRole('button', { name: 'Entrar com Ferreira Costa' })).not.toBeInTheDocument();
    });

    it('aparece quando enabled:true e o clique navega para a URL de authorize com S256', async () => {
      const user = userEvent.setup();
      const assignMock = vi.fn();
      vi.stubGlobal('location', { ...window.location, origin: 'http://localhost', assign: assignMock });

      ssoConfigResp = {
        enabled: true,
        base_url: 'https://kc.example/realms/ferreiracosta',
        client_id: 'stockflow-web',
        redirect_uri: 'http://localhost/auth/callback',
        scopes: 'openid profile email',
      };
      renderPage();

      const botao = await screen.findByRole('button', { name: 'Entrar com Ferreira Costa' });
      // o form de senha continua sendo o caminho padrão visível
      expect(screen.getByRole('button', { name: 'Entrar' })).toBeInTheDocument();

      await user.click(botao);

      await waitFor(() => expect(assignMock).toHaveBeenCalledTimes(1));
      const url = assignMock.mock.calls[0][0] as string;
      expect(url).toContain('https://kc.example/realms/ferreiracosta/protocol/openid-connect/auth?');
      expect(url).toContain('code_challenge_method=S256');
      expect(url).toContain('client_id=stockflow-web');
      expect(url).toContain('response_type=code');
    });
  });
});
